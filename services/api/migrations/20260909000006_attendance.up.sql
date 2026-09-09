-- Attendance bounded context: per-class daily sessions and per-learner
-- records with offline-tolerant idempotency (client_mutation_id). Sessions
-- hang off academics.class_groups; records reference students.learners and
-- identity.users via DB FKs.

CREATE TABLE attendance_sessions (
    id             UUID PRIMARY KEY,
    school_id      UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    class_group_id UUID NOT NULL REFERENCES class_groups(id) ON DELETE CASCADE,
    session_date   DATE NOT NULL,
    status         TEXT NOT NULL DEFAULT 'open'
                   CHECK (status IN ('open','closed')),
    created_by     UUID NOT NULL REFERENCES users(id),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (school_id, class_group_id, session_date)
);
CREATE INDEX attendance_sessions_school_date_idx ON attendance_sessions (school_id, session_date DESC);

CREATE TABLE attendance_records (
    id                 UUID PRIMARY KEY,
    school_id          UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    session_id         UUID NOT NULL REFERENCES attendance_sessions(id) ON DELETE CASCADE,
    learner_id         UUID NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    status             TEXT NOT NULL
                       CHECK (status IN ('present','absent','late','excused')),
    reason             TEXT NOT NULL DEFAULT '',
    recorded_by        UUID NOT NULL REFERENCES users(id),
    client_mutation_id TEXT NOT NULL,
    recorded_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_id, learner_id)                -- one outcome per learner per session
);
-- Global uniqueness of mutation ids makes replays detectable no matter which
-- row they land on (offline sync groundwork, master spec §46).
CREATE UNIQUE INDEX attendance_records_mutation_key ON attendance_records (client_mutation_id);
CREATE INDEX attendance_records_learner_idx ON attendance_records (learner_id);
