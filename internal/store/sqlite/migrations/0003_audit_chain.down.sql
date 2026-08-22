-- Reverse of 0003_audit_chain. Product rows in agents and older audit
-- columns stay; chain fields and the key registry are removed.

DROP TRIGGER IF EXISTS agent_keys_no_delete;
DROP TRIGGER IF EXISTS agent_keys_no_update;
DROP INDEX IF EXISTS agent_keys_agent;
DROP TABLE IF EXISTS agent_keys;

DROP INDEX IF EXISTS audit_session;
DROP INDEX IF EXISTS audit_hash;
DROP INDEX IF EXISTS audit_prev_hash;

ALTER TABLE audit_records DROP COLUMN session_id;
ALTER TABLE audit_records DROP COLUMN agent_id;
ALTER TABLE audit_records DROP COLUMN hash;
ALTER TABLE audit_records DROP COLUMN prev_hash;
ALTER TABLE audit_records DROP COLUMN agent_pubkey;
ALTER TABLE audit_records DROP COLUMN approver;
ALTER TABLE audit_records DROP COLUMN lease_id;
ALTER TABLE audit_records DROP COLUMN postcondition;
ALTER TABLE audit_records DROP COLUMN precondition;
ALTER TABLE audit_records DROP COLUMN plan_hash;
ALTER TABLE audit_records DROP COLUMN target;
