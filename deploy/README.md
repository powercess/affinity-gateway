# Container

Single image containing the Go gateway and the built console. It runs three
listeners:

| Port | Purpose | Content |
| --- | --- | --- |
| 8236 | inbound | public AI entry point; every path passed through to the inbound target |
| 8237 | egress | downstream relay entry point; only `/egress/{id}/*` |
| 8238 | console | web UI at `/ui` + control API at `/api/v1` (Bearer token) |

```bash
docker run -d --name affinity-gateway \
  -p 8236:8236 -p 8237:8237 -p 8238:8238 \
  -v affinity-data:/data \
  -e AFFINITY_TOKEN=your-console-token \
  ghcr.io/powercess/affinity-gateway:latest
```

- Console: `http://localhost:8238/ui/`
- Control API: `http://localhost:8238/api/v1/*` (Bearer token)
- Inbound: `http://localhost:8236/<any-path>`
- Egress: `http://localhost:8237/egress/{route-id}/*`

State (`config.json`, `secret`, `token`, `requests.json`, `metrics.json`,
`affinity.json`) lives in the `/data` volume. Set `AFFINITY_TOKEN` to pin the
control-plane token, or read the generated one from `/data/token`.

Addresses can be overridden with `AFFINITY_LISTEN` (inbound),
`AFFINITY_EGRESS_LISTEN`, and `AFFINITY_CONSOLE_LISTEN`.

Configure global outbound proxy settings in the console's **Settings** page and
per-provider overrides in **Egress routes**. These settings persist in
`/data/config.json` and apply without recreating the container.

The default global policy is `environment`, which preserves deployments using
`HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY`. Changing those environment variables
still requires recreating the container. In environment mode, include internal
targets such as `new-api` in `NO_PROXY` so inbound forwarding stays on the Docker
network. A custom global outbound proxy applies only to egress routes; mark a
route `direct` when it must bypass that proxy.
