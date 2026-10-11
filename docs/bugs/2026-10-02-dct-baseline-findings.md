# DCT migration — design-health baseline (before Plan 3)

The "before" snapshot the Plan 3 final report diffs against (plan
`docs/superpowers/plans/2026-10-02-dct-plan-3-migration-workflow.md`, Task 1 Step 2).

- Captured: 2026-10-03, branch `deterministic-component-testing` @ `b7fd1d42`.
- Command: `cd server && make method-check` (`GOWORK=off go test -tags methoddesign ./internal/... -run TestMethod -count=1 -v`).
- Result: `TestMethodLayering` PASS; `TestMethodDesignArtifacts` FAIL — on the 19 expected
  `scenario-tests-only` file-layout violations (Plan 3 drives these to zero; see bottom), not on a design rule.

## Summary

| Rule | Count | Notes |
|---|---|---|
| `UC-NO-IO` | 3 | use cases with no external input stimulus — no scenario can be bound |
| `TP-OP-REACHED` | 150 | of 151 contract operations across 28 contracts, only 1 (`operationsManager.ReconcileOperatedState`) is reached by any projected scenario |
| `CC-*` | 0 | no call-chain findings emitted |

## UC-NO-IO (3)

- `onboard-a-new-customer`
- `track-weekly-project-progress`
- `view-operating-cost-projection`

## TP-OP-REACHED (150) — unreached operations by contract

| Contract | Unreached | Operations |
|---|---|---|
| `activityExecutionAccess` | 12 | OpenActivity, StageTaskOutput, RecordAttemptOutcome, OpenReviewRound, AppendReviewVerdict, SetReviewCommentStatus, DecideReviewRound, CommitActivityArtifacts, RecordActivityOutcome, RecordOperatorNote, AcknowledgeStaleBasis, ReadActivityExecution |
| `agenticJobAccess` | 3 | CancelAgenticJob, ObserveAgenticJob, SubmitAgenticJob |
| `artifactAccess` | 5 | GetTestRunIndex, ReadTestRunFile, RetrieveConstructionOutput, RetrieveOutputTree, StoreConstructionOutput |
| `autoscalerEngine` | 1 | ProposeDesiredState |
| `billingEngine` | 3 | ComputeNet, ProjectCommitTimeComputeCost, RecomputeNet |
| `billingManager` | 6 | CloseBillingCycle, OnboardPaymentIntegration, RecordInboundRevenue, RecordRevenueReversal, RegisterCustomer, RunShortfallSweep |
| `billingStateAccess` | 6 | BindGatewayLive, ReadPersistentlyDelinquentCustomers, ReadBilling, RegisterCustomer, ResettleCycle, SettleCycle |
| `constructionTransitionAccess` | 12 | RecordChangeReviewed, RecordActivityExited, RecordActivityFailed, RecordOperatorPaused, RecordOperatorResumed, RecordReviewPolicy, RecordPhaseStarted, RecordPhaseCompleted, RecordServiceContractProduced, RecordPhaseArtifactProduced, RecordOperatorNote, RecordOperatorNoteDelivered |
| `deliveryManager` | 11 | StartProject, ExecuteNextActivity, DispatchActivityTask, SubmitReviewDecision, AskQuestions, AcknowledgeStaleBasis, SetProjectRunState, OverrideActivity, SetProjectExecutionPolicy, QueryProjectView, QueryActivityView |
| `designSessionAccess` | 8 | ReadProjectOnBranch, StageArtifactForReviewOnBranch, CommitArtifactWithProvenance, RejectArtifactOnBranchWithComments, WithdrawArtifactOnBranch, ReconcileBranchFromMain, SetReviewCommentStatusOnBranch, SeedReviewCommentsOnBranch |
| `episodeAccess` | 3 | AppendEpisode, ListEpisodes, ReadTraceEvents |
| `estimationEngine` | 4 | ComputeEarnedValue, ComputeNetwork, EstimateForOption, DerivePlan |
| `gitActivityStatusAccess` | 6 | RecordActivityBranchOpened, RecordActivityCIObserved, RecordActivityArchApproved, RecordActivityMerged, RecordActivityStarted, RecordActivityCompleted |
| `interventionEngine` | 4 | ApplyPausePolicy, DecideOnHealth, DecideOnSettlementFailure, DecideOnVariance |
| `mcpClient` | 3 | listTools, invokeTool, readProjectState |
| `merchantGatewayAccess` | 2 | ChargeCustomer, ValidateStoredInstrument |
| `messageBus` | 2 | DeliverSignal, RegisterSchedule |
| `operatedRuntimeAccess` | 7 | GetApplicationHealth, GetDeploymentResourceHealth, GetSloStatus, PublishDesiredState, ReadComputeAttribution, WirePaymentConfig, Withdraw |
| `operatedSystemStateAccess` | 7 | RegisterOperatedSystem, PublishDesiredState, ReadInFlightOperatedApps, ReadOperatedSystem, RecordDelinquencyAction, RecordRuntimeStatusChange, WithdrawSystem |
| `operationEstimationEngine` | 2 | EstimateForOption, ProjectForOperatedApp |
| `operationsManager` | 7 | ApplyDelinquencyPolicy, DeployAfterConstruction, QueryCostProjection, QueryDeploymentHealth, QueryOperatedSystemView, RegisterOperatedApp, WithdrawSystem |
| `projectStateAccess` | 9 | AcknowledgeStaleBasis, AdvancePhase, CommitArtifact, CreateProject, ListProjects, ReadProject, ReadProjectVersion, SetOperatingModel, SetResearchInput |
| `revenueLedgerAccess` | 3 | RecordInboundRevenue, RecordReversal, ReadRange |
| `reviewEngine` | 1 | ProposeReviews |
| `security` | 4 | resolvePrincipal, authorize, verifyWebhookSignature, obtainServiceIdentity |
| `sourceControlAccess` | 11 | AdoptProjectRepo, CommitManagedFiles, ConfigureBranchProtection, GetInstallationToken, GetPullRequestStatus, InstallAuthorizeApp, MergePullRequest, OpenBranch, OpenPullRequest, PostReview, SyncManagedScaffold |
| `usageAccess` | 3 | ReadRange, RecordComputeUsage, RecordFinalUsage |
| `webClient` | 5 | driveDesignPhase, superviseConstruction, operateDeliveredSystem, managePaymentLifecycle, readOperatedSystemView |

