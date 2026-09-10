-- 20260909000011_invoice_allocation_cap.up.sql
-- Issue #44: DB backstop guaranteeing per-invoice allocations can never
-- exceed the invoice total. The service layer caps allocations via
-- SELECT ... FOR UPDATE inside the confirmation transaction; this deferrable
-- constraint trigger is the last line of defense (mirrors
-- journal_entry_balanced in 20260909000009).
CREATE OR REPLACE FUNCTION check_invoice_allocation_cap() RETURNS trigger AS $$
DECLARE
    over_count int;
BEGIN
    SELECT 1 INTO over_count
    FROM invoices i
    WHERE i.id = NEW.invoice_id
      AND COALESCE((SELECT SUM(a.amount_minor) FROM payment_allocations a WHERE a.invoice_id = i.id), 0)
        > COALESCE((SELECT SUM(l.amount_minor) FROM invoice_lines l WHERE l.invoice_id = i.id), 0);
    IF FOUND THEN
        RAISE EXCEPTION 'invoice % over-allocated: payment allocations exceed invoice total', NEW.invoice_id;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER invoice_allocation_cap
    AFTER INSERT OR UPDATE ON payment_allocations
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_invoice_allocation_cap();
