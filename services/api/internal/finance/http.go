package finance

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/httpx"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/observability"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
)

// Handler exposes finance endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register wires routes. Writes are finance.manage; reads finance.read. The
// webhook route is PUBLIC — it authenticates via HMAC signature (§30) and is
// registered separately in RegisterWebhook.
func (h *Handler) Register(mux *http.ServeMux, jwt *identity.JWTManager, resolver identity.PermissionResolver) {
	observability.Register(mux, "POST /api/v1/ledger/accounts",
		identity.RequirePermission(jwt, resolver, identity.PermFinanceManage, h.schoolScoped(h.createAccount)))
	observability.Register(mux, "GET /api/v1/ledger/accounts",
		identity.RequirePermission(jwt, resolver, identity.PermFinanceRead, h.schoolScoped(h.listAccounts)))
	observability.Register(mux, "POST /api/v1/journal-entries",
		identity.RequirePermission(jwt, resolver, identity.PermFinanceManage, h.schoolScoped(h.postEntry)))
	observability.Register(mux, "GET /api/v1/journal-entries/{id}",
		identity.RequirePermission(jwt, resolver, identity.PermFinanceRead, h.schoolScoped(h.getEntry)))

	observability.Register(mux, "POST /api/v1/fee-structures",
		identity.RequirePermission(jwt, resolver, identity.PermFinanceManage, h.schoolScoped(h.createFeeStructure)))
	observability.Register(mux, "GET /api/v1/fee-structures",
		identity.RequirePermission(jwt, resolver, identity.PermFinanceRead, h.schoolScoped(h.listFeeStructures)))

	observability.Register(mux, "POST /api/v1/invoices",
		identity.RequirePermission(jwt, resolver, identity.PermFinanceManage, h.schoolScoped(h.createInvoice)))
	observability.Register(mux, "GET /api/v1/invoices",
		identity.RequirePermission(jwt, resolver, identity.PermFinanceRead, h.schoolScoped(h.listInvoices)))
	observability.Register(mux, "GET /api/v1/invoices/{id}",
		identity.RequirePermission(jwt, resolver, identity.PermFinanceRead, h.schoolScoped(h.getInvoice)))
	observability.Register(mux, "POST /api/v1/invoices/{id}/void",
		identity.RequirePermission(jwt, resolver, identity.PermFinanceManage, h.schoolScoped(h.voidInvoice)))

	observability.Register(mux, "POST /api/v1/payments",
		identity.RequirePermission(jwt, resolver, identity.PermFinanceManage, h.schoolScoped(h.createPayment)))
	observability.Register(mux, "GET /api/v1/wallet",
		identity.RequirePermission(jwt, resolver, identity.PermFinanceRead, h.schoolScoped(h.wallet)))
}

// RegisterWebhook mounts the PUBLIC payment webhook endpoint. It must be
// attached to the mux BEFORE authentication middleware so provider calls
// without bearer tokens reach it; it authenticates via HMAC instead.
func (h *Handler) RegisterWebhook(mux *http.ServeMux) {
	observability.Register(mux, "POST /api/v1/payments/webhooks", http.HandlerFunc(h.webhook))
}

func (h *Handler) schoolScoped(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tenancy.SchoolFrom(r.Context()) == "" {
			httpx.WriteError(w, http.StatusForbidden, "no_school_context",
				"no active school context: pass X-School-ID of a school you belong to")
			return
		}
		next(w, r)
	})
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code    string `json:"code"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Purpose string `json:"purpose"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	a, err := h.svc.CreateAccount(r.Context(), tenancy.SchoolFrom(r.Context()), req.Code, req.Name, AccountType(req.Type), req.Purpose)
	if err != nil {
		h.writeDomainError(w, r, err, "create account")
		return
	}
	httpx.JSON(w, http.StatusCreated, a)
}

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.svc.Accounts(r.Context(), tenancy.SchoolFrom(r.Context()))
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list accounts", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"accounts": accounts})
}

