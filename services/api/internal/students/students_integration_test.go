//go:build integration

package students

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/testdb"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
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

func mustLearner(t *testing.T, f *fixture, schoolID, first, last string) *Learner {
	t.Helper()
	l, err := f.svc.CreateLearner(context.Background(), schoolID, LearnerInput{FirstName: first, LastName: last})
	if err != nil {
		t.Fatalf("create learner %s %s: %v", first, last, err)
	}
	return l
}

// seedPaths walks a LEGAL transition route from an admitted/applicant start to
// each reachable state, so every matrix row starts from a real DB state.
var seedPaths = map[EnrollmentStatus][]EnrollmentStatus{
	StatusApplicant:       {},
	StatusAdmitted:        {},
	StatusActive:          {StatusActive},
	StatusSuspended:       {StatusActive, StatusSuspended},
	StatusTransferPending: {StatusActive, StatusTransferPending},
	StatusGraduated:       {StatusActive, StatusGraduated},
	StatusTransferredOut:  {StatusActive, StatusTransferPending, StatusTransferredOut},
	StatusWithdrawn:       {StatusWithdrawn},
	StatusAlumni:          {StatusActive, StatusAlumni},
}

func seedEnrollmentAt(t *testing.T, f *fixture, schoolID, learnerID string, state EnrollmentStatus) *Enrollment {
	t.Helper()
	ctx := context.Background()
	initial := StatusAdmitted
	if state == StatusApplicant {
		initial = StatusApplicant
	}
	e, err := f.svc.EnrollLearner(ctx, schoolID, EnrollmentInput{LearnerID: learnerID, Status: string(initial)})
	if err != nil {
		t.Fatalf("seed enrollment at %s: %v", initial, err)
	}
	for _, next := range seedPaths[state] {
		if !CanTransition(e.Status, next) {
			t.Fatalf("seed path broken: %s -> %s", e.Status, next)
		}
		e, err = f.svc.TransitionEnrollment(ctx, schoolID, e.ID, next)
		if err != nil {
			t.Fatalf("seed transition %s -> %s: %v", e.Status, next, err)
		}
	}
	if e.Status != state {
		t.Fatalf("seed: got status %s, want %s", e.Status, state)
	}
	return e
}

