package notes

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"

	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// ErrNotFound is returned when a row does not exist (or is outside the session).
var ErrNotFound = errors.New("not found")

// Store is the SQLite-backed data layer. It holds many sessions; every method
// that touches session data takes the sessionID and cannot see another
// session's agents, tasks, boards or notes.
type Store struct {
	db *sql.DB
}

// OpenStore opens (creating if needed) the database at path and migrates it to
// the current schema. It creates no session; use CreateSession.
func OpenStore(path string) (*Store, error) {
	dsn := url.URL{Scheme: "file", OmitHost: true, Path: path,
		RawQuery: "_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	// ponytail: one connection serializes all access; raise it if write contention shows up.
	db.SetMaxOpenConns(1)
	if err := migrate(db, path); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migrate applies migrations/NNN_*.sql in order, each in its own transaction,
// recording the number reached in PRAGMA user_version. A Phase 0 database has
// user_version 0 with its tables in place; migration 1 (the Phase 0 schema) is
// all IF NOT EXISTS, so it is a no-op there and later migrations upgrade it.
func migrate(db *sql.DB, path string) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx) // pragmas and transactions must share one connection
	if err != nil {
		return err
	}
	defer conn.Close()
	files, err := fs.Glob(migrationFS, "migrations/*.sql") // sorted by name
	if err != nil {
		return err
	}
	var v int
	if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v > len(files) {
		return fmt.Errorf("%s was created by a newer fragile (schema version %d, this build knows %d)", path, v, len(files))
	}
	if v == 0 { // pre-token Phase 0 databases cannot be upgraded: refuse rather than guess
		var tables, hasToken int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(name = 'token'), 0) FROM pragma_table_info('agent_instances')`).
			Scan(&tables, &hasToken); err != nil {
			return err
		}
		if tables > 0 && hasToken == 0 {
			return fmt.Errorf("%s was created by an older fragile schema; delete it and rerun", path)
		}
	}
	// Foreign keys stay off while a migration rebuilds tables, then are checked.
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
	for i := v; i < len(files); i++ {
		if err := applyMigration(ctx, conn, files[i], i+1); err != nil {
			return fmt.Errorf("migrate %s: %w", files[i], err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, conn *sql.Conn, file string, version int) error {
	sqlText, err := migrationFS.ReadFile(file)
	if err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, string(sqlText)); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	violated := rows.Next()
	rows.Close()
	if violated {
		return errors.New("foreign key check failed")
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface{ Scan(...any) error }

// one maps sql.ErrNoRows to ErrNotFound.
func one(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// affected returns ErrNotFound if an UPDATE matched no row.
func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const now = `strftime('%Y-%m-%dT%H:%M:%fZ','now')`

// Sessions

// Session statuses (the sidebar badge).
const (
	SessionWorking  = "working"
	SessionDone     = "done"
	SessionNeedsYou = "needs_you"
)

type Session struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

const sessionCols = `id, title, status, created_at`

func scanSession(r scanner) (se Session, err error) {
	err = r.Scan(&se.ID, &se.Title, &se.Status, &se.CreatedAt)
	return se, one(err)
}

// CreateSession starts a session in the working state together with its
// session-scope board.
func (s *Store) CreateSession(title string) (Session, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	se, err := scanSession(tx.QueryRow(`INSERT INTO sessions (title) VALUES (?) RETURNING `+sessionCols, title))
	if err != nil {
		return Session{}, err
	}
	if _, err := tx.Exec(`INSERT INTO boards (session_id, scope_type) VALUES (?, 'session')`, se.ID); err != nil {
		return Session{}, err
	}
	return se, tx.Commit()
}

func (s *Store) GetSession(id int64) (Session, error) {
	return scanSession(s.db.QueryRow(`SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id))
}

// ListSessions returns all sessions, oldest first.
func (s *Store) ListSessions() ([]Session, error) {
	rows, err := s.db.Query(`SELECT ` + sessionCols + ` FROM sessions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		se, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, se)
	}
	return out, rows.Err()
}

func (s *Store) SetSessionStatus(id int64, status string) error {
	return affected(s.db.Exec(`UPDATE sessions SET status = ? WHERE id = ?`, status, id))
}

// SessionBoard returns the id of the session's session-scope board.
func (s *Store) SessionBoard(sessionID int64) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM boards WHERE session_id = ? AND scope_type = 'session'`, sessionID).Scan(&id)
	return id, one(err)
}

// Agents

