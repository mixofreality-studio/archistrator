# Deterministic Activity Derivation (Table 11-1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make archistrator's construction activity list exactly what the engine would derive on a real first run — Löwy Table 11-1 applied to the architecture, one client app, no test harness, no utilities — with zero legacy records and zero authored overrides, then re-apply the founder's "fully implemented = done, reviewed, integrated" ruling to the new derived set from evidence in the code.

**Architecture:** `EstimationEngine.DerivePlan` becomes a general Table 11-1 rule driven by the architecture's existing typed component fields (`constructionProfile`, `uiSurface`, `provisioning`) — no per-project knob. The production materializer finally writes slot 10 (network) from the derived dependencies and milestones, closing a known defect. State is reset in one transition: slots 9 and 10 re-materialized, all 25 authored overrides deleted, `.activityConstruction` emptied, then re-seeded by a rewritten backfill whose evidence is the codebase itself. The agent/skill doctrine describing the activity inventory is changed upstream in method-assets and released first.

**Tech Stack:** Go 1.26 (`GOWORK=off` always), `go/parser`, Temporal, React 19 + TypeScript, `node:test`, Playwright.

**Spec:** the founder rulings and architect rulings below are the binding authority for this plan. They were produced in the construction-ui-rewrite session (ledger: `.superpowers/sdd/2026-09-09-construction-ui-rewrite-stage-b/progress.md`, sections "FOUNDER RULING D9", "ARCHITECT RULINGS ON D9", "FOUNDER RULINGS on the architect's open decisions").

## Binding rulings

**Founder D9 (verbatim):** "i essentially want no legacy rows/activities. i want it all to be the deterministic activities that the app would have generated it we ran for real for the first time. just delete the old stuff. as i mentioned early what gets generated should be deterministic like table 11-1 ... (there is only 1 client app tho and can skip 5-8 cuz archistrator platform provides that stuff)". Supersedes the earlier "render only — do not touch DerivePlan" decision.

**Founder F1:** edit method-assets upstream, release it, bump the pin.
**Founder F2:** "if they're not implemented in code, then leave them as real work that still needs to be done." N-STP is NOT signed off.
> *Dated note (2026-09-12):* the N-STP clause above is superseded — the founder signed N-STP off on 2026-09-12, so it qualifies on that sign-off (see "Out of scope of the ruling" below). The ruling text above is kept as given.
**Founder F3:** "we don't have customers, so i want to just move right to fully deterministic activity gen." → delete ALL 25 `.activityListOverrides`.
**Earlier founder ruling, still standing:** "assume any component that is fully implemented is done and reviewed and integrated."

**Architect A1 — the target set: 29 activities + M0–M3.**

| Table 11-1 | Activity ids |
|---|---|
| #1–3 | M0 only — Phases 1–2 are the design rail, not activities |
| #4 Test Plan | `N-STP` |
| #5 Test Harness | none — platform-generated (`systemtests`) |
| #6–8 Logging/Security/Pub-Sub | none — utilities are `provided` |
| #9–10 Resources | `R-construction-pipeline-runtime`, `R-github`, `R-merchant-gateway`, `R-operated-runtime` |
| #11–13 ResourceAccess | `C-agentic-job-access`, `C-artifact-access`, `C-billing-state-access`, `C-episode-access`, `C-merchant-gateway-access`, `C-operated-runtime-access`, `C-operated-system-state-access`, `C-project-state-access`, `C-source-control-access`, `C-usage-access` |
| #14–16 Engines | `C-autoscaler-engine`, `C-billing-engine`, `C-design-health-engine`, `C-estimation-engine`, `C-intervention-engine`, `C-operation-estimation-engine`, `C-review-engine` |
| #17–18 Managers | `C-billing-manager`, `C-construction-manager`, `C-operations-manager`, `C-project-design-manager`, `C-system-design-manager` |
| #19 Client App | `U-SPA-web-client` — componentId `web-client`, "Build Web Client (SPA)", junior-developer, coding |
| #21 System Testing | `N-IT` |

