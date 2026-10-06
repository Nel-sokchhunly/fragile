-- Per-agent context usage (tokens in the latest assistant message, and the model's window), 0 = unknown.
ALTER TABLE agent_instances ADD COLUMN context_used INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_instances ADD COLUMN context_window INTEGER NOT NULL DEFAULT 0;
