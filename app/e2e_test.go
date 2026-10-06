//go:build e2e

package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// Real end-to-end check against the `claude` CLI (costs money):
//
//	go test -tags e2e -run TestE2E -v -timeout 10m ./app
func TestE2E(t *testing.T) {
	if os.Getenv("ANTHROPIC_MODEL") == "" {
		t.Setenv("ANTHROPIC_MODEL", "haiku") // cheap
	}
	a, ev := newTestApp(t, t.TempDir(), "")
	work := t.TempDir()
	se, err := a.CreateSession("Spawn one sub-agent that writes hello.txt containing hi, then finish", work)
	if err != nil {
		t.Fatal(err)
	}
	status := func() string { return snapshot(t, a, se.ID).Session.Status }
	waitDone := func(what string) {
		t.Helper()
		for deadline := time.Now().Add(8 * time.Minute); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
			if status() == "done" {
				return
			}
		}
		t.Fatalf("timed out waiting for done (%s); chat: %v", what, chatTexts(snapshot(t, a, se.ID)))
	}
	waitDone("task")
	s := snapshot(t, a, se.ID)
	b, err := os.ReadFile(work + "/hello.txt")
	t.Logf("after task: status=%s agents=%d tasks=%d notes=%d chat=%d hello.txt=%q err=%v", s.Session.Status, len(s.Agents), len(s.Tasks), len(s.Notes), len(s.Chat), b, err)
	if err != nil || !strings.Contains(string(b), "hi") || len(s.Agents) != 2 {
		t.Fatalf("task not done as asked; chat: %v", chatTexts(s))
	}

	before := len(s.Chat)
	if err := a.SendMessage(se.ID, "Reply with the word OK"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "follow-up reply", func() bool {
		c := snapshot(t, a, se.ID).Chat
		return len(c) > before+1 && c[len(c)-1].Kind == "assistant"
	})
	waitDone("follow-up")
	s = snapshot(t, a, se.ID)
	t.Logf("follow-up chat tail: %v", chatTexts(s)[before:])
	t.Logf("status events: %v", ev.statuses(se.ID))

	// total_cost_usd is cumulative within a process, so the last result of each agent counts.
	total := 0.0
	for _, ag := range s.Agents {
		last := 0.0
		evs, _ := a.GetAgentEvents(ag.ID, 0, 1000)
		for _, e := range evs {
			var r struct {
				Cost float64 `json:"total_cost_usd"`
			}
			if e.Type == evResult && json.Unmarshal([]byte(e.Payload), &r) == nil {
				last = r.Cost
			}
		}
		t.Logf("agent %d (%s) cost: $%.4f", ag.ID, ag.Role, last)
		total += last
	}
	t.Logf("TOTAL COST: $%.4f", total)
}