Removed (12): `G-SPA`, `U-SPA-S`, `U-SPA-<manager>` ×5, `N-STH`, `N-RTH`, `N-SMOKE`, `N-PERF`, `N-QA`. "One client app" = clients with `uiSurface: true`; `mcp-client` and `scheduler-client` are `constructionProfile: generated` and get no activity. N-QA is a role (indirect cost), not an activity — the `qa-engineer` stays on the roster.

**A2:** general engine rule, no per-project setting. **A3:** slot 10 materialized from derived deps/milestones; M0 gets fan-OUT to every root. **A4:** "fully implemented" evidence = see Task 5. **A5:** state reset as one transition with the construction pump paused. **A6:** blast radius as listed per task.

## Global Constraints

- Go from `server/`, ALWAYS `GOWORK=off`. Node from `webApp/`. The platform repo is `../archistrator-platform`.
- **Real TS gate: `npm run typecheck` (`tsc -b`)**, never `npx tsc --noEmit`. **No vitest** — `node:test` only.
- **Never hand-edit** a `*.gen.go`/`*.gen.ts`, or a state slot in `.aiarch/state/project.json`. State changes are made by a tool and committed as its output.
- **Never weaken a gate.** Every task ends with all of these green unless the task states an explicit, temporary exception: `go test ./...`, `make lint` (0 issues), `TestFileLayout`, `TestGeneratedOnlyPublic`, `TestRepoStructureCmdIsClosed`, `TestNoBannedPhaseIdentifier`, and every drift target: `make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-uiprofiles-check derived-plan-check`.
- New exported symbols in `server/internal/resourceaccess/projectstate` need an allowlist entry in `server/internal/arch_test.go` whose every claim is verified by grep. New files may not be added to that package (layout gate).
- The bare word `phase` is banned as a NEW identifier.
- **The construction pump stays paused.** On resume it would dispatch `N-STP`, `C-design-health-engine`, `C-billing-state-access`, `C-merchant-gateway-access`, `R-merchant-gateway` immediately, and `U-SPA-web-client` once the managers are done — which collides with this branch. Do not resume it.
- **`project.json` is compiler input.** It drives the Go contract layer, OpenAPI, the TS client and Temporal. Only the slots named in a task may change.
- Commits end with:
  ```
  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_017LH9mNR8E5CUtzmANRvc4E
  ```

---

### Task 0: Change the doctrine upstream and release method-assets v0.7.0

**Repo:** `../archistrator-platform` (public on GitHub, `main`, clean). Tags are `method-assets/vX.Y.Z`; `v0.6.0` is current (`7e3077e0`, one commit + tag pushed to origin, no CI).

**Files:**
- `method-assets/assets/claude/skills/the-method-activity-list/SKILL.md` — architect cites lines ~31, 71, 81, 112–145, 270, 280–282, 316, 322
- `method-assets/assets/claude/skills/the-method-testing/SKILL.md`
- `method-assets/assets/claude/agents/test-engineer.md`, `software-tester.md`, `junior-developer.md`, `qa-engineer.md`
- `method-assets/stepmanifest.gen.go` only if `cmd/gen-stepmanifest` regenerates it

- [ ] **Step 1:** Read each file and change every statement of the activity inventory to the A1 target: Table 11-1 applied to the architecture; one coding activity per hand-built component; one client activity per `uiSurface` client; noncoding activities exactly `N-STP` (test plan) and `N-IT` (system testing); no test-harness, smoke, perf or QA activities — harness and build automation are platform-generated, QA is a role. Keep the three quality roles distinct as *roles*. Do not invent doctrine beyond A1.
- [ ] **Step 2:** If `cmd/gen-stepmanifest` consumes any changed file, regenerate `stepmanifest.gen.go` with it. Never hand-edit it.
- [ ] **Step 3:** Run the module's tests: `cd ../archistrator-platform/method-assets && GOWORK=off go test ./...` — all green.
- [ ] **Step 4:** Commit on `main`: `method-assets: derive the activity inventory from Table 11-1`.
- [ ] **Step 5:** Tag and publish — **founder-authorized (F1)**:
  ```bash
  cd ../archistrator-platform
  git tag method-assets/v0.7.0
  git push origin main method-assets/v0.7.0
  git ls-remote --tags origin 'method-assets/v0.7.0'   # must print the tag
  ```
  A rejected push means the remote moved: stop and report. Never force-push.

