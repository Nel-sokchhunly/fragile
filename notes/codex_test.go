package notes

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCodexHelper(t *testing.T) {
	mode := os.Getenv("FRAGILE_CODEX_HELPER")
	if mode == "" {
		return
	}
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 4096), maxLine)
	send := func(v any) { b, _ := json.Marshal(v); fmt.Println(string(b)) }
	notify := func(method string, p any) { send(map[string]any{"method": method, "params": p}) }
	for s.Scan() {
		var v struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		json.Unmarshal(s.Bytes(), &v)
		if f := os.Getenv("FRAGILE_CODEX_TRACE"); f != "" {
			out, _ := os.OpenFile(f, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			out.Write(append(s.Bytes(), '\n'))
			out.Close()
		}
		var result any = map[string]any{}
		switch v.Method {
		case "initialized":
			continue
		case "initialize":
			result = map[string]string{"userAgent": "fragile-test"}
		case "account/read":
			if mode == "auth-fail" {
				result = map[string]any{"account": nil}
			} else {
				result = map[string]any{"account": map[string]string{"type": "chatgpt"}}
			}
		case "model/list":
			result = map[string]any{"data": []any{map[string]any{"id": "gpt-test", "model": "gpt-test", "isDefault": true}}}
		case "config/read":
			result = map[string]any{"config": map[string]any{"mcp_servers": map[string]any{"personal_server": map[string]any{"command": "/bin/false"}}}}
		case "thread/start", "thread/resume":
			result = map[string]any{"thread": map[string]string{"id": "test-thread"}, "model": "gpt-test"}
		case "turn/start":
			result = map[string]any{"turn": map[string]string{"id": "test-turn"}}
			send(map[string]any{"id": v.ID, "result": result})
			notify("turn/started", map[string]any{"turn": map[string]string{"id": "test-turn"}})
			b, _ := json.Marshal(v.Params["input"])
			if strings.Contains(string(b), "hold") {
				continue
			}
			notify("item/completed", map[string]any{"item": map[string]string{"id": "message", "type": "agentMessage", "text": "hello from codex"}})
			notify("turn/completed", map[string]any{"turn": map[string]string{"id": "test-turn", "status": "completed"}})
			continue
		case "turn/interrupt":
			send(map[string]any{"id": v.ID, "result": result})
			notify("turn/completed", map[string]any{"turn": map[string]string{"id": "test-turn", "status": "interrupted"}})
			continue
		case "thread/compact/start":
			send(map[string]any{"id": v.ID, "result": result})
			notify("thread/compacted", map[string]string{"threadId": "test-thread"})
			continue
		}
		send(map[string]any{"id": v.ID, "result": result})
	}
	os.Exit(0)
}

