//go:build integration

package attendance

import (
	"bytes"
	"context"
	"encoding/json"
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

// mustActor creates a real identity user (sessions.created_by and
// records.recorded_by FK users(id)).
func mustActor(t *testing.T, f *fixture, email string) string {
	t.Helper()
	u, err := f.auth.CreateUser(context.Background(), email, email, "s3cure-passw0rd!", "")
	if err != nil {
		t.Fatalf("seed actor: %v", err)
	}
	return u.ID
}

// seedClass inserts an academics class_group via raw SQL — attendance
// consumes class ids through FKs WITHOUT importing the academics package.
func seedClass(t *testing.T, f *fixture, schoolID, name string) string {
	t.Helper()
	yearID := uuid.NewString()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO academic_years (id, school_id, name, start_date, end_date) VALUES ($1,$2,$3,'2026-01-01','2026-12-31')`,
		yearID, schoolID, "2026"); err != nil {
		t.Fatalf("seed academic year: %v", err)
	}
	id := uuid.NewString()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO class_groups (id, school_id, academic_year_id, name) VALUES ($1,$2,$3,$4)`,
		id, schoolID, yearID, name); err != nil {
		t.Fatalf("seed class: %v", err)
	}
	return id
}

// seedLearner inserts a learner via raw SQL (no students import).
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

// enrollLearner inserts an ACTIVE enrollment at the school via raw SQL
// (issue #49: attendance records are enrollment-scoped).
func enrollLearner(t *testing.T, f *fixture, learnerID, schoolID string) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(),
		`INSERT INTO enrollments (id, school_id, learner_id, status, started_at)
		 VALUES (gen_random_uuid(), $1, $2, 'active', now())`, schoolID, learnerID)
	if err != nil {
		t.Fatalf("enroll learner: %v", err)
	}
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

func TestAttendanceRecordIdempotentReplay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "ATT-S", "Attendance School")
	actor1 := mustActor(t, f, "att-replay@skolara.test")
	class := seedClass(t, f, school.ID, "Grade 3")
	l1 := seedLearner(t, f, "Ada", "One")
	l2 := seedLearner(t, f, "Ben", "Two")
	enrollLearner(t, f, l1, school.ID)
	enrollLearner(t, f, l2, school.ID)

	sess, err := f.svc.OpenSession(ctx, school.ID, class, "2026-05-04", actor1)
	if err != nil {
		t.Fatal(err)
	}
	// Convergent reopen: same class+date returns the SAME session.
	again, err := f.svc.OpenSession(ctx, school.ID, class, "2026-05-04", actor1)
	if err != nil || again.ID != sess.ID {
		t.Fatalf("reopen mismatch: %v %s vs %s", err, again.ID, sess.ID)
	}

	batch := []RecordInput{
		{LearnerID: l1, Status: StatusPresent, ClientMutationID: "mut-1"},
		{LearnerID: l2, Status: StatusAbsent, Reason: "Sick", ClientMutationID: "mut-2"},
	}
	if err := f.svc.SubmitRecords(ctx, school.ID, sess.ID, actor1, batch); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// EXACT replay (offline retry): no duplicates, no error.
	if err := f.svc.SubmitRecords(ctx, school.ID, sess.ID, actor1, batch); err != nil {
		t.Fatalf("replay errored: %v", err)
	}
	records, err := f.svc.Records(ctx, school.ID, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2 after replay", len(records))
	}
	if records[0].Status != StatusPresent || records[1].Status != StatusAbsent {
		t.Fatalf("statuses wrong: %+v", records)
	}
}

