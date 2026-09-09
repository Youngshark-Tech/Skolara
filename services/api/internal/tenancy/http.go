package tenancy

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/httpx"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/observability"
)

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type ctxKey int

const ctxSchool ctxKey = iota

// SchoolFrom extracts the tenant school ID resolved by RequireSchool.
// Repositories MUST source school scoping from here — never from payloads.
func SchoolFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxSchool).(string)
	return v
}

// RequireSchool resolves and enforces the active tenant context:
//   - X-School-ID header when provided (requires ACTIVE membership, or
//     platform-admin role for administrative impersonation)
//   - otherwise the user's single active membership
//
// Requests without a resolvable context are rejected before domain work.
func RequireSchool(svc *Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := identity.ClaimsFrom(r.Context())
		if claims.UserID == "" {
			httpx.Unauthorized(w, "authentication required")
			return
		}
		school, err := svc.ResolveSchoolContext(r.Context(), claims.UserID, r.Header.Get("X-School-ID"))
		if err != nil {
			httpx.WriteError(w, http.StatusForbidden, "no_school_context",
				"no active school context: pass X-School-ID of a school you belong to")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxSchool, school.ID)))
	})
}

// Handler exposes tenancy endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register wires routes. jwt + resolver come from the identity context
// (imported, one-directional: tenancy → identity, never the reverse).
func (h *Handler) Register(mux *http.ServeMux, jwt *identity.JWTManager, resolver identity.PermissionResolver) {
	observability.Register(mux, "POST /api/v1/education-groups",
		identity.RequirePermission(jwt, resolver, identity.PermSchoolManage, http.HandlerFunc(h.createGroup)))
	observability.Register(mux, "GET /api/v1/education-groups",
		identity.RequirePermission(jwt, resolver, identity.PermSchoolRead, http.HandlerFunc(h.listGroups)))

	observability.Register(mux, "POST /api/v1/schools",
		identity.RequirePermission(jwt, resolver, identity.PermSchoolManage, http.HandlerFunc(h.createSchool)))
	observability.Register(mux, "GET /api/v1/schools",
		identity.RequirePermission(jwt, resolver, identity.PermSchoolRead, http.HandlerFunc(h.listSchools)))
	observability.Register(mux, "GET /api/v1/schools/{id}",
		identity.RequirePermission(jwt, resolver, identity.PermSchoolRead, http.HandlerFunc(h.getSchool)))
	observability.Register(mux, "GET /api/v1/schools/{id}/campuses",
		identity.RequirePermission(jwt, resolver, identity.PermSchoolRead, http.HandlerFunc(h.listCampuses)))
	observability.Register(mux, "POST /api/v1/schools/{id}/campuses",
		identity.RequirePermission(jwt, resolver, identity.PermAcadManage, http.HandlerFunc(h.createCampus)))
	observability.Register(mux, "GET /api/v1/schools/{id}/members",
		identity.RequirePermission(jwt, resolver, identity.PermSchoolRead, http.HandlerFunc(h.listMembers)))
	observability.Register(mux, "POST /api/v1/schools/{id}/members",
		identity.RequirePermission(jwt, resolver, identity.PermSchoolManage, http.HandlerFunc(h.addMember)))
	observability.Register(mux, "GET /api/v1/me/memberships",
		identity.RequireAuth(jwt, http.HandlerFunc(h.myMemberships)))
}

func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	if req.Name == "" {
		httpx.BadRequest(w, "name required")
		return
	}
	g := &EducationGroup{ID: newID(), Name: req.Name}
	if err := h.svc.repo.CreateGroup(r.Context(), g); err != nil {
		httpx.Internal(w, nil, r.Context(), "create group", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, g)
}

func (h *Handler) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.svc.repo.ListGroups(r.Context())
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list groups", err)
		return
	}
	httpx.JSON(w, http.StatusOK, groups)
}

