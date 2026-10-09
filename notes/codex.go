package notes

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Each agent owns one stdio app-server. JSON-RPC is kept out of the application:
// the adapter emits the same stream-json envelopes the existing UI/store uses.
// The orchestrator stays alive between turns; workers exit after one turn.
type codexClient struct {
	in               io.WriteCloser
	emit             func(map[string]any)
	stop             func()
	mu               sync.Mutex
	writeMu          sync.Mutex
	compacting       bool
	interruptPending bool
	seq              int
	pending          map[int]chan rpcReply
	thread, turn     string
	mcpURL           string
	lastText         string
	turnDone         chan struct{}
	success          bool
	failed           bool
	done             chan struct{}
	once             sync.Once
	inputs           chan []map[string]any
}
type rpcReply struct {
	result json.RawMessage
	err    error
}

func (c *codexClient) close() { c.once.Do(func() { close(c.done) }) }
func (c *codexClient) write(v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, e = c.in.Write(append(b, '\n'))
	return e
}
func (c *codexClient) call(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.seq++
	id := c.seq
	ch := make(chan rpcReply, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if e := c.write(map[string]any{"id": id, "method": method, "params": params}); e != nil {
		return nil, e
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case v := <-ch:
		return v.result, v.err
	case <-c.done:
		return nil, ErrNotRunning
	case <-timer.C:
		return nil, fmt.Errorf("Codex %s timed out", method)
	}
}
func (c *codexClient) result(err error) {
	c.mu.Lock()
	c.failed = err != nil
	text := c.lastText
	c.mu.Unlock()
	v := map[string]any{"type": "result", "subtype": "success", "is_error": err != nil, "result": text}
	if err != nil {
		v["subtype"] = "error"
		v["result"] = err.Error()
	}
	c.emit(v)
}
func (c *codexClient) fail(err error) { c.result(err); c.stop() }
func (c *codexClient) run(a Agent, dir, prompt, systemPrompt, resume, model string, interactive bool) {
	_, err := c.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "fragile", "version": "1.0"}})
	if err == nil {
		err = c.write(map[string]string{"method": "initialized"})
	}
	// Codex owns credential storage and refresh. Never read/copy auth.json or
	// accept API credentials as an accidental substitute for ChatGPT plan usage.
	if err == nil {
		var raw json.RawMessage
		raw, err = c.call("account/read", map[string]any{"refreshToken": false})
		if err == nil {
			var v struct {
				Account *struct {
					Type string `json:"type"`
				} `json:"account"`
			}
			if json.Unmarshal(raw, &v) != nil || v.Account == nil || v.Account.Type != "chatgpt" {
				err = errors.New("Codex is not signed in with ChatGPT; run codex login, then retry")
			}
		}
	}
	if err != nil {
		c.fail(err)
		return
	}
	// Disable inherited MCP servers explicitly: config table overlays can merge.
	isolated, err := c.isolatedConfig(dir)
	if err != nil {
		c.fail(err)
		return
	}
	instructions := systemPrompt
	if c.mcpURL != "" {
		instructions = codexPrompt(a, systemPrompt)
	}
	params := map[string]any{"cwd": dir, "approvalPolicy": "never", "developerInstructions": instructions, "config": isolated}
	preferred, _ := isolated["model"].(string)
	selected, err := c.selectModel(model, preferred)
	if err != nil {
		c.fail(err)
		return
	}
	params["model"] = selected
	delete(isolated, "model")
	method := "thread/start"
	if resume != "" {
		method = "thread/resume"
		params["threadId"] = resume
	}
	raw, err := c.call(method, params)
	if err != nil {
		c.fail(err)
		return
	}
	var v struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model string `json:"model"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Thread.ID == "" {
		c.fail(errors.New("Codex returned no thread id"))
		return
	}
	c.mu.Lock()
	c.thread = v.Thread.ID
	c.mu.Unlock()
	c.emit(map[string]any{"type": "system", "subtype": "init", "session_id": v.Thread.ID, "model": v.Model, "provider": "codex"})
	if prompt != "" {
		if err = c.enqueue([]map[string]any{{"type": "text", "text": prompt}}); err != nil {
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
		c.turn = ""
		c.compacting = compact
		c.lastText = ""
		c.mu.Unlock()
		if compact {
			raw, err = c.call("thread/compact/start", map[string]any{"threadId": v.Thread.ID})
		} else {
			raw, err = c.call("turn/start", map[string]any{"threadId": v.Thread.ID, "input": input})
		}
		if err != nil {
			c.mu.Lock()
			c.turnDone = nil
			c.compacting = false
			c.mu.Unlock()
			c.result(err)
			if !interactive {
				c.stop()
				return
			}
			continue
		}
		var started struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		json.Unmarshal(raw, &started)
		c.mu.Lock()
		select {
		case <-finished:
		default:
			if started.Turn.ID != "" {
				c.turn = started.Turn.ID
			}
		}
		c.mu.Unlock()
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

// config/read is local configuration only; never log its values (they may
// contain private connector headers). Only names are used to disable servers.
func (c *codexClient) isolatedConfig(dir string) (map[string]any, error) {
	raw, err := c.call("config/read", map[string]any{"includeLayers": false, "cwd": dir})
	if err != nil {
		return nil, err
	}
	var v struct {
		Config struct {
			MCP   map[string]json.RawMessage `json:"mcp_servers"`
			Model string                     `json:"model"`
		} `json:"config"`
	}
	if err = json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	config := map[string]any{"model": v.Config.Model, "features.multi_agent": false, "features.plugins": false, "features.memories": false, "features.apps": false, "features.hooks": false, "web_search": "disabled"}
	if c.mcpURL != "" { // empty: a normal-mode agent, which gets no Fragile tools
		config["mcp_servers.fragile.url"], config["mcp_servers.fragile.enabled"], config["mcp_servers.fragile.required"] = c.mcpURL, true, true
	}
	for name := range v.Config.MCP {
		if name != "fragile" || c.mcpURL == "" {
			if strings.ContainsAny(name, ".\" \t\n") {
				return nil, errors.New("cannot isolate an MCP server with an unsupported name; rename it in Codex config")
			}
			config["mcp_servers."+name+".enabled"] = false
		}
	}
	return config, nil
}

// Ask the authenticated CLI for its catalog instead of sending a configured
// API-only model (or a Claude alias) to a ChatGPT account. No hardcoded model ids.
func (c *codexClient) selectModel(requested, preferred string) (string, error) {
	raw, err := c.call("model/list", map[string]any{"includeHidden": true})
	if err != nil {
		return "", err
	}
	var catalog struct {
		Data []struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Default bool   `json:"isDefault"`
		} `json:"data"`
	}
	if err = json.Unmarshal(raw, &catalog); err != nil {
		return "", err
	}
	fallback := ""
	for _, m := range catalog.Data {
		id := m.Model
		if id == "" {
			id = m.ID
		}
		if requested != "" && (requested == id || requested == m.ID) {
			return id, nil
		}
		if requested == "" && preferred != "" && (preferred == id || preferred == m.ID) {
			return id, nil
		}
		if m.Default || fallback == "" {
			fallback = id
		}
	}
	if requested != "" {
		return "", fmt.Errorf("Codex model %q is not available in the CLI catalog for this account", requested)
	}
	if fallback == "" {
		return "", errors.New("Codex reported no available models")
	}
	return fallback, nil
}

func (c *codexClient) enqueue(input []map[string]any) error {
	select {
	case <-c.done:
		return ErrNotRunning
	default:
	}
	select {
	case c.inputs <- input:
		return nil
	default:
		return errors.New("Codex has too many queued messages; wait for the current turn")
	}
}
func (c *codexClient) Write(p []byte) (int, error) {
	var v struct {
		Type    string `json:"type"`
		Message struct {
			Content []map[string]any `json:"content"`
		} `json:"message"`
		Request struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	if e := json.Unmarshal(p, &v); e != nil {
		return 0, e
	}
	if v.Type == "control_request" && v.Request.Subtype == "interrupt" {
		c.mu.Lock()
		thread, turn := c.thread, c.turn
		c.mu.Unlock()
		if turn == "" {
			c.mu.Lock()
			c.interruptPending = true
			c.mu.Unlock()
		} else {
			// Do not hold the caller's stdin mutex while waiting on an RPC reply.
			go c.interrupt(thread, turn)
		}
		return len(p), nil
	}
	input, e := codexInput(v.Message.Content)
	if e != nil {
		return 0, e
	}
	if e = c.enqueue(input); e != nil {
		return 0, e
	}
	return len(p), nil
}
func (c *codexClient) Close() error { return c.in.Close() }
func codexInput(blocks []map[string]any) ([]map[string]any, error) {
	var out []map[string]any
	for _, b := range blocks {
		switch b["type"] {
		case "text":
			out = append(out, map[string]any{"type": "text", "text": b["text"]})
		case "image":
			s, ok := b["source"].(map[string]any)
			if !ok {
				return nil, errors.New("invalid image source")
			}
			mt, _ := s["media_type"].(string)
			data, _ := s["data"].(string)
			if mt == "" || data == "" {
				return nil, errors.New("invalid image source")
			}
			out = append(out, map[string]any{"type": "image", "url": "data:" + mt + ";base64," + data})
		default:
			return nil, errors.New("Codex supports text and image attachments, not PDFs; send extracted text instead")
		}
	}
	if len(out) == 0 {
		return nil, errors.New("message has no content")
	}
	return out, nil
}

func (c *codexClient) line(line []byte) {
	var v struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(line, &v) != nil {
		return
	}
	if len(v.ID) > 0 && v.Method == "" {
		var id int
		if json.Unmarshal(v.ID, &id) != nil {
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		c.mu.Unlock()
		if ch != nil {
			reply := rpcReply{result: v.Result}
			if v.Error != nil {
				reply.err = errors.New(v.Error.Message)
			}
			select {
			case ch <- reply:
			default:
			}
		}
		return
	}
	if len(v.ID) > 0 && v.Method != "" {
		// No approval bypass: the unattended runner declines permission requests.
		// Other server requests receive a method error rather than hanging forever.
		if strings.HasSuffix(v.Method, "requestApproval") {
			c.write(map[string]any{"id": v.ID, "result": map[string]string{"decision": "decline"}})
		} else {
			c.write(map[string]any{"id": v.ID, "error": map[string]any{"code": -32601, "message": "Fragile does not handle this interactive request; escalate through the Fragile board"}})
		}
		return
	}
	var p struct {
		Item map[string]any `json:"item"`
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
	}
	json.Unmarshal(v.Params, &p)
	switch v.Method {
	case "turn/started":
		c.mu.Lock()
		if c.turnDone == nil || (c.turn != "" && c.turn != p.Turn.ID) {
			c.mu.Unlock()
			return
		}
		c.turn = p.Turn.ID
		interrupt := c.interruptPending
		c.interruptPending = false
		thread := c.thread
		c.mu.Unlock()
		if interrupt {
			go c.interrupt(thread, p.Turn.ID)
		}
	case "turn/completed":
		c.mu.Lock()
		if c.turnDone == nil || c.turn == "" || c.turn != p.Turn.ID {
			c.mu.Unlock()
			return
		}
		finished := c.turnDone
		c.turnDone = nil
		c.turn = ""
		c.compacting = false
		c.mu.Unlock()
		var err error
		if p.Turn.Status == "interrupted" {
			c.mu.Lock()
			c.failed = false
			c.success = true
			c.mu.Unlock()
			c.emit(map[string]any{"type": "result", "subtype": "error_during_execution", "is_error": false, "result": ""})
		} else {
			if p.Turn.Status != "completed" {
				err = errors.New("Codex turn " + p.Turn.Status)
				if p.Turn.Error != nil {
					err = errors.New(p.Turn.Error.Message)
				}
			}
			c.result(err)
			c.mu.Lock()
			c.success = err == nil
			c.mu.Unlock()
		}
		close(finished)
	case "item/started", "item/completed":
		c.item(v.Method == "item/completed", p.Item)
	case "thread/tokenUsage/updated":
		var usage struct {
			TokenUsage struct {
				ModelContextWindow int `json:"modelContextWindow"`
				Last               struct {
					Input  int `json:"inputTokens"`
					Output int `json:"outputTokens"`
				} `json:"last"`
			} `json:"tokenUsage"`
		}
		if json.Unmarshal(v.Params, &usage) == nil {
			c.emit(map[string]any{"type": "system", "subtype": "context_usage", "provider": "codex", "context_used": usage.TokenUsage.Last.Input + usage.TokenUsage.Last.Output, "context_window": usage.TokenUsage.ModelContextWindow})
		}
	case "thread/compacted":
		c.mu.Lock()
		manual := c.compacting
		c.mu.Unlock()
		trigger := "auto"
		if manual {
			trigger = "manual"
		}
		c.emit(map[string]any{"type": "system", "subtype": "compact_boundary", "provider": "codex", "compact_metadata": map[string]any{"trigger": trigger}})
	}
}
func (c *codexClient) item(completed bool, item map[string]any) {
	typ, _ := item["type"].(string)
	id, _ := item["id"].(string)
	assistant := func(block map[string]any) {
		c.emit(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{block}}})
	}
	result := func(content any, failed bool) {
		c.emit(map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": content, "is_error": failed}}}})
	}
	if typ == "agentMessage" && completed {
		if t, ok := item["text"].(string); ok && t != "" {
			c.mu.Lock()
			c.lastText = t
			c.mu.Unlock()
			assistant(map[string]any{"type": "text", "text": t})
		}
		return
	}
	name := ""
	input := map[string]any{}
	switch typ {
	case "commandExecution":
		name = "Bash"
		input["command"] = item["command"]
	case "fileChange":
		name = "Edit"
		input["changes"] = item["changes"]
	case "mcpToolCall":
		name = "mcp__" + fmt.Sprint(item["server"]) + "__" + fmt.Sprint(item["tool"])
		if args, ok := item["arguments"].(map[string]any); ok {
			input = args
		} else {
			input["arguments"] = item["arguments"]
		}
	case "webSearch":
		name = "WebSearch"
		input["query"] = item["query"]
	default:
		return
	}
	if !completed {
		assistant(map[string]any{"type": "tool_use", "id": id, "name": name, "input": input})
		return
	}
	var content any
	switch typ {
	case "commandExecution":
		content = item["aggregatedOutput"]
	case "fileChange":
		content = item["changes"]
	case "mcpToolCall":
		content = item["result"]
		if item["error"] != nil {
			content = item["error"]
		}
	default:
		content = item
	}
	failed := item["status"] == "failed" || item["error"] != nil
	result(codexToolText(content), failed)
}

func (r *Runner) startCodex(a Agent, mcpConfig, logPath, prompt, systemPrompt, resume, model string) (int, <-chan exit, error) {
	r.mu.Lock()
	refused := r.refusing(a.SessionID)
	r.mu.Unlock()
	if refused {
		return 0, nil, errors.New("runner is stopping")
	}
	// Keep the per-agent config marker private, as for Claude. The HTTP URL is
	// never appears on the child command line: sandboxed ps must not reveal
	// another agent's token. Only the config path is an orphan-recovery marker.
	url := fmt.Sprintf("http://%s/mcp/%s", r.cfg.Addr, a.Token)
	b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fragile": map[string]string{"type": "http", "url": url}}})
	if e := os.WriteFile(mcpConfig, b, 0600); e != nil {
		return 0, nil, e
	}
	out, e := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return 0, nil, e
	}
	dir := r.workDir(a.SessionID)
	cmd := exec.Command(r.CodexCommand, codexArgs(r.cfg, dir, mcpConfig)...)
	cmd.Dir = dir
	cmd.Env = codexEnv(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, e := cmd.StdinPipe()
	if e != nil {
		out.Close()
		return 0, nil, e
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		in.Close()
		out.Close()
		return 0, nil, e
	}
	var outputMu sync.Mutex
	emit := func(v map[string]any) {
		b, _ := json.Marshal(v)
		outputMu.Lock()
		defer outputMu.Unlock()
		out.Write(append(b, '\n'))
		if r.OnLine != nil {
			r.OnLine(a, b)
		}
	}
	// Keep diagnostic stderr, without passing arbitrary diagnostics to the parser.
	cmd.Stderr = &lockedWriter{mu: &outputMu, w: out}
	cmd.WaitDelay = time.Second
	if r.normalOrchestrator(a) {
		url = ""
	}
	c := &codexClient{mcpURL: url, in: in, emit: emit, pending: map[int]chan rpcReply{}, done: make(chan struct{}), inputs: make(chan []map[string]any, 32)}
	r.mu.Lock()
	sr := r.session(a.SessionID)
	if r.refusing(a.SessionID) {
		r.mu.Unlock()
		in.Close()
		out.Close()
		return 0, nil, errors.New("runner is stopping")
	}
	if e = cmd.Start(); e != nil {
		r.mu.Unlock()
		in.Close()
		out.Close()
		return 0, nil, e
	}
	pid := cmd.Process.Pid
	exited := make(chan struct{})
	code := make(chan exit, 1)
	p := &proc{sessionID: a.SessionID, agentID: a.ID, exited: exited, codex: c}
	if r.stdinAgent(a) {
		p.stdin = &stdinPipe{w: c}
	}
	r.running[pid] = p
	sr.wg.Add(1)
	r.mu.Unlock()
	c.stop = func() { r.signal(pid, syscall.SIGTERM) }
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
					c.line(line)
				}
				line = nil
				skipping = false
			}
			if err == bufio.ErrBufferFull {
				continue
			}
			if err != nil {
				if err != io.EOF {
					c.result(fmt.Errorf("Codex output: %w", err))
					c.stop()
				}
				break
			}
		}
		c.close()
	}()
	runDone := make(chan struct{})
	go func() { defer close(runDone); c.run(a, dir, prompt, systemPrompt, resume, model, r.stdinAgent(a)) }()
	go func() {
		<-readDone
		cmd.Wait()
		c.close()
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
		c.mu.Lock()
		if c.failed && !killed {
			status = 1
		}
		if !r.stdinAgent(a) && c.success && !killed {
			status = 0
		}
		c.mu.Unlock()
		close(exited)
		code <- exit{status, killed}
	}()
	return pid, code, nil
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

// Codex command line must never receive Claude model aliases.
func validCodexModel(model string) error {
	if model == "sonnet" || model == "opus" || model == "haiku" || strings.HasPrefix(model, "claude-") {
		return fmt.Errorf("%s is a Claude model; omit model for the Codex default or pass a Codex model id", strconv.Quote(model))
	}
	return nil
}

func (c *codexClient) interrupt(thread, turn string) {
	if _, err := c.call("turn/interrupt", map[string]any{"threadId": thread, "turnId": turn}); err != nil && !errors.Is(err, ErrNotRunning) {
		c.result(err)
	}
}

func codexToolText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	if result, ok := content.(map[string]any); ok {
		if blocks, ok := result["content"].([]any); ok {
			var text []string
			for _, block := range blocks {
				if b, ok := block.(map[string]any); ok {
					if t, ok := b["text"].(string); ok {
						text = append(text, t)
					}
				}
			}
			if len(text) > 0 {
				return strings.Join(text, "\n")
			}
		}
	}
	b, _ := json.MarshalIndent(content, "", "  ")
	return string(b)
}
