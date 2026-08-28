-- Reverse of 0007_harness_session. A killed turn restarts from its prompt
-- again rather than resuming.

ALTER TABLE sessions DROP COLUMN harness_session;