func (h *Handler) createSchool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code    string `json:"code"`
		Name    string `json:"name"`
		GroupID string `json:"groupId"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	school, err := h.svc.CreateSchool(r.Context(), req.Code, req.Name, req.GroupID, identity.ClaimsFrom(r.Context()).UserID)
	if err != nil {
		switch {
		case errors.Is(err, ErrCodeTaken):
			httpx.Conflict(w, "school code already in use")
		case errors.Is(err, ErrValidation):
			httpx.BadRequest(w, err.Error())
		default:
			httpx.Internal(w, nil, r.Context(), "create school", err)
		}
		return
	}
	httpx.JSON(w, http.StatusCreated, school)
}

func (h *Handler) listSchools(w http.ResponseWriter, r *http.Request) {
	claims := identity.ClaimsFrom(r.Context())
	// Platform admins list everything; others only their schools.
	if ok, _ := h.svc.IsPlatformAdmin(r.Context(), claims.UserID); ok {
		schools, err := h.svc.ListSchools(r.Context(), r.URL.Query().Get("groupId"))
		if err != nil {
			httpx.Internal(w, nil, r.Context(), "list schools", err)
			return
		}
		httpx.JSON(w, http.StatusOK, schools)
		return
	}
	memberships, err := h.svc.MembershipsFor(r.Context(), claims.UserID)
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "memberships", err)
		return
	}
	out := make([]*School, 0, len(memberships))
	for _, m := range memberships {
		if m.Status != "active" {
			continue
		}
		if s, err := h.svc.School(r.Context(), m.SchoolID); err == nil {
			out = append(out, s)
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) getSchool(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.canAccessSchool(r, id) {
		// 404 (not 403) to avoid tenant enumeration (ADR-006).
		httpx.NotFound(w, "school not found")
		return
	}
	school, err := h.svc.School(r.Context(), id)
	if err != nil {
		httpx.NotFound(w, "school not found")
		return
	}
	httpx.JSON(w, http.StatusOK, school)
}

func (h *Handler) canAccessSchool(r *http.Request, schoolID string) bool {
	ctx := r.Context()
	claims := identity.ClaimsFrom(ctx)
	if ok, _ := h.svc.IsPlatformAdmin(ctx, claims.UserID); ok {
		return true
	}
	status, err := h.svc.repo.MembershipStatus(ctx, claims.UserID, schoolID)
	return err == nil && status == "active"
}

func (h *Handler) listCampuses(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.canAccessSchool(r, id) {
		httpx.NotFound(w, "school not found")
		return
	}
	campuses, err := h.svc.Campuses(r.Context(), id)
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list campuses", err)
		return
	}
	httpx.JSON(w, http.StatusOK, campuses)
}

func (h *Handler) createCampus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.canAccessSchool(r, id) {
		httpx.NotFound(w, "school not found")
		return
	}
	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Location string `json:"location"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	campus, err := h.svc.CreateCampus(r.Context(), id, req.Code, req.Name, req.Location)
	if err != nil {
		switch {
		case errors.Is(err, ErrCodeTaken):
			httpx.Conflict(w, "campus code already in use for school")
		case errors.Is(err, ErrValidation):
			httpx.BadRequest(w, err.Error())
		default:
			httpx.Internal(w, nil, r.Context(), "create campus", err)
		}
		return
	}
	httpx.JSON(w, http.StatusCreated, campus)
}

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.canAccessSchool(r, id) {
		httpx.NotFound(w, "school not found")
		return
	}
	members, err := h.svc.Members(r.Context(), id)
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list members", err)
		return
	}
	httpx.JSON(w, http.StatusOK, members)
}

func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.canAccessSchool(r, id) {
		httpx.NotFound(w, "school not found")
		return
	}
	var req struct {
		UserID string `json:"userId"`
		Role   string `json:"role"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	if req.UserID == "" {
		httpx.BadRequest(w, "userId required")
		return
	}
	if err := h.svc.AddMember(r.Context(), id, req.UserID, req.Role); err != nil {
		if errors.Is(err, ErrValidation) {
			httpx.BadRequest(w, err.Error())
			return
		}
		httpx.Internal(w, nil, r.Context(), "add member", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) myMemberships(w http.ResponseWriter, r *http.Request) {
	claims := identity.ClaimsFrom(r.Context())
	memberships, err := h.svc.MembershipsFor(r.Context(), claims.UserID)
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "memberships", err)
		return
	}
	httpx.JSON(w, http.StatusOK, memberships)
}
