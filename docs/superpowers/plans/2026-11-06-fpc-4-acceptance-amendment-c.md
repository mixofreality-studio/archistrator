# FPC Plan 4 — Amendment C: Use-Case Acceptance as a Platform Gate

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Step detail is filled from Plan 3's final signatures before execution.

**Goal:** The full use-case paths that no single client projection can cover (cross-client, event-entered, later-stimulus outcomes) run against the composed app at an M‑ACCEPT milestone before the project can end, and a failure routes to a named activity.

**Architecture:** `scenario.ForAcceptance` keeps every path with actor-originated stimuli only; the platform computes the residual not already covered by one client projection. N‑ATP is a test-engineer activity binding only that residual (shared steps `$ref` the client plan). M‑ACCEPT is a milestone with a new agent-less `job` task kind run by the venue and gated in `runGate` like `testing`. **Founder ruling 1 (Amendment C to DCT §1/T5) is required before this plan starts**; §7 N‑IT removal stands.

**Spec:** §W1; §4b "N-ATP binds only the residual", "M-ACCEPT is a milestone with an agent-less job task kind", per-request principals. **Depends on:** Plan 3.

## Global Constraints (plan-local)
- No new Manager/Engine. The job task kind lives in method-assets lifecycles + delivery Manager's pump; the residual computation lives in `framework-go/scenario`.
- `TaskKind` becomes a sum type: `dispatch | review | job`; every switch over it is exhaustive (`gochecksumtype`).

## Review Focus
1. **"Come back intact" never asserted** (reload/second session): Task 4.1 `TestForAcceptance_LaterStimulusOutcomeIsResidual`.
2. **MCP write never seen in the SPA**: Task 4.1 `TestForAcceptance_CrossClientIsResidual`.
3. **Acceptance red with no route**: Task 4.4 `TestAcceptanceGate_HoldNamesPathComponents`.
4. **Multi-actor scenario silently using one principal**: Task 4.3 `TestDevPrincipals_PerRequestHeaderOnlyWhenAllowlisted`.
5. **Milestone job run on a stale composed revision**: Task 4.4 `TestAcceptanceGate_RequiresRunAtComposedRevision`.

---

### Task 4.1: `ForAcceptance` and the residual
**Files:** `framework-go/scenario/acceptance.go`, `acceptance_test.go`, fixtures.
**Interfaces:**
```go
func ForAcceptance(all []Scenario) []Scenario            // stimuli with Input.Kind != "call" only; ActorOutcome carried; ids unchanged
type Residual struct { Scenario string; Reason ResidualReason }    //sumtype:decl ResidualReason: CrossClient{Clients []string} | EventEntered{Node string} | LaterOutcome{OutcomeNode, AfterStimulus string}
func AcceptanceResidual(all []Scenario, clients []string) []Residual // a scenario is residual iff its actor stimuli span >1 client, or its first stimulus Kind ∈ {timeEvent, acceptEvent} with no client, or an ActorOutcome attaches to a stimulus later than the one whose client projection would assert it
```

### Task 4.2: N‑ATP activity and `AcceptancePlan`
**Files:** app `$defs` (`AcceptancePlan{Bindings []ScenarioBinding, Principals}` keyed by unprojected id; `RefValue` may reference `plan:<component>/<scenario>/<seq>` — extend `TestArgValue` with variant `PlanRef`), `projectstateaccess.go` (`recordPhaseArtifact` kind `acceptancePlan`), `estimationengine.go` (`deriveActivities` emits `N-ATP` {WorkerClass test-engineer, dependsOn design-system + every U-*/C-* detailedDesign}), `method-assets/lifecycles.json` (`testing:acceptancePlan` lifecycle: `atp` dispatch → `atpReview`), `commands/acceptance-test-plan.md`, methodcheck rules `ATP-BOUND` (every residual id has a binding), `ATP-REF` (plan refs resolve), TP-* v2 reused.

### Task 4.3: Per-request principals (local profile)
**Files:** `composegen/emit_http.go`, `configgen` (`<PREFIX>_DEV_PRINCIPALS` allowlist JSON `[{subject, roles}]`), `web.DevConfig{Enabled, Principal, Allowlist []Principal}`; middleware honours header `X-Dev-Principal: <subject>` only when the subject is allowlisted and profile is local; `rungen` passes the plan's `Principals` map into the env. Tests: `TestDevPrincipals_PerRequestHeaderOnlyWhenAllowlisted`, `TestDevPrincipals_RefusedUnderNonLocalProfile`.

### Task 4.4: M‑ACCEPT milestone, `job` task kind, gate, routing
**Files:** `method-assets/lifecycles.json` (`milestone:acceptance` lifecycle with task `{id:"acceptance", kind:"job", command:"acceptance-run", dependsOn:[]}` + `acceptanceReview` review), `lifecycles.go` (`LifecycleTaskJob`), app `delivery/contract.gen.go` (`ActivityTaskJob`), `estimationengine.go` (`projectEndMilestoneFor` gains predecessor `M-ACCEPT{dependsOn: every terminal activity + N-ATP}`; `MEND` depends on `M-ACCEPT`), `deliveryactivity.go` (`strategySlotFor` → `"job"`; pump runs a job task by dispatching the venue command `make acceptance-run` with no agent, records a `CheckRun` with `Task: "acceptance"`; `runGate` for `acceptanceReview` reuses `testingGateHold` with `Projected = len(ForAcceptance ∩ residual)`), `holdAcceptanceGate` (system comment listing `components on path` from the dynamic view of each failing scenario, and a `reopen` intervention naming the client activity of the first actor stimulus: new attempt on its `integration` task from a fresh branch off main). testgen `emit_acceptance_{playwright,http,mcpwire}.go` over `ForAcceptance` bindings against `LaunchDevstack`. Tests: `TestAcceptanceGate_HoldNamesPathComponents`, `_RequiresRunAtComposedRevision`, `TestPump_JobTaskRunsWithoutAgent`, `TestDerivedPlan_MAcceptPrecedesMEND`.

### Task 4.5: Release, pin, proof
Conformance (tag): `TestConformance_Acceptance` — todomvc fixture gains an `acceptancePlan` for its residual (`come-back-to-an-intact-list-P1` reload; `drive-the-list-from-agent-tooling` → SPA sees the MCP write); run green; a copy of the fixture with the SPA's checkbox made non-optimistic (null-op `markTodo` on the SPA route) goes red at M‑ACCEPT with a hold naming `todo-web-app`.

## Done when
Platform + app released and pinned; `TestConformance_Acceptance` green; own project: `N-ATP` and `M-ACCEPT` appear in the derived plan (drift gate amended via MCP in the same change, GC‑5).
