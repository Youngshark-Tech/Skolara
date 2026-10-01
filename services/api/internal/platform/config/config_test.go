package config

import (
	"os"
	"testing"
	"time"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestLoadDefaultsDevelopment(t *testing.T) {
	for _, k := range []string{"SKOLARA_ENV", "SKOLARA_JWT_SECRET", "SKOLARA_CORS_ORIGINS"} {
		os.Unsetenv(k)
	}
	t.Setenv("DATABASE_URL", "postgres://localhost/skolara_dev")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != EnvDevelopment {
		t.Errorf("env = %v, want development", cfg.Env)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("addr = %v", cfg.HTTPAddr)
	}
	if cfg.AccessTokenExpiry != 15*time.Minute {
		t.Errorf("access expiry = %v", cfg.AccessTokenExpiry)
	}
	if len(cfg.JWTSecret) < 32 {
		t.Errorf("dev fallback secret too short")
	}
}

func TestLoadProductionRequiresSecrets(t *testing.T) {
	setEnv(t, map[string]string{
		"SKOLARA_ENV":  "production",
		"DATABASE_URL": "postgres://x",
	})
	t.Setenv("SKOLARA_JWT_SECRET", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error: production requires >=32-byte JWT secret")
	}

	setEnv(t, map[string]string{"SKOLARA_JWT_SECRET": "production-grade-secret-with-more-than-32-bytes!"})
	if _, err := Load(); err == nil {
		t.Fatal("expected error: production must configure CORS origins")
	}
}

func TestLoadProductionValid(t *testing.T) {
	setEnv(t, map[string]string{
		"SKOLARA_ENV":            "production",
		"DATABASE_URL":           "postgres://x",
		"SKOLARA_JWT_SECRET":     "production-grade-secret-with-more-than-32-bytes!",
		"SKOLARA_WEBHOOK_SECRET": "production-webhook-hmac-0123456789abcdef",
		"SKOLARA_CORS_ORIGINS":   "https://app.skolara.com, https://admin.skolara.com",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.CORSOrigins) != 2 {
		t.Errorf("cors origins = %v", cfg.CORSOrigins)
	}
	if cfg.WebhookSecret != "production-webhook-hmac-0123456789abcdef" {
		t.Errorf("webhook secret not loaded from environment")
	}
}

func TestLoadProductionRequiresWebhookSecret(t *testing.T) {
	setEnv(t, map[string]string{
		"SKOLARA_ENV":          "production",
		"DATABASE_URL":         "postgres://x",
		"SKOLARA_JWT_SECRET":   "production-grade-secret-with-more-than-32-bytes!",
		"SKOLARA_CORS_ORIGINS": "https://app.skolara.com",
	})
	for _, secret := range []string{"", "short", "0123456789abcdef0123456789abcde"} { // 0, 5, 31 bytes
		t.Setenv("SKOLARA_WEBHOOK_SECRET", secret)
		if _, err := Load(); err == nil {
			t.Fatalf("expected error for webhook secret of %d bytes", len(secret))
		}
	}

	t.Setenv("SKOLARA_WEBHOOK_SECRET", "0123456789abcdef0123456789abcdef") // boundary: exactly 32
	if _, err := Load(); err != nil {
		t.Fatalf("unexpected error for 32-byte webhook secret: %v", err)
	}
}

func TestLoadDevelopmentWebhookSecretOptional(t *testing.T) {
	setEnv(t, map[string]string{
		"SKOLARA_ENV":  "development",
		"DATABASE_URL": "postgres://localhost/skolara_dev",
	})
	t.Setenv("SKOLARA_WEBHOOK_SECRET", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WebhookSecret != "" {
		t.Errorf("dev webhook secret = %q, want empty (dev defaults unchanged)", cfg.WebhookSecret)
	}
}

func TestLoadInvalidEnv(t *testing.T) {
	setEnv(t, map[string]string{"SKOLARA_ENV": "staging"})
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid environment")
	}
}

func TestLoadMissingDatabaseURLErrors(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	t.Setenv("SKOLARA_ENV", "development")
	if _, err := Load(); err == nil {
		t.Fatal("expected error: DATABASE_URL required outside test env")
	}
}
