# ADR-011: Web — Memory-Only Access Token, Silent Refresh, Middleware Guard

**Status:** Accepted · **Date:** 2026-10 · **Supersedes (in part):** ADR-008 §Auth · **Issues:** #54, #55, #56 (web slice)

## Context

Audit round 2 verified four defects on the web auth surface:

1. **Token in localStorage** (`skolara_access_token`) contradicted ADR-008 ("no tokens in localStorage"): any XSS payload read a long-lived session.
2. **Concurrent 401 refresh race**: every 401 independently called `/auth/refresh`. The refresh endpoint rotates the refresh cookie on every call and revokes the token family on reuse (ADR-007 hardening, #41), so concurrent 401s — e.g. the dashboard firing 3 parallel calls on token expiry — presented an already-rotated token and hard-logged the user out.
3. **Dead session state**: after a final 401 the token was cleared but the SessionProvider was never notified — `me` stayed set, every action 401'd, and no redirect happened. `logout()` also left the persisted `activeSchoolId` in localStorage (shared-machine tenant-context leak).
4. **No server-side gating and no security headers**: auth was client-side post-hydration only (ADR-008 mandates middleware guards), and `next.config.mjs` carried no CSP/HSTS/X-Frame-Options at all.

Facts coded against (verified in `services/api/internal/identity/http.go`, read-only):

- Refresh cookie: `skolara_refresh`, `Path=/api/v1/auth`, `HttpOnly`, `SameSite=Lax`, `Secure` in production.
- `POST /api/v1/auth/refresh` → `200 {accessToken, tokenType, expiresIn}` + rotated cookie; `401` when rejected (incl. family revocation).
- `POST /api/v1/auth/login` → same body + sets cookie. `POST /api/v1/auth/logout` → `204` + unsets cookie.

## Decision

- **Access token is memory-only.** A module-scoped variable in `lib/api.ts` replaces all localStorage persistence (the non-secret school-id preference is still persisted). ADR-008's original intent is restored.
- **Silent refresh on boot.** After every full page load (token lost with memory), the SessionProvider exchanges the refresh cookie for a fresh access token before loading `/me`. Cost: one extra round trip per hard navigation; benefit: nothing stealable persists.
- **Single-flight refresh.** One shared in-flight promise coalesces all concurrent 401 refreshes; retries queue behind it, so exactly one cookie rotation happens per expiry window (regression-tested).
- **Normalized refresh outcomes.** `refreshSession()` never throws: `{ok:true}` or `{ok:false, reason:"unauthenticated"|"network"}`. Network failures no longer escape as raw `TypeError`s, and bootstrap distinguishes a 401 (logged out) from a network blip (one silent retry, then an explicit error state with a retry button — never a silent logout).
- **Session-expired callback.** The api client notifies the provider exactly when the refresh cookie is definitively rejected; the provider resets all state (including the persisted school id) and `router.replace("/login")`. `logout()` clears the same set.
- **Middleware presence guard** (`src/middleware.ts`): protected paths require evidence of a session cookie, else `307 → /login`. Presence check ONLY — the API validates the real session. Evidence: the API's `skolara_refresh` cookie, or a web-origin hint cookie (`skolara_auth_hint`, value `1`, no secret) that `session.tsx` sets on login and clears on logout/expiry. The hint is needed because the API scopes `skolara_refresh` to `Path=/api/v1/auth`, so browsers never send it on web navigation requests; if the API ever widens the path (API-side follow-up), the guard observes it directly with no change here. The hint grants nothing by itself: it only pre-renders a shell whose API calls still require a valid token.
- **Security headers** in `next.config.mjs`: CSP (`default-src 'self'`, `script-src 'self' 'unsafe-inline'` for the Next.js hydration bootstrap, `style-src 'self' 'unsafe-inline'` for Tailwind, `img-src 'self' data:`, `connect-src 'self' $NEXT_PUBLIC_API_URL`, `frame-ancestors 'none'`, `object-src 'none'`, `base-uri 'self'`, `form-action 'self'`), HSTS in production, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`, minimal `Permissions-Policy`.
- **Build correctness (#56 web slice)**: the Dockerfile takes `NEXT_PUBLIC_API_URL` as a build arg before `npm run build` and fails fast when unset; `/api/health` liveness route + container `HEALTHCHECK`.

## Consequences

- **Positive:** XSS can no longer exfiltrate a persistent session credential; token-expiry no longer hard-logs users out; server-side gating exists; security headers baseline in place; production builds cannot silently ship a localhost API URL.
- **Negative:** one extra refresh round trip per hard navigation; `'unsafe-inline'` script CSP remains until Next.js nonce-based CSP is adopted (follow-up); the middleware guard is only as strong as the hint-cookie signal until the API widens the refresh-cookie path; `SameSite=Lax` requires same-site deployments (or an API change to `SameSite=None; Secure`) for split-site hosting such as separate Vercel domains — documented in `apps/web/.env.example`.
