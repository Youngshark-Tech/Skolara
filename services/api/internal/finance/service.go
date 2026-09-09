package finance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/events"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/google/uuid"
)

// Service implements finance business rules.
type Service struct {
	repo          Repo
	pool          *postgres.Pool
	webhookSecret string
}

func NewService(repo Repo, pool *postgres.Pool, webhookSecret string) *Service {
	return &Service{repo: repo, pool: pool, webhookSecret: webhookSecret}
}

// defaultChart is the per-school chart of accounts seeded lazily before the
// first financial operation (school-creation hook lands via the
// tenancy.SchoolCreated consumer when the event fabric matures).
var defaultChart = []struct {
	code    string
	name    string
	typ     AccountType
	purpose WalletPurpose
}{
	{"fees_receivable", "Fees Receivable", AccountAsset, ""},
	{"bank", "Bank Account", AccountAsset, ""},
	{"tuition_income", "Tuition Income", AccountRevenue, ""},
	{"other_income", "Other Income", AccountRevenue, ""},
	{"tuition_expense", "Tuition Expense", AccountExpense, ""},
	{"tutor_payable", "Tutor Payable", AccountLiability, ""},
	{"wallet_main", "Wallet — Main", AccountAsset, WalletMain},
	{"wallet_fees", "Wallet — Fees", AccountAsset, WalletFees},
	{"wallet_payroll", "Wallet — Payroll", AccountAsset, WalletPayroll},
	{"wallet_transport", "Wallet — Transport", AccountAsset, WalletTransport},
	{"wallet_meals", "Wallet — Meals", AccountAsset, WalletMeals},
	{"wallet_activities", "Wallet — Activities", AccountAsset, WalletActivities},
	{"wallet_reserve", "Wallet — Reserve", AccountAsset, WalletReserve},
}

// EnsureChart seeds the default chart of accounts for a school exactly once
// (idempotent: existing codes are skipped).
func (s *Service) EnsureChart(ctx context.Context, schoolID string) error {
	for _, def := range defaultChart {
		if _, err := s.repo.AccountByCode(ctx, schoolID, def.code); err == nil {
			continue
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		a := &LedgerAccount{
			ID:       uuid.NewString(),
			Code:     def.code,
			Name:     def.name,
			Type:     def.typ,
			Purpose:  string(def.purpose),
			Currency: "KES",
		}
		if err := s.repo.CreateAccount(ctx, schoolID, a); err != nil {
			return err
		}
	}
	return nil
}

// CreateAccount registers an extra account in the school's chart.
func (s *Service) CreateAccount(ctx context.Context, schoolID, code, name string, typ AccountType, purpose string) (*LedgerAccount, error) {
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)
	if code == "" || len(code) > 64 {
		return nil, fmt.Errorf("%w: account code required (<=64 chars)", ErrValidation)
	}
	if name == "" || len(name) > 128 {
		return nil, fmt.Errorf("%w: account name required (<=128 chars)", ErrValidation)
	}
	switch typ {
	case AccountAsset, AccountLiability, AccountEquity, AccountRevenue, AccountExpense:
	default:
		return nil, fmt.Errorf("%w: unknown account type %q", ErrValidation, typ)
	}
	if purpose != "" && !ValidWalletPurpose(WalletPurpose(purpose)) {
		return nil, fmt.Errorf("%w: unknown wallet purpose %q", ErrValidation, purpose)
	}
	a := &LedgerAccount{ID: uuid.NewString(), Code: code, Name: name, Type: typ, Purpose: purpose, Currency: "KES"}
	if err := s.repo.CreateAccount(ctx, schoolID, a); err != nil {
		return nil, err
	}
	return a, nil
}

// Accounts lists the school's chart of accounts.
func (s *Service) Accounts(ctx context.Context, schoolID string) ([]*LedgerAccount, error) {
	return s.repo.ListAccounts(ctx, schoolID)
}

