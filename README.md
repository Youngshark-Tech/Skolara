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
├── apps/web/            # Next.js web application (React, TypeScript, Tailwind, shadcn-style UI)
├── services/api/        # Go modular monolith — domain-bounded backend API
├── packages/contracts/  # OpenAPI contracts & generated typed clients
├── infrastructure/      # Docker, Compose, deployment
├── docs/                # Architecture, ADRs, domain docs, runbooks, master spec
└── .github/workflows/   # CI pipelines
```

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
