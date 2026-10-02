import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// In dev, the Go server runs on :8080 and Vite proxies API calls to it.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: false },
      "/ingest": "http://localhost:8080",
    },
  },
  build: {
    chunkSizeWarningLimit: 1200, // MapLibre is one big lazy chunk by design
  },
});