func TestAttendanceBulkUpsertCorrection(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "ATT-C", "Correction School")
	actor1 := mustActor(t, f, "att-correction@skolara.test")
	class := seedClass(t, f, school.ID, "Grade 5")
	l1 := seedLearner(t, f, "Cara", "Three")
	enrollLearner(t, f, l1, school.ID)

	sess, _ := f.svc.OpenSession(ctx, school.ID, class, "2026-05-05", actor1)
	if err := f.svc.SubmitRecords(ctx, school.ID, sess.ID, actor1,
		[]RecordInput{{LearnerID: l1, Status: StatusAbsent, Reason: "unknown", ClientMutationID: "mut-a"}}); err != nil {
		t.Fatal(err)
	}
	// Correction with a NEW mutation id flips the outcome.
	if err := f.svc.SubmitRecords(ctx, school.ID, sess.ID, actor1,
		[]RecordInput{{LearnerID: l1, Status: StatusLate, Reason: "bus", ClientMutationID: "mut-b"}}); err != nil {
		t.Fatal(err)
	}
	records, _ := f.svc.Records(ctx, school.ID, sess.ID)
	if len(records) != 1 || records[0].Status != StatusLate || records[0].Reason != "bus" {
		t.Fatalf("correction failed: %+v", records)
	}
	// Reusing a mutation id for a DIFFERENT learner is a client bug: rejected.
	l2 := seedLearner(t, f, "Dan", "Four")
	enrollLearner(t, f, l2, school.ID)
	if err := f.svc.SubmitRecords(ctx, school.ID, sess.ID, actor1,
		[]RecordInput{{LearnerID: l2, Status: StatusPresent, ClientMutationID: "mut-a"}}); err == nil {
		t.Fatal("mutation id reuse across learners accepted")
	}
}

func TestAttendanceClosedSessionAndValidation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "ATT-V", "Validation School")
	actor1 := mustActor(t, f, "att-validation@skolara.test")
	class := seedClass(t, f, school.ID, "Grade 6")
	l1 := seedLearner(t, f, "Eve", "Five")
	enrollLearner(t, f, l1, school.ID)

	if _, err := f.svc.OpenSession(ctx, school.ID, class, "04/05/2026", actor1); err == nil {
		t.Fatal("malformed date accepted")
	}
	if _, err := f.svc.OpenSession(ctx, school.ID, "00000000-0000-0000-0000-000000000000", "2026-05-06", actor1); err == nil {
		t.Fatal("unknown class accepted")
	}

	sess, _ := f.svc.OpenSession(ctx, school.ID, class, "2026-05-06", actor1)
	if err := f.svc.SubmitRecords(ctx, school.ID, sess.ID, actor1,
		[]RecordInput{{LearnerID: l1, Status: "sleeping", ClientMutationID: "mut-x"}}); err == nil {
		t.Fatal("invalid status accepted")
	}
	if err := f.svc.SubmitRecords(ctx, school.ID, sess.ID, actor1,
		[]RecordInput{{LearnerID: l1, Status: StatusPresent, ClientMutationID: ""}}); err == nil {
		t.Fatal("missing mutation id accepted")
	}

	closed, err := f.svc.CloseSession(ctx, school.ID, sess.ID)
	if err != nil || closed.Status != SessionClosed {
		t.Fatalf("close: %v %s", err, closed.Status)
	}
	if err := f.svc.SubmitRecords(ctx, school.ID, sess.ID, actor1,
		[]RecordInput{{LearnerID: l1, Status: StatusPresent, ClientMutationID: "mut-y"}}); err != ErrSessionClosed {
		t.Fatalf("records on closed session: %v", err)
	}
}

