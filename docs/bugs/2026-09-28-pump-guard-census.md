# The pump guard census — every guard-shaped line in the four files stage 4b2 rewrites

**Measured 2026-09-28 against `c5851e90` (the pump files are byte-identical to `bae7681f`,
which the 4b2 plan verified its eleven rows at). Read-only: this census changed no
production line.**

## Why this document exists

Stage 4b1 deleted bodies and lost **eight preconditions** inside them — the
open-review-comment guard (a live `vibes` autogate regression that shipped for two waves),
`runTask`'s pre-`recordTaskAttempt` return, `dispatchConstructionOnce`'s
pre-`resolveWorkAttempt` return, `eligibleUnder`'s `==`-where-cumulative, and four more the
final fix wave found. **Every one was invisible to the full test suite.** The fakes still
modelled them; no test armed them. They were found by a reviewer reading the retired rail's
guards against the new one, by hand, after the fact.

Stage 4b2 rewrites `PumpNextActivityWorkflow` (Task 12) and deletes `ReplanSweepWorkflow`
outright (Task 11). **The pump is this wave's deleted body.** So its guards come out of it
FIRST — as this list and as executable assertions — and **Task 16 walks this list row by
row rather than re-deriving it.**

A guard recorded in prose is a guard nobody re-runs. Every row below names a Go test, and
`Test_PumpGuardCensus_EveryGuardIsPinned` fails if a row names none or names one that does
not exist. `Test_PumpGuardCensus_TheDocAndTheCodeAgree` fails if this document and that
census disagree about the ID set, so the two halves cannot drift.

## What a row means

| column | meaning |
|---|---|
| **ID** | the stable handle. **Bolded, in the first cell, at the start of the line** — that shape is what `Test_PumpGuardCensus_TheDocAndTheCodeAgree` reads as a ROW, so a mention in prose is not one. |
| **Line** | at `c5851e90`. Task 12 moves these; the ID is the stable handle, not the number. |
| **Verdict (Task 16)** | what the SHIPPED code does with this guard, and WHERE. One of **RE-ASSERTED** (with its new `file:line`), **DELETED WITH ITS SUBJECT** (with the reason), **LOST** (a blocker, never a note) — or, for exactly one row, **RE-ASSERTED IN REDUCED FORM**, which is the fourth verdict Task 16 had to invent and defends in the closing section. The line numbers in this cell are relative to the row's own file, like the `Line` column. **They are RE-MEASURED at `d3a4f898`, the wave's last code commit** — Task 16 wrote them at `cb244838` and the commit after it added ~55 net lines to `pumpnextactivity.go`, which left 21 of these 22 rows pointing at the wrong line (G-P17 pointed at a `Reason` type assertion, so the row read as though `PARENT_CLOSE_POLICY_ABANDON` had been deleted). **NOTHING GATES THIS CELL'S CITATION.** The two meta-tests compare the ID set and the `PinnedBy` cell; the `Line` column matches `pumpGuard.Site`, which is the *pre-wave* provenance line and does not move. The next commit that touches these files makes this cell stale again and every test stays green — see the earmark in `2026-09-28-stage4b2-earmarks.md`. |
| **Guard** | the line, and what it refuses / gates / orders / versions |
| **What it protects** | the concrete failure it prevents — never a restatement of the code |
| **BreaksAs** | the observable symptom if the new pump does not re-assert it |
| **PinnedBy** | the Go test that re-runs it. **"a fake models it" is not coverage.** The meta-test compares this cell against `pumpGuardCensus()`'s own `PinnedBy` and requires every test it names to exist, so the doc cannot name a different test — or a deleted one — and stay green. A cell may name MORE tests than the code does (an arm apiece); it may not name fewer or other. |

**Seven columns, and they used to be five.** The header declared five while every row
carried six, so Markdown DROPPED the last one: `PinnedBy` — the census's whole point — did
not render at all. Fixed by stage 4b2 Task 4 with the meta-test that now walks these cells;
Task 16 added the seventh, `Verdict`, and moved `pumpGuardDocRowCells` 6 → 7 with it, which
is the one place the row SHAPE is written down.

## Headline counts

**THE TOTAL IS 36, NOT 37.** Task 1's report and `progress.md` both say 37, and both are
stale by one: they were written before Task 11 discharged `replansweep.go`'s two rows and
before Task 4 added `G-S7`, and 38 − 2 = 36. The arithmetic that settles it is the
per-file line below — 22 + 7 + 7 — and `pumpGuardCensus()` returns exactly that many, which
is the number Task 16 walks.

| | |
|---|---:|
| Guard rows total | **36** |
| `pumpnextactivity.go` | 22 |
| `pumpsweep.go` | 7 |
| `projectsupervision.go` | 7 |
| Rows the 4b2 plan's brief names | 12 |
| **Rows this census found that the brief does not name** | **24** |
| **Rows NOTHING armed before the census** | **14** |
| Rows armed by a test written for the census | 14 |
| Rows with no pin | **0** |

**`replansweep.go`'s two rows, G-R1 and G-R2, are DISCHARGED and gone (Task 11).** They were
listed to be shown to protect nothing that survives, and they were: both pinning tests ran
green, the all-projects arm had no reachable caller over either transport, and the workflow
they guarded is deleted. The counts above are the census's post-discharge shape — 38 - 2 —
because a row whose subject no longer exists is not a guard, and `Test_PumpGuardCensus_-
TheDocAndTheCodeAgree` fails a doc row the code no longer knows.

**One of the fourteen is not Task 1's.** `G-S7` — `PARENT_CLOSE_POLICY_ABANDON` on the
SWEEP's child pump start — was missed by the census and found by its reviewer, who
measured that deleting the policy leaves the **entire delivery package GREEN**. Stage 4b2
Task 4 added the row, the pin and the code comment the line never had. A census that can
miss a row is why the meta-tests exist; a census that cannot be added to after the fact
would be worse.

## THE UNARMED LIST — the census's whole point

These fourteen guards had **no test at all** before they were pinned. Each is a line whose
removal the entire suite would have accepted in silence — the exact shape of the eight 4b1
lost. Nine of them live in `pumpnextactivity.go`, the body Task 12 rewrites.

