# DCT coverage baseline after the Amendment A re-pin (Plan 4 Task 2)

Measured 2026-10-03 on branch `dct4/repin` (from `deterministic-component-testing` @ `2fb5657f`)
after re-pinning `server/go.mod` to **framework-go v0.13.0** (Amendment A1–A4 derivation,
`UC-START-REDUNDANT`) and **method-assets v0.10.1**. Spec §11 is the ruling. This file is the
input to Plan 4 Tasks 3–5. Every number below comes from the committed `project.json`.

How it was measured:

- `TP-OP-REACHED`, `TP-BOUND`, `UC-START-REDUNDANT`: `make method-check` (framework-go v0.13.0
  methodcheck over the committed project.json), deduplicated per finding.
- Scenarios per component: `projectstate.ComponentScenarios(&p, <component>)` (the server-side
  adapter the `listComponentScenarios` MCP verb uses), run for every `serviceContracts` key plus
  `design-health-engine`. Full derivation: `scenario.DeriveReport(projectstate.ScenarioInputOf(&p))`.
- "Views calling": slot-5 dynamic-view calls whose `to` is the contract's slot-5 component.
  "Named in" means a call into that component has a label containing the op name as a word.

## 1. Gates after the re-pin

| Gate | Result |
|---|---|
| `make gen` | clean. It wrote 9 new `*_scenarios.gen_test.go` files; they are committed (see §6) |
| `gen-models/fakes/client/internal-tools/temporal/config/main/lifecycles-check` | clean |
| `derived-plan-check` | clean |
| `gen-tests-check` | **red, pre-existing (2 lines):** `stale: internal/manager/operations/manager_scenarios.gen_test.go`, `missing hooks file internal/manager/operations/manager_hooks_test.go`. This is the operationsManager unit that Plan 3 rejected (`8c077917`, migration report §2.6, §3.2). The other 9 stale lines it reported are fixed by this commit |
| `go test ./... -short` | green except `TestFileLayout` (13, unchanged) |
| `make method-check` | red. It was already red on `TestFileLayout`'s 13. **New: `TP-BOUND` 149 errors (§4).** Amendment A now projects scenarios onto 15 components whose committed plans have `bindings: []`, and adds 4 new scenarios to operationsManager's partly bound plan. Task 6 binds them |
| `TestArchistratorConstructWorkflowIsTheTemplateRendering` | went red on the pin bump. Fixed: `.github/workflows/aiarch-construct.yml` was re-rendered from the v0.10.1 template (one results tree, Playwright mirror, `-uitests` spelled) |

## 2. Headline measures

| Measure | Plan 3 final (`2b2b3d37`, v0.12.0) | Now (v0.13.0) |
|---|---|---|
| Scenarios derived | 60 | **53** (7 dropped by A3) |
| `UC-START-REDUNDANT` | n/a | **1**: `execute-a-project-activity`, 7 start-family paths dropped |
| `TP-OP-REACHED` | 150 | **87** across 22 contracts |
| Components with ≥ 1 scenario | entry points only | **22** of 28 contracts (+ 0 for `design-health-engine`) |
| `UC-NO-IO` | 3 | 3: `onboard-a-new-customer`, `track-weekly-project-progress`, `view-operating-cost-projection` (unchanged) |
| `TP-BOUND` | 0 | 149 (unbound projections, §4) |
| Other findings | APPC-SVC-STRIVE 19, AVOID-12 1, ALIGN-EXTERNAL-UNWIRED 2, OPC-TOPIC-COVERAGE 4, SYS-CARD 2, DEP-PLANNED-SKIPPED 1, DV-REL-UTILITY-EXEMPT 1 | unchanged |

Stimuli with an empty `To` (26) are human decisions with no call into any component. Examples:
`commit-to-a-project-option/decision`, `replan-under-scope-change/classify` and
`view-the-project-state-log/inspect` (an operator decision). They shape the paths but no
component receives them.

## 3. `TP-OP-REACHED` per contract (87)

Kinds, for the system architect (Task 4):

- **facet gap**: the op belongs to a projectstate facet contract. Views call
  `project-state-access` with this op's name, but the projection lands on `projectStateAccess`,
  where the op does not exist. This is Task 5 (route by op name across the facet contracts). It
  is not a missing use case.
