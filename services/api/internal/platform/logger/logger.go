// Package logger provides structured JSON logging via log/slog with
// mandatory correlation fields and secret redaction.
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxTenantID
	ctxActorID
)

// New builds the application logger. Only JSON format is used in production;
// text is available for local ergonomics.
func New(level, format string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lv}
	var h slog.Handler
	if strings.EqualFold(format, "text") {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

// WithRequestID annotates a context (and thereby all log lines produced from it).
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxRequestID, id)
}

// WithTenant annotates the context with the active school (tenant) id.
func WithTenant(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxTenantID, id)
}

// WithActor annotates the context with the acting user id.
func WithActor(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxActorID, id)
}

// Fields extracts logging correlation fields from a context.
func Fields(ctx context.Context) []any {
	var out []any
	if v, ok := ctx.Value(ctxRequestID).(string); ok && v != "" {
		out = append(out, "request_id", v)
	}
	if v, ok := ctx.Value(ctxTenantID).(string); ok && v != "" {
		out = append(out, "school_id", v)
	}
	if v, ok := ctx.Value(ctxActorID).(string); ok && v != "" {
		out = append(out, "actor_id", v)
	}
	return out
}

// forbiddenKeys are never allowed to be logged (master spec §44).
var forbiddenKeys = map[string]struct{}{
	"password": {}, "password_hash": {}, "token": {}, "access_token": {},
	"refresh_token": {}, "secret": {}, "authorization": {}, "pin": {},
}

// SafeLogValue scrub: drop forbidden keys from attribute groups.
func SafeLogValue(groups []string, a slog.Attr) slog.Value {
	if _, bad := forbiddenKeys[strings.ToLower(a.Key)]; bad && len(groups) == 0 {
		return slog.StringValue("[REDACTED]")
	}
	return a.Value
}

// L returns a logger that injects correlation fields from ctx automatically.
func L(ctx context.Context, base *slog.Logger) *slog.Logger {
	return base.With(Fields(ctx)...)
}
