package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	args = append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func put(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newChangesApp is a test app with one session whose work dir is dir.
func newChangesApp(t *testing.T, dir string) (*App, int64) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	a, _ := newTestApp(t, t.TempDir(), "")
	se, err := a.CreateSession("", dir)
	if err != nil {
		t.Fatal(err)
	}
	return a, se.ID
}

func changeMap(t *testing.T, a *App, id int64) map[string]ChangedFile {
	t.Helper()
	c, err := a.GetChanges(id)
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsRepo {
		t.Fatal("is_repo = false")
	}
	m := map[string]ChangedFile{}
	for i, f := range c.Files {
		if i > 0 && c.Files[i-1].Path >= f.Path {
			t.Errorf("files not sorted: %q before %q", c.Files[i-1].Path, f.Path)
		}
		m[f.Path] = f
	}
	return m
}

const tenLines = "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n"

func TestChanges(t *testing.T) {
	dir := t.TempDir()
	a, id := newChangesApp(t, dir)
	runGit(t, dir, "init", "-q")
	put(t, dir, "a.txt", tenLines)
	put(t, dir, "del.txt", "x\ny\nz\n")
	put(t, dir, "old.txt", "one\ntwo\nthree\nfour\nfive\n")
	put(t, dir, "sub/s.txt", "s\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "init")

	put(t, dir, "a.txt", strings.Replace(tenLines, "l5", "L5", 1))
	put(t, dir, "new.txt", "n1\nn2\n")
	put(t, dir, "bin.dat", "a\x00b")
	runGit(t, dir, "add", "new.txt", "bin.dat")
	if err := os.Remove(filepath.Join(dir, "del.txt")); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "mv", "old.txt", "renamed.txt")
	put(t, dir, "u.txt", "x\ny\n")
	put(t, dir, "ub.dat", "\x00\x01")

	m := changeMap(t, a, id)
	want := []struct {
		path, status, old string
		added, removed    int
		binary            bool
	}{
		{"a.txt", "M", "", 1, 1, false},
		{"new.txt", "A", "", 2, 0, false},
		{"del.txt", "D", "", 0, 3, false},
		{"renamed.txt", "R", "old.txt", 0, 0, false},
		{"u.txt", "?", "", 2, 0, false},
		{"bin.dat", "A", "", 0, 0, true},
		{"ub.dat", "?", "", 0, 0, true},
	}
	if len(m) != len(want) {
		t.Errorf("got %d files, want %d: %+v", len(m), len(want), m)
	}
	for _, w := range want {
		f := m[w.path]
		if f.Status != w.status || f.OldPath != w.old || f.Added != w.added || f.Removed != w.removed || f.Binary != w.binary || f.Sig == "" {
			t.Errorf("%s = %+v, want %+v", w.path, f, w)
		}
	}

	sig := m["a.txt"].Sig
	put(t, dir, "a.txt", strings.Replace(tenLines, "l5", "Lfive", 1))
	if changeMap(t, a, id)["a.txt"].Sig == sig {
		t.Error("sig did not change after an edit")
	}
	put(t, dir, "a.txt", strings.Replace(tenLines, "l5", "L5", 1))

	// hunk line numbers
	d, err := a.GetFileDiff(id, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Hunks) != 1 || len(d.FileLines) != 10 || d.FileLines[4] != "L5" {
		t.Fatalf("a.txt diff = %+v", d)
	}
	h := d.Hunks[0]
	if h.OldStart != 2 || h.OldLines != 7 || h.NewStart != 2 || h.NewLines != 7 || len(h.Lines) != 8 {
		t.Errorf("hunk header = %+v", h)
	}
	if got := h.Lines[0]; got != (DiffLine{" ", "l2", 2, 2}) {
		t.Errorf("first line = %+v", got)
	}
	if got := h.Lines[3]; got != (DiffLine{"-", "l5", 5, 0}) {
		t.Errorf("removed line = %+v", got)
	}
	if got := h.Lines[4]; got != (DiffLine{"+", "L5", 0, 5}) {
		t.Errorf("added line = %+v", got)
	}
	if got := h.Lines[7]; got != (DiffLine{" ", "l8", 8, 8}) {
		t.Errorf("last line = %+v", got)
	}

	// deleted: all "-", no file lines
	if d, err = a.GetFileDiff(id, "del.txt"); err != nil || len(d.Hunks) != 1 || len(d.Hunks[0].Lines) != 3 || len(d.FileLines) != 0 || d.Hunks[0].Lines[0].Kind != "-" {
		t.Errorf("del.txt diff = %+v, %v", d, err)
	}
	// renamed without edits: no hunks
	if d, err = a.GetFileDiff(id, "renamed.txt"); err != nil || len(d.Hunks) != 0 || len(d.FileLines) != 5 {
		t.Errorf("renamed.txt diff = %+v, %v", d, err)
	}
	// untracked: one all-"+" hunk
	d, err = a.GetFileDiff(id, "u.txt")
	if err != nil || len(d.Hunks) != 1 || d.Hunks[0].NewLines != 2 || len(d.FileLines) != 2 {
		t.Fatalf("u.txt diff = %+v, %v", d, err)
	}
	if got := d.Hunks[0].Lines[1]; got != (DiffLine{"+", "y", 0, 2}) {
		t.Errorf("untracked line = %+v", got)
	}
	// binary, tracked and untracked
	for _, p := range []string{"bin.dat", "ub.dat"} {
		if d, err = a.GetFileDiff(id, p); err != nil || !d.Binary || len(d.Hunks) != 0 {
			t.Errorf("%s diff = %+v, %v", p, d, err)
		}
	}
}

