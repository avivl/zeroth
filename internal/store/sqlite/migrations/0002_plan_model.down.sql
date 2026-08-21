-- Reverse of 0002_plan_model. Row-level JSON fields added in 0002 are
-- left in effects_json; dropping unknown keys is the reader's job.

ALTER TABLE plans DROP COLUMN hash;
ALTER TABLE plans DROP COLUMN expires_at_unix_nano;
ALTER TABLE plans DROP COLUMN cost_ceiling;
ALTER TABLE plans DROP COLUMN scope_id;
ALTER TABLE plans DROP COLUMN credentials_json;
