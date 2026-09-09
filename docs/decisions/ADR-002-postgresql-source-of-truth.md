# ADR-002: PostgreSQL as Transactional Source of Truth

**Status:** Accepted · **Date:** 2026-09-09

## Context

The platform requires strong consistency for financial records, enrollment state, attendance, and audit. The specification (§9) designates PostgreSQL as the authoritative store and forbids Redis/ClickHouse/search indexes from holding business truth.

## Decision

- **PostgreSQL 16** is the single transactional source of truth.
- **golang-migrate** runs versioned SQL migrations from `services/api/migrations/`.
- **pgx/v5** with a connection pool for all access; no ORM — repositories issue explicit SQL so query intent, locking, and indexes stay visible.
- Migrations follow **strict conventions** (§53): one migration per issue, timestamp-prefixed (`20260909000002_identity.sql`), paired `up`/`down`, never rewritten after merge.
- Every tenant-scoped table carries `school_id` with FK + covering indexes.
- Financial tables get **database-level immutability triggers** (see ADR-004).

## Consequences

- **Positive:** ACID guarantees for ledger and state machines; SQL reviewable; migration history is an auditable schema changelog.
- **Negative:** schema changes require migrations discipline; hot paths must be indexed deliberately (N+1 and unbounded queries are forbidden per §60).

## Alternatives Considered

- ORM (GORM/ent): rejected for the finance/tenancy-critical paths — hidden queries and implicit locking are unacceptable for ledger correctness.
- NoSQL: rejected — financial and enrollment truth requires relational integrity.
