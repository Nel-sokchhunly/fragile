package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// events collects what the app emits to the UI.
type events struct {
	mu  sync.Mutex
	got []notes.Event
}

func (e *events) emit(_ string, data any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.got = append(e.got, data.(notes.Event))
}

func (e *events) named(name string) []notes.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []notes.Event
	for _, ev := range e.got {
		if ev.Event == name {
			out = append(out, ev)
		}
	}
	return out
}

// statuses is the sequence of statuses announced by session_status_changed.
func (e *events) statuses(sessionID int64) []string {
	var out []string
	for _, ev := range e.named(notes.EventSessionStatusChanged) {
		if ev.SessionID == sessionID {
			out = append(out, ev.Payload.(map[string]string)["status"])
		}
	}
	return out
}

// echoOrchestrator stands in for `claude -p --input-format stream-json`: for
// every stdin line it prints an assistant message quoting it, then a result.
const echoOrchestrator = `while IFS= read -r line; do
  esc=$(printf '%s' "$line" | sed 's/\\/\\\\/g; s/"/\\"/g')
  printf '{"type":"assistant","message":{"content":[{"type":"text","text":"echo: %s"}]}}\n' "$esc"
  printf '{"type":"result","subtype":"success","is_error":false,"result":"ok"}\n'
done`

func newTestApp(t *testing.T, dir, script string) (*App, *events) {
	t.Helper()
	ev := &events{}
	a := NewApp()
	if err := a.open(dir, ev.emit); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.close)
	a.runner.Preflight = nil // tests must not depend on the host's claude or sandbox tools
	if script != "" {
		a.runner.Command = writeScript(t, script)
	}
	return a, ev
}

