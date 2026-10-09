/**
 * Deployment mode flags (issues #141, #142).
 *
 * Two orthogonal, temporary switches:
 *
 * 1. NEXT_PUBLIC_AUTH_BYPASS=true — open-access mode: the login surface is
 *    disabled and visitors are signed in automatically as the seeded demo
 *    user against the REAL API + database.
 * 2. NEXT_PUBLIC_DEMO_MODE=true — full demo data mode: every API call is
 *    answered by the in-memory mock transport (lib/mock). No database, no
 *    API service, no network. Implies (1).
 *
 * SECURITY: these flags exist for demo/staging deployments ONLY. They must
 * NEVER be enabled on an environment holding real student, guardian, or
 * financial data — open-access hands every visitor a working admin session.
 * The warning mirrors SKOLARA_DEMO_SEED (services/api/internal/demo/demo.go).
 *
 * NEXT_PUBLIC_* values are inlined at BUILD time: toggling the flags on
 * Vercel requires a redeploy, not just a save. The credential overrides
 * exist for deployments that seeded accounts with SKOLARA_DEMO_PASSWORD.
 *
 * Zero-config default (issue #153): next.config.mjs injects
 * NEXT_PUBLIC_DEMO_MODE="true" at the CONFIGURATION layer when the variable
 * is unset/empty at build time — a zero-config deployment is a demo. The
 * decision lives in src/lib/build-flags.mjs (unit-pinned there); THIS module
 * keeps strict parsing ("true" enables, everything else is off, demo mode
 * implies open access). Going live = set NEXT_PUBLIC_DEMO_MODE=false and
 * redeploy (RUNBOOK go-live checklist).
 */

export const AUTH_BYPASS_FLAG = "NEXT_PUBLIC_AUTH_BYPASS";
export const DEMO_MODE_FLAG = "NEXT_PUBLIC_DEMO_MODE";

/**
 * BUILD-TIME flag values. Next.js inlines ONLY the exact dotted expression
 * `process.env.NEXT_PUBLIC_*` into client bundles — a dynamic lookup on the
 * process.env object (e.g. `env.NEXT_PUBLIC_X`) reads an empty shim in the
 * browser and always yields undefined. The browser E2E pass for #142 caught
 * exactly that: the flags never activated in a real browser. These constants
 * capture the inlined values once; the helpers below fall back to their `env`
 * parameter only when the build constant is absent (unit tests stub env at
 * runtime, where the build constant is undefined).
 */
const BUILD_DEMO_MODE: string | undefined = process.env.NEXT_PUBLIC_DEMO_MODE;
const BUILD_AUTH_BYPASS: string | undefined = process.env.NEXT_PUBLIC_AUTH_BYPASS;
const BUILD_BYPASS_EMAIL: string | undefined = process.env.NEXT_PUBLIC_BYPASS_EMAIL;
const BUILD_BYPASS_PASSWORD: string | undefined = process.env.NEXT_PUBLIC_BYPASS_PASSWORD;

/** Defaults mirror the demo seed's documented accounts (docs/operations/DEMO.md). */
export const DEFAULT_BYPASS_EMAIL = "admin@skolara.dev";
export const DEFAULT_BYPASS_PASSWORD = "SkolaraDemo!2026";

/** Whether the in-memory demo dataset answers every API call (issue #142). */
export function mockDataEnabled(
  env: Record<string, string | undefined> = process.env,
): boolean {
  if (BUILD_DEMO_MODE !== undefined) return BUILD_DEMO_MODE === "true";
  return env.NEXT_PUBLIC_DEMO_MODE === "true";
}

/**
 * Whether open-access mode is active: either the explicit auth-bypass flag
 * (real API + real database, no login surface — issue #141) or full demo
 * data mode, which implies it (no database at all — issue #142).
 */
export function authBypassEnabled(
  env: Record<string, string | undefined> = process.env,
): boolean {
  if (BUILD_AUTH_BYPASS !== undefined) {
    return BUILD_AUTH_BYPASS === "true" || BUILD_DEMO_MODE === "true";
  }
  return env.NEXT_PUBLIC_AUTH_BYPASS === "true" || mockDataEnabled(env);
}

/**
 * Credentials used for the automatic demo sign-in. Only the documented demo
 * pair by default; both values must match what the API actually seeded
 * (SKOLARA_DEMO_PASSWORD overrides change the seeded password — the matching
 * NEXT_PUBLIC_* override must then be set on the web app).
 */
export function bypassCredentials(
  env: Record<string, string | undefined> = process.env,
): { email: string; password: string } {
  if (BUILD_BYPASS_EMAIL !== undefined || BUILD_BYPASS_PASSWORD !== undefined) {
    return {
      email: BUILD_BYPASS_EMAIL || DEFAULT_BYPASS_EMAIL,
      password: BUILD_BYPASS_PASSWORD || DEFAULT_BYPASS_PASSWORD,
    };
  }
  return {
    email: env.NEXT_PUBLIC_BYPASS_EMAIL || DEFAULT_BYPASS_EMAIL,
    password: env.NEXT_PUBLIC_BYPASS_PASSWORD || DEFAULT_BYPASS_PASSWORD,
  };
}
