# DCT Plan 4 (coverage per Amendment A): report, rulings, remaining red, merge checklist

Plan 4 Task 7 (`docs/superpowers/plans/2026-10-03-dct-plan-4-coverage.md`). This revision
replaces the 2026-10-03 version (`6af283b0`), which was written before Task 6 ran.

Measured 2026-10-04 on `deterministic-component-testing` @ `4c6af1f9`, worktree
`/Users/davidmarne/mixofrealitystudio/archistrator-dct`. The tree is clean. The pin is
framework-go **v0.14.0**. `dct4/unbindables` is merged (`d1c92c22`).

**Verdict: NOT merge-ready.** Only one of the six merge gates holds (`TP-OP-REACHED` = 0). Task 6
ran once over 23 units. It merged 5 of them, and one of those 5 (billingstate) is red. Another
11 units committed work that review rejected, and 7 units produced nothing.

## 1. Green (measured for this report unless marked)

| Item | Evidence |
|---|---|
| framework-go **v0.13.0** (Amendment A1–A4, `UC-START-REDUNDANT`) | tag on platform `c69fdc99`, pushed, proxy-verified, Platform Checks `37162570333` green (release notes) |
| framework-go **v0.14.0** (facet routing in `scenario.Derive` via `Contract.FacetOf`) | platform `575bb64b` / merge `e8ce5e9a`, pushed, CI green (Task 5 notes). Consumed at `d1c92c22` |
| **`TP-OP-REACHED` = 0** | `make method-check` @ `4c6af1f9` prints no `TP-OP-REACHED` finding. History: 87 → 72 (Task 3) → 17 (A5 round 3) → 0 (v0.14.0 facet routing) |
| Tasks 2–5 landed | re-pin + baseline `f7f2e88a`; dead contracts **deleted** `44809e08` (`constructionTransitionAccess`, `revenueLedgerAccess`); A5 rounds `c45e78d4` `78a7fdb7` `f11942fd` `9ab055ed`; Task 5 `6c927931`…`301504a5` |
| 17 test plans recorded (`testplan(dct4): …`, `41655ba3`..`440db7f0`) | bindings: activityExecutionAccess 20, agenticJobAccess 20, artifactAccess 9, autoscalerEngine 4, billingManager 11, billingStateAccess 11, deliveryManager 37, episodeAccess 8, estimationEngine 18, gitActivityStatusAccess 7, interventionEngine 12, operatedRuntimeAccess 8, operatedSystemStateAccess 14, operationEstimationEngine 12, operationsManager 16, reviewEngine 6, usageAccess 7 |
| 4 units merged and green | `go test -short -run TestScenario_` @ `4c6af1f9`: autoscaler **4/4**, estimation **18/18**, operationestimation **12/12**, review **6/6** |
| `GOWORK=off go build ./...` | green |
| webApp `npm run check` | green (1250/1250 node tests) |
| `git merge-tree` of `4c6af1f9` × `origin/main` | conflict-free |

## 2. Founder rulings needed

### 2.1 Carried from Task 5 (still open)

| # | Ruling |
|---|---|
| T5-1 | Deployment-edge derivation now lives in deliveryManager, which calls `methodcheck.DeriveDeploymentRelationships`. Accept it as a projection, or give designHealthEngine an op and a use case for it |
| T5-2 | `var-onboard-new-customer` draws web/MCP → billingManager `OnboardPaymentIntegration` / `RegisterCustomer`, which are deliberately unexposed. **Expose them, or redraw the entry.** This blocks the webClient and mcpClient onboarding P1/P2 scenarios |
| T5-4 | The billing read uses an authenticated-only PDP: any authenticated principal can read any customer's period. Accept that for now, or require the customer-scoped guard before merge |
| T5-5 | Browser review of the Billing-period lookup. It has never been run in a browser (Playwright MCP is still failing to connect) |
| D-1 | Delete what is still "deprecated in place": the gitActivityStatusAccess and designSessionAccess facet wording, the `ReviewCommentAddressed`/`Waived` statuses, `ReviewComment.Response`, and the `"plan"` TestingVariant alias. Do it in this release, once the workflows are terminated (§4 step 6) |
| — | `FinalizeDraftModel` rewrites 3 slot-4 ids from their names (`payment-provider`, `view-operating-cost-projection`, `requeue-a-failed-construction-activity`). Decide whether names or ids are the stable key |

