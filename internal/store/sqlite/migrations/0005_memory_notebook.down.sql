-- Reverse of 0005_memory_notebook. Existing bodies stay in content.

DROP INDEX IF EXISTS memory_entries_fact_key;

ALTER TABLE memory_entries DROP COLUMN fact_key;
ALTER TABLE memory_entries DROP COLUMN author;
ALTER TABLE memory_entries DROP COLUMN author_kind;
ALTER TABLE memory_entries DROP COLUMN source;
ALTER TABLE memory_entries DROP COLUMN action;
ALTER TABLE memory_entries DROP COLUMN deleted;
ALTER TABLE memory_entries DROP COLUMN version;
ALTER TABLE memory_entries DROP COLUMN updated_at_unix_nano;
ALTER TABLE memory_entries DROP COLUMN history_json;

ALTER TABLE memory_proposals DROP COLUMN fact_key;
ALTER TABLE memory_proposals DROP COLUMN author;
ALTER TABLE memory_proposals DROP COLUMN author_kind;
ALTER TABLE memory_proposals DROP COLUMN source;
