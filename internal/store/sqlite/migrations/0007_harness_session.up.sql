-- Persist the harness vendor session so a killed turn can resume (Linear
-- 42-78). The session id lives in the driver's memory and dies with the
-- daemon, so a restart could only ever start the turn over from the
-- original prompt.

ALTER TABLE sessions ADD COLUMN harness_session TEXT NOT NULL DEFAULT '';
