package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// Event names beyond the notes package's observation-log events
// (notes.Event*); the app emits these straight to the UI, not to the log.
// Keep in sync with frontend/src/lib/events.ts. Every event reaches the UI as a
// notes.Event envelope {time, event, session_id, agent_id?, payload}.
const (
	eventAgentEvent   = "agent_event"   // payload notes.AgentEvent: one new row of an agent's output
	eventChatItem     = "chat_item"     // payload ChatItem: upsert by id into the orchestrator chat
	eventAgentUpdated = "agent_updated" // payload notes.Agent: upsert by id (after spawn / exit)
	eventTaskUpdated  = "task_updated"  // payload notes.Task: upsert by id
)

// App is the Wails application: it owns the store, the notes server and the
// runner, and its exported methods are the UI's API. Backend state changes reach
// the UI only as events (Go -> Wails events -> Zustand store -> components);
// nothing polls.
type App struct {
	ctx   context.Context
	ready chan struct{}               // closed once ctx is set
	emit  func(name string, data any) // runtime.EventsEmit once started; tests substitute their own

	store     *notes.Store
	log       *notes.EventLog
	runner    *notes.Runner
	httpSrv   *http.Server
	addr      string   // the notes server's loopback address
	agentDir  string   // per-agent MCP configs and output logs
	attachDir string   // chat attachments: <session>/<user_message event>/<index>-<name>
	lock      *os.File // holds the data dir's flock for the process lifetime
	closed    bool

	// Events flow OnEvent/push -> queue -> loop -> emit. The queue is unbounded
	// on purpose: OnEvent runs under the log's lock and the loop itself writes to
	// the log, so a bounded channel could deadlock.
	qmu     sync.Mutex
	queue   []notes.Event
	wake    chan struct{}
	stop    chan struct{}
	stopped chan struct{}

	mu     sync.Mutex       // guards busy and serializes session status updates
	busy   map[int64]bool   // session -> its orchestrator is mid-turn
	limit  *RateLimit       // latest subscription limits (guarded by mu)
	models map[int64]string // agent -> its init model (guarded by mu)

	sendMu sync.Mutex // orders "persist user message, then write it to stdin"
	ansMu  sync.Mutex // one escalation answer at a time

	startMu sync.Mutex // one orchestrator start/resume, or session create, at a time

	terms terminals // session -> its terminal pane's shell (terminal.go)

	prefs prefs // user settings (settings.go) and auto-compact bookkeeping (autocompact.go)

	wq wakeQueues // session -> pending wake events for its orchestrator (wake.go)
}

func NewApp() *App { return &App{ready: make(chan struct{})} }

// openBackend opens the backend before the window exists, so no bound method can
// run against a half-open App. Events wait until startup hands over the Wails
// context (the event queue is unbounded, so nothing is lost meanwhile).
func (a *App) openBackend() error {
	widenPath() // before the runner looks for claude
	dir, err := dataDir()
	if err != nil {
		return err
	}
	return a.open(dir, func(name string, data any) {
		<-a.ready
		runtime.EventsEmit(a.ctx, name, data)
	})
}

// startup runs once when the window is created; ctx lives until the app quits.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	close(a.ready)
}

// shutdown runs when the app quits: stop every agent, then close everything.
func (a *App) shutdown(context.Context) { a.close() }

// dataDir is where the database, event log and agent files live
// (FRAGILE_DATA_DIR overrides the per-user default).
func dataDir() (string, error) {
	if d := os.Getenv("FRAGILE_DATA_DIR"); d != "" {
		// Absolute: agents run in another cwd and the sandbox deny rules need absolute paths.
		return filepath.Abs(d)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Fragile"), nil
}

// open starts the backend: store, event log, notes server on a free loopback
// port, runner. Agents left "running" by a previous run are recorded as crashed.
// Only one instance may use a data dir: recovery would crash and kill the other's agents.
func (a *App) open(dir string, emit func(string, any)) (err error) {
	a.emit = emit
	agentDir := filepath.Join(dir, "agents")
	a.agentDir = agentDir
	a.attachDir = filepath.Join(dir, "attachments") // created on the first attachment
	// The agent dir holds the MCP configs, which contain the agents' secret URLs.
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(agentDir, 0o700); err != nil {
		return err
	}
	if a.lock, err = lockDataDir(dir); err != nil {
		return err
	}
	dbPath, logPath := filepath.Join(dir, "fragile.db"), filepath.Join(dir, "events.jsonl")
	if a.store, err = notes.OpenStore(dbPath); err != nil {
		a.lock.Close()
		return err
	}
	if a.log, err = notes.OpenEventLog(logPath); err != nil {
		a.store.Close()
		a.lock.Close()
		return err
	}
	// Loopback only: the server can launch agents and must not be reachable from the network.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		a.log.Close()
		a.store.Close()
		a.lock.Close()
		return err
	}

	a.addr = ln.Addr().String()
	a.busy = map[int64]bool{}
	a.models = map[int64]string{}
	a.wake, a.stop, a.stopped = make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	a.loadRateLimit()
	a.log.OnEvent = a.push
	go a.loop()

	a.runner = notes.NewRunner(notes.Config{Addr: ln.Addr().String(), AgentDir: agentDir, DBPath: dbPath, LogPath: logPath}, a.store, a.log)
	a.runner.Interactive = true
	a.runner.DefaultModel = a.subagentDefaultModel
	a.runner.PluginDirs = a.subagentPluginDirs
	a.runner.OnLine = a.onLine
	srv := &notes.Server{Store: a.store, Log: a.log, Runner: a.runner, OnEscalation: a.onEscalation}
	a.httpSrv = &http.Server{Handler: srv.Handler()}
	go a.httpSrv.Serve(ln)

	return a.recoverStale()
}

