// Package events implements the transactional outbox pattern (ADR-003):
// domain events are recorded in the same transaction as the state change and
// delivered asynchronously at-least-once. Consumers must be idempotent.
//
// Recording: Record writes to the outbox through a Querier, which both the
// pool and a transaction satisfy — callers pass the tx of the business write
// so the event commits or rolls back atomically with it.
//
// Delivery: the Dispatcher claims unpublished rows FOR UPDATE SKIP LOCKED
// inside one transaction, publishes, and marks published in that same
// transaction — concurrent dispatchers never double-claim, and a crash before
// commit yields redelivery (at-least-once), never loss. Publish failures are
// structured-logged with the event id, bump attempts, persist last_error on
// the row, and raise skolara_events_deadlettered_total once the retry cap is
// exhausted. Errors are never silently swallowed.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/observability"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"
)

// eventsDeadLettered counts outbox events that exhausted the delivery retry
// cap. Dead-lettered rows keep published_at NULL, carry their final failure
// in last_error, and are excluded from further claims. Registered on the
// default registry so /metrics exposes it next to the other skolara_* series.
var eventsDeadLettered = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "skolara_events_deadlettered_total",
	Help: "Outbox events that exhausted delivery attempts (dead-letter state); inspect last_error on event_outbox",
})

func init() {
	prometheus.MustRegister(eventsDeadLettered)
}

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

// TxBeginner supplies transactions for claim-and-mark batches. Satisfied by
// both *pgxpool.Pool and postgres.Pool (and by pgx.Tx, whose nested Begin
// maps to savepoints).
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Dispatcher claims unpublished events FOR UPDATE SKIP LOCKED inside one
// transaction, hands them to a Publisher, and marks them published in that
// same transaction. Concurrent dispatchers claim disjoint batches, so a
// given event is published exactly once per claim; a crash between Publish
// and commit re-delivers (at-least-once semantics; consumers idempotent).
// Deliveries and marks commit atomically — the event is never silently lost.
type Dispatcher struct {
	q          TxBeginner
	pub        Publisher
	interval   time.Duration
	maxRetries int
	logf       func(format string, args ...any)
}

// NewDispatcher builds a dispatcher; poll interval defaults to 500ms and the
// delivery retry cap to 20 attempts.
func NewDispatcher(q TxBeginner, pub Publisher) *Dispatcher {
	return &Dispatcher{
		q: q, pub: pub,
		interval:   500 * time.Millisecond,
		maxRetries: 20,
		logf:       func(f string, a ...any) { log.Printf("events: "+f, a...) },
	}
}

// Run polls until ctx is cancelled. Batch-level errors (claim/scan/commit
// failures) are structured-logged; the batch is retried on the next tick.
func (d *Dispatcher) Run(ctx context.Context) {
	t := time.NewTicker(d.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := d.deliverBatch(ctx); err != nil {
				d.logf("dispatcher batch error: %v", err)
			}
		}
	}
}

// deliverBatch claims up to 100 unpublished events in one transaction — the
// FOR UPDATE SKIP LOCKED claim keeps concurrent dispatchers off each other's
// rows — delivers them, and marks them published in the same transaction.
func (d *Dispatcher) deliverBatch(ctx context.Context) error {
	tx, err := d.q.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dispatcher: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx,
		`SELECT event_id, event_type, schema_version, school_id, aggregate_id, occurred_at, actor_id, correlation_id, payload
                 FROM event_outbox
                 WHERE published_at IS NULL AND attempts < $1
                 ORDER BY occurred_at
                 LIMIT 100
                 FOR UPDATE SKIP LOCKED`, d.maxRetries)
	if err != nil {
		return fmt.Errorf("dispatcher: claim: %w", err)
	}
	var batch []Envelope
	for rows.Next() {
		var env Envelope
		if err := rows.Scan(&env.EventID, &env.Type, &env.SchemaVer, &env.SchoolID, &env.AggregateID,
			&env.OccurredAt, &env.ActorID, &env.CorrelationID, &env.Payload); err != nil {
			rows.Close()
			return fmt.Errorf("dispatcher: scan: %w", err)
		}
		batch = append(batch, env)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("dispatcher: rows: %w", err)
	}

	for _, env := range batch {
		if err := d.pub.Publish(ctx, env); err != nil {
			// Delivery failed: structured-log with the event id,
			// bump attempts, and persist the error on the row — all
			// inside the claim transaction. Once the retry cap is
			// reached the row dead-letters: it stays unpublished,
			// keeps last_error for inspection, and is excluded from
			// further claims. The counter makes the stuck event
			// observable instead of silently retried forever.
			d.logf("dispatcher publish failed event_id=%s type=%s: %v", env.EventID, env.Type, err)
			var attempts int
			if aerr := tx.QueryRow(ctx,
				`UPDATE event_outbox SET attempts = attempts + 1, last_error = $2 WHERE event_id = $1 RETURNING attempts`,
				env.EventID, err.Error()).Scan(&attempts); aerr != nil {
				return fmt.Errorf("dispatcher: record failure for %s: %w", env.EventID, aerr)
			}
			if attempts >= d.maxRetries {
				eventsDeadLettered.Inc()
				d.logf("dispatcher dead-lettered event_id=%s type=%s attempts=%d last_error=%q",
					env.EventID, env.Type, attempts, err.Error())
			}
			continue
		}
		if _, uerr := tx.Exec(ctx,
			`UPDATE event_outbox SET published_at = now() WHERE event_id = $1`, env.EventID); uerr != nil {
			return fmt.Errorf("dispatcher: mark published %s: %w", env.EventID, uerr)
		}
		observability.IncEventsPublished()
	}
	return tx.Commit(ctx)
}
