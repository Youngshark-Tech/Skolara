# ADR-009: Offline-First Strategy (Groundwork Now, Sync Engine Later)

**Status:** Accepted · **Date:** 2026-09-09

## Context

Schools operate with unreliable connectivity (§46). Attendance and lesson records must tolerate offline operation.

## Decision

**Phase now (groundwork):**
- All offline-bound mutation APIs accept **`client_mutation_id`** (client-generated UUID) and are **idempotent on replay** — the attendance bulk endpoint is the reference implementation (unique index + upsert keyed on mutation ID).
- Every offline mutation carries: client mutation ID, client timestamp, actor, entity version (optimistic concurrency), sync status on the client.

**Phase later (tracked roadmap issue):**
- Web PWA + local store (IndexedDB) with a sync queue; mobile (Expo) with SQLite local persistence.
- Conflict resolution: last-writer-wins for attendance edits within a grace window; server arbitration with audit for the rest.
- Push notifications once the communication fabric lands.

## Consequences

- **Positive:** API contract is already offline-correct — no retrofit; sync engine becomes a client concern over stable idempotent APIs.
- **Negative:** full offline UX is deferred; documented as roadmap rather than silently missing.
