//go:build integration

package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/testdb"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestOutboxAtomicWithTransaction(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	// Record an event inside a transaction, then ROLL BACK via error return.
	err := pool.WithinTx(ctx, func(q postgres.Querier) error {
		if _, err := Record(ctx, q, nil, "agg-1", "platform.TestEvent", 1, map[string]string{"k": "v"}); err != nil {
			return err
		}
		return errRollback
	})
	if err == nil {
		t.Fatal("expected rollback error")
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("event survived rollback: %d rows", count)
	}

	// Now commit a real event.
	_, err = Record(ctx, pool, nil, "agg-2", "platform.TestEvent", 1, map[string]string{"k": "v2"})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("committed event missing: %d rows", count)
	}
}

var errRollback = &rollbackError{}

type rollbackError struct{}

func (*rollbackError) Error() string { return "intentional rollback" }

func TestOutboxSchemaVersionEnforced(t *testing.T) {
	pool := testdb.New(t)
	if _, err := Record(context.Background(), pool, nil, "agg", "platform.Bad", 0, map[string]string{}); err == nil {
		t.Fatal("schema version 0 accepted")
	}
}

type collectingPublisher struct {
	mu     sync.Mutex
	events []Envelope
}

func (c *collectingPublisher) Publish(_ context.Context, env Envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, env)
	return nil
}

