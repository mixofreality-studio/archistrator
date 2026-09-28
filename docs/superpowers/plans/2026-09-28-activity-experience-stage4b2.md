# Activity Experience — Stage 4b2 (DeliveryManager: the pump, the folds, the retirements) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the project's ONE pump grant a lease instead of blocking on a child, and land the folds and retirements the drain has to cover with it. 4b1 put every activity on one generic DAG child; 4b2 deletes `child.Get` (`pumpnextactivity.go:230`) so the pump starts every eligible child, parks on a `Selector` over lease and finish signals, and reconciles against Temporal's execution state every 30 s — which alone collapses **27 full 1.17 MB `project.json` reads to 1** on this repo's plan. The merge does **not** move into the pump (architect Ruling 1(b) restated): the pump serialises **admission**, the child keeps its merge, its variance loop, its human hold, its credential and its branch. Around that go one live sweep defect, two dead resolvers, a two-derived-types-into-one wire fold, `reconcileBranchFromMain(kinds []ArtifactKind)`, ONE model edit that removes revenue share as a concept and retires two artifact kinds, a webApp provider re-mount that turns the Deployment lens back on, the `gitActivityStatusAccess` facet, and the pump's first replay fixtures since it was written.

**Architecture:** The pump goes **LAST among the code changes**, and that is the whole shape of this plan. 4b1's most productive finding class was *a precondition that lived in a deleted body and was not re-asserted in the body that replaced it* — eight instances, every one invisible to the test suite. The pump is this wave's deleted body. So Task 1 is a **guard census** that enumerates every guard-shaped line in `pumpnextactivity.go` and `replansweep.go` and pins each one with an executable assertion **before** anything touches them, and Task 12 is judged against that list rather than against a re-derivation. Everything cheap, local and independently revertable lands in front of it (Tasks 2–11), so that when the first parallel cascade misbehaves there are ten fewer candidate causes. There is **ONE `project.json` edit and ONE regen** (Task 7) — every contract delta in the wave queues behind it, because each regen costs the same nine generated surfaces and three `gen-*-check` drift risks. **Drain-and-cutover, not `GetVersion`:** 4b2 is inside the SAME single drain as 3 + 4a + 4b1, no `{p}:nextActivity` execution survives it, and `changeGenericActivityChild` was already retired in 4b1 Task 13 on exactly this argument.

**Tech Stack:** Go 1.26 (`GOWORK=off` always), Temporal Go SDK v1.44.0 (`workflow.Selector`, `GetSignalChannel`, `NewContinueAsNewError`, `GetContinueAsNewSuggested`, replay testing), `temporal` CLI 1.7.0 at `/opt/homebrew/bin/temporal` (Server 1.31.0) for fixture capture, `.aiarch/state/project.json` as the model database (git-as-DB), modelgen / clientgen / appgen / temporalgen codegen, `framework-go@v0.11.1` (`arch.CheckFileLayout`, `methodcheck`, `fwra`), `method-assets@v0.9.0` (`lifecycles.json` — PINNED; Task 8 is the one task that proposes a release), React 19 + TypeScript 5.9 for the SPA, Playwright 1.50 in `uitests/`.

**Spec:** `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` — §5.1 (the pump), §5.2 (the child, whose merge tail this plan deliberately does not move), §8's **4b2 row**, §9 (testing, and the one owed bullet), §10 (risks: parallel children, the god-manager, the ONE drain). **Architect rulings this plan executes:** `scratchpad/stage4b2-architect-rulings.md` Rulings 1 (the react-by-signal pump), 3 (the session-view fold and the fork-steer precheck), 5 (scope discipline and THE ORDER), 6 (the six founder questions). **Controller rulings that OVERRIDE the architect where they differ:** `scratchpad/stage4b2-status.md` — FOUNDER RULINGS R-F1…R-F4 and the five CONTROLLER RULINGS. **Measurements:** `scratchpad/stage4b2-recon.md` (928 lines, measured at `bae7681f`). **Earmarks that gate this stage:** `docs/bugs/2026-09-26-stage4b1-earmarks.md` (the 4b2 carry list, the OPENED-by-the-final-fix-wave section), `docs/bugs/2026-09-24-stage3-rail-earmarks.md` (the DRAIN NOTE). **Predecessor plan whose format, density and constraints this plan carries forward:** `docs/superpowers/plans/2026-09-26-activity-experience-stage4b1.md`.

---

## Rulings carried into this plan

Each was decided before or during planning. An implementer does not re-litigate them. **R1–R13 are the controller's and the founder's and BIND.** R-A–R-H were ruled while writing this plan, each with the measurement that forced it. **C1–C3 are conflicts between a binding ruling and the code, with the resolution this plan takes** — they are called out as such because a reviewer must check them first.

