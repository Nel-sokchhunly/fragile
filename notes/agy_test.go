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
	"strings"
	"testing"
	"time"
)

// TestAGYHelper is a fake agy speaking the real stream-json protocol: it
// answers each {"event":"user"} stdin line with one turn. "tools" replays the
// recorded testdata/agy-tools.jsonl; "hold" never answers.
func TestAGYHelper(t *testing.T) {
	if os.Getenv("FRAGILE_AGY_HELPER") == "" {
		return
	}
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 4096), maxLine)
	send := func(v any) { b, _ := json.Marshal(v); fmt.Println(string(b)) }
	step := func(v map[string]any) {
		v["conversation_id"] = "conv-1"
		send(map[string]any{"event": "step_update", "step_update": v})
	}
	started := false
	for s.Scan() {
		var v struct {
			Event   string `json:"event"`
			Message struct {
				Content []map[string]any `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(s.Bytes(), &v) != nil || v.Event != "user" {
			fmt.Println("error: unexpected input " + s.Text())
			os.Exit(2)
		}
		text := ""
		for _, b := range v.Message.Content {
			text += fmt.Sprint(b["text"])
		}
		if !started {
			started = true
			send(map[string]any{"event": "init", "conversation_id": "conv-1", "init": map[string]any{"cwd": ".", "tools": []string{"run_command"}}})
		}
		switch text {
		case "hold":
			continue
		case "tools":
			b, _ := os.ReadFile(os.Getenv("FRAGILE_AGY_TESTDATA"))
			os.Stdout.Write(b)
			continue
		}
		step(map[string]any{"step_index": 0, "state": "DONE", "step_type": "user_input"})
		step(map[string]any{"step_index": 1, "state": "ACTIVE", "step_type": "agent_response", "text_delta": "hello from agy: "})
		step(map[string]any{"step_index": 1, "state": "DONE", "step_type": "agent_response", "text_delta": text, "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}})
		send(map[string]any{"event": "result", "result": map[string]any{"conversation_id": "conv-1", "status": "SUCCESS", "response": "hello from agy: " + text, "num_turns": 1}})
	}
	os.Exit(0)
}

// fakeAGY returns a script running TestAGYHelper; it records its arguments
// and HOME in the returned file.
func fakeAGY(t *testing.T) (bin, argsFile string) {
	t.Helper()
	t.Setenv("FRAGILE_AGY_HELPER", "1")
	data, _ := filepath.Abs(filepath.Join("testdata", "agy-tools.jsonl"))
	t.Setenv("FRAGILE_AGY_TESTDATA", data)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	return writeFake(t, dir, fmt.Sprintf("{ printf '%%s\\n' \"$@\"; echo \"HOME=$HOME\"; } > %q\nexec %q -test.run='^TestAGYHelper$'", argsFile, exe)), argsFile
}

func agyRunner(t *testing.T) (*Runner, *Store, Session, <-chan string, string) {
	t.Setenv("HOME", t.TempDir()) // agyHome must not touch the real ~/.gemini
	r, s, _, _ := newTestRunner(t, "")
	var argsFile string
	r.AGYCommand, argsFile = fakeAGY(t)
	r.Interactive = true
	se, err := s.CreateSessionWithProvider("agy", t.TempDir(), ProviderAGY)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 100)
	r.OnLine = func(_ Agent, b []byte) { events <- string(b) }
	t.Cleanup(r.StopAll)
	return r, s, se, events, argsFile
}

func nextAGY(t *testing.T, ch <-chan string, typ string) string {
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
			t.Fatalf("timed out waiting for event type %q", typ)
			return ""
		}
	}
}

func TestAGYArgs(t *testing.T) {
	base := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--sandbox"}
	if got := agyArgs("", ""); !slices.Equal(got, base) {
		t.Fatalf("args = %q", got)
	}
	got := agyArgs("conv-9", "gemini-3.1-pro-high")
	want := append(slices.Clone(base), "--model", "gemini-3.1-pro-high", "--conversation", "conv-9")
	if !slices.Equal(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
	for _, a := range got {
		if a == "run" || a == "-p" || a == "--dangerously-skip-permissions" {
			t.Fatalf("unexpected arg %q in %q", a, got)
		}
	}
}

// replay feeds a recorded agy output file through the adapter.
func replay(t *testing.T, file string) []map[string]any {
	t.Helper()
	var out []map[string]any
	turn := make(chan struct{})
	c := &agyClient{emit: func(v map[string]any) {
		b, _ := json.Marshal(v)
		var m map[string]any
		json.Unmarshal(b, &m)
		out = append(out, m)
	}, model: "gemini-3.8-flash-medium", text: map[int]string{}, tools: map[int]bool{}, turnDone: turn}
	b, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		c.line([]byte(l))
	}
	select {
	case <-turn:
	default:
		t.Fatal("result did not end the turn")
	}
	return out
}

func TestAGYParseTools(t *testing.T) {
	out := replay(t, "agy-tools.jsonl")
	b, _ := json.Marshal(out)
	got := string(b)
	for _, want := range []string{
		`"model":"gemini-3.8-flash-medium","provider":"agy","session_id":"4d1507c2-c6b2-4696-8daf-2052e8b6d35b","subtype":"init"`,
		`"id":"agy-4d1507c2-c6b2-4696-8daf-2052e8b6d35b-2","input":{"CommandLine":"cat a.txt","command":"cat a.txt"},"name":"run_command","type":"tool_use"`,
		`"tool_use_id":"agy-4d1507c2-c6b2-4696-8daf-2052e8b6d35b-2","type":"tool_result"`,
		`"subtype":"stderr","text":"jetski: no output produced`,
		`"text":"[Antigravity auto-denied: RunCommand (command)]"`,
		`"is_error":false`,
		`"session_id":"4d1507c2-c6b2-4696-8daf-2052e8b6d35b","subtype":"success","type":"result"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if n := strings.Count(got, `"type":"tool_use"`); n != 1 {
		t.Errorf("tool_use emitted %d times", n)
	}
}

func TestAGYParseText(t *testing.T) {
	out := replay(t, "agy-text.jsonl")
	var texts []string
	used := 0.0
	for _, v := range out {
		if v["type"] == "assistant" {
			blocks := v["message"].(map[string]any)["content"].([]any)
			texts = append(texts, blocks[0].(map[string]any)["text"].(string))
		}
		if v["subtype"] == "context_usage" {
			used = v["context_used"].(float64)
		}
	}
	if !slices.Equal(texts, []string{"Hello world"}) || used != 120 {
		t.Fatalf("texts = %q, context used = %v; events %v", texts, used, out)
	}
	if out[0]["subtype"] != "stderr" || out[len(out)-1]["result"] != "Hello world\n" {
		t.Fatalf("events = %v", out)
	}
}

func TestAGYContextUsage(t *testing.T) {
	var out []map[string]any
	c := &agyClient{
		emit:     func(v map[string]any) { out = append(out, v) },
		text:     map[int]string{},
		tools:    map[int]bool{},
		turnDone: make(chan struct{}),
	}
	c.line([]byte(`{"event":"step_update","step_update":{"conversation_id":"c","step_index":1,"state":"DONE","step_type":"agent_response","text_delta":"done","usage":{"input_tokens":2000,"cache_read_tokens":10000,"output_tokens":300}}}`))
	var used any
	for _, v := range out {
		if v["subtype"] == "context_usage" {
			used = v["context_used"]
		}
	}
	if used != 12300 {
		t.Fatalf("context_used = %v, want 12300; events = %v", used, out)
	}
}

func TestAGYParseError(t *testing.T) {
	var out []map[string]any
	c := &agyClient{emit: func(v map[string]any) { out = append(out, v) }, text: map[int]string{}, tools: map[int]bool{}, turnDone: make(chan struct{})}
	c.line([]byte(`{"event":"result","result":{"conversation_id":"c","status":"ERROR","error":"quota exceeded"}}`))
	if r := out[len(out)-1]; r["is_error"] != true || r["result"] != "quota exceeded" || !c.failed {
		t.Fatalf("result = %v", r)
	}
	c.line([]byte(`{"event":"step_update","step_update":{"conversation_id":"c","step_index":3,"state":"ERROR","step_type":"tool","tool_name":"run_command","error":"TOOL_ERROR connecting to sandbox server"}}`))
	if blocks := out[len(out)-1]["message"].(map[string]any)["content"].([]any); blocks[0].(map[string]any)["is_error"] != true || blocks[0].(map[string]any)["content"] != "TOOL_ERROR connecting to sandbox server" {
		t.Fatalf("tool result = %v", blocks)
	}
	// A turn agy abandons (it exits) ends with an error naming its last notice.
	c.turnDone = make(chan struct{})
	c.line([]byte("error: not logged in"))
	c.eof()
	if r := out[len(out)-1]; r["is_error"] != true || !strings.Contains(fmt.Sprint(r["result"]), "error: not logged in") {
		t.Fatalf("result = %v", r)
	}
}

func TestAGYShutdownEchoIgnored(t *testing.T) {
	var out []map[string]any
	turn := make(chan struct{})
	c := &agyClient{
		emit:     func(v map[string]any) { out = append(out, v) },
		text:     map[int]string{},
		tools:    map[int]bool{},
		turnDone: turn,
	}

	// Normal turn completes successfully.
	c.line([]byte(`{"event":"result","result":{"conversation_id":"c","status":"SUCCESS","response":"done","num_turns":1}}`))
	select {
	case <-turn:
	default:
		t.Fatal("result did not end the turn")
	}
	n := len(out)
	if n == 0 || out[n-1]["subtype"] != "success" || !c.success || c.failed {
		t.Fatalf("after first result: out=%v, success=%v, failed=%v", out, c.success, c.failed)
	}

	// Second result emitted on SIGTERM shutdown echo while turnDone is nil.
	c.line([]byte(`{"event":"result","result":{"conversation_id":"c","status":"CANCELLED","error":"stream input cancelled: context canceled"}}`))
	if len(out) != n {
		t.Fatalf("echo result was emitted: %v", out[n:])
	}
	if !c.success || c.failed {
		t.Fatalf("shutdown echo corrupted status: success=%v, failed=%v", c.success, c.failed)
	}
}

func TestAGYInputDropsAttachments(t *testing.T) {
	got := agyInput([]map[string]any{{"type": "text", "text": "hi"}, {"type": "image", "source": map[string]any{}}})
	if len(got) != 2 || got[0]["text"] != "hi" || !strings.Contains(fmt.Sprint(got[1]["text"]), "1 attachment") {
		t.Fatalf("input = %v", got)
	}
}

func TestAGYHome(t *testing.T) {
	user := t.TempDir()
	t.Setenv("HOME", user)
	g := filepath.Join(user, ".gemini")
	cli := filepath.Join(g, "antigravity-cli")
	for _, d := range []string{filepath.Join(g, "config"), filepath.Join(g, "antigravity"), filepath.Join(cli, "conversations")} {
		os.MkdirAll(d, 0o700)
	}
	settings := `{"colorScheme":"x","permissions":{"allow":["command(ls)"]}}`
	for name, data := range map[string]string{"GEMINI.md": "USER RULES", "config/config.json": "{}", "antigravity-cli/settings.json": settings, "antigravity-cli/history.jsonl": ""} {
		os.WriteFile(filepath.Join(g, name), []byte(data), 0o600)
	}
	r := NewRunner(Config{Addr: "127.0.0.1:9", AgentDir: t.TempDir()}, nil, nil)
	a := Agent{ID: 7, Token: "tok", Role: "subagent"}
	home, err := r.agyHome(a, "SYSTEM PROMPT", "/work")
	if err != nil {
		t.Fatal(err)
	}
	if home != filepath.Join(r.cfg.AgentDir, "agent-7.home") {
		t.Fatalf("home = %s", home)
	}
	if _, err := r.agyHome(a, "SYSTEM PROMPT", "/work"); err != nil { // rebuilt on relaunch
		t.Fatal(err)
	}
	h := filepath.Join(home, ".gemini")
	read := func(name string) string { b, _ := os.ReadFile(filepath.Join(h, name)); return string(b) }
	if p := read("GEMINI.md"); !strings.HasPrefix(p, "SYSTEM PROMPT") || !strings.HasSuffix(p, "\n\nUSER RULES") {
		t.Fatalf("GEMINI.md = %q", p)
	}
	if m := read("config/mcp_config.json"); m != `{"mcpServers":{"fragile":{"url":"http://127.0.0.1:9/mcp/tok"}}}` {
		t.Fatalf("mcp_config.json = %s", m)
	}
	if hk := read("config/hooks.json"); !strings.Contains(hk, `"PreToolUse"`) || !strings.Contains(hk, "invoke") || !strings.Contains(hk, `\"decision\":\"deny\"`) {
		t.Fatalf("hooks.json = %s", hk)
	}
	for name, target := range map[string]string{
		"config/config.json":            filepath.Join(g, "config", "config.json"),
		"antigravity":                   filepath.Join(g, "antigravity"),
		"antigravity-cli/conversations": filepath.Join(cli, "conversations"),
		"antigravity-cli/brain":         filepath.Join(cli, "brain"),
		"antigravity-cli/history.jsonl": filepath.Join(cli, "history.jsonl"),
	} {
		if got, err := os.Readlink(filepath.Join(h, name)); err != nil || got != target {
			t.Errorf("%s -> %q (%v), want %s", name, got, err, target)
		}
	}
	if fi, err := os.Lstat(filepath.Join(h, "antigravity-cli", "settings.json")); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("settings.json must be a copy: %v %v", fi, err)
	}
	var s struct {
		ColorScheme string
		Permissions struct{ Allow []string }
		Trusted     []string `json:"trustedWorkspaces"`
	}
	if err := json.Unmarshal([]byte(read("antigravity-cli/settings.json")), &s); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"command(ls)", "command(*)", "write_file(/work)", "mcp(fragile/*)"} {
		if !slices.Contains(s.Permissions.Allow, rule) {
			t.Errorf("allow %q missing: %v", rule, s.Permissions.Allow)
		}
	}
	if s.ColorScheme != "x" || !slices.Equal(s.Trusted, []string{"/work"}) {
		t.Fatalf("settings = %+v", s)
	}
	if b, _ := os.ReadFile(filepath.Join(cli, "settings.json")); string(b) != settings {
		t.Fatalf("the user's settings changed: %s", b)
	}
	if _, err := os.Stat(filepath.Join(cli, "conversations")); err != nil {
		t.Fatal("rebuilding removed the user's conversations")
	}
}

