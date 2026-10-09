package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Nel-sokchhunly/fragile/notes"
)

func TestSetSessionConfig(t *testing.T) {
	a, _ := newTestApp(t, t.TempDir(), "")
	defer a.close()

	cfg := notes.SessionConfig{EnabledProviders: []string{notes.ProviderClaude, notes.ProviderCodex}}
	se, err := a.CreateSessionWithProvider("s", t.TempDir(), notes.ProviderClaude, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(se.SessionConfig, cfg) {
		t.Fatalf("created with %+v", se.SessionConfig)
	}
	board, err := a.store.SessionBoard(se.ID)
	if err != nil {
		t.Fatal(err)
	}
	countNotes := func() []notes.Note {
		ns, err := a.store.ListNotes(se.ID, board, notes.NoteFilter{})
		if err != nil {
			t.Fatal(err)
		}
		return ns
	}

	// Only the auto-compact threshold: no note.
	cfg.AutoCompactTokens = 40_000
	if se, err = a.SetSessionConfig(se.ID, cfg); err != nil || se.AutoCompactTokens != 40_000 {
		t.Fatalf("set = %+v, %v", se.SessionConfig, err)
	}
	if n := len(countNotes()); n != 0 {
		t.Fatalf("%d notes after an auto-compact change", n)
	}

	// CLIs and rules: one decision note naming both.
	cfg.EnabledProviders, cfg.OrchestratorRules = []string{notes.ProviderAGY}, "agy flash: tests only"
	if se, err = a.SetSessionConfig(se.ID, cfg); err != nil || !reflect.DeepEqual(se.SessionConfig, cfg) {
		t.Fatalf("set = %+v, %v", se.SessionConfig, err)
	}
	ns := countNotes()
	if len(ns) != 1 || ns[0].Type != "decision" || !strings.Contains(ns[0].Content, "agy") || !strings.Contains(ns[0].Content, cfg.OrchestratorRules) {
		t.Fatalf("notes = %+v", ns)
	}

	// Threshold: decision note names it.
	cfg.EscalationThreshold = "only when blocked"
	if se, err = a.SetSessionConfig(se.ID, cfg); err != nil || !reflect.DeepEqual(se.SessionConfig, cfg) {
		t.Fatalf("set = %+v, %v", se.SessionConfig, err)
	}
	ns = countNotes()
	if len(ns) != 2 || ns[1].Type != "decision" || !strings.Contains(ns[1].Content, "only when blocked") {
		t.Fatalf("notes = %+v", ns)
	}

	if _, err := a.SetSessionConfig(se.ID, notes.SessionConfig{}); err == nil {
		t.Fatal("empty CLI list accepted")
	}
}
