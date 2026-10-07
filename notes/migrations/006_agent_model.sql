-- The model the agent's Claude Code process runs on (from its stream-json init line), '' = unknown.
ALTER TABLE agent_instances ADD COLUMN model TEXT NOT NULL DEFAULT '';