// PostEntry validates and posts a balanced journal entry atomically. When
// idempotencyKey is non-empty, a replay returns the original entry id with no
// new posting (ADR-004 §3). correctionOf links a compensating entry.
func (s *Service) PostEntry(ctx context.Context, schoolID, actorID, description, entryDate, idempotencyKey, correctionOf string, lines []LineInput) (*JournalEntry, error) {
	if len(lines) < 2 {
		return nil, fmt.Errorf("%w: an entry needs at least two lines", ErrValidation)
	}
	if entryDate == "" {
		entryDate = time.Now().UTC().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", entryDate); err != nil {
		return nil, fmt.Errorf("%w: entryDate must be YYYY-MM-DD", ErrValidation)
	}
	var debits, credits int64
	for _, l := range lines {
		if _, err := uuid.Parse(l.AccountID); err != nil {
			return nil, fmt.Errorf("%w: invalid accountId", ErrValidation)
		}
		if l.DebitMinor < 0 || l.CreditMinor < 0 {
			return nil, fmt.Errorf("%w: negative amounts are not allowed", ErrValidation)
		}
		if (l.DebitMinor > 0 && l.CreditMinor > 0) || (l.DebitMinor == 0 && l.CreditMinor == 0) {
			return nil, fmt.Errorf("%w: each line needs exactly one of debit/credit", ErrValidation)
		}
		debits += l.DebitMinor
		credits += l.CreditMinor
	}
	if debits != credits {
		return nil, fmt.Errorf("%w: debits=%d credits=%d", ErrUnbalanced, debits, credits)
	}
	if debits <= 0 {
		return nil, fmt.Errorf("%w: entry total must be positive", ErrValidation)
	}

	// Verify all accounts belong to the school before posting.
	for _, l := range lines {
		if _, err := s.repo.AccountByID(ctx, schoolID, l.AccountID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, fmt.Errorf("%w: account %s not found in this school", ErrNotFound, l.AccountID)
			}
			return nil, err
		}
	}

	source := "manual"
	sourceRef := ""
	if correctionOf != "" {
		if _, err := s.repo.EntryByID(ctx, schoolID, correctionOf); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, fmt.Errorf("%w: correction target not found in this school", ErrNotFound)
			}
			return nil, err
		}
		source = "correction"
		sourceRef = correctionOf
	}

	// Idempotent replay: return the original result without posting.
	if idempotencyKey != "" {
		stored, err := s.repo.IdempotencyResult(ctx, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if stored != nil {
			var res struct {
				EntryID string `json:"entryId"`
			}
			if err := json.Unmarshal([]byte(*stored), &res); err != nil {
				return nil, fmt.Errorf("%w: stored idempotency result unreadable", ErrValidation)
			}
			e, err := s.repo.EntryByID(ctx, schoolID, res.EntryID)
			if err != nil {
				return nil, err
			}
			return e, nil
		}
	}

	e := &JournalEntry{
		ID:          uuid.NewString(),
		SchoolID:    schoolID,
		EntryDate:   entryDate,
		Description: strings.TrimSpace(description),
		Source:      source,
		SourceRef:   sourceRef,
		ActorID:     actorID,
	}
	for _, l := range lines {
		e.Lines = append(e.Lines, JournalLine{AccountID: l.AccountID, DebitMinor: l.DebitMinor, CreditMinor: l.CreditMinor})
	}
	if err := s.repo.InsertEntry(ctx, e); err != nil {
		return nil, err
	}
	if idempotencyKey != "" {
		if err := s.repo.StoreIdempotencyKey(ctx, s.pool, idempotencyKey, []byte(`{"entryId":"`+e.ID+`"}`)); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// Entry resolves one posting with its lines (school-scoped).
func (s *Service) Entry(ctx context.Context, schoolID, id string) (*JournalEntry, error) {
	return s.repo.EntryByID(ctx, schoolID, id)
}

// --- fee structures ---------------------------------------------------------

// CreateFeeStructure validates and creates a fee definition.
func (s *Service) CreateFeeStructure(ctx context.Context, schoolID, name string, classGroupID, academicYearID, termID *string, amountMinor int64) (*FeeStructure, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 {
		return nil, fmt.Errorf("%w: fee name required (<=128 chars)", ErrValidation)
	}
	if amountMinor <= 0 {
		return nil, fmt.Errorf("%w: amountMinor must be positive", ErrValidation)
	}
	fs := &FeeStructure{
		ID:             uuid.NewString(),
		Name:           name,
		ClassGroupID:   classGroupID,
		AcademicYearID: academicYearID,
		TermID:         termID,
		AmountMinor:    amountMinor,
		Currency:       "KES",
	}
	if err := s.repo.CreateFeeStructure(ctx, schoolID, fs); err != nil {
		return nil, err
	}
	return fs, nil
}

// FeeStructures lists the school's fee definitions.
func (s *Service) FeeStructures(ctx context.Context, schoolID string) ([]*FeeStructure, error) {
	return s.repo.ListFeeStructures(ctx, schoolID)
}

// --- invoices ---------------------------------------------------------------

// CreateInvoiceInput carries invoice creation fields.
type CreateInvoiceInput struct {
	LearnerID      string
	FeeStructureID string
	TermID         string
	DueDate        string
	Lines          []InvoiceLineInput
}

// CreateInvoice creates an invoice with >= 1 positive lines (the total is
// derived from lines, never stored) and emits finance.InvoiceCreated.v1.
func (s *Service) CreateInvoice(ctx context.Context, schoolID, actorID string, in CreateInvoiceInput) (*Invoice, error) {
	if _, err := uuid.Parse(in.LearnerID); err != nil {
		return nil, fmt.Errorf("%w: invalid learnerId", ErrValidation)
	}
	if len(in.Lines) == 0 || len(in.Lines) > maxInvoiceLines {
		return nil, ErrInvoiceNoLines
	}
	for _, l := range in.Lines {
		if l.AmountMinor <= 0 {
			return nil, fmt.Errorf("%w: line amounts must be positive", ErrValidation)
		}
		if len(l.Description) > 200 {
			return nil, fmt.Errorf("%w: line description too long", ErrValidation)
		}
	}
	if _, err := time.Parse("2006-01-02", in.DueDate); err != nil {
		return nil, fmt.Errorf("%w: dueDate must be YYYY-MM-DD", ErrValidation)
	}
	var feeID, termID *string
	if in.FeeStructureID != "" {
		feeID = &in.FeeStructureID
	}
	if in.TermID != "" {
		termID = &in.TermID
	}
	inv := &Invoice{
		ID:             uuid.NewString(),
		SchoolID:       schoolID,
		LearnerID:      in.LearnerID,
		FeeStructureID: feeID,
		TermID:         termID,
		DueDate:        in.DueDate,
		Currency:       "KES",
		Status:         InvoiceOpen,
	}
	for _, l := range in.Lines {
		inv.Lines = append(inv.Lines, InvoiceLine{Description: l.Description, AmountMinor: l.AmountMinor})
	}
	if err := s.repo.InsertInvoice(ctx, inv); err != nil {
		return nil, err
	}
	s.emit(ctx, schoolID, inv.ID, "finance.InvoiceCreated", map[string]any{
		"learner_id":  inv.LearnerID,
		"total_minor": inv.TotalMinor,
		"due_date":    inv.DueDate,
	})
	return s.repo.InvoiceByID(ctx, schoolID, inv.ID)
}

// Invoice resolves one invoice with derived totals (school-scoped).
func (s *Service) Invoice(ctx context.Context, schoolID, id string) (*Invoice, error) {
	return s.repo.InvoiceByID(ctx, schoolID, id)
}

// Invoices lists invoices (status/learner filters), paginated.
func (s *Service) Invoices(ctx context.Context, schoolID string, status *InvoiceStatus, learnerID string, limit, offset int) ([]*Invoice, int, error) {
	return s.repo.ListInvoices(ctx, schoolID, status, learnerID, limit, offset)
}

// VoidInvoice voids an open invoice (no allocations allowed).
func (s *Service) VoidInvoice(ctx context.Context, schoolID, id string) (*Invoice, error) {
	inv, err := s.repo.InvoiceByID(ctx, schoolID, id)
	if err != nil {
		return nil, err
	}
	if inv.Status == InvoiceVoid {
		return inv, nil
	}
	if inv.PaidMinor > 0 {
		return nil, fmt.Errorf("%w: invoice has allocations", ErrNotAllocatable)
	}
	if err := s.repo.SetInvoiceStatus(ctx, schoolID, id, InvoiceVoid); err != nil {
		return nil, err
	}
	return s.repo.InvoiceByID(ctx, schoolID, id)
}

// --- payments ---------------------------------------------------------------

// CreatePaymentInput carries the payment intent fields.
type CreatePaymentInput struct {
	InvoiceID   string
	PayerRef    string
	AmountMinor int64
	Provider    string
	ProviderRef string
}

// CreatePayment records a money-in intent (status pending). Confirmation
// happens ONLY through the verified webhook path (§30).
func (s *Service) CreatePayment(ctx context.Context, schoolID string, in CreatePaymentInput) (*Payment, error) {
	if in.AmountMinor <= 0 {
		return nil, fmt.Errorf("%w: amountMinor must be positive", ErrValidation)
	}
	in.Provider = strings.TrimSpace(in.Provider)
	in.ProviderRef = strings.TrimSpace(in.ProviderRef)
	if in.Provider == "" || len(in.Provider) > 64 {
		return nil, fmt.Errorf("%w: provider required (<=64 chars)", ErrValidation)
	}
	if in.ProviderRef == "" || len(in.ProviderRef) > 128 {
		return nil, fmt.Errorf("%w: providerRef required (<=128 chars)", ErrValidation)
	}
	var invID *string
	if in.InvoiceID != "" {
		if _, err := s.repo.InvoiceByID(ctx, schoolID, in.InvoiceID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, fmt.Errorf("%w: invoice not found in this school", ErrNotFound)
			}
			return nil, err
		}
		invID = &in.InvoiceID
	}
	p := &Payment{
		ID:          uuid.NewString(),
		SchoolID:    schoolID,
		InvoiceID:   invID,
		PayerRef:    strings.TrimSpace(in.PayerRef),
		AmountMinor: in.AmountMinor,
		Currency:    "KES",
		Provider:    in.Provider,
		ProviderRef: in.ProviderRef,
		Status:      "pending",
	}
	if err := s.repo.InsertPayment(ctx, p); err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: providerRef already recorded (reconciliation key)", ErrValidation)
		}
		return nil, err
	}
	return p, nil
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

