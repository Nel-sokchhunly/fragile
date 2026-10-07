package notes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Tools a sub-agent may use without a permission prompt. "mcp__fragile" allows
// all tools of the fragile MCP server; the server itself decides per agent
// which of them it may call. Edit and Write are not here: they are allowed only
// inside the working directory (see sandboxSettings). In -p mode a tool that is
// not allowed is denied, never prompted for.
const allowedTools = "Read,Glob,Grep,Bash,mcp__fragile"

// The orchestrator runs like the user's own CLI: unsandboxed, with the user's
// settings, in auto mode (Claude Code's classifier approves routine actions and
// denies risky ones, as there is nobody to prompt in -p mode). Only the fragile
// tools are pre-approved: a broad "Bash" allow rule would skip the classifier.
const orchestratorAllowedTools = "mcp__fragile"

// Claude Code tools that launch sub-agents inside the agent process (built-in
// sub-agent tool is "Task" or "Agent" depending on version; "Workflow" spawns
// sub-agents too). Never allowed: sub-agents only come from spawn_subagent.
const disallowedTools = "Task,Agent,Workflow"

// Sub-agent isolation from the user's personal Claude Code setup (issue #39).
// The orchestrator is not isolated: it loads the user's setup like their CLI.
// "--setting-sources project" skips user and local settings, which is where
// their plugins, hooks and statusline come from; the project's own settings
// and CLAUDE.md still apply. "--disable-slash-commands" disables all skills.
// Auth is unaffected (claude.ai login lives in the keychain, not in settings).
// Auto-memory is switched off by env so agents do not read or write ~/.claude memory.
var isolationArgs = []string{"--setting-sources", "project", "--disable-slash-commands"}

const isolationEnv = "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1"

// Variables that make claude bill an API account (or another provider) instead
// of the user's subscription. Agents never get them; CLAUDE_CODE_OAUTH_TOKEN
// (subscription auth) is kept.
var billingEnv = []string{
	"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_CUSTOM_HEADERS",
	"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
	"CLAUDE_CODE_SKIP_BEDROCK_AUTH", "CLAUDE_CODE_SKIP_VERTEX_AUTH", "CLAUDE_CODE_SKIP_FOUNDRY_AUTH",
	"ANTHROPIC_BEDROCK_BASE_URL", "ANTHROPIC_VERTEX_BASE_URL", "ANTHROPIC_VERTEX_PROJECT_ID",
	"ANTHROPIC_FOUNDRY_API_KEY", "ANTHROPIC_FOUNDRY_BASE_URL", "ANTHROPIC_FOUNDRY_RESOURCE",
	"AWS_BEARER_TOKEN_BEDROCK",
}

// agentEnv is the environment an agent runs with: environ minus billingEnv,
// plus isolationEnv for an isolated (sub-)agent.
func agentEnv(environ []string, isolated bool) []string {
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if name, _, _ := strings.Cut(kv, "="); !slices.Contains(billingEnv, name) {
			out = append(out, kv)
		}
	}
	if isolated {
		out = append(out, isolationEnv)
	}
	return out
}

// Hosts sandboxed Bash may reach (package registries and GitHub); everything
// else is refused. Only affects Bash: WebFetch and WebSearch follow permission rules.
var sandboxDomains = []string{
	"registry.npmjs.org", "registry.yarnpkg.com", "pypi.org", "files.pythonhosted.org",
	"proxy.golang.org", "sum.golang.org", "index.crates.io", "static.crates.io", "crates.io",
	"rubygems.org", "github.com", "*.github.com", "*.githubusercontent.com",
}

// Package caches sandboxed Bash may write besides the working directory, so
// builds and installs work (go build, npm/pnpm/pip/cargo). The OS user cache
// dir (go-build, pip, yarn) is added in sandboxSettings.
// ponytail: default locations only; a custom GOMODCACHE/npm cache needs adding here.
var sandboxCaches = []string{"~/go/pkg/mod", "~/.npm", "~/.cargo/registry", "~/.cargo/git", "~/Library/pnpm", "~/.local/share/pnpm"}

