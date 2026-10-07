package main

import (
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

	"github.com/Nel-sokchhunly/fragile/notes"
)

// ChatItem is one row of the orchestrator chat (mirrors ChatItem in
// frontend/src/lib/types.ts): the user's messages, the orchestrator's text and
// tool calls, and escalations. IDs are the ids of the underlying agent_events
// rows, so they are unique and ordered within a session.
type ChatItem struct {
	ID         int64             `json:"id"`
	Kind       string            `json:"kind"` // user | assistant | tool | escalation
	Text       string            `json:"text,omitempty"`
	Name       string            `json:"name,omitempty"`    // tool
	Summary    string            `json:"summary,omitempty"` // tool
	Escalation *notes.Escalation `json:"escalation,omitempty"`
	At         string            `json:"at"`
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
func (a *App) CreateSession(name, workDir string) (notes.Session, error) {
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
	se, err := a.store.CreateSessionIn(name, dir)
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
// first if the session has never had one.
func (a *App) SendMessage(sessionID int64, text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("message must not be empty")
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
	return a.deliver(orch, text, true)
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

// StopSession stops the session's orchestrator and sub-agents (for good: past
// sessions are not resumed).
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
// to it from the database, plus the agents' MCP configs and output logs. The
// session's working directory and events.jsonl are left alone.
func (a *App) DeleteSession(sessionID int64) error {
	if _, err := a.store.GetSession(sessionID); err != nil {
		return err
	}
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
	s.Escalations = []notes.Escalation{}
	for _, e := range escs {
		if e.Status == "open" {
			s.Escalations = append(s.Escalations, e)
		}
	}
	s.Chat = []ChatItem{}
	for _, ag := range s.Agents {
		if ag.Role != "orchestrator" {
			continue
		}
		evs, err := a.store.ListAgentEventsOfType(sessionID, ag.ID, evUserMessage, evAssistantText, evToolUse, evEscalation, evResult)
		if err != nil {
			return s, err
		}
		for _, ev := range evs {
			if item, ok := a.chatItem(sessionID, ev); ok {
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
	if err := a.deliver(orch, msg, false); err != nil { // the chat shows the answer on the escalation itself
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
		return notes.Agent{}, errors.New("this session's orchestrator is not running (it was stopped, exited, or the app was restarted)")
	}
	return orchs[len(orchs)-1], nil
}

// deliver writes a message to the orchestrator's stdin; with persist it is also
// stored (and shown) as a user message, but only once the write succeeded, so a
// failed send leaves no phantom chat message. The row is reserved first (its id
// then orders it before the orchestrator's reply, which can arrive at once) and
// removed again if the write fails; it is published after the write. The
// orchestrator is mid-turn from then on.
func (a *App) deliver(orch notes.Agent, text string, persist bool) error {
	a.sendMu.Lock()
	defer a.sendMu.Unlock()
	var ev notes.AgentEvent
	if persist {
		b, _ := json.Marshal(map[string]string{"text": text})
		var err error
		if ev, err = a.store.AppendAgentEvent(orch.SessionID, orch.ID, evUserMessage, string(b)); err != nil {
			return err
		}
	}
	a.setBusy(orch.SessionID, true)
	if err := a.runner.SendUser(orch.ID, text); err != nil {
		if persist {
			a.store.DeleteAgentEvent(orch.SessionID, orch.ID, ev.ID)
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
	item := ChatItem{ID: ev.ID, At: ev.CreatedAt}
	var p struct {
		Text         string          `json:"text"`
		Name         string          `json:"name"`
		Input        json.RawMessage `json:"input"`
		EscalationID int64           `json:"escalation_id"`
		IsError      bool            `json:"is_error"`
		Result       string          `json:"result"`
	}
	if json.Unmarshal([]byte(ev.Payload), &p) != nil {
		return item, false
	}
	switch ev.Type {
	case evUserMessage:
		item.Kind, item.Text = "user", p.Text
	case evAssistantText:
		item.Kind, item.Text = "assistant", p.Text
	case evResult:
		if !p.IsError {
			return item, false
		}
		item.Kind, item.Text = "assistant", "Error: "+p.Result
	case evToolUse:
		item.Kind, item.Name, item.Summary = "tool", strings.TrimPrefix(p.Name, "mcp__fragile__"), toolSummary(p.Input)
	case evEscalation:
		e, err := a.store.FindEscalation(p.EscalationID)
		if err != nil || e.SessionID != sessionID {
			return item, false
		}
		item.Kind, item.Escalation = "escalation", &e
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
	if err != nil || !strings.Contains(string(out), filepath.Join(a.agentDir, fmt.Sprintf("agent-%d.mcp.json", ag.ID))) {
		return
	}
	syscall.Kill(-ag.PID, syscall.SIGKILL) // agents run as their own process group leader
}

// recoverStale records agents a previous run left "running" as crashed
// (nothing is resumed; processes that outlived a hard kill are killed) and
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
