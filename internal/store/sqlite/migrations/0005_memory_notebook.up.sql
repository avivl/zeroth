-- Notebook fields (Z1-022): key, provenance, version history, tombstone.
-- history_json is the full revision list; content is the current body.

ALTER TABLE memory_entries ADD COLUMN fact_key TEXT NOT NULL DEFAULT '';
ALTER TABLE memory_entries ADD COLUMN author TEXT NOT NULL DEFAULT '';
ALTER TABLE memory_entries ADD COLUMN author_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE memory_entries ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE memory_entries ADD COLUMN action TEXT NOT NULL DEFAULT '';
ALTER TABLE memory_entries ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memory_entries ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE memory_entries ADD COLUMN updated_at_unix_nano INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memory_entries ADD COLUMN history_json TEXT NOT NULL DEFAULT '[]';

CREATE INDEX memory_entries_fact_key ON memory_entries (kind, ref_id, fact_key);

ALTER TABLE memory_proposals ADD COLUMN fact_key TEXT NOT NULL DEFAULT '';
ALTER TABLE memory_proposals ADD COLUMN author TEXT NOT NULL DEFAULT '';
ALTER TABLE memory_proposals ADD COLUMN author_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE memory_proposals ADD COLUMN source TEXT NOT NULL DEFAULT '';
