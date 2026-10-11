import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// In dev, the Go server runs on :8080 (or GT_API) and Vite proxies API calls to it.
const api = process.env.GT_API ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/api": { target: api, changeOrigin: false },
      "/ingest": api,
    },
  },
  build: {
    chunkSizeWarningLimit: 1200, // MapLibre is one big lazy chunk by design
  },
});