## CC-* (0)

No `CC-` findings in the run.

## Other findings in the same run (context, not diffed)

| Rule | Count |
|---|---|
| `APPC-SVC-STRIVE` (info) | 19 |
| `APPC-SVC-AVOID-12` (warning, ProjectStateAccess 18 ops) | 1 |
| `ALIGN-EXTERNAL-UNWIRED` (Logging, Diagnostics) | 2 |
| `OPC-TOPIC-COVERAGE` (info) | 4 |
| `SYS-CARD-RATIO`, `SYS-CARD-TOTAL` | 1 each |
| `DEP-PLANNED-SKIPPED`, `DV-REL-UTILITY-EXEMPT` (info) | 1 each |

## Expected-red file layout (scenario-tests-only), 19 packages

- `internal/engine/autoscaler` — `engine_test.go`
- `internal/engine/billing` — `engine_test.go`
- `internal/engine/designhealth` — `engine_test.go`
- `internal/engine/estimation` — `engine_test.go`
- `internal/engine/intervention` — `engine_test.go`
- `internal/engine/operationestimation` — `engine_test.go`
- `internal/engine/review` — `engine_test.go`
- `internal/manager/billing` — `manager_test.go`
- `internal/manager/delivery` — `manager_test.go`
- `internal/manager/operations` — `manager_test.go`
- `internal/resourceaccess/agenticjob` — `access_test.go`
- `internal/resourceaccess/artifact` — `access_test.go`
- `internal/resourceaccess/episode` — `access_test.go`
- `internal/resourceaccess/merchantgateway` — `access_test.go`
- `internal/resourceaccess/operatedruntime` — `access_test.go`
- `internal/resourceaccess/operatedsystemstate` — `access_test.go`
- `internal/resourceaccess/projectstate` — `access_test.go`
- `internal/resourceaccess/sourcecontrol` — `access_test.go`
- `internal/resourceaccess/usage` — `access_test.go`

(The layout gate reports only the first offending file per package. `billingstate`, the
utility packages and the webApp are not in this list.)

## Step 4 (re-derive project design) — state at baseline

`make derived-plan-check` is green at `b7fd1d42`: the committed slots 9/10 already match the
derivation without N-STP/N-IT. Re-materialized by `19044bf0` (`make derived-plan-write`, Plan 2
Task 4's documented rail). `.slots["9"].model.activities` and `.slots["10"].model` hold no N-STP
or N-IT. One residue is outside the plan slots: `.activityExecution["N-STP"]`, an execution
record. It is construction history; only `make construction-state-reset` (D9) removes it, and
that would wipe every activity's history, so it is left for a founder ruling.
