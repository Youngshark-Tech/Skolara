# ADR-003: Event Architecture — Transactional Outbox First

**Status:** Accepted · **Date:** 2026-09-09

## Context

Domains must communicate through events (§41) without distributed transactions. NATS JetStream is the target message infrastructure but introducing it before the event contract is stable would be premature.

## Decision

**Transactional outbox pattern** implemented in `internal/platform/events`:

1. Domain code calls `events.Record(tx, evt)` inside the **same database transaction** as the state change.
2. The outbox table `event_outbox` stores the full envelope: event ID (UUIDv7), type, version, tenant/school ID, aggregate ID, timestamp, actor, correlation ID, causation ID, JSON payload, `published_at NULL`.
3. A dispatcher loop (in-process now) claims unpublished rows with `FOR UPDATE SKIP LOCKED`, delivers to a **Publisher port** (interface), and marks published — at-least-once semantics; consumers must be idempotent (§42).
4. A future NATS JetStream adapter implements the same `Publisher` port without touching domain code.

Envelope is versioned (`schema_version` per event type); adding fields is additive-only within a version.

## Consequences

- **Positive:** events are atomic with business state (rollback removes the event — proven by test); broker becomes swappable infrastructure; event history is queryable for debugging.
- **Negative:** outbox polling latency (mitigated by short interval + LISTEN/NOTIFY later); at-least-once requires consumer idempotency (accepted and enforced by contract).

## Alternatives Considered

- Broker-in-the-request-path: rejected — a broker outage must never make business writes fail.
- Change-data-capture (Debezium): considered for later scale; outbox is simpler and sufficient now.

## Implementation Note (2026-10, issue #52)

Audit round 2 found the guarantee above was not actually honored at call sites. Hardened as follows:

- **Recording:** every domain mutation in tenancy, students, academics, attendance, assignments and finance records its event on the same `pool.WithinTx` transaction as the write (repo `*Tx` variants take a `postgres.Querier`). Per-domain rollback regression tests force an event failure and assert the mutation rolls back with it. One documented best-effort exception: `identity.UserCreated` is recorded post-commit at the handler with an audit-trail fallback (identity is not in the six-domain scope of #52; user creation remains the source of truth via `users`).
- **Dispatcher:** claims unpublished rows `FOR UPDATE SKIP LOCKED` inside one transaction and marks them published in that same transaction — concurrent dispatchers claim disjoint batches and a crash before commit re-delivers (at-least-once). Delivery failures are structured-logged with `event_id`, bump `attempts`, and persist `last_error` on the row; at the attempts cap (20) the row dead-letters (stays unpublished, excluded from claims) and raises `skolara_events_deadlettered_total`. No errors are swallowed.
- **Delivery failure of a whole batch** (claim/scan/commit): logged and retried on the next tick; nothing is marked published on a failed commit.