func TestAttendanceTenantIsolationAndEvents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sA := mustSchool(t, f, "ATT-A", "Att A")
	sB := mustSchool(t, f, "ATT-B", "Att B")
	actor1 := mustActor(t, f, "att-iso@skolara.test")
	actor2 := mustActor(t, f, "att-iso-b@skolara.test")
	classA := seedClass(t, f, sA.ID, "A Class")
	lA := seedLearner(t, f, "Fay", "Six")
	enrollLearner(t, f, lA, sA.ID)

	sessA, _ := f.svc.OpenSession(ctx, sA.ID, classA, "2026-05-07", actor1)

	// School B cannot resolve, record into, or close school A's session.
	if _, err := f.svc.Session(ctx, sB.ID, sessA.ID); err == nil {
		t.Fatal("school B resolved school A session")
	}
	if err := f.svc.SubmitRecords(ctx, sB.ID, sessA.ID, actor2,
		[]RecordInput{{LearnerID: lA, Status: StatusPresent, ClientMutationID: "mut-iso"}}); err == nil {
		t.Fatal("school B wrote into school A session")
	}
	if _, err := f.svc.CloseSession(ctx, sB.ID, sessA.ID); err == nil {
		t.Fatal("school B closed school A session")
	}

	// Absence events outboxed with school scope and payload.
	if err := f.svc.SubmitRecords(ctx, sA.ID, sessA.ID, actor1,
		[]RecordInput{{LearnerID: lA, Status: StatusAbsent, Reason: "sick", ClientMutationID: "mut-abs"}}); err != nil {
		t.Fatal(err)
	}
	var payload []byte
	if err := f.pool.QueryRow(ctx,
		`SELECT payload FROM event_outbox WHERE event_type = 'attendance.AbsenceRecorded' AND school_id = $1`, sA.ID).
		Scan(&payload); err != nil {
		t.Fatalf("absence event not outboxed: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got["learner_id"] != lA || got["date"] != "2026-05-07" || got["reason"] != "sick" {
		t.Fatalf("absence payload wrong: %s", payload)
	}

	// Session close event.
	if _, err := f.svc.CloseSession(ctx, sA.ID, sessA.ID); err != nil {
		t.Fatal(err)
	}
	if n := eventCount(t, f, "attendance.SessionClosed", sA.ID); n != 1 {
		t.Fatalf("SessionClosed events = %d, want 1", n)
	}
}

func TestAttendanceHTTPFlow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.auth.CreateUser(ctx, "att-admin@skolara.test", "Att Admin", "s3cure-passw0rd!", identity.RolePlatformAdmin); err != nil {
		t.Fatal(err)
	}
	token, _, err := f.auth.Login(ctx, "att-admin@skolara.test", "s3cure-passw0rd!", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	school := mustSchool(t, f, "ATT-H", "HTTP Attendance")
	class := seedClass(t, f, school.ID, "Grade 7")
	l1 := seedLearner(t, f, "Gil", "Seven")
	enrollLearner(t, f, l1, school.ID)

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

	// Create session: 200 (convergent semantics), identical retry same id.
	rr := do("POST", "/api/v1/attendance/sessions", map[string]any{"classGroupId": class, "date": "2026-05-08"}, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("create session: %d %s", rr.Code, rr.Body.String())
	}
	var sess AttendanceSession
	_ = json.Unmarshal(rr.Body.Bytes(), &sess)
	rr = do("POST", "/api/v1/attendance/sessions", map[string]any{"classGroupId": class, "date": "2026-05-08"}, true)
	var again AttendanceSession
	_ = json.Unmarshal(rr.Body.Bytes(), &again)
	if again.ID != sess.ID {
		t.Fatalf("retry created a new session %s != %s", again.ID, sess.ID)
	}

	// Bulk records: 204; listing: 200 with one record.
	rr = do("POST", fmt.Sprintf("/api/v1/attendance/sessions/%s/records", sess.ID),
		map[string]any{"records": []map[string]any{
			{"learnerId": l1, "status": "present", "clientMutationId": "http-mut-1"},
		}}, true)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("records: %d %s", rr.Code, rr.Body.String())
	}
	rr = do("GET", fmt.Sprintf("/api/v1/attendance/sessions/%s/records", sess.ID), nil, true)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(l1)) {
		t.Fatalf("list records: %d %s", rr.Code, rr.Body.String())
	}

	// Invalid status: 400.
	rr = do("POST", fmt.Sprintf("/api/v1/attendance/sessions/%s/records", sess.ID),
		map[string]any{"records": []map[string]any{
			{"learnerId": l1, "status": "ghost", "clientMutationId": "http-mut-2"},
		}}, true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid status: %d", rr.Code)
	}

	// Session listing with date filter: 200.
	rr = do("GET", "/api/v1/attendance/sessions?date=2026-05-08", nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("list sessions: %d", rr.Code)
	}

	// Close: 200; records afterwards: 409.
	rr = do("POST", fmt.Sprintf("/api/v1/attendance/sessions/%s/close", sess.ID), nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("close: %d", rr.Code)
	}
	rr = do("POST", fmt.Sprintf("/api/v1/attendance/sessions/%s/records", sess.ID),
		map[string]any{"records": []map[string]any{
			{"learnerId": l1, "status": "late", "clientMutationId": "http-mut-3"},
		}}, true)
	if rr.Code != http.StatusConflict {
		t.Fatalf("records after close: %d", rr.Code)
	}

	// Missing school context: 403.
	rr = do("GET", "/api/v1/attendance/sessions", nil, false)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("no school context: %d", rr.Code)
	}
}

