//go:build integration

package academics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/testdb"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
	"github.com/google/uuid"
)

type fixture struct {
	svc     *Service
	auth    *identity.AuthService
	jwt     *identity.JWTManager
	tenancy *tenancy.Service
	pool    *postgres.Pool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testdb.New(t)
	jwt := identity.NewJWTManager("integration-test-secret-at-least-32-bytes!", 1<<30*time.Second)
	authSvc := identity.NewAuthService(identity.NewRepo(pool), jwt)
	tenSvc := tenancy.NewService(tenancy.NewRepo(pool), pool)
	svc := NewService(NewRepo(pool), pool)
	return &fixture{svc: svc, auth: authSvc, jwt: jwt, tenancy: tenSvc, pool: pool}
}

func mustSchool(t *testing.T, f *fixture, code, name string) *tenancy.School {
	t.Helper()
	s, err := f.tenancy.CreateSchool(context.Background(), code, name, "", "")
	if err != nil {
		t.Fatalf("create school %s: %v", code, err)
	}
	return s
}

// seedLearner inserts a learner via raw SQL — academics consumes learner ids
// through the roster FK WITHOUT importing the students package.
func seedLearner(t *testing.T, f *fixture, first, last string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.pool.Exec(context.Background(),
		`INSERT INTO learners (id, first_name, last_name) VALUES ($1,$2,$3)`, id, first, last)
	if err != nil {
		t.Fatalf("seed learner: %v", err)
	}
	return id
}

func mustTeacher(t *testing.T, f *fixture, email string) string {
	t.Helper()
	u, err := f.auth.CreateUser(context.Background(), email, "Techer "+email, "s3cure-passw0rd!", "")
	if err != nil {
		t.Fatalf("create teacher: %v", err)
	}
	return u.ID
}

