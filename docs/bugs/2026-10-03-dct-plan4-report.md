# DCT Plan 4 (coverage per Amendment A): report, rulings, remaining red, merge checklist

Written 2026-10-03 as Plan 4 Task 7 (`docs/superpowers/plans/2026-10-03-dct-plan-4-coverage.md`).
Integration branch `deterministic-component-testing` @ `767539fa` (merge of `dct4/unbindables`).
`dct4/unbindables` @ `301504a5` is **4 commits ahead and not merged**. Platform main @ `e8ce5e9a`.

**Verdict: not merge-ready.** Tasks 1–5 landed, but Task 6 (bind → construct → verify →
integrate over all 28 contracts) did not happen. Its binding pass applied 0 plans and rejected 0,
the final integrator merged nothing, and the final suites never ran (§3.1).

## 1. Green

| Item | Evidence |
|---|---|
| framework-go **v0.13.0** released (Amendment A1–A4, `UC-START-REDUNDANT`) | tag on platform `c69fdc99`, pushed, proxy-verified; Platform Checks run `37162570333` green |
| framework-go **v0.14.0** released (facet routing: a call into a facetted component lands on the facet that declares the op, inside `scenario.Derive` via `Contract.FacetOf`) | platform `575bb64b`, merge `e8ce5e9a`, pushed, proxy-verified, CI green |
| Task 2: re-pin to framework-go v0.13.0 and method-assets v0.10.1, plus coverage baseline | `f7f2e88a`, merged `c23abf81`; `docs/bugs/2026-10-03-dct-coverage-baseline.md` |
| Task 3: **`constructionTransitionAccess` and `revenueLedgerAccess` deleted**, not deprecated (contracts, impl, generated activities, callers re-pointed) | `44809e08`, merged `7555dbcf`. The remaining mentions are history comments in Go and the deletion notes in `project.json`. No type, op, activity or caller remains |
| Task 4 (A5): 29 unreachable ops deleted, and 3 rounds of use-case extensions | `c45e78d4`; `78a7fdb7`, `f11942fd`, `9ab055ed` |
| Task 5 (A7): `designHealthEngine` service contract (`EvaluateDesignHealth`); `billingManager.GetBillingCycle` + `billingStateAccess.ReadCycle`, exposed to web and MCP (`exposedManagers` filters by op; the money-moving ops are pinned absent by a test); replan redrawn as architect-initiated and its labels name real ops; webApp gen-enums sparse-enum ordinal bug fixed; 19 drifted preview fixtures re-spliced | `ab00c566`, `c860b8a2`, `6c927931`, `7aef511f` (merged); `d25aa067`, `c08c698d`, `e5660067`, `301504a5` (on `dct4/unbindables` only) |
| **`TP-OP-REACHED` = 0** on `dct4/unbindables` (v0.14.0) | measured for this report: `make method-check` @ `301504a5` prints no `TP-OP-REACHED` finding. The count went 87 (baseline) → 72 (Task 3) → 17 (Task 4 round 3, all of them facet gaps) → 0 |
| `GOWORK=off go build ./...` | green at `767539fa` |

### 1.1 Design-health findings on `dct4/unbindables` @ `301504a5` (`make method-check`)

