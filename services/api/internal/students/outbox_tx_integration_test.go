//go:build integration

package students

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

func learnerCount(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM learners`).Scan(&n); err != nil {
		t.Fatalf("count learners: %v", err)
	}
	return n
}

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

// TestLearnerMutationAndEventShareTransaction proves ADR-003 at the students
// call sites (issue #52): the learner write and its outbox event commit or
// roll back as ONE transaction.
func TestLearnerMutationAndEventShareTransaction(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "OBX-S", "Outbox School")

	// Success: mutation and event both land.
	if _, err := f.svc.CreateLearner(ctx, school.ID, LearnerInput{FirstName: "Ada", LastName: "Lovelace", ExternalID: "ext-1"}); err != nil {
		t.Fatalf("create learner: %v", err)
	}
	if got := learnerCount(t, f); got != 1 {
		t.Fatalf("learners %d, want 1", got)
	}
	if got := outboxEventCountForSchool(t, f.pool, "students.LearnerCreated", school.ID); got != 1 {
		t.Fatalf("LearnerCreated events %d, want 1", got)
	}

	// Mutation failure: duplicate external id -> ErrExternalIDTaken, no event
	// for the failed attempt.
	if _, err := f.svc.CreateLearner(ctx, school.ID, LearnerInput{FirstName: "Grace", LastName: "Hopper", ExternalID: "ext-1"}); err == nil {
		t.Fatal("duplicate external id accepted")
	}
	if got := learnerCount(t, f); got != 1 {
		t.Fatalf("learners after duplicate %d, want 1", got)
	}
	if got := outboxEventCountForSchool(t, f.pool, "students.LearnerCreated", school.ID); got != 1 {
		t.Fatalf("events after duplicate %d, want 1", got)
	}

	// Event failure: the whole transaction must roll back — the learner may
	// NOT survive without its event (dual-write bug fixed in #52).
	hideEventOutbox(t, f)
	if _, err := f.svc.CreateLearner(ctx, school.ID, LearnerInput{FirstName: "Alan", LastName: "Turing"}); err == nil {
		t.Fatal("mutation succeeded while outbox was unwritable")
	}
	if got := learnerCount(t, f); got != 1 {
		t.Fatalf("learners after forced rollback %d, want 1 (mutation leaked)", got)
	}
	restoreEventOutbox(t, f)

	// Restored: writes flow again, mutation + event together.
	if _, err := f.svc.CreateLearner(ctx, school.ID, LearnerInput{FirstName: "Alan", LastName: "Turing"}); err != nil {
		t.Fatalf("create learner after restore: %v", err)
	}
	if got := learnerCount(t, f); got != 2 {
		t.Fatalf("learners %d, want 2", got)
	}
	if got := outboxEventCountForSchool(t, f.pool, "students.LearnerCreated", school.ID); got != 2 {
		t.Fatalf("LearnerCreated events %d, want 2", got)
	}
}
