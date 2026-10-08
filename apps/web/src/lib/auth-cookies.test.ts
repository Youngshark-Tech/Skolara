import { describe, it, expect } from "vitest";
import {
  AUTH_HINT_COOKIE_NAME,
  REFRESH_COOKIE_NAME,
  decideAuth,
  isPublicPath,
} from "./auth-cookies";

describe("auth-cookies (middleware guard facts)", () => {
  it("matches the API refresh cookie name exactly", () => {
    // Contract with services/api/internal/identity/http.go (refreshCookieName).
    expect(REFRESH_COOKIE_NAME).toBe("skolara_refresh");
    expect(AUTH_HINT_COOKIE_NAME).toBe("skolara_auth_hint");
  });

  it("treats public paths as always allowed", () => {
    expect(isPublicPath("/")).toBe(true); // #129: public landing page
    expect(isPublicPath("/login")).toBe(true);
    expect(isPublicPath("/signup")).toBe(true); // #130: public signup
    expect(isPublicPath("/api/health")).toBe(true);
    expect(isPublicPath("/favicon.ico")).toBe(true);
    expect(isPublicPath("/_next/static/chunks/main.js")).toBe(true);
    expect(isPublicPath("/brand/logo.png")).toBe(true);
  });

  it("redirects protected paths without session-cookie evidence", () => {
    expect(decideAuth("/students", false, false)).toBe("redirect");
    expect(decideAuth("/finance", false, false)).toBe("redirect");
    // "/" became public with #129 (landing page); the workspace moved to
    // /dashboard which stays protected.
    expect(decideAuth("/", false, false)).toBe("allow");
    expect(decideAuth("/dashboard", false, false)).toBe("redirect");
  });

  it("allows protected paths with a refresh or hint cookie present", () => {
    expect(decideAuth("/students", true, false)).toBe("allow");
    expect(decideAuth("/students", false, true)).toBe("allow");
    // Presence check ONLY: an empty-page shell must never guard on content.
    expect(decideAuth("/login", false, false)).toBe("allow");
  });
});
