# ADR-008: Web — Next.js App Router, Tailwind, shadcn-style UI, TanStack Query

**Status:** Accepted · **Date:** 2026-09-09

## Context

The web experience must be responsive, accessible, keyboard-friendly, fast on low bandwidth, and role/permission-aware (§5, §61), consuming a typed API.

## Decision

- **Next.js (App Router) + TypeScript** in `apps/web`; strict TS config.
- **Tailwind CSS** with design tokens (light/dark), **shadcn-style primitives** (Radix behavior, class-variance-authority styling) vendored in `components/ui` — accessible dialog/menu/tab behavior without a heavy component library lock-in.
- **TanStack Query** for server state (cache, retry, invalidation); plain fetch wrapper with typed error envelope mapping; request IDs injected per call.
- **Auth:** the API issues **httpOnly, Secure, SameSite=Lax** cookie sessions; no tokens in localStorage; middleware guards routes; 403/404 pages for forbidden/missing resources.
- **Role-aware navigation:** nav config maps items → permissions; filtered by session permissions. Server always enforces (ADR-006/007).
- **Generated typed client** from OpenAPI (ADR see #14) once contracts land; interim hand-typed models mirror the spec exactly.

## Consequences

- **Positive:** one app for all roles with permission-aware surfaces; strong typing end-to-end; accessible primitives by default.
- **Negative:** vendored primitives require upkeep; mitigated by keeping the set minimal and behavior-focused.
