# Deterministic component testing, Plan 2: earmarks, deferred items, and the Plan 3 checklist (2026-10-03)

Plan 2 (`docs/superpowers/plans/2026-10-02-dct-plan-2-archistrator.md`) wired the Plan 1 platform releases into archistrator on branch `deterministic-component-testing`:

- the schema
- the scenario verbs
- `record-test-run`
- the N-STP/N-IT removal
- the test-run fetch
- the `testRun` view and its file endpoint
- the testing gate
- the systemtests removal
- the webApp panels
- (Task 10) the CI drift gate for the generated scenario tests

The branch **does not merge** until `TestFileLayout` is green at zero waivers. Plan 3's migration workflow gets it there (spec §9 step 4).

This file has three parts:

1. Spec §10's open earmarks, carried forward unchanged.
2. Everything Tasks 0–10 deferred, ruled out of scope, or left needing a ruling. Each item names the commit or code comment that records it.
3. The Plan 3 checklist: the exact `TestFileLayout` failure list, measured on `dct2/ci-docs`.

## 1. Spec §10 earmarks (not in this wave)

- **Fork/join interleaving coverage** beyond single-stimulus scenarios (the paper's §5 future work).
- **Boundary-value generation** from param schemas (`minimum` / `maxLength`), as additional deterministic cases.
- **Other-language emitters** beyond Go and TypeScript.
- **Retention.** GitHub artifacts expire (90 days by default). A run whose artifact is gone renders as "expired" (Task 5, `TestGetTestRunIndex_Expired`). It cannot re-pass the gate without a re-run (Task 7, `TestTestingGate_ExpiredArtifactHolds`).

## 2. Deferred during Tasks 0–10

### 2.1 Needs a founder ruling

| # | Item | Where it is recorded |
|---|---|---|
| R1 | **`TestRun.Revision` changed meaning.** Spec §6.1 says it is "the construction revision tested", and `record-test-run` documents `AIARCH_REVISION` as "the commit the scenario tests ran against". The gate instead compares it with the judged integration attempt id (`<activity>:integration:<n>`), because the walk holds no commit SHA: no RA op returns one. There are two ways out. Either the venue stamps the attempt id, which its checkout already holds, or the walk learns a SHA through a staged ref. Until one of them is ruled, every cloud run reads as stale. | `deliveryactivity.go` testing-gate header (EARMARK: spec drift); commits `07e528c5`, `5f5ecabd` |
| R2 | **Vacuous pass.** A run with no results, for a component that has no projected scenarios, passes the gate. Spec §6.5 does not cover this case. It was allowed so that rigs projecting use-case-less projects do not wedge. The ruling needed: hold on an empty projection, or accept it as "nothing to test". | `deliveryactivity.go` (EARMARK: the vacuous pass), `testingGateHold` |
| R3 | **`TestingVariant` ordinal 0 is kept as `TestVariantNone`, not retired.** It crosses Temporal payloads positionally, and the F81 wire-map gate requires the name. `"plan"` is a decode-only alias, kept so in-flight workflows replay. Ratified in the spec §7 row. Drop the alias only after every pre-rename `DeliveryActivityWorkflow` has drained. | commits `0368439c`, `27dec91d`, `e9ae1a1c` |

### 2.2 The venue records no run yet, so cloud testing is fail-closed

| # | Item | Where it is recorded |
|---|---|---|
| V1 | **The record step has no commit or push.** The construct template's "Record test run" step runs *after* claude-code-action has published. It writes only the runner's working tree, and no later step commits or pushes it. The template needs its own commit and push to `AIARCH_TARGET_BRANCH` (a method-assets change, then a re-seat). | `deliveryactivity.go` (EARMARK: the venue records nothing today, reason 1) |
| V2 | **`AIARCH_REVISION` is unset by the template**, so `record-test-run` exits 1 before it writes anything. This blocks on R1: which value should the template stamp? | same header, reason 2; `cmd/aiarch-state-mcp/recordtestrun.go` |
| V3 | **Archistrator's own construct workflow cannot run its scenario step.** The template assumes the app module is at the repo root (`testgen -root .`, `go test ./internal/...`), but archistrator's Go module is `server/`. `.github/workflows/aiarch-construct.yml` is gated to be the byte-exact rendering of the template (`TestArchistratorConstructWorkflowIsTheTemplateRendering`), so the fix belongs in method-assets: a module-directory template variable, then a re-seat. It must not be a hand edit. `server/Makefile.scenarios.mk` (Task 10) is the server-relative counterpart for local and CI use. | found in Task 10 |
| V4 | **No run is recorded on a `CONSTRUCTION_DRYRUN` boot.** The agentic stub does not record a run, so the gate holds every service and frontend activity for a human there. The git rigs' `gitVenueDouble` covers the tests, but not a live dry-run boot. | commit `5f5ecabd` |

Until V1 and V2 land (and R1 is ruled), **every service and frontend activity holds at its `testing` gate for a human.** This is intended: the gate fails closed, and the hold is visible.

### 2.3 Reads and plumbing

| # | Item | Where it is recorded |
|---|---|---|
| P1 | **`ReadTestRunFile` still reads `main`.** The gate and the `testRun` view read the run off `activity/<id>`, but the file endpoint resolves the artifact name from main's run record. Before the activity merges, a reviewer's video, trace, or output fetch can therefore 404. | commit `5f5ecabd` |
| P2 | **`uitests.yml` stays red until Plan 3 generates specs.** `npm run test:scenarios` loops over `uitests/generated/<component>/playwright.config.gen.ts`, and there are none yet. | commit `d724c89b`; the plan's "Known red" |
| P3 | **Plan text and code disagree on file locations.** The plan's helper files `scenario_adapter.go`, `testrun_cloud.go`, `testrun_local.go` and `testinggate.go` live inside each package's one permitted handwritten file. The reason is `TestFileLayout`'s `file-not-allowed` rule: one handwritten impl file per RA package, and one per workflow entry func in a Manager. This is recorded so that nobody "restores" the plan's layout. | commits `65a8ed9b`, `855abad2`, `07e528c5` |
| P4 | **testgen's Go filename differs from the spec.** testgen emits `<stereotype>_scenarios.gen_test.go`, with `.gen` *before* the mandatory `_test.go` suffix so the Go tool sees a test file. Spec §4.1 and §5.3 spell it `_scenarios_test.gen.go`. The spec text should be amended to match the code. | `framework-go-app-generator@v0.11.0/testgen/testgen.go:31` |
| P5 | **No `CLAUDE.md` was written.** Plan Task 10 said to add the testing section to `CLAUDE.md`. The repo has no `CLAUDE.md`, and creating one changes every agent session's instructions, which needs the founder's own say-so. The section went into `README.md` ("Testing") instead. If the founder wants it, move or copy it to `CLAUDE.md`. | Task 10 |
| P6 | **Only one of the plan's two `make` checks was used.** Plan Task 10 names `make gen-tests-check` after `gen-models-check`. testgen's `-check` is a self-contained comparison that prints drift and exits 1, with no `git diff`, so it never rewrites the tree in CI. It was verified by mutation: an orphan `engine_scenarios.gen_test.go` fails it by name. | Task 10 |

### 2.4 Platform drain before deploy

- **Drain `DeliveryActivityWorkflow` executions** for the removed activity types (N-STP, N-IT) and for the retired `testing:plan` / `testing:systemTest` lifecycles before the new worker runs (spec §9 step 4).
- The three replay fixtures re-recorded in Task 7 (`serial-stp-after-design`, `sendback-judged-pair`, `child-lease-granted-and-released`) changed at the gate's read. Any **in-flight walk that is parked at a service or frontend `testing` gate** replays against the new read, so drain those as well.

## 3. Plan 3 checklist: `TestFileLayout` (`scenario-tests-only`)

Measured on `dct2/ci-docs` with `GOWORK=off go test ./... -short` from `server/`. There are **19** violations, not the 21 the plan estimated (Task 0 measured 19 too). Each line is one in-package hand-written test file that Plan 3's per-component pipeline deletes. Every pipeline goes bind → generate → fill hooks → purge → run green → review.

- [ ] `server/internal/engine/autoscaler/engine_test.go` → `engine_scenarios.gen_test.go` + `engine_hooks_test.go`
- [ ] `server/internal/engine/billing/engine_test.go` → `engine_scenarios.gen_test.go` + `engine_hooks_test.go`
- [ ] `server/internal/engine/designhealth/engine_test.go` → `engine_scenarios.gen_test.go` + `engine_hooks_test.go`
- [ ] `server/internal/engine/estimation/engine_test.go` → `engine_scenarios.gen_test.go` + `engine_hooks_test.go`
- [ ] `server/internal/engine/intervention/engine_test.go` → `engine_scenarios.gen_test.go` + `engine_hooks_test.go`
- [ ] `server/internal/engine/operationestimation/engine_test.go` → `engine_scenarios.gen_test.go` + `engine_hooks_test.go`
- [ ] `server/internal/engine/review/engine_test.go` → `engine_scenarios.gen_test.go` + `engine_hooks_test.go`
- [ ] `server/internal/manager/billing/manager_test.go` → `manager_scenarios.gen_test.go` + `manager_hooks_test.go`
- [ ] `server/internal/manager/delivery/manager_test.go` → `manager_scenarios.gen_test.go` + `manager_hooks_test.go`
- [ ] `server/internal/manager/operations/manager_test.go` → `manager_scenarios.gen_test.go` + `manager_hooks_test.go`
- [ ] `server/internal/resourceaccess/agenticjob/access_test.go` → `access_scenarios.gen_test.go` + `access_hooks_test.go`
- [ ] `server/internal/resourceaccess/artifact/access_test.go` → `access_scenarios.gen_test.go` + `access_hooks_test.go`
- [ ] `server/internal/resourceaccess/episode/access_test.go` → `access_scenarios.gen_test.go` + `access_hooks_test.go`
- [ ] `server/internal/resourceaccess/merchantgateway/access_test.go` → `access_scenarios.gen_test.go` + `access_hooks_test.go`
- [ ] `server/internal/resourceaccess/operatedruntime/access_test.go` → `access_scenarios.gen_test.go` + `access_hooks_test.go`
- [ ] `server/internal/resourceaccess/operatedsystemstate/access_test.go` → `access_scenarios.gen_test.go` + `access_hooks_test.go`
- [ ] `server/internal/resourceaccess/projectstate/access_test.go` → `access_scenarios.gen_test.go` + `access_hooks_test.go`
- [ ] `server/internal/resourceaccess/sourcecontrol/access_test.go` → `access_scenarios.gen_test.go` + `access_hooks_test.go`
- [ ] `server/internal/resourceaccess/usage/access_test.go` → `access_scenarios.gen_test.go` + `access_hooks_test.go`

The webApp side of the same rule, which spec T2 applies to the eslint ratchet: `webApp/eslint.config.js` refuses any new `src/**/*.test.{ts,tsx}`. The **130** test files that existed before the rule are listed in `webApp/eslint.scenario-tests-only.js` (`PRE_RULE_TEST_FILES`). Plan 3 empties that list. A list that is empty, but still present, means the ratchet is done.

### Other reds on this branch at Task 10

None. `GOWORK=off go test ./... -short`: 28 packages ok, 1 FAIL (`server/internal`, `TestFileLayout` alone, the 19 above). Every gen drift gate is clean after one `make gen`: `gen-models-check`, `gen-fakes-check`, `gen-client-check`, `gen-internal-tools-check`, `gen-temporal-check`, `gen-config-check`, `gen-main-check`, `gen-lifecycles-check`, and the new `gen-tests-check`, where testgen wrote 0 files because no test plan is bound yet. `uitests.yml` is red by design until Plan 3 (P2).

## 4. Plan 4 Task 5 (unbindables as design changes): rulings owed and earmarks

Recorded at the Task 5 review fix (branch `dct4/unbindables`). Everything below is open; nothing here blocks Task 6.

| # | Item | Where it is recorded |
|---|---|---|
| T5-1 | **Ruling owed: deployment-edge derivation now lives in a Manager.** `DeriveDeploymentEdges` was a contract-less DesignHealthEngine op. When the engine gained its service contract (`EvaluateDesignHealth`), no use case drew the call, so by A5 it left the engine. deliveryManager now calls `methodcheck.DeriveDeploymentRelationships` (a framework library, the same function the DEP-EDGE-* rules judge with) from `deriveDeploymentEdges`. The founder should rule: accept it as a projection of the served model, or give it an engine op and a use case that draws it. | `deliverymanager.go` `deriveDeploymentEdges` doc comment; commit `ab00c566` |
| T5-2 | **`var-onboard-new-customer` still draws web/MCP -> billingManager for ops no client exposes.** `OnboardPaymentIntegration` and `RegisterCustomer` stay off the client layer (`cmd/clientgen` `exposedManagers` exposes billing's read-only `GetBillingCycle` only), so webClient/mcpClient's onboarding scenarios have nothing to drive. These predate Task 5. Ruling: expose them (RegisterCustomer makes a zero-amount authorization), or redraw the entry. | `cmd/clientgen/main.go` `exposedManagers`; commit `c08c698d` |
| T5-3 | **projectStateAccess labels that still resolve to no op.** After the replan redraw, 22 projectStateAccess stimuli carry `op ""`. They come from `commit-to-a-project-option` (commit-option, revoke, reject-all: `commitOption` / `recordSdpReviewOutcome` / `recordActivityWorkItemOpened`), `track-weekly-project-progress` (record-point, no-trend, corrective), `view-the-project-state-log` (read-commits, read-ledger), `add-a-use-case-to-an-in-flight-project` (replan) and `ask-a-clarifying-question-during-review` (remain-open). TP-STEP-OP makes each binding name its op, so Task 6 can still bind them, but the views should name the ops that exist, as `replan-under-scope-change` now does. | commit `e5660067` (the replan pattern) |
| T5-4 | **Billing view authorization is the interim authenticated-only PDP.** Any authenticated principal can read any customer's billing period, over REST and MCP alike, exactly as for every other exposed op. The REST handler already names `{customer, id}` for the Cedar PDP; the MCP tools authorize nothing of their own, so the Cedar cut needs a customer-scoped guard in `WrapManagers`, as delivery has for projects. | `cmd/server/hooks.go` `WrapManagers`; `authz.go` TODO(cedar) |
| T5-5 | **Founder UI review owed for the billing-period lookup** (`BillingCycleLookup` on the Billing screen). It was typechecked, linted and built, but not run in a browser in that session. | commit `c08c698d` |
| T5-6 | **`make gen` leaves two untracked files in `internal/manager/operations`** (`manager_scenarios.gen_test.go` and `manager_hooks_test.go`). They fail `TestScenario_operate_a_delivered_system_P2..P5` with `FILL newSubject` until operations is constructed in Task 6. They are not committed. | the known operations pair from Task 2 |
