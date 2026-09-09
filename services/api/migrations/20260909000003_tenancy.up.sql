-- Tenancy bounded context: education groups -> schools -> campuses,
-- plus school memberships binding users to schools with school-scoped roles.
-- School is the TENANT ROOT: every tenant-scoped domain table carries school_id.

CREATE TABLE education_groups (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE schools (
    id         UUID PRIMARY KEY,
    code       TEXT NOT NULL,
    name       TEXT NOT NULL,
    group_id   UUID REFERENCES education_groups(id) ON DELETE SET NULL,
    status     TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive','archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX schools_code_key ON schools (lower(code));

CREATE TABLE campuses (
    id         UUID PRIMARY KEY,
    school_id  UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    code       TEXT NOT NULL,
    name       TEXT NOT NULL,
    location   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX campuses_school_code_key ON campuses (school_id, lower(code));

-- School-scoped role binding (complements identity.user_roles platform grants).
CREATE TABLE school_memberships (
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    school_id UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    role      SMALLINT NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    status    TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, school_id, role)
);
CREATE INDEX school_memberships_school_idx ON school_memberships (school_id);

-- Invite codes for school onboarding (admin-issued, single-use or capped).
CREATE TABLE school_join_codes (
    id         UUID PRIMARY KEY,
    school_id  UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    code       TEXT NOT NULL,
    role       SMALLINT NOT NULL REFERENCES roles(id),
    max_uses   INT NOT NULL DEFAULT 1 CHECK (max_uses > 0),
    used_count INT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX school_join_codes_code_key ON school_join_codes (code);
