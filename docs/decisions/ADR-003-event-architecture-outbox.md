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
