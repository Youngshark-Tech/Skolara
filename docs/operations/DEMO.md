# Skolara Demo Mode

Skolara has THREE deployment postures on the demo spectrum, from the most
detached to the most production-like:

| Mode | Flag | Database | API service | Auth surface |
|------|------|----------|-------------|--------------|
| **Demo data mode** | `NEXT_PUBLIC_DEMO_MODE=true` (web) | **None** | Not needed | Disabled, any credentials sign in |
| **Open-access mode** | `NEXT_PUBLIC_AUTH_BYPASS=true` (web) | Required | Required | Disabled, auto sign-in as demo admin |
| **Full production posture** | none | Required | Required | Normal login (+ optional demo accounts below) |

All are temporary and demo-only — see the security envelope at the bottom.

> **Zero-config default (issue #153, temporary):** a web build with **no
> `NEXT_PUBLIC_DEMO_MODE` value** ships demo data mode — a fresh deployment is
> a working demo with no configuration at all. An explicit value always wins:
> `NEXT_PUBLIC_DEMO_MODE=false` is the go-live switch (real API + database);
> unsetting the flag hands the deployment back to the demo, so production
> cutovers must SET it to `false`, not merely remove it.

## Demo data mode (no database at all)

For product demos and staging reviews before the database is provisioned, the
web app can run ENTIRELY on an in-memory sample dataset (issue #142):

```bash
# Web app — DEFAULT since issue #153: a build with the variable UNSET ships
# demo data mode. Set it explicitly to be unambiguous (build-time, REDEPLOY
# after any change):
NEXT_PUBLIC_DEMO_MODE=true

# Go live (real API + database) — the explicit off switch:
NEXT_PUBLIC_DEMO_MODE=false
```

What changes:

- Every API call is answered by an in-memory mock transport
  (`apps/web/src/lib/mock`) that mirrors the real API contract: the same
  envelopes, status codes, error mapping, pagination headers (X-Total-Count),
  and abort semantics. The Go API service and PostgreSQL are not contacted at
  all — the deployment needs no `DATABASE_URL`.
- Authentication is disabled (implied `NEXT_PUBLIC_AUTH_BYPASS`): any email
  and password sign in, sessions never expire, and the sign-in name is derived
  from the email's local part.
- The seeded dataset mirrors the API's demo seed and covers the WHOLE
  enrollment lifecycle: 12 learners, an academic year pair, two class groups,
  enrollments in all nine states, wallet balances, and invoices.
- Writes are interactive: creating learners, enrolling, and lifecycle
  transitions all work — through the same validation and state machine as the
  real API (including the "new learners become searchable only after their
  first enrollment" behavior).
- A **"Demo data — not live"** badge is pinned in the sidebar so sample
  figures are never mistaken for live ones.
- Sample data resets on every full page load — deliberate, so a demo always
  starts from the clean seed.

**Connecting production later (the point of this mode):** unset
`NEXT_PUBLIC_DEMO_MODE` on the web app, set `DATABASE_URL` (+ the API
variables from [DEPLOY_VERCEL.md](DEPLOY_VERCEL.md)) on the api service, and
redeploy. Migrations and schemas are already in place and CI-verified
(`services/api/migrations/`, up/down/up gate) — no data work is needed; the
app flips from mock transport to the real fetch client byte-for-byte.

## Open-access mode (authentication bypass, temporary)

For demo/staging deployments with a real database where even typing the demo
credentials is friction, the web app can disable the login surface entirely
(issue #141):

```bash
# Web app (build-time — REDEPLOY after changing it):
NEXT_PUBLIC_AUTH_BYPASS=true
# Optional overrides — must match what the API actually seeded:
NEXT_PUBLIC_BYPASS_EMAIL=admin@skolara.dev
NEXT_PUBLIC_BYPASS_PASSWORD=SkolaraDemo!2026

# API (required, or the automatic sign-in has nothing to log into):
SKOLARA_DEMO_SEED=true
```

What changes:

- The edge guard stops redirecting unauthenticated visitors to `/login`.
- On boot, the session provider exchanges the seeded demo credentials for a
  **real session** (the same login endpoint, JWT, refresh rotation, RBAC,
  tenant scoping, and audit as production). The visitor simply never sees the
  form.
- `/login` shows a demo-mode note and retries the automatic sign-in once if
  boot could not establish a session.
- "Sign out" is hidden in the app shell: logging out would instantly
  auto-relogin, so the control would be misleading.
- The hero landing page shows an **Open workspace** CTA instead of
  Log in / Sign up once the automatic session is live.

Failure is loud, never silent: if the demo dataset is not seeded (the API
runs without `SKOLARA_DEMO_SEED=true`) or the password override does not
match, the login surface shows the API's real error and the note above.

## Standard demo seed

The API-side idempotent demo dataset (issue #128) provisions working login
details for any fresh deployment so a production posture always has accounts
to sign in with:

```bash
SKOLARA_DEMO_SEED=true
# Optional — override the documented password (fresh seeds only):
SKOLARA_DEMO_PASSWORD=<your-own-demo-password>
```

| Account | Email | Password | Workspace role |
|---------|-------|----------|----------------|
| School admin | `admin@skolara.dev` | `SkolaraDemo!2026` | School administrator (full school workspace) |
| Teacher | `teacher@skolara.dev` | `SkolaraDemo!2026` | Teacher (teaching-scoped workspace) |

Both accounts have **no platform-scope roles** — their authority comes purely
from school memberships, following the role-scope model hardened in issue #40.
If the optional `SKOLARA_DEMO_PASSWORD` is set, it replaces the default
password for newly seeded accounts (existing users are never re-passworded, so
re-running the seed keeps whatever was there first).

## What gets seeded

Everything below is created through the real domain services, so the dataset
exercises the same validations, guarded transitions, audit entries, and outbox
events as production traffic:

| Object | Natural key (idempotency key) | Value |
|--------|-------------------------------|-------|
| Education group | name | Skolara Demo Group |
| School | code | Riverside High School (`RVS-001`) |
| Campus | code | Main Campus (`MC`) |
| Academic year | school + name | 2026 (2026-01-05 → 2026-11-20) |
| Class group | school + year + name | Grade 8 - Blue |
| Learners × 5 | external admission number | `DEMO-L001` … `DEMO-L005` (Amina Njeri Otieno, Brian Kiprop Mutai, Cynthia Awuor Ochieng, Daniel Mwangi Kamau, Esther Wanjiku Njoroge) |
| Enrollments × 5 | learner + school (one live each) | status `admitted` into Grade 8 - Blue, year 2026 |
| Roster entries × 5 | class group + learner | Grade 8 - Blue roster |
| Memberships × 2 | user + school | admin → `school_admin`, teacher → `teacher` |

## Idempotency

The seed is safe to run repeatedly (every API boot while enabled) and safe
under concurrent cold starts:

- Users are keyed by email; memberships by active-membership existence.
- Group / school / campus / year / class group are keyed by name or code.
- Learners are keyed by the globally-unique external admission number.
- Enrollments respect the students domain's live-enrollment guard; roster
  inserts are skipped per-learner when the pair already exists.

This is proven by an integration test (`internal/demo`) that runs the seed
three times against a live PostgreSQL and asserts exact object counts.

## Security envelope — read before enabling anything on real data

> Demo credentials are public knowledge (documented here and in the README),
> and every mode above hands visitors working sessions. These postures must
> NEVER be enabled on a deployment that holds real student, guardian, or
> financial data. The API prints a loud startup warning whenever
> `SKOLARA_DEMO_SEED` is active; the web modes are guarded only by
> configuration discipline, so review the environment variables before every
> production cutover.
>
> **Returning to full production:** set `NEXT_PUBLIC_DEMO_MODE=false` on the
> web app (do NOT merely unset it — since #153 an unset flag means the demo
> default applies) and unset `NEXT_PUBLIC_AUTH_BYPASS`, keep
> `SKOLARA_DEMO_SEED` unset on the API, and redeploy. No code changes are
> involved — the switches exist only in configuration.

## Removing demo data

Demo mode does not delete anything (it is additive and idempotent). To retire a
demo deployment, disable `SKOLARA_DEMO_SEED` and drop the database (migrations
re-apply on next boot). Do not point a demo-seeded deployment at a database
containing real data.
