import { defineConfig, devices } from '@playwright/test';

/**
 * Black-box Playwright config for the archistrator SPA — the UI sibling of
 * ../systemtests. It drives the REAL running SPA in a browser and links ZERO
 * webApp source; selectors are published `data-testid`s only.
 *
 * ── Topology ────────────────────────────────────────────────────────────────
 * The SPA is a Vite app that proxies `/api/*` → the archistrator Go server
 * (dev-mode auth injects a dev principal, so no OIDC round-trip is needed).
 * Therefore a run needs TWO processes up behind the one baseURL:
 *
 *   [browser] → [SPA dev server :5173] ──/api──▶ [Go server :8888] → Postgres (+ Temporal/worker for drafting)
 *
 * The Go server is NOT started here (it needs Postgres and is provisioned the
 * same way as ../systemtests — docker-compose + the ARCHISTRATOR_* env). This
 * config only owns the SPA process, mirroring the split in systemtests where
 * infra is provisioned out-of-band and the test process drives over the wire.
 *
 * ── baseURL / webServer ─────────────────────────────────────────────────────
 *   • Default: this config starts the SPA via `npm run dev` in ../webApp and
 *     points baseURL at it (UITESTS_SPA_URL, default http://localhost:5173).
 *   • Set UITESTS_BASE_URL to drive an ALREADY-running SPA (e.g. `vite preview`
 *     or a deployed origin); the managed webServer is then skipped.
 *
 * ── Infra gating ────────────────────────────────────────────────────────────
 * Like systemtests, specs that need a live drafting backend (Postgres+Temporal
 * +worker) are gated: they SKIP unless UITESTS_LIVE_DRAFTING=1. The pure-UI /
 * navigation specs only need the SPA + a Postgres-backed server and self-skip
 * (annotate) when the server is unreachable. See README.md.
 */

const SPA_URL = process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const BASE_URL = process.env.UITESTS_BASE_URL ?? SPA_URL;

// When UITESTS_BASE_URL is set we drive an already-running SPA — do not manage one.
const manageSpa = process.env.UITESTS_BASE_URL === undefined;

// The managed webServer must bind the SAME port SPA_URL names — derive it
// rather than hardcoding 5173, so UITESTS_SPA_URL actually controls where the
// managed dev server listens (previously these could silently disagree: SPA_URL
// repointed but the spawned `vite --port 5173` did not follow).
const SPA_PORT = new URL(SPA_URL).port || '5173';

// ARCHISTRATOR_API_PROXY_TARGET (passed through to the spawned Vite process,
// see ../webApp/vite.config.ts) lets a run point the SPA's /api proxy at a
// throwaway backend instance instead of whatever dev-mode server happens to
// already be running on the default :8888 — needed to test against a
// specific, known-good server without disturbing an unrelated one.

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: [['html', { open: 'never' }], ['list']],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: BASE_URL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  ...(manageSpa
    ? {
        webServer: {
          // Boot the real SPA. It proxies /api → the Go server on :8888 (or
          // ARCHISTRATOR_API_PROXY_TARGET, when set), which MUST already be up
          // in dev mode (see README "Running"). We do NOT start the Go server
          // here — it needs provisioned Postgres.
          command: `npm run dev -- --port ${SPA_PORT} --strictPort`,
          cwd: '../webApp',
          url: SPA_URL,
          timeout: 120_000,
          reuseExistingServer: !process.env.CI,
          stdout: 'pipe',
          stderr: 'pipe',
          env: process.env.ARCHISTRATOR_API_PROXY_TARGET
            ? { ARCHISTRATOR_API_PROXY_TARGET: process.env.ARCHISTRATOR_API_PROXY_TARGET }
            : {},
        },
      }
    : {}),
});