func fakeCodex(t *testing.T, mode string) string {
	t.Helper()
	t.Setenv("FRAGILE_CODEX_HELPER", mode)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	return writeFake(t, t.TempDir(), "exec "+strconv.Quote(exe)+" -test.run='^TestCodexHelper$'")
}
func codexRunner(t *testing.T, mode string) (*Runner, *Store, Session, <-chan string) {
	r, s, _, _ := newTestRunner(t, "")
	r.CodexCommand = fakeCodex(t, mode)
	r.Interactive = true
	se, e := s.CreateSessionWithProvider("codex", t.TempDir(), ProviderCodex)
	if e != nil {
		t.Fatal(e)
	}
	events := make(chan string, 100)
	r.OnLine = func(_ Agent, b []byte) { events <- string(b) }
	t.Cleanup(r.StopAll)
	return r, s, se, events
}
func nextCodex(t *testing.T, ch <-chan string, typ string) string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line := <-ch:
			var v struct {
				Type string `json:"type"`
			}
			json.Unmarshal([]byte(line), &v)
			if v.Type == typ {
				return line
			}
		case <-deadline:
			t.Fatalf("no %s event", typ)
			return ""
		}
	}
}
func TestCodexRunnerTeamTurnsInterruptAndResume(t *testing.T) {
	r, s, se, ch := codexRunner(t, "normal")
	trace := filepath.Join(t.TempDir(), "rpc.jsonl")
	t.Setenv("FRAGILE_CODEX_TRACE", trace)
	a, e := r.StartOrchestrator(se.ID, "")
	if e != nil {
		t.Fatal(e)
	}
	nextCodex(t, ch, "system")
	for _, text := range []string{"first", "second"} {
		if e = r.SendUser(a.ID, text); e != nil {
			t.Fatal(e)
		}
		if line := nextCodex(t, ch, "assistant"); !strings.Contains(line, "hello from codex") {
			t.Fatal(line)
		}
		nextCodex(t, ch, "result")
	}
	if e = r.SendUser(a.ID, "hold"); e != nil {
		t.Fatal(e)
	}
	// turn/started is internal: wait until the driver's active turn id is set.
	var c *codexClient
	r.mu.Lock()
	for _, p := range r.running {
		if p.agentID == a.ID {
			c = p.codex
		}
	}
	r.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		turn := c.turn
		c.mu.Unlock()
		if turn != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("turn never started")
		}
		time.Sleep(time.Millisecond)
	}
	if e = r.Interrupt(a.ID); e != nil {
		t.Fatal(e)
	}
	if line := nextCodex(t, ch, "result"); !strings.Contains(line, `"is_error":true`) {
		t.Fatal(line)
	}
	if !r.Running(a.ID) {
		t.Fatal("interrupt killed the conversation")
	}
	if e = r.SendUser(a.ID, "/compact"); e != nil {
		t.Fatal(e)
	}
	nextCodex(t, ch, "system")
	nextCodex(t, ch, "result")
	sub, e := r.SpawnSubagent(se.ID, a.ID, "worker", "task", "gpt-test")
	if e != nil {
		t.Fatal(e)
	}
	nextCodex(t, ch, "system")
	nextCodex(t, ch, "assistant")
	nextCodex(t, ch, "result")
	deadline = time.Now().Add(5 * time.Second)
	for {
		got, _ := s.GetAgent(se.ID, sub.ID)
		if got.Status != "running" {
			if got.Status != "exited" {
				t.Fatalf("worker status %+v", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker stayed alive")
		}
		time.Sleep(time.Millisecond)
	}
	r.StopSession(se.ID)
	a, e = r.ResumeOrchestrator(se.ID, "test-thread")
	if e != nil {
		t.Fatal(e)
	}
	nextCodex(t, ch, "system")
	if e = r.SendUser(a.ID, "resumed"); e != nil {
		t.Fatal(e)
	}
	nextCodex(t, ch, "assistant")
	nextCodex(t, ch, "result")
	data, _ := os.ReadFile(trace)
	if !strings.Contains(string(data), `"mcp_servers.personal_server.enabled":false`) {
		t.Fatal("personal MCP was not disabled")
	}
	if !strings.Contains(string(data), `"method":"thread/resume"`) || !strings.Contains(string(data), `"threadId":"test-thread"`) {
		t.Fatal("resume did not use the saved thread")
	}
	if _, e = r.SpawnSubagent(se.ID, a.ID, "bad model", "task", "sonnet"); e == nil {
		t.Fatal("Claude model accepted for Codex")
	}
}
func TestCodexRunnerAuthenticationFailure(t *testing.T) {
	r, s, se, ch := codexRunner(t, "auth-fail")
	a, e := r.StartOrchestrator(se.ID, "")
	if e != nil {
		t.Fatal(e)
	}
	if line := nextCodex(t, ch, "result"); !strings.Contains(line, "codex login") {
		t.Fatal(line)
	}
	r.Wait(se.ID)
	got, _ := s.GetAgent(se.ID, a.ID)
	if got.Status != "crashed" {
		t.Fatalf("auth failure status: %+v", got)
	}
}
func TestCodexProviderPersistenceAndInput(t *testing.T) {
	_, s, legacy, _ := newTestRunner(t, "")
	if legacy.Provider != ProviderClaude {
		t.Fatal(legacy)
	}
	se, e := s.CreateSessionWithProvider("codex", t.TempDir(), ProviderCodex)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.GetSession(se.ID)
	if e != nil || got.Provider != ProviderCodex {
		t.Fatal(got, e)
	}
	if _, e = s.CreateSessionWithProvider("bad", "", "nope"); e == nil {
		t.Fatal("invalid provider accepted")
	}
	input, e := codexInput([]map[string]any{{"type": "text", "text": "a"}, {"type": "image", "source": map[string]any{"media_type": "image/png", "data": "YWJj"}}})
	if e != nil || input[1]["url"] != "data:image/png;base64,YWJj" {
		t.Fatal(input, e)
	}
	if _, e = codexInput([]map[string]any{{"type": "document"}}); e == nil {
		t.Fatal("PDF silently accepted")
	}
}
func TestCodexArgumentsProtectSecretsAndSubscription(t *testing.T) {
	dir := t.TempDir()
	args := strings.Join(codexArgs(Config{AgentDir: filepath.Join(dir, "agents"), DBPath: filepath.Join(dir, "f.db"), LogPath: filepath.Join(dir, "events")}, dir, filepath.Join(dir, "agent-1.mcp.json")), " ")
	if strings.Contains(args, "/mcp/") {
		t.Fatal("secret MCP URL leaked into process arguments")
	}
	for _, want := range []string{`--strict-config`, `default_permissions="fragile"`, `features.multi_agent=false`, `forced_login_method="chatgpt"`, `hooks.SessionStart=[]`, `permissions.fragile.network.domains=`, `f.db-wal"="deny"`, `agents"="deny"`} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %s", want)
		}
	}
	env := strings.Join(codexEnv([]string{"PATH=/bin", "CODEX_API_KEY=secret", "OPENAI_API_KEY=secret", "CODEX_HOME=/home/test", "OPENAI_BASE_URL=elsewhere"}), " ")
	if strings.Contains(env, "secret") || strings.Contains(env, "elsewhere") || !strings.Contains(env, "CODEX_HOME") {
		t.Fatal("wrong billing environment")
	}
}

