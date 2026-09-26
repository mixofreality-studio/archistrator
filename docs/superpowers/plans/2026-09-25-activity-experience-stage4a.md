# Activity Experience — Stage 4a (DeliveryManager: the model + package collapse) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make archistrator's model say what the spec ruled — ONE `delivery-manager` component with ONE 12-op `deliveryManager` contract where three Managers stood — and make the code say the same thing on the same commit, **without changing one byte of rail behaviour**. The three Go packages (`internal/manager/{systemdesign,projectdesign,construction}`, 61,576 lines) become one `internal/manager/delivery`; the fourteen hand-written files move in with their bodies unchanged except for name collisions the merge forces; the 12 ops are written as a THIN DISPATCHER over the forty implementations that already exist; one Temporal worker on one task queue `delivery` registers all eleven existing workflow types under their existing names. Stage 4b — the generic DAG child, the parallel pump, deterministic Project Design, the twin deletion, engines 7→4 — is a separate plan written after this one merges.

**Architecture:** This is a self-amendment wave with a code half. The unit of work is a hand edit to `.aiarch/state/project.json` followed by the §8 gate loop, plus a `git mv`-shaped package collapse. Stage-1 planning established (and this plan re-verified) that `SYS-CARD-MGR` (Error at a 6th Manager), `ALIGN-EXTRA-PKG` (Error for a package with no component), `DV-SINGLE-MGR` and `DH-CONTRACT-FACET` close every door on a model-first or code-first ordering: the model edit and the package edit are **one indivisible commit** (Task 6). Everything that can be de-risked ahead of that commit is pulled out in front of it — the replay fixtures that will prove the move is determinism-safe (Task 1), the `buildStatus` vocabulary rule (Task 2), the per-activity CAS (Task 3), the workflow-id collision (Task 4), the derived-plan Makefile seam (Task 5) — so that when the big commit's gates go red there are five fewer candidate causes. Everything downstream of the wire (webApp, uitests, MCP widget) follows in Tasks 7–9, and the drain note and spec amendment close the stage.

**Tech Stack:** Go 1.26 (`GOWORK=off` always), Temporal Go SDK (workflows, replay testing), `.aiarch/state/project.json` as the model database (git-as-DB), modelgen / clientgen / appgen / temporalgen codegen, `framework-go@v0.11.1` methodcheck + arch gates and the in-repo `designhealth` engine, React 19 + TypeScript 5.9 for the SPA schema, Playwright 1.50 in `uitests/`.

**Spec:** `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` — §4 (architecture model: the volatility, the components, the 3 core use cases, the 12-op table), §5 (5.1 pump, 5.2 child, 5.3 the one staging/review rail + execution data model, 5.4 one review engine), §6 (Project Design · M0), §8 (the stage-4 row, the must-ship-together set for stage 4's FIRST COMMIT, the self-amendment procedure, "drain-and-cutover, not `GetVersion`, where task queues change owner"), §10 (risks). Predecessor plans whose authored text this plan carries forward: `docs/superpowers/plans/2026-09-23-activity-experience-stage1.md` **Tasks 2 and 3** (the component, the contract, the relationships, the core use case, the re-parentings — reproduced here in full and corrected), `2026-09-23-activity-experience-stage3.md` (the rail this moves unchanged), `2026-09-24-activity-experience-stage5.md` (the screens Task 7–9 re-key). Earmarks that gate this stage: `docs/bugs/2026-09-23-stage1-model-wave-earmarks.md`, `docs/bugs/2026-09-24-stage3-rail-earmarks.md` (the DRAIN NOTE and the stage-4 ENTRY CRITERIA), `docs/bugs/2026-09-24-stage5-webapp-earmarks.md` (the stage-4 hook re-key list).

## Rulings carried into this plan

Each was decided before or during planning. An implementer does not re-litigate them. R1–R14 came from the founder/planning brief; R-A–R-G were ruled while writing this plan, each with the measurement that forced it.

| # | Ruling |
|---|---|
| **R1** | **Stage 4 is TWO plans.** This is **4a**: the indivisible model commit; the package collapse with the old workflows moved UNCHANGED; the 12-op façade as a thin dispatcher over the forty existing implementations; ONE worker on ONE task queue `delivery` registering all eleven existing workflow types; codegen + gates; the webApp/uitests re-key; the drain note. **4b** is a separate plan written after 4a merges — see "Out of scope (4b)". |
| **R2** | **Workflow TYPE names and workflow IDs do NOT change in 4a** (only the Go package and the task queue). Consequence: the 15 construction replay fixtures keep replaying unchanged — a hard gate of this plan. The ONE id change is the pre-existing collision `{p}:phaseAdvance`, used by BOTH design Managers (`systemdesignmanager.go:4879`, `projectdesignmanager.go:2587` — both `fmt.Sprintf("%s:phaseAdvance", projectID)`); it re-keys to `{p}:phaseAdvance:systemDesign` / `{p}:phaseAdvance:projectDesign` for NEW histories only (Task 4). |
| **R3** | **Capture design-rail replay fixtures BEFORE the move** (Task 1). The two design Managers carry 21 `GetVersion` fences and **zero** recorded histories (`find internal/manager/{systemdesign,projectdesign}/testdata -name '*.json'` → nothing). Use the `CONSTRUCT_HISTORY_CAPTURE` rig (`construction/manager_test.go:7480`) as the template. They must replay green after the move — that is the proof the move is determinism-safe. |
| **R4** | **No deploy from 4a.** 4a and 4b both merge; ONE drain (stages 3 + 4a + 4b combined, per the stage-3 DRAIN NOTE) precedes the next deploy. Task 10 EXTENDS that note and states "do not deploy 4a alone". |
| **R5** | **Arm the per-activity CAS in 4a** (stage-3 entry criterion 1), in the OLD packages, before the move (Task 3). Thread the row's `Version` through the mutating `activityExecutionAccess` verbs' callers. The child already holds the row (it comes back on the `readProject` the workflow already makes), so **no new Temporal command is added** — which is why the existing `changeExecutionLedger` fence suffices and the fixtures decide. |
| **R6** | **Relationships: 16, not 14.** The stage-1 text omits `delivery-manager → operation-estimation-engine` and `→ billing-engine`. Both are added: they are spec §6's cost headline and, measured, the two Engines' ONLY Manager caller today (`jq` over `.slots["5"].model.relationships` indices 14 and 15). Without them `DV-REL-COVERAGE` has nothing to exercise for either Engine. |
| **R7** | **`buildStatus` is a closed vocabulary** (stage-3/1 entry criterion 2), pinned in the estimation engine and reported as an ERROR by `designhealth` (Task 2). Measured live values: `null` ×33, `"external"` ×3, `"planned"` ×1. An unknown value is never silently re-derived. |
| **R8** | **Design-health pins.** `DH-CONTRACT-OPCOUNT-MAX` goes SILENT after the three contracts go — **measured**: with `systemDesignManager` (16) and `constructionManager` (13) deleted, the largest surviving contract is 12 ops (`constructionTransitionAccess`, `activityExecutionAccess`) and the rule fires only above 12. `engine_test.go:64`'s `assertPresent` therefore flips to `assertAbsent`, with the measurement in the comment. `DH-CONTRACT-DEADOP` STAYS pinned at Warning (`engine_test.go:99`): deleting the deprecated facets is 4b's, and even then `projectStateAccess.AcknowledgeStaleBasis` keeps the duplicate alive — the stage-3 earmark is wrong to say it flips. `DH-CONTRACT-FACET` stays `assertAbsent` (`:90`). |
| **R9** | **`make derived-plan-write` / `derived-plan-check` move ahead of the model commit** (Task 5). Both targets run `./internal/manager/projectdesign/`, the package being deleted. They CANNOT be physically relocated to `cmd/` first: `TestWriteDerivedPlan` calls the unexported `materializePhase2Draft`, `materializeNetwork`, `rewriteStateFile`, `derivedPlanMembers` and `committedStatePath`, and exporting five seams to move a test is exactly the techdebt "fix right over techdebt" forbids. **What moves first is the SEAM**: Task 5 puts the package behind a `DERIVED_PLAN_PKG` Makefile variable in its own green commit, and Task 6 Step 4 flips the default in the same edit that moves the files — before any slot-9/10 write. |
| **R10** | **The model commit is ONE commit and is green only with the code.** It carries: the component + the contract + 16 relationships + the core use case + every dynamic view + the 11 re-parentings + the demotion + the three deletions + the new Go package + `cmd/clientgen`/`cmd/appgen` exposed lists + `cmd/clientgen/mcpdocs.go` + `arch_test.go` allowlists + the `registered_names_test.go` golden + `engine_test.go` + `cmd/server/managerlog.go` + `cmd/server/hooks.go` + the slot-6 deployment container + `make derived-plan-write`'s slots 9/10. The self-amendment loop runs `validate --slot System` AND `--slot Volatilities` AND every other edited slot. **Never two implementers on `project.json`.** |
| **R11** | **The webApp/uitests re-key is Tasks 7–9, after the server commit.** `npm run gen:api && npm run gen:ops` is the ONLY regen this plan runs in `webApp/` and it happens in Task 7, committed with its inputs. |
| **R12** | **The MCP widget keeps building.** `containers/McpSystemDesignContainer.tsx` + `mcpShell` are out of scope for deletion (stage 6) but their ops re-key with the rest; `npm run build:mcp` is a gate of Task 8. |
| **R13** | **Scope guard: no behaviour change on any rail in 4a.** Acceptance: every gate green; 15 + 4 replay fixtures green; the same 40 behaviours reachable through 12 ops; `validate` 0 errors with 3 Managers and no `SYS-CARD-MGR`/`DV-SINGLE-MGR`/`DH-CONTRACT-FACET`; ONE worker process; the drain note extended. **4a is line-neutral by design** — it moves code, it does not delete it. The spec §9 "line count of delivery manager < sum of predecessors" acceptance is measured at the END of 4b, not here. |
| **R14** | **Recon corrections honoured:** 40 ops on the three Managers, not 50; 8 dispatchable Phase-2 draft commands + the SDP assembly, not 9; the stage-1 plan's Step-7 prediction about `DH-CONTRACT-OPCOUNT-MAX` is wrong (R8); engines stay at 7 in 4a (7→4 is 4b's, with the system-architect). |
| **R-A** | **`SubmitReviewDecision` takes a `ReviewDecisionInput` OBJECT, not a bare enum.** Measured: the nine writers it replaces need five distinct extra facts — `acknowledgeStale` (`AdvancePhase`, `AdvanceToConstruction`), `optionId` (`SubmitSDPDecision`), and `commentId`+`commentStatus` (`SetReviewCommentStatus` ×2). The stage-1 plan's 5-param signature could not reach them, so the "same 40 behaviours through 12 ops" acceptance would have failed. `ReviewDecision` is WIDENED by appending ordinals 4 (`ReviewAdvance`) and 5 (`ReviewSetCommentStatus`) — appended, never renumbered. |
| **R-B** | **`QueryProjectView` takes ONE `ProjectViewQuery` object.** The 13 readers need `owner` (`ListProjects`), `projectId`, `artifactKind` (design `GetSessionState`/`ListEpisodesForArtifact`), `activityId` (construction `GetSessionState`/`ListEpisodesForActivity`) and `episodeId` (`GetEpisodeTimeline`) — six selectors. Six flat params on one op is a worse contract than one typed query object; the query object is also what makes `ProjectViewKind` self-documenting. This CORRECTS the stage-1 plan's `ProjectViewKind` enum, which had `plan` (there is no such reader) and lacked `session` and `timeline` (there are five). |
| **R-C** | **`AnchoredComment` merges to the four-field design shape.** Measured, exactly three `$defs` names conflict across the three contracts (`AnchoredComment`, `SessionStage`, `SessionStateView`); everything else with a shared name is byte-identical. `constructionManager`'s `AnchoredComment` is the design shape MINUS `anchorText`, i.e. a strict subset, so the merged def is the design one and construction's Go literals (which name their fields) gain a zero `AnchorText`. One comment shape, not two. |
| **R-D** | **`SessionStage` / `SessionStateView` keep BOTH shapes under distinct names.** The two `SessionStage` enums are NOT compatible: `projectDesignManager`'s inserts `StageAssemblingSDP` at ordinal 2 and shifts `StageAwaitingReview`…`StageDraftFailed` up by one. Merging them would renumber a wire-visible ordinal, which is forbidden. The merged contract therefore carries `SessionStage`/`SessionStateView` (the systemDesign bodies, verbatim) and `ProjectSessionStage`/`ProjectSessionStateView` (the projectDesign bodies, verbatim, with every `x-enum-varname` prefixed `Project…` because two enums in one Go package cannot share a constant name). The webApp already calls them `SessionStage` and `ProjectSessionStage` — so `gen-enums` `OUTPUT_NAMES` keeps both output names exactly and the SPA sees no type rename for either. |
| **R-E** | **The merged contract's `deps` are the UNION of the three, unchanged.** Measured from `.serviceContracts.*.deps`: 21 entries (`client`, `projectState`, `artifact`, `intervention`, `review`, `estimator`, `operationEstimator`, `billingEstimator`, `pipeline`, `rail`, `constructionTransition`, `gitStatus`, `designSession`, `activityExecution`, `messageBus`, `episodes`, `escalationWaitTimeout`, `interventionMode`, `repo`, `repoBase`). Dropping the deprecated facets is 4b's (post-drain); dropping them here would delete 26 registered Temporal activity names that in-flight workflows still need. `designHealth` is NOT a dep — `systemDesignManager` constructs `designhealth.NewEngine()` inline (`systemdesignmanager.go:164`), and 4a does not change that; the slot-5 edge `delivery-manager → design-health-engine` still holds, because the call is real. |
| **R-F** | **The three impl files become ONE `deliverymanager.go`; the three `manager_test.go` become ONE `manager_test.go`.** This is not a style choice: `arch.CheckFileLayout` (`framework-go@v0.11.1/arch/filelayout.go:110-111, 191-208`) computes `implFile = leaf + FileStereotype + ".go"` = `deliverymanager.go` and flags EVERY `_test.go` in the directory whose name is not `manager_test.go`. The eleven workflow files keep their current names: `want = strings.ToLower(strings.TrimSuffix(entryFunc, "Workflow")) + ".go"` already holds for all eleven. |
| **R-G** | **The merged golden is 139 entries, computed, and must be regenerated from the measured diff.** Derivation: today 231; the three Managers contribute 172 (construction 69 activities + 5 workflows; systemdesign 46 + 3; projectdesign 46 + 3 — each measured by `grep -c RegisterActivityWithOptions` on `worker.gen.go`); the merged Manager contributes ONE copy of each dep's activities (9+3+3+11+12+6+8+12+2+3 = 69) + 11 workflows = 80. 231 − 172 + 80 = **139**, a net removal of 92 names. Write the number the test prints, not this one. |

## Global Constraints

