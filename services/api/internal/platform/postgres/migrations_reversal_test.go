//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/testdb"
	migrations "github.com/Roy-Wanyoike/Skolara/services/api/migrations"
)

// keyTables spans every bounded context; if the full up-migration set applied
// cleanly, all of these must exist (issue #63: down-migrations were never
// exercised anywhere before this test).
var keyTables = []string{
	// identity
	"users", "refresh_tokens", "audit_logs",
	// tenancy
	"schools", "school_memberships",
	// students
	"learners",
	// academics
	"academic_years", "terms", "class_groups",
	// attendance
	"attendance_sessions", "attendance_records",
	// assignments
	"assignments", "assignment_submissions",
	// finance
	"invoices", "payments", "payment_allocations",
	// platform
	"event_outbox", "idempotency_keys",
}

// TestMigrationsUpAndDownAll proves the whole migration set is REVERSIBLE on a
// scratch database: up all (done by testdb.New) -> down all -> up all again,
// then asserts the key tables exist after the final up and that schema_migrations
// points at the newest migration. It mirrors what the CI `migrations` job runs.
func TestMigrationsUpAndDownAll(t *testing.T) {
	pool := testdb.New(t) // phase 1: fresh scratch DB, all up-migrations applied
	ctx := context.Background()
	url := pool.Config().ConnString()

	// Phase 2: roll the schema all the way back. MigrateDown(url, dir, -1)
	// cannot be used for this (negative n means Up in postgres.runMigrateFS),
	// so drive golang-migrate's Down() directly over the embedded FS.
	if err := withMigrator(url, func(m *migrate.Migrate) error { return m.Down() }); err != nil {
		t.Fatalf("down all: %v", err)
	}
	for _, table := range keyTables {
		if tableExists(t, pool, table) {
			t.Errorf("table %s still exists after down-all (drop missing or incomplete)", table)
		}
	}

	// Phase 3: re-apply everything via the exported helper over the SAME
	// embedded FS the api binary self-migrates from at startup.
	if err := postgres.MigrateUpFS(url, migrations.FS); err != nil {
		t.Fatalf("re-up all: %v", err)
	}

	// Phase 4: every key table exists again and the version is the newest one.
	for _, table := range keyTables {
		if !tableExists(t, pool, table) {
			t.Errorf("table %s missing after final up-all", table)
		}
	}
	want := latestMigrationVersion(t)
	var got uint64
	if err := pool.QueryRow(ctx, `SELECT version FROM schema_migrations`).Scan(&got); err != nil {
		t.Fatalf("read schema_migrations version: %v", err)
	}
	if got != want {
		t.Errorf("schema_migrations version after final up = %d, want %d", got, want)
	}
}

func tableExists(t *testing.T, pool *postgres.Pool, table string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
		table).Scan(&exists); err != nil {
		t.Fatalf("probe table %s: %v", table, err)
	}
	return exists
}

// withMigrator builds a golang-migrate instance over the embedded migrations FS
// against the given database URL, runs fn, and always releases the source,
// database connection and advisory lock via Close.
func withMigrator(databaseURL string, fn func(m *migrate.Migrate) error) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return err
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, databaseURL)
	if err != nil {
		return err
	}
	defer m.Close()
	err = fn(m)
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return err
}

// latestMigrationVersion parses the highest numeric prefix from the embedded
// *.up.sql files (e.g. 20260909000012), so the assertion never hardcodes a count.
func latestMigrationVersion(t *testing.T) uint64 {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	var versions []uint64
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		v, err := strconv.ParseUint(name[:strings.IndexByte(name, '_')], 10, 64)
		if err != nil {
			t.Fatalf("parse version from %s: %v", name, err)
		}
		versions = append(versions, v)
	}
	if len(versions) == 0 {
		t.Fatal("no *.up.sql files in embedded migrations FS")
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	return versions[len(versions)-1]
}
