# Unified Activity Experience & Delivery Manager — Design

**Date:** 2026-09-20 · **Status:** founder-approved direction + UX prototype; spec for review
**Prototype:** branch `activity-experience-proto` — `webApp/proto.html` (harness, throwaway), `webApp/src/components/activity/` (reusable). Serve with `npx vite --port 5199`, open `/proto.html`.

## 1. Thesis

System design, project design and construction are one workflow: **agents generate artifacts, agents and humans review them, and an activity advances through its own little life cycle** (Righting Software App. A). Requirements, Architecture and Project Design are simply activities 1–3 of the project network (Table 11-1), fenced from construction by milestone M0 (ch. 11 "About Milestones"). The product stops having three phase rails with three managers and three UIs; it has one plan of activities, one manager, one review engine, one staging/review rail, and one full-screen Activity Experience.

### Rulings this spec rests on (founder, 2026-09-20)

| # | Ruling |
|---|---|
| R1 | Requirements, Architecture, Project Design are **activities** at the head of the plan. Today's per-artifact steps become **tasks** inside them. |
| R2 | Every task is either an **agentic dispatch** (produces artifacts; its screen shows the episode) or an **artifact review** (its screen shows the artifact, commentable; approve / send back). Tasks form a DAG; parallel branches are real. |
| R3 | One manager, **`DeliveryManager`**: a parent **pump** workflow per project + a child **activity-lifecycle** workflow per activity. |
| R4 | "Commit to a project option" is `execute-a-project-activity` on the Project Design activity. **3 core use cases.** |
| R5 | The per-activity-type task DAG is **platform-fixed** data (method-assets), not per-project. |
| R6 | Design and construction **unify onto one stage → review → commit rail**. Construction gains persisted review threads, rounds and verdicts. |
| R7 | **Project Design is deterministic.** No agent-drafted steps. The activity is ONE review task (SDP Review · M0): the user sees the engine-derived plan and **cost estimate**, may comment, and approves before construction starts. There is no send-back; to change the plan, amend the Architecture. |
| R8 | One **review engine** decides who reviews and whether a human must, for every activity. |
| R9 | UX: opening an activity goes **full screen immediately**; the right-hand preview pane is removed. The stepper becomes a **branching graph**. The iteration unit is a **revision**; one episode is shown, with a revision select shared by dispatch and review screens. The plan has LIST and GRAPH (React Flow, a tile per activity) lenses. |

### Supersedes / stands

- **Supersedes** the 2026-08-30 S2 spec's 5-node design prefix (`design-mission … design-system`) → 3 activities whose old steps are tasks; and the 2026-09-12 A1 row "#1–3 = M0 only".
- **Stands:** one delivery pump; M0 is non-negotiable (no construction dispatch before the SDP approval); `phase` becomes derived from milestone position (freeze, deprecate, never renumber); ArtifactKind ordinals never renumber; retired kinds 2/6/7 stay retired; engines 7→4; RA consolidation ruling 6 (sourceControlAccess, episodeAccess stay separate).

## 2. Vocabulary

Extends the R3 naming ruling in `projectstateaccess.go` (the bare word "phase" stays banned).

- **Activity** — a node of the project network. Has a type, a **lifecycle** and a state.
- **LifecyclePhase** — Figure A-2 grouping of tasks with a weight and a binary exit criterion (its gate task). Earned-value unit.
- **Task** — a node of the lifecycle DAG. `kind ∈ {dispatch, review}`. A review task may be `gate: true` for its LifecyclePhase.
- **Attempt** — one execution of a task (exists today, append-only).
- **Revision** — NEW. Revision *n* of a LifecyclePhase = the *n*-th work-task attempt that reached the gate + the gate attempt that judged it. A send-back opens revision *n+1*. Failed/retried attempts inside a revision are sub-attempts of that revision, not new revisions.
- **Episode** — the trace of one agentic attempt (unchanged; `TargetRef = AttemptID`).

## 3. Lifecycles (platform-fixed)

Source of truth moves from the Go table `profileRows`/`phaseTasks`/`gateTasks` in **projectStateAccess** (wrong layer) to a data file in **method-assets** (`lifecycles.json`, released with the platform), loaded by the estimation engine's plan derivation and code-generated to `webApp/src/components/activity/lifecycles.gen.ts` (replacing `construction/lifecycleTemplates.gen.ts`).

