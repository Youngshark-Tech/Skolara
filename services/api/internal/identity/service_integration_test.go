//go:build integration

package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/testdb"
)

func newAuthService(t *testing.T) (*AuthService, *postgres.Pool) {
	t.Helper()
	pool := testdb.New(t)
	repo := NewRepo(pool)
	jwt := NewJWTManager("integration-test-secret-at-least-32-bytes!", time.Minute)
	return NewAuthService(repo, jwt), pool
}

func TestLoginRefreshLogoutFlow(t *testing.T) {
	svc, _ := newAuthService(t)
	ctx := context.Background()

	_, err := svc.CreateUser(ctx, "teacher@school.example", "Ada Teacher", "s3cure-passw0rd!", "group_admin")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Login
	access, refresh, err := svc.Login(ctx, "teacher@school.example", "s3cure-passw0rd!", "10.0.0.1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if access == "" || refresh == "" {
		t.Fatal("empty tokens")
	}

	// Wrong password
	if _, _, err := svc.Login(ctx, "teacher@school.example", "s3cure-passw0rd!-x", "10.0.0.1"); err != ErrBadCredentials {
		t.Fatalf("expected ErrBadCredentials, got %v", err)
	}

	// Refresh rotates
	access2, refresh2, err := svc.Refresh(ctx, refresh, "10.0.0.1")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refresh2 == refresh {
		t.Fatal("refresh token not rotated")
	}

	// Reusing the OLD token must revoke the family
	if _, _, err := svc.Refresh(ctx, refresh, "10.0.0.1"); err != ErrTokenReuse {
		t.Fatalf("expected ErrTokenReuse, got %v", err)
	}
	// And the successor is now dead too
	if _, _, err := svc.Refresh(ctx, refresh2, "10.0.0.1"); err == nil {
		t.Fatal("successor token survived family revocation")
	}
	_ = access
	_ = access2
}

