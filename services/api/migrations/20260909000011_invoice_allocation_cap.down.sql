-- 20260909000011_invoice_allocation_cap.down.sql
DROP TRIGGER IF EXISTS invoice_allocation_cap ON payment_allocations;
DROP FUNCTION IF EXISTS check_invoice_allocation_cap();
