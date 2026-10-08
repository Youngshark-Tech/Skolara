// Package demo seeds an idempotent demo dataset (issue #128) so any fresh
// deployment has working login details and a populated school workspace.
//
// It is enabled ONLY when SKOLARA_DEMO_SEED=true (config.DemoSeed) and runs
// at API startup after migrations, reusing the real domain services so every
// write passes the same validation, guarded-transition, audit, and event
// machinery as production traffic. Re-running the seed (or racing cold
// starts) is safe: every step checks for existing data by a stable natural
// key first, then creates only what is missing.
//
// SECURITY: demo credentials are public knowledge. Never enable this mode on
// a deployment that holds real student data.
package demo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/academics"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/students"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
	"github.com/jackc/pgx/v5"
)

// Stable natural keys for the demo dataset. Emails / codes / names double as
// idempotency keys, so they must not change between releases.
const (
	DemoGroupName   = "Skolara Demo Group"
	DemoSchoolName  = "Riverside High School"
	DemoSchoolCode  = "RVS-001"
	DemoCampusCode  = "MC"
	DemoCampusName  = "Main Campus"
	DemoYearName    = "2026"
	DemoYearStart   = "2026-01-05"
	DemoYearEnd     = "2026-11-20"
	DemoClassName   = "Grade 8 - Blue"
	DemoAdminEmail  = "admin@skolara.dev"
	DemoAdminName   = "Demo School Admin"
	DemoTeacherName = "Demo Teacher"
	// DefaultPassword is the documented demo password used when
	// SKOLARA_DEMO_PASSWORD is not set. Public by design — see DEMO.md.
	DefaultPassword = "SkolaraDemo!2026"
)

var demoLearners = []struct {
	first, middle, last, gender, externalID, dob string
}{
	{"Amina", "Njeri", "Otieno", "female", "DEMO-L001", "2012-03-14"},
	{"Brian", "Kiprop", "Mutai", "male", "DEMO-L002", "2012-07-02"},
	{"Cynthia", "Awuor", "Ochieng", "female", "DEMO-L003", "2011-11-30"},
	{"Daniel", "Mwangi", "Kamau", "male", "DEMO-L004", "2012-01-19"},
	{"Esther", "Wanjiku", "Njoroge", "female", "DEMO-L005", "2012-05-08"},
}

// Deps carries the composed services from the API composition root. The demo
// package writes through the services (not raw SQL) so validations, guarded
// transitions, audit entries, and outbox events all behave exactly like the
// running product. Pool-backed reads are used only for natural-key existence
// checks the service layer does not expose.
type Deps struct {
	Pool      *postgres.Pool
	Identity  *identity.AuthService
	Tenancy   *tenancy.Service
	Students  *students.Service
	Academics *academics.Service
}

// Seed applies the demo dataset. Missing pieces are created; existing pieces
// are left untouched. Returns the demo school ID for observability.
func Seed(ctx context.Context, d Deps, password string, log *slog.Logger) (schoolID string, err error) {
	if password == "" {
		password = DefaultPassword
	}

	// 1. Demo users (created with NO platform-scope role — their power comes
	// purely from school memberships, per the #40 role-scope model).
	adminID, err := ensureUser(ctx, d, DemoAdminEmail, DemoAdminName, password)
	if err != nil {
		return "", fmt.Errorf("demo admin user: %w", err)
	}
	teacherID, err := ensureUser(ctx, d, "teacher@skolara.dev", DemoTeacherName, password)
	if err != nil {
		return "", fmt.Errorf("demo teacher user: %w", err)
	}

	// 2. Education group.
	groupID, err := ensureGroup(ctx, d, adminID)
	if err != nil {
		return "", fmt.Errorf("demo group: %w", err)
	}

	// 3. School + campus.
	schoolID, err = ensureSchool(ctx, d, groupID)
	if err != nil {
		return "", fmt.Errorf("demo school: %w", err)
	}
	if err := ensureCampus(ctx, d, schoolID); err != nil {
		return "", fmt.Errorf("demo campus: %w", err)
	}

	// 4. Memberships (school-scope roles only — DB triggers enforce this).
	if err := ensureMembership(ctx, d, schoolID, adminID, "school_admin"); err != nil {
		return "", fmt.Errorf("demo admin membership: %w", err)
	}
	if err := ensureMembership(ctx, d, schoolID, teacherID, "teacher"); err != nil {
		return "", fmt.Errorf("demo teacher membership: %w", err)
	}

	// 5. Academic year + class group.
	yearID, err := ensureAcademicYear(ctx, d, schoolID)
	if err != nil {
		return "", fmt.Errorf("demo academic year: %w", err)
	}
	classID, err := ensureClassGroup(ctx, d, schoolID, yearID)
	if err != nil {
		return "", fmt.Errorf("demo class group: %w", err)
	}

	// 6. Learners, enrollments, roster — in that order: the academics domain
	// requires learners to be enrolled at the school before they join a class
	// roster ("learner(s) not enrolled at this school" guard in AddRoster).
	learnerIDs, err := ensureLearners(ctx, d, schoolID)
	if err != nil {
		return "", fmt.Errorf("demo learners: %w", err)
	}
	if err := ensureEnrollments(ctx, d, schoolID, classID, yearID, learnerIDs); err != nil {
		return "", fmt.Errorf("demo enrollments: %w", err)
	}
	if err := ensureRoster(ctx, d, schoolID, classID, learnerIDs); err != nil {
		return "", fmt.Errorf("demo roster: %w", err)
	}

	log.Info("demo dataset ready",
		"school_id", schoolID,
		"admin_email", DemoAdminEmail,
		"teacher_email", "teacher@skolara.dev",
	)
	return schoolID, nil
}

