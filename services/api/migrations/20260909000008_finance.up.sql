-- Finance bounded context (ADR-004/ADR-005): double-entry ledger, immutable
-- postings, idempotency keys, fee structures, invoices, payments, wallet
-- purpose accounts. Amounts are INTEGER minor units — never floats.
-- Balances are DERIVED (never stored): SUM(debits) - SUM(credits) per account.

-- ---------------------------------------------------------------------------
-- Ledger core
-- ---------------------------------------------------------------------------

CREATE TABLE ledger_accounts (
    id         UUID PRIMARY KEY,
    school_id  UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    code       TEXT NOT NULL,
    name       TEXT NOT NULL,
    type       TEXT NOT NULL
               CHECK (type IN ('ASSET','LIABILITY','EQUITY','REVENUE','EXPENSE')),
    purpose    TEXT NOT NULL DEFAULT '',   -- wallet purpose: main|fees|payroll|transport|meals|activities|reserve
    currency   TEXT NOT NULL DEFAULT 'KES',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (school_id, code)
);

CREATE TABLE journal_entries (
    id             UUID PRIMARY KEY,
    school_id      UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    entry_date     DATE NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    source         TEXT NOT NULL DEFAULT 'manual'
                   CHECK (source IN ('manual','payment','correction')),
    source_ref     TEXT NOT NULL DEFAULT '',   -- payment id / corrected entry id
    actor_id       UUID,                       -- nullable: system/webhook postings
    correlation_id TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE journal_lines (
    id           BIGSERIAL PRIMARY KEY,
    entry_id     UUID NOT NULL REFERENCES journal_entries(id),
    school_id    UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    account_id   UUID NOT NULL REFERENCES ledger_accounts(id),
    debit_minor  BIGINT NOT NULL DEFAULT 0 CHECK (debit_minor  >= 0),
    credit_minor BIGINT NOT NULL DEFAULT 0 CHECK (credit_minor >= 0),
    -- exactly one side non-zero per line
    CHECK ((debit_minor > 0 AND credit_minor = 0) OR (credit_minor > 0 AND debit_minor = 0))
);
CREATE INDEX journal_lines_entry_idx ON journal_lines (entry_id);
CREATE INDEX journal_lines_account_idx ON journal_lines (account_id);

-- Idempotency registry: REUSED from the platform foundation migration
-- (000001, table idempotency_keys with scope+key). Finance keys use
-- scope 'finance'. Do NOT re-create the table here.

-- ---------------------------------------------------------------------------
-- Fee structures and invoices
-- ---------------------------------------------------------------------------

CREATE TABLE fee_structures (
    id               UUID PRIMARY KEY,
    school_id        UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    class_group_id   UUID REFERENCES class_groups(id) ON DELETE CASCADE,
    academic_year_id UUID REFERENCES academic_years(id) ON DELETE CASCADE,
    term_id          UUID REFERENCES terms(id) ON DELETE CASCADE,
    amount_minor     BIGINT NOT NULL CHECK (amount_minor > 0),
    currency         TEXT NOT NULL DEFAULT 'KES',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX fee_structures_school_idx ON fee_structures (school_id);

-- Invoice totals are the SUM of their lines (never stored mutable fields);
-- payment state (open/partially_paid/paid) is maintained on allocation.
CREATE TABLE invoices (
    id               UUID PRIMARY KEY,
    school_id        UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    learner_id       UUID NOT NULL REFERENCES learners(id) ON DELETE RESTRICT,
    fee_structure_id UUID REFERENCES fee_structures(id),
    term_id          UUID REFERENCES terms(id) ON DELETE SET NULL,
    due_date         DATE NOT NULL,
    currency         TEXT NOT NULL DEFAULT 'KES',
    status           TEXT NOT NULL DEFAULT 'open'
                     CHECK (status IN ('open','partially_paid','paid','void')),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX invoices_school_idx ON invoices (school_id, status);
CREATE INDEX invoices_learner_idx ON invoices (learner_id);

CREATE TABLE invoice_lines (
    id           BIGSERIAL PRIMARY KEY,
    invoice_id   UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    description  TEXT NOT NULL DEFAULT '',
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0)
);

-- ---------------------------------------------------------------------------
-- Payments
-- ---------------------------------------------------------------------------

CREATE TABLE payments (
    id           UUID PRIMARY KEY,
    school_id    UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    invoice_id   UUID REFERENCES invoices(id),
    payer_ref    TEXT NOT NULL DEFAULT '',
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    currency     TEXT NOT NULL DEFAULT 'KES',
    provider     TEXT NOT NULL,
    provider_ref TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','confirmed','failed')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at TIMESTAMPTZ
);
-- Provider transaction identity is unique: reconciliation groundwork.
CREATE UNIQUE INDEX payments_provider_ref_key ON payments (provider, provider_ref);
CREATE INDEX payments_invoice_idx ON payments (invoice_id);

CREATE TABLE payment_allocations (
    payment_id   UUID NOT NULL REFERENCES payments(id) ON DELETE CASCADE,
    invoice_id   UUID NOT NULL REFERENCES invoices(id),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    allocated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (payment_id, invoice_id)
);
CREATE INDEX payment_allocations_invoice_idx ON payment_allocations (invoice_id);
