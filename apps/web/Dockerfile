# Skolara Web — multi-stage build
# Build context: repository root
#   docker build -f apps/web/Dockerfile apps/web
#
# NEXT_PUBLIC_API_URL is REQUIRED: Next.js inlines NEXT_PUBLIC_* variables into
# the client bundle at build time, so an unset value would ship a bundle
# hardwired to the localhost:8080 dev default (issue #56). The build-stage gate
# below fails fast with a clear message instead.

FROM node:20-alpine AS deps
WORKDIR /src
COPY apps/web/package.json apps/web/package-lock.json* ./
RUN npm ci

FROM node:20-alpine AS build
WORKDIR /src
COPY --from=deps /src/node_modules ./node_modules
COPY apps/web/ .
# Build-time public env: must be present BEFORE `npm run build` (inlining).
ARG NEXT_PUBLIC_API_URL
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
RUN test -n "$NEXT_PUBLIC_API_URL" || { \
      echo "ERROR: build arg NEXT_PUBLIC_API_URL is required (e.g. --build-arg NEXT_PUBLIC_API_URL=https://api.example.com)"; \
      exit 1; \
    }
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM node:20-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000
RUN addgroup -S skolara && adduser -S skolara -G skolara
COPY --from=build /src/.next/standalone ./
COPY --from=build /src/.next/static ./.next/static
COPY --from=build /src/public ./public
USER skolara
EXPOSE 3000
# node:20-alpine ships busybox wget; the endpoint is dependency-free (route
# handler, no upstream calls). If a future base image drops wget, swap for:
#   CMD node -e "fetch('http://127.0.0.1:3000/api/health').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -qO- http://127.0.0.1:3000/api/health >/dev/null || exit 1
CMD ["node", "server.js"]
