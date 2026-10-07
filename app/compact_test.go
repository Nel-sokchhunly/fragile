package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// compactOrchestrator logs every stdin line to stdin.log in its working
// directory and answers /compact the way Claude Code does: status lines, a new
// init, the compact_boundary, the summary as user lines, then a result.
const compactOrchestrator = `while IFS= read -r line; do
  printf '%s\n' "$line" >> stdin.log
  case "$line" in *'"text":"/compact"'*)
    printf '{"type":"system","subtype":"status","status":"compacting"}\n'
    printf '{"type":"system","subtype":"status","status":null,"compact_result":"success"}\n'
    printf '{"type":"system","subtype":"init","session_id":"claude-1","model":"claude-opus-5-5"}\n'
    printf '{"type":"system","subtype":"compact_boundary","compact_metadata":{"trigger":"manual","pre_tokens":25363,"post_tokens":4736}}\n'
    printf '{"type":"user","message":{"role":"user","content":"This session is being continued..."}}\n'
    printf '{"type":"result","subtype":"success","is_error":false,"result":""}\n' ;;
  esac
done`

func TestCompactSession(t *testing.T) {
	a, ev := newTestApp(t, t.TempDir(), compactOrchestrator)
	work := t.TempDir()
	se, err := a.CreateSession("", work)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CompactSession(se.ID); err == nil {
		t.Fatal("compact without an orchestrator accepted")
	}
	orch, err := a.runner.StartOrchestrator(se.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	stdinLog := filepath.Join(work, "stdin.log")

	// Mid-turn: refused, nothing written.
	a.setBusy(se.ID, true)
	if err := a.CompactSession(se.ID); err == nil || !strings.Contains(err.Error(), "the orchestrator is working") {
		t.Fatalf("busy compact: err = %v", err)
	}
	if _, err := os.Stat(stdinLog); !os.IsNotExist(err) {
		t.Fatalf("busy compact wrote to stdin (stat err = %v)", err)
	}
	a.setBusy(se.ID, false)

	if err := a.CompactSession(se.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "compaction to finish", func() bool {
		a.mu.Lock()
		busy := a.busy[se.ID]
		a.mu.Unlock()
		ag, err := a.store.GetAgent(se.ID, orch.ID)
		return !busy && err == nil && ag.ContextUsed == 4736 && len(snapshot(t, a, se.ID).Chat) > 0
	})
	b, err := os.ReadFile(stdinLog)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); strings.Count(got, "\n") != 1 || !strings.Contains(got, `"text":"/compact"`) {
		t.Fatalf("stdin = %s", got)
	}

	// Only the compact_boundary shows, as a notice; no user message, no other system line.
	s := snapshot(t, a, se.ID)
	if len(s.Chat) != 1 || s.Chat[0].Kind != "notice" || s.Chat[0].Text != "Context compacted (manual): 25.4k → 4.7k tokens" {
		t.Fatalf("chat = %v", chatTexts(s))
	}
	waitFor(t, "live chat item", func() bool { return len(ev.named(eventChatItem)) >= 1 })
	if items := ev.named(eventChatItem); len(items) != 1 || items[0].Payload.(ChatItem).Kind != "notice" {
		t.Fatalf("chat_item events = %+v", items)
	}
	ag, _ := a.store.GetAgent(se.ID, orch.ID)
	if ag.ContextUsed != 4736 || ag.ContextWindow != defaultWindow {
		t.Fatalf("context = %d/%d", ag.ContextUsed, ag.ContextWindow)
	}
	waitFor(t, "agent update with the new context", func() bool {
		for _, e := range ev.named(eventAgentUpdated) {
			if u, ok := e.Payload.(notes.Agent); ok && u.ID == orch.ID && u.ContextUsed == 4736 {
				return true
			}
		}
		return false
	})

	if err := a.StopSession(se.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.CompactSession(se.ID); err == nil {
		t.Error("compact of a stopped session accepted")
	}
}

func TestTokenCount(t *testing.T) {
	for n, want := range map[int]string{0: "0", 812: "812", 1000: "1k", 4736: "4.7k", 25363: "25.4k", 199000: "199k", 123456: "123k", 1_000_000: "1M", 1_500_000: "1.5M"} {
		if got := tokenCount(n); got != want {
			t.Errorf("tokenCount(%d) = %q, want %q", n, got, want)
		}
	}
	var c compactBoundary
	c.Metadata.PreTokens, c.Metadata.PostTokens = 199000, 12000
	if got := compactNotice(c); got != "Context compacted (auto): 199k → 12k tokens" {
		t.Errorf("notice = %q", got)
	}
}
