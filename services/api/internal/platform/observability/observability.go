// Package observability exposes health, readiness, and Prometheus metrics endpoints.
package observability

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Depinger checks a dependency (e.g. database) for readiness.
type Depinger func(ctx context.Context) error

var (
	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "skolara_http_request_duration_seconds",
		Help:    "HTTP request latency by route/method/status",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method", "status"})

	httpTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "skolara_http_requests_total",
		Help: "HTTP requests by route/method/status",
	}, []string{"route", "method", "status"})

	eventsPublished = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "skolara_events_published_total",
		Help: "Outbox events delivered to the publisher",
	})
)

func init() {
	prometheus.MustRegister(httpDuration, httpTotal, eventsPublished)
}

// MetricsMiddleware records per-route latency and totals. route is the route
// template (e.g. "/api/v1/students/{id}") to avoid label cardinality blowup.
func MetricsMiddleware(route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		status := strconv.Itoa(sw.status)
		httpDuration.WithLabelValues(route, r.Method, status).Observe(time.Since(start).Seconds())
		httpTotal.WithLabelValues(route, r.Method, status).Inc()
	})
}

// IncEventsPublished bumps the outbox delivery counter.
func IncEventsPublished() { eventsPublished.Inc() }

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Write is overridden so implicit 200s are tracked too.
func (w *statusWriter) Write(b []byte) (int, error) {
	return w.ResponseWriter.Write(b)
}

// Handler builds /healthz, /readyz, /metrics.
func Handler(dbPing Depinger) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := dbPing(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

	mux.Handle("/metrics", promhttp.Handler())
	return mux
}
