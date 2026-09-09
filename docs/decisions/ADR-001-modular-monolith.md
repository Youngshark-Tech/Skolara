# ADR-001: Modular Monolith in Go

**Status:** Accepted · **Date:** 2026-09-09 · **Deciders:** Engineering Organization

## Context

Skolara must support students, guardians, teachers, administrators, academics, workforce, timetabling, communication, discipline, finance, transport, inventory, and AI — initially for one school, eventually for networks of schools. The master specification forbids premature microservices while demanding the ability to extract domains later without a rewrite.

## Decision

Build a **single Go API service** (`services/api`) structured as a **modular monolith** with strictly enforced bounded contexts:

```
services/api/
├── cmd/api/                 # composition root — the ONLY place wires everything
├── internal/
│   ├── platform/            # cross-cutting: config, logging, httpx, middleware,
│   │                        # postgres, events (outbox), observability
│   ├── identity/            # users, auth, RBAC, audit        (bounded context)
│   ├── tenancy/             # groups, schools, campuses        (bounded context)
│   ├── students/            # learners, guardians, enrollments (bounded context)
│   ├── academics/           # years, terms, subjects, classes  (bounded context)
│   ├── attendance/          # sessions, records                (bounded context)
│   ├── assignments/         # assignments, submissions         (bounded context)
│   └── finance/             # ledger, invoices, payments       (bounded context)
```

Layering inside each context: **Transport → Application → Domain**. Domain packages never import HTTP, database, or external-provider packages; they receive ports (interfaces) from the composition root.

## Rules Enforced

- A domain package may import `platform/*` and its own packages — never another domain's internals.
- Cross-domain reads go through explicitly exported service interfaces or events, never direct table joins across contexts.
- The composition root (`cmd/api`) is the only place that constructs concrete types and wires dependencies.

## Consequences

- **Positive:** single deployable, transactional consistency across modules, fast local dev, clear extraction seams (each context can become a service behind its existing interface).
- **Negative:** requires discipline to prevent cross-context imports; mitigated by review checklists and package-boundary tests in CI.
- **Extraction path:** a context graduates by (1) owning its migrations, (2) replacing in-process calls with events, (3) moving to its own deployable — no domain rewrite needed.

## Alternatives Considered

- **Microservices from day one:** rejected — operationally premature, distributed transactions would complicate ledger correctness.
- **Single flat package:** rejected — would create the disconnected-module mess §62 forbids.
