# Contributing to Skolara

## The Non-Negotiable Development Rule

**NEVER work directly on `main`.** Every meaningful change happens through:

```
Issue → Branch → Implementation → Tests → Pull Request → Review → CI → Merge → Issue Closure
```

A PR must solve its issue completely. PRs containing half-built features, placeholder
implementations, fake APIs, disabled tests, or known broken builds are rejected.

## Branch Naming

- `feat/<issue#>-<slug>` — feature work (e.g. `feat/7-identity-auth`)
- `fix/<issue#>-<slug>` — bug fixes
- `chore/<issue#>-<slug>` — tooling, infra, docs
- `qa/<issue#>-<slug>` — test infrastructure

## Local Development

### Backend (services/api)

Requires Go 1.23+, PostgreSQL 16, and Docker (or a local Postgres).

```bash
cd services/api
cp .env.example .env                 # configure DATABASE_URL, JWT_SECRET, etc.
make migrate-up                      # apply database migrations
make run                             # start API on :8080
make test                            # unit tests (no DB required)
DATABASE_URL=postgres://... make test-integration   # integration tests
```

### Frontend (apps/web)

Requires Node 20+.

```bash
cd apps/web
npm install
npm run dev                          # start web app on :3000
npm run build && npm run start       # production build
npm run lint && npm run typecheck    # quality gates
```

### Full stack (Docker Compose)

```bash
docker compose -f infrastructure/docker-compose.yml up --build
# API on :8080, Web on :3000, Postgres on :5432, Redis on :6379
```

## Conventions

- **Migrations**: one migration per issue, timestamp-prefixed, never rewrite another
  migration — see `services/api/migrations/` and `docs/decisions/`
- **API**: versioned under `/api/v1/`, consistent error envelope, cursor/limit-offset
  pagination, idempotency keys on financial mutations
- **Events**: domain events recorded in the transactional outbox with schema versions
- **Commits**: Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`)
- **Security**: never commit secrets; never log passwords/tokens; authorization is
  enforced server-side; every tenant-scoped query filters by school context

## Pull Request Standard

Every PR must document: Summary, Problem (issue ref), Architecture, API changes,
Database changes, Events, Security considerations, Tests, Verification commands,
Known limitations. Screenshots are required for meaningful frontend changes.

## Definition of Done

Implementation complete, tests written and passing, lint/format/typecheck passing,
migrations validated, contracts validated, security considered, tenant isolation
tested, observability added where appropriate, documentation updated, PR reviewed
and merged, issue closed.
