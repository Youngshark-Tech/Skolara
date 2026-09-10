package students

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/httpx"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/observability"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
)

// Handler exposes students endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register wires routes. jwt + resolver come from the identity context and
// the tenant school comes from tenancy.SchoolFrom (students → identity/tenancy,
// one-directional like tenancy → identity — never the reverse).
func (h *Handler) Register(mux *http.ServeMux, jwt *identity.JWTManager, resolver identity.PermissionResolver) {
	observability.Register(mux, "POST /api/v1/learners",
		identity.RequirePermission(jwt, resolver, identity.PermStudentManage, h.schoolScoped(h.createLearner)))
	observability.Register(mux, "GET /api/v1/learners",
		identity.RequirePermission(jwt, resolver, identity.PermStudentRead, h.schoolScoped(h.listLearners)))
	observability.Register(mux, "GET /api/v1/learners/{id}",
		identity.RequirePermission(jwt, resolver, identity.PermStudentRead, h.schoolScoped(h.getLearner)))

	observability.Register(mux, "POST /api/v1/guardians",
		identity.RequirePermission(jwt, resolver, identity.PermStudentManage, h.schoolScoped(h.createGuardian)))
	observability.Register(mux, "POST /api/v1/learners/{id}/guardians",
		identity.RequirePermission(jwt, resolver, identity.PermStudentManage, h.schoolScoped(h.linkGuardian)))
	observability.Register(mux, "GET /api/v1/learners/{id}/guardians",
		identity.RequirePermission(jwt, resolver, identity.PermStudentRead, h.schoolScoped(h.listGuardians)))

	observability.Register(mux, "POST /api/v1/enrollments",
		identity.RequirePermission(jwt, resolver, identity.PermStudentManage, h.schoolScoped(h.createEnrollment)))
	observability.Register(mux, "GET /api/v1/enrollments",
		identity.RequirePermission(jwt, resolver, identity.PermStudentRead, h.schoolScoped(h.listEnrollments)))
	observability.Register(mux, "POST /api/v1/enrollments/{id}/transition",
		identity.RequirePermission(jwt, resolver, identity.PermStudentManage, h.schoolScoped(h.transitionEnrollment)))
}

// schoolScoped rejects requests without a resolved tenant school (injected by
// tenancy.RequireSchool at the composition root) before any domain work.
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

func (h *Handler) createLearner(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FirstName   string     `json:"firstName"`
		LastName    string     `json:"lastName"`
		MiddleName  string     `json:"middleName"`
		DateOfBirth *time.Time `json:"dateOfBirth"`
		Gender      string     `json:"gender"`
		ExternalID  string     `json:"externalId"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	learner, err := h.svc.CreateLearner(r.Context(), tenancy.SchoolFrom(r.Context()), LearnerInput{
		FirstName:   req.FirstName,
		LastName:    req.LastName,
		MiddleName:  req.MiddleName,
		DateOfBirth: req.DateOfBirth,
		Gender:      req.Gender,
		ExternalID:  req.ExternalID,
	})
	if err != nil {
		h.writeDomainError(w, r, err, "create learner")
		return
	}
	httpx.JSON(w, http.StatusCreated, learner)
}

func (h *Handler) listLearners(w http.ResponseWriter, r *http.Request) {
	limit, offset := httpx.Pagination(r)
	learners, total, err := h.svc.ListLearners(r.Context(), tenancy.SchoolFrom(r.Context()), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list learners", err)
		return
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	httpx.JSON(w, http.StatusOK, map[string]any{
		"learners": learners, "total": total, "limit": limit, "offset": offset,
	})
}

func (h *Handler) getLearner(w http.ResponseWriter, r *http.Request) {
	learner, err := h.svc.LearnerByID(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// 404 (not 403) to avoid tenant enumeration (ADR-006).
			httpx.NotFound(w, "learner not found")
		} else {
			httpx.Internal(w, nil, r.Context(), "get learner", err)
		}
		return
	}
	httpx.JSON(w, http.StatusOK, learner)
}

func (h *Handler) createGuardian(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Phone     string `json:"phone"`
		Email     string `json:"email"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	guardian, err := h.svc.CreateGuardian(r.Context(), tenancy.SchoolFrom(r.Context()), GuardianInput{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Phone:     req.Phone,
		Email:     req.Email,
	})
	if err != nil {
		h.writeDomainError(w, r, err, "create guardian")
		return
	}
	httpx.JSON(w, http.StatusCreated, guardian)
}

