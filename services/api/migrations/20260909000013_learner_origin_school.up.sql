-- 20260909000013_learner_origin_school.up.sql
-- Issue #99: a learner visible only through the enrollment JOIN was invisible
-- to the school that created it until its first enrollment, breaking the
-- create → pick → enroll onboarding path. Record WHICH school provisioned the
-- learner so the creating school can see it pre-enrollment.
--
-- Nullable + backfill-safe: pre-existing learners keep NULL and remain visible
-- exactly via their enrollments (visibility widening is additive only).
--
-- ON DELETE SET NULL, not CASCADE: learners are GLOBAL identity records
-- (spec §19 / ADR-001) that may be enrolled at several schools — CASCADE would
-- delete the person when the origin school is deleted, destroying rows other
-- schools still reference. Clearing the pointer simply drops the learner back
-- to enrollment-based visibility. (schools.group_id uses SET NULL for the same
-- optional-reference reason; every mandatory school binding, e.g.
-- enrollments.school_id, uses CASCADE.)
ALTER TABLE learners
    ADD COLUMN origin_school_id UUID REFERENCES schools(id) ON DELETE SET NULL;

-- The hot predicate is origin_school_id = $school in list/by-id lookups.
CREATE INDEX learners_origin_school_idx ON learners (origin_school_id);
