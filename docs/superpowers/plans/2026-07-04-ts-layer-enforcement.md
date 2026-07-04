# TS Layer Enforcement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Ship a reusable ESLint package from `archistrator-platform` that enforces a 6-layer import DAG (routes → components → hooks → api, with contracts + utilities as universal leaves) and migrate the webApp onto it as the reference implementation.

**Architecture:** Pure ESLint via `eslint-plugin-boundaries` (element-type matrix + `no-unknown`), bundling the full strict baseline (typescript-eslint strictTypeChecked, react, jsx-a11y, prettier) so one import gives every archistrator TS app identical rules. Spec: `docs/superpowers/specs/2026-07-04-ts-layer-enforcement-design.md`.

**Tech Stack:** ESLint 9 flat config (ESM), eslint-plugin-boundaries, typescript-eslint 8, React 19 webApp on Vite 7 + TanStack Router.

## Global Constraints

- Package dir: `archistrator-platform/framework-web-eslint-config/`; npm name `@mixofreality-studio/archistrator-platform-eslint-config-web`.
- Element types + matrix are authoritative in the spec Layer Model table — copy exactly.
- Only `hooks` may import `api` (the IO client). `contracts`/`utilities` are universal leaves. No upward imports. Sideways allowed within a layer.
- All boundary rules `error`. `boundaries/no-unknown` + `no-unknown-files` on (everything classifies). Ignore only `src/main.tsx`, `src/vite-env.d.ts`, and generated `src/contracts/schema.ts` content.
- Port the webApp's existing custom rule set verbatim (see spec Packaging → What it bundles).
- Gate every webApp step on `npm run typecheck` then `npm run lint` then `npm run build`.

---

### Task 1: Scaffold the eslint-config package

**Files:**
- Create: `archistrator-platform/framework-web-eslint-config/package.json`
- Create: `archistrator-platform/framework-web-eslint-config/README.md`

- [ ] Create `package.json`: name `@mixofreality-studio/archistrator-platform-eslint-config-web`, version `0.1.0`, `type: module`, `main: index.js`, `files: ["index.js"]`, `publishConfig.access: public`. `dependencies`: all plugins (`@eslint/js`, `typescript-eslint`, `eslint-plugin-react`, `eslint-plugin-react-hooks`, `eslint-plugin-react-refresh`, `eslint-plugin-jsx-a11y`, `eslint-config-prettier`, `eslint-plugin-boundaries`, `globals`) at the webApp's current versions. `peerDependencies`: `eslint >=9`, `typescript >=5.9`. `scripts.test`: runs eslint over fixtures (see Task 3).
- [ ] Add it to `archistrator-platform/go.work`? No — JS package, not a Go module. Leave go.work untouched.
- [ ] `cd archistrator-platform/framework-web-eslint-config && npm install`. Expected: lockfile written, no errors.
- [ ] Commit.

### Task 2: Author the flat-config factory (`index.js`)

**Files:**
- Create: `archistrator-platform/framework-web-eslint-config/index.js`

**Interfaces:**
- Produces: `default export function archWeb({ tsconfigRootDir, ignores = [] })` → flat-config array.

- [ ] Write `index.js`: default-export a factory that returns the flat-config array — `globalIgnores(['dist', ...ignores])`, then the `src/**/*.{ts,tsx}` block extending js.recommended + tseslint strictTypeChecked + stylisticTypeChecked + react flat recommended/jsx-runtime + reactHooks flat + reactRefresh vite + jsxA11y strict + prettier, with `parserOptions.projectService` + `tsconfigRootDir`, the verbatim custom rules, and a boundaries block: `plugins:{boundaries}`, `settings['boundaries/elements']` for the 6 types (mode folder, `src/<type>`), `settings['boundaries/include']:['src/**/*']`, `settings['boundaries/ignore']:['src/main.tsx','src/vite-env.d.ts']`; rules `boundaries/element-types` = the matrix, `boundaries/no-unknown` + `boundaries/no-unknown-files` = error.
- [ ] Verify the exact boundaries flat API empirically in Task 3 (element-types rule shape, settings keys). Adjust until fixtures pass.

### Task 3: Fixtures proving each rule fires

