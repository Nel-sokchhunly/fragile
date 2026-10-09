package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Nel-sokchhunly/fragile/notes"
)

func lastLine(path string) string {
	l := stdinLines(path)
	if len(l) == 0 {
		return ""
	}
	return l[len(l)-1]
}

func wantErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want one containing %q", err, want)
	}
}

// Pause holds wakes, Resume sends them with the resume message, Finish on an
// idle agent records it stopped, and a stopped one resumes its conversation on the same id.
func TestAgentPauseResumeFinish(t *testing.T) {
	a, se, orch, _, in := wakeSetup(t)
	sub := spawnSub(t, a, se, orch, "HOLD", in)
	activity := func() AgentActivity { return snapshot(t, a, se.ID).Activity[sub.ID] }
	if got := activity(); got != (AgentActivity{Busy: true, Live: true}) {
		t.Fatalf("activity of a new sub-agent = %+v", got)
	}
	wantErr(t, a.PauseAgent(orch.ID), "session controls")
	wantErr(t, a.ResumeAgent(99999), "not found")

	if err := a.PauseAgent(sub.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "paused and idle", func() bool { return activity() == AgentActivity{Paused: true, Live: true} })
	waitFor(t, "orchestrator told", func() bool {
		w := wakes(in)
		return len(w) > 0 && strings.Contains(w[len(w)-1], "agent "+itoa(sub.ID)+" paused by the user")
	})
	wantErr(t, a.PauseAgent(sub.ID), "already paused")
	post(t, a, se.ID, orch.ID, "decision", "the answer")
	time.Sleep(wakeDebounce + 500*time.Millisecond)
	if len(wakes(in+".sub")) != 0 {
		t.Fatalf("paused sub-agent woken: %v", stdinLines(in+".sub"))
	}

	if err := a.ResumeAgent(sub.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "resume message", func() bool { return strings.Contains(lastLine(in+".sub"), resumedMessage) })
	if l := lastLine(in + ".sub"); !strings.Contains(l, "decision from the orchestrator: the answer") {
		t.Fatalf("resume message = %s", l)
	}
	waitFor(t, "idle after the resumed turn", func() bool { return activity() == AgentActivity{Live: true} })
	wantErr(t, a.PauseAgent(sub.ID), "idle")

	if err := a.FinishAgent(sub.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stopped", func() bool { ag, _ := a.store.GetAgent(se.ID, sub.ID); return ag.Status == "stopped" })
	waitFor(t, "orchestrator told it exited", func() bool {
		w := wakes(in)
		return strings.Contains(w[len(w)-1], "agent "+itoa(sub.ID)+" exited (stopped)")
	})
	if _, ok := snapshot(t, a, se.ID).Activity[sub.ID]; ok {
		t.Fatal("activity kept for a stopped agent")
	}
	wantErr(t, a.FinishAgent(sub.ID), "not running")
	wantErr(t, a.ResumeAgent(sub.ID), "no recorded Claude Code conversation")
	agents := len(snapshot(t, a, se.ID).Agents)

	if _, err := a.store.AppendAgentEvent(se.ID, sub.ID, evSystem, `{"type":"system","subtype":"init","session_id":"claude-sub"}`); err != nil {
		t.Fatal(err)
	}
	if err := a.ResumeAgent(sub.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "resumed process", func() bool { return strings.Contains(lastLine(in+".sub"), resumedAfterStopMessage) })
	if ag, _ := a.store.GetAgent(se.ID, sub.ID); ag.Status != "running" {
		t.Fatalf("resumed agent = %+v", ag)
	}
	waitFor(t, "idle again", func() bool { return activity() == AgentActivity{Live: true} })
	if err := a.ResumeAgent(sub.ID); err != nil { // a nudge
		t.Fatal(err)
	}
	waitFor(t, "nudge", func() bool {
		l := stdinLines(in + ".sub")
		return len(l) >= 2 && strings.Contains(l[len(l)-1], resumedMessage) && !strings.Contains(l[len(l)-1], "after it stopped")
	})
	if n := len(snapshot(t, a, se.ID).Agents); n != agents {
		t.Fatalf("%d agents after resuming, want the same %d", n, agents)
	}

	post(t, a, se.ID, sub.ID, "done", "finished")
	a.runner.StopSession(se.ID)
	wantErr(t, a.ResumeAgent(sub.ID), "done note")
}

// A user message reaches a live sub-agent, is recorded in its events, and the
// orchestrator gets a heads_up note; a stopped agent and empty text are rejected.
func TestMessageAgent(t *testing.T) {
	a, se, orch, _, in := wakeSetup(t)
	sub := spawnSub(t, a, se, orch, "HOLD", in)
	wantErr(t, a.MessageAgent(sub.ID, "  "), "must not be empty")
	wantErr(t, a.MessageAgent(orch.ID, "hi"), "session controls")

	if err := a.MessageAgent(sub.ID, "use the other API"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "message on stdin", func() bool { return strings.Contains(lastLine(in+".sub"), "use the other API") })
	evs, err := a.store.ListAgentEventsOfType(se.ID, sub.ID, evUserMessage)
	if err != nil || len(evs) == 0 || !strings.Contains(evs[len(evs)-1].Payload, "use the other API") {
		t.Fatalf("user_message events = %v, %v", evs, err)
	}
	board, _ := a.store.SessionBoard(se.ID)
	ns, err := a.store.ListNotes(se.ID, board, notes.NoteFilter{Type: "heads_up"})
	if err != nil || len(ns) == 0 || !strings.Contains(ns[len(ns)-1].Content, "User messaged sub-agent #"+itoa(sub.ID)) {
		t.Fatalf("heads_up notes = %v, %v", ns, err)
	}

	if err := a.FinishAgent(sub.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stopped", func() bool { ag, _ := a.store.GetAgent(se.ID, sub.ID); return ag.Status == "stopped" })
	wantErr(t, a.MessageAgent(sub.ID, "again"), "not running")
}

// Restart stops a running sub-agent and starts a fresh one on its task (own
// task record with the same text); the orchestrator gets a heads_up note.
func TestRestartAgent(t *testing.T) {
	a, se, orch, _, in := wakeSetup(t)
	sub := spawnSub(t, a, se, orch, "HOLD", in)
	wantErr(t, func() error { _, err := a.RestartAgent(orch.ID); return err }(), "session controls")

	id, err := a.RestartAgent(sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if id == sub.ID {
		t.Fatal("restart reused the agent id")
	}
	old, _ := a.store.GetAgent(se.ID, sub.ID)
	if old.Status != "stopped" {
		t.Fatalf("old agent status = %q, want stopped", old.Status)
	}
	n, err := a.store.GetAgent(se.ID, id)
	if err != nil || n.Status != "running" || n.ParentID != orch.ID || n.TaskID == 0 || n.TaskID == old.TaskID {
		t.Fatalf("new agent = %+v, %v", n, err)
	}
	ot, _ := a.store.GetTask(se.ID, old.TaskID)
	nt, err := a.store.GetTask(se.ID, n.TaskID)
	if err != nil || nt.Description != ot.Description || nt.Title != ot.Title || nt.AgentID != id {
		t.Fatalf("new task = %+v, %v; old = %+v", nt, err, ot)
	}
	waitFor(t, "task on the new agent's stdin", func() bool { return len(stdinLines(in+".sub")) >= 2 })
	board, _ := a.store.SessionBoard(se.ID)
	ns, err := a.store.ListNotes(se.ID, board, notes.NoteFilter{Type: "heads_up"})
	want := "User restarted sub-agent #" + itoa(sub.ID) + " as #" + itoa(id) + " (" + ot.Title + ")"
	if err != nil || len(ns) == 0 || ns[len(ns)-1].Content != want {
		t.Fatalf("heads_up notes = %v, %v; want %q", ns, err, want)
	}
}

// Finishing a busy sub-agent signals it; it is recorded as stopped.
func TestAgentFinishBusy(t *testing.T) {
	a, se, orch, _, in := wakeSetup(t)
	sub := spawnSub(t, a, se, orch, "HOLD", in)
	if err := a.FinishAgent(sub.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stopped", func() bool { ag, _ := a.store.GetAgent(se.ID, sub.ID); return ag.Status == "stopped" })
}
