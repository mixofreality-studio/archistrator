# Unified Activity Experience & Delivery Manager — Design

**Date:** 2026-09-20 · **Status:** founder-approved direction + UX prototype; spec for review
**Prototype:** REMOVED 2026-09-24 with stage 5. The reusable half (`webApp/src/components/activity/`) was committed in stage-5 Task 1 and is the shipped code; the throwaway harness (`webApp/proto.html`, `webApp/proto/` — `main.tsx`, `ProtoApp.tsx`, `useHashRoute.ts`, `fixtures.ts` and the four mock artifact modules) was deleted in Task 14. It was never tracked in git on any branch, including `activity-experience-proto`, so there is nothing to check out: the fixtures that replaced it live in `uitests/preview-fixtures/web-client/`.

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

**AMENDED 2026-09-25 (stage 4a, SHIPPED). Four of the twelve could not reach the behaviours the table above assigns them with the signature the table implies.** The landed contract (`.serviceContracts.deliveryManager`):

| Op | Landed signature, and the clause that forced it |
|---|---|
| `StartProject` | `(owner OwnerScope, name string, projectID *string, model *OperatingModel, research *ResearchInput, start bool) → StartProjectResult`. `projectID` is **optional and carried in the BODY** — absent means create, present means adopt — because the four ops it replaces are incremental: the SPA sets research and the operating model *after* creation, not at it. The route is `POST /api/v1/delivery/start-project` with **no path segment**; the first landing's `…/start-project/{projectID}` made create unreachable over REST, since Go's mux will not match an empty segment. |
| `SubmitReviewDecision` | `(projectID, activityID, taskID, decision ReviewDecisionInput, feedback *ReviewFeedback)`. The verdict is an OBJECT, not a bare `ReviewDecision`: the nine writers it replaces need `acknowledgeStale`, `optionId`, `commentId` and `commentStatus` (`ReviewDecisionInput`, ordinals 4/5 appended, never renumbered). |
| `AskQuestions` | `(projectID, activityID, taskID, addressee string, questions []AnchoredComment)`. Questions are **anchored comments** (`jsonPath`, `text`, `anchorText`, `replyTo`), not strings — §5.3 makes an Ask a `ReviewComment.type = question`, and a string cannot carry an anchor. |
| `QueryProjectView` | `(query ProjectViewQuery) → ProjectView`. ONE object parameter, because the thirteen readers it replaces share no parameter list: `ProjectViewQuery{kind, owner?, projectId?, activityId?, artifactKind?, episodeId?}` over **seven** `ProjectViewKind` selectors — `summary, projects, session, pump, designHealth, episodes, timeline`. A missing required selector is `ContractMisuse` naming the selector. `ProjectView` carries three session members (`session`, `projectSession`, `constructionSession`) under the one `session` kind; they differ only by an optional `critique?` and by stage ordinals that diverge at `AssemblingSDP`, so the wrong one typechecks — never prefix-strip a session view. `SessionStage` and `ProjectSessionStage` both survive as separate enums. |

Unchanged from the table: the remaining eight signatures, and `ReplanProject`'s `projectID` is still `*ProjectID` where nil means "sweep every active project" — an arm that is **unreachable over both transports** and driven only by the Temporal Schedule (earmarked).

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
Per-type differences are data (lifecycle) and strategy (command, worker class, artifact codec) — never a branch on "design vs construction". Acceptance: merged workflow code is materially smaller than the three it replaces.

**AMENDED 2026-09-25 (stage 4a). The baseline restated, and when it is measured.** "≈ 6,300 twin lines + `constructactivity.go`" was the 2026-08-30 measurement and is stale by ~1,700 lines. Measured at the stage-4 branch base `4baed01a`, across `internal/manager/{systemdesign,projectdesign,construction}`: the co-author twins are **4,997 + 3,027 = 8,024** lines; hand-written non-generated code across the three packages is **25,643**; non-test total **29,778**; tests **31,798**. That is the baseline the acceptance compares against.

**4a is line-neutral by design** — it moves the three packages into `internal/manager/delivery` and makes the twelve ops a thin dispatcher over the forty implementations that already existed; it deletes no behaviour. §9's "line count of delivery manager < sum of predecessors" is therefore measured at the **END of 4b**, when the generic DAG child replaces the twelve dispatcher bodies and the twins are deleted. Measuring it at 4a would fail the acceptance for doing exactly what 4a was scoped to do.

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
TaskAttempt{ AttemptID "<activityId>:<taskId>:<n>", TaskID, Phase, Attempt, Actor,
  StartedAt, EndedAt, Outcome, Evidence, Provenance }
ReviewRound{ RoundID "<activityId>:<reviewTaskId>:<n>", TaskID, Reviews (judged task), Round,
  SubjectRef{kind, ref = stagedRef sha}, Reviewers[]{role, actor, required},
  Verdicts[]{reviewerRole, actor, verdict approve|sendBack|abstain, summary, at, attemptId},
  Thread []ReviewComment,          // EXISTING type unchanged (replies, status, type, addressee)
  Outcome passed|sentBack|pending, DecidedAt, DecidedBy, Provenance }
