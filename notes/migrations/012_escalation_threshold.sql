-- Per-session setting: when the orchestrator should escalate to the user.
ALTER TABLE sessions ADD COLUMN escalation_threshold TEXT NOT NULL DEFAULT '';
