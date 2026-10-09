package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// holdingOrchestrator records every stdin line to path (a sub-agent's to path.sub)
// and ends each turn with a result, except for a message containing HOLD: that
// turn stays open until the next message.
func holdingOrchestrator(path string) string {
	return `f='` + path + `'; case "$*" in *--setting-sources*) f="$f.sub" ;; esac
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$f"
  printf '{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}\n'
  case "$line" in *HOLD*) ;; *) printf '{"type":"result","subtype":"success","is_error":false,"result":"ok"}\n' ;; esac
done`
}

// wakeSetup starts a session whose orchestrator has finished its first turn, plus one sub-agent row.
func wakeSetup(t *testing.T) (*App, notes.Session, notes.Agent, notes.Agent, string) {
	t.Helper()
	in := filepath.Join(t.TempDir(), "stdin.jsonl")
	a, _ := newTestApp(t, t.TempDir(), holdingOrchestrator(in))
	se := startSession(t, a, "task")
	waitFor(t, "first turn", func() bool { return len(stdinLines(in)) == 1 && !a.isBusy(se.ID) })
	orch := snapshot(t, a, se.ID).Agents[0]
	sub, err := a.store.CreateAgent(se.ID, "subagent", orch.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return a, se, orch, sub, in
}

func (a *App) isBusy(sessionID int64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.busy[sessionID]
}

// post posts a note as author (0 = the user) the way the notes server does.
func post(t *testing.T, a *App, sessionID, author int64, typ, content string) notes.Note {
	t.Helper()
	board, _ := a.store.SessionBoard(sessionID)
	n, err := a.store.PostNote(sessionID, board, author, typ, content)
	if err != nil {
		t.Fatal(err)
	}
	a.log.Write(notes.EventNotePosted, sessionID, author, n)
	return n
}

// wakes returns the wake messages the orchestrator received (raw stdin lines).
func wakes(path string) []string {
	var out []string
	for _, l := range stdinLines(path) {
		if strings.Contains(l, "[Fragile] Board update") {
			out = append(out, l)
		}
	}
	return out
}

// A burst of events becomes one message, sent once the orchestrator is idle, and shown in the chat.
func TestWakeDebounceBatches(t *testing.T) {
	a, se, _, sub, in := wakeSetup(t)
	d := post(t, a, se.ID, sub.ID, "done", "Built the API\nmore detail")
	b := post(t, a, se.ID, sub.ID, "blocker", "need a token")
	a.log.Write(notes.EventAgentStatusChanged, se.ID, sub.ID, map[string]any{"role": "subagent", "status": "crashed"})
	waitFor(t, "wake", func() bool { return len(wakes(in)) == 1 })
	w := wakes(in)[0]
	for _, want := range []string{
		"#" + itoa(d.ID) + " done from agent " + itoa(sub.ID) + ": Built the API\\n",
		"#" + itoa(b.ID) + " blocker from agent " + itoa(sub.ID) + ": need a token",
		"agent " + itoa(sub.ID) + " exited (crashed)",
		"Read the board for details.",
	} {
		if !strings.Contains(w, want) {
			t.Errorf("wake %s lacks %q", w, want)
		}
	}
	if strings.Contains(w, "more detail") {
		t.Errorf("wake carries more than the first line: %s", w)
	}
	waitFor(t, "chat shows the wake", func() bool { return hasChat(a, se.ID, "notice: #") })
	time.Sleep(wakeDebounce + 500*time.Millisecond)
	if n := len(wakes(in)); n != 1 {
		t.Fatalf("%d wake messages, want 1", n)
	}
}

// Events during a turn are held and sent when it ends.
func TestWakeHeldWhileBusy(t *testing.T) {
	a, se, orch, sub, in := wakeSetup(t)
	if err := a.deliver(orch, "HOLD", nil, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "held turn", func() bool { return len(stdinLines(in)) == 2 })
	post(t, a, se.ID, sub.ID, "question", "which db?")
	time.Sleep(wakeDebounce + 500*time.Millisecond)
	if len(wakes(in)) != 0 || !a.isBusy(se.ID) {
		t.Fatalf("woke mid-turn: %v", stdinLines(in))
	}
	if err := a.deliver(orch, "end the turn", nil, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "wake after the turn", func() bool { return len(wakes(in)) == 1 })
	if w := wakes(in)[0]; !strings.Contains(w, "question from agent "+itoa(sub.ID)+": which db?") {
		t.Fatalf("wake = %s", w)
	}
}

// decision and heads_up notes, the orchestrator's own notes and its own exit do not wake.
func TestWakeIgnores(t *testing.T) {
	a, se, orch, sub, in := wakeSetup(t)
	dec := post(t, a, se.ID, sub.ID, "decision", "use tabs")
	hu := post(t, a, se.ID, sub.ID, "heads_up", "renamed X")
	own := post(t, a, se.ID, orch.ID, "question", "anyone?")
	a.log.Write(notes.EventAgentStatusChanged, se.ID, sub.ID, map[string]any{"role": "subagent", "status": "running"})
	user := post(t, a, se.ID, 0, "done", "from the UI") // the user is not the orchestrator: wakes
	waitFor(t, "wake", func() bool { return len(wakes(in)) == 1 })
	w := wakes(in)[0]
	for _, n := range []notes.Note{dec, hu, own} {
		if strings.Contains(w, "#"+itoa(n.ID)+" ") {
			t.Errorf("wake includes ignored note %+v: %s", n, w)
		}
	}
	if strings.Contains(w, "exited") || !strings.Contains(w, "#"+itoa(user.ID)+" done from the user: from the UI") {
		t.Errorf("wake = %s", w)
	}
}

// Nothing is sent to a stopped orchestrator.
func TestWakeDroppedWhenNotRunning(t *testing.T) {
	a, se, _, sub, in := wakeSetup(t)
	if err := a.StopSession(se.ID); err != nil {
		t.Fatal(err)
	}
	post(t, a, se.ID, sub.ID, "done", "finished")
	time.Sleep(wakeDebounce + 500*time.Millisecond)
	if len(wakes(in)) != 0 {
		t.Fatalf("woke a stopped orchestrator: %v", stdinLines(in))
	}
	a.wq.mu.Lock()
	defer a.wq.mu.Unlock()
	if len(a.wq.m) != 0 {
		t.Fatalf("queue kept: %+v", a.wq.m)
	}
}

// The one-shot runner (no stdin to wake) keeps the wait loop: no messages, old prompt.
func TestWakeOneShotUnaffected(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), "")
	a.runner.Interactive = false
	se, _ := a.store.CreateSession("one-shot")
	orch, _ := a.store.CreateAgent(se.ID, "orchestrator", 0, 0)
	sub, _ := a.store.CreateAgent(se.ID, "subagent", orch.ID, 0)
	a.queueWake(notes.Event{Event: notes.EventNotePosted, SessionID: se.ID, AgentID: sub.ID,
		Payload: notes.Note{ID: 1, AuthorID: sub.ID, Type: "done", Content: "finished"}})
	a.wq.mu.Lock()
	queued := len(a.wq.m)
	a.wq.mu.Unlock()
	if queued != 0 {
		t.Fatal("one-shot session queued a wake")
	}
	if p := notes.OrchestratorPrompt("/w", false, ""); !strings.Contains(p, "**Wait loop.**") || strings.Contains(p, "[Fragile]") {
		t.Fatal("one-shot prompt changed")
	}
}

