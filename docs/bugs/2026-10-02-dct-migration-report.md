# DCT migration: final report (Plan 3)

The "after" report for the deterministic-component-testing (DCT) wave. It diffs against
[`2026-10-02-dct-baseline-findings.md`](2026-10-02-dct-baseline-findings.md) (captured at
`b7fd1d42`) and carries forward the open items in [`2026-10-02-dct-earmarks.md`](2026-10-02-dct-earmarks.md).

- Measured: 2026-10-03, branch `deterministic-component-testing` @ `2b2b3d37`, in
  `/Users/davidmarne/mixofrealitystudio/archistrator-dct`.
- Platform: framework-go v0.12.0, infra-github v0.2.0, scenariohost v0.1.0, app-generator v0.11.0,
  method-assets v0.10.0 **and v0.10.1** (all tagged, pushed and proxy-verified; platform CI green on
  `0421c78d`).
- **Verdict: the branch does not merge.** `TestFileLayout` is at 13, not 0. Of the 22 migration
  units, 7 merged and 15 were rejected or never delivered. The 7 that merged deleted 200 tests
  (6,206 lines) and replaced them with **zero** executing scenarios. Section 2.1 asks for a ruling on
  that first.

---

## 1. Green

| Gate | Result at `2b2b3d37` |
|---|---|
| `GOWORK=off go test ./... -short` (server) | 29 packages ok. 1 FAIL: `server/internal`, `TestFileLayout` only (§3.1) |
| `make method-check` | `TestMethodLayering` PASS. `TestMethodDesignArtifacts` fails only on the 13 layout violations |
| webApp `npm run check` | typecheck, lint and prettier clean. `node --test`: 1249/1249 pass. The 7 fixture-schema reds from Task 9 are gone (Task 8 moved the fixtures) |
| Platform release | 6 tags pushed and served by proxy.golang.org. Platform CI success: runs 37117832744 and 37113355866 |
| Plan 2 tasks 1–10 (incl. 4b) | All accepted, all merged into the branch |
| Design patch `c02a36b8` | 114/114 I/O-explicit nodes added across 17 use cases. method-check findings are line-for-line identical to the pre-patch run |
| Test plan `e5e79a69` | 21 components recorded through the real `aiarch-state-mcp recordPhaseArtifact` path. `aiarch-state-mcp validate`: PASS |
| Dry-run merge into `origin/main` | `git merge-tree origin/main HEAD` is clean (tree `45ffb393`) |

**Diff against the baseline:**

| Measure | Baseline `b7fd1d42` | Final `2b2b3d37` | Δ |
|---|---|---|---|
| `TestFileLayout` scenario-tests-only | 19 | **13** | −6 (autoscaler, estimation, artifact, merchantgateway, operatedruntime, operatedsystemstate) |
| `UC-NO-IO` | 3 | 3 | 0: the same 3 use cases |
| `TP-OP-REACHED` | 150 | **150** | 0: **the identical set of ops** (set diff is empty both ways) |
| `CC-*` | 0 | 0 | 0 |
| `TP-BOUND`/`TP-STATE`/other `TP-*` errors | n/a (no plans) | 0 | the committed plans are internally clean |
| Other findings (APPC-SVC-STRIVE 19, AVOID-12 1, ALIGN-EXTERNAL-UNWIRED 2, OPC-TOPIC-COVERAGE 4, SYS-CARD 2, DEP-PLANNED-SKIPPED 1, DV-REL-UTILITY-EXEMPT 1) | as listed | unchanged | 0 |
| `make gen-tests-check` | clean (0 plans bound) | **red**: 10 stale + 1 missing hooks (§3.2) | regression |
| Generated `*_scenarios.gen_test.go` on the branch | 0 | 7 | +7. Each holds only `TestMain`, so **0 `TestScenario_*`** run |
| webApp `PRE_RULE_TEST_FILES` ratchet | 130 | 130 | 0 |
| `uitests/generated/` | absent | absent | 0 |

