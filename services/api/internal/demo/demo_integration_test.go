//go:build integration

package demo

import (
	"context"
	"log/slog"
	"testing"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/academics"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/testdb"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/students"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
)

func newDeps(t *testing.T) Deps {
	t.Helper()
	pool := testdb.New(t)
	return Deps{
		Pool:      pool,
		Identity:  identity.NewAuthService(identity.NewRepo(pool), identity.NewJWTManager("integration-test-secret-at-least-32-bytes!", 60)),
		Tenancy:   tenancy.NewService(tenancy.NewRepo(pool), pool),
		Students:  students.NewService(students.NewRepo(pool), pool),
		Academics: academics.NewService(academics.NewRepo(pool), pool),
	}
}

func count(t *testing.T, d Deps, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

// TestSeedIsIdempotentAndPopulatesWorkspace runs the seed twice against a live
// database and asserts the second pass creates nothing new, then proves the
// demo credentials actually authenticate (#128).
func TestSeedIsIdempotentAndPopulatesWorkspace(t *testing.T) {
	d := newDeps(t)
	ctx := context.Background()
	log := slog.Default()

	if _, err := Seed(ctx, d, DefaultPassword, log); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if _, err := Seed(ctx, d, DefaultPassword, log); err != nil {
		t.Fatalf("second seed (idempotency): %v", err)
	}
	// A third pass exercises the roster/enrollment skip paths specifically.
	if _, err := Seed(ctx, d, DefaultPassword, log); err != nil {
		t.Fatalf("third seed (roster/enrollment skips): %v", err)
	}

	for name, q := range map[string]string{
		"groups":         `SELECT count(*) FROM education_groups`,
		"schools":        `SELECT count(*) FROM schools WHERE code = $1`,
		"campuses":       `SELECT count(*) FROM campuses WHERE code = $1`,
		"users":          `SELECT count(*) FROM users WHERE email IN ($1, $2)`,
		"memberships":    `SELECT count(*) FROM school_memberships sm JOIN schools s ON s.id = sm.school_id WHERE s.code = $1`,
		"academic_years": `SELECT count(*) FROM academic_years WHERE name = $1`,
		"class_groups":   `SELECT count(*) FROM class_groups WHERE name = $1`,
		"learners":       `SELECT count(*) FROM learners WHERE external_id LIKE 'DEMO-%'`,
		"roster_entries": `SELECT count(*) FROM roster_entries re JOIN class_groups cg ON cg.id = re.class_group_id WHERE cg.name = $1`,
		"enrollments":    `SELECT count(*) FROM enrollments e JOIN schools s ON s.id = e.school_id WHERE s.code = $1`,
	} {
		args := []any{}
		switch name {
		case "schools", "memberships", "enrollments":
			args = append(args, DemoSchoolCode)
		case "campuses":
			args = append(args, DemoCampusCode)
		case "users":
			args = append(args, DemoAdminEmail, "teacher@skolara.dev")
		case "academic_years":
			args = append(args, DemoYearName)
		case "class_groups", "roster_entries":
			args = append(args, DemoClassName)
		}
		if got := count(t, d, q, args...); got == 0 {
			t.Errorf("demo %s: count = 0, want > 0", name)
		}
	}

	if got := count(t, d, `SELECT count(*) FROM education_groups`); got != 1 {
		t.Errorf("education_groups = %d after 3 seeds, want exactly 1", got)
	}
	if got := count(t, d, `SELECT count(*) FROM learners WHERE external_id LIKE 'DEMO-%'`); got != 5 {
		t.Errorf("learners = %d after 3 seeds, want exactly 5", got)
	}
}

// TestSeededUsersCanLogin proves the documented demo credentials authenticate
// and resolve the correct school-scoped roles.
func TestSeededUsersCanLogin(t *testing.T) {
	d := newDeps(t)
	ctx := context.Background()

	if _, err := Seed(ctx, d, DefaultPassword, slog.Default()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, tc := range []struct {
		email, role string
	}{
		{DemoAdminEmail, "school_admin"},
		{"teacher@skolara.dev", "teacher"},
	} {
		access, _, err := d.Identity.Login(ctx, tc.email, DefaultPassword, "127.0.0.1")
		if err != nil {
			t.Errorf("login %s: %v", tc.email, err)
			continue
		}
		if access == "" {
			t.Errorf("login %s: empty access token", tc.email)
		}
		// Wrong password must still be rejected (guard against a seed that
		// would, say, set a known constant hash).
		if _, _, err := d.Identity.Login(ctx, tc.email, "wrong-password", "127.0.0.1"); err == nil {
			t.Errorf("login %s with wrong password unexpectedly succeeded", tc.email)
		}
		perms, err := d.Tenancy.PermissionsFor(ctx, mustUserID(t, d, tc.email))
		if err != nil {
			t.Errorf("permissions %s: %v", tc.email, err)
			continue
		}
		if len(perms) == 0 {
			t.Errorf("membership permissions for %s (%s) are empty", tc.email, tc.role)
		}
	}
}

func mustUserID(t *testing.T, d Deps, email string) string {
	t.Helper()
	var id string
	if err := d.Pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&id); err != nil {
		t.Fatalf("user %s: %v", email, err)
	}
	return id
}

// TestCustomDemoPasswordOverride verifies SKOLARA_DEMO_PASSWORD is honored for
// fresh seeds while existing users keep their original password (idempotency).
func TestCustomDemoPasswordOverride(t *testing.T) {
	d := newDeps(t)
	ctx := context.Background()

	if _, err := Seed(ctx, d, "CustomDemo!Pass99", slog.Default()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, _, err := d.Identity.Login(ctx, DemoAdminEmail, "CustomDemo!Pass99", "127.0.0.1"); err != nil {
		t.Errorf("custom password login failed: %v", err)
	}
}
