// Package finance is the bounded context owning financial truth (ADR-004,
// ADR-005): a double-entry ledger with immutable postings, idempotency keys,
// fee structures, invoices with payment allocation, server-confirmed
// payments, and the institution wallet as a ledger abstraction.
//
// Non-negotiables (master spec §28–30, §59):
//   - amounts are integer minor units — floats never;
//   - balances are DERIVED from lines, never stored mutable fields;
//   - ledger rows are immutable (DB-trigger backstop); corrections are new
//     compensating entries;
//   - payments are confirmed ONLY server-side (webhook signature +
//     provider-event-id idempotency); posting + allocation + receipt events
//     happen in ONE transaction.
package finance

import (
	"context"
	"errors"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
)

// AccountType is the accounting classification of a ledger account.
type AccountType string

const (
	AccountAsset     AccountType = "ASSET"
	AccountLiability AccountType = "LIABILITY"
	AccountEquity    AccountType = "EQUITY"
	AccountRevenue   AccountType = "REVENUE"
	AccountExpense   AccountType = "EXPENSE"
)

// WalletPurpose labels the institution wallet view (ADR-005). Each purpose
// maps to a dedicated ASSET ledger account.
type WalletPurpose string

const (
	WalletMain       WalletPurpose = "main"
	WalletFees       WalletPurpose = "fees"
	WalletPayroll    WalletPurpose = "payroll"
	WalletTransport  WalletPurpose = "transport"
	WalletMeals      WalletPurpose = "meals"
	WalletActivities WalletPurpose = "activities"
	WalletReserve    WalletPurpose = "reserve"
)

// AllWalletPurposes is the closed set of wallet purposes.
var AllWalletPurposes = []WalletPurpose{
	WalletMain, WalletFees, WalletPayroll, WalletTransport,
	WalletMeals, WalletActivities, WalletReserve,
}

// ValidWalletPurpose reports whether p is a known purpose.
func ValidWalletPurpose(p WalletPurpose) bool {
	for _, known := range AllWalletPurposes {
		if known == p {
			return true
		}
	}
	return false
}

// LedgerAccount is a per-school account in the chart of accounts.
type LedgerAccount struct {
	ID        string      `json:"id"`
	SchoolID  string      `json:"schoolId"`
	Code      string      `json:"code"`
	Name      string      `json:"name"`
	Type      AccountType `json:"type"`
	Purpose   string      `json:"purpose,omitempty"`
	Currency  string      `json:"currency"`
	CreatedAt time.Time   `json:"createdAt"`
}

// JournalEntry is an immutable posting with >= 2 balanced lines.
type JournalEntry struct {
	ID            string        `json:"id"`
	SchoolID      string        `json:"schoolId"`
	EntryDate     string        `json:"entryDate"` // YYYY-MM-DD
	Description   string        `json:"description,omitempty"`
	Source        string        `json:"source"` // manual | payment | correction
	SourceRef     string        `json:"sourceRef,omitempty"`
	ActorID       string        `json:"actorId,omitempty"`
	CorrelationID string        `json:"correlationId,omitempty"`
	CreatedAt     time.Time     `json:"createdAt"`
	Lines         []JournalLine `json:"lines,omitempty"`
}

// JournalLine is one side of one posting: exactly one of debit/credit > 0.
type JournalLine struct {
	ID          int64  `json:"id"`
	EntryID     string `json:"entryId"`
	AccountID   string `json:"accountId"`
	DebitMinor  int64  `json:"debitMinor"`
	CreditMinor int64  `json:"creditMinor"`
}

// LineInput carries one posting line for entry creation.
type LineInput struct {
	AccountID   string `json:"accountId"`
	DebitMinor  int64  `json:"debitMinor"`
	CreditMinor int64  `json:"creditMinor"`
}

// FeeStructure prices one charge for a class/year/term.
type FeeStructure struct {
	ID             string    `json:"id"`
	SchoolID       string    `json:"schoolId"`
	Name           string    `json:"name"`
	ClassGroupID   *string   `json:"classGroupId,omitempty"`
	AcademicYearID *string   `json:"academicYearId,omitempty"`
	TermID         *string   `json:"termId,omitempty"`
	AmountMinor    int64     `json:"amountMinor"`
	Currency       string    `json:"currency"`
	CreatedAt      time.Time `json:"createdAt"`
}

// InvoiceStatus is derived from allocations (open -> partially_paid -> paid);
// void is terminal.
type InvoiceStatus string

const (
	InvoiceOpen          InvoiceStatus = "open"
	InvoicePartiallyPaid InvoiceStatus = "partially_paid"
	InvoicePaid          InvoiceStatus = "paid"
	InvoiceVoid          InvoiceStatus = "void"
)

// Invoice is a learner's bill; the total is the SUM of its lines (derived).
type Invoice struct {
	ID             string        `json:"id"`
	SchoolID       string        `json:"schoolId"`
	LearnerID      string        `json:"learnerId"`
	FeeStructureID *string       `json:"feeStructureId,omitempty"`
	TermID         *string       `json:"termId,omitempty"`
	DueDate        string        `json:"dueDate"`
	Currency       string        `json:"currency"`
	Status         InvoiceStatus `json:"status"`
	TotalMinor     int64         `json:"totalMinor"`   // derived: SUM(lines)
	PaidMinor      int64         `json:"paidMinor"`    // derived: SUM(allocations)
	BalanceMinor   int64         `json:"balanceMinor"` // derived: total - paid
	CreatedAt      time.Time     `json:"createdAt"`
	Lines          []InvoiceLine `json:"lines,omitempty"`
}

