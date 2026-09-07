FROM node:22-alpine AS build
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

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
