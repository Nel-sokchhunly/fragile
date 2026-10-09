//go:build e2e

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// Uses the operator's existing Google Antigravity login and the real app backend, CLI,
// per-agent HOME (GEMINI.md prompt, MCP config, permission rules, hooks) and
// authenticated MCP server. Opt-in: no paid API credential.
// FRAGILE_AGY_E2E=1 go test -tags e2e -run TestAGYAppE2E -v -timeout 8m ./app
func TestAGYAppE2E(t *testing.T) {
	if os.Getenv("FRAGILE_AGY_E2E") != "1" {
		t.Skip("set FRAGILE_AGY_E2E=1 to use an existing Google Antigravity CLI login")
	}
	a, ev := newTestApp(t, t.TempDir(), "")
	work := t.TempDir()
	se, err := a.CreateSessionWithProvider("AGY E2E", work, notes.ProviderAGY)
	if err != nil {
		t.Fatal(err)
	}
	if se.Provider != notes.ProviderAGY {
		t.Fatal("provider not persisted")
	}
	wait := func(what string, cond func(SessionSnapshot) bool) {
		t.Helper()
		for end := time.Now().Add(2 * time.Minute); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
			if cond(snapshot(t, a, se.ID)) {
				t.Log("PASS", what)
				return
			}
		}
		t.Fatalf("timeout %s; chat: %v", what, chatTexts(snapshot(t, a, se.ID)))
	}
	contains := func(s SessionSnapshot, text string) bool {
		for _, c := range s.Chat {
			if c.Kind == "assistant" && strings.Contains(c.Text, text) {
				return true
			}
		}
		return false
	}
	send := func(text string) {
		t.Helper()
		if err := a.SendMessage(se.ID, text, nil); err != nil {
			t.Fatal(err)
		}
	}
	send("Bounded end-to-end test. Use Fragile spawn_subagent to start exactly one worker. Its sole task: write hello.txt containing exactly AGY_WORKER_E2E_OK in this project, post a done note, then stop. Wait for it using Fragile tools and verify its success. Then use escalate_to_user to ask exactly E2E_CHOOSE_COLOR with red/blue choices, and stop your turn to wait for my answer. Do not use native subagents, change other files, or do any unrelated work.")
	wait("real worker + MCP escalation", func(s SessionSnapshot) bool { return len(s.Agents) == 2 && len(s.Escalations) == 1 && len(s.Notes) > 0 })
	b, err := os.ReadFile(filepath.Join(work, "hello.txt"))
	if err != nil || strings.TrimSpace(string(b)) != "AGY_WORKER_E2E_OK" {
		t.Fatalf("worker artifact %q: %v", b, err)
	}
	s := snapshot(t, a, se.ID)
	if s.Session.Status != "needs_you" {
		t.Fatalf("escalation status=%s", s.Session.Status)
	}
	if err := a.AnswerEscalation(s.Escalations[0].ID, "blue; reply exactly AGY_ANSWER_BLUE_OK and do nothing else"); err != nil {
		t.Fatal(err)
	}
	wait("answer delivery + streamed reply", func(s SessionSnapshot) bool {
		return len(s.Escalations) == 0 && s.Session.Status == "done" && contains(s, "AGY_ANSWER_BLUE_OK")
	})
	send("Run only the shell command sleep 30, then reply AGY_SLEEP_DONE. No other work.")
	wait("running turn", func(s SessionSnapshot) bool { return s.Session.Status == "working" })
	time.Sleep(time.Second)
	// agy has no interrupt message: the request fails and the turn runs on.
	if err := a.InterruptSession(se.ID); err == nil || !strings.Contains(err.Error(), "not supported for Antigravity") {
		t.Fatalf("interrupt: %v", err)
	}
	wait("sandboxed command turn", func(s SessionSnapshot) bool {
		return s.Session.Status == "done" && contains(s, "AGY_SLEEP_DONE")
	})
	send("Reply exactly AGY_SAME_PROCESS_OK; no tools.")
	wait("same-process follow-up", func(s SessionSnapshot) bool {
		return s.Session.Status == "done" && contains(s, "AGY_SAME_PROCESS_OK")
	})
	if err := a.StopSession(se.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.ResumeSession(se.ID); err != nil {
		t.Fatal(err)
	}
	wait("persisted thread resume", func(s SessionSnapshot) bool { return s.Session.Status == "done" && len(s.Agents) == 3 })
	send("Reply exactly AGY_RESUMED_OK; no tools.")
	wait("resumed follow-up", func(s SessionSnapshot) bool { return s.Session.Status == "done" && contains(s, "AGY_RESUMED_OK") })
	if len(ev.named(eventAgentEvent)) == 0 || len(ev.named(eventChatItem)) == 0 {
		t.Fatal("UI event stream was empty")
	}
	t.Logf("UI events=%d chat events=%d statuses=%v", len(ev.named(eventAgentEvent)), len(ev.named(eventChatItem)), ev.statuses(se.ID))
}
