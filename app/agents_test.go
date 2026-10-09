package main

import (
	"strings"
	"testing"
	"time"
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
	if got := activity(); got != (AgentActivity{Busy: true}) {
		t.Fatalf("activity of a new sub-agent = %+v", got)
	}
	wantErr(t, a.PauseAgent(orch.ID), "session controls")
	wantErr(t, a.ResumeAgent(99999), "not found")

	if err := a.PauseAgent(sub.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "paused and idle", func() bool { return activity() == AgentActivity{Paused: true} })
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
	waitFor(t, "idle after the resumed turn", func() bool { return activity() == AgentActivity{} })
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
	waitFor(t, "idle again", func() bool { return activity() == AgentActivity{} })
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

// Finishing a busy sub-agent signals it; it is recorded as stopped.
func TestAgentFinishBusy(t *testing.T) {
	a, se, orch, _, in := wakeSetup(t)
	sub := spawnSub(t, a, se, orch, "HOLD", in)
	if err := a.FinishAgent(sub.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stopped", func() bool { ag, _ := a.store.GetAgent(se.ID, sub.ID); return ag.Status == "stopped" })
}
