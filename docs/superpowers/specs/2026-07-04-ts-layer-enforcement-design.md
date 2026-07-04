# TS Layer Enforcement — Reusable ESLint Architecture Gate

**Date:** 2026-07-04
**Status:** Approved (design)

## Problem

Go code in `archistrator-platform` has strict, mechanically-enforced structure: the
custom `framework-go/arch` analyzer (run as a `go test` in each consuming module)
classifies every package into exactly one layer, forbids upward *and* sideways
imports, and fails loudly with no "misc" bucket and no vacuous pass. TS apps built
with archistrator have **no equivalent** — the webApp's `eslint.config.js` is strict
on code quality (`strictTypeChecked`, custom `error` rules) but has zero
import-boundary enforcement.

We want the same architectural discipline in TS, enforced by a **reusable ESLint
config published from `archistrator-platform`** and consumed by every archistrator TS
app.

## Goal / Non-goals

**Goal:** A single shared ESLint package that (a) bundles the full strict lint
baseline every archistrator TS app should share, and (b) enforces a layered
import-direction gate mirroring the Go `arch` philosophy.

**Non-goals:**
- Not a custom AST analyzer (unlike Go's `arch`); we use pure ESLint so it runs at
  `eslint .` / CI time and integrates with editors.
- Not runtime code — this package ships only lint config, never React components.
- No business logic lives in the UI at all (see Layer Model). This gate *enforces*
  that; it does not relocate logic.

## Layer Model

Views are dumb; **all business logic lives in the server**. The bottom app layer does
one thing: call the generated API client.

> **Amendment (reality-driven, 2026-07-04):** the original design had 5 element types
> with a single `api` leaf. Inspecting the real webApp showed that ~90 of the imports
> from `src/api/` are inert **data types, pure adapters, wire-mapping functions, and
> error classes** — things dumb views legitimately need to render — while the genuine
> IO surface is essentially just the `apiClient` instance. Collapsing those into one
> "only hooks may import" leaf would force pointless re-export churn through hooks (a
> lint workaround, not a real fix). So the model is refined to **6 element types**,
> splitting the inert `contracts` (universal leaf) from the IO `api` (hooks-only leaf).
> This mirrors the Go layering precisely: `api` ≈ ResourceAccess (sole IO leaf),
> `contracts` ≈ shared data contracts, `utilities` ≈ Utility.

Six element types:

| Layer        | Folder            | Role                                                              |
|--------------|-------------------|------------------------------------------------------------------|
| `routes`     | `src/routes/`     | Pages / screens (top). Compose components. Data via hooks.        |
| `components` | `src/components/` | Dumb views. No IO. Consume contracts/utilities + hooks/props.     |
| `hooks`      | `src/hooks/`      | Thin data-access. TanStack Query wrappers. **Sole `api` importer.** |
| `api`        | `src/api/`        | The IO client only (`apiClient`). Hooks-only leaf.               |
| `contracts`  | `src/contracts/`  | Data types, adapters, wire mappers, error classes, generated `schema.ts`. Pure. Universal leaf. |
| `utilities`  | `src/utilities/`  | Cross-cutting: theme, auth context, constants, config, static data. Universal leaf. |

### Import rule matrix (downward-only; sideways *within* a layer allowed)

```
routes      → components, hooks, contracts, utilities        (NOT api)
components  → components (siblings), hooks, contracts, utilities   (NOT api, NOT routes)
hooks       → hooks (siblings), api, contracts, utilities    (the ONLY api importer)
api         → contracts, utilities                           (IO client: needs schema types + config)
utilities   → utilities (siblings), contracts                (infra may use pure types)
contracts   → contracts (siblings)                           (pure leaf, bottom of the DAG)
```

Key constraints, all decided:
- **Only `hooks` may import `api` (the IO client).** Routes and components never touch
  `apiClient` — they get server data via hooks (or props). This is the "dumb views"
  guarantee, and the thing the gate actually enforces.
- **`contracts` is freely importable by everyone.** Types and pure adapters are inert;
  a typed dumb view needs them. `contracts` itself imports nothing but sibling
  contracts (the generated `schema.ts` type file lives here — it is inert types, not IO).
- **No upward imports.** `hooks` cannot import `components` or `routes`;
  `components` cannot import `routes`; `api` cannot import `hooks`/`components`/`routes`.
- **Sideways is allowed *within* a layer** (a component composes sibling components; a
  hook may use a sibling hook). This is the deliberate, necessary divergence from
  Go's no-sideways rule — React components must compose.
- **Everything must classify** — a file outside a known layer fails lint (mirrors Go
  Rule 0, "no misc bucket"). Enforced via `boundaries/no-unknown` +
  `boundaries/no-unknown-files`. The only exemptions are true entry/ambient files:
  `src/main.tsx`, `src/vite-env.d.ts` (ignored). `src/App.tsx` classifies as `routes`
  (app shell).

## Mechanism — `eslint-plugin-boundaries`

Chosen because it maps almost 1:1 onto the Go `arch` model as pure ESLint:

- `settings['boundaries/elements']` tags each folder with its element type
  (mode `folder`, canonical `src/<layer>` patterns) for all six types:
  `routes`, `components`, `hooks`, `api`, `contracts`, `utilities`.
- `boundaries/element-types` encodes the direction matrix above (default `disallow`,
  explicit `allow` per layer) — this is the downward-only rule, declarative instead of
  path regexes.
- `boundaries/no-unknown` + `boundaries/no-unknown-files` reproduce the
  "everything classifies, no bucket" invariant — unclassified files error out.
- All boundary rules set to `error`; fails loudly, no vacuous pass.

## Packaging

A **new standalone package** in `archistrator-platform`:

- Directory: `archistrator-platform/framework-web-eslint-config/`
- npm name: `@mixofreality-studio/archistrator-platform-eslint-config-web`
- Kept separate from `framework-web` (which ships runtime MUI components) so eslint
  plugins never enter the runtime lib's dependency graph.

### What it bundles (Option A — full baseline)

Every archistrator TS app gets identical strictness from one import, the way
`.golangci.yml` is one shared file. The package's flat-config array includes:

- `@eslint/js` recommended
- `typescript-eslint` `strictTypeChecked` + `stylisticTypeChecked`
- `eslint-plugin-react` (flat recommended + jsx-runtime), `eslint-plugin-react-hooks`,
  `eslint-plugin-react-refresh` (vite)
- `eslint-plugin-jsx-a11y` strict
- `eslint-config-prettier`
- The webApp's existing custom `error` rules (verbatim): `no-explicit-any`,
  `explicit-function-return-type`, `explicit-module-boundary-types`,
  `no-non-null-assertion`, `consistent-type-imports`/`-exports`,
  `no-import-type-side-effects`, `strict-boolean-expressions`,
  `switch-exhaustiveness-check`, `no-unnecessary-condition`,
  `prefer-nullish-coalescing`, `prefer-optional-chain`, and the React rules
  (`jsx-no-leaked-render`, `hook-use-state`, `jsx-curly-brace-presence`,
  `self-closing-comp`, `jsx-sort-props`, `prop-types: off`).
- `eslint-plugin-boundaries` with the element map + matrix above.

### Consumption shape

Exports a **factory** (default export) so apps supply only project-local bits:

```js
// app eslint.config.js
import archWeb from '@mixofreality-studio/archistrator-platform-eslint-config-web'

export default archWeb({
  tsconfigRootDir: import.meta.dirname,
  ignores: ['dist', 'src/api/schema.ts'],   // generated file content
})
```

The factory returns the full flat-config array (baseline + boundaries), wiring
`parserOptions.projectService`, `tsconfigRootDir`, and app-supplied `ignores`. Element
patterns are canonical (`src/routes`, `src/components`, `src/hooks`, `src/api`,
`src/utilities`), so any app using the canonical layout needs no boundary config.

### Dependencies

The package `dependencies` carry all the eslint plugins above (so consumers get them
transitively via one install). `peerDependencies`: `eslint >=9`, `typescript >=5.9`.

## webApp Migration (proving ground — Option A)

The webApp is migrated now as the reference implementation. The measured current
state (2026-07-04): `src/api/` = 14 files, only ~5 files import the IO surface outside
`hooks/`, and all 5 import merely `ApiError` (an error class), not `apiClient`. So the
migration is **mechanical moves + import rewrites**, with no data-fetching logic moved
into hooks.

1. **`src/screens/` → `src/routes/`**; move `src/navigation/router.tsx` →
   `src/routes/router.tsx`. `src/App.tsx` stays at src root, classified `routes`.
2. **Create `src/contracts/`** and move the inert data files there: `types.ts`,
   `enums.ts`, `models.ts`, `operationsTypes.ts`, `adapters.ts`, `projectAdapters.ts`,
   `constructionAdapters.ts`, `operationsAdapters.ts`, `serviceContracts.ts`,
   `contractComponentId.ts`, `constructionRows.ts`, `wire.ts`, and the generated
   `schema.ts`.
3. **Extract error types from `src/api/client.ts`** — move `ApiError`, `WireError`,
   `toApiError` into `src/contracts/errors.ts`. `src/api/client.ts` then exports only
   `apiClient` (the `createClient<paths>` instance). The 5 `ApiError` importers now
   point at `contracts/errors`, resolving the only cross-layer "violations" cleanly.
4. **Create `src/utilities/`** and move `theme/`, `auth/`, `constants/`, `config.ts`,
   `data/` under it (`src/utilities/{theme,auth,constants,config,data}`).
5. **Update the codegen** `scripts/gen-api.mjs` to emit `schema.ts` into
   `src/contracts/` instead of `src/api/`, and the eslint `ignores` entry accordingly
   (`src/contracts/schema.ts`).
6. **Rewrite all import specifiers** across `src/**` to the new paths (scripted, then
   gated by `tsc --noEmit` which will flag any missed path). Update `tsconfig` paths if
   any alias references the moved dirs.
7. **Replace `webApp/eslint.config.js`** with the factory import; drop the now-
   duplicated baseline + plugin devDependencies (they arrive via the shared package).
8. **Verify** `npm run typecheck`, `npm run lint`, `npm run build` all clean; then run
   the app locally on real state (per the founder's standing review loop) to confirm no
   runtime regressions from the moves.

Ignored entry/ambient files: `src/main.tsx`, `src/vite-env.d.ts`.

**Review note:** file moves that touch rendered screens fall under the founder's
"STOP-for-review per UI change" loop. This migration changes *only import paths and
file locations*, not component behavior or markup — but the branch is left for founder
review (run locally) before merge rather than self-merged.

## Testing

- **Package fixtures:** the eslint-config package ships a `fixtures/` tree with a
  *valid* sample app (passes clean) and an *invalid* sample app exercising each rule —
  component→api (IO client), hook→component (upward), route→api, api→hooks (upward),
  and an unclassified file — each asserted to produce the expected boundary error. A
  `test` script runs eslint over the fixtures and checks the error set. This is the
  analogue of `framework-go/arch`'s self-test.
- **webApp:** `npm run lint` clean post-migration is the integration proof.

## Rollout / Risks

- **Rename churn** (`screens`→`routes`, utilities consolidation) touches many imports;
  done in one migration commit with typecheck + local run as the gate.
- **Generated `schema.ts`:** now lives in `src/contracts/` (inert types). The eslint
  `ignores` skips linting its content, but `src/contracts` remains a classified leaf —
  the ignore is for lint rules on generated code, not for boundary classification. The
  codegen script's output path moves with it.
- **Future apps** adopt by (a) using the canonical `src/<layer>` layout and (b) the
  three-line factory import. No per-app boundary config needed.

## Open follow-ups (out of scope here)

- Whether `framework-web`'s own `src/` (a component *library*, not an app) should adopt
  a library-flavored variant of these rules — deferred.
- A shared canonical `tsconfig` base package (the maximally-strict flags are currently
  duplicated between `framework-web` and the webApp) — noted, not in this spec.
