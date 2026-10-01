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
[skolara-api]  ──►  [PostgreSQL 17]   (source of truth)
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
| `SKOLARA_HTTP_TIMEOUT` | no | request timeout, default `30s` |
| `SKOLARA_SHUTDOWN_PERIOD` | no | graceful drain window, default `15s` |
| `SKOLARA_LOG_LEVEL` / `SKOLARA_LOG_FORMAT` | no | `info` / `json` in prod |
| `SKOLARA_ACCESS_TOKEN_EXPIRY` | no | default `15m` (short-lived by design) |
| `SKOLARA_REFRESH_TOKEN_EXPIRY` | no | default `720h` (30 days, rotating) |
| `SKOLARA_RATE_LIMIT_RPS` / `SKOLARA_RATE_LIMIT_BURST` | no | per-IP token bucket (evicts idle buckets; see #52) |
| `SKOLARA_MAX_BODY_BYTES` | no | default 1 MiB |
| `SKOLARA_REDIS_ADDR` | no | **reserved, currently unused by the API** — compose keeps the service for the shared rate limiter on the roadmap |
| `SKOLARA_BOOTSTRAP_ADMIN_EMAIL` / `_PASSWORD` | first boot only | create the first platform admin; disable afterwards |

Migrations are **embedded in the API binary** — there is no runtime migrations-directory override. The standalone `services/api/cmd/migrate` tool accepts `SKOLARA_MIGRATIONS_DIR` (default `./migrations`) for manual operation only.

**Production refuses to boot** without a strong JWT secret, a webhook secret ≥ 32 bytes, and real CORS origins.

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

Minimum alert set (Prometheus rules example — paste into your rules file and tune thresholds per environment):

```yaml
groups:
  - name: skolara
    rules:
      - alert: SkolaraAPIDown
        expr: up{job="skolara-api"} == 0
        for: 1m
      - alert: SkolaraReadinessFailing
        expr: probe_success{job="skolara-readyz"} == 0
        for: 2m
      - alert: SkolaraHigh5xx
        expr: sum(rate(skolara_http_requests_total{code=~"5.."}[5m]))
              / sum(rate(skolara_http_requests_total[5m])) > 0.01
        for: 5m
      - alert: SkolaraSlowRequests
        expr: histogram_quantile(0.99, sum(rate(skolara_http_request_duration_seconds_bucket[5m])) by (le)) > 1
        for: 5m
      - alert: SkolaraOutboxDeadLetters
        expr: increase(skolara_events_deadlettered_total[15m]) > 0
      - alert: SkolaraOutboxBacklog
        expr: skolara_outbox_unpublished > 100   # or publisher-side equivalent gauge
        for: 10m
      - alert: SkolaraPGPoolSaturation
        expr: pg_stat_activity_count / pg_settings_max_connections > 0.8
        for: 5m
```

`/metrics` must NOT be publicly reachable — restrict at the proxy. nginx example (allow the private CIDR only):

```nginx
location = /metrics {
    allow 10.0.0.0/8;      # private network(s) that run monitoring
    allow 127.0.0.1;
    deny all;
    proxy_pass http://skolara-api:8080;
}
```

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

## 9. Local development & developer tools

See [CONTRIBUTING](../../CONTRIBUTING.md). Compose stack: `docker compose -f infrastructure/docker-compose.yml up --build`.

**E2E smoke prerequisites** (the script logs in as the bootstrap admin and signs webhooks): start the API with `SKOLARA_BOOTSTRAP_ADMIN_EMAIL=admin@skolara.test`, a matching `SKOLARA_BOOTSTRAP_ADMIN_PASSWORD` (script default `S0pera!Admin2026`), and `SKOLARA_WEBHOOK_SECRET=dev-webhook-secret-0123456789abcdef` — the compose api service ships these dev defaults (override via `.env`). Then `scripts/e2e-smoke.sh [BASE_URL]` (26 checks over real HTTP). Requires `python3` on PATH (JSON parsing + HMAC signature).

**Developer tools**: `services/api/cmd/dbq` is a raw-SQL debug utility (admin/roles/membership counts for `admin@skolara.test`). It executes arbitrary SQL against the `DATABASE_URL` you pass — dev/debug use only, never in the production path. The `services/api/cmd/migrate` tool applies/rolls back migrations manually (`up`, `down [N]`; exit 0 includes documented no-ops, 1 real failure, 2 usage error).

## 10. Hosting topologies

The repository carries a multi-service `vercel.json` (web + api). Two supported shapes:

- **Co-located** (same site, e.g. reverse-proxied `app.example.com` → web, `app.example.com/api` → api): refresh cookies work with `SameSite=Lax`; this is the default contract.
- **Split-domain** (web on `app.example.com`, API on `api.example.com`): cross-site `fetch` will NOT send Lax cookies — silent refresh breaks. This requires `SameSite=None; Secure` on the refresh cookie (with the CSRF review in `docs/security/THREAT_MODEL.md`) or a same-site topology. The decision is tracked in issue #83 — do NOT deploy split-domain until it lands. `NEXT_PUBLIC_API_URL` must be set in the **web build** environment (it is inlined into the client bundle at build time; see `apps/web/.env.example`).
