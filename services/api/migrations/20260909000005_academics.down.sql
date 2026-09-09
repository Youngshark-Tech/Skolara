-- Reverse of 20260909000005_academics.up.sql, in dependency order.

ALTER TABLE enrollments
    DROP CONSTRAINT IF EXISTS enrollments_class_group_fk,
    DROP CONSTRAINT IF EXISTS enrollments_academic_year_fk;

DROP TABLE IF EXISTS teaching_assignments;
DROP TABLE IF EXISTS roster_entries;
DROP TABLE IF EXISTS class_groups;
DROP TABLE IF EXISTS subjects;
DROP TABLE IF EXISTS terms;
DROP TABLE IF EXISTS academic_years;
