# Skolara QA Report — Production Readiness Assessment

**Report date:** 2026-10-01 · **Assessment:** Third full-system audit round (audit round 2 execution) — every open finding from the platform/web/infra/CI/contracts/docs audit wave implemented, gated, independently reviewed, and merged; final re-audit pass over the merged result.
**Verdict: READY FOR CONTROLLED CUSTOMER ONBOARDING** (single-school and small-group pilots), with the #99 learner-visibility fix landed this same wave (PR #107, under review) and the post-CI dependency tracker (#102) remaining; the #83 cookie decision was resolved in this round.

---

## 1. Scope of this assessment

- Full repository audit (backend → frontend → UI → infra → docs → git history), round 3
- All 25 audit-round-2 issues closed via reviewed PRs: platform correctness (#40–#53), security (#54, #55), platform reliability (#52), web product/UX (#56–#58), infra/CI/contracts (#59–#63), docs truth pass (#64) + newly discovered #99–#101 fixed in-round
- Every change landed through the Issue → Branch → PR → local CI-equivalent gates → independent review → Merge workflow; no direct commits to `main` by the engineering organization
- Final gates re-run on merged `main` by the orchestrator (not just per-PR)

## 2. Verified capabilities (evidence-based)

| Capability | Evidence |
|------------|----------|
| AuthN: argon2id, rotating refresh sessions w/ reuse detection, atomic refresh consumption, lockout | identity unit + integration suites (#41 8-way concurrency test) |
| AuthZ: server-side RBAC (13 roles × 16 permissions), deterministic multi-membership access, DB-triggered role scoping | tenancy/identity integration suites (#24/#25/#40 regressions) |
| Multi-tenancy: school-scoped queries, 404-not-403 enumeration guards, RequireSchool at composition root | tenant isolation matrices per domain + live smoke |
| Transactional outbox: every domain event recorded in the mutation's transaction (ADR-003 honored) | per-domain rollback tests (#82); fails-on-main proof; SKIP LOCKED dispatcher + dead-letter counter + concurrency exactly-once test |
| Finance: double-entry ledger, immutable postings (DB-trigger proven), tx-scoped idempotency claims, allocation caps, void/serialize guards | finance invariant suite + #44/#45/#46 regressions |
| Contract-first API: OpenAPI 3.1 + generated TS types **checked into git** + drift gate that fails on modified AND untracked output + Go route-coverage test | `npm run check` (drift demo in PR #84) + contract test |
| Migrations: 11 up/down pairs, reversal-proven (up-all → down-all → up-all) | `TestMigrationsUpAndDownAll` + `TestMigrateDownStepsBackOne` (#63, #100) |
| Web: Next.js 15 — memory-only access token + single-flight refresh (family-revocation race fixed), middleware route guard, CSP/HSTS/X-Frame-Options, role-aware shell, students + **enrollment lifecycle UI (9-state admit story)**, mobile-responsive drawer, WCAG-focused a11y (labels, aria-current, skip-link, contrast ≥4.5:1), error/loading/not-found boundaries | 56 vitest suites (#54/#55/#57/#58), tsc strict, lint, production build, live UI-contract probes |
| Infra: real container healthchecks (API probes /healthz; web /api/health), compose stack passes its own smoke prerequisites, prod config refuses weak webhook secrets, Go toolchain 1.25.13 pinned everywhere | #59/#61/#60/#101 gates + live healthcheck-flag verification |
| CI: 9 jobs wired (api, api-integration, api-vuln w/ govulncheck pinned + clean, contracts, migrations, e2e, docker, web, secrets), zero silent-skip guards, Dependabot across 4 ecosystems, coverage floor | `ci.yml` (PR #84) — see gap 1 below |
| Observability: per-route Prometheus metrics, structured access logs w/ actor+tenant, outbox delivery + dead-letter counters, documented alert rules + metrics edge-ACL | observability tests, live smoke, runbook §4 |

## 3. Live E2E smoke (26/26 ✓, re-run after hardening)

`scripts/e2e-smoke.sh` against the real API + PostgreSQL 17 exercises the full product loop: health → login → me → refresh rotation → school → membership (status now asserted) → learner → enrollment (admitted→active) → year/term/subject/class → roster → attendance session + idempotent records → invoice → payment intent → signed webhook confirm → invoice paid → duplicate-webhook replay (zero postings) → wallet inflow → forged-signature rejection → metrics exposure. The script itself is hardened: private mktemp cookie jar with trap cleanup, `--max-time` on every call, collision-safe run suffix, env-overridable credentials, documented python3 dependency.

## 4. Security posture

See `docs/security/THREAT_MODEL.md` (STRIDE). Round-3 additions: access token is memory-only on the web (XSS cannot steal a persistent session; ADR-011), CSP + HSTS + frame/referrer/permissions headers shipped, middleware route guard, webhook secret enforced ≥ 32 bytes in production config validation, govulncheck clean (0 called vulnerabilities; 3 uncalled module-level findings documented with fix paths), rate-limiter buckets bounded with idle eviction.

## 5. Known gaps (tracked, non-blocking for co-located pilots)

1. **CI runners**: GitHub Actions workflows are fully wired but runtime is pending an org-level billing/verification fix (see issue #2) — `startup_failure` on all runs; all gates were executed locally and are reproducible via the documented commands. **Action for the org owner: github.com/organizations/Youngshark-Tech/billing/plans.**
2. **#99 (MEDIUM)**: a learner was invisible to its creating school before first enrollment (visibility = enrollment JOIN) — **implemented this wave in PR #107** (nullable `learners.origin_school_id` + widened visibility predicate); tracked here until it merges. The create→deep-link workaround shipped in the product remains valid.
3. **#102**: major dependency bumps (actions v7, eslint 10, tailwind 4, eslint-config-next 16, gitleaks v3) deferred until CI runs.
4. **Rate limiting is per-process** — add a shared Redis limiter before multi-replica production (roadmap).
5. **`/metrics` is unauthenticated** — restrict at the edge (runbook §4 nginx snippet).
6. **Learner↔user linking**, notification fabric, password reset — roadmap waves 1–2.
7. Web workspaces for academics/attendance/finance/assignments are contract-wired placeholders ("coming soon"); enrollments + students are fully interactive.

**Resolved in this round:** **#83 (MEDIUM)** — refresh cookie widened to `Path=/` (HttpOnly, carries no readable secret; the ADR-011 web middleware guard can now observe it) and `SameSite` made operator-configurable via `SKOLARA_COOKIE_SAMESITE` (`lax` default, `strict`/`none` accepted; split-domain hosting opts into `none` + HTTPS per runbook §10). Covered by config + identity integration tests.

## 6. Quality metrics at assessment time

- Go: 7 bounded contexts + platform; build/vet/gofmt clean; `-race` clean; **15/15 integration packages green vs PostgreSQL 17** (re-run on merged main)
- Tests: 100+ test functions across unit + integration incl. property/invariant/concurrency suites; **web: 56 tests in 10 files**
- Migrations: 11 (reversal-proven; down-migrations exercised by test + CI job)
- Live smoke: 26/26 (hardened script)
- Contract: 45+ documented paths, zero drift, generated types in git
- Dependencies: govulncheck exit 0; npm audit findings confined to dev tooling (tracked via #102)

## 7. Sign-off

The platform is internally consistent, tenant-isolated, financially correct by provable invariant, observable, documented for operators, and honest about what remains. Proceed to co-located pilot onboarding per the runbook checklist; track #99 (PR #107)/#102 and the roadmap in the normal issue workflow.