The wave added I/O nodes to every use case and bound test plans, but **no operation became
reachable**. Two causes, both in the platform: `scenario.resolveOp` matches only when the
*whole* call label normalizes to an op name, and the slot-5 labels are prose with arguments (for
example `replanProject(projectId?, tickId)`). `scenario.ForComponent` keeps a stimulus only when
`Input.To == component`. So callee components (Engines and ResourceAccess) and the engines that
decide branches (`DecidedBy` lands in `Input.From`) never receive a scenario.

---

## 2. Founder rulings needed

Ordered by what blocks the merge first.

### 2.1 Merged coverage loss (rule before anything else)

These 7 units merged into the branch even though none of them executes a scenario:

| Unit (merge) | Purged | Tests | Scenarios now |
|---|---|---|---|
| `engine/estimation` (`c6304a53`) | `engine_test.go` −2391 | 98 | 0 |
| `resourceaccess/operatedruntime` (`0365a525`) | `access_test.go` −2228 | 59 | 0 |
| `resourceaccess/artifact` (`9617e085`) | `access_test.go` −557 | 11 | 0 |
| `resourceaccess/operatedsystemstate` (`2b2b3d37`) | `access_test.go` −493 | 22 | 0 |
| `engine/autoscaler` (`98cab862`) | `engine_test.go` −484 | 8 | 0 |
| `resourceaccess/merchantgateway` (`cbfc5aa6`) | `access_test.go` −53 | 2 | 0 |
| `resourceaccess/billingstate` (`662ed2b4`) | nothing (it had no in-package test) | 0 | 0 |

The review rejected `engine/billing`, `engine/intervention`, `engine/operationestimation`,
`resourceaccess/usage` and `resourceaccess/agenticjob` because their changes had **this exact
shape** ("coverage regression with nothing replacing it"). The 7 above went through the blocker
path instead, so the same defect was accepted in one place and rejected in another. Some of the
lost guarantees are safety-relevant:

- **operatedruntime**: `Withdraw` refuses the self-managed app, fails closed when it cannot tell
  whether an app is self-managed, and leaves other apps in the same repo alone. `PublishDesiredState`
  refuses a self-managed downgrade. The byte-level production goldens (`testdata/golden/production`,
  now orphaned). A wrong Withdraw is irreversible.
- **estimation**: Table 11.1 parity, `DerivePlan` derivation and transitive reduction, the `MEND`
  project-end milestone that Task 4 just added, override and additive validation, network
  forward/backward pass.
- **autoscaler**: all 18 STP decision rules, including the step and burst bounds and determinism.
- **operatedsystemstate**: optimistic concurrency and dedup-first replay, which `operationsManager`
  relies on.
- **artifact**: content addressing, the test-run index (local and cloud), the expired-artifact
  mapping that the testing gate (Task 7) reads.
- **merchantgateway**: the not-configured variant returns `Auth`, so billing treats it as terminal.

The full per-unit lists are in the blocker records of the wave run. **Ruling R-A:** either revert
the 6 purging merges (keep `billingstate`) until each package projects real scenarios, or accept
the loss explicitly and earmark it. The memory rule "never weaken gates" points to reverting.

### 2.2 The method question underneath: which components get scenarios?

20 of 21 committed test plans have `bindings: []`. Only `operationsManager` has bindings (4).
Spec §4 assumes every handwritten component is tested by projected scenarios. The projection
only reaches **entry-point** components. **Ruling R-B:** pick one of these.

1. Fix the platform. Make `ForComponent` follow call chains (callee stimuli) and `DecidedBy`.
   Make `resolveOp` resolve on the label's leading identifier, or make slot-5 labels bare op
   names. Then re-bind. This needs a framework-go release.
2. Rule that Engines and ResourceAccess are tested **only through** the Managers that call them.
   Then the in-package tests for those layers are deleted for good, and `TestFileLayout` should
   say so.
3. Allow RA/Engine-level scenarios authored from the contracts.

Until this is ruled, every remaining RA/Engine unit will either be rejected or merge with zero
coverage.

### 2.3 Dead operations or missing use cases (`TP-OP-REACHED`, 150)