**Files:**
- Create: `archistrator-platform/framework-web-eslint-config/fixtures/valid/**` (clean sample tree, one file per layer, legal imports)
- Create: `archistrator-platform/framework-web-eslint-config/fixtures/invalid/**` (component→api, hook→component, route→api, api→hooks, an unclassified `src/misc/x.ts`)
- Create: `archistrator-platform/framework-web-eslint-config/fixtures/eslint.config.js` (imports the factory)
- Create: `archistrator-platform/framework-web-eslint-config/test.mjs` (runs ESLint API over both trees, asserts valid=0 errors, invalid produces the expected boundary rule IDs)

- [ ] Write the two fixture trees + a minimal tsconfig so `projectService` resolves.
- [ ] Write `test.mjs` using the `ESLint` node API: lint `fixtures/valid` → assert 0 boundary errors; lint `fixtures/invalid` → assert each expected `boundaries/*` violation present.
- [ ] Run `npm test`. Iterate on `index.js` until valid passes clean and invalid reports exactly the intended violations.
- [ ] Commit.

### Task 4: webApp file migration (moves + rewrites, typecheck-gated)

**Files:** `webApp/src/**`, `webApp/scripts/gen-api.mjs`, `webApp/eslint.config.js` ignore for schema path, `webApp/tsconfig*.json` (only if an alias references a moved dir).

- [ ] `git mv src/screens src/routes`; `git mv src/navigation/router.tsx src/routes/router.tsx`; `rmdir src/navigation`.
- [ ] `mkdir src/contracts`; `git mv` the inert files (types, enums, models, operationsTypes, adapters, projectAdapters, constructionAdapters, operationsAdapters, serviceContracts, contractComponentId, constructionRows, wire, schema) into `src/contracts/`.
- [ ] Extract `ApiError`, `WireError`, `toApiError` from `src/api/client.ts` into new `src/contracts/errors.ts`; leave `apiClient` (+ its imports) in `src/api/client.ts`.
- [ ] `mkdir src/utilities`; `git mv` `theme auth constants config.ts data` under `src/utilities/`.
- [ ] Update `scripts/gen-api.mjs` output path → `src/contracts/schema.ts`.
- [ ] Rewrite all import specifiers across `src/**` (scripted sed by old→new path segment: `/api/types`→`/contracts/types`, `/api/adapters`→`/contracts/adapters`, `/api/client` (ApiError)→`/contracts/errors`, `/screens/`→`/routes/`, `/theme/`→`/utilities/theme/`, etc.). Fix relative-depth changes.
- [ ] `npm run typecheck` → must be clean. Fix every unresolved path it reports.
- [ ] Commit.

### Task 5: Wire the shared config + verify

**Files:** `webApp/eslint.config.js`, `webApp/package.json`.

- [ ] Add `@mixofreality-studio/archistrator-platform-eslint-config-web` as a devDependency (file: link to the platform package for local dev, matching how framework-web is consumed). Remove now-redundant plugin devDeps that the shared package supplies transitively (keep `eslint`, `prettier`, `typescript`, `openapi-typescript`, `@types/*`, vite).
- [ ] Replace `eslint.config.js` with the factory import: `import archWeb from '...'; export default archWeb({ tsconfigRootDir: import.meta.dirname, ignores: ['src/contracts/schema.ts'] })`.
- [ ] `npm install`.
- [ ] `npm run lint` → clean (0 errors). Fix any real boundary violation the way the spec prescribes (never by weakening a rule).
- [ ] `npm run typecheck` and `npm run build` → clean.
- [ ] Run the app locally on real state (per `archistrator-run-app-locally` memory: `GOWORK=off`, `CONSTRUCTION_DRYRUN`, server + SPA) and smoke-test the migrated screens.
- [ ] Commit. Leave the branch for founder review (do not self-merge — UI-review-loop).

## Self-Review notes

- Spec coverage: Tasks 1–3 = reusable package + fixtures; Tasks 4–5 = webApp migration + verify. All spec sections covered.
- Risk: boundaries flat-config API details are verified empirically in Task 3 before the webApp depends on them.
- The only behavior-adjacent change is extracting 3 error exports (Task 4) — pure move, typecheck-gated.
