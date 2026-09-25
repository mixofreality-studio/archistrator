# Activity Experience — Stage 5 (webApp) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse three phase rails and three UIs into ONE plan of activities and ONE full-screen Activity Experience (spec §7). Today the SPA has a Phase-1 design experience (`routes/DesignExperience.tsx` → `containers/SystemDesignContainer.tsx` → `SystemDesignView`'s step ladder), a Phase-2 rail (`routes/ProjectDesignExperience.tsx`, 952 L), and a construction console (`routes/ConstructionConsole.tsx`, 1425 L → `ConstructionShell` → `ActivityTreeView`/`ActivityGraphLens`/`TasksLens` + a 1925-line `DetailPane`). Stage 5 replaces all of it with two routes — `/project/$projectId/plan?lens=list|graph|tasks` and `/project/$projectId/activity/$activityId?task=&rev=` — reading the stage-0/3 `constructionQueryActivityView` contract (`hooks/useActivityView.ts`, which **no screen calls today**) and `systemDesignGetProject`. The branching stepper, the git-style lane layout, the revision select and the plan-graph layout already exist as a prototype; Task 1 lands them and the last code task deletes what they replace.

**Architecture:** Strict layering (lint-enforced, `eslint.platform.config.js` `BOUNDARY_RULES`): `routes/*` (route literal + `getRouteApi`) → `containers/*` (fetch + mutate) → `components/activity/*` (pure, no hooks/api imports). Every rule the screens obey is a pure, node-tested `.ts` module beside the component that renders it — the house convention (`tasksLensCopy.ts`, `submitVerb.ts`, `graphViewport.ts`), not a new one. Wire→props adaptation is a tested module, never an inline map: the wire says `passed`/`completed` and an outcome enum, the graph props say `done`/`passed` and a display string. The read is per-activity on the activity route and **derived from `GetProject.activityExecution` on the plan route** — 32 per-activity reads on one screen is not a read model, it is a stampede (R3). Nothing in this stage changes a server contract; two server-side gaps (artifact-as-of-revision, construction comment lifecycle) are shipped as honest, tested read-only postures and earmarked for stage 4.

**Tech Stack:** React 19.2 + TypeScript 5.9 (strict), `@tanstack/react-router` 1.135 (code-based tree, `getRouteApi('<literal>')`, never generics), `@tanstack/react-query` 5.90, MUI 7.3, `@xyflow/react` 12.10, `node --test` over `src/**/*.test.ts` (cannot import `.tsx` — every tested module is `.ts`), eslint 9.39 + `eslint-plugin-boundaries` 6 + `jsx-a11y` **strict** + `react-hooks/exhaustive-deps: error`, Playwright 1.50 in `uitests/` (its own toolchain).

**Spec:** `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` — **§7 in full (7.1 components, 7.2 Activity Experience, 7.3 Plan, 7.4 Deleted, 7.5 component gaps)**, §6 (Project Design · M0), §2 (vocabulary: Activity / LifecyclePhase / Task / Attempt / Revision / Episode), §8 stage-5 row ("Starts after stage 1 against preview fixtures validated by the OAS schema; lands after 3–4"), §9 (testing). Recon of record: the stage-5 read-only recon (24 gaps, taken at `ea0313f2`; every anchor below re-verified in this worktree at `5e09b0b6`). Predecessor plans whose facts this plan builds on: `docs/superpowers/plans/2026-09-21-activity-experience-stage0.md` (the `QueryActivityView` read contract), `docs/superpowers/plans/2026-09-23-activity-experience-stage3.md` (the persisted rounds this screen renders).

## Rulings carried into this plan

Each was decided during planning; an implementer does not re-litigate them.

