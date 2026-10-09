package notes

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// agyClient coordinates one Antigravity (agy) process, adapting stream-json and
// JSON-RPC lines into the Fragile stream-json envelopes used by the UI and store.
type agyClient struct {
	in         io.WriteCloser
	emit       func(map[string]any)
	stop       func()
	mu         sync.Mutex
	writeMu    sync.Mutex
	compacting bool
	lastText   string
	turnDone   chan struct{}
	success    bool
	failed     bool
	done       chan struct{}
	once       sync.Once
	inputs     chan []map[string]any
	sessionID  string
}

func (c *agyClient) close() { c.once.Do(func() { close(c.done) }) }

func (c *agyClient) write(line []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.in.Write(append(line, '\n'))
	return err
}

func (c *agyClient) result(err error) {
	c.mu.Lock()
	c.failed = err != nil
	text := c.lastText
	c.mu.Unlock()
	v := map[string]any{
		"type":     "result",
		"subtype":  "success",
		"is_error": err != nil,
		"result":   text,
		"provider": "agy",
	}
	if err != nil {
		v["subtype"] = "error"
		v["result"] = err.Error()
	}
	c.emit(v)
}

func (c *agyClient) fail(err error) {
	c.result(err)
	if c.stop != nil {
		c.stop()
	}
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

func (c *agyClient) Write(p []byte) (int, error) {
	var v struct {
		Type    string `json:"type"`
		Message struct {
			Content []map[string]any `json:"content"`
		} `json:"message"`
		Request struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	if err := json.Unmarshal(p, &v); err != nil {
		return 0, err
	}
	if v.Type == "control_request" && v.Request.Subtype == "interrupt" {
		c.interrupt()
		return len(p), nil
	}
	if err := c.enqueue(v.Message.Content); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *agyClient) Close() error {
	c.close()
	return c.in.Close()
}

func (c *agyClient) interrupt() {
	// Send stream-json interrupt control request
	req, _ := json.Marshal(map[string]any{
		"type":    "control_request",
		"request": map[string]string{"subtype": "interrupt"},
	})
	_ = c.write(req)
}

func (c *agyClient) run(a Agent, dir, prompt, systemPrompt, resume, model string, interactive bool) {
	sessionID := resume
	if sessionID == "" {
		sessionID = fmt.Sprintf("agy-session-%d", a.ID)
	}
	c.mu.Lock()
	c.sessionID = sessionID
	c.mu.Unlock()

	c.emit(map[string]any{
		"type":       "system",
		"subtype":    "init",
		"session_id": sessionID,
		"model":      model,
		"provider":   "agy",
	})

	if prompt != "" {
		if err := c.enqueue([]map[string]any{{"type": "text", "text": prompt}}); err != nil {
			c.fail(err)
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

		compact := len(input) == 1 && input[0]["type"] == "text" && input[0]["text"] == "/compact"
		c.mu.Lock()
		finished := make(chan struct{})
		c.turnDone = finished
		c.compacting = compact
		c.lastText = ""
		c.mu.Unlock()

		line, err := json.Marshal(map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": input,
			},
		})
		if err != nil {
			c.fail(err)
			return
		}
		if err := c.write(line); err != nil {
			c.fail(err)
			return
		}

		select {
		case <-finished:
		case <-c.done:
			return
		}

		if !interactive {
			if c.stop != nil {
				c.stop()
			}
			return
		}
	}
}

func (c *agyClient) line(line []byte) {
	line = []byte(strings.TrimSpace(string(line)))
	if len(line) == 0 {
		return
	}

	var raw map[string]any
	if json.Unmarshal(line, &raw) != nil {
		// Non-JSON line: wrap as assistant text if non-empty
		c.emit(map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{map[string]any{"type": "text", "text": string(line)}},
			},
		})
		return
	}

	// 1. Check for standard stream-json event types
	typ, _ := raw["type"].(string)
	switch typ {
	case "assistant", "user", "system":
		// Track text
		if typ == "assistant" {
			if msg, ok := raw["message"].(map[string]any); ok {
				if blocks, ok := msg["content"].([]any); ok {
					for _, blk := range blocks {
						if b, ok := blk.(map[string]any); ok && b["type"] == "text" {
							if txt, ok := b["text"].(string); ok {
								c.mu.Lock()
								c.lastText = txt
								c.mu.Unlock()
							}
						}
					}
				}
			}
		}
		c.emit(raw)
		return
	case "result":
		c.emit(raw)
		c.mu.Lock()
		isErr, _ := raw["is_error"].(bool)
		c.failed = isErr
		c.success = !isErr
		finished := c.turnDone
		c.turnDone = nil
		c.compacting = false
		c.mu.Unlock()
		if finished != nil {
			close(finished)
		}
		return
	}

	// 2. Check for JSON-RPC methods (if agy sends RPC messages)
	method, _ := raw["method"].(string)
	switch method {
	case "turn/started":
		return
	case "turn/completed":
		c.mu.Lock()
		finished := c.turnDone
		c.turnDone = nil
		c.compacting = false
		c.mu.Unlock()
		c.result(nil)
		if finished != nil {
			close(finished)
		}
		return
	case "thread/tokenUsage/updated":
		if params, ok := raw["params"].(map[string]any); ok {
			var used, window int
			if u, ok := params["tokens_used"].(float64); ok {
				used = int(u)
			}
			if w, ok := params["context_window"].(float64); ok {
				window = int(w)
			}
			c.emit(map[string]any{
				"type":           "system",
				"subtype":        "context_usage",
				"provider":       "agy",
				"context_used":   used,
				"context_window": window,
			})
		}
		return
	}

	// Fallback: emit as system message
	c.emit(raw)
}

