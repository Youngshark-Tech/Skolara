package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestRateLimiterAllowsBurst(t *testing.T) {
	rl := NewRateLimiter(1, 3)
	for i := 0; i < 3; i++ {
		if !rl.Allow("k") {
			t.Fatalf("request %d within burst denied", i)
		}
	}
	if rl.Allow("k") {
		t.Fatal("request beyond burst allowed")
	}
	// Different key independent.
	if !rl.Allow("other") {
		t.Fatal("independent key denied")
	}
}

func TestRateLimiterRefills(t *testing.T) {
	rl := NewRateLimiter(1000, 1)
	if !rl.Allow("k") {
		t.Fatal("first denied")
	}
	time.Sleep(5 * time.Millisecond) // ~5 tokens at 1000rps
	if !rl.Allow("k") {
		t.Fatal("refill denied")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	rl := NewRateLimiter(0.1, 1)
	handler := RateLimitMiddleware(rl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("POST", "/x", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: %d", rec.Code)
	}
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Code != 429 {
		t.Fatalf("second request: %d, want 429", rec2.Code)
	}
	if got := rec2.Header().Get("Retry-After"); got == "" {
		t.Fatal("missing Retry-After header")
	}
}

func TestTimeoutMiddleware(t *testing.T) {
	h := TimeoutMiddleware(50*time.Millisecond, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("expected deadline on request context")
		}
	}))
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.7:12345"
	if got := ClientIP(req); got != "192.168.1.7" {
		t.Fatalf("got %s", got)
	}
}

// TestRateLimiterSweepEvictsOnlyIdleBuckets proves the eviction rule: buckets
// idle for longer than the TTL are dropped, recently-touched buckets survive,
// and an evicted key simply starts from a fresh full bucket (#52).
func TestRateLimiterSweepEvictsOnlyIdleBuckets(t *testing.T) {
	base := time.Now()
	cur := base
	// Manual construction (no janitor goroutine) with an injectable clock so
	// bucket ages are fully deterministic.
	rl := &RateLimiter{
		buckets:    map[string]*bucket{},
		rps:        1,
		burst:      3,
		now:        func() time.Time { return cur },
		idleTTL:    time.Hour,
		sweepEvery: defaultBucketSweepEvery,
		stop:       make(chan struct{}),
	}

	if !rl.Allow("stale") {
		t.Fatal("stale allow denied")
	}
	cur = base.Add(30 * time.Minute)
	if !rl.Allow("fresh") {
		t.Fatal("fresh allow denied")
	}

	// At +89m: "stale" has been idle 89m (past the 1h TTL), "fresh" only
	// 59m (still fresh).
	if n := rl.sweep(base.Add(89 * time.Minute)); n != 1 {
		t.Fatalf("evicted %d buckets at +89m, want 1", n)
	}
	rl.mu.Lock()
	_, hasStale := rl.buckets["stale"]
	_, hasFresh := rl.buckets["fresh"]
	rl.mu.Unlock()
	if hasStale {
		t.Fatal("idle bucket survived sweep")
	}
	if !hasFresh {
		t.Fatal("touched bucket was evicted")
	}

	// Past the fresh bucket's TTL too: it goes as well.
	if n := rl.sweep(base.Add(2 * time.Hour)); n != 1 {
		t.Fatalf("evicted %d buckets at +2h, want 1", n)
	}
	rl.mu.Lock()
	if len(rl.buckets) != 0 {
		t.Fatalf("bucket map not empty: %d", len(rl.buckets))
	}
	rl.mu.Unlock()

	// Evicted keys restart with a full bucket (burst available again).
	cur = base.Add(2 * time.Hour)
	if !rl.Allow("stale") {
		t.Fatal("post-eviction allow denied")
	}
}

// TestRateLimiterBoundedUnderKeyChurn simulates IPv6 rotation: thousands of
// one-shot keys must not accumulate once the janitor sweep runs.
func TestRateLimiterBoundedUnderKeyChurn(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	defer rl.Stop()

	for i := 0; i < 5000; i++ {
		_ = rl.Allow(keyFor(i))
	}
	rl.mu.Lock()
	got := len(rl.buckets)
	rl.mu.Unlock()
	if got != 5000 {
		t.Fatalf("bucket count %d, want 5000", got)
	}
	if n := rl.sweep(time.Now().Add(2 * defaultBucketIdleTTL)); n != 5000 {
		t.Fatalf("sweep removed %d, want 5000", n)
	}
	rl.mu.Lock()
	got = len(rl.buckets)
	rl.mu.Unlock()
	if got != 0 {
		t.Fatalf("bucket map not empty after sweep: %d", got)
	}
}

func keyFor(i int) string {
	return "203.0.113." + strconv.Itoa(i)
}

// TestRateLimiterJanitorSweepsPeriodically exercises the janitor goroutine
// end-to-end with a short sweep interval: the idle bucket disappears while a
// continuously-touched bucket survives.
func TestRateLimiterJanitorSweepsPeriodically(t *testing.T) {
	rl := &RateLimiter{
		buckets:    map[string]*bucket{},
		rps:        1,
		burst:      3,
		now:        time.Now,
		idleTTL:    30 * time.Millisecond,
		sweepEvery: 5 * time.Millisecond,
		stop:       make(chan struct{}),
	}
	go rl.janitor()
	defer rl.Stop()

	_ = rl.Allow("gone")
	stopTouch := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopTouch:
				return
			case <-ticker.C:
				_ = rl.Allow("kept")
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)
	close(stopTouch)
	wg.Wait()

	rl.mu.Lock()
	_, hasGone := rl.buckets["gone"]
	_, hasKept := rl.buckets["kept"]
	rl.mu.Unlock()
	if hasGone {
		t.Fatal("janitor did not evict idle bucket")
	}
	if !hasKept {
		t.Fatal("janitor evicted an active bucket")
	}
}

// TestRateLimiterStopIsIdempotent guards the shutdown hook against
// double-close panics and verifies Allow keeps working after Stop.
func TestRateLimiterStopIsIdempotent(t *testing.T) {
	rl := NewRateLimiter(1, 3)
	rl.Stop()
	rl.Stop() // must not panic
	if !rl.Allow("k") {
		t.Fatal("allow after stop denied")
	}
}
