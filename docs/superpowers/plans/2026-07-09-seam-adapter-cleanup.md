# Seam/Adapter Cleanup + Hand-Written-File Allowlist Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the manager-local "seam" type mirrors and hand-written adapters that survive from pre-codegen construction (billing, operations, construction), excise the dead revenue-fold spine, consolidate each component's hand-written code into a canonical file set, and add arch-test lints that keep it that way.

**Architecture:** Per Löwy (Righting Software ch. 11), managers were built against simulators (seam interfaces + mirror types in `deps.go`, converters in `adapters.go`) of not-yet-built Engines/RAs — a compression technique whose residue is deleted at integration ("when the owner ships, these local mirrors are deleted and the import substituted", `deps.go`'s own words). Every Engine/RA now has a generated contract (`contract.gen.go` from project.json via modelgen), so the integration step is: substitute generated contract types for seam mirrors, keep converters only where two contracts genuinely disagree, and fix those disagreements in project.json where ownership is clear.

**Tech Stack:** Go, modelgen/temporalgen codegen from `.aiarch/state/project.json`, `framework-go/arch` go/ast arch tests, Temporal SDK.

**Status refresh (2026-07-10, verified against main @ v0.8.41 — founder green-light to execute):**
- Seams verified still present: billing `deps.go` (57 seam refs), operations (38), construction (14);
  revenue zombie spine present (`RecordInboundRevenue`/`revenueEntrySeam` in billing deps.go:112);
  `Settlement*` naming still in `engine/intervention/contract.gen.go` (Task 3 pending); manager
  `contract.go` husks present. Tasks 1–8 apply as written (re-verify exact generated type names per each
  task's own lookup steps).
- **Landed since authoring — adjust Tasks 8–9:** appgen step-8 A1 folded RA variants into their packages
  (`variant.go` now exists in `sourcecontrol`, `constructionpipeline`, etc.) and step-8 A2 adopted the
  generated composition root (`cmd/server/main.gen.go` + `config.gen.go`; `run()` deleted; hand seam =
  `hooks.go`, plus `authz.go`/`config_adapter.go`/`managerlog.go`). Task 9's per-package merge lists must
  be re-derived from `ls` at execution time (fold `variant.go` into `access.go` with the rest); Task 8's
  deadcode re-run baseline has moved — re-run, don't reuse the stale list. `cmd/validate` still exists.
- Method framing unchanged: this is ch. 11's integration step — simulators (seams) built for compression
  get deleted when the real owner ships. All owners have shipped (every Engine/RA has `contract.gen.go`).

## Global Constraints

- All server builds/tests run with `GOWORK=off` from the `server/` directory (pinned platform releases, no workspace).
- project.json is the single source of truth for contracts; NEVER hand-edit generated `*.gen.go` files. After any project.json contract change run `cd server && make gen` (or the individual `go run ./cmd/modelgen` + `go run ./cmd/appgen` targets — check `server/Makefile` for the exact target names before running) and commit the regenerated output in the same commit as the project.json change.
- Never weaken existing gates (arch tests, methodcheck, linters). New lints are additive.
- Temporal determinism: workflow-side code (`workflow.go`) must not gain nondeterministic calls; the existing `fweng.Context{Context: context.Background()}` pattern used by the engine adapters is the established replay-safe idiom — preserve it exactly when inlining engine calls.
- **Temporal payload compatibility:** activity input/output types change shape in Tasks 1 and 4 only where seam structs are field-for-field identical to contract types (same field names → same JSON). Where a task deletes an activity or changes a payload (Task 7), the deploy requires drained workflows — same constraint as the temporalgen migration commits (`baf84c8`, `08f7043`). Note it in each such commit message.
- Commit after every green task. Messages follow existing style, e.g. `billing: retire billingstate seam mirrors — workflow speaks generated contract types`.
- Baseline verification command used throughout:
  ```bash
  cd server && GOWORK=off go build ./... && GOWORK=off go test ./internal/...
  ```

---

### Task 1: billing — retire the billingstate seam mirrors

**Files:**
- Modify: `server/internal/manager/billing/deps.go` (delete mirror types)
- Modify: `server/internal/manager/billing/adapters.go` (delete converters, keep 2)
- Modify: `server/internal/manager/billing/workflow.go` (substitute types)
- Modify: `server/internal/manager/billing/workermanifest.go`, `server/internal/manager/billing/billingmanager.go` (touch points only)
- Test: `server/internal/manager/billing/*_test.go` (same substitution in fakes/assertions)

**Interfaces:**
- Consumes: generated `billingstate` contract types from `server/internal/resourceaccess/billingstate/contract.gen.go`: `billingstate.Billing`, `billingstate.BillingTerms`, `billingstate.BillingOutcome`, `billingstate.RoutingDirective`, `billingstate.CustomerSummary`, `billingstate.Version`.
- Produces: `workflow.go` speaking `billingstate` types directly; two surviving converters in `adapters.go` with these exact signatures:
  - `func termsToEngine(t billingstate.BillingTerms) billingengine.BillingTerms`
  - `func routingDirectiveToState(d billingengine.RoutingDirective) billingstate.RoutingDirective`

- [ ] **Step 1: Delete the mirror types and their converters**

Delete from `deps.go`: `version`, `gatewayBindingSeam`, `billingOutcomeSeam`, `billingHead`, `customerSummary`, `delinquencyScope`.
Delete from `adapters.go`: `billingHeadFromState`, `billingTermsFromState`, `billingOutcomeToState`, and the OLD `routingDirectiveToState(d routingDirectiveSeam)`.

- [ ] **Step 2: Substitute at every compile error, using this exact mapping**

| Deleted symbol | Replacement |
|---|---|
| `billingHead` | `billingstate.Billing` |
| `version` | `billingstate.Version` |
| `billingTermsSeam` | `billingstate.BillingTerms` (field renames: `RevenueShareKind`/`ComputeCostKind`/`ScheduleKind`/`BillingKind` are identical; values are `int64` not `int`) |
| `billingOutcomeSeam` | `billingstate.BillingOutcome` (field `Net` is `billingstate.Money`; `Directive` is `billingstate.RoutingDirective`) |
| `customerSummary` | `billingstate.CustomerSummary` |
| `gatewayBindingSeam` | `billingstate.GatewayBinding` (verify the generated name in `billingstate/contract.gen.go` first) |
| `delinquencyScope` | inline the single field at call sites, or `billingstate` equivalent if the generated contract has one |
| `billingHeadFromState(b)` | just `b` |

Representative before/after in `workflow.go`:

```go
// BEFORE
func (wf *workflows) readBilling(ctx workflow.Context, customerID customerID) (billingHead, error) {
	b, err := wf.Acts.BillingStateReadBilling(ctx, customerID)
	if err != nil {
		return billingHead{}, err
	}
	return billingHeadFromState(b), nil
}

// AFTER
func (wf *workflows) readBilling(ctx workflow.Context, customerID customerID) (billingstate.Billing, error) {
	return wf.Acts.BillingStateReadBilling(ctx, customerID)
}
```

Where the workflow builds an outcome to persist, build the contract type directly and convert only the engine-owned directive:

```go
outcome := billingstate.BillingOutcome{
	Net:       billingstate.Money{MinorUnits: result.SignedNet.MinorUnits, Currency: result.SignedNet.Currency},
	Directive: routingDirectiveToState(result.RoutingDirective),
	Escalated: escalated,
}
```

- [ ] **Step 3: Write the two surviving converters (real divergences)**

In `adapters.go` (these replace, not join, the deleted ones):

```go
// termsToEngine bridges the RA-owned terms head-state onto the engine's compute
// input. The two contracts genuinely disagree (the engine carries percent fields
// the head-state does not); zero-fill preserves today's behavior. Divergence
// earmarked for a project.json contract alignment (see plan Task 12 earmarks).
func termsToEngine(t billingstate.BillingTerms) billingengine.BillingTerms {
	return billingengine.BillingTerms{
		RevenueShare: billingengine.RevenueShareKind(t.RevenueShareKind),
		ComputeCost:  billingengine.ComputeCostKind(t.ComputeCostKind),
		Schedule:     billingengine.ScheduleKind(t.ScheduleKind),
	}
}

// routingDirectiveToState maps the engine-owned routing decision onto the
// RA-owned persisted enum by IDENTITY (explicit switch — re-order safe).
func routingDirectiveToState(d billingengine.RoutingDirective) billingstate.RoutingDirective {
	switch d {
	case billingengine.RoutingPayout:
		return billingstate.RoutingPayout
	case billingengine.RoutingCharge:
		return billingstate.RoutingCharge
	default:
		return billingstate.RoutingNoAction
	}
}
```

(Exact enum constant names: read them from `engine/billing/contract.gen.go` and `resourceaccess/billingstate/contract.gen.go` before writing — do not guess.)

- [ ] **Step 4: Apply the same substitution table to the package tests** (fakes construct `billingstate.Billing` instead of `billingHead`, etc.)

- [ ] **Step 5: Build + test**

Run: `cd server && GOWORK=off go build ./... && GOWORK=off go test ./internal/manager/billing/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/manager/billing
git commit -m "billing: retire billingstate seam mirrors — workflow speaks generated contract types"
```

---

### Task 2: billing — call Engines through their published contracts

**Files:**
- Modify: `server/internal/manager/billing/deps.go` (delete `billingEngine`, `interventionEngine` seam interfaces + `cycleRevenueSeam`, `cycleUsageSeam`, `billingTermsSeam`, `billingResultSeam`, `reBillingInputSeam`, `routingDirectiveSeam` + consts, `billingFailureSeam`, `billingFailureKindSeam` + consts, `billingFailureDirectiveSeam` + consts)
- Modify: `server/internal/manager/billing/adapters.go` (delete `billingEngineAdapter`, `interventionAdapter` and all their `*ToEngine`/`*FromEngine` helpers)
- Modify: `server/internal/manager/billing/workflow.go`, `workermanifest.go`
- Test: `server/internal/manager/billing/*_test.go`

**Interfaces:**
- Consumes: `billingengine.BillingEngine`, `intervention.InterventionEngine` published interfaces (from each `contract.gen.go`); `termsToEngine` from Task 1.
- Produces: `wfDeps`/`workflows` fields typed as the published interfaces:
  ```go
  type wfDeps struct {
  	Billing      billingengine.BillingEngine
  	Intervention intervention.InterventionEngine
  	Acts         genInvokers
  	Custom       *customActivities
  }
  ```

- [ ] **Step 1: Retype `wfDeps`/`workflows` to the published interfaces** (as above; `newWorkflows` unchanged otherwise).

- [ ] **Step 2: Inline the context injection at engine call sites** — the published ops take `fweng.Context` as the first param; keep the exact idiom the adapter used:

```go
// BEFORE (via billingEngineAdapter)
result, cerr := wf.Billing.ComputeNet(revenue, usage, billing.Terms)

// AFTER (published contract, direct)
result, cerr := wf.Billing.ComputeNet(fweng.Context{Context: context.Background()},
	billingengine.CycleRevenue{GrossInbound: gross, EventCount: int64(n)},
	billingengine.CycleUsage{ComputeUnitSeconds: usageSeconds},
	termsToEngine(billing.Terms),
)
```

The intervention call becomes (names per current generated contract — Settlement\* until Task 3 renames them):

```go
d, derr := wf.Intervention.DecideOnSettlementFailure(fweng.Context{Context: context.Background()},
	intervention.SettlementFailure{
		CustomerID:   intervention.CustomerID(customerID.String()),
		CycleID:      intervention.CycleID(cycleID),
		Kind:         intervention.ChargeDeclined,
		AttemptCount: int64(attempts),
		ShortfallAge: int64(age),
	})
```

and the workflow's directive switch uses `intervention.SettlementRetry` / `SettlementDelay` / `SettlementEscalate` directly.

- [ ] **Step 3: Update `workermanifest.go`** — `WorkerManifest()` passes `m.billing` / `m.intervention` (already the published interfaces on `billingManager`) straight into `wfDeps` with no adapter wrap.

- [ ] **Step 4: Update tests** — fakes now implement the published interfaces (`billingengine.BillingEngine`, `intervention.InterventionEngine`) with the published types.

- [ ] **Step 5: Build + test** — `cd server && GOWORK=off go build ./... && GOWORK=off go test ./internal/manager/billing/...` → PASS.

- [ ] **Step 6: Commit** — `billing: call engines through published contracts; delete engine seams + adapters`

---

### Task 3: project.json — finish the Settlement→Billing rename in interventionEngine's contract

The interventionEngine contract still says `DecideOnSettlementFailure` / `SettlementFailure` / `SettlementFailureKind` / `SettlementFailureDirective` — residue of the ratified 2026-07-03 Settlement→Billing rename. Fix at the source so no naming-translation adapter can ever come back.

**Files:**
- Modify: `.aiarch/state/project.json` → `serviceContracts.interventionEngine` (`$defs` keys + `interface.operations[].name` + `$ref` strings)
- Regenerate: `server/internal/engine/intervention/contract.gen.go` (+ any other package that regen touches)
- Modify: `server/internal/engine/intervention/*.go` (hand impl), `server/internal/manager/billing/workflow.go` (call sites from Task 2)
- Test: existing suites

**Interfaces:**
- Produces: `intervention.BillingFailure`, `intervention.BillingFailureKind`, `intervention.BillingFailureDirective`, op `DecideOnBillingFailure` — consumed by billing's workflow.

- [ ] **Step 1: Check invariants before editing.** project.json state changes normally go through the aiarch-state MCP tools (`the-method-project-state` skill). Contract `$defs` edits of this kind were done via direct edit + regen in the schema-first migration; follow whichever path `server/Makefile`'s gen pipeline documents. Keep the edit minimal: rename keys `SettlementFailure→BillingFailure`, `SettlementFailureKind→BillingFailureKind`, `SettlementFailureDirective→BillingFailureDirective`, op name `DecideOnSettlementFailure→DecideOnBillingFailure`, and every `#/$defs/Settlement*` `$ref` accordingly. If the `$defs` enums carry `Settlement*` value names (check first), rename those too.

- [ ] **Step 2: Regenerate** — `cd server && make gen` (or the modelgen/appgen targets). Expected: `engine/intervention/contract.gen.go` and billing/operations gen files update; nothing else drifts.

- [ ] **Step 3: Fix compile errors** in `engine/intervention` hand-written impl and billing's Task-2 call sites (mechanical rename). `manager/operations` uses the health ops (`DecideOnHealth`, `ApplyPausePolicy`) — should be untouched; verify.

- [ ] **Step 4: Run the contract/validate gates** — `cd server && GOWORK=off go test ./internal/... && GOWORK=off go run ./cmd/validate` (validate reads project.json). Expected: PASS.

- [ ] **Step 5: Commit** — `interventionEngine: finish Settlement→Billing contract rename (project.json + regen)`

---

### Task 4: operations — retire the RA seam mirrors

Same recipe as Task 1, against `operatedsystemstate`, `operatedruntime`, and `usage` generated contracts.

**Files:**
- Modify: `server/internal/manager/operations/deps.go`, `adapters.go`, `workflow.go`, `signals.go`, `workermanifest.go`, `operationsmanager.go`
- Test: `server/internal/manager/operations/*_test.go`

**Interfaces:**
- Consumes: `operatedsystemstate.RuntimeStatus`, `operatedsystemstate.DesiredStateReason`, `operatedsystemstate.DelinquencyAction`, `operatedsystemstate.AutoscaleDecision`, `operatedruntime.RuntimeStatus`, `usage.UsageEvent`, `usage.ComputeUnits`, `usage.UsageRangeQuery` (verify each name in the respective `contract.gen.go` before substituting).
- Produces: `workflow.go`/`signals.go` speaking generated contract types; surviving converters ONLY where two contracts disagree.

- [ ] **Step 1: Classify each seam before deleting.** For each of `RuntimeStatusSeam`, `operatedSystem`, `operatedSystemSummary`, `inFlightScope`, `runtimeDesiredState`, `sloStatusSeam`, `computeAttribution`, `computeUnitsSeam`, `usageEventSeam`, `usageRangeQuerySeam`, `deployableBundle`, `delinquencyAction`, `version`: diff its fields against the generated contract type it mirrors (its own doc comment names the owner). Record IDENTICAL vs DIVERGENT in the commit message.

- [ ] **Step 2: IDENTICAL → delete seam + converter, substitute the generated type** at all call sites (workflow, signals, manifest, façade, tests). The paired `*FromState`/`*ToState` identity converters (`runtimeStatusFromState`, `runtimeStatusToState`, `delinquencyActionToState`, `autoscaleActionToState`, `autoscaleDecisionToState`) die here.

- [ ] **Step 3: DIVERGENT → keep ONE named converter per direction** in `adapters.go`, rewritten to convert between two *generated* types (never seam↔generated), each with a comment naming the contract divergence. Expected survivor: `runtimeStatusFromRuntime` (operatedruntime.RuntimeStatus → operatedsystemstate.RuntimeStatus — two RAs legitimately own distinct enums; the manager maps at the boundary).

- [ ] **Step 4: Build + test** — `cd server && GOWORK=off go build ./... && GOWORK=off go test ./internal/manager/operations/...` → PASS.

- [ ] **Step 5: Commit** — `operations: retire RA seam mirrors — workflow speaks generated contract types`

---

### Task 5: operations — call Engines through their published contracts

Same recipe as Task 2, for `intervention` (health ops), `autoscaler`, and `operationestimation`.

**Files:**
- Modify: `server/internal/manager/operations/deps.go` (delete `interventionEngine`, `autoscalerEngine`, `operationEstimationEngine` seam interfaces + their mirror types: `healthChange`, `interventionPolicy`, `healthDirective`, `telemetry`, `autoscalerDesiredState`, `autoscalerPolicy`, `infrastructureKind`, `autoscaleDecisionSeam`, `observedUsage`, `CostProjectionSeam`, `WhatIfCurve`, …)
- Modify: `server/internal/manager/operations/adapters.go` (delete `interventionAdapter`, `autoscalerAdapter`, `estimationAdapter` + identity helpers)
- Modify: `server/internal/manager/operations/workflow.go`, `workermanifest.go`
- Test: `server/internal/manager/operations/*_test.go`

**Interfaces:**
- Consumes: `intervention.InterventionEngine`, `autoscaler.AutoscalerEngine`, `operationestimation.OperationEstimationEngine` published interfaces + types.
- Produces: `wfDeps` holding the three published interfaces directly.

- [ ] **Step 1: Retype `wfDeps`/`workflows`** to the published interfaces; inline `fweng.Context{Context: context.Background()}` at call sites (same idiom as Task 2 Step 2).

- [ ] **Step 2: Delete identity helpers; keep real conversions.** Candidate genuine survivors (verify, don't assume): `slaTierFromString` (string config → `intervention.SLATier`) and anything bridging a workflow-local computed value onto an engine input. Anything of the form `switch localEnum → sameShapedEngineEnum` is identity — delete with the seam.

- [ ] **Step 3: Update tests** to fake the published interfaces.

- [ ] **Step 4: Build + test** — `GOWORK=off go test ./internal/manager/operations/...` → PASS.

- [ ] **Step 5: Commit** — `operations: call engines through published contracts; delete engine seams + adapters`

---

### Task 6: construction — same treatment (handoff / intervention / review engines, RA reader seams)

**Files:**
- Modify: `server/internal/manager/construction/deps.go` (seam interfaces: `projectStateReader`, `constructionTransitionAccess`, `gitActivityStatusAccess`, `handOffEngine`, `interventionEngine`, `reviewEngine` + mirrors: `constructionActivity`, `activityKind`, `workerClass`, `handOffPolicy`, `interventionMode`, `interventionPolicy`, `constructionVariance`, `varianceKind`, `varianceDirective`, `pauseRequestContext`, `pausePlan`, `reviewChange`, …)
- Modify: `server/internal/manager/construction/adapters.go` (delete `handoffAdapter`, `interventionAdapter`, `reviewAdapter` + identity maps `handoffActivityKind`, `managerWorkerClass`, `interventionVarianceKind`, `managerVarianceDirective`)
- Modify: `server/internal/manager/construction/workflow.go`, `activities_custom.go`, `workermanifest.go`
- Test: `server/internal/manager/construction/*_test.go` (incl. `adapters_test.go` — fold surviving cases into the workflow tests, delete the identity-map tests along with the maps)

**Interfaces:**
- Consumes: `handoff.HandOffEngine`, `intervention.InterventionEngine`, `review.ReviewEngine` published interfaces; `projectstate` published types for the RA reader seams.
- Produces: `wfDeps` holding published interfaces; RA reads via generated invokers/types.

- [ ] **Step 1: Check which RA reader seams are already covered by generated invokers** (`invokers.gen.go`). Where covered, delete the seam interface and call the invoker. Where a seam wraps something with no contract op (check `activities_custom.go`), keep the interface but retype its methods to `projectstate` published types — no mirrors.

- [ ] **Step 2: Engines → published contracts** (same recipe as Task 2 Step 1–2). `constructionInterventionPolicy` likely survives as a real config→contract-type builder; the four identity enum maps do not.

- [ ] **Step 3: Update tests; build + test** — `GOWORK=off go test ./internal/manager/construction/...` → PASS.

- [ ] **Step 4: Commit** — `construction: call engines/RA through published contracts; delete seams + adapters`

---

### Task 7: billing — excise the revenue-fold zombie spine (CONTRACT CHANGE — founder-visible)

Under ratified charge-only (2026-07-03), the revenue ledger was removed; billing still carries the full spine wired to a no-op (`noopRevenueLedger`), including two dead public contract ops. This task deletes the spine at the root: the contract.

**Pre-condition gate:** billing is not mounted in `cmd/server` today (verified 2026-07-09: `cmd/server` has no billing wiring), so no live client consumes these ops. Re-verify in Step 1; if a webhook route exists by execution time, STOP and surface to the founder instead of proceeding.

**Files:**
- Modify: `.aiarch/state/project.json` → `serviceContracts.billingManager` (remove ops `RecordInboundRevenue`, `RecordRevenueReversal`; remove now-unreferenced `$defs`: `GatewayRevenueEvent`, `GatewayReversalEvent`, and any orphaned refs)
- Regenerate: billing gen files (+ clientgen outputs if billing is client-mounted — expected: not)
- Modify: `server/internal/manager/billing/` — delete `signals.go` inbound/reversal paths, `activities_custom.go` (whole file: its three activities are the spine), `noopRevenueLedger` + `revenueLedgerAccess` + `revenueEntrySeam`/`reversalEntrySeam`/`entryRefSeam`/`revenueKindSeam` in `deps.go`/`adapters.go`, `foldRevenue` + `RecordInboundRevenue`/`RecordRevenueReversal` façade methods + workflow signal handlers, manifest entries `actRecordInboundRevenue`/`actRecordReversal`/`actReadRevenueRange`
- Test: billing package tests covering the removed paths get deleted; close-cycle tests updated

**Interfaces:**
- Produces: `BillingManager` interface without the two signal ops; `CloseCycleWorkflow` computing from usage only — it passes a zero `billingengine.CycleRevenue{}` to `ComputeNet` (engine contract left unchanged this pass; slimming `ComputeNet`'s revenue input is EARMARKED, not done here).

- [ ] **Step 1: Verify no client surface references the ops** — `grep -rn "RecordInboundRevenue\|RecordRevenueReversal\|recordInboundRevenue" server/internal/client server/cmd/server webApp/` → expected: no hits outside billing package + gen files. If hits: STOP, surface.

- [ ] **Step 2: Edit project.json** (remove the 2 ops + 2 event `$defs`), regenerate (`make gen`), and confirm the diff is confined to billing gen files.

- [ ] **Step 3: Delete the spine** (files/symbols listed above), fix compile errors. In `CloseCycleWorkflow`, replace the `foldRevenue` call with a zero literal + comment:

```go
// charge-only (ratified 2026-07-03): no platform-tracked inbound revenue; the
// cycle folds usage only. Engine contract still takes CycleRevenue — slimming
// ComputeNet is earmarked (see plan Task 12 earmarks).
revenue := billingengine.CycleRevenue{}
```

- [ ] **Step 4: Build + full test** — `GOWORK=off go build ./... && GOWORK=off go test ./internal/...` → PASS.

- [ ] **Step 5: Commit** — `billing: excise revenue-fold zombie spine + drop dead contract ops (charge-only) — REQUIRES drained billing workflows at deploy`

---

### Task 8: dissolve husk files + delete dead code

**Files:**
- Modify: `server/internal/manager/{billing,operations,construction,systemdesign,projectdesign}/contract.go` — for each: keep the package doc comment (updated: fix the stale file-layout listings that reference deleted files) at the top of the surviving façade file; move live symbols (type aliases like `customerID`, `newError`) into the façade file; delete `contract.go`.
- Modify/delete: `behavior.go` and `errors.go` in billing/operations (fold `mapErr` and any still-referenced helper into their single caller's file; delete unreferenced stringers — `routingDirectiveName`, `autoscaleActionName`, `desiredStateReasonName` if unreferenced after Tasks 1–6).
- Delete dead functions confirmed by deadcode AND not on the not-yet-wired allowlist: `engine/handoff/behavior.go:workerClassString`, `engine/handoff/strategy.go:architectOnlyStrategy.pickWorkerClass`, `manager/construction/contract.go:overrideKindName` (fold file first), `manager/systemdesign/contract.go:critique.Validate` (verify no reflective/test use), `cmd/aiarch-state-mcp/rawexec.go:inSubstrateComponents`.
- **Do NOT delete** (built-but-unwired UC4/UC5 surface): `resourceaccess/durableexecution/temporal.go` + `registry.go`, billing/operations `RegisterSchedules`, `constructionpipeline/behavior.go` + `durableexecution/behavior.go` exported helpers.

- [ ] **Step 1: Re-run deadcode post-Tasks-1–7** (the earlier list is stale after the refactor): `cd server && GOWORK=off go run golang.org/x/tools/cmd/deadcode@latest ./... > /tmp/deadcode.txt` and reconcile against the delete/keep lists above.
- [ ] **Step 2: Apply deletions + folds; update the package docs' file-layout comments to the real layout.**
- [ ] **Step 3: Build + test** → PASS. **Commit** — `managers: dissolve contract.go husks, fold single-use helpers, delete dead code`

---

### Task 9: consolidate to the canonical file set

Target hand-written layout (the allowlist Task 10 enforces):
- **Manager package:** `manager.go` (package doc, façade types/aliases, contract-implementing methods, worker manifest + registration) + `workflow.go` (workflow bodies, signal payloads, custom activities, in-workflow helpers, surviving converters).
- **Engine package:** `engine.go` (everything hand-written).
- **ResourceAccess package:** `access.go` (everything hand-written). Exception: `projectstate` (see below).

**Files (moves only — no logic changes):**
- billing: `billingmanager.go`+`workermanifest.go`+remnants → `manager.go`; `workflow.go`+`signals.go`+`adapters.go` remnants → `workflow.go`
- operations: same shape (`operationsmanager.go`+`workermanifest.go` → `manager.go`; `workflow.go`+`signals.go`+`adapters.go` → `workflow.go`)
- construction: same shape (incl. `dispatch.go` content → `workflow.go`, `activities_custom.go` → `workflow.go`)
- systemdesign: `systemdesignmanager.go`+`catalog.go`+`askquestions.go`+`acknowledgestale.go`+`workermanifest.go`+`contract.go` remnants → `manager.go`; `workflow.go`+`dispatch.go`+`activities_custom.go`+`gitrail.go`+`gitsession.go`+`codec.go`+`reviewledger.go`+`statevalidationfindings.go`+`prompts.go`+`behavior.go`+`findings.go`+`errors.go` → decide per file: workflow-side → `workflow.go`; anything that is neither façade nor workflow (prompt corpus, validation findings) stays as explicitly allowlisted extra files — record each in Task 10's exceptions map with a one-line reason
- projectdesign: same recipe as systemdesign
- engines (`estimation`, `handoff`, `autoscaler`, `intervention`, `review`, `billing`, `operationestimation`): merge hand files → `engine.go`
- RAs (`sourcecontrol`, `durableexecution`, `constructionpipeline`, `artifact`, `usage`, `billingstate`, `merchantgateway`, `operatedruntime`, `operatedsystemstate`): merge hand files → `access.go`
- `projectstate`: merge the git-store method files (`gitstore.go`, `gitconstruction.go`, `gitactivity.go`, `gitactivityconstruction.go`, `gitactivitystatus.go`, `reconcile.go`) → `access.go`; model/codec files stay (they are the authored schema source for schemagen) and are enumerated in the Task 10 exceptions map

- [ ] **Step 1: Per package: `git mv` the primary file to its canonical name, append the others' contents, delete the empties.** Keep every doc comment. One commit per layer.
- [ ] **Step 2: Test files:** merge only where trivially small; DO NOT force one test file (table-test files stay split; the lint does not restrict `*_test.go`).
- [ ] **Step 3: Build + full test after each layer** → PASS.
- [ ] **Step 4: Commits** — `managers: consolidate to manager.go+workflow.go`, `engines: consolidate to engine.go`, `resourceaccess: consolidate to access.go`

---

### Task 10: arch lint — hand-written file allowlist

**Files:**
- Create: `server/internal/arch_files_test.go`
- Test: itself (it IS the test)

**Interfaces:**
- Produces: `TestHandWrittenFileAllowlist` — fails if any component package contains a hand-written non-test file outside its layer's canonical set + explicit exceptions.

- [ ] **Step 1: Write the test**

```go
package internal_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandWrittenFileAllowlist enforces the post-codegen file discipline: a
// component package is generated files + a canonical hand-written set. Growing
// a new hand-written file requires adding it to `exceptions` with a reason —
// that review friction is the point.
func TestHandWrittenFileAllowlist(t *testing.T) {
	layerAllow := map[string][]string{
		"manager":        {"manager.go", "workflow.go"},
		"engine":         {"engine.go"},
		"resourceaccess": {"access.go"},
	}
	// package (layer-relative) → extra allowed files, each with a recorded reason.
	exceptions := map[string][]string{
		// projectstate model files are the authored schema source (schemagen
		// derives project.json's JSON Schema from them) — not impl split.
		"resourceaccess/projectstate": {
			"models_phase1.go", "models_phase2.go", /* …enumerate the full
			post-Task-9 set here at implementation time… */
		},
		// systemdesign prompt corpus + validation findings (nether façade nor
		// workflow); enumerated per Task 9 Step 1 decisions.
		"manager/systemdesign": { /* …enumerate… */ },
		"manager/projectdesign": { /* …enumerate… */ },
	}

	for layer, allow := range layerAllow {
		pkgs, err := os.ReadDir(layer)
		if err != nil {
			t.Fatalf("read layer dir %s: %v", layer, err)
		}
		for _, pkg := range pkgs {
			if !pkg.IsDir() {
				continue
			}
			rel := layer + "/" + pkg.Name()
			allowed := map[string]bool{}
			for _, f := range allow {
				allowed[f] = true
			}
			for _, f := range exceptions[rel] {
				allowed[f] = true
			}
			entries, err := os.ReadDir(rel)
			if err != nil {
				t.Fatalf("read %s: %v", rel, err)
			}
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() || !strings.HasSuffix(name, ".go") ||
					strings.HasSuffix(name, "_test.go") ||
					strings.HasSuffix(name, ".gen.go") {
					continue
				}
				if !allowed[name] {
					t.Errorf("%s: hand-written file %q is not in the layer allowlist %v "+
						"or the exceptions map — consolidate it into the canonical file, "+
						"or add an exception WITH a reason",
						rel, name, layerAllow[filepath.Base(layer)])
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run — expect PASS** on the post-Task-9 tree: `cd server && GOWORK=off go test ./internal/ -run TestHandWrittenFileAllowlist -v`
- [ ] **Step 3: Prove it bites** — `touch server/internal/manager/billing/stray.go` (with `package billing`), re-run, expect FAIL naming `stray.go`; delete the stray file.
- [ ] **Step 4: Add a companion check to the same file:** all methods on each contract-implementing receiver live in ONE file. Implement with `go/parser` over each package: collect `func (x *T) …` decls for the receiver type that the generated `New<Iface>` constructor returns, and assert a single filename. (Follow the existing go/ast pattern in `arch_activitynames_test.go`.)
- [ ] **Step 5: Commit** — `arch: enforce hand-written file allowlist + single-impl-file per component`

---

### Task 11: deadcode gate

**Files:**
- Modify: `server/Makefile` (new `deadcode` target wired into the existing `lint` target)
- Create: `server/.deadcode-allow` (the not-yet-wired UC4/UC5 allowlist, one symbol per line, each with a trailing `# reason` comment)

- [ ] **Step 1: Add the target** (pin the tool version like the Makefile pins its other tools — copy the existing pin idiom):

```make
deadcode: ## fail on unreachable functions not on the allowlist
	GOWORK=off go run golang.org/x/tools/cmd/deadcode@v0.48.0 ./... \
		| grep -v -F -f .deadcode-allow \
		| (! grep .) || (echo "deadcode: unreachable symbols above — delete or allowlist with reason" && exit 1)
```

- [ ] **Step 2: Populate `.deadcode-allow`** with the surviving intentional entries (durableexecution temporal impl, RegisterSchedules, behavior.go exported helpers of unwired RAs) — each line the exact `file:line:` prefix-stripped symbol string deadcode prints, with `# UC4/UC5 unwired — remove when cmd/server mounts billing/operations`.
- [ ] **Step 3: Run `make deadcode`** → expect PASS (empty output). Temporarily add an unused func, re-run, expect FAIL; remove it.
- [ ] **Step 4: Wire into `make lint` (additive) and commit** — `lint: add deadcode gate with explicit unwired-component allowlist`

---

### Task 12: earmarks (record, do not implement here)

- [ ] Add the following to the follow-ups doc the repo uses (or `docs/` note + memory):
  1. **BillingTerms contract alignment** — billingstate vs billingEngine disagree (percent fields zero-filled at the seam). Under charge-only the revenue-share fields are themselves suspect; needs a founder contract decision, then `termsToEngine` dies.
  2. **`ComputeNet` revenue-input slim-down** — engine contract still takes `CycleRevenue`; billing passes a zero literal after Task 7.
  3. **Promote the file-allowlist + single-impl-file checks into `framework-go/arch`** (platform repo) so archistrator-built apps get them from appgen's arch gate — same promotion path as the layer checks.
  4. **Generate converter functions in modelgen** for any residual cross-contract type pairs (only worth it if survivors remain after 1–2).

---

## Self-Review Notes

- **Ordering:** Tasks 1–2 before 3 (rename lands on already-direct call sites); 1–7 before 8 (deadcode list changes); 8 before 9 (don't move code that's about to be deleted); 9 before 10 (lint enforces the post-consolidation layout); 7 is independent after 2 but sequenced before 8/9 so its deletions are consolidated once.
- **Types:** surviving converter signatures defined in Task 1 Step 3 are the ones referenced in Task 2 Step 2 (`termsToEngine`) and Task 7 Step 3 (zero `CycleRevenue` literal) — consistent.
- **Unknowns deliberately deferred to implementation time with explicit "verify in contract.gen.go first" steps:** generated enum constant names, `gatewayBindingSeam`'s generated counterpart, exact Makefile gen-target names, systemdesign/projectdesign exception file lists. These are look-ups, not decisions.
- **Blast radius:** every task ends at a full `GOWORK=off go build ./... && go test ./internal/...`; Temporal payload compatibility called out in Global Constraints; Task 7 gated on the no-client-surface check.
