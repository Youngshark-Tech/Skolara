package identity

import (
	"strings"
	"testing"
	"time"
)

func TestHashPasswordAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("not argon2id: %s", hash)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("wrong password accepted")
	}
}

func TestHashPasswordTooShort(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
}

func TestVerifyPasswordMalformedHash(t *testing.T) {
	if VerifyPassword("not-a-hash", "x") {
		t.Fatal("malformed hash verified")
	}
	if VerifyPassword("$argon2id$v=19$m=1,t=1,p=1$AAA$AAA", "x") {
		t.Fatal("corrupt hash verified")
	}
	// bcrypt-style should fail
	if VerifyPassword("$2a$10$abcdefghijklmnopqrstuv", "x") {
		t.Fatal("bcrypt hash verified against argon2")
	}
}

func TestJWTIssueVerify(t *testing.T) {
	m := NewJWTManager("test-secret-that-is-at-least-32-bytes-long!!", time.Minute)
	token, err := m.Issue(SessionClaims{UserID: "u1", Email: "u1@x.y", Roles: []string{"teacher"}})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := m.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.UserID != "u1" || claims.Email != "u1@x.y" || len(claims.Roles) != 1 || claims.Roles[0] != "teacher" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestJWTExpired(t *testing.T) {
	m := NewJWTManager("test-secret-that-is-at-least-32-bytes-long!!", -time.Minute) // already expired
	token, err := m.Issue(SessionClaims{UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Verify(token); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestJWTWrongSecret(t *testing.T) {
	m1 := NewJWTManager("secret-one-secret-one-secret-one-32!", time.Minute)
	m2 := NewJWTManager("secret-two-secret-two-secret-two-32!", time.Minute)
	token, _ := m1.Issue(SessionClaims{UserID: "u1"})
	if _, err := m2.Verify(token); err == nil {
		t.Fatal("token from other secret accepted")
	}
}

func TestJWTAlgorithmConfusion(t *testing.T) {
	m := NewJWTManager("test-secret-that-is-at-least-32-bytes-long!!", time.Minute)
	token, _ := m.Issue(SessionClaims{UserID: "u1"})
	// "none" algorithm attack: token with alg none should be rejected
	noneToken := token[:strings.LastIndex(token, ".")] + "."
	if _, err := m.Verify(noneToken); err == nil {
		t.Fatal("alg-none token accepted")
	}
}

func TestOpaqueTokenUniqueness(t *testing.T) {
	a, _ := NewOpaqueToken()
	b, _ := NewOpaqueToken()
	if a == b {
		t.Fatal("tokens not unique")
	}
	if HashToken(a) == HashToken(b) {
		t.Fatal("hashes not unique")
	}
}
