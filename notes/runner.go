package notes

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Tools every agent may use without a permission prompt. "mcp__fragile" allows
// all tools of the fragile MCP server; the server itself decides per agent
// which of them it may call.
const allowedTools = "Read,Edit,Write,Glob,Grep,Bash,mcp__fragile"

// Claude Code tools that launch sub-agents inside the agent process (built-in
// sub-agent tool is "Task" or "Agent" depending on version; "Workflow" spawns
// sub-agents too). Never allowed: sub-agents only come from spawn_subagent.
const disallowedTools = "Task,Agent,Workflow"

// Isolation from the user's personal Claude Code setup (issue #39).
// "--setting-sources project" skips user and local settings, which is where
// their plugins, hooks and statusline come from; the project's own settings
// and CLAUDE.md still apply. "--disable-slash-commands" disables all skills.
// Auth is unaffected (claude.ai login lives in the keychain, not in settings).
// Auto-memory is switched off by env so agents do not read or write ~/.claude memory.
var isolationArgs = []string{"--setting-sources", "project", "--disable-slash-commands"}

const isolationEnv = "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1"

const (
	titleMax  = 80
	killAfter = 5 * time.Second
)

// Runner launches agents as headless Claude Code processes and watches them.
// One Runner serves every session of a store; sessions can be stopped
// individually (StopSession) or together (StopAll).
type Runner struct {
	Command string // binary to run; "claude" unless a test substitutes a fake

	cfg   Config
	store *Store
	log   *EventLog

	mu       sync.Mutex // guards everything below
	running  map[int]proc
	sessions map[int64]*sessionRun
	stopping bool // set by StopAll; start refuses new processes
}

// proc is a live agent process; exited is closed once it has exited.
type proc struct {
	sessionID int64
	exited    chan struct{}
}

// sessionRun tracks one session's processes.
type sessionRun struct {
	// wg counts processes from start until finish() has recorded their outcome.
	// wg.Add runs from request goroutines (spawn_subagent) while Wait may be
	// blocked, which is safe because the orchestrator stays counted for as long
	// as it can spawn; once stopped is set, start never calls Add again.
	wg      sync.WaitGroup
	stopped bool // set by StopSession/StopAll; final for this session in this process
}

func NewRunner(cfg Config, store *Store, log *EventLog) *Runner {
	return &Runner{Command: "claude", cfg: cfg, store: store, log: log,
		running: map[int]proc{}, sessions: map[int64]*sessionRun{}}
}

// session returns the session's run state; the caller holds r.mu.
func (r *Runner) session(id int64) *sessionRun {
	sr := r.sessions[id]
	if sr == nil {
		sr = &sessionRun{}
		r.sessions[id] = sr
	}
	return sr
}

// StartOrchestrator registers the session's orchestrator and launches it; task is its prompt.
func (r *Runner) StartOrchestrator(sessionID int64, task string) (Agent, error) {
	a, err := r.store.CreateAgent(sessionID, "orchestrator", 0, 0)
	if err != nil {
		return Agent{}, err
	}
	return r.launch(a, "", task, OrchestratorPrompt(r.cfg.WorkDir))
}

// SpawnSubagent creates the task and sub-agent rows in the session and launches
// the process. parentID must be the session's orchestrator (one level deep only).
func (r *Runner) SpawnSubagent(sessionID, parentID int64, task string) (Agent, error) {
	if p, err := r.store.GetAgent(sessionID, parentID); err != nil {
		return Agent{}, err
	} else if p.Role != "orchestrator" {
		return Agent{}, errors.New("only an orchestrator can spawn sub-agents")
	}
	title, _, _ := strings.Cut(strings.TrimSpace(task), "\n")
	if rs := []rune(title); len(rs) > titleMax {
		title = string(rs[:titleMax])
	}
	t, err := r.store.CreateTask(sessionID, title, task)
	if err != nil {
		return Agent{}, err
	}
	a, err := r.store.CreateAgent(sessionID, "subagent", parentID, t.ID)
	if err != nil {
		return Agent{}, err
	}
	if err := r.store.SetTaskAgent(sessionID, t.ID, a.ID); err != nil {
		return Agent{}, err
	}
	return r.launch(a, title, task, SubagentPrompt(a.ID, task, r.cfg.WorkDir))
}

// args builds the claude command line. The prompt goes after "--" so a task
// starting with "-" is not parsed as a flag and the variadic flags stop there.
func (r *Runner) args(mcpConfig, prompt, systemPrompt string) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose",
		"--append-system-prompt", systemPrompt,
		"--mcp-config", mcpConfig, "--strict-mcp-config",
		"--allowedTools", allowedTools,
		"--disallowedTools", disallowedTools}
	args = append(args, isolationArgs...)
	return append(args, "--", prompt)
}

func (r *Runner) launch(a Agent, taskTitle, prompt, systemPrompt string) (Agent, error) {
	id := strconv.FormatInt(a.ID, 10)
	mcpConfig := filepath.Join(r.cfg.AgentDir, "agent-"+id+".mcp.json")
	logPath := filepath.Join(r.cfg.AgentDir, "agent-"+id+".jsonl")

	pid, done, err := r.start(a, mcpConfig, logPath, prompt, systemPrompt)
	if err != nil {
		r.finish(a, taskTitle, -1, err.Error())
		return Agent{}, fmt.Errorf("launch agent %d: %w", a.ID, err)
	}
	setErr := r.store.SetAgentProcess(a.SessionID, a.ID, pid, logPath)
	r.log.Write(EventAgentSpawned, a.SessionID, a.ID, map[string]any{
		"role": a.Role, "parent_id": a.ParentID, "task_id": a.TaskID,
		"pid": pid, "log_path": logPath, "task": taskTitle,
	})
	go func() {
		defer r.sessionDone(a.SessionID)
		r.finish(a, taskTitle, <-done, "")
	}()
	if setErr != nil {
		return Agent{}, setErr
	}
	return r.store.GetAgent(a.SessionID, a.ID)
}

