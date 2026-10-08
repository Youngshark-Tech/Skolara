/** @type {import('next').NextConfig} */

// CSP + client URL derivation share ONE implementation (issue #135): the CSP
// connect-src directive tracks exactly where the client calls the API. The
// resolution rules live in src/lib/api-url.mjs (resolveApiUrl, #127) —
// documented there.
import { buildCsp } from "./src/lib/api-url.mjs";

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
