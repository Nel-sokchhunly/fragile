-- Named shared/private boards (spec 5.1): a board's scope name, e.g. "private:auth".
-- Session-scope boards keep name ''.
ALTER TABLE boards ADD COLUMN name TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX boards_session_name ON boards(session_id, name) WHERE name <> '';