```jsonc
{ "type": "service",
  "phases": [{ "id": "requirements", "label": "Requirements", "weight": 15, "gate": "srsReview" }, …],
  "tasks":  [{ "id": "srs", "kind": "dispatch", "phase": "requirements", "dependsOn": [], "command": "service-requirements", "workerClass": "senior-developer", "artifactKind": "SRS" },
             { "id": "srsReview", "kind": "review", "phase": "requirements", "dependsOn": ["srs"], "reviews": "srs" }, …] }
```

A `review` task names the dispatch task it judges (`reviews`); that pair is what a send-back re-opens. Validation (generator + Go test): acyclic, single root set, every phase has exactly one gate, weights sum to 100, every `reviews` target is an ancestor.

| Activity type | Lifecycle |
|---|---|
| **requirements** (new) | Mission `D→R` → Glossary `D→R` → Volatilities `D→R` → Core Use Cases `D→R`. Linear (each reads the previous). Weights 15/20/35/30. Commands: today's `*-draft` / `*-critique`. |
| **architecture** (new) | Draft Architecture & Call Chains `D` → Review Architecture `R`. One phase, 100. The artifact is the System model with its four views. |
| **projectDesign** (new) | SDP Review · M0 `R` only. 100. No dispatch; the artifact is computed (§6). Send-back is not offered. |
| **service** | Figure A-1: SRS `D` → SRS Review `R` → **fork** { Detailed Design `D` → Design Review `R` → Construction (+ test client) `D` → Code Review `R` → Integration `D` } ∥ { STP `D` → STP Review `R` } → **join** Testing `R`. Weights 15/20/10/40/15 (Table A-1). `someConstruction` / `testClient` stay conditional sub-attempts of their phase's work task, not nodes. |
| **frontend** | Same shape as service with today's labels (UX Requirements / Design / Flows / Construction / Integration). |
| **deployment, documentation, uiDesign, integration, testing:\*** | Today's ordered subsets, each phase = `D→R`; linear. |

The existing twelve `MethodTask` ids and `AttemptID` format are kept, so the append-only ledgers need no rewrite.

## 4. Architecture model (`project.json`)

Self-amendment of archistrator's own model; code and model move together (alignment gate).

- **Volatility:** B-02, B-03, B-04, B-13, B-14 merge into one **Project Delivery Workflow** ("how a project's activities are scheduled and how one activity's task DAG advances"). `designHealthEngine`'s rules stop being a facet of the system-design volatility (it dissolves into methodcheck per the 08-30 ruling).
- **Components:** `system-design-manager`, `project-design-manager`, `construction-manager` → **`delivery-manager`** (`deliveryManager`). `reviewEngine` generalized. `designSessionAccess` + `constructionTransitionAccess` review/staging verbs unify (§5.3).
- **Core use cases (3):** `execute-a-project-activity` (absorbs `drive-system-design`, `commit-to-a-project-option`, `execute-a-construction-activity`; two entries — timer/pump and clientAction — as step-local `alt` groups in one dynamic view), `operate-a-delivered-system`, `bill-the-user-for-usage`. All former variations of the three absorbed cases re-parent to `execute-a-project-activity`; `commit-to-a-project-option` is demoted to a variation.
- **`deliveryManager` contract (12 ops):**