| ID | The guard nothing armed | Why the silence is dangerous |
|---|---|---|
| **G-P12** | `child.Get`'s error arm — a failed child **FAILS the pump run** | **The single most important row in this census.** This is how a cascade STOPS. Both `OnWorkflow(executionKindDeliveryActivity, …)` mocks in the suite `Return(nil)`; **no test has ever failed a child.** A react-by-signal pump has no error channel at all (`deliveryActivity` has no failure-signal producer), so this guard is not merely at risk of being dropped — it has no obvious replacement. |
| **G-P13** | the happens-after ordering `child.Get` buys: the pump physically cannot re-select while the child runs | Ordering is structural today, so no test asserts it. Once the block goes, it becomes a *claim*, and its failure mode is re-dispatching a still-Running activity. |
| **G-P10** | the ContinueAsNew payload's **bound** (the carry is pinned; the bound is not) | Task 12 grows the payload. Unbounded carry across a per-activity ContinueAsNew chain is unbounded history. |
| **G-P1** | the Query handler is registered **before any blocking call** (the ordering half) | Every existing query runs after the run closed. Registered late, `awaitDispatchDecision` polls a run that cannot serve it and falls through to `terminalPumpResult` — a slow dispatch reported as a closed pump. |
| **G-P8** | the dispatch decision is recorded **before** the block | The façade tests observe the Query's answer; nothing asserted the ORDER. Recorded after the block, every `Begin` blocks for the whole drain. |
| **G-P15** | the failure RECORD's own error arm — a failed `RecordActivityFailed` fails the run | The loudness of G-P5 is not best-effort. Swallow this and a blocked frontier is invisible again, which is the defect G-P5 exists to end. |
| **G-P14** | the 1 s pace between cascade iterations | An unpaced pump busy-spins ContinueAsNew. The constant appeared in **zero** test files. |
| **G-P17** | `PARENT_CLOSE_POLICY_ABANDON` on the child start | `PARENT_CLOSE` appears in **no** test file in the package. The pump's own close (or ContinueAsNew) killing every in-flight activity is a silent, catastrophic regression. |
| **G-S7** | `PARENT_CLOSE_POLICY_ABANDON` on the **SWEEP's** child pump start | **The row the census itself missed.** Measured by Task 1's reviewer: removing the policy leaves the entire delivery package GREEN. It is G-P17 one level up, and the level is what makes it worse — a sweep tick lives for *milliseconds* (it waits for the start ack alone, G-S4) and the pump it starts runs for hours, so the default TERMINATE kills **every pump the platform starts by itself** a moment after it is born. The platform's whole self-start path, with no symptom but pumps that vanish. |
| **G-P21** | the eligibility ladder's **second** rung (`design-activities-dispatchable`) | Rung 1 has a DefaultVersion test; rung 2 has none. A dropped rung silently un-dispatches the three design activities. |
| **G-S5** | `ListProjects`'s error arm — the whole sweep tick fails, no partial fan-out | The lister fake could not fail. A swallowed enumeration error is a sweep that silently pumps a subset of the platform. |
| **G-S6** | `pumpSweepOwnerScope` is non-empty | An empty scope is `fwra.ContractMisuse` at the RA: every sweep tick fails, platform-wide, and the only symptom is a Schedule log. |
| **G-V1** | supervision's `querySessionState` handler is registered **before** the blocking `Receive` | Same class as G-P1, on the other long-lived workflow. |
| **G-V6** | `!plan.RecordPaused` ⇒ **no** head-state write | Every pause test set `RecordPaused: true`. The engine's decision not to record was never honoured under test. |

---

## `pumpnextactivity.go` — 22 guards

