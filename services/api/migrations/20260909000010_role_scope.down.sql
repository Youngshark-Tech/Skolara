-- 20260909000010_role_scope.down.sql
DROP TRIGGER IF EXISTS user_roles_platform_scope ON user_roles;
DROP TRIGGER IF EXISTS school_memberships_school_scope ON school_memberships;
DROP FUNCTION IF EXISTS enforce_role_scope();
ALTER TABLE roles DROP COLUMN IF EXISTS scope;
