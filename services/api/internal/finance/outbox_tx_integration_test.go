//go:build integration

package finance

import (
	"context"
	"testing"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
)

// outboxEventCountForSchool counts outbox rows of a type scoped to a school.
func outboxEventCountForSchool(t *testing.T, pool *postgres.Pool, eventType, schoolID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM event_outbox WHERE event_type = $1 AND school_id = $2`, eventType, schoolID).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

// hideEventOutbox makes the outbox temporarily unwritable so the event insert
// inside a mutation transaction fails — forcing the rollback the test asserts on.
func hideEventOutbox(t *testing.T, pool *postgres.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `ALTER TABLE event_outbox RENAME TO event_outbox_hidden`); err != nil {
		t.Fatalf("hide outbox: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `ALTER TABLE event_outbox_hidden RENAME TO event_outbox`)
	})
}

func restoreEventOutbox(t *testing.T, pool *postgres.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `ALTER TABLE event_outbox_hidden RENAME TO event_outbox`); err != nil {
		t.Fatalf("restore outbox: %v", err)
	}
}

// TestFinanceMutationAndEventShareTransaction proves ADR-003 at the finance
// call sites (issue #52): the invoice write and its outbox event commit or
// roll back as ONE transaction. ConfirmWebhook — the pre-existing in-tx
// reference pattern — is exercised too: a forced event failure must undo the
// confirmation, the idempotency claim included, so the provider retry with
// the SAME webhook event id succeeds afterwards.
func TestFinanceMutationAndEventShareTransaction(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	school := mustSchool(t, f, "OBX-F", "Outbox Finance School")
	learner := seedLearner(t, f, "Ria", "One")
	due := time.Now().UTC().Add(72 * time.Hour).Format("2006-01-02")

	// Success: the invoice and its event both land.
	inv, err := f.svc.CreateInvoice(ctx, school.ID, "", CreateInvoiceInput{
		LearnerID: learner,
		DueDate:   due,
		Lines:     []InvoiceLineInput{{Description: "Tuition term 1", AmountMinor: 500_000}},
	})
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}
	if got := outboxEventCountForSchool(t, f.pool, "finance.InvoiceCreated", school.ID); got != 1 {
		t.Fatalf("InvoiceCreated events %d, want 1", got)
	}

	// Pending payment for the webhook leg.
	p, err := f.svc.CreatePayment(ctx, school.ID, CreatePaymentInput{
		InvoiceID: inv.ID, PayerRef: "guardian-obx", AmountMinor: 500_000,
		Provider: "mpesa", ProviderRef: "MPX-OBX-1",
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}

	// Event failure: with the outbox unwritable the whole transaction must
	// roll back — the invoice may NOT survive without its event (the dual
	// write bug fixed in #52).
	hideEventOutbox(t, f.pool)
	if _, err := f.svc.CreateInvoice(ctx, school.ID, "", CreateInvoiceInput{
		LearnerID: learner,
		DueDate:   due,
		Lines:     []InvoiceLineInput{{Description: "Rollback probe", AmountMinor: 100}},
	}); err == nil {
		t.Fatal("invoice succeeded while outbox was unwritable")
	}
	var invoices int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM invoices`).Scan(&invoices); err != nil {
		t.Fatal(err)
	}
	if invoices != 1 {
		t.Fatalf("invoices after forced rollback %d, want 1 (mutation leaked)", invoices)
	}

	// ConfirmWebhook must equally roll back: payment stays pending and the
	// idempotency claim is undone together with the events.
	if _, err := f.svc.ConfirmWebhook(ctx, school.ID, ConfirmWebhookInput{
		EventID: "evt-obx-1", PaymentID: p.ID, Status: "confirmed", ProviderRef: "MPX-OBX-1",
	}); err == nil {
		t.Fatal("webhook confirm succeeded while outbox was unwritable")
	}
	pm, err := f.svc.Payment(ctx, school.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pm.Status != "pending" {
		t.Fatalf("payment status %q after forced rollback, want pending", pm.Status)
	}
	restoreEventOutbox(t, f.pool)

	// Restored: retrying with the SAME webhook event id succeeds — proof that
	// the idempotency claim, ledger postings, invoice status and events share
	// one transaction (nothing half-applied survived the rollback).
	if _, err := f.svc.ConfirmWebhook(ctx, school.ID, ConfirmWebhookInput{
		EventID: "evt-obx-1", PaymentID: p.ID, Status: "confirmed", ProviderRef: "MPX-OBX-1",
	}); err != nil {
		t.Fatalf("confirm after restore (same event id): %v", err)
	}
	if got := outboxEventCountForSchool(t, f.pool, "finance.PaymentConfirmed", school.ID); got != 1 {
		t.Fatalf("PaymentConfirmed events %d, want 1", got)
	}
	if got := outboxEventCountForSchool(t, f.pool, "finance.ReceiptIssued", school.ID); got != 1 {
		t.Fatalf("ReceiptIssued events %d, want 1", got)
	}
}
