-- Agent status 'stopped': the runner killed the agent on purpose (user stopped the session or quit).
-- SQLite cannot alter a CHECK in place, so agent_instances is rebuilt (foreign keys are off, see migrate).

CREATE TABLE agent_instances_new (
	id         INTEGER PRIMARY KEY,
	session_id INTEGER NOT NULL REFERENCES sessions(id),
	parent_id  INTEGER REFERENCES agent_instances(id),
	role       TEXT NOT NULL CHECK (role IN ('orchestrator','subagent')),
	token      TEXT UNIQUE,
	task_id    INTEGER REFERENCES tasks(id),
	status     TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running','exited','stopped','crashed')),
	pid        INTEGER,
	log_path   TEXT,
	exit_code  INTEGER,
	exited_at  TEXT,
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	context_used   INTEGER NOT NULL DEFAULT 0,
	context_window INTEGER NOT NULL DEFAULT 0
);
INSERT INTO agent_instances_new (id, session_id, parent_id, role, token, task_id, status, pid, log_path, exit_code, exited_at, created_at, context_used, context_window)
	SELECT id, session_id, parent_id, role, token, task_id, status, pid, log_path, exit_code, exited_at, created_at, context_used, context_window FROM agent_instances;
DROP TABLE agent_instances;
ALTER TABLE agent_instances_new RENAME TO agent_instances;
CREATE INDEX agent_instances_session ON agent_instances(session_id, id);