| Op | Replaces |
|---|---|
| `StartProject` | CreateProject, SetResearchInput, SetOperatingModel, StartSystemDesign |
| `ExecuteNextActivity` | (same; timer) |
| `DispatchActivityTask` | RequestArtifactDraft ×2, construction run/re-run |
| `SubmitReviewDecision` | SubmitReviewDecision ×2, SubmitPhaseDecision, SetReviewCommentStatus ×2, RequestSDPCommit, SubmitSDPDecision, AdvancePhase, AdvanceToConstruction |
| `AskQuestions` | ×2 |
| `AcknowledgeStaleBasis` | ×2 |
| `SetProjectRunState` | PauseProject, ResumeProject |
| `OverrideActivity` | (same) |
| `ReplanProject` | RunReplanSweep |
| `SetProjectExecutionPolicy` | SetReviewPolicy, UpdateReviewPolicy |
| `QueryProjectView` | GetProject, ListProjects, GetSessionState ×3, GetPumpStatus, GetDesignHealth, ListEpisodes ×2, GetEpisodeTimeline ×2 — view-parameterized (precedent: `operationsManager.QueryOperatedSystemView`) |
| `QueryActivityView` | activity lifecycle + per-task revisions + review threads + episode for one activity (the Activity Experience's single read) |

  M0 approval is `SubmitReviewDecision` on the projectDesign activity's gate; its plan-materializing side effect lives in the child workflow's gate-passed handler, not in a separate op.

## 5. Server design

### 5.1 Parent: the pump (`{projectId}:pump`)
Walks the network; starts a child for **every** eligible activity (dependencies Done, M0 fence honoured), not one at a time; reacts to child completion by signal rather than blocking on `child.Get`. Singular per project (lands on top of the `pump-singular-per-project` fix). The plan derivation emits activities 1–3 and M0 as a fixed prefix; M0 `dependsOn` projectDesign and fans out to every construction root.

### 5.2 Child: the activity lifecycle (`{projectId}:activity:{activityId}`)
One generic DAG walker replaces `walkPhases`, `coauthorartifact.go` and `coauthorphase2artifact.go`:

```
ready = tasks whose dependsOn are all passed
for each ready task, concurrently:
  dispatch → run agentic job, record attempt + episode, stage artifact
  review   → reviewEngine.ProposeReviews → run agent reviewers → if requiresHuman await decision
             approve  → commit staged artifact, mark passed
             sendBack → record verdict+comments on the thread, re-open `reviews` task as revision n+1
join: a task with several dependsOn waits for all
```
Per-type differences are data (lifecycle) and strategy (command, worker class, artifact codec) — never a branch on "design vs construction". Acceptance: merged workflow code is materially smaller than the three it replaces (baseline ≈ 6,300 twin lines + constructactivity.go).

### 5.3 One staging/review rail (ResourceAccess)
One verb family for every activity: **stage** (write the task's artifact on the activity branch) → **review** (append to the task's review thread) → **commit** (merge on gate pass). Design activities' artifacts are `project.json` slots; construction's are code + `.serviceContracts`/`.phaseArtifacts`; both ride an **activity branch** (`activity/{activityId}`), replacing the design session branch.
- `ReviewThread` (today on `ArtifactSlot`) moves to the **task**: `.activityExecution[activityId].tasks[taskId].revisions[n] = { attemptIds[], episodeRef, stagedRef (commit sha), verdicts[{reviewer, role, verdict, summary}], thread: ReviewComment[], outcome, decidedAt }`. The design slot keeps `Revisions`/`Provenance`; its thread becomes a read-through to the task's.
- `stagedRef` makes "artifact as of revision n" a git read — history costs no new storage.
- `OperatorNote{sendBack}` stops being the only survivor of a construction send-back; `sig.Feedback` is persisted as the verdict + thread.
- `designSessionAccess` (8 ops) and `constructionTransitionAccess` (12) shrink to one facet set; `RecordPhaseStarted/Completed` die with the phase rail (ruling 6 endorsement).

### 5.4 One review engine
`reviewEngine.ProposeReviews(activityType, taskId, artifactKind, policy, floorTouched) → ReviewSet{reviewers[], requiresHuman, reason}`. `ReviewPolicy.EffectiveGate`/`RequiresHuman` and the floor keywords move out of projectStateAccess into the engine; the policy **data** stays in project state. Design's PM critic / architect self-review become ordinary agent reviewers in the set. New non-overridable floor: **`projectDesign` gate always requires a human** (spend approval). Fixes the live defect where `constructactivity.go` passes `phase.String()` (`"detailed_design"`) as `artifactKind` (`"DetailedDesign"` expected) so `ProposeReviews` always errors and rosters are always empty — the error must no longer be swallowed.

## 6. Project Design activity (R7)

On Architecture commit the pump makes projectDesign eligible; its single task has no dispatch. The child computes the plan via `estimationEngine` (DerivePlan → network → the four options → risk → cost in tokens/$), stages it as the SDP artifact, and opens the M0 review. The screen shows: cost & schedule headline, options table (normal preselected/recommended; radio to choose), derived activity list, network. Verbs: **Approve plan & cost — start construction**; comments and questions allowed; **no send-back** — an `Amend Architecture →` link instead. Re-opening Architecture invalidates projectDesign (stale basis → recompute → re-approve). `coauthorphase2artifact.go`, the nine Phase-2 draft commands and the Phase-2 SPA rail are deleted; slots 8–16 remain as computed, committed records (ordinals unchanged).

## 7. webApp design

### 7.1 Components (pure, `src/components/activity/`)
- `lifecycleGraphLayout.ts` / `lifecycleGraphGeometry.ts` — tested pure layout: column = longest-path depth; git-style lanes (trunk on lane 0, branches take the lowest free lane and return to the join's lane).
- `LifecycleGraph.tsx` — hand-rolled SVG, SlimSpine's visual language. Uniform circular pips with **icons**: robot = agent task, artifact/document = review task; state by fill/ring + small corner badge. Rails are **directed** (forward arrowheads). A **return arc** review → agent is drawn only when that pair has been sent back ≥ once, carrying `↻N`. Phase **labels** (name · weight, ✓ when gated) with no bracket bars. Lane labels at forks. Active node = pill with inline title + caret. Click = latest revision; right-click / caret / ContextMenu key / Shift+F10 = revision menu (shortcut to the select).
- `LifecycleGraphMini.tsx` — thumbnail for plan rows and graph tiles.
- `RevisionSelect` — `Revision 3 of 3 · latest ▾`, identical on dispatch and review bodies; the selection carries across a draft→review pair; URL param `rev`.

### 7.2 Activity Experience (route `/project/$projectId/activity/$activityId?task=&rev=`)
`ExperienceChrome` (eyebrow `R-BSA · SERVICE` / `ACTIVITY 2 · ARCHITECTURE`) + `spine = <LifecycleGraph>`; body by task kind:
- **dispatch** → ONE episode (the selected revision's): header facts (worker class, command, exit criterion) + turn timeline; running → generating scene. Sub-attempts listed only when a revision had more than one.
- **review** → reviewers strip (set + one-line reason from the engine) → artifact via the existing renderers (`ArtifactRenderer` kinds, `ServiceContractView`, `FrontendArtifactView`, `TestPlanView`, …) wrapped in `CommentProvider` → `CommentMargin` → `SubmitBar` (`submitVerb.ts` gains `allowSendBack: false` for projectDesign).
- **non-latest revision** → read-only banner + Back to latest; artifact as of that revision; that revision's threads with resolutions expanded; no submit bar.

Default task when none is in the URL: awaiting-human → failed → running → last passed.

### 7.3 Plan (route `/project/$projectId/plan?lens=list|graph|tasks`)
Replaces the construction console's shell and HomeBase's phase cards. LIST: Table 11-1 order, M0 divider, mini graph + state per row. GRAPH: `@xyflow/react`, existing lens conventions (layer gutter, top-down, utilities bar without lines, hover-focus dimming, alarm edges, milestone ribbon) with a new **FRONT END** row (`1 Requirements → 2 Architecture → 3 Project Design → ◆ M0`) and a tile per **activity** whose body is the mini lifecycle. TASKS lens survives as is (it already lists owed decisions) and now includes design reviews. Any row/tile/decision opens the Activity Experience full screen; ✕ returns with viewport and selection preserved (module memory).

### 7.4 Deleted
`construction/detail/DetailPane.tsx`, `FocusView.tsx`, body dispatch ladder; `routes/DesignExperience.tsx`, `containers/SystemDesignContainer.tsx`, `SystemDesignView`'s step ladder, `routes/ProjectDesignExperience.tsx`, `SlimSpine.tsx`, `PHASE1_ORDER`/`PHASE2_ORDER`, `gateOccurrences.ts` (the server now stores the revision). Old routes redirect to the matching activity. Artifact renderers and review aids (contract flow diagrams, UI-mock iframe, STP flows) are kept — inventory before deleting.

### 7.5 Component gaps found by the prototype (fix in stage 5)
1. `CommentableList`'s disabled branch never registers anchors → read-only history cannot place margin cards. Anchor enrolment must not depend on comment affordances.
2. `ContractSignatureList` arms `contractOpAnchor` but never registers it → contract threads are always UNPLACED.
3. `GeneratingScene` role line/footer copy is design-rail specific.
4. `MarginThreadCard` collapses resolved threads → add an `expandResolved` mode for history.

## 8. Stages

Each stage = its own plan + subagent-driven run in a worktree; drain-and-cutover, not `GetVersion`, where task queues change owner.

**Merge order (green at every merge, per the 2026-09-20 gate recon): 0 → 2 → 1 → 3 → 4 → 6, with 5 running in parallel from 0 and merging after 3.** Why not model-first: ALIGN-MISSING-PKG / ALIGN-EXTRA-PKG / CC-* (Error severity) tie slot 5 to the Go packages, so the model wave must keep the three old manager components and add `delivery-manager` as `buildStatus: "planned"` with contracts carrying no `goPackage`; the component deletion ships only with stage 4.

Must-ship-together sets: **stage 2** = method-assets release + pin bump + both generated tables + `derived-plan-write`; **stage 1** = model + every use-case realization (a half-authored dynamic view is red); **stage 4** = new manager package + old slot-5 component deletion + `cmd/clientgen/main.go` and `cmd/appgen/main.go` exposed-manager lists + `arch_test.go` allowlists + `registered_names_test.go` golden + drain.

Self-amendment procedure (precedent `2026-07-31-callchain-rollout.md`): hand-edit `.aiarch/state/project.json` → `make gen-models` → `make method-check` → `GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System` → `GOWORK=off make test-short` → `cd webApp && npm run check`. State slots 9/10 are never hand-edited — `make derived-plan-write`.

Pre-req: land or abandon the unmerged `lifecycle-2-conformance-gate` branch (touches manager/construction) before stage 3.

| Stage | Content | Ships |
|---|---|---|
| **0 Read contract** | Add read-only `QueryActivityView` to the **existing** `constructionManager` (12→13 ops: a design-health Warning only, no ALIGN/CC exposure, no drain) returning lifecycle + tasks + revisions (derived from today's `Attempts`/`OperatorNotes`/episodes) for one activity; preview fixtures validated by `fixture-schema.mjs`. Same PR: fix the `phase.String()` → `artifactKind` defect and stop swallowing the `ProposeReviews` error. | a schema-stable read for the webApp |
| **1 Model** | `project.json` self-amendment (§4): volatility merge, `delivery-manager`, 3 core use cases + re-parented variations, dynamic views, `deliveryManager`/`reviewEngine` contracts; regen (modelgen, OAS, ops.gen). Old managers remain as the implementation until stage 4 behind the new contract names where generation requires. | model + generated contracts |
| **2 Lifecycles + review engine** | `lifecycles.json` in method-assets (platform release), loader + validation, `lifecycles.gen.ts`; remove `profileRows` family from projectStateAccess; `EffectiveGate` → reviewEngine, generalized signature, artifactKind bug fix, M0 human floor; plan derivation emits activities 1–3 + M0 prefix (`ActivityType` requirements/architecture/projectDesign appended, never renumbered). | engine + data, no behaviour change on the rails yet |
| **3 Unified rail** | task-level revisions/threads/verdicts in project state; activity branch; stage/review/commit verb family; construction send-back persistence; `QueryActivityView` read. | construction review history becomes real |
| **4 DeliveryManager** | generic DAG child + parallel pump; design activities run as children; deterministic Project Design (§6); delete the three managers, co-author twins, Phase-2 commands; engines 7→4. Drain → cutover. | one manager |
| **5 webApp** | §7. Starts after stage 1 against preview fixtures validated by the OAS schema; lands after 3–4. Playwright per UI change, founder review per change. | one experience |
| **6 Migration + deploy** | map existing slots/threads/`activityConstruction` onto `.activityExecution`; backfill activities 1–3 as Done for sealed projects; drain `*:nextActivity:*` and design sessions; release. | production |

## 9. Testing

- Pure layout/geometry: node tests (20 exist). Lifecycle data: schema + DAG validation in Go and in the generator.
- Child workflow: Temporal test-suite cases per shape — linear, fork/join (both branch orders), send-back re-opens only the judged pair, join waits for all, M0 no-send-back, human-floor under `vibes`.
- Review engine: table tests over (activityType, task, policy, floor); a Manager fake that **validates** `artifactKind` (the current fake hid the live bug).
- Rail: revision read returns artifact-as-of-`stagedRef`; construction send-back round-trips comments.
- webApp: fixtures per scenario in the preview shell; Playwright interaction suite from the prototype (`interact.mjs`) promoted into `uitests`; `npm run check` green.
- Acceptance: a fresh project runs Requirements → Architecture → M0 approval → parallel construction children from the Plan screen alone; line count of delivery manager < sum of predecessors.

## 10. Risks

- **God-manager** — 12 ops is the App. B ceiling; guard with the smaller-than-sum acceptance test and keep per-type behaviour in data/strategy.
- **Self-amendment blast radius** — model, realizations, derived plan, service contracts and code must move together; stage 1 is its own wave.
- **Parallel children** raise concurrent writes to `project.json` — the rail's commit step must serialize merges per project (activity branches merge through one queue).
- **In-flight workflows** — drain before stages 4 and 6; task-queue ownership changes.
- **Amendment UX** — re-opening an upstream activity invalidates downstream (unifies StaleBasis with replan); covered minimally here (Architecture → projectDesign recompute); the general case is a follow-up spec.
