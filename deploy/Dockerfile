FROM caddy:2.11.4-builder AS builder
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
COPY *.go ./
RUN xcaddy build v2.11.4 --with github.com/powercess/caddy-session-affinity=/src --output /usr/bin/caddy
# All listeners use high ports. Remove xcaddy's file capability so cap_drop ALL
# can execute the binary under rootless Docker.
RUN setcap -r /usr/bin/caddy

FROM caddy:2.11.4-alpine
ARG VERSION=dev
ARG REVISION=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.title="Session Affinity Gateway" \
      org.opencontainers.image.source="https://github.com/powercess/caddy-session-affinity" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$REVISION" \
      org.opencontainers.image.created="$BUILD_DATE"
COPY --from=builder /usr/bin/caddy /usr/bin/caddy
COPY deploy/entrypoint.sh /usr/local/bin/affinity-entrypoint
ENTRYPOINT ["/bin/sh", "/usr/local/bin/affinity-entrypoint"]
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
