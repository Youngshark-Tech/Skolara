# Skolara Demo Mode

Demo mode seeds an **idempotent demo dataset** at API startup so a fresh
deployment always has working login details and a populated workspace
(issue #128). It is enabled with the environment variable:

```bash
SKOLARA_DEMO_SEED=true
# Optional — override the documented password (fresh seeds only):
SKOLARA_DEMO_PASSWORD=<your-own-demo-password>
```

> **SECURITY — READ BEFORE ENABLING ON ANY REAL DEPLOYMENT**
> Demo credentials are public knowledge (documented here and in the README).
> Demo mode must NEVER be enabled on a deployment that holds real student,
> guardian, or financial data. The startup log prints a loud warning whenever
> it is active.

## Demo accounts

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

## Removing demo data

Demo mode does not delete anything (it is additive and idempotent). To retire a
demo deployment, disable `SKOLARA_DEMO_SEED` and drop the database (migrations
re-apply on next boot). Do not point a demo-seeded deployment at a database
containing real data.
