-- Persist the PR a run opened and the later retraction (Linear 42-56).
-- In-memory maps are not enough: retract happens after the fact, and
-- after a daemon restart.

ALTER TABLE sessions ADD COLUMN pull_request TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN retract_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN retracted_at_unix_nano INTEGER;
