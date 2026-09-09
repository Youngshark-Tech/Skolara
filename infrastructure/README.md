# Skolara — Local Development

## Quickstart (Docker Compose)

```bash
cd infrastructure
cp .env.example .env                    # adjust if needed
docker compose up --build
```

| Service   | URL                 | Notes                          |
|-----------|---------------------|--------------------------------|
| Web       | http://localhost:3000 | Next.js app                  |
| API       | http://localhost:8080 | Go API, /healthz /readyz /metrics |
| Postgres  | localhost:5432      | user/pass/db: skolara          |
| Redis     | localhost:6379      | cache/ephemeral state          |

## Service-by-service development

See the repository [CONTRIBUTING](../CONTRIBUTING.md) for running the API and web app natively with `make` targets.

## Notes

- Containers run as **non-root**; no secrets are baked into images — all configuration is environment-driven.
- The compose stack is for development and self-hosted evaluation. Production deployment hardening (TLS, secrets manager, replicas) is covered in docs/operations/RUNBOOK.md.
