package finance

import (
	"context"
	"errors"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

type pgRepo struct {
	pool *postgres.Pool
}

// NewRepo builds the finance repository.
func NewRepo(pool *postgres.Pool) Repo { return &pgRepo{pool: pool} }

// --- ledger accounts --------------------------------------------------------

const accountCols = `id, school_id, code, name, type, purpose, currency, created_at`

func scanAccount(row pgx.Row) (*LedgerAccount, error) {
	var a LedgerAccount
	err := row.Scan(&a.ID, &a.SchoolID, &a.Code, &a.Name, &a.Type, &a.Purpose, &a.Currency, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

func (r *pgRepo) CreateAccount(ctx context.Context, schoolID string, a *LedgerAccount) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO ledger_accounts (id, school_id, code, name, type, purpose, currency)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+accountCols,
		a.ID, schoolID, a.Code, a.Name, string(a.Type), a.Purpose, a.Currency).
		Scan(&a.ID, &a.SchoolID, &a.Code, &a.Name, &a.Type, &a.Purpose, &a.Currency, &a.CreatedAt)
}

func (r *pgRepo) AccountByID(ctx context.Context, schoolID, id string) (*LedgerAccount, error) {
	return scanAccount(r.pool.QueryRow(ctx,
		`SELECT `+accountCols+` FROM ledger_accounts WHERE id = $1 AND school_id = $2`, id, schoolID))
}

func (r *pgRepo) AccountByCode(ctx context.Context, schoolID, code string) (*LedgerAccount, error) {
	return scanAccount(r.pool.QueryRow(ctx,
		`SELECT `+accountCols+` FROM ledger_accounts WHERE school_id = $1 AND code = $2`, schoolID, code))
}

func (r *pgRepo) ListAccounts(ctx context.Context, schoolID string) ([]*LedgerAccount, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+accountCols+` FROM ledger_accounts WHERE school_id = $1 ORDER BY code`, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*LedgerAccount{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- journal ----------------------------------------------------------------

// InsertEntry writes the entry and all its lines inside ONE transaction. The
// deferred constraint trigger re-verifies debits == credits at commit.
func (r *pgRepo) InsertEntry(ctx context.Context, e *JournalEntry) error {
	return r.pool.WithinTx(ctx, func(tx postgres.Querier) error {
		return r.InsertEntryTx(ctx, tx, e)
	})
}

// InsertEntryTx writes entry + lines on the caller's transaction.
func (r *pgRepo) InsertEntryTx(ctx context.Context, tx postgres.Querier, e *JournalEntry) error {
	{
		if err := tx.QueryRow(ctx,
			`INSERT INTO journal_entries (id, school_id, entry_date, description, source, source_ref, actor_id, correlation_id)
			 VALUES ($1,$2,$3::date,$4,$5,$6,NULLIF($7,'')::uuid,$8)
			 RETURNING created_at`,
			e.ID, e.SchoolID, e.EntryDate, e.Description, e.Source, e.SourceRef, e.ActorID, e.CorrelationID).
			Scan(&e.CreatedAt); err != nil {
			return err
		}
		for i := range e.Lines {
			line := &e.Lines[i]
			if err := tx.QueryRow(ctx,
				`INSERT INTO journal_lines (entry_id, school_id, account_id, debit_minor, credit_minor)
				 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
				e.ID, e.SchoolID, line.AccountID, line.DebitMinor, line.CreditMinor).
				Scan(&line.ID); err != nil {
				return err
			}
			line.EntryID = e.ID
		}
		return nil
	}
}

func (r *pgRepo) EntryByID(ctx context.Context, schoolID, id string) (*JournalEntry, error) {
	e := &JournalEntry{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, school_id, entry_date::text, description, source, source_ref, COALESCE(actor_id::text,''), correlation_id, created_at
		 FROM journal_entries WHERE id = $1 AND school_id = $2`, id, schoolID).
		Scan(&e.ID, &e.SchoolID, &e.EntryDate, &e.Description, &e.Source, &e.SourceRef, &e.ActorID, &e.CorrelationID, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, entry_id, account_id, debit_minor, credit_minor
		 FROM journal_lines WHERE entry_id = $1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	e.Lines = []JournalLine{}
	for rows.Next() {
		var l JournalLine
		if err := rows.Scan(&l.ID, &l.EntryID, &l.AccountID, &l.DebitMinor, &l.CreditMinor); err != nil {
			return nil, err
		}
		e.Lines = append(e.Lines, l)
	}
	return e, rows.Err()
}

// --- idempotency ------------------------------------------------------------

// Idempotency keys are scoped per operation AND per tenant (issue #45):
// journal postings and webhook deliveries never share a namespace, and two
// schools cannot collide or read each other's stored responses. scope format:
// "finance:entry:<schoolID>" and "finance:webhook:<schoolID>".

// idempotencyResult reads a stored response within (scope, key).
func (r *pgRepo) idempotencyResult(ctx context.Context, q postgres.Querier, scope, key string) (*string, error) {
	var result *string
	err := q.QueryRow(ctx,
		`SELECT response::text FROM idempotency_keys WHERE scope = $1 AND key = $2`,
		scope, key).Scan(&result)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *pgRepo) IdempotencyResult(ctx context.Context, scope, key string) (*string, error) {
	return r.idempotencyResult(ctx, r.pool, scope, key)
}

// ClaimIdempotencyKey atomically claims (scope, key): INSERT first; on
// conflict it WAITS for the concurrent holder to finish and returns that
// holder's stored response. The boolean return reports whether THIS caller
// won the claim (reserved placeholder still in place) and must persist its
// result via StoreIdempotencyKey in the SAME transaction (issue #45: the old
// check-then-act split let concurrent same-key requests double-post and a
// crash between entry commit and key store duplicate the posting).
// Ownership is compared as JSONB equality inside SQL — JSONB normalizes
// whitespace, so string comparison of the placeholder is unsafe.
const claimReservedResponse = `{"skolara:claimed":true}`

func (r *pgRepo) ClaimIdempotencyKey(ctx context.Context, tx postgres.Querier, scope, key, schoolID string) (bool, *string, error) {
	var owned bool
	var resp *string
	err := tx.QueryRow(ctx,
		`INSERT INTO idempotency_keys (scope, key, school_id, response) VALUES ($1,$2,$3,$4::jsonb)
		 ON CONFLICT (scope, key) DO UPDATE SET response = idempotency_keys.response
		 RETURNING response IS NOT DISTINCT FROM $4::jsonb, response::text`,
		scope, key, schoolID, claimReservedResponse).Scan(&owned, &resp)
	if err != nil {
		return false, nil, err
	}
	return owned, resp, nil
}

// StoreIdempotencyKey records the result for a key claimed earlier in this
// transaction (platform registry table 000001, school-scoped).
func (r *pgRepo) StoreIdempotencyKey(ctx context.Context, tx postgres.Querier, scope, key, schoolID string, result []byte) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO idempotency_keys (scope, key, school_id, response) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (scope, key) DO UPDATE SET response = EXCLUDED.response, school_id = EXCLUDED.school_id`,
		scope, key, schoolID, result)
	return err
}

// --- fee structures ---------------------------------------------------------

func (r *pgRepo) CreateFeeStructure(ctx context.Context, schoolID string, fs *FeeStructure) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO fee_structures (id, school_id, name, class_group_id, academic_year_id, term_id, amount_minor, currency)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id, school_id, name, class_group_id, academic_year_id, term_id, amount_minor, currency, created_at`,
		fs.ID, schoolID, fs.Name, fs.ClassGroupID, fs.AcademicYearID, fs.TermID, fs.AmountMinor, fs.Currency).
		Scan(&fs.ID, &fs.SchoolID, &fs.Name, &fs.ClassGroupID, &fs.AcademicYearID, &fs.TermID, &fs.AmountMinor, &fs.Currency, &fs.CreatedAt)
}

func (r *pgRepo) ListFeeStructures(ctx context.Context, schoolID string) ([]*FeeStructure, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, school_id, name, class_group_id, academic_year_id, term_id, amount_minor, currency, created_at
		 FROM fee_structures WHERE school_id = $1 ORDER BY created_at DESC`, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*FeeStructure{}
	for rows.Next() {
		fs := &FeeStructure{}
		if err := rows.Scan(&fs.ID, &fs.SchoolID, &fs.Name, &fs.ClassGroupID, &fs.AcademicYearID, &fs.TermID, &fs.AmountMinor, &fs.Currency, &fs.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, fs)
	}
	return out, rows.Err()
}

// --- invoices ---------------------------------------------------------------

func (r *pgRepo) InsertInvoice(ctx context.Context, inv *Invoice) error {
	return r.pool.WithinTx(ctx, func(tx postgres.Querier) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO invoices (id, school_id, learner_id, fee_structure_id, term_id, due_date, currency)
			 VALUES ($1,$2,$3,$4,$5,$6::date,$7)
			 RETURNING created_at, status`,
			inv.ID, inv.SchoolID, inv.LearnerID, inv.FeeStructureID, inv.TermID, inv.DueDate, inv.Currency).
			Scan(&inv.CreatedAt, &inv.Status); err != nil {
			return err
		}
		for i := range inv.Lines {
			line := &inv.Lines[i]
			if err := tx.QueryRow(ctx,
				`INSERT INTO invoice_lines (invoice_id, description, amount_minor)
				 VALUES ($1,$2,$3) RETURNING id`,
				inv.ID, line.Description, line.AmountMinor).Scan(&line.ID); err != nil {
				return err
			}
			line.InvoiceID = inv.ID
		}
		// Derived total comes back with the invoice on read; fill it here too.
		return tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(amount_minor),0) FROM invoice_lines WHERE invoice_id = $1`, inv.ID).
			Scan(&inv.TotalMinor)
	})
}

const invoiceDerived = `i.id, i.school_id, i.learner_id, i.fee_structure_id, i.term_id, i.due_date::text,
		i.currency, i.status, i.created_at,
		COALESCE((SELECT SUM(l.amount_minor) FROM invoice_lines l WHERE l.invoice_id = i.id), 0) AS total_minor,
		COALESCE((SELECT SUM(a.amount_minor) FROM payment_allocations a WHERE a.invoice_id = i.id), 0) AS paid_minor`

func scanInvoice(row pgx.Row) (*Invoice, error) {
	var inv Invoice
	err := row.Scan(&inv.ID, &inv.SchoolID, &inv.LearnerID, &inv.FeeStructureID, &inv.TermID,
		&inv.DueDate, &inv.Currency, &inv.Status, &inv.CreatedAt, &inv.TotalMinor, &inv.PaidMinor)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	inv.BalanceMinor = inv.TotalMinor - inv.PaidMinor
	return &inv, nil
}

func (r *pgRepo) InvoiceByID(ctx context.Context, schoolID, id string) (*Invoice, error) {
	return scanInvoice(r.pool.QueryRow(ctx,
		`SELECT `+invoiceDerived+` FROM invoices i WHERE i.id = $1 AND i.school_id = $2`, id, schoolID))
}

// InvoiceByIDForUpdate is the transaction-scoped read used by ConfirmWebhook:
// the row lock serializes concurrent confirmations on the same invoice so the
// allocation cap is computed from committed state (issue #44).
func (r *pgRepo) InvoiceByIDForUpdate(ctx context.Context, q postgres.Querier, schoolID, id string) (*Invoice, error) {
	return scanInvoice(q.QueryRow(ctx,
		`SELECT `+invoiceDerived+` FROM invoices i WHERE i.id = $1 AND i.school_id = $2 FOR UPDATE`, id, schoolID))
}

func (r *pgRepo) ListInvoices(ctx context.Context, schoolID string, status *InvoiceStatus, learnerID string, limit, offset int) ([]*Invoice, int, error) {
	var st any
	if status != nil {
		st = string(*status)
	}
	var ln any
	if learnerID != "" {
		ln = learnerID
	}
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM invoices i
		 WHERE i.school_id = $1 AND ($2::text IS NULL OR i.status = $2::text) AND ($3::uuid IS NULL OR i.learner_id = $3)`,
		schoolID, st, ln).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+invoiceDerived+` FROM invoices i
		 WHERE i.school_id = $1 AND ($2::text IS NULL OR i.status = $2::text) AND ($3::uuid IS NULL OR i.learner_id = $3)
		 ORDER BY i.created_at DESC
		 LIMIT $4 OFFSET $5`,
		schoolID, st, ln, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Invoice{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, inv)
	}
	return out, total, rows.Err()
}

