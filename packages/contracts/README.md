# @skolara/contracts

Single source of truth for the Skolara API contract (§54 contract-first).

- `openapi.yaml` — the OpenAPI 3.1 specification (all `/api/v1` endpoints)
- `generated/schema.d.ts` — TypeScript types generated from the spec (checked in)

## Regenerate / drift check

```bash
cd packages/contracts
npm install
npm run generate   # regenerate types from the spec
npm run check      # regenerate + git diff --exit-code (CI drift gate)
```

If `npm run check` fails, either regenerate and commit the updated types, or
fix the Go handlers — the spec is authoritative.

## Rules

- API changes land here FIRST (or in the same PR as handler changes)
- `generated/` is committed; CI verifies it matches the spec
- Response shapes follow the envelopes documented in `docs/api/README.md`