### Task 1: Bump the method-assets pin and re-materialize `.claude`

**Files:** `server/go.mod`, `server/go.sum`, materialized `.claude/**`

- [ ] **Step 1:** `cd server && GOWORK=off go get github.com/mixofreality-studio/archistrator-platform/method-assets@v0.7.0` (the Go proxy may need a minute to see a fresh tag; retry, do not use a pseudo-version).
- [ ] **Step 2:** `GOWORK=off make claude-assets` and confirm the materialized skill/agent text now matches Task 0.
- [ ] **Step 3:** `GOWORK=off go test ./...` — tests that read the `.claude` prompt surface must pass against the new text. If a test pins old inventory wording, update the test to the new doctrine.
- [ ] **Step 4:** Commit: `chore(method-assets): consume v0.7.0 — Table 11-1 activity doctrine`.

### Task 2: Materialize slot 10 from the derivation; give M0 its fan-out

**Files:** `server/internal/manager/projectdesign/projectdesignmanager.go` (`materializePhase2Draft` ~:3161–3194, the `list, _, _, err :=` discard ~:3180), `server/internal/engine/estimation/estimationengine.go` (`deriveMilestones` ~:1806), tests beside them.

Today `materializePhase2Draft` discards the derived dependencies and milestones, so slot 10 is whatever the drafting agent wrote, held honest only by CI. A real first run must write the derived network.

- [ ] **Step 1: failing tests.** (a) Materializing a `KindNetwork` draft replaces its dependencies and milestones with the derived ones, keeps each existing milestone's `Name`/`Public`, and recomputes `criticalPath` via `ComputeNetwork`. (b) After derivation, every activity with no other predecessor depends on `M0`.
- [ ] **Step 2:** Run them; confirm they fail.
- [ ] **Step 3:** Implement. Remove the discard. M0 keeps an empty `dependsOn` (its predecessors are the design rail) — the pump already resolves an empty-`dependsOn` milestone as satisfied (`constructionmanager.go` ~:1051–1067), which is correct because the pump cannot run before the SDP review.
- [ ] **Step 4:** Full gates. `derived-plan-check` must stay green; if it also checks slot 10, this task re-materializes slot 10 by tool in the same commit.
- [ ] **Step 5:** Commit: `fix(project-design): materialize the network from the derivation`.

### Task 3: Derive the Table 11-1 activity set; delete every authored override

**Files:** `server/internal/engine/estimation/estimationengine.go` and `engine_test.go`; `.aiarch/state/project.json` slots 9 + 10 and `.activityListOverrides` (tool output); any test that pins the 40-activity shape.

This task changes the engine and re-materializes the state it drives **in one commit**, so `derived-plan-check` never goes red.

- [ ] **Step 1: failing tests** pinning A1 exactly: the derivation of the committed architecture emits the 29 ids in the A1 table and no others; `mcp-client`/`scheduler-client` get no activity; utilities get no activity; `U-SPA-web-client` exists with componentId `web-client`; `N-IT` depends on `N-STP` and `U-SPA-web-client`; `U-SPA-web-client` depends (after transitive reduction) on `C-billing-manager`, `C-construction-manager`, `C-system-design-manager`; there are 18 roots and each depends on `M0`. Assert the SET, so an added activity fails.
- [ ] **Step 2:** Implement per A2: reduce `alwaysEmitNoncoding` (~:1421) to `N-STP` + `N-IT` and trim `noncodingInventoryClass` (~:1350) to match; delete `managerSPAActivityFor` (~:1497), `spaScaffoldActivities` (~:1512), `systemHasUISurface` (~:1541), `spaScreenNames` (~:1692) and the body of `addFixedPatternEdges` (~:1740); add `clientAppActivityFor(c)` emitting `U-SPA-<clientId>` for each client with `UiSurface` set; index it in `activityForComponent` (~:1659) so `architectureEdges` routes client → manager edges; add the **sink rule** (`N-IT` depends on every activity with no other successor) and the **source rule** (every activity with no other predecessor depends on `M0`). Remove the island workaround edges (~:1773–1777).
- [ ] **Step 3: Delete all 25 `.activityListOverrides` (F3).** Effort and risk now come only from `defaultEffortFor` / `defaultRiskFor`. With no authored deltas, the production materializer's empty-deltas behaviour becomes simply correct.
- [ ] **Step 4:** Re-materialize slots 9 and 10 with the project-design materializer (never hand-typed). Diff them: slot 9 has exactly the 29 A1 ids; slot 10 has 29 dependency rows.
- [ ] **Step 5:** Full gates, including `derived-plan-check`. Fix every test the engine change breaks (the architect counts ~34 old-id references in `engine_test.go`).
- [ ] **Step 6:** Commit: `feat(estimation): derive the activity list from Table 11-1; drop authored overrides`.

