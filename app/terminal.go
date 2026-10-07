package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/creack/pty"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// The terminal pane: one interactive shell per session on a pty, streamed to the
// UI. Nothing about terminals is persisted; their events go straight to emit,
// not through the event log.
const (
	eventTerminalOutput = "terminal_output" // payload {data: base64 bytes}
	eventTerminalExit   = "terminal_exit"   // payload {code: int}; 128+n when killed by signal n

	terminalBacklog = 256 << 10              // output kept for TerminalOpen to repaint a remounted pane
	terminalChunk   = 32 << 10               // read size, and the most one output event coalesces
	terminalFlush   = 16 * time.Millisecond  // at most one output event per session per this
	terminalGrace   = 500 * time.Millisecond // SIGHUP -> SIGKILL, and shell exit -> pty close
	terminalMinSize = 2
	terminalMaxSize = 1000
)

// terminals maps session -> its running shell. An entry is removed when its shell
// exits, so the next TerminalOpen starts a fresh one.
type terminals struct {
	mu     sync.Mutex
	m      map[int64]*terminal
	closed bool // the app is quitting: no new shells
}

type terminal struct {
	sessionID int64
	cmd       *exec.Cmd
	pty       *os.File      // master side; pollable, so Close unblocks a Read
	exited    chan struct{} // closed once cmd.Wait returned
	done      chan struct{} // closed once the exit is emitted and the entry removed

	mu      sync.Mutex // guards backlog
	backlog ring
}

// ring keeps the last max bytes written to it.
type ring struct {
	max int
	buf []byte
}

func (r *ring) write(p []byte) {
	if len(p) >= r.max {
		r.buf = append(r.buf[:0], p[len(p)-r.max:]...)
		return
	}
	// Trim only once the buffer reaches twice max, so writes stay amortized O(len(p)).
	if len(r.buf)+len(p) > 2*r.max {
		keep := r.max - len(p)
		n := copy(r.buf, r.buf[len(r.buf)-keep:])
		r.buf = r.buf[:n]
	}
	r.buf = append(r.buf, p...)
}

func (r *ring) bytes() []byte {
	b := r.buf
	if len(b) > r.max {
		b = b[len(b)-r.max:]
	}
	return append([]byte(nil), b...)
}

// TerminalOpen starts the session's shell if none is running, else resizes it.
// It returns the shell's recent output (base64, at most 256 KB) so the UI can
// repaint after a remount.
func (a *App) TerminalOpen(sessionID int64, cols, rows int) (string, error) {
	se, err := a.store.GetSession(sessionID)
	if err != nil {
		return "", err
	}
	ws := winsize(cols, rows)
	a.terms.mu.Lock()
	defer a.terms.mu.Unlock()
	if a.terms.closed {
		return "", errors.New("the app is quitting")
	}
	t := a.terms.m[sessionID]
	if t != nil {
		setSize(t.pty, ws) // fails only if the shell is exiting, and then its terminal_exit follows
	} else {
		if t, err = startTerminal(sessionID, se.WorkDir, ws); err != nil {
			return "", err
		}
		if a.terms.m == nil {
			a.terms.m = map[int64]*terminal{}
		}
		a.terms.m[sessionID] = t
		go a.pumpTerminal(t)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return base64.StdEncoding.EncodeToString(t.backlog.bytes()), nil
}

// TerminalWrite writes the UI's input (xterm's onData string) to the shell.
func (a *App) TerminalWrite(sessionID int64, data string) error {
	t, err := a.terminal(sessionID)
	if err != nil {
		return err
	}
	_, err = t.pty.Write([]byte(data))
	return err
}

// TerminalResize sets the shell's window size; cols and rows are clamped to 2..1000.
func (a *App) TerminalResize(sessionID int64, cols, rows int) error {
	t, err := a.terminal(sessionID)
	if err != nil {
		return err
	}
	return setSize(t.pty, winsize(cols, rows))
}

// TerminalClose kills the session's shell (its process group) and returns once
// its terminal_exit is emitted. Closing a session without a shell is a no-op.
func (a *App) TerminalClose(sessionID int64) error {
	a.terms.mu.Lock()
	t := a.terms.m[sessionID]
	a.terms.mu.Unlock()
	if t != nil {
		t.kill()
	}
	return nil
}

// closeTerminals kills every shell, and refuses new ones; for App.close.
func (a *App) closeTerminals() {
	a.terms.mu.Lock()
	a.terms.closed = true
	var ts []*terminal
	for _, t := range a.terms.m {
		ts = append(ts, t)
	}
	a.terms.mu.Unlock()
	var wg sync.WaitGroup
	for _, t := range ts {
		wg.Go(t.kill)
	}
	wg.Wait()
}

func (a *App) terminal(sessionID int64) (*terminal, error) {
	a.terms.mu.Lock()
	defer a.terms.mu.Unlock()
	if t := a.terms.m[sessionID]; t != nil {
		return t, nil
	}
	return nil, fmt.Errorf("session %d has no running terminal; open it first", sessionID)
}

// startTerminal starts $SHELL (or /bin/sh) as a login shell in dir (the home
// dir if dir is unset or gone) on a new pty. pty.Start makes the shell a session
// and process group leader with the pty as its controlling terminal.
func startTerminal(sessionID int64, dir string, ws *pty.Winsize) (*terminal, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	if fi, err := os.Stat(dir); dir == "" || err != nil || !fi.IsDir() {
		if dir, err = os.UserHomeDir(); err != nil {
			dir = "/"
		}
	}
	cmd := exec.Command(shell, "-l")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor") // later duplicates win
	f, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", shell, err)
	}
	t := &terminal{sessionID: sessionID, cmd: cmd, exited: make(chan struct{}), done: make(chan struct{}), backlog: ring{max: terminalBacklog}}
	if t.pty, err = pollable(f); err != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Wait()
		return nil, err
	}
	go func() {
		cmd.Wait()
		close(t.exited)
	}()
	return t, nil
}

