FROM node:22-alpine AS build
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM caddy:2.11.4-alpine
COPY --from=build /web/dist /srv
COPY deploy/console/UI.Caddyfile /etc/caddy/Caddyfile
