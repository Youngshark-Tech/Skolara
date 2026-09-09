package contract

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/academics"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/assignments"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/attendance"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/finance"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/students"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
	"gopkg.in/yaml.v3"
)

// pathItem mirrors only what the drift test needs from the OpenAPI document.
type pathItem map[string]any

type specDoc struct {
	Paths map[string]pathItem `yaml:"paths"`
}

// TestDocumentedRoutesExist is the contract drift gate: every path+method in
// packages/contracts/openapi.yaml MUST be mounted on the real router. A
// documented route that is missing from code would otherwise only surface as
// a 404 in production; here an unauthenticated request to an EXISTING route
// yields 401 (auth rejected first), while a MISSING route yields the root
// mux's 404 not_found envelope.
func TestDocumentedRoutesExist(t *testing.T) {
	specPath := locateSpec(t)
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var doc specDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if len(doc.Paths) < 40 {
		t.Fatalf("spec suspiciously small: %d paths — spec truncated?", len(doc.Paths))
	}

	mux := buildRouter()

	for path, item := range doc.Paths {
		for key := range item {
			method := strings.ToUpper(key)
			switch method {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			default:
				continue // parameters, summaries, etc.
			}
			reqPath := concretePath(path)
			req := httptest.NewRequest(method, reqPath, nil)
			// ServeMux.Handler resolves the registered pattern for the
			// request WITHOUT invoking any handler — safe with nil-backed
			// services and precise for existence checks.
			_, pattern := mux.Handler(req)
			if pattern == "" {
				t.Errorf("DRIFT: %s %s is documented in openapi.yaml but NOT mounted on the router", method, path)
			}
		}
	}
}

// buildRouter mounts every domain exactly like cmd/api/main.go but with nil
// pools — domain logic never runs because auth rejects unauthenticated
// requests before reaching handlers.
func buildRouter() *http.ServeMux {
	root := http.NewServeMux()
	root.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	root.Handle("/readyz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	root.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	jwt := identity.NewJWTManager("drift-check-secret-0123456789abcdef0123456789abcdef", 1<<30)
	var resolver identity.PermissionResolver = nilResolver{}

	idHandler := identity.NewHandler(identity.NewAuthService(identity.NewRepo(nil), jwt), jwt, nil)
	tenHandler := tenancy.NewHandler(tenancy.NewService(tenancy.NewRepo(nil), nil))
	stuHandler := students.NewHandler(students.NewService(students.NewRepo(nil), nil))
	acaHandler := academics.NewHandler(academics.NewService(academics.NewRepo(nil), nil))
	attHandler := attendance.NewHandler(attendance.NewService(attendance.NewRepo(nil), nil))
	asgHandler := assignments.NewHandler(assignments.NewService(assignments.NewRepo(nil), nil))
	finHandler := finance.NewHandler(finance.NewService(finance.NewRepo(nil), nil, "drift-webhook-secret-0123456789abcdef"))

	idHandler.Register(root)
	tenHandler.Register(root, jwt, resolver)
	stuHandler.Register(root, jwt, resolver)
	acaHandler.Register(root, jwt, resolver)
	attHandler.Register(root, jwt, resolver)
	asgHandler.Register(root, jwt, resolver)
	finHandler.Register(root, jwt, resolver)
	finHandler.RegisterWebhook(root)

	root.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"resource not found"}}`))
	})
	return root
}

type nilResolver struct{}

func (nilResolver) PermissionsFor(context.Context, string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func locateSpec(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		candidate := filepath.Join(dir, "packages", "contracts", "openapi.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("packages/contracts/openapi.yaml not found from " + wd)
		}
		dir = parent
	}
}

// concretePath turns an OpenAPI template into a requestable path.
func concretePath(path string) string {
	out := path
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			out = strings.Replace(out, seg, "00000000-0000-0000-0000-000000000000", 1)
		}
	}
	return out
}