### 2.2 New from Task 6: design changes (A7) that make scenarios bindable

| # | Change | Unblocks |
|---|---|---|
| R-D | **Client vocabulary.** `webClient` and `mcpClient` have **zero ops** (they became shells in `c45e78d4`), so `TP-STEP-OP` rejects every step: 50 for webClient, 81 for mcpClient. Choose one: (a) model the surface as contract ops, i.e. 24 web actions (`openSdpReview`, `steerActivity`, `decideReview`, …) and one op per generated MCP tool (about 20), each with an `action`/`tool` param; or (b) give TP-STEP-OP/TP-ARG-*/TP-EXPECT a Client dialect (web = data-testid, MCP = `input.tool` checked against `internal/client/mcp/*/*_tools.gen.go`). Separately, testgen routes every Client to `emitPlaywright`, so mcpClient also needs an MCP-call emitter | all 33 webClient and 47 mcpClient bound scenarios |
| A7-1 | billingEngine: the views call `PriceConstruction(estimate)` at `execute-a-project-activity/record-exit`, and that op does not exist. Add it or delete the call. Also move the call off `record-exit` (to the M0 gate, or ahead of `RecordActivityOutcome`/`ComputeEarnedValue`): it is the last stimulus in that step, so the exit and earned-value states attach to an Engine call | billingEngine execute-a-project-activity P1–P7 |
| A7-2 | designHealthEngine: in `commit-artifact`, `EvaluateDesignHealth` is drawn as the **last** call, after the commit and the merge, so "Task artifact [committed]" and "PR [merged]" attach to a pure Engine. The real gate runs at `putDraftModel` (`cmd/aiarch-state-mcp/crossartifact.go:71`). Move the call ahead of `StageArtifactForReviewOnBranch` and fix its caller | designHealthEngine execute-a-project-activity P3/P4 |
| A7-3 | messageBus: add a read-only `DescribeSchedule(scheduleID)` → `{…, Registered}`. Also decide whether `Cadence.Every`/`CronExpr` becomes a oneOf | messageBus onboard P1/P2 |
| A7-4 | merchantGatewayAccess: add a read-only `ReadStoredInstrument(customerID)` (and optionally `ReadCharges`). **Also:** P3 and retry-P4 reuse `settle:{customer}:{cycle}` after a decline, and a real gateway replays the cached decline. The idempotency key needs an attempt suffix, or the retry semantics need a ruling | merchantGatewayAccess onboard P2 |
| A7-5 | sourceControlAccess: move the existing hand-written managed-file read onto the contract as `ReadManagedFile(repo, path, cred)` → `{Content, Found}` | sourceControlAccess manage-projects P1/P2 |
| T5-3 | projectStateAccess: 13 scenarios name ops that do not exist. Add `RecordReplanTrigger`, `RecordTrackingPoint` (plus a readable progress record on `ReadProject`), and `ListStateCommits` / `ReadAppliedMutations` / `ReadProjectAt`. Relabel commit-to-a-project-option to the real facet ops (`CommitArtifactWithProvenance`, `OpenActivity`, `RejectArtifactOnBranchWithComments`, plus a revoke op). Relabel `remain-open` to `readProject`, or persist the miss | projectStateAccess's plan (none recorded today) |
| SPA-1..8 | SPA surfaces the webClient scenarios need: the amend composer on the Activity Experience (add-a-use-case P1–P4, replan P1); a grace-window **Revoke** on the M0 gate; an M0 **reject-all/reject** verb (commit P3, replan P3), or redraw those branches; `ResearchInputPanel` on first dispatch, with the operating-model ids moved into `UI_IDENTIFIERS` (manage-projects P1/P2; `CreateProjectDialog` also sends `start:false`); a **pause** control and reason field (pause P1); `Operations.REGISTER_BUTTON` (register P1); a **state-history** screen (view-the-project-state-log P1/P2); onboarding (T5-2) | 16 unbound webClient scenarios |
| B-1 | billingStateAccess and **merchantGatewayAccess** are still `stub: true`, so the only constructor is the generated not-implemented stub. Un-stub them (a Postgres store, infra `["Postgres"]`, per-profile binding arms, `main.gen.go` regen) and **choose the local-profile arm**. Also needed: a customer→project link for `ReadPersistentlyDelinquentCustomers`; per-scenario DB isolation (5 scenarios share `stp-billing-project-01`); `ReadCycle` returning `Settled=false` instead of NotFound; `ReadBilling` returning `Registered=false` | the 11 billingstate scenarios, red today; merchantgateway once it is bound |
| P-1 | The estimation `calendarStretch` change (5→7/d) **is merged** (`2d74ded7`). `DurationDays`, `IndirectCost` and `BuildCost` are now calendar days, which makes every option 1.4× longer at 5 d/wk. Confirm the calendar-day reading of the contract | already on the branch |
| P-2 | operatedSystemStateAccess `RegisterOperatedSystem` now inserts `in_flight=true` (`6d649e6b`, unmerged). The reconcile tick and the delinquency sweep then pick up an app that has no desired state. Accept that, or rebind | operatedsystemstate |
| P-3 | Replay gate: the delivery unit deleted `Test_Replay_DeliveryHistories`/`Test_Replay_PumpHistories` and all their histories while changing workflow code (`openReopenedGate`, `reopenAtSeed`, the sdpCompute outcome). After that, no replayer is left in `server/`. Rule on where determinism is gated under black-box-only (scenariohost replay, or a sanctioned arch exemption) | manager/delivery, manager/billing (`registercustomer.go:30` adds an activity without `GetVersion`) |
| P-4 | **Manager hooks import rule.** All three managers' hooks build real collaborators (engines, RAs, Temporal client/worker), and `HooksImportAllowlist` (`internal/arch_test.go:427`) admits only `projectstate/fake`. Choose one: collaborators come from generated fakes and scenariohost wires the subject (the fix-right route), or the allowlist is widened | manager/billing (15 violations), delivery (19), operations (18) |
| P-5 | **Hook symbol lint.** testgen's `Step_*`/`Probe_*` names trip revive var-naming, and units answered with inline `//nolint:revive` (artifact 11, episode 5, operatedruntime 7), which the gate forbids. Fix testgen naming, or add one scoped `.golangci.yml` exclusion for `*_hooks_test.go` | artifact, episode, operatedruntime, billingstate (13 findings) |

