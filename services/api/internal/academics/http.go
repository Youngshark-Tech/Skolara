package academics

import (
	"errors"
	"net/http"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/httpx"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/observability"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
)

// Handler exposes academics endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register wires routes. jwt + resolver come from the identity context and
// the tenant school comes from tenancy.SchoolFrom (academics → identity/
// tenancy, one-directional — never the reverse).
func (h *Handler) Register(mux *http.ServeMux, jwt *identity.JWTManager, resolver identity.PermissionResolver) {
	observability.Register(mux, "POST /api/v1/academic-years",
		identity.RequirePermission(jwt, resolver, identity.PermAcadManage, h.schoolScoped(h.createAcademicYear)))
	observability.Register(mux, "GET /api/v1/academic-years",
		identity.RequirePermission(jwt, resolver, identity.PermAcadRead, h.schoolScoped(h.listAcademicYears)))
	observability.Register(mux, "GET /api/v1/academic-years/{id}",
		identity.RequirePermission(jwt, resolver, identity.PermAcadRead, h.schoolScoped(h.getAcademicYear)))
	observability.Register(mux, "GET /api/v1/academic-years/{id}/terms",
		identity.RequirePermission(jwt, resolver, identity.PermAcadRead, h.schoolScoped(h.listTerms)))
	observability.Register(mux, "POST /api/v1/terms",
		identity.RequirePermission(jwt, resolver, identity.PermAcadManage, h.schoolScoped(h.createTerm)))

	observability.Register(mux, "POST /api/v1/subjects",
		identity.RequirePermission(jwt, resolver, identity.PermAcadManage, h.schoolScoped(h.createSubject)))
	observability.Register(mux, "GET /api/v1/subjects",
		identity.RequirePermission(jwt, resolver, identity.PermAcadRead, h.schoolScoped(h.listSubjects)))

	observability.Register(mux, "POST /api/v1/classes",
		identity.RequirePermission(jwt, resolver, identity.PermAcadManage, h.schoolScoped(h.createClass)))
	observability.Register(mux, "GET /api/v1/classes",
		identity.RequirePermission(jwt, resolver, identity.PermAcadRead, h.schoolScoped(h.listClasses)))
	observability.Register(mux, "GET /api/v1/classes/{id}/roster",
		identity.RequirePermission(jwt, resolver, identity.PermAcadRead, h.schoolScoped(h.listRoster)))
	observability.Register(mux, "POST /api/v1/classes/{id}/roster",
		identity.RequirePermission(jwt, resolver, identity.PermAcadManage, h.schoolScoped(h.addRoster)))
	observability.Register(mux, "GET /api/v1/classes/{id}/teachers",
		identity.RequirePermission(jwt, resolver, identity.PermAcadRead, h.schoolScoped(h.listTeachers)))
	observability.Register(mux, "POST /api/v1/classes/{id}/teachers",
		identity.RequirePermission(jwt, resolver, identity.PermAcadManage, h.schoolScoped(h.assignTeacher)))
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

func (h *Handler) createAcademicYear(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	year, err := h.svc.CreateAcademicYear(r.Context(), tenancy.SchoolFrom(r.Context()), req.Name, req.StartDate, req.EndDate)
	if err != nil {
		h.writeDomainError(w, r, err, "create academic year")
		return
	}
	httpx.JSON(w, http.StatusCreated, year)
}

func (h *Handler) listAcademicYears(w http.ResponseWriter, r *http.Request) {
	years, err := h.svc.AcademicYears(r.Context(), tenancy.SchoolFrom(r.Context()))
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list academic years", err)
		return
	}
	httpx.JSON(w, http.StatusOK, years)
}

func (h *Handler) getAcademicYear(w http.ResponseWriter, r *http.Request) {
	year, err := h.svc.AcademicYearByID(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		// 404 (not 403) to avoid tenant enumeration (ADR-006).
		httpx.NotFound(w, "academic year not found")
		return
	}
	httpx.JSON(w, http.StatusOK, year)
}

