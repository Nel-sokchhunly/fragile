-- Per-agent CLI (sub-agents may run on another CLI than their session) and
-- per-session settings, copied from the global template when a session is created.
ALTER TABLE agent_instances ADD COLUMN provider TEXT NOT NULL DEFAULT 'claude';
UPDATE agent_instances SET provider = (SELECT provider FROM sessions WHERE sessions.id = agent_instances.session_id);

ALTER TABLE sessions ADD COLUMN enabled_providers TEXT NOT NULL DEFAULT '[]'; -- JSON array; the first is the spawn default
ALTER TABLE sessions ADD COLUMN auto_compact_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN orchestrator_rules TEXT NOT NULL DEFAULT '';
-- Existing sessions keep what applied to them: their own CLI, the global auto-compact.
UPDATE sessions SET enabled_providers = json_array(provider),
	auto_compact_tokens = COALESCE((SELECT CAST(value AS INTEGER) FROM settings WHERE key = 'auto_compact_tokens'), 0);
