# Stage 4b2 (the pump, the folds, the reads) — earmarks, carry-forwards, and one live defect outside this wave (2026-09-29)

Stage 4b2 shipped §5.1 of `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md`: **the react-by-signal pump** — `child.Get`'s blocking wait is gone, the pump parks on a selector, dispatches the whole eligible frontier in one pass and **grants a main-write lease** instead of serialising merges by blocking; plus the model edit that took revenue share out of the vocabulary and retired two kinds' draftability, one derived session view where two stood, the replan sweep's deletion, the guard census's verdicts, and the pump's first replay fixtures.

Branch `activity-experience-stage4b2` from `main` @`c5851e90`. 16 tasks planned, Task 8 dropped by the founder and Task 13 dropped as BLOCKED (its premise was false — §6). Code commits in landing order:

| Task | Commits |
|---|---|
| 1 — the pump guard census (the brief predicted 14 guards and 2 unarmed; the census landed 37 rows, later 36, with **15 armed by nothing**) | `78e1b458` |
| 2 — the steer precheck asks the ledger | `3f1746b3` |
| 3 — design questions file on main; the task gate stops asking which branch was entered last | `6ba40dd7` |
| 4 — the sweep's phase filter, G-S7, the meta-test re-key, the thread settlement | `a23b12f5`, `e4015c76`, `eb7177dd`, `4ee5c270`, `12c7655c` |
| 5 — one derived session view | `62e7106a` |
| 6 — the branch reconcile preserves a SET of slots | `635a7e7e` |
| 7 — THE ONE MODEL EDIT (revenue share out; two kinds undraftable; the session `$defs` fold; 12 ops → 11) | `ebfc1a42` |
| 9 — the Activity Experience mounts the three cross-slot providers; the MCP design gate is re-pointed at live state | `a574fac1` |
| 10 — a construction question says a person will answer it, and who | `eb3a9f78` |
| 11A — a joined gate attempt brings its sentence with it | `61df8899`, corrected `f549dede` |
| 11B — the replan sweep is deleted; the pump sweep is frozen | `f3673fb1` |
| 11C — an id nothing can select is an id nobody should read (153 orphan UI identifiers) | `05254eb5` |
| 12 — THE PUMP: the lease replaces the blocking child wait | `b84098eb`, fix round `aa977ae9` |
| 14 — the pump's replay fixtures, and the lease wire-form fix | `37b0e768` |
| 16 — the census's verdicts and its checked numbers | `cb244838` |

Gates at ship (measured at `cb244838`): registered-names golden **133** (134 − `ReplanSweepWorkflow`), frozen workflow names **15**, `Test_Replay_DeliveryHistories` **8/8** + `Test_Replay_PumpHistories` **6/6** = **14 fixtures**, `Test_LifecycleShapes` **11/11**, `make lint` **0 issues**, `fix-check` clean, 9× `gen-*-check` no drift, `encapsulation-check` + `derived-plan-check` green, `validate --root .. --slot System` **43 advisory / 0 errors** (unmoved all wave), `npm run check` **1234/1234**, uitests preview **57 over 23 fixture states**, `systemtests` builds and vets.

Claims below carry the task and the commit that produced them.

---

## Deploy note — 4b2 does not deploy, and not alone

The drain sequence is `docs/bugs/2026-09-24-stage3-rail-earmarks.md` and was amended in the same commit as this file. One drain covers stages **3 + 4a + 4b1 + 4b2**, once, before one release. What 4b2 changes in it: the drain now owes **THREE** `temporal schedule delete` calls (`delivery:replanSweep` joins the two abandoned `construction:*` ids) and step 6 confirms **TWO** sweep Schedules, not three. No new workflow TYPE name, no new workflow id family, no state migration. Read the amended note before releasing anything; this paragraph is a summary, not the procedure.

---

## 🔴 THE WIRE-FORM DEFECT — and it is NOT confined to delivery

### What happened here

**The whole stage-4b2 main-write lease was INERT in production until `37b0e768`.** All three lease messages ride `messageBus.deliverSignal`, which hands the Temporal client a bare `[]byte`; the default converter therefore tags the payload **binary/plain**, and `ByteSlicePayloadConverter` can assign that to nothing but a `*[]byte`. `pumpPark`, `drainForContinue` and `requestMainWriteLease` all received into the concrete STRUCT, so the SDK logged