func (h *Handler) createTerm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AcademicYearID string `json:"academicYearId"`
		Name           string `json:"name"`
		StartDate      string `json:"startDate"`
		EndDate        string `json:"endDate"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	term, err := h.svc.CreateTerm(r.Context(), tenancy.SchoolFrom(r.Context()),
		req.AcademicYearID, req.Name, req.StartDate, req.EndDate)
	if err != nil {
		h.writeDomainError(w, r, err, "create term")
		return
	}
	httpx.JSON(w, http.StatusCreated, term)
}

func (h *Handler) listTerms(w http.ResponseWriter, r *http.Request) {
	terms, err := h.svc.TermsForYear(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.NotFound(w, "academic year not found")
			return
		}
		httpx.Internal(w, nil, r.Context(), "list terms", err)
		return
	}
	httpx.JSON(w, http.StatusOK, terms)
}

func (h *Handler) createSubject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	subject, err := h.svc.CreateSubject(r.Context(), tenancy.SchoolFrom(r.Context()), req.Code, req.Name)
	if err != nil {
		h.writeDomainError(w, r, err, "create subject")
		return
	}
	httpx.JSON(w, http.StatusCreated, subject)
}

func (h *Handler) listSubjects(w http.ResponseWriter, r *http.Request) {
	subjects, err := h.svc.Subjects(r.Context(), tenancy.SchoolFrom(r.Context()))
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list subjects", err)
		return
	}
	httpx.JSON(w, http.StatusOK, subjects)
}

func (h *Handler) createClass(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AcademicYearID string `json:"academicYearId"`
		Name           string `json:"name"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	class, err := h.svc.CreateClassGroup(r.Context(), tenancy.SchoolFrom(r.Context()), req.AcademicYearID, req.Name)
	if err != nil {
		h.writeDomainError(w, r, err, "create class")
		return
	}
	httpx.JSON(w, http.StatusCreated, class)
}

func (h *Handler) listClasses(w http.ResponseWriter, r *http.Request) {
	classes, err := h.svc.ClassGroups(r.Context(), tenancy.SchoolFrom(r.Context()), r.URL.Query().Get("yearId"))
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list classes", err)
		return
	}
	httpx.JSON(w, http.StatusOK, classes)
}

func (h *Handler) addRoster(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LearnerIDs []string `json:"learnerIds"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	err := h.svc.AddRoster(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"), req.LearnerIDs)
	if err != nil {
		h.writeDomainError(w, r, err, "add roster entries")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listRoster(w http.ResponseWriter, r *http.Request) {
	roster, err := h.svc.Roster(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		h.writeDomainError(w, r, err, "list roster")
		return
	}
	httpx.JSON(w, http.StatusOK, roster)
}

func (h *Handler) assignTeacher(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SubjectID string `json:"subjectId"`
		TeacherID string `json:"teacherId"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	assignment, err := h.svc.AssignTeacher(r.Context(), tenancy.SchoolFrom(r.Context()),
		r.PathValue("id"), req.SubjectID, req.TeacherID)
	if err != nil {
		h.writeDomainError(w, r, err, "assign teacher")
		return
	}
	httpx.JSON(w, http.StatusCreated, assignment)
}

func (h *Handler) listTeachers(w http.ResponseWriter, r *http.Request) {
	assignments, err := h.svc.AssignmentsForClass(r.Context(), tenancy.SchoolFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		h.writeDomainError(w, r, err, "list teaching assignments")
		return
	}
	httpx.JSON(w, http.StatusOK, assignments)
}

// writeDomainError maps academics domain errors onto the API error envelope.
func (h *Handler) writeDomainError(w http.ResponseWriter, r *http.Request, err error, action string) {
	switch {
	case errors.Is(err, ErrValidation), errors.Is(err, ErrDateRange), errors.Is(err, ErrOutsideYear), errors.Is(err, ErrBatchSize):
		httpx.BadRequest(w, err.Error())
	case errors.Is(err, ErrNotFound):
		httpx.NotFound(w, "resource not found")
	case errors.Is(err, ErrTermOverlap), errors.Is(err, ErrCodeTaken), errors.Is(err, ErrNameTaken), errors.Is(err, ErrAssignmentExists):
		httpx.Conflict(w, err.Error())
	default:
		httpx.Internal(w, nil, r.Context(), action, err)
	}
}
