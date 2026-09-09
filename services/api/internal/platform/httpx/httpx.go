// Package httpx provides the HTTP transport plumbing shared by all domains:
// the standard JSON envelope, request-id propagation, recovery, security
// headers, CORS, and body limits.
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ErrBody is the standard API error envelope.
type ErrBody struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []Detail `json:"details,omitempty"`
}

type Detail struct {
	Field string `json:"field,omitempty"`
	Issue string `json:"issue"`
}

type errorBody struct {
	Error ErrBody `json:"error"`
}

type ctxKey int

const ctxRequestID ctxKey = iota

// NewRequestID returns a 128-bit random request id.
func NewRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// RequestID extracts the request id from a request context.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

// RequestIDMiddleware assigns/propagates X-Request-ID and exposes it in the
// response and log context.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 128 {
			id = NewRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxRequestID, id)))
	})
}

// StatusWriter captures the response status code for metrics and access
// logging while proxying everything to the underlying ResponseWriter.
type StatusWriter struct {
	http.ResponseWriter
	status int
}

// NewStatusWriter wraps w, defaulting the captured status to 200 (implicit
// writes).
func NewStatusWriter(w http.ResponseWriter) *StatusWriter {
	return &StatusWriter{ResponseWriter: w, status: http.StatusOK}
}

// Status returns the captured status code.
func (w *StatusWriter) Status() int { return w.status }

func (w *StatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Write is overridden so implicit 200s are tracked too.
func (w *StatusWriter) Write(b []byte) (int, error) {
	return w.ResponseWriter.Write(b)
}

// Flush lets streaming handlers pass through to the underlying writer.
func (w *StatusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// RequestScope is a per-request scratch record created by the access-log
// middleware and mutated by deeper layers (auth, tenant resolution) so the
// final log line carries actor and tenant without log-context plumbing.
type RequestScope struct {
	ActorID  string
	TenantID string
}

type scopeKey struct{}

// WithScope injects a fresh scope into the context.
func WithScope(ctx context.Context, s *RequestScope) context.Context {
	return context.WithValue(ctx, scopeKey{}, s)
}

// ScopeFrom returns the request scope, or nil when absent (e.g. unit tests
// that mount handlers without the access-log middleware).
func ScopeFrom(ctx context.Context) *RequestScope {
	s, _ := ctx.Value(scopeKey{}).(*RequestScope)
	return s
}

// AccessLogMiddleware writes one structured line per request (method, path,
// status, duration, request id, actor, tenant). It must sit OUTSIDE the
// request-id middleware (request id is read from the response header) and
// creates the RequestScope deeper layers annotate.
func AccessLogMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := NewStatusWriter(w)
		scope := &RequestScope{}
		next.ServeHTTP(sw, r.WithContext(WithScope(r.Context(), scope)))
		if logger == nil {
			return
		}
		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", sw.Status()),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.String("request_id", w.Header().Get("X-Request-ID")),
			slog.String("remote", r.RemoteAddr),
		}
		if scope.ActorID != "" {
			attrs = append(attrs, slog.String("actor_id", scope.ActorID))
		}
		if scope.TenantID != "" {
			attrs = append(attrs, slog.String("school_id", scope.TenantID))
		}
		logger.LogAttrs(r.Context(), slog.LevelInfo, "http_request", attrs...)
	})
}

// JSON writes a JSON response with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes the standard error envelope.
func WriteError(w http.ResponseWriter, status int, code, message string, details ...Detail) {
	JSON(w, status, errorBody{Error: ErrBody{Code: code, Message: message, Details: details}})
}

// Common error helpers keep status/code pairs consistent across domains.
func BadRequest(w http.ResponseWriter, msg string, details ...Detail) {
	WriteError(w, http.StatusBadRequest, "bad_request", msg, details...)
}
func Unauthorized(w http.ResponseWriter, msg string) {
	WriteError(w, http.StatusUnauthorized, "unauthorized", msg)
}
func Forbidden(w http.ResponseWriter, msg string) {
	WriteError(w, http.StatusForbidden, "forbidden", msg)
}
func NotFound(w http.ResponseWriter, msg string) {
	WriteError(w, http.StatusNotFound, "not_found", msg)
}
func Conflict(w http.ResponseWriter, msg string) {
	WriteError(w, http.StatusConflict, "conflict", msg)
}
func TooManyRequests(w http.ResponseWriter, msg string) {
	WriteError(w, http.StatusTooManyRequests, "rate_limited", msg)
}
func Internal(w http.ResponseWriter, logger *slog.Logger, ctx context.Context, msg string, err error) {
	if logger != nil {
		logger.ErrorContext(ctx, msg, "error", err.Error(), "request_id", RequestID(ctx))
	}
	WriteError(w, http.StatusInternalServerError, "internal", "an internal error occurred")
}

// ErrValidation signals a 400-class failure from application code.
var ErrValidation = errors.New("validation failed")

// RecoverMiddleware converts panics into 500s without leaking stack traces.
func RecoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if logger != nil {
					logger.Error("panic recovered", "panic", rec, "path", r.URL.Path,
						"request_id", RequestID(r.Context()))
				}
				WriteError(w, http.StatusInternalServerError, "internal", "an internal error occurred")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SecurityHeadersMiddleware sets defensive defaults on every response.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware implements an explicit allow-list CORS policy.
func CORSMiddleware(allowed []string, next http.Handler) http.Handler {
	allowedSet := map[string]bool{}
	for _, o := range allowed {
		allowedSet[strings.ToLower(o)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowedSet[strings.ToLower(origin)] {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, Idempotency-Key")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions && origin != "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// BodyLimitMiddleware enforces a maximum request body size.
func BodyLimitMiddleware(maxBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// DecodeJSON decodes a strictly-shaped JSON body with defensive size enforcement.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		BadRequest(w, "invalid request body")
		return err
	}
	return nil
}
