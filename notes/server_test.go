package notes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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

func connect(t *testing.T, ts *httptest.Server, token string) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp/" + token}, nil)
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
	sess, _ := s.Store.CreateSession("test")
	orch, _ := s.Store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	a, _ := s.Store.CreateAgent(sess.ID, "subagent", orch.ID, 0)
	b, _ := s.Store.CreateAgent(sess.ID, "subagent", orch.ID, 0)
	ca, cb, co := connect(t, ts, a.Token), connect(t, ts, b.Token), connect(t, ts, orch.Token)

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
	s, ts := newTestServer(t)
	sess, _ := s.Store.CreateSession("test")
	orch, _ := s.Store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	if len(orch.Token) != 32 {
		t.Fatalf("token = %q, want 32 hex chars", orch.Token)
	}
	// The integer id is no longer an identity, even for a real agent.
	for _, id := range []string{itoa(orch.ID), "999", "abc"} {
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

func TestMCPSpawnSubagent(t *testing.T) {
	s, ts := newTestServer(t)
	sess, _ := s.Store.CreateSession("test")
	dir := t.TempDir()
	s.Runner = NewRunner(Config{Addr: strings.TrimPrefix(ts.URL, "http://"), AgentDir: dir, WorkDir: dir}, s.Store, s.Log)
	s.Runner.Command = writeFake(t, dir, `exit 0`)
	orch, _ := s.Store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	co := connect(t, ts, orch.Token)

	for _, bad := range []string{"", "  \n\t"} {
		if _, isErr := call(t, co, "spawn_subagent", map[string]any{"task": bad}); !isErr {
			t.Fatalf("spawn_subagent(%q) should be a tool error", bad)
		}
	}
	out, isErr := call(t, co, "spawn_subagent", map[string]any{"task": "write the thing"})
	if isErr {
		t.Fatal(out)
	}
	s.Runner.Wait(sess.ID)

	subs, _ := s.Store.ListAgents(sess.ID, "subagent")
	if len(subs) != 1 || subs[0].ParentID != orch.ID || subs[0].TaskID == 0 {
		t.Fatalf("subagents = %+v", subs)
	}
	if task, err := s.Store.GetTask(sess.ID, subs[0].TaskID); err != nil || task.Title != "write the thing" || task.AgentID != subs[0].ID {
		t.Fatalf("task = %+v, err %v", task, err)
	}
	path := filepath.Join(dir, "agent-"+itoa(subs[0].ID)+".mcp.json")
	if cfg, _ := os.ReadFile(path); !strings.Contains(string(cfg), "/mcp/"+subs[0].Token+`"`) || subs[0].Token == orch.Token {
		t.Fatalf("mcp config = %q", cfg)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mcp config mode = %v, want 0600", fi.Mode().Perm())
	}
}

// Two sessions share one store and server; nothing of one is visible to the other.
func TestSessionIsolation(t *testing.T) {
	s, ts := newTestServer(t)
	var events []Event
	s.Log.OnEvent = func(e Event) { events = append(events, e) }

	type side struct {
		sess       Session
		orch, sub  Agent
		co, cs     *mcp.ClientSession
		secretNote string
	}
	mk := func(title string) *side {
		sess, err := s.Store.CreateSession(title)
		if err != nil {
			t.Fatal(err)
		}
		orch, _ := s.Store.CreateAgent(sess.ID, "orchestrator", 0, 0)
		task, _ := s.Store.CreateTask(sess.ID, title+" task", "")
		sub, err := s.Store.CreateAgent(sess.ID, "subagent", orch.ID, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		s.Store.SetTaskAgent(sess.ID, task.ID, sub.ID)
		return &side{sess: sess, orch: orch, sub: sub, co: connect(t, ts, orch.Token), cs: connect(t, ts, sub.Token), secretNote: "secret-of-" + title}
	}
	a, b := mk("A"), mk("B")
	for _, x := range []*side{a, b} {
		if out, isErr := call(t, x.cs, "post_note", map[string]any{"scope": "session", "type": "decision", "content": x.secretNote}); isErr {
			t.Fatal(out)
		}
	}

	// Tools: each caller sees only its own session.
	for _, x := range []struct{ me, other *side }{{a, b}, {b, a}} {
		for _, c := range []*mcp.ClientSession{x.me.co, x.me.cs} {
			out, _ := call(t, c, "read_notes", map[string]any{"scope": "session"})
			if !strings.Contains(out, x.me.secretNote) || strings.Contains(out, x.other.secretNote) {
				t.Fatalf("read_notes in %s = %s", x.me.sess.Title, out)
			}
		}
		out, isErr := call(t, x.me.co, "get_subagent_status", nil)
		if isErr || !strings.Contains(out, `"agent_id":`+itoa(x.me.sub.ID)) || strings.Contains(out, `"agent_id":`+itoa(x.other.sub.ID)) {
			t.Fatalf("get_subagent_status in %s = %s", x.me.sess.Title, out)
		}
		if out, isErr := call(t, x.me.co, "get_subagent_status", map[string]any{"id": x.other.sub.ID}); !isErr {
			t.Fatalf("status of another session's sub-agent allowed: %s", out)
		}
		otherNote := noteIDs(t, s.Store, x.other.sess.ID)[0]
		if out, isErr := call(t, x.me.cs, "update_note", map[string]any{"id": otherNote, "status": "resolved"}); !isErr {
			t.Fatalf("update of another session's note allowed: %s", out)
		}
	}

	// Store: every query is scoped to its session.
	for _, x := range []struct{ me, other *side }{{a, b}, {b, a}} {
		board, _ := s.Store.SessionBoard(x.me.sess.ID)
		otherBoard, _ := s.Store.SessionBoard(x.other.sess.ID)
		if got, _ := s.Store.ListNotes(x.me.sess.ID, board, NoteFilter{}); len(got) != 1 || got[0].Content != x.me.secretNote {
			t.Fatalf("ListNotes own = %+v", got)
		}
		if got, _ := s.Store.ListNotes(x.me.sess.ID, otherBoard, NoteFilter{}); len(got) != 0 {
			t.Fatalf("ListNotes other board = %+v", got)
		}
		if _, err := s.Store.PostNote(x.me.sess.ID, otherBoard, x.me.sub.ID, "done", "x"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("post to other board = %v, want ErrNotFound", err)
		}
		if _, err := s.Store.PostNote(x.me.sess.ID, board, x.other.sub.ID, "done", "x"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("post as other agent = %v, want ErrNotFound", err)
		}
		if _, err := s.Store.GetAgent(x.me.sess.ID, x.other.sub.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetAgent other = %v, want ErrNotFound", err)
		}
		if _, err := s.Store.GetTask(x.me.sess.ID, x.other.sub.TaskID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetTask other = %v, want ErrNotFound", err)
		}
		if _, err := s.Store.CreateAgent(x.me.sess.ID, "subagent", x.other.orch.ID, 0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("CreateAgent with other session's parent = %v, want ErrNotFound", err)
		}
		if _, err := s.Store.CreateEscalation(x.me.sess.ID, x.other.orch.ID, "q", ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("CreateEscalation as other agent = %v, want ErrNotFound", err)
		}
		agents, _ := s.Store.ListAgents(x.me.sess.ID, "")
		if len(agents) != 2 || agents[0].SessionID != x.me.sess.ID || agents[1].SessionID != x.me.sess.ID {
			t.Fatalf("ListAgents = %+v", agents)
		}
		if _, err := s.Store.UpdateNote(x.me.sess.ID, noteIDs(t, s.Store, x.other.sess.ID)[0], nil, ptr("resolved")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("UpdateNote other = %v, want ErrNotFound", err)
		}
	}

	// Every observed event names its session.
	if len(events) != 2 {
		t.Fatalf("events = %+v, want 2 note_posted", events)
	}
	for i, x := range []*side{a, b} {
		if events[i].Event != EventNotePosted || events[i].SessionID != x.sess.ID || events[i].AgentID != x.sub.ID {
			t.Fatalf("event %d = %+v", i, events[i])
		}
	}
}

func ptr(s string) *string { return &s }

// noteIDs lists the ids of the notes on a session's board.
func noteIDs(t *testing.T, st *Store, sessionID int64) []int64 {
	t.Helper()
	board, _ := st.SessionBoard(sessionID)
	notes, err := st.ListNotes(sessionID, board, NoteFilter{})
	if err != nil || len(notes) == 0 {
		t.Fatalf("notes of session %d = %+v, err %v", sessionID, notes, err)
	}
	var ids []int64
	for _, n := range notes {
		ids = append(ids, n.ID)
	}
	return ids
}
