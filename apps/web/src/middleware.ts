import { NextResponse, type NextRequest } from "next/server";
import {
  AUTH_HINT_COOKIE_NAME,
  REFRESH_COOKIE_NAME,
  decideAuth,
} from "@/lib/auth-cookies";

/**
 * Edge auth guard (ADR-008 §Auth / ADR-011): protected paths require evidence
 * of a session cookie before we even ship the app shell — unauthenticated
 * deep links get a 307 to /login instead of a client-side bounce after
 * hydration. This is a PRESENCE CHECK ONLY; the API validates the real session
 * (and the app still boots via silent refresh) on every request.
 *
 * Evidence accepted: the API's HttpOnly refresh cookie (`skolara_refresh`, set
 * by services/api/internal/identity/http.go) or the web-origin session-hint
 * cookie (`skolara_auth_hint`, no secret value — see lib/session.tsx).
 */
export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;
  const decision = decideAuth(
    pathname,
    request.cookies.has(REFRESH_COOKIE_NAME),
    request.cookies.has(AUTH_HINT_COOKIE_NAME),
  );
  if (decision === "allow") return NextResponse.next();

  const url = request.nextUrl.clone();
  url.pathname = "/login";
  url.search = "";
  // Preserve the requested deep link so the login surface can return the user
  // to it after signing in; the client sanitizes the value before honoring it
  // (lib/next-path.ts — the middleware only forwards, never trusts).
  const target = `${request.nextUrl.pathname}${request.nextUrl.search}`;
  if (target !== "/") url.searchParams.set("next", target);
  return NextResponse.redirect(url, 307);
}

export const config = {
  // Skip Next internals and static assets; everything else goes through the
  // guard (public paths are decided in lib/auth-cookies.ts).
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
