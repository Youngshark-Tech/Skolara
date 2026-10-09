# Deploying Skolara on Vercel (Services)

Skolara deploys to Vercel as a **single project with two services** (Vercel
[Services](https://vercel.com/docs/services), beta): the Next.js web app and the
Go API. One deployment = one domain, with the top-level rewrite table routing
`/api/*` to the Go service. This keeps the API first-party to the web origin —
the refresh cookie stays same-site and no CORS handshake is needed for
first-party traffic.

```text
[Browser]
   │  https://<your-app>.vercel.app
   ▼
[Vercel edge routing]  ──  /api/(.*)  ──►  [api service: Go server]
   │                                            │ PORT env binding
   └─  /(.*)  ──►  [web service: Next.js]      ▼
                                        [PostgreSQL 17] (e.g. Neon/Supabase)
```

> **Zero-configuration deployments are demos by default** (issue #153, temporary):
> if `NEXT_PUBLIC_DEMO_MODE` is **unset** at build time, `next.config.mjs` injects
> demo data mode — the web app runs on the in-memory sample dataset with open
> access, no database and no API service needed. An **explicit** value always
> wins: set `NEXT_PUBLIC_DEMO_MODE=false` (+ `DATABASE_URL` on the api service)
> to go live, and redeploy. See [DEMO.md](DEMO.md) for the full mode matrix.

## 1. Project setup

1. Import the repository into Vercel. Vercel reads the root `vercel.json`
   (already committed) which defines:

   ```json
   {
     "services": {
       "web": { "root": "apps/web", "framework": "nextjs" },
       "api": { "root": "services/api", "runtime": "go", "entrypoint": "cmd/api/main.go" }
     },
     "rewrites": [
       { "source": "/api/(.*)", "destination": { "service": "api" } },
       { "source": "/(.*)",     "destination": { "service": "web" } }
     ]
   }
   ```

2. The rewrite **preserves the original path** — the Go service receives
   `/api/v1/...` unchanged, matching its registered routes exactly. Do not
   strip or remap the prefix.

## 2. Environment variables (Project → Settings → Environment Variables)

Set for **Production** (and Preview if you want preview deploys to work):

| Variable | Value | Why |
|----------|-------|-----|
| `DATABASE_URL` | `postgres://user:pass@host/db?sslmode=require` | Required. Use a managed Postgres (Neon, Supabase, RDS). The API applies embedded migrations at boot. |
| `SKOLARA_ENV` | `production` | Enables strict config validation. |
| `SKOLARA_JWT_SECRET` | ≥ 32 random bytes | Signs access tokens. Generate: `openssl rand -base64 48`. |
| `SKOLARA_WEBHOOK_SECRET` | ≥ 32 random bytes | Payment-webhook HMAC (config validation requires it in production). |
| `SKOLARA_CORS_ORIGINS` | `https://<your-app>.vercel.app` | Same-origin traffic needs no CORS, but production config validation requires a non-localhost origin list; also covers any split-origin callers. |
| `SKOLARA_BOOTSTRAP_ADMIN_EMAIL` / `SKOLARA_BOOTSTRAP_ADMIN_PASSWORD` | your operator credentials | Creates the first platform admin at boot (only when the users table is empty). |
| `SKOLARA_DEMO_SEED` | `true` (optional) | Seeds the idempotent demo dataset (demo school + staff accounts — see [DEMO.md](DEMO.md)). **Never enable on a deployment holding real data.** |
| `SKOLARA_DEMO_PASSWORD` | optional | Overrides the documented demo password for freshly seeded accounts. |
| `NEXT_PUBLIC_API_URL` | leave unset | Unset = same-origin (`""`), which is correct for this topology. Set it only if you split the API onto its own domain. |
| `NEXT_PUBLIC_DEMO_MODE` | unset (default) = **demo ON** · `false` = go-live | **Demo data mode (issues #142, #153): a build with NO value ships the zero-config demo — the web app runs entirely on the in-memory sample dataset, no database, no API service needed.** Set `false` for production (real API + database). Implies `NEXT_PUBLIC_AUTH_BYPASS`. Build-time: change → redeploy. **Never leave unset/`true` on a deployment holding real data** — see [DEMO.md](DEMO.md). |
| `NEXT_PUBLIC_AUTH_BYPASS` | `true` (optional, temporary) | Open-access mode (issue #141): disables the login surface — visitors are signed in automatically as the demo admin. Requires `SKOLARA_DEMO_SEED=true` on the API. **Build-time**: change it → redeploy. **Never on a deployment holding real data** — see [DEMO.md](DEMO.md). |
| `NEXT_PUBLIC_BYPASS_EMAIL` / `NEXT_PUBLIC_BYPASS_PASSWORD` | optional | Open-access credential overrides — must match what the API seeded (`SKOLARA_DEMO_PASSWORD`). Defaults to the documented demo pair. |

Notes:

- **`PORT` is provided by Vercel.** The Go server binds `:$PORT` automatically
  (binding precedence: `SKOLARA_HTTP_ADDR` > `PORT` > `:8080`; see issue #127).
  Do not set `SKOLARA_HTTP_ADDR` on Vercel.
- `SKOLARA_COOKIE_SAMESITE` may stay at its default (`lax`) because API traffic
  is same-origin. `none` is only needed for split-domain hosting (runbook §10).
- Migrations run at API boot; concurrent cold starts are serialized by the
  migration lock in the postgres migration driver.

## 3. Verify the deployment

```bash
# API service alive (the /api/healthz alias routes through the same
# public domain — the bare /healthz does NOT, it falls through to the web):
curl -s https://<your-app>.vercel.app/api/healthz

# Login smoke (expects 401 JSON for wrong creds — that proves the API is
# reachable end-to-end; 404/502 means routing or the service is broken):
curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST https://<your-app>.vercel.app/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"nobody@example.com","password":"wrong"}'
```

Then open `https://<your-app>.vercel.app` — the app should render, and signing
in with your bootstrap-admin credentials should land you in the workspace.

With `SKOLARA_DEMO_SEED=true` you can also sign in with the public demo
accounts (`admin@skolara.dev` / `SkolaraDemo!2026` — full list in
[DEMO.md](DEMO.md)). With `NEXT_PUBLIC_AUTH_BYPASS=true` even that step
disappears: any visitor lands directly in a working demo session. With
`NEXT_PUBLIC_DEMO_MODE=true` the whole API/database layer is replaced by the
in-memory sample dataset — the lightest possible demo deployment.

## 4. Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| Login spins / `Failed to fetch` in console, requests go to `localhost:8080` | `NEXT_PUBLIC_API_URL` was set to a localhost value — remove it. |
| `502`/`FUNCTION_INVOCATION_FAILED` on `/api/*` | API crashed at boot — check the api service runtime logs (most often `DATABASE_URL` unreachable or a config-validation error like a short JWT secret). |
| `404` from `/api/v1/...` with a JSON `not_found` envelope | The request **reached** the Go API but no route matched — the rewrite prefix was likely changed; restore `/api/(.*)`. |
| Config error mentioning `SKOLARA_CORS_ORIGINS` in production | Set it to your deployment origin (see table above). |
| Session lost after ~15 min | Cookies blocked — verify you are testing on the deployment domain itself, not an embedded iframe. |
| Every visitor lands in the demo admin workspace | Demo mode is active — either `NEXT_PUBLIC_DEMO_MODE=true` or (issue #153 default) the variable is **unset** at build time. Set `NEXT_PUBLIC_DEMO_MODE=false` and redeploy to restore normal authentication. |
| Sidebar shows "Demo data — not live", figures reset on reload | Demo data mode (#142/#153) — set `NEXT_PUBLIC_DEMO_MODE=false`, add `DATABASE_URL` to the api service, and redeploy to connect the real database (migrations run automatically at boot). |
| Automatic demo sign-in fails with `invalid_credentials` | The web flag is on but the API has no demo users — set `SKOLARA_DEMO_SEED=true` on the api service (or match `NEXT_PUBLIC_BYPASS_PASSWORD` to `SKOLARA_DEMO_PASSWORD`) and redeploy both. |
