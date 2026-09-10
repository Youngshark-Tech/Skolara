-- 20260909000012_attendance_mutation_scope.down.sql
DROP INDEX IF EXISTS attendance_records_mutation_key;
CREATE UNIQUE INDEX attendance_records_mutation_key
    ON attendance_records (client_mutation_id);