func (h *Handler) linkGuardian(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GuardianID        string `json:"guardianId"`
		Relationship      string `json:"relationship"`
		IsPrimary         bool   `json:"isPrimary"`
		CanViewFinancials bool   `json:"canViewFinancials"`
		CanViewAcademics  bool   `json:"canViewAcademics"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	link, err := h.svc.LinkGuardian(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"), LinkInput{
		GuardianID:        req.GuardianID,
		Relationship:      req.Relationship,
		IsPrimary:         req.IsPrimary,
		CanViewFinancials: req.CanViewFinancials,
		CanViewAcademics:  req.CanViewAcademics,
	})
	if err != nil {
		h.writeDomainError(w, r, err, "link guardian")
		return
	}
	httpx.JSON(w, http.StatusCreated, link)
}

func (h *Handler) listGuardians(w http.ResponseWriter, r *http.Request) {
	views, err := h.svc.GuardiansForLearner(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.NotFound(w, "learner not found")
			return
		}
		httpx.Internal(w, nil, r.Context(), "list guardians", err)
		return
	}
	httpx.JSON(w, http.StatusOK, views)
}

func (h *Handler) createEnrollment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LearnerID      string `json:"learnerId"`
		ClassGroupID   string `json:"classGroupId"`
		AcademicYearID string `json:"academicYearId"`
		Status         string `json:"status"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	enrollment, err := h.svc.EnrollLearner(r.Context(), tenancy.SchoolFrom(r.Context()), EnrollmentInput{
		LearnerID:      req.LearnerID,
		ClassGroupID:   req.ClassGroupID,
		AcademicYearID: req.AcademicYearID,
		Status:         req.Status,
	})
	if err != nil {
		h.writeDomainError(w, r, err, "create enrollment")
		return
	}
	httpx.JSON(w, http.StatusCreated, enrollment)
}

func (h *Handler) listEnrollments(w http.ResponseWriter, r *http.Request) {
	var status *EnrollmentStatus
	if raw := r.URL.Query().Get("status"); raw != "" {
		s := EnrollmentStatus(raw)
		if !ValidEnrollmentStatus(s) {
			httpx.BadRequest(w, "unknown enrollment status: "+raw)
			return
		}
		status = &s
	}
	limit, offset := httpx.Pagination(r)
	enrollments, total, err := h.svc.ListEnrollments(r.Context(), tenancy.SchoolFrom(r.Context()), status, limit, offset)
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list enrollments", err)
		return
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	httpx.JSON(w, http.StatusOK, map[string]any{
		"enrollments": enrollments, "total": total, "limit": limit, "offset": offset,
	})
}

func (h *Handler) transitionEnrollment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To string `json:"to"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	enrollment, err := h.svc.TransitionEnrollment(r.Context(), tenancy.SchoolFrom(r.Context()),
		r.PathValue("id"), EnrollmentStatus(req.To))
	if err != nil {
		h.writeDomainError(w, r, err, "transition enrollment")
		return
	}
	httpx.JSON(w, http.StatusOK, enrollment)
}

// writeDomainError maps students domain errors onto the API error envelope.
func (h *Handler) writeDomainError(w http.ResponseWriter, r *http.Request, err error, action string) {
	switch {
	case errors.Is(err, ErrIllegalTransition):
		httpx.Conflict(w, err.Error())
	case errors.Is(err, ErrAlreadyEnrolled), errors.Is(err, ErrLinkExists), errors.Is(err, ErrExternalIDTaken):
		httpx.Conflict(w, err.Error())
	case errors.Is(err, ErrNotFound):
		httpx.NotFound(w, "resource not found")
	case errors.Is(err, ErrValidation):
		httpx.BadRequest(w, err.Error())
	default:
		httpx.Internal(w, nil, r.Context(), action, err)
	}
}
