// Package testdb provides a unique-per-test database bootstrap for integration
// tests. Tests are tagged `integration` and require TEST_DATABASE_URL pointing
// at a PostgreSQL server (template database).
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
)

// envURL returns the server URL used to create throwaway test databases.
func envURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test")
	}
	return url
}

// New creates a uniquely named database, applies all migrations from dir, and
// returns a pool connected to it. Cleanup drops the database at test end.
func New(t *testing.T) *postgres.Pool {
	t.Helper()
	serverURL := envURL(t)

	admin, err := postgres.Connect(context.Background(), serverURL)
	if err != nil {
		t.Fatalf("testdb: connect server: %v", err)
	}
	dbName := fmt.Sprintf("sk_it_%d_%s", time.Now().UnixMilli(), randomSuffix())
	_, err = admin.Exec(context.Background(), fmt.Sprintf(`CREATE DATABASE %s`, quoteIdent(dbName)))
	if err != nil {
		admin.Close()
		t.Fatalf("testdb: create database: %v", err)
	}

	dbURL := replaceDBName(serverURL, dbName)
	pool, err := postgres.Connect(context.Background(), dbURL)
	if err != nil {
		_, _ = admin.Exec(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, quoteIdent(dbName)))
		admin.Close()
		t.Fatalf("testdb: connect test db: %v", err)
	}

	migrationsDir, err := findMigrationsDir()
	if err != nil {
		pool.Close()
		_, _ = admin.Exec(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, quoteIdent(dbName)))
		admin.Close()
		t.Fatalf("testdb: locate migrations: %v", err)
	}
	if err := postgres.MigrateUp(dbURL, migrationsDir); err != nil {
		pool.Close()
		_, _ = admin.Exec(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, quoteIdent(dbName)))
		admin.Close()
		t.Fatalf("testdb: migrate: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, quoteIdent(dbName)))
		admin.Close()
	})
	return pool
}

func quoteIdent(name string) string { return `"` + name + `"` }

// findMigrationsDir ascends from the working directory until it finds a
// "migrations" folder containing SQL files — test binaries run from their
// package directory, so depth varies by caller.
func findMigrationsDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		candidate := filepath.Join(dir, "migrations")
		if entries, err := os.ReadDir(candidate); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".up.sql") {
					return candidate, nil
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("migrations directory not found from %s", wd)
		}
		dir = parent
	}
}

func randomSuffix() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func replaceDBName(serverURL, dbName string) string {
	// Swap the path component (database name) of the postgres URL.
	idx := strings.LastIndex(serverURL, "/")
	if idx < 0 {
		return serverURL + "/" + dbName
	}
	base := serverURL[:idx+1]
	// Preserve query string if present.
	rest := serverURL[idx+1:]
	if q := strings.Index(rest, "?"); q >= 0 {
		return base + dbName + rest[q:]
	}
	return base + dbName
}