func (h *Handler) postEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description    string      `json:"description"`
		EntryDate      string      `json:"entryDate"`
		IdempotencyKey string      `json:"idempotencyKey"`
		CorrectionOf   string      `json:"correctionOf"`
		Lines          []LineInput `json:"lines"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	e, err := h.svc.PostEntry(r.Context(), tenancy.SchoolFrom(r.Context()),
		identity.ClaimsFrom(r.Context()).UserID, req.Description, req.EntryDate,
		req.IdempotencyKey, req.CorrectionOf, req.Lines)
	if err != nil {
		h.writeDomainError(w, r, err, "post journal entry")
		return
	}
	httpx.JSON(w, http.StatusCreated, e)
}

func (h *Handler) getEntry(w http.ResponseWriter, r *http.Request) {
	e, err := h.svc.Entry(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		httpx.NotFound(w, "journal entry not found")
		return
	}
	httpx.JSON(w, http.StatusOK, e)
}

func (h *Handler) createFeeStructure(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string  `json:"name"`
		ClassGroupID   *string `json:"classGroupId"`
		AcademicYearID *string `json:"academicYearId"`
		TermID         *string `json:"termId"`
		AmountMinor    int64   `json:"amountMinor"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	fs, err := h.svc.CreateFeeStructure(r.Context(), tenancy.SchoolFrom(r.Context()),
		req.Name, req.ClassGroupID, req.AcademicYearID, req.TermID, req.AmountMinor)
	if err != nil {
		h.writeDomainError(w, r, err, "create fee structure")
		return
	}
	httpx.JSON(w, http.StatusCreated, fs)
}

func (h *Handler) listFeeStructures(w http.ResponseWriter, r *http.Request) {
	fees, err := h.svc.FeeStructures(r.Context(), tenancy.SchoolFrom(r.Context()))
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list fee structures", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"feeStructures": fees})
}

func (h *Handler) createInvoice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LearnerID      string             `json:"learnerId"`
		FeeStructureID string             `json:"feeStructureId"`
		TermID         string             `json:"termId"`
		DueDate        string             `json:"dueDate"`
		Lines          []InvoiceLineInput `json:"lines"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	inv, err := h.svc.CreateInvoice(r.Context(), tenancy.SchoolFrom(r.Context()),
		identity.ClaimsFrom(r.Context()).UserID, CreateInvoiceInput{
			LearnerID:      req.LearnerID,
			FeeStructureID: req.FeeStructureID,
			TermID:         req.TermID,
			DueDate:        req.DueDate,
			Lines:          req.Lines,
		})
	if err != nil {
		h.writeDomainError(w, r, err, "create invoice")
		return
	}
	httpx.JSON(w, http.StatusCreated, inv)
}

func (h *Handler) listInvoices(w http.ResponseWriter, r *http.Request) {
	limit, offset := httpx.Pagination(r)
	var status *InvoiceStatus
	if raw := r.URL.Query().Get("status"); raw != "" {
		s := InvoiceStatus(raw)
		switch s {
		case InvoiceOpen, InvoicePartiallyPaid, InvoicePaid, InvoiceVoid:
			status = &s
		default:
			httpx.BadRequest(w, "unknown invoice status: "+raw)
			return
		}
	}
	invoices, total, err := h.svc.Invoices(r.Context(), tenancy.SchoolFrom(r.Context()),
		status, r.URL.Query().Get("learnerId"), limit, offset)
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list invoices", err)
		return
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	httpx.JSON(w, http.StatusOK, map[string]any{
		"invoices": invoices, "total": total, "limit": limit, "offset": offset,
	})
}

func (h *Handler) getInvoice(w http.ResponseWriter, r *http.Request) {
	inv, err := h.svc.Invoice(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		httpx.NotFound(w, "invoice not found")
		return
	}
	httpx.JSON(w, http.StatusOK, inv)
}

func (h *Handler) voidInvoice(w http.ResponseWriter, r *http.Request) {
	inv, err := h.svc.VoidInvoice(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		h.writeDomainError(w, r, err, "void invoice")
		return
	}
	httpx.JSON(w, http.StatusOK, inv)
}

func (h *Handler) createPayment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InvoiceID   string `json:"invoiceId"`
		PayerRef    string `json:"payerRef"`
		AmountMinor int64  `json:"amountMinor"`
		Provider    string `json:"provider"`
		ProviderRef string `json:"providerRef"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	p, err := h.svc.CreatePayment(r.Context(), tenancy.SchoolFrom(r.Context()), CreatePaymentInput{
		InvoiceID:   req.InvoiceID,
		PayerRef:    req.PayerRef,
		AmountMinor: req.AmountMinor,
		Provider:    req.Provider,
		ProviderRef: req.ProviderRef,
	})
	if err != nil {
		h.writeDomainError(w, r, err, "create payment")
		return
	}
	httpx.JSON(w, http.StatusCreated, p)
}

