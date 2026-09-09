package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRegisterMountsWithRouteLabel(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, "GET /api/v1/demo/{id}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot) // 418 — unambiguous marker
	}))

	before := testutil.ToFloat64(httpTotal.WithLabelValues("/api/v1/demo/{id}", "GET", "418"))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/demo/abc", nil))
	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rr.Code)
	}
	after := testutil.ToFloat64(httpTotal.WithLabelValues("/api/v1/demo/{id}", "GET", "418"))
	if after-before != 1 {
		t.Fatalf("counter delta = %v, want 1 (route label must be the template, not the path)", after-before)
	}
}

func TestRouteLabelStripsMethod(t *testing.T) {
	if got := RouteLabel("POST /api/v1/terms"); got != "/api/v1/terms" {
		t.Fatalf("RouteLabel = %q", got)
	}
	if got := RouteLabel("/healthz"); got != "/healthz" {
		t.Fatalf("RouteLabel bare = %q", got)
	}
}