func (r *pgRepo) SetInvoiceStatus(ctx context.Context, q postgres.Querier, schoolID, id string, status InvoiceStatus) error {
	tag, err := q.Exec(ctx,
		`UPDATE invoices SET status = $3 WHERE id = $1 AND school_id = $2`, id, schoolID, string(status))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- payments ---------------------------------------------------------------

func (r *pgRepo) InsertPayment(ctx context.Context, p *Payment) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO payments (id, school_id, invoice_id, payer_ref, amount_minor, currency, provider, provider_ref)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id, school_id, invoice_id, payer_ref, amount_minor, currency, provider, provider_ref, status, created_at, confirmed_at`,
		p.ID, p.SchoolID, p.InvoiceID, p.PayerRef, p.AmountMinor, p.Currency, p.Provider, p.ProviderRef).
		Scan(&p.ID, &p.SchoolID, &p.InvoiceID, &p.PayerRef, &p.AmountMinor, &p.Currency, &p.Provider, &p.ProviderRef, &p.Status, &p.CreatedAt, &p.ConfirmedAt)
}

const paymentCols = `id, school_id, invoice_id, payer_ref, amount_minor, currency, provider, provider_ref, status, created_at, confirmed_at`

func scanPayment(row pgx.Row) (*Payment, error) {
	var p Payment
	err := row.Scan(&p.ID, &p.SchoolID, &p.InvoiceID, &p.PayerRef, &p.AmountMinor, &p.Currency,
		&p.Provider, &p.ProviderRef, &p.Status, &p.CreatedAt, &p.ConfirmedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (r *pgRepo) PaymentByID(ctx context.Context, schoolID, id string) (*Payment, error) {
	return scanPayment(r.pool.QueryRow(ctx,
		`SELECT `+paymentCols+` FROM payments WHERE id = $1 AND school_id = $2`, id, schoolID))
}

func (r *pgRepo) PaymentByProviderRef(ctx context.Context, schoolID, provider, providerRef string) (*Payment, error) {
	return scanPayment(r.pool.QueryRow(ctx,
		`SELECT `+paymentCols+` FROM payments WHERE school_id = $1 AND provider = $2 AND provider_ref = $3`,
		schoolID, provider, providerRef))
}

// MarkPaymentConfirmed CAS: pending -> confirmed. Returns false when the
// payment was not in pending (concurrent/replayed confirmation).
func (r *pgRepo) MarkPaymentConfirmed(ctx context.Context, tx postgres.Querier, schoolID, id string) (bool, error) {
	tag, err := tx.Exec(ctx,
		`UPDATE payments SET status = 'confirmed', confirmed_at = now()
		 WHERE id = $1 AND school_id = $2 AND status = 'pending'`,
		id, schoolID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *pgRepo) MarkPaymentFailed(ctx context.Context, schoolID, id string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE payments SET status = 'failed' WHERE id = $1 AND school_id = $2 AND status = 'pending'`,
		id, schoolID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *pgRepo) InsertAllocation(ctx context.Context, tx postgres.Querier, paymentID, invoiceID string, amountMinor int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO payment_allocations (payment_id, invoice_id, amount_minor) VALUES ($1,$2,$3)
		 ON CONFLICT (payment_id, invoice_id) DO NOTHING`,
		paymentID, invoiceID, amountMinor)
	return err
}

func (r *pgRepo) AllocatedMinor(ctx context.Context, tx postgres.Querier, invoiceID string) (int64, error) {
	var n int64
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_minor),0) FROM payment_allocations WHERE invoice_id = $1`, invoiceID).Scan(&n)
	return n, err
}

// --- wallet (derived) -------------------------------------------------------

func (r *pgRepo) WalletBalances(ctx context.Context, schoolID string) ([]*WalletBalance, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT a.purpose, a.id, a.currency,
			COALESCE(SUM(l.debit_minor), 0) - COALESCE(SUM(l.credit_minor), 0) AS balance_minor
		 FROM ledger_accounts a
		 LEFT JOIN journal_lines l ON l.account_id = a.id
		 WHERE a.school_id = $1 AND a.purpose <> ''
		 GROUP BY a.purpose, a.id, a.currency
		 ORDER BY a.purpose`, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*WalletBalance{}
	for rows.Next() {
		w := &WalletBalance{}
		if err := rows.Scan(&w.Purpose, &w.AccountID, &w.Currency, &w.BalanceMinor); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
