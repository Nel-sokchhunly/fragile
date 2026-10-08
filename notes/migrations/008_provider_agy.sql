-- Provider 'agy': Google Antigravity CLI support.
-- SQLite cannot alter a CHECK constraint in place, so sessions is rebuilt (foreign keys are off, see migrate).

CREATE TABLE sessions_new (
	id         INTEGER PRIMARY KEY,
	title      TEXT NOT NULL DEFAULT '',
	status     TEXT NOT NULL DEFAULT 'working' CHECK (status IN ('working','done','needs_you')),
	provider   TEXT NOT NULL DEFAULT 'claude' CHECK (provider IN ('claude','codex','agy')),
	work_dir   TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO sessions_new (id, title, status, provider, work_dir, created_at)
	SELECT id, title, status, provider, work_dir, created_at FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_new RENAME TO sessions;