func TestAGYProviderPersistence(t *testing.T) {
	_, s, legacy, _ := newTestRunner(t, "")
	if legacy.Provider != ProviderClaude {
		t.Fatalf("unexpected default provider: %+v", legacy)
	}
	se, err := s.CreateSessionWithProvider("agy-test", t.TempDir(), ProviderAGY)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(se.ID)
	if err != nil || got.Provider != ProviderAGY {
		t.Fatalf("got %+v, err: %v", got, err)
	}
	if _, err = s.CreateSessionWithProvider("invalid", "", "unknown-provider"); err == nil {
		t.Fatal("invalid provider should be rejected")
	}
}

func TestAGYModelResolution(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"flash", "gemini-3.8-flash-medium", false},
		{"pro", "gemini-3.1-pro-high", false},
		{"flash_lite", "gemini-3.8-flash-low", false},
		{"gemini-3.7-flash-high", "gemini-3.7-flash-high", false},
		{"claude-sonnet-4-6", "claude-sonnet-4-6", false},
		{"sonnet", "", true},
		{"opus", "", true},
		{"haiku", "", true},
		{"invalid with spaces", "", true},
	}
	for _, tc := range cases {
		got, err := resolveAGYModel(tc.input)
		if tc.wantErr != (err != nil) || got != tc.want {
			t.Errorf("resolveAGYModel(%q) = %q, %v", tc.input, got, err)
		}
	}
}

