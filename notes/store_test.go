package notes

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreNoteRoundTrip(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	orch, err := s.CreateAgent("orchestrator", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if orch.Role != "orchestrator" || orch.ParentID != 0 || orch.Status != "running" || orch.SessionID != s.SessionID {
		t.Fatalf("unexpected agent: %+v", orch)
	}

	posted, err := s.PostNote(s.BoardID, orch.ID, "decision", "use sqlite")
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListNotes(s.BoardID, NoteFilter{Type: "decision", Status: "open", AuthorID: orch.ID})
	if err != nil || len(list) != 1 || list[0] != posted {
		t.Fatalf("list = %+v, err %v; want [%+v]", list, err, posted)
	}

	resolved := "resolved"
	upd, err := s.UpdateNote(posted.ID, nil, &resolved)
	if err != nil {
		t.Fatal(err)
	}
	if upd.Status != "resolved" || upd.Content != "use sqlite" || upd.UpdatedAt < posted.UpdatedAt {
		t.Fatalf("unexpected update: %+v", upd)
	}
	if open, _ := s.ListNotes(s.BoardID, NoteFilter{Status: "open"}); len(open) != 0 {
		t.Fatalf("resolved note still listed as open: %+v", open)
	}

	if _, err := s.PostNote(s.BoardID, orch.ID, "bogus", "x"); err == nil {
		t.Fatal("invalid note type accepted")
	}
	if _, err := s.GetNote(9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetNote missing = %v, want ErrNotFound", err)
	}
}

// A path with URI metacharacters must reach SQLite unchanged.
func TestStoreSpecialCharsInPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a#b?c%d")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "f.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