| ID | Line | Verdict (Task 16) | Guard | What it protects | BreaksAs | PinnedBy |
|---|---|---|---|---|---|---|
| **G-P1** | `:61-66` | **RE-ASSERTED** · `:531-536` | `SetQueryHandler(queryPumpDispatch)` registered BEFORE any blocking call; its own `err` arm fails the run rather than running blind | The façade's synchronous `ExecuteNextActivity` reads this Query. Registered late, `awaitDispatchDecision` polls a run that cannot serve it. | A `Begin` on a healthy project returns an Infrastructure error or `terminalPumpResult`'s answer — a slow dispatch reported as a closed pump. | `Test_Pump_DispatchQueryIsServedBeforeTheFirstBlockingCall` |
| **G-P2** | `:92-98` | **RE-ASSERTED** · `:563-568` | `pumpPausedAtRunStart` ⇒ quiet return, **and NO ContinueAsNew** | An operator pause at run start stops the cascade. *The no-ContinueAsNew half is the guard*: a paused pump that continues-as-new re-enters and dispatches on the next run. | PauseProject appears to work, then the project keeps building. | `Test_Pump_PauseSignal_HaltsCascade_NoDispatch` |
| **G-P3** | `:100-108` | **RE-ASSERTED** · `:689-701` | `isReadNotFound(err)` ⇒ quiet `PumpResult{}`, not an error; every OTHER read error still fails the run | A project with no state yet is a normal quiet tick. | Returned as an error it fails the Schedule's child start and logs a platform-wide sweep error every 30 s. | `Test_Pump_ProjectNotFound_QuietTick` |
| **G-P4** | `:116-121` | **RE-ASSERTED** · `:702-714` | `pumpHonorsRecordedPause`, **placed before `nextEligible`** | The supervision pause branch RECORDS before it relays, so a pump the sweep restarts inside the relay window sees the recorded pause. Placed AFTER `nextEligible` it would still act on the frontier. | A paused project keeps dispatching, or writes a durable failure record while paused. | `Test_Pump_SweepStarted_RecordedPause_BlockedFrontier_NoFailureRecord` |
| **G-P5** | `:133-168` | **RE-ASSERTED**, widened · `:769-783` + `:861-875` | `verdictBlocked` writes `ConstructionTransitionRecordActivityFailed` through `applyRecovering` and returns quiet | **LOUD, DURABLE, APP-VISIBLE.** "A warning buried in a serve log is how this defect consumed an entire benchmark run undetected." The write also takes the activity out of NotStarted so the next tick considers the rest of the network. | A plan defect becomes a silent quiescent pump: 29 activities to build, and the project looks finished. | `Test_Pump_BlockedActivity_RecordsTerminalFailure` |
| **G-P6** | `:169-172` | **RE-ASSERTED** · `:639-648` | `verdictQuiescent` returns **WITHOUT** ContinueAsNew | The cascade's own drain-to-quiet is what ENDS the pump. | ContinueAsNew here is an infinite pump: one run per second, forever, per project. | `Test_Pump_DrainedNetwork_QuietNoContinueAsNew` |
| **G-P7** | `:187-192` | **RE-ASSERTED** · `:792-802` | `pumpPausedBehindGate("pump-pause-before-dispatch")`, between `readProject` and the child start | `readProject` is an Activity, so a pause can land after G-P2 and before the dispatch. **Stated BOUND:** a pause arriving DURING the dispatching workflow task is honoured at G-P9 instead, after exactly one activity. | A pause lands mid-read and one more activity is dispatched with nothing able to cancel it (`PipelinesToCancel` is empty). | `Test_Pump_PauseDuringReadProject_NoNewDispatch` |
| **G-P8** | `:219-220` | **RE-ASSERTED**, stronger · `:624-630` | `dispatch = {Decided, Dispatched, ActivityID, ActivityIDs}` recorded **BEFORE** the block — **Task 12: after the WHOLE frontier is started and before the selector park** | The façade returns this tick's decision without waiting for the cascade. Stronger in the new shape: a `Begin` that joined a run dispatching five activities still returns within a workflow task. | Recorded after the block, every `Begin` blocks for the whole drain — hours — and then times out at `pumpDispatchWaitBudget`. | `Test_Pump_DispatchDecisionIsReadableBeforeTheCascadeDrains` |
| **G-P9** | `:252-256` | **RE-ASSERTED**, generalised · `:1194-1199` + `:1213-1243` + `:578-596` | `pumpPausedBehindGate("pump-drain-pause-before-continue-as-new")` — **Task 12 GENERALISED it to all three channels** (`pumpContinueAsNew` + `drainForContinue` + `replayCarried`) | **"A signal still buffered on a run that ends in ContinueAsNew is NOT carried into the next run."** True for one channel, now true for three: a lost finish or lease request stops the project, and there is no second pump. | A pause that lands while the run is parked is discarded by ContinueAsNew and the next run dispatches; generalised, a lost finish leaves the pump waiting on an activity that is over. | `Test_Pump_PauseDuringChildGet_StopsCascadeAfterCurrentActivity`, `Test_Pump_DrainPause_StopsTheCascadeInsteadOfContinuing` |
| **G-P10** | `:259` | **RE-ASSERTED** · `:1200`, bound at `:74-120`; LOST INSIDE THE WAVE, fixed `aa977ae9` | ContinueAsNew carries **ONLY** `pumpInput` — and carries the WHOLE of it. **Task 12 grew it to 7 members and every one states its bound** in `pumpContinueAsNewCarry`. **FIX ROUND 1 made the declared bound TRUE:** `Finished` and `Carried` are bounded by the plan only if the ids in them are ids the pump STARTED, and neither the finish arm nor the pre-CAN drain checked — 50 fabricated ids were accepted and carried. Both doors now mirror `pumpGrantLease`'s drop | Unbounded history is avoided and determinism is trivial; the whole-input carry keeps a v1 cascade's mandate. | An unbounded carry is unbounded history and an eventual payload-size failure that wedges the project's one pump. A bound stated over a set an unauthenticated signaler can grow is a bound on the sender's manners. | `Test_Pump_ContinueAsNewPayloadIsBoundedByThePlan` (bound, now with the fabrication case at both doors) + `Test_Pump_ContinueAsNew_CarriesOperatorDriven` (carry) |
| **G-P11** | `:327-345` | **RE-ASSERTED** · `:1324-1342` | an **UNDECODABLE** pause still counts as a pause | Dropping a pause over a malformed body fails OPEN — the cascade keeps dispatching through an operator halt. Counting it fails SAFE. | An operator halt is ignored because a byte payload did not parse. | `Test_Pump_UndecodablePauseSignal_StillPauses` |
| **G-P12** | `:230-232` | **RE-ASSERTED**, degraded pre-CAN · `:1057-1060` + `:1071-1080` + `:502-520`; LOST INSIDE THE WAVE, fixed `aa977ae9`, and its last two back doors closed in FIX ROUND 2 (C1) at `:465-468` + `:1085-1097` | the failed-child arm: **a failed child FAILS the pump run**. **Task 12 kept `ExecuteChildWorkflow` and selects over the N futures**, so the error channel survives for children of THIS run; for a child that predates a ContinueAsNew the reconcile tick APPLIES the same semantics off the row (terminal FAILURE + no finish signal = `failWalk`). **FIX ROUND 1 restored it after the commit's own lease release defeated it:** `finalizeWalk`'s deferred release rides the `activityFinished` signal and fires on EVERY exit including the failing ones, so a broken merge tail reported a finish FIRST, `markFinished` deleted the child's future, and the run went QUIESCENT. A finish now never discharges a future this run holds (`applyFinish`). **FIX ROUND 2 (C1) closed the two sites that still did:** the grant delivery's and the liveness probe's NotFound arms called `markFinished` on a child of THIS run, so a holder that `failWalk`ed in its merge tail — no finish, lease still held — was discharged by the probe and its error swallowed. Both now `markVanished`: release the admission, keep the future, and let the next reconcile read the future or a FRESH row; a vanished child with no terminal either way stops the cascade rather than being counted finished or waited on forever | This is how a cascade STOPS on a broken activity instead of marching down the frontier. | A failing activity is retried forever by the next tick, or the cascade walks past it and builds on a broken dependency. **DEGRADED, stated:** for a pre-CAN child the cascade stops within one wake-up rather than instantly, and only if the row was written OR the pump probed its execution and found it gone (C1's vanished arm); a broken merge tail that never held the lease still writes NO row at all, because `finalizeWalk`'s tail errors bypass `failWalk` (earmarked at that site, fix round 1). | `Test_Pump_AFailedChildFailsTheRunAndStopsTheCascade` + `Test_Pump_AFinishReportFollowedByAChildFailureStillFailsTheRun` (the finish-then-fail shape, which the first case cannot reach) + `Test_Pump_ALeaseHolderFoundGoneStillFailsTheRunWhenItsChildFailed` and `Test_Pump_AVanishedPreContinueChildWithNoTerminalFailsTheRun` (C1's two NotFound doors) |
| **G-P13** | `:221-230` | **RE-ASSERTED IN REDUCED FORM** — the wave's one non-parity verdict · `:913`, `:787-790`, `:689` | **Ordering / happens-after — REPLACED, not preserved, by Task 12, and the replacement covers ONE of its two halves properly.** The pump dispatches N children in parallel ON PURPOSE, so "cannot re-select while a child runs" is deliberately abandoned; what stands in its place is LEASE EXCLUSIVITY plus a re-read on every wake-up plus `pumpState.Started`. **Half A (two children write main at once) is pinned, at CHAIN scope only** — `Test_Pump_GrantsAtMostOneLease` is one pump run, and `LeaseHolder` lives only in `pumpState`/`pumpInput`, so nothing pins it across a pump restart (see the module header's chain-boundary note; the row-level CAS is what holds there). **Half B (the same activity is dispatched twice) is pinned only by assertion 2 of `TheNextSelectionWaitsForTheChildsTerminal`, and that assertion's OWN comment flags it as meaning-changing** — it reads `children == 1` and survives only because the rig's selection double hands back one activity forever, so it silently stops meaning "at most once" the day a rig dispatches two | Without a replacement `nextEligible` re-selects a still-Running activity, or two children race one main-write. | The same activity is dispatched twice, or two children write main at once. **Do not read "replaced" as parity:** half B has no pin that is robust to the rig, and half A has none across a chain boundary. | `Test_Pump_TheNextSelectionWaitsForTheChildsTerminal` (rewritten: liveness, at-most-once, re-read) + `Test_Pump_GrantsAtMostOneLease` (what replaced ordering, within one chain) |
| **G-P14** | `:242-244` | **RE-ASSERTED** · `:675-682` | `workflow.Sleep(pumpPaceInterval = 1s)` — **Task 12 moved it between WAKE-UPS** (the first iteration is unpaced, so a `Begin` is not delayed), and its own error arm | Keeps the loop from busy-spinning. Worse in the new shape: each iteration also builds a durable head-state read and a fresh timer. | An unpaced pump burns a workflow task per iteration and floods history and the task queue. | `Test_Pump_TheCascadeIsPacedBetweenIterations` |
| **G-P15** | `:164-166` | **RE-ASSERTED** · `:874` via `:775-777` | the failure record's own error arm — a failed `RecordActivityFailed` **fails the run** | G-P5's loudness is not best-effort: if the durable record cannot land, the run must not report a clean quiet tick. | A blocked frontier with a write failure becomes exactly the silent quiescent pump G-P5 exists to end. | `Test_Pump_BlockedActivity_AFailedFailureRecordFailsTheRun` |
| **G-P16** | `:386` | **RE-ASSERTED** · `:1391` | the child is addressed by `deliveryActivityWorkflowID(projectID, id)` — **idempotency / dedup** | A redundant tick collapses onto the running child instead of starting a second one. A hand-built id is how the pump and the façade disagree about which execution to signal. | Two executions for one activity, both writing the same row. | `Test_Pump_StartsOneChildAndNamesNoActivityType` |
| **G-P17** | `:387` | **RE-ASSERTED**, pin strengthened · `:1392` | `ParentClosePolicy: PARENT_CLOSE_POLICY_ABANDON`. **Task 12 re-asserted it STRUCTURALLY** (`pumpChildOptionsField` walks the AST to the key) because the pin was a substring check and the line now carries a comment naming the constant — the old test would have stayed green with the assignment deleted | The activity is its own durable execution, independent of this pump's continue-as-new chain. | Drop it and the pump's own close — every ContinueAsNew, every failure — terminates every in-flight activity. Silent, and catastrophic. | `Test_Pump_TheChildIsAbandonedSoThePumpsOwnCloseNeverKillsIt` |
| **G-P18** | `:268-273` | **RE-ASSERTED**, NOT discharged · `:1266` (`:798`, `:1194`) | `pumpPausedBehindGate`'s `GetVersion` fence — **two change ids through one func** (`pump-pause-before-dispatch`, `pump-drain-pause-before-continue-as-new`). Default ⇒ skip the check entirely | A pre-change execution keeps its recorded command sequence; taking the new arm on replay is a non-determinism panic on the project's ONE pump. | A parked pump wedges at the deploy and the project stops. | `Test_Pump_PreDispatchGate_DefaultVersion_KeepsOldDispatch` (and `Test_Pump_DrainGate_DefaultVersion_ContinuesAsNew`) |
| **G-P19** | `:284-293` | **RE-ASSERTED**, NOT discharged · `:1281-1290` (three arms) | `changePumpHonorsRecordedPause`, **three arms** (Default ⇒ no gate; v1 ⇒ only a non-operator-driven pump; v2 ⇒ every pump) | The semantics changed twice and local histories recorded each. | Same wedge as G-P18, plus a resumed project that will not pump. | `Test_Pump_RecordedPauseGate_DefaultVersion_StillDispatches`, `Test_Pump_OperatorDriven_RecordedPause_StillDispatches` (v1), `Test_Pump_V2_RecordedPauseBindsAnOperatorDrivenPump` (v2) |
| **G-P20** | `:301-310` | **RE-ASSERTED**, NOT discharged · `:1299` | `"pump-pause-decode-any"` — Default keeps the old struct decode | The old decode silently drops a relayed binary pause; replaying a pre-change history through the new decode takes the quiet branch where the history recorded a dispatch. | Non-determinism on the one pump. | `Test_Pump_DecodeGate_DefaultVersion_KeepsOldStructDecode` |
| **G-P21** | `:362-370` | **RE-ASSERTED**, NOT discharged · `:1359-1367` | the eligibility ladder: **two fences, three arms** (`ledger-partial-resume` ⇒ `eligibleNotStarted`; `design-activities-dispatchable` ⇒ `eligibleDispatchable`; else `eligibleWithDesign`), and the rules are CUMULATIVE | A recorded history that walked past `requirements` and dispatched a construction activity would, replayed under the newer rule, select a DIFFERENT child id. | Non-determinism on replay; or, with a rung dropped, the three design activities silently stop being dispatchable. | `Test_Pump_EligibilityRuleLadder_EachFenceArmSelectsItsRule` (all three arms) + `Test_Pump_LedgerPartialResume_DefaultVersion_KeepsTheOldSelection` (the selection consequence) |
| **G-P22** | `:398-403` | **RE-ASSERTED** · `:1404-1407` | `nextEligible`: a nil `NextEligibleActivity` helper ⇒ `verdictQuiescent` | An unwired pump dispatches NOTHING rather than panicking or dispatching arbitrarily — fail-safe by construction. | A wiring regression becomes a nil-deref inside the project's one pump. | `Test_Pump_NoEligibleActivity_QuietTick` |

