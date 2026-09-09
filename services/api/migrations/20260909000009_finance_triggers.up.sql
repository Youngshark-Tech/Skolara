-- Finance integrity backstops (ADR-004): immutability + balance triggers.
-- Kept in a dedicated migration so the ledger tables migrate atomically and
-- the trigger DDL is isolated and reviewable.

CREATE OR REPLACE FUNCTION finance_reject_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ledger records are immutable: % on % is blocked (use a compensating entry)', TG_OP, TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER journal_entries_immutable
    BEFORE UPDATE OR DELETE ON journal_entries
    FOR EACH STATEMENT EXECUTE FUNCTION finance_reject_mutation();

CREATE TRIGGER journal_lines_immutable
    BEFORE UPDATE OR DELETE ON journal_lines
    FOR EACH STATEMENT EXECUTE FUNCTION finance_reject_mutation();

CREATE OR REPLACE FUNCTION finance_assert_entry_balanced() RETURNS trigger AS $$
DECLARE
    d BIGINT;
    c BIGINT;
BEGIN
    SELECT COALESCE(SUM(debit_minor), 0), COALESCE(SUM(credit_minor), 0)
      INTO d, c
      FROM journal_lines
     WHERE entry_id = NEW.entry_id;
    IF d <> c THEN
        RAISE EXCEPTION 'journal entry % unbalanced: debits=% credits=%', NEW.entry_id, d, c;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER journal_entry_balanced
    AFTER INSERT ON journal_lines
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION finance_assert_entry_balanced();
