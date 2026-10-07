package main

import (
	"encoding/base64"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// termOutput is everything the session's terminal_output events carried, decoded.
func termOutput(t *testing.T, ev *events, sessionID int64) string {
	t.Helper()
	var b strings.Builder
	for _, e := range ev.named(eventTerminalOutput) {
		if e.SessionID != sessionID {
			continue
		}
		d, err := base64.StdEncoding.DecodeString(e.Payload.(map[string]string)["data"])
		if err != nil {
			t.Fatal(err)
		}
		b.Write(d)
	}
	return b.String()
}

func termExits(ev *events, sessionID int64) []int {
	var out []int
	for _, e := range ev.named(eventTerminalExit) {
		if e.SessionID == sessionID {
			out = append(out, e.Payload.(map[string]int)["code"])
		}
	}
	return out
}

func termPid(a *App, sessionID int64) int {
	a.terms.mu.Lock()
	defer a.terms.mu.Unlock()
	if t := a.terms.m[sessionID]; t != nil {
		return t.cmd.Process.Pid
	}
	return 0
}

func decodeBacklog(t *testing.T) func(string, error) string {
	return func(s string, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
}

// newTerminalApp is a test app whose terminals run /bin/sh with a scratch home.
func newTerminalApp(t *testing.T) (*App, *events, notes.Session) {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ENV", "")
	a, ev := newTestApp(t, t.TempDir(), "")
	se, err := a.CreateSession("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return a, ev, se
}

func TestTerminal(t *testing.T) {
	a, ev, se := newTerminalApp(t)
	if _, err := a.TerminalOpen(se.ID+100, 80, 24); err == nil {
		t.Error("terminal opened for an unknown session")
	}
	if err := a.TerminalWrite(se.ID, "x"); err == nil {
		t.Error("write to a terminal that was never opened accepted")
	}
	if err := a.TerminalResize(se.ID, 80, 24); err == nil {
		t.Error("resize of a terminal that was never opened accepted")
	}

	if _, err := a.TerminalOpen(se.ID, 80, 24); err != nil {
		t.Fatal(err)
	}
	pid := termPid(a, se.ID)
	if pid == 0 {
		t.Fatal("no shell running after TerminalOpen")
	}
	if err := a.TerminalWrite(se.ID, "echo hi-$((1+1))\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "hi-2 in terminal_output", func() bool { return strings.Contains(termOutput(t, ev, se.ID), "hi-2") })

	// A second open (a remounted pane) gets the backlog and keeps the same shell.
	backlog := decodeBacklog(t)(a.TerminalOpen(se.ID, 100, 30))
	if !strings.Contains(backlog, "hi-2") {
		t.Fatalf("backlog = %q", backlog)
	}
	if termPid(a, se.ID) != pid {
		t.Fatal("TerminalOpen started a second shell")
	}
	if err := a.TerminalWrite(se.ID, "echo size-$(stty size)\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "size from open", func() bool { return strings.Contains(termOutput(t, ev, se.ID), "size-30 100") })
	if err := a.TerminalResize(se.ID, 1, 5000); err != nil { // clamped to 2 cols, 1000 rows
		t.Fatal(err)
	}
	if err := a.TerminalWrite(se.ID, "echo size-$(stty size)\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "clamped size", func() bool { return strings.Contains(termOutput(t, ev, se.ID), "size-1000 2") })

	// exit ends the shell: terminal_exit with its code, and the next open starts a new one.
	if err := a.TerminalResize(se.ID, 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := a.TerminalWrite(se.ID, "exit 3\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "terminal_exit", func() bool { return len(termExits(ev, se.ID)) == 1 })
	if got := termExits(ev, se.ID); got[0] != 3 {
		t.Fatalf("exit code = %v, want 3", got)
	}
	if termPid(a, se.ID) != 0 {
		t.Fatal("exited shell still registered")
	}
	if err := a.TerminalWrite(se.ID, "x"); err == nil {
		t.Error("write after exit accepted")
	}
	backlog = decodeBacklog(t)(a.TerminalOpen(se.ID, 80, 24))
	if strings.Contains(backlog, "hi-2") {
		t.Fatalf("new shell's backlog has the old shell's output: %q", backlog)
	}
	if p := termPid(a, se.ID); p == 0 || p == pid {
		t.Fatalf("new shell pid = %d (old %d)", p, pid)
	}
	if err := a.TerminalWrite(se.ID, "echo again-$((2+2))\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "output from the new shell", func() bool { return strings.Contains(termOutput(t, ev, se.ID), "again-4") })
}

func TestTerminalClose(t *testing.T) {
	a, ev, se := newTerminalApp(t)
	if err := a.TerminalClose(se.ID); err != nil { // nothing to close
		t.Fatal(err)
	}
	if _, err := a.TerminalOpen(se.ID, 80, 24); err != nil {
		t.Fatal(err)
	}
	pid := termPid(a, se.ID)
	// A background job (its own process group under job control) keeps the pty
	// open after the shell is gone; close must not hang on it.
	if err := a.TerminalWrite(se.ID, "sleep 300 &\necho bg-$!\n"); err != nil {
		t.Fatal(err)
	}
	bgRe := regexp.MustCompile(`bg-(\d+)\r\n`)
	waitFor(t, "background job", func() bool { return bgRe.MatchString(termOutput(t, ev, se.ID)) })
	bg, _ := strconv.Atoi(bgRe.FindStringSubmatch(termOutput(t, ev, se.ID))[1])
	if bg <= 1 { // kill(0 or -1) would hit this test or everything
		t.Fatalf("background pid = %d", bg)
	}
	t.Cleanup(func() { syscall.Kill(bg, syscall.SIGKILL) })
	if err := a.TerminalClose(se.ID); err != nil {
		t.Fatal(err)
	}
	if got := termExits(ev, se.ID); len(got) != 1 || got[0] == 0 { // returns once the exit is emitted
		t.Fatalf("exits = %v", got)
	}
	if termPid(a, se.ID) != 0 {
		t.Fatal("closed shell still registered")
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatal("shell still alive")
	}
	if err := a.TerminalClose(se.ID); err != nil { // idempotent
		t.Fatal(err)
	}
	if len(termExits(ev, se.ID)) != 1 {
		t.Fatal("second close emitted another exit")
	}
}

func TestTerminalKilledWithSessionAndApp(t *testing.T) {
	a, ev, se := newTerminalApp(t)
	other, err := a.CreateSession("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{se.ID, other.ID} {
		if _, err := a.TerminalOpen(id, 80, 24); err != nil {
			t.Fatal(err)
		}
	}
	pid, otherPid := termPid(a, se.ID), termPid(a, other.ID)
	if err := a.DeleteSession(se.ID); err != nil {
		t.Fatal(err)
	}
	if len(termExits(ev, se.ID)) != 1 || termPid(a, se.ID) != 0 {
		t.Fatalf("terminal of a deleted session: exits %v pid %d", termExits(ev, se.ID), termPid(a, se.ID))
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatal("deleted session's shell still alive")
	}
	if _, err := a.TerminalOpen(se.ID, 80, 24); err == nil {
		t.Error("terminal opened for a deleted session")
	}
	if termPid(a, other.ID) != otherPid {
		t.Fatal("other session's shell disturbed")
	}

	a.close()
	if len(termExits(ev, other.ID)) != 1 {
		t.Fatalf("app close: exits %v", termExits(ev, other.ID))
	}
	if err := syscall.Kill(otherPid, 0); err == nil {
		t.Fatal("shell alive after app close")
	}
}

func TestRing(t *testing.T) {
	r := ring{max: 8}
	for _, c := range []struct{ write, want string }{
		{"abc", "abc"},
		{"defgh", "abcdefgh"},
		{"ijk", "defghijk"},
		{"0123456789", "23456789"}, // larger than max: only its tail
		{"x", "3456789x"},
		{"", "3456789x"},
	} {
		r.write([]byte(c.write))
		if got := string(r.bytes()); got != c.want {
			t.Fatalf("after %q: %q, want %q", c.write, got, c.want)
		}
	}
	var all strings.Builder
	for i := range 1000 { // many small writes exercise the trimming
		s := string(rune('a' + i%26))
		r.write([]byte(s))
		all.WriteString(s)
	}
	want := all.String()[all.Len()-8:]
	if got := string(r.bytes()); got != want {
		t.Fatalf("after many writes: %q, want %q", got, want)
	}
	if len(r.buf) > 2*r.max {
		t.Fatalf("buffer grew to %d", len(r.buf))
	}
}
