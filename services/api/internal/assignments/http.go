package assignments

import (
	"errors"
	"net/http"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/httpx"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/observability"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
)

// Handler exposes assignments endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register wires routes. Create/publish/close/grade are assignment.manage;
// reads are assignment.read; submissions POST is manage (recorded by staff on
// behalf of learners until identity↔learner linking lands).
func (h *Handler) Register(mux *http.ServeMux, jwt *identity.JWTManager, resolver identity.PermissionResolver) {
	observability.Register(mux, "POST /api/v1/assignments",
		identity.RequirePermission(jwt, resolver, identity.PermAssignmentManage, h.schoolScoped(h.create)))
	observability.Register(mux, "GET /api/v1/assignments",
		identity.RequirePermission(jwt, resolver, identity.PermAssignmentRead, h.schoolScoped(h.list)))
	observability.Register(mux, "GET /api/v1/assignments/{id}",
		identity.RequirePermission(jwt, resolver, identity.PermAssignmentRead, h.schoolScoped(h.get)))
	observability.Register(mux, "POST /api/v1/assignments/{id}/publish",
		identity.RequirePermission(jwt, resolver, identity.PermAssignmentManage, h.schoolScoped(h.publish)))
	observability.Register(mux, "POST /api/v1/assignments/{id}/close",
		identity.RequirePermission(jwt, resolver, identity.PermAssignmentManage, h.schoolScoped(h.close)))
	observability.Register(mux, "POST /api/v1/assignments/{id}/submissions",
		identity.RequirePermission(jwt, resolver, identity.PermAssignmentManage, h.schoolScoped(h.submit)))
	observability.Register(mux, "GET /api/v1/assignments/{id}/submissions",
		identity.RequirePermission(jwt, resolver, identity.PermAssignmentRead, h.schoolScoped(h.listSubmissions)))
	observability.Register(mux, "POST /api/v1/assignments/{id}/submissions/{learnerId}/grade",
		identity.RequirePermission(jwt, resolver, identity.PermAssignmentManage, h.schoolScoped(h.grade)))
	observability.Register(mux, "POST /api/v1/assignments/{id}/submissions/{learnerId}/return",
		identity.RequirePermission(jwt, resolver, identity.PermAssignmentManage, h.schoolScoped(h.returnSubmission)))
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

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClassGroupID string `json:"classGroupId"`
		SubjectID    string `json:"subjectId"`
		Title        string `json:"title"`
		Instructions string `json:"instructions"`
		DueDate      string `json:"dueDate"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	a, err := h.svc.CreateAssignment(r.Context(), tenancy.SchoolFrom(r.Context()),
		identity.ClaimsFrom(r.Context()).UserID, CreateAssignmentInput{
			ClassGroupID: req.ClassGroupID,
			SubjectID:    req.SubjectID,
			Title:        req.Title,
			Instructions: req.Instructions,
			DueDate:      req.DueDate,
		})
	if err != nil {
		h.writeDomainError(w, r, err, "create assignment")
		return
	}
	httpx.JSON(w, http.StatusCreated, a)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := httpx.Pagination(r)
	var status *AssignmentStatus
	if raw := r.URL.Query().Get("status"); raw != "" {
		s := AssignmentStatus(raw)
		if s != AssignmentDraft && s != AssignmentPublished && s != AssignmentClosed {
			httpx.BadRequest(w, "unknown assignment status: "+raw)
			return
		}
		status = &s
	}
	assignments, total, err := h.svc.Assignments(r.Context(), tenancy.SchoolFrom(r.Context()),
		r.URL.Query().Get("classGroupId"), status, limit, offset)
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list assignments", err)
		return
	}
	w.Header().Set("X-Total-Count", itoa(total))
	httpx.JSON(w, http.StatusOK, map[string]any{
		"assignments": assignments, "total": total, "limit": limit, "offset": offset,
	})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.Assignment(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		httpx.NotFound(w, "assignment not found")
		return
	}
	httpx.JSON(w, http.StatusOK, a)
}

func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.PublishAssignment(r.Context(), tenancy.SchoolFrom(r.Context()),
		identity.ClaimsFrom(r.Context()).UserID, r.PathValue("id"))
	if err != nil {
		h.writeDomainError(w, r, err, "publish assignment")
		return
	}
	httpx.JSON(w, http.StatusOK, a)
}

func (h *Handler) close(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.CloseAssignment(r.Context(), tenancy.SchoolFrom(r.Context()),
		identity.ClaimsFrom(r.Context()).UserID, r.PathValue("id"))
	if err != nil {
		h.writeDomainError(w, r, err, "close assignment")
		return
	}
	httpx.JSON(w, http.StatusOK, a)
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LearnerID string `json:"learnerId"`
		Content   string `json:"content"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	sub, err := h.svc.SubmitAssignment(r.Context(), tenancy.SchoolFrom(r.Context()),
		r.PathValue("id"), req.LearnerID, req.Content)
	if err != nil {
		h.writeDomainError(w, r, err, "submit assignment")
		return
	}
	httpx.JSON(w, http.StatusCreated, sub)
}

func (h *Handler) listSubmissions(w http.ResponseWriter, r *http.Request) {
	subs, err := h.svc.Submissions(r.Context(), tenancy.SchoolFrom(r.Context()),
		identity.ClaimsFrom(r.Context()).UserID, r.PathValue("id"))
	if err != nil {
		h.writeDomainError(w, r, err, "list submissions")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"submissions": subs})
}

func (h *Handler) grade(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Grade    string `json:"grade"`
		Feedback string `json:"feedback"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	sub, err := h.svc.GradeAssignment(r.Context(), tenancy.SchoolFrom(r.Context()),
		identity.ClaimsFrom(r.Context()).UserID, r.PathValue("id"), r.PathValue("learnerId"), req.Grade, req.Feedback)
	if err != nil {
		h.writeDomainError(w, r, err, "grade submission")
		return
	}
	httpx.JSON(w, http.StatusOK, sub)
}

func (h *Handler) returnSubmission(w http.ResponseWriter, r *http.Request) {
	sub, err := h.svc.ReturnAssignment(r.Context(), tenancy.SchoolFrom(r.Context()),
		identity.ClaimsFrom(r.Context()).UserID, r.PathValue("id"), r.PathValue("learnerId"))
	if err != nil {
		h.writeDomainError(w, r, err, "return submission")
		return
	}
	httpx.JSON(w, http.StatusOK, sub)
}

func (h *Handler) writeDomainError(w http.ResponseWriter, r *http.Request, err error, action string) {
	switch {
	case errors.Is(err, ErrValidation), errors.Is(err, ErrDueDatePast), errors.Is(err, ErrGradeRequired):
		httpx.BadRequest(w, err.Error())
	case errors.Is(err, ErrNotFound):
		// 404 (not 403) to avoid tenant enumeration (ADR-006).
		httpx.NotFound(w, "resource not found")
	case errors.Is(err, ErrNotOwner):
		httpx.Forbidden(w, err.Error())
	case errors.Is(err, ErrIllegalAssignmentTransition), errors.Is(err, ErrNotOpen),
		errors.Is(err, ErrAlreadyGraded), errors.Is(err, ErrNotRostered):
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
