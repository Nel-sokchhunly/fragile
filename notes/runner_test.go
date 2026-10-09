package notes

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestRunner returns a Runner whose "claude" is a shell script with the given body.
func newTestRunner(t *testing.T, script string) (*Runner, *Store, Session, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	evPath := filepath.Join(dir, "events.jsonl")
	ev, err := OpenEventLog(evPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ev.Close() })
	r := NewRunner(Config{Addr: "127.0.0.1:1", AgentDir: dir, WorkDir: dir}, store, ev)
	r.Preflight = nil // tests must not depend on the host's sandbox tools
	r.Command = writeFake(t, dir, script)
	sess, err := store.CreateSession("test")
	if err != nil {
		t.Fatal(err)
	}
	return r, store, sess, evPath
}

// writeFake writes a shell script standing in for "claude" and returns its path.
func writeFake(t *testing.T, dir, script string) string {
	t.Helper()
	fake := filepath.Join(dir, "fake-claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return fake
}

func TestRunnerSubagentExitsCleanly(t *testing.T) {
	r, store, sess, evPath := newTestRunner(t, `echo '{"type":"result"}'`)
	orch, _ := store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	a, err := r.SpawnSubagent(sess.ID, orch.ID, "", "first line\nsecond line", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.PID <= 0 || a.PID == os.Getpid() || a.LogPath == "" || a.Role != "subagent" || a.ParentID != orch.ID {
		t.Fatalf("unexpected agent: %+v", a)
	}
	r.Wait(sess.ID)

	got, _ := store.GetAgent(sess.ID, a.ID)
	if got.Status != "exited" || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("agent after exit: %+v", got)
	}
	task, _ := store.GetTask(sess.ID, a.TaskID)
	if task.Title != "first line" || task.Description != "first line\nsecond line" || task.Status != "done" || task.AgentID != a.ID {
		t.Fatalf("task: %+v", task)
	}
	if b, _ := os.ReadFile(a.LogPath); strings.TrimSpace(string(b)) != `{"type":"result"}` {
		t.Fatalf("log file = %q", b)
	}
	tok, _ := store.GetAgent(sess.ID, a.ID)
	cfgPath := strings.TrimSuffix(a.LogPath, ".jsonl") + ".mcp.json"
	if cfg, _ := os.ReadFile(cfgPath); !strings.Contains(string(cfg), "http://127.0.0.1:1/mcp/"+tok.Token) {
		t.Fatalf("mcp config = %q", cfg)
	}
	ev, _ := os.ReadFile(evPath)
	for _, want := range []string{`"event":"agent_spawned"`, `"event":"agent_status_changed"`, `"status":"exited"`, `"missing_done_note":true`} {
		if !strings.Contains(string(ev), want) {
			t.Errorf("event log missing %s:\n%s", want, ev)
		}
	}
}

func TestRunnerDoneNoteSuppressesFlag(t *testing.T) {
	r, store, sess, evPath := newTestRunner(t, `sleep 1`)
	orch, _ := store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	a, err := r.SpawnSubagent(sess.ID, orch.ID, "", "do it", "")
	if err != nil {
		t.Fatal(err)
	}
	board, _ := store.SessionBoard(sess.ID)
	store.PostNote(sess.ID, board, a.ID, "done", "finished")
	r.Wait(sess.ID)
	if ev, _ := os.ReadFile(evPath); !strings.Contains(string(ev), `"status":"exited"`) || strings.Contains(string(ev), "missing_done_note") {
		t.Fatalf("events:\n%s", ev)
	}
}

func TestRunnerSubagentCrashes(t *testing.T) {
	r, store, sess, evPath := newTestRunner(t, `echo boom >&2; exit 1`)
	orch, _ := store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	a, err := r.SpawnSubagent(sess.ID, orch.ID, "", "do it", "")
	if err != nil {
		t.Fatal(err)
	}
	r.Wait(sess.ID)
	got, _ := store.GetAgent(sess.ID, a.ID)
	if got.Status != "crashed" || got.ExitCode == nil || *got.ExitCode != 1 {
		t.Fatalf("agent: %+v", got)
	}
	if task, _ := store.GetTask(sess.ID, a.TaskID); task.Status != "blocked" {
		t.Fatalf("task status = %q", task.Status)
	}
	if b, _ := os.ReadFile(a.LogPath); strings.TrimSpace(string(b)) != "boom" {
		t.Fatalf("stderr not in log: %q", b)
	}
	if ev, _ := os.ReadFile(evPath); !strings.Contains(string(ev), `"status":"crashed"`) || strings.Contains(string(ev), "missing_done_note") {
		t.Fatalf("events:\n%s", ev)
	}
}

func TestRunnerStopAllRecordsEverything(t *testing.T) {
	r, store, sess, evPath := newTestRunner(t, `sleep 300`)
	orch, err := r.StartOrchestrator(sess.ID, "build a thing")
	if err != nil {
		t.Fatal(err)
	}
	if orch.Role != "orchestrator" || orch.PID <= 0 || orch.TaskID != 0 {
		t.Fatalf("agent: %+v", orch)
	}
	sub, err := r.SpawnSubagent(sess.ID, orch.ID, "", "do it", "")
	if err != nil {
		t.Fatal(err)
	}
	finished, err := r.SpawnSubagent(sess.ID, orch.ID, "", "already done", "")
	if err != nil {
		t.Fatal(err)
	}
	store.SetTaskStatus(sess.ID, finished.TaskID, "done")

	r.StopAll()                                                                    // no Wait: StopAll itself must leave state and log final
	if task, _ := store.GetTask(sess.ID, finished.TaskID); task.Status != "done" { // stopping does not undo a done task
		t.Fatalf("done task status after StopAll = %q", task.Status)
	}
	for _, id := range []int64{orch.ID, sub.ID, finished.ID} {
		if got, _ := store.GetAgent(sess.ID, id); got.Status != "stopped" { // killed on purpose, not a crash
			t.Fatalf("agent %d status after StopAll = %q", id, got.Status)
		}
	}
	if task, _ := store.GetTask(sess.ID, sub.TaskID); task.Status != "blocked" { // a stopped sub-agent leaves its task unfinished
		t.Fatalf("task status = %q", task.Status)
	}
	if ev, _ := os.ReadFile(evPath); strings.Count(string(ev), `"event":"agent_status_changed"`) != 3 {
		t.Fatalf("want 3 agent_status_changed events:\n%s", ev)
	}
	if _, err := r.SpawnSubagent(sess.ID, orch.ID, "", "too late", ""); err == nil {
		t.Fatal("SpawnSubagent after StopAll succeeded")
	}
}

// Glob characters in a path must not widen (or break) the Read/Edit permission rules built from it.
func TestSandboxSettingsEscapesPaths(t *testing.T) {
	var settings struct {
		Permissions struct{ Allow, Deny []string }
	}
	cfg := Config{AgentDir: "/d[x]/a*", DBPath: "/d[x]/f?.db"}
	if err := json.Unmarshal([]byte(sandboxSettings(cfg, `/w/[x]/*dir`)), &settings); err != nil {
		t.Fatal(err)
	}
	if want := []string{`Edit(//w/\[x\]/\*dir/**)`}; !slices.Equal(settings.Permissions.Allow, want) {
		t.Errorf("allow rules = %v, want %v", settings.Permissions.Allow, want)
	}
	for _, rule := range []string{`Read(//d\[x\]/a\*)`, `Edit(//d\[x\]/a\*)`, `Read(//d\[x\]/f\?.db)`, `Edit(//d\[x\]/f\?.db-wal)`} {
		if !slices.Contains(settings.Permissions.Deny, rule) {
			t.Errorf("missing deny rule %s: %v", rule, settings.Permissions.Deny)
		}
	}
	if got := globEscape(`a\b{c}`); got != `a\\b{c}` {
		t.Errorf("globEscape = %q", got)
	}
}

func TestRunnerArgs(t *testing.T) {
	r := NewRunner(Config{AgentDir: "/d/agents", DBPath: "/d/f.db", LogPath: "/d/events.jsonl"}, nil, nil)
	args := r.args(Agent{Role: "subagent"}, "/w/dir", "/x/agent-3.mcp.json", "do -it", "SYS", "", "")
	for _, want := range [][]string{
		{"--mcp-config", "/x/agent-3.mcp.json"}, {"--append-system-prompt", "SYS"},
		{"--output-format", "stream-json"}, {"--disallowedTools", "Task,Agent,Workflow"}, {"--", "do -it"},
		{"--setting-sources", "project"},
	} {
		if i := slices.Index(args, want[0]); i < 0 || args[i+1] != want[1] {
			t.Errorf("args missing %v: %v", want, args)
		}
	}
	var settings struct {
		Sandbox struct {
			Enabled, FailIfUnavailable, AllowUnsandboxedCommands bool
			Filesystem                                           struct{ AllowWrite, DenyRead, DenyWrite []string }
			Credentials                                          struct{ Files []struct{ Path, Mode string } }
			Network                                              struct{ AllowedDomains []string }
		}
		Permissions struct{ Allow, Deny []string }
	}
	if i := slices.Index(args, "--settings"); i < 0 {
		t.Fatalf("args missing --settings: %v", args)
	} else if err := json.Unmarshal([]byte(args[i+1]), &settings); err != nil {
		t.Fatalf("--settings is not JSON: %v", err)
	}
	sb := settings.Sandbox
	if !sb.Enabled || !sb.FailIfUnavailable || sb.AllowUnsandboxedCommands || !slices.Contains(sb.Network.AllowedDomains, "github.com") {
		t.Errorf("sandbox settings: %+v", sb)
	}
	if !slices.Contains(sb.Filesystem.AllowWrite, "~/go/pkg/mod") || !slices.Contains(sb.Credentials.Files, struct{ Path, Mode string }{"~/.ssh", "deny"}) {
		t.Errorf("sandbox misses package caches or credential denies: %+v", sb)
	}
	for _, p := range []string{"/d/agents", "/d/f.db", "/d/f.db-wal", "/d/events.jsonl"} {
		if !slices.Contains(sb.Filesystem.DenyRead, p) || !slices.Contains(sb.Filesystem.DenyWrite, p) {
			t.Errorf("sandbox does not hide %s: %+v", p, sb.Filesystem)
		}
	}
	for _, rule := range []string{"Read(//d/agents)", "Edit(//d/agents)", "Read(//d/f.db)", "Edit(//d/events.jsonl)"} {
		if !slices.Contains(settings.Permissions.Deny, rule) {
			t.Errorf("missing deny rule %s: %v", rule, settings.Permissions.Deny)
		}
	}
	// Edit/Write are allowed only inside the working directory; secrets are denied to the file tools and to Bash writes.
	if i := slices.Index(args, "--allowedTools"); i < 0 || strings.Contains(args[i+1], "Edit") || strings.Contains(args[i+1], "Write") {
		t.Errorf("--allowedTools must not allow Edit/Write everywhere: %v", args)
	}
	if !slices.Equal(settings.Permissions.Allow, []string{"Edit(//w/dir/**)"}) {
		t.Errorf("allow rules = %v", settings.Permissions.Allow)
	}
	for _, rule := range []string{"Read(~/.ssh)", "Edit(~/.ssh)", "Edit(~/.claude/.credentials.json)"} {
		if !slices.Contains(settings.Permissions.Deny, rule) {
			t.Errorf("missing deny rule %s: %v", rule, settings.Permissions.Deny)
		}
	}
	if !slices.Contains(sb.Filesystem.DenyWrite, "~/.ssh") || !slices.Contains(sb.Filesystem.DenyWrite, "~/.aws") {
		t.Errorf("sandbox does not deny writes to credentials: %v", sb.Filesystem.DenyWrite)
	}
	if slices.Contains(args, "--bare") {
		t.Errorf("--bare would break subscription login: %v", args)
	}
	if slices.Contains(args, "--input-format") {
		t.Errorf("one-shot args take input from stdin: %v", args)
	}
	// No model given: no --model (the CLI's default); a given one goes in as --model <id>, before the prompt.
	if slices.Contains(args, "--model") {
		t.Errorf("args without a model must not pass --model: %v", args)
	}
	margs := r.args(Agent{Role: "subagent"}, "/w/dir", "/x/agent-3.mcp.json", "do -it", "SYS", "", "claude-sonnet-5-5")
	if i := slices.Index(margs, "--model"); i < 0 || margs[i+1] != "claude-sonnet-5-5" || i > slices.Index(margs, "--") {
		t.Errorf("sonnet sub-agent args: %v", margs)
	}
	r.Interactive = true
	iargs := r.args(Agent{Role: "orchestrator"}, "/w/dir", "/x/agent-3.mcp.json", "", "SYS", "", "")
	if i := slices.Index(iargs, "--input-format"); i < 0 || iargs[i+1] != "stream-json" || slices.Contains(iargs, "--") || slices.Contains(iargs, "--resume") {
		t.Errorf("interactive orchestrator args: %v", iargs)
	}
	if slices.Contains(iargs, "--model") {
		t.Errorf("the orchestrator runs on the CLI's default model, no --model: %v", iargs)
	}
	// A resumed orchestrator gets the same arguments plus --resume <claude session id>.
	rargs := r.args(Agent{Role: "orchestrator"}, "/w/dir", "/x/agent-3.mcp.json", "", "SYS", "abc-123", "")
	if i := slices.Index(rargs, "--resume"); i < 0 || rargs[i+1] != "abc-123" || !slices.Equal(slices.Delete(slices.Clone(rargs), i, i+2), iargs) {
		t.Errorf("resumed orchestrator args: %v", rargs)
	}
	// The orchestrator runs like the user's CLI: auto mode, user setup, no sandbox; only fragile tools pre-approved.
	for _, want := range [][]string{{"--permission-mode", "auto"}, {"--allowedTools", "mcp__fragile"}, {"--disallowedTools", "Task,Agent,Workflow"}} {
		if i := slices.Index(iargs, want[0]); i < 0 || iargs[i+1] != want[1] {
			t.Errorf("orchestrator args missing %v: %v", want, iargs)
		}
	}
	for _, flag := range []string{"--settings", "--setting-sources", "--disable-slash-commands", "--plugin-dir", "--dangerously-skip-permissions"} {
		if slices.Contains(iargs, flag) {
			t.Errorf("orchestrator args must not have %s: %v", flag, iargs)
		}
	}
	if slices.Contains(args, "--permission-mode") {
		t.Errorf("sub-agents keep the sandbox, not auto mode: %v", args)
	}
	// Interactive Claude sub-agents take their task and wakes on stdin too: no prompt argument.
	if sub := r.args(Agent{Role: "subagent"}, "/w/dir", "/x", "p", "SYS", "", ""); !slices.Contains(sub, "--input-format") || slices.Contains(sub, "--") {
		t.Errorf("interactive sub-agent args: %v", sub)
	}
	if !slices.Contains(args, "--strict-mcp-config") || slices.Contains(args, "--disable-slash-commands") || !slices.Contains(args, "--verbose") || slices.Contains(args, "--dangerously-skip-permissions") {
		t.Errorf("args: %v", args)
	}
}

// Sub-agents get the user's enabled plugins and a synthetic "user" plugin for personal skills.
func TestUserPluginDirs(t *testing.T) {
	claude, agents := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	if d := UserPluginDirs(agents); len(d) != 0 {
		t.Errorf("empty config dir gave %v", d)
	}
	on, off := filepath.Join(claude, "plugins", "cache", "on"), filepath.Join(claude, "plugins", "cache", "off")
	for _, p := range []string{on, off, filepath.Join(claude, "skills", "mine")} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, s string) {
		if err := os.WriteFile(filepath.Join(claude, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("settings.json", `{"enabledPlugins":{"on@m":true,"off@m":false,"gone@m":true,"missing@m":true}}`)
	write("plugins/installed_plugins.json", fmt.Sprintf(`{"plugins":{"on@m":[{"installPath":%q},{"installPath":"/other"}],"off@m":[{"installPath":%q}],"gone@m":[{"installPath":"/no/such/dir"}]}}`, on, off))
	want := []string{on, filepath.Join(agents, "user-skills")}
	for range 2 { // the second call must be idempotent
		if got := UserPluginDirs(agents); !slices.Equal(got, want) {
			t.Fatalf("UserPluginDirs = %v, want %v", got, want)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(want[1], ".claude-plugin", "plugin.json")); string(b) != `{"name":"user"}` {
		t.Errorf("plugin.json = %q", b)
	}
	if l, _ := os.Readlink(filepath.Join(want[1], "skills")); l != filepath.Join(claude, "skills") {
		t.Errorf("skills symlink -> %q", l)
	}
	r := NewRunner(Config{AgentDir: agents}, nil, nil)
	r.PluginDirs = want
	args := r.args(Agent{Role: "subagent"}, "/w", "/x", "p", "SYS", "", "")
	for _, d := range want {
		if i := slices.Index(args, d); i < 1 || args[i-1] != "--plugin-dir" {
			t.Errorf("args missing --plugin-dir %s: %v", d, args)
		}
	}
	// Unparseable files mean no plugins, and no skills dir means no synthetic plugin.
	write("settings.json", `{nope`)
	if err := os.RemoveAll(filepath.Join(claude, "skills")); err != nil {
		t.Fatal(err)
	}
	if d := UserPluginDirs(t.TempDir()); len(d) != 0 {
		t.Errorf("broken settings gave %v", d)
	}
}

// A missing sandbox prerequisite fails the launch with a readable error and starts nothing.
func TestRunnerLaunchNeedsSandbox(t *testing.T) {
	r, store, sess, _ := newTestRunner(t, `echo hi`)
	r.Preflight = func() error { return errors.New(`agent sandbox needs "bwrap"`) }
	orch, _ := store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	if _, err := r.SpawnSubagent(sess.ID, orch.ID, "", "t", ""); err == nil || !strings.Contains(err.Error(), "bwrap") {
		t.Fatalf("err = %v, want the sandbox error", err)
	}
}

// StopSession stops one session's agents, records them, and leaves the others running.
func TestRunnerStopSessionLeavesOthers(t *testing.T) {
	r, store, a, _ := newTestRunner(t, `sleep 300`)
	b, _ := store.CreateSession("other")
	oa, err := r.StartOrchestrator(a.ID, "task a")
	if err != nil {
		t.Fatal(err)
	}
	ob, err := r.StartOrchestrator(b.ID, "task b")
	if err != nil {
		t.Fatal(err)
	}
	sa, err := r.SpawnSubagent(a.ID, oa.ID, "", "sub a", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.SpawnSubagent(b.ID, oa.ID, "", "wrong session", ""); err == nil {
		t.Fatal("spawn with another session's orchestrator succeeded")
	}

	r.StopSession(a.ID)
	for _, id := range []int64{oa.ID, sa.ID} {
		if got, _ := store.GetAgent(a.ID, id); got.Status != "stopped" {
			t.Fatalf("session A agent %d status = %q", id, got.Status)
		}
	}
	if got, _ := store.GetAgent(b.ID, ob.ID); got.Status != "running" {
		t.Fatalf("session B orchestrator status = %q, want running", got.Status)
	}
	if _, err := r.SpawnSubagent(a.ID, oa.ID, "", "late", ""); err == nil {
		t.Fatal("spawn in a stopped session succeeded")
	}
	if _, err := r.SpawnSubagent(b.ID, ob.ID, "", "still fine", ""); err != nil {
		t.Fatalf("spawn in the other session: %v", err)
	}
	r.StopAll()
	if got, _ := store.GetAgent(b.ID, ob.ID); got.Status != "stopped" {
		t.Fatalf("session B orchestrator after StopAll = %q", got.Status)
	}
	if _, err := r.StartOrchestrator(b.ID, "x"); err == nil {
		t.Fatal("start after StopAll succeeded")
	}
}

func TestEventLogOnEvent(t *testing.T) {
	r, store, sess, evPath := newTestRunner(t, `exit 0`)
	var got []Event
	r.log.OnEvent = func(e Event) { got = append(got, e) }
	orch, _ := store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	if _, err := r.SpawnSubagent(sess.ID, orch.ID, "", "do it", ""); err != nil {
		t.Fatal(err)
	}
	r.Wait(sess.ID)
	if len(got) != 2 || got[0].Event != EventAgentSpawned || got[1].Event != EventAgentStatusChanged {
		t.Fatalf("events = %+v", got)
	}
	for _, e := range got {
		if e.SessionID != sess.ID || e.AgentID == 0 {
			t.Fatalf("event without session/agent: %+v", e)
		}
	}
	if ev, _ := os.ReadFile(evPath); !strings.Contains(string(ev), `"session_id":`+itoa(sess.ID)) {
		t.Fatalf("log lines lack session_id:\n%s", ev)
	}
}

func TestRunnerIsolationEnvAndWorkDir(t *testing.T) {
	r, store, sess, _ := newTestRunner(t, `echo "$CLAUDE_CODE_DISABLE_AUTO_MEMORY" "$PWD"; for a in "$@"; do echo "$a"; done`)
	orch, _ := store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	a, err := r.SpawnSubagent(sess.ID, orch.ID, "", "do it", "")
	if err != nil {
		t.Fatal(err)
	}
	r.Wait(sess.ID)
	out, _ := os.ReadFile(a.LogPath)
	if !strings.HasPrefix(string(out), "1 ") {
		t.Fatalf("auto-memory env not set: %q", out)
	}
	wd := r.cfg.WorkDir
	if !strings.Contains(string(out), "Your working directory is `"+wd+"`") || strings.Contains(string(out), "{{") {
		t.Fatalf("prompt lacks work dir %q or has unreplaced placeholders:\n%s", wd, out)
	}
}

func TestPromptsWorkDirAndNoSleep(t *testing.T) {
	for name, p := range map[string]string{"orchestrator": OrchestratorPrompt("/w/dir", false), "subagent": SubagentPrompt(7, "task {{WORKDIR}}", "/w/dir", false)} {
		if !strings.Contains(p, "`/w/dir`") || !strings.Contains(p, "wait_for_notes") || strings.Contains(p, "sleep") {
			t.Errorf("%s prompt: workdir/wait_for_notes/sleep check failed", name)
		}
	}
}

// An interactive orchestrator takes stream-json user messages on stdin, and
// OnLine sees every output line while the log file still gets all of it.
func TestRunnerInteractiveStdinAndOnLine(t *testing.T) {
	r, store, sess, _ := newTestRunner(t, `while IFS= read -r line; do echo "got: $line"; done`)
	r.Interactive = true
	var mu sync.Mutex
	var lines []string
	r.OnLine = func(a Agent, l []byte) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, string(l))
	}
	a, err := r.StartOrchestrator(sess.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Running(a.ID) {
		t.Fatal("orchestrator not running")
	}
	if err := r.SendUser(a.ID, `say "hi"`); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		mu.Lock()
		n := len(lines)
		mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no output line")
		}
	}
	want := `got: {"message":{"content":[{"text":"say \"hi\"","type":"text"}],"role":"user"},"type":"user"}`
	if lines[0] != want {
		t.Fatalf("line = %s\nwant   %s", lines[0], want)
	}
	r.StopAll()
	if r.Running(a.ID) || r.SendUser(a.ID, "x") != ErrNotRunning {
		t.Error("stopped orchestrator still takes messages")
	}
	got, _ := store.GetAgent(sess.ID, a.ID)
	if got.Status != "stopped" {
		t.Errorf("status = %s", got.Status)
	}
	if b, _ := os.ReadFile(a.LogPath); !strings.Contains(string(b), "got: ") {
		t.Errorf("log file = %q", b)
	}
}

// An interactive sub-agent gets its task as the first stdin message and exits
// once its stdin is closed; its prompt waits by ending the turn.
func TestRunnerInteractiveSubagentStdin(t *testing.T) {
	r, store, sess, _ := newTestRunner(t, `while IFS= read -r line; do echo "got: $line"; done`)
	r.Interactive = true
	got := make(chan string, 4)
	r.OnLine = func(a Agent, l []byte) { got <- string(l) }
	orch, _ := store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	a, err := r.SpawnSubagent(sess.ID, orch.ID, "", "do -it", "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case l := <-got:
		if want := `got: {"message":{"content":[{"text":"do -it","type":"text"}],"role":"user"},"type":"user"}`; l != want {
			t.Fatalf("line = %s\nwant   %s", l, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no task on stdin")
	}
	if !r.Running(a.ID) {
		t.Fatal("sub-agent not running after its first message")
	}
	r.CloseStdin(a.ID)
	r.Wait(sess.ID)
	if ag, _ := store.GetAgent(sess.ID, a.ID); ag.Status != "exited" {
		t.Fatalf("after CloseStdin: %+v", ag)
	}
	one, chat := SubagentPrompt(7, "t", "/w", false), SubagentPrompt(7, "t", "/w", true)
	if strings.Contains(one, "{{") || strings.Contains(chat, "{{") || strings.Contains(one, "[Fragile]") ||
		!strings.Contains(one, "`wait_for_notes` for the orchestrator's reply") || !strings.Contains(chat, `"[Fragile] Board update"`) {
		t.Error("sub-agent prompt modes wrong")
	}
}

// SendUserContent writes the content blocks as given, in one line however long.
func TestRunnerSendUserContent(t *testing.T) {
	r, _, sess, _ := newTestRunner(t, `cat`)
	r.Interactive = true
	got := make(chan []byte, 4)
	r.OnLine = func(a Agent, l []byte) {
		select {
		case got <- append([]byte(nil), l...):
		default:
		}
	}
	a, err := r.StartOrchestrator(sess.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer r.StopAll()
	if err := r.SendUserContent(a.ID, nil); err == nil {
		t.Error("empty content accepted")
	}
	data := strings.Repeat("A", 1<<20)
	blocks := []map[string]any{
		{"type": "text", "text": "look"},
		{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": data}},
	}
	if err := r.SendUserContent(a.ID, blocks); err != nil {
		t.Fatal(err)
	}
	var line []byte
	select {
	case line = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("no output line")
	}
	var msg struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type   string `json:"type"`
				Text   string `json:"text"`
				Source struct {
					Type      string `json:"type"`
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
				} `json:"source"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		t.Fatal(err)
	}
	c := msg.Message.Content
	if msg.Type != "user" || msg.Message.Role != "user" || len(c) != 2 || c[0].Type != "text" || c[0].Text != "look" ||
		c[1].Type != "image" || c[1].Source.Type != "base64" || c[1].Source.MediaType != "image/png" || c[1].Source.Data != data {
		t.Fatalf("message = %.300s", line)
	}
}

func TestLineWriter(t *testing.T) {
	var lines []string
	var out strings.Builder
	w := &lineWriter{w: &out, onLine: func(l []byte) { lines = append(lines, string(l)) }}
	for _, chunk := range []string{"ab", "c\nde", "f\n\ngh\nij"} {
		w.Write([]byte(chunk))
	}
	if strings.Join(lines, "|") != "abc|def|gh" || out.String() != "abc\ndef\n\ngh\nij" {
		t.Fatalf("lines = %q, out = %q", lines, out.String())
	}
}

func TestOrchestratorPromptModes(t *testing.T) {
	one, chat := OrchestratorPrompt("/w", false), OrchestratorPrompt("/w", true)
	for _, p := range []string{one, chat} {
		if strings.Contains(p, "{{") {
			t.Errorf("unreplaced placeholder in prompt")
		}
		if !strings.Contains(p, "Tokens are a priority") || !strings.Contains(p, "spawn_subagent(title, task, scopes, model?)") {
			t.Errorf("orchestrator prompt lacks the model rule")
		}
	}
	if !strings.Contains(one, "one-shot") || !strings.Contains(one, "log-only") || strings.Contains(one, "Answer to your escalation") {
		t.Error("one-shot prompt wrong")
	}
	if strings.Contains(chat, "one-shot") || strings.Contains(chat, "log-only") || !strings.Contains(chat, "Answer to your escalation #N") {
		t.Error("interactive prompt wrong")
	}
	// One-shot keeps the wait_for_notes loop; interactive ends its turn and is woken by Fragile.
	if !strings.Contains(one, "**Wait loop.**") || strings.Contains(one, "[Fragile]") {
		t.Error("one-shot prompt lost the wait loop")
	}
	if strings.Contains(chat, "**Wait loop.**") || !strings.Contains(chat, `"[Fragile] Board update"`) || strings.Contains(chat, "usual wait loop") {
		t.Error("interactive prompt still describes the wait loop")
	}
}

// Billing-related variables never reach an agent; subscription auth does.
func TestAgentEnv(t *testing.T) {
	in := []string{"PATH=/bin", "ANTHROPIC_API_KEY=k", "ANTHROPIC_AUTH_TOKEN=t", "ANTHROPIC_BASE_URL=u",
		"CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CODE_USE_VERTEX=1", "CLAUDE_CODE_OAUTH_TOKEN=o", "ANTHROPIC_MODEL=haiku"}
	got := agentEnv(in, true)
	want := []string{"PATH=/bin", "CLAUDE_CODE_OAUTH_TOKEN=o", "ANTHROPIC_MODEL=haiku", isolationEnv}
	if !slices.Equal(got, want) {
		t.Fatalf("agentEnv = %v, want %v", got, want)
	}
	if got := agentEnv(in, false); !slices.Equal(got, want[:3]) { // the orchestrator keeps auto-memory
		t.Fatalf("agentEnv(orchestrator) = %v, want %v", got, want[:3])
	}
	// End to end: the child process sees none of them.
	t.Setenv("ANTHROPIC_API_KEY", "sk-leak")
	r, store, sess, _ := newTestRunner(t, `echo "key=[$ANTHROPIC_API_KEY]"`)
	orch, _ := store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	a, err := r.SpawnSubagent(sess.ID, orch.ID, "", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	r.Wait(sess.ID)
	if out, _ := os.ReadFile(a.LogPath); strings.TrimSpace(string(out)) != "key=[]" {
		t.Fatalf("child env leaked the API key: %q", out)
	}
}

// Symlinked paths (/tmp on macOS) are resolved in the settings; missing files resolve through their directory.
func TestSandboxSettingsResolveSymlinks(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.Mkdir(filepath.Join(base, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "real"), link); err != nil {
		t.Fatal(err)
	}
	st := sandboxSettings(Config{AgentDir: link + "/agents", DBPath: link + "/f.db"}, link)
	for _, want := range []string{`"Edit(/` + base + `/real/**)"`, `"Read(/` + base + `/real/f.db-wal)"`, `"` + base + `/real/agents"`} {
		if !strings.Contains(st, want) {
			t.Errorf("settings lack %s:\n%s", want, st)
		}
	}
	if strings.Contains(st, "/link") {
		t.Errorf("settings still use the symlink:\n%s", st)
	}
}

// At most maxRunningSubagents run per session, and a task has a size limit.
func TestRunnerSpawnCaps(t *testing.T) {
	r, store, sess, _ := newTestRunner(t, `sleep 300`)
	orch, _ := store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	defer r.StopAll()
	if _, err := r.SpawnSubagent(sess.ID, orch.ID, "", strings.Repeat("x", maxTaskBytes+1), ""); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized task: err = %v", err)
	}
	for i := 0; i < maxRunningSubagents; i++ {
		if _, err := r.SpawnSubagent(sess.ID, orch.ID, "", "t", ""); err != nil {
			t.Fatalf("spawn %d: %v", i, err)
		}
	}
	if _, err := r.SpawnSubagent(sess.ID, orch.ID, "", "one too many", ""); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("spawn over the cap: err = %v", err)
	}
	if ts, _ := store.ListTasks(sess.ID); len(ts) != maxRunningSubagents {
		t.Fatalf("a refused spawn left a task behind: %d tasks", len(ts))
	}
}

// A refused launch writes no files; Forget lets a session that reuses the id launch again.
func TestRunnerRefusedLaunchAndForget(t *testing.T) {
	r, store, sess, _ := newTestRunner(t, `exit 0`)
	r.StopSession(sess.ID)
	if _, err := r.StartOrchestrator(sess.ID, "x"); err == nil {
		t.Fatal("launch in a stopped session succeeded")
	}
	if m, _ := filepath.Glob(filepath.Join(r.cfg.AgentDir, "agent-*")); len(m) != 0 {
		t.Fatalf("refused launch left files: %v", m)
	}
	r.Forget(sess.ID)
	a, err := r.StartOrchestrator(sess.ID, "x")
	if err != nil {
		t.Fatalf("launch after Forget: %v", err)
	}
	r.Wait(sess.ID)
	if got, _ := store.GetAgent(sess.ID, a.ID); got.Status != "exited" {
		t.Fatalf("status = %s", got.Status)
	}
}

// ResumeOrchestrator lifts StopSession (not StopAll) and passes --resume to a new orchestrator row.
func TestRunnerResumeOrchestrator(t *testing.T) {
	r, _, sess, _ := newTestRunner(t, `echo "args: $*"`)
	first, err := r.StartOrchestrator(sess.ID, "x")
	if err != nil {
		t.Fatal(err)
	}
	r.StopSession(sess.ID)
	if _, err := r.ResumeOrchestrator(sess.ID, ""); err == nil {
		t.Fatal("resume without a Claude session id succeeded")
	}
	a, err := r.ResumeOrchestrator(sess.ID, "abc-123")
	if err != nil {
		t.Fatalf("resume after StopSession: %v", err)
	}
	r.Wait(sess.ID)
	if a.ID == first.ID || a.Role != "orchestrator" {
		t.Fatalf("resumed agent = %+v", a)
	}
	if out, _ := os.ReadFile(a.LogPath); !strings.Contains(string(out), "--resume abc-123") {
		t.Fatalf("resumed args = %q", out)
	}
	r.StopAll()
	if _, err := r.ResumeOrchestrator(sess.ID, "abc-123"); err == nil {
		t.Fatal("resume after StopAll succeeded")
	}
}

// ResumeSubagent relaunches a sub-agent on its own row with --resume, appending to its log.
func TestRunnerResumeSubagent(t *testing.T) {
	r, store, sess, _ := newTestRunner(t, `echo "args: $*"`)
	orch, _ := store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	a, err := r.SpawnSubagent(sess.ID, orch.ID, "", "build it", "")
	if err != nil {
		t.Fatal(err)
	}
	r.Wait(sess.ID)
	if _, err := r.ResumeSubagent(sess.ID, a.ID, "", "go on"); err == nil {
		t.Fatal("resume without a Claude session id succeeded")
	}
	if _, err := r.ResumeSubagent(sess.ID, orch.ID, "abc-123", "go on"); err == nil {
		t.Fatal("resumed the orchestrator as a sub-agent")
	}
	b, err := r.ResumeSubagent(sess.ID, a.ID, "abc-123", "go on")
	if err != nil {
		t.Fatal(err)
	}
	r.Wait(sess.ID)
	if b.ID != a.ID {
		t.Fatalf("resumed as agent %d, want %d", b.ID, a.ID)
	}
	out, _ := os.ReadFile(a.LogPath)
	if s := string(out); strings.Count(s, "args:") != 2 || !strings.Contains(s, "--resume abc-123") || !strings.Contains(s, "-- go on") {
		t.Fatalf("log = %q", s)
	}
	if got, _ := store.GetAgent(sess.ID, a.ID); got.Status != "exited" {
		t.Fatalf("status = %s", got.Status)
	}
}

// StopAgent records the agent as stopped even when it exits 0 on SIGTERM.
func TestRunnerStopAgent(t *testing.T) {
	r, store, sess, _ := newTestRunner(t, `trap 'exit 0' TERM; sleep 30 & wait`)
	a, err := r.StartOrchestrator(sess.ID, "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.StopAgent(a.ID, false); err != nil {
		t.Fatal(err)
	}
	r.Wait(sess.ID)
	if got, _ := store.GetAgent(sess.ID, a.ID); got.Status != "stopped" {
		t.Fatalf("status = %s", got.Status)
	}
	if err := r.StopAgent(a.ID, false); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("second stop: %v", err)
	}
}

// An explicit title wins over the task's first line.
func TestRunnerSpawnTitle(t *testing.T) {
	r, store, sess, _ := newTestRunner(t, `exit 0`)
	orch, _ := store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	for title, want := range map[string]string{"Auth API": "Auth API", "": "Working directory: /tmp/x"} {
		a, err := r.SpawnSubagent(sess.ID, orch.ID, title, "Working directory: /tmp/x\nbuild it", "")
		if err != nil {
			t.Fatal(err)
		}
		if task, _ := store.GetTask(sess.ID, a.TaskID); task.Title != want || !strings.Contains(task.Description, "build it") {
			t.Errorf("title %q: task = %+v, want title %q", title, task, want)
		}
	}
	r.Wait(sess.ID)
}
