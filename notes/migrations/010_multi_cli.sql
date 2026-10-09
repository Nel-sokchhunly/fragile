ALTER TABLE agent_instances ADD COLUMN provider TEXT NOT NULL DEFAULT 'claude';
ALTER TABLE sessions ADD COLUMN enabled_providers TEXT;
