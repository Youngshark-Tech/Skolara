# Skolara

**The Intelligent Operating System for Schools.**

Skolara is an intelligent school operating and financial infrastructure platform. It connects students, parents, teachers, and administrators across academic operations, workforce planning, timetabling, communication, discipline, finance, and school-wide automation — designed as infrastructure for one school, a school group, or a network of schools.

The fundamental product loop:

```
Observe → Understand → Predict → Simulate → Optimize → Communicate → Execute → Settle → Learn
```

## Repository Layout

```
skolara/
├── apps/web/            # Next.js 15 web application (TypeScript, Tailwind)
├── services/api/        # Go modular monolith — identity, tenancy, students, academics,
│                         # attendance, assignments, finance (bounded contexts)
├── packages/contracts/  # OpenAPI 3.1 spec (source of truth) + generated TS types
├── infrastructure/      # Docker, Compose, deployment
├── scripts/             # Live E2E smoke + ops scripts
├── docs/                # ADRs, domain docs, threat model, runbook, QA report, roadmap
└── .github/workflows/   # CI pipelines
```

## Status

Production-ready alpha for pilot onboarding — see the
[QA report](docs/qa/QA_REPORT.md), [runbook](docs/operations/RUNBOOK.md), and
[roadmap](docs/operations/ROADMAP.md).

## Key Principles

1. **Domain ownership** — every piece of data has one authoritative owner
2. **Ledger-first finance** — financial truth comes from immutable double-entry postings
3. **Intelligence never owns truth** — AI recommends; deterministic domains decide; humans approve
4. **Event-driven integration** — domains communicate through contracts and events
5. **Multi-tenancy from day one** — tenant isolation enforced at every layer
6. **Security by default** — authorization enforced server-side
7. **Small coherent PRs** — one issue → one coherent feature → one PR; never commit to `main` directly

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, and [docs/](docs/) for architecture and decision records.

## License

See [LICENSE](LICENSE).
