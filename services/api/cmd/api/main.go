// Command api is the Skolara API composition root. It is the ONLY place where
// concrete platform and domain implementations are wired together (ADR-001).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/config"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/events"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/httpx"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/logger"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/middleware"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/observability"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/students"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
	migrations "github.com/Roy-Wanyoike/Skolara/services/api/migrations"
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

	// Self-migrate from the embedded schema at startup (production-safe).
	if err := postgres.MigrateUpFS(cfg.DatabaseURL, migrations.FS); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	// Identity bounded context wiring.
	idRepo := identity.NewRepo(pool)
	jwtMgr := identity.NewJWTManager(cfg.JWTSecret, cfg.AccessTokenExpiry)
	authSvc := identity.NewAuthService(idRepo, jwtMgr)
	idHandler := identity.NewHandler(authSvc, jwtMgr, pool)

	if err := bootstrapPlatformAdmin(ctx, authSvc, log); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}

	// Tenancy bounded context wiring.
	tenSvc := tenancy.NewService(tenancy.NewRepo(pool), pool)
	tenHandler := tenancy.NewHandler(tenSvc)

	// Students bounded context wiring.
	stuSvc := students.NewService(students.NewRepo(pool), pool)
	stuHandler := students.NewHandler(stuSvc)

	// Effective permission set = platform roles (identity) ∪ active
	// membership roles (tenancy). Wired here at the composition root so
	// neither domain imports the other.
	resolver := combinedResolver{platform: authSvc, membership: tenSvc}

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
	idHandler.Register(root)
	tenHandler.Register(root, jwtMgr, resolver)
	stuHandler.Register(root, jwtMgr, resolver)

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

// bootstrapPlatformAdmin creates the initial platform administrator when the
// users table is empty and SKOLARA_BOOTSTRAP_ADMIN_* is configured.
func bootstrapPlatformAdmin(ctx context.Context, svc *identity.AuthService, log *slog.Logger) error {
	email := os.Getenv("SKOLARA_BOOTSTRAP_ADMIN_EMAIL")
	password := os.Getenv("SKOLARA_BOOTSTRAP_ADMIN_PASSWORD")
	if email == "" || password == "" {
		return nil
	}
	u, err := svc.CreateUser(ctx, email, "Platform Admin", password, identity.RolePlatformAdmin)
	if err != nil {
		if errors.Is(err, identity.ErrEmailTaken) {
			log.Info("bootstrap admin already exists")
			return nil
		}
		return err
	}
	_ = svc.Audit(ctx, identity.AuditEntry{
		ActorID: u.ID, Action: "user.bootstrapped", ResourceType: "user", ResourceID: u.ID,
	})
	log.Info("bootstrap platform admin created", "email", email)
	return nil
}

// combinedResolver unions permission sets from platform roles and school
// membership roles (ADR-006/007 authorization model).
type combinedResolver struct {
	platform   identity.PermissionResolver
	membership identity.PermissionResolver
}

func (c combinedResolver) PermissionsFor(ctx context.Context, userID string) (map[string]bool, error) {
	perms, err := c.platform.PermissionsFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	mp, err := c.membership.PermissionsFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	for k, v := range mp {
		if v {
			perms[k] = true
		}
	}
	return perms, nil
}
