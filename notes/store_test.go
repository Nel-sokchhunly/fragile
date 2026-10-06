package notes

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreNoteRoundTrip(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	sess, err := s.CreateSession("t")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != SessionWorking || sess.Title != "t" {
		t.Fatalf("unexpected session: %+v", sess)
	}
	board, err := s.SessionBoard(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	orch, err := s.CreateAgent(sess.ID, "orchestrator", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if orch.Role != "orchestrator" || orch.ParentID != 0 || orch.Status != "running" || orch.SessionID != sess.ID {
		t.Fatalf("unexpected agent: %+v", orch)
	}

	posted, err := s.PostNote(sess.ID, board, orch.ID, "decision", "use sqlite")
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListNotes(sess.ID, board, NoteFilter{Type: "decision", Status: "open", AuthorID: orch.ID})
	if err != nil || len(list) != 1 || list[0] != posted {
		t.Fatalf("list = %+v, err %v; want [%+v]", list, err, posted)
	}

	resolved := "resolved"
	upd, err := s.UpdateNote(sess.ID, posted.ID, nil, &resolved)
	if err != nil {
		t.Fatal(err)
	}
	if upd.Status != "resolved" || upd.Content != "use sqlite" || upd.UpdatedAt < posted.UpdatedAt {
		t.Fatalf("unexpected update: %+v", upd)
	}
	if open, _ := s.ListNotes(sess.ID, board, NoteFilter{Status: "open"}); len(open) != 0 {
		t.Fatalf("resolved note still listed as open: %+v", open)
	}

	if _, err := s.PostNote(sess.ID, board, orch.ID, "bogus", "x"); err == nil {
		t.Fatal("invalid note type accepted")
	}
	if _, err := s.GetNote(sess.ID, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetNote missing = %v, want ErrNotFound", err)
	}
}

// A path with URI metacharacters must reach SQLite unchanged.
func TestStoreSpecialCharsInPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a#b?c%d")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "f.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestStoreSessions(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.ListSessions(); err != nil || len(got) != 0 {
		t.Fatalf("fresh store sessions = %+v, err %v", got, err)
	}
	a, _ := s.CreateSession("a")
	b, _ := s.CreateSession("b")
	if err := s.SetSessionStatus(b.ID, SessionNeedsYou); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionStatus(b.ID, "bogus"); err == nil {
		t.Fatal("invalid session status accepted")
	}
	if err := s.SetSessionStatus(999, SessionDone); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetSessionStatus missing = %v, want ErrNotFound", err)
	}
	got, err := s.ListSessions()
	if err != nil || len(got) != 2 || got[0].ID != a.ID || got[1].Status != SessionNeedsYou {
		t.Fatalf("sessions = %+v, err %v", got, err)
	}
	ba, _ := s.SessionBoard(a.ID)
	bb, _ := s.SessionBoard(b.ID)
	if ba == 0 || ba == bb {
		t.Fatalf("boards %d, %d: want one distinct board per session", ba, bb)
	}
	if _, err := s.SessionBoard(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SessionBoard missing = %v, want ErrNotFound", err)
	}
}

