ALTER TABLE sessions ADD COLUMN provider TEXT NOT NULL DEFAULT 'claude' CHECK (provider IN ('claude', 'codex'));
