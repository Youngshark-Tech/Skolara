//go:build integration

package tenancy

import (
	"context"
	"testing"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
)

// outboxEventCount counts outbox rows of the given event type.
func outboxEventCount(t *testing.T, pool *postgres.Pool, eventType string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM event_outbox WHERE event_type = $1`, eventType).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

func tableCount(t *testing.T, pool *postgres.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// hideEventOutbox makes the outbox temporarily unwritable so the event insert
// inside a mutation transaction fails — forcing the rollback the tests assert on.
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

// TestSchoolMutationAndEventShareTransaction proves ADR-003 at the tenancy
// call sites (issue #52): the business write and its outbox event commit or
// roll back as ONE transaction — an event failure must undo the mutation, and
// a mutation failure must leave no event behind.
func TestSchoolMutationAndEventShareTransaction(t *testing.T) {
	svc, _, pool := newTenancyFixture(t)
	ctx := context.Background()

	// Success: mutation and event both land.
	if _, err := svc.CreateSchool(ctx, "TXN-1", "Shared Tx School", "", ""); err != nil {
		t.Fatalf("create school: %v", err)
	}
	if got := tableCount(t, pool, "schools"); got != 1 {
		t.Fatalf("schools %d, want 1", got)
	}
	if got := outboxEventCount(t, pool, "tenancy.SchoolCreated"); got != 1 {
		t.Fatalf("SchoolCreated events %d, want 1", got)
	}

	// Mutation failure: duplicate code -> ErrCodeTaken, and no event for the
	// failed attempt.
	if _, err := svc.CreateSchool(ctx, "TXN-1", "Duplicate School", "", ""); err == nil {
		t.Fatal("duplicate code accepted")
	}
	if got := tableCount(t, pool, "schools"); got != 1 {
		t.Fatalf("schools after duplicate %d, want 1", got)
	}
	if got := outboxEventCount(t, pool, "tenancy.SchoolCreated"); got != 1 {
		t.Fatalf("events after duplicate %d, want 1", got)
	}

	// Event failure: with the outbox unwritable the whole transaction must
	// roll back — the mutation may NOT survive without its event (the dual
	// write bug fixed in #52).
	hideEventOutbox(t, pool)
	if _, err := svc.CreateSchool(ctx, "TXN-2", "Rollback School", "", ""); err == nil {
		t.Fatal("mutation succeeded while outbox was unwritable")
	}
	if got := tableCount(t, pool, "schools"); got != 1 {
		t.Fatalf("schools after forced rollback %d, want 1 (mutation leaked)", got)
	}
	restoreEventOutbox(t, pool)

	// Restored: writes flow again, mutation + event together.
	if _, err := svc.CreateSchool(ctx, "TXN-2", "Rollback School", "", ""); err != nil {
		t.Fatalf("create school after restore: %v", err)
	}
	if got := tableCount(t, pool, "schools"); got != 2 {
		t.Fatalf("schools %d, want 2", got)
	}
	if got := outboxEventCount(t, pool, "tenancy.SchoolCreated"); got != 2 {
		t.Fatalf("SchoolCreated events %d, want 2", got)
	}
}
