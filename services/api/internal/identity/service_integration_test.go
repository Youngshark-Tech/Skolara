//go:build integration

package identity

import (
	"context"
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

	_, err := svc.CreateUser(ctx, "teacher@school.example", "Ada Teacher", "s3cure-passw0rd!", "teacher")
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
	if _, _, err := svc.Login(ctx, "teacher@school.example", "wrong", "10.0.0.1"); err != ErrBadCredentials {
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
	_, err := svc.CreateUser(ctx, "student@school.example", "Bo Student", "s3cure-passw0rd!", "student")
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
	u, err := svc.CreateUser(ctx, "fin@school.example", "Fay Finance", "s3cure-passw0rd!", "finance_officer")
	if err != nil {
		t.Fatal(err)
	}
	perms, err := svc.PermissionsFor(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !perms["finance.read"] || !perms["finance.manage"] {
		t.Fatalf("finance officer missing finance perms: %v", perms)
	}
	if perms["user.manage"] {
		t.Fatal("finance officer has user.manage")
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
	if err := svc.AssignRole(ctx, u.ID, "00000000-0000-0000-0000-000000000008", "teacher"); err == nil {
		t.Fatal("unknown user accepted")
	}
	if err := svc.AssignRole(ctx, u.ID, u.ID, "teacher"); err != nil {
		t.Fatalf("valid assignment: %v", err)
	}
}
