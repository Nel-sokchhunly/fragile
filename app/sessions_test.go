package main

import (
	"strings"
	"testing"

	"github.com/Nel-sokchhunly/fragile/notes"
)

func TestSessionProviders(t *testing.T) {
	dir := t.TempDir()
	a, _ := newTestApp(t, dir, "")
	defer a.close()

	work1 := t.TempDir()
	// 1. CreateSession without explicit providers populates from global default settings.
	se1, err := a.CreateSession("test-default", work1)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	defaults, err := a.GetSubagentProviderSettings()
	if err != nil {
		t.Fatalf("GetSubagentProviderSettings failed: %v", err)
	}
	var wantDefaults []string
	for _, p := range []string{notes.ProviderClaude, notes.ProviderCodex, notes.ProviderAGY} {
		if defaults[p].Enabled {
			wantDefaults = append(wantDefaults, p)
		}
	}
	if len(wantDefaults) == 0 {
		wantDefaults = []string{notes.ProviderClaude}
	}
	if len(se1.EnabledProviders) != len(wantDefaults) {
		t.Fatalf("expected enabled providers %v, got %v", wantDefaults, se1.EnabledProviders)
	}
	for i, p := range wantDefaults {
		if se1.EnabledProviders[i] != p {
			t.Fatalf("expected provider %s at index %d, got %s", p, i, se1.EnabledProviders[i])
		}
	}

	// GetSessionProviders returns matching providers
	gotProviders, err := a.GetSessionProviders(se1.ID)
	if err != nil {
		t.Fatalf("GetSessionProviders failed: %v", err)
	}
	if len(gotProviders) != len(wantDefaults) {
		t.Fatalf("GetSessionProviders mismatch: %v vs %v", gotProviders, wantDefaults)
	}

	// 2. CreateSessionWithProviders with explicit providers.
	work2 := t.TempDir()
	custom := []string{notes.ProviderClaude, notes.ProviderCodex}
	se2, err := a.CreateSessionWithProviders("test-custom", work2, notes.ProviderClaude, custom)
	if err != nil {
		t.Fatalf("CreateSessionWithProviders failed: %v", err)
	}
	if len(se2.EnabledProviders) != 2 || se2.EnabledProviders[0] != notes.ProviderClaude || se2.EnabledProviders[1] != notes.ProviderCodex {
		t.Fatalf("unexpected custom enabled providers: %+v", se2.EnabledProviders)
	}

	// 3. UpdateSessionProviders updates the session and posts a decision note.
	boardID, err := a.store.SessionBoard(se2.ID)
	if err != nil {
		t.Fatalf("SessionBoard failed: %v", err)
	}
	notesBefore, err := a.store.ListNotes(se2.ID, boardID, notes.NoteFilter{})
	if err != nil {
		t.Fatalf("ListNotes failed: %v", err)
	}

	newProviders := []string{notes.ProviderAGY}
	if err := a.UpdateSessionProviders(se2.ID, newProviders); err != nil {
		t.Fatalf("UpdateSessionProviders failed: %v", err)
	}

	updatedProviders, err := a.GetSessionProviders(se2.ID)
	if err != nil {
		t.Fatalf("GetSessionProviders after update failed: %v", err)
	}
	if len(updatedProviders) != 1 || updatedProviders[0] != notes.ProviderAGY {
		t.Fatalf("unexpected updated providers: %+v", updatedProviders)
	}

	// Decision note posted on board
	notesAfter, err := a.store.ListNotes(se2.ID, boardID, notes.NoteFilter{})
	if err != nil {
		t.Fatalf("ListNotes after update failed: %v", err)
	}
	if len(notesAfter) != len(notesBefore)+1 {
		t.Fatalf("expected 1 new note, got %d before and %d after", len(notesBefore), len(notesAfter))
	}
	lastNote := notesAfter[len(notesAfter)-1]
	if lastNote.Type != "decision" {
		t.Fatalf("expected decision note, got type %s", lastNote.Type)
	}
	if !strings.Contains(lastNote.Content, notes.ProviderAGY) {
		t.Fatalf("expected decision note content to mention provider, got %q", lastNote.Content)
	}

	// 4. UpdateSessionProviders validation: invalid provider rejected
	if err := a.UpdateSessionProviders(se2.ID, []string{"invalid-prov"}); err == nil {
		t.Fatal("expected error for invalid provider in UpdateSessionProviders")
	}

	// 5. GetProviders returns detected providers
	providers := a.GetProviders()
	if len(providers) == 0 {
		t.Fatal("GetProviders returned empty list")
	}
}
