/**
 * Single source of truth for the web's API base-URL resolution (issue #135).
 *
 * Plain .mjs so BOTH the client bundle (lib/api.ts) and the Node-side
 * next.config.mjs (CSP header derivation) share one implementation — the CSP
 * connect-src directive must always match where the client actually calls.
 * Tests: src/lib/api-url.test.ts pins the four arms; the CSP test pins the
 * header derivation.
 */

/**
 * Resolve the API base URL (issue #127):
 * - An explicitly set NEXT_PUBLIC_API_URL always wins. It may be a full URL
 *   (split deployment, e.g. "https://api.example.com") or an empty string
 *   (same-origin calls behind a reverse proxy / Vercel Services rewrite).
 * - Otherwise production builds default to SAME-ORIGIN (""): the Vercel
 *   Services rewrite routes /api/* to the Go service on the shared domain, so
 *   relative URLs are correct and the refresh cookie stays first-party.
 * - Dev keeps the local Go server default of http://localhost:8080.
 * A localhost default previously left production browsers calling the
 * user's own machine — why login silently failed on the first Vercel deploy.
 * @param {string | undefined} explicit
 * @param {boolean} isProduction
 * @returns {string}
 */
export function resolveApiUrl(explicit, isProduction) {
  if (explicit !== undefined) return explicit;
  return isProduction ? "" : "http://localhost:8080";
}

/**
 * Build the Content-Security-Policy header value (issues #55, #135).
 * connect-src always includes 'self' plus the resolved API origin when the
 * client calls cross-origin; 'unsafe-eval' is appended to script-src in dev
 * ONLY (React's dev runtime requires eval; production stays eval-free).
 * @param {{apiUrl: string | undefined, isProduction: boolean}} opts
 * @returns {string}
 */
export function buildCsp({ apiUrl, isProduction }) {
  const apiOrigin = resolveApiUrl(apiUrl, isProduction);
  const connectSrc = apiOrigin ? `connect-src 'self' ${apiOrigin}` : "connect-src 'self'";
  const scriptSrc = `script-src 'self' 'unsafe-inline'${isProduction ? "" : " 'unsafe-eval'"}`;
  return [
    "default-src 'self'",
    // Next.js App Router bootstraps hydration through inline <script> tags in
    // the HTML shell (self-hosting + nonces is the follow-up hardening step).
    scriptSrc,
    // 'unsafe-inline' for styles covers Tailwind-injected style tags and the
    // styled-jsx dev runtime.
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data:",
    "font-src 'self'",
    connectSrc,
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join("; ");
}
