-- Assignments bounded context: assignment workflow (draft -> published ->
-- closed) and submissions (submitted -> graded -> returned). References
-- academics.class_groups / subjects and students.learners via DB FKs.

CREATE TABLE assignments (
    id             UUID PRIMARY KEY,
    school_id      UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    class_group_id UUID NOT NULL REFERENCES class_groups(id) ON DELETE CASCADE,
    subject_id     UUID NOT NULL REFERENCES subjects(id) ON DELETE CASCADE,
    teacher_id     UUID NOT NULL REFERENCES users(id),
    title          TEXT NOT NULL,
    instructions   TEXT NOT NULL DEFAULT '',
    due_date       DATE,
    status         TEXT NOT NULL DEFAULT 'draft'
                   CHECK (status IN ('draft','published','closed')),
    published_at   TIMESTAMPTZ,
    closed_at      TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX assignments_school_idx ON assignments (school_id, status);
CREATE INDEX assignments_class_idx ON assignments (class_group_id);
CREATE INDEX assignments_teacher_idx ON assignments (teacher_id);

CREATE TABLE assignment_submissions (
    id            UUID PRIMARY KEY,
    school_id     UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    assignment_id UUID NOT NULL REFERENCES assignments(id) ON DELETE CASCADE,
    learner_id    UUID NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    content       TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'submitted'
                  CHECK (status IN ('submitted','graded','returned')),
    grade         TEXT,
    feedback      TEXT NOT NULL DEFAULT '',
    submitted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    graded_at     TIMESTAMPTZ,
    returned_at   TIMESTAMPTZ,
    UNIQUE (assignment_id, learner_id)
);
CREATE INDEX assignment_submissions_learner_idx ON assignment_submissions (learner_id);
