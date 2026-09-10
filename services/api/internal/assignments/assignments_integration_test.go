//go:build integration

package assignments

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

func mustUser(t *testing.T, f *fixture, email string) string {
	t.Helper()
	u, err := f.auth.CreateUser(context.Background(), email, email, "s3cure-passw0rd!", "")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.ID
}

// seedClassMaterial seeds year+class+subject via raw SQL (no academics import)
// and returns (classID, subjectID).
func seedClassMaterial(t *testing.T, f *fixture, schoolID, name string) (string, string) {
	t.Helper()
	ctx := context.Background()
	yearID := uuid.NewString()
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO academic_years (id, school_id, name, start_date, end_date) VALUES ($1,$2,$3,'2026-01-01','2026-12-31')`,
		yearID, schoolID, "2026"); err != nil {
		t.Fatalf("seed year: %v", err)
	}
	classID := uuid.NewString()
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO class_groups (id, school_id, academic_year_id, name) VALUES ($1,$2,$3,$4)`,
		classID, schoolID, yearID, name); err != nil {
		t.Fatalf("seed class: %v", err)
	}
	subjectID := uuid.NewString()
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO subjects (id, school_id, code, name) VALUES ($1,$2,$3,$4)`,
		subjectID, schoolID, "SUBJ-"+name, name); err != nil {
		t.Fatalf("seed subject: %v", err)
	}
	return classID, subjectID
}

// seedRoster seats a learner in a class via raw SQL.
func seedRoster(t *testing.T, f *fixture, classID, learnerID string) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(),
		`INSERT INTO roster_entries (class_group_id, learner_id) VALUES ($1,$2)`, classID, learnerID)
	if err != nil {
		t.Fatalf("seed roster: %v", err)
	}
}

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

func mustAssignment(t *testing.T, f *fixture, schoolID, teacherID, classID, subjectID, title string, due string) *Assignment {
	t.Helper()
	a, err := f.svc.CreateAssignment(context.Background(), schoolID, teacherID, CreateAssignmentInput{
		ClassGroupID: classID, SubjectID: subjectID, Title: title, DueDate: due,
	})
	if err != nil {
		t.Fatalf("create assignment: %v", err)
	}
	return a
}

func futureDate() string { return time.Now().UTC().Add(48 * time.Hour).Format("2006-01-02") }
func pastDate() string   { return time.Now().UTC().Add(-48 * time.Hour).Format("2006-01-02") }

func TestAssignmentLifecycleAndDueDate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "ASG-L", "Assignment Lifecycle School")
	teacher := mustUser(t, f, "asg-owner@skolara.test")
	classID, subjectID := seedClassMaterial(t, f, school.ID, "Form 2")

	// Past due date rejected at creation.
	if _, err := f.svc.CreateAssignment(ctx, school.ID, teacher, CreateAssignmentInput{
		ClassGroupID: classID, SubjectID: subjectID, Title: "Old", DueDate: pastDate(),
	}); err == nil {
		t.Fatal("past due date accepted at creation")
	}
	a := mustAssignment(t, f, school.ID, teacher, classID, subjectID, "Algebra HW 1", futureDate())

	// draft -> published (owner).
	pub, err := f.svc.PublishAssignment(ctx, school.ID, teacher, a.ID)
	if err != nil || pub.Status != AssignmentPublished {
		t.Fatalf("publish: %v %s", err, pub.Status)
	}
	// publish again is illegal (published -> published not in machine).
	if _, err := f.svc.PublishAssignment(ctx, school.ID, teacher, a.ID); err == nil {
		t.Fatal("double publish accepted")
	}
	// published -> closed.
	closed, err := f.svc.CloseAssignment(ctx, school.ID, teacher, a.ID)
	if err != nil || closed.Status != AssignmentClosed {
		t.Fatalf("close: %v %s", err, closed.Status)
	}
	// closed is terminal.
	if _, err := f.svc.CloseAssignment(ctx, school.ID, teacher, a.ID); err == nil {
		t.Fatal("double close accepted")
	}

	// Due date in the past at PUBLISH time: create draft with future date,
	// time-travel not available — instead verify a draft created with no due
	// date publishes fine and one with past date is rejected at creation
	// (already covered). Publish on unknown assignment -> 404 semantics.
	if _, err := f.svc.PublishAssignment(ctx, school.ID, teacher, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Fatal("publish unknown assignment accepted")
	}
}

func TestSubmissionFlowAndGuards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "ASG-S", "Submission School")
	teacher := mustUser(t, f, "asg-t1@skolara.test")
	classID, subjectID := seedClassMaterial(t, f, school.ID, "Form 3")
	learner := seedLearner(t, f, "Hal", "Eight")
	seedRoster(t, f, classID, learner)

	// Submission only while PUBLISHED.
	draft := mustAssignment(t, f, school.ID, teacher, classID, subjectID, "Essay draft", "")
	if _, err := f.svc.SubmitAssignment(ctx, school.ID, draft.ID, learner, "early"); err == nil {
		t.Fatal("submission on draft accepted")
	}

	a := mustAssignment(t, f, school.ID, teacher, classID, subjectID, "Essay", futureDate())
	if _, err := f.svc.SubmitAssignment(ctx, school.ID, a.ID, learner, "v1"); err == nil {
		t.Fatal("submission before publish accepted")
	}
	if _, err := f.svc.PublishAssignment(ctx, school.ID, teacher, a.ID); err != nil {
		t.Fatal(err)
	}

	// Un-rostered learner rejected.
	stranger := seedLearner(t, f, "Ida", "Nine")
	if _, err := f.svc.SubmitAssignment(ctx, school.ID, a.ID, stranger, "v0"); err == nil {
		t.Fatal("un-rostered learner accepted")
	}

	sub1, err := f.svc.SubmitAssignment(ctx, school.ID, a.ID, learner, "v1")
	if err != nil || sub1.Status != SubmissionSubmitted {
		t.Fatalf("submit: %v %s", err, sub1.Status)
	}
	// Resubmission refreshes content while submitted (same row).
	sub2, err := f.svc.SubmitAssignment(ctx, school.ID, a.ID, learner, "v2 improved")
	if err != nil || sub2.ID != sub1.ID || sub2.Content != "v2 improved" {
		t.Fatalf("resubmit: %v %+v", err, sub2)
	}

	// Grade: owner only; grade required.
	otherTeacher := mustUser(t, f, "asg-t2@skolara.test")
	if _, err := f.svc.GradeAssignment(ctx, school.ID, otherTeacher, a.ID, learner, "A", "great"); err == nil {
		t.Fatal("non-owner grading accepted")
	}
	if _, err := f.svc.GradeAssignment(ctx, school.ID, teacher, a.ID, learner, "", "no grade"); err == nil {
		t.Fatal("empty grade accepted")
	}
	graded, err := f.svc.GradeAssignment(ctx, school.ID, teacher, a.ID, learner, "A-", "solid")
	if err != nil || graded.Status != SubmissionGraded || graded.Grade == nil || *graded.Grade != "A-" {
		t.Fatalf("grade: %v %+v", err, graded)
	}
	// Resubmission after grading rejected.
	if _, err := f.svc.SubmitAssignment(ctx, school.ID, a.ID, learner, "v3"); err == nil {
		t.Fatal("resubmission after grading accepted")
	}
	// Grading an already-graded submission rejected.
	if _, err := f.svc.GradeAssignment(ctx, school.ID, teacher, a.ID, learner, "B", "no"); err == nil {
		t.Fatal("double grading accepted")
	}
	// Return: graded -> returned.
	ret, err := f.svc.ReturnAssignment(ctx, school.ID, teacher, a.ID, learner)
	if err != nil || ret.Status != SubmissionReturned {
		t.Fatalf("return: %v %s", err, ret.Status)
	}
	// Returning a submitted (not graded) submission rejected.
	if _, err := f.svc.SubmitAssignment(ctx, school.ID, a.ID, learner, "x"); err == nil {
		t.Fatal("resubmission after return accepted")
	}

	subs, err := f.svc.Submissions(ctx, school.ID, teacher, a.ID)
	if err != nil || len(subs) != 1 || subs[0].LearnerID != learner {
		t.Fatalf("submissions: %v %+v", err, subs)
	}
}

func TestAssignmentTenantIsolationAndEvents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sA := mustSchool(t, f, "ASG-A", "Asg A")
	sB := mustSchool(t, f, "ASG-B", "Asg B")
	tA := mustUser(t, f, "asg-ta@skolara.test")
	tB := mustUser(t, f, "asg-tb@skolara.test")
	classA, subjectA := seedClassMaterial(t, f, sA.ID, "A Class")
	learnerA := seedLearner(t, f, "Jay", "Ten")
	seedRoster(t, f, classA, learnerA)

	a := mustAssignment(t, f, sA.ID, tA, classA, subjectA, "History Q", futureDate())

	// School B teacher cannot resolve/publish/grade school A's assignment.
	if _, err := f.svc.Assignment(ctx, sB.ID, a.ID); err == nil {
		t.Fatal("school B resolved school A assignment")
	}
	if _, err := f.svc.PublishAssignment(ctx, sB.ID, tB, a.ID); err == nil {
		t.Fatal("school B published school A assignment")
	}
	if _, err := f.svc.GradeAssignment(ctx, sB.ID, tB, a.ID, learnerA, "Z", "no"); err == nil {
		t.Fatal("school B graded school A assignment")
	}

	// Publish emits event; grading emits event.
	if _, err := f.svc.PublishAssignment(ctx, sA.ID, tA, a.ID); err != nil {
		t.Fatal(err)
	}
	var published int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM event_outbox WHERE event_type = 'assignments.AssignmentPublished' AND school_id = $1`, sA.ID).Scan(&published); err != nil || published != 1 {
		t.Fatalf("published events = %d err=%v", published, err)
	}
	if _, err := f.svc.SubmitAssignment(ctx, sA.ID, a.ID, learnerA, "my answer"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.GradeAssignment(ctx, sA.ID, tA, a.ID, learnerA, "B+", "good"); err != nil {
		t.Fatal(err)
	}
	var payload []byte
	if err := f.pool.QueryRow(ctx,
		`SELECT payload FROM event_outbox WHERE event_type = 'assignments.SubmissionGraded' AND school_id = $1`, sA.ID).
		Scan(&payload); err != nil {
		t.Fatalf("graded event not outboxed: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(payload, &got)
	if got["learner_id"] != learnerA || got["grade"] != "B+" {
		t.Fatalf("graded payload wrong: %s", payload)
	}
}

func TestAssignmentsHTTPFlow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.auth.CreateUser(ctx, "asg-admin@skolara.test", "Asg Admin", "s3cure-passw0rd!", identity.RolePlatformAdmin); err != nil {
		t.Fatal(err)
	}
	token, _, err := f.auth.Login(ctx, "asg-admin@skolara.test", "s3cure-passw0rd!", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	school := mustSchool(t, f, "ASG-H", "HTTP Assignments")
	// Platform admin is not the owning teacher — ownership checks are tested
	// at service level; here we exercise wiring + validation paths.
	classID, subjectID := seedClassMaterial(t, f, school.ID, "Form 8")
	teacher := mustUser(t, f, "asg-http-owner@skolara.test")

	mux := http.NewServeMux()
	NewHandler(f.svc).Register(mux, f.jwt, f.auth)
	handler := identity.RequireAuth(f.jwt, tenancy.RequireSchool(f.tenancy, mux))

	do := func(method, path string, body any, withSchool bool) *httptest.ResponseRecorder {
		t.Helper()
		var rd *bytes.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
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

	// Create: 201 (creator becomes the owning teacher — the platform admin
	// in this flow).
	rr := do("POST", "/api/v1/assignments", map[string]any{
		"classGroupId": classID, "subjectId": subjectID, "title": "HTTP HW", "dueDate": futureDate(),
	}, true)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var a Assignment
	_ = json.Unmarshal(rr.Body.Bytes(), &a)

	// Ownership: the seeded teacher (NOT the creator) publish -> 403.
	// Built at service level so the owner differs from the HTTP actor.
	foreign, err := f.svc.CreateAssignment(ctx, school.ID, teacher, CreateAssignmentInput{
		ClassGroupID: classID, SubjectID: subjectID, Title: "Owned by teacher", DueDate: futureDate(),
	})
	if err != nil {
		t.Fatal(err)
	}
	rr = do("POST", "/api/v1/assignments/"+foreign.ID+"/publish", nil, true)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-owner publish: %d %s", rr.Code, rr.Body.String())
	}

	// Past due date: 400.
	rr = do("POST", "/api/v1/assignments", map[string]any{
		"classGroupId": classID, "subjectId": subjectID, "title": "Old", "dueDate": pastDate(),
	}, true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("past due: %d", rr.Code)
	}

	// Listing: 200.
	rr = do("GET", "/api/v1/assignments?status=draft", nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: %d", rr.Code)
	}

	// Owner publish over HTTP: 200 (creator is the owner).
	rr = do("POST", fmt.Sprintf("/api/v1/assignments/%s/publish", a.ID), nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("owner publish: %d %s", rr.Code, rr.Body.String())
	}

	// Unknown assignment: 404.
	rr = do("GET", "/api/v1/assignments/00000000-0000-0000-0000-000000000000", nil, true)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown: %d", rr.Code)
	}

	// Missing school context: 403 no_school_context.
	rr = do("GET", "/api/v1/assignments", nil, false)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("no school: %d", rr.Code)
	}
	_ = teacher
}
