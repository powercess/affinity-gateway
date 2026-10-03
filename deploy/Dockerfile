# syntax=docker/dockerfile:1

# --- console ---------------------------------------------------------------
FROM oven/bun:1.4.2-alpine AS console
WORKDIR /src
COPY package.json bun.lock ./
COPY apps/console/package.json apps/console/package.json
RUN bun install --frozen-lockfile
COPY apps/console apps/console
RUN cd apps/console && bun run build

# --- gateway ---------------------------------------------------------------
FROM golang:1.23-alpine AS gateway
WORKDIR /src/apps/gateway
RUN apk add --no-cache git
COPY apps/gateway/go.mod apps/gateway/go.sum ./
RUN go mod download
COPY apps/gateway ./
ARG VERSION=dev
ARG REVISION=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.revision=${REVISION}" \
    -o /out/affinity-gateway ./cmd/affinity-gateway

# --- runtime ---------------------------------------------------------------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 affinity \
    && mkdir -p /data \
    && chown affinity:affinity /data
COPY --from=gateway /out/affinity-gateway /app/affinity-gateway
COPY --from=console /src/apps/console/dist /app/console
ENV AFFINITY_CONFIG_FILE=/data/config.json \
    AFFINITY_CONSOLE_DIR=/app/console
WORKDIR /app
USER affinity
VOLUME ["/data"]
EXPOSE 8236 8237 8238
ENTRYPOINT ["/app/affinity-gateway"]
