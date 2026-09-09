# API Conventions

- **Single source of truth**: [`packages/contracts/openapi.yaml`](../../packages/contracts/openapi.yaml) — OpenAPI 3.1, all `/api/v1` endpoints, schemas, error envelope, pagination and idempotency conventions.
- **Generated types**: [`packages/contracts/generated/schema.d.ts`](../../packages/contracts/generated/schema.d.ts) — regenerated via `npm run generate`, drift-checked in CI (`npm run check` → `git diff --exit-code`) and by the Go `contract` drift test.
- Versioned: `/api/v1/…`
- Consistent error envelope: `{ "error": { "code", "message", "details?" } }`
- Pagination: `limit` (default 50, max 100) / `offset`, `X-Total-Count` header + `total` in body on collection endpoints
- Idempotency: financial mutations accept client-supplied keys (journal entries); payment webhooks are idempotent by provider event id; attendance records by `clientMutationId`
- Request IDs: `X-Request-ID` propagated into logs and audit
- Authentication: Bearer JWT (15 min) + rotating refresh session (HttpOnly cookie-capable)
- Tenant context: `X-School-ID` of an ACTIVE membership (or platform admin); resolved server-side, never trusted from payloads
- Money: integer minor units only