func eventCount(t *testing.T, f *fixture, eventType, schoolID string) int {
	t.Helper()
	var n int
	err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM event_outbox WHERE event_type = $1 AND school_id = $2`, eventType, schoolID).Scan(&n)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

func TestAcademicYearValidation(t *testing.T) {
	f := newFixture(t)
	school := mustSchool(t, f, "AY-S", "Year School")

	if _, err := f.svc.CreateAcademicYear(context.Background(), school.ID, "2026", "2026-01-10", "2026-01-01"); err == nil {
		t.Fatal("end before start: want error, got nil")
	}
	if _, err := f.svc.CreateAcademicYear(context.Background(), school.ID, "Bad", "not-a-date", "2026-12-31"); err == nil {
		t.Fatal("malformed date: want error, got nil")
	}
	if _, err := f.svc.CreateAcademicYear(context.Background(), school.ID, "", "2026-01-01", "2026-12-31"); err == nil {
		t.Fatal("empty name: want error, got nil")
	}

	y, err := f.svc.CreateAcademicYear(context.Background(), school.ID, "2026", "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("valid year: %v", err)
	}
	if y.Status != "planning" {
		t.Fatalf("default status = %s, want planning", y.Status)
	}
	// Inclusive bound: same-day year is legal.
	if _, err := f.svc.CreateAcademicYear(context.Background(), school.ID, "SameDay", "2027-01-01", "2027-01-01"); err != nil {
		t.Fatalf("same-day year should be legal: %v", err)
	}
}

func TestTermOverlapRejected(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "TERM-S", "Term School")
	year, err := f.svc.CreateAcademicYear(ctx, school.ID, "2026", "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.CreateTerm(ctx, school.ID, year.ID, "Term 1", "2026-01-01", "2026-04-30"); err != nil {
		t.Fatalf("seed term 1: %v", err)
	}

	// Direct overlap rejected.
	if _, err := f.svc.CreateTerm(ctx, school.ID, year.ID, "Overlap", "2026-04-01", "2026-08-31"); err == nil {
		t.Fatal("overlapping term accepted")
	}
	// Touching-boundary overlap rejected (inclusive ranges).
	if _, err := f.svc.CreateTerm(ctx, school.ID, year.ID, "Touch", "2026-04-30", "2026-06-30"); err == nil {
		t.Fatal("boundary-touching term accepted")
	}
	// Outside the year bounds rejected.
	if _, err := f.svc.CreateTerm(ctx, school.ID, year.ID, "Spill", "2026-11-01", "2027-02-28"); err == nil {
		t.Fatal("term outside year accepted")
	}
	// Unknown year rejected.
	if _, err := f.svc.CreateTerm(ctx, school.ID, "00000000-0000-0000-0000-000000000000", "Ghost", "2026-01-01", "2026-02-28"); err == nil {
		t.Fatal("term on unknown year accepted")
	}

	// Adjacent (day after) term is legal.
	t2, err := f.svc.CreateTerm(ctx, school.ID, year.ID, "Term 2", "2026-05-01", "2026-08-31")
	if err != nil {
		t.Fatalf("adjacent term rejected: %v", err)
	}
	if t2.Name != "Term 2" {
		t.Fatalf("adjacent term name = %s", t2.Name)
	}

	// Term-creation events outboxed for the school.
	if n := eventCount(t, f, "academics.TermCreated", school.ID); n != 2 {
		t.Fatalf("academics.TermCreated count = %d, want 2", n)
	}
}

func TestClassRosterBulkAddAndUnique(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "ROSTER-S", "Roster School")
	year, _ := f.svc.CreateAcademicYear(ctx, school.ID, "2026", "2026-01-01", "2026-12-31")
	class, err := f.svc.CreateClassGroup(ctx, school.ID, year.ID, "Grade 4 Blue")
	if err != nil {
		t.Fatal(err)
	}

	l1 := seedLearner(t, f, "Amina", "Okello")
	l2 := seedLearner(t, f, "Brian", "Wekesa")

	// Batch size guards.
	if err := f.svc.AddRoster(ctx, school.ID, class.ID, nil); err == nil {
		t.Fatal("empty batch accepted")
	}
	if err := f.svc.AddRoster(ctx, school.ID, class.ID, []string{"not-a-uuid"}); err == nil {
		t.Fatal("invalid uuid accepted")
	}
	if err := f.svc.AddRoster(ctx, school.ID, class.ID, []string{l1, l2, l1}); err != nil {
		t.Fatalf("valid bulk add: %v", err)
	}
	// Idempotent: re-adding the same pair is a no-op, not an error.
	if err := f.svc.AddRoster(ctx, school.ID, class.ID, []string{l1}); err != nil {
		t.Fatalf("idempotent re-add: %v", err)
	}

	// Unknown learner id (valid uuid) -> validation error via FK.
	if err := f.svc.AddRoster(ctx, school.ID, class.ID, []string{"00000000-0000-0000-0000-000000000001"}); err == nil {
		t.Fatal("unknown learner accepted")
	}

	roster, err := f.svc.Roster(ctx, school.ID, class.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster) != 2 {
		t.Fatalf("roster size = %d, want 2 (idempotent pair must not duplicate)", len(roster))
	}
	if roster[0].LastName != "Okello" || roster[1].LastName != "Wekesa" {
		t.Fatalf("roster order wrong: %s, %s", roster[0].LastName, roster[1].LastName)
	}

	// Roster of another school's class is not resolvable.
	other := mustSchool(t, f, "ROSTER-T", "Roster Two")
	if _, err := f.svc.Roster(ctx, other.ID, class.ID); err == nil {
		t.Fatal("cross-school roster read accepted")
	}
}

func TestTeachingAssignmentUniquePerClassSubject(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "TEACH-S", "Teach School")
	year, _ := f.svc.CreateAcademicYear(ctx, school.ID, "2026", "2026-01-01", "2026-12-31")
	class, _ := f.svc.CreateClassGroup(ctx, school.ID, year.ID, "Grade 5")
	math, err := f.svc.CreateSubject(ctx, school.ID, "MATH", "Mathematics")
	if err != nil {
		t.Fatal(err)
	}
	teacher := mustTeacher(t, f, "teach1@skolara.test")

	a, err := f.svc.AssignTeacher(ctx, school.ID, class.ID, math.ID, teacher)
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if a.TeacherID != teacher || a.SubjectID != math.ID {
		t.Fatalf("assignment fields wrong: %+v", a)
	}

	// Same class+subject again -> 409 semantics (ErrAssignmentExists).
	if _, err := f.svc.AssignTeacher(ctx, school.ID, class.ID, math.ID, teacher); err == nil {
		t.Fatal("duplicate assignment accepted")
	}

	// Unknown teacher (valid uuid, no user row) -> validation error.
	if _, err := f.svc.AssignTeacher(ctx, school.ID, class.ID, math.ID, "00000000-0000-0000-0000-000000000002"); err == nil {
		t.Fatal("unknown teacher accepted")
	}
	// Unknown subject -> 404 semantics.
	if _, err := f.svc.AssignTeacher(ctx, school.ID, class.ID, "00000000-0000-0000-0000-000000000003", teacher); err == nil {
		t.Fatal("unknown subject accepted")
	}

	views, err := f.svc.AssignmentsForClass(ctx, school.ID, class.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Teacher.Email != "teach1@skolara.test" || views[0].Subject.Code != "MATH" {
		t.Fatalf("assignment view wrong: %+v", views)
	}
}

func TestAllScopedBySchool(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sa := mustSchool(t, f, "SCOPE-A", "Scope A")
	sb := mustSchool(t, f, "SCOPE-B", "Scope B")

	yearA, _ := f.svc.CreateAcademicYear(ctx, sa.ID, "2026", "2026-01-01", "2026-12-31")
	subA, _ := f.svc.CreateSubject(ctx, sa.ID, "ENG", "English")

	// School B sees nothing of school A.
	if years, _ := f.svc.AcademicYears(ctx, sb.ID); len(years) != 0 {
		t.Fatalf("school B sees %d years of school A", len(years))
	}
	if subs, _ := f.svc.Subjects(ctx, sb.ID); len(subs) != 0 {
		t.Fatalf("school B sees %d subjects of school A", len(subs))
	}
	if _, err := f.svc.AcademicYearByID(ctx, sb.ID, yearA.ID); err == nil {
		t.Fatal("school B resolved school A's year")
	}
	if _, err := f.svc.SubjectByID(ctx, sb.ID, subA.ID); err == nil {
		t.Fatal("school B resolved school A's subject")
	}
	if _, err := f.svc.CreateTerm(ctx, sb.ID, yearA.ID, "Hijack", "2026-01-01", "2026-02-28"); err == nil {
		t.Fatal("school B wrote a term onto school A's year")
	}
	// Duplicate subject code in DIFFERENT schools is fine (per-school codes).
	if _, err := f.svc.CreateSubject(ctx, sb.ID, "ENG", "English"); err != nil {
		t.Fatalf("same code in another school rejected: %v", err)
	}
}

func TestClassCreatedEventOutboxed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "EVT-S", "Event School")
	year, _ := f.svc.CreateAcademicYear(ctx, school.ID, "2026", "2026-01-01", "2026-12-31")
	class, err := f.svc.CreateClassGroup(ctx, school.ID, year.ID, "Grade 1")
	if err != nil {
		t.Fatal(err)
	}

	var (
		id      string
		sid     string
		agg     string
		typ     string
		ver     int
		payload []byte
	)
	if err = f.pool.QueryRow(ctx,
		`SELECT event_id, school_id, aggregate_id, event_type, schema_version, payload
                 FROM event_outbox WHERE event_type = 'academics.ClassCreated' AND school_id = $1`, school.ID).
		Scan(&id, &sid, &agg, &typ, &ver, &payload); err != nil {
		t.Fatalf("class-created event not outboxed: %v", err)
	}
	if sid != school.ID || agg != class.ID || typ != "academics.ClassCreated" || ver != 1 {
		t.Fatalf("envelope wrong: school=%s agg=%s type=%s ver=%d", sid, agg, typ, ver)
	}
	// JSONB normalizes whitespace — assert via parsing, not substring.
	var payloadMap map[string]any
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		t.Fatalf("payload not json: %v", err)
	}
	if payloadMap["name"] != "Grade 1" || payloadMap["academic_year_id"] != year.ID {
		t.Fatalf("payload wrong: %s", payload)
	}
}

func TestAcademicsHTTPFlow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.auth.CreateUser(ctx, "admin@skolara.test", "Admin", "s3cure-passw0rd!", identity.RolePlatformAdmin); err != nil {
		t.Fatal(err)
	}
	token, _, err := f.auth.Login(ctx, "admin@skolara.test", "s3cure-passw0rd!", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	school := mustSchool(t, f, "HTTP-A", "HTTP Academics")

	mux := http.NewServeMux()
	NewHandler(f.svc).Register(mux, f.jwt, f.auth)
	handler := identity.RequireAuth(f.jwt, tenancy.RequireSchool(f.tenancy, mux))

	do := func(method, path string, body any, withSchool bool) *httptest.ResponseRecorder {
		t.Helper()
		var rd *bytes.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			rd = bytes.NewReader(raw)
		} else {
			rd = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		if withSchool {
			req.Header.Set("X-School-ID", school.ID)
		}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}

	// Create year: 201.
	rr := do("POST", "/api/v1/academic-years", map[string]any{"name": "2026", "startDate": "2026-01-01", "endDate": "2026-12-31"}, true)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create year: %d %s", rr.Code, rr.Body.String())
	}
	var year AcademicYear
	_ = json.Unmarshal(rr.Body.Bytes(), &year)

	// Year validation over HTTP: end before start -> 400.
	rr = do("POST", "/api/v1/academic-years", map[string]any{"name": "Bad", "startDate": "2026-12-31", "endDate": "2026-01-01"}, true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad range: %d", rr.Code)
	}

	// Create term: 201; overlapping term: 409.
	rr = do("POST", "/api/v1/terms", map[string]any{"academicYearId": year.ID, "name": "T1", "startDate": "2026-01-01", "endDate": "2026-04-30"}, true)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create term: %d %s", rr.Code, rr.Body.String())
	}
	rr = do("POST", "/api/v1/terms", map[string]any{"academicYearId": year.ID, "name": "T2", "startDate": "2026-04-01", "endDate": "2026-06-30"}, true)
	if rr.Code != http.StatusConflict {
		t.Fatalf("overlap term: %d", rr.Code)
	}

	// Subject + class: 201 each.
	rr = do("POST", "/api/v1/subjects", map[string]any{"code": "MATH", "name": "Mathematics"}, true)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create subject: %d %s", rr.Code, rr.Body.String())
	}
	var subject Subject
	_ = json.Unmarshal(rr.Body.Bytes(), &subject)

	rr = do("POST", "/api/v1/classes", map[string]any{"academicYearId": year.ID, "name": "Grade 1"}, true)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create class: %d %s", rr.Code, rr.Body.String())
	}
	var class ClassGroup
	_ = json.Unmarshal(rr.Body.Bytes(), &class)

	// Duplicate class name in same year: 409.
	rr = do("POST", "/api/v1/classes", map[string]any{"academicYearId": year.ID, "name": "grade 1"}, true)
	if rr.Code != http.StatusConflict {
		t.Fatalf("dup class (case-insensitive): %d", rr.Code)
	}

	// Roster: bulk add 204, list 200 with seeded learner.
	l1 := seedLearner(t, f, "Roster", "One")
	rr = do("POST", fmt.Sprintf("/api/v1/classes/%s/roster", class.ID), map[string]any{"learnerIds": []string{l1}}, true)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("add roster: %d %s", rr.Code, rr.Body.String())
	}
	rr = do("GET", fmt.Sprintf("/api/v1/classes/%s/roster", class.ID), nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("list roster: %d", rr.Code)
	}
	var roster []RosterEntryView
	_ = json.Unmarshal(rr.Body.Bytes(), &roster)
	if len(roster) != 1 || roster[0].LearnerID != l1 {
		t.Fatalf("roster content wrong: %+v", roster)
	}

	// Teacher assignment: 201; duplicate: 409.
	teacher := mustTeacher(t, f, "http-teach@skolara.test")
	rr = do("POST", fmt.Sprintf("/api/v1/classes/%s/teachers", class.ID), map[string]any{"subjectId": subject.ID, "teacherId": teacher}, true)
	if rr.Code != http.StatusCreated {
		t.Fatalf("assign teacher: %d %s", rr.Code, rr.Body.String())
	}
	rr = do("POST", fmt.Sprintf("/api/v1/classes/%s/teachers", class.ID), map[string]any{"subjectId": subject.ID, "teacherId": teacher}, true)
	if rr.Code != http.StatusConflict {
		t.Fatalf("dup teacher: %d", rr.Code)
	}

	// Unknown class: 404 (not 403) — tenant enumeration guard.
	rr = do("GET", "/api/v1/classes/00000000-0000-0000-0000-000000000000/roster", nil, true)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown class roster: %d", rr.Code)
	}

	// Missing school context: 403 before domain work.
	rr = do("GET", "/api/v1/academic-years", nil, false)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("no school context: %d", rr.Code)
	}
}