func writeScript(t *testing.T, script string) string {
	t.Helper()
	path := t.TempDir() + "/fake-claude"
	if err := writeFile(path, "#!/bin/sh\n"+script+"\n"); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func snapshot(t *testing.T, a *App, id int64) SessionSnapshot {
	t.Helper()
	s, err := a.GetSession(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func chatTexts(s SessionSnapshot) []string {
	var out []string
	for _, c := range s.Chat {
		out = append(out, c.Kind+": "+c.Text)
	}
	return out
}

func hasChat(a *App, id int64, want string) bool {
	s, err := a.GetSession(id)
	if err != nil {
		return false
	}
	for _, c := range chatTexts(s) {
		if strings.Contains(c, want) {
			return true
		}
	}
	return false
}

// startSession creates a session and sends its first message, which starts the orchestrator.
func startSession(t *testing.T, a *App, task string) notes.Session {
	t.Helper()
	se, err := a.CreateSession("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SendMessage(se.ID, task, nil); err != nil {
		t.Fatal(err)
	}
	return se
}

func TestSessionChatAndStatus(t *testing.T) {
	a, ev := newTestApp(t, t.TempDir(), echoOrchestrator)
	work := t.TempDir()
	se, err := a.CreateSession("", work)
	if err != nil {
		t.Fatal(err)
	}
	if se.Title != filepath.Base(work) || se.Status != "done" || se.WorkDir == "" {
		t.Fatalf("session = %+v", se)
	}
	if named, _ := a.CreateSession("my name\nmore", t.TempDir()); named.Title != "my name" {
		t.Fatalf("named session = %+v", named)
	}
	if err := a.SendMessage(se.ID, "Write the thing\nwith details", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first reply and done", func() bool {
		return hasChat(a, se.ID, "assistant: echo: ") && snapshot(t, a, se.ID).Session.Status == "done"
	})
	if err := a.SendMessage(se.ID, "second message", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "second reply", func() bool {
		return hasChat(a, se.ID, "second message") && snapshot(t, a, se.ID).Session.Status == "done"
	})

	s := snapshot(t, a, se.ID)
	if len(s.Agents) != 1 || s.Agents[0].Role != "orchestrator" || s.Tasks == nil || s.Notes == nil || len(s.Escalations) != 0 {
		t.Fatalf("snapshot = %+v", s)
	}
	var kinds []string
	for _, c := range s.Chat {
		kinds = append(kinds, c.Kind)
	}
	if strings.Join(kinds, ",") != "user,assistant,user,assistant" || !strings.Contains(s.Chat[0].Text, "Write the thing") {
		t.Fatalf("chat = %v", chatTexts(s))
	}
	// Raw events are paged per agent.
	page, err := a.GetAgentEvents(s.Agents[0].ID, 0, 2)
	if err != nil || len(page) != 2 {
		t.Fatalf("page 1 = %+v, %v", page, err)
	}
	rest, _ := a.GetAgentEvents(s.Agents[0].ID, page[1].ID, 100)
	if len(rest) == 0 || rest[0].ID <= page[1].ID {
		t.Fatalf("page 2 = %+v", rest)
	}

	waitFor(t, "status events", func() bool { return len(ev.statuses(se.ID)) >= 2 })
	if got := strings.Join(ev.statuses(se.ID), ","); !strings.HasPrefix(got, "done") {
		// created as working, so the first change announced is to done; later turns toggle
		t.Fatalf("status events = %s", got)
	}
	if len(ev.named(notes.EventSessionCreated)) != 2 || len(ev.named(eventAgentEvent)) < 4 || len(ev.named(eventChatItem)) < 4 {
		t.Fatalf("missing events: created=%d agent_event=%d chat_item=%d",
			len(ev.named(notes.EventSessionCreated)), len(ev.named(eventAgentEvent)), len(ev.named(eventChatItem)))
	}
	if err := a.SendMessage(se.ID, " ", nil); err == nil {
		t.Error("empty message accepted")
	}
	if _, err := a.CreateSession("x", "/definitely/not/here"); err == nil {
		t.Error("bad work dir accepted")
	}
}

// A directory holds one session, whatever its status, until that session is deleted.
func TestOneSessionPerDirectory(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), echoOrchestrator)
	work := t.TempDir()
	first, err := a.CreateSession("first", work)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(work+"/sub", 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{work, work + "/", work + "/.", work + "/sub/.."} {
		_, err := a.CreateSession("dup", dir)
		if want := `a session for this directory already exists: "first"; open it from the sidebar instead`; err == nil || err.Error() != want {
			t.Fatalf("create in %q: err = %v, want %q", dir, err, want)
		}
	}
	if err := a.StopSession(first.ID); err != nil { // a past session still holds its directory
		t.Fatal(err)
	}
	if _, err := a.CreateSession("dup", work); err == nil {
		t.Fatal("directory of a stopped session reused")
	}
	if _, err := a.CreateSession("other", t.TempDir()); err != nil {
		t.Fatalf("other directory: %v", err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	race := t.TempDir()
	for range 2 { // racing creates for one directory make one session
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.CreateSession("", race); err == nil {
				mu.Lock()
				created++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("racing creates made %d sessions", created)
	}
	if err := a.DeleteSession(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateSession("again", work); err != nil {
		t.Fatalf("directory not freed by delete: %v", err)
	}
}

// A new session has no orchestrator (and no agents) until the first message starts it, once.
func TestFirstMessageStartsOrchestrator(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), echoOrchestrator)
	se, err := a.CreateSession("fresh", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s := snapshot(t, a, se.ID); len(s.Agents) != 0 || len(s.Chat) != 0 {
		t.Fatalf("fresh session = %+v", s)
	}
	var wg sync.WaitGroup
	for _, m := range []string{"one", "two"} { // racing first messages must start one orchestrator
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.SendMessage(se.ID, m, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	waitFor(t, "both replies", func() bool { return hasChat(a, se.ID, "echo: ") && snapshot(t, a, se.ID).Session.Status == "done" })
	if s := snapshot(t, a, se.ID); len(s.Agents) != 1 || s.Agents[0].Role != "orchestrator" || s.Agents[0].Status != "running" {
		t.Fatalf("agents = %+v", s.Agents)
	}
}

func TestStopSession(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), echoOrchestrator)
	se := startSession(t, a, "task")
	if err := a.StopSession(se.ID); err != nil {
		t.Fatal(err)
	}
	s := snapshot(t, a, se.ID)
	if s.Session.Status != "done" || s.Agents[0].Status != "stopped" { // killed on purpose, not "crashed"
		t.Fatalf("after stop: %+v / %+v", s.Session, s.Agents[0])
	}
	if err := a.SendMessage(se.ID, "hello?", nil); err == nil {
		t.Error("message to a stopped session accepted")
	}
}

// initOrchestrator is echoOrchestrator that first prints Claude Code's init line,
// and says so when it was started with --resume of that session.
const initOrchestrator = `printf '{"type":"system","subtype":"init","session_id":"claude-1"}\n'
case "$*" in *"--resume claude-1"*) printf '{"type":"assistant","message":{"content":[{"type":"text","text":"resumed claude-1"}]}}\n' ;; esac
` + echoOrchestrator

func TestResumeSession(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), initOrchestrator)
	se, err := a.CreateSession("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ResumeSession(se.ID); err == nil || !strings.Contains(err.Error(), "not started") {
		t.Fatalf("resume of a never-started session: err = %v", err)
	}
	if err := a.SendMessage(se.ID, "task", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first reply", func() bool { return hasChat(a, se.ID, "echo: ") && snapshot(t, a, se.ID).Session.Status == "done" })
	if err := a.ResumeSession(se.ID); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("resume of a running session: err = %v", err)
	}
	if err := a.StopSession(se.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.SendMessage(se.ID, "hello?", nil); err == nil || !strings.Contains(err.Error(), "resume") {
		t.Fatalf("message to a stopped session: err = %v", err)
	}
	if err := a.ResumeSession(se.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "resumed reply", func() bool {
		return hasChat(a, se.ID, "assistant: resumed claude-1") && hasChat(a, se.ID, `"text":"This session was resumed`) &&
			snapshot(t, a, se.ID).Session.Status == "done"
	})
	s := snapshot(t, a, se.ID)
	if len(s.Agents) != 2 || s.Agents[0].Status != "stopped" || s.Agents[1].Role != "orchestrator" || s.Agents[1].Status != "running" {
		t.Fatalf("agents = %+v", s.Agents)
	}
	if !hasChat(a, se.ID, "user: This session was resumed") {
		t.Fatalf("resume message not in chat: %v", chatTexts(s))
	}
	if err := a.SendMessage(se.ID, "after resume", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reply after resume", func() bool { return hasChat(a, se.ID, `"text":"after resume`) })
}

// Without a recorded init line there is no Claude Code session to resume.
func TestResumeSessionNeedsClaudeSessionID(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), echoOrchestrator)
	se := startSession(t, a, "task")
	waitFor(t, "first reply", func() bool { return hasChat(a, se.ID, "echo: ") })
	if err := a.StopSession(se.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.ResumeSession(se.ID); err == nil || !strings.Contains(err.Error(), "session id") {
		t.Fatalf("resume without a session id: err = %v", err)
	}
}

func TestDeleteSession(t *testing.T) {
	a, ev := newTestApp(t, t.TempDir(), echoOrchestrator)
	keep := startSession(t, a, "keep me")
	work := t.TempDir()
	gone, err := a.CreateSession("gone", work)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SendMessage(gone.ID, "delete me", []Attachment{{Name: "a.txt", MediaType: "text/plain", Data: b64("doomed")}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "replies", func() bool { return hasChat(a, keep.ID, "echo: ") && hasChat(a, gone.ID, "echo: ") })
	orch := snapshot(t, a, gone.ID).Agents[0]
	task, _ := a.store.CreateTask(gone.ID, "t", "d")
	sub, _ := a.store.CreateAgent(gone.ID, "subagent", orch.ID, task.ID)
	a.store.SetTaskAgent(gone.ID, task.ID, sub.ID)
	if _, err := a.AddNote(gone.ID, "decision", "use tabs"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddNote(keep.ID, "decision", "use spaces"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(work+"/mine.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := filepath.Join(a.agentDir, fmt.Sprintf("agent-%d.*", orch.ID))
	if m, _ := filepath.Glob(files); len(m) != 2 {
		t.Fatalf("agent files before delete = %v", m)
	}
	goneAtts := filepath.Join(a.attachDir, fmt.Sprint(gone.ID))
	if m, _ := filepath.Glob(filepath.Join(goneAtts, "*", "0-a.txt")); len(m) != 1 {
		t.Fatalf("attachment files before delete = %v", m)
	}

	if err := a.DeleteSession(gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetSession(gone.ID); err == nil {
		t.Error("deleted session still readable")
	}
	if list, _ := a.ListSessions(); len(list) != 1 || list[0].ID != keep.ID {
		t.Fatalf("sessions = %+v", list)
	}
	if m, _ := filepath.Glob(files); len(m) != 0 {
		t.Errorf("agent files left = %v", m)
	}
	if _, err := os.Stat(goneAtts); !os.IsNotExist(err) {
		t.Errorf("attachments dir left: %v", err)
	}
	if _, err := os.Stat(work + "/mine.txt"); err != nil {
		t.Errorf("work dir touched: %v", err)
	}
	if ags, _ := a.store.ListAgents(gone.ID, ""); len(ags) != 0 {
		t.Errorf("agents left behind: %+v", ags)
	}
	if ts, _ := a.store.ListTasks(gone.ID); len(ts) != 0 {
		t.Errorf("tasks left behind: %+v", ts)
	}
	if s := snapshot(t, a, keep.ID); len(s.Agents) != 1 || len(s.Notes) != 1 || len(s.Chat) < 2 {
		t.Fatalf("other session damaged: %+v", s)
	}
	waitFor(t, "session_deleted", func() bool { return len(ev.named(notes.EventSessionDeleted)) == 1 })
	if err := a.DeleteSession(gone.ID); err == nil {
		t.Error("deleted twice")
	}
}

// SQLite reuses the highest rowid, so a session created after a delete can get the deleted one's id:
// its orchestrator must still start.
func TestCreateAfterDeleteReusesIDAndStarts(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), echoOrchestrator)
	old := startSession(t, a, "first")
	waitFor(t, "reply", func() bool { return hasChat(a, old.ID, "echo: ") })
	if err := a.DeleteSession(old.ID); err != nil {
		t.Fatal(err)
	}
	se, err := a.CreateSession("again", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if se.ID != old.ID {
		t.Fatalf("new session id = %d, want the reused %d (the test needs the reuse)", se.ID, old.ID)
	}
	if err := a.SendMessage(se.ID, "second", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reply", func() bool { return hasChat(a, se.ID, "assistant: echo: ") })
	if s := snapshot(t, a, se.ID); len(s.Agents) != 1 || s.Agents[0].Status != "running" {
		t.Fatalf("agents = %+v", s.Agents)
	}
}

// A launch that fails on the first message does not brick the session: once the
// cause is fixed, the next message starts the orchestrator.
func TestFailedFirstStartCanBeRetried(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), echoOrchestrator)
	a.runner.Preflight = func() error { return errors.New("claude CLI not found on PATH") }
	se, err := a.CreateSession("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // failing twice must not matter either
		if err := a.SendMessage(se.ID, "hello", nil); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("send %d: err = %v, want the launch error", i, err)
		}
	}
	a.mu.Lock()
	busy := a.busy[se.ID]
	a.mu.Unlock()
	if busy {
		t.Fatal("session stuck busy after a failed start")
	}
	waitFor(t, "status done", func() bool { return snapshot(t, a, se.ID).Session.Status == "done" })
	a.runner.Preflight = nil
	if err := a.SendMessage(se.ID, "hello again", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reply", func() bool { return hasChat(a, se.ID, "assistant: echo: ") })
}

// An orchestrator that crashed on the first message without doing any work (e.g.
// `claude` not logged in: only an error result) does not brick the session either.
func TestCrashedWithoutWorkCanBeRetried(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), `IFS= read -r line
printf '{"type":"system","subtype":"init"}\n'
printf '{"type":"result","subtype":"success","is_error":true,"result":"Invalid API key · Please run /login"}\n'
exit 1`)
	se := startSession(t, a, "hello")
	waitFor(t, "crash", func() bool {
		s := snapshot(t, a, se.ID)
		return len(s.Agents) == 1 && s.Agents[0].Status == "crashed" && hasChat(a, se.ID, "Please run /login")
	})
	a.runner.Command = writeScript(t, echoOrchestrator) // logged in now
	if err := a.SendMessage(se.ID, "hello again", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reply", func() bool { return hasChat(a, se.ID, "assistant: echo: ") })
	if s := snapshot(t, a, se.ID); len(s.Agents) != 2 || s.Agents[1].Status != "running" {
		t.Fatalf("agents = %+v", s.Agents)
	}
}

// A send that fails leaves no chat message behind.
func TestFailedSendLeavesNoChatMessage(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), echoOrchestrator)
	se := startSession(t, a, "task")
	waitFor(t, "reply", func() bool { return hasChat(a, se.ID, "assistant: echo: ") })
	orch := snapshot(t, a, se.ID).Agents[0]
	a.runner.StopAll()
	atts, err := prepareAttachments([]Attachment{{Name: "g.txt", MediaType: "text/plain", Data: b64("boo")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.deliver(orch, "ghost", atts, true); err == nil {
		t.Fatal("deliver to a dead orchestrator succeeded")
	}
	if hasChat(a, se.ID, "ghost") {
		t.Fatal("phantom user message in the chat")
	}
	if m, _ := filepath.Glob(filepath.Join(a.attachDir, fmt.Sprint(se.ID), "*", "*")); len(m) != 0 {
		t.Fatalf("attachment files left after a failed send: %v", m)
	}
}

// Result lines show in the chat only as errors; an interrupted turn says so.
func TestChatItemResult(t *testing.T) {
	a := NewApp()
	for _, c := range []struct {
		payload string
		ok      bool
		text    string
	}{
		{`{"type":"result","subtype":"success","is_error":false,"result":"ok"}`, false, ""},
		{`{"type":"result","subtype":"success","is_error":true,"result":"Invalid API key"}`, true, "Error: Invalid API key"},
		{`{"type":"result","subtype":"error_during_execution","is_error":true,"result":null}`, true, "Interrupted."},
		{`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"boom"}`, true, "Error: boom"},
	} {
		item, ok := a.chatItem(1, notes.AgentEvent{ID: 5, Type: evResult, Payload: c.payload})
		if ok != c.ok || (ok && (item.Kind != "assistant" || item.Text != c.text)) {
			t.Errorf("%s: item = %+v, ok = %v; want %q, %v", c.payload, item, ok, c.text, c.ok)
		}
	}
}

func TestChatItemWakeIsNotice(t *testing.T) {
	a := NewApp()
	b, _ := json.Marshal(userMessage{Text: wakeMessage([]string{"#3 done from agent 2: API ready", "agent 4 exited (crashed)"})})
	item, ok := a.chatItem(1, notes.AgentEvent{ID: 5, Type: evUserMessage, Payload: string(b)})
	if !ok || item.Kind != "notice" || item.Text != "#3 done from agent 2: API ready\nagent 4 exited (crashed)" {
		t.Fatalf("wake item = %+v", item)
	}
	b, _ = json.Marshal(userMessage{Text: "hello [Fragile]"})
	if item, _ := a.chatItem(1, notes.AgentEvent{ID: 6, Type: evUserMessage, Payload: string(b)}); item.Kind != "user" {
		t.Fatalf("user item = %+v", item)
	}
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// stdinLines returns the lines a recordingOrchestrator wrote to path.
func stdinLines(path string) []string {
	b, _ := os.ReadFile(path)
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")[:strings.Count(string(b), "\n")]
}

// recordingOrchestrator appends every stdin line to path and ends each turn with a result.
func recordingOrchestrator(path string) string {
	return `while IFS= read -r line; do
  printf '%s\n' "$line" >> '` + path + `'
  printf '{"type":"result","subtype":"success","is_error":false,"result":"ok"}\n'
done`
}

// Attachments go to the orchestrator as content blocks after the text, are
// stored on disk, show in the chat, and load back as data URLs.
func TestSendMessageWithAttachments(t *testing.T) {
	in := filepath.Join(t.TempDir(), "stdin.jsonl")
	a, _ := newTestApp(t, t.TempDir(), recordingOrchestrator(in))
	se, err := a.CreateSession("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	png, pdf := "\x89PNG\r\n\x1a\nfake", "%PDF-1.4 fake"
	err = a.SendMessage(se.ID, "look at these", []Attachment{
		{Name: "../../shot.png", MediaType: "image/png", Data: b64(png)},
		{Name: `dir\notes "v2".md`, MediaType: "text/plain", Data: b64("# hi\nthere")},
		{Name: "doc.pdf", MediaType: "application/pdf", Data: b64(pdf)},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stdin line", func() bool { return len(stdinLines(in)) == 1 })
	var msg struct {
		Type    string `json:"type"`
		Message struct {
			Role    string           `json:"role"`
			Content []map[string]any `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(stdinLines(in)[0]), &msg); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"type": "text", "text": "look at these"},
		{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": b64(png)}},
		{"type": "text", "text": "<file name=\"notes _v2_.md\">\n# hi\nthere\n</file>"},
		{"type": "document", "source": map[string]any{"type": "base64", "media_type": "application/pdf", "data": b64(pdf)}},
	}
	if got, _ := json.Marshal(msg.Message.Content); msg.Type != "user" || msg.Message.Role != "user" || string(got) != mustJSON(t, want) {
		t.Fatalf("stdin message = %s\nwant content %s", stdinLines(in)[0], mustJSON(t, want))
	}

	var item ChatItem
	for _, c := range snapshot(t, a, se.ID).Chat {
		if c.Kind == "user" {
			item = c
		}
	}
	wantInfo := []AttachmentInfo{
		{Name: "shot.png", MediaType: "image/png", Size: int64(len(png))},
		{Name: "notes _v2_.md", MediaType: "text/plain", Size: 10},
		{Name: "doc.pdf", MediaType: "application/pdf", Size: int64(len(pdf))},
	}
	if item.Text != "look at these" || mustJSON(t, item.Attachments) != mustJSON(t, wantInfo) {
		t.Fatalf("chat item = %+v", item)
	}
	if b, err := os.ReadFile(filepath.Join(a.attachDir, fmt.Sprint(se.ID), fmt.Sprint(item.ID), "0-shot.png")); err != nil || string(b) != png {
		t.Fatalf("stored image = %q, %v", b, err)
	}
	if m, _ := filepath.Glob(filepath.Join(a.attachDir, fmt.Sprint(se.ID), fmt.Sprint(item.ID), "*")); len(m) != 3 {
		t.Fatalf("stored files = %v", m)
	}

	if url, err := a.GetAttachment(se.ID, item.ID, 0); err != nil || url != "data:image/png;base64,"+b64(png) {
		t.Fatalf("GetAttachment = %.80q, %v", url, err)
	}
	if url, err := a.GetAttachment(se.ID, item.ID, 2); err != nil || url != "data:application/pdf;base64,"+b64(pdf) {
		t.Fatalf("GetAttachment pdf = %.80q, %v", url, err)
	}
	for _, idx := range []int{-1, 3} {
		if _, err := a.GetAttachment(se.ID, item.ID, idx); err == nil {
			t.Errorf("GetAttachment index %d accepted", idx)
		}
	}
	other, err := a.CreateSession("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetAttachment(other.ID, item.ID, 0); err == nil {
		t.Error("GetAttachment read another session's message")
	}
	if _, err := a.GetAttachment(se.ID, item.ID+1, 0); err == nil { // the result row after it
		t.Error("GetAttachment read a non-user_message row")
	}

	// Attachments without text: only the file blocks go out.
	if err := a.SendMessage(se.ID, "", []Attachment{{Name: "only.txt", MediaType: "text/plain; charset=utf-8", Data: b64("x")}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "second stdin line", func() bool { return len(stdinLines(in)) == 2 })
	if !strings.Contains(stdinLines(in)[1], `"content":[{"text":"\u003cfile name=\"only.txt\"\u003e\nx\n\u003c/file\u003e","type":"text"}]`) {
		t.Fatalf("attachment-only message = %s", stdinLines(in)[1])
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Invalid attachments are rejected before anything is sent, naming the file.
func TestSendMessageAttachmentValidation(t *testing.T) {
	in := filepath.Join(t.TempDir(), "stdin.jsonl")
	a, _ := newTestApp(t, t.TempDir(), recordingOrchestrator(in))
	se, err := a.CreateSession("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	txt := func(name, s string) Attachment { return Attachment{Name: name, MediaType: "text/plain", Data: b64(s)} }
	many := make([]Attachment, maxAttachments+1)
	for i := range many {
		many[i] = txt(fmt.Sprintf("f%d.txt", i), "x")
	}
	bigPDF := b64(strings.Repeat("p", 8<<20))
	for _, c := range []struct {
		name string
		text string
		atts []Attachment
		want string
	}{
		{"empty", " ", nil, "must not be empty"},
		{"bad type", "x", []Attachment{{Name: "a.exe", MediaType: "application/octet-stream", Data: b64("MZ")}}, `"a.exe": unsupported file type`},
		{"big image", "x", []Attachment{{Name: "big.png", MediaType: "image/png", Data: b64(strings.Repeat("i", maxImageBytes+1))}}, `"big.png" is too large`},
		{"big text", "x", []Attachment{txt("big.txt", strings.Repeat("t", maxTextBytes+1))}, `"big.txt" is too large`},
		{"too many", "x", many, "too many attachments"},
		{"total", "x", []Attachment{
			{Name: "1.pdf", MediaType: "application/pdf", Data: bigPDF},
			{Name: "2.pdf", MediaType: "application/pdf", Data: bigPDF},
			{Name: "3.pdf", MediaType: "application/pdf", Data: bigPDF},
		}, `"3.pdf" (at most 20 MB in total)`},
		{"bad base64", "x", []Attachment{{Name: "b.png", MediaType: "image/png", Data: "!!not base64!!"}}, `"b.png": invalid base64`},
		{"bad utf8", "x", []Attachment{{Name: "bin.txt", MediaType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe, 0x00})}}, `"bin.txt" is not valid UTF-8`},
		{"empty file", "x", []Attachment{txt("e.txt", "")}, `"e.txt" is empty`},
		{"path in name", "x", []Attachment{{Name: "../../etc/x.exe", MediaType: "application/x-msdownload", Data: b64("MZ")}}, `"x.exe": unsupported`},
	} {
		err := a.SendMessage(se.ID, c.text, c.atts)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	if s := snapshot(t, a, se.ID); len(s.Agents) != 0 || len(s.Chat) != 0 {
		t.Fatalf("rejected messages started or reached the orchestrator: %+v", s)
	}
	if _, err := os.Stat(a.attachDir); !os.IsNotExist(err) {
		t.Fatalf("attachments dir created by rejected messages: %v", err)
	}
}

// At startup, an agent a hard-killed app left running is killed if (and only if) its
// command line carries this agent's MCP config.
func TestStartupKillsOrphans(t *testing.T) {
	dir := t.TempDir()
	a, _ := newTestApp(t, dir, "")
	se, _ := a.CreateSession("", t.TempDir())
	orphan, _ := a.store.CreateAgent(se.ID, "orchestrator", 0, 0)
	bystander, _ := a.store.CreateAgent(se.ID, "subagent", orphan.ID, 0)
	start := func(ag notes.Agent, args ...string) (pid int, died chan struct{}) {
		cmd := exec.Command("sh", append([]string{"-c", "sleep 300; :", "x"}, args...)...) // "; :" keeps sh from exec'ing sleep, which would drop the args
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		died = make(chan struct{})
		go func() { cmd.Wait(); close(died) }()
		pid = cmd.Process.Pid
		t.Cleanup(func() { syscall.Kill(-pid, syscall.SIGKILL); <-died })
		a.store.SetAgentProcess(se.ID, ag.ID, pid, "")
		return pid, died
	}
	cfg := filepath.Join(dir, "agents", fmt.Sprintf("agent-%d.mcp.json", orphan.ID))
	_, victimDied := start(orphan, cfg)
	other, _ := start(bystander, "unrelated") // a reused pid: same pid, different process
	a.close()

	newTestApp(t, dir, "")
	select {
	case <-victimDied:
	case <-time.After(10 * time.Second):
		t.Fatal("orphan still running")
	}
	if err := syscall.Kill(other, 0); err != nil {
		t.Fatalf("unrelated process was killed: %v", err)
	}
}

func TestMergePath(t *testing.T) {
	if got := mergePath("/opt/homebrew/bin:/usr/bin", "/usr/bin:/bin::"); got != "/opt/homebrew/bin:/usr/bin:/bin" {
		t.Fatalf("mergePath = %q", got)
	}
}

func TestEscalationAnswerFlow(t *testing.T) {
	a, ev := newTestApp(t, t.TempDir(), echoOrchestrator)
	se := startSession(t, a, "build it")
	waitFor(t, "first turn done", func() bool {
		return snapshot(t, a, se.ID).Session.Status == "done" && hasChat(a, se.ID, "assistant: echo: ")
	})

	// The orchestrator escalates through its real MCP endpoint.
	orch := snapshot(t, a, se.ID).Agents[0]
	full, _ := a.store.GetAgent(se.ID, orch.ID)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(context.Background(),
		&mcp.StreamableClientTransport{Endpoint: "http://" + a.addr + "/mcp/" + full.Token}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "escalate_to_user",
		Arguments: map[string]any{"question": "Postgres or SQLite?", "context": "small app"}})
	if err != nil || res.IsError {
		t.Fatalf("escalate: %v %+v", err, res)
	}
	if txt := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(txt, "Their answer will arrive as a new user message") {
		t.Fatalf("tool result = %q", txt)
	}
	waitFor(t, "needs_you", func() bool { return snapshot(t, a, se.ID).Session.Status == "needs_you" })
	s := snapshot(t, a, se.ID)
	last := s.Chat[len(s.Chat)-1]
	if len(s.Escalations) != 1 || last.Kind != "escalation" || last.Escalation.Question != "Postgres or SQLite?" || last.Escalation.Status != "open" {
		t.Fatalf("snapshot after escalate: %+v / chat %+v", s.Escalations, s.Chat)
	}
	waitFor(t, "escalation events", func() bool { return len(ev.named(notes.EventEscalation)) == 1 && len(ev.named(eventChatItem)) >= 3 })

	if err := a.AnswerEscalation(s.Escalations[0].ID, "  "); err == nil {
		t.Error("blank answer accepted")
	}
	if err := a.AnswerEscalation(s.Escalations[0].ID, "SQLite"); err != nil {
		t.Fatal(err)
	}
	// The orchestrator received the answer with the question for context ...
	waitFor(t, "orchestrator echoes the answer", func() bool {
		return hasChat(a, se.ID, "Answer to your escalation #1.") && hasChat(a, se.ID, "Postgres or SQLite?") && hasChat(a, se.ID, "The user's answer: SQLite")
	})
	// ... the escalation is answered in place (same chat row), the badge cleared, nothing duplicated as a user message.
	waitFor(t, "badge cleared", func() bool { return snapshot(t, a, se.ID).Session.Status == "done" })
	s = snapshot(t, a, se.ID)
	var esc ChatItem
	for _, c := range s.Chat {
		if c.Kind == "escalation" {
			esc = c
		}
		if c.Kind == "user" && strings.Contains(c.Text, "SQLite") {
			t.Errorf("answer shown as a user message: %+v", c)
		}
	}
	if len(s.Escalations) != 0 || esc.Escalation == nil || esc.Escalation.Status != "answered" || esc.Escalation.Answer != "SQLite" {
		t.Fatalf("after answer: open=%v item=%+v", s.Escalations, esc)
	}
	got := strings.Join(ev.statuses(se.ID), ",")
	if !strings.Contains(got, "needs_you,working") {
		t.Errorf("status events = %s, want needs_you then working", got)
	}
	if err := a.AnswerEscalation(esc.Escalation.ID, "again"); err == nil {
		t.Error("answered an escalation twice")
	}
}

// An answer that cannot be delivered leaves the escalation open, to be answered again.
func TestUndeliveredAnswerReopensEscalation(t *testing.T) {
	// Takes the first message, then closes its stdin (so writes fail) but keeps running.
	a, _ := newTestApp(t, t.TempDir(), `IFS= read -r line
exec 0<&-
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"deaf now"}]}}\n'
printf '{"type":"result","subtype":"success","is_error":false,"result":"ok"}\n'
sleep 300`)
	se := startSession(t, a, "task")
	waitFor(t, "reply", func() bool { return hasChat(a, se.ID, "assistant: deaf now") })
	orch := snapshot(t, a, se.ID).Agents[0]
	e, err := a.store.CreateEscalation(se.ID, orch.ID, "Postgres or SQLite?", "small app")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AnswerEscalation(e.ID, "SQLite"); err == nil {
		t.Fatal("answer to a deaf orchestrator succeeded")
	}
	if got, err := a.store.FindEscalation(e.ID); err != nil || got.Status != "open" || got.Answer != "" {
		t.Fatalf("escalation after failed delivery = %+v, %v", got, err)
	}
}

func TestUserNotesWakeAgents(t *testing.T) {
	a, ev := newTestApp(t, t.TempDir(), echoOrchestrator)
	se := startSession(t, a, "task")
	changed := a.log.Changed(se.ID)
	n, err := a.AddNote(se.ID, "decision", "use tabs")
	if err != nil || n.AuthorID != 0 || n.Status != "open" {
		t.Fatalf("note = %+v, %v", n, err)
	}
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("user note did not wake wait_for_notes")
	}
	if _, err := a.AddNote(se.ID, "nonsense", "x"); err == nil {
		t.Error("bad type accepted")
	}
	u, err := a.UpdateNote(se.ID, n.ID, "use spaces", "resolved")
	if err != nil || u.Content != "use spaces" || u.Status != "resolved" || u.AuthorID != 0 {
		t.Fatalf("updated = %+v, %v", u, err)
	}
	if _, err := a.UpdateNote(se.ID, n.ID, "", ""); err == nil {
		t.Error("empty update accepted")
	}
	if got := snapshot(t, a, se.ID).Notes; len(got) != 1 || got[0].Content != "use spaces" {
		t.Fatalf("snapshot notes = %+v", got)
	}
	waitFor(t, "note events", func() bool {
		return len(ev.named(notes.EventNotePosted)) == 1 && len(ev.named(notes.EventNoteUpdated)) == 1
	})
	b, _ := json.Marshal(n)
	if !strings.Contains(string(b), `"author_agent_id":0`) {
		t.Errorf("user note JSON = %s", b)
	}
}

// A previous run's running agents are recorded as crashed on start, their
// sessions recomputed, and history stays viewable.
func TestStartupRecovery(t *testing.T) {
	dir := t.TempDir()
	a, _ := newTestApp(t, dir, echoOrchestrator)
	se := startSession(t, a, "old session")
	waitFor(t, "reply", func() bool {
		return hasChat(a, se.ID, "assistant: echo: ") && snapshot(t, a, se.ID).Session.Status == "done"
	})
	// Leave a sub-agent "running" with a working task, as an app crash would.
	orch := snapshot(t, a, se.ID).Agents[0]
	task, _ := a.store.CreateTask(se.ID, "t", "d")
	sub, _ := a.store.CreateAgent(se.ID, "subagent", orch.ID, task.ID)
	a.store.SetTaskAgent(se.ID, task.ID, sub.ID)
	a.store.AppendAgentEvent(se.ID, sub.ID, "assistant_text", `{"text":"half done"}`)
	a.store.SetSessionStatus(se.ID, "working")
	a.close()

	// Simulate that the orchestrator was also still running when the app died.
	st, _ := notes.OpenStore(dir + "/fragile.db")
	st.SetAgentStatus(se.ID, orch.ID, "running", nil)
	st.Close()

	b, _ := newTestApp(t, dir, "")
	s := snapshot(t, b, se.ID)
	if s.Session.Status != "done" || len(s.Agents) != 2 {
		t.Fatalf("recovered session = %+v agents %d", s.Session, len(s.Agents))
	}
	for _, ag := range s.Agents {
		if ag.Status != "crashed" || ag.ExitedAt == "" {
			t.Errorf("agent %d = %+v, want crashed", ag.ID, ag)
		}
	}
	if len(s.Tasks) != 1 || s.Tasks[0].Status != "blocked" {
		t.Fatalf("tasks = %+v", s.Tasks)
	}
	if !hasChat(b, se.ID, "user: old session") || !hasChat(b, se.ID, "assistant: echo: ") {
		t.Fatalf("chat lost: %v", chatTexts(s))
	}
	if evs, err := b.GetAgentEvents(sub.ID, 0, 10); err != nil || len(evs) != 1 {
		t.Fatalf("sub-agent events = %+v, %v", evs, err)
	}
	if list, _ := b.ListSessions(); len(list) != 1 || list[0].ID != se.ID {
		t.Fatalf("sessions = %+v", list)
	}
	if err := b.SendMessage(se.ID, "hi", nil); err == nil {
		t.Error("message to a past session accepted")
	}
}

// A second instance on the same data dir refuses to start rather than crash and
// kill the first one's agents; once the first has closed, the dir is free again.
func TestSecondInstanceRefused(t *testing.T) {
	dir := t.TempDir()
	a, _ := newTestApp(t, dir, echoOrchestrator)
	se := startSession(t, a, "task")
	waitFor(t, "reply", func() bool { return hasChat(a, se.ID, "assistant: echo: ") })

	b := NewApp()
	err := b.open(dir, (&events{}).emit)
	if err == nil {
		b.close()
		t.Fatal("second instance opened the same data dir")
	}
	if !strings.Contains(err.Error(), "another Fragile instance") {
		t.Fatalf("err = %v", err)
	}
	if s := snapshot(t, a, se.ID); s.Agents[0].Status != "running" || !a.runner.Running(s.Agents[0].ID) {
		t.Fatalf("first instance's orchestrator disturbed: %+v", s.Agents[0])
	}
	a.close()
	newTestApp(t, dir, "")
}

func TestDeriveStatus(t *testing.T) {
	for _, tc := range []struct {
		name       string
		esc        int
		orch, busy bool
		subs       int
		want       string
	}{
		{"idle", 0, true, false, 0, "done"},
		{"mid-turn", 0, true, true, 0, "working"},
		{"sub-agent running while orchestrator idle", 0, true, false, 2, "working"},
		{"escalation beats working", 1, true, true, 3, "needs_you"},
		{"escalation with a live orchestrator", 2, true, false, 0, "needs_you"},
		{"escalation nobody can answer", 1, false, false, 0, "done"},
		{"dead orchestrator is never mid-turn", 0, false, true, 0, "done"},
		{"orphaned sub-agents still working", 0, false, false, 1, "working"},
	} {
		if got := deriveStatus(tc.esc, tc.orch, tc.busy, tc.subs); got != tc.want {
			t.Errorf("%s: deriveStatus = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o755) }

func TestGetAgentEventTail(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), "")
	sess, err := a.store.CreateSession("tail")
	if err != nil {
		t.Fatal(err)
	}
	ag, err := a.store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := a.GetAgentEventTail(ag.ID, 2000)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty tail: %+v, %v", empty, err)
	}
	for i := 0; i < 5; i++ {
		if _, err := a.store.AppendAgentEvent(sess.ID, ag.ID, "output", fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	tail, err := a.GetAgentEventTail(ag.ID, 2)
	if err != nil || len(tail) != 2 || tail[0].Payload != "3" || tail[1].Payload != "4" {
		t.Fatalf("tail: %+v, %v", tail, err)
	}
	page, err := a.GetAgentEvents(ag.ID, 0, 2)
	if err != nil || len(page) != 2 || page[0].Payload != "0" {
		t.Fatalf("old paging changed: %+v, %v", page, err)
	}
	if _, err := a.GetAgentEventTail(99999, 2000); !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("missing agent: %v", err)
	}
}
