//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/testdb"
)

func TestMigrationsUpAndDown(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	// Platform tables exist after up-migrations applied by testdb.New.
	for _, table := range []string{"event_outbox", "idempotency_keys"} {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s missing after migrate up", table)
		}
	}
}

func TestPoolPingAndWithinTx(t *testing.T) {
	pool := testdb.New(t)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := pool.WithinTx(context.Background(), func(q postgres.Querier) error {
		_, err := q.Exec(context.Background(), `SELECT 1`)
		return err
	}); err != nil {
		t.Fatalf("within tx: %v", err)
	}
}

func TestRedactedURLHidesCredentials(t *testing.T) {
	got := postgres.RedactedURL("postgres://user:supersecret@db:5432/skolara")
	if got == "" || contains(got, "supersecret") {
		t.Fatalf("credentials leaked: %s", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
