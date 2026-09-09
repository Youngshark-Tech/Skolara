# Skolara Web — multi-stage build
# Build context: repository root
#   docker build -f apps/web/Dockerfile apps/web

FROM node:20-alpine AS deps
WORKDIR /src
COPY apps/web/package.json apps/web/package-lock.json* ./
RUN npm ci || npm install

FROM node:20-alpine AS build
WORKDIR /src
COPY --from=deps /src/node_modules ./node_modules
COPY apps/web/ .
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
CMD ["node", "server.js"]