| Finding | Count | Note |
|---|---|---|
| `TP-OP-REACHED` | **0** | Plan 4 Review Focus 5 holds on this branch only. At `767539fa` (v0.13.0) it is still 17 |
| `TP-BOUND` | **216** errors over 19 components | Task 6 work. Up from 176 because facet routing gives the facets their own scenarios: activityExecutionAccess 20, designSessionAccess 16, gitActivityStatusAccess 7. Other counts: agenticJobAccess 20, estimationEngine 18, billingEngine 15, operatedSystemStateAccess 14, operationsManager 12, operationEstimationEngine 12, interventionEngine 12, sourceControlAccess 11, billingStateAccess 11, artifactAccess 9, operatedRuntimeAccess 8, episodeAccess 8, usageAccess 7, reviewEngine 6, merchantGatewayAccess 6, autoscalerEngine 4. The components with no plan (deliveryManager, billingManager, projectStateAccess, messageBus, mcpClient, webClient, designHealthEngine) are not checked yet |
| `UC-NO-IO` | 3 | carried: onboard-a-new-customer, track-weekly-project-progress, view-operating-cost-projection |
| `UC-START-REDUNDANT` | 1 (warning) | execute-a-project-activity, by design (A3) |
| `ALIGN-EXTERNAL-UNWIRED` 2, `SYS-CARD-*` 2, `OPC-TOPIC-COVERAGE` 4, `DV-REL-UTILITY-EXEMPT` 1, `DEP-PLANNED-SKIPPED` 1 | | carried from Plan 3 |
| `APPC-SVC-STRIVE` | 20 (warning) | was 19. The new one is DesignHealthEngine's 0-op slot-5 card |

## 2. Founder rulings needed

| # | Ruling | Recorded at |
|---|---|---|
| T5-1 | Deployment-edge derivation now lives in **deliveryManager**, which calls `methodcheck.DeriveDeploymentRelationships` directly. Accept it as a projection of the served model, or give designHealthEngine an op and a use case that draws it | earmarks §4; `ab00c566` |
| T5-2 | `var-onboard-new-customer` draws web/MCP → billingManager `OnboardPaymentIntegration` / `RegisterCustomer`, but no client exposes them. Either expose them (RegisterCustomer makes a zero-amount authorization) or redraw the entry. Until then the onboarding scenarios for webClient and mcpClient have nothing to drive | earmarks §4; `c08c698d` |
| T5-5 | **Browser review of the Billing-period lookup** (`BillingCycleLookup` on the Billing screen). It was typechecked, linted and built, but never run in a browser, because the Playwright MCP failed to connect | `c08c698d` |
| T5-4 | Accept the interim authenticated-only PDP for the billing read: any authenticated principal can read any customer's period over REST and MCP. Or require the customer-scoped guard in `WrapManagers` before merge | earmarks §4 |
| D-1 | **Delete what is still "deprecated in place".** This follows the founder's 2026-10-03 note (no production usage, delete rather than deprecate). The `projectStateAccess` notes in `project.json` still say gitActivityStatusAccess and designSessionAccess are "deprecated in place" (`projectstateaccess.go:9434` says the same), yet both carry live, reached ops. Either fold their remaining ops into activityExecutionAccess or projectStateAccess and delete the facets, or delete the word "deprecated". Also still present: the `ReviewCommentAddressed` / `ReviewCommentWaived` statuses (`projectstateaccess.go:8897-8901`), the never-written `ReviewComment.Response` (`:9264`), and the `TestingVariant` `"plan"` decode alias (earmark R3). Each one is kept only for replay of in-flight workflows. Under the ruling, terminate those workflows (§4 step 7) and delete all of it in this release, not later | this report |
| T5-3 | 22 projectStateAccess stimuli still resolve to op `""`: commit-to-a-project-option, track-weekly-project-progress, view-the-project-state-log, add-a-use-case/replan, ask-a-clarifying-question/remain-open. They are bindable (TP-STEP-OP), but the views should name real ops, following the replan pattern in `e5660067`. Confirm that this is in scope for Task 6 | earmarks §4 |
| — | `FinalizeDraftModel` rewrites 3 slot-4 ids from their names (`payment-provider` → `merchant-gateway-provider`, `view-operating-cost-projection` → `…-for-a-deployed-app`, `requeue-a-failed-construction-activity` → `re-queue-…`). This is pre-existing. Any real `putDraftModel` round trip of slot 4 renames them. Decide whether names or ids are stable | Task 4 round 3 findings |

## 3. Exactly what is still red