### Task 4: Reset construction state

**Files:** `.aiarch/state/project.json` (`.activityConstruction`, `.constructionProgress`, slots 11–16 stale-basis marks — tool output); `server/internal/resourceaccess/projectstate/projectstateaccess.go` (`activityAliases` ~:8842, resolver ~:8907) and its tests.

- [ ] **Step 1:** Set `.activityConstruction` to `{}` — all 69 legacy rows go (D9).
- [ ] **Step 2:** Delete `activityAliases` and its resolver and tests — they exist only to reconcile legacy short ids.
- [ ] **Step 3:** Mark slots 11–16 as having a stale basis: none contains a removed id, but their numbers were computed over the 40-activity network. Do not re-run Phase 2 here.
- [ ] **Step 4:** Recompute `.constructionProgress` from the new state.
- [ ] **Step 5:** Full gates. Commit: `chore(state): reset construction state onto the derived plan` — body must say state now holds zero legacy records.

### Task 5: Rewrite the backfill on code evidence and re-run it

**Files:** `server/cmd/backfill-attempts/main.go` + `main_test.go`; `.aiarch/state/project.json` `.activityConstruction` (tool output, separate commit).

The legacy `produced[]` evidence is gone. A **component** is fully implemented only when ALL hold (A4.1):
1. It has `.serviceContracts` entries, grouped by `.component`.
2. Every entry has a non-empty `goPackage` and no `stub: true`.
3. `server/<goPackage>/contract.gen.go` exists, AND the hand-written (not generated) `server/<goPackage>/<lowercase interface>.go` declares ONE receiver type with a method for every operation of the component's own contract — checked with `go/parser`. A contract with zero operations is refused, not vacuously covered.
   - **3b (ResourceAccess only):** at least one covering receiver must be a struct with at least one field. Its declaration is resolved across ALL non-test files of the package, `contract.gen.go` included (`GitArtifactAccess` is declared there). An RA binds a Resource, and every placeholder in the repo is an empty `struct{}`. Where 3b applied, the basis carries the component's kind and names the fielded receivers.
   - **Exempt from 3b:** Engines and Managers. They are stateless by doctrine, and `stub: true` stays authoritative for them.
   - **Accepted residual:** `erroringArtifactAccess{err}` has a field, so 3b cannot tell it from a live receiver. It only ever co-covers beside `GitArtifactAccess`.

*A4.3 amended 2026-09-12 (architect ruling Q1).* Condition 3 first read "`<lowercase interface>.go` declares `type <Interface>Impl`". Taken literally, that qualifies ZERO components:
- an Engine's `<Interface>Impl` is generated, in `contract.gen.go`;
- a Manager's concrete type is the unexported `<interface>` struct;
- an RA names its type after the Resource it binds.

The method-coverage rule above replaces it and is strictly stronger: a bare `type XImpl struct{}` fails it. 3b closes the hole it left, an RA whose only implementation is an empty no-op.

**Facet rule:** facet entries share the component's `goPackage`. Condition 3 reads the component's own interface and ignores facet interface names; otherwise `C-project-state-access`, which is implemented, would falsely fail. `revenueLedgerAccess` behaves the same.

**Resources:** an `R-*` qualifies iff every RA with a slot-5 relationship to it qualifies; its basis must say the evidence is inferred.