// Credentials sandboxed Bash may not read, including the claude login on
// Linux; the allowed hosts (e.g. github.com) could otherwise carry them out.
var sandboxSecretFiles = []string{"~/.ssh", "~/.aws", "~/.config/gh", "~/.netrc", "~/.docker/config.json", "~/.kube", "~/.claude/.credentials.json"}

var sandboxSecretEnv = []string{"GITHUB_TOKEN", "GH_TOKEN", "NPM_TOKEN", "ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"}

// sandboxSettings returns the JSON for --settings (issue #40): Claude Code's
// built-in Bash sandbox (Seatbelt / bubblewrap) with writes limited to the
// working directory, the network limited to sandboxDomains, and the files that
// hold agent tokens (the agent dir) or the whole store (DB, event log) hidden
// from Bash (sandbox rules) and from Read/Edit/Write (permission rules), even
// when they sit under the working directory. The sandbox covers Bash only, so
// the file tools get permission rules: Edit (which also governs Write) is
// allowed only inside workDir, and credentials (sandboxSecretFiles) cannot be
// read or edited. Paths are made absolute with symlinks resolved (/tmp is a
// symlink on macOS), as both the sandbox and the permission check compare real paths.
// Subscription login is untouched: no --bare, no API key.
func sandboxSettings(cfg Config, workDir string) string {
	var hidden, rules []string
	for _, p := range []string{cfg.AgentDir, cfg.DBPath, cfg.LogPath} {
		if p == "" {
			continue
		}
		p = realPath(p)
		hidden = append(hidden, p)
		if p == realPath(cfg.DBPath) { // SQLite sidecar files hold the same data
			hidden = append(hidden, p+"-wal", p+"-shm")
		}
	}
	for _, p := range hidden {
		for _, tool := range []string{"Read", "Edit"} { // Edit also covers Write
			rules = append(rules, tool+"(/"+globEscape(p)+")") // "//abs" = absolute path; a dir covers what is under it
		}
	}
	for _, f := range sandboxSecretFiles {
		rules = append(rules, "Read("+f+")", "Edit("+f+")")
	}
	allow := []string{"Edit(/" + globEscape(realPath(workDir)) + "/**)"}
	writable := sandboxCaches
	if d, err := os.UserCacheDir(); err == nil {
		writable = append([]string{d}, sandboxCaches...)
	}
	var files, env []map[string]string
	for _, f := range sandboxSecretFiles {
		files = append(files, map[string]string{"path": f, "mode": "deny"})
	}
	for _, e := range sandboxSecretEnv {
		env = append(env, map[string]string{"name": e, "mode": "deny"})
	}
	b, _ := json.Marshal(map[string]any{
		"sandbox": map[string]any{
			"enabled":                  true,
			"failIfUnavailable":        true,  // never silently run Bash unsandboxed
			"allowUnsandboxedCommands": false, // no dangerouslyDisableSandbox escape hatch
			"autoAllowBashIfSandboxed": true,
			"filesystem": map[string]any{
				"allowWrite": writable,
				"denyRead":   hidden,
				"denyWrite":  append(slices.Clone(hidden), sandboxSecretFiles...),
			},
			"credentials": map[string]any{"files": files, "envVars": env},
			"network":     map[string]any{"allowedDomains": sandboxDomains, "strictAllowlist": true},
		},
		"permissions": map[string]any{"allow": allow, "deny": rules},
	})
	return string(b)
}

// globEscape backslash-escapes the characters a Read/Edit permission rule
// (gitignore syntax) would read as a pattern, so a real path matches only itself.
// Braces are not special in gitignore syntax and are left as they are.
func globEscape(p string) string {
	return globMeta.Replace(p)
}