- **label drift**: a view calls the contract, but the label names an op the contract does not
  have (renamed or never written). Correct the label to the real op, or rename the op.
- **label shape**: the op is named, but not as a leading identifier that A2 can resolve.
  Correct the label.
- **no use case**: the contract is called, but no view names this op. Under A5, extend or add a
  use case, or delete the op.
- **no caller**: no view calls the contract's component at all.

| Contract | Unreached / ops | Views calling its slot-5 component | Unreached ops → kind |
|---|---|---|---|
| `activityExecutionAccess` (facet → `project-state-access`) | 12 / 12 | 13 (all project-state-access callers) | **facet gap:** `CommitActivityArtifacts`, `RecordActivityOutcome`, `RecordOperatorNote`, `ReadActivityExecution` (execute-a-project-activity), `AcknowledgeStaleBasis` (add-a-use-case-…). **No use case:** `OpenActivity`, `StageTaskOutput`, `RecordAttemptOutcome`, `OpenReviewRound`, `AppendReviewVerdict`, `SetReviewCommentStatus`, `DecideReviewRound` |
| `agenticJobAccess` | 1 / 3 | 6 | **no use case:** `CancelAgenticJob`. Production calls it on the operator-pause path (the delivery replay shows `recordOperatorPaused → deliverSignal → cancelAgenticJob`), so a pause use case is missing; this is not a dead op. 17 of 38 stimuli are unresolved: the "dispatch the … job" prose labels |
| `artifactAccess` | 3 / 5 | 2: execute-a-project-activity, operate-a-delivered-system | **no use case:** `GetTestRunIndex`, `ReadTestRunFile` (Plan 2's TestRun viewer), `RetrieveOutputTree` |
| `autoscalerEngine` | 0 / 1 | 1 | — |
| `billingEngine` | 3 / 3 | 4: bill-the-user-for-usage, commit-to-a-project-option, execute-a-project-activity, replan-under-scope-change | **label drift (all 15 stimuli unresolved):** the views name `ComputeCharge`, `ProjectCommitTimeChargeAndComputeCost` and `PriceConstruction`, but the contract has `ComputeNet`, `ProjectCommitTimeComputeCost` and `RecomputeNet` |
| `billingManager` | 2 / 6 | 3 | **no use case:** `RecordInboundRevenue`, `RecordRevenueReversal` (the revenue ledger, see `revenueLedgerAccess`) |
| `billingStateAccess` | 0 / 6 | 3 | — (3 prose "record …" stimuli are unresolved) |
| `constructionTransitionAccess` (facet) | 12 / 12 | 13 | **Delete (Task 3, ruling A5).** Three labels name its ops and must be re-pointed to `activityExecutionAccess` ops in the same edit: `RecordOperatorNote` (execute-a-project-activity), `RecordOperatorNoteDelivered` (requeue-a-failed-construction-activity), `RecordOperatorResumed` (resume-paused-construction). The other 9 ops are not named in any label |
| `deliveryManager` | 2 / 11 | 12 | **no use case:** `AcknowledgeStaleBasis` (the only label that names it calls `project-state-access`, not the manager), `SetProjectExecutionPolicy` |
| `designSessionAccess` (facet) | 8 / 8 | 13 | **facet gap:** `ReadProjectOnBranch`, `SeedReviewCommentsOnBranch`, `SetReviewCommentStatusOnBranch` (ask-a-clarifying-question-…), `StageArtifactForReviewOnBranch`, `ReconcileBranchFromMain`, `CommitArtifactWithProvenance` (add-a-use-case-…), `RejectArtifactOnBranchWithComments` (send-back-change-requests-…). **No use case:** `WithdrawArtifactOnBranch` |
| `episodeAccess` | 2 / 3 | 2: execute-a-project-activity, replan-under-scope-change | **no use case:** `ListEpisodes`, `ReadTraceEvents` (the trace viewer reads) |
| `estimationEngine` | 2 / 4 | 4 | **no use case:** `ComputeNetwork`, `DerivePlan` |
| `gitActivityStatusAccess` (facet) | 6 / 6 | 13 | **facet gap:** `RecordActivityStarted` (resume-paused-construction). **No use case:** `RecordActivityBranchOpened`, `RecordActivityCIObserved`, `RecordActivityArchApproved`, `RecordActivityMerged`, `RecordActivityCompleted` |
| `interventionEngine` | 2 / 4 | 4 | **label drift:** views name `DecideOnBillingFailure`, but the contract has `DecideOnSettlementFailure`. **No use case:** `ApplyPausePolicy` |
| `mcpClient` | 3 / 3 | 15 | **client vocabulary:** all 71 stimuli are unresolved because the labels are prose surface actions. The contract ops are `listTools`, `invokeTool` and `readProjectState`. Needs ruling R-D, or labels that lead with these ops |
| `merchantGatewayAccess` | 0 / 2 | 3 | — |
| `messageBus` | 0 / 2 | 2 | — (no test plan; no construction activity) |
| `operatedRuntimeAccess` | 3 / 7 | 1: operate-a-delivered-system | **no use case:** `GetDeploymentResourceHealth`, `WirePaymentConfig`, `Withdraw` |
| `operatedSystemStateAccess` | 3 / 7 | 2: operate-a-delivered-system, view-operating-cost-projection | **no use case:** `RegisterOperatedSystem`, `RecordDelinquencyAction`, `WithdrawSystem` |
| `operationEstimationEngine` | 0 / 2 | 4 | — (7 unresolved stimuli: `EstimateOperatingCost` label drift in execute-a-project-activity) |
| `operationsManager` | 4 / 8 | 3 | **label shape/drift:** `ApplyDelinquencyPolicy` (named in retry-a-declined-service-invoice, not resolved). **No use case:** `QueryDeploymentHealth`, `RegisterOperatedApp`, `WithdrawSystem` |
| `projectStateAccess` | 3 / 9 | 13 | **label shape:** `AcknowledgeStaleBasis`. Its label is `reconcileBranchFromMain / amend the stale slot, or acknowledgeStaleBasis(…)`, and the `/` piece leads with "amend". **No use case:** `AdvancePhase`, `CommitArtifact`. 87 of its 136 stimuli are unresolved: these are facet ops (the facet gap above) |
| `revenueLedgerAccess` (facet → `billing-state-access`) | 3 / 3 | 3 call billing-state-access, but **no label names a revenue-ledger op** | **no caller of the contract.** Task 3 deletes it, together with the impl (a permanent no-op). `billingManager.RecordInboundRevenue/RecordRevenueReversal` go to Task 4 |
| `reviewEngine` | 0 / 1 | 2 | — |
| `security` | 4 / 4 | **0** (utility, exempted by DV-REL-UTILITY-EXEMPT) | **no caller:** `resolvePrincipal`, `authorize`, `verifyWebhookSignature`, `obtainServiceIdentity` |
| `sourceControlAccess` | 3 / 11 | 3: execute-a-project-activity, manage-projects, replan-under-scope-change | **no use case:** `ConfigureBranchProtection`, `GetPullRequestStatus`, `InstallAuthorizeApp` |
| `usageAccess` | 1 / 3 | 3 | **no use case:** `RecordFinalUsage` |
| `webClient` | 5 / 5 | 15 | **client vocabulary:** all 71 stimuli are unresolved. The ops are `driveDesignPhase`, `superviseConstruction`, `operateDeliveredSystem`, `managePaymentLifecycle` and `readOperatedSystemView` (ruling R-D) |

Totals: 87 = facet contracts 38 (`activityExecutionAccess` 12, `constructionTransitionAccess` 12,
`designSessionAccess` 8, `gitActivityStatusAccess` 6) + clients 8 + `security` 4 +
`revenueLedgerAccess` 3 + 34 on the other contracts. After Task 3 deletes
`constructionTransitionAccess` and `revenueLedgerAccess`, 72 remain. Task 5's facet routing
should reach at least the 13 facet-gap ops listed above.

## 4. Scenarios per component (`ComponentScenarios`)

| Component | Scenarios | Stimuli | Unresolved-op stimuli | Ops reached | Bindings in plan | `TP-BOUND` |
|---|---|---|---|---|---|---|
| `activityExecutionAccess` | 0 | 0 | 0 | 0 | 0 | 0 |
| `agenticJobAccess` | 20 | 38 | 17 | 2 | 0 | 20 |
| `artifactAccess` | 6 | 13 | 0 | 2 | 0 | 6 |
| `autoscalerEngine` | 4 | 4 | 0 | 1 | 0 | 4 |
| `billingEngine` | 15 | 15 | 15 | 0 | 0 | 15 |
| `billingManager` | 9 | 10 | 0 | 4 | no plan | 0 |
| `billingStateAccess` | 9 | 24 | 3 | 6 | 0 | 9 |
| `constructionTransitionAccess` | 0 | 0 | 0 | 0 | 0 | 0 |
| `deliveryManager` | 37 | 74 | 4 | 9 | no plan | 0 |
| `design-health-engine` | 0 | 0 | 0 | 0 | no plan / no contract | 0 |
| `designSessionAccess` | 0 | 0 | 0 | 0 | 0 | 0 |
| `episodeAccess` | 8 | 9 | 0 | 1 | 0 | 8 |
| `estimationEngine` | 17 | 22 | 0 | 2 | 0 | 17 |
| `gitActivityStatusAccess` | 0 | 0 | 0 | 0 | 0 | 0 |
| `interventionEngine` | 11 | 12 | 7 | 2 | 0 | 11 |
| `mcpClient` | 41 | 71 | 71 | 0 | no plan | 0 |
| `merchantGatewayAccess` | 6 | 8 | 0 | 2 | 0 | 6 |
| `messageBus` | 6 | 6 | 0 | 2 | no plan | 0 |
| `operatedRuntimeAccess` | 5 | 15 | 0 | 4 | 0 | 5 |
| `operatedSystemStateAccess` | 7 | 15 | 0 | 4 | 0 | 7 |
| `operationEstimationEngine` | 13 | 13 | 7 | 2 | 0 | 13 |
| `operationsManager` | 8 | 10 | 1 | 4 | 4 (P2–P5) | 4 (new: operate-P1, retry-P3, view-cost-P1/P2) |
| `projectStateAccess` | 38 | 136 | 87 | 6 | no plan | 0 |
| `revenueLedgerAccess` | 0 | 0 | 0 | 0 | 0 | 0 |
| `reviewEngine` | 6 | 9 | 3 | 1 | 0 | 6 |
| `security` | 0 | 0 | 0 | 0 | no plan | 0 |
| `sourceControlAccess` | 12 | 31 | 0 | 8 | 0 | 12 |
| `usageAccess` | 6 | 6 | 0 | 2 | 0 | 6 |
| `webClient` | 41 | 71 | 71 | 0 | no plan | 0 |

`TP-BOUND` total is 149, spread over the 16 components that have a committed plan and at least
one unbound projected scenario. The 6 components with scenarios but no plan (`deliveryManager`,
`billingManager`, `projectStateAccess`, `messageBus`, `mcpClient`, `webClient`) are not checked by
`TP-BOUND` until Task 6 records their plans. Components with
zero scenarios: the four projectstate facets (projection lands on `projectStateAccess`),
`revenueLedgerAccess`, `security`, and `design-health-engine` (no contract; Task 5).

## 5. Contracts

| Contract | Slot-5 component | Layer | goPackage | Ops | Construction activityId (`.activityList`) |
|---|---|---|---|---|---|
| `activityExecutionAccess` | `project-state-access` | ResourceAccess | `internal/resourceaccess/projectstate` | 12 | `C-project-state-access` |
| `agenticJobAccess` | `agentic-job-access` | ResourceAccess | `internal/resourceaccess/agenticjob` | 3 | `C-agentic-job-access` |
| `artifactAccess` | `artifact-access` | ResourceAccess | `internal/resourceaccess/artifact` | 5 | `C-artifact-access` |
| `autoscalerEngine` | `autoscaler-engine` | Engine | `internal/engine/autoscaler` | 1 | `C-autoscaler-engine` |
| `billingEngine` | `billing-engine` | Engine | `internal/engine/billing` | 3 | `C-billing-engine` |
| `billingManager` | `billing-manager` | Manager | `internal/manager/billing` | 6 | `C-billing-manager` |
| `billingStateAccess` | `billing-state-access` | ResourceAccess | `internal/resourceaccess/billingstate` | 6 | `C-billing-state-access` |
| `constructionTransitionAccess` | `project-state-access` | ResourceAccess | `internal/resourceaccess/projectstate` | 12 | `C-project-state-access` |
| `deliveryManager` | `delivery-manager` | Manager | `internal/manager/delivery` | 11 | `C-delivery-manager` |
| `designSessionAccess` | `project-state-access` | ResourceAccess | `internal/resourceaccess/projectstate` | 8 | `C-project-state-access` |
| `episodeAccess` | `episode-access` | ResourceAccess | `internal/resourceaccess/episode` | 3 | `C-episode-access` |
| `estimationEngine` | `estimation-engine` | Engine | `internal/engine/estimation` | 4 | `C-estimation-engine` |
| `gitActivityStatusAccess` | `project-state-access` | ResourceAccess | `internal/resourceaccess/projectstate` | 6 | `C-project-state-access` |
| `interventionEngine` | `intervention-engine` | Engine | `internal/engine/intervention` | 4 | `C-intervention-engine` |
| `mcpClient` | `mcp-client` | Client | — (generated; `internal/client/mcp`) | 3 | none |
| `merchantGatewayAccess` | `merchant-gateway-access` | ResourceAccess | `internal/resourceaccess/merchantgateway` | 2 | `C-merchant-gateway-access` |
| `messageBus` | `message-bus` | Utility | `internal/utility/messagebus` | 2 | none |
| `operatedRuntimeAccess` | `operated-runtime-access` | ResourceAccess | `internal/resourceaccess/operatedruntime` | 7 | `C-operated-runtime-access` |
| `operatedSystemStateAccess` | `operated-system-state-access` | ResourceAccess | `internal/resourceaccess/operatedsystemstate` | 7 | `C-operated-system-state-access` |
| `operationEstimationEngine` | `operation-estimation-engine` | Engine | `internal/engine/operationestimation` | 2 | `C-operation-estimation-engine` |
| `operationsManager` | `operations-manager` | Manager | `internal/manager/operations` | 8 | `C-operations-manager` |
| `projectStateAccess` | `project-state-access` | ResourceAccess | `internal/resourceaccess/projectstate` | 9 | `C-project-state-access` |
| `revenueLedgerAccess` | `billing-state-access` | ResourceAccess | `internal/resourceaccess/billingstate` | 3 | `C-billing-state-access` |
| `reviewEngine` | `review-engine` | Engine | `internal/engine/review` | 1 | `C-review-engine` |
| `security` | `security` | Utility | — (framework) | 4 | none |
| `sourceControlAccess` | `source-control-access` | ResourceAccess | `internal/resourceaccess/sourcecontrol` | 11 | `C-source-control-access` |
| `usageAccess` | `usage-access` | ResourceAccess | `internal/resourceaccess/usage` | 3 | `C-usage-access` |
| `webClient` | `web-client` | Client | — (webApp) | 5 | `U-SPA-web-client` |

There are 28 contracts. 23 are hand-built (they have a construction activity); `mcpClient`,
`messageBus` and `security` have none. `design-health-engine` (slot 5, `internal/engine/designhealth`,
1 of the 13 `TestFileLayout` packages) has no contract.

## 6. Re-pin side effects (committed with this doc)

- `server/go.mod` / `go.sum`: framework-go v0.12.0 → **v0.13.0**, method-assets v0.10.0 →
  **v0.10.1**.
- `.github/workflows/aiarch-construct.yml` re-rendered from the v0.10.1 template
  (`ARCHISTRATOR_WRITE_CONSTRUCT_WORKFLOW=1`). Earmark, unchanged from the migration report: the
  template assumes a repo-root module (`-root .`), but archistrator's module is `server/` (V3).
- Nine `*_scenarios.gen_test.go` files, each `TestMain`-only (they are bound to no scenario):
  engine `billing`, `intervention`, `operationestimation`, `review`; resourceaccess `agenticjob`,
  `episode`, `projectstate`, `sourcecontrol`, `usage`.
- **Not committed on purpose:** `make gen-tests` also writes
  `internal/manager/operations/manager_scenarios.gen_test.go` and seeds an **unfilled**
  `manager_hooks_test.go` (`t.Fatal("FILL …")`), because operationsManager's plan holds 4
  bindings. Committing the stub would turn 4 `TestScenario_operate_a_delivered_system_P2..P5`
  red, and the hooks are construction work (Task 6, ruling R-E). The `gen-tests-check` red stays
  the pre-existing operations pair. **Do not run `make gen-tests` and stage `internal/manager/operations/` blind.**
