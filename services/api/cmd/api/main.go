// Command api is the Skolara API composition root. It is the ONLY place where
// concrete platform and domain implementations are wired together (ADR-001).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/config"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/events"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/httpx"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/logger"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/middleware"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/observability"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
)

func main() {
	healthcheckOnly := flag.Bool("healthcheck", false, "exit 0 (container health probe)")
	flag.Parse()
	if *healthcheckOnly {
		os.Exit(0)
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := logger.New(cfg.LogLevel, cfg.LogFormat)
	log.Info("starting skolara api", "env", cfg.Env, "db", postgres.RedactedURL(cfg.DatabaseURL))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer pool.Close()

	// Event dispatcher (transactional outbox, ADR-003).
	dispatcher := events.NewDispatcher(pool, events.LogPublisher{Logf: func(f string, a ...any) {
		log.Info(fmt.Sprintf(f, a...))
	}})
	go dispatcher.Run(ctx)

	root := http.NewServeMux()
	root.Handle("/healthz", observability.Handler(pool.Ping))
	root.Handle("/readyz", observability.Handler(pool.Ping))
	root.Handle("/metrics", observability.Handler(pool.Ping))
	root.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "resource not found")
	})

	// Global middleware chain (outermost first):
	// recover → security headers → request ID → CORS → body limit → rate limit → timeout.
	var handler http.Handler = root
	handler = middleware.TimeoutMiddleware(cfg.HTTPTimeout, handler)
	handler = middleware.RateLimitMiddleware(middleware.NewRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst), handler)
	handler = httpx.BodyLimitMiddleware(cfg.MaxBodyBytes, handler)
	handler = httpx.CORSMiddleware(cfg.CORSOrigins, handler)
	handler = httpx.RequestIDMiddleware(handler)
	handler = httpx.SecurityHeadersMiddleware(handler)
	handler = httpx.RecoverMiddleware(log, handler)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Info("api listening", "addr", cfg.HTTPAddr)

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownPeriod)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("api stopped cleanly")
	return nil
}