---

## `replansweep.go` — DISCHARGED, 0 guards (Task 11 DELETED this workflow)

The two rows this section held, **G-R1** (`in.ProjectID == nil` ⇒ empty result) and **G-R2**
(`isReadNotFound` ⇒ empty, not an error), existed to be shown to protect nothing that
survives. Task 11 confirmed it rather than assuming it: both pinning tests
(`Test_ReplanSweep_NoProjectNamed_IsAQuietEmptySweep`,
`Test_ReplanSweep_ProjectNotFound_IsAQuietEmptySweep`) ran GREEN, and so did the deletion's
own argument, `Test_ReplanSweep_SurfacesNothingForAnyProject` — which seeded the loudest
variance the head-state can hold (an activity terminally failed with `VarianceExhausted`
behind four rejected gate attempts) and got **0** flagged variances back. `flagVariances`
returned `nil` unconditionally, so the `delivery:replanSweep` Schedule fired every five
minutes to produce an empty result: worse than no sweep, because an operator reading
`temporal schedule list` sees variance coverage that does not exist.

The workflow, its Schedule registration, its id helper, its frozen name, its golden entry
and all four of those tests are gone. **The rows go with them** — a census row whose subject
no longer exists is not a guard, and the two meta-tests are what make the doc and the code
one edit instead of two.

---

## `pumpsweep.go` — 7 guards (Task 4 deleted the phase filter, G-S2, and added G-S7)

