package notes

import (
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"
)

//go:embed schema.sql
var schema string

// ErrNotFound is returned when a row does not exist (or is outside the session).
var ErrNotFound = errors.New("not found")

// Store is the SQLite-backed data layer. Phase 0 runs one implicit session per
// process start; every query is scoped to SessionID.
type Store struct {
	db        *sql.DB
	SessionID int64
	BoardID   int64 // the session-scope board
}

// OpenStore opens (creating if needed) the database at path, applies the
// schema, and starts a new session with its session-scope board.
func OpenStore(path string) (*Store, error) {
	dsn := url.URL{Scheme: "file", OmitHost: true, Path: path,
		RawQuery: "_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	// ponytail: one connection serializes all access; raise it if write contention shows up.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	// No migrations in Phase 0: a DB from an older schema is refused, not upgraded.
	if _, err := db.Exec(`SELECT token FROM agent_instances LIMIT 0`); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s was created by an older fragile schema; delete it and rerun: %w", path, err)
	}
	if err := db.QueryRow(`INSERT INTO sessions DEFAULT VALUES RETURNING id`).Scan(&s.SessionID); err != nil {
		db.Close()
		return nil, err
	}
	if err := db.QueryRow(`INSERT INTO boards (session_id, scope_type) VALUES (?, 'session') RETURNING id`,
		s.SessionID).Scan(&s.BoardID); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
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

// CreateAgent registers an agent instance. parentID and taskID 0 mean none.
// Each agent gets a random token that authenticates its MCP URL.
func (s *Store) CreateAgent(role string, parentID, taskID int64) (Agent, error) {
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return Agent{}, err
	}
	return scanAgent(s.db.QueryRow(`INSERT INTO agent_instances (session_id, parent_id, role, token, task_id)
		VALUES (?, ?, ?, ?, ?) RETURNING `+agentCols, s.SessionID, nullable(parentID), role, hex.EncodeToString(tok), nullable(taskID)))
}

func (s *Store) GetAgentByToken(token string) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agent_instances WHERE token = ? AND session_id = ?`,
		token, s.SessionID))
}

func (s *Store) GetAgent(id int64) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agent_instances WHERE id = ? AND session_id = ?`,
		id, s.SessionID))
}

// ListAgents returns this session's agents, optionally only those with role.
func (s *Store) ListAgents(role string) ([]Agent, error) {
	rows, err := s.db.Query(`SELECT `+agentCols+` FROM agent_instances
		WHERE session_id = ? AND (? = '' OR role = ?) ORDER BY id`, s.SessionID, role, role)
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

func (s *Store) SetAgentProcess(id int64, pid int, logPath string) error {
	return affected(s.db.Exec(`UPDATE agent_instances SET pid = ?, log_path = ? WHERE id = ? AND session_id = ?`,
		pid, logPath, id, s.SessionID))
}

// SetAgentStatus updates status; exited_at is stamped for any status but running.
func (s *Store) SetAgentStatus(id int64, status string, exitCode *int) error {
	return affected(s.db.Exec(`UPDATE agent_instances SET status = ?1, exit_code = ?2,
		exited_at = CASE WHEN ?1 = 'running' THEN NULL ELSE `+now+` END
		WHERE id = ?3 AND session_id = ?4`, status, exitCode, id, s.SessionID))
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
func (s *Store) CreateTask(title, description string) (Task, error) {
	return scanTask(s.db.QueryRow(`INSERT INTO tasks (session_id, title, description, status)
		VALUES (?, ?, ?, 'working') RETURNING `+taskCols, s.SessionID, title, description))
}

func (s *Store) GetTask(id int64) (Task, error) {
	return scanTask(s.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE id = ? AND session_id = ?`, id, s.SessionID))
}

func (s *Store) SetTaskAgent(taskID, agentID int64) error {
	return affected(s.db.Exec(`UPDATE tasks SET agent_id = ? WHERE id = ? AND session_id = ?`,
		nullable(agentID), taskID, s.SessionID))
}

func (s *Store) SetTaskStatus(taskID int64, status string) error {
	return affected(s.db.Exec(`UPDATE tasks SET status = ? WHERE id = ? AND session_id = ?`,
		status, taskID, s.SessionID))
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

// inSession restricts a notes query to boards of the current session.
const inSession = `board_id IN (SELECT id FROM boards WHERE session_id = ?)`

func scanNote(r scanner) (n Note, err error) {
	err = r.Scan(&n.ID, &n.BoardID, &n.AuthorID, &n.Type, &n.Content, &n.Status, &n.CreatedAt, &n.UpdatedAt)
	return n, one(err)
}

func (s *Store) PostNote(boardID, authorID int64, typ, content string) (Note, error) {
	return scanNote(s.db.QueryRow(`INSERT INTO notes (board_id, author_agent_id, type, content)
		SELECT id, ?, ?, ? FROM boards WHERE id = ? AND session_id = ? RETURNING `+noteCols,
		authorID, typ, content, boardID, s.SessionID))
}

func (s *Store) GetNote(id int64) (Note, error) {
	return scanNote(s.db.QueryRow(`SELECT `+noteCols+` FROM notes WHERE id = ? AND `+inSession, id, s.SessionID))
}

func (s *Store) ListNotes(boardID int64, f NoteFilter) ([]Note, error) {
	rows, err := s.db.Query(`SELECT `+noteCols+` FROM notes
		WHERE board_id = ? AND `+inSession+`
		AND (? = '' OR type = ?) AND (? = '' OR status = ?)
		AND (? = 0 OR author_agent_id = ?) AND id > ? ORDER BY id`,
		boardID, s.SessionID, f.Type, f.Type, f.Status, f.Status, f.AuthorID, f.AuthorID, f.SinceID)
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
func (s *Store) UpdateNote(id int64, content, status *string) (Note, error) {
	return scanNote(s.db.QueryRow(`UPDATE notes SET content = COALESCE(?, content),
		status = COALESCE(?, status), updated_at = `+now+`
		WHERE id = ? AND `+inSession+` RETURNING `+noteCols, content, status, id, s.SessionID))
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

func (s *Store) CreateEscalation(agentID int64, question, context string) (Escalation, error) {
	var e Escalation
	err := s.db.QueryRow(`INSERT INTO escalations (session_id, agent_id, question, context)
		VALUES (?, ?, ?, ?) RETURNING id, session_id, agent_id, question, context, status, COALESCE(answer,'')`,
		s.SessionID, agentID, question, context).
		Scan(&e.ID, &e.SessionID, &e.AgentID, &e.Question, &e.Context, &e.Status, &e.Answer)
	return e, err
}
