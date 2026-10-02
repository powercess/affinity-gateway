import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import pkg from './package.json';

declare const process: { env: Record<string, string | undefined> };
const gateway = process.env.AFFINITY_GATEWAY ?? 'http://127.0.0.1:8236';

// Extra tunnel hosts must come from AFFINITY_ALLOWED_HOSTS (comma separated, or
// "*"). When unset, Vite's default allow-list (localhost / IPs) applies and no
// hostname is baked into the code.
const allowedHostsEnv = process.env.AFFINITY_ALLOWED_HOSTS?.trim();
const allowedHosts: string[] | true | undefined = !allowedHostsEnv
  ? undefined
  : allowedHostsEnv === '*'
    ? true
    : allowedHostsEnv
        .split(',')
        .map((host) => host.trim())
        .filter(Boolean);

export default defineConfig({
  base: '/ui/',
  plugins: [react(), tailwindcss()],
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version),
  },
  resolve: {
    alias: {
      '@': new URL('./src', import.meta.url).pathname,
    },
  },
  server: {
    allowedHosts,
    proxy: {
      '/api': gateway,
      '/v1': gateway,
      '/egress': gateway,
      '/healthz': gateway,
    },
  },
});
