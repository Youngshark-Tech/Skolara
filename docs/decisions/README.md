# Architecture Decision Records

Skolara records every significant architectural decision as an ADR (master spec §65).

**Index**

| ADR | Title | Status |
|-----|-------|--------|
| [ADR-001](ADR-001-modular-monolith.md) | Modular Monolith in Go | Accepted |
| [ADR-002](ADR-002-postgresql-source-of-truth.md) | PostgreSQL as Transactional Source of Truth | Accepted |
| [ADR-003](ADR-003-event-architecture-outbox.md) | Event Architecture — Transactional Outbox | Accepted |
| [ADR-004](ADR-004-financial-ledger.md) | Ledger-First Finance — Double-Entry, Immutable, Idempotent | Accepted |
| [ADR-005](ADR-005-institution-wallet.md) | Institution Wallet as Ledger Account Abstraction | Accepted |
| [ADR-006](ADR-006-tenant-isolation.md) | Tenant Isolation Model | Accepted |
| [ADR-007](ADR-007-identity-auth-rbac.md) | Identity — Argon2id, JWT + Rotating Refresh, RBAC | Accepted |
| [ADR-008](ADR-008-web-frontend.md) | Web — Next.js, Tailwind, shadcn-style UI, TanStack Query | Accepted |
| [ADR-009](ADR-009-offline-first.md) | Offline-First Strategy | Accepted |
| [ADR-010](ADR-010-ai-gateway.md) | AI Gateway — Provider-Agnostic, Recommend-Only | Accepted |

**Process:** to change architecture, open an issue referencing the ADR, propose a new ADR (superseding the old one), and land it via PR. ADRs are never edited silently.