| ID | Line | Verdict (Task 16) | Guard | What it protects | BreaksAs | PinnedBy |
|---|---|---|---|---|---|---|
| **G-S1** | `:94-96` | **RE-ASSERTED** · `:104-106` | `s.OperatorPaused` ⇒ skip the project | The sweep must not silently override an operator pause every 30 s. **Task 4 deleted the line ABOVE it (G-S2) and did not touch this one** — which makes the pause the ONLY thing that takes a project out of the fan-out. | PauseProject stops the cascade for at most 30 seconds. | `Test_PumpSweep_ExcludesPausedProject_IncludesUnpaused` + `Test_PumpSweep_StillSkipsAPausedProject` |
| **G-S2** | `:83-99` | **RE-ASSERTED** (the absence, with its reason at the site) · `:84-99` | **NO phase filter.** Task 4 DELETED `s.Phase != PhaseConstruction` and put nothing in its place | The filter had been wrong for the three design activities since 4b1: it mirrored the blanket gate `nextEligibleActivity` replaced with `admissibleInPhase`, so a Phase-1/2 project was swept never and only a manual `Begin` started its design walk. Nothing replaces it because the per-project pump is already a quiet no-op (`verdictQuiescent` returns with no write and no continue-as-new), so the filter only ever saved a child start — and re-deriving the admission rule here would be a second copy of the rule that just drifted. | The filter back, in any form: a project at phase 1 or 2 self-starts never. | `Test_PumpSweep_SweepsAProjectInDesignPhases` |
| **G-S3** | `:94` | **RE-ASSERTED** · `:104` | `s.OperatorPaused != nil` — a nil pointer is NOT paused | A summary that omits the flag must not be read as paused; that would silently stop every project on an older envelope. | The whole platform stops sweeping after an envelope change. | `Test_PumpSweep_NilOperatorPaused_TreatedAsNotPaused` |
| **G-S4** | `:106-116` | **RE-ASSERTED** · `:125-136` | wait for the **start ack only** (`GetChildWorkflowExecution().Get`), and swallow `WorkflowExecutionAlreadyStarted` as the DESIRED outcome | The sweep must stay short so the 30 s cadence is not blocked by a long cascade; and a still-cascading project must be left alone, not raced. | Awaiting completion blocks the whole platform fan-out behind one project's drain; treating AlreadyStarted as an error fails every tick on every healthy cascading project. | `Test_PumpSweep_DuplicateProjectIDInOneTick_SecondCollapsesOntoFirst` |
| **G-S5** | `:77-80` | **RE-ASSERTED** · `:77-80` | `ProjectStateListProjects`'s error arm — the whole tick FAILS | No partial fan-out: a truncated enumeration must not read as "these are all the projects". | A catalog fault silently pumps a subset of the platform and the rest look drained. | `Test_PumpSweep_AFailedListProjects_FailsTheWholeTick` |
| **G-S6** | `:65` | **RE-ASSERTED** · `:65` | `pumpSweepOwnerScope` is a **non-empty** constant | `projectStateAccess.ListProjects` answers `fwra.ContractMisuse` on an empty owner; both real catalog implementations then discard the value entirely. | Every sweep tick fails platform-wide, with a Schedule log as the only symptom. | `Test_PumpSweep_TheOwnerScopeIsNeverEmpty` |
| **G-S7** | `:100` | **RE-ASSERTED** · `:119` | `ParentClosePolicy: PARENT_CLOSE_POLICY_ABANDON` on the child pump start — **the row this census missed** | The pump is its own durable execution, outliving the millisecond-long tick that started it. The default policy is TERMINATE. | Every pump the 30 s Schedule starts is killed when its tick closes: nothing on the platform ever self-starts, and the only symptom is pumps that vanish. Removing the line left the whole delivery package GREEN. | `Test_PumpSweep_TheChildPumpIsAbandonedSoTheTickNeverKillsIt` |

---

## `projectsupervision.go` — 7 guards (the relay is the seam Task 12 generalises)

This file is in the census because `relayPauseToPump` is **the one existing example of an
out-of-band signal reaching the pump** — it is the shape a react-by-signal pump copies, and
G-V5 is precisely the "guaranteed delivery" hole that shape inherits.

| ID | Line | Verdict (Task 16) | Guard | What it protects | BreaksAs | PinnedBy |
|---|---|---|---|---|---|---|
| **G-V1** | `:40-44` | **RE-ASSERTED**, line unmoved · `:40-44` | `SetQueryHandler(querySessionState)` registered BEFORE the blocking `pauseCh.Receive`; its `err` arm returns | The project-level session Query must be answerable for the whole life of a long-lived workflow that spends it parked. | A project-scope `GetSessionState` fails for every unpaused project. | `Test_Supervision_SessionStateIsQueryableWhileItWaitsForThePause` |
| **G-V2** | `:74` | **RE-ASSERTED**, line unmoved · `:74` | `GetVersion("pause-relays-to-pump")` — Default keeps main's cancel→record with NO relay | A supervision run already inside this branch at deploy replays its recorded sequence. | Non-determinism on the project's supervision workflow. | `Test_Pause_RelayGate_DefaultVersion_CancelThenRecord_NoRelay` |
| **G-V3** | `:84-92` | **RE-ASSERTED**, line unmoved · `:84-92` | **RECORD → RELAY → CANCEL**, in that order | RECORD FIRST makes the pause durable before anything else, so a pump the 30 s sweep restarts *inside the relay window* reads it at G-P4 and goes quiet. | A pump started in the relay window dispatches through an operator halt. | `Test_Pause_RecordsBeforeRelayingToPump` |
| **G-V4** | `:84-92` | **RE-ASSERTED**, line unmoved · `:84-92` | each step's `err` arm ABORTS the rest — and the pause **STAYS recorded** | A failure after the record must not un-record the pause; the sweep keeps honouring it. | A half-applied pause that the next sweep tick overrides. | `Test_Pause_RelayFailsAfterRecord_PausedStaysRecorded_WorkflowFails` |
| **G-V5** | `:152-155` | **RE-ASSERTED**, line unmoved · `:152-155` | only `isSignalTargetNotFound` is tolerated; **every other delivery failure propagates** | No pump running is the normal case for a project paused between cascades. **This is also the react-by-signal hole:** a completion signal to a dead pump is silently dropped, so Task 12 needs signal-with-start or the 30 s sweep as its backstop. | Tolerate too much and a pause is lost with no trace; tolerate too little and every between-cascades pause fails. | `Test_Pause_NoRunningPump_NotFoundTolerated` |
| **G-V6** | `:125-127` | **RE-ASSERTED**, line unmoved · `:125-127` | `!plan.RecordPaused` ⇒ **no** head-state write | The engine's DECIDE step owns whether the pause is recorded; the Manager EXECUTES the plan and must not record on its own initiative. | A policy that says "do not record" records anyway, and a project is paused in head-state that the engine never paused. | `Test_Pause_APlanThatDoesNotRecord_WritesNoPause` |
| **G-V7** | `:66` | **RE-ASSERTED**, line unmoved · `:66` | `Policy: wf.InterventionPolicy` is threaded into `ApplyPausePolicy` | The retired adapter omitted it, which made the real engine reject EVERY pause with "unknown policy mode". | Every pause fails at the engine. | `Test_ApplyPausePolicy_ZeroValuePolicy_IsTheOldBug` |