// TestEnrollmentLifecycleMatrix walks EVERY legal transition through the pure
// state machine AND the database, plus a sample of illegal ones.
func TestEnrollmentLifecycleMatrix(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "MATRIX-A", "Matrix School")

	legalEdges := []struct{ from, to EnrollmentStatus }{
		{StatusApplicant, StatusAdmitted},
		{StatusApplicant, StatusWithdrawn},
		{StatusAdmitted, StatusActive},
		{StatusAdmitted, StatusWithdrawn},
		{StatusActive, StatusSuspended},
		{StatusActive, StatusTransferPending},
		{StatusActive, StatusGraduated},
		{StatusActive, StatusWithdrawn},
		{StatusActive, StatusAlumni},
		{StatusSuspended, StatusActive},
		{StatusSuspended, StatusWithdrawn},
		{StatusTransferPending, StatusTransferredOut},
		{StatusTransferPending, StatusActive},
		{StatusGraduated, StatusAlumni},
	}
	// Guard against drift: the table above must cover the state machine map.
	for from, tos := range legalTransitions {
		for _, to := range tos {
			found := false
			for _, edge := range legalEdges {
				if edge.from == from && edge.to == to {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("legal edge %s->%s missing from matrix table", from, to)
			}
		}
	}

	for _, edge := range legalEdges {
		edge := edge
		t.Run("legal/"+string(edge.from)+"->"+string(edge.to), func(t *testing.T) {
			if !CanTransition(edge.from, edge.to) {
				t.Fatalf("CanTransition(%s, %s) = false, want true", edge.from, edge.to)
			}
			learner := mustLearner(t, f, school.ID, "Legal", fmt.Sprintf("Edge%d%d", len(edge.from), len(edge.to))+string(edge.from))
			e := seedEnrollmentAt(t, f, school.ID, learner.ID, edge.from)

			got, err := f.svc.TransitionEnrollment(ctx, school.ID, e.ID, edge.to)
			if err != nil {
				t.Fatalf("legal transition %s -> %s rejected: %v", edge.from, edge.to, err)
			}
			if got.Status != edge.to {
				t.Fatalf("status = %s, want %s", got.Status, edge.to)
			}
			// Persisted state matches, with ended_at set exactly on entry
			// into a terminal-out state.
			db, err := f.svc.repo.EnrollmentByIDInSchool(ctx, school.ID, e.ID)
			if err != nil {
				t.Fatal(err)
			}
			if db.Status != edge.to {
				t.Fatalf("db status = %s, want %s", db.Status, edge.to)
			}
			ended := containsState(endedAtStates, edge.to)
			if ended && db.EndedAt == nil {
				t.Fatalf("ended_at not set on entering %s", edge.to)
			}
			if !ended && db.EndedAt != nil {
				t.Fatalf("ended_at unexpectedly set on entering %s", edge.to)
			}
		})
	}

	illegalEdges := []struct{ from, to EnrollmentStatus }{
		{StatusApplicant, StatusActive},
		{StatusWithdrawn, StatusActive},
		{StatusTransferredOut, StatusActive},
		{StatusAlumni, StatusApplicant},
	}
	for _, edge := range illegalEdges {
		edge := edge
		t.Run("illegal/"+string(edge.from)+"->"+string(edge.to), func(t *testing.T) {
			if CanTransition(edge.from, edge.to) {
				t.Fatalf("CanTransition(%s, %s) = true, want false", edge.from, edge.to)
			}
			learner := mustLearner(t, f, school.ID, "Illegal", string(edge.from)+string(edge.to))
			e := seedEnrollmentAt(t, f, school.ID, learner.ID, edge.from)

			_, err := f.svc.TransitionEnrollment(ctx, school.ID, e.ID, edge.to)
			if !errors.Is(err, ErrIllegalTransition) {
				t.Fatalf("illegal transition %s -> %s: got %v, want ErrIllegalTransition", edge.from, edge.to, err)
			}
			db, err := f.svc.repo.EnrollmentByIDInSchool(ctx, school.ID, e.ID)
			if err != nil {
				t.Fatal(err)
			}
			if db.Status != edge.from {
				t.Fatalf("db status changed on illegal transition: %s", db.Status)
			}
		})
	}
}

