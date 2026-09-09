package middleware

import (
	"net/http"
	"net/http/httptest"
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