func TestAGYEnv(t *testing.T) {
	env := []string{
		"PATH=/bin", "HOME=/real", "GOPATH=/gp",
		"GEMINI_API_KEY=secret123", "GOOGLE_API_KEY=secret456", "ANTIGRAVITY_API_KEY=secret789",
		"AGY_API_KEY=secretabc", "VERTEX_API_KEY=secretdef", "ANTHROPIC_API_KEY=secretclaude",
	}
	cleaned := agyEnv(env, "/agent/home")
	for _, kv := range cleaned {
		if strings.Contains(kv, "secret") {
			t.Errorf("agyEnv leaked %q", kv)
		}
	}
	homes := slices.DeleteFunc(slices.Clone(cleaned), func(kv string) bool { return !strings.HasPrefix(kv, "HOME=") })
	if !slices.Equal(homes, []string{"HOME=/agent/home"}) || !slices.Contains(cleaned, "GOPATH=/gp") || !slices.Contains(cleaned, "PATH=/bin") {
		t.Fatalf("env = %q", cleaned)
	}
}

func TestAGYPromptFormatting(t *testing.T) {
	a := Agent{Role: roleOrchestrator}
	base := "# You are the orchestrator\nClaude Code task. The orchestrator is not sandboxed.\n4. **Spawn**\n5. **Wait loop.**\n"
	prompt := agyPrompt(a, base)
	for _, bad := range []string{"Claude Code", "not sandboxed"} {
		if strings.Contains(prompt, bad) {
			t.Errorf("prompt still contains %q: %s", bad, prompt)
		}
	}
	if !strings.Contains(prompt, "Antigravity") || !strings.Contains(prompt, "Fragile MCP server") {
		t.Errorf("prompt = %s", prompt)
	}
}

