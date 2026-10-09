package notes

import (
	"strings"
	"testing"
)

func TestMCPPrivateScopes(t *testing.T) {
	s, ts := newTestServer(t)
	sess, _ := s.Store.CreateSession("test")
	orch, _ := s.Store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	a, _ := s.Store.CreateAgent(sess.ID, "subagent", orch.ID, 0)
	b, _ := s.Store.CreateAgent(sess.ID, "subagent", orch.ID, 0)
	c, _ := s.Store.CreateAgent(sess.ID, "subagent", orch.ID, 0)
	for _, ag := range []Agent{a, b} {
		if err := s.Store.JoinScopes(sess.ID, ag.ID, []string{"session", "private:auth"}); err != nil {
			t.Fatal(err)
		}
	}
	co, ca, cb, cc := connect(t, ts, orch.Token), connect(t, ts, a.Token), connect(t, ts, b.Token), connect(t, ts, c.Token)

	if out, isErr := call(t, ca, "post_note", map[string]any{"scope": "private:auth", "type": "decision", "content": "use jwt"}); isErr || !strings.Contains(out, `"scope":"private:auth"`) {
		t.Fatalf("member post = %s", out)
	}
	if out, isErr := call(t, ca, "post_note", map[string]any{"scope": "session", "type": "heads_up", "content": "public"}); isErr || !strings.Contains(out, `"scope":"session"`) {
		t.Fatalf("session post = %s", out)
	}

	// member and orchestrator see the private note; non-member does not
	if out, isErr := call(t, cb, "read_notes", map[string]any{"scope": "private:auth"}); isErr || !strings.Contains(out, "use jwt") {
		t.Fatalf("member read = %s", out)
	}
	if out, isErr := call(t, co, "read_notes", map[string]any{"scope": "all"}); isErr || !strings.Contains(out, "use jwt") || !strings.Contains(out, "public") {
		t.Fatalf("orchestrator read all = %s", out)
	}
	if out, isErr := call(t, co, "read_notes", map[string]any{"scope": "private:auth"}); isErr || !strings.Contains(out, "use jwt") {
		t.Fatalf("orchestrator read private = %s", out)
	}
	if out, isErr := call(t, cc, "read_notes", map[string]any{"scope": "private:auth"}); !isErr || !strings.Contains(out, "not available") {
		t.Fatalf("non-member read allowed: %s", out)
	}
	if out, isErr := call(t, cc, "read_notes", map[string]any{"scope": "all"}); isErr || strings.Contains(out, "use jwt") || !strings.Contains(out, "public") {
		t.Fatalf("non-member read all = %s", out)
	}
	if out, isErr := call(t, cc, "post_note", map[string]any{"scope": "private:auth", "type": "heads_up", "content": "x"}); !isErr {
		t.Fatalf("non-member post allowed: %s", out)
	}
	if out, isErr := call(t, cc, "read_notes", map[string]any{"scope": "private:nope"}); !isErr || !strings.Contains(out, "not available") {
		t.Fatalf("unknown scope read = %s", out) // same error as a scope you are not in
	}
	if out, isErr := call(t, cc, "update_note", map[string]any{"id": 1, "status": "resolved"}); !isErr || !strings.Contains(out, "not found") {
		t.Fatalf("non-member update allowed: %s", out)
	}
	if out, isErr := call(t, cb, "update_note", map[string]any{"id": 1, "status": "resolved"}); isErr {
		t.Fatalf("member update = %s", out)
	}
	if out, isErr := call(t, ca, "post_note", map[string]any{"scope": "private:auth", "type": "done", "content": "x"}); !isErr {
		t.Fatalf("done in private scope allowed: %s", out)
	}
	if _, isErr := call(t, ca, "post_note", map[string]any{"scope": "private:Bad Name", "type": "heads_up", "content": "x"}); !isErr {
		t.Fatal("invalid scope name accepted")
	}

	// wait_for_notes only wakes for accessible scopes
	if out, isErr := call(t, cc, "wait_for_notes", map[string]any{"since_id": 0, "timeout_s": 1, "type": "decision"}); isErr || strings.Contains(out, "use jwt") {
		t.Fatalf("non-member wait = %s", out)
	}
	if out, isErr := call(t, cb, "wait_for_notes", map[string]any{"since_id": 0, "timeout_s": 1, "type": "decision"}); isErr || !strings.Contains(out, "use jwt") {
		t.Fatalf("member wait = %s", out)
	}
	if out, isErr := call(t, cc, "wait_for_notes", map[string]any{"since_id": 0, "timeout_s": 1, "scope": "private:auth"}); !isErr {
		t.Fatalf("non-member scoped wait allowed: %s", out)
	}
}

func TestStoreJoinScopes(t *testing.T) {
	s, _ := newTestServer(t)
	sess, _ := s.Store.CreateSession("test")
	other, _ := s.Store.CreateSession("other")
	orch, _ := s.Store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	a, _ := s.Store.CreateAgent(sess.ID, "subagent", orch.ID, 0)
	if err := s.Store.JoinScopes(sess.ID, a.ID, []string{"private:x", "private:x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.JoinScopes(sess.ID, a.ID, []string{"bogus"}); err == nil {
		t.Fatal("bogus scope accepted")
	}
	if got, _ := s.Store.AgentScopes(sess.ID, a.ID); len(got) != 2 || got[0].Name != "session" || got[1].Name != "private:x" {
		t.Fatalf("AgentScopes = %+v", got)
	}
	if got, _ := s.Store.ListScopes(other.ID); len(got) != 1 {
		t.Fatalf("scopes leaked across sessions: %+v", got)
	}
	// default: no scopes means session only
	b, _ := s.Store.CreateAgent(sess.ID, "subagent", orch.ID, 0)
	if got, _ := s.Store.AgentScopes(sess.ID, b.ID); len(got) != 1 || got[0].Name != "session" {
		t.Fatalf("default AgentScopes = %+v", got)
	}
}
