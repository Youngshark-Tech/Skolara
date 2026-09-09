//go:build integration

package finance

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/testdb"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
	"github.com/google/uuid"
)

type fixture struct {
	svc     *Service
	auth    *identity.AuthService
	jwt     *identity.JWTManager
	tenancy *tenancy.Service
	pool    *postgres.Pool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testdb.New(t)
	jwt := identity.NewJWTManager("integration-test-secret-at-least-32-bytes!", 1<<30)
	authSvc := identity.NewAuthService(identity.NewRepo(pool), jwt)
	tenSvc := tenancy.NewService(tenancy.NewRepo(pool), pool)
	svc := NewService(NewRepo(pool), pool, "integration-webhook-secret-0123456789abcdef")
	return &fixture{svc: svc, auth: authSvc, jwt: jwt, tenancy: tenSvc, pool: pool}
}

func mustSchool(t *testing.T, f *fixture, code, name string) *tenancy.School {
	t.Helper()
	s, err := f.tenancy.CreateSchool(context.Background(), code, name, "", "")
	if err != nil {
		t.Fatalf("create school %s: %v", code, err)
	}
	if err := f.svc.EnsureChart(context.Background(), s.ID); err != nil {
		t.Fatalf("ensure chart: %v", err)
	}
	return s
}

func mustAccount(t *testing.T, f *fixture, schoolID, code string) *LedgerAccount {
	t.Helper()
	a, err := f.svc.repo.AccountByCode(context.Background(), schoolID, code)
	if err != nil {
		t.Fatalf("account %s: %v", code, err)
	}
	return a
}

func mustUser(t *testing.T, f *fixture, email string) string {
	t.Helper()
	u, err := f.auth.CreateUser(context.Background(), email, email, "s3cure-passw0rd!", "")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.ID
}

// seedLearner inserts a learner via raw SQL (no students import).
func seedLearner(t *testing.T, f *fixture, first, last string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.pool.Exec(context.Background(),
		`INSERT INTO learners (id, first_name, last_name) VALUES ($1,$2,$3)`, id, first, last)
	if err != nil {
		t.Fatalf("seed learner: %v", err)
	}
	return id
}

