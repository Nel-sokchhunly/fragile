package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// ChatItem is one row of the orchestrator chat (mirrors ChatItem in
// frontend/src/lib/types.ts): the user's messages, the orchestrator's text and
// tool calls, and escalations. IDs are the ids of the underlying agent_events
// rows, so they are unique and ordered within a session.
type ChatItem struct {
	ID          int64             `json:"id"`
	Kind        string            `json:"kind"` // user | assistant | tool | escalation | notice (a compaction)
	Text        string            `json:"text,omitempty"`
	Name        string            `json:"name,omitempty"`        // tool
	Summary     string            `json:"summary,omitempty"`     // tool
	Escalation  *notes.Escalation `json:"escalation,omitempty"`  // escalation
	Attachments []AttachmentInfo  `json:"attachments,omitempty"` // user; GetAttachment loads one by index
	At          string            `json:"at"`
}

// Attachment is a file the user sends with a chat message. Data is the base64
// of the file's bytes (no data: prefix).
type Attachment struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// AttachmentInfo describes a sent attachment; Size is in decoded bytes.
type AttachmentInfo struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
}

// Attachment limits (sizes are of the decoded bytes).
const (
	maxAttachments     = 10
	maxImageBytes      = 5 << 20
	maxPDFBytes        = 10 << 20
	maxTextBytes       = 256 << 10
	maxAttachmentBytes = 20 << 20 // all of one message's attachments together
)

// userMessage is the payload of a user_message agent event.
type userMessage struct {
	Text        string           `json:"text"`
	Attachments []AttachmentInfo `json:"attachments,omitempty"`
}

// attachment is a validated Attachment: what is stored and the content block sent.
type attachment struct {
	info  AttachmentInfo
	data  []byte
	block map[string]any
}

var imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// textTypes are the non-text/* media types accepted as text files.
var textTypes = map[string]bool{
	"application/json": true, "application/xml": true, "application/javascript": true,
	"application/x-javascript": true, "application/typescript": true, "application/x-sh": true,
	"application/x-yaml": true, "application/yaml": true, "application/toml": true, "application/sql": true,
	"application/x-httpd-php": true, "application/x-python": true, "application/graphql": true,
}

// prepareAttachments validates the attachments and builds their content blocks.
// Errors name the offending file.
func prepareAttachments(in []Attachment) ([]attachment, error) {
	if len(in) > maxAttachments {
		return nil, fmt.Errorf("too many attachments: %d (at most %d)", len(in), maxAttachments)
	}
	out := make([]attachment, 0, len(in))
	total := 0
	for _, at := range in {
		name := safeName(at.Name)
		mt := strings.ToLower(strings.TrimSpace(at.MediaType))
		if i := strings.IndexByte(mt, ';'); i >= 0 { // "text/plain; charset=utf-8"
			mt = strings.TrimSpace(mt[:i])
		}
		var limit int
		switch {
		case imageTypes[mt]:
			limit = maxImageBytes
		case mt == "application/pdf":
			limit = maxPDFBytes
		case strings.HasPrefix(mt, "text/") || textTypes[mt]:
			limit = maxTextBytes
		default:
			return nil, fmt.Errorf("%q: unsupported file type %q (images, PDFs and text files only)", name, at.MediaType)
		}
		if base64.StdEncoding.DecodedLen(len(at.Data)) > limit+3 { // before decoding something huge
			return nil, fmt.Errorf("%q is too large (at most %s)", name, sizeString(limit))
		}
		data, err := base64.StdEncoding.DecodeString(at.Data)
		if err != nil {
			return nil, fmt.Errorf("%q: invalid base64 data", name)
		}
		switch {
		case len(data) == 0:
			return nil, fmt.Errorf("%q is empty", name)
		case len(data) > limit:
			return nil, fmt.Errorf("%q is too large (at most %s)", name, sizeString(limit))
		}
		if total += len(data); total > maxAttachmentBytes {
			return nil, fmt.Errorf("attachments are too large together at %q (at most %s in total)", name, sizeString(maxAttachmentBytes))
		}
		var block map[string]any
		switch {
		case imageTypes[mt]:
			block = map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": mt, "data": at.Data}}
		case mt == "application/pdf":
			block = map[string]any{"type": "document", "source": map[string]any{"type": "base64", "media_type": mt, "data": at.Data}}
		default:
			if !utf8.Valid(data) {
				return nil, fmt.Errorf("%q is not valid UTF-8 text", name)
			}
			block = map[string]any{"type": "text", "text": "<file name=\"" + name + "\">\n" + string(data) + "\n</file>"}
		}
		out = append(out, attachment{info: AttachmentInfo{Name: name, MediaType: mt, Size: int64(len(data))}, data: data, block: block})
	}
	return out, nil
}