// Payment resolves one payment (school-scoped).
func (s *Service) Payment(ctx context.Context, schoolID, id string) (*Payment, error) {
	return s.repo.PaymentByID(ctx, schoolID, id)
}

// ConfirmWebhookInput carries the verified webhook payload.
type ConfirmWebhookInput struct {
	EventID     string // provider event id — the idempotency key
	PaymentID   string
	Status      string // confirmed | failed
	ProviderRef string
}

// ConfirmWebhook applies a provider webhook: signature verified by the
// transport; here provider-event-id idempotency + atomic confirmation:
//
//	tx {
//	  payment CAS pending -> confirmed
//	  journal posting (debit bank/wallet_main, credit fees_receivable)
//	  allocation to the invoice (capped at balance)
//	  invoice status update (open -> partially_paid -> paid)
//	  idempotency key write
//	  finance.PaymentConfirmed.v1 + finance.ReceiptIssued.v1 (same tx)
//	}
//
// Replayed event ids return the stored result WITHOUT any new posting
// (ADR-004 §3).
func (s *Service) ConfirmWebhook(ctx context.Context, schoolID string, in ConfirmWebhookInput) (map[string]any, error) {
	if in.EventID == "" || in.PaymentID == "" {
		return nil, fmt.Errorf("%w: eventId and paymentId required", ErrValidation)
	}
	key := "webhook:" + in.EventID

	// Fast path: already processed.
	if stored, err := s.repo.IdempotencyResult(ctx, key); err != nil {
		return nil, err
	} else if stored != nil {
		var out map[string]any
		if err := unmarshalStored(*stored, &out); err != nil {
			return nil, err
		}
		out["replayed"] = true
		return out, nil
	}

	// Lock the payment row via the CAS inside the tx below; resolve first
	// for validation errors.
	p, err := s.repo.PaymentByID(ctx, schoolID, in.PaymentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: payment not found in this school", ErrNotFound)
		}
		return nil, err
	}
	if p.ProviderRef != "" && in.ProviderRef != "" && p.ProviderRef != in.ProviderRef {
		return nil, fmt.Errorf("%w: providerRef mismatch", ErrValidation)
	}

	out := map[string]any{"paymentId": p.ID, "status": "confirmed"}
	err = s.pool.WithinTx(ctx, func(tx postgres.Querier) error {
		// CAS: only the first confirmation proceeds; replays no-op.
		confirmed, err := s.repo.MarkPaymentConfirmed(ctx, tx, schoolID, p.ID)
		if err != nil {
			return err
		}
		if !confirmed {
			// Already confirmed or failed: serve the current state without
			// new postings (provider retries with fresh event ids).
			cur, gerr := s.repo.PaymentByID(ctx, schoolID, p.ID)
			if gerr != nil {
				return gerr
			}
			out["status"] = cur.Status
			out["replayed"] = true
			return nil
		}

		// Ledger posting: Debit bank (asset up), Credit fees_receivable
		// (asset down = collected). The wallet_main purpose account carries
		// the inflow; bank mirrors the institutional account per ADR-005 —
		// we post to wallet_main to keep the wallet view truthful.
		walletMain, aerr := s.accountCodeToID(ctx, schoolID, "wallet_main")
		if aerr != nil {
			return aerr
		}
		receivable, aerr := s.accountCodeToID(ctx, schoolID, "fees_receivable")
		if aerr != nil {
			return aerr
		}
		entry := &JournalEntry{
			ID:          uuid.NewString(),
			SchoolID:    schoolID,
			EntryDate:   time.Now().UTC().Format("2006-01-02"),
			Description: "Payment " + p.ProviderRef,
			Source:      "payment",
			SourceRef:   p.ID,
		}
		entry.Lines = []JournalLine{
			{AccountID: walletMain, DebitMinor: p.AmountMinor},
			{AccountID: receivable, CreditMinor: p.AmountMinor},
		}
		if err := s.repo.InsertEntryTx(ctx, tx, entry); err != nil {
			return err
		}

		// Allocation + invoice status (when attached to an invoice).
		if p.InvoiceID != nil && *p.InvoiceID != "" {
			inv, gerr := s.repo.InvoiceByID(ctx, schoolID, *p.InvoiceID)
			if gerr != nil {
				return gerr
			}
			if inv.Status == InvoiceVoid {
				return ErrNotAllocatable
			}
			allocatable := inv.BalanceMinor
			if allocatable > 0 {
				apply := p.AmountMinor
				if apply > allocatable {
					apply = allocatable // overpayment stays on the wallet, not the invoice
				}
				if err := s.repo.InsertAllocation(ctx, tx, p.ID, inv.ID, apply); err != nil {
					return err
				}
				paid, err := s.repo.AllocatedMinor(ctx, tx, inv.ID)
				if err != nil {
					return err
				}
				status := InvoicePartiallyPaid
				if paid >= inv.TotalMinor {
					status = InvoicePaid
				}
				if err := s.repo.SetInvoiceStatus(ctx, schoolID, inv.ID, status); err != nil {
					return err
				}
				out["invoiceId"] = inv.ID
				out["invoiceStatus"] = string(status)
			}
		}

		// Idempotency key + events — SAME transaction (ADR-004 §6).
		result, merr := marshalResult(out)
		if merr != nil {
			return merr
		}
		if err := s.repo.StoreIdempotencyKey(ctx, tx, key, result); err != nil {
			return err
		}
		if _, err := events.Record(ctx, tx, &schoolID, p.ID, "finance.PaymentConfirmed", 1, map[string]any{
			"payment_id":   p.ID,
			"amount_minor": p.AmountMinor,
			"provider":     p.Provider,
			"provider_ref": p.ProviderRef,
		}); err != nil {
			return err
		}
		if _, err := events.Record(ctx, tx, &schoolID, p.ID, "finance.ReceiptIssued", 1, map[string]any{
			"payment_id":   p.ID,
			"amount_minor": p.AmountMinor,
			"invoice_id":   p.InvoiceID,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// FailWebhook marks a payment failed (verified webhook, status=failed).
func (s *Service) FailWebhook(ctx context.Context, schoolID, eventID, paymentID string) (map[string]any, error) {
	if eventID == "" || paymentID == "" {
		return nil, fmt.Errorf("%w: eventId and paymentId required", ErrValidation)
	}
	key := "webhook:" + eventID
	if _, err := s.repo.PaymentByID(ctx, schoolID, paymentID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: payment not found in this school", ErrNotFound)
		}
		return nil, err
	}
	ok, err := s.repo.MarkPaymentFailed(ctx, schoolID, paymentID)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"paymentId": paymentID, "status": "failed", "applied": ok}
	if err := s.repo.StoreIdempotencyKey(ctx, s.pool, key, []byte(`{"status":"failed"}`)); err != nil {
		return nil, err
	}
	return out, nil
}

// Wallet returns the derived balances of the institution wallet (ADR-005).
func (s *Service) Wallet(ctx context.Context, schoolID string) ([]*WalletBalance, error) {
	if err := s.EnsureChart(ctx, schoolID); err != nil {
		return nil, err
	}
	return s.repo.WalletBalances(ctx, schoolID)
}

// --- helpers ----------------------------------------------------------------

func (s *Service) accountCodeToID(ctx context.Context, schoolID, code string) (string, error) {
	a, err := s.repo.AccountByCode(ctx, schoolID, code)
	if errors.Is(err, ErrNotFound) {
		// Lazy chart seed on first financial operation.
		if err := s.EnsureChart(ctx, schoolID); err != nil {
			return "", err
		}
		a, err = s.repo.AccountByCode(ctx, schoolID, code)
	}
	if err != nil {
		return "", err
	}
	return a.ID, nil
}

func marshalResult(v map[string]any) ([]byte, error) {
	return json.Marshal(v)
}

func unmarshalStored(raw string, out *map[string]any) error {
	return json.Unmarshal([]byte(raw), out)
}

func (s *Service) emit(ctx context.Context, schoolID, aggregateID, eventType string, payload map[string]any) {
	if s.pool == nil {
		return
	}
	_, _ = events.Record(ctx, s.pool, &schoolID, aggregateID, eventType, 1, payload)
}
