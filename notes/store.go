package notes

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
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
	ID               int64    `json:"id"`
	Title            string   `json:"title"`
	Status           string   `json:"status"`
	Provider         string   `json:"provider"` // claude (legacy/default), codex or agy
	WorkDir          string   `json:"work_dir"` // "" = none recorded; the runner's configured WorkDir applies
	CreatedAt        string   `json:"created_at"`
	EnabledProviders []string `json:"enabled_providers,omitempty"`
	Agents           int      `json:"agent_count"` // 0 = new: the orchestrator starts with the first message
}

const sessionCols = `id, title, status, provider, work_dir, created_at, enabled_providers, (SELECT COUNT(*) FROM agent_instances WHERE session_id = sessions.id)`

func scanSession(r scanner) (se Session, err error) {
	var rawEnabled sql.NullString
	err = r.Scan(&se.ID, &se.Title, &se.Status, &se.Provider, &se.WorkDir, &se.CreatedAt, &rawEnabled, &se.Agents)
	if err != nil {
		return se, one(err)
	}
	se.EnabledProviders = []string{}
	if rawEnabled.Valid && rawEnabled.String != "" {
		var ep []string
		if err := json.Unmarshal([]byte(rawEnabled.String), &ep); err == nil && ep != nil {
			se.EnabledProviders = ep
		}
	}
	return se, nil
}

// CreateSession starts a session in the working state together with its
// session-scope board.
func (s *Store) CreateSession(title string) (Session, error) { return s.CreateSessionIn(title, "") }

// CreateSessionIn is CreateSession with the working directory its agents run in.
func (s *Store) CreateSessionIn(title, workDir string) (Session, error) {
	return s.CreateSessionWithProvider(title, workDir, ProviderClaude)
}

func (s *Store) CreateSessionWithProvider(title, workDir, provider string) (Session, error) {
	return s.CreateSessionWithProviders(title, workDir, provider, nil)
}

func (s *Store) CreateSessionWithProviders(title, workDir, provider string, enabledProviders []string) (Session, error) {
	if err := CheckProvider(provider); err != nil {
		return Session{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	var rawProviders any = nil
	if len(enabledProviders) > 0 {
		b, err := json.Marshal(enabledProviders)
		if err != nil {
			return Session{}, err
		}
		rawProviders = string(b)
	}
	se, err := scanSession(tx.QueryRow(`INSERT INTO sessions (title, work_dir, provider, enabled_providers) VALUES (?, ?, ?, ?) RETURNING id, title, status, provider, work_dir, created_at, enabled_providers, 0`, title, workDir, provider, rawProviders))
	if err != nil {
		return Session{}, err
	}
	if _, err := tx.Exec(`INSERT INTO boards (session_id, scope_type) VALUES (?, 'session')`, se.ID); err != nil {
		return Session{}, err
	}
	return se, tx.Commit()
}

func (s *Store) SetSessionEnabledProviders(sessionID int64, providers []string) error {
	if providers == nil {
		providers = []string{}
	}
	data, err := json.Marshal(providers)
	if err != nil {
		return err
	}
	return affected(s.db.Exec(`UPDATE sessions SET enabled_providers = ? WHERE id = ?`, string(data), sessionID))
}

// DeleteSession removes the session and everything keyed by it, in one
// transaction. agent_instances and tasks reference each other, so foreign key
// checks wait for the commit.
func (s *Store) DeleteSession(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	const mine = `(SELECT id FROM agent_instances WHERE session_id = ?)`
	const myBoards = `(SELECT id FROM boards WHERE session_id = ?)`
	for _, q := range []string{
		`DELETE FROM agent_events WHERE agent_id IN ` + mine,
		`DELETE FROM notes WHERE board_id IN ` + myBoards,
		`DELETE FROM board_members WHERE board_id IN ` + myBoards,
		`DELETE FROM escalations WHERE session_id = ?`,
		`DELETE FROM tasks WHERE session_id = ?`,
		`DELETE FROM agent_instances WHERE session_id = ?`,
		`DELETE FROM boards WHERE session_id = ?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	if err := affected(tx.Exec(`DELETE FROM sessions WHERE id = ?`, id)); err != nil {
		return err
	}
	return tx.Commit()
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

	ContextUsed   int `json:"context_used,omitempty"`   // tokens in the latest assistant message; 0 = unknown
	ContextWindow int `json:"context_window,omitempty"` // the model's window; 0 = unknown

	Model    string `json:"model,omitempty"` // from the stream-json init line; "" = unknown
	Provider string `json:"provider"`
}

const agentCols = `id, session_id, COALESCE(parent_id,0), role, token, COALESCE(task_id,0), status,
	COALESCE(pid,0), COALESCE(log_path,''), exit_code, created_at, COALESCE(exited_at,''), context_used, context_window, model, COALESCE(provider, 'claude')`

func scanAgent(r scanner) (a Agent, err error) {
	err = r.Scan(&a.ID, &a.SessionID, &a.ParentID, &a.Role, &a.Token, &a.TaskID, &a.Status,
		&a.PID, &a.LogPath, &a.ExitCode, &a.CreatedAt, &a.ExitedAt, &a.ContextUsed, &a.ContextWindow, &a.Model, &a.Provider)
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
// If provider is empty, fallback to the session's provider.
func (s *Store) CreateAgent(sessionID int64, role string, parentID, taskID int64, provider ...string) (Agent, error) {
	var prov string
	if len(provider) > 0 {
		prov = provider[0]
	}
	if prov != "" {
		if err := CheckProvider(prov); err != nil {
			return Agent{}, err
		}
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return Agent{}, err
	}
	return scanAgent(s.db.QueryRow(`INSERT INTO agent_instances (session_id, parent_id, role, token, task_id, provider)
		SELECT id, ?2, ?3, ?4, ?5, CASE WHEN ?6 != '' THEN ?6 ELSE provider END FROM sessions WHERE id = ?1
		AND (?2 IS NULL OR EXISTS (SELECT 1 FROM agent_instances WHERE id = ?2 AND session_id = ?1))
		AND (?5 IS NULL OR EXISTS (SELECT 1 FROM tasks WHERE id = ?5 AND session_id = ?1))
		RETURNING `+agentCols, sessionID, nullable(parentID), role, hex.EncodeToString(tok), nullable(taskID), prov))
}

// GetAgentByToken resolves an agent from its secret; the agent carries its SessionID.
func (s *Store) GetAgentByToken(token string) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agent_instances WHERE token = ?`, token))
}

// FindAgent returns an agent by id alone, for callers (the app) that do not know its session yet.
func (s *Store) FindAgent(id int64) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agent_instances WHERE id = ?`, id))
}

// MarkRunningAgentsCrashed records agents still marked running as crashed (their
// processes died with the previous run) and blocks their working tasks. It
// returns the affected agents.
func (s *Store) MarkRunningAgentsCrashed() ([]Agent, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`UPDATE agent_instances SET status = 'crashed', exited_at = ` + now + `
		WHERE status = 'running' RETURNING ` + agentCols)
	if err != nil {
		return nil, err
	}
	var out []Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE tasks SET status = 'blocked' WHERE status = 'working'
		AND agent_id IN (SELECT id FROM agent_instances WHERE status = 'crashed')`); err != nil {
		return nil, err
	}
	return out, tx.Commit()
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