> `Corrupted signal received on channel activityLeaseRequested … type *delivery.activityLeaseRequest: type is not *[]byte`

and **dropped every message**. Measured on a real Temporal dev server during Task 14's capture: no request reached the pump, no grant reached a child, no finish reached the pump; both children armed `NewTimer … Duration 2h0m0s` and the capture timed out. **Every merge tail armed its 2-hour budget and ran UNLEASED.** It failed OPEN — which is the documented, deliberate fail-open path — so nothing went red and no test caught it: **every existing lease test signals a STRUCT** (json/plain), a wire form production never produces. The file already knew: `pumpPauseRequested` decodes into `any` for exactly this reason and says so verbatim at the site. The lease channels were written against the same transport without inheriting the lesson.

The fix (`pumpReceiveSignal` / `pumpReceiveSignalBlocking` / `pumpDecodeSignal`, typed over a closed `pumpLeaseSignal` constraint, four call sites) receives into `any` and normalises both wire forms. It **emits no workflow command**, so no recorded sequence moved and the eight pre-4b2 child fixtures stayed 8/8. An undecodable LEASE message is DROPPED — deliberately the opposite of the pause rule, because a pause's channel NAME carries the operator's intent while a lease message whose activity id cannot be read names nobody.

### 🔴 THE FIFTH PRODUCER — A LIVE PRODUCTION DEFECT, PRE-EXISTING, OUTSIDE THIS WAVE, AND IT NEEDS A FOUNDER DECISION

Task 14 believed `deliverSignal` had three production call sites. **The review found FIVE.** The fifth is outside `delivery` entirely:

- **Producer:** `server/internal/manager/billing/shortfallsweep.go:118` sends `applyDelinquencyPolicy` to `{customerId}:delinquency`.
- **Consumer:** `server/internal/manager/operations/delinquencyenforcement.go:51` does `sigCh.Receive(ctx, &sig)` **into a concrete struct** — the identical defect.
- **And the payload shapes do not match.** The producer sends `deliverSignalPayload{CustomerID, PauseNotWithdraw}`; the consumer receives `applyDelinquencySignal{CustomerID, Context}`. **So fixing the wire form alone would leave `Context` empty** — the receiver would decode, and then act on a field nobody filled.

**Live consequence today: the delinquency-enforcement branch blocks FOREVER on `Receive`, and no app is ever paused or withdrawn for delinquency.** The SDK logs "Corrupted signal" and nothing goes red — the same silence that hid the lease for the whole of 4b2.

**This is not hygiene and it is not a gate item. It is a defect with a design question inside it, and the question is the founder's: what should `Context` carry, and is `PauseNotWithdraw` the thing it is meant to carry under a different name?** The shapes cannot be reconciled by a wire-form fix; somebody has to say what the enforcement branch is supposed to be told. Until that is answered, do not "fix the wire form" here — a decoding consumer acting on an empty `Context` is worse than a blocked one, because it would start pausing or withdrawing on a default.

### What is armed, and what is not

**Shipped (Task 16, `cb244838`): `Test_DeliverSignal_TheWireFormProducersAreAClosedList`.** It AST-reads every `MessageBusDeliverSignal` call site in the package's hand-written files, requires the name argument to be `messagebus.SignalName(<const>)` (a computed name is a channel nobody can check), and pins the producer set to exactly four: `signalOperatorPauseRequested`, `signalActivityLeaseRequested`, `signalActivityLeaseGranted`, `signalActivityFinished`. The failure message is the rule. It is a **producer** gate and it is package-scoped.

**Owed to 4b3: the CONSUMER gate, as a real arch gate.** The rule is *a signal delivered through `messageBus.deliverSignal` must never be received into a concrete struct*. A precise gate needs dataflow from `GetSignalChannel(ctx, name)` to a `Receive` target across a struct field (`pumpChannels`), a closure param (`AddReceive(ch, func(c …))`) and a helper func (`pumpReceiveSignal`) — go/types work beside `framework-go`'s other gates, not a package test. Two exceptions a real gate must answer, both measured:

