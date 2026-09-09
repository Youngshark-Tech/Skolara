-- Academics bounded context: academic years, terms, subjects, class groups,
-- rosters, teaching assignments. Everything is school-scoped: the class group
-- is the hub attendance/assignments hang off next. This migration also lands
-- the deferred FKs promised by the students migration (000004) on
-- enrollments.class_group_id / academic_year_id.

CREATE TABLE academic_years (
    id         UUID PRIMARY KEY,
    school_id  UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    start_date DATE NOT NULL,
    end_date   DATE NOT NULL,
    status     TEXT NOT NULL DEFAULT 'planning'
               CHECK (status IN ('planning','active','closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_date >= start_date)
);
CREATE UNIQUE INDEX academic_years_school_name_key ON academic_years (school_id, lower(name));
CREATE INDEX academic_years_school_idx ON academic_years (school_id);

CREATE TABLE terms (
    id               UUID PRIMARY KEY,
    school_id        UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    academic_year_id UUID NOT NULL REFERENCES academic_years(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    start_date       DATE NOT NULL,
    end_date         DATE NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_date >= start_date)
);
-- Term overlap is a SERVICE-layer invariant (inclusive ranges, clear domain
-- errors) — deliberately not an EXCLUDE constraint (btree_gist extension).
CREATE UNIQUE INDEX terms_school_year_name_key ON terms (school_id, academic_year_id, lower(name));
CREATE INDEX terms_year_idx ON terms (academic_year_id);

CREATE TABLE subjects (
    id         UUID PRIMARY KEY,
    school_id  UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    code       TEXT NOT NULL,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX subjects_school_code_key ON subjects (school_id, lower(code));

CREATE TABLE class_groups (
    id               UUID PRIMARY KEY,
    school_id        UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    academic_year_id UUID NOT NULL REFERENCES academic_years(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX class_groups_school_year_name_key ON class_groups (school_id, academic_year_id, lower(name));
CREATE INDEX class_groups_school_idx ON class_groups (school_id);

CREATE TABLE roster_entries (
    class_group_id UUID NOT NULL REFERENCES class_groups(id) ON DELETE CASCADE,
    learner_id     UUID NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    added_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (class_group_id, learner_id)
);
CREATE INDEX roster_entries_learner_idx ON roster_entries (learner_id);

CREATE TABLE teaching_assignments (
    id             UUID PRIMARY KEY,
    school_id      UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    class_group_id UUID NOT NULL REFERENCES class_groups(id) ON DELETE CASCADE,
    subject_id     UUID NOT NULL REFERENCES subjects(id) ON DELETE CASCADE,
    teacher_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One teacher owns a subject in a class; reassignment is a future workflow.
CREATE UNIQUE INDEX teaching_assignments_class_subject_key ON teaching_assignments (class_group_id, subject_id);
CREATE INDEX teaching_assignments_teacher_idx ON teaching_assignments (teacher_id);

-- Deferred FKs from the students migration (000004 hand-off comment):
-- enrollments may now reference real classes and academic years.
ALTER TABLE enrollments
    ADD CONSTRAINT enrollments_class_group_fk
        FOREIGN KEY (class_group_id) REFERENCES class_groups(id) ON DELETE SET NULL,
    ADD CONSTRAINT enrollments_academic_year_fk
        FOREIGN KEY (academic_year_id) REFERENCES academic_years(id) ON DELETE SET NULL;
