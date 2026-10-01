package identity

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRefreshCookieAttributes pins the refresh-cookie contract (issue #83):
// Path must be "/" (web navigations and the ADR-011 middleware guard observe
// the session; the cookie is HttpOnly and carries no readable secret), and
// SameSite must come from operator config rather than a hardcoded Lax.
// Secure stays gated by the constructor flag (production HTTPS deployments).
func TestRefreshCookieAttributes(t *testing.T) {
	cases := []struct {
		name         string
		sameSite     http.SameSite
		wantAttrText string
	}{
		{"default lax", http.SameSiteLaxMode, "SameSite=Lax"},
		{"operator-configured none (split-domain)", http.SameSiteNoneMode, "SameSite=None"},
		{"operator-configured strict", http.SameSiteStrictMode, "SameSite=Strict"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// secureCookies=true mirrors a production HTTPS deployment.
			h := NewHandler(nil, nil, nil, true, tc.sameSite)

			rr := httptest.NewRecorder()
			h.setRefreshCookie(rr, "refresh-token-value")
			res := rr.Result()

			var c *http.Cookie
			for _, ck := range res.Cookies() {
				if ck.Name == refreshCookieName {
					c = ck
				}
			}
			if c == nil {
				t.Fatalf("skolara_refresh not set; headers: %v", res.Header)
			}
			if c.Path != "/" {
				t.Fatalf("Path = %q, want \"/\" (issue #83)", c.Path)
			}
			if !c.HttpOnly {
				t.Fatal("HttpOnly missing")
			}
			if !c.Secure {
				t.Fatal("Secure missing although handler built with secureCookies=true")
			}
			if c.SameSite != tc.sameSite {
				t.Fatalf("SameSite = %v, want %v", c.SameSite, tc.sameSite)
			}
			if c.Value != "refresh-token-value" {
				t.Fatalf("cookie value = %q", c.Value)
			}

			raw := res.Header.Get("Set-Cookie")
			if !strings.Contains(raw, tc.wantAttrText) {
				t.Fatalf("Set-Cookie %q missing %s", raw, tc.wantAttrText)
			}
			if strings.Contains(raw, "Path=/api/v1/auth") {
				t.Fatalf("Set-Cookie still carries the old narrow path: %s", raw)
			}

			// The unset cookie must repeat every attribute (Path/SameSite/
			// Secure) or browsers refuse to delete the set cookie.
			rr2 := httptest.NewRecorder()
			h.unsetRefreshCookie(rr2)
			raw2 := rr2.Result().Header.Get("Set-Cookie")
			for _, want := range []string{"Path=/;", "HttpOnly", tc.wantAttrText, "Max-Age=0"} {
				if !strings.Contains(raw2, want) {
					t.Fatalf("unset Set-Cookie %q missing %q", raw2, want)
				}
			}
		})
	}
}
