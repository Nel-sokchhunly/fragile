package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// A normal session's agent runs without Fragile tools; switching to orchestra
// relaunches it resuming the same conversation and tells it so.
func TestSwitchNormalToOrchestra(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	a, _ := newTestApp(t, t.TempDir(), `echo "args: $*" >> '`+argsFile+`'
`+initOrchestrator)
	se, err := a.CreateSession("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if se.Mode != notes.ModeOrchestra {
		t.Fatalf("default mode = %q", se.Mode)
	}
	if _, err := a.SetSessionMode(se.ID, "bogus"); err == nil {
		t.Fatal("bogus mode accepted")
	}
	if se, err = a.SetSessionMode(se.ID, notes.ModeNormal); err != nil || se.Mode != notes.ModeNormal { // not started yet: just stored
		t.Fatalf("set normal: %v / %+v", err, se)
	}
	if err := a.SendMessage(se.ID, "task", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first reply", func() bool { return hasChat(a, se.ID, "echo: ") && snapshot(t, a, se.ID).Session.Status == "done" })
	launches := func() []string { b, _ := os.ReadFile(argsFile); return strings.Split(strings.TrimSpace(string(b)), "\nargs: ") } // the prompt spans lines
	if l := launches(); len(l) != 1 || strings.Contains(l[0], "--mcp-config") || strings.Contains(l[0], "--append-system-prompt") {
		t.Fatalf("normal agent launch = %q", l)
	}

	if se, err = a.SetSessionMode(se.ID, notes.ModeOrchestra); err != nil || se.Mode != notes.ModeOrchestra {
		t.Fatalf("switch: %v / %+v", err, se)
	}
	waitFor(t, "resumed", func() bool { return hasChat(a, se.ID, "assistant: resumed claude-1") })
	agents := snapshot(t, a, se.ID).Agents
	if len(agents) != 2 || agents[0].Status != "stopped" || agents[1].Status != "running" {
		t.Fatalf("agents after switch = %+v", agents)
	}
	if l := launches(); len(l) != 2 || !strings.Contains(l[1], "--mcp-config") || !strings.Contains(l[1], "--resume claude-1") {
		t.Fatalf("orchestra launch = %q", l)
	}
	if _, err := a.SetSessionMode(se.ID, notes.ModeNormal); err == nil {
		t.Fatal("a started session went back to normal")
	}
}
