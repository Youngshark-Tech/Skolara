package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/events"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/httpx"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
)

// Handler exposes identity endpoints.
type Handler struct {
	svc  *AuthService
	jwt  *JWTManager
	pool *postgres.Pool
}

// NewHandler builds the identity HTTP handler. The pool is provided by the
// composition root so handlers can emit outbox events.
func NewHandler(svc *AuthService, jwt *JWTManager, pool *postgres.Pool) *Handler {
	return &Handler{svc: svc, jwt: jwt, pool: pool}
}

const refreshCookieName = "skolara_refresh"

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/login", h.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.handleRefresh)
	mux.HandleFunc("POST /api/v1/auth/logout", h.handleLogout)
	mux.Handle("GET /api/v1/me", RequireAuth(h.jwt, http.HandlerFunc(h.handleMe)))
	mux.Handle("GET /api/v1/users", RequirePermission(h.jwt, h.svc, PermUserRead, http.HandlerFunc(h.handleListUsers)))
	mux.Handle("POST /api/v1/users", RequirePermission(h.jwt, h.svc, PermUserManage, http.HandlerFunc(h.handleCreateUser)))
	mux.Handle("GET /api/v1/users/{id}", RequirePermission(h.jwt, h.svc, PermUserRead, http.HandlerFunc(h.handleGetUser)))
	mux.Handle("POST /api/v1/users/{id}/roles", RequirePermission(h.jwt, h.svc, PermUserManage, http.HandlerFunc(h.handleAssignRole)))
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	if req.Email == "" || req.Password == "" {
		httpx.BadRequest(w, "email and password are required")
		return
	}
	ip := clientIP(r)
	access, refresh, err := h.svc.Login(r.Context(), req.Email, req.Password, ip)
	if err != nil {
		h.auditLogin(r, "", req.Email, err)
		switch {
		case errors.Is(err, ErrBadCredentials):
			httpx.Unauthorized(w, "invalid email or password")
		case errors.Is(err, ErrAccountLocked):
			httpx.WriteError(w, http.StatusLocked, "account_locked", "account locked due to failed attempts; try later")
		case errors.Is(err, ErrAccountDisabled):
			httpx.WriteError(w, http.StatusForbidden, "account_disabled", "account disabled")
		default:
			httpx.Internal(w, nil, r.Context(), "login", err)
		}
		return
	}
	h.auditLogin(r, "", req.Email, nil)
	setRefreshCookie(w, refresh)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"accessToken": access,
		"tokenType":   "Bearer",
		"expiresIn":   int(h.jwt.Expiry().Seconds()),
	})
}

func (h *Handler) auditLogin(r *http.Request, actor, email string, err error) {
	detail := map[string]any{"email": email, "ip": clientIP(r)}
	action := "auth.login"
	if err != nil {
		action = "auth.login_failed"
		detail["reason"] = err.Error()
	}
	_ = h.svc.Audit(r.Context(), AuditEntry{
		ActorID:      actor,
		Action:       action,
		ResourceType: "user",
		ResourceID:   email,
		Detail:       detail,
	})
}

