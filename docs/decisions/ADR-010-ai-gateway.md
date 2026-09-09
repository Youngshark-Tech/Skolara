# ADR-010: AI Gateway — Provider-Agnostic, Recommend-Only

**Status:** Accepted · **Date:** 2026-09-09

## Context

Skolara's intelligence layer (§40, §64) must never let AI own truth, bypass authorization, or fabricate school facts.

## Decision

- A **provider-agnostic AI gateway** (single port, multiple adapters) will serve: NL queries over approved aggregates, summaries, drafting, anomaly detection, forecasting, explanations.
- **Hard rule: intelligence recommends; deterministic domains decide; authorized humans approve.** AI never writes to domain tables, never approves payments, never decides discipline, never modifies the ledger (§64).
- AI answers over school data flow through the **same authorization and tenant-isolation layers** as human users; citations to underlying records are mandatory; no citation → no display.
- Deterministic verification: model output used for decisions must map to existing records (IDs verified) before presentation; unmapped content is labeled as provisional.
- Prompt/response audit records are kept for sensitive operations.

**Status:** groundwork decision recorded; implementation is a tracked roadmap phase (after communication fabric). No AI code ships in the production-readiness wave.

## Consequences

- **Positive:** clear constitutional constraints before any AI feature; domains stay AI-clean.
- **Negative:** no AI value in the first release — accepted deliberately per §64 safety posture.
