# Skolara Threat Model

**Scope:** Skolara API (`services/api`) and web app (`apps/web`) — multi-tenant school operating platform.
**Framework:** STRIDE per trust boundary. **Status:** v1.0 (initial release).

## Assets

| Asset | Sensitivity | Notes |
|-------|-------------|-------|
| Learner PII (names, DOB, external ids) | High — child data | strictest handling |
| Guardian contact data | High | phone/email |
| Credentials (argon2id hashes, refresh tokens) | Critical | hashed: SHA-256(opaque token) |
| Financial truth (ledger, invoices, payments) | Critical | immutable postings |
| Session/audit records | High | non-repudiation |
| JWT signing secret / webhook secret | Critical | config-only, never in code |

## Trust boundaries

1. Internet ↔ API (bearer JWT / HMAC webhook)
2. Browser ↔ API (HttpOnly refresh cookie; CORS allow-list)
3. Tenant ↔ tenant (school scoping in every query)
4. Role ↔ action (server-side RBAC resolution)
5. Provider ↔ API (webhook signature verification)

## STRIDE analysis

### Spoofing
- **Credential replay**: refresh tokens are 256-bit random, stored as SHA-256 hashes, rotated on every use with **family reuse detection** (replay revokes the whole chain) — *mitigated*.
- **JWT forgery**: HS256 with ≥32-byte secret, algorithm pinned via `WithValidMethods`, exp required — *mitigated*.
- **Webhook spoofing**: HMAC-SHA256 over the raw body, constant-time compare; unsigned/forged requests → 401 — *mitigated* (proven by E2E smoke).
- **Tenant spoofing**: `X-School-ID` is never trusted alone — resolved against ACTIVE memberships (or platform admin) server-side — *mitigated*.

### Tampering
- **Ledger mutation**: DB triggers reject UPDATE/DELETE on journal rows; corrections are compensating entries — *mitigated* (integration-tested).
- **Unbalanced postings**: enforced in application code AND by a deferrable constraint trigger at commit — *mitigated*.
- **Invoice/payment state jumps**: CAS updates (pending→confirmed once; status guarded by expected source state) — *mitigated*.

### Repudiation
- Audit log records actor, action, resource, before/after, request id for auth and admin operations — *partial* (ledger carries actor/correlation; extend audit to all domain mutations).

### Information disclosure
- Cross-tenant reads: every repo query carries `school_id`; foreign resources return **404** (not 403) to avoid enumeration — *mitigated*.
- Secret leakage in logs: logger scrub-list (password/token/secret/authorization/pin); DB URL redacted — *mitigated*.
- Error leakage: 500s return a generic envelope; invalid references return 4xx without DB internals — *mitigated*.
- Transport: TLS terminates at the deployment edge (runbook); HSTS to be added at the edge — *open (deployment)*.

### Denial of service
- Rate limiting per client IP (token bucket), 1 MiB body cap, request timeouts, bounded pagination (max 100), attendance batch caps (500) — *mitigated*.
- Postgres pool bounds + connection lifetime — *mitigated*.

### Elevation of privilege
- RBAC: permissions resolved server-side per request (platform roles ∪ active memberships); assignment ownership enforced (owner-only grading); unknown role/user references rejected with 4xx — *mitigated*.
- Multi-membership access determinism: any-active-row EXISTS check — *mitigated* (regression-tested).

## Known limitations / follow-ups

1. In-process rate limiting: replicas need a shared limiter (Redis) before horizontal scaling.
2. `/metrics` is unauthenticated — restrict at the edge / network policy (runbook).
3. Learner↔user identity linking not yet implemented; learner-facing surfaces must enforce object-level authorization when they land.
4. Notification fabric (events → SMS/email) does not exist yet; absence events accumulate in the outbox only.
5. Account recovery (password reset) flow pending.

## Review cadence

Re-review on every new trust boundary (new transport, provider integration, mobile client) and at least each minor release.
