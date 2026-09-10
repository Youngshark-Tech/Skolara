-- 20260909000010_role_scope.up.sql
-- Issue #40: roles gain an explicit scope so platform-level roles can never
-- be granted as school memberships (privilege-escalation chain) and
-- school-level roles can never be assigned through the platform user-role
-- route. 'platform' roles are managed by identity (user_roles); 'school'
-- roles are managed by tenancy (school_memberships.role).
ALTER TABLE roles
    ADD COLUMN scope TEXT NOT NULL DEFAULT 'platform'
    CHECK (scope IN ('platform', 'school'));

-- School-level roles: granted only via school memberships.
UPDATE roles SET scope = 'school' WHERE name IN (
    'school_admin', 'principal', 'deputy_principal', 'academic_admin',
    'finance_officer', 'teacher', 'tutor', 'class_teacher', 'guardian',
    'student', 'support_staff'
);

-- Platform-level roles (platform_admin, group_admin) keep scope='platform'.

-- DB-level enforcement (defense in depth — the service-layer RoleExists
-- checks are necessary but not sufficient):
--   user_roles may only reference platform-scope roles;
--   school_memberships.role may only reference school-scope roles.
CREATE OR REPLACE FUNCTION enforce_role_scope() RETURNS trigger AS $$
DECLARE
    r_scope TEXT;
BEGIN
    IF TG_TABLE_NAME = 'user_roles' THEN
        SELECT scope INTO r_scope FROM roles WHERE id = NEW.role_id;
        IF r_scope IS DISTINCT FROM 'platform' THEN
            RAISE EXCEPTION 'user_roles may only reference platform-scope roles (got %)', COALESCE(r_scope, 'unknown');
        END IF;
    ELSIF TG_TABLE_NAME = 'school_memberships' THEN
        SELECT scope INTO r_scope FROM roles WHERE id = NEW.role;
        IF r_scope IS DISTINCT FROM 'school' THEN
            RAISE EXCEPTION 'school_memberships may only reference school-scope roles (got %)', COALESCE(r_scope, 'unknown');
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER user_roles_platform_scope
    BEFORE INSERT OR UPDATE ON user_roles
    FOR EACH ROW EXECUTE FUNCTION enforce_role_scope();

CREATE TRIGGER school_memberships_school_scope
    BEFORE INSERT OR UPDATE ON school_memberships
    FOR EACH ROW EXECUTE FUNCTION enforce_role_scope();
