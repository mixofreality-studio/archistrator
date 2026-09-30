# Activity Experience — Stage 4b3 (one honest state, one arch gate, one model edit, and the RELEASE) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the correctness items 4b2 deferred — the delinquency payload, the signal-wire-form CONSUMER gate, the per-task session state, the late-approve-to-the-wrong-revision defect, the two reconcile gaps and the re-open sweep — land the ONE model edit that carries every contract delta plus the preview-fixture regen, split what the file-layout standard permits of the 13,234-line `deliverymanager.go`, and then **drain, merge, TAG and DEPLOY.** This is the wave that ends in production.

**Architecture:** Two orderings are load-bearing and everything else is ordinary. **First**, `projectsupervision.go:48` is fixed and the five pump `GetVersion` fences are discharged BEFORE the consumer arch gate is written, so the gate lands on a tree with **no production exception at all** — its one sanctioned exception shape (a pre-change arm behind a fence) is implemented and proved against a fixture corpus rather than against a live instance. **Second**, THE ONE MODEL EDIT (Task 8) sits in the middle: every task before it is Go-only with no wire change, and the three items whose fixes need a contract delta — the per-task view's wire move, the third head fact, and the derived red node — take their Go half before it and their wire half in it. That is forced by R8 (one model edit per wave), not chosen. The 4b2 lesson governs the whole wave: **a fix that reads right, passes, and does not fire on the case it names is this programme's signature failure**, so every gate in this plan is mutation-checked and the consumer gate is proved RED on three known instances before it ships.

**Tech Stack:** Go 1.26 (`GOWORK=off` always), Temporal Go SDK v1.44.0 (`workflow.Selector`, `GetSignalChannel`, `GetVersion`, replay testing), `temporal` CLI 1.7.0 (Server 1.31.0) — a hard dependency of both capture tools, stated only in a `t.Fatalf`; `.aiarch/state/project.json` as the model database (git-as-DB); modelgen / clientgen / appgen / temporalgen codegen; `framework-go@v0.11.1` (`arch.Check`, `arch.CheckFileLayout`, `methodcheck`, `fwra`) and `method-assets@v0.9.0` (`lifecycles.json`) — both PINNED, never a `replace`; `golang.org/x/tools/go/packages` (already a test dependency of `server/internal`); React 19 + TypeScript 5.9; Playwright 1.50 in `uitests/`.

**Spec:** `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` — §5.1 (the pump and its lease), §5.2 (the child and its merge tail), §5.3 (the two append-only ledgers, the head facts, the facets), §6 (deterministic Project Design), §8's **4b3 row and merge order** (both corrected by Task 12), §9 (testing; the one owed bullet moves to stage 6), §10 (risks: the ONE drain, the god-manager, parallel children).

**Binding inputs, in precedence order.** The controller's file OVERRIDES the architect where they differ, and the recon's measurements override both where a claim no longer reproduces:

1. `scratchpad/stage4b3-status.md` — **FOUNDER RULINGS and the accepted architect rulings. These BIND.**
2. `scratchpad/stage4b3-recon.md` (370 lines, measured at `8d9604b1`) — every `file:line` in this plan comes from there or from a fresh grep taken while writing it, and the load-bearing ones were re-verified.
3. `scratchpad/stage4b3-architect-rulings.md` (26 rulings, R1–R26), as amended by (1).
4. `docs/bugs/2026-09-28-stage4b2-earmarks.md` — the carry list and the standing rules.
5. `docs/bugs/2026-09-24-stage3-rail-earmarks.md` — **THE DRAIN PROCEDURE.** Task 13 runs it.
6. `docs/superpowers/plans/2026-09-28-activity-experience-stage4b2.md` — the predecessor plan whose format, density and constraints this plan carries forward.

---

## Global Constraints

Every task's requirements implicitly include this section. The first block is copied **verbatim** from the controller's standing rules.

- **`GOWORK=off` on EVERY `go`/`make` command under `server/`.** `ASDF_NODEJS_VERSION=lts` on EVERY npm/npx command.
- **`golangci-lint` needs `GOWORK=off` EVEN FROM `server/`.** A bare `make lint` in a detached worktree exits **3** on missing sibling platform modules. Always `cd server && GOWORK=off make lint`. And **`golangci-lint cache clean` runs FIRST in any fresh worktree** — a stale cache printed 30 phantom issues pointing into a deleted sibling worktree (4b2).
- **Run `go test -short -count=1 ./...`, never `make test-short`.**
- **THE ONE MODEL EDIT (Task 8) is the only `.aiarch/state/project.json` change in this wave, and it goes through the generators.** Never hand-edit a generated file. Slots 9/10 only via `make derived-plan-write`. **Never two implementers on `project.json`, even in disjoint regions.**
- **No `//nolint`. No `default:` over a sum type.** (`gochecksumtype` is in the linter set; a `default:` arm that swallows a new member is how a vocabulary change goes silent — and Task 8 ADDS a member to `ActivityConstructionPhase`, so this rule is live this wave.)
- **Copy `.claude/{skills,commands,agents}` MERGED (not nested) into any worktree** — three tests read them.
- **The uitests preview suite runs the MANAGED way and never with `UITESTS_PREVIEW_URL` set.**
- **`Test_LifecycleShapes`'s fork case flakes ~7.5% under parallel load and 0/30 serially.** Run it ALONE (`-run Test_LifecycleShapes -count=1`) when it is the thing being judged; a failure seen while another task's `go test ./...` is running is re-run before it is believed. **A serial CI run understates the class** — nobody may declare a future flake "gone" from a serial run.
- **A DEADNESS CLAIM CARRIES A COMPILE-PROBE TRANSCRIPT, NEVER A BARE GREP** — delete the member, then `GOWORK=off go build ./... && GOWORK=off go vet ./...` in **BOTH `server/` and `systemtests/`** (vet type-checks tests, build does not) — **and it must name WHICH deadness was measured: *uncalled*, *unreachable*, or *unreferenced-in-Go-but-live-via-MCP*.** Six of the nine indirections that reach a contract op are invisible to `\.Op(`; the worst is the generated Temporal invoker, whose method prefix is the FIELD name in `genActivities` (`GitActivityStatusAccess` is held in a field called `GitStatus`).

Plus, for this wave:

- Work in the git worktree `.claude/worktrees/activity-stage4b3` (branch `activity-experience-stage4b3`, from `main` @`8d9604b1`). The main checkout is shared. `webApp/node_modules` must be installed there. The working tree at `main` carries two UNTRACKED paths — `webApp/proto.html` and `webApp/proto/` — which are **not** part of this wave and must not be committed (spec §Prototype says the throwaway harness was deleted in stage 5; these are a local re-creation).
- Gates run against PINNED platform tags. **A field, lifecycle task or arch helper this plan cannot do without is a STOP, never a workaround.** There is no founder-STOP task in this wave.
- **The self-amendment loop**, run after the `project.json` edit (Task 8 only):
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage4b3/server
  GOWORK=off make gen-models gen-fakes gen-client gen-internal-tools gen-temporal gen-sdk gen-config gen-main gen-lifecycles
  GOWORK=off make method-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot <every other slot this edit touched>
  GOWORK=off go test -short -count=1 ./...
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run gen:api && ASDF_NODEJS_VERSION=lts npm run gen:ops && ASDF_NODEJS_VERSION=lts npm run check
  cd ../systemtests && GOWORK=off go build ./... && GOWORK=off go vet ./...
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
- **`make gen-sdk` DELETES SDK files a separate Go module's hand-written harness calls.** `pruneStaleSDK` (`cmd/appgen/main.go:342`) removes every `*.gen.go` under `../systemtests/internal/sdk` not in the fresh output set, and `systemtests.yml` triggers on `server/**` and `.aiarch/**`. Every task whose gate block runs a generator ends with the `systemtests` build **and** vet.
- **NO DEPLOY until one drain covers stages 3 + 4a + 4b1 + 4b2 + 4b3.** Task 13 is that drain, and it is the last task in the wave.

---

## The gate ledger

**Measured at `8d9604b1` while writing this plan. Every task's gate block asserts against these and states what it expects them to become. An unpredicted number is how a dropped registration hides — reconcile it, never rubber-stamp it.**

| Gate | Command | Baseline @ `8d9604b1` | End of 4b3 |
|---|---|---|---|
| Registered-names golden | `go test ./internal/ -run TestRegisteredTemporalNamesGolden` | **133** | **133 — UNMOVED.** 4b3 adds no workflow type, renames no workflow, and adds/removes **no contract op**, so no generated activity name moves. A movement here is a defect, not a surprise. |
| Frozen workflow TYPE names | `…_FrozenWorkflowNames` | **15** | **15 — UNMOVED** |
| `Test_LifecycleShapes` | `go test ./internal/manager/delivery/ -run Test_LifecycleShapes -count=1` | **11/11** | **15/15** (+1 Task 5, +1 Task 6, +1 Task 7, +1 Task 9) |
| Replay — child | `-run Test_Replay_DeliveryHistories` | **8/8** | **8/8 — UNMOVED** |
| Replay — pump | `-run Test_Replay_PumpHistories` | **6/6** | **6/6 — UNMOVED** |
| Replay fixtures total | `find testdata/replay -name '*.json' \| wc -l` | **14 live** (8 child + 6 pump) **+ 19 archived** | **14 + 19 — UNMOVED.** No fixture is re-captured this wave. |
| Pump guard census rows | `len(pumpGuardCensus())`, doc `docs/bugs/2026-09-28-pump-guard-census.md` | **36** (22 + 7 + 7), **47 pins**, **3 meta-tests green** | **33** (−4 at Task 3: G-P18…G-P21 die with the fences; +1 at Task 7: the zombie probe). Pin count is **re-measured** by Task 3 and Task 7, never transcribed. |
| `Test_DeliverSignal_…ProducersAreAClosedList` | `-run Test_DeliverSignal_` | PASS, 4 producers | PASS, **4 producers — UNMOVED** (Task 7's zombie probe reuses `signalActivityLeaseGranted`; see Task 7 for why it may not mint a fifth name) |
| **Consumer wire-form arch gate** | `go test ./internal/ -run TestSignalWireFormConsumers` | **does not exist** | **PASS over production, RED over all 3 fixture instances** (Task 4) |
| `validate --root .. --slot System` | `go run ./cmd/aiarch-state-mcp validate --root .. --slot System` | **43 advisory / 0 errors** | **43 / 0 — UNMOVED.** Task 8 must not disturb `DH-CARD-ENGINES`(7), `DH-CARD-RA-RESOURCES`(18), `DH-CARD-VOLATILITY`(18), `DH-OBJ-COVERAGE`, `PA-RATECARD-KEYS`×8, `APPC-SVC-STRIVE`; `DH-CONTRACT-DEADOP` stays ×2 (`AcknowledgeStaleBasis`, `RecordOperatorNote`). |
| `DH-CONTRACT-OPCOUNT-MAX` | pinned ABSENT, `internal/engine/designhealth/engine_test.go:60-67` | ABSENT | ABSENT — **no contract gains an op in 4b3.** `deliveryManager` stays at 11, `activityExecutionAccess` and `constructionTransitionAccess` stay at 12/12. |
| webApp | `cd webApp && ASDF_NODEJS_VERSION=lts npm run check` | **1237** | **≈1243** (Task 5 +2, Task 8 net +1, Task 9 +2, Task 10 +1). Task 12 measures and corrects the number rather than restating it. |
| uitests preview | `cd uitests && npx playwright test tests/preview/` | **57 cases** over **23 fixture files / 22 exercised** | **57 / 23 — UNMOVED.** Task 8 rewrites fixture CONTENT, not the case set; if a case moves, a spec was asserting the stale contract world and that is a finding. |
| `systemtests` | `cd systemtests && GOWORK=off go build ./... && go vet ./...` | builds, vets | builds, vets |
| Hand-written `delivery` lines | `wc -l internal/manager/delivery/*.go` minus `manager_test.go` minus the four `*.gen.go` | **20,714** (51,560 − 28,675 − 2,171) | **≈21,000–21,500.** §9's acceptance is a **CEILING, not a ratchet**: 20,714 < 25,643 holds and will keep holding while the number grows. The floor the next wave measures against is 20,714 and Task 12 writes that sentence into §10. |
| `deliverymanager.go` | `wc -l internal/manager/delivery/deliverymanager.go` | **13,234** | measured by Task 11; **`TestFileLayout` stays green** is the acceptance, not a line target (see C4). |

---

## Rulings carried into this plan

**R-prefixed rulings are the controller's, the founder's and the architect's, and they BIND — an implementer does not re-litigate them.** P-prefixed rulings were made while writing this plan, each with the measurement that forced it. **C-prefixed items are conflicts between a binding ruling and the code; a reviewer checks those first.**

| # | Ruling |
|---|---|
| **R1** | **NO PRODUCTION USERS (founder).** Stale Temporal state is not a constraint; the drain is optional cleanup, not a gate — **with one exception that still matters: the three `temporal schedule delete` calls, on any namespace that ever ran an older image.** `messagebus.RegisterSchedule` (`internal/utility/messagebus/messagebus.go:167-217`) does Create-then-Update-in-place and therefore **ADOPTS** a same-id Schedule rather than replacing it, so an unregistered Schedule is not a deleted one; it keeps firing into a workflow type no worker serves, which is a silent dead sweep, not an error. |
| **R2** | **THE RELEASE MOVES TO THE END OF 4b3 (R19).** Nothing left in stage 6 changes a workflow type, an id family, a queue owner or a Schedule, so stage 6 ships onto a DEPLOYED system. 4b3 ends: drain → merge → tag → deploy. |
| **R3** | **DELINQUENCY (founder): flatten to billing's shape AND replace the bool with a three-value action `{unknown|pause|withdraw}` whose `unknown` is REFUSED.** This adopts R12's substance MINUS the shared-type modelling. Measured (recon §1, re-verified): `DelinquencyContext` has **exactly one** member, `PauseNotWithdraw bool` (`operations/contract.gen.go:58-60`), so the two payloads differ **only in nesting**; the zero value means **WITHDRAW**, and withdraw is the only arm that ACTS (`delinquencyenforcement.go:77-91` — the pause arm is a log line, deliberately, with the reason at the site). **The producer is UNREACHABLE today** (`billingStateAccess` is the arm-less stub in every profile, `cmd/server/main.gen.go:427`), so this is LATENT, not live — which is precisely why it is cheap to do right rather than fast. **Do not "just fix the wire form."** |
| **R4** | **THE CONSUMER GATE IS NAME-KEYED, and id-keyed is NOT IMPLEMENTABLE** (recon §5, measured): every bus target is a runtime value — `pumpWorkflowID(in.ProjectID)`, `deliveryActivityWorkflowID(…)`, `fmt.Sprintf("%s:delinquency", customerID)`. Name-keying is also stricter exactly where strictness is wanted: `applyDelinquencyPolicy`'s two producers share an id (both shapes flag it) while `operatorPauseRequested`'s go to DIFFERENT ids, so id-keying would CLEAR `projectsupervision.go:48` — the latent defect the gate exists to arm. **ONE sanctioned exception SHAPE — a pre-change arm behind a `GetVersion` fence — never an allowlist.** |
| **R5** | **THE GATE MUST RECOGNISE ALL THREE RECEIVE FORMS** — `Receive`, `ReceiveAsync` (`billing/closecycle.go:192`, `:217`) and the `AddReceive` CLOSURE (`deliveryactivity.go:2009-2024`). **A gate matching only the first ships as coverage it has not.** Free fourth rule: **flag any producer that sets `ExecutionPayload.ContentType`** — the field is declared once (`internal/utility/messagebus/contract.gen.go:23`) and read by nothing (`messagebus.go:162` passes `payload.Bytes` alone), so its presence is the strongest available signal that its author believed the transport serialised for them. |
| **R6** | **THE GATE IS THE WAVE'S RISKIEST ITEM (R9)**, by risk = silence × blast-radius: it is the only item that fails green-and-blind, in the exact class it exists to catch. **It must be proved RED on three known instances before it ships**, mutation-checked each. A gate that passes DISCHARGES the earmark, which is worse than no gate. |
| **R7** | **THE FIVE PUMP `GetVersion` FENCES ARE DISCHARGED** as ONE edit with their census arms and their `…_DefaultVersion_…` tests. No users means no history to protect. Carry the counter-evidence with it: **a replay fixture does NOT pin a `GetVersion` rung** (deleting the `changeDesignActivitiesDispatchable` rung left all six pump fixtures GREEN), so "replay is 14/14" is not licence — the `…_DefaultVersion_…` tests are those rows' only pin, and they go together with the rows. |
| **R8** | **THE PER-TASK VIEW ALREADY EXISTS (R15).** `ActivityView.tasks[]` is `ActivityTaskView{id,kind,title,phase,dependsOn,reviews,state,revisions}` and `TaskRevisionView` already carries `round`. The six lossy members move DOWN and are **DELETED, not deprecated** — two independent call sites got a fork wrong in 4b2 and one was the ROUTINE approval path, and with no users there is no compat argument. `awaitingGate` is deleted outright: a per-task view needs no field naming which task it is; **the key IS the task.** |
| **R9** | **"ASK THE LEDGER" IS PERMANENT FOR SERVER DECISIONS AND NEVER ACCEPTABLE FOR THE CLIENT (R16).** The SPA has no ledger; telling it to ask means re-implementing `latestRoundFor` / `roundOfComment` / `escalatedTaskOf` in TypeScript, which is the exact drift hazard this programme exists to remove. R8 and R9 are one ruling read from two ends. |
| **R10** | **THE LATE-APPROVE FIX RIDES THE PER-TASK VIEW (R17) and the child-side half costs NO contract change** — `taskDecisionSignal` (`deliverymanager.go:8377-8394`, re-verified: 6 fields, no round) is Manager-INTERNAL. Only "the browser's round travels" is a contract change. **Buy the cheap half and say what the other costs.** |
| **R11** | **THE NO-FAILURE-ROW GAP GETS A THIRD HEAD FACT AND NEVER OVERWRITES `Completed` (R18a).** The red node is DERIVED (`completedNotLanded`) — derived-not-stored is §5.3's own rule, and "a fact may only be asserted by whoever established it" is 4b2's `markFinished`/`releaseLease` split. **Its test must construct a genuinely broken merge tail**; asserting over a hand-built row is how 4b2's `roundRevisions` fix certified a shape that could never match. |
| **R12** | **THE ZOMBIE GETS A STALENESS BOUND REUSING `pumpCheckLease`'S ONE-PROBE-PER-TICK (R18b)**, not a new RA op (a contract change on a 12/12 facet) and not N probes per tick. Recon narrows the gap to **futureless AND never-asked-for-a-lease AND no row**. |
| **R13** | **ONE MODEL EDIT PER WAVE, ONE INDIVISIBLE COMMIT, and every 4b3 contract delta rides it (R8-architect).** Two edits = two self-amendment loops, two `systemtests` regens, two rounds of preview-fixture churn, and the §8 rule that no two implementers may touch `project.json` concurrently. |
| **R14** | **THE PREVIEW FIXTURE REGEN IS A MEMBER OF THE MODEL COMMIT, not a follow-up.** Measured and reproduced exactly while writing this plan: **17 of 23 fixtures carry a 30-entry `ServiceContracts` map** whose delta from the live 28 is `+constructionManager, +projectDesignManager, +systemDesignManager` (the three 4a deleted) and **`−deliveryManager`** (the Manager this whole stage exists to build). They also carry `RunReplanSweep` / `ReplanSweepResult`. **57/57 are green over it and nothing gates fixture `ServiceContracts` against `project.json`.** |
| **R15** | **`ReplanSweepResult` IS UNREFERENCED, not merely uncalled** — 4 declaration sites (`delivery/contract.gen.go:673`, `systemtests/internal/sdk/types_delivery.gen.go:661`, `webApp/src/contracts/schema.ts:856`, `server/api/openapi.yaml:1641`), zero references across both Go modules incl. tests and gen, TS and uitests, with all six indirections excluded BY CONSTRUCTION. **A compile probe is still owed at deletion.** |
| **R16** | **THE GOD COMPONENT IS `projectStateAccess`, NOT `deliveryManager` (R22/R23).** `deliveryManager` must **NOT** be split: it encapsulates exactly one volatility with its variation in data, and both ways to shrink it (by phase, by domain) are the anti-patterns this programme deleted. What 4b3 owes is the FILE, and the ceiling-not-ratchet sentence in §10. |
| **R17** | **`operationalConcepts` GETS NO DRAFTING STEP (founder, second batch).** R24 is REJECTED: no method-assets release, no lifecycle change, the Phase-1 seal stays as the 2026-08-30 ruling left it, and **`ReviewRound.artifactKind` stays defensive rather than load-bearing.** Task 12 records this as a deliberate choice with its cost, not an oversight. |
| **P-A** | **THE FENCES ARE DISCHARGED BEFORE THE GATE IS WRITTEN, which reverses the controller's items 2 and 3.** Reason, measured: the fence arm at `pumpnextactivity.go:1299-1306` (`pumpPausedAtRunStart`'s `DefaultVersion` body) is the gate's **only live production exception** (recon §5.2 item 1). Writing the gate first means shipping an exception mechanism whose single instance disappears three commits later, and it means the gate's first green run is over a tree that still contains a receive-into-struct. Discharging first lets the gate land over a production corpus with **zero exceptions**, and the exception SHAPE — which is a standing rule for the future, not a waiver for today — is implemented and proved against a **fixture** instance instead, which is strictly better evidence (a production instance would be deleted by the next wave and take the proof with it). |
| **P-B** | **THE ONE MODEL EDIT MOVES TO THE MIDDLE (Task 8), ahead of the two items whose fixes need a field that does not exist.** Forced, not chosen: `ActivityExecution` has no member that can say "completed its work and failed to land it" (measured: the four sticky head facts are `StartedAt`, `CompletedAt`, `FailureReason`, `FailureDetail`, and `FailureDetail` is the paired half of `FailureReason` at all three of its write sites), and `ActivityConstructionPhase` has exactly four members (`contract.gen.go:23-26`), none of which is the derived red node. R13 forbids a second model edit, so the tasks that need the new members come after the one that adds them. |
| **P-C** | **THE ARTIFACT-AS-OF-REVISION READ MOVES TO STAGE 6.** Justified in the Scope table below; it is the one scope decision this plan makes rather than inherits. |
| **P-D** | **THE `deliverymanager.go` SPLIT IS THE RULE-2 RE-HOMING, because Rule 1 and Rule 4 of the file-layout standard FORBID a second impl file.** See **C4** — this is the wave's sharpest conflict between a binding ruling and a live gate. |

### The conflicts between a binding ruling and the code, and how this plan resolves each

| # | Conflict | Resolution |
|---|---|---|
| **C1** | **"No contract change" and "`unknown` is REFUSED" cannot BOTH hold at the public façade.** Measured: `DelinquencyContext.pauseNotWithdraw` is a non-pointer `bool` in a **generated contract type** reachable over REST (`operations_handlers.gen.go:37`) and MCP (`operations_tools.gen.go:42`). A request body that omits it decodes as `false`, and `false` means **WITHDRAW** (`delinquencyenforcement.go:69-72`). So a REST/MCP caller who says nothing still gets a hard withdraw, and only a `$defs` change (an enum, or an optional bool) can refuse them. | **Task 1 does the INNER half only, with no contract change, and it is the half that buys the headline safety.** The Manager-internal `applyDelinquencySignal` is flattened to billing's shape and carries `Action delinquencyAction` whose zero value is `unknown`, and the enforcement branch **refuses `unknown` loudly**. That closes the hazard R11(e) names — a wire-form fix that leaves the payload empty and then HARD-WITHDRAWS every in-flight app — because an empty or garbled decode now fails instead of withdrawing. The façade maps `PauseNotWithdraw bool` → `{pause, withdraw}` and can never produce `unknown`, so the outer half guards only the "caller omitted the field" case. **That residue is recorded as an earmark by Task 12 with its price: one `$defs` enum swap across six surfaces (`operations/contract.gen.go`, `openapi.yaml`, `schema.ts`, `ops.gen.ts`, the MCP tool schema, the `systemtests` SDK).** If the controller wants it, it is a ~30-line addition to Task 8 Step 4 and this plan says exactly where. |
| **C2** | **The controller's order is delinquency → consumer gate → fences. The recon's own argument is that the fences are the gate's only real exception.** | **P-A.** Order becomes: supervision fix → fences → gate. Stated with its reason so nobody reads it as drift. |
| **C3** | **The controller's item 6 says "the two reconcile gaps, together", but 6(a) needs a head field that does not exist and 6(b) does not, and R13 allows exactly one model edit.** | **P-B.** 6(b) (the zombie) is Task 7, before the model edit; 6(a) (the third head fact + the derived red node) is Task 9, immediately after it, with the re-open sweep (item 7) at Task 10 as its heal half. **Tasks 9 and 10 are ONE review pass and neither merges without the other** — a marker with no heal is a red node an operator cannot clear, and a heal with no marker is a sweep with nothing to key on. |
| **C4** | **🔴 R23 names "split `deliverymanager.go` by op family under the existing file-layout standard" — and the standard is exactly what FORBIDS it.** Measured: `TestFileLayout` (`internal/arch_test.go:61`, `arch.CheckFileLayout` from framework-go) enforces the 2026-07-11 standard, whose **Rule 1** is *one contract-implementation file* holding "**all** methods of the contract implementation, plus everything shared across workflows", whose **Rule 4** is *no other files*, and which says in as many words: *"Large files are an accepted consequence — `projectstateaccess.go` folds ~40 source files."* The file header of `deliverymanager.go:37-43` restates the allowed set. **A split by op family lands RED on a gate this repo has held at zero waivers since 2026-07-12.** | **Task 11 does the ONLY split the standard permits — Rule 2's re-homing:** a helper used by exactly one workflow belongs in that workflow's file. A first-cut scan while writing this plan found **107** top-level symbols in `deliverymanager.go` referenced by exactly one other non-generated file (all of them `deliveryactivity.go`); that is an UPPER BOUND, because a symbol also used by the Manager façade inside `deliverymanager.go` must stay. Task 11 measures the true set, moves it, and **records in the docs that "split by op family" is forbidden and that its real owners are (a) a platform amendment to the standard or (b) the facet wave, which removes the RA-shaped code rather than re-filing it.** Task 11 may legitimately land as a docs-only correction if the measured movable set is small; its acceptance is `TestFileLayout` green plus an honest number, never a line target. **This is the first of the three claims a reviewer should check.** |
| **C5** | **Recon says "13 signal channels in three receive forms". Measured at HEAD: there are TWELVE.** `grep -rn "GetSignalChannel" internal | grep -v _test | grep -v .gen.go` returns 12 call sites (1 delinquency + 1 supervision + 3 pump + 4 router + 1 lease-grant + 2 billing); the thirteenth line the recon's grep caught is **prose** at `deliveryactivity.go:3729` ("The hold reads the merge gate's INBOX, not `workflow.GetSignalChannel`"). | **Task 4's gate asserts the corpus size it walked** (files scanned and channels found) so a future path move cannot silently shrink it, and it uses **12**. Task 12 corrects the recon's number in the earmark file. Also confirmed out of scope and named as such in the gate's doc: `deliveryactivity.go:1338`, `:1895`, `:2769`, `:3882` are `workflow.NewChannel` inboxes and futures, not signal channels — **which is why the gate's dataflow starts at `GetSignalChannel` and not at `.Receive`.** |