### 3.1 Plan 4 merge gates (Task 6)

| Gate | Required | `767539fa` | `dct4/unbindables` @ `301504a5` |
|---|---|---|---|
| `TestFileLayout` | 0 | **13** (scenario-tests-only / test-package-not-external, unchanged since Plan 3) | 13 (no package work happened) |
| `TP-OP-REACHED` | 0 | **17** (facet gaps; v0.13.0 has no facet routing) | **0** ✓ |
| `make gen-tests-check` | clean | **red**: `stale internal/manager/operations/manager_scenarios.gen_test.go`, `missing manager_hooks_test.go` (earmark T5-6) | red, same pair |
| scenario tests (`make test-scenarios`) | all green | **not run** (the permission classifier denied `go test` in the final pass) | not run |
| `go test ./... -short` | green except nothing | not run in the final pass. The last green was Task 4 round 3, with `TestFileLayout` as the only red | not run |
| webApp `npm run check`, uitests `test:scenarios` | green | **not run** | not run |
| a recorded `TestRun` per component | 28 contracts | **1** run recorded | — |
| `TP-BOUND` | 0 | 176 | **216** (§1.1) |

### 3.2 Other red and loose ends

- **Two untracked files are in both worktrees** (`archistrator-dct` and `archistrator-dct-wt-dct4-unbindables`):
  `server/internal/manager/operations/manager_hooks_test.go` (with `t.Fatal("FILL …")` stubs)
  and `manager_scenarios.gen_test.go`. `make gen` / `make gen-tests` recreates them. The agents
  were denied the delete, so **the founder must remove them**. Never stage `internal/manager/operations/` blind.
- **operationsManager production defects.** The real hooks exist only on the rejected
  `dct3/internal-manager-operations` @ `8c077917`. Its 4 `operate-a-delivered-system-P2..P5`
  scenarios fail on production code: autoscaler policy `Kind=Unknown`, and zero `CustomerID` in
  usage writes. These defects must be fixed in code. Test changes alone will not fix them.
- **Nine `dct3/*` branches are 1 commit ahead** (engine billing, intervention, operationestimation;
  manager operations; resourceaccess agenticjob, episode, projectstate, sourcecontrol, usage).
  They are the Plan 3 units that review rejected (migration report §3.5), not merges still owed.
  Under A6 their purges stand, but each one added zero scenarios. Re-run them through Task 6;
  do not merge them as they are.
- `dct4/unbindables` (+4: `d25aa067`, `c08c698d`, `e5660067`, `301504a5`) holds the v0.14.0 pin,
  the client exposure of `GetBillingCycle`, the replan label fix and the T5 earmarks. The
  integration branch is still on v0.13.0.
- Carried from Plan 3: the `aiarch-construct.yml` template assumes a repo-root module (V3). The
  record step does not commit and push (V1). The `aiarch-state-mcp` lossy re-encode.

## 4. Merge checklist (`deterministic-component-testing` → `main`)

Do these in order.

1. **Clean the trees.** Delete the two untracked operations files in both worktrees (§3.2).
2. **Merge `dct4/unbindables`** into `deterministic-component-testing`. Re-run `make method-check`
   and assert `TP-OP-REACHED == 0` before merging anything else (Plan 4 Review Focus 5).
3. **Rulings** T5-1, T5-2, T5-4, T5-5 (browser), D-1, T5-3 (§2). D-1 decides how much is deleted
   in step 5.
4. **Run Task 6 for real.** Bind the full projected set: 216 `TP-BOUND`, plus plans for the
   7 unplanned components. An empty plan for a component with scenarios counts as a rejection.
   Construct each Go package, plus `client:webClient` and `client:mcpClient`. Fix the
   operationsManager production defects. Integrate only units that add `TestScenario_*`.
   The gates are all of §3.1: `TestFileLayout` 0, `TP-OP-REACHED` 0, `gen-tests-check` clean,
   every scenario green, `npm run test:scenarios` green, and a `TestRun` recorded per component.