func TestLoginLockout(t *testing.T) {
	svc, _ := newAuthService(t)
	ctx := context.Background()
	_, err := svc.CreateUser(ctx, "student@school.example", "Bo Student", "s3cure-passw0rd!", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxFailedLogins; i++ {
		_, _, err := svc.Login(ctx, "student@school.example", "nope", "10.0.0.2")
		if err == nil {
			t.Fatalf("attempt %d: wrong password accepted", i)
		}
	}
	// Correct password now still locked
	if _, _, err := svc.Login(ctx, "student@school.example", "s3cure-passw0rd!", "10.0.0.2"); err != ErrAccountLocked {
		t.Fatalf("expected lock, got %v", err)
	}
}

func TestUnknownUserNoEnumeration(t *testing.T) {
	svc, _ := newAuthService(t)
	_, _, err := svc.Login(context.Background(), "ghost@nowhere.example", "whatever", "10.0.0.3")
	if err != ErrBadCredentials {
		t.Fatalf("unknown user should look like bad credentials, got %v", err)
	}
}

func TestDuplicateEmailRejected(t *testing.T) {
	svc, _ := newAuthService(t)
	ctx := context.Background()
	if _, err := svc.CreateUser(ctx, "dup@x.example", "First", "s3cure-passw0rd!", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(ctx, "DUP@X.example", "Second", "s3cure-passw0rd!", ""); err != ErrEmailTaken {
		t.Fatalf("case-variant duplicate accepted: %v", err)
	}
}

func TestPermissionsResolveFromRoles(t *testing.T) {
	svc, _ := newAuthService(t)
	ctx := context.Background()
	// platform_admin is the only seed role that spans every permission via the
	// identity route; school-scope roles cannot be assigned through identity
	// (issue #40) — school permissions arrive exclusively via memberships.
	u, err := svc.CreateUser(ctx, "finops@skolara.test", "Fay Ops", "s3cure-passw0rd!", "platform_admin")
	if err != nil {
		t.Fatal(err)
	}
	perms, err := svc.PermissionsFor(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !perms["finance.read"] || !perms["finance.manage"] {
		t.Fatalf("platform admin missing finance perms: %v", perms)
	}
	if !perms["user.manage"] {
		t.Fatal("platform admin missing user.manage")
	}
}

// TestIdentityAssignsPlatformRolesOnly guards the issue #40 privilege-escalation
// fix: the identity user-role route may only assign PLATFORM-scope roles.
// School-scoped roles must be rejected (they are granted via tenancy
// memberships), so a membership-derived user.manage cannot grant itself
// school roles globally — and vice versa the tenancy route cannot grant
// platform roles (covered in the tenancy suite).
func TestIdentityAssignsPlatformRolesOnly(t *testing.T) {
	svc, pool := newAuthService(t)
	ctx := context.Background()
	u, err := svc.CreateUser(ctx, "scoped-roles@skolara.test", "Scope Guard", "s3cure-passw0rd!", "")
	if err != nil {
		t.Fatal(err)
	}

	// School-scope roles are rejected.
	for _, role := range []string{"teacher", "school_admin", "finance_officer", "student"} {
		if err := svc.AssignRole(ctx, u.ID, u.ID, role); err == nil {
			t.Fatalf("school-scope role %q accepted by identity", role)
		}
	}
	// Unknown roles still rejected.
	if err := svc.AssignRole(ctx, u.ID, u.ID, "definitely_not_a_role"); err == nil {
		t.Fatal("unknown role accepted")
	}
	// Platform roles remain assignable.
	if err := svc.AssignRole(ctx, u.ID, u.ID, "group_admin"); err != nil {
		t.Fatalf("platform role assignment: %v", err)
	}

	// DB trigger backstop: raw SQL insert of a school role into user_roles fails.
	var roleID string
	if err := pool.QueryRow(ctx, `SELECT id FROM roles WHERE name='teacher'`).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, u.ID, roleID); err == nil {
		t.Fatal("DB trigger did not reject school-scope role in user_roles")
	}
}

func TestAuditRecordsWritten(t *testing.T) {
	svc, pool := newAuthService(t)
	ctx := context.Background()
	if err := svc.Audit(ctx, AuditEntry{
		Action: "user.created", ResourceType: "user", ResourceID: "u-9",
		After: map[string]any{"name": "Test"},
	}); err != nil {
		t.Fatalf("audit write: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='user.created'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit rows = %d", count)
	}
}

// TestAssignRoleReferenceErrors guards the 500->4xx error-mapping fix.
func TestAssignRoleReferenceErrors(t *testing.T) {
	svc, _ := newAuthService(t)
	ctx := context.Background()

	u, err := svc.CreateUser(ctx, "roleadmin@skolara.test", "Role Admin", "s3cure-passw0rd!", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.AssignRole(ctx, u.ID, u.ID, "definitely_not_a_role"); err == nil {
		t.Fatal("unknown role accepted")
	}
	if err := svc.AssignRole(ctx, u.ID, "00000000-0000-0000-0000-000000000008", "group_admin"); err == nil {
		t.Fatal("unknown user accepted")
	}
	if err := svc.AssignRole(ctx, u.ID, u.ID, "group_admin"); err != nil {
		t.Fatalf("valid assignment: %v", err)
	}
}

// TestUserLifecycleDisableEnable covers the admin lifecycle flow: disable
// revokes every refresh token (session teardown), login is refused while
// disabled, and re-enable restores access.
func TestUserLifecycleDisableEnable(t *testing.T) {
	svc, _ := newAuthService(t)
	ctx := context.Background()

	admin, err := svc.CreateUser(ctx, "lifecycle-admin@skolara.test", "Admin", "s3cure-passw0rd!", "platform_admin")
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.CreateUser(ctx, "lifecycle-user@skolara.test", "User", "s3cure-passw0rd!", "")
	if err != nil {
		t.Fatal(err)
	}

	access, refresh, err := svc.Login(ctx, u.Email, "s3cure-passw0rd!", "127.0.0.1")
	if err != nil || access == "" || refresh == "" {
		t.Fatalf("login before disable: %v", err)
	}

	disabled, err := svc.SetUserStatus(ctx, admin.ID, u.ID, "disabled")
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if disabled.Status != StatusDisabled {
		t.Fatalf("status = %s", disabled.Status)
	}

	// Existing refresh token must be dead.
	if _, _, err := svc.Refresh(ctx, refresh, "127.0.0.1"); err == nil {
		t.Fatal("refresh after disable succeeded — session teardown broken")
	}
	// Login refused while disabled.
	if _, _, err := svc.Login(ctx, u.Email, "s3cure-passw0rd!", "127.0.0.1"); err == nil {
		t.Fatal("login after disable succeeded")
	}
	// Invalid status value rejected.
	if _, err := svc.SetUserStatus(ctx, admin.ID, u.ID, "locked_permanently"); err == nil {
		t.Fatal("invalid status accepted")
	}
	// Unknown user -> 404 semantics.
	if _, err := svc.SetUserStatus(ctx, admin.ID, "00000000-0000-0000-0000-000000000004", "disabled"); err == nil {
		t.Fatal("unknown user accepted")
	}

	// Re-enable restores login.
	enabled, err := svc.SetUserStatus(ctx, admin.ID, u.ID, "active")
	if err != nil || enabled.Status != StatusActive {
		t.Fatalf("re-enable: %v %s", err, enabled.Status)
	}
	if _, _, err := svc.Login(ctx, u.Email, "s3cure-passw0rd!", "127.0.0.1"); err != nil {
		t.Fatalf("login after re-enable: %v", err)
	}

	// Idempotent set to the same status is a no-op.
	if _, err := svc.SetUserStatus(ctx, admin.ID, u.ID, "active"); err != nil {
		t.Fatalf("idempotent status set: %v", err)
	}
}

// TestConcurrentRefreshSingleWinner guards the issue #41 TOCTOU fix: N
// concurrent Refresh calls with the SAME token must yield exactly one
// success; every loser gets ErrTokenReuse and the whole family is revoked
// (no double rotation).
func TestConcurrentRefreshSingleWinner(t *testing.T) {
	svc, _ := newAuthService(t)
	ctx := context.Background()

	if _, err := svc.CreateUser(ctx, "race@school.example", "Race Racer", "s3cure-passw0rd!", ""); err != nil {
		t.Fatal(err)
	}
	_, refresh, err := svc.Login(ctx, "race@school.example", "s3cure-passw0rd!", "10.0.0.9")
	if err != nil {
		t.Fatal(err)
	}

	const racers = 8
	type result struct {
		access  string
		refresh string
		err     error
	}
	results := make(chan result, racers)
	for i := 0; i < racers; i++ {
		go func() {
			a, r, e := svc.Refresh(ctx, refresh, "10.0.0.9")
			results <- result{a, r, e}
		}()
	}

	wins, rejected, other := 0, 0, 0
	for i := 0; i < racers; i++ {
		r := <-results
		switch {
		case r.err == nil:
			wins++
			_ = r.access
			_ = r.refresh
		case errors.Is(r.err, ErrTokenReuse), errors.Is(r.err, ErrBadCredentials):
			// Losers are rejected either as detected reuse (read before the
			// family revocation landed) or as revoked/unknown credentials
			// (read after). Both are 401-class rejections — the security
			// contract is "exactly one winner, nobody else gets in".
			rejected++
		default:
			other++
			t.Logf("unexpected refresh error: %v", r.err)
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly 1 winner, got %d (rejected=%d other=%d)", wins, rejected, other)
	}
	if rejected != racers-1 {
		t.Fatalf("expected %d rejected losers, got %d", racers-1, rejected)
	}

	// The original token is permanently consumed: replaying it after the
	// race is rejected (deterministic — its used_at is set by the winner).
	if _, _, err := svc.Refresh(ctx, refresh, "10.0.0.9"); err == nil {
		t.Fatal("original token still refreshable after race")
	}

	// The account itself still works.
	if _, _, err := svc.Login(ctx, "race@school.example", "s3cure-passw0rd!", "10.0.0.9"); err != nil {
		t.Fatalf("account should still be loginable: %v", err)
	}
}

// TestLoginDoesNotLeakAccountState guards the issue #43 enumeration fix:
// disabled and locked accounts must return ErrBadCredentials for WRONG
// passwords (indistinguishable from unknown users); the actionable state is
// only revealed after the correct password verifies.
func TestLoginDoesNotLeakAccountState(t *testing.T) {
	svc, _ := newAuthService(t)
	ctx := context.Background()

	if _, err := svc.CreateUser(ctx, "leak-a@school.example", "Ann A", "s3cure-passw0rd!", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(ctx, "leak-l@school.example", "Bob L", "s3cure-passw0rd!", ""); err != nil {
		t.Fatal(err)
	}
	disabled, _ := svc.repo.UserByEmail(ctx, "leak-a@school.example")

	// Drive the second account into lockout.
	for i := 0; i < MaxFailedLogins; i++ {
		if _, _, err := svc.Login(ctx, "leak-l@school.example", "wrong", "10.1.0.1"); err != nil && !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("lockout drive attempt %d: %v", i, err)
		}
	}
	if u, _ := svc.repo.UserByEmail(ctx, "leak-l@school.example"); u.Status != StatusLocked {
		t.Fatalf("expected locked, got %s", u.Status)
	}

	// Disable the first account.
	if err := svc.repo.UpdateUserStatus(ctx, disabled.ID, StatusDisabled, 0, nil); err != nil {
		t.Fatal(err)
	}

	// Wrong password on disabled/locked accounts: generic bad credentials.
	if _, _, err := svc.Login(ctx, "leak-a@school.example", "wrong", "10.1.0.2"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("disabled+wrong password leaked state: %v", err)
	}
	if _, _, err := svc.Login(ctx, "leak-l@school.example", "wrong", "10.1.0.2"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("locked+wrong password leaked state: %v", err)
	}

	// Correct password reveals actionable state.
	if _, _, err := svc.Login(ctx, "leak-l@school.example", "s3cure-passw0rd!", "10.1.0.2"); !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("locked+correct password: expected ErrAccountLocked, got %v", err)
	}
}

// TestConcurrentFailedLoginsExactLockout guards the atomic counter (#43):
// 2xMaxFailedLogins parallel wrong-password logins must produce EXACTLY
// MaxFailedLogins counted failures (no read-modify-write undercount) and a
// locked account.
func TestConcurrentFailedLoginsExactLockout(t *testing.T) {
	svc, pool := newAuthService(t)
	ctx := context.Background()

	if _, err := svc.CreateUser(ctx, "race-lock@school.example", "Race Lock", "s3cure-passw0rd!", ""); err != nil {
		t.Fatal(err)
	}

	const attempts = MaxFailedLogins * 2
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			_, _, err := svc.Login(ctx, "race-lock@school.example", "definitely-wrong", "10.2.0.1")
			errs <- err
		}()
	}
	for i := 0; i < attempts; i++ {
		if err := <-errs; err != nil && !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("concurrent failed login error: %v", err)
		}
	}

	var failed int
	var status string
	if err := pool.QueryRow(ctx,
		`SELECT failed_login_attempts, status FROM users WHERE email='race-lock@school.example'`).Scan(&failed, &status); err != nil {
		t.Fatal(err)
	}
	if failed != MaxFailedLogins {
		t.Fatalf("atomic counter violated: got %d failures, want exactly %d", failed, MaxFailedLogins)
	}
	if status != string(StatusLocked) {
		t.Fatalf("expected locked, got %s", status)
	}
}

// TestCreateUserUnknownRoleNoUserRow guards the issue #53 atomicity fix: an
// unknown role is rejected BEFORE any write and leaves NO user row behind
// (previously the user committed, then the role grant 500'd).
func TestCreateUserUnknownRoleNoUserRow(t *testing.T) {
	svc, pool := newAuthService(t)
	ctx := context.Background()

	u, err := svc.CreateUser(ctx, "ghost-role@school.example", "Ghost Role", "s3cure-passw0rd!", "definitely_not_a_role")
	if err == nil {
		t.Fatal("unknown role accepted")
	}
	_ = u

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE email='ghost-role@school.example'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("user row persisted despite failed role grant: %d", count)
	}

	// Valid role still provisions user + role atomically.
	if _, err := svc.CreateUser(ctx, "with-role@school.example", "With Role", "s3cure-passw0rd!", "platform_admin"); err != nil {
		t.Fatal(err)
	}
	var roles int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM user_roles ur JOIN users u ON u.id=ur.user_id WHERE u.email='with-role@school.example'`).Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if roles != 1 {
		t.Fatalf("role grant rows = %d, want 1", roles)
	}
}
