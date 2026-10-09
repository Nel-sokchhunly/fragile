package notes

import (
	"os"
	"strings"
	"testing"
)

func TestSessionModePersists(t *testing.T) {
	_, store, sess, _ := newTestRunner(t, `true`)
	if sess.Mode != ModeOrchestra {
		t.Fatalf("default mode = %q, want %q", sess.Mode, ModeOrchestra)
	}
	if err := store.SetSessionMode(sess.ID, ModeNormal); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.GetSession(sess.ID); got.Mode != ModeNormal {
		t.Fatalf("mode = %q after set", got.Mode)
	}
	if err := store.SetSessionMode(sess.ID, "bogus"); err == nil {
		t.Fatal("bogus mode accepted")
	}
}

// A normal session's agent launches with no Fragile MCP config, no system
// prompt and the ordinary tools; switching to orchestra restores both.
func TestNormalModeLaunchesWithoutOrchestratorTools(t *testing.T) {
	r, store, sess, _ := newTestRunner(t, `echo "args: $*"`)
	if err := store.SetSessionMode(sess.ID, ModeNormal); err != nil {
		t.Fatal(err)
	}
	a, err := r.StartOrchestrator(sess.ID, "hi")
	if err != nil {
		t.Fatal(err)
	}
	r.Wait(sess.ID)
	out, _ := os.ReadFile(a.LogPath)
	log := string(out)
	for _, bad := range []string{"--mcp-config", "--append-system-prompt", "mcp__fragile"} {
		if strings.Contains(log, bad) {
			t.Errorf("normal launch has %s: %q", bad, log)
		}
	}
	for _, want := range []string{"--strict-mcp-config", "--disallowedTools Task,Agent,Workflow", "--allowedTools " + normalAllowedTools} {
		if !strings.Contains(log, want) {
			t.Errorf("normal launch lacks %q: %q", want, log)
		}
	}

	if err := store.SetSessionMode(sess.ID, ModeOrchestra); err != nil {
		t.Fatal(err)
	}
	b, err := r.ResumeOrchestrator(sess.ID, "abc-123")
	if err != nil {
		t.Fatal(err)
	}
	r.Wait(sess.ID)
	out, _ = os.ReadFile(b.LogPath)
	for _, want := range []string{"--mcp-config", "--append-system-prompt", "--allowedTools mcp__fragile", "--resume abc-123"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("orchestra launch lacks %q: %q", want, out)
		}
	}
}
