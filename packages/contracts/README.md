# @skolara/contracts

Single source of truth for the Skolara API contract (§54 contract-first).

- `openapi.yaml` — the OpenAPI 3.1 specification (all `/api/v1` endpoints)
- `generated/schema.d.ts` — TypeScript types generated from the spec

`generated/` is **committed** (issue #62): a fresh clone contains the types,
and the CI `contracts` job regenerates them and fails the build on any drift.

## Regenerate / drift check

```bash
cd packages/contracts
npm ci
npm run generate   # regenerate types from the spec
npm run check      # regenerate + assert generated/ is committed and current
```

`check` fails when `git status --porcelain -- generated/` is non-empty — that
catches both modified tracked files and brand-new untracked files (the old
`git diff --exit-code` gate was a no-op for untracked output).

If `npm run check` fails, run `npm run generate` and commit `generated/`
(or fix the spec if generated output is correct — the spec is authoritative
for API shape; Go handlers are verified against it separately by the Go
`contract` drift test).

## Rules

- API changes land here FIRST (or in the same PR as handler changes)
- `generated/` is committed and must stay current with `openapi.yaml`; the
  `contracts` CI job fails the PR otherwise
- `npm test` re-asserts the no-drift invariant without regenerating
- Response shapes follow the envelopes documented in `docs/api/README.md`
