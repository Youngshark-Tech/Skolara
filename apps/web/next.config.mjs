/** @type {import('next').NextConfig} */

// API origin for the CSP connect-src directive. NEXT_PUBLIC_API_URL is baked
// into the client bundle at build time; keep the localhost fallback identical
// to lib/api.ts for local dev.
const API_ORIGIN = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const isProd = process.env.NODE_ENV === "production";

// Content-Security-Policy (issue #55):
// - 'unsafe-inline' for scripts is required by the Next.js App Router runtime,
//   which bootstraps hydration through inline <script> tags in the HTML shell
//   (self-hosting + nonces is the follow-up hardening step).
// - 'unsafe-inline' for styles covers Tailwind-injected style tags and the
//   styled-jsx dev runtime.
const csp = [
  "default-src 'self'",
  "script-src 'self' 'unsafe-inline'", // Next.js hydration bootstrap (see note above)
  "style-src 'self' 'unsafe-inline'", // Tailwind / dev style injection
  "img-src 'self' data:",
  "font-src 'self'",
  `connect-src 'self' ${API_ORIGIN}`,
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
].join("; ");

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
