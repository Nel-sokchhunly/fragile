-- Phase 0 schema (spec 5.3). Timestamps are ISO-8601 UTC text.
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
