-- Reverse of 0006_run_retract. Retraction history on the session row is dropped.

ALTER TABLE sessions DROP COLUMN pull_request;
ALTER TABLE sessions DROP COLUMN retract_reason;
ALTER TABLE sessions DROP COLUMN retracted_at_unix_nano;
