package attendance

import (
	"errors"
	"net/http"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/httpx"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/observability"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
)

// Handler exposes attendance endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register wires routes. Writes require attendance.record; reads
// attendance.read. All routes resolve the tenant school via
// tenancy.RequireSchool at the composition root.
func (h *Handler) Register(mux *http.ServeMux, jwt *identity.JWTManager, resolver identity.PermissionResolver) {
	observability.Register(mux, "POST /api/v1/attendance/sessions",
		identity.RequirePermission(jwt, resolver, identity.PermAttendanceRecord, h.schoolScoped(h.createSession)))
	observability.Register(mux, "GET /api/v1/attendance/sessions",
		identity.RequirePermission(jwt, resolver, identity.PermAttendanceRead, h.schoolScoped(h.listSessions)))
	observability.Register(mux, "POST /api/v1/attendance/sessions/{id}/records",
		identity.RequirePermission(jwt, resolver, identity.PermAttendanceRecord, h.schoolScoped(h.submitRecords)))
	observability.Register(mux, "GET /api/v1/attendance/sessions/{id}/records",
		identity.RequirePermission(jwt, resolver, identity.PermAttendanceRead, h.schoolScoped(h.listRecords)))
	observability.Register(mux, "POST /api/v1/attendance/sessions/{id}/close",
		identity.RequirePermission(jwt, resolver, identity.PermAttendanceRecord, h.schoolScoped(h.closeSession)))
}

// schoolScoped rejects requests without a resolved tenant school before any
// domain work.
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

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClassGroupID string `json:"classGroupId"`
		Date         string `json:"date"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	sess, err := h.svc.OpenSession(r.Context(), tenancy.SchoolFrom(r.Context()), req.ClassGroupID, req.Date, identity.ClaimsFrom(r.Context()).UserID)
	if err != nil {
		h.writeDomainError(w, r, err, "create session")
		return
	}
	// 200 even when pre-existing: offline retries converge (documented).
	httpx.JSON(w, http.StatusOK, sess)
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	limit, offset := httpx.Pagination(r)
	classGroupID, ok := httpx.QueryUUID(w, r, "classGroupId")
	if !ok {
		return
	}
	date, ok := httpx.QueryDate(w, r, "date")
	if !ok {
		return
	}
	sessions, total, err := h.svc.Sessions(r.Context(), tenancy.SchoolFrom(r.Context()),
		classGroupID, date, limit, offset)
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list sessions", err)
		return
	}
	w.Header().Set("X-Total-Count", itoa(total))
	httpx.JSON(w, http.StatusOK, map[string]any{
		"sessions": sessions, "total": total, "limit": limit, "offset": offset,
	})
}

func (h *Handler) submitRecords(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Records []RecordInput `json:"records"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	err := h.svc.SubmitRecords(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"),
		identity.ClaimsFrom(r.Context()).UserID, req.Records)
	if err != nil {
		h.writeDomainError(w, r, err, "submit records")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listRecords(w http.ResponseWriter, r *http.Request) {
	records, err := h.svc.Records(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		h.writeDomainError(w, r, err, "list records")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"records": records})
}

func (h *Handler) closeSession(w http.ResponseWriter, r *http.Request) {
	sess, err := h.svc.CloseSession(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		h.writeDomainError(w, r, err, "close session")
		return
	}
	httpx.JSON(w, http.StatusOK, sess)
}

func (h *Handler) writeDomainError(w http.ResponseWriter, r *http.Request, err error, action string) {
	switch {
	case errors.Is(err, ErrValidation), errors.Is(err, ErrMutationUsed):
		httpx.BadRequest(w, err.Error())
	case errors.Is(err, ErrNotFound):
		// 404 (not 403) to avoid tenant enumeration (ADR-006).
		httpx.NotFound(w, "resource not found")
	case errors.Is(err, ErrSessionClosed):
		httpx.Conflict(w, err.Error())
	default:
		httpx.Internal(w, nil, r.Context(), action, err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
