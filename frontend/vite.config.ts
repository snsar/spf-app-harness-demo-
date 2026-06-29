/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Vite + React config for the GPSR admin. Vitest runs in a jsdom environment
// with a global setup file that registers @testing-library/jest-dom matchers.

// Under `shopify app dev` the CLI provides FRONTEND_PORT (or PORT) to the frontend
// process and BACKEND_PORT to reach the Go backend. Plain `npm run dev` gets neither
// so we fall back to sensible defaults.
const backendPort = process.env.BACKEND_PORT ?? process.env.VITE_BACKEND_PORT ?? "8000";
const frontendPort = Number(process.env.FRONTEND_PORT ?? process.env.PORT ?? 5173);

// Proxy target for the Go backend — used in dev only; nginx handles prod routing.
const backendTarget = `http://localhost:${backendPort}`;

export default defineConfig({
  plugins: [react()],
  server: {
    // Allow the nginx reverse-proxy host (gpsr.quotesnap.local) and the Shopify CLI
    // tunnel host to reach the Vite dev server. Vite 5 blocks non-localhost Host
    // headers by default; nginx proxies with Host=gpsr.quotesnap.local so it must be
    // allow-listed. The CLI tunnel uses its own subdomain — allowedHosts: true lets any
    // host through in dev (the CLI tunnel hostname is dynamic and unknown at config time).
    allowedHosts: true,
    // Use the port the CLI tells us to listen on (FRONTEND_PORT / PORT); fall back to
    // 5173 for plain `npm run dev` so existing local dev still works.
    port: frontendPort,
    // Proxy backend paths so the embedded iframe talks to one origin (Vite) and Vite
    // forwards API / auth / webhook calls to the Go backend. This is the Shopify
    // two-process dev model: / → Vite (UI), /api+/auth+/webhooks+/healthz → Go backend.
    // These proxy rules are dev-only (Vite dev server); they do NOT affect `vite build`
    // or `vitest`, and they do NOT touch the prod nginx config.
    proxy: {
      "/api": {
        target: backendTarget,
        changeOrigin: true,
      },
      "/auth": {
        target: backendTarget,
        changeOrigin: true,
      },
      "/webhooks": {
        target: backendTarget,
        changeOrigin: true,
      },
      "/healthz": {
        target: backendTarget,
        changeOrigin: true,
      },
    },
  },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    css: false,
  },
});