func accountBalance(t *testing.T, f *fixture, accountID string) int64 {
	t.Helper()
	var bal int64
	err := f.pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(debit_minor),0) - COALESCE(SUM(credit_minor),0)
		 FROM journal_lines WHERE account_id = $1`, accountID).Scan(&bal)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	return bal
}

func journalCount(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM journal_entries`).Scan(&n)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func sign(t *testing.T, f *fixture, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte("integration-webhook-secret-0123456789abcdef"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// TestLedgerPostingInvariants is the acceptance-critical property test (§59):
// across a sequence of randomized valid postings, debits == credits on every
// entry and derived account balances equal the line sums.
func TestLedgerPostingInvariants(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "FIN-P", "Invariant School")
	receivable := mustAccount(t, f, school.ID, "fees_receivable")
	bank := mustAccount(t, f, school.ID, "bank")
	income := mustAccount(t, f, school.ID, "tuition_income")

	rng := rand.New(rand.NewSource(42))
	expectedBank := int64(0)
	expectedIncome := int64(0)

	for i := 0; i < 25; i++ {
		amount := int64(100 + rng.Intn(100000))
		// Two-posting business flow: money in (bank vs receivable), then
		// revenue recognition (receivable vs income).
		if _, err := f.svc.PostEntry(ctx, school.ID, "", fmt.Sprintf("op-%d", i), "", "", "",
			[]LineInput{
				{AccountID: bank.ID, DebitMinor: amount},
				{AccountID: receivable.ID, CreditMinor: amount},
			}); err != nil {
			t.Fatalf("posting %d: %v", i, err)
		}
		if _, err := f.svc.PostEntry(ctx, school.ID, "", fmt.Sprintf("rev-%d", i), "", "", "",
			[]LineInput{
				{AccountID: receivable.ID, DebitMinor: amount},
				{AccountID: income.ID, CreditMinor: amount},
			}); err != nil {
			t.Fatalf("posting %d: %v", i, err)
		}
		expectedBank += amount
		expectedIncome += amount
	}

	if got := accountBalance(t, f, bank.ID); got != expectedBank {
		t.Fatalf("bank balance = %d, want %d", got, expectedBank)
	}
	if got := accountBalance(t, f, income.ID); got != -expectedIncome {
		t.Fatalf("income balance = %d, want -%d (credits negative)", got, expectedIncome)
	}
	// Receivable nets to zero across in+recognition.
	if got := accountBalance(t, f, receivable.ID); got != 0 {
		t.Fatalf("receivable balance = %d, want 0", got)
	}
	// Global invariant: SUM(debits) == SUM(credits) across the school.
	var d, c int64
	if err := f.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(debit_minor),0), COALESCE(SUM(credit_minor),0)
		 FROM journal_lines WHERE school_id = $1`, school.ID).Scan(&d, &c); err != nil {
		t.Fatal(err)
	}
	if d != c {
		t.Fatalf("GLOBAL INVARIANT BROKEN: debits=%d credits=%d", d, c)
	}
}

func TestLedgerValidationAndImmutability(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "FIN-I", "Immutable School")
	bank := mustAccount(t, f, school.ID, "bank")
	receivable := mustAccount(t, f, school.ID, "fees_receivable")

	// Unbalanced rejected by the app.
	if _, err := f.svc.PostEntry(ctx, school.ID, "", "bad", "", "", "",
		[]LineInput{
			{AccountID: bank.ID, DebitMinor: 100},
			{AccountID: receivable.ID, CreditMinor: 90},
		}); err == nil {
		t.Fatal("unbalanced entry accepted")
	}
	// Single-line rejected (needs >= 2).
	if _, err := f.svc.PostEntry(ctx, school.ID, "", "solo", "", "", "",
		[]LineInput{{AccountID: bank.ID, DebitMinor: 100}}); err == nil {
		t.Fatal("single-line entry accepted")
	}
	// Both sides set on one line rejected.
	if _, err := f.svc.PostEntry(ctx, school.ID, "", "both", "", "", "",
		[]LineInput{
			{AccountID: bank.ID, DebitMinor: 100, CreditMinor: 100},
			{AccountID: receivable.ID, DebitMinor: 100, CreditMinor: 100},
		}); err == nil {
		t.Fatal("two-sided line accepted")
	}
	// Foreign-school account rejected.
	other := mustSchool(t, f, "FIN-O", "Other")
	otherBank := mustAccount(t, f, other.ID, "bank")
	if _, err := f.svc.PostEntry(ctx, school.ID, "", "cross", "", "", "",
		[]LineInput{
			{AccountID: otherBank.ID, DebitMinor: 50},
			{AccountID: receivable.ID, CreditMinor: 50},
		}); err == nil {
		t.Fatal("cross-school account accepted")
	}

	// DB immutability backstop: UPDATE/DELETE rejected by trigger.
	e, err := f.svc.PostEntry(ctx, school.ID, "", "immutable", "", "", "",
		[]LineInput{
			{AccountID: bank.ID, DebitMinor: 70},
			{AccountID: receivable.ID, CreditMinor: 70},
		})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE journal_entries SET description = 'hacked' WHERE id = $1`, e.ID); err == nil {
		t.Fatal("UPDATE on journal_entries not blocked")
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM journal_lines WHERE entry_id = $1`, e.ID); err == nil {
		t.Fatal("DELETE on journal_lines not blocked")
	}
	// Direct unbalanced insert is blocked by the deferred trigger at commit.
	err = f.pool.WithinTx(ctx, func(tx postgres.Querier) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO journal_lines (entry_id, school_id, account_id, debit_minor, credit_minor)
			 VALUES ($1,$2,$3,999,0)`, e.ID, school.ID, bank.ID)
		return err
	})
	// Wait: inserting a 4th line makes debits != credits for that entry.
	if err == nil {
		t.Fatal("unbalanced direct insert committed — trigger missing")
	}
}

