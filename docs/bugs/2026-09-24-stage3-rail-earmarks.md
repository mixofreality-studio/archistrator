# Stage 3 (one staging/review rail) — the drain procedure, the entry criteria, and the carries

Opened 2026-09-24 for stage 3. **The drain procedure below was rewritten on 2026-09-29 as one current sequence** covering stages 3 + 4a + 4b1 + 4b2 together; every number, id and name in it was measured against the branch head `d3a4f898`, not transcribed from the amendments it replaces. The amendment history those four waves produced is kept verbatim in **Appendix A**, below the rule, as history.

Stage 3 shipped: the `activityExecutionAccess` facet (12 verbs, additive), the `.activityExecution` row with per-activity `Version`, the construction workflow writing attempts + review rounds behind the `execution-ledger-writes` fence (2 new replay fixtures, 13 old ones byte-identical), the design rails dual-writing rounds behind `design-round-ledger`, `QueryActivityView` reading persisted rounds, `ActivityView` carrying verdicts/thread/subject/round, and the migration of this repo's state (26 rows, zero derived deltas). Spec §5.3/§8 amended where planning proved them wrong.

---

# THE DRAIN — one procedure, run once, before one release

**Read this section and nothing else to run the drain.** The six steps are executable top to bottom. Nothing below the `APPENDIX` rule is an instruction; it is the amendment history that produced these steps, kept as evidence.

**One release, one drain.** Stages 3, 4a, 4b1, 4b2 **and 4b3** merge together, drain once, release once. None of the five deploys alone: 4a shrank the registered activity-name set by 92 names, 4b1 deleted seven workflow TYPE names, and 4b2 deleted an eighth and a Schedule. A release between any two of them buys a second drain for nothing.

