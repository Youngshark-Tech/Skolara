package httpx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDAssignedAndEchoed(t *testing.T) {
	var captured string
	h := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = RequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if captured == "" {
		t.Fatal("no request id injected into context")
	}
	if got := rec.Header().Get("X-Request-ID"); got != captured {
		t.Fatalf("response id %s != context id %s", got, captured)
	}
}

func TestRequestIDPropagatedFromClient(t *testing.T) {
	h := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "client-provided-id")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); got != "client-provided-id" {
		t.Fatalf("client id not propagated: %s", got)
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	h := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	for _, hdr := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Content-Security-Policy"} {
		if rec.Header().Get(hdr) == "" {
			t.Errorf("missing security header %s", hdr)
		}
	}
}

func TestErrorEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	BadRequest(rec, "invalid input", Detail{Field: "email", Issue: "required"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json envelope: %v", err)
	}
	if body.Error.Code != "bad_request" || body.Error.Message != "invalid input" {
		t.Fatalf("envelope = %+v", body.Error)
	}
	if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "email" {
		t.Fatalf("details = %+v", body.Error.Details)
	}
}

func TestCORSAllowList(t *testing.T) {
	h := CORSMiddleware([]string{"http://localhost:3000"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("disallowed origin received CORS headers")
	}

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("Origin", "http://localhost:3000")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatal("allowed origin missing CORS header")
	}
}

func TestCORSPreflight(t *testing.T) {
	h := CORSMiddleware([]string{"http://localhost:3000"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("preflight should not reach handler")
	}))
	req := httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status %d", rec.Code)
	}
}

func TestRecoverMiddleware(t *testing.T) {
	h := RecoverMiddleware(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic not converted to 500: %d", rec.Code)
	}
	var body errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error.Code != "internal" {
		t.Fatalf("panic leak: %+v", body.Error)
	}
}

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	var p payload
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"x","admin":true}`))
	rec := httptest.NewRecorder()
	if err := DecodeJSON(rec, req, &p); err == nil {
		t.Fatal("unknown field accepted — mass-assignment risk")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestBodyLimitMiddleware(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	h := BodyLimitMiddleware(10, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p payload
		if err := DecodeJSON(w, r, &p); err == nil {
			t.Error("oversized body accepted")
		}
	}))
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"way":"too much payload for the limit"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized body status %d", rec.Code)
	}
}

func TestAccessLogEmitsScopeAndRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate deeper layers annotating the scope.
		if sc := ScopeFrom(r.Context()); sc != nil {
			sc.ActorID = "user-1"
			sc.TenantID = "school-1"
		}
		// Simulate request-id middleware having run (sets response header).
		w.Header().Set("X-Request-ID", "req-42")
		w.WriteHeader(http.StatusCreated)
	})

	handler := AccessLogMiddleware(logger, inner)
	req := httptest.NewRequest("POST", "/api/v1/things", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	line := buf.String()
	for _, want := range []string{`"request_id":"req-42"`, `"actor_id":"user-1"`, `"school_id":"school-1"`, `"status":201`, `"method":"POST"`, `"path":"/api/v1/things"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line missing %s: %s", want, line)
		}
	}
}

func TestAccessLogWithoutScopeStillLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := AccessLogMiddleware(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest("GET", "/healthz", nil))
	if !strings.Contains(buf.String(), `"status":200`) {
		t.Fatalf("missing status in log: %s", buf.String())
	}
}
