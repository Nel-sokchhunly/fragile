package main

import (
	"strings"
	"testing"

	"github.com/Nel-sokchhunly/fragile/notes"
)

func TestAGYSessionAppLifecycle(t *testing.T) {
	dir := t.TempDir()
	workDir := t.TempDir()

	script := `#!/bin/sh
printf '{"type":"system","subtype":"init","session_id":"agy-init-123","model":"gemini-3.8-flash","provider":"agy"}\n'
while IFS= read -r line; do
  printf '{"type":"assistant","message":{"content":[{"type":"text","text":"agy response"}]}}\n'
  printf '{"type":"result","subtype":"success","is_error":false,"result":"agy response","provider":"agy"}\n'
done
`
	fakeBin := writeScript(t, script)

	a, ev := newTestApp(t, dir, "")
	a.runner.AGYCommand = fakeBin

	se, err := a.CreateSessionWithProvider("Test AGY App", workDir, notes.ProviderAGY)
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
