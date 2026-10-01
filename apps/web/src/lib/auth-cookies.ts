/**
 * Cookie + path facts shared between the middleware and the session layer.
 *
 * REFRESH_COOKIE_NAME must match `refreshCookieName` in
 * services/api/internal/identity/http.go exactly. The API scopes that cookie
 * to Path=/api/v1/auth, so on navigation requests the browser does not send it
 * to the web app; the web-origin hint cookie (set on login, cleared on
 * logout/session expiry, contains no secret) gives the middleware a reliable
 * presence signal today. If the refresh cookie ever widens to Path=/ (API-side
 * follow-up), the guard observes it directly with no change here.
 */

export const REFRESH_COOKIE_NAME = "skolara_refresh";
export const AUTH_HINT_COOKIE_NAME = "skolara_auth_hint";

const PUBLIC_PATHS = new Set(["/login", "/api/health", "/favicon.ico"]);

/** Public paths never hit the auth guard. */
export function isPublicPath(pathname: string): boolean {
  if (PUBLIC_PATHS.has(pathname)) return true;
  return (
    pathname.startsWith("/_next") ||
    pathname.startsWith("/__next") ||
    // Next.js App Router static assets that are not versioned under /_next.
    /\.(?:svg|png|jpg|jpeg|gif|webp|ico|txt|xml|webmanifest)$/i.test(pathname)
  );
}

/**
 * Pure auth-guard decision so it stays unit-testable without NextRequest:
 * presence check ONLY — the API validates the real session on every call.
 */
export function decideAuth(
  pathname: string,
  hasRefreshCookie: boolean,
  hasHintCookie: boolean,
): "allow" | "redirect" {
  if (isPublicPath(pathname)) return "allow";
  if (hasRefreshCookie || hasHintCookie) return "allow";
  return "redirect";
}
