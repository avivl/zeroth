-- Reverse of 0001_init. Product tables only; schema_migrations is owned
-- by the migrator. Foreign keys are disabled so drop order is not a puzzle.

PRAGMA foreign_keys = OFF;

DROP TRIGGER IF EXISTS audit_no_delete;
DROP TRIGGER IF EXISTS audit_no_update;
DROP TRIGGER IF EXISTS events_no_delete;
DROP TRIGGER IF EXISTS events_no_update;

DROP INDEX IF EXISTS checkpoints_session_created;
DROP INDEX IF EXISTS leases_agent;
DROP INDEX IF EXISTS audit_created;
DROP INDEX IF EXISTS audit_resource;
DROP INDEX IF EXISTS memory_proposals_status_created;
DROP INDEX IF EXISTS memory_entries_kind_ref;
DROP INDEX IF EXISTS approvals_status_created;
DROP INDEX IF EXISTS plans_status_created;
DROP INDEX IF EXISTS plans_session_created;
DROP INDEX IF EXISTS events_session_seq;
DROP INDEX IF EXISTS sessions_agent_created;
DROP INDEX IF EXISTS sessions_status_created;
DROP INDEX IF EXISTS agents_created;

DROP TABLE IF EXISTS checkpoints;
DROP TABLE IF EXISTS leases;
DROP TABLE IF EXISTS audit_records;
DROP TABLE IF EXISTS memory_proposals;
DROP TABLE IF EXISTS memory_entries;
DROP TABLE IF EXISTS approvals;
DROP TABLE IF EXISTS plans;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS agents;

PRAGMA foreign_keys = ON;
