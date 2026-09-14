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
 * +worker) are gated: they SKIP unless UITESTS_LIVE_DRAFTING=1, the one explicit
 * opt-in. The pure-UI / navigation specs only need the SPA + a Postgres-backed
 * server, and FAIL when it does not answer (requireServer): a skip reads as green.
 * See README.md.
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

// ── The PREVIEW target ───────────────────────────────────────────────────────
// The `preview` project (tests/preview/) drives the SAME app built in PREVIEW
// MODE (webApp/vite.preview.config.ts): the real components over the fixture
// transport, fed this package's test-local fixtures (./preview-fixtures), served
// statically. It needs no Go server and no network, so it runs deterministically
// wherever it runs. Default: this config builds the preview and serves it on
// UITESTS_PREVIEW_URL's port (5832). Set UITESTS_PREVIEW_URL to drive an
// already-running preview server instead; the managed one is then skipped.
const PREVIEW_URL = process.env.UITESTS_PREVIEW_URL ?? 'http://localhost:5832';
const managePreview = process.env.UITESTS_PREVIEW_URL === undefined;
const PREVIEW_PORT = new URL(PREVIEW_URL).port || '5832';

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
    // THE SEED STEP (fix-G review ruling): the one place in this suite that may
    // create a real project. Every spec depends on it, so it runs first and by
    // name, never as some spec's side effect. It runs only with
    // UITESTS_LIVE_DRAFTING on (only the live-drafting specs need a real project;
    // every other spec fakes its project in the browser, under the dispatch
    // guard), and it SKIPS otherwise, CI's default run included. A skipped seed
    // holds no spec back. See tests/seed/shared-project.setup.ts.
    {
      name: 'seed-shared-project',
      testMatch: /seed\/shared-project\.setup\.ts$/,
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'chromium',
      testIgnore: /preview\//,
      use: { ...devices['Desktop Chrome'] },
      dependencies: ['seed-shared-project'],
    },
    {
      // The preview build (see "The PREVIEW target" above). Its own baseURL; no
      // server, so no seed dependency.
      name: 'preview',
      testMatch: /preview\/.*\.spec\.ts$/,
      use: { ...devices['Desktop Chrome'], baseURL: PREVIEW_URL },
    },
  ],
  webServer: [
    ...(manageSpa
      ? [
          {
            // Boot the real SPA. It proxies /api → the Go server on :8888 (or
            // ARCHISTRATOR_API_PROXY_TARGET, when set), which MUST already be up
            // in dev mode (see README "Running"). We do NOT start the Go server
            // here — it needs provisioned Postgres.
            command: `npm run dev -- --port ${SPA_PORT} --strictPort`,
            cwd: '../webApp',
            url: SPA_URL,
            timeout: 120_000,
            reuseExistingServer: !process.env.CI,
            stdout: 'pipe' as const,
            stderr: 'pipe' as const,
            env: process.env.ARCHISTRATOR_API_PROXY_TARGET
              ? { ARCHISTRATOR_API_PROXY_TARGET: process.env.ARCHISTRATOR_API_PROXY_TARGET }
              : {},
          },
        ]
      : []),
    ...(managePreview
      ? [
          {
            // Build the preview over this package's fixtures, then serve it.
            // `build:preview` validates every fixture against the OAS-generated
            // schema and fails on the first drift.
            command: `npm run build:preview && npx vite preview -c vite.preview.config.ts --port ${PREVIEW_PORT} --strictPort`,
            cwd: '../webApp',
            url: PREVIEW_URL,
            timeout: 180_000,
            reuseExistingServer: !process.env.CI,
            stdout: 'pipe' as const,
            stderr: 'pipe' as const,
            env: { ARCHISTRATOR_PREVIEW_FIXTURES: '../uitests/preview-fixtures' },
          },
        ]
      : []),
  ],
});
