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
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
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

// MigrateUp applies all pending migrations from a local directory.
func MigrateUp(databaseURL, dir string) error { return runMigrate(databaseURL, dir, -1) }

// MigrateUpFS applies all pending migrations from an in-memory filesystem
// (e.g. go:embed).
func MigrateUpFS(databaseURL string, fsys fs.FS) error { return runMigrateFS(databaseURL, fsys, -1) }

// MigrateDown rolls back n migrations (all if n < 0).
func MigrateDown(databaseURL, dir string, n int) error { return runMigrate(databaseURL, dir, n) }

func runMigrate(databaseURL, dir string, n int) error {
	return runMigrateFS(databaseURL, os.DirFS(dir), n)
}

func runMigrateFS(databaseURL string, fsys fs.FS, n int) error {
	src, err := iofs.New(fsys, ".")
	if err != nil {
		return fmt.Errorf("migrate: source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, "pgx5://"+stripScheme(databaseURL))
	if err != nil {
		return fmt.Errorf("migrate: init: %w", err)
	}
	defer m.Close()
	if n < 0 {
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("migrate: up: %w", err)
		}
		return nil
	}
	if err := m.Steps(-n); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate: down: %w", err)
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