---

## Task 12 landed: what the lease pump did with each of the four affordances

**The controller's ruling, and it overrode the brief.** `ExecuteChildWorkflow` STAYS; only
the blocking `.Get` is gone, and the pump selects over the N child futures alongside the
lease/finish channels and the reconcile timer. That keeps all four affordances below for
children started in the CURRENT run, with no new ResourceAccess producer and no contract
change — the lease rides the existing `messageBus.deliverSignal`.

| affordance | where it lives now |
|---|---|
| **Ordering** (G-P13) | REPLACED, not preserved. Parallel dispatch is the point, so the pump re-reads and re-derives on every wake-up, `pumpState.Started` makes one chain dispatch one activity at most once, and LEASE EXCLUSIVITY is what serialises main-writing. |
| **Error propagation** (G-P12) | `workflow.Selector.AddFuture` over the child futures, whose `Get` carries the error — for children of this run. For a child that predates a ContinueAsNew the reconcile APPLIES stop-the-cascade off the row: a terminal FAILURE with no finish signal is `failWalk`, because a clean give-up signals and `failWalk` does not. |
| **Liveness** (`PumpStatus.open`) | The pump PARKS on the selector and continues as new only on `pumpHistoryBudget`, so `Open` keeps meaning "the cascade is alive"; the Query is registered first (G-P1) and answered before the park (G-P8). |
| **Guaranteed delivery** (G-V5, G-P9) | The 30 s reconcile is the backstop for every lost signal, and G-P9 is generalised from *pause* to all three channels by `pumpContinueAsNew`'s drain-and-carry. |

## The four `child.Get` affordances, and what the lease pump owes each

`pumpnextactivity.go:230` is one line and four guards. The architect flagged the
ContinueAsNew drain-and-carry as the wave's riskiest item because **it fails silently, only
under concurrency, and with one pump per project the blast radius is "the project stops".**

