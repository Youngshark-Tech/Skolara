//go:build integration

package events

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/testdb"
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
