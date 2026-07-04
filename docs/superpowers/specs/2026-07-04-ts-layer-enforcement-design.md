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
one thing: call the generated API client. Five element types:

| Layer        | Folder        | Role                                                        |
|--------------|---------------|------------------------------------------------------------|
| `routes`     | `src/routes/` | Pages / screens (top). Compose components.                 |
| `components` | `src/components/` | Dumb views. No business logic, no direct API access.   |
| `hooks`      | `src/hooks/`  | Thin data-access. TanStack Query wrappers over the client. |
| `api`        | `src/api/`    | **Generated** client (`schema.ts`, `client.ts`). Leaf.     |
| `utilities`  | `src/utilities/` | Cross-cutting leaf: theme, auth context, constants, config. |

### Import rule matrix (downward-only; sideways *within* a layer allowed)

```
routes      → components, hooks, utilities
components  → components (siblings), hooks, utilities
hooks       → hooks (siblings), api, utilities
api         → utilities                      (generated leaf)
utilities   → utilities (siblings)           (leaf)
```

Key constraints, all decided:
- **Only `hooks` may import `api`.** Routes and components never touch the generated
  client directly — they get data via hooks (or props). This is the "dumb views"
  guarantee.
- **No upward imports.** `hooks` cannot import `components` or `routes`;
  `components` cannot import `routes`.
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
  (mode `folder`, canonical `src/<layer>` patterns).
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

The webApp is migrated now as the reference implementation:

1. **Rename `src/screens/` → `src/routes/`** (update imports; `src/navigation/router.tsx`
   references screens).
2. **Consolidate cross-cutting dirs under `src/utilities/`:** move `theme/`, `auth/`,
   `constants/`, `config.ts`, `data/` → `src/utilities/{theme,auth,constants,config,data}`
   (or keep `config.ts` as `src/utilities/config.ts`). Update imports.
3. **Router wiring** (`src/navigation/router.tsx`): classify as `routes` (route tree
   definition) — move to `src/routes/` or map `navigation` as part of the routes
   element. Decision: move under `src/routes/`.
4. **Replace `webApp/eslint.config.js`** with the factory import above; drop the now-
   duplicated baseline and plugin devDependencies (they arrive via the shared package).
5. **Run `eslint .` and fix every real violation** the boundary rules surface — most
   likely a `component` importing `api/` directly, which must be rerouted through a
   hook. Each such fix is a genuine "dumb views" correction, not a lint workaround.
6. **Verify** `npm run lint` and `npm run typecheck` are clean; run the app locally on
   real state (per the founder's standing review loop) to confirm no runtime
   regressions from the moves.

Ignored entry/ambient files during migration: `src/main.tsx`, `src/vite-env.d.ts`.

## Testing

- **Package fixtures:** the eslint-config package ships a `fixtures/` tree with a
  *valid* sample app (passes clean) and an *invalid* sample app exercising each rule —
  component→api, hook→component, route imported by hook, an unclassified file — each
  asserted to produce the expected boundary error. A `test` script runs eslint over the
  fixtures and checks the error set. This is the analogue of `framework-go/arch`'s
  self-test.
- **webApp:** `npm run lint` clean post-migration is the integration proof.

## Rollout / Risks

- **Rename churn** (`screens`→`routes`, utilities consolidation) touches many imports;
  done in one migration commit with typecheck + local run as the gate.
- **Generated `api/`:** `boundaries/elements` must classify `src/api` as `api` and the
  config must still ignore `src/api/schema.ts` *content* (generated), while the folder
  itself remains a classified leaf — the ignore is for lint rules on generated code,
  not for boundary classification.
- **Future apps** adopt by (a) using the canonical `src/<layer>` layout and (b) the
  three-line factory import. No per-app boundary config needed.

## Open follow-ups (out of scope here)

- Whether `framework-web`'s own `src/` (a component *library*, not an app) should adopt
  a library-flavored variant of these rules — deferred.
- A shared canonical `tsconfig` base package (the maximally-strict flags are currently
  duplicated between `framework-web` and the webApp) — noted, not in this spec.
