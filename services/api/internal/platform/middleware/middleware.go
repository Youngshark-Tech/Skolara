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

// Bucket eviction defaults. A bucket idle for longer than the TTL is dropped
// by the janitor, keeping the bucket map bounded under high-cardinality
// client identities (e.g. IPv6 rotation) instead of growing forever.
const (
	defaultBucketIdleTTL    = time.Hour
	defaultBucketSweepEvery = 10 * time.Minute
)

// RateLimiter is a token-bucket limiter keyed by client identity. Buckets are
// touched on every Allow and swept by a janitor goroutine (Stop halts it on
// shutdown); eviction only ever drops idle buckets, so active clients keep
// their budget while the memory footprint stays bounded.
type RateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	rps        float64
	burst      int
	now        func() time.Time
	idleTTL    time.Duration
	sweepEvery time.Duration
	stop       chan struct{}
	stopOnce   sync.Once
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter creates a limiter allowing rps sustained and burst peak. A
// janitor goroutine sweeps buckets idle for more than an hour every ten
// minutes; call Stop to release it on shutdown.
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets:    map[string]*bucket{},
		rps:        rps,
		burst:      burst,
		now:        time.Now,
		idleTTL:    defaultBucketIdleTTL,
		sweepEvery: defaultBucketSweepEvery,
		stop:       make(chan struct{}),
	}
	go rl.janitor()
	return rl
}

// Allow reports whether key may proceed. Every call touches the bucket's
// last-seen timestamp, which is what the janitor's idle test keys on.
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

// sweep evicts buckets idle for longer than the idle TTL and returns how many
// were removed. A client returning after eviction simply starts from a fresh
// full bucket — the accepted tradeoff for bounded memory (issue #52).
func (rl *RateLimiter) sweep(now time.Time) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	removed := 0
	for key, b := range rl.buckets {
		if now.Sub(b.last) > rl.idleTTL {
			delete(rl.buckets, key)
			removed++
		}
	}
	return removed
}

// janitor periodically sweeps idle buckets so the per-IP bucket map stays
// bounded under IPv6 rotation and other key churn. It exits when Stop is
// called (or the process ends).
func (rl *RateLimiter) janitor() {
	t := time.NewTicker(rl.sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-rl.stop:
			return
		case <-t.C:
			rl.sweep(rl.now())
		}
	}
}

// Stop terminates the janitor goroutine. Idempotent; Allow keeps working.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.stop) })
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
