FROM oven/bun:1.4.0-alpine AS build
WORKDIR /web
COPY web/package.json web/bun.lock ./
RUN --mount=type=cache,target=/root/.bun/install/cache bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

FROM caddy:2.11.4-alpine
ARG VERSION=dev
ARG REVISION=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.title="Session Affinity Console" \
      org.opencontainers.image.source="https://github.com/powercess/caddy-session-affinity" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$REVISION" \
      org.opencontainers.image.created="$BUILD_DATE"
COPY --from=build /web/dist /srv
COPY deploy/console/UI.Caddyfile /etc/caddy/Caddyfile
