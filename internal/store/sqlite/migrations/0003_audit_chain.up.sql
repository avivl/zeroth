-- Audit hash chain (Z1-057) and append-only agent pubkey registry.
-- Rotation inserts a new key row; historical signatures stay verifiable.

ALTER TABLE audit_records ADD COLUMN target TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_records ADD COLUMN plan_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_records ADD COLUMN precondition TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_records ADD COLUMN postcondition TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_records ADD COLUMN lease_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_records ADD COLUMN approver TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_records ADD COLUMN agent_pubkey TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_records ADD COLUMN prev_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_records ADD COLUMN hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_records ADD COLUMN agent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_records ADD COLUMN session_id TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX audit_prev_hash ON audit_records (prev_hash);
CREATE UNIQUE INDEX audit_hash ON audit_records (hash) WHERE hash != '';
CREATE INDEX audit_session ON audit_records (session_id, created_at_unix_nano ASC, id ASC);

CREATE TABLE agent_keys (
	pubkey TEXT PRIMARY KEY,
	agent_id TEXT NOT NULL REFERENCES agents(id),
	created_at_unix_nano INTEGER NOT NULL
);

CREATE INDEX agent_keys_agent ON agent_keys (agent_id, created_at_unix_nano ASC);

CREATE TRIGGER agent_keys_no_update BEFORE UPDATE ON agent_keys
BEGIN
	SELECT RAISE(ABORT, 'agent keys are append-only');
END;

CREATE TRIGGER agent_keys_no_delete BEFORE DELETE ON agent_keys
BEGIN
	SELECT RAISE(ABORT, 'agent keys are append-only');
END;
