import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

export default defineConfig({
  server: {
    proxy: {
      "/api": {
        target:
          process.env.AFFINITY_CONSOLE_UPSTREAM || "http://127.0.0.1:18242",
        changeOrigin: false,
      },
    },
  },
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  build: {
    rollupOptions: {
      output: { manualChunks: { charts: ["recharts"], ui: ["radix-ui"] } },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test-setup.ts"],
    css: false,
  },
});
