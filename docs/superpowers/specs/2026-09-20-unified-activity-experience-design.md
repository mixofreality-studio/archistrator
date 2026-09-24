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

- **Volatility:** B-02, B-03, B-04, B-13, B-14 merge into one **Project Delivery Workflow** ("how a project's activities are scheduled and how one activity's task DAG advances"). `designHealthEngine`'s rules stop being a facet of the system-design volatility: as shipped in stage 1 they are the stand-alone **Design Conformance Rules** volatility with `design-health-engine` as sole owner (the 08-30 ruling's dissolution into methodcheck is stage 4's, when the engine dies). The three Managers' shared encapsulation of Project Delivery Workflow is a TRANSITIONAL facet group that expires with stage 4 — an owed waiver sentence on the slot-3 §2h justification (earmark file).
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

### 5.3 One staging/review rail + execution data model (system-architect ruling, 2026-09-21)

The mega `project.json` and slots 0–16 stay. One verb family for every activity: **stage** (write the task's output on the activity branch `activity/{activityId}`, replacing the design session branch) → **review** → **commit** (merge on gate pass). Design activities' artifacts are slot models; construction's are code + `.serviceContracts`/`.phaseArtifacts`.

**Two append-only ledgers per activity; everything else derived.** A revision is a derived grouping, never a stored container (a nested `tasks[].revisions[]` tree is read-modify-write under Temporal retry — the reason the attempt ledger exists). The attempts ledger alone cannot hold a review, because comments are replied to and resolved after the attempt ends.

```
.activityExecution[activityId] = ActivityExecution{
  ActivityID, Type, Variant, LifecyclePin{id, version},
  StartedAt, CompletedAt, FailureReason, FailureDetail,      // sticky head facts
  Attempts []TaskAttempt,                                    // APPEND-ONLY — written by the workflow (today only cmd/backfill-attempts writes it)
  Reviews  []ReviewRound,                                    // APPEND-ONLY container
  Produced []ProducedArtifact, OperatorNotes []OperatorNote, // notes narrowed to override/retry/takeover/requeue/skip
  Version int64 }                                            // optimistic concurrency
TaskAttempt{ AttemptID "<activityId>:<taskId>:<n>", TaskID, Revision, Attempt, Actor,
  StartedAt, EndedAt, Outcome, StagedRef, Evidence, Provenance }
ReviewRound{ RoundID "<activityId>:<reviewTaskId>:<n>", TaskID, Reviews (judged task), Round,
  SubjectRef{kind, ref = stagedRef sha}, Reviewers[]{role, actor, required},
  Verdicts[]{reviewerRole, actor, verdict approve|sendBack|abstain, summary, at, attemptId},
  Thread []ReviewComment,          // EXISTING type unchanged (replies, status, type, addressee)
  Outcome passed|sentBack|pending, DecidedAt, DecidedBy, Provenance }
```

- Agent and human verdicts are rows in `Verdicts`; Ask/questions are `ReviewComment.type = question` — no third mechanism. Artifact-as-of-revision = `SubjectRef.ref` (a git read; no new storage). The only in-place mutations are comment `status`/`replies` (existing normalizer `ApplyReviewBatch`).
- **Derived, never stored:** revisions, task state, `Phases`/`PhaseCompletion`, `CurrentPhase`, `BuildStatus`, coarse `Phase`, earned value, `constructionProgress`.
- **Key rename** `.activityConstruction` → `.activityExecution` in **stage 3**, with the shape change (one wire break). ActivityID keys and the AttemptID format are kept so the episode ledger's `TargetRef` join survives.
- **Dispositions.** `ArtifactSlot.ReviewThread`: read-through to `ReviewRound` in stage 3 (dual-write during the wave), deleted in stage 6. `CritiqueVerdict`/`CritiqueNotes`: deleted in stage 3 — an ordinary verdict with `role: projectManager`. `ArtifactSlot.Revisions`: kept, deprecated in place, stamped at commit, with a drift test equal to the derived round count. `NoteSendBack` stops being written in stage 3 (pending feedback = the latest round's open comments). `Phases`/`CurrentPhase`/`BuildStatus`/`Phase`/`Kind`: no longer stored from stage 3, emitted as computed view fields through stage 5, stored fields deleted in stage 6. Root `phase`: frozen, derived from milestone position, never renumbered or deleted.
- **ResourceAccess (AMENDED 2026-09-23, stage-3 planning).** `gitActivityStatusAccess`, `constructionTransitionAccess` and `designSessionAccess` are not components — they are CONTRACT FACETS of the one `project-state-access` component (ratified facet doctrine, 2026-07-17). Stage 3 therefore added **`activityExecutionAccess`** as the FIFTH FACET of that component (ruling (A): green and true; a new component would fire SYS-RA-ORPHAN / DV-REL-COVERAGE, both Errors), owner of both ledgers, ADDITIVELY: the three folded facets stay DEPRECATED IN PLACE (no op deleted or renamed) so every Temporal activity name survives replay; the construction workflow switched to the new verbs behind a `GetVersion` fence; the old facets are deleted after the stage-4 drain. `sourceControlAccess` and `episodeAccess` stay separate (ruling 6). Twelve atomic verbs: `OpenActivity`, `StageTaskOutput`, `RecordAttemptOutcome`, `OpenReviewRound`, `AppendReviewVerdict` (verdict + its comments in one commit), `SetReviewCommentStatus`, `DecideReviewRound`, `CommitActivityArtifacts`, `RecordActivityOutcome`, `RecordOperatorNote`, `AcknowledgeStaleBasis`, `ReadActivityExecution`. `projectStateAccess` keeps slots/plan/policy (it has 9 ops, none dead — the 2026-07-20 "12 dead ops D1–D4" finding was about duplicate call-chain EDGES in the dynamic views, pruned in stage 3); view derivation stays in the Manager for now (stage-0 F3: no production Engine may import projectstate) — an Engine home is a stage-4 item.
- **Concurrency (AMENDED 2026-09-23).** `.activityExecution[activityId]` carries a per-activity `Version` (stamped at the single write-back point; the CAS is written and tested but NOT YET ARMED — the verbs return the project version; threading the activity version is a stage-4 contract change and a stage-4 ENTRY CRITERION, needed when parallel children exist). The `{projectId}:statewrite` lane is NOT built: `applyMutationOnBranchFiles` already does dedup-first + project version guard + git ref-CAS, `Conflict` is retryable, and the workflow's `applyRecovering` is the bounded re-read→re-apply loop; a second per-project singleton is the shape `pump-singular-per-project` had to remove. Deterministic ids (AttemptID/RoundID/NoteID) make every append idempotent under retry; design rounds use a 4-part `RoundID <activity>:<gate>:<kind>:<n>` because kinds share a lifecycle phase, and nothing may parse a RoundID — joins use `(TaskID, Round)`.
- **Migration (RAN IN STAGE 3, `cmd/migrate-activity-execution`; the shape changed there, so the state moved there).** Rename the map; carry identity, timestamps, failure facts, Produced, Attempts, OperatorNotes verbatim with each row's `Provenance.Origin` preserved (backfilled stays backfilled); drop the derived fields; stamp `Version = 1` + the current `LifecyclePin`; convert each sealed slot's `ReviewThread` into one backfilled `ReviewRound` per distinct `round`; `CritiqueVerdict/Notes` → one backfilled projectManager verdict; `NoteSendBack` notes → a round whose verdict carries the note's comments; synthesize activities 1–3 + M0 as Done for sealed projects. Acceptance: derived `Phases` equal the pre-migration stored `Phases` for every row.

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
Replaces the construction console's shell and HomeBase's phase cards. LIST: Table 11-1 order, M0 divider, mini graph + state per row. GRAPH: `@xyflow/react`, a tile per **activity** whose body is the mini lifecycle. It is a **project network, not an architecture diagram**, so rows read top→bottom in Table 11-1 **build order** — the architecture flipped: **FRONT END** (`1 Requirements → 2 Architecture → 3 Project Design → ◆ M0`) → RESOURCES → RESOURCE ACCESS → ENGINES → MANAGERS → CLIENTS → SYSTEM TESTING. Edges are dependency edges pointing down (predecessor → dependent); M0 fans out to every root; N-STP runs in a side lane from M0 to N-IT. No utilities bar (utilities are platform-provided, no activities). Kept from the existing lens: row gutter, hover-focus (lights the full upstream + downstream chain), critical-path weight, milestone ribbon. The LIST lens uses the same order. TASKS lens survives as is (it already lists owed decisions) and now includes design reviews. Any row/tile/decision opens the Activity Experience full screen; ✕ returns with viewport and selection preserved (module memory).

### 7.4 Deleted
`construction/detail/DetailPane.tsx`, `FocusView.tsx`, body dispatch ladder; `routes/DesignExperience.tsx`, `containers/SystemDesignContainer.tsx`, `SystemDesignView`'s step ladder, `routes/ProjectDesignExperience.tsx`, `SlimSpine.tsx`, `PHASE1_ORDER`/`PHASE2_ORDER`, `gateOccurrences.ts` (the server now stores the revision). Old routes redirect to the matching activity. Artifact renderers and review aids (contract flow diagrams, UI-mock iframe, STP flows) are kept — inventory before deleting.

### 7.5 Component gaps found by the prototype (fix in stage 5)
1. `CommentableList`'s disabled branch never registers anchors → read-only history cannot place margin cards. Anchor enrolment must not depend on comment affordances.
2. `ContractSignatureList` arms `contractOpAnchor` but never registers it → contract threads are always UNPLACED.
3. `GeneratingScene` role line/footer copy is design-rail specific.
4. `MarginThreadCard` collapses resolved threads → add an `expandResolved` mode for history.

## 8. Stages

Each stage = its own plan + subagent-driven run in a worktree; drain-and-cutover, not `GetVersion`, where task queues change owner.

**Merge order (green at every merge, per the 2026-09-20 gate recon): 0 → 2 → 1 → 3 → 4 → 6, with 5 running in parallel from 0 and merging after 3.** Stage 3 entry criteria (from the stage-0 final review): the N4 reconstruction in `normalizeAttempts` appends a rejected gate attempt per send-back note with no dedup against ledger rejections, and phase completion is re-derived from gate state while `ResolveConstructionRow`'s resolved completions are discarded — both inert today and both corrupt revision history the moment stage 3 persists gate attempts; they are blocking work items of stage 3, not earmarks. Why not model-first (AMENDED 2026-09-23, stage-1 planning, reproduced against the live state by `jq` into a temp root + `go run ./cmd/aiarch-state-mcp validate --root <tmp> --slot System`): the earlier recon named ALIGN-MISSING-PKG / ALIGN-EXTRA-PKG / CC-* but missed three more Errors that close every door on a model-first wave — **SYS-CARD-MGR** ("system has 6 Managers; The Method limits a system to 5", no `buildStatus` exemption and no waiver path for an Error), **DV-SINGLE-MGR** (a view's Client edges may enter one Manager; the unified core use case enters the three old rails) and **DH-CONTRACT-FACET** (a `deliveryManager` contract no component owns is a fossil). With ALIGN-EXTRA-PKG forcing the three old components to stay while their packages exist, `delivery-manager` + its contract + its relationships + the three-into-one core use case + every re-keyed dynamic view + the deletion of the three Manager components and contracts are ONE indivisible commit that is green only alongside the new Go package — i.e. stage 4's first commit. Stage 1 therefore carries only what is true today: the volatility merge and the planned-component derivation rule. The self-amendment loop must run `validate --slot <every slot edited>` as well as `--slot System` (System DOWNGRADES other slots' Errors — VOL-GLOSS was caught only by `--slot Volatilities`).

