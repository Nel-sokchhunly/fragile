-- Phase 1 schema (spec 5.3): many sessions, board_members, agent_events.
-- Fields only. Runs in a transaction with foreign keys off (see migrate), so
-- sessions can be rebuilt to add a status CHECK, which SQLite cannot ALTER in.

CREATE TABLE sessions_new (
	id         INTEGER PRIMARY KEY,
	title      TEXT NOT NULL DEFAULT '',
	status     TEXT NOT NULL DEFAULT 'working' CHECK (status IN ('working','done','needs_you')),
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
-- Phase 0 sessions ('active') belong to processes that are long gone: done.
INSERT INTO sessions_new (id, title, status, created_at)
	SELECT id, title, CASE WHEN status IN ('working','needs_you') THEN status ELSE 'done' END, created_at FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_new RENAME TO sessions;

CREATE INDEX agent_instances_session ON agent_instances(session_id, id);
CREATE INDEX tasks_session ON tasks(session_id, id);
-- One session-scope board per session; shared/private boards may be many.
CREATE UNIQUE INDEX boards_session_scope ON boards(session_id) WHERE scope_type = 'session';

-- Unused until Phase 2 (shared boards).
CREATE TABLE board_members (
	board_id INTEGER NOT NULL REFERENCES boards(id),
	agent_id INTEGER NOT NULL REFERENCES agent_instances(id),
	PRIMARY KEY (board_id, agent_id)
);

-- Per-agent history for the UI: output stream lines, status changes.
CREATE TABLE agent_events (
	id         INTEGER PRIMARY KEY,
	agent_id   INTEGER NOT NULL REFERENCES agent_instances(id),
	event_type TEXT NOT NULL,
	payload    TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX agent_events_agent ON agent_events(agent_id, id);