// safeName reduces a file name to a base name that is safe on disk and inside
// the <file name="..."> tag: no path, no separators, quotes or control characters.
func safeName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, strings.ToValidUTF8(name, "_"))
	name = strings.TrimSpace(name)
	if rs := []rune(name); len(rs) > 100 {
		name = string(rs[len(rs)-100:]) // keep the extension
	}
	if strings.Trim(name, ".") == "" {
		return "file"
	}
	return name
}

func sizeString(n int) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%d MB", n>>20)
	}
	return fmt.Sprintf("%d KB", n>>10)
}

// attachmentPath is where a user message's attachment is stored:
// <dataDir>/attachments/<session>/<event>/<index>-<name>.
func (a *App) attachmentPath(sessionID, eventID int64, index int, name string) string {
	return filepath.Join(a.eventAttachDir(sessionID, eventID), fmt.Sprintf("%d-%s", index, safeName(name)))
}

func (a *App) eventAttachDir(sessionID, eventID int64) string {
	return filepath.Join(a.attachDir, strconv.FormatInt(sessionID, 10), strconv.FormatInt(eventID, 10))
}

// saveAttachments writes a user message's attachments to disk; on error it removes what it wrote.
func (a *App) saveAttachments(sessionID, eventID int64, atts []attachment) error {
	if len(atts) == 0 {
		return nil
	}
	dir := a.eventAttachDir(sessionID, eventID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for i, at := range atts {
		if err := os.WriteFile(a.attachmentPath(sessionID, eventID, i, at.info.Name), at.data, 0o600); err != nil {
			os.RemoveAll(dir)
			return fmt.Errorf("saving attachment %q: %w", at.info.Name, err)
		}
	}
	return nil
}

// GetAttachment returns attachment index of the session's user chat message
// chatItemID as a data URL ("data:<media_type>;base64,...").
func (a *App) GetAttachment(sessionID, chatItemID int64, index int) (string, error) {
	orchs, err := a.store.ListAgents(sessionID, "orchestrator")
	if err != nil {
		return "", err
	}
	for _, o := range orchs {
		evs, err := a.store.ListAgentEvents(sessionID, o.ID, chatItemID-1, 1)
		if err != nil {
			return "", err
		}
		if len(evs) == 0 || evs[0].ID != chatItemID {
			continue
		}
		var p userMessage
		if evs[0].Type != evUserMessage || json.Unmarshal([]byte(evs[0].Payload), &p) != nil {
			break
		}
		if index < 0 || index >= len(p.Attachments) {
			return "", fmt.Errorf("message %d has no attachment %d", chatItemID, index)
		}
		info := p.Attachments[index]
		b, err := os.ReadFile(a.attachmentPath(sessionID, chatItemID, index, info.Name))
		if err != nil {
			return "", fmt.Errorf("attachment %q: %w", info.Name, err)
		}
		return "data:" + info.MediaType + ";base64," + base64.StdEncoding.EncodeToString(b), nil
	}
	return "", fmt.Errorf("message %d not found in session %d", chatItemID, sessionID)
}

// SessionSnapshot is everything the UI shows for one session; live changes
// after it arrive as events.
type SessionSnapshot struct {
	Session     notes.Session      `json:"session"`
	Agents      []notes.Agent      `json:"agents"`
	Tasks       []notes.Task       `json:"tasks"`
	Notes       []notes.Note       `json:"notes"`
	Chat        []ChatItem         `json:"chat"`
	Escalations []notes.Escalation `json:"escalations"` // the open ones
}

// CreateSession creates an empty session in workDir (name defaults to the
// directory's base name). Its orchestrator starts with the first SendMessage.
// A directory holds at most one session, live or past; deleting it frees the directory.
func (a *App) CreateSession(name, workDir string) (notes.Session, error) {
	return a.CreateSessionWithProvider(name, workDir, notes.ProviderClaude)
}

// CreateSessionWithProvider fixes the CLI provider for this conversation and its team.
func (a *App) CreateSessionWithProvider(name, workDir, provider string) (notes.Session, error) {
	if err := notes.CheckProvider(provider); err != nil {
		return notes.Session{}, err
	}
	dir, err := filepath.Abs(workDir)
	if err != nil {
		return notes.Session{}, err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return notes.Session{}, fmt.Errorf("%q is not a directory", workDir)
	}
	name = notes.FirstLine(strings.TrimSpace(name))
	if name == "" {
		name = filepath.Base(dir)
	}
	a.startMu.Lock() // racing creates for one directory must not both pass the check
	all, err := a.store.ListSessions()
	if err != nil {
		a.startMu.Unlock()
		return notes.Session{}, err
	}
	for _, other := range all {
		if other.WorkDir != "" && filepath.Clean(other.WorkDir) == dir {
			a.startMu.Unlock()
			return notes.Session{}, fmt.Errorf("a session for this directory already exists: %q; open it from the sidebar instead", other.Title)
		}
	}
	se, err := a.store.CreateSessionWithProvider(name, dir, provider)
	a.startMu.Unlock()
	if err != nil {
		return notes.Session{}, err
	}
	a.log.Write(notes.EventSessionCreated, se.ID, 0, se)
	a.recompute(se.ID) // no orchestrator yet: derives to done rather than the stored default "working"
	if cur, err := a.store.GetSession(se.ID); err == nil {
		se = cur
	}
	return se, nil
}

// SendMessage sends a chat message to the session's orchestrator, starting it
// first if the session has never had one. The text may be empty if there are
// attachments (images, PDFs, text files; see prepareAttachments for the limits).
func (a *App) SendMessage(sessionID int64, text string, attachments []Attachment) error {
	if strings.TrimSpace(text) == "" && len(attachments) == 0 {
		return errors.New("message must not be empty")
	}
	se, err := a.store.GetSession(sessionID)
	if err != nil {
		return err
	}
	atts, err := prepareAttachments(attachments)
	if err != nil {
		return err
	}
	if se.Provider == notes.ProviderCodex {
		for _, at := range atts {
			if at.info.MediaType == "application/pdf" {
				return errors.New("Codex does not support PDF attachments; send extracted text or an image instead")
			}
		}
	}
	a.startMu.Lock()
	orchs, err := a.store.ListAgents(sessionID, "orchestrator")
	var orch notes.Agent
	switch {
	case err != nil:
	case a.neverStarted(sessionID, orchs):
		a.setBusy(sessionID, true) // before the process exists, so the session never flickers to done
		if orch, err = a.runner.StartOrchestrator(sessionID, ""); err != nil {
			a.setBusy(sessionID, false) // the runner recorded the crash; the session derives to done
		}
	default:
		orch, err = a.liveOrchestrator(sessionID)
	}
	a.startMu.Unlock()
	if err != nil {
		return err
	}
	return a.deliver(orch, text, atts, true)
}

// neverStarted reports whether the session has no orchestrator that ever did
// any work: none at all, or only ones that crashed (e.g. a failed launch, or
// `claude` not logged in: just an error result) without replying or calling a
// tool. Such a session may start a fresh orchestrator.
func (a *App) neverStarted(sessionID int64, orchs []notes.Agent) bool {
	for _, o := range orchs {
		evs, err := a.store.ListAgentEventsOfType(sessionID, o.ID, evAssistantText, evToolUse)
		if o.Status != "crashed" || err != nil || len(evs) > 0 {
			return false
		}
	}
	return true
}

// resumeMessage is the first message a resumed orchestrator gets.
const resumeMessage = "This session was resumed after the app restarted or the session was stopped. " +
	"Sub-agents started before that are no longer running. Check get_subagent_status and the board, " +
	"respawn or finish any unfinished work, then report."

// ResumeSession starts a new orchestrator for a session whose orchestrator is
// not running (stopped, exited, or the app was restarted). It resumes the
// previous Claude Code conversation (--resume) and tells it what happened.
func (a *App) ResumeSession(sessionID int64) error {
	if _, err := a.store.GetSession(sessionID); err != nil {
		return err
	}
	a.startMu.Lock()
	orchs, err := a.store.ListAgents(sessionID, "orchestrator")
	var orch notes.Agent
	switch {
	case err != nil:
	case a.neverStarted(sessionID, orchs):
		err = errors.New("this session has not started an orchestrator yet; send a message to start one")
	case a.runner.Running(orchs[len(orchs)-1].ID):
		err = errors.New("this session's orchestrator is already running")
	default:
		var providerID string
		if providerID, err = a.providerSessionID(sessionID, orchs); err != nil {
			break
		}
		a.setBusy(sessionID, true) // before the process exists, so the session never flickers to done
		if orch, err = a.runner.ResumeOrchestrator(sessionID, providerID); err != nil {
			a.setBusy(sessionID, false) // the runner recorded the crash; the session derives to done
		}
	}
	a.startMu.Unlock()
	if err != nil {
		return err
	}
	return a.deliver(orch, resumeMessage, nil, true)
}

// providerSessionID returns the provider session id from the newest init line
// of the session's orchestrators, newest first (a resumed one logs its own).
func (a *App) providerSessionID(sessionID int64, orchs []notes.Agent) (string, error) {
	for i := len(orchs) - 1; i >= 0; i-- {
		evs, err := a.store.ListAgentEventsOfType(sessionID, orchs[i].ID, evSystem)
		if err != nil {
			return "", err
		}
		for j := len(evs) - 1; j >= 0; j-- {
			var p struct {
				Subtype   string `json:"subtype"`
				SessionID string `json:"session_id"`
			}
			if json.Unmarshal([]byte(evs[j].Payload), &p) == nil && p.Subtype == "init" && p.SessionID != "" {
				return p.SessionID, nil
			}
		}
	}
	se, _ := a.store.GetSession(sessionID)
	if se.Provider == notes.ProviderCodex {
		return "", errors.New("no Codex thread id was recorded for this session, so it cannot be resumed")
	}
	return "", errors.New("no Claude Code session id was recorded for this session, so it cannot be resumed")
}

// StopSession stops the session's orchestrator and sub-agents; ResumeSession
// can start the orchestrator again.
func (a *App) StopSession(sessionID int64) error {
	if _, err := a.store.GetSession(sessionID); err != nil {
		return err
	}
	a.runner.StopSession(sessionID)
	a.setBusy(sessionID, false)
	a.recompute(sessionID)
	return nil
}

// DeleteSession stops the session's agents, then removes it and everything tied
// to it from the database, plus the agents' MCP configs and output logs and the
// chat attachments. The
// session's working directory and events.jsonl are left alone.
func (a *App) DeleteSession(sessionID int64) error {
	if _, err := a.store.GetSession(sessionID); err != nil {
		return err
	}
	a.TerminalClose(sessionID)
	a.runner.StopSession(sessionID) // returns once every process is gone and recorded
	agents, err := a.store.ListAgents(sessionID, "")
	if err != nil {
		return err
	}
	if err := a.store.DeleteSession(sessionID); err != nil {
		return err
	}
	// SQLite reuses the highest rowid, so a new session can get this id: it must start clean.
	a.runner.Forget(sessionID)
	a.mu.Lock()
	delete(a.busy, sessionID)
	for _, ag := range agents {
		delete(a.models, ag.ID)
	}
	a.mu.Unlock()
	for _, ag := range agents {
		for _, ext := range []string{".mcp.json", ".jsonl"} {
			os.Remove(filepath.Join(a.agentDir, fmt.Sprintf("agent-%d%s", ag.ID, ext)))
		}
	}
	if err := os.RemoveAll(filepath.Join(a.attachDir, strconv.FormatInt(sessionID, 10))); err != nil {
		log.Printf("session %d: removing attachments: %v", sessionID, err)
	}
	a.log.Write(notes.EventSessionDeleted, sessionID, 0, nil)
	return nil
}

// GetSession returns the session's full state from the database, live or past.
func (a *App) GetSession(sessionID int64) (SessionSnapshot, error) {
	var s SessionSnapshot
	var err error
	if s.Session, err = a.store.GetSession(sessionID); err != nil {
		return s, err
	}
	if s.Agents, err = a.store.ListAgents(sessionID, ""); err != nil {
		return s, err
	}
	if s.Tasks, err = a.store.ListTasks(sessionID); err != nil {
		return s, err
	}
	board, err := a.store.SessionBoard(sessionID)
	if err != nil {
		return s, err
	}
	if s.Notes, err = a.store.ListNotes(sessionID, board, notes.NoteFilter{}); err != nil {
		return s, err
	}
	escs, err := a.store.ListEscalations(sessionID)
	if err != nil {
		return s, err
	}
	// Reuse this session-scoped read for chat history too: one lookup per
	// escalation event would otherwise repeat data already loaded here.
	escalations := make(map[int64]notes.Escalation, len(escs))
	s.Escalations = []notes.Escalation{}
	for _, e := range escs {
		escalations[e.ID] = e
		if e.Status == "open" {
			s.Escalations = append(s.Escalations, e)
		}
	}
	lookupEscalation := func(id int64) (notes.Escalation, error) {
		e, ok := escalations[id]
		if !ok {
			// History is read after the escalation list; a new escalation can
			// arrive between them. Resolve misses as live events do, with the
			// session check below still preventing foreign references.
			return a.store.FindEscalation(id)
		}
		return e, nil
	}
	s.Chat = []ChatItem{}
	for _, ag := range s.Agents {
		if ag.Role != "orchestrator" {
			continue
		}
		evs, err := a.store.ListAgentEventsOfType(sessionID, ag.ID, evUserMessage, evAssistantText, evToolUse, evEscalation, evResult, evSystem)
		if err != nil {
			return s, err
		}
		for _, ev := range evs {
			if item, ok := chatItemWithEscalationLookup(sessionID, ev, lookupEscalation); ok {
				s.Chat = append(s.Chat, item)
			}
		}
	}
	// ponytail: the whole chat in one call; page it if sessions grow past a few thousand items.
	if s.Agents == nil {
		s.Agents = []notes.Agent{}
	}
	if s.Tasks == nil {
		s.Tasks = []notes.Task{}
	}
	if s.Notes == nil {
		s.Notes = []notes.Note{}
	}
	return s, nil
}

// AnswerEscalation stores the user's answer, delivers it to the orchestrator as
// a user message that restates the question, and recomputes the session status.
// The answer is stored first and the escalation reopened if delivery fails, so
// the orchestrator never gets an answer the database does not have.
func (a *App) AnswerEscalation(escalationID int64, answer string) error {
	if strings.TrimSpace(answer) == "" {
		return errors.New("answer must not be empty")
	}
	a.ansMu.Lock()
	defer a.ansMu.Unlock()
	e, err := a.store.FindEscalation(escalationID)
	if err != nil {
		return err
	}
	if e.Status != "open" {
		return fmt.Errorf("escalation %d is already answered", e.ID)
	}
	orch, err := a.liveOrchestrator(e.SessionID)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("Answer to your escalation #%d.\nYour question: %s\nContext you gave: %s\nThe user's answer: %s",
		e.ID, e.Question, e.Context, answer)
	if e, err = a.store.AnswerEscalation(e.SessionID, e.ID, answer); err != nil {
		return err
	}
	if err := a.deliver(orch, msg, nil, false); err != nil { // the chat shows the answer on the escalation itself
		if rerr := a.store.ReopenEscalation(e.SessionID, e.ID); rerr != nil {
			log.Printf("escalation %d: reopening after failed delivery: %v", e.ID, rerr)
		}
		a.recompute(e.SessionID) // deliver recomputed while it was answered
		return err
	}
	a.log.Write(notes.EventEscalation, e.SessionID, e.AgentID, e) // react() recomputes the status
	if ev, err := a.store.EscalationEvent(e.SessionID, e.ID); err == nil {
		if item, ok := a.chatItem(e.SessionID, ev); ok {
			a.pushEvent(eventChatItem, e.SessionID, ev.AgentID, item)
		}
	}
	return nil
}

// onEscalation (notes.Server.OnEscalation) puts a new escalation into the
// orchestrator's history, which is what the chat shows. The status change comes
// from the escalation event the server already logged.
func (a *App) onEscalation(ag notes.Agent, e notes.Escalation) {
	b, _ := json.Marshal(map[string]int64{"escalation_id": e.ID})
	if _, err := a.record(ag, evEscalation, string(b)); err != nil {
		log.Printf("escalation %d: recording chat item: %v", e.ID, err)
	}
}

// liveOrchestrator returns the session's orchestrator if its process can take a message.
func (a *App) liveOrchestrator(sessionID int64) (notes.Agent, error) {
	orchs, err := a.store.ListAgents(sessionID, "orchestrator")
	if err != nil {
		return notes.Agent{}, err
	}
	if len(orchs) == 0 || !a.runner.Running(orchs[len(orchs)-1].ID) {
		return notes.Agent{}, errors.New("this session's orchestrator is not running (it was stopped, exited, or the app was restarted); resume the session to continue")
	}
	return orchs[len(orchs)-1], nil
}

// deliver writes a message to the orchestrator's stdin; with persist it is also
// stored (and shown) as a user message, but only once the write succeeded, so a
// failed send leaves no phantom chat message. The row is reserved first (its id
// then orders it before the orchestrator's reply, which can arrive at once) and
// removed again if the write fails; it is published after the write. The
// orchestrator is mid-turn from then on. Attachments (persisted messages only)
// are saved under the row's id once it exists and removed again with it.
func (a *App) deliver(orch notes.Agent, text string, atts []attachment, persist bool) error {
	a.sendMu.Lock()
	defer a.sendMu.Unlock()
	var blocks []map[string]any
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	msg := userMessage{Text: text}
	for _, at := range atts {
		blocks = append(blocks, at.block)
		msg.Attachments = append(msg.Attachments, at.info)
	}
	var ev notes.AgentEvent
	if persist {
		b, _ := json.Marshal(msg)
		var err error
		if ev, err = a.store.AppendAgentEvent(orch.SessionID, orch.ID, evUserMessage, string(b)); err != nil {
			return err
		}
		if err := a.saveAttachments(orch.SessionID, ev.ID, atts); err != nil {
			a.store.DeleteAgentEvent(orch.SessionID, orch.ID, ev.ID)
			a.setBusy(orch.SessionID, false) // a first message set it before starting the orchestrator
			a.recompute(orch.SessionID)
			return err
		}
	}
	a.setBusy(orch.SessionID, true)
	if err := a.runner.SendUserContent(orch.ID, blocks); err != nil {
		if persist {
			a.store.DeleteAgentEvent(orch.SessionID, orch.ID, ev.ID)
			if len(atts) > 0 {
				os.RemoveAll(a.eventAttachDir(orch.SessionID, ev.ID))
			}
		}
		a.setBusy(orch.SessionID, false)
		a.recompute(orch.SessionID)
		return err
	}
	if persist {
		a.publish(orch, ev)
	}
	a.recompute(orch.SessionID)
	return nil
}

// Output handling

// onLine (notes.Runner.OnLine) turns one line of an agent's stream-json output
// into stored and emitted agent events, and tracks whether the orchestrator is mid-turn.
func (a *App) onLine(ag notes.Agent, line []byte) {
	changed := false
	a.trackUsage(ag, line)
	for _, pe := range parseLine(line) {
		if _, err := a.record(ag, pe.Type, pe.Payload); err != nil {
			log.Printf("agent %d: storing %s event: %v", ag.ID, pe.Type, err)
			continue
		}
		if ag.Role == "orchestrator" {
			// A result ends the turn; any other activity means a turn is under way
			// (self-corrects if Claude Code folded queued messages into one turn).
			switch pe.Type {
			case evResult:
				changed = a.setBusy(ag.SessionID, false)
			case evAssistantText, evToolUse, evToolResult:
				changed = a.setBusy(ag.SessionID, true)
			}
		}
	}
	if changed {
		a.recompute(ag.SessionID)
	}
}

// record stores an agent event and emits it, plus the chat item it makes for the orchestrator.
func (a *App) record(ag notes.Agent, typ, payload string) (notes.AgentEvent, error) {
	ev, err := a.store.AppendAgentEvent(ag.SessionID, ag.ID, typ, payload)
	if err != nil {
		return ev, err
	}
	a.publish(ag, ev)
	return ev, nil
}

// publish emits a stored agent event, plus the chat item it makes for the orchestrator.
func (a *App) publish(ag notes.Agent, ev notes.AgentEvent) {
	a.pushEvent(eventAgentEvent, ag.SessionID, ag.ID, ev)
	if ag.Role == "orchestrator" {
		if item, ok := a.chatItem(ag.SessionID, ev); ok {
			a.pushEvent(eventChatItem, ag.SessionID, ag.ID, item)
		}
	}
}

// chatItem maps an orchestrator agent event to its chat row; ok is false for
// events the chat does not show.
func (a *App) chatItem(sessionID int64, ev notes.AgentEvent) (ChatItem, bool) {
	return chatItemWithEscalationLookup(sessionID, ev, a.store.FindEscalation)
}

// Live events resolve the current escalation from the store; snapshots reuse
// their already-loaded session list. Both paths keep the same access check.
func chatItemWithEscalationLookup(sessionID int64, ev notes.AgentEvent, findEscalation func(int64) (notes.Escalation, error)) (ChatItem, bool) {
	item := ChatItem{ID: ev.ID, At: ev.CreatedAt}
	var p struct {
		Text         string           `json:"text"`
		Name         string           `json:"name"`
		Input        json.RawMessage  `json:"input"`
		EscalationID int64            `json:"escalation_id"`
		IsError      bool             `json:"is_error"`
		Result       string           `json:"result"`
		Subtype      string           `json:"subtype"`
		Attachments  []AttachmentInfo `json:"attachments"`
	}
	if json.Unmarshal([]byte(ev.Payload), &p) != nil {
		return item, false
	}
	switch ev.Type {
	case evUserMessage:
		item.Kind, item.Text, item.Attachments = "user", p.Text, p.Attachments
	case evAssistantText:
		item.Kind, item.Text = "assistant", p.Text
	case evResult:
		switch {
		case !p.IsError:
			return item, false
		case p.Subtype == "error_during_execution" && p.Result == "": // how an interrupted turn ends
			item.Kind, item.Text = "assistant", "Interrupted."
		default:
			item.Kind, item.Text = "assistant", "Error: "+p.Result
		}
	case evToolUse:
		item.Kind, item.Name, item.Summary = "tool", strings.TrimPrefix(p.Name, "mcp__fragile__"), toolSummary(p.Input)
	case evEscalation:
		e, err := findEscalation(p.EscalationID)
		if err != nil || e.SessionID != sessionID {
			return item, false
		}
		item.Kind, item.Escalation = "escalation", &e
	case evSystem: // only compactions; init, status etc. are not shown
		var c compactBoundary
		if p.Subtype != "compact_boundary" || json.Unmarshal([]byte(ev.Payload), &c) != nil {
			return item, false
		}
		item.Kind, item.Text = "notice", compactNotice(c)
	default:
		return item, false
	}
	return item, true
}

// toolSummary picks the most telling argument of a tool call as a one-line summary.
func toolSummary(input json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(input, &m) == nil {
		for _, k := range []string{"file_path", "path", "command", "pattern", "task", "question", "url", "query", "description", "content"} {
			if s, ok := m[k].(string); ok && s != "" {
				return oneLine(s)
			}
		}
	}
	return oneLine(string(input))
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if rs := []rune(s); len(rs) > 120 {
		s = string(rs[:120]) + "..."
	}
	return s
}

// Status

// Session status rules: needs_you while an escalation is open and the
// orchestrator is alive to receive the answer; else working while the
// orchestrator is mid-turn or any sub-agent runs; else done.
func deriveStatus(openEscalations int, orchRunning, orchBusy bool, subagentsRunning int) string {
	switch {
	case openEscalations > 0 && orchRunning:
		return notes.SessionNeedsYou
	case orchRunning && orchBusy, subagentsRunning > 0:
		return notes.SessionWorking
	}
	return notes.SessionDone
}

// setBusy records whether the session's orchestrator is mid-turn and reports whether that changed.
func (a *App) setBusy(sessionID int64, busy bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	changed := a.busy[sessionID] != busy
	a.busy[sessionID] = busy
	return changed
}

// recompute derives the session's status and, if it changed, stores it and
// writes session_status_changed (which the UI receives and the log keeps).
func (a *App) recompute(sessionID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	se, err := a.store.GetSession(sessionID)
	if err != nil {
		return
	}
	agents, err := a.store.ListAgents(sessionID, "")
	if err != nil {
		log.Printf("session %d: status: %v", sessionID, err)
		return
	}
	escs, err := a.store.ListEscalations(sessionID)
	if err != nil {
		log.Printf("session %d: status: %v", sessionID, err)
		return
	}
	open := 0
	for _, e := range escs {
		if e.Status == "open" {
			open++
		}
	}
	orchRunning, subs := false, 0
	for _, ag := range agents {
		if ag.Status != "running" {
			continue
		}
		if ag.Role == "orchestrator" {
			orchRunning = true
		} else {
			subs++
		}
	}
	st := deriveStatus(open, orchRunning, a.busy[sessionID], subs)
	if st == se.Status {
		return
	}
	if err := a.store.SetSessionStatus(sessionID, st); err != nil {
		log.Printf("session %d: storing status: %v", sessionID, err)
		return
	}
	a.log.Write(notes.EventSessionStatusChanged, sessionID, 0, map[string]string{"status": st})
}

// Event flow

// push is the EventLog.OnEvent hook: it only queues, so it returns at once.
func (a *App) push(ev notes.Event) {
	a.qmu.Lock()
	a.queue = append(a.queue, ev)
	a.qmu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// pushEvent queues an app-level event (not written to the log) in the same stream.
func (a *App) pushEvent(name string, sessionID, agentID int64, payload any) {
	a.push(notes.Event{Time: nowUTC(), Event: name, SessionID: sessionID, AgentID: agentID, Payload: payload})
}

// loop forwards queued events to the UI, in order, then lets the status react to them.
// On stop it drains what is queued (and what that causes) before returning.
func (a *App) loop() {
	defer close(a.stopped)
	stopping := false
	for {
		a.qmu.Lock()
		batch := a.queue
		a.queue = nil
		a.qmu.Unlock()
		for _, ev := range batch {
			a.emit(ev.Event, ev)
			a.react(ev)
		}
		if len(batch) > 0 {
			continue
		}
		if stopping {
			return
		}
		select {
		case <-a.wake:
		case <-a.stop:
			stopping = true
		}
	}
}

// react updates what depends on an event: the session status, and the agent and
// task rows the UI upserts after a spawn or an exit.
func (a *App) react(ev notes.Event) {
	switch ev.Event {
	case notes.EventAgentSpawned, notes.EventAgentStatusChanged:
		if ev.Event == notes.EventAgentStatusChanged {
			a.mu.Lock()
			delete(a.models, ev.AgentID) // the agent is done with its model
			a.mu.Unlock()
		}
		if ag, err := a.store.GetAgent(ev.SessionID, ev.AgentID); err == nil {
			a.pushEvent(eventAgentUpdated, ev.SessionID, ag.ID, ag)
			if t, err := a.store.GetTask(ev.SessionID, ag.TaskID); err == nil {
				a.pushEvent(eventTaskUpdated, ev.SessionID, ag.ID, t)
			}
		}
		a.recompute(ev.SessionID)
	case notes.EventEscalation:
		a.recompute(ev.SessionID)
	}
}

// Startup recovery

// killOrphan kills the process group of an agent a hard-killed previous run
// left behind, but only if the process at its pid still runs with this agent's
// MCP config on its command line (the pid may have been reused). Best effort.
func (a *App) killOrphan(ag notes.Agent) {
	if ag.PID <= 1 {
		return
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(ag.PID), "-o", "command=").Output()
	if err != nil {
		return
	}
	marker := filepath.Join(a.agentDir, fmt.Sprintf("agent-%d.mcp.json", ag.ID))
	if !strings.Contains(string(out), marker) {
		return
	}
	syscall.Kill(-ag.PID, syscall.SIGKILL) // agents run as their own process group leader
}

// recoverStale records agents a previous run left "running" as crashed
// (nothing is resumed until ResumeSession; processes that outlived a hard kill are killed) and
// recomputes every session's status.
func (a *App) recoverStale() error {
	stale, err := a.store.MarkRunningAgentsCrashed()
	if err != nil {
		return err
	}
	for _, ag := range stale {
		a.killOrphan(ag)
		a.log.Write(notes.EventAgentStatusChanged, ag.SessionID, ag.ID,
			map[string]any{"role": ag.Role, "status": "crashed", "reason": "app restarted"})
	}
	sessions, err := a.store.ListSessions()
	if err != nil {
		return err
	}
	for _, se := range sessions {
		a.recompute(se.ID)
	}
	return nil
}

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339Nano) }
