-- Plan model (Z1-052): canonical hash, expiry, cost ceiling, scope, and
-- credential constraints on the plan as a whole. Row-level lease,
-- idempotency key, and postcondition live in effects_json.

ALTER TABLE plans ADD COLUMN hash TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN expires_at_unix_nano INTEGER NOT NULL DEFAULT 0;
ALTER TABLE plans ADD COLUMN cost_ceiling INTEGER NOT NULL DEFAULT 0;
ALTER TABLE plans ADD COLUMN scope_id TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN credentials_json TEXT NOT NULL DEFAULT '[]';