1. **`pumpPausedAtRunStart`'s `DefaultVersion` arm** (`pumpnextactivity.go:1247`) receives into a struct **on purpose** — it is the pre-change body behind the `pump-pause-decode-any` fence. A gate that forbade it would force retiring that fence.
2. **`projectsupervision.go:48` is a LATENT instance.** `pauseCh.Receive(ctx, &sig)` into `operatorPauseSignal` is **correct today and wrong tomorrow**: its one producer is `client.SignalWithStartWorkflow` with a struct (json/plain), so nothing is dropped — but the signal NAME is the same `operatorPauseRequested` that `relayPauseToPump` delivers as raw bytes to a *different* execution id. The day anything relays a pause to `{p}:construction` through the bus, supervision drops it silently. **A name-keyed gate flags this; an id-keyed gate does not**, and choosing between those two rules is the gate author's first decision.

---

## The lease RUNS FOR THE FIRST TIME after this deploy

Because every lease message was dropped on the wire until `37b0e768`, the following were **dead code exercised only by tests using a wire form production never produces**:

- `activityLeaseGrantWaitBudget` — the **2-hour** grant budget a child waits out before giving up and running its merge tail unleased;
- the liveness probe and its reap path (`pumpCheckLease`);
- the lease **epoch** and its re-delivery rule;
- `pumpGrantLease`'s validation that the requester is a child of this run.

