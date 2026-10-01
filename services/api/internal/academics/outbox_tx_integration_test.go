//go:build integration

package academics

import (
	"context"
	"testing"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
)

func tableCount(t *testing.T, pool *postgres.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
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

// TestAcademicsMutationAndEventShareTransaction proves ADR-003 at the
// academics call sites (issue #52): year/term writes and their outbox events
// commit or roll back as ONE transaction.
func TestAcademicsMutationAndEventShareTransaction(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "OBX-A", "Outbox Academics School")

	// Success: year mutation and event both land.
	year, err := f.svc.CreateAcademicYear(ctx, school.ID, "2026", "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("create year: %v", err)
	}
	if got := tableCount(t, f.pool, "academic_years"); got != 1 {
		t.Fatalf("years %d, want 1", got)
	}
	if got := eventCount(t, f, "academics.AcademicYearCreated", school.ID); got != 1 {
		t.Fatalf("AcademicYearCreated events %d, want 1", got)
	}

	// Mutation failure: duplicate name -> ErrNameTaken, no event.
	if _, err := f.svc.CreateAcademicYear(ctx, school.ID, "2026", "2027-01-01", "2027-12-31"); err == nil {
		t.Fatal("duplicate year name accepted")
	}
	if got := eventCount(t, f, "academics.AcademicYearCreated", school.ID); got != 1 {
		t.Fatalf("events after duplicate %d, want 1", got)
	}

	// Event failure on CreateAcademicYear: forced rollback — no year row survives.
	hideEventOutbox(t, f.pool)
	if _, err := f.svc.CreateAcademicYear(ctx, school.ID, "2027", "2027-01-01", "2027-12-31"); err == nil {
		t.Fatal("mutation succeeded while outbox was unwritable")
	}
	if got := tableCount(t, f.pool, "academic_years"); got != 1 {
		t.Fatalf("years after forced rollback %d, want 1 (mutation leaked)", got)
	}

	// Event failure on CreateTerm: the event rides the term tx (issue #52), so
	// the term must roll back with it.
	if _, err := f.svc.CreateTerm(ctx, school.ID, year.ID, "Term 1", "2026-02-01", "2026-04-30"); err == nil {
		t.Fatal("term mutation succeeded while outbox was unwritable")
	}
	if got := tableCount(t, f.pool, "terms"); got != 0 {
		t.Fatalf("terms after forced rollback %d, want 0 (mutation leaked)", got)
	}
	restoreEventOutbox(t, f.pool)

	// Restored: both write paths flow again, mutation + event together.
	if _, err := f.svc.CreateAcademicYear(ctx, school.ID, "2027", "2027-01-01", "2027-12-31"); err != nil {
		t.Fatalf("create year after restore: %v", err)
	}
	if _, err := f.svc.CreateTerm(ctx, school.ID, year.ID, "Term 1", "2026-02-01", "2026-04-30"); err != nil {
		t.Fatalf("create term after restore: %v", err)
	}
	if got := eventCount(t, f, "academics.TermCreated", school.ID); got != 1 {
		t.Fatalf("TermCreated events %d, want 1", got)
	}
	if got := eventCount(t, f, "academics.AcademicYearCreated", school.ID); got != 2 {
		t.Fatalf("AcademicYearCreated events %d, want 2", got)
	}
}
