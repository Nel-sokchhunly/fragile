package notes

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type waitResult struct {
	text  string
	isErr bool
	took  time.Duration
}

// callAsync runs a tool call in the background; its result arrives on the channel.
func callAsync(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) <-chan waitResult {
	t.Helper()
	ch := make(chan waitResult, 1)
	go func() {
		start := time.Now()
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			ch <- waitResult{err.Error(), true, time.Since(start)}
			return
		}
		ch <- waitResult{res.Content[0].(*mcp.TextContent).Text, res.IsError, time.Since(start)}
	}()
	return ch
}

func expectBlocked(t *testing.T, ch <-chan waitResult) {
	t.Helper()
	select {
	case r := <-ch:
		t.Fatalf("returned early: %+v", r)
	case <-time.After(300 * time.Millisecond):
	}
}

func expectWake(t *testing.T, ch <-chan waitResult) waitResult {
	t.Helper()
	select {
	case r := <-ch:
		if r.isErr {
			t.Fatalf("tool error: %s", r.text)
		}
		if r.took > 5*time.Second {
			t.Fatalf("took %v", r.took)
		}
		return r
	case <-time.After(time.Second):
		t.Fatal("waiter not woken within 1s")
		return waitResult{}
	}
}

func post(t *testing.T, cs *mcp.ClientSession, typ, content string) {
	t.Helper()
	if out, isErr := call(t, cs, "post_note", map[string]any{"scope": "session", "type": typ, "content": content}); isErr {
		t.Fatal(out)
	}
}

func TestWaitForNotesWakesOnPost(t *testing.T) {
	s, ts := newTestServer(t)
	sess, _ := s.Store.CreateSession("test")
	orch, _ := s.Store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	a, _ := s.Store.CreateAgent(sess.ID, "subagent", orch.ID, 0)
	b, _ := s.Store.CreateAgent(sess.ID, "subagent", orch.ID, 0)
	ca, cb := connect(t, ts, a.Token), connect(t, ts, b.Token)

	ch := callAsync(t, ca, "wait_for_notes", map[string]any{"since_id": 0, "timeout_s": 30})
	expectBlocked(t, ch)
	post(t, cb, "heads_up", "hello from B")
	r := expectWake(t, ch)
	if !strings.Contains(r.text, "hello from B") || !strings.Contains(r.text, `"id":1`) {
		t.Fatalf("result = %s", r.text)
	}

	// Existing notes: returns at once. A newer since_id blocks again.
	if r := expectWake(t, callAsync(t, ca, "wait_for_notes", map[string]any{"since_id": 0})); !strings.Contains(r.text, "hello from B") {
		t.Fatalf("immediate result = %s", r.text)
	}
	ch = callAsync(t, ca, "wait_for_notes", map[string]any{"since_id": 1, "timeout_s": 30})
	expectBlocked(t, ch)
	post(t, cb, "done", "second")
	if r := expectWake(t, ch); strings.Contains(r.text, "hello from B") || !strings.Contains(r.text, "second") {
		t.Fatalf("result = %s", r.text)
	}
}

func TestWaitForNotesTimeoutAndValidation(t *testing.T) {
	s, ts := newTestServer(t)
	sess, _ := s.Store.CreateSession("test")
	orch, _ := s.Store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	a, _ := s.Store.CreateAgent(sess.ID, "subagent", orch.ID, 0)
	ca := connect(t, ts, a.Token)

	r := <-callAsync(t, ca, "wait_for_notes", map[string]any{"since_id": 0, "timeout_s": 1})
	if r.isErr || r.text != `{"notes":[]}` || r.took < 900*time.Millisecond || r.took > 3*time.Second {
		t.Fatalf("timeout result = %+v", r)
	}
	if out, isErr := call(t, ca, "wait_for_notes", map[string]any{"since_id": 0, "type": "nope"}); !isErr {
		t.Fatalf("bad type allowed: %s", out)
	}
	if out, isErr := call(t, ca, "wait_for_notes", map[string]any{"since_id": 0, "finished_subagents": 0}); !isErr {
		t.Fatalf("sub-agent may not use finished_subagents: %s", out)
	}
}

func TestWaitForNotesTypeFilterAndIsolation(t *testing.T) {
	s, ts := newTestServer(t)
	s1, _ := s.Store.CreateSession("one")
	s2, _ := s.Store.CreateSession("two")
	o1, _ := s.Store.CreateAgent(s1.ID, "orchestrator", 0, 0)
	a1, _ := s.Store.CreateAgent(s1.ID, "subagent", o1.ID, 0)
	o2, _ := s.Store.CreateAgent(s2.ID, "orchestrator", 0, 0)
	c1, c1b, c2 := connect(t, ts, a1.Token), connect(t, ts, o1.Token), connect(t, ts, o2.Token)

	ch := callAsync(t, c1, "wait_for_notes", map[string]any{"since_id": 0, "type": "done", "timeout_s": 30})
	expectBlocked(t, ch)
	post(t, c2, "done", "other session") // wrong session: must not wake
	expectBlocked(t, ch)
	post(t, c1b, "heads_up", "wrong type") // wrong type: wakes internally, must keep waiting
	expectBlocked(t, ch)
	post(t, c1b, "done", "right one")
	if r := expectWake(t, ch); !strings.Contains(r.text, "right one") || strings.Contains(r.text, "wrong type") || strings.Contains(r.text, "other session") {
		t.Fatalf("result = %s", r.text)
	}
}

func TestWaitForNotesCancelled(t *testing.T) {
	s, ts := newTestServer(t)
	sess, _ := s.Store.CreateSession("test")
	a, _ := s.Store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	c := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp/" + a.Token}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "wait_for_notes", Arguments: map[string]any{"since_id": 0, "timeout_s": 60}}); err == nil {
		t.Fatal("expected cancellation error")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancel did not return promptly")
	}
	// The server-side handler must end too: the deferred ts.Close blocks on open requests, so a hang shows as a slow test.
}

func TestOrchestratorWakesOnSubagentExit(t *testing.T) {
	r, store, sess, _ := newTestRunner(t, `sleep 1`)
	srv := &Server{Store: store, Log: r.log, Runner: r}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	orch, _ := store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	co := connect(t, ts, orch.Token)

	// No sub-agents: nothing to wait for, returns at once.
	if out := expectWake(t, callAsync(t, co, "wait_for_notes", map[string]any{"since_id": 0, "finished_subagents": 0})); out.text != `{"notes":[],"finished_subagents":0,"running_subagents":0}` {
		t.Fatalf("no sub-agents: %s", out.text)
	}
	if _, err := r.SpawnSubagent(sess.ID, orch.ID, "do it"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	res, err := co.CallTool(context.Background(), &mcp.CallToolParams{Name: "wait_for_notes", Arguments: map[string]any{"since_id": 0, "finished_subagents": 0, "timeout_s": 30}})
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	var out waitOut
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < 500*time.Millisecond || took > 4*time.Second {
		t.Fatalf("woke after %v, want about the sub-agent's 1s", took)
	}
	if len(out.Notes) != 0 || *out.FinishedSubagents != 1 || *out.RunningSubagents != 0 {
		t.Fatalf("result = %+v", out)
	}
	r.Wait(sess.ID)
}
