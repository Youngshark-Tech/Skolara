package signup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
)

// TestGenerateSchoolCodeFormat pins the SL-XXXXXX shape: generated from the
// lookalike-free alphabet, unique across many draws.
func TestGenerateSchoolCodeFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code := generateSchoolCode()
		if len(code) != 9 || !strings.HasPrefix(code, "SL-") {
			t.Fatalf("code = %q, want SL-XXXXXX", code)
		}
		suffix := code[3:]
		for _, c := range suffix {
			if strings.ContainsRune("0O1IL", c) {
				t.Fatalf("code %q contains a lookalike character", code)
			}
		}
		if seen[code] {
			t.Fatalf("duplicate code generated: %q", code)
		}
		seen[code] = true
	}
}

// TestGroupDisplayName derives the owning group's name from the school.
func TestGroupDisplayName(t *testing.T) {
	if got := groupDisplayName("Riverside High School"); got != "Riverside High School Group" {
		t.Errorf("groupDisplayName = %q", got)
	}
}

// TestRunValidationBeforeDatabase pins that malformed inputs are rejected with
// identity.ErrValidation BEFORE any repository access (the service is built
// with a nil pool here — a DB touch would panic and fail the test).
func TestRunValidationBeforeDatabase(t *testing.T) {
	svc := New(nil, nil, nil) // nil pool/repos: valid inputs must never reach them
	cases := []struct {
		name string
		in   Input
	}{
		{"school too short", Input{SchoolName: "S", AdminName: "A", Email: "a@b.example", Password: "longenough1"}},
		{"school missing", Input{SchoolName: "", AdminName: "A", Email: "a@b.example", Password: "longenough1"}},
		{"admin missing", Input{SchoolName: "Valid School", AdminName: "", Email: "a@b.example", Password: "longenough1"}},
		{"email missing @", Input{SchoolName: "Valid School", AdminName: "A", Email: "not-an-email", Password: "longenough1"}},
		{"email with whitespace", Input{SchoolName: "Valid School", AdminName: "A", Email: "a b@example.com", Password: "longenough1"}},
		{"short password", Input{SchoolName: "Valid School", AdminName: "A", Email: "a@b.example", Password: "short"}},
	}
	for _, tc := range cases {
		_, err := svc.run(context.Background(), tc.in)
		if !errors.Is(err, identity.ErrValidation) {
			t.Errorf("%s: err = %v, want identity.ErrValidation", tc.name, err)
		}
	}
}
