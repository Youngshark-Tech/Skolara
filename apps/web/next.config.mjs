/** @type {import('next').NextConfig} */

// CSP + client URL derivation share ONE implementation (issue #135): the CSP
// connect-src directive tracks exactly where the client calls the API. The
// resolution rules live in src/lib/api-url.mjs (resolveApiUrl, #127) —
// documented there.
import { buildCsp } from "./src/lib/api-url.mjs";
import { resolveDemoModeFlag } from "./src/lib/build-flags.mjs";

const isProd = process.env.NODE_ENV === "production";

const csp = buildCsp({
  apiUrl: process.env.NEXT_PUBLIC_API_URL,
  isProduction: isProd,
});

const securityHeaders = [
  { key: "Content-Security-Policy", value: csp },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
];

// HSTS only in production: dev runs on plain HTTP.
if (isProd) {
  securityHeaders.push({
    key: "Strict-Transport-Security",
    value: "max-age=63072000; includeSubDomains; preload",
  });
}

const nextConfig = {
  output: "standalone",
  reactStrictMode: true,
  // Zero-config demo default (issue #153 — TEMPORARY, go-live via explicit
  // NEXT_PUBLIC_DEMO_MODE=false): a build with NO configuration must ship a
  // WORKING demo (in-memory sample data, open access) instead of a login form
  // pointed at an API with no database. The injection happens ONLY when the
  // operator expressed no preference (unset or empty) — an explicit value
  // always wins, byte-for-byte. lib/auth-bypass.ts keeps its strict parsing;
  // the security-sensitive decision (what an absent flag means) lives in
  // src/lib/build-flags.mjs and is unit-pinned in build-flags.test.ts.
  //
  // GO-LIVE: set NEXT_PUBLIC_DEMO_MODE=false (+ DATABASE_URL on the API) and
  // redeploy — release blocker docs/operations/RUNBOOK.md §go-live.
  // Vitest does not read next.config, so unit tests keep strict opt-in
  // semantics in lib/auth-bypass.ts.
  env: {
    NEXT_PUBLIC_DEMO_MODE: resolveDemoModeFlag(process.env.NEXT_PUBLIC_DEMO_MODE),
  },
  async headers() {
    return [
      {
        source: "/:path*",
        headers: securityHeaders,
      },
    ];
  },
};

export default nextConfig;
