// Package postgres provides the database access foundation: pooled connections,
// context-scoped transactions, and migration execution.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	// The database/sql (lib/pq) postgres driver executes migration files via
	// the simple protocol, which applies ALL statements in the file inside
	// one implicit transaction. The pgx/v5 driver truncates multi-statement
	// migrations nondeterministically — do not switch back.
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx, letting repositories
// run identically inside or outside transactions.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Pool wraps the pgx connection pool.
type Pool struct {
	*pgxpool.Pool
}

// Connect builds a validated pool for the given URL.
func Connect(ctx context.Context, databaseURL string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse config: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 15 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return &Pool{Pool: pool}, nil
}

// WithinTx runs fn in a transaction, committing on nil error and rolling back
// otherwise. The deferred rollback is a no-op after a successful commit.
func (p *Pool) WithinTx(ctx context.Context, fn func(q Querier) error) error {
	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RedactedURL strips credentials for safe logging.
func RedactedURL(databaseURL string) string {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "unparseable"
	}
	if u.User != nil {
		u.User = url.UserPassword("REDACTED", "REDACTED")
	}
	return u.String()
}

// migrateDirection selects which way runMigrateFS drives the schema. The
// exported helpers map onto it explicitly so the direction is never inferred
// from the sign of a step count (issue #100: MigrateDown(url, dir, n<0) used
// to run Up because runMigrateFS overloaded the sign of n).
type migrateDirection int

const (
	migrateUp migrateDirection = iota
	migrateDown
)

// MigrateUp applies all pending migrations from a local directory.
func MigrateUp(databaseURL, dir string) error {
	return runMigrate(databaseURL, dir, migrateUp, 0)
}

// MigrateUpFS applies all pending migrations from an in-memory filesystem
// (e.g. go:embed).
func MigrateUpFS(databaseURL string, fsys fs.FS) error {
	return runMigrateFS(databaseURL, fsys, migrateUp, 0)
}

// MigrateDown rolls back n migrations (all if n < 0, or capped at the number
// applied when n exceeds it). Rolling back on a database with no applied
// migrations is a no-op, as is n == 0.
func MigrateDown(databaseURL, dir string, n int) error {
	return runMigrate(databaseURL, dir, migrateDown, n)
}

func runMigrate(databaseURL, dir string, direction migrateDirection, n int) error {
	return runMigrateFS(databaseURL, os.DirFS(dir), direction, n)
}

func runMigrateFS(databaseURL string, fsys fs.FS, direction migrateDirection, n int) error {
	src, err := iofs.New(fsys, ".")
	if err != nil {
		return fmt.Errorf("migrate: source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, "postgres://"+stripScheme(databaseURL))
	if err != nil {
		return fmt.Errorf("migrate: init: %w", err)
	}
	defer m.Close()

	switch direction {
	case migrateUp:
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("migrate: up: %w", err)
		}
	case migrateDown:
		// Steps DOWN unconditionally (issue #100): n > 0 rolls back exactly
		// n migrations, n < 0 rolls back every applied migration, and n == 0
		// is a no-op. Down past the first migration (clean database) is the
		// documented no-op too: golang-migrate reports os.ErrNotExist for
		// Steps(-n) from the nil version, so the current version is checked
		// up-front instead of pattern-matching on that error.
		if n == 0 {
			return nil
		}
		if _, _, verr := m.Version(); verr != nil {
			if errors.Is(verr, migrate.ErrNilVersion) {
				return nil // nothing applied — documented no-op
			}
			return fmt.Errorf("migrate: version: %w", verr)
		}
		var err error
		if n < 0 {
			err = m.Down()
		} else {
			err = m.Steps(-n)
		}
		// ErrShortLimit: golang-migrate applies every down it can and then
		// reports the shortfall. Landing at the bottom of the applied set
		// satisfies a down-N request, so the shortfall is not a failure (the
		// nothing-applied case is handled by the ErrNilVersion pre-check).
		var short migrate.ErrShortLimit
		if err != nil && !errors.Is(err, migrate.ErrNoChange) && !errors.As(err, &short) {
			return fmt.Errorf("migrate: down: %w", err)
		}
	default:
		return fmt.Errorf("migrate: unknown direction %d", int(direction))
	}
	return nil
}

func stripScheme(databaseURL string) string {
	for _, p := range []string{"postgres://", "postgresql://"} {
		if len(databaseURL) > len(p) && databaseURL[:len(p)] == p {
			return databaseURL[len(p):]
		}
	}
	return databaseURL
}
