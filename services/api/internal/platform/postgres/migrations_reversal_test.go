//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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

// identityTables are created by migration 2 (20260909000002_identity) and
// dropped by its down-migration — used to prove the schema actually changed
// when MigrateDown steps back one migration (issue #100).
var identityTables = []string{"users", "refresh_tokens", "audit_logs"}

// platformTables are created by migration 1 (20260909000001_platform) and must
// still exist once only that migration is applied.
var platformTables = []string{"event_outbox", "idempotency_keys"}

// TestMigrationsUpAndDownAll proves the whole migration set is REVERSIBLE on a
// scratch database: up all (done by testdb.New) -> down all -> up all again,
// then asserts the key tables exist after the final up and that schema_migrations
// points at the newest migration. It mirrors what the CI `migrations` job runs.
func TestMigrationsUpAndDownAll(t *testing.T) {
	pool := testdb.New(t) // phase 1: fresh scratch DB, all up-migrations applied
	ctx := context.Background()
	url := pool.Config().ConnString()

	// Phase 2: roll the schema all the way back through the exported helper.
	// (Issue #100: MigrateDown(url, dir, n<0) used to run UP — the fix makes
	// negative n a true down-all, so this test now drives the helper instead
	// of bypassing it with golang-migrate's Down().)
	if err := postgres.MigrateDown(url, materializeMigrations(t), -1); err != nil {
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
	assertVersion(t, pool, ctx, latestMigrationVersion(t))
}

// TestMigrateDownStepsBackOne is the issue #100 regression test. From a clean
// database: apply exactly two migrations, then MigrateDown(url, dir, 1) must
// leave the schema at the PREVIOUS version with the second migration's tables
// dropped — not silently migrate up again, as the old sign-overloaded helper
// did. It also proves negative n is a true down-all now, and that a down on a
// clean database is the documented no-op.
func TestMigrateDownStepsBackOne(t *testing.T) {
	pool := testdb.New(t) // fresh scratch DB, all up-migrations applied
	ctx := context.Background()
	url := pool.Config().ConnString()
	dir := materializeMigrations(t) // exercises the os.DirFS path MigrateDown uses

	// Start clean: roll back everything testdb.New applied (down-all on a
	// fully migrated database — also proves the negative-n fix end to end).
	if err := postgres.MigrateDown(url, dir, -1); err != nil {
		t.Fatalf("down all on fresh scratch DB: %v", err)
	}
	assertClean(t, pool, ctx)

	// Up exactly 2 (no exported up-N helper; drive golang-migrate directly).
	if err := withMigrator(url, func(m *migrate.Migrate) error { return m.Steps(2) }); err != nil {
		t.Fatalf("up 2: %v", err)
	}
	versions := migrationVersions(t) // ascending
	// After up 2 the version is the SECOND migration in the set; after the
	// fix under test steps down one, it must be the FIRST.
	assertVersion(t, pool, ctx, versions[1])
	for _, table := range platformTables {
		if !tableExists(t, pool, table) {
			t.Errorf("table %s missing after up 2", table)
		}
	}

	// The fix under test: exactly one step DOWN.
	if err := postgres.MigrateDown(url, dir, 1); err != nil {
		t.Fatalf("MigrateDown(url, dir, 1): %v", err)
	}
	assertVersion(t, pool, ctx, versions[0])
	// The schema reflects it: migration 2's tables are gone, migration 1's remain.
	for _, table := range identityTables {
		if tableExists(t, pool, table) {
			t.Errorf("table %s still exists after down 1 (migration 2 not rolled back)", table)
		}
	}
	for _, table := range platformTables {
		if !tableExists(t, pool, table) {
			t.Errorf("table %s missing after down 1 (migration 1 should remain)", table)
		}
	}

	// Over-count: down 99 with one migration applied rolls back everything
	// without error (golang-migrate reports ErrShortLimit for the shortfall;
	// landing at the bottom satisfies the request, so the helper caps it).
	if err := postgres.MigrateDown(url, dir, 99); err != nil {
		t.Fatalf("MigrateDown(url, dir, 99) over-count: %v", err)
	}
	assertClean(t, pool, ctx)

	// Documented no-op: down past the last applied migration changes nothing
	// and must not error (this is the "second down on a clean database" case).
	if err := postgres.MigrateDown(url, dir, 1); err != nil {
		t.Fatalf("MigrateDown(url, dir, 1) on clean DB: %v", err)
	}
	assertClean(t, pool, ctx)
	if err := postgres.MigrateDown(url, dir, -1); err != nil {
		t.Fatalf("MigrateDown(url, dir, -1) on clean DB: %v", err)
	}
	assertClean(t, pool, ctx)

	// Sanity: the same scratch DB still migrates UP cleanly through the
	// exported helper (MigrateUp must not have been disturbed by the fix).
	if err := postgres.MigrateUp(url, dir); err != nil {
		t.Fatalf("MigrateUp after down-all: %v", err)
	}
	assertVersion(t, pool, ctx, versions[len(versions)-1])
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

// assertVersion checks schema_migrations points at exactly the wanted version.
func assertVersion(t *testing.T, pool *postgres.Pool, ctx context.Context, want uint64) {
	t.Helper()
	var got uint64
	if err := pool.QueryRow(ctx, `SELECT version FROM schema_migrations`).Scan(&got); err != nil {
		t.Fatalf("read schema_migrations version: %v", err)
	}
	if got != want {
		t.Errorf("schema_migrations version = %d, want %d", got, want)
	}
}

// assertClean checks the database is fully rolled back: schema_migrations
// still exists (golang-migrate TRUNCATEs it at the nil version) but holds no
// applied version.
func assertClean(t *testing.T, pool *postgres.Pool, ctx context.Context) {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("read schema_migrations on clean DB: %v", err)
	}
	if n != 0 {
		t.Errorf("schema_migrations holds %d rows after full down, want 0", n)
	}
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

// materializeMigrations writes the embedded migration files into a temp
// directory and returns it, so tests can exercise the on-disk os.DirFS path
// the MigrateUp/MigrateDown helpers use.
func materializeMigrations(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	for _, e := range entries {
		body, err := fs.ReadFile(migrations.FS, e.Name())
		if err != nil {
			t.Fatalf("read embedded %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), body, 0o644); err != nil {
			t.Fatalf("write %s: %v", e.Name(), err)
		}
	}
	return dir
}

// migrationVersions parses the numeric prefixes of the embedded *.up.sql files
// (e.g. 20260909000012) in ascending order, so assertions never hardcode a count.
func migrationVersions(t *testing.T) []uint64 {
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
	return versions
}

func latestMigrationVersion(t *testing.T) uint64 {
	t.Helper()
	versions := migrationVersions(t)
	return versions[len(versions)-1]
}
