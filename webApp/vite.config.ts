import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// In dev, proxy /api to the locally-running archistrator Go server (dev-mode
// auth: the server injects a dev principal when no x-aiarch-claim-* headers are
// present, so the SPA is locally runnable without a full OIDC round-trip).
// ARCHISTRATOR_API_PROXY_TARGET overrides the default :8888 — e.g. so a test
// harness (uitests) can point a managed dev server at its OWN throwaway
// backend instance instead of whatever dev-mode server happens to be running
// on :8888.
// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: process.env['ARCHISTRATOR_API_PROXY_TARGET'] ?? 'http://localhost:8888',
        changeOrigin: true,
      },
    },
  },
});