func (h *Handler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	raw := readRefreshCookie(r)
	if raw == "" {
		var req struct {
			RefreshToken string `json:"refreshToken"`
		}
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return
		}
		raw = req.RefreshToken
	}
	if raw == "" {
		httpx.BadRequest(w, "refresh token missing")
		return
	}
	access, newRefresh, err := h.svc.Refresh(r.Context(), raw, clientIP(r))
	if err != nil {
		if errors.Is(err, ErrTokenReuse) {
			// Security event: whole family revoked.
			_ = h.svc.Audit(r.Context(), AuditEntry{
				Action: "auth.refresh_reuse_detected", ResourceType: "session", ResourceID: "family",
			})
			httpx.Unauthorized(w, "session revoked")
			return
		}
		httpx.Unauthorized(w, "invalid refresh token")
		return
	}
	setRefreshCookie(w, newRefresh)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"accessToken": access,
		"tokenType":   "Bearer",
		"expiresIn":   int(h.jwt.Expiry().Seconds()),
	})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	raw := readRefreshCookie(r)
	if raw == "" {
		var req struct {
			RefreshToken string `json:"refreshToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		raw = req.RefreshToken
	}
	if raw != "" {
		if err := h.svc.Logout(r.Context(), raw); err != nil {
			httpx.Internal(w, nil, r.Context(), "logout", err)
			return
		}
	}
	unsetRefreshCookie(w)
	_ = h.svc.Audit(r.Context(), AuditEntry{Action: "auth.logout", ResourceType: "session", ResourceID: "self"})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFrom(r.Context())
	u, err := h.svc.GetUser(r.Context(), claims.UserID)
	if err != nil {
		httpx.NotFound(w, "user not found")
		return
	}
	perms, err := h.svc.PermissionsFor(r.Context(), u.ID)
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "permissions", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"id":          u.ID,
		"email":       u.Email,
		"name":        u.Name,
		"status":      u.Status,
		"roles":       claims.Roles,
		"permissions": perms,
	})
}

func (h *Handler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	u, err := h.svc.CreateUser(r.Context(), req.Email, req.Name, req.Password, req.Role)
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			httpx.Conflict(w, "email already registered")
			return
		}
		if errors.Is(err, ErrValidation) {
			httpx.BadRequest(w, err.Error())
			return
		}
		httpx.Internal(w, nil, r.Context(), "create user", err)
		return
	}
	actor := ClaimsFrom(r.Context())
	_ = h.svc.Audit(r.Context(), AuditEntry{
		ActorID:      actor.UserID,
		Action:       "user.created",
		ResourceType: "user",
		ResourceID:   u.ID,
		After:        map[string]any{"email": u.Email, "name": u.Name, "role": req.Role},
	})
	if h.pool != nil {
		if _, err := events.Record(r.Context(), h.pool, nil, u.ID, "identity.UserCreated", 1,
			map[string]any{"email": u.Email, "name": u.Name, "role": req.Role}); err != nil {
			// Event failure must not fail the committed business operation;
			// the dispatcher re-derives user.created from audit if needed.
			// Logged via recover-free path: record as audit detail.
			_ = h.svc.Audit(r.Context(), AuditEntry{
				ActorID: actor.UserID, Action: "user.created_event_failed",
				ResourceType: "user", ResourceID: u.ID, Detail: map[string]any{"error": err.Error()},
			})
		}
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"id": u.ID, "email": u.Email, "name": u.Name, "status": u.Status,
	})
}

func (h *Handler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	limit := intQuery(r, "limit", 50)
	offset := intQuery(r, "offset", 0)
	users, total, err := h.svc.ListUsers(r.Context(), limit, offset)
	if err != nil {
		httpx.Internal(w, nil, r.Context(), "list users", err)
		return
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "status": u.Status})
	}
	w.Header().Set("X-Total-Count", itoa(total))
	httpx.JSON(w, http.StatusOK, map[string]any{"users": out, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) handleGetUser(w http.ResponseWriter, r *http.Request) {
	u, err := h.svc.GetUser(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.NotFound(w, "user not found")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "status": u.Status})
}

func (h *Handler) handleAssignRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Role string `json:"role"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}
	if req.Role == "" {
		httpx.BadRequest(w, "role required")
		return
	}
	if err := h.svc.AssignRole(r.Context(), ClaimsFrom(r.Context()).UserID, id, req.Role); err != nil {
		httpx.Internal(w, nil, r.Context(), "assign role", err)
		return
	}
	actor := ClaimsFrom(r.Context())
	_ = h.svc.Audit(r.Context(), AuditEntry{
		ActorID: actor.UserID, Action: "user.role_assigned",
		ResourceType: "user", ResourceID: id,
		After: map[string]any{"role": req.Role},
	})
	w.WriteHeader(http.StatusNoContent)
}

// --- middleware -------------------------------------------------------------

type claimsKey struct{}

// ClaimsFrom returns the verified session claims from a request context.
func ClaimsFrom(ctx context.Context) *SessionClaims {
	v, _ := ctx.Value(claimsKey{}).(*SessionClaims)
	if v == nil {
		return &SessionClaims{}
	}
	return v
}

// RequireAuth enforces a valid access token.
func RequireAuth(jwt *JWTManager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authenticate(r, jwt)
		if !ok {
			httpx.Unauthorized(w, "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(withClaims(r.Context(), claims)))
	})
}

// PermissionResolver resolves a user's effective permission set server-side.
// The composition root wires a resolver that unions platform roles (identity)
// with school-scoped membership roles (tenancy).
type PermissionResolver interface {
	PermissionsFor(ctx context.Context, userID string) (map[string]bool, error)
}

// RequirePermission enforces a valid token AND the named permission,
// resolved server-side (never trusted from the token alone).
func RequirePermission(jwt *JWTManager, resolver PermissionResolver, permission string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authenticate(r, jwt)
		if !ok {
			httpx.Unauthorized(w, "authentication required")
			return
		}
		perms, err := resolver.PermissionsFor(r.Context(), claims.UserID)
		if err != nil {
			httpx.Internal(w, nil, r.Context(), "permissions", err)
			return
		}
		if !perms[permission] {
			httpx.Forbidden(w, "missing permission: "+permission)
			return
		}
		next.ServeHTTP(w, r.WithContext(withClaims(r.Context(), claims)))
	})
}

func authenticate(r *http.Request, jwt *JWTManager) (*SessionClaims, bool) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, false
	}
	claims, err := jwt.Verify(strings.TrimPrefix(auth, "Bearer "))
	if err != nil {
		return nil, false
	}
	return claims, true
}

func withClaims(ctx context.Context, c *SessionClaims) context.Context {
	return context.WithValue(ctx, claimsKey{}, c)
}

// --- helpers ------------------------------------------------------------------

func setRefreshCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     "/api/v1/auth",
		Expires:  time.Now().Add(RefreshTokenExpiry),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func unsetRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookieName, Value: "", Path: "/api/v1/auth",
		MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

func readRefreshCookie(r *http.Request) string {
	c, err := r.Cookie(refreshCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func clientIP(r *http.Request) string {
	// RemoteAddr only — proxy headers are not trusted without a validated proxy chain.
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

func intQuery(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		return def
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