// Optional real-CLI smoke: verifies protocol and actual OS deny-read enforcement
// without inference, credentials, or network requests. Run with
// FRAGILE_CODEX_SMOKE=1 go test ./notes -run TestCodexSandboxSmoke -v.
func TestCodexSandboxSmoke(t *testing.T) {
	if os.Getenv("FRAGILE_CODEX_SMOKE") != "1" {
		t.Skip("set FRAGILE_CODEX_SMOKE=1 for installed-CLI sandbox proof")
	}
	if _, e := exec.LookPath("codex"); e != nil {
		t.Skip("Codex CLI not installed")
	}
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.db")
	os.WriteFile(secret, []byte("FRAGILE_PRIVATE_TEST"), 0600)
	cfg := Config{DBPath: secret, AgentDir: filepath.Join(dir, "agents"), LogPath: filepath.Join(dir, "events")}
	runner, store, session, _ := newTestRunner(t, "")
	agent, err := store.CreateAgent(session.ID, roleOrchestrator, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer((&Server{Store: store, Log: runner.log, Runner: runner}).Handler())
	defer server.Close()
	cmd := exec.Command("codex", codexArgs(cfg, dir, filepath.Join(dir, "agent.mcp.json"))...)
	cmd.Dir = dir
	cmd.Env = codexEnv(os.Environ())
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	var diagnostics strings.Builder
	cmd.Stderr = &diagnostics
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	c := &codexClient{mcpURL: server.URL + "/mcp/" + agent.Token, in: in, emit: func(map[string]any) {}, pending: map[int]chan rpcReply{}, done: make(chan struct{}), inputs: make(chan []map[string]any, 1)}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s := bufio.NewScanner(out)
		for s.Scan() {
			c.line(s.Bytes())
		}
		c.close()
	}()
	defer func() { cmd.Process.Kill(); cmd.Wait(); wg.Wait() }()
	if _, e := c.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "fragile-test", "version": "1"}}); e != nil {
		t.Fatal(e, diagnostics.String())
	}
	c.write(map[string]string{"method": "initialized"})
	isolated, err := c.isolatedConfig()
	if err != nil {
		t.Fatal(err)
	}
	thread, err := c.call("thread/start", map[string]any{"cwd": dir, "approvalPolicy": "never", "developerInstructions": "Use Fragile board tools only.", "config": isolated})
	if err != nil {
		t.Fatal("real MCP/thread startup:", err)
	}
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(thread, &started) != nil || started.Thread.ID == "" {
		t.Fatal("real thread missing id")
	}
	raw, e := c.call("command/exec", map[string]any{"command": []string{"/bin/cat", secret}, "cwd": dir})
	if e != nil {
		t.Fatal(e, diagnostics.String())
	}
	var result struct {
		ExitCode int    `json:"exitCode"`
		Stdout   string `json:"stdout"`
	}
	if json.Unmarshal(raw, &result) != nil || result.ExitCode == 0 || strings.Contains(result.Stdout, "FRAGILE_PRIVATE_TEST") {
		t.Fatalf("secret read was not denied: %s", raw)
	}
	raw, e = c.call("command/exec", map[string]any{"command": []string{"/bin/sh", "-c", "echo ok > proof.txt"}, "cwd": dir})
	if e != nil {
		t.Fatal(e)
	}
	if json.Unmarshal(raw, &result) != nil || result.ExitCode != 0 {
		t.Fatalf("project write failed: %s", raw)
	}
}

