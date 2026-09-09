# Changelog

All notable changes to Skolara are documented here. Format based on Keep a Changelog.

## [1.0.0-alpha] — 2026-09-09

### Added
- Platform foundation: config, structured logging, HTTP middleware (recover/security headers/access log/request ID/CORS/body limit/rate limit/timeout), Postgres pool, transactional outbox, observability (health/readyz/per-route Prometheus metrics).
- Identity: argon2id credentials, JWT access tokens + rotating refresh sessions with family reuse detection, lockout policy, 13×16 RBAC matrix, audit log, user disable/enable with session revocation.
- Tenancy: education groups → schools → campuses; school memberships with school-scoped roles; deterministic tenant resolution (`RequireSchool`).
- Students: global learner identity, guardians with per-link access flags, enrollment lifecycle state machine (9 states, optimistic concurrency).
- Academics: academic years, terms (overlap/nesting policy), subjects, class groups, rosters (idempotent bulk), teaching assignments.
- Attendance: per-class daily sessions, offline-tolerant idempotent bulk records (`clientMutationId`), absence/session-close events.
- Assignments: draft→published→closed, submissions→graded→returned, owner-only publish/grade, due-date policy.
- Finance (ADR-004/005): double-entry ledger with immutable postings and balance triggers, idempotent postings/webhooks, fee structures, invoices with allocation, server-side-confirmed payments (HMAC webhooks), institution wallet with derived purpose balances.
- Contracts: complete OpenAPI 3.1 spec + generated TypeScript types + drift gates (Go contract test, npm check).
- Web: Next.js 15 foundation — auth, role-aware shell, command center, students UI.
- Operations: threat model, runbook, domain docs, QA report, roadmap, live E2E smoke (26 checks).

### Fixed
- Deterministic school access for multi-membership users (EXISTS over all active rows).
- Handlers route through the service layer; invalid role/user references return 4xx.
- Pagination + X-Total-Count on all high-cardinality collections.
- Observability wiring (metrics/access log/outbox counter were dead code).
- Migration driver: golang-migrate pgx/v5 truncated multi-statement files — switched to lib/pq.
- Composition root: RequireSchool now actually mounted (caught by live E2E smoke).

## [0.1.0] — repository foundation
- Conventions, master spec preservation, ADR-001…010, docs skeleton, CI pipeline, Docker/Compose stack.
