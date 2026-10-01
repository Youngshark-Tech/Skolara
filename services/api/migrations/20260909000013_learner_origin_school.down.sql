-- 20260909000013_learner_origin_school.down.sql
-- Reverses 20260909000013_learner_origin_school.up.sql (reverse order:
-- dependent index first, then the column with its FK).
DROP INDEX IF EXISTS learners_origin_school_idx;
ALTER TABLE learners DROP COLUMN IF EXISTS origin_school_id;