func (r *Runner) agyArgs(a Agent, mcpConfig, prompt, systemPrompt, resume, model string) []string {
	args := []string{
		"run",
		"--output-format", "stream-json",
		"--append-system-prompt", agyPrompt(a, systemPrompt),
		"--mcp-config", mcpConfig,
		"--disallowed-tools", "invoke_subagent,subagents,Task,Agent,Workflow",
	}
	if r.stdinAgent(a) {
		args = append(args, "--input-format", "stream-json")
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	if !r.stdinAgent(a) && prompt != "" {
		args = append(args, "--", prompt)
	}
	return args
}

func (r *Runner) startAGY(a Agent, mcpConfig, logPath, prompt, systemPrompt, resume, model string) (int, <-chan exit, error) {
	r.mu.Lock()
	refused := r.refusing(a.SessionID)
	r.mu.Unlock()
	if refused {
		return 0, nil, errors.New("runner is stopping")
	}

	url := fmt.Sprintf("http://%s/mcp/%s", r.cfg.Addr, a.Token)
	b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fragile": map[string]string{"type": "http", "url": url}}})
	if err := os.WriteFile(mcpConfig, b, 0o600); err != nil {
		return 0, nil, err
	}

	out, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, nil, err
	}

	workDir := r.workDir(a.SessionID)
	cmd := exec.Command(r.AGYCommand, r.agyArgs(a, mcpConfig, prompt, systemPrompt, resume, model)...)
	cmd.Dir = workDir
	cmd.Env = agyEnv(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	in, err := cmd.StdinPipe()
	if err != nil {
		out.Close()
		return 0, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		out.Close()
		return 0, nil, err
	}

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
	cmd.Stderr = &lockedWriter{mu: &outputMu, w: out}
	cmd.WaitDelay = time.Second

	client := &agyClient{
		in:     in,
		emit:   emit,
		done:   make(chan struct{}),
		inputs: make(chan []map[string]any, 32),
	}

	r.mu.Lock()
	sr := r.session(a.SessionID)
	if r.refusing(a.SessionID) {
		r.mu.Unlock()
		in.Close()
		out.Close()
		return 0, nil, errors.New("runner is stopping")
	}
	if err := cmd.Start(); err != nil {
		r.mu.Unlock()
		in.Close()
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
		reader := bufio.NewReaderSize(stdout, 64<<10)
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
			if err != bufio.ErrBufferFull {
				if !skipping && len(line) > 0 {
					client.line(line)
				}
				line = nil
				skipping = false
			}
			if err == bufio.ErrBufferFull {
				continue
			}
			if err != nil {
				if err != io.EOF {
					client.result(fmt.Errorf("Antigravity output: %w", err))
					client.stop()
				}
				break
			}
		}
		client.close()
	}()

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		client.run(a, workDir, prompt, systemPrompt, resume, model, r.stdinAgent(a))
	}()

	go func() {
		<-readDone
		cmd.Wait()
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
