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

const (
	titleMax  = 80
	killAfter = 5 * time.Second
)

// Runner launches agents as headless Claude Code processes and watches them.
type Runner struct {
	Command string // binary to run; "claude" unless a test substitutes a fake

	cfg   Config
	store *Store
	log   *EventLog

	// wg counts processes from start until finish() has recorded their outcome.
	// wg.Add runs from request goroutines (spawn_subagent) while Wait may be
	// blocked, which is safe because the orchestrator stays counted for as long
	// as it can spawn; after StopAll sets stopping, start never calls Add again.
	wg       sync.WaitGroup
	mu       sync.Mutex            // guards running and stopping
	running  map[int]chan struct{} // pid -> closed when the process has exited
	stopping bool                  // set by StopAll; start refuses new processes
}

func NewRunner(cfg Config, store *Store, log *EventLog) *Runner {
	return &Runner{Command: "claude", cfg: cfg, store: store, log: log, running: map[int]chan struct{}{}}
}

// StartOrchestrator registers the orchestrator and launches it; task is its prompt.
func (r *Runner) StartOrchestrator(task string) (Agent, error) {
	a, err := r.store.CreateAgent("orchestrator", 0, 0)
	if err != nil {
		return Agent{}, err
	}
	return r.launch(a, "", task, OrchestratorPrompt())
}

// SpawnSubagent creates the task and sub-agent rows and launches the process.
func (r *Runner) SpawnSubagent(parentID int64, task string) (Agent, error) {
	title, _, _ := strings.Cut(strings.TrimSpace(task), "\n")
	if rs := []rune(title); len(rs) > titleMax {
		title = string(rs[:titleMax])
	}
	t, err := r.store.CreateTask(title, task)
	if err != nil {
		return Agent{}, err
	}
	a, err := r.store.CreateAgent("subagent", parentID, t.ID)
	if err != nil {
		return Agent{}, err
	}
	if err := r.store.SetTaskAgent(t.ID, a.ID); err != nil {
		return Agent{}, err
	}
	return r.launch(a, title, task, SubagentPrompt(a.ID, task))
}

// args builds the claude command line. The prompt goes after "--" so a task
// starting with "-" is not parsed as a flag and the variadic flags stop there.
func (r *Runner) args(mcpConfig, prompt, systemPrompt string) []string {
	return []string{"-p", "--output-format", "stream-json", "--verbose",
		"--append-system-prompt", systemPrompt,
		"--mcp-config", mcpConfig, "--strict-mcp-config",
		"--allowedTools", allowedTools,
		"--disallowedTools", disallowedTools,
		"--", prompt}
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
	setErr := r.store.SetAgentProcess(a.ID, pid, logPath)
	r.log.Write(EventAgentSpawned, a.ID, map[string]any{
		"role": a.Role, "parent_id": a.ParentID, "task_id": a.TaskID,
		"pid": pid, "log_path": logPath, "task": taskTitle,
	})
	go func() {
		defer r.wg.Done()
		r.finish(a, taskTitle, <-done, "")
	}()
	if setErr != nil {
		return Agent{}, setErr
	}
	return r.store.GetAgent(a.ID)
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
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Check, start and register under one lock so StopAll cannot miss a process.
	r.mu.Lock()
	if r.stopping {
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
	r.running[pid] = exited
	r.wg.Add(1)
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
	if err := r.store.SetAgentStatus(a.ID, status, &code); err != nil {
		payload["store_error"] = err.Error()
	}
	if a.Role == "subagent" {
		if err := r.store.SetTaskStatus(a.TaskID, taskStatus); err != nil {
			payload["store_error"] = err.Error()
		}
		payload["task_id"], payload["task_status"] = a.TaskID, taskStatus
		if code == 0 {
			if done, err := r.store.ListNotes(r.store.BoardID, NoteFilter{Type: "done", AuthorID: a.ID}); err == nil && len(done) == 0 {
				payload["missing_done_note"] = true
			}
		}
	}
	r.log.Write(EventAgentStatusChanged, a.ID, payload)
}

// Wait blocks until every launched process has exited.
func (r *Runner) Wait() { r.wg.Wait() }

// StopAll refuses further launches, sends SIGTERM to every running process
// group, then SIGKILL to any still alive after a few seconds. It returns once
// they have all exited and their outcome is recorded in the store and log.
func (r *Runner) StopAll() {
	r.mu.Lock()
	r.stopping = true
	procs := make(map[int]chan struct{}, len(r.running))
	for pid, ch := range r.running {
		procs[pid] = ch
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
	r.wg.Wait()
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