---

## Scope — what is OUT of 4b3, and where each goes

Say it here so nobody re-derives it mid-wave. Each row carries the measurement that settles it.

| Out | Where it goes, and why |
|---|---|
| **The batched `QueryProjectView(plan)` (GAP-7)** | **DROPPED on a measured false premise (R5-architect).** There is no `plan` kind — `ProjectViewKind` has SEVEN (`summary, projects, session, pump, designHealth, episodes, timeline`, `delivery/contract.gen.go:642-651`) — and **the pump never touches `QueryProjectView` at all**: `readProject` (`pumpnextactivity.go:1412-1430`) calls `wf.Acts.DesignSessionReadProjectOnBranch(ctx, projectID, "")` inside a workflow, 1,161,922 B every 30 s, pinned by `manager_test.go:27696-27698`'s `reads < 2`. An eighth `ProjectViewKind` buys the pump **zero bytes**. The client half already ships (`miniLifecycleFromRow.ts`). The narrowed read the pump actually wants is a `projectStateAccess` op and it belongs to the facet wave. |
| **The PUSHED job-completion signal** | **DROPPED (R6-architect).** A new RA producer (a signature change on a 3-op contract) to buy LATENCY on a loop that works, against an observe ladder 4b1 tuned and 4b2 deliberately did not shorten. On a system with no users, latency on a polling loop is not a defect. Re-open only when a real run measures the poll cost. |
| **The census citation gate** | **DROPPED (R7-architect).** Both candidate designs fail — `AtHead` makes the field the thing that drifts, a content hash goes red on every unrelated pump edit — and the killer is measured: **an in-range check PASSED on the live failure** (`:1337` was in range in a 1,375-line file). Task 12 writes one sentence into the census saying the line citations are a human-navigation aid with no net, and gates nothing. |
| **The reachability pass AS FRAMED** | **SPLIT (R7-architect).** The import-graph orphan gate ships in **stage 6** (a ~15-line resolver already written during the 4b2 sweep; it also catches the ten construction modules imported only by their own tests). The `inFocus` branches — `ServiceContract.OPEN_FOCUS` / `CODE_CANVAS*` / `STRUCT_CARD`, which can never be true because nothing provides `FocusRailContext` and no caller passes `inFocus` — are **deleted by hand in stage 6** with the two orphan contexts. No identifier or import gate can ever see them; it is type-level dataflow over React branches. |
| **The three deprecated RA facets** | **NOT 4b3 — a COMPONENT PROMOTION in its own POST-DEPLOY wave (R4-architect).** The blocker is not the 12-op ceiling: **`designSessionAccess.ReadProjectOnBranch` IS the pump's whole-aggregate read on every wake-up** (`pumpnextactivity.go:1424`) and `commitArtifactWithProvenance` is how the child commits design slots, so deleting the facet **stalls the pump**. `projectCatalogAccess` exists NOWHERE in code — it is a sentence in the committed state (`project.json:29644`) and nothing else. And re-pointing the pump's read in 4b3 would move 7 call sites across 3 workflow types, invalidating **all fourteen** replay fixtures, whose re-capture needs the `temporal` CLI and a live dev server. Spend the fixture corpus once, in the wave that buys the whole collapse. |
| **The artifact-as-of-revision read (R1/GAP-5)** | **→ STAGE 6, and this plan owes the justification (P-C).** Four measured reasons. **(1) The handle is not ref-shaped:** `stagedRefString` (`deliveryactivity.go:1586-1591`) is the only `@v`-bearing producer in the module and it emits `<activityId>:<taskId>:<Kind>@v<version>`, **dropping `StagedRef.Branch`** which the RA return value carries; the `<branch>@v<n>` form 4b1 ratified came from `designSubjectRef`, deleted in `98e4a906`. Live state holds **ONE review round** and its `SubjectRef` is `{kind:"artifact", ref:"operationalConcepts"}`, backfilled — **no `@v` ref exists on this repo at all.** (2) The preferred resolution (`(activity,task,kind,version)` → commit, server-side) is a **history walk against depth-1 clones** whose cost nobody has measured, and the alternative is a persisted-format change needing a dual-form reader. (3) It would put a **tenth op on `projectStateAccess`** — the measured god component (47 ops / 10,864 lines across five facets) — in the very wave that names the facet collapse as its own post-deploy wave, and `QueryActivityView` cannot take the selector additively (positional params, path-routed, **9 surfaces**). (4) **The cost of waiting is a caption, not a defect:** the client half is built and honest — `?rev=` is validated at `router.tsx:108-115`, the screen goes read-only, and `HISTORY_ARTIFACT_CAPTION` says so with the reason written at `ActivityExperienceContainer.tsx:58-62`. **Stage 6 already opens a model edit (R20 + R21), so the read rides that one and the one-edit-per-wave budget still holds.** |
| **`operationalConcepts`'s drafting step / GAP-4B-4** | **NOT 4b3 (R17, founder).** Measured: it is now **ONE** orphan, not three — 4b2 mapped `scrubbedRequirements` and `standardCheck` to the EMPTY slug (`projectstateaccess.go:9295-9313`) — and the honest category is NINE (it plus the eight surviving Phase-2 draft slugs). **Both resolutions are method-assets releases and 4b3 changes neither.** Task 12 records the cost: `ReviewRound.artifactKind` stays defensive, and the ONE kinded round this repo holds judges a kind no lifecycle names. |
| **The construction answer command** | **NOT BUILT (R26/founder).** An agent answering a question about code it wrote, to clear a thread that gates its own merge, is the autogate hazard in costume — the one 4b1 had to close twice. The label already shipped (4b2 Task 10, `eb3a9f78`). |
| **The addressee vocabulary fix (R26's rider)** | **FOLDED into Task 8 Step 6 as a droppable RIDER**, because the fixture regen already rewrites the three files that carry the out-of-enum value (`construction-round-withdrawn.json:31408/:31428/:31519`, `"seniorDeveloper"`). Zero marginal cost while those files are open; delete the sub-step if the controller wants the wave narrower. |
| **The dual-write collapse, the estimation Engine merge, `RevenueShareNone`, the webApp orphan-module gate, the alias/meta-test holes, `deployment-linear.json`** | **STAGE 6 (R3-architect).** Stage 6 keeps ONE subject — "one thread store" — and it is a MIGRATION, not a deletion: `ArtifactSlot.ReviewThread` has 75 references and **14 of them are the agent-facing `cmd/aiarch-state-mcp` surface**. |
| **The double `ReadActivityExecution` on the approve path** | **Not 4b3.** Two git reads on the hottest write is a cost, not a defect, and it is inside the code the facet wave re-homes. Re-earmarked by Task 12. |
| **The LIVE DOUBLE-WRITE (`GitStatusRecordActivityStarted` + `ActivityExecutionOpenActivity` on the same row in the same walk)** | **STAGE 6 with the dual-write collapse.** Nobody has said which wins; deciding that is the facet question, and 4b2 measured that deleting the git-status verbs **stalls the pump**. |

---

## Task order

**The two fixes the gate needs, then the gate:**
Task 1 (the delinquency payload) → Task 2 (`projectsupervision.go:48`) → Task 3 (the five fences discharged) → **Task 4 (THE CONSUMER ARCH GATE)**.

**Then the state items that need no wire:**
Task 5 (per-task session state) → Task 6 (the late-approve refusal, rides 5) → Task 7 (the zombie probe).

**Then the one model edit, and the two items that were waiting on it:**
**Task 8 (THE ONE MODEL EDIT + THE FIXTURE REGEN)** → Task 9 (the third head fact + the derived red node) → Task 10 (the re-open sweep).

**Then the file, the record and the release:**
Task 11 (the sanctioned re-homing) → Task 12 (docs, spec corrections, earmarks, the measurement) → **Task 13 (DRAIN → MERGE → TAG → DEPLOY)**.

**Must ship together:**
- **Task 3 is ONE commit**: the five fence call sites, the four census rows (G-P18…G-P21), the census DOC's rows and headline numbers, and the five `…_DefaultVersion_…` tests. A fence deleted without its census row leaves `Test_PumpGuardCensus_TheDocAndTheCodeAgree` red; a census row deleted without its test leaves a pin naming a test that does not exist.
- **Task 4 is ONE commit**: the gate, the `testdata/wireform/` fixture corpus and the meta-test that asserts the gate is red on each fixture. A gate without its red proof is exactly the artifact R6 forbids.
- **Task 8 is ONE commit** and it runs the self-amendment loop exactly once. It is the ONLY task that edits `project.json`.
- **Tasks 9 and 10 are ONE review pass** (C3) and neither merges without the other.
- **Task 13's drain steps run in the printed order**, and step 5's three `temporal schedule delete` calls happen BEFORE the release, not after.

**Parallelism.** Tasks 1, 2 and 5 are genuinely disjoint (one `operations`+`billing` change, one `delivery` line, one `delivery` struct) and may run in parallel. **Task 2 must land before Task 4. Task 3 must land before Task 4. Task 5 must land before Task 6.** Everything from Task 8 onward is strictly sequential.

---

## Execution risks and how this plan removes each

1. **The consumer gate ships green and blind — the wave's dominant risk, named by R6.** 4b2 shipped the PRODUCER gate, it was green all wave, and the CONSUMER defect shipped **inert** and was found by a real-server capture, not by a test. The gate must follow a channel across a struct field (`pumpChannels`), a closure param (`AddReceive(ch, func(c …))`) and a helper func (`pumpReceiveSignal`) — three indirections, any one of which, missed, yields a gate that passes while `projectsupervision.go:48` and the delinquency consumer stand. **Removed three ways, all in Task 4:** (a) the gate's decision rule is inverted — **a channel whose flow the gate cannot follow is a FAILURE, not a pass**, with a message telling the author to route it through a sanctioned decoder; (b) a `testdata/wireform/` corpus carries the three known instances in the three receive forms and a meta-test asserts the gate flags **exactly** them; (c) the gate asserts the size of the corpus it walked, so a path move cannot silently shrink it.
2. **Task 9's subject is an INVISIBLE failure, so a wrong fix is invisible by construction (R11).** Its test must construct a genuinely broken merge tail — a `commitDesignArtifacts` that errors after `finalizeActivity` has recorded `Completed` — and assert the DERIVED red node through the same projection the SPA reads. Asserting over a hand-built row is how 4b2's `roundRevisions` fix certified a shape that could never match, and the test's fourth assertion passed a round with no attempts supplied.
3. **Task 7's probe can hand a child a lease it never asked for, which would create the second concurrent main-writer the lease exists to prevent.** Measured while writing this plan and it is the task's whole design constraint: a grant re-delivered to a non-holder **buffers on `activityLeaseGranted`**, and when that child later reaches `requestMainWriteLease` it would consume the buffered grant and run its tail believing it holds a lease. **The seam already exists and is the answer:** `pumpLivenessProbeEpoch` (`pumpnextactivity.go:312`, `= 0`) is a child-side FLOOR read at exactly one site — `deliveryactivity.go:3474`'s `case grant.Epoch <= pumpLivenessProbeEpoch || grant.ActivityID != in.ActivityID: continue` — so a grant at epoch 0 is a message the child is REQUIRED to ignore, by a rule that is already written, already commented and already the documented meaning of the constant. Task 7 sends exactly that and nothing else. **Its doc comment currently says "NO SIGNAL THE PUMP SENDS EVER CARRIES IT" and Task 7 owes that correction at the site.**
4. **Task 8 is the LARGEST item and it fails loudly — which is why it is not the riskiest.** 4a's equivalent was 112 files, 4 generated layers, 3 test goldens, 20 fixtures and 92 registered names. The failure modes are a gate, a typecheck or a golden. **The one part of it that can fail QUIETLY is the fixture regen**, because 57/57 are green over the stale world today: a regen that rewrites the wrong sub-tree, or writes a shape the panel cannot read, would be caught only by a browser. Task 8 therefore lands the **gate first** (fixture `ServiceContracts` key-set and per-contract op-count vs `.aiarch/state/project.json`), watches it go RED over the 17 stale fixtures, and only then regenerates.
5. **`make gen-sdk` deletes SDK files a separate module's hand-written harness calls.** Task 8 changes no op, so no SDK file should be pruned — **and that prediction is itself the check**: if `pruneStaleSDK` removes a file, an op moved and Task 8 did something it did not intend. `git status systemtests/` is read before the commit.
6. **A vocabulary member added mid-sum-type is a renumber; appended is not.** Task 8 appends `ActivityConstructionPhase` member **4** (`completedNotLanded`) and appends nothing else. Ordinals 0–3 are wire values in every committed project; `TestRetiredKinds_KeepTheirOrdinals` is the precedent guard. **And `gochecksumtype` will find every `switch` that must gain an arm** — measured: `ActivityConstructionDone` appears in 11 files, of which 4 are non-test non-generated.
7. **Task 11 can only disappoint, and the plan says so in advance.** C4 means the deliverable may be a docs correction plus a small move. An implementer who "makes progress" by adding a second impl file lands a red `TestFileLayout` and has to revert it. The task's first step is to READ the standard and print the allowed file set.
8. **`Test_LifecycleShapes` grows from 11 to 15 cases in a plan that runs subagents in parallel.** Every task that adds a case runs it ALONE and says so; a failure observed while another task's `go test ./...` is running is re-run before it is believed.

---

### Task 1: The delinquency payload — one flat shape, one three-value action, and `unknown` is refused

**The founder's ruling, and the honest sizing that goes with it.** The producer **cannot fire today**: `billingStateAccess` is the arm-less generated stub in every profile (`internal/resourceaccess/billingstate/contract.gen.go:101-106`; `cmd/server/main.gen.go:427` constructs `billingstate.NewBillingStateAccess()`), so `ReadPersistentlyDelinquentCustomers` returns `fwra.Unknown` on every hourly firing and `ShortfallSweepWorkflow` takes its quiet-no-op arm (`shortfallsweep.go:49-55`) **before** reaching `deliverDelinquencySignal`. The controller told the founder twice that this was live; it is not, and this plan says so where the fix lands. **What makes it worth doing properly rather than fast is the other half:** the façade path DOES work today (`operationsManager.ApplyDelinquencyPolicy` is reachable over REST and MCP), and the zero value of the one field means **WITHDRAW**, which is the only arm that acts — the pause arm is a log line, deliberately (`delinquencyenforcement.go:77-91`). A wire-form-only fix would turn a dropped signal into a **hard withdraw of every in-flight app of any flagged customer**.

Measured shapes, re-verified while writing this plan:

```go
// PRODUCER — internal/manager/billing/shortfallsweep.go:96-99
type deliverSignalPayload struct { CustomerID customerID; PauseNotWithdraw bool }          // FLAT, raw []byte

// CONSUMER — internal/manager/operations/delinquencyenforcement.go:30
type applyDelinquencySignal struct { CustomerID customerID; Context DelinquencyContext }   // NESTED, concrete-struct receive

// operations/contract.gen.go:58-60 — the whole of the difference
type DelinquencyContext struct { PauseNotWithdraw bool `json:"pauseNotWithdraw"` }
```

**Files:**
- Modify: `server/internal/manager/operations/delinquencyenforcement.go` — `applyDelinquencySignal` (`:28-33`), `DelinquencyEnforcementWorkflow`'s receive (`:49-53`), `runDelinquencyBranch` (`:62-105`).
- Modify: `server/internal/manager/operations/operationsmanager.go` — `ApplyDelinquencyPolicy` (`:490-509`), the signal construction at `:497`.
- Modify: `server/internal/manager/billing/shortfallsweep.go` — delete `deliverSignalPayload` (`:96-99`), rewrite `deliverDelinquencySignal` (`:113-122`).
- Modify: `server/internal/manager/operations/manager_test.go` — the two existing signal cases at `:1432` and `:1465`.
- Modify: `server/internal/manager/billing/manager_test.go` — the sweep's delivery case.

**Interfaces produced (Task 4 consumes the first two as its regression case):**
- `operations.applyDelinquencySignal{CustomerID customerID; Action delinquencyAction}` — FLAT, Manager-internal, **not a contract type**.
- `operations.delinquencyAction` — `int`-based, `delinquencyActionUnknown = 0`, `delinquencyActionPause = 1`, `delinquencyActionWithdraw = 2`, with a `String()`.
- `operations.decodeDelinquencySignal(raw any) (applyDelinquencySignal, error)` — the `any`-first normaliser, the shape `pumpDecodeSignal` established.
- Billing keeps NO payload type of its own: `deliverDelinquencySignal` marshals an anonymous struct with the two json tags, and a **cross-package shape test** pins the two json key sets equal.

- [ ] **Step 1: Write the failing test — an empty payload must REFUSE, not withdraw.**

  In `operations/manager_test.go`:
  ```go
  // Test_Delinquency_AnUnsaidActionIsRefusedRatherThanWithdrawn is the whole point of the
  // vocabulary change. Before it, applyDelinquencySignal's zero value decoded to
  // DelinquencyContext{PauseNotWithdraw:false}, runDelinquencyBranch read that as
  // DelinquencyActionWithdrawn, and withdrawRuntime removed the runtime of EVERY in-flight
  // app of the customer — so "nobody said" and "withdraw" were the same value. A bool
  // cannot express the difference; a three-member vocabulary can, and the enforcement
  // branch refuses the unknown member instead of acting on it.
  func Test_Delinquency_AnUnsaidActionIsRefusedRatherThanWithdrawn(t *testing.T) { … }
  ```
  Drive `DelinquencyEnforcementWorkflow` in the Temporal test environment, signal `applyDelinquencyPolicy` with **raw bytes of `{"CustomerID":"<uuid>"}`** (no action member at all — the exact shape a producer that forgets the field sends), seed one in-flight app, and assert **two** things: the workflow returns an error whose message names the customer and the missing action, and `withdrawRuntime` was called **zero** times.
  - [ ] **Verify first:** `grep -n 'func (wf \*workflows) withdrawRuntime' -A 10 internal/manager/operations/*.go` and confirm which double the environment registers, so "zero calls" is assertable rather than hoped for.
  - [ ] Run: `GOWORK=off go test ./internal/manager/operations/ -run Test_Delinquency_AnUnsaid -count=1 -v`. Expected: **FAIL** — today the signal is dropped entirely (concrete-struct receive over binary/plain), so the workflow blocks and the case times out. That timeout **is** the red.

- [ ] **Step 2: Write the second failing test — the bus wire form must decode.**

  ```go
  // Test_Delinquency_TheBusWireFormIsDecoded pins the half the SDK drops today. The billing
  // sweep delivers through messageBus.deliverSignal, which hands the client a bare []byte,
  // so the default converter tags the payload binary/plain and ByteSlicePayloadConverter can
  // assign it to nothing but a *[]byte. The receive therefore decodes into `any` FIRST and
  // normalises both forms — the pumpReceiveSignal pattern, which this package did not
  // inherit when it was written.
  func Test_Delinquency_TheBusWireFormIsDecoded(t *testing.T) { … }
  ```
  Two sub-cases over the SAME workflow: `env.SignalWorkflow(signalApplyDelinquencyPolicy, rawJSONBytes)` (the bus form) and `env.SignalWorkflow(signalApplyDelinquencyPolicy, applyDelinquencySignal{…})` (the façade's struct form). **Both** must reach `runDelinquencyBranch` with `Action == delinquencyActionPause` and record `DelinquencyActionPaused`.
  - [ ] Run it. Expected: the **bytes** sub-case FAILS (timeout / "Corrupted signal" in the log), the struct sub-case passes.

- [ ] **Step 3: Introduce the vocabulary and the flat payload.**

  In `delinquencyenforcement.go`, replacing `applyDelinquencySignal`:
  ```go
  // delinquencyAction is what the enforcement branch is TOLD to do, and it has three members
  // because a bool has two and the missing one is the important one. `unknown` is what a
  // payload that named no action decodes to, and the branch REFUSES it — where the retired
  // bool made "nobody said" and "withdraw" the same value, and withdraw is the only arm that
  // acts (the pause arm publishes nothing; see runDelinquencyBranch). This is the rule 4b1
  // already wrote at the planning-assumptions site: a field whose value is its vocabulary's
  // UNKNOWN member is absent in the only sense that matters.
  type delinquencyAction int

  const (
      delinquencyActionUnknown  delinquencyAction = 0
      delinquencyActionPause    delinquencyAction = 1
      delinquencyActionWithdraw delinquencyAction = 2
  )

  func (a delinquencyAction) String() string {
      switch a {
      case delinquencyActionPause:
          return "pause"
      case delinquencyActionWithdraw:
          return "withdraw"
      case delinquencyActionUnknown:
          return "unknown"
      }
      return "unknown"
  }

  // applyDelinquencySignal is the payload of the ONE sanctioned queued Manager→Manager edge,
  // and it is FLAT because both producers are flat: billing has always sent
  // {CustomerID, PauseNotWithdraw} and the façade nested the same single bit one level
  // deeper. The nesting was the whole of the mismatch (the contract type DelinquencyContext
  // has exactly one member), so flattening costs no contract change and makes the two
  // producers converge on ONE payload type — which is what the missing gate would have
  // required anyway.
  type applyDelinquencySignal struct {
      CustomerID customerID        `json:"CustomerID"`
      Action     delinquencyAction `json:"Action"`
  }
  ```
  - [ ] **Verify first:** `grep -rn 'applyDelinquencySignal' internal/manager/operations/` — every site must be updated in this step; there are three non-test (`:30`, `:50`, `operationsmanager.go:497`) and two test.

- [ ] **Step 4: Receive into `any` and normalise both wire forms.**

  ```go
  // decodeDelinquencySignal normalises the TWO wire forms this channel really carries, and
  // the reason is transport, not taste: the façade sends a STRUCT through
  // SignalWithStartWorkflow (json/plain) while the billing sweep sends raw []byte through
  // messageBus.deliverSignal (binary/plain), and a concrete-struct receive target can hold
  // the second one not at all — the SDK logs "Corrupted signal received on channel …" and
  // DROPS it. That silence is what hid the whole stage-4b2 main-write lease for a wave, and
  // it is what an arch gate now forbids for every name any deliverSignal producer uses.
  func decodeDelinquencySignal(raw any) (applyDelinquencySignal, error) {
      switch v := raw.(type) {
      case []byte:
          var sig applyDelinquencySignal
          if err := json.Unmarshal(v, &sig); err != nil {
              return applyDelinquencySignal{}, err
          }
          return sig, nil
      case map[string]any:
          b, err := json.Marshal(v)
          if err != nil {
              return applyDelinquencySignal{}, err
          }
          var sig applyDelinquencySignal
          if err := json.Unmarshal(b, &sig); err != nil {
              return applyDelinquencySignal{}, err
          }
          return sig, nil
      }
      return applyDelinquencySignal{}, fmt.Errorf(
          "applyDelinquencyPolicy: unreadable payload of type %T", raw)
  }
  ```
  and the receive becomes:
  ```go
  	sigCh := workflow.GetSignalChannel(ctx, signalApplyDelinquencyPolicy)
  	var raw any
  	sigCh.Receive(ctx, &raw)
  	sig, derr := decodeDelinquencySignal(raw)
  	if derr != nil {
  		// AN UNREADABLE DELINQUENCY MESSAGE IS A REFUSAL, NOT A DEFAULT. The channel name
  		// carries no intent here (unlike a pause, whose NAME is the operator's intent), and
  		// the only action a zero value could select is the destructive one.
  		return derr
  	}
  	return wf.runDelinquencyBranch(ctx, in.CustomerID, sig.Action)
  ```
  - [ ] **Verify first:** `sed -n '295,315p' internal/manager/delivery/pumpnextactivity.go` and `grep -n 'func pumpDecodeSignal' -A 25 internal/manager/delivery/pumpnextactivity.go` — reproduce that pattern rather than inventing one; the two packages may not share code (different Managers), so the duplication is deliberate and the comment says so.

- [ ] **Step 5: Refuse `unknown` in the branch, and state the pause arm's limitation as a refusal-shaped note.**

  ```go
  func (wf *workflows) runDelinquencyBranch(ctx workflow.Context, customerID customerID, action delinquencyAction) error {
  	logger := workflow.GetLogger(ctx)
  	switch action {
  	case delinquencyActionPause, delinquencyActionWithdraw:
  	case delinquencyActionUnknown:
  		// REFUSED, LOUDLY. The retired bool's zero value selected WITHDRAW, so a payload that
  		// named no action removed the runtime of every in-flight app of this customer. There
  		// is no safe default here: pause publishes nothing today and withdraw is irreversible,
  		// so the only honest answer to "nobody said" is to stop and say so.
  		return temporal.NewNonRetryableApplicationError(
  			"applyDelinquencyPolicy for customer "+customerID.String()+" named no action; "+
  				"pause and withdraw are the two this branch enforces and neither may be assumed",
  			"DelinquencyActionUnsaid", nil)
  	}
  	…
  	state := operatedsystemstate.DelinquencyActionWithdrawn
  	if action == delinquencyActionPause {
  		state = operatedsystemstate.DelinquencyActionPaused
  	}
  ```
  Keep the existing per-app loop verbatim, reading `action == delinquencyActionPause` where it read `dctx.PauseNotWithdraw`, and **keep the pause arm's existing comment** (the replica-override earmark) unchanged — it is accurate and it is the reason `pause` records head state and publishes nothing.
  - [ ] **Verify first:** `grep -n 'temporal.NewNonRetryableApplicationError' internal/manager/operations/*.go | head -3` — if the package does not already import `go.temporal.io/sdk/temporal`, use the error shape it does use rather than adding an import for one line, and say which in the commit.

- [ ] **Step 6: The façade maps the contract bool onto the action, and says what it cannot express.**

  `operationsmanager.go:497`:
  ```go
  	// THE FAÇADE CANNOT SAY "NOBODY SAID", AND THAT IS RECORDED RATHER THAN HIDDEN.
  	// DelinquencyContext.pauseNotWithdraw is a non-pointer bool on the generated REST and MCP
  	// surfaces, so a request body that omits it decodes as false and arrives here
  	// indistinguishable from an explicit withdraw. Mapping it to the enum keeps today's
  	// semantics exactly and moves the three-value vocabulary to where a MISSING payload can
  	// still be caught (the workflow's own decode). Closing the outer half is a $defs change —
  	// pauseNotWithdraw -> a delinquencyAction enum — across six generated surfaces, and it is
  	// earmarked rather than smuggled into a Manager body.
  	action := delinquencyActionWithdraw
  	if delinquencyContext.PauseNotWithdraw {
  		action = delinquencyActionPause
  	}
  	sig := applyDelinquencySignal{CustomerID: customerID, Action: action}
  ```

- [ ] **Step 7: Billing sends the one shape, and stops decorating the payload.**

  Delete `deliverSignalPayload` (`shortfallsweep.go:96-99`) and rewrite the delivery:
  ```go
  // deliverDelinquencySignal invokes messageBus.deliverSignal — the one sanctioned queued
  // M→M edge. THE PAYLOAD IS NOT MIRRORED ANY MORE: billing may not import operations (the
  // signal name is a string literal here for exactly that reason), so the shape is written
  // inline and PINNED by a cross-package json-key test rather than by a struct nobody
  // compiler-links. Its keys are the operations-side applyDelinquencySignal's, flat, and the
  // action is a small integer whose ZERO value the receiver refuses.
  //
  // NO ContentType. messagebus.ExecutionPayload.ContentType is declared once and read by
  // NOTHING (contract.gen.go:23; DeliverSignal passes payload.Bytes alone, messagebus.go:162,
  // and the utility's own header says "this utility is a transport, not a serialiser"). A
  // producer that sets it is telling its reader the transport serialises for them, which is
  // the belief that dropped every lease message for a whole wave.
  func (wf *workflows) deliverDelinquencySignal(ctx workflow.Context, customerID customerID, pauseNotWithdraw bool) error {
  	action := delinquencyActionWithdrawWire
  	if pauseNotWithdraw {
  		action = delinquencyActionPauseWire
  	}
  	bytes, err := json.Marshal(struct {
  		CustomerID customerID `json:"CustomerID"`
  		Action     int        `json:"Action"`
  	}{CustomerID: customerID, Action: action})
  	if err != nil {
  		return err
  	}
  	return wf.Acts.MessageBusDeliverSignal(ctx,
  		messagebus.ExecutionID(fmt.Sprintf("%s:delinquency", customerID)),
  		messagebus.SignalName(signalApplyDelinquencyPolicy),
  		messagebus.ExecutionPayload{Bytes: bytes})
  }

  // The two wire values billing may send. They mirror operations.delinquencyAction's
  // ordinals and are pinned against them by Test_Delinquency_TheTwoPackagesAgreeOnTheWire.
  const (
  	delinquencyActionPauseWire    = 1
  	delinquencyActionWithdrawWire = 2
  )
  ```

- [ ] **Step 8: The cross-package shape test, because nothing compiler-links the two sides.**

  In `operations/manager_test.go` (the side that owns the vocabulary):
  ```go
  // Test_Delinquency_TheTwoPackagesAgreeOnTheWire is the gate the hand-mirrored payload never
  // had. billing may not import operations, so the two shapes are linked by nothing but this
  // assertion: it reads billing/shortfallsweep.go as TEXT, extracts the json tags and the two
  // wire ordinals, and compares them with operations' own struct tags and constants. The
  // mirror is the anomaly the file's own readDelinquent comment boasts about not having
  // ("the workflow speaks the generated billingstate contract types directly — no
  // Manager-local mirror"), and this is what makes the remaining inline shape honest.
  func Test_Delinquency_TheTwoPackagesAgreeOnTheWire(t *testing.T) { … }
  ```
  Assert: the literal `"CustomerID"` and `"Action"` both appear in billing's anonymous struct; `delinquencyActionPauseWire = 1` and `delinquencyActionWithdrawWire = 2` match `delinquencyActionPause`/`delinquencyActionWithdraw`; and billing's file contains **no** `ContentType`.
  - [ ] **Mutation-check it:** change `delinquencyActionPauseWire` to `3`, run, watch it go RED, restore. Paste both outputs into the commit message.

- [ ] **Step 9: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/manager/operations/ ./internal/manager/billing/ -count=1
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint fix-check sumtype-check vet
  GOWORK=off go test ./internal/ -run 'TestRegisteredTemporalNamesGolden|TestMethodLayering|TestFileLayout' -count=1
  ```
  Expected: green. **Every gate number unchanged — golden 133, frozen 15, shapes 11/11, replay 8/8 + 6/6, census 36, validate 43/0, npm 1237, preview 57/23.** No contract type changed, so no generated surface moves and no `systemtests` regen is owed. **`Test_DeliverSignal_TheWireFormProducersAreAClosedList` stays at four names** — the delinquency producer is in `billing`, and that gate is package-scoped to `delivery`; Task 4 is what finally sees it.
  ```bash
  git add server/internal/manager/operations server/internal/manager/billing
  git commit -F - <<'MSG'
  fix(operations,billing): the delinquency signal gets one flat shape and an action that can say nobody said

  The producer and the consumer carried the SAME single bit, one flat and one
  nested a level deeper, and the nesting was the whole of the mismatch:
  DelinquencyContext has exactly one member. So the payload flattens onto
  billing's shape, both producers converge on one type, and no contract changes.

  The bool goes, and that is the part that matters. Its zero value selected
  WITHDRAW, and withdraw is the only arm that acts — the pause arm records head
  state and publishes nothing, deliberately, because there is no replica-override
  path yet. So "nobody said" and "remove this customer's runtime" were the same
  value, and a wire-form fix on its own would have started hard-withdrawing every
  in-flight app of any flagged customer. delinquencyAction has three members and
  the branch refuses the unknown one.

  The consumer now receives into `any` and normalises both wire forms, because
  the facade sends a struct (json/plain) and the billing sweep sends raw bytes
  (binary/plain) and a concrete-struct target holds the second one not at all.
  The producer stops setting ExecutionPayload.ContentType, a field read by
  nothing, whose presence is the strongest signal its author believed the
  transport serialised for them.

  HONEST SIZING: this is LATENT, not live. billingStateAccess is the arm-less
  stub in every profile, so the sweep no-ops before the send and nothing has ever
  been paused or withdrawn for delinquency by it. The facade path does work, and
  that is what made it worth fixing properly rather than fast.

  What this does NOT close: the facade's own pauseNotWithdraw is a non-pointer
  bool on REST and MCP, so a caller who omits it still arrives as WITHDRAW.
  Closing that is a $defs enum swap across six surfaces and it is earmarked.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 2: `projectsupervision.go:48` — the latent instance is fixed before the gate that would flag it

**One line of behaviour, and it must not be waived.** `ProjectSupervisionWorkflow` opens `operatorPauseRequested` and receives it into a concrete struct:

```go
// internal/manager/delivery/projectsupervision.go:46-48 — measured at HEAD
	pauseCh := workflow.GetSignalChannel(ctx, signalOperatorPauseRequested)
	var sig operatorPauseSignal
	pauseCh.Receive(ctx, &sig)
```

It is **correct today and wrong tomorrow.** Its one producer is `deliverymanager.go:5908`'s `client.SignalWithStartWorkflow` with a struct (json/plain), so nothing is dropped — but the signal NAME is the same `operatorPauseRequested` that `relayPauseToPump` (`projectsupervision.go:148`) delivers as **raw bytes** to a *different* execution id. The day anything relays a pause to `{p}:construction` through the bus, supervision drops it silently. **A name-keyed gate flags this; an id-keyed gate would clear it** — which is the measured argument for name-keying (R4), so the gate cannot ship with this as a waiver for the defect it exists to prevent.

**Files:**
- Modify: `server/internal/manager/delivery/projectsupervision.go` (`:46-53`).
- Modify: `server/internal/manager/delivery/manager_test.go` — one new case beside the existing supervision cases.

**Interfaces:** none new. Consumes `pumpReceiveSignalBlocking` and `pumpDecodeSignal`, which already exist in `pumpnextactivity.go` and are **in the same package** — so unlike Task 1 there is no duplication to justify.

- [ ] **Step 1: Write the failing test — the bus wire form must reach the pause branch.**
  ```go
  // Test_Supervision_ARelayedPauseIsNotDropped pins the LATENT half of the wire-form defect.
  // The supervision workflow's operatorPauseRequested receive is correct today only because
  // of WHICH producer happens to reach WHICH execution id — a fact no local reading of this
  // consumer can establish, and exactly the reason the arch gate keys on the NAME. The same
  // name is already delivered as raw bytes to the pump by relayPauseToPump, so the first
  // relay aimed at {p}:construction would be dropped with nothing going red.
  func Test_Supervision_ARelayedPauseIsNotDropped(t *testing.T) { … }
  ```
  Signal the workflow with **`[]byte`** of `{"Reason":"operator relayed"}` and assert `runPauseBranch` ran with that reason (the existing supervision cases show how the intervention double is asserted).
  - [ ] **Verify first:** `grep -n 'operatorPauseSignal' internal/manager/delivery/*.go | grep -v _test` — confirm the struct's json shape, because the bytes in the test must match what a real relay marshals (`projectsupervision.go:143-156`).
  - [ ] Run: `GOWORK=off go test ./internal/manager/delivery/ -run Test_Supervision_ARelayedPause -count=1 -v`. Expected: **FAIL/timeout** — the SDK logs `Corrupted signal received on channel operatorPauseRequested … type *delivery.operatorPauseSignal: type is not *[]byte` and the workflow never returns.

- [ ] **Step 2: Receive into `any`, through the decoder the package already owns.**
  ```go
  	// RECEIVED INTO `any`, NOT INTO THE STRUCT (stage 4b3 Task 2). The producer that reaches
  	// THIS execution sends a struct today, so nothing was being dropped — but operatorPauseRequested
  	// is also a messageBus.deliverSignal name (relayPauseToPump), and a bus payload is raw []byte
  	// tagged binary/plain, which a concrete-struct target can hold not at all. Which producer
  	// reaches which execution id is not a fact this file can establish, so the wire form is
  	// decided by the NAME. pumpReceiveSignalBlocking normalises both forms and is the same
  	// decoder the pump's own channels use.
  	pauseCh := workflow.GetSignalChannel(ctx, signalOperatorPauseRequested)
  	var sig operatorPauseSignal
  	if err := pumpReceiveSignalBlocking(ctx, pauseCh, &sig); err != nil {
  		// A PAUSE WHOSE BODY CANNOT BE READ IS STILL A PAUSE. The channel NAME carries the
  		// operator's intent, which is why this is the opposite of the lease rule (an undecodable
  		// lease message names nobody and is dropped). The reason is lost; the pause is not.
  		workflow.GetLogger(ctx).Error("supervision: the pause payload could not be read; pausing with no stated reason",
  			"projectId", string(in.ProjectID), "err", err.Error())
  	}
  	return wf.runPauseBranch(ctx, in.ProjectID, sig.Reason, state)
  ```
  - [ ] **Verify first:** `grep -n 'func pumpReceiveSignalBlocking' -A 20 internal/manager/delivery/pumpnextactivity.go` — confirm the signature and whether the closed `pumpLeaseSignal` type constraint admits `operatorPauseSignal`. **If it does not, widen the constraint in this task and say so**; do not copy the decoder.
  - [ ] Run both the new case and the existing supervision cases: expected **all green**.

- [ ] **Step 3: Assert the struct form still works.** Add a sibling sub-case signalling `operatorPauseSignal{Reason:"direct"}` (json/plain) and assert the same branch runs. **Both forms, one channel** — the rule the gate will enforce.

- [ ] **Step 4: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Supervision|Test_Pause_' -count=1 -v
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Replay_' -count=1
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint fix-check sumtype-check vet
  ```
  Expected: green, **replay 8/8 + 6/6 UNMOVED**. A decode change emits no workflow command, which is exactly what `37b0e768` measured when the lease channels were fixed — the eight pre-4b2 child fixtures stayed 8/8. **One supervision replay fixture exists (`supervision-pause-record-relay-cancel`, 26 events) and it must stay green; if it moves, the change emitted a command and the reason must be found before anything else proceeds.** Every other gate number unchanged.
  ```bash
  git add server/internal/manager/delivery/projectsupervision.go server/internal/manager/delivery/manager_test.go
  git commit -F - <<'MSG'
  fix(delivery): supervision reads a pause the way the bus can send one

  operatorPauseRequested is received into a concrete struct here, and that is
  correct only because of which producer happens to reach which execution id —
  a fact no local reading of this consumer can establish. The same name is
  delivered as raw bytes to the pump by relayPauseToPump, and a bus payload is
  binary/plain, which a struct target can hold not at all. The first relay aimed
  at {p}:construction would have been dropped with nothing going red.

  Fixed BEFORE the arch gate that flags it, deliberately: a gate that arrives
  with a waiver for the defect it exists to prevent is not a gate.

  An unreadable pause still pauses — the channel NAME carries the operator's
  intent. That is the opposite of the lease rule, where a message whose activity
  id cannot be read names nobody and is dropped.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 3: The five pump `GetVersion` fences are discharged — with their census rows and their `DefaultVersion` tests, as ONE edit

**4b2 refused to discharge them because the drain had not run. With no production users the drain's purpose is gone (R1), and discharging removes the consumer gate's only live exception (P-A).** Measured at HEAD — **five `GetVersion` call expressions over SIX change ids**, and the file's own comment at `pumpnextactivity.go:1255-1258` says *"Five of them over four change ids"*, which is **wrong** and is a doc correction this task owes:

| # | Call site | Change id(s) | What the `DefaultVersion` arm does |
|---|---|---|---|
| 1 | `pumpnextactivity.go:1266` (`pumpPausedBehindGate`) | `"pump-pause-before-dispatch"` (called at `:798`) and `"pump-drain-pause-before-continue-as-new"` (called at `:1194`) — **one func, two ids** | skips the pause check entirely |
| 2 | `:1282` (`pumpHonorsRecordedPause`) | `changePumpHonorsRecordedPause` = `"pump-honors-recorded-pause"` (`:1274`), **v2** | no gate on the recorded pause |
| 3 | `:1299` (`pumpPausedAtRunStart`) | `"pump-pause-decode-any"` | **receives into a concrete struct on purpose** — the gate's one live exception |
| 4 | `:1360` (`pumpEligibilityRule` rung 1) | `changeLedgerPartialResume` (declared `deliverymanager.go:7621`) | `eligibleNotStarted` |
| 5 | `:1363` (`pumpEligibilityRule` rung 2) | `changeDesignActivitiesDispatchable` (`:1347`) | `eligibleDispatchable` |

Four census rows hang off them — **G-P18, G-P19, G-P20, G-P21** (`manager_test.go`, `pumpGuardCensusPump()`) — and **five `…_DefaultVersion_…` tests** are those rows' only pin: `Test_Pump_PreDispatchGate_DefaultVersion_KeepsOldDispatch` (`:24548`), `Test_Pump_DrainGate_DefaultVersion_ContinuesAsNew` (`:24574`), `Test_Pump_DecodeGate_DefaultVersion_KeepsOldStructDecode` (`:24610`), `Test_Pump_RecordedPauseGate_DefaultVersion_StillDispatches` (`:24729`), `Test_Pump_LedgerPartialResume_DefaultVersion_KeepsTheOldSelection` (`:25105`).

**Two measurements travel with this edit as counter-evidence, and they must be in the commit message:** (1) **a replay fixture does NOT pin a `GetVersion` rung** — deleting the `changeDesignActivitiesDispatchable` rung left all six pump fixtures GREEN, because the SDK tolerates a recorded `Version` marker the replayed code never asks for, whereas deleting the pace `Sleep` gives `[TMPRL1100] a matching Timer command was expected in history event position 34` on 2 of 6; (2) **no capture can ever produce a `DefaultVersion` history**, because `GetVersion` returns `maxSupported` on a new execution. So "replay is 14/14 green" is **not** licence for this edit — the founder's no-users ruling is.

**OUT of this task, and named so nobody widens it:** `changeActivityMainWriteLease` (`deliveryactivity.go:3405`), `changeRowConflictReread`, `changeOperatorNoteDelivery`, `changeExecutionLedger`. Those fence the CHILD and the Manager, **eight captured `deliveryActivity` histories replay against that body**, and a new Activity command in a recorded position is a non-determinism failure. This task touches the PUMP's five and nothing else.

**Files:**
- Modify: `server/internal/manager/delivery/pumpnextactivity.go` — `:798`, `:1192-1196`, `:1253-1310`, `:1345-1370`.
- Modify: `server/internal/manager/delivery/deliverymanager.go` — `changeLedgerPartialResume` (`:7621`) if it loses its last reader (**verify: it may be read by the child or the Manager too**).
- Modify: `server/internal/manager/delivery/manager_test.go` — `pumpGuardCensusPump()` (4 rows out) and the five `…_DefaultVersion_…` tests (out).
- Modify: `docs/bugs/2026-09-28-pump-guard-census.md` — the four rows and the headline numbers.

**Interfaces produced:** `pumpPausedBehindGate(ctx, ch)` loses its `changeID` parameter; `pumpPausedAtRunStart(ctx, ch)` keeps its signature and loses its struct arm; `pumpEligibilityRule(ctx)` **becomes `pumpEligibilityRule()`** and always returns `eligibleWithDesign`. Task 4's gate and Task 7's probe both read this file afterwards.

- [ ] **Step 1: Verify the fence inventory before deleting anything.**
  ```bash
  cd .../server
  grep -n 'GetVersion' internal/manager/delivery/pumpnextactivity.go
  grep -rn 'changeLedgerPartialResume\|changeDesignActivitiesDispatchable\|changePumpHonorsRecordedPause\|pump-pause-decode-any\|pump-pause-before-dispatch\|pump-drain-pause-before-continue-as-new' internal/ | grep -v '\.gen\.go'
  ```
  Expected: five `GetVersion` lines in the pump; the six ids reaching only the sites tabled above **plus their census rows and `DefaultVersion` tests**. **If `changeLedgerPartialResume` has a reader outside `pumpEligibilityRule`, the constant STAYS and only the pump's rung goes** — record which in the commit.

- [ ] **Step 2: Delete the five fences, arms and all.**
  - `pumpPausedBehindGate` collapses to `return pumpPauseRequested(ch)` and loses its `changeID` param; both call sites drop the id argument.
  - `pumpHonorsRecordedPause` collapses to `return proj.OperatorPaused` (the v2 arm), and its three-arm doc comment is replaced by one sentence naming the discharge.
  - `pumpPausedAtRunStart` collapses to `return pumpPauseRequested(ch)`; **the struct arm and its `operatorPauseSignal` local go with it.**
  - `pumpEligibilityRule` collapses to `func pumpEligibilityRule() eligibilityRule { return eligibleWithDesign }` and its `ctx` argument is dropped at the call site.
  - The section header at `:1253-1258` is rewritten:
    ```go
    // ---------------------------------------------------------------------------
    // THE VERSION FENCES ARE GONE (stage 4b3 Task 3). There were FIVE GetVersion call
    // sites over SIX change ids — the header here used to say "five over four", which was
    // wrong — and every one existed to keep a pre-change execution's recorded command
    // sequence replayable. There are no such executions: the founder has ruled there are no
    // production users, and the one drain this release rides kills every {p}:nextActivity
    // execution that could hold a recorded marker.
    //
    // WHAT DID NOT LICENSE THIS, and the distinction matters because the obvious argument is
    // the wrong one: a replay fixture does NOT pin a GetVersion rung. Deleting the
    // changeDesignActivitiesDispatchable rung left all six pump fixtures GREEN, because the
    // SDK tolerates a recorded Version marker the replayed code never asks for; deleting the
    // pace Sleep, by contrast, fails 2 of 6 at a named event position. Fixtures pin COMMANDS.
    // The four census rows that hung off these arms, and the five …_DefaultVersion_… tests
    // that were those rows' ONLY pin, are deleted in this same commit for the same reason —
    // an arm that cannot exist needs no guard, and a guard for it is a test nobody can ever
    // make fail honestly.
    // ---------------------------------------------------------------------------
    ```

- [ ] **Step 3: Delete the five `DefaultVersion` tests and the four census rows, in the same commit.**
  Remove `Test_Pump_PreDispatchGate_DefaultVersion_KeepsOldDispatch`, `…DrainGate…`, `…DecodeGate…`, `…RecordedPauseGate…`, `…LedgerPartialResume…` and the `G-P18`…`G-P21` entries of `pumpGuardCensusPump()`.
  - [ ] **Leave `Test_Pause_RelayGate_DefaultVersion_CancelThenRecord_NoRelay` (`manager_test.go:12289`) ALONE** — it is the Manager-side relay gate, not a pump fence. Verify by reading it before deciding.
  - [ ] Renumber **nothing**. A census id is a name, not an index; deleting G-P18…G-P21 leaves a gap and the gap is the record.

- [ ] **Step 4: Update the census DOC in the same commit, and let its own meta-tests prove it.**
  Delete the four rows from `docs/bugs/2026-09-28-pump-guard-census.md`, update the headline counts (**36 → 32**, and the pin total **re-measured, not subtracted**), and add a one-paragraph note under the table saying the four fences were discharged in 4b3 Task 3 with the two counter-measurements.
  - [ ] Run: `GOWORK=off go test ./internal/manager/delivery/ -run Test_PumpGuardCensus -count=1 -v`
  - [ ] Expected: **all three meta-tests green** — `…_TheDocAndTheCodeAgree` (the ID set and `PinnedBy`), `…_EveryGuardIsPinned` (no row names a test that does not exist), `…_TheHeadlineCountsAreTrue` (the doc's totals). **If the third is green without you touching the doc's numbers, the doc was lying before and that is a finding, not a convenience.**

- [ ] **Step 5: Mutation-check the census meta-test, because this task is the first to lean on it.**
  Change the doc's row count to `33`, run `…_TheHeadlineCountsAreTrue`, watch it go **RED**, restore. Paste both outputs into the commit message.

- [ ] **Step 6: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Replay_PumpHistories|Test_Replay_DeliveryHistories' -count=1 -v
  GOWORK=off go test ./internal/manager/delivery/ -run Test_LifecycleShapes -count=1
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint fix-check sumtype-check vet
  ```
  Expected: **replay 6/6 + 8/8 UNMOVED** (the measured reason is in the header above — and if a pump fixture DOES move, the counter-evidence is wrong and this task stops until that is understood), shapes **11/11**, golden **133**, frozen **15**, **census 36 → 32**, validate 43/0, npm 1237, preview 57/23. Hand-written `delivery` lines drop ~40–70; not the point, and Task 12 measures.
  ```bash
  git add server/internal/manager/delivery docs/bugs/2026-09-28-pump-guard-census.md
  git commit -F - <<'MSG'
  chore(pump): the five version fences are discharged, with their census rows and their DefaultVersion tests

  Five GetVersion call sites over six change ids (the file said "five over four";
  that is corrected here) existed to keep pre-change executions replayable.
  There are none: no production users, and the one drain this release rides
  kills every {p}:nextActivity execution.

  What did NOT license it: a replay fixture does not pin a GetVersion rung.
  Deleting the changeDesignActivitiesDispatchable rung left all six pump
  fixtures green, because the SDK tolerates a recorded Version marker the
  replayed code never asks for — while deleting the pace Sleep fails 2 of 6 at a
  named event position. Fixtures pin COMMANDS. "Replay is 14/14" is not an
  argument about a fence and nobody may use it as one.

  So the four census rows that hung off the DefaultVersion arms, and the five
  …_DefaultVersion_… tests that were those rows' only pin, go in this same
  commit. No capture can ever produce a DefaultVersion history, so those tests
  guard an arm that cannot exist.

  One consequence beyond hygiene: pump-pause-decode-any's DefaultVersion arm
  received a pause into a concrete struct ON PURPOSE, and it was the consumer
  wire-form gate's only live production exception. It is gone, so the gate two
  tasks from now lands over a corpus with none.

  Census 36 rows -> 32. Replay 8/8 + 6/6, shapes 11/11, golden 133 — all unmoved.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 4: THE CONSUMER ARCH GATE — name-keyed, three receive forms, and unanalysable means RED

**The riskiest item in the wave (R6), and the only one that fails green-and-blind in the exact class it exists to catch.** 4b2 shipped the PRODUCER gate; the CONSUMER defect shipped inert and was found by a real-server capture. **The rule:** *a signal channel whose NAME any `messageBus.deliverSignal` producer uses must be received into `any` and normalised.*

**Three design decisions, each measured, each non-negotiable:**

1. **NAME-KEYED (R4).** Id-keying is not computable — every bus target is a runtime value (`pumpWorkflowID(in.ProjectID)`, `deliveryActivityWorkflowID(projectID, id)`, `fmt.Sprintf("%s:delinquency", customerID)`) — and it would have CLEARED `projectsupervision.go:48`, the latent defect, because that name's two producers reach different ids.
2. **THE DATAFLOW STARTS AT `GetSignalChannel`, NOT AT `.Receive`.** `deliveryactivity.go:1338`, `:1895`, `:2769` and `:3882` are `workflow.NewChannel` inboxes and futures, not signal channels, and a gate that started at the receive would have to tell them apart by type. **Twelve** `GetSignalChannel` call sites exist (C5 — the recon's thirteenth is prose at `deliveryactivity.go:3729`).
3. **UNANALYSABLE IS A FAILURE, NOT A PASS.** If the gate cannot follow a bus-named channel from its `GetSignalChannel` to every receive target, it **fails** with a message telling the author to route it through a sanctioned decoder. This inverts the one failure mode that matters: a gate that silently skips what it cannot parse is precisely the artifact R6 forbids.

**Files:**
- Create: `server/internal/signalwire_arch_test.go` — the gate. **Module-scoped** (package `internal_test`), because the producer at `billing/shortfallsweep.go` and the consumer at `operations/delinquencyenforcement.go` are in different packages from `delivery` and a package test cannot see across them.
- Create: `server/internal/testdata/wireform/bad_receive.go.txt`, `bad_receiveasync.go.txt`, `bad_addreceive.go.txt`, `good_fenced.go.txt` — the fixture corpus (`.txt` so `go build` never sees them; the gate parses them with `parser.ParseFile` from source text, which is how `paramguard_arch_test.go` handles its corpus — **verify that precedent before choosing the extension**).
- Modify: `docs/bugs/2026-09-28-stage4b2-earmarks.md` — mark the owed consumer gate DISCHARGED with the commit, in Task 12 rather than here.

**Interfaces produced:**
- `TestSignalWireFormConsumers` — the gate.
- `TestSignalWireFormConsumers_IsRedOnEveryKnownInstance` — the meta-test that runs the analyser over `testdata/wireform/` and asserts one finding per bad fixture and **zero** for the fenced one.
- `func busDeliveredSignalNames(t) map[string]string` — signal-name VALUE → the producer site that delivers it (resolving const idents to their string values across packages, because `billing`'s name is a local literal const and `delivery`'s is another).
- `func analyseSignalConsumers(fset, files) []wireFinding` — the shared analyser both tests call; `type wireFinding struct { Site, Channel, Name, Why string }`.

- [ ] **Step 1: Read the two precedents before writing anything.**
  ```bash
  cd .../server
  sed -n '27240,27340p' internal/manager/delivery/manager_test.go   # the producer gate + deliverSignalNameArg
  sed -n '1,60p' internal/paramguard_arch_test.go                    # the module-scoped AST gate this one sits beside
  grep -n 'packages.Load' -A 12 internal/arch_test.go                # the corpus-loading posture (Tests:false, ./internal/...)
  ```
  Decide, and **write the decision into the gate's header**: AST-only (`go/parser` over the file set) or `go/types` via `packages.Load`. **AST-only is sufficient and this plan specifies it**, because every rule below is intra-function and the identifier resolution needed (a const's string value) is a package-level lookup, not a type inference. `packages.Load` is slower and its `Tests:false` posture would hide a test-only violation the gate does not care about anyway.

- [ ] **Step 2: Write the meta-test and the four fixtures FIRST — the gate is written against them.**

  `testdata/wireform/bad_receive.go.txt`:
  ```go
  package fixture
  const signalOperatorPauseRequested = "operatorPauseRequested"
  func BadReceive(ctx workflow.Context) {
      ch := workflow.GetSignalChannel(ctx, signalOperatorPauseRequested)
      var sig operatorPauseSignal
      ch.Receive(ctx, &sig)   // WANT: flagged — concrete struct target on a bus-delivered name
      _ = sig
  }
  ```
  `bad_receiveasync.go.txt` (the form `billing/closecycle.go:192`/`:217` use):
  ```go
  package fixture
  const signalApplyDelinquencyPolicy = "applyDelinquencyPolicy"
  func BadReceiveAsync(ctx workflow.Context) {
      ch := workflow.GetSignalChannel(ctx, signalApplyDelinquencyPolicy)
      var ev applyDelinquencySignal
      if !ch.ReceiveAsync(&ev) { return }   // WANT: flagged
      _ = ev
  }
  ```
  `bad_addreceive.go.txt` (the form `deliveryactivity.go:2009-2024` use):
  ```go
  package fixture
  const signalActivityFinished = "activityFinished"
  func BadAddReceive(ctx workflow.Context) {
      ch := workflow.GetSignalChannel(ctx, signalActivityFinished)
      sel := workflow.NewSelector(ctx)
      sel.AddReceive(ch, func(c workflow.ReceiveChannel, _ bool) {
          var s activityFinishedSignal
          c.Receive(ctx, &s)   // WANT: flagged — the closure param is the same channel
      })
      sel.Select(ctx)
  }
  ```
  `good_fenced.go.txt` — the ONE sanctioned exception SHAPE (R4), which after Task 3 has no production instance and lives only here:
  ```go
  package fixture
  const signalOperatorPauseRequested = "operatorPauseRequested"
  func FencedPreChangeArm(ctx workflow.Context, ch workflow.ReceiveChannel) (string, bool) {
      if workflow.GetVersion(ctx, "some-pre-change-fence", workflow.DefaultVersion, 1) >= 1 {
          var raw any
          ch.Receive(ctx, &raw)
          return normalise(raw)
      }
      // The PRE-CHANGE body: a concrete struct on purpose, kept only so a recorded history
      // replays. WANT: NOT flagged — and only in this shape.
      var sig operatorPauseSignal
      if ch.ReceiveAsync(&sig) { return sig.Reason, true }
      return "", false
  }
  ```
  ```go
  // TestSignalWireFormConsumers_IsRedOnEveryKnownInstance is the mitigation R6 made mandatory
  // and it is not review advice: this gate's whole failure mode is passing while the defect
  // stands, and 4b2 proved that a wire-form test which signals the wrong form certifies
  // nothing. So the analyser is run over a corpus of the three KNOWN instances, in the three
  // receive forms production really uses, and the expected finding set is exact — one per
  // bad fixture, ZERO for the fenced one. A change that makes this test pass by finding
  // fewer is a change that broke the gate.
  func TestSignalWireFormConsumers_IsRedOnEveryKnownInstance(t *testing.T) { … }
  ```
  - [ ] Run it. Expected: **FAIL — `analyseSignalConsumers` undefined.** That is the red this task starts from.

- [ ] **Step 3: Implement `busDeliveredSignalNames`.**
  Walk every non-test, non-`.gen.go` `.go` file under `server/internal/`, find each `MessageBusDeliverSignal` call, read its **third** argument through `messagebus.SignalName(<ident>)` (reuse the producer gate's `deliverSignalNameArg` shape), then resolve `<ident>` to its **string VALUE** from the declaring package's consts. Return `{value → site}`.
  - [ ] **Verify first:** `grep -rn 'MessageBusDeliverSignal' internal/ | grep -v _test | grep -v '\.gen\.go'` — expect **five** sites (`delivery/projectsupervision.go:148`, `delivery/deliveryactivity.go:3437`, `:3521`, `delivery/pumpnextactivity.go:1003`, `billing/shortfallsweep.go:118`).
  - [ ] Assert in the gate that the resolved value set is exactly `{"operatorPauseRequested","activityLeaseRequested","activityFinished","activityLeaseGranted","applyDelinquencyPolicy"}`, **and fail on a name the gate could not resolve to a literal** — a computed name is a channel nobody can check, which is the producer gate's own rule restated on the consumer side.

- [ ] **Step 4: Implement `analyseSignalConsumers` — the three forms, and the refusal.**
  For each function, collect locals assigned from `workflow.GetSignalChannel(ctx, <expr>)` where `<expr>` resolves to a bus-delivered name. For each such channel identifier, classify **every** use:
  - `ch.Receive(ctx, X)` / `ch.ReceiveAsync(X)` → OK iff `X` is `&<v>` where `<v>` is declared `var <v> any` **in the same function**, or `X` is a `*[]byte`.
  - `sel.AddReceive(ch, func(c workflow.ReceiveChannel, _ bool) { … })` → recurse into the closure body treating the **first parameter's name** as the same channel.
  - passed as an argument to a call whose callee name is in `sanctionedDecoders = {"pumpReceiveSignal","pumpReceiveSignalBlocking","pumpPauseRequested","decodeDelinquencySignal"}` → OK.
  - inside an `if workflow.GetVersion(…) …` **else/pre-change** branch → OK (the exception SHAPE).
  - **anything else — assigned to a struct field, returned, ranged, passed to an unlisted func — is a FINDING**, with:
    ```
    <site>: channel %q (signal %q) is delivered by messageBus.deliverSignal at %s, and this
    gate cannot follow it to its receive target. A bus payload is raw []byte tagged
    binary/plain and a concrete-struct target holds it NOT AT ALL — the SDK logs "Corrupted
    signal" and drops the message, silently. Receive into `any` and normalise (see
    pumpReceiveSignal), or route this channel through a sanctioned decoder. This gate refuses
    what it cannot analyse on purpose: the whole class it exists to catch is invisible.
    ```
  - [ ] **The free fourth rule (R5):** in the same walk, flag any `messagebus.ExecutionPayload` composite literal that sets `ContentType`, with its own message naming `contract.gen.go:23` and "read by nothing".

- [ ] **Step 5: Run the meta-test to GREEN, then run the gate over production.**
  - [ ] `GOWORK=off go test ./internal/ -run TestSignalWireFormConsumers_IsRedOnEveryKnownInstance -count=1 -v` → **PASS** (3 findings, fenced one clean).
  - [ ] `GOWORK=off go test ./internal/ -run 'TestSignalWireFormConsumers$' -count=1 -v` → **PASS with ZERO findings.** Tasks 1, 2 and 3 are what make that true. **If it is not zero, do not add an exception — read the finding: it is either a real instance this plan missed, or an analyser bug, and both are findings worth more than a green run.**

- [ ] **Step 6: Assert the corpus size, so a path move cannot shrink the gate in silence.**
  ```go
  	// THE CORPUS IS ASSERTED, NOT ASSUMED. A gate that walks a directory is one `git mv` away
  	// from walking nothing and reporting success. Measured at stage 4b3: 12 GetSignalChannel
  	// call sites across 5 non-test non-generated files, and 5 MessageBusDeliverSignal
  	// producers. A change to either number is a change to this gate's coverage and must be
  	// made deliberately here.
  	if len(channels) != 12 || len(producers) != 5 {
  		t.Fatalf("the wire-form corpus moved: %d signal channels and %d bus producers, want 12 and 5 — "+
  			"update these numbers in the same commit as the code that moved them", len(channels), len(producers))
  	}
  ```
  - [ ] **Verify first:** re-run `grep -rn "GetSignalChannel" internal | grep -v _test | grep -v '\.gen\.go' | wc -l` at the moment of writing; if Tasks 1–3 changed it, use the measured number and say so.

- [ ] **Step 7: Mutation-check the gate against PRODUCTION, three ways.**
  For each of the three, make the change, run `TestSignalWireFormConsumers`, confirm **RED** with the right site named, then revert:
  1. Revert Task 2's fix at `projectsupervision.go:48` (receive into `operatorPauseSignal`).
  2. Revert Task 1's `any` receive at `delinquencyenforcement.go:51`.
  3. Re-add `ContentType: "application/json"` at `shortfallsweep.go:118`.
  - [ ] **Paste all three outputs into the commit message.** R6: *a gate not proved red on three real instances does not ship.*

- [ ] **Step 8: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/ -run 'TestSignalWireForm' -count=1 -v
  GOWORK=off go test ./internal/ -count=1
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint fix-check sumtype-check vet
  ```
  Expected: green; **every other gate number unchanged** (this task adds one test file and one testdata directory and changes no production code). Golden 133, frozen 15, shapes 11/11, replay 8/8 + 6/6, census 32, validate 43/0, npm 1237, preview 57/23.
  ```bash
  git add server/internal/signalwire_arch_test.go server/internal/testdata/wireform
  git commit -F - <<'MSG'
  test(arch): a signal any bus producer delivers may not be received into a struct

  The rule 4b2 owed and could not enforce: messageBus.deliverSignal hands the
  client a bare []byte, the default converter tags it binary/plain, and
  ByteSlicePayloadConverter can assign that to nothing but a *[]byte — so a
  concrete-struct receive target drops the message and the SDK's "Corrupted
  signal" log is the only trace. That silence hid the whole main-write lease for
  a wave and was found by a real-server capture, not by a test.

  NAME-KEYED, because id-keyed is not computable: every bus target is a runtime
  value. Name-keying is also stricter where it matters — operatorPauseRequested's
  two producers reach DIFFERENT ids, so an id-keyed gate would have cleared
  projectsupervision.go:48, the latent instance.

  THREE RECEIVE FORMS: Receive, ReceiveAsync and the AddReceive closure. A gate
  matching only the first would ship as coverage it does not have.

  AND UNANALYSABLE IS A FAILURE. If the gate cannot follow a bus-named channel
  from GetSignalChannel to every receive target, it fails and says so. A gate
  that silently skips what it cannot parse is the artifact this one exists to
  replace.

  PROVED RED ON THREE KNOWN INSTANCES before shipping (transcripts below), plus
  a testdata corpus carrying all three receive forms and the one sanctioned
  exception shape — a pre-change arm behind a GetVersion fence, which after the
  fence discharge has no production instance at all.

  Free fourth rule: a producer that sets ExecutionPayload.ContentType is flagged.
  That field is declared once and read by nothing, and its presence is the
  strongest signal its author believed the transport serialised for them.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 5: The per-task session state — `constructState` stops being single-valued, and the wire does not move

**The cheapest correctness item in the wave, and it unblocks Task 6.** Measured at HEAD (`deliverymanager.go:12971-13031`), `constructState` holds these SINGLE-VALUED fields, every one written by whichever forked coroutine ran last:

| Field | Written by | Honest scope |
|---|---|---|
| `reviewSet *ReviewSet` | every gate entry | **per TASK** |
| `reviewSetError string` | every gate entry | **per TASK** |
| `redraftExhausted bool` (`:12996`) | recomputed on entry to every gate | **per TASK** |
| `awaitingGate string` / `awaitingSince time.Time` / `awaitingUntil *time.Time` (`:13004-13006`) | `enterHumanStage` only, cleared by `leaveHumanStage` only | **per TASK** |
| `stage`, `pipelinePhase`, `variance`, `attempt` | the walk | whole-activity — **these STAY** |

The LEDGER is already correct: §5.2 fact 2 gave every review coroutine its own `*gateLedger` (`deliverymanager.go:8224-8238`) for exactly this reason. **4b2 worked around the view twice rather than fixing it**, and both workarounds are the evidence: `SubmitTaskDecision`'s precheck was split so the VIEW is asked only whole-activity questions and the LEDGER the per-task one (`:6714-6730`), and `mergeGateKey` is the ONE gate still answered from the view — **legitimately**, because the merge hold runs after the join, so the single-valued pair has exactly one occupant (`:6759-6767`).

**This task is Go-only and changes no wire** — `constructState.view()` (`:8396`) is the single projection point and it derives the flat members from the per-task map. **Task 8 is where the members move onto `ActivityTaskView` and leave the wire**, and that move is cheap only because this task already made the state correct.

**Files:**
- Modify: `server/internal/manager/delivery/deliverymanager.go` — `constructState` (`:12971-13031`), `constructState.view()` (`:8396`), `enterHumanStage` / `leaveHumanStage`, every writer of the six fields.
- Modify: `server/internal/manager/delivery/deliveryactivity.go` — the gate coroutines that set `reviewSet` / `reviewSetError` / `redraftExhausted`.
- Modify: `server/internal/manager/delivery/manager_test.go` — one new shape case, plus the existing view assertions.

**Interfaces produced (Task 6 and Task 8 both consume):**
```go
// taskViewState is one task's live view facts. It exists because a fork holds two gates at
// once and the fields below described whichever was ENTERED LAST — so on an ordinary
// service fork (stp's review and designReview open together) the view could only ever
// describe one of them, and two 4b2 call sites read it and got the fork wrong.
type taskViewState struct {
    reviewSet        *ReviewSet
    reviewSetError   string
    redraftExhausted bool
    awaitingSince    time.Time
    awaitingUntil    *time.Time
    round            int        // the gate's round number, from gateLedger.number — Task 6 reads this
}
```
- `constructState.tasks map[string]taskViewState` (initialised at workflow start, never nil).
- `(*constructState) taskView(taskID string) taskViewState` — zero value for an unknown task.
- `(*constructState) setTaskView(taskID string, mut func(*taskViewState))` — the ONE writer.
- `(*constructState) awaitingTasks() []string` — task ids currently in a human stage, **sorted**, because a map range in a workflow is nondeterministic and this feeds a query the replay must reproduce.

- [ ] **Step 1: Write the failing shape case — a fork must describe BOTH gates.**
  ```go
  // "fork-view-names-both-gates" is the twelfth Test_LifecycleShapes case and it is the
  // oracle for the single-valued view. A service fork opens stp's review and designReview at
  // once; enterHumanStage OVERWRITES awaitingGate/awaitingSince on every entry, so the query
  // could only ever describe the gate entered second — and which one that is depends on
  // coroutine scheduling, which is why two independent 4b2 call sites read it and got the
  // fork wrong, one of them on the ROUTINE approval path rather than an override.
  //
  // Asserted through the QUERY, not through the struct: the struct is what changes and the
  // query is what a caller sees, and a test that reads the field it just moved proves the
  // move, not the fix.
  ```
  Drive the `service` lifecycle to the fork, hold both gates, and assert via `querySessionView` that **both** task ids report a human stage with their own `awaitingSince`, and that each carries its own `reviewSet`.
  - [ ] **Verify first:** `grep -n 'fork-join-service-stp-first' -A 30 internal/manager/delivery/manager_test.go` — reuse that case's harness rather than building a second one, and reuse its `Timeline` field (the merged `start:`/`done:` log), because *"both tasks appear in TaskOrder before either appears in CompletedOrder" compares indices in two independent projections of one walk* and passed against a strictly serial walk (§9's 4b1 amendment).
  - [ ] Run ALONE: `GOWORK=off go test ./internal/manager/delivery/ -run Test_LifecycleShapes -count=1 -v`. Expected: **FAIL**, one gate reported and one absent.

- [ ] **Step 2: Add the map and the three accessors; leave every field in place.**
  This step must be green on its own — the map is written and read by nothing yet.

- [ ] **Step 3: Move the writers, one field family at a time, keeping `view()` reading the old fields.**
  - `enterHumanStage(ctx, taskID, gate, until)` writes `setTaskView(taskID, …)` **and** the old `awaitingGate/awaitingSince/awaitingUntil`.
  - `leaveHumanStage(ctx, taskID, …)` clears the entry **and** the old triple.
  - The gate coroutine writes `reviewSet` / `reviewSetError` / `redraftExhausted` into the task's entry **and** the old fields.
  - [ ] **Verify first:** `grep -n 'enterHumanStage\|leaveHumanStage\|redraftExhausted\|state.reviewSet' internal/manager/delivery/*.go | grep -v _test` — **every** writer must be found; a missed one is a task whose view silently stays zero.
  - [ ] Run the full delivery suite. Expected: **green, unchanged** — this step is a pure addition.

- [ ] **Step 4: Flip `view()` onto the map, and derive the flat wire members.**
  ```go
  	// THE FLAT MEMBERS ARE DERIVED NOW, AND THEY ARE STILL A LIE — a bounded one, for one
  	// more task. ConstructionSessionView's awaiting* / reviewSet* / redraftExhausted describe
  	// an ACTIVITY and the facts are per TASK, so on a fork there is no correct single answer.
  	// Until stage 4b3 Task 8 moves them onto ActivityTaskView and deletes them from the wire,
  	// this projection answers with the MERGE gate when one is held (after the join there is
  	// exactly one occupant, which is why mergeGateKey is the one gate 4b2 left reading the
  	// view) and otherwise with the most recently entered task, tie-broken by task id so a
  	// replay reproduces it. Nothing NEW may read these; the per-task map is the source.
  ```
  - [ ] Run the new shape case. Expected: **GREEN**.
  - [ ] Run the whole delivery suite and the replays. Expected: **green; replay 8/8 + 6/6 UNMOVED** — a query handler's projection emits no workflow command.

- [ ] **Step 5: Mutation-check the new case.**
  Make `setTaskView` write to a single shared key (`"_"` instead of `taskID`) — i.e. re-introduce the defect — run the case, watch it go **RED**, restore. Paste both outputs into the commit message. Without this the case could be passing because both gates happen to carry equal values.

- [ ] **Step 6: Fix the doc drift this task sits on top of.**
  `deliverymanager.go:13165` still says `querySessionState` returns a `SessionStateView`; the handlers return `ConstructionSessionView`. One line.

- [ ] **Step 7: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/manager/delivery/ -run Test_LifecycleShapes -count=1 -v
  GOWORK=off go test ./internal/manager/delivery/ -count=1
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint fix-check sumtype-check vet
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check
  ```
  Expected: shapes **11/11 → 12/12**, replay **8/8 + 6/6 unmoved**, golden **133**, census **32**, validate **43/0**, **npm 1237 → 1239** (two node assertions on the derived projection's tie-break, so the rule is pinned on the client side of the contract too — if you add none, say so and leave 1237), preview **57/23**.
  ```bash
  git add server/internal/manager/delivery
  git commit -F - <<'MSG'
  fix(delivery): the session state is keyed by task, because a fork holds two gates

  constructState carried six view facts as single values — reviewSet,
  reviewSetError, redraftExhausted and the awaitingGate/Since/Until triple — and
  every one is written per GATE. On an ordinary service fork, stp's review and
  designReview open together, so the state described whichever coroutine ran
  last, and which one that is depends on scheduling.

  The ledger was already right: every review coroutine has owned its own
  gateLedger since 4b1, for exactly this reason. 4b2 worked around the view
  twice rather than fixing it, and one of those two call sites was the routine
  approval path, not an override. This is the fix those workarounds were
  waiting for.

  NO WIRE CHANGE. constructState.view() is the single projection point, so the
  flat members are derived from the map — still a lie on a fork, bounded to one
  more task, and said so at the site. Task 8's model edit is where they move
  onto ActivityTaskView and leave the wire, and it is cheap only because the
  state underneath is correct first.

  The shape oracle is the twelfth Test_LifecycleShapes case and it is
  mutation-checked: collapsing the map back to one key turns it red.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 6: The late-approve refusal — the decision names the round it judged, and a superseded round is refused

**One item with Task 5 (R10), and the cheap half costs no contract change.** Measured: `taskDecisionSignal` (`deliverymanager.go:8377-8394`) carries **six** fields — `TaskID`, `Decision`, `OptionID`, `Feedback`, `DecidedBy`, `AcknowledgeStale` — and **no round, no revision**. `SubmitTaskDecision` (`:6735-6788`) checks `requireOpenRound` (`:7050`) at `:6768`, settles threads at `:6778`, and then fires a **fire-and-forget** signal (`m.signalActivity` → `client.SignalWorkflow`, `:6929`). The child re-checks at consumption (`decideTaskGate`, `deliveryactivity.go:2885`) — but it re-checks **openness**, not **identity**. So a redraft that opens round *n+1* between the façade's check and the child's consumption gets round *n*'s approve.

**Two windows, two fixes, and this task buys one.**

- **The window between the façade's check and the child's consumption** — closed here, by the round the MANAGER resolved at submit time travelling in the signal and the child refusing a mismatch. `taskDecisionSignal` is Manager-internal, so this is **no contract change, no model edit, no regen**.
- **The window between the RENDER and the CLICK** — NOT closed here. The human read revision *n* in the browser; if *n+1* opened before they clicked, the Manager resolves *n+1* and stamps it, and the refusal never fires. **Closing that means `SubmitTaskDecision` grows a `round` parameter**, which is a contract change: the op's `$defs`, `openapi.yaml`, `schema.ts`, `ops.gen.ts`, the MCP tool schema, the `systemtests` SDK, `useDeliveryMutations.ts` and the SubmitBar that must now pass `TaskRevisionView.round` (which **already exists** on the wire — measured in the committed `$defs`). Roughly 8 surfaces plus one SPA call site. **It rides stage 6's model edit, and Task 12 earmarks it with this costing.**

**Files:**
- Modify: `server/internal/manager/delivery/deliverymanager.go` — `taskDecisionSignal` (`:8377`), `requireOpenRound` (`:7050`), `SubmitTaskDecision` (`:6735`), and `:6911`'s second `requireOpenRound` caller.
- Modify: `server/internal/manager/delivery/deliveryactivity.go` — `decideTaskGate` (`:2885`).
- Modify: `server/internal/manager/delivery/manager_test.go` — one shape case, two façade cases.

**Interfaces produced:**
- `taskDecisionSignal` gains `Round int` (**appended**, and it is an internal struct so there is no ordinal to preserve — but appending keeps the diff readable).
- `requireOpenRound(...) (int, error)` — **returns the round number it validated**, zero on error. Both call sites change.
- `decideTaskGate` refuses when `sig.Round != 0 && sig.Round != gate.number`.

- [ ] **Step 1: Write the failing shape case.**
  ```go
  // "late-approve-to-a-superseded-round-is-refused" is the thirteenth shape case. The defect
  // is pre-existing and a stage check never could have closed it: SubmitTaskDecision is
  // fire-and-forget, so between the facade's requireOpenRound and the child's
  // decideTaskGate a redraft can withdraw the round and open n+1 — and the child's own
  // re-check asks whether a round is OPEN, not whether it is THE SAME ROUND. So the approve
  // a human cast against revision n lands on n+1, which is a different artifact judged by
  // nobody.
  ```
  Drive a review task to its gate, send a `redraft` signal that opens round 2, then deliver a `taskDecisionSignal{Decision: ReviewApprove, Round: 1}`. Assert the round is **not** passed, the gate is still awaiting, and the child logged the refusal naming both numbers.
  - [ ] Run ALONE. Expected: **FAIL** — the approve lands on round 2.

- [ ] **Step 2: `requireOpenRound` returns what it validated.**
  ```go
  // requireOpenRound returns the ROUND NUMBER it found open, because the caller is about to
  // send a fire-and-forget signal and the child must be able to tell "the round I checked"
  // from "a round that is open". Those are different questions and only the second one was
  // being asked on both sides.
  func (m *constructionManager) requireOpenRound(
      ctx context.Context, projectID ProjectID, activityID ActivityID, taskID string, kind *projectstate.ArtifactKind,
  ) (int, error) {
      …
      for _, r := range rounds {
          if r.Outcome == projectstate.RoundPending {
              return r.Round, nil
          }
          …
      }
  ```
  - [ ] **Verify first:** read the loop at `:7068-7078` as it stands — it returns `nil` on the FIRST pending round found, and `roundsAtTask` may return several when two artifact kinds share a gate task. **Return that same round**, not a recomputed maximum, or the façade stamps a round the check did not validate.
  - [ ] Update both callers (`:6768`, `:6911`). Expected: compiles, suite green, behaviour unchanged.

- [ ] **Step 3: Stamp the round on the signal.**
  ```go
  	// THE ROUND THE MANAGER VALIDATED TRAVELS WITH THE DECISION. Without it the child can
  	// only ask "is a round open", which is true again the moment a redraft opens n+1 — so an
  	// approve cast against n was applied to n+1. taskDecisionSignal is Manager-INTERNAL, so
  	// carrying it costs no contract delta.
  	//
  	// WHAT THIS DOES NOT CLOSE, stated because the fix reads bigger than it is: the round is
  	// the one the MANAGER resolved at submit time, not the one the HUMAN read in the browser.
  	// A redraft between the render and the click still slips through, and closing that means
  	// SubmitTaskDecision taking a round parameter — a contract change across eight surfaces.
  	// TaskRevisionView.round is already on the wire, so the client half is a one-line read.
  	sig := taskDecisionSignal{TaskID: taskID, Decision: decision, OptionID: option,
  		Feedback: feedback, DecidedBy: decidedByOperator, Round: round}
  ```
  For the `mergeGateKey` arm, which opens no round, `round` stays **0** and means "no round identity" — the child's refusal is written `sig.Round != 0 && …` for exactly that reason, and the comment says so.

- [ ] **Step 4: Refuse the mismatch at the child.**
  At the top of `decideTaskGate`, before the approve/reject switch:
  ```go
  	// THE ROUND IDENTITY CHECK (stage 4b3 Task 6). A zero Round means the sender had no round
  	// identity to carry — the merge hold, which opens no round — and is accepted. A non-zero
  	// one that does not match the gate's own round is a decision about an artifact this gate
  	// is no longer judging: a redraft withdrew the round the reviewer read and opened the
  	// next one. The gate keeps awaiting, and the reviewer is told the artifact moved and
  	// which revision is current, because "your approve was ignored" without that sentence is
  	// indistinguishable from a lost signal.
  	if sig.Round != 0 && sig.Round != gate.number {
  		workflow.GetLogger(ctx).Warn("delivery.gate.decisionNamesASupersededRound",
  			"activityId", in.ActivityID, "taskId", t.ID,
  			"decidedRound", sig.Round, "currentRound", gate.number,
  			"consequence", "the decision is not applied; the gate keeps awaiting the current round")
  		return walkTaskFailed, false, nil
  	}
  ```
  - [ ] **Verify first:** `grep -n 'type gateLedger struct' -A 15 internal/manager/delivery/deliverymanager.go` — confirm `number` is the round number and not an index (it is: `gateLedger{task, number, roundID, subject, actor, judgedAttemptID}`, `:8224-8238`).
  - [ ] **Verify first:** confirm `walkTaskFailed, false, nil` is the "keep awaiting, do not decide" return — it is what the open-comment refusal two arms below uses (`:2902-2906`). **If it is not, use whatever that arm uses**, and do not invent a third disposition.
  - [ ] Run the shape case. Expected: **GREEN**.

- [ ] **Step 5: Pin the non-regression — an ordinary approve still passes.**
  A second façade case asserting a normal approve (Round = the open round) passes the gate, and an M0 approve (`mergeGateKey`, Round 0) is unaffected.
  - [ ] **Mutation-check:** change the child's condition to `sig.Round != gate.number` (dropping the zero arm), run, watch the **merge-hold** case go red, restore. Paste both outputs.

- [ ] **Step 6: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/manager/delivery/ -run Test_LifecycleShapes -count=1 -v
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Replay_' -count=1
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint fix-check sumtype-check vet
  ```
  Expected: shapes **12/12 → 13/13**, **replay 8/8 + 6/6 UNMOVED** (a new field on a signal payload and a new early return on a path no fixture takes emit no command — **verify, and if a child fixture moves, the refusal is firing on a recorded happy path and the condition is wrong**), golden 133, census 32, validate 43/0, npm 1237+, preview 57/23.
  ```bash
  git add server/internal/manager/delivery
  git commit -F - <<'MSG'
  fix(delivery): a decision names the round it judged, and a superseded round is refused

  SubmitTaskDecision checks that a round is open and then fires a
  fire-and-forget signal. Between those two moments a redraft can withdraw the
  round and open n+1 — and the child's own re-check asks whether a round is
  OPEN, not whether it is the SAME round. So an approve cast against revision n
  was applied to n+1: a different artifact, judged by nobody.

  requireOpenRound now returns the round it validated, the signal carries it,
  and the gate refuses a mismatch with a log line naming both numbers. A zero
  round means the sender had no round identity — the merge hold, which opens
  none — and is accepted; that arm is mutation-checked.

  taskDecisionSignal is Manager-internal, so this costs no contract delta.

  WHAT IT DOES NOT CLOSE: the round is the one the MANAGER resolved at submit
  time, not the one the HUMAN read. A redraft between the render and the click
  still slips through. Closing it means SubmitTaskDecision taking a round
  parameter — eight surfaces — and TaskRevisionView.round is already on the
  wire, so the client half is one line. Earmarked with that costing.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 7: The `Started`-forever zombie — one extra liveness probe per tick, on the epoch the child is required to ignore

**Measured, and the gap is NARROWER than the earmark says.** Detection runs entirely through `markVanished` (`pumpnextactivity.go:465`), which has **exactly two callers**, both on the lease path: `pumpDeliverGrant`'s `isSignalTargetNotFound` arm (`:945`) and `pumpCheckLease`'s (`:983`). A child of THIS run is covered regardless — its future resolves (`pumpReconcile`, `:1055-1066`). **So the gap is the intersection of three conditions:** the child predates a `ContinueAsNew` (no future) **AND** never reached its merge tail (so never asked for a lease) **AND** wrote no row. Such an id sits in `st.inFlight()` (`:384-392`) on every tick forever; the pump never quiesces and never dispatches past it.

**The design constraint that decides the whole task, measured while writing this plan.** The obvious probe — re-deliver the holder's grant, the way `pumpCheckLease` does — is **unsafe for a non-holder**: the grant buffers on the child's `activityLeaseGranted` channel, and when that child later reaches `requestMainWriteLease` it consumes the buffered grant and runs its merge tail **believing it holds a lease it was never granted**. The mechanism meant to prevent two concurrent main writers would create one.

**The seam already exists.** `pumpLivenessProbeEpoch` (`:312`, `= 0`) is a child-side FLOOR read at exactly one site — `deliveryactivity.go:3474`:

```go
	case grant.Epoch <= pumpLivenessProbeEpoch || grant.ActivityID != in.ActivityID:
		// A LIVENESS PROBE (pumpLivenessProbeEpoch) or a grant addressed to somebody else.
		// Both are inert here BY CONSTRUCTION: the pump bumps LeaseEpoch before it delivers
		// any real grant, so no genuine grant can ever carry the probe's epoch …
		continue
```

So a grant at epoch **0** is a message the child is **required** to ignore, by a rule that is already written, already commented, already tested and already the documented meaning of the constant. **The pump has never sent one; this task is what makes the constant's name true.** Its doc comment currently says *"NO SIGNAL THE PUMP SENDS EVER CARRIES IT"* (`:299`) and that correction is part of the task.

**Consequences that make this the cheapest correct option:** no new signal name (so `Test_DeliverSignal_TheWireFormProducersAreAClosedList` stays at **four** producers), no new RA op (so `activityExecutionAccess` stays at 12/12 and `DH-CONTRACT-OPCOUNT-MAX` stays ABSENT), no contract change, no child change, and the cost is **at most one extra `deliverSignal` Activity per 30 s tick** — not N per tick, which is the earmark's own objection and is right.

**Files:**
- Modify: `server/internal/manager/delivery/pumpnextactivity.go` — `pumpState` (run-local cursor), `pumpCheckLease`'s caller at `:914`, a new `pumpProbeStaleInFlight`, and the `pumpLivenessProbeEpoch` doc comment (`:296-312`).
- Modify: `server/internal/manager/delivery/manager_test.go` — one shape case, one census row.
- Modify: `docs/bugs/2026-09-28-pump-guard-census.md` — the new row.

**Interfaces produced:**
- `func (wf *csWorkflows) pumpProbeStaleInFlight(ctx workflow.Context, in pumpInput, st *pumpState) error` — called immediately after `pumpCheckLease` on the reconcile tick.
- `pumpState.probeCursor int` — **run-local, NOT carried through `pumpInput`** (like `futures`, `vanished` and `blocked`). It is a rotation index; correctness does not depend on it surviving a `ContinueAsNew`, and putting it in the payload would be a payload change for nothing.

- [ ] **Step 1: Write the failing shape case.**
  ```go
  // "futureless-zombie-is-reaped" is the fourteenth shape case and the pump's third. A child
  // started before a ContinueAsNew, never reached its merge tail (so never asked for the
  // lease), and died writing no row. Its id has no future in this run, no terminal row to
  // read and no lease to probe — so markVanished's two callers, both on the lease path,
  // never fire for it. It sits in inFlight() on every tick forever: the pump never
  // quiesces, never dispatches past it, and the project stops with nothing red.
  //
  // The assertion is that the pump REACHES A VERDICT within a bounded number of ticks, not
  // that it succeeds: a vanished child that recorded no terminal STOPS the cascade
  // (pumpReconcile's vanished arm), and that is the correct answer — the wrong one is
  // waiting forever.
  ```
  Seed a `pumpState` with an id in `Started`, no future, no row; make the probe's `deliverSignal` double answer NotFound; run two ticks; assert the run returns `ActivityChildVanished`.
  - [ ] **Verify first:** `sed -n '1080,1100p' internal/manager/delivery/pumpnextactivity.go` — the vanished arm already exists (fix round 2, C1) and this task only supplies the `st.vanished[id]` it reads. **Do not write a second verdict path.**
  - [ ] Run ALONE. Expected: **FAIL** — the run parks and the case times out.

- [ ] **Step 2: Write the probe.**
  ```go
  // pumpProbeStaleInFlight is the ONE extra liveness probe per tick, and it exists for the
  // one activity shape nothing else can see: started before this run's ContinueAsNew (so no
  // future), never reached its merge tail (so it never asked for the lease and markVanished's
  // two callers never fire for it), and dead without writing a row. Such an id sits in
  // inFlight() forever and the pump never ends.
  //
  // WHY IT SENDS A GRANT AT pumpLivenessProbeEpoch AND NOT THE HOLDER'S OWN GRANT. Re-delivering
  // a real grant to a child that did not ask for one is not a probe, it is a HAZARD: the message
  // buffers on activityLeaseGranted, and when that child later reaches requestMainWriteLease it
  // consumes the buffered grant and runs its merge tail believing it holds a lease nobody gave
  // it — two concurrent main writers, produced by the mechanism that exists to prevent them.
  // Epoch 0 is the one value the child is REQUIRED to ignore (deliveryactivity.go's
  // `grant.Epoch <= pumpLivenessProbeEpoch` arm), because pumpDeliverGrant bumps LeaseEpoch
  // before every real grant, so no genuine grant can carry it. The probe's ONLY observable is
  // the delivery's answer: NotFound means the execution is gone.
  //
  // ONE PER TICK, ROTATING. Probing every futureless id every tick is N deliverSignal
  // Activities per tick on an N-activity plan, which is the objection that killed the
  // earmark's version. The cursor is run-local, like futures and vanished: it is a rotation
  // index and nothing depends on it surviving a ContinueAsNew. A 30-activity frontier is
  // therefore fully probed within 30 ticks — fifteen minutes, on a rail whose gates wait
  // hours for a human.
  //
  // IT NEVER JUDGES. NotFound marks the id vanished and releases any admission it held;
  // whether the activity finished or broke is pumpReconcile's answer, off a FRESH row. That
  // is markVanished's own rule and this caller does not get to bend it.
  func (wf *csWorkflows) pumpProbeStaleInFlight(ctx workflow.Context, in pumpInput, st *pumpState) error {
      var candidates []ActivityID
      for _, id := range st.inFlight() {
          if _, hasFuture := st.futures[id]; hasFuture {
              continue // a child of THIS run: its future is the authority
          }
          if st.LeaseHolder != nil && *st.LeaseHolder == id {
              continue // pumpCheckLease already probes the holder, at its own epoch
          }
          if st.vanished[id] {
              continue // already answered; pumpReconcile owes the verdict
          }
          candidates = append(candidates, id)
      }
      if len(candidates) == 0 {
          return nil
      }
      id := candidates[st.probeCursor%len(candidates)]
      st.probeCursor++
      err := wf.pumpSignalChild(ctx, in.ProjectID, id, activityLeaseGrant{ActivityID: id, Epoch: pumpLivenessProbeEpoch})
      switch {
      case isSignalTargetNotFound(err):
          workflow.GetLogger(ctx).Error("pump: an activity started before this run's continue-as-new has no execution; the reconcile will say whether it finished or broke",
              "projectId", string(in.ProjectID), "activityId", string(id))
          st.markVanished(id)
          return nil
      case err != nil:
          return err
      }
      return nil
  }
  ```
  - [ ] **Verify first:** `grep -n 'ROW IS READ, not skipped' -B 6 -A 4 internal/manager/delivery/pumpnextactivity.go` — `pumpReconcile` reads the row BEFORE the `st.vanished[id]` arm, so an id marked vanished on tick *k* is judged on tick *k* if a row exists and on *k+1* otherwise. **Confirm the call order puts the probe before `pumpReconcile` in the tick** (or accept one extra tick and say so in the comment).

- [ ] **Step 3: Call it, once, on the reconcile tick.**
  Beside `pumpCheckLease` at `:914`:
  ```go
  	if err := wf.pumpCheckLease(ctx, in, st); err != nil {
  		return err
  	}
  	if err := wf.pumpProbeStaleInFlight(ctx, in, st); err != nil {
  		return err
  	}
  ```
  - [ ] Run the new shape case. Expected: **GREEN**.

- [ ] **Step 4: Correct `pumpLivenessProbeEpoch`'s doc comment — it now says the opposite of the truth.**
  Replace the "NO SIGNAL THE PUMP SENDS EVER CARRIES IT" paragraph (`:299-306`) with the one signal that does, naming `pumpProbeStaleInFlight` and keeping the reason the HOLDER's probe still uses the real epoch (it is a **renewal** — a live holder that reads it sees the grant it already has, which is why the deadline does not revoke).

- [ ] **Step 5: Add the census row.**
  A new `pumpGuardCensusPump()` entry — the guard is *"a futureless in-flight id with no row is probed, at an epoch the child ignores, at most once per tick"* — with `PinnedBy` naming the new shape case, plus its row in `docs/bugs/2026-09-28-pump-guard-census.md` and the headline count **32 → 33**.
  - [ ] Run `-run Test_PumpGuardCensus -count=1 -v`. Expected: all three metas green.

- [ ] **Step 6: Mutation-check twice — the epoch and the cap.**
  1. Change `Epoch: pumpLivenessProbeEpoch` to `st.LeaseEpoch + 1`: **a test must go red.** If none does, the hazard in the header is unguarded — **add the case that catches it** (a child that never requested a lease must not return `true` from `requestMainWriteLease` after a probe) before continuing. This is the task's most important step.
  2. Remove the `%len(candidates)` cap and probe all candidates: assert the per-tick `deliverSignal` count in the shape case, watch it go red, restore.
  - [ ] Paste all four outputs into the commit message.

- [ ] **Step 7: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_LifecycleShapes|Test_PumpGuardCensus|Test_DeliverSignal_' -count=1 -v
  GOWORK=off go test ./internal/manager/delivery/ -run Test_Replay_PumpHistories -count=1 -v
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint fix-check sumtype-check vet
  ```
  Expected: shapes **13/13 → 14/14**, census **32 → 33**, producer gate **4 names, unmoved**, golden 133, validate 43/0.
  **Replay 6/6 is the number to watch, and here is the prediction with its reason:** the probe emits a `deliverSignal` Activity, which IS a recorded command, so a pump fixture whose history contains a futureless in-flight id would break. **None does** — all six were captured within a single run, so every started child has a future — but this is the one gate in this task that could move, and if it does, **do not adjust the fixture**: the probe fired where the plan says it cannot, and that is the finding.
  ```bash
  git add server/internal/manager/delivery docs/bugs/2026-09-28-pump-guard-census.md
  git commit -F - <<'MSG'
  fix(pump): a child that died writing no row is probed, not waited on forever

  markVanished has two callers and both are on the lease path, so the pump can
  only notice a child that asked for the lease. A child that predates a
  ContinueAsNew (no future), never reached its merge tail (never asked) and died
  writing no row sits in inFlight() on every tick forever: the pump never
  quiesces and never dispatches past it, and nothing goes red.

  One extra probe per tick, rotating, for exactly that set. Not N per tick —
  that objection killed the earmark's version and it was right — and not a new
  execution-status RA op, which would be a contract change on a facet already
  at 12/12.

  IT SENDS A GRANT AT EPOCH ZERO, and that is the whole design. Re-delivering a
  real grant to a child that did not ask for one is a hazard, not a probe: the
  message buffers on activityLeaseGranted and the child consumes it at its tail,
  running the merge believing it holds a lease nobody gave it — two concurrent
  main writers, created by the mechanism meant to prevent them.
  pumpLivenessProbeEpoch is the one value the child is REQUIRED to ignore, by a
  rule already written and already tested at deliveryactivity.go. The constant's
  own doc said no signal ever carries it; now one does, and it says so.

  The probe never judges. NotFound marks the id vanished and releases its
  admission; whether it finished or broke is the reconcile's answer off a fresh
  row, which is markVanished's rule and this caller does not bend it.

  No new signal name, so the producer closed list stays at four.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 8: THE ONE MODEL EDIT — six `$defs` deltas, the per-task wire move, and the preview fixtures stop describing a world four waves old

**ONE indivisible commit, ONE self-amendment loop, and the ONLY task in this wave that touches `.aiarch/state/project.json` (R13).** Two edits is two self-amendment loops, two `systemtests` regens, two rounds of preview-fixture churn, and the §8 rule that no two implementers may hold `project.json` even in disjoint regions.

**What rides it, and why each one is here rather than in its own task:**

| Delta | Slot / contract | Why it rides |
|---|---|---|
| `PumpResult += activityIds` | `deliveryManager.$defs.PumpResult` | 4b2's owed delta; the exact JSON is in the earmark. A plural pump reporting one of N is a lie. |
| `ReplanSweepResult` **DELETED** | `deliveryManager.$defs` | 4 declaration sites, **zero references** (R15). A compile probe is owed here, not in the recon. |
| `ActiveRole`, `ActiveStep` **DELETED** | `deliveryManager.$defs` | The cascade 4b2's provider sweep did not take. Measured while writing this plan: the only occurrences outside generated files are **two prose mentions** (`deliverymanager.go:12867`, `webApp/src/components/activity/activityCopy.ts:279`), both saying the enums are not used. |
| `ActivityTaskView += reviewSet, reviewSetError, awaitingSince, awaitingUntil, redraftExhausted` | `deliveryManager.$defs` | R8. The type already exists and already carries per-task facts. |
| `ActivityView −= reviewSet, reviewSetError`; `ConstructionSessionView −= reviewSet, reviewSetError, awaitingGate, awaitingSince, awaitingUntil, redraftExhausted` | `deliveryManager.$defs` | R8: **deleted, not deprecated.** `awaitingGate` goes outright — the key IS the task. |
| `ActivityExecution += tailFailureDetail` | `projectStateAccess.$defs` (shared by `activityExecutionAccess`) | **The THIRD HEAD FACT (R11, P-B).** Task 9 writes it; it cannot exist without this edit. |
| `ActivityConstructionPhase += completedNotLanded` (ordinal **4**, appended) | `projectStateAccess.$defs` | The DERIVED red node (R11). Task 9 derives it; the vocabulary must exist first. |
| **The preview-fixture regen + its gate** | `uitests/preview-fixtures/**`, `webApp/scripts/fixture-schema.test.mjs` | **R14 — a MEMBER of this commit, not a follow-up.** |

**What does NOT ride it, and the sentence that keeps it out:** no op is added and no op is removed, so **`deliveryManager` stays at 11**, `activityExecutionAccess` and `constructionTransitionAccess` stay at 12/12, `DH-CONTRACT-OPCOUNT-MAX` stays pinned ABSENT, the registered-names golden stays at **133**, and `pruneStaleSDK` should delete nothing. **Those four predictions are the check on whether this edit did what it intended.**

**Files:**
- Modify: `.aiarch/state/project.json` (**the one edit**).
- Regenerated (never hand-edited): `server/internal/manager/delivery/{contract.gen.go,fake/fake.gen.go}`, `server/internal/resourceaccess/projectstate/{contract.gen.go,toolcatalog.gen.go}`, `server/api/openapi.yaml`, `webApp/src/contracts/{schema.ts,enums.gen.ts}`, `webApp/src/api/ops.gen.ts`, `systemtests/internal/sdk/*_delivery.gen.go`.
- Modify by hand (compile-forced or behaviour): `server/internal/manager/delivery/deliverymanager.go` (`constructState.view()`, the `ActivityTaskView` projection, `serviceContractsToContract` untouched), `server/internal/resourceaccess/projectstate/projectstateaccess.go` (`CoarsePhaseFor`'s new arm is **Task 9**, not here — here only the enum's `String()` and any `switch` `gochecksumtype` forces), `webApp/src/contracts/{types.ts,wire.ts}`, `webApp/src/contracts/constructionSession.test.ts`, `webApp/src/containers/ActivityExperienceContainer.tsx:790-791`, `webApp/src/components/activity/{ReviewBody.tsx,ReviewersStrip.tsx}`.
- Create: `server/internal/manager/delivery/` — a `PREVIEW_FIXTURE_CONTRACTS_WRITE`-guarded updater test (precedent: `CONSTRUCT_HISTORY_CAPTURE_CASE`).
- Modify: `webApp/scripts/fixture-schema.test.mjs` — the new gate.
- Modify: `uitests/preview-fixtures/web-client/**` — 17 files.

**Interfaces produced:**
- `PumpResult.ActivityIDs []ActivityID` (Go) / `activityIds` (wire); `PumpResult.ActivityID` stays populated with the first element and is **deprecated in place**.
- `ActivityTaskView.ReviewSet`, `.ReviewSetError`, `.AwaitingSince`, `.AwaitingUntil`, `.RedraftExhausted`.
- `projectstate.ActivityExecution.TailFailureDetail string` — consumed by Task 9 and Task 10.
- `projectstate.ActivityConstructionCompletedNotLanded ActivityConstructionPhase = 4` — consumed by Task 9.

- [ ] **Step 1: The compile probes, BEFORE the edit, both modules, and name the deadness.**
  ```bash
  cd .../server
  # ReplanSweepResult — delete the four declarations by hand, then:
  GOWORK=off go build ./... && GOWORK=off go vet ./...
  cd ../systemtests && GOWORK=off go build ./... && GOWORK=off go vet ./...
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run typecheck
  # then `git checkout` the four files and repeat for ActiveRole / ActiveStep.
  ```
  - [ ] Record in the commit message, for EACH of the three types, **which deadness was measured** using the three-word vocabulary: *uncalled* / *unreachable* / *unreferenced-in-Go-but-live-via-MCP*. All three are expected to be **unreferenced**, and the probe is what makes that a measurement rather than a grep. **A bare grep is not acceptable here** — 4b2's Task 13 was blocked precisely because a grep could not see the generated Temporal invoker's field-name prefix.
  - [ ] **Also check the MCP catalog by name, not by Go**: `grep -rn 'ReplanSweep\|ActiveRole\|ActiveStep' server/internal/resourceaccess/projectstate/toolcatalog.gen.go server/cmd/aiarch-state-mcp/` — an agent-reachable tool has zero Go call sites by construction.

- [ ] **Step 2: Land the fixture GATE first, and watch it go RED over the 17 stale fixtures.**
  In `webApp/scripts/fixture-schema.test.mjs`:
  ```js
  // THE FIXTURE CONTRACT SET IS GATED AGAINST THE MODEL (stage 4b3 Task 8). Measured at the
  // wave head: 17 of the 23 preview fixtures carried a 30-entry ServiceContracts map holding
  // constructionManager, projectDesignManager and systemDesignManager — the three Manager
  // contracts stage 4a DELETED — and NOT deliveryManager, the Manager this whole stage
  // exists to build. All 57 preview cases were green over it. The contract-code panel and
  // the contract React Flow diagrams, which this repo treats as first-class review aids, were
  // browser-tested against a world four waves old, and nothing said so.
  void test('every preview fixture describes the contracts the model actually holds', () => {
    const live = Object.keys(JSON.parse(readFileSync(PROJECT_JSON, 'utf8')).serviceContracts).sort();
    for (const file of fixtureFiles()) {
      const sc = serviceContractsOf(file);        // undefined when the fixture carries no summary
      if (sc === undefined) continue;
      assert.deepEqual(Object.keys(sc).sort(), live, `${file}: ServiceContracts vs .aiarch/state/project.json`);
      for (const [name, c] of Object.entries(sc)) {
        assert.equal(c.Ops.length, liveOpCount(name), `${file}: ${name} op count`);
      }
    }
  });
  ```
  - [ ] **Verify first:** `plan/owed-gate.json` carries a **1**-entry map and four fixtures carry none — read them before choosing between "skip when absent" and "require the full set". A gate that silently skips 18 of 23 files is the failure mode this whole task is about. **Decide explicitly and write the reason into the test.**
  - [ ] Run: `cd webApp && ASDF_NODEJS_VERSION=lts node --test scripts/fixture-schema.test.mjs`. Expected: **RED on 17 files**, each naming the `+3 / −1` delta.

- [ ] **Step 3: Edit `project.json` — all six `$defs` deltas, by hand, once.**
  The `PumpResult` addition, verbatim from the earmark:
  ```json
  "activityIds": { "type": "array", "items": { "$ref": "#/$defs/ActivityID" }, "x-go-name": "ActivityIDs" }
  ```
  `ActivityConstructionPhase` gains ordinal **4** and the varname `ActivityConstructionCompletedNotLanded`, **appended** — ordinals 0–3 are wire values in every committed project and this repo renumbers nothing (`TestRetiredKinds_KeepTheirOrdinals` is the precedent). `ActivityExecution` gains:
  ```json
  "tailFailureDetail": { "type": "string",
    "description": "Set when an activity's MERGE TAIL failed AFTER its binary exit was recorded — it completed its work and failed to land it. It is a THIRD head fact and not a re-meaning of failureDetail, whose pair with failureReason every reader and every write site assumes: a Completed row with a failureReason is a contradiction, and a Completed row with a tail failure is an honest and different thing. Write-once, cleared only by a requeue note (which clears the other four head facts with it). The operator's red node is DERIVED from it (completedNotLanded), never stored." }
  ```
  - [ ] **Verify first:** read the `ActivityExecution` `$def` and confirm `tailFailureDetail` sorts where the generator expects and that no `required` array needs it (it must be optional — every existing row lacks it).
  - [ ] **Do not touch the contract notes' op lists, the `operations` `$defs`, or anything the table above does not name.** C1's façade enum is **not** in this edit unless the controller has said so; if it is, it is one `$defs` swap in `operationsManager` and it is listed in Step 4's diff review.

- [ ] **Step 4: Run the self-amendment loop exactly once, and read the diff before believing it.**
  Run the loop from Global Constraints verbatim. Then:
  ```bash
  git status --porcelain | sort
  git diff --stat
  ```
  - [ ] **Expected and asserted:** `systemtests/internal/sdk/*_delivery.gen.go` change (type shapes) but **none are DELETED** — `pruneStaleSDK` removing a file means an op moved, which this edit does not do. `server/api/openapi.yaml` loses `DeliveryReplanSweepResult`. `webApp/src/contracts/schema.ts` loses it too. **The registered-names golden is UNTOUCHED.** If any of those four is false, stop and find out why before the commit.
  - [ ] `GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System` → **43 advisory / 0 errors**, and `--slot System` plus every edited slot (**slot 5** for the contracts). System DOWNGRADES other slots' Errors.

- [ ] **Step 5: Fix the compile breaks, and the one the sum-type checker will force.**
  - `constructState.view()` stops emitting the six deleted members (Task 5 already made the per-task map the source).
  - The `ActivityTaskView` projection fills the five moved members from `constructState.tasks` / the round ledger — **per task, from the LEDGER, because the client has none (R9)**: `awaitingSince`/`awaitingUntil` from the task's pending round, `reviewSet`/`reviewSetError` from `taskViewState`, `redraftExhausted` from round count vs the redraft budget.
  - `GOWORK=off make sumtype-check` will name every `switch` over `ActivityConstructionPhase` that must gain a `case ActivityConstructionCompletedNotLanded:`. **Measured: 4 non-test non-generated files carry `ActivityConstructionDone`** (`projectstateaccess.go`, `delivery/deliverymanager.go`, plus the two generated ones). **No `default:` arm** — that is how a vocabulary change goes silent.
  - [ ] For each new arm, decide deliberately: in `CoarseBuildStatusFor` the new phase reads as **`BuildFailed`** (it is not integrated and it is not in review), and say so at the site. **`CoarsePhaseFor` itself is NOT changed here** — deriving the new member is Task 9's, and this task only makes the member exist.

- [ ] **Step 6: Regenerate the preview fixtures, and clean the addressee residue while the files are open.**
  - [ ] Add the guarded updater in `manager_test.go`:
    ```go
    // Test_PreviewFixtureContracts_Write regenerates the ServiceContracts block of every
    // preview fixture from the committed model, using the SAME projection the server uses
    // (serviceContractsToContract) rather than a hand-written shape — because a fixture whose
    // shape is hand-maintained is exactly what produced a 30-contract pre-4a world that 57
    // green browser cases could not see. Guarded by PREVIEW_FIXTURE_CONTRACTS_WRITE=1, the
    // posture CONSTRUCT_HISTORY_CAPTURE_CASE already established for capture tools.
    ```
  - [ ] Run it, then re-run the Step 2 gate: expected **GREEN on all 23**.
  - [ ] **RIDER (droppable):** in the same pass, replace the three `"seniorDeveloper"` addressee values in `construction-round-withdrawn.json` (`:31408`, `:31428`, `:31519`) with `"pm"`, and extend the Step 2 gate with a `ReviewCommentAddressee` enum assertion over every fixture comment. Measured: the value is outside the generated enum and the SPA adapter silently DROPS it, so those three change requests are addressed to nobody — and the human-answered label 4b2 shipped is only true if the addressee is a real person. **Zero marginal cost while the files are open; delete this sub-step if the controller wants the wave narrower.**
  - [ ] Run the preview suite the MANAGED way: `cd uitests && npx playwright test tests/preview/`. Expected **57/57 over 23 states, UNMOVED.** **If a case moves, a spec was asserting the stale contract world — read it, do not retune it.**

- [ ] **Step 7: The SPA half.**
  - `webApp/src/contracts/types.ts:1189-1201` and `wire.ts:860-893` lose the six members from the session view and gain the five on the task view.
  - `webApp/src/contracts/constructionSession.test.ts` loses its assertions on the deleted members (`:20-30`, `:40-48`, `:59-62`) and gains the per-task equivalents. **This is where the npm count moves; predict it and reconcile.**
  - `ActivityExperienceContainer.tsx:790-791` re-points `reviewSet`/`reviewSetError` from `view.reviewSet` to the **selected task's** `ActivityTaskView` — which is the whole point, since `ReviewBody`/`ReviewersStrip` render one task's reviewers and were being handed the activity's last-written set.
  - [ ] `cd webApp && ASDF_NODEJS_VERSION=lts npm run check`. Predicted **1237 → 1238** (−4 deleted session assertions, +3 per-task, +2 fixture-contract gate). **Reconcile any difference; do not accept it.**

- [ ] **Step 8: Gates and the ONE commit.**
  ```bash
  cd .../server
  GOWORK=off make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check \
      gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-lifecycles-check
  GOWORK=off make lint fix-check sumtype-check vet encapsulation-check derived-plan-check
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off go test ./internal/ -run 'TestRegisteredTemporalNamesGolden|TestMethodLayering|TestFileLayout' -count=1
  GOWORK=off go test ./internal/engine/designhealth/ -count=1
  cd ../systemtests && GOWORK=off go build ./... && GOWORK=off go vet ./...
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && npx playwright test tests/preview/
  ```
  Expected: **golden 133 UNMOVED**, frozen 15, shapes 14/14, replay 8/8 + 6/6 (an additive field on an Activity result decodes from a recorded payload; **if a replay moves, a field was not additive**), census 33, `DH-CONTRACT-OPCOUNT-MAX` ABSENT, validate **43/0**, npm ≈1238, preview **57/23**.
  ```bash
  git add .aiarch/state/project.json server webApp uitests systemtests
  git commit -F - <<'MSG'
  model(4b3): the one edit — the frontier is plural, the view is per task, a tail failure has a name, and the fixtures stop describing a deleted world

  ONE model edit per wave, so every contract delta rides it.

  PumpResult gains activityIds; activityId stays populated with the first and is
  deprecated in place. A plural pump reporting one of N is a lie the frontier
  has been working around through a Manager-internal payload since 4b2.

  ReplanSweepResult, ActiveRole and ActiveStep are DELETED. All three are
  UNREFERENCED — not merely uncalled — and each carries its own compile-probe
  transcript in both Go modules plus the webApp typecheck, because a bare grep
  cannot see the generated Temporal invoker's field-name prefix and that is
  exactly what blocked 4b2's Task 13.

  The six lossy view members move DOWN onto ActivityTaskView and leave the wire.
  They are deleted, not deprecated: two independent 4b2 call sites read the pair
  and got a fork wrong, one of them on the routine approval path, and with no
  production users there is no compatibility argument left. awaitingGate goes
  outright — a per-task view needs no field naming which task it is.

  ActivityExecution gains tailFailureDetail and ActivityConstructionPhase gains
  completedNotLanded (ordinal 4, APPENDED). Neither is used yet: the next task
  writes the fact and derives the node, and they are here because one wave gets
  one model edit.

  AND THE FIXTURES. 17 of 23 preview fixtures carried 30 service contracts
  including the three Managers 4a deleted and NOT deliveryManager, and 57 browser
  cases were green over it because nothing gated a fixture's contracts against
  the model. The gate lands first and goes red on all 17; then they are
  regenerated through the server's own projection, not by hand.

  No op added, none removed: deliveryManager stays at 11, the two 12/12 facets
  stay at 12, DH-CONTRACT-OPCOUNT-MAX stays absent, the registered-names golden
  stays at 133 and pruneStaleSDK deleted nothing. Those four were the check.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 9: A broken merge tail gets a name, and the red node is DERIVED — `Completed` is never overwritten

**ONE review pass with Task 10 (C3); neither merges without the other.** The earmark is written AT the site (`deliveryactivity.go:3366-3378`) and it is accurate: an error from `finalizeActivity` or `commitDesignArtifacts` returns straight through `walkTasks` **without passing `failWalk`**, so no terminal failure row is written. Head state reads `Running`, or — if `finalizeActivity` landed and only `commitDesignArtifacts` broke — **`Completed` with uncommitted slots.** An operator sees no red node. And `pumpReconcile`'s failure arm cannot cover it: it keys on `row.FailureReason != FailureReasonUnknown` (`pumpnextactivity.go:1074`), which a broken tail never sets.

**The founder question, answered here with its contract consequence (R11).** *May a `Completed` row carry a tail-failure detail?* **YES — as a THIRD head fact with its own name, not as a re-meaning of `failureDetail`.** Three measurements force that answer:

1. **`failureDetail` is the paired half of `failureReason` at every one of its three write sites** (`projectstateaccess.go:2175-2176`, `:10680-10681`, and `reopenTerminalRow` at `:10789` which clears the pair together). A row with a detail and no reason is a shape no reader was written for, and `CoarsePhaseFor` (`:7977`) would still answer `Done`.
2. **Writing the pair would overwrite a fact the walk genuinely established.** `failWalk` records `VarianceExhausted`, and the walk *did* complete its work. That destroys the record git-as-DB exists to keep, and it is the same rule 4b2 already ruled on the lease: `markFinished` was split from `releaseLease` precisely because **a fact may only be asserted by whoever established it.**
3. **The write-once promise is a CONTRACT NOTE, not an enforced invariant — and this task arms it.** Measured: the committed note says *"StartedAt/CompletedAt/FailureReason/FailureDetail are write-once"*, but only `CompletedAt` is guarded (`stampExit`, `:2151-2156`, which no-ops when already set). `RecordActivityOutcome` (`:10672`) assigns `cs.FailureReason` and `cs.FailureDetail` **unconditionally**. So "a second terminal cannot overwrite the first" is true of one field out of four. **The rule below is where it becomes true of the rest.**

**The shape: no new op, no ceiling pressure.** `activityExecutionAccess` is at exactly 12/12 (`engine_test.go:60-67` pins `RuleContractOpMax` ABSENT), so a thirteenth verb is not available — the same wall 4b1's requeue fold hit. The fact is therefore written by **the verb that already exists**, under a rule inside the store:

> **`RecordActivityOutcome` on a row that has ALREADY recorded a non-failure terminal writes `TailFailureDetail` and leaves `Outcome`, `CompletedAt`, `FailureReason` and `FailureDetail` exactly as the walk left them.**

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — `RecordActivityOutcome` (`:10672-10700`), `CoarsePhaseFor` (`:7977-7991`), and the contract note on the facet.
- Modify: `server/internal/manager/delivery/deliveryactivity.go` — `finalizeWalk`'s tail (`:3366-3382`), replacing the earmark comment with the mechanism.
- Modify: `server/internal/manager/delivery/pumpnextactivity.go` — `pumpReconcile`'s arms (`:1071-1090`).
- Modify: `server/internal/resourceaccess/projectstate/access_test.go`, `server/internal/manager/delivery/manager_test.go`.
- Modify: `webApp/src/contracts/enums.gen.ts` consumers — whatever renders the coarse phase (find them; `ActivityConstructionDone` appears in `webApp/src/contracts/operating.ts`).

**Interfaces produced (Task 10 consumes both):**
- `projectstate.ActivityExecution.TailFailureDetail` is written **only** by the new arm of `RecordActivityOutcome`.
- `CoarsePhaseFor` returns `ActivityConstructionCompletedNotLanded` when `CompletedAt != nil && FailureReason == FailureReasonUnknown && TailFailureDetail != ""`, checked **before** the `Done` arm.

- [ ] **Step 1: Write the failing test — and construct a GENUINELY broken tail (R11).**
  ```go
  // Test_Walk_ABrokenMergeTailLeavesARedNode is the case whose subject is an INVISIBLE
  // failure, so it is built the expensive way ON PURPOSE. It drives the real walk to its
  // real tail with a commitDesignArtifacts double that ERRORS after finalizeActivity has
  // already recorded the binary exit — the exact sequence the earmark at
  // deliveryactivity.go:3366 describes — and then asserts through the same derivation the
  // SPA reads.
  //
  // IT IS NOT ASSERTED OVER A HAND-BUILT ROW. 4b2's roundRevisions fix passed a round with no
  // attempts supplied and thereby CERTIFIED a shape that could never match; the commit
  // message, the comment at the site and the test's own doc were all false for the one gate
  // they named. A hand-built row here would prove the derivation and not the write.
  func Test_Walk_ABrokenMergeTailLeavesARedNode(t *testing.T) { … }
  ```
  Assert three things: `Outcome` is still `Completed` and `CompletedAt` is unchanged; `TailFailureDetail` names the failing step; `CoarsePhaseFor(row, nil) == ActivityConstructionCompletedNotLanded`.
  - [ ] **Verify first:** `sed -n '3319,3390p' internal/manager/delivery/deliveryactivity.go` and `grep -n 'func (wf \*csWorkflows) commitDesignArtifacts' -A 20 internal/manager/delivery/deliveryactivity.go` — confirm which double the harness injects and that an error there is reachable AFTER `finalizeActivity` succeeds. **If the only reachable break is before the exit, say so and build the other one** — the whole point is the post-exit window.
  - [ ] Run ALONE. Expected: **FAIL** — `TailFailureDetail` is empty and the phase reads `Done`.

- [ ] **Step 2: Add the store rule, and make the write-once promise true.**
  ```go
  	return a.onActivity(rc, "RecordActivityOutcome", …, func(cs *ActivityExecution) error {
  		// A SECOND TERMINAL DOES NOT OVERWRITE THE FIRST (stage 4b3 Task 9). The row already
  		// holds a non-failure terminal, so the walk COMPLETED ITS WORK; what failed is the
  		// tail that had to land it. Recording VarianceExhausted over that would destroy a fact
  		// the walk genuinely established, which is the rule 4b2 ruled on the lease when it
  		// split markFinished from releaseLease: a fact may only be asserted by whoever
  		// established it.
  		//
  		// So the honest statement — "completed its work and failed to land it" — gets its own
  		// field, and the operator's red node is DERIVED from it (completedNotLanded), never
  		// stored. Derived-not-stored is spec 5.3's own rule.
  		//
  		// THIS IS ALSO WHERE THE CONTRACT NOTE'S WRITE-ONCE PROMISE STOPS BEING A CLAIM. The
  		// note has said StartedAt/CompletedAt/FailureReason/FailureDetail are write-once since
  		// stage 3; only CompletedAt was guarded (stampExit no-ops when set) and this verb
  		// assigned the other two unconditionally.
  		if cs.CompletedAt != nil && cs.FailureReason == FailureReasonUnknown && reason != FailureReasonUnknown {
  			if cs.TailFailureDetail == "" {
  				cs.TailFailureDetail = detail
  			}
  			return nil
  		}
  		stampExit(cs, now)
  		…
  ```
  - [ ] **Verify first:** `grep -n 'func (a \*activityExecutionAccess) RecordActivityOutcome' -A 30 internal/resourceaccess/projectstate/projectstateaccess.go` and re-read the misuse guard at the top (`outcome == Unknown && reason == Unknown` is refused). The new arm sits INSIDE `onActivity`'s mutator, after that guard.
  - [ ] **Verify first:** `grep -n 'func reopenTerminalRow' -A 12 internal/resourceaccess/projectstate/projectstateaccess.go` — `TailFailureDetail` must be cleared there with the other four, or a requeued activity carries a stale tail failure forever. **`RequeuedAfterExit` (`:8798`) must also be read**: it tests `StartedAt`, `CompletedAt` and `FailureReason`, and whether it must also test the new field is a decision this step makes explicitly (it should NOT — the field is cleared by the re-open, so testing it adds nothing and a fifth condition is a fifth thing to keep in step).
  - [ ] Add a store-level test in `access_test.go`: a second `RecordActivityOutcome` over a `Completed` row leaves `CompletedAt` and `FailureReason` untouched and fills `TailFailureDetail`; a first one over a `Running` row behaves exactly as before.

- [ ] **Step 3: Derive the red node.**
  ```go
  func CoarsePhaseFor(r ActivityExecution, phases []PhaseCompletion) ActivityConstructionPhase {
  	if r.FailureReason != FailureReasonUnknown {
  		return ActivityConstructionFailed
  	}
  	// COMPLETED ITS WORK AND FAILED TO LAND IT. Checked BEFORE Done, because the row genuinely
  	// holds a completion and the honest reading is neither Running nor Failed. It is DERIVED
  	// from the third head fact rather than stored, so nothing can drift out of step with it.
  	if r.CompletedAt != nil && r.TailFailureDetail != "" {
  		return ActivityConstructionCompletedNotLanded
  	}
  	if r.CompletedAt != nil {
  		return ActivityConstructionDone
  	}
  	…
  ```
  - [ ] **Verify first:** `grep -rn 'CoarsePhaseFor(' server/internal | grep -v _test` — every caller must be read, because a new member reaching a caller that switches on four is a `gochecksumtype` failure at best and a wrong screen at worst. **`reopenTerminalRow` calls it** (`:10786`) and its `switch` has arms for Done/Failed and for NotStarted/Running: the new member must join the **Done/Failed** arm, or a re-open of a not-landed activity is refused — which is the one heal path Task 10 depends on.

- [ ] **Step 4: Write the fact from the tail.**
  Replace the earmark comment at `deliveryactivity.go:3366-3378` with the mechanism:
  ```go
  	// THE TAIL'S FAILURE IS RECORDED, AND IT DOES NOT CLAIM THE ACTIVITY FAILED (stage 4b3
  	// Task 9; the earmark this replaces was written here in 4b2's fix round). An error from
  	// either call below used to return straight through walkTasks without passing failWalk,
  	// so no row was written at all and head state read Running — or Completed with
  	// uncommitted slots — for an activity whose tail broke. The pump stops the cascade on the
  	// child's future, which is safe; what was missing was the DURABLE record an operator can
  	// see.
  	//
  	// recordTailFailure writes the third head fact through the verb that already exists. On a
  	// row with no terminal yet it is an ORDINARY failure and lands as one; on a row that
  	// already recorded its binary exit the store keeps the exit and records the tail detail
  	// beside it. Neither arm is a decision taken here — the store owns it, which is why the
  	// rule is written there and not in three callers.
  	if err := wf.finalizeActivity(…); err != nil {
  		return wf.recordTailFailure(ctx, in, state, "finalizeActivity", err)
  	}
  	if err := wf.commitDesignArtifacts(ctx, in, lc, ws, state); err != nil {
  		return wf.recordTailFailure(ctx, in, state, "commitDesignArtifacts", err)
  	}
  	return nil
  ```
  `recordTailFailure` calls `recordExecutionOutcome(… ActivityOutcomeUnknown, PipelineFailed, "the merge tail failed at "+step+": "+err.Error())` and **returns the original error unchanged**, the way `failWalk` does (`:3910`) — the caller and the pump must see the real cause, not a summary of it.
  - [ ] **Verify first:** `grep -n 'func (wf \*csWorkflows) recordExecutionOutcome' -A 12 internal/manager/delivery/deliveryactivity.go internal/manager/delivery/deliverymanager.go` — confirm the parameter order and that it threads `state.walk.headVersion` and `state.walk.cred`, as `failWalk` does.
  - [ ] Run the Step-1 test. Expected: **GREEN**.

- [ ] **Step 5: Let the reconcile see it.**
  `pumpReconcile`'s `case row.CompletedAt != nil:` arm (`:1082`) currently calls `st.markFinished(id)` — correct, and it must stay correct for a not-landed row too (the activity IS over; it is not going to land itself). **Add nothing to the control flow; add the log.**
  ```go
  		case row.CompletedAt != nil:
  			// STILL FINISHED, even when the tail did not land — the child is not coming back, and
  			// leaving it in flight would wedge the pump on an activity nothing will ever finish.
  			// The heal is an operator re-open (or the sweep that offers one), not a pump decision.
  			if row.TailFailureDetail != "" {
  				logger.Error("pump: an activity completed its work and FAILED TO LAND IT; it is finished here and needs a re-open",
  					"projectId", string(in.ProjectID), "activityId", string(id), "detail", row.TailFailureDetail)
  			}
  ```
  - [ ] **Decide and state:** does a not-landed activity **unblock its dependents**? Measured: `AllDepsSatisfied` reads the rows `RecordActivityCompleted` upserts, and `CoarseBuildStatusFor` answers `BuildFailed` for the new phase after Task 8 Step 5. **Write the answer into the comment**, because "the node is red but the plan advanced" is the worst possible outcome and it must be a decision, not a side effect.

- [ ] **Step 6: Mutation-check the store rule.**
  Delete the `cs.CompletedAt != nil && …` arm so the second record overwrites, run the Step-1 test and the `access_test.go` case, watch **both** go red (the phase reads `Failed` and `CompletedAt` moves), restore. Paste both outputs.

- [ ] **Step 7: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/resourceaccess/projectstate/ ./internal/manager/delivery/ -count=1
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_Replay_|Test_LifecycleShapes' -count=1 -v
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint fix-check sumtype-check vet
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check
  ```
  Expected: shapes **14/14 → 15/15**, **replay 8/8 + 6/6 UNMOVED** (the eight child fixtures all reach their tails successfully, so the new call site is not on their path — **and if one moves, the error arm fired on a happy path**), golden 133, census 33, validate **43/0**, **npm 1238 → 1240** (the coarse-phase renderer gains the new member).
  ```bash
  git add server webApp
  git commit -F - <<'MSG'
  fix(delivery): an activity that completed its work and failed to land it gets a name, and Completed is never overwritten

  A broken merge tail returned through walkTasks without passing failWalk, so no
  row was written: head state read Running, or Completed with uncommitted slots,
  and an operator saw no red node. The cascade was already safe — the pump stops
  on the child's future — but the durable record was missing.

  A THIRD HEAD FACT, not a re-meaning of failureDetail. failureDetail is the
  paired half of failureReason at all three of its write sites, and a row with a
  detail and no reason is a shape no reader was written for. Routing the tail
  through failWalk would write VarianceExhausted over an outcome the walk
  genuinely established — the rule 4b2 ruled on the lease when it split
  markFinished from releaseLease: a fact may only be asserted by whoever
  established it.

  So: the store keeps the exit and records the tail detail beside it, the
  operator's red node is DERIVED (completedNotLanded, never stored), and the
  heal-by-re-open path is untouched.

  And the write-once promise stops being a claim. The contract note has said the
  four head facts are write-once since stage 3; only CompletedAt was guarded,
  and RecordActivityOutcome assigned the other two unconditionally. The new arm
  is where that becomes true, and it is mutation-checked.

  The test constructs a REAL broken tail — commitDesignArtifacts erroring after
  finalizeActivity recorded the exit — and asserts through the derivation the
  SPA reads. A hand-built row would have proved the derivation and not the
  write, which is exactly how 4b2's roundRevisions fix certified a shape that
  could never match.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 10: The re-open sweep — a Completed activity whose slots never landed is offered its heal

**ONE review pass with Task 9.** A marker with no heal is a red node an operator cannot clear; a heal with no marker is a sweep with nothing to key on.

**It is a SECOND STEP inside `RoundSweepWorkflow`, not a new workflow type — and that is a scope decision with a reason.** A new registered type would be a new name in the golden, a new frozen name, a new Schedule and a new drain item **in the wave that runs the drain and cuts the release** (R2). `RoundSweepWorkflow` (`roundsweep.go`) is already Schedule-triggered at 300 s, already has the fan-out/single-project arm discriminator, already bounds its writes per tick (`roundSweepMaxPerTick = 200`), and already closes *"the review rounds that no run will ever decide"* — which is the same job one noun over.

**What it keys on, and why it is precise rather than eager.** Only rows where **`CompletedAt != nil` AND `FailureReason == Unknown` AND `TailFailureDetail != ""`** — i.e. rows the tail itself marked in Task 9. It does **not** go looking for uncommitted slots on its own: "Completed with a slot still AwaitingReview" is also the legitimate mid-flight shape of a design activity whose gate has not run, and a sweep that re-ran those would re-dispatch work nobody asked for.

**And the idempotency is already built.** `RecordOperatorNote{NoteRequeue}` re-arms a terminal row (`reopenTerminalRow`, `:10785`), its dedup comes FIRST so a replay is the same no-op success (`:10745-10750`), and `RequeuedAfterExit` (`:8798`) requires the newest requeue note to be **newer than the newest resolved attempt** — which is exactly what stops this sweep re-opening the same row every 300 s forever.

**Files:**
- Modify: `server/internal/manager/delivery/roundsweep.go` — the single-project arm.
- Modify: `server/internal/manager/delivery/manager_test.go`.

**Interfaces produced:** `roundSweepResult` gains `Reopened int` (**unexported type, so no contract surface** — the reason `roundSweepResult` states for itself); `func (wf *csWorkflows) sweepReopenNotLanded(ctx, projectID, proj) (int, error)`.

- [ ] **Step 1: Write the failing test.**
  ```go
  // Test_RoundSweep_ReopensAnActivityThatCompletedAndDidNotLand pins the heal half of the
  // broken-tail pair. The row holds a real completion AND a tail-failure detail — the state
  // Task 9 made visible — so the activity is over, its slots are not committed, and nothing
  // was ever going to re-run it: RecordOperatorNote{NoteRequeue} is the documented heal and
  // until now a human had to know to press it, on a node that showed nothing wrong.
  //
  // It also pins the NON-case, which is the half that matters more: a Completed row with NO
  // tail-failure detail is NOT re-opened, because "Completed with a slot awaiting review" is
  // also the honest mid-flight shape of a design activity, and a sweep that re-ran those
  // would re-dispatch work nobody asked for.
  func Test_RoundSweep_ReopensAnActivityThatCompletedAndDidNotLand(t *testing.T) { … }
  ```
  - [ ] Run. Expected: **FAIL** — nothing re-opens.

- [ ] **Step 2: Implement the arm.**
  ```go
  // sweepReopenNotLanded offers the heal for an activity that completed its work and failed
  // to land it (CoarsePhaseFor -> completedNotLanded). It writes a REQUEUE note, which is the
  // one operator-note kind that changes the row: it clears the head facts, keeps both
  // ledgers and the lifecycle pin, and the pump selects the activity again because the note
  // itself is the evidence it reads.
  //
  // WHY A SWEEP MAY PRESS THIS BUTTON. It is not a decision about the work — the walk already
  // decided that and the ledger keeps it — it is a decision about a LANDING that provably did
  // not happen, and the re-run re-seeds every task that passed rather than redoing them.
  // Leaving it to a human means leaving it to a human who was shown nothing until Task 9.
  //
  // IT CANNOT LOOP. RecordOperatorNote dedups on NoteID before it re-arms, and
  // RequeuedAfterExit requires the newest requeue note to be newer than the newest RESOLVED
  // attempt — so an activity re-opened, re-run and finished again is finished, and its stale
  // requeue note does not re-arm it a second time. The NoteID is derived from the activity
  // id and the tail detail so one broken landing mints one note however many ticks see it.
  //
  // BOUNDED per tick, the same reason roundSweepMaxPerTick states for the rounds: one
  // Temporal task may not do unbounded work on a project with years of history.
  ```
  - [ ] **Verify first:** `grep -n 'NoteRequeue\|OperatorNoteInput{' internal/manager/delivery/*.go | grep -v _test` — reuse the existing requeue construction (4b1 built it for the operator button) rather than assembling a second one, and **confirm the NoteID derivation is deterministic**, because a workflow that mints a random id is non-deterministic on replay.
  - [ ] Run both cases. Expected: **GREEN**.

- [ ] **Step 3: Mutation-check the non-case.**
  Drop the `TailFailureDetail != ""` condition so the sweep keys on `CompletedAt` alone, run, watch the NON-case go **RED** (a mid-flight design activity gets re-opened), restore. Paste both outputs. **This is the mutation that matters** — the eager version of this sweep is the one that would re-dispatch real work.

- [ ] **Step 4: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_RoundSweep|Test_Replay_' -count=1 -v
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint fix-check sumtype-check vet
  GOWORK=off go test ./internal/ -run TestRegisteredTemporalNamesGolden -count=1
  ```
  Expected: **golden 133 UNMOVED** (no new workflow type — that is the scope decision, and the golden is how it is checked), frozen 15, shapes 15/15, **replay 8/8 + 6/6 unmoved** (no replay fixture records `deliveryRoundSweep`), census 33, validate 43/0, npm unchanged, preview 57/23. **No new Schedule — 4b3 REGISTERS none.** (Corrected 2026-09-30: that is not the same as the namespace holding two. It holds four fixed ids plus one `closeBillingCycle:*` per customer. Task 13 Step 8 checks by id, not by count.)
  ```bash
  git add server/internal/manager/delivery
  git commit -F - <<'MSG'
  feat(delivery): the round sweep re-opens an activity that completed and did not land

  The heal half of the broken-tail pair, and it ships with the marker that makes
  the state visible. RecordOperatorNote{NoteRequeue} has been the documented
  heal since 4b1 and a human had to know to press it, on a node that showed
  nothing wrong.

  A SECOND STEP IN RoundSweepWorkflow, not a new workflow type. A new registered
  type would be a new golden name, a new frozen name, a new Schedule and a new
  drain item, in the wave that runs the drain and cuts the release. The round
  sweep is already Schedule-triggered, already has the fan-out discriminator,
  already bounds its writes per tick, and already closes the rounds no run will
  decide — the same job one noun over.

  IT KEYS ON THE TAIL'S OWN MARK, not on uncommitted slots: "Completed with a
  slot awaiting review" is also the honest mid-flight shape of a design
  activity, and the eager version of this sweep would re-dispatch work nobody
  asked for. That non-case is the mutation that was checked.

  It cannot loop: the requeue note dedups on its id before it re-arms, and
  RequeuedAfterExit requires the note to be newer than the newest resolved
  attempt.

  Golden 133 unmoved, and that is the check on the scope decision.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 11: `deliverymanager.go` — the ONLY split the file-layout standard permits, and the record of why the named one is forbidden

**Read C4 before starting. R23's remediation row names a fix its own standard forbids, and this task's first deliverable is that correction.** Measured: `TestFileLayout` (`internal/arch_test.go:61`, `arch.CheckFileLayout`) enforces the 2026-07-11 standard, and the file's own header restates the allowed set at `deliverymanager.go:37-43`:

- **Rule 1 — one contract-implementation file.** `deliverymanager.go` holds "**all** methods of the contract implementation, plus everything shared across workflows or not specific to one workflow", and the standard says in as many words: *"Large files are an accepted consequence — `projectstateaccess.go` folds ~40 source files; `systemdesignmanager.go` is similar scale."*
- **Rule 2 — one file per Temporal workflow**, named `tolower(funcName − "Workflow") + ".go"`, declaring exactly one `workflow.Context`-taking func **plus helpers used only by that workflow**.
- **Rule 3 — one test file.** **Rule 4 — no other files.**

**So "split by op family" lands RED on a gate this repo has held at zero waivers since 2026-07-12.** The only sanctioned reduction is **Rule 2's re-homing**: a helper used by exactly one workflow belongs in that workflow's file. A first-cut scan while writing this plan found **107** top-level symbols in `deliverymanager.go` referenced by exactly one other non-generated file — all of them `deliveryactivity.go` — but that is an **UPPER BOUND**, because a symbol also used by the Manager façade inside `deliverymanager.go` must stay by Rule 1.

**Last code task, deliberately:** a 13,234-line file moving under a later task is how a wave loses a change.

**Files:**
- Modify: `server/internal/manager/delivery/deliverymanager.go` (moving out), `deliveryactivity.go` / `pumpnextactivity.go` / `roundsweep.go` / `pumpsweep.go` / `projectsupervision.go` (moving in).
- Modify: `docs/bugs/2026-09-28-stage4b2-earmarks.md` — the correction (or Task 12 carries it; decide and say which).

**Interfaces:** **none.** This task moves code between files in ONE package and changes no symbol, no signature and no behaviour. **If a rename is required to move something, do not move it.**

- [ ] **Step 1: Print the standard and the current file set, before touching anything.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator
  sed -n '20,80p' docs/superpowers/specs/2026-07-11-layer-file-layout-standard-design.md
  sed -n '37,43p' server/internal/manager/delivery/deliverymanager.go
  ls server/internal/manager/delivery/*.go
  cd server && GOWORK=off go test ./internal/ -run TestFileLayout -count=1 -v
  ```
  Expected: the allowed set is `deliverymanager.go` + one file per workflow + `manager_test.go` + `*.gen.go`, and `TestFileLayout` is green. **Write the allowed set into the task report. A second impl file is not an option and an implementer who adds one has to revert it.**

- [ ] **Step 2: Measure the movable set properly — "referenced by one other file" is not "used by one workflow".**
  ```bash
  cd server/internal/manager/delivery
  python3 - <<'PY'
  import re, os, collections
  files=[f for f in os.listdir('.') if f.endswith('.go') and not f.endswith('_test.go') and not f.endswith('.gen.go')]
  src={f:open(f).read() for f in files}
  dm=src['deliverymanager.go']
  decls={}
  for m in re.finditer(r'^func (?:\([^)]*\) )?([A-Za-z_]\w*)\(', dm, re.M): decls[m.group(1)]=m.start()
  for m in re.finditer(r'^type ([A-Za-z_]\w*)\b', dm, re.M): decls.setdefault(m.group(1), m.start())
  for m in re.finditer(r'^(?:var|const) ([A-Za-z_]\w*)\b', dm, re.M): decls.setdefault(m.group(1), m.start())
  movable=collections.defaultdict(list)
  for n,_ in decls.items():
      pat=re.compile(r'\b'+re.escape(n)+r'\b')
      others={f for f in files if f!='deliverymanager.go' and pat.search(src[f])}
      # uses INSIDE deliverymanager.go, excluding the declaration lines themselves
      inside=len(pat.findall(dm))
      declcount=len(re.findall(r'^(?:func (?:\([^)]*\) )?|type |var |const )'+re.escape(n)+r'\b', dm, re.M))
      if len(others)==1 and inside<=declcount:
          movable[next(iter(others))].append(n)
  for f in sorted(movable): print(f, len(movable[f]), sorted(movable[f])[:12])
  PY
  ```
  - [ ] **This heuristic is a CANDIDATE LIST, not a verdict** — a bare-word regex hits prose comments (the mistake that put `detailPaneState.ts` on a retirement list in 4b2). **Every candidate is confirmed by reading it** before it moves.
  - [ ] **Record the honest number in the task report.** If it is small, say so: **this task may legitimately land as a docs-only correction**, and that is a truthful outcome rather than a failure.

- [ ] **Step 3: Move, in slices, compiling between each.**
  Move confirmed symbols to the single workflow file that uses them, **byte-identical**, preserving their doc comments verbatim. After each slice:
  ```bash
  cd .../server && GOWORK=off go build ./... && GOWORK=off go test ./internal/ -run TestFileLayout -count=1
  ```
  - [ ] **Do not reformat, rename, re-order or "tidy" anything that moves.** A move whose diff is not pure relocation cannot be reviewed as one, and this file's blame is the only way a reader finds the pre-merge history (the 199-row rename table at `docs/superpowers/plans/2026-09-25-stage4a-rename-table.md`).
  - [ ] Watch for the trap: moving a symbol into `deliveryactivity.go` makes it unavailable to a FUTURE Manager caller by Rule 1's own logic. **If the Manager plausibly grows a caller, leave it.**

- [ ] **Step 4: Write the correction into the record.**
  A paragraph, in the earmark file and quoted by Task 12 into the spec's §10 remediation discussion:
  > **R23's remediation row for the 13,234-line file — "split by op family under the existing file-layout standard" — is FORBIDDEN BY that standard.** Rule 1 admits one contract-implementation file and Rule 4 admits no others; the standard's own text calls large files an accepted consequence and names `projectstateaccess.go` as the precedent. The only sanctioned reduction is Rule 2's re-homing of workflow-exclusive helpers, which 4b3 did and measured. **The real owners of the file-size finding are (a) a platform amendment to the standard — a `framework-go` release, i.e. not this programme — or (b) the facet wave, which removes RA-shaped code from the Manager rather than re-filing it.** Recording this as "done" without saying so would leave the next reader believing a split is available.

- [ ] **Step 5: Gates and commit.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/ -run 'TestFileLayout|TestMethodLayering|TestRegisteredTemporalNamesGolden' -count=1 -v
  GOWORK=off go test -short -count=1 ./...
  GOWORK=off make lint fix-check sumtype-check vet encapsulation-check
  wc -l internal/manager/delivery/*.go
  ```
  Expected: **`TestFileLayout` GREEN** (the acceptance; not a line target), golden 133, every other gate unmoved, and **the package's hand-written total unchanged** — a move between files in one package moves no lines out of the package, which Task 12's measurement must not confuse with a reduction.
  ```bash
  git add server/internal/manager/delivery docs/bugs/2026-09-28-stage4b2-earmarks.md
  git commit -F - <<'MSG'
  refactor(delivery): the workflow-exclusive helpers go to their workflow files, and the named split is recorded as forbidden

  The 13,234-line file was to be "split by op family under the existing
  file-layout standard". The standard is what forbids it: Rule 1 admits ONE
  contract-implementation file holding everything shared across workflows,
  Rule 4 admits no others, and the standard's own text calls large files an
  accepted consequence, naming projectstateaccess.go as the precedent.
  TestFileLayout has held that at zero waivers since 2026-07-12.

  So this does the only split the standard permits — Rule 2's re-homing of
  helpers used by exactly one workflow — and records the correction. The real
  owners of the file-size finding are a platform amendment to the standard, or
  the facet wave, which removes RA-shaped code rather than re-filing it.

  Pure relocation: no rename, no reformat, no re-order, no behaviour. The blame
  is how a reader reaches the pre-4a history through the rename table.

  And the measurement not to misread: moving code between files in one package
  moves no lines out of the package. The hand-written total is unchanged.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 12: The record — the spec's three false claims, the earmarks, the ceiling sentence, and the measurement

**Docs only. No Go, no TS, no `project.json`.** Every number in it is re-run here, not transcribed from a task report — which is the discipline that caught "37 census rows" in two documents at 4b2.

**Files:**
- Modify: `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` — §5.3, §8's 4b3 row + merge order, §9's owed bullet, §10.
- Create: `docs/bugs/2026-09-29-stage4b3-earmarks.md`.
- Modify: `docs/bugs/2026-09-28-stage4b2-earmarks.md` — discharge what 4b3 closed; correct the two measured errors.
- Modify: `docs/bugs/2026-09-24-stage3-rail-earmarks.md` — what 4b3 adds to the drain (**nothing but the confirmation**, and that is the finding).
- Modify: `docs/bugs/2026-09-28-pump-guard-census.md` — the citation sentence.

- [ ] **Step 1: Correct §8's 4b3 row, which repeats two false claims.**
  - **"no member of it is part of the drain" is FALSE and moot** (R10-architect). With the release moved into 4b3 (R2), **4b3 IS the wave the one drain and the one release ride.** Rewrite the row to what shipped, and amend the **merge order** sentence: *"all three must be on main before the single drain and the one release"* becomes **four** waves — 3 + 4a + 4b1 + 4b2 + 4b3 — one drain, one release, at the end of 4b3. Stage 6 then lands as an ordinary post-deploy wave.
  - **The GAP-7 argument is FALSE** (R5-architect): delete the sentence calling the pump's per-wake-up read "the strongest argument for pulling the batched plan read forward", in **both** §5.1's 4b2 amendment (`:120`) and the 4b3 row. The measurement to put in its place: *the pump reads through `designSessionAccess.ReadProjectOnBranch` inside a workflow and never touches `QueryProjectView`, so an eighth `ProjectViewKind` buys it zero bytes; the narrowed read it wants is a `projectStateAccess` op and belongs to the facet wave.*

- [ ] **Step 2: Correct §5.3 — `designSessionAccess` is on the scheduling spine, and that is the sentence that explains the wave.**
  The amendment says the facet survives because *"it is still how the child commits design slots."* Measured (`pumpnextactivity.go:1424`), that **understates it**: it is also **how the PUMP READS THE PROJECT ON EVERY WAKE-UP** — 1,161,922 B every 30 s, pinned by `manager_test.go:27696-27698`'s `reads < 2`. Add that sentence, and add its consequence: **deleting the facet stalls the pump**, which is why the collapse is a component promotion in its own post-deploy wave and not a deletion, and why re-pointing the read in 4b3 was refused (7 call sites across 3 workflow types would invalidate all fourteen replay fixtures).

- [ ] **Step 3: §9's owed bullet, re-pointed honestly.**
  *"revision read returns artifact-as-of-`stagedRef`"* — **still owed after 4b3, and now with a measured reason rather than a slip.** Record the four findings from the Scope table: the handle is not ref-shaped (`stagedRefString` drops `StagedRef.Branch`; `designSubjectRef` was deleted in `98e4a906`; the live state holds ONE round whose `SubjectRef` is `{artifact, "operationalConcepts"}`, backfilled — **no `@v` ref exists on this repo**), the server-side resolution is preferred over a format change, the client half already ships and says so honestly, and **it rides stage 6's model edit.**

- [ ] **Step 4: §10 — the ceiling-not-ratchet sentence (R23), written where the guard is nominated.**
  > **§9's smaller-than-sum acceptance is a CEILING, not a ratchet.** It reads green while the number grows, and it grew: 18,983 at 4b1 → 20,714 at 4b2 (+1,141 in a wave that budgeted a reduction) → *(Task 12's measured figure)* at 4b3. **The floor the next wave measures against is 20,714**, and a wave that meets the ceiling while raising the floor should say so in its own words rather than quote the green tick. And the honest reading beside it (R22): by every measure except op count `deliveryManager` is the system — **81.0%** of all hand-written Manager code, **6.7×** operations, **15 of 35 components** as direct collaborators, one **13,234**-line file — **and it must NOT be split**, because it encapsulates exactly one volatility whose variation is data, and both ways to shrink it are the anti-patterns this programme deleted. **The real god component is `projectStateAccess`: 5 contracts, 47 ops, 10,864 lines, one component** — with its wave named (R4).

- [ ] **Step 5: Write `docs/bugs/2026-09-29-stage4b3-earmarks.md`.**
  Structure it the way 4b2's is — gates re-measured at ship, the changed-path set by tree, the line measurement, an ordered open list — and carry **at least** these, each with its measurement and its owner:
  1. **The façade's `pauseNotWithdraw` still cannot say "nobody said"** (C1). One `$defs` enum swap, six surfaces, named.
  2. **The late-approve window between the RENDER and the CLICK is open** (Task 6). `SubmitTaskDecision` + a `round` parameter, ~8 surfaces; `TaskRevisionView.round` is already on the wire so the client half is one line. **Stage 6's model edit.**
  3. **The artifact-as-of-revision read** → stage 6, with §2.2's four measurements.
  4. **The file-size finding's real owners** (Task 11 Step 4), quoted.
  5. **`operationalConcepts` is the ONE orphan kind left** and the founder has declined a drafting step (R17). **Record the cost as a deliberate choice:** `ReviewRound.artifactKind` stays defensive rather than load-bearing, and the one kinded round this repo holds (`architecture:architectureReview:operationalConcepts:2`) judges a kind no lifecycle names. The honest category is **nine** (it plus the eight surviving Phase-2 draft slugs), and all of it is a method-assets release.
  6. **The `ContinueAsNew` boundary is still unfixtured and cannot be captured**, carried verbatim from 4b2 with both counter-measurements.
  7. **The lease's scope is per pump CHAIN, not per project**, carried; **and the lease runs for the first time after THIS release** — the one watch-item that matters.
  8. **The double `ReadActivityExecution` on the approve path**, and **the live double-write** (`GitStatusRecordActivityStarted` + `ActivityExecutionOpenActivity` on one row in one walk with nobody having said which wins) → stage 6 / the facet wave.
  9. **Two recon corrections**: signal channels are **12, not 13** (C5 — the thirteenth was prose at `deliveryactivity.go:3729`), and the pump's fence header said *"five over four change ids"* where it is **five call sites over six ids** (fixed in Task 3).
  10. **`deployment-linear.json` is still browser-unexercised**; **`gen-enums.mjs`'s `NON_MECHANICAL.ProjectSessionStage` is still dead configuration**; **`cmd/server/hooks_test.go:321-323` still names the replan sweep**; **`homebase.spec.ts:47`'s `/^phase-card-/` regex is a raw literal asserting ABSENCE and is silently unfalsifiable**; **`shapeSDPActivity = "P-SDP"` is a shape-rig artefact that mis-classifies.**

- [ ] **Step 6: Discharge what 4b3 closed, in 4b2's earmark file, by name and commit.**
  The consumer gate (Task 4); the delinquency `Context` founder decision (Task 1 — **and record the controller's correction of the record: it was reported LIVE twice and it is LATENT**, because `billingStateAccess` is the arm-less stub in every profile); the no-failure-row gap (Task 9); the `Started`-forever zombie (Task 7); `PumpResult.activityIds` and `ReplanSweepResult` (Task 8); the `ActiveRole`/`ActiveStep` cascade (Task 8); the per-task session view (Tasks 5 + 8); the late-approve defect's **child-side half** (Task 6, with the other half re-earmarked); the re-open sweep (Task 10); the five fences (Task 3). **Leave the ones 4b3 dropped by ruling as DROPPED with the ruling's reason** — the batched plan read, the pushed job-completion signal, the census citation gate, the reachability pass as framed — never as silently absent.

- [ ] **Step 7: The census citation sentence (R7-architect).**
  One paragraph in `docs/bugs/2026-09-28-pump-guard-census.md`: *the head-line citations are a human-navigation aid with no net, deliberately. Both candidate gates fail — an `AtHead` literal makes the field the thing that drifts, and a content hash turns any pump edit into a red doc test — and the killer is measured: an in-range check PASSED on the live failure (`:1337` was in range in a 1,375-line file). A gate that passes on the live failure is worse than none.*

- [ ] **Step 8: Re-run and record every gate, and the line measurement.**
  ```bash
  cd .../server
  GOWORK=off go test ./internal/ -run 'TestRegisteredTemporalNamesGolden' -count=1 -v
  GOWORK=off go test ./internal/manager/delivery/ -run 'Test_LifecycleShapes|Test_Replay_|Test_PumpGuardCensus|Test_DeliverSignal_' -count=1 -v
  GOWORK=off go test ./internal/ -run TestSignalWireFormConsumers -count=1 -v
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off make lint fix-check && for t in models fakes client internal-tools temporal sdk config main lifecycles; do GOWORK=off make gen-$t-check; done
  wc -l internal/manager/delivery/*.go
  cd ../systemtests && GOWORK=off go build ./... && GOWORK=off go vet ./...
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && npx playwright test tests/preview/
  git diff --name-only main...HEAD | cut -d/ -f1 | sort | uniq -c
  ```
  - [ ] Record the changed-path set **by tree, and name `systemtests/` explicitly** — a merge-safety read that lists only `server/`, `webApp/`, `uitests/` and `docs/` drops a tree that holds hand-written changes, and it is a separate Go module the server's own gates never compile.
  - [ ] Line recipe, verbatim: total `wc -l internal/manager/delivery/*.go` − `manager_test.go` − the four `*.gen.go` (`activities`, `contract`, `invokers`, `worker`). Compare against **20,714** (the 4b2 floor) and **25,643** (the `4baed01a` predecessor sum). **State the percentage and state that the acceptance is a ceiling.**

- [ ] **Step 9: Commit.**
  ```bash
  git add docs
  git commit -F - <<'MSG'
  docs(4b3): the spec stops repeating two false claims, the release moves in, and the acceptance is called a ceiling

  Three spec corrections, each measured. Section 8's 4b3 row said no member of
  it is part of the drain: with the release moved into 4b3, 4b3 IS the wave the
  one drain and the one release ride, and the merge order now says four waves,
  one drain, one release. The same row repeated the GAP-7 argument: the pump
  reads through designSessionAccess.ReadProjectOnBranch inside a workflow and
  never touches QueryProjectView, so an eighth view kind buys it zero bytes.
  And 5.3 understated designSessionAccess — it is not only how the child commits
  design slots, it is how the PUMP READS THE PROJECT ON EVERY WAKE-UP, which is
  the one sentence that explains why the facet collapse is a wave and not a
  deletion.

  Section 10 gains the sentence the guard was missing: the smaller-than-sum
  acceptance is a CEILING, not a ratchet. It reads green while the number grows,
  and it grew 1,141 lines in the wave that budgeted a reduction. The floor the
  next wave measures against is 20,714.

  The 4b3 earmark file carries what this wave chose not to close, each with its
  price: the facade's boolean, the render-to-click half of the late approve, the
  artifact-as-of-revision read, the file-size finding's real owners, and the one
  orphan artifact kind the founder has deliberately left without a drafting step.

  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01971KM9cmBjP5owngJ89ezd
  MSG
  ```

---

### Task 13: DRAIN → MERGE → TAG → DEPLOY

**The wave ends in production. This is the task the whole programme has been deferring since stage 3, and it covers stages 3 + 4a + 4b1 + 4b2 + 4b3 in ONE drain and ONE release (R2).**

**Read the procedure, not this task.** `docs/bugs/2026-09-24-stage3-rail-earmarks.md` holds the six steps as ONE current sequence (rewritten by 4b2's Task 17, every number re-measured). This task's job is to run them, to say what 4b3 adds, and to cut the release.

**What 4b3 adds to the drain: NOTHING but a confirmation, and that is the finding.** No new workflow TYPE name (golden **133** unmoved), no new workflow id family, no new Schedule (Task 10 is a second step in an existing sweep, deliberately), no renamed activity (no op added or removed by Task 8), no state migration (`tailFailureDetail` is additive and absent means absent). **The founder's no-users ruling makes the drain optional cleanup — with the single exception of step 5's three `temporal schedule delete` calls on any namespace that ever ran an older image**, because `RegisterSchedule` ADOPTS a same-id Schedule rather than replacing it (`messagebus.go:167-217`), so an unregistered Schedule is not a deleted one and keeps firing into a workflow type no worker serves.

- [ ] **Step 1: Pre-merge — run the whole gate set on the branch, once, in one session.**
  ```bash
  cd .../server
  GOWORK=off golangci-lint cache clean
  GOWORK=off make lint fix-check sumtype-check vet encapsulation-check derived-plan-check
  for t in models fakes client internal-tools temporal sdk config main lifecycles; do GOWORK=off make gen-$t-check; done
  GOWORK=off go test -count=1 ./...            # NOT -short: the full suite, once, before a release
  GOWORK=off go test ./internal/manager/delivery/ -run Test_LifecycleShapes -count=1 -v   # ALONE
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  cd ../systemtests && GOWORK=off go build ./... && GOWORK=off go vet ./...
  cd ../webApp && ASDF_NODEJS_VERSION=lts npm run check
  cd ../uitests && npx playwright test tests/preview/
  ```
  - [ ] Expected: golden **133**, frozen **15**, shapes **15/15**, replay **8/8 + 6/6**, census **33**, the consumer gate green over production and red over its corpus, validate **43/0**, npm ≈**1240**, preview **57/23**, both modules build and vet.
  - [ ] **Two known non-4b3 hazards, so a red run is diagnosed and not chased:** `TestRegisterOperatedSystem_AlreadyRegistered_Conflict` **hangs at 10 minutes under full non-short `./...`** (pre-existing, not in `-short`, untouched here); and `Test_LifecycleShapes` flakes ~7.5% under parallel load and 0/30 serially.

- [ ] **Step 2: Whole-branch review against the census, not against a re-derivation.**
  ```bash
  git diff --name-only main...HEAD | cut -d/ -f1 | sort | uniq -c
  git log --oneline main..HEAD
  ```
  - [ ] Walk the 33 census rows and confirm each is still pinned by a test that exists (`Test_PumpGuardCensus_EveryGuardIsPinned` is the machine half; the human half is reading the four rows Task 3 deleted and confirming nothing else leaned on them).
  - [ ] **The 4b1/4b2 finding class this review exists for:** *a precondition that lived in a deleted body and was not re-asserted in the body that replaced it.* This wave deleted five fence bodies (Task 3) and rewrote one projection (Task 5). **Read those six sites against what they replaced**, not against what they say now.

- [ ] **Step 3: Pause every project, then drain.**
  Follow the procedure's **Step 1** (pause) and **Step 2** (drain every in-flight workflow) verbatim. Sweep the `delivery:` prefix as well as `*:nextActivity:*` — the memorised guidance is incomplete.
  - [ ] **Step 3 of the procedure** (terminate by hand what this image cannot resume): 4b1 retired **seven** workflow types and 4b2 an eighth (`constructionReplanSweep`). **4b3 retires none**, so the list is unchanged — confirm that rather than assume it.

- [ ] **Step 4: Migrate the state — there is nothing to migrate, and confirm it.**
  The procedure's **Step 4** ran in stage 3. 4b3 adds `tailFailureDetail` (absent means absent) and `ActivityConstructionPhase` ordinal 4 (derived, never stored). **No migration. Say so in the run log**, because "no migration needed" is a claim worth one sentence and the rollback note depends on it.

- [ ] **Step 5: Delete THREE Schedules, BY ID, BEFORE the release.**
  ```
  temporal schedule delete --schedule-id construction:pumpSweep
  temporal schedule delete --schedule-id construction:replanSweep
  temporal schedule delete --schedule-id delivery:replanSweep
  ```
  - [ ] This is **the one drain step the no-users ruling does not excuse** (R1). A Schedule left on a queue no worker polls is a *silent dead sweep, not an error*, and `temporal schedule describe` is the only thing that reports it.

- [ ] **Step 6: MERGE to main.**
  ```bash
  git checkout main && git pull
  git merge --no-ff activity-experience-stage4b3
  git push
  ```
  - [ ] **`.github/workflows/release.yml` fires on push to main.** It resolves the version by **bumping the last `archistrator-server-v*` tag's patch** (not by reading `server/VERSION`), builds and pushes the server image, commits the bumped `VERSION` back to main, and does the same for the webApp from `archistrator-webapp-v*`. **Read the workflow before merging** and confirm that behaviour is still current — it is the mechanism this step's "tag" comes from.
  - [ ] Current tags: `archistrator-server-v0.8.110` (`server/VERSION` 0.8.110) and `archistrator-webapp-v0.6.92` (`webApp/package.json` 0.6.92). **Expected after the merge: `archistrator-server-v0.8.111` and `archistrator-webapp-v0.6.93`.**

- [ ] **Step 7: TAG — confirm the release workflow produced them; do not hand-tag over it.**
  ```bash
  gh run list --workflow=release.yml --limit 3
  git fetch --tags && git tag --sort=-creatordate | head -4
  ```
  - [ ] Both tags present and both images pushed. **If the workflow failed, fix it and re-run — a hand-cut tag that the workflow did not produce breaks the next release's version resolution, which reads the last tag.**

- [ ] **Step 8: DEPLOY, then confirm, then unpause.**
  Deploy the new server and webApp images per the procedure's **Step 6**, then:
  ```
  temporal schedule list        # read as PRESENT / ABSENT by id — NOT as a count
  temporal schedule describe --schedule-id delivery:pumpSweep      # 30s, queue `delivery`
  temporal schedule describe --schedule-id delivery:roundSweep     # 300s, queue `delivery`
  ```
  - [ ] **CORRECTED 2026-09-30 — this step used to say "expect exactly TWO" and "three is wrong". Both were false: on a CORRECT deploy you will see FOUR OR MORE.** Three Managers register Schedules at startup, and `closeBillingCycle:{customerId}` is one per customer, so the total is unbounded. Check by id, never by count:
    - **PRESENT**, each with its action on task queue `delivery`: `delivery:pumpSweep` (30 s, `constructionPumpSweep`) and `delivery:roundSweep` (300 s, `deliveryRoundSweep`).
    - **ABSENT** — step 5's three deletions, and this listing is how you find out step 5 was skipped: `delivery:replanSweep` (its workflow type is deleted, so a survivor fires into a type no worker serves) and both `construction:*` ids.
    - **EXPECTED, not this wave's business, DO NOT DELETE:** `operations:operatedStateReconcile`, `shortfallSweep`, and every `closeBillingCycle:*`.
  - [ ] Also expect **THREE task queues with a worker** — `billing`, `delivery`, `operations` (`main.gen.go:593/606/622`; both gates `return true` at `hooks.go:1250/1254`). The procedure's §0 said "exactly one" until the same fix round; one queue would be the broken image, not the healthy one.
  - [ ] Then unpause every project (`SetProjectRunState` with `runState: "running"`).

- [ ] **Step 9: Watch the first cascade DELIBERATELY. This is not optional reading.**
  **The main-write lease has never run in production.** Every lease message was dropped on the wire until `37b0e768`, so the 2-hour grant budget, the liveness probe, the epoch and `pumpGrantLease`'s requester validation were all dead code exercised only by tests using a wire form production never produces. **The first cascade after this release is the first time the admission queue has ever been in the path of a merge.** Measure three things, and record them in the earmark file:
  1. how long a tail actually holds the lease (the 2 h budget has to clear *other* activities' merge tails, each of which can hold a human approval gate);
  2. whether a busy project serialises behind the budget;
  3. whether the grant → release → re-grant handshake behaves as the dev-server capture showed (`granted epoch 1 → released → re-granted epoch 2` in 7.4 s).
  - [ ] **And watch 4b3's own two firsts:** Task 7's liveness probe (a `deliverSignal` at epoch 0 that no child has ever received in production) and Task 10's re-open sweep (which now writes a requeue note on a 300 s schedule). **A probe storm or an unexpected re-open is a rollback trigger, not a curiosity.**

- [ ] **Step 10: Rollback, if it is needed — the order is not the obvious one.**
  **The state move is ONE-WAY** (the pre-stage-3 code has no read tolerance for `.activityExecution`). The order is **pause → restore `project.json` to its pre-migration commit → roll the image → unpause.** Rolling the image alone is not a rollback: an older image reads every activity as `NotStarted` and the pump re-dispatches the WHOLE project the instant it is unpaused — fresh attempts, fresh branches, fresh spend, against activities that are already Done. **Schedules are the one part of the cutover that is not one-way** (a rollback re-registers under the old image's ids), which is why step 5 is cheap in both directions.

- [ ] **Step 11: Record the release.** Append to `docs/bugs/2026-09-29-stage4b3-earmarks.md`: the two tags, the image digests, the drain's actual outcome per step, the Schedule listing, and the three lease measurements. **Then, and only then, is stage 6 unblocked** — and it lands on a deployed system, which is what moving the release into 4b3 bought.

---

## Self-review

Run with fresh eyes against the spec, the controller's file, the recon and the architect's rulings.

### 1. Spec → task coverage

| Source requirement | Task |
|---|---|
| Controller item 1 — the delinquency payload, flattened, three-value action, `unknown` refused | **1**, with **C1** naming the half that cannot be bought without a contract change |
| Controller item 2 — fix `projectsupervision.go:48` FIRST, then the CONSUMER GATE; name-keyed; all three receive forms; proved RED on three; one exception SHAPE; the `ContentType` rule | **2** then **4**, with the fences moved between them (**P-A**) |
| Controller item 3 — discharge the five pump fences with their census arms and `DefaultVersion` tests, as one edit | **3** |
| Controller item 4 — the per-task session view; six members move DOWN and are DELETED; one projection point; no wire change | **5** (the Go half, no wire change) + **8** (the wire move) — split by **P-B**/R13 and stated at both |
| Controller item 5 — the late-approve defect; child-side refusal costs no contract change; say what the other half costs | **6**, and the costing is in **12** Step 5 item 2 |
| Controller item 6a — the third head fact, never overwrite `Completed`, derived `completedNotLanded`; answer "may a Completed row carry a tail-failure detail?" and say what it costs | **9** — answered **YES, as a separately-named third fact**, with the three measurements that force it, including that the write-once promise was a claim and not an invariant |
| Controller item 6b — the zombie; a staleness bound reusing `pumpCheckLease`'s one-probe-per-tick; futureless AND never-asked AND no row | **7**, with the grant-buffer hazard measured and `pumpLivenessProbeEpoch` as the seam |
| Controller item 7 — a re-open sweep for a Completed activity with uncommitted slots | **10** |
| Controller item 8 — THE ONE MODEL EDIT: `PumpResult.activityIds`, `ReplanSweepResult` removed (unreferenced, compile probe owed), the `ActiveRole`/`ActiveStep` cascade, the four op-count literals, the PREVIEW FIXTURE REGEN + a gate | **8**. **One correction to the brief, measured:** the four hand-maintained op-count literals (`fixture-schema.test.mjs:151` = 22, mcp-tools 19, `statusDecides`' floor, `deliveryMutationStatus`'s nine) **do NOT move**, because 4b3 adds and removes no op. They are named in Task 8's "what does not ride it" so their non-movement is a prediction rather than an omission. |
| Controller item 9 — split `deliverymanager.go`, last code task | **11**, and **C4**: the named split is FORBIDDEN by the live `TestFileLayout` gate; Task 11 does Rule 2's re-homing and records the correction |
| Controller item 10 — docs + the release: §8's 4b3 row, §5.3, the ceiling sentence, drain → merge → tag → deploy | **12** then **13** |
| Spec §5.1 — the pump's per-wake-up read, and the false GAP-7 claim | **12** Step 1 (deleted from both sites) |
| Spec §5.2 — the four structural facts the child forced | untouched; Task 5 adds the per-task map beside the per-coroutine `*gateLedger` that fact 2 established |
| Spec §5.3 — the two ledgers, head facts, derived-not-stored | **9** (the third fact, derived node) + **12** Step 2 (the `designSessionAccess` correction) |
| Spec §6 — deterministic Project Design | untouched. `operationalConcepts` gets no drafting step (**R17**) and **12** Step 5 records the cost. |
| Spec §8 — merge order and the 4b3 row | **12** Step 1 |
| Spec §9 — the owed "revision read returns artifact-as-of-`stagedRef`" bullet | **OUT → stage 6** (**P-C**, four measurements), re-pointed by **12** Step 3 |
| Spec §9 — shape cases | **5**, **6**, **7**, **9** (11 → 15) |
| Spec §10 — the ONE drain; the god-manager ceiling | **13**; **12** Step 4 |
| 4b2 earmark — the CONSUMER gate owed as a real arch gate | **4** |
| 4b2 earmark — the fifth `deliverSignal` producer and its founder question | **1**, with the controller's correction of the record (LATENT, not live) |
| 4b2 earmark — the four deferred contract deltas | **8** |
| 4b2 earmark — `deployment-linear.json`, the alias gate hole, the two meta-test holes, the orphan-module gate | **stage 6**, re-earmarked by **12** |
| Recon §6.3 — the preview fixtures' pre-4a contract world | **8** Steps 2 and 6, gate-first |
| Recon §7 — the four remaining founder questions | all four recorded as decided or deferred in **12** Step 5; none blocks finishing |

**Gaps found and closed while writing this table.** Three. (a) Controller item 4's "no wire change" and controller item 8's "every contract delta rides the model edit" are both true only if the item is split in two — so it is, at Tasks 5 and 8, and both tasks say why. (b) Controller item 6 could not be one task because 6(a) needs a field that does not exist and the model edit sits between them — Tasks 9 and 10 are therefore ONE review pass. (c) The brief lists the four op-count literals as carried by the model edit; measured, none of them moves, and saying so is worth more than moving them.

### 2. Placeholder scan

Searched for `TBD`, `TODO`, `implement later`, `fill in details`, `add appropriate error handling`, `add validation`, `handle edge cases`, `write tests for the above`, `similar to Task N`. **None present.** Every code step carries a code block or a named `file:line` to read first; every test step names its assertion and its expected red and green; every gate step states the number it expects and why. Nine steps begin **"Verify first:"** and hand the implementer a command whose expected output is printed — that is a measurement instruction, not a placeholder, and each one exists because the claim under it would otherwise be inherited rather than measured. One sub-step is explicitly marked **RIDER (droppable)** (Task 8 Step 6's addressee clean-up) and states what to delete. Task 11 states in advance that it may land as a docs-only correction, with the condition under which that is the truthful outcome.

### 3. Type and name consistency

Checked across tasks: `delinquencyAction` / `delinquencyActionUnknown|Pause|Withdraw` (Task 1 Interfaces; used in Steps 3–7) and their billing mirrors `delinquencyActionPauseWire|WithdrawWire` (pinned equal by the Step 8 test); `decodeDelinquencySignal` (Task 1, and named in Task 4's `sanctionedDecoders`); `pumpReceiveSignalBlocking` (Task 2 Step 2 = Task 4's sanctioned list); `taskViewState` / `constructState.tasks` / `taskView` / `setTaskView` / `awaitingTasks` (Task 5 Interfaces; consumed by Task 6's `round` and Task 8's `ActivityTaskView` projection); `gateLedger.number` (Task 6 Step 4 — verified present at `deliverymanager.go:8226`, and it is the field Task 5's `taskViewState.round` mirrors); `requireOpenRound(...) (int, error)` (Task 6 Step 2, both call sites at `:6768` and `:6911`); `pumpProbeStaleInFlight` / `pumpState.probeCursor` / `pumpLivenessProbeEpoch` / `markVanished` (Task 7, all four at the lines given); `TailFailureDetail` (Task 8 `$defs` `tailFailureDetail` → Go `TailFailureDetail`, written in Task 9, read in Tasks 9, 10 and by `CoarsePhaseFor`); `ActivityConstructionCompletedNotLanded` = ordinal 4 (Task 8 defines, Task 9 derives, Task 8 Step 5 gives it its `CoarseBuildStatusFor` arm); `recordTailFailure` → `recordExecutionOutcome` (Task 9 Step 4, the same helper `failWalk` uses at `:3903`); `sweepReopenNotLanded` / `roundSweepResult.Reopened` (Task 10); `roundSweepMaxPerTick` (Task 10 reuses, does not redefine).

**Two inconsistencies found and fixed inline.** (a) The gate ledger first predicted preview `57 → 57` while Task 8 rewrites 17 fixture files; the row now states that the CONTENT moves and the case count does not, and that a moved case is a finding rather than a retune. (b) An early draft had Task 7 minting a fifth `deliverSignal` signal name, which would have moved `Test_DeliverSignal_TheWireFormProducersAreAClosedList` from four producers to five; measuring `pumpLivenessProbeEpoch`'s existing child-side arm removed the need, and the gate-ledger row now says **4, unmoved** with the reason.

### 4. The three claims a reviewer should check first

1. **🔴 That the `deliverymanager.go` split is genuinely forbidden — C4 and Task 11.** R23's remediation table names "split by op family under the existing file-layout standard" as the owner of the 13,234-line finding, and this plan says the standard forbids exactly that. **The check:** read `docs/superpowers/specs/2026-07-11-layer-file-layout-standard-design.md` §2 Rules 1 and 4 (including the sentence *"Large files are an accepted consequence"* and its `projectstateaccess.go` precedent), then `server/internal/manager/delivery/deliverymanager.go:37-43`, then run `GOWORK=off go test ./internal/ -run TestFileLayout -count=1`. **If the standard does permit a second impl file, Task 11 is under-scoped and should do the real split.** If it does not, the correction in Task 11 Step 4 is the deliverable and nobody should read "done" as "the file was split".
2. **That the consumer gate was proved RED on three real instances, not merely written.** Task 4 Step 7 requires reverting Task 2's fix, reverting Task 1's `any` receive, and re-adding `ContentType`, watching the gate go red at each, and **pasting all three transcripts into the commit message.** **If those three outputs are not in the commit, the mitigation is unproven** — and this is the one item in the wave whose failure is silent, in the exact class it exists to catch. Check also that Task 4 Step 5's production run reported **zero** findings for the right reason (Tasks 1–3 fixed them) and not because the analyser skipped what it could not parse: the "unanalysable is a FAILURE" rule in Step 4 is what makes that distinguishable.
3. **That C1's resolution is what the founder meant.** The ruling says "flatten (no contract change, no regen) AND replace the bool with a three-value action whose `unknown` is REFUSED". Measured, those cannot both hold at the public façade: `DelinquencyContext.pauseNotWithdraw` is a non-pointer `bool` on REST and MCP, so an omitted field decodes as `false` and `false` means WITHDRAW. This plan buys the inner half — which closes the hazard that matters, an empty decode hard-withdrawing every in-flight app — and earmarks the outer half with its price. **The check:** read `operations/contract.gen.go:58-60`, `operations_handlers.gen.go:37` and `delinquencyenforcement.go:69-72`, then decide whether "unknown is refused" was meant to reach the REST caller. **If it was, add the `$defs` enum swap to Task 8 Step 4 — the plan says exactly where and what it costs — and do NOT open a second model edit for it.**
