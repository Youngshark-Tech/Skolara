// Package config loads and validates all service configuration from the environment.
// Configuration is the ONLY sanctioned source of secrets and environment-specific settings.
package config

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment enumerates runtime environments.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvProduction  Environment = "production"
	EnvTest        Environment = "test"
)

// Config is the complete service configuration.
type Config struct {
	Env            Environment
	HTTPAddr       string
	HTTPTimeout    time.Duration
	ShutdownPeriod time.Duration

	DatabaseURL string
	RedisAddr   string

	JWTSecret          string
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration

	WebhookSecret string

	CORSOrigins []string

	LogLevel  string
	LogFormat string

	RateLimitRPS   float64
	RateLimitBurst int

	MaxBodyBytes int64

	// CookieSameSite is the SameSite attribute for the identity refresh
	// cookie (issue #83): "lax" (default), "strict", or "none". Split-domain
	// hosting (web and API on different sites) requires "none" — which
	// browsers only honor over HTTPS, i.e. Secure cookies in production.
	CookieSameSite string
}

func (c *Config) IsProd() bool { return c.Env == EnvProduction }

// Load reads configuration from the environment, applying safe defaults for development.
func Load() (*Config, error) {
	c := &Config{
		Env:                Environment(envOr("SKOLARA_ENV", "development")),
		HTTPAddr:           envOr("SKOLARA_HTTP_ADDR", ":8080"),
		HTTPTimeout:        durationOr("SKOLARA_HTTP_TIMEOUT", 30*time.Second),
		ShutdownPeriod:     durationOr("SKOLARA_SHUTDOWN_PERIOD", 15*time.Second),
		DatabaseURL:        envOr("DATABASE_URL", ""),
		RedisAddr:          envOr("SKOLARA_REDIS_ADDR", "127.0.0.1:6379"),
		JWTSecret:          os.Getenv("SKOLARA_JWT_SECRET"),
		AccessTokenExpiry:  durationOr("SKOLARA_ACCESS_TOKEN_EXPIRY", 15*time.Minute),
		RefreshTokenExpiry: durationOr("SKOLARA_REFRESH_TOKEN_EXPIRY", 30*24*time.Hour),
		WebhookSecret:      os.Getenv("SKOLARA_WEBHOOK_SECRET"),
		LogLevel:           envOr("SKOLARA_LOG_LEVEL", "info"),
		LogFormat:          envOr("SKOLARA_LOG_FORMAT", "json"),
		RateLimitRPS:       floatOr("SKOLARA_RATE_LIMIT_RPS", 20),
		RateLimitBurst:     intOr("SKOLARA_RATE_LIMIT_BURST", 40),
		MaxBodyBytes:       int64Or("SKOLARA_MAX_BODY_BYTES", 1<<20), // 1 MiB
		CookieSameSite:     strings.ToLower(strings.TrimSpace(envOr("SKOLARA_COOKIE_SAMESITE", "lax"))),
	}

	switch c.Env {
	case EnvDevelopment, EnvProduction, EnvTest:
	default:
		return nil, fmt.Errorf("config: invalid SKOLARA_ENV %q", c.Env)
	}

	// CORS
	raw := envOr("SKOLARA_CORS_ORIGINS", "http://localhost:3000")
	for _, o := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(o); t != "" {
			c.CORSOrigins = append(c.CORSOrigins, t)
		}
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	if c.DatabaseURL == "" && c.Env != EnvTest {
		return fmt.Errorf("config: DATABASE_URL is required")
	}
	if len(c.JWTSecret) < 32 {
		if c.IsProd() {
			return fmt.Errorf("config: SKOLARA_JWT_SECRET must be >= 32 bytes in production")
		}
		// Deterministic dev/test secret so local runs and unit tests work without setup.
		c.JWTSecret = envOr("SKOLARA_JWT_SECRET", "development-only-insecure-jwt-secret-000")
		if len(c.JWTSecret) < 32 {
			return fmt.Errorf("config: JWT secret shorter than 32 bytes")
		}
	}
	if c.IsProd() && c.CORSOrigins[0] == "http://localhost:3000" {
		return fmt.Errorf("config: SKOLARA_CORS_ORIGINS must be configured in production")
	}
	// Webhook HMAC secret: production refuses to boot on a missing or weak
	// secret, mirroring the JWT-secret policy above. Development and test may
	// run without one (webhook deliveries will 401 until it is configured).
	if c.IsProd() && len(c.WebhookSecret) < 32 {
		return fmt.Errorf("config: SKOLARA_WEBHOOK_SECRET must be >= 32 bytes in production")
	}
	switch c.CookieSameSite {
	case "lax", "strict", "none":
	default:
		return fmt.Errorf("config: SKOLARA_COOKIE_SAMESITE must be one of lax|strict|none, got %q", c.CookieSameSite)
	}
	return nil
}

// CookieSameSiteAttr maps the validated CookieSameSite setting onto the
// net/http constant the identity refresh cookie must carry (issue #83). The
// value is validated at Load time, so the default branch is unreachable for
// config built through Load.
func (c *Config) CookieSameSiteAttr() http.SameSite {
	switch c.CookieSameSite {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func durationOr(k string, d time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if dur, err := time.ParseDuration(v); err == nil {
			return dur
		}
	}
	return d
}

func floatOr(k string, d float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return d
}

func intOr(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return d
}

func int64Or(k string, d int64) int64 {
	if v := os.Getenv(k); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return d
}
