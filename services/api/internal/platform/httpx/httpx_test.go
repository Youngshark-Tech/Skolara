package httpx

import (
	"encoding/json"
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