**AMENDED 2026-09-30 (stage 4b3): 4b3 IS the wave the drain and the release ride, and it adds no NAME to the six steps — but it does add one id FAMILY, and that correction is the finding.** Measured at `48fb184a`, every fact in §0 below is **unmoved**: golden **133**, frozen **15**, so 4b3 registers no name and retires none; **no new workflow TYPE, no new workflow id family, no new task queue owner and no new Schedule** (4b3 ADDS none — which is not the same as the namespace holding two; see §0's correction and step 6, which check Schedules by id and not by count); and **no state migration** — `tailFailureDetail` is an `omitempty` addition and every pre-existing row reads as absent. Stage 4b3's re-open sweep is a second STEP inside `RoundSweepWorkflow`'s existing single-project arm, so `{projectId}:roundSweep:{tickId}` was already in step 2's scope.

**CORRECTED 2026-09-30, same day, by the fix round.** The sentence that stood here said the only 4b3 change was in *After the release*. That was wrong, and wrong in the direction that ships: Task 1 changed the `applyDelinquencyPolicy` signal's JSON shape with **no `GetVersion` fence**, and the affected id family — `{customerId}:delinquency`, on the **`operations`** task queue, which the `delivery:` prefix sweep never reaches — was in no step at all. It is now the fifth bullet of step 2's *Id families this image creates*, with the measurement of who can produce one and why draining alone is insufficient. *After the release* item 5 still closes and the second first-run still appears beside the lease; that part of the old sentence holds. Stage 6 lands afterwards as an ordinary post-deploy wave.

## 0. The facts these steps rely on, measured at `d3a4f898` — and re-confirmed unmoved at `48fb184a` (stage 4b3)

| Fact | Value now | Where it lives |
|---|---|---|
| Temporal task queues with a worker | **THREE: `billing`, `delivery`, `operations`** — corrected 2026-09-30, see the note below the table | `main.gen.go:593` (`billing`, unconditional), `:606` (`delivery`), `:622` (`operations`); both registration gates `return true` at `hooks.go:1250`, `:1254` |
| Task queues with **no** worker any more | `system-design`, `project-design`, `construction` | retired in 4a |
| Registered Temporal names (golden) | **133** | `server/internal/registered_names_test.go` |
| Frozen externally-held workflow TYPE names | **15** | same file, `TestRegisteredTemporalNamesGolden_FrozenWorkflowNames` |
| Schedules this image registers | **FOUR fixed ids plus a per-customer family** — corrected 2026-09-30. THIS WAVE'S TWO: `delivery:pumpSweep` (30 s → type `constructionPumpSweep`), `delivery:roundSweep` (300 s → type `deliveryRoundSweep`). NOT THIS WAVE'S, AND EXPECTED: `operations:operatedStateReconcile`, `shortfallSweep` (hourly), and one `closeBillingCycle:{customerId}` per customer | `deliverymanager.go:8630`, `:8638`; `operationsmanager.go:1231`; `billingmanager.go:792`, `:788`. All four fixed ids are registered at startup by their Manager's `RegisterSchedules` — `main.gen.go:600`, `:613`, `:629` |
| Workflow id families this image creates | `{projectId}:nextActivity` (the one pump), `{projectId}:activity:{activityId}` (the one child), `{projectId}:roundSweep:{tickId}`, `{projectId}:construction` (supervision) | `pumpWorkflowID`, `deliveryActivityWorkflowID`, `roundSweepWorkflowID`, `pauseTargetWorkflowID` — `deliverymanager.go:7316`, `:7338`, `:7326`, `:7346` |
| Retired workflow TYPE names, unresumable | **eight** — see step 3 | no worker registers them |
| State migration | stage 3's `.activityExecution` move, owed on any state that predates stage 3 — and it is **one-way** | step 4, which also says why "does the live image predate stage 3" is the one unverified fact here |
| New workflow TYPE names or id families in 4b2 | **none** | 4b2 changed the pump's history shape, not its name |

**CORRECTED 2026-09-30 by the fix round, and this is the one correction in this document that would have done operational damage.** Two rows of this table were false, in the direction that makes a correct deploy look wrong:

- **"exactly one task queue with a worker"** — there are **three**. `billing` registers unconditionally; `delivery` and `operations` are behind composition-root gates that both `return true`. An operator told to expect one queue and shown three has no way to tell a healthy image from a broken one.
- **"exactly two Schedules"** — there are **four registered ids plus a per-customer family**. `operations:operatedStateReconcile`, `shortfallSweep` and every `closeBillingCycle:{customerId}` are registered at startup by their own Managers and are **expected on a correct deploy**. They are simply not this wave's business.

Both rows said "re-confirmed unmoved at `48fb184a`", which is how a number survives being wrong: it was re-confirmed against the previous copy of itself rather than against the code. Every citation in the two corrected rows was re-read at the file and line given. **The consequence for the procedure is in step 5's sibling instruction, not in the deletions:** step 5's three `temporal schedule delete` calls are unchanged and were not disputed — those three ids are genuinely retired — but any instruction that told an operator to COUNT the schedules was counting the wrong set.

`temporal` CLI used for the Schedule steps: 1.7.0 against Server 1.31.0 here. It is also a hard dependency of the replay-capture tooling and is named in no README — see the 4b2 earmarks.

## Step 1 — Pause every project

For every project in the catalog, call `SetProjectRunState` with `runState: "paused"`:

```
POST /api/v1/delivery/set-project-run-state/{projectID}
{"runState": "paused", "reason": "<drain 3+4a+4b1+4b2>"}
```

Both `runState` and `reason` are required. Enumerate the projects with `QueryProjectView{kind: "projects"}` (the landing screen's own read).

A pause is what makes steps 2–5 safe: nothing dispatches, so nothing you drain can be re-created behind you, and the empty-world window in step 4 is harmless.

## Step 2 — Drain every in-flight workflow

Drain, do not terminate, anything this image can still serve. Sweep by id prefix on the `delivery` task queue **and** on the three retired queues, because in-flight executions from an older image are still parked on them.

**Id families this image creates** (drain normally; new histories only after the release):

- `{projectId}:nextActivity` — the project's one pump. After 4b2 it is a long-lived run that parks on a selector, wakes on a 30 s reconcile timer and continues-as-new on a 4000-event budget. Its **id is unchanged** by 4b2; the change is internal to its history.
- `{projectId}:activity:{activityId}` — `deliveryActivityWorkflowID`, the generic DAG child, one per activity, and since 4b1 the only child the pump starts. **Design sessions run through it too** (`startSystemDesign` starts this type at this id), so there is no separate design-session id family any more.
- `{projectId}:roundSweep:{tickId}` — the stranded-round sweep's per-project child, one per firing.
- `{projectId}:construction` — `pauseTargetWorkflowID`, the project supervision execution the pause in step 1 signals (`SignalWithStartWorkflow`, so step 1 may have created it).
- `{customerId}:delinquency` — **ADDED 2026-09-30 (stage 4b3), and it is the one id family in this step that is NOT on the `delivery` task queue: it lives on `operations`, so the `delivery:` prefix sweep above does not reach it.** `delinquencyWorkflowID` (`manager/operations/operationsmanager.go:567`), type `operationsDelinquencyEnforcement`, started by `ApplyDelinquencyPolicy` via `SignalWithStartWorkflow`. It carries a **payload break with no `GetVersion` fence**: stage 4b3 Task 1 flattened the `applyDelinquencyPolicy` signal from `{CustomerID, Context:{PauseNotWithdraw}}` to `{CustomerID, Action}`, so an execution that straddles the boundary decodes the wrong shape — a new worker reading an old payload gets `Action: 0` (`DelinquencyActionUnknown`) and **refuses**, which is the harmless direction; an **old worker reading a new payload** gets `PauseNotWithdraw: false` and **WITHDRAWS**, which removes the runtime of every in-flight app of that customer and is irreversible. Draining is therefore not enough on its own: no `ApplyDelinquencyPolicy` call may be served while both images are live, so hold the route (`POST /api/v1/operations/apply-delinquency-policy/{customerID}` and the `operationsApplyDelinquencyPolicy` MCP tool) closed across the whole rollout, not merely until the queue empties.

  **Measured, so the expected count is known before you look:** the family's two producers are the façade op and billing's shortfall sweep, and **only the façade can have produced one.** `billingStateAccess` carries `perProfile: {}` — an arm-less REQUIRED binding in *every* profile (`.aiarch/state/project.json`, deployment bindings) — so `readDelinquent` returns the generated stub's `fwra.Unknown` on the first attempt of every hourly firing, `ShortfallSweepWorkflow` takes its tolerant-tick branch and returns an empty result, and `deliverDelinquencySignal` is **never reached** (`manager/billing/shortfallsweep.go:45-67`). The façade has **no in-repo caller and no webApp surface** — only the generated REST route and MCP tool, both authz-gated on verb `apply-delinquency-policy` over resource kind `customer` — so an execution exists only if a permitted principal deliberately called it. That is not a fact this repository can settle, so the family is drained rather than declared empty; expect **zero**, and if the sweep finds any, each one is a live instance of the hazard above.

**Id families only an OLDER image creates.** Sweep for these too; each one is dealt with in step 3 if it is still running:

- `{projectId}:nextActivity:{tickId}` — the **pre-`8bde0272`** tick-bearing pump id. Its TYPE (`constructionPumpNextActivity`) is still registered, so this image *can* serve it — which is the hazard, not the comfort: it would be a **second pump over the same dependency frontier**. Terminate any of these rather than letting them resume.
- `{projectId}:{activityId}` — the retired construction child's id (the reason the new child took a distinct `:activity:` segment: reusing the old shape would make Temporal answer AlreadyStarted and the pump read a dispatch that silently did nothing).
- `{projectId}:systemDesign` — the Phase-1 phase workflow.
- `{projectId}:<kindOrdinal>` — the per-artifact-kind co-authors, e.g. `p1:3`. The segment is the numeric `ArtifactKind` ordinal, not a name.
- `{projectId}:sdpReview` — the Phase-2 SDP assembly.
- `{projectId}:phaseAdvance:systemDesign` and `{projectId}:phaseAdvance:projectDesign`, plus the unsplit pre-4a `{projectId}:phaseAdvance`. These last seconds.
- `{projectId}:replanSweep:{tickId}` and `:all:replanSweep:{tickId}` — the deleted replan sweep's executions, plus whatever the `construction:replanSweep` and `delivery:replanSweep` Schedules started under their own Schedule-generated ids. These last seconds.

Grepping one prefix is how a live sweep survives a drain, so sweep the whole `delivery:` prefix as well as per-project ids — the memorised `drain *:nextActivity:*` guidance predates the `delivery` namespace and is not sufficient on its own.

## Step 3 — Terminate by hand what this image cannot resume

**Eight workflow TYPE names have no worker in this image. A workflow parked on one of them cannot be resumed, replayed, fenced or continued — there is no rollback-forward for it.** If step 2 did not empty them, each must be terminated:

```
temporal workflow terminate --workflow-id <id>      # per execution
```

| Retired workflow TYPE | Was started by | Retired in |
|---|---|---|
| `constructionConstructActivity` | the pump, per construction activity | 4b1 (`98e4a906`) |
| `systemDesignCoAuthor` | `RequestArtifactDraft` / `StartSystemDesign`, per Phase-1 artifact kind | 4b1 (`98e4a906`) |
| `systemDesignPhase` | `StartProject{start:true}` | 4b1 (`98e4a906`) |
| `systemDesignPhaseAdvance` | `SubmitReviewDecision{advance}`, Phase-1 | 4b1 (`98e4a906`) |
| `projectDesignCoAuthor` | `RequestArtifactDraft`, per Phase-2 artifact kind | 4b1 (`98e4a906`) |
| `projectDesignSDPReview` | `RequestSDPCommit` | 4b1 (`98e4a906`) |
| `projectDesignPhaseAdvance` | `SubmitReviewDecision{advance}`, Phase-2 | 4b1 (`98e4a906`) |
| `constructionReplanSweep` | the `construction:replanSweep` / `delivery:replanSweep` Schedules, and the retired `RunReplanSweep` façade op | 4b2 (`f3673fb1`) |

Terminating them by hand is the bounded, manual cost **architect Ruling 3(d)** accepted when it chose "archive the fixtures as evidence and cut over" over carrying the retired rails behind a second `GetVersion` roster.

**This step is also what discharges the frozen-names guarantee, and it is why the removals were legal at all.** The frozen list exists so an externally-held workflow TYPE name cannot vanish silently — a client or a Schedule outside this codebase holds the name, so a deletion otherwise leaves it starting a type no worker serves. A completed drain is the only thing that honours it. The arithmetic, all measured from the golden literal in `server/internal/registered_names_test.go`:

| Registered names (golden) | Frozen workflow names | At |
|---|---|---|
| 231 | 20 | `4baed01a` (pre-4a) |
| 139 | — | 4a: 92 removals, every workflow TYPE name unchanged, the duplication of three Managers' RA dep surface gone |
| 141 | 22 | 4b1 after `deliveryRoundSweep` + `deliveryActivity` were added |
| **134** | **15** | 4b1 after the seven removals (`98e4a906`) |
| **133** | **15** | 4b2 (`f3673fb1`: `constructionReplanSweep` left the golden and the frozen list; `constructionPumpSweep` joined the frozen list, having always been in the golden) |

**Drain in both directions.** A worker built from an older commit cannot serve a workflow started on this one, and a worker built from this commit cannot serve a workflow an older one started against a name that no longer exists. The `execution-ledger-writes` and `design-round-ledger` fences make in-flight *replays* safe; they do nothing for a name that is gone.

A completed drain is also what made retiring the pump's own child fence (`changeGenericActivityChild`) safe in the same commit as the fixture archive — a `GetVersion` marker whose `DefaultVersion` arm names a deleted workflow type compiles, records a version, and then panics differently.

## Step 4 — Migrate the state

Run the stage-3 migration on **any other project state that predates stage 3**. This repo's own `.aiarch/state/project.json` is already migrated; re-running is a no-op by design (a row already at a version, already pinned, already holding the round an id names is left exactly as it stands).

```
cd server && GOWORK=off go run ./cmd/migrate-activity-execution -root .. [-dry-run]
```

`make construction-state-reset` is safe only **after** migration — and note that it deletes ALL construction history, not a subset.

**This step is owed only if the live image predates stage 3, and that is the one fact in this procedure nobody could verify from the repo.** Confirm which image is deployed before deciding: if stage 3 has already shipped somewhere, its state is already migrated and this step is a no-op (which is safe — re-running writes nothing). If you are unsure, run it with `-dry-run` first and read what it would write.

**Between this step and the release in step 6 an old reader pod renders an EMPTY WORLD.** The state now carries `.activityExecution` and a pre-stage-3 reader knows only `.activityConstruction`, so every activity reads as `NotStarted` until the new image is live. Harmless while every project is paused, and expected — it is not a migration failure.

## Step 5 — Delete THREE Schedules, by id, before the release

```
temporal schedule delete --schedule-id construction:pumpSweep
temporal schedule delete --schedule-id construction:replanSweep
temporal schedule delete --schedule-id delivery:replanSweep
```

**After this step the namespace holds exactly TWO sweep Schedules, and that is what step 6 confirms.**

Why each one:

- `construction:pumpSweep` and `construction:replanSweep` are **abandoned ids**. 4a renamed the consts into the `delivery:` namespace, so these two keep firing with their action on task queue `construction`, which no worker polls. **Nothing will ever converge them.** A Schedule left pointing at `construction` is a *silent dead sweep, not an error*: every project simply stops advancing, and `temporal schedule describe` is the only thing that says so.
- `delivery:replanSweep` fired `ReplanSweepWorkflow` every 300 s into a `flagVariances` that returned `nil` unconditionally — measured green against a project seeded with an exhausted, terminally-failed variance behind four rejected gate attempts. It read as variance coverage and was not. `RegisterSchedules` no longer registers it, and **an unregistered Schedule is not a deleted one**: it stays in the namespace firing into a workflow type no worker serves until this delete runs.

**A re-register will not fix any of the three.** `messagebus.RegisterSchedule` (`server/internal/utility/messagebus/messagebus.go:167-217`) does Create-then-Update-in-place on `ErrScheduleAlreadyRunning`, which **adopts** a same-id Schedule rather than replacing it. That adoption is exactly why the id had to change, and `temporal schedule delete` is the only thing that retires one.

Schedules are the one part of this cutover that is **not** one-way: a rollback re-registers under whatever ids the old image holds, which is why this step is cheap in both directions. The STATE rollback is one-way — see below.

## Step 6 — Release, confirm, unpause

Release the image, then confirm before unpausing:

```
temporal schedule list        # read it as PRESENT / ABSENT, never as a COUNT — see below
temporal schedule describe --schedule-id delivery:pumpSweep
temporal schedule describe --schedule-id delivery:roundSweep
```

**CORRECTED 2026-09-30. This step used to say "expect exactly TWO" and "THREE would be wrong". Both were false, and on a correct deploy you will see FOUR OR MORE.** A count is the wrong question here: three Managers register Schedules at startup, and only two of the ids are this wave's. Check presence and absence by id:

- **MUST BE PRESENT**, and `temporal schedule describe` must show each one's action on task queue **`delivery`**:
  - `delivery:pumpSweep` — 30 s, type `constructionPumpSweep`
  - `delivery:roundSweep` — 300 s, type `deliveryRoundSweep`
- **MUST BE ABSENT — these are step 5's three deletions, and this listing is how you find out a delete was skipped:**
  - `delivery:replanSweep` — its workflow type is deleted, so a surviving Schedule under that id fires into a type no worker serves.
  - `construction:pumpSweep` and `construction:replanSweep` — abandoned ids firing into a task queue no worker polls.
- **EXPECTED, AND NOT THIS WAVE'S BUSINESS.** Do not delete these and do not read them as a defect:
  - `operations:operatedStateReconcile` — registered by `operations.RegisterSchedules` (`operationsmanager.go:1231`).
  - `shortfallSweep` — hourly, registered by `billing.RegisterSchedules` (`billingmanager.go:792`).
  - `closeBillingCycle:{customerId}` — **one per customer**, so this family alone makes the total unbounded and is the single clearest reason the old "exactly TWO" could never have been right.

Then unpause every project (`SetProjectRunState` with `runState: "running"`), and watch the first cascade — see *After the release* below, which is not optional reading this time.

**A local `CONSTRUCTION_DRYRUN=true` server proves nothing about steps 5 and 6 in either direction.** `dryRunConstructionScheduleGate` (`cmd/server/hooks.go:1216-1222`) skips every `RegisterSchedule` whose `ExecutionKind` sits on the `delivery` task queue, logging `messageBus.RegisterSchedule skipped — CONSTRUCTION_DRYRUN=true` and forwarding nothing, so no Schedule is created at all.

One boot fact worth knowing before a future wave adds a third Schedule: `cmd/server/hooks.go`'s `KindBinding` table (`MessageBusTemporalArgs`, `:1127-1153`) is **hand-maintained** and nothing compiler-links it to the managers' unexported `executionKind*` constants. A kind missing from it makes `RegisterSchedules` fail at startup with `unknown executionKind` and the server never becomes healthy — including under dry-run, because the skip set is *derived from that same table*, so an unlisted kind is forwarded to the real bus and refused there. The covering gates are `TestFinalizeMessageBus_DryRun_SkipsConstructionSchedules` / `..._NotDryRun_RegistersConstructionSchedules` (`cmd/server/hooks_test.go:346`, `:388`) and `Test_RegisterSchedules_RegistersPumpSweepAndRoundSweep`, which asserts both Schedule ids as **literals** as well as through the consts, plus the count.

## Rollback: restore `project.json` FIRST, then roll the image

**The state move is ONE-WAY.** The pre-stage-3 `projectstateaccess.go` has no read tolerance for `.activityExecution` — the tolerance stage 3 added reads the LEGACY member, not the new one — and the stage-3 encoder never emits `activityConstruction` again. So the moment the migration runs (step 4), or the moment the FIRST write lands on the new image, an older image reads every activity as `NotStarted`. Roll the image back on that state and the pump re-dispatches the WHOLE project the instant it is unpaused: fresh attempts, fresh branches, fresh spend, against activities that are already Done.

The order is therefore: **pause → `git revert`/restore `project.json` to its pre-migration commit → roll the image → unpause.** Rolling the image alone is not a rollback.

## After the release — the main-write lease runs for the first time

**This is not a drain step, and it is the most dangerous new thing in the release.** Stage 4b2 replaced the pump's blocking `child.Get` wait with a **main-write lease**: a child asks the pump for admission before its merge tail, and the pump grants at most one at a time. Every lease message rides `messageBus.deliverSignal`, which hands the Temporal client bare `[]byte` tagged **binary/plain** — and until `37b0e768` all three lease channels received into a concrete struct, so the SDK logged `Corrupted signal received on channel activityLeaseRequested` and **dropped every message**. It failed open, so nothing went red.

**Consequence: the 2-hour grant budget, the liveness probe, the lease epoch and `pumpGrantLease`'s requester validation have never executed in production. The first cascade after this release is the first time the admission queue has ever been in a merge's path.** Watch it deliberately, not incidentally.

**What to watch**, in the worker logs:

| Log line | Means |
|---|---|
| `delivery.lease.granted` (`activityId`, `epoch`) — child side | the handshake works. Against a dev server the capture showed granted epoch 1 → released → re-granted epoch 2 in **7.4 s**. |
| `pump: main-write lease granted` — pump side | the pump's half of the same handshake |
| `pump: the lease holder is alive and its lease is renewed, not revoked` | the 10-minute liveness probe (`pumpLeaseDeadline`) finding the holder healthy |
| `pump: the main-write lease holder's execution has closed; revoking and re-granting at the next epoch` | the reap path, first exercise ever |

**What "it went wrong" looks like:**

1. **`delivery.lease.timedOut … waited 2h0m0s` on every merge tail.** Grants are not arriving at all. Look immediately for `Corrupted signal received on channel activityLeaseRequested` / `activityLeaseGranted` / `activityFinished` — that is the wire-form regression returning, and it means the lease is inert again and every merge runs unleased. Also check that `{projectId}:nextActivity` is actually running. Severity: the release loses the admission queue but not correctness — merges still serialise on the per-row CAS and the branch-file version guard, which is the pre-wave state — at a cost of up to two hours of dead wait per activity. That delay, not corruption, is the symptom the founder will notice.
2. **A cascade that stalls with one activity holding the lease for hours.** The 2-hour budget has to clear *other* activities' merge tails, and each of those can hold a **human approval gate**. Expect `pump: the lease holder is alive and its lease is renewed` repeating every 10 minutes against the same `activityId` while sibling children wait. This is designed behaviour at a badly-chosen budget, not a fault; if a busy project serialises behind it, the budget is the knob (`activityLeaseGrantWaitBudget`, `deliveryactivity.go:3411`).
3. **`pump: dropping a main-write lease request for an activity this pump never started`, right after a pump restart.** Expected and documented: `LeaseHolder` lives only in `pumpState`/`pumpInput`, so it survives a ContinueAsNew and **nothing else**. Any pump run that ends while a lease is held exits with a holder, and the 30 s sweep starts a fresh chain with no holder and an empty `Started` set. **The invariant's true scope is per pump CHAIN, not per project**, and "fails open" is the NORMAL state after any pump restart, not a rare fault path. Two activities *can* write main concurrently across a chain boundary; only the row-level CAS and the branch-file version guard stand there. The lease sits on top of the CAS and never replaces it.
4. **A pump run failing with `ActivityChildVanished`.** New at `d3a4f898`, and deliberately loud: a child whose execution is gone and whose row carries neither a failure nor a `CompletedAt` fails the pump run and stops the cascade rather than being counted finished. The false-positive window is narrow by construction (the row is re-read only after the execution is known closed), and the failure is recoverable where the alternative corrupts a build.
5. ~~**A broken merge tail that leaves no red node.**~~ **CLOSED by stage 4b3** (Task 9, `dc0361eb`; Task 10, `4f4655f7`; Task 11, `48fb184a`). A tail that breaks after the binary exit now writes `tailFailureDetail` as a separately-named **fifth** write-once head fact, `CoarsePhaseFor` answers `completedNotLanded`, and the SPA draws a **red node**. The round sweep then **re-opens** such a row on its own 300 s tick, bounded by `healWouldRepeatOneThatChangedNothing` so a deterministically broken tail is healed **once** and then left alone. **What to watch instead:** `delivery.sweep.reopenNotLanded` filing a `NoteRequeue` at the `reopen` gate — once per unit of measurable progress, never on a paused project. A node that **flaps** (red → requeued → red, every 300 s, with the note ledger growing) means the bound is not firing; a node that stays red with exactly one requeue note is the bound working.

**And the second first-run to watch, new at 4b3: the heal bound itself has never executed against production data.** It rests on the claim that a re-run of an already-passed walk records **no new resolved attempt**. If some path does, **the bound is a no-op on exactly its target case and it fails OPEN and SILENT** — there is no red test and no "the bound declined" log line, just the flapping node above. Check it on the first broken tail this release produces.

The measurements behind all of this are in `docs/bugs/2026-09-28-stage4b2-earmarks.md` and `docs/bugs/2026-09-29-stage4b3-earmarks.md`.

---
---

# APPENDIX A — amendment history (history, NOT instructions)

**Nothing in this appendix is a step.** These are the four waves' amendment blocks, kept because they carry the measurements and the reasoning that produced the procedure above. Where an amendment and the procedure disagree, **the procedure is right** — it was re-measured against `d3a4f898`, and each amendment was written at the head of its own wave. Two specific disagreements, resolved in the procedure's favour and named here so nobody re-litigates them from this appendix: the 4b1 block's prediction that 4b2 would **re-key the pump** (it did not — `pumpWorkflowID` is still `{projectId}:nextActivity`), and the 4a block's statement that the Schedule step owes **two** deletes (it owes three, and confirms two).

## Recorded 2026-09-25 (stage 4a)

4a must not deploy alone: 4b changes workflow TYPE names and workflow ids again. Every row measured in the worktree at `b4815ec4`:

| Thing | Before 4a | After 4a | Mechanism |
|---|---|---|---|
| TaskQueue `system-design` | systemdesign worker (`internal/manager/systemdesign/worker.gen.go:13` at `4baed01a`) | **gone** | drain-and-cutover |
| TaskQueue `project-design` | projectdesign worker | **gone** | drain-and-cutover |
| TaskQueue `construction` | construction worker | **gone** | drain-and-cutover |
| TaskQueue `delivery` | — | new: ONE worker, eleven workflow types, every name unchanged (`internal/manager/delivery/worker.gen.go:13`) | — |
| Schedule `construction:pumpSweep` (30 s) | fires into TaskQueue `construction` | **renamed `delivery:pumpSweep`** on TaskQueue `delivery` (`574012f7`); the old id must be DELETED | `RegisterSchedules` creates if absent and ADOPTS a same-id Schedule; `temporal schedule delete` is the only thing that retires an id |
| Schedule `construction:replanSweep` (300 s) | fires into TaskQueue `construction` | **renamed `delivery:replanSweep`** on TaskQueue `delivery` (`574012f7`); same | same |
| `{p}:phaseAdvance` | BOTH design Managers, one string | `{p}:phaseAdvance:systemDesign` / `:projectDesign` | new histories only; drain the in-flight ones (they last seconds) |

**Every workflow TYPE name and every other workflow id is UNCHANGED in 4a** — deliberately, and it is why the fifteen construction replay fixtures still replayed. The drain was required anyway for one reason: **the registered activity-name set SHRANK by 92 names** (231 → 139).

The model↔code drift the rename was owed a ruling on is CLOSED: the label at `project.json:6758` and the two consts both say `delivery:*`. What is NOT closed is that nothing in the gate set compares a Schedule id string to the model — the only guard is that the Schedule-registration test asserts both ids as LITERALS as well as through the consts. Carried in `docs/bugs/2026-09-25-stage4a-earmarks.md`.

*(Recorded at the time and not re-verified in the 2026-09-29 rewrite: that stage 3 grew the registered activity set by 36 names, 12 construction + 24 design.)*

## Recorded 2026-09-27 (stage 4b1)

Unlike 4a, 4b1 changes workflow TYPE names — which is what the six steps were always being kept for.

| Thing | After 4a | After 4b1 | Mechanism |
|---|---|---|---|
| `{p}:activity:{a}` (`deliveryActivityWorkflowID`) | — | the ONE child the pump starts, per activity | new histories only |
| `constructionConstructActivity` | the pump's construction child | **gone** (`98e4a906`) | drain-and-cutover; an in-flight one is **unresumable** and must be terminated by hand |
| `systemDesignCoAuthor`, `systemDesignPhase`, `systemDesignPhaseAdvance` | Phase-1 rail | **gone** (`98e4a906`) | same |
| `projectDesignCoAuthor`, `projectDesignSDPReview`, `projectDesignPhaseAdvance` | Phase-2 rail | **gone** (`98e4a906`) | same |
| `deliveryActivity` | — | new, and **FROZEN from its first commit** (`e5b25b06`) | a continue-as-new re-starts this type BY NAME from inside the running image, so a mid-wave rename breaks the continue of every in-flight walk |
| `deliveryRoundSweep` | — | new, self-fan-out (`31edd214`) | a THIRD Schedule, `delivery:roundSweep` @300 s, **created not renamed**, so it has no old id to delete |
| Registered names (golden) | 139 | **134** (139 → 141 → 134) | +2 workflows, −7 workflows, **0 activity names** |
| Frozen workflow names | 22 | **15** | the drain is what discharges the guarantee for the seven |
| Replay fixtures | 19 live | **8 fresh** in `testdata/replay/` + **19 archived** in `testdata/replay-archive/` | architect Ruling 3(d): archive as evidence, cut over |
| The pump's child fence | `changeGenericActivityChild` | **retired in the same commit as the archive** | a `GetVersion` marker whose `DefaultVersion` arm names a deleted type compiles, records a version and then panics differently |

**The three places the drain is load-bearing.** (1) It discharges the frozen-names guarantee for the seven retired types. (2) It is what made retiring the pump's own child fence safe in the archive commit. (3) There is no rollback-forward for a workflow parked on a name this image does not register.

**One thing 4b1 does NOT add: a state migration.** `.aiarch/state/project.json` was touched only by `ReviewRound.artifactKind` (an APPENDED optional field), `TaskRevisionOutcome` (an APPENDED enum member) and the requeue fold (a `description` only). All three are additive and all three read back on the old image.

*(This block also predicted that 4b2 would re-key the pump and add `PumpResult.activityIds`. Neither happened — see the note at the head of this appendix.)*

## Recorded 2026-09-29 (stage 4b2)

The drain is unchanged in SHAPE and changed in three numbers. 4b2 adds **no** workflow TYPE name, **no** new workflow id family and **no** state migration — `.aiarch/state/project.json` moved only through the one model edit (`ebfc1a42`: revenue share out of the vocabulary, two kinds' draftability retired with their ordinals kept, the folded session `$defs`), which is a slot-data and contract change, not a row shape. What moved:

| Thing | After 4b1 | After 4b2 |
|---|---|---|
| `temporal schedule delete` calls the drain owes | 2 | **3** — `delivery:replanSweep` joins them |
| Sweep Schedules the confirmation step expects | 3 | **2** — `delivery:pumpSweep`, `delivery:roundSweep` |
| Registered names (golden) | 134 | **133** (`ReplanSweepWorkflow` deleted, `f3673fb1`) |
| Frozen workflow names | 15 | **15** — `constructionReplanSweep` left, `constructionPumpSweep` joined (it was already in the golden and only missing from the FROZEN list) |
| The pump's history | ContinueAsNew per dispatch | **one long-lived run** that parks on a selector, wakes on a 30 s reconcile timer and continues-as-new on a 4000-event budget (`b84098eb`) |
| `deliveryManager` ops | 12 | **11** — the replan-sweep route removed |

The op removal has no drain consequence; it is a wire break for any caller of that route.

**And one fact that is not a drain step:** the main-write lease had never run. Every lease message was dropped on the wire until `37b0e768`. That is now written into the procedure above, under *After the release*.

## The operator trap this rewrite exists to remove

The six steps carried roughly 150 lines of amendment blocks, and the corrections lived as paragraphs *beside* the steps they corrected rather than inside them. The result was a genuine trap: **step 5 ordered `delivery:replanSweep` deleted while step 6 told the operator to confirm it existed**, so a literal top-to-bottom reading recreated it. `a883bcb1` fixed that contradiction in five places. The shape that produced it — append a correction, leave the step — is what this rewrite removes, by making the steps the only instructions and this appendix the only history.

---

# Stage-4 ENTRY CRITERIA (blocking)

- ~~Design-rail round numbering is per-SESSION, so a second session of one kind re-mints the first's `RoundID`~~ — **LANDED IN STAGE 3** (`seedRoundBaseFromLedger` on both design rails, behind the existing `design-round-ledger` fence, reading the registered `ReadActivityExecution` verb). A session now numbers its rounds above everything the durable ledger holds for that kind's gate.
- ~~Arm the per-activity CAS: thread the activity `Version` through the 12 verbs' contract.~~ — **LANDED IN STAGE 4a** (Task 3, `d06b6745` + `225966a5`): armed across ten row-writing paths including the four retired-facet verbs, the three workflow fakes honour the expectation, and `NoActivityVersionExpectation` is the named sentinel for a caller that has no number.
- ~~`buildStatus` enum/vocabulary rule before `delivery-manager` is authored as `planned`.~~ — **LANDED IN STAGE 4a** (Task 2, `0ab023ec` + `f8f6fd1f`): `DH-BUILDSTATUS-VOCAB` is an Error in the live tier, and `TestBuildStatusVocabulariesAgree` (`internal/arch_test.go:215`) pins estimation's `knownBuildStatus` equal to designhealth's inline vocabulary through the AST. **And the criterion's own text was wrong:** `delivery-manager` carries no `buildStatus` at all, because `ALIGN-STALE-PLANNED` is an Error for a `planned` component whose package exists, and 4a lands `internal/manager/delivery` in the same commit as the component.
- Delete the three deprecated facets (`gitActivityStatusAccess`, `constructionTransitionAccess` Record\*, `designSessionAccess`) AFTER the drain — **still open after 4b2, and NOT for the reason this bullet assumed.** 4b2's Task 13 went to delete `gitActivityStatusAccess` and found the premise false: all six of its ops have live production callers through the GENERATED Temporal invoker, whose method prefix is the FIELD name in `genActivities` (`GitStatus…`), so a `\.RecordActivityBranchOpened(` grep matched nothing while seven call sites existed. Two of the six are not git mirrors at all — `RecordActivityStarted`/`RecordActivityCompleted` upsert the rows `AllDepsSatisfied` reads, so deleting them stalls the pump. The facet count is UNCHANGED at five and no op was deleted; the enabling move is the `projectCatalogAccess` split, which is in no row of this wave. Details in `docs/bugs/2026-09-28-stage4b2-earmarks.md`. **CORRECTION:** `DH-CONTRACT-DEADOP` does NOT flip back to `assertAbsent` then. Measured: deleting `constructionTransitionAccess` clears the `RecordOperatorNote` duplicate but NOT the `AcknowledgeStaleBasis` one, because `projectStateAccess` publishes that verb too and spec §5.3 keeps `projectStateAccess` at 9 ops. The pin stays until one of those two verbs goes. (Both findings also now read "with DIFFERING param signatures — a name collision, not a proven dead op", which is the honest reading.)
- ~~`applyRecovering` treats terminality `Conflict`s like version conflicts.~~ — **DISCHARGED IN STAGE 4b1** (Task 7, `290ce5ec` + `5d6c4f31`). All three `applyRecovering` loops re-read the ROW as well as the project, behind the `row-conflict-reread` fence, and a Conflict where NEITHER version moved (and the run HELD a row expectation) fails in ONE attempt as `MutateTerminalConflict`. Fix round 1 carried the half that mattered most: a `NotFound` re-read must NOT downgrade a held expectation to `NoActivityVersionExpectation`. The same round added the design-rail seed degradation: `seedRoundBaseFromLedger` returns an error on both rails and a non-`NotFound` read failure fails the session before it drafts anything.
- ~~Stage-4 sweep closes stranded `pending` rounds.~~ — **DISCHARGED IN STAGE 4b1** (Task 6, `31edd214`). `deliveryRoundSweep`, one workflow type with self-fan-out, on the `delivery:roundSweep` Schedule at 300 s. Stranded = a later round exists on the same GATE (`roundGateKey(taskID, artifactKind)`); a pending round that is its gate's latest is left alone, which makes a race with a live child structurally impossible. Closure is `RoundWithdrawn` by `platform-sweep`, never a synthesized decision.
- ~~Design-rail join collision: two kinds' round 1 would bind one gate attempt.~~ — **DISCHARGED IN STAGE 4b1** (Task 3, `a6fb9e0f`). `ReviewRound.artifactKind` / `ReviewRoundInput.artifactKind`, optional, write-once in the store, with FOUR writers threaded including the migration tool. `roundGateKey` (gate identity) and `roundJoinKey` (revision identity) are the two arities of the one rule, and `ledgerRoundBase` now asks the field and falls back to the id prefix only for the one legacy row.
- ~~`engineReviewPolicy` triplication + inline `NewReviewEngine()` collapse.~~ — **PARTLY DISCHARGED IN STAGE 4a** (the three byte-identical copies collapsed because the package merge put them in one namespace and the compiler forced it). **THE REMAINING HALF IS DISCHARGED IN STAGE 4b1** (Task 13, `98e4a906`): one `engineReviewPolicy` converter survives in `deliverymanager.go` and the child takes the injected `wf.Review`; the design slot→activity/phase table's third copy (`designActivityFor`) was deleted by the lifecycle data (`839d98a6`). One correction to this criterion's own text: the `EffectiveGate` / `RequiresHuman` / floor-keyword move into the review Engine **was stage 2's, not 4b's**.
- ~~`RoundWithdrawn` maps to wire `failed` — give it its own wire member.~~ — **DISCHARGED IN STAGE 4b1** (Task 4, `aae23748`). `withdrawn` / `TaskRevisionWithdrawn` APPENDED last to `TaskRevisionOutcome` in both parallel arrays, `roundOutcome` re-pointed, and the single TS consumer fixed. A withdrawn round is deliberate, not a fault, which is why it is not `failed`.

**Everything else on this list stays open and is carried, with its 4a measurements and the order 4b must take it in, in `docs/bugs/2026-09-25-stage4a-earmarks.md`. What 4b1 left for 4b2 is `docs/bugs/2026-09-26-stage4b1-earmarks.md`. What 4b2 left for 4b3 and for the next model wave is `docs/bugs/2026-09-28-stage4b2-earmarks.md`.**

## Deferred to stage 6
- Delete `ArtifactSlot.ReviewThread` (dual-written now), `CritiqueVerdict`/`CritiqueNotes` (→ ordinary `productManager` verdicts), `ArtifactSlot.Revisions` (add the drift test = derived round count first), the `LegacyActivityConstructionRow` read tolerance, the stored derived fields; narrow `PendingOperatorNotes` (the writer stopped in stage 3; the reader still honours stored `NoteSendBack` because narrowing it breaks a pre-fence history).
- `ReconcileBranchFromMain` overlays only the slot table, so a design branch's `.activityExecution` is stale (harmless: the 3-way merge resolves main-side); fix the doc invariant or the overlay.
- Each design gate entry now writes up to five commits to main's `project.json` (openActivity, openReviewRound, critic verdict, human verdict, decide) — repo-growth earmark.

## Carried from the whole-branch review (not fixed in stage 3)
- ~~`OpenActivity` re-opening an existing row with the SAME pin but a different `typ`/`variant` overwrites both silently.~~ — **DISCHARGED IN STAGE 4b1** (Task 5, `38fd7f9c`): a re-open naming a different `typ`/`variant` on a LIVE row is `fwra.ContractMisuse` naming both pairs, and the discriminator is `StartedAt != nil` rather than `Type != 0` — because `ActivityTypeService` IS the zero value, so "has a type" is unaskable. A birth still writes both. (Task 12's requeue fold later rewrote the *other* half of this verb's doc: a requeue DOES re-arm the row, through `RecordOperatorNote{NoteRequeue}`, not through this verb; `179a43b2` + `bdf90047`.)
- ~~The PR rail's rounds 1 and 2 cite the SAME `SubjectRef`.~~ — **DISCHARGED IN STAGE 4b1** (Task 5, `38fd7f9c`): `gateSubjectRef(gf, stagedRef, workAttemptID)` is a ladder — staged ref ⇒ `SubjectCommit`, else a live PR ⇒ `SubjectPullRequest`, else the work attempt ⇒ `SubjectArtifact` — and the design rail runs the SAME rule with its own signature. Its ref is `<branch>@v<version>`: a sha is not reachable from this layer, and the git-as-DB substrate makes one commit per mutation, so the version the staging commit RETURNED names that commit as exactly as this layer can. Ratified as the opaque revision handle 4b3's artifact-as-of-revision read will take. **Earmark that survives:** `.aiarch/state/project.json`'s `ReviewSubjectRef.kind` description still says `commit` is what "a future subject-by-sha writer will use" — stale as of `38fd7f9c`; carried in the 4b1 earmark file.
- Gate-attempt `Actor` semantics CHANGED in stage 3: a gate attempt is now `human`/`system` (who decided) where it used to be `agent` (who was dispatched). The stage-5 UI must label gate rows from that vocabulary, not the work rows'.
- No test covers "a NEW send-back after a completed redraft carries only the new round's comments" — the shape exists, the regression net does not. **STILL OPEN after 4b1**, and the file it named is deleted: 4b1's cases cover ONE send-back → redraft → approve (`sendback-reopens-only-the-judged-pair`, `driveServiceSendBackJudgedPair`), never a second send-back after a completed redraft. Worse, the carry is now weaker — see the 4b1 earmark file's `walkState.feedback` item.
- The design-rail fake `AppendReviewVerdict` (and `ReadActivityExecution`) are more permissive than the store: the fake appends to any round it holds and answers an unopened activity with a zero row where the store raises `NotFound`. A workflow bug the store would refuse can pass the unit tests.
- `applyRoundReviewBatch`'s content-dedup keys on (author, anchor, text), so two genuinely distinct byte-identical comments in one round are mis-paired as a re-issue and the second is dropped. Narrow, but it is a silent drop.
- The migration's acceptance test (`TestMigrate_DerivedPhasesEqualThePreMigrationDerivation`) is SELF-REFERENTIAL and confirmed vacuous on this repo: the base rows stored no `phases`, so the before/after comparison compares two empty derivations. Derive a real pre-migration subset before the tool is trusted on another project's state.
- `designRounds` mints NO round for a critique-only slot (`CritiqueVerdict` set, `ReviewThread` empty): the skip guard admits such a slot but `slotRounds` groups by thread rounds, so the §5.3 projectManager-verdict conversion is not performed for it. Documented on the function; none exist on this repo's state.
- A migrated round's thread renders `answered` where the slot stored `addressed` (codec alias, carried by both copies of the comment). A dual-read window, closed when stage 6 deletes `ArtifactSlot.ReviewThread`.
- Wire-field casing: `GetProject.activityExecution` is camelCase among PascalCase siblings (`ActivityGit`, `ServiceContracts`, `GitRows`). Normalise in stage 6 with the other wire cleanups — it is a wire break either way.

## Owed waiver (slot 3, carried from stage 1, still owed)
- The §2h sentence naming the three Managers' TRANSITIONAL `Project Delivery Workflow` facet group and its stage-4 expiry; a Glossary entry for `Design Conformance Rules`.

## Platform (method-assets) — founder STOP
- `.claude/` skills naming `constructionTransitionAccess` / `gitActivityStatusAccess` / `designSessionAccess` / `systemDesignManager` (`grep -rln` across `.claude/skills .claude/commands .claude/agents`); `the-method-review-routing` drift vs the stage-2 engine; the nine Phase-2 draft kinds have no review task in the `projectDesign` lifecycle (correct per R7 — the rail dies in stage 4).

## Post-merge on main only
- `cd uitests && npm run regen:core-use-cases-fixture` (reads branch `main`).
- Browser run of `uitests/tests/preview/preview-shell.spec.ts` (`construction · owed-gate`, `service-pane`, `built-surface-link`, `unclassified-row`) — the stage-3 fixtures were re-derived through the real read path (zero deltas) but not driven in a browser; the previews render a 29-row world vs the server's 32 (three planned-no-record design rows hand-trimmed).

## Model / code hygiene
- Backfilled rounds carry no `openedAt`/`decidedAt`/`Reviewers` (nothing in the record knows them — not laundered); `fixture-schema.mjs`'s "a round-backed revision always has a startedAt" rule must not be pointed at migrated rows.
- `webApp/src/components/comments/CommentContext.tsx` still mints `$.activityConstruction[id=…]` anchors deliberately (stored comment anchors keep the old string); prose in `wire.ts`/`construction-tracker.spec.ts` still says `activityConstruction`.
- The `fixture-schema` `GATE_TASK` table is a hand mirror of the lifecycle gates — generate it from `lifecycles.gen.ts` in stage 5.
- Three orphan `contract.*.schema.json` files in `projectstate` are produced by no generator; `cmd/backfill-attempts`' two gosec suppressions could take `filepath.Clean` like the migration cmd; `atomicBusinessVerbs` is at 18/20.
- Six `uitests/preview-fixtures/**` snapshots still embed the 19-volatility world; `designhealthengine.go:22` and `deliverymanager.go:160` name a retired volatility.
- The MCP output schema of `activityExecutionReadActivityExecution` is shapeless (`x-go-type` only) after the `$defs.ActivityExecution` deletion; the codec's strict-decode gap (an idempotent value mangle passes the writer guard) belongs in the codec.
- `cmd/server/hooks_test.go:321-323`'s own doc comment still names the Schedules it gates as "pump sweep / replan sweep"; the replan sweep is deleted and the second Schedule is the round sweep. The assertions are correct; only the comment is stale.
