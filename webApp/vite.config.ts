import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// In dev, proxy /api to the locally-running archistrator Go server (dev-mode
// auth: the server injects a dev principal when no x-aiarch-claim-* headers are
// present, so the SPA is locally runnable without a full OIDC round-trip).
// ARCHISTRATOR_API_PROXY_TARGET overrides the default :8888 — e.g. so a test
// harness (uitests) can point a managed dev server at its OWN throwaway
// backend instance instead of whatever dev-mode server happens to be running
// on :8888.
//
// FRAME_DENIAL mirrors production's anti-clickjacking headers (webApp/nginx.conf
// for the SPA, server/cmd/server/framedenial.go for /api) so a dev or preview
// build refuses to be framed exactly as production does.
// https://vite.dev/config/
const FRAME_DENIAL = {
  'X-Frame-Options': 'DENY',
  'Content-Security-Policy': "frame-ancestors 'none'",
};

export default defineConfig({
  plugins: [react()],
  preview: {
    headers: FRAME_DENIAL,
  },
  server: {
    headers: FRAME_DENIAL,
    proxy: {
      '/api': {
        target: process.env['ARCHISTRATOR_API_PROXY_TARGET'] ?? 'http://localhost:8888',
        changeOrigin: true,
      },
    },
  },
});
