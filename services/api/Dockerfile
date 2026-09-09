# Skolara API — multi-stage build
# Build context: repository root
#   docker build -f services/api/Dockerfile services/api

FROM golang:1.23-alpine AS build
WORKDIR /src

# Layer-cached dependency resolution
COPY services/api/go.mod services/api/go.sum* ./
RUN go mod download || true

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