| # | Ruling |
|---|---|
| **R1** | **Artifact as of a non-latest revision is NOT read in stage 5** (GAP-5 — no op takes a ref/sha; `ConstructionReviewSubjectRef.ref` names a git object nothing exposes over HTTP). The true as-of read is stage 4's `QueryActivityView` (spec §4). Stage 5's non-latest view = a read-only banner `Revision N of M — read-only` + **Back to latest**, that revision's thread with resolutions EXPANDED, its verdicts and its note, and the artifact panel shows the **current** artifact under a one-line caption from `activityCopy.ts` saying so. Earmarked. |
| **R2** | **No server op is added for the construction thread lifecycle** (GAP-6 — `constructionManager` has no `SetReviewCommentStatus`/`AskQuestions`). Resolve / Reopen / Ask render only where a mutation exists: design activities (`useSetReviewCommentStatus`, `useAskQuestions`), projectDesign (`useSetProjectReviewCommentStatus`); construction gets none — the EXISTING `CommentMargin` posture of omitting `onResolve`/`onReopen` (its own doc at `MarginThreadCard.tsx:70-73` already states this contract). Earmarked for stage 4's `SubmitReviewDecision`. |
| **R3** | **Plan-wide mini lifecycles are derived CLIENT-SIDE** (GAP-7) from `GetProject.activityExecution[<id>]` (`Phases`, `CurrentPhase`, `BuildStatus`, `Phase`, `attempts`, `Type`, `Variant`) joined with `lifecycles.gen.ts`. **No per-activity `QueryActivityView` read on the plan screen.** A pure tested module `components/activity/miniLifecycleFromRow.ts` produces `LifecycleNode[]` (states only; `revisions: []`), mirroring what `components/construction/graph/laneSpine.ts` does today. |
| **R4** | **The build-order row is derived client-side** (GAP-4 — the server reports `layer: ""`, `layerBand: "projectWide"` for requirements/architecture/projectDesign/N-STP/N-IT). Pure tested module `components/activity/planRowFor.ts`: `type ∈ {requirements, architecture, projectDesign} → 'frontEnd'`; `testing` + `plan → 'sideLane'`; `testing` + `systemTest → 'systemTesting'`; else `layer ∈ {resource, resourceAccess, engine, manager, client} →` the same-named row; anything else → `undefined` (the tile is LISTED in LIST but NOT DRAWN in GRAPH, under a tested copy line). No server change. |
| **R5** | **The `deployment` `PlanRow` is DROPPED** (GAP-9) from `PlanRow`, `STACK_ROWS` and `ROW_LABEL`. The live model has no `deployment` layer (`slots.5.model.components[].layer` ∈ {resourceAccess 10, resource 8, engine 7, manager 5, utility 4, client 3}) and §7.3's row list omits it. The proto's invented `N-DEP` fixture is not ported. |
| **R6** | **Hover-focus is the TRANSITIVE upstream + downstream closure** over dependency edges (GAP-22 — §7.3's "lights the full upstream + downstream chain" is binding; both existing implementations light direct neighbours only). `planFocusFor` is rewritten with tests for a 3-chain, a fork, and M0 (= every non-`frontEnd` tile). |
| **R7** | **Adapters are pure modules in `components/activity/`** (GAP-19/20/21). `components → contracts` is legal and `contracts → contracts` only, so an adapter that must read `lifecycles.gen.ts` CANNOT live in `contracts/`. Two modules: `activityViewToGraph.ts` (wire `ConstructionActivityView` → `{nodes, phases}` + the per-task `workerClass`/`command`/`exitCriterion`/`artifactKind` join) and `threadAdapter.ts` (`ConstructionReviewThreadComment[]` → `ReviewCommentView[]`). Every adapter has node tests. **A review task carries no `artifactKind` of its own** (verified across all 14 lifecycles: `missionReview`, `glossaryReview`, `srsReview`, `designReview`, `codeReview`, `stpReview`, `testing`, `architectureReview` all have `reviews` and no `artifactKind`; the lone exception is `projectDesign`'s `sdpReview`, which has `artifactKind: 'SdpReview'` and no `reviews` because it judges no dispatch). `taskFactsFor` therefore resolves it **own `artifactKind` → the `reviews` target's → the `revisionGroup` sibling's**. |
| **R8** | **Routes:** `/project/$projectId/activity/$activityId?task=&rev=` and `/project/$projectId/plan?lens=list\|graph\|tasks`. The route PATH LITERALS and their builders live in **`src/contracts/routePaths.ts`** — a contracts leaf that imports nothing, so routes, containers AND components may all import it (`BOUNDARY_RULES`: `components → [components, contracts, utilities]`; nothing outside `routes/` may import `routes/`). `routes/activityRedirect.ts` re-exports them and owns the `redirect()` throw. `useLensSelection`'s ROUTE-facing search codec is reduced to `lens`; `a`/`p`/`k`/`n`/`av`/`focus`/`sc` die with the DetailPane. **`LensSelection` (the type) SURVIVES** — `tasks/TasksLens.tsx:47` and `tasks/decisionFlow.ts:52` both consume it and both are KEEP — narrowed to the members they use. `/project/$projectId/construction`, `/design/system/*`, `/design/project/*` redirect to `/plan` via `beforeLoad`, unit-tested exactly like `routes/operationsGuard.test.ts`. `?lens=tasks` (TasksLens) SURVIVES; its decision rows navigate to the activity route with `?task=<gate>`. |
| **R9** | **Default task** when `?task` is absent (spec §7.2): awaiting-human → failed → running → last passed → first task. Promoted out of the proto's `defaultNodeId` into a tested pure module. |
| **R10** | **Persisted pending comments** (GAP-23): `setActiveKey` gains the key shape `activity:<projectId>:<activityId>:<taskId>:<rev>`. The store's `projectVersion` incarnation stamp is kept (it is what makes a shape change safe). Old design keys (`<projectId>:<kind>`) are untouched — the design rail still uses them until stage 6. |
| **R11** | **`resolveSubmitVerb` gains `allowSendBack?: boolean`** (default `true`) and `approveCopy?: { label: string; consequence: string }` (GAP-8). With `allowSendBack === false`, staged change-requests stay comments and the primary verb stays `approve`. `SubmitBar` threads both. projectDesign passes `allowSendBack: false` + "Approve plan & cost — start construction" and renders an `Amend Architecture →` link in place of Send back. |
| **R12** | **Write ops per activity type** (today's ops through today's hooks; stage 4 re-keys at the hook layer only). The container owns this as a pure tested dispatch table `containers/activityVerbs.ts`. Construction types: approve/send-back = `useSubmitPhaseDecision` (`phase` = the task's lifecycle-phase wire name), run = `useBeginConstruction`, override = `useOverrideActivity`. `requirements`/`architecture`: `useSubmitReviewDecision` (`kind` = the task's `artifactKind` mapped to `ArtifactKind`), `useSetReviewCommentStatus`, `useAskQuestions`, `useAcknowledgeStaleBasis`, `useRequestArtifactDraft`. `projectDesign`: `useSubmitSDPDecision(optionId)` then `useAdvanceToConstruction`; comments via `useSetProjectReviewCommentStatus`; NO send-back. |
| **R13** | **Fixtures move** (GAP-11): the four `activity-experience` fixtures move to `uitests/preview-fixtures/web-client/activity-experience/` (which is where the `preview` Playwright project looks), gain the GAP-3 scenarios, and a `plan/` set is added. `webApp/preview/fixtures/web-client/activity-experience/` keeps ONE smoke fixture so `fixture-schema.test.mjs`'s second tree assertion still has something to validate. |
| **R14** | **Playwright is AUTHORED, not promoted** (GAP-1): the spec §9 line "promoted from the prototype (`interact.mjs`)" has no artefact behind it — that file exists in no branch and no working tree. Stage 5 writes `uitests/tests/preview/activity-experience.spec.ts` and `plan.spec.ts` from scratch against the preview fixtures (no Go server), selecting ONLY by `UI_IDENTIFIERS` ids, plus a 2-assertion `live` spec that skips without a server. |
| **R15** | **Deletions happen LAST**, after the new routes are green and the redirects exist, and **a file is deleted only when the grep proves ZERO surviving importers**. `laneSpine.ts` is NOT reused (R3 supersedes it) and goes with the lens; `graphViewport.ts` and `rowGutter.ts` (both zero-import, verified) MOVE to `components/activity/`; every other `construction/graph/` file is deleted. Three things the first draft wrongly listed as deletable are **KEPT, deferred to stage 6**, because the out-of-scope `containers/McpSystemDesignContainer.tsx` consumes them: `SystemDesignView`'s step ladder (`:55 import { SystemDesignView, type SpineStep }`, `:82 buildSpine`, `:215`, `:419 spine={spine}`), `components/design/SlimSpine.tsx` (`SystemDesignView.tsx:64`), and `PHASE1_ORDER` (`McpSystemDesignContainer.tsx:38`, plus `HomeBase.tsx:49` and `uitests/tests/support/testids.ts:26`). `components/construction/list/*` (except `ActivityTreeView.tsx`) and most of `detail/bodies/*` are likewise KEPT — `tasks/TasksLens.tsx:48` reaches `list/activityRowPresentation.ts` which reaches `list/activityTree.ts`, and four surviving `tasks/*` modules reach `bodies/taskBriefing.ts`. Review aids (`ContractCodeFlow`, `ScenarioBrowser`, `FrontendArtifactView.PreviewSection`, `ComponentRelationshipsView`, `ServiceContractView`, `TestPlanView`, `components/project/*`) are KEPT **and their only reachable caller is replaced first** — Task 9 ports `ArtifactBody.tsx`'s renderer dispatch into `ArtifactPanel` BEFORE Task 13 deletes it. |
| **R16** | **§7.5 gaps 1–4** are fixed as the recon's minimal fixes describe — `ReadOnlyRow` in `CommentableList`, `OpRow` in `ContractSignatureList`, `roleLine`/`footerNote` props on `GeneratingScene`, `expandResolved` on `MarginThreadCard` + `CommentMargin`. Each has a test (a node test where non-DOM, otherwise the Playwright preview assertion). |
| **R17** | **No new artifact renderer is written in stage 5** (GAP-10 — `components/construction/artifactRenderers.tsx` covers 4 of 14 classifications). Stage 5 adds a tested `taskArtifactFor()` dispatcher that maps a task to an existing renderer or to a typed `unavailable` result carrying a copy line naming exactly what is missing. The seven unrendered classifications are earmarked. |
| **R18** | **GAP-14** — the stale comment at `server/internal/manager/construction/constructionmanager.go:785-787` (it claims design activities are `NotFound`; stage 2 made them real) is fixed in the docs task. Comment only; `GOWORK=off make lint` must stay green. |
| **R19** | **Layering + copy.** `routes/ActivityExperience.tsx` → `containers/ActivityExperienceContainer.tsx` → `components/activity/*`; `routes/Plan.tsx` → `containers/PlanContainer.tsx` → `components/activity/{PlanList,PlanGraph}.tsx` + the surviving `TasksLens`. No file joins `LEGACY_COMPONENTS_HOOKS_FILES` (it only shrinks). Every sentence a new screen says lives in a `*Copy.ts` with node tests. Viewport/selection memory is module memory keyed by a content signature (`graphViewport.ts`'s pattern), never React state a 1.5 s poll can wipe. |
| **R20** | **Polling.** The activity route uses `useActivityView` unchanged (2 s live / 8 s at a gate / 5 s degraded / off on 404 · done · failed). The plan route uses `useProject` with the existing cascade predicate (`components/construction/lens/beginControl.ts:377 consolePollMs`), moved with the pipeline. |

## Global Constraints

- Work in the git worktree `.claude/worktrees/activity-stage5` (branch `activity-experience-stage5`, from `main` @`5e09b0b6`). The main checkout is shared with other sessions. `cd webApp && npm install` is already done in this worktree (`node_modules` present — verified).
- `ASDF_NODEJS_VERSION=lts` on EVERY npm/npx command. `GOWORK=off` on every `go`/`make` command under `server/`.
- **Gates before every commit:**
  - `cd webApp && ASDF_NODEJS_VERSION=lts npm run check` — `tsc -b` → `eslint .` (boundaries + `jsx-a11y` strict + `react-hooks/exhaustive-deps: error`) → `prettier --check "src/**/*.{ts,tsx,css,json}"` → `node --test 'src/**/*.test.ts' 'scripts/**/*.test.mjs'` (which includes `fixture-schema.test.mjs`).
  - `cd uitests && ASDF_NODEJS_VERSION=lts npx tsc --noEmit && ASDF_NODEJS_VERSION=lts npx eslint .` whenever `uitests/` is touched.
  - `cd uitests && ASDF_NODEJS_VERSION=lts npx playwright test --project=preview` at every task that touches a fixture or a screen. It builds the preview itself and needs no Go server.
  - `cd server && GOWORK=off make lint` + `GOWORK=off make gen-lifecycles-check` when a server file is touched (Task 14 only).
- **Never** run `git restore`, `git clean`, `git stash`, `git checkout -- <path>` or any tree-wide reset. The untracked prototype under `webApp/proto/` is 3 415 lines that exist on NO branch (GAP-24); a clean destroys it. Task 1 commits the reusable half; `proto/` itself is removed by `rm -rf` in Task 14 and never by a git verb.
- **No `eslint-disable`, no `@ts-expect-error`, no `any`, no new entry in `LEGACY_COMPONENTS_HOOKS_FILES`** (`eslint.platform.config.js:79` — it only shrinks). Decompose instead.
- **Nothing outside `routes/` may import `routes/`.** `BOUNDARY_RULES` (`eslint.platform.config.js:63-71`) gives `containers → [containers, components, hooks, contracts, utilities]` and `components → [components, contracts, utilities]`; neither list holds `routes`. Every shared route constant therefore lives in `src/contracts/routePaths.ts` (Task 7 Step 1) — a contracts LEAF with zero imports of its own, which `contracts → contracts` permits.
- `src/contracts/schema.ts`, `src/contracts/enums.gen.ts`, `src/api/ops.gen.ts` and `src/components/activity/lifecycles.gen.ts` are GENERATED — never hand-edited. Do not run `npm run gen:api` / `gen:ops` / `make gen-lifecycles` in this stage: no OAS input changes, and a regen would put unrelated drift in a stage-5 commit.
- `node --test` runs `src/**/*.test.ts` only — it **cannot import `.tsx`**. Every rule that needs a test lives in a `.ts` module; a `.tsx` component holds rendering and nothing else. (This is why `submitVerb.ts`, `commentMarginLayout.ts` and `reviewBatch.ts` are shaped the way they are.)
- **No i18n, copy-in-a-pure-module instead** (house convention: `tasksLensCopy.ts`, `serviceContractCopy.ts`, `graphPresentation.ts`, `roleLine.ts`). Every sentence the Activity Experience or the Plan says lives in a `*Copy.ts` with node tests.
- **a11y house rules** (the `jsx-a11y` strict preset is on): colour is never the sole carrier of meaning; a read-only surface renders zero orphaned ARIA and zero focusable ghosts; focus moves to a region HEADING (`tabIndex={-1}`) on open and is restored on close; `utilities/reducedMotion.ts prefersReducedMotion` is respected in anything animated. `LifecycleGraph` already ships ContextMenu-key / Shift+F10 / caret affordances for its revision menu — keep all three.
- **Every `localStorage` access is wrapped in `try/catch`** (it throws outright in a private window) — see `pendingCommentsStore.ts` and `graphViewport.ts`.
- **No route lazy-loading.** There is zero `React.lazy` / dynamic `import()` in `src/` and no `manualChunks` in `vite.config.ts`; `scripts/check-prod-bundle.mjs` asserts `dist/` carries no preview code. Do not introduce code splitting.
- Every `data-testid` in `src/` comes from `UI_IDENTIFIERS`; `uitests` selects only by those published ids (its `eslint.config.js` has a `no-inline-testid` rule).
- Match the surrounding code's comment density, naming and em-dash rationale idiom. Commit messages end with:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  ```
- **Founder review per UI change** is satisfied by the Playwright preview suite plus one screenshot per new screen saved to the SDD workspace. The founder reviews at the END of the stage — do not stop mid-plan to ask.
- **Out of scope, EARMARK only:** the artifact-as-of-revision read (R1), a construction comment-lifecycle op (R2), the seven missing artifact renderers (R17), `QueryProjectView` / the batched plan read (stage 4), `McpSystemDesignContainer` + `SystemDesignView` (stage 6), deleting `ArtifactSlot.ReviewThread` / the stored derived fields (stage 6). No deploy in this plan.

## Task order

**Foundation, in one commit each:** Task 1 (land the prototype `src/` files + the `eyebrow` prop + the `ActivityLifecycle` id block) → Task 2 (the wire→props adapters and the copy module) → Task 3 (the plan-side pure modules: build-order row, mini lifecycle, transitive focus, `deployment` dropped).

**Then the shared UI fixes the screens depend on:** Task 4 (`submitVerb` `allowSendBack` + `approveCopy`, `SubmitBar`) → Task 5 (§7.5 gaps 1–4).

**Then the data the screens are built against:** Task 6 (fixtures move + the GAP-3 scenarios + the plan fixture set + schema realism).

**Then the screens:** Task 7 (routes + redirects, both screens stubbed and reachable) → Task 8 (Activity Experience container/route + the dispatch body) → Task 9 (the review body, the verbs table, the projectDesign M0 body) → Task 10 (the revision select + the non-latest read-only mode + the per-task pending-comment key) → Task 11 (Plan LIST + GRAPH + TASKS survival + the HomeBase card).

**Then proof and removal:** Task 12 (Playwright preview suites + the live smoke) → Task 13 (the §7.4 deletions, with a consumer grep per file, including the uitests specs that only exercised the deleted screens) → Task 14 (docs, earmarks, the GAP-14 server comment, `rm -rf webApp/proto*`).

**Must ship together:**
- Task 1 is ONE commit: the prototype files typecheck only as a set (they import each other) and `planGraphLayout.ts` imports `../flow/flowLayoutCore.ts`.
- Task 7 is ONE commit: a route registered without its `beforeLoad` redirect leaves `/construction` dead, and a redirect without its target route is a loop.
- Task 13 is ONE commit per deleted CLUSTER (console, design rails, home cards), each with its own grep evidence — never a bulk delete.

**Parallelism, corrected.** Task 3 is NOT disjoint from Task 2: `miniLifecycleFromRow.ts` imports `lifecycleKeyFor` from `activityViewToGraph.ts` (Task 2 Step 1). **Run 2 → 3 in that order.** Tasks 4 and 5 touch only `components/design/` + `components/comments/` and are genuinely disjoint from 2/3 and from each other; those three streams may run in parallel. Everything from Task 6 onward is strictly sequential.

## Execution risks and how this plan removes each

Six hazards a fresh implementer would otherwise hit with a red gate and no plan text to resolve them. Each is named here and resolved in the task that owns it.

1. **A review task has no `artifactKind`, so every design activity would lose its approve verb.** `taskFactsFor` resolves it through `task.reviews` → the reviewed dispatch's `artifactKind`, falling back to the `revisionGroup` sibling; `projectDesign`'s `sdpReview` carries its own and is the one task that needs neither hop. Task 2 Step 1 (code), Task 2 Step 2 (test), Task 9 Step 1 (the verb table consumes the RESOLVED kind).
2. **`ArtifactRendererProps` has no source on the activity route.** `artifactRenderers.tsx:25-31` wants `{ vm: { activityId, name, row: ConstructionRow }, project, systemEnvelope, t }`, and `useActivityView` carries none of it. The container therefore calls `useProject(projectId)` for EVERY activity type (not only projectDesign) and builds `vm` from `data.constructionRows[activityId]`. Task 9 Step 4 (props, verbatim), Task 8 Step 2 (the second read).
3. **The plan screen's input type is not what `useProject` returns.** `planActivitiesFrom` takes `Readonly<Record<string, ConstructionRow>>` — i.e. `useProject(projectId).data.constructionRows`, already ordinal-decoded and camel-cased by `contracts/wire.ts:479 mapConstructionRow` into `contracts/types.ts:727 ConstructionRow` — whose `kind`/`variant`/`layer`/`currentLifecyclePhase`/`status`/`phases` feed `planRowFor` and `miniLifecycleFromRow` unchanged. The raw `SystemDesignActivityConstructionStatus` (PascalCase, ordinal `Type`/`Variant`) is never handled in `components/`. Task 3 Interfaces + Task 11 Step 2.
4. **Dropping the `deployment` row reddens the landed test file.** `planGraphLayout.test.ts` carries an `N-DEP` tile at `:33`, an eight-row expectation at `:46-60` (incl. `L.height === 7 * PLAN_ROW_H + PLAN_TILE.h`), two `N-DEP` edge assertions at `:92-94`, and two focus tests at `:128` / `:137` whose expectations the transitive rule contradicts. Task 3 Step 5 and Step 7 REWRITE all five, line by line.
5. **Deleting the console breaks files that survive it.** `detail/bodies/*` and `list/*` are KEPT except `ActivityTreeView.tsx`, `FocusView.tsx`, `bodyDispatch.ts`, `ArtifactBody.tsx`, `artifactPlacement.ts`, `ArtifactPlacementView.tsx`, `ContractSummaryCard.tsx`, `ComponentTestPlanBody.tsx`, `componentCoverage.ts`, `ArtifactFrame.tsx`, `UnknownBody.tsx`, `AbsentBody.tsx`, `ProvenanceNote.tsx`, `ReviewBody.tsx`, `reviewVerdict.ts`, `EpisodeBody.tsx`, `SubagentGantt.tsx` and their tests — every one of which the greps in Task 13's table prove has no surviving importer once `DetailPane.tsx` goes. `SystemDesignView`'s step ladder, `SlimSpine.tsx`, `SpineStep` and `PHASE1_ORDER` are all KEPT because the out-of-scope `McpSystemDesignContainer.tsx:38,55,215,419` consumes them.
6. **TASKS "now includes design reviews" has no data source today.** `owedWorkFor`'s gate path reads `constructionGetSessionState`, which exists only for construction activities — a design activity waiting at a review gate is invisible to it. Task 11 Step 5 adds a SECOND, definite source that needs no new op and no new poll: a design activity owes a decision exactly when one of its lifecycle's artifact slots is `stage === 'awaitingReview'` (`ArtifactSlotView.stage`, `ARTIFACT_STAGE_APP_STRINGS = ['empty','awaitingReview','committed','rejected','withdrawn']`), which `useProject` already returns.

---

### Task 1: Land the prototype into `src/`

The reusable half of the prototype is already sitting in this worktree as UNTRACKED files (verified: `git status --short` lists all ten, plus working-tree edits to two committed files). It is green as it stands. This task commits it and nothing else — no behaviour changes, no new consumers. Nothing imports these modules yet, which is exactly why this is one safe commit.

**Files:**
- Add (untracked → tracked, verbatim, no edits): `webApp/src/components/activity/lifecycleGraphTypes.ts` (162 L), `lifecycleGraphTypes.test.ts` (110 L), `lifecycleGraphLayout.ts` (174 L), `lifecycleGraphLayout.test.ts` (179 L), `lifecycleGraphGeometry.ts` (437 L), `lifecycleGraphGeometry.test.ts` (353 L), `planGraphLayout.ts` (234 L), `planGraphLayout.test.ts` (154 L), `LifecycleGraph.tsx` (721 L), `LifecycleGraphMini.tsx` (127 L), `RevisionSelect.tsx` (89 L).
- Modify (already edited in the working tree — keep the edits verbatim): `webApp/src/components/design/ExperienceChrome.tsx` (the `eyebrow` prop), `webApp/src/utilities/constants/UIIdentifiers.ts` (the `ActivityLifecycle` block).
- Do NOT add: `webApp/proto/**`, `webApp/proto.html`.

**Interfaces produced (every later task depends on these exact names):**

```ts
// components/activity/lifecycleGraphTypes.ts
export type LifecycleNodeKind = 'dispatch' | 'review';
export type LifecycleNodeState =
  | 'done' | 'running' | 'awaitingHuman' | 'sentBack' | 'failed' | 'locked' | 'pending';
export interface LifecycleRevision {
  n: number;                       // 1-based, ascending; highest is latest
  outcome: string;                 // shown VERBATIM
  detail?: string | undefined;     // "14m · 312k tok"
  at?: string | undefined;         // "Sep 12"
  commentCount?: number | undefined;
}
export interface LifecycleNode {
  id: string;
  kind: LifecycleNodeKind;
  title: string;
  phase: string;                   // LifecyclePhase.id
  state: LifecycleNodeState;
  dependsOn: readonly string[];
  revisions: readonly LifecycleRevision[];
  revisionGroup?: string | undefined;
  laneLabel?: string | undefined;
}
export interface LifecyclePhase { id: string; label: string; weight?: number | undefined; passed: boolean }
export interface LifecycleSelection { nodeId: string; revision: number }
export interface LifecycleBackEdge { from: string; to: string; revisions: number }
export function backEdgesOf(nodes: readonly LifecycleNode[]): LifecycleBackEdge[];
export function latestRevision(node: Pick<LifecycleNode, 'revisions'>): number;
export function revisionLine(r: LifecycleRevision, latest: boolean): string;
export function revisionOnNavigate(
  nodes: readonly LifecycleNode[], from: LifecycleSelection, toNodeId: string
): number;

// components/activity/LifecycleGraph.tsx
export interface LifecycleGraphProps {
  nodes: readonly LifecycleNode[];
  phases: readonly LifecyclePhase[];
  selected: LifecycleSelection;
  onSelect: (nodeId: string, revision: number, picked: boolean) => void;
  surface?: string | undefined;
}
export function LifecycleGraph(props: LifecycleGraphProps): ReactNode;

// components/activity/LifecycleGraphMini.tsx
export function LifecycleGraphMini(props: {
  nodes: readonly LifecycleNode[];
  label: string;                      // accessible name, e.g. "Lifecycle: 4 of 11 tasks done"
  surface?: string | undefined;
  maxWidth?: number | undefined;      // scales DOWN only
}): ReactNode;

// components/activity/RevisionSelect.tsx
export function RevisionSelect(props: {
  revisions: readonly LifecycleRevision[];
  value: number;
  onChange: (revision: number) => void;
}): ReactNode;

// components/activity/planGraphLayout.ts
export type PlanRow =
  | 'frontEnd' | 'resource' | 'resourceAccess' | 'engine'
  | 'manager' | 'deployment' | 'client' | 'systemTesting' | 'sideLane';   // 'deployment' dies in Task 3
export interface PlanTileInput { id: string; row: PlanRow; calls: readonly string[] }
export type PlanEdgeKind = 'sequence' | 'milestone' | 'call';
export interface PlanEdge { id: string; from: string; to: string; kind: PlanEdgeKind }
export interface PlanGraphRow { row: PlanRow; y: number; height: number; label: string }
export interface PlanGraphLayout {
  pos: ReadonlyMap<string, { x: number; y: number }>;
  rows: PlanGraphRow[]; edges: PlanEdge[]; width: number; height: number;
}
export const PLAN_TILE: { w: number; h: number };       // { w: NODE_W(188), h: 96 }
export const PLAN_MILESTONE: { w: number; h: number };  // { w: 132, h: 44 }
export const PLAN_COL_W: number;                        // COL_W = 220
export const PLAN_ROW_H: number;                        // ROW_H = 150
export function layoutPlanGraph(tiles: readonly PlanTileInput[], milestoneId: string | undefined): PlanGraphLayout;
export interface PlanFocus { tiles: ReadonlySet<string>; incident: (e: PlanEdge) => boolean }
export function planFocusFor(
  hoveredId: string, tiles: readonly PlanTileInput[], edges: readonly PlanEdge[], milestoneId: string | undefined
): PlanFocus;

// utilities/constants/UIIdentifiers.ts — UI_IDENTIFIERS.ActivityLifecycle
{
  GRAPH: 'lifecycle-graph',
  MINI: 'lifecycle-graph-mini',
  node: (nodeId: string) => `lifecycle-node-${nodeId}`,
  nodeMenuButton: (nodeId: string) => `lifecycle-node-menu-${nodeId}`,
  phase: (phaseId: string) => `lifecycle-phase-${phaseId}`,
  REVISION_MENU: 'lifecycle-revision-menu',
  REVISION_SELECT: 'lifecycle-revision-select',
  revisionOption: (n: number) => `lifecycle-revision-option-${String(n)}`,
  laneLabel: (nodeId: string) => `lifecycle-lane-label-${nodeId}`,
  revisionItem: (nodeId: string, n: number) => `lifecycle-revision-${nodeId}-${String(n)}`,
}

// components/design/ExperienceChrome.tsx — the new optional prop, already in the tree
eyebrow?: string | undefined;   // omitted ⇒ `PHASE ${phaseNum} · EXPERIENCE`, unchanged
```

- [ ] **Step 1: Confirm the files are there and have not been edited.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage5
  git status --short -- webApp/
  ASDF_NODEJS_VERSION=lts bash -c 'cd webApp && npx eslint src/components/activity'
  ```
  Expected: **eleven** untracked `src/components/activity/*` files (7 `.ts` + 3 `.tsx` + … count them: `lifecycleGraphTypes.ts`, `lifecycleGraphTypes.test.ts`, `lifecycleGraphLayout.ts`, `lifecycleGraphLayout.test.ts`, `lifecycleGraphGeometry.ts`, `lifecycleGraphGeometry.test.ts`, `planGraphLayout.ts`, `planGraphLayout.test.ts`, `LifecycleGraph.tsx`, `LifecycleGraphMini.tsx`, `RevisionSelect.tsx`) + two modified files + `webApp/proto/` + `webApp/proto.html`, and eslint clean. If eslint complains, FIX THE CODE — do not add a disable.

- [ ] **Step 2: Run the prototype's own tests.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts node --test 'src/components/activity/*.test.ts'
  ```
  Expected: **60 pass, 0 fail** (geometry 20, layout 11, types 10, planGraphLayout 12, the committed `lifecycles.gen.test.ts` 7). Spec §9 says "20 exist" — that sentence is stale and Task 14 corrects it.

- [ ] **Step 3: Full gate.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check
  ```
  Expected green. `LifecycleGraph.tsx` imports only `../../utilities/theme/*`, `../../utilities/constants/UIIdentifiers`, `../../contracts/exhaustive` and its siblings — all legal for a `components` file under `BOUNDARY_RULES`.

- [ ] **Step 4: Commit — the eleven files plus the two edits, and nothing else.**
  ```bash
  git add webApp/src/components/activity/ webApp/src/components/design/ExperienceChrome.tsx webApp/src/utilities/constants/UIIdentifiers.ts
  git status --short   # webApp/proto/ and webApp/proto.html MUST still be untracked
  git commit
  ```
  Message: `feat(activity): land the branching lifecycle graph, mini graph and revision select`.

---

### Task 2: The wire→props adapters and the Activity Experience's copy

The wire vocabulary and the graph's props vocabulary differ on purpose (`passed` vs `done`, `completed` vs `passed`, an outcome ENUM vs a display STRING), and four facts the dispatch header needs (`workerClass`, `command`, `exitCriterion`, `artifactKind`) are not on the wire at all — they live in `lifecycles.gen.ts`. One tested module owns both translations; nothing else may translate.

**Files:**
- Add: `webApp/src/components/activity/activityViewToGraph.ts`, `activityViewToGraph.test.ts`.
- Add: `webApp/src/components/activity/threadAdapter.ts`, `threadAdapter.test.ts`.
- Add: `webApp/src/components/activity/defaultTask.ts`, `defaultTask.test.ts`.
- Add: `webApp/src/components/activity/activityCopy.ts`, `activityCopy.test.ts`.

**Interfaces:**
- Consumes: `components['schemas']['ConstructionActivityView' | 'ConstructionActivityTaskView' | 'ConstructionTaskRevisionView' | 'ConstructionReviewThreadComment']` from `../../contracts/schema.ts` (generated; `contracts` is legal for `components`), `lifecycleFor` / `LifecycleDef` / `LifecycleTaskDef` / `LifecyclePhaseDef` / `LifecycleTypeKey` from `./lifecycles.gen.ts`, `ReviewCommentView` / `ReviewCommentReply` / `ReviewCommentStatus` / `ReviewCommentType` / `ReviewCommentAddressee` from `../../contracts/types.ts`, and `LifecycleNode` / `LifecyclePhase` / `LifecycleRevision` from `./lifecycleGraphTypes.ts` (Task 1).
- Produces the names in the code below.

> **Why `components/` and not `contracts/`:** `BOUNDARY_RULES` says `contracts → contracts` only, and these adapters must import `lifecycles.gen.ts`, which lives under `components/activity/`. `components → contracts` is legal, so this is the only placement that typechecks (verified against `eslint.platform.config.js:63-71`).

- [ ] **Step 1: Write `activityViewToGraph.ts`.**

  ```ts
  /**
   * The ONE translation between the `QueryActivityView` wire shape and the
   * lifecycle graph's props vocabulary — and the ONE place the four facts the
   * wire does not carry (worker class, command, exit criterion, artifact kind)
   * are joined in from the generated lifecycle table.
   *
   * The two vocabularies differ on purpose. The wire says a task `passed` and a
   * lifecycle phase is `completed`; the graph says a node is `done` and a phase
   * has `passed`. The wire's revision outcome is a closed enum; the graph shows
   * the outcome VERBATIM, so it needs a display string. Translating inline at
   * each call site is how two screens end up disagreeing about one task — the
   * defect this module exists to make impossible.
   *
   * Pure and React-free: `node --test` loads it directly.
   */
  import type { components } from '../../contracts/schema.ts';
  import {
    lifecycleFor,
    type LifecycleDef,
    type LifecycleTaskDef,
  } from './lifecycles.gen.ts';
  import type {
    LifecycleNode,
    LifecycleNodeState,
    LifecyclePhase,
    LifecycleRevision,
  } from './lifecycleGraphTypes.ts';

  export type ActivityViewWire = components['schemas']['ConstructionActivityView'];
  type TaskWire = components['schemas']['ConstructionActivityTaskView'];
  type RevisionWire = components['schemas']['ConstructionTaskRevisionView'];
  type TaskStateWire = components['schemas']['ConstructionActivityTaskState'];
  type OutcomeWire = components['schemas']['ConstructionTaskRevisionOutcome'];

  /**
   * The wire's task state → the graph's node state. Only `passed`/`done` differ;
   * the rest are the same word on both sides, and are listed anyway so a new
   * wire value fails to compile here rather than rendering as `pending`.
   */
  const NODE_STATE: Readonly<Record<TaskStateWire, LifecycleNodeState>> = {
    pending: 'pending',
    locked: 'locked',
    running: 'running',
    awaitingHuman: 'awaitingHuman',
    passed: 'done',
    sentBack: 'sentBack',
    failed: 'failed',
  };

  /** The revision outcome, said the way a reader says it. Shown verbatim. */
  export const OUTCOME_TEXT: Readonly<Record<OutcomeWire, string>> = {
    running: 'running',
    awaitingHuman: 'awaiting you',
    passed: 'approved',
    sentBack: 'sent back',
    failed: 'failed',
    skipped: 'skipped',
  };

  /**
   * The lifecycle key for a wire (type, variant) pair — the same rule
   * `projectstate.LifecycleKeyFor` applies server-side: `testing:<variant>` for
   * a testing activity, the bare type for everything else. A testing row that
   * arrived without a variant reads as the plan variant, matching the server's
   * zero `TestingVariant`.
   */
  export function lifecycleKeyFor(type: string, variant: string | undefined): string {
    if (type !== 'testing') return type;
    return `testing:${variant !== undefined && variant.length > 0 ? variant : 'plan'}`;
  }

  /** The dispatch header's facts for one task — every one a join, none on the wire. */
  export interface TaskFacts {
    workerClass?: string | undefined;
    command?: string | undefined;
    /** Resolved, never raw — see {@link artifactKindOf}. */
    artifactKind?: string | undefined;
    /** The binary exit criterion of the task's lifecycle phase. */
    exitCriterion?: string | undefined;
  }

  /**
   * WHICH ARTIFACT A TASK IS ABOUT.
   *
   * A REVIEW task carries no `artifactKind` of its own — it carries `reviews`,
   * naming the dispatch it judges, and that dispatch is what names the artifact.
   * Verified across all fourteen lifecycles: `missionReview`, `glossaryReview`,
   * `volatilitiesReview`, `coreUseCasesReview`, `architectureReview`, `srsReview`,
   * `designReview`, `codeReview`, `stpReview` and `testing` all have `reviews` and
   * no `artifactKind`. The ONE exception is `projectDesign`'s `sdpReview`, which
   * carries `artifactKind: 'SdpReview'` and NO `reviews`, because the SDP is
   * computed and there is no dispatch to judge (spec §6/R7).
   *
   * Reading `task.artifactKind` alone therefore returns `undefined` for every
   * design review in the product — and the review body's whole verb table keys on
   * it. The chain is: the task's own kind, then its `reviews` target's, then its
   * `revisionGroup` sibling's (the group a draft and its review share, which is
   * the same answer by another road and survives a lifecycle that ever omits
   * `reviews`).
   */
  export function artifactKindOf(def: LifecycleDef, task: LifecycleTaskDef): string | undefined {
    if (task.artifactKind !== undefined) return task.artifactKind;
    if (task.reviews !== undefined) {
      const judged = def.tasks.find((t) => t.id === task.reviews);
      if (judged?.artifactKind !== undefined) return judged.artifactKind;
    }
    const sibling = def.tasks.find(
      (t) => t.id !== task.id && t.revisionGroup === task.revisionGroup && t.artifactKind !== undefined
    );
    return sibling?.artifactKind;
  }

  export function taskFactsFor(view: ActivityViewWire, taskId: string): TaskFacts {
    const def: LifecycleDef | undefined = lifecycleFor(lifecycleKeyFor(view.type, view.variant));
    if (def === undefined) return {};
    const task: LifecycleTaskDef | undefined = def.tasks.find((t) => t.id === taskId);
    if (task === undefined) return {};
    const phase = def.phases.find((p) => p.id === task.phase);
    const artifactKind = artifactKindOf(def, task);
    return {
      ...(task.workerClass !== undefined ? { workerClass: task.workerClass } : {}),
      ...(task.command !== undefined ? { command: task.command } : {}),
      ...(artifactKind !== undefined ? { artifactKind } : {}),
      ...(phase !== undefined ? { exitCriterion: phase.exitCriterion } : {}),
    };
  }

  function revisionOf(r: RevisionWire): LifecycleRevision {
    return {
      n: r.n,
      outcome: OUTCOME_TEXT[r.outcome],
      ...(r.commentCount > 0 ? { commentCount: r.commentCount } : {}),
      ...(r.decidedAt !== undefined ? { at: shortDate(r.decidedAt) } : {}),
    };
  }

  /** "2026-09-12T…" → "Sep 12". An unparseable stamp is shown verbatim, never as "Invalid Date". */
  export function shortDate(at: string): string {
    const d = new Date(at);
    if (Number.isNaN(d.getTime())) return at;
    return `${MONTHS[d.getUTCMonth()] ?? ''} ${String(d.getUTCDate())}`.trim();
  }

  const MONTHS = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'] as const;

  function nodeOf(view: ActivityViewWire, t: TaskWire, def: LifecycleDef | undefined): LifecycleNode {
    const gen = def?.tasks.find((d) => d.id === t.id);
    return {
      id: t.id,
      kind: t.kind,
      title: t.title,
      phase: t.phase,
      state: NODE_STATE[t.state],
      dependsOn: t.dependsOn,
      revisions: t.revisions.map(revisionOf),
      ...(gen?.revisionGroup !== undefined ? { revisionGroup: gen.revisionGroup } : {}),
    };
  }

  export interface ActivityGraph {
    nodes: LifecycleNode[];
    phases: LifecyclePhase[];
  }

  /**
   * The whole activity as the graph wants it. Task order is the wire's (the
   * server emits them in lifecycle order, and the first-authored chain is what
   * the layout treats as the trunk — lifecycleGraphLayout.ts).
   */
  export function activityViewToGraph(view: ActivityViewWire): ActivityGraph {
    const def = lifecycleFor(lifecycleKeyFor(view.type, view.variant));
    return {
      nodes: view.tasks.map((t) => nodeOf(view, t, def)),
      phases: view.phases.map((p) => ({
        id: p.id,
        label: p.label,
        weight: p.weight,
        passed: p.completed,
      })),
    };
  }
  ```

- [ ] **Step 2: Test it** — `activityViewToGraph.test.ts`. Build the input from the shipped fixture so the test is about real data, not an invented shape:

  ```ts
  /// <reference types="node" />
  import { test } from 'node:test';
  import assert from 'node:assert/strict';
  import { readFileSync } from 'node:fs';
  import {
    activityViewToGraph,
    artifactKindOf,
    lifecycleKeyFor,
    OUTCOME_TEXT,
    shortDate,
    taskFactsFor,
    type ActivityViewWire,
  } from './activityViewToGraph.ts';
  import { lifecycleFor } from './lifecycles.gen.ts';

  function fixture(name: string): ActivityViewWire {
    const url = new URL(`../../../preview/fixtures/web-client/activity-experience/${name}.json`, import.meta.url);
    const doc = JSON.parse(readFileSync(url, 'utf8')) as {
      ops: { constructionQueryActivityView: { result: ActivityViewWire } };
    };
    return doc.ops.constructionQueryActivityView.result;
  }

  void test('a passed task becomes a done node, and a completed phase becomes a passed phase', () => {
    const graph = activityViewToGraph(fixture('done'));
    assert.ok(graph.nodes.length > 0);
    assert.ok(graph.nodes.every((n) => n.state === 'done'), 'every task of the done fixture passed');
    assert.ok(graph.phases.every((p) => p.passed), 'every phase of the done fixture completed');
    assert.deepEqual(
      graph.phases.map((p) => p.weight),
      [20, 45, 35],
      'the weights ride through untouched'
    );
  });

  void test('the outcome enum becomes the sentence a reader reads, never the wire word', () => {
    assert.equal(OUTCOME_TEXT.awaitingHuman, 'awaiting you');
    assert.equal(OUTCOME_TEXT.sentBack, 'sent back');
    assert.equal(OUTCOME_TEXT.passed, 'approved');
  });

  void test('the dispatch facts are joined from the lifecycle table — the wire carries none of them', () => {
    const view = fixture('not-started');            // U-SPA-web-client, type: frontend
    const facts = taskFactsFor(view, 'srs');
    // The FRONTEND lifecycle's `srs` is "UX Requirements", run by the ui-designer
    // charter — not the service lifecycle's senior-developer SRS. The two share a
    // task id and nothing else, which is exactly why this join is a table lookup
    // keyed on (type, variant) and never on the task id alone.
    assert.equal(facts.workerClass, 'ui-designer');
    assert.equal(facts.command, 'frontend-requirements');
    assert.equal(facts.artifactKind, 'SRS');
    assert.ok((facts.exitCriterion ?? '').length > 0, 'its phase names a binary exit criterion');
    assert.deepEqual(taskFactsFor(view, 'noSuchTask'), {}, 'an unknown task joins nothing, it does not throw');
  });

  void test('a REVIEW task takes its artifact kind from the dispatch it judges', () => {
    // `architectureReview` carries `reviews: 'architectureDraft'` and no
    // artifactKind of its own; the draft carries `artifactKind: 'System'`.
    const view = { type: 'architecture', variant: undefined, tasks: [], phases: [] } as unknown as ActivityViewWire;
    assert.equal(taskFactsFor(view, 'architectureReview').artifactKind, 'System');
    assert.equal(taskFactsFor(view, 'architectureDraft').artifactKind, 'System');
  });

  void test('every review task in every lifecycle resolves an artifact kind', () => {
    // The regression this chain exists to prevent: reading `task.artifactKind`
    // alone returns undefined for all ten review tasks below, and the review
    // body's verb table keys on it.
    for (const [key, taskId, expected] of [
      ['requirements', 'missionReview', 'Mission'],
      ['requirements', 'glossaryReview', 'Glossary'],
      ['requirements', 'volatilitiesReview', 'Volatilities'],
      ['requirements', 'coreUseCasesReview', 'CoreUseCases'],
      ['service', 'srsReview', 'SRS'],
      ['service', 'designReview', 'DetailedDesign'],
      ['service', 'codeReview', 'Construction'],
      ['service', 'stpReview', 'STP'],
      ['service', 'testing', 'Integration'],
    ] as const) {
      const def = lifecycleFor(key);
      assert.ok(def !== undefined, key);
      const task = def.tasks.find((t) => t.id === taskId);
      assert.ok(task !== undefined, `${key}/${taskId}`);
      assert.equal(artifactKindOf(def, task), expected, `${key}/${taskId}`);
    }
  });

  void test('projectDesign’s sdpReview carries its own kind and judges no dispatch', () => {
    const def = lifecycleFor('projectDesign');
    assert.ok(def !== undefined);
    const task = def.tasks.find((t) => t.id === 'sdpReview');
    assert.ok(task !== undefined);
    assert.equal(task.reviews, undefined, 'the SDP is computed — there is nothing to judge');
    assert.equal(artifactKindOf(def, task), 'SdpReview');
  });

  void test('a testing activity keys on its variant; everything else keys on its type', () => {
    assert.equal(lifecycleKeyFor('testing', 'systemTest'), 'testing:systemTest');
    assert.equal(lifecycleKeyFor('testing', undefined), 'testing:plan');
    assert.equal(lifecycleKeyFor('architecture', undefined), 'architecture');
  });

  void test('an unparseable date is shown verbatim rather than as "Invalid Date"', () => {
    assert.equal(shortDate('2026-09-12T10:00:00Z'), 'Sep 12');
    assert.equal(shortDate('whenever'), 'whenever');
  });
  ```
  - [ ] **Verify first:** `ls webApp/preview/fixtures/web-client/activity-experience/` shows `done.json`, `not-started.json`, `deployment-linear.json`, `service-fork-sent-back.json`. Task 6 MOVES three of these; when it does, it updates this test's path to the surviving smoke fixture and to `../../../../uitests/preview-fixtures/...` for the rest. Note that dependency in Task 6 Step 5.

- [ ] **Step 3: Write `threadAdapter.ts`.** `CommentMargin` takes the design `ReviewCommentView`; the rail serves `ConstructionReviewThreadComment` — parallel shapes, not identical (GAP-21).

  ```ts
  /**
   * The stage-3 review round's thread, in the shape the comment margin already
   * speaks. `CommentMargin` / `MarginThreadCard` were built for the design
   * rail's `ReviewCommentView`; the unified rail serves
   * `ConstructionReviewThreadComment`, whose members are parallel but not
   * identical (`anchorText` and `addressee` are optional on the wire and
   * required in the view; the view carries a deprecated `response` nothing
   * reads). Adapting is a dozen lines; rewriting the margin is not.
   */
  import type { components } from '../../contracts/schema.ts';
  import type { ReviewCommentView, ReviewCommentReply } from '../../contracts/types.ts';

  type ThreadWire = components['schemas']['ConstructionReviewThreadComment'];
  type ReplyWire = components['schemas']['ConstructionReviewThreadReply'];

  function replyOf(r: ReplyWire): ReviewCommentReply {
    return { id: r.id, authorRole: r.authorRole, text: r.text, at: r.at };
  }

  /**
   * `staleAck` has no counterpart in the view's two-value `type` — it is an
   * audit entry, not a change request and not a question. It maps to
   * `changeRequest` (the wire's own migration-safe default) and the margin
   * renders it as an ordinary thread: dropping it would hide a recorded
   * acknowledgement from the history it belongs to.
   */
  export function toReviewCommentView(c: ThreadWire): ReviewCommentView {
    return {
      id: c.id,
      anchor: c.anchor,
      anchorText: c.anchorText ?? '',
      text: c.text,
      authorRole: c.authorRole,
      round: c.round,
      status: c.status,
      replies: c.replies.map(replyOf),
      reopened: c.reopened,
      type: c.type === 'question' ? 'question' : 'changeRequest',
      addressee: c.addressee === 'pm' || c.addressee === 'architect' ? c.addressee : '',
    };
  }

  export function toReviewThread(thread: readonly ThreadWire[] | undefined): ReviewCommentView[] {
    return (thread ?? []).map(toReviewCommentView);
  }

  /** Threads still owed an answer — what blocks approve (`submitVerb.openThreads`). */
  export function openThreadCount(thread: readonly ReviewCommentView[]): number {
    return thread.filter((c) => c.type === 'changeRequest' && c.status !== 'resolved').length;
  }
  ```

- [ ] **Step 4: Test it** — `threadAdapter.test.ts`:

  ```ts
  /// <reference types="node" />
  import { test } from 'node:test';
  import assert from 'node:assert/strict';
  import { openThreadCount, toReviewCommentView, toReviewThread } from './threadAdapter.ts';
  import type { components } from '../../contracts/schema.ts';
  import type { ReviewCommentView } from '../../contracts/types.ts';

  type ThreadWire = components['schemas']['ConstructionReviewThreadComment'];

  function wire(over: Partial<ThreadWire>): ThreadWire {
    return {
      id: 'c1',
      anchor: '$.mission.vision',
      authorRole: 'architect',
      text: 'tighten this',
      round: 1,
      reopened: false,
      replies: [],
      status: 'open',
      type: 'changeRequest',
      ...over,
    } as ThreadWire;
  }

  void test('a question keeps its addressee; a change request is given the empty one', () => {
    assert.equal(toReviewCommentView(wire({ type: 'question', addressee: 'pm' })).addressee, 'pm');
    assert.equal(toReviewCommentView(wire({ type: 'question', addressee: 'architect' })).addressee, 'architect');
    assert.equal(toReviewCommentView(wire({})).addressee, '', 'a change request is addressed to no one');
  });

  void test('an addressee the view has no member for falls back to the empty one, never crashes', () => {
    assert.equal(toReviewCommentView(wire({ type: 'question', addressee: 'someoneElse' as never })).addressee, '');
  });

  void test('a staleAck entry survives as a change-request thread rather than being dropped', () => {
    const view = toReviewCommentView(wire({ id: 'ack1', type: 'staleAck', text: 'reviewed — unaffected' }));
    assert.equal(view.type, 'changeRequest', 'the view has no staleAck member; the wire default is changeRequest');
    assert.equal(view.text, 'reviewed — unaffected', 'the audit entry is kept, not hidden from its own history');
  });

  void test('the optional wire members become the view’s required ones', () => {
    const view = toReviewCommentView(wire({ anchorText: undefined }));
    assert.equal(view.anchorText, '', 'absent anchorText is the empty snapshot, never undefined');
    assert.deepEqual(toReviewThread(undefined), [], 'an absent thread is an empty one');
  });

  void test('replies ride through in order, with every member carried', () => {
    const view = toReviewCommentView(
      wire({ replies: [{ id: 'r1', authorRole: 'pm', text: 'done', at: '2026-09-12T10:00:00Z' }] })
    );
    assert.deepEqual(view.replies, [{ id: 'r1', authorRole: 'pm', text: 'done', at: '2026-09-12T10:00:00Z' }]);
  });

  void test('openThreadCount counts unresolved change requests only — questions never block approve', () => {
    const thread = [
      { type: 'changeRequest', status: 'open' },
      { type: 'changeRequest', status: 'answered' },
      { type: 'changeRequest', status: 'resolved' },
      { type: 'question', status: 'open' },
    ] as unknown as ReviewCommentView[];
    assert.equal(openThreadCount(thread), 2);
  });
  ```
  - [ ] **Wire it where it is consumed.** `openThreadCount(toReviewThread(revision.thread))` is what the container passes as `SubmitBar`'s `openThreads` (Task 9 Step 5) — nothing else computes that number, so `submitVerb`'s "Resolve N threads to approve" and the margin's card count can never disagree.

- [ ] **Step 5: Write `defaultTask.ts`** (R9, spec §7.2).

  ```ts
  /**
   * Which task the Activity Experience opens on when the URL names none
   * (spec §7.2): what needs YOU first, then what broke, then what is moving,
   * then the last thing that finished, then the first task of the lifecycle.
   *
   * Promoted out of the prototype's `defaultNodeId`, where it was an inline
   * `find` chain over fixture data.
   */
  import type { LifecycleNode } from './lifecycleGraphTypes.ts';

  export function defaultTaskId(nodes: readonly LifecycleNode[]): string | undefined {
    if (nodes.length === 0) return undefined;
    const first = (state: LifecycleNode['state']): LifecycleNode | undefined =>
      nodes.find((n) => n.state === state);
    const lastPassed = [...nodes].reverse().find((n) => n.state === 'done');
    return (
      first('awaitingHuman')?.id ??
      first('failed')?.id ??
      first('running')?.id ??
      lastPassed?.id ??
      nodes[0]?.id
    );
  }

  /**
   * The task the URL actually selects: the one it names when the activity has
   * it, else the default. A stale deep link opens the activity rather than an
   * empty body.
   */
  export function selectedTaskId(
    nodes: readonly LifecycleNode[],
    requested: string | undefined
  ): string | undefined {
    if (requested !== undefined && nodes.some((n) => n.id === requested)) return requested;
    return defaultTaskId(nodes);
  }
  ```

- [ ] **Step 6: Test it** — `defaultTask.test.ts`, at least five cases: awaiting-human wins over failed and running; failed wins over running; running wins over a later passed; with nothing live it lands on the LAST passed (not the first); a pristine activity lands on `nodes[0]`; `selectedTaskId` falls back when the URL names a task the activity does not have; `defaultTaskId([])` is `undefined`.

- [ ] **Step 7: Write `activityCopy.ts`** — every sentence the Activity Experience says, verbatim. Bodies, not signatures: a copy module with no strings in it is a copy review nobody can do.

  ```ts
  /**
   * Every sentence the Activity Experience says, in one pure module with tests
   * (house convention — tasksLensCopy.ts, serviceContractCopy.ts, roleLine.ts,
   * submitVerb's describeConsequence). There is no i18n in this app, so a string
   * written inline in JSX is a string nobody reviews.
   */

  /** The chrome eyebrow above the title: `R-GITHUB · DEPLOYMENT`, `ACTIVITY 2 · ARCHITECTURE`. */
  export function eyebrowFor(input: {
    activityId: string;
    type: string;
    variant?: string | undefined;
    /** 1-based position in the committed activity list; the three design activities have one. */
    planIndex?: number | undefined;
  }): string {
    const kind =
      input.variant !== undefined && input.variant.length > 0
        ? `${input.type}:${input.variant}`
        : input.type;
    const left = input.planIndex !== undefined ? `ACTIVITY ${String(input.planIndex)}` : input.activityId;
    return `${left.toUpperCase()} · ${kind.toUpperCase()}`;
  }

  /** The read-only banner on a non-latest revision (R1). */
  export function historyBanner(revision: number, latest: number): string {
    return `Revision ${String(revision)} of ${String(latest)} — read-only`;
  }

  export const BACK_TO_LATEST = 'Back to latest';

  /**
   * The caption over the artifact panel while a non-latest revision is on screen
   * (R1): what is shown is the CURRENT artifact, not the one this revision
   * judged, because no op reads an artifact as of a ref. Saying so IS the
   * feature — a silently-current artifact under a history banner is a lie.
   */
  export const HISTORY_ARTIFACT_CAPTION =
    'Showing the current artifact. The version this revision judged is not readable yet — its comments and verdicts below are.';

  /** Why a task's artifact cannot be rendered (R17). Names what is missing and what remains. */
  export function artifactUnavailable(classification: string): string {
    return `No artifact view for a ${classification} activity yet. Its episodes and review history are below.`;
  }

  /** Why a construction thread offers no Resolve / Reopen / Ask (R2). */
  export const CONSTRUCTION_THREAD_READ_ONLY =
    'Construction review threads are recorded, but cannot be resolved, reopened or replied to from here yet.';

  /** The reviewers strip when the review engine refused to propose a roster. */
  export function reviewSetRefused(detail: string): string {
    return `The review engine could not propose reviewers: ${detail}`;
  }

  /**
   * The sub-attempt disclosure. §7.2 shows it only when a revision had MORE than
   * one attempt, so one attempt (and zero) says nothing at all rather than "1
   * attempt". An empty string is the honest render, not a throw: copy does not
   * police its caller, and the caller renders nothing for an empty line.
   */
  export function subAttemptsLine(count: number): string {
    if (count <= 1) return '';
    return `${String(count)} attempts before this revision reached the gate`;
  }

  /** What a 404 from QueryActivityView means: the committed plan has no such activity. */
  export const ACTIVITY_NOT_IN_PLAN =
    'This activity is not in the committed activity list, so there is nothing to show for it.';
  ```

- [ ] **Step 7b: Test it** — `activityCopy.test.ts`. A copy module's test IS the copy review, so it asserts the exact strings:

  ```ts
  /// <reference types="node" />
  import { test } from 'node:test';
  import assert from 'node:assert/strict';
  import {
    ACTIVITY_NOT_IN_PLAN,
    artifactUnavailable,
    CONSTRUCTION_THREAD_READ_ONLY,
    eyebrowFor,
    historyBanner,
    HISTORY_ARTIFACT_CAPTION,
    reviewSetRefused,
    subAttemptsLine,
  } from './activityCopy.ts';

  void test('the eyebrow names the activity and its type, or its plan position when it has one', () => {
    assert.equal(eyebrowFor({ activityId: 'R-github', type: 'deployment' }), 'R-GITHUB · DEPLOYMENT');
    assert.equal(
      eyebrowFor({ activityId: 'architecture', type: 'architecture', planIndex: 2 }),
      'ACTIVITY 2 · ARCHITECTURE'
    );
    assert.equal(eyebrowFor({ activityId: 'N-STP', type: 'testing', variant: 'plan' }), 'N-STP · TESTING:PLAN');
  });

  void test('the history banner counts from one and says read-only', () => {
    assert.equal(historyBanner(2, 3), 'Revision 2 of 3 — read-only');
  });

  void test('the sub-attempt line says nothing at one attempt or none', () => {
    assert.equal(subAttemptsLine(0), '');
    assert.equal(subAttemptsLine(1), '');
    assert.equal(subAttemptsLine(3), '3 attempts before this revision reached the gate');
  });

  void test('an unavailable artifact names what is missing and what is still there', () => {
    assert.equal(
      artifactUnavailable('deployment'),
      'No artifact view for a deployment activity yet. Its episodes and review history are below.'
    );
  });

  void test('the engine’s refusal is quoted, not paraphrased', () => {
    assert.equal(
      reviewSetRefused('unknown artifact kind "detailed_design"'),
      'The review engine could not propose reviewers: unknown artifact kind "detailed_design"'
    );
  });

  void test('the standing sentences say the thing they exist to say', () => {
    assert.match(HISTORY_ARTIFACT_CAPTION, /^Showing the current artifact\./);
    assert.match(HISTORY_ARTIFACT_CAPTION, /not readable yet/);
    assert.match(CONSTRUCTION_THREAD_READ_ONLY, /cannot be resolved, reopened or replied to/);
    assert.match(ACTIVITY_NOT_IN_PLAN, /committed activity list/);
  });
  ```

- [ ] **Step 8: Gate and commit.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check
  ```
  Commit: `feat(activity): wire→props adapters, thread adapter, default task and the experience's copy`.

---

### Task 3: The plan-side pure modules

Three rules the Plan screen needs, none of which the server answers: which build-order row an activity belongs to (GAP-4), what its mini lifecycle looks like without a per-activity read (GAP-7/R3), and what a hover lights (GAP-22/R6). Plus the `deployment` row's removal (R5).

**Files:**
- Add: `webApp/src/components/activity/planRowFor.ts`, `planRowFor.test.ts`.
- Add: `webApp/src/components/activity/miniLifecycleFromRow.ts`, `miniLifecycleFromRow.test.ts`.
- Add: `webApp/src/components/activity/planCopy.ts`, `planCopy.test.ts`.
- Modify: `webApp/src/components/activity/planGraphLayout.ts` (drop `'deployment'`; rewrite `planFocusFor`), `planGraphLayout.test.ts` (extend).

**Interfaces:**
- Consumes: **`ConstructionRow`** from `../../contracts/types.ts` (`:727`) — NOT the raw wire row — plus `PhaseRow` (`types.ts:719`), `Layer` (`types.ts:82`), `TestingVariantName` (`types.ts:678`), `ActivityBuildStatusRow` (`types.ts:686`); `lifecycleFor` from `./lifecycles.gen.ts`; `lifecycleKeyFor` from `./activityViewToGraph.ts` (Task 2); `PlanRow` from `./planGraphLayout.ts`; `LifecycleNode` / `LifecycleNodeState` from `./lifecycleGraphTypes.ts`.
- Produces: `planRowFor`, `PlanRowInput`, `miniLifecycleFromRow`, `MiniLifecycleInput`, `miniLifecycleLabel`, the rewritten `planFocusFor`, and `planCopy.ts`.

> **Wire note, verified — read this before writing a line.** The raw `GetProject` row (`components['schemas']['SystemDesignActivityConstructionStatus']`) is PascalCase with ORDINAL enums (`"Type": 0`, `"Phases": [{"Phase": …, "Completed": …}]` — confirmed in `uitests/preview-fixtures/web-client/construction/resting.json`). **`components/` never sees it.** `contracts/wire.ts:479 mapConstructionRow` decodes every row into `contracts/types.ts:727 ConstructionRow` — camelCase, app-string enums — and `useProject` returns them as `ProjectStateWithGit.constructionRows?: ConstructionRows` (`types.ts:1159`, `ConstructionRows = Record<string, ConstructionRow>` at `:887`). The members Task 3 needs are:
> ```ts
> kind?: 'service'|'frontend'|'testing'|'deployment'|'documentation'|'uiDesign'|'integration'
>      | 'requirements'|'architecture'|'projectDesign';   // absent ⇒ the server could not classify
> variant?: TestingVariantName;                           // 'plan'|'harness'|'perf'|'systemTest'|'qaProcess'
> status?: 'integrated'|'in-review'|'in-construction'|'failed';
> currentLifecyclePhase?: string;
> phases: PhaseRow[];                                     // { phase, weight, label, completed, completedAt? }
> layer?: Layer;                                          // 'resourceAccess'|'resource'|'engine'|'manager'|'utility'|'client'
> layerBand?: 'layered' | 'projectWide';
> classified: boolean;  hasBuildEvidence: boolean;  recorded: boolean;
> ```
> Do NOT re-derive the ordinal tables, and do NOT go looking in `contracts/projectAdapters.narrowProject` — that is a SLOT-ENVELOPE narrower (`projectAdapters.ts:51`) and has nothing to do with construction rows. The decoder is `wire.ts:479`, full stop.

- [ ] **Step 1: Write `planRowFor.ts`.**

  ```ts
  /**
   * Which BUILD-ORDER row of the plan graph an activity sits in (spec §7.3).
   *
   * The server cannot answer this. `LayerForActivity` gives a componentless
   * activity `("", "projectWide")`, so requirements, architecture,
   * projectDesign, N-STP and N-IT all arrive indistinguishable — front end,
   * side lane and system testing collapse into one bucket. The (type, variant)
   * pair is what tells them apart, and it is already on the row.
   *
   * The rows read TOP→DOWN in build order: what nothing depends on is built
   * first (resources), what depends on everything is built last (clients),
   * system testing closes. That is the exact REVERSE of the architecture
   * diagram's order (`activityGraphModel.LAYERED_ROWS` = client-first) — this
   * is a project network, not an architecture, and reusing that list would
   * draw the plan upside down.
   */
  import type { ConstructionRow, Layer } from '../../contracts/types.ts';
  import type { PlanRow } from './planGraphLayout.ts';

  /** The Method layers that map 1:1 onto a build-order row. `utility` derives no activity. */
  const LAYER_ROW: Readonly<Partial<Record<Layer, PlanRow>>> = {
    resource: 'resource',
    resourceAccess: 'resourceAccess',
    engine: 'engine',
    manager: 'manager',
    client: 'client',
  };

  /** Exactly the three members of `ConstructionRow` this rule reads — nothing else. */
  export type PlanRowInput = Pick<ConstructionRow, 'kind' | 'variant' | 'layer'>;

  /**
   * `undefined` means "this activity has no row" — it is LISTED in the LIST
   * lens (the committed activity list decides what exists) but not DRAWN in the
   * GRAPH, under planCopy.UNPLACED_TILE_REASON. Inventing a row for it would
   * put a tile somewhere the model never said. An UNCLASSIFIED row (`kind`
   * absent) lands here too, and for the same reason: the server refused to
   * guess its type, so neither may this.
   */
  export function planRowFor(input: PlanRowInput): PlanRow | undefined {
    if (input.kind === 'requirements' || input.kind === 'architecture' || input.kind === 'projectDesign') {
      return 'frontEnd';
    }
    if (input.kind === 'testing') {
      if (input.variant === 'plan') return 'sideLane';
      if (input.variant === 'systemTest') return 'systemTesting';
      return undefined;                       // harness / perf / qaProcess have no band
    }
    return input.layer === undefined ? undefined : LAYER_ROW[input.layer];
  }
  ```

- [ ] **Step 2: Test it** — `planRowFor.test.ts`, at least these, written against the live plan's own 32 activities (§7.4 of the recon, re-verified: `slots.9.model.activities` has 32 entries beginning `requirements`, `architecture`, `projectDesign`):

  ```ts
  void test('the three design activities are the front end, whatever layer the server reports', () => {
    for (const kind of ['requirements', 'architecture', 'projectDesign'] as const) {
      assert.equal(planRowFor({ kind }), 'frontEnd');
    }
  });

  void test('N-STP is the side lane and N-IT is system testing — the server calls both projectWide', () => {
    assert.equal(planRowFor({ kind: 'testing', variant: 'plan' }), 'sideLane');
    assert.equal(planRowFor({ kind: 'testing', variant: 'systemTest' }), 'systemTesting');
  });

  void test('a coding activity takes its component layer', () => {
    assert.equal(planRowFor({ kind: 'service', layer: 'resourceAccess' }), 'resourceAccess');
    assert.equal(planRowFor({ kind: 'frontend', layer: 'client' }), 'client');
    assert.equal(planRowFor({ kind: 'service', layer: 'resource' }), 'resource');
    assert.equal(planRowFor({ kind: 'service', layer: 'engine' }), 'engine');
    assert.equal(planRowFor({ kind: 'service', layer: 'manager' }), 'manager');
  });

  void test('an activity with no row is undefined, not guessed into one', () => {
    assert.equal(planRowFor({ kind: 'documentation' }), undefined, 'no component layer, no band');
    assert.equal(planRowFor({ kind: 'service', layer: 'utility' }), undefined, 'utilities derive no activity');
    assert.equal(planRowFor({ kind: 'testing', variant: 'harness' }), undefined);
    assert.equal(planRowFor({ kind: 'testing', variant: 'perf' }), undefined);
    assert.equal(planRowFor({ kind: 'testing', variant: 'qaProcess' }), undefined);
  });

  void test('an UNCLASSIFIED row is undefined — the server refused to guess and so does this', () => {
    assert.equal(planRowFor({}), undefined);
    assert.equal(planRowFor({ layer: 'engine' }), 'engine', 'a classified layer still places it');
  });

  void test('the live plan places 29 of its 32 activities and says which three it cannot', () => {
    // The repo's own committed list (slot 9): requirements/architecture/
    // projectDesign → frontEnd, 4 R-* → resource, 10 C-*-access → resourceAccess,
    // 7 C-*-engine → engine, 5 C-*-manager → manager, U-SPA-web-client → client,
    // N-STP → sideLane, N-IT → systemTesting. That is all 32, with nothing unplaced —
    // pinned here so a model change that orphans a tile fails a test, not a screen.
    const live: PlanRowInput[] = [
      { kind: 'requirements' }, { kind: 'architecture' }, { kind: 'projectDesign' },
      { kind: 'service', layer: 'resource' }, { kind: 'service', layer: 'resourceAccess' },
      { kind: 'service', layer: 'engine' }, { kind: 'service', layer: 'manager' },
      { kind: 'frontend', layer: 'client' },
      { kind: 'testing', variant: 'plan' }, { kind: 'testing', variant: 'systemTest' },
    ];
    assert.ok(live.every((r) => planRowFor(r) !== undefined), 'every live shape places');
  });
  ```

- [ ] **Step 3: Write `miniLifecycleFromRow.ts` (R3).** The plan draws 32 mini lifecycles; `QueryActivityView` is per-activity, so 32 reads each on their own 2 s/8 s cadence is a stampede. Derive instead — exactly what `laneSpine.ts` does today from the list lens's tree.

  ```ts
  /**
   * The plan row's mini lifecycle, derived from ONE project read.
   *
   * `QueryActivityView` answers for one activity; a plan screen showing 32 of
   * them would make 32 HTTP reads, each on its own 2s/8s poll. The project read
   * already carries, per activity, the stored lifecycle-phase completions, the
   * coarse phase, the build status and the attempt ledger — which is exactly
   * what `components/construction/graph/laneSpine.ts` derives its spine from
   * today (and has done since the graph lens shipped). The batched read
   * (`QueryProjectView`) is stage 4's op, not stage 5's.
   *
   * The nodes this produces carry STATES ONLY. `revisions` is deliberately
   * empty: a revision is per-task history the project read does not carry, and
   * a fabricated one would let a mini graph claim a round that never happened.
   * Opening the activity is what fetches real revisions.
   */
  import { lifecycleFor } from './lifecycles.gen.ts';
  import { lifecycleKeyFor } from './activityViewToGraph.ts';
  import type { ConstructionRow } from '../../contracts/types.ts';
  import type { LifecycleNode, LifecycleNodeState } from './lifecycleGraphTypes.ts';

  /**
   * Exactly the five members of `ConstructionRow` this rule reads. Taking a
   * `Pick` rather than the whole row keeps the test fixtures four lines long and
   * makes it impossible for this module to start depending on evidence flags it
   * has no business branching on.
   */
  export type MiniLifecycleInput = Pick<
    ConstructionRow,
    'kind' | 'variant' | 'phases' | 'currentLifecyclePhase' | 'status'
  >;

  const COMPLETE: LifecycleNodeState = 'done';

  /**
   * A phase's tasks all take that phase's state; the graph's job here is to say
   * how far the activity has got, not which task inside a phase is moving —
   * the project read cannot answer that, and pretending otherwise is the lie
   * this module is shaped to avoid.
   */
  export function miniLifecycleFromRow(input: MiniLifecycleInput): LifecycleNode[] {
    // An UNCLASSIFIED row has no `kind`, so it has no lifecycle and gets no
    // spine at all (§9 AC4). A default profile would launder "we do not know
    // what this is" into a plausible-but-wrong lifecycle.
    if (input.kind === undefined) return [];
    const def = lifecycleFor(lifecycleKeyFor(input.kind, input.variant));
    if (def === undefined) return [];
    const completed = new Set(input.phases.filter((p) => p.completed).map((p) => p.phase));
    return def.tasks.map((t) => ({
      id: t.id,
      kind: t.kind,
      title: t.title,
      phase: t.phase,
      state: stateFor(t.phase, completed, input),
      dependsOn: t.dependsOn,
      revisions: [],
      ...(t.revisionGroup !== undefined ? { revisionGroup: t.revisionGroup } : {}),
    }));
  }

  function stateFor(
    phase: string,
    completed: ReadonlySet<string>,
    input: MiniLifecycleInput
  ): LifecycleNodeState {
    if (completed.has(phase)) return COMPLETE;
    if (phase !== input.currentLifecyclePhase) return 'pending';
    // `status` is the row's coarse build state (ConstructionRow.status —
    // 'integrated' | 'in-review' | 'in-construction' | 'failed'). 'in-review' IS
    // the awaiting-a-human state on the plan screen: the pump has suspended at a
    // gate. Absent status (unclassified, or no build evidence) reads as running
    // only because the phase is the current one — nothing else claims progress.
    if (input.status === 'failed') return 'failed';
    if (input.status === 'in-review') return 'awaitingHuman';
    return 'running';
  }

  /** The mini graph's accessible name — a count, never a colour (WCAG 1.4.1). */
  export function miniLifecycleLabel(nodes: readonly LifecycleNode[]): string {
    const done = nodes.filter((n) => n.state === 'done').length;
    return `Lifecycle: ${String(done)} of ${String(nodes.length)} tasks done`;
  }
  ```

- [ ] **Step 4: Test it** — `miniLifecycleFromRow.test.ts`:

  ```ts
  /// <reference types="node" />
  import { test } from 'node:test';
  import assert from 'node:assert/strict';
  import { miniLifecycleFromRow, miniLifecycleLabel, type MiniLifecycleInput } from './miniLifecycleFromRow.ts';
  import type { PhaseRow } from '../../contracts/types.ts';

  const SERVICE_PHASES = ['requirements', 'detailed_design', 'test_plan', 'construction', 'integration'];
  const phases = (done: readonly string[]): PhaseRow[] =>
    SERVICE_PHASES.map((phase) => ({ phase, weight: 20, label: phase, completed: done.includes(phase) }));

  const row = (over: Partial<MiniLifecycleInput>): MiniLifecycleInput => ({
    kind: 'service',
    phases: phases([]),
    ...over,
  });

  void test('a finished service activity is all done, and its label counts its ten tasks', () => {
    const nodes = miniLifecycleFromRow(row({ phases: phases(SERVICE_PHASES), status: 'integrated' }));
    assert.equal(nodes.length, 10, 'the service lifecycle has ten tasks');
    assert.ok(nodes.every((n) => n.state === 'done'));
    assert.equal(miniLifecycleLabel(nodes), 'Lifecycle: 10 of 10 tasks done');
  });

  void test('the current phase runs, everything before it is done, everything after is pending', () => {
    const nodes = miniLifecycleFromRow(
      row({
        phases: phases(['requirements', 'detailed_design', 'test_plan']),
        currentLifecyclePhase: 'construction',
        status: 'in-construction',
      })
    );
    const state = (id: string): string => nodes.find((n) => n.id === id)?.state ?? 'MISSING';
    assert.equal(state('srs'), 'done');
    assert.equal(state('designReview'), 'done');
    assert.equal(state('construction'), 'running');
    assert.equal(state('codeReview'), 'running');
    assert.equal(state('integration'), 'pending');
    assert.equal(state('testing'), 'pending');
  });

  void test('in-review is the awaiting-a-human state; failed is failed; both leave finished phases done', () => {
    const at = (status: MiniLifecycleInput['status']): string =>
      miniLifecycleFromRow(
        row({ phases: phases(['requirements']), currentLifecyclePhase: 'detailed_design', status })
      ).find((n) => n.id === 'designReview')?.state ?? 'MISSING';
    assert.equal(at('in-review'), 'awaitingHuman');
    assert.equal(at('failed'), 'failed');
    assert.equal(at('in-construction'), 'running');
    const done = miniLifecycleFromRow(
      row({ phases: phases(['requirements']), currentLifecyclePhase: 'detailed_design', status: 'failed' })
    ).find((n) => n.id === 'srs');
    assert.equal(done?.state, 'done', 'a failure does not un-finish what finished');
  });

  void test('a testing row takes its VARIANT lifecycle, not the bare type', () => {
    assert.equal(miniLifecycleFromRow(row({ kind: 'testing', variant: 'plan' })).length, 6);
    assert.equal(miniLifecycleFromRow(row({ kind: 'testing', variant: 'qaProcess' })).length, 4);
  });

  void test('an unclassified row gets no spine at all rather than a guessed one', () => {
    assert.deepEqual(miniLifecycleFromRow(row({ kind: undefined })), []);
  });

  void test('EVERY produced node carries zero revisions — the invariant this module exists to hold', () => {
    // The project read carries no per-task revision history. A fabricated
    // revision would let a mini graph claim a round that never happened;
    // opening the activity is what fetches real ones (QueryActivityView).
    for (const kind of ['service', 'frontend', 'deployment', 'documentation', 'uiDesign', 'integration'] as const) {
      for (const n of miniLifecycleFromRow(row({ kind, phases: phases(SERVICE_PHASES) }))) {
        assert.equal(n.revisions.length, 0, `${kind}/${n.id}`);
      }
    }
  });
  ```

- [ ] **Step 5: Drop the `deployment` row (R5) — from the module AND from the landed test.** The test file Task 1 committed asserts the eight-row world; leaving it alone makes this step red. Both edits are this step.

  **In `planGraphLayout.ts`:** remove `'deployment'` from the `PlanRow` union (the `:29` block), from `ROW_LABEL` (the `:80` table) and from `STACK_ROWS` (the `:93` list). Add, above the union:
  ```ts
  /**
   * There is no `deployment` row. The prototype had one and the live model has
   * no deployment LAYER (slots.5 components are resourceAccess / resource /
   * engine / manager / utility / client only) — the tile the proto drew in it
   * was invented. A deployment-type activity takes its component's layer like
   * any other coding activity (planRowFor).
   */
  ```

  **In `planGraphLayout.test.ts`, five edits, every one of which fails without it:**
  1. `:33` — delete the tile `{ id: 'N-DEP', row: 'deployment', calls: ['M-a', 'M-b'] },`.
  2. `:46-60` — in `'rows run front end → the build-order stack → system testing, one pitch apart'`, delete `'deployment',` from the expected row list and change `assert.equal(L.height, 7 * PLAN_ROW_H + PLAN_TILE.h)` to `6 * PLAN_ROW_H + PLAN_TILE.h` (one fewer band).
  3. `:92-94` — in `'dependency edges point DOWN in build order: predecessor → dependent'`, replace the deployment pair with the client pair alone:
     ```ts
     // Managers → their clients, never the reverse.
     assert.ok(ids.has('M-a>U-web') && ids.has('M-b>U-web'));
     assert.ok(!ids.has('U-web>M-a') && !ids.has('U-web>M-b'));
     ```
  4. `:128` and `:137` — the two focus tests, rewritten in Step 7 below (they also name `N-DEP`).
  5. Re-read the file's header comment and drop `deployment` from its list of what it pins, if it names it.

  Then run the file alone before moving on:
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts node --test 'src/components/activity/planGraphLayout.test.ts'
  ```
  Expected: green, with one fewer row and no `N-DEP`.

- [ ] **Step 6: Rewrite `planFocusFor` as a transitive closure (R6).** Replace the body at `planGraphLayout.ts:217` with:

  ```ts
  /**
   * What a hover lights: the tile, every activity it TRANSITIVELY depends on,
   * and every activity that transitively depends on it — the full upstream and
   * downstream chain (spec §7.3), not the direct neighbours the prototype and
   * the old graph lens both lit. On a build plan the direct neighbours are the
   * least interesting answer: what a reader wants from hovering C-review-engine
   * is everything that must exist before it and everything that cannot ship
   * without it.
   *
   * Hovering the MILESTONE lights everything it gates — all of construction,
   * the side lane and system testing — because that is what a forced dependency
   * means, and M0's own edges reach only the build roots.
   */
  export function planFocusFor(
    hoveredId: string,
    tiles: readonly PlanTileInput[],
    edges: readonly PlanEdge[],
    milestoneId: string | undefined
  ): PlanFocus {
    if (hoveredId === milestoneId) {
      const lit = new Set<string>([hoveredId]);
      for (const t of tiles) {
        if (t.row !== 'frontEnd') lit.add(t.id);
      }
      return { tiles: lit, incident: (e) => lit.has(e.from) && lit.has(e.to) };
    }
    const up = new Map<string, string[]>();
    const down = new Map<string, string[]>();
    for (const e of edges) {
      push(down, e.from, e.to);
      push(up, e.to, e.from);
    }
    const lit = new Set<string>([hoveredId]);
    walk(up, hoveredId, lit);
    walk(down, hoveredId, lit);
    return { tiles: lit, incident: (e) => lit.has(e.from) && lit.has(e.to) };
  }

  function push(m: Map<string, string[]>, key: string, value: string): void {
    const cur = m.get(key);
    if (cur === undefined) m.set(key, [value]);
    else cur.push(value);
  }

  /** Iterative, with a visited set: a cycle in the data must dim the graph, not hang it. */
  function walk(adj: ReadonlyMap<string, readonly string[]>, from: string, lit: Set<string>): void {
    const stack = [from];
    while (stack.length > 0) {
      const id = stack.pop();
      if (id === undefined) continue;
      for (const next of adj.get(id) ?? []) {
        if (lit.has(next)) continue;
        lit.add(next);
        stack.push(next);
      }
    }
  }
  ```

- [ ] **Step 7: REWRITE the two focus tests, then add four more.** The landed file carries two assertions the transitive rule contradicts, and appending beside them leaves the suite red. Both must be REPLACED, not supplemented:

  - **`:128` `'hover lights the tile, its neighbours and only the edges between them'`** asserts `planFocusFor('M-a', …).tiles` is exactly `['E-x','M-a','N-DEP','R-p','U-web']` — direct neighbours only — and that `incident` means `e.from === hovered || e.to === hovered`. Under the transitive rule `M-a` also lights `X-db` (through `R-p`) and `N-IT` (through `U-web`), and `incident` now means `lit.has(e.from) && lit.has(e.to)`. **Delete this test and put the first of the four below in its place.**
  - **`:137` `'hovering the milestone lights everything it gates, not just the roots'`** asserts that the front-end tile `'3'` IS lit (as M0's direct predecessor) while `'1'` and `'2'` stay dim. The new M0 branch lights the milestone plus every **non-`frontEnd`** tile and nothing else, so `'3'` moves from the lit list to the dim one. **Rewrite its two loops accordingly** — keep its name and its intent, change what it expects.

  Replace them with these, all against small local fixtures so a change to the shared `TILES` cannot silently move the answer:

  ```ts
  void test('hovering the middle of a 3-chain lights the whole chain, not its neighbours', () => {
    const tiles = [
      { id: 'A', row: 'resource' as const, calls: [] },
      { id: 'B', row: 'resourceAccess' as const, calls: ['A'] },
      { id: 'C', row: 'engine' as const, calls: ['B'] },
      { id: 'D', row: 'manager' as const, calls: ['C'] },
    ];
    const { edges } = layoutPlanGraph(tiles, undefined);
    const focus = planFocusFor('C', tiles, edges, undefined);
    assert.deepEqual([...focus.tiles].sort(), ['A', 'B', 'C', 'D']);
  });

  void test('a fork lights both downstream branches and the shared upstream', () => {
    const tiles = [
      { id: 'root', row: 'resource' as const, calls: [] },
      { id: 'left', row: 'engine' as const, calls: ['root'] },
      { id: 'right', row: 'engine' as const, calls: ['root'] },
      { id: 'other', row: 'engine' as const, calls: [] },
    ];
    const { edges } = layoutPlanGraph(tiles, undefined);
    const focus = planFocusFor('root', tiles, edges, undefined);
    assert.deepEqual([...focus.tiles].sort(), ['left', 'right', 'root']);
    assert.ok(!focus.tiles.has('other'), 'an unrelated tile stays dimmed');
  });

  void test('hovering the milestone lights everything it gates, not just the roots', () => {
    // The REWRITE of the landed :137 test. M0 lights itself and every
    // non-frontEnd tile — the side lane and system testing included, which its
    // own edges never reach — because that is what a forced dependency means.
    // The front-end chain is NOT lit: M0 does not gate what precedes it, and '3'
    // (its direct predecessor) was lit before only as a plain neighbour.
    const f = planFocusFor('M0', TILES, L.edges, 'M0');
    for (const id of ['M0', 'X-db', 'R-lonely', 'N-STP', 'N-IT', 'U-web']) {
      assert.ok(f.tiles.has(id), `${id} should be lit`);
    }
    for (const id of ['1', '2', '3']) {
      assert.ok(!f.tiles.has(id), `${id} is front end and stays dim`);
    }
  });

  void test('incident now means BOTH ends are lit, not "touches the hovered tile"', () => {
    const tiles = [
      { id: 'A', row: 'resource' as const, calls: [] },
      { id: 'B', row: 'engine' as const, calls: ['A'] },
      { id: 'C', row: 'manager' as const, calls: ['B'] },
      { id: 'Z', row: 'engine' as const, calls: [] },
    ];
    const { edges } = layoutPlanGraph(tiles, undefined);
    const f = planFocusFor('B', tiles, edges, undefined);
    const drawn = edges.filter((e) => f.incident(e)).map((e) => e.id).sort();
    assert.deepEqual(drawn, ['A>B', 'B>C'].sort(), 'the whole lit chain stays drawn, not only B’s own edges');
  });

  void test('a cycle in the dependency data dims the graph rather than hanging it', () => {
    const tiles = [
      { id: 'X', row: 'engine' as const, calls: ['Y'] },
      { id: 'Y', row: 'engine' as const, calls: ['X'] },
    ];
    const { edges } = layoutPlanGraph(tiles, undefined);
    const focus = planFocusFor('X', tiles, edges, undefined);
    assert.deepEqual([...focus.tiles].sort(), ['X', 'Y']);
  });
  ```

- [ ] **Step 8: Write `planCopy.ts` + its test.** Bodies, not signatures:

  ```ts
  /** Every sentence the Plan screen says. Pure, tested, no i18n (house convention). */

  export const LENS_LABEL: Readonly<Record<'list' | 'graph' | 'tasks', string>> = {
    list: 'List',
    graph: 'Graph',
    tasks: 'Tasks',
  };

  /** The divider between the three design activities and the build stack. */
  export const M0_DIVIDER = 'M0 · SDP Review approved — construction begins';

  /**
   * Why an activity is in the list but not on the canvas. The committed activity
   * list decides what EXISTS; the model decides where a tile can be DRAWN. When
   * the two disagree the screen says so rather than dropping a row on the floor.
   */
  export function unplacedTilesNote(ids: readonly string[]): string {
    const names = [...ids].sort((a, b) => a.localeCompare(b)).join(', ');
    return `Not drawn: ${names}. These activities build no layered component, so the plan has no row for them — they are listed above.`;
  }

  /** The list/graph when the project has no committed plan yet. */
  export function planEmptyState(reason: 'noPlan' | 'noRows'): string {
    return reason === 'noPlan'
      ? 'No activity list is committed yet. Approve the Project Design activity (M0) and the plan appears here.'
      : 'The committed activity list is empty.';
  }

  /** The always-visible numeral beside the critical-path rail (WCAG 1.4.1 — never colour alone). */
  export function criticalPathNote(count: number): string {
    return count === 1
      ? '1 activity on the critical path'
      : `${String(count)} activities on the critical path`;
  }
  ```
  Test (`planCopy.test.ts`) the exact strings, including: `unplacedTilesNote(['C-b','C-a'])` sorts to `'Not drawn: C-a, C-b. …'`; `criticalPathNote(1)` is singular and `criticalPathNote(15)` is plural (15 is the live count); `M0_DIVIDER` names M0 and says construction begins; both `planEmptyState` branches.

- [ ] **Step 9: Gate and commit.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check
  ```
  Commit: `feat(activity): build-order rows, derived mini lifecycles and a transitive plan focus`.

---

### Task 4: `resolveSubmitVerb` learns `allowSendBack` and `approveCopy`

Project Design is ONE deterministic review with no send-back (spec R7/§6); the verb bar has no way to say that today, which is why the prototype hand-rolled its own `ApproveOnlyBar`. Two optional inputs fix it without touching any existing call site's behaviour.

**Files:**
- Modify: `webApp/src/components/design/submitVerb.ts` (146 L), `webApp/src/components/design/submitVerb.test.ts` (194 L), `webApp/src/components/design/SubmitBar.tsx` (286 L).

**Interfaces:**
- Changed (additive; every existing caller compiles unchanged):
  ```ts
  export function resolveSubmitVerb(input: {
    committed: boolean;
    stage: 'drafted' | 'awaitingReview' | 'other';
    stagedChangeRequests: number;
    stagedQuestions: number;
    openThreads: number;
    allowEmptySendBack?: boolean;
    /**
     * False on a surface where sending back is not a verb at all (spec R7: the
     * Project Design M0 gate — to change the plan you amend the Architecture).
     * Staged change requests then STAY comments: they ride the approval as
     * recorded feedback rather than flipping the primary verb to a redraft that
     * has nowhere to go. Default true — every existing caller is unchanged.
     */
    allowSendBack?: boolean;
    /** Overrides the approve verb's wording where the consequence is bigger than "commits and advances". */
    approveCopy?: { label: string; consequence: string };
  }): SubmitVerb;
  ```
- `SubmitBar` gains the same two optional props and threads them straight through.

- [ ] **Step 1: Write the failing tests** — append to `submitVerb.test.ts`:

  ```ts
  void test('allowSendBack:false keeps approve primary with change requests staged', () => {
    const verb = resolveSubmitVerb({
      committed: false, stage: 'awaitingReview',
      stagedChangeRequests: 3, stagedQuestions: 0, openThreads: 0,
      allowSendBack: false,
    });
    assert.equal(verb.action, 'approve');
    assert.equal(verb.disabled, false);
    assert.deepEqual(verb.secondaryActions, [], 'send back is not offered anywhere, not even in the overflow');
  });

  void test('allowSendBack:false still routes a questions-only batch to ask', () => {
    const verb = resolveSubmitVerb({
      committed: false, stage: 'awaitingReview',
      stagedChangeRequests: 0, stagedQuestions: 2, openThreads: 0,
      allowSendBack: false,
    });
    assert.equal(verb.action, 'ask', 'asking is not sending back — the M0 gate still takes questions');
  });

  void test('approveCopy replaces the label and consequence of the approve verb only', () => {
    const verb = resolveSubmitVerb({
      committed: false, stage: 'awaitingReview',
      stagedChangeRequests: 0, stagedQuestions: 0, openThreads: 0,
      allowSendBack: false,
      approveCopy: { label: 'Approve plan & cost — start construction', consequence: 'Commits the SDP and releases construction' },
    });
    assert.equal(verb.label, 'Approve plan & cost — start construction');
    assert.equal(verb.consequence, 'Commits the SDP and releases construction');
  });

  void test('approveCopy does not leak into the blocked-approve variant', () => {
    const verb = resolveSubmitVerb({
      committed: false, stage: 'awaitingReview',
      stagedChangeRequests: 0, stagedQuestions: 0, openThreads: 2,
      allowSendBack: false,
      approveCopy: { label: 'Approve plan & cost — start construction', consequence: 'x' },
    });
    assert.equal(verb.disabled, true);
    assert.equal(verb.label, 'Resolve 2 threads to approve', 'the blocked verb says what blocks it, not what it would do');
  });

  void test('the default is unchanged: allowSendBack omitted still sends back on staged change requests', () => {
    const verb = resolveSubmitVerb({
      committed: false, stage: 'awaitingReview',
      stagedChangeRequests: 1, stagedQuestions: 0, openThreads: 0,
    });
    assert.equal(verb.action, 'sendBack');
  });
  ```
  Run them; the first, third and fourth must FAIL (the input is not read yet) and the last two PASS.

- [ ] **Step 2: Implement.** In `resolveSubmitVerb`, destructure `allowSendBack = true` and `approveCopy` alongside `allowEmptySendBack = false`. Then:
  - The `staged > 0 && crs === 0` (ask) branch is untouched.
  - Guard the `staged > 0` branch: `if (staged > 0 && allowSendBack) { … today's amend/sendBack … }`. With `allowSendBack === false` control falls through to the approve family, which is precisely the ruling ("staged change-requests stay comments and the primary verb stays approve").
  - `const secondaryActions: SubmitAction[] = allowEmptySendBack && liveDraft && allowSendBack ? ['sendBack'] : [];`
  - In the final (enabled approve) return only: `label: approveCopy?.label ?? 'Approve'`, `consequence: approveCopy?.consequence ?? 'Commits the artifact and advances'`. Leave the `openThreads > 0` return exactly as it is.
  - Add the rationale comment above the guarded branch, in the file's ruling idiom:
    ```ts
    // RULING R11 (spec §6/R7): on a surface with no send-back — today only the
    // Project Design M0 gate — staged change requests are not a redraft trigger,
    // because there is nothing to redraft: the plan is computed, and changing it
    // means amending the Architecture. They ride the approval as recorded
    // feedback and the primary verb stays Approve.
    ```

- [ ] **Step 3: Thread both through `SubmitBar.tsx`.** Add to its props (with docs) at the `export function SubmitBar({` signature (`SubmitBar.tsx:109`), pass them into the `resolveSubmitVerb` call at **`SubmitBar.tsx:130`**, and — when `allowSendBack === false` — render NO send-back affordance anywhere, including the overflow menu. Do not add the `Amend Architecture →` link here; that is the M0 body's (Task 9), because it is a navigation, not a verb.

- [ ] **Step 4: Re-run and gate.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts node --test 'src/components/design/submitVerb.test.ts' && ASDF_NODEJS_VERSION=lts npm run check
  ```
  Commit: `feat(design): submit verb learns allowSendBack and an approve-copy override`.

---

### Task 5: §7.5 gaps 1–4

Four small defects the prototype surfaced. Each is its own step with its own evidence; none is a rewrite.

**Files:**
- Modify: `webApp/src/components/comments/CommentableList.tsx` (gap 1, the `!enabled` branch at `:251-261`).
- Modify: `webApp/src/components/construction/ContractSignatureList.tsx` (gap 2, the `ops.map` at `:144`).
- Modify: `webApp/src/components/design/GeneratingScene.tsx` (gap 3, props + the footer at `:207-210`).
- Modify: `webApp/src/components/design/MarginThreadCard.tsx` (gap 4, the collapse at `:88-90`) and `webApp/src/components/design/CommentMargin.tsx` (thread the prop through at `:316`).

**Interfaces produced:**
```ts
// GeneratingScene — two new optional props
roleLine?: { seed: string; text: string } | undefined;   // overrides roleLineFor; `seed` is a RoleAvatar seed
footerNote?: ReactNode | undefined;                      // replaces the GitHub-Actions sentence
// MarginThreadCard / CommentMargin — one new optional prop each
expandResolved?: boolean | undefined;                    // default false = today's collapse
```

- [ ] **Step 1: Gap 1 — `CommentableList`'s read-only rows register their anchors.** Hooks cannot be called inside the `.map` callback, so this needs a child component exactly as `CommentableRow` already is (`CommentableList.tsx:50`, whose own doc says so). Add above `CommentableList`:

  ```tsx
  /**
   * A read-only row. Separate from the map for the reason `CommentableRow` is:
   * it calls {@link useRegisterAnchor}, a hook, which React forbids in a loop
   * callback.
   *
   * The disabled branch used to render a bare Box with no anchor enrolment at
   * all, so on a read-only surface — a non-latest revision's history, exactly
   * what spec §7.2 asks for — EVERY margin card fell into the unplaced bucket.
   * Enrolment is about where a row IS, not about whether it can be commented
   * on; the two were conflated. Everything else about the inert branch is
   * unchanged: no list/listitem roles, no roving tabindex, no comment button,
   * no hover chrome.
   */
  function ReadOnlyRow({
    jsonPath,
    children,
  }: {
    jsonPath: string;
    children: ReactNode;
  }): ReactNode {
    const registerAnchor = useRegisterAnchor(jsonPath);
    return (
      <Box ref={registerAnchor} sx={{ px: 1, py: 0.75 }}>
        {children}
      </Box>
    );
  }
  ```
  and replace the `!enabled` body's map with:
  ```tsx
        {items.map((item, index) => (
          <ReadOnlyRow jsonPath={getAnchor(item, index).jsonPath} key={getKey(item, index)}>
            {renderItem(item, index)}
          </ReadOnlyRow>
        ))}
  ```
  This is non-DOM-untestable under `node --test` (it is `.tsx`); its proof is the Playwright assertion in Task 12 ("a margin card is PLACED in a read-only history"). Say so in a one-line comment on the branch.

- [ ] **Step 2: Gap 2 — `ContractSignatureList` registers `contractOpAnchor`.** Same hooks-in-a-loop constraint. Extract an `OpRow` child taking `{ component, signature, children }`, calling `useRegisterAnchor(contractOpAnchor(component, signature))`, and putting the returned callback on the `<Box component="li">`'s `ref` at `:147`. `component` and `op.signature` are both already in scope there (verified). Keep `data-testid={UI_IDENTIFIERS.ServiceContract.opRow(i)}` on the inner button exactly where it is. Proof: the Task 12 assertion "a contract thread is placed, not unplaced".

- [ ] **Step 3: Gap 3 — `GeneratingScene` stops being design-rail-only.** Add the two props with docs; compute
  ```tsx
  const line = roleLine ?? (activeRole !== undefined && phrase !== undefined
    ? roleLineFor(activeRole, activeStep ?? 'none', round ?? 0, phrase)
    : undefined);
  ```
  and render `footerNote ?? <the existing GitHub-Actions Typography>` in the footer, keeping the `actionsUrl` link exactly as it is (it is orthogonal). Do NOT widen `roleLine.ts` — the wire enums `ActiveRole`/`ActiveStep` genuinely carry no construction worker class, and a caller that knows the worker class from `lifecycles.gen.ts` can just say the line.
  - [ ] **Verify first:** `RoleAvatar`'s `PROP_FOR` (`components/RoleAvatar.tsx:224`) already carries `senior-developer`, `junior-developer`, `test-engineer`, `software-tester`, `ux-reviewer`, `qa-engineer`, `ui-designer`, `project-manager` — verified. So a construction `roleLine.seed` renders a correct avatar with no change to `RoleAvatar` at all. Note that in the prop's doc.

- [ ] **Step 4: Gap 4 — `MarginThreadCard` can expand resolved threads.** Add `expandResolved?: boolean | undefined` (default `false`) and change the collapse guard at `:90` to `if (resolved && !active && expandResolved !== true) {`. Add the same optional prop to `CommentMargin` and pass it at the `MarginThreadCard` call site (`CommentMargin.tsx:316`). Document it as "a read-only history shows resolutions expanded (spec §7.2) — a decided thread is the POINT of the history, not noise in it."

- [ ] **Step 5: Gate and commit.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check
  ```
  Commit: `fix(comments): anchor enrolment in read-only rows and contract ops; neutral generating scene; expandable resolved threads`.

---

### Task 6: Fixtures — move, extend, and add the plan set

The four `activity-experience` fixtures live under `webApp/preview/fixtures/`, but the `preview` Playwright project builds with `ARCHISTRATOR_PREVIEW_FIXTURES: '../uitests/preview-fixtures'` (verified at `uitests/playwright.config.ts:141`) — so the suite literally cannot see them (GAP-11). They also exercise none of the scenarios §7.2/§6 need (GAP-3).

**Files:**
- Move: `webApp/preview/fixtures/web-client/activity-experience/{done,deployment-linear,service-fork-sent-back}.json` → `uitests/preview-fixtures/web-client/activity-experience/`.
- Keep in place: `webApp/preview/fixtures/web-client/activity-experience/not-started.json` (the one smoke fixture, so `fixture-schema.test.mjs`'s `DESIGN_FIXTURES` tree still validates something real).
- Add: `uitests/preview-fixtures/web-client/activity-experience/{requirements-backfilled,architecture-round,project-design-m0,failed,review-set-error,sub-attempts}.json`.
- Add: `uitests/preview-fixtures/web-client/plan/{list,graph,tasks}.json`.
- Modify: `webApp/scripts/fixture-schema.test.mjs` (realism clauses for the new shape).
- Modify: `webApp/src/components/activity/activityViewToGraph.test.ts` (the fixture paths Task 2 Step 2 wrote).

**Interfaces:**
- Fixture envelope (unchanged, enforced by `scripts/fixture-schema.mjs`): `{ route, title?, note?, ops: { <opId>: { result } | { error: { status, code?, message? } } | { pending: true } } }`, `additionalProperties: false`, `route` matching `^/`, `<screen>` and `<state>` both kebab (`/^[a-z0-9]+(-[a-z0-9]+)*$/`).
- New screens: `activity-experience` (already kebab), `plan`.

- [ ] **Step 1: Move the three, with `git mv`, and re-point the smoke fixture's route.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage5
  mkdir -p uitests/preview-fixtures/web-client/activity-experience
  for f in done deployment-linear service-fork-sent-back; do
    git mv webApp/preview/fixtures/web-client/activity-experience/$f.json \
           uitests/preview-fixtures/web-client/activity-experience/$f.json
  done
  ls webApp/preview/fixtures/web-client/activity-experience/   # must be exactly not-started.json
  ```

- [ ] **Step 2: Author the six new activity fixtures.** Each is `{ route, title, note, ops }`. Build every `constructionQueryActivityView.result` to the verified wire shape (`contracts/schema.ts:807-842` and the `Construction*` siblings) — required members `activityId`, `name`, `phases`, `state`, `tasks`, `type`; per task `dependsOn`, `id`, `kind`, `phase`, `revisions`, `state`, `title`; per revision `n`, `outcome`, `attemptIds`, `commentCount`, `comments`, `provenance`.

  | State | What it must carry (the GAP-3 list) |
  |---|---|
  | `requirements-backfilled` | `type: "requirements"`, the 8-task linear DAG (`missionDraft`→`missionReview`→`glossaryDraft`→…), 4 phases 15/20/35/30, every revision `provenance: "backfilled"`. Route `/project/archistrator/activity/requirements`. |
  | `architecture-round` | `type: "architecture"`, 2 tasks, 1 phase weight 100, `architectureReview` carrying a PERSISTED round: `round`, `subjectRef {kind:"artifact"}`, `reviewers[]`, `verdicts[]`, and a `thread[]` with one `type:"question"`, one `type:"staleAck"` and one `status:"resolved"` entry with `replies[]`. Route `…/activity/architecture?task=architectureReview`. |
  | `project-design-m0` | `type: "projectDesign"`, ONE task `sdpReview` (kind `review`), 1 phase `sdp` weight 100, `state: "awaitingHuman"`, `reviewSet` carrying BOTH `reason` and `requiresHuman: true`. **Also carries `systemDesignGetProject`** whose `Slots` hold slot 9 (`activityList`), 10 (`network`) and 16 (`sdpReview`) — the M0 body renders them (§6). Route `…/activity/projectDesign`. |
  | `failed` | `state: "failed"` with one task `state: "failed"` and a revision `outcome: "failed"`; one revision `outcome: "skipped"`; one `provenance: "synthesized"`. |
  | `review-set-error` | A live gate with `reviewSetError` set and `reviewSet` ABSENT — the reviewers-strip failure path. |
  | `sub-attempts` | One revision whose `attemptIds` has THREE entries (§7.2 "sub-attempts listed only when a revision had more than one"), plus a `subjectRef.kind: "commit"`. |

  - [ ] **Verify first:** copy the envelope and member spelling from `uitests/preview-fixtures/web-client/activity-experience/service-fork-sent-back.json` — it is the richest existing one (both fork branches live, a full persisted round, an open round with no decision). Do not invent member names; `fixture-schema.mjs` will reject them, but only after you have written six files.

- [ ] **Step 3: Capture the plan fixtures from the LIVE state, not by hand (R13).** The shipped `uitests/preview-fixtures/web-client/construction/resting.json` carries a full `systemDesignGetProject` — but its `activityExecution` has **29** rows and **no design prefix**, and its slot-10 `M0` has no `dependsOn` (verified: it predates stage 2). It is the wrong world; do not derive from it. Capture instead:
  ```bash
  # One terminal: boot the server against this repo's own state (see the
  # "run app locally" recipe — GOWORK=off, CONSTRUCTION_DRYRUN=1, local-git substrate).
  cd server && GOWORK=off CONSTRUCTION_DRYRUN=1 go run ./cmd/server

  # Another: capture the read and wrap it as a fixture.
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage5
  curl -fsS http://localhost:8888/api/v1/system-design/get-project/archistrator \
    | ASDF_NODEJS_VERSION=lts node -e '
        let s=""; process.stdin.on("data",d=>s+=d).on("end",()=>{
          const result = JSON.parse(s);
          const doc = {
            route: process.argv[1],
            title: process.argv[2],
            note: "Captured from the live local server against this repo own state. Regenerate with the curl+node recipe in the stage-5 plan, Task 6 Step 3.",
            ops: {
              compositionGetUserinfo: { result: { subject: "preview", email: "preview@example.com" } },
              systemDesignGetProject: { result }
            }
          };
          process.stdout.write(JSON.stringify(doc, null, 2) + "\n");
        });' "/project/archistrator/plan?lens=list" "Plan · LIST" \
    > uitests/preview-fixtures/web-client/plan/list.json
  ```
  Repeat for `graph.json` (`?lens=graph`) and `tasks.json` (`?lens=tasks`; that one also needs `constructionGetSessionState` — copy the shape from `construction/owed-gate.json`, which already has one). Confirm what you captured:
  ```bash
  ASDF_NODEJS_VERSION=lts node -e '
    const d=require("./uitests/preview-fixtures/web-client/plan/list.json");
    const r=d.ops.systemDesignGetProject.result;
    console.log("rows", Object.keys(r.activityExecution).length);
    console.log("prefix", ["requirements","architecture","projectDesign"].filter(k=>k in r.activityExecution));
    const net = r.Slots.find(s=>s.kind==="network").model.model;
    console.log("M0", net.milestones.find(m=>m.id==="M0"));'
  ```
  Expected: **32 rows**, all three prefix ids present, and `M0` carrying `dependsOn: ["projectDesign"]`. Anything else means the server is serving a different state — STOP and say so rather than hand-patching the JSON.
  - [ ] **Verify first:** the fixture must contain no `BUNDLE_MARKERS` string (`scripts/bundle-markers.mjs`) — `validateFixtureTree` rejects it, and a captured payload is exactly where one could sneak in.

- [ ] **Step 4: Extend the schema test's realism clauses.** `scripts/fixture-schema.test.mjs` already asserts that every construction row is one the server could serve (`CurrentPhase` is the first incomplete phase of the row's own resolved set; `hasBuildEvidence` agrees with it; the `GATE_TASK` table). Add two more, in the same register and with the same comment density. The first pins that an activity view is one `QueryActivityView` could DERIVE — it derives a lifecycle phase's `completed` from its GATE task's state (`constructionmanager.go`'s `deriveTaskViews`), and a revision's `n` is 1-based and ascending with a dispatch/review PAIR sharing numbers; a fixture that completes a phase whose gate did not pass, or numbers revisions from 0, asserts a view no server emits, and every spec over it then tests a lie. The second pins that a review revision is either a PROJECTION or a RECONSTRUCTION and never both: `provenance: "observed"` means a persisted round (verdicts and reviewers present, a decision stamped), while `"backfilled"`/`"synthesized"` means it was rebuilt from a pre-ledger row (`reviewers` empty, `decidedAt` and `subjectRef` omitted — `schema.ts` says so verbatim). Both walk every `activity-experience` fixture in BOTH trees:
  ```js
  const activityViews = (root) => {
    const { files } = validateFixtureTree(root, { validate });
    return files
      .filter((p) => p.includes(`${SURFACE}/activity-experience/`))
      .map((p) => [p, JSON.parse(readFileSync(p, 'utf8'))])
      .map(([p, doc]) => [p, doc.ops?.constructionQueryActivityView?.result])
      .filter(([, view]) => view !== undefined);
  };

  void test('every activity-experience fixture is a view the server could derive', () => {
    for (const root of [UITESTS_FIXTURES, DESIGN_FIXTURES]) {
      for (const [path, view] of activityViews(root)) {
        const byId = new Map(view.tasks.map((t) => [t.id, t]));
        for (const phase of view.phases) {
          const gate = byId.get(phase.gateTaskId);
          assert.ok(gate !== undefined, `${path}: phase ${phase.id} names a gate task that is not in tasks[]`);
          assert.equal(
            phase.completed,
            gate.state === 'passed',
            `${path}: phase ${phase.id} completion disagrees with its gate task state (deriveTaskViews derives one from the other)`
          );
        }
        assert.equal(
          view.phases.reduce((n, p) => n + p.weight, 0),
          100,
          `${path}: lifecycle-phase weights must sum to 100`
        );
        for (const task of view.tasks) {
          const ns = task.revisions.map((r) => r.n);
          assert.deepEqual(ns, [...ns].sort((a, b) => a - b), `${path}: ${task.id} revisions must be oldest first`);
          assert.ok(ns.every((n) => n >= 1), `${path}: ${task.id} revisions are 1-based`);
          for (const dep of task.dependsOn) {
            assert.ok(byId.has(dep), `${path}: ${task.id} depends on ${dep}, which is not in tasks[]`);
          }
          if (task.reviews !== undefined) {
            assert.ok(byId.has(task.reviews), `${path}: ${task.id} reviews ${task.reviews}, which is not in tasks[]`);
          }
        }
      }
    }
  });

  void test('a revision provenance matches what it carries', () => {
    for (const root of [UITESTS_FIXTURES, DESIGN_FIXTURES]) {
      for (const [path, view] of activityViews(root)) {
        for (const task of view.tasks) {
          for (const rev of task.revisions) {
            const where = `${path}: ${task.id} rev ${rev.n}`;
            assert.ok(rev.attemptIds.length >= 1, `${where}: a revision has at least one attempt`);
            if (rev.provenance === 'backfilled' || rev.provenance === 'synthesized') {
              assert.deepEqual(rev.reviewers ?? [], [], `${where}: a reconstructed revision has an empty roster`);
              assert.equal(rev.decidedAt, undefined, `${where}: a reconstructed revision carries no decision stamp`);
              assert.equal(rev.subjectRef, undefined, `${where}: a reconstructed revision names no subject`);
            }
            if (rev.provenance === 'observed' && rev.outcome !== 'running' && rev.outcome !== 'awaitingHuman') {
              assert.ok(rev.decidedAt !== undefined, `${where}: a decided, observed revision carries decidedAt`);
            }
          }
        }
      }
    }
  });
  ```

- [ ] **Step 5: Re-point Task 2's test paths.** `activityViewToGraph.test.ts` reads `done` and `not-started` from `webApp/preview/fixtures/...`; `done` has moved. Point the `fixture()` helper at `../../../../uitests/preview-fixtures/web-client/activity-experience/` for everything but `not-started`, or (cleaner) give it a two-root lookup that tries the app tree then the uitests tree and says which it used in its failure message.

- [ ] **Step 6: Gates and commit.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && ASDF_NODEJS_VERSION=lts npx playwright test --project=preview
  ```
  The preview project's `build:preview` runs `validateFixtureTree` in `buildStart` and fails on the first invalid fixture — that is the real gate on the ten new files. Commit: `test(fixtures): move the activity-experience fixtures into uitests and add the plan + GAP-3 scenarios`.

---

### Task 7: The two routes and the redirects

One commit: a route without its redirect leaves `/construction` dead, and a redirect without its target is a loop.

**Files:**
- Add: `webApp/src/contracts/routePaths.ts`, `webApp/src/contracts/routePaths.test.ts`.
- Add: `webApp/src/routes/activityRedirect.ts`, `webApp/src/routes/activityRedirect.test.ts`.
- Add: `webApp/src/routes/Plan.tsx`, `webApp/src/routes/ActivityExperience.tsx` (both thin; the containers land in Tasks 8 and 11).
- Add: `webApp/src/containers/PlanContainer.tsx`, `webApp/src/containers/ActivityExperienceContainer.tsx` (stubs this task, filled next).
- Modify: `webApp/src/routes/router.tsx` (register two routes; add `beforeLoad` to three).
- Modify: `webApp/src/components/construction/lens/useLensSelection.ts` (reduce the ROUTE-facing search codec only — see Step 4).
- Modify: `webApp/src/utilities/constants/UIIdentifiers.ts` (new `Activity` and `Plan` groups).

**Interfaces produced:**
```ts
// contracts/routePaths.ts — a contracts LEAF: zero imports, so routes, containers
// AND components may all import it. `components → routes` is lint-fatal and no
// eslint-disable is permitted, which is the whole reason these live here.
export const PLAN_PATH = '/project/$projectId/plan' as const;
export const ACTIVITY_PATH = '/project/$projectId/activity/$activityId' as const;
export const LENS_IDS = ['list', 'graph', 'tasks'] as const;
export type PlanLensId = (typeof LENS_IDS)[number];
export function isPlanLensId(value: unknown): value is PlanLensId;
/** The plan route's search object for a given lens (junk ⇒ 'list'). */
export function planSearch(lens: unknown): { lens: PlanLensId };
/** The activity route's search object; a junk `rev` is DROPPED, never coerced to NaN. */
export function activitySearch(task: unknown, rev: unknown): { task?: string; rev?: number };

// routes/activityRedirect.ts — re-exports the two paths so router.tsx has one import,
// and owns the only thing that cannot live in contracts/: the redirect THROW.
export { ACTIVITY_PATH, PLAN_PATH } from '../contracts/routePaths.ts';
/** Throws a TanStack Redirect to /plan. Pure but for the throw, exactly like operationsGuard. */
export function redirectToPlan(projectId: string, search: { lens: PlanLensId }): never;
/** What an old construction deep link becomes. `?lens=` survives; a/p/k/n/av/focus/sc do not. */
export function planSearchFromLegacy(search: Record<string, unknown>): { lens: PlanLensId };
/** The old design rails have no lens — they land on the plan's LIST. */
export function designRedirectSearch(): { lens: PlanLensId };

// routes/Plan.tsx
export function PlanScreen(): ReactNode;                 // getRouteApi('/project/$projectId/plan')
// routes/ActivityExperience.tsx
export function ActivityExperienceScreen(): ReactNode;   // getRouteApi('/project/$projectId/activity/$activityId')

// components/construction/lens/useLensSelection.ts — the ROUTE-facing codec only
export interface LensSearchParams { lens?: LensId }      // a, p, k, n, av, focus, sc dropped from the URL
export function validateLensSearch(search: Record<string, unknown>): LensSearchParams;
// KEPT, UNCHANGED, and NOT deleted in Task 13 either:
//   LENS_IDS, LensId, LensSelection (tasks/TasksLens.tsx:47, tasks/decisionFlow.ts:52),
//   SCOPE_IDS, ScopeId, SORT_IDS, SortId, ToolbarState, DEFAULT_TOOLBAR,
//   toolbarSignatureOf, loadToolbar, LensToolbarApi, useLensToolbar
// KEPT UNTIL TASK 13, then deleted with their last consumer:
//   ArtifactViewState, ARTIFACT_VIEW_IDS, ArtifactViewId, LensState,
//   parseLensSearch, serializeLensSearch, artifactKeptFor, LensSelectionApi, useLensSelection
```

> **`LensSelection` survives this stage.** `components/construction/tasks/TasksLens.tsx:47` (`selection: LensSelection` at `:99`, `isSelected` at `:348`) and `components/construction/tasks/decisionFlow.ts:52` (`:346`) both consume it, and both files are KEEP (spec §7.3: the TASKS lens survives as is). Task 13 NARROWS the type to the members those two read and deletes nothing from it in this stage.

- [ ] **Step 1: Write `contracts/routePaths.ts` FIRST** — nothing else in this task compiles without it, and putting these literals anywhere under `routes/` makes Tasks 8–11 lint-fatal.

  ```ts
  /**
   * The two route paths the Activity Experience and the Plan live at, plus the
   * pure codecs for their search params.
   *
   * WHY THIS IS IN `contracts/`. The boundary DAG (eslint.platform.config.js
   * BOUNDARY_RULES:63-71) gives `containers → [containers, components, hooks,
   * contracts, utilities]` and `components → [components, contracts,
   * utilities]`. Neither may import `routes/`. A container that navigates and a
   * component that renders a row-as-a-link both need the path literal, so the
   * literal cannot live beside the route. `contracts → contracts` permits a
   * leaf, and this file imports NOTHING — which also lets `node --test` load it
   * directly.
   *
   * The literals are the exact strings `createRoute({ path })` registers, so
   * TanStack's module augmentation (router.tsx:178) type-checks every
   * `navigate({ to: PLAN_PATH })` against the real route tree.
   */
  export const PLAN_PATH = '/project/$projectId/plan' as const;
  export const ACTIVITY_PATH = '/project/$projectId/activity/$activityId' as const;

  export const LENS_IDS = ['list', 'graph', 'tasks'] as const;
  export type PlanLensId = (typeof LENS_IDS)[number];

  export function isPlanLensId(value: unknown): value is PlanLensId {
    return typeof value === 'string' && (LENS_IDS as readonly string[]).includes(value);
  }

  /**
   * `lens` is ALWAYS emitted, even for the default `list`: a route's
   * validateSearch output IS the address bar, so dropping it would quietly
   * rewrite a shared deep link — the failure the old console's codec documented
   * at useLensSelection.ts:157-163 and this one inherits.
   */
  export function planSearch(lens: unknown): { lens: PlanLensId } {
    return { lens: isPlanLensId(lens) ? lens : 'list' };
  }

  /**
   * A junk `rev` is DROPPED, never coerced: `NaN` downstream renders as
   * "Revision NaN" and silently poisons every comparison that touches it — the
   * rule `useLensSelection.attemptOf` already states for the old `?n=`.
   */
  export function activitySearch(task: unknown, rev: unknown): { task?: string; rev?: number } {
    const n = typeof rev === 'string' || typeof rev === 'number' ? Number(rev) : Number.NaN;
    return {
      ...(typeof task === 'string' && task.length > 0 ? { task } : {}),
      ...(Number.isInteger(n) && n >= 1 ? { rev: n } : {}),
    };
  }
  ```
  `contracts/routePaths.test.ts`:
  ```ts
  /// <reference types="node" />
  import { test } from 'node:test';
  import assert from 'node:assert/strict';
  import { ACTIVITY_PATH, activitySearch, PLAN_PATH, planSearch } from './routePaths.ts';

  void test('the path literals are exactly what the router registers', () => {
    assert.equal(PLAN_PATH, '/project/$projectId/plan');
    assert.equal(ACTIVITY_PATH, '/project/$projectId/activity/$activityId');
  });

  void test('an unknown lens falls back to list, and the default is still emitted', () => {
    assert.deepEqual(planSearch('graph'), { lens: 'graph' });
    assert.deepEqual(planSearch('nonsense'), { lens: 'list' });
    assert.deepEqual(planSearch(undefined), { lens: 'list' });
  });

  void test('a junk revision is dropped rather than coerced to NaN', () => {
    assert.deepEqual(activitySearch('srsReview', '2'), { task: 'srsReview', rev: 2 });
    assert.deepEqual(activitySearch('srsReview', 'x'), { task: 'srsReview' });
    assert.deepEqual(activitySearch('srsReview', '0'), { task: 'srsReview' });
    assert.deepEqual(activitySearch('srsReview', '1.5'), { task: 'srsReview' });
    assert.deepEqual(activitySearch('', undefined), {});
  });
  ```

- [ ] **Step 1b: Write `routes/activityRedirect.ts` + its test**, modelled line-for-line on `routes/operationsGuard.ts` / `operationsGuard.test.ts` (the repo's one precedent — the guard lives in its own module because `router.tsx`'s header bans local definitions for fast-refresh AND because a `beforeLoad` inside `router.tsx` cannot be unit-tested). The module:

  ```ts
  import { redirect } from '@tanstack/react-router';
  import { ACTIVITY_PATH, PLAN_PATH, planSearch, type PlanLensId } from '../contracts/routePaths.ts';

  export { ACTIVITY_PATH, PLAN_PATH };

  /**
   * An old construction deep link keeps only its LENS. `a`/`p`/`k`/`n` addressed
   * a task attempt inside the DetailPane and `av`/`focus`/`sc` a view of its
   * artifact; the pane is gone, so those params address nothing. Carrying them
   * into `/plan` would put junk in the address bar that validateSearch strips on
   * the next navigation — worse than dropping them here, where it is deliberate.
   */
  export function planSearchFromLegacy(search: Record<string, unknown>): { lens: PlanLensId } {
    return planSearch(search['lens']);
  }

  /** The design rails had no lens of their own; they land on the plan's LIST. */
  export function designRedirectSearch(): { lens: PlanLensId } {
    return planSearch('list');
  }

  export function redirectToPlan(projectId: string, search: { lens: PlanLensId }): never {
    // redirect({ throw: true }) throws internally — @tanstack/router-core's
    // Redirect extends Response, not Error, so an explicit `throw redirect(...)`
    // trips @typescript-eslint/only-throw-error (operationsGuard.ts:88-91).
    redirect({ to: PLAN_PATH, params: { projectId }, search, throw: true });
    throw new Error('unreachable: redirect did not throw');
  }
  ```
  `routes/activityRedirect.test.ts` (`isRedirect(err)` + `err.options.*`, exactly as `operationsGuard.test.ts` does):
  ```ts
  /// <reference types="node" />
  import { test } from 'node:test';
  import assert from 'node:assert/strict';
  import { isRedirect } from '@tanstack/react-router';
  import { designRedirectSearch, planSearchFromLegacy, PLAN_PATH, redirectToPlan } from './activityRedirect.ts';

  void test('an old construction link keeps its lens and drops the pane selection', () => {
    assert.deepEqual(
      planSearchFromLegacy({
        lens: 'tasks', a: 'C-x', p: 'construction', k: 'codeReview', n: 2, av: 'code', focus: 1, sc: 'S1',
      }),
      { lens: 'tasks' }
    );
  });

  void test('an unknown or absent lens falls back to list rather than throwing', () => {
    assert.deepEqual(planSearchFromLegacy({ lens: 'nonsense' }), { lens: 'list' });
    assert.deepEqual(planSearchFromLegacy({}), { lens: 'list' });
  });

  void test('the design rails land on the plan list', () => {
    assert.deepEqual(designRedirectSearch(), { lens: 'list' });
  });

  void test('the redirect names the plan route, the project it came from, and its lens', () => {
    assert.throws(
      () => redirectToPlan('archistrator', { lens: 'graph' }),
      (err: unknown): boolean => {
        assert.ok(isRedirect(err), 'expected a Redirect to be thrown, not an arbitrary error');
        const options = (err as {
          options: { to?: string; params?: { projectId?: string }; search?: { lens?: string } };
        }).options;
        assert.equal(options.to, PLAN_PATH);
        assert.equal(options.params?.projectId, 'archistrator');
        assert.equal(options.search?.lens, 'graph');
        return true;
      }
    );
  });
  ```

- [ ] **Step 2: Register the two routes in `router.tsx`.** Follow the file's existing shape exactly (`createRoute({ getParentRoute, path, component, validateSearch })`, added to `rootRoute.addChildren([...])` at `:149`, and its header comment's route list updated):

  ```ts
  const planRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/project/$projectId/plan',
    component: PlanScreen,
    // The plan's LENS is the only selection left in the URL — the DetailPane's
    // a/p/k/n/av/focus/sc died with it (spec §7.4). An unknown lens falls back
    // to `list` instead of throwing, and `lens` is ALWAYS emitted, even for the
    // default, because validateSearch's output IS the address bar: dropping it
    // would quietly rewrite a shared deep link.
    validateSearch: validateLensSearch,
  });

  const activityRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/project/$projectId/activity/$activityId',
    component: ActivityExperienceScreen,
    // ?task=<lifecycle task id>&rev=<1-based revision>. Both optional: absent,
    // the experience opens the default task (spec §7.2 — awaiting-human →
    // failed → running → last passed → first) at its latest revision. The codec
    // is `contracts/routePaths.activitySearch`, not an inline lambda, because
    // the containers construct the same object when they navigate and a second
    // copy of the rule is how two callers end up disagreeing about `?rev=0`.
    validateSearch: (search: Record<string, unknown>): { task?: string; rev?: number } =>
      activitySearch(search['task'], search['rev']),
  });
  ```
  Both new imports come from `../contracts/routePaths` (`activitySearch`) and the existing `validateLensSearch`; `PlanScreen` and `ActivityExperienceScreen` from their route files. Add both routes to `rootRoute.addChildren([...])` at `:149`.

- [ ] **Step 3: Redirect the three old routes.** Add `beforeLoad` to `constructionRoute`, `systemDesignRoute` and `projectDesignRoute`, each a one-liner into `activityRedirect.ts`, with a comment saying the screen moved to `/plan` and why the module is separate. Keep the routes registered (an unregistered path is a 404, not a redirect) and keep `constructionRoute`'s `validateSearch: validateLensSearch` so a legacy `?lens=` still parses on the way through.

- [ ] **Step 4: Narrow ONLY the route-facing codec in `useLensSelection.ts` (R8).** Change `LensSearchParams` to `{ lens?: LensId }` and make `validateLensSearch` return `planSearch(search['lens'])` from `contracts/routePaths.ts` — so the plan route and the still-registered construction route agree on one rule. **Change nothing else in the file in this task.** `parseLensSearch`, `serializeLensSearch`, `artifactKeptFor`, `LensState`, `ArtifactViewState`, `ARTIFACT_VIEW_IDS`, `ArtifactViewId`, `LensSelectionApi` and `useLensSelection` still have live consumers (`ConstructionConsole.tsx:147,398`, `ConstructionShell.tsx:76`, `DetailPane.tsx:102-105,367`, `UnknownBody.tsx:47,99`, `ArtifactPlacementView.tsx:23`, `useLensSelection.test.ts`, `artifactViewSearch.test.ts`) and are deleted in Task 13 Step 2, with those consumers. Mark them:
  ```ts
  /** @deprecated Dies with the construction console in Task 13 Step 2. No new caller. */
  ```
  **`LensSelection` is NOT deprecated and NOT deleted in this stage** — `tasks/TasksLens.tsx:47` and `tasks/decisionFlow.ts:52` consume it and both files survive (spec §7.3). Give it the note it earns instead:
  ```ts
  /**
   * The one selectable thing. The URL stopped carrying it in stage 5 (the plan's
   * only search param is `lens`), but the TASKS lens still passes one down to
   * mark the row a decision belongs to — see TasksLens.isSelected and
   * decisionFlow. Task 13 narrows it to the members those two read.
   */
  ```

- [ ] **Step 5: Stub the two screens and their containers, WITH their screen ids.** `routes/Plan.tsx` and `routes/ActivityExperience.tsx` each call `getRouteApi('<literal path>')` at module scope and render their container with `useParams()`/`useSearch()` — no local component definitions, matching `routes/HomeBase.tsx:54,81` and `routes/Billing.tsx:27,30`. The containers render an honest "filled in the next task" placeholder wrapped in `ExperienceChrome` with the right `eyebrow` — and each already carries its screen id (`data-testid={UI_IDENTIFIERS.Plan.SCREEN}` / `UI_IDENTIFIERS.Activity.SCREEN}`) on its outermost element, so the route is navigable, the fixtures render something real, and Task 12's very first assertion has something to select at this commit. **An id declared in Step 6 and never placed on an element is the same failure as an id asserted and never declared** — Step 6's table therefore names the file and component that places each one.

- [ ] **Step 6: Add the id groups.** In `UI_IDENTIFIERS`, beside `ActivityLifecycle`, add (kebab values, camelCase functions for parameterised ids — the file's convention):
  ```ts
  Activity: {
    SCREEN: 'activity-screen',
    EYEBROW: 'activity-eyebrow',
    DISPATCH_BODY: 'activity-dispatch-body',
    REVIEW_BODY: 'activity-review-body',
    REVIEWERS_STRIP: 'activity-reviewers-strip',
    REVIEW_SET_ERROR: 'activity-review-set-error',
    TASK_FACTS: 'activity-task-facts',
    SUB_ATTEMPTS: 'activity-sub-attempts',
    HISTORY_BANNER: 'activity-history-banner',
    BACK_TO_LATEST: 'activity-back-to-latest',
    ARTIFACT_PANEL: 'activity-artifact-panel',
    ARTIFACT_UNAVAILABLE: 'activity-artifact-unavailable',
    AMEND_ARCHITECTURE: 'activity-amend-architecture',
    reviewerChip: (role: string) => `activity-reviewer-${role}`,
  },
  Plan: {
    SCREEN: 'plan-screen',
    LENS_LIST: 'plan-lens-list',
    LENS_GRAPH: 'plan-lens-graph',
    LENS_TASKS: 'plan-lens-tasks',
    LIST: 'plan-list',
    GRAPH: 'plan-graph',
    M0_DIVIDER: 'plan-m0-divider',
    GUTTER: 'plan-gutter',
    UNPLACED_NOTE: 'plan-unplaced-note',
    row: (activityId: string) => `plan-row-${activityId}`,
    tile: (activityId: string) => `plan-tile-${activityId}`,
    milestone: (id: string) => `plan-milestone-${id}`,
    gutterRow: (row: string) => `plan-gutter-${row}`,
  },
  ```

  **Every id above has a named placing site. A declared-but-unplaced id is a Task-12 failure, so this table is the contract between the two tasks:**

  | Id | Placed on | In |
  |---|---|---|
  | `Activity.SCREEN` | the container's outermost `Box` | Task 7 Step 5 (stub), kept in Task 8 |
  | `Activity.EYEBROW` | the `ExperienceChrome` eyebrow `Typography` — pass it through as a new optional `eyebrowTestId` prop, or wrap the value; decide in Task 8 Step 5 and place it there | Task 8 Step 5 |
  | `Activity.TASK_FACTS` | `TaskHeader`'s facts row | Task 8 Step 3 |
  | `Activity.DISPATCH_BODY` | `DispatchBody`'s root | Task 8 Step 4 |
  | `Activity.SUB_ATTEMPTS` | the sub-attempt line | Task 8 Step 4 |
  | `Activity.REVIEW_BODY` | `ReviewBody`'s root | Task 9 Step 5 |
  | `Activity.REVIEWERS_STRIP`, `Activity.reviewerChip`, `Activity.REVIEW_SET_ERROR` | `ReviewersStrip` | Task 9 Step 3 |
  | `Activity.ARTIFACT_PANEL`, `Activity.ARTIFACT_UNAVAILABLE` | `ArtifactPanel` | Task 9 Step 4 |
  | `Activity.AMEND_ARCHITECTURE` | the M0 body's link | Task 9 Step 5 |
  | `Activity.HISTORY_BANNER`, `Activity.BACK_TO_LATEST` | the read-only header | Task 10 Step 4 |
  | `Plan.SCREEN` | the container's outermost `Box` | Task 7 Step 5 (stub), kept in Task 11 |
  | `Plan.LENS_LIST`, `Plan.LENS_GRAPH`, `Plan.LENS_TASKS` | the lens toggle's three buttons | Task 11 Step 5 |
  | `Plan.LIST`, `Plan.row`, `Plan.M0_DIVIDER` | `PlanList` | Task 11 Step 3 |
  | `Plan.GRAPH`, `Plan.tile`, `Plan.milestone`, `Plan.UNPLACED_NOTE` | `PlanGraph` / `PlanTile` | Task 11 Step 4 |
  | `Plan.GUTTER`, `Plan.gutterRow` | the pinned HTML gutter | Task 11 Step 4 |

- [ ] **Step 7: Gates and commit.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && ASDF_NODEJS_VERSION=lts npx tsc --noEmit && ASDF_NODEJS_VERSION=lts npx eslint . && ASDF_NODEJS_VERSION=lts npx playwright test --project=preview
  ```
  The preview suite proves the fixtures' routes now resolve instead of hitting the router's not-found (GAP-2). Commit: `feat(routes): the plan and activity routes, with the old rails redirecting`.

---

### Task 8: The Activity Experience container, route and dispatch body

**Files:**
- Modify: `webApp/src/containers/ActivityExperienceContainer.tsx`, `webApp/src/routes/ActivityExperience.tsx`.
- Add: `webApp/src/components/activity/DispatchBody.tsx`, `webApp/src/components/activity/TaskHeader.tsx`.
- Add: `webApp/src/components/activity/activitySelection.ts`, `activitySelection.test.ts` (the URL ⇄ selection rule).

**Interfaces produced:**
```ts
// components/activity/activitySelection.ts
export interface ActivitySelection { taskId: string; revision: number }
/** The selection the URL asks for, corrected against what the activity actually has. */
export function selectionFor(
  nodes: readonly LifecycleNode[], task: string | undefined, rev: number | undefined
): ActivitySelection | undefined;
/** True when the reader is looking at history rather than the head (drives R1's read-only mode). */
export function isHistorical(nodes: readonly LifecycleNode[], sel: ActivitySelection): boolean;

// components/activity/TaskHeader.tsx
export function TaskHeader(props: {
  title: string;
  facts: TaskFacts;                                   // from activityViewToGraph
  revisions: readonly LifecycleRevision[];
  revision: number;
  onRevision: (n: number) => void;
}): ReactNode;

// components/activity/DispatchBody.tsx
export function DispatchBody(props: {
  title: string;
  facts: TaskFacts;
  revisions: readonly LifecycleRevision[];
  revision: number;
  onRevision: (n: number) => void;
  /** The selected revision's episode timeline, already fetched by the container. */
  timeline: EpisodeTimeline | undefined;
  timelineLoading: boolean;
  timelineError: Error | null;
  /** Running ⇒ the generating scene instead of a timeline. */
  running: boolean;
  /** Attempt ids of the selected revision — the sub-attempt line shows only above 1. */
  attemptIds: readonly string[];
}): ReactNode;

// containers/ActivityExperienceContainer.tsx
export function ActivityExperienceContainer(props: {
  projectId: string;
  activityId: string;
  task: string | undefined;
  rev: number | undefined;
}): ReactNode;
```

- [ ] **Step 1: Write `activitySelection.ts` + tests.** Rules: an unknown `task` falls back to `defaultTaskId`; a `rev` the task does not have falls back to `latestRevision`; a task with no revisions yields `revision: 0` (the graph's own "no revision" value — `latestRevision` returns 0 for an empty list, verified at `lifecycleGraphTypes.ts:119`); `isHistorical` is `sel.revision > 0 && sel.revision < latestRevision(node)`. Four tests minimum, one per rule.

- [ ] **Step 2: Write the container.** It is the ONLY file here that may call hooks. It makes **TWO reads, for every activity type**:
  ```ts
  import { ACTIVITY_PATH, PLAN_PATH, activitySearch } from '../contracts/routePaths.ts';

  const { data: view, error, isLoading } = useActivityView(projectId, activityId);
  // The SECOND read is not optional and not projectDesign-only. The review body's
  // renderer branch takes `ArtifactRendererProps` (artifactRenderers.tsx:25-31),
  // whose `vm.row` is a decoded ConstructionRow and whose `project` is the whole
  // project state — none of which QueryActivityView carries. The design bodies
  // need the artifact SLOTS from the same read, and the M0 body needs slots
  // 9/10/16. One read serves all three; splitting it by type would mean three
  // different reasons for the same call.
  const { data: project } = useProject(projectId);
  const row = project?.constructionRows?.[activityId];
  ```
  then `activityViewToGraph(view)` → `selectionFor(...)` → `taskFactsFor(view, sel.taskId)`. For a dispatch task it resolves the revision's `episodeId` and calls
  ```ts
  useEpisodeTimeline({ projectId, manager: 'construction', targetRef: activityId }, episodeId)
  ```
  (`hooks/useEpisodes.ts` — the list op matches by ACTIVITY, not by exact `TargetRef`, so a legacy record with a bare id still resolves). Navigation is `navigate({ to: ACTIVITY_PATH, params: { projectId, activityId }, search: () => activitySearch(taskId, rev) })` — `?task` and `?rev` are the selection, so a 2 s poll's remount cannot wipe it and a link addresses exactly one revision. ✕ navigates to `PLAN_PATH` with the remembered lens (Task 11 adds the memory; here it is `'list'`).
  - **`useProject` here takes NO poll interval** (`refetchInterval` defaults to `false`, `hooks/useProject.ts:28`). The activity screen's liveness is `useActivityView`'s own 2 s/8 s cadence; adding a second poll for slot data that only moves on a commit would double the traffic for nothing.
  - 404 handling: `useActivityView`'s `retry` treats a no-session 404 as an ERROR and never polls it (`hooks/activityViewPolling.ts` returns `false`). Render `activityCopy.ACTIVITY_NOT_IN_PLAN`, not a spinner.

- [ ] **Step 3: Write `TaskHeader.tsx`** — title + `RevisionSelect` + the three facts (`workerClass · command · exitCriterion`), each omitted when the join found nothing rather than rendered blank. `data-testid={UI_IDENTIFIERS.Activity.TASK_FACTS}`. Port the layout from `webApp/proto/TaskHeader.tsx` (50 L).

- [ ] **Step 4: Write `DispatchBody.tsx`** — `TaskHeader` + one of: `GeneratingScene` when `running` (passing the Task 5 `roleLine` built from `facts.workerClass` + the task title, and a `footerNote` that says where THIS job runs, not "the design job"), else `EpisodeTimeline` (`components/episodes/EpisodeTimeline.tsx:69`, props `{ timeline, loading, error }`). Below it, `activityCopy.subAttemptsLine(attemptIds.length)` under `Activity.SUB_ATTEMPTS`, rendered ONLY when `attemptIds.length > 1`. Port the structure from `webApp/proto/DispatchBody.tsx` (444 L) but drop its fixture→wire fabrication entirely — the container hands real `EpisodeTimeline` data.

- [ ] **Step 5: Assemble in `ExperienceChrome`.** `eyebrow={eyebrowFor(...)}`, `spine={<LifecycleGraph … />}`, `bodyScroll="shared"` (required for the margin to place cards — the component DEV-warns otherwise), `margin={(scrollRoot) => <CommentMargin … />}` (wired properly in Task 9; here pass `undefined`). Port the shape from `webApp/proto/ActivityExperienceProto.tsx` (153 L).

- [ ] **Step 6: Gates and commit.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && ASDF_NODEJS_VERSION=lts npx playwright test --project=preview
  ```
  Commit: `feat(activity): the Activity Experience route, container and dispatch body`.

---

### Task 9: The review body, the verbs table, and the Project Design M0 body

**Files:**
- Add: `webApp/src/containers/activityVerbs.ts`, `activityVerbs.test.ts`.
- Add: `webApp/src/components/activity/ReviewBody.tsx`, `ReviewersStrip.tsx`.
- Add: `webApp/src/components/activity/taskArtifactFor.ts`, `taskArtifactFor.test.ts`.
- Add: `webApp/src/components/activity/ArtifactPanel.tsx`.
- Modify: `webApp/src/containers/ActivityExperienceContainer.tsx`.

**Interfaces produced:**
```ts
// containers/activityVerbs.ts — PURE. Which op a verb dispatches for which activity type (R12).
export type VerbTarget =
  | { kind: 'constructionPhaseDecision'; lifecyclePhase: string }
  | { kind: 'designReviewDecision'; artifactKind: ArtifactKind }
  | { kind: 'sdpDecision' }
  | { kind: 'none'; reason: string };
export interface VerbsFor {
  approve: VerbTarget;
  sendBack: VerbTarget;          // { kind: 'none' } for projectDesign (spec R7)
  ask: VerbTarget;
  commentStatus: VerbTarget;     // { kind: 'none' } for construction (R2/GAP-6)
  rerun: VerbTarget;
  allowSendBack: boolean;
  approveCopy?: { label: string; consequence: string };
}
export function verbsFor(input: {
  type: string; variant?: string | undefined; taskId: string; lifecyclePhase: string;
  /** The RESOLVED kind from `taskFactsFor` (Task 2) — a review task has none of its own. */
  artifactKind?: string | undefined;
}): VerbsFor;

// components/activity/taskArtifactFor.ts — R17
export type TaskArtifact =
  | { kind: 'slot'; artifactKind: ArtifactKind }          // ArtifactRenderer / ProjectArtifactRenderer
  | { kind: 'serviceContract'; componentId: string }      // ServiceContractView
  | { kind: 'classified'; classification: Classification } // components/construction/artifactRenderers.tsx
  | { kind: 'unavailable'; reason: string };              // activityCopy.artifactUnavailable(...)
export function taskArtifactFor(input: {
  type: string; variant?: string | undefined; taskId: string;
  componentId?: string | undefined;
  /** The RESOLVED kind from `taskFactsFor`, same as above. */
  artifactKind?: string | undefined;
}): TaskArtifact;

// components/activity/ArtifactPanel.tsx
export function ArtifactPanel(props: {
  artifact: TaskArtifact;
  /** For the `slot` branch: the committed slot envelopes from useProject. */
  slots: readonly ArtifactSlotView[];
  /** For the `classified` branch — exactly artifactRenderers.tsx's ArtifactRendererProps. */
  vm: ArtifactActivityVM | undefined;                     // { activityId, name, row: ConstructionRow }
  project: ProjectStateWithGit | undefined;
  systemEnvelope: ArtifactModelEnvelope | undefined;
  /** For the `serviceContract` branch: the join the container already computes. */
  serviceContracts: ServiceContract[] | undefined;
}): ReactNode;

// components/activity/ReviewersStrip.tsx
export function ReviewersStrip(props: {
  /** The LIVE set, while a gate is open. */
  reviewSet?: components['schemas']['ConstructionReviewSet'] | undefined;
  /** The engine's refusal, when it refused (reviewSetError). */
  error?: string | undefined;
  /** The HISTORICAL roster of the selected revision — a different wire shape. */
  roster?: readonly components['schemas']['ConstructionReviewRosterSeat'][] | undefined;
  verdicts?: readonly components['schemas']['ConstructionReviewVerdictView'][] | undefined;
}): ReactNode;
```

> **Where the artifact kind comes from.** `verbsFor` and `taskArtifactFor` both take `artifactKind` as an INPUT, and the container supplies `taskFactsFor(view, sel.taskId).artifactKind` — the RESOLVED value, which for a review task is the kind of the dispatch it judges (R7, Task 2 Step 1 `artifactKindOf`). Reading `lifecycles.gen.ts`'s `task.artifactKind` directly here would hand every design review `undefined` and silently strip its approve verb.

> **Where the renderer props come from.** `artifactRenderers.tsx:25-31` declares `ArtifactRendererProps = { vm: ArtifactActivityVM; project?: ProjectStateWithGit; systemEnvelope?: ArtifactModelEnvelope; t: Tokens }` with `ArtifactActivityVM = { activityId: string; name: string; row: ConstructionRow }` (`:19-23`). `QueryActivityView` carries none of it, so the container's second read (Task 8 Step 2) supplies all of it: `vm = { activityId, name: view.name, row: project.constructionRows[activityId] }`, `project` is that read, `systemEnvelope` is the `system` slot's model, and `t` comes from `useTokens()` inside `ArtifactPanel` (a component may call the theme hook; it is `utilities`, not `hooks`). When `row` is absent — a planned-no-record activity — the `classified` branch degrades to `unavailable`, because a renderer with no row has nothing to draw.

> **Two roster shapes, both rendered (verified):** `reviewSet.reviewers` is `ConstructionReviewer { role, perspective, mayAmend, referenceArtifact? }` — the live proposal; `revision.reviewers` is `ConstructionReviewRosterSeat { role, actor, required }` — what the round was actually opened with. The strip renders whichever it has, and both when a live gate sits on a persisted round. Do not collapse them into one type; they say different things.

- [ ] **Step 1: Write `activityVerbs.ts` + tests** to R12. It is PURE — it names a target, it does not call a hook (the container maps a target onto the hook). Tests:
  ```ts
  void test('a construction gate approves through the phase decision, keyed on the task own lifecycle phase', () => {
    const v = verbsFor({ type: 'service', taskId: 'designReview', lifecyclePhase: 'detailed_design' });
    assert.deepEqual(v.approve, { kind: 'constructionPhaseDecision', lifecyclePhase: 'detailed_design' });
    assert.equal(v.allowSendBack, true);
  });
  void test('a construction thread offers no comment-status verb, and says why', () => {
    const v = verbsFor({ type: 'service', taskId: 'designReview', lifecyclePhase: 'detailed_design' });
    assert.equal(v.commentStatus.kind, 'none');
    assert.match((v.commentStatus as { reason: string }).reason, /cannot yet be resolved/);
  });
  void test('a requirements review decides on its artifact kind, not on a phase', () => {
    // 'Glossary' is what `taskFactsFor` RESOLVED for `glossaryReview` — through
    // its `reviews: 'glossaryDraft'` hop, since the review task carries no kind
    // of its own. Passing the raw `task.artifactKind` here would pass undefined.
    const v = verbsFor({ type: 'requirements', taskId: 'glossaryReview', lifecyclePhase: 'glossary', artifactKind: 'Glossary' });
    assert.deepEqual(v.approve, { kind: 'designReviewDecision', artifactKind: 'glossary' });
  });
  void test('the resolved kind is what the caller passes — the raw table has none for a review', () => {
    // Guards the seam between Task 2 and Task 9: verbsFor must be fed the
    // RESOLVED kind, and it must refuse to invent one when it is missing.
    const v = verbsFor({ type: 'architecture', taskId: 'architectureReview', lifecyclePhase: 'architecture' });
    assert.equal(v.approve.kind, 'none');
    assert.match((v.approve as { reason: string }).reason, /artifact kind/);
    const resolved = verbsFor({ type: 'architecture', taskId: 'architectureReview', lifecyclePhase: 'architecture', artifactKind: 'System' });
    assert.deepEqual(resolved.approve, { kind: 'designReviewDecision', artifactKind: 'system' });
  });
  void test('a construction artifact kind has no app-string counterpart and is refused, not guessed', () => {
    // 'SRS' / 'DetailedDesign' / 'Construction' / 'Integration' / 'STP' are
    // construction kinds; ARTIFACT_KIND_APP_STRINGS holds only the 17 slot kinds.
    const v = verbsFor({ type: 'service', taskId: 'srsReview', lifecyclePhase: 'requirements', artifactKind: 'SRS' });
    assert.equal(v.approve.kind, 'constructionPhaseDecision', 'a service gate never routes through the design op');
  });
  void test('projectDesign approves the SDP, offers no send-back, and carries the M0 wording', () => {
    const v = verbsFor({ type: 'projectDesign', taskId: 'sdpReview', lifecyclePhase: 'sdp' });
    assert.deepEqual(v.approve, { kind: 'sdpDecision' });
    assert.equal(v.sendBack.kind, 'none');
    assert.equal(v.allowSendBack, false);
    assert.equal(v.approveCopy?.label, 'Approve plan & cost — start construction');
  });
  ```
  and the Go-cased → app-string table this needs, verbatim (`lifecycles.gen.ts`'s `artifactKind` values are Go-cased; `ARTIFACT_KIND_APP_STRINGS` in `enums.gen.ts` holds the 17 app strings):
  ```ts
  /**
   * The six lifecycle artifact kinds that name a committed SLOT. The other five
   * ('SRS', 'DetailedDesign', 'Construction', 'Integration', 'STP') are
   * CONSTRUCTION artifacts with no slot and no app string — they are decided
   * through the phase decision, never through a design review op, so they are
   * absent here on purpose rather than mapped to something plausible.
   */
  const SLOT_KIND: Readonly<Record<string, ArtifactKind>> = {
    Mission: 'mission',
    Glossary: 'glossary',
    Volatilities: 'volatilities',
    CoreUseCases: 'coreUseCases',
    System: 'system',
    SdpReview: 'sdpReview',
  };
  ```
  Return `{ kind: 'none', reason: 'this task names no artifact kind' }` from the design branch when the lookup misses — a design review with no resolvable kind must refuse loudly, not approve something arbitrary.

- [ ] **Step 2: Write `taskArtifactFor.ts` + tests (R17), porting the dispatch BEFORE Task 13 deletes it.** Three modules are the source; port from all three and cite each:

  **(a) `detail/bodies/bodyDispatch.ts`** — `ArtifactBodyKind` (`:64` — exactly `service | uiDesign | frontend | testing:plan | testing:systemTest`), `ARTIFACT_PHASES` (**`:89`**), `isArtifactBodyKind` (`:105`) and `lifecyclePhaseOfTask` (`:110`). `ARTIFACT_PHASES` is the rule that stops the service contract being labelled the SRS's artifact, and it must come across verbatim, comment and all:
  ```ts
  const ARTIFACT_PHASES: Record<ArtifactBodyKind, readonly string[]> = {
    // The service contract is placed by the contract JOIN rather than a phase
    // alone: a missing contract and a contract by design need different bodies.
    service: [],
    uiDesign: ['detailed_design'],
    frontend: ['construction'],
    'testing:plan': ['construction', 'integration'],
    'testing:systemTest': ['construction', 'integration'],
  };
  ```
  **(b) `components/construction/artifactRenderers.tsx`** — the `Classification → renderer` registry itself is KEPT and imported by `ArtifactPanel`; only the path to it moves.
  **(c) `detail/bodies/ArtifactBody.tsx`** — this is the ONLY thing that reaches the registry today, so its branch ORDER is what must survive: `service` falls to `ServiceContractView` through the contract join (not through the registry, which has no `service` entry); `testing:plan` / `testing:systemTest` / `frontend` / `uiDesign` go to `artifactRenderers[classification]`; everything else falls to the unknown body, which becomes `{ kind: 'unavailable' }`. Read it before writing `taskArtifactFor`, and record in the new file's header that it replaces it.

  Tests: a service `designReview` → `serviceContract`; a frontend `codeReview` → `classified: 'frontend'`; a uiDesign `designReview` → `classified: 'uiDesign'`; a testing:plan `codeReview` → `classified: 'testing:plan'`; a requirements `glossaryReview` → `slot: 'glossary'`; a projectDesign `sdpReview` → `slot: 'sdpReview'`; a `deployment` construction task → `unavailable` with a reason naming `deployment`; a `documentation`, an `integration`, a `testing:harness`, a `testing:perf` and a `testing:qaProcess` task → `unavailable` (the seven R17 earmarks, each asserted once so the earmark list cannot drift from the code); a service `srsReview` → `unavailable` (the contract is NOT the SRS's artifact — the `ARTIFACT_PHASES` rule).

- [ ] **Step 3: Write `ReviewersStrip.tsx`.** Chips for each reviewer (role + perspective, `required`/`mayAmend` as text, never as colour alone), the engine's one-line `reason` beneath, and — when `error` is set and `reviewSet` is absent — `activityCopy.reviewSetRefused(error)` under `Activity.REVIEW_SET_ERROR` with `role="status"`. Historical verdicts render as one line each (`reviewerRole · verdict · summary · at`).

- [ ] **Step 4: Write `ArtifactPanel.tsx`** — the dispatcher's renderer, and the file that replaces `ArtifactBody.tsx` as the review aids' only reachable caller. Props are the block in this task's Interfaces section; the four branches:
  - `slot` → `ArtifactRenderer` (`components/ArtifactRenderer.tsx:29`, Phase-1 kinds) or `ProjectArtifactRenderer` (`components/project/ProjectArtifactRenderer.tsx:34`, Phase-2 kinds), fed `slots.find((s) => s.kind === artifact.artifactKind)?.model`.
  - `serviceContract` → `ServiceContractView` over the contract join (`contracts/serviceContracts.contractJoinFor`), which is what keeps `ContractCodeFlow` and `ContractSignatureList` reachable.
  - `classified` → `artifactRenderers[artifact.classification]` called with **exactly** `{ vm, project, systemEnvelope, t }` — `ArtifactRendererProps` (`artifactRenderers.tsx:25-31`). `vm` and `project` come from the container's second read (Task 8 Step 2); `t` is `useTokens()` here. If `vm === undefined` (a planned-no-record activity has no `ConstructionRow`), render the `unavailable` panel instead: a renderer handed no row draws an empty frame that reads as "there is nothing here", which is a different and false claim.
  - `unavailable` → an honest panel under `Activity.ARTIFACT_UNAVAILABLE` carrying `artifact.reason` verbatim.

  Wrap the whole panel in the caller's `CommentProvider` scope; do not introduce a second one. Put `Activity.ARTIFACT_PANEL` on the root.

- [ ] **Step 5: Write `ReviewBody.tsx`** — `ReviewersStrip` → `ArtifactPanel` → `SubmitBar`, threading `allowSendBack` and `approveCopy` from `verbsFor` and `openThreads={openThreadCount(thread)}` from Task 2's `threadAdapter` (nothing else computes that number, so the bar's "Resolve N threads to approve" and the margin's card count can never disagree). The comment margin is `ExperienceChrome`'s, not this component's. When `verbs.sendBack.kind === 'none'` and the type is `projectDesign`, render the `Amend Architecture →` link under `Activity.AMEND_ARCHITECTURE` — a navigation, deliberately not a verb on the bar. The link's target comes from `contracts/routePaths.ts`:
  ```tsx
  import { ACTIVITY_PATH } from '../../contracts/routePaths.ts';
  // …
  <Link data-testid={UI_IDENTIFIERS.Activity.AMEND_ARCHITECTURE} onClick={() => { onNavigate(ACTIVITY_PATH, { activityId: 'architecture' }); }}>
  ```
  A `components/` file may import `contracts/` and may NOT import `routes/` or call `useNavigate` — the navigation itself is an `onNavigate` callback the container supplies.

- [ ] **Step 6: The Project Design M0 body.** For `type === 'projectDesign'`, `ArtifactPanel` renders the REAL Phase-2 views rather than anything new (the prototype's `PlanArtifact.tsx` was a mock of components that already exist): `SdpReviewView` (`components/project/SdpReviewView.tsx:92`, props `{ envelope, pending, readOnly, onCommit, onRejectAll }`) over slot 16, `ActivityListView` (`:80`, `{ envelope }`) over slot 9, `NetworkView` (`:266`, `{ networkEnvelope, activityEnvelope, height?, … }`) over slots 10 × 9. Pass `onRejectAll` a no-op that cannot be reached — with `allowSendBack: false` the bar never offers it — and say so in a comment. The container already reads the slots (Task 8 Step 2's `useProject`); the M0 fixture (Task 6) carries slots 9/10/16.

- [ ] **Step 7: Wire the container's verbs.** Map each `VerbTarget` onto its hook per R12: `useSubmitPhaseDecision` / `useSubmitReviewDecision` / `useSubmitSDPDecision` + `useAdvanceToConstruction` / `useSetReviewCommentStatus` / `useSetProjectReviewCommentStatus` / `useAskQuestions` / `useAcknowledgeStaleBasis` / `useRequestArtifactDraft` / `useBeginConstruction` / `useOverrideActivity`. `CommentMargin` gets `onResolve`/`onReopen` ONLY when `verbs.commentStatus.kind !== 'none'` — omitting them is the existing, documented posture for a surface with no mutation (R2).
  - [ ] **Verify first:** `useSubmitPhaseDecision` returns `UseMutationResult<PhaseDecisionAnswer, PhaseDecisionFailure, SubmitPhaseDecisionVars>` with `mutationKey: phaseDecisionMutationKey(projectId)` so an in-flight decision survives a remount via `useMutationState` — keep that property; the 2 s poll remounts this screen.

- [ ] **Step 8: Gates and commit.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && ASDF_NODEJS_VERSION=lts npx playwright test --project=preview
  ```
  Commit: `feat(activity): the review body, the per-type verb table and the M0 project-design body`.

---

### Task 10: Revisions, the read-only history, and the per-task comment key

**Files:**
- Modify: `webApp/src/containers/ActivityExperienceContainer.tsx`, `webApp/src/components/activity/ReviewBody.tsx`, `DispatchBody.tsx`.
- Add: `webApp/src/components/activity/pendingCommentKey.ts`, `pendingCommentKey.test.ts`.

**Interfaces produced:**
```ts
// components/activity/pendingCommentKey.ts (R10)
/** `activity:<projectId>:<activityId>:<taskId>:<rev>` — the draft slot a reader's unsent comments persist under. */
export function activityCommentKey(input: {
  projectId: string; activityId: string; taskId: string; revision: number;
}): string;
/** The design rails' existing key shape, unchanged and untouched until stage 6. */
export function designCommentKey(projectId: string, kind: string): string;
```

- [ ] **Step 1: Write `pendingCommentKey.ts` + tests.**
  ```ts
  void test('the activity key carries all five segments, in order', () => {
    assert.equal(
      activityCommentKey({ projectId: 'archistrator', activityId: 'C-billing-state-access', taskId: 'designReview', revision: 2 }),
      'activity:archistrator:C-billing-state-access:designReview:2'
    );
  });
  void test('a task with no revisions still keys stably at zero', () => {
    assert.equal(
      activityCommentKey({ projectId: 'p', activityId: 'A', taskId: 'srs', revision: 0 }),
      'activity:p:A:srs:0'
    );
  });
  void test('two revisions of one task are different slots', () => {
    const at = (revision: number): string => activityCommentKey({ projectId: 'p', activityId: 'A', taskId: 'srsReview', revision });
    assert.notEqual(at(1), at(2));
  });
  void test('the design rails’ key shape is untouched', () => {
    // Byte-for-byte what SystemDesignContainer.tsx:170 and
    // ProjectDesignExperience.tsx:204 pass today, so their stored drafts are not
    // orphaned by the new shape (they keep using it until stage 6).
    assert.equal(designCommentKey('archistrator', 'glossary'), 'archistrator:glossary');
  });
  void test('an activity key can never collide with a design key', () => {
    assert.ok(activityCommentKey({ projectId: 'p', activityId: 'A', taskId: 't', revision: 1 }).startsWith('activity:'));
    assert.ok(!designCommentKey('p', 'A').startsWith('activity:'));
  });
  ```
  - Add a comment recording WHY the shape may change safely: `pendingCommentsStore` already stamps every envelope with a `projectVersion` incarnation and refuses one from the future (`loadPending` at `pendingCommentsStore.ts:73-84`), so a key that has never been written simply loads empty — which is correct, not lossy.

- [ ] **Step 2: Call `setActiveKey` on every selection change.** In the container:
  ```ts
  setActiveKey(activityCommentKey({ projectId, activityId, taskId: sel.taskId, revision: sel.revision }), projectVersion);
  ```
  `projectVersion` comes from the project read (`ProjectStateWithGit.version`). `setActiveKey` short-circuits when the key and version are unchanged (`CommentContext.tsx:130-143`), so this is safe on every poll — do not guard it with a `useEffect` dependency dance that `exhaustive-deps` will fight.

- [ ] **Step 3: Wire `RevisionSelect` into both bodies.** One `onRevision` handler in the container: `navigate({ search: (prev) => ({ ...prev, rev: n }) })`. Node clicks come from `LifecycleGraph`'s `onSelect(nodeId, revision, picked)`: `picked === false` means a plain click, which per `revisionOnNavigate` carries the revision across a draft↔review pair and otherwise opens the target's latest; `picked === true` is an explicit menu choice and is always honoured. Both write `?task` and `?rev`.

- [ ] **Step 4: The non-latest read-only mode (R1).** When `isHistorical(nodes, sel)`:
  - Render `activityCopy.historyBanner(sel.revision, latest)` under `Activity.HISTORY_BANNER` with `role="status"`, and a **Back to latest** control under `Activity.BACK_TO_LATEST` that clears `?rev`.
  - Render `CommentProvider` with `enabled={false}` (its whole affordance set goes quiet) and `CommentMargin` with `expandResolved` (Task 5) — the decisions ARE the history.
  - Render **no** `SubmitBar` at all.
  - Render the artifact panel with `activityCopy.HISTORY_ARTIFACT_CAPTION` above it. Do NOT try to read the artifact as of `subjectRef.ref`: there is no op (GAP-5), and a caption that says so is honest where a silently-current artifact is not.
  - The read-only branch must render zero focusable ghosts and zero orphaned ARIA (house rule; it is what Task 5 Step 1 just fixed one layer down).

- [ ] **Step 5: Gates and commit.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && ASDF_NODEJS_VERSION=lts npx playwright test --project=preview
  ```
  Commit: `feat(activity): revision selection, the read-only history and a per-task comment draft key`.

---

### Task 11: The Plan screen — LIST, GRAPH, TASKS

§7.3. The TASKS lens survives AS IS (spec, binding) — which means its whole feeding pipeline must move, not be rewritten. That pipeline is the largest single piece of `ConstructionConsole.tsx`; **port it, do not reimplement it.**

**Files:**
- Modify: `webApp/src/containers/PlanContainer.tsx` (fill the Task-7 stub), `webApp/src/routes/Plan.tsx`.
- Add: `webApp/src/components/activity/PlanList.tsx`, `PlanGraph.tsx`, `PlanTile.tsx`.
- Add: `webApp/src/components/activity/planTiles.ts`, `planTiles.test.ts` (project read → `PlanTileInput[]` + the LIST order).
- Move: `webApp/src/components/construction/graph/graphViewport.ts` (+ its test) → `webApp/src/components/activity/graphViewport.ts`; `rowGutter.ts` (+ its test) → `webApp/src/components/activity/rowGutter.ts`. **Both have ZERO imports (verified)** — the move is a `git mv` plus import-path fixes in the new consumers.
- Modify: `webApp/src/routes/HomeBase.tsx` (phase cards → one "Open plan" card).

**Interfaces produced:**
```ts
// components/activity/planTiles.ts
export interface PlanActivity {
  id: string;
  title: string;
  row: PlanRow | undefined;          // planRowFor; undefined ⇒ listed, not drawn
  calls: readonly string[];          // its dependencies, from slot 10
  lifecycle: readonly LifecycleNode[];   // miniLifecycleFromRow
  onCriticalPath: boolean;
  effortDays: number;
}
/**
 * Table 11-1 order: the three design activities, M0, then the build stack.
 *
 * `rows` is `useProject(projectId).data.constructionRows` — `ConstructionRows =
 * Record<string, ConstructionRow>` (contracts/types.ts:887, field at :1159),
 * already ordinal-decoded and camel-cased by `contracts/wire.ts:479
 * mapConstructionRow`. The raw `SystemDesignActivityConstructionStatus` (with
 * `Type: 0` and `Phases[].Completed`) never reaches `components/`.
 */
export function planActivitiesFrom(input: {
  activityList: ProjectArtifactModelEnvelope | undefined;
  network: ProjectArtifactModelEnvelope | undefined;
  rows: Readonly<Record<string, ConstructionRow>>;
}): PlanActivity[];
/** The tiles the GRAPH draws (those with a row) and the ids it cannot place. */
export function planTilesFrom(activities: readonly PlanActivity[]): {
  tiles: PlanTileInput[]; unplaced: string[];
};
export const MILESTONE_ID = 'M0';
```

- [ ] **Step 1: Move `graphViewport.ts` and `rowGutter.ts`.**
  ```bash
  git mv webApp/src/components/construction/graph/graphViewport.ts webApp/src/components/activity/graphViewport.ts
  git mv webApp/src/components/construction/graph/graphViewport.test.ts webApp/src/components/activity/graphViewport.test.ts
  git mv webApp/src/components/construction/graph/rowGutter.ts webApp/src/components/activity/rowGutter.ts
  git mv webApp/src/components/construction/graph/rowGutter.test.ts webApp/src/components/activity/rowGutter.test.ts
  grep -rn "graph/graphViewport\|graph/rowGutter" webApp/src uitests/tests   # must be empty afterwards
  ```
  Keep their contents byte-identical apart from the header sentence naming their new home; their tests move with them unchanged. These carry `VIEWPORT_STORE_LIMIT = 16`, `graphSignatureOf`, `loadGraphViewport`/`saveGraphViewport` (module memory keyed by a CONTENT signature, never by status — a cascade completing an activity must not reset the reader's viewport) and `GUTTER_PX = 76` / `gutterWidthFor` / `rowGutterLabels` / `gutterLabelFontPx` (the pinned HTML gutter, at a fixed on-screen size, because canvas nodes were unreadable at fit).

- [ ] **Step 2: Write `planTiles.ts` + tests.** LIST order is Table 11-1: `requirements`, `architecture`, `projectDesign`, the M0 divider, then the build stack in `STACK_ROWS` order with the side lane last. `calls` comes from slot 10's `dependencies[]` (`{ activity, dependsOn[] }` — verified shape), M0 included: `R-github ← [M0]` is exactly what `layoutPlanGraph`'s milestone fan-out assumes. `onCriticalPath` from slot 10's `criticalPath[]` (15 entries live). Tests run against the captured plan fixture (Task 6 Step 3), so they are about the real 32-activity plan:
  ```ts
  /// <reference types="node" />
  import { test } from 'node:test';
  import assert from 'node:assert/strict';
  import { readFileSync } from 'node:fs';
  import { MILESTONE_ID, planActivitiesFrom, planTilesFrom } from './planTiles.ts';
  import { mapConstructionRow } from '../../contracts/wire.ts';

  // The fixture is the wire payload; decode it exactly as useProject does, so the
  // test exercises the same ConstructionRow the screen gets.
  function live(): ReturnType<typeof planActivitiesFrom> {
    const url = new URL('../../../../uitests/preview-fixtures/web-client/plan/list.json', import.meta.url);
    const doc = JSON.parse(readFileSync(url, 'utf8')) as { ops: { systemDesignGetProject: { result: Record<string, never> } } };
    const project = doc.ops.systemDesignGetProject.result as unknown as {
      activityExecution: Record<string, unknown>;
      Slots: { kind: string; model: unknown }[];
    };
    const rows = Object.fromEntries(
      Object.entries(project.activityExecution).map(([id, raw]) => [id, mapConstructionRow(raw as never)])
    );
    const slot = (kind: string): unknown => project.Slots.find((s) => s.kind === kind)?.model;
    return planActivitiesFrom({
      activityList: slot('activityList') as never,
      network: slot('network') as never,
      rows,
    });
  }

  void test('the list opens with the three design activities in Table 11-1 order, then M0', () => {
    const ids = live().map((a) => a.id);
    assert.deepEqual(ids.slice(0, 3), ['requirements', 'architecture', 'projectDesign']);
    assert.ok(ids.indexOf('R-github') > ids.indexOf('projectDesign'), 'the build stack follows the front end');
    assert.equal(ids.at(-1), 'N-IT', 'system testing closes the list');
  });

  void test('every activity of the committed list appears, placed or not', () => {
    const activities = live();
    assert.equal(activities.length, 32, 'the repo’s own plan has 32 activities');
    const { tiles, unplaced } = planTilesFrom(activities);
    assert.equal(tiles.length + unplaced.length, activities.length, 'nothing is dropped on the floor');
  });

  void test('M0 gates the build roots and nothing in the front end', () => {
    const byId = new Map(live().map((a) => [a.id, a]));
    assert.deepEqual(byId.get('R-github')?.calls, [MILESTONE_ID]);
    assert.deepEqual(byId.get('N-STP')?.calls, [MILESTONE_ID]);
    assert.deepEqual(byId.get('requirements')?.calls, [], 'the first activity depends on nothing');
    assert.deepEqual(byId.get('architecture')?.calls, ['requirements']);
    assert.deepEqual(byId.get('projectDesign')?.calls, ['architecture']);
  });

  void test('an activity with no build-order row is reported as unplaced, not dropped', () => {
    const activities = [
      ...live(),
      { id: 'X-orphan', title: 'Orphan', row: undefined, calls: [], lifecycle: [], onCriticalPath: false, effortDays: 5 },
    ];
    const { tiles, unplaced } = planTilesFrom(activities);
    assert.ok(!tiles.some((t) => t.id === 'X-orphan'));
    assert.deepEqual(unplaced, ['X-orphan']);
  });

  void test('every row carries a mini lifecycle, and none of them claims a revision', () => {
    for (const a of live()) {
      for (const n of a.lifecycle) assert.equal(n.revisions.length, 0, `${a.id}/${n.id}`);
    }
  });
  ```

- [ ] **Step 3: Write `PlanList.tsx`.** One row per activity: title, id, state, `LifecycleGraphMini` (nodes from `planActivitiesFrom`, label from `miniLifecycleLabel`), the critical-path mark, and the M0 divider row under `Plan.M0_DIVIDER`. Each row is a button navigating to `ACTIVITY_PATH`. `data-testid={UI_IDENTIFIERS.Plan.row(id)}`. Port the layout from `webApp/proto/PlanPage.tsx` (209 L).

- [ ] **Step 4: Write `PlanGraph.tsx` + `PlanTile.tsx`.** `FlowCanvas` (`components/flow/flowShared.tsx:102`) with custom node types, fed by `layoutPlanGraph(tiles, MILESTONE_ID)`. Carry over, from the old lens, exactly these four behaviours and no more:
  - **Row gutter** — the pinned HTML gutter (`rowGutter.ts`, moved in Step 1), synced to the viewport y, `data-testid={UI_IDENTIFIERS.Plan.gutterRow(row)}`.
  - **Hover focus** — `planFocusFor` (transitive, Task 3), with `MUTED_OPACITY` from `components/flow/flowLayout.ts` for the dimmed set.
  - **Critical-path weight** — the 3 px lane left edge, on the LANE, never on a card or an edge (the architect's Q2 ruling; `graphPresentation`'s `CRITICAL_PATH_TOKEN` idiom), plus an always-visible numeral (WCAG 1.4.1 — colour is never the sole carrier).
  - **Viewport memory** — `loadGraphViewport`/`saveGraphViewport` keyed by `graphSignatureOf(projectId, ids)`, passed as `defaultViewport` / `onMoveEnd` (`FlowCanvas` supports both, verified). This is what makes §7.3's "✕ returns with viewport and selection preserved".
  Below the canvas, when `unplaced.length > 0`, render `planCopy.UNPLACED_TILE_REASON` under `Plan.UNPLACED_NOTE` naming them — a tile the model cannot place is said out loud, never silently dropped.
  Do NOT port the milestone RIBBON, the hover cards, the lane schedule strip or the graph filter from the old lens; they belong to the per-activity lane view that no longer exists. Say so in the file header.

- [ ] **Step 5: Fill `PlanContainer.tsx` — port the TASKS pipeline.** From `routes/ConstructionConsole.tsx`, move (do not rewrite) the imports and wiring for: `useProject(projectId, consolePollMs(...))`, `narrowProject`, `useConstructionSessions`, `owedWorkFor` / `probeCandidatesFor`, `rankOwed` / `nextOwedAfter`, `decidedFor` / `decisionViewFor` / `gateControlFor` / `observedGateFor`, `decisionActivityOf` / `decisionEntriesFrom` / `pendingDecisionActivities`, `emptyStateCounts` / `reasonSentenceFor` / `shapeFor`, `owedMarksFor`, `waitingActivityIds`, `pendingNoteLineFor`, `computeActivityStatuses`, `contractJoinFor`, `gitFor`, `useBeginConstruction` / `useResumeConstruction` / `useSubmitPhaseDecision` / `useBeginConstructionPending`, `beginControlFor` / `beginHoldFor` / `pausedControlFor` / `consolePollMs` and the rest of `lens/beginControl.ts`, `BeginConfirmDialog`, and `useLensToolbar`. Feed `TasksLens` its existing `TasksLensProps` unchanged — the spec says it survives as is, so its props surface does not move.
  - `onReview` navigates to `ACTIVITY_PATH` with `?task=<the decision's gate>` (R8) instead of selecting into the dead DetailPane.
  - **Do NOT port:** `useGateOccurrences` / `occurrenceKey`, `useLensSelection` (the hook), `ConstructionShell`, `ActivityTreeView`, `ActivityGraphLens`, `DetailPane`, `FocusView`, `toC4View` — all of it dies in Task 13. **`slotStageFromOrdinal` IS ported** — Step 5b needs it to read a slot's stage.
  - `TasksLensProps.selection` still takes a `LensSelection`; the container passes `{}` (nothing is selected on the plan — selection moved to the activity route) or the activity id of the row a decision was just made on. The type survives for this reason (Task 7's Interfaces note).
  - [ ] **Verify first:** `cd webApp && ASDF_NODEJS_VERSION=lts npx tsc --noEmit` after the port and BEFORE deleting anything — two screens feeding `TasksLens` at once is fine and temporary.

- [ ] **Step 5b: Make TASKS list design reviews (§7.3), from a source that exists.** `owedWorkFor` walks every `ConstructionRow` already (`owedWork.ts:228`, no kind filter), but its owed-gate verdict reads `session.stage === 'awaitingApproval'` from `constructionGetSessionState` — which exists only for construction activities. A design activity waiting at a review gate is therefore invisible to it today. **Do not add a probe and do not add an op.** Add a second, purely-derived source in `owedWork.ts`, fed by data `useProject` already returns:

  ```ts
  /**
   * A DESIGN activity's owed decision, derived rather than probed.
   *
   * requirements / architecture / projectDesign are dispatched by the design
   * rails, not the construction pump, so `constructionGetSessionState` never
   * answers for them and `owedFor`'s gate branch cannot see them (it needs a
   * session at `awaitingApproval`). What IS visible, from the same project read
   * the plan already makes, is the artifact slot: a slot at
   * `stage === 'awaitingReview'` is precisely "a draft is staged and a human
   * owes it a verdict". That is the whole condition, and it needs no probe, no
   * new op and no second poll.
   *
   * The lifecycle's phase → artifactKind mapping says WHICH slots belong to the
   * activity (requirements: mission/glossary/volatilities/coreUseCases;
   * architecture: system; projectDesign: sdpReview), and the first such slot
   * awaiting review is the gate the row is sitting at.
   */
  export function designOwedFor(
    row: ConstructionRow,
    slots: readonly ArtifactSlotView[],
    titleFor: ((id: string) => string | undefined) | undefined
  ): OwedItem | 'clear';
  ```
  Wire it into `owedWorkFor` as the branch taken when `row.kind` is one of the three design kinds (before the session branch, which can never fire for them), give `OwedWorkInput` an extra `slots` member, and pass `project.slots` from `PlanContainer`. Tests in `owedWork.test.ts`: a `requirements` row with the glossary slot `awaitingReview` yields one owed item naming the glossary gate; the same row with every slot `committed` yields `clear`; an `architecture` row with the `system` slot `awaitingReview` yields one item; a `projectDesign` row with slot 16 `awaitingReview` yields one item whose gate is `sdpReview`; a construction row is untouched by the new branch (its existing session-based tests still pass unchanged).

- [ ] **Step 6: HomeBase's phase cards become one "Open plan" card.** In `routes/HomeBase.tsx`, delete the `toPhaseCards` memo (`:261`) and the `<PhaseCard>` map (`:315-330`), and render one card linking to `PLAN_PATH` (imported from `contracts/routePaths.ts`) with the project's headline state. **Leave `PHASE1_ORDER`'s use for the artifact table of contents (`:253-258`) exactly as it is** — it is KEPT for the whole stage (R15: `McpSystemDesignContainer.tsx:38` and `uitests/tests/support/testids.ts:26` also read it), so this step touches the cards and nothing else.

- [ ] **Step 7: Gates and commit.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && ASDF_NODEJS_VERSION=lts npx playwright test --project=preview
  ```
  Commit: `feat(plan): the plan screen — list, build-order graph and the surviving tasks lens`.

---

### Task 12: The Playwright suites

The spec's "promoted from `interact.mjs`" has nothing behind it (R14/GAP-1) — this is authored. Select ONLY by `UI_IDENTIFIERS` ids through `TESTID` (`uitests/eslint.config.js` has a `no-inline-testid` rule), and import `test`/`expect` from `../support/dispatchGuard.js`, never from `@playwright/test` (pinned by `tests/meta/suite-rules.spec`).

**Files:**
- Add: `uitests/tests/preview/activity-experience.spec.ts`, `uitests/tests/preview/plan.spec.ts`.
- Add: `uitests/tests/activity-experience-live.spec.ts`.
- Modify: `uitests/tests/support/testids.ts` (add the `Activity`, `Plan` and `ActivityLifecycle` entries).

- [ ] **Step 1: Extend `TESTID`.** Add flat camelCase entries derived from `UI_IDENTIFIERS.Activity`, `.Plan` and `.ActivityLifecycle` — values imported, never retyped (the file's own doc: "a renamed testid should fail ONE import resolution, not silently drift").

- [ ] **Step 2: `preview/activity-experience.spec.ts`.** One assertion per §7.2 promise, each over the fixture that exercises it:
  1. `?screen=activity-experience&state=service-fork-sent-back` opens FULL SCREEN with the eyebrow reading the activity, not a phase.
  2. With no `?task`, the default-task rule lands on the awaiting-human gate (`service-fork-sent-back`'s `designReview`), and with `?task=stp` it lands where the URL says.
  3. Clicking a node opens its LATEST revision (`?rev` absent or equal to the latest).
  4. The revision menu opens on **right-click**, on the active pill's **caret**, and on **Shift+F10** — three separate assertions; all three are shipped affordances and all three must stay.
  5. Choosing a revision in `RevisionSelect` changes `?rev`.
  6. A non-latest revision shows the read-only banner, expands its resolved threads, and shows NO submit bar.
  7. A return arc (`↻N`) is drawn only where a pair has been sent back — present on `service-fork-sent-back`, absent on `done`.
  8. `?screen=activity-experience&state=project-design-m0` has an Approve verb and **no send-back anywhere**, including the overflow.
  9. `?screen=activity-experience&state=review-set-error` shows the engine's refusal, not an empty strip.
  10. `?screen=activity-experience&state=sub-attempts` shows the sub-attempt line; `done` does not.
  11. **§7.5 gap 1:** in the read-only history, a margin card is PLACED (it has a non-zero offset / is not in the unplaced group) — the assertion that proves `ReadOnlyRow`.
  12. **§7.5 gap 2:** on a service contract, a thread anchored to a contract op is PLACED — the assertion that proves `OpRow`.

  **The ✕ assertion lives in `plan.spec.ts`, not here.** An `activity-experience` fixture's `ops` map holds `constructionQueryActivityView` (and, for the M0 state, `systemDesignGetProject`); navigating to `/plan` from inside it would make an unfixtured `systemDesignGetProject` call, which the preview shell answers with the ALARM and an incident — a red test that proves nothing about ✕. A preview fixture is one screen's data, and the round trip spans two. Task 12 Step 3's plan spec owns it, opening an activity from a LIST row (both screens' ops in one fixture) and pressing ✕.

- [ ] **Step 3: `preview/plan.spec.ts`.** Over `?screen=plan&state=list|graph|tasks`: the LIST opens in Table 11-1 order with the M0 divider between `projectDesign` and the first build row; a LIST row opens the activity full screen; the GRAPH draws a tile per placed activity with the row gutter labelled top→down `Resources → Resource Access → Engines → Managers → Clients → System Testing` (no Deployment row — R5); hovering a mid-stack tile lights its full upstream AND downstream chain (assert a tile two hops away is lit); hovering M0 lights every non-front-end tile; a GRAPH tile opens the activity full screen; `?lens=tasks` still renders the TASKS lens, lists at least one DESIGN review row (Task 11 Step 5b), and its decision rows navigate to an activity with `?task=`.
  **Plus the ✕ round trip**, which belongs here because only a plan fixture carries both screens' ops: from `?screen=plan&state=graph`, open a tile, assert the activity screen, press ✕, assert the plan screen again with `?lens=graph` still selected — and assert `incidents(page)` is empty throughout, so a missing op cannot pass as a pass.
  - [ ] **Give the three plan fixtures the activity op they now need.** Add `constructionQueryActivityView` to `plan/graph.json` and `plan/list.json` — one activity's view, reusing the payload from `activity-experience/done.json` — so the round trip answers from fixtures instead of raising the alarm. Note it in each fixture's `note` field.

- [ ] **Step 4: Retarget `preview/preview-shell.spec.ts` — it cannot stay as written.** Its first test drives `?screen=construction&state=resting` and asserts `TESTID.constructionListTree` plus one `construction-list-row-<id>` per activity (`:77-113`); that tree is `ActivityTreeView`, which Task 13 deletes. Its second test (`unclassified-row`) asserts the same rows. **Keep all four of the spec's guarantees** — the real screen draws exactly the fixture's activities; an unfixtured call fails LOUDLY (alarm + incident log); nothing but the static bundle is fetched; a nested preview is refused — and move the first two onto the new plan fixtures:
  - `?screen=plan&state=list`: assert `TESTID.planList` is visible and that `TESTID.planRow(id)` is visible for every id in the fixture's `activityExecution`, with `page.getByTestId(/^plan-row-/)` counting exactly that many. Same shape as today, new ids.
  - the unclassified-row guarantee moves with it: a row the classifier refused to type still appears in the LIST (the committed list decides what exists) and draws no mini lifecycle — which is exactly what `miniLifecycleFromRow` returns `[]` for (Task 3 Step 4). Add an `unclassified` state to `plan/` carrying the same `C-usage-access` row `construction/unclassified-row.json` has.
  - the network, incident and nested-preview tests are screen-agnostic and change only the `screen`/`state` they open.
  Delete the `construction/` fixture states whose only consumer was the retargeted assertions, and keep the rest until Task 13 Step 5 rules on them.

- [ ] **Step 5: The live smoke.** `uitests/tests/activity-experience-live.spec.ts` — `test.skip()` unless the server answers (copy the `requireServer` pattern the existing non-preview specs use), then exactly two assertions: `/project/archistrator/plan` renders the plan screen with at least one row, and opening one activity renders the lifecycle graph. Two, not more: this is a smoke test against shared state that other sessions mutate.

- [ ] **Step 6: Save one screenshot per new screen** to the session SDD workspace (plan LIST, plan GRAPH, the activity dispatch body, the activity review body, the M0 body, the read-only history) — the founder's per-UI-change review at the end of the stage reads these.

- [ ] **Step 7: Gates and commit.**
  ```bash
  cd uitests && ASDF_NODEJS_VERSION=lts npx tsc --noEmit && ASDF_NODEJS_VERSION=lts npx eslint . \
    && ASDF_NODEJS_VERSION=lts npx playwright test --project=preview
  ```
  Commit: `test(uitests): preview suites for the activity experience and the plan, plus a live smoke`.

---

### Task 13: The §7.4 deletions

**Only now** — the new routes are green, the redirects exist and the suites pass. One commit per cluster, each with the grep that proves zero surviving importers. Never a bulk `rm`.

> **THE ONE-SENTENCE RULE FOR THIS TASK.** `detail/bodies/*` and `list/*` are KEPT except the files this table names, whose every importer is deleted with them; `SystemDesignView`'s step ladder, `SlimSpine.tsx`, `SpineStep` and `PHASE1_ORDER` are all KEPT because the out-of-scope `containers/McpSystemDesignContainer.tsx` (`:38`, `:55`, `:215`, `:419`) consumes them, and their deletion moves to stage 6. **If `tsc -b` goes red after a `git rm`, the answer is never to improvise another deletion — it is that the file was not deletable. Put it back and record it.**

**How every verdict below was measured.** Import-edge greps in this worktree, not word mentions:
```bash
cd webApp/src && grep -rn "from '.*<name>'" --include='*.ts' --include='*.tsx' . | grep -v "<the file's own directory>"
```
A comment that names a file is not an importer (three of the first draft's wrong rows were comments).

#### Cluster A — the construction console

| File | LOC | Verdict | Evidence |
|---|---|---|---|
| `routes/ConstructionConsole.tsx` | 1425 | **DELETE** | `router.tsx` only; Task 7 already replaced its component with a redirect |
| `containers/ConstructionEpisodeBodyContainer.tsx` | 117 | **DELETE** | `ConstructionConsole.tsx:154` only |
| `components/construction/lens/ConstructionShell.tsx` | 741 | **DELETE** | `ConstructionConsole` only |
| `components/construction/lens/BeginConfirmDialog.tsx`, `beginControl.ts` (+test), `toolbarForLens.ts` | — | **KEEP** | `PlanContainer` uses them (Task 11 Step 5) |
| `components/construction/lens/useLensSelection.ts` | 429 | **REDUCE** | delete `parseLensSearch`, `serializeLensSearch`, `artifactKeptFor`, `LensState`, `ArtifactViewState`, `ARTIFACT_VIEW_IDS`, `ArtifactViewId`, `LensSelectionApi`, `useLensSelection` + `useLensSelection.test.ts` and `artifactViewSearch.test.ts`, once their consumers (`ConstructionConsole:147,398`, `ConstructionShell:76`, `DetailPane:102-105,367`, `UnknownBody:47,99`, `ArtifactPlacementView:23`) are gone. **KEEP** `LENS_IDS`, `LensId`, `validateLensSearch`, `LensSearchParams`, and **`LensSelection`** (narrow it to `{ activityId?: string; task?: string }` — the members `TasksLens.tsx:99,348` and `decisionFlow.ts:346` read), plus the whole toolbar half (`SCOPE_IDS` … `useLensToolbar`), which `list/activityScope.ts:48` and `PlanContainer` need |
| `components/construction/list/ActivityTreeView.tsx` | 1903 | **DELETE** | `ConstructionShell` only. **Its re-export at `:184`** (`chipFor`, `floatPresentation`, `RowState` from `activityRowPresentation.ts`) dies with it — confirm nothing imports those THROUGH it before removing |
| **all other `components/construction/list/*`** — `activityTree.ts`, `activityRowPresentation.ts`, `activityMeta.ts`, `activityScope.ts`, `listEmptyState.ts`, `observedOnly.ts`, `pendingNotes.ts`, `pendingResume.ts`, `pendingResumeFixtures.ts`, `searchExpansion.ts` + their tests | — | **KEEP ALL** | `tasks/TasksLens.tsx:48` → `activityRowPresentation.ts`; `activityRowPresentation.ts:33` → `activityTree.ts`; `activityScope.ts:49,50` → both; `ConstructionConsole`'s ported pipeline uses `pendingResume.waitingActivityIds` and `pendingNotes.pendingNoteLineFor`. The first draft's claim that `list/` dies was wrong |
| `components/construction/graph/ActivityGraphLens.tsx` | 624 | **DELETE** | `ConstructionShell` only |
| `graph/{activityGraphModel, activityGraphLayout, graphCardPresentation, graphEdges, graphNodeTypes, graphPresentation, laneSchedule, laneSpine, m0Gate, segmentCode, gateRibbon, graphFilter}.ts` + tests | — | **DELETE** | every importer is inside `graph/`, or is `ConstructionConsole.tsx` (`graphFilter`, `graphPresentation`, `m0Gate`), which goes first |
| `graph/{GraphNodes, GraphStrips, GraphRowGutter, GraphDeepLinkFrame}.tsx` | — | **DELETE** | no importer outside `graph/` |
| `graph/hoverCard.ts`, `hoverCardPlacement.ts` (+ tests) | — | **DELETE, together with two cross-assertions** | the only surviving importers are `list/pendingResume.test.ts:25` (`laneChipFor`) and `list/waitsOnLine.test.ts:10` (`segmentLineFor`) — cross-checks that the LIST's wording matched the GRAPH's hover card. The hover card is gone, so the cross-check has no second party: **remove those two imports and the assertions that use them, keep the rest of both test files**, and name the removed assertions in the commit message |
| `graph/graphViewport.ts`, `rowGutter.ts` (+ tests) | — | **ALREADY MOVED** in Task 11 Step 1 | zero own imports; every importer deleted above |
| `components/construction/detail/DetailPane.tsx` (+ `DetailPane.test.ts`, `decisionActions.test.ts`, `selectionSummary.test.ts`) | 1925 | **DELETE** | `ConstructionConsole` only |
| `components/construction/detail/detailPaneState.ts` | — | **KEEP (reduce if anything is orphaned)** | surviving importers: `tasks/TasksLens.tsx:49` (`taskDetailStateFill`), `tasks/tasksLensCopy.test.ts:41`, `bodies/taskBriefing.ts:43` (`TaskDetailState`). Delete only exports the greps prove unreferenced |

#### Cluster A′ — `detail/bodies/`, per file (26 files; this is the cluster the first draft left unverdicted)

| File | Verdict | Surviving importer? |
|---|---|---|
| `bodyDispatch.ts` | **DELETE** | `DetailPane.tsx:136`, `bodies/ArtifactBody.tsx:49`, `bodies/artifactPlacement.ts:26`, `bodies/taskBriefing.test.ts:38`, `bodies/artifactPlacement.test.ts:29`, `bodies/reviewVerdict.test.ts:21` — all deleted here. (`FocusView` does NOT import it; the first draft's evidence row was wrong.) Its mapping was PORTED into `taskArtifactFor.ts` in Task 9 Step 2 |
| `ArtifactBody.tsx` | **DELETE** | `DetailPane.tsx:156`, `bodies/ReviewBody.tsx:48`. `artifactRenderers.tsx` names it only in COMMENTS (`:17`, `:34`) — update those two comments to name `components/activity/ArtifactPanel.tsx` instead |
| `artifactPlacement.ts` (+ `artifactPlacement.test.ts`) | **DELETE** | `DetailPane.tsx`, `ContractSummaryCard`, `ArtifactPlacementView`, `ComponentTestPlanBody`, `ArtifactFrame` — all deleted here |
| `ArtifactPlacementView.tsx` | **DELETE** | `DetailPane.tsx` only |
| `ArtifactFrame.tsx` | **DELETE** | `ContractSummaryCard`, `ArtifactPlacementView`, `ComponentTestPlanBody` — all deleted here |
| `ContractSummaryCard.tsx` | **DELETE** | `ArtifactPlacementView` only |
| `ComponentTestPlanBody.tsx` | **DELETE** | `ArtifactPlacementView` only |
| `componentCoverage.ts` (+ test) | **DELETE** | `ComponentTestPlanBody` only |
| `UnknownBody.tsx` | **DELETE** | `DetailPane.tsx`, `ArtifactBody.tsx` |
| `AbsentBody.tsx` | **DELETE** | `DetailPane.tsx` only |
| `ProvenanceNote.tsx` | **DELETE** | `DetailPane.tsx` only |
| `ReviewBody.tsx` (the construction one, 305 L) | **DELETE** | `DetailPane.tsx` only. Not to be confused with the NEW `components/activity/ReviewBody.tsx` |
| `reviewVerdict.ts` (+ test) | **DELETE** | `bodies/ReviewBody.tsx` only |
| `FocusView.tsx` | **DELETE** | `DetailPane.tsx` only |
| `EpisodeBody.tsx` | **DELETE** | `containers/ConstructionEpisodeBodyContainer.tsx` only, itself deleted in Cluster A |
| `SubagentGantt.tsx` | **DELETE** | `EpisodeBody.tsx` only |
| `spanGeometry.ts` | **KEEP** | `SubagentGantt` goes, but `episodeAttribution.test.ts:11` still imports `formatSpanDuration` / `ganttBarsFor`, and that test covers a KEPT module |
| `episodeAttribution.ts` (+ test) | **KEEP** | `components/episodes/EpisodesPanel.tsx` — a surviving surface |
| `taskBriefing.ts` (+ `taskBriefing.test.ts`, `taskBriefingLens.test.ts`, `unknownTitle.test.ts`) | **KEEP** | FOUR surviving importers in `tasks/`: `tasksLensCopy.ts`, `decisionFlow.ts`, `owedWork.ts`, `owedRanking.ts`. **Reduce, do not delete:** its own `import { … } from './bodyDispatch.ts'` in the three TESTS goes with `bodyDispatch`, so trim those test files to what survives |
| `components/construction/tasks/*` | **KEEP ALL** | the TASKS lens survives (spec §7.3) |

#### Cluster B — the design rails

| File | LOC | Verdict | Evidence |
|---|---|---|---|
| `routes/DesignExperience.tsx` | 36 | **DELETE** | `router.tsx` only (now a redirect) |
| `containers/SystemDesignContainer.tsx` | 453 | **DELETE** | `DesignExperience.tsx` only (every other hit is a comment) |
| `routes/ProjectDesignExperience.tsx` | 952 | **DELETE** | `DesignExperience.tsx:21` re-export only |
| `components/design/SystemDesignView.tsx` | 827 | **KEEP WHOLE — deletion deferred to stage 6** | `containers/McpSystemDesignContainer.tsx:55` imports `{ SystemDesignView, type SpineStep }`, `:82` defines `buildSpine`, `:215` calls it, `:419` passes `spine={spine}`. The step ladder IS what the out-of-scope consumer needs. Touch nothing in this file |
| `components/design/SlimSpine.tsx` | 164 | **KEEP** | `SystemDesignView.tsx:64` (kept above). `ProjectDesignExperience.tsx:62` goes, but one importer remains |
| `ApproveFaultBanner` (exported from `SystemDesignView.tsx:811`) | — | **KEEP IN PLACE, do not move** | its only importer outside the file is `ProjectDesignExperience.tsx:67`, which is deleted; `SystemDesignView` stays, so the export stays where it is. (The first draft said `:69` and said to move it — both wrong) |
| `contracts/methodMetadata.ts` `PHASE1_ORDER` | — | **KEEP** | `contracts/adapters.ts:46`, `routes/HomeBase.tsx:49`, `containers/McpSystemDesignContainer.tsx:38`, `uitests/tests/support/testids.ts:26`. Deferred to stage 6 with the MCP container |
| `contracts/methodMetadata.ts` `PHASE2_ORDER` | — | **DELETE** | `contracts/adapters.ts`, `graph/m0Gate.ts:24`, `routes/ProjectDesignExperience.tsx:34` — all three deleted here |

#### Cluster C — the home cards

| File | Verdict | Evidence |
|---|---|---|
| `contracts/adapters.ts` `toPhaseCards` (`:120-150`) | **DELETE** | `HomeBase.tsx` only; Task 11 Step 6 removed the call |
| `components/HomeBaseParts.tsx` `PhaseCard` | **DELETE the export** | `HomeBase.tsx` only — grep `PhaseCard` across `webApp/src` and `uitests/tests` and paste the empty result into the commit message |
| `hooks/gateOccurrences.ts` + `hooks/useGateOccurrences.ts` | **DELETE** (both, + their tests) | `ConstructionConsole.tsx:45-46` only. The `decisionFlow.ts` and `readRequestTimes.ts` hits are COMMENTS — update their wording, do not treat them as importers |

#### Cluster D — the uitests

26 `construction-*.spec.ts` plus `design-experience.spec.ts`, `design-experience-regressions.spec.ts`, `close-and-no-render.spec.ts`, `gate-sendback-fault.spec.ts`, `status-decides-outcome.spec.ts` and `deployment-lens.spec.ts` drive the deleted screens. Judge per file and write the verdict into the commit message: a spec asserting a DetailPane geometry or a graph lane goes with it; a spec asserting an owed decision routes correctly is RETARGETED at `/plan?lens=tasks`, because TASKS survives. `tests/preview/preview-shell.spec.ts` was already retargeted in Task 12 Step 4 and must be green here.

- [ ] **Step 1: Delete Cluster A, then A′, in that order.** A′ only becomes deletable once `DetailPane.tsx` is gone. Before each `git rm`, run the import-edge grep for that basename and PASTE THE RESULT INTO THE COMMIT MESSAGE. After each file, `cd webApp && ASDF_NODEJS_VERSION=lts npx tsc --noEmit`. A red typecheck means the row was wrong — restore the file, correct the table in this plan, and say so.

- [ ] **Step 2: Reduce `useLensSelection.ts` and `detailPaneState.ts`** exactly as the Cluster A rows say. `LensSelection` is narrowed, NOT deleted; `validateLensSearch` and the toolbar half stay.

- [ ] **Step 3: Delete Cluster B.** Delete the three route/container files and `PHASE2_ORDER`. **Do not touch `SystemDesignView.tsx`, `SlimSpine.tsx`, `SpineStep`, `ApproveFaultBanner` or `PHASE1_ORDER`** — all five are kept for `McpSystemDesignContainer` and deferred to stage 6 (Task 14 records the deferral in the spec and the earmark file). Verify with:
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npx tsc --noEmit && ASDF_NODEJS_VERSION=lts npm run build:mcp
  ```
  The MCP build is the gate that proves the widget still compiles.

- [ ] **Step 4: Delete Cluster C**, updating the two stale comments rather than deleting them.

- [ ] **Step 5: Delete or retarget Cluster D**, then:
  ```bash
  cd uitests && ASDF_NODEJS_VERSION=lts npx tsc --noEmit && ASDF_NODEJS_VERSION=lts npx eslint . \
    && ASDF_NODEJS_VERSION=lts npx playwright test --project=preview
  ```
  Also delete the `uitests/preview-fixtures/web-client/construction/` states whose only consumer was a deleted spec — `tests/preview/preview-shell.spec.ts` reads that directory directly, so check it first (Task 12 Step 4 already moved its assertions onto `plan/`).

- [ ] **Step 6: Final count, and one commit per cluster.**
  ```bash
  git diff --stat HEAD~4 -- webApp/src uitests    # the stage's net deletion
  ```
  Commits: `refactor(construction): delete the console, its lenses and the detail pane`; `refactor(design): delete the system- and project-design rails`; `refactor(home): one plan card replaces the phase cards`; `test(uitests): retire the specs whose screens are gone`.

---

### Task 14: Docs, earmarks, the stale server comment, and the prototype's removal

**Files:**
- Modify: `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` (§7, §9).
- Modify: `server/internal/manager/construction/constructionmanager.go` (lines 785–787, COMMENT ONLY).
- Add: `docs/bugs/2026-09-24-stage5-webapp-earmarks.md`.
- Delete (untracked, `rm -rf`, never a git verb): `webApp/proto/`, `webApp/proto.html`.

- [ ] **Step 1: Amend the spec where planning proved it wrong.** Each correction with its evidence:
  - **§9** "Pure layout/geometry: node tests (**20** exist)" → **60** (geometry 20, layout 11, types 10, planGraphLayout 12, lifecycles.gen 7 — measured in Task 1 Step 2).
  - **§9** "Playwright interaction suite from the prototype (`interact.mjs`) promoted into `uitests`" → the suite was **authored** in stage 5; `interact.mjs` exists in no branch and no working tree (`git log --all --diff-filter=A -- '*interact.mjs'` is empty) and the `activity-experience-proto` branch carries no `webApp/proto*` file at all.
  - **§7.1** — the `PlanRow` vocabulary has no `deployment` row (R5): the live model has no deployment layer.
  - **§7.2** — "non-latest revision → artifact as of that revision" is **not** shipped in stage 5 (R1): no op takes a ref. Record what IS shipped (banner, thread, verdicts, current artifact + caption) and point at stage 4's `QueryActivityView`.
  - **§7.2/§7.3** — the plan's mini lifecycles are DERIVED from the project read, not read per activity (R3), and the build-order row is derived client-side (R4) because `LayerForActivity` gives a componentless activity `("", "projectWide")`.
  - **§7.4** — three corrections. (a) Add `laneSpine.ts` to the deleted list and `graphViewport.ts` / `rowGutter.ts` to a new MOVED list. (b) **`SystemDesignView`'s step ladder, `SlimSpine.tsx` and `PHASE1_ORDER` are NOT deleted in stage 5** — the out-of-scope MCP widget (`containers/McpSystemDesignContainer.tsx:38,55,215,419`) consumes all three, so their deletion is deferred to stage 6 with that container. §7.4 names all three as stage-5 deletions today and is wrong. (c) `detail/bodies/` and `list/` are not deleted wholesale: `taskBriefing.ts`, `episodeAttribution.ts`, `spanGeometry.ts` and every `list/*` but `ActivityTreeView.tsx` survive because the TASKS lens and the episodes panel reach them.

- [ ] **Step 2: Fix the stale comment (R18/GAP-14).** `server/internal/manager/construction/constructionmanager.go:785-787` currently reads:
  ```go
  // An id the committed activity list does not
  // hold is NotFound — today that includes the requirements, architecture and
  // projectDesign activities, which become real in stage 2.
  ```
  Replace the second and third lines with:
  ```go
  // An id the committed activity list does not
  // hold is NotFound. Since stage 2 that list HOLDS requirements, architecture and
  // projectDesign (slot 9 opens with all three), so a design activity reads like any
  // other — ClassifyType tolerates ErrDesignActivityNotDispatchable for exactly this
  // reason, and LifecycleKeyFor resolves all three against method-assets.
  ```
  Then:
  ```bash
  cd server && GOWORK=off make lint && GOWORK=off make gen-lifecycles-check && GOWORK=off go build ./...
  ```

- [ ] **Step 3: Write the earmark file**, in the register of `docs/bugs/2026-09-23-stage1-model-wave-earmarks.md`:
  - **Deferred to stage 4 (server):** the **artifact-as-of-revision read** (R1/GAP-5 — `ConstructionReviewSubjectRef.ref` names a git object no HTTP/MCP op exposes; `QueryActivityView` should take a revision and return the artifact as of its `stagedRef`). The **construction comment lifecycle** (R2/GAP-6 — no `SetReviewCommentStatus`/`AskQuestions` on `constructionManager`; the unified rail is read-unified and write-asymmetric until `SubmitReviewDecision`). The **batched plan read** (R3/GAP-7 — `QueryProjectView`, which would let the plan stop deriving mini lifecycles). Optionally, widening `LayerForActivity` so the build-order row is a server fact (R4/GAP-4).
  - **The stage-4 hook re-key list:** every call site the containers make through `useSubmitPhaseDecision`, `useSubmitReviewDecision`, `useSubmitSDPDecision`, `useAdvanceToConstruction`, `useSetReviewCommentStatus`, `useSetProjectReviewCommentStatus`, `useAskQuestions`, `useAcknowledgeStaleBasis`, `useRequestArtifactDraft`, `useBeginConstruction`, `useOverrideActivity` collapses onto `deliveryManager`'s 12 ops. `containers/activityVerbs.ts` is the ONE table that changes — name it explicitly as the seam.
  - **R17's seven:** `service`, `deployment`, `documentation`, `integration`, `testing:harness`, `testing:perf`, `testing:qaProcess` have no artifact renderer; `taskArtifactFor` returns `unavailable` with a copy line for each. List them.
  - **GAP-3 leftovers:** any wire member still exercised by no fixture after Task 6.
  - **Stage 6, carried out of Task 13 with their evidence:** delete `containers/McpSystemDesignContainer.tsx` and, with it, `components/design/SystemDesignView.tsx` (827 L incl. its step ladder and `ApproveFaultBanner`), `components/design/SlimSpine.tsx` (164 L) and `contracts/methodMetadata.ts`'s `PHASE1_ORDER` — all four are alive in stage 5 ONLY because that widget imports them (`:38`, `:55`, `:215`, `:419`); `HomeBase.tsx:49` and `uitests/tests/support/testids.ts:26` also read `PHASE1_ORDER` and must move with it. Also retire the design rails' `<projectId>:<kind>` pending-comment keys (`SystemDesignContainer` is gone by then, so only the MCP path still writes one).
  - **Post-merge on main only:** `cd uitests && npm run regen:core-use-cases-fixture` — the regen reads the committed `main` branch (`server/cmd/gen-uitests-fixtures/main.go:96`) and cannot run in a worktree.
  - **Deploy note, DO NOT DEPLOY HERE:** this stage is SPA-only and adds no server contract, so it needs no drain of its own — but it must not ship ahead of stages 3/4's server, because the screens read `ActivityView.thread`/`verdicts`/`subjectRef`, which only a stage-3 server fills.

- [ ] **Step 4: Remove the prototype.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage5
  git status --short -- webApp/proto webApp/proto.html   # must show ?? only — NEVER tracked
  rm -rf webApp/proto webApp/proto.html
  git status --short -- webApp/                           # must now be clean of them
  ```
  **Never `git clean`, `git restore` or `git checkout --`** anywhere near this (memory rule: a subagent's tree restore has destroyed untracked work in this repo before). The reusable half was committed in Task 1; what is being removed is the throwaway harness (`main.tsx`, `ProtoApp.tsx`, `useHashRoute.ts`, `fixtures.ts`, the four mock artifact modules, `proto.html`, `proto/tsconfig.json`).

- [ ] **Step 5: Final full gate, then commit.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && ASDF_NODEJS_VERSION=lts npx tsc --noEmit && ASDF_NODEJS_VERSION=lts npx eslint . \
    && ASDF_NODEJS_VERSION=lts npx playwright test --project=preview
  cd ../server && GOWORK=off make lint
  ```
  Commit: `docs: stage-5 spec corrections, earmarks, and the stale QueryActivityView comment`.

---

## Self-review

### Spec §7.1 (components) → task

| §7.1 item | Task |
|---|---|
| `lifecycleGraphLayout.ts` / `lifecycleGraphGeometry.ts` — tested pure layout, longest-path columns, git-style lanes | 1 (landed as built; 31 of the 60 node tests are theirs) |
| `LifecycleGraph.tsx` — SVG, icons by kind, state by fill/ring, directed rails, return arc with `↻N` only after a send-back, phase labels, lane labels, active pill + caret, click = latest, right-click / caret / ContextMenu / Shift+F10 = revision menu | 1 (component), 12 Step 2 assertions 3–4, 7 (the three menu affordances and the return arc are separately asserted) |
| `LifecycleGraphMini.tsx` — thumbnail for plan rows and graph tiles | 1 (component), 3 (its nodes, via `miniLifecycleFromRow`), 11 (both call sites) |
| `RevisionSelect` — identical on dispatch and review, selection carries across a draft→review pair, URL param `rev` | 1 (component), 10 Step 3 (`revisionOnNavigate` + `?rev`), 12 Step 2 assertion 5 |

### Spec §7.2 (Activity Experience) → task

| §7.2 promise | Task |
|---|---|
| Route `/project/$projectId/activity/$activityId?task=&rev=` | 7 Step 2 |
| `ExperienceChrome` eyebrow `R-BSA · SERVICE` / `ACTIVITY 2 · ARCHITECTURE` | 1 (the prop), 2 (`eyebrowFor` + test), 8 Step 5 |
| `spine = <LifecycleGraph>` | 8 Step 5 |
| **dispatch** body: ONE episode (the selected revision's), header facts (worker class · command · exit criterion), turn timeline, generating scene while running | 2 (`taskFactsFor`), 8 Steps 3–4 |
| Sub-attempts listed only when a revision had more than one | 2 (`subAttemptsLine`), 8 Step 4, 6 (`sub-attempts` fixture), 12 Step 2 assertion 10 |
| **review** body: reviewers strip (set + the engine's one-line reason) → artifact via the existing renderers → `CommentProvider` → `CommentMargin` → `SubmitBar` | 9 Steps 3–5 |
| `submitVerb` gains `allowSendBack: false` for projectDesign | 4 |
| **non-latest revision**: read-only banner + Back to latest, artifact as of that revision, that revision's threads with resolutions expanded, no submit bar | 10 Step 4 — **with R1's deviation**: the artifact shown is the CURRENT one under a caption saying so, because no op reads an artifact as of a ref (GAP-5). Earmarked in 14 Step 3; spec amended in 14 Step 1. |
| Default task: awaiting-human → failed → running → last passed | 2 Step 5 (+ "first task", which the spec's chain leaves undefined for a pristine activity) |

### Spec §7.3 (Plan) → task

| §7.3 promise | Task |
|---|---|
| Route `/project/$projectId/plan?lens=list\|graph\|tasks`; replaces the console shell and HomeBase's phase cards | 7 Step 2, 11 Step 6 |
| LIST: Table 11-1 order, M0 divider, mini graph + state per row | 11 Steps 2–3 |
| GRAPH: `@xyflow/react`, a tile per ACTIVITY whose body is the mini lifecycle | 11 Step 4 |
| Build order FRONT END → RESOURCES → RESOURCE ACCESS → ENGINES → MANAGERS → CLIENTS → SYSTEM TESTING (the architecture flipped) | 3 (`planRowFor`, and the explicit warning not to reuse `LAYERED_ROWS`, which is client-first), 11 Step 2 |
| Edges point down; M0 fans out to every root; N-STP in a side lane | 1 (`layoutPlanGraph` already does all three), 11 Step 2 |
| No utilities bar | 3 (`planRowFor` has no `utility` entry — utilities derive no activity) |
| Kept from the existing lens: row gutter, hover-focus, critical-path weight, milestone ribbon | row gutter → 11 Step 1 (moved) + Step 4; hover-focus → 3 Step 6 (**rewritten transitive**, R6); critical-path weight → 11 Step 4. **The milestone RIBBON is deliberately NOT ported** (Task 11 Step 4): `gateRibbon.ts` describes a per-activity lane's feeders and its complete-count, which the activity-tile graph has no lane to hang off. The M0 DIVIDER (LIST) and the M0 tile + fan-out (GRAPH) carry the milestone instead. Stated in the file header and in the §7.4 amendment. |
| TASKS lens survives as is and now includes design reviews | 11 Step 5 (the lens and its pipeline), **11 Step 5b** (design reviews, from `ArtifactSlotView.stage === 'awaitingReview'` — a definite derived source, not a probe) |
| Any row/tile/decision opens the Activity Experience full screen; ✕ returns with viewport and selection preserved (module memory) | 11 Steps 3–5 (`onReview`/row/tile), 11 Step 4 (`graphViewport`), **12 Step 3** (the ✕ round trip, in `plan.spec.ts` because only a plan fixture carries both screens' ops) |

### Spec §7.4 (Deleted) → task

Every name in §7.4 appears in Task 13's per-file table with a verdict and the import-edge grep that justifies it: `DetailPane`, `FocusView`, the body dispatch ladder (PORTED into `taskArtifactFor`/`ArtifactPanel` in Task 9 FIRST), `DesignExperience`, `SystemDesignContainer`, `ProjectDesignExperience`, `PHASE2_ORDER`, `toPhaseCards`, `gateOccurrences.ts`, old routes redirecting (Task 7 Step 3). §7.4's "artifact renderers and review aids are kept — inventory before deleting" is the table's KEEP block, and Task 9 Step 4 replaces their only reachable caller before Task 13 removes it.

**Three deviations from §7.4, each with its evidence and each amended into the spec in Task 14 Step 1:**
- **`SystemDesignView`'s step ladder, `SlimSpine.tsx` and `PHASE1_ORDER` are NOT deleted** — `containers/McpSystemDesignContainer.tsx:38,55,215,419` consumes all three and is out of scope. Deferred to stage 6 with that container.
- **`laneSpine.ts` IS deleted** though §7.4 does not name it: R3 replaced it with `miniLifecycleFromRow.ts`.
- **26+ uitests specs** must be deleted or retargeted; §7.4 does not mention the suite at all.

### Spec §7.5 (component gaps) → task

| Gap | Task | Proof |
|---|---|---|
| 1 `CommentableList`'s disabled branch never registers anchors | 5 Step 1 | Playwright: a margin card is PLACED in a read-only history (12 Step 2 assertion 11) — a `.tsx` change `node --test` cannot reach |
| 2 `ContractSignatureList` arms `contractOpAnchor` but never registers it | 5 Step 2 | Playwright: a contract-op thread is placed (12 Step 2 assertion 12) |
| 3 `GeneratingScene` role line / footer copy is design-rail specific | 5 Step 3 | `roleLine`/`footerNote` props; `RoleAvatar.PROP_FOR` already carries every worker class (verified), so no avatar change |
| 4 `MarginThreadCard` collapses resolved threads | 5 Step 4 | `expandResolved` on both components; asserted by 12 Step 2 assertion 6 |

### Spec §6 (Project Design · M0) → task

| §6 promise | Task |
|---|---|
| Cost & schedule headline, options table (normal preselected, radio) | 9 Step 6 via `SdpReviewView` (718 L, already renders options + the time-cost/time-risk scatters + the commit gate) |
| Derived activity list; network | 9 Step 6 via `ActivityListView`, `NetworkView` |
| Verb "Approve plan & cost — start construction" | 4 (`approveCopy`), 9 Step 1 (`verbsFor`) |
| Comments and questions allowed | 9 Step 7 (`useSetProjectReviewCommentStatus`; `ask` survives `allowSendBack: false` — tested in 4 Step 1) |
| **No send-back**; an `Amend Architecture →` link instead | 4 (`allowSendBack: false`), 9 Step 5 (the link), 12 Step 2 assertion 8 |
| The M0 gate always requires a human | read-only on the client (`reviewSet.requiresHuman` is display-only — "the enforced gate is the suspend itself"); enforced server-side in stage 2. Rendered by the reviewers strip (9 Step 3). |

### Spec §9 (testing) → task

| §9 line | Task |
|---|---|
| Pure layout/geometry node tests | 1 (60, not the spec's stale 20 — corrected in 14 Step 1) |
| webApp: fixtures per scenario in the preview shell | 6 |
| webApp: Playwright interaction suite | 12 — **authored, not promoted** (R14; `interact.mjs` never existed). Spec corrected in 14 Step 1. |
| `npm run check` green | every task's last step |

### Placeholder scan

Run over this file:
```bash
grep -n "/\* … \*/\|TBD\|similar to Task\|grep before deciding\|grep first\|Decide once\|Confirm whether" \
  docs/superpowers/plans/2026-09-24-activity-experience-stage5.md | grep -v 'grep -n'
```
(the trailing filter drops this command's own line, which necessarily contains every pattern it looks for).
**Expected: 0 hits.** Every test in this plan has real assertions; every copy module has its strings; every deletion in Task 13 carries its verdict and its measured evidence rather than an instruction to go and measure. The three places that deliberately say "port from X" rather than quoting hundreds of lines — Task 8 Step 4 (`proto/DispatchBody.tsx`, 444 L), Task 11 Step 3 (`proto/PlanPage.tsx`, 209 L), Task 11 Step 5 (`ConstructionConsole.tsx`'s TASKS pipeline, whose ~30 imports are listed by name) — each name the source file, its size and what to drop from it, and each source is IN THE WORKTREE until Task 14 removes it.

### Type consistency

- `ActivityView` (`hooks/useActivityView.ts`) is `OpResult<'constructionQueryActivityView'>`, which resolves through `OP_BINDINGS` + `paths['/api/v1/construction/query-activity-view/{projectID}/{activityID}']['get']` to `components['schemas']['ConstructionActivityView']` — verified in `schema.ts:119-134,823-842`. The adapters therefore name the schema type directly and stay in the `components` layer, which is the only placement the boundary DAG permits (Task 2's header note).
- `LifecycleNodeState 'done'` ↔ wire `'passed'`, `LifecyclePhase.passed` ↔ wire `completed`, `LifecycleRevision.outcome: string` ↔ wire enum: all three cross exactly once, in `activityViewToGraph.ts`, with a total `Record<TaskStateWire, LifecycleNodeState>` so a new wire value fails to compile rather than defaulting.
- **The plan screen never touches the raw wire row.** `planRowFor` and `miniLifecycleFromRow` take `Pick<ConstructionRow, …>` (`contracts/types.ts:727`), which `contracts/wire.ts:479 mapConstructionRow` produced and `useProject` returns as `ProjectStateWithGit.constructionRows` (`types.ts:1159`, `:887`). The PascalCase, ordinal-valued `SystemDesignActivityConstructionStatus` stops at the decoder. Task 3's wire note says this, names the decoder, and explicitly sends the reader AWAY from `projectAdapters.narrowProject`, which is a slot-envelope narrower and not this.
- **A review task's `artifactKind` is resolved, never read raw.** `artifactKindOf` (Task 2 Step 1) walks own → `reviews` → `revisionGroup`; `verbsFor` and `taskArtifactFor` take the resolved value as an input and refuse (`{ kind: 'none' }`) rather than invent one when it is missing. Ten review tasks across the 14 lifecycles have no kind of their own, and `projectDesign`'s `sdpReview` is the one that does — both cases tested.
- **The renderer branch's props have exactly one source.** `ArtifactRendererProps` (`artifactRenderers.tsx:25-31`) needs `vm.row: ConstructionRow`, `project` and `systemEnvelope`; the activity container's second read (`useProject`, Task 8 Step 2, for EVERY activity type) supplies all three, and `ArtifactPanel` degrades to `unavailable` when the row is absent.
- `ConstructionReviewer` (live) vs `ConstructionReviewRosterSeat` (historical) are kept distinct in `ReviewersStrip`'s props — Task 9's note.
- `lifecycles.gen.ts`'s `artifactKind` is Go-cased (`'Glossary'`) and `ArtifactKind` is the app string (`'glossary'`): mapped once, by the `SLOT_KIND` table in `activityVerbs.ts` (Task 9 Step 1), whose six entries are the only lifecycle kinds that name a committed slot; the five construction kinds are absent on purpose and tested as such.
- **Route paths are a `contracts/` leaf.** `PLAN_PATH`/`ACTIVITY_PATH`/`planSearch`/`activitySearch` live in `contracts/routePaths.ts` with zero imports, so `routes/`, `containers/` AND `components/` may all use them; `components → routes` is lint-fatal and no `eslint-disable` is permitted.

### Open questions this plan does NOT stop on (decided, recorded above)

R1 (no as-of read), R2 (no construction comment op), R3 (derive, don't fan out), R4 (client-side rows), R5 (no `deployment` row), R6 (transitive focus), R9's tail (a pristine activity opens `nodes[0]`), R7's `artifactKind` hop; and the five the spec did not raise, each decided in the "Execution risks" section above: the milestone RIBBON is not ported; the 26+ uitests specs over deleted screens are deleted or retargeted (Task 13 Cluster D); `preview-shell.spec.ts` is retargeted at the plan fixtures (Task 12 Step 4); `SystemDesignView`/`SlimSpine`/`PHASE1_ORDER` stay alive for the MCP widget until stage 6; and TASKS gets its design reviews from `ArtifactSlotView.stage`, not from a new probe.
