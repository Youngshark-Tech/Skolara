# Skolara QA Report — Production Readiness Assessment

**Report date:** 2026-09-09 · **Assessment:** Full-repo audit + implementation + verification across backend, contracts, frontend, and operations docs.
**Verdict: READY FOR CONTROLLED CUSTOMER ONBOARDING** (single-school and small-group pilots), with the roadmap items in `docs/operations/ROADMAP.md` tracked for general availability.

---

## 1. Scope of this assessment

- Full repository audit (backend → frontend → UI → infra → docs → git history)
- All GitHub issues #1–#15 (roadmap) and audit findings #23–#27 closed via reviewed PRs
- Test suites: unit, integration (real PostgreSQL 17), contract drift, invariant (finance), and a 26-check live E2E smoke over real HTTP
- Every change landed through the Issue → Branch → PR → CI-equivalent gates → Merge workflow; no direct commits to `main`

## 2. Verified capabilities (evidence-based)

| Capability | Evidence |
|------------|----------|
| AuthN: argon2id, rotating refresh sessions w/ reuse detection, lockout | `identity` unit + integration tests |
| AuthZ: server-side RBAC (13 roles × 16 permissions), deterministic multi-membership access | tenancy/identity integration tests (#24/#25 regression suites) |
| Multi-tenancy: school-scoped queries, 404-not-403 enumeration guards, RequireSchool at composition root | tenant isolation matrices per domain + live smoke |
| User lifecycle: disable/enable with full session revocation | `TestUserLifecycleDisableEnable` |
| Students: global learner identity, guardians w/ access flags, 9-state enrollment machine | `TestEnrollmentLifecycleMatrix` (14 legal + illegal edges) |
| Academics: years/terms (overlap+nesting policy), subjects, classes, rosters, assignments of teachers | academics integration suite |
| Attendance: idempotent offline-style batch upserts, absence events | `TestAttendanceRecordIdempotentReplay` + HTTP flow |
| Assignments: publish→submit→grade→return, owner-only grading | assignments integration suite |
| Finance: double-entry ledger, immutable postings (DB-trigger proven), balanced-by-trigger, idempotent postings + webhooks, invoice allocation, derived wallet | §59 invariant suite (randomized postings, duplicate-webhook zero-posting proof) |
| Contract-first API: OpenAPI 3.1 spec of all 45+ endpoints + generated TS types + two drift gates | `contract` drift test + `npm run check` |
| Web foundation: login, role-aware shell, command center, students UI | typecheck + 8 unit tests + lint + production build |
| Observability: per-route Prometheus metrics, structured access logs w/ actor+tenant, outbox delivery counter | observability/httpx tests + live smoke |

## 3. Live E2E smoke (26/26 ✓)

`scripts/e2e-smoke.sh` against the real API + PostgreSQL 17 exercises the full product loop:
health → login → me → refresh rotation → school → membership → learner → enrollment (admitted→active) → year/term/subject/class → roster → attendance session + idempotent records → invoice → payment intent → signed webhook confirm → invoice paid → duplicate-webhook replay (zero postings) → wallet inflow → forged-signature rejection → metrics exposure.

## 4. Security posture

See `docs/security/THREAT_MODEL.md` (STRIDE). Highlights: no plaintext secrets; credential hashing; refresh reuse detection; webhook HMAC with constant-time compare; tenant isolation enforced in SQL; permission checks resolved server-side per request; pagination/body/rate caps; scrubbed structured logs.

## 5. Known gaps (tracked, non-blocking for pilots)

1. **CI runners**: GitHub Actions currently fails at startup (account-level billing) — all gates are executed locally and documented; re-enable Actions to restore automated gating.
2. **Rate limiting is per-process** — add a shared Redis limiter before multi-replica production.
3. **`/metrics` is unauthenticated** — restrict at the edge (runbook §4).
4. **Learner↔user linking** not implemented; learner-facing surfaces must enforce object-level authz when built.
5. **Notification fabric** (outbox → SMS/email) pending; absence events accumulate in the outbox.
6. **Password reset / account recovery** flow pending.
7. Web workspaces beyond Students/Command Center are contract-wired placeholders.

## 6. Quality metrics at assessment time

- Go packages: 7 bounded contexts + platform; `go vet` + `gofmt` clean; `-race` clean
- Tests: 60+ test functions across unit + integration (incl. property/invariant suites); web: 8 unit tests
- Migrations: 9 (all with tested down-migrations; up/down verified by `TestMigrationsUpAndDown`)
- Live smoke: 26/26
- Contract: 45+ documented paths, zero drift

## 7. Sign-off

The platform is internally consistent, tenant-isolated, financially correct by provable invariant, observable, and documented for operators. Proceed to onboarding per the runbook checklist; track the gaps above in the roadmap.
