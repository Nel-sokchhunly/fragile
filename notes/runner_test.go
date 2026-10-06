package notes

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
	a, err := r.SpawnSubagent(sess.ID, orch.ID, "first line\nsecond line")
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
	a, err := r.SpawnSubagent(sess.ID, orch.ID, "do it")
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
	a, err := r.SpawnSubagent(sess.ID, orch.ID, "do it")
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
	sub, err := r.SpawnSubagent(sess.ID, orch.ID, "do it")
	if err != nil {
		t.Fatal(err)
	}

	r.StopAll() // no Wait: StopAll itself must leave state and log final
	for _, id := range []int64{orch.ID, sub.ID} {
		if got, _ := store.GetAgent(sess.ID, id); got.Status != "crashed" { // SIGTERM: exit code -1
			t.Fatalf("agent %d status after StopAll = %q", id, got.Status)
		}
	}
	if task, _ := store.GetTask(sess.ID, sub.TaskID); task.Status != "blocked" {
		t.Fatalf("task status = %q", task.Status)
	}
	if ev, _ := os.ReadFile(evPath); strings.Count(string(ev), `"event":"agent_status_changed"`) != 2 {
		t.Fatalf("want 2 agent_status_changed events:\n%s", ev)
	}
	if _, err := r.SpawnSubagent(sess.ID, orch.ID, "too late"); err == nil {
		t.Fatal("SpawnSubagent after StopAll succeeded")
	}
}

func TestRunnerArgs(t *testing.T) {
	r := NewRunner(Config{}, nil, nil)
	args := r.args("/x/agent-3.mcp.json", "do -it", "SYS")
	for _, want := range [][]string{
		{"--mcp-config", "/x/agent-3.mcp.json"}, {"--append-system-prompt", "SYS"},
		{"--output-format", "stream-json"}, {"--disallowedTools", "Task,Agent,Workflow"}, {"--", "do -it"},
		{"--setting-sources", "project"},
	} {
		if i := slices.Index(args, want[0]); i < 0 || args[i+1] != want[1] {
			t.Errorf("args missing %v: %v", want, args)
		}
	}
	if !slices.Contains(args, "--strict-mcp-config") || !slices.Contains(args, "--disable-slash-commands") || !slices.Contains(args, "--verbose") || slices.Contains(args, "--dangerously-skip-permissions") {
		t.Errorf("args: %v", args)
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
	sa, err := r.SpawnSubagent(a.ID, oa.ID, "sub a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.SpawnSubagent(b.ID, oa.ID, "wrong session"); err == nil {
		t.Fatal("spawn with another session's orchestrator succeeded")
	}

	r.StopSession(a.ID)
	for _, id := range []int64{oa.ID, sa.ID} {
		if got, _ := store.GetAgent(a.ID, id); got.Status != "crashed" {
			t.Fatalf("session A agent %d status = %q", id, got.Status)
		}
	}
	if got, _ := store.GetAgent(b.ID, ob.ID); got.Status != "running" {
		t.Fatalf("session B orchestrator status = %q, want running", got.Status)
	}
	if _, err := r.SpawnSubagent(a.ID, oa.ID, "late"); err == nil {
		t.Fatal("spawn in a stopped session succeeded")
	}
	if _, err := r.SpawnSubagent(b.ID, ob.ID, "still fine"); err != nil {
		t.Fatalf("spawn in the other session: %v", err)
	}
	r.StopAll()
	if got, _ := store.GetAgent(b.ID, ob.ID); got.Status != "crashed" {
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
	if _, err := r.SpawnSubagent(sess.ID, orch.ID, "do it"); err != nil {
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
	a, err := r.SpawnSubagent(sess.ID, orch.ID, "do it")
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
	for name, p := range map[string]string{"orchestrator": OrchestratorPrompt("/w/dir"), "subagent": SubagentPrompt(7, "task {{WORKDIR}}", "/w/dir")} {
		if !strings.Contains(p, "`/w/dir`") || !strings.Contains(p, "wait_for_notes") || strings.Contains(p, "sleep") {
			t.Errorf("%s prompt: workdir/wait_for_notes/sleep check failed", name)
		}
	}
}