**The first real cascade after this release is the first time the admission queue has ever been in the path of a merge.** Watch it deliberately, not incidentally. Three things to measure on it: how long a tail actually holds the lease (the 2 h budget has to clear *other* activities' merge tails, each of which can hold a human approval gate); whether a busy project serialises behind the budget; and whether the grant→release→re-grant handshake behaves as the capture showed (`granted epoch 1 → released → re-granted epoch 2` in 7.4 s against the dev server).

---

## The lease invariant's true scope is per pump CHAIN, not per project

The pump's module header originally claimed PROJECT scope for "at most one activity holds the main-write lease". **It is per pump CHAIN**, and the header now says so (corrected in `aa977ae9`).

`LeaseHolder` lives only in `pumpState`/`pumpInput`, so **it survives a ContinueAsNew and nothing else**. Any pump run that ends while a lease is held — a pause while parked, a child failure stopping the cascade, any error return — exits WITH a holder. The 30 s sweep then starts a fresh chain with `LeaseHolder = nil` and an empty `Started` set; child A is still inside its merge tail (a human gate means minutes to hours); the new chain dispatches D, D asks for the lease and **is granted it**. A and D write main concurrently, and only the row-level CAS and the branch-file version guard prevent damage — i.e. exactly the pre-wave state.

Two consequences worth keeping:

- **"Fails open" is the NORMAL state after any pump restart, not a rare fault path.** A lease request from a pre-restart child is dropped by the new chain's `isStarted` validation, so that child runs its tail unleased by construction.
- **The lease sits ON TOP OF the row-level CAS; it never replaces it.** That ordering is why failing a completed activity because the admission queue is unreachable would be worse than the pre-wave behaviour, and it is why the child's lease request fails open by design.

---

## Fixtures, fences, and the measurement that must not be lost

### A replay fixture does NOT pin a `GetVersion` fence

Measured twice, by the implementer and independently by the reviewer:

- Deleting the `changeDesignActivitiesDispatchable` rung from `pumpEligibilityRule` left **all six** new pump fixtures **GREEN**. The SDK tolerates a recorded `Version` marker the replayed code never asks for.
- By contrast a real command change **does** bite: deleting the pace `Sleep` gives `[TMPRL1100] a matching Timer command was expected in history event position 34` on **2 of 6** (positions 34 and 33; the other four never reach a second loop iteration).

**Fixtures pin COMMANDS, not version rungs.** "Replay is 14/14 green" is therefore not evidence about a fence, and nobody may read it as licence to discharge one. The census's `…_DefaultVersion_…` tests are those rows' only pin.

### Every fence's `DefaultVersion` arm is unreachable by capture

`GetVersion` returns `maxSupported` on a new execution, so **no capture can ever produce a `DefaultVersion` history**. Those arms exist for executions already in flight. Four census rows hang off them, and if someone deletes one of those tests as "untestable in production" the fence goes unpinned and nothing else notices.

All five pump fences were deliberately KEPT at 4b2 (Task 16 refused to discharge them). The discharge is owed **after** the drain that covers 3 + 4a + 4b, as one edit deleting each fence, its census row's arm and its `…_DefaultVersion_…` test together — and the two measurements above must travel with that earmark as the counter-evidence to "the fixtures cover it now".

### The ContinueAsNew boundary is entirely unfixtured and CANNOT be captured

`pump-drain-pause-before-continue-as-new` v1's `GetVersion` is reached only once `pumpShouldContinueAsNew` is true — i.e. past `pumpHistoryBudget` (4000 events) — so **no capture can reach it**, and with it the whole ContinueAsNew boundary is unfixtured: the drain, the carry, and the replay of `Carried`. **By the pump's own header those are the riskiest ten lines in the file.** The one mutation that does cover the drain is a unit test (`continue-as-new-loses-no-signal`: deleting `st.drainForContinue` gives `SignalsDelivered = [], want [C-ONE:finish C-TWO:leaseRequest]`), which is a good pin and not a history.

**If one thing in this wave deserves a targeted non-replay guard, it is this.** A deterministic harness that forces the budget low enough to cross the boundary in a unit environment would buy what no capture can.

Also not covered, stated in code at `pumpReplayCases`: G-P12's pre-CAN row arm (needs a CAN), and a child FAILURE stopping the cascade.

---

## Deferred to 4b3

**1. The no-failure-row half of the broken merge tail.** The cascade is now SAFE — the pump stops on the child's future (F1, `aa977ae9`) — but head state still reads `Running`, or `Completed`-with-uncommitted-slots, for an activity whose tail broke, **so an operator sees no red node**. `pumpReconcile`'s "terminal failure row, no finish" arm cannot fire for a broken tail that outlives its pump run, because `finalizeWalk`'s tail errors bypass `failWalk` and no terminal row is written. Routing it through `failWalk` would write `VarianceExhausted` over an already-recorded `Completed` — **that is a decision about the documented heal-by-re-open path, not a line to add in a fix round.** Earmarked at the site.

**2. A child that dies without writing any row is invisible to the reconcile** and stays in `Started` forever. The lease HOLDER is probed and reaped; a non-holder zombie is not, because probing all N started activities every 30 s is N `deliverSignal` Activities per tick on a 30-activity plan. Wants either a real execution-status RA op (a contract change) or a staleness bound on `Started`.

**3. The per-wake-up whole-aggregate read is now STRUCTURAL — 1.17 MB every 30 s while parked.** The read-amplification win was DEFERRED, deliberately and by ruling: a long-lived lease pump must re-read on every wake-up because the CHILD writes head state and the frontier genuinely moves, so deriving from carried in-memory state is stale by construction. The real 4b2 win is the history churn of per-dispatch ContinueAsNew, not the read count — and a test asserts `reads >= 2` so no comment or commit text can claim otherwise. **This is the strongest argument yet for pulling the batched/narrowed plan read (`QueryProjectView(plan)`, R3/GAP-7) forward.**

**4. `PumpResult.activityIds` is an owed CONTRACT delta.** The field does not exist; Task 7's one model edit did not carry it and opening a second model edit + full self-amendment loop for one optional array was refused. Nothing was hand-edited. The whole frontier already rides the Manager-INTERNAL `queryPumpDispatch` payload (`pumpDispatch.ActivityIDs`, not a contract type), with `PumpResult.ActivityID` = the first and the deprecation stated at both sites. The exact delta, for the next model wave, on `deliveryManager.$defs.PumpResult`:

```json
"activityIds": { "type": "array", "items": { "$ref": "#/$defs/ActivityID" }, "x-go-name": "ActivityIDs" }
```

**5. `ReplanSweepResult` is DEAD CONTRACT SURFACE.** `ReplanSweepWorkflow` is gone (`f3673fb1`) but the model still defines the type, so `contract.gen.go`, `openapi.yaml`, the `systemtests` SDK and `schema.ts` all carry it with **no Go caller** (verified by the caller sweep, not by grep: two Go lines, both declarations). It trips no gate. Deleting it is a MODEL-level edit for the next model wave.

**6. Also 4b3, carried from earlier tasks in this wave:** the per-task session view (`constructState`'s single-valued view fields are clobbered on a fork, so a query sees the most recently entered gate — this is what an honest refusal of an early decision needs); the late-approve-applied-to-the-wrong-revision defect (pre-existing: `taskDecisionSignal` carries no round or revision identity, so a human who read revision *n* can have their approve applied to *n+1*; the fix shape is to put the round the human read in the signal payload — a stage check never could close it); the double `ReadActivityExecution` on the approve path (two git reads on the hottest write); the LIVE DOUBLE-WRITE where the child calls both `GitStatusRecordActivityStarted` and `ActivityExecutionOpenActivity` on the same row in the same walk with nobody having said which wins; and the artifact-as-of-revision read (R1/GAP-5), still owed — no op takes a ref.

---

## The deadness-claim lesson — this wave's dominant defect

**"No caller" from a grep was WRONG four times in this wave.** On record: `Solution.ClassRates` ("nothing reads it" — `SolutionView` does), the over-deleted exported `RegisterManagerWorker`/`RegisterSchedules`, the `statuses` channel "measured dead", and `detailPaneState.ts`. Nothing was actually deleted on a false premise — a dedicated verification sweep confirmed that — but only because the reviews caught each one.

**Why the grep lies: nine indirections reach a contract op and SIX are invisible to `\.Op(`.**

1. **The generated Temporal invoker** — and this is the one that defeated the original Task 13 recon: **the invoker method's prefix is the FIELD NAME in `genActivities`, not the contract name.** `GitActivityStatusAccess` is held in a field called `GitStatus`, so the method is `wf.Acts.GitStatusRecordActivityBranchOpened(…)` and `\.RecordActivityBranchOpened(` matches nothing. **Nothing in the repo states that map in one place.**
2. String-keyed worker registration.
3. The generated MCP tool catalog (`toolcatalog.gen.go`, served by `cmd/aiarch-state-mcp`) — **agent-reachable with zero Go call sites**.
4. Interface satisfaction.
5. Composition-root wiring.
6. The generated SDK / REST / webApp clients.

**The decisive check is: delete the member, then run `GOWORK=off go build ./... && go vet ./...` in BOTH `server/` and `systemtests/`** (vet type-checks tests, build does not). The converse hole is real too — 32 of `delivery`'s 70 invoker methods have no non-test call site, yet most are alive via direct synchronous calls.

### Adopted as standing rules

1. **Every deadness claim carries a COMPILE-PROBE TRANSCRIPT, never a bare grep.**
2. **`temporalgen` should EMIT the contract → invoker-prefix table.** The one fact that defeated the recon is written down nowhere.
3. **Extend `UIIdentifiers.test.ts` to assert the composed PREFIX of function-form ids.**
4. **Add a webApp orphan-module gate built on an import graph** (a ~15-line resolver was written during the sweep).
5. **Make "dead" a TWO-WORD VERDICT** — *uncalled* vs *unreachable* vs *unreferenced-in-Go-but-live-via-MCP*. Requiring the author to name which they measured would have caught `detailPaneState` on its own, because its author could not have named any of the three.

### 🔴 STRIKE THIS FROM ANY RETIREMENT LIST

**`webApp/src/components/construction/detail/detailPaneState.ts` has SEVEN production importers** — `activity/PlanList.tsx`, `activity/PlanTile.tsx`, `activity/planTiles.ts`, `construction/detail/bodies/taskBriefing.ts`, `construction/list/activityRowPresentation.ts`, `construction/list/activityTree.ts`, `construction/tasks/TasksLens.tsx` — and exports `taskDetailStateFill`, `TaskDetailState`, `RUN_NOT_WIRED_REASON`, `evidencePointerFor`, `selectedAttemptOf`, `taskDetailStateFor`, `outcomeStateOf`, `WIDE_PANE_SX`. Deleting it takes out the Plan tiles' fill, the task briefing and the Tasks lens. **It was listed as test-only; the claim came from a basename grep that hit prose comments.** The file still exists at HEAD — nothing was lost.

Verdicts from the same sweep, for anyone reading the 4b1/4b2 lists: `provenance.tsx` TRUE; `toolbarForLens` / `activityMeta` / `listEmptyState` / `searchExpansion` / `decisionRecords` TRUE; `observedOnly` TRUE **but six tests depend on it**; `decisionFlow` PARTLY FALSE (also imported by production `decisionRecords.ts` — retire the pair together); `scrollerGeometry.ts` FALSE in the safe direction (zero importers AND no test); `FocusRailContext` / `ScenarioLinkContext` / `inFocus` all TRUE. Four true orphans the wave never listed: `AwaitingPanel.tsx`, `ProjectArtifactRenderer.tsx`, `EpisodesPanelContainer.tsx`, `contracts/constructionRows.ts`.

### Task 13's premise, and why `gitActivityStatusAccess` survived

The recon said "zero production callers on all six ops". Corrected counts, excluding `_test.go` and `*.gen.go`: `BranchOpened` 1, `CIObserved` 2, `ArchApproved` 1, `Merged` 1, `Started` 1, `Completed` 1 = **7 live call sites; every one of the six ops has a caller.** All trace to the generic DAG child's spine (`openActivityRow`, `finalizeActivity`); no `GetVersion` retires them; the composition root binds the facet non-nil in BOTH profiles. `RecordActivityStarted`/`RecordActivityCompleted` are not git mirrors at all — they upsert the rows `AllDepsSatisfied` reads, and `Completed`'s own doc says it "unblocks its dependents", so **deleting them stalls the pump, this wave's whole subject.** `ActivityExecutionAccess` is at exactly 12/12 and cannot absorb a verb; the enabling move is the `projectCatalogAccess` split, in no row of this wave. `DH-CONTRACT-DEADOP ×2` never flagged these six — an independent signal that they have live callers.

One MODEL-level correction falls out of it and is owed to the next model wave: the committed contract note's stage-3 deferral states a reason that has EXPIRED (all nine fixtures recording these names now live under `testdata/replay-archive/`, which no test reads). The real blocker is the 12-op ceiling plus three unsuperseded verbs.

---

## Smaller carries

- **The `temporal` CLI is a HARD DEPENDENCY of both capture tools, stated only in a `t.Fatalf`** (`Test_Capture_DeliveryHistories`, `Test_Capture_PumpHistories`). It is in no README and no Makefile target, so a fresh checkout discovers it by failing. Version here: **1.7.0 (Server 1.31.0)**. `CONSTRUCT_HISTORY_CAPTURE_CASE=<name>` re-takes a single fixture and is documented only in the tool's own comment.
- **Three `.claude/commands/*.md` could not be deleted.** `.gitignore:29` ignores `/.claude/commands/`; the tree is MATERIALIZED from the pinned `method-assets` by `make claude-assets`, which `make test` runs, so deleting them is an untrackable no-op the next `make test` undoes. The retirement of `scrubbedRequirements` / `standardCheck` draftability is enforced by the EMPTY slug in `designKindSlugs` instead. **The file deletion rides the next method-assets release.**
- **`construction-round-withdrawn.json`'s three change requests still carry an addressee outside the generated enum.** Task 10 fixed one occurrence to `"pm"`; three remain at `:31408`, `:31428`, `:31519` carrying `"seniorDeveloper"`, which is outside `ReviewCommentAddressee` and is therefore DROPPED by the adapter. **Nothing gates preview fixtures against that enum** — one clean pass plus a fixture-vs-enum assertion closes it.
- **`AskQuestions` validates non-empty but not the vocabulary.** `deliverymanager.go:10625` refuses an EMPTY addressee and nothing else, so a value outside `{pm, architect}` can be persisted and then emitted on a wire whose enum forbids it. The server half of the item above.
- **The reachability sequel to the orphan guard is its own task (likely 4b3).** The UI-identifier guard proves an id is REFERENCED, not that the reference can ever RUN, and two clusters hide behind that: `Construction.PROVENANCE_RAIL`/`PROVENANCE_BADGE` are placed only by `components/construction/provenance.tsx`, which nothing imports; and `ServiceContract.OPEN_FOCUS`, `CANVAS_NEEDS_ROOM*`, `CODE_CANVAS*`, `CODE_INTERFACE_NODE`, `STRUCT_CARD` sit on `ServiceContractView`'s `inFocus` branches, **which can never be true** (nothing provides `FocusRailContext` and no caller passes `inFocus`). Both count as "referenced" and survive. Related: **ten construction modules are imported ONLY by their own tests** — green tests proving nothing about the app.
- **The MCP widget has NO preview surface at all.** `mcpShell` is not booted by `previewShell`, so Task 9's re-pointed design gate is covered by unit tests and the compiler only: a wrong `taskId` would be a silently-shut gate nothing in CI catches — **the same class of silence as the bug it fixed** (the widget's approve/reject gate had been dead since 4b1, because its derived data source stopped emitting the stages the branches read). The wiring is now pinned by a source-scan test; a real preview surface is the durable fix.
- **Two orphan React contexts**, recorded as a UX decision rather than a re-mount: `FocusRailContext` and `scenarioLink`'s `ScenarioLinkContext` have no `.Provider` anywhere in `src`. Their mounts died with the construction console (stage 5 Task 13). Neither is a data lens — one controls a collapsible panel this screen does not have, the other a `?sc=` URL parameter this route does not carry — so mounting either means adding the thing it controls. **Nothing gates a THIRD screen appearing without the three cross-slot providers.**
- **Two meta-test holes in the UI-identifier guard**, both from the review of `05254eb5`: any PROSE mention of `Namespace.key` in a non-excluded file vouches for that id (only three files are excluded, so a tombstone comment written elsewhere keeps a dead id green forever) — strip comments from the corpus; and `leafKeys` is INDENTATION-EXACT (`^ {2}\w+: \{$` / `^ {4}\w+:`), so a prettier width change or one nested object silently removes leaves from the scan with no signal — a count assertion against a known total catches it.
- **Doc/model staleness for the next model wave:** the `ActiveRole`/`ActiveStep` `$defs` cascade was not taken (it is a `project.json` edit plus a second regen of committed state, with `gen-enums.mjs`'s `OUTPUT_NAMES` and `enums.gen.ts`'s two blocks); the rewritten slot-8 notes state the hosting-fee sentence twice; `ReviewSubjectRef.kind`'s description still says `commit` is what "a future subject-by-sha writer will use" (stale since 4b1's `38fd7f9c`); `RevenueShareNone` is still the missing vocabulary member that `RevenueShareNegotiatedRate at 0%` stands in for.
- **Prose still naming things that moved:** five doc lines state revenue share as current (`billingengine.go:2` and `:20`, `billingmanager.go:3`, `projectstateaccess.go:4588`, `docs/later.md:58`); `deliverymanager.go:13165` says `querySessionState` returns a `SessionStateView` where the handlers return `ConstructionSessionView`; `internal/arch_test.go:943` calls `ReconcileBranchFromMain` "a required ProjectStateAccess op" (it is `DesignSessionAccess`-only); `ServiceContractView`'s prose describes a focus view that no longer exists; `homebase.spec.ts:47` still regexes `/^phase-card-/` — a raw literal asserting ABSENCE, so it passes and is now silently unfalsifiable.
- **`project-design-m0-defaulted` is a MISNOMER.** The fixture was minted for the retired "platform wrote slot 8" arm; it now means an AUTHORED slot 8 with no detail. Not renamed, recorded.
- **The `-defaulted` pair's meaning, so nobody reads it as broken plumbing:** on THIS repo the M0 cost-basis detail is EMPTY, and that is the PROOF the founder's revenue-share ruling closed the uncomputable-SDP finding at source — with revenue share gone, slot 8 is fully authored and `computeProjectPlanSlots` defaults nothing. **An absent detail is "nothing was assumed", never broken plumbing.** Said in the module header at the site.
- **Shape-rig artefact:** `shapeSDPActivity = "P-SDP"` is not a production activity id and mis-classifies (Documentation → no `sdpReview` task → kind nil), so two façade M0 tests and the `projectDesign` shape rig exercise a kind resolution production never takes. Rename to `projectDesign` (it touches several shape cases). The measurement that depends on it: **"`settleThreadsBeforeApprove` has never run at M0" is TRUE of the test rig and FALSE of production** — no downstream plan may assume M0 was ungated since 4b1.
- **`Test_LifecycleShapes` load-flake, for anyone reading a red run:** it does NOT reproduce serially (0/30 at base); it needs LOAD (3/40 at 8-way concurrency, 7.5%) and it hits `fork-join-service-design-first` as well as `stp-first`. Task 4 fixed the root cause for the declaration-order claim by moving it to `readyTasks` (pure, 100% instead of ~92%), but **a serial CI run understates the class; nobody should declare a future flake "gone" from a serial run.**
- **Two unrelated pre-existing hangs/flakes seen this wave:** `TestRegisterOperatedSystem_AlreadyRegistered_Conflict` hangs at 10 m under full non-short `./...` (not in `-short`, not touched here); and `golangci-lint cache clean` must run FIRST in any worktree — a stale cache printed 30 phantom issues pointing into a DELETED sibling worktree.
- **`progress.md` and Task 1's report still say the census holds 37 rows.** It holds **36** (38 − 2), and the census now fails if it lies about its own numbers; the two stale documents do not.