- Work in the git worktree `.claude/worktrees/activity-stage4a` (branch `activity-experience-stage4a`, from `origin/main` @`4baed01a`). The main checkout is shared with other sessions. `.claude/skills` and `.claude/commands` are materialized in this worktree (three tests read them) and `webApp/node_modules` is installed.
- `GOWORK=off` on EVERY `go`/`make` command under `server/`. `ASDF_NODEJS_VERSION=lts` on EVERY npm/npx command. Gates run against PINNED platform tags (`framework-go v0.11.1`, `method-assets v0.9.0`) — never a `replace`.
- **Slot edits.** `.serviceContracts` and `.slots["3"]` (the expiring waiver) / `["4"]` (decisions) / `["5"]` (components, relationships, dynamic views) / `["6"]` (the deployment container's component list) of `.aiarch/state/project.json` ARE hand-edited in Task 6. **Slot 7 is NOT** — the brief's "3/4/5/6/7" was a carry-over; nothing this stage changes lives in it, and `validate --slot` is run for the four that do change. **Slots 9 and 10 are NEVER hand-edited** — they move by `make derived-plan-write` alone. Never two implementers on `project.json`, even in disjoint regions.
- **The self-amendment loop**, run after every `project.json` edit:
  ```bash
  cd server
  GOWORK=off make gen-models gen-fakes gen-client gen-internal-tools gen-temporal gen-sdk gen-config gen-main gen-lifecycles
  GOWORK=off make method-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot Volatilities
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot CoreUseCases
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot OperationalConcepts
  GOWORK=off go test -short -count=1 ./...
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run gen:api && ASDF_NODEJS_VERSION=lts npm run gen:ops && ASDF_NODEJS_VERSION=lts npm run check
  ```
  Use `go test -short -count=1 ./...`, **not** `make test-short` — the latter masks the `designhealth` package.
- **Drift gates before EVERY commit that touches a generated input:**
  ```bash
  cd server
  GOWORK=off make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check \
      gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-lifecycles-check
  GOWORK=off make sumtype-check derived-plan-check encapsulation-check method-check fix-check lint
  GOWORK=off go test ./internal/ -run 'TestRegisteredTemporalNamesGolden|TestFileLayout|TestGeneratedOnlyPublic|TestMethodLayering|TestNoBannedPhaseIdentifier|TestMessageBusManagersOnly' -count=1
  ```
  plus, on any commit that touches a workflow body or a Temporal call: `GOWORK=off go test ./internal/manager/<pkg>/ -run Test_Replay -count=1`.
- Never weaken, skip or allowlist around a gate. No `//nolint`. No `default:` arm over a sum type (`sumtype-check`). No hand-edited `*.gen.*` file — regen only, committed with the input change that caused it.
- Never run `git restore`, `git clean`, `git stash`, `git checkout -- <path>` or any tree-wide reset. Back up the gitignored SDD ledger (`.superpowers/sdd/`) to the session scratchpad after every append.
- **A changed Manager op also needs:** `cmd/clientgen/mcpdocs.go`'s op-doc table (`mcpemit.Generate` ERRORS on an op with no non-empty doc — the table is a build gate), `webApp/scripts/gen-enums.mjs` `OUTPUT_NAMES`, and `cmd/server/managerlog.go`.
- Wire-visible identifiers are NEVER renumbered: `ArtifactKind` ordinals, `ActivityType` ordinals, the root `phase`, `AttemptID`/`RoundID` formats, and every existing `x-enum` ordinal. New members are APPENDED.
- `required` in the contract schema dialect is PRESENCE-only. Non-emptiness lives in the Go implementation, never in `minLength` (2026-08-13 contract-strictness ruling; `ValidateModelIdentities` + the paramguard arch gate hold the line).
- Match the surrounding idiom exactly — JSON key order, Go comment density, the `— …` em-dash rationale style. Commit messages end with:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  ```
- **Out of scope (4b), EARMARK only:** the generic DAG child driven by `methodassets.LifecycleFor`; the parallel pump (`{p}:pump`, react-by-signal, no `child.Get` cascade); design activities dispatched as children (deleting `ErrDesignActivityNotDispatchable`'s error arm); deterministic Project Design (§6) and the deletion of `coauthorphase2artifact.go` + the 8 draft commands; deleting the co-author twins; ONE review engine (§5.4) and the `engineReviewPolicy` ×3 + inline `NewReviewEngine()` collapse; engines 7→4 and the `design-health-engine` dissolution; the artifact-as-of-revision read and the batched `QueryProjectView(plan)`; the `applyRecovering` terminality-`Conflict` class; the stranded-`pending`-round sweep; the artifact-kind FIELD on `ReviewRound`; `RoundWithdrawn`'s own wire member; `amend-N` branch retirement; deleting the three deprecated RA facets. Also out of scope: the `the-method-review-routing` / `the-method-project-tracking` SKILL.md drift in **method-assets** (a platform release, founder STOP), and `uitests/testdata/coreUseCasesProject.json` (its regen reads the committed `main` branch, so it is a post-merge step).

## Task order

**De-risking, one commit each, all ahead of the big one:** Task 1 (design-rail replay fixtures) → Task 2 (`buildStatus` vocabulary) → Task 3 (arm the per-activity CAS) → Task 4 (`{p}:phaseAdvance` re-key) → Task 5 (the `DERIVED_PLAN_PKG` seam).

**The indivisible commit:** Task 6.

**Then the wire's consumers:** Task 7 (webApp regen + enums) → Task 8 (hooks, containers, MCP widget) → Task 9 (preview fixtures + uitests).

**Then the record:** Task 10 (drain note + earmarks) → Task 11 (spec §8 amendment + the 4b hand-off).

**Must ship together:**
- Task 3 is ONE commit: the contract change, the regenerated files and all 18 call sites — a half-threaded CAS does not compile.
- Task 6 is ONE commit, and its steps run in the printed order. `SYS-CARD-MGR` forbids a 6th Manager; `ALIGN-EXTRA-PKG` forbids a package with no component; `ALIGN-MISSING-PKG` forbids a component with no package; `DH-CONTRACT-FACET` forbids a contract with no component; `USECASE-DYNAMIC-MISSING` forbids a use case with no view. There is no ordering in which any two of them are separate commits.
- Tasks 7 and 8 may not be split across a green `npm run check`: `ops.gen.ts` regenerated without the hook re-key leaves every mutation calling a dead op id.

**Parallelism.** Tasks 1, 2, 4 and 5 are genuinely disjoint (fixtures / estimation engine / two workflow-id helpers / the Makefile) and may run in parallel. Task 3 must land before Task 6 and after Task 1 (the fixtures are what prove the CAS threading did not move a command). Everything from Task 6 onward is strictly sequential.

## Execution risks and how this plan removes each

1. **The design rails have no recorded history, so nothing mechanical catches a determinism break when they move.** 21 `GetVersion` fences, zero fixtures. Task 1 captures four before the move; Task 6 Step 13 replays all nineteen after it.
2. **`{p}:phaseAdvance` is already used by two Managers.** Today they never collide because they run on different task queues and never concurrently. One queue makes the collision real and silent — `SignalWithStartWorkflow` would join the WRONG workflow. Task 4 re-keys it ahead of the merge, so the merge commit does not also have to debug it.
3. **The `DH-CONTRACT-OPCOUNT-MAX` pin goes RED, and the stage-1 plan predicts the opposite.** A plan that copies that prediction ships a red test. R8 states the measurement; Task 6 Step 11 flips the assertion with the number in the comment.
4. **Three `$defs` names conflict and two of them cannot be merged.** `SessionStage`'s two ordinal sets differ by an inserted member. Merging them silently renumbers `StageAwaitingReview` from 2 to 3 on the design rail — a wire break that no gate would catch, because both sides regenerate together. R-D keeps both; Task 6 Step 3 renames the projectDesign side in its own contract and in its Go callers.
5. **`derived-plan-write` lives in the package being deleted.** The self-amendment procedure would break in the middle of the one commit that may not be split. Task 5 moves the seam; Task 6 Step 4 flips it before slots 9/10 are touched.
6. **The registered-name golden loses 92 entries and nothing tells you which.** `TestRegisteredTemporalNamesGolden` prints a diff, not a count. Task 6 Step 12 regenerates the literal from the test's own output and asserts the arithmetic of R-G against it.
7. **`AskQuestions` in the stage-1 plan takes `[]string`.** The live op takes `[]AnchoredComment` (`jq` on `.serviceContracts.systemDesignManager.interface.operations[] | select(.name=="AskQuestions")`). Shipping the stage-1 signature would silently drop every question's anchor — a behaviour change on a rail, which R13 forbids. Corrected in Task 6 Step 5.
8. **There is a THIRD exposed-manager list, and it is the one that generates the worker.** `server/cmd/appgen/main.go:84` `var managers` drives BOTH the temporalgen loop (`:98`, which emits `contract.gen.go`, `activities.gen.go`, `invokers.gen.go` and `worker.gen.go` per manager) and `generateSDK` (`:311`). Left at its five entries, `make gen-temporal` dereferences `m.Contracts["systemDesignManager"]` for a contract Step 7 deleted, and `internal/manager/delivery/worker.gen.go` is never emitted at all — GREEN POINT B fails with an error that names neither `managers` nor `appgen`. Task 6 Step 4 sets all THREE lists, and GREEN POINT B names `worker.gen.go` as a required output so its absence is caught immediately.
9. **`make gen-sdk` silently deletes the SDK the systemtests harness calls.** `generateSDK` → `pruneStaleSDK` (`appgen/main.go:302-337`) deletes every `*.gen.go` under `../systemtests/internal/sdk` that is not in the fresh output set, so `http_{system-design,project-design,construction}.gen.go` and their `mcp_*`/`types_*` siblings go and `http_delivery.gen.go` arrives. The hand-written `systemtests/internal/harness/*` calls the deleted symbols and stops compiling, and `.github/workflows/systemtests.yml` triggers on `server/**` and `.aiarch/**` — both touched by Task 6 — so CI goes red on *Go fix check*, *Lint*, *constitution tests* and *wire system tests*. **The `stp_uc*` drift gate is NOT the failure**: its input is `.testingState.systemTestPlan`, which 4a never edits, so the regenerated tables are byte-identical. Task 6 Step 14b re-points the harness and gates on the systemtests module's own checks.

---

### Task 1: Capture design-rail replay fixtures — the proof the move is determinism-safe

The two design Managers carry **21 `GetVersion` fences** between them (`coauthorartifact.go` 14, `coauthorphase2artifact.go` 7) and **zero recorded histories**. Construction has 15 fixtures in 5 directories and a vacuity guard that turns an empty fixture directory into a failure (`manager_test.go:7561`). When the design workflows move into a new package with renamed private symbols, nothing mechanical will catch a changed command sequence. This task closes that hole BEFORE the move, using construction's own rig as the template.

**Files:**
- Modify: `server/internal/manager/systemdesign/manager_test.go` — add the capture tool + the replay test, both copied in shape from `construction/manager_test.go:7470-7565`.
- Modify: `server/internal/manager/projectdesign/manager_test.go` — same.
- Create: `server/internal/manager/systemdesign/testdata/replay/design-pre-stage4/{coauthor-approve-merge,coauthor-sendback-redraft-approve,phase-advance}.json`
- Create: `server/internal/manager/projectdesign/testdata/replay/phase2-pre-stage4/{sdp-review-commit}.json`

**Interfaces produced (Task 6 Step 13 depends on these exact names):**
- `systemdesign`: `Test_Replay_DesignHistories_StayDeterministic(t *testing.T)`, capture env var `DESIGN_HISTORY_CAPTURE=1`, optional `DESIGN_HISTORY_CAPTURE_DIR`.
- `projectdesign`: `Test_Replay_Phase2Histories_StayDeterministic(t *testing.T)`, capture env var `PHASE2_HISTORY_CAPTURE=1`.
- **The fixture directory names are the FINAL ones from the moment they are captured**: `designReplayCases()` returns `c.dir == "design-pre-stage4"` and `phase2ReplayCases()` returns `c.dir == "phase2-pre-stage4"`, even though both live under their own package's `testdata/replay/` today. Task 6 Step 2 `git mv`s each directory into `delivery/testdata/replay/` under the SAME name, so **`c.dir` is never edited by any task** and Step 13's nineteen replays find every fixture by the string Task 1 wrote. Naming them `pre-stage4` in both packages and renaming later would put a one-line edit in the middle of the largest commit in the wave, where its failure mode reads as "the capture failed" rather than "the directory moved".

- [ ] **Step 1: Read the template before writing anything.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  sed -n '6995,7065p;7470,7570p' internal/manager/construction/manager_test.go
  ```
  Expected: the doc block explaining the rig, the `CONSTRUCT_HISTORY_CAPTURE` guard at `:7480`, the per-directory walk, and the `t.Fatal("no replay fixtures found under testdata/replay")` vacuity guard at `:7561`. Copy the SHAPE — the env var name, the directory walk, the vacuity guard, the "fixture %s is missing (capture it with …)" message — and change only the env var and the workflow registration.

- [ ] **Step 2: Pick the four histories from tests that already drive these workflows end to end.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off go test ./internal/manager/systemdesign/ -list '.*' -count=1 | grep -iE 'approve|sendback|redraft|advance' | head -30
  GOWORK=off go test ./internal/manager/projectdesign/ -list '.*' -count=1 | grep -iE 'sdp|commit|advance' | head -30
  ```
  Choose exactly four, one per fixture name in **Files** above, preferring the longest path through the fences: a co-author session that drafts → critiques → gates → approves → merges (exercises `design-round-ledger`, `design-vibes-autogate`, `system-architect-critique`, `resolve-before-decision`, `managed-scaffold-sync`); one that sends back and redrafts to approval (`failed-gate-redraft-drain`, `gate-decision-token-remint-%d`, `amend-seed-notes`); `PhaseAdvanceWorkflow`; and `AssembleSDPReviewWorkflow` through `SDPCommit`. Record the chosen test names in the capture doc comment so the next author can re-capture.

- [ ] **Step 3: Write the capture tool in `systemdesign/manager_test.go`.** Append after the last existing test, in the file's idiom:
  ```go
  // Test_Capture_DesignHistories is the capture half of the replay gate — the design-rail
  // twin of construction's Test_Capture_ConstructHistories. It is SKIPPED unless asked for
  // by name, because it REWRITES testdata/replay/. Run it only when a deliberate command-
  // sequence change has been reviewed:
  //
  //	DESIGN_HISTORY_CAPTURE=1 [DESIGN_HISTORY_CAPTURE_DIR=<dir>] GOWORK=off \
  //	  go test ./internal/manager/systemdesign/ -run Test_Capture_DesignHistories -count=1
  //
  // The fixtures it writes are the ONLY mechanical proof that the fourteen GetVersion fences
  // in coauthorartifact.go and systemdesignphase.go still produce the same command sequence.
  // Stage 4a moves this whole package; a fixture captured here and replayed there is what
  // makes that move reviewable instead of hopeful.
  func Test_Capture_DesignHistories(t *testing.T) {
  	if os.Getenv("DESIGN_HISTORY_CAPTURE") != "1" {
  		t.Skip("capture tool: set DESIGN_HISTORY_CAPTURE=1 to (re)write testdata/replay/ fixtures")
  	}
  	only := os.Getenv("DESIGN_HISTORY_CAPTURE_DIR")
  	for _, c := range designReplayCases() {
  		if only != "" && c.dir != only {
  			continue
  		}
  		t.Run(c.name, func(t *testing.T) {
  			env := c.drive(t)
  			hist := env.GetWorkflowHistory()
  			path := filepath.Join("testdata", "replay", c.dir, c.name+".json")
  			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
  				t.Fatal(err)
  			}
  			blob, err := protojson.Marshal(hist)
  			if err != nil {
  				t.Fatal(err)
  			}
  			if err := os.WriteFile(path, blob, 0o644); err != nil {
  				t.Fatal(err)
  			}
  			t.Logf("captured %s (%d events)", path, len(hist.GetEvents()))
  		})
  	}
  }
  ```
  - [ ] **Verify first:** read construction's capture tool and use ITS marshalling and history-extraction calls verbatim — the Temporal test env's history accessor and the JSON encoder must match the replayer's expectations exactly. If construction uses a helper (`captureHistory`, `historyJSON`, …) rather than `protojson.Marshal`, use that helper's shape here and adjust the code above accordingly. Do not invent an encoding.

- [ ] **Step 4: Write the replay test in `systemdesign/manager_test.go`**, copying construction's `Test_Replay_PreB1Histories_StayDeterministic` (`:7542`) including its vacuity guard:
  ```go
  // Test_Replay_DesignHistories_StayDeterministic replays every captured design-rail
  // history against the CURRENT workflow code. A non-determinism error here means the
  // command sequence moved — either add a GetVersion fence for the change, or revert it.
  // The vacuity guard is deliberate: an empty fixture directory is a failure, not a pass.
  func Test_Replay_DesignHistories_StayDeterministic(t *testing.T) {
  	var replayed int
  	for _, c := range designReplayCases() {
  		path := filepath.Join("testdata", "replay", c.dir, c.name+".json")
  		blob, err := os.ReadFile(path)
  		if err != nil {
  			t.Fatalf("fixture %s is missing (capture it with DESIGN_HISTORY_CAPTURE=1): %v", path, err)
  		}
  		t.Run(c.dir+"/"+c.name, func(t *testing.T) {
  			replayDesignHistory(t, blob)
  		})
  		replayed++
  	}
  	if replayed == 0 {
  		t.Fatal("no replay fixtures found under testdata/replay")
  	}
  }
  ```
  `replayDesignHistory` registers the three workflow functions under their registered names (`executionKindPhase`, `executionKindCoAuthor`, `executionKindPhaseAdvance` — read the consts at `systemdesignmanager.go:4682-4686`) on a `worker.WorkflowReplayer` and calls `ReplayWorkflowHistory`. Mirror construction's helper exactly.

- [ ] **Step 5: Same two additions in `projectdesign/manager_test.go`**, with `PHASE2_HISTORY_CAPTURE` / `Test_Capture_Phase2Histories` / `Test_Replay_Phase2Histories_StayDeterministic` / `phase2ReplayCases()` / `replayPhase2History`, registering `projectDesignCoAuthor`, `projectDesignSDPReview` and `projectDesignPhaseAdvance` (consts at `projectdesignmanager.go:2429-2433`).

- [ ] **Step 6: Capture, then replay, then prove the construction fixtures are untouched.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  DESIGN_HISTORY_CAPTURE=1 GOWORK=off go test ./internal/manager/systemdesign/ -run Test_Capture_DesignHistories -count=1 -v
  PHASE2_HISTORY_CAPTURE=1 GOWORK=off go test ./internal/manager/projectdesign/ -run Test_Capture_Phase2Histories -count=1 -v
  GOWORK=off go test ./internal/manager/systemdesign/ ./internal/manager/projectdesign/ -run Test_Replay -count=1
  GOWORK=off go test ./internal/manager/construction/ -run Test_Replay -count=1
  ls server/internal/manager/systemdesign/testdata/replay/design-pre-stage4 \
     server/internal/manager/projectdesign/testdata/replay/phase2-pre-stage4
  ```
  Expected: four JSON files written; all three replay tests `ok`; the construction run reports 15 subtests, unchanged. If a capture produces a history with fewer than ~20 events the chosen test did not drive the workflow to a terminal state — pick a different test rather than accepting a thin fixture.

- [ ] **Step 7: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off make lint fix-check
  GOWORK=off go test ./internal/ -run 'TestFileLayout' -count=1
  GOWORK=off go test -short -count=1 ./internal/manager/...
  ```
  `TestFileLayout` matters here: the new code goes in the EXISTING `manager_test.go` of each package, never in a new file.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  git add server/internal/manager/systemdesign server/internal/manager/projectdesign
  git commit -F - <<'MSG'
  test(design): record the design rails' first replay fixtures

  The two design Managers carry 21 GetVersion fences and, until now, zero
  recorded histories — so nothing mechanical could tell a refactor that moved a
  durable command from one that did not. Construction has had that gate since
  B1; the rails that actually drive Phase 1 and Phase 2 have never had it.

  Four fixtures, captured through the same rig construction uses: a co-author
  session that drafts, critiques, gates, approves and merges; one that sends
  back and redrafts to approval; the phase advance; and the SDP assembly
  through commit. Both replay tests keep construction's vacuity guard, so an
  empty fixture directory fails rather than passes.

  Stage 4a moves both packages. These fixtures are what make that move
  reviewable.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 2: `buildStatus` is a closed vocabulary (stage-4 entry criterion)

Stage 1 made `buildStatus: "planned"` skip the plan derivation. It did NOT make the field a vocabulary: a component authored `"Planned"`, `"plannned"` or `"PLANNED"` falls through the skip, derives a `C-<id>` construction activity, lands in slot 9, and the 30-second pump sweep hands a designed-but-unbuilt component to a build agent. Task 6 authors `delivery-manager` — the moment that typo matters. Measured live values today: `null` ×33, `"external"` ×3, `"planned"` ×1 (`scheduler-client`).

**Files:**
- Modify: `server/internal/engine/estimation/estimationengine.go` — the vocabulary const set beside `buildStatusPlanned`.
- Modify: `server/internal/engine/estimation/engine_test.go` — the typo test.
- Modify: `server/internal/engine/designhealth/designhealthengine.go` — a new rule id + its evaluation.
- Modify: `server/internal/engine/designhealth/engine_test.go` — the rule's negative fixture + the green-state assertion.

**Interfaces:**
- Consumes: `estimation.SystemComponent.BuildStatus string` (stage 1), `buildStatusPlanned` (stage 1).
- Produces: `estimation.KnownBuildStatus(s string) bool`; `designhealth.RuleBuildStatusVocabulary` with id `DH-BUILDSTATUS-VOCAB`, severity **Error**.

- [ ] **Step 1: Confirm the live vocabulary before fixing it to a set.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  jq -r '[.slots["5"].model.components[]|.buildStatus]|group_by(.)|map({v:.[0],n:length})' .aiarch/state/project.json
  ```
  Expected, verbatim: `null` 33, `"external"` 3, `"planned"` 1. If a fourth value appears, STOP — the vocabulary below is wrong and the founder must rule on the new member before this task proceeds.

- [ ] **Step 2: Write the failing tests.** Append to `server/internal/engine/estimation/engine_test.go`:
  ```go
  // A buildStatus the vocabulary does not know is a MODEL DEFECT, not a hint. The skip in
  // codingActivityFor/provisioningActivityFor/clientAppActivityFor keys on the exact string
  // "planned"; "Planned" sails past it, derives C-<id> into slot 9, and the 30-second pump
  // sweep hands a component with no code to a build agent. The derivation must never
  // re-interpret a value it does not recognise — it must refuse to recognise it, loudly,
  // and let the design-health gate name the component.
  func TestKnownBuildStatusIsAClosedVocabulary(t *testing.T) {
  	for _, ok := range []string{"", "planned", "external"} {
  		if !KnownBuildStatus(ok) {
  			t.Errorf("KnownBuildStatus(%q) = false, want true — this is a live value in the committed model", ok)
  		}
  	}
  	for _, bad := range []string{"Planned", "PLANNED", "plannned", "built", "todo", " planned"} {
  		if KnownBuildStatus(bad) {
  			t.Errorf("KnownBuildStatus(%q) = true, want false — a near-miss must not be accepted", bad)
  		}
  	}
  }

  // The typo case, end to end: a component whose buildStatus is "Planned" must NOT be
  // silently re-derived as buildable. The derivation treats an unknown value exactly as it
  // treats "planned" — it emits nothing — and the design-health gate is what names it.
  // Deriving nothing is the safe half: a missing activity is a visible hole in the plan,
  // where a spurious one is an agent spending money on a component that does not exist.
  func TestDeriveActivitiesEmitsNothingForAnUnknownBuildStatus(t *testing.T) {
  	sys := sampleSystem()
  	sys.Components = append(sys.Components,
  		SystemComponent{ID: "typo-manager", Name: "TypoManager", Kind: "manager", ConstructionProfile: "handwritten", BuildStatus: "Planned"},
  	)
  	for _, a := range deriveActivities(sys) {
  		if a.ComponentID == "typo-manager" {
  			t.Errorf("emitted %s for typo-manager, whose buildStatus %q is not in the vocabulary — a typo must never re-derive a planned component", a.Name, "Planned")
  		}
  	}
  }
  ```
  Run it and confirm it fails for the right reason:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off go test ./internal/engine/estimation/ -run 'TestKnownBuildStatus|TestDeriveActivitiesEmitsNothingForAnUnknownBuildStatus' -count=1 2>&1 | head
  ```
  Expected: `undefined: KnownBuildStatus` (build failure) — the vocabulary does not exist.

- [ ] **Step 3: The vocabulary, beside the const stage 1 added.** In `server/internal/engine/estimation/estimationengine.go`, directly under `const buildStatusPlanned = "planned"`:
  ```go
  // buildStatusExternal marks a component supplied from outside the project — the platform
  // utilities carry it. Like "planned" it derives no activity, but for the opposite reason:
  // planned means "no code YET", external means "not ours".
  const buildStatusExternal = "external"

  // KnownBuildStatus reports whether s is a value the derivation understands. The empty
  // string is the common case (built, or to be built) and is legal.
  //
  // This is a CLOSED vocabulary on purpose. buildStatus is the one field in the model that
  // decides whether a component becomes a dispatchable activity at all, and the skip keys on
  // an exact string — so "Planned" is not a near-miss, it is a component handed to a build
  // agent by the 30-second pump sweep. An unknown value is a model defect: the derivation
  // refuses to build it (see deriveActivities) and DH-BUILDSTATUS-VOCAB names it at Error.
  // Widening this set is a founder ruling, not a fix for a red gate.
  func KnownBuildStatus(s string) bool {
  	switch s {
  	case "", buildStatusPlanned, buildStatusExternal:
  		return true
  	default:
  		return false
  	}
  }
  ```
  Then change the guard at the head of each of the three emitters (`codingActivityFor`, `provisioningActivityFor`, `clientAppActivityFor`) from the stage-1 form to:
  ```go
  	if !KnownBuildStatus(c.BuildStatus) || c.BuildStatus == buildStatusPlanned || c.BuildStatus == buildStatusExternal {
  		return DerivedActivity{}, false
  	}
  ```
  - [ ] **Verify first:** open `estimationengine.go` and confirm stage 1's guard is exactly `if c.BuildStatus == buildStatusPlanned {` in all three emitters; if stage 1 shipped a different shape, edit that shape rather than pasting over it. Confirm too that `"external"` was already being skipped for another reason (the three external components are `provided` utilities) — if this change REMOVES an activity from the derived set, the skip is too wide and the guard, not the expectation, is wrong.

- [ ] **Step 4: The design-health rule.** In `server/internal/engine/designhealth/designhealthengine.go`, beside the other `DH-*` rule ids, add:
  ```go
  // RuleBuildStatusVocabulary fires for a component whose buildStatus is outside the closed
  // vocabulary the plan derivation understands ("", "planned", "external"). ERROR, not
  // Warning: this is the one model field that decides whether a component becomes a
  // dispatchable construction activity, the derivation keys on the exact string, and a
  // near-miss ("Planned") is a component handed to a build agent by the pump sweep. There
  // is nothing advisory about it.
  RuleBuildStatusVocabulary RuleID = "DH-BUILDSTATUS-VOCAB"
  ```
  and emit one finding per offending component, in the file's existing finding idiom (`Location{Section: "component " + c.ID}`), with the message:
  ```
  component %s has buildStatus %q, which is not one of "", "planned", "external" — the plan derivation keys on the exact string, so this component derives no activity and no gate would otherwise say so
  ```
  - [ ] **Verify first:** `grep -n 'RuleID = "DH-' server/internal/engine/designhealth/designhealthengine.go | head` and copy the declaration block's exact shape. Confirm the severity constant the file uses for Errors (`methodcheck.SeverityError`).

- [ ] **Step 5: Pin it both ways.** In `server/internal/engine/designhealth/engine_test.go`:
  - add `assertAbsent(t, got, RuleBuildStatusVocabulary)` to `TestGreenFixtureAdvisoriesFire` (the committed state's three values are all in the vocabulary — verified in Step 1);
  - add a negative fixture test in the file's existing negative-fixture idiom, feeding a component with `"buildStatus": "Planned"` and asserting exactly one `DH-BUILDSTATUS-VOCAB` finding at `methodcheck.SeverityError` naming that component.
  `TestGreenFixtureNoErrors` is the backstop — it fails on ANY Error on the committed state, so a mis-scoped rule reddens immediately.

- [ ] **Step 6: Run everything, including the derived plan.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off go test ./internal/engine/estimation/ ./internal/engine/designhealth/ -count=1
  GOWORK=off make derived-plan-write
  git -C .. status --short -- .aiarch/state/project.json
  GOWORK=off make derived-plan-check method-check lint fix-check
  ```
  `derived-plan-write` MUST be a no-op — every live `buildStatus` is already in the vocabulary, so the derived set cannot move. A diff here means the guard caught something it should not have: read it, do not commit it.

- [ ] **Step 7: Commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  git add server/internal/engine/estimation server/internal/engine/designhealth
  git commit -F - <<'MSG'
  fix(estimation): buildStatus is a closed vocabulary, and a typo is an Error

  Stage 1 made "planned" skip the derivation. It did not make the field a
  vocabulary, so "Planned" sailed past the skip, derived C-<id> into slot 9,
  and the 30-second pump sweep would have handed a component with no code to a
  build agent. Stage 4a authors delivery-manager — the commit where that typo
  starts to matter.

  KnownBuildStatus closes the set to "", "planned" and "external" (the three
  values the committed model actually carries: 33, 1 and 3). The derivation
  refuses to build an unknown value rather than re-interpreting it, and
  DH-BUILDSTATUS-VOCAB names the component at Error — because this is the one
  field that decides whether a component becomes dispatchable at all.

  The derived plan is unchanged, which is what makes this a rule rather than a
  behaviour change.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 3: Arm the per-activity CAS (stage-4 entry criterion, in the OLD packages)

`withActivityVersion` (`server/internal/resourceaccess/projectstate/projectstateaccess.go:9945`) already stamps `cs.Version++` on every applied transition and already refuses a mismatch with `fwra.Conflict`. It has never compared anything: every call site passes `noActivityVersionExpectation` (`= 0`, `:9933`) through `onActivity` (`:9969`). The comment on the const says why — "inventing a parameter for one no caller can fill would be a guard that only ever compared a fabricated number". Stage 4a is the moment the callers CAN fill it, and it is the **last safe place to change these verbs' contract before parallel children exist** (4b's pump starts a child for every eligible activity).

**This lands in the OLD packages, before the move**, so that the fixtures from Task 1 and the 15 construction fixtures judge exactly one change.

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts.activityExecutionAccess.interface.operations`: add one `expectedActivityVersion` param to the eleven MUTATING verbs (every op but `ReadActivityExecution`).
- Regenerate (never hand-edit): `server/internal/resourceaccess/projectstate/contract.gen.go`, `.../fake/fake.gen.go`, `server/internal/manager/{systemdesign,projectdesign,construction}/{activities.gen.go,invokers.gen.go}`, `server/internal/resourceaccess/projectstate/toolcatalog.gen.go`.
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — `onActivity` + the eleven verb bodies + `OpenActivity`.
- Modify: `server/internal/manager/construction/constructactivity.go` — the six call sites.
- Modify: `server/internal/manager/systemdesign/coauthorartifact.go`, `server/internal/manager/projectdesign/coauthorphase2artifact.go` — the twelve design call sites.
- Modify: `server/internal/resourceaccess/projectstate/access_test.go` and the three `manager_test.go` — the tests below.

**Interfaces produced (Task 6 consumes these exact names):**
- `projectstate.ActivityExecutionAccess`'s eleven mutating verbs each gain, immediately after `expectedVersion Version`, a parameter `expectedActivityVersion int64`.
- `projectstate.NoActivityVersionExpectation` — **exported** const `int64 = 0`, the "I have not read the row" value, for the two writers that genuinely have not (`OpenActivity` on a birth, and the backfill tool).
- In each Manager package, the workflow state gains an `activityVersion int64` field, seeded from the project read and re-seeded by `applyRecovering` on a Conflict.

- [ ] **Step 1: Measure the call sites before touching anything.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  grep -rhoE "Acts\.ActivityExecution[A-Za-z]+" internal/manager/*/[a-z]*.go | grep -v _test | sort | uniq -c
  ```
  Expected, verbatim (measured at `4baed01a`): `AppendReviewVerdict` 3, `DecideReviewRound` 3, `OpenActivity` 3, `OpenReviewRound` 3, `ReadActivityExecution` 2, `RecordActivityOutcome` 1, `RecordAttemptOutcome` 1, `SetReviewCommentStatus` 2 — **18 sites** across **8** of the twelve verbs, of which 16 are mutating. If the counts differ, the tree has moved: re-derive the site list and say so in the commit message.
  - **Four of the eleven mutating verbs have NO manager call site, and that is expected, not a missed site.** `StageTaskOutput`, `CommitActivityArtifacts`, `RecordOperatorNote` and `AcknowledgeStaleBasis` landed in stage 3 as part of the facet but are not yet wired into the workflows — the rails still reach those behaviours through the deprecated facets, and re-pointing them is 4b's. They take the parameter in their signature and their non-workflow callers (tests, `cmd/backfill-attempts`) pass `NoActivityVersionExpectation`. Do not go hunting for callers that do not exist, and do not skip the four in Step 3: a verb whose signature diverges from its siblings is the next reader's trap.

- [ ] **Step 2: Write the two failing tests first** (`server/internal/resourceaccess/projectstate/access_test.go`), in the file's idiom:
  ```go
  // A STALE expectation is refused with Conflict, naming both versions — the same class the
  // git ref-CAS loss carries, because it is the same "someone already moved this" the caller
  // resolves by re-reading. This is the guard that makes parallel children safe: two writers
  // on the SAME activity cannot interleave, while two children on DIFFERENT activities never
  // contend at all (the project-level CAS would have made them).
  func TestActivityExecutionRefusesAStaleActivityVersion(t *testing.T) {
  	ra, projectID, activityID := seedOpenActivity(t)
  	row, err := ra.ReadActivityExecution(raCtx(), projectID, activityID)
  	if err != nil {
  		t.Fatalf("ReadActivityExecution: %v", err)
  	}
  	if _, err := ra.RecordAttemptOutcome(raCtx(), projectID, anyVersion(t, ra, projectID), row.Version,
  		activityID, sampleAttempt(activityID), noCred, "idem-1"); err != nil {
  		t.Fatalf("fresh expectation must apply: %v", err)
  	}
  	// row.Version is now one behind — the same value a second child would still be holding.
  	_, err = ra.RecordAttemptOutcome(raCtx(), projectID, anyVersion(t, ra, projectID), row.Version,
  		activityID, sampleAttempt(activityID), noCred, "idem-2")
  	if !isRAConflict(err) {
  		t.Fatalf("stale activity version must be refused with Conflict, got %v", err)
  	}
  	if !strings.Contains(err.Error(), "re-read the activity and re-apply") {
  		t.Errorf("the Conflict must tell the caller what to do, got %q", err.Error())
  	}
  }

  // NoActivityVersionExpectation is still honoured, and it is not a loophole: it is the
  // posture of a writer that has not read the row (OpenActivity on a birth; the backfill
  // tool). Every workflow caller reads the row from the project read it already makes, so
  // every workflow caller passes a real number.
  func TestActivityExecutionAcceptsTheUnreadPosture(t *testing.T) {
  	ra, projectID, activityID := seedOpenActivity(t)
  	if _, err := ra.RecordAttemptOutcome(raCtx(), projectID, anyVersion(t, ra, projectID),
  		NoActivityVersionExpectation, activityID, sampleAttempt(activityID), noCred, "idem-3"); err != nil {
  		t.Fatalf("the unread posture must still apply: %v", err)
  	}
  }
  ```
  - [ ] **Verify first:** `seedOpenActivity`, `raCtx`, `anyVersion`, `noCred`, `sampleAttempt`, `isRAConflict` are placeholders for whatever the file already has. Read the neighbouring activity-execution tests and use the real helpers; do not add new ones unless the file has none.

- [ ] **Step 3: The contract change.** In `.aiarch/state/project.json`, `.serviceContracts.activityExecutionAccess.interface.operations`, insert into each of the eleven mutating ops — `OpenActivity`, `StageTaskOutput`, `RecordAttemptOutcome`, `OpenReviewRound`, `AppendReviewVerdict`, `SetReviewCommentStatus`, `DecideReviewRound`, `CommitActivityArtifacts`, `RecordActivityOutcome`, `RecordOperatorNote`, `AcknowledgeStaleBasis` — immediately after the `expectedVersion` param:
  ```json
  { "name": "expectedActivityVersion", "schema": { "type": "integer", "x-go-base": "int64" } },
  ```
  `ReadActivityExecution` is untouched (it is the read that PRODUCES the number). Then:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off make gen-models gen-fakes gen-temporal gen-internal-tools
  git -C .. status --short -- server/internal
  ```
  Expected modified: `internal/resourceaccess/projectstate/{contract.gen.go,toolcatalog.gen.go}`, `internal/resourceaccess/projectstate/fake/fake.gen.go`, and `activities.gen.go` + `invokers.gen.go` in all three manager packages. **`worker.gen.go` must NOT change** — the registered activity NAMES are unaffected by a parameter, which is the whole reason this is safe to land before the move. If `worker.gen.go` moved, stop: something renamed an op.
  - [ ] **Verify first:** open the regenerated `contract.gen.go` and use the exact Go parameter name and type modelgen emitted (`expectedActivityVersion int64`) in Steps 4–6.

- [ ] **Step 4: Arm the guard.** In `server/internal/resourceaccess/projectstate/projectstateaccess.go`:
  - export the const, keeping its doc and correcting the paragraph that says no caller can fill it:
    ```go
    // NoActivityVersionExpectation is the "I have not read this row" posture. It is NOT a
    // loophole: OpenActivity may birth the row, and cmd/backfill-attempts writes history it
    // did not read. Every WORKFLOW caller holds the row — it comes back on the readProject the
    // child already makes — so every workflow caller passes a real number, and a stale one is
    // refused. Stage 4a armed this; before it, the guard compared a fabricated zero.
    const NoActivityVersionExpectation int64 = 0
    ```
    and leave `noActivityVersionExpectation` as an unexported alias ONLY if the file has in-package readers that would otherwise churn; otherwise delete it and update the call sites.
  - thread the parameter through `onActivity`:
    ```go
    func (a *activityExecutionAccess) onActivity(
    	rc fwra.Context, op string, projectID ProjectID, expectedVersion Version, expectedActivityVersion int64,
    	activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey,
    	mutate func(cs *ActivityExecution) error,
    ) (Version, error) {
    	if activityID == "" {
    		return 0, execMisuse(op, "empty activityID")
    	}
    	return a.store.applyMutation(rc.Context, op, projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting,
    		withActivityVersion(op, activityID, expectedActivityVersion, mutate))
    }
    ```
  - pass the new parameter from each of the eleven verb bodies. `OpenActivity` does not use `onActivity` (it may birth the row); give it the same check explicitly, skipped when the row is being born.

- [ ] **Step 5: Thread it in construction.** `server/internal/manager/construction/constructactivity.go` holds six sites (`:2011`, `:2032`, `:2158`, `:2224`, `:2254`, `:2387`), each inside an `applyRecovering` closure that already receives `expected` (the PROJECT version). Add a sibling:
  - add `activityVersion int64` to `constructState` beside the existing ledger fields;
  - seed it where `seedResumeFromLedger` (`:1277`) already reads `proj.ActivityExecution[id]` — `state.activityVersion = row.Version`;
  - in each of the six closures, pass `state.activityVersion` after `expected`, and on success set `state.activityVersion++` (the RA increments the stored counter by exactly one per applied transition — `withActivityVersion`'s `cs.Version++`);
  - in `applyRecovering`'s re-read arm, re-seed `state.activityVersion` from the re-read row alongside the project version it already re-reads.
  - [ ] **Verify first:** read `applyRecovering` and `readVersionE` before editing. If the re-read does not already hand back the whole project (only a version), re-seeding needs the row — use the read the function already makes rather than adding a second Temporal command. **Adding a command here is the one thing that would force a new `GetVersion` fence**; Step 7 is what proves you did not.

- [ ] **Step 6: Thread it in both design rails.** `coauthorartifact.go` and `coauthorphase2artifact.go` hold the other twelve sites, all inside the `design-round-ledger` fenced block (`openDesignRound`, `seedRoundBaseFromLedger` and their verdict/decide companions). The session state already carries the row for `ledgerRoundBase`; add `activityVersion int64` to the session state struct in each file, seed it where the row is read, and pass/increment exactly as in Step 5.

- [ ] **Step 7: Prove no command moved — this is the acceptance of the whole task.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off go test ./internal/manager/construction/ -run Test_Replay -count=1 -v 2>&1 | tail -30
  GOWORK=off go test ./internal/manager/systemdesign/ ./internal/manager/projectdesign/ -run Test_Replay -count=1 -v 2>&1 | tail -20
  ```
  Expected: 15 construction subtests and 4 design subtests, all `--- PASS`. **A non-determinism failure here means a command was added**, not that the fixture is stale: find the extra `ExecuteActivity`/`SideEffect` and remove it, or — if the extra command is genuinely required — put it behind a NEW `GetVersion` change id (`"activity-cas-armed"`) whose default arm is the pre-change sequence, and re-run. Never re-capture a fixture to make this pass.

- [ ] **Step 8: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off go test ./internal/resourceaccess/projectstate/ -count=1
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make gen-models-check gen-fakes-check gen-temporal-check gen-internal-tools-check \
      gen-client-check gen-sdk-check gen-config-check gen-main-check gen-lifecycles-check
  GOWORK=off make sumtype-check derived-plan-check encapsulation-check method-check fix-check lint
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check
  ```
  `activityExecutionAccess` is not a web-exposed Manager, so `npm run check` needs no regen; if `schema.ts` moves, something exposed changed and the contract edit was too wide.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  git add .aiarch/state/project.json server/internal/resourceaccess/projectstate server/internal/manager
  git commit -F - <<'MSG'
  feat(projectstate): arm the per-activity CAS on the twelve-verb facet

  withActivityVersion has stamped cs.Version++ on every applied transition since
  stage 3 and has never compared it: every call site passed a fabricated zero,
  because no caller could fill the parameter. Every caller can now — the row
  comes back on the readProject the child already makes — so the eleven mutating
  verbs take an expectedActivityVersion and a stale one is refused with Conflict,
  naming both numbers.

  This is a stage-4 ENTRY criterion and it lands here, in the old packages,
  deliberately: 4b's pump starts a child for every eligible activity, and this
  is the last commit where these verbs' contract can change while exactly one
  child per project exists. Landing it before the package collapse also means
  the nineteen replay fixtures judge one change, not nine.

  No Temporal command moved: 15 construction and 4 design histories replay
  unchanged, which is why no new GetVersion fence was needed.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 4: Re-key the `{projectId}:phaseAdvance` collision

Both design Managers derive the SAME workflow id — `systemdesignmanager.go:4878-4879` and `projectdesignmanager.go:2586-2587` are both `fmt.Sprintf("%s:phaseAdvance", projectID)`. Today they do not collide because they run on DIFFERENT task queues and never concurrently. Task 6 puts them on one queue, where `SignalWithStartWorkflow`/`ExecuteWorkflow` with `USE_EXISTING` would silently JOIN the other rail's workflow. This is the one workflow id 4a changes (R2), and it lands ahead of the merge so the merge commit does not also have to debug it.

**Files:**
- Modify: `server/internal/manager/systemdesign/systemdesignmanager.go` — `phaseAdvanceWorkflowID` (`:4876-4880`) and the doc comment at `:914`.
- Modify: `server/internal/manager/projectdesign/projectdesignmanager.go` — `phaseAdvanceWorkflowID` (`:2585-2588`) and the doc comment at `:793`.
- Modify: `server/internal/manager/systemdesign/manager_test.go`, `server/internal/manager/projectdesign/manager_test.go` — any assertion on the literal id.

**Interfaces:** the two helpers keep their names (they are package-private and the packages are still separate); only the returned string changes.

- [ ] **Step 1: Find every reader of the literal.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  grep -rn "phaseAdvance" server/ uitests/ webApp/src systemtests/ docs/ --include='*.go' --include='*.ts' --include='*.md' | grep -v '\.gen\.' 
  ```
  Everything outside the two helpers and their tests is documentation; note it for Task 10's drain note.

- [ ] **Step 2: systemdesign.** Replace `systemdesignmanager.go:4876-4880` with:
  ```go
  // phaseAdvanceWorkflowID derives the continuity token for the short-lived gating
  // workflow: {projectId}:phaseAdvance:systemDesign (systemDesignManager.md §6.1).
  //
  // The rail suffix is NOT decoration. projectDesignManager derived the same
  // {projectId}:phaseAdvance string; the two never collided only because they polled
  // different task queues and never ran at once. Stage 4a puts both on the single
  // `delivery` queue, where USE_EXISTING would silently join the OTHER rail's advance.
  // Existing in-flight advances keep the old id — they are covered by the stage-4 drain,
  // and a phase advance is seconds long.
  func phaseAdvanceWorkflowID(projectID ProjectID) string {
  	return fmt.Sprintf("%s:phaseAdvance:systemDesign", projectID)
  }
  ```

- [ ] **Step 3: projectdesign.** Replace `projectdesignmanager.go:2585-2588` with the twin, suffix `:projectDesign` and the same rationale paragraph (one sentence, pointing at the systemDesign helper rather than repeating it).

- [ ] **Step 4: Fix the two doc comments** that quote the id in prose (`systemdesignmanager.go:914`, `projectdesignmanager.go:793`) so the code and its documentation agree.

- [ ] **Step 5: Run.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off go test ./internal/manager/systemdesign/ ./internal/manager/projectdesign/ -count=1
  GOWORK=off go test ./internal/manager/systemdesign/ ./internal/manager/projectdesign/ -run Test_Replay -count=1
  GOWORK=off make lint fix-check
  ```
  Expected: all green. The replay fixtures are unaffected — a workflow id is a START attribute, not a command in the history being replayed. If a test asserts the literal `":phaseAdvance"`, update the assertion to the new literal (it is testing the id derivation, which is exactly what changed).

- [ ] **Step 6: Commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  git add server/internal/manager/systemdesign server/internal/manager/projectdesign
  git commit -F - <<'MSG'
  fix(design): the two phase-advance workflows stop sharing one id

  systemDesignManager and projectDesignManager both derived
  {projectId}:phaseAdvance. They never collided because they polled different
  task queues and never ran concurrently — an accident of the deployment, not a
  property of the ids. Stage 4a puts both rails on the single `delivery` queue,
  where a start with USE_EXISTING would join the other rail's advance and return
  its result.

  The ids become {projectId}:phaseAdvance:systemDesign and
  {projectId}:phaseAdvance:projectDesign. In-flight advances keep the old id and
  are covered by the stage-4 drain; an advance is seconds long.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 5: Move the derived-plan seam ahead of the package it lives in

`make derived-plan-write` and `make derived-plan-check` both run `./internal/manager/projectdesign/` (`server/Makefile:252`, `:261`) — the package Task 6 deletes. The self-amendment procedure's "slots 9 and 10 are never hand-edited, use `make derived-plan-write`" therefore breaks in the middle of the one commit that may not be split. The tests cannot be physically relocated first (R9: they call five unexported seams), so what moves first is the **seam**: the package name becomes a variable, in its own green commit, and Task 6 flips its default in the same edit that moves the files.

**Files:**
- Modify: `server/Makefile` — the `derived-plan-check` (`:251-252`) and `derived-plan-write` (`:260-261`) recipes, and `construction-state-reset` (`:268`), which runs in the same package.

**Interfaces produced:** `DERIVED_PLAN_PKG` — a Makefile variable, default `./internal/manager/projectdesign/`, consumed by three targets. Task 6 Step 4 changes the default to `./internal/manager/delivery/` and nothing else.

- [ ] **Step 1: Read the three recipes as they stand.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  sed -n '245,275p' Makefile
  ```

- [ ] **Step 2: Introduce the variable**, directly above the `derived-plan-check` target, with the reason:
  ```makefile
  # DERIVED_PLAN_PKG — the package that owns the plan-derivation gate and its writer
  # (TestDerivedPlanMatchesCommittedState / TestWriteDerivedPlan / TestResetConstructionState).
  # A variable rather than a literal because stage 4a MOVES that package: the three targets
  # below are part of the self-amendment procedure ("slots 9 and 10 are never hand-edited"),
  # and that procedure has to keep working DURING the one commit that relocates the package
  # it points at. Flipping one default is a reviewable line; sed-ing three recipes inside a
  # 40-file commit is not.
  DERIVED_PLAN_PKG ?= ./internal/manager/projectdesign/
  ```
  then replace the literal in all three recipes with `$(DERIVED_PLAN_PKG)`. Keep every other token — the `-run` patterns, `-count=1 -v`, the `DERIVED_PLAN_WRITE=1` / `CONSTRUCTION_STATE_RESET=1` guards, and `derived-plan-check`'s `...` suffix — byte-identical.
  - [ ] **Verify first:** `derived-plan-check` uses `./internal/manager/projectdesign/...` (with the ellipsis) and the other two use the bare directory. Preserve that difference: put the ellipsis in the recipe, not in the variable.

- [ ] **Step 3: Prove all three targets still do exactly what they did.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off make derived-plan-check
  GOWORK=off make derived-plan-write
  git -C .. status --short -- .aiarch/state/project.json
  GOWORK=off make -n construction-state-reset
  ```
  Expected: `derived-plan-check` passes; `derived-plan-write` reports `changed 0 member(s): []` and leaves `project.json` clean; `make -n` prints the reset recipe with the package path expanded. **`git status` must show no change to `project.json`** — a diff means slot 9 or 10 has drifted for an unrelated reason and must be understood before Task 6 rewrites them.

- [ ] **Step 4: Commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  git add server/Makefile
  git commit -F - <<'MSG'
  build: put the derived-plan gate's package behind DERIVED_PLAN_PKG

  derived-plan-check, derived-plan-write and construction-state-reset all name
  ./internal/manager/projectdesign/ — the package stage 4a deletes. Those
  targets are part of the self-amendment procedure ("slots 9 and 10 are never
  hand-edited"), so the procedure has to keep working DURING the single,
  indivisible commit that relocates the package it points at.

  One variable, three recipes, no behaviour change: the gate passes and the
  writer is a no-op, exactly as before. Stage 4a's model commit flips the
  default and touches nothing else here.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 6: THE MODEL + PACKAGE COMMIT — one commit, seventeen steps, in this order

This is the commit stage 1 could not carry. Five Error-severity gates make it indivisible: `SYS-CARD-MGR` (a 6th Manager), `ALIGN-EXTRA-PKG` (a package with no component), `ALIGN-MISSING-PKG` (a component with no package), `DH-CONTRACT-FACET` (a contract with no component) and `USECASE-DYNAMIC-MISSING` (a use case with no view). Work the steps in the printed order and stop at each **GREEN POINT** — the tree does not compile between most of them, and that is expected; the green points are where it must.

**Files** (`.aiarch/state/project.json` unless noted):
- `slots["5"].model.components` — append `delivery-manager`; DELETE indices 3, 4, 5 (`system-design-manager`, `project-design-manager`, `construction-manager`). 37 → 35 components.
- `slots["5"].model.relationships` — delete 37 edges (indices `0 1 2 5 6 7 10 13 14 15 16 17 18 19 25 26 27 28 29 30 31 39 40 41 42 43 44 58 59 60 61 62 63 70 72 73 74` — **measured at `4baed01a`; recompute, never trust this list**), add 17. 76 → 56.
- `slots["5"].model.dynamicViews` — delete `uc1-drive-system-design` and `uc3-execute-construction-activity`; re-key 11; add `uc-execute-project-activity`. 18 → 17.
- `slots["4"].model.decisions` — delete 2, demote 1, append 1, re-point 11 `variationOf`. 18 → 17.
- `slots["3"].model.waivers` — the transitional-facet-group waiver sentence expires.
- `slots["6"].model.deployment.containers[0].components` — the three names at indices 3, 5, 6 become one `DeliveryManager`.
- `serviceContracts.deliveryManager` — new; `serviceContracts.{systemDesignManager,projectDesignManager,constructionManager}` — deleted.
- Create: `server/internal/manager/delivery/` — `deliverymanager.go`, eleven workflow files, one `manager_test.go`, `testdata/replay/**`.
- Delete: `server/internal/manager/{systemdesign,projectdesign,construction}/`.
- Modify: `server/cmd/clientgen/main.go:62`, `server/cmd/appgen/main.go:84` **and** `:155`, `server/cmd/clientgen/mcpdocs.go`, `server/internal/arch_test.go`, `server/internal/registered_names_test.go`, `server/internal/engine/designhealth/engine_test.go`, `server/cmd/server/managerlog.go`, `server/cmd/server/hooks.go`, `server/Makefile` (`DERIVED_PLAN_PKG`).
- Modify (a SEPARATE Go module, Step 14b): `systemtests/internal/harness/{transport,httptransport,mcptransport,enums,server,steps,construction_seed}.go`; and `.testingState.systemTestPlan.useCaseIndex` in `project.json`, whose four references to the two deleted use cases are re-pointed.
- Regenerate: everything `make gen-*` emits, `server/api/openapi.yaml`, `systemtests/internal/sdk/**` (via `make gen-sdk`'s prune-stale rewrite), `webApp/src/contracts/schema.ts`, `webApp/src/api/ops.gen.ts`.

**Interfaces produced (Tasks 7–9 depend on these exact names):**
- Go package `internal/manager/delivery`, task queue const `TaskQueue = "delivery"`, interface `DeliveryManager`, constructor `NewDeliveryManager(...)`, exported `MaterializeActivityPlan`, `RegisterManagerWorker`, `RegisterWorker`, `RegisterSchedules`.
- Contract key `deliveryManager`, component id `delivery-manager`, component name `DeliveryManager`.
- Twelve op names: `StartProject`, `ExecuteNextActivity`, `DispatchActivityTask`, `SubmitReviewDecision`, `AskQuestions`, `AcknowledgeStaleBasis`, `SetProjectRunState`, `OverrideActivity`, `ReplanProject`, `SetProjectExecutionPolicy`, `QueryProjectView`, `QueryActivityView`.
- REST base path `/api/v1/delivery/…`; op ids `delivery<PascalOp>` in `ops.gen.ts`; MCP tool names `delivery<PascalOp>`.
- New `$defs`: `ProjectRunState`, `ProjectViewKind`, `ProjectViewQuery`, `ProjectView`, `ExecutionPolicyInput`, `ReviewDecisionInput`, `StartProjectResult`, `ProjectSessionStage`, `ProjectSessionStateView`.
- Schedules re-registered as `delivery:pumpSweep` (30 s) and `delivery:replanSweep` (300 s).

---

- [ ] **Step 1: Re-measure everything this commit is about to trust.** Never trust an index list written on another day.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  jq -r '.slots["5"].model.relationships | to_entries[] | select((.value.from|IN("system-design-manager","project-design-manager","construction-manager")) or (.value.to|IN("system-design-manager","project-design-manager","construction-manager"))) | .key' .aiarch/state/project.json | tr '\n' ' '; echo
  jq -r '.slots["5"].model.components | to_entries[] | select(.value.id|IN("system-design-manager","project-design-manager","construction-manager")) | "\(.key)\t\(.value.id)"' .aiarch/state/project.json
  jq -r '.slots["5"].model.dynamicViews[] | "\(.key)\t\(.useCaseId)"' .aiarch/state/project.json
  jq -r '.slots["4"].model.decisions | to_entries[] | "\(.key)\t\(.value.useCase.id)\t\(.value.useCase.classification)\t\(.value.useCase.variationOf // "-")"' .aiarch/state/project.json
  jq -r '.slots["6"].model.deployment.containers[0].components | to_entries[] | select(.value|test("SystemDesignManager|ProjectDesignManager|ConstructionManager")) | "\(.key)\t\(.value)"' .aiarch/state/project.json
  jq -r '
    def utils: ["security","logging","diagnostics","message-bus"];
    . as $r |
    ($r.slots["5"].model.dynamicViews | map({uc:.useCaseId, calls:[.steps[].calls[] | "\(.from)|\(.to)|\(.mode)"]})) as $views |
    $r.slots["5"].model.relationships
    | map(select(.to as $t | (utils | index($t)) | not))
    | map(. as $rel | ("\($rel.from)|\($rel.to)|\($rel.mode)") as $k |
        {k:$k, by: [$views[] | select(.calls | index($k)) | .uc]})
    | map(select((.by|length) > 0 and ((.by - ["drive-system-design","commit-to-a-project-option","execute-a-construction-activity"])|length) == 0))
    | .[] | .k' .aiarch/state/project.json
  ```
  Expected at `4baed01a`: 37 indices exactly as listed in **Files**; components at 3/4/5; 18 views and 18 decisions; deployment components at 3 (`ConstructionManager`), 5 (`ProjectDesignManager`), 6 (`SystemDesignManager`); twelve sole-exerciser rows — `scheduler-client→construction-manager`, `construction-manager→{intervention-engine, review-engine, artifact-access, source-control-access, episode-access}`, `system-design-manager→{review-engine, episode-access}`, `project-design-manager→review-engine`, `episode-access→project-git-repo`, `artifact-access→project-git-repo`, `agentic-job-access→construction-pipeline-runtime`. **The last three are RA→Resource rows that survive the Manager deletion and MUST be exercised by the new view** (Step 8).
  Save all of it to the scratchpad; Step 15 re-runs the last query with the new view's id excluded and it must print nothing.

- [ ] **Step 2: Create the package and move the eleven workflow files, bodies unchanged.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server/internal/manager
  mkdir -p delivery/testdata
  git mv systemdesign/systemdesignphase.go        delivery/systemdesignphase.go
  git mv systemdesign/coauthorartifact.go         delivery/coauthorartifact.go
  git mv systemdesign/phaseadvance.go             delivery/phaseadvance.go
  git mv projectdesign/coauthorphase2artifact.go  delivery/coauthorphase2artifact.go
  git mv projectdesign/assemblesdpreview.go       delivery/assemblesdpreview.go
  git mv projectdesign/phase2advance.go           delivery/phase2advance.go
  git mv construction/constructactivity.go        delivery/constructactivity.go
  git mv construction/pumpnextactivity.go         delivery/pumpnextactivity.go
  git mv construction/pumpsweep.go                delivery/pumpsweep.go
  git mv construction/replansweep.go              delivery/replansweep.go
  git mv construction/projectsupervision.go       delivery/projectsupervision.go
  git mv construction/testdata/replay             delivery/testdata/replay
  git mv systemdesign/testdata/replay/design-pre-stage4  delivery/testdata/replay/design-pre-stage4
  git mv projectdesign/testdata/replay/phase2-pre-stage4 delivery/testdata/replay/phase2-pre-stage4
  ```
  - **The two design fixture directories keep their names**, because Task 1 captured them under the final names on purpose. `designReplayCases()`'s `c.dir == "design-pre-stage4"` and `phase2ReplayCases()`'s `c.dir == "phase2-pre-stage4"` are therefore correct as they stand and **no step edits them** — Step 2c merges the three replay tests into one walking all three directory groups, and Step 13 finds all nineteen fixtures by the strings already in the code. Confirm after the moves:
    ```bash
    ls delivery/testdata/replay
    ```
    Expected: `design-pre-stage4  phase2-pre-stage4  post-b1  post-b17  post-stage3  pre-b1  pre-d`.
  The three impl files and the three test files are NOT `git mv`'d — they MERGE (Steps 2a/2c). Change the `package` clause of every moved file to `package delivery`.
  - **The eleven workflow file names are already correct** and must not be renamed: `arch.CheckFileLayout` computes `want = strings.ToLower(strings.TrimSuffix(entryFunc, "Workflow")) + ".go"`, which for `SystemDesignPhaseWorkflow` / `CoAuthorArtifactWorkflow` / `PhaseAdvanceWorkflow` / `CoAuthorPhase2ArtifactWorkflow` / `AssembleSDPReviewWorkflow` / `Phase2AdvanceWorkflow` / `PumpNextActivityWorkflow` / `ConstructActivityWorkflow` / `PumpSweepWorkflow` / `ReplanSweepWorkflow` / `ProjectSupervisionWorkflow` gives exactly the eleven names above.
  - **Do not delete the three old directories yet** — their `.gen.go` files are the reference for Steps 2a and 3.

- [ ] **Step 2a: Merge the three impl files into ONE `delivery/deliverymanager.go`.** `arch.CheckFileLayout` computes `implFile = leaf + FileStereotype + ".go"` = `deliverymanager.go` (`framework-go@v0.11.1/arch/filelayout.go:110`), and a handwritten file that is neither the impl file nor a one-entry-func workflow file is `file-not-allowed`. So `systemdesignmanager.go` (5,255) + `projectdesignmanager.go` (3,451) + `constructionmanager.go` (3,749) concatenate into one file, in that order, each under a banner comment naming its origin:
  ```go
  // ---------------------------------------------------------------------------
  // SYSTEM-DESIGN RAIL — moved verbatim from internal/manager/systemdesign/
  // systemdesignmanager.go at stage 4a. Bodies are unchanged; only package-private
  // names that collided with another rail were renamed (the collision table is in
  // docs/superpowers/plans/2026-09-25-activity-experience-stage4a.md, Task 6 Step 2b,
  // and in this commit's message). 4b replaces this block with the generic DAG child.
  // ---------------------------------------------------------------------------
  ```
  The three `newXManager` builders and the three `WorkerManifest()` methods survive as package-private; the exported registration entrypoints collapse to one each (Step 2b class B).
  - **No `workflow.Context`-taking func may live in `deliverymanager.go`** (`workflow-in-impl-file`, Error):
    ```bash
    grep -n "workflow\.Context" server/internal/manager/delivery/deliverymanager.go
    ```
    Expected: nothing. If a helper came across, move it into the workflow file that calls it.

- [ ] **Step 2b: Resolve the package-private name collisions.** Measured at `4baed01a` by extracting every package-level `func`/`type`/`const`/`var` from the non-generated files of the three packages and intersecting. Four classes; handle each class the same way every time.

  **Step 2b.0 — dump the measured collision set to the scratchpad FIRST, and work it as a checklist.** ~180 collisions with the compiler as the only oracle is a build log, not a plan; the list below is dated `4baed01a` and the tree has moved twice since (Tasks 3 and 4 both edited these files). Re-derive it before touching a symbol:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server/internal/manager
  S=/private/tmp/claude-501/-Users-davidmarne-mixofrealitystudio-archistrator/*/scratchpad
  for d in systemdesign projectdesign construction; do
    for f in $d/*.go; do case "$f" in *_test.go|*.gen.go) continue;; esac
      awk '
        /^(const|var|type) \(/ { blk=1; next }
        blk && /^\)/           { blk=0; next }
        blk && /^\t[A-Za-z_][A-Za-z0-9_]*/ { match($0, /^\t[A-Za-z_][A-Za-z0-9_]*/); print substr($0, RSTART+1, RLENGTH-1); next }
        /^func [A-Za-z_][A-Za-z0-9_]*\(/   { match($0, /^func [A-Za-z_][A-Za-z0-9_]*/); print substr($0, 6, RLENGTH-5); next }
        /^(type|const|var) [A-Za-z_]/      { split($0, a, " "); print a[2]; next }
      ' "$f"
    done | sort -u > $S/pkgnames-$d.txt
  done
  comm -12 $S/pkgnames-systemdesign.txt $S/pkgnames-projectdesign.txt > $S/collide-sd-pd.txt
  comm -12 $S/pkgnames-systemdesign.txt $S/pkgnames-construction.txt  > $S/collide-sd-cs.txt
  comm -12 $S/pkgnames-projectdesign.txt $S/pkgnames-construction.txt > $S/collide-pd-cs.txt
  wc -l $S/collide-*.txt
  ```
  Then classify every line of the three files into A/B/C/D by the table below, tick them off as you go, and keep the annotated list — it is the source of the class-D rename table the commit message needs.

  | Class | Rule | Examples (measured) |
  |---|---|---|
  | **A. Generated-type collisions** | **No action.** They come from the three `contract.gen.go`/`activities.gen.go`/`worker.gen.go` files and are regenerated ONCE from the merged contract in Step 12. | `ProjectID`, `ArtifactKind`, `ReviewDecision`, `ReviewFeedback`, `SessionRef`, `EpisodeKind`, `EpisodeOutcome`, `EpisodeRecordView`, `EpisodeTimeline`, `EpisodeUsage`, `EpisodeLineage`, `SubagentSpan`, `TimelineEvent`, `AnchoredComment`, `ActiveRole`, `ActiveStep`, `Finding`, `Location`, `RuleID`, `Severity`, `DraftModel`, `ReviewCommentView`, `ReviewCommentReply`, `PhaseAdvanceResult`, the seventeen `Kind*` consts, `TaskQueue`, `genActivities`, `genInvokers`, `genRegisteredWorkflow`, `genWorkerManifest`, `RegisterWorker` |
  | **B. Exported registration entrypoints** | Keep exactly ONE of each, on the merged manager. | `RegisterManagerWorker`, `RegisterWorker`, `RegisterSchedules`, `TaskQueue`, `MaterializeActivityPlan` |
  | **C. Byte-identical private helpers** | Keep ONE copy, delete the others, put a one-line comment on the survivor naming the rails it now serves. This IS the mechanical twin collapse — forced by the compiler, not chosen. | `engineReviewPolicy` (3 copies: `constructactivity.go:954`, `coauthorartifact.go:4361`, `coauthorphase2artifact.go:48`), `episodeIDSeed`, `episodeLineage`, `episodeVenueIsRemote`, `episodeRecordToView`, `episodeRecordViews`, `episodeOutcomeFrom`, `episodeSubagentSpans`, `episodeTimelineEvents`, `episodeTraceEventType`, `episodeViewKind`, `episodeViewOutcome`, `episodeGapReason`, `episodeGapRecord`, `episodeIDSafe`, `findEpisodeRecord`, `appendEpisodeActivityOptions`, `appendEpisodeRetryWindow`, `mapQueryError`, `mapRAError`, `mapSignalError`, `mapStartError`, `mapReadProjectError`, `newError`, `isConflict`, `isNotFound`, `isRAConflict`, `isReadNotFound`, `isEpisodeTraceNotFound`, `strPtrOrNil`, `mainBranch`, `maxMutateConflictAttempts`, `pipelineObservation`, `pipelineDefaultToolchain`, `lateEpisodePollInterval`, `maxLateEpisodePolls`, `railCredEnvelope`, `pullRequestStatusView`, `raConflictErrType`, `raNotFoundErrType`, `raContractMisuseErrType`, `genActivityIdempotencyKey`, `genDefaultActivityOptions`, `readProjectActivityOptions`, `mintCredActivityOptions`, plus the ~28 `design*`/`ledger*` helpers the two co-author twins share (`designPRTitle`, `designPRBody`, `designArchApprovalBody`, `designApproverActor`, `designRoundKeyFor`, `designRoundID`, `designRoundIDPrefix`, `designRoundReviewers`, `designSubjectRef`, `designActivityFor`, `designPipelinePhase`, `ledgerRoundBase`, `feedbackToLedgerComments`, `newSlotCommentIDs`, `psActivityTypeFor`, `stageForAttempt`, `approveFailedReason`, `draftFailedReason`, `rejectFailedReason`, `stageFailedReason`, `railStepFailedReason`, `mergeRedraftFeedback`, `reviewFeedbackOrZero`, `dispatchErrSummary`, `isRailAuthFault`, `isTerminalReadBack`, `failedSessionView`, `wedgedSupersedeReason`) |
  | **D. Same name, DIFFERENT body** | Prefix with the rail: `sd`/`pd`/`cs` for a short identifier, `systemDesign`/`projectDesign`/`construction` where the name reads as prose. | `workflows` → `sdWorkflows`/`pdWorkflows`/`csWorkflows`; `querySessionState`, `signalReviewDecision`, `signalSetCommentStatus`, `redraftSignal`, `reviewDecisionSignal`, `setCommentStatusSignal`, `coAuthorInput`, `coAuthorState`, `coAuthorStep`, `coAuthorOutcome`, `coAuthorApproved`, `coAuthorUnknown`, `coAuthorWithdrawn`, `coAuthorWorkflowID`, `phaseAdvanceInput`, `phaseAdvanceWorkflowID`, `executionKindCoAuthor`, `executionKindPhaseAdvance`, `gitSession`, `modelEnvelope`, `projectEnvelope`, `designRound`, `designRoundKey`, `dispatchDesignJobArgs`, the six `dispatchInput*` consts, `jobModeAnswer`/`jobModeDraft`, the six `answerEpisode*` consts, `observePollInterval`, `maxObservePolls`, `designWorkflowFileName`, `designActorOperator`, `designRoleHuman`, `autoApproverVibes`, `acknowledgeStaleMaxAttempts`, `askQuestionsMaxAttempts`, `slotFor`, `encodeModel`, `encodeProject`, `draftModelFor`, `committedSessionView`, `sessionStageLabel`, `sessionStageIsLive`, `isLiveSessionStage`, `nextQuestionRound`, `existingQuestionRound`, `checkCommentTransition`, `checkNoReplyTo`, `checkReviewPrecondition`, `openReviewCommentViewIDs`, `toReviewCommentView`, `toViewReplies`, `reviewThreadToView`, `questionsToLedger`, `principalLabel`, `workflowStatusLabel`, `isAbnormalClosedStatus`, `isWorkflowTaskFailedQueryErr`, `terminatedSessionReason`, `predecessorNotCommittedMsg`, `newSessionRef`, `withStageName`, `waitOrDone`, `validateReviewDecisionArgs`, `signalNotes`, `acknowledgeStaleIdempotencyKey`, `askQuestionsIdempotencyKey`, `answerJobDispatchKey`, `answerJobDispatchSeq`, the six `railAuthRetry*` consts, `artifactKindString`, `artifactKindWireName`, `mutateActivityOptions`, `observeActivityOptions`, `dispatchActivityOptions`, `failActivityOptions`, `railDraftedBy` |

  - **Do not batch-rename with `sed`.** Rename one symbol at a time with the compiler as the oracle — `GOWORK=off go build ./internal/manager/delivery/ 2>&1 | head -40`, fix the top error, repeat. A `sed` over 60k lines WILL hit a substring inside a string literal, including the wire-visible enum varnames.
  - **Record every class-D rename in a table in the commit message.** A reader of `coauthorartifact.go` must be able to find the symbol the old blame names.
  - **Class C is where 4a's line reduction actually comes from**, and it is bounded: helpers only, never a workflow body. Verify identity MECHANICALLY before collapsing a pair; if collapsing needs ANY edit to either body it is class D — prefix both and earmark the pair for 4b:
    ```bash
    cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server/internal/manager/delivery
    for f in constructactivity.go coauthorartifact.go coauthorphase2artifact.go; do
      awk '/^func engineReviewPolicy/,/^}/' $f | shasum | cut -c1-12
    done
    ```
    Equal digests ⇒ class C. Unequal ⇒ class D, no exceptions.
  - `Test_EngineReviewPolicy_CarriesTheStoredDocument` exists in all three test files and pins the parity of those three copies. When the three become one, the three tests become ONE, and its doc comment says the parity it pinned is now structural.

- [ ] **Step 2c: Merge the three `manager_test.go` into ONE.** `testFileNameViolations` (`filelayout.go:191-208`) flags EVERY `_test.go` in the package directory whose name is not `manager_test.go`, so the package gets exactly one test file: 13,146 + 7,461 + 11,191 = **31,798 lines** before the class-C collapse. Concatenate in the Step-2a rail order under the same banner comments, then resolve test-side collisions by the Step-2b classes (expect a large class-C collapse among fixture builders and fake constructors). The capture/replay tests from Task 1 merge with construction's into ONE capture tool and ONE replay test walking all three fixture directories.
  - Put a header comment at the top of the merged file saying WHY it is the largest file in the repo — `arch.CheckFileLayout`'s one-test-file rule — and pointing at 4b, which deletes the two co-author blocks outright.

- [ ] **GREEN POINT A — every collision is resolved.** The package cannot compile yet (no generated files), so prove the collision work instead:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off go vet ./internal/manager/delivery/ 2>&1 | grep -vE "^#|undefined: (DeliveryManager|NewDeliveryManager|genActivities|genInvokers|genWorkerManifest|genRegisteredWorkflow|RegisterWorker|TaskQueue)" | head -40
  ```
  Expected: empty. Anything printed that is not an undefined generated symbol is an unresolved collision. **Do not proceed while this prints anything.**

- [ ] **Step 3: Resolve the three `$defs` conflicts.** Measured: exactly three names exist in more than one of the three contracts with a DIFFERENT body — `AnchoredComment`, `SessionStage`, `SessionStateView`. Every other shared name is byte-identical, so the merged contract takes one copy.
  - **`AnchoredComment`** (R-C): the two design bodies are identical; construction's is the same minus `anchorText`. The merged def is the design shape, verbatim:
    ```json
    "AnchoredComment": {
      "type": "object",
      "properties": {
        "jsonPath": { "type": "string", "x-go-name": "JSONPath" },
        "text": { "type": "string" },
        "anchorText": { "type": "string" },
        "replyTo": { "type": "string" }
      },
      "required": ["jsonPath", "text", "anchorText", "replyTo"],
      "additionalProperties": false
    }
    ```
    Construction's Go literals are keyed, so they compile unchanged and carry a zero `AnchorText`. Confirm none is positional:
    ```bash
    grep -rn "AnchoredComment{" server/internal/manager/delivery/ | grep -v "JSONPath:" | head
    ```
    Expected: nothing.
  - **`SessionStage` / `SessionStateView`** (R-D): the two `SessionStage` enums are NOT compatible — the projectDesign one inserts `StageAssemblingSDP` at ordinal 2 and shifts `StageAwaitingReview` (2→3) … `StageDraftFailed` (7→8). Merging renumbers a wire-visible ordinal, which is forbidden. Carry BOTH:
    ```json
    "SessionStage": {
      "type": "integer",
      "enum": [0, 1, 2, 3, 4, 5, 6, 7],
      "x-enum-varnames": ["SessionStageUnknown", "StageDrafting", "StageAwaitingReview", "StageRedrafting", "StageCommitted", "StageWithdrawn", "StageRefused", "StageDraftFailed"],
      "x-go-base": "int"
    },
    "ProjectSessionStage": {
      "type": "integer",
      "enum": [0, 1, 2, 3, 4, 5, 6, 7, 8],
      "x-enum-varnames": ["ProjectSessionStageUnknown", "ProjectStageDrafting", "ProjectStageAssemblingSDP", "ProjectStageAwaitingReview", "ProjectStageRedrafting", "ProjectStageCommitted", "ProjectStageWithdrawn", "ProjectStageRefused", "ProjectStageDraftFailed"],
      "x-go-base": "int"
    }
    ```
    plus `SessionStateView` (the systemDesign body verbatim) and `ProjectSessionStateView` (the projectDesign body verbatim with `"stage": { "$ref": "#/$defs/ProjectSessionStage" }`). The varname prefix is forced: two Go enums in one package cannot share a constant name. Rename the projectDesign rail's Go references — they are confined to `coauthorphase2artifact.go`, `assemblesdpreview.go` and the projectDesign block of `deliverymanager.go`.
    - [ ] **Verify first:** the webApp already calls these two `SessionStage` and `ProjectSessionStage` (`webApp/scripts/gen-enums.mjs:52-53`). Keeping these def names is what lets Task 7 map `DeliverySessionStage → 'SessionStage'` and `DeliveryProjectSessionStage → 'ProjectSessionStage'` with NO type rename in the SPA. Do not invent other names.

- [ ] **Step 4: Flip `DERIVED_PLAN_PKG` and ALL THREE manager lists** — before any slot 9/10 write (R9). There are three, not two, and the one the plan's first draft missed is the one that generates the worker.
  - `server/Makefile`: `DERIVED_PLAN_PKG ?= ./internal/manager/delivery/`
  - `server/cmd/clientgen/main.go:62-67` — the WEB/MCP client layer:
    ```go
    var exposedManagers = []string{
    	"deliveryManager",
    	"operationsManager",
    }
    ```
    Rewrite the comment above it — it says "the 5 managers mounted by the former hand-written web.go", which is now two.
  - `server/cmd/appgen/main.go:155-160` — the composition root's web mounts:
    ```go
    		WebExposedManagers: []string{
    			"deliveryManager",
    			"operationsManager",
    		},
    ```
  - `server/cmd/appgen/main.go:84` — **the temporalgen + SDK list.** This is the one that emits `contract.gen.go`, `activities.gen.go`, `invokers.gen.go` and `worker.gen.go` (the loop at `:98`) and the systemtests SDK (`generateSDK`, `:311`). It carries `billingManager`, which the other two deliberately exclude, so it is NOT the same list:
    ```go
    var managers = []string{"deliveryManager", "operationsManager", "billingManager"}
    ```
    and correct `generateSDK`'s doc comment at `:302-303`, which says "for the 5 managers", to three.
  - [ ] **Verify first:** all three lists differ in membership by design — `exposedManagers`/`WebExposedManagers` are the WEB-WIRED set (billing is excluded because it is not web-wired) and `managers` is the CODE-GENERATED set (billing has a Temporal worker and an SDK). Do not unify them; set each to its own correct value.
    ```bash
    cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
    grep -n "systemDesignManager\|projectDesignManager\|constructionManager" cmd/appgen/main.go cmd/clientgen/main.go
    ```
    Expected after the edit: nothing.

- [ ] **Step 5: Author `serviceContracts.deliveryManager` — deps and the twelve ops.** Top-level shape copied from `serviceContracts.constructionManager` (keys in order: `$defs`, `component`, `deps`, `goPackage`, `interface`, `layer`, `title`).
  ```json
  {
    "component": "deliveryManager",
    "layer": "Manager",
    "goPackage": "internal/manager/delivery",
    "title": "DeliveryManager",
    "deps": [
      { "name": "client", "goType": "client.Client", "goImport": "go.temporal.io/sdk/client" },
      { "name": "projectState", "component": "projectStateAccess" },
      { "name": "artifact", "component": "artifactAccess" },
      { "name": "intervention", "component": "interventionEngine" },
      { "name": "review", "component": "reviewEngine" },
      { "name": "estimator", "component": "estimationEngine" },
      { "name": "operationEstimator", "component": "operationEstimationEngine" },
      { "name": "billingEstimator", "component": "billingEngine" },
      { "name": "pipeline", "component": "agenticJobAccess" },
      { "name": "rail", "component": "sourceControlAccess" },
      { "name": "constructionTransition", "component": "constructionTransitionAccess" },
      { "name": "gitStatus", "component": "gitActivityStatusAccess" },
      { "name": "designSession", "component": "designSessionAccess" },
      { "name": "activityExecution", "component": "activityExecutionAccess" },
      { "name": "messageBus", "component": "messageBus" },
      { "name": "episodes", "component": "episodeAccess" },
      { "name": "escalationWaitTimeout", "goType": "time.Duration", "goImport": "time" },
      { "name": "interventionMode", "goType": "string" },
      { "name": "repo", "goType": "func(projectID ProjectID) (sourcecontrol.RepoRef, bool)", "goImport": "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol" },
      { "name": "repoBase", "goType": "string" }
    ]
  }
  ```
  This is the UNION of the three, unchanged (R-E). The three deprecated facets stay: their 26 registered activity names must survive the drain, and dropping them is 4b's post-drain work. `designHealthEngine` is NOT a dep — `systemdesignmanager.go:164` constructs `designhealth.NewEngine()` inline and 4a does not change that.

  The twelve operations:
  ```json
  [
    { "name": "StartProject",
      "params": [ { "name": "owner", "schema": { "$ref": "#/$defs/OwnerScope" } },
                  { "name": "name", "schema": { "type": "string" } },
                  { "name": "projectID", "pointer": true, "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "model", "pointer": true, "schema": { "$ref": "#/$defs/OperatingModel" } },
                  { "name": "research", "pointer": true, "schema": { "$ref": "#/$defs/ResearchInput" } },
                  { "name": "start", "schema": { "type": "boolean" } } ],
      "result": { "$ref": "#/$defs/StartProjectResult" }, "error": true },
    { "name": "ExecuteNextActivity",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "tickID", "schema": { "type": "string" } } ],
      "result": { "$ref": "#/$defs/PumpResult" }, "error": true },
    { "name": "DispatchActivityTask",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } },
                  { "name": "taskID", "schema": { "type": "string" } },
                  { "name": "feedback", "pointer": true, "schema": { "$ref": "#/$defs/ReviewFeedback" } } ],
      "result": { "$ref": "#/$defs/SessionRef" }, "error": true },
    { "name": "SubmitReviewDecision",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } },
                  { "name": "taskID", "schema": { "type": "string" } },
                  { "name": "decision", "schema": { "$ref": "#/$defs/ReviewDecisionInput" } },
                  { "name": "feedback", "pointer": true, "schema": { "$ref": "#/$defs/ReviewFeedback" } } ],
      "error": true },
    { "name": "AskQuestions",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } },
                  { "name": "taskID", "schema": { "type": "string" } },
                  { "name": "addressee", "schema": { "type": "string" } },
                  { "name": "questions", "schema": { "type": "array", "items": { "$ref": "#/$defs/AnchoredComment" } } } ],
      "error": true },
    { "name": "AcknowledgeStaleBasis",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } },
                  { "name": "taskID", "schema": { "type": "string" } },
                  { "name": "note", "schema": { "type": "string" } } ],
      "error": true },
    { "name": "SetProjectRunState",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "runState", "schema": { "$ref": "#/$defs/ProjectRunState" } },
                  { "name": "reason", "schema": { "type": "string" } } ],
      "error": true },
    { "name": "OverrideActivity",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } },
                  { "name": "override", "schema": { "$ref": "#/$defs/ActivityOverride" } } ],
      "error": true },
    { "name": "ReplanProject",
      "params": [ { "name": "projectID", "pointer": true, "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "tickID", "schema": { "type": "string" } } ],
      "result": { "$ref": "#/$defs/ReplanSweepResult" }, "error": true },
    { "name": "SetProjectExecutionPolicy",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "policy", "schema": { "$ref": "#/$defs/ExecutionPolicyInput" } } ],
      "error": true },
    { "name": "QueryProjectView",
      "params": [ { "name": "query", "schema": { "$ref": "#/$defs/ProjectViewQuery" } } ],
      "result": { "$ref": "#/$defs/ProjectView" }, "error": true },
    { "name": "QueryActivityView",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } } ],
      "result": { "$ref": "#/$defs/ActivityView" }, "error": true }
  ]
  ```
  Exactly twelve. `DH-CONTRACT-OPCOUNT-MAX` fires above 12 and `DH-CONTRACT-OPCOUNT-REJECT` (Error) at ≥20, so at 12 neither fires — the first Manager contract in the repo that is clean on op count.
  - `AskQuestions` takes `[]AnchoredComment`, NOT `[]string`. The live op does; `[]string` would silently drop every question's anchor, a behaviour change R13 forbids. This CORRECTS the stage-1 plan's Step 4.
  - [ ] **Verify first:** `required`/`pointer` in this dialect are PRESENCE-only. Never add `minLength` (2026-08-13 contract-strictness ruling).

- [ ] **Step 6: The `$defs`.** COPY VERBATIM from the source contracts — do not re-derive, or the generated Go stops being structurally identical and every wire consumer moves.

  | From | Defs to copy (take each def and everything it transitively `$ref`s) |
  |---|---|
  | `constructionManager` | `ActivityID`, **`ActivityView`** and its whole closure (`ActivityLifecyclePhase`, `ActivityTaskKind`, `ActivityTaskState`, `ActivityTaskView`, `ActivityViewState`, `TaskRevisionView`, `TaskRevisionComment`, `TaskRevisionOutcome`, `TaskRevisionProvenance`), `ActivityOverride`, `OverrideKind`, `PumpResult`, `PumpStatus`, `ReplanSweepResult`, `ReviewPolicyInput`, `ReviewSet`, `Reviewer`, `ReviewRosterSeat`, `ReviewSubjectRef`, `ReviewThreadComment`, `ReviewThreadReply`, `ReviewVerdictKind`, `ReviewVerdictView`, `ConstructionSessionView`, `ConstructionStage`, `PhaseDecision`, `PipelinePhase`, `FlaggedVariance` |
  | `systemDesignManager` | `ProjectID`, `OwnerScope`, `ResearchInput`, `ResearchSource`, `OperatingModel`, `Version`, `Phase`, `PhaseAdvanceResult`, `ProjectState` and its closure (`ArtifactSlotView`, `ArtifactStage`, `ArtifactSlotModel`, `ActivityConstructionStatus`, `ActivityGitStatus`, `ActivityMethodPhase`, `ActivityType`, `ActivityBuildStatus`, `ActivityConstructionPhase`, `AttemptProvenance`, `CheckItem`, `CICheckState`, `ConstructionProgress`, `ContractOp`, `ContractParty`, `ContractRevision`, `ContractStruct`, `CritiqueView`, `DefectView`, `EvidenceRef`, `EVCurve`, `EvPoint`, `FailureReason`, `GoField`, `NoteComment`, `OperatorNote`, `OperatorNoteKind`, `PendingDependency`, `PendingResume`, `PhaseCompletion`, `ProducedArtifact`, `ReviewPolicyView`, `ServiceContract`, `SystemTestPlanView`, `TaskAttempt`, `TestingStateView`, `TestingVariant`, and the `Test*View` family), `ProjectSummary`, `DesignHealth`, `Finding`, `RuleID`, `Severity`, `Location`, `ArtifactKind`, `ReviewDecision`, `ReviewFeedback`, `AnchoredComment`, `ReviewCommentView`, `ReviewCommentReply`, `DraftModel`, `SessionRef`, `SessionStage`, `SessionStateView`, `ActiveRole`, `ActiveStep`, `EpisodeKind`, `EpisodeOutcome`, `EpisodeLineage`, `EpisodeRecordView`, `EpisodeTimeline`, `EpisodeUsage`, `SubagentSpan`, `TimelineEvent` |
  | `projectDesignManager` | `OptionID`, `SDPDecision`, `ProjectSessionStage` (renamed, Step 3), `ProjectSessionStateView` (renamed, Step 3) |
  | NEW | `ProjectRunState`, `ProjectViewKind`, `ProjectViewQuery`, `ProjectView`, `ExecutionPolicyInput`, `ReviewDecisionInput`, `StartProjectResult` |

  Take the closure MECHANICALLY, not by reading:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  jq -r '.serviceContracts.systemDesignManager["$defs"] | tostring' .aiarch/state/project.json | grep -oE '#/\$defs/[A-Za-z0-9_]+' | sort -u
  ```
  and repeat per contract until the reachable set stops growing. `ActivityView` is taken AS-IS from `constructionManager` — stage 0 designed it as the Activity Experience's single read, stage 5's fixtures are validated against it, and changing it here would break stage 5 for nothing.

  The seven new defs, in the repo's string-enum idiom (`x-enum-varnames` + `x-go-base`, copied from `ArtifactStage`):
  ```json
  "ProjectRunState": {
    "type": "string",
    "enum": ["running", "paused"],
    "x-enum-varnames": ["ProjectRunning", "ProjectPaused"],
    "x-go-base": "string"
  },
  "ProjectViewKind": {
    "type": "string",
    "enum": ["summary", "projects", "session", "pump", "designHealth", "episodes", "timeline"],
    "x-enum-varnames": ["ProjectViewSummary", "ProjectViewProjects", "ProjectViewSession", "ProjectViewPump", "ProjectViewDesignHealth", "ProjectViewEpisodes", "ProjectViewTimeline"],
    "x-go-base": "string"
  },
  "ProjectViewQuery": {
    "type": "object",
    "properties": {
      "kind": { "$ref": "#/$defs/ProjectViewKind" },
      "owner": { "type": ["null"], "$ref": "#/$defs/OwnerScope" },
      "projectId": { "type": ["null", "string"], "x-go-name": "ProjectID" },
      "activityId": { "type": ["null", "string"], "x-go-name": "ActivityID" },
      "artifactKind": { "type": ["null"], "$ref": "#/$defs/ArtifactKind" },
      "episodeId": { "type": ["null", "string"], "x-go-name": "EpisodeID" }
    },
    "required": ["kind"],
    "additionalProperties": false
  },
  "ProjectView": {
    "type": "object",
    "properties": {
      "kind": { "$ref": "#/$defs/ProjectViewKind" },
      "summary": { "type": ["null"], "$ref": "#/$defs/ProjectState" },
      "projects": { "type": ["null", "array"], "items": { "$ref": "#/$defs/ProjectSummary" } },
      "session": { "type": ["null"], "$ref": "#/$defs/SessionStateView" },
      "projectSession": { "type": ["null"], "$ref": "#/$defs/ProjectSessionStateView" },
      "constructionSession": { "type": ["null"], "$ref": "#/$defs/ConstructionSessionView" },
      "pump": { "type": ["null"], "$ref": "#/$defs/PumpStatus" },
      "designHealth": { "type": ["null"], "$ref": "#/$defs/DesignHealth" },
      "episodes": { "type": ["null", "array"], "items": { "$ref": "#/$defs/EpisodeRecordView" } },
      "timeline": { "type": ["null"], "$ref": "#/$defs/EpisodeTimeline" }
    },
    "required": ["kind"],
    "additionalProperties": false
  },
  "ExecutionPolicyInput": {
    "type": "object",
    "properties": {
      "preset": { "type": "string" },
      "policy": { "type": ["null"], "$ref": "#/$defs/ReviewPolicyInput" }
    },
    "required": ["preset"],
    "additionalProperties": false
  },
  "ReviewDecisionInput": {
    "type": "object",
    "properties": {
      "decision": { "$ref": "#/$defs/ReviewDecision" },
      "acknowledgeStale": { "type": "boolean" },
      "optionId": { "type": ["null", "string"], "x-go-name": "OptionID" },
      "commentId": { "type": "string", "x-go-name": "CommentID" },
      "commentStatus": { "type": "string" }
    },
    "required": ["decision"],
    "additionalProperties": false
  },
  "StartProjectResult": {
    "type": "object",
    "properties": {
      "projectId": { "$ref": "#/$defs/ProjectID", "x-go-name": "ProjectID" },
      "version": { "$ref": "#/$defs/Version" },
      "session": { "type": ["null"], "$ref": "#/$defs/SessionRef" }
    },
    "required": ["projectId", "version"],
    "additionalProperties": false
  }
  ```
  and `ReviewDecision` is WIDENED by APPENDING two ordinals (never renumbering 0–3):
  ```json
  "ReviewDecision": {
    "type": "integer",
    "enum": [0, 1, 2, 3, 4, 5],
    "x-enum-varnames": ["ReviewDecisionUnknown", "ReviewApprove", "ReviewReject", "ReviewWithdraw", "ReviewAdvance", "ReviewSetCommentStatus"],
    "x-go-base": "int"
  }
  ```
  `ReviewAdvance` carries `AdvancePhase`/`AdvanceToConstruction`; `ReviewSetCommentStatus` carries `SetReviewCommentStatus` ×2; `ReviewApprove`/`ReviewReject` carry the design decisions, `SubmitPhaseDecision` (`PhaseApprove`/`PhaseSendBack`) and `SubmitSDPDecision` (`SDPCommit`/`SDPRejectAll`). `PhaseDecision` and `SDPDecision` stay as `$defs` (the workflows' signal payloads still use them) but no longer appear in any op signature.

- [ ] **Step 7: The component, the 16 relationships, the waiver, and the three deletions.**

  Append to `slots["5"].model.components`, key order copied from `construction-manager`:
  ```json
  {
    "id": "delivery-manager",
    "name": "DeliveryManager",
    "kind": "manager",
    "layer": "manager",
    "encapsulates": "Encapsulates the Project Delivery Workflow volatility (B-02, B-03, B-04, B-13, B-14): how a project's activities are scheduled and how one activity's task DAG advances — dispatch → collect → review → gate → advance. A parent pump workflow per project starts a child activity-lifecycle workflow for every eligible activity (R3); the child is one generic DAG walker whose per-type differences are DATA (the method-assets lifecycle) and STRATEGY (command, worker class, artifact codec), never a branch on design-versus-construction. Replaces SystemDesignManager, ProjectDesignManager and ConstructionManager, which were three choreographies of one workflow.",
    "encapsulatesVolatilities": [
      "Project Delivery Workflow"
    ],
    "atomicBusinessVerbs": [],
    "contractKey": "deliveryManager",
    "constructionProfile": "handwritten",
    "provisioning": "owned",
    "uiSurface": false
  }
  ```
  - **No `buildStatus` key.** The Go package lands in this same commit, and `buildStatus: "planned"` on a component that HAS a package is itself an Error (`ALIGN-STALE-PLANNED`, `align.go:348-359`).
  - [ ] **Verify first:** `DeliveryManager` normalizes to `delivery` under `StereotypeSuffixNormalizer` (`align.go:72-87` strips one trailing `access|engine|manager|client`), and `ownerKeyForPackage` (`align.go:415-426`) attributes a package to the DEEPEST path segment that normalizes to a component key in that layer. Confirm no other Manager-layer path contains a `delivery` segment: `ls server/internal/manager/`.

  The sixteen relationships (shape copied from the existing entries — the keys are exactly `from`, `to`, `mode`, `label`):

  | from | to | mode | label |
  |---|---|---|---|
  | `web-client` | `delivery-manager` | sync | `startProject \| dispatchActivityTask \| submitReviewDecision \| askQuestions \| acknowledgeStaleBasis \| setProjectRunState \| overrideActivity \| setProjectExecutionPolicy \| queryProjectView \| queryActivityView` |
  | `mcp-client` | `delivery-manager` | sync | same as web-client (R4 cross-surface equivalence) |
  | `scheduler-client` | `delivery-manager` | sync | `executeNextActivity \| replanProject` |
  | `delivery-manager` | `review-engine` | sync | `ProposeReviews(change, activityType, lifecyclePhase, componentId, policy, floorTouched, contracts) → ReviewSet` |
  | `delivery-manager` | `estimation-engine` | sync | `DerivePlan / computeNetwork / EstimateForOption / computeEarnedValue — the projectDesign activity's computed artifact (§6)` |
  | `delivery-manager` | `operation-estimation-engine` | sync | `EstimateOperatingCost(option) → the SDP review's operating-cost headline (§6)` |
  | `delivery-manager` | `billing-engine` | sync | `PriceConstruction(estimate) → the SDP review's cost in tokens and dollars (§6)` |
  | `delivery-manager` | `intervention-engine` | sync | `DecideOnVariance(variance) → {Retry \| Escalate \| Takeover}` |
  | `delivery-manager` | `design-health-engine` | sync | `EvaluateDesignHealth(project, systemModel) → findings` |
  | `delivery-manager` | `project-state-access` | sync | `readProject / stage·commit·reject·withdraw typed artifact model / plan + policy writes / the activity-execution ledgers` |
  | `delivery-manager` | `agentic-job-access` | sync | `submitAgenticJob / observeAgenticJob / cancelAgenticJob` |
  | `delivery-manager` | `source-control-access` | sync | `getInstallationToken \| openBranch \| openPullRequest \| getPullRequestStatus \| postReview \| mergePullRequest` |
  | `delivery-manager` | `artifact-access` | sync | `storeConstructionOutput / retrieveConstructionOutput / retrieveOutputTree` |
  | `delivery-manager` | `episode-access` | sync | `appendEpisode / listEpisodes / readTraceEvents` |
  | `delivery-manager` | `message-bus` | sync | `registerSchedule(deliveryPump, replanSweep) / deliverSignal(replan trigger)` |
  | `delivery-manager` | `logging` / `diagnostics` | sync | `Logs` / `Reports health` — **TWO JSON rows**, one per Utility |

  **16 table rows = 17 JSON edges**, because the last row is two. `76 − 37 + 17 = 56`. If you reconcile to 55 you have dropped `delivery-manager → logging` or `→ diagnostics`; every other Manager in the model carries both, and `DV-REL-UTILITY-EXEMPT` is what keeps them out of `DV-REL-COVERAGE`, not their absence.

  - **R6 correction to the stage-1 plan:** `→ operation-estimation-engine` and `→ billing-engine` are rows 6 and 7. The stage-1 text listed 14 edges and omitted both; measured, indices 14 and 15 today are `project-design-manager →` each of them, and they are those Engines' ONLY Manager caller. Dropping them leaves `DV-REL-COVERAGE` nothing to exercise for either Engine and leaves spec §6's cost headline with no source.
  - **`construction-manager → project-design-manager` (queued, index 72) is DELETED.** The replan trigger becomes an internal signal of one Manager, which is not an architecture edge (the `message-bus` doctrine note on `components[36]`). Its step in `var-replan-scope-change` must be re-authored in the SAME commit or `DV-EDGE-IN-MODEL` / `CC-STEP-NONEMPTY` fire. `billing-manager → operations-manager` (queued) survives untouched.
  - **Never reintroduce a SYNC Manager→Manager edge** — `SYS-DONT-MGR-SYNC-MGR` is an Error; index 72 was legal only because it was queued.
  - Every non-Utility row above must be exercised by some dynamic view or `DV-REL-COVERAGE` fires at Error, one per row (`rules_dynamic.go:204-261`; Utility targets are exempt, which is why the `logging`/`diagnostics` rows need no step). Step 8 discharges that — the mechanical reason Steps 7 and 8 are one commit.

  Then DELETE, in this order: the three contracts from `.serviceContracts`; the 37 relationships (by the Step-1 index list, descending, so earlier indices stay valid); the three components. And expire the transitional waiver: `slots["3"].model.waivers`'s §2h justification sentence about the three Managers' shared encapsulation becomes a statement that the facet group ended at stage 4a — `Project Delivery Workflow` is now 1:1 on `delivery-manager`.

  Finally, `slots["6"].model.deployment.containers[0].components`: remove `ConstructionManager`, `ProjectDesignManager` and `SystemDesignManager`; insert `DeliveryManager` in the list's existing alphabetical-within-layer position (after `BillingManager`). `DEP-CONTAINER-REF` / `DEP-COVERAGE` resolve container components against slot 5 by NAME, so a stale name here is an Error the moment the component goes.

- [ ] **Step 8: Five core use cases become three, and every realization moves with them.** `.slots["4"].model.decisions` holds 18 (5 core, 13 `nonCore`) and `.slots["5"].model.dynamicViews` holds 18, one per use case. That 1:1 is `USECASE-DYNAMIC-MISSING` (`rules_system.go:379-410`, **Error**), keyed on `dv.useCaseId == uc.id` with `variationOf` never consulted; `UC-ACT-PRESENT` (`rules_statevalidation.go:216-244`, **Error**) is equally unconditional.

  **The move:** `drive-system-design` and `execute-a-construction-activity` are DELETED (their diagrams' content becomes steps of the new core — spec §4 "absorbs"); `commit-to-a-project-option` is DEMOTED to a variation; `execute-a-project-activity` is ADDED as core. Decisions 18 → 17; views 18 → 17; core 5 → 3, inside `DH-CARD-COREUC`'s 2–6 band.

  **Re-parenting (`variationOf`) — eleven rows:**

  | use case | before | after |
  |---|---|---|
  | `manage-projects`, `add-a-use-case-to-an-in-flight-project`, `ask-a-clarifying-question-during-review`, `send-back-change-requests-for-a-redraft` | `drive-system-design` | `execute-a-project-activity` |
  | `replan-under-scope-change`, `track-weekly-project-progress`, `view-the-project-state-log`, `download-generated-source-code`, `resume-paused-construction`, `requeue-a-failed-construction-activity` | `execute-a-construction-activity` | `execute-a-project-activity` |
  | `commit-to-a-project-option` | *(core)* | `execute-a-project-activity`, `classification` flips `core` → `nonCore`, and it gains a non-empty `rejectionReason` |
  | `retry-a-declined-service-invoice`, `onboard-a-new-customer`, `view-operating-cost-projection` | unchanged | unchanged |

  `UC-VARIATION-REF` (`rules_statevalidation.go:300-360`, **Error**) enforces all three halves: a `core` UC must NOT carry `variationOf`; a `nonCore` UC MUST carry one resolving to an existing **core** id AND a non-empty `rejectionReason`. `commit-to-a-project-option`'s `rejectionReason`, verbatim:
  > `Absorbed into execute-a-project-activity 2026-09-23 (R4/R7). Committing to a project option is not a use case of its own: it is the projectDesign activity's single gate task — the user approves an engine-computed plan and cost at M0. Nothing about it differs from any other review gate except that no send-back is offered, which is a policy of the gate, not a different chain.`

  **The new core decision.** Append to `slots["4"].model.decisions`, key order `useCase` / `rejectionReason` / `essenceRationale`, and inside `useCase` the order `id` / `name` / `actors` / `trigger` / `classification` / `variationOf` / `activity`:
  ```json
  {
    "useCase": {
      "id": "execute-a-project-activity",
      "name": "Execute a Project Activity",
      "actors": [ { "id": "architect-user", "role": "Architect User" }, { "id": "operator", "role": "Operator" } ],
      "trigger": "timer",
      "classification": "core",
      "variationOf": null,
      "activity": { "nodes": [ "…see the node table" ], "edges": [ "…see the edge list" ] }
    },
    "rejectionReason": "",
    "essenceRationale": "This is the whole delivery promise in one chain: an activity of the project network becomes eligible, an agent produces its artifact, agents and humans review it, and the activity advances through its own life cycle (Righting Software App. A). It reads identically for Requirements, for Architecture and for a Service component — which is exactly why the platform used to carry three of it. Requirements, Architecture and Project Design are activities 1–3 of the same network (Table 11-1), fenced from construction by M0; nothing about the chain changes across that fence except the data (the lifecycle) and the strategy (command, worker class, artifact codec). Venues, agents and code generation will all churn; orchestrated activity execution against a committed plan is what the business does."
  }
  ```
  `trigger` is `"timer"`, which `CC-TRIGGER-EVENT` (`designhealthengine.go:1274-1301`, **Error**) reads as "the diagram must carry a `timeEvent` entry node with zero incoming edges". `clientAction` would be wrong: that arm demands the diagram carry NEITHER a `timeEvent` NOR an `acceptEvent` entry, which the pump entry violates. A `start` node for the human-initiated arm is legal alongside the `timeEvent` — it is neither forbidden kind, and `drive-system-design` already proves a `start`-rooted chain walks (`CC-PATH-CONNECTED`'s `isActorToClient` arm — the first branch of `callConnects` at `designhealthengine.go:1421`, declared at `:1430-1432`).

  Nodes (20) — the three absorbed diagrams merged, keeping every absorbed node id that is still true so the prose stays traceable:

  | id | kind | label |
  |---|---|---|
  | `pump-fires` | `timeEvent` | Delivery pump comes due |
  | `start` | `start` | *(empty — the human-initiated arm)* |
  | `activity-eligible` | `action` | Activity eligible: predecessors done and the M0 fence honoured; open its lifecycle |
  | `next-task` | `action` | Take the next ready task of the activity's lifecycle DAG |
  | `task-kind` | `decision` | Dispatch task or review task? |
  | `dispatch-task` | `action` | Dispatch the task's agentic job; stage its artifact on the activity branch |
  | `observe-validate` | `action` | Read the result back and validate it against the task's exit criterion |
  | `exit-met` | `decision` | Exit criterion met? |
  | `retry-remain` | `decision` | Retries remain? *(`decidedBy: intervention-engine`)* |
  | `escalate-operator` | `action` | Escalate to the operator; the steer is kept on the activity and delivered to its next attempt *(`roleName: Operator`, `linkedActorId: operator`)* |
  | `operator-intervened` | `decision` | Operator intervened? *(`decidedBy: operator`)* |
  | `propose-reviews` | `action` | Determine the reviewer set and whether a human must decide |
  | `requires-human` | `decision` | Does the policy require a human decision? *(`decidedBy: review-engine`)* |
  | `dispatch-reviewers` | `action` | Reviewers review the staged artifact |
  | `human-decision` | `action` | The architect user approves or sends back, with comments *(`roleName: Architect User`, `linkedActorId: architect-user`)* |
  | `verdict` | `decision` | Approve or send back? |
  | `commit-artifact` | `action` | Commit the staged artifact; the gate passes; the LifecyclePhase is earned |
  | `merge-advance` | `merge` | *(empty)* |
  | `record-exit` | `action` | Record binary exit; recompute earned value and projection |
  | `end` | `end` | *(empty)* |

  Edges: `pump-fires→activity-eligible`; `start→activity-eligible`; `activity-eligible→next-task`; `next-task→task-kind`; `task-kind→dispatch-task [dispatch]`; `task-kind→propose-reviews [review]`; `dispatch-task→observe-validate`; `observe-validate→exit-met`; `exit-met→merge-advance [yes]`; `exit-met→retry-remain [no]`; `retry-remain→dispatch-task [yes: retry]`; `retry-remain→escalate-operator [no: exhausted]`; `escalate-operator→operator-intervened`; `operator-intervened→escalate-operator [no]`; `operator-intervened→merge-advance [yes]`; `propose-reviews→requires-human`; `requires-human→dispatch-reviewers [agents only]`; `requires-human→human-decision [human required]`; `dispatch-reviewers→verdict`; `human-decision→verdict`; `verdict→commit-artifact [approve]`; `verdict→next-task [sendBack: reopens the judged task as revision n+1]`; `commit-artifact→merge-advance`; `merge-advance→record-exit`; `record-exit→end`.
  - Every `guardedFlow` edge carries non-empty guard text or `UC-GUARD-LABEL` (**Error**) fires. The bracketed text IS the `guard` value; edges with no bracket are `controlFlow` with `"guard": ""`.
  - [ ] **Verify first:** `activityHasEntryAndAction` (`rules_statevalidation.go:246-270`) needs ≥1 `start` OR a zero-incoming `timeEvent`/`acceptEvent`, AND ≥1 `action`. This diagram has both entry forms and eleven actions.
  - [ ] **Verify first:** `CC-PATH-CONNECTED` enumerates every entry→end path (`activityPaths`, cap `maxActivityPaths = 512`). The `retry-remain→dispatch-task` and `verdict→next-task` back-edges make the graph cyclic. Run the gate and READ the finding rather than guessing; if the path count exceeds the cap, move the send-back back-edge to `merge-advance` and re-run.

  **The dynamic view.** Key `uc-execute-project-activity`, `useCaseId` `execute-a-project-activity`, title `Execute a Project Activity`. `CC-COVERAGE` (**Error**, bidirectional) requires a step for EVERY `action`/`timeEvent`/`acceptEvent` node and FORBIDS a step on anything that is not one of those or a `decision`/`switch` (`ccMustHaveStep`/`ccMayHaveStep`, `designhealthengine.go:842-860`). So: a step for `pump-fires` and each of the eleven actions (12 required); optional steps on `retry-remain`, `operator-intervened`, `requires-human` (keep them — they are where the Engines are called, and the absorbed views already carried them); and **no step on `start`, `task-kind`, `exit-met`, `verdict`, `merge-advance`, `end`**.

  The two entries as step-local `alt` groups — the idiom already used ~100 times in the committed views:
  ```json
  { "activityNodeId": "pump-fires",
    "calls": [ { "from": "scheduler-client", "to": "delivery-manager", "mode": "sync", "label": "executeNextActivity(projectId, tickId) — the pump starts a child for every eligible activity" } ] },
  { "activityNodeId": "activity-eligible",
    "calls": [
      { "from": "architect-user", "to": "web-client", "mode": "sync", "label": "opens the activity from the plan", "alt": "s1" },
      { "from": "architect-user", "to": "mcp-client", "mode": "sync", "label": "opens the activity over the MCP surface (R4 cross-surface equivalence)", "alt": "s1" },
      { "from": "web-client", "to": "delivery-manager", "mode": "sync", "label": "queryActivityView(projectId, activityId)", "alt": "s2" },
      { "from": "mcp-client", "to": "delivery-manager", "mode": "sync", "label": "queryActivityView(projectId, activityId) — same Manager entry", "alt": "s2" },
      { "from": "delivery-manager", "to": "project-state-access", "mode": "sync", "label": "readProject → the next ready tasks of the activity's lifecycle DAG" } ] }
  ```
  Then one step per remaining action, routing EXACTLY the Step-7 relationships: `next-task` → `project-state-access`; `dispatch-task` → `source-control-access` (token, branch) + `agentic-job-access` (submit) + `agentic-job-access→construction-pipeline-runtime`; `observe-validate` → `agentic-job-access` (observe) + `artifact-access` (store) + `artifact-access→project-git-repo` + `episode-access` (append) + `episode-access→project-git-repo`; `retry-remain` → `intervention-engine`; `escalate-operator` → `project-state-access` + the operator `alt` pair into both clients + back into `delivery-manager` (`overrideActivity`); `propose-reviews` → `source-control-access` (openPullRequest) + `review-engine`; `requires-human` → `review-engine`; `dispatch-reviewers` → `agentic-job-access`; `human-decision` → the `architect-user` `alt` pair + `submitReviewDecision` into `delivery-manager`; `commit-artifact` → `project-state-access` (commit) + `source-control-access` (postReview, mergePullRequest) + `design-health-engine` (EvaluateDesignHealth on a design artifact's gate); `record-exit` → `project-state-access` + `estimation-engine` (computeEarnedValue) + `operation-estimation-engine` and `billing-engine` (the projectDesign activity's cost headline, §6 — these two rows exist NOWHERE else once `uc2` is re-keyed, so place them deliberately and check Step 15's query).

  Rules this must satisfy, each an Error: `DV-EDGE-IN-MODEL` (every component↔component call matches a declared `(from,to,mode)`); `DV-EDGE-ENDS` (every end is a component id or an actor **of this use case**); `DV-SINGLE-MGR` (Client edges enter exactly ONE Manager — `delivery-manager`); `CC-ACTOR-EDGE` (an actor's counterpart is a `client`-kind component, mode `sync`); `CC-ACTOR-LANE` (`escalate-operator` and `human-decision` carry `linkedActorId`, so their steps must name that actor); `CC-PATH-CONNECTED` (the `timeEvent` path's first call is client→manager ✓, the `start` path's first is actor→client ✓, every later call rooted at an already-reached `from`).

  **Delete the two absorbed views and re-key the rest.** Remove `uc1-drive-system-design` and `uc3-execute-construction-activity` (confirm the exact `key` values from Step 1). In the remaining eleven that name a deleted Manager, replace the endpoint with `delivery-manager` and rewrite the op label to the new contract's verb (`dispatchActivityTask`, `submitReviewDecision`, `askQuestions`, `acknowledgeStaleBasis`, `setProjectRunState`, `overrideActivity`, `replanProject`, `setProjectExecutionPolicy`, `queryProjectView`, `queryActivityView`, `startProject`, `executeNextActivity`). `uc2-commit-project-option` is KEPT as-is except for exactly that. `var-replan-scope-change`'s step that exercised the deleted index-72 edge is re-authored as a `delivery-manager → message-bus` `deliverSignal` step.

- [ ] **Step 9: Write the twelve-op façade as a THIN DISPATCHER.** In `delivery/deliverymanager.go`, a `deliveryManager` struct holding the three moved impls, each still the receiver of its own forty methods. Nothing in this step changes rail behaviour — every op resolves an activity id to a rail and forwards.

  ```go
  // deliveryManager is the ONE Manager of the Project Delivery Workflow volatility. In
  // stage 4a it is deliberately a DISPATCHER: the three rails it replaces are still here,
  // moved verbatim, and the twelve ops route onto their forty. That is the whole point of
  // splitting stage 4 — the model, the package and the wire move in one reviewable commit
  // while the choreography stays byte-for-byte what fifteen replay fixtures already pin.
  // Stage 4b replaces the three rails with one generic DAG walker and these twelve bodies
  // stop being a switch.
  type deliveryManager struct {
  	sd *systemDesignManager
  	pd *projectDesignManager
  	cs *constructionManager
  	// railFor's inputs, held once so no op re-reads them.
  	projectState projectstate.ProjectStateAccess
  }

  // rail names which of the three moved choreographies owns an activity. It is derived,
  // never stored: projectstate.ClassifyActivity already maps the committed activity id to
  // an ActivityType, and the design types are exactly the three the plan derivation emits
  // as activities 1-3 (requirements, architecture, projectDesign). A construction activity
  // is anything else with a committed row.
  type rail int

  const (
  	railUnknown rail = iota
  	railSystemDesign
  	railProjectDesign
  	railConstruction
  )
  ```
  `railFor(rc, projectID, activityID) (rail, ArtifactKind, error)` reads the project once, calls `projectstate.ClassifyActivity`, and maps `ActivityTypeRequirements`/`ActivityTypeArchitecture` → `railSystemDesign`, `ActivityTypeProjectDesign` → `railProjectDesign`, everything else → `railConstruction`. `ErrDesignActivityNotDispatchable` is TOLERATED here — it is rule 0 of `ClassifyActivity` and says only that the PUMP does not dispatch the activity, not that it is unclassifiable. The `ArtifactKind` comes from the task: `methodassets.LifecycleFor(LifecycleKeyFor(typ, variant))`, the task whose id is `taskID`, its own `artifactKind` or — for a review task — the `artifactKind` of the dispatch task its `reviews` field names. This is the same resolution `webApp/src/components/activity/activityViewToGraph.ts` `taskFactsFor` does client-side; keep the two consistent and say so in the doc comment.

  **The dispatch tables.** Each row is `(input) → the old implementation called, verbatim`.

  `StartProject(rc, owner, name, projectID *ProjectID, model *OperatingModel, research *ResearchInput, start bool) (StartProjectResult, error)` — applied in this order, each step skipped when its argument is absent:

  | condition | forwards to |
  |---|---|
  | `projectID == nil` | `sd.CreateProject(rc, owner, name)` |
  | `model != nil` | `sd.SetOperatingModel(rc, id, *model)` |
  | `research != nil` | `sd.SetResearchInput(rc, id, *research)` |
  | `start` | `sd.StartSystemDesign(rc, id)` → `result.Session` |

  Errors: an empty `name` with `projectID == nil` is `ContractMisuse`; every forwarded error passes through unchanged.

  `SubmitReviewDecision(rc, projectID, activityID, taskID, decision ReviewDecisionInput, feedback *ReviewFeedback) error` — the nine writers:

  | rail | `decision.decision` | forwards to |
  |---|---|---|
  | systemDesign | `ReviewApprove` / `ReviewReject` / `ReviewWithdraw` | `sd.SubmitReviewDecision(rc, projectID, kind, decision.Decision, feedback)` |
  | systemDesign | `ReviewSetCommentStatus` | `sd.SetReviewCommentStatus(rc, projectID, kind, decision.CommentID, decision.CommentStatus)` |
  | systemDesign | `ReviewAdvance` | `sd.AdvancePhase(rc, projectID, decision.AcknowledgeStale)` — result discarded; the gating outcome is readable through `QueryProjectView(summary)` |
  | projectDesign | `ReviewApprove` / `ReviewReject` on `taskID == "sdpReview"` | `pd.SubmitSDPDecision(rc, projectID, SDPCommit\|SDPRejectAll, decision.OptionID, feedback)` |
  | projectDesign | `ReviewApprove` / `ReviewReject` / `ReviewWithdraw` on any other task | `pd.SubmitReviewDecision(rc, projectID, kind, decision.Decision, feedback)` |
  | projectDesign | `ReviewSetCommentStatus` | `pd.SetReviewCommentStatus(rc, projectID, kind, decision.CommentID, decision.CommentStatus)` |
  | projectDesign | `ReviewAdvance` | `pd.AdvanceToConstruction(rc, projectID, decision.AcknowledgeStale)` |
  | construction | `ReviewApprove` → `PhaseApprove`, `ReviewReject` → `PhaseSendBack` | `cs.SubmitPhaseDecision(rc, projectID, activityID, phaseOf(taskID), phaseDecision, feedback)` |
  | construction | `ReviewSetCommentStatus` / `ReviewWithdraw` / `ReviewAdvance` | `fwm.New(fwm.ContractMisuse, "deliveryManager.SubmitReviewDecision: the construction rail has no comment-status or withdraw verb until stage 4b")` |

  `phaseOf(taskID)` is the task's LifecyclePhase wire name — the same string `SubmitPhaseDecision` takes today (`receivePhaseDecision` filters on it, `constructactivity.go:1845`), plus the two special gate keys `mergeGateKey = "merge"` (`:1917`) and `takeoverGateKey = "takeover"`, which pass through when `taskID` is one of them.

  `QueryProjectView(rc, query ProjectViewQuery) (ProjectView, error)` — the thirteen readers:

  | `query.kind` | required selectors | forwards to |
  |---|---|---|
  | `summary` | `projectId` | `sd.GetProject` → `.Summary` |
  | `projects` | `owner` | `sd.ListProjects` → `.Projects` |
  | `session` | `projectId` + `artifactKind` (Phase-1 kind) | `sd.GetSessionState` → `.Session` |
  | `session` | `projectId` + `artifactKind` (Phase-2 kind) | `pd.GetSessionState` → `.ProjectSession` |
  | `session` | `projectId` + `activityId` | `cs.GetSessionState` → `.ConstructionSession` |
  | `pump` | `projectId` | `cs.GetPumpStatus` → `.Pump` |
  | `designHealth` | `projectId` | `sd.GetDesignHealth` → `.DesignHealth` |
  | `episodes` | `projectId` + `artifactKind` | `sd`/`pd.ListEpisodesForArtifact` by kind phase → `.Episodes` |
  | `episodes` | `projectId` + `activityId` | `cs.ListEpisodesForActivity` → `.Episodes` |
  | `timeline` | `projectId` + `episodeId` | `cs.GetEpisodeTimeline` → `.Timeline` |

  The Phase-1/Phase-2 split on `artifactKind` uses the existing kind tables — `KindMission…KindStandardCheck` are Phase 1, `KindPlanningAssumptions…KindSdpReview` Phase 2. The three `GetEpisodeTimeline` implementations are byte-identical (they read the same `episodeAccess`), so one is chosen and the other two are class-C collapsed in Step 2b; say which in the doc comment. A missing required selector is `ContractMisuse` naming the selector; an unknown `kind` is impossible (`sumtype-check` covers the enum, and there is no `default:` arm).

  The remaining seven ops are one-line forwards: `ExecuteNextActivity` → `cs.ExecuteNextActivity`; `DispatchActivityTask` → `sd`/`pd.RequestArtifactDraft` by rail (and, for `railProjectDesign` with `taskID == "sdpReview"`, `pd.RequestSDPCommit`; for `railConstruction`, `fwm.New(fwm.FailedPrecondition, "deliveryManager.DispatchActivityTask: construction run/re-run has no op before stage 4b — a send-back re-dispatches the task")`); `AskQuestions` → `sd`/`pd.AskQuestions` (construction: `ContractMisuse`, naming 4b); `AcknowledgeStaleBasis` → `sd`/`pd.AcknowledgeStaleBasis`; `SetProjectRunState` → `cs.PauseProject`/`cs.ResumeProject` on the enum; `OverrideActivity` → `cs.OverrideActivity`; `ReplanProject` → `cs.RunReplanSweep`; `SetProjectExecutionPolicy` → `cs.SetReviewPolicy(preset)` when `policy.Policy == nil`, else `cs.UpdateReviewPolicy(*policy.Policy)`; `QueryActivityView` → `cs.QueryActivityView`.

  **One worker, one queue.** `WorkerManifest()` on the merged manager returns ONE `genWorkerManifest` whose `Workflows` is the concatenation of the three old lists — **eleven entries, each keeping its existing registered name string** (`systemDesignPhase`, `systemDesignCoAuthor`, `systemDesignPhaseAdvance`, `projectDesignCoAuthor`, `projectDesignSDPReview`, `projectDesignPhaseAdvance`, `constructionPumpNextActivity`, `constructionConstructActivity`, `constructionReplanSweep`, `constructionProjectSupervision`, `constructionPumpSweep`). **Do not rename them** (R2) — the fifteen construction fixtures replay against those names. `ActivityOptions` is the merged options hook; where the three old hooks answer differently for the same activity name, keep the CONSTRUCTION answer and earmark the divergence (construction's are the tuned ones). `RegisterSchedules` registers `delivery:pumpSweep` (30 s) and `delivery:replanSweep` (300 s) on TaskQueue `delivery`; the old `construction:*` Schedule ids are DELETED by the drain (Task 10), not by code.

- [ ] **GREEN POINT B — generate, and the server builds.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off make gen-models gen-fakes gen-client gen-internal-tools gen-temporal gen-sdk gen-config gen-main gen-lifecycles
  rm -rf internal/manager/systemdesign internal/manager/projectdesign internal/manager/construction
  GOWORK=off go build ./...
  ```
  Expected, and check each by name:
  ```bash
  ls internal/manager/delivery/contract.gen.go internal/manager/delivery/activities.gen.go \
     internal/manager/delivery/invokers.gen.go internal/manager/delivery/worker.gen.go \
     internal/manager/delivery/fake/fake.gen.go
  grep -n 'TaskQueue = ' internal/manager/delivery/worker.gen.go
  ls internal/client/web/delivery internal/client/mcp/delivery
  ls ../systemtests/internal/sdk | sort
  ```
  `worker.gen.go` with `const TaskQueue = "delivery"` is the output that proves Step 4's `cmd/appgen/main.go:84` edit landed — temporalgen emits it, and temporalgen runs off `managers`, not off either web list. The six old `internal/client/{web,mcp}/{systemdesign,projectdesign,construction}` directories are gone; `server/api/openapi.yaml` carries `/api/v1/delivery/…`; `cmd/server/main.gen.go` has ONE `DeliveryManager` field and one worker block; `../systemtests/internal/sdk` has lost its `{http,mcp,types}_{system-design,project-design,construction}.gen.go` and gained `{http,mcp,types}_delivery.gen.go`. `go build ./...` must be clean before Step 10.
  - If `make gen-temporal` panics dereferencing a contract, `cmd/appgen/main.go:84` still names a deleted key — go back to Step 4.
  - If `internal/manager/delivery/worker.gen.go` is absent while the build otherwise proceeds, `deliveryManager` is missing from that same list.
  - If `mcpemit` errors with "operation has no documentation", Step 10 has not run yet — run Step 10 and regenerate.
  - `go build ./...` covers the `server` module only. The `systemtests` module is a SEPARATE module and is now broken by the SDK prune above — Step 14b repairs it, and nothing before Step 14b will tell you.

- [ ] **Step 10: The MCP op-doc table.** `server/cmd/clientgen/mcpdocs.go` — delete the `"SystemDesignManager"`, `"ProjectDesignManager"` and `"ConstructionManager"` blocks (lines 18-63, forty entries) and add ONE `"DeliveryManager"` block with exactly twelve entries. `mcpemit.Generate` ERRORS if an op has no non-empty doc (`mcpdocs.go:13-16`) — the table is a build gate, not documentation. Write each entry as prose that names what the op DOES for a caller with no access to this plan; carry the surviving sentences from the forty being deleted rather than writing new ones. Example shape for the two that are genuinely new:
  ```go
  	"DeliveryManager": {
  		"QueryProjectView": "Return one composed view of a project, selected by kind: summary (head state), projects (every project for an owner), session (the live draft/review session for one artifact kind or one construction activity), pump (the delivery pump's dispatch status), designHealth (the live Method-rule findings), episodes (the agentic episode records for one artifact kind or activity) or timeline (one episode's full trace). Read-only; the query object carries the selector each kind needs.",
  		"SubmitReviewDecision": "Record a human decision on one review task of one activity: approve, send back with change requests, withdraw, advance past the phase seal, or set one review comment's status. The decision object carries the extras a particular decision needs — the chosen option id at the M0 spend gate, the comment id and status for a comment transition, and whether a stale basis is being acknowledged. This is the single write behind every gate in the product.",
  		// … the other ten
  	},
  ```

- [ ] **Step 11: `arch_test.go` allowlists and the design-health pins.**
  - `server/internal/arch_test.go`: replace the three entries (`"internal/manager/construction"` at `:291-296`, `"internal/manager/projectdesign"` at `:314-319`, `"internal/manager/systemdesign"` at `:329-333`) with ONE:
    ```go
    	// Temporal registration entrypoints (see operations). RegisterSchedules registers the
    	// two platform-wide delivery Schedules at startup (the pump sweep, 30s, and the replan
    	// sweep, 5m). MaterializeActivityPlan is the single render-on-read entry point for the
    	// Phase-2 plan — it derives the baseline from the committed System and applies the
    	// authored deltas — and is exported because the slot-9/10 read path that calls it lives
    	// OUTSIDE this package (projectstate.ProjectStateAccess) and because the drift gate
    	// (make derived-plan-check) reads it. It came across from internal/manager/projectdesign
    	// with the stage-4a collapse; nothing else about it changed.
    	"internal/manager/delivery": {
    		"MaterializeActivityPlan",
    		"RegisterManagerWorker",
    		"RegisterSchedules",
    		"RegisterWorker",
    		"TaskQueue",
    	},
    ```
    Regenerate rather than hand-tune if it is wrong: `ENCAP_DUMP=1 GOWORK=off go test ./internal/ -run TestGeneratedOnlyPublic -v`.
  - `server/internal/engine/designhealth/engine_test.go:64` — flip the pin, with the measurement in the comment:
    ```go
    	// DH-CONTRACT-OPCOUNT-MAX goes SILENT at stage 4a. It fires only ABOVE 12 ops, and the
    	// two contracts that carried it — systemDesignManager (16) and constructionManager (13)
    	// — dissolved into the 12-op deliveryManager. Measured on the committed state after the
    	// collapse, the largest surviving contract is 12 (constructionTransitionAccess and
    	// activityExecutionAccess, both exactly at the limit), so nothing is left to fire on.
    	// This is the FIRST time the repo has had no Manager contract past App-C's ceiling.
    	assertAbsent(t, got, RuleContractOpMax)
    ```
  - `engine_test.go:99` — keep `assertPresent(t, got, RuleContractDeadOp, methodcheck.SeverityWarning)` and CORRECT its comment: the stage-3 earmark says this flips when the deprecated facets are deleted, and that is **wrong**. Deleting `constructionTransitionAccess` kills the `RecordOperatorNote` duplicate but NOT the `AcknowledgeStaleBasis` one — `projectStateAccess` publishes it too and spec §5.3 keeps `projectStateAccess` at its 9 ops. The pin stays until one of those two verbs goes.
  - `engine_test.go:90` — `assertAbsent(t, got, RuleContractFacet)` is unchanged and is the guard that the new contract has an owning component.
  - `engine_test.go:175-194` — the `realizedViews` table: delete `{"drive-system-design", "PoC"}` and `{"execute-a-construction-activity", "batch-1"}`, add `{"execute-a-project-activity", "stage-4a"}` → 17 rows. Then correct every hard-coded number the file states in prose or in a `t.Errorf` string: "all 18 dynamic views are realized" and "all 18 committed use cases" become 17, and the eligible-node arithmetic in the comment block at `:110-148` is re-derived from the new diagram (the old text's 118 covered the deleted diagrams' nodes). Re-measure, do not re-pin:
    ```bash
    cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
    jq '[.slots["4"].model.decisions[].useCase.activity.nodes[] | select(.kind|IN("action","timeEvent","acceptEvent"))] | length' .aiarch/state/project.json
    ```
  - `SYS-CARD-RATIO` goes from "7 Engines but only 5 Managers" to "7 vs 3" — same id, same severity, no test edit. `APPC-CARD-SUB-MGR` ("5 Managers; App-C §3.2c strives for ≤3") goes SILENT at 3; `indexBySeverity` is a set and nothing asserts its presence, so this is safe — note it in the commit message.

- [ ] **Step 12: The registered-names golden.** `server/internal/registered_names_test.go`:
  - `mustRegisteredNames` (doc `:316-320`, func `:321-345`): the three build-and-register blocks collapse to one. The nil-argument count must match the new constructor's 20 deps exactly:
    ```go
    	deliveryMgr := delivery.NewDeliveryManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil, "")
    	delivery.RegisterManagerWorker(&reg, deliveryMgr)
    ```
    Read the generated `NewDeliveryManager` signature and count from it; do not count from this plan.
  - `registeredTemporalNamesGolden` (`:69-314`, **231 entries** measured): regenerate the literal from the test's own failure output rather than editing by hand. Expected count **139** by R-G's arithmetic (231 − 172 + 80). Run, capture, rewrite, re-run:
    ```bash
    cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
    GOWORK=off go test ./internal/ -run TestRegisteredTemporalNamesGolden -count=1 -v 2>&1 | tee /tmp/golden.txt | head -60
    awk '/registeredTemporalNamesGolden = \[\]string\{/{f=1;next} f&&/^\}/{exit} f&&/^\t"/{c++} END{print c}' internal/registered_names_test.go
    ```
    Expected after the rewrite: the count prints `139` and the test passes. **If it does not, do not adjust the literal to match — work out which dep is registered a different number of times and why.** A count above 139 means a dep is still registered twice (the merge kept two `genActivities` threadings); below means a dep was dropped from the contract, which would delete Temporal activity names the drain has not covered.
  - Rewrite the golden's header comment: it currently explains stage 3's "twenty-four ADDITIONS, zero removals". Stage 4a is the opposite — **ninety-two removals** — and the comment must say so, and say that removals are what make the drain non-negotiable (a worker from an older commit can serve a workflow this one started, but not the reverse).

- [ ] **Step 13: Replay — the acceptance of the whole move.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off go test ./internal/manager/delivery/ -run Test_Replay -count=1 -v 2>&1 | tail -40
  ```
  Expected: **nineteen** subtests, all `--- PASS` — the fifteen construction fixtures under `testdata/replay/{pre-b1,post-b1,post-b17,pre-d,post-stage3}/` and the four captured in Task 1 under `design-pre-stage4/` and `phase2-pre-stage4/`. The vacuity guard must still be present and must still fail on an empty directory.
  **A non-determinism error here means the move changed a command sequence** — a class-C collapse that was not byte-identical, or a helper that was inlined "while it was being moved anyway". Find it and revert that one edit. Never re-capture a fixture to make this pass, and never add a `GetVersion` fence to paper over a move that was supposed to be verbatim.

- [ ] **Step 14: `cmd/server` — the hand-written half.**
  - `server/cmd/server/managerlog.go` (305 L): delete `loggingSystemDesignManager` (`:48`), `loggingProjectDesignManager` (`:133`) and `loggingConstructionManager` (`:190`) with their ~40 wrapper methods; add ONE `loggingDeliveryManager` with twelve. Keep `loggingOperationsManager` (`:262`) untouched. Copy the surviving log fields per op from the wrappers being deleted — the log lines are what production reads.
  - `server/cmd/server/hooks.go`: `:537/:541/:542` become one wrap; `:599` hands two managers to the client mounts, not four. Every `*Manager*` hook name is appgen-derived from the component key, so `RegisterConstructionManagerWorker` (`:1139`), `RegisterProjectDesignManagerWorker`/`RegisterSystemDesignManagerWorker` (`:1147-1148`), `ConstructionManagerEscalationWaitTimeout` (`:1150`), `ConstructionManagerInterventionMode` (`:1154`), `ProjectDesignManagerRepo` (`:1161`), `SystemDesignManagerRepo` (`:1175`), `ConstructionManagerRepo` (`:1199`) and `SystemDesignManagerRepoBase` (`:1227`) all re-spell to `DeliveryManager*`. The three `Repo()` hooks become ONE — read all three bodies before merging: if they differ, the merged hook keeps the construction body and the design difference becomes an earmark, not a silent choice.
  - `config_parity_test.go` and `hooks_test.go` (638 L) follow the rename. `make gen-config-check` and `make gen-main-check` are the gates that prove the hand-written half matches the generated half.

- [ ] **Step 14b: Repair the `systemtests` module — it is IN this commit, and nothing before this step will tell you it broke.** `generateSDK` → `pruneStaleSDK` (`cmd/appgen/main.go:302-337`) deletes every `*.gen.go` under `../systemtests/internal/sdk` that is not in the fresh output set. GREEN POINT B already did that: `{http,mcp,types}_{system-design,project-design,construction}.gen.go` are gone and `{http,mcp,types}_delivery.gen.go` are there. The hand-written harness calls the deleted symbols, so the whole module stops compiling — and `.github/workflows/systemtests.yml` triggers on `server/**` AND `.aiarch/**`, both touched by this commit, so *Go fix check*, *Lint (systemtests)*, *constitution tests* and *wire system tests* all go red on merge.

  **This is NOT the `stp_uc*` drift gate.** `systemtests/Makefile`'s `gen-check` runs `cmd/gen-systemtests` against `.testingState.systemTestPlan` (keys `scenarios`, `useCaseIndex`), which 4a never edits — the regenerated tables are byte-identical and `diff -rq` passes. Regenerating them is not the fix and would change nothing.

  - [ ] **First, see exactly what went:**
    ```bash
    cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
    git status --short -- systemtests/internal/sdk
    ls systemtests/internal/sdk | sort
    cd systemtests && GOWORK=off go build ./... 2>&1 | head -40
    ```
    Expected: three deletions × three file families, three additions; the build names every undefined `sdk.SystemDesign*`, `sdk.ProjectDesign*` and `sdk.Construction*` symbol. That list IS the work of this step.
  - [ ] **Re-point each harness file**, using the build errors as the checklist:
    - `systemtests/internal/harness/httptransport.go` — the SDK emits ONE `sdk.HTTPClient` (`core.gen.go`, survives the prune; built inline at `httptransport.go:34`, unchanged) with per-manager METHODS (`func (c *HTTPClient) SystemDesignGetProject(ctx, projectID) (ProjectState, error)`) and `<Manager><Op>Request`/`Input` types in the pruned files. There is NO per-manager constructor to swap: the work is method + request-type renames — every `c.SystemDesign*`/`c.ProjectDesign*`/`c.Construction*` call and its request type becomes the matching `c.Delivery*` method of the twelve. Use Task 6 Step 9's dispatch tables as the old-op → new-op map; a test that called `SystemDesignGetProject` now calls `DeliveryQueryProjectView` with `{Kind: "summary", ProjectID: …}`. `sdk.ArtifactKind`/`sdk.Kind*` live in `types_shared.gen.go`, which survives the prune — `enums.go` needs only the renames the build errors name.
    - `systemtests/internal/harness/mcptransport.go` — the same substitution over the MCP tool names.
    - `systemtests/internal/harness/transport.go` — the transport-agnostic interface the two above satisfy: its method set shrinks to the twelve.
    - `systemtests/internal/harness/enums.go` — the ordinal/name tables re-key from three manager namespaces to one; `ProjectSessionStage`'s members keep their `Project…`-prefixed Go names (Step 3).
    - `systemtests/internal/harness/server.go`, `steps.go`, `construction_seed.go` — call sites only; no logic changes.
  - [ ] **Re-point `.testingState.systemTestPlan.useCaseIndex`.** It names `drive-system-design` and `execute-a-construction-activity` (two occurrences each, measured), both deleted by Step 8. Point all four at `execute-a-project-activity` in THIS commit — leaving them dangling makes the system-test plan cite use cases the model no longer holds, and the index is the join the `stp_uc*` tables are generated through.
    ```bash
    cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
    jq -r '[.testingState.systemTestPlan.useCaseIndex | tostring] | .[]' .aiarch/state/project.json | grep -o "drive-system-design\|execute-a-construction-activity" | sort | uniq -c
    ```
    Expected before: 2 and 2. After: nothing.
  - [ ] **Gate the module on its own checks** (they are a separate module and are NOT covered by anything run so far):
    ```bash
    cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/systemtests
    GOWORK=off go build ./...
    GOWORK=off make fix-check
    GOWORK=off make gen-check
    GOWORK=off go test -short ./... -count=1
    GOWORK=off go test ./constitution/... -count=1
    ```
    All green. `make gen-check` must be a NO-OP (`diff -rq` clean) — if it is not, `.testingState` was edited by accident and the `useCaseIndex` change above went into the wrong member.
  - **`make gen-sdk-check` stays green only because Step 16 commits at the repo root.** That gate is `git diff --exit-code -- ../systemtests/internal/sdk`, so the regenerated SDK must be STAGED by this commit's `git add -A`, not left as an untracked/unstaged sibling. Step 16's `git status --short | head -60` is where you confirm the `systemtests/` entries are present.

- [ ] **Step 15: The self-amendment loop, in full, plus the coverage re-check.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/server
  GOWORK=off make gen-models gen-fakes gen-client gen-internal-tools gen-temporal gen-sdk gen-config gen-main gen-lifecycles
  GOWORK=off make method-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot Volatilities
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot CoreUseCases
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot OperationalConcepts
  GOWORK=off make derived-plan-write
  git -C .. diff --stat -- .aiarch/state/project.json
  GOWORK=off make derived-plan-check
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check \
      gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-lifecycles-check
  GOWORK=off make sumtype-check encapsulation-check fix-check lint
  GOWORK=off go test ./internal/ -run 'TestRegisteredTemporalNamesGolden|TestFileLayout|TestGeneratedOnlyPublic|TestMethodLayering|TestNoBannedPhaseIdentifier|TestMessageBusManagersOnly' -count=1
  ```
  Expected from `validate`: **`PASS (43 advisory finding(s), 0 errors)`** on every slot, with `SYS-CARD-MGR`, `DV-SINGLE-MGR`, `DH-CONTRACT-FACET` and every `ALIGN-*` absent. The baseline is 48. The derivation — check each term against the histogram rather than accepting the total:

  | Δ | Rule | Why |
  |---|---|---|
  | **−2** | `DH-CONTRACT-OPCOUNT-MAX` | the rule is `case n > 12` (`designhealthengine.go:2030`); its two subjects were `systemDesignManager` (16) and `constructionManager` (13), and the largest survivor is 12 |
  | **−1** | `APPC-CARD-SUB-MGR` | the message reads "strives for ≤3"; 5 Managers become 3 |
  | **−3** | `APPC-SVC-STRIVE` | the rule counts by COMPONENT, not contract, and reports "has 0 ops" for each of the three deleted Manager components |
  | **+1** | `APPC-SVC-STRIVE` | `DeliveryManager` joins the same tally |
  | **0** | `SYS-CARD-RATIO` | re-words itself from "7 Engines but only 5 Managers" to "7 vs 3" — same id, same severity, no test edit |
  | **0** | `DH-CARD-ENGINES` | engines stay at 7 in 4a (R14) |
  | **0** | `APPC-SVC-AVOID-12` | it names `ProjectStateAccess` at 18 ops across its FACET GROUP; Task 3 added a parameter, not an op |
  | **0** | `DEP-PLANNED-SKIPPED` | `delivery-manager` carries no `buildStatus`, so `ALIGN-STALE-PLANNED` cannot fire and this Info is unmoved |

  Net −5, so **48 → 43**. A different total is information, not noise: read the histogram and find which term moved before you touch anything.

  `make derived-plan-write` MOVES slots 9 and 10 this time: `C-system-design-manager`, `C-project-design-manager` and `C-construction-manager` disappear and `C-delivery-manager` appears (32 → 30 activities; slot 10's 31 dependencies re-derive). **The three `.activityExecution` rows for the disappearing activities are KEPT** — they are Done/Integrated history, `QueryActivityView` answers `NotFound` for them because they have no committed item, and that is the honest answer. Do NOT run `construction-state-reset` (it deletes all construction history). Record the three orphan keys in Task 10's earmark file.

  Re-run Step 1's sole-exerciser query with the new view excluded — it must print nothing:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  jq -r '
    def utils: ["security","logging","diagnostics","message-bus"];
    . as $r |
    ($r.slots["5"].model.dynamicViews | map({uc:.useCaseId, calls:[.steps[].calls[] | "\(.from)|\(.to)|\(.mode)"]})) as $views |
    $r.slots["5"].model.relationships
    | map(select(.to as $t | (utils | index($t)) | not))
    | map(. as $rel | ("\($rel.from)|\($rel.to)|\($rel.mode)") as $k |
        {k:$k, by: [$views[] | select(.calls | index($k)) | .uc]})
    | map(select((.by|length) == 0))
    | .[] | .k' .aiarch/state/project.json
  ```
  Expected: empty. Every row this prints is a `DV-REL-COVERAGE` Error waiting to fire.

- [ ] **Step 16: Commit.** `cd webApp && npm run check` is EXPECTED TO BE RED at this point — `ops.gen.ts` now carries twelve `delivery*` ids and no hook calls them. That is Task 7's and Task 8's work, and splitting it out is deliberate: this commit is already the largest in the wave and a red SPA typecheck is a precise, self-describing failure, not an ambiguous one. Commit the regenerated `schema.ts`/`ops.gen.ts` HERE, with their input.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  git add -A
  git status --short | head -60
  git commit -F - <<'MSG'
  feat(delivery): three Managers become one — the model and the package together

  SYS-CARD-MGR forbids a sixth Manager, ALIGN-EXTRA-PKG forbids a package with
  no component, ALIGN-MISSING-PKG forbids a component with no package,
  DH-CONTRACT-FACET forbids a contract with no component, and
  USECASE-DYNAMIC-MISSING forbids a use case with no view. There is no ordering
  in which any two of those are separate commits, which is why this one carries
  the model, the Go package, four generated layers, three test goldens and the
  composition root at once — stage-1 planning proved it and this commit is the
  discharge.

  MODEL. delivery-manager (contract deliveryManager, 12 ops) replaces
  system-design-manager, project-design-manager and construction-manager;
  37 relationships out, 17 in (76 -> 56); execute-a-project-activity becomes the
  third core use case, absorbing drive-system-design and
  execute-a-construction-activity, with commit-to-a-project-option demoted and
  eleven variations re-parented; 18 use cases and 18 dynamic views become 17 and
  17. The Project Delivery Workflow volatility is 1:1 on one component for the
  first time — the transitional facet group stage 1 waived has expired.

  CODE. internal/manager/{systemdesign,projectdesign,construction} become
  internal/manager/delivery. The eleven workflow files move verbatim and keep
  their registered Temporal names; the three impl files merge into one
  deliverymanager.go and the three test files into one manager_test.go, because
  arch.CheckFileLayout allows exactly one of each per package. The twelve ops are
  a THIN DISPATCHER over the forty that already exist: nothing about any rail's
  choreography changed, which is what nineteen replay fixtures assert.

  The name collisions the merge forced are what collapsed the twins'
  byte-identical helpers — engineReviewPolicy's three copies, the episode-capture
  family, and the twenty-eight design*/ledger* helpers the two co-author files
  shared. Everything whose bodies differed was prefixed by rail rather than
  merged; the rename table is below.

  SYSTEMTESTS. make gen-sdk prunes stale SDK packages, so the three deleted
  managers' http/mcp/types files go and the hand-written harness that called them
  is re-pointed onto sdk.Delivery* in the same commit — the module is separate,
  its CI triggers on server/** and .aiarch/**, and nothing in the server module's
  own gates would have caught it.

  GATES. One task queue `delivery`, one worker, eleven workflow types. The
  registered-name golden goes 231 -> 139: ninety-two REMOVALS, which is why the
  drain is non-negotiable and why this must not deploy alone.
  DH-CONTRACT-OPCOUNT-MAX goes silent (nothing is left above 12 ops) and its pin
  flips to assertAbsent; DH-CONTRACT-DEADOP stays pinned, because deleting
  constructionTransitionAccess would not clear the projectStateAccess half.

  <the class-D rename table>

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 7: webApp — regenerate the wire and re-key the enum tables

The OAS changed under the SPA in Task 6. This task makes `src/contracts/schema.ts`, `src/api/ops.gen.ts` and `src/contracts/enums.gen.ts` agree with it. **This is the ONLY place this plan runs `npm run gen:*`** (R11).

**Files:**
- Regenerate (never hand-edit): `webApp/src/contracts/schema.ts`, `webApp/src/api/ops.gen.ts`, `webApp/src/contracts/enums.gen.ts`.
- Modify: `webApp/scripts/gen-enums.mjs` — `MANAGER_PREFIXES` (`:31`), `OUTPUT_NAMES` (`:36-93`), `DEDUPE_GROUPS` (`:100-108`).

**Interfaces produced (Task 8 depends on these exact names):**
- Op ids `deliveryStartProject`, `deliveryExecuteNextActivity`, `deliveryDispatchActivityTask`, `deliverySubmitReviewDecision`, `deliveryAskQuestions`, `deliveryAcknowledgeStaleBasis`, `deliverySetProjectRunState`, `deliveryOverrideActivity`, `deliveryReplanProject`, `deliverySetProjectExecutionPolicy`, `deliveryQueryProjectView`, `deliveryQueryActivityView`.
- Every logical enum name in `enums.gen.ts` is UNCHANGED — that is this task's acceptance.

- [ ] **Step 1: `MANAGER_PREFIXES`.** `webApp/scripts/gen-enums.mjs:31`:
  ```js
  const MANAGER_PREFIXES = ['Delivery', 'Operations'];
  ```

- [ ] **Step 2: `DEDUPE_GROUPS` empties out.** All seven groups existed because one logical enum was published by two or three Manager namespaces. With one Manager namespace, each is published once and there is nothing to dedupe:
  ```js
  /** Groups of OAS schema names known (verified by one-off comparison against
   * server/*.go iota order) to carry byte-identical enum + x-enum-varnames
   * arrays. Only the first member of each group is emitted; the rest are
   * asserted (at generation time) to still match, so drift trips a build error
   * instead of silently forking the tables.
   *
   * EMPTY since stage 4a. Every group here existed because ArtifactKind,
   * ReviewDecision, Severity, ActiveRole, ActiveStep, EpisodeKind and
   * EpisodeOutcome were published once per design/construction Manager. One
   * Manager publishes each exactly once, so the assertion has nothing to
   * compare — the fork it guarded against is now structurally impossible.
   * Keep the mechanism: it costs nothing and the next facet split will want it. */
  const DEDUPE_GROUPS = [];
  ```

- [ ] **Step 3: `OUTPUT_NAMES` re-keys, and every OUTPUT name stays identical.** Replace the whole table with:
  ```js
  const OUTPUT_NAMES = {
    DeliveryArtifactKind: 'ArtifactKind',
    DeliveryReviewDecision: 'ReviewDecision',
    DeliverySeverity: 'Severity',
    DeliveryActiveRole: 'ActiveRole',
    DeliveryActiveStep: 'ActiveStep',
    // Two DISTINCT session-stage enums, not twins: the projectDesign one inserts
    // StageAssemblingSDP at ordinal 2 and shifts the rest, so stage 4a kept both
    // shapes under distinct $defs rather than renumbering a wire ordinal. The
    // output names are unchanged from the three-Manager era on purpose.
    DeliverySessionStage: 'SessionStage',
    DeliveryProjectSessionStage: 'ProjectSessionStage',
    DeliverySDPDecision: 'SDPDecision',
    DeliveryPhase: 'ProjectPhase',
    DeliveryArtifactStage: 'ArtifactStage',
    DeliveryFailureReason: 'FailureReason',
    DeliveryActivityType: 'ActivityType',
    DeliveryActivityBuildStatus: 'ActivityBuildStatus',
    DeliveryActivityConstructionPhase: 'ActivityConstructionPhase',
    DeliveryCICheckState: 'CICheckState',
    DeliveryTestingVariant: 'TestingVariant',
    DeliveryConstructionStage: 'ConstructionStage',
    DeliveryOverrideKind: 'OverrideKind',
    DeliveryPhaseDecision: 'PhaseDecision',
    DeliveryPipelinePhase: 'PipelinePhase',
    DeliveryActivityViewState: 'ActivityViewState',
    DeliveryActivityTaskKind: 'ActivityTaskKind',
    DeliveryActivityTaskState: 'ActivityTaskState',
    DeliveryTaskRevisionOutcome: 'TaskRevisionOutcome',
    DeliveryTaskRevisionProvenance: 'TaskRevisionProvenance',
    DeliveryReviewVerdictKind: 'ReviewVerdictKind',
    DeliveryEpisodeKind: 'EpisodeKind',
    DeliveryEpisodeOutcome: 'EpisodeOutcome',
    DeliveryOperatorNoteKind: 'OperatorNoteKind',
    // NEW in stage 4a — the twelve-op contract's own enums.
    DeliveryProjectRunState: 'ProjectRunState',
    DeliveryProjectViewKind: 'ProjectViewKind',
    OperationsAutoscaleAction: 'AutoscaleAction',
    OperationsAutoscalerMode: 'AutoscalerMode',
    OperationsDesiredStateReason: 'DesiredStateReason',
    OperationsHealthState: 'HealthState',
    OperationsPatchKind: 'PatchKind',
    OperationsRuntimeStatusSeam: 'RuntimeStatusSeam',
  };
  ```
  - [ ] **Verify first:** diff the OUTPUT VALUES against the pre-change table. **Every one must be byte-identical except the two new rows.** If any logical name moved, a `src/` type rename leaked into this task and belongs in Task 8 — revert it here.

- [ ] **Step 4: Regenerate and read the diff.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/webApp
  ASDF_NODEJS_VERSION=lts npm run gen:api
  ASDF_NODEJS_VERSION=lts npm run gen:ops
  git -C .. diff --stat -- webApp/src/contracts webApp/src/api
  git -C .. diff -- webApp/src/contracts/enums.gen.ts | head -60
  grep -c "^  delivery" src/api/ops.gen.ts
  ```
  Expected: `ops.gen.ts` carries exactly **12** `delivery*` ids (plus operations 8 and composition 3 = 23 total, down from 51); `enums.gen.ts`'s exported NAMES are unchanged and only its provenance comments move. `gen-ops.mjs` (`:33-40`) and `mcp-tools.mjs` (`:26-45`) need **no edit** — both derive from the path shape and a directory scan.
  - If `gen:api` fails on a `$ref` it cannot resolve, a `$defs` closure in Task 6 Step 6 was incomplete. Fix it there, in that commit's amend, not here.

- [ ] **Step 5: Commit** (typecheck is still red — Task 8 clears it).
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  git add webApp/scripts/gen-enums.mjs webApp/src/contracts webApp/src/api
  git commit -F - <<'MSG'
  chore(webapp): regenerate the wire for deliveryManager

  Twelve delivery* ops replace forty across three manager namespaces; ops.gen.ts
  goes 51 ids to 23. MANAGER_PREFIXES drops to Delivery + Operations and
  DEDUPE_GROUPS empties: all seven groups existed because one logical enum was
  published once per Manager, and one Manager publishes each exactly once, so the
  fork they guarded against is now structurally impossible. The mechanism stays.

  Every logical enum NAME is unchanged, deliberately — including SessionStage and
  ProjectSessionStage, which stage 4a kept as two distinct $defs rather than
  renumbering the design rail's ordinals to absorb StageAssemblingSDP.

  The typecheck is red until the hooks re-key; that is the next commit.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 8: webApp — three hook families become one, and the verbs table collapses

Twenty-four hooks across three modules call the forty ops. `containers/activityVerbs.ts` is the ONE table the two stage-5 containers consult (the stage-5 earmark's claim, confirmed), so the collapse is contained: the hook modules merge, the verbs table loses its rail branch, and the episode dispatch table disappears.

**Files:**
- Create: `webApp/src/hooks/useDeliveryMutations.ts`, `webApp/src/hooks/useDeliveryQueries.ts`.
- Delete: `webApp/src/hooks/useDesignMutations.ts`, `useProjectDesignMutations.ts`, `useConstructionMutations.ts`, `useStartDesign.ts`, `useCreateProject.ts`, `useProject.ts`, `useProjects.ts`, `useSessionState.ts`, `useProjectSessionState.ts`, `useConstructionSession.ts`, `useConstructionSessions.ts`, `useDesignHealth.ts`, `useEpisodes.ts`, `useActivityView.ts` — **each only after a grep proves zero surviving importers**.
- Modify: `webApp/src/hooks/activityEpisodesManager.ts` (+ its test), `webApp/src/containers/activityVerbs.ts` (+ its test), `webApp/src/containers/ActivityExperienceContainer.tsx`, `webApp/src/containers/PlanContainer.tsx`, `webApp/src/containers/McpSystemDesignContainer.tsx`, `webApp/src/components/activity/submitVerb.ts`, `webApp/src/components/comments/CommentMargin.tsx`.

**Interfaces produced:**
- `useDeliveryQueries.ts`: `useProject(projectId)`, `useProjects(owner)`, `useSessionState(projectId, kind)`, `useProjectSessionState(projectId, kind)`, `useConstructionSession(projectId, activityId)`, `useDesignHealth(projectId)`, `useEpisodesList(...)`, `useEpisodeTimeline(...)`, `useActivityView(projectId, activityId)` — **the same names and the same query-key shapes as today**, so `activityViewKey` invalidation keeps working.
- `useDeliveryMutations.ts`: `useStartProject`, `useDispatchActivityTask`, `useSubmitReviewDecision`, `useAskQuestions`, `useAcknowledgeStaleBasis`, `useSetProjectRunState`, `useOverrideActivity`, `useReplanProject`, `useSetProjectExecutionPolicy`, `useExecuteNextActivity`.

- [ ] **Step 1: Keep the query keys, change only the op.** Move every reader into `useDeliveryQueries.ts` with its CURRENT key factory verbatim. The keys are what `activityViewKey`, the plan's cascade predicate and every `invalidateQueries` call join on; changing them here would be a silent cache break that no test catches. Each hook's body changes in exactly one place — the op it calls:
  ```ts
  // BEFORE: api.systemDesignGetProject({ projectId })
  // AFTER:
  export function useProject(projectId: string) {
    return useQuery({
      queryKey: projectKey(projectId),          // UNCHANGED
      queryFn: () => api.deliveryQueryProjectView({ query: { kind: 'summary', projectId } }),
      select: (v) => v.summary,                 // QueryProjectView returns a union; unwrap here
      // … every other option unchanged
    });
  }
  ```
  The `select` unwrap is the one new idea and it belongs in exactly one place per hook. Write it once per view kind; do not spread `view.summary!` through the containers.

- [ ] **Step 2: `activityEpisodesManager.ts` disappears as a dispatch table.** It maps `requirements`/`architecture` → `'systemDesign'` and everything else to a Manager (`:31-32`) so `useEpisodes` can pick an op. With one Manager there is no pick: `deliveryQueryProjectView({ query: { kind: 'episodes', projectId, artifactKind | activityId } })` takes the selector directly. Delete the module and its test, and update the four call sites in `useEpisodes`'s new home. Keep the artifact-kind-vs-activity-id DECISION (it is still real — it chooses the selector), as a two-line helper in `useDeliveryQueries.ts` with the test moved across.

- [ ] **Step 3: `containers/activityVerbs.ts` loses its rail branch.** Today it returns `VerbTarget`s `constructionPhaseDecision` / `designReviewDecision` / `sdpDecision` / `projectAsk` / `none`, and the container maps a target onto a hook. After this task every write is `useSubmitReviewDecision` / `useAskQuestions` / `useSetProjectRunState` / `useOverrideActivity` / `useDispatchActivityTask`, so the table's job shrinks to building the `ReviewDecisionInput`:
  ```ts
  // One Manager, one write per intent. What used to be a rail choice is now only a
  // question of which ReviewDecisionInput members the intent fills:
  //   approve        -> { decision: ReviewDecision.ReviewApprove }
  //   sendBack       -> { decision: ReviewDecision.ReviewReject }
  //   approvePlan    -> { decision: ReviewDecision.ReviewApprove, optionId }
  //   resolveComment -> { decision: ReviewDecision.ReviewSetCommentStatus, commentId, commentStatus: 'resolved' }
  //   reopenComment  -> { decision: ReviewDecision.ReviewSetCommentStatus, commentId, commentStatus: 'open' }
  //   advance        -> { decision: ReviewDecision.ReviewAdvance, acknowledgeStale }
  ```
  **`NO_CONSTRUCTION_THREAD_OP` becomes dead and is deleted**, and with it `submitVerb.ts`'s `allowAsk` suppression and `CommentMargin`'s `allowQuestions` false branch, plus the notice line `submitVerb.ts` rendered to explain the gap. This is the R2/GAP-6 asymmetry the stage-5 earmark deferred to stage 4 — closing it here is the visible user-facing win of 4a, and it is the ONE place 4a changes what a screen offers.
  - [ ] **Verify first:** `grep -rn "NO_CONSTRUCTION_THREAD_OP\|allowAsk\|allowQuestions" webApp/src uitests` and delete only what the grep proves has no other reader. A uitests assertion on the notice line must be deleted in Task 9, not left dangling.

- [ ] **Step 4: The MCP widget (R12).** `containers/McpSystemDesignContainer.tsx` consumes `useSessionState`, `useProject` and `useRequestArtifactDraft`. Re-point all three at the new hooks; do NOT delete the container or `SystemDesignView`/`SlimSpine`/`PHASE1_ORDER` (stage 6 owns those).
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/webApp
  ASDF_NODEJS_VERSION=lts npm run build:mcp
  ```
  Expected: a clean build. This is a gate of this task.

- [ ] **Step 5: Delete the old hook modules, one grep per file.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/webApp
  for f in useDesignMutations useProjectDesignMutations useConstructionMutations useStartDesign \
           useCreateProject useProject useProjects useSessionState useProjectSessionState \
           useConstructionSession useConstructionSessions useDesignHealth useEpisodes useActivityView; do
    echo "== $f"; grep -rn "hooks/$f" src uitests ../uitests 2>/dev/null | grep -v "hooks/$f.ts:"
  done
  ```
  A file is deleted only when its block prints nothing. `usePauseConstruction` had no caller at all before this task (stage-5 earmark) — `useSetProjectRunState` covers pause and resume, and the pause control's absence stays an earmark, not a new feature.

- [ ] **Step 6: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/webApp
  ASDF_NODEJS_VERSION=lts npm run check
  ASDF_NODEJS_VERSION=lts npm run build:mcp
  ```
  `npm run check` = `tsc -b` → `eslint .` (boundaries + `jsx-a11y` strict + `react-hooks/exhaustive-deps: error`) → `prettier --check` → `node --test`. It must be GREEN — this is the commit that clears Task 7's red.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  git add webApp/src
  git commit -F - <<'MSG'
  refactor(webapp): one hook family for one Manager

  Twenty-four hooks in fourteen modules called forty ops across three manager
  namespaces. They become useDeliveryQueries + useDeliveryMutations against
  twelve. Every query KEY is unchanged — the keys are what activityViewKey
  invalidation and the plan's cascade predicate join on, and moving them would be
  a cache break no test catches.

  activityEpisodesManager's dispatch table is gone: with one Manager there is no
  manager to pick, only a selector (artifactKind or activityId) to fill.

  The user-visible change is the one stage 5 earmarked as R2/GAP-6: a
  construction review can now Resolve, Reopen and Ask, because deliveryManager
  carries SetReviewCommentStatus and AskQuestions for every rail where
  constructionManager carried neither. NO_CONSTRUCTION_THREAD_OP, submitVerb's
  allowAsk suppression and CommentMargin's allowQuestions false branch are all
  dead and deleted, along with the notice line that explained the gap.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 9: Preview fixtures and uitests

Twenty preview fixture states key their `ops` map by op id, and the uitests' REST path regexes name the three old manager paths. Both are mechanical; the only judgement call is whether to do the rename in place or introduce the shared-fixture mechanism the stage-5 earmark proposes.

**Ruling, made here: rename in place.** The shared-fixture `$ref` mechanism is a new schema, a new resolver in `fixture-schema.mjs`, a new validation path and a change to how every preview test reads a fixture — against a rename that is one deterministic map over 20 files. Introducing it inside the stage that also re-keys every op would mean a fixture failure could be either the rename or the new mechanism. The earmark stays open for its own stage, with this stage's measurement attached (20 files, 16 MB, one mechanical map).

**Files:**
- Modify: `uitests/preview-fixtures/web-client/**/*.json` (20 states).
- Modify: `webApp/preview/fixtures/web-client/activity-experience/*.json` (the one smoke fixture).
- Modify: `uitests/tests/support/designStubs.ts` (`:60,71,98,112`), `dispatchGuard.ts` (`:78,84`), `gating.ts` (`:144,256,314`).
- Modify: `webApp/scripts/fixture-schema.mjs` (+ `fixture-schema.test.mjs`) — the op roster.
- Modify: `webApp/scripts/mcp-tools.test.mjs` — it HARD-CODES `projectDesignRequestSdpCommit` and `projectDesignRequestSDPCommit` (`:41,42`), `projectDesignSubmitSdpDecision` (`:47`) and `constructionExecuteNextActivity` (`:49,50`), and it runs under `npm run check`'s `node --test`. Re-key each to its `delivery*` successor (`deliveryDispatchActivityTask`, `deliverySubmitReviewDecision`, `deliveryExecuteNextActivity`) and drop the casing-variant pair, which existed only to pin the `SDP`-vs-`Sdp` camel split on an op id that no longer exists.
- **NOT modified: `uitests/tests/support/testids.ts:594`.** It is `export const ACTIVE_PHASE_ID: PhaseId = 'systemDesign'` — the root `phase` VALUE, which the Global Constraints freeze ("Wire-visible identifiers are NEVER renumbered: … the root `phase`") and which `testids.ts:73-75` builds `phase-card-systemDesign` ids from. It is not an MCP tool roster and it does not move in 4a.
- Modify: `webApp/src/previewShell/networkGuard.test.ts`, `webApp/src/api/ops.test.ts`, `uitests/tests/meta/suite-rules.spec.ts`, `uitests/tests/status-decides-outcome.spec.ts`, `uitests/tests/preview/preview-shell.spec.ts`.

- [ ] **Step 1: Write the rename map once, as data.** Create `/tmp/opmap.json` in the scratchpad (NOT in the repo) holding the forty→twelve map, keyed old→new, derived from Task 6's dispatch tables:
  ```json
  {
    "systemDesignCreateProject": "deliveryStartProject",
    "systemDesignSetResearchInput": "deliveryStartProject",
    "systemDesignSetOperatingModel": "deliveryStartProject",
    "systemDesignStartSystemDesign": "deliveryStartProject",
    "systemDesignRequestArtifactDraft": "deliveryDispatchActivityTask",
    "projectDesignRequestArtifactDraft": "deliveryDispatchActivityTask",
    "systemDesignSubmitReviewDecision": "deliverySubmitReviewDecision",
    "projectDesignSubmitReviewDecision": "deliverySubmitReviewDecision",
    "constructionSubmitPhaseDecision": "deliverySubmitReviewDecision",
    "systemDesignSetReviewCommentStatus": "deliverySubmitReviewDecision",
    "projectDesignSetReviewCommentStatus": "deliverySubmitReviewDecision",
    "projectDesignRequestSdpCommit": "deliveryDispatchActivityTask",
    "projectDesignSubmitSdpDecision": "deliverySubmitReviewDecision",
    "systemDesignAdvancePhase": "deliverySubmitReviewDecision",
    "projectDesignAdvanceToConstruction": "deliverySubmitReviewDecision",
    "systemDesignAskQuestions": "deliveryAskQuestions",
    "projectDesignAskQuestions": "deliveryAskQuestions",
    "systemDesignAcknowledgeStaleBasis": "deliveryAcknowledgeStaleBasis",
    "projectDesignAcknowledgeStaleBasis": "deliveryAcknowledgeStaleBasis",
    "constructionPauseProject": "deliverySetProjectRunState",
    "constructionResumeProject": "deliverySetProjectRunState",
    "constructionOverrideActivity": "deliveryOverrideActivity",
    "constructionRunReplanSweep": "deliveryReplanProject",
    "constructionSetReviewPolicy": "deliverySetProjectExecutionPolicy",
    "constructionUpdateReviewPolicy": "deliverySetProjectExecutionPolicy",
    "constructionExecuteNextActivity": "deliveryExecuteNextActivity",
    "systemDesignGetProject": "deliveryQueryProjectView",
    "systemDesignListProjects": "deliveryQueryProjectView",
    "systemDesignGetSessionState": "deliveryQueryProjectView",
    "projectDesignGetSessionState": "deliveryQueryProjectView",
    "constructionGetSessionState": "deliveryQueryProjectView",
    "constructionGetPumpStatus": "deliveryQueryProjectView",
    "systemDesignGetDesignHealth": "deliveryQueryProjectView",
    "systemDesignListEpisodesForArtifact": "deliveryQueryProjectView",
    "projectDesignListEpisodesForArtifact": "deliveryQueryProjectView",
    "constructionListEpisodesForActivity": "deliveryQueryProjectView",
    "systemDesignGetEpisodeTimeline": "deliveryQueryProjectView",
    "projectDesignGetEpisodeTimeline": "deliveryQueryProjectView",
    "constructionGetEpisodeTimeline": "deliveryQueryProjectView",
    "constructionQueryActivityView": "deliveryQueryActivityView"
  }
  ```
  **Many-to-one is the whole difficulty, and it is EIGHT collapses, not one — three large ones handled by `DISCRIMINATOR` and five small many-to-one groups that must ALSO be discriminated, never thrown on: `deliveryDispatchActivityTask` (3 old ops → discriminate by which request shape the fixture carries), `deliveryAskQuestions` (2 → by manager), `deliveryAcknowledgeStaleBasis` (2 → by manager), `deliverySetProjectRunState` (pause + resume → `{state: "paused"|"running"}` from the old op id), `deliverySetProjectExecutionPolicy` (set + update → both map to the one op; if a fixture carries both, keep the `update` result). Today zero fixtures carry two ops of any of these five groups (measured), so the `else` collision `throw` is a latent trap, not a live one — extend `DISCRIMINATOR` with all eight groups before running the script..** `deliveryQueryProjectView` absorbs 13 readers, `deliverySubmitReviewDecision` absorbs 8 writers, and `deliveryStartProject` absorbs 4. A fixture stubbing two members of any one group would collapse two `ops` entries onto one key and lose one — silently, producing a plausible and wrong screen. Every one of the three therefore gets a discriminator that mirrors what the real op discriminates on, so no fixture needs a hand decision:

  | Merged op | Discriminator | Per-old-op value |
  |---|---|---|
  | `deliveryQueryProjectView` | `kind` (the `ProjectViewKind` the real query carries) | `systemDesignGetProject` → `summary`; `systemDesignListProjects` → `projects`; `systemDesignGetSessionState` / `projectDesignGetSessionState` / `constructionGetSessionState` → `session`; `constructionGetPumpStatus` → `pump`; `systemDesignGetDesignHealth` → `designHealth`; `systemDesignListEpisodesForArtifact` / `projectDesignListEpisodesForArtifact` / `constructionListEpisodesForActivity` → `episodes`; `systemDesignGetEpisodeTimeline` / `projectDesignGetEpisodeTimeline` / `constructionGetEpisodeTimeline` → `timeline` |
  | `deliverySubmitReviewDecision` | `decision` (the `ReviewDecision` member the real call fills) | `systemDesignSubmitReviewDecision` / `projectDesignSubmitReviewDecision` / `constructionSubmitPhaseDecision` / `projectDesignSubmitSdpDecision` → `verdict`; `systemDesignSetReviewCommentStatus` / `projectDesignSetReviewCommentStatus` → `commentStatus`; `systemDesignAdvancePhase` / `projectDesignAdvanceToConstruction` → `advance` |
  | `deliveryStartProject` | `step` (which of the four the old op was) | `systemDesignCreateProject` → `create`; `systemDesignSetOperatingModel` → `model`; `systemDesignSetResearchInput` → `research`; `systemDesignStartSystemDesign` → `start` |

  Three old ops map to `verdict` on the same rail only if a fixture stubbed two rails' verdict at once — which no single-project fixture does, and which the script's collision `throw` catches if one ever did. `projectDesignRequestSdpCommit` maps to `deliveryDispatchActivityTask`, not to the review op, and is a plain one-to-one.

- [ ] **Step 2: The rename script, run once, then thrown away.** Write it to the scratchpad and run it with `node`:
  ```js
  // /tmp/rekey-fixtures.mjs — one deterministic pass over the fixture tree.
  // THREE ops are many-to-one and none of them can be a flat rename: QueryProjectView
  // absorbs 13 readers, SubmitReviewDecision 9 writers, StartProject 4. Two old entries
  // would overwrite each other and render a plausible, wrong screen. Each merged entry is
  // therefore keyed by the same discriminator the REAL op carries — the view kind, the
  // decision member, the start step — so nothing needs a hand decision.
  import { readFileSync, writeFileSync } from 'node:fs';
  import { globSync } from 'node:fs';
  const MAP = JSON.parse(readFileSync('/tmp/opmap.json', 'utf8'));
  const DISCRIMINATOR = {
    deliveryQueryProjectView: {
      systemDesignGetProject: 'summary', systemDesignListProjects: 'projects',
      systemDesignGetSessionState: 'session', projectDesignGetSessionState: 'session',
      constructionGetSessionState: 'session', constructionGetPumpStatus: 'pump',
      systemDesignGetDesignHealth: 'designHealth',
      systemDesignListEpisodesForArtifact: 'episodes', projectDesignListEpisodesForArtifact: 'episodes',
      constructionListEpisodesForActivity: 'episodes',
      systemDesignGetEpisodeTimeline: 'timeline', projectDesignGetEpisodeTimeline: 'timeline',
      constructionGetEpisodeTimeline: 'timeline',
    },
    deliverySubmitReviewDecision: {
      systemDesignSubmitReviewDecision: 'verdict', projectDesignSubmitReviewDecision: 'verdict',
      constructionSubmitPhaseDecision: 'verdict', projectDesignSubmitSdpDecision: 'verdict',
      systemDesignSetReviewCommentStatus: 'commentStatus',
      projectDesignSetReviewCommentStatus: 'commentStatus',
      systemDesignAdvancePhase: 'advance', projectDesignAdvanceToConstruction: 'advance',
    },
    deliveryStartProject: {
      systemDesignCreateProject: 'create', systemDesignSetOperatingModel: 'model',
      systemDesignSetResearchInput: 'research', systemDesignStartSystemDesign: 'start',
    },
  };
  for (const path of globSync('uitests/preview-fixtures/**/*.json').concat(
                     globSync('webApp/preview/fixtures/**/*.json'))) {
    const doc = JSON.parse(readFileSync(path, 'utf8'));
    if (!doc.ops) continue;
    const next = {};
    for (const [op, value] of Object.entries(doc.ops)) {
      const to = MAP[op] ?? op;
      const table = DISCRIMINATOR[to];
      if (table) {
        const key = table[op];
        if (!key) throw new Error(`${path}: ${op} maps to ${to} with no discriminator — extend DISCRIMINATOR`);
        next[to] ??= {};
        if (next[to][key]) throw new Error(`${path}: two ops collapse onto ${to}.${key}`);
        next[to][key] = value;
      } else {
        if (next[to]) throw new Error(`${path}: ${op} collides with an already-mapped op on ${to}`);
        next[to] = value;
      }
    }
    doc.ops = next;
    writeFileSync(path, JSON.stringify(doc, null, 2) + '\n');
  }
  ```
  The three `throw`s are the point, and each names its own recipe: a missing discriminator means `DISCRIMINATOR` is short a row (add it from Step 1's table), and a duplicate key means one fixture genuinely stubbed two members of one group and a human must say which the merged screen reads. A silent overwrite would produce a fixture that renders a plausible but wrong screen, which no test catches.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  ASDF_NODEJS_VERSION=lts node /tmp/rekey-fixtures.mjs
  git diff --stat -- uitests/preview-fixtures webApp/preview/fixtures
  ```
  Expected: 20 + 1 files changed. The preview shell's fixture reader must be taught the nested `deliveryQueryProjectView[view]` shape in the same commit — it is a handful of lines in the shell's op resolver.

- [ ] **Step 3: `fixture-schema.mjs`'s roster and the REST path regexes.**
  - `webApp/scripts/fixture-schema.mjs`: the op roster becomes the twelve `delivery*` ids (plus operations and composition, unchanged). `fixture-schema.test.mjs` asserts the roster; update its expectation to the measured list, not a hand-typed one.
  - `uitests/tests/support/designStubs.ts`, `dispatchGuard.ts`, `gating.ts`: every `^/api/v1/system-design/…`, `/api/v1/project-design/…` and `/api/v1/construction/…` regex re-targets to `^/api/v1/delivery/…`. Find them all:
    ```bash
    cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
    grep -rn "api/v1/system-design\|api/v1/project-design\|api/v1/construction" uitests webApp/src webApp/preview
    ```
  - `webApp/scripts/mcp-tools.test.mjs`: re-key the four hard-coded op ids at `:41,42,47,49,50` per the Files list above. It runs under `npm run check`'s `node --test`, so a stale id is a red gate, not a stale comment. `webApp/scripts/mcp-tools.mjs` itself needs NO edit — it scans `server/internal/client/mcp/<mgr>/` by directory and throws loudly on an empty read.
  - **Do NOT touch `uitests/tests/support/testids.ts:594`.** `ACTIVE_PHASE_ID: PhaseId = 'systemDesign'` is the frozen root-`phase` value, not a tool name; re-keying it would break every `phase-card-systemDesign` id built at `testids.ts:73-75` and would violate this plan's own Global Constraint on wire-visible identifiers.

- [ ] **Step 4: Run the suites.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a/webApp
  ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests
  ASDF_NODEJS_VERSION=lts npx tsc --noEmit
  ASDF_NODEJS_VERSION=lts npx eslint .
  ASDF_NODEJS_VERSION=lts npx playwright test --project=preview
  ```
  Expected: **44/44 preview cases green** (42 from stage 5 plus the two the construction comment lifecycle adds — a Resolve and an Ask on a construction review, which Task 8 made possible and which must now be asserted rather than assumed). The preview project builds the preview itself and needs no Go server.

- [ ] **Step 5: Commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  git add uitests webApp
  git commit -F - <<'MSG'
  test(preview): re-key twenty fixtures and the uitests onto delivery

  Forty op ids become twelve across 20 preview fixture states, the smoke
  fixture, four uitests support modules and the fixture-schema roster. The
  many-to-one collapses are the difficulty, not the rename: QueryProjectView
  absorbs thirteen readers and SubmitReviewDecision nine, so a flat rename would
  have two fixture entries overwrite each other and render a plausible, wrong
  screen. The fixtures key the merged read by view kind, exactly as the real op
  discriminates, and the one-pass script throws on any collision rather than
  picking a winner.

  The shared-fixture $ref mechanism the stage-5 earmark proposes is NOT
  introduced here: it is a new schema, resolver and validation path against a
  rename that is one deterministic map over 20 files, and doing both at once
  would make a fixture failure ambiguous. The earmark stays open with this
  stage's measurement attached.

  Two new preview cases cover what stage 4a actually changed for a reader: a
  construction review can now resolve a comment and ask a question.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 10: Extend the drain note, and write the stage-4a earmarks

R4: 4a does not deploy. The stage-3 DRAIN NOTE has not been executed — stage 3 shipped on a branch and merged — so the next drain is stages 3 + 4a + 4b combined. This task makes the note true for what 4a added, and records what 4a found and could not fix.

**Files:**
- Modify: `docs/bugs/2026-09-24-stage3-rail-earmarks.md` — the DRAIN NOTE and the stage-4 ENTRY CRITERIA list.
- Create: `docs/bugs/2026-09-25-stage4a-earmarks.md`.

- [ ] **Step 1: Extend the DRAIN NOTE.** Add to `docs/bugs/2026-09-24-stage3-rail-earmarks.md`, under the existing five steps, keeping the note's voice:

  > **AMENDED 2026-09-25 (stage 4a). The drain now covers stages 3 + 4a + 4b, and 4a MUST NOT DEPLOY ALONE.** 4b changes workflow TYPE names and workflow ids again, so a deploy between the two would need its own drain for nothing. Merge both, drain once, release once.
  >
  > **What 4a added to the drain, beyond stage 3's five steps:**
  >
  > | Thing | Today | After 4a | Mechanism |
  > |---|---|---|---|
  > | TaskQueue `system-design` | systemdesign worker | **gone** | drain-and-cutover |
  > | TaskQueue `project-design` | projectdesign worker | **gone** | drain-and-cutover |
  > | TaskQueue `construction` | construction worker | **gone** | drain-and-cutover |
  > | TaskQueue `delivery` | — | new, one worker, eleven workflow types | — |
  > | Schedule `construction:pumpSweep` (30 s) | construction | **DELETE, then register `delivery:pumpSweep`** | Schedule ids are namespace-global; `RegisterSchedules` is idempotent for CREATE but will not move a Schedule's task queue |
  > | Schedule `construction:replanSweep` (300 s) | construction | **DELETE, then register `delivery:replanSweep`** | same |
  > | `{p}:phaseAdvance` | BOTH design Managers, same string | `{p}:phaseAdvance:systemDesign` / `:projectDesign` | new histories only; drain the in-flight ones (they last seconds) |
  >
  > **Every workflow TYPE name and every other workflow id is UNCHANGED in 4a** — that is deliberate, and it is why the fifteen construction replay fixtures still replay. The drain is required anyway, for one reason: **the registered activity-name set SHRANK by 92 names** (231 → 139). A worker built from an older commit can serve a workflow this commit started; a worker built from THIS commit cannot serve a workflow an older one started against a name that no longer exists. Drain before release, in both directions.
  >
  > **Delete the two old Schedules explicitly.** `RegisterSchedules` creates if absent and is a no-op if present, so leaving `construction:pumpSweep` in place would leave a Schedule pointing at a task queue no worker polls — a silent dead sweep, not an error. Delete them with `temporal schedule delete --schedule-id construction:pumpSweep` (and `construction:replanSweep`) as an explicit drain step, BEFORE the release, and confirm with `temporal schedule list`.
  >
  > **Rollback is unchanged and still one-way:** restore `project.json` FIRST, then roll the image.

- [ ] **Step 2: Retire the stage-4 ENTRY CRITERIA that 4a discharged**, in place, with a strikethrough and the commit that closed each — matching how the note already records stage 3's discharge:
  - Arm the per-activity CAS → **LANDED IN STAGE 4a** (Task 3).
  - `buildStatus` enum/vocabulary rule → **LANDED IN STAGE 4a** (Task 2), and CORRECT the criterion's text: it says "before `delivery-manager` is authored **as `planned`**", and `delivery-manager` is NOT authored as planned — `ALIGN-STALE-PLANNED` is an Error for a planned component that has a package, and 4a lands the package in the same commit.
  - Delete the three deprecated facets → **still open, still 4b, still post-drain** — and CORRECT the claim that `DH-CONTRACT-DEADOP` "flips back to `assertAbsent` then". Measured: deleting `constructionTransitionAccess` clears the `RecordOperatorNote` duplicate but NOT the `AcknowledgeStaleBasis` one, because `projectStateAccess` publishes it too and spec §5.3 keeps `projectStateAccess` at 9 ops. The pin stays until one of those two verbs goes.
  - `engineReviewPolicy` triplication → **PARTLY DISCHARGED**: the three byte-identical copies collapsed to one in 4a because the compiler forced it (Task 6 Step 2b, class C). The `EffectiveGate`/`RequiresHuman`/floor-keyword move into the review Engine is still 4b's.
  - Everything else stays open and moves to the new earmark file's "carried to 4b" section.

- [ ] **Step 3: Write `docs/bugs/2026-09-25-stage4a-earmarks.md`**, carrying:
  - **The three orphan `.activityExecution` rows.** `C-system-design-manager`, `C-project-design-manager` and `C-construction-manager` name activities the derived plan no longer holds (slot 9 went 32 → 30 and gained `C-delivery-manager`). **They are KEPT**: they are Done/Integrated history with real attempts and provenance, and deleting history to make a read tidy is the wrong trade. `QueryActivityView` answers `NotFound` for them because `committedActivityItem` is absent, which is the honest answer — the activity is not in the plan. Do NOT run `make construction-state-reset`; it deletes ALL construction history. If a future stage wants them reachable, migrate them onto `C-delivery-manager` deliberately, with a written provenance note saying three rows were merged.
  - **`ActivityOptions` divergence.** The three old worker manifests answered the per-activity-name options hook differently for at least some shared activity names; 4a kept the construction answer (Task 6 Step 9). Record the exact divergences the merge found, with the old values — a retry budget or timeout that silently changed for a design rail is a behaviour change 4a claims not to have made.
  - **The three `Repo()` hooks merged into one** (Task 6 Step 14). Record whether their bodies differed and, if so, which body survived and what the design rails lost.
  - **`GetEpisodeTimeline` ×3 collapsed to one** (Task 6 Step 9 / Step 2b class C). Name the surviving implementation.
  - **The shared-fixture `$ref` mechanism** (Task 9's ruling): still open, now with a measurement — 20 files, 16 MB, one deterministic map; the mechanism is worth its own stage, not a passenger in a re-key.
  - **The three `.claude` materialized assets** that still name the retired Managers. Re-run the sweep now that the names are gone:
    ```bash
    cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
    grep -rln "systemDesignManager\|projectDesignManager\|constructionManager" .claude/skills .claude/commands .claude/agents
    ```
    Every hit is method-assets content (`server/Makefile:9`, `:175` materializes it), so the fix is a platform release and a founder STOP in its own right. Record the exact file:line list.
  - **`uitests/testdata/coreUseCasesProject.json`** is already drifted (16 decisions against the model's 18, now 17) and its regen reads the committed `main` branch (`cmd/gen-uitests-fixtures/main.go:96`), so it cannot be fixed in a worktree. Post-merge step, carried since stage 1: `cd uitests && npm run regen:core-use-cases-fixture`.
  - **`systemtests` is DONE in 4a (Task 6 Step 14b), and the reason it had to be is worth recording**, because the first draft of this plan got it wrong in both directions. The failure was never the `stp_uc*` drift gate: `systemtests/Makefile`'s `gen-check` runs `cmd/gen-systemtests` against `.testingState.systemTestPlan`, which 4a does not edit, so those tables regenerate byte-identically and regenerating them fixes nothing. The failure is `make gen-sdk` → `pruneStaleSDK` (`cmd/appgen/main.go:302-337`) DELETING `systemtests/internal/sdk/{http,mcp,types}_{system-design,project-design,construction}.gen.go`, which the hand-written harness calls — a separate Go module that the server module's own gates never compile, whose CI (`systemtests.yml`) triggers on `server/**` and `.aiarch/**`. `.testingState.systemTestPlan.useCaseIndex`'s four references to `drive-system-design` and `execute-a-construction-activity` were re-pointed in the same commit rather than left dangling. **Earmark for 4b:** the wire system tests exercise the twelve ops only through the harness's re-pointed call sites — nobody has yet written a system test for a behaviour that only the merged contract makes possible (a construction review resolving a comment).
  - **What 4b must do FIRST**, in order: (1) delete the three deprecated RA facets, post-drain, and re-measure `DH-CONTRACT-DEADOP`; (2) replace the twelve dispatcher bodies with the generic DAG child and the parallel pump, which is what makes the `deliveryManager` line count fall below the sum of its predecessors (spec §9's acceptance, deliberately NOT measured in 4a — 4a is line-neutral by design); (3) the `applyRecovering` terminality-`Conflict` class, which stops being an edge case the moment the pump starts more than one child; (4) engines 7→4, which needs a founder ruling first (the only surviving statement of that ruling says 7→5).

- [ ] **Step 4: Commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  git add docs/bugs
  git commit -F - <<'MSG'
  docs(earmarks): what stage 4a changed about the drain, and what it left

  The drain now covers stages 3 + 4a + 4b and 4a must not deploy alone: 4b
  changes workflow types and ids again, so a release between them would need its
  own drain for nothing.

  4a adds four task-queue owner changes, two Schedules that must be DELETED and
  re-registered (RegisterSchedules creates but will not move a Schedule's queue,
  so leaving construction:pumpSweep behind is a silent dead sweep), and the
  phaseAdvance re-key. Every other workflow type and id is unchanged — but the
  registered activity-name set SHRANK by 92, and that alone makes the drain
  non-negotiable in both directions.

  Two stage-4 entry criteria are struck through as discharged, and two are
  corrected: buildStatus was never going to gate a `planned` delivery-manager
  (ALIGN-STALE-PLANNED forbids that posture), and DH-CONTRACT-DEADOP does not
  flip when the deprecated facets go, because projectStateAccess publishes
  AcknowledgeStaleBasis too.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 11: Amend the spec, and hand 4b its starting line

The spec's §8 stage-4 row describes stage 4 as one stage. It is two, and the split is a ruling with evidence behind it. A planner reading §8 next must find the split, not re-derive it.

**Files:**
- Modify: `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` — §4's 12-op table, §8's stage table and must-ship-together set, §9's acceptance line, §10's risks.

- [ ] **Step 1: Split the §8 stage-4 row into 4a and 4b**, keeping the table's voice and its "| Stage | Content | Ships |" shape:
  - **4a DeliveryManager (model + package)** — SHIPPED 2026-09-25 on branch `activity-experience-stage4a`, N commits from `4baed01a`. Delivered: the indivisible model commit (`delivery-manager`, the 12-op `deliveryManager` contract, 16 relationships, `execute-a-project-activity` with every re-keyed dynamic view, eleven re-parentings, `commit-to-a-project-option` demoted, the three components/contracts deleted); `internal/manager/delivery` with the eleven workflow files moved verbatim, the three impl files merged into one and the three test files into one; the twelve ops as a thin dispatcher over the forty; ONE worker on task queue `delivery` registering all eleven existing workflow types under their existing names; the per-activity CAS armed; the `buildStatus` vocabulary; the `phaseAdvance` re-key; four new design-rail replay fixtures; the `systemtests` harness re-pointed onto the regenerated SDK; the webApp/uitests/MCP re-key. **Ships: one manager, one wire. No deploy.**
  - **4b DeliveryManager (the behaviour)** — the generic DAG child, the parallel pump, design activities as children, deterministic Project Design (§6), the co-author twins and the Phase-2 commands deleted, one review engine, engines 7→4, the artifact-as-of-revision read, the batched `QueryProjectView(plan)`, the `applyRecovering` Conflict class, the deprecated facets deleted post-drain. **Ships: the behaviour. Drain → cutover with 4a.**
- [ ] **Step 2: Correct §4's 12-op table** where 4a's contract differs from it, with one clause of reason each: `SubmitReviewDecision` takes a `ReviewDecisionInput` object (the nine writers need `acknowledgeStale`, `optionId`, `commentId` and `commentStatus`); `QueryProjectView` takes a `ProjectViewQuery` object (the thirteen readers need six selectors); `StartProject` is incremental and idempotent (the SPA sets research and the operating model after creation, not at it); `AskQuestions` carries `[]AnchoredComment`, not strings.
- [ ] **Step 3: Correct §5.2's acceptance baseline.** It quotes "≈ 6,300 twin lines + `constructactivity.go`" — that is the 2026-08-30 measurement. Measured at `4baed01a`: the twins are 4,997 + 3,027 = **8,024**; hand-written non-generated across the three packages is **25,643**; non-test total **29,778**; tests **31,798**. Restate the baseline as measured at the stage-4 branch base, and say explicitly that **4a is line-neutral and §9's "line count of delivery manager < sum of predecessors" is measured at the end of 4b**.
- [ ] **Step 4: Correct §6's "nine Phase-2 draft commands"** to **eight dispatchable draft commands plus the SDP assembly** — `DesignCommandFor` returns `""` for `KindSdpReview` ("assembled server-side"), pinned by `access_test.go:7602-7603` — the "undispatchable combinations" block (`{"sdpReview draft undispatchable (assembled server-side)", KindSdpReview, DesignJobModeDraft, "", ""}`), NOT the Phase-1/Phase-2 draft table at `:7578-7586`.
- [ ] **Step 5: Add to §8's must-ship-together set** the seven things 4a proved belong to stage 4's first commit and the spec does not name: `cmd/clientgen/mcpdocs.go` (a build gate, not documentation); **`cmd/appgen/main.go:84`'s `managers` list**, which is a THIRD manager list driving temporalgen and the SDK and is not either web list; `cmd/server/managerlog.go` and `hooks.go`; the slot-6 deployment container list (`DEP-CONTAINER-REF` resolves by name); the `engine_test.go` `realizedViews` table AND its `RuleContractOpMax` pin; `make derived-plan-write`'s slots 9/10 (which needs the Makefile seam moved first); and **the `systemtests` module's harness**, because `make gen-sdk`'s prune-stale rewrite deletes the SDK it calls and `systemtests.yml` triggers on `server/**` and `.aiarch/**`. Say in the same breath that `.testingState.systemTestPlan.useCaseIndex` must be re-pointed whenever a use case is deleted from slot 4 — it is the join the `stp_uc*` tables are generated through.
- [ ] **Step 6: Extend §10's "Self-amendment blast radius" risk** with the measured radius: one commit touching 4 generated layers, 3 test goldens, 20 fixtures, 2 composition-root files and 92 registered Temporal names.
- [ ] **Step 7: Commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4a
  git add docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md
  git commit -F - <<'MSG'
  docs(spec): stage 4 is two stages, and five corrections the build measured

  §8's stage-4 row described one stage that would land the model, the package
  collapse, the generic DAG child, the parallel pump, deterministic Project
  Design and an engine merge together. Those are two different debugging loops:
  4a's failure mode is "nine gates are red and you cannot tell which of nine
  simultaneous changes did it", 4b's is "a workflow deadlocks". The row splits.

  Corrected against what the build measured: the 12-op table's three signatures
  that could not reach the forty behaviours they replace; §5.2's acceptance
  baseline, stale by ~1,700 lines, and the fact that 4a is line-neutral so §9's
  smaller-than-sum acceptance belongs at the end of 4b; §6's "nine Phase-2 draft
  commands", which is eight plus the SDP assembly; and seven members of the
  must-ship-together set the spec never named, each of which is a gate — among
  them a third exposed-manager list that generates the Temporal worker, and a
  separate Go module whose SDK make gen-sdk deletes.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

## Self-review

### Spec §4 → task coverage

| §4 clause | Where it lands | Evidence it is discharged |
|---|---|---|
| B-02/B-03/B-04/B-13/B-14 → one **Project Delivery Workflow** | **already shipped in stage 1**; 4a only expires its transitional-facet waiver | Task 6 Step 7, `slots["3"].model.waivers` |
| three Managers → **`delivery-manager`** (`deliveryManager`) | Task 6 Steps 5, 7 | `validate --slot System` 0 errors with 3 Managers; `SYS-CARD-MGR` absent |
| `reviewEngine` generalized | **already shipped in stage 2** — nothing to do | the 7-param `ProposeReviews` in `.serviceContracts.reviewEngine` |
| `designSessionAccess` + `constructionTransitionAccess` verbs unify (§5.3) | **shipped in stage 3**; their DELETION is 4b (post-drain) | Task 10 Step 2 corrects the earmark's `DH-CONTRACT-DEADOP` claim |
| 3 core use cases; two entries as step-local `alt` groups | Task 6 Step 8 | 17 decisions, 17 views, `USECASE-DYNAMIC-MISSING` absent |
| every variation re-parents; `commit-to-a-project-option` demoted | Task 6 Step 8 (the eleven-row table + the `UC-VARIATION-REF` triple) | `UC-VARIATION-REF` absent |
| `deliveryManager` contract, 12 ops | Task 6 Steps 5, 6 | `DH-CONTRACT-OPCOUNT-MAX` silent (R8, measured) |
| M0 approval is `SubmitReviewDecision` on the projectDesign gate, no separate op | Task 6 Steps 5 (no M0 op) and 9 (`taskID == "sdpReview"` → `pd.SubmitSDPDecision`) | the dispatch table's projectDesign rows |

### Spec §5.3 → task coverage

| §5.3 clause | Where |
|---|---|
| one verb family, stage → review → commit | **shipped in stage 3**; 4a moves it unchanged (R13) — Task 6 Step 13's nineteen fixtures are the proof |
| two append-only ledgers, everything else derived | shipped in stage 3; untouched |
| per-activity `Version`, CAS "written and tested but NOT YET ARMED … a stage-4 contract change and a stage-4 ENTRY CRITERION" | **Task 3** — the only §5.3 clause 4a discharges |
| the three folded facets stay deprecated in place until the stage-4 drain | Task 6 Step 5 keeps all three as deps (R-E); deletion is 4b, Task 10 Step 2 |
| `projectStateAccess` stays at 9 ops | unchanged; measured in Task 6 Step 1's op-count query |

### Spec §8 "stage 4 (first commit)" must-ship-together → task step

| Member | Step |
|---|---|
| `delivery-manager` component | 6.7 |
| `deliveryManager` contract (12 ops) | 6.5 + 6.6 |
| relationships | 6.7 (16 table rows = **17** JSON edges, R6; 76 → 56) |
| `execute-a-project-activity` core UC with every re-keyed dynamic view | 6.8 |
| re-parented variations | 6.8 |
| deletion of the three Manager components/contracts | 6.7 |
| the new Go package | 6.2, 6.2a, 6.2b, 6.2c, 6.9 |
| old slot-5 component deletion | 6.7 |
| `cmd/clientgen/main.go` + `cmd/appgen/main.go` exposed lists | 6.4 — **all three lists**, including `appgen/main.go:84`'s `managers`, which the spec does not know exists |
| `arch_test.go` allowlists | 6.11 |
| `registered_names_test.go` golden | 6.12 |
| drain | Task 10 (R4 — recorded, not executed) |
| *(not in §8, added here because each is a gate)* `mcpdocs.go`, `managerlog.go`, `hooks.go`, slot 6, `engine_test.go`, slots 9/10, the `systemtests` harness + `.testingState.systemTestPlan.useCaseIndex` | 6.10, 6.14, 6.14, 6.7, 6.11, 6.15, **6.14b** — and Task 11 Step 5 writes them into §8 |

### Placeholder scan (re-run after the pre-flight revisions)

**Count: 0.** No step says "and so on", "etc.", "update the tests", "handle the rest", "TBD", "<fill in>" or "as appropriate". No step defers a decision to the implementer that this plan could have made. Every code step carries its code and every gate step carries its command and its expected output.

Four steps deliberately delegate a LITERAL to a mechanical oracle rather than transcribing it. Each names the oracle, so none is a hole an implementer has to fill from judgement:
1. **The class-D rename table in Task 6's commit message** (`<the class-D rename table>`) — the oracle is Step 2b.0's measured collision dump plus the compiler. The classes and their membership are given; only the chosen prefixes are the implementer's, and Step 2b fixes the prefix convention (`sd`/`pd`/`cs`, or the spelled-out rail).
2. **Task 1's capture-tool marshalling calls** — the oracle is `construction/manager_test.go:7470-7565`, which the step gives the exact `sed` range to read and instructs to copy verbatim. Inventing a history encoding would be worse than reading the one that already works.
3. **Task 6 Step 10's twelve MCP doc strings** — the oracle is `mcpemit.Generate`, which ERRORS on an empty doc (`mcpdocs.go:13-16`). Two of the twelve are written out in full as the shape; the other ten carry surviving sentences from the forty being deleted.
4. **Task 6 Step 6's `$defs` closure** — the oracle is the `jq` that computes the transitive `$ref` closure, run until the reachable set stops growing. Listing ~110 def bodies verbatim would be a copy of `project.json`, and "copy verbatim from the source contract" is the actual instruction.

### Type and name consistency

- `delivery-manager` (component id) / `DeliveryManager` (component + interface name) / `deliveryManager` (contract key + struct) / `internal/manager/delivery` (package) / `delivery` (task queue) — consistent in Tasks 6, 7, 8, 9, 10, 11, and consistent with `align.go`'s `StereotypeSuffixNormalizer` (`DeliveryManager` → `delivery`).
- The twelve op names are identical in Task 6 Step 5 (contract), Step 9 (dispatch tables), Step 10 (MCP docs), Task 7 (op ids), Task 8 (hook names) and Task 9 (fixture map). The fixture map in Task 9 Step 1 was written FROM Step 5's list, not beside it.
- `ReviewDecisionInput` / `ProjectViewQuery` / `ProjectView` / `ProjectViewKind` / `ProjectRunState` / `ExecutionPolicyInput` / `StartProjectResult` / `ProjectSessionStage` / `ProjectSessionStateView` appear in Step 6 (definition), Step 9 (consumption), Task 7 (`OUTPUT_NAMES`, for the two that are enums) and Task 8 (`activityVerbs.ts`) with the same spelling and the same members.
- `NoActivityVersionExpectation` (Task 3, exported) replaces `noActivityVersionExpectation` and is referenced by that name in Task 3 Steps 2 and 4 only — Task 6 does not touch it.
- `DERIVED_PLAN_PKG` is introduced in Task 5 and flipped in Task 6 Step 4; no other task names it.
- Fixture directory names: `design-pre-stage4/` and `phase2-pre-stage4/` from the moment Task 1 captures them, `git mv`'d under the SAME names in Task 6 Step 2, and found by those names in Step 13. **`c.dir` is never edited by any task** — Task 1's `Interfaces produced` block states the rule and Step 2 re-states it with an `ls` that confirms all seven directories.
- `appgen`'s three manager lists are distinguished everywhere they appear: `exposedManagers` (`clientgen/main.go:62`) and `WebExposedManagers` (`appgen/main.go:155`) are the WEB-WIRED set (2 entries, billing excluded); `managers` (`appgen/main.go:84`) is the CODE-GENERATED set (3 entries, billing included). Task 6 Step 4 sets each to its own value and says why they differ; GREEN POINT B's `worker.gen.go` check and Execution risk 8 both key on the third.

### The two claims this plan makes that a reviewer should check first

1. **`DH-CONTRACT-OPCOUNT-MAX` goes silent.** Re-derivable in one command: `jq -r '.serviceContracts | to_entries[] | "\(.value.interface.operations|length)\t\(.key)"' .aiarch/state/project.json | sort -rn | head -5`. Today: 16, 13, 12, 12, 11. Remove the 16 and the 13, add a 12: nothing is above 12. The stage-1 plan predicted the opposite and a plan that copied it would ship a red test.
2. **The golden lands at 139.** Re-derivable from `grep -c RegisterActivityWithOptions` on the five `worker.gen.go` files plus the workflow counts. If the measured number is not 139, a dep was dropped or double-registered — both are real defects, and neither is fixed by editing the literal.

**Open question carried to the founder:** none blocks execution. The one thing 4b cannot start without is the engines 7→4 ruling (spec §1 says 4, the only surviving statement of the 2026-08-30 ruling says 7→5) — recorded in Task 10 Step 3's "what 4b must do first", not needed here.