| affordance | census row | what a signal does NOT give | what Task 12 owes |
|---|---|---|---|
| **Ordering** | G-P13 | A signal arrives asynchronously; the pump must re-read and re-derive, and **two children finishing in one workflow task must not collapse into one wake-up**. | A re-read + re-derive on every wake-up, and a wake-up count that survives coalescing (a counter/sequence, not a bare `Await` on a bool). |
| **Error propagation** | G-P12 | A signal carries no error channel. **`deliveryActivity` has no failure-signal producer today.** | Either a failure signal with a producer, or keep `ExecuteChildWorkflow` and select over N futures (the recon's third option: smallest honest change, preserves the error channel, and is NOT what the spec says — worth a ruling). |
| **Liveness** (`PumpStatus.open`) | G-P1, G-P8 | With no block the run ENDS after each dispatch, so `PumpStatus.open` stops meaning "the cascade is alive" — which `awaitDispatchDecision` (`deliverymanager.go:6006`) and the SPA both assume. | The pump must PARK (`workflow.Await` / a selector) and continue-as-new on a history budget, so `Open` keeps its meaning; and the Query must stay served across the park (G-P1) and answer before the drain (G-P8). |
| **Guaranteed delivery** | G-V5, G-P9 | `messageBus.deliverSignal` to a pump that is not running answers RA `NotFound`. **A completion signal to a dead pump is silently dropped.** | Signal-with-start (the supervision pattern) or the 30 s sweep as the backstop — plus G-P9 generalised from *pause* to **every channel**: drain EVERY buffered signal before ContinueAsNew, because ContinueAsNew discards them all, not just pauses. |

**The drain-and-carry rule, stated once:** at the ContinueAsNew boundary the new pump must
(a) drain every channel it reads, (b) carry forward what it drained, and (c) keep the carry
bounded — the started set is bounded by the plan's activity count. (a) without (b) loses
completions; (b) without (c) is unbounded history; (c) without (a) is the 4b1 defect class
in a new body. G-P9, G-P10 and G-P13 are the three rows that say so.

## Carries

- **Task 11** — DONE. G-R1/G-R2 were discharged, not deleted blind: both pins ran green and
  so did the deletion's own argument. Their tests died with `ReplanSweepWorkflow`; the
  golden moved 134 → 133 as predicted, and the frozen list stayed at 15 (−
  `constructionReplanSweep`, + `constructionPumpSweep`, the 4b1 earmark closed).
- **Task 12** — the nine `pumpnextactivity.go` rows in the unarmed list are the ones with no
  prior history of being checked. G-P12 and G-P13 have no obvious replacement in a
  signal-driven pump; G-P9's generalisation and G-P10's bound are the drain-and-carry.
  **Five `GetVersion` fences over four change ids (G-P18 ×2, G-P19, G-P20, G-P21 ×2) must
  each be either re-asserted or explicitly discharged by the drain** — a marker whose other
  arm names something the build no longer has compiles, records a version, and then panics
  differently (the `changeGenericActivityChild` lesson, `pumpnextactivity.go:374-380`).
- **Task 14** — the pump replay fixtures must cover one run per fence ARM, not one per
  workflow: G-P18's two ids, G-P19's three arms, G-P20 and G-P21's ladder. A pump run that
  quiesces immediately records ~10 events and fails `deliveryReplayMinEvents = 20`, so a
  pump fixture must dispatch.
- **Task 16** — walk this table. A row whose PinnedBy test was deleted, or whose guard has no
  counterpart in the new body, is a finding. `Test_PumpGuardCensus_EveryGuardIsPinned` and
  `Test_PumpGuardCensus_TheDocAndTheCodeAgree` keep the two halves honest between now and
  then.

## Where the executable half lives — a correction to the plan

The 4b2 plan's Task 1 says to create
`server/internal/manager/delivery/pumpguards_test.go`. **That file cannot exist.**
`arch.CheckFileLayout`'s `testFileNameViolations` rule (framework-go `arch/filelayout.go:192`)
flags *every* `_test.go` in a component package whose name is not the package's one allowed
test file — which for `internal/manager/delivery` is `manager_test.go`, as that file's own
header explains. A second test file is a red `TestFileLayout` in `internal/arch_test.go`,
and no gate is weakened to accommodate a doc.

So `pumpGuardCensus()`, `testFuncNamesInPackage`, the two meta-tests and the fifteen new
guard tests all live at the end of `manager_test.go`, under the banner
`THE PUMP GUARD CENSUS`. Everything else in the brief is unchanged — the type, the function
name and the meta-test's contract are exactly as specified.

---

# TASK 16 — THE VERDICTS. The census is closed.

**Walked 2026-09-29 against `37b0e768`, row by row, against the SHIPPED code and not
against a re-derivation.** Every row now carries its verdict in its own `Verdict` column,
which is where a reader meets it; this section is the arithmetic, the four things the walk
found, and the one verdict the brief's three words could not say honestly.

## The tally

**This table is CHECKED, not typed.** `Test_PumpGuardCensus_TheHeadlineCountsAreTrue` reads
the `Verdict` cell of all 36 rows, requires each to OPEN with one of exactly these four
words, and counts them against these numbers — so a row cannot carry an invented verdict, an
empty verdict, or a **LOST** that this tally still reports as zero.

| verdict | rows |
|---|---:|
| **RE-ASSERTED** | 35 |
| **RE-ASSERTED IN REDUCED FORM** | 1 |
| **DELETED WITH ITS SUBJECT** | 0 |
| **LOST** | 0 |
| Verdict rows total | 36 |

The one **RE-ASSERTED IN REDUCED FORM** is G-P13, and the closing carry says why.

**`G-R1` and `G-R2` are not in that total and are not rows.** Task 11 discharged them with
`ReplanSweepWorkflow` — both pins ran green first, and so did the deletion's own argument —
and the two meta-tests are what made removing the code and removing the rows one edit. The
brief predicted them as *DELETED WITH ITS SUBJECT*; they were already gone when Task 16
opened, which is the outcome that prediction wanted.

**Two rows were LOST inside this wave and are re-asserted because they were FOUND inside
it, not because they never moved.** Both are marked as such in their Verdict cell:

- **G-P12** — the pump commit's own lease release defeated it. `finalizeWalk`'s deferred
  release rides the `activityFinished` signal and fires on **every** exit including the
  failing ones, so a broken merge tail reported a finish FIRST, `markFinished` deleted the
  child's future, and the run returned **QUIESCENT** while the child failed. That is G-P12's
  stated `BreaksAs`, reached through the one message that was supposed to be its friend.
  Fixed at `aa977ae9`: a finish never discharges a future this run holds (`applyFinish`).
  **And that fix left two back doors, closed in fix round 2 (C1).** The rule it established —
  only a fact the pump settled for itself may call `markFinished` — was honoured to the LETTER
  by the lease's two NotFound arms, because a NotFound *is* such a fact. The consequence was
  the one the rule exists to prevent: a holder that took the lease in its merge tail and then
  `failWalk`ed sends no finish, so the lease stays held, the probe finds the execution gone at
  `pumpLeaseDeadline`, `markFinished` discharged the future, and `pumpReconcile` never called
  `f.Get`. Both arms now `markVanished` — release the admission, keep the verdict for the next
  reconcile — and the reconcile stops the cascade on a vanished child that recorded no
  terminal, which is also what keeps the pump from waiting on a future that will never
  resolve. Measured: deleting that arm hangs the pump for the length of the test timeout.
- **G-P10** — the declared bound was FALSE. `Finished` and `Carried` are "bounded by the
  plan" only if the ids in them are ids the pump STARTED, and neither the finish arm nor the
  pre-ContinueAsNew drain checked. Measured: **50 fabricated ids accepted and carried**, at
  both doors. Fixed in the same commit.

Neither was found by the fixtures and neither was found by the suite. **Both were found by
a reviewer reading a guard against the body that replaced it** — the same method that found
the eight 4b1 guards, on a wave that had this census in front of it the whole time. The
census made the finding cheap; it did not make it automatic, and it is worth saying so.

## THE ONE THING TASK 16 REFUSED TO DO: no fence was discharged

Task 12's carry and Task 14's carry both offer the five `GetVersion` fences
(**G-P18** ×2, **G-P19**, **G-P20**, **G-P21** ×2) for retirement, on the argument that the
required drain kills every `{p}:nextActivity` execution so no recorded history will ever
replay against this body. **All five are kept.** Two reasons, and the second is the one
that matters:

1. **A REPLAY FIXTURE DOES NOT PIN A `GetVersion` FENCE — measured, Task 14 §2(b).**
   Deleting the `changeDesignActivitiesDispatchable` rung left **all six** new pump fixtures
   GREEN: the SDK tolerates a recorded `Version` marker the replayed code never asks for. A
   real command change *does* bite — deleting the pace `Sleep` gives
   `[TMPRL1100] a matching Timer command was expected in history event position 34` on 2 of
   6 — so **fixtures pin COMMANDS, not version rungs.** "Replay is 14/14 green" is therefore
   not evidence about a fence and is not licence to retire one. The census's
   `…_DefaultVersion_…` tests are the only pin those four rows have.
2. **A discharge is a separate edit with its own drain evidence.** Retiring a fence deletes
   a census row's arm AND its DefaultVersion test in the same stroke; doing it inside the
   task whose job is to *verify* the census would mean this document certifying its own
   deletions. The drain that justifies it has not run (the wave's rule is one drain covering
   3 + 4a + 4b, before any deploy). Until that drain is evidence rather than a plan, a fence
   costs one `GetVersion` call and buys the project's ONE pump not wedging at a deploy.

## What NO fixture covers — stated here, not left in a task report

Task 14 recorded these in code at `pumpReplayCases`. They belong on the census because they
are the shape of what a green replay run does *not* mean:

- **`pump-drain-pause-before-continue-as-new` v1 is unreachable by any capture.** Its
  `GetVersion` fires only once `pumpShouldContinueAsNew` is already true — past
  `pumpHistoryBudget` (4000 events) — so **the entire ContinueAsNew boundary is unfixtured**:
  the drain, the carry and the replay of `Carried`, which the pump's own header calls "the
  wave's riskiest ten lines". Held by `Test_Pump_DrainGate_DefaultVersion_ContinuesAsNew`,
  `Test_Pump_DrainPause_StopsTheCascadeInsteadOfContinuing`,
  `Test_Pump_ContinueAsNewPayloadIsBoundedByThePlan` and the `continue-as-new-loses-no-signal`
  shape case — and by nothing that replays a real history.
- **Every fence's `DefaultVersion` arm.** `GetVersion` returns `maxSupported` on a new
  execution, so **no capture can ever produce a DefaultVersion history.** Those arms exist
  for executions already in flight; their DefaultVersion tests are their only pin. This is
  the mechanical reason (1) above is not a preference.
- **The transition itself** (old pump → new pump). The required drain is what makes that
  acceptable.
- **G-P12's pre-ContinueAsNew row arm** (a terminal failure row with no finish report): it
  needs a child that predates a CAN, which needs a CAN — see the first bullet.
- **A child failure stopping the cascade** (G-P12's future arm). No fixture fails a child.

## The headline numbers were unchecked, and now are not

Task 4's review found the last hole in the meta-tests: the two of them check the row SET and
the `PinnedBy` column, and **neither reads a NUMBER**. `| Guard rows total | **38** |` and
`## \`pumpnextactivity.go\` — 22 guards` both stayed green while wrong — and the total *was*
wrong for two tasks. That is the one figure a reader uses to notice a missing row.

`Test_PumpGuardCensus_TheHeadlineCountsAreTrue` now asserts, all against
`len(pumpGuardCensus())` and its own per-file split: the total, each per-file headline row,
each `## <file> — N guards` heading, `Rows with no pin`, and THE UNARMED LIST's own count
against the number of three-cell rows it actually holds. Measured RED on every arm, and —
the case that matters — **a row deleted from the doc AND the code together, which both other
meta-tests accept in silence, now fails three ways.**

## The sweeps beyond the pump, per the brief's steps 2 and 3

- **Deleted refusals in `deliverymanager.go` (the two `askX`/`ackX` twin pairs, the two
  refusals, `resolveQuestionBranch`, `OverrideActivity`'s stage precheck).** Every deleted
  refusal has a live successor: `artifactKindIsPhase1` / `artifactKindIsPhase2` at `:735-747`,
  `refuseArtifactAckDuringLiveSession` at `:767` (ONE copy for both halves), the addressee,
  empty-projectId, non-empty-note and no-questions `ContractMisuse` refusals all present.
  **`pdCheckNoReplyTo` survives kind-scoped and is called at `:736`** — the risk this plan
  named — and its operator-note twin `checkNoReplyTo` is restored at `:7197`.
  `OverrideActivity`'s stage precheck is honestly REPLACED by a ledger-keyed one, with the
  window the replacement no longer covers written out at the site (`:6058-6066`).
- **`deliveryactivity.go` (Task 12's `finalizeWalk`).** One deleted refusal:
  `reconcileDivergedBranch`'s `if !rec.ok` early return, which existed because the reconcile
  verb preserved exactly ONE slot. It is **DELETED WITH ITS SUBJECT** — the verb now takes a
  SET of kinds — and the replacement refuses nothing because there is nothing left to refuse.
  `guardMergePreconditions` and `mergeAndRecord` survive re-signatured.
- **The webApp provider class.** One `CommentProvider` mount removed with no replacement, in
  `SystemDesignView.tsx`: it wrapped the committed-revision preview inside the `generating`
  branch, and **that whole branch is deleted with a stated reason** (no surviving stage or
  field reaches it on the derived door). Deliberate, not lost. The other removal,
  `ActivityExperienceContainer.tsx`, is a re-mount — the same provider moved so the three
  cross-slot providers wrap it, and a test pins the nesting order.

## THE CENSUS'S SIBLING, and the arch gate it is owed

This wave established a rule nothing enforces:

> **A signal delivered through `messageBus.deliverSignal` must never be received into a
> concrete struct.**

`deliverSignal` hands the Temporal client a `[]byte`, the default converter tags it
`binary/plain`, and `ByteSlicePayloadConverter` can assign it to nothing but a `*[]byte`. A
struct target makes the SDK log `Corrupted signal received on channel …` and **drop it**, and
a failed signal assign is invisible to a workflow. All three lease channels did exactly that,
so **the whole stage-4b2 main-write lease was inert in production** until `37b0e768`: no
request reached the pump, no grant reached a child, every merge tail waited out its two-hour
budget and ran unleased. Nothing failed. Only the pause path had it right, and it said so at
the site (`pumpPauseRequested`) without the lesson being inherited three functions away.

**What Task 16 shipped for it:**
`Test_DeliverSignal_TheWireFormProducersAreAClosedList` makes the PRODUCER side a closed
list — four names, AST-read off the `MessageBusDeliverSignal` call sites — so a fifth
channel cannot acquire this wire form in silence, and the failure message is the rule. The
only way a fifth consumer gets the bytes is a fifth producer, and this is the door its author
walks through.

**What it deliberately did NOT ship, and why it is an arch gate and not a package test:**
enforcing the *consumer* needs dataflow from a `GetSignalChannel(ctx, name)` to a
`Receive`/`ReceiveAsync` target across struct fields (`pumpChannels`), closure params
(`AddReceive(ch, func(c …))`) and helper funcs (`pumpReceiveSignal`) — go/types work that
belongs beside `framework-go`'s other gates. It also needs two exceptions stated rather than
waived, and both are real:

- **`pumpPausedAtRunStart`'s `DefaultVersion` arm** (`pumpnextactivity.go:1247`) receives into
  a struct **on purpose** — it is the pre-change body, preserved verbatim behind
  `pump-pause-decode-any` so a recorded history keeps its sequence. A gate that forbade it
  would force retiring the fence, which is exactly what must not happen.
- **`ProjectSupervisionWorkflow`'s `pauseCh.Receive(ctx, &sig)`** (`projectsupervision.go:48`)
  receives into `operatorPauseSignal`, and it is **correct today and latently wrong**: the
  channel's one producer is `client.SignalWithStartWorkflow` with a struct (`json/plain`), so
  nothing is dropped — but the signal NAME is the same `operatorPauseRequested` that
  `relayPauseToPump` delivers as raw bytes to a *different* execution id. The day anything
  relays a pause to `{p}:construction` through the bus, supervision drops it silently. A
  name-keyed gate flags this; an execution-id-keyed gate does not; and which of those is the
  right rule is the first thing the gate's author has to decide. **Carry for 4b3.**

## Carries out of Task 16

- **4b3 — the signal-wire-form arch gate** (above), and the `projectsupervision.go:48`
  latent receive that is the first thing it should be pointed at.
- **The five fences are still live and still owed a deliberate discharge**, AFTER the drain
  that covers 3 + 4a + 4b, as one edit that deletes each fence, its census row's arm and its
  `…_DefaultVersion_…` test together. `pump-drain-pause-before-continue-as-new` is the one
  no fixture can ever hold.
- **G-P13 stays the wave's one non-parity verdict.** Half A (two children write main at once)
  is pinned within ONE pump chain and by the row-level CAS across a chain boundary; half B
  (the same activity dispatched twice) is pinned only by an assertion whose own comment flags
  it as meaning-changing the day a rig hands back two activities. Neither is LOST — both have
  a named successor and the row says exactly what each covers — but neither is parity, and a
  plain **RE-ASSERTED** would have claimed parity. That is why the fourth verdict exists, and
  it exists for one row.
- **`pumpReconcile`'s pre-CAN failure arm still cannot fire for a broken merge tail**, because
  `finalizeWalk`'s tail errors bypass `failWalk` and write no terminal row (earmarked at the
  site, fix round 1). G-P12's degradation is stated; this is the half of it still owed. **Fix
  round 2 (C1) narrowed it rather than closing it:** such a tail is caught WITHOUT a row when
  the pump probed its execution and found it gone — which is the merge-tail case, since a
  merge tail is exactly what holds the lease — and remains invisible when no delivery to it was
  ever attempted.