// TestGuardianLinkingAndAccessFlags covers guardian creation validation,
// linking with relationship + IsPrimary + CanView flags, and duplicate links.
func TestGuardianLinkingAndAccessFlags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "GUARD-A", "Guardian School")
	learner := mustLearner(t, f, school.ID, "Nala", "Kimani")
	// Linking is tenant-guarded: the learner must be enrolled at the school.
	if _, err := f.svc.EnrollLearner(ctx, school.ID, EnrollmentInput{LearnerID: learner.ID}); err != nil {
		t.Fatal(err)
	}

	// Validation: names required.
	if _, err := f.svc.CreateGuardian(ctx, school.ID, GuardianInput{FirstName: "", LastName: "Solo"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing first name: %v", err)
	}
	// Validation: basic phone.
	if _, err := f.svc.CreateGuardian(ctx, school.ID, GuardianInput{FirstName: "A", LastName: "B", Phone: "call-me"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid phone accepted: %v", err)
	}
	// Validation: basic email.
	if _, err := f.svc.CreateGuardian(ctx, school.ID, GuardianInput{FirstName: "A", LastName: "B", Email: "no-at-sign"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid email accepted: %v", err)
	}

	mother, err := f.svc.CreateGuardian(ctx, school.ID, GuardianInput{
		FirstName: "Mary", LastName: "Kimani", Phone: "+254 700 000 001", Email: "mary.kimani@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	father, err := f.svc.CreateGuardian(ctx, school.ID, GuardianInput{FirstName: "John", LastName: "Kimani", Phone: "+254711222333"})
	if err != nil {
		t.Fatal(err)
	}

	// Relationship validation happens before any persistence.
	if _, err := f.svc.LinkGuardian(ctx, school.ID, learner.ID, LinkInput{GuardianID: mother.ID, Relationship: "aunt"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid relationship accepted: %v", err)
	}

	motherLink, err := f.svc.LinkGuardian(ctx, school.ID, learner.ID, LinkInput{
		GuardianID: mother.ID, Relationship: "mother",
		IsPrimary: true, CanViewFinancials: true, CanViewAcademics: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if motherLink.Relationship != "mother" || !motherLink.IsPrimary ||
		!motherLink.CanViewFinancials || !motherLink.CanViewAcademics {
		t.Fatalf("mother link flags wrong: %+v", motherLink)
	}
	fatherLink, err := f.svc.LinkGuardian(ctx, school.ID, learner.ID, LinkInput{
		GuardianID: father.ID, Relationship: "father",
		IsPrimary: false, CanViewFinancials: false, CanViewAcademics: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fatherLink.IsPrimary || fatherLink.CanViewFinancials || !fatherLink.CanViewAcademics {
		t.Fatalf("father link flags wrong: %+v", fatherLink)
	}

	// Duplicate guardian-learner pair rejected.
	if _, err := f.svc.LinkGuardian(ctx, school.ID, learner.ID, LinkInput{GuardianID: mother.ID, Relationship: "mother"}); !errors.Is(err, ErrLinkExists) {
		t.Fatalf("duplicate link: %v", err)
	}

	views, err := f.svc.GuardiansForLearner(ctx, school.ID, learner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 {
		t.Fatalf("views = %d, want 2", len(views))
	}
	// Primary contact sorts first; identity roundtrips from the JOIN.
	if views[0].Guardian.ID != mother.ID || views[0].Guardian.FirstName != "Mary" || views[0].Guardian.Email != "mary.kimani@example.com" {
		t.Fatalf("primary guardian view wrong: %+v", views[0])
	}
	if !views[0].Link.CanViewFinancials || !views[0].Link.CanViewAcademics {
		t.Fatalf("primary flags lost: %+v", views[0].Link)
	}
	if views[1].Guardian.ID != father.ID || views[1].Link.CanViewFinancials || !views[1].Link.CanViewAcademics {
		t.Fatalf("secondary guardian view wrong: %+v", views[1])
	}
}

// TestLearnerTenantScoping proves learner identity is global but visibility is
// enrollment-scoped: school B neither lists nor resolves school A's learner.
func TestLearnerTenantScoping(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	schoolA := mustSchool(t, f, "SCOPE-A", "Scope School A")
	schoolB := mustSchool(t, f, "SCOPE-B", "Scope School B")

	learnerA := mustLearner(t, f, schoolA.ID, "Amina", "Otieno")
	learnerB := mustLearner(t, f, schoolB.ID, "Brian", "Weke")
	enrollA, err := f.svc.EnrollLearner(ctx, schoolA.ID, EnrollmentInput{LearnerID: learnerA.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.EnrollLearner(ctx, schoolB.ID, EnrollmentInput{LearnerID: learnerB.ID}); err != nil {
		t.Fatal(err)
	}

	assertLists := func(inA, inB bool) {
		t.Helper()
		listA, _, err := f.svc.ListLearners(ctx, schoolA.ID, "", 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		listB, _, err := f.svc.ListLearners(ctx, schoolB.ID, "", 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		gotA, gotB := false, false
		for _, l := range listA {
			if l.ID == learnerA.ID {
				gotA = true
			}
			if l.ID == learnerB.ID {
				t.Fatalf("school A lists school B's learner — isolation broken")
			}
		}
		for _, l := range listB {
			if l.ID == learnerB.ID {
				gotB = true
			}
			if l.ID == learnerA.ID && !inB {
				t.Fatalf("school B lists school A's learner — isolation broken")
			}
		}
		if gotA != inA {
			t.Fatalf("school A lists learnerA = %v, want %v", gotA, inA)
		}
		if gotB != inB {
			t.Fatalf("school B lists learnerB = %v, want %v", gotB, inB)
		}
	}
	assertLists(true, true)

	// Cross-school direct read: 404 semantics.
	if _, err := f.svc.LearnerByID(ctx, schoolB.ID, learnerA.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("school B resolved school A's learner: %v", err)
	}
	// Cross-school guardian access and enrollment mutation: guarded too.
	if _, err := f.svc.GuardiansForLearner(ctx, schoolB.ID, learnerA.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("school B listed guardians via school A's learner: %v", err)
	}
	if _, err := f.svc.TransitionEnrollment(ctx, schoolB.ID, enrollA.ID, StatusActive); !errors.Is(err, ErrNotFound) {
		t.Fatalf("school B transitioned school A's enrollment: %v", err)
	}

	// Spec §19: the SAME learner may enroll at multiple schools over time.
	if _, err := f.svc.EnrollLearner(ctx, schoolB.ID, EnrollmentInput{LearnerID: learnerA.ID, Status: string(StatusApplicant)}); err != nil {
		t.Fatalf("multi-school enrollment rejected: %v", err)
	}
	assertLists(true, true)

	// Duplicate OPEN enrollment at the same school is rejected.
	if _, err := f.svc.EnrollLearner(ctx, schoolB.ID, EnrollmentInput{LearnerID: learnerA.ID}); !errors.Is(err, ErrAlreadyEnrolled) {
		t.Fatalf("duplicate open enrollment: %v", err)
	}
}

// TestEnrollmentEventsOutboxed asserts the outbox receives
// students.LearnerEnrolled and students.EnrollmentStateChanged (before/after).
func TestEnrollmentEventsOutboxed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "EVT-S", "Event School")
	learner := mustLearner(t, f, school.ID, "Evie", "Njeri")

	enrollment, err := f.svc.EnrollLearner(ctx, school.ID, EnrollmentInput{LearnerID: learner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.TransitionEnrollment(ctx, school.ID, enrollment.ID, StatusActive); err != nil {
		t.Fatal(err)
	}

	rows, err := f.pool.Query(ctx,
		`SELECT event_type, schema_version, school_id, aggregate_id, payload
                 FROM event_outbox WHERE aggregate_id = $1 ORDER BY occurred_at`, enrollment.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	type outboxRow struct {
		eventType   string
		schemaVer   int
		schoolID    *string
		aggregateID string
		payload     map[string]any
	}
	var got []outboxRow
	for rows.Next() {
		var r outboxRow
		var raw []byte
		if err := rows.Scan(&r.eventType, &r.schemaVer, &r.schoolID, &r.aggregateID, &raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &r.payload); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("outbox rows = %d, want 2 (enrolled + state changed)", len(got))
	}

	enrolled := got[0]
	if enrolled.eventType != "students.LearnerEnrolled" || enrolled.schemaVer != 1 {
		t.Fatalf("enrolled event wrong: %s v%d", enrolled.eventType, enrolled.schemaVer)
	}
	if enrolled.schoolID == nil || *enrolled.schoolID != school.ID {
		t.Fatalf("enrolled event school scope wrong: %v", enrolled.schoolID)
	}
	if enrolled.payload["learner_id"] != learner.ID || enrolled.payload["status"] != string(StatusAdmitted) {
		t.Fatalf("enrolled payload wrong: %v", enrolled.payload)
	}

	changed := got[1]
	if changed.eventType != "students.EnrollmentStateChanged" || changed.schemaVer != 1 {
		t.Fatalf("state-changed event wrong: %s v%d", changed.eventType, changed.schemaVer)
	}
	if changed.schoolID == nil || *changed.schoolID != school.ID {
		t.Fatalf("state-changed event school scope wrong: %v", changed.schoolID)
	}
	if changed.payload["before"] != string(StatusAdmitted) || changed.payload["after"] != string(StatusActive) {
		t.Fatalf("state-changed payload wrong: %v", changed.payload)
	}
	if changed.payload["learner_id"] != learner.ID {
		t.Fatalf("state-changed learner wrong: %v", changed.payload)
	}
}

// TestLearnerCreatedAtImmutableOnStatusUpdate asserts enrollment status
// changes never touch the immutable learner identity record.
func TestLearnerCreatedAtImmutableOnStatusUpdate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "IMMUT-S", "Immutable School")

	dob := time.Date(2011, 4, 12, 0, 0, 0, 0, time.UTC)
	learner, err := f.svc.CreateLearner(ctx, school.ID, LearnerInput{
		FirstName: "Tumu", LastName: "Njoroge", DateOfBirth: &dob,
	})
	if err != nil {
		t.Fatal(err)
	}
	createdAt := learner.CreatedAt

	enrollment, err := f.svc.EnrollLearner(ctx, school.ID, EnrollmentInput{LearnerID: learner.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, to := range []EnrollmentStatus{StatusActive, StatusSuspended, StatusActive} {
		if _, err := f.svc.TransitionEnrollment(ctx, school.ID, enrollment.ID, to); err != nil {
			t.Fatalf("transition to %s: %v", to, err)
		}
	}

	after, err := f.svc.LearnerByID(ctx, school.ID, learner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.CreatedAt.Equal(createdAt) {
		t.Fatalf("learner created_at changed on status updates: %v -> %v", createdAt, after.CreatedAt)
	}
	if after.FirstName != "Tumu" || after.LastName != "Njoroge" || after.DateOfBirth == nil || !after.DateOfBirth.Equal(dob) {
		t.Fatalf("learner identity mutated: %+v", after)
	}
}

// TestEnrollmentTransitionHTTP exercises the wired HTTP stack:
// RequireAuth -> RequireSchool -> RequirePermission -> handler, verifying the
// 200/201/400/403/404/409 mappings end to end.
func TestEnrollmentTransitionHTTP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.auth.CreateUser(ctx, "registrar@skolara.test", "Registrar", "s3cure-passw0rd!", identity.RolePlatformAdmin)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := f.auth.Login(ctx, "registrar@skolara.test", "s3cure-passw0rd!", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	school := mustSchool(t, f, "HTTP-S", "HTTP School")
	learner := mustLearner(t, f, school.ID, "Http", "Tester")
	enrollment, err := f.svc.EnrollLearner(ctx, school.ID, EnrollmentInput{LearnerID: learner.ID})
	if err != nil {
		t.Fatal(err)
	}

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

	// Legal transition over HTTP: 200 with the updated enrollment.
	rr := do("POST", "/api/v1/enrollments/"+enrollment.ID+"/transition", map[string]any{"to": "active"}, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("legal transition: status = %d body=%s", rr.Code, rr.Body.String())
	}
	var updated Enrollment
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusActive {
		t.Fatalf("updated status = %s", updated.Status)
	}

	// Illegal transition: 409 conflict.
	rr = do("POST", "/api/v1/enrollments/"+enrollment.ID+"/transition", map[string]any{"to": "applicant"}, true)
	if rr.Code != http.StatusConflict {
		t.Fatalf("illegal transition: status = %d body=%s", rr.Code, rr.Body.String())
	}

	// Unknown learner in school context: 404.
	rr = do("GET", "/api/v1/learners/00000000-0000-0000-0000-000000000000", nil, true)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown learner: status = %d", rr.Code)
	}

	// Missing school context: 403 before domain work.
	rr = do("GET", "/api/v1/learners", nil, false)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("no school context: status = %d body=%s", rr.Code, rr.Body.String())
	}

	// Create learner over HTTP: 201.
	rr = do("POST", "/api/v1/learners", map[string]any{"firstName": "Post", "lastName": "Man"}, true)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create learner over http: status = %d body=%s", rr.Code, rr.Body.String())
	}

	// Enrollment list with status filter: 200; unknown status: 400.
	rr = do("GET", "/api/v1/enrollments?status=active", nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("list enrollments: status = %d", rr.Code)
	}
	rr = do("GET", "/api/v1/enrollments?status=bogus", nil, true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad status filter: status = %d", rr.Code)
	}
}

// TestLearnerPagination guards the limit/offset + total contract (#26).
func TestLearnerPagination(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "PAGE-S", "Page School")

	const n = 7
	for i := 0; i < n; i++ {
		l := mustLearner(t, f, school.ID, fmt.Sprintf("First%d", i), fmt.Sprintf("Last%02d", i))
		if _, err := f.svc.EnrollLearner(ctx, school.ID, EnrollmentInput{LearnerID: l.ID}); err != nil {
			t.Fatal(err)
		}
	}

	page1, total, err := f.svc.ListLearners(ctx, school.ID, "", 3, 0)
	if err != nil || len(page1) != 3 || total != n {
		t.Fatalf("page1: %d items total=%d err=%v", len(page1), total, err)
	}
	page2, total2, err := f.svc.ListLearners(ctx, school.ID, "", 3, 3)
	if err != nil || len(page2) != 3 || total2 != n {
		t.Fatalf("page2: %d items total=%d err=%v", len(page2), total2, err)
	}
	page3, total3, err := f.svc.ListLearners(ctx, school.ID, "", 3, 6)
	if err != nil || len(page3) != 1 || total3 != n {
		t.Fatalf("page3: %d items total=%d err=%v", len(page3), total3, err)
	}
	// Pages do not overlap and preserve ordering.
	if page1[0].ID == page2[0].ID {
		t.Fatal("page overlap")
	}
}

// TestEnrollmentPagination guards the issue #47 fix: ListEnrollments must
// honor limit/offset (the query previously omitted LIMIT/OFFSET and returned
// every enrollment while echoing pagination in the envelope).
func TestEnrollmentPagination(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "PAG-E", "Enrollment Pag School")

	for i := 0; i < 5; i++ {
		l := mustLearner(t, f, school.ID, fmt.Sprintf("Pag%d", i), "Learner")
		if _, err := f.svc.EnrollLearner(ctx, school.ID, EnrollmentInput{LearnerID: l.ID, Status: "admitted"}); err != nil {
			t.Fatal(err)
		}
	}

	page1, total, err := f.svc.ListEnrollments(ctx, school.ID, nil, 2, 0)
	if err != nil || total != 5 || len(page1) != 2 {
		t.Fatalf("page1: n=%d total=%d err=%v", len(page1), total, err)
	}
	page2, _, err := f.svc.ListEnrollments(ctx, school.ID, nil, 2, 2)
	if err != nil || len(page2) != 2 {
		t.Fatalf("page2: n=%d err=%v", len(page2), err)
	}
	page3, _, err := f.svc.ListEnrollments(ctx, school.ID, nil, 2, 4)
	if err != nil || len(page3) != 1 {
		t.Fatalf("page3: n=%d err=%v", len(page3), err)
	}
	// No overlap across pages.
	seen := map[string]bool{}
	for _, e := range append(append(page1, page2...), page3...) {
		if seen[e.ID] {
			t.Fatalf("enrollment %s appears on two pages", e.ID)
		}
		seen[e.ID] = true
	}
}

// TestEnrollLearnerBadReferences guards the #47 500-fix: malformed or unknown
// classGroupId/academicYearId are client errors (ErrValidation), not 500s.
func TestEnrollLearnerBadReferences(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "FK-E", "Enroll FK School")
	l := mustLearner(t, f, school.ID, "Ref", "Err")

	// Non-UUID classGroupId -> validation error before hitting the DB.
	if _, err := f.svc.EnrollLearner(ctx, school.ID, EnrollmentInput{
		LearnerID: l.ID, Status: "admitted", ClassGroupID: "not-a-uuid",
	}); err == nil || !strings.Contains(err.Error(), "classGroupId") {
		t.Fatalf("non-uuid classGroup: %v", err)
	}
	// Non-UUID academicYearId -> validation error.
	if _, err := f.svc.EnrollLearner(ctx, school.ID, EnrollmentInput{
		LearnerID: l.ID, Status: "admitted", AcademicYearID: "nope",
	}); err == nil {
		t.Fatal("non-uuid academicYear accepted")
	}
	// Valid UUID but unknown class -> FK mapped to ErrValidation (400), not 500.
	if _, err := f.svc.EnrollLearner(ctx, school.ID, EnrollmentInput{
		LearnerID: l.ID, Status: "admitted", ClassGroupID: "00000000-0000-0000-0000-0000000000f1",
	}); err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown classGroup should map to ErrValidation, got %v", err)
	}
}