func TestAGYRunnerTurns(t *testing.T) {
	r, _, se, ch, argsFile := agyRunner(t)
	a, err := r.StartOrchestrator(se.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.SendUser(a.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	if line := nextAGY(t, ch, "system"); !strings.Contains(line, `"session_id":"conv-1"`) {
		t.Fatalf("init = %s", line)
	}
	if line := nextAGY(t, ch, "assistant"); !strings.Contains(line, "hello from agy: hello") {
		t.Fatalf("assistant = %s", line)
	}
	nextAGY(t, ch, "result")

	b, _ := os.ReadFile(argsFile)
	args := strings.Split(strings.TrimSpace(string(b)), "\n")
	want := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--sandbox", "HOME=" + filepath.Join(r.cfg.AgentDir, fmt.Sprintf("agent-%d.home", a.ID))}
	if !slices.Equal(args, want) {
		t.Fatalf("agy args = %q, want %q", args, want)
	}

	if err = r.SendUser(a.ID, "tools"); err != nil {
		t.Fatal(err)
	}
	if line := nextAGY(t, ch, "assistant"); !strings.Contains(line, `"name":"run_command"`) {
		t.Fatalf("tool_use = %s", line)
	}
	nextAGY(t, ch, "result")

	if err = r.SendUser(a.ID, "hold"); err != nil {
		t.Fatal(err)
	}
	if err = r.Interrupt(a.ID); err == nil || errors.Is(err, ErrNotRunning) || !strings.Contains(err.Error(), "Antigravity") {
		t.Fatalf("interrupt = %v", err)
	}
}

func TestAGYWorkerOneTurn(t *testing.T) {
	r, s, se, ch, _ := agyRunner(t)
	orch, _ := s.CreateAgent(se.ID, "orchestrator", 0, 0)
	w, err := r.SpawnSubagent(se.ID, orch.ID, "", "do it", "flash", "")
	if err != nil {
		t.Fatal(err)
	}
	if line := nextAGY(t, ch, "assistant"); !strings.Contains(line, "hello from agy: do it") {
		t.Fatalf("assistant = %s", line)
	}
	r.Wait(se.ID)
	got, _ := s.GetAgent(se.ID, w.ID)
	if got.Status != "exited" || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("worker after its turn: %+v", got)
	}
}

// Optional real-CLI smoke: one agy process with a Fragile-built HOME runs two
// stream-json turns, the second a shell command that the settings must allow
// (headless agy auto-denies anything else). Uses your Antigravity login.
// FRAGILE_AGY_SMOKE=1 go test ./notes -run TestAGYSandboxSmoke -v
func TestAGYSandboxSmoke(t *testing.T) {
	if os.Getenv("FRAGILE_AGY_SMOKE") != "1" {
		t.Skip("set FRAGILE_AGY_SMOKE=1 to run the installed agy CLI")
	}
	agyPath, err := exec.LookPath("agy")
	if err != nil {
		t.Skip("Antigravity CLI (agy) not installed")
	}
	work := t.TempDir()
	r := NewRunner(Config{Addr: "127.0.0.1:1", AgentDir: t.TempDir()}, nil, nil)
	home, err := r.agyHome(Agent{ID: 1, Token: "smoke", Role: "subagent"}, "You are a terse test agent.", work)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(agyPath, agyArgs("", "")...)
	cmd.Dir = work
	cmd.Env = agyEnv(append(os.Environ(), "GEMINI_API_KEY=must-not-leak"), home)
	in, _ := cmd.StdinPipe()
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	lines := make(chan string, 1000)
	go func() {
		s := bufio.NewScanner(pr)
		s.Buffer(make([]byte, 4096), maxLine)
		for s.Scan() {
			t.Log(s.Text())
			lines <- s.Text()
		}
		close(lines)
	}()
	turn := func(text string) (result struct {
		Status        string
		Response      string
		DeniedActions []any `json:"denied_actions"`
	}) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"event": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": text}}}})
		in.Write(append(b, '\n'))
		timeout := time.After(3 * time.Minute)
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatal("agy exited")
				}
				var v struct {
					Event  string
					Result json.RawMessage
				}
				if json.Unmarshal([]byte(l), &v) == nil && v.Event == "result" {
					json.Unmarshal(v.Result, &result)
					return result
				}
			case <-timeout:
				t.Fatal("no result")
			}
		}
	}
	if res := turn("Reply exactly AGY_SMOKE_OK; use no tools."); res.Status != "SUCCESS" || !strings.Contains(res.Response, "AGY_SMOKE_OK") {
		t.Fatalf("text turn = %+v", res)
	}
	res := turn("Run exactly this shell command and nothing else: echo agy_cmd_ok > smoke.txt")
	if len(res.DeniedActions) > 0 {
		t.Fatalf("command denied; the settings.json allow rules do not work: %+v", res)
	}
	if b, err := os.ReadFile(filepath.Join(work, "smoke.txt")); err != nil || strings.TrimSpace(string(b)) != "agy_cmd_ok" {
		t.Fatalf("smoke.txt = %q, %v (result %+v)", b, err, res)
	}
}
