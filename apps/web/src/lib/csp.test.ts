import { describe, it, expect } from "vitest";
import { buildCsp } from "./api-url.mjs";

/**
 * Pins the CSP derivation against resolveApiUrl semantics (issue #135):
 * connect-src tracks the client's actual API base URL, and 'unsafe-eval'
 * appears in script-src for dev ONLY (production stays eval-free).
 */
describe("buildCsp follows resolveApiUrl (#135)", () => {
  it("production without an explicit URL: same-origin only (no localhost in connect-src)", () => {
    const csp = buildCsp({ apiUrl: undefined, isProduction: true });
    expect(csp).toContain("connect-src 'self'");
    expect(csp).not.toContain("localhost:8080");
    expect(csp).not.toContain("'unsafe-eval'"); // prod stays eval-free
    expect(csp).toContain("script-src 'self' 'unsafe-inline'");
  });

  it("development without an explicit URL: localhost:8080 allowed + dev-only unsafe-eval", () => {
    const csp = buildCsp({ apiUrl: undefined, isProduction: false });
    expect(csp).toContain("connect-src 'self' http://localhost:8080");
    expect(csp).toContain("script-src 'self' 'unsafe-inline' 'unsafe-eval'");
  });

  it("explicit split-origin URL wins in production", () => {
    const csp = buildCsp({ apiUrl: "https://api.example.com", isProduction: true });
    expect(csp).toContain("connect-src 'self' https://api.example.com");
    expect(csp).not.toContain("localhost:8080");
    expect(csp).not.toContain("'unsafe-eval'");
  });

  it("explicit empty string (same-origin opt-in) adds no origin", () => {
    const csp = buildCsp({ apiUrl: "", isProduction: false });
    expect(csp).toContain("connect-src 'self'");
    expect(csp).not.toContain("connect-src 'self' ");
  });
});
