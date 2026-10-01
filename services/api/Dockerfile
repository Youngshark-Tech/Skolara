# Skolara API — multi-stage build
# Build context: repository root (matches infrastructure/docker-compose.yml)
#   docker build -f services/api/Dockerfile .

# golang:1.25.13-alpine matches the toolchain directive in go.mod (issue #101):
# pinning the PATCH keeps image builds reproducible.
FROM golang:1.25.13-alpine AS build
WORKDIR /src

# Layer-cached dependency resolution
COPY services/api/go.mod services/api/go.sum* ./
# No "|| true": a failed dependency download must fail the build,
# not silently produce a layer that breaks at compile time.
RUN go mod download

COPY services/api/ .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=build /out/api /app/api
# migrations are embedded via go:embed by the migrations package
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD ["/app/api", "-healthcheck"]
ENTRYPOINT ["/app/api"]
