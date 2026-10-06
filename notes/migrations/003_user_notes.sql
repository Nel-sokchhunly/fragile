-- User-authored notes: author_agent_id becomes nullable (NULL = the user).
-- SQLite cannot drop NOT NULL in place, so notes is rebuilt (foreign keys are off, see migrate).
-- Also adds the per-session working directory the app asks for when creating a session.

CREATE TABLE notes_new (
	id              INTEGER PRIMARY KEY,
	board_id        INTEGER NOT NULL REFERENCES boards(id),
	author_agent_id INTEGER REFERENCES agent_instances(id), -- NULL = the user
	type            TEXT NOT NULL CHECK (type IN ('decision','blocker','heads_up','done','question')),
	content         TEXT NOT NULL,
	status          TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
	created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO notes_new SELECT id, board_id, author_agent_id, type, content, status, created_at, updated_at FROM notes;
DROP TABLE notes;
ALTER TABLE notes_new RENAME TO notes;
CREATE INDEX notes_board ON notes(board_id, id);

-- '' = none recorded (Phase 0 and CLI sessions); the runner then uses its configured WorkDir.
ALTER TABLE sessions ADD COLUMN work_dir TEXT NOT NULL DEFAULT '';
