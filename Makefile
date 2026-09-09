# Skolara — repository-level orchestration
.PHONY: help api-build api-test api-test-integration api-run web-install web-dev web-build web-lint web-typecheck web-test migrate-up migrate-down compose-up compose-down

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}'

api-build: ; cd services/api && go build ./...
api-test: ; cd services/api && go test -race ./...
api-test-integration: ; cd services/api && TEST_DATABASE_URL="$${TEST_DATABASE_URL}" go test -race -tags=integration ./...
api-run: ; cd services/api && go run ./cmd/api

web-install: ; cd apps/web && npm ci
web-dev: ; cd apps/web && npm run dev
web-build: ; cd apps/web && npm run build
web-lint: ; cd apps/web && npm run lint
web-typecheck: ; cd apps/web && npm run typecheck
web-test: ; cd apps/web && npm test

migrate-up: ; cd services/api && go run ./cmd/migrate up
migrate-down: ; cd services/api && go run ./cmd/migrate down

compose-up: ; docker compose -f infrastructure/docker-compose.yml up --build
compose-down: ; docker compose -f infrastructure/docker-compose.yml down
