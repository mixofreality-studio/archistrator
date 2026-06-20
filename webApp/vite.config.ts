import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// In dev, proxy /api to the locally-running archistrator Go server (dev-mode
// auth: the server injects a dev principal when no x-aiarch-claim-* headers are
// present, so the SPA is locally runnable without a full OIDC round-trip).
// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8888',
        changeOrigin: true,
      },
    },
  },
});