func TestDispatcherDeliversAndMarksPublished(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	env, err := Record(ctx, pool, nil, "agg-3", "platform.Deliverable", 1, map[string]int{"n": 42})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	pub := &collectingPublisher{}
	d := NewDispatcher(pool, pub)
	d.interval = 0
	if err := d.deliverBatch(ctx); err != nil {
		t.Fatalf("deliverBatch: %v", err)
	}

	if len(pub.events) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.events))
	}
	if pub.events[0].EventID != env.EventID {
		t.Fatalf("wrong event delivered: %s", pub.events[0].EventID)
	}

	var published int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE published_at IS NOT NULL`).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if published != 1 {
		t.Fatalf("published_at not marked: %d", published)
	}
}

func TestEventEnvelopeFieldsRoundTrip(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	school := "11111111-1111-1111-1111-111111111111"
	ctx = IntoContext(ctx, "actor-9", "corr-7")

	env, err := Record(ctx, pool, &school, "agg-5", "platform.Fields", 2, map[string]any{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if env.ActorID != "actor-9" || env.CorrelationID != "corr-7" {
		t.Fatalf("ctx metadata missing: %+v", env)
	}
	if env.SchemaVer != 2 || env.SchoolID == nil || *env.SchoolID != school {
		t.Fatalf("envelope fields wrong: %+v", env)
	}
	var payload map[string]any
	if err := json.Unmarshal(env.Payload, &payload); err != nil || payload["a"].(float64) != 1 {
		t.Fatalf("payload round trip: %v %v", payload, err)
	}
}

// --- dispatcher: SKIP LOCKED + exactly-once per publisher contract (issue #52)

// lockSomeRows takes row locks on the given event ids inside a caller-owned
// transaction, simulating a second dispatcher mid-batch.
func lockSomeRows(t *testing.T, pool *postgres.Pool, ids []string) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	if _, err := tx.Exec(context.Background(),
		`SELECT event_id FROM event_outbox WHERE event_id = ANY($1) FOR UPDATE`, ids); err != nil {
		t.Fatalf("lock rows: %v", err)
	}
	return tx
}

func publishedIDs(t *testing.T, pool *postgres.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT event_id FROM event_outbox WHERE published_at IS NOT NULL ORDER BY event_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestDispatcherSkipLockedClaimDoesNotTouchLockedRows deterministically proves
// the FOR UPDATE SKIP LOCKED claim: rows locked by another transaction are
// skipped, remain unpublished, and are delivered later without duplication.
func TestDispatcherSkipLockedClaimDoesNotTouchLockedRows(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	envs := make([]Envelope, 0, 6)
	for i := 0; i < 6; i++ {
		env, err := Record(ctx, pool, nil, "agg-lock", "platform.SkipLocked", 1, map[string]int{"i": i})
		if err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
		envs = append(envs, env)
	}
	locked := []string{envs[0].EventID, envs[1].EventID}
	unlocked := map[string]bool{}
	for _, e := range envs[2:] {
		unlocked[e.EventID] = true
	}

	lockTx := lockSomeRows(t, pool, locked)

	pub := &collectingPublisher{}
	d := NewDispatcher(pool, pub)
	d.interval = 0
	if err := d.deliverBatch(ctx); err != nil {
		t.Fatalf("deliverBatch (with locks held): %v", err)
	}
	if len(pub.events) != 4 {
		t.Fatalf("published %d events while 2 rows locked, want 4", len(pub.events))
	}
	for _, e := range pub.events {
		if !unlocked[e.EventID] {
			t.Fatalf("locked row %s was published", e.EventID)
		}
	}

	// Release the locks: the next batch delivers the remaining rows exactly once.
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatalf("release locks: %v", err)
	}
	if err := d.deliverBatch(ctx); err != nil {
		t.Fatalf("deliverBatch (after release): %v", err)
	}
	if len(pub.events) != 6 {
		t.Fatalf("published %d events total, want 6", len(pub.events))
	}
	seen := map[string]int{}
	for _, e := range pub.events {
		seen[e.EventID]++
	}
	for _, e := range envs {
		if seen[e.EventID] != 1 {
			t.Fatalf("event %s published %d times, want exactly 1", e.EventID, seen[e.EventID])
		}
	}
	if got := publishedIDs(t, pool); len(got) != 6 {
		t.Fatalf("published_at set on %d rows, want 6", len(got))
	}
}

// TestDispatcherConcurrentDeliveriesExactlyOnce hammers deliverBatch from
// overlapping goroutines: claims must stay disjoint (SKIP LOCKED), so every
// event is delivered exactly once across all runs and none is lost.
func TestDispatcherConcurrentDeliveriesExactlyOnce(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	const total = 40
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		env, err := Record(ctx, pool, nil, "agg-concurrent", "platform.Concurrent", 1, map[string]int{"i": i})
		if err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
		ids = append(ids, env.EventID)
	}

	var mu sync.Mutex
	published := map[string]int{}
	pub := publisherFunc(func(_ context.Context, env Envelope) error {
		time.Sleep(200 * time.Microsecond) // widen the overlap window
		mu.Lock()
		published[env.EventID]++
		mu.Unlock()
		return nil
	})

	d := NewDispatcher(pool, pub)
	d.interval = 0
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := 0; round < 5; round++ {
				if err := d.deliverBatch(ctx); err != nil {
					t.Errorf("deliverBatch: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(published) != total {
		t.Fatalf("%d distinct events published, want %d (some lost)", len(published), total)
	}
	for id, n := range published {
		if n != 1 {
			t.Fatalf("event %s published %d times, want exactly 1", id, n)
		}
	}
	if got := publishedIDs(t, pool); len(got) != total {
		t.Fatalf("published_at set on %d rows, want %d", len(got), total)
	}
}

type publisherFunc func(ctx context.Context, env Envelope) error

func (f publisherFunc) Publish(ctx context.Context, env Envelope) error {
	return f(ctx, env)
}

// TestDispatcherDeadLettersAfterCap proves the dead-letter path: a publisher
// that always fails drives attempts to the cap, bumps
// skolara_events_deadlettered_total exactly once per event, persists
// last_error, structured-logs the event id, and the row is never claimed again.
func TestDispatcherDeadLettersAfterCap(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	env, err := Record(ctx, pool, nil, "agg-dead", "platform.Dead", 1, map[string]string{"x": "y"})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	var logs syncBuffer
	d := NewDispatcher(pool, publisherFunc(func(_ context.Context, _ Envelope) error {
		return errors.New("broker unavailable")
	}))
	d.interval = 0
	d.logf = func(f string, a ...any) { fmt.Fprintf(&logs, f, a...) }

	before := testutil.ToFloat64(eventsDeadLettered)
	for i := 0; i < d.maxRetries; i++ {
		if err := d.deliverBatch(ctx); err != nil {
			t.Fatalf("deliverBatch %d: %v", i, err)
		}
	}
	after := testutil.ToFloat64(eventsDeadLettered)
	if after-before != 1 {
		t.Fatalf("dead-letter counter delta %v, want 1", after-before)
	}

	var attempts int
	var lastError string
	var published *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT attempts, last_error, published_at FROM event_outbox WHERE event_id = $1`,
		env.EventID).Scan(&attempts, &lastError, &published); err != nil {
		t.Fatal(err)
	}
	if attempts != d.maxRetries {
		t.Fatalf("attempts %d, want %d", attempts, d.maxRetries)
	}
	if lastError != "broker unavailable" {
		t.Fatalf("last_error %q not persisted", lastError)
	}
	if published != nil {
		t.Fatal("dead-lettered row must stay unpublished")
	}
	if !strings.Contains(logs.String(), env.EventID) {
		t.Fatalf("structured log missing event_id: %q", logs.String())
	}

	// Past the cap the row is excluded from claims: no further publish attempts
	// and no further counter bumps.
	for i := 0; i < 3; i++ {
		if err := d.deliverBatch(ctx); err != nil {
			t.Fatalf("deliverBatch after cap: %v", err)
		}
	}
	if got := testutil.ToFloat64(eventsDeadLettered); got != after {
		t.Fatalf("counter moved after cap: %v -> %v", after, got)
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
