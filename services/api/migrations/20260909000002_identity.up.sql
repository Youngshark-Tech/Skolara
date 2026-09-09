-- Identity bounded context: users, credentials, RBAC matrix, sessions, audit.
-- Roles/permissions are seeded here; school-scoped membership binding lives in tenancy.

CREATE TABLE users (
    id            UUID PRIMARY KEY,
    email         TEXT NOT NULL,
    name          TEXT NOT NULL,
    password_hash TEXT NOT NULL,             -- argon2id encoded hash
    status        TEXT NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active','disabled','locked')),
    failed_login_attempts INT NOT NULL DEFAULT 0,
    locked_until  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

CREATE TABLE roles (
    id         SMALLSERIAL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,        -- platform_admin, school_admin, ...
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE permissions (
    id   SMALLSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE               -- user.read, user.manage, ...
);

CREATE TABLE role_permissions (
    role_id       SMALLINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id SMALLINT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

-- user_roles stores PLATFORM-WIDE role grants.
-- School-scoped role binding lives in tenancy.school_memberships (migration 000003):
-- identity owns accounts and platform roles; tenancy owns membership contexts.
CREATE TABLE user_roles (
    user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id  SMALLINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE refresh_tokens (
    id          UUID PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL,               -- sha256 of opaque token
    family_id   UUID NOT NULL,               -- rotation chain (reuse detection)
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_ip  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);
CREATE UNIQUE INDEX refresh_tokens_hash_key ON refresh_tokens (token_hash);

CREATE TABLE audit_logs (
    id             BIGSERIAL PRIMARY KEY,
    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id       UUID,
    action         TEXT NOT NULL,            -- e.g. auth.login, user.created
    resource_type  TEXT NOT NULL,
    resource_id    TEXT NOT NULL,
    school_id      UUID,                     -- tenant scope when applicable
    before_state   JSONB,
    after_state    JSONB,
    request_id     TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    detail         JSONB
);
CREATE INDEX audit_logs_actor_time_idx ON audit_logs (actor_id, occurred_at DESC);
CREATE INDEX audit_logs_resource_idx ON audit_logs (resource_type, resource_id);
CREATE INDEX audit_logs_school_time_idx ON audit_logs (school_id, occurred_at DESC);

-- RBAC seed: roles -----------------------------------------------------------
INSERT INTO roles (name, description) VALUES
  ('platform_admin', 'Platform operator — full control across schools'),
  ('group_admin',    'Education group administrator'),
  ('school_admin',   'School administrator'),
  ('principal',      'School principal'),
  ('deputy_principal','Deputy principal'),
  ('academic_admin', 'Academic operations administrator'),
  ('finance_officer','Finance officer'),
  ('teacher',        'Teacher'),
  ('tutor',          'Contract tutor'),
  ('class_teacher',  'Class teacher'),
  ('guardian',       'Parent/guardian'),
  ('student',        'Student'),
  ('support_staff',  'Support staff member')
ON CONFLICT (name) DO NOTHING;

-- RBAC seed: permissions ------------------------------------------------------
INSERT INTO permissions (name) VALUES
  ('user.read'), ('user.manage'),
  ('school.read'), ('school.manage'),
  ('student.read'), ('student.manage'),
  ('academics.read'), ('academics.manage'),
  ('attendance.read'), ('attendance.record'),
  ('assignment.read'), ('assignment.manage'), ('assignment.submit'),
  ('finance.read'), ('finance.manage'),
  ('audit.read')
ON CONFLICT (name) DO NOTHING;

-- Platform admin: everything
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p WHERE r.name = 'platform_admin'
ON CONFLICT DO NOTHING;

-- school_admin: school-scoped management
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p
  ON p.name IN ('user.read','user.manage','school.read','student.read','student.manage',
                'academics.read','academics.manage','attendance.read','attendance.record',
                'assignment.read','assignment.manage','finance.read','finance.manage','audit.read')
WHERE r.name = 'school_admin'
ON CONFLICT DO NOTHING;

-- principal: read-mostly + academic management
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p
  ON p.name IN ('user.read','school.read','student.read','student.manage',
                'academics.read','academics.manage','attendance.read','attendance.record',
                'assignment.read','assignment.manage','finance.read','audit.read')
WHERE r.name = 'principal'
ON CONFLICT DO NOTHING;

-- academic_admin
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p
  ON p.name IN ('school.read','student.read','academics.read','academics.manage',
                'attendance.read','attendance.record','assignment.read','assignment.manage')
WHERE r.name = 'academic_admin'
ON CONFLICT DO NOTHING;

-- finance_officer
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p
  ON p.name IN ('school.read','student.read','finance.read','finance.manage')
WHERE r.name = 'finance_officer'
ON CONFLICT DO NOTHING;

-- teacher
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p
  ON p.name IN ('school.read','student.read','academics.read','attendance.read',
                'attendance.record','assignment.read','assignment.manage')
WHERE r.name = 'teacher'
ON CONFLICT DO NOTHING;

-- class_teacher: teacher + student.manage for their class (app-scoped later)
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p
  ON p.name IN ('school.read','student.read','academics.read','attendance.read',
                'attendance.record','assignment.read','assignment.manage','student.manage')
WHERE r.name = 'class_teacher'
ON CONFLICT DO NOTHING;

-- tutor
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p
  ON p.name IN ('academics.read','attendance.read','assignment.read')
WHERE r.name = 'tutor'
ON CONFLICT DO NOTHING;

-- guardian
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p
  ON p.name IN ('student.read','attendance.read','assignment.read','finance.read')
WHERE r.name = 'guardian'
ON CONFLICT DO NOTHING;

-- student
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p
  ON p.name IN ('assignment.read','assignment.submit','attendance.read')
WHERE r.name = 'student'
ON CONFLICT DO NOTHING;

-- support_staff
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p
  ON p.name IN ('school.read','student.read','attendance.read')
WHERE r.name = 'support_staff'
ON CONFLICT DO NOTHING;

-- group_admin: school-level permissions at group scope
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p
  ON p.name IN ('school.read','school.manage','user.read','finance.read','audit.read')
WHERE r.name = 'group_admin'
ON CONFLICT DO NOTHING;