// Optional bounded inference smoke using the operator's existing ChatGPT login.
// No shell/file edits, no workers, one short turn and one board note.
func TestCodexLiveMCP(t *testing.T) {
	if os.Getenv("FRAGILE_CODEX_LIVE") != "1" {
		t.Skip("set FRAGILE_CODEX_LIVE=1 to use an existing ChatGPT login")
	}
	r, store, _, _ := newTestRunner(t, "")
	r.CodexCommand = "codex"
	r.cfg.WorkDir = t.TempDir()
	session, err := store.CreateSessionWithProvider("live Codex smoke", r.cfg.WorkDir, ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer((&Server{Store: store, Log: r.log, Runner: r}).Handler())
	defer server.Close()
	r.cfg.Addr = strings.TrimPrefix(server.URL, "http://")
	t.Cleanup(r.StopAll)
	ag, err := r.StartOrchestrator(session.ID, "This is a bounded integration smoke test, not a development task. Do not launch workers, run commands, or change files. Use the Fragile post_note MCP tool to post a done note whose content is exactly CODEX_LIVE_SMOKE_OK. Then reply CODEX_LIVE_SMOKE_OK and stop.")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { r.Wait(session.ID); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		r.StopSession(session.ID)
		t.Fatal("live Codex smoke exceeded 60 seconds")
	}
	got, _ := store.GetAgent(session.ID, ag.ID)
	if got.Status != "exited" {
		data, _ := os.ReadFile(got.LogPath)
		t.Fatalf("live Codex status %s: %s", got.Status, data)
	}
	board, _ := store.SessionBoard(session.ID)
	notes, err := store.ListNotes(session.ID, board, NoteFilter{Type: "done", AuthorID: ag.ID})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range notes {
		if n.Content == "CODEX_LIVE_SMOKE_OK" {
			found = true
		}
	}
	if !found {
		data, _ := os.ReadFile(got.LogPath)
		t.Fatalf("live CLI did not post the expected authenticated MCP note: %s", data)
	}
}
