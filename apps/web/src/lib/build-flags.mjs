/**
 * Build-time flag defaults shared by next.config.mjs and the test suite
 * (issue #153).
 *
 * Plain .mjs so the Node-side next.config.mjs loads it in every Node version
 * (no TypeScript in the config path), while the unit tests pin the semantics
 * from src/lib/build-flags.test.ts — the same split as lib/api-url.mjs.
 */

/**
 * TEMPORARY zero-config demo default (issue #153).
 *
 * NEXT_PUBLIC_* flags are inlined into the client bundle at BUILD time. A
 * deployment built with NO configuration used to ship with demo mode OFF,
 * leaving a login form pointed at an API service with no database — every
 * fresh Vercel deployment failed at login (the operator had to know about
 * build-time env vars before seeing a working product). next.config.mjs
 * therefore injects the demo default at the CONFIGURATION layer, and only
 * when the operator has not expressed any preference:
 *
 * - unset or empty -> "true"  (a zero-config deployment is a demo)
 * - any other value -> passed through verbatim ("false" is the go-live
 *   switch; anything non-"true" is treated as OFF by lib/auth-bypass.ts)
 *
 * The security module keeps its strict semantics — this helper only decides
 * what an ABSENT flag means. Going live stays an explicit, documented act:
 * set NEXT_PUBLIC_DEMO_MODE=false (plus DATABASE_URL on the API) and
 * redeploy. See docs/operations/DEPLOY_VERCEL.md and the RUNBOOK go-live
 * checklist.
 *
 * SECURITY: a deployment holding real student, guardian, or financial data
 * MUST set NEXT_PUBLIC_DEMO_MODE=false — tracked as a release blocker in the
 * RUNBOOK, next to the SKOLARA_DEMO_SEED warning.
 *
 * @param {string | undefined} raw - process.env.NEXT_PUBLIC_DEMO_MODE at build time
 * @returns {string}
 */
export function resolveDemoModeFlag(raw) {
  if (raw === undefined || raw === "") return "true";
  return raw;
}
