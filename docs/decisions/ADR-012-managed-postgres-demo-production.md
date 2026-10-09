# ADR-012: Managed Postgres Strategy — Vercel Postgres (Neon) for Demo, Supabase for Production

**Status:** Accepted · **Date:** 2026-10-09

## Context

Skolara currently runs its public demo with **no database at all** (in-memory
mock transport — issues #142/#153). The next step for the product is a
**real-database demo** (persistent data across reloads, the API service
live), and later a **production deployment** holding real student, guardian,
and financial data.

The operator asked whether the demo can run on **Postgres as provided by
Vercel** and production can later switch to **Supabase**.

Relevant existing constraints:

- **ADR-002** fixed PostgreSQL as the transactional source of truth and the
  schema as **plain, provider-neutral Postgres** — no vendor-proprietary
  features are used by any migration (`services/api/migrations/`, verified
  up/down/up in CI).
- Migrations are **embedded in the API binary** and applied automatically at
  boot (golang-migrate, atomic per file); the migration lock serializes
  concurrent cold starts (serverless included).
- The API connects with a single `DATABASE_URL` (pgx pool) — there is no
  provider-specific configuration anywhere in the codebase.
- Vercel's Postgres offering is **Neon-backed** and speaks the standard
  Postgres wire protocol; Supabase is standard Postgres fronted by Supavisor
  (connection pooling). Both are drop-in compatible at the protocol level.

## Decision

1. **Yes — demo on Vercel Postgres, production on Supabase, with zero code
   changes.** The provider is chosen entirely by `DATABASE_URL`. Provisioning
   order: create the database → copy its connection string into the api
   service's `DATABASE_URL` → redeploy. The first API boot applies every
   migration; the schema that lands on Supabase is byte-identical to the one
   that landed on Vercel Postgres.
2. **Connection strings:**
   - **Vercel Postgres (demo/staging):** use the **pooled** connection string
     Vercel shows by default (`...-pooler...` host). Serverless functions open
     many short-lived connections; the pooler is the correct default there.
     Append `?sslmode=require` if not already present.
   - **Supabase (production):** prefer the **session pooler** (port `5432`)
     as the default `DATABASE_URL` — it is fully compatible with pgx's
     prepared statements out of the box.
   - If the **transaction pooler** (port `6543`) is required at Supabase
     scale, append pgx's simple-protocol execution mode to the DSN:
     `?sslmode=require&default_query_exec_mode=simple_protocol`. This avoids
     prepared-statement reuse across pooled transactions (the one known
     incompatibility). No application code changes — the mode is a DSN
     parameter.
3. **One database per environment.** Demo and production NEVER share a
   database (tenant isolation is enforced *within* a database by school
   scoping — ADR-006 — but environments stay physically separate so a demo
   can never leak into production data).
4. **Migrations stay embedded and boot-applied in every environment.** No
   provider-specific migration step; the CI up/down/up gate remains the
   contract for schema portability.
5. **`SKOLARA_DEMO_SEED`** may be enabled on the demo database (it seeds the
   documented demo accounts) and **must remain unset** on production.

## Consequences

- **Positive:** switching providers is a configuration edit + redeploy, not a
  project; demo and production can use different vendors freely (price,
  region, backup posture); the schema remains portable by CI-enforced
  construction.
- **Negative:** transaction-pooling mode trades a small per-query overhead
  (simple protocol) for connection density — acceptable for school-scale
  workloads, revisitable if hot paths ever need extended protocol (then use
  the session pooler or a dedicated PgBouncer in front).
- **Neutral:** backups/PITR are provider-managed; the RUNBOOK's backup
  posture (§6) applies to whichever provider holds the environment.

## References

- [DEPLOY_VERCEL.md](../operations/DEPLOY_VERCEL.md) — environment variable table (`DATABASE_URL` row)
- [RUNBOOK.md](../operations/RUNBOOK.md) §2-3, §6 — configuration contract, boot migrations, backups
- [DEMO.md](../operations/DEMO.md) — demo postures and the `SKOLARA_DEMO_SEED` envelope
- ADR-002 (PostgreSQL source of truth), ADR-006 (tenant isolation)
