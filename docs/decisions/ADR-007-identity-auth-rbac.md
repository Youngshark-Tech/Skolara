# ADR-007: Identity — Argon2id, JWT Access + Rotating Refresh Sessions, RBAC

**Status:** Accepted · **Date:** 2026-09-09

## Context

The platform needs secure authentication and granular, server-side-enforced authorization (§17, §44) from day one, supporting many roles (platform admin → student) and school-scoped memberships.

## Decision

- **Password hashing: argon2id** (time=3, memory=64MiB, parallelism=4), constant-time verification, per-user salt. No plaintext or reversible storage anywhere; logs redact credentials.
- **Sessions:** short-lived **JWT access tokens** (15 min, HMAC-SHA256, secret from config) carrying `sub`, active school context, roles, and a permission-matrix version. **Refresh tokens** are opaque, stored **hashed**, rotated on every use with **reuse detection** (replaying a rotated token revokes the whole session chain).
- **Authorization: RBAC** with a permission matrix seeded in migrations (`roles`, `permissions`, `role_permissions`, `user_roles`). Route registration declares the required permission; middleware `RequirePermission` enforces server-side. UI permission checks are UX only (documented).
- **Audit:** authentication events and every audited mutation write `audit_logs` (who, what, when, before/after, request ID, correlation ID) in the same transaction (§43).
- **Brute-force protection:** login rate limit + progressive account lockout counters.

## Consequences

- **Positive:** stateless request authorization; revocation via refresh-chain + short access TTL; permission evolution via matrix rows (no code changes for most role changes).
- **Negative:** access-token revocation is TTL-bound (15 min) — accepted for operational simplicity; permission-matrix version in claims guards against stale permissions mid-session.

## Alternatives Considered

- Fully opaque server-side sessions: stronger revocation but adds a store hit per request; revisit when Redis lands.
- External IdP (OIDC): roadmap — ports and claims are designed to admit an IdP later without domain rewrites.
