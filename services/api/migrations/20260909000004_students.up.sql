-- Students bounded context: global learner identity, guardians,
-- guardian-learner links, and school enrollments with a lifecycle state
-- machine. Learners are deliberately NOT tenant-scoped (spec §19): a learner
-- may enroll at multiple schools over time — enrollment is the tenant binding.

CREATE TABLE learners (
    id            UUID PRIMARY KEY,
    first_name    TEXT NOT NULL,
    last_name     TEXT NOT NULL,
    middle_name   TEXT,
    date_of_birth DATE,
    gender        TEXT NOT NULL DEFAULT '',
    external_id   TEXT,                          -- school-issued admission number
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- External ids are unique when present (NULLs never collide).
CREATE UNIQUE INDEX learners_external_id_key ON learners (external_id) WHERE external_id IS NOT NULL;

CREATE TABLE guardians (
    id         UUID PRIMARY KEY,
    first_name TEXT NOT NULL,
    last_name  TEXT NOT NULL,
    phone      TEXT NOT NULL DEFAULT '',
    email      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A guardian may care for many learners; a learner may have many guardians.
-- Access flags (financials/academics) are per-link, consumed by guardian surfaces.
CREATE TABLE guardian_learner_links (
    guardian_id         UUID NOT NULL REFERENCES guardians(id) ON DELETE CASCADE,
    learner_id          UUID NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    relationship        TEXT NOT NULL CHECK (relationship IN ('mother','father','guardian','other')),
    is_primary          BOOLEAN NOT NULL DEFAULT FALSE,
    can_view_financials BOOLEAN NOT NULL DEFAULT FALSE,
    can_view_academics  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (guardian_id, learner_id)      -- unique guardian-learner pair
);
CREATE INDEX guardian_learner_links_learner_idx ON guardian_learner_links (learner_id);

-- class_group_id / academic_year_id are plain UUIDs for now — the classes and
-- academic_years tables do not exist yet; FK constraints land with the
-- academics migration (issue #9).
CREATE TABLE enrollments (
    id               UUID PRIMARY KEY,
    school_id        UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    learner_id       UUID NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    class_group_id   UUID,                      -- FK lands with academics migration
    academic_year_id UUID,                      -- FK lands with academics migration
    status           TEXT NOT NULL DEFAULT 'applicant'
                     CHECK (status IN ('applicant','admitted','active','suspended',
                                       'transfer_pending','transferred_out','alumni',
                                       'withdrawn','graduated')),
    started_at       TIMESTAMPTZ,
    ended_at         TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX enrollments_school_status_idx ON enrollments (school_id, status);
CREATE INDEX enrollments_learner_idx ON enrollments (learner_id);

-- Partial index: the hot lookup is live (non-terminal) enrollments per school
-- and status; terminal rows drop out of the index automatically.
CREATE INDEX enrollments_school_open_status_idx ON enrollments (school_id, status)
    WHERE status IN ('applicant','admitted','active','suspended','transfer_pending');

-- At most one OPEN enrollment per learner per school; re-enrollment is allowed
-- after a terminal state (transferred_out / withdrawn / graduated->alumni).
CREATE UNIQUE INDEX enrollments_school_learner_open_key ON enrollments (school_id, learner_id)
    WHERE status IN ('applicant','admitted','active','suspended','transfer_pending');
