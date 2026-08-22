-- Per-agent cross-exam reviewer (Z1-019). Dual requires both models
-- to pass. Block-on-fail returns a failed plan to the agent.

ALTER TABLE agents ADD COLUMN reviewer_model TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN reviewer_model_2 TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN reviewer_dual INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN block_on_fail INTEGER NOT NULL DEFAULT 0;
