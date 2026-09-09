# API Conventions

- Versioned: `/api/v1/…`
- Consistent error envelope: `{ "error": { "code", "message", "details?" } }`
- Pagination: `limit`/`cursor` (cursor-based where high-cardinality), `X-Total-Count` on collection endpoints
- Idempotency: `Idempotency-Key` header required on financial mutations
- Request IDs: `X-Request-ID` propagated into logs and audit
- Authentication: Bearer JWT (15 min) + rotating refresh session (cookie-capable for web)
- OpenAPI source of truth: `services/api/openapi/openapi.yaml` + `packages/contracts`
