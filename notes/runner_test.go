package notes

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// newTestRunner returns a Runner whose "claude" is a shell script with the given body.
func newTestRunner(t *testing.T, script string) (*Runner, *Store, string) {
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
	return r, store, evPath
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
	r, store, evPath := newTestRunner(t, `echo '{"type":"result"}'`)
	orch, _ := store.CreateAgent("orchestrator", 0, 0)
	a, err := r.SpawnSubagent(orch.ID, "first line\nsecond line")
	if err != nil {
		t.Fatal(err)
	}
	if a.PID <= 0 || a.PID == os.Getpid() || a.LogPath == "" || a.Role != "subagent" || a.ParentID != orch.ID {
		t.Fatalf("unexpected agent: %+v", a)
	}
	r.Wait()

	got, _ := store.GetAgent(a.ID)
	if got.Status != "exited" || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("agent after exit: %+v", got)
	}
	task, _ := store.GetTask(a.TaskID)
	if task.Title != "first line" || task.Description != "first line\nsecond line" || task.Status != "done" || task.AgentID != a.ID {
		t.Fatalf("task: %+v", task)
	}
	if b, _ := os.ReadFile(a.LogPath); strings.TrimSpace(string(b)) != `{"type":"result"}` {
		t.Fatalf("log file = %q", b)
	}
	tok, _ := store.GetAgent(a.ID)
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
	r, store, evPath := newTestRunner(t, `sleep 1`)
	orch, _ := store.CreateAgent("orchestrator", 0, 0)
	a, err := r.SpawnSubagent(orch.ID, "do it")
	if err != nil {
		t.Fatal(err)
	}
	store.PostNote(store.BoardID, a.ID, "done", "finished")
	r.Wait()
	if ev, _ := os.ReadFile(evPath); !strings.Contains(string(ev), `"status":"exited"`) || strings.Contains(string(ev), "missing_done_note") {
		t.Fatalf("events:\n%s", ev)
	}
}

func TestRunnerSubagentCrashes(t *testing.T) {
	r, store, evPath := newTestRunner(t, `echo boom >&2; exit 1`)
	orch, _ := store.CreateAgent("orchestrator", 0, 0)
	a, err := r.SpawnSubagent(orch.ID, "do it")
	if err != nil {
		t.Fatal(err)
	}
	r.Wait()
	got, _ := store.GetAgent(a.ID)
	if got.Status != "crashed" || got.ExitCode == nil || *got.ExitCode != 1 {
		t.Fatalf("agent: %+v", got)
	}
	if task, _ := store.GetTask(a.TaskID); task.Status != "blocked" {
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
	r, store, evPath := newTestRunner(t, `sleep 300`)
	orch, err := r.StartOrchestrator("build a thing")
	if err != nil {
		t.Fatal(err)
	}
	if orch.Role != "orchestrator" || orch.PID <= 0 || orch.TaskID != 0 {
		t.Fatalf("agent: %+v", orch)
	}
	sub, err := r.SpawnSubagent(orch.ID, "do it")
	if err != nil {
		t.Fatal(err)
	}

	r.StopAll() // no Wait: StopAll itself must leave state and log final
	for _, id := range []int64{orch.ID, sub.ID} {
		if got, _ := store.GetAgent(id); got.Status != "crashed" { // SIGTERM: exit code -1
			t.Fatalf("agent %d status after StopAll = %q", id, got.Status)
		}
	}
	if task, _ := store.GetTask(sub.TaskID); task.Status != "blocked" {
		t.Fatalf("task status = %q", task.Status)
	}
	if ev, _ := os.ReadFile(evPath); strings.Count(string(ev), `"event":"agent_status_changed"`) != 2 {
		t.Fatalf("want 2 agent_status_changed events:\n%s", ev)
	}
	if _, err := r.SpawnSubagent(orch.ID, "too late"); err == nil {
		t.Fatal("SpawnSubagent after StopAll succeeded")
	}
}

func TestRunnerArgs(t *testing.T) {
	r := NewRunner(Config{}, nil, nil)
	args := r.args("/x/agent-3.mcp.json", "do -it", "SYS")
	for _, want := range [][]string{
		{"--mcp-config", "/x/agent-3.mcp.json"}, {"--append-system-prompt", "SYS"},
		{"--output-format", "stream-json"}, {"--disallowedTools", "Task,Agent,Workflow"}, {"--", "do -it"},
	} {
		if i := slices.Index(args, want[0]); i < 0 || args[i+1] != want[1] {
			t.Errorf("args missing %v: %v", want, args)
		}
	}
	if !slices.Contains(args, "--strict-mcp-config") || !slices.Contains(args, "--verbose") || slices.Contains(args, "--dangerously-skip-permissions") {
		t.Errorf("args: %v", args)
	}
}
