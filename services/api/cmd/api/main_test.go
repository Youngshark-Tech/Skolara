package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeHealthz(t *testing.T) {
	t.Run("200 issues no error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				t.Errorf("probe hit %q, want /healthz", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		if err := probeHealthz(srv.URL+"/healthz", 2*time.Second); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("500 is unhealthy", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		if err := probeHealthz(srv.URL+"/healthz", 2*time.Second); err == nil {
			t.Fatal("expected error for HTTP 500")
		}
	})

	t.Run("refused connection is unhealthy", func(t *testing.T) {
		// Grab an ephemeral port, then release it: nothing is listening there.
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		url := "http://" + l.Addr().String() + "/healthz"
		l.Close()

		if err := probeHealthz(url, 2*time.Second); err == nil {
			t.Fatal("expected error for refused connection")
		}
	})
}

func TestHealthcheckTarget(t *testing.T) {
	cases := []struct {
		name string
		addr string
		want string
	}{
		{"port-only default", ":8080", "http://127.0.0.1:8080/healthz"},
		{"wildcard host stripped", "0.0.0.0:8080", "http://127.0.0.1:8080/healthz"},
		{"explicit loopback kept", "127.0.0.1:9000", "http://127.0.0.1:9000/healthz"},
		{"other host kept as configured", "api.internal:8080", "http://api.internal:8080/healthz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := healthcheckTarget(tc.addr); got != tc.want {
				t.Errorf("healthcheckTarget(%q) = %q, want %q", tc.addr, got, tc.want)
			}
		})
	}
}