// pollable returns f's descriptor as a non-blocking, runtime-polled file and
// closes f. pty opens the master blocking (its ioctls call Fd), and a blocked
// Read on such a file cannot be interrupted, so a background job that keeps the
// pty open after the shell exits would pin the reader forever.
func pollable(f *os.File) (*os.File, error) {
	defer f.Close()
	syscall.ForkLock.RLock() // no exec may inherit the dup before it is close-on-exec
	fd, err := syscall.Dup(int(f.Fd()))
	if err == nil {
		syscall.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, err
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), f.Name()), nil
}

// setSize is pty.Setsize without Fd, which would switch f back to blocking.
func setSize(f *os.File, ws *pty.Winsize) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	if err := rc.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(ws)))
	}); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}

func winsize(cols, rows int) *pty.Winsize {
	clamp := func(n int) uint16 { return uint16(min(max(n, terminalMinSize), terminalMaxSize)) }
	return &pty.Winsize{Cols: clamp(cols), Rows: clamp(rows)}
}

// pumpTerminal streams the shell's output to the UI until the shell is gone,
// then removes the entry and emits terminal_exit.
func (a *App) pumpTerminal(t *terminal) {
	out := make(chan []byte, 64) // a slow UI blocks the reader, and so the shell
	go func() {
		defer close(out)
		for {
			buf := make([]byte, terminalChunk)
			n, err := t.pty.Read(buf)
			if n > 0 {
				t.mu.Lock()
				t.backlog.write(buf[:n])
				t.mu.Unlock()
				out <- buf[:n]
			}
			if err != nil { // EIO once every holder of the pty's other end is gone
				return
			}
		}
	}()

	exited, closeAt := t.exited, (<-chan time.Time)(nil)
	var last time.Time
	for open := true; open; {
		select {
		case chunk, ok := <-out:
			if !ok {
				open = false
				break
			}
			// The first output after a quiet spell goes out at once; more within
			// terminalFlush of the last event is batched into the next one.
			var batch []byte
			batch, open = gather(chunk, out, last.Add(terminalFlush))
			a.emitTerminal(t.sessionID, eventTerminalOutput, map[string]string{"data": base64.StdEncoding.EncodeToString(batch)})
			last = time.Now()
		case <-exited: // the shell is gone; give the reader a moment to drain, then cut off whatever still holds the pty
			exited, closeAt = nil, time.After(terminalGrace)
		case <-closeAt:
			closeAt = nil
			t.pty.Close()
		}
	}
	t.pty.Close()
	select {
	case <-t.exited:
	case <-time.After(terminalGrace): // the shell closed its terminal but lives on
		t.signal(syscall.SIGKILL)
		<-t.exited
	}

	a.terms.mu.Lock()
	if a.terms.m[t.sessionID] == t {
		delete(a.terms.m, t.sessionID)
	}
	a.terms.mu.Unlock()
	a.emitTerminal(t.sessionID, eventTerminalExit, map[string]int{"code": exitCode(t.cmd.ProcessState)})
	close(t.done)
}

// gather appends to first what out delivers until deadline (draining only what
// is ready if deadline has passed) or until terminalChunk bytes; it reports
// whether out is still open.
func gather(first []byte, out <-chan []byte, deadline time.Time) ([]byte, bool) {
	batch := first
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for len(batch) < terminalChunk {
		select {
		case c, ok := <-out:
			if !ok {
				return batch, false
			}
			batch = append(batch, c...)
		case <-timer.C:
			return batch, true
		}
	}
	return batch, true
}

// kill sends SIGHUP to the shell's process group, SIGKILL after a grace period,
// and returns once the exit is emitted.
func (t *terminal) kill() {
	t.signal(syscall.SIGHUP)
	select {
	case <-t.done:
		return
	case <-time.After(terminalGrace):
	}
	t.signal(syscall.SIGKILL)
	<-t.done
}

// signal signals the shell's process group, unless the shell was reaped (its pid may be reused).
func (t *terminal) signal(sig syscall.Signal) {
	select {
	case <-t.exited:
	default:
		syscall.Kill(-t.cmd.Process.Pid, sig)
	}
}

func (a *App) emitTerminal(sessionID int64, name string, payload any) {
	a.emit(name, notes.Event{Time: nowUTC(), Event: name, SessionID: sessionID, Payload: payload})
}

func exitCode(ps *os.ProcessState) int {
	if ps == nil {
		return -1
	}
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ps.ExitCode()
}
