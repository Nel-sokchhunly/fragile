package notes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	evlog, err := OpenEventLog(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { evlog.Close() })
	s := &Server{Store: store, Log: evlog}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func connect(t *testing.T, ts *httptest.Server, agentID int64) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp/" + itoa(agentID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// call returns the text of a tool result and whether it was a tool error.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error(), true // protocol-level refusal, e.g. unknown tool
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestMCPRolesAndNotes(t *testing.T) {
	s, ts := newTestServer(t)
	orch, _ := s.Store.CreateAgent("orchestrator", 0, 0)
	a, _ := s.Store.CreateAgent("subagent", orch.ID, 0)
	b, _ := s.Store.CreateAgent("subagent", orch.ID, 0)
	ca, cb, co := connect(t, ts, a.ID), connect(t, ts, b.ID), connect(t, ts, orch.ID)

	if got := toolNames(t, ca); len(got) != 3 {
		t.Fatalf("subagent tools = %v, want only read_notes, post_note, update_note", got)
	}
	if got := toolNames(t, co); len(got) != 6 {
		t.Fatalf("orchestrator tools = %v, want 6", got)
	}
	if out, isErr := call(t, ca, "escalate_to_user", map[string]any{"question": "q", "context": "c"}); !isErr {
		t.Fatalf("subagent escalate_to_user allowed: %s", out)
	}

	out, isErr := call(t, ca, "post_note", map[string]any{"scope": "session", "type": "heads_up", "content": "renamed Foo"})
	if isErr {
		t.Fatal(out)
	}
	if out, _ := call(t, cb, "read_notes", map[string]any{"scope": "session"}); !strings.Contains(out, `"author_agent_id":`+itoa(a.ID)) || !strings.Contains(out, "renamed Foo") {
		t.Fatalf("agent B read_notes = %s", out)
	}
	for _, bad := range []map[string]any{
		{"scope": "session", "type": "nope", "content": "x"},
		{"scope": "private", "type": "done", "content": "x"},
	} {
		if _, isErr := call(t, ca, "post_note", bad); !isErr {
			t.Fatalf("post_note(%v) should be a tool error", bad)
		}
	}

	if out, isErr := call(t, cb, "update_note", map[string]any{"id": 1, "content": "hijack"}); !isErr {
		t.Fatalf("non-author content edit allowed: %s", out)
	}
	if out, isErr := call(t, cb, "update_note", map[string]any{"id": 1, "status": "resolved"}); isErr || !strings.Contains(out, `"status":"resolved"`) {
		t.Fatalf("non-author resolve failed: %s", out)
	}

	if out, isErr := call(t, co, "escalate_to_user", map[string]any{"question": "q", "context": "c"}); isErr || !strings.Contains(out, "no answer is available") {
		t.Fatalf("escalate = %s", out)
	}
	var n int
	if err := s.Store.db.QueryRow(`SELECT COUNT(*) FROM escalations WHERE agent_id = ?`, orch.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("escalations rows = %d, err %v", n, err)
	}
	if out, isErr := call(t, co, "get_subagent_status", nil); isErr || !strings.Contains(out, `"has_done_note":false`) {
		t.Fatalf("get_subagent_status = %s", out)
	}
}

func TestMCPUnknownAgent(t *testing.T) {
	_, ts := newTestServer(t)
	for _, id := range []string{"999", "abc"} {
		resp, err := http.Post(ts.URL+"/mcp/"+id, "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("/mcp/%s status = %d, want 404", id, resp.StatusCode)
		}
	}
}
