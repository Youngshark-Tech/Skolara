# Identity Domain

**Owner:** `services/api/internal/identity` · **Migration:** `20260909000002`

## Ownership

Users (accounts), password credentials (argon2id), platform-wide roles, JWT access tokens, rotating refresh sessions, the audit log, and the permission constants referenced by every domain.

## Data

- `users` — email (unique, case-insensitive), argon2id hash (m=64 MiB, t=3, p=4), status (`active|disabled|locked`), lockout counters.
- `roles` / `permissions` / `role_permissions` / `user_roles` — the 13×16 RBAC matrix, seeded in the migration.
- `refresh_tokens` — SHA-256 of the opaque token, family id (rotation chain), used/revoked markers.
- `audit_logs` — actor, action, resource, before/after, request/correlation ids.

## Rules

- Unknown-user login burns an argon2 verify (no timing enumeration).
- 5 failed logins → 15-minute lockout; auto-unlock after the window.
- Refresh rotation: used tokens are detectable; reuse revokes the whole family (ADR-007).
- `PATCH /users/{id}/status` disable → revokes ALL refresh tokens immediately.
- Permission resolution is server-side per request; the JWT carries identity, never authority.

## Events

`identity.UserCreated.v1` (outbox). Login/logout land in `audit_logs`.

## API surface

`/api/v1/auth/*`, `/api/v1/me`, `/api/v1/users*` — see `packages/contracts/openapi.yaml`.
