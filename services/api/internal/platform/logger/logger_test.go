package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestSecretRedactionActive guards the issue #53 wiring: forbidden keys are
// redacted at any group depth in real JSON output.
func TestSecretRedactionActive(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: SafeLogValue})
	log := slog.New(h)
	log.Info("login attempt",
		"email", "user@school.example",
		"password", "super-secret",
		slog.Group("request",
			"authorization", "Bearer abc.def.ghi",
			"path", "/api/v1/auth/login",
		),
	)
	line := buf.String()
	if strings.Contains(line, "super-secret") || strings.Contains(line, "Bearer abc.def.ghi") {
		t.Fatalf("secrets leaked to logs: %s", line)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if decoded["password"] != "[REDACTED]" {
		t.Fatalf("top-level password not redacted: %v", decoded["password"])
	}
	grp, _ := decoded["request"].(map[string]any)
	if grp == nil || grp["authorization"] != "[REDACTED]" {
		t.Fatalf("nested authorization not redacted: %v", grp)
	}
	if decoded["email"] != "user@school.example" {
		t.Fatalf("non-secret fields must pass through: %v", decoded["email"])
	}
}
