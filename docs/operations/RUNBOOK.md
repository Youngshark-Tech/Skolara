# Skolara Runbook

Operational guidance for running Skolara in production-like environments.

## 1. Topology

```
[Browser / Client]
      │ HTTPS
      ▼
[Reverse proxy / load balancer]   ← TLS termination, HSTS, /metrics ACL
      │ HTTP (private network)
      ▼
[skolara-api]  ──►  [PostgreSQL 16]   (source of truth)
      │                    ▲
      └── migrations (embedded, applied at boot)
```

- The API is a single static binary (distroless container, non-root, embedded migrations).
- Redis is provisioned in the compose stack for future use; the API does not require it yet.

## 2. Configuration (environment only — no secrets in files)

| Variable | Required | Notes |
|----------|----------|-------|
| `DATABASE_URL` | yes | `postgres://user:pass@host:5432/db?sslmode=require` (require TLS in prod) |
| `SKOLARA_ENV` | yes | `production` enables strict validation |
| `SKOLARA_JWT_SECRET` | yes | ≥ 32 random bytes; rotate with dual-accept window |
| `SKOLARA_WEBHOOK_SECRET` | yes | ≥ 32 bytes; shared with the payment provider signer |
| `SKOLARA_CORS_ORIGINS` | yes | exact web origins, comma separated |
| `SKOLARA_HTTP_ADDR` | no | default `:8080` |
| `SKOLARA_LOG_LEVEL` / `SKOLARA_LOG_FORMAT` | no | `info` / `json` in prod |
| `SKOLARA_RATE_LIMIT_RPS` / `SKOLARA_RATE_LIMIT_BURST` | no | per-IP token bucket |
| `SKOLARA_MAX_BODY_BYTES` | no | default 1 MiB |
| `SKOLARA_BOOTSTRAP_ADMIN_EMAIL` / `_PASSWORD` | first boot only | create the first platform admin; disable afterwards |

**Production refuses to boot** without a strong JWT/webhook secret and real CORS origins.

## 3. Deployments & migrations

- Migrations are embedded and applied **automatically at boot** (golang-migrate, atomic per file).
- Rolling deploys are safe: migrations are additive; destructive changes ship as expand/contract pairs.
- Manual control: `make migrate-up` / `migrate-down` (down applies one step).
- **Never edit an applied migration.** The ledger (000008/000009) is append-only by design.

## 4. Health, metrics, alerts

| Probe | Meaning | Alert |
|-------|---------|-------|
| `GET /healthz` | process alive | restart pod on fail |
| `GET /readyz` | DB reachable (2s timeout) | remove from LB on fail |
| `GET /metrics` | Prometheus | see below |

Suggested alerts: 5xx rate > 1% (5m); p99 `skolara_http_request_duration_seconds` > 1s; `readyz` flapping; outbox `attempts` growing (delivery degraded); Postgres connections ≥ 80% of pool (20).

`/metrics` must NOT be publicly reachable — restrict at the proxy (allow internal networks only).

## 5. Logs

Structured JSON on stdout: `http_request` access lines (method, path, status, duration_ms, request_id, actor_id, school_id) plus domain events. Ship to the log aggregator; search by `request_id`, `actor_id`, `school_id`. Passwords/tokens/secrets are scrubbed by the logger.

## 6. Backups & recovery

- PostgreSQL: nightly base backup + WAL archiving (PITR); restore drill quarterly.
- **RPO target: 15 min. RTO target: 1 h.**
- Ledger integrity after restore: `SELECT` a per-entry balance check across all journal entries (the DB trigger re-verifies new postings automatically).
- The API is stateless — scale by replicas; no local state to recover.

## 7. Incident playbook (abbreviated)

| Symptom | First actions |
|---------|---------------|
| 401 storm | Check JWT secret rotation mismatch; verify provider clock for token exp |
| Webhook 401s | Confirm provider signature secret matches `SKOLARA_WEBHOOK_SECRET` |
| `no_school_context` spike | Member deprovisioned? Check `school_memberships.status` |
| Ledger anomaly | DO NOT edit rows (blocked anyway); post a compensating entry referencing the suspect entry; page finance owner |
| DB saturation | Inspect slow queries; enforce pagination at the edge (already capped at 100) |

Account compromise: `PATCH /api/v1/users/{id}/status {"status":"disabled"}` — kills all refresh sessions immediately.

## 8. Onboarding a school (operator checklist)

1. Create education group (optional) → `POST /api/v1/education-groups`
2. Create school → `POST /api/v1/schools`
3. Grant memberships → `POST /api/v1/schools/{id}/members` (roles: school_admin, teacher, …)
4. Seed academics: academic year → terms → subjects → classes → roster
5. Verify: admin signs in, switches school (web shell), creates a learner, records attendance, issues an invoice
6. Finance chart seeds itself lazily on first financial operation (`wallet` GET or first posting)

## 9. Local development

See [CONTRIBUTING](../../CONTRIBUTING.md). Compose stack: `docker compose -f infrastructure/docker-compose.yml up --build`. Live E2E smoke: `scripts/e2e-smoke.sh` (26 checks over real HTTP).
