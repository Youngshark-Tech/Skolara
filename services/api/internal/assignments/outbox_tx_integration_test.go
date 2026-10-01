//go:build integration

package assignments

import (
	"context"
	"testing"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
)

// outboxEventCountForSchool counts outbox rows of a type scoped to a school.
func outboxEventCountForSchool(t *testing.T, pool *postgres.Pool, eventType, schoolID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM event_outbox WHERE event_type = $1 AND school_id = $2`, eventType, schoolID).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

// hideEventOutbox makes the outbox temporarily unwritable so the event insert
// inside a mutation transaction fails — forcing the rollback the test asserts on.
func hideEventOutbox(t *testing.T, pool *postgres.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `ALTER TABLE event_outbox RENAME TO event_outbox_hidden`); err != nil {
		t.Fatalf("hide outbox: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `ALTER TABLE event_outbox_hidden RENAME TO event_outbox`)
	})
}

func restoreEventOutbox(t *testing.T, pool *postgres.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `ALTER TABLE event_outbox_hidden RENAME TO event_outbox`); err != nil {
		t.Fatalf("restore outbox: %v", err)
	}
}

// TestAssignmentMutationAndEventShareTransaction proves ADR-003 at the
// assignments call sites (issue #52): publish/grade transitions and their
// outbox events commit or roll back as ONE transaction — a forced event
// failure must undo the mutation, leaving neither row behind.
func TestAssignmentMutationAndEventShareTransaction(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "OBX-AS", "Outbox Assignments School")
	teacher := mustUser(t, f, "asg-outbox@skolara.test")
	classID, subjectID := seedClassMaterial(t, f, school.ID, "Outbox Form 1")
	learner := seedLearner(t, f, "Quinn", "One")
	seedRoster(t, f, classID, learner)

	a1 := mustAssignment(t, f, school.ID, teacher, classID, subjectID, "Outbox Publish", futureDate())
	a2 := mustAssignment(t, f, school.ID, teacher, classID, subjectID, "Outbox Rollback", futureDate())

	// Success: the publish transition and its event both land.
	if _, err := f.svc.PublishAssignment(ctx, school.ID, teacher, a1.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := outboxEventCountForSchool(t, f.pool, "assignments.AssignmentPublished", school.ID); got != 1 {
		t.Fatalf("AssignmentPublished events %d, want 1", got)
	}

	// Submission for the grading leg (assignment must be published).
	if _, err := f.svc.SubmitAssignment(ctx, school.ID, a1.ID, learner, "my answer"); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Event failure: with the outbox unwritable the whole transaction must
	// roll back — the assignment may NOT become published without its event
	// (the dual write bug fixed in #52).
	hideEventOutbox(t, f.pool)
	if _, err := f.svc.PublishAssignment(ctx, school.ID, teacher, a2.ID); err == nil {
		t.Fatal("publish succeeded while outbox was unwritable")
	}
	rolled, err := f.svc.Assignment(ctx, school.ID, a2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolled.Status != AssignmentDraft {
		t.Fatalf("assignment status %q after forced rollback, want draft (mutation leaked)", rolled.Status)
	}
	// Grading is event-coupled as well: a forced event failure keeps the
	// submission in 'submitted'.
	if _, err := f.svc.GradeAssignment(ctx, school.ID, teacher, a1.ID, learner, "A", "well done"); err == nil {
		t.Fatal("grade succeeded while outbox was unwritable")
	}
	subs, err := f.svc.Submissions(ctx, school.ID, teacher, a1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Status != SubmissionSubmitted {
		t.Fatalf("submission %+v after forced rollback, want status submitted (mutation leaked)", subs)
	}
	restoreEventOutbox(t, f.pool)

	// Restored: both write paths land together with their events.
	if _, err := f.svc.PublishAssignment(ctx, school.ID, teacher, a2.ID); err != nil {
		t.Fatalf("publish after restore: %v", err)
	}
	if got := outboxEventCountForSchool(t, f.pool, "assignments.AssignmentPublished", school.ID); got != 2 {
		t.Fatalf("AssignmentPublished events %d, want 2", got)
	}
	if _, err := f.svc.GradeAssignment(ctx, school.ID, teacher, a1.ID, learner, "A", "well done"); err != nil {
		t.Fatalf("grade after restore: %v", err)
	}
	if got := outboxEventCountForSchool(t, f.pool, "assignments.SubmissionGraded", school.ID); got != 1 {
		t.Fatalf("SubmissionGraded events %d, want 1", got)
	}
}
