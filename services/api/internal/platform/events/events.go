// Package events implements the transactional outbox pattern (ADR-003):
// domain events are recorded in the same transaction as the state change and
// delivered asynchronously at-least-once. Consumers must be idempotent.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/observability"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Envelope is the canonical event shape (master spec §41).
type Envelope struct {
	EventID       string          `json:"event_id"`
	Type          string          `json:"type"` // e.g. "students.EnrollmentStateChanged"
	SchemaVer     int             `json:"schema_version"`
	SchoolID      *string         `json:"school_id,omitempty"` // nil for platform-level events
	AggregateID   string          `json:"aggregate_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	ActorID       string          `json:"actor_id,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	CausationID   string          `json:"causation_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Record writes an event to the outbox within the caller's transaction.
// It never touches the network: delivery is the dispatcher's concern.
func Record(ctx context.Context, q Querier, schoolID *string, aggregateID, eventType string, schemaVersion int, payload any) (Envelope, error) {
	if schemaVersion < 1 {
		return Envelope{}, fmt.Errorf("events: schema version must be >= 1 for %s", eventType)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("events: marshal payload: %w", err)
	}
	env := Envelope{
		EventID:     uuid.NewString(),
		Type:        eventType,
		SchemaVer:   schemaVersion,
		SchoolID:    schoolID,
		AggregateID: aggregateID,
		OccurredAt:  time.Now().UTC(),
		Payload:     raw,
	}
	if actor := actorFromCtx(ctx); actor != "" {
		env.ActorID = actor
	}
	if corr := correlationFromCtx(ctx); corr != "" {
		env.CorrelationID = corr
	}

	const query = `INSERT INTO event_outbox (event_id, event_type, schema_version, school_id, aggregate_id, occurred_at, actor_id, correlation_id, payload)
                   VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	if _, err := q.Exec(ctx, query, env.EventID, env.Type, env.SchemaVer, env.SchoolID,
		env.AggregateID, env.OccurredAt, env.ActorID, env.CorrelationID, env.Payload); err != nil {
		return Envelope{}, fmt.Errorf("events: record: %w", err)
	}
	return env, nil
}

type ctxKey int

const (
	ctxActor ctxKey = iota
	ctxCorrelation
)

// IntoContext attaches actor/correlation metadata used when recording events.
func IntoContext(ctx context.Context, actorID, correlationID string) context.Context {
	ctx = context.WithValue(ctx, ctxActor, actorID)
	return context.WithValue(ctx, ctxCorrelation, correlationID)
}

func actorFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxActor).(string)
	return v
}

func correlationFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxCorrelation).(string)
	return v
}

// Publisher is the delivery port. Implementations (log, NATS JetStream later)
// receive envelopes and must tolerate at-least-once delivery.
type Publisher interface {
	Publish(ctx context.Context, env Envelope) error
}

// LogPublisher delivers events to structured logs — the default adapter until
// NATS JetStream is provisioned (ADR-003).
type LogPublisher struct {
	Logf func(format string, args ...any)
}

// Publish logs the envelope.
func (p LogPublisher) Publish(_ context.Context, env Envelope) error {
	p.Logf("event published type=%s id=%s school=%v aggregate=%s payload=%s",
		env.Type, env.EventID, env.SchoolID, env.AggregateID, string(env.Payload))
	return nil
}

// Dispatcher claims unpublished events (SKIP LOCKED) and hands them to a
// Publisher, marking them published on success. At-least-once semantics.
type Dispatcher struct {
	q          Querier
	pub        Publisher
	interval   time.Duration
	maxRetries int
}

// NewDispatcher builds a dispatcher; poll interval defaults to 500ms.
func NewDispatcher(q Querier, pub Publisher) *Dispatcher {
	return &Dispatcher{q: q, pub: pub, interval: 500 * time.Millisecond, maxRetries: 20}
}

// Run polls until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	t := time.NewTicker(d.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = d.deliverBatch(ctx)
		}
	}
}

// deliverBatch claims up to 100 unpublished events and delivers them.
func (d *Dispatcher) deliverBatch(ctx context.Context) error {
	rows, err := d.q.Query(ctx,
		`SELECT event_id, event_type, schema_version, school_id, aggregate_id, occurred_at, actor_id, correlation_id, payload
                 FROM event_outbox
                 WHERE published_at IS NULL AND attempts < $1
                 ORDER BY occurred_at
                 LIMIT 100`, d.maxRetries)
	if err != nil {
		return err
	}
	var batch []Envelope
	for rows.Next() {
		var env Envelope
		if err := rows.Scan(&env.EventID, &env.Type, &env.SchemaVer, &env.SchoolID, &env.AggregateID,
			&env.OccurredAt, &env.ActorID, &env.CorrelationID, &env.Payload); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, env)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, env := range batch {
		if err := d.pub.Publish(ctx, env); err != nil {
			_, _ = d.q.Exec(ctx,
				`UPDATE event_outbox SET attempts = attempts + 1, last_error = $2 WHERE event_id = $1`,
				env.EventID, err.Error())
			continue
		}
		_, _ = d.q.Exec(ctx, `UPDATE event_outbox SET published_at = now() WHERE event_id = $1`, env.EventID)
		observability.IncEventsPublished()
	}
	return nil
}
