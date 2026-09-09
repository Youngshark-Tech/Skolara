// Package middleware provides cross-cutting request policies shared by all domains:
// in-process rate limiting and per-request timeouts.
package middleware

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/httpx"
)

// RateLimiter is a token-bucket limiter keyed by client identity.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rps     float64
	burst   int
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter creates a limiter allowing rps sustained and burst peak.
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{buckets: map[string]*bucket{}, rps: rps, burst: burst, now: time.Now}
}

// Allow reports whether key may proceed.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &bucket{tokens: float64(rl.burst) - 1, last: rl.now()}
		return true
	}
	now := rl.now()
	b.tokens += now.Sub(b.last).Seconds() * rl.rps
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// Reset clears state (used by tests).
func (rl *RateLimiter) Reset() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.buckets = map[string]*bucket{}
}

// ClientIP extracts the client IP from RemoteAddr (proxy-validated XFF handling
// is a deployment concern and intentionally not trusted here).
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return strings.TrimSpace(host)
}

// RateLimitMiddleware applies the limiter per client IP. Override keys (e.g.
// login limiter) can be supplied via WithKey.
type keyFunc func(*http.Request) string

func RateLimitMiddleware(rl *RateLimiter, next http.Handler) http.Handler {
	return RateLimitWithKey(rl, ClientIP, next)
}

// RateLimitWithKey applies the limiter with a custom key extractor (e.g. username
// for login brute-force protection).
func RateLimitWithKey(rl *RateLimiter, key keyFunc, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.Allow(key(r)) {
			w.Header().Set("Retry-After", strconv.Itoa(5))
			httpx.TooManyRequests(w, "too many requests, please retry later")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// TimeoutMiddleware bounds the total request time.
func TimeoutMiddleware(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