func TestIdempotentPostingAndCorrections(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "FIN-K", "Idempotency School")
	bank := mustAccount(t, f, school.ID, "bank")
	receivable := mustAccount(t, f, school.ID, "fees_receivable")

	lines := []LineInput{
		{AccountID: bank.ID, DebitMinor: 500},
		{AccountID: receivable.ID, CreditMinor: 500},
	}
	e1, err := f.svc.PostEntry(ctx, school.ID, "", "first", "", "caller-key-1", "", lines)
	if err != nil {
		t.Fatal(err)
	}
	before := journalCount(t, f)
	e2, err := f.svc.PostEntry(ctx, school.ID, "", "replay", "", "caller-key-1", "", lines)
	if err != nil {
		t.Fatalf("replay errored: %v", err)
	}
	if e1.ID != e2.ID {
		t.Fatalf("replay returned different entry %s != %s", e2.ID, e1.ID)
	}
	if after := journalCount(t, f); after != before {
		t.Fatalf("replay created %d extra postings", after-before)
	}

	// Correction: compensating entry referencing the original keeps balance.
	if _, err := f.svc.PostEntry(ctx, school.ID, "", "reverse", "", "", e1.ID,
		[]LineInput{
			{AccountID: receivable.ID, DebitMinor: 500},
			{AccountID: bank.ID, CreditMinor: 500},
		}); err != nil {
		t.Fatalf("correction: %v", err)
	}
	if got := accountBalance(t, f, bank.ID); got != 0 {
		t.Fatalf("bank after correction = %d, want 0", got)
	}
	var d, c int64
	if err := f.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(debit_minor),0), COALESCE(SUM(credit_minor),0) FROM journal_lines`).Scan(&d, &c); err != nil {
		t.Fatal(err)
	}
	if d != c {
		t.Fatalf("GLOBAL INVARIANT BROKEN after correction: %d vs %d", d, c)
	}
}

func TestInvoicePaymentWebhookEndToEnd(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "FIN-E", "E2E School")
	learner := seedLearner(t, f, "Kay", "Eleven")

	// Invoice: 12,500.00 KES total.
	inv, err := f.svc.CreateInvoice(ctx, school.ID, "", CreateInvoiceInput{
		LearnerID: learner,
		DueDate:   time.Now().UTC().Add(72 * time.Hour).Format("2006-01-02"),
		Lines: []InvoiceLineInput{
			{Description: "Tuition term 1", AmountMinor: 1_000_000},
			{Description: "Activities", AmountMinor: 250_000},
		},
	})
	if err != nil || inv.TotalMinor != 1_250_000 || inv.Status != InvoiceOpen {
		t.Fatalf("invoice: %v %+v", err, inv)
	}

	// Payment intent (pending) — created via API as the provider handshake.
	p, err := f.svc.CreatePayment(ctx, school.ID, CreatePaymentInput{
		InvoiceID:   inv.ID,
		PayerRef:    "guardian-42",
		AmountMinor: 1_250_000,
		Provider:    "mpesa",
		ProviderRef: "MPX-1001",
	})
	if err != nil || p.Status != "pending" {
		t.Fatalf("payment: %v %+v", err, p)
	}

	// Duplicate provider ref rejected (reconciliation groundwork).
	if _, err := f.svc.CreatePayment(ctx, school.ID, CreatePaymentInput{
		InvoiceID: inv.ID, AmountMinor: 5, Provider: "mpesa", ProviderRef: "MPX-1001",
	}); err == nil {
		t.Fatal("duplicate provider ref accepted")
	}

	// Confirm via verified webhook.
	out, err := f.svc.ConfirmWebhook(ctx, school.ID, ConfirmWebhookInput{
		EventID: "evt-1", PaymentID: p.ID, Status: "confirmed", ProviderRef: "MPX-1001",
	})
	if err != nil || out["status"] != "confirmed" {
		t.Fatalf("confirm: %v %v", err, out)
	}

	// Invoice fully paid.
	inv2, err := f.svc.Invoice(ctx, school.ID, inv.ID)
	if err != nil || inv2.Status != InvoicePaid || inv2.PaidMinor != 1_250_000 || inv2.BalanceMinor != 0 {
		t.Fatalf("invoice after payment: %v %+v", err, inv2)
	}

	// Ledger posting exists and wallet_main grew by the amount.
	walletMain := mustAccount(t, f, school.ID, "wallet_main")
	if got := accountBalance(t, f, walletMain.ID); got != 1_250_000 {
		t.Fatalf("wallet_main = %d, want 1250000", got)
	}

	// Events emitted (same-tx path): PaymentConfirmed + ReceiptIssued.
	var evn int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM event_outbox WHERE event_type IN ('finance.PaymentConfirmed','finance.ReceiptIssued') AND school_id = $1`,
		school.ID).Scan(&evn); err != nil || evn != 2 {
		t.Fatalf("events = %d err=%v", evn, err)
	}

	// Replay the SAME event: stored result, zero additional postings.
	before := journalCount(t, f)
	out2, err := f.svc.ConfirmWebhook(ctx, school.ID, ConfirmWebhookInput{
		EventID: "evt-1", PaymentID: p.ID, Status: "confirmed",
	})
	if err != nil || out2["replayed"] != true {
		t.Fatalf("replay: %v %v", err, out2)
	}
	if after := journalCount(t, f); after != before {
		t.Fatalf("duplicate webhook created %d extra postings", after-before)
	}

	// A DIFFERENT event id for the same payment: no new postings either
	// (payment status CAS guard).
	if _, err := f.svc.ConfirmWebhook(ctx, school.ID, ConfirmWebhookInput{
		EventID: "evt-2", PaymentID: p.ID, Status: "confirmed",
	}); err != nil {
		t.Fatalf("late duplicate event errored: %v", err)
	}
	if after := journalCount(t, f); after != before {
		t.Fatalf("second event created %d extra postings", after-before)
	}
}

