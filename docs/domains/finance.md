# Finance Domain

**Owner:** `services/api/internal/finance` · **Migrations:** `20260909000008` (tables), `20260909000009` (integrity triggers)

Implements ADR-004 (ledger-first, immutable, idempotent) and ADR-005 (institution wallet as ledger abstraction). This is the highest-stakes domain — the invariants below are acceptance-critical (spec §28–30, §59).

## Ledger

- **Double-entry**: every operation posts a `journal_entry` with ≥ 2 `journal_lines`; exactly one of debit/credit per line, both non-negative, **integer minor units only**.
- **Balanced twice**: application validation AND a deferrable DB constraint trigger re-verify `SUM(debits) = SUM(credits)` at commit.
- **Immutable**: DB triggers reject `UPDATE`/`DELETE` on entries and lines. Corrections are new entries with `source='correction'`, `source_ref` = the original entry.
- **Balances are derived**: `SUM(debits) − SUM(credits)` per account — never stored.
- Chart of accounts seeds lazily per school (fees_receivable, bank, income/expense, tutor_payable + 7 wallet-purpose ASSET accounts).

## Idempotency

The platform `idempotency_keys` registry (000001), scope `finance`:
- Journal entries accept caller-supplied keys — replay returns the original entry, zero new postings.
- Webhook processing keys on the provider **event id** — duplicates return the stored result with zero new postings.

## Invoicing & payments

- Fee structures price charges (optional class/year/term scoping).
- Invoice totals are the SUM of lines (never stored); payment status is `open → partially_paid → paid` (allocations) with `void` for unallocated invoices.
- Payments are **intents** until the provider webhook confirms them server-side (§30): HMAC-SHA256 signature over the raw body (constant-time compare) + provider-ref uniqueness (reconciliation groundwork).
- Confirmation is ONE transaction: CAS `pending → confirmed`, ledger posting (debit `wallet_main`, credit `fees_receivable`), allocation capped at the invoice balance (overpayment stays on the wallet), invoice status update, idempotency key, and `finance.PaymentConfirmed.v1` + `finance.ReceiptIssued.v1` events.

## Wallet (ADR-005)

Logical purpose accounts (`main, fees, payroll, transport, meals, activities, reserve`) are plain ASSET ledger accounts; the wallet view is a derived query. Skolara records and reconciles — it never holds funds.

## Invariant tests (acceptance-critical)

`internal/finance/finance_integration_test.go` proves: randomized posting sequences keep global debits=credits; DB triggers block mutation; duplicate/late-duplicate webhooks produce exactly one posting; the full invoice→payment→webhook→posting→allocation→receipt flow; overpay caps; tenant isolation.

## Non-goals (this phase)

Multi-currency, refunds UI (compensating entries documented), bank statement import, tutor settlement.
