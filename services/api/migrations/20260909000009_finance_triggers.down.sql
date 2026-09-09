DROP TRIGGER IF EXISTS journal_entry_balanced ON journal_lines;
DROP TRIGGER IF EXISTS journal_lines_immutable ON journal_lines;
DROP TRIGGER IF EXISTS journal_entries_immutable ON journal_entries;
DROP FUNCTION IF EXISTS finance_assert_entry_balanced();
DROP FUNCTION IF EXISTS finance_reject_mutation();