func TestPartialPaymentAndVoid(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "FIN-V", "Void School")
	learner := seedLearner(t, f, "Lew", "Twelve")

	inv, err := f.svc.CreateInvoice(ctx, school.ID, "", CreateInvoiceInput{
		LearnerID: learner,
		DueDate:   time.Now().UTC().Add(48 * time.Hour).Format("2006-01-02"),
		Lines:     []InvoiceLineInput{{AmountMinor: 100_000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	p1, err := f.svc.CreatePayment(ctx, school.ID, CreatePaymentInput{
		InvoiceID: inv.ID, AmountMinor: 40_000, Provider: "manual", ProviderRef: "MAN-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ConfirmWebhook(ctx, school.ID, ConfirmWebhookInput{EventID: "v-evt-1", PaymentID: p1.ID, Status: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	partial, _ := f.svc.Invoice(ctx, school.ID, inv.ID)
	if partial.Status != InvoicePartiallyPaid || partial.BalanceMinor != 60_000 {
		t.Fatalf("partial: %+v", partial)
	}
	// Overpayment: allocation capped at balance; rest stays on the wallet.
	p2, err := f.svc.CreatePayment(ctx, school.ID, CreatePaymentInput{
		InvoiceID: inv.ID, AmountMinor: 80_000, Provider: "manual", ProviderRef: "MAN-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ConfirmWebhook(ctx, school.ID, ConfirmWebhookInput{EventID: "v-evt-2", PaymentID: p2.ID, Status: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	paid, _ := f.svc.Invoice(ctx, school.ID, inv.ID)
	if paid.Status != InvoicePaid || paid.PaidMinor != 100_000 {
		t.Fatalf("paid: %+v", paid)
	}
	// Voiding a paid invoice rejected; voiding an untouched open invoice works.
	if _, err := f.svc.VoidInvoice(ctx, school.ID, inv.ID); err == nil {
		t.Fatal("void of paid invoice accepted")
	}
	inv2, err := f.svc.CreateInvoice(ctx, school.ID, "", CreateInvoiceInput{
		LearnerID: learner, DueDate: "2026-12-31",
		Lines: []InvoiceLineInput{{AmountMinor: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	voided, err := f.svc.VoidInvoice(ctx, school.ID, inv2.ID)
	if err != nil || voided.Status != InvoiceVoid {
		t.Fatalf("void: %v %s", err, voided.Status)
	}
}

func TestFinanceTenantIsolation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sA := mustSchool(t, f, "FIN-A", "Fin A")
	sB := mustSchool(t, f, "FIN-B", "Fin B")
	accA := mustAccount(t, f, sA.ID, "bank")

	// School B cannot resolve school A's account or entries.
	if _, err := f.svc.repo.AccountByID(ctx, sB.ID, accA.ID); err == nil {
		t.Fatal("school B resolved school A account")
	}
	e, err := f.svc.PostEntry(ctx, sA.ID, "", "a entry", "", "", "",
		[]LineInput{
			{AccountID: accA.ID, DebitMinor: 10},
			{AccountID: mustAccount(t, f, sA.ID, "fees_receivable").ID, CreditMinor: 10},
		})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Entry(ctx, sB.ID, e.ID); err == nil {
		t.Fatal("school B resolved school A journal entry")
	}
	// Posting with school A's account under school B rejected.
	if _, err := f.svc.PostEntry(ctx, sB.ID, "", "steal", "", "", "",
		[]LineInput{
			{AccountID: accA.ID, DebitMinor: 10},
			{AccountID: mustAccount(t, f, sB.ID, "fees_receivable").ID, CreditMinor: 10},
		}); err == nil {
		t.Fatal("cross-school posting accepted")
	}
	// Wallets are isolated: school B wallet has zero balances.
	wb, err := f.svc.Wallet(ctx, sB.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range wb {
		if w.BalanceMinor != 0 {
			t.Fatalf("school B wallet %s nonzero: %d", w.Purpose, w.BalanceMinor)
		}
	}
}

func TestFinanceHTTPFlow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.auth.CreateUser(ctx, "fin-admin@skolara.test", "Fin Admin", "s3cure-passw0rd!", identity.RolePlatformAdmin); err != nil {
		t.Fatal(err)
	}
	token, _, err := f.auth.Login(ctx, "fin-admin@skolara.test", "s3cure-passw0rd!", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	school := mustSchool(t, f, "FIN-H", "HTTP Finance")
	learner := seedLearner(t, f, "Mia", "Thirteen")

	// Authed API routes live behind the auth/school wrapper; the webhook is
	// PUBLIC (HMAC-authenticated) and mounted outside the wrapper — exactly
	// like the composition root does.
	apiMux := http.NewServeMux()
	NewHandler(f.svc).Register(apiMux, f.jwt, f.auth)
	root := http.NewServeMux()
	NewHandler(f.svc).RegisterWebhook(root)
	root.Handle("/api/v1/", identity.RequireAuth(f.jwt, tenancy.RequireSchool(f.tenancy, apiMux)))

	do := func(method, path string, body any, withSchool, signIt bool) *httptest.ResponseRecorder {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		} else {
			raw = []byte{}
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		if withSchool || !strings.Contains(path, "/webhooks") {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if withSchool {
			req.Header.Set("X-School-ID", school.ID)
		}
		if signIt {
			req.Header.Set("X-Skolar-Signature", sign(t, f, raw))
		}
		rr := httptest.NewRecorder()
		root.ServeHTTP(rr, req)
		return rr
	}

	// Invoice: 201.
	rr := do("POST", "/api/v1/invoices", map[string]any{
		"learnerId": learner, "dueDate": "2026-12-31",
		"lines": []map[string]any{{"description": "Tuition", "amountMinor": 500_000}},
	}, true, false)
	if rr.Code != http.StatusCreated {
		t.Fatalf("invoice: %d %s", rr.Code, rr.Body.String())
	}
	var inv Invoice
	_ = json.Unmarshal(rr.Body.Bytes(), &inv)

	// Payment intent: 201.
	rr = do("POST", "/api/v1/payments", map[string]any{
		"invoiceId": inv.ID, "amountMinor": 500_000, "provider": "mpesa", "providerRef": "MPX-2001",
	}, true, false)
	if rr.Code != http.StatusCreated {
		t.Fatalf("payment: %d %s", rr.Code, rr.Body.String())
	}
	var p Payment
	_ = json.Unmarshal(rr.Body.Bytes(), &p)

	// Webhook WITHOUT signature: 401.
	rr = do("POST", "/api/v1/payments/webhooks", map[string]any{
		"schoolId": school.ID, "eventId": "h-evt-1", "paymentId": p.ID, "status": "confirmed",
	}, false, false)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned webhook: %d", rr.Code)
	}

	// Webhook WITH signature: 200 confirmed.
	rr = do("POST", "/api/v1/payments/webhooks", map[string]any{
		"schoolId": school.ID, "eventId": "h-evt-1", "paymentId": p.ID, "status": "confirmed",
	}, false, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("signed webhook: %d %s", rr.Code, rr.Body.String())
	}

	// Invoice now paid; wallet endpoint reflects the inflow.
	rr = do("GET", "/api/v1/invoices/"+inv.ID, nil, true, false)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"paid"`)) {
		t.Fatalf("invoice read: %d %s", rr.Code, rr.Body.String())
	}
	rr = do("GET", "/api/v1/wallet", nil, true, false)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte("500000")) {
		t.Fatalf("wallet: %d %s", rr.Code, rr.Body.String())
	}

	// Bad signature: 401.
	rr = do("POST", "/api/v1/payments/webhooks", map[string]any{
		"schoolId": school.ID, "eventId": "h-evt-2", "paymentId": p.ID, "status": "failed",
	}, false, false)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature: %d", rr.Code)
	}

	// Ledger accounts listing: 200.
	rr = do("GET", "/api/v1/ledger/accounts", nil, true, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("accounts: %d", rr.Code)
	}
}
