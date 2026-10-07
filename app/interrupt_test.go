package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// interruptOrchestrator logs every stdin line to stdin.log in its working
// directory and, like Claude Code, answers an interrupt with a
// control_response and a result line that ends the turn.
const interruptOrchestrator = `while IFS= read -r line; do
  printf '%s\n' "$line" >> stdin.log
  case "$line" in *'"control_request"'*)
    printf '{"type":"control_response","response":{"subtype":"success","request_id":"x"}}\n'
    printf '{"type":"result","subtype":"error_during_execution","is_error":false,"result":""}\n' ;;
  esac
done`

func TestInterruptSession(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), interruptOrchestrator)
	work := t.TempDir()
	se, err := a.CreateSession("", work)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.InterruptSession(se.ID); err == nil {
		t.Fatal("interrupt without an orchestrator accepted")
	}
	orch, err := a.runner.StartOrchestrator(se.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	stdinLog := filepath.Join(work, "stdin.log")

	// Idle: nothing to interrupt, nothing written.
	if err := a.InterruptSession(se.ID); err != nil {
		t.Fatalf("idle interrupt: %v", err)
	}
	if _, err := os.Stat(stdinLog); !os.IsNotExist(err) {
		t.Fatalf("idle interrupt wrote to stdin (stat err = %v)", err)
	}

	// Mid-turn: the control_request goes out and the result line clears busy.
	a.setBusy(se.ID, true)
	if err := a.InterruptSession(se.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "turn to end", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return !a.busy[se.ID]
	})
	b, err := os.ReadFile(stdinLog)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); !strings.Contains(got, `"type":"control_request"`) || !strings.Contains(got, `"request":{"subtype":"interrupt"}`) {
		t.Fatalf("stdin = %s", got)
	}
	if !a.runner.Running(orch.ID) {
		t.Fatal("interrupt ended the orchestrator")
	}

	if err := a.StopSession(se.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.InterruptSession(se.ID); err == nil {
		t.Error("interrupt of a stopped session accepted")
	}
}