var globMeta = strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `]`, `\]`)

// realPath returns p absolute with symlinks resolved. A path that does not
// exist yet (e.g. SQLite's -wal file) is resolved through its directory.
func realPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	if d, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		return filepath.Join(d, filepath.Base(abs))
	}
	return abs
}

// checkSandbox reports a missing prerequisite of the Bash sandbox. macOS ships
// Seatbelt (sandbox-exec); Linux needs bubblewrap and socat.
func checkSandbox() error {
	var need []string
	switch runtime.GOOS {
	case "darwin":
		need = []string{"sandbox-exec"}
	case "linux":
		need = []string{"bwrap", "socat"}
	default:
		return fmt.Errorf("agent sandbox is not supported on %s", runtime.GOOS)
	}
	for _, bin := range need {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("agent sandbox needs %q on PATH (Linux: apt install bubblewrap socat)", bin)
		}
	}
	return nil
}

// checkPrereqs is the launch preflight: the claude CLI must be on PATH (the app
// widens PATH to the login shell's at startup) and the sandbox must be usable.
func checkPrereqs(command string) error {
	if _, err := exec.LookPath(command); err != nil {
		return errors.New("claude CLI not found on PATH; install Claude Code and log in")
	}
	return checkSandbox()
}

const (
	maxRunningSubagents = 8        // per session
	maxTaskBytes        = 32 << 10 // one sub-agent task

	titleMax  = 80
	killAfter = 5 * time.Second
)

// Runner launches agents as headless Claude Code processes and watches them.
// One Runner serves every session of a store; sessions can be stopped
// individually (StopSession) or together (StopAll).
type Runner struct {
	Command string // binary to run; "claude" unless a test substitutes a fake

	// Interactive (set before use) makes the orchestrator a long-lived process
	// that reads stream-json user messages from stdin (see SendUser) instead of
	// taking its prompt as an argument, and uses the interactive prompt.
	Interactive bool
	// OnLine, if set before use, is called with every line an agent writes to
	// stdout/stderr, in order, from that agent's copy goroutine; the slice is only
	// valid during the call. The raw output still goes to the agent's log file.
	OnLine func(a Agent, line []byte)

	cfg   Config
	store *Store
	log   *EventLog

	// Preflight, if set, checks prerequisites (claude on PATH, sandbox tools)
	// before every launch; a launch it rejects fails with its error. Tests clear it.
	Preflight func() error

	spawnMu sync.Mutex // makes "count running sub-agents, create one" atomic

	mu       sync.Mutex // guards everything below
	running  map[int]*proc
	sessions map[int64]*sessionRun
	stopping bool // set by StopAll; start refuses new processes
}

// proc is a live agent process; exited is closed once it has exited.
type proc struct {
	sessionID int64
	agentID   int64
	exited    chan struct{}
	stdin     *stdinPipe // nil unless the agent takes messages on stdin
	killed    bool       // the runner signalled it on purpose (stop); guarded by Runner.mu
}

// exit is how a process ended.
type exit struct {
	code    int  // -1 if killed by a signal
	stopped bool // the runner killed it on purpose
}

// stdinPipe serializes writes to an agent's stdin.
type stdinPipe struct {
	mu sync.Mutex
	w  io.WriteCloser
}

// sessionRun tracks one session's processes.
type sessionRun struct {
	// wg counts processes from start until finish() has recorded their outcome.
	// wg.Add runs from request goroutines (spawn_subagent) while Wait may be
	// blocked, which is safe because the orchestrator stays counted for as long
	// as it can spawn; once stopped is set, start never calls Add again.
	wg      sync.WaitGroup
	stopped bool // set by StopSession/StopAll; only ResumeOrchestrator lifts it (by replacing the sessionRun)
}

// FirstLine returns the first line of text, trimmed and cut to titleMax characters.
func FirstLine(text string) string {
	title, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if rs := []rune(title); len(rs) > titleMax {
		title = string(rs[:titleMax])
	}
	return title
}

