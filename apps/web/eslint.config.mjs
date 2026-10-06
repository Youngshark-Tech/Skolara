import coreWebVitals from "eslint-config-next/core-web-vitals";
import nextTypescript from "eslint-config-next/typescript";

// eslint-config-next 16 ships native flat config (no FlatCompat needed —
// the @eslint/eslintrc shim was removed with the Next 16 upgrade, #102).
const eslintConfig = [
  ...coreWebVitals,
  ...nextTypescript,
  {
    rules: {
      // Re-enabled (#57): explicit `any` defeats the point of TS strict mode.
      // Contract shapes live in src/types/api.ts instead.
      "@typescript-eslint/no-explicit-any": "error",
      // react-hooks v6 (via eslint-config-next 16) ships this new rule. The 7
      // remaining sites are the app's fetch-in-effect data layer (async
      // setState after `await` inside useCallback loaders — #81/#85 design);
      // migrating them is deliberate architecture work tracked in the
      // follow-up issue, not smuggled into the Next 16 dependency bump.
      // The two genuinely-synchronous sites were already fixed to
      // render-time adjustment in this PR.
      "react-hooks/set-state-in-effect": "warn",
    },
  },
];

export default eslintConfig;