---

## Founder decisions owed — and one that closed itself

1. **🔴 What should the delinquency signal's `Context` carry?** The live defect at the top of this file. Nothing is paused or withdrawn for delinquency today, and the two payload shapes cannot be reconciled without an answer. This is the one that costs money while it waits.
2. **GAP-4B-4's three orphan kinds**, carried from 4b1.
3. **`RevenueShareNone`** as a vocabulary member — `RevenueShareNegotiatedRate` at 0 % is standing in for it, which is truthful but is an encoding, not a member. (4b2 removed revenue share from the *model* entirely, so this is now a billing-vocabulary question, not an SDP one.)
4. **The assumed-planning-assumptions UX** — an operating-model screen, and a way to tell "assumed" from "accepted" once the founder has read them. The disclosure itself now works for the first time: the M0 attempt `Detail` was produced and dropped on every run until Task 7 persisted it.
5. **The construction answer command** — a construction question is recorded and rendered and nothing can answer it (`respondToReviewComment` is slot-scoped and unregistered in the construction job mode).

**CLOSED by measurement rather than by ruling: design questions are filed on MAIN.** 4b1 left "whether design questions belong on the activity branch or on main" as an open founder question; Task 3 (`6ba40dd7`) found `resolveQuestionBranch` was dead **by type** — it could only ever return `""` — so every design question has always been filed on main, and the resolver, `isLiveSessionStage`, `readProjectMaybeBranch` and `projectstate.DesignBranch` were deleted with it. The question was about a choice nothing was making.

---

## The lesson this wave is owed by name

**A fix that reads right, passes, and does not fire on the case it names is this programme's signature failure — and the controller produced one.** Two of the three M0 fixes in this wave looked correct and were INERT: `roundRevisions` copied a gate attempt's `Detail` only inside `attemptGateKey(a) == roundJoinKey(r)`, and because `roundJoinKey` includes the artifact kind while `attemptGateKey` is hardcoded kindless, at M0 the keys are `"sdpReview:sdpReview:1"` vs `"sdpReview:1"` and **can never match**. The commit message, the comment at the site and the test's doc were all false for the one gate they named, and the test's fourth assertion passed a round with no attempts supplied — so it CERTIFIED the broken shape as correct. Corrected at `f549dede` with `soleKindClaims`.

**The review loop caught both. Nothing else would have.** The same holds for the census: it named the rows and made the review a checklist, but the two guards lost *inside* this wave (G-P12, G-P10) were found by a human reading a new body against the old guard, with a machine-checked census in front of them the whole time. **A future wave that treats "the census is green" as coverage will repeat 4b1.**
