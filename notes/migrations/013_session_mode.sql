-- A session is a normal one-on-one chat or an orchestra (the orchestrator
-- with sub-agents); existing sessions keep working as orchestras.
ALTER TABLE sessions ADD COLUMN mode TEXT NOT NULL DEFAULT 'orchestra' CHECK (mode IN ('normal', 'orchestra'));