// SetAgentContext records the agent's context usage.
func (s *Store) SetAgentContext(sessionID, id int64, used, window int) error {
	return affected(s.db.Exec(`UPDATE agent_instances SET context_used = ?, context_window = ? WHERE id = ? AND session_id = ?`,
		used, window, id, sessionID))
}

// GetSetting returns the stored (JSON) value of an app setting, or ErrNotFound if it was never set.
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	return v, one(s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v))
}

// PutSetting stores the (JSON) value of an app setting, replacing any earlier one.
func (s *Store) PutSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// SetAgentModel records the model the agent runs on.
func (s *Store) SetAgentModel(sessionID, id int64, model string) error {
	return affected(s.db.Exec(`UPDATE agent_instances SET model = ? WHERE id = ? AND session_id = ?`,
		model, id, sessionID))
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

// ListTasks returns the session's tasks, oldest first.
func (s *Store) ListTasks(sessionID int64) ([]Task, error) {
	rows, err := s.db.Query(`SELECT `+taskCols+` FROM tasks WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
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
	AuthorID  int64  `json:"author_agent_id"` // 0 = the user
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

const noteCols = `id, board_id, COALESCE(author_agent_id,0), type, content, status, created_at, updated_at`

// inSession restricts a notes query to boards of the given session (one ? arg).
const inSession = `board_id IN (SELECT id FROM boards WHERE session_id = ?)`

func scanNote(r scanner) (n Note, err error) {
	err = r.Scan(&n.ID, &n.BoardID, &n.AuthorID, &n.Type, &n.Content, &n.Status, &n.CreatedAt, &n.UpdatedAt)
	return n, one(err)
}

// PostNote adds a note to a board of the session. authorID 0 means the user;
// otherwise the author must be an agent of the same session (else ErrNotFound).
func (s *Store) PostNote(sessionID, boardID, authorID int64, typ, content string) (Note, error) {
	return scanNote(s.db.QueryRow(`INSERT INTO notes (board_id, author_agent_id, type, content)
		SELECT b.id, ?5, ?1, ?2 FROM boards b WHERE b.id = ?3 AND b.session_id = ?4
		AND (?5 IS NULL OR EXISTS (SELECT 1 FROM agent_instances WHERE id = ?5 AND session_id = ?4))
		RETURNING `+noteCols, typ, content, boardID, sessionID, nullable(authorID)))
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
		RETURNING `+escalationCols,
		question, context, agentID, sessionID).
		Scan(&e.ID, &e.SessionID, &e.AgentID, &e.Question, &e.Context, &e.Status, &e.Answer)
	return e, one(err)
}

const escalationCols = `id, session_id, agent_id, question, context, status, COALESCE(answer,'')`

func scanEscalation(r scanner) (e Escalation, err error) {
	err = r.Scan(&e.ID, &e.SessionID, &e.AgentID, &e.Question, &e.Context, &e.Status, &e.Answer)
	return e, one(err)
}

// FindEscalation returns an escalation by id alone (it carries its SessionID).
func (s *Store) FindEscalation(id int64) (Escalation, error) {
	return scanEscalation(s.db.QueryRow(`SELECT `+escalationCols+` FROM escalations WHERE id = ?`, id))
}

// ListEscalations returns the session's escalations, oldest first.
func (s *Store) ListEscalations(sessionID int64) ([]Escalation, error) {
	rows, err := s.db.Query(`SELECT `+escalationCols+` FROM escalations WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Escalation
	for rows.Next() {
		e, err := scanEscalation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AnswerEscalation stores the answer and marks the escalation answered; an
// escalation that is already answered is ErrNotFound.
func (s *Store) AnswerEscalation(sessionID, id int64, answer string) (Escalation, error) {
	return scanEscalation(s.db.QueryRow(`UPDATE escalations SET status = 'answered', answer = ?
		WHERE id = ? AND session_id = ? AND status = 'open' RETURNING `+escalationCols, answer, id, sessionID))
}

// ReopenEscalation undoes AnswerEscalation (its answer could not be delivered);
// an escalation that is not answered is ErrNotFound.
func (s *Store) ReopenEscalation(sessionID, id int64) error {
	return affected(s.db.Exec(`UPDATE escalations SET status = 'open', answer = NULL
		WHERE id = ? AND session_id = ? AND status = 'answered'`, id, sessionID))
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

// DeleteAgentEvent removes one event of the agent (undoes an append whose delivery failed).
func (s *Store) DeleteAgentEvent(sessionID, agentID, id int64) error {
	return affected(s.db.Exec(`DELETE FROM agent_events WHERE id = ? AND agent_id IN
		(SELECT id FROM agent_instances WHERE id = ? AND session_id = ?)`, id, agentID, sessionID))
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

// ListAgentEventTail returns at most limit newest events, in chronological order.
// Unlike paged history retrieval, work is bounded by limit, not the agent's history.
func (s *Store) ListAgentEventTail(sessionID, agentID int64, limit int) ([]AgentEvent, error) {
	if limit <= 0 || limit > 2000 {
		limit = 2000
	}
	rows, err := s.db.Query(`SELECT `+agentEventCols+` FROM agent_events
		WHERE agent_id = ? AND agent_id IN (SELECT id FROM agent_instances WHERE session_id = ?)
		ORDER BY id DESC LIMIT ?`, agentID, sessionID, limit)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// LastAgentEventOfType returns the newest event of the type across all agents (ErrNotFound if none).
func (s *Store) LastAgentEventOfType(eventType string) (AgentEvent, error) {
	return scanAgentEvent(s.db.QueryRow(`SELECT `+agentEventCols+` FROM agent_events WHERE event_type = ? ORDER BY id DESC LIMIT 1`, eventType))
}

// ListAgentEventsOfType is ListAgentEvents restricted to the given event types, with no paging.
func (s *Store) ListAgentEventsOfType(sessionID, agentID int64, types ...string) ([]AgentEvent, error) {
	args := []any{agentID, sessionID}
	marks := ""
	for i, t := range types {
		if i > 0 {
			marks += ","
		}
		marks += "?"
		args = append(args, t)
	}
	rows, err := s.db.Query(`SELECT `+agentEventCols+` FROM agent_events
		WHERE agent_id = ? AND agent_id IN (SELECT id FROM agent_instances WHERE session_id = ?)
		AND event_type IN (`+marks+`) ORDER BY id`, args...)
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

// EscalationEvent returns the "escalation" agent event that put the escalation into its agent's history.
func (s *Store) EscalationEvent(sessionID, escalationID int64) (AgentEvent, error) {
	return scanAgentEvent(s.db.QueryRow(`SELECT `+agentEventCols+` FROM agent_events
		WHERE event_type = 'escalation' AND json_extract(payload, '$.escalation_id') = ?
		AND agent_id IN (SELECT id FROM agent_instances WHERE session_id = ?)`, escalationID, sessionID))
}
