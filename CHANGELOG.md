# Changelog

All notable changes to Skolara are documented here. Format based on Keep a Changelog.

## [1.0.0-beta] — 2026-10-01

Audit round 3 — full-system audit execution wave (25 issues closed via reviewed PRs #65–#105).

### Added
- Web: enrollment lifecycle UI (9-state admit story), middleware route guard, CSP/HSTS/frame/referrer/permissions headers, mobile-responsive shell, error/loading/not-found boundaries, favicon + per-page metadata, accessible forms (labels, aria-current, skip-link, ≥4.5:1 contrast).
- Platform: transactional outbox at every call site (ADR-003 enforced by per-domain rollback tests), SKIP LOCKED dispatcher with dead-letter counter + `last_error`, bounded rate-limiter buckets with idle eviction.
- CI: contracts job (real drift gate — generated types checked in, gate fails on modified AND untracked output), migrations job (up→down→up reversal proof), e2e job (26-check smoke over real HTTP), docker build job, coverage floor, Dependabot (4 ecosystems), govulncheck pinned + clean; all silent-skip guards removed.
- Infra: real container healthchecks (API probes /healthz; web /api/health route), compose smoke prerequisites (webhook secret + bootstrap admin), prod config refuses webhook secrets < 32 bytes.
- Runbook: complete env table, concrete Prometheus alert rules, /metrics edge-ACL example, developer tools, hosting topologies (co-located vs split-domain cookie contract).
- ADR-011 (memory-token + silent refresh) + ADR-006/008 amendments; hardened e2e smoke (mktemp/trap, --max-time, asserted membership grant).

### Fixed
- Web: concurrent-401 refresh race (single-flight — no more family-revocation logouts on token expiry), dead session after final 401, school state surviving logout, token removed from localStorage, dashboard stat refetch, debounced search with AbortController, pagination zero-state, nav permission mismatches.
- Platform: MigrateDown mis-wired direction (negative n ran Up) with regression tests; cmd/migrate exit-code contract documented.
- Toolchain: Go 1.25.13 pinned across go.mod/Dockerfile/docs (govulncheck clean); x/text, pgx 5.11, prometheus/client 1.24, react 19.3 + web dev-dep minors.
- Migrations driver safety net: migration-reversal test now drives down through the fixed helper.

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
