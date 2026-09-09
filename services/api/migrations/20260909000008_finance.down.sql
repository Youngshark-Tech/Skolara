-- Reverse of 20260909000008_finance.up.sql (triggers/functions drop with
-- their tables; the function must be dropped explicitly).

DROP TABLE IF EXISTS payment_allocations;
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS invoice_lines;
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS fee_structures;
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS journal_lines;
DROP TABLE IF EXISTS journal_entries;
DROP TABLE IF EXISTS ledger_accounts;