Must-ship-together sets: **stage 2** = method-assets release + pin bump + both generated tables + `derived-plan-write`; **stage 1** = the volatility merge + its component re-points in one commit (deleting a volatility without re-pointing its components is red); **stage 4 (first commit)** = `delivery-manager` component + `deliveryManager` contract (12 ops) + relationships + the `execute-a-project-activity` core use case with every re-keyed dynamic view + re-parented variations + deletion of the three Manager components/contracts + the new Go package + old slot-5 component deletion + `cmd/clientgen/main.go` and `cmd/appgen/main.go` exposed-manager lists + `arch_test.go` allowlists + `registered_names_test.go` golden + drain.

Self-amendment procedure (precedent `2026-07-31-callchain-rollout.md`): hand-edit `.aiarch/state/project.json` → `make gen-models` → `make method-check` → `GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System` AND `--slot <every slot edited>` (System downgrades other slots' Errors) → `GOWORK=off make test-short` → `cd webApp && npm run check`. State slots 9/10 are never hand-edited — `make derived-plan-write`. Never two implementers on `project.json` concurrently, even in disjoint regions.

Pre-req: land or abandon the unmerged `lifecycle-2-conformance-gate` branch (touches manager/construction) before stage 3.

| Stage | Content | Ships |
|---|---|---|
| **0 Read contract** | Add read-only `QueryActivityView` to the **existing** `constructionManager` (12→13 ops: a design-health Warning only, no ALIGN/CC exposure, no drain) returning lifecycle + tasks + revisions (derived from today's `Attempts`/`OperatorNotes`/episodes) for one activity; preview fixtures validated by `fixture-schema.mjs`. Same PR: fix the `phase.String()` → `artifactKind` defect and stop swallowing the `ProposeReviews` error. | a schema-stable read for the webApp |
| **1 Model (reduced)** | `project.json` self-amendment: B-02/B-03/B-04/B-13/B-14 merge into one **Project Delivery Workflow** volatility (all three Managers encapsulate it); `designHealthEngine`'s rules split out as the stand-alone **Design Conformance Rules** volatility; the estimation engine's rule that a `buildStatus: "planned"` component derives NO construction activity (pinned — the production pump sweep would otherwise try to build it). `reviewEngine` was already generalized in stage 2. Only the estimation contract regenerates (`engine/estimation/contract.gen.go`, `projectstate/toolcatalog.gen.go`). | model, 19→18 volatilities; pump-safe `planned` posture |
| **2 Lifecycles + review engine** | *(stage 0 already shipped `lifecycles.json` in method-assets v0.9.0, the server pin + parity test, `gen-lifecycles` and `lifecycles.gen.ts`, and the artifactKind fix — all additive.)* Stage 2 REMOVES the `profileRows`/`phaseTasks`/`gateTasks` family from projectStateAccess and re-points every reader (`ProfileFor`, `CommandFor`, `gen-uiprofiles`) at the method-assets data, retiring the stage-0 parity test and the test-only `lifecycleTypeKey` copy; `EffectiveGate` → reviewEngine, generalized signature taking the activity type (dissolving the Manager-side kind table), M0 human floor; plan derivation emits activities 1–3 + M0 prefix (`ActivityType` requirements/architecture/projectDesign appended, never renumbered). | engine + data, no behaviour change on the rails yet |
| **3 Unified rail** | task-level revisions/threads/verdicts in project state; activity branch; stage/review/commit verb family; construction send-back persistence; `QueryActivityView` read. | construction review history becomes real |
| **4 DeliveryManager** | FIRST COMMIT = the model wave stage 1 could not carry: `delivery-manager` (`deliveryManager`, 12 ops per §4, `ActivityView` reused) + relationships + `execute-a-project-activity` core use case (timer + clientAction entries as step-local `alt` groups) with every dynamic view + 11 variations re-parented + `commit-to-a-project-option` demoted + deletion of the three Manager components and contracts, together with the new Go package, `cmd/clientgen`/`cmd/appgen` exposed-manager lists, `arch_test.go` allowlists, `registered_names` golden, `engine_test.go` `realizedViews`; stage-4 entry criteria: a `buildStatus` enum/vocabulary rule (a typo re-derives a planned component). THEN: generic DAG child + parallel pump; design activities run as children; deterministic Project Design (§6); co-author twins and Phase-2 commands deleted; `engineReviewPolicy` triplication + inline `NewReviewEngine()` collapsed; engines 7→4. Drain → cutover. | one manager |
| **5 webApp** | §7. Starts after stage 1 against preview fixtures validated by the OAS schema; lands after 3–4. Playwright per UI change, founder review per change. | one experience |
| **6 Cleanup + deploy** | (the `.activityExecution` migration and the activities-1–3 backfill already ran in stages 3 and 2) delete `ArtifactSlot.ReviewThread`/`CritiqueVerdict`/`CritiqueNotes`, the legacy `activityConstruction` read tolerance, the stored derived fields and the deprecated facets after the stage-4 drain; narrow `PendingOperatorNotes`; drain `*:nextActivity:*` and design sessions; release. | production |

## 9. Testing

- Pure layout/geometry: node tests (20 exist). Lifecycle data: schema + DAG validation in Go and in the generator.
- Child workflow: Temporal test-suite cases per shape — linear, fork/join (both branch orders), send-back re-opens only the judged pair, join waits for all, M0 no-send-back, human-floor under `vibes`.
- Review engine: table tests over (activityType, task, policy, floor); a Manager fake that **validates** `artifactKind` (the current fake hid the live bug).
- Rail: revision read returns artifact-as-of-`stagedRef`; construction send-back round-trips comments.
- webApp: fixtures per scenario in the preview shell; Playwright interaction suite from the prototype (`interact.mjs`) promoted into `uitests`; `npm run check` green.
- Acceptance: a fresh project runs Requirements → Architecture → M0 approval → parallel construction children from the Plan screen alone; line count of delivery manager < sum of predecessors.

## 10. Risks

- **God-manager** — 12 ops is the App. B ceiling; guard with the smaller-than-sum acceptance test and keep per-type behaviour in data/strategy.
- **Self-amendment blast radius** — model, realizations, derived plan, service contracts and code must move together — and stage-1 planning proved it is stronger than "own wave": the Method cardinality and single-Manager view rules make the Manager collapse one indivisible commit with its code (stage 4). `activityExecutionAccess` (§5.3) likewise has no green-and-true `planned` posture (SYS-RA-ORPHAN without relationships, DV-REL-COVERAGE with them) and is modelled in stage 3 with its code.
- **Parallel children** raise concurrent writes to `project.json` — the rail's commit step must serialize merges per project (activity branches merge through one queue).
- **In-flight workflows** — drain before stages 4 and 6; task-queue ownership changes.
- **Amendment UX** — re-opening an upstream activity invalidates downstream (unifies StaleBasis with replan); covered minimally here (Architecture → projectDesign recompute); the general case is a follow-up spec.
