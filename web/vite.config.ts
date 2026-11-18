import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The build lands inside the Go module and is embedded in avora-server.
// In dev, API calls go to the server given by AVORA_DEV_API.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/webui/static",
    emptyOutDir: true,
    assetsInlineLimit: 0,
  },
  server: {
    proxy: {
      "/v1": { target: process.env.AVORA_DEV_API ?? "http://127.0.0.1:8080", changeOrigin: true },
      "/healthz": { target: process.env.AVORA_DEV_API ?? "http://127.0.0.1:8080", changeOrigin: true },
    },
  },
});
