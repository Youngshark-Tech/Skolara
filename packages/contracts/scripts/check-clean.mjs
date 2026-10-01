// Drift gate for @skolara/contracts (issue #62).
// After `npm run generate`, the generated/ directory must be byte-identical
// to what openapi-typescript produces from openapi.yaml. `git status
// --porcelain` — unlike `git diff --exit-code` — fails on BOTH modified
// tracked files and brand-new untracked files, so a fresh spec edit can
// never slip through unregenerated.
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const pkgRoot = fileURLToPath(new URL("..", import.meta.url));

const dirty = execFileSync("git", ["status", "--porcelain", "--", "generated/"], {
  cwd: pkgRoot,
  encoding: "utf8",
}).trim();

if (dirty) {
  console.error(
    "contracts: DRIFT DETECTED — generated/ does not match openapi.yaml:\n" +
      dirty
        .split("\n")
        .map((l) => "  " + l)
        .join("\n"),
  );
  console.error(
    "Fix: cd packages/contracts && npm run generate, then commit generated/.",
  );
  process.exit(1);
}

console.log("contracts: generated/ matches openapi.yaml (no drift)");
