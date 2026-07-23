import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    // Build straight into the Go embed target (internal/transport/web/static).
    outDir: "../static",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      // Dev-mode: `todo serve` owns the API, Vite owns everything else.
      "/api": "http://127.0.0.1:8080",
    },
  },
});
