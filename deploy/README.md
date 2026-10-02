# Container

Single image containing the Go gateway and the built console. The console is
served under `/ui`; everything else is inbound traffic.

```bash
docker run -d --name affinity-gateway \
  -p 8236:8236 \
  -v affinity-data:/data \
  -e AFFINITY_TOKEN=your-console-token \
  ghcr.io/powercess/affinity-gateway:latest
```

- Console: `http://localhost:8236/ui/`
- Control API: `http://localhost:8236/api/v1/*` (Bearer token)
- Inbound: `http://localhost:8236/v1/*`
- Egress: `http://localhost:8236/egress/{route-id}/*`

State (`config.json`, `secret`, `token`, `requests.json`, `metrics.json`,
`affinity.json`) lives in the `/data` volume. Set `AFFINITY_TOKEN` to pin the
control-plane token, or read the generated one from `/data/token`.
