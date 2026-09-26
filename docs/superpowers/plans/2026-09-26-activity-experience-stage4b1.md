# Activity Experience — Stage 4b1 (DeliveryManager: one child) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make ONE workflow type walk ANY activity's lifecycle DAG. Stage 4a put three rails in one package behind twelve ops; 4b1 replaces the three rails' bodies with one registered child — `deliveryActivity`, id `{projectId}:activity:{activityId}` — that reads `methodassets.LifecycleFor(key).Tasks`, forks and joins on `dependsOn`, suspends a review task on its gate, and re-opens only the judged pair on a send-back. Its first instance is **deterministic Project Design** (spec §6): the `projectDesign` lifecycle's single review task has no dispatch, so the child COMPUTES its subject — DerivePlan → network → the four options → risk → cost — stages it, opens the M0 review with the engine's human floor, and on approve advances the root `phase` and lets the pump go. The seven open stage-3/4a entry criteria that gate any new writer land FIRST; the co-author twins, `systemdesignphase.go`, both phase-advance wrappers and the SDP-assembly session are deleted in one indivisible commit; and the five construction write paths the 4a dispatcher refuses become real, which is what finally closes R2/GAP-6. **Stage 4b2** — the signal-driven parallel pump, `ReadProjectAtRef`, the batched `plan` view, engines 7→5, the deprecated facets' deletion — is a separate plan written after this one merges.

**Architecture:** This is a behaviour wave with almost no model in it. The unit of work is a Go commit behind the drift gates, not a `project.json` edit: R9 holds the model to **two additive `$defs` edits and no dynamic-view edit** — measured, because no dynamic-view step in the committed model names any workflow, so retiring seven workflow types touches nothing in it. Everything that can fail independently is pulled in front of the child — the behaviour oracle (Task 1), the design-health rule move (Task 2), four wire/store entry criteria (Tasks 3–6), the `applyRecovering` row re-read (Task 7) — so that when the child's first fork/join deadlocks there are six fewer candidate causes. The child is then built on the SIMPLEST lifecycle first (`projectDesign`: one task, `dependsOn: []`, no dispatch — Task 9), before the design rail's four-phase linear walk (Task 10) and construction's ten-task fork/join (Task 11). The deletion is LAST (Task 13), because the fresh replay fixtures must be captured against the new child while the old bodies are still registered, and because the golden, the frozen-names list and the seven dynamic views all move in that one commit. **Drain-and-cutover, not `GetVersion`**, for the type re-key.

**Tech Stack:** Go 1.26 (`GOWORK=off` always), Temporal Go SDK (workflows, `workflow.Selector`, replay testing), `.aiarch/state/project.json` as the model database (git-as-DB), modelgen / clientgen / appgen / temporalgen codegen, `framework-go@v0.11.1` (`arch.CheckFileLayout`, `methodcheck`, `fwra`) and the in-repo `designhealth` engine, `method-assets@v0.9.0` (`lifecycles.json` — PINNED, R5), React 19 + TypeScript 5.9 for the SPA, Playwright 1.50 in `uitests/`.

**Spec:** `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` — §3 (the lifecycles, and the `reviews` pair a send-back re-opens), §5.1 (the pump, unchanged here except for the child it starts), §5.2 (the child and its acceptance), §5.3 (the one staging/review rail and the two append-only ledgers), §5.4 (one review engine), §5.5 (body-carried ids — nothing new here), §6 (Project Design · M0), §8 (the 4b row), §9 (testing), §10 (risks). **Architect rulings this plan executes:** `stage4b-architect-rulings.md` Ruling 3 (a) one type with a dispatch-strategy seam, (c) the deterministic projectDesign child and the compute strategy, (d) the 19 fixtures archived as evidence with a fresh set captured at cutover. Ruling 3(b) (the react-by-signal pump) and Rulings 1–2 are **4b2's**. **Predecessor plan whose format and constraints this plan carries forward:** `docs/superpowers/plans/2026-09-25-activity-experience-stage4a.md`. **Earmarks that gate this stage:** `docs/bugs/2026-09-25-stage4a-earmarks.md` ("What 4b must do FIRST, in order", "Armed but not recoverable"), `docs/bugs/2026-09-24-stage3-rail-earmarks.md` (the DRAIN NOTE and the seven open stage-4 ENTRY CRITERIA), `docs/bugs/2026-09-24-stage5-webapp-earmarks.md` (R2/GAP-6, R1).

## Rulings carried into this plan

Each was decided before or during planning. An implementer does not re-litigate them. R1–R12 came from the founder/architect brief; R-A–R-K were ruled while writing this plan, each with the measurement that forced it.