The set is identical to the baseline table. That table is the authoritative per-contract list.
The orchestrator's dead-op list holds 137 ops: it is those 150 minus 13 ops that drafted
bindings name but whose label `resolveOp` cannot resolve: `security`×4,
`billingManager.CloseBillingCycle/RunShortfallSweep`,
`deliveryManager.ExecuteNextActivity/QueryProjectView/QueryActivityView`,
`projectStateAccess.ReadProject`, and
`webClient.driveDesignPhase/superviseConstruction/operateDeliveredSystem`. After the R-B fix,
rule per contract. These groups need it most:

- **Whole contracts with no use-case reach:** `activityExecutionAccess` (12),
  `constructionTransitionAccess` (12, deprecated: retire it?), `gitActivityStatusAccess` (6),
  `designSessionAccess` (8), `sourceControlAccess` (11; slot-5 views 3, 7 and 16 *do* call it, so
  this is the projection gap and not a dead contract), `usageAccess` (3; views uc4, uc5 and
  var-view-cost-projection call it, the same gap), `security` (4; it has no test plan and no
  migration unit), `revenueLedgerAccess` (3; a permanent no-op impl).
- **No core use case drives the op:** `autoscalerEngine.ProposeDesiredState`,
  `estimationEngine.*` (4), `operationEstimationEngine.*` (2), `reviewEngine.ProposeReviews`,
  `billingEngine.*` (3), `interventionEngine.*` (4; it is `decidedBy` in uc 2, 4 and 16, so this
  is the projection gap), `episodeAccess.*` (3), `agenticJobAccess.*` (3), `artifactAccess.*` (5,
  including Task 5's own `GetTestRunIndex` and `ReadTestRunFile`).
- **Not implemented:** `billingStateAccess` still uses the generated not-implemented stub, so a
  bound scenario would fail.
- **`design-health-engine`** is a slot-5 handwritten component with no service contract, no
  test plan and no migration unit, yet it is one of the 13 `TestFileLayout` violations (37 tests,
  2672 lines). The wave never covered it.
- **`UC-NO-IO` (3):** `onboard-a-new-customer`, `track-weekly-project-progress` and
  `view-operating-cost-projection` need `linkedActorId` set on their existing input nodes. The
  patch format cannot express that.

### 2.4 Unbindable scenarios (15)

| Component | Scenarios | Cause | Ruling |
|---|---|---|---|
| billingManager | bill-the-user-for-usage-P1, P2, P3; retry-a-declined-service-invoice-P1, P2, P4 | `TP-STATE`: no read-only op observes cycle, invoice or settlement state | Add a probe op (proposed `billingStateAccess.ReadCycle`), which is a contract change |
| deliveryManager | replan-under-scope-change-P1..P4 | The stimulus `replanProject` is not on the interface (the replan sweep was deleted in 4b2). `scope-trigger-captured` is unobservable. The `[defer]` guard needs a `decisionDate` that `ReviewDecisionInput` lacks | Redraw the use case, or add the op and field |
| projectStateAccess | execute-a-project-activity-P2, P6, P9, P13 | Facet routing: `recordOperatorNote` belongs to `activityExecutionAccess`, but slot-5 `project-state-access` normalizes onto `projectStateAccess`. P9 and P13 also lack an earned-value probe | Give each facet its own slot-5 component, or route the projection by op to the declaring facet |
| webClient | replan-under-scope-change-P3 | The webApp has no "defer until decision date" control or testid | Add the UI affordance, or drop the guard |

### 2.5 Whole-component rejections at bind time (shape mismatches)

- **`TP-BOUND` is all-or-nothing.** `deliveryManager` (drafted P8..P14, 7 of 11),
  `projectStateAccess` (requeue-P1 and resume-P1, 2 of 6) and `billingManager` (retry-P3, 1 of 7)
  each had valid partial plans that could not be recorded. **Ruling R-C:** allow a partial plan
  with explicit per-scenario `unbindable` reasons, or keep all-or-nothing.
- **webClient (111 errors).** The UI-action binding vocabulary (`action` inputs and
  `{visible:…}` results) cannot be expressed against a client contract whose ops have no params
  and return nothing (void). **Ruling R-D:** change the webClient contract shape, or give
  `TP-ARG-NAME` and `TP-EXPECT` a UI dialect.
- **mcpClient, messageBus:** they have no construction activity, so `recordPhaseArtifact` refuses
  ("needs the ambient activity"). Are generated clients and utilities in DCT scope?
  `messagebus_test.go` (white-box, with an existing `t.Skip` under `-short`) is still in place.

### 2.6 operationsManager (`dct3/internal-manager-operations` @ `8c077917`, rejected)

- **Production defects found by the scenarios. Each needs a fix, not a ruling.** (1) The
  construction-time autoscaler policy has `Kind=Unknown` while the infrastructure is
  `GoTemporalPostgres`, so **every reconcile tick fails with ContractMisuse** for every in-flight
  app. (2) Usage writes carry a zero `CustomerID` and an empty `CycleID`. (3)
  `QueryOperatedSystemView` reads usage with a zero `CustomerID` and returns nil
  Decisions/RecentEvents.
- **Plan defects:** P3, P4 and P5 expect head-state versions 2/1/2 that cannot be reached, and the
  decision-nochange branch cannot be steered from contract inputs. P3 and P5 cannot pass even after
  the code fixes, so the plan needs an amendment.
- **Gate:** the hooks file imports 13 packages outside the `TestFileLayout` hooks allowlist
  (`arch_test.go:427`) and builds real collaborators and Postgres pools. **Ruling R-E:** a Manager
  hooks allowlist (testinfra/scenariohost wiring), or move the arranging into scenariohost.

### 2.7 Platform, design and state defects found along the way

- **The `aiarch-state-mcp` write path loses data** when it re-encodes `project.json` (earmark). One
  construct-mode `recordPhaseArtifact` rewrote the top-level id to `archistrator-dct`, deleted
  `activityListOverrides` and `testingState.systemTestPlan`, dropped `testRuns[0]` fields, zeroed
  `updatedAt` and migrated 5 reviewThread statuses. `e5e79a69` avoided this with a byte-splice, but
  any real construct run will hit it.
- **The design patch `c02a36b8` was applied with a Python one-off.** It did not bump slot-4
  revisions, the project version or `updatedAt`, and it did not run the StaleBasis cascade.
  **Ruling R-F:** is that bookkeeping owed?
- **5 retypes to `sendSignal` were rejected:** `ccMayHaveStep` in framework-go v0.12.0 does not
  allow a step on a `sendSignal` node, contrary to spec §2.1. This needs a framework-go release.
  Even after it, `CC-ACTOR-LANE` blocks publish-a, publish-c and charge-user.
- **7 anchored notes have no edges**, because their anchor is a guarded decision. Open
  design-patch rulings: operate Escalate branch plus operator notification; architect notification
  on the execute "human required" branch; requeue label drift; double-record in
  track-weekly-progress; the orphan note `customer-charged`.
- **`TP-PROBE` checks that a probe op exists, not that it is read-only** (`rules_testplan.go:153`).
- **Safety:** the billingManager retry-P3 hook must target a stub messageBus, never a live
  delinquency route (WITHDRAW is irreversible).

### 2.8 Plan deviations to ratify (carried from Plan 2 and this wave)

| # | Deviation | Source |
|---|---|---|
| D1 | `TestRun.Revision` is the integration attempt id, not a SHA (earmark R1). Until this is ruled, every cloud run reads as stale | Task 7 |
| D2 | A run with no scenarios passes the testing gate (earmark R2). With 20/21 empty plans, this is now the **common** case, not an edge case | Task 7 |
| D3 | The venue records no run (V1–V4), so cloud testing fails closed: every service and frontend activity holds for a human | Task 7 |
| D4 | The project-end milestone id `MEND` was chosen by the implementer (the spec names none) | Task 4 |
| D5 | `ReadTestRunFile` reads `main`; the gate reads the activity branch (P1) | Task 7 |
| D6 | testgen filename `_scenarios.gen_test.go` vs spec `_scenarios_test.gen.go` (P4). The spec text is wrong | Task 10 |
| D7 | Testing section went into README, not a new CLAUDE.md (P5) | Task 10 |
| D8 | 19 layout violations, not 21 | Task 0 |
| D9 | Task 8 moved the preview fixtures to `webApp/preview/fixtures` and rewrote them to the current schema; Software Tester role removed from the Team view | Task 8 |
| D10 | `.activityExecution["N-STP"]` residue: only `construction-state-reset` removes it | baseline |
| D11 | Task 1 kept `TestVariantNone`, plus a decode-only `"plan"` alias (R3) | Task 1 |
| D12 | **24 of 43** non-merge commits on the branch carry `Co-Authored-By: Claude Fable 5.1`, which the workflow prompts asked for. The relayed user request asked for Sonnet/Opus agents instead of Fable. Later commits use Opus 5.5, and the merged ones were not rewritten. **The workflow script's agent model and trailer text are still owed.** No agent in this wave could change them | all tasks |

**Process incidents:** the auto-mode classifier denied the purge commits for `engine/review`
(its worktree holds a staged deletion and 17 untracked foreign generated files; a broad
`git add` would sweep them in), `resourceaccess/episode` and `resourceaccess/sourcecontrol`.
One unit ran `pkill -f operations.test` without scoping it to its own worktree, around 11:19 PDT,
and that may have aborted a sibling's run. The Docker daemon on this host hung on build, pull,
run and testcontainer start, which blocks the Postgres scenario runs and Task 9's `docker build`.

---

## 3. Exactly what is still red

### 3.1 `TestFileLayout`: 13 (was 19)

| Package | File | Tests | Lines | Unit status |
|---|---|---|---|---|
| `engine/billing` | `engine_test.go` | 5 | 300 | rejected (zero coverage) @ `a8171ec0` |
| `engine/designhealth` | `engine_test.go` | 37 | 2672 | **no unit, no contract, no plan** |
| `engine/intervention` | `engine_test.go` | 5 | 429 | rejected (zero coverage) @ `703dc806` |
| `engine/operationestimation` | `engine_test.go` | 3 | 301 | rejected (zero coverage) @ `8b1a561e` |
| `engine/review` | `engine_test.go` | 10 | 359 | commit denied, branch @ base |
| `manager/billing` | `manager_test.go` | 32 | 1156 | never delivered, branch @ base |
| `manager/delivery` | `manager_test.go` | 546 | 32720 | never delivered, branch @ base |
| `manager/operations` | `manager_test.go` | 62 | 2320 | rejected (hooks gate, failing) @ `8c077917` |
| `resourceaccess/agenticjob` | `access_test.go` | 108 | 4661 | rejected (zero coverage) @ `991f0caa` |
| `resourceaccess/episode` | `access_test.go` | 15 | 434 | purge denied @ `0b401643` |
| `resourceaccess/projectstate` | `access_test.go` | 355 | 12711 | rejected (zero coverage, gates deleted) @ `360e9492` |
| `resourceaccess/sourcecontrol` | `access_test.go` | 82 | 2835 | purge held @ `70974265` |
| `resourceaccess/usage` | `access_test.go` | 10 | 436 | rejected (zero coverage) @ `48847a39` |

Total remaining: **1,270 test functions, 61,334 lines**. The projectstate purge would delete
non-waiver ratchets, including `Test_PersistedStateJSONTags_AreLowerCamel`,
`TestEnumWireMap_BidirectionalCompletenessVsContract`, `TestCodecCarriesEveryMember_*`,
`TestProjectIdentityGuard_RefusesCrossProjectWrite` and the Go half of the `operating_fixtures.json`
parity check. Production comments at `projectstateaccess.go:891, 2776, 3357, 3417` and
`manager/delivery/manager_test.go:1990` name those tests. Those gates have to move somewhere
before the file goes.

### 3.2 `make gen-tests-check`: red (new since baseline)

```
stale: internal/engine/billing/engine_scenarios.gen_test.go
stale: internal/engine/intervention/engine_scenarios.gen_test.go
stale: internal/engine/operationestimation/engine_scenarios.gen_test.go
stale: internal/engine/review/engine_scenarios.gen_test.go
stale: internal/manager/operations/manager_scenarios.gen_test.go
stale: internal/resourceaccess/agenticjob/access_scenarios.gen_test.go
stale: internal/resourceaccess/episode/access_scenarios.gen_test.go
stale: internal/resourceaccess/projectstate/access_scenarios.gen_test.go
stale: internal/resourceaccess/sourcecontrol/access_scenarios.gen_test.go
stale: internal/resourceaccess/usage/access_scenarios.gen_test.go
missing hooks file internal/manager/operations/manager_hooks_test.go
```

`e5e79a69` bound plans for components whose generated files live only on the unmerged branches.
Running `make gen-tests` here would also seed an **unfilled** `manager/operations` hooks stub. Its
`TestScenario_*` hang until the 10-minute timeout, and it adds 19 lint findings (nilnil and
revive). Do not run it blind. `server-checks.yml` runs this gate, so CI is red on it.

### 3.3 Scenarios

- On the branch: 7 generated files, **0 `TestScenario_*`**. Nothing fails because nothing runs.
- On `dct3/internal-manager-operations` (rejected): `operate-a-delivered-system-P2` failed under the
  gate when the Postgres testcontainer timed out after 10 minutes. `-P3`, `-P4` and `-P5` never ran
  under the gate. A diagnostic off-gate run says all 4 fail on the production defects in §2.6, but it
  left no `results.json`.
- `record-test-run` refused every merged unit's empty run, so no `TestRun` is recorded for any
  component.

### 3.4 Other red

- `uitests.yml`: `uitests/generated/` does not exist (no webClient plan), so it is red by design (P2).
- The webApp ratchet `PRE_RULE_TEST_FILES` still lists 130 files. Plan 3 was supposed to empty it.
- Not verified on this host because the Docker daemon hangs: non-`-short` Postgres suites
  (`usage` and `operatedsystemstate` timed out at 10 minutes in Task 8) and `docker build webApp/`
  (Task 9).
- **The branch pins `method-assets v0.10.0`.** The release notes require **v0.10.1** (the task-10
  review fixes: one results-tree upload, the Playwright mirror, the `-uitests` spelling).

### 3.5 Rejected or undelivered branches (15)

| Branch | Tip | Why |
|---|---|---|
| `dct3/internal-resourceaccess-projectstate` | `360e9492` | Zero scenarios. Deletes 355 tests, including non-waiver ratchets. Dangling test references |
| `dct3/internal-resourceaccess-agenticjob` | `991f0caa` | Zero scenarios. Deletes 108 tests. Orphaned `testdata/run1statedoc`, `streamjson` |
| `dct3/internal-engine-billing` | `a8171ec0` | Zero scenarios. Money arithmetic left with no test |
| `dct3/internal-manager-billing` | `e5e79a69` | No work done |
| `dct3/internal-manager-delivery` | `e5e79a69` | No work done |
| `dct3/internal-resourceaccess-episode` | `0b401643` | Purge denied, legacy white-box file kept, zero scenarios |
| `dct3/internal-engine-intervention` | `703dc806` | Zero scenarios. The commit message's claim is false: intervention-engine *is* `decidedBy` in 3 use cases (projection gap) |
| `dct3/client-mcpClient` | `e5e79a69` | No plan recordable, no work done |
| `dct3/internal-utility-messagebus` | `e5e79a69` | No plan, no work done |
| `dct3/internal-engine-operationestimation` | `8b1a561e` | Zero scenarios. Deletes 7 behaviours |
| `dct3/internal-manager-operations` | `8c077917` | Hooks import gate (13), failing scenarios, unverified results |
| `dct3/internal-engine-review` | `e5e79a69` | Commit denied. Would zero the design-gate tests (the 2026-07-20 vibes incident) |
| `dct3/internal-resourceaccess-sourcecontrol` | `70974265` | Purge held. Zero scenarios although views 3, 7 and 16 call it |
| `dct3/internal-resourceaccess-usage` | `48847a39` | Zero scenarios. Deletes dedup and append-only tests. Views call it |
| `dct3/client-webClient` | `e5e79a69` | No plan (111 errors), no work done |

---

## 4. Checklist to merge `deterministic-component-testing` into `main`

Do these in order. Do not start a later step until the earlier ones are done.

1. **Coordinate with the other session first.** It owns the main checkout at
   `/Users/davidmarne/mixofrealitystudio/archistrator` (on `main`, with untracked `webApp/proto*`).
   - Local `main` is `168819d7`. It is **1 ahead / 2 behind** `origin/main`: the Plan 2 Task 4b
     doc commit has not been pushed, and the release bot cut server v0.8.117 and webapp v0.6.100.
   - Agree who pushes `main` and freeze it for the merge window.
   - Confirm the stage-4b3 deploy obligations (`docs/bugs/2026-09-29-stage4b3-earmarks.md`:
     schedule deletes, delinquency route kept closed) are either done or sequenced around this
     deploy.
2. **Get rulings R-A…R-F (§2).** R-A (revert or accept the 6 purging merges) and R-B (which
   components get scenarios) decide whether the remaining 13 units can be done at all.
3. **Platform release if R-B(1) or the sendSignal fix is chosen.** framework-go: `ForComponent`
   callee and `DecidedBy` projection, `resolveOp` on the leading identifier, `sendSignal` in
   `ccMayHaveStep`, a read-only check in `TP-PROBE`. Fix the `aiarch-state-mcp` lossy re-encode in
   this repo at the same time.
4. **Bump pins.** `method-assets v0.10.0 → v0.10.1`, plus any framework-go from step 3. Then:
   - Re-seat `.github/workflows/aiarch-construct.yml` from the template. Add the module-dir variable
     (V3) and the record-step commit and push (V1).
   - Run `make gen`. Every `gen-*-check` and `derived-plan-check` must be clean.
5. **Finish the migration.** Re-bind the test plans and apply the design fixes from §2.3–2.6.
   Fix the operationsManager production defects. Then re-run the per-unit pipeline for all 13
   packages in §3.1, plus mcpClient, messageBus and webClient, until:
   - `TestFileLayout` = 0 with zero waivers
   - `make gen-tests-check` is clean
   - `PRE_RULE_TEST_FILES` is empty
   - `uitests/generated/` exists and `uitests.yml` is green
   - every bound scenario passes, and `record-test-run` has recorded a run per component
6. **Integrate main.** Push or rebase the stray `168819d7` (step 1), then merge `origin/main` into
   the branch. A dry run today is conflict-free. Re-run step 7 afterwards.
7. **Full verification on a host where Docker works:**
   - `GOWORK=off go test ./...` (not `-short`)
   - `make method-check`, `make test-scenarios`
   - `golangci-lint`, `gofmt`, `go fix -diff`
   - webApp `npm run check`
   - `docker build webApp/` (check that `/usr/share/nginx/html/trace-viewer/index.html` exists)
   - `aiarch-state-mcp validate --root .`
8. **Merge** `deterministic-component-testing` into `main` as a merge commit. Push, and confirm
   `server-checks`, `webapp-checks`, `go-checks` and `uitests` are green on `main`.
9. **Drain Temporal before the new worker runs:**
   - `DeliveryActivityWorkflow` executions for the removed activity types **N-STP and N-IT**.
   - Executions on the retired lifecycles **`testing:plan` and `testing:systemTest`**.
   - Every walk **parked at a service or frontend `testing` gate**: the gate read changed, and the
     replay fixtures `serial-stp-after-design`, `sendback-judged-pair` and
     `child-lease-granted-and-released` were re-recorded.
   - `*:nextActivity:*` pumps (pump double-start memory).
   - Keep the `"plan"` TestingVariant alias until all of the above have drained. Remove it in a
     later release.
10. **Release and deploy.** Let the release bot cut the server and webapp tags from merged `main`,
    then deploy. Expect **every service and frontend activity to hold at its testing gate** until
    D1–D3 (R1, V1, V2) land. Either pause construction runs or accept the human holds.
11. **After deploy:** clear `.activityExecution["N-STP"]` (D10) by ruling. Drop the `"plan"` alias.
    Earmark the retention and expiry behaviour (spec §10).