func TestStoreAgentEvents(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.CreateSession("a")
	b, _ := s.CreateSession("b")
	ag, _ := s.CreateAgent(a.ID, "orchestrator", 0, 0)
	for i := 0; i < 5; i++ {
		if _, err := s.AppendAgentEvent(a.ID, ag.ID, "output", itoa(int64(i))); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListAgentEvents(a.ID, ag.ID, 0, 2)
	if err != nil || len(page) != 2 || page[0].Payload != "0" || page[0].Type != "output" {
		t.Fatalf("page 1 = %+v, err %v", page, err)
	}
	rest, _ := s.ListAgentEvents(a.ID, ag.ID, page[1].ID, 0)
	if len(rest) != 3 || rest[2].Payload != "4" {
		t.Fatalf("rest = %+v", rest)
	}
	// Another session can neither append to nor read this agent's events.
	if _, err := s.AppendAgentEvent(b.ID, ag.ID, "output", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-session append = %v, want ErrNotFound", err)
	}
	if got, _ := s.ListAgentEvents(b.ID, ag.ID, 0, 0); len(got) != 0 {
		t.Fatalf("cross-session events = %+v", got)
	}
}

// The Phase 0 schema, verbatim, as an existing user's database has it.
const phase0Schema = `-- Phase 0 schema (spec 5.3). Timestamps are ISO-8601 UTC text.
-- board_members and agent_events are Phase 1; not created here.

CREATE TABLE IF NOT EXISTS sessions (
	id         INTEGER PRIMARY KEY,
	title      TEXT NOT NULL DEFAULT '',
	status     TEXT NOT NULL DEFAULT 'active',
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS agent_instances (
	id         INTEGER PRIMARY KEY,
	session_id INTEGER NOT NULL REFERENCES sessions(id),
	parent_id  INTEGER REFERENCES agent_instances(id), -- future nesting; unused in P0
	role       TEXT NOT NULL CHECK (role IN ('orchestrator','subagent')),
	token      TEXT UNIQUE, -- secret in the agent's MCP URL; identifies the agent
	task_id    INTEGER REFERENCES tasks(id),
	status     TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running','exited','crashed')),
	pid        INTEGER,
	log_path   TEXT,
	exit_code  INTEGER,
	exited_at  TEXT,
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS tasks (
	id          INTEGER PRIMARY KEY,
	session_id  INTEGER NOT NULL REFERENCES sessions(id),
	title       TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	status      TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','working','blocked','review','done')),
	agent_id    INTEGER REFERENCES agent_instances(id)
);

CREATE TABLE IF NOT EXISTS boards (
	id         INTEGER PRIMARY KEY,
	session_id INTEGER NOT NULL REFERENCES sessions(id),
	scope_type TEXT NOT NULL CHECK (scope_type IN ('session','shared','private')),
	owner_id   INTEGER REFERENCES agent_instances(id)
);

CREATE TABLE IF NOT EXISTS notes (
	id              INTEGER PRIMARY KEY,
	board_id        INTEGER NOT NULL REFERENCES boards(id),
	author_agent_id INTEGER NOT NULL REFERENCES agent_instances(id),
	type            TEXT NOT NULL CHECK (type IN ('decision','blocker','heads_up','done','question')),
	content         TEXT NOT NULL,
	status          TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
	created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS notes_board ON notes(board_id, id);

CREATE TABLE IF NOT EXISTS escalations (
	id         INTEGER PRIMARY KEY,
	session_id INTEGER NOT NULL REFERENCES sessions(id),
	agent_id   INTEGER NOT NULL REFERENCES agent_instances(id),
	question   TEXT NOT NULL,
	context    TEXT NOT NULL DEFAULT '',
	status     TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','answered')),
	answer     TEXT
);
`

func TestMigratePhase0Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p0.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{phase0Schema,
		`INSERT INTO sessions (id, title) VALUES (1, 'old')`, // status defaults to 'active'
		`INSERT INTO boards (id, session_id, scope_type) VALUES (1, 1, 'session')`,
		`INSERT INTO agent_instances (id, session_id, role, token) VALUES (1, 1, 'orchestrator', 'tok')`,
		`INSERT INTO notes (board_id, author_agent_id, type, content) VALUES (1, 1, 'decision', 'keep me')`,
		`INSERT INTO escalations (session_id, agent_id, question) VALUES (1, 1, 'q')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var v int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != 3 {
		t.Fatalf("user_version = %d, want 3", v)
	}
	sess, err := s.GetSession(1)
	if err != nil || sess.Title != "old" || sess.Status != SessionDone {
		t.Fatalf("migrated session = %+v, err %v", sess, err)
	}
	board, _ := s.SessionBoard(1)
	notes, err := s.ListNotes(1, board, NoteFilter{})
	if err != nil || len(notes) != 1 || notes[0].Content != "keep me" || notes[0].AuthorID != 1 {
		t.Fatalf("migrated notes = %+v, err %v", notes, err)
	}
	if a, err := s.GetAgentByToken("tok"); err != nil || a.ID != 1 || a.SessionID != 1 {
		t.Fatalf("migrated agent = %+v, err %v", a, err)
	}
	for _, table := range []string{"board_members", "agent_events", "escalations"} {
		if _, err := s.db.Exec(`SELECT 1 FROM ` + table + ` LIMIT 0`); err != nil {
			t.Errorf("table %s: %v", table, err)
		}
	}
	// New sessions work next to the migrated one, and cannot see its data.
	n, err := s.CreateSession("new")
	if err != nil || n.Status != SessionWorking {
		t.Fatalf("new session = %+v, err %v", n, err)
	}
	nb, _ := s.SessionBoard(n.ID)
	if got, _ := s.ListNotes(n.ID, nb, NoteFilter{}); len(got) != 0 {
		t.Fatalf("new session sees old notes: %+v", got)
	}
	if _, err := s.db.Exec(`UPDATE sessions SET status = 'active' WHERE id = 1`); err == nil {
		t.Fatal("migrated sessions table lacks the status CHECK")
	}
	// Reopening an up-to-date database is a no-op.
	s.Close()
	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s2.Close()
}

func TestOpenStoreRefusesUnknownSchemas(t *testing.T) {
	dir := t.TempDir()
	for name, setup := range map[string]string{
		"pre-token": `CREATE TABLE agent_instances (id INTEGER PRIMARY KEY, session_id INTEGER)`,
		"newer":     `PRAGMA user_version = 99`,
	} {
		path := filepath.Join(dir, name+".db")
		db, _ := sql.Open("sqlite", path)
		if _, err := db.Exec(setup); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if s, err := OpenStore(path); err == nil {
			s.Close()
			t.Errorf("%s database opened, want refusal", name)
		}
	}
}

// Migration 003: notes may be authored by the user (NULL author, 0 in Go), without losing the FK for agents.
func TestUserNotesAndMigration003(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "u.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sess, _ := s.CreateSessionIn("t", "/some/dir")
	other, _ := s.CreateSession("other")
	board, _ := s.SessionBoard(sess.ID)
	agent, _ := s.CreateAgent(sess.ID, "orchestrator", 0, 0)
	foreign, _ := s.CreateAgent(other.ID, "orchestrator", 0, 0)

	un, err := s.PostNote(sess.ID, board, 0, "decision", "from the user")
	if err != nil || un.AuthorID != 0 {
		t.Fatalf("user note = %+v, err %v", un, err)
	}
	var isNull bool
	if err := s.db.QueryRow(`SELECT author_agent_id IS NULL FROM notes WHERE id = ?`, un.ID).Scan(&isNull); err != nil || !isNull {
		t.Fatalf("author stored as NULL = %v, err %v", isNull, err)
	}
	an, err := s.PostNote(sess.ID, board, agent.ID, "done", "from an agent")
	if err != nil || an.AuthorID != agent.ID {
		t.Fatalf("agent note = %+v, err %v", an, err)
	}
	// The FK still holds for agents: unknown or foreign-session authors are refused.
	for _, bad := range []int64{9999, foreign.ID} {
		if _, err := s.PostNote(sess.ID, board, bad, "done", "x"); err != ErrNotFound {
			t.Errorf("author %d: err = %v, want ErrNotFound", bad, err)
		}
	}
	if _, err := s.db.Exec(`UPDATE notes SET author_agent_id = 9999 WHERE id = ?`, an.ID); err == nil {
		t.Error("notes lost the author foreign key")
	}
	all, _ := s.ListNotes(sess.ID, board, NoteFilter{})
	byAgent, _ := s.ListNotes(sess.ID, board, NoteFilter{AuthorID: agent.ID})
	if len(all) != 2 || len(byAgent) != 1 || byAgent[0].ID != an.ID {
		t.Fatalf("all = %+v byAgent = %+v", all, byAgent)
	}
	if got, _ := s.GetSession(sess.ID); got.WorkDir != "/some/dir" {
		t.Fatalf("work dir = %q", got.WorkDir)
	}
}

func TestMarkRunningAgentsCrashed(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sess, _ := s.CreateSession("t")
	orch, _ := s.CreateAgent(sess.ID, "orchestrator", 0, 0)
	task, _ := s.CreateTask(sess.ID, "t", "")
	sub, _ := s.CreateAgent(sess.ID, "subagent", orch.ID, task.ID)
	s.SetTaskAgent(sess.ID, task.ID, sub.ID)
	zero := 0
	s.SetAgentStatus(sess.ID, orch.ID, "exited", &zero)

	got, err := s.MarkRunningAgentsCrashed()
	if err != nil || len(got) != 1 || got[0].ID != sub.ID || got[0].Status != "crashed" || got[0].ExitedAt == "" {
		t.Fatalf("marked = %+v, err %v", got, err)
	}
	if a, _ := s.GetAgent(sess.ID, orch.ID); a.Status != "exited" {
		t.Errorf("exited agent touched: %+v", a)
	}
	if tk, _ := s.GetTask(sess.ID, task.ID); tk.Status != "blocked" {
		t.Errorf("task = %+v, want blocked", tk)
	}
	if again, _ := s.MarkRunningAgentsCrashed(); len(again) != 0 {
		t.Errorf("second run marked %+v", again)
	}
}
