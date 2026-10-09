package notes

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// agyClient adapts one Antigravity CLI process (agy 1.3+, run as
// `agy --input-format stream-json --output-format stream-json`) to the Fragile
// stream-json envelopes the UI and store use, the way codexClient does for
// Codex. agy reads one {"event":"user",...} line per turn and prints "init",
// "step_update" and one "result" event per turn; it also prints plain-text
// warnings and errors, which are kept as system lines. The orchestrator stays
// alive between turns; workers are stopped after their first turn.
type agyClient struct {
	in       io.WriteCloser
	emit     func(map[string]any)
	stop     func()
	model    string
	mu       sync.Mutex
	turnDone chan struct{}
	success  bool
	failed   bool
	done     chan struct{}
	once     sync.Once
	inputs   chan []map[string]any

	// Touched only by the output reader (line and finish).
	conv     string
	text     map[int]string // agent_response text_delta pieces by step index
	tools    map[int]bool   // tool steps already announced as tool_use
	sawText  bool           // an assistant text was emitted this turn
	lastNote string         // the last non-JSON line, for a turn that ends without a result
}

func (c *agyClient) close() { c.once.Do(func() { close(c.done) }) }

// fail reports a turn that ended without agy's own result.
func (c *agyClient) fail(err error) {
	c.mu.Lock()
	c.failed = true
	c.mu.Unlock()
	c.emit(map[string]any{"type": "result", "subtype": "error", "is_error": true, "result": err.Error(), "provider": ProviderAGY})
}

func (c *agyClient) enqueue(input []map[string]any) error {
	select {
	case <-c.done:
		return ErrNotRunning
	default:
	}
	select {
	case c.inputs <- input:
		return nil
	default:
		return errors.New("Antigravity has too many queued messages; wait for the current turn")
	}
}