type Agent struct {
	ID        int64  `json:"id"`
	SessionID int64  `json:"session_id"`
	ParentID  int64  `json:"parent_id,omitempty"`
	Role      string `json:"role"`
	Token     string `json:"-"` // secret; never serialized
	TaskID    int64  `json:"task_id,omitempty"`
	Status    string `json:"status"`
	PID       int    `json:"pid,omitempty"`
	LogPath   string `json:"log_path,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	CreatedAt string `json:"created_at"`
	ExitedAt  string `json:"exited_at,omitempty"`
}

const agentCols = `id, session_id, COALESCE(parent_id,0), role, token, COALESCE(task_id,0), status,
	COALESCE(pid,0), COALESCE(log_path,''), exit_code, created_at, COALESCE(exited_at,'')`

func scanAgent(r scanner) (a Agent, err error) {
	err = r.Scan(&a.ID, &a.SessionID, &a.ParentID, &a.Role, &a.Token, &a.TaskID, &a.Status,
		&a.PID, &a.LogPath, &a.ExitCode, &a.CreatedAt, &a.ExitedAt)
	return a, one(err)
}

// nullable turns the zero id into NULL.
func nullable(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// CreateAgent registers an agent instance in the session. parentID and taskID
// 0 mean none; otherwise they must belong to the same session (else ErrNotFound).
// Each agent gets a random token that authenticates its MCP URL.
func (s *Store) CreateAgent(sessionID int64, role string, parentID, taskID int64) (Agent, error) {
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return Agent{}, err
	}
	return scanAgent(s.db.QueryRow(`INSERT INTO agent_instances (session_id, parent_id, role, token, task_id)
		SELECT id, ?2, ?3, ?4, ?5 FROM sessions WHERE id = ?1
		AND (?2 IS NULL OR EXISTS (SELECT 1 FROM agent_instances WHERE id = ?2 AND session_id = ?1))
		AND (?5 IS NULL OR EXISTS (SELECT 1 FROM tasks WHERE id = ?5 AND session_id = ?1))
		RETURNING `+agentCols, sessionID, nullable(parentID), role, hex.EncodeToString(tok), nullable(taskID)))
}

// GetAgentByToken resolves an agent from its secret; the agent carries its SessionID.
func (s *Store) GetAgentByToken(token string) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agent_instances WHERE token = ?`, token))
}

func (s *Store) GetAgent(sessionID, id int64) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agent_instances WHERE id = ? AND session_id = ?`,
		id, sessionID))
}

// ListAgents returns the session's agents, optionally only those with role.
func (s *Store) ListAgents(sessionID int64, role string) ([]Agent, error) {
	rows, err := s.db.Query(`SELECT `+agentCols+` FROM agent_instances
		WHERE session_id = ? AND (? = '' OR role = ?) ORDER BY id`, sessionID, role, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) SetAgentProcess(sessionID, id int64, pid int, logPath string) error {
	return affected(s.db.Exec(`UPDATE agent_instances SET pid = ?, log_path = ? WHERE id = ? AND session_id = ?`,
		pid, logPath, id, sessionID))
}

// SetAgentStatus updates status; exited_at is stamped for any status but running.
func (s *Store) SetAgentStatus(sessionID, id int64, status string, exitCode *int) error {
	return affected(s.db.Exec(`UPDATE agent_instances SET status = ?1, exit_code = ?2,
		exited_at = CASE WHEN ?1 = 'running' THEN NULL ELSE `+now+` END
		WHERE id = ?3 AND session_id = ?4`, status, exitCode, id, sessionID))
}

// Tasks

type Task struct {
	ID          int64  `json:"id"`
	SessionID   int64  `json:"session_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	AgentID     int64  `json:"agent_id,omitempty"`
}

const taskCols = `id, session_id, title, description, status, COALESCE(agent_id,0)`

func scanTask(r scanner) (t Task, err error) {
	err = r.Scan(&t.ID, &t.SessionID, &t.Title, &t.Description, &t.Status, &t.AgentID)
	return t, one(err)
}

// CreateTask adds a task in the working state (spawn_subagent creates it as it starts the agent).
func (s *Store) CreateTask(sessionID int64, title, description string) (Task, error) {
	return scanTask(s.db.QueryRow(`INSERT INTO tasks (session_id, title, description, status)
		VALUES (?, ?, ?, 'working') RETURNING `+taskCols, sessionID, title, description))
}