**Out of scope of the ruling (F2 + A4):** `U-SPA-web-client` (this branch is rewriting it), `N-STP` (signed off by the founder 2026-09-12, so it qualifies on that sign-off, not on this ruling), `N-IT`. Components that fail the test stay not done — real work. Expected: `C-design-health-engine`, `C-billing-state-access`, `C-merchant-gateway-access`, `R-merchant-gateway` do not qualify. **Count:** 19 of the 22 `C-*` qualify. The earlier "at most 17" was an arithmetic error: 22 − 3 = 19.

Qualifying activities get a passed attempt for every non-conditional task in their profile. Origin `backfilled`, never `observed`. Basis cites the `serviceContracts[...]` keys, the file paths, the HEAD commit, and the ruling verbatim.

- [ ] **Step 1: failing tests** pinning the three conditions, the facet rule, the resource inference, the exact qualifying set on a fixture, and that every emitted attempt passes `Validate()`.
- [ ] **Step 2:** Implement; `Validate()` every attempt before writing and abort on failure; keep the byte-fidelity gate.
- [ ] **Step 3:** Dry run, then real run. Report which activities qualified and which did not, and why.
- [ ] **Step 4:** Commit tool and output separately; the state commit carries `NOT FOR MERGE TO MAIN without an explicit founder decision recorded here.` and quotes the ruling.

### Task 6: Remove dead server code and stale references

- [ ] Delete the G-SPA completion-reconciliation logic (`systemdesignmanager.go` ~:3483–3546) now that G-SPA is gone.
- [ ] Replace `ClassifyType`'s manager-to-client layer workaround (~:7916–7942) with an ordinary component lookup; confirm `hydrateConstructionActivity` (`constructionmanager.go` ~:1124) now stamps the Client layer honestly.
- [ ] Keep the prefix classifiers (`ClassifyActivity`, `DeriveVariant`) — additives may still use those names.
- [ ] Fix remaining old-id references in `access_test.go`, `systemdesign/manager_test.go`, `construction/manager_test.go`, `projectdesign/manager_test.go`, `cmd/aiarch-state-mcp/constructverbs_test.go`, and the stale comment in `operations/view.go:25`.
- [ ] Full gates; commit: `refactor(construction): drop legacy-id and per-manager-SPA code paths`.

### Task 7: Remove the legacy UI and fix client-side references

- [ ] Delete the legacy partition and group in `webApp/src/components/construction/list/ActivityTreeView.tsx`, and `CoverageStrip.tsx`, `coverageCounts.ts`, `coverageCounts.test.ts` — the 40-vs-69 seam they measured no longer exists.
- [ ] Update G-SPA- and legacy-specific comments and tests (`ConstructionConsole.tsx`, `lifecycleTemplates.ts`, `activityRowPresentation.ts`, `activityTree.test.ts`, `useLensSelection.test.ts`, `reviewVerdict.test.ts`, `team.ts`).
- [ ] Rework `uitests/tests/construction-search-provenance-guarantee.spec.ts` and `construction-tracker.spec.ts` for the new ids; the provenance guarantee must still be pinned against whatever rows are now reconstructed.
- [ ] webApp gates; live drive the list; commit: `refactor(construction): remove the legacy seam from the console`.

### Task 8: Rebuild, verify end to end, hand to the founder

- [ ] Rebuild the bundled `webappdist` assets.
- [ ] Every gate listed in Global Constraints, plus webApp typecheck/eslint/test/build and the construction uitests.
- [ ] Restart the local server on the new state and verify on the wire: 29 activities; `.activityConstruction` holds only derived ids; qualifying components show passed attempts stamped `backfilled`; the four non-qualifying components show no history; zero legacy rows.
- [ ] Screenshot the list and the architecture of the plan; read them back.

## Earmarks (not this plan)

- Other projects on the platform (e.g. gtdapp) drift on their next re-materialization — drain their workflows before deploying.
- The platform does not yet ship the generated test harness to other apps; archistrator alone has `gen-systemtests`. Platform backlog.
- `U-SPA-web-client` is capped at 35 days by `legalEffort`, which understates a five-manager SPA. Löwy's remedy is the compression-only client-design split, not per-manager activities.
- The pump's M0 resolver could later require "slot 16 committed" rather than treating an empty-`dependsOn` milestone as satisfied.
- `C-design-health-engine` has code but no contract; writing one is a detailed-design task.