## 3. Exactly what is still red

### 3.1 Merge gates @ `4c6af1f9`

| Gate | Required | Now |
|---|---|---|
| `TestFileLayout` | 0 | **11**: `scenario-tests-only` in engine/billing, designhealth, intervention; manager/billing, delivery, operations; resourceaccess/agenticjob, episode, projectstate, sourcecontrol, usage (the white-box `*_test.go` files are still on the integration branch) |
| `TP-OP-REACHED` | 0 | **0** ✓ |
| `make gen-tests-check` | clean | **red**: 11 stale `*_scenarios.gen_test.go` + 11 missing hooks files (intervention; manager billing/delivery/operations; RA agenticjob, artifact, episode, operatedruntime, operatedsystemstate, projectstate, usage) + 48 "no binding" |
| `TP-BOUND` (`make method-check`, `TestMethodDesignArtifacts`) | 0 | **48**: billingEngine 15, designSessionAccess 16, sourceControlAccess 11, merchantGatewayAccess 6. Components with no plan are not checked yet: projectStateAccess, webClient, mcpClient, messageBus, designHealthEngine, security |
| scenarios | all green | `-short`: 40 pass, **11 fail** (billingstate, all `resourceaccess: not implemented`, from merge `1228aa87`). Nothing has run **without** `-short` (Docker's network API is wedged on this host), so no Postgres-backed scenario has a verdict |
| `go test ./... -short` | green | red: `TestFileLayout`, **`TestRepoStructureCmdIsClosed`** (`server/cmd/record-testplan` is tracked but not in `allowedCmd`), billingstate ×11 |
| uitests `test:scenarios` | green | **no webClient plan, so nothing is generated** (R-D) |
| a `TestRun` per component | 26 hand-built/planned | **0 recorded by DCT.** The only `.testingState.testRuns` entry is the 2026-08-09 legacy suite run. Every `record-test-run` attempt (autoscaler, estimation, operationestimation, review) was refused by the whole-document `TP-BOUND` 48 |

### 3.2 Per-unit status (23 units)

| Unit | State | Blocking item |
|---|---|---|
| engine/autoscaler, estimation, operationestimation, review | **merged, green** (-short) | the TestRun is owed until TP-BOUND reaches 0 |
| resourceaccess/billingstate | **merged, RED** (`1228aa87`, 0/11) | B-1. Undoing the merge was denied, so either fix forward (un-stub) or have the founder approve dropping `1228aa87` |
| engine/intervention `63358c4f` | rejected (10p/2f) | `engine_hooks_test.go:14` imports `framework-go/engine`. Use the generated `callContext` (a one-line fix) |
| resourceaccess/agenticjob `73bc3c55` | rejected (13p/7f) | delete `// #nosec G306` at `access_hooks_test.go:345` (a one-line fix) |
| resourceaccess/artifact `d3feb551` | rejected (9/9) | 11 `nolint:revive` (P-5) |
| resourceaccess/episode `64131fce` | rejected (6p/2f) | 5 `nolint:revive` (P-5). Its 2 probes target designSessionAccess state, which is a plan defect |
| resourceaccess/operatedruntime `c631a9ac` | rejected (8/8) | 7 `nolint:revive` (P-5). results.json is older than the hooks, so re-run it |
| resourceaccess/operatedsystemstate `6d649e6b` | rejected | all 14 scenarios skip under -short, no non-short run, and P-2 |
| resourceaccess/usage `62d90811` | rejected | never executed (Docker). `access_test.go` was not purged |
| resourceaccess/projectstate `8e444bbf` | rejected | **false green**: scenariohost v0.1.0 `Host.Record` (`results.go:50-57`) keys only on the scenario id, so 7 gitActivityStatusAccess passes overwrote 7 activityExecutionAccess failures. A single `scenariohost.Main(m, "activityExecutionAccess")` index covers 3 facets. **This is a platform fix (facet-qualified key or a per-facet index).** The unit also has a production change inside a `test(` commit (`decodeStagedModel`, `isGateTask` deleted), and 16 designSessionAccess scenarios are unbound |
| manager/billing `71ae9bde` | rejected (9p/2f) | P-4. The retry-P2 pass is vacuous (the seed already has `Escalated=true`). P-3 (`GetVersion`) |
| manager/delivery `eacc38dd` | rejected (30p/7f) | P-4, P-3. The send-back P1/P2 failure looks like a defect in this package (`firstDraftSessionView` reads `staged.ReviewThread` only) |
| manager/operations `c6fb4415` | rejected (7p/9f; plan has 16, not 17) | P-4. Hardcoded `planCycleID="2026-10"` makes verdicts depend on the calendar from 2026-11. Production defect: Query*/Withdraw map every `we.Get` error to Infrastructure (`operationsmanager.go:327,409,446,478`) |
| engine/billing, engine/designhealth, RA merchantgateway, RA sourcecontrol, utility/messagebus, client:webClient, client:mcpClient | **no work** (branches at `440db7f0`) | empty or missing plans: A7-1, A7-2, A7-3, A7-4, A7-5, R-D. White-box test files remain in billing, designhealth (`t.Skip` at `:2226`), sourcecontrol (`t.Skip`, `nolint:gosec`) and messagebus (`t.Skip` at `:298`) |

### 3.3 Plan/record discrepancies and loose ends

- The binding pass reported **designSessionAccess as "applied"**, but its plan has **0 bindings**
  and 16 `TP-BOUND`. Treat it as unbound.
- 23 `archistrator-dct-wt4-*` worktrees remain. Five are dirty with cross-unit generated files
  (designhealth 32 paths, review, artifact, operatedruntime, operatedsystemstate 30 each). Remove
  them only after their branches are settled. Never stage `internal/*/` blind from them.
- **Environment:** Docker Desktop's `docker network ls` has hung since 2026-10-02 18:19. Every
  Postgres-backed scenario, and every non-short run, is unverified until Docker is restarted.
  Docker is shared with other sessions, so the founder has to restart it.
- Carried from Plan 3: the `aiarch-construct.yml` template assumes a repo-root module (V3); the
  record step neither commits nor pushes (V1); the lossy `aiarch-state-mcp` re-encode.

## 4. Merge checklist (`deterministic-component-testing` → `main`)

1. **Rulings** from §2: R-D and P-4 first, because they gate the most scenarios. Then A7-1…A7-5,
   T5-2/T5-3, B-1, P-1…P-5, and the carried T5-1/T5-4/T5-5/D-1.
2. **Platform fixes, then release** (framework-go-scenariohost / app-generator): facet-qualified
   results keys, the hook-name lint fix (P-5), and the MCP emitter plus Client dialect (R-D).
   Re-pin and re-run `make gen-tests`.
3. **Restart Docker** (founder; it is shared). Everything below needs non-short runs.
4. **Finish Task 6.** Apply the slot-5 design changes, record the missing plans (billingEngine,
   designSessionAccess, merchantGatewayAccess, sourceControlAccess, projectStateAccess, webClient,
   mcpClient, messageBus, designHealthEngine), un-stub billingstate and merchantgateway, and
   re-run the 11 rejected units plus the 7 empty ones. Fix production defects in code (operations
   error kinds, delivery send-back thread). Resolve `TestRepoStructureCmdIsClosed`: fold
   `record-testplan` into `aiarch-state-mcp`, or delete it. Do not widen the allowlist to make it
   pass. Gates: `TestFileLayout` 0, `TP-OP-REACHED` 0, `TP-BOUND` 0, `gen-tests-check` clean,
   `make test-scenarios` and uitests `test:scenarios` green, and **one `record-test-run` per
   component**.
5. **Coordinate with the other session (6b)** before any merge. It owns
   `/Users/davidmarne/mixofrealitystudio/archistrator` on `main`, which has untracked
   `webApp/proto*` and a spec. Local `main` is **4 ahead** of `origin/main` (docs only:
   `168819d7`, `47b090a2`, `d5048071`, `ad39e34f`) and **2 behind** (release bot: server v0.8.117,
   webapp v0.6.100). The DCT branch lacks 3 of those docs commits. Agree who pushes `main` and
   freeze it for the window. Sequence this deploy against the stage-4b3 obligations in
   `docs/bugs/2026-09-29-stage4b3-earmarks.md` (`temporal schedule delete` first; the
   delinquency route stays closed).
6. **Clear Temporal before the new worker runs.** There is no production usage, so terminate;
   do not drain. Terminate:
   - executions that use any of the **31 removed or renamed activities**:
     `constructionTransitionAccess.*` (12), `revenueLedgerAccess.{readRange,recordInboundRevenue,recordReversal}`,
     `gitActivityStatusAccess.{recordActivityStarted,ArchApproved,Merged,Completed}`,
     `designSessionAccess.{rejectArtifactOnBranchWithComments,setReviewCommentStatusOnBranch,withdrawArtifactOnBranch}`,
     `activityExecutionAccess.{acknowledgeStaleBasis,commitActivityArtifacts}`,
     `agenticJobAccess.cancelAgenticJob`, `artifactAccess.retrieveOutputTree`,
     `operatedRuntimeAccess.wirePaymentConfig`, `projectStateAccess.commitArtifact`,
     `sourceControlAccess.{configureBranchProtection,installAuthorizeApp}`,
     `usageAccess.recordFinalUsage`;
   - billing close workflows (the drain, chargeback await and recompute saga are gone);
     in-flight `RegisterCustomer` workflows (an activity was added without `GetVersion`);
   - every `DeliveryActivityWorkflow` (the pause-path cancel step was removed, and
     `openReopenedGate` and the sdpCompute outcome changed), and `*:nextActivity:*` pumps;
   - the Plan 3 set: N-STP/N-IT activity types, the `testing:plan`/`testing:systemTest`
     lifecycles, and walks parked at a `testing` gate.

   Then apply the D-1 deletions in the same release.
7. **Integrate main.** Merge `origin/main` into the branch (the dry run is clean today), then run
   the full verification: `GOWORK=off go test ./...` (non-short), `make method-check`,
   `make test-scenarios`, golangci-lint v2.13.2, gofmt, `go fix -diff`, webApp `npm run check`,
   uitests, `docker build webApp/`, and `aiarch-state-mcp validate --root .`.
8. **Merge** into `main` as a merge commit (explicit-path staging only). Push, and confirm that
   `server-checks`, `webapp-checks`, `go-checks` and `uitests` are green.
9. **Release and deploy.** framework-go v0.13.0/v0.14.0 are already out, along with any platform
   releases from step 2. Let the release bot cut the server and webapp tags, then deploy after
   step 6. After deploy, clear `.activityExecution["N-STP"]` (D10) and confirm T5-5 in a browser
   on the deployed SPA.
