//go:build integration

package tenancy

import (
	"context"
	"testing"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/testdb"
)

func newTenancyFixture(t *testing.T) (*Service, *identity.AuthService, *postgres.Pool) {
	t.Helper()
	pool := testdb.New(t)
	jwt := identity.NewJWTManager("integration-test-secret-at-least-32-bytes!", 1<<30)
	authSvc := identity.NewAuthService(identity.NewRepo(pool), jwt)
	svc := NewService(NewRepo(pool), pool)
	return svc, authSvc, pool
}

func seedUsers(t *testing.T, svc *identity.AuthService, emails ...string) map[string]string {
	t.Helper()
	ids := map[string]string{}
	for _, e := range emails {
		u, err := svc.CreateUser(context.Background(), e, e, "s3cure-passw0rd!", "")
		if err != nil {
			t.Fatalf("seed %s: %v", e, err)
		}
		ids[e] = u.ID
	}
	return ids
}

func TestSchoolCreationAndHierarchy(t *testing.T) {
	svc, authSvc, _ := newTenancyFixture(t)
	ctx := context.Background()
	ids := seedUsers(t, authSvc, "root@skolara.test")
	_ = ids

	// Group
	g := &EducationGroup{ID: newID(), Name: "Greenhill Group"}
	if err := svc.repo.CreateGroup(ctx, g); err != nil {
		t.Fatal(err)
	}

	// Schools
	s1, err := svc.CreateSchool(ctx, "GH-MAIN", "Greenhill Main", g.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if s1.Status != "active" || s1.GroupID == nil {
		t.Fatalf("school fields wrong: %+v", s1)
	}
	s2, err := svc.CreateSchool(ctx, "GH-WEST", "Greenhill West", g.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	// Duplicate code (case-insensitive) rejected
	if _, err := svc.CreateSchool(ctx, "gh-main", "Dup", g.ID, ""); err != ErrCodeTaken {
		t.Fatalf("expected ErrCodeTaken, got %v", err)
	}

	// Campuses
	c, err := svc.CreateCampus(ctx, s1.ID, "CAMPUS-A", "North Campus", "Hill Rd")
	if err != nil {
		t.Fatal(err)
	}
	if c.SchoolID != s1.ID {
		t.Fatalf("campus school mismatch")
	}
	campuses, err := svc.Campuses(ctx, s1.ID)
	if err != nil || len(campuses) != 1 {
		t.Fatalf("campus list: %v %v", campuses, err)
	}

	// Group listing returns both schools
	all, err := svc.ListSchools(ctx, g.ID)
	if err != nil || len(all) != 2 {
		t.Fatalf("group schools: %v %v", all, err)
	}
	_ = s2
}

func TestTenantIsolationMatrix(t *testing.T) {
	svc, authSvc, pool := newTenancyFixture(t)
	ctx := context.Background()
	ids := seedUsers(t, authSvc, "adminA@schoola.test", "adminB@schoolb.test", "plat@skolara.test")

	sA, err := svc.CreateSchool(ctx, "SCH-A", "School A", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sB, err := svc.CreateSchool(ctx, "SCH-B", "School B", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Membership: adminA -> school A only
	if err := svc.AddMember(ctx, sA.ID, ids["adminA@schoola.test"], "school_admin"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddMember(ctx, sB.ID, ids["adminB@schoolb.test"], "school_admin"); err != nil {
		t.Fatal(err)
	}
	// Platform role for the platform user
	if err := authSvc.AssignRole(ctx, "", ids["plat@skolara.test"], "platform_admin"); err != nil {
		t.Fatal(err)
	}

	userA := ids["adminA@schoola.test"]
	_ = ids["adminB@schoolb.test"] // userB exercised via membership status checks

	// 1. A resolves own school: OK
	school, err := svc.ResolveSchoolContext(ctx, userA, sA.ID)
	if err != nil || school.ID != sA.ID {
		t.Fatalf("own school resolution failed: %v %v", school, err)
	}

	// 2. A requests school B context: DENIED
	if _, err := svc.ResolveSchoolContext(ctx, userA, sB.ID); err != ErrNotAMember {
		t.Fatalf("cross-tenant context resolved: %v", err)
	}

	// 3. A with no header + 1 membership: defaults to A
	school, err = svc.ResolveSchoolContext(ctx, userA, "")
	if err != nil || school.ID != sA.ID {
		t.Fatalf("default context failed: %v %v", school, err)
	}

	// 4. Membership status check is the isolation guard
	active, err := svc.repo.HasActiveMembership(ctx, userA, sB.ID)
	if err != nil || active {
		t.Fatal("A has membership in B — isolation broken")
	}

	// 5. Platform admin may access any school explicitly
	school, err = svc.ResolveSchoolContext(ctx, ids["plat@skolara.test"], sB.ID)
	if err != nil || school.ID != sB.ID {
		t.Fatalf("platform admin context failed: %v %v", school, err)
	}

	// 6. Platform admin without explicit school and no membership: no context
	if _, err := svc.ResolveSchoolContext(ctx, ids["plat@skolara.test"], ""); err == nil {
		t.Fatal("platform admin without X-School-ID should have no implicit tenant")
	}

	// 7. Suspended membership grants nothing
	if _, err := pool.Exec(ctx,
		`UPDATE school_memberships SET status='suspended' WHERE school_id=$1 AND user_id=$2`,
		sA.ID, userA); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveSchoolContext(ctx, userA, sA.ID); err != ErrNotAMember {
		t.Fatalf("suspended membership still resolves: %v", err)
	}
}

func TestMembershipScopedRoles(t *testing.T) {
	svc, authSvc, _ := newTenancyFixture(t)
	ctx := context.Background()
	ids := seedUsers(t, authSvc, "teacher1@school.test")

	s, err := svc.CreateSchool(ctx, "SCH-T", "Test School", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AddMember(ctx, s.ID, ids["teacher1@school.test"], "teacher"); err != nil {
		t.Fatal(err)
	}
	members, err := svc.Members(ctx, s.ID)
	if err != nil || len(members) != 1 {
		t.Fatalf("members: %v %v", members, err)
	}
	if members[0].Role != "teacher" {
		t.Fatalf("role = %s", members[0].Role)
	}
}

func TestSchoolCreatedEventOutboxed(t *testing.T) {
	svc, _, pool := newTenancyFixture(t)
	if _, err := svc.CreateSchool(context.Background(), "SCH-EVT", "Event School", "", ""); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM event_outbox WHERE event_type='tenancy.SchoolCreated'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("SchoolCreated events = %d", count)
	}
}

// TestDualRoleMembershipAccessDeterministic guards the audit fix for the
// role-agnostic, non-deterministic access check: a user with one SUSPENDED
// and one ACTIVE membership at the same school must always be granted access
// (any-active-row semantics), while a suspended-only user must always be
// denied — regardless of row order returned by the database.
func TestDualRoleMembershipAccessDeterministic(t *testing.T) {
	svc, authSvc, pool := newTenancyFixture(t)
	ctx := context.Background()

	school, err := svc.CreateSchool(ctx, "DUAL-S", "Dual Role School", "", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateSchool(ctx, "DUAL-T", "Other School", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ids := seedUsers(t, authSvc, "dual@skolara.test", "susp@skolara.test", "out@skolara.test")
	dual, suspOnly, outsider := ids["dual@skolara.test"], ids["susp@skolara.test"], ids["out@skolara.test"]

	// dual: suspended teacher + active school_admin at DUAL-S.
	if err := svc.AddMember(ctx, school.ID, dual, "teacher"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE school_memberships SET status='suspended' WHERE user_id=$1 AND school_id=$2 AND role=(SELECT id FROM roles WHERE name='teacher')`,
		dual, school.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddMember(ctx, school.ID, dual, "school_admin"); err != nil {
		t.Fatal(err)
	}

	// suspOnly: only a suspended membership at DUAL-S.
	if err := svc.AddMember(ctx, school.ID, suspOnly, "teacher"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE school_memberships SET status='suspended' WHERE user_id=$1 AND school_id=$2`,
		suspOnly, school.ID); err != nil {
		t.Fatal(err)
	}

	// outsider: active only at the OTHER school (isolation control).
	if err := svc.AddMember(ctx, other.ID, outsider, "teacher"); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 20; i++ { // repeat: row order must never leak into the decision
		got, err := svc.HasActiveMembership(ctx, dual, school.ID)
		if err != nil || !got {
			t.Fatalf("dual-role user denied on iteration %d: %v", i, err)
		}
		got, err = svc.HasActiveMembership(ctx, suspOnly, school.ID)
		if err != nil || got {
			t.Fatalf("suspended-only user granted on iteration %d: %v", i, err)
		}
		got, err = svc.HasActiveMembership(ctx, outsider, school.ID)
		if err != nil || got {
			t.Fatalf("outsider granted on iteration %d: %v", i, err)
		}
	}
}
