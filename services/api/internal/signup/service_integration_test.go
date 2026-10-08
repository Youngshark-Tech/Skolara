//go:build integration

package signup

import (
	"context"
	"testing"

	"errors"
	"fmt"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/testdb"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
)

func newService(t *testing.T) (*Service, *identity.AuthService, *postgres.Pool) {
	t.Helper()
	pool := testdb.New(t)
	idRepo := identity.NewRepo(pool)
	authSvc := identity.NewAuthService(idRepo, identity.NewJWTManager("integration-test-secret-at-least-32-bytes!", 60))
	return New(pool, idRepo, tenancy.NewRepo(pool)), authSvc, pool
}

// TestSignupProvisionsWorkspaceAndLogsIn walks the full #130 acceptance path:
// one call provisions user + group + school + membership in ONE transaction,
// the account authenticates, and its authority comes purely from the
// school_admin membership (never a platform role).
func TestSignupProvisionsWorkspaceAndLogsIn(t *testing.T) {
	svc, authSvc, pool := newService(t)
	ctx := context.Background()

	out, err := svc.Signup(ctx, identity.SignupRequest{
		SchoolName: "Green Valley Primary",
		AdminName:  "Ada Founder",
		Email:      "founder@greenvalley.example",
		Password:   "onboard-pass-1",
	})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if out.SchoolCode == "" || len(out.SchoolCode) != 9 || out.SchoolCode[:3] != "SL-" {
		t.Errorf("school code = %q, want SL-XXXXXX", out.SchoolCode)
	}

	// The account authenticates through the standard login flow.
	access, _, err := authSvc.Login(ctx, out.Email, "onboard-pass-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login after signup: %v", err)
	}
	if access == "" {
		t.Fatal("empty access token after signup+login")
	}

	// Authority comes ONLY from the school_admin membership (no platform
	// role): identity-side resolution is empty by design; the composition
	// root unions it with tenancy membership permissions.
	platformPerms, err := authSvc.PermissionsFor(ctx, out.UserID)
	if err != nil {
		t.Fatalf("platform permissions: %v", err)
	}
	if len(platformPerms) != 0 {
		t.Errorf("signup admin has platform permissions (%d), want none", len(platformPerms))
	}
	tenSvc := tenancy.NewService(tenancy.NewRepo(pool), pool)
	perms, err := tenSvc.PermissionsFor(ctx, out.UserID)
	if err != nil {
		t.Fatalf("membership permissions: %v", err)
	}
	if len(perms) == 0 {
		t.Error("school_admin membership yielded no permissions")
	}
	platform, err := tenSvc.IsPlatformAdmin(ctx, out.UserID)
	if err != nil {
		t.Fatalf("platform check: %v", err)
	}
	if platform {
		t.Error("signup admin must NOT be a platform admin")
	}
}

// TestSignupRejectsDuplicateEmail pins the 409 path (pre-check AND the
// raced-insert unique key inside the tx).
func TestSignupRejectsDuplicateEmail(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	in := identity.SignupRequest{
		SchoolName: "Dup Test Academy",
		AdminName:  "First One",
		Email:      "dup@example.com",
		Password:   "password-123",
	}
	if _, err := svc.Signup(ctx, in); err != nil {
		t.Fatalf("first signup: %v", err)
	}
	in.AdminName = "Second One"
	_, err := svc.Signup(ctx, in)
	if err == nil {
		t.Fatal("duplicate email signup succeeded, want error")
	}
	if !errors.Is(err, identity.ErrEmailTaken) {
		t.Errorf("err = %v, want ErrEmailTaken", err)
	}
}

// TestSignupValidationPaths covers the 400 arms: short password, bad email,
// too-short school name.
func TestSignupValidationPaths(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	cases := []struct {
		name string
		in   identity.SignupRequest
	}{
		{"short password", identity.SignupRequest{SchoolName: "School", AdminName: "A", Email: "a@b.example", Password: "short"}},
		{"bad email", identity.SignupRequest{SchoolName: "School", AdminName: "A", Email: "not-an-email", Password: "longenough1"}},
		{"short school name", identity.SignupRequest{SchoolName: "S", AdminName: "A", Email: "s@b.example", Password: "longenough1"}},
	}
	for _, tc := range cases {
		_, err := svc.Signup(ctx, tc.in)
		if !errors.Is(err, identity.ErrValidation) {
			t.Errorf("%s: err = %v (%v), want ErrValidation", tc.name, err, fmt.Sprintf("%T", err))
		}
	}
}
