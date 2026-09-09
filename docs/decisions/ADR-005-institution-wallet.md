# ADR-005: Institution Wallet as Ledger Account Abstraction

**Status:** Accepted · **Date:** 2026-09-09

## Context

Schools need logical accounts (fees collection, payroll, transport, meals, activities, reserve) and the product surface of an "Institution Wallet" (§29). Operating a regulated money custodian is out of scope today.

## Decision

- The **Institution Wallet is a control abstraction over the ledger**, not a custodial balance holder. Each logical wallet = one or more ledger accounts of type ASSET/LIABILITY with a wallet label.
- Logical wallets map to **external financial accounts** (bank, mobile money, PSP) through a provider-agnostic `PaymentProvider` port; Skolara records and reconciles, it does not hold funds.
- The wallet view (available, reserved, by-purpose balances) is **derived from ledger lines** — consistent with ADR-004.
- Multi-provider by design: providers are registered adapters; no provider logic leaks into the finance domain.

## Consequences

- **Positive:** zero custodial-regulatory exposure initially; wallet features (purpose accounts, sweeps, budgeting) are ledger views; provider swap is an adapter change.
- **Negative:** real-money movement depends on provider integrations (roadmap); wallet UI must be clear that Skolara reflects provider truth rather than replacing it.
