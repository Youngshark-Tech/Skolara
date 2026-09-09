# ADR-004: Ledger-First Finance — Double-Entry, Immutable, Idempotent

**Status:** Accepted · **Date:** 2026-09-09

## Context

Skolara's financial infrastructure (fees, payments, tutor compensation, settlements) demands absolute correctness (§28, §59). Balances must never be authoritative mutable fields.

## Decision

1. **Double-entry ledger**: every financial operation posts a `journal_entry` with ≥2 `journal_lines`; `SUM(debits) = SUM(credits)` per entry, enforced **in application code AND by database trigger**.
2. **Immutability**: database triggers reject `UPDATE`/`DELETE` on `journal_entries` and `journal_lines`. Corrections happen **only** via compensating entries that reference the original entry.
3. **Idempotency**: an `idempotency_keys` table maps external idempotency keys and provider event IDs to entries; replayed webhooks return the original result without new postings (proven by test).
4. **Integer minor units** for all amounts (no floats, ever).
5. **Balances are derived**: account balances are computed views/queries over lines, never stored mutable columns.
6. **Single transaction**: payment confirmation → ledger posting → invoice allocation → receipt event happen atomically; partial failure rolls back everything.
7. **Payments require server-side confirmation** (§30): webhook signature verification + provider-event-ID deduplication; frontend confirmation is never trusted.
8. **Enhanced audit**: every posting records actor, correlation ID, and source event.

## Consequences

- **Positive:** financial truth provable by invariant tests (debits=credits across arbitrary operation sequences); full auditability; safe corrections.
- **Negative:** every financial operation costs a transaction with trigger checks (accepted — correctness over raw speed for this volume).

## Alternatives Considered

- Mutable balance columns: forbidden by spec and for good reason — silent divergence, no audit trail.
- Event-sourced ledger: attractive long-term; the append-only ledger with outbox events delivers the same audit properties with less machinery now.