| # | Ruling |
|---|---|
| **R1** | **The pump is ONE workflow, `{p}:nextActivity`, and it grants a LEASE — it does not do the merge.** Measured: the child's tail holds three distinct main-writing steps that are not one thing — `runWalkMerge` (`deliveryactivity.go:3539`) with its own variance loop, its own escalation inbox and `holdForMergeApproval` (`:3674`, a HUMAN gate); `finalizeActivity` → `mergeAndRecord` (`:4085`) with `guardMergePreconditions` (`:4133`) and the approve-time credential re-mint; and `commitDesignArtifacts` (`:3351`), N slot commits through `designSessionAccess.CommitArtifactWithProvenance`. Moving (1) into the pump moves a human gate, a variance loop, a signal inbox and a gate ledger into it — §10's god-workflow by the back door, one wave after seven workflows were deleted to avoid it. **THE INVARIANT: at most one activity of a project holds the main-write lease at a time.** Row-level CAS and `applyMutationOnBranchFiles`'s dedup + version guard + ref-CAS **stay** and are what serialise per ROW; the lease is the main-level mechanism and the two are not the same mechanism. |
| **R2** | **NO FACT FROM A SIGNAL ALONE.** The finish signal is an *optimisation over the tick*, never the source of truth. The 30 s tick becomes a **RECONCILE**: for each started-and-unfinished activity, ask Temporal whether the execution is CLOSED (the façade already has `pumpRunClosed`, `deliverymanager.go:6073`) and treat a closed execution as finished, releasing any lease it held. That one rule answers signal-lost, child-died-before-signalling and child-died-holding-the-lease at once. |
| **R3** | **No new `GetVersion` fence, and the five existing ones in `pumpnextactivity.go` are NOT touched.** The drain is the mechanism (4b2 is inside the SAME drain as 3 + 4a + 4b1). `changeGenericActivityChild` was already retired in 4b1 Task 13 with the reason at the site: *"a `GetVersion` marker whose other arm names a workflow the build no longer has is worse than no marker — it compiles, records a version, and then panics differently."* A fence over the react-by-signal rewrite would have to keep the blocking arm alive, which keeps `child.Get` alive, which is the thing being deleted. Surviving untouched: `pumpPausedBehindGate`'s two ids (`pump-pause-before-dispatch`, `pump-drain-pause-before-continue-as-new`), `changePumpHonorsRecordedPause` (v2), `"pump-pause-decode-any"`, `changeLedgerPartialResume`, `changeDesignActivitiesDispatchable`. |
| **R4** | **`PumpResult.activityIds` is ADDED; `activityId` stays populated with the first element and is deprecated in place.** Additive, never renumbered — the 4a convention. **Measured, and it reframes the item: nothing reads `activityId` today.** `useExecuteNextActivity` narrows the response body to `{ dispatched?: boolean }`; the SPA drives the pump through the mutation and re-reads `QueryProjectView{pump}` for `PumpStatus{open, runStartedAt}`. The list serves MCP, `systemtests/usecases/uc3_construction_test.go` and the operator log. Do it because a plural pump reporting one of N is a lie; do not do it believing a screen is waiting. **`PumpResult` must NOT grow a `stillDeciding` outcome** — `pumpDispatchWaitBudget`'s OPEN note asks for a contract change the new shape makes unnecessary. |
| **R5** | **Engines 7→5 is OUT.** Both agents concluded independently: there is no `method-check` Utility in slot 5 (the four are `security`, `logging`, `diagnostics`, `message-bus`), `methodcheck` is PLATFORM code, spec §1's 2026-09-27 amendment already deferred the dissolution, and `DH-CARD-ENGINES` warns at 7 and still warns at 6. The merge buys **zero** gate movement and costs one indivisible self-amendment commit. **Its own wave, after the deploy.** |
| **R6** | **Facets: only `gitActivityStatusAccess` goes.** Measured: **zero production callers on all six ops**. The other two leave FIVE homeless verbs (`RecordOperatorPaused`, `RecordChangeReviewed`, `ReadProjectOnBranch`, `ReconcileBranchFromMain`, `SeedReviewCommentsOnBranch`) and `activityExecutionAccess` sits at **exactly 12**, the App-C ceiling `DH-CONTRACT-OPCOUNT-MAX` pins ABSENT at `internal/engine/designhealth/engine_test.go:67`. They wait for the `projectCatalogAccess` split, which is in no 4b2 row. |
| **R7** | **REMOVE REVENUE SHARE AS A CONCEPT, COMPLETELY (founder, R-F1).** Verified against committed objective 3: *"Operations-first revenue with room to grow: … a usage-based fee for operating delivered systems … while keeping the door open to charging for other value it creates — design and construction work, tokens, or consulting."* Revenue share is not among the named growth paths, so deleting it is consistent with the ratified objectives, not a narrowing of them. This **SUPERSEDES** the architect's Q3 recommendation to add a `RevenueShareNone` member: the concept goes, so the vocabulary that could not express "none" goes with it. It also removes the defaulting arm that made slot 8 uncomputable from 2026-06-09. |
| **R8** | **`scrubbedRequirements` and `standardCheck` are RETIRED (founder, R-F3)** — slugs, command files, critique entries, renderer paths and their mentions in `ReviewRoundInput.roundId`'s contract text. This SUPERSEDES the architect's Q1 split. **See C1 for what "completely" can and cannot mean against an ordinal enum.** |
| **R9** | **`operationalConcepts` IS the Deployment diagram and is NOT an orphan to retire (founder, R-F2).** MEASURED — it already is: `ArchitectureView.tsx:165` reads `useCommittedSlotEnvelope('operationalConcepts')` → `listDeploymentProfiles` → the Deployment tab is `disabled={deploymentProfile === undefined}` (`:589`). The slot is committed (status 2, 3 revisions) with 3 environments / 5 infrastructure / 16 bindings. **See C2 for the split between the half that is free and the half that is a platform release.** |
| **R10** | **Construction questions are HUMAN-ANSWERED; label the addressee (founder, R-F4).** An agent answering about code it wrote, to clear a thread gating its own merge, is the autogate hazard in costume — the one 4b1 had to close twice. Ship the label, write no command. |
| **R11** | **`ReplanSweepWorkflow` is DELETED** — 47 lines, `flagVariances` is `return nil` (`replansweep.go:45`), the nil-projectID arm returns empty and is unreachable over both transports, and a Temporal Schedule (`delivery:replanSweep`, 300 s) fires it every five minutes **to do nothing**. A Schedule that fires a stub is worse than no sweep: it reads as coverage. Its workflow, its Schedule, its frozen name and its façade op `deliveryManager.ReplanProject` all go. **`constructionPumpSweep` is ADDED to the frozen-names list** in the same edit — a real hole by `deliveryRoundSweep`'s own argument. |
| **R12** | **Replay fixtures for the pump are the wave's highest risk and are captured LAST, after `child.Get` goes.** A capture that registers only the pump parks forever on `child.Get`; capturing after the rewrite means the pump's history has no child future in it at all. It needs a **SECOND registration list**, not a widened one — `deliveryReplayRegistrations` (`manager_test.go:13709`) names ONE workflow *by design* and its doc comment says so — its own fixture directory landed in the **same commit** as its case list (`Test_Replay_DeliveryHistories`'s directory-level orphan guard fails on a directory no case list names), **≥4 drivers** (one per `GetVersion` arm), the 20-event floor cleared, the `temporal` CLI, and a ~6-minute await. |
| **R13** | **Out of scope, each with where it goes** — stated in the plan's own Scope section below, because 4b1's ten-item carry list is what made 4b2 plannable. |
| **R-A** | **The lease is granted to an ACTIVITY ID, and the pump validates it against two things it already holds.** Temporal does not authenticate a signaler. The pump reads the committed plan every run (`committedPlanInputs`, `deliverymanager.go:7719`) and owns its started set; an id in neither is **logged and DROPPED, never granted**. `epoch` on the grant is not decoration: it is what lets a child recognise a grant revoked and re-granted underneath it and refuse to act on a stale one. |
| **R-B** | **Delivery is `messageBus.DeliverSignal` — no new RA producer.** Measured: `relayPauseToPump` (`projectsupervision.go:143-156`) is the ONE existing example of an out-of-band signal reaching the pump, it rides the generated `MessageBusDeliverSignal` invoker (`invokers.gen.go:500`), and `isSignalTargetNotFound` (`:173`) already tolerates a target that is not running. The lease signals generalise exactly that shape. `messageBus` keeps its 2 ops; **no contract change**. |
| **R-C** | **The CAN rule: continue-as-new ONLY at zero held lease and zero unanswered request, and drain every channel non-blocking immediately before it, carrying what is found in the input.** `pumpnextactivity.go` **already documents this exact trap** at pause check 3: *"a signal still buffered on a run that ends in ContinueAsNew is NOT carried into the next run."* The CAN payload grows from `pumpInput{ProjectID, OperatorDriven}` to carry the started set, the lease state and the carried signals — bounded by the activity count (30 on this repo), so the unbounded-history property survives. |
| **R-D** | **The session-view fold is ONE derived type + ONE live type. `constructionSession` does NOT fold in.** Measured: `designCompletedSessionView` (`deliverymanager.go:222`) and `planCompletedSessionView` (`:4250`) are the SAME FUNCTION and the ONLY producers of their types, and between them they emit exactly **three** stages — `ReviewCommitted → Committed`, `ReviewWithdrawn → Withdrawn`, everything else → `DraftFailed` (read at `committedSessionView`, `:237-270`). Every other member of both the 8- and the 9-member enum is **unreachable**, including `ProjectStageAssemblingSDP`, the one member that made the two enums structurally different, whose producer 4b1 deleted. Neither producer sets `Critique`, `FailureRunURL`, `RunURL`, `Findings`, `StageName`, `ActiveRole`, `ActiveStep` or `Round`. `constructionSession` is a different SUBJECT (an activity, not an artifact kind), a different LIFETIME (a running execution's live query) and 12 non-overlapping members; folding it behind a discriminator gives one type whose every consumer branches on the discriminator immediately. |
| **R-E** | **`resolveQuestionBranch` is DEAD BY TYPE, and questions resolve to MAIN.** `isLiveSessionStage`'s true-set is `{Drafting, AwaitingReview, Redrafting, Refused}` (`deliverymanager.go:895-899`); `committedSessionView`'s output-set is `{Committed, Withdrawn, DraftFailed}`. **The sets are DISJOINT — the guard is false for every possible input.** The substantive case for main is that a question's thread **outlives the branch squashed at merge**: `activity/{activityId}` is deleted at merge, and the slot's thread on main is what the SPA reads, what the answer job answers, and what a reader finds six months later. Ratify main, delete the resolver, take the earmark off the list (4b1 Q6 closes). |
| **R-F** | **`pumpsweep.go:88`'s phase filter is a LIVE DEFECT, not a nicety.** `if s.Phase != projectstate.PhaseConstruction { continue }` mirrors the pre-4b1 blanket gate that `nextEligibleActivity` replaced with `admissibleInPhase` (`deliverymanager.go:7818`). A project in Phase 1 or 2 is therefore **never swept**, and the three design activities 4b1 made dispatchable are reached only by a manual `Begin`. It is one line and it is the difference between "the pump runs design" and "an operator runs design". |
| **R-G** | **The fork-steer fix is a RE-ORDER and a DELETION, not a new field.** `OverrideActivity` (`deliverymanager.go:6331`) prechecks `view.Stage != StageAwaitingTakeover` at `:6357` off the single-valued `constructState.stage`, and **fifteen lines later** calls `m.escalatedTask` (`:6474`) which resolves the task from `escalatedTaskOf(row)` (`:9271`) — the LEDGER, which 4b1 proved correct on a fork. Ask the ledger FIRST; if it resolves, the steer is legal and names its task; if it does not, refuse with the same sentence. The session read stays only to distinguish LIVE from TERMINAL, which is what the `isManagerNotFound(err)` arm at `:6365` already does. **No wire change. The per-task session view is 4b3's and this fix does not wait for it.** |
| **R-H** | **ONE `project.json` edit, ONE regen, ONE task (Task 7), and every contract delta in the wave queues behind it.** Measured surfaces per regen: `contract.gen.go` × N packages, `fake/fake.gen.go`, `toolcatalog.gen.go`, `activities.gen.go`, `invokers.gen.go`, `worker.gen.go`, `cmd/server/main.gen.go`, `cmd/aiarch-state-mcp/rawexec.go`, `api/openapi.yaml`, `webApp/src/contracts/{schema.ts,enums.gen.ts}`, `webApp/src/api/ops.gen.ts`, `systemtests/internal/sdk/*.gen.go` — plus nine `gen-*-check` gates and `pruneStaleSDK`'s deletion of SDK files a separate Go module's hand-written harness calls. **Never two implementers on `project.json`, even in disjoint regions.** |

### The three conflicts between a binding ruling and the code, and how this plan resolves each

| # | Conflict | Resolution |
|---|---|---|
| **C1** | **R8 says `scrubbedRequirements` and `standardCheck` are "RETIRED completely (slots, …)". `ArtifactKind` is an ORDINAL enum `0..16` (`projectStateAccess.$defs.ArtifactKind`), and the two kinds sit at **2** and **7** — in the MIDDLE. Deleting the members renumbers 3..16, which every rule in this repo forbids and which would silently re-key every committed slot in every project. Worse, `Phase1RequiredKinds` (`projectstateaccess.go:2996-3018`) **already carries a ratified 2026-08-30 founder ruling that retired exactly these two plus `operationalConcepts` IN PLACE**, with its reason written down: *"the kinds, their ordinals, their slots and `IsPhase1()`/`AllArtifactKinds()` membership are all untouched, because the ordinals are wire values in every already-committed project.json and existing projects still carry those slots."* | **Retire the DRAFTABILITY, keep the ORDINALS and the committed slot data.** What goes in 4b2: the two `designKindSlugs` entries, the `designKindHasCritique` `scrubbedRequirements` case, the three `.claude/commands/*.md` files (`scrubbed-requirements-draft`, `scrubbed-requirements-critique`, `standard-check-draft`), the `ReviewRoundInput.roundId` contract text that names them, and any renderer path that offers them as a drafting target. What STAYS: the enum members at ordinals 2 and 7, `slots["2"]` (status 2, 3 revisions) and `slots["7"]` (status 4, 3 revisions) as durable history, and `GlossaryView`'s cross-slot read of `scrubbedRequirements` (a join against a committed artifact of the past is a READ of the record, not a live drafting rail). **Deleting committed slot data from a git-as-DB is destroying the durable record this platform exists to produce**, and the founder's words — "old artifacts we used to have but have since removed" — describe exactly the retired-in-place state that already holds. Task 7 Step 9 states this in the commit message. |
| **C2** | **R9 says give `operationalConcepts` "a home in the ARCHITECTURE lifecycle", and the controller folds it into "ONE model edit + ONE regen". Measured: lifecycles do NOT live in `project.json`. They live in `lifecycles.json` inside `method-assets@v0.9.0` (PINNED; `parseLifecycles` uses `DisallowUnknownFields` and `mustParseLifecycles` panics at package init). The `architecture` lifecycle has exactly two tasks, `architectureDraft`(System) and `architectureReview`. Adding an `operationalConceptsDraft`/`Review` pair is a **platform release** — and it breaks `Test_Phase1RequiredKinds_AreExactlyTheTwoDesignLifecyclesOutput` (`manager_test.go:18654`), which asserts `architecture produces … want ONE kind` and that the produced set **equals** `Phase1RequiredKinds()`. Making it green means adding `KindOperationalConcepts` back to `Phase1RequiredKinds()` — **reversing the ratified 2026-08-30 collapse ruling** and putting the Phase-1 seal back behind a kind. | **Split R9 into its two halves and ship the free one now.** The founder's stated complaint was *"that is disabled now for some reason but it should not be"* — and the measured root cause is the **provider**, not the lifecycle: `ActivityExperienceContainer.tsx` renders `ArtifactRenderer` but mounts none of the three cross-slot providers. **Task 9 fixes that and turns the Deployment lens on today, at zero contract cost.** The lifecycle-home half is **Task 8**, written in full and marked a **FOUNDER STOP**: it needs a method-assets release, a `Phase1RequiredKinds()` change that reverses a ratified ruling, and a Phase-1 seal that then waits on `operationalConcepts`. Task 8 is the LAST task in the order and the ONE task that may be dropped without touching any other task's gate block. If the release is refused, Task 9 alone satisfies the founder's complaint and `ReviewRound.artifactKind` stays defensive rather than load-bearing — which Task 17 records as the surviving earmark. |
| **C3** | **R11 deletes `ReplanSweepWorkflow` "with its Schedule and façade op", and the controller orders that at item 9 — AFTER the ONE model edit at item 6. But the façade op IS a model edit:** `deliveryManager.ReplanProject` is one of the twelve ops in `.serviceContracts.deliveryManager.interface.operations`. Removing it takes the Manager 12 → 11 and regenerates `contract.gen.go`, `api/openapi.yaml:4958`, `webApp/src/contracts/schema.ts:119/2431`, `webApp/src/api/ops.gen.ts:88`, `systemtests/internal/sdk/http_delivery.gen.go:117-126`, `cmd/clientgen/mcpdocs.go:26` and `cmd/server/managerlog.go:97-104`. Doing it as a second edit pays the regen tax twice and risks a second `gen-*-check` drift. | **The op removal rides Task 7's single model edit; the Go deletion is Task 11 and runs immediately after.** Task 7 predicts the golden **unchanged at 134** (the workflow is still registered after the op goes — the registration is the hand-written `executionKindReplanSweep` entry at `deliverymanager.go:8750`); Task 11 predicts **133**. Between the two commits `deliveryManager.ReplanProject` is an exported method the generated interface no longer names, which compiles and is green — verified reasoning: `loggingDeliveryManager` is a hand-written decorator and an extra method on it is legal, and `useReplanProject` (`webApp/src/hooks/useDeliveryMutations.ts:580`) has **zero consumers outside its own file**. The two tasks are one review pass and neither may be merged without the other. |

---

## Global Constraints

Every task's requirements implicitly include this section. The first block is copied **verbatim** from the controller's standing rules.

- **`GOWORK=off` on EVERY `go`/`make` command under `server/`.** `ASDF_NODEJS_VERSION=lts` on EVERY npm/npx command.
- **`golangci-lint` runs from `server/`.** From the repo root it exits 7 on a go.work typecheck error **while printing a misleading "0 issues"**. Always `cd server && GOWORK=off make lint`.
- **Run `go test -short -count=1 ./...`, never `make test-short`.**
- **Never hand-edit `.aiarch/state/project.json` outside the one sanctioned model-edit task (Task 7), and slots 9/10 only via `make derived-plan-write`.**
- **No `//nolint`. No `default:` over a sum type.** (`gochecksumtype` is in the linter set; a `default:` arm that swallows a new member is how a vocabulary change goes silent.)
- **Copy `.claude/{skills,commands,agents}` MERGED (not nested) into any worktree** — three tests read them.
- **The uitests preview suite runs the MANAGED way and never with `UITESTS_PREVIEW_URL` set.**
- **`Test_LifecycleShapes` is timing-coupled to the observe ladder AND to a drain window, and it flakes under heavy parallel load.** Run it alone when it is the thing being judged; a failure under parallel load is re-run before it is believed.
- **NO DEPLOY until one drain covers stages 3 + 4a + 4b1 + 4b2.**

Plus:

- Work in the git worktree `.claude/worktrees/activity-stage4b2` (branch `activity-experience-stage4b2`, from `main` @`bae7681f`). The main checkout is shared with other sessions. `webApp/node_modules` is installed there.
- Gates run against PINNED platform tags (`framework-go v0.11.1`, `method-assets v0.9.0`) — **never** a `replace`. A field or lifecycle task this plan cannot do without is a **STOP** (Task 8 is the only one), never a workaround.
- **The self-amendment loop**, run after the `project.json` edit (Task 7 only):
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b2/server
  GOWORK=off make gen-models gen-fakes gen-client gen-internal-tools gen-temporal gen-sdk gen-config gen-main gen-lifecycles
  GOWORK=off make method-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot <every other slot this edit touched>
  GOWORK=off go test -short -count=1 ./...
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run gen:api && ASDF_NODEJS_VERSION=lts npm run gen:ops && ASDF_NODEJS_VERSION=lts npm run check
  cd ../systemtests && GOWORK=off go build ./...
  ```
  `validate --slot System` **DOWNGRADES other slots' Errors** — run `--slot <slot>` for every slot edited, or a slot-local Error ships green.
- **The drift block before EVERY commit that touches a generated input:**
  ```bash
  cd .../server
  GOWORK=off make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check \
      gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-lifecycles-check
  GOWORK=off make lint fix-check sumtype-check vet encapsulation-check derived-plan-check
  ```
  `make fix-check` is `go fix -diff ./...` and **exits 0 while printing a diff** — the output is the gate, not the exit code.
- **Line-count acceptance.** The floor 4b2 must defend is **19,573** hand-written non-generated non-test lines under `server/internal/manager/delivery/` — **not** the 18,983 the 4b1 earmark file states (the docs were written at `bdf90047`; main is four commits past it). Re-measured for this plan at `bae7681f`: total 46,857, `manager_test.go` 25,066, the four `*.gen.go` 2,218 (`contract` 1,053 + `activities` 530 + `invokers` 529 + `worker` 106), hand-written **19,573**. `fake/` is outside the glob at every baseline. Recipe in Task 17 Step 4.

---

## The gate ledger

**Measured today at `bae7681f`. Every task's gate block asserts against these and states what it expects them to become.** An unpredicted number is how a dropped registration hides.

| Gate | Command | Baseline | End of 4b2 |
|---|---|---|---|
| Registered-names golden | `go test ./internal/ -run TestRegisteredTemporalNamesGolden` | **134** | **127** (−1 `constructionReplanSweep` at Task 11, −6 `gitActivityStatusAccess.*` at Task 13) |
| Frozen workflow names | `…_FrozenWorkflowNames` | **15** | **15** (−`constructionReplanSweep`, +`constructionPumpSweep`, both at Task 11) |
| `Test_LifecycleShapes` | `go test ./internal/manager/delivery/ -run Test_LifecycleShapes` | **10/10** | **11/11** (+`continue-as-new-loses-no-signal`, Task 12) |
| Replay — child | `-run Test_Replay_DeliveryHistories` | **8/8** (201–537 events, 19 archived) | **8/8** |
| Replay — pump | `-run Test_Replay_PumpHistories` | *does not exist* | **5/5** (4 pump + 1 supervision, Task 14) |
| `validate --slot System` | `go run ./cmd/aiarch-state-mcp validate --root .. --slot System` | **43 advisory / 0 errors** | **43 advisory / 0 errors** — unmoved. Task 7 must not disturb `DH-CARD-ENGINES`(7), `DH-CARD-RA-RESOURCES`(18), `DH-CARD-VOLATILITY`(18), `DH-OBJ-COVERAGE`, `PA-RATECARD-KEYS`×8, `APPC-SVC-STRIVE`; `DH-CONTRACT-DEADOP` stays at ×2 (`AcknowledgeStaleBasis`, `RecordOperatorNote`) because Task 13 deletes neither publisher |
| `DH-CONTRACT-OPCOUNT-MAX` | pinned ABSENT, `internal/engine/designhealth/engine_test.go:67` | ABSENT | ABSENT — **no contract gains an op in 4b2** |
| webApp | `cd webApp && npm run check` | **1228** | **1235** (+4 Task 9, +3 Task 10; Tasks 5/7 are net zero — verified: **zero** node tests name `mapProjectSessionState`, `projectSessionStageFromOrdinal`, `ProjectSessionStage`, `mapSessionState` or revenue share except one fixture literal in `m0CostBasis.test.ts:29-30`) |
| uitests preview | `cd uitests && npx playwright test tests/preview/` | **50 cases** over **23 fixture states** (`activity-experience` 29, `preview-shell` 12, `plan` 7, `activity-renderers` 2) — the docs' 44 is **stale** | **52 / 23** (+2 in `activity-renderers.spec.ts`, Task 9; no new fixture state) |
| `systemtests` | `cd systemtests && GOWORK=off go build ./...` | builds | builds |
| Delivery hand-written lines | recipe above | **19,573** | expected **≈18,900–19,300** (Task 17 measures and reports; +pump lease state, −`child.Get`, −`replansweep.go` 47, −the twin pairs the fold retires, −`resolveQuestionBranch`) |

---

## Scope — what is OUT of 4b2, and where each goes

Say it here so nobody re-derives it mid-wave.

| Out | Where it goes, and why |
|---|---|
| **Engines 7→5** | **Its own wave, after the deploy.** The `method-check` Utility does not exist in slot 5, `methodcheck` is platform code, and the merge moves ZERO advisories (`DH-CARD-ENGINES` warns at 7 and at 6 alike). Only `operationEstimation → estimation` is reachable and it COLLIDES on `EstimateForOption` (two signatures), so one must be renamed. |
| **`ReadProjectAtRef` + the `revision` selector + minting the real substrate revision** | **4b3.** And note the routing: **`QueryActivityView` CANNOT take the selector additively** — positional params, path-routed `GET /api/v1/delivery/query-activity-view/{projectID}/{activityID}`, **9 surfaces**. `QueryProjectView(ProjectViewQuery)` already takes ONE object with five optionals (`Owner`, `ProjectID`, `ActivityID`, `ArtifactKind`, `EpisodeID`), so routing the revision read there costs **2**. |
| **The batched `QueryProjectView(plan)`** | **4b3.** Pure read, additive. `useConstructionSessions` (`useDeliveryQueries.ts:424-457`) fans out one query per in-flight activity, and `probeCandidatesFor` admits **zero** rows on this repo today — a latent amplification, not an active one. |
| **The PUSHED job-completion signal** | **4b3.** It is a **DIFFERENT signal** from the child-completion one — it is the AGENTIC JOB's completion, retiring the 23-poll observe ladder, and it needs an RA producer nothing has. **The spec's 4b2 row conflates the two.** Task 8 Step 6 of the 4b1 plan (the observe selector) is the seam. |
| **The per-TASK session view** (`ConstructionSessionView.tasks[]`) | **4b3.** Additive, and the defect it exists to fix is already fixed by Task 2. |
| **The sweep that re-opens a Completed activity with uncommitted slots** | **4b3.** Additive; reuses `deliveryRoundSweep`'s exact shape. |
| **`amend-N` / `AmendmentIndexFor`** | **Its own wave.** `AmendmentIndexFor` has readers beyond `DesignBranch`, and its **monotonic-commit-bump reasoning at `projectstateaccess.go:6593 / :6597 / :6731-6732 / :6795 / :6820` survives the branch scheme's retirement and must not be deleted with it** (Task 3 Step 4 pins that). Whether the activity rail needs an amendment index at all is the §10 amendment-UX follow-up. |
| **The M0 attempt `Detail` on `DeliveryTaskRevisionView`** | **CONDITIONAL — decided at Task 7, not at the end.** The controller's instruction is to pull it forward "if the wave comes in short", but with ONE model edit that decision cannot be deferred: taken late it is a second full regen. **This plan's default is INCLUDE, as Task 7 Step 7**, because it is ~40 lines, it is the only item on the whole list that changes what a human approves, and the silent case is LIVE on this very repo (`projectDesignComputedKinds()` excludes `KindPlanningAssumptions`, slot 8 IS committed with an Unknown revenue share, so `resolvePlanningAssumptions` fills a family and the M0 notice returns `''` — a founder can approve a spend computed on numbers nobody showed them). If the controller rules it out, delete Step 7 and Task 9 Step 5 together, before Task 7 starts. |
| **The three deprecated facets other than `gitActivityStatusAccess`** | **After the `projectCatalogAccess` split.** R6. |

---

## Task order

**The census first, because the pump is this wave's deleted body:**
Task 1 (the guard census, executable).

**Then everything cheap, local and independently revertable, in the controller's order:**
Task 2 (fork-steer precheck) → Task 3 (`resolveQuestionBranch` + `DesignBranch`) → Task 4 (`pumpsweep.go:88`) → Task 5 (the session-view fold) → Task 6 (`reconcileBranchFromMain(kinds)`) → **Task 7 (THE ONE MODEL EDIT + THE ONE REGEN)** → Task 9 (the provider/context sweep) → Task 10 (human-answered construction questions) → Task 11 (`ReplanSweep` dies).

**Then the pump, and only then:**
Task 12 (the react-by-signal pump) → Task 13 (`gitActivityStatusAccess`) → Task 14 (the pump's replay fixtures).

**Then the record and the platform STOP:**
Task 15 (the eight Phase-2 slugs + eight command files + the `<branch>@vN` doc correction) → Task 16 (the whole-branch lost-guard review against Task 1's census) → Task 17 (drain note, earmarks, spec amendments, the measurement) → **Task 8 (the FOUNDER STOP: `operationalConcepts` gets a lifecycle home)**, last and droppable.

**Must ship together:**
- **Task 5 is ONE commit**: the `$defs` deletion, the regenerated files, the merged producers, the four retired twin pairs and the SPA re-point. A half-folded session view leaves `ProjectView` carrying a member no producer fills, which reads as coverage.
- **Task 7 is ONE commit** and it runs the self-amendment loop exactly once. It is the ONLY task that edits `project.json`.
- **Task 7 and Task 11 are one review pass** (C3) and neither merges without the other.
- **Task 12's steps run in the printed order.** The lease and the CAN drain are one change: a lease with no drain-and-carry is the silent failure R-C exists to prevent.
- **Task 14's fixture directory and its case list land in ONE commit** — `Test_Replay_DeliveryHistories`'s directory-level orphan guard fails both on a directory no case names and on zero directories.
- **Task 9's steps may not be split across a green `npm run check`**: mounting a provider without its node test leaves an assertion that a lens is disabled while it is now enabled.

**Parallelism.** Tasks 2, 3, 4 and 6 are genuinely disjoint (one façade precheck / two dead symbols / one sweep line / one RA verb) and may run in parallel — **except that none of them may hold `project.json`**, and none does. Task 5 must land before Task 7 (the fold deletes `$defs` entries Task 7 would otherwise regenerate twice). **Task 1 must land before Task 12.** Everything from Task 7 onward is strictly sequential.

---

## Execution risks and how this plan removes each

1. **The pump's `ContinueAsNew` drain-and-carry fails SILENTLY and only under the concurrency 4b2 creates.** Every other item in this wave fails loudly — a wrong fold is a typecheck error, a wrong precheck is a refused button, a wrong reconcile is a logged refusal. A lease-release or finish signal buffered on a run that continues-as-new is discarded by the SDK; the next run has no record of it; the project's one pump then waits out a lease deadline for an activity that finished, or never re-selects an activity whose finish was the thing lost. Three aggravating facts, all measured: (a) `pumpnextactivity.go` **already documents this exact trap** at pause check 3, so this file has been stepped on once; (b) `pump-singular-per-project` means there is no second pump to cover a wedged one — **the blast radius is: the project stops**; (c) the failure needs concurrency to appear, which is precisely the condition 4b2 creates and which no pre-4b2 test exercises. Removed by R-C's four mitigations, all in Task 12 Steps 6–8, and by the `continue-as-new-loses-no-signal` shape case in Step 11 — **mutation-checked by removing the carry and watching it go red**, the discipline `Test_LifecycleShapes` was written with.
2. **The pump's only regression net is four ARCHIVED fixtures, and 4b2 rewrites the pump.** `constructionPumpNextActivity` and `constructionProjectSupervision` are in the golden (`registered_names_test.go:114-115`) and on the frozen list, and the four histories that replay them are all in `replay-archive/`. Task 14 builds the rig. **It runs AFTER Task 12 and that is deliberate**: a capture that registers only the pump parks forever on `child.Get`, so the only two honest orders are "capture the OLD pump first, then rewrite" or "capture the NEW pump after". The first buys a fixture that is worthless the moment Task 12 lands (it replays a command sequence that no longer exists); the second buys a fixture that pins the shape going forward. **Cost, stated: nothing pins the transition itself** — the drain is what makes that acceptable, and Task 16 is what checks the transition by reading.
3. **Eight guards were lost in 4b1 inside deleted bodies and every one was invisible to the test suite.** Task 1 enumerates every guard-shaped line in the two files this wave deletes or rewrites, records what each protects and pins each with an assertion, **before** Task 12 opens the file. Task 16 then checks a LIST rather than re-deriving one.
4. **`make gen-sdk` deletes SDK files a separate Go module's hand-written harness calls.** `pruneStaleSDK` (`cmd/appgen/main.go:342`) removes every `*.gen.go` under `../systemtests/internal/sdk` not in the fresh output set, and `systemtests.yml` triggers on `server/**` and `.aiarch/**` — both touched by Task 7. Task 7 REMOVES an op (`ReplanProject`), so `systemtests/internal/sdk/http_delivery.gen.go:117-126` and its MCP/type twins **will** be pruned, and the harness that calls `DeliveryReplanProject` must be re-pointed in the same commit. Every task whose gate block runs a generator ends with `cd ../systemtests && GOWORK=off go build ./...`.
5. **`awaitDispatchDecision` is 130 lines and four budgets built to extract a boolean from a workflow that BLOCKS, and a non-blocking pump makes most of it dead weight.** The temptation is to delete the poll. **Do not:** the workflow start is still asynchronous, so the decision still lands *after* `ExecuteWorkflow` returns, just within one workflow task instead of one dispatch. Task 12 Step 9 shrinks the budgets and states each new value's reason; it does not remove the poll and it does not add `stillDeciding` (R4).
6. **A retired vocabulary member that is renumbered re-keys every committed project.** C1 is the instance this wave creates, and the ordinal enum is not the only one: `SessionStage` (0–7) and `ProjectSessionStage` (0–8) diverge at `AssemblingSDP`, which is why spec §4 says "never prefix-strip a session view". Task 5 deletes `ProjectSessionStage` **entirely** rather than merging ordinals, because the surviving derived type is a THREE-member vocabulary (R-D) and neither old enum is a prefix of it. A merge of the two 8/9-member enums would be an ordinal renumber; a replacement is not.
7. **Two model-truth changes in Task 7 touch money.** Removing `BillingResult.RevenueShareApplied` and `BillingTerms.revenueShare` reaches the billing engine, the billing manager's `closecycle.go:462` and `billingStateAccess.$defs.BillingTerms`. The rule Task 7 Step 3 states and enforces: **the settled-money LEDGER shape changes only where the removed concept is the whole field**; a field that carries a total which happened to include a revenue share is renamed or left alone, never silently re-meaned. `RevenueShareApplied` is the whole field and goes; `ComputeCost`, `ProjectedMonthlyCost` and `ExpectedPerCycleNet` stay untouched.
8. **`Test_LifecycleShapes` is timing-coupled and Task 12 adds a case to it under a plan that runs subagents in parallel.** Task 12 Step 11 runs it ALONE (`-run Test_LifecycleShapes -count=1`) and says so; a failure observed while another task's `go test ./...` is running is re-run before it is believed.

---

### Task 1: The guard census — every guard-shaped line in the two files this wave rewrites, pinned before it is touched

**This is the task 4b1's plan did not have, and eight lost guards are why it exists.** The 4b1 earmark file names the class at the top of its defect section: *"a precondition that lived in a deleted body and was not re-asserted in the body that replaced it"* — the open-review-comment guard (a live `vibes` autogate regression), `runTask`'s pre-`recordTaskAttempt` return, `dispatchConstructionOnce`'s pre-`resolveWorkAttempt` return, `eligibleUnder`'s `==`-where-cumulative, and four more found by the final fix wave. **Every one was invisible to the test suite.** Task 12 deletes and rewrites `PumpNextActivityWorkflow`'s body; Task 11 deletes `ReplanSweepWorkflow` outright. So the guards come out of those bodies FIRST, as a written list and as executable assertions, and Task 16 checks the list rather than re-deriving it.

The census is not a doc-only task. A guard recorded in prose is a guard nobody re-runs.

**Files:**
- Create: `docs/bugs/2026-09-28-pump-guard-census.md` — the written census (one row per guard: site, what it protects, what breaks without it, the assertion that pins it).
- Create: `server/internal/manager/delivery/pumpguards_test.go` — the executable half.
- Read-only: `server/internal/manager/delivery/pumpnextactivity.go` (424 lines), `server/internal/manager/delivery/replansweep.go` (47 lines), `server/internal/manager/delivery/pumpsweep.go` (121 lines).

**Interfaces produced (Tasks 11, 12, 14 and 16 all consume this):**
- `docs/bugs/2026-09-28-pump-guard-census.md` — the canonical list. Task 16 walks it row by row.
- `func pumpGuardCensus() []pumpGuard` in `pumpguards_test.go`, where `type pumpGuard struct { ID, Site, Protects, BreaksAs, PinnedBy string }` — the machine-readable twin, so a guard with no `PinnedBy` is a test failure rather than a note.
- `Test_PumpGuardCensus_EveryGuardIsPinned` — fails if any census row's `PinnedBy` names a test that does not exist in the package.

- [ ] **Step 1: Enumerate. Read both files end to end and write the census.**

  Run the mechanical sweep first so nothing is missed by eye:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b2/server
  grep -n 'if \|switch \|case \|return .*err\|GetVersion\|ReceiveAsync\|Fatalf\|NonRetryable' \
    internal/manager/delivery/pumpnextactivity.go internal/manager/delivery/replansweep.go
  ```
  Expected: ~70 lines out of 471. The census keeps only the **guard-shaped** ones — a line that REFUSES, GATES, ORDERS or VERSIONS something, as opposed to a line that merely computes. The eleven the reader will find, and which the census must contain at minimum (each verified for this plan at `bae7681f`):

  | ID | Site | What it protects |
  |---|---|---|
  | **G-P1** | `pumpnextactivity.go:62-68` — `SetQueryHandler(queryPumpDispatch)` registered BEFORE any blocking call | The façade's synchronous `ExecuteNextActivity` reads this Query. Registered late, `awaitDispatchDecision` polls a run that cannot serve it and falls through to `terminalPumpResult` — a slow dispatch reported as a closed pump. |
  | **G-P2** | `:92-98` — `pumpPausedAtRunStart`, quiet return, **NO ContinueAsNew** | An operator pause at run start stops the cascade. The *no ContinueAsNew* half is the guard: a paused pump that continues-as-new re-enters and dispatches on the next run. |
  | **G-P3** | `:101-107` — `isReadNotFound(err)` ⇒ quiet `PumpResult{}`, not an error | A project with no state yet is a normal quiet tick. Returned as an error it fails the Schedule's child start and logs a platform-wide sweep error every 30 s. |
  | **G-P4** | `:116-121` — `pumpHonorsRecordedPause`, **before `nextEligible`** | The supervision pause branch RECORDS before it relays, so a pump the sweep restarts inside the relay window sees the recorded pause. Placed AFTER `nextEligible` it would still dispatch. |
  | **G-P5** | `:133-168` — `verdictBlocked` writes `ConstructionTransitionRecordActivityFailed` through `applyRecovering` and returns quiet | **LOUD, DURABLE, APP-VISIBLE.** A warning in a serve log is the failure mode being eliminated — "a warning buried in a serve log is how this defect consumed an entire benchmark run undetected". The write also takes the activity out of NotStarted so the next tick considers the rest of the network. |
  | **G-P6** | `:169-175` — `verdictQuiescent` returns WITHOUT ContinueAsNew | The cascade's own drain-to-quiet is what ENDS the pump. Continue-as-new here is an infinite pump. |
  | **G-P7** | `:187-192` — `pumpPausedBehindGate("pump-pause-before-dispatch")`, between `readProject` and the child start | `readProject` is an Activity, so a pause can land after G-P2 and before the dispatch. Its stated BOUND: a pause arriving DURING the dispatching workflow task is honoured at G-P9 instead, after exactly one activity. |
  | **G-P8** | `:219-220` — `dispatch = {Decided, Dispatched, ActivityID}` recorded **BEFORE** the block | The façade returns this tick's decision without waiting for the cascade. Recorded after the block, every `Begin` blocks for the whole drain. |
  | **G-P9** | `:252-256` — `pumpPausedBehindGate("pump-drain-pause-before-continue-as-new")`, and the comment that explains it | **"A signal still buffered on a run that ends in ContinueAsNew is NOT carried into the next run."** This is the file's own record of the trap R-C exists to prevent, and Task 12 must generalise it from *pause* to *every channel*. |
  | **G-P10** | `:259` — ContinueAsNew carries ONLY `pumpInput` | Unbounded history is avoided and determinism is trivial. Task 12 grows the payload and therefore **owes a bound**: the started set is bounded by the activity count. |
  | **G-P11** | `pumpPauseRequested` `:337-345` — an UNDECODABLE pause still counts as a pause | Decided and pinned by `Test_Pump_UndecodablePauseSignal_StillPauses`. Dropping a pause over a malformed body fails OPEN — the cascade keeps dispatching through an operator halt. Counting it fails SAFE. |

  Plus, from `replansweep.go` (deleted in Task 11, so its guards must be shown to protect nothing):

  | ID | Site | What it protects |
  |---|---|---|
  | **G-R1** | `replansweep.go:25-27` — `in.ProjectID == nil` ⇒ empty result | The all-projects fan-out is unreachable over both transports. **Protects nothing that survives**: Task 11 shows the arm has no reachable caller. |
  | **G-R2** | `:31-36` — `isReadNotFound` ⇒ empty, not an error | Same shape as G-P3, and it dies with the workflow. |

  And the one from `pumpsweep.go` Task 4 changes:

  | ID | Site | What it protects |
  |---|---|---|
  | **G-S1** | `pumpsweep.go:94-96` — `s.OperatorPaused` ⇒ skip | The sweep must not silently override an operator pause every 30 s. **Task 4 changes the line ABOVE it and must not touch this one.** |

  Write each row into `docs/bugs/2026-09-28-pump-guard-census.md` with a fourth column, **BreaksAs** — the observable symptom if the guard is not re-asserted — and a fifth, **PinnedBy**, naming the Go test.

- [ ] **Step 2: Write the failing meta-test.**

  ```go
  // Test_PumpGuardCensus_EveryGuardIsPinned is the 4b1 lesson made executable. Stage 4b1
  // lost EIGHT preconditions inside bodies it deleted, and every one was invisible to the
  // suite: the reviewer found them by reading the retired rail's guards against the new one.
  // 4b2 rewrites the pump, so the guards come out of the body BEFORE the body moves, and
  // this test refuses a census row that names no test.
  //
  // It does NOT assert the guards still hold — the named tests do that. It asserts that the
  // LIST and the SUITE agree, which is the property a census has and a comment does not.
  func Test_PumpGuardCensus_EveryGuardIsPinned(t *testing.T) {
  	names := testFuncNamesInPackage(t) // parses *_test.go with go/ast; see Step 3
  	for _, g := range pumpGuardCensus() {
  		if g.PinnedBy == "" {
  			t.Errorf("guard %s (%s) names no test — a guard nobody re-runs is a comment", g.ID, g.Site)
  			continue
  		}
  		if !names[g.PinnedBy] {
  			t.Errorf("guard %s names %s, which does not exist in this package", g.ID, g.PinnedBy)
  		}
  	}
  }
  ```
  - [ ] Run it: `GOWORK=off go test ./internal/manager/delivery/ -run Test_PumpGuardCensus -count=1 -v`. Expected: **RED**, listing every census row whose `PinnedBy` is empty or names nothing.

- [ ] **Step 3: Write `testFuncNamesInPackage` and pin the unpinned guards.**

  ```go
  // testFuncNamesInPackage parses every *_test.go in this directory and returns the set of
  // top-level Test* func names. Precedent for the technique: paramguard_arch_test.go keys
  // bodies by receiver + name, and internal/arch_test.go's TestBuildStatusVocabulariesAgree
  // parses two switches.
  func testFuncNamesInPackage(t *testing.T) map[string]bool {
  	t.Helper()
  	fset := token.NewFileSet()
  	out := map[string]bool{}
  	entries, err := filepath.Glob("*_test.go")
  	if err != nil { t.Fatal(err) }
  	for _, e := range entries {
  		f, perr := parser.ParseFile(fset, e, nil, 0)
  		if perr != nil { t.Fatalf("parse %s: %v", e, perr) }
  		for _, d := range f.Decls {
  			fn, ok := d.(*ast.FuncDecl)
  			if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
  				out[fn.Name.Name] = true
  			}
  		}
  	}
  	return out
  }
  ```
  - [ ] **Verify first:** `grep -n 'func Test_Pump' internal/manager/delivery/manager_test.go | wc -l` — expected **32** `Test_Pump*` / `Test_PumpSweep_*` blocks, restored in 4b1 Task 13. Most census rows already have a pin among them; map each row to the one that actually drives it, not to one that merely mentions it.
  - [ ] For any row with no existing pin, write the test **in this task**. The two most likely gaps, both checked for this plan: **G-P8** (the decision is recorded before the block) has no direct test — the façade tests observe the Query's answer but nothing asserts the ORDER; and **G-P10**'s bound has none, because there is nothing to bound yet. Write:
    ```go
    // Test_Pump_DispatchDecisionIsReadableBeforeTheCascadeDrains pins G-P8: the façade's
    // synchronous answer does not wait for the activity. Driven through the test suite's
    // Query handler against a child that never terminates, so a regression that moves the
    // assignment after the block HANGS this test rather than passing it slowly.
    func Test_Pump_DispatchDecisionIsReadableBeforeTheCascadeDrains(t *testing.T) { … }

    // Test_Pump_ContinueAsNewPayloadIsBoundedByThePlan pins G-P10. Today the payload is
    // pumpInput and the bound is trivial; 4b2 Task 12 grows it, and this is where the bound
    // is stated once rather than argued twice.
    func Test_Pump_ContinueAsNewPayloadIsBoundedByThePlan(t *testing.T) { … }
    ```
  - [ ] Re-run Step 2's test. Expected: **GREEN**, 14 rows all pinned.

- [ ] **Step 4: Gates and commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b2/server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_PumpGuard|Test_Pump' -count=1 2>&1 | tail -20
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint
  ```
  Expected: all green. **Golden 134 unchanged, frozen 15 unchanged, `Test_LifecycleShapes` 10/10, replay 8/8, `validate` 43/0, `npm run check` 1228, preview 50/23** — this task adds one test file and one doc and touches no production line, so **every gate number must be identical**. A moved number here means the worktree is not clean.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b2
  git add docs/bugs/2026-09-28-pump-guard-census.md server/internal/manager/delivery/pumpguards_test.go
  git commit -F - <<'MSG'
  test(pump): the guards come out of the body before the body moves

  Stage 4b1 lost EIGHT preconditions inside bodies it deleted, and every one of
  them was invisible to the test suite — they were found by a reviewer reading the
  retired rail's guards against the new one, one of them a live `vibes` autogate
  regression that had been shipping for two waves.

  Stage 4b2 rewrites the pump. So its guards are enumerated FIRST: fourteen rows
  across pumpnextactivity.go, replansweep.go and pumpsweep.go, each naming what it
  protects, how it breaks without it, and the test that re-runs it. Two of the
  fourteen had no pin at all — the order of the dispatch assignment against the
  blocking Get, and the bound on the ContinueAsNew payload — and both now do.

  The meta-test is the point: a census row whose PinnedBy names no test is a
  failure, not a note. A guard nobody re-runs is a comment.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 2: `OverrideActivity` prechecks the LEDGER, not the single-valued stage

**The open 4b1 defect, moved here from the 4b2 nicety list by the final fix wave.** On a `service`/`frontend` fork with `stp` escalated and `designReview` at an approval gate, `constructState.stage` reports whichever gate was entered LAST, so `OverrideActivity` refuses a steer the client correctly offers — and the client's `overrideActionFor` is right to offer it. Measured, the fix is a re-order: the SUCCESS path fifteen lines below the precheck already asks the authoritative question.

```go
// deliverymanager.go:6355-6360 — the precheck, TODAY
view, err := m.activitySession(ctx, projectID, activityID)
switch {
case err == nil:
    if view.Stage != StageAwaitingTakeover {
        return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
            "activity %s is at %s, not awaiting a takeover — an override steers an escalation; …",
            activityID, sessionStageName(view.Stage)))
    }
case isManagerNotFound(err):
    return m.reopenActivity(ctx, projectID, activityID, override)   // the REOPEN arm — unchanged
default:
    return err
}
…
task, err := m.escalatedTask(ctx, projectID, activityID)   // :6376 — the LEDGER, and it is correct on a fork
```

`m.escalatedTask` (`:6474`) reads the row and calls `escalatedTaskOf` (`:9271`), which scans attempts newest-first for a task whose `latestTaskOutcome` is `OutcomeFailed`. **That is per-TASK and a fork cannot clobber it** — 4b1 proved each review coroutine owns its own `*gateLedger`.

**Files:**
- Modify: `server/internal/manager/delivery/deliverymanager.go` — `constructionManager.OverrideActivity` (`:6331-6390`), the precheck at `:6355-6363`.
- Modify: `server/internal/manager/delivery/manager_test.go` — the fork case and the refusal case.

**Interfaces:** none new. No wire change, no `project.json` edit, no signature change. Consumed by nothing; consumes `escalatedTaskOf` (already there).

- [ ] **Step 1: Write the failing test — a fork with two live gates, one escalated.**
  ```go
  // Test_OverrideActivity_SteersAnEscalatedForkBranchWhileASiblingHoldsAGate is the OPEN
  // 4b1 defect. A `service` fork holds `stp` (escalated: its latest attempt FAILED) and
  // `designReview` (at an approval gate). constructState.stage is single-valued and reports
  // whichever was ENTERED LAST, so the precheck refuses a steer the ledger can name.
  //
  // It is driven at the FAÇADE with a seeded row rather than through a live child,
  // deliberately: the defect is in the precheck's choice of source, and driving it through
  // a walk would make the test depend on which gate the scheduler enters second — the very
  // nondeterminism the fix removes.
  func Test_OverrideActivity_SteersAnEscalatedForkBranchWhileASiblingHoldsAGate(t *testing.T) { … }
  ```
  Seed: an `ActivityExecution` row whose `Attempts` end with a FAILED attempt on `stp` and a later resolved one on `designReview`; a session double answering `ConstructionSessionView{Stage: StageAwaitingGate}`. Assert the override is **accepted** and that the signal carries `TaskID: "stp"`.
  - [ ] **Verify first:** read `escalatedTaskOf` and `latestTaskOutcome` (`grep -n 'func latestTaskOutcome' -A 20 internal/manager/delivery/deliverymanager.go`) so the seeded row actually produces `("stp", true)`. A seed that does not is a test that passes for the wrong reason.
  - [ ] Run: `GOWORK=off go test ./internal/manager/delivery/ -run Test_OverrideActivity_Steers -count=1 -v`. Expected: **FAIL**, with the message *"activity … is at awaitingGate, not awaiting a takeover"*.

- [ ] **Step 2: Write the second test — the refusal must survive.**
  ```go
  // Test_OverrideActivity_RefusesWhenTheLedgerNamesNoEscalatedTask pins the other half:
  // the re-order must not turn the precheck into an accept-everything. An activity with a
  // LIVE child and no failed attempt on any task is not escalated, and the refusal sentence
  // is the operator's only explanation.
  func Test_OverrideActivity_RefusesWhenTheLedgerNamesNoEscalatedTask(t *testing.T) { … }
  ```
  Expected before Step 3: **GREEN** (today's precheck refuses it for the stage reason). After Step 3 it must still be green, for the ledger reason — assert on `fwmanager.FailedPrecondition` and on the substring `no task on its ledger holds a failed attempt`, which is `escalatedTask`'s own existing sentence.

- [ ] **Step 3: Re-order.** Replace the stage precheck with the ledger question, keeping the liveness read for exactly the job it still has:
  ```go
  	// THE PRECHECK ASKS THE LEDGER, NOT THE VIEW (stage 4b2 Task 2; the open 4b1 defect).
  	// constructState.stage is SINGLE-VALUED and a fork holds two gates, so the view reports
  	// whichever was entered last and this precheck refused a steer the client correctly
  	// offered. The authoritative answer was already being read fifteen lines below, on the
  	// success path: escalatedTaskOf scans the row's attempts per TASK, and 4b1 proved the
  	// ledger correct on a fork (each review coroutine owns its own *gateLedger).
  	//
  	// The session read STAYS, for the one thing it still answers that the row cannot: is
  	// there a LIVE CHILD. NotFound means the activity is over, and the reopen arm below is
  	// what an operator looking at a finished activity is asking for.
  	if _, err := m.activitySession(ctx, projectID, activityID); err != nil {
  		if isManagerNotFound(err) {
  			return m.reopenActivity(ctx, projectID, activityID, override)
  		}
  		return err
  	}
  	// escalatedTask refuses FailedPrecondition when the ledger names no escalated task,
  	// with the sentence the operator reads. It is the ONE question, asked ONCE.
  	task, err := m.escalatedTask(ctx, projectID, activityID)
  	if err != nil {
  		return err
  	}
  ```
  and delete the now-duplicate `m.escalatedTask` call at the old `:6376`.
  - [ ] **Verify first:** `grep -n 'sessionStageName\|StageAwaitingTakeover' internal/manager/delivery/*.go | grep -v _test` — if `sessionStageName` or `StageAwaitingTakeover` loses its last non-test caller, leave the symbol and say why in a one-line comment (the SPA's `overrideActionFor` still reads the wire stage; the server-side helper is used by other refusals). Do **not** delete a symbol this task did not measure to be dead.
  - [ ] Run both tests: expected **both GREEN**.

- [ ] **Step 4: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_OverrideActivity|Test_Facade_.*Override|Test_Reopen' -count=1 -v 2>&1 | tail -30
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint vet sumtype-check
  ```
  Expected: green. **Every gate number unchanged — golden 134, frozen 15, shapes 10/10, replay 8/8, validate 43/0, npm 1228, preview 50/23.** This task changes one façade body and adds two tests; the replays are the load-bearing assertion that the *workflow* command sequence did not move (the override is a Manager-side signal send, not a workflow change).
  ```bash
  git add server/internal/manager/delivery/deliverymanager.go server/internal/manager/delivery/manager_test.go
  git commit -F - <<'MSG'
  fix(delivery): the steer precheck asks the ledger the success path already asks

  OverrideActivity refused a steer the client correctly offered. On a service or
  frontend fork with `stp` escalated and `designReview` at an approval gate,
  constructState.stage is single-valued and reports whichever gate was ENTERED
  LAST — so the precheck read `awaitingGate`, refused, and the operator could not
  steer a genuinely escalated branch.

  The authoritative answer was already in the same function, fifteen lines below:
  escalatedTask -> escalatedTaskOf reads the ROW, per task, and stage 4b1 proved
  the ledger correct on a fork because each review coroutine owns its own
  gateLedger. So the fix is a re-order and a deletion, not a new field: ask the
  ledger first, keep the session read only for the question it alone answers — is
  there a live child — and let the NotFound arm keep meaning reopen.

  No wire change. The per-task session view is still the right answer for the
  QUERY, and it is 4b3's; this defect did not need it.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 3: `resolveQuestionBranch` and `DesignBranch` are deleted — a question's thread outlives the branch

**Dead BY TYPE, not by measurement.** `resolveQuestionBranch` (`deliverymanager.go:868-878`) guards on `isLiveSessionStage(view.Stage)` where `view` comes from `designCompletedSessionView`:

```go
func isLiveSessionStage(stage SessionStage) bool {            // :895
	case StageDrafting, StageAwaitingReview, StageRedrafting, StageRefused:  return true
	case SessionStageUnknown, StageCommitted, StageWithdrawn, StageDraftFailed: return false
}
```
and `committedSessionView` (`:237-270`) can emit **only** `StageCommitted`, `StageWithdrawn` or `StageDraftFailed`. **The sets are disjoint. The guard is false for every possible input**, so the function returns `""` unconditionally and `readProjectMaybeBranch` always reads main.

**The ratification, not just the deletion (R-E).** Main is not a fallback the code drifted into — it is the only branch reachable since 4b1. And it is the RIGHT answer: the activity branch `activity/{activityId}` is **squashed away at merge**, while the slot's thread on main is what the SPA reads, what the answer job answers, and what a reader finds six months later asking why a volatility was named the way it was. The one thing lost is proximity-while-drafting, and the mitigation already exists: the comment names its round, so the join survives even though the branch does not.

**Files:**
- Modify: `server/internal/manager/delivery/deliverymanager.go` — delete `resolveQuestionBranch` (`:854-878`) and `isLiveSessionStage` (`:892-901`); simplify the two callers at `:792` (`askDesignQuestions`) and `:4484` (`askPlanQuestions`); `readProjectMaybeBranch` (`:881-889`) collapses onto `ReadProject`.
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — delete `DesignBranch` (`:3644-3665`); **KEEP `AmendmentIndexFor` (`:3634`)**.
- Modify: `server/internal/manager/delivery/deliveryactivity.go:563` — the comment that says "`projectstate.DesignBranch` survives unused for one release".
- Modify: `server/internal/resourceaccess/projectstate/access_test.go` — the `DesignBranch` unit tests.

**Interfaces produced:** nothing new. `readProjectMaybeBranch` is DELETED and its two callers call `m.projectState.ReadProject` directly — Task 5 depends on that, because the fold retires both callers' twins.

- [ ] **Step 1: Prove the guard is unsatisfiable, in a test, before deleting anything.**
  ```go
  // Test_QuestionBranch_TheLiveStageAndTheDerivedStagesAreDisjoint is the deletion's
  // ARGUMENT, executable. resolveQuestionBranch asked isLiveSessionStage of a view only
  // committedSessionView produces, and the two vocabularies do not intersect — so the
  // branch arm was unreachable for every possible input, not merely unused in practice.
  //
  // It survives the deletion as the pin on the RATIFIED answer: design questions are
  // seeded on MAIN, beside the slot, because a question's thread outlives the activity
  // branch that is squashed at merge.
  func Test_QuestionBranch_TheLiveStageAndTheDerivedStagesAreDisjoint(t *testing.T) {
  	live := []SessionStage{StageDrafting, StageAwaitingReview, StageRedrafting, StageRefused}
  	derived := []SessionStage{StageCommitted, StageWithdrawn, StageDraftFailed}
  	for _, d := range derived {
  		for _, l := range live {
  			if d == l {
  				t.Fatalf("stage %v is in both sets — resolveQuestionBranch's branch arm was reachable after all", d)
  			}
  		}
  	}
  	// And the producer's OUTPUT set is exactly `derived`, driven rather than asserted:
  	for _, st := range []projectstate.ReviewStatus{
  		projectstate.ReviewCommitted, projectstate.ReviewWithdrawn,
  		projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected,
  	} { … assert committedSessionView(...).Stage ∈ derived … }
  }
  ```
  - [ ] Run: expected **GREEN** immediately — it is a statement about two vocabularies, and it is green before and after the deletion. That is the point: it is the argument, kept.

- [ ] **Step 2: Assert the seeding target, so the behaviour is pinned and not merely inherited.**
  ```go
  // Test_AskDesignQuestions_SeedsOnMain pins the RATIFIED answer (4b1 Q6, closed in 4b2).
  // Before this task the target was main by accident — through a guard that could not be
  // true. After it, main is the only thing the code can express, and this test is what
  // says so out loud.
  func Test_AskDesignQuestions_SeedsOnMain(t *testing.T) { … }
  ```
  Drive `askDesignQuestions` against a double recording the `branch` argument every `SeedReviewCommentsOnBranch` call carries, and assert `""` for both a committed slot and a withdrawn one. Repeat for `askPlanQuestions`.
  - [ ] **Verify first:** `grep -n 'SeedReviewCommentsOnBranch' internal/manager/delivery/deliverymanager.go` — expected **two** call sites, `:820` and `:4503`. Both are re-pointed in Step 3.

- [ ] **Step 3: Delete the resolver, the guard and the branch-taking read.**
  - In `askDesignQuestions` and `askPlanQuestions`, replace `branch := m.resolveQuestionBranch(rc, projectID, kind)` + `m.readProjectMaybeBranch(ctx, psID, branch)` with `m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)`, and pass `""` to `SeedReviewCommentsOnBranch` with the reason at the site:
    ```go
    	// MAIN, BESIDE THE SLOT (ratified 4b2; 4b1 Q6). A question's thread OUTLIVES the draft
    	// it is about: activity/{activityId} is squashed away at merge, while the slot's thread
    	// on main is what the SPA reads, what the answer job answers, and what a reader finds
    	// six months later. The resolver that used to ask for a session branch was dead BY TYPE
    	// — isLiveSessionStage's true-set and committedSessionView's output-set are disjoint —
    	// so main was already the only reachable answer; this is the answer stated rather than
    	// arrived at. The join to the draft survives without the branch: the comment names its
    	// round.
    ```
  - Delete `resolveQuestionBranch`, `isLiveSessionStage` and `readProjectMaybeBranch`.
  - [ ] **Verify first:** `grep -rn 'isLiveSessionStage\|readProjectMaybeBranch\|resolveQuestionBranch' server/ --include='*.go'` must return **zero** outside the diff. `isLiveSessionStage` in particular is a tempting keep — it has no other caller (measured at `bae7681f`: one).

- [ ] **Step 4: Delete `DesignBranch` — and KEEP the reasoning `AmendmentIndexFor` carries.**
  - Delete `projectstate.DesignBranch` (`projectstateaccess.go:3644-3665`) and its unit tests.
  - **`AmendmentIndexFor` (`:3634`) STAYS.** Its five doc-comment references inside the commit-transition logic (`:6593`, `:6597`, `:6731-6732`, `:6795`, `:6820`) explain why the commit bump is **monotonic**, and that reasoning survives the branch scheme's retirement. Add one line at `AmendmentIndexFor` recording that its last branch-naming caller is gone and what it is still for:
    ```go
    // The DESIGN BRANCH SCHEME that this index used to name is retired (stage 4b2): there is
    // one activity branch per activity now, and `-amend-N` goes with the amendment-UX
    // follow-up. The INDEX stays, because five commit-transition sites below reason from it
    // about why the commit bump is monotonic, and that argument is about commits, not branches.
    ```
  - Update `deliveryactivity.go:563`'s comment (it currently says `DesignBranch` "survives unused for one release" — that release is this one).
  - [ ] **Verify first:** `grep -rn 'DesignBranch\|AmendmentIndexFor' server/ --include='*.go'` — after the edit, `DesignBranch` must be **zero** in code and comments; `AmendmentIndexFor` must still have its declaration plus its five doc references plus any live caller.
  - [ ] Check the remote branch is untouched: `git ls-remote --heads origin 'aiarch-design/*'` — expected exactly one ref, `refs/heads/aiarch-design/archistrator/0-amend-1`. **Deleting the Go function does not delete the branch, and this task must not delete the branch.** Record it in Task 17's earmarks instead.

- [ ] **Step 5: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_QuestionBranch|Test_AskDesignQuestions|Test_AskPlanQuestions|Test_Ask' -count=1 -v 2>&1 | tail -30
  GOWORK=off go test ./internal/resourceaccess/projectstate/ -count=1
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint vet encapsulation-check
  ```
  Expected: green. **Golden 134, frozen 15, shapes 10/10, replay 8/8, validate 43/0, npm 1228, preview 50/23 — all unchanged.** `DesignBranch` is not a Temporal name and `resolveQuestionBranch` is a Manager-side helper; neither appears in any generated surface (verified: zero hits in `*.gen.go`). Hand-written delivery lines fall by ~55.
  ```bash
  git add server/internal/manager/delivery server/internal/resourceaccess/projectstate
  git commit -F - <<'MSG'
  refactor(delivery): design questions are filed on main, and the resolver that
  pretended otherwise is deleted

  resolveQuestionBranch was dead BY TYPE, not by measurement. It asked
  isLiveSessionStage — true only for {Drafting, AwaitingReview, Redrafting,
  Refused} — of a view that committedSessionView produces, and that producer can
  emit only {Committed, Withdrawn, DraftFailed}. The two sets are disjoint, so the
  branch arm was false for every possible input and main was already the only
  reachable answer.

  Main is also the RIGHT answer, which is why this is a ratification and not a
  tidy-up: a question's thread outlives the draft it is about. activity/{id} is
  squashed away at merge; the slot's thread on main is what the SPA reads, what the
  answer job answers, and what a reader finds six months from now asking why a
  volatility was named the way it was. Filing questions on a branch about to
  disappear optimises for proximity while drafting and pays for it in the durable
  record — and the durable record is this system's product. The one loss, a
  question no longer sitting beside its draft, is already mitigated: the comment
  names its round.

  projectstate.DesignBranch goes with it. AmendmentIndexFor STAYS: five
  commit-transition sites reason from it about why the commit bump is monotonic,
  and that argument is about commits, not branches.

  Closes 4b1 Q6.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 4: The pump sweep stops skipping every project that is not in construction

**A LIVE DEFECT on main, one line, named in no doc before the 4b2 recon.** `pumpsweep.go:88`:

```go
		// Eligibility mirrors nextEligibleActivity's own Phase gate exactly …
		if s.Phase != projectstate.PhaseConstruction {
			continue
		}
```

The comment is now false. `nextEligibleActivity`'s phase gate is no longer blanket — 4b1 replaced it with `admissibleInPhase` (`deliverymanager.go:7818`) plus the `eligibleWithDesign` rung, so a design activity is admissible in Phase 1 or 2 (`:7716`: `proj.Phase != PhaseConstruction && !rule.admitsDesignActivities()`). The sweep still filters on the OLD rule, so **a project in Phase 1 or 2 is never swept and its three design activities are reached only by a manual `Begin`.** That is the difference between "the pump runs design" and "an operator runs design", and it is the first thing that will be noticed on this repo after the drain, because this repo's `phase` is 2.

**Files:**
- Modify: `server/internal/manager/delivery/pumpsweep.go:82-92`.
- Modify: `server/internal/manager/delivery/manager_test.go` — the `Test_PumpSweep_*` family.

**Interfaces:** none. `pumpSweepResult` stays unexported (no contract entry) and the workflow name is unchanged.

- [ ] **Step 1: Write the failing test.**
  ```go
  // Test_PumpSweep_SweepsAProjectInDesignPhases is the live defect. Stage 4b1 made the three
  // design activities dispatchable and re-pointed the pump at the generic child, but the
  // SWEEP kept the pre-4b1 blanket phase gate — so a project at phase 1 or 2 was skipped
  // every 30 seconds and only a manual Begin ever started its design walk.
  //
  // The assertion is on the sweep's RESULT (PumpedProjects), not on a log line: a sweep that
  // silently skips is exactly the failure this catches.
  func Test_PumpSweep_SweepsAProjectInDesignPhases(t *testing.T) {
  	// three summaries: phase 1, phase 2, phase 3(construction); none paused.
  	// want: all three pumped.
  }
  ```
  - [ ] **Verify first:** `grep -n 'func Test_PumpSweep_' internal/manager/delivery/manager_test.go` and read the nearest existing case to reuse its rig (the sweep drives `ProjectStateListProjects` through a double and starts children on a test env). Do not build a second rig.
  - [ ] Run: `GOWORK=off go test ./internal/manager/delivery/ -run Test_PumpSweep_SweepsAProjectInDesignPhases -count=1 -v`. Expected: **FAIL**, `PumpedProjects` holding only the construction project.

- [ ] **Step 2: Write the guard test that must NOT change.**
  ```go
  // Test_PumpSweep_StillSkipsAPausedProject pins G-S1 from the guard census. The line above
  // it moves in this task; this one must not. The sweep must never silently override an
  // operator pause every 30 seconds.
  func Test_PumpSweep_StillSkipsAPausedProject(t *testing.T) { … }
  ```
  Expected: **GREEN** before and after Step 3.

- [ ] **Step 3: Delete the filter and say what replaced it.**
  ```go
  	for _, s := range summaries {
  		// THE PHASE FILTER IS GONE (stage 4b2 Task 4). It used to read
  		// `if s.Phase != PhaseConstruction { continue }` and its comment claimed to mirror
  		// nextEligibleActivity's own gate — which was true until stage 4b1 replaced that
  		// blanket gate with admissibleInPhase plus the eligibleWithDesign rung. After 4b1 the
  		// two disagreed: the pump will dispatch `requirements` / `architecture` / `projectDesign`
  		// at phase 1 or 2, and the sweep refused to start a pump that would. So a project in
  		// either design phase was swept never and walked only when an operator pressed Begin.
  		//
  		// There is nothing to replace it with. The per-project pump is ALREADY a quiet no-op
  		// for anything it must not touch — verdictQuiescent returns without ContinueAsNew and
  		// without a write — so the filter only ever saved a child start. Re-deriving the
  		// admission rule here would be a SECOND copy of nextEligibleActivity's phase logic,
  		// which is the drift hazard that produced this defect in the first place.
  		//
  		// The operator pause stays, and it is a different question: it is an instruction, not
  		// an eligibility fact, and the sweep must not override it every 30 seconds.
  		if s.OperatorPaused != nil && *s.OperatorPaused {
  			continue
  		}
  ```
  - [ ] Run both tests: expected **both GREEN**.

- [ ] **Step 4: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_PumpSweep' -count=1 -v 2>&1 | tail -20
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint vet
  ```
  Expected: green. **Golden 134, frozen 15, shapes 10/10, replay 8/8, validate 43/0, npm 1228, preview 50/23 — all unchanged.** The sweep's four archived replay fixtures are in `replay-archive/` and are not replayed; the eight live ones are `deliveryActivity` histories and do not contain the sweep. **State the consequence in the commit: after the drain, this repo's first sweep tick starts the pump for a phase-2 project, which is the intended behaviour and is load-bearing on `seedWalkFromLedger`'s `committedArtifactOfTask` arm** (all 17 slots are status 2 except `standardCheck`, so all three design walks seed passed and exit without drafting).
  ```bash
  git add server/internal/manager/delivery/pumpsweep.go server/internal/manager/delivery/manager_test.go
  git commit -F - <<'MSG'
  fix(pump): the sweep stops skipping every project that is not in construction

  Stage 4b1 made the three design activities dispatchable and re-pointed the pump
  at the generic child. It did not move the SWEEP, which still filtered
  `s.Phase != PhaseConstruction` under a comment claiming to mirror
  nextEligibleActivity's gate. That gate stopped being blanket in 4b1 —
  admissibleInPhase plus the eligibleWithDesign rung admit a design activity at
  phase 1 or 2 — so for one wave a project in either design phase was swept never,
  and its design walk started only when an operator pressed Begin.

  Nothing replaces the filter. The per-project pump is already a quiet no-op for
  anything it must not touch: verdictQuiescent returns with no write and no
  continue-as-new, so the filter only ever saved a child start. Re-deriving the
  admission rule in the sweep would be a second copy of the rule that just drifted.

  The operator-pause skip stays. It is an instruction, not an eligibility fact.

  After the drain this repo's first sweep tick starts a pump for a phase-2 project.
  That is intended, and it rests on one guard: seedWalkFromLedger seeds every task
  of an already-committed slot as passed, so the three walks dispatch nothing,
  commit nothing and exit. Watch the first tick anyway.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 5: The session-view fold — two derived types become ONE, and `constructionSession` does not join them

**The measurement settles the shape (R-D).** `designCompletedSessionView` (`deliverymanager.go:222`) and `planCompletedSessionView` (`:4250`) are the same function and the only producers of `SessionStateView` / `ProjectSessionStateView`, and between them they emit exactly three stages. Every other member of the 8- and 9-member enums is unreachable, **including `ProjectStageAssemblingSDP`, the one member that made the two enums structurally different, whose producer 4b1 deleted.** Neither producer sets `Critique`, `FailureRunURL`, `RunURL`, `Findings`, `StageName`, `ActiveRole`, `ActiveStep` or `Round`.

**The honest derived type,** serving BOTH Phase-1 and Phase-2 kinds because the kind is already a field on it, with **no discriminator** (a discriminator nothing branches on is a field documenting a distinction the code does not make):

```
DesignArtifactSessionView { projectId, artifactKind, stage: committed|withdrawn|draftFailed, draft, reviewThread[], failureReason? }
```

`constructionSession` does **NOT** fold in: different subject (an ACTIVITY, not an artifact kind), different lifetime (a running execution's live query), 12 members with no overlap. §10's god-* risk applies to types as much as to workflows.

**What the fold RETIRES, and it is the payoff:** the four surviving near-twin body pairs — `askDesignQuestions`/`askPlanQuestions` (`:759`/`:4460`), `ackDesignStaleBasis`/`ackPlanStaleBasis` (`:642`/`:4328`), `refuseDesignAckDuringLiveSession`/`refusePlanAckDuringLiveSession` (`:692`/`:4378`), and the two completed-session views (`:222`/`:4250`). They differ only by the kind SET they admit and the wire TYPE they return.

**Files:**
- Modify: `.aiarch/state/project.json` — **NO. This task edits no model.** *(See Step 1: the `$defs` deletion rides Task 7, and the Go/SPA fold lands first behind the still-generated types. Read Step 1 before starting.)*
- Modify: `server/internal/manager/delivery/deliverymanager.go` — merge the four pairs; one `designArtifactSessionView`; delete `pdCommittedSessionView` (`:4263`).
- Modify: `webApp/src/contracts/types.ts` (`:466-520`, `:572-631`), `webApp/src/contracts/wire.ts` (`:58`, `:836`, `:871-878`), `webApp/src/contracts/enumMappings.ts` (`:125-152`), `webApp/src/hooks/useDeliveryQueries.ts` (`:193-204`, `:219-307`), `webApp/src/hooks/sessionPolling.ts`, `webApp/src/components/design/SystemDesignView.tsx`, `webApp/src/containers/McpSystemDesignContainer.tsx`.
- Modify: `server/internal/manager/delivery/manager_test.go`.

**Interfaces produced (Task 7 deletes the `$defs` these name; Task 17 records the wire break):**
- `func (m *deliveryManager) designArtifactSessionView(ctx context.Context, projectID ProjectID, kind ArtifactKind) (SessionStateView, error)` — the ONE producer, serving all seventeen kinds.
- `func (m *deliveryManager) askArtifactQuestions(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, addressee string, questions []AnchoredComment) error` — the merged ask, admitting any kind.
- `func (m *deliveryManager) ackArtifactStaleBasis(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, note string) error`.
- webApp: `DesignArtifactSessionView` replaces `SessionStateView` and `ProjectSessionStateView`; `SessionStateResponse` keeps its name (`SystemDesignView.tsx` consumes it); `ProjectSessionStage`, `projectSessionStageFromOrdinal`, `mapProjectSessionState`, `useProjectSessionState`, `PROJECT_REVIEWABLE_STAGE` and `PROJECT_TERMINAL_STAGES` are **deleted** (measured: zero consumers outside their own files, and **zero node tests name any of them**).

- [ ] **Step 1: Read this before writing anything — where the `$defs` edit goes and why it is not here.**

  The fold is a WIRE change, and this plan has ONE model edit (R-H, Task 7). Splitting it costs a second full regen and a second `gen-*-check` risk. So this task lands the **Go and SPA fold behind the types that still exist**, and **Task 7 Step 6 deletes `ProjectSessionStateView`, `ProjectSessionStage` and `ProjectView.projectSession` from `.serviceContracts.deliveryManager.$defs`.** Between the two commits the generated `ProjectSessionStateView` exists and nothing produces it — which is exactly the state the fold is removing, held for one commit, and it is GREEN because a `$defs` entry nothing constructs is inert.

  **The consequence the implementer must not miss:** this task may not delete `mapProjectSessionState` from `wire.ts` until `schema.ts` stops carrying `DeliveryProjectSessionStateView`, because `wire.ts` types the parameter off `Schemas[...]`. So `wire.ts`'s deletion is **Task 7 Step 6b**, and this task leaves `mapProjectSessionState` in place with a one-line marker naming Task 7. Everything else folds here.
  - [ ] Confirm the split is still true: `grep -n 'DeliveryProjectSessionStateView\|DeliveryProjectSessionStage' webApp/src/contracts/schema.ts | head`. Expected: present (generated). If absent, the model edit already ran and this step is moot.

- [ ] **Step 2: Write the failing test — one producer, three stages, seventeen kinds.**
  ```go
  // Test_DesignArtifactSessionView_IsTotalOverEveryKind is the fold's premise, executable.
  // Before it, TWO producers answered the same question for disjoint kind sets and returned
  // two wire types whose enums diverged at exactly one member — ProjectStageAssemblingSDP,
  // whose producer stage 4b1 deleted. After it there is ONE producer, and the assertion is
  // that it is TOTAL: every one of the seventeen artifact kinds resolves, and every answer
  // is one of the three stages the slot can actually express.
  func Test_DesignArtifactSessionView_IsTotalOverEveryKind(t *testing.T) {
  	for _, k := range projectstate.AllArtifactKinds() {
  		for _, st := range []projectstate.ReviewStatus{
  			projectstate.ReviewCommitted, projectstate.ReviewWithdrawn,
  			projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected,
  		} {
  			// seed a project whose slot k has status st; assert the view resolves and
  			// its Stage ∈ {StageCommitted, StageWithdrawn, StageDraftFailed}
  		}
  	}
  }

  // Test_DesignArtifactSessionView_PhaseTwoKindsNoLongerTakeASecondDoor pins the deletion:
  // a Phase-2 kind must reach the SAME producer a Phase-1 kind reaches. Two doors to one
  // question is how the two enums drifted in the first place.
  func Test_DesignArtifactSessionView_PhaseTwoKindsNoLongerTakeASecondDoor(t *testing.T) { … }
  ```
  - [ ] **Verify first:** `grep -n 'func AllArtifactKinds' -A 8 internal/resourceaccess/projectstate/projectstateaccess.go` — confirm it returns all seventeen including the two C1 retires (it must: C1 keeps the ordinals and the membership).
  - [ ] Run: expected **FAIL** (`designArtifactSessionView` undefined).

- [ ] **Step 3: Merge the two producers.** Delete `pdCommittedSessionView` (`:4263`) and `planCompletedSessionView` (`:4250`); rename `designCompletedSessionView` → `designArtifactSessionView` and drop its kind restriction. The body is `committedSessionView` unchanged — it already switches on `slot.Status` and nothing else.
  ```go
  // designArtifactSessionView projects the durable SLOT of one artifact kind onto the view the
  // SPA reads. ONE producer for all seventeen kinds (stage 4b2 Task 5).
  //
  // WHY THERE WERE TWO, AND WHY THAT IS OVER. Phase 1 and Phase 2 each had a rail, so each had
  // a session type and a stage enum; the enums diverged at exactly one member,
  // ProjectStageAssemblingSDP, emitted by the SDP-assembly workflow that stage 4b1 deleted.
  // What was left was the same function twice, projecting the same slot through the same
  // switch, into two types whose only remaining difference was two optional members neither
  // producer ever set. The kind is a FIELD on the view, so one type serves both halves and no
  // discriminator is needed — a discriminator nothing branches on documents a distinction the
  // code does not make.
  //
  // THE THREE STAGES ARE ALL A SLOT CAN SAY, and that is a property of the door, not a
  // simplification: a committed slot is Committed, a withdrawn one is Withdrawn, and any other
  // terminal-but-uncommitted status is an honest DraftFailed — NEVER StageDrafting, so the SPA
  // cannot wedge on a spinner for a session that is over. A MID-WALK activity reports its
  // SLOT's stage through this door; the live view is constructionSession, which is a different
  // subject with a different lifetime and does NOT fold in here.
  ```
  - [ ] **Verify first:** `grep -n 'designCompletedSessionView\|planCompletedSessionView\|pdCommittedSessionView\|committedSessionView' internal/manager/delivery/*.go | grep -v _test` — walk every caller. Expected call sites before the edit: `:792`+`:870` (ask/resolve — `:870` already gone in Task 3), `:4484`, the two `QueryProjectView` session arms, and the two refusals.

- [ ] **Step 4: Merge the three other twin pairs.** For each pair, the merged body is the design one with its kind guard widened; the pd twin's ONE semantic difference must be carried, not dropped:
  - `askDesignQuestions` + `askPlanQuestions` → `askArtifactQuestions`. **Carry `pdCheckNoReplyTo`** (`deliverymanager.go` ~`:6053`): the Phase-2 ledger's outright refusal of a `replyTo` is real and 4b1's R-K confirmed it. Widen it to every kind or keep it kind-scoped — **whichever, say which in a comment**, because silently applying a Phase-2 refusal to Phase-1 asks is a behaviour change the brief did not name. **This plan's ruling: keep it kind-scoped** (`k.IsPhase2()`), because the Phase-1 rails have accepted threaded replies since stage 3 and the client's `askEntriesFor(foldReplies: true)` fold is keyed off exactly that.
  - `ackDesignStaleBasis` + `ackPlanStaleBasis` → `ackArtifactStaleBasis`.
  - `refuseDesignAckDuringLiveSession` + `refusePlanAckDuringLiveSession` → `refuseArtifactAckDuringLiveSession`.
  - [ ] **Verify first, per pair:** `diff <(sed -n '<d1>,<d2>p' deliverymanager.go) <(sed -n '<p1>,<p2>p' deliverymanager.go)` and read every differing line. **A pair that differs by more than the kind set and the wire type is not a twin and must not be merged** — record it instead and say so in Task 17.
  - [ ] Run the façade suite: `GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Ask|Test_Ack|Test_Refus|Test_SessionView|Test_DesignArtifactSessionView' -count=1 -v`.

- [ ] **Step 5: Fold the SPA.** Delete `ProjectSessionStage` and its mapping table (`types.ts:572-590`, `enumMappings.ts:125-152`), `ProjectSessionStateView` (`types.ts:592-612`), `PROJECT_REVIEWABLE_STAGE` / `PROJECT_TERMINAL_STAGES` (`:627-632`), `useProjectSessionState` (`useDeliveryQueries.ts:284`) and `projectSessionStageFromOrdinal`'s import (`wire.ts:58`). Rename `SessionStateView` → `DesignArtifactSessionView` and re-export the old name as a deprecated alias for the stage-6 MCP cluster.
  - [ ] **Verify first:** `grep -rn 'ProjectSessionStage\|ProjectSessionStateView\|useProjectSessionState\|PROJECT_TERMINAL_STAGES\|PROJECT_REVIEWABLE_STAGE' webApp/src | grep -v contracts/schema.ts` — measured at `bae7681f`: **all hits are inside `types.ts`, `enumMappings.ts`, `wire.ts`, `enums.gen.ts` and `useDeliveryQueries.ts`, and there are ZERO in any `*.test.ts`/`*.test.tsx` and ZERO in any component.** If that is still true, `npm run check` stays at **1228** and no node test is added or removed by this task.
  - [ ] `cd webApp && ASDF_NODEJS_VERSION=lts npm run check`. Expected: **1228/1228**, typecheck clean.

- [ ] **Step 6: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/manager/delivery/ -count=1 2>&1 | tail -20
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint vet sumtype-check encapsulation-check
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check && ASDF_NODEJS_VERSION=lts npm run build:mcp
  ```
  Expected: **golden 134, frozen 15, shapes 10/10, replay 8/8, validate 43/0 (no model edit yet), npm 1228, preview 50/23 — all unchanged.** Hand-written delivery lines fall by **300–500** (the four retired pairs) — this is the single largest deletion in the wave outside the pump, and Task 17's measurement is where it shows.
  ```bash
  git add server webApp
  git commit -F - <<'MSG'
  refactor(delivery): one derived session view, because there was only ever one
  question

  QueryProjectView carried THREE session members. Two of them were the same
  function: designCompletedSessionView and planCompletedSessionView both read the
  project, both projected the durable slot through the same status switch, and both
  were the only producers of their respective wire types. Between them they could
  emit exactly three stages — committed, withdrawn, draftFailed — out of an
  eight-member enum and a nine-member one. Every other member was unreachable,
  including ProjectStageAssemblingSDP, the ONE member that made the two enums
  structurally different, whose producer stage 4b1 deleted.

  So the two derived types become one, serving all seventeen kinds, with no
  discriminator: the artifact kind is already a field on the view, and a
  discriminator nothing branches on documents a distinction the code does not make.

  constructionSession does NOT fold in. Different subject (an activity, not an
  artifact kind), different lifetime (a live execution's query), twelve members
  with no overlap. Folding it behind a discriminator would produce one type whose
  every consumer branches on the discriminator immediately — two types wearing one
  name, which is the god-* risk applied to types.

  This is what retires the four near-twin body pairs stage 4b1 had to leave:
  ask, acknowledge, the two refusals and the two views. pdCheckNoReplyTo stays
  kind-scoped, deliberately — the Phase-1 rails have accepted threaded replies
  since stage 3 and the client's reply fold is keyed off exactly that.

  The $defs deletion rides the wave's one model edit; this commit is the Go and SPA
  half, and the generated ProjectSessionStateView is inert until it lands.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 6: `reconcileBranchFromMain` preserves a SET of slots, not one

**F80c is still live for a multi-slot design activity on the gh venue, and the final fix wave could not close it because closing it is a contract change.** The RA verb overlays main's every slot but ONE onto the branch tip — written for the retired rail, where a session owned exactly one artifact kind. The generic child's design walk can hold **FOUR** in-flight kinds on ONE activity branch (`designSlotsOfLifecycle`), and reconciling that branch would overwrite three live drafts with main's older copies. The 4b1 workaround, `reconcileTargetOf` (`deliveryactivity.go:4225-4234`), offers the reconcile only at 0 or 1 in-flight kinds; 2+ logs `delivery.merge.reconcileUnavailable` (`:4252`) and takes the honest `MergeBranchReconciled` refusal.

**Ten surfaces, ZERO webApp** — it is an RA verb, not a Manager op, so `schema.ts` and `openapi.yaml` are untouched. **This is the cheapest of the four carries and it unblocks the `requirements` lifecycle (4 in-flight kinds) on the gh venue.**

**Files:**
- Modify: `.aiarch/state/project.json` — **NO.** *(Same split as Task 5: the contract delta is Task 7 Step 5. This task lands the Go-side `kinds []ArtifactKind` plumbing behind the single-`kind` generated signature and then flips at Task 7. Read Step 1.)*
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — `GitStore.ReconcileBranchFromMain` (`:275-290`, the `applyMutationOnBranch` overlay closure at `:284`), `projectStateGitAdapter` (`:1940-1947`), the internal iface (`:3204`), `designSessionAccess` (`:3318-3320`).
- Modify: `server/internal/manager/delivery/deliveryactivity.go` — `branchReconcile` / `reconcileTargetOf` (`:4218-4234`), `reconcileDivergedBranch` (`:4244-4275`).
- Modify: `server/internal/resourceaccess/projectstate/access_test.go` (`:1633`, `:1795-1802`, `:3557-3567`), the four fakes in `manager_test.go` (`:242`, `:479`, `:1448`, `:2585`, `:4607`, `:11278`), `projectstate/fake/fake.gen.go` (regenerated).

**Interfaces produced:**
- `ReconcileBranchFromMain(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kinds []ArtifactKind, idempotencyKey fwra.IdempotencyKey) (Version, error)` — **`kinds` is the PRESERVE set**: every slot NOT in it is overlaid from main. An empty/nil slice preserves nothing and adopts main's every slot, which is exactly the construction case.
- `type branchReconcile struct { ok bool; kinds []projectstate.ArtifactKind }` — `ok` is now **always true**; the field survives one commit so `reconcileDivergedBranch`'s call shape does not move twice, and Task 6 Step 5 deletes it.

- [ ] **Step 1: Read this first — the two-commit shape.** The generated `contract.gen.go:1091` and the generated Activity/invoker (`activities.gen.go:373`, `invokers.gen.go:374`) carry `kind ArtifactKind` until Task 7 regenerates them. So **this task changes the STORE and the internal iface**, adds `ReconcileBranchFromMainKinds` as the new store-level verb, and leaves the generated single-kind surface delegating to it with a one-element slice. Task 7 Step 5 then edits the `$defs`, regenerates, and Step 5b deletes the delegation. Both halves are named in both commits so neither reads as unfinished.
  - [ ] Confirm: `grep -n 'ReconcileBranchFromMain' server/internal/resourceaccess/projectstate/contract.gen.go` — expected one line, `:1091`, single `kind`.

- [ ] **Step 2: Write the failing test at the STORE.**
  ```go
  // Test_ReconcileBranchFromMain_PreservesEverySlotInTheSet is F80c's real case. The verb was
  // written for a session that owned ONE artifact kind; the generic child's `requirements`
  // walk holds FOUR in-flight kinds on ONE activity branch, and reconciling it with the
  // one-kind verb overwrites three live drafts with main's older copies.
  //
  // Seeded: main carries older models for mission/glossary/volatilities/coreUseCases; the
  // branch carries NEWER drafts for all four plus an older `system`. Reconciling with
  // kinds={mission,glossary,volatilities,coreUseCases} must leave all four branch drafts
  // byte-identical and adopt main's `system`.
  func Test_ReconcileBranchFromMain_PreservesEverySlotInTheSet(t *testing.T) { … }

  // Test_ReconcileBranchFromMain_EmptySetAdoptsMainEntirely pins the construction case, which
  // used to be expressed as "the ZERO ArtifactKind matches no slot-table entry". That worked
  // by accident of the zero value naming a real kind (KindMission is 0!) — measured: the old
  // code passed branchReconcile{}.kind, which is KindMission, so a construction reconcile
  // PRESERVED the branch's mission slot. Harmless only because a construction branch holds no
  // mission draft. An explicit empty SET says what was meant.
  func Test_ReconcileBranchFromMain_EmptySetAdoptsMainEntirely(t *testing.T) { … }
  ```
  - [ ] **Verify first, and this is the finding the task must not lose:** `grep -n 'KindMission' internal/resourceaccess/projectstate/projectstateaccess.go | head -3` — confirm `KindMission == 0`. If so, `reconcileTargetOf`'s zero-value case at `deliveryactivity.go:4229` (`return branchReconcile{ok: true}`) passes `KindMission`, **not** "no kind", and its comment ("the ZERO `ArtifactKind` matches no slot-table entry") is **wrong**. Record it in Task 17 regardless of whether it ever bit.
  - [ ] Run: expected **FAIL** (compile error; the verb takes one kind).

- [ ] **Step 3: Widen the store.** In `GitStore.ReconcileBranchFromMain`, change the overlay closure at `:284` to skip every member of the set:
  ```go
  	// PRESERVE A SET, NOT A SLOT (stage 4b2 Task 6; F80c's real case). The overlay adopts
  	// main's copy of every slot EXCEPT the ones this branch is actively drafting. That used
  	// to be exactly one, because a co-author session owned one artifact kind; the generic
  	// child's `requirements` walk owns FOUR on one branch, and preserving one of four means
  	// silently replacing three live drafts with main's older copies at the moment a diverged
  	// PR is being repaired — the worst possible moment for a quiet data loss.
  	preserve := make(map[ArtifactKind]bool, len(kinds))
  	for _, k := range kinds {
  		preserve[k] = true
  	}
  	…
  		for idx, slot := range mainProject.Slots {
  			if preserve[slotKindOf(idx)] {
  				continue
  			}
  			p.Slots[idx] = slot
  		}
  ```
  - [ ] **Verify first:** read `applyMutationOnBranch`'s closure signature and the existing slot-table walk (`sed -n '266,300p' internal/resourceaccess/projectstate/projectstateaccess.go`) and use ITS variable names and ITS slot-indexing idiom. Do not introduce a second way to walk slots.
  - [ ] Guard: an empty `branch` still refuses `ContractMisuse` (`:277`) — that guard is unchanged and its test must stay green.

- [ ] **Step 4: Widen the callers.** `reconcileTargetOf` returns every kind of the lifecycle; `reconcileDivergedBranch` passes the slice; the `reconcileUnavailable` warn branch and the `ok` field become dead.
  ```go
  // reconcileTargetOf reads the preserve SET off the LIFECYCLE's own slots, so a lifecycle that
  // gains or loses a design task moves this answer with it. There is no longer an "unavailable"
  // answer: the verb preserves a set, and a set of any size is expressible.
  func reconcileTargetOf(lc methodassets.Lifecycle) []projectstate.ArtifactKind {
  	return designSlotsOfLifecycle(lc)
  }
  ```
  - [ ] Delete `branchReconcile` and the `if !rec.ok` warn arm in `reconcileDivergedBranch`, **and delete the log key `delivery.merge.reconcileUnavailable` with it** — a log key nothing emits is a search that returns nothing and a reader who concludes the path is cold.
  - [ ] Run: `GOWORK=off go test ./internal/resourceaccess/projectstate/ ./internal/manager/delivery/ -run 'Reconcile' -count=1 -v`. Expected: all green, both new cases included.

- [ ] **Step 5: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint vet sumtype-check encapsulation-check
  cd ../systemtests && GOWORK=off go build ./...
  ```
  Expected: **golden 134, frozen 15, shapes 10/10, replay 8/8, validate 43/0, npm 1228, preview 50/23 — all unchanged.** The replay assertion is load-bearing: the generated Activity still takes one kind at this commit, so the recorded command sequence is byte-identical. **It will NOT be after Task 7** — Task 7's gate block says so and the eight fixtures are re-checked there.
  ```bash
  git add server/internal/resourceaccess/projectstate server/internal/manager/delivery
  git commit -F - <<'MSG'
  fix(projectstate): the branch reconcile preserves a SET of slots

  F80c repairs a diverged activity branch by overlaying main's slots onto its tip,
  preserving the one the branch is drafting. That was right when a co-author
  session owned exactly one artifact kind. The generic child's `requirements` walk
  owns FOUR on one branch — so repairing a diverged PR would have replaced three
  live drafts with main's older copies, at the exact moment somebody was trying to
  rescue the branch.

  Stage 4b1's final fix wave could not close it, because closing it is a contract
  change: it offered the reconcile only at zero or one in-flight kinds and took an
  honest refusal otherwise, which left F80c live for a multi-slot design activity
  on the gh venue.

  `kind ArtifactKind` becomes `kinds []ArtifactKind`, read as a PRESERVE set, and
  reconcileTargetOf is now just designSlotsOfLifecycle — the lifecycle already
  knows the answer. There is no "unavailable" arm left, so its log key goes too: a
  log key nothing emits is a search that returns nothing and a reader who concludes
  the path is cold.

  One thing found while widening it, recorded because it was wrong in a comment for
  a whole wave: the construction case passed branchReconcile{}.kind, and
  KindMission is ZERO — so a construction reconcile PRESERVED the branch's mission
  slot rather than "matching no slot-table entry" as its comment claimed. Harmless,
  because a construction branch holds no mission draft. An explicit empty set says
  what was meant.

  The $defs edit and the regen ride the wave's one model edit.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---


### Task 7: THE ONE MODEL EDIT — revenue share leaves the vocabulary, two kinds stop being draftable, three contract deltas ride along

**This is the only task in 4b2 that touches `.aiarch/state/project.json`, and it runs the self-amendment loop exactly once.** Seven independent changes queue behind it because each regen costs the same nine generated surfaces and nine `gen-*-check` gates (R-H). No two implementers on this file, ever.

**The seven:**

| # | Change | Why it is here |
|---|---|---|
| **A** | **Revenue share removed as a concept** (R7, founder R-F1) | 46 structural occurrences + 1 prose, mapped below. Verified against committed objective 3 — revenue share is not among the named growth paths, so deleting it is consistent with the ratified objectives. SUPERSEDES the architect's `RevenueShareNone`. Also removes the defaulting arm that made slot 8 uncomputable from 2026-06-09. |
| **B** | **`scrubbedRequirements` and `standardCheck` stop being draftable** (R8, founder R-F3; scoped by **C1**) | Slugs, critique entry, three command files, contract prose. **Ordinals and committed slot data STAY** — see C1. |
| **C** | **`ProjectSessionStateView` / `ProjectSessionStage` / `ProjectView.projectSession` deleted** | Task 5's `$defs` half. |
| **D** | **`ReconcileBranchFromMain`'s `kind` → `kinds`** | Task 6's `$defs` half. |
| **E** | **`deliveryManager.ReplanProject` removed (12 → 11 ops)** | Task 11's contract half (**C3**). |
| **F** | **`DeliveryTaskRevisionView` gains `detail?: string`** | **CONDITIONAL — see the Scope table. Default: INCLUDE.** ~40 lines, the only item that changes what a human approves, and the silent case is LIVE on this repo. |
| **G** | **Two stale descriptions fixed** | `ReviewSubjectRef.kind` (says today's writers mint only `pullRequest`; as of `38fd7f9c` both rails mint `commit`) and `ReviewRoundInput.roundId` (names `scrubbedRequirements` / `standardCheck` as kinds sharing a phase — B retires them). Both are pure text, both are copied verbatim into `openapi.yaml`, `schema.ts` and `delivery_tools.gen.go`, and both ride the one regen or wait a wave. |

**Files:**
- Modify: `.aiarch/state/project.json` — the exact JSON pointers are in Steps 2–8. **Slots 9 and 10 are never hand-edited** (`make derived-plan-write` alone); this edit touches neither.
- Regenerate: everything the loop produces.
- Modify (hand-written, because the generator does not own them): `server/internal/engine/billing/billingengine.go`, `server/internal/engine/operationestimation/operationestimationengine.go`, `server/internal/manager/billing/closecycle.go:462`, `server/internal/manager/delivery/deliverymanager.go` (`:5367`, `:5418-5433`, `:5446-5464`, `:11106-11162`), `server/internal/resourceaccess/projectstate/projectstateaccess.go` (`:5673-5690`, `:9263-9320`), `server/internal/arch_test.go:1058-1061`, `server/cmd/clientgen/mcpdocs.go:26`, `server/cmd/server/managerlog.go:97-104`, `server/internal/manager/delivery/manager_test.go`, `server/internal/engine/billing/engine_test.go`, `server/internal/engine/operationestimation/engine_test.go`, `server/cmd/aiarch-state-mcp/crossartifact.go`.
- Modify (webApp): `src/contracts/adapters.ts` (`:907`, `:961`, `:1071`), `src/contracts/projectAdapters.ts` (`:639`, `:664`), `src/components/HomeBaseParts.tsx` (`:84-114`), `src/components/project/PlanningAssumptionsView.tsx:107`, `src/components/activity/m0CostBasis.test.ts:29-30`.
- Modify (uitests): the **20 preview fixtures** that carry a revenue-share field — `activity-experience/*.json` (13) and `plan/*.json` (5), plus the two in `plan/` that carry `revenueSharePercent` on an SDP option row. The fixture schema validator (`fixture-schema.mjs`) is what catches a missed one.
- Delete: `.claude/commands/scrubbed-requirements-draft.md`, `.claude/commands/scrubbed-requirements-critique.md`, `.claude/commands/standard-check-draft.md`.

**Interfaces produced:**
- `billingEngine.ProjectCommitTimeComputeCost(rc fweng.Context, option ProjectOption) (Projection, error)` — the renamed op (was `…RevenueShareAndComputeCost`). **Not a Temporal name** (engines register no activities — verified: the golden carries `billingStateAccess.*` and no `billingEngine.*`), so the golden does not move for it.
- `projectstate.SettlementTerms{ ComputeCost, ComputeMarkupPercent, Schedule }` — `revenueShare` and `revenueSharePercent` gone.
- `billing.BillingTerms{ ComputeCost, … }`, `billing.BillingResult` without `RevenueShareApplied`, `billing.Projection` without `RevenueShareKind`/`RevenueSharePercent`.
- `projectstate.SdpOptionRow` without `revenueSharePercent`.
- `delivery.ReconcileBranchFromMain(… kinds []ArtifactKind …)`.
- `DeliveryTaskRevisionView.detail?: string` (conditional F) — the render-ready attempt sentence (`drafted <kind> on <branch>`, `dispatched <command> for <phase>`, `asked N question(s) of <role>`, the venue's failure sentence with its run URL). Produced at `deliveryactivity.go:1508` (`Detail: defaultedDetail(defaulted)`), derived at `deliverymanager.go:8969`. Consumed by Task 9 Step 5.

- [ ] **Step 1: Snapshot and map, before editing.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b2
  cp .aiarch/state/project.json /tmp/pj-before.json
  python3 - <<'PY'
import json
d=json.load(open('.aiarch/state/project.json'))
def walk(o,p=""):
    if isinstance(o,dict):
        for k,v in o.items():
            if 'evenueShare' in k: print("KEY",p+"/"+k,"=",json.dumps(v)[:120])
            walk(v,p+"/"+k)
    elif isinstance(o,list):
        for i,v in enumerate(o): walk(v,p+f"[{i}]")
    elif isinstance(o,str) and 'evenueShare' in o: print("STR",p)
walk(d)
PY
  ```
  Expected, exactly (measured for this plan at `bae7681f`) — **21 `revenueShare` + 25 `RevenueShare` = 46 structural, plus 1 prose in `slots["8"].model.notes`**:
  - `/slots/16/model/options[0..3]/revenueSharePercent` (4)
  - `/slots/8/model/terms/revenueShare`, `/revenueSharePercent` (2)
  - `/serviceContracts/billingEngine/$defs/`: `BillingResult.properties.RevenueShareApplied` + its `required[2]`; `BillingTerms.properties.revenueShare` (+`$ref`) and `.revenueSharePercent` + `required[0..1]`; `Projection.properties.RevenueShareKind` (+`$ref`) and `.RevenueSharePercent` + `required[0..1]`; `$defs.RevenueShareKind` + its three `x-enum-varnames`
  - `/serviceContracts/billingEngine/interface/operations[1]/name` = `ProjectCommitTimeRevenueShareAndComputeCost`
  - `/serviceContracts/billingStateAccess/$defs/BillingTerms/properties/RevenueShareKind` + `required[0]`
  - `/serviceContracts/operationEstimationEngine/$defs/`: `RevenueShareKind` + 3 varnames; `SettlementTerms.revenueShare` (+`$ref`), `.revenueSharePercent`, `required[0..1]`
  - `/serviceContracts/projectStateAccess/$defs/`: `RevenueShareKind` + 3 varnames; `SdpOptionRow.revenueSharePercent` + `required[7]`; `SettlementTerms.revenueShare` (+`$ref`), `.revenueSharePercent`, `required[0..1]`
  - `/slots/8/model/notes` — the 2026-06-09 MoR-reversal sentence, which **rewrites** rather than deletes (see Step 3).
  - [ ] Print the same map for `scrubbedRequirements` / `standardCheck` / `operationalConcepts`, so Step 9's edit is scoped before it starts.

- [ ] **Step 2 (A): Remove revenue share from the four contracts.** Delete every `$defs.RevenueShareKind`, every `revenueShare`/`revenueSharePercent`/`RevenueShareKind`/`RevenueSharePercent` property, every `RevenueShareApplied` property, and their `required` entries. **Rename** `billingEngine` operation `ProjectCommitTimeRevenueShareAndComputeCost` → `ProjectCommitTimeComputeCost`, and rewrite its description to say what it now does and what it stopped doing.
  - [ ] **The rule for a money field (execution risk 7), stated in the edit:** a field that IS the removed concept goes (`RevenueShareApplied`, `revenueShare`, `revenueSharePercent`, `RevenueShareKind`). A field that carries a TOTAL which happened to include a revenue share is **left alone and not re-meaned** — `ComputeCost`, `ComputeMarkupPercent`, `Schedule`, `ProjectedMonthlyCost`, `ExpectedPerCycleNet`, `Total` all stay untouched.
  - [ ] **Do NOT renumber any surviving enum.** `ComputeCostKind` and `SettlementScheduleKind` keep every ordinal.

- [ ] **Step 3 (A): Rewrite slot 8's terms and its notes; strip slot 16's option rows.**
  - `slots["8"].model.terms` becomes `{"computeCost": 2, "computeMarkupPercent": 0, "schedule": 1}`.
  - `slots["8"].model.notes` — replace the sentence *"no revenue share; no charge for construction or LLM work; revenueShare=0 (no payout; the old merchant-of-record / revenue-share model was reversed)"* with the state that is now true: *"The platform bills a usage-based hosting fee for operating a delivered system and nothing else. Revenue share was reversed on 2026-06-09 and is no longer part of the vocabulary (stage 4b2, founder ruling): the concept is gone rather than set to zero, because a field whose UNKNOWN member had to stand in for 'none' could not state the platform's actual posture."*
  - `slots["16"].model.options[0..3]` lose `revenueSharePercent`.
  - [ ] **Verify first:** `python3 -c "import json;d=json.load(open('.aiarch/state/project.json'));print(json.dumps(d['slots']['16']['model']['options'][0],indent=1))"` — confirm the row's other members and edit only the one key. **Slot 16 is NOT slots 9/10 and is hand-editable**, but it is COMPUTED by `computeProjectPlanSlots`, so the Go compute must stop emitting the field in the same commit or `derived-plan-check`'s sibling gates will drift on the next M0.

- [ ] **Step 4 (A): Walk the Go and TypeScript callers.** In this order, because the type errors cascade:
  1. `projectstate`: delete `RevenueShareKind` (`projectstateaccess.go:5673-5690`) and its members; `arch_test.go:1058-1061`'s allowlist entries go with it (**an allowlist entry naming a deleted symbol is the exact residue 4b1's R12 warned about**).
  2. `engine/billing`: rename the op, drop `RevenueShareApplied` from `BillingResult`, drop the two `Projection` members, and **delete the unknown-regime refusal** in `ProjectCommitTimeComputeCost` — *"settling real money under an unregistered revenue-share regime is a financial-correctness hazard"* refuses a vocabulary that no longer exists. Say so at the site.
  3. `engine/operationestimation`: same shape.
  4. `manager/billing/closecycle.go:462`.
  5. `manager/delivery/deliverymanager.go`: `:5367`'s doc line; `:5418-5433` — **`defaultSettlementTerms` loses the whole `RevenueShareNegotiatedRate`-at-0% workaround and the EARMARK comment at `:5427` that asked for `RevenueShareNone`**; `:5446-5464` — `resolvePlanningAssumptions`'s `pa.Terms.RevenueShare == RevenueShareUnknown` disjunct goes, leaving `ComputeCost == ComputeCostUnknown` as the settlement-terms defaulting trigger; `:11106-11162`'s three conversions.
  6. webApp: `adapters.ts:907` (`REVENUE_SHARE_LABELS`), `:961` (the terms markdown line), `:1071` (the SDP options table column — **the column header goes with the cell**); `projectAdapters.ts:639/664`; `HomeBaseParts.tsx:84-114` (the whole `revenueShareValue` helper and its `Metric`); `PlanningAssumptionsView.tsx:107`; `m0CostBasis.test.ts:29-30` (**fixture literal only — the case count does not change**).
  - [ ] **Verify first, and this is the one that bites:** `grep -rn 'evenueShare' server webApp/src uitests .aiarch --include='*.go' --include='*.ts' --include='*.tsx' --include='*.json' | grep -v webappdist` must return **ZERO** before the commit. `server/cmd/server/webappdist/**` is a build artifact and is regenerated, not edited.

- [ ] **Step 5 (D): `ReconcileBranchFromMain` takes `kinds`.** In `.serviceContracts.designSessionAccess.interface.operations` (the entry at `project.json:22187`), change the `kind` parameter to `kinds` with `{"type":"array","items":{"$ref":"#/$defs/ArtifactKind"}}` and a description naming it a PRESERVE set. Then delete Task 6's delegation shim.

- [ ] **Step 6 (C): Delete the folded session types.** In `.serviceContracts.deliveryManager.$defs`: delete `ProjectSessionStateView` and `ProjectSessionStage`; delete `ProjectView.properties.projectSession`; rename `SessionStateView` → `DesignArtifactSessionView` **and narrow its `stage` `$ref` to a new three-member `DesignArtifactSessionStage`** (`committed`/`withdrawn`/`draftFailed`), deleting `SessionStage`'s five unreachable members with it.
  - **This is a REPLACEMENT, not a renumber** (execution risk 6): neither old enum is a prefix of the new one, and no producer can emit a member the new one lacks (R-D's measurement). State that in the `$defs` description.
  - [ ] **Step 6b:** delete `mapProjectSessionState` from `webApp/src/contracts/wire.ts:871-878` and `projectSessionStageFromOrdinal` from `enumMappings.ts:150`, which Task 5 Step 1 deliberately left.

- [ ] **Step 7 (E, F, G): The three riders.**
  - **E:** delete the `ReplanProject` entry from `.serviceContracts.deliveryManager.interface.operations`. **12 → 11.** Delete `cmd/clientgen/mcpdocs.go:26`'s doc string (an empty one ERRORS `mcpemit.Generate`; a doc for an op that no longer exists is a stale key and the generator will say so), delete `loggingDeliveryManager.ReplanProject` (`managerlog.go:97-104`), and re-point `systemtests` off `DeliveryReplanProject` before `pruneStaleSDK` deletes the SDK file it calls.
  - **F (conditional, default INCLUDE):** add to `.serviceContracts.deliveryManager.$defs.DeliveryTaskRevisionView.properties`:
    ```json
    "detail": {
      "type": ["null", "string"],
      "description": "The attempt's own render-ready sentence, verbatim — 'drafted <kind> on <branch>', 'dispatched <command> for <phase>', 'asked N question(s) of <role>', or a failed venue's whole sentence including its run URL. PRESENT where the revision cites a gate attempt; absent on a reconstructed revision and where no attempt was captured. It exists because the M0 review is a SPEND APPROVAL and the per-family planning-assumption defaulting left no trace on any view: a founder could approve a cost computed on numbers nobody showed them. This is the field that shows them."
    }
    ```
    Then set it in the derivation at `deliverymanager.go:8969` from the attempt's `Detail` (producer `deliveryactivity.go:1508`). **Do NOT add it to `required`.**
  - **G:** rewrite `ReviewSubjectRef.kind`'s description (both rails mint `commit` as of `38fd7f9c`) and `ReviewRoundInput.roundId`'s (it names `scrubbedRequirements` and `standardCheck` as kinds sharing a phase — Step 9 retires them, so the sentence must name kinds that still exist: `system` and `operationalConcepts`).
  - [ ] **Do NOT** attempt the `<branch>@vN` correction here — it is a DOC fix and it is Task 15 Step 3.

- [ ] **Step 8: Run the self-amendment loop ONCE.**
  ```bash
  cd .../server
  GOWORK=off make gen-models gen-fakes gen-client gen-internal-tools gen-temporal gen-sdk gen-config gen-main gen-lifecycles
  GOWORK=off make method-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot PlanningAssumptions
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot SdpReview
  GOWORK=off go test -short -count=1 ./...
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run gen:api && ASDF_NODEJS_VERSION=lts npm run gen:ops && ASDF_NODEJS_VERSION=lts npm run check
  cd ../systemtests && GOWORK=off go build ./...
  ```
  **Expected, stated so an unpredicted number is visible:**
  - `validate --slot System`: **43 advisory / 0 errors — UNMOVED.** Removing an enum and a Manager op does not create a component, a relationship or a volatility. `DH-CONTRACT-DEADOP` stays at **×2** (`AcknowledgeStaleBasis`, `RecordOperatorNote`) — this edit deletes neither publisher. `DH-CONTRACT-OPCOUNT-MAX` stays **ABSENT**: no contract GAINS an op and `deliveryManager` goes 12 → 11.
  - `validate --slot PlanningAssumptions` and `--slot SdpReview`: **0 errors** (System DOWNGRADES other slots' Errors — this is why they are run separately).
  - Registered-names golden: **134, UNCHANGED.** No Temporal name moves: engine ops register nothing, `designSessionAccess.reconcileBranchFromMain` keeps its name through a parameter change, and `constructionReplanSweep` is still registered by the hand-written `executionKindReplanSweep` entry until Task 11.
  - Frozen names: **15, unchanged.**
  - `Test_Replay_DeliveryHistories`: **8/8 — and this is the load-bearing assertion of the task.** Step 5 changes a registered Activity's PARAMETER LIST (`designSessionAccess.reconcileBranchFromMain`). Confirm none of the eight fixtures schedules it: `for f in internal/manager/delivery/testdata/replay/post-4b1/*.json; do echo -n "$f "; grep -c 'reconcileBranchFromMain' $f; done` — expected **0 for all eight** (none of the eight drives a diverged branch). **If any is non-zero, that fixture must be re-captured in Task 14 and the plan says so there.**
  - `Test_LifecycleShapes`: **10/10** (run alone).
  - webApp: **1228/1228** — the revenue-share deletions touch one fixture literal and no assertion.
  - `systemtests`: builds, after the `DeliveryReplanProject` re-point.
  - [ ] Nine `gen-*-check` all clean; `make lint fix-check sumtype-check vet encapsulation-check derived-plan-check`.

- [ ] **Step 9 (B): Retire the two kinds' DRAFTABILITY — and keep their ordinals.**
  Read **C1** before touching anything.
  - `projectstateaccess.go:9263-9281` — delete the `KindScrubbedRequirements` and `KindStandardCheck` rows from `designKindSlugs`.
  - `:9291-9320` — remove `KindScrubbedRequirements` from `designKindHasCritique`'s `true` arm and move it (with `KindStandardCheck`, already there) into the `false` arm. **`gochecksumtype` requires a case per variant, so the members must be NAMED, not dropped** — which is itself the enforcement of C1.
  - **Delete the dangling LOCKSTEP PIN at `:9302-9305`.** It points at `critiqueCriticFor` in `manager/systemdesign/coauthorartifact.go` — **a file 4b1 DELETED**. A lockstep pin naming a file that does not exist is worse than no pin: the next editor looks for the twin, does not find it, and concludes the pin is stale in the other direction.
  - Delete the three `.claude/commands/*.md` files.
  - Add the retirement statement where a reader will hit it, at `designKindSlugs`:
    ```go
    // scrubbedRequirements and standardCheck carry NO SLUG (stage 4b2, founder ruling): they
    // are old artifacts this project used to have. What is retired is their DRAFTABILITY —
    // the slug, the command file and the critique — and not their identity: the ArtifactKind
    // ordinals 2 and 7 are WIRE VALUES in every committed project.json, and their slots are
    // durable history a git-as-DB exists to keep. This is the same retired-IN-PLACE the
    // 2026-08-30 Phase-1 collapse already applied to these two kinds and to operationalConcepts
    // (see Phase1RequiredKinds), extended from "not required for the seal" to "not draftable
    // at all". Deleting the members would renumber 3..16 and silently re-key every committed
    // slot in every project; deleting the slots would destroy the record this platform
    // produces.
    ```
  - [ ] **Verify first:** `GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestDesignCommandsExistInMethodAssets' -count=1 -v` — this test requires only that a NON-empty slug has a command file, so deleting a slug and its file together is green, and deleting a file while leaving the slug is RED. Run it before and after.
  - [ ] `GOWORK=off go test ./internal/manager/delivery/ -run 'Test_DesignCommands_MatchTheLifecycleData|Test_Phase1RequiredKinds|Test_RoundKindOfTask' -count=1 -v` — all three must stay **GREEN**: no lifecycle names either kind, so no lifecycle-derived pin moves.
  - [ ] `grep -rn 'scrubbed-requirements\|standard-check' server webApp/src .claude uitests` — remaining hits must be only historical prose (a comment recording the retirement) and `GlossaryView.tsx:142`'s cross-slot READ, which C1 keeps.

- [ ] **Step 10: Commit — ONE commit, the whole edit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b2
  git add -A
  git commit -F - <<'MSG'
  feat(model): revenue share leaves the vocabulary, and two old kinds stop being
  draftable

  THE WAVE'S ONE MODEL EDIT. Seven changes ride it, because each regen costs the
  same nine generated surfaces and nine drift gates.

  REVENUE SHARE IS REMOVED AS A CONCEPT. The founder's question — "we will make
  money on operations not revenue share, right?" — checks out against the committed
  objectives: objective 3 commits to "a usage-based fee for operating delivered
  systems on the customer's behalf, while keeping the door open to charging for
  other value it creates — design and construction work, tokens, or consulting."
  Revenue share is not among the named growth paths, so deleting it is consistent
  with the ratified objectives rather than a narrowing of them.

  This closes stage 4b1's sharpest state finding a different way than the earmark
  proposed. Slot 8's terms.revenueShare has been 0 since the 2026-06-09
  merchant-of-record reversal, meaning "no revenue share" — and 0 IS
  RevenueShareUnknown, which billingEngine refuses outright, which is a sufficient
  explanation for slots 11-16 carrying revisions:1 + staleBasis:true ever since.
  The earmark asked for a RevenueShareNone member. But the truthful encoding of a
  concept the business does not have is no concept, not a fourth member: 4b1's
  workaround recorded a NEGOTIATED RATE at zero percent, which is correct behaviour
  resting on a false statement, and the ledger would have said a rate was agreed
  when none was.

  scrubbedRequirements and standardCheck stop being draftable. Their slugs, their
  three command files and the scrubbedRequirements critique go. Their ORDINALS and
  their committed slots STAY, and that is not a hedge: ArtifactKind is an ordinal
  enum and they sit at 2 and 7, so deleting the members renumbers 3..16 and
  silently re-keys every committed slot in every project — and the 2026-08-30
  Phase-1 collapse already ruled these exact kinds retired IN PLACE, with that
  reason written into Phase1RequiredKinds. This extends that ruling from "not
  required for the seal" to "not draftable at all". Deleting the slot data would
  destroy the durable record this platform exists to produce.

  Riding along: the folded session view's $defs (two derived types become one,
  three stages instead of eight and nine); reconcileBranchFromMain's preserve SET;
  deliveryManager 12 -> 11 ops as ReplanProject goes with its stub sweep;
  DeliveryTaskRevisionView.detail, so the M0 spend approval can finally show the
  founder which planning assumptions were defaulted; and two descriptions that have
  been lying since 38fd7f9c.

  Also deleted: the LOCKSTEP PIN at designKindHasCritique, which points at a file
  stage 4b1 removed. A pin naming a file that does not exist is worse than no pin.

  validate --slot System: 43 advisory / 0 errors, unmoved. Registered names 134,
  unchanged — no Temporal name moves here. deliveryManager 12 -> 11, so
  DH-CONTRACT-OPCOUNT-MAX stays absent.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 9: The provider sweep — the Activity Experience mounts NONE of the three cross-slot providers

**The founder's complaint, measured to its root cause, and it is the 4b1 lesson in the UI.** *"Should operational concepts just be the deployment diagram on the architecture page? That is disabled now for some reason but it should not be."* It already is the deployment diagram: `ArchitectureView.tsx:165` reads `useCommittedSlotEnvelope('operationalConcepts')` → `listDeploymentProfiles(opEnvelope)[0]?.profile` → the Deployment toggle is `disabled={deploymentProfile === undefined}` (`:589`). The slot is committed (status 2, 3 revisions) with `deployment{deliveryStyle cloud, containers 2, environments 3, infrastructure 5, bindings 16, settings 18}`. **The envelope is undefined because no provider is mounted.**

**And it is not one provider — it is THREE.** Measured across the whole `webApp/src`:

| Provider | Mounted at | Consumed inside `ArtifactRenderer` by |
|---|---|---|
| `CommittedSlotsProvider` | `McpSystemDesignContainer.tsx:409`, `HomeBase.tsx:440` | `ArchitectureView.tsx:165` (`operationalConcepts`), `GlossaryView.tsx:142-145` (`scrubbedRequirements`, `volatilities`, `coreUseCases`, `system`) |
| `StructureFindingsProvider` | `McpSystemDesignContainer.tsx:407`, `HomeBase.tsx:438` | `ArchitectureView.tsx:146`, `UseCaseCarousel.tsx:69` |
| `DeploymentHealthProvider` | `McpSystemDesignContainer.tsx:408`, `HomeBase.tsx:439` | `ArchitectureView.tsx:175` |

`ActivityExperienceContainer.tsx` renders `ArtifactRenderer` (through `ArtifactPanel.tsx:197`) and mounts **only** `CommentProvider` (`:804`). So on the Activity Experience — **the screen stage 5 built to replace both of those mount points** — the Deployment lens is unavailable, the architecture diagram has no design-health tint, the use-case carousel has no findings, and the Glossary's four cross-slot term-usage joins render nothing. Each degrades silently *by design* (`CommittedSlotsContext.tsx:14-17`: *"Defensive by construction: with no provider mounted … consumers get undefined — the joins render nothing instead of crashing"*), which is precisely why nobody noticed for a wave.

**The container already has everything the three need:** `project` (`:177`), `slots` (`:190`). Only `designHealth` must be added, and `useDesignHealth` already exists (`useDeliveryQueries.ts:174-184`) and is already used by `GatePanel`.

**Files:**
- Modify: `webApp/src/containers/ActivityExperienceContainer.tsx` — the provider stack around the returned screen (`:757` region for `slots`, `:800-805` for the wrap).
- Modify: `webApp/src/hooks/useDeliveryQueries.ts` — nothing, if `useDesignHealth` is already exported; verify.
- Create/modify: `webApp/src/containers/ActivityExperienceContainer.test.tsx` (or the nearest existing container test file) — four node tests.
- Modify: `uitests/tests/preview/activity-renderers.spec.ts` — two preview cases.
- Modify (conditional F): `webApp/src/components/activity/m0CostBasis.ts` + `activityCopy.ts:193` — re-point the M0 notice at `revision.detail`.

**Interfaces consumed:** `DeliveryTaskRevisionView.detail` (Task 7 Step 7F).

- [ ] **Step 1: Write the four failing node tests.**
  ```tsx
  // The Activity Experience renders ArtifactRenderer, and ArtifactRenderer's views read three
  // CROSS-SLOT contexts. None of the three was mounted here: both existing mount points are
  // the retired HomeBase route and the stage-6 MCP widget container. Every consumer degrades
  // to undefined SILENTLY by design, which is why a committed operationalConcepts slot with
  // three environments, five infrastructure entries and sixteen bindings rendered as a
  // DISABLED toggle for a whole wave.
  it('mounts CommittedSlotsProvider so the Deployment lens is available on a committed operationalConcepts slot', …)
  it('mounts StructureFindingsProvider so the architecture diagram carries its design-health tint', …)
  it('mounts DeploymentHealthProvider so the deployment lens can tint an observed node', …)
  it('feeds the providers the SAME project head-state the container already reads', …)   // no second fetch
  ```
  The fourth is not decoration: mounting a provider that fetches its own copy of the head-state is a second read of a 1.17 MB document per render, and the container already holds `project`.
  - [ ] Run: `cd webApp && ASDF_NODEJS_VERSION=lts npm run test -- ActivityExperienceContainer`. Expected: **4 FAIL**.

- [ ] **Step 2: Mount the three, in the same nesting order the two existing mount points use.**
  ```tsx
    // THE THREE CROSS-SLOT PROVIDERS (stage 4b2 Task 9). ArtifactRenderer's views read the
    // project's OTHER committed slots and the live design-health findings through context,
    // because the components layer may not reach src/hooks. Both prior mount points —
    // HomeBase and the MCP widget container — are screens this experience replaced, and the
    // providers did not move with the body. Every consumer degrades to undefined silently, so
    // nothing failed: the Deployment lens simply stayed disabled over a committed slot.
    //
    // Same nesting as the two existing mounts, and fed from the head-state this container
    // ALREADY holds — a provider that fetches its own copy would re-read a 1.17 MB aggregate
    // on every render of a screen that polls.
    <StructureFindingsProvider findings={designHealth?.findings}>
      <DeploymentHealthProvider healthByKey={deploymentHealth}>
        <CommittedSlotsProvider slots={project?.slots}>
          {screen}
        </CommittedSlotsProvider>
      </DeploymentHealthProvider>
    </StructureFindingsProvider>
  ```
  - [ ] **Verify first:** read `HomeBase.tsx:437-451` and `McpSystemDesignContainer.tsx:406-463` and copy their nesting and their prop derivations **exactly**. A different order is a third opinion about a stack that already has two agreeing ones. In particular check how `deploymentHealth` is derived at both sites — if it comes from a hook this container does not call, either call it or pass `undefined` **and say which in a comment**, because a silently-absent overlay is the bug this task is fixing.
  - [ ] The historical (`historical === true`) path wraps in `CommentProvider enabled={false}`; the three providers go **outside** it, so a read-only revision still gets its lenses.
  - [ ] Run the four tests: expected **GREEN**.

- [ ] **Step 3: Sweep for the same gap elsewhere, and record the result either way.**
  ```bash
  cd webApp
  grep -rn 'useContext(' src/components src/containers | grep -v node_modules
  for p in CommittedSlotsProvider StructureFindingsProvider DeploymentHealthProvider CommentProvider AnchorRegistryProvider OpsClientProvider UserProvider ThemeProvider; do
    echo "== $p"; grep -rn "<$p" src
  done
  ```
  Measured for this plan at `bae7681f`, the full context inventory is **ten** (`opsContext`, `AnchorRegistry`, `CommentContext`, `CommittedSlotsContext`, `FocusRailContext`, `scenarioLink`, `DeploymentHealthContext`, `StructureFindingsContext`, `UserContext`, `ThemeContext`) and only five of them expose a Provider component. `AnchorRegistryProvider` is mounted inside `ExperienceChrome.tsx:304`, which the Activity Experience DOES render — so it is fine. **Write the whole table into the commit message**, present and absent alike: a sweep whose negative results are not recorded is a sweep somebody repeats.
  - [ ] If a fourth gap turns up, fix it here. If none does, say "the other two Providers are mounted at or above every consumer; checked by name" in the commit.

- [ ] **Step 4: Two preview cases.** In `uitests/tests/preview/activity-renderers.spec.ts` (currently 2 cases):
  - *"the Deployment toggle is ENABLED on an architecture round whose project carries a committed operationalConcepts slot"* — reuse fixture `activity-experience/architecture-round.json`; **no new fixture state**.
  - *"the Glossary's cross-slot term-usage joins render on the Activity Experience"* — reuse `activity-experience/requirements-backfilled.json`.
  - [ ] **Verify first:** the fixture must actually carry a committed `operationalConcepts` slot. `python3 -c "import json;d=json.load(open('uitests/preview-fixtures/web-client/activity-experience/architecture-round.json'));print([s.get('kind') for s in d.get('project',{}).get('slots',[])])"`. If it does not, ADD the slot to that fixture rather than creating a 24th fixture state — `fixture-schema.mjs` validates it either way.
  - [ ] Run the MANAGED way, never with `UITESTS_PREVIEW_URL` set: `cd uitests && npx playwright test tests/preview/`. Expected: **52 cases over 23 fixture states** (was 50/23).

- [ ] **Step 5 (conditional F): re-point the M0 notice at the attempt `Detail`.** `m0CostBasis.ts`'s committed-slot arm was deleted in the final fix wave (F3) because it could never fire; what remains reads slot 8's absence and is **SILENT for the per-family case, which is the live case on this repo**. Replace the slot-reading proxy with `revision.detail`, and delete `activityCopy.ts:193`'s reference to the already-deleted twin.
  - [ ] Two node tests: the defaulted revision renders its sentence; a revision with no `detail` renders nothing (not an empty bullet).
  - [ ] **If Task 7 Step 7F was dropped, delete this step and leave `m0CostBasis.ts` alone** — and move the silence to Task 17's earmarks, where it already is.

- [ ] **Step 6: Gates and commit.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check && ASDF_NODEJS_VERSION=lts npm run build:mcp
  cd ../uitests && npx playwright test tests/preview/ && ASDF_NODEJS_VERSION=lts npm run lint && ASDF_NODEJS_VERSION=lts npm run typecheck
  ```
  Expected: **npm check 1228 → 1232** (+4 from Step 1; Step 5's two land in the same total if F is in, making it **1234** — state which in the commit); **preview 50 → 52 over 23 fixture states**. Go gates untouched: golden 134 (127 after Tasks 11/13), shapes, replays and `validate` are not reached by an SPA change.
  ```bash
  git add webApp uitests
  git commit -F - <<'MSG'
  fix(webApp): the Activity Experience mounts the three cross-slot providers

  The founder asked why the Deployment diagram is disabled on the architecture
  page. It is disabled because nothing mounts CommittedSlotsProvider on the screen
  that replaced the two screens which did.

  ArchitectureView reads useCommittedSlotEnvelope('operationalConcepts') and
  disables the Deployment toggle when it is undefined. The slot is COMMITTED on
  this repo — status 2, three revisions, three environments, five infrastructure
  entries, sixteen bindings — and the envelope is undefined anyway, because the
  only two mount points are the retired HomeBase route and the stage-6 MCP widget
  container. It is the stage-4b1 lesson arriving in the UI: a provider that lived
  in a deleted body and was not re-mounted in the body that replaced it.

  And it is three, not one. StructureFindingsProvider feeds the architecture
  diagram's design-health tint and the use-case carousel's findings;
  DeploymentHealthProvider feeds the deployment lens' health overlay;
  CommittedSlotsProvider feeds the Deployment lens AND the Glossary's four
  cross-slot term-usage joins. All three degrade to undefined silently — which is
  correct behaviour for a missing provider and is exactly why nobody noticed.

  The whole provider inventory was swept rather than spot-checked, and the negative
  results are here so nobody repeats it: ten contexts, five with Provider
  components. AnchorRegistryProvider is mounted inside ExperienceChrome, which this
  container renders. OpsClientProvider, UserProvider and ThemeProvider are
  app-level. CommentProvider was already here. The three above were not.

  All three are fed from the head-state the container already holds. A provider
  that fetches its own copy is a second read of a 1.17 MB aggregate on a screen
  that polls.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 10: A construction question says a HUMAN will answer it, and names who

**Founder ruling R-F4, and the doctrinal reason matters more than the cost.** `AskTaskQuestions` records questions on the round as `ReviewComment.type = question` and the SPA renders them — that is all. No agent job can answer a construction round's thread: `respondToReviewComment` is **slot-scoped** and is **not registered in the construction job mode** (`cmd/aiarch-state-mcp/tools.go`, `modes: {draft, answer}`), and a construction round has no kind and no slot, so a dispatched answer job would start a session that finds nothing to do.

Writing the command instead would mean a method-assets release, a new job mode, and — the real cost — an agent session answering against a round with no kind and no slot, which is the state `respondToReviewComment`'s slot-scoping refuses, for a reason. **And the doctrinal argument is the one that decides it: an agent answering a question about code it wrote, to clear a thread that gates its own merge, is the autogate hazard in a different costume** — the one 4b1 had to close twice (the open-comment guard, and the critic's send-back under `vibes`).

**Files:**
- Modify: `webApp/src/components/activity/` — the review-body thread renderer that draws a `question` comment (find it in Step 1; it is a component, not a container).
- Modify: `webApp/src/components/activity/activityCopy.ts` — the copy constant.
- Modify: the corresponding `*.test.ts`.
- Modify: `uitests/tests/preview/activity-experience.spec.ts` — reuse an existing fixture that carries a construction question.

**Interfaces:** none. **Zero server change, zero contract change** — the addressee is already on the wire (`AskQuestions` takes it and the thread carries it).

- [ ] **Step 1: Locate the renderer and the addressee, and verify it reaches the screen.**
  ```bash
  cd webApp
  grep -rn "'question'\|type === 'question'\|addressee" src/components/activity src/components/comments | head -20
  grep -rn 'allowAsk\|allowQuestions\|NO_CONSTRUCTION_THREAD_OP' src | head
  ```
  Expected: a comment-type branch in the thread renderer, and an `addressee` (`pm` | `architect`) carried on the ask. **If the addressee is NOT on the rendered thread**, stop and record it: this task then needs a server field and it becomes a Task 7 rider — **which means it must be decided before Task 7 runs.** Measured expectation from 4b1: the ask records the addressee on the round's verdict row (`asked N question(s) of <role>`), so the value exists; where it is *readable from* is what Step 1 establishes.

- [ ] **Step 2: Write the three failing node tests.**
  ```ts
  it('labels a construction question as awaiting a HUMAN answer, naming the addressee', …)
  it('does not label a DESIGN question the same way — a design thread has an answer job', …)
  it('says nothing about an addressee when the ask recorded none', …)
  ```
  The second is the one that makes the label true rather than decorative: a design round's thread IS answerable by an agent (`respondToReviewComment` is slot-scoped and the slot exists), so labelling both the same way would be a new lie replacing an old silence.
  - [ ] Run: expected **3 FAIL**.

- [ ] **Step 3: Add the copy and the branch.** In `activityCopy.ts`:
  ```ts
  /** A construction round has no artifact kind and no slot, so no agent job can answer its
   *  thread — respondToReviewComment is slot-scoped and is not registered in the construction
   *  job mode. The addressee is a person. Ruled 2026-09-28 (founder): an agent answering a
   *  question about code it wrote, to clear a thread gating its own merge, is the autogate
   *  hazard in a different costume. */
  export const CONSTRUCTION_QUESTION_HUMAN = (role: string): string =>
    `Awaiting an answer from ${role} — construction questions are answered by a person.`;
  export const CONSTRUCTION_QUESTION_HUMAN_NO_ROLE =
    'Awaiting a human answer — construction questions are answered by a person.';
  ```
  - [ ] **Verify first:** `grep -n 'export const' src/components/activity/activityCopy.ts | head -20` and match the file's existing naming and export idiom. Do not introduce a second copy convention.
  - [ ] Run the three tests: expected **GREEN**.

- [ ] **Step 4: One preview case.** In `activity-experience.spec.ts`, assert the label on a fixture whose construction round holds a question.
  - [ ] **Verify first:** `grep -ln '"type": *"question"' uitests/preview-fixtures/web-client/activity-experience/*.json`. If none carries one, add it to `service-fork-sent-back.json` (a sent-back construction round is where a question most naturally sits) rather than minting a 24th fixture state.

- [ ] **Step 5: Gates and commit.**
  ```bash
  cd webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && npx playwright test tests/preview/
  ```
  Expected: **npm 1232 → 1235** (+3); **preview 52 → 53 over 23 fixture states** *(state it: Task 9 predicted 52, this adds one; the wave's final preview number is **53**, not the 52 in the ledger's end-state column if this case lands — reconcile in Task 17 and report the real number)*.
  ```bash
  git add webApp uitests
  git commit -F - <<'MSG'
  feat(webApp): a construction question says a person will answer it, and who

  AskTaskQuestions records questions on a construction round and the SPA renders
  them. That is all it does. No agent job can answer one: respondToReviewComment is
  slot-scoped and is not registered in the construction job mode, and a
  construction round has no artifact kind and no slot — so a dispatched answer job
  would open a session that finds nothing to do. The addressee has always been a
  human and the screen has never said so.

  Ruled (founder, 2026-09-28): construction questions are human-answered, label the
  addressee, do not write the command. The cost comparison is not close — writing
  it means a method-assets release, a new job mode, and an agent session answering
  against exactly the state respondToReviewComment's slot-scoping refuses. But the
  argument that decides it is doctrinal: an agent answering a question about code
  it wrote, to clear a thread that gates its own merge, is the autogate hazard in a
  different costume. Stage 4b1 had to close that hazard twice.

  A DESIGN question keeps its own label, and that is what makes this one true
  rather than decorative: a design thread IS answerable by an agent, because the
  slot exists.

  No server change and no contract change. The addressee is already on the wire.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 11: `ReplanSweepWorkflow` dies, and `constructionPumpSweep` joins the frozen list

**A Schedule that fires a stub is worse than no sweep: it reads as coverage.** Measured — 47 lines; `flagVariances` is `return nil` unconditionally (`replansweep.go:45`); the nil-projectID arm returns empty and is unreachable over both transports; and `delivery:replanSweep` fires it **every five minutes** to do nothing. Its contract op went in Task 7 (C3); this task takes the workflow, the Schedule, the registration, the frozen name and the golden entry.

**In the same edit, `constructionPumpSweep` is ADDED to the frozen-names list.** `registered_names_test.go:296-299` carries the earmark verbatim: *"EARMARK: constructionPumpSweep is a Schedule target too and is NOT in this list, which is an omission by the same argument rather than a decision."* A Temporal Schedule holds the workflow TYPE name as live namespace state, so a rename this test did not catch would leave the Schedule firing into a name no worker serves — a dead sweep that reports no error at all.

**Files:**
- Delete: `server/internal/manager/delivery/replansweep.go` (47 lines).
- Modify: `server/internal/manager/delivery/deliverymanager.go` — `RunReplanSweep` (`:6114-6146`), `replanSweepWorkflowID` (`:7381-7386`), `deliveryManager.ReplanProject` (`:10788-10793`), `scheduleIDReplanSweep`/`replanSweepIntervalSecs` (`:8551-8554`), the `registerSchedules` entry (`:12979-12983`), the workflow registration (`:8750`), `executionKindReplanSweep` (`:13200`), the `ReplanSweepResult`/`FlaggedVariance` types if they lose every caller (**`FlaggedVariance` does NOT — `deliveryactivity.go:1224` sets `tc.State.variance`; keep it**).
- Modify: `server/internal/registered_names_test.go` — golden (`:117`) and frozen list (`:308` out, `constructionPumpSweep` in), plus the argument in the frozen list's doc comment.
- Modify: `server/internal/manager/delivery/manager_test.go` — the `Test_ReplanSweep_*` / `Test_RunReplanSweep_*` blocks.
- Modify: `docs/bugs/2026-09-24-stage3-rail-earmarks.md` — the drain sequence gains the Schedule deletion (Task 17 does the full pass; this task adds the one line it owns).

**Interfaces:** `FlaggedVariance` SURVIVES (live producer in the child's variance loop). `ReplanSweepResult` is deleted with the op.

- [ ] **Step 1: Prove the sweep surfaces nothing, before deleting it.**
  ```go
  // Test_ReplanSweep_SurfacesNothingForAnyProject is the deletion's argument, executable and
  // then deleted with its subject. flagVariances returns nil unconditionally, so the sweep is
  // a Schedule firing every five minutes to produce an empty result — which is worse than no
  // sweep, because an operator reading the Schedule list sees variance coverage.
  func Test_ReplanSweep_SurfacesNothingForAnyProject(t *testing.T) { … }
  ```
  Drive it over a project seeded with an over-threshold variance and assert `FlaggedVariances` is empty. Expected: **GREEN**, which is the finding.
  - [ ] Record the output in the commit message. Then delete the test with the workflow.

- [ ] **Step 2: Delete, in dependency order.** Workflow file → registration → Schedule const + `registerSchedules` entry → `executionKindReplanSweep` → `RunReplanSweep` → `replanSweepWorkflowID` → `deliveryManager.ReplanProject` → `ReplanSweepResult`.
  - [ ] **Verify first:** `grep -rn 'ReplanSweep\|replanSweep\|ReplanProject\|flagVariances' server webApp/src systemtests uitests | grep -v FlaggedVariance` must be **zero** afterwards, in code AND comments — except the drain note, which must GAIN a line. `FlaggedVariance` must still resolve (`deliveryactivity.go:1224`).
  - [ ] The two prose references at `deliverymanager.go:8538-8543` (the Schedule-id migration note naming `construction:replanSweep`) are HISTORICAL and describe a drain step that has not run. **Keep them and add the 4b2 line**, do not delete a drain instruction because its workflow is gone — the old Schedule still exists in the live namespace and still needs `temporal schedule delete`.

- [ ] **Step 3: Move the two frozen names and rewrite the argument.**
  ```go
  // STAGE 4b2 REMOVED constructionReplanSweep AND ADDED constructionPumpSweep, and the two
  // moves have the same justification read in opposite directions.
  //
  // This list exists so an externally-held workflow TYPE name cannot vanish silently: a
  // client or a SCHEDULE outside this codebase holds the name, and a rename or a deletion
  // leaves it starting a type no worker serves. constructionReplanSweep leaves it because the
  // workflow is GONE and its Schedule is deleted in the same drain that this wave already
  // requires — there is no external starter left to protect. constructionPumpSweep joins it
  // because its Schedule (delivery:pumpSweep, 30s) has held its type name as live namespace
  // state since stage 4a and this list never said so; that was an omission by this list's own
  // argument, earmarked at stage 4b1 and closed here.
  //
  // The count is 15 - 1 + 1 = 15.
  ```
  - [ ] Regenerate the golden literal **from the test's own printed diff**, never by hand. Run `GOWORK=off go test ./internal/ -run TestRegisteredTemporalNamesGolden -v` and paste the diff's removal.
  - [ ] Expected: golden **134 → 133**; frozen **15 → 15**.

- [ ] **Step 4: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/ -run 'TestRegisteredTemporalNames' -count=1 -v
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint vet sumtype-check encapsulation-check
  cd ../systemtests && GOWORK=off go build ./...
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check
  ```
  Expected: **golden 133, frozen 15, shapes 10/10, replay 8/8, validate 43/0, npm unchanged at this task's baseline, preview unchanged.** `webApp`'s `useReplanProject` was deleted with the generated op in Task 7, and it had **zero consumers outside its own file** (measured) — so no node test moves here either. Hand-written delivery lines fall by **~100** (47 in the file, ~55 in the façade).
  ```bash
  git add server
  git commit -F - <<'MSG'
  refactor(delivery): the replan sweep is deleted, and the pump sweep is frozen

  ReplanSweepWorkflow was 47 lines, and flagVariances returned nil
  unconditionally. Its nil-projectID arm returned an empty result and was
  unreachable over both transports. A Temporal Schedule fired it every five
  minutes to do nothing — which is worse than having no sweep, because an operator
  reading the Schedule list sees variance coverage that does not exist. The test
  that proved it green against a project seeded with an over-threshold variance is
  in this commit's history and is deleted with its subject.

  The façade op went with the wave's one model edit; this takes the workflow, the
  Schedule, the registration, the id helper and the frozen name. FlaggedVariance
  STAYS — the generic child's per-task variance loop is its live producer, and the
  type was never the problem.

  In the same edit, constructionPumpSweep JOINS the frozen list. Stage 4b1
  earmarked the omission in the test's own comment: a Schedule holds the workflow
  TYPE name as live namespace state, so delivery:pumpSweep has been an external
  starter since stage 4a and this list never said so. A rename the list did not
  catch would leave the Schedule firing into a name no worker serves — a dead sweep
  that reports no error at all, which is the exact failure the list exists to
  prevent.

  15 - 1 + 1 = 15. Registered names 134 -> 133.

  The old construction:replanSweep Schedule still exists in the live namespace and
  still needs an explicit `temporal schedule delete` in the drain. The drain note
  says so and this commit does not remove that instruction just because its
  workflow is gone.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 12: THE PUMP — `child.Get` is deleted, the lease is granted, the tick reconciles

**The wave's headline, and it goes LAST among the code changes for the reason stated in the Architecture line: the pump is this wave's deleted body, and landing it last means the guards it must re-assert are already in their final shape.** Task 1's census is the list it is judged against.

**What it does, in one paragraph.** `PumpNextActivityWorkflow` stops selecting one activity and blocking on it. It selects **every** eligible activity, starts a child for each (starts are already idempotent on the child id — `pumpsweep.go` relies on exactly that), records the started set, and parks on a `workflow.Selector` over {finish signals, lease requests, the pause channel, a reconcile timer}. It grants **at most one main-write lease per project at a time** (R1's invariant), keyed by activity id, validated against its own started set AND the committed plan (R-A). The merge stays in the child. Every 30 s the tick **reconciles** against Temporal's execution state, which is authoritative over any signal (R2). ContinueAsNew happens **only at zero held lease and zero unanswered request**, after a non-blocking drain of every channel whose contents ride the input (R-C).

**Measured payoff:** deleting `child.Get` alone collapses **27 whole-aggregate reads to 1** per drain on this repo's 30-activity plan (the pump `ContinueAsNew`s once per dispatched activity, and each run does one `readProject` of a 1,166,110-byte / 32,774-line document), and it is the difference between a serial cascade and the parallel children spec §9's acceptance asks for.

**Files:**
- Modify: `server/internal/manager/delivery/pumpnextactivity.go` — the whole body (424 lines in, expect ~520 out).
- Modify: `server/internal/manager/delivery/deliveryactivity.go` — `finalizeWalk` (`:3304-3330`) requests the lease before `runWalkMerge` and releases it after `commitDesignArtifacts`; the walk's terminal path signals `activityFinished`; the child's signal router (`routeSignals`, `:1993`) gains the grant channel.
- Modify: `server/internal/manager/delivery/deliverymanager.go` — the signal-name consts (`:13171-13190`), `awaitDispatchDecision` (`:5995-6046`) and its four budgets (`:5976`, `:5982`, `:5989`, `:6054`), `PumpResult` construction at `:6016`.
- Modify: `server/internal/manager/delivery/manager_test.go` — the shape case, the `Test_Pump*` family, Task 1's two new pins.
- Modify: `.aiarch/state/project.json` — **NO. `PumpResult.activityIds` rides Task 7.** *(See Step 10 — this is the one deviation from the controller's ordering and it is deliberate.)*

**Interfaces produced:**
- `const signalActivityLeaseRequested = "activityLeaseRequested"` — child → pump, payload `activityLeaseRequest{ActivityID ActivityID}`.
- `const signalActivityLeaseGranted = "activityLeaseGranted"` — pump → child, payload `activityLeaseGrant{ActivityID ActivityID; Epoch int64}`.
- `const signalActivityFinished = "activityFinished"` — child → pump, payload `activityFinishedSignal{ActivityID ActivityID; Outcome projectstate.AttemptOutcome}`.
- `type pumpInput struct { ProjectID ProjectID; OperatorDriven bool; Started []ActivityID; LeaseHolder *ActivityID; LeaseEpoch int64; LeaseGrantedAt *time.Time; Carried []pumpCarriedSignal }` — the CAN payload, bounded by the plan's activity count.
- `func (wf *csWorkflows) pumpReconcile(ctx workflow.Context, st *pumpState) error` — the authoritative tick.
- `func (wf *csWorkflows) requestMainWriteLease(ctx workflow.Context, in deliveryActivityInput, ws *walkState) (int64, error)` — the child side; returns the epoch.
- `PumpResult{ Dispatched bool; ActivityID *ActivityID; ActivityIDs []ActivityID }` (Task 7's `$defs`).

- [ ] **Step 1: Re-read Task 1's census and pin the plan to it.** Open `docs/bugs/2026-09-28-pump-guard-census.md` and, for each of G-P1…G-P11, write ONE line in this task's working notes saying which Step re-asserts it. **A guard with no Step is the task's own blocker.** The mapping this plan expects: G-P1→Step 4, G-P2→Step 5, G-P3→Step 4, G-P4→Step 4, G-P5→Step 5, G-P6→Step 7, G-P7→Step 5, G-P8→Step 4, G-P9→Step 8, G-P10→Step 8, G-P11→unchanged (the helper is untouched).

- [ ] **Step 2: Write the shape case FIRST, and make it red.**
  ```go
  // "continue-as-new-loses-no-signal" is the eleventh shape case and it exists because this
  // wave's riskiest failure is the only one that fails SILENTLY. A lease-release or a finish
  // signal buffered on a run that continues-as-new is DISCARDED by the SDK; the next run has
  // no record of it; the project's one pump then waits out a lease deadline for an activity
  // that already finished, or never re-selects an activity whose finish was the thing lost.
  // pump-singular-per-project means there is no second pump to cover it — the blast radius is
  // that the project stops.
  //
  // The case buffers BOTH a finish and a lease request across a CAN boundary and asserts both
  // survive into the next run's input. It is MUTATION-CHECKED: remove the drain-and-carry and
  // this case must go red, which is the discipline every case in this table was written with.
  {name: "continue-as-new-loses-no-signal", …}
  ```
  - [ ] Add it to `Test_LifecycleShapes`' table, marked skipped as `shapeFailsUntilTheLease` exactly as 4b1's three new cases were, and **flip it green by hand once to prove the oracle is not vacuous before implementing**. An assertion that passes against the current blocking pump is an assertion about nothing.
  - [ ] Run alone: `GOWORK=off go test ./internal/manager/delivery/ -run Test_LifecycleShapes -count=1 -v`. Expected: **10 pass, 1 skip**.

- [ ] **Step 3: The pump's new state, and its bound.**
  ```go
  // pumpState is what a run of the pump knows. It is carried across ContinueAsNew, so every
  // member must be JSON-serialisable and BOUNDED — the unbounded-history property the old
  // one-field pumpInput had for free is now a thing this struct owes.
  //
  // THE BOUND: Started is bounded by the committed activity list (30 on this repo), Carried by
  // what one run can buffer between two workflow tasks, and the lease is one activity. There
  // is no per-EVENT accumulation anywhere in it, which is the property that matters.
  type pumpState struct {
  	Started    []ActivityID                 // dispatched by THIS pump and not yet known-finished
  	LeaseHolder *ActivityID                 // at most one, ever — the invariant
  	LeaseEpoch  int64                       // bumped on every grant; a child refuses a stale grant
  	LeaseGrantedAt *time.Time               // workflow.Now at grant, for the deadline check
  	Carried    []pumpCarriedSignal          // drained immediately before CAN, replayed at run start
  }
  ```
  - [ ] Write `Test_Pump_ContinueAsNewPayloadIsBoundedByThePlan` (Task 1 Step 3 created the name) to assert `len(st.Started) <= len(committed activity list)`.

- [ ] **Step 4: Start EVERY eligible activity, and keep G-P1/G-P3/G-P4/G-P8.** Replace the single `nextEligible` + `startActivityChild` with a loop that drains the frontier:
  ```go
  	// EVERY ELIGIBLE ACTIVITY, NOT ONE (stage 4b2). nextEligibleActivity is pure over the
  	// project and returns the FIRST eligible activity in declaration order; call it in a loop
  	// against a set of already-started ids, so one pass starts the whole frontier. Starts are
  	// idempotent on the child id — pumpsweep.go has relied on exactly that since stage 4a —
  	// so a redundant tick collapses instead of forking a second walk.
  	//
  	// THE DECISION IS STILL RECORDED BEFORE ANY BLOCK (guard G-P8): the façade's synchronous
  	// ExecuteNextActivity reads queryPumpDispatch, and a decision recorded after the park is a
  	// Begin that hangs for the length of a cascade.
  ```
  - [ ] `pumpDispatch` grows `ActivityIDs []ActivityID`; `dispatch` is assigned **once**, after the whole frontier is started and before the selector.
  - [ ] `SetQueryHandler` (G-P1) stays where it is, first, before anything that can block.
  - [ ] The `isReadNotFound` quiet return (G-P3) and the recorded-pause gate (G-P4, `pumpHonorsRecordedPause`) are **unchanged and still before `nextEligible`**.
  - [ ] **Verify first:** read `nextEligibleActivity`'s scan (`deliverymanager.go:7756-7800`) and confirm `isActivityDispatchable`'s `PumpWroteRow` arm — an activity this pump already started has a row, so the loop terminates without a manual exclusion set. **If it does not** (a start whose `OpenActivity` has not landed yet), the loop MUST carry its own started set, and that is the honest reason for `pumpState.Started` to exist independent of the lease.

- [ ] **Step 5: Keep G-P2, G-P5 and G-P7 verbatim.** The run-start pause (`pumpPausedAtRunStart`), the `verdictBlocked` sticky-failure write through `applyRecovering`, and the pre-dispatch pause check all stay, in place, unchanged. **`verdictBlocked` is now evaluated per activity in the frontier loop** — a blocked activity records its failure and the loop CONTINUES to the next, which is a behaviour improvement the old serial pump could not have (it returned).
  - [ ] Write `Test_Pump_OneBlockedActivityDoesNotStopTheFrontier`.

- [ ] **Step 6: The lease. Pump side.**
  ```go
  	// THE INVARIANT: at most one activity of a project holds the main-write lease at a time.
  	//
  	// WHY A LEASE AND NOT "LET THEM RACE". applyRecovering's bound is
  	// maxMutateConflictAttempts, and stage 4b1 MEASURED that bound being exhausted by ONE
  	// coroutine's own writes with no sibling present — the 65-override storm,
  	// MutateConflictExhausted, an activity killed by an operator pressing a button. Under N
  	// parallel children each landing 1-5 slot commits plus a ref-CAS against main, exhaustion
  	// is not a hypothesis.
  	//
  	// WHAT THE LEASE DOES NOT REPLACE: the per-activity CAS and applyMutationOnBranchFiles's
  	// dedup + version guard + ref-CAS stay exactly as they are, and they are what serialise
  	// per ROW. Row-level and main-level are TWO mechanisms; conflating them is how an earlier
  	// ruling put the merge in the pump.
  	//
  	// WHAT THE PUMP NEVER LEARNS: what a merge is. It grants and revokes an opaque lease keyed
  	// by activity id. The child does its own merge, its own variance loop, its own human hold
  	// and its own slot commits while holding it — which keeps the gate, the inbox, the
  	// credential and the branch with the only thing that has them.
  	//
  	// VALIDATION, because Temporal does not authenticate a signaler: an id is granted only if
  	// it is in this pump's Started set AND in the committed activity list this run already
  	// read. An id in neither is LOGGED and DROPPED, never granted.
  	//
  	// EPOCH: bumped on every grant. A child that was revoked and re-granted underneath itself
  	// must be able to refuse a stale grant, and the epoch is how.
  ```
  - [ ] Grant delivery uses `wf.Acts.MessageBusDeliverSignal` to `deliveryActivityWorkflowID(projectID, activityID)` — the same generated invoker `relayPauseToPump` uses, with `isSignalTargetNotFound` tolerated (R-B). **No new RA producer, no contract change to `messageBus`.**
  - [ ] **The lease deadline does NOT blindly revoke.** On expiry the pump asks Temporal whether the holder is alive (the same Describe `pumpRunClosed` uses, through an Activity — a workflow cannot Describe directly); a live holder gets a **renewal**, a closed one is revoked and the lease re-granted at `epoch+1`. **A live holder that is merely slow must never lose its lease mid-merge**, which is what the liveness check buys and a naive timeout does not.
  - [ ] Three tests: `Test_Pump_GrantsAtMostOneLease`, `Test_Pump_DropsALeaseRequestForAnUnknownActivity`, `Test_Pump_RenewsALiveHoldersExpiredLease`.

- [ ] **Step 7: The reconcile tick replaces `child.Get`, and G-P6 survives.**
  ```go
  	// THE TICK IS A RECONCILE, NOT A POLL, AND NO FACT COMES FROM A SIGNAL ALONE.
  	//
  	// The finish signal is an OPTIMISATION OVER THIS TICK and never the source of truth. Every
  	// 30 seconds, for each started-and-unfinished activity, the pump asks Temporal whether the
  	// execution is CLOSED and treats a closed execution as finished, releasing any lease it
  	// held. That single rule answers three failure modes at once: a lost signal costs at most
  	// one tick; a child that dies between finishing and signalling is found closed; a child
  	// that dies holding the lease is found closed and the lease is re-granted at epoch+1. And
  	// the terminal is independently in the ROW (RecordActivityOutcome), so head state and
  	// Temporal agree without reference to any signal at all.
  	//
  	// QUIESCENCE STILL ENDS THE PUMP (guard G-P6). With nothing started, no lease held, no
  	// request outstanding and nothing eligible, the run RETURNS — it does not continue-as-new.
  	// The cascade's own drain-to-quiet is what ends the pump, and a continue here is an
  	// infinite pump.
  ```
  - [ ] `Test_Pump_ReconcileReleasesALeaseHeldByAClosedExecution`, `Test_Pump_QuiescesWhenNothingIsStartedAndNothingIsEligible`.

- [ ] **Step 8: THE CAN RULE — the wave's riskiest ten lines. G-P9 and G-P10.**
  ```go
  	// CONTINUE-AS-NEW ONLY AT ZERO HELD LEASE AND ZERO UNANSWERED REQUEST, AND DRAIN FIRST.
  	//
  	// This file already documents the trap, at the pause check below: "a signal still buffered
  	// on a run that ends in ContinueAsNew is NOT carried into the next run". That was true for
  	// one channel and it is now true for four. A finish or a release lost across the boundary
  	// is not an error anywhere — the pump simply waits out a lease deadline for an activity
  	// that finished, or never re-selects an activity whose finish was the thing lost — and
  	// pump-singular-per-project means there is no second pump to cover it. The blast radius is
  	// the project stops.
  	//
  	// So, three rules and they are one mechanism:
  	//   1. CAN only when LeaseHolder == nil and no lease request is unanswered — the child's
  	//      own shouldContinueAsNew-at-inflight==0 discipline, one level up;
  	//   2. immediately before CAN, ReceiveAsync every channel until empty and put what is
  	//      found in pumpState.Carried;
  	//   3. at run start, replay Carried BEFORE selecting, so a carried finish is applied
  	//      before the frontier is computed.
  	// The reconcile tick is the backstop for all three, which is why losing one costs a tick
  	// rather than a project.
  ```
  - [ ] **MUTATION CHECK, and it is a required step, not a suggestion:** delete rule 2, run the `continue-as-new-loses-no-signal` case, **watch it go red**, restore rule 2, watch it go green. Paste both outputs into the commit message. A mitigation nobody proved can fail is a comment.

- [ ] **Step 9: Shrink `awaitDispatchDecision`; do not delete the poll and do not add `stillDeciding`.**
  ```go
  // pumpDispatchWaitBudget bounds the poll. It was 30s because the pump recorded its decision
  // and then PARKED IN child.Get, so a slow project could take most of a tick to reach the
  // decision point. A non-blocking pump reaches it within a workflow task of start, so the
  // budget drops to 5s — still an order of magnitude above the observed path, and low enough
  // that a genuinely wedged pump is reported while a human is still looking.
  //
  // THE POLL STAYS. The start is still ASYNCHRONOUS: ExecuteWorkflow returns before the
  // workflow's first task runs, so there is still a window in which the Query has no answer.
  // And PumpResult does NOT grow a stillDeciding outcome — the OPEN note at this const asked
  // for a contract change to distinguish a slow pump from a failed one, and the new shape
  // makes that distinction uninteresting: a pump that has not decided within 5s of start is
  // failed.
  var pumpDispatchWaitBudget = 5 * time.Second
  ```
  - [ ] `pumpQueryFailureBudget` 10s → 5s, `pumpClosureCheckInterval` 250ms unchanged, `pumpRPCTimeout` 5s unchanged. **Each new value gets its reason in a comment.**
  - [ ] `PumpResult` is synthesised from `pumpDispatch` at `:6016` and now carries `ActivityIDs`; `ActivityID` is set to `ActivityIDs[0]` where non-empty, with the deprecation line at the site.

- [ ] **Step 10: `PumpResult.activityIds` — the one ordering deviation, stated.** The controller's order puts the pump at item 10 and the model edit at item 6, and `activityIds` is an additive `$defs` member on `deliveryManager.PumpResult`. **This plan puts it in Task 7 (Step 7, alongside E/F/G) and NOT here**, because the wave has exactly ONE regen and a late additive member is a second one. So by the time this task runs, `PumpResult.ActivityIDs` **already exists and is empty**; this step fills it.
  - [ ] **If Task 7 did not carry it** (a planning slip), do NOT hand-edit `project.json` here. Stop, and take it as its own model commit with its own full loop — R-H's discipline is worth more than one member's convenience.
  - [ ] `webApp/src/components/construction/contractCode.test.ts` pins the contract SIGNATURE STRING `'ExecuteNextActivity(projectID: ProjectID, tickID: string) → (PumpResult, fwm.Error)'` in five places — **the signature does not change** (the return TYPE is the same name), so those five must stay green. Verify rather than assume: `grep -n 'PumpResult' webApp/src/components/construction/contractCode.test.ts`.

- [ ] **Step 11: The child side.** In `finalizeWalk` (`deliveryactivity.go:3304`), request the lease **before** `runWalkMerge` and release it after `commitDesignArtifacts`; signal `activityFinished` on every terminal path including `failWalk`.
  ```go
  	// THE LEASE IS TAKEN HERE, AND HERE IS WHY HERE. Everything above this line writes to the
  	// ACTIVITY BRANCH or to the activity's own row, and both are already serialised — the row
  	// by its CAS, the branch by being this activity's alone. Everything below writes MAIN:
  	// runWalkMerge's merge, finalizeActivity's mergeAndRecord, and commitDesignArtifacts' N
  	// slot commits. So the lease brackets exactly the main-writing tail and nothing else, and
  	// a walk that never reaches its tail never asks for one.
  	//
  	// The epoch is checked at every step of the tail: a grant the pump revoked and re-granted
  	// underneath us is STALE, and acting on it would be the second writer the lease exists to
  	// prevent.
  ```
  - [ ] `routeSignals` (`:1993`) gains the grant channel. **It must stay non-blocking** — `SendAsync` with the `pending` fallback, exactly as the four existing channels do; a `Send` parked on a full inbox is the wedge `full-inbox-does-not-wedge-the-router` was written for.
  - [ ] Release on **every** exit from the tail, including the error paths — a lease leaked by a returning error is a project that stops until the deadline. Use a deferred release keyed on the epoch.
  - [ ] Flip the `continue-as-new-loses-no-signal` case off `shapeFailsUntilTheLease`.

- [ ] **Step 12: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/manager/delivery/ -run Test_LifecycleShapes -count=1 -v      # ALONE — timing-coupled
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Pump|Test_PumpGuard|Test_Replay' -count=1 -v 2>&1 | tail -40
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint fix-check sumtype-check vet encapsulation-check
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check
  ```
  Expected: **`Test_LifecycleShapes` 10/10 → 11/11**; `Test_Replay_DeliveryHistories` **8/8** (the eight are `deliveryActivity` histories and this task changes the child's TAIL — **if any goes red, the lease request was added to a path the fixtures traverse, and the fixture must be re-captured in Task 14 rather than the guard relaxed**); golden **133 unchanged** (no new workflow type, no new activity name — `messageBus.deliverSignal` is already registered); frozen **15 unchanged**; `validate` **43/0**; `npm run check` unchanged; preview unchanged. **Hand-written delivery lines: expect +80 to +120** — this is the one task in the wave that ADDS, and Task 17's measurement is where the net is judged.
  ```bash
  git add server
  git commit -F - <<'MSG'
  feat(pump): the pump grants a lease instead of blocking on a child

  child.Get is gone. The pump starts EVERY eligible activity, records the started
  set, and parks on a selector over finish signals, lease requests, the pause
  channel and a reconcile timer. Deleting that one blocking call collapses 27 whole
  1.17 MB project.json reads to 1 on this repo's plan, because the pump
  continues-as-new once per DISPATCHED activity today and re-reads the aggregate
  every time.

  THE PUMP DOES NOT DO THE MERGE, and the code stage 4b1 shipped is what settles
  it. The child's tail holds three distinct main-writing steps and they are not one
  thing: runWalkMerge with its own variance loop, its own escalation inbox and a
  HUMAN approval hold; mergeAndRecord with its CI precondition and its approve-time
  credential re-mint; and N slot commits. Moving the first into the pump moves a
  human gate, a variance loop, a signal inbox and a gate ledger into it — the
  god-workflow arriving by the back door, one wave after seven workflows were
  deleted to avoid exactly that.

  So the pump serialises ADMISSION, not work. At most one activity of a project
  holds the main-write lease at a time; the child takes it immediately before its
  merge tail and releases it after its last slot commit; the pump never learns what
  a merge is. Row-level serialisation is unchanged and is a different mechanism:
  the per-activity CAS and applyMutationOnBranchFiles' version guard and ref-CAS
  still serialise per ROW, and conflating the two is how an earlier ruling put the
  merge in the pump.

  NO FACT COMES FROM A SIGNAL ALONE. The 30-second tick is a RECONCILE: for each
  started-and-unfinished activity it asks Temporal whether the execution is closed
  and treats a closed execution as finished. One rule answers three failure modes —
  a lost signal costs one tick, a child that dies before signalling is found
  closed, and a child that dies holding the lease is found closed and the lease is
  re-granted at epoch+1. A live holder that is merely SLOW is renewed, never
  revoked: losing a lease mid-merge is what a naive timeout buys and a liveness
  check does not.

  The riskiest ten lines in the wave are the continue-as-new drain-and-carry, and
  they are the only part of it that fails silently. This file already documented the
  trap for one channel; there are four now. CAN only at zero held lease and zero
  unanswered request, drain every channel non-blocking immediately before it, carry
  what is found, replay it before the frontier is computed. The
  continue-as-new-loses-no-signal shape case buffers a finish AND a lease request
  across the boundary, and it was mutation-checked by removing the carry and
  watching it go red.

  awaitDispatchDecision shrinks rather than grows: 30s becomes 5s because a
  non-blocking pump decides within a workflow task of start. The poll STAYS — the
  start is still asynchronous — and PumpResult does NOT gain a stillDeciding
  outcome, because a pump that has not decided within five seconds is failed.

  No GetVersion fence. The drain this wave already requires covers 3 + 4a + 4b1 +
  4b2, no {p}:nextActivity execution survives it, and a marker whose other arm
  keeps child.Get alive keeps alive the thing being deleted.

  Guards re-asserted against docs/bugs/2026-09-28-pump-guard-census.md, row by row.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 13: `gitActivityStatusAccess` is deleted

**Free, and the only one of the three deprecated facets that is (R6).** Measured across the whole server, excluding `_test.go` and `*.gen.go`: `RecordActivityBranchOpened`, `RecordActivityCIObserved`, `RecordActivityArchApproved`, `RecordActivityMerged`, `RecordActivityStarted`, `RecordActivityCompleted` — **0, 0, 0, 0, 0, 0 production callers.** Nothing is re-homed. The other two facets leave five homeless verbs and `activityExecutionAccess` is at exactly 12, so they wait for the `projectCatalogAccess` split.

**Files:**
- Modify: `.aiarch/state/project.json` — **NO.** *(Contract deletion rides… nothing. See Step 1: this is the one place the plan accepts a SECOND `project.json` touch, and the reason is stated there.)*
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — `GitConstructionPorts` (`:1639-1646`), `NewGitLocalGitActivityStatusAccess` (`:1665`), `NewGitHubGitActivityStatusAccess` (`:1685`), the facet impl.
- Modify: `server/cmd/server/hooks.go` — `GitActivityStatusAccessGitHubArgs` (`:893`), `GitActivityStatusAccessGitLocalArgs` (`:917`), `FinalizeGitActivityStatusAccess` (`:1079`) and the three doc comments (`:42`, `:884`, `:1071`).
- Modify: `server/cmd/appgen/main.go:189-215` — the two variant-table entries and the comment at `:215` that names them.
- Modify: `server/cmd/aiarch-state-mcp/rawexec.go:86-95`.
- Modify: `server/cmd/backfill-attempts/main.go:715` — the doc comment.
- Modify: `server/internal/registered_names_test.go` — six golden entries.
- Delete (regenerated away): the facet's `contract.gen.go` block, `fake` entries, `toolcatalog.gen.go` entries, and the six `activities.gen.go`/`invokers.gen.go`/`worker.gen.go` registrations.

**Interfaces:** none produced. Six Temporal activity names REMOVED.

- [ ] **Step 1: Read this first — why this is a second `project.json` touch, and why that is accepted.** R-H's discipline is ONE edit and ONE regen, and this task breaks it. The alternative — folding the facet deletion into Task 7 — was considered and rejected on a measurement: Task 7 is already a seven-change commit touching four contracts, two slots, three command files and twenty preview fixtures, and adding a **contract deletion that removes six registered Temporal names** to it would put a golden change inside a commit whose golden prediction is "unchanged", which is the exact condition under which a dropped registration hides. **So: two edits, and the second one is a pure deletion whose entire observable signature is `-6` on the golden.** That is a number a reviewer can check in one line, and it is worth one extra regen to have it stand alone.
  - [ ] Confirm the count before starting: `grep -c 'gitActivityStatusAccess\.' server/internal/registered_names_test.go` — expected **6**.

- [ ] **Step 2: Re-verify zero callers, per op, and record the output.**
  ```bash
  cd .../server
  for op in RecordActivityBranchOpened RecordActivityCIObserved RecordActivityArchApproved \
            RecordActivityMerged RecordActivityStarted RecordActivityCompleted; do
    echo -n "$op: "
    grep -rn "\.$op(" --include='*.go' . | grep -v '_test.go' | grep -v '\.gen\.go' | wc -l
  done
  ```
  Expected: **0 for all six.** A non-zero means the facet is not free and this task stops and reports — it does NOT re-home a verb, because re-homing is what R6 measured as blocked.
  - [ ] `grep -rn 'RecordActivityStarted\|RecordActivityCompleted' --include='*.go' . | grep -v gitActivityStatus | head` — **watch for a NAME COLLISION**: `activityExecutionAccess` has no ops by these names (its twelve are listed in R6), but `constructionTransitionAccess.RecordActivityExited` is adjacent in spirit. Confirm the grep hits are the facet's own, not a survivor's.

- [ ] **Step 3: Delete the contract entry, run the loop, delete the wiring.**
  - Remove `.serviceContracts.gitActivityStatusAccess` entirely.
  - Run the full self-amendment loop (Global Constraints) with `--slot System`.
  - Then delete the hand-written wiring in the order: `hooks.go` → `appgen/main.go` → `rawexec.go` → `projectstateaccess.go`'s constructors and `GitConstructionPorts` → `backfill-attempts`' comment.
  - [ ] **`GitConstructionPorts` returns `(ConstructionTransitionAccess, GitActivityStatusAccess, bool)`** — it loses its second return value and every caller moves with it. `grep -rn 'GitConstructionPorts' --include='*.go' .` before touching it.
  - [ ] **Verify first:** `grep -rn 'GitActivityStatus\|gitActivityStatusAccess' server/ --include='*.go' --include='*.json'` must be **zero** except historical prose in an earmark file.

- [ ] **Step 4: The golden, from the test's own output.**
  ```bash
  GOWORK=off go test ./internal/ -run TestRegisteredTemporalNamesGolden -v 2>&1 | head -30
  ```
  Paste the six removals into the literal. Expected: **133 → 127.** Frozen: **15, unchanged** (none of the six is a workflow name).

- [ ] **Step 5: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint fix-check sumtype-check vet encapsulation-check derived-plan-check
  GOWORK=off make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-lifecycles-check
  cd ../systemtests && GOWORK=off go build ./...
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check
  ```
  Expected: **golden 127, frozen 15, shapes 11/11, replay 8/8, validate 43 advisory / 0 errors.** `DH-CONTRACT-DEADOP` stays at **×2** — this deletes neither `AcknowledgeStaleBasis`'s nor `RecordOperatorNote`'s second publisher (the latter is `constructionTransitionAccess`, which survives). `DH-CARD-RA-RESOURCES` counts RA **components**, not facets, so 18 is unmoved. `npm run check` unchanged (no SPA surface — the facet is an RA, never exposed).
  ```bash
  git add -A
  git commit -F - <<'MSG'
  refactor(projectstate): gitActivityStatusAccess is deleted

  Six ops, zero production callers on every one of them, measured per op with
  _test.go and *.gen.go excluded. Deprecated in place at stage 3 task 3 with the
  note "superseded by activityExecutionAccess, deleted after the stage-4 drain, NOT
  now" — this is after, and the drain this wave requires is the one it meant.

  It is the ONLY one of the three deprecated facets that is free. Deleting
  constructionTransitionAccess and designSessionAccess leaves five verbs with no
  home — RecordOperatorPaused is project-scoped, RecordChangeReviewed has no
  equivalent, and ReadProjectOnBranch / ReconcileBranchFromMain /
  SeedReviewCommentsOnBranch are branch mechanics the facet's own note says are
  "not superseded at all" — and activityExecutionAccess sits at exactly 12 ops,
  App-C's ceiling, which designhealth's own test pins ABSENT on the committed
  state. Stage 4b1 proved that empirically: ReopenActivity was built, measured at
  142/141 with DH-CONTRACT-OPCOUNT-MAX firing, and withdrawn. The enabling move is
  the projectCatalogAccess split, and it is in no row of this wave.

  Deliberately its own commit rather than a rider on the wave's model edit. That
  edit predicts an UNCHANGED golden, and a contract deletion that removes six
  registered Temporal names inside a commit whose prediction is "unchanged" is the
  exact condition under which a dropped registration hides. This commit's whole
  observable signature is -6 on the golden: 133 -> 127.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 14: The pump gets replay fixtures, for the first time since it was written

**The wave's highest-risk gap, closed last because it can only be closed last.** `constructionPumpNextActivity` and `constructionProjectSupervision` are both in the golden and both on the frozen list, and **the four fixtures that replay them are all in `replay-archive/`**. 4b1 changed `pumpnextactivity.go` and reached both through `applyRecovering`; 4b2 **rewrote** the pump. Until these exist, a non-determinism introduced into either workflow is caught by nothing.

**Why AFTER Task 12 and not before (R12).** `deliveryReplayRegistrations` returns a ONE-element slice and its doc says that is *"the whole point of the wave"*. A capture rig that registers only the pump **parks forever** on `child.Get`; registering the child alongside it makes the worker poll for two types on one queue and a driver mis-start silently captures the wrong one. Capturing the OLD pump first buys a fixture that is worthless the moment Task 12 lands, because it replays a command sequence (`ExecuteChildWorkflow → child.Get → Sleep → ContinueAsNew`) that no longer exists. **Cost, stated plainly: nothing pins the transition itself. The drain is what makes that acceptable, and Task 16 is what checks the transition by reading.**

**Files:**
- Modify: `server/internal/manager/delivery/manager_test.go` — `pumpReplayRegistrations`, `pumpReplayCases`, `Test_Capture_PumpHistories`, `Test_Replay_PumpHistories`, `deliveryReplayDirs()` (`:14118`).
- Create: `server/internal/manager/delivery/testdata/replay/post-4b2-pump/` + five fixtures.

**Interfaces produced:**
- `func pumpReplayRegistrations(wf *csWorkflows) []genRegisteredWorkflow` — `{executionKindPump, wf.PumpNextActivityWorkflow}`, `{executionKindProjectSupervision, wf.ProjectSupervisionWorkflow}`, **and `{executionKindDeliveryActivity, wf.DeliveryActivityWorkflow}`** (the pump still STARTS children; it no longer awaits them, but the driver needs a worker that can run one for the lease and finish signals to exist at all).
- `const pumpReplayDir = "post-4b2-pump"`; `deliveryReplayDirs()` returns **both** directories.

- [ ] **Step 1: The second registration list — not a widened one.**
  ```go
  // pumpReplayRegistrations is the SECOND list, and it is second rather than an extension of
  // deliveryReplayRegistrations on purpose. That one names exactly one workflow and its doc
  // comment says that is the whole point of stage 4b1; widening it would register three
  // workflows for every deliveryActivity replay — harmless for replay, wrong for CAPTURE,
  // because RegisterWorker would make the worker poll for three types on one queue and a
  // driver mis-start would silently capture the wrong one.
  //
  // deliveryActivity IS in this list, and that is not a contradiction: the pump starts
  // children and no longer awaits them, so a pump fixture needs a worker that can actually run
  // one — otherwise the started children never reach a terminal, never signal, and the pump's
  // history records a park rather than a shape.
  func pumpReplayRegistrations(wf *csWorkflows) []genRegisteredWorkflow { … }
  ```

- [ ] **Step 2: Five drivers — one per `GetVersion` arm, plus supervision.** The four pump fences must each be exercised or the fixtures pin nothing:

  | Fixture | Drives | Fence it pins |
  |---|---|---|
  | `pump-dispatch-then-quiesce` | starts a frontier of two, both children finish, the pump reconciles and quiesces | `changeLedgerPartialResume` v1, `changeDesignActivitiesDispatchable` v1, the new selector and the reconcile |
  | `pump-recorded-pause-quiet-return` | a project with `OperatorPaused` recorded | `changePumpHonorsRecordedPause` **v2** |
  | `pump-signal-pause-at-gate-two` | a pause signal delivered between `readProject` and the frontier start | `"pump-pause-before-dispatch"` and `"pump-pause-decode-any"` |
  | `pump-blocked-writes-sticky-failure` | an activity with an unresolvable `componentId` | `verdictBlocked`'s `applyRecovering` write (G-P5), and that the frontier CONTINUES past it |
  | `supervision-pause-record-relay-cancel` | the supervision workflow's record → relay → cancel | its one `GetVersion` (`projectsupervision.go:74`) |

  - [ ] **The 20-event floor applies.** `deliveryReplayMinEvents = 20`; a pump run that quiesces immediately records ~10 and would FAIL. **Every pump fixture must dispatch.** `pump-recorded-pause-quiet-return` is the one at risk — drive it so the pump does a `readProject` and at least one recorded write before going quiet, or raise the floor for this directory **with the reason written down**, never silently.
  - [ ] A **sixth** fixture is worth the cost if the schedule allows: `pump-lease-granted-and-released`, driving two children into the merge tail so the lease is granted, released and re-granted in one history. That is the only fixture that would pin the invariant itself.

- [ ] **Step 3: The directory and the case list land in ONE commit.**
  ```go
  func deliveryReplayDirs() []string { return []string{deliveryReplayDir, pumpReplayDir} }
  ```
  `Test_Replay_DeliveryHistories`'s folded directory-level orphan guard (`manager_test.go:14073-14090`) fails on a directory no case list names **and** on zero directories, so a fixture directory committed before its cases is red and cases committed before their directory are red. **One commit.**
  - [ ] `Test_Replay_PumpHistories` mirrors `Test_Replay_DeliveryHistories` exactly: missing fixture FAILS (never skips), a fixture no case names FAILS, a fixture under 20 events FAILS.

- [ ] **Step 4: Capture.**
  ```bash
  cd .../server
  which temporal   # expect /opt/homebrew/bin/temporal — version 1.7.0 (Server 1.31.0)
  CONSTRUCT_HISTORY_CAPTURE=1 GOWORK=off go test ./internal/manager/delivery/ \
      -run Test_Capture_PumpHistories -count=1 -v -timeout 30m
  ```
  - [ ] `deliveryReplayAwaitDone` needs **six minutes** (the observe ladder's real 15 s timers), and a pump fixture adds one reconcile interval per cascade iteration on top. Budget the capture at **20 minutes wall clock** and do not shorten the ladder to make it faster — a fixture captured against a substituted sleep records a different command sequence than production.
  - [ ] **The `temporal` CLI dependency is stated only in a `t.Fatalf`** (`manager_test.go:13995-13998`). Add it to this task's commit message and to Task 17's earmarks; it is in no README and no Makefile target, so a fresh checkout discovers it by failing.

- [ ] **Step 5: Gates and commit.**
  ```bash
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Replay' -count=1 -v 2>&1 | tail -30
  GOWORK=off go test -short -count=1 ./...
  ```
  Expected: `Test_Replay_DeliveryHistories` **8/8**, `Test_Replay_PumpHistories` **5/5** (or 6/6), every fixture ≥20 events, **two** fixture directories both named. Golden **127**, frozen **15**, shapes **11/11**, `validate` **43/0**, `npm` unchanged.
  ```bash
  git add server/internal/manager/delivery/manager_test.go server/internal/manager/delivery/testdata/replay/post-4b2-pump
  git commit -F - <<'MSG'
  test(pump): the pump has replay fixtures again, captured against the pump that
  exists

  constructionPumpNextActivity and constructionProjectSupervision are both in the
  registered-names golden and both on the frozen list, and every fixture that
  replayed them has been in replay-archive/ since stage 4b1. Stage 4b1 changed
  pumpnextactivity.go; stage 4b2 rewrote it. In between, a non-determinism
  introduced into either workflow was caught by nothing.

  Captured AFTER the rewrite, deliberately. A rig that registers only the pump
  parks forever on child.Get, and capturing the old pump first would have produced
  a fixture that was worthless the moment the rewrite landed — it replays a command
  sequence (ExecuteChildWorkflow, child.Get, Sleep, ContinueAsNew) that no longer
  exists. The cost is stated rather than hidden: nothing pins the TRANSITION
  itself, and the drain this wave already requires is what makes that acceptable.

  A SECOND registration list, not a widened one. deliveryReplayRegistrations names
  exactly one workflow and its comment says that is the point of the wave; widening
  it would make the capture worker poll for three types on one queue, where a
  driver mis-start silently captures the wrong workflow. deliveryActivity is in the
  new list because the pump still STARTS children — it just no longer awaits them —
  and without a worker that can run one, the started children never terminate and
  the pump's history records a park rather than a shape.

  Five drivers, one per GetVersion arm plus supervision, because a fixture that
  does not take an arm pins nothing about it. The 20-event floor is a gate here for
  the same reason it is one for the child: a thin history replays green against any
  walker.

  The capture needs the temporal CLI on PATH (1.7.0 here). That requirement is
  stated only in a t.Fatalf, in no README and no Makefile target, and it is
  earmarked.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 15: The eight Phase-2 slugs retire with their eight command files, and the `<branch>@vN` claim is corrected in both earmark files

**A live command nothing dispatches is the state a reader mistakes for coverage.** Verified present at `bae7681f`, all eight: `planning-assumptions-draft.md`, `activity-list-draft.md`, `network-draft.md`, `normal-solution-draft.md`, `subcritical-solution-draft.md`, `compressed-solution-draft.md`, `decompressed-solution-draft.md`, `risk-model-draft.md`. `DesignCommandFor` (`projectstateaccess.go:9330`) still answers for every one, through `designKindSlugs`; all eight are in `designKindHasCritique`'s `false` arm, so only `-draft` exists for them; `KindSdpReview` already returns `""` unconditionally. **Nothing dispatches them — the compute does their work** (`computeProjectPlan`, staging eight slots through `activityExecutionAccess.StageTaskOutput`, measured as 8 `stageTaskOutput` + 8 `commitArtifactWithProvenance` in `m0-no-sendback.json`).

**And one correction that is not optional.** Both earmark files state that `SubjectRef.ref` is `<branch>@v<version>` and *"that is what `ReadProjectAtRef` should take"*. **It is not.** Measured at `deliveryactivity.go:1586-1588`:
```go
func stagedRefString(ref projectstate.StagedRef, kind projectstate.ArtifactKind) string {
	return ref.ActivityID + ":" + ref.TaskID + ":" + kind.String() + "@v" + strconv.FormatInt(int64(ref.Version), 10)
}
```
`projectstate.StagedRef` carries `{ActivityID, TaskID, Branch, Version}` and the renderer **DISCARDS `Branch`**. So the ref a round actually cites is `<activityId>:<taskId>:<ArtifactKind>@v<N>`, which names **no branch at all** and carries an artifact-kind segment the earmark never mentions. Worse, `gateSubjectRef` (`deliverymanager.go:12184`) files it under `SubjectRef.Kind = SubjectCommit`, whose contract description reads *"a commit sha"*. **A design round today claims to judge a commit sha and actually holds a composite logical handle.** 4b3's plan is built on this; leaving the docs wrong is how 4b3 starts from a false premise.

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — eight rows out of `designKindSlugs`; the eight `designKindHasCritique` `false`-arm cases STAY (sum-type exhaustiveness).
- Delete: the eight `.claude/commands/*.md`.
- Modify: `docs/bugs/2026-09-26-stage4b1-earmarks.md` (carry #7) and `docs/bugs/2026-09-24-stage3-rail-earmarks.md` — the `<branch>@vN` claim.
- Modify: `server/internal/manager/delivery/deliveryactivity.go:1584-1588` — the renderer's doc comment says what it emits and what it does NOT.

**Interfaces:** `DesignCommandFor(k, DesignJobModeDraft, _)` returns `""` for all eight Phase-2 kinds. Callers already treat `""` as a contract-misuse error ("the caller asked for a job shape the Method doesn't produce"), and **nothing asks**.

- [ ] **Step 1: Prove nothing dispatches them, before deleting.**
  ```bash
  cd .../server
  for k in PlanningAssumptions ActivityList Network NormalSolution SubcriticalSolution \
           CompressedSolution DecompressedSolution RiskModel; do
    echo -n "Kind$k: "; grep -rn "DesignCommandFor(.*Kind$k" --include='*.go' . | grep -v _test | wc -l
  done
  grep -rn 'projectDesignComputedKinds' --include='*.go' . | grep -v _test
  ```
  Expected: **0 for all eight**, and `projectDesignComputedKinds()` naming the seven the compute writes. Paste into the commit message.

- [ ] **Step 2: Delete the eight rows and the eight files, together.**
  ```go
  // The EIGHT Phase-2 kinds carry NO SLUG (stage 4b2). Project Design is COMPUTED since stage
  // 4b1 — the child's compute strategy derives the plan, the network, the four options, the
  // risk model and the SDP and stages eight slots under one task — so an agent draft command
  // for any of them has had nothing to dispatch it for a whole wave. A live command nothing
  // dispatches is the state a reader mistakes for coverage.
  //
  // The eight kinds STAY in designKindHasCritique's false arm: gochecksumtype wants a case per
  // variant, and a Phase-2 kind still legitimately takes no critique.
  ```
  - [ ] `TestDesignCommandsExistInMethodAssets` requires only that a NON-empty slug has a command file — so **deleting both together is green and deleting either alone is red**. Run it between the two halves once, deliberately, to see the red; then finish.
  - [ ] `Test_DesignCommands_MatchTheLifecycleData` walks only `requirements` and `architecture` and is untouched. `Test_DispatchInputs_CommandFallbackAgreesWithTheLifecycleData` likewise. Confirm both green.

- [ ] **Step 3: Correct the handle claim in BOTH earmark files, and at the renderer.**
  Replace the `<branch>@v<version>` sentence in `docs/bugs/2026-09-26-stage4b1-earmarks.md` (carry #7) and in the stage-3 earmarks with the measured shape, and state the three consequences 4b3 inherits:
  1. `ReadProjectAtRef` must take an **opaque handle the RA PARSES**, which makes the shape a contract — so it needs ONE minter and ONE parser, both in `projectstate`. The minter must move out of `deliveryactivity.go`, or the Manager and the RA will drift about what `@v` means.
  2. `SubjectRef.Kind` must stop saying `commit` for a staged design subject. `SubjectArtifact` already exists and is the honest member.
  3. `@v<version>` is a project AGGREGATE version on a branch, **not a git object** — it is not reachable by `git show`, and resolving it by history-walk is unbounded and fails outright against the **depth-1 clones the 2026-08-11 OOM fix introduced**. So the real substrate revision must be minted at STAGE time and stored alongside the logical handle, which is the addition that makes 4b3 cheap instead of open-ended.
  - [ ] At `stagedRefString`, add: *"It emits `<activityId>:<taskId>:<ArtifactKind>@v<N>`. It does NOT emit a branch — `StagedRef.Branch` is discarded here, and the branch is implied as `activity/<activityId>`. Two earmark files claimed otherwise until 4b2; if a reader needs the branch, derive it, do not parse it out of this."*

- [ ] **Step 4: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/resourceaccess/projectstate/ ./internal/manager/delivery/ -run 'DesignCommand|Lifecycle|RoundKind' -count=1 -v
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint
  ```
  Expected: **golden 127, frozen 15, shapes 11/11, replay 8/8 + 5/5, validate 43/0, npm unchanged, preview unchanged.** No generated surface moves — `designKindSlugs` is a Go map, not a contract.
  ```bash
  git add server .claude/commands docs/bugs
  git commit -F - <<'MSG'
  chore(method): the eight Phase-2 draft commands retire, and the staged-ref claim
  is corrected

  Project Design has been COMPUTED since stage 4b1: the child's compute strategy
  derives the plan, the network, the four options, the risk model and the SDP and
  stages eight slots under one task. So the eight Phase-2 draft slugs and their
  eight .claude/commands files have had nothing to dispatch them for a whole wave —
  measured, zero DesignCommandFor callers for any of the eight — while
  DesignCommandFor kept answering for every one. A live command nothing dispatches
  is the state a reader mistakes for coverage.

  The slugs and the files go together, which is the only green order:
  TestDesignCommandsExistInMethodAssets requires that a non-empty slug HAS a
  command file, so deleting either half alone is red. The eight kinds stay in
  designKindHasCritique's false arm — gochecksumtype wants a case per variant, and
  a Phase-2 kind still legitimately takes no critique.

  AND THE HANDLE CLAIM IS CORRECTED IN BOTH EARMARK FILES, because stage 4b3 is
  built on it. They say SubjectRef.ref is "<branch>@v<version>". It is not.
  stagedRefString renders <activityId>:<taskId>:<ArtifactKind>@v<N> and DISCARDS
  StagedRef.Branch — there is no branch in it at all, and there IS an artifact-kind
  segment no earmark mentions. Worse, gateSubjectRef files it under
  SubjectRef.Kind = commit, whose contract text says "a commit sha": a design round
  claims to judge a commit sha and actually holds a composite logical handle.

  Three consequences 4b3 now inherits correctly: ReadProjectAtRef must take an
  opaque handle the RA parses, so the shape is a contract with ONE minter and ONE
  parser both in projectstate; SubjectRef.Kind must say artifact, not commit; and
  @v<N> is a project AGGREGATE version, not a git object — unreachable by git show
  and unbounded to resolve by history-walk, which fails outright against the
  depth-1 clones the 2026-08-11 OOM fix introduced. The real substrate revision has
  to be minted at stage time, and that is what makes 4b3 cheap instead of
  open-ended.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 16: The whole-branch lost-guard review — against Task 1's list, not a re-derivation

**This is the review 4b1 did by reading and 4b2 does by checklist.** It is a TASK, not a step, because it has an output that ships: a row-by-row verdict against `docs/bugs/2026-09-28-pump-guard-census.md`, appended to that file.

**Files:**
- Modify: `docs/bugs/2026-09-28-pump-guard-census.md` — a **Verdict** column and a closing section.

- [ ] **Step 1: Walk all fourteen census rows against the shipped code.** For each: open the site the census names, find the guard's successor in the new body (or its deliberate absence), and write one of exactly three verdicts — **RE-ASSERTED** (with the new `file:line`), **DELETED WITH ITS SUBJECT** (with the reason), or **LOST** (which is a blocker, not a note).
  - [ ] G-R1 and G-R2 are expected **DELETED WITH ITS SUBJECT** (Task 11). G-S1 is expected **RE-ASSERTED** at its unchanged line (Task 4). The eleven pump rows are expected **RE-ASSERTED**, and G-P10's re-assertion is the strongest claim in the wave because the CAN payload grew — it is verified by `Test_Pump_ContinueAsNewPayloadIsBoundedByThePlan`, not by reading.

- [ ] **Step 2: Sweep for the class in the bodies 4b2 deleted that Task 1 did not cover.** Task 1 scoped the census to `pumpnextactivity.go` and `replansweep.go`. Four other bodies were deleted or folded in this wave, and each is a candidate for the same class:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b2
  git diff main...HEAD -- server/internal/manager/delivery/deliverymanager.go | grep '^-' | \
    grep -E 'if |return newError|FailedPrecondition|ContractMisuse|refuse|guard|must ' | head -60
  ```
  Read every deleted refusal and answer one question per line: **does the body that replaced it refuse the same thing?** The four bodies: the two `askX`/`ackX` twin pairs and the two refusals (Task 5), `resolveQuestionBranch` (Task 3), `reconcileTargetOf`'s `ok` arm (Task 6), and `OverrideActivity`'s stage precheck (Task 2). **The one this plan predicts is at risk: `pdCheckNoReplyTo`** — Task 5 rules it stays kind-scoped, and a merge that dropped it silently re-files a reviewer's threaded reply as a detached flat note, which is exactly the defect 4b1's G4 restored `Test_ReplyTo_RefusedOnDoorsThatCannotRouteIt` for.
  - [ ] Do the same sweep on `deliveryactivity.go` for Task 12's `finalizeWalk` change.

- [ ] **Step 3: Sweep the webApp for the provider class.** Task 9 swept the ten contexts by name. Repeat it against the branch's diff:
  ```bash
  cd webApp && git diff main...HEAD -- src | grep '^-' | grep -E 'Provider|useContext|createContext'
  ```
  Expected: nothing removed. If something was, it must be re-mounted or deliberately dropped with a reason.

- [ ] **Step 4: Append the verdict section and commit.** If any row is **LOST**, this task does not commit — it reports, and the fix lands in the task that lost it.
  ```bash
  git add docs/bugs/2026-09-28-pump-guard-census.md
  git commit -F - <<'MSG'
  docs(pump): the guard census gets its verdicts

  Fourteen rows walked against the shipped code, one of three verdicts each:
  re-asserted with its new line, deleted with its subject, or lost. Stage 4b1 lost
  eight guards this way and found them by reading; this wave checked a list.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 17: The drain note, the earmarks, the spec, and the measurement

**Files:**
- Create: `docs/bugs/2026-09-28-stage4b2-earmarks.md`.
- Modify: `docs/bugs/2026-09-24-stage3-rail-earmarks.md` — the DRAIN NOTE.
- Modify: `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` — §5.1, §8's 4b2 row, §9, §10.
- Modify: `docs/bugs/2026-09-26-stage4b1-earmarks.md` — mark each of the ten carries CLOSED / MOVED-TO-4b3 / STILL OPEN.

- [ ] **Step 1: The drain note.** Add what 4b2 contributes, and be exact — a drain instruction that is approximately right is a drain that misses something:
  - `delivery:replanSweep` must be `temporal schedule delete`d (Task 11), **and so must the pre-4a `construction:replanSweep` and `construction:pumpSweep`**, which the stage-4a note already required and which are still live namespace state.
  - No workflow TYPE name is ADDED by 4b2, and one is REMOVED (`constructionReplanSweep`) — so an in-flight replan sweep is **unresumable and must be terminated by hand**, exactly like 4b1's seven.
  - Every `{p}:nextActivity` execution must be drained: 4b2 replaces the pump's command sequence wholesale with **no `GetVersion` fence** (R3), so a parked pump is unresumable.
  - Six registered activity names are removed (`gitActivityStatusAccess.*`), so the shrink argument in the golden's own comment applies again: *"a worker built from an older commit can serve a workflow THIS one started, but a worker built from this commit cannot serve one an older worker started."*
  - **The memorised `drain *:nextActivity:*` guidance remains incomplete — sweep the `delivery:` prefix too**, and now also `{p}:activity:*`.

- [ ] **Step 2: The earmark file.** Carry at minimum:
  - **`reconcileTargetOf` passed `KindMission` where its comment claimed "no kind"** (Task 6) — harmless, and recorded because a comment was wrong for a wave.
  - **The `temporal` CLI dependency is stated only in a `t.Fatalf`** and is in no README and no Makefile target.
  - **Nothing pins the pump's TRANSITION** — the fixtures are post-rewrite by necessity (Task 14).
  - **The lease deadline's liveness check costs a Describe per expiry**; nobody has measured it against a real drain.
  - **`origin/aiarch-design/archistrator/0-amend-1` still exists on the remote** and `DesignBranch` no longer names it (Task 3). Deleting the ref is a separate, deliberate act.
  - **C2's unshipped half:** if Task 8 was dropped, `ReviewRound.artifactKind` is still defensive rather than load-bearing, and the one kinded row on this repo (`architecture:architectureReview:operationalConcepts:2`) still judges a kind no lifecycle names.
  - **`GlossaryView` still joins against `scrubbedRequirements`** (C1) — correct, and worth a line so nobody "finishes" the retirement by deleting a read of the durable record.
  - The five 4b1 carries that moved to 4b3, each with its name.
  - **The M0 per-family defaulting silence** — CLOSED if Task 7 Step 7F and Task 9 Step 5 shipped, still OPEN otherwise. Say which.

- [ ] **Step 3: The spec.** §5.1 gains the react-by-signal pump as SHIPPED with the lease invariant and the explicit statement that **the merge did NOT move**; §8's 4b2 row is rewritten as a shipped row with its gate numbers; §9's owed bullet (*"revision read returns artifact-as-of-`stagedRef`"*) is **re-pointed at 4b3 with the corrected handle shape**; §9's fixture line gains the second directory; §10's "parallel children raise concurrent writes" risk gains the lease as its answer and the row-vs-main distinction as its caveat; §10's drain line gains 4b2.

- [ ] **Step 4: The measurement.**
  ```bash
  cd .../server/internal/manager/delivery
  wc -l *.go
  python3 - <<'PY'
import glob
tot=gen=test=0
for f in sorted(glob.glob('*.go')):
    n=sum(1 for _ in open(f)); tot+=n
    if f=='manager_test.go': test+=n
    elif f.endswith('.gen.go'): gen+=n
print("total",tot,"manager_test",test,"gen",gen,"hand-written",tot-test-gen)
PY
  ```
  Baseline **19,573** at `bae7681f`. Report the number, the delta, and **the per-task attribution** — Task 5 (−300 to −500), Task 11 (−100), Task 3 (−55), Task 12 (+80 to +120). §9's acceptance is against the `4baed01a` sum of predecessors, **25,643**; state the ratio.

- [ ] **Step 5: Final whole-branch gate run, and the numbers against the ledger.**
  ```bash
  cd .../server
  GOWORK=off go test -count=1 ./internal/    # NOT -short: the full-stack boot test
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off go test ./internal/manager/delivery/ -run Test_LifecycleShapes -count=1 -v   # ALONE
  GOWORK=off make lint fix-check sumtype-check vet encapsulation-check derived-plan-check method-check
  GOWORK=off make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-lifecycles-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check && ASDF_NODEJS_VERSION=lts npm run build:mcp
  cd ../uitests && npx playwright test tests/preview/ && ASDF_NODEJS_VERSION=lts npm run lint && ASDF_NODEJS_VERSION=lts npm run typecheck
  cd ../systemtests && GOWORK=off go build ./...
  ```
  Expected, against the ledger: **golden 127, frozen 15, `Test_LifecycleShapes` 11/11, replay 8/8 + 5/5, `validate` 43 advisory / 0 errors, `npm run check` 1235, preview 53 over 23 fixture states, `systemtests` builds.** **Any number that differs from its prediction is investigated before the branch is proposed, not explained afterwards.**

- [ ] **Step 6: Commit.**
  ```bash
  git add docs
  git commit -F - <<'MSG'
  docs(4b2): the drain note, the earmarks, the spec, and the measurement

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 8: FOUNDER STOP — `operationalConcepts` gets a home in the architecture lifecycle

**Ordered LAST and droppable. Read C2 before starting.** This is the half of founder ruling R-F2 that costs a platform release and reverses a ratified ruling; Task 9 already shipped the half that answers the founder's stated complaint.

**What it costs, measured:**
1. **A `method-assets` release.** `lifecycles.json` lives in `method-assets@v0.9.0` (PINNED). The `architecture` lifecycle has one phase and two tasks: `architectureDraft`(`artifactKind: System`, `command: system-draft`) and `architectureReview`(`reviews: architectureDraft`, `command: system-critique`). It gains `operationalConceptsDraft`(`artifactKind: OperationalConcepts`, `command: operational-concepts-draft`, `dependsOn: [architectureReview]`) and `operationalConceptsReview`. The command file **already exists** (`.claude/commands/operational-concepts-draft.md`, verified present) and the slug is **already live** in `designKindSlugs` — so the server side is free; the release is the cost.
2. **`Phase1RequiredKinds()` must gain `KindOperationalConcepts`.** `Test_Phase1RequiredKinds_AreExactlyTheTwoDesignLifecyclesOutput` (`manager_test.go:18654`) asserts `architecture produces … want ONE kind` and that the produced list **equals** `Phase1RequiredKinds()`. Both assertions move.
3. **It REVERSES the 2026-08-30 founder ruling** that retired `operationalConcepts` from the Phase-1 drafting sequence, whose reasoning is written into `Phase1RequiredKinds`' doc comment. **That is not a blocker — a founder may reverse a founder ruling — but it must be done knowingly and the doc comment must say which ruling now stands.**
4. **The Phase-1 seal then waits on `operationalConcepts`.** On this repo that is safe: the slot is committed, so `seedWalkFromLedger`'s `committedArtifactOfTask` arm seeds both new tasks **passed** and the walk drafts nothing. On a fresh project it is a real new step in the architecture walk, which is the intent.
5. **It is what makes `ReviewRound.artifactKind` load-bearing.** Today the one kinded row this repo holds is `architecture:architectureReview:operationalConcepts:2` — a round judging a kind no lifecycle names. After this, `architectureReview` judges two kinds and the field is the join, which is exactly the collision stage 4b1 Task 3 built it for.

**Files:** `method-assets` (a separate repo — a release, a tag, a pin bump in `server/go.mod`), `server/internal/resourceaccess/projectstate/projectstateaccess.go` (`Phase1RequiredKinds`), `server/internal/manager/delivery/manager_test.go`, `server/internal/manager/delivery/lifecycles.gen.ts`/`gen-lifecycles` output, `uitests` fixtures that assert the architecture walk's task list.

- [ ] **Step 1: STOP. Put the five costs in front of the founder, and get a yes or a no.** Specifically: *"Adding operationalConcepts to the architecture lifecycle means a method-assets release, and it puts the Phase-1 seal back behind operationalConcepts — reversing the 2026-08-30 ruling that took it out. The Deployment diagram is already fixed and needs none of this. Do you want the drafting step back as well?"*
  - [ ] **On a NO:** delete this task, and record in Task 17's earmarks that `ReviewRound.artifactKind` stays defensive and the one kinded row still judges a kind no lifecycle names.

- [ ] **Step 2: The release.** Add the two tasks to `lifecycles.json`'s `architecture` entry, tag `method-assets v0.10.0`, bump the pin in `server/go.mod`, run `GOWORK=off make gen-lifecycles`.
  - [ ] **Verify first:** `parseLifecycles` uses `dec.DisallowUnknownFields()` and `mustParseLifecycles` **panics at package init**, so a malformed release is a hard failure at import time, not a test failure. Validate the JSON against the schema in the method-assets repo before tagging.
  - [ ] `Test_RoundKindOfTask_AgreesWithWhatTheChildStamps` pins `len(methodassets.Lifecycles()) == 14` — **unchanged**, this adds tasks, not lifecycles.
  - [ ] `Test_LastPhaseGateIsTheWalksFinalTask` asserts, for all fourteen, that the last phase's gate IS the last task in declaration order and nothing depends on it. **`operationalConceptsReview` must therefore be the LAST task**, after `architectureReview`, or this goes red — which is the test doing its job.

- [ ] **Step 3: `Phase1RequiredKinds` and the doc comment.** Add `KindOperationalConcepts` after `KindSystem`, and rewrite the RETIRED-STEPS paragraph to say which ruling now stands and which is superseded — **naming both dates**, so the next reader sees a decision and not a drift.

- [ ] **Step 4: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off make gen-lifecycles && GOWORK=off make gen-lifecycles-check
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Phase1RequiredKinds|Test_DesignCommands|Test_RoundKindOfTask|Test_LastPhaseGate|Test_LifecycleShapes' -count=1 -v
  GOWORK=off go test -short -count=1 ./...
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && npx playwright test tests/preview/
  ```
  Expected: golden **127** (a lifecycle task is not a Temporal name), frozen **15**, shapes **11/11**, replay **8/8 + 5/5**, `validate` **43/0**, `npm run check` **1235 + any lifecycle-derived node test** (state the number), preview **53** unless a fixture asserts the architecture task list, in which case say which and by how much.
  - [ ] **The commit message must name the reversed ruling explicitly.** A ruling reversed silently is the worst artifact this wave could produce.

---

## Self-review

Run with fresh eyes against the spec, the controller's scope list and the architect's rulings.

### 1. Spec → task coverage

| Source requirement | Task |
|---|---|
| Spec §5.1 — "reacts to child completion by signal rather than blocking on `child.Get`" | **12** |
| Spec §5.1 — "starts a child for **every** eligible activity, not one at a time" | **12** Step 4 |
| Spec §5.1 — "singular per project" | **12** (unchanged; R1) |
| Spec §8 4b2 row — `PumpResult.activityIds` | **7** Step 10 (`$defs`) + **12** Step 9/10 (filled) |
| Spec §8 4b2 row — eight Phase-2 slugs + eight command files, ONE coordinated step | **15** |
| Spec §8 4b2 row — `projectstate.DesignBranch` | **3** |
| Spec §8 4b2 row — "three `kind:'session'` members collapsed to one" | **5** + **7** Step 6 — **AMENDED to TWO, not one** (R-D): `constructionSession` does not fold in, and the plan says why at the ruling. |
| Spec §8 4b2 row — three deprecated RA facets deleted post-drain | **13** — **AMENDED to ONE** (R6), with the blocker measured. |
| Spec §8 4b2 row — artifact-as-of-revision read, `<branch>@vN` handle | **OUT → 4b3**, and **15** corrects the handle claim the spec repeats. |
| Spec §8 4b2 row — batched `QueryProjectView(plan)` | **OUT → 4b3** |
| Spec §8 4b2 row — PUSHED job-completion signal | **OUT → 4b3**, and the plan records that the spec **conflates** it with child-completion. |
| Spec §8 4b2 row — per-task session view | **OUT → 4b3**; the defect it existed to fix is closed by **2**. |
| Spec §8 4b2 row — sweep that re-opens a Completed activity with uncommitted slots | **OUT → 4b3** |
| Spec §8 4b2 row — GAP-4B-4's three orphan kinds | **7** Step 9 (two retired) + **8** (one gets a lifecycle, FOUNDER STOP) |
| Spec §8 4b2 row — `RevenueShareNone` | **7** Step 2 — **SUPERSEDED**: the concept goes (R7), so no member is added. |
| Spec §8 4b2 row — assumed-planning-assumptions UX | **7** Step 7F + **9** Step 5 (the disclosure half); the editing screen stays its own wave. |
| Spec §8 4b2 row — construction answer command | **10** — ruled human-answered; no command written. |
| Spec §8 4b2 row — design questions on branch or main | **3** — main, ratified. |
| Spec §9 — "revision read returns artifact-as-of-`stagedRef`" (the owed bullet) | **OUT → 4b3**; **17** Step 3 re-points it with the corrected handle. |
| Spec §9 — shape cases | **12** Step 2 (+`continue-as-new-loses-no-signal`, 11th) |
| Spec §9 — acceptance: parallel construction children from the Plan screen | **12** makes it true; **17** Step 4 measures the line half. |
| Spec §10 — "the rail's commit step must serialize merges per project" | **12** — via the LEASE, with the merge staying in the child (R1). |
| Spec §10 — ONE drain covers 3 + 4a + 4b1 + 4b2 | **17** Step 1 |
| Controller item 1 — fork-steer precheck | **2** |
| Controller item 2 — dead resolver | **3** |
| Controller item 3 — `pumpsweep.go:88` | **4** |
| Controller item 4 — session-view fold | **5** + **7** |
| Controller item 5 — F80c `kinds []ArtifactKind` | **6** + **7** |
| Controller item 6 — ONE model edit | **7** |
| Controller item 7 — `CommittedSlotsProvider` re-mount + sweep | **9** |
| Controller item 8 — construction questions human-answered | **10** |
| Controller item 9 — `ReplanSweepWorkflow` + frozen name | **7** (op) + **11** (workflow) |
| Controller item 10 — THE PUMP | **12** |
| Controller item 11 — `gitActivityStatusAccess` | **13** |
| Controller item 12 — replay fixtures | **14** |
| Controller item 13 — eight slugs + `<branch>@vN` doc fix | **15** |
| Controller (a) — the lost-guard task | **1** (census) + **16** (verdicts) |
| Controller (b) — the provider/context sweep | **9** Step 3 |

**Gaps found and closed while writing this table:** the controller's item 9 could not be one task (C3 — the façade op is a model edit), so it is split across 7 and 11 with both predictions stated. The controller's item 6 folded `operationalConcepts`' lifecycle home into the model edit, which is impossible (C2 — lifecycles are in method-assets), so it is Task 8 and a STOP. Nothing in the controller's list is unassigned.

### 2. Placeholder scan

Searched for `TBD`, `TODO`, `implement later`, `add appropriate error handling`, `add validation`, `handle edge cases`, `write tests for the above`, `similar to Task N`. **None present.** Every code step carries a code block or a named file:line to read first; every test step names its assertion and its expected red/green; every gate step states the number it expects and why. Three steps deliberately say "**Verify first**" and hand the implementer a grep whose expected output is printed — that is a measurement instruction, not a placeholder. Two steps are explicitly **CONDITIONAL** (Task 7 Step 7F, Task 9 Step 5) and both state the default, the decision point and what to delete on the other ruling.

### 3. Type and name consistency

Checked across tasks: `pumpState` / `pumpInput` (Task 12 Step 3 defines both; Step 8 carries them), `signalActivityLeaseRequested` / `signalActivityLeaseGranted` / `signalActivityFinished` (Task 12 Interfaces; used in Steps 6, 7, 11), `pumpReplayRegistrations` / `pumpReplayDir` / `Test_Replay_PumpHistories` (Task 14, all three consistent), `designArtifactSessionView` (Task 5 Interfaces; Task 7 Step 6 names the wire type `DesignArtifactSessionView` and the enum `DesignArtifactSessionStage` — **the Go producer and the wire type deliberately differ in case only, which is this repo's convention**), `ReconcileBranchFromMain(… kinds []ArtifactKind …)` (Task 6 Interfaces = Task 7 Step 5 = Task 13's unaffected wiring), `ProjectCommitTimeComputeCost` (Task 7 Interfaces; the only rename in the wave), `reconcileTargetOf` returns `[]projectstate.ArtifactKind` after Task 6 and `branchReconcile` is deleted in the same task (no later task names it), `FlaggedVariance` survives Task 11 and is named as surviving, `escalatedTaskOf` / `m.escalatedTask` (Task 2, both real, both at the lines given). **One inconsistency found and fixed inline:** the gate ledger's end-state predicted preview **52**, while Task 10 adds a case for **53**; Task 10's gate block now states the reconciliation and Task 17 Step 5 asserts **53**.

### 4. The three claims a reviewer should check first

1. **That the merge really does stay in the child, and the lease really is enough.** This is the wave's one architectural decision and it reverses a prior ruling. The check: read `finalizeWalk` (`deliveryactivity.go:3304`) and confirm that the lease brackets `runWalkMerge` + `finalizeActivity` + `commitDesignArtifacts` and **nothing else** — and that a walk that never reaches its tail never asks for one. If any main-write escaped the bracket, the invariant is a claim rather than a mechanism. Then confirm the release is on **every** exit including the error paths: a lease leaked by a returning error stops the project until the deadline, and the deadline's liveness check will *renew* a holder that is closed-but-unreconciled only until the next tick.
2. **That the ContinueAsNew drain-and-carry was mutation-checked, not merely written.** Task 12 Step 8 requires deleting rule 2, watching `continue-as-new-loses-no-signal` go red, restoring it, and pasting both outputs into the commit. **If both outputs are not in the commit message, the mitigation is unproven** — and it is the one failure in this wave that is silent, intermittent and project-stopping.
3. **That C1's resolution is what the founder meant.** R-F3 says `scrubbedRequirements` and `standardCheck` are retired "completely (slots, …)"; this plan retires their DRAFTABILITY and keeps their ordinals and their committed slot data, because `ArtifactKind` is an ordinal enum, they sit at 2 and 7, and `Phase1RequiredKinds`' own doc comment already carries a ratified 2026-08-30 ruling retiring exactly these kinds *in place* for exactly this reason. The check: read `Phase1RequiredKinds`' RETIRED-STEPS paragraph, then Task 7 Step 9's new comment, and decide whether "removed" meant "stop drafting them" or "erase the record". **If it meant erase, this plan is wrong and the correct fix is a migration wave, not a line in Task 7.**

