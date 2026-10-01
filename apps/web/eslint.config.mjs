import { dirname } from "path";
import { fileURLToPath } from "url";
import { FlatCompat } from "@eslint/eslintrc";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const compat = new FlatCompat({
  baseDirectory: __dirname,
});

const eslintConfig = [
  ...compat.extends("next/core-web-vitals", "next/typescript"),
  {
    rules: {
      // Re-enabled (#57): explicit `any` defeats the point of TS strict mode.
      // Contract shapes live in src/types/api.ts instead.
      "@typescript-eslint/no-explicit-any": "error",
    },
  },
];

export default eslintConfig;
