-- 20260909000012_attendance_mutation_scope.up.sql
-- Issue #49: client mutation ids are replay keys within a school, not global
-- identifiers. The global unique index let two schools' unrelated
-- client-generated ids collide into a spurious 400 ErrMutationUsed.
DROP INDEX IF EXISTS attendance_records_mutation_key;
CREATE UNIQUE INDEX attendance_records_mutation_key
    ON attendance_records (school_id, client_mutation_id);
