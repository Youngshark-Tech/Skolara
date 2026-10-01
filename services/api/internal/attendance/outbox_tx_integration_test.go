//go:build integration

package attendance

import (
	"context"
	"testing"
)

// hideEventOutbox makes the outbox temporarily unwritable so the event insert
// inside a mutation transaction fails — forcing the rollback the test asserts on.
func hideEventOutbox(t *testing.T, f *fixture) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `ALTER TABLE event_outbox RENAME TO event_outbox_hidden`); err != nil {
		t.Fatalf("hide outbox: %v", err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `ALTER TABLE event_outbox_hidden RENAME TO event_outbox`)
	})
}

func restoreEventOutbox(t *testing.T, f *fixture) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `ALTER TABLE event_outbox_hidden RENAME TO event_outbox`); err != nil {
		t.Fatalf("restore outbox: %v", err)
	}
}

// TestAttendanceMutationAndEventShareTransaction proves ADR-003 at the
// attendance call sites (issue #52): the record batch / session status flip
// and their outbox events commit or roll back as ONE transaction — a forced
// event failure must undo the mutation, leaving neither row behind.
func TestAttendanceMutationAndEventShareTransaction(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "OBX-AT", "Outbox Attendance School")
	actor := mustActor(t, f, "att-outbox@skolara.test")
	class := seedClass(t, f, school.ID, "Grade 9")
	l1 := seedLearner(t, f, "Ora", "One")
	l2 := seedLearner(t, f, "Pia", "Two")
	enrollLearner(t, f, l1, school.ID)
	enrollLearner(t, f, l2, school.ID)

	sess, err := f.svc.OpenSession(ctx, school.ID, class, "2026-06-01", actor)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}

	// Success: the record batch and its absence event both land.
	if err := f.svc.SubmitRecords(ctx, school.ID, sess.ID, actor,
		[]RecordInput{{LearnerID: l1, Status: StatusAbsent, Reason: "sick", ClientMutationID: "obx-mut-1"}}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	records, err := f.svc.Records(ctx, school.ID, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records %d, want 1", len(records))
	}
	if got := eventCount(t, f, "attendance.AbsenceRecorded", school.ID); got != 1 {
		t.Fatalf("AbsenceRecorded events %d, want 1", got)
	}

	// Event failure: with the outbox unwritable the whole transaction must
	// roll back — the record may NOT survive without its event (the dual
	// write bug fixed in #52). Outbox-row assertions happen after the restore
	// (the table is renamed while hidden).
	hideEventOutbox(t, f)
	if err := f.svc.SubmitRecords(ctx, school.ID, sess.ID, actor,
		[]RecordInput{{LearnerID: l2, Status: StatusAbsent, Reason: "late bus", ClientMutationID: "obx-mut-2"}}); err == nil {
		t.Fatal("submit succeeded while outbox was unwritable")
	}
	rolledRecords, err := f.svc.Records(ctx, school.ID, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rolledRecords) != 1 {
		t.Fatalf("records after forced rollback %d, want 1 (mutation leaked)", len(rolledRecords))
	}
	// The close transition is event-coupled too: a forced event failure must
	// leave the session open (status flip rolled back with its event).
	if _, err := f.svc.CloseSession(ctx, school.ID, sess.ID); err == nil {
		t.Fatal("close succeeded while outbox was unwritable")
	}
	sessAfter, err := f.svc.Session(ctx, school.ID, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sessAfter.Status != SessionOpen {
		t.Fatalf("session status %q after forced rollback, want open (mutation leaked)", sessAfter.Status)
	}
	restoreEventOutbox(t, f)

	// Restored: the close transition now lands together with its event.
	closed, err := f.svc.CloseSession(ctx, school.ID, sess.ID)
	if err != nil {
		t.Fatalf("close after restore: %v", err)
	}
	if closed.Status != SessionClosed {
		t.Fatalf("session status %q after close, want closed", closed.Status)
	}
	if got := eventCount(t, f, "attendance.SessionClosed", school.ID); got != 1 {
		t.Fatalf("SessionClosed events %d, want 1", got)
	}
	// The rolled-back submit attempt left no event behind either.
	if got := eventCount(t, f, "attendance.AbsenceRecorded", school.ID); got != 1 {
		t.Fatalf("AbsenceRecorded events after rollback %d, want 1", got)
	}
}