// ---------------------------------------------------------------------------
// users

func ensureUser(ctx context.Context, d Deps, email, name, password string) (string, error) {
	u, err := d.Identity.CreateUser(ctx, email, name, password, "")
	if err == nil {
		return u.ID, nil
	}
	if errors.Is(err, identity.ErrEmailTaken) {
		var id string
		if err := d.Pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&id); err != nil {
			return "", err
		}
		return id, nil
	}
	return "", err
}

// ---------------------------------------------------------------------------
// tenancy

func ensureGroup(ctx context.Context, d Deps, actorID string) (string, error) {
	groups, err := d.Tenancy.Groups(ctx)
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		if g.Name == DemoGroupName {
			return g.ID, nil
		}
	}
	g, err := d.Tenancy.CreateGroup(ctx, DemoGroupName, actorID)
	if err != nil {
		return "", err
	}
	return g.ID, nil
}

func ensureSchool(ctx context.Context, d Deps, groupID string) (string, error) {
	var id string
	err := d.Pool.QueryRow(ctx, `SELECT id FROM schools WHERE code = $1`, DemoSchoolCode).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	s, err := d.Tenancy.CreateSchool(ctx, DemoSchoolCode, DemoSchoolName, groupID, "")
	if err != nil {
		return "", err
	}
	return s.ID, nil
}

func ensureCampus(ctx context.Context, d Deps, schoolID string) error {
	campuses, _, err := d.Tenancy.Campuses(ctx, schoolID, 100, 0)
	if err != nil {
		return err
	}
	for _, c := range campuses {
		if c.Code == DemoCampusCode {
			return nil
		}
	}
	_, err = d.Tenancy.CreateCampus(ctx, schoolID, DemoCampusCode, DemoCampusName, "1 Riverside Drive")
	return err
}

func ensureMembership(ctx context.Context, d Deps, schoolID, userID, role string) error {
	ok, err := d.Tenancy.HasActiveMembership(ctx, userID, schoolID)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return d.Tenancy.AddMember(ctx, schoolID, userID, role)
}

// ---------------------------------------------------------------------------
// academics

func ensureAcademicYear(ctx context.Context, d Deps, schoolID string) (string, error) {
	years, err := d.Academics.AcademicYears(ctx, schoolID)
	if err != nil {
		return "", err
	}
	for _, y := range years {
		if y.Name == DemoYearName {
			return y.ID, nil
		}
	}
	y, err := d.Academics.CreateAcademicYear(ctx, schoolID, DemoYearName, DemoYearStart, DemoYearEnd)
	if err != nil {
		return "", err
	}
	return y.ID, nil
}

func ensureClassGroup(ctx context.Context, d Deps, schoolID, yearID string) (string, error) {
	groups, err := d.Academics.ClassGroups(ctx, schoolID, yearID)
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		if g.Name == DemoClassName {
			return g.ID, nil
		}
	}
	g, err := d.Academics.CreateClassGroup(ctx, schoolID, yearID, DemoClassName)
	if err != nil {
		return "", err
	}
	return g.ID, nil
}

// ---------------------------------------------------------------------------
// students

func ensureLearners(ctx context.Context, d Deps, schoolID string) ([]string, error) {
	ids := make([]string, 0, len(demoLearners))
	for _, l := range demoLearners {
		id, err := ensureLearner(ctx, d, schoolID, l)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func ensureLearner(ctx context.Context, d Deps, schoolID string, l struct {
	first, middle, last, gender, externalID, dob string
}) (string, error) {
	var id string
	err := d.Pool.QueryRow(ctx,
		`SELECT id FROM learners WHERE external_id = $1`, l.externalID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	dob, err := time.Parse("2006-01-02", l.dob)
	if err != nil {
		return "", err
	}
	learner, err := d.Students.CreateLearner(ctx, schoolID, students.LearnerInput{
		FirstName:   l.first,
		MiddleName:  l.middle,
		LastName:    l.last,
		Gender:      l.gender,
		ExternalID:  l.externalID,
		DateOfBirth: &dob,
	})
	if err != nil {
		return "", err
	}
	return learner.ID, nil
}

func ensureRoster(ctx context.Context, d Deps, schoolID, classID string, learnerIDs []string) error {
	var missing []string
	for _, id := range learnerIDs {
		var exists bool
		err := d.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM roster_entries WHERE class_group_id = $1 AND learner_id = $2)`,
			classID, id).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return d.Academics.AddRoster(ctx, schoolID, classID, missing)
}

func ensureEnrollments(ctx context.Context, d Deps, schoolID, classID, yearID string, learnerIDs []string) error {
	for _, learnerID := range learnerIDs {
		var live bool
		// Mirror of the students service open-enrollment guard: any
		// non-terminal (ended_at IS NULL) enrollment blocks a new one.
		err := d.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM enrollments WHERE learner_id = $1 AND school_id = $2 AND ended_at IS NULL)`,
			learnerID, schoolID).Scan(&live)
		if err != nil {
			return err
		}
		if live {
			continue
		}
		if _, err := d.Students.EnrollLearner(ctx, schoolID, students.EnrollmentInput{
			LearnerID:      learnerID,
			ClassGroupID:   classID,
			AcademicYearID: yearID,
			Status:         "admitted",
		}); err != nil {
			return err
		}
	}
	return nil
}