func (h *Handler) wallet(w http.ResponseWriter, r *http.Request) {
	balances, err := h.svc.Wallet(r.Context(), tenancy.SchoolFrom(r.Context()))
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "wallet", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"wallet": balances})
}

// webhook handles POST /api/v1/payments/webhooks — PUBLIC, HMAC-authenticated.
// Signature: X-Skolar-Signature: hex(HMAC-SHA256(raw_body, webhook_secret)).
// Replay protection: provider event id keys idempotent processing.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	const maxWebhookBody = 1 << 16 // 64 KiB is generous for a payment event
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		httpx.BadRequest(w, "unreadable body")
		return
	}
	sig := r.Header.Get("X-Skolar-Signature")
	if !h.verifySignature(body, sig) {
		// 401 without detail — do not leak verification specifics.
		httpx.Unauthorized(w, "invalid signature")
		return
	}
	var req struct {
		SchoolID    string `json:"schoolId"`
		EventID     string `json:"eventId"`
		PaymentID   string `json:"paymentId"`
		Status      string `json:"status"`
		ProviderRef string `json:"providerRef"`
	}
	// The body was consumed for signature verification — decode from memory
	// (strictly, matching DecodeJSON semantics).
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpx.BadRequest(w, "invalid request body")
		return
	}
	if req.SchoolID == "" || req.EventID == "" || req.PaymentID == "" {
		httpx.BadRequest(w, "schoolId, eventId and paymentId are required")
		return
	}
	var out map[string]any
	switch req.Status {
	case "confirmed":
		out, err = h.svc.ConfirmWebhook(r.Context(), req.SchoolID, ConfirmWebhookInput{
			EventID:     req.EventID,
			PaymentID:   req.PaymentID,
			Status:      req.Status,
			ProviderRef: req.ProviderRef,
		})
	case "failed":
		out, err = h.svc.FailWebhook(r.Context(), req.SchoolID, req.EventID, req.PaymentID)
	default:
		httpx.BadRequest(w, "status must be confirmed or failed")
		return
	}
	if err != nil {
		h.writeDomainError(w, r, err, "webhook")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) verifySignature(body []byte, sig string) bool {
	if h.svc.webhookSecret == "" || sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.svc.webhookSecret))
	mac.Write(body)
	expected := mac.Sum(nil)
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, expected) == 1
}

func (h *Handler) writeDomainError(w http.ResponseWriter, r *http.Request, err error, action string) {
	switch {
	case errors.Is(err, ErrValidation), errors.Is(err, ErrUnbalanced), errors.Is(err, ErrInvoiceNoLines):
		httpx.BadRequest(w, err.Error())
	case errors.Is(err, ErrNotFound):
		// 404 (not 403) to avoid tenant enumeration (ADR-006).
		httpx.NotFound(w, "resource not found")
	case errors.Is(err, ErrIdempotencyUsed), errors.Is(err, ErrNotAllocatable), errors.Is(err, ErrOverpay):
		httpx.Conflict(w, err.Error())
	default:
		httpx.Internal(w, nil, r.Context(), action, err)
	}
}
