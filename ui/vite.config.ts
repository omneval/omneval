import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { resolve } from "path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": resolve(__dirname, "src"),
    },
  },
  server: {
    proxy: {
      "/api": "http://localhost:8002",
      "/login": "http://localhost:8002",
      "/logout": "http://localhost:8002",
      "/internal": "http://localhost:8002",
    },
  },
  build: {
    outDir: resolve(__dirname, "../services/query/internal/server/ui/dist"),
    emptyOutDir: true,
  },
});