// TestAttendanceEnrollmentScopeAndCloseIdempotency guards the #49 fixes:
// (1) records require an open enrollment at the acting school (replays of
// already-recorded learners still succeed); (2) closing an already-closed
// session is idempotent (200 semantics, no duplicate SessionClosed event);
// (3) mutation-id uniqueness is per school, not global.
func TestAttendanceEnrollmentScopeAndCloseIdempotency(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sA := mustSchool(t, f, "ATT-ES", "Enroll Scope School")
	sB := mustSchool(t, f, "ATT-ESB", "Other School")
	actor := mustActor(t, f, "att-scope@skolara.test")
	class := seedClass(t, f, sA.ID, "Grade 8")
	local := seedLearner(t, f, "Local", "Kid")
	enrollLearner(t, f, local, sA.ID)
	foreign := seedLearner(t, f, "Foreign", "Kid")
	enrollLearner(t, f, foreign, sB.ID)

	sess, err := f.svc.OpenSession(ctx, sA.ID, class, "2026-05-08", actor)
	if err != nil {
		t.Fatal(err)
	}

	// Foreign (enrolled only at school B) learner rejected.
	if err := f.svc.SubmitRecords(ctx, sA.ID, sess.ID, actor,
		[]RecordInput{{LearnerID: foreign, Status: StatusPresent, ClientMutationID: "scope-1"}}); err == nil || !strings.Contains(err.Error(), "no open enrollment") {
		t.Fatalf("cross-school attendance accepted: %v", err)
	}

	// Local learner accepted; replay with the same mutation id still succeeds
	// (idempotent) even though the check path is exercised again.
	if err := f.svc.SubmitRecords(ctx, sA.ID, sess.ID, actor,
		[]RecordInput{{LearnerID: local, Status: StatusPresent, ClientMutationID: "scope-2"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SubmitRecords(ctx, sA.ID, sess.ID, actor,
		[]RecordInput{{LearnerID: local, Status: StatusPresent, ClientMutationID: "scope-2"}}); err != nil {
		t.Fatalf("replay rejected: %v", err)
	}

	// Close twice: second close is idempotent and does not re-emit the event.
	if _, err := f.svc.CloseSession(ctx, sA.ID, sess.ID); err != nil {
		t.Fatal(err)
	}
	closed, err := f.svc.CloseSession(ctx, sA.ID, sess.ID)
	if err != nil || closed.Status != SessionClosed {
		t.Fatalf("double close: %v %s", err, closed.Status)
	}
	var events int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM event_outbox WHERE event_type='attendance.SessionClosed' AND school_id=$1 AND aggregate_id=$2`,
		sA.ID, sess.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("SessionClosed events = %d, want exactly 1", events)
	}

	// Same mutation id at another school is independent (per-school scope).
	sessB, err := f.svc.OpenSession(ctx, sB.ID, seedClass(t, f, sB.ID, "B Class"), "2026-05-08", actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SubmitRecords(ctx, sB.ID, sessB.ID, actor,
		[]RecordInput{{LearnerID: foreign, Status: StatusPresent, ClientMutationID: "scope-2"}}); err != nil {
		t.Fatalf("per-school mutation-id scope violated: %v", err)
	}
}
