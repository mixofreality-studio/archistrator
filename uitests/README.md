# archistrator scenario tests (clients)

The Playwright half of **deterministic component testing**
(`docs/superpowers/specs/2026-10-02-deterministic-component-testing-design.md`).
Nothing in this package is hand-written: every spec is **generated** by
`testgen` (framework-go-app-generator) from the committed per-component test
plan, and the one file an agent owns is a hooks module the generator emits once.

The hand-written end-to-end harness that used to live here (`tests/**`, the
preview fixtures, the replay cassettes, the dogfood seed script) was removed
with the system test plan (spec §7). The Go-server sibling (`../systemtests`)
went with it: a component is tested through its own scenarios, not through a
system-wide black-box suite.

## Layout

```
uitests/
  generated/<component>/
    <scenario>.spec.gen.ts     one spec per bound scenario (regenerated)
    playwright.config.gen.ts   video + trace always on; results.json reporter (regenerated)
    reporter.gen.ts            maps Playwright results to the TestRun shape (regenerated)
    hooks.ts                   emitted ONCE; the construction agent fills before/step
  eslint.config.js             selector discipline for hooks.ts (no .locator())
  tsconfig.json                type-checks generated/
```

`<component>` is the client component's key in the committed System
(`webClient`, `webClientPlan`, ...). The scenarios and their bindings come from
`project.json` `.phaseArtifacts.testPlan.<component>` × the deterministic
scenario projection (`listComponentScenarios`); `make gen-tests` in `../server`
regenerates, and `make gen-tests-check` is the drift gate.

## Running

```bash
npm ci
npm run install:browsers

# Every generated component config, in turn:
npm run test:scenarios

# One component:
npx playwright test -c generated/webClient/playwright.config.gen.ts
```

A generated config has no `webServer` block: `hooks.before` owns whatever the
scenario needs up, and the SPA it drives must already be reachable
(`PLAYWRIGHT_TEST_BASE_URL`, a Vite dev server whose `/api` proxy,
`ARCHISTRATOR_API_PROXY_TARGET`, points at `UITESTS_API_PORT`). The webClient
hooks bring up a stack of their own for every scenario: a fresh in-memory
Temporal dev server (`temporal` on PATH), a fresh LOCAL-profile project repo,
and the Go server on `UITESTS_API_PORT` with real construction through the
local executor, whose `claude` is the hooks' scripted agent. Postgres is the
harness's (`UITESTS_POSTGRES_URL`). `UITESTS_SERVER_BIN` /
`UITESTS_STATE_MCP_BIN` name prebuilt binaries (built from `../server` when
unset), and `UITESTS_WORK_DIR` is where each scenario's repo and logs go. In CI
(`.github/workflows/uitests.yml`) Postgres is a service, the binaries are
prebuilt, the Vite dev server is started in the background, and
`npm run test:scenarios` runs. The construction venue (`aiarch-construct.yml`,
integration phase) runs the one component's config directly and uploads
`test-results/` as the run's artifact.

## Results

Each config writes under `test-results/<component>/`: Playwright's JSON report,
`results.json` in the `TestRun` shape (`scenario`, `status`, `durationMs`,
`videoRef`, `traceRef`), and the per-test video + trace the webApp's test-run
panel plays back. `test-results/` and `playwright-report/` are gitignored.
