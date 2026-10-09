# Skolara Roadmap

Post-onboarding engineering roadmap. Order reflects dependency and risk.

## Wave 1 — Onboarding hardening (immediate)

- [ ] **CI runners**: re-enable GitHub Actions (org billing/verification hold, #2) — all workflows ARE wired (`ci.yml`, 9 jobs) and locally gate-equivalent; only the runtime is blocked
- [x] Transactional outbox at every call site + bounded in-process rate limiter (#52) — Redis-backed shared limiter below remains for multi-replica
- [ ] Shared Redis rate limiter (multi-replica ready); middleware port exists
- [x] `/metrics` edge ACL + alert rules documented for operators (runbook §4) — proxy deployment action remains
- [ ] Password reset / account recovery (email verification groundwork)
- [x] Web: enrollments workspace live (admit story, #58); students + command center fully interactive — academics/attendance/finance workspaces remain on the established pattern (contract already documents every endpoint)
- [ ] School onboarding wizard (groups → school → members → academics) in the web shell

## Wave 2 — Notification fabric & sync

- [ ] Event consumer framework (outbox → NATS JetStream publisher port is ready)
- [ ] Notifications: absence alerts to guardians (attendance.AbsenceRecorded consumer), invoice/payment receipts
- [ ] Attendance offline sync: applied-mutations ledger + client queue semantics on top of the idempotency groundwork
- [ ] Teacher assignment reassignment workflow (finance of payroll depends on it)

## Wave 3 — Financial expansion

- [ ] Payment provider adapters behind the `PaymentProvider` port (M-Pesa, card PSPs) + reconciliation views
- [ ] Refunds UI via compensating entries (engine exists)
- [ ] Multi-currency per school (currency column already flows end-to-end)
- [ ] Tutor settlement & compensation (ledger accounts exist: `tutor_payable`, `tuition_expense`)
- [ ] Fee templates per class/term auto-invoicing

## Wave 4 — Intelligence (spec §1 product loop)

> The AI capability ladder now lives in [AI-ROADMAP.md](AI-ROADMAP.md)
> (epic #184, children #160-#167) — queued as issues per the wave-4 items below.

- [ ] Analytics over derived ledger + attendance (read models)
- [ ] Timetabling; curriculum strands/outcomes (academics is extensible by design)
- [ ] AI recommendations behind the intelligence-never-owns-truth boundary (ADR-010)

## Deferred / conscious decisions

- Mobile clients (contract + JWT + offline groundwork already prepared)
- Student self-service portals (requires learner↔user identity linking — see threat model follow-up)
- Cursor pagination if collections outgrow offset semantics (contract documents current bounds)