// spawnSub launches a sub-agent on the wakeSetup fake; its stdin lines go to in+".sub".
func spawnSub(t *testing.T, a *App, se notes.Session, orch notes.Agent, task, in string) notes.Agent {
	t.Helper()
	sub, err := a.runner.SpawnSubagent(se.ID, orch.ID, "", task, "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "task on stdin", func() bool { return len(stdinLines(in+".sub")) == 1 })
	return sub
}

// A sub-agent ending its turn without a done note is idle: the orchestrator is
// told, a note from someone else wakes it, its own do not. Stopping drops its queue.
func TestWakeSubagentIdle(t *testing.T) {
	a, se, orch, _, in := wakeSetup(t)
	sub := spawnSub(t, a, se, orch, "task", in)
	idle := "agent " + itoa(sub.ID) + " idle, waiting (no done note)"
	waitFor(t, "orchestrator told", func() bool { w := wakes(in); return len(w) == 1 && strings.Contains(w[0], idle) })
	post(t, a, se.ID, sub.ID, "heads_up", "own note")
	d := post(t, a, se.ID, orch.ID, "decision", "use tabs\nmore")
	waitFor(t, "sub-agent woken", func() bool { return len(wakes(in+".sub")) == 1 })
	if w := wakes(in + ".sub")[0]; !strings.Contains(w, "#"+itoa(d.ID)+" decision from the orchestrator: use tabs\\n") ||
		strings.Contains(w, "own note") || strings.Contains(w, "more") {
		t.Fatalf("sub-agent wake = %s", w)
	}
	waitFor(t, "idle again", func() bool { w := wakes(in); return len(w) == 2 && strings.Contains(w[1], idle) })
	if err := a.StopSession(se.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "queue dropped", func() bool {
		a.wq.mu.Lock()
		defer a.wq.mu.Unlock()
		return len(a.wq.subs) == 0
	})
}

// Notes posted while a sub-agent is mid-turn are held until its turn ends.
func TestWakeSubagentHeldWhileBusy(t *testing.T) {
	a, se, orch, _, in := wakeSetup(t)
	sub := spawnSub(t, a, se, orch, "HOLD", in)
	post(t, a, se.ID, orch.ID, "decision", "the answer")
	time.Sleep(wakeDebounce + 500*time.Millisecond)
	if len(wakes(in+".sub")) != 0 || len(wakes(in)) != 0 {
		t.Fatalf("woke mid-turn: %v / %v", stdinLines(in+".sub"), stdinLines(in))
	}
	if err := a.runner.SendUser(sub.ID, "end the turn"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "wake after the turn", func() bool { return len(wakes(in+".sub")) == 1 })
	if w := wakes(in + ".sub")[0]; !strings.Contains(w, "decision from the orchestrator: the answer") {
		t.Fatalf("wake = %s", w)
	}
}

// A turn that ends with the sub-agent's done note posted closes its stdin: it exits, never idle.
func TestWakeSubagentDoneExits(t *testing.T) {
	a, se, orch, _, in := wakeSetup(t)
	sub := spawnSub(t, a, se, orch, "HOLD", in)
	post(t, a, se.ID, sub.ID, "done", "finished")
	if err := a.runner.SendUser(sub.ID, "end the turn"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "exit", func() bool {
		ag, err := a.store.GetAgent(se.ID, sub.ID)
		return err == nil && ag.Status == "exited"
	})
	waitFor(t, "orchestrator told", func() bool { w := wakes(in); return len(w) > 0 && strings.Contains(w[len(w)-1], "exited") })
	for _, w := range wakes(in) {
		if strings.Contains(w, "idle") {
			t.Fatalf("done sub-agent reported idle: %s", w)
		}
	}
}

func TestWakeMessageCap(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = strings.Repeat("x", 200)
	}
	m := wakeMessage(lines)
	if len(m) > wakeMsgMax+200 || !strings.HasPrefix(m, wakePrefix+"\n- x") || !strings.Contains(m, "more\n"+wakeSuffix) {
		t.Fatalf("message (%d bytes) = %q", len(m), m)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