5. **Coordinate with the other session (6b).** It owns `/Users/davidmarne/mixofrealitystudio/archistrator`
   on `main`, which has untracked `webApp/proto*`. Local `main` is **3 ahead** (`168819d7`,
   `47b090a2`, `d5048071`, docs only) and **2 behind** `origin/main` (release bot: server
   v0.8.117, webapp v0.6.100). Agree who pushes `main` and freeze it for the merge window. Sequence
   the stage-4b3 deploy obligations (`docs/bugs/2026-09-29-stage4b3-earmarks.md`: the
   `temporal schedule delete` calls, delinquency route kept closed) around this deploy.
6. **Integrate main.** Push or rebase the 3 local doc commits, then merge `origin/main` into the
   branch. A `git merge-tree` dry run of `origin/main` × `dct4/unbindables` is conflict-free today.
   Then run the full verification on a host where Docker works: `GOWORK=off go test ./...` (not
   `-short`), `make method-check`, `make test-scenarios`, `golangci-lint` (v2.13.2), `gofmt`,
   `go fix -diff`, webApp `npm run check`, `docker build webApp/`, and
   `aiarch-state-mcp validate --root .`.
7. **Clear Temporal before the new worker runs.** There is no production usage, so terminate
   rather than wait for a drain. The replay fixtures were re-recorded, so old histories will not
   replay. Terminate:
   - executions that use any of the **31 removed activity names**:
     `constructionTransitionAccess.*` (12: recordActivityExited/Failed, recordChangeReviewed,
     recordOperatorNote/NoteDelivered/Paused/Resumed, recordPhaseArtifactProduced,
     recordPhaseCompleted/Started, recordReviewPolicy, recordServiceContractProduced);
     `revenueLedgerAccess.{readRange,recordInboundRevenue,recordReversal}`;
     `gitActivityStatusAccess.{recordActivityStarted,ArchApproved,Merged,Completed}`;
     `designSessionAccess.{rejectArtifactOnBranchWithComments,setReviewCommentStatusOnBranch,withdrawArtifactOnBranch}`;
     `activityExecutionAccess.{acknowledgeStaleBasis,commitActivityArtifacts}`;
     `agenticJobAccess.cancelAgenticJob`; `artifactAccess.retrieveOutputTree`;
     `operatedRuntimeAccess.wirePaymentConfig`; `projectStateAccess.commitArtifact`;
     `sourceControlAccess.{configureBranchProtection,installAuthorizeApp}`;
     `usageAccess.recordFinalUsage`. Four of them were moved rather than dropped, under new
     names: `projectStateAccess.{recordOperatorPaused,recordOperatorResumed,recordReviewPolicy}`
     and `activityExecutionAccess.recordOperatorNote`;
   - **billing close workflows**: the drain, the chargeback await and the recompute saga are gone (`c45e78d4`);
   - every in-flight `DeliveryActivityWorkflow`, the paused supervision walks in particular,
     because the pause path's cancel step was removed;
   - the Plan 3 set: N-STP/N-IT activity types, the `testing:plan` / `testing:systemTest`
     lifecycles, walks parked at a service or frontend `testing` gate, and `*:nextActivity:*` pumps.

   Then apply the D-1 deletions (the `"plan"` alias and the other deprecated items) in the same
   release.
8. **Merge** the branch into `main` as a merge commit. Push, and confirm `server-checks`,
   `webapp-checks`, `go-checks` and `uitests` are green on `main`.
9. **Release and deploy.** framework-go v0.13.0 and v0.14.0 are already released (§1). Let the
   release bot cut the server and webapp tags from merged `main`, then deploy after step 7. After
   deploy, clear `.activityExecution["N-STP"]` (D10) and confirm the founder's browser review of
   the billing lookup (T5-5) on the deployed SPA.