| # | Ruling |
|---|---|
| **R1** | **4b is TWO plans.** This is **4b1 "one child"**: the seven open entry criteria; deterministic Project Design as the first generic-child instance; the co-author twins deleted; ONE review engine; the generic `{p}:activity:{activityId}` child with a per-kind strategy table keyed on EXISTING lifecycle fields; the five refused construction write paths made real. **4b2** is a separate plan — see "Out of scope (4b2)". |
| **R2** | **Workflow TYPE names change here.** The child is a new type `deliveryActivity` at id `{p}:activity:{activityId}`. `constructionConstructActivity`, `systemDesignCoAuthor`, `systemDesignPhase`, `systemDesignPhaseAdvance`, `projectDesignCoAuthor`, `projectDesignSDPReview` and `projectDesignPhaseAdvance` are DELETED. The pump quartet (`constructionPumpNextActivity`, `constructionPumpSweep`, `constructionReplanSweep`, `constructionProjectSupervision`) is UNCHANGED except that the pump starts the new child type and no longer walks past design activities. **Drain-and-cutover, not `GetVersion`.** The `frozen-workflow-names` list (`internal/registered_names_test.go:271-301`) is an ARGUED edit, not a golden refresh; the golden literal (`:68`) is regenerated from the test's own output. |
| **R3** | **Replay fixtures: archive as evidence, re-capture fresh.** The 19 pre-4b fixtures move to `server/internal/manager/delivery/testdata/replay-archive/` — OUTSIDE the `testdata/replay/*` glob, so no vacuity or orphan guard sees them — with a README naming the drain that retired them and the commit at which each last replayed; the `Test_Replay_*` registrations for the deleted types go with them. A FRESH set is captured against the new child BEFORE the twins are deleted, over spec §9's shapes: linear (deployment), fork/join (service, `stp` ∥ `detailedDesign`, both branch orders), send-back → redraft → approve (re-opens only the judged pair), join-waits-for-all, the design equivalents (requirements approve; architecture send-back) and M0 no-send-back. Those replay green at every later commit of this plan. |
| **R4** | **Root `phase` still moves at M0.** `nextEligibleActivity` (`deliverymanager.go:9387-9389`) returns `verdictQuiescent` unless `proj.Phase == PhaseConstruction`, `PumpSweepWorkflow` filters on the same (`pumpsweep.go:88`), and `AdvancePhase` is literally `p.Phase++` (`projectstateaccess.go:483-488`). The projectDesign child's gate-passed handler calls `ProjectStateAdvancePhase` exactly as `Phase2AdvanceWorkflow` does today. Spec §5.3's "derived from milestone position" is amended to say so; derivation is stage 6. Cost if wrong: one assignment. |
| **R5** | **No method-assets release in 4b1.** `parseLifecycles` uses `dec.DisallowUnknownFields()` (`method-assets@v0.9.0/lifecycles.go:96`) and `mustParseLifecycles` panics at package init, so an unknown task field is a hard failure. The strategy table keys ONLY on v0.9.0's fields: `id, kind, title, phase, dependsOn, reviews, command, workerClass, artifactKind` plus the phase's `gate`/`weight`/`exitCriterion`. A field this plan cannot do without is a **STOP** (platform release + founder), never a workaround. |
| **R6** | **Behaviour parity where the rail is unchanged.** Every construction gate decision, roster, venue and artifact write reachable today is reproduced by the generic child through the same RA verbs (`activityExecutionAccess`'s 12 with `expectedActivityVersion`, `agenticJobAccess`, `sourceControlAccess`, `artifactAccess`), with `isGitLocalVenue` recognition preserved at EVERY reader (R-D). The local profile's construction rail stays DORMANT and its design rail ACTIVE; the M0 floor is human; `vibes` still auto-gates design reviews through the same `reviewEngine` answer. |
| **R7** | **The five refused write paths become real** (`deliverymanager.go:11911`, `:11995`, `:11998`, `:12018`, `:12047`): comment-status (Resolve/Reopen) and `AskQuestions` on a construction round write `activityExecutionAccess.SetReviewCommentStatus` / `AppendReviewVerdict`+thread; withdraw gets `RoundWithdrawn`'s own wire member (Task 4); `AcknowledgeStaleBasis` on construction acknowledges the row's stale pin; `DispatchActivityTask` on construction re-runs the task as a new attempt. The SPA's `NO_CONSTRUCTION_THREAD_OP` / `NO_CONSTRUCTION_STALE_OP` notices and the `allowAsk`/`allowQuestions` gating come OUT in the same plan (Task 14), with the preview fixtures and node tests inverted rather than deleted. |
| **R8** | **Design-health rules move FIRST.** The ~724 lines in `coauthorartifact.go` (`:1801-1989` and `:4585-5119`) leave the Manager for `internal/engine/designhealth` as ordinary rules (the engine stays an Engine in 4b1; 4b2 reclassifies it), pinned by the existing `TestGreenFixtureAdvisoriesFire` plus a parity test that the moved rules fire identically on the green fixture and on one red mutant. |
| **R9** | **Model edits only where code forces them, and there are exactly TWO.** `deliveryManager`'s 12 op signatures are unchanged; `activityExecutionAccess` gains no verb; the `$defs` edits are `ReviewRound.artifactKind` + `ReviewRoundInput.artifactKind` (Task 3) and `TaskRevisionOutcome`'s `withdrawn` member (Task 4); the design-health move needs no slot-5 edit (same component, and `design-health-engine` has no `.serviceContracts` entry at all). **There is NO dynamic-view edit.** Measured at `86d3223a`: `grep -oE 'systemDesignCoAuthor|systemDesignPhase|systemDesignPhaseAdvance|projectDesignCoAuthor|projectDesignSDPReview|projectDesignPhaseAdvance|constructionConstructActivity' .aiarch/state/project.json` returns **zero hits in the whole file** — the `execute-a-project-activity` view's 14 steps name components and op labels (`recordOperatorNote(activityId, note)` style) and no step names a workflow, so retiring seven workflow types leaves the model untouched. Task 13 verifies that by grep instead of editing. Self-amendment loop after EVERY `project.json` edit; `validate --slot` for every edited slot; **never two implementers on `project.json`** (Tasks 3 and 4 are the only two, and they serialize); slots 9/10 only via `make derived-plan-write`. |
| **R10** | **Testing** (spec §9): Temporal test-suite cases per shape — linear, fork/join in both branch orders, send-back re-opens only the judged pair, join waits for all, M0 no-send-back, human floor under `vibes`; the review-engine table over (activityType, task, policy, floor) with a Manager fake that **validates** `artifactKind`; rail — a revision read returns its round, and a construction send-back round-trips comments. `Test_RailLifecycleEnabled_ReadsWhatTheResolverAnswers`, the catalog-authz ratchet, the CAS tests and `TestBuildStatusVocabulariesAgree` stay green. |
| **R11** | **Line-count acceptance is MEASURED and REPORTED here, and GATED in 4b2.** GAP-4B-1 is ruled: "line count of delivery manager" means **hand-written, non-generated, non-test lines under `server/internal/manager/delivery/`** — `25,162` at the 4b1 base (`86d3223a`), against the `4baed01a` baseline of `25,643` (spec §5.2's amendment). The recipe is in Task 15 Step 4. The predecessors 4b1 deletes are the 7,712 twin lines plus `walkPhases` and the SDP-assembly session. |
| **R12** | **Out of scope:** the deploy (ONE drain covers 3 + 4a + 4b1 + 4b2 — Task 15 extends the note with the new type name and the `{p}:activity:*` ids); the `the-method-*` SKILL.md drift in method-assets (a platform release and a founder STOP in its own right). |
| **R-A** | **The re-read IS the discriminator for a terminal `Conflict` — there is no new error class.** `fwra.Kind` is platform-fixed (`framework-go@v0.11.1/resourceaccess/errors.go:15-26`) and the Temporal `Type()` a Conflict arrives as is only the Kind's name (`fwmanager.RAErrType(fwra.Conflict)`, `deliverymanager.go:4962`), so the workflow cannot tell a version conflict from a terminality conflict by class, and message-matching a store sentence from a workflow is worse than either. Measured: the two terminality Conflicts (`OpenActivity` on an exited row, `projectstateaccess.go:10064-10066`; `DecideReviewRound`/`AppendReviewVerdict` on a decided round, `:10421`/`:10260`) differ from a version conflict in exactly one observable way — **re-reading changes nothing**. So `applyRecovering` re-reads BOTH the project version and the ROW version, and when neither moved it fails IMMEDIATELY and non-retryably as `MutateTerminalConflict` instead of burning 20 attempts (Task 7). Cost if wrong: a genuine race that resolves at the same two version numbers is reported as terminal — bounded, loud and named, where today it is a 20-attempt stall reported as `MutateConflictExhausted`. |
| **R-B** | **The design-health rules become `designhealth` rules over the STAGED aggregate; no new Engine op, and no new `ActivityView` field.** Recon §3.6 asks for a new `Findings` surface because the generic child has no `SessionStateView`. It does not need one: `designhealth.EvaluateRaw` already reads `slots[n].model` with NO status filter (`absorbSystemDesign(slot, &out)`, `designhealthengine.go:684`; `parseSlots`, `:602`), and the rail STAGES the draft into its slot before the gate opens, so the eight generators become ordinary rules over bytes the engine already parses and their findings surface through the EXISTING `QueryProjectView{designHealth}` view. That also resolves the Engine-encapsulation problem outright: `designhealth` imports no `internal/` package today (measured: zero hits for `archistrator/server/internal` across all four engines' non-test files) and it stays that way. |
| **R-C** | **ONE new hand-written file, `deliveryactivity.go`, holds the child AND every workflow-context helper it needs.** `arch.CheckFileLayout` forbids a `workflow.Context`-taking func in the impl file at all (`filelayout.go:144-148`, `workflow-in-impl-file`), forbids a handwritten file that takes a workflow context but declares no ENTRY func (`:171-177`, `file-not-allowed`), forbids two entry funcs in one file (`:179-181`) and computes the name as `strings.ToLower(strings.TrimSuffix(entryFuncs[0],"Workflow")) + ".go"` (`:182`) — so `DeliveryActivityWorkflow` ⇒ `deliveryactivity.go`, and the strategies, being workflow-context takers, live in it. **Ruling 3(a)'s arch guard is therefore scoped to the WALKER FUNCTIONS, not the file**: `Test_DeliveryActivityWalker_NamesNoTypeOrCommand` parses `deliveryactivity.go` with `go/ast`, finds the four walker funcs by name, and fails if any of their bodies names an `ActivityType`/`ArtifactKind` constant or a command string literal. Precedents for the technique: `paramguard_arch_test.go` (keys bodies by receiver + name) and `TestBuildStatusVocabulariesAgree` (`internal/arch_test.go:215`, parses two switches). |
| **R-D** | **The one repo resolver has NINE readers, not six; the child replaces three and Task 13 deletes three more.** (GAP-4B-2, corrected by the pre-flight review's W5.) The six that decide rail behaviour: `constructRepoTarget` (`constructactivity.go:81,:84,:93`), `gitEnabled` cs (`:404,:407`), `railLifecycleEnabled` (`deliverymanager.go:10240-10248`), `constructionManager.railEnabled()` (`:10252-10254`), `gitEnabled` sd (`coauthorartifact.go:3035,:3038`), `gitEnabled` pd (`coauthorphase2artifact.go:1656,:1659`). **Three more read `m.repo` on the MANAGER side and none of them uses `isGitLocalVenue`**, which is why R6's parity claim survives their omission — but they are load-bearing and they die with the façades: `deliverymanager.go:2255/:2259` (the systemDesign answer-job dispatch, which **Task 12 Step 3 reuses for construction's `AskQuestions`**), `:6928/:6932` (the projectDesign answer-job twin), and `:3599/:3602` (`systemDesignManager.projectRepoBase`, the PR-url host — **losing it silently drops the design PR link**). Disposition: the child's ONE `gitEnabled` replaces the three rail `gitEnabled`s; `constructRepoTarget`, `railLifecycleEnabled` and `railEnabled()` survive verbatim; the answer-job dispatch and `projectRepoBase` MOVE onto the surviving child/Manager in Task 13 Step 4, which names them. Nine → six. `isGitLocalVenue` (`constructactivity.go:110`) stays the single recognition the two rail survivors share, and Task 13 Step 3 names it in the move-list because the file that defines it is deleted. `Test_RailLifecycleEnabled_ReadsWhatTheResolverAnswers` (`manager_test.go:27244`) is unchanged — it is already derived from producible profiles, which is the 4a lesson. |
| **R-E** | **The deterministic compute writes slots 9, 10, 11–14, 15 and 16; slot 8 is the ONE authored input it READS.** Measured: `PlanningAssumptions` (`contract.gen.go:609-618`) carries the founder's `Resources`, `CalendarDaysPerWeek`, `InfrastructureKind`, `DeclaredUsage`, `Terms`, `IndirectDailyRate` and `RateCard` — business input no engine can derive, which is why the compute reads it rather than deriving it. `Solution` (`:901-908`) contributes exactly THREE numbers to `assembleOption` (`StaffingCap`, `BufferDays`, `CriticalSpeedup`; its `ClassRates` and `CalendarDaysPerWeek` are ignored — rates come from `deriveClassRates(pa, classes)` and the per-option calendar "cheat" was retired by F5), so the four solution slots are a FOUR-ROW Go doctrine table, reproducing the committed dials exactly (11 cap 6 / buf 0 / speedup 1; 12 cap 4 / 0 / 1; 13 cap 6 / 0 / 1.8; 14 cap 6 / buf 20 / speedup 1). `RiskModel` (slot 15) is a pure join over the `ce.Risk` (`estimation.RiskScore`) rows `EstimateForOption` already returns — which publish `CriticalityRisk` and `ActivityRisk` **separately**, verified, so no field is carried twice — plus the App-C exclusion zones already in `sdpOptionInBand` (`assemblesdpreview.go:493-540`). **Slot 8 absent does NOT refuse** (controller ruling, 2026-09-26, overriding this plan's first draft): the compute fills The Method's DEFAULT planning assumptions from a Go doctrine table, PROCEEDS, records the defaulting in the attempt's `Provenance`/`Detail`, and surfaces it on the M0 review as a copy line the founder reads — because a project that cannot reach its own cost-approval gate cannot be told what it would cost, and refusing at M0 is refusing the one screen that exists to ask. `SDPInputsIncomplete` is NOT raised for slot 8. The open founder question is therefore **when the founder edits the defaulted assumptions** (a later UX), not who authors them — earmarked in Task 15 Step 2. |
| **R-F** | **`DesignCommandFor` is NOT touched in 4b1.** Deleting the eight Phase-2 draft slugs means either a method-assets release (R5's STOP) or a server-side `designKindSlug` edit that would also remove `planning-assumptions-draft`, the producer of the one slot R-E keeps authored. Measured: `TestDesignCommandsExistInMethodAssets` (`projectstate/access_test.go:7605`) requires only that a non-`""` slug HAS a command file — it does not require a dispatcher — so deleting the pd twin leaves the eight slugs green and unreachable, which is the honest state. Retiring the slugs and the eight `.claude/commands/*.md` is ONE coordinated platform step, listed under "Out of scope (4b2)". Cost: eight slugs that answer a question nothing asks for one release. |
| **R-G** | **Comment-status and Ask are Manager-side LEDGER writes on every rail; the child keeps ONE mirror signal.** R7's paths do not need new child signals: `activityExecutionAccess.SetReviewCommentStatus` and `AppendReviewVerdict` are already registered activities on the Manager's own invoker surface, and the round ledger has been the source of truth since stage 3. The child keeps `setCommentStatus` as a MIRROR signal for exactly one reason — the `vibes` autogate's "no open comments" precondition must be re-read before it synthesizes an approve (`coauthorartifact.go:551-557`), and the `resolve-before-decision` registration order (`:588`) is what makes a resolve land before a decision. The design rail's slot-side `ReviewThread` mirror stays a Manager-side write (stage 6 deletes it). |
| **R-H** | **`designActivityFor`'s third copy is deleted by DATA, not moved.** The generic child passes `(review.ActivityType(activity.activityTypeName()), task.Phase)` straight into `ProposeReviews`, and the lifecycle data already answers what the table answered: `missionReview.phase == "mission"`, `glossaryReview.phase == "glossary"`, `architectureReview.phase == "architecture"`, `sdpReview.phase == "sdp"`. The table's deliberate Phase-2 carve-out (`coauthorartifact.go:4440-4468`: nine Phase-2 kinds map to their own wire name and NOT to `"sdp"`, so nine drafts do not inherit the M0 human floor) **evaporates** — 4b1 deletes the nine Phase-2 drafts, leaving `sdpReview` as the only projectDesign review task, which SHOULD be the always-human M0 gate (`reviewengine.go:432`). `Test_DesignActivityFor_Phase2KindsAreNotTheSdpGate` is deleted WITH the carve-out it pinned, and the reason is written into the deletion commit. |
| **R-I** | **The critique round becomes a REVIEW TASK's agent reviewer, driven by the review task's own `command`/`workerClass`.** Measured in `lifecycles.json` v0.9.0: `missionReview`, `glossaryReview`, `coreUseCasesReview` and `architectureReview` each carry a `command` (`*-critique`, `system-critique`) and a `workerClass` (`product-manager`, `system-architect`), and `volatilitiesReview` carries NEITHER — which is exactly the one reviewer-less row `reviewengine.go`'s `phaseVolatilities` constant (`:119`) already names. So the strategy for "run the agent reviewers" is `task.Command != ""` and nothing else, and `runCritiqueRound` (`coauthorartifact.go:919`, systemDesign-only — measured: zero callers in `coauthorphase2artifact.go`, confirming GAP-4B-3(c)) dissolves into the ordinary dispatch strategy pointed at a review task. |
| **R-J** | **`QueryProjectView{session}`'s `session` and `projectSession` members are DERIVED from the ledger when their producers die, not deleted.** Measured SPA consumers: `useSessionState` has exactly one — `containers/McpSystemDesignContainer.tsx:48,:241`, the stage-6 widget cluster — and `useProjectSessionState` has **none**. Deleting a wire member is a contract change plus an SPA type change inside a cluster stage 6 deletes anyway, so 4b1 keeps both shapes and answers them from `.activityExecution`'s rounds and attempts plus the slot, which is the derivation `QueryActivityView` already runs. `constructionSession` keeps coming from the child's live `sessionState` query, so `liveSessionFor` (`deliverymanager.go:11378`) needs only its workflow id re-keyed. GAP-4B-9 is thereby deferred without a wire break. |
| **R-K** | **The three semantically real pd-vs-sd differences, CONFIRMED before the twin is deleted** (GAP-4B-3): (a) the extra `ProjectStageAssemblingSDP` ordinal, which survives in `ProjectSessionStage` untouched (R-D of the 4a plan — never renumber); (b) `pdCheckNoReplyTo` (`deliverymanager.go:6053-6061`), the Phase-2 ledger's outright refusal of a `replyTo`, which **STAYS** and whose client-side fold (`askEntriesFor`, `foldReplies: true`) stays with it; (c) **no critique round** — measured, `runCritiqueRound` has zero callers in the pd file. Everything else in the 199-row rename table's 160 pd rows is a prefixed copy and dies with the file. |
| **R-L** | **The child `ContinueAsNew`s on a history budget, and the dispatch wait backs off. This is a DESIGN requirement, not an earmark.** Measured, and it is a regression the collapse causes: today `SystemDesignPhaseWorkflow` starts a CHILD co-author per kind (`systemdesignphase.go:69`), so a `requirements` activity is **four separate histories** — `jq`-measured at `testdata/replay/design-pre-stage4/`: `coauthor-approve-merge.json` **187** events, `coauthor-sendback-redraft-approve.json` **311**, `phase-advance.json` 19; construction's post-stage3 fixtures are **337** and **392** for one gate path. The generic child collapses all of `requirements`' 8 tasks plus its 3 critique dispatches into ONE execution, and those fixture counts come from a test env whose pipeline poll returns immediately. In PRODUCTION each dispatch polls `15 s × 240` (`observeDesignJob`, `coauthorartifact.go:2593`/`:2600`; `runPipeline`, `constructactivity.go:821`/`:824`) and each poll is ≈4 events (timer started, timer fired, activity scheduled, activity completed) ⇒ **≈960 events per dispatch worst case**, ×11 dispatches ⇒ **≈10,560 events for one `requirements` activity**, which crosses Temporal's 10k warning on its own — before `maxPhaseRedrafts = 5` or `maxVarianceAttempts = 10` multiply it. There is no `ContinueAsNew` anywhere in the child today (`constructactivity.go` has none; the only `NewContinueAsNewError` in the package is the pump's, `pumpnextactivity.go:252`). **Two mandatory mitigations, both in Task 8:** (i) **a capped-backoff observe loop** — 15 s for the first 4 polls, then 60 s to the 10-minute mark, then 300 s to the same 1-hour ceiling: **23 polls ≈ 92 events per dispatch instead of 240 ≈ 960**, a 10× cut with the ceiling unchanged, and the wait is a `workflow.Selector` over the backoff timer plus **the task's own inbox** so an operator never waits out a backoff (a PUSHED completion signal needs a producer no RA has, and is 4b2's). **The inbox, not a shared signal channel, and that is load-bearing:** two tasks in flight on a fork means two coroutines, and a `ReceiveChannel` delivers each message to exactly ONE of them, so a polling dispatch task reading `operatorOverride` directly would eat the override meant for the gate beside it. Task 8 Step 3a is the router that makes every signal per-task, and the `fork-signal-reaches-the-named-task` shape case is what proves it; (ii) **`ContinueAsNew` at a documented budget** — `workflow.GetInfo(ctx).GetContinueAsNewSuggested()` (the server's own signal) OR `GetCurrentHistoryLength() > deliveryActivityHistoryBudget = 4000`, both verified present in `go.temporal.io/sdk v1.44.0` (`internal/workflow.go:1581`, `:1594`), checked at the ONE safe point — the top of the walk loop when `inflight == 0` — carrying a serialisable snapshot of the walk. `walkState`'s four maps are all JSON-serialisable (`byTask map[string]taskState` where `taskState` is an int, `revision map[string]int64`, `feedback map[string]string`, `produced map[string]producedSubject` whose fields are strings and an `AttemptOutcome` string), so the snapshot is exported-field copies of exactly those four and nothing else — no channel, no lifecycle (re-resolved from the activity), no policy (re-snapshotted). Cost if omitted: the deepest activity in the product becomes unresumable mid-walk, which is the one failure the append-only ledgers cannot repair. |

## Global Constraints

- Work in the git worktree `.claude/worktrees/activity-stage4b1` (branch `activity-experience-stage4b1`, from `origin/main` @`86d3223a`). The main checkout is shared with other sessions. `.claude/{skills,commands,agents}` are materialized here (three tests read them) and `webApp/node_modules` is installed.
- `GOWORK=off` on EVERY `go`/`make` command under `server/`. `ASDF_NODEJS_VERSION=lts` on EVERY npm/npx command. Gates run against PINNED platform tags (`framework-go v0.11.1`, `method-assets v0.9.0`) — **never** a `replace`.
- **Slot edits.** EXACTLY TWO: `.serviceContracts.activityExecutionAccess.$defs` (Task 3) and `.serviceContracts.deliveryManager.$defs` (Task 4). **No slot model is edited by any task** — Task 13's dynamic-view step is verify-only (R9). **Slots 9 and 10 are NEVER hand-edited** — `make derived-plan-write` alone (`DERIVED_PLAN_PKG` already defaults to `./internal/manager/delivery/`, `server/Makefile:258`, so no Makefile two-step is owed). Never two implementers on `project.json`, even in disjoint regions.
- **The self-amendment loop**, run after every `project.json` edit:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  GOWORK=off make gen-models gen-fakes gen-client gen-internal-tools gen-temporal gen-sdk gen-config gen-main gen-lifecycles
  GOWORK=off make method-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot <every other slot this edit touched>
  GOWORK=off go test -short -count=1 ./...
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run gen:api && ASDF_NODEJS_VERSION=lts npm run gen:ops && ASDF_NODEJS_VERSION=lts npm run check
  ```
  Use `go test -short -count=1 ./...`, **not** `make test-short` — the latter masks the `designhealth` package, which Task 2 rewrites.
- **Drift gates before EVERY commit that touches a generated input:**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  GOWORK=off make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check \
      gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-lifecycles-check
  GOWORK=off make sumtype-check derived-plan-check encapsulation-check method-check fix-check lint
  GOWORK=off go test ./internal/ -run 'TestRegisteredTemporalNamesGolden|TestFileLayout|TestGeneratedOnlyPublic|TestMethodLayering|TestNoBannedPhaseIdentifier|TestMessageBusManagersOnly|TestCatalogScopedOpsArePinned|TestCompositionRootGuardsEveryCatalogScopedDeliveryOp|TestBuildStatusVocabulariesAgree' -count=1
  ```
  plus, on any commit touching a workflow body or a Temporal call: `GOWORK=off go test ./internal/manager/delivery/ -run Test_Replay -count=1` (the OLD nineteen until Task 13 archives them; the NEW set from Task 13 Step 3 onward).
  **`golangci-lint cache clean` before `make lint`** — this worktree is a copy of a tree that has been linted, and the cache reports the other copy's absolute paths as false findings (measured twice, 4a and stage 5).
- Never weaken, skip or allowlist around a gate. No `//nolint`. No `default:` arm over a sum type (`sumtype-check`). No hand-edited `*.gen.*` file — regen only, committed with the input change that caused it.
- Never run `git restore`, `git clean`, `git stash`, `git checkout -- <path>` or any tree-wide reset. Back up the gitignored SDD ledger (`.superpowers/sdd/`) to the session scratchpad after every append.
- **A changed Manager op also needs** `cmd/clientgen/mcpdocs.go`'s op-doc table (a build gate — `mcpemit.Generate` ERRORS on an empty doc), `webApp/scripts/gen-enums.mjs` `OUTPUT_NAMES`, and `cmd/server/managerlog.go`. **None is expected in 4b1**: R9 keeps all twelve op signatures byte-identical, so no op doc, no manager log line and no enum OUTPUT_NAME changes. Task 4's `TaskRevisionOutcome` member is an EXISTING enum gaining a value, which `gen-enums` emits without an `OUTPUT_NAMES` edit — verify that claim by running `npm run gen:api && npm run gen:ops` and confirming `git status` shows no `OUTPUT_NAMES` diff.
- Wire-visible identifiers are NEVER renumbered: `ArtifactKind` ordinals, `ActivityType` ordinals, `ProjectSessionStage` ordinals, the root `phase`, the `AttemptID`/`RoundID` formats. New members are APPENDED.
- `required` in the contract schema dialect is PRESENCE-only. Non-emptiness lives in the Go implementation, never in `minLength` (2026-08-13 ruling; `ValidateModelIdentities` + the paramguard arch gate hold the line).
- Match the surrounding idiom exactly — JSON key order, Go comment density, the `— …` em-dash rationale style. Commit messages end with:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  ```
- **Out of scope (4b2), EARMARK only:** the signal-driven parallel pump (`{p}:pump`, a candidate SET, react-by-signal, no `child.Get`, the pump as the per-project merge queue, `PumpResult.activityIds`) and the `ProjectSupervisionWorkflow` question (GAP-4B-5); `projectStateAccess.ReadProjectAtRef` + the `revision` selector on `QueryActivityView` (architect Ruling 2, GAP-4B-10); the batched `QueryProjectView{plan}` and the deletion of `miniLifecycleFromRow.ts` + `planRowFor.ts` (R3/GAP-7); engines 7→5 with the `EstimationEngine` merge (architect Ruling 1) and `design-health-engine` → the in-repo `method-check` Utility; the three deprecated RA facets deleted POST-DRAIN with the five orphan project-scoped verbs re-homed (GAP-4B-6) and `DH-CONTRACT-DEADOP` re-measured, never predicted (GAP-4B-7); `amend-N` branch retirement; retiring the eight Phase-2 draft slugs and their eight `.claude/commands/*.md` as one coordinated platform step (R-F); collapsing the three `kind:'session'` members to one (GAP-4B-9, deferred by R-J). Also out of scope: the `the-method-project-tracking` / `the-method-review-routing` SKILL.md drift in **method-assets** (a platform release, founder STOP), and `uitests/testdata/coreUseCasesProject.json`'s regen (reads committed `main`, so post-merge).

## Task order

**The oracle and the entry criteria, one commit each, all ahead of the child:** Task 1 (the behaviour oracle + the archive skeleton) → Task 2 (the design-health rule move) → Task 3 (`ReviewRound.artifactKind`) → Task 4 (`RoundWithdrawn`'s own wire member) → Task 5 (`gateSubjectRef` names the commit + `OpenActivity` refuses a typ/variant change) → Task 7 (`applyRecovering`'s row re-read and the terminal-conflict class) → Task 6 (the stranded-`pending`-round sweep).

**Task 6 is LAST of the seven, not sixth.** Its code calls `roundGateKey` (born in Task 3 Step 4), reads `ReviewRound.ArtifactKind` (Task 3 Step 2) and calls `isTerminalConflict` (born in Task 7). It is numbered 6 because the sweep belongs with the entry criteria it discharges; it is EXECUTED after 3 and 7, and it is struck from the parallel set below.

**The child, smallest lifecycle first:** Task 8 (the strategy table + the DAG walker, tested by Task 1's cases) → Task 9 (deterministic Project Design: the compute strategy, the M0 gate, the `phase` advance, the stale-basis recompute) → Task 10 (requirements + architecture through the child) → Task 11 (construction through the child; `walkPhases` deleted) → Task 12 (the five refused write paths).

**The deletion:** Task 13 — ONE indivisible commit.

**Then the wire's consumers and the record:** Task 14 (webApp + uitests) → Task 15 (drain note, earmarks, spec amendments, the line-count measurement).

**Must ship together:**
- Task 3 is ONE commit: the two `$defs` edits, the regenerated files, both writers and the join test — a half-threaded kind field lets two kinds' round 1 bind one gate attempt, which is the defect it exists to close.
- Task 5's two halves are ONE commit: `gateSubjectRef` and `OpenActivity` are both store-or-writer fixes the generic child's single review-round writer would otherwise have to be touched for twice.
- Task 13 is ONE commit and its steps run in the printed order. `TestFileLayout` forbids a handwritten file with no entry func, the registered-names golden and the frozen-names list both move together, and `TestRegisteredTemporalNamesGolden` fails on a partial deletion. There is no ordering in which any two of those are separate green commits. It carries **no model edit** (R9), so unlike 4a's indivisible commit it runs no self-amendment loop.
- Task 14's steps may not be split across a green `npm run check`: inverting `activityVerbs.ts` without its node tests and the preview fixtures leaves four buttons asserted absent that are now present.

**Parallelism.** Tasks 2, 3, 4 and 5 are genuinely disjoint (the designhealth package / two different `$defs` / one store guard + one workflow helper) and may run in parallel — **except that only one of them at a time may hold `project.json`** (Tasks 3 and 4 both edit it, so they serialize against each other). **Task 6 is NOT in the parallel set:** it depends on Task 3's `roundGateKey` and `ReviewRound.ArtifactKind` and on Task 7's `isTerminalConflict`, so it runs after both. Task 1 must land before Task 8. Task 7 must land before Task 8. Everything from Task 8 onward is strictly sequential.

## Execution risks and how this plan removes each

1. **A fork/join deadlock is the hardest failure in this wave to attribute, and nothing existing exercises one.** Measured: `stp` does NOT run in parallel with `detailedDesign` today — `walkPhases` iterates `in.Activity.Phases`, a flat `[]ActivityMethodPhase` from `ProfileFor(typ,variant).PhaseIDs()`, and `service`'s phase order puts `test_plan` AFTER `detailed_design`. The fork and the join are in `lifecycles.json` (`stp dependsOn [srsReview]`, `testing dependsOn [integration, stpReview]`) and unread by any code. Task 1 writes the seven target scenarios as Temporal test-suite cases against TODAY's code first, so the oracle exists before the walker does, and Task 8 is judged by cases that already passed once.
2. **The row-level `Conflict` becomes live the moment a second writer touches a row, and 4b1 creates one before 4b2's pump does.** Task 12's Manager-side comment-status and Ask writes ARE that second writer, on a row a live child holds. Task 7 lands the row re-read first and R-A names the discriminator; without it, resolving a comment during a live gate fails the child non-retryably on a healthy activity.
3. **The frozen-workflow-names list is a deliberate ruling, not a golden.** Its doc says the twenty names "must never silently disappear **even if the golden above is updated**" (`registered_names_test.go:267-269`). Task 13 Step 6 removes seven of them with the argument written into the test's own comment and into the commit message; the golden literal is regenerated from the test's printed diff, never hand-computed.
4. **The four design replay fixtures cannot be re-captured under their old names, and three vacuity guards make deleting them a trap.** `replayFixtureFiles` (`manager_test.go:32141`) `t.Fatalf`s on an EMPTY fixture directory, each per-rail test errors on a fixture no case names, and `Test_Replay_EveryFixtureDirectoryIsNamed` (`:32204`) fails on a directory no case list names AND on zero directories. R3's archive path is OUTSIDE the `testdata/replay/*` glob for exactly that reason, and Task 13 moves the directories, the case lists and the registrations in one commit.
5. **The inert `ActivityOptions` divergence goes live if the merged hook is "simplified".** `mf.ActivityOptions` has exactly one reader — the struct literal at `deliverymanager.go:12350` — and each rail's workflows consult their OWN hook through `genInvokers{Opts: optsHook}` (`:5167`, `:7563`, `:10268`), so `sourceControlAccess.syncManagedScaffold` is genuinely **5 min** on the systemDesign hook (`scaffoldSyncActivityOptions()`, `:5126`) and **30 s** on the other two. Task 13 Step 5 gives the ONE surviving child hook the 5-minute scaffold-sync preset and says why in a comment, because the child now does the design work that needed it.
6. **`designSessionAccess` has four NON-design callers and a plan that "deletes the design verbs" would take the pump's only whole-project read with them.** Measured: `pumpnextactivity.go:374` (`DesignSessionReadProjectOnBranch(projectID, "")`), `replansweep.go:29` and `projectsupervision.go` through the same `wf.readProject`, plus `deliverymanager.go:2069`/`:6875` (the two answer-job reads) and the three SDP-assembly verbs at `assemblesdpreview.go:194`/`:208`/`:226`. **4b1 deletes NO facet** (that is post-drain, 4b2) — Task 9 re-homes the three SDP-assembly verbs onto `activityExecutionAccess.StageTaskOutput`/`CommitActivityArtifacts` and Task 13 deletes the two answer-job call sites with the twins, which is what LEAVES 4b2 a facet it can actually delete.
7. **`make gen-sdk` deletes the SDK a separate Go module's hand-written harness calls.** `pruneStaleSDK` (`cmd/appgen/main.go:342`) removes every `*.gen.go` under `../systemtests/internal/sdk` not in the fresh output set, and `systemtests.yml` triggers on `server/**` and `.aiarch/**`. 4b1 changes no op signature (R9), so the SDK output set is byte-identical and the harness is untouched — **but Tasks 3, 4 and 13 all run `make gen-sdk` through the self-amendment loop**, so each of their gate blocks ends with `cd ../systemtests && GOWORK=off go build ./...` to prove it.
8. **The collapse REGRESSES history growth, and the child has no `ContinueAsNew`.** Four per-kind co-author executions become one walk: measured at 187 and 311 events per co-author fixture in a test env whose poll returns instantly, against a production budget of `15 s × 240` polls per dispatch ≈ 960 events, ×11 dispatches on `requirements` ≈ 10,560 — Temporal's 10k warning, from one activity, before any redraft. R-L designs both mitigations into Task 8 Step 6 (a capped-backoff observe loop: 23 polls ≈ 92 events, same 1-hour ceiling) and Step 7 (`ContinueAsNew` on `GetContinueAsNewSuggested()` or a 4,000-event budget, at the one point where `inflight == 0`, carrying the walk snapshot). Task 8 Step 10 has the test-suite case. This is the one risk the pre-flight review found that the first draft had not flagged at all.
9. **Concurrency makes every SHARED signal channel a theft risk, and the backoff selector would have opened one.** A `workflow.ReceiveChannel` delivers each message to exactly one receiver, so two coroutines selecting on `operatorOverride` — a polling dispatch task and a waiting gate, which is precisely what a fork produces — means an override aimed at the gate is consumed by whichever the SDK schedules first and silently lost. Filtering inside the receiver does not help: the message is already gone. Task 8 Step 3a puts ONE router coroutine in front of all four channels and gives every task its own inbox, so no task coroutine ever touches a shared channel; the `fork-signal-reaches-the-named-task` case asserts the gate got it and the poller did not, through `shapeRecorder.signalDelivered` rather than through the walk's terminal (a lost override and a declined one produce the same terminal). **And the router must never block**, or the fix becomes a worse bug: a `Send` parked on a full inbox outlives the task's `closeInbox`, no receiver can ever exist for that channel again, the router never selects a second time, and one wedged task silently swallows every later signal in the activity — including a sibling gate's decision — while the parked message sits in neither `inbox` nor `pending` and `ContinueAsNew` loses it too. Hence `SendAsync` with a `pending` fallback, a receiver-driven `drainPending`, and a `closeInbox` that drains **both the channel and `pending`** rather than abandoning either; `full-inbox-does-not-wedge-the-router` is the case that would have caught it. Two traps the same design opens and closes at the site: a `pending` entry left behind at retire rides every later `ContinueAsNew` and, after a send-back, is flushed into revision n+1; and a dispatch task that hands an unactionable decision back through `pending` spins, because `drainPending` returns it to a receive arm that is immediately ready while each iteration builds a fresh timer — so that one message goes in a local slice, not the shared queue.
10. **A failing task leaves its siblings blocked on an unbuffered channel send.** `failWalk` returns while sibling coroutines sit in `results.Send`, which is a leaked-coroutine warning at best and a masked panic at worst. Task 8 Step 3 gives `results` capacity `len(lc.Tasks)`, so every started task can always deliver its result whether or not anyone is still receiving.
11. **`ClassifyActivity`'s signature change has ten readers and one of them is a backfill tool nobody runs in CI.** Every site of `ErrDesignActivityNotDispatchable` was measured: declaration `projectstateaccess.go:8449`, producer `:8510`, `ClassifyType` tolerance `:8565`, `isDesignActivity` `deliverymanager.go:9490`, the chosen-activity guard `:9530`, `railFor` `:11682`, the `ClassifyType` doc `:8782`, `cmd/backfill-attempts/main.go:322`, the encapsulation allowlist `internal/arch_test.go:680`, and `projectstate/access_test.go:8984-8985`. Task 10 Step 2 changes rule 0 to `(typ, variant, nil)` and walks all ten, including the allowlist entry that must be REMOVED rather than left naming a deleted sentinel.

---

### Task 1: The behaviour oracle — seven scenarios as test-suite cases against TODAY's code

The generic child's hardest failure is a fork/join that deadlocks or a send-back that re-opens the wrong pair, and there is no existing test that would catch either: the current child walks a flat phase list, so the DAG has never executed. This task writes spec §9's seven shapes as Temporal test-suite cases **against the code as it stands**, so that when Task 8 replaces the walker the cases are a differential oracle rather than a new specification written by the same hand that wrote the walker. Five of the seven pass today (they are the linear shapes); **two are expected to FAIL today and are marked so**, because they assert the parallelism only the DAG has.

It also creates the replay ARCHIVE directory and its README skeleton, ahead of Task 13's move, so the archive's justification is written while the nineteen fixtures are still green and re-measurable.

**Files:**
- Modify: `server/internal/manager/delivery/manager_test.go` — one new region, appended after the last existing test.
- Create: `server/internal/manager/delivery/testdata/replay-archive/README.md`

**Interfaces produced (Tasks 8–11 and 13 depend on these exact names):**
- `lifecycleShapeCases() []lifecycleShapeCase` — the seven cases, in this order and under these exact names: `linear-deployment`, `fork-join-service-stp-first`, `fork-join-service-design-first`, `sendback-reopens-only-the-judged-pair`, `join-waits-for-all`, `m0-no-sendback`, `human-floor-under-vibes`.
- `type lifecycleShapeCase struct { name string; typeKey string; drive func(*testing.T, *shapeRig) shapeOutcome; wantToday shapeExpectation }`
- `shapeOutcome` carries what every shape asserts on: `TaskOrder []string` (the order tasks were STARTED), **`CompletedOrder []string`** (the order they finished — the branch-order cases' only discriminator, see Step 2), `Dispatched []string`, `RoundsOpened []string`, `RoundsDecided map[string]string`, `Reopened []string`, `PhaseAdvanced bool`.
- `shapeExpectation ∈ {shapePassesToday, shapeFailsUntilTheDAG}`.
- `server/internal/manager/delivery/testdata/replay-archive/README.md`

- [ ] **Step 1: Read the three existing rigs before writing anything — the cases must drive the SAME rig shape the replays use, or Task 8 cannot reuse them.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  sed -n '28076,28200p'  internal/manager/delivery/manager_test.go   # replayRegistrations + the construction scenario list
  sed -n '28473,28560p'  internal/manager/delivery/manager_test.go   # TestCaptureConstructHistories + Test_Replay_PreB1Histories
  sed -n '13356,13410p'  internal/manager/delivery/manager_test.go   # designReplayRegistrations + designReplayCases
  sed -n '32141,32230p'  internal/manager/delivery/manager_test.go   # replayFixtureFiles vacuity + Test_Replay_EveryFixtureDirectoryIsNamed
  ```
  Expected: `replayRegistrations(wf *csWorkflows) []genRegisteredWorkflow` at `:28078`; `replayScenariosPostStage3()` at `:28153`; `replayScenariosPreChange()` at `:28198`; `TestCaptureConstructHistories` gated on `CONSTRUCT_HISTORY_CAPTURE=1` at `:28473` with a `t.Fatalf` naming the `temporal` CLI; `Test_Replay_EveryFixtureDirectoryIsNamed` at `:32204` failing on an unnamed directory AND on zero directories. Note the rig constructor the scenarios call (`sc.rig(t)` / `sc.rig()`) and its fake set — the shape cases use THAT constructor, not a new one.

- [ ] **Step 2: Add the case type and the outcome recorder.** Append in the file's idiom:
  ```go
  // ---------------------------------------------------------------------------
  // THE LIFECYCLE-SHAPE ORACLE (stage 4b1 Task 1). Spec §9 asks for a Temporal
  // test-suite case per lifecycle SHAPE: linear, fork/join in both branch orders,
  // send-back re-opens only the judged pair, join waits for all, M0 no-send-back,
  // human floor under vibes. They are written HERE, against the pre-4b1 rails, so
  // that the generic DAG child is judged by cases that already ran rather than by
  // cases written alongside it.
  //
  // TWO OF THE SEVEN FAIL TODAY, DELIBERATELY. The current child walks
  // in.Activity.Phases — a FLAT []ActivityMethodPhase from
  // ProfileFor(typ,variant).PhaseIDs() — so `service`'s phase order runs test_plan
  // AFTER detailed_design and the fork in lifecycles.json (stp dependsOn
  // [srsReview]; testing dependsOn [integration, stpReview]) is never read. A case
  // marked shapeFailsUntilTheDAG asserts the parallel order and is SKIPPED with its
  // reason until Task 8 lands; Task 8 Step 10 flips it to shapePassesToday and the
  // flip is the acceptance. A skipped case that is never flipped is a plan failure,
  // which is why shapeFailsUntilTheDAG names the task that owes it.
  // ---------------------------------------------------------------------------

  // shapeExpectation says whether a shape case can pass against the code in the
  // tree at the commit that reads it.
  type shapeExpectation int

  const (
  	shapePassesToday shapeExpectation = iota
  	shapeFailsUntilTheDAG
  )

  // shapeOutcome is what every shape case asserts on.
  //
  // TWO order fields, and the distinction is the whole reason the fork is testable.
  // TaskOrder is the order tasks were STARTED, and it is IDENTICAL in both branch-order
  // cases by construction: readyTasks fans out in lifecycle DECLARATION order, which is
  // deterministic, so once srsReview passes it always returns [detailedDesign, stp] in
  // that order. Branch order is not a walker variable and must never be asserted as one.
  // What a branch order actually changes is which branch's pipeline COMPLETES first, so
  // CompletedOrder is the discriminator, and the fork's real claim — "both branches were
  // in flight at once" — is asserted as BOTH tasks appearing in TaskOrder before EITHER
  // appears in CompletedOrder.
  type shapeOutcome struct {
  	TaskOrder      []string
  	CompletedOrder []string
  	Dispatched     []string
  	RoundsOpened   []string
  	RoundsDecided  map[string]string
  	Reopened       []string
  	PhaseAdvanced  bool
  }

  // lifecycleShapeCase is one shape, its lifecycle key, the driver that runs it on
  // whatever child the tree currently registers, and what it may assert today.
  type lifecycleShapeCase struct {
  	name      string
  	typeKey   string
  	drive     func(t *testing.T, rig *shapeRig) shapeOutcome
  	wantToday shapeExpectation
  }
  ```
  - [ ] **Verify first:** `genRegisteredWorkflow` is the registration struct `replayRegistrations` returns — read it before writing `shapeRig` in Step 3 and reuse it verbatim rather than declaring a second shape.

- [ ] **Step 3: Add `shapeRig` as a THIN wrapper over the existing construction rig**, so the cases move to the new child in Task 8 by changing one registration:
  ```go
  // shapeRig wraps whichever rig the tree's current child needs, plus the recorder
  // the outcome is read out of. It deliberately owns NO fakes of its own: the fakes
  // are the ones the replay scenarios already register, so a shape case and a replay
  // fixture exercise the same store semantics. Task 8 re-points `register` at the
  // generic child; nothing else in this region changes.
  type shapeRig struct {
  	env      *testsuite.TestWorkflowEnvironment
  	rec      *shapeRecorder
  	register func(env *testsuite.TestWorkflowEnvironment)
  }

  // shapeRecorder is the observation side. Every task start, dispatch, round open and
  // round decision the rig's fakes see is appended here in the order it happened;
  // nothing is sorted, because order IS the assertion.
  type shapeRecorder struct {
  	mu        sync.Mutex
  	started   []string
  	completed []string
  	jobs      []string
  	opened    []string
  	decided   map[string]string
  }

  func newShapeRecorder() *shapeRecorder {
  	return &shapeRecorder{decided: map[string]string{}}
  }

  func (r *shapeRecorder) taskStarted(taskID string) {
  	r.mu.Lock()
  	defer r.mu.Unlock()
  	r.started = append(r.started, taskID)
  }

  // taskCompleted is the fork's discriminator. It is recorded where the task's
  // PIPELINE reaches a terminal — not where the walker marks the task passed — because
  // a gated branch's task passes only after its human decision, and the fork's claim is
  // about the work overlapping, not the gates.
  func (r *shapeRecorder) taskCompleted(taskID string) {
  	r.mu.Lock()
  	defer r.mu.Unlock()
  	r.completed = append(r.completed, taskID)
  }

  func (r *shapeRecorder) jobDispatched(command string) {
  	r.mu.Lock()
  	defer r.mu.Unlock()
  	r.jobs = append(r.jobs, command)
  }

  func (r *shapeRecorder) roundOpened(roundID string) {
  	r.mu.Lock()
  	defer r.mu.Unlock()
  	r.opened = append(r.opened, roundID)
  }

  func (r *shapeRecorder) roundDecided(roundID, outcome string) {
  	r.mu.Lock()
  	defer r.mu.Unlock()
  	r.decided[roundID] = outcome
  }

  // outcome snapshots the recorder. Copies rather than handing out the slices, so a
  // case that keeps driving after reading cannot retro-edit its own assertion.
  func (r *shapeRecorder) outcome(reopened []string, phaseAdvanced bool) shapeOutcome {
  	r.mu.Lock()
  	defer r.mu.Unlock()
  	out := shapeOutcome{
  		TaskOrder:      append([]string(nil), r.started...),
  		CompletedOrder: append([]string(nil), r.completed...),
  		Dispatched:     append([]string(nil), r.jobs...),
  		RoundsOpened:   append([]string(nil), r.opened...),
  		RoundsDecided:  make(map[string]string, len(r.decided)),
  		Reopened:       append([]string(nil), reopened...),
  		PhaseAdvanced:  phaseAdvanced,
  	}
  	for k, v := range r.decided {
  		out.RoundsDecided[k] = v
  	}
  	return out
  }
  ```
  - [ ] **Verify first:** the fakes' hook points. `shapeRecorder.jobDispatched` must be called from the `agenticJobAccess` fake's submit, `roundOpened`/`roundDecided` from the `activityExecutionAccess` fake's `OpenReviewRound`/`DecideReviewRound`, `taskStarted` from whichever fake the child touches first per task (the pipeline submit for a dispatch task, `OpenReviewRound` for a review task), and `taskCompleted` from the `agenticJobAccess` fake's OBSERVE call on the poll that returns a terminal phase. Wire them by adding a nilable `rec *shapeRecorder` field to the EXISTING fakes and a `if f.rec != nil { … }` guard at each of the five sites — never a second fake type, which would let the shape cases and the replays diverge.

- [ ] **Step 4: Write the seven cases.** Each is a table row; the drivers reuse the existing scenario helpers. Write them in this exact order and with these exact names:
  ```go
  // lifecycleShapeCases returns the seven shapes spec §9 names, in a fixed order.
  // Task 8 Step 10 appends three more — continue-as-new-mid-walk (R-L),
  // fork-signal-reaches-the-named-task and full-inbox-does-not-wedge-the-router (the
  // signal router) — none of which spec §9 names, because collapsing four per-kind
  // executions into one walk is what creates an unbounded history, a shared-channel
  // race and a delivery-overflow path all at once.
  // The names are contract: Task 8 Step 10, Task 9 Step 7, Task 10 Step 6 and Task 11
  // Step 5 each name the subset they must turn green.
  func lifecycleShapeCases() []lifecycleShapeCase {
  	return []lifecycleShapeCase{
  		{
  			// LINEAR. `deployment` is three phases, D→R each, no fork — the shape the
  			// flat phase walk and the DAG walk must agree on exactly.
  			name: "linear-deployment", typeKey: "deployment",
  			drive: driveLinearDeployment, wantToday: shapePassesToday,
  		},
  		{
  			// FORK, STP BRANCH COMPLETING FIRST. srsReview passes, then stp and
  			// detailedDesign are BOTH started (in declaration order, which is fixed);
  			// this case lets the STP branch's pipeline reach terminal first.
  			name: "fork-join-service-stp-first", typeKey: "service",
  			drive: driveServiceForkSTPFirst, wantToday: shapeFailsUntilTheDAG,
  		},
  		{
  			// FORK, DESIGN BRANCH COMPLETING FIRST. The same DAG, the other completion
  			// order. Two cases, not one: a walker that serialized the branches would
  			// pass whichever single case matched its accident.
  			name: "fork-join-service-design-first", typeKey: "service",
  			drive: driveServiceForkDesignFirst, wantToday: shapeFailsUntilTheDAG,
  		},
  		{
  			// SEND-BACK RE-OPENS ONLY THE JUDGED PAIR. designReview sends back; the
  			// assertion is that Reopened == ["detailedDesign"] and that stp/srs are NOT
  			// in it — a re-walk that re-dispatched the whole phase list would pass a
  			// naive "it redrafted" assertion.
  			name: "sendback-reopens-only-the-judged-pair", typeKey: "service",
  			drive: driveServiceSendBackJudgedPair, wantToday: shapePassesToday,
  		},
  		{
  			// JOIN WAITS FOR ALL. `testing` dependsOn [integration, stpReview]; the
  			// assertion is that its round does NOT open while either is unpassed.
  			name: "join-waits-for-all", typeKey: "service",
  			drive: driveServiceJoinWaits, wantToday: shapeFailsUntilTheDAG,
  		},
  		{
  			// M0 NO SEND-BACK. The projectDesign lifecycle's one review task: approve
  			// advances the root phase; a send-back is refused, not absorbed.
  			name: "m0-no-sendback", typeKey: "projectDesign",
  			drive: driveM0NoSendBack, wantToday: shapePassesToday,
  		},
  		{
  			// HUMAN FLOOR UNDER VIBES. Preset "vibes" auto-approves a design review and
  			// STILL holds M0 for a human — the non-overridable spend floor
  			// (reviewengine.go:432). One case, both halves.
  			name: "human-floor-under-vibes", typeKey: "requirements",
  			drive: driveVibesFloor, wantToday: shapePassesToday,
  		},
  	}
  }
  ```
  Each `drive*` func: build the rig from the existing scenario constructor, register today's child, start it, drive its signals to the case's terminal, and return `rig.rec.outcome(reopened, phaseAdvanced)`. `driveServiceForkSTPFirst` and `driveServiceForkDesignFirst` differ ONLY in which branch's pipeline observe returns a terminal phase first — **never in the start order, which is declaration order and fixed.** `assertShape`'s two fork rows therefore assert: `stp` and `detailedDesign` BOTH appear in `TaskOrder` before EITHER appears in `CompletedOrder` (the overlap, which is the fork), `TaskOrder` is `[…, detailedDesign, stp, …]` in **both** cases (declaration order, asserted so a future data reorder is caught), and `CompletedOrder` puts the case's named branch first (the discriminator). `driveM0NoSendBack` drives `projectDesignSDPReview` today (Task 9 re-points it) and asserts `PhaseAdvanced == true` after commit, plus that `SDPRejectAll` does not decide the round as `sentBack`.

- [ ] **Step 5: Write the runner, which SKIPS the two failing cases with the task that owes them.**
  ```go
  // Test_LifecycleShapes is the differential oracle: the seven shapes, run against
  // whatever child this commit registers. A shapeFailsUntilTheDAG case is skipped
  // with the task that owes it named in the skip message, so `go test -v` prints the
  // remaining work rather than hiding it.
  func Test_LifecycleShapes(t *testing.T) {
  	for _, c := range lifecycleShapeCases() {
  		t.Run(c.name, func(t *testing.T) {
  			if c.wantToday == shapeFailsUntilTheDAG {
  				t.Skip("asserts the lifecycle DAG's parallelism; the flat phase walk cannot satisfy it — flipped to shapePassesToday by stage 4b1 Task 8")
  			}
  			got := c.drive(t, newShapeRig(t, c.typeKey))
  			assertShape(t, c.name, got)
  		})
  	}
  }
  ```
  `assertShape` holds the per-case expectations as a `switch c.name` with **no `default:` arm that passes** — an unnamed case is a `t.Fatalf`, so adding a case without its assertion cannot go green silently.

- [ ] **Step 6: Create the replay archive's README skeleton**, with the numbers re-measured now while the fixtures are green:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  mkdir -p internal/manager/delivery/testdata/replay-archive
  GOWORK=off go test ./internal/manager/delivery/ -run Test_Replay -count=1 -v 2>&1 | tail -40
  ls internal/manager/delivery/testdata/replay/*/ | cat
  git log -1 --format=%H
  ```
  Expected: three replay tests `ok`, nineteen subtests across seven directories (`design-pre-stage4` 3, `phase2-pre-stage4` 1, `pre-b1` 7, `post-b1` 3, `post-b17` 1, `post-stage3` 2, `pre-d` 2). Write `testdata/replay-archive/README.md` stating: what the nineteen prove (that no durable command moved through stages B1 → 4a on the three pre-4b1 rails); the exact commit they last replayed at (the `git log -1` output above); that they are OUTSIDE the `testdata/replay/*` glob on purpose, so `replayFixtureFiles`'s vacuity guard and `Test_Replay_EveryFixtureDirectoryIsNamed`'s orphan guard do not see them; that the single 3 + 4a + 4b drain is what discharged them (a replay fixture protects an IN-FLIGHT execution from a command-sequence change, and after the drain nothing is in flight on `constructionConstructActivity`, `systemDesignCoAuthor` or `projectDesignCoAuthor`); and that the fresh set captured against `deliveryActivity` lives under `testdata/replay/`. Do **not** move any fixture yet — Task 13 Step 4 does that.

- [ ] **Step 7: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  golangci-lint cache clean
  GOWORK=off make lint fix-check
  GOWORK=off go test ./internal/ -run 'TestFileLayout' -count=1
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_LifecycleShapes|Test_Replay' -count=1 -v 2>&1 | tail -30
  ```
  Expected: `TestFileLayout` green (the new code is in the EXISTING `manager_test.go` — a new `_test.go` file in this package is a `test-file-name` violation); `Test_LifecycleShapes` reports **5 passed, 2 skipped**, each skip naming Task 8; all nineteen replays green. If a shape case marked `shapePassesToday` fails, the case is wrong about today's behaviour — fix the case, not the rail: this task changes no production code.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1
  git add server/internal/manager/delivery/manager_test.go server/internal/manager/delivery/testdata/replay-archive
  git commit -F - <<'MSG'
  test(delivery): the seven lifecycle shapes, written against the rails they replace

  The generic DAG child's hardest failure is a fork that serialises or a
  send-back that re-opens the wrong pair, and nothing in the tree would catch
  either: the current child walks a FLAT phase list, so the DAG in
  lifecycles.json has never executed. Two of these seven cases therefore FAIL
  today, deliberately, and are skipped with the task that owes them named in the
  skip line — `service`'s phase order runs test_plan after detailed_design while
  the data says stp dependsOn [srsReview].

  Writing them now makes them a differential oracle instead of a specification
  written by the same hand as the walker. They reuse the replay scenarios' own
  rig and fakes, so a shape case and a replay fixture exercise one store.

  Also lands the replay ARCHIVE's README, with the nineteen fixtures' meaning and
  their last green commit recorded while they are still green. It sits outside the
  testdata/replay/* glob so neither vacuity guard sees it.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 2: The design-health rules leave the Manager for `designhealth` (R8)

`coauthorartifact.go` carries **724 lines of Method conformance rules** in two blocks — the `view()`-appended kind checks (`:1801-1989`, 189 L: `useCaseActivityFindings`, `useCaseDynamicFindings`, `activityDefect`, `systemLayerDegenerateFindings`, `nameLayerMismatch`, `layerWire`) and the `statevalidationfindings.go` region (`:4585-5119`, 535 L: the `stateValidationFindingGenerators` table at `:4602` with `raOrphanFindings` SYS-RA-ORPHAN, `encapsulatesFindings` SYS-ENCAPSULATES, `relDupFindings` SYS-REL-DUP, `dvTitleFindings` DV-TITLE-EMPTY, `variationRefFindings` UC-VARIATION-REF, `glossaryFourQFindings` GLOSS-FOURQ, `scrubbedIDFindings` SR-ID-UNIQUE, `opcTopicFindings` OPC-TOPIC-COVERAGE, plus `volatilityCoverageFindings`, `servicesExplosionFindings`, `mirroredManagerNames`, `normalizeNameToken`, `componentDisplayLabel`, `modeWire`). They are Method rules living in a Manager, and they are the reason the twin cannot be deleted. They move first.

**R-B is what makes this a move and not a rewrite of the surface:** they do NOT need a new `ActivityView.Findings` field. `designhealth.EvaluateRaw` already reads `slots[n].model` with no status filter, and the rail stages a draft into its slot before the gate opens — so the same rules, as ordinary `designhealth` rules, see the staged draft and surface through the EXISTING `QueryProjectView{designHealth}` view.

**Files:**
- Modify: `server/internal/engine/designhealth/designhealthengine.go` — eight new rules, one new slot absorber (glossary), the rule-id constants.
- Modify: `server/internal/engine/designhealth/engine_test.go` — the parity test and the red mutant.
- Modify: `server/internal/manager/delivery/coauthorartifact.go` — delete both blocks and the `view()` appends that call them.
- Modify: `server/internal/manager/delivery/manager_test.go` — delete the unit tests of the moved generators; keep any that assert the `view()` wire shape.

**Interfaces produced (Task 13 Step 2 depends on these):**
- New rule-id constants in `designhealth`, one per moved rule, spelled EXACTLY as the Manager spelled them: `RuleSysRAOrphan = "SYS-RA-ORPHAN"`, `RuleSysEncapsulates = "SYS-ENCAPSULATES"`, `RuleSysRelDup = "SYS-REL-DUP"`, `RuleDVTitleEmpty = "DV-TITLE-EMPTY"`, `RuleUCVariationRef = "UC-VARIATION-REF"`, `RuleGlossFourQ = "GLOSS-FOURQ"`, `RuleSRIDUnique = "SR-ID-UNIQUE"`, `RuleOPCTopicCoverage = "OPC-TOPIC-COVERAGE"`. Plus `RuleUCActivityMissing`, `RuleUCDynamicMissing`, `RuleSysLayerDegenerate`, `RuleVolCoverage`, `RuleSysServicesExplosion` for the five in the first block that carried no id string.
- `coauthorartifact.go`'s `coAuthorState.view()` no longer appends findings; `SessionStateView.Findings` is served EMPTY from the session and non-empty from `QueryProjectView{designHealth}`.

- [ ] **Step 1: Measure the two blocks and the rule inventory before touching either side.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  sed -n '1801,1989p' internal/manager/delivery/coauthorartifact.go | wc -l
  sed -n '4585,5119p' internal/manager/delivery/coauthorartifact.go | wc -l
  sed -n '1700,1795p' internal/manager/delivery/coauthorartifact.go | grep -n 'Findings'
  grep -on 'Rule[A-Za-z]* *= *"[A-Z][A-Z0-9-]*"' internal/engine/designhealth/designhealthengine.go | wc -l
  grep -n 'func parseSlots' -A 32 internal/engine/designhealth/designhealthengine.go
  grep -n 'func absorbRequirements\|func absorbCoreUseCases\|func absorbVolatilities\|func absorbOperationalConcepts\|func absorbSystemDesign' internal/engine/designhealth/designhealthengine.go
  GOWORK=off go test ./internal/engine/designhealth/ -count=1 -v 2>&1 | tail -20
  ```
  Expected: 189 and 535 lines; six `Findings` appends inside `view()` at relative lines 16/22/31/34/43 plus the struct field at 78; **44** existing rule ids; `parseSlots` at `:602` dispatching six `absorb*` per slot kind with **no glossary absorber**; the designhealth suite green including `TestGreenFixtureAdvisoriesFire` (`engine_test.go:49`), `TestNegativeFixturesEachRuleFires` (`:265`) and `TestCC_AllRulesAreErrorSeverity` (`:240`). Record the 44 → 57 arithmetic (44 + 13 moved) and confirm it against the grep at Step 6; if the measured count is not 57, a rule was dropped or double-declared.

- [ ] **Step 2: Add the glossary absorber**, the one input the moved rules need and `parseSlots` does not have. In `designhealthengine.go`, beside the existing absorbers:
  ```go
  // kindGlossary is the Glossary slot's ArtifactKind ordinal. GLOSS-FOURQ (moved out
  // of the Manager in stage 4b1) is the first rule to read it, so this is the first
  // absorber for it; the ordinal is wire-frozen, so it is safe to name by number here
  // exactly as its five siblings are.
  const kindGlossary = 1

  // absorbGlossary decodes the Glossary slot's terms tolerantly — the same discipline
  // every absorber follows: a missing or malformed slot yields an empty section rather
  // than an error, because a rule must be able to say "no glossary" and a parse
  // failure on one slot must not blind the other forty-four rules.
  func absorbGlossary(model json.RawMessage, out *slotData) {
  	var m struct {
  		Terms []struct {
  			Term       string `json:"term"`
  			Definition string `json:"definition"`
  			Question   string `json:"question"`
  		} `json:"terms"`
  	}
  	_ = json.Unmarshal(model, &m)
  	for _, t := range m.Terms {
  		out.GlossaryTerms = append(out.GlossaryTerms, glossaryTerm{
  			Term: t.Term, Definition: t.Definition, Question: t.Question,
  		})
  	}
  }
  ```
  - [ ] **Verify first:** read `projectstate.Glossary`'s Go struct (`grep -n '^type Glossary' -A 12 internal/resourceaccess/projectstate/contract.gen.go`) and make the tolerant shape above name the SAME JSON keys the committed slot carries. Then add `case kindGlossary: absorbGlossary(slot.Model, &out)` to `parseSlots`' switch, and `GlossaryTerms []glossaryTerm` + the `glossaryTerm` type to `slotData`. Confirm with `GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System` that the count of findings does not change yet — an absorber with no rule reading it must be inert.

- [ ] **Step 3: Move the eight `statevalidationfindings.go` rules**, one at a time, in the `stateValidationFindingGenerators` table's order (`raOrphanFindings`, `encapsulatesFindings`, `relDupFindings`, `dvTitleFindings`, `variationRefFindings`, `glossaryFourQFindings`, `scrubbedIDFindings`, `opcTopicFindings`). For each: copy the body into `designhealthengine.go` as a rule over `slotData`, keep the rule id string and the message text **byte-identical**, re-type `projectstate.X` to the engine's own absorbed shape, and delete the Manager copy in the same edit. The severity is the one the Manager's `Finding` carried — `TestCC_AllRulesAreErrorSeverity` (`engine_test.go:240`) walks the set, so read that test before choosing, and do not "upgrade" an advisory to an error to make a table tidy.
  - [ ] **Verify after EACH rule** (thirteen times; this is the whole point of moving them one at a time):
    ```bash
    cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
    GOWORK=off go build ./... && GOWORK=off go test ./internal/engine/designhealth/ -count=1
    GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System 2>&1 | tail -5
    ```
    Expected: build and suite green, and the `validate` advisory/error counts **unchanged from the 43 advisory / 0 errors baseline** for every rule that finds nothing on this repo's committed state. A rule whose move CHANGES the count on the committed state has been re-typed wrong — the Manager copy and the engine copy must agree on this repo's state before either is trusted anywhere else.

- [ ] **Step 4: Move the five first-block generators** (`useCaseActivityFindings`, `useCaseDynamicFindings`, `systemLayerDegenerateFindings` with its `nameLayerMismatch`/`layerWire` helpers, `volatilityCoverageFindings`, `servicesExplosionFindings`), with the same one-at-a-time verification. Note the two that took committed CONTEXT as a third parameter — `useCaseDynamicFindings(kind, draft, committedCoreUseCases)` and `servicesExplosionFindings(kind, draft, committedCoreUseCases)`, plus `volatilityCoverageFindings(kind, draft, committedVolatilities)`. In the engine they need no such parameter: `slotData` carries every slot at once, which is the structural reason this move is a simplification and not a port. Record that sentence in the moved rules' doc comments.

- [ ] **Step 5: Delete the Manager's side.** In `coauthorartifact.go`: delete both blocks and the six `Findings` appends in `coAuthorState.view()` (`:1703-1790`), leaving `SessionStateView.Findings` served as the nil-when-empty wire form it already documents. Delete `stateValidationFindingGenerators`. In `manager_test.go`: delete the unit tests of the thirteen moved generators; KEEP any test that asserts `view()`'s wire shape, and add one assertion that `view().Findings` is now empty with the reason ("the Method rules moved to designhealth in stage 4b1 and surface through QueryProjectView{designHealth}"), so the emptiness is a stated fact rather than an untested regression.

- [ ] **Step 6: Add the parity test and its red mutant** to `engine_test.go`:
  ```go
  // TestMovedManagerRulesFireIdenticallyOnTheGreenFixture is the acceptance for the
  // stage-4b1 move of thirteen Method rules out of the delivery Manager. The green
  // fixture is the committed project.json, where every one of the thirteen must be
  // SILENT — that is what "green" means — and one hand-built red mutant per rule
  // must make exactly that rule, and no other, fire.
  //
  // Mutation is the whole test. A rule copied with its condition inverted, or with a
  // projectstate type re-typed onto the wrong absorbed field, is SILENT on the green
  // fixture too; only the mutant separates "moved correctly" from "moved dead".
  func TestMovedManagerRulesFireIdenticallyOnTheGreenFixture(t *testing.T) {
  	moved := []string{
  		RuleSysRAOrphan, RuleSysEncapsulates, RuleSysRelDup, RuleDVTitleEmpty,
  		RuleUCVariationRef, RuleGlossFourQ, RuleSRIDUnique, RuleOPCTopicCoverage,
  		RuleUCActivityMissing, RuleUCDynamicMissing, RuleSysLayerDegenerate,
  		RuleVolCoverage, RuleSysServicesExplosion,
  	}
  	raw := readCommittedProjectJSON(t)
  	green := indexByRule(EvaluateRaw(raw))
  	for _, id := range moved {
  		if len(green[id]) != 0 {
  			t.Errorf("%s fires on the committed state; the Manager copy was silent there, so the move changed behaviour: %v", id, green[id])
  		}
  	}
  	for _, id := range moved {
  		t.Run(id, func(t *testing.T) {
  			mutant := mutantFor(t, raw, id)
  			got := indexByRule(EvaluateRaw(mutant))
  			if len(got[id]) == 0 {
  				t.Fatalf("%s did not fire on its own mutant — the moved rule is dead", id)
  			}
  			for other, fs := range got {
  				if other != id && len(green[other]) == 0 && len(fs) > 0 {
  					t.Errorf("%s's mutant also fired %s (%v); the mutant is too broad to attribute", id, other, fs)
  				}
  			}
  		})
  	}
  }
  ```
  - [ ] **Verify first:** `indexByRule` and `readCommittedProjectJSON` — reuse the existing helpers (`indexBySeverity` is at `engine_test.go:52`'s call site; add `indexByRule` beside it only if no rule-keyed indexer exists). `mutantFor` builds the minimal `json` edit that trips ONE rule, and the oracle for each is that rule's own condition — thirteen mutants, one per moved rule: blank a dynamic view's `title` (DV-TITLE-EMPTY); point a `variations[].parent` at a missing id (UC-VARIATION-REF); drop a term's `question` (GLOSS-FOURQ); duplicate a scrubbed-requirement `id` (SR-ID-UNIQUE); add a `resourceAccess` component named by no relationship (SYS-RA-ORPHAN); blank a component's `encapsulates` (SYS-ENCAPSULATES); duplicate a relationship pair (SYS-REL-DUP); delete an operational-concepts topic (OPC-TOPIC-COVERAGE); null a core use case's `activity` (UC-ACTIVITY-MISSING); delete a use case's dynamic view (UC-DYNAMIC-MISSING); re-label a `manager` component's layer to `engine` while keeping its `…Manager` name (SYS-LAYER-DEGENERATE); remove a volatility's last `encapsulatesVolatilities` reference (VOL-COVERAGE); add a ninth `service`-suffixed component (SYS-SERVICES-EXPLOSION). Each must `t.Fatalf` — never silently return the input — when it cannot find its target in the committed state, or the subtest passes for the wrong reason.

- [ ] **Step 7: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  grep -c 'Rule[A-Za-z]* *= *"[A-Z][A-Z0-9-]*"' internal/engine/designhealth/designhealthengine.go
  GOWORK=off go test ./internal/engine/designhealth/ -count=1 -v 2>&1 | tail -30
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System 2>&1 | tail -5
  GOWORK=off go test -short -count=1 ./...
  golangci-lint cache clean && GOWORK=off make lint fix-check method-check
  GOWORK=off go test ./internal/ -run 'TestFileLayout|TestMethodLayering|TestBuildStatusVocabulariesAgree' -count=1
  wc -l internal/manager/delivery/coauthorartifact.go internal/engine/designhealth/designhealthengine.go
  ```
  Expected: **57** rule ids; the whole designhealth suite green including the new parity test's thirteen subtests; `validate --slot System` still **43 advisory / 0 errors** (the move is behaviour-preserving on this state, which is the acceptance); `coauthorartifact.go` down from 5,119 to ≈4,395; `designhealthengine.go` up from 2,666 by ≈724 minus the re-typing. `TestBuildStatusVocabulariesAgree` matters here: it names `"engine/designhealth/designhealthengine.go"` as a **string literal** (`internal/arch_test.go:219`), so this task must not split that file.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1
  git add server/internal/engine/designhealth server/internal/manager/delivery
  git commit -F - <<'MSG'
  refactor(designhealth): thirteen Method rules leave the Manager

  724 lines of Method conformance rules lived in coauthorartifact.go — eight
  state-validation rules with real ids (SYS-RA-ORPHAN, SYS-ENCAPSULATES,
  SYS-REL-DUP, DV-TITLE-EMPTY, UC-VARIATION-REF, GLOSS-FOURQ, SR-ID-UNIQUE,
  OPC-TOPIC-COVERAGE) and five kind checks with none. They are Method rules in a
  Manager, and they are the reason the co-author twin could not be deleted.

  They need no new surface. designhealth's EvaluateRaw already reads slots[n].model
  with no status filter, and the rail stages a draft into its slot before its gate
  opens — so as ordinary engine rules they see the same staged draft the session
  showed and surface through QueryProjectView{designHealth}. The three generators
  that took committed context as a third parameter need no parameter at all: the
  engine's slotData carries every slot at once.

  Moved one rule at a time, with `validate --slot System` re-run after each: 43
  advisory / 0 errors before and after. Pinned by a parity test that requires each
  moved rule SILENT on the committed state and firing on its own red mutant, and
  nothing else firing on that mutant — a rule copied with an inverted condition is
  silent on green too, so the mutant is the only thing that separates moved from
  dead.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 3: `ReviewRound` carries the artifact kind as a FIELD (entry criterion)

The stage-3 entry criterion, verbatim: *"once stage 4 records DESIGN attempts, two kinds' round 1 would bind one gate attempt — add the artifact kind as a FIELD on the round (today it lives only inside the 4-part id, which nothing may parse)."* Measured: `ReviewRound` (`projectstate/contract.gen.go:771-785`) and `ReviewRoundInput` (`:787-794`) carry `RoundID, TaskID, Reviews, Round, SubjectRef, Reviewers …` and **no kind**. The kind lives only inside the design rails' 4-part `RoundID` (`designRoundID`, `coauthorartifact.go:3909`), and `ReviewRoundInput.roundId`'s own contract text says outright that **nothing may parse a RoundID** — joins use `(TaskID, Round)`.

**The justification, re-grounded on v0.9.0 as it actually is** (the first draft of this plan cited `glossary` + `scrubbedRequirements` sharing the `glossary` phase; measured, **v0.9.0 carries no `scrubbedRequirements` task and no `ScrubbedRequirements` artifact kind at all** — the eleven kinds present are Construction, CoreUseCases, DetailedDesign, Glossary, Integration, Mission, SdpReview, SRS, STP, System, Volatilities — so no lifecycle produces two kinds on one gate task *today*). Two reasons the field is needed anyway, both measured:

1. **A review task's id does not determine what it judges.** `designReview` appears in **eight** lifecycles (deployment, documentation, frontend, service, testing:harness, testing:perf, testing:qaProcess, uiDesign), `testing` and `codeReview` in **nine** each, `srsReview` in **five**, `stpReview` in two. Every one of them carries `artifactKind: ""` and resolves its subject through `reviews` → that task's kind — so a stored round keyed `(taskId, round)` is a question with up to nine answers, and the reader can only resolve it by re-fetching the activity's lifecycle. **The field is what makes the stored round self-describing**, which is the property a ledger is for.
2. **The store's own contract already states the collision as a design fact**, and the three kinds it names are exactly GAP-4B-4's orphans: `roundId`'s description says the design rails mint a four-part id *"because several artifact kinds share one lifecycle phase (glossary and scrubbedRequirements; system, operationalConcepts and standardCheck)"*. Those three kinds have committed slots, live `DesignCommandFor` slugs and **no lifecycle task** (Task 15 Step 2 carries all three as the founder question). The moment the founder resolves GAP-4B-4 by giving `operationalConcepts` a task on the `architecture` lifecycle — the likeliest resolution — `architectureReview` judges two kinds and the collision is live in one method-assets release, against a child that is the single writer for both.

Additive `$defs` change on both types, optional on the wire (an absent kind is a construction round, which has no artifact kind of its own), plus the two writers and the join.

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts.activityExecutionAccess.$defs.ReviewRound.properties` and `.…ReviewRoundInput.properties`.
- Regenerate: `server/internal/resourceaccess/projectstate/contract.gen.go` + every generated surface the loop touches.
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — `OpenReviewRound` carries the kind onto the stored row.
- Modify: `server/internal/manager/delivery/constructactivity.go` (`openGateRound`) and `coauthorartifact.go` / `coauthorphase2artifact.go` (`openDesignRound` and its pd twin) — the writers.
- Modify: `server/internal/manager/delivery/deliverymanager.go` — the round→revision join.
- Modify: `server/internal/manager/delivery/manager_test.go` — the two-kinds-one-gate case.

**Interfaces produced (Tasks 8, 9, 10 and 11 all write this field):**
- `projectstate.ReviewRound.ArtifactKind *ArtifactKind` (JSON `artifactKind`, optional).
- `projectstate.ReviewRoundInput.ArtifactKind *ArtifactKind` (JSON `artifactKind`, optional).
- `func roundGateKey(taskID projectstate.MethodTask, kind *projectstate.ArtifactKind) string` — the GATE identity, called by Task 6's `strandedRounds` and Task 12's `latestRoundFor`.
- `func roundJoinKey(r projectstate.ReviewRound) string` — `roundGateKey` plus the round number; the REVISION identity.
- The join rule, stated once and in the code: rounds group by **`(TaskID, ArtifactKind, Round)`** where the kind is present, and by `(TaskID, Round)` where it is absent.
- The RESOLUTION rule Task 8 Step 4 implements: a round's `ArtifactKind` is `lifecycleTaskByID(lc, t.Reviews).ArtifactKind` — measured, no review task in v0.9.0 carries its own except `sdpReview`.

- [ ] **Step 1: Prove the collision exists before fixing it.** Add a failing case to `manager_test.go` first:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  GOWORK=off python3 - <<'PY'
import json
d=json.load(open('../.aiarch/state/project.json'))
defs=d['serviceContracts']['activityExecutionAccess']['$defs']
print('ReviewRound props:', list(defs['ReviewRound']['properties'].keys()))
print('ReviewRoundInput props:', list(defs['ReviewRoundInput']['properties'].keys()))
print('required:', defs['ReviewRound']['required'])
PY
  ```
  Expected: `roundId, taskId, reviews, round, subjectRef, reviewers, verdicts, thread, outcome, decidedBy, openedAt, decidedAt, provenance` and `roundId, taskId, reviews, round, subjectRef, reviewers` — **no `artifactKind` in either**, and the `ReviewRound.required` list ending `…outcome, openedAt, decidedBy, decidedAt, provenance`.
  Then write the two cases — one that reproduces the join defect at the store, and one that pins the resolution rule the real data forces:
  ```go
  // Test_ReviewRounds_TwoKindsOnOneGateDoNotBindOneAttempt is the stage-3 entry
  // criterion made executable. Two rounds numbered 1 on ONE review task, judging
  // DIFFERENT artifact kinds, must read as TWO revisions.
  //
  // The four-part design RoundID keeps them distinct ROWS — nothing collides in
  // storage — but every read path joins on (TaskID, Round), because
  // ReviewRoundInput.roundId's own contract says nothing may parse a RoundID. So the
  // two rows join as one revision and the reader is shown one kind judged by the
  // other's verdict.
  //
  // It is driven at the STORE rather than through a lifecycle, deliberately: v0.9.0
  // carries no lifecycle that puts two kinds on one gate task (measured), so a
  // lifecycle-driven case would be asserting a shape the data cannot currently
  // produce. What the store must guarantee is that when it CAN — one method-assets
  // release after GAP-4B-4 gives operationalConcepts a task on `architecture` — the
  // ledger is already honest. A defence written after the data arrives is a
  // migration; written before, it is a contract.
  func Test_ReviewRounds_TwoKindsOnOneGateDoNotBindOneAttempt(t *testing.T) { … }

  // Test_ReviewRounds_KindComesFromTheJudgedTask pins the resolution rule the real
  // data forces (and which Task 8 Step 4 implements): NO review task in v0.9.0
  // carries an artifactKind of its own except `sdpReview`, so a round's kind is
  // lifecycleTaskByID(lc, t.Reviews).ArtifactKind. Measured spread: `designReview`
  // appears in 8 lifecycles, `testing` and `codeReview` in 9, `srsReview` in 5 —
  // so a reader that resolved the kind from the TASK ID would be guessing between
  // up to nine answers.
  func Test_ReviewRounds_KindComesFromTheJudgedTask(t *testing.T) { … }
  ```
  Drive the first through the store fake: open round 1 of `architectureReview` for `KindSystem`, then round 1 of the same task for `KindOperationalConcepts`, then read the activity's revisions and assert **two** revisions of that gate with distinct kinds. Drive the second over all fourteen lifecycles as a table, asserting that every review task resolves a non-empty kind through `reviews` except `sdpReview`, which resolves its own. Expected: the first is **RED** before Step 2, with the failure printing one revision; the second is green as a data assertion from the moment it is written.

- [ ] **Step 2: Add the field to both `$defs`.** Insert `artifactKind` into `ReviewRound.properties` immediately after `reviews` (the JSON key order matters — it mirrors the Go field order the generator emits, and the surrounding idiom groups identity before subject):
  ```json
  "artifactKind": {
    "$ref": "#/$defs/ArtifactKind",
    "pointer": true,
    "description": "The artifact this round judges, when the round is about one. PRESENT on a design round and ABSENT on a construction round, whose subject is a commit rather than a slot model. It is a FIELD for two reasons. A review task's ID does not determine what it judges: `designReview` names a task in eight lifecycles and `testing` in nine, each carrying no artifactKind of its own and resolving its subject through `reviews`, so a round keyed (taskId, round) alone can only be resolved by re-fetching the activity's lifecycle. And several artifact kinds are designed to share one lifecycle phase — the four-part design RoundID exists for exactly that — so the day one gate task judges two kinds, a reader that joins on (taskId, round) binds both into one revision. The kind is ALSO inside that RoundID, and nothing may parse a RoundID: this is the field that makes the join honest."
  },
  ```
  and the same block, with the description trimmed to its first two sentences plus "The caller supplies it; the store stores it verbatim.", into `ReviewRoundInput.properties` after `reviews`. **Do NOT add it to either `required` list** — `required` is PRESENCE-only and a construction round legitimately has no kind.
  - [ ] Run the self-amendment loop (Global Constraints) with `--slot System` and `--slot ActivityList` omitted (no slot model changed; only `.serviceContracts`), i.e. `--slot System` alone. Expected: `43 advisory / 0 errors` unchanged; `ArtifactKind` already exists in this contract's `$defs` (verified: `serviceContracts.projectStateAccess.$defs.ArtifactKind` is the 17-value enum, and `activityExecutionAccess` shares the component's `$defs` namespace) — if `make gen-models` reports an unresolved `$ref`, the `$defs` block that owns `ArtifactKind` is the projectStateAccess facet's and the ref must be added to `activityExecutionAccess.$defs` as a sibling, copied byte-identically, never re-numbered.

- [ ] **Step 3: Carry it through the store.** In `projectstateaccess.go`'s `OpenReviewRound`, set the stored row's `ArtifactKind` from the input's, and state the write-once rule:
  ```go
  // The kind is WRITE-ONCE with the round, like every other identity fact on it: a
  // round is opened once per (task, kind, n) and re-opening the same RoundID opens
  // ONE round, so a second open carrying a different kind is the caller contradicting
  // itself about what this round judges.
  if r.ArtifactKind == nil {
  	r.ArtifactKind = round.ArtifactKind
  } else if round.ArtifactKind != nil && *r.ArtifactKind != *round.ArtifactKind {
  	return execMisuse("OpenReviewRound", fmt.Sprintf(
  		"round %s already judges %s and cannot be re-opened judging %s", round.RoundID, *r.ArtifactKind, *round.ArtifactKind))
  }
  ```
  - [ ] **Verify first:** read `OpenReviewRound`'s body (`grep -n 'func (a \*activityExecutionAccess) OpenReviewRound' -A 45 internal/resourceaccess/projectstate/projectstateaccess.go`) and place the block inside the SAME `upsert`-shaped closure the idempotent re-open already runs in, using that closure's own variable names. Do not introduce a second lookup.

- [ ] **Step 4: Set it at all three writers.** `openGateRound` (`constructactivity.go:1980`) leaves it **nil** with a one-line reason ("construction stages no slot model: its subject is the commit `gateSubjectRef` names"). `openDesignRound` (`coauthorartifact.go:4080`) and its pd twin set `ArtifactKind: &kind` from the session's `state.artifactKind`. In `deliverymanager.go`, add the gate identity and the revision identity as **two functions at two arities over ONE rule** — the split is mandatory, not stylistic: Task 6's sweep needs the GATE identity without a round number, and synthesising a zero-`Round` `ReviewRound` to get it would make the sweep depend on `roundJoinKey`'s suffix being constant, which is true by accident and not by contract:
  ```go
  // roundGateKey is the identity of a GATE: which review task, judging which artifact.
  // A review task's id is not enough on its own — `designReview` names a task in eight
  // lifecycles and `testing` in nine, each resolving its subject through `reviews` — and
  // several kinds are designed to share one lifecycle phase, which is why the design
  // rails' RoundID is four-part. A construction round has no kind and keys on the task
  // alone, exactly as it always did.
  //
  // Two arities over one rule, deliberately: the stranded-round sweep needs the gate
  // identity WITHOUT a round number, and asking for it by synthesising a zero-Round
  // ReviewRound would depend on roundJoinKey's suffix being constant — true by accident.
  func roundGateKey(taskID projectstate.MethodTask, kind *projectstate.ArtifactKind) string {
  	if kind == nil {
  		return string(taskID)
  	}
  	return string(taskID) + ":" + kind.WireName()
  }

  // roundJoinKey is the identity a REVISION groups rounds by: the gate, plus the round
  // number. This is the fix for the stage-3 entry criterion "two kinds' round 1 would
  // bind one gate attempt"; nothing here parses a RoundID.
  func roundJoinKey(r projectstate.ReviewRound) string {
  	return roundGateKey(r.TaskID, r.ArtifactKind) + ":" + strconv.FormatInt(r.Round, 10)
  }
  ```
  - [ ] **Verify first:** grep every place a round is grouped or matched to an attempt — `grep -n 'TaskID == \|\.Round == \|normalizeAttempts\|revisionProvenance\|slotRounds\|designRounds' internal/manager/delivery/deliverymanager.go | grep -v '^.*_test'` — and route each through `roundJoinKey`. A site left on the raw pair is the defect surviving its own fix.

- [ ] **Step 5: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_ReviewRounds_|Test_Replay|Test_LifecycleShapes' -count=1 -v 2>&1 | tail -30
  GOWORK=off go test ./internal/resourceaccess/projectstate/ -count=1
  GOWORK=off go test -short -count=1 ./...
  # the full drift block (Global Constraints), then:
  cd ../systemtests && GOWORK=off go build ./...
  ```
  Expected: the new case GREEN (two revisions, distinct kinds); all nineteen replays still green — **this is the load-bearing assertion of the task**: an optional field added to a struct a workflow passes to an Activity changes no durable command, so a red replay here means a writer was moved rather than widened; `npm run check` green with no `OUTPUT_NAMES` diff; systemtests still builds.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1
  git add .aiarch/state/project.json server webApp
  git commit -F - <<'MSG'
  feat(rail): a review round says which artifact it judges

  Stage-3 entry criterion. A review task's ID does not determine what it judges:
  `designReview` names a task in EIGHT lifecycles and `testing` in nine, every one
  of them carrying no artifactKind of its own and resolving its subject through
  `reviews`. So a stored round keyed (taskId, round) is a question with up to nine
  answers, and the only way to resolve it is to re-fetch the activity's lifecycle —
  which is not what a ledger is for.

  And the collision the store's own contract already names is one release away.
  ReviewRoundInput.roundId documents the four-part design RoundID as existing
  "because several artifact kinds share one lifecycle phase (glossary and
  scrubbedRequirements; system, operationalConcepts and standardCheck)" — and those
  three kinds have committed slots, live draft commands and no lifecycle task
  between them. Give operationalConcepts a task on `architecture`, which is the
  likeliest resolution of that open question, and architectureReview judges two
  kinds against a child that is the single writer for both.

  artifactKind becomes an optional FIELD on ReviewRound and ReviewRoundInput —
  present on a design round, absent on a construction round, whose subject is a
  commit and not a slot model. roundGateKey is the gate identity and roundJoinKey
  the revision identity: two arities over one rule, because the stranded-round sweep
  needs the gate without a round number and must not get it by synthesising a zero.
  Write-once with the round, because a second open claiming a different kind is the
  caller contradicting itself.

  A defence written after the data arrives is a migration; written before, it is a
  contract.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 4: `RoundWithdrawn` gets its own wire member (entry criterion)

Measured: the stored vocabulary has four values (`projectstate/contract.gen.go:797-803`: `pending`, `passed`, `sentBack`, `withdrawn`), and the wire's `TaskRevisionOutcome` (`.serviceContracts.deliveryManager.$defs`) has six (`running`, `awaitingHuman`, `passed`, `sentBack`, `failed`, `skipped`) with **no `withdrawn`** — so `roundOutcome` (`deliverymanager.go:11082`) collapses a withdrawal to `revFailed`, and its own earmark (`:11071-11081`) says why that is wrong: *"Failed overstates the drama (a withdrawal is deliberate, not a fault)"*. R7 needs the member for real, because construction withdraw is one of the five paths Task 12 turns on, and a construction reviewer who withdraws a round must not see it rendered as a failure.

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts.deliveryManager.$defs.TaskRevisionOutcome`.
- Regenerate: the delivery contract, the OAS, `webApp/src/contracts/*`, `enums.gen.ts`.
- Modify: `server/internal/manager/delivery/deliverymanager.go` — `roundOutcome` + the `rev*` const block.
- Modify: `webApp/src/components/activity/*` — the outcome→label/icon maps that switch on the vocabulary.
- Modify: `server/internal/manager/delivery/manager_test.go`, `webApp/src/components/activity/*.test.ts`.

**Interfaces produced (Task 12 and Task 14 depend on these):**
- Wire value `withdrawn`, appended LAST, `x-enum-varname` `TaskRevisionWithdrawn`.
- Go const `revWithdrawn = "withdrawn"` beside the existing `rev*` block.

- [ ] **Step 1: Read the existing enum and every reader before appending.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  python3 -c "
import json;d=json.load(open('../.aiarch/state/project.json'))
print(json.dumps(d['serviceContracts']['deliveryManager']['\$defs']['TaskRevisionOutcome'],indent=1))"
  grep -n 'revPassed\|revSentBack\|revFailed\|revSkipped\|revRunning\|revAwaitingHuman' internal/manager/delivery/deliverymanager.go | grep -v '_test'
  cd ../webApp && grep -rn "'sentBack'\|'awaitingHuman'\|'skipped'" src/components/activity/ src/contracts/ | grep -v '\.test\.' | head -20
  ```
  Expected: the six-value enum with its six `x-enum-varnames`; the `rev*` consts and their readers in `roundOutcome` plus the attempt-side derivations; and the SPA's switch sites. Record every SPA site — a TS union gaining a member makes an exhaustive `switch` a compile error, which is the gate doing its job, and the list is what Step 4 walks.

- [ ] **Step 2: Append the member.** `withdrawn` LAST in `enum`, `TaskRevisionWithdrawn` LAST in `x-enum-varnames` — appended, never inserted, because these are wire-visible ordinals in a string enum whose ORDER the generator emits and whose consumers iterate:
  ```json
  "enum": ["running", "awaitingHuman", "passed", "sentBack", "failed", "skipped", "withdrawn"],
  "x-enum-varnames": ["TaskRevisionRunning", "TaskRevisionAwaitingHuman", "TaskRevisionPassed", "TaskRevisionSentBack", "TaskRevisionFailed", "TaskRevisionSkipped", "TaskRevisionWithdrawn"],
  ```
  Add a `description` if the def has none: *"A revision's rendered outcome. `withdrawn` is a round pulled back before anyone decided it — deliberate, not a fault, which is why it is not `failed`; `skipped` is a gate the policy did not hold for a human."*
  - [ ] Run the self-amendment loop. Expected: `43 advisory / 0 errors`; `npm run gen:api && npm run gen:ops` produce the widened union; `npm run check` **RED** at every exhaustive SPA switch — that redness is the reader inventory Step 4 fixes, and a green `check` at this point means the SPA is switching with a `default:` somewhere it should not be.

- [ ] **Step 3: Stop collapsing it on the server.** In `deliverymanager.go`, add `revWithdrawn = "withdrawn"` to the `rev*` const block and change the one arm:
  ```go
  	case projectstate.RoundWithdrawn:
  		return revWithdrawn
  ```
  and **delete the EARMARK paragraph** from `roundOutcome`'s doc comment (`:11071-11081`), replacing it with what is now true:
  ```go
  // roundOutcome renders a stored round outcome as a revision outcome. Total over the
  // vocabulary with no default arm.
  //
  // RoundWithdrawn has its own wire member from stage 4b1: a round pulled back before
  // anyone decided it is deliberate, and `failed` said the revision faulted. The
  // construction rail gains a withdraw verb in the same wave (spec §7.2's fifth refused
  // path), so this stopped being a design-rail-only rendering question.
  //
  // Its neighbour, the STRANDED pending round, is closed by the sweep in the same wave:
  // a pending round with no live session and a later round on the same gate is stamped
  // RoundWithdrawn rather than rendering `running` forever.
  ```

- [ ] **Step 4: Fix every SPA reader `npm run check` named.** Each site gets a `withdrawn` arm with its own copy — a withdrawn revision reads as withdrawn, never as failed — and the copy string goes in `UI_IDENTIFIERS` / `activityCopy.ts` beside its siblings, matching their voice. Invert the node tests that asserted `failed` for a withdrawal rather than deleting them; add one asserting `withdrawn` renders as itself.

- [ ] **Step 5: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Replay|roundOutcome|RoundWithdrawn' -count=1
  GOWORK=off go test -short -count=1 ./...
  # the full drift block, then:
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && ASDF_NODEJS_VERSION=lts npx playwright test --project=preview 2>&1 | tail -10
  cd ../systemtests && GOWORK=off go build ./...
  ```
  Expected: nineteen replays green (a rendering vocabulary is not a durable command); `npm run check` green with no `OUTPUT_NAMES` diff in `git status`; preview 44/44 — no fixture carries a withdrawn revision yet, so the suite must be unchanged, and a preview failure here means a switch was made non-exhaustive instead of widened.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1
  git add .aiarch/state/project.json server webApp
  git commit -F - <<'MSG'
  feat(rail): a withdrawn round reads as withdrawn, not as failed

  Stage-3 entry criterion. The STORE has carried RoundWithdrawn since stage 3; the
  WIRE never had a name for it, so roundOutcome collapsed it to `failed` and its own
  doc comment said why that was wrong: a withdrawal is deliberate, and `failed`
  claims the revision faulted.

  It mattered less while withdraw was design-rail-only. Stage 4b1 gives the
  construction rail a withdraw verb (spec §7.2's fifth refused path), so a
  construction reviewer who pulls a round back would have been shown a red
  revision for doing the thing the button offered.

  `withdrawn` is APPENDED to TaskRevisionOutcome, never inserted. Widening the union
  turned every exhaustive SPA switch red, which is exactly the reader inventory this
  change needed, and each one gained its own copy line rather than a default arm.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 5: `gateSubjectRef` names the commit; `OpenActivity` refuses a re-open that re-types the row

Two carried review findings, one commit, because both are fixes the generic child's SINGLE review-round writer and SINGLE row opener would otherwise be touched for twice.

**(a) `gateSubjectRef`** (`constructactivity.go:2027-2032`, a free function — the recon's "method" is wrong):
```go
func gateSubjectRef(gf *gitForward, workAttemptID string) projectstate.SubjectRef {
	if gf.enabled && gf.prRef != "" {
		return projectstate.SubjectRef{Kind: projectstate.SubjectPullRequest, Ref: gf.prRef}
	}
	return projectstate.SubjectRef{Kind: projectstate.SubjectArtifact, Ref: workAttemptID}
}
```
The pull request is per-ACTIVITY, so every round on the rail cites the same `prRef` and the ledger cannot say which revision each round judged. `SubjectCommit` already exists in the vocabulary (`projectstate/contract.gen.go:921-926`) and is unused. The design side has **one** counterpart — `designSubjectRef(gf gitSession, kind ArtifactKind)` (`coauthorartifact.go:4021`), a DIFFERENT signature over the same defect; **there is no projectDesign twin** (`grep -n 'SubjectRef{' coauthorphase2artifact.go` → zero hits), so this task touches two functions, not three.

**(b) `OpenActivity`'s re-open** (`projectstateaccess.go:10087-10088`): `cs.Type = typ` and `cs.Variant = variant`, unconditional. The guard above protects `Pin` (`:10079-10087`) and `StartedAt` (`:10089-10095`) and the terminality refusal protects an exited row (`:10060-10066`), but a re-open with a DIFFERENT type silently re-types the row — and the row's `Type` is what `ResolveConstructionRow` resolves every read-time derivation against, so a re-typed row retro-dates every attempt to a lifecycle it was never written under. That is the same class of lie the pin's write-once rule already refuses, one field over.

**Files:**
- Modify: `server/internal/manager/delivery/constructactivity.go` — `gateSubjectRef` + its caller's inputs.
- Modify: `server/internal/manager/delivery/coauthorartifact.go` — `designSubjectRef` (the ONLY design counterpart; `coauthorphase2artifact.go` has none).
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — `OpenActivity`.
- Modify: `server/internal/manager/delivery/manager_test.go`, `server/internal/resourceaccess/projectstate/access_test.go`.

**Interfaces produced (Tasks 8–11 call the first; Task 10 depends on the second's refusal):**
- `func gateSubjectRef(gf *gitForward, stagedRef, workAttemptID string) projectstate.SubjectRef` — three parameters; the staged ref wins, then the PR, then the attempt.
- `OpenActivity` returns `fwra.ContractMisuse` naming both types when a live row is re-opened as a different `(typ, variant)`.

- [ ] **Step 1: Find the staged ref the round should cite.** The subject of a review is what the work task PRODUCED, and stage 3 already returns it: `activityExecutionAccess.StageTaskOutput` answers a `StagedRef{ActivityID, TaskID, Branch, Version}` (`contract.gen.go:910-915`).
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  grep -n 'StagedRef' internal/resourceaccess/projectstate/contract.gen.go
  grep -rn 'ActivityExecutionStageTaskOutput' internal/manager/delivery/*.go | grep -v manager_test
  grep -n 'func gateSubjectRef' -B 8 -A 10 internal/manager/delivery/constructactivity.go
  grep -n 'func designSubjectRef' -A 14 internal/manager/delivery/coauthorartifact.go
  grep -cn 'SubjectRef{' internal/manager/delivery/coauthorphase2artifact.go
  ```
  Expected: `StagedRef` with four fields; **zero** production callers of `ActivityExecutionStageTaskOutput` (stage 3 wired the verb and nothing calls it — the 4a earmark says so, and Tasks 9–11 are its first callers); `gateSubjectRef` at `:2027` with its doc at `:2021-2026`; `designSubjectRef(gf gitSession, kind ArtifactKind)` at `:4021` — a different parameter list over the same defect; and **0** in `coauthorphase2artifact.go`, confirming there is no projectDesign twin to change. Because there is no staged ref on the construction path **yet**, this task widens the signature and threads a value that is empty until Task 11 fills it — state that in the doc comment rather than leaving the parameter unexplained.

- [ ] **Step 2: Rewrite `gateSubjectRef`.**
  ```go
  // gateSubjectRef names WHAT this round judges — and it must differ from round to
  // round, or the ledger cannot say which revision each round looked at.
  //
  // THE DEFECT THIS REPLACES: the pull request is per-ACTIVITY, so rounds 1 and 2 of
  // one gate both cited gf.prRef and the ledger claimed one subject for two different
  // drafts. The fix is to name the COMMIT — stagedRef, the ref StageTaskOutput returned
  // for the work task this round judges, which advances with every redraft.
  //
  // The ladder, most specific first:
  //   - a staged ref ⇒ SubjectCommit. The honest answer, and the only one that moves
  //     per revision. (Empty until the generic child stages construction output;
  //     stage 4b1 Task 11 fills it.)
  //   - else a live PR ⇒ SubjectPullRequest. Coarse, but it is a handle a reviewer can
  //     open, and it is what the rail had.
  //   - else the work ATTEMPT ⇒ SubjectArtifact. Joins to both ledgers and to the
  //     episode that burned it.
  func gateSubjectRef(gf *gitForward, stagedRef, workAttemptID string) projectstate.SubjectRef {
  	if stagedRef != "" {
  		return projectstate.SubjectRef{Kind: projectstate.SubjectCommit, Ref: stagedRef}
  	}
  	if gf.enabled && gf.prRef != "" {
  		return projectstate.SubjectRef{Kind: projectstate.SubjectPullRequest, Ref: gf.prRef}
  	}
  	return projectstate.SubjectRef{Kind: projectstate.SubjectArtifact, Ref: workAttemptID}
  }
  ```
  - [ ] Update `openGateRound`'s single call (`constructactivity.go:2024`, inside the `gateLedger` literal) to pass `state.stagedRef` — adding that field to `constructState` beside `workAttemptID`, zero until Task 11 sets it. Give `designSubjectRef` the same three-rung ladder — it keeps its own `(gf gitSession, kind ArtifactKind)` parameter list and GAINS a `stagedRef string` first rung, so the two functions share a rule and not a signature — passing the design rail's already-known staged version (the `readBackVersion` the session stages at `coauthorartifact.go:1132`) as `stagedRef`. The design rail HAS a staged ref today, so its half of this fix is live immediately, which is what makes the change testable before Task 11.

- [ ] **Step 3: Refuse a re-typing re-open.** In `OpenActivity`, ahead of the two assignments:
  ```go
  		// TYPE AND VARIANT ARE WRITE-ONCE, for the same reason the pin is. The row's
  		// Type is what ResolveConstructionRow resolves every read-time derivation
  		// against — the lifecycle profile, the phase set, earned value — so a re-open
  		// that quietly re-typed it would retro-date every attempt and round already
  		// recorded to a DAG they were never written under. Write-once cannot be honoured
  		// here by keeping the old value and reporting success: the caller would be told
  		// it opened the activity it asked for. So this refuses, exactly as the pin does.
  		//
  		// A genuine re-classification is an amendment to the committed activity list
  		// followed by a NEW execution, not a re-open of the old one.
  		if cs.StartedAt != nil && (cs.Type != typ || cs.Variant != variant) {
  			refused = execMisuse("OpenActivity", fmt.Sprintf(
  				"activity %s is open as %s/%s and cannot be re-opened as %s/%s; the ledger below it was written under the first",
  				activityID, cs.Type, cs.Variant, typ, variant))
  			return
  		}
  		cs.Type = typ
  		cs.Variant = variant
  ```
  - [ ] **Verify first:** `cs.StartedAt != nil` is the "this row is live, not being birthed" test the surrounding code already uses (`:10089-10095`: *"StartedAt IS 'running' now that no roll-up is stored"*), so a BIRTH — where `StartedAt` is nil and `Type` is the zero value — still writes both fields. Check that `execMisuse` is the helper in scope (it is: `OpenActivity` already calls it at `:10038` and `:10083`) and that `ActivityType`/`TestingVariant` render usefully in `%s` (if either is an int enum with no `String()`, use its `.String()` explicitly rather than printing an ordinal at a human).

- [ ] **Step 4: Tests, both halves.**
  - `access_test.go`: a re-open with a different `typ` is `ContractMisuse` naming both types; a re-open with the SAME pair still succeeds and does not re-date `StartedAt`; a BIRTH with any pair succeeds. **Mutation-check**: with the refusal commented out, the first case must fail.
  - `manager_test.go`: two rounds of one design gate cite **different** `SubjectRef.Ref` values, and the second's `Kind` is `commit`. Assert the pre-fix behaviour is gone by value, not by absence.

- [ ] **Step 5: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'OpenActivity' -count=1 -v 2>&1 | tail -20
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Replay|SubjectRef|GateSubject' -count=1 -v 2>&1 | tail -20
  GOWORK=off go test -short -count=1 ./...
  golangci-lint cache clean && GOWORK=off make lint fix-check
  ```
  Expected: the store cases green and the mutation check red when disarmed; **all nineteen replays green** — `gateSubjectRef` is a pure function whose RESULT rides an existing Activity's arguments, so no durable command moves; if a replay goes red, the call was moved rather than re-parameterised.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1
  git add server
  git commit -F - <<'MSG'
  fix(rail): name the commit a round judged, and refuse a re-open that re-types a row

  Two carried review findings, one commit, because the generic DAG child is the
  single review-round writer and the single row opener — fixing them after it lands
  means touching the new writer twice.

  gateSubjectRef cited the pull request, which is per-ACTIVITY: rounds 1 and 2 of
  one gate named the same subject, so the ledger could not say which draft each
  round judged. It now names the COMMIT the work task staged, falling back to the PR
  and then to the work attempt. SubjectCommit has been in the vocabulary, unused,
  since stage 3. The design rails have a staged ref today, so their half is live
  immediately; construction's is empty until the generic child stages its output.

  OpenActivity re-opened a live row with whatever type and variant the caller
  passed. The row's Type is what ResolveConstructionRow resolves every read-time
  derivation against, so a re-typed row retro-dates every attempt and round to a
  lifecycle they were never written under. It now refuses, exactly as the pin
  already does, and says both types in the message. A genuine re-classification is
  an amended activity list and a new execution.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 6: The stranded-`pending`-round sweep (entry criterion)

> **EXECUTION ORDER: this task runs AFTER Tasks 3 and 7**, not sixth. It calls `roundGateKey` (Task 3 Step 4) and `isTerminalConflict` (Task 7 Step 2) and reads `ReviewRound.ArtifactKind` (Task 3 Step 2). It is numbered 6 because it belongs with the entry criteria it discharges; it is not in the parallel set.

Both rails state the crash window and both hand it to "the stage-4 sweep". `openGateRound`'s doc is explicit (`constructactivity.go:1969-1979`): a run that dies between `OpenReviewRound` and `DecideReviewRound` leaves round *n* pending forever; the resume mints *n+1* off the same counter and opens a fresh round, so nothing is duplicated and no id collides — *"but nobody goes back to close n"*. `roundOutcome` renders such a round `revRunning` while it is the gate's last round (`deliverymanager.go:11091-11094`), and with no live session it never even reads `awaitingHuman`. The parallel pump produces MORE of these, not fewer, and Task 4 just gave the sweep the terminal it stamps.

**Files:**
- Create: `server/internal/manager/delivery/roundsweep.go` — one new workflow, `RoundSweepWorkflow`.
- Modify: `server/internal/manager/delivery/deliverymanager.go` — the execution-kind const, the workflow id, the manifest entry, the Schedule registration.
- Modify: `server/internal/registered_names_test.go` — the golden (a NEW name, appended by regeneration).
- Modify: `server/internal/manager/delivery/manager_test.go`.

**Interfaces produced (Task 13 Step 6 keeps this name in the frozen list; Task 15 lists its Schedule in the drain note):**
- `executionKindRoundSweep = "deliveryRoundSweep"`, entry `(*csWorkflows).RoundSweepWorkflow`, file `roundsweep.go` (`arch.CheckFileLayout`: `strings.ToLower(strings.TrimSuffix("RoundSweepWorkflow","Workflow")) + ".go"`).
- `roundSweepWorkflowID(projectID ProjectID, tickID string) string = fmt.Sprintf("%s:roundSweep:%s", projectID, tickID)`.
- `scheduleIDRoundSweep = "delivery:roundSweep"`, interval `roundSweepIntervalSecs = 300`.

- [ ] **Step 1: Read the two rails' statements of the window and the existing sweep's shape.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  sed -n '1960,1985p' internal/manager/delivery/constructactivity.go
  sed -n '4070,4085p' internal/manager/delivery/coauthorartifact.go
  sed -n '1,121p'    internal/manager/delivery/pumpsweep.go
  sed -n '10128,10160p;10360,10400p' internal/manager/delivery/deliverymanager.go
  ```
  Expected: both crash-window docs naming the stage-4 sweep and `RoundWithdrawn` as its terminal; `PumpSweepWorkflow` listing projects through `ProjectStateListProjects(pumpSweepOwnerScope)` (`pumpsweep.go:77`, scope const `:65`) and starting a child per project with `ParentClosePolicy: ABANDON`, waiting only for the START ack (`:106`) and treating `IsWorkflowExecutionAlreadyStartedError` as success (`:107-114`); the Schedule const block at `:10144-10153` with its cutover rationale at `:10130-10143` and `RegisterSchedules` at `:10368`. Copy `pumpsweep.go`'s shape — the project listing, the ABANDON policy, the already-started tolerance — and change only what it sweeps.

- [ ] **Step 2: Write the sweep.** Create `roundsweep.go`:
  ```go
  package delivery

  // roundSweepMaxPerTick bounds one tick's writes. A sweep that stamped an unbounded
  // number of rounds would, on a project whose ledger has years of history, make one
  // Temporal task do unbounded work; 200 is far above any real backlog and low enough
  // that a pathological one is paced across ticks rather than timing a task out.
  const roundSweepMaxPerTick = 200

  // RoundSweepWorkflow closes rounds that no run will ever decide.
  //
  // THE WINDOW, as both rails already state it: a child that dies between
  // OpenReviewRound and DecideReviewRound leaves round n PENDING forever. The resume
  // mints n+1 off the same counter, so nothing is duplicated and no id collides — but
  // nobody goes back to close n, and roundOutcome renders it `running` for as long as
  // it is the gate's last round. With no live session it never even reads
  // awaitingHuman, so the screen shows work in flight that nothing is doing.
  //
  // THE RULE, and it is deliberately the narrowest one that is always true: a pending
  // round is stranded when a LATER round exists on the same (taskId, artifactKind)
  // join key. That later round is proof a run already decided to start over, which is
  // the only thing a sweep can know from outside — it cannot know whether a run is
  // gone, and asking Temporal would make head-state truth depend on a control-plane
  // query — and "same gate" is roundGateKey, the task AND the kind.
  // A pending round that is its gate's LATEST is left alone: it may be a live
  // gate awaiting a human, and stamping that would withdraw a review someone is
  // reading.
  //
  // The terminal is RoundWithdrawn, which stage 4b1 gave its own wire member: the
  // round was pulled back before anyone decided it, which is exactly what happened.
  // decidedBy names the sweep, so the ledger never claims a person decided it.
  func (wf *csWorkflows) RoundSweepWorkflow(ctx workflow.Context, in roundSweepInput) (RoundSweepResult, error) {
  	logger := workflow.GetLogger(ctx)
  	proj, err := wf.readProject(ctx, in.ProjectID)
  	if err != nil {
  		if isReadNotFound(err) {
  			return RoundSweepResult{}, nil
  		}
  		return RoundSweepResult{}, err
  	}
  	headVersion := proj.Version
  	stamped := 0
  	for _, activityID := range sortedActivityIDs(proj.ActivityExecution) {
  		row := proj.ActivityExecution[activityID]
  		for _, r := range strandedRounds(row) {
  			if stamped >= roundSweepMaxPerTick {
  				logger.Info("round sweep hit its per-tick bound; the rest is swept on the next tick",
  					"projectId", string(in.ProjectID), "stamped", stamped)
  				return RoundSweepResult{Stamped: stamped, Bounded: true}, nil
  			}
  			v, serr := wf.applyRecovering(ctx, in.ProjectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
  				return wf.Acts.ActivityExecutionDecideReviewRound(
  					ctx, projectstate.ProjectID(in.ProjectID), expected,
  					projectstate.NoActivityVersionExpectation, activityID, r.RoundID,
  					projectstate.RoundWithdrawn, roundSweepDecidedBy,
  					railCredEnvelope{}.toProjectState())
  			})
  			if serr != nil {
  				// A round another writer decided between the read and the write is
  				// ALREADY closed, which is this sweep's goal; anything else is real.
  				if isTerminalConflict(serr) {
  					logger.Info("round already decided by someone else; nothing to sweep",
  						"projectId", string(in.ProjectID), "activityId", activityID, "roundId", r.RoundID)
  					continue
  				}
  				return RoundSweepResult{}, serr
  			}
  			headVersion = v
  			stamped++
  			logger.Info("stranded review round withdrawn",
  				"projectId", string(in.ProjectID), "activityId", activityID, "roundId", r.RoundID)
  		}
  	}
  	return RoundSweepResult{Stamped: stamped}, nil
  }
  ```
  plus, in `deliverymanager.go` (pure helpers — no workflow context, so they may live in the impl file, and R-C's rule forbids them in a workflow file only if that file would then have no entry func, which it has):
  ```go
  // roundSweepDecidedBy is who the ledger records for a swept round. It is deliberately
  // not an operator and not a role: nobody decided this round, a sweep closed it.
  const roundSweepDecidedBy = "platform-sweep"

  // strandedRounds returns the PENDING rounds of one row that a later round on the same
  // GATE has superseded — in ledger order, so a tick's writes are deterministic.
  // A pending round that is its gate's latest is NOT stranded: it may be a live gate
  // awaiting a human.
  //
  // "Same gate" is roundGateKey (Task 3): the review task AND the artifact kind. Two
  // kinds sharing one gate task are two gates here, so neither can strand the other —
  // which is the artifactKind field doing its job one commit after it landed. This is
  // WHY roundGateKey exists at its own arity: asking roundJoinKey for a gate identity
  // would mean synthesising a zero-Round ReviewRound and relying on every call getting
  // the same ":0" suffix, which is true by accident and not by contract.
  func strandedRounds(row projectstate.ActivityExecution) []projectstate.ReviewRound {
  	latest := make(map[string]int64, len(row.Reviews))
  	for _, r := range row.Reviews {
  		gate := roundGateKey(r.TaskID, r.ArtifactKind)
  		if r.Round > latest[gate] {
  			latest[gate] = r.Round
  		}
  	}
  	var out []projectstate.ReviewRound
  	for _, r := range row.Reviews {
  		if r.Outcome != projectstate.RoundPending {
  			continue
  		}
  		if r.Round < latest[roundGateKey(r.TaskID, r.ArtifactKind)] {
  			out = append(out, r)
  		}
  	}
  	return out
  }
  ```
  - [ ] **Verify first:** `roundGateKey` and `isTerminalConflict` must ALREADY EXIST — this task runs after Tasks 3 and 7, per the note at the head of it, so neither is a forward reference and neither has a fallback. If either is missing, the task order was not followed and the fix is to land the missing task, not to inline a substitute. `sortedActivityIDs` may not exist — check with `grep -n 'func sortedActivityIDs' internal/manager/delivery/*.go` (measured at `86d3223a`: **0 hits**, so write it) and add it beside `strandedRounds`; map iteration order in a workflow is non-determinism, so the sort is not a style choice.

- [ ] **Step 3: Register it.** Add `executionKindRoundSweep = "deliveryRoundSweep"` to the construction-rail const block (`deliverymanager.go:10108-10120`), `roundSweepWorkflowID`, the `genRegisteredWorkflow` entry in `constructionManager.WorkerManifest()` (`:10259`), `scheduleIDRoundSweep = "delivery:roundSweep"` + `roundSweepIntervalSecs = 300` in the Schedule const block (`:10144-10153`, with the same cutover rationale the block already carries), and the registration in `RegisterSchedules` (`:10368`). Also add `RoundSweepResult{Stamped int; Bounded bool}` and `roundSweepInput{ProjectID ProjectID; TickID string}` beside the other workflow value types. **This is a NEW Temporal name**: the golden grows by two (`deliveryRoundSweep` plus nothing else — `activityExecutionAccess.decideReviewRound` is already registered), so regenerate the literal from the test's printed diff rather than editing it by hand.

- [ ] **Step 4: Tests.** In `manager_test.go`: a row with rounds 1 (pending) and 2 (pending) of one gate ⇒ round 1 withdrawn, round 2 untouched; a row whose ONLY round is pending ⇒ nothing swept (the live-gate case, and the one a careless sweep breaks); a row with two kinds on one gate task, each with one pending round ⇒ **nothing swept**, because Task 3's key makes them separate gates — this is the case that proves the sweep inherited the join fix; `Stamped` reporting; the per-tick bound; a project with no state ⇒ no error. **Mutation-check** the "latest is left alone" rule: with `<` changed to `<=`, the live-gate case must fail.

- [ ] **Step 5: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  GOWORK=off go test ./internal/manager/delivery/ -run 'RoundSweep|Test_Replay|Test_RegisterSchedules' -count=1 -v 2>&1 | tail -30
  GOWORK=off go test ./internal/ -run 'TestRegisteredTemporalNamesGolden|TestFileLayout' -count=1
  GOWORK=off go test -short -count=1 ./...
  # the full drift block
  ```
  Expected: the sweep's cases green and the mutation check red when the rule is loosened; nineteen replays green (a NEW workflow type adds no command to an existing history); `TestFileLayout` green — `roundsweep.go` is the only legal name for a file whose entry func is `RoundSweepWorkflow`; the golden regenerated to 140.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1
  git add server
  git commit -F - <<'MSG'
  feat(rail): close the rounds no run will ever decide

  Stage-3 entry criterion, and both rails already wrote the window down: a child
  that dies between OpenReviewRound and DecideReviewRound leaves round n pending
  forever. The resume mints n+1, so nothing duplicates and no id collides — but
  nobody closes n, and the screen renders it `running` for as long as it is the
  gate's last round. With no live session it never even reads awaitingHuman.

  The rule is the narrowest one that is always true: a pending round is stranded
  when a LATER round exists on the same (taskId, artifactKind) join key, because
  that later round is proof a run already started over. A pending round that is its
  gate's latest is left alone — it may be a live gate with a human reading it, and
  withdrawing that would be worse than the bug. The sweep asks Temporal nothing;
  head-state truth does not depend on a control-plane query.

  The terminal is RoundWithdrawn, which this wave just gave its own wire member, and
  decidedBy is the sweep, so the ledger never claims a person decided it. Two kinds
  sharing one gate task are two gates here, which is the artifactKind join doing its
  job one commit after it landed.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 7: `applyRecovering` re-reads the ROW, and a terminal `Conflict` fails immediately

The 4a earmark's item 3, and the one the pump's parallelism makes unavoidable: *"applyRecovering cannot recover a per-activity `Conflict`. Its re-read arm holds only the PROJECT version… The missing capability has a name: re-read the ROW on a row-level Conflict."* Measured: three bodies, two signatures, 60 call sites — `csWorkflows` (`constructactivity.go:3214`, no branch param), `workflows` (`coauthorartifact.go:124`, +branch), `pdWorkflows` (`coauthorphase2artifact.go:85`, +branch) — each re-reading only `readVersionE` / `readVersionOnBranch`. And two `Conflict`s are not version conflicts at all (`OpenActivity` on an exited row, `projectstateaccess.go:10064-10066`; a decided round, `:10260`/`:10421`): both burn 20 attempts and then fail as `MutateConflictExhausted`, which names the wrong cause.

**R-A is the ruling:** there is no new error class to add — `fwra.Kind` is platform-fixed and a Conflict arrives as nothing but the Kind's name — so **the re-read is the discriminator.** Re-read both versions; if neither moved, retrying cannot help, so fail now and say so.

**Files:**
- Modify: `server/internal/manager/delivery/constructactivity.go` — the `csWorkflows` loop + a row re-read behind a new fence.
- Modify: `server/internal/manager/delivery/coauthorartifact.go`, `coauthorphase2artifact.go` — the same in both twins.
- Modify: `server/internal/manager/delivery/deliverymanager.go` — `isTerminalConflict`, the fence const, the new error type name.
- Modify: `server/internal/manager/delivery/manager_test.go`.

**Interfaces produced (Task 6 Step 2, Task 8 and Task 12 all call these):**
- `changeRowConflictReread = "row-conflict-reread"` — the `GetVersion` change id.
- `func isTerminalConflict(err error) bool` — true for the `MutateTerminalConflict` non-retryable this task raises.
- The Temporal error type string `"MutateTerminalConflict"`.
- The three `applyRecovering` signatures are UNCHANGED — the row version is read off `state`, not passed.

- [ ] **Step 1: Confirm the two non-version Conflicts and the fence's necessity.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  sed -n '10058,10068p;10255,10263p;10415,10425p' internal/resourceaccess/projectstate/projectstateaccess.go
  grep -n 'func activityVersionMismatch' -A 10 internal/resourceaccess/projectstate/projectstateaccess.go
  grep -c 'applyRecovering(' internal/manager/delivery/constructactivity.go internal/manager/delivery/coauthorartifact.go internal/manager/delivery/coauthorphase2artifact.go
  grep -rn 'ActivityExecutionReadActivityExecution' internal/manager/delivery/worker.gen.go internal/manager/delivery/invokers.gen.go
  ```
  Expected: the exited-row refusal as `fwra.New(fwra.Conflict, …"a finished activity is not re-opened in place")`; both decided-round refusals as `fwra.Conflict`; `activityVersionMismatch` returning `fwra.Conflict` with *"is at version %d, not the expected %d; re-read the activity and re-apply"*; 19/14/12 call sites; and `activityExecutionAccess.readActivityExecution` **already a registered activity** (`worker.gen.go:43`, invoker `invokers.gen.go:458`). That last line is why the fence is needed and the verb is not: the row re-read is a NEW durable command inside an existing loop, so a recorded history that conflicted once and re-read only the project version must keep replaying that sequence.

- [ ] **Step 2: Add the discriminator and its error.** In `deliverymanager.go`, beside `isConflict` (`:4973`):
  ```go
  // changeRowConflictReread fences the row re-read applyRecovering gained in stage
  // 4b1. Pre-change executions keep their recorded sequence — project version only —
  // because the row read is a NEW durable command inside the loop, and a history that
  // conflicted once would otherwise replay into a command it never made.
  const changeRowConflictReread = "row-conflict-reread"

  // terminalConflictErrType is the Temporal Type() applyRecovering raises when a
  // Conflict cannot be a version conflict, because RE-READING CHANGED NOTHING.
  //
  // Why the re-read is the discriminator and not an error class: fwra.Kind is
  // platform-fixed (framework-go/resourceaccess/errors.go), every Conflict reaches a
  // workflow as nothing but that Kind's name (fwmanager.RAErrType), and matching a
  // store's message text from a workflow would couple the two across a release. The
  // two terminality Conflicts this catches — OpenActivity on an exited row, and
  // Append/Decide on a decided round — differ from a genuine version conflict in
  // exactly one OBSERVABLE way: nothing moves when you look again. So we look again,
  // and when neither the project version nor the row version moved we fail with the
  // honest cause instead of burning twenty attempts to report the wrong one.
  const terminalConflictErrType = "MutateTerminalConflict"

  // isTerminalConflict reports whether err is that terminal. Callers that legitimately
  // race to a terminal state — the round sweep withdrawing a round someone else just
  // decided — treat it as success rather than as a failure.
  func isTerminalConflict(err error) bool {
  	var appErr *temporal.ApplicationError
  	if errors.As(err, &appErr) {
  		return appErr.Type() == terminalConflictErrType
  	}
  	return false
  }
  ```

- [ ] **Step 3: Rewrite the `csWorkflows` loop.** Replace the body at `constructactivity.go:3214` (signature unchanged):
  ```go
  func (wf *csWorkflows) applyRecovering(
  	ctx workflow.Context,
  	projectID ProjectID,
  	seed projectstate.Version,
  	apply func(expected projectstate.Version) (projectstate.Version, error),
  ) (projectstate.Version, error) {
  	expected := seed
  	for attempt := 0; ; attempt++ {
  		v, err := apply(expected)
  		if err == nil {
  			return v, nil
  		}
  		if !isConflict(err) {
  			return 0, err
  		}
  		if attempt+1 >= maxMutateConflictAttempts {
  			return 0, temporal.NewNonRetryableApplicationError(
  				"head-state conflict did not converge within bounded attempts",
  				"MutateConflictExhausted", err)
  		}
  		next, rerr := wf.readVersionE(ctx, projectID)
  		if rerr != nil {
  			if isReadNotFound(rerr) {
  				expected = 0
  				continue
  			}
  			return 0, rerr
  		}
  		// THE ROW RE-READ (stage 4b1). Two things the old loop could not tell apart:
  		// an EXTERNAL row writer — the pump's RecordActivityFailed, an operator note
  		// filed through the API, or (from this wave) a reviewer resolving a comment on
  		// a round a live child holds — bumps the ROW version while the PROJECT version
  		// the loop re-read may or may not move; and a TERMINAL refusal (an exited row,
  		// a decided round) moves neither, because there is nothing to move. Re-reading
  		// the row answers both: it re-seeds the CAS the child holds by hand, and its
  		// standing still is what proves retrying is pointless.
  		if workflow.GetVersion(ctx, changeRowConflictReread, workflow.DefaultVersion, 1) == 1 {
  			rowBefore := wf.rowVersion()
  			rowAfter, rowErr := wf.rereadRowVersion(ctx, projectID)
  			if rowErr != nil {
  				return 0, rowErr
  			}
  			if next == expected && rowAfter == rowBefore {
  				return 0, temporal.NewNonRetryableApplicationError(
  					"head-state conflict is terminal: neither the project version nor the activity row moved on re-read, so the store is refusing this transition rather than racing it",
  					terminalConflictErrType, err)
  			}
  			wf.setRowVersion(rowAfter)
  		}
  		expected = next
  		workflow.GetLogger(ctx).Info("head-state conflict; re-read version and retrying",
  			"attempt", attempt+1, "nextExpectedVersion", expected)
  	}
  }
  ```
  - [ ] **Verify first, and this is the shape decision the step turns on:** `applyRecovering` is a method on the WORKFLOWS struct and has no access to the per-run `constructState` that holds `activityVersion` (seeded at `constructactivity.go:1093`, hand-advanced by the 20 `rowAdvanced()` sites). Add a **row accessor to the workflows struct** — `rowVersion func() int64`, `setRowVersion func(int64)`, `rowActivityID func() string`, all three bound once per run by the child's entry func right after `state` is built — rather than changing the signature at 60 call sites. `rereadRowVersion` calls `wf.Acts.ActivityExecutionReadActivityExecution(ctx, projectstate.ProjectID(projectID), wf.rowActivityID())` and returns `row.Version`, mapping `fwra.NotFound` to `NoActivityVersionExpectation` (a row that does not exist cannot have moved, and a birth legitimately holds no number). When the accessors are **unbound** the fenced block must be SKIPPED, not nil-called: guard on `wf.rowVersion == nil` and fall through to the project-version-only behaviour, with a comment naming all **THREE** such callers — the pump (`pumpnextactivity.go:162`), `projectsupervision.go`'s pause record, and `RoundSweepWorkflow` (Task 6), which sweeps rounds across EVERY activity of a project and so holds no single row by construction. A nil-call here would take down the pump, which is the one workflow in this package that cannot fail quietly, and it would take down the sweep, which runs on a Schedule where a crash is silent.

- [ ] **Step 4: Do the same in both twins** (`coauthorartifact.go:124`, `coauthorphase2artifact.go:85`), keeping their `branch` parameter and their `readVersionOnBranch` re-read. Their row accessors bind from `coAuthorState`/`pdCoAuthorState`, whose `activityVersion` is seeded in `seedRoundBaseFromLedger` (`coauthorartifact.go:3954`, `coauthorphase2artifact.go:2117`) — and **fix the silent degradation in the same edit**, because it is the second half of the same earmark: on a non-`NotFound` read failure those functions return early leaving `activityVersion` at 0 and admitting the session unread. Change both to propagate the error, and say why:
  ```go
  	// A non-NotFound read failure is NOT "no row": it is "we could not tell". Returning
  	// early here admitted the session UNREAD — activityVersion stayed 0, its
  	// OpenActivity applied and bumped the row N→N+1 while the session's own counter
  	// went 0→1, and the next OpenReviewRound Conflicted, was logged only, and left the
  	// session writing to the slot ledger alone. That was correct degradation for a
  	// best-effort rail and is wrong for the one writer of both ledgers.
  	if err != nil && !isReadNotFound(err) {
  		return err
  	}
  ```
  `roundsweep.go` (Task 6) is written AFTER this task and calls `isTerminalConflict` directly, so there is nothing to convert here — the execution order makes this a one-way dependency, not a two-step.

- [ ] **Step 5: Tests.** In `manager_test.go`: (a) a row-version conflict where the ROW moved and the project version did not ⇒ the loop re-reads, re-applies and SUCCEEDS (the case the old loop could not pass); (b) `OpenActivity` on an exited row ⇒ **one** attempt, `MutateTerminalConflict`, and assert the attempt COUNT, because the whole point is that 20 became 1; (c) `DecideReviewRound` on a decided round ⇒ same; (d) a genuine project-version race ⇒ converges as before; (e) `maxMutateConflictAttempts` exhaustion still reports `MutateConflictExhausted`; (f) a caller with unbound row accessors ⇒ project-version-only behaviour, unchanged, as a **table over all three** (pump, supervision, round sweep) so none can be the one that was forgotten. **Mutation-check** (b): with the terminal arm removed it must report `MutateConflictExhausted` after 20 attempts.

- [ ] **Step 6: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  GOWORK=off go test ./internal/manager/delivery/ -run 'ApplyRecovering|TerminalConflict|Conflict' -count=1 -v 2>&1 | tail -40
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Replay' -count=1 -v 2>&1 | tail -25
  GOWORK=off go test -short -count=1 ./...
  golangci-lint cache clean && GOWORK=off make lint fix-check
  ```
  Expected: every case green, the mutation check red when disarmed, **and all nineteen replays green — this is the assertion the fence exists for.** A red replay means the `GetVersion` guard is in the wrong place: the row read must sit INSIDE the fenced branch, and `GetVersion` must be called before any branch that could skip it, so the marker is recorded deterministically on every new run.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1
  git add server
  git commit -F - <<'MSG'
  fix(rail): re-read the row, and stop burning twenty attempts on a refusal

  The 4a earmark's third item, and the one the parallel pump makes unavoidable.
  applyRecovering's re-read arm held only the PROJECT version, so an external row
  writer — the pump's RecordActivityFailed, an operator note filed through the API,
  and from this wave a reviewer resolving a comment on a round a live child holds —
  drove the loop to MutateConflictExhausted instead of a re-read. Nothing was
  corrupted; a perfectly healthy activity failed non-retryably.

  Two more Conflicts were never version conflicts at all: OpenActivity on an exited
  row, and Append/Decide on a decided round. Both burned twenty attempts and then
  named the wrong cause.

  There is no error class to add. fwra.Kind is platform-fixed and a Conflict reaches
  a workflow as nothing but that Kind's name, and message-matching a store sentence
  from a workflow couples the two across a release. So the RE-READ is the
  discriminator: read the project version and the row version, and when neither
  moved, the store is refusing rather than racing — fail now, as
  MutateTerminalConflict, with the honest reason. Twenty attempts became one.

  The row read is a new durable command inside an existing loop, so it is fenced;
  all nineteen replays stay green. The three callers that hold no row — the pump,
  supervision, and the round sweep that walks every activity of a project — keep the
  project-only behaviour, guarded rather than nil-called.

  Fixed in the same commit because it is the same earmark: both design rails' seed
  read returned early on a non-NotFound failure and admitted the session UNREAD,
  which is how a session's counter and its row silently disagreed.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 8: The strategy table and the generic DAG walker

The child. ONE registered workflow type that walks any `methodassets.Lifecycle` by `dependsOn`, with per-kind behaviour in the DATA (`kind`, `command`, `workerClass`, `artifactKind`, `reviews`, and the phase's `gate`) plus a small Go strategy table for the two things the data cannot carry: **how a task's output is produced** (an agentic dispatch, or a server-side computation) and **how its subject is staged**. Architect Ruling 3(a) explicitly rejects the spec's stronger claim that per-type differences are *only* data — the interactive design session and the CI poll/variance/override loop are two real implementations — so they sit behind one `taskStrategy` interface the walker never branches on.

This task lands the walker and **one injectable registry** holding all three strategy slots, with the two dispatch slots filled by refusals that name the task that completes them (Tasks 10 and 11) and the compute slot filled in Task 9. The registry is injectable because Step 10's shape cases must be able to substitute a stub for a slot that does not work yet — a registry with two hard-coded arms has nowhere to register one. Nothing starts the child yet except the tests; the pump is re-pointed in Task 9.

It also lands **R-L's two history mitigations** (Steps 6 and 7), because they are not optimisations: one child history replaces what were four per-kind co-author executions, and at `15 s × 240` polls per dispatch a single `requirements` activity reaches ≈10,560 events on its own.

**Files:**
- Create: `server/internal/manager/delivery/deliveryactivity.go` — the child, the walker, the strategy interface and table, and every `workflow.Context`-taking helper they need (R-C: the file-layout gate forbids them anywhere else).
- Modify: `server/internal/manager/delivery/deliverymanager.go` — the execution-kind const, the workflow id, the input/state types (pure), the manifest entry.
- Modify: `server/internal/registered_names_test.go` — golden (regenerated).
- Modify: `server/internal/manager/delivery/manager_test.go` — re-point Task 1's `shapeRig.register`, add the walker arch guard.

**Interfaces produced (every later task names these):**
- `executionKindDeliveryActivity = "deliveryActivity"`; entry `(*csWorkflows).DeliveryActivityWorkflow`; file `deliveryactivity.go`.
- `func deliveryActivityWorkflowID(projectID ProjectID, activityID ActivityID) string` → `"{projectID}:activity:{activityID}"`.
- `type deliveryActivityInput struct { ProjectID ProjectID; ActivityID ActivityID; Activity constructionActivity; Resume *walkSnapshot }` — `constructionActivity` is REUSED unrenamed (a class-D rename wave is not in scope; say so in the type's doc). `Resume` is nil on a fresh start and carries the walk across `ContinueAsNew` (Step 7).
- Signals, all FOUR opened by the entry func and all owned by the ROUTER (Step 3a): `signalTaskDecision = "taskDecision"`, `signalCommentStatus = "setCommentStatus"` (existing), `signalOperatorOverride` (existing), `signalRedraft` (existing). Query: `querySessionState` (existing).
- `type taskDecisionSignal struct { TaskID string; Decision ReviewDecision; OptionID *OptionID; Feedback *ReviewFeedback; DecidedBy string; AcknowledgeStale bool }` — and **every** signal payload carries a `TaskID`, because the router keys on it: `commentStatusSignal`, `operatorOverrideSignal` and `redraftSignal` each gain one if they lack it.
- `type routedSignal struct { Kind string; TaskID string; Decision *taskDecisionSignal; Status *commentStatusSignal; Override *operatorOverrideSignal; Redraft *redraftSignal }` with `routedKindDecision|Status|Override|Redraft` — exactly one pointer non-nil; exported fields so it survives `ContinueAsNew`.
- `func (wf *csWorkflows) routeSignals(ctx workflow.Context, ws *walkState, decisions, statuses, overrides, redrafts workflow.ReceiveChannel)` — the ONE coroutine that receives from a shared signal channel, and it **never blocks**. `deliveryTaskInboxCapacity = 64`.
- Per-task delivery: `walkState.inbox map[string]workflow.Channel` (created at schedule time, `workflow.NewNamedBufferedChannel(ctx, "inbox:"+taskID, deliveryTaskInboxCapacity)`) and `walkState.pending map[string][]routedSignal` (messages with no inbox yet, **and any the inbox was too full to take**). Three methods on `walkState`: `deliver(logger, msg)` — `SendAsync`, falling back to `pending`, no `ctx` because nothing in it can block; `drainPending(taskID, ch)` — pulls `pending` into the inbox in order, called by `openInbox` AND at the top of every receive-loop iteration; `closeInbox(ctx, logger, taskID)` — logs and clears **`pending[taskID]`** and `ReceiveAsync`-drains the channel, both as too-late, so a retiring task leaves nothing held and nothing queued. A dispatch task additionally holds a **local `deferred []routedSignal`** for messages it cannot act on (a decision or comment status naming a dispatch task is a misroute), flushed back through `deliver` on the way out — never into `pending`, which would spin (Step 6).
- **No task coroutine ever receives from a shared signal channel** — `runTask`, `runGate`, `awaitTaskDecision` and the observe loop all take ONE `inbox workflow.ReceiveChannel`.
- `type taskStrategy interface { Produce(ctx workflow.Context, tc taskContext) (producedSubject, error) }` and `type producedSubject struct { StagedRef string; AttemptID string; Outcome projectstate.AttemptOutcome; Detail string }`
- `type strategyRegistry map[string]func(*csWorkflows) taskStrategy` with the three slot keys `strategySlotDispatch = "dispatch"`, `strategySlotJudged = "judged"`, and `strategySlotCompute(artifactKind) = "compute:" + artifactKind`; `func productionStrategies() strategyRegistry`; `func strategyFor(reg strategyRegistry, wf *csWorkflows, lc methodassets.Lifecycle, t methodassets.LifecycleTask) (taskStrategy, error)`. The registry is a field on `csWorkflows` (`wf.Strategies`), defaulted to `productionStrategies()` by `csNewWorkflows` so an unwired slice cannot nil-map-read, and overridden per test.
- `taskState ∈ {taskPending, taskRunning, taskPassed, taskSentBack, taskFailed}` — **its ordinals are payload-visible across `ContinueAsNew` (see `walkSnapshot.ByTask`) and are therefore APPEND-ONLY, never renumbered**, exactly like every other wire-visible identifier in this repo; `walkState{ byTask map[string]taskState; revision map[string]int64; feedback map[string]string; produced map[string]producedSubject; inbox map[string]workflow.Channel; pending map[string][]routedSignal }`; `type walkSnapshot struct { ByTask map[string]int; Revision map[string]int64; Feedback map[string]string; Produced map[string]producedSubject; Pending map[string][]routedSignal }` (exported fields, JSON-serialisable, nothing else — `inbox` holds channels and is re-created, never carried).
- `deliveryActivityHistoryBudget = 4000`; `observeFastInterval`/`observeFastPolls`, `observeMedInterval`/`observeMedPolls`, `observeSlowInterval`/`observeSlowPolls`, `maxObserveTotalPolls`, `func observeInterval(poll int) time.Duration` (Step 6 — these exact names, nothing called `observeBackoff*`).
- Declared here and used by Task 9: `func lifecyclePhaseByID(lc methodassets.Lifecycle, phaseID string) (methodassets.LifecyclePhase, bool)`; `gateActorSystem = "system"` (beside the existing `gateActorOperator = "operator"`, `constructactivity.go:1827`); `func reasonOf(set ReviewSet) string` (the engine's one-line reason, `""` when `set.Reason` is nil); `func (wf *csWorkflows) passRound(ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle, t methodassets.LifecycleTask, tc taskContext, state *constructState, decidedBy, reason string) (taskState, error)` — **it takes `lc`**, because Task 9's M0 handler asks the lifecycle whether this is the M0 gate.
- Declared here, bodies delegated to their oracle: `seedWalkFromLedger`, `openActivityRow`, `bindRowAccessors`, `recordTaskAttempt`, `finalizeWalk`, `failWalk`, and the widened `loadReviewSnapshot`/`proposeReviewSet`.

- [ ] **Step 1: Read the three spines the walker replaces, and the lifecycle data it replaces them with.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  sed -n '838,930p'   internal/manager/delivery/constructactivity.go    # the entry + runAttempt loop
  sed -n '1179,1235p' internal/manager/delivery/constructactivity.go    # walkPhases, verbatim
  sed -n '1390,1460p' internal/manager/delivery/constructactivity.go    # runPhaseGate + gateWithoutHuman
  sed -n '1497,1620p' internal/manager/delivery/constructactivity.go    # awaitPhaseDecision, sendBackGate, the outcome vocabulary
  sed -n '275,340p'   internal/manager/delivery/coauthorartifact.go     # the co-author spine
  MA=$(GOWORK=off go env GOMODCACHE)/github.com/mixofreality-studio/archistrator-platform/method-assets@v0.9.0
  python3 -c "
import json;d=json.load(open('$MA/lifecycles.json'))['lifecycles']
for l in d:
  print(l['type'], [ (t['id'],t['kind'],t['phase'],t.get('dependsOn',[]),t.get('reviews',''),t.get('command',''),t.get('workerClass',''),t.get('artifactKind','')) for t in l['tasks'] ])" | sed 's/), (/)\n    (/g'
  ```
  Expected: `walkPhases` iterating `in.Activity.Phases` with the `state.completedPhases[phase]` resume skip; `runPhaseGate` → `recordPhaseStarted` → `proposeReviewSet` → gated-or-not, with `gateWithoutHuman` taking the `set.RequiresHuman == nil || !*set.RequiresHuman` arm; `awaitPhaseDecision` filtering `signalPhaseDecision` by gate KEY; `sendBackGate` with `maxPhaseRedrafts = 5` and the "on exhaustion the gate KEEPS awaiting" behaviour; the outcome vocabulary (`approved`/`sentBack`/`sentBackExhausted`/`timedOut`/`override:`) at `:1609-1615`. And in the data: **14 lifecycles**, the two real forks being `service`'s and `frontend`'s `stp` branch (`stp dependsOn [srsReview]`; `testing dependsOn [integration, stpReview]`), and `projectDesign` being exactly one review task with `dependsOn: []`, no `command`, no `workerClass`, `artifactKind: SdpReview`.

- [ ] **Step 2: Write the strategy seam.** In `deliveryactivity.go`:
  ```go
  // ---------------------------------------------------------------------------
  // THE STRATEGY SEAM (architect Ruling 3(a), 2026-09-26).
  //
  // Per-type behaviour is DATA — lifecycles.json already carries kind, phase,
  // dependsOn, reviews, command, workerClass and artifactKind, and the phase carries
  // its gate and weight. Two things the data cannot carry, and the ruling is explicit
  // that pretending otherwise is wrong: how a task's output is PRODUCED, and how its
  // subject is STAGED. The interactive design session and the CI poll/variance/override
  // loop are two real implementations, and a server-side deterministic computation is a
  // third. They sit behind ONE interface the walker never branches on.
  //
  // THE RULE THE WALKER OBEYS, and the arch test that holds it:
  // Test_DeliveryActivityWalker_NamesNoTypeOrCommand parses this file and fails if any
  // of the four walker funcs names an ActivityType, an ArtifactKind or a command string.
  // The moment a walker grows `if activityType ==`, this is a god-workflow worse than
  // the two types it replaced, and §9's smaller-than-sum acceptance is the thing that
  // pays for it.
  // ---------------------------------------------------------------------------

  // taskContext is everything a strategy may read. It is assembled ONCE per task
  // attempt by the walker, so a strategy cannot reach back into the walk and cannot
  // decide what runs next.
  type taskContext struct {
  	In       deliveryActivityInput
  	Task     methodassets.LifecycleTask
  	Phase    methodassets.LifecyclePhase
  	Revision int64
  	Attempt  int
  	State    *constructState
  	Feedback string
  }

  // producedSubject is what a strategy hands back: the ref a review round will cite,
  // the attempt id the ledger recorded, and the attempt's outcome. StagedRef is empty
  // when the strategy staged nothing (construction's output is a commit the agent
  // pushed, not a model this platform staged) and gateSubjectRef falls through its
  // ladder accordingly.
  type producedSubject struct {
  	StagedRef string
  	AttemptID string
  	Outcome   projectstate.AttemptOutcome
  	Detail    string
  }

  // taskStrategy produces one task's output. Exactly one implementation is registered
  // per (task kind × how its subject comes to exist); strategyFor is the only place
  // that chooses, and it chooses on lifecycle FIELDS, never on an activity type.
  type taskStrategy interface {
  	Produce(ctx workflow.Context, tc taskContext) (producedSubject, error)
  }

  // strategyRegistry maps a SLOT to a strategy constructor. ONE registry for all three
  // slots, not two hard-coded arms plus a map — because Step 10's shape cases must be
  // able to substitute a stub for a slot whose real implementation arrives two tasks
  // later, and a hard-coded arm has nowhere to register one. It is a field on
  // csWorkflows (wf.Strategies), defaulted by csNewWorkflows so an unwired slice cannot
  // read a nil map, and overridden per test.
  type strategyRegistry map[string]func(*csWorkflows) taskStrategy

  const (
  	strategySlotDispatch = "dispatch"
  	strategySlotJudged   = "judged"
  )

  // strategySlotCompute keys the COMPUTE slot by the artifactKind the lifecycle spells,
  // so a second computation is a registry entry and not a branch.
  func strategySlotCompute(artifactKind string) string { return "compute:" + artifactKind }

  // productionStrategies is the registry the composition path uses. Stage 4b1 fills
  // dispatch in Tasks 10/11 and compute:SdpReview in Task 9; until then the two
  // unfilled slots REFUSE, naming the task that owes them — a strategy that silently
  // succeeded would make an out-of-order execution look like a passing walk.
  func productionStrategies() strategyRegistry {
  	return strategyRegistry{
  		strategySlotDispatch: func(wf *csWorkflows) taskStrategy { return agenticDispatchStrategy{wf: wf} },
  		strategySlotJudged:   func(wf *csWorkflows) taskStrategy { return judgedTaskStrategy{} },
  	}
  }

  // strategyFor resolves a task to its strategy SLOT from the lifecycle fields ALONE
  // (R5: v0.9.0's field set, since parseLifecycles DisallowUnknownFields makes a new
  // field a platform release), then reads that slot out of the registry. The table,
  // one row per (kind × subject class) — three rows, and there is no fourth: a review
  // task's own `command` runs its AGENT REVIEWERS, which runGate does, not strategyFor.
  //
  //	kind=dispatch, command!="" , artifactKind!="" , workerClass!=""  -> slot "dispatch"
  //	kind=review  , reviews!=""                                       -> slot "judged" (its subject is the judged task's output; it produces nothing of its own)
  //	kind=review  , reviews==""  , artifactKind!=""                   -> slot "compute:<artifactKind>"
  //
  // The third row IS the carve-out that makes deterministic Project Design a generic
  // child rather than a special case: ValidateLifecycle permits "a review that names no
  // dispatch, in a lifecycle with no dispatch at all", so the compute must NOT be
  // modelled as a task — that would be a method-assets release inside this wave. A
  // review task with no `reviews` target has its subject produced by the strategy
  // registered for its artifactKind.
  func strategyFor(reg strategyRegistry, wf *csWorkflows, lc methodassets.Lifecycle, t methodassets.LifecycleTask) (taskStrategy, error) {
  	slot := ""
  	switch t.Kind {
  	case methodassets.LifecycleTaskDispatch:
  		if t.Command == "" || t.WorkerClass == "" || t.ArtifactKind == "" {
  			return nil, newError(fwmanager.FailedPrecondition,
  				"lifecycle "+lc.Type+" task "+t.ID+" is a dispatch with no command, workerClass or artifactKind; the platform's lifecycle data is malformed")
  		}
  		slot = strategySlotDispatch
  	case methodassets.LifecycleTaskReview:
  		if t.Reviews != "" {
  			slot = strategySlotJudged
  		} else if t.ArtifactKind != "" {
  			slot = strategySlotCompute(t.ArtifactKind)
  		} else {
  			return nil, newError(fwmanager.FailedPrecondition,
  				"lifecycle "+lc.Type+" task "+t.ID+" reviews nothing and names no artifact kind, so nothing can produce its subject")
  		}
  	default:
  		return nil, newError(fwmanager.FailedPrecondition,
  			"lifecycle "+lc.Type+" task "+t.ID+" has task kind "+t.Kind+", which is neither dispatch nor review")
  	}
  	make, ok := reg[slot]
  	if !ok {
  		return nil, newError(fwmanager.FailedPrecondition,
  			"lifecycle "+lc.Type+" task "+t.ID+" needs strategy slot "+slot+", which nothing has registered")
  	}
  	return make(wf), nil
  }
  ```
  - [ ] **Verify first:** `projectstate.AttemptOutcome`'s spelling and members (`grep -n '^type AttemptOutcome' -A 12 internal/resourceaccess/projectstate/contract.gen.go`) and `newError`'s signature in this package. `make` shadows the builtin in the snippet above — rename the local to `ctor` when writing it; the plan spells it `make` only to keep the table's shape readable, and a shadowed builtin is exactly what `gocritic` will flag. `agenticDispatchStrategy{wf *csWorkflows}` and `judgedTaskStrategy{}` are declared here with a `Produce` that returns a `FailedPrecondition` **naming the task that will fill it** (Task 10 for the design half, Task 11 for construction; `judgedTaskStrategy` returns an empty `producedSubject` and no error from the start, because a review's subject IS its judged task's output and Step 4 reads it out of `walkState`). A strategy that silently succeeds is worse than one that refuses.

- [ ] **Step 3: Write the walk.** Still in `deliveryactivity.go`:
  ```go
  // taskState is one task's position in the walk. It is WALK-LOCAL: the durable truth
  // is the attempt and round ledgers, and this map is rebuilt from them on resume.
  type taskState int

  const (
  	taskPending taskState = iota
  	taskRunning
  	taskPassed
  	taskSentBack
  	taskFailed
  )

  // walkState is the walk's own bookkeeping. FOUR maps, and each earns its place:
  //
  //	byTask    each task's position. A revision is a DERIVED grouping everywhere else
  //	          (spec §5.3) and stored nowhere; this is walk-local and rebuilt on resume.
  //	revision  the counter the next round id is minted from.
  //	feedback  the send-back notes a re-opened task must be re-dispatched WITH. Without
  //	          it a redraft runs against the same prompt that was just rejected, which
  //	          is the send-back doing nothing.
  //	produced  what each task's strategy produced, keyed by TASK. This is how a review
  //	          task reaches its JUDGED task's staged ref and attempt id: a review
  //	          produces nothing of its own (judgedTaskStrategy), so without this map
  //	          gateSubjectRef would fall to its third rung on every round and Task 5's
  //	          "the subject changes between rounds 1 and 2" could not hold.
  //
  // Those four are JSON-serialisable, which is what makes walkSnapshot possible (Step 7).
  // Two more are DELIVERY state, not walk state, and Step 3a is what they are for:
  //
  //	inbox     one per RUNNING task, created when the task is scheduled. It is the ONLY
  //	          channel a task coroutine receives from.
  //	pending   messages the router took off a shared channel for a task that has no
  //	          inbox yet. Flushed into the inbox at creation, carried across a continue.
  type walkState struct {
  	byTask   map[string]taskState
  	revision map[string]int64
  	feedback map[string]string
  	produced map[string]producedSubject
  	inbox    map[string]workflow.Channel
  	pending  map[string][]routedSignal
  }

  // walkSnapshot is walkState across ContinueAsNew: exported fields, the four walk maps
  // plus the undelivered messages, and nothing else. No channel (re-created), no
  // lifecycle (re-resolved from the activity), no review policy (re-snapshotted),
  // because anything re-derivable must be re-derived rather than carried — a snapshot
  // that carries a derivable fact is a second copy to keep in step.
  //
  // ByTask ENCODES THE taskState IOTA. That makes the ordinals payload-visible across a
  // continue-as-new, which puts them under this repo's never-renumber rule: a new state
  // is APPENDED, and re-ordering the existing five would silently re-interpret every
  // in-flight walk at the moment the new image goes live. Pinned by
  // Test_TaskStateOrdinalsNeverRenumber. (Serialising them as strings was the
  // alternative and was rejected: it trades one pinned table for a second vocabulary to
  // keep in step with the iota, and the pin is three lines.)
  type walkSnapshot struct {
  	ByTask   map[string]int                `json:"byTask"`
  	Revision map[string]int64              `json:"revision"`
  	Feedback map[string]string             `json:"feedback"`
  	Produced map[string]producedSubject    `json:"produced"`
  	Pending  map[string][]routedSignal     `json:"pending"`
  }

  // readyTasks returns every task whose dependsOn are all PASSED and which is not
  // already running, passed or failed — in lifecycle declaration order, which
  // ValidateLifecycle guarantees is a topological order (every dependsOn appears
  // earlier). Declaration order is what makes the fan-out deterministic under replay:
  // a map walk here would be non-determinism, and a sort by id would be a second rule
  // to keep in step with the data.
  func readyTasks(lc methodassets.Lifecycle, ws *walkState) []methodassets.LifecycleTask {
  	var out []methodassets.LifecycleTask
  	for _, t := range lc.Tasks {
  		if ws.byTask[t.ID] != taskPending {
  			continue
  		}
  		ready := true
  		for _, dep := range t.DependsOn {
  			if ws.byTask[dep] != taskPassed {
  				ready = false
  				break
  			}
  		}
  		if ready {
  			out = append(out, t)
  		}
  	}
  	return out
  }

  // DeliveryActivityWorkflow walks ONE activity's lifecycle DAG to its terminal.
  //
  // It replaces walkPhases (a flat []ActivityMethodPhase from ProfileFor, which threw
  // the task DAG away), CoAuthorArtifactWorkflow, CoAuthorPhase2ArtifactWorkflow,
  // SystemDesignPhaseWorkflow and both phase-advance wrappers. The DAG, the gates, the
  // ledger appends and the send-back machinery are one thing; 7,712 lines of near-twin
  // co-author code was the price of keeping them two.
  //
  // THE LOOP: take every ready task, run them CONCURRENTLY as workflow goroutines, and
  // wait on a Selector until one finishes. A review task suspends on its gate inside its
  // own goroutine, so a fork with a gate on one branch does not stall the other — which
  // is the whole reason `stp` and `detailedDesign` can now overlap. A join task simply
  // never becomes ready until all of its dependsOn have passed, so "the join waits for
  // all" needs no code of its own: it is readyTasks' definition.
  //
  // A SEND-BACK re-opens ONLY the judged pair: the review task and the task its
  // `reviews` field names go back to pending at revision n+1, and nothing else is
  // touched. The flat walk could not express that — it re-walked the phase list — and
  // it is the behaviour the send-back shape case asserts.
  func (wf *csWorkflows) DeliveryActivityWorkflow(ctx workflow.Context, in deliveryActivityInput) error {
  	lc, ok := methodassets.LifecycleFor(projectstate.LifecycleKeyFor(in.Activity.Type, in.Activity.Variant))
  	if !ok {
  		return temporal.NewNonRetryableApplicationError(
  			"the platform's method assets carry no lifecycle for "+projectstate.LifecycleKeyFor(in.Activity.Type, in.Activity.Variant),
  			"LifecycleUnknown", nil)
  	}
  	state := &constructState{
  		projectID: in.ProjectID, activityID: in.ActivityID, stage: StageDispatching,
  		completedPhases: map[projectstate.ActivityMethodPhase]bool{},
  	}
  	if err := workflow.SetQueryHandler(ctx, querySessionState, state.view); err != nil {
  		return err
  	}
  	wf.bindRowAccessors(state)
  	// All FOUR shared signal channels are opened here and handed to the ROUTER (Step 3a),
  	// which is the only coroutine that receives from any of them. redrafts is the one
  	// DispatchActivityTask's construction re-run lands on (Task 12).
  	decisions := workflow.GetSignalChannel(ctx, signalTaskDecision)
  	statuses := workflow.GetSignalChannel(ctx, signalCommentStatus)
  	overrides := workflow.GetSignalChannel(ctx, signalOperatorOverride)
  	redrafts := workflow.GetSignalChannel(ctx, signalRedraft)

  	reviewPolicy, err := wf.loadReviewSnapshot(ctx, in, state)
  	if err != nil {
  		return err
  	}
  	if err := wf.openActivityRow(ctx, in, state); err != nil {
  		return err
  	}
  	ws, err := wf.seedWalkFromLedger(ctx, in, lc, state)
  	if err != nil {
  		return err
  	}
  	// The router starts BEFORE the first task is scheduled, so a signal that arrives
  	// during the very first dispatch is buffered against its task rather than lost.
  	workflow.Go(ctx, func(gctx workflow.Context) {
  		wf.routeSignals(gctx, ws, decisions, statuses, overrides, redrafts)
  	})

  	type done struct {
  		taskID string
  		state  taskState
  		err    error
  	}
  	// BUFFERED to len(lc.Tasks), deliberately. On failWalk this workflow returns while
  	// sibling coroutines are still running, and an unbuffered channel would leave them
  	// blocked in Send — a leaked-coroutine warning at best. Capacity equal to the task
  	// count means every task that was ever started can always deliver its result,
  	// whether or not anyone is left to receive it.
  	results := workflow.NewBufferedChannel(ctx, len(lc.Tasks))
  	inflight := 0
  	for {
  		for _, t := range readyTasks(lc, ws) {
  			ws.byTask[t.ID] = taskRunning
  			inflight++
  			task := t
  			// The task's inbox is created HERE, before its coroutine runs, and anything
  			// the router already buffered for it is flushed in first — so a signal that
  			// beat the task to the walk arrives in order rather than being dropped.
  			inbox := ws.openInbox(ctx, task.ID)
  			workflow.Go(ctx, func(gctx workflow.Context) {
  				st, rerr := wf.runTask(gctx, in, lc, task, ws, state, reviewPolicy, inbox)
  				results.Send(gctx, done{taskID: task.ID, state: st, err: rerr})
  			})
  		}
  		if inflight == 0 {
  			return wf.finalizeWalk(ctx, in, lc, ws, state)
  		}
  		var d done
  		results.Receive(ctx, &d)
  		inflight--
  		if d.err != nil {
  			ws.byTask[d.taskID] = taskFailed
  			return wf.failWalk(ctx, in, state, d.taskID, d.err)
  		}
  		ws.byTask[d.taskID] = d.state
  		ws.closeInbox(ctx, workflow.GetLogger(ctx), d.taskID)
  		if d.state == taskSentBack {
  			wf.reopenJudgedPair(lc, ws, d.taskID)
  		}
  		// THE ONLY SAFE CONTINUE-AS-NEW POINT (Step 7). inflight == 0 means no
  		// coroutine holds a gate or a poll, so the snapshot is complete and nothing is
  		// abandoned mid-dispatch.
  		if inflight == 0 && shouldContinueAsNew(ctx) {
  			return workflow.NewContinueAsNewError(ctx, executionKindDeliveryActivity,
  				deliveryActivityInput{ProjectID: in.ProjectID, ActivityID: in.ActivityID,
  					Activity: in.Activity, Resume: ws.snapshot()})
  		}
  	}
  }

  // reopenJudgedPair is the send-back rule, and it is four lines because the data says
  // which pair: a review task names the dispatch it judges. Both go back to PENDING at
  // revision n+1 and NOTHING ELSE is touched — not the sibling branch, not a later
  // phase, not a task that already passed. The flat walk had no way to say this.
  func (wf *csWorkflows) reopenJudgedPair(lc methodassets.Lifecycle, ws *walkState, reviewTaskID string) {
  	t, ok := lifecycleTaskByID(lc, reviewTaskID)
  	if !ok || t.Reviews == "" {
  		return
  	}
  	ws.revision[t.Reviews]++
  	ws.revision[t.ID]++
  	ws.byTask[t.Reviews] = taskPending
  	ws.byTask[t.ID] = taskPending
  }
  ```
- [ ] **Step 3a: The signal router — one owner per shared channel, one inbox per task.** This is not plumbing; it is the fix for a correctness hole the concurrency creates. **The defect it prevents, stated:** with two tasks in flight on a fork — `stp` polling its pipeline while `designReview` waits at its gate — two coroutines would each be selecting on the SHARED `operatorOverride` channel. A `ReceiveChannel` delivers each message to exactly one receiver, so an override aimed at the gate is consumed by whichever coroutine the SDK schedules first and **silently lost**. Filtering inside each receiver does not fix it either: the message is already gone by the time the filter runs, and re-publishing it would put the workflow in a forwarding loop with no ordering guarantee. So no task coroutine ever touches a shared channel.
  ```go
  // deliveryTaskInboxCapacity sizes one task's inbox. 64 is far above any real backlog
  // (an operator sends single digits), and the overflow rule is the ONE thing this const
  // must not get wrong: **the router NEVER blocks.**
  //
  // WHY NOT BACK-PRESSURE, which is what a first reading suggests. If deliver blocked on
  // a full inbox, that Send would still be parked when the task retired: closeInbox
  // deletes the map entry, the coroutine that would have received is gone, no receiver
  // can ever exist for that channel again, and the ROUTER never selects a second time. One
  // wedged task would then silently swallow every later signal in the whole activity —
  // including a sibling gate's decision — and the parked message would sit in neither
  // `inbox` nor `pending`, so ContinueAsNew would lose it too. A bound whose overflow
  // behaviour can deadlock the thing that enforces it is not a bound.
  //
  // So: SendAsync, and a failed send falls back to `pending`, which is exactly where an
  // undelivered message already belongs. "A decision is never dropped" lives in `pending`
  // and never in blocking.
  const deliveryTaskInboxCapacity = 64

  // routeSignals is the ONE coroutine that receives from a shared signal channel. It
  // takes each message off whichever of the four channels carried it, reads the TaskID
  // every payload carries, and forwards it to that task's inbox — or buffers it in
  // ws.pending when the task has not been scheduled yet.
  //
  // THE ORDERING GUARANTEE, and why this is replay-safe: the router receives in signal
  // DELIVERY order (the order the history records the signals) and forwards
  // SYNCHRONOUSLY and WITHOUT EVER BLOCKING, before selecting again. So per task, the
  // order a coroutine reads its messages is exactly the order the history delivered them,
  // on every replay, with no dependence on how the SDK interleaved the task coroutines —
  // and no task can stall the router, which is what keeps that guarantee true for the
  // OTHER tasks. workflow.Selector, NewNamedBufferedChannel and Channel.SendAsync are all
  // SDK primitives the replayer schedules itself; nothing here reads a clock or a map.
  //
  // A signal for a task that has already reached its TERMINAL is logged and dropped: the
  // task it names is over, and delivering it to the next revision's coroutine would let an
  // override of revision 1 decide revision 2. closeInbox is what makes that state
  // observable — an absent inbox plus a non-pending taskState is "too late", where an
  // absent inbox plus taskPending is "too early" and buffers.
  func (wf *csWorkflows) routeSignals(ctx workflow.Context, ws *walkState, decisions, statuses, overrides, redrafts workflow.ReceiveChannel) {
  	logger := workflow.GetLogger(ctx)
  	for {
  		var msg routedSignal
  		sel := workflow.NewSelector(ctx)
  		sel.AddReceive(decisions, func(c workflow.ReceiveChannel, _ bool) {
  			var s taskDecisionSignal
  			c.Receive(ctx, &s)
  			msg = routedSignal{Kind: routedKindDecision, TaskID: s.TaskID, Decision: &s}
  		})
  		sel.AddReceive(statuses, func(c workflow.ReceiveChannel, _ bool) {
  			var s commentStatusSignal
  			c.Receive(ctx, &s)
  			msg = routedSignal{Kind: routedKindStatus, TaskID: s.TaskID, Status: &s}
  		})
  		sel.AddReceive(overrides, func(c workflow.ReceiveChannel, _ bool) {
  			var s operatorOverrideSignal
  			c.Receive(ctx, &s)
  			msg = routedSignal{Kind: routedKindOverride, TaskID: s.TaskID, Override: &s}
  		})
  		sel.AddReceive(redrafts, func(c workflow.ReceiveChannel, _ bool) {
  			var s redraftSignal
  			c.Receive(ctx, &s)
  			msg = routedSignal{Kind: routedKindRedraft, TaskID: s.TaskID, Redraft: &s}
  		})
  		sel.Select(ctx)
  		ws.deliver(logger, msg)
  	}
  }
  ```
  and on `walkState`:
  ```go
  // openInbox creates a task's inbox and flushes whatever the router buffered for it
  // BEFORE its coroutine reads, so a signal that beat the task into the walk keeps its
  // place in the order rather than being dropped or re-ordered behind a later one.
  func (ws *walkState) openInbox(ctx workflow.Context, taskID string) workflow.Channel {
  	ch := workflow.NewNamedBufferedChannel(ctx, "inbox:"+taskID, deliveryTaskInboxCapacity)
  	ws.inbox[taskID] = ch
  	ws.drainPending(taskID, ch)
  	return ch
  }

  // drainPending moves as much of a task's pending queue into its inbox as will fit, in
  // order, keeping the remainder queued. It is called at openInbox AND at the top of every
  // receive-loop iteration (Steps 5 and 6), because that is the only moment the inbox is
  // known to have just freed a slot — the router cannot wait for one, so the RECEIVER is
  // what pulls the overflow through.
  func (ws *walkState) drainPending(taskID string, ch workflow.Channel) {
  	held := ws.pending[taskID]
  	sent := 0
  	for _, m := range held {
  		if !ch.SendAsync(m) {
  			break
  		}
  		sent++
  	}
  	switch {
  	case sent == len(held):
  		delete(ws.pending, taskID)
  	case sent > 0:
  		ws.pending[taskID] = held[sent:]
  	}
  }

  // closeInbox retires a finished task's undelivered messages — from BOTH places they can
  // sit, the channel and the pending queue — so nothing is left held by a channel no
  // coroutine will ever read again and nothing is left queued for a task that is over.
  // Each is logged as too-late rather than re-queued: the task they name is finished, and
  // handing them to the next revision's coroutine would let an override of revision 1
  // decide revision 2. Draining rather than dropping silently is what makes the loss
  // auditable.
  //
  // CLEARING pending IS THE LOAD-BEARING HALF, and it is easy to leave out. A message the
  // inbox was too full to take lives in ws.pending, which `walkSnapshot` carries — so an
  // entry left behind here would ride every ContinueAsNew for the rest of the activity,
  // and on a SEND-BACK `reopenJudgedPair` puts the task back to taskPending, whose next
  // openInbox would flush that stale message into revision n+1. That is exactly the leak
  // the paragraph above exists to prevent, arriving through the other door.
  func (ws *walkState) closeInbox(ctx workflow.Context, logger log.Logger, taskID string) {
  	tooLate := func(msg routedSignal) {
  		logger.Info("signal was undelivered when its task retired",
  			"kind", msg.Kind, "taskId", taskID)
  	}
  	for _, msg := range ws.pending[taskID] {
  		tooLate(msg)
  	}
  	delete(ws.pending, taskID)

  	ch := ws.inbox[taskID]
  	delete(ws.inbox, taskID)
  	if ch == nil {
  		return
  	}
  	for {
  		var msg routedSignal
  		if !ch.ReceiveAsync(&msg) {
  			return
  		}
  		tooLate(msg)
  	}
  }

  // deliver is the router's one write, and it NEVER BLOCKS (see
  // deliveryTaskInboxCapacity's comment for the deadlock it would otherwise open). An
  // unnamed TaskID is a caller error the Manager should have refused, so it is logged
  // loudly and dropped rather than broadcast — a signal delivered to every task is worse
  // than one delivered to none.
  func (ws *walkState) deliver(logger log.Logger, msg routedSignal) {
  	switch {
  	case msg.TaskID == "":
  		logger.Error("signal names no task; dropped", "kind", msg.Kind)
  	case ws.inbox[msg.TaskID] != nil:
  		// A full inbox falls back to `pending`, which is where an undelivered message
  		// belongs anyway: the receiver drains it on its next loop iteration, and
  		// ContinueAsNew carries it.
  		if !ws.inbox[msg.TaskID].SendAsync(msg) {
  			ws.pending[msg.TaskID] = append(ws.pending[msg.TaskID], msg)
  		}
  	case ws.byTask[msg.TaskID] == taskPending:
  		ws.pending[msg.TaskID] = append(ws.pending[msg.TaskID], msg)
  	default:
  		logger.Info("signal arrived for a task that has already finished; dropped",
  			"kind", msg.Kind, "taskId", msg.TaskID, "state", ws.byTask[msg.TaskID])
  	}
  }
  ```
  `routeSignals`' last line is therefore `ws.deliver(logger, msg)` — no `ctx`, because nothing in it can block. Step 3's receive arm calls `ws.closeInbox(ctx, workflow.GetLogger(ctx), d.taskID)`.
  - [ ] **Verify first:** that every signal payload carries a `TaskID`. `taskDecisionSignal` does by construction (Task 8's Interfaces block); `phaseDecisionSignal` (`constructactivity.go:3182`) is its ancestor and already keys by gate; the construction `operatorOverride` and the design `redraft`/`setCommentStatus` payloads must be re-read (`grep -n 'operatorOverrideSignal\|redraftSignal\|setCommentStatusSignal\|type.*Signal struct' internal/manager/delivery/*.go`) and **each one that lacks a TaskID gains one**, set by its Manager-side sender. A signal with no task is the router's one drop path, and it must be unreachable from the twelve ops rather than merely logged. `workflow.NewNamedBufferedChannel` is confirmed present in SDK v1.44.0 (`workflow/deterministic_wrappers.go:128`) and `Channel.SendAsync`/`ReceiveAsync` at `internal/workflow.go:193` — re-confirm both with `go doc go.temporal.io/sdk/workflow.Channel` before writing, because `SendAsync` returning `false` is the whole overflow path and `ReceiveAsync` is what makes `closeInbox` drain rather than abandon.

  - [ ] **Verify first:** `workflow.NewBufferedChannel` + `workflow.Go` is the Temporal Go SDK's concurrency primitive and is replay-deterministic BECAUSE the SDK schedules the coroutines itself — do not use a native `go` statement or a `sync.WaitGroup`, either of which is non-determinism. Read the SDK's `workflow.Go` doc before writing, and confirm `Channel.Receive` blocks the coroutine rather than the worker. Also confirm `constructState.view` satisfies the query handler's signature and that `StageDispatching` is in scope. `ws.snapshot()` and its inverse `walkStateFrom(ctx, *walkSnapshot)` go beside `walkState`; `snapshot()` copies the four walk maps plus `pending` and **never** `inbox`, and `walkStateFrom` re-creates `inbox` empty. `seedWalkFromLedger` must take `in.Resume` and **prefer it over the ledger** when non-nil, because the snapshot knows the in-memory feedback, produced refs and undelivered signals that the ledger does not record per task.

- [ ] **Step 4: Write `runTask` — the one place a task's kind is read**, and the gate:
  ```go
  // runTask runs ONE task to its terminal. It is the only function in the walk that
  // reads a task's kind, and it reads only the kind — never the activity type.
  //
  // A DISPATCH task: produce, record the attempt, done. Its gate belongs to the REVIEW
  // task that judges it, which is a separate node.
  // A REVIEW task: open the round with the roster the engine proposes, run the agent
  // reviewers the data names, then either auto-pass (the engine says no human is
  // required) or SUSPEND on the gate signal. Approve commits; send back records the
  // verdict and its comments and returns taskSentBack, which is what re-opens the pair.
  // inbox is THIS task's channel and the only one it may read (Step 3a). It carries all
  // four signal kinds for this task and nothing for any other, so nothing downstream
  // filters by TaskID and nothing can steal a sibling's message.
  func (wf *csWorkflows) runTask(
  	ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle,
  	t methodassets.LifecycleTask, ws *walkState, state *constructState,
  	policy projectstate.ReviewPolicy, inbox workflow.ReceiveChannel,
  ) (taskState, error) {
  	strat, err := strategyFor(wf.Strategies, wf, lc, t)
  	if err != nil {
  		return taskFailed, err
  	}
  	phase, _ := lifecyclePhaseByID(lc, t.Phase)
  	tc := taskContext{In: in, Task: t, Phase: phase, Revision: ws.revision[t.ID], State: state, Feedback: ws.feedback[t.ID]}

  	produced, perr := strat.Produce(ctx, tc)
  	if perr != nil {
  		return taskFailed, perr
  	}
  	if err := wf.recordTaskAttempt(ctx, in, t, tc, produced, state); err != nil {
  		return taskFailed, err
  	}
  	if t.Kind == methodassets.LifecycleTaskDispatch {
  		// The dispatch's output is what its REVIEW task will cite. Recorded here, keyed
  		// by this task's id, so runGate can reach it through t.Reviews — a review task
  		// produces nothing of its own, and without this hand-off every round would cite
  		// the third rung of gateSubjectRef's ladder.
  		ws.produced[t.ID] = produced
  		return taskPassed, nil
  	}
  	return wf.runGate(ctx, in, lc, t, tc, ws, state, policy, inbox)
  }

  // runGate is the review half: the roster, the agent reviewers, and the suspend.
  //
  // The ROSTER comes from the reviewEngine, ONE injected instance (wf.Review — the two
  // inline review.NewReviewEngine() constructions in the co-author rails are deleted in
  // stage 4b1), called with the activity type and the task's own PHASE. That phase is
  // what deletes the third copy of the design slot->activity/phase table: the lifecycle
  // data already answers what designActivityFor answered, because missionReview.phase is
  // "mission" and sdpReview.phase is "sdp".
  //
  // The AGENT REVIEWERS are the review task's own command, when it has one. In v0.9.0
  // missionReview, glossaryReview, coreUseCasesReview and architectureReview each carry
  // a *-critique command and a workerClass, and volatilitiesReview carries neither —
  // which is exactly the one reviewer-less row the engine's phaseVolatilities constant
  // already names. So the critique round is not a special case any more: it is a review
  // task with a command.
  func (wf *csWorkflows) runGate(
  	ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle,
  	t methodassets.LifecycleTask, tc taskContext,
  	ws *walkState, state *constructState, policy projectstate.ReviewPolicy,
  	inbox workflow.ReceiveChannel,
  ) (taskState, error) {
  	set, err := wf.proposeReviewSet(in, t, tc.Phase, policy, state)
  	if err != nil {
  		return taskFailed, err
  	}
  	// The round's SUBJECT and its KIND both come from the task this review JUDGES, not
  	// from the review task itself.
  	//
  	// The subject: ws.produced[t.Reviews] is the staged ref and attempt id the judged
  	// dispatch reported. A review produces nothing of its own, so reading its own
  	// producedSubject here would hand gateSubjectRef two empty strings and every round
  	// would cite the ladder's third rung.
  	//
  	// The kind: MEASURED in method-assets v0.9.0 — NO review task carries an
  	// artifactKind of its own except `sdpReview`; missionReview, glossaryReview,
  	// volatilitiesReview, coreUseCasesReview, architectureReview, srsReview,
  	// designReview, codeReview, stpReview and testing all carry "". So the kind is the
  	// JUDGED task's, and `sdpReview` — which names no `reviews` target — is the one
  	// task that answers with its own. Pinned by
  	// Test_ReviewRounds_KindComesFromTheJudgedTask over all fourteen lifecycles.
  	judged := ws.produced[t.Reviews]
  	kind := roundArtifactKind(lc, t)
  	if err := wf.openRound(ctx, in, t, tc, judged, kind, set, state); err != nil {
  		return taskFailed, err
  	}
  	if t.Command != "" {
  		if err := wf.runAgentReviewers(ctx, in, t, tc, set, state); err != nil {
  			return taskFailed, err
  		}
  	}
  	if set.RequiresHuman == nil || !*set.RequiresHuman {
  		return wf.passRound(ctx, in, lc, t, tc, state, gateActorSystem, reasonOf(set))
  	}
  	state.stage = StageAwaitingReview
  	return wf.awaitTaskDecision(ctx, in, lc, t, tc, ws, state, inbox)
  }

  // roundArtifactKind resolves what a review round judges: the review task's own
  // artifactKind when it has one (only `sdpReview` does), else the artifactKind of the
  // task its `reviews` field names. Returns nil for a construction round, whose subject
  // is a commit and not a slot model, which is exactly what ReviewRound.ArtifactKind's
  // optionality means.
  func roundArtifactKind(lc methodassets.Lifecycle, t methodassets.LifecycleTask) *projectstate.ArtifactKind {
  	name := t.ArtifactKind
  	if name == "" && t.Reviews != "" {
  		if judged, ok := lifecycleTaskByID(lc, t.Reviews); ok {
  			name = judged.ArtifactKind
  		}
  	}
  	if name == "" {
  		return nil
  	}
  	k, ok := projectstate.ArtifactKindFromWireName(lowerFirstRune(name))
  	if !ok {
  		return nil
  	}
  	return &k
  }
  ```
  `openRound` therefore takes `(judged producedSubject, kind *projectstate.ArtifactKind)` and builds its `ReviewRoundInput` with `SubjectRef: gateSubjectRef(gf, judged.StagedRef, judged.AttemptID)` and `ArtifactKind: kind` — the two facts Tasks 3 and 5 added, both sourced from the judged task and neither from the reviewer.
  - [ ] **Verify first:** `proposeReviewSet`'s current signature (`constructactivity.go:2913`) takes `(in constructActivityInput, phase projectstate.ActivityMethodPhase, policy, state)` and calls `wf.Review.ProposeReviews(..., review.ActivityType(in.Activity.activityTypeName()), phase.String(), in.Activity.ComponentID, engineReviewPolicy(policy), state.floorTouched, state.reviewContracts)`. Widen it to take the lifecycle task and the phase STRUCT and pass `tc.Phase.ID` where `phase.String()` was. **The framing matters**: do NOT assert that a phase id round-trips through `projectstate.ActivityMethodPhase`, because that enum has only five values (`requirements`, `detailed_design`, `test_plan`, `construction`, `integration`) and none of `mission`/`glossary`/`volatilities`/`coreUseCases`/`architecture`/`sdp` is one of them. The claim that must hold is narrower and is about the ENGINE: **every lifecycle phase id in v0.9.0 is a string the `reviewEngine` already reasons about** — `reviewengine.go`'s `phaseRequirements`/`phaseDetailedDesign`/`phaseTestPlan`/`phaseConstruction`/`phaseIntegration`/`phaseVolatilities`/`phaseSDP` (`:110-122`) are the seven it names, and every OTHER phase id must fall to the rail's default row rather than to a silent miss. Write it as a table over all fourteen lifecycles' phase ids asserting that `ProposeReviews` returns a NON-EMPTY reviewer set (or an explicitly reviewer-less one, which is `volatilities` alone) for each — an empty roster with no reason is precisely the live defect stage 0 fixed once already, and the engine's third parameter being a `lifecyclePhase string` is what makes it re-testable.

- [ ] **Step 5: Write `awaitTaskDecision`.** Carry forward the existing gate's vocabulary and budgets verbatim — `maxPhaseRedrafts = 5` (`constructactivity.go:817`), the outcome strings at `:1609-1615`, the human-stage bookkeeping (`enterPhaseGate`, `enterHumanStage`, `leaveHumanStage`, `humanGateClass`, `gateMetrics`), and the `resolve-before-decision` ordering (`coauthorartifact.go:588`: a comment-status message is applied BEFORE a decision is acted on, so a resolve lands first).
  **Every iteration of its loop begins with `ws.drainPending(t.ID, inbox)`** — the router cannot wait for a slot (Step 3a: it never blocks), so the RECEIVER is what pulls an overflowed message through, and the one moment a slot is known to have freed is just after a receive. Skip that call and a 65th signal sits in `pending` until the task retires.
  It reads **ONE channel — its own inbox** — and dispatches on `msg.Kind`: `routedKindStatus` applies the comment status and loops (this is where `resolve-before-decision`'s guarantee now comes from, and it is stronger than the old registration order because the router already put the messages in history order); `routedKindOverride` takes the operator arm; `routedKindDecision` decides the gate; `routedKindRedraft` re-dispatches the judged task (Task 12's re-run). **No `TaskID` filter appears here** — the inbox is per-task by construction, which is what replaces the construction gate's filter-by-gate-KEY and the design session's implicit one-session-one-kind assumption. Note in the doc comment that a send-back past the budget **keeps awaiting** rather than failing, exactly as `sendBackGate` does today, and that changing that is not in this wave.

- [ ] **Step 6: The capped-backoff observe loop** (R-L, mitigation i). Today both observe loops are a flat `15 s × 240` — `observeDesignJob` (`coauthorartifact.go:2593`/`:2600`) and `runPipeline` (`constructactivity.go:821`/`:824`) — and each poll costs ≈4 history events (timer started, timer fired, activity scheduled, activity completed), so a stuck job burns ≈960 events before its 1-hour ceiling. One child now holds ELEVEN of those for a `requirements` activity. Replace the flat interval with a capped ladder that keeps the ceiling:
  ```go
  // The observe ladder. Fast while a job plausibly finishes in a minute, then coarse,
  // because a job that has run ten minutes will not finish in the next fifteen seconds
  // and each extra poll is four durable events in a history that now holds a whole
  // activity rather than one artifact.
  //
  //	4 polls × 15s  =   1 min   (the common case: a job that just finished)
  //	9 polls × 60s  =   9 min   (to the 10-minute mark)
  //	10 polls × 300s = 50 min   (to the same 1-hour ceiling the flat loop had)
  //	-------------------------------------------------------------------
  //	23 polls ≈ 92 events, against 240 polls ≈ 960. Same ceiling, one tenth the history.
  const (
  	observeFastInterval = 15 * time.Second
  	observeFastPolls    = 4
  	observeMedInterval  = 60 * time.Second
  	observeMedPolls     = 9
  	observeSlowInterval = 300 * time.Second
  	observeSlowPolls    = 10
  	maxObserveTotalPolls = observeFastPolls + observeMedPolls + observeSlowPolls // 23
  )

  // observeInterval is the ladder as a pure function of the poll index, so the schedule
  // is one readable table rather than three loops.
  func observeInterval(poll int) time.Duration {
  	switch {
  	case poll < observeFastPolls:
  		return observeFastInterval
  	case poll < observeFastPolls+observeMedPolls:
  		return observeMedInterval
  	}
  	return observeSlowInterval
  }
  ```
  and make the wait a **selector over the timer and THIS TASK'S INBOX**, so a backoff is never something an operator has to sit through — and never something a sibling task's coroutine can steal a message through (Step 3a):
  ```go
  	// The wait is a Selector over the backoff timer AND this task's own inbox. A
  	// five-minute slow poll is correct for a machine and intolerable for a person, and
  	// an override or a redraft arriving mid-backoff must be acted on when it lands, not
  	// when the timer expires.
  	//
  	// IT READS THE INBOX, NOT A SHARED SIGNAL CHANNEL, and that is not a style choice: on
  	// a fork this coroutine runs beside a GATE's coroutine, and two receivers on one
  	// ReceiveChannel means the SDK hands each message to exactly one of them — an override
  	// meant for the gate would be eaten here and silently lost. The router (Step 3a) is
  	// what makes this channel per-task, and a message on it is always for this task.
  	//
  	// A PUSHED job-completion signal would remove the polling entirely, but nothing
  	// produces one today — that is 4b2's, with the RA change it needs, and this selector
  	// is the seam it will plug into.
  	// deferred holds the messages this loop received and cannot act on. It is declared
  	// ONCE, outside the poll loop, and flushed back through ws.deliver just before the
  	// dispatch returns — so closeInbox logs them too-late rather than the walk silently
  	// eating them. Local, so nothing re-offers them and nothing can spin.
  	var deferred []routedSignal
  	defer func() {
  		for _, m := range deferred {
  			ws.deliver(workflow.GetLogger(ctx), m)
  		}
  	}()

  	// Same rule as the gate's loop (Step 5): pull any overflowed message through before
  	// waiting, because the router never blocks and this is the one moment a slot is known
  	// to have freed.
  	ws.drainPending(t.ID, inbox)
  	sel := workflow.NewSelector(ctx)
  	sel.AddFuture(workflow.NewTimer(ctx, observeInterval(poll)), func(workflow.Future) { /* poll again */ })
  	sel.AddReceive(inbox, func(c workflow.ReceiveChannel, _ bool) {
  		c.Receive(ctx, &msg)
  		// An override or a redraft interrupts the poll. A DECISION or a COMMENT STATUS is
  		// a MISROUTE, not a queue: a dispatch task has no gate, so nobody in this task's
  		// lifetime will ever act on it. It is held in a LOCAL slice for the rest of that
  		// lifetime and handed back to the router on the way out, where closeInbox logs it
  		// too-late — which is the honest record of a signal nothing could act on.
  		//
  		// DO NOT put it back in ws.pending. drainPending would push it straight into this
  		// same inbox on the next iteration, whose receive arm is immediately ready, and
  		// each iteration builds a fresh workflow.NewTimer — a spin that grows durable
  		// history exactly as fast as the walk can loop, which is the failure R-L's budget
  		// exists to bound. A local slice cannot spin because the router never re-offers it.
  		interrupted = msg.Kind == routedKindOverride || msg.Kind == routedKindRedraft
  		if !interrupted {
  			deferred = append(deferred, msg)
  		}
  	})
  	sel.Select(ctx)
  ```
  - [ ] **Verify first:** the existing loops' escalation behaviour on exhaustion — `runPipeline` escalates through `handleVariance` (`constructactivity.go:2945`) and `observeDesignJob` routes to the failed-draft gate. **The ceiling and both escalations are unchanged**; only the poll SCHEDULE moves. Re-point both `maxObservePolls` and `maxPipelinePolls` at `maxObserveTotalPolls` and delete the two flat interval consts, so there is one ladder and not two that drift.

- [ ] **Step 7: `ContinueAsNew` on a history budget** (R-L, mitigation ii). The check, called at the one point Step 3 marks:
  ```go
  // deliveryActivityHistoryBudget is the child's own ceiling, below the server's. The
  // server's suggestion (GetContinueAsNewSuggested) is the primary signal because it
  // knows the namespace's real limits; the budget is the floor for a deployment that
  // does not set one, and 4000 leaves room for the ~92-event dispatch Step 6 produces
  // plus a gate, so a continue never lands mid-task.
  const deliveryActivityHistoryBudget = 4000

  // shouldContinueAsNew is checked ONLY where inflight == 0: no coroutine holds a gate
  // or a poll, so walkSnapshot is complete and nothing is abandoned mid-dispatch.
  //
  // Why the child needs this AT ALL, when neither rail it replaces did: today
  // SystemDesignPhaseWorkflow starts a CHILD co-author per kind, so a `requirements`
  // activity is FOUR histories (measured: 187 and 311 events per co-author fixture, in
  // a test env whose poll returns instantly). The generic child collapses all eight
  // tasks and three critique dispatches into ONE execution. Collapsing the histories is
  // the point of the wave; letting one of them grow unbounded is not.
  func shouldContinueAsNew(ctx workflow.Context) bool {
  	info := workflow.GetInfo(ctx)
  	return info.GetContinueAsNewSuggested() || info.GetCurrentHistoryLength() > deliveryActivityHistoryBudget
  }
  ```
  - [ ] **Verify first:** both accessors exist in the pinned SDK — `go.temporal.io/sdk v1.44.0`. They are methods on `WorkflowInfo` in **`internal/workflow.go`** (`:1581` `GetCurrentHistoryLength`, `:1594` `GetContinueAsNewSuggested`), re-exported through the type alias in `workflow/`, so grep the internal file or ask `go doc` — grepping `workflow/workflow.go` returns zero hits and reads as if the claim were false:
    ```bash
    cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
    grep -n 'GetContinueAsNewSuggested\|GetCurrentHistoryLength' $(GOWORK=off go env GOMODCACHE)/go.temporal.io/sdk@v1.44.0/internal/workflow.go
    GOWORK=off go doc go.temporal.io/sdk/workflow.Info | grep -n 'ContinueAsNewSuggested\|CurrentHistoryLength'
    ```
    `ContinueAsNew` re-runs the entry func, so: `seedWalkFromLedger` prefers `in.Resume`; `SetQueryHandler` and all FOUR shared signal channels are re-established; the ROUTER restarts and its per-task inboxes are re-created empty, which is exactly why **`walkSnapshot` carries `Pending`** — at `inflight == 0` no task holds an inbox, so every message the router has taken off a shared channel and not yet delivered lives in `ws.pending` and would otherwise be dropped by the continue. **And at `inflight == 0` the router itself holds NO message** — which is true only because `deliver` never blocks (Step 3a): every routed message is by then in an inbox, in `pending`, or logged as undeliverable, so the snapshot is complete and there is no in-flight send for the continue to lose. A blocking `deliver` would have made that sentence false and this check unsafe. (The alternative — drain the shared channels before continuing — was rejected: a `ReceiveChannel` cannot be drained to a known-empty state without a `ReceiveAsync` loop whose result depends on delivery timing, which is exactly the non-determinism the router exists to remove.) `bindRowAccessors` re-binds against the re-read row.

- [ ] **Step 8: Register the child.** In `deliverymanager.go`: `executionKindDeliveryActivity = "deliveryActivity"` in the construction-rail const block; `deliveryActivityWorkflowID`; `deliveryActivityInput` (with `Resume`); `walkSnapshot`; `routedSignal` with its four `routedKind*` consts; `taskDecisionSignal`; `signalTaskDecision = "taskDecision"`; the `TaskID` added to any of `commentStatusSignal`/`operatorOverrideSignal`/`redraftSignal` that lacks one, **with its Manager-side sender setting it** (Step 3a's verify note lists them); `wf.Strategies` defaulted to `productionStrategies()` in `csNewWorkflows` (`:9779`); the `genRegisteredWorkflow` entry in `constructionManager.WorkerManifest()` (`:10259`). Nothing calls `deliveryActivityWorkflowID` from the pump yet — Task 9 does.

- [ ] **Step 9: Add the walker arch guard** (R-C). In `manager_test.go`:
  ```go
  // Test_DeliveryActivityWalker_NamesNoTypeOrCommand is architect Ruling 3(a)'s guard,
  // scoped to the WALKER FUNCTIONS rather than to the file — because arch.CheckFileLayout
  // forbids a workflow.Context-taking func anywhere but a workflow file, so the strategies
  // and the walker must share deliveryactivity.go.
  //
  // The failure it prevents: a walker that grows `if activityType == ActivityTypeService`
  // is a god-workflow worse than the two types it replaced, and §9's smaller-than-sum
  // acceptance is what pays for it. The technique is paramguard_arch_test.go's — key the
  // bodies by receiver + name through go/ast — and it fails LOUDLY on a func it cannot
  // find, so a rename cannot make it vacuous.
  func Test_DeliveryActivityWalker_NamesNoTypeOrCommand(t *testing.T) {
  	walkers := []string{"DeliveryActivityWorkflow", "readyTasks", "reopenJudgedPair", "runTask"}
  	banned := []string{"ActivityType", "ArtifactKind", "Kind" + "Mission", "projectstate.Command"}
  	// … parse deliveryactivity.go, locate each walker, walk its body, fail on a banned
  	// identifier or on any string literal that appears in method-assets' command set.
  }
  ```
  Expected: green, and **mutation-checked** — adding `_ = projectstate.ActivityTypeService` inside `runTask` must fail it, and renaming a walker must fail it with "not found".

- [ ] **Step 10: Re-point Task 1's shape cases, flip the skips, and add the `ContinueAsNew` case.** In `manager_test.go`, `newShapeRig` registers `DeliveryActivityWorkflow` under `executionKindDeliveryActivity` instead of the old child **and overrides `wf.Strategies`** with:
  ```go
  // stubStrategy fills the dispatch slot while Tasks 10 and 11 are unwritten, so the
  // DAG can be proved before either real dispatch implementation exists. It is the
  // reason strategyFor reads an injectable registry instead of hard-coding two arms.
  // Deleted in Task 11 Step 5, when the real strategies drive the same cases.
  func stubStrategies(rec *shapeRecorder) strategyRegistry {
  	reg := productionStrategies()
  	reg[strategySlotDispatch] = func(*csWorkflows) taskStrategy { return stubStrategy{rec: rec} }
  	return reg
  }
  ```
  Then flip `fork-join-service-stp-first`, `fork-join-service-design-first` and `join-waits-for-all` from `shapeFailsUntilTheDAG` to `shapePassesToday`, and add an eighth case:
  ```go
  		{
  			// CONTINUE-AS-NEW MID-WALK (R-L). Drives a service walk with the history
  			// budget set to a value the walk crosses, and asserts that the child
  			// continues-as-new, that it continues with inflight == 0 and NEVER mid-
  			// dispatch, and that the resumed run finishes the SAME walk — same tasks
  			// passed, same revisions, same feedback carried, no task re-dispatched.
  			// A continue that lost the feedback map would silently redraft against the
  			// prompt that was just rejected.
  			name: "continue-as-new-mid-walk", typeKey: "service",
  			drive: driveContinueAsNewMidWalk, wantToday: shapePassesToday,
  		},
  		{
  			// SIGNAL ROUTING ON A FORK (Step 3a). The case that would have caught the
  			// hole: `stp` is mid-poll while `designReview` waits at its gate, and an
  			// operator override is sent naming the GATE. Asserts the gate acted on it
  			// and the polling task did NOT — which two receivers on one shared
  			// ReceiveChannel could never guarantee, because the SDK hands each message
  			// to exactly one of them. Also drives a signal for a task that has not
  			// started yet (buffered in ws.pending, delivered in order at openInbox) and
  			// one for a task that has already finished (logged, dropped, and NOT
  			// delivered to the next revision's coroutine).
  			name: "fork-signal-reaches-the-named-task", typeKey: "service",
  			drive: driveForkSignalRouting, wantToday: shapePassesToday,
  		},
  		{
  			// A FULL INBOX MUST NOT WEDGE THE ROUTER (Step 3a's overflow rule). Sends 65
  			// signals — one more than deliveryTaskInboxCapacity — to a task that then
  			// retires WITHOUT draining them, and asserts that the NEXT task's decision is
  			// still delivered and the walk still reaches its terminal. With a blocking
  			// Send this deadlocks: the parked send outlives closeInbox, no receiver can
  			// ever exist for that channel again, the router never selects a second time,
  			// and every later signal in the activity is silently swallowed. It also
  			// asserts the drained messages were LOGGED as too-late rather than vanishing,
  			// and that nothing is left in `pending` for the retired task — which is true
  			// only because closeInbox clears pending as well as the channel. Leave that
  			// half out and the 65th signal rides every later ContinueAsNew and, after a
  			// send-back, gets flushed into revision n+1 by the next openInbox.
  			name: "full-inbox-does-not-wedge-the-router", typeKey: "service",
  			drive: driveInboxOverflow, wantToday: shapePassesToday,
  		},
  ```
  `shapeRecorder` gains `signalDelivered(taskID, kind string)`, called from `walkState.deliver`, so the assertion is about WHICH task received a message and not merely about the walk's terminal — a case that only checked the terminal would pass with the override lost, because a gate that never receives an override just waits for its decision instead.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_LifecycleShapes' -count=1 -v 2>&1 | tail -25
  ```
  Expected: `linear-deployment`, both `fork-join-service-*`, `sendback-reopens-only-the-judged-pair`, `join-waits-for-all`, `continue-as-new-mid-walk`, `fork-signal-reaches-the-named-task` and `full-inbox-does-not-wedge-the-router` **PASS against the stub dispatch strategy**; `m0-no-sendback` and `human-floor-under-vibes` still run against the OLD rails (Tasks 9 and 10 move them). The two branch-order cases must produce the **SAME `TaskOrder`** (declaration order — assert it, so a data reorder is caught) and **DIFFERENT `CompletedOrder`**, with both fork tasks appearing in `TaskOrder` before either appears in `CompletedOrder`. If `CompletedOrder` is identical in both, the coroutines are being serialized and the fork is not real.

- [ ] **Step 11: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_LifecycleShapes|Test_DeliveryActivityWalker|Test_TaskStateOrdinalsNeverRenumber|Test_Replay' -count=1 -v 2>&1 | tail -40
  GOWORK=off go test ./internal/ -run 'TestFileLayout|TestRegisteredTemporalNamesGolden|TestRegisteredTemporalNamesGolden_FrozenWorkflowNames' -count=1
  GOWORK=off go test -short -count=1 ./...
  golangci-lint cache clean && GOWORK=off make lint fix-check
  ```
  Expected: the shape cases as Step 10 predicts; the arch guard green and mutation-checked; **nineteen replays green** (a new workflow type adds nothing to an existing history); `TestFileLayout` green — if it reports `workflow-file-name`, the entry func and the file name disagree, and the file name is what must change; the golden regenerated to **141** (139 base + `deliveryRoundSweep` from Task 6 + `deliveryActivity` here); the frozen-names list still green, because this task DELETES no name.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1
  git add server
  git commit -F - <<'MSG'
  feat(delivery): one child walks any lifecycle DAG

  deliveryActivity at {projectId}:activity:{activityId} reads
  methodassets.LifecycleFor(key).Tasks and walks dependsOn. Ready tasks run as
  Temporal workflow goroutines on a Selector, so a gate on one branch no longer
  stalls the other — which is why `stp` and `detailedDesign` can finally overlap.
  The join needs no code: a task with several dependsOn simply never becomes ready
  until all of them pass, which is readyTasks' definition. A send-back re-opens ONLY
  the judged pair, because the data says which pair — a review task names the
  dispatch it judges.

  Per-type behaviour is data plus a strategy seam, and the architect's ruling is
  explicit that pretending it is data ALONE is wrong: an interactive design session,
  a CI poll/variance loop and a server-side computation are three real
  implementations behind one interface the walker never branches on. strategyFor
  chooses a SLOT on lifecycle FIELDS — kind, command, workerClass, artifactKind,
  reviews — and reads it out of ONE injectable registry, so a slot whose real
  implementation is two tasks away has somewhere for a stub to live. The third row is
  what makes deterministic Project Design generic rather than special: a review task
  that names no `reviews` target has its subject produced by the computation
  registered for its artifactKind, because ValidateLifecycle permits exactly that
  shape and modelling the compute as a task would have been a method-assets release
  inside this wave.

  A review round's subject and its artifact kind both come from the task it JUDGES,
  never from itself: a review produces nothing, and — measured — no review task in
  v0.9.0 carries an artifactKind of its own except sdpReview. So walkState carries the
  judged task's staged ref and attempt id forward, and without that hand-off every
  round would cite the fallback rung and claim to judge nothing.

  ONE coroutine owns each signal channel and forwards to per-task inboxes. Two tasks in
  flight means two coroutines, and a ReceiveChannel hands each message to exactly one
  receiver — so a shared override channel read from inside a polling dispatch task would
  eat the override meant for the gate running beside it, silently. The router receives in
  history order and forwards synchronously, so per task the inbox order IS the history
  order on every replay; a message for an unstarted task is buffered and flushed in order
  when it starts, and one for a finished task is logged rather than handed to the next
  revision's coroutine.

  It forwards with SendAsync and never blocks, which is not an optimisation. A Send parked
  on a full inbox would outlive its task's retirement: no receiver could ever exist for
  that channel again, the router would never select a second time, and one wedged task
  would swallow every later signal in the activity while the parked message sat outside
  both the inbox and the pending queue where continue-as-new could not carry it. A bound
  whose overflow can deadlock the thing enforcing it is not a bound. So overflow goes to
  pending, the receiver drains it, and a retiring task drains its own channel into the log.

  And it CONTINUES AS NEW. Today SystemDesignPhaseWorkflow starts a child co-author
  per kind, so a requirements activity is four histories; collapsing them is the point
  of the wave, and letting one of them grow unbounded is not. Measured: 187 and 311
  events per co-author fixture in a test env whose poll returns instantly, against
  eleven dispatches each polling 15s x 240 at four events a poll. So the observe loop
  backs off — 23 polls where there were 240, same one-hour ceiling, a tenth of the
  history — and the walk continues-as-new on the server's own suggestion or a 4000
  event budget, at the one point where nothing is in flight.

  The roster comes from ONE injected reviewEngine, called with the task's own PHASE —
  which deletes the third copy of the design slot->phase table outright, since
  missionReview.phase is "mission" and sdpReview.phase is "sdp". The critique round
  stops being special too: it is a review task with a command, and
  volatilitiesReview's lack of one is the reviewer-less row the engine already names.

  Guarded by an arch test over the four walker funcs: no ActivityType, no
  ArtifactKind, no command literal. The moment a walker branches on a type it is a
  god-workflow worse than the two it replaced.

  Nothing starts it yet. The two dispatch strategies refuse, naming the task that
  fills them, and the fork/join shape cases are proved against a stub — which is how
  the DAG is tested before either dispatch implementation exists.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 9: Deterministic Project Design — the child's first instance (spec §6)

The `projectDesign` lifecycle is ONE review task with `dependsOn: []`, no `command`, no `workerClass` and `artifactKind: SdpReview` — the simplest possible instance of the generic child, and the one that proves the gate, the round, the commit and the phase advance before any fork exists. Its subject is COMPUTED: the child derives the plan, stages it as the SDP artifact, opens the M0 review with the engine's non-overridable human floor (`reviewengine.go:432`), and on approve materialises the plan and lifts M0 (R4). **There is no send-back** — to change the plan you amend the Architecture, and re-opening Architecture makes the child recompute rather than ask for an acknowledgement.

R-E is the scope ruling: the compute writes slots **9, 10, 11–14, 15 and 16**; slot **8** (`planningAssumptions`) is the one authored input it READS, and **DEFAULTS when absent** rather than refusing on (controller ruling — Step 3a has the defaults and their sources).

**Files:**
- Modify: `server/internal/manager/delivery/deliveryactivity.go` — `sdpComputeStrategy`, the M0 gate-passed handler.
- Modify: `server/internal/manager/delivery/assemblesdpreview.go` — DELETE the workflow, the `SDPRejectAll` arm, `maxSDPReassembleAttempts`, `stageReview`/`commitReview`/`rejectReview`; KEEP `assembleSdpReview` and everything under it, re-receivered.
- Modify: `server/internal/manager/delivery/deliverymanager.go` — the four solution dial-sets, the RiskModel join, `RequestSDPCommit`/`AdvanceToConstruction` deleted, `submitProjectDesignDecision` re-routed, `pumpnextactivity.go`'s design-skip narrowed.
- Delete: `server/internal/manager/delivery/phase2advance.go`
- Modify: `server/internal/manager/delivery/manager_test.go`

**Interfaces produced (Task 13 keeps these; Task 14 reads the M0 verbs):**
- The registry entry `strategySlotCompute("SdpReview"): func(wf *csWorkflows) taskStrategy { return sdpComputeStrategy{wf: wf} }`, added to `productionStrategies()`.
- `type solutionDials struct { StaffingCap int; BufferDays, CriticalSpeedup float64 }` and `func derivedSolutionDials(kind projectstate.ArtifactKind, staffingCap int) (solutionDials, bool)` — **two parameters**, because every dial set is relative to the normal option's cap.
- `const subcriticalStaffingCut = 2`, `compressedCriticalSpeedup = 1.8`, `decompressedBufferDays = 20.0`.
- `func defaultPlanningAssumptions(proj projectstate.Project, al projectstate.ActivityList) (projectstate.PlanningAssumptions, []string)` — The Method's defaults plus the names of the families it defaulted, used when slot 8 is uncommitted (R-E, controller override). It takes the whole project because `InfrastructureKind` reads the committed operationalConcepts, and the activity list because `Resources` is the roles the derived plan actually uses.
- `func (wf *csWorkflows) computeProjectPlan(ctx workflow.Context, in deliveryActivityInput, state *constructState) (ref string, defaulted []string, err error)` — returns the staged ref AND the names of any assumption families it had to default, which the attempt's `Detail` records and the M0 copy line reads.
- `func riskModelFrom(rows []projectstate.SdpOptionRow, risks map[projectstate.ArtifactKind]estimation.RiskScore, recommendation projectstate.ArtifactKind) projectstate.RiskModel` — **three parameters**, and the recommendation is an **`ArtifactKind`, not an `OptionID`**: measured, `RiskModel.Recommendation` is an `ArtifactKind` (`contract.gen.go:819`), the same vocabulary `Rows[].SolutionKind` uses, while `SdpReview.Recommendation` is an `OptionID`. The two slots name the chosen option in two vocabularies and the join must not mix them.

- [ ] **Step 1: Measure the compute's inputs and confirm the dial table reproduces the committed state.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1
  python3 -c "
import json;d=json.load(open('.aiarch/state/project.json'))
for i in (11,12,13,14):
    m=d['slots'][str(i)]['model']
    print(i, m['slotKind'], 'cap',m['staffingCap'],'cal',m['calendarDaysPerWeek'],'buf',m['bufferDays'],'speedup',m['criticalSpeedup'])
print('slot8 keys:', list(d['slots']['8']['model'].keys()))
print('slot15 rows:', [r['solutionKind'] for r in d['slots']['15']['model']['rows']])
for i in range(8,17): print(i, 'status',d['slots'][str(i)]['status'],'rev',d['slots'][str(i)].get('revisions'),'stale',d['slots'][str(i)].get('staleBasis'))"
  cd server
  sed -n '355,415p' internal/manager/delivery/assemblesdpreview.go   # assembleOption: which Solution fields it reads
  grep -n 'func deriveClassRates' -A 14 internal/manager/delivery/assemblesdpreview.go
  grep -n 'func SolutionKinds' -A 9 internal/resourceaccess/projectstate/projectstateaccess.go
  grep -n 'func MaterializeActivityPlan' -A 18 internal/manager/delivery/deliverymanager.go
  ```
  Expected: the four dial rows exactly as R-E states them (cap 6/0/1, cap 4/0/1, cap 6/0/1.8, cap 6/20/1, all with `calendarDaysPerWeek: 0`); slot 8 carrying `resources, calendarDaysPerWeek, infrastructureKind, declaredUsage, terms, notes, indirectDailyRate, rateCard`; slot 15 holding one row per solution kind; slots 11–16 all `staleBasis: true` with `revisions: 1` — written once and stale ever since, which is the posture §6 exists to end. And `assembleOption` reading **only** `sol.StaffingCap`, `sol.BufferDays`, `sol.CriticalSpeedup` (its `ClassRates` come from `deriveClassRates(pa, classes)` and the per-option calendar was retired by F5) — **if it reads a fourth `sol` field, the dial table needs that field too and Step 2's struct is wrong.**

- [ ] **Step 2: Write the dial table and the RiskModel join.** In `deliverymanager.go` (pure, no workflow context):
  ```go
  // solutionDials is everything a Solution slot contributes to an assembled option.
  // MEASURED, not assumed: assembleOption reads sol.StaffingCap, sol.BufferDays and
  // sol.CriticalSpeedup and NOTHING ELSE — its ClassRates come from deriveClassRates(pa,
  // classes) and the per-option CalendarDaysPerWeek "cheat" (compressed silently
  // switching 2 -> 5 d/wk) was retired by the Phase-2 rework's F5. So the four
  // agent-drafted solution slots were four sets of three numbers.
  type solutionDials struct {
  	StaffingCap     int
  	BufferDays      float64
  	CriticalSpeedup float64
  }

  // derivedSolutionDials is The Method's four options as doctrine, not as drafts.
  //
  //	normal        — minimum staffing for unimpeded critical-path progress (ch. 12).
  //	subcritical   — deliberately understaffed: LONGER, COSTLIER and RISKIER than
  //	                normal, whose whole purpose is disproving "fewer people = cheaper".
  //	compressed    — same staffing, critical path sped up; the >30% exclusion guard in
  //	                recommendOption is what keeps it out of the death zone.
  //	decompressed  — normal, deliberately extended, to drop criticality risk toward the
  //	                tipping point without consuming the float by cutting staff.
  //
  // The values reproduce this repo's committed slots 11-14 EXACTLY (cap 6/4/6/6, buffer
  // 0/0/0/20, speedup 1/1/1.8/1), which is the acceptance: the compute must not change
  // the plan on the state it is first run against, or its first output would be
  // indistinguishable from a regression.
  //
  // subcriticalStaffingCut is the one number with a judgement in it: two fewer agents
  // than normal, matching the committed 6 -> 4. A cap below 1 is clamped, because a
  // subcritical option with no staff has no duration to be costlier than.
  const (
  	subcriticalStaffingCut    = 2
  	compressedCriticalSpeedup = 1.8
  	decompressedBufferDays    = 20.0
  )

  func derivedSolutionDials(kind projectstate.ArtifactKind, staffingCap int) (solutionDials, bool) {
  	switch kind {
  	case projectstate.KindNormalSolution:
  		return solutionDials{StaffingCap: staffingCap, BufferDays: 0, CriticalSpeedup: 1}, true
  	case projectstate.KindSubcriticalSolution:
  		cut := staffingCap - subcriticalStaffingCut
  		if cut < 1 {
  			cut = 1
  		}
  		return solutionDials{StaffingCap: cut, BufferDays: 0, CriticalSpeedup: 1}, true
  	case projectstate.KindCompressedSolution:
  		return solutionDials{StaffingCap: staffingCap, BufferDays: 0, CriticalSpeedup: compressedCriticalSpeedup}, true
  	case projectstate.KindDecompressedSolution:
  		return solutionDials{StaffingCap: staffingCap, BufferDays: decompressedBufferDays, CriticalSpeedup: 1}, true
  	}
  	return solutionDials{}, false
  }

  // riskModelFrom joins the per-option risk the estimation Engine already computed into
  // the RiskModel slot, with the App-C exclusion zones the SDP review already applies.
  //
  // Slot 15 was agent-drafted and has carried revisions:1 + staleBasis:true since it was
  // written. There was never anything for an agent to judge here: EstimateForOption
  // returns criticality risk, activity risk and their composite per option, and
  // sdpOptionInBand already decides inclusion from the same numbers. This is the join,
  // and it uses the SAME thresholds recommendOption uses, so the committed risk model and
  // the committed recommendation can no longer disagree.
  func riskModelFrom(rows []projectstate.SdpOptionRow, risks map[projectstate.ArtifactKind]estimation.RiskScore, recommendation projectstate.ArtifactKind) projectstate.RiskModel { … }
  ```
  - [ ] **Verify first:** `RiskModel`/`RiskRow`'s exact fields (`contract.gen.go`: `Rows, TooRiskyThreshold, OverSafeThreshold, MaxCompressionPct, Recommendation` and `SolutionKind, CriticalityRisk, ActivityRisk, Composite, DurationDays, TotalCost, Included, ExclusionReason`) — the `Recommendation` member is why the join takes three parameters and not two, and it is an **`ArtifactKind`** (`:819`), NOT the `OptionID` that `SdpReview.Recommendation` carries. `recommendOption` returns an `OptionID`, so the caller maps it to the winning row's `SolutionKind` before passing it here; do not widen either field to make one call site simpler. `ce.Risk` is an `estimation.RiskScore` and it **does publish `CriticalityRisk` and `ActivityRisk` separately** (verified), so both `RiskRow` fields get their own number and nothing is carried twice; `assembleSdpReview` today reads only `ce.Risk.Composite` (`assemblesdpreview.go:288`), so the change is to keep the whole score per option rather than just the composite. There is no STOP here.

- [ ] **Step 3: Write the compute strategy.** In `deliveryactivity.go`:
  ```go
  // sdpComputeStrategy is the FIRST computation registered against the strategy table's
  // third row: a review task that names no `reviews` target, whose subject the platform
  // derives rather than dispatches. Spec §6/R7 — Project Design is deterministic, there
  // are no agent-drafted steps, and the reviewer's job is to approve a plan and its cost.
  //
  // WHAT IT WRITES, and what it deliberately does not:
  //	slot 8  planningAssumptions — READ, never written. It carries the founder's
  //	        resources, calendar, infrastructure kind, declared usage, settlement terms
  //	        and rate card: business input no engine can derive. **Absent ⇒ the compute
  //	        fills The Method's DEFAULT planning assumptions (resources, calendar, rate
  //	        card, indirect rate, terms) and PROCEEDS, recording the defaulting in the
  //	        attempt's Provenance/Detail so the M0 reviewer sees which numbers were
  //	        assumed; it must NOT raise SDPInputsIncomplete.** A project that cannot
  //	        reach its own cost-approval gate cannot be told what it would cost, and
  //	        refusing at M0 refuses the one screen that exists to ask the question.
  //	        (Controller ruling, 2026-09-26. The SDP assembly's historical refusal was
  //	        correct when slot 8 had an agent to draft it; stage 4b1 deletes that rail.)
  //	slots 9,10   activityList + network — MaterializeActivityPlan, the SAME derivation
  //	        `make derived-plan-write` drives and `derived-plan-check` gates, so the
  //	        committed plan and the child's plan cannot diverge.
  //	slots 11-14  the four Solution dial-sets — derivedSolutionDials.
  //	slot 15 riskModel — riskModelFrom, over the risk EstimateForOption already returns.
  //	slot 16 sdpReview — assembleSdpReview, unchanged, kept whole.
  //
  // Recorded as a TaskAttempt with Actor = the engine, so a failed computation is
  // visible and retryable exactly like a failed draft (architect Ruling 3(c)). A silent
  // compute failure at M0 would be a project that never starts with nothing saying why.
  type sdpComputeStrategy struct{ wf *csWorkflows }

  func (s sdpComputeStrategy) Produce(ctx workflow.Context, tc taskContext) (producedSubject, error) {
  	attemptID := projectstate.AttemptID(string(tc.In.ActivityID), tc.Task.ID, int(tc.Revision)+1)
  	ref, defaulted, err := s.wf.computeProjectPlan(ctx, tc.In, tc.State)
  	if err != nil {
  		return producedSubject{AttemptID: attemptID, Outcome: projectstate.OutcomeFailed, Detail: err.Error()}, err
  	}
  	return producedSubject{
  		StagedRef: ref, AttemptID: attemptID, Outcome: projectstate.OutcomePassed,
  		// The defaulting rides the attempt's Detail because that is what the M0 screen
  		// reads back: an assumed number the founder never saw is the one way a computed
  		// cost can mislead.
  		Detail: defaultedDetail(defaulted),
  	}, nil
  }
  ```
  `computeProjectPlan` reads the project, resolves slot 8 (committed, or `defaultPlanningAssumptions` with the family names collected into `defaulted`), calls `MaterializeActivityPlan` then writes slots 9/10, then 11–14, then runs `assembleSdpReview` (which is what calls the three Engines per option), then writes 15 and 16 — **each write through `activityExecutionAccess.StageTaskOutput`**, not through the three `designSessionAccess` verbs the SDP assembly used. That re-homing is execution risk 6's requirement and is what leaves 4b2 a facet it can delete. `sdpIncomplete` survives for the slots that genuinely cannot be defaulted — an uncommitted slot 5 (`materializePhase2Draft` already raises `FailedPrecondition` naming it, `deliverymanager.go:7944-7946`), because a plan cannot be derived from an architecture that does not exist.

- [ ] **Step 3a: The Method's default planning assumptions.** Beside the dial table in `deliverymanager.go`:
  ```go
  // defaultPlanningAssumptions is what the compute assumes when slot 8 is uncommitted.
  // Every number has a source, and none of them is invented here:
  //
  //	Resources            the WORKER CLASSES the derived plan actually uses, read off
  //	                     the activity list — Löwy ch. 7's staffing question answered by
  //	                     the plan rather than guessed ahead of it. Never a head count:
  //	                     the option's StaffingCap is the cap, and the resources list is
  //	                     the roster of ROLES the network needs.
  //	CalendarDaysPerWeek  5 — the book's nominal working week (App. A's day is a working
  //	                     day). This repo's own committed value is 2, which is a FOUNDER
  //	                     fact about a solo founder's availability and precisely the kind
  //	                     of thing a default must not pretend to know.
  //	IndirectDailyRate    defaultIndirectDailyRate ($50/day, assemblesdpreview.go's F6
  //	                     constant) — the overhead that accrues per calendar day
  //	                     regardless of which agents are active, and what makes a
  //	                     subcritical option demonstrably costlier.
  //	RateCard             defaultRateSpec per class (the same helper deriveClassRates
  //	                     already falls back to: defaultModelForClass +
  //	                     defaultMTokInPerDay/OutPerDay). So the default rate card is the
  //	                     rate card deriveClassRates has ALWAYS synthesised for a class
  //	                     the authored card omitted — this makes the whole-slot default
  //	                     the same rule as the per-class one, not a second one.
  //	InfrastructureKind   the committed operationalConcepts' deployment scenario when it
  //	                     is there, else the zero value, which is the platform's own
  //	                     "unchosen" and is what the operating-model screen fills.
  //	DeclaredUsage        UsageAssumption{1, 1, 4096} — one user, one request a minute,
  //	                     a 4 KiB payload: the smallest load that is not zero, because a
  //	                     zero-load option has no operating cost to compare and the
  //	                     operating half of the M0 headline would read $0.
  //	Terms                RevenueShare 0 / ComputeCost tieredFloors / Schedule monthly —
  //	                     the platform's shipped billing posture (docs/billing-setup.md;
  //	                     the 2026-06-09 MoR reversal), not a per-project choice.
  //
  // It returns the ASSUMPTIONS and the caller collects the family names it defaulted, so
  // the M0 screen can say "cost computed on assumed calendar and rate card" rather than
  // presenting an assumption as a decision.
  func defaultPlanningAssumptions(proj projectstate.Project, al projectstate.ActivityList) (projectstate.PlanningAssumptions, []string)
  ```
  - [ ] **Verify first:** `defaultRateSpec`, `defaultModelForClass`, `defaultMTokInPerDay`, `defaultMTokOutPerDay` and `defaultIndirectDailyRate` all exist in `assemblesdpreview.go` (`:723-724`, `:740-744`, `:777-781`) — reuse them rather than restating their numbers, so the whole-slot default and the per-class fallback cannot drift. Read `projectstate.SettlementTerms`' and `UsageAssumption`'s field names and the `ComputeCostKind`/`ScheduleKind` enum spellings before writing the terms, and confirm `InfrastructureKind`'s zero value is the "unchosen" member and not a real infrastructure.

- [ ] **Step 4: The M0 gate-passed handler advances the root `phase`** (R4). In `deliveryactivity.go`, inside `passRound`'s terminal for a gate task, after `DecideReviewRound` and `CommitActivityArtifacts`:
  ```go
  	// M0 MOVES THE ROOT PHASE, and the spec's "derived from milestone position" is
  	// amended to say so (stage 4b1, R4; derivation is stage 6).
  	//
  	// MEASURED, and this is the whole reason the write is here: nextEligibleActivity
  	// returns verdictQuiescent unless proj.Phase == PhaseConstruction, and
  	// PumpSweepWorkflow filters on the same. AdvancePhase is literally p.Phase++. So a
  	// gate-passed handler that did not advance would approve the plan and then leave
  	// the pump permanently quiet — construction would never start, and nothing would
  	// say why. It is the ONE write that materialises the plan.
  	//
  	// It is idempotent by the phase's own ordering: advance only from the phase BELOW
  	// construction, so a replay or a re-decided round cannot push the project past it.
  	if isM0Gate(lc, t) {
  		if err := wf.advanceToConstruction(ctx, in, state); err != nil {
  			return taskFailed, err
  		}
  	}
  ```
  with `isM0Gate` reading the DATA — `t.Kind == review && t.Reviews == "" && t.ArtifactKind == "SdpReview"` — so the walker still names no activity type, and `advanceToConstruction` calling `wf.Acts.ProjectStateAdvancePhase` through `applyRecovering`, guarded on the read-back phase being below `PhaseConstruction`.

- [ ] **Step 5: Delete the SDP assembly's session half.** In `assemblesdpreview.go`: delete `AssembleSDPReviewWorkflow`, `sdpCommit`, `stageReview`, `commitReview`, `rejectReview`, `maxSDPReassembleAttempts`, `sdpReviewInput`, `sdpDecisionSignal`, `optionInReview`; KEEP `assembleSdpReview`, `assembleOption`, the three `to*Option` converters, `monthlyCostAtDeclaredLoad`, `recommendOption`, `sdpOptionInBand`, `pickBestSdpOption`, the whole airates block and the `committed*` readers, re-receivered from `*pdWorkflows` to `*csWorkflows`. **The file now has no workflow entry func**, which `TestFileLayout` reports as `file-not-allowed` — so its surviving contents move: the pure helpers to `deliverymanager.go`, and anything taking a `workflow.Context` to `deliveryactivity.go`. Delete `phase2advance.go` and `Phase2AdvanceWorkflow` with it (the child's handler is what it did). In `deliverymanager.go` delete `RequestSDPCommit` and `AdvanceToConstruction`, and re-route `submitProjectDesignDecision`'s `sdpReviewTaskID` arm to signal the child:
  ```go
  	// M0's approve is an ORDINARY gate decision from stage 4b1 — the same signal every
  	// other gate takes, carrying the chosen option. Its RejectAll arm is gone: spec §6
  	// gives M0 no send-back, because the plan is derived and changing it means amending
  	// the Architecture, and the SPA has refused it since stage 5 (NO_SDP_SEND_BACK).
  	// AdvanceToConstruction is gone too: the child's gate-passed handler advances.
  ```
  A `ReviewReject` at M0 becomes `fwmanager.FailedPrecondition` naming the amendment path, matching the SPA's own copy so the server and the screen give one reason.

- [ ] **Step 6: The pump starts the child for `projectDesign`, and the stale basis recomputes.** In `pumpnextactivity.go`, narrow the design skip and start the new type; in `nextEligibleActivity`/`dispatchSelectionFor`, let a `projectDesign` activity through. The recompute is the pump's, per architect Ruling 3(c) — *invalidation is a network fact, the pump is the only thing holding the network, and a child invalidating its dependents is an up-call needing a bespoke edge per pair* — so the pump treats a `projectDesign` activity whose `sdpReview` slot is `staleBasis: true` as ELIGIBLE again, which restarts the child, which recomputes from the current architecture and opens revision *n+1* through the existing stale-basis machinery. Write that sentence into the eligibility rule, and record in the doc comment that the general amendment case is spec §10's follow-up.

- [ ] **Step 7: Tests.** Re-point `m0-no-sendback` to the child and flip it green: approve advances the root phase; a reject is refused with the amendment reason; the compute's outputs on THIS repo's committed state are **byte-identical to slots 11–14's committed dials** and produce the same `recommendation` slot 16 holds — that equality is the acceptance, and a difference means the doctrine table is wrong, not that the state is stale. Plus: **slot 8 uncommitted ⇒ the compute PROCEEDS on The Method's defaults, the attempt records which families were defaulted, and M0 still opens** (R-E's controller override — assert the round opens and the `Detail` names the defaulted families; a case asserting a refusal here would pin the behaviour the override removed); slot 5 uncommitted ⇒ `FailedPrecondition` naming the architecture, which is the one precondition that cannot be defaulted; a failed compute is recorded as a failed `TaskAttempt` with the engine as actor and is retryable; the human floor holds under `vibes` (the `human-floor-under-vibes` case's M0 half); Architecture re-commit ⇒ the pump re-selects projectDesign and the child recomputes.
  **The M0 copy line is Task 14's, not this task's.** This task's job is that the SERVER records which families were defaulted, in the attempt's `Detail`, and a test asserts the string names them. The SPA sentence that renders it belongs where the webApp gates already run (`npm run check`, the preview suite) — Task 9 has no `webApp` entry in its Files list and adding one would put an ungated SPA edit inside a server commit. Task 14 Step 3a renders it, and until then the defaulting is recorded and unrendered, which is a one-task gap this plan states rather than an invisible one.

- [ ] **Step 8: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_LifecycleShapes|SDP|ProjectPlan|SolutionDials|RiskModel|AdvanceToConstruction' -count=1 -v 2>&1 | tail -40
  GOWORK=off make derived-plan-check
  GOWORK=off go test ./internal/ -run 'TestFileLayout|TestRegisteredTemporalNamesGolden' -count=1
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off go test ./internal/manager/delivery/ -run Test_Replay -count=1 -v 2>&1 | tail -20
  golangci-lint cache clean && GOWORK=off make lint fix-check
  ```
  Expected: `m0-no-sendback` green against the child; the dial equality green; `derived-plan-check` green — the compute uses `MaterializeActivityPlan`, so it must agree with the gate that checks the committed slots, and a red `derived-plan-check` here means the child derives a plan the repo does not have; `TestFileLayout` green after `assemblesdpreview.go` is emptied and removed; the golden loses `projectDesignSDPReview` and `projectDesignPhaseAdvance`; **the `phase2-pre-stage4` replay fails**, because `projectDesignSDPReview` is no longer registered — that is expected and its case list and directory move in Task 13 Step 4, so this task's replay run is scoped: `-run 'Test_Replay_DesignHistories|Test_Replay_PreB1Histories'`. Record the scoping in the commit message, because a narrowed gate that is not explained reads as a skipped one.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1
  git add server
  git commit -F - <<'MSG'
  feat(delivery): Project Design is computed, and M0 is an ordinary gate

  The projectDesign lifecycle is one review task with dependsOn: [], no command and
  artifactKind SdpReview — the simplest instance of the generic child, and the one
  that proves the gate, the round, the commit and the phase advance before any fork
  exists. Its subject is COMPUTED, which is what the strategy table's third row is
  for: ValidateLifecycle permits a review that names no dispatch, so modelling the
  computation as a task would have been a method-assets release inside this wave.

  The compute writes slots 9 and 10 through MaterializeActivityPlan — the same
  derivation derived-plan-check gates, so the child's plan and the committed plan
  cannot diverge — then the four Solution dial-sets, then the risk model, then the
  SDP review. assembleSdpReview is kept whole; only its session loop dies.

  The four solution slots turned out to be four sets of THREE numbers: assembleOption
  reads StaffingCap, BufferDays and CriticalSpeedup and nothing else, because rates
  come from the AI rate card and the per-option calendar cheat was retired by F5. So
  they are doctrine now, and the acceptance is that the doctrine reproduces this
  repo's committed dials exactly. Slot 15 was never something to draft either:
  EstimateForOption already returns criticality and activity risk per option and
  sdpOptionInBand already decides inclusion from them — it has carried revisions:1
  and staleBasis:true since the day it was written.

  Slot 8 is READ and never written: planningAssumptions carries the founder's
  resources, calendar, infrastructure, usage, terms and rate card, which no engine can
  derive. When it is absent the compute fills The Method's DEFAULTS and PROCEEDS,
  recording which families it assumed on the attempt and saying so on the M0 screen.
  It does not refuse: a project that cannot reach its cost-approval gate cannot be
  told what it would cost, and refusing there refuses the one screen that exists to
  ask. Every default has a source — the nominal 5-day week from App. A, the $50/day
  indirect rate and the per-class rate specs from the constants deriveClassRates has
  always fallen back to, the billing posture from the shipped one — and the resources
  list is the roles the derived plan actually uses rather than a guessed head count.
  When the founder edits those assumptions is a UX question, earmarked.

  The one thing that still cannot be defaulted is the architecture: a plan derived
  from an uncommitted slot 5 would be a plan for nothing, so that stays a
  FailedPrecondition naming it.

  M0's approve is now the same signal every other gate takes. SDPRejectAll is gone
  (§6 gives M0 no send-back; the SPA has refused it since stage 5) and so is
  AdvanceToConstruction: the gate-passed handler advances the root phase, which is
  the one write that materialises the plan — measured, because nextEligibleActivity
  and the sweep both gate on PhaseConstruction and AdvancePhase is p.Phase++.

  Invalidation lives in the PUMP, per the architect: it is a network fact, the pump
  is the only thing holding the network, and a child invalidating its dependents
  would be an up-call needing a bespoke edge per pair. A stale sdpReview slot makes
  projectDesign eligible again, and the child recomputes.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 10: Requirements and Architecture through the child

The design dispatch strategy: the co-author session's command sequence, reproduced inside the generic child, against the SAME `agenticJobAccess` the construction rail uses. Recon §3.3 is the load-bearing measurement — *"the design rails dispatch through the SAME `agenticJobAccess`… There is no `aiarch-*.yml` name in the design path… so the design rails are already venue-blind too."* What the strategy adds over construction's is the artifact STAGING: a design task's output is a slot MODEL read back off the session branch, not a commit the agent pushed.

`SystemDesignPhaseWorkflow`'s orchestration goes away entirely: it sequenced `phase1KindsForRun` per kind with a skip-if-committed guard, which is the pump's eligibility over slot-10 dependencies plus the child's own `dependsOn` walk. The mapping is exact and worth stating: `projectstate.Phase1RequiredKinds()` (`projectstateaccess.go:2984-2992`) is **five** kinds — mission, glossary, volatilities, coreUseCases, system — and `requirements`' four phases are the first four while `architecture`'s single phase is the fifth. Activity 1 = 4 phases / 8 tasks, activity 2 = 1 phase / 2 tasks, **no residue**.

**Files:**
- Modify: `server/internal/manager/delivery/deliveryactivity.go` — `agenticDispatchStrategy` (the design arm), `runAgentReviewers`, the staging.
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — `ClassifyActivity` rule 0.
- Modify: `server/internal/manager/delivery/deliverymanager.go`, `pumpnextactivity.go` — `isDesignActivity` and the quiescent guard deleted.
- Modify: `server/internal/arch_test.go` — the `ErrDesignActivityNotDispatchable` allowlist entry REMOVED.
- Modify: `cmd/backfill-attempts/main.go`, `projectstate/access_test.go`, `server/internal/manager/delivery/manager_test.go`.

**Interfaces produced (Task 11 shares the strategy; Task 13 deletes what this orphans):**
- `agenticDispatchStrategy{wf *csWorkflows}` with `Produce` complete for a task whose `artifactKind` names a design slot.
- `func (wf *csWorkflows) stageDesignOutput(ctx workflow.Context, tc taskContext, model projectstate.ArtifactModel) (string, error)` — `StageTaskOutput` on `activityBranchName(activityID)`, returning the staged ref.
- The design half of `agenticDispatchStrategy` fills `strategySlotDispatch` for a task whose `artifactKind` names a design slot; the construction half (Task 11) fills the same slot for one that does not. ONE strategy, two staging branches, chosen on `artifactKind` — which is data.
- `ClassifyActivity(id, workerClass, coding) (ActivityType, TestingVariant, error)` — rule 0 now returns `nil`.

- [ ] **Step 1: Walk the co-author command sequence and every `ErrDesignActivityNotDispatchable` site before changing either.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  sed -n '659,830p'   internal/manager/delivery/coauthorartifact.go   # produceReviewableDraft -> runDraftRoundTrip -> dispatchDraftAndReadBack
  sed -n '2630,2700p' internal/manager/delivery/coauthorartifact.go   # dispatchDesignJob + observeDesignJob
  sed -n '2960,3000p' internal/manager/delivery/coauthorartifact.go   # readBackCommittedModelOn
  sed -n '1119,1140p' internal/manager/delivery/coauthorartifact.go   # stageDraftForReview
  sed -n '919,1035p'  internal/manager/delivery/coauthorartifact.go   # runCritiqueRound — what becomes an ordinary reviewer
  sed -n '28,60p;120,150p' internal/manager/delivery/systemdesignphase.go
  grep -rn 'ErrDesignActivityNotDispatchable' internal/ cmd/ | grep -v '_test.go:.*//'
  grep -n 'func Phase1RequiredKinds' -A 12 internal/resourceaccess/projectstate/projectstateaccess.go
  ```
  Expected: the draft round trip as recon §3.1 draws it — session branch (`DesignBranch`), `mintCred` → `RailOpenBranch`, a resume probe (`readBackCommittedModelOn`), `dispatchDesignJob` with `command = DesignCommandFor(kind, draft)`, `observeDesignJob` (15 s × 240), the read-back, the amendment no-change guard, `openPR` **only after** the read-back; `stageDraftForReview` → `DesignSessionStageArtifactForReviewOnBranch`; and **eleven** sites of `ErrDesignActivityNotDispatchable` exactly as execution risk 8 lists them. Confirm `Phase1RequiredKinds()` is the five and that the lifecycle phases line up four-then-one with no sixth kind — if a sixth appears, the mapping is not exact and that is a STOP, not a rounding.

- [ ] **Step 2: Delete rule 0's ERROR, keep its table.** In `projectstateaccess.go:8510`, return `(typ, TestVariantPlan, nil)`; keep `designActivityTypes` (`:8452-8462`, the exact-id map requirements/architecture/projectDesign → the three `ActivityType`s) — `QueryActivityView`, `ClassifyType`, `railFor` and the backfill all need the mapping. Then walk all eleven sites: delete `isDesignActivity` (`deliverymanager.go:9486-9490`) and its scan skip (`:9464-9467`), delete the chosen-activity quiescent guard (`:9530-9532`), delete the pump's skip log (`pumpnextactivity.go:132-135`) and `pumpSelection.SkippedDesign` with every producer and reader, simplify `railFor`'s tolerance (`:11682`) and `ClassifyType`'s (`:8565`) and the backfill's (`cmd/backfill-attempts/main.go:322`), update both docs (`:8443`, `deliverymanager.go:8782`), fix `projectstate/access_test.go:8984-8985`, and **REMOVE the encapsulation allowlist entry** at `internal/arch_test.go:673-680` — an allowlist naming a deleted sentinel is a waiver nobody can retire.
  - [ ] **Verify:** `GOWORK=off grep -rn 'ErrDesignActivityNotDispatchable\|SkippedDesign\|isDesignActivity' internal/ cmd/` returns **nothing**, and `GOWORK=off make encapsulation-check` is green — if it fails naming the removed entry, something still exports the sentinel and the deletion is incomplete.

- [ ] **Step 3: Complete the design arm of the dispatch strategy.** Its `Produce`, in the order the session ran the commands, reusing the existing helpers rather than re-writing them:
  ```go
  // agenticDispatchStrategy runs ONE dispatch task: submit the agentic job the task's
  // `command` names, observe it, read back what it produced, and stage that as the
  // task's output on the activity branch.
  //
  // It is ONE implementation for both rails, because the rails were already venue-blind:
  // the design session dispatched through the same agenticJobAccess the construction rail
  // does (there is no aiarch-*.yml name anywhere in the design path — the workflow file is
  // the RA variant's concern), and the composition root is what chooses GH Actions or
  // local. The command is DATA (task.Command), the worker class is DATA
  // (task.WorkerClass), and the artifact kind is DATA (task.ArtifactKind).
  //
  // The ONE real difference, and it is the staging codec rather than the venue: a design
  // task's output is a slot MODEL that must be read back off the branch and decoded
  // before it can be staged, while a construction task's output is a commit the agent
  // already pushed. That is the strategy's whole per-rail surface, and Task 11 adds the
  // other half of it.
  func (s agenticDispatchStrategy) Produce(ctx workflow.Context, tc taskContext) (producedSubject, error)
  ```
  The body: `branch := activityBranchName(tc.In.ActivityID)` — **the activity branch, not `DesignBranch`** (spec §5.3 unifies on `activity/{activityId}`, which construction already uses: `activityBranchName` is an existing named helper at `constructactivity.go:684` with five production callers, so this is a REUSE and not a new scheme; R12 lists `amend-N` retirement as 4b2's, so `DesignBranch` survives unused for one release) → `beginSession`/`mintCred`/`RailOpenBranch` when `gitEnabled` → the resume probe → `submitPipeline` with `dispatchInputsFor` carrying `command = tc.Task.Command` → `observePipeline` → `readBackCommittedModelOn` for a task whose `artifactKind` names a slot, skipped for one that does not → `stageDesignOutput` → `openPR` **after** the read-back. Every one of those is an existing helper in `constructactivity.go` or `coauthorartifact.go`; the strategy's body is the sequence, not the implementations.
  - [ ] **Verify first:** `activityBranchName` (`constructactivity.go:684`) is already a named helper — `grep -n 'activityBranchName' internal/manager/delivery/*.go` shows its five production callers — so **do not promote it to `projectstate`**: it stays in the delivery package and MOVES with the rest of the keep-list in Task 13 Step 3. A new exported `projectstate.ActivityBranch` would be an export with no caller outside its package, which the house doctrine and `TestGeneratedOnlyPublic` both push back on. Widen its doc instead to say it is now BOTH rails' branch scheme.

- [ ] **Step 4: Fold the critique round into the reviewer set.** `runAgentReviewers` (called from `runGate` when `t.Command != ""`) dispatches the review task's own command through the SAME `agenticDispatchStrategy`, then appends the result as an ordinary `ReviewVerdict` with `reviewerRole` from `t.WorkerClass` — which is what `runCritiqueRound` did with three hundred lines of bespoke machinery around it. Keep `critiqueReadBackEmptyType`'s safe default (a job that succeeded and committed no verdict routes to a failed gate, `coauthorartifact.go:1035`) and `armCritiqueRetry`'s budget; delete `critiqueCriticFor`, `criticVerdictKind`, `appendCriticVerdict` and the `critique` type with the twin in Task 13.

- [ ] **Step 5: The pump starts the child for requirements and architecture**, and `SystemDesignPhaseWorkflow`'s sequencing is gone: eligibility is slot 10's dependencies (`requirements → architecture → projectDesign` is a chain in the committed network) and the skip-if-committed guard is the child's own `seedWalkFromLedger`, which marks a task passed when its round is passed. Record the exact mapping (five required kinds = four `requirements` phases + one `architecture` phase, no residue) in the deletion's doc comment, and record **GAP-4B-4** — `operationalConcepts` (slot 6, status 2, three revisions) and `standardCheck` (slot 7, status 4, the only non-committed slot) appear in **no lifecycle and no required list**, yet both still have live `DesignCommandFor` slugs — as a founder question in Task 15, not as a guess here. Nothing in this task drafts them and nothing in this task deletes them.

- [ ] **Step 6: Tests.** Flip `human-floor-under-vibes` fully green against the child (a design review auto-approves under `vibes`; M0 still holds). Add: a `requirements` activity walks mission → glossary → volatilities → coreUseCases in order, four gates, four rounds, one per phase, with `artifactKind` set on each (Task 3's field carrying its first real load); an `architecture` send-back re-opens `architectureDraft` + `architectureReview` and NOTHING else; `volatilitiesReview` dispatches **no** agent reviewer while its three siblings do; a design task's staged ref is a `SubjectCommit` that CHANGES between rounds 1 and 2 (Task 5's fix, now live on the generic writer); the local profile's design rail stays ACTIVE (`Test_DeliveryManager_LocalProfile_*` still green).

- [ ] **Step 7: Gates and commit.** Run the full drift block plus `GOWORK=off go test ./internal/manager/delivery/ -run 'Test_LifecycleShapes|Design|Requirements|Architecture' -count=1 -v` and the scoped replay (`-run 'Test_Replay_PreB1Histories'` — the design fixtures' registrations die in Task 13). Expected: the shape cases green except the three fork/join ones still on the stub; `encapsulation-check` green with one FEWER allowlist entry; the local-profile tests green. Commit message: the eleven sites of the sentinel and why the table survives; that `SystemDesignPhaseWorkflow` was the pump for Phase 1 in miniature and the mapping is exact with no residue; that the critique round is now a review task with a command; that `operationalConcepts` and `standardCheck` are earmarked, not silently retired.

---

### Task 11: Construction through the child; `walkPhases` deleted

Parity, and the fork that has never run. R6 is the standard: every gate decision, roster, venue and artifact write reachable today must be reachable through the child, through the same RA verbs, with `isGitLocalVenue` recognition preserved at every reader (R-D: **nine** readers — three rail `gitEnabled`s replaced by the child's one, three Manager-side ones re-homed or deleted in Task 13, three kept verbatim).

Measured, and this is what makes the task smaller than it looks: `constructactivity.go` contains **eleven** lines mentioning the activity type or variant and **not one is a `switch`** — `:599` and `:1854` are passthroughs, `:695`/`:698-722` are a display Stringer over the Manager's own `activityKind` enum (not the activity TYPE), `:980`/`:2287` are `ProfileFor` re-seeds the child stops making, and `:2523-2524` is a `pipelineSpec` passthrough into `CommandFor`. **The child is already generic on TYPE; it is not generic on SHAPE, and that is all this task changes.**

**Files:**
- Modify: `server/internal/manager/delivery/deliveryactivity.go` — the construction arm of `agenticDispatchStrategy`, the merge step, the variance loop.
- Modify: `server/internal/manager/delivery/constructactivity.go` — the flat walk taken OFF the child's path; the helpers the child calls kept; **`ConstructActivityWorkflow` keeps its working body until Task 13** (Step 3 says why).
- Modify: `server/internal/manager/delivery/deliverymanager.go` — `hydrateConstructionActivity` stops stamping `Phases`; `pumpnextactivity.go` starts `deliveryActivity`.
- Modify: `server/internal/manager/delivery/manager_test.go`

- [ ] **Step 1: Re-read the eleven type mentions and the three `Phases` producers, and confirm the child needs none of them.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  grep -nE 'Activity\.Type|Activity\.Variant|Activity\.Kind|activityKind|ProfileFor' internal/manager/delivery/constructactivity.go
  grep -n 'Phases:' internal/manager/delivery/deliverymanager.go
  sed -n '2744,2830p' internal/manager/delivery/constructactivity.go   # runLocalMergeStep + runMergePipeline
  sed -n '2945,3100p' internal/manager/delivery/constructactivity.go   # handleVariance + the intervention Engine
  sed -n '1119,1140p' internal/manager/delivery/constructactivity.go   # seedResumeFromLedger
  ```
  Expected: the eleven lines as listed above; `Phases:` set in **four** places, not three — `deliverymanager.go:3745` and `:9652` (the hydration), `constructactivity.go:980` (the re-seed) and `:2287` (the send-back re-walk). `:3745` is the one the first draft of this plan missed; determine from its context whether it is a VIEW struct (in which case it is a read-path derivation and 4b2's, not this task's) or a second hydration (in which case it stops stamping with `:9652`), and write the answer into the commit rather than leaving four sites and three instructions. Also expected: `runLocalMergeStep`'s guard at `:2765` (`if !gitOn || wf.RailEnabled(in.ProjectID) || state.mergeCompleted { return }`); the variance budget `maxVarianceAttempts = 10` separate from the redraft budget. **`Phases` becomes dead on the WRITE path**: the child reads the lifecycle, so the hydration stops stamping it and `constructionActivity.Phases` is deleted with the field's last reader — unless `QueryActivityView` reads it, which the grep must settle; if it does, leave the field, stop STAMPING it, and note the read as a 4b2 item rather than changing a view in this task.

- [ ] **Step 2: Complete the construction arm.** The same `agenticDispatchStrategy`, with the staging branch taken: a task whose `artifactKind` names no slot model stages **nothing** and returns an empty `StagedRef`, so `gateSubjectRef` falls to its second rung (the PR) or its third (the attempt) exactly as it does today. Then `runLocalMergeStep` and `runMergePipeline` move onto the child's gate-passed handler for the LAST gate of the lifecycle — identified from the DATA (the task is the gate of the last phase in `lc.Phases`), never from a type — with the `mergeGateKey = "merge"` gate and its own `proposeReviewSet` call preserved, including the deliberate "log and proceed unheld" on a `ProposeReviews` error (`constructactivity.go:2778-2781`).

- [ ] **Step 3: Delete the flat walk, and leave `ConstructActivityWorkflow` ALONE.** Remove `walkPhases` (`:1179-1225` — the func closes at `:1225`, not `:1231`), `runAttempt`'s phase iteration, `state.completedPhases` and its resume skip, `seedSendBackCarry`'s `ProfileFor` re-walk and `loadReviewSnapshot`'s `Phases` re-seed **from the paths the generic child uses** — but `ConstructActivityWorkflow` keeps its BODY, working, until Task 13 deletes the whole file.
  **Why the plan's first draft was wrong to stub it:** a non-retryable refusal in that body makes all fifteen construction fixtures (`pre-b1` 7, `post-b1` 3, `post-b17` 1, `post-stage3` 2, `pre-d` 2) unreplayable from this commit through Task 12 — inside gate blocks that run `Test_Replay` — and the justification offered ("the fresh fixtures need the type registered for one more commit") was not a reason at all: a fixture captured against `deliveryActivity` needs `deliveryActivity` registered, not the retired type. The old fixtures are the only evidence that Tasks 11 and 12 did not move a durable command on the rail they are rewriting, so they must keep replaying until the commit that archives them.
  Practically: `walkPhases` and the helpers only it used move OUT of the child's path but stay compiled and reachable from `ConstructActivityWorkflow`; the generic child calls the shared helpers directly. If keeping both callers alive forces a helper to carry two shapes, duplicate the helper for one commit with a `// DELETED WITH ConstructActivityWorkflow in Task 13` comment rather than making the old path take a parameter it never had — a shared helper bent to serve a dying caller is how a replay goes red for a reason nobody can attribute.

- [ ] **Step 4: The pump starts the child.** In `pumpnextactivity.go:201-210`, `constructActivityWorkflowID` → `deliveryActivityWorkflowID`, `executionKindConstructActivity` → `executionKindDeliveryActivity`, `constructActivityInput` → `deliveryActivityInput`. **`child.Get` at `:223` STAYS** — the react-by-signal pump is 4b2's (architect Ruling 3(b)), and changing the pump's shape in the same wave as the child's would make a cascade failure unattributable. Say so in a comment at the `child.Get` site naming 4b2, so the next reader does not think it was missed.

- [ ] **Step 5: Tests — the fork, for real.** Flip the three fork/join shape cases off the stub and onto the real strategy, and delete `stubStrategy` and `stubStrategies` (the registry seam STAYS — it is what made this flip a one-line change). Assert: `stp` and `detailedDesign` both appear in `TaskOrder` before either appears in `CompletedOrder`, with the SAME `TaskOrder` (declaration order) and DIFFERENT `CompletedOrder` in the two cases, and the same terminal; `testing` does not open its round while `integration` or `stpReview` is unpassed; a `designReview` send-back re-opens `detailedDesign`+`designReview` and leaves `stp` alone **even when `stp` is mid-flight**; the variance loop, the operator override and the 5-redraft budget behave as the pre-4b1 cases asserted; the local merge runs on the local profile and is skipped on a rail profile; `Test_RailLifecycleEnabled_ReadsWhatTheResolverAnswers` and the two local/catalog profile tests green.

- [ ] **Step 6: Gates and commit.** The full drift block, plus `wc -l internal/manager/delivery/*.go` to record the arithmetic for Task 15. The replay run is **scoped exactly as Task 9 Step 8 scopes its own**, and for the same reason — `projectDesignSDPReview` and `projectDesignPhaseAdvance` stopped being registered in Task 9, so `phase2-pre-stage4` cannot replay until Task 13 archives it:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Replay_DesignHistories|Test_Replay_PreB1Histories' -count=1 -v 2>&1 | tail -25
  ```
  Expected: the three design fixtures and **all fifteen** construction fixtures green — `ConstructActivityWorkflow`'s body is untouched, so they must be, and a red one here means a shared helper was bent to serve the new path; every shape case green with no stub; the profile tests green (R6's parity claim is only true if they are); `TestFileLayout` green. Commit message: that the child was already generic on TYPE and never on SHAPE, with the eleven-line measurement; that `child.Get` is deliberately unchanged and belongs to 4b2; **that the old entry func keeps working on purpose, because the fifteen fixtures are the only evidence this commit moved no durable command, and the replay run is scoped only for `phase2-pre-stage4`, whose registrations Task 9 retired.**

---

### Task 12: The five refused write paths become real (R2/GAP-6, R7)

One child means one write path. The 4a dispatcher refuses five construction paths, each naming 4b (`deliverymanager.go:11911`, `:11995`, `:11998`, `:12018`, `:12047`), and spec §7.2's amendment is explicit that **nothing in 4a may claim GAP-6 closed**. This task closes it.

Per R-G, four of the five are Manager-side LEDGER writes through verbs that already exist and are already registered — no new child signal, because the round ledger has been the source of truth since stage 3.

| Refusal | Site | What it becomes |
|---|---|---|
| comment-status (Resolve / Reopen) | `submitConstructionDecision`, `:11995` | `activityExecutionAccess.SetReviewCommentStatus` on the round the task's latest revision names, then the `setCommentStatus` mirror signal to a live child so the `vibes` autogate re-reads "no open comments" before synthesizing an approve |
| withdraw | same arm, `:11998` | `DecideReviewRound(…, RoundWithdrawn, decidedBy)` — Task 4 gave it its wire member, and the composer now has somewhere honest to put it |
| `AskQuestions` | `:12018` | `AppendReviewVerdict` with the questions as `ReviewComment.type = question` on the round's thread (spec §5.3: Ask is a comment, not a third mechanism), plus the answer job the design rail already dispatches |
| `AcknowledgeStaleBasis` | `:12047` | `activityExecutionAccess.AcknowledgeStaleBasis` against the row's stale pin — the verb exists on the facet (`contract.gen.go:1062`) and has no caller |
| `DispatchActivityTask` | `:11911` | a `redraft` signal to the live child **carrying the `taskID`**, which the router forwards to that task's inbox (Task 8 Step 3a) and `awaitTaskDecision`'s `routedKindRedraft` arm turns into a NEW attempt at the current revision — the same thing a send-back does, asked for directly. This is the one op that makes the `redrafts` channel load-bearing, so its test asserts the signal reached the NAMED task's inbox and not merely that the child accepted it |

- [ ] **Step 1: Prove all five refuse today, then prove each stops.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  grep -n 'stage 4b' internal/manager/delivery/deliverymanager.go
  grep -n 'func (m \*deliveryManager) AskQuestions' -A 30 internal/manager/delivery/deliverymanager.go
  grep -rn 'ActivityExecutionAcknowledgeStaleBasis\|ActivityExecutionSetReviewCommentStatus' internal/manager/delivery/*.go | grep -v manager_test
  ```
  Expected: the six `stage 4b` hits (one doc, five refusals); `AskQuestions` refusing construction before it resolves a kind; and **zero** production callers of `ActivityExecutionAcknowledgeStaleBasis` and `ActivityExecutionSetReviewCommentStatus` — stage 3 wired both verbs and nothing has called either, so this task is their first caller and the fakes' permissiveness (a stage-3 earmark: the design-rail fakes append to any round they hold and answer an unopened activity with a zero row where the store raises `NotFound`) must be tightened in the same commit or the tests prove less than they appear to.

- [ ] **Step 2: Add the one shared resolver** — every one of the five needs the same thing, and writing it five times is how they drift:
  ```go
  // latestRoundFor resolves (activity, task) to the round a write should land on: the
  // HIGHEST round on that task's join key. It is the one place the five formerly-refused
  // construction writes agree about which round they mean, and it refuses rather than
  // guessing when there is none — a comment resolved against a task that has never been
  // reviewed is a caller error, not an empty success.
  //
  // It keys through roundJoinKey, so two artifact kinds sharing a gate task resolve to
  // their OWN latest round (stage 4b1 Task 3) rather than to whichever was written last.
  func latestRoundFor(row projectstate.ActivityExecution, taskID string, kind *projectstate.ArtifactKind) (projectstate.ReviewRound, error)
  ```

- [ ] **Step 3: Replace the five refusals**, each with its own doc sentence saying what it now does and which verb it uses. Delete the `ReviewWithdraw`/`ReviewSetCommentStatus` arm of `submitConstructionDecision`'s `ContractMisuse` and route both; delete the `r == railConstruction` early refusals in `AskQuestions` and `AcknowledgeStaleBasis`; replace `DispatchActivityTask`'s `railConstruction` arm with a `SignalWorkflow` of `signalRedraft` to `deliveryActivityWorkflowID(projectID, activityID)`, **whose payload sets `TaskID: taskID`** — the router drops a signal that names no task, so an unset id would make this op a silent no-op — tolerating a not-found target the way `isSignalTargetNotFound` (`projectsupervision.go:173`) already does and answering `FailedPrecondition` naming the dormant activity rather than a bare error.
  - [ ] **Verify:** `grep -n 'stage 4b' internal/manager/delivery/deliverymanager.go` returns **nothing**. A surviving hit is a path still refusing while the SPA's button is enabled — which is the exact shape of defect the stage-5 notice existed to prevent.

- [ ] **Step 4: Tests, and the fakes.** One case per path, on a CONSTRUCTION activity: resolve then reopen a comment round-trips through the ledger; withdraw stamps `RoundWithdrawn` and renders `withdrawn` (Task 4's member earning its keep); a question lands as `ReviewComment.type == "question"` on the round's thread and dispatches the answer job; a stale acknowledgement clears the row's pin with the note; a re-dispatch opens a new attempt at the SAME revision, not a new revision, **and lands in the named task's inbox** (assert through `shapeRecorder.signalDelivered`, not through the walk's terminal — a redraft that reached nobody looks identical to one that was declined). Plus the spec §9 rail case — **a construction send-back round-trips comments** — and `latestRoundFor`'s refusal when no round exists. Tighten the fakes so `AppendReviewVerdict` refuses an unopened round and `ReadActivityExecution` raises `NotFound` for an unopened activity, matching the store; then re-run the whole delivery suite, because a permissive fake has been holding some tests up.

- [ ] **Step 5: Gates and commit.** Full drift block plus the replay run scoped exactly as Tasks 9 and 11 scope theirs — `-run 'Test_Replay_DesignHistories|Test_Replay_PreB1Histories'`, three design plus fifteen construction fixtures green, `phase2-pre-stage4` excluded because Task 9 retired its registrations. Commit message: that one contract was never one behaviour, and 4a said so; the five paths and their verbs; that four are Manager-side ledger writes rather than new child signals, because the round ledger has been the truth since stage 3 and a new signal per verb would be a second mechanism; that the fakes were tightened in the same commit, with what they used to allow.

---

### Task 13: The twins die — ONE indivisible commit

Everything the child replaced, deleted together, because no two of these are separately green: `TestFileLayout` forbids a handwritten file with no entry func, `TestRegisteredTemporalNamesGolden` fails on a partial deletion, the frozen-names list fails the moment a name it hard-lists goes, and `USECASE-DYNAMIC-MISSING` / `CC-*` fail if a dynamic view's steps name a workflow the code no longer has.

**Deleted:** `coauthorartifact.go` (≈4,395 L after Task 2), `coauthorphase2artifact.go` (2,593 L), `systemdesignphase.go` (189 L), `phaseadvance.go` (22 L, and `phase2advance.go`'s 64 L already went in Task 9), `assemblesdpreview.go`'s remainder, `constructactivity.go` in full — **including `ConstructActivityWorkflow`'s still-working body, which Task 11 deliberately left alone so the fifteen fixtures kept replaying through Task 12** — plus the two dead `GetEpisodeTimeline` leaves (`deliverymanager.go:5234` sd, `:7628` pd — unreachable since `QueryProjectView{timeline}` routes to `cs.GetEpisodeTimeline` at `:12215`) and the `rail` enum with `railFor` (`:11648-11704`), whose one surviving line — `methodassets.LifecycleFor(projectstate.LifecycleKeyFor(typ, variant))` at `:11685` — the child already has.

**Files:** the six deletions above; `deliverymanager.go` (the three rail façades collapse into `deliveryManager` holding one `csWorkflows`); `deliveryactivity.go` (the move-list's destination); `worker.gen.go` via regen; `internal/registered_names_test.go`; `manager_test.go`; `testdata/replay/` → `testdata/replay-archive/`. **NOT `.aiarch/state/project.json`** — measured, no dynamic-view step names any workflow, so there is nothing to re-label and this commit runs no self-amendment loop (R9; Step 7 verifies it by grep).

- [ ] **Step 1: Capture the FRESH fixtures against the new child, BEFORE deleting anything** (R3). The rig needs the `temporal` CLI on PATH — `TestCaptureConstructHistories` `t.Fatalf`s saying so (`manager_test.go:28473`), and nothing outside that test file says it, which is its own earmark. Add `deliveryReplayCases()` with one case per spec §9 shape and `CONSTRUCT_HISTORY_CAPTURE=1` writing into `testdata/replay/post-4b1/`:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  command -v temporal || { echo "STOP: the capture rig needs the temporal CLI on PATH"; exit 1; }
  CONSTRUCT_HISTORY_CAPTURE=1 GOWORK=off go test ./internal/manager/delivery/ -run Test_Capture_DeliveryHistories -count=1 -v 2>&1 | tail -30
  ls testdata/replay/post-4b1/
  GOWORK=off go test ./internal/manager/delivery/ -run Test_Replay_DeliveryHistories -count=1 -v
  ```
  Expected: **eight** fixtures — `linear-deployment`, `fork-join-stp-first`, `fork-join-design-first`, `sendback-judged-pair`, `join-waits-for-all`, `requirements-approve`, `architecture-sendback`, `m0-no-sendback` — each with more than ~20 events (a thinner history means the driver did not reach a terminal; pick a longer driver rather than accepting it), and the new replay test green. **These replay green at every later commit of this plan** — every remaining step's gate block re-runs them.

- [ ] **Step 2: Archive the nineteen.** `git mv` all seven directories under `testdata/replay/` into `testdata/replay-archive/`, keeping their names; finish the README Task 1 skeletoned with the drain that discharged them and each directory's last-green commit; delete `designReplayCases`/`designReplayRegistrations`/`Test_Capture_DesignHistories`/`Test_Replay_DesignHistories_StayDeterministic`, the `phase2*` quartet, and the construction case lists' registrations of `constructionConstructActivity` — **case lists and directories in the same commit**, because `Test_Replay_EveryFixtureDirectoryIsNamed` fails on a directory no case names AND on zero directories, and `replayFixtureFiles` `t.Fatalf`s on an empty one.

- [ ] **Step 3: Move every surviving symbol, THEN delete.** Destination rule (R-C): a `workflow.Context`-taker goes to `deliveryactivity.go`; a pure helper goes to `deliverymanager.go`. `constructactivity.go` goes ENTIRELY — a file with context-taking funcs and no entry func is `file-not-allowed`. **The move-list, written out rather than left to a grep, because "move every surviving symbol" without the list is how a load-bearing helper gets deleted with its file:**

  | From `constructactivity.go` | Why it survives |
  |---|---|
  | `isGitLocalVenue` (`:110`) | **R6's single recognition point**, and `railLifecycleEnabled` (`deliverymanager.go:10240`) calls it from outside this file |
  | `constructRepoTarget` (`:75-100`) | R-D says it survives verbatim — the venue resolver for BOTH rails now |
  | `activityBranchName` (`:684`) | both rails' branch scheme (Task 10 Step 3), five callers |
  | `maxPhaseRedrafts` (`:817`), `maxVarianceAttempts` (`:811`) | the child's two budgets, carried forward verbatim (Task 8 Step 5) |
  | `runLocalMergeStep`, `runMergePipeline` (`:2744-2830`) | Task 11 Step 2 re-homes them onto the last gate's handler |
  | `handleVariance` and the intervention-Engine call (`:2945-3100`) | the child's variance loop |
  | `constructState` itself, with `rowAdvanced` (`deliverymanager.go:10005`) | the child's state type; the CAS seed rides it |
  | `gateSubjectRef` (`:2027`), `engineReviewPolicy` (`coauthorartifact.go:4481`) | Tasks 3/5's writers and the one policy converter |
  | the execution-ledger block (`:1843-2259`), the pipeline submit/observe/episode family, the operator-note family, `attemptOutcomeFor`, `deriveFailureReason` | the child's ledger and dispatch spine |

  | From `coauthorartifact.go` | Why it survives |
  |---|---|
  | `railCredEnvelope` (type `:3381`; its `toProjectState` already lives at `deliverymanager.go:9684`) | every credential-bearing RA call takes it |
  | `mintCred` (`:3324`) | the cloud arm of the child's `gitEnabled` path |
  | `encodeModel` (`:4508`, 5 production hits) | `StageTaskOutput`'s envelope — the design staging codec |
  | `readBackCommittedModelOn`, `observeDesignJob`, `dispatchDesignJob` | the design dispatch strategy's sequence (Task 10 Step 3) |

  Delete `activityKindName` (`:698-722`) and `prBody`'s per-kind copy (`:693`) **only** after `grep -n 'activityKindName\|prBody' internal/manager/delivery/` shows no surviving caller; a display Stringer with one caller in a deleted file is dead, with two it is not.

- [ ] **Step 4: Collapse the three façades, and re-home the three Manager-side `m.repo` readers.** `deliveryManager` keeps ONE workflows struct; `newSystemDesignManager` (`:182`), `newProjectDesignManager` (`:5520`) and the `rail` enum go; `newDeliveryManager`'s 20 parameters are unchanged (R9 — dropping the deprecated facets is post-drain, 4b2), and the comment at `:11606` saying it is the one place that "still knows there were three" is deleted because that stops being true. `designhealth.NewEngine()`'s inline construction (`:183`) moves onto `deliveryManager` unchanged — reclassifying it is 4b2's.
  **The three readers R-D names, each with a decision, because deleting a façade deletes them silently:**
  - `deliverymanager.go:2255/:2259` — the systemDesign **answer-job dispatch**. It MOVES onto `deliveryManager`, because Task 12 Step 3 routes construction's `AskQuestions` through the same answer job; deleting it would take out the path that task just widened.
  - `:6928/:6932` — the projectDesign answer-job twin. DELETED, and the sd copy above is the survivor: the two bodies differ only by receiver, and the M0 ask is now the same op on the same child.
  - `:3599/:3602` — `systemDesignManager.projectRepoBase`, the **PR-url host**. It MOVES, renamed `deliveryProjectRepoBase`. Losing it silently drops the design PR link from every review body, which is a UI regression no server gate would catch — so it gets a test of its own asserting a non-empty host for a catalog-resolved project and an empty one for a GitLocal project.
  Verify with `grep -n 'm\.repo(' internal/manager/delivery/deliverymanager.go` after the collapse: every surviving hit must be one of the two moved readers or a `wfDeps`/`workflows` threading site.

- [ ] **Step 5: ONE `ActivityOptions` hook, with the scaffold-sync preset re-tuned** (execution risk 5). The surviving child hook takes the **5-minute** `scaffoldSyncActivityOptions()` answer for `sourceControlAccess.syncManagedScaffold`, not the 30-second one, because the child now does the design work that needed it. Write the measurement into the comment: the divergence was inert only because `mf.ActivityOptions` had exactly one reader and each rail's workflows consulted their own hook; collapsing without re-tuning would turn 5 min into 30 s on a scaffold sync, which is the difference between a slow refresh and a failed session.

- [ ] **Step 6: The golden and the frozen names.** Regenerate the golden literal **from the test's printed diff**, never by hand. Remove the seven names from `TestRegisteredTemporalNamesGolden_FrozenWorkflowNames`' list (`:271-301`) — `constructionConstructActivity`, `systemDesignPhase`, `systemDesignCoAuthor`, `systemDesignPhaseAdvance`, `projectDesignCoAuthor`, `projectDesignSDPReview`, `projectDesignPhaseAdvance` — and ADD `deliveryActivity` and `deliveryRoundSweep`, with the argument written into the test's own doc comment: *the list exists so an externally-started workflow name cannot vanish silently, and these seven are retired by a DRAIN, which is the one thing that discharges the guarantee; the drain sequence is in `docs/bugs/2026-09-24-stage3-rail-earmarks.md`.* **Also fix the count in that doc comment**: it says "asserts the **20** externally-referenced workflow names" (`:265`); after 20 − 7 + 2 it is **15**, and a doc that still says 20 beside a list of 15 is the next reader's first wrong assumption. `constructionPumpNextActivity`, `constructionReplanSweep` and `constructionProjectSupervision` **stay** — the pump quartet is unchanged in 4b1.

- [ ] **Step 7: VERIFY the model needs no edit — do not edit it.** Measured at `86d3223a`: no dynamic-view step, and nothing else in the committed model, names any of the seven retired workflow types. So this commit touches no slot, runs no self-amendment loop, and regenerates no model surface. Prove it rather than assuming it:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1
  grep -cE 'systemDesignCoAuthor|systemDesignPhase|systemDesignPhaseAdvance|projectDesignCoAuthor|projectDesignSDPReview|projectDesignPhaseAdvance|constructionConstructActivity' .aiarch/state/project.json
  git status --short .aiarch/
  ```
  Expected: **0** and an EMPTY `git status` for `.aiarch/` at the end of this task. A non-zero grep means the model gained a workflow name since this plan was written, and the right response is to re-open R9 and re-key the hits — not to proceed as if the count were still zero. An `.aiarch/` diff means something edited the model that this task did not authorise.

- [ ] **Step 8: Derive the two session views from the ledger** (R-J). `QueryProjectView{session}`'s `session` and `projectSession` members lose their producers here; both are answered from `.activityExecution`'s rounds and attempts plus the slot, which is the derivation `QueryActivityView` already runs. `constructionSession` keeps coming from the child's live `sessionState`, so `liveSessionFor` (`:11378`) needs only its workflow id re-keyed to `deliveryActivityWorkflowID`. No wire change, and `containers/McpSystemDesignContainer.tsx` keeps rendering — verified by `npm run build:mcp`.

- [ ] **Step 9: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  # the full drift block (NO self-amendment loop — this commit edits no model; Step 7)
  GOWORK=off go test ./internal/ -run 'TestRegisteredTemporalNamesGolden|TestRegisteredTemporalNamesGolden_FrozenWorkflowNames|TestFileLayout|TestGeneratedOnlyPublic|TestNoBannedPhaseIdentifier|TestCatalogScopedOpsArePinned|TestCompositionRootGuardsEveryCatalogScopedDeliveryOp' -count=1
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Replay|Test_LifecycleShapes' -count=1 -v 2>&1 | tail -30
  GOWORK=off go test -short -count=1 ./...
  ls internal/manager/delivery/*.go
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check && ASDF_NODEJS_VERSION=lts npm run build:mcp
  cd ../systemtests && GOWORK=off go build ./...
  ```
  Expected: the delivery package down to `deliverymanager.go`, `deliveryactivity.go`, `pumpnextactivity.go`, `pumpsweep.go`, `replansweep.go`, `projectsupervision.go`, `roundsweep.go`, `manager_test.go` and the four `*.gen.go`; **the golden at 134** — derived, and an unpredicted golden is how a dropped registration hides: 139 base → 140 (Task 6's `deliveryRoundSweep`) → 141 (Task 8's `deliveryActivity`) → 139 (Task 9 drops `projectDesignSDPReview`, `projectDesignPhaseAdvance`) → **134** (this task drops `constructionConstructActivity`, `systemDesignCoAuthor`, `systemDesignPhase`, `systemDesignPhaseAdvance`, `projectDesignCoAuthor`). Activity names are generated from contracts this wave does not change, so not one of them moves; if the measured number is not 134, a dep was dropped or double-registered and editing the literal does not fix it. Also expected: the frozen list green with its new 15-name membership; `testdata/replay/` holding ONE directory (`post-4b1`, eight fixtures) and `testdata/replay-archive/` holding seven; every shape case green; `npm run check` and `build:mcp` green. Commit message: what died and what its one surviving line was; the move-list's load-bearing members; the three re-homed `m.repo` readers and what `projectRepoBase` would have silently dropped; that the drain is what discharges the frozen-names guarantee, with the note's path; the `ActivityOptions` re-tune and its measurement; the archive's reason; and that the model needed no edit, verified by grep.

---

### Task 14: The SPA stops refusing what the server now does

Four buttons were deliberately hidden because the server answered a guaranteed 400 (R2/GAP-6). Task 12 removed the 400s. Leaving the notices would be the mirror-image defect: a rail that CAN resolve a comment, with the composer's Question toggle removed so the comment cannot be staged.

**Files:** `webApp/src/containers/activityVerbs.ts`, `activityVerbs.test.ts`; `webApp/src/components/design/submitVerb.ts` + its tests; `webApp/src/containers/ActivityExperienceContainer.tsx`; `webApp/src/hooks/useDeliveryMutations.ts` (four doc comments); `webApp/src/components/activity/activityCopy.ts` + `webApp/src/utilities/constants/UIIdentifiers.ts` (Step 3a's M0 notice); `uitests/preview-fixtures/web-client/activity-experience/*`; `uitests/tests/preview/activity-experience.spec.ts`.

- [ ] **Step 1: Invert the construction arm of `verbsFor`** (`activityVerbs.ts:191-210`) — `ask: { kind: 'ask' }`, `commentStatus: { kind: 'commentStatus' }`, `rerun: { kind: 'dispatch' }` (from `{ kind: 'override' }` — the override verb stays for the variance loop, but a re-run is now a real dispatch), `acknowledgeStale: { kind: 'acknowledgeStale' }`, `reconcileStale: { kind: 'dispatch' }` — and DELETE `NO_CONSTRUCTION_THREAD_OP` (`:108-109`) and `NO_CONSTRUCTION_STALE_OP` (`:111-112`). **`NO_SDP_SEND_BACK` and `NO_SDP_RERUN` STAY** (R7 — M0 has no send-back and the SDP is re-derived, not re-run), as does `ask: { kind: 'ask', foldReplies: true }` at M0, because `pdCheckNoReplyTo` survives (R-K(b)). Update the file's header comment, which lists the five server refusals by name.

- [ ] **Step 2: `allowAsk` / `allowQuestions` stop being needed on construction.** `submitVerb.ts`'s `allowAsk` (`:78`, default `true`) and `CommentMargin`'s `allowQuestions` are driven from `verbs.ask.kind !== 'none'`, so Step 1 flips them for free — **verify that rather than editing them**, and keep both mechanisms: they are the right guard for any future rail that lacks a question op, and deleting them would re-open the C1 defect stage 5 fixed (a staged question replacing Approve and Send back with a button that dispatched nothing).

- [ ] **Step 3: Invert the four node assertions, do not delete them.** `activityVerbs.test.ts:52` and `:58` asserted the refusal reasons; they now assert `commentStatus.kind === 'commentStatus'` and `ask.kind === 'ask'`. `:164`'s table over all seven construction types flips from "must not offer ask before stage 4b" to "offers ask", and the file header at `:7` is re-worded. Add one case pinning that M0 still refuses send-back and re-run, so the two survivors are asserted rather than inherited.

- [ ] **Step 3a: Render the M0 defaulted-assumptions notice** (moved here from Task 9, where the webApp gates do not run). Task 9's compute records which planning-assumption families it had to default in the attempt's `Detail`; this step is the sentence the founder actually reads. `activityCopy.ts` gains one line — *"Cost computed on assumed &lt;families&gt; — no planning assumptions are committed for this project yet"* — with its id in `UI_IDENTIFIERS`, rendered on the M0 review body from the selected revision's attempt `Detail`, beside the existing stale-basis chip and never instead of it. **Without it the defaulting is recorded and invisible, which is the same lie as refusing**: the founder approves a cost computed on numbers nobody showed them. Add a preview fixture state whose M0 attempt carries a defaulted `Detail` and a node test that the notice is ABSENT when the `Detail` names nothing — a notice that always shows is a notice nobody reads.

- [ ] **Step 4: Invert the uitests assertion, and keep its discriminator.** `uitests/tests/preview/activity-experience.spec.ts:339` asserts `marginComposerQuestion` has count 0 on a live SERVICE gate; it now asserts it VISIBLE. The case at `:349` — "…while the M0 gate, which HAS a question op, still offers the toggle" — was the discriminator that proved the composer was not question-less everywhere; it is now the discriminator for M0's **send-back** refusal instead, so re-point it rather than deleting it: a suite with two cases asserting the same thing has lost a guard. Add a fixture state carrying a construction round with a RESOLVED comment and a WITHDRAWN revision (Task 4's wire member and Task 12's paths, rendered), validate it with `fixture-schema.mjs`, and note in its `note` field that it is the first fixture to carry either.

- [ ] **Step 5: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/webApp
  ASDF_NODEJS_VERSION=lts npm run check
  ASDF_NODEJS_VERSION=lts node --test src/containers/activityVerbs.test.ts src/components/design/submitVerb.test.ts 2>&1 | tail -15
  cd ../uitests && ASDF_NODEJS_VERSION=lts npx playwright test --project=preview 2>&1 | tail -15
  cd ../webApp && grep -rn 'stage 4b' src/ ; cd ../uitests && grep -rn 'stage 4b' tests/
  ```
  Expected: `npm run check` green; the node suites green; preview **46/46** (44 plus the resolved-comment/withdrawn-revision state from Step 4 and the defaulted-assumptions state from Step 3a); and **both greps return nothing** — a surviving "stage 4b" string in the SPA is a notice explaining a refusal that no longer exists.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1
  git add webApp uitests
  git commit -F - <<'MSG'
  feat(webApp): the construction rail can be talked to

  Stage 5 hid four buttons on construction reviews because deliveryManager answered
  a guaranteed 400, and 4a's spec amendment was explicit that one contract is not one
  behaviour. Stage 4b1 made all five paths real, so the notices go — leaving them
  would be the mirror defect: a rail that can resolve a comment with the composer's
  Question toggle removed so the comment cannot be staged.

  Inverted, not deleted: the four node assertions and the uitests notice-line case.
  The M0 discriminator at :349 is re-pointed at the send-back refusal rather than
  dropped, because a suite with two cases asserting the same thing has lost a guard.
  allowAsk and allowQuestions STAY — they are driven from verbs.ask.kind and they are
  the right guard for the next rail that lacks a question op; deleting them would
  re-open the defect where one staged question replaced Approve and Send back with a
  button that dispatched nothing.

  NO_SDP_SEND_BACK and NO_SDP_RERUN stay too: M0 has no send-back because the plan is
  derived, and the SDP is re-derived rather than re-run.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 15: The drain note, the earmarks, the spec, and the measurement

- [ ] **Step 1: Extend the drain note** (`docs/bugs/2026-09-24-stage3-rail-earmarks.md`). Step 2 of the six-step sequence gains: drain `{p}:activity:*` (the new child's ids) and the seven RETIRED type names, whose workers this image no longer registers — a workflow started on `constructionConstructActivity`, `systemDesignCoAuthor`, `systemDesignPhase`, `systemDesignPhaseAdvance`, `projectDesignCoAuthor`, `projectDesignSDPReview` or `projectDesignPhaseAdvance` is **unresumable** after this release and must be terminated by hand, which is the bounded cost architect Ruling 3(d) accepted. Step 5 gains `delivery:roundSweep` as a THIRD Schedule to confirm with `temporal schedule list` (three now, not two) and `temporal schedule describe` (task queue `delivery`). Restate that 4b1 does not deploy alone: one drain covers 3 + 4a + 4b1 + 4b2. Strike through the **seven** entry criteria 4b1 discharged, each with its commit: the `ReviewRound` artifact-kind field, `RoundWithdrawn`'s wire member, `gateSubjectRef`, `OpenActivity`'s re-open, the stranded-round sweep, `applyRecovering`'s terminality class **with** the design-rail seed degradation, and `engineReviewPolicy`'s remaining half (the inline `NewReviewEngine()` collapse and the third `designActivityFor` copy — the `EffectiveGate` move was already stage 2's).

- [ ] **Step 2: Write `docs/bugs/2026-09-26-stage4b1-earmarks.md`**, carrying at least:
  - **GAP-4B-4 as a FOUNDER question, and it has THREE orphans, not two.** `operationalConcepts` (slot 6, status 2, 3 revisions), `standardCheck` (slot 7, status 4, the only non-committed slot) **and `scrubbedRequirements`** appear in no lifecycle and no required list — measured: v0.9.0's eleven artifact kinds are Construction, CoreUseCases, DetailedDesign, Glossary, Integration, Mission, SdpReview, SRS, STP, System, Volatilities, and none of the three is among them — yet all three keep live `DesignCommandFor` slugs and matching method-assets command files, and all three are named in `ReviewRoundInput.roundId`'s own contract text as kinds that share a lifecycle phase. Add them to the `requirements`/`architecture` lifecycles (a method-assets release) or declare them frozen-as-committed. Note that resolving this is what makes Task 3's `artifactKind` field load-bearing rather than defensive.
  - **When the founder edits the DEFAULTED planning assumptions** (R-E, controller override). Slot 8 absent no longer refuses: the compute fills The Method's defaults, records which families it assumed, and says so at M0. What is missing is the UX — an operating-model screen where the founder replaces an assumed calendar or rate card with a decided one, and a way to tell "assumed" from "accepted" once they have read it. Until then a founder can approve a cost on defaults they only ever saw in a copy line. **This is the open question, and "who authors slot 8" is NOT** — the compute does, provisionally, by design.
  - **The child's history budget is a floor, not a measurement** (R-L). 4,000 events and the 23-poll ladder are derived from fixture counts in a test env plus the production poll arithmetic; nobody has yet watched a real `requirements` activity's history length. First production run: record it, and re-tune the budget against the observed number rather than the predicted one.
  - The eight Phase-2 draft slugs are now dead-but-green and retiring them with their eight `.claude/commands/*.md` is one coordinated platform step (R-F). `DesignBranch` and the one live `origin/aiarch-design/archistrator/0-amend-1` branch survive unused for one release. `child.Get` is still the self-cascade (4b2). The three `kind:'session'` members are now two DERIVED views plus one live one (R-J) and collapsing them is 4b2's. The capture rig's `temporal`-CLI dependency is still stated only in a `t.Fatalf`. A PUSHED job-completion signal would remove the polling the backoff ladder mitigates, and needs an RA producer nothing has (4b2, and Task 8 Step 6's selector is the seam it plugs into). The stage-3 earmark *"no test covers a NEW send-back after a completed redraft carrying only the new round's comments"* — say whether Task 11's cases closed it, and if not, that it is still open. Back the file up to the scratchpad after writing it.

- [ ] **Step 3: Amend the spec.** §5.2: the child's acceptance is measured here, with the numbers Step 4 produces. §5.3's dispositions: root `phase` is **still stored and still moved at M0** by the child's gate-passed handler (R4); "derived from milestone position" is stage 6's. §5.4: strike what was already done (the `EffectiveGate`/`RequiresHuman`/floor move landed in stage 2; the `phase.String()` defect is fixed and the fix is load-bearing) and record what 4b1 did (the two inline engines deleted, the critique round folded into a review task's own command, the third `designActivityFor` copy deleted by the lifecycle data). §6: the compute writes slots 9–16 and READS slot 8, with R-E's measurement — **and records that slot 8 absent is DEFAULTED, not refused**, with the defaults' sources and the M0 copy line, because §6's own "no agent-drafted steps" is what makes a default the only alternative to a dead end. §7.2: GAP-6 is **CLOSED**, with the five paths and their verbs. §8: the 4b row splits — **4b1 SHIPPED** (contents, gates at ship, the earmark file) and **4b2 next** (the out-of-scope list). §9: the fixture count 19 → 8 fresh + 19 archived, and the shape cases' names.

- [ ] **Step 4: Measure and report the line count** (R11/GAP-4B-1):
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b1/server
  wc -l internal/manager/delivery/*.go | sort -rn
  # hand-written non-generated non-test = total − manager_test.go − (contract+activities+invokers+worker).gen.go
  ```
  Report three numbers against the `4baed01a` baseline (hand-written **25,643**; non-test **29,778**) and the 4b1 base `86d3223a` (hand-written **25,162**; non-test 27,379), name the recipe, and state plainly whether spec §9's "line count of delivery manager < sum of predecessors" holds — it is 4b2's gate to pass, not 4b1's, and reporting a number that does not yet clear it is the honest outcome, not a failure.

- [ ] **Step 5: Commit** the docs (`git add docs/bugs docs/superpowers/specs`) with a message naming the seven discharged entry criteria, the drain's three new obligations, and the measured line count.

---

## Self-review

### Spec → task coverage

| Spec clause | Where it lands | Evidence it is discharged |
|---|---|---|
| §3 lifecycles are platform-fixed data; a `review` names the dispatch it judges and that pair is what a send-back re-opens | Tasks 8 (walk + `reopenJudgedPair`), 10, 11 | the `sendback-reopens-only-the-judged-pair` shape case; `reopenJudgedPair` reads `t.Reviews` and nothing else |
| §5.1 the pump | **unchanged in 4b1** except the child it starts (Task 9 Step 6, Task 11 Step 4) — react-by-signal is 4b2 (architect Ruling 3(b)) | the `child.Get` comment naming 4b2 |
| §5.2 one generic DAG walker replaces `walkPhases` + both co-author files; per-type differences are data and strategy, never a branch on design-vs-construction | Tasks 8, 10, 11, 13 | `Test_DeliveryActivityWalker_NamesNoTypeOrCommand`, mutation-checked |
| §5.2 acceptance: merged workflow code materially smaller | Task 15 Step 4 — MEASURED and reported; 4b2's gate (R11) | the three-number recipe |
| §5.3 one stage → review → commit rail; activity branch `activity/{activityId}` | Task 10 Step 3 (`activityBranchName`, reused for both rails — no new exported helper) | construction already used the scheme (`activityBranchName`, `constructactivity.go:684`, five callers); `DesignBranch` retires in 4b2 |
| §5.3 two append-only ledgers, everything else derived | shipped in stage 3; 4b1 adds `ReviewRound.artifactKind` (Task 3) and the `RoundWithdrawn` wire member (Task 4) | the two-kinds-one-gate case; `roundJoinKey` |
| §5.3 root `phase` | Task 9 Step 4 — R4: still stored, still moved at M0; the spec amended in Task 15 Step 3 | `nextEligibleActivity:9387`, `pumpsweep.go:88`, `AdvancePhase = p.Phase++` |
| §5.4 ONE review engine; the critique becomes an ordinary agent reviewer; the design slot→phase table's third copy | Tasks 8 Step 4 (the injected `wf.Review`), 10 Step 4, R-H | `designActivityFor` deleted by the lifecycle data; `Test_DesignActivityFor_Phase2KindsAreNotTheSdpGate` deleted WITH its carve-out |
| §5.5 body-carried-id authorization | nothing new — R9 keeps all twelve signatures byte-identical | `TestCompositionRootGuardsEveryCatalogScopedDeliveryOp` in every drift block |
| §6 deterministic Project Design: compute, stage, M0 review, no send-back, Architecture re-open ⇒ recompute | Task 9 | the dial-equality test; the refusal naming the amendment path; the pump's stale-basis eligibility; slot 8 DEFAULTED and disclosed, never refused (R-E) |
| §10 god-workflow / history growth | **R-L**, built into Task 8 Steps 6, 7 and 10 | the backoff ladder's 23-vs-240 arithmetic; `ContinueAsNew` at `inflight == 0`; the `continue-as-new-mid-walk` case |
| §7.2 R2/GAP-6 the rail becomes write-unified | Tasks 12 (server) + 14 (SPA) | `grep -n 'stage 4b'` returns nothing in `server/`, `webApp/src` and `uitests/tests` |
| §8 drain-and-cutover where task queues / types change owner | Task 15 Step 1 | the seven retired names + `{p}:activity:*` + the third Schedule |
| §9 child cases per shape; the review-engine table with a kind-validating fake; the rail's two cases | Tasks 1, 8, 10, 11, 12 | **ten** shape cases (§9's seven, plus R-L's `continue-as-new-mid-walk` and the router's `fork-signal-reaches-the-named-task` and `full-inbox-does-not-wedge-the-router` — none of which §9 names, because collapsing four executions into one walk is what creates an unbounded history, a shared-channel race and an overflow path at once), all green by Task 11; Task 12's fake tightening |
| §10 god-manager guarded by keeping per-type behaviour in data/strategy | R-C's arch guard | scoped to the four walker funcs, mutation-checked both ways |

### Architect Ruling 3 → task

| Ruling 3 clause | Task |
|---|---|
| (a) ONE type with a dispatch-strategy seam; the walker names no type | 8 — ONE injectable `strategyRegistry` holding all three slots (+ R-C's guard) |
| (b) react-by-signal pump, pump-as-merge-queue, `PumpResult.activityIds` | **4b2** — listed once under "Out of scope" |
| (c) the projectDesign child, the compute recorded as a `TaskAttempt`, the invalidation hook in the PUMP | 9 (Steps 3, 4, 6) |
| (d) the 19 fixtures archived as evidence, a fresh set at cutover | 1 Step 6, 13 Steps 1–2 |

### Placeholder scan

**Count: 0.** No step says "and so on", "etc.", "update the tests", "handle the rest", "TBD" or "<fill in>". Every code step carries its code; every gate step carries its command and its expected output; every task ends in a commit.

Six steps deliberately delegate a LITERAL or a body to a mechanical oracle rather than transcribing it, and each names the oracle:
1. **The nine `drive*` bodies and `assertShape`'s per-case rows** (seven in Task 1, two added in Task 8 Step 10) — the oracle is the existing replay scenarios' rig (`manager_test.go:28198`, `:28153`, `:13401`), whose exact `sed` ranges Step 1 gives; what each case must assert is stated per case, in Task 1 Step 4's prose and in the two new rows' own comments. Inventing a second rig would let the shape cases and the fixtures diverge, which is the one thing this task exists to prevent.
2. **Task 2's thirteen rule bodies** — the oracle is the Manager's own source at the two line ranges, moved one rule at a time with `validate --slot System` re-run after each. Transcribing 724 lines into a plan would be a copy of the repo.
3. **Task 5's `%s` rendering of `ActivityType`/`TestingVariant`** — the oracle is whether each has a `String()`; the step says to check and to use it rather than printing an ordinal at a human.
4. **Task 8 Step 5's `awaitTaskDecision`** — the oracle is the existing gate's vocabulary, budgets and signal ORDER, each cited by line (`:817`, `:1609-1615`, `:1625-1670`, `coauthorartifact.go:588`). The step's instruction is "carry forward verbatim", which is stronger than a re-derivation.
5. **Task 13 Step 6's golden literal** — the oracle is `TestRegisteredTemporalNamesGolden`'s own printed diff, and Step 9 states the derivation (139 → 140 → 141 → 139 → **134**) so the diff is checked against an expectation rather than accepted. A hand-computed golden is how a dropped dep hides.
6. **Task 15 Step 4's three numbers** — the oracle is `wc -l` plus the stated arithmetic. The plan gives the recipe and the two baselines, not a predicted result.
7. **Task 10 Step 3's `agenticDispatchStrategy.Produce` body** — the oracle is the co-author session's own command order, whose six `sed` ranges Step 1 gives; every element of the sequence is an existing helper, and the strategy's body is the sequence. It is the largest prose block in the plan, deliberately: transcribing five helper bodies would be a copy of the repo, and re-deriving the order from scratch is how a `openPR`-before-read-back regression gets written.
8. **Task 9 Step 3a's `defaultPlanningAssumptions` body** — the oracle is the five existing default constants it must reuse (`defaultRateSpec`, `defaultModelForClass`, `defaultMTokInPerDay`, `defaultMTokOutPerDay`, `defaultIndirectDailyRate`), each cited by line, plus the sourced rationale table for the four values that are not already constants. Restating their numbers would create a second copy to drift.

### Type and name consistency

- `deliveryActivity` (registered type) / `DeliveryActivityWorkflow` (entry) / `deliveryactivity.go` (file, forced by `arch.CheckFileLayout`'s `ToLower(TrimSuffix(entry,"Workflow"))+".go"`) / `deliveryActivityWorkflowID` / `deliveryActivityInput` / `executionKindDeliveryActivity` — spelled identically in Tasks 8, 9, 10, 11, 12, 13 and 15, and distinct from the pre-existing `deliveryActivityOptions` (`deliverymanager.go:12360`), which this plan does not rename.
- `deliveryRoundSweep` / `RoundSweepWorkflow` / `roundsweep.go` / `roundSweepWorkflowID` / `scheduleIDRoundSweep = "delivery:roundSweep"` — Tasks 6, 13 Step 6 and 15 Step 1, same spelling, and the Schedule id carries the `delivery:` prefix 4a established.
- `changeRowConflictReread` / `terminalConflictErrType = "MutateTerminalConflict"` / `isTerminalConflict` — introduced in Task 7 and called by name in Task 6 Step 2 (with the ordering caveat stated there) and nowhere else.
- `roundGateKey` (the gate identity) and `roundJoinKey` (the revision identity) — both introduced in Task 3 Step 4 as **two arities over one rule, mandatory not optional**; `roundGateKey` is called by Task 6 Step 2 (`strandedRounds`) and Task 12 Step 2 (`latestRoundFor`). No second key function appears anywhere.
- `taskStrategy` / `taskContext` / `producedSubject` / `strategyRegistry` / `strategyFor` / `strategySlotDispatch` / `strategySlotJudged` / `strategySlotCompute` — Task 8 Step 2 defines all of them with ONE shape (`map[string]func(*csWorkflows) taskStrategy`, held on `wf.Strategies`); Task 9 adds the `compute:SdpReview` entry; Tasks 10 and 11 complete `agenticDispatchStrategy`. There is no corrected-later signature anywhere in the plan.
- `walkState`'s six maps (`byTask`, `revision`, `feedback`, `produced`, `inbox`, `pending`) and `walkSnapshot`'s five exported fields (the four walk maps plus `Pending`; `inbox` holds channels and is re-created, never carried) — declared together in Task 8 Step 3, read in Step 4 (`ws.produced[t.Reviews]`, `ws.feedback[t.ID]`), written by the router in Step 3a (`openInbox`/`closeInbox`/`deliver`) and serialised in Step 7. `produced` is what carries the judged task's staged ref to its gate, which is what makes Task 5's fix observable; `pending` is what stops a `ContinueAsNew` dropping a routed-but-undelivered signal.
- `routedSignal` + `routedKindDecision|Status|Override|Redraft`, `routeSignals`, `openInbox`, `drainPending`, `closeInbox`, `deliver`, `deliveryTaskInboxCapacity` — Task 8 Step 3a defines them; Step 4's `runTask`/`runGate`, Step 5's `awaitTaskDecision` and Step 6's observe selector all take ONE `inbox`, none of them names a shared signal channel, and **both receive loops call `drainPending` at the top of every iteration**; Step 3's receive arm calls `closeInbox(ctx, logger, taskID)`; Task 12 Step 3 sets `TaskID` on the redraft it sends. `deliver` takes no `ctx`, which is the signature saying it cannot block. `closeInbox` clears **both** `inbox` and `pending` for its task; Step 6's observe loop is the one place that must NOT use `pending` as a hand-back, and it uses a local `deferred` slice instead with the spin argument written at the site.
- The FOUR shared signal channels (`decisions`, `statuses`, `overrides`, `redrafts`) are opened in one place — Step 3's entry func — and handed to `routeSignals` and to nothing else. `signalRedraft` appears in the Interfaces block, in Step 3's opener, in Step 3a's router and in Task 12 Step 3's sender: four mentions, one channel, and it is no longer named by a step that cannot reach it.
- `taskState`'s five ordinals are payload-visible through `walkSnapshot.ByTask` and are append-only, stated on the type in Step 3 and pinned by `Test_TaskStateOrdinalsNeverRenumber`, which Step 11's gate block runs.
- `passRound(ctx, in, lc, t, tc, state, decidedBy, reason)` — **takes `lc`** in Task 8's Interfaces block, in Step 4's call and in Task 9 Step 4's M0 handler. One signature, three mentions.
- `lifecyclePhaseByID`, `gateActorSystem`, `reasonOf`, `roundArtifactKind`, `seedWalkFromLedger`, `openActivityRow`, `bindRowAccessors`, `recordTaskAttempt`, `finalizeWalk`, `failWalk`, `shouldContinueAsNew`, `observeInterval`, `defaultedDetail` — all declared in Task 8's Interfaces block or in the step that writes them; none is used before it is named.
- `lifecycleShapeCase` / `lifecycleShapeCases()` / the **ten** case names (seven from Task 1 plus `continue-as-new-mid-walk`, `fork-signal-reaches-the-named-task` and `full-inbox-does-not-wedge-the-router` from Task 8 Step 10) — Task 1 fixes the first seven, and Tasks 8 Step 10, 9 Step 7, 10 Step 6 and 11 Step 5 each name the subset they turn green. `shapeOutcome.TaskOrder` (declaration order, asserted IDENTICAL in both fork cases) and `CompletedOrder` (the discriminator) are used with that meaning in Task 1 Steps 2/4 and Tasks 8 Step 10 / 11 Step 5. `stubStrategy`/`stubStrategies` are introduced in Task 8 Step 10 and deleted in Task 11 Step 5, both stated; the registry seam that hosted them stays.
- `ReviewRound.ArtifactKind` / `ReviewRoundInput.ArtifactKind` (Task 3) and `TaskRevisionWithdrawn` / `revWithdrawn` (Task 4) are the only two wire additions, and both are APPENDED.
- `solutionDials` / `derivedSolutionDials(kind, staffingCap)` / `riskModelFrom(rows, risks, recommendation)` / `computeProjectPlan` (returning `(ref, defaulted, err)`) / `defaultPlanningAssumptions` / `sdpComputeStrategy{wf}` / `subcriticalStaffingCut` / `compressedCriticalSpeedup` / `decompressedBufferDays` — Task 9 only, each with ONE signature in both its Interfaces entry and its code block.
- `activityBranchName` (`constructactivity.go:684`, an EXISTING helper with five callers) is reused by name in Task 10 Step 3 and moved by name in Task 13 Step 3. No `projectstate.ActivityBranch` is created.

### The three claims a reviewer should check first

1. **The four solution slots are three numbers each.** Re-derivable in one read: `assembleOption` (`assemblesdpreview.go:355-418`) uses `sol.StaffingCap`, `sol.BufferDays` and `sol.CriticalSpeedup` and no other `sol` field — its `ClassRates` come from `deriveClassRates(pa, classes)` and the per-option `CalendarDaysPerWeek` was retired by F5, both stated in the function's own comments. If it reads a fourth, R-E's dial table is incomplete and Task 9 Step 1 says to stop.
2. **The re-read is a sufficient discriminator for a terminal `Conflict`.** The claim is that `OpenActivity` on an exited row and `Decide`/`Append` on a decided round move neither the project version nor the row version, while every genuine race moves at least one. Re-checkable at `projectstateaccess.go:10060-10066`, `:10255-10263`, `:10415-10425`. If a store path can refuse while ALSO bumping a version, R-A is wrong and Task 7's terminal arm would mis-report a race as terminal — bounded and loud, but wrong.
3. **The design rails were already venue-blind.** Recon §3.3's measurement is that `dispatchDesignJob` goes through the same `agenticJobAccess` the construction rail does and no `aiarch-*.yml` name appears in the design path. If a venue fact is hiding in the design dispatch, Task 10's single `agenticDispatchStrategy` is two strategies wearing one name, and R-D's reader disposition is wrong with it.
4. **`readyTasks`' declaration-order fan-out is deterministic, and branch order is therefore NOT a walker variable.** `ValidateLifecycle` guarantees every `dependsOn` appears earlier in `Tasks`, so the slice is a topological order and `readyTasks` returns `[detailedDesign, stp]` on every run. Everything the two fork cases assert rests on this: identical `TaskOrder`, differing `CompletedOrder`. If a lifecycle ever ships tasks out of topological order, `readyTasks` is wrong before the oracle is, and the shape cases' `TaskOrder` assertion is what will say so.

**Open questions carried to the founder** (neither blocks execution; both are recorded in Task 15 Step 2): **when and where the founder edits the DEFAULTED planning assumptions** — the compute now fills them and discloses the defaulting, so the gap is a UX one and no longer "who authors slot 8" — and what becomes of `operationalConcepts`, `standardCheck` and `scrubbedRequirements`, the **three** kinds with live draft commands, committed or half-committed slots, and no lifecycle task between them.