// InvoiceLine is one charge row of an invoice.
type InvoiceLine struct {
	ID          int64  `json:"id"`
	InvoiceID   string `json:"invoiceId"`
	Description string `json:"description,omitempty"`
	AmountMinor int64  `json:"amountMinor"`
}

// InvoiceLineInput carries one line at creation.
type InvoiceLineInput struct {
	Description string `json:"description"`
	AmountMinor int64  `json:"amountMinor"`
}

// Payment is a money-in intent; confirmation happens ONLY via the verified
// webhook path (§30).
type Payment struct {
	ID          string     `json:"id"`
	SchoolID    string     `json:"schoolId"`
	InvoiceID   *string    `json:"invoiceId,omitempty"`
	PayerRef    string     `json:"payerRef,omitempty"`
	AmountMinor int64      `json:"amountMinor"`
	Currency    string     `json:"currency"`
	Provider    string     `json:"provider"`
	ProviderRef string     `json:"providerRef"`
	Status      string     `json:"status"` // pending | confirmed | failed
	CreatedAt   time.Time  `json:"createdAt"`
	ConfirmedAt *time.Time `json:"confirmedAt,omitempty"`
}

// WalletBalance is a derived balance for one wallet purpose.
type WalletBalance struct {
	Purpose      WalletPurpose `json:"purpose"`
	AccountID    string        `json:"accountId"`
	Currency     string        `json:"currency"`
	BalanceMinor int64         `json:"balanceMinor"` // SUM(debits) - SUM(credits)
}

// Domain errors.
var (
	ErrValidation      = errors.New("finance: validation failed")
	ErrNotFound        = errors.New("finance: not found")
	ErrUnbalanced      = errors.New("finance: journal entry debits must equal credits")
	ErrIdempotencyUsed = errors.New("finance: idempotency key already used")
	ErrNotAllocatable  = errors.New("finance: invoice is not open for allocation")
	ErrOverpay         = errors.New("finance: allocation exceeds invoice balance")
	ErrInvoiceNoLines  = errors.New("finance: invoice needs at least one line")
)

// maxInvoiceLines bounds invoice creation.
const maxInvoiceLines = 100

// Repo is the persistence port. Every method takes the acting schoolID.
type Repo interface {
	// Ledger.
	CreateAccount(ctx context.Context, schoolID string, a *LedgerAccount) error
	AccountByID(ctx context.Context, schoolID, id string) (*LedgerAccount, error)
	AccountByCode(ctx context.Context, schoolID, code string) (*LedgerAccount, error)
	ListAccounts(ctx context.Context, schoolID string) ([]*LedgerAccount, error)
	// InsertEntry writes entry + lines inside ONE transaction; the deferred
	// DB trigger re-checks debits == credits at commit.
	InsertEntry(ctx context.Context, e *JournalEntry) error
	// InsertEntryTx is InsertEntry on a caller-owned transaction (payment
	// confirmation composes posting + allocation + events in one tx).
	InsertEntryTx(ctx context.Context, tx postgres.Querier, e *JournalEntry) error
	EntryByID(ctx context.Context, schoolID, id string) (*JournalEntry, error)

	// Idempotency (platform table, scope='finance').
	IdempotencyResult(ctx context.Context, scope, key string) (*string, error)
	// ClaimIdempotencyKey inserts-first (gating claim) inside the caller's
	// transaction; on conflict it waits and returns the winner's stored
	// response — single-tx idempotency (issue #45).
	ClaimIdempotencyKey(ctx context.Context, tx postgres.Querier, scope, key, schoolID string) (bool, *string, error)
	StoreIdempotencyKey(ctx context.Context, tx postgres.Querier, scope, key, schoolID string, result []byte) error

	// Fee structures.
	CreateFeeStructure(ctx context.Context, schoolID string, fs *FeeStructure) error
	ListFeeStructures(ctx context.Context, schoolID string) ([]*FeeStructure, error)

	// Invoices.
	InsertInvoice(ctx context.Context, inv *Invoice) error
	InvoiceByID(ctx context.Context, schoolID, id string) (*Invoice, error)
	// InvoiceByIDForUpdate locks the invoice row (SELECT ... FOR UPDATE) for
	// use inside the confirmation transaction — serializes concurrent
	// confirmations per invoice so allocations can never exceed the balance
	// (issue #44).
	InvoiceByIDForUpdate(ctx context.Context, q postgres.Querier, schoolID, id string) (*Invoice, error)
	ListInvoices(ctx context.Context, schoolID string, status *InvoiceStatus, learnerID string, limit, offset int) ([]*Invoice, int, error)
	// SetInvoiceStatus executes on the supplied Querier so the status write
	// participates in the caller's transaction (issue #44: a pool write
	// escaped the confirmation tx and could persist paid/partially_paid while
	// the postings rolled back).
	SetInvoiceStatus(ctx context.Context, q postgres.Querier, schoolID, id string, status InvoiceStatus) error

	// Payments.
	InsertPayment(ctx context.Context, p *Payment) error
	PaymentByID(ctx context.Context, schoolID, id string) (*Payment, error)
	PaymentByProviderRef(ctx context.Context, schoolID, provider, providerRef string) (*Payment, error)
	// MarkPaymentConfirmed CAS: pending -> confirmed; false when not pending.
	MarkPaymentConfirmed(ctx context.Context, tx postgres.Querier, schoolID, id string) (bool, error)
	MarkPaymentFailed(ctx context.Context, schoolID, id string) (bool, error)
	InsertAllocation(ctx context.Context, tx postgres.Querier, paymentID, invoiceID string, amountMinor int64) error
	AllocatedMinor(ctx context.Context, tx postgres.Querier, invoiceID string) (int64, error)

	// Wallet (derived balances).
	WalletBalances(ctx context.Context, schoolID string) ([]*WalletBalance, error)
}
