-- Reverse of 0004_agent_reviewer. Reviewer config is dropped; plans
-- keep their stored cross_exam_json.

ALTER TABLE agents DROP COLUMN reviewer_model;
ALTER TABLE agents DROP COLUMN reviewer_model_2;
ALTER TABLE agents DROP COLUMN reviewer_dual;
ALTER TABLE agents DROP COLUMN block_on_fail;