// close stops every agent (recording their outcome), then the server, then
// flushes pending events and closes the files.
func (a *App) close() {
	if a.closed {
		return
	}
	a.closed = true
	a.closeTerminals()
	a.runner.StopAll()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.httpSrv.Shutdown(ctx)
	close(a.stop)
	<-a.stopped
	a.log.Close()
	a.store.Close()
	a.lock.Close() // releases the flock
}

// lockDataDir takes an exclusive, non-blocking flock on dir/fragile.lock; it
// fails if another Fragile instance holds it. Closing the file releases it.
func lockDataDir(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "fragile.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another Fragile instance is already using %s; quit it first", dir)
		}
		return nil, fmt.Errorf("locking %s: %w", dir, err)
	}
	return f, nil
}

// Bound methods. Session ids and agent ids are the database ids.

// PickDirectory opens a native directory chooser; "" means the user cancelled.
func (a *App) PickDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Working directory for the session"})
}

// ListSessions returns every session, newest first.
func (a *App) ListSessions() ([]notes.Session, error) {
	list, err := a.store.ListSessions()
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	if list == nil {
		list = []notes.Session{}
	}
	return list, err
}

// GetAgentEvents pages an agent's output: events with id > sinceID, oldest
// first, at most limit (default and cap 1000). Page by passing the last id seen.
func (a *App) GetAgentEvents(agentID, sinceID int64, limit int) ([]notes.AgentEvent, error) {
	ag, err := a.store.FindAgent(agentID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	evs, err := a.store.ListAgentEvents(ag.SessionID, agentID, sinceID, limit)
	if evs == nil {
		evs = []notes.AgentEvent{}
	}
	return evs, err
}

// GetAgentEventTail returns the newest output, oldest first (default and cap 2000).
// The paged GetAgentEvents API remains available for consumers needing full history.
func (a *App) GetAgentEventTail(agentID int64, limit int) ([]notes.AgentEvent, error) {
	ag, err := a.store.FindAgent(agentID)
	if err != nil {
		return nil, err
	}
	evs, err := a.store.ListAgentEventTail(ag.SessionID, agentID, limit)
	if evs == nil {
		evs = []notes.AgentEvent{}
	}
	return evs, err
}

// AddNote posts a note to the session's board as the user (author_agent_id 0).
func (a *App) AddNote(sessionID int64, noteType, content string) (notes.Note, error) {
	return a.AddScopedNote(sessionID, notes.ScopeSession, noteType, content)
}

// AddScopedNote is AddNote in the given scope ("session" or an existing "private:<name>").
func (a *App) AddScopedNote(sessionID int64, scope, noteType, content string) (notes.Note, error) {
	if err := notes.CheckNoteType(noteType); err != nil {
		return notes.Note{}, err
	}
	if strings.TrimSpace(content) == "" {
		return notes.Note{}, errors.New("content must not be empty")
	}
	scopes, err := a.store.ListScopes(sessionID)
	if err != nil {
		return notes.Note{}, err
	}
	i := slices.IndexFunc(scopes, func(sc notes.Scope) bool { return sc.Name == scope })
	if i < 0 {
		return notes.Note{}, fmt.Errorf("unknown scope %q", scope)
	}
	n, err := a.store.PostNote(sessionID, scopes[i].BoardID, 0, noteType, content)
	if err != nil {
		return notes.Note{}, err
	}
	a.log.Write(notes.EventNotePosted, sessionID, 0, n) // wakes agents blocked in wait_for_notes
	return n, nil
}

// UpdateNote changes a note's content and/or status ("" leaves a field as is), as the user.
func (a *App) UpdateNote(sessionID, noteID int64, content, status string) (notes.Note, error) {
	var c, s *string
	if content != "" {
		c = &content
	}
	if status != "" {
		if err := notes.CheckNoteStatus(status); err != nil {
			return notes.Note{}, err
		}
		s = &status
	}
	if c == nil && s == nil {
		return notes.Note{}, errors.New("provide content and/or status")
	}
	n, err := a.store.UpdateNote(sessionID, noteID, c, s)
	if err != nil {
		return notes.Note{}, fmt.Errorf("update note %d: %w", noteID, err)
	}
	a.log.Write(notes.EventNoteUpdated, sessionID, 0, n)
	return n, nil
}

// GetProviders returns available CLI providers with their detection state and default models.
func (a *App) GetProviders() []notes.ProviderInfo {
	return notes.DetectProviders()
}
