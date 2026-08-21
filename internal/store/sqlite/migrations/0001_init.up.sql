-- Stage-1 schema. Indices match OpenAPI list filters (status, agent, run,
-- resource). Events and audit_records are append-only via triggers.

CREATE TABLE agents (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	harness TEXT NOT NULL,
	status TEXT NOT NULL,
	model TEXT NOT NULL DEFAULT '',
	tools_json TEXT NOT NULL DEFAULT '[]',
	autonomy_tier TEXT NOT NULL DEFAULT '',
	created_at_unix_nano INTEGER NOT NULL,
	updated_at_unix_nano INTEGER NOT NULL
);

CREATE INDEX agents_created ON agents (created_at_unix_nano DESC, id DESC);

CREATE TABLE sessions (
	id TEXT PRIMARY KEY,
	agent_id TEXT NOT NULL REFERENCES agents(id),
	plan_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	prompt TEXT NOT NULL DEFAULT '',
	tracker_ref TEXT NOT NULL DEFAULT '',
	workspace_json TEXT NOT NULL DEFAULT '{}',
	autonomy_tier TEXT NOT NULL DEFAULT '',
	created_at_unix_nano INTEGER NOT NULL,
	updated_at_unix_nano INTEGER NOT NULL,
	finished_at_unix_nano INTEGER
);

CREATE INDEX sessions_status_created ON sessions (status, created_at_unix_nano DESC, id DESC);
CREATE INDEX sessions_agent_created ON sessions (agent_id, created_at_unix_nano DESC, id DESC);

CREATE TABLE events (
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id),
	type TEXT NOT NULL,
	plan_id TEXT NOT NULL DEFAULT '',
	message TEXT NOT NULL DEFAULT '',
	payload TEXT NOT NULL DEFAULT '',
	created_at_unix_nano INTEGER NOT NULL
);

CREATE INDEX events_session_seq ON events (session_id, seq);

CREATE TRIGGER events_no_update BEFORE UPDATE ON events
BEGIN
	SELECT RAISE(ABORT, 'events are append-only');
END;

CREATE TRIGGER events_no_delete BEFORE DELETE ON events
BEGIN
	SELECT RAISE(ABORT, 'events are append-only');
END;

CREATE TABLE plans (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL REFERENCES sessions(id),
	parent_plan_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	summary TEXT NOT NULL,
	effects_json TEXT NOT NULL DEFAULT '[]',
	cross_exam_json TEXT NOT NULL DEFAULT '',
	secret_scan_findings_json TEXT NOT NULL DEFAULT '[]',
	review_comment TEXT NOT NULL DEFAULT '',
	created_at_unix_nano INTEGER NOT NULL,
	updated_at_unix_nano INTEGER NOT NULL
);

CREATE INDEX plans_session_created ON plans (session_id, created_at_unix_nano DESC, id DESC);
CREATE INDEX plans_status_created ON plans (status, created_at_unix_nano DESC, id DESC);

CREATE TABLE approvals (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	status TEXT NOT NULL,
	plan_id TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	created_at_unix_nano INTEGER NOT NULL
);

CREATE INDEX approvals_status_created ON approvals (status, created_at_unix_nano ASC, id ASC);

CREATE TABLE memory_entries (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	ref_id TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL,
	created_at_unix_nano INTEGER NOT NULL
);

CREATE INDEX memory_entries_kind_ref ON memory_entries (kind, ref_id, created_at_unix_nano DESC, id DESC);

CREATE TABLE memory_proposals (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	ref_id TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL,
	status TEXT NOT NULL,
	memory_id TEXT NOT NULL DEFAULT '',
	created_at_unix_nano INTEGER NOT NULL,
	reviewed_at_unix_nano INTEGER
);

CREATE INDEX memory_proposals_status_created ON memory_proposals (status, created_at_unix_nano DESC, id DESC);

CREATE TABLE audit_records (
	id TEXT PRIMARY KEY,
	action TEXT NOT NULL,
	resource_type TEXT NOT NULL,
	resource_id TEXT NOT NULL,
	actor TEXT NOT NULL DEFAULT '',
	signature TEXT NOT NULL,
	created_at_unix_nano INTEGER NOT NULL
);

CREATE INDEX audit_resource ON audit_records (resource_type, resource_id, created_at_unix_nano DESC, id DESC);
CREATE INDEX audit_created ON audit_records (created_at_unix_nano DESC, id DESC);

CREATE TRIGGER audit_no_update BEFORE UPDATE ON audit_records
BEGIN
	SELECT RAISE(ABORT, 'audit records are append-only');
END;

CREATE TRIGGER audit_no_delete BEFORE DELETE ON audit_records
BEGIN
	SELECT RAISE(ABORT, 'audit records are append-only');
END;

CREATE TABLE leases (
	id TEXT PRIMARY KEY,
	grant_id TEXT NOT NULL,
	scope_id TEXT NOT NULL,
	agent_id TEXT NOT NULL REFERENCES agents(id),
	expires_at_unix_nano INTEGER NOT NULL,
	minted_at_unix_nano INTEGER NOT NULL
);

CREATE INDEX leases_agent ON leases (agent_id, expires_at_unix_nano);

CREATE TABLE checkpoints (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL REFERENCES sessions(id),
	label TEXT NOT NULL DEFAULT '',
	location TEXT NOT NULL DEFAULT '',
	created_at_unix_nano INTEGER NOT NULL
);

CREATE INDEX checkpoints_session_created ON checkpoints (session_id, created_at_unix_nano DESC, id DESC);
