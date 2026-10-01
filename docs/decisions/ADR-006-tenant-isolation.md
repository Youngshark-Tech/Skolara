# ADR-006: Tenant Isolation Model

**Status:** Accepted · **Date:** 2026-09-09

## Context

Multi-tenancy is foundational (§16): Education Group → School → Campus → Users. No school may access another school's data. Isolation must never rely on the frontend.

## Decision

**Defense in depth across four layers:**

1. **Identity layer** — `school_memberships` bind users to schools with roles; a JWT carries the *active school context* selected from verified memberships only.
2. **Middleware layer** — `tenancy.Middleware` extracts the active school from the verified token (never from client-controlled body/headers), injects it into request context; routes without school context cannot reach tenant-scoped repositories.
3. **Repository layer** — every tenant-scoped repository method **requires** the school from context and appends `WHERE school_id = $ctx` to every query and mutation; there is no API to bypass it. Writes set `school_id` from context, never from payloads.
4. **Database layer** — FK constraints on `school_id`; composite indexes lead with `school_id`; unique constraints are school-scoped (e.g. `UNIQUE(school_id, code)`).

**Failure semantics:** foreign-tenant resources return **404** (not 403) to avoid resource enumeration. Cross-tenant access attempts are audit-logged.

**Tests are acceptance-critical:** an isolation matrix test (user of school A attempting CRUD against school B resources across every domain) is part of the QA gate.

## Consequences

- **Positive:** consistent, testable isolation without per-request policy evaluation cost; simple reasoning for reviewers ("is school_id from context?").
- **Negative:** Row-Level Security (Postgres RLS) not enabled initially — application-layer guard is the enforced contract now; RLS is the documented hardening step for multi-region scale (tracked issue).

## Alternatives Considered

- Postgres RLS from day one: stronger but heavier to operate with connection pooling and test complexity; chosen as hardening step once posture is stable.
- Schema-per-tenant: rejected — migration explosion across hundreds of schools.

## Amendment (2026-10) — active-school resolution

The original wording above (layer 1: "a JWT carries the *active school context*"; layer 2: middleware "extracts the active school from the verified token (never from client-controlled body/headers)") was **inaccurate** and is corrected here (docs audit, issue #64). The implemented resolution (see `services/api/internal/tenancy/http.go` and `internal/tenancy/service.go`):

- The client selects the desired school per request via the **`X-School-ID` header**.
- The server **validates that header against the caller's ACTIVE `school_memberships`** before honoring it; a school the caller does not belong to (or an inactive membership) is rejected, and requests without any resolvable school context cannot reach tenant-scoped repositories.
- The header value is therefore **never trusted directly** — the membership lookup is the source of truth. The original claim that school context comes "from the verified token" was wrong; what the token certifies is the *user identity* whose memberships are checked.
- Failure semantics are unchanged: foreign-tenant resources still return **404**, and the isolation-matrix test remains part of the QA gate.

`docs/security/THREAT_MODEL.md` already described the header-based resolution correctly; it is the reference description.