func (s *Store) GetTask(sessionID, id int64) (Task, error) {
	return scanTask(s.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE id = ? AND session_id = ?`, id, sessionID))
}

func (s *Store) SetTaskAgent(sessionID, taskID, agentID int64) error {
	return affected(s.db.Exec(`UPDATE tasks SET agent_id = ?1 WHERE id = ?2 AND session_id = ?3
		AND (?1 IS NULL OR EXISTS (SELECT 1 FROM agent_instances WHERE id = ?1 AND session_id = ?3))`,
		nullable(agentID), taskID, sessionID))
}

func (s *Store) SetTaskStatus(sessionID, taskID int64, status string) error {
	return affected(s.db.Exec(`UPDATE tasks SET status = ? WHERE id = ? AND session_id = ?`,
		status, taskID, sessionID))
}

// Notes

type Note struct {
	ID        int64  `json:"id"`
	BoardID   int64  `json:"board_id"`
	AuthorID  int64  `json:"author_agent_id"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// NoteFilter narrows ListNotes; zero fields match everything.
type NoteFilter struct {
	Type, Status string
	AuthorID     int64
	SinceID      int64 // only notes with id > SinceID
}

const noteCols = `id, board_id, author_agent_id, type, content, status, created_at, updated_at`

// inSession restricts a notes query to boards of the given session (one ? arg).
const inSession = `board_id IN (SELECT id FROM boards WHERE session_id = ?)`

func scanNote(r scanner) (n Note, err error) {
	err = r.Scan(&n.ID, &n.BoardID, &n.AuthorID, &n.Type, &n.Content, &n.Status, &n.CreatedAt, &n.UpdatedAt)
	return n, one(err)
}

// PostNote adds a note to a board of the session; the author must be an agent of
// the same session (else ErrNotFound).
func (s *Store) PostNote(sessionID, boardID, authorID int64, typ, content string) (Note, error) {
	return scanNote(s.db.QueryRow(`INSERT INTO notes (board_id, author_agent_id, type, content)
		SELECT b.id, a.id, ?1, ?2 FROM boards b, agent_instances a
		WHERE b.id = ?3 AND b.session_id = ?4 AND a.id = ?5 AND a.session_id = ?4 RETURNING `+noteCols,
		typ, content, boardID, sessionID, authorID))
}

func (s *Store) GetNote(sessionID, id int64) (Note, error) {
	return scanNote(s.db.QueryRow(`SELECT `+noteCols+` FROM notes WHERE id = ? AND `+inSession, id, sessionID))
}

func (s *Store) ListNotes(sessionID, boardID int64, f NoteFilter) ([]Note, error) {
	rows, err := s.db.Query(`SELECT `+noteCols+` FROM notes
		WHERE board_id = ? AND `+inSession+`
		AND (? = '' OR type = ?) AND (? = '' OR status = ?)
		AND (? = 0 OR author_agent_id = ?) AND id > ? ORDER BY id`,
		boardID, sessionID, f.Type, f.Type, f.Status, f.Status, f.AuthorID, f.AuthorID, f.SinceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UpdateNote changes content and/or status (nil = leave as is) and bumps updated_at.
func (s *Store) UpdateNote(sessionID, id int64, content, status *string) (Note, error) {
	return scanNote(s.db.QueryRow(`UPDATE notes SET content = COALESCE(?, content),
		status = COALESCE(?, status), updated_at = `+now+`
		WHERE id = ? AND `+inSession+` RETURNING `+noteCols, content, status, id, sessionID))
}

// Escalations

type Escalation struct {
	ID        int64  `json:"id"`
	SessionID int64  `json:"session_id"`
	AgentID   int64  `json:"agent_id"`
	Question  string `json:"question"`
	Context   string `json:"context"`
	Status    string `json:"status"`
	Answer    string `json:"answer,omitempty"`
}

// CreateEscalation records an escalation by an agent of the session (else ErrNotFound).
func (s *Store) CreateEscalation(sessionID, agentID int64, question, context string) (Escalation, error) {
	var e Escalation
	err := s.db.QueryRow(`INSERT INTO escalations (session_id, agent_id, question, context)
		SELECT session_id, id, ?, ? FROM agent_instances WHERE id = ? AND session_id = ?
		RETURNING id, session_id, agent_id, question, context, status, COALESCE(answer,'')`,
		question, context, agentID, sessionID).
		Scan(&e.ID, &e.SessionID, &e.AgentID, &e.Question, &e.Context, &e.Status, &e.Answer)
	return e, one(err)
}

// Agent events: per-agent history for the UI (output lines, status changes).

type AgentEvent struct {
	ID        int64  `json:"id"`
	AgentID   int64  `json:"agent_id"`
	Type      string `json:"event_type"`
	Payload   string `json:"payload"` // caller-defined, typically JSON text
	CreatedAt string `json:"created_at"`
}

const agentEventCols = `id, agent_id, event_type, payload, created_at`

func scanAgentEvent(r scanner) (e AgentEvent, err error) {
	err = r.Scan(&e.ID, &e.AgentID, &e.Type, &e.Payload, &e.CreatedAt)
	return e, one(err)
}

// AppendAgentEvent adds an event to an agent of the session (else ErrNotFound).
func (s *Store) AppendAgentEvent(sessionID, agentID int64, eventType, payload string) (AgentEvent, error) {
	return scanAgentEvent(s.db.QueryRow(`INSERT INTO agent_events (agent_id, event_type, payload)
		SELECT id, ?, ? FROM agent_instances WHERE id = ? AND session_id = ? RETURNING `+agentEventCols,
		eventType, payload, agentID, sessionID))
}

// ListAgentEvents returns the agent's events with id > sinceID, oldest first,
// at most limit of them (limit <= 0: no limit). Page by passing the last id seen.
func (s *Store) ListAgentEvents(sessionID, agentID, sinceID int64, limit int) ([]AgentEvent, error) {
	if limit <= 0 {
		limit = -1 // SQLite: no limit
	}
	rows, err := s.db.Query(`SELECT `+agentEventCols+` FROM agent_events
		WHERE agent_id = ? AND id > ? AND agent_id IN (SELECT id FROM agent_instances WHERE session_id = ?)
		ORDER BY id LIMIT ?`, agentID, sinceID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentEvent
	for rows.Next() {
		e, err := scanAgentEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