```

`TaskAttempt` carries no `StagedRef` (deliberate collapse, stage 3): the staged ref is the RETURN VALUE of `StageTaskOutput`, and the attempt cites what it produced through `Evidence` — a second field naming the same thing would be a copy nothing keeps in step. `Revision` is gone from the member list for the reason the next bullet gives (revisions are derived, never stored); `Phase` is denormalized from `PhaseForTask(Task)` and is what the shipped struct actually holds.

- Agent and human verdicts are rows in `Verdicts`; Ask/questions are `ReviewComment.type = question` — no third mechanism. Artifact-as-of-revision = `SubjectRef.ref` (a git read; no new storage). The only in-place mutations are comment `status`/`replies` (existing normalizer `ApplyReviewBatch`).
- **Derived, never stored:** revisions, task state, `Phases`/`PhaseCompletion`, `CurrentPhase`, `BuildStatus`, coarse `Phase`, earned value, `constructionProgress`.
- **Key rename** `.activityConstruction` → `.activityExecution` in **stage 3**, with the shape change (one wire break). ActivityID keys and the AttemptID format are kept so the episode ledger's `TargetRef` join survives.
- **Dispositions.** `ArtifactSlot.ReviewThread`: read-through to `ReviewRound` in stage 3 (dual-write during the wave), deleted in stage 6. `CritiqueVerdict`/`CritiqueNotes`: deleted in **stage 6** (the SPA's critique panel still reads them); from stage 3 an ordinary verdict with `role: projectManager` is ALSO written, so the round ledger carries the critic's conclusion during the dual-write window. Matches §8's stage-6 row and what the branch shipped. `ArtifactSlot.Revisions`: kept, deprecated in place, stamped at commit, with a drift test equal to the derived round count. `NoteSendBack` stops being written in stage 3 (pending feedback = the latest round's open comments). `Phases`/`CurrentPhase`/`BuildStatus`/`Phase`/`Kind`: no longer stored from stage 3, emitted as computed view fields through stage 5, stored fields deleted in stage 6. Root `phase`: frozen, derived from milestone position, never renumbered or deleted.
- **ResourceAccess (AMENDED 2026-09-23, stage-3 planning).** `gitActivityStatusAccess`, `constructionTransitionAccess` and `designSessionAccess` are not components — they are CONTRACT FACETS of the one `project-state-access` component (ratified facet doctrine, 2026-07-17). Stage 3 therefore added **`activityExecutionAccess`** as the FIFTH FACET of that component (ruling (A): green and true; a new component would fire SYS-RA-ORPHAN / DV-REL-COVERAGE, both Errors), owner of both ledgers, ADDITIVELY: the three folded facets stay DEPRECATED IN PLACE (no op deleted or renamed) so every Temporal activity name survives replay; the construction workflow switched to the new verbs behind a `GetVersion` fence; the old facets are deleted after the stage-4 drain. `sourceControlAccess` and `episodeAccess` stay separate (ruling 6). Twelve atomic verbs: `OpenActivity`, `StageTaskOutput`, `RecordAttemptOutcome`, `OpenReviewRound`, `AppendReviewVerdict` (verdict + its comments in one commit), `SetReviewCommentStatus`, `DecideReviewRound`, `CommitActivityArtifacts`, `RecordActivityOutcome`, `RecordOperatorNote`, `AcknowledgeStaleBasis`, `ReadActivityExecution`. `projectStateAccess` keeps slots/plan/policy (it has 9 ops, none dead — the 2026-07-20 "12 dead ops D1–D4" finding was about duplicate call-chain EDGES in the dynamic views, pruned in stage 3); view derivation stays in the Manager for now (stage-0 F3: no production Engine may import projectstate) — an Engine home is a stage-4 item.
- **Concurrency (AMENDED 2026-09-23).** `.activityExecution[activityId]` carries a per-activity `Version` (stamped at the single write-back point; the CAS is written and tested but NOT YET ARMED — the verbs return the project version; threading the activity version is a stage-4 contract change and a stage-4 ENTRY CRITERION, needed when parallel children exist). The `{projectId}:statewrite` lane is NOT built: `applyMutationOnBranchFiles` already does dedup-first + project version guard + git ref-CAS, `Conflict` is retryable, and the workflow's `applyRecovering` is the bounded re-read→re-apply loop; a second per-project singleton is the shape `pump-singular-per-project` had to remove. Deterministic ids (AttemptID/RoundID/NoteID) make every append idempotent under retry; design rounds use a 4-part `RoundID <activity>:<gate>:<kind>:<n>` because kinds share a lifecycle phase, and nothing may parse a RoundID — joins use `(TaskID, Round)`.
- **Migration (RAN IN STAGE 3, `cmd/migrate-activity-execution`; the shape changed there, so the state moved there).** Rename the map; carry identity, timestamps, failure facts, Produced, Attempts, OperatorNotes verbatim with each row's `Provenance.Origin` preserved (backfilled stays backfilled); drop the derived fields; stamp `Version = 1` + the current `LifecyclePin`; convert each sealed slot's `ReviewThread` into one backfilled `ReviewRound` per distinct `round`; `CritiqueVerdict/Notes` → one backfilled projectManager verdict; `NoteSendBack` notes → a round whose verdict carries the note's comments; synthesize activities 1–3 + M0 as Done for sealed projects. Acceptance: derived `Phases` equal the pre-migration stored `Phases` for every row.

### 5.4 One review engine
`reviewEngine.ProposeReviews(activityType, taskId, artifactKind, policy, floorTouched) → ReviewSet{reviewers[], requiresHuman, reason}`. `ReviewPolicy.EffectiveGate`/`RequiresHuman` and the floor keywords move out of projectStateAccess into the engine; the policy **data** stays in project state. Design's PM critic / architect self-review become ordinary agent reviewers in the set. New non-overridable floor: **`projectDesign` gate always requires a human** (spend approval). Fixes the live defect where `constructactivity.go` passes `phase.String()` (`"detailed_design"`) as `artifactKind` (`"DetailedDesign"` expected) so `ProposeReviews` always errors and rosters are always empty — the error must no longer be swallowed.

### 5.5 Authorization of body-carried project ids (ruling, 2026-09-25)

Moving `StartProject`'s `projectID` off the path and into the body (§4) moved its generated authorization with it. `httpgen`'s convention is that a **path** id param yields `security.ResourceRef{Kind: "project", ID: <the id>}` and **no** path id yields `{Kind: "<manager>Catalog", ID: principal.Subject}` — so the adopt arm was presenting the authorizer with a ref that did not name the project being adopted. Not exploitable under the interim `authenticatedOnlyPDP`, but it destroys the resource ref a real (Cedar) PDP would decide on, which is the whole reason `cmd/server/authz.go` keeps the seam wired.

**The ruling: the generator is NOT taught to read a body id.** A path id is the convention's identity signal; reaching into a request wrapper for an optional id would make every body-carried id a silent authorization surface. A body-carried project id is authorized instead by a **project-scoped Manager decorator at the composition root** (`cmd/server/hooks.go`, installed by `WrapManagers`), which re-asks the same question with the same verb against `{Kind: "project", ID: <body id>}` and answers `Unauthorized` (403) on deny or on an unreachable engine. `projectScopedDeliveryManager` is the instance; `projectScopedOperationsManager` is its precedent, and the guard sits at the composition root rather than inside the Manager façade because the derivation does — the Manager holds no `security.Security` dep, and one built inside it would get framework-go's deny-by-default `defaultPolicyDecisionPoint`. Because `WrapManagers` hands the same instance to the REST handlers and the MCP tools, the guard covers both transports; the MCP surface authorized nothing of its own before.

**This is the convention for any future body-carried id**, and it is a must-ship-together member (§8): a guard the composition root forgets is not a guard, so the wiring is pinned by its own test. Both arms landed in 4a — `StartProject`'s adopt branch and `QueryProjectView`'s `projectId`, the one read of every project over all six project-addressed kinds. Two cases are deliberately NOT guarded: `QueryProjectView{kind:"projects"}` carries an owner and no project id — it IS the catalog listing, so the catalog decision is exactly the question it asks — and an empty non-nil id falls through to the Manager's `ContractMisuse`, because denying it would turn a malformed query into a permissions error.

**Audited across all twelve ops at 4a:** the generated handlers hold exactly two catalog-scoped refs (`start-project`, `query-project-view`) and ten project-scoped ones from path params, and **no other op names a project in its body** — the only inbound body-side `projectId` members in the contract are `startProjectRequest`'s and `ProjectViewQuery`'s. Re-run that audit whenever an id leaves a path.

## 6. Project Design activity (R7)

On Architecture commit the pump makes projectDesign eligible; its single task has no dispatch. The child computes the plan via `estimationEngine` (DerivePlan → network → the four options → risk → cost in tokens/$), stages it as the SDP artifact, and opens the M0 review. The screen shows: cost & schedule headline, options table (normal preselected/recommended; radio to choose), derived activity list, network. Verbs: **Approve plan & cost — start construction**; comments and questions allowed; **no send-back** — an `Amend Architecture →` link instead. Re-opening Architecture invalidates projectDesign (stale basis → recompute → re-approve). `coauthorphase2artifact.go`, the **eight dispatchable Phase-2 draft commands plus the server-side SDP assembly**, and the Phase-2 SPA rail are deleted; slots 8–16 remain as computed, committed records (ordinals unchanged).

**CORRECTED 2026-09-25 (stage 4a).** "Nine draft commands" counts nine Phase-2 kinds but only eight commands. `DesignCommandFor` returns `""` for `KindSdpReview` in draft mode — "assembled server-side" — so the dispatchable eight are `planning-assumptions-draft`, `activity-list-draft`, `network-draft`, `normal-solution-draft`, `subcritical-solution-draft`, `compressed-solution-draft`, `decompressed-solution-draft`, `risk-model-draft`. Pinned by the **undispatchable-combinations** block of `projectstate/access_test.go` (`{"sdpReview draft undispatchable (assembled server-side)", KindSdpReview, DesignJobModeDraft, "", ""}` at `:7602-7603`), not by the Phase-1/Phase-2 draft table at `:7578-7586`. The ninth thing 4b deletes is the SDP *assembly path* (`RequestSDPCommit` / `projectDesignSDPReview`), which §6 replaces with the deterministic computation — it was never a command.

## 7. webApp design

### 7.1 Components (pure, `src/components/activity/`)
- `lifecycleGraphLayout.ts` / `lifecycleGraphGeometry.ts` — tested pure layout: column = longest-path depth; git-style lanes (trunk on lane 0, branches take the lowest free lane and return to the join's lane).
- `LifecycleGraph.tsx` — hand-rolled SVG, SlimSpine's visual language. Uniform circular pips with **icons**: robot = agent task, artifact/document = review task; state by fill/ring + small corner badge. Rails are **directed** (forward arrowheads). A **return arc** review → agent is drawn only when that pair has been sent back ≥ once, carrying `↻N`. Phase **labels** (name · weight, ✓ when gated) with no bracket bars. Lane labels at forks. Active node = pill with inline title + caret. Click = latest revision; right-click / caret / ContextMenu key / Shift+F10 = revision menu (shortcut to the select).
- `LifecycleGraphMini.tsx` — thumbnail for plan rows and graph tiles.
- `RevisionSelect` — `Revision 3 of 3 · latest ▾`, identical on dispatch and review bodies; the selection carries across a draft→review pair; URL param `rev`.

**AMENDED 2026-09-24 (stage 5, shipped).** Three corrections this section's build proved:

- **`rev` is OMITTED when the selection is the latest** (stage-5 ruling, Task 8; implemented in `components/activity/activitySelection.ts`'s `revisionParam`). A URL with no `rev` MEANS "the latest", and only a deliberately picked non-latest revision writes the param. Pinning `rev=N` on every click would turn a running task's link into history the moment N+1 landed. Same rule in §7.2's route.
- **`PlanRow` (`components/activity/planRowFor.ts`) has no `deployment` row** (R5): the live architecture model carries no deployment layer, so a row for it would always be empty. The vocabulary is FRONT END / RESOURCES / RESOURCE ACCESS / ENGINES / MANAGERS / CLIENTS / SYSTEM TESTING, plus the M0 milestone.
- **`laneLabel` is never set.** §7.1 asks for lane labels at forks; the two forks the 14 lifecycles actually contain are too narrow for a label, which `lifecycleGraphGeometry` would drop anyway. The id (`ActivityLifecycle.laneLabel`) exists and nothing places it.

### 7.2 Activity Experience (route `/project/$projectId/activity/$activityId?task=&rev=`)
`ExperienceChrome` (eyebrow `R-BSA · SERVICE` / `ACTIVITY 2 · ARCHITECTURE`) + `spine = <LifecycleGraph>`; body by task kind:
- **dispatch** → ONE episode (the selected revision's): header facts (worker class, command, exit criterion) + turn timeline; running → generating scene. Sub-attempts listed only when a revision had more than one.
- **review** → reviewers strip (set + one-line reason from the engine) → artifact via the existing renderers (`ArtifactRenderer` kinds, `ServiceContractView`, `FrontendArtifactView`, `TestPlanView`, …) wrapped in `CommentProvider` → `CommentMargin` → `SubmitBar` (`submitVerb.ts` gains `allowSendBack: false` for projectDesign).
- **non-latest revision** → read-only banner + Back to latest; artifact as of that revision; that revision's threads with resolutions expanded; no submit bar.

Default task when none is in the URL: awaiting-human → failed → running → last passed. (A PRISTINE activity — no awaiting-human, no failed, no running, nothing passed — the chain leaves undefined; it opens `nodes[0]`.)

**AMENDED 2026-09-24 (stage 5, shipped).**

- **"artifact as of that revision" DID NOT SHIP** (R1/GAP-5). No op takes a ref: `ConstructionReviewSubjectRef.ref` names a git object that no HTTP or MCP operation exposes, so the screen cannot fetch the artifact the way it stood at that revision. What ships is the read-only banner + Back to latest, THAT revision's threads with resolutions expanded, that revision's verdicts and roster, no submit bar — and the CURRENT artifact under a caption (`Activity.HISTORY_CAPTION`) saying it is the current one, not the one this round judged. The fix belongs to stage 4: `QueryActivityView` should take a revision and return the artifact as of its `stagedRef`. Earmarked in `docs/bugs/2026-09-24-stage5-webapp-earmarks.md`.
- **The rail is read-unified and WRITE-asymmetric** (R2/GAP-6). The review body is one component for every activity type, but a CONSTRUCTION review can only submit a verdict: `constructionManager` has no `SetReviewCommentStatus` and no `AskQuestions`, so Resolve / Reopen / Ask are design-rail-only.

  **CORRECTED 2026-09-25 (stage 4a). GAP-6 IS NOT CLOSED, and 4a must not be read as closing it.** This bullet used to promise the gap closed "at stage 4's `deliveryManager.SubmitReviewDecision`". One contract is not one behaviour: the merged `deliveryManager` answers the CONSTRUCTION rail with an explicit refusal on five paths, each naming 4b — `SubmitReviewDecision` for comment-status and withdraw → `ContractMisuse`, `AskQuestions` → `ContractMisuse`, `AcknowledgeStaleBasis` → `ContractMisuse`, `DispatchActivityTask` → `FailedPrecondition`. The SPA's `NO_CONSTRUCTION_THREAD_OP` notice, `allowAsk` and `allowQuestions` were therefore KEPT and re-worded to the server's reason, and the uitests notice-line assertion was kept: deleting them would have shipped four buttons dispatching a guaranteed 400. **The rail becomes write-unified in 4b**, when the generic DAG child replaces the dispatcher bodies.
- **`?rev` is omitted when the selection is the latest** — see §7.1's amendment.

### 7.3 Plan (route `/project/$projectId/plan?lens=list|graph|tasks`)
Replaces the construction console's shell and HomeBase's phase cards. LIST: Table 11-1 order, M0 divider, mini graph + state per row. GRAPH: `@xyflow/react`, a tile per **activity** whose body is the mini lifecycle. It is a **project network, not an architecture diagram**, so rows read top→bottom in Table 11-1 **build order** — the architecture flipped: **FRONT END** (`1 Requirements → 2 Architecture → 3 Project Design → ◆ M0`) → RESOURCES → RESOURCE ACCESS → ENGINES → MANAGERS → CLIENTS → SYSTEM TESTING. Edges are dependency edges pointing down (predecessor → dependent); M0 fans out to every root; N-STP runs in a side lane from M0 to N-IT. No utilities bar (utilities are platform-provided, no activities). Kept from the existing lens: row gutter, hover-focus (lights the full upstream + downstream chain), critical-path weight, milestone ribbon. The LIST lens uses the same order. TASKS lens survives as is (it already lists owed decisions) and now includes design reviews. Any row/tile/decision opens the Activity Experience full screen; ✕ returns with viewport and selection preserved (module memory).

**AMENDED 2026-09-24 (stage 5, shipped).**

- **The milestone RIBBON is NOT ported.** `gateOccurrences`/`gateRibbon.ts` described a per-ACTIVITY lane's feeders and its complete-count; the activity-tile graph has no lane to hang that off. The milestone is carried instead by the **M0 divider** (LIST) and the **M0 tile with its fan-out to every non-front-end root** (GRAPH). The other three "kept from the existing lens" items — row gutter, hover-focus, critical-path weight — all shipped; hover-focus was REWRITTEN transitive (R6: the full upstream + downstream chain, with milestone edges excluded from the walk so a build tile's hover does not light activities 1/2/3, while M0's own hover keeps the every-build-tile rule).
- **The plan's mini lifecycles are DERIVED, not read per activity** (R3). There is no batched `QueryProjectView`, and fanning `QueryActivityView` out over 32 rows to draw 32 thumbnails is not a read the screen can afford. `miniLifecycleFromRow.ts` derives each thumbnail from the project read the plan already has. Earmarked for stage 4.
- **The build-order ROW is derived client-side** (R4). `LayerForActivity` answers `("", "projectWide")` for a componentless activity, which is not a build-order row, so `planRowFor.ts` derives the row from the activity id + the committed architecture. Widening `LayerForActivity` would make it a server fact; earmarked.
- **TASKS gets its design reviews from `ArtifactSlotView.stage === 'awaitingReview'`** — a definite derived source off the project read, not a new probe.
- **✕ returns to the plan LENS the reader left** via module memory (`planLensMemory`), which is page-lifetime, exactly as `graphViewport` already is. A cold tab lands on `?lens=list`.

### 7.4 Deleted
`construction/detail/DetailPane.tsx`, `FocusView.tsx`, body dispatch ladder; `routes/DesignExperience.tsx`, `containers/SystemDesignContainer.tsx`, `SystemDesignView`'s step ladder, `routes/ProjectDesignExperience.tsx`, `SlimSpine.tsx`, `PHASE1_ORDER`/`PHASE2_ORDER`, `gateOccurrences.ts` (the server now stores the revision). Old routes redirect to the matching activity. Artifact renderers and review aids (contract flow diagrams, UI-mock iframe, STP flows) are kept — inventory before deleting.

**AMENDED 2026-09-24 (stage 5, shipped). Three corrections, each measured by an import-edge grep in Task 13.**

**(a) DELETED as listed, plus one this section did not name.** `DetailPane.tsx`, `FocusView.tsx`, the body dispatch ladder (PORTED into `taskArtifactFor.ts` / `ArtifactPanel.tsx` FIRST), `routes/DesignExperience.tsx`, `containers/SystemDesignContainer.tsx`, `routes/ProjectDesignExperience.tsx`, `PHASE2_ORDER`, `toPhaseCards` (replaced by `adapters.currentPhaseOf`, which also fixed a latent bug: a fully-committed current phase used to fall back to `phases[0]`, so the header could read "System Design" for a project in Construction), `PhaseCard`, `gateOccurrences.ts`, and the construction console with its three lenses. **Also deleted, though §7.4 does not name it: `laneSpine.ts`** — R3 replaced it with `miniLifecycleFromRow.ts`. Old routes redirect (`beforeLoad`, registered with no component).

**MOVED, not deleted:** `graphViewport.ts` and `rowGutter.ts` → `components/activity/`. `PlanGraph.tsx` depends on both; five importers were repointed.

**(b) `SystemDesignView`'s step ladder, `SlimSpine.tsx` and `PHASE1_ORDER` are NOT deleted in stage 5.** The out-of-scope MCP widget `containers/McpSystemDesignContainer.tsx` consumes all three (`:38` `PHASE1_ORDER`, `:55`/`:390` `SystemDesignView`, which imports `SlimSpine` at `:64`). `routes/HomeBase.tsx:44` and `uitests/tests/support/testids.ts:26` also read `PHASE1_ORDER`. **Deferred to stage 6, to be deleted with that container** — §7.4 names all three as stage-5 deletions and is wrong about the timing, not about the outcome.

**(c) `detail/bodies/` and `list/` are not deleted wholesale.** `taskBriefing.ts`, `episodeAttribution.ts`, `spanGeometry.ts` and every `list/*` but `ActivityTreeView.tsx` survive, because the TASKS lens and the episodes panel reach them. The KEEP block of §7.4's own sentence — artifact renderers and review aids — held: `ArtifactPanel.tsx` replaced `ArtifactBody.tsx` as their only reachable caller BEFORE it was removed, and `uitests/tests/preview/activity-renderers.spec.ts` is the black-box proof that the five Phase-1 renderers still mount. **26+ uitests specs** (38 in the end) were deleted or retargeted; §7.4 does not mention the suite at all.

### 7.5 Component gaps found by the prototype (fix in stage 5)
1. `CommentableList`'s disabled branch never registers anchors → read-only history cannot place margin cards. Anchor enrolment must not depend on comment affordances.
2. `ContractSignatureList` arms `contractOpAnchor` but never registers it → contract threads are always UNPLACED.
3. `GeneratingScene` role line/footer copy is design-rail specific.
4. `MarginThreadCard` collapses resolved threads → add an `expandResolved` mode for history.

## 8. Stages

Each stage = its own plan + subagent-driven run in a worktree; drain-and-cutover, not `GetVersion`, where task queues change owner.

**Merge order (green at every merge, per the 2026-09-20 gate recon): 0 → 2 → 1 → 3 → 4 → 6, with 5 running in parallel from 0 and merging after 3.** Stage 4 is **two merges, 4a then 4b** (the split and its evidence are in the table below); both must be on main before the single drain and the one release that covers stages 3 + 4a + 4b. Stage 3 entry criteria (from the stage-0 final review): the N4 reconstruction in `normalizeAttempts` appends a rejected gate attempt per send-back note with no dedup against ledger rejections, and phase completion is re-derived from gate state while `ResolveConstructionRow`'s resolved completions are discarded — both inert today and both corrupt revision history the moment stage 3 persists gate attempts; they are blocking work items of stage 3, not earmarks. Why not model-first (AMENDED 2026-09-23, stage-1 planning, reproduced against the live state by `jq` into a temp root + `go run ./cmd/aiarch-state-mcp validate --root <tmp> --slot System`): the earlier recon named ALIGN-MISSING-PKG / ALIGN-EXTRA-PKG / CC-* but missed three more Errors that close every door on a model-first wave — **SYS-CARD-MGR** ("system has 6 Managers; The Method limits a system to 5", no `buildStatus` exemption and no waiver path for an Error), **DV-SINGLE-MGR** (a view's Client edges may enter one Manager; the unified core use case enters the three old rails) and **DH-CONTRACT-FACET** (a `deliveryManager` contract no component owns is a fossil). With ALIGN-EXTRA-PKG forcing the three old components to stay while their packages exist, `delivery-manager` + its contract + its relationships + the three-into-one core use case + every re-keyed dynamic view + the deletion of the three Manager components and contracts are ONE indivisible commit that is green only alongside the new Go package — i.e. stage 4's first commit. Stage 1 therefore carries only what is true today: the volatility merge and the planned-component derivation rule. The self-amendment loop must run `validate --slot <every slot edited>` as well as `--slot System` (System DOWNGRADES other slots' Errors — VOL-GLOSS was caught only by `--slot Volatilities`).

Must-ship-together sets: **stage 2** = method-assets release + pin bump + both generated tables + `derived-plan-write`; **stage 1** = the volatility merge + its component re-points in one commit (deleting a volatility without re-pointing its components is red); **stage 4 (first commit)** = `delivery-manager` component + `deliveryManager` contract (12 ops) + relationships + the `execute-a-project-activity` core use case with every re-keyed dynamic view + re-parented variations + deletion of the three Manager components/contracts + the new Go package + old slot-5 component deletion + `cmd/clientgen/main.go` and `cmd/appgen/main.go` exposed-manager lists + `arch_test.go` allowlists + `registered_names_test.go` golden + drain.

**AMENDED 2026-09-25: the stage-4 set SHIPPED as commit `5354b1f2` (112 files) — and it has seven more members this spec never named.** Each is a gate: leave it out and the wave is red, or silently wrong.

1. **`cmd/clientgen/mcpdocs.go`** — a build gate, not documentation. `mcpemit.Generate` ERRORS on an empty doc string, so the twelve ops need twelve docs in the same commit.
2. **`cmd/appgen/main.go:84`'s `managers` list** — a **THIRD** exposed-manager list, and neither of the two the bullet above names. `exposedManagers` (`clientgen/main.go:62`) and `WebExposedManagers` (`appgen/main.go:155`) are the WEB-WIRED set (billing excluded); `managers` is the CODE-GENERATED set (billing included) and it is what drives temporalgen, the worker registration and the SDK. Miss it and `worker.gen.go` registers the wrong manager.
3. **`cmd/server/managerlog.go` and `cmd/server/hooks.go`** — the composition root. `hooks.go` also carries the body-id authorization guard (§5.5) and the per-rail `Repo()` hook whose three bodies were NOT equivalent: the design arm is the superset and must be the one that survives, or the local design rails go dormant.
4. **The slot-6 deployment container list** — `DEP-CONTAINER-REF` resolves by NAME, so a deleted Manager component leaves a dangling container reference.
5. **`engine_test.go`'s `realizedViews` table AND its `RuleContractOpMax` pin.** Re-keying the dynamic views without the table is red; and `DH-CONTRACT-OPCOUNT-MAX` goes SILENT at 4a (removing a 16-op and a 13-op contract and adding a 12-op one leaves nothing above 12), which is the opposite of what a copied stage-1 prediction says.
6. **`make derived-plan-write`'s slots 9 and 10** — which needs the Makefile seam moved FIRST. `derived-plan-check`, `derived-plan-write` and `construction-state-reset` all name `./internal/manager/projectdesign/`, the package this very commit deletes, and slots 9/10 are never hand-edited. See the `DERIVED_PLAN_PKG` note in the self-amendment procedure below.
7. **The `systemtests` module's harness.** `make gen-sdk` → `pruneStaleSDK` (`cmd/appgen/main.go:342`) DELETES `systemtests/internal/sdk/{http,mcp,types}_{system-design,project-design,construction}.gen.go`, which the hand-written harness calls — a separate Go module the server module's own gates never compile, whose CI (`systemtests.yml`) triggers on `server/**` and `.aiarch/**`, both touched here. In the same breath: **`.testingState.systemTestPlan.useCaseIndex` must be re-pointed whenever a use case is deleted from slot 4** — it is the join the `stp_uc*` tables are generated through, and a dangling entry there outlives the commit that caused it.

Self-amendment procedure (precedent `2026-07-31-callchain-rollout.md`): hand-edit `.aiarch/state/project.json` → `make gen-models` → `make method-check` → `GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System` AND `--slot <every slot edited>` (System downgrades other slots' Errors) → `GOWORK=off make test-short` → `cd webApp && npm run check`. State slots 9/10 are never hand-edited — `make derived-plan-write`. Never two implementers on `project.json` concurrently, even in disjoint regions.

**AMENDED 2026-09-25 (stage 4a): `DERIVED_PLAN_PKG`.** The procedure's own slot-9/10 step names a Go package, and a wave that RELOCATES that package breaks the procedure in the middle of the one commit that must stay green. `server/Makefile` therefore reads the package from one variable — `DERIVED_PLAN_PKG`, consumed by `derived-plan-check`, `derived-plan-write` and `construction-state-reset` — defaulting to the current home and flipped by the relocating commit itself. Move the seam in its own commit BEFORE the indivisible one (4a did: `b10c9fce`, no behaviour change, gate passes and the writer no-ops exactly as before). Any future self-amendment that moves a package named by a Makefile recipe owes the same two-step.

Pre-req: land or abandon the unmerged `lifecycle-2-conformance-gate` branch (touches manager/construction) before stage 3.

| Stage | Content | Ships |
|---|---|---|
| **0 Read contract** | Add read-only `QueryActivityView` to the **existing** `constructionManager` (12→13 ops: a design-health Warning only, no ALIGN/CC exposure, no drain) returning lifecycle + tasks + revisions (derived from today's `Attempts`/`OperatorNotes`/episodes) for one activity; preview fixtures validated by `fixture-schema.mjs`. Same PR: fix the `phase.String()` → `artifactKind` defect and stop swallowing the `ProposeReviews` error. | a schema-stable read for the webApp |
| **1 Model (reduced)** | `project.json` self-amendment: B-02/B-03/B-04/B-13/B-14 merge into one **Project Delivery Workflow** volatility (all three Managers encapsulate it); `designHealthEngine`'s rules split out as the stand-alone **Design Conformance Rules** volatility; the estimation engine's rule that a `buildStatus: "planned"` component derives NO construction activity (pinned — the production pump sweep would otherwise try to build it). `reviewEngine` was already generalized in stage 2. Only the estimation contract regenerates (`engine/estimation/contract.gen.go`, `projectstate/toolcatalog.gen.go`). | model, 19→18 volatilities; pump-safe `planned` posture |
| **2 Lifecycles + review engine** | *(stage 0 already shipped `lifecycles.json` in method-assets v0.9.0, the server pin + parity test, `gen-lifecycles` and `lifecycles.gen.ts`, and the artifactKind fix — all additive.)* Stage 2 REMOVES the `profileRows`/`phaseTasks`/`gateTasks` family from projectStateAccess and re-points every reader (`ProfileFor`, `CommandFor`, `gen-uiprofiles`) at the method-assets data, retiring the stage-0 parity test and the test-only `lifecycleTypeKey` copy; `EffectiveGate` → reviewEngine, generalized signature taking the activity type (dissolving the Manager-side kind table), M0 human floor; plan derivation emits activities 1–3 + M0 prefix (`ActivityType` requirements/architecture/projectDesign appended, never renumbered). | engine + data, no behaviour change on the rails yet |
| **3 Unified rail** | task-level revisions/threads/verdicts in project state; activity branch; stage/review/commit verb family; construction send-back persistence; `QueryActivityView` read. | construction review history becomes real |
| **4a DeliveryManager (model + package)** | **SHIPPED 2026-09-25** on branch `activity-experience-stage4a`, `4baed01a..<branch HEAD>` — 24 code commits ending at `747f2c6e`, plus this docs commit. The model wave stage 1 could not carry, as ONE indivisible commit (`5354b1f2`, 112 files): `delivery-manager` (`deliveryManager`, 12 ops per §4, `ActivityView` reused) + 16 relationships + the `execute-a-project-activity` core use case (timer + clientAction entries as step-local `alt` groups) with every re-keyed dynamic view + 11 variations re-parented + `commit-to-a-project-option` demoted + deletion of the three Manager components and contracts — together with `internal/manager/delivery`, where the eleven workflow files moved **verbatim under their existing registered names**, the three impl files merged into one and the three test files into one, and the twelve ops became a thin dispatcher over the forty implementations that already existed. Also: ONE worker on task queue `delivery` registering all eleven workflow types; the per-activity CAS **armed**; the `buildStatus` closed vocabulary (entry criterion — a typo re-derives a planned component); the `{p}:phaseAdvance` re-key; four new design-rail replay fixtures (19 total); the `systemtests` harness re-pointed onto the regenerated SDK; the `DERIVED_PLAN_PKG` seam; and the webApp / uitests / MCP re-key. Gates at ship: `validate --slot System` **43 advisory / 0 errors**, registered-names golden **139** (231 → 139), 19/19 replays, `npm run check` 1216/0, preview 44/44. Earmarks: `docs/bugs/2026-09-25-stage4a-earmarks.md`. | **one manager, one wire. NO DEPLOY** (see §10's drain line) |
| **4b DeliveryManager (the behaviour)** | *(the next plan)* The generic DAG child + the parallel pump; design activities run as children; deterministic Project Design (§6); the co-author twins and the eight Phase-2 draft commands deleted; ONE review engine (§5.4 — the `EffectiveGate`/`RequiresHuman`/floor-keyword move and the inline `NewReviewEngine()` collapse); engines 7→4 (**needs a founder ruling first** — the only surviving statement of that ruling says 7→**5**); the three deprecated RA facets deleted POST-drain; the artifact-as-of-revision read (R1/GAP-5); the batched `QueryProjectView(plan)` (R3/GAP-7); the `applyRecovering` row-level `Conflict` class, which stops being an edge case the moment the pump starts more than one child; and the construction rail's five refused write paths (R2/GAP-6, §7.2). Start from "What 4b must do FIRST, in order" in `docs/bugs/2026-09-25-stage4a-earmarks.md`. | the behaviour. **Drain → cutover with 4a** |
| **5 webApp** | **SHIPPED 2026-09-24** on branch `activity-experience-stage5`, 25 commits from `10d32f23` to the docs commit carrying this amendment (base `main` @`5e09b0b6`), 14 tasks each reviewed. Delivered: the branching lifecycle graph + mini + revision select (§7.1, 60 node tests); the Activity Experience route, container, dispatch body, review body and M0 Project Design body (§7.2/§6); the Plan screen's LIST / build-order GRAPH / TASKS lenses (§7.3); §7.5's four component gaps; the console, both design rails and the home phase cards deleted with old routes redirecting (§7.4); 20 preview fixture states and 42 preview Playwright cases (§9). **Deviations, each amended into §7.1–§7.4 above and earmarked in `docs/bugs/2026-09-24-stage5-webapp-earmarks.md`:** no artifact-as-of-revision read (R1), no construction comment ops (R2), mini lifecycles and build-order rows derived client-side (R3/R4), no `deployment` row (R5), the milestone ribbon not ported, `SystemDesignView`/`SlimSpine`/`PHASE1_ORDER` held for the MCP widget until stage 6. **SPA-only: no server contract, so no drain of its own — but it must not ship ahead of stages 3/4's server**, because the screens read `ActivityView.thread`/`verdicts`/`subjectRef`, which only a stage-3 server fills. | one experience |
| **6 Cleanup + deploy** | (the `.activityExecution` migration and the activities-1–3 backfill already ran in stages 3 and 2) delete `ArtifactSlot.ReviewThread`/`CritiqueVerdict`/`CritiqueNotes`, the legacy `activityConstruction` read tolerance, the stored derived fields and the deprecated facets after the stage-4 drain; narrow `PendingOperatorNotes`; drain `*:nextActivity:*` and design sessions; release. | production |

## 9. Testing

- Pure layout/geometry: node tests (**66 exist** — geometry 20, layout 11, types 10, `planGraphLayout` 18, `lifecycles.gen` 7; re-measured 2026-09-24 by running `node --test` over the five files, not by counting `void test(` by eye. "20" was the prototype's count; "60 / planGraphLayout 12" was this line's own first correction, and was short by the six cases `planGraphLayout` gained after it was written). Lifecycle data: schema + DAG validation in Go and in the generator.
- Child workflow: Temporal test-suite cases per shape — linear, fork/join (both branch orders), send-back re-opens only the judged pair, join waits for all, M0 no-send-back, human-floor under `vibes`.
- Review engine: table tests over (activityType, task, policy, floor); a Manager fake that **validates** `artifactKind` (the current fake hid the live bug).
- Rail: revision read returns artifact-as-of-`stagedRef`; construction send-back round-trips comments.
- webApp: fixtures per scenario in the preview shell; Playwright interaction suite **AUTHORED in `uitests`** — not promoted. There was nothing to promote: `interact.mjs` exists in no branch and no working tree (`git log --all --diff-filter=A -- '*interact.mjs'` is empty) and the `activity-experience-proto` branch carries no `webApp/proto*` file at all. What shipped: `tests/preview/{activity-experience,plan,activity-renderers}.spec.ts` + a retargeted `preview-shell.spec.ts` — **42 preview cases** over 20 fixture states, plus the live smoke. `npm run check` green.
- Acceptance: a fresh project runs Requirements → Architecture → M0 approval → parallel construction children from the Plan screen alone; line count of delivery manager < sum of predecessors — **measured at the END of 4b, against the `4baed01a` baseline restated in §5.2** (4a is line-neutral by design; see that amendment).

## 10. Risks

- **God-manager** — 12 ops is the App. B ceiling; guard with the smaller-than-sum acceptance test and keep per-type behaviour in data/strategy.
- **Self-amendment blast radius** — model, realizations, derived plan, service contracts and code must move together — and stage-1 planning proved it is stronger than "own wave": the Method cardinality and single-Manager view rules make the Manager collapse one indivisible commit with its code (stage 4). `activityExecutionAccess` (§5.3) likewise has no green-and-true `planned` posture (SYS-RA-ORPHAN without relationships, DV-REL-COVERAGE with them) and is modelled in stage 3 with its code. **MEASURED at 4a: the indivisible commit is 112 files touching 4 generated layers, 3 test goldens, 20 fixtures, 2 composition-root files and 92 registered Temporal names** (231 → 139). Plan the next one as a commit, not a wave — and move any Makefile seam that names a relocating package out first (`DERIVED_PLAN_PKG`, §8).
- **Authorization follows the id off the path** — `httpgen` derives a project-scoped `ResourceRef` from a PATH id and a catalog-scoped one otherwise, so moving an id into the body silently widens the ref the PDP decides on. Inert under the interim `authenticatedOnlyPDP` and invisible to every gate. The convention (§5.5) is a composition-root project-scoped Manager decorator, pinned by a test that the root installs it; the generator is deliberately not taught to read a body id.
- **One drain covers stages 3 + 4a + 4b, once, before one release.** 4a must NOT deploy alone: 4b changes workflow type names and workflow ids again, so a deploy between them would need its own drain for nothing. 4a's own contribution to the drain is three retired task queues, one new one (`delivery`), the `{p}:phaseAdvance` re-key, the two sweep Schedules renamed `construction:*` → `delivery:*` (the OLD ids must be `temporal schedule delete`d explicitly — a Schedule left on a queue no worker polls is a silent dead sweep, not an error), and the reason the drain is non-negotiable even though no workflow TYPE name changed: the registered activity-name set SHRANK by 92 names. The memorised `drain *:nextActivity:*` guidance is now incomplete — sweep the `delivery:` prefix too. The full sequence is `docs/bugs/2026-09-24-stage3-rail-earmarks.md`.
- **Parallel children** raise concurrent writes to `project.json` — the rail's commit step must serialize merges per project (activity branches merge through one queue).
- **In-flight workflows** — drain before the 3+4a+4b release and before stage 6; task-queue ownership changes.
- **Amendment UX** — re-opening an upstream activity invalidates downstream (unifies StaleBasis with replan); covered minimally here (Architecture → projectDesign recompute); the general case is a follow-up spec.