// start writes the MCP config and starts the process in its own process
// group. The returned channel yields the exit code once the process is gone.
func (r *Runner) start(a Agent, mcpConfig, logPath, prompt, systemPrompt string) (int, <-chan int, error) {
	cfg := fmt.Sprintf(`{"mcpServers":{"fragile":{"type":"http","url":"http://%s/mcp/%s"}}}`, r.cfg.Addr, a.Token)
	if err := os.WriteFile(mcpConfig, []byte(cfg), 0o600); err != nil {
		return 0, nil, err
	}
	out, err := os.Create(logPath)
	if err != nil {
		return 0, nil, err
	}
	defer out.Close() // the child holds its own copy of the descriptor

	// Deliberately not exec.CommandContext: spawn runs inside an MCP request
	// and the process must outlive it.
	cmd := exec.Command(r.Command, r.args(mcpConfig, prompt, systemPrompt)...)
	cmd.Dir = r.cfg.WorkDir
	cmd.Env = append(os.Environ(), isolationEnv)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Check, start and register under one lock so StopAll cannot miss a process.
	r.mu.Lock()
	sr := r.session(a.SessionID)
	if r.stopping || sr.stopped {
		r.mu.Unlock()
		return 0, nil, errors.New("runner is stopping")
	}
	if err := cmd.Start(); err != nil {
		r.mu.Unlock()
		return 0, nil, err
	}
	pid := cmd.Process.Pid
	exited := make(chan struct{})
	code := make(chan int, 1)
	r.running[pid] = proc{a.SessionID, exited}
	sr.wg.Add(1)
	r.mu.Unlock()
	go func() {
		cmd.Wait()
		// Forget the pid at once: it may be reused and must not be signalled again.
		r.mu.Lock()
		delete(r.running, pid)
		r.mu.Unlock()
		close(exited)
		code <- cmd.ProcessState.ExitCode() // -1 if killed by a signal
	}()
	return pid, code, nil
}

// finish records the outcome of an agent process (or of a failed launch, code -1).
func (r *Runner) finish(a Agent, taskTitle string, code int, launchErr string) {
	status, taskStatus := "crashed", "blocked"
	if code == 0 {
		status, taskStatus = "exited", "done"
	}
	payload := map[string]any{"role": a.Role, "status": status, "exit_code": code}
	if launchErr != "" {
		payload["error"] = launchErr
	}
	if err := r.store.SetAgentStatus(a.SessionID, a.ID, status, &code); err != nil {
		payload["store_error"] = err.Error()
	}
	if a.Role == "subagent" {
		if err := r.store.SetTaskStatus(a.SessionID, a.TaskID, taskStatus); err != nil {
			payload["store_error"] = err.Error()
		}
		payload["task_id"], payload["task_status"] = a.TaskID, taskStatus
		if code == 0 {
			if board, err := r.store.SessionBoard(a.SessionID); err == nil {
				if done, err := r.store.ListNotes(a.SessionID, board, NoteFilter{Type: "done", AuthorID: a.ID}); err == nil && len(done) == 0 {
					payload["missing_done_note"] = true
				}
			}
		}
	}
	r.log.Write(EventAgentStatusChanged, a.SessionID, a.ID, payload)
}

// sessionDone marks one process of the session as fully recorded.
func (r *Runner) sessionDone(sessionID int64) {
	r.mu.Lock()
	sr := r.session(sessionID)
	r.mu.Unlock()
	sr.wg.Done()
}

// Wait blocks until every process launched for the session has exited and been recorded.
func (r *Runner) Wait(sessionID int64) {
	r.mu.Lock()
	sr := r.session(sessionID)
	r.mu.Unlock()
	sr.wg.Wait()
}

// StopSession refuses further launches for the session (for good, in this
// process), signals its processes as StopAll does, and returns once they have
// all exited and their outcome is recorded in the store and log. Other
// sessions are untouched.
func (r *Runner) StopSession(sessionID int64) { r.stop(&sessionID) }

// StopAll refuses further launches, sends SIGTERM to every running process
// group, then SIGKILL to any still alive after a few seconds. It returns once
// they have all exited and their outcome is recorded in the store and log.
func (r *Runner) StopAll() { r.stop(nil) }

// stop stops one session, or every session when only is nil.
func (r *Runner) stop(only *int64) {
	r.mu.Lock()
	var runs []*sessionRun
	if only == nil {
		r.stopping = true
		for _, sr := range r.sessions {
			runs = append(runs, sr)
		}
	} else {
		runs = append(runs, r.session(*only))
	}
	for _, sr := range runs {
		sr.stopped = true
	}
	procs := map[int]chan struct{}{}
	for pid, p := range r.running {
		if only == nil || p.sessionID == *only {
			procs[pid] = p.exited
		}
	}
	r.mu.Unlock()

	for pid := range procs {
		r.signal(pid, syscall.SIGTERM)
	}
	expired := make(chan struct{})
	time.AfterFunc(killAfter, func() { close(expired) })
	for pid, ch := range procs {
		select {
		case <-ch:
		case <-expired:
			r.signal(pid, syscall.SIGKILL)
			<-ch
		}
	}
	for _, sr := range runs {
		sr.wg.Wait()
	}
}

// signal signals pid's process group only while pid is still in running, so a
// reused pid is never hit.
func (r *Runner) signal(pid int, sig syscall.Signal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.running[pid]; ok {
		syscall.Kill(-pid, sig)
	}
}
