package main

import (
	"strings"
	"testing"

	"github.com/Nel-sokchhunly/fragile/notes"
)

func TestAGYSessionAppLifecycle(t *testing.T) {
	dir := t.TempDir()
	workDir := t.TempDir()

	t.Setenv("HOME", t.TempDir()) // the agent HOME links into ~/.gemini
	script := `#!/bin/sh
while IFS= read -r line; do
  printf '{"event":"init","conversation_id":"agy-init-123","init":{"cwd":"."}}\n'
  printf '{"event":"step_update","step_update":{"conversation_id":"agy-init-123","step_index":1,"state":"DONE","step_type":"agent_response","text_delta":"agy response"}}\n'
  printf '{"event":"result","result":{"conversation_id":"agy-init-123","status":"SUCCESS","response":"agy response","num_turns":1}}\n'
done
`
	fakeBin := writeScript(t, script)

	a, ev := newTestApp(t, dir, "")
	a.runner.AGYCommand = fakeBin

	se, err := a.CreateSessionWithProvider("Test AGY App", workDir, notes.ProviderAGY, nil)
	if err != nil {
		t.Fatal(err)
	}
	if se.Provider != notes.ProviderAGY {
		t.Fatalf("expected provider agy, got %q", se.Provider)
	}

	sessions, err := a.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range sessions {
		if s.ID == se.ID && s.Provider == notes.ProviderAGY {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("session %d not listed with provider agy", se.ID)
	}

	// Send message
	if err := a.SendMessage(se.ID, "hello agy", nil); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "assistant response in chat", func() bool {
		sn := snapshot(t, a, se.ID)
		for _, c := range sn.Chat {
			if c.Kind == "assistant" && strings.Contains(c.Text, "agy response") {
				return true
			}
		}
		return false
	})

	if err := a.StopSession(se.ID); err != nil {
		t.Fatal(err)
	}

	_ = ev
}