// workDir is the directory the session's agents run in: its own, else the configured one.
func (r *Runner) workDir(sessionID int64) string {
	if se, err := r.store.GetSession(sessionID); err == nil && se.WorkDir != "" {
		return se.WorkDir
	}
	return r.cfg.WorkDir
}

func NewRunner(cfg Config, store *Store, log *EventLog) *Runner {
	r := &Runner{Command: "claude", cfg: cfg, store: store, log: log,
		running: map[int]*proc{}, sessions: map[int64]*sessionRun{}}
	r.Preflight = func() error { return checkPrereqs(r.Command) }
	return r
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

// Forget drops the session's run state, including the "stopped" mark, so a
// session that reuses the id (SQLite reuses the highest rowid after a delete)
// can launch. Call it only after StopSession has returned and the session is deleted.
func (r *Runner) Forget(sessionID int64) {
	r.mu.Lock()
	delete(r.sessions, sessionID)
	r.mu.Unlock()
}

// StartOrchestrator registers the session's orchestrator and launches it; task is its prompt.
func (r *Runner) StartOrchestrator(sessionID int64, task string) (Agent, error) {
	a, err := r.store.CreateAgent(sessionID, "orchestrator", 0, 0)
	if err != nil {
		return Agent{}, err
	}
	return r.launch(a, "", task, OrchestratorPrompt(r.workDir(sessionID), r.Interactive), "", "")
}

// ResumeOrchestrator registers a new orchestrator for the session and launches
// it resuming Claude Code session claudeSessionID (--resume); its arguments are
// otherwise those of StartOrchestrator. It lifts StopSession's refusal (not
// StopAll's): once the stopped session's processes are all recorded, the
// session gets fresh run state, as stop() may still be returning from Wait on the old.
func (r *Runner) ResumeOrchestrator(sessionID int64, claudeSessionID string) (Agent, error) {
	if claudeSessionID == "" {
		return Agent{}, errors.New("no Claude Code session to resume")
	}
	r.mu.Lock()
	sr := r.session(sessionID)
	stopped := sr.stopped
	r.mu.Unlock()
	if stopped {
		sr.wg.Wait() // every process was signalled and no new one can start: returns once they are recorded
		r.mu.Lock()
		if r.sessions[sessionID] == sr {
			r.sessions[sessionID] = &sessionRun{}
		}
		r.mu.Unlock()
	}
	a, err := r.store.CreateAgent(sessionID, "orchestrator", 0, 0)
	if err != nil {
		return Agent{}, err
	}
	return r.launch(a, "", "", OrchestratorPrompt(r.workDir(sessionID), r.Interactive), claudeSessionID, "")
}

// SpawnSubagent creates the task and sub-agent rows in the session and launches
// the process. title is the task title shown on the agent card (the task's first line if empty). parentID must be the session's orchestrator (one level deep only).
// model is the full model id it runs with (--model); empty uses the CLI's default.
func (r *Runner) SpawnSubagent(sessionID, parentID int64, title, task, model string) (Agent, error) {
	if p, err := r.store.GetAgent(sessionID, parentID); err != nil {
		return Agent{}, err
	} else if p.Role != "orchestrator" {
		return Agent{}, errors.New("only an orchestrator can spawn sub-agents")
	}
	if len(task) > maxTaskBytes {
		return Agent{}, fmt.Errorf("task is %d bytes; the limit is %d", len(task), maxTaskBytes)
	}
	if title = FirstLine(title); title == "" {
		title = FirstLine(task)
	}
	r.spawnMu.Lock() // the agent row counts as running from CreateAgent on
	a, err := r.createSubagent(sessionID, parentID, title, task)
	r.spawnMu.Unlock()
	if err != nil {
		return Agent{}, err
	}
	return r.launch(a, title, task, SubagentPrompt(a.ID, task, r.workDir(sessionID)), "", model)
}

// createSubagent adds the task and sub-agent rows unless the session already
// has maxRunningSubagents running; the caller holds r.spawnMu.
func (r *Runner) createSubagent(sessionID, parentID int64, title, task string) (Agent, error) {
	subs, err := r.store.ListAgents(sessionID, "subagent")
	if err != nil {
		return Agent{}, err
	}
	n := 0
	for _, s := range subs {
		if s.Status == "running" {
			n++
		}
	}
	if n >= maxRunningSubagents {
		return Agent{}, fmt.Errorf("%d sub-agents are already running (the limit); wait for some to finish before spawning more", n)
	}
	t, err := r.store.CreateTask(sessionID, title, task)
	if err != nil {
		return Agent{}, err
	}
	a, err := r.store.CreateAgent(sessionID, "subagent", parentID, t.ID)
	if err != nil {
		return Agent{}, err
	}
	return a, r.store.SetTaskAgent(sessionID, t.ID, a.ID)
}

// defaultModel runs every agent that is not given a model: the orchestrator always.
const defaultModel = "claude-opus-5-5"

// args builds the claude command line. The prompt goes after "--" so a task
// starting with "-" is not parsed as a flag and the variadic flags stop there.
// A non-empty resume is the Claude Code session id to continue (--resume); a
// non-empty model is a sub-agent's model id (--model).
func (r *Runner) args(a Agent, workDir, mcpConfig, prompt, systemPrompt, resume, model string) []string {
	args := []string{"-p"}
	if r.stdinAgent(a) { // messages arrive on stdin; the first one is the task
		args = append(args, "--input-format", "stream-json")
	}
	args = append(args, "--output-format", "stream-json", "--verbose",
		"--append-system-prompt", systemPrompt,
		"--mcp-config", mcpConfig, "--strict-mcp-config",
		"--disallowedTools", disallowedTools)
	if a.Role == roleOrchestrator {
		args = append(args, "--permission-mode", "auto", "--allowedTools", orchestratorAllowedTools)
	} else {
		args = append(args, "--allowedTools", allowedTools)
		args = append(args, isolationArgs...)
		args = append(args, "--settings", sandboxSettings(r.cfg, workDir))
	}
	if model == "" { // the orchestrator always, and sub-agents spawned without a model
		model = defaultModel
	}
	args = append(args, "--model", model)
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	if r.stdinAgent(a) {
		return args
	}
	return append(args, "--", prompt)
}

// stdinAgent reports whether the agent is the long-lived orchestrator fed over stdin.
func (r *Runner) stdinAgent(a Agent) bool { return r.Interactive && a.Role == roleOrchestrator }

func (r *Runner) launch(a Agent, taskTitle, prompt, systemPrompt, resume, model string) (Agent, error) {
	id := strconv.FormatInt(a.ID, 10)
	mcpConfig := filepath.Join(r.cfg.AgentDir, "agent-"+id+".mcp.json")
	logPath := filepath.Join(r.cfg.AgentDir, "agent-"+id+".jsonl")

	if r.Preflight != nil {
		if err := r.Preflight(); err != nil {
			r.finish(a, taskTitle, -1, err.Error(), false)
			return Agent{}, fmt.Errorf("launch agent %d: %w", a.ID, err)
		}
	}
	pid, done, err := r.start(a, mcpConfig, logPath, prompt, systemPrompt, resume, model)
	if err != nil {
		r.finish(a, taskTitle, -1, err.Error(), false)
		return Agent{}, fmt.Errorf("launch agent %d: %w", a.ID, err)
	}
	setErr := r.store.SetAgentProcess(a.SessionID, a.ID, pid, logPath)
	r.log.Write(EventAgentSpawned, a.SessionID, a.ID, map[string]any{
		"role": a.Role, "parent_id": a.ParentID, "task_id": a.TaskID,
		"pid": pid, "log_path": logPath, "task": taskTitle,
	})
	go func() {
		defer r.sessionDone(a.SessionID)
		x := <-done
		r.finish(a, taskTitle, x.code, "", x.stopped)
	}()
	if setErr != nil {
		return Agent{}, setErr
	}
	return r.store.GetAgent(a.SessionID, a.ID)
}

// start writes the MCP config and starts the process in its own process
// group. The returned channel yields how it ended once the process is gone.
func (r *Runner) start(a Agent, mcpConfig, logPath, prompt, systemPrompt, resume, model string) (int, <-chan exit, error) {
	// Refuse before any file is written; checked again under the lock below.
	r.mu.Lock()
	refused := r.refusing(a.SessionID)
	r.mu.Unlock()
	if refused {
		return 0, nil, errors.New("runner is stopping")
	}
	cfg := fmt.Sprintf(`{"mcpServers":{"fragile":{"type":"http","url":"http://%s/mcp/%s"}}}`, r.cfg.Addr, a.Token)
	if err := os.WriteFile(mcpConfig, []byte(cfg), 0o600); err != nil {
		return 0, nil, err
	}
	out, err := os.Create(logPath)
	if err != nil {
		return 0, nil, err
	}

	// Deliberately not exec.CommandContext: spawn runs inside an MCP request
	// and the process must outlive it.
	workDir := r.workDir(a.SessionID)
	cmd := exec.Command(r.Command, r.args(a, workDir, mcpConfig, prompt, systemPrompt, resume, model)...)
	cmd.Dir = workDir
	cmd.Env = agentEnv(os.Environ(), a.Role != roleOrchestrator)
	cmd.Stdout, cmd.Stderr = out, out // the child writes straight to the file ...
	if r.OnLine != nil {              // ... unless lines are wanted: then through the tee
		lw := &lineWriter{w: out, onLine: func(l []byte) { r.OnLine(a, l) }}
		cmd.Stdout, cmd.Stderr = lw, lw
		cmd.WaitDelay = time.Second // a grandchild holding the pipe must not delay the exit record
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdin *stdinPipe
	if r.stdinAgent(a) {
		w, err := cmd.StdinPipe()
		if err != nil {
			out.Close()
			return 0, nil, err
		}
		stdin = &stdinPipe{w: w}
	}
	// Check, start and register under one lock so StopAll cannot miss a process.
	r.mu.Lock()
	sr := r.session(a.SessionID)
	if r.refusing(a.SessionID) {
		r.mu.Unlock()
		out.Close()
		return 0, nil, errors.New("runner is stopping")
	}
	if err := cmd.Start(); err != nil {
		r.mu.Unlock()
		out.Close()
		return 0, nil, err
	}
	pid := cmd.Process.Pid
	exited := make(chan struct{})
	code := make(chan exit, 1)
	p := &proc{sessionID: a.SessionID, agentID: a.ID, exited: exited, stdin: stdin}
	r.running[pid] = p
	sr.wg.Add(1)
	r.mu.Unlock()
	go func() {
		cmd.Wait()
		out.Close() // all output is copied once Wait returns
		// Forget the pid at once: it may be reused and must not be signalled again.
		r.mu.Lock()
		delete(r.running, pid)
		killed := p.killed
		r.mu.Unlock()
		close(exited)
		code <- exit{cmd.ProcessState.ExitCode(), killed} // -1 if killed by a signal
	}()
	return pid, code, nil
}

// refusing reports whether no new process may start for the session; the caller holds r.mu.
func (r *Runner) refusing(sessionID int64) bool {
	return r.stopping || r.session(sessionID).stopped
}

// finish records the outcome of an agent process (or of a failed launch, code
// -1). A process the runner killed on purpose (stopped) is recorded as
// "stopped" and its unfinished task becomes "blocked", as after a crash (a
// task already done stays done); one that exited 0 anyway is just "exited".
func (r *Runner) finish(a Agent, taskTitle string, code int, launchErr string, stopped bool) {
	status, taskStatus := "crashed", "blocked"
	switch {
	case code == 0:
		status, taskStatus = "exited", "done"
	case stopped:
		status = "stopped"
		if t, err := r.store.GetTask(a.SessionID, a.TaskID); err == nil && t.Status == "done" {
			taskStatus = ""
		}
	}
	payload := map[string]any{"role": a.Role, "status": status, "exit_code": code}
	if launchErr != "" {
		payload["error"] = launchErr
	}
	if err := r.store.SetAgentStatus(a.SessionID, a.ID, status, &code); err != nil {
		payload["store_error"] = err.Error()
	}
	if a.Role == "subagent" {
		payload["task_id"] = a.TaskID
		if taskStatus != "" {
			if err := r.store.SetTaskStatus(a.SessionID, a.TaskID, taskStatus); err != nil {
				payload["store_error"] = err.Error()
			}
			payload["task_status"] = taskStatus
		}
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

// StopSession refuses further launches for the session (until
// ResumeOrchestrator), signals its processes as StopAll does, and returns once they have
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
			p.killed = true
		}
	}
	r.mu.Unlock()

	for pid := range procs {
		r.signal(pid, syscall.SIGTERM)
	}
	expired := make(chan struct{})
	timer := time.AfterFunc(killAfter, func() { close(expired) })
	defer timer.Stop()
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

// ErrNotRunning is returned by SendUser when the agent has no live process taking messages.
var ErrNotRunning = errors.New("agent is not running")

// Running reports whether the agent has a live process that SendUser can reach.
func (r *Runner) Running(agentID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.running {
		if p.agentID == agentID && p.stdin != nil {
			return true
		}
	}
	return false
}

// SendUser writes text to the agent's stdin as a stream-json user message
// (one JSON object per line), the way Claude Code takes further chat turns.
func (r *Runner) SendUser(agentID int64, text string) error {
	return r.SendUserContent(agentID, []map[string]any{{"type": "text", "text": text}})
}

// SendUserContent is SendUser with the message's content blocks given as is
// (text, image, document). Stdin lines have no size cap; a message with large
// attachments is one long line.
func (r *Runner) SendUserContent(agentID int64, blocks []map[string]any) error {
	if len(blocks) == 0 {
		return errors.New("message has no content")
	}
	r.mu.Lock()
	var in *stdinPipe
	for _, p := range r.running {
		if p.agentID == agentID {
			in = p.stdin
		}
	}
	r.mu.Unlock()
	if in == nil {
		return ErrNotRunning
	}
	line, err := json.Marshal(map[string]any{"type": "user", "message": map[string]any{
		"role": "user", "content": blocks}})
	if err != nil {
		return err
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if _, err := in.w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("%w: %v", ErrNotRunning, err)
	}
	return nil
}

// maxLine bounds one buffered output line; longer lines are still written to the
// log but not passed to the line hook.
const maxLine = 32 << 20

// lineWriter copies everything to w and hands each complete line to onLine.
type lineWriter struct {
	w      io.Writer
	onLine func([]byte)
	buf    []byte
	skip   bool // inside an over-long line
}

func (l *lineWriter) Write(p []byte) (int, error) {
	n, err := l.w.Write(p)
	for rest := p; len(rest) > 0; {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			if !l.skip {
				if l.buf = append(l.buf, rest...); len(l.buf) > maxLine {
					l.buf, l.skip = nil, true
				}
			}
			break
		}
		if !l.skip {
			l.buf = append(l.buf, rest[:i]...)
			if len(l.buf) > 0 {
				l.onLine(l.buf)
			}
		}
		l.buf, l.skip = l.buf[:0], false
		rest = rest[i+1:]
	}
	return n, err
}
