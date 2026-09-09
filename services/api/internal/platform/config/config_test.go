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
		"SKOLARA_ENV":          "production",
		"DATABASE_URL":         "postgres://x",
		"SKOLARA_JWT_SECRET":   "production-grade-secret-with-more-than-32-bytes!",
		"SKOLARA_CORS_ORIGINS": "https://app.skolara.com, https://admin.skolara.com",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.CORSOrigins) != 2 {
		t.Errorf("cors origins = %v", cfg.CORSOrigins)
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
