import { NextResponse, type NextRequest } from "next/server";
import {
  AUTH_HINT_COOKIE_NAME,
  REFRESH_COOKIE_NAME,
  decideAuth,
} from "@/lib/auth-cookies";
import { authBypassEnabled } from "@/lib/auth-bypass";

/**
 * Edge auth guard (proxy — Next 16 renamed middleware.ts -> proxy.ts) (ADR-008 §Auth / ADR-011): protected paths require evidence
 * of a session cookie before we even ship the app shell — unauthenticated
 * deep links get a 307 to /login instead of a client-side bounce after
 * hydration. This is a PRESENCE CHECK ONLY; the API validates the real session
 * (and the app still boots via silent refresh) on every request.
 *
 * Evidence accepted: the API's HttpOnly refresh cookie (`skolara_refresh`, set
 * by services/api/internal/identity/http.go) or the web-origin session-hint
 * cookie (`skolara_auth_hint`, no secret value — see lib/session.tsx).
 *
 * Open-access mode (#141): when NEXT_PUBLIC_AUTH_BYPASS=true the guard steps
 * aside entirely — the session provider establishes a real demo session on
 * boot (lib/session.tsx), so the API's validation is never skipped.
 */
export default function proxy(request: NextRequest) {
  if (authBypassEnabled()) return NextResponse.next();

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
  // MUST stay a static literal (Next parses proxy entries statically — a
  // shared constant fails the build). Skips Next internals, static assets,
  // and ALL /api/* paths: those belong to the API service. In the Vercel
  // topology the top-level rewrite routes /api/* to the Go service before
  // this middleware runs, but in same-origin setups without that rewrite
  // (local `next start`, third-party proxies) letting the guard 307 an API
  // call into /login HTML masked real errors as HTML 200s (#136).
  matcher: ["/((?!_next/static|_next/image|favicon.ico|api/).*)"],
};