func TestChangesFreshRepo(t *testing.T) {
	dir := t.TempDir()
	a, id := newChangesApp(t, dir)
	runGit(t, dir, "init", "-q")
	put(t, dir, "f.txt", "f\n")
	runGit(t, dir, "add", "f.txt")
	put(t, dir, "g.txt", "g1\ng2\ng3")

	m := changeMap(t, a, id)
	if f := m["f.txt"]; f.Status != "A" || f.Added != 1 {
		t.Errorf("f.txt = %+v", f)
	}
	if f := m["g.txt"]; f.Status != "?" || f.Added != 3 {
		t.Errorf("g.txt = %+v", f)
	}
	d, err := a.GetFileDiff(id, "f.txt")
	if err != nil || len(d.Hunks) != 1 || d.Hunks[0].Lines[0] != (DiffLine{"+", "f", 0, 1}) {
		t.Errorf("f.txt diff = %+v, %v", d, err)
	}
	if d, err = a.GetFileDiff(id, "g.txt"); err != nil || len(d.FileLines) != 3 {
		t.Errorf("g.txt diff = %+v, %v", d, err)
	}
}

func TestChangesNotARepo(t *testing.T) {
	dir := t.TempDir()
	a, id := newChangesApp(t, dir)
	c, err := a.GetChanges(id)
	if err != nil || c.IsRepo || c.Files == nil || len(c.Files) != 0 {
		t.Errorf("GetChanges = %+v, %v", c, err)
	}
}

func TestFileDiffRejectsBadPaths(t *testing.T) {
	dir := t.TempDir()
	a, id := newChangesApp(t, dir)
	runGit(t, dir, "init", "-q")
	put(t, dir, "ok.txt", "ok\n")
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "outside.txt"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"", ".", "..", "../outside.txt", "a/../../outside.txt", filepath.Join(filepath.Dir(dir), "outside.txt")} {
		if d, err := a.GetFileDiff(id, p); err == nil {
			t.Errorf("GetFileDiff(%q) = %+v, want an error", p, d)
		}
	}
	if _, err := a.GetFileDiff(id, "sub/../ok.txt"); err != nil {
		t.Errorf("clean in-dir path rejected: %v", err)
	}
}