// Write takes the runner's stream-json user lines (see SendUserContent).
// Interrupts never get here: Runner.Interrupt refuses Antigravity agents.
func (c *agyClient) Write(p []byte) (int, error) {
	var v struct {
		Type    string `json:"type"`
		Message struct {
			Content []map[string]any `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(p, &v); err != nil {
		return 0, err
	}
	if v.Type != "user" {
		return 0, fmt.Errorf("Antigravity does not support %q messages", v.Type)
	}
	if err := c.enqueue(v.Message.Content); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *agyClient) Close() error { return c.in.Close() }

// agyInput keeps the text blocks of a message. Image and document blocks are
// dropped with a note in the text: agy's stream-json image input is untested.
func agyInput(blocks []map[string]any) []map[string]any {
	var out []map[string]any
	dropped := 0
	for _, b := range blocks {
		if t, ok := b["text"].(string); ok && b["type"] == "text" {
			out = append(out, map[string]any{"type": "text", "text": t})
		} else {
			dropped++
		}
	}
	if dropped > 0 {
		out = append(out, map[string]any{"type": "text", "text": fmt.Sprintf("[Fragile: %d attachment(s) were not sent; Antigravity agents receive text only.]", dropped)})
	}
	return out
}

func (c *agyClient) run(prompt string, interactive bool) {
	if prompt != "" {
		if err := c.enqueue([]map[string]any{{"type": "text", "text": prompt}}); err != nil {
			c.fail(err)
			c.stop()
			return
		}
	}
	for {
		var input []map[string]any
		select {
		case input = <-c.inputs:
		case <-c.done:
			return
		}
		if len(input) == 1 && input[0]["type"] == "text" && input[0]["text"] == "/compact" {
			c.fail(errors.New("Antigravity has no compact command"))
			if !interactive {
				c.stop()
				return
			}
			continue
		}
		finished := make(chan struct{})
		c.mu.Lock()
		c.turnDone = finished
		c.mu.Unlock()
		line, _ := json.Marshal(map[string]any{"event": "user", "message": map[string]any{"role": "user", "content": agyInput(input)}})
		if _, err := c.in.Write(append(line, '\n')); err != nil {
			c.mu.Lock()
			c.turnDone = nil
			c.mu.Unlock()
			c.fail(err)
			c.stop()
			return
		}
		select {
		case <-finished:
		case <-c.done:
			return
		}
		if !interactive {
			c.stop()
			return
		}
	}
}

type agyUsage struct {
	Input     int `json:"input_tokens"`
	Output    int `json:"output_tokens"`
	Thinking  int `json:"thinking_tokens"`
	CacheRead int `json:"cache_read_tokens"`
	Total     int `json:"total_tokens"`
}

type agyEvent struct {
	Event          string `json:"event"`
	ConversationID string `json:"conversation_id"`
	Init           struct {
		Cwd   string   `json:"cwd"`
		Tools []string `json:"tools"`
	} `json:"init"`
	Step struct {
		ConversationID string         `json:"conversation_id"`
		StepIndex      int            `json:"step_index"`
		State          string         `json:"state"`
		StepType       string         `json:"step_type"`
		TextDelta      string         `json:"text_delta"`
		ToolName       string         `json:"tool_name"`
		Output         any            `json:"output"`
		Error          any            `json:"error"`
		ToolInfo       map[string]any `json:"tool_info"`
		Usage          *agyUsage      `json:"usage"`
	} `json:"step_update"`
	Result struct {
		ConversationID string    `json:"conversation_id"`
		Status         string    `json:"status"`
		Response       string    `json:"response"`
		Error          string    `json:"error"`
		NumTurns       int       `json:"num_turns"`
		Usage          *agyUsage `json:"usage"`
		DeniedActions  []struct {
			Action      string `json:"action"`
			DisplayName string `json:"display_name"`
		} `json:"denied_actions"`
	} `json:"result"`
}

func (c *agyClient) assistant(block map[string]any) {
	c.emit(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{block}}})
}

func (c *agyClient) line(raw []byte) {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return
	}
	var v agyEvent
	if json.Unmarshal(raw, &v) != nil || v.Event == "" {
		// Plain-text warnings, errors and permission notices from agy.
		c.lastNote = string(raw)
		c.emit(map[string]any{"type": "system", "subtype": "stderr", "provider": ProviderAGY, "text": string(raw)})
		return
	}
	switch v.Event {
	case "init":
		c.conv = v.ConversationID
		ev := map[string]any{"type": "system", "subtype": "init", "session_id": v.ConversationID, "provider": ProviderAGY, "cwd": v.Init.Cwd, "tools": v.Init.Tools}
		if c.model != "" {
			ev["model"] = c.model
		}
		c.emit(ev)
	case "step_update":
		c.step(v)
	case "result":
		c.finish(v)
	default:
		c.emit(map[string]any{"type": "system", "subtype": "agy_event", "provider": ProviderAGY, "event": json.RawMessage(raw)})
	}
}

func (c *agyClient) step(v agyEvent) {
	s := v.Step
	if c.conv == "" {
		c.conv = s.ConversationID
	}
	switch s.StepType {
	case "agent_response":
		c.text[s.StepIndex] += s.TextDelta
		if s.State == "ACTIVE" {
			return
		}
		if t := c.text[s.StepIndex]; strings.TrimSpace(t) != "" {
			c.sawText = true
			c.assistant(map[string]any{"type": "text", "text": strings.TrimRight(t, "\n")})
		}
		delete(c.text, s.StepIndex)
		if s.Usage != nil {
			c.emit(map[string]any{"type": "system", "subtype": "context_usage", "provider": ProviderAGY, "context_used": s.Usage.Input + s.Usage.CacheRead + s.Usage.Output})
		}
	case "tool":
		id := "agy-" + c.conv + "-" + strconv.Itoa(s.StepIndex)
		if !c.tools[s.StepIndex] {
			c.tools[s.StepIndex] = true
			name := s.ToolName
			input := map[string]any{}
			if p, ok := s.ToolInfo["parameters"].(map[string]any); ok {
				input = p
			}
			if n, ok := s.ToolInfo["name"].(string); ok && name == "" {
				name = n
			}
			if cmd, ok := input["CommandLine"]; ok {
				input["command"] = cmd // what the chat summary shows
			}
			c.assistant(map[string]any{"type": "tool_use", "id": id, "name": name, "input": input})
		}
		if s.State == "ACTIVE" {
			return
		}
		content, failed := s.Output, s.State != "DONE"
		if e, ok := s.Error.(string); s.Error != nil && (!ok || e != "") {
			content, failed = s.Error, true
		}
		if content == nil {
			content = strings.ToLower(s.State)
		}
		c.emit(map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": codexToolText(content), "is_error": failed}}}})
	}
}

// finish handles a turn's result event.
func (c *agyClient) finish(v agyEvent) {
	c.mu.Lock()
	finished := c.turnDone
	c.turnDone = nil
	c.mu.Unlock()
	if finished == nil {
		// A result with no turn in progress is the shutdown echo of an ended turn; ignore it.
		return
	}

	r := v.Result
	if r.ConversationID != "" {
		c.conv = r.ConversationID
	}
	if strings.TrimSpace(r.Response) != "" && !c.sawText {
		c.assistant(map[string]any{"type": "text", "text": r.Response})
	}
	if len(r.DeniedActions) > 0 {
		var names []string
		for _, d := range r.DeniedActions {
			names = append(names, d.DisplayName+" ("+d.Action+")")
		}
		c.assistant(map[string]any{"type": "text", "text": "[Antigravity auto-denied: " + strings.Join(names, ", ") + "]"})
	}
	failed := r.Status != "SUCCESS"
	out := map[string]any{"type": "result", "subtype": "success", "is_error": failed, "result": r.Response, "session_id": c.conv, "num_turns": r.NumTurns, "provider": ProviderAGY}
	if r.Usage != nil {
		out["usage"] = r.Usage
	}
	if failed {
		out["subtype"] = "error"
		msg := r.Error
		if msg == "" {
			msg = "Antigravity turn " + strings.ToLower(r.Status)
		}
		out["result"] = msg
	}
	c.emit(out)
	c.text, c.tools, c.sawText = map[int]string{}, map[int]bool{}, false
	c.mu.Lock()
	c.failed, c.success = failed, !failed
	c.mu.Unlock()
	close(finished)
}

// eof ends a turn agy left without a result (it exited, e.g. on an auth error).
func (c *agyClient) eof() {
	c.mu.Lock()
	finished := c.turnDone
	c.turnDone = nil
	c.mu.Unlock()
	if finished == nil {
		return
	}
	msg := "Antigravity exited before the turn finished"
	if c.lastNote != "" {
		msg += ": " + c.lastNote
	}
	c.fail(errors.New(msg))
	close(finished)
}

// agyArgs builds the agy command line. Messages always arrive on stdin (also
// a worker's single task), so no prompt is on the command line. --sandbox
// applies agy's terminal restrictions to the commands the settings allow.
func agyArgs(resume, model string) []string {
	args := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--sandbox"}
	if model != "" {
		args = append(args, "--model", model)
	}
	if resume != "" {
		args = append(args, "--conversation", resume)
	}
	return args
}

// agySubagentHook makes agy refuse its native sub-agent tools; delegation goes
// through Fragile's spawn_subagent. Schema: antigravity-cli builtin docs hooks.md.
var agySubagentHook = map[string]any{"fragile-no-native-subagents": map[string]any{
	"PreToolUse": []any{map[string]any{
		"matcher": "(invoke|define|manage)_subagents?",
		"hooks": []any{map[string]any{"type": "command", "timeout": 10,
			"command": `echo '{"decision":"deny","reason":"Native sub-agents are disabled; use the Fragile spawn_subagent tool."}'`}},
	}},
}}

// agyHome builds the agent's private HOME. agy reads its system prompt
// (GEMINI.md), MCP servers, hooks and permissions only from files under
// ~/.gemini, so each agent gets its own tree that links back to the user's
// login and conversation store (so --conversation can resume) and overrides
// the rest. The tree is rebuilt on every launch.
func (r *Runner) agyHome(a Agent, systemPrompt, workDir string) (string, error) {
	home := filepath.Join(r.cfg.AgentDir, "agent-"+strconv.FormatInt(a.ID, 10)+".home")
	if err := os.RemoveAll(home); err != nil { // removes links, never their targets
		return "", err
	}
	g := filepath.Join(home, ".gemini")
	cli := filepath.Join(g, "antigravity-cli")
	for _, d := range []string{filepath.Join(g, "config"), cli} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return "", err
		}
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	userGemini := filepath.Join(userHome, ".gemini")
	realCLI := filepath.Join(userGemini, "antigravity-cli")

	normal := r.normalOrchestrator(a) // no Fragile tools and no orchestrator prompt
	prompt := systemPrompt
	servers := map[string]any{}
	if !normal {
		prompt = agyPrompt(a, systemPrompt)
		servers["fragile"] = map[string]string{"url": fmt.Sprintf("http://%s/mcp/%s", r.cfg.Addr, a.Token)}
	}
	if b, err := os.ReadFile(filepath.Join(userGemini, "GEMINI.md")); err == nil {
		prompt += "\n\n" + string(b)
	}
	mcp, _ := json.Marshal(map[string]any{"mcpServers": servers})
	hooks, _ := json.MarshalIndent(agySubagentHook, "", "  ")
	settings, err := agySettings(filepath.Join(realCLI, "settings.json"), workDir)
	if err != nil {
		return "", err
	}
	for name, data := range map[string][]byte{
		"GEMINI.md":                     []byte(prompt),
		"config/mcp_config.json":        mcp,
		"config/hooks.json":             hooks,
		"antigravity-cli/settings.json": settings,
	} {
		if err := os.WriteFile(filepath.Join(g, name), data, 0o600); err != nil {
			return "", err
		}
	}

	link := func(target, name string) error {
		if _, err := os.Lstat(target); err != nil {
			return nil // nothing to share (e.g. not logged in yet)
		}
		return os.Symlink(target, name)
	}
	if err := link(filepath.Join(userGemini, "config", "config.json"), filepath.Join(g, "config", "config.json")); err != nil {
		return "", err
	}
	if err := link(filepath.Join(userGemini, "antigravity"), filepath.Join(g, "antigravity")); err != nil {
		return "", err
	}
	// Conversations must land in the user's store for a later resume (a resumed
	// orchestrator is a new agent with a new HOME), so make sure they are links.
	if _, err := os.Stat(realCLI); err == nil {
		for _, d := range []string{"conversations", "brain"} {
			os.MkdirAll(filepath.Join(realCLI, d), 0o700)
		}
	}
	entries, _ := os.ReadDir(realCLI)
	for _, e := range entries {
		if e.Name() == "settings.json" {
			continue
		}
		if err := link(filepath.Join(realCLI, e.Name()), filepath.Join(cli, e.Name())); err != nil {
			return "", err
		}
	}
	return home, nil
}

// agySettings is the user's antigravity-cli settings.json with permission
// rules added so a headless agent can run commands (under --sandbox) and edit
// files in the work dir, the Fragile MCP server allowed and the work dir trusted.
// Headless agy auto-denies anything not allowed. Rule syntax: command(...),
// write_file(path), mcp(server/tool), per the CLI's own docs and error text.
func agySettings(path, workDir string) ([]byte, error) {
	s := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &s); err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	perms, _ := s["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
	}
	allow, _ := perms["allow"].([]any)
	for _, rule := range []string{"command(*)", "read_file(" + workDir + ")", "write_file(" + workDir + ")", "mcp(fragile/*)"} {
		if !slices.Contains(allow, any(rule)) {
			allow = append(allow, rule)
		}
	}
	perms["allow"] = allow
	s["permissions"] = perms
	trusted, _ := s["trustedWorkspaces"].([]any)
	if !slices.Contains(trusted, any(workDir)) {
		trusted = append(trusted, workDir)
	}
	s["trustedWorkspaces"] = trusted
	return json.MarshalIndent(s, "", "  ")
}

func (r *Runner) startAGY(a Agent, logPath, prompt, systemPrompt, resume, model string) (int, <-chan exit, error) {
	r.mu.Lock()
	refused := r.refusing(a.SessionID)
	r.mu.Unlock()
	if refused {
		return 0, nil, errors.New("runner is stopping")
	}
	workDir := r.workDir(a.SessionID)
	home, err := r.agyHome(a, systemPrompt, workDir)
	if err != nil {
		return 0, nil, fmt.Errorf("preparing the Antigravity home: %w", err)
	}
	out, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, nil, err
	}
	cmd := exec.Command(r.AGYCommand, agyArgs(resume, model)...)
	cmd.Dir = workDir
	cmd.Env = agyEnv(os.Environ(), home)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		out.Close()
		return 0, nil, err
	}
	// agy prints plain-text notices (permission denials, errors) on either
	// stream; one pipe for both keeps them in order with the events.
	pr, pw, err := os.Pipe()
	if err != nil {
		in.Close()
		out.Close()
		return 0, nil, err
	}
	cmd.Stdout, cmd.Stderr = pw, pw

	var outputMu sync.Mutex
	emit := func(v map[string]any) {
		data, _ := json.Marshal(v)
		outputMu.Lock()
		defer outputMu.Unlock()
		out.Write(append(data, '\n'))
		if r.OnLine != nil {
			r.OnLine(a, data)
		}
	}
	client := &agyClient{in: in, emit: emit, model: model, done: make(chan struct{}), inputs: make(chan []map[string]any, 32),
		text: map[int]string{}, tools: map[int]bool{}}

	r.mu.Lock()
	sr := r.session(a.SessionID)
	if r.refusing(a.SessionID) {
		r.mu.Unlock()
		in.Close()
		pr.Close()
		pw.Close()
		out.Close()
		return 0, nil, errors.New("runner is stopping")
	}
	err = cmd.Start()
	pw.Close() // the child holds its own copy
	if err != nil {
		r.mu.Unlock()
		in.Close()
		pr.Close()
		out.Close()
		return 0, nil, err
	}
	pid := cmd.Process.Pid
	exited := make(chan struct{})
	code := make(chan exit, 1)
	p := &proc{sessionID: a.SessionID, agentID: a.ID, exited: exited, agy: client}
	if r.stdinAgent(a) {
		p.stdin = &stdinPipe{w: client}
	}
	r.running[pid] = p
	sr.wg.Add(1)
	r.mu.Unlock()
	client.stop = func() { r.signal(pid, syscall.SIGTERM) }

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		reader := bufio.NewReaderSize(pr, 64<<10)
		var line []byte
		skipping := false
		for {
			fragment, err := reader.ReadSlice('\n')
			if !skipping {
				if len(line)+len(fragment) > maxLine {
					line = nil
					skipping = true
				} else {
					line = append(line, fragment...)
				}
			}
			if err == bufio.ErrBufferFull {
				continue
			}
			if !skipping && len(line) > 0 {
				client.line(line)
			}
			line = nil
			skipping = false
			if err != nil {
				break
			}
		}
		client.eof()
		client.close()
	}()

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		client.run(prompt, r.stdinAgent(a))
	}()

	go func() {
		cmd.Wait()
		// A grandchild may still hold the output pipe; do not wait on it for long.
		select {
		case <-readDone:
		case <-time.After(time.Second):
			pr.Close()
			<-readDone
		}
		pr.Close()
		client.close()
		<-runDone
		in.Close()
		outputMu.Lock()
		out.Close()
		outputMu.Unlock()
		r.mu.Lock()
		delete(r.running, pid)
		killed := p.killed
		r.mu.Unlock()
		status := cmd.ProcessState.ExitCode()
		client.mu.Lock()
		if client.failed && !killed {
			status = 1
		}
		if !r.stdinAgent(a) && client.success && !killed {
			status = 0
		}
		client.mu.Unlock()
		close(exited)
		code <- exit{status, killed}
	}()
	return pid, code, nil
}
