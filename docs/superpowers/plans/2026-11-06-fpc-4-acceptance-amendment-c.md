# FPC Plan 4 — Amendment C: Use-Case Acceptance as a Platform Gate

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Expanded to step detail 2026-11-12 from the code at platform `fpc/main` @bd67392f and app `worktree-first-pass-correctness-plan` @c4db549c (Plans 1–3 landed and released: framework-go v0.23.0, http-generator v0.6.0, app-generator v0.25.2, scenariohost v0.6.0, framework-go-conformance v0.2.0, method-assets v0.25.3).

**Goal:** The full use-case paths that no single client projection can cover (cross-client, event-entered, later-stimulus outcomes) run against the composed app at an M‑ACCEPT milestone before the project can end, and a failure routes to a named activity.

**Architecture:** `scenario.ForAcceptance` keeps every path with actor-originated stimuli only, one variant per client an actor walks it through; the platform computes the residual not already covered by one client projection (`scenario.AcceptanceResidual`). N‑ATP is a test-engineer activity (lifecycle `testing:acceptancePlan`) binding only that residual into `.phaseArtifacts.acceptancePlan` (shared step inputs `planRef` the client plan; cross-actor observations are probes run `as` another actor). M‑ACCEPT is a zero-effort derived activity (lifecycle `milestone:acceptance`) whose one work task has the new agent-less `job` kind: the venue runs the task spec's checks (boot smoke, regenerated acceptance tests, every residual scenario over the wire and in the browser) and records the CheckRun; its `testing` review reuses the scenario testing gate's judgement over the pseudo-contract `acceptance` and, when held, names the components on each failing path and the client activity to re-open. **Founder ruling 1 (Amendment C to DCT §1/T5) is required before this plan starts**; §7 N‑IT removal stands.

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

## Detail-phase decisions (2026-11-12; each derived from the code named, the lead may override)

- **D1 — what the residual is, from the data.** `scenario.stimuli` (`framework-go/scenario/derive.go:134-153`) opens one stimulus per call of a node, so a node where an actor may use several clients (todomvc `work-the-list` n1a: team-member→todo-web-app, team-member→todo-mcp-server, team-agent→todo-mcp-server, team-agent→todo-api) yields several *alternative* stimuli at one node, and `observeOutput` (`derive.go:237-254`) attaches an actor-visible output to every actor stimulus of that node. A client projection (`ForComponentAs(all, C, true)`, `derive.go:431`) keeps the stimuli into C with their `ActorOutcome`. So the one thing no client projection asserts is an actor-visible outcome whose actor did not call through that client: `work-the-list`'s `out-team-member` on the agent's `todo-api` stimulus ("the surface shows the member the list as kept" = the SPA sees the agent's write), and the mirror `out-team-agent` on the member's stimulus. `ForAcceptance` therefore emits one **variant per cover client** (`<id>@<client>`: the path as walked through that client, alternatives into other clients dropped at nodes the client serves), and `AcceptanceResidual` names a variant residual for exactly three reasons (Task 4.1). Over the current todomvc fixture the residual is every variant of `work-the-list` and `resume-the-list` (the two use cases two actors originate); `come-back-to-an-intact-list-P1` and `drive-the-list-from-agent-tooling-P1`, which the sketch named, are covered by one client projection (one actor, one client, the outcome's actor is the caller) and the tests pin them as **not** residual. A same-client reload path is the web projection's (two steps, one client), never the residual's.
- **D2 — the acceptance plan record.** `.phaseArtifacts.acceptancePlan` is a singleton record (`PhaseArtifacts` is a struct of maps keyed by component, `projectstateaccess.go:3038-3047`; the acceptance plan belongs to no component, so it is a pointer field, recorded with mapKey `acceptance`). It reuses `ScenarioBinding`/`StepBinding` (`methodcheck/project.go:768-790`) keyed by the variant id. A shared step's inputs reference the client plan by a new `TestArgValue` variant `planRef{component, scenario, seq, input}` (kind `planRef`), resolved at generation to the literal it names; a cross-actor observation is a `Probe` run `as` another actor (`Probe` gains `as` (optional actor id), `project.go:795-801`, app `contract.gen.go:777-783`); a web-bearing variant names its `browserActor` (one browser identity per launch: the app runs as `principals[0]`, `scenariohost/devstack.go:93-120`). TP-* v2 is reused unchanged through `bindingFindings` (`rules_testplan.go:144`) with a per-step contract lookup by the stimulus's `Input.To`.
- **D3 — N-ATP's place in the network.** `ResolveDependencySatisfied` (`projectstateaccess.go:9798-9828`) knows activities (satisfied when Done) and milestones only; a dependency on another activity's *task* (the sketch's "every U-*/C-* detailedDesign") does not exist. N-ATP therefore depends on M0 alone (the source rule, `estimationengine.go:1963`) and runs in parallel with construction; a residual variant whose web surface has no committed UI design yet is bound with `skip: {reason, until: <that client's activity id>}` (TP-SKIP, `rules_testplan.go:475-489`), and the M-ACCEPT job re-runs the ATP-*/TP-* rules at run time, where a skip whose `until` activity is integrated is an Error that holds the gate and routes to N-ATP.
- **D4 — M-ACCEPT is a zero-effort derived activity, not a `NetworkMilestone`.** The pump opens activities only (milestones are derived pass-throughs, D3), so the node that runs the job is a `DerivedActivity{Name: "M-ACCEPT", EffortDays: 0}` (`EstimateForOption` refuses only `EffortDays < 0`, `estimationengine.go:674`; `legalEffort` guards additives only, `:2081`). `projectEndMilestoneFor` (`:1914`) is unchanged: once M-ACCEPT depends on every other terminal plus N-ATP, `networkTerminals` yields `{M-ACCEPT}` and MEND depends on it. The activity is classified by exact id (as the three design ids are, `projectstateaccess.go:9420-9435`) to a new `ActivityTypeMilestone` ("milestone"), and its lifecycle key is spelled `milestone:<variant>` with `TestingVariant` gaining `acceptance` (6) — the same `<type>:<variant>` form `LifecycleKeyFor` (`:9995`) spells for testing — so the key is `milestone:acceptance`; N-ATP is `testing:acceptancePlan` (`TestVariantAcceptancePlan` = 5, by the `N-ATP` prefix in `deriveVariant`, `:9393`).
- **D5 — the `job` kind and its venue.** A job task carries `command` as the venue routing slug (`aiarch-construct.yml` requires a command input and `constructDispatchPlan` a command) but no step manifest, workerClass or prompt: `MethodSpecFor` (`method-assets/taskspec.go:35`) returns the identity for a job before `ManifestFor`. The venue runs the task spec's checks and records the CheckRun exactly as it does after an agent (`run-checks --gate` → `record-check-run`, template lines 705-781; local `runGates`/`recordCheckRun`, `agenticjobaccess.go:2610,2735`), and never starts an agent (the template conditions its agent steps on `steps.spec.outputs.kind`; the local arm runs `runJob` instead of spawning claude, so a job needs no `claude` on PATH). The lifecycle's task ids stay Figure A-1 ledger keys (`TestLifecycles_ConstructionFamilyReusesTheLedgerTaskIDs`, `lifecycles_test.go:280`): the job is `integration`, its review `testing`, so `integrationTaskID` (`deliverymanager.go:14670`), `selectCheckRun` and `queryTestRunView` serve M-ACCEPT unchanged. The Manager's `jobTaskStrategy` is ONE `dispatchConstructionOnce` (no variance loop: a red check run is a verdict for the gate, not a variance) and its subject is passed when the attempt's CheckRun was recorded, red or green.
- **D6 — the acceptance check set.** The job's spec is `rule:atp` + `bootChecks` (build-server, build-devstack, `smoke:composition`) + `command:gen-tests-check` + one `scenario:acceptance:<variant>` per residual variant, under the pseudo-contract `acceptance` (`projectstate.AcceptanceContract`). testgen emits the Go half into `internal/acceptance/acceptance_scenarios.gen_test.go` (the non-layer `internal/scenariocompose` precedent) and the web half into `uitests/generated/acceptance/`; the check runner splits the batch by which file holds the variant. The gate reuses `testingGateHold` (`deliveryactivity.go:6717`) with `Contract: "acceptance"` and `Projected = len(residual)`.
- **D7 — the route.** `OverrideActivity(kind: retry)` on a finished activity re-arms it and pumps it now (`deliverymanager.go:6489-6544, 6682`); that is the reopen. The hold comment names, per failing variant, the components on the path (every call endpoint of the dynamic view steps attached to the path's nodes) and `OverrideActivity(<client activity>, retry)` for the client of the variant's first actor stimulus. No automatic reopen: the gate is a human gate, as the testing gate is.
- **D8 — Task 4.3 reduced.** Plan 3 Task 3.3 delivered the local-only `<PREFIX>_DEV_PRINCIPALS` allowlist, `X-Dev-Principal` honoured only for an allowlisted subject (`httpgen/wiring.go:108-150`), refusal under a non-local profile (`configgen/emit.go:255-270`, pinned by `TestConfig_LocalDevPrincipals`) and the launch env from a plan's principals (`scenariohost/devstack.go:93-150`). What Plan 4 still needs: the browser runtime sending a per-call principal (`tsruntime/runtime.ts:120-123` reads one env var), the acceptance plan's principals reaching `cmd/webscenarios` (`surfacePrincipals` reads `testPlan.<surface>`, `main.go:287`), and the browser actor first.
- **D9 — the fixture's SPA.** The fixture's browser scenarios are TP-BROKEN against the scaffold SPA (`clients_test.go:258-264`), so an acceptance run over web probes cannot be green on it. RUN-021's harvest holds the hand-written SPA (`archistrator-bench-scratch/archive/run-20261105T141816Z-04ce59ca/harvest/app/webApp/src/todo-web-app/{main.tsx,routes/App.tsx,components/{Footer,NewTodoInput,StatusStrip,TodoItem}.tsx,hooks/{useHashView,useTodoList}.ts}`); Task 4.5 seats it into `testdata/todomvc/impl/webApp/src/todo-web-app/` (the chain overlays `impl/` after `webgen generate` and before `npm install`, `chain.go:160-190`). The red proof mutates that copy in memory (as `vacuousDiscard` mutates the JSON, `run021_test.go:373`).

## File map

Platform:
- Modify `framework-go/scenario/scenario.go`; create `scenario/acceptance.go`, `acceptance_test.go`, `testdata/acceptance_{crossactor,event,covered}.json` (+ goldens).
- Modify `framework-go/methodcheck/{project.go,bindings.go,emitters.go,validate.go,rules_testplan.go}`; create `rules_acceptance.go`, `rules_acceptance_test.go`, `testdata/atp_{clean,unbound,badref}.json`.
- Modify `method-assets/{lifecycles.json,lifecycles.go,taskspec.go,lifecycles_test.go,stepmanifest.gen.go}`, `assets/claude/agents/test-engineer.md`, `assets/workflows/aiarch-construct.yml.tmpl`; create `assets/claude/commands/acceptance-test-plan.md`.
- Modify `framework-go-app-generator/testgen/{testgen.go,emit_mcp.go,emit_steps.go,tsruntime/runtime.ts}`; create `testgen/emit_acceptance.go`, `emit_acceptance_playwright.go`, `emit_acceptance_http.go`, `emit_acceptance_mcpwire.go`, `emit_acceptance_test.go`.
- Modify `framework-go-scenariohost/cmd/webscenarios/main.go` (+ test).
- Modify `framework-go-conformance/testdata/todomvc/{project.json,README.md}`, `.github/workflows/platform-checks.yml`; create `testdata/todomvc/impl/webApp/src/todo-web-app/**`, `framework-go-conformance/acceptance_test.go`.

App:
- Modify `.aiarch/state/project.json` (`$defs`: `ActivityType`, `TestingVariant`, `ActivityTaskKind` ×2, `PhaseArtifacts`, `PhaseArtifactPayload`, `Probe`, `TestArgValue`; new `AcceptancePlanRecord`, `PlanRef`; `ruleWaivers`; slots 9/10 re-materialized), `server/internal/resourceaccess/projectstate/projectstateaccess.go`, `server/internal/engine/estimation/estimationengine.go`, `server/internal/manager/delivery/{deliveryactivity.go,deliverymanager.go}`, `server/internal/resourceaccess/agenticjob/agenticjobaccess.go`, `server/cmd/aiarch-state-mcp/{checks.go,constructverbs.go,scenarioverbs.go,tools.go}`, `server/cmd/gen-lifecycles/main.go`, `webApp/src/components/activity/{lifecycleGraphTypes.ts,LifecycleGraph.tsx}`, `server/go.mod`.
- Create `server/internal/{acceptance_test.go,derivedplan_test.go,jobtask_test.go}`, `server/internal/acceptance/doc.go`.
- Regenerate `contract.gen.go` (projectstate, delivery), fakes, `webApp/src/contracts/schema.ts`, `webApp/src/components/activity/lifecycles.gen.ts`.

---

### Task 4.1: `ForAcceptance` and the residual

**Files:**
- Modify: `framework-go/scenario/scenario.go` (doc of `Stimulus.Input.Via`; nothing structural)
- Create: `framework-go/scenario/acceptance.go`, `framework-go/scenario/acceptance_test.go`
- Create: `framework-go/scenario/testdata/acceptance_crossactor.json` (the todomvc `work-the-list` diagram and view verbatim from `framework-go-conformance/testdata/todomvc/project.json` slots 4/5, contracts `todoWebApp`/`todoMcpServer`/`todoApi` (Client) and `listManager`), `testdata/acceptance_event.json` (a path `timeEvent "nightly" → manager action → sendSignal to member`, one client contract), `testdata/acceptance_covered.json` (the todomvc `come-back-to-an-intact-list` and `drive-the-list-from-agent-tooling` diagrams and views) + `*.golden.json`

**Interfaces (Produces):**
```go
// ForAcceptance is every scenario as one actor walks it through one client: for each scenario
// and each client C ∈ clients (contract keys, Normalize-folded) that some actor-originated
// stimulus targets, the variant "<id>@<C>" keeps, per node, the actor-originated stimuli into C
// (alternatives into other clients at that node dropped), every actor-originated stimulus into
// another client at a node C does not serve, and every non-call event stimulus
// (Input.Kind ∉ {"call"} with To not a client); component-to-component stimuli (Input.Kind ==
// "call") are dropped. Seq is renumbered from 1; ActorOutcome is kept on every stimulus; ids,
// titles, paths and guards are the source scenario's. A scenario with no actor stimulus yields
// no variant. Variants are sorted by id.
func ForAcceptance(all []Scenario, clients []string) []Scenario

// Residual is one acceptance variant no single client projection covers, and why.
type Residual struct {
	Scenario string         // the variant id, "<id>@<client>"
	Reason   ResidualReason
}

//sumtype:decl
type ResidualReason interface{ isResidualReason() }
// CrossClient: the variant's actor-originated stimuli target more than one client (some actor
// node has no stimulus into the cover client), so the path leaves the client mid-way.
type CrossClient struct{ Clients []string }
// EventEntered: the variant's first stimulus is a timeEvent or acceptEvent whose To is not a client.
type EventEntered struct{ Node string }
// LaterOutcome: an ActorOutcome output of stimulus AfterStimulus is seen by an actor that did not
// originate it (Output.To ∉ {Input.From} ∪ Input.Via), or the stimulus's To is not a client
// (no client projection keeps its ActorOutcome: stimuliTo zeroes it, derive.go:448-461).
type LaterOutcome struct{ OutcomeNode, AfterStimulus string }

// AcceptanceResidual is ForAcceptance(all, clients) filtered to the residual, one entry per
// (variant, reason) in variant then node order; a variant with several reasons has several entries.
func AcceptanceResidual(all []Scenario, clients []string) []Residual
// IsResidual reports whether variant id names a residual variant (the gate's and testgen's one question).
func IsResidual(rs []Residual, id string) bool
```
- Variant id: `s.ID + "@" + Normalize(C)`; the `@` keeps the binding key, the check id (`scenario:acceptance:<variant>`) and the Go test name (`ident` maps `@` to `_`) one string.

- [ ] **Step 1: Write the failing tests** (`acceptance_test.go`, using `load(t, name)` and `golden(t, name, b)` from `derive_test.go:13-41`):
```go
func TestForAcceptance_OneVariantPerCoverClient(t *testing.T)    // acceptance_crossactor: work-the-list-P1 yields variants @todoApi, @todoMcpServer, @todoWebApp; each has exactly one stimulus at n1a (the alternatives into the other clients dropped); no Input.Kind == "call" stimulus survives; the @todoApi variant's stimulus has Input.From == "team-agent" and ActorOutcome.ExpectedOutputs naming out-team-member and out-team-agent
func TestForAcceptance_CrossClientIsResidual(t *testing.T)        // acceptance_crossactor: AcceptanceResidual names "work-the-list-P1@todoApi" with LaterOutcome{OutcomeNode: "out-team-member", AfterStimulus: "n1a"} (the SPA sees the MCP/REST write) and "work-the-list-P1@todoWebApp" with LaterOutcome{"out-team-agent", "n1a"}; a hand-built scenario whose actor stimuli at n1 target clientA and at n3 clientB is residual as CrossClient{Clients: ["clientA", "clientB"]} for both variants
func TestForAcceptance_LaterStimulusOutcomeIsResidual(t *testing.T) // acceptance_event: the variant's first stimulus is the timeEvent (To == "listManager"); AcceptanceResidual has EventEntered{Node: "t1"} AND LaterOutcome{OutcomeNode: "out-member", AfterStimulus: "t1"} (the member's "come back" outcome attaches to a stimulus no client projection keeps)
func TestForAcceptance_CoveredIsNotResidual(t *testing.T)           // acceptance_covered: come-back-to-an-intact-list-P1@todoWebApp, @todoMcpServer and drive-the-list-from-agent-tooling-P1@todoMcpServer, @todoApi exist in ForAcceptance and AcceptanceResidual is empty
func TestAcceptanceResidual_IsDeterministic(t *testing.T)           // two calls over a shuffled copy of all give equal results
func TestForAcceptance_Golden(t *testing.T)                        // golden(t, "acceptance_crossactor", CanonicalJSON(ForAcceptance(...)))
```
- [ ] **Step 2: Run** `cd /Users/davidmarne/mixofrealitystudio/fpcwt/archistrator-platform/framework-go && GOWORK=off go test ./scenario -run 'TestForAcceptance|TestAcceptanceResidual' -v` — Expected: FAIL (undefined `ForAcceptance`, `AcceptanceResidual`).
- [ ] **Step 3: Implement** `acceptance.go`: `coverClients(s, clients)`, `variant(s, c, clients)`, the three reason checks (`crossClient`, `eventEntered`, `laterOutcomes`) each a pure function over one variant; `isResidualReason` on the three structs; a `describe(r ResidualReason) string` with an exhaustive `switch` (no default) used by the error messages. Regenerate the golden with `-update` once green and inspect it.
- [ ] **Step 4: Run** `GOWORK=off go test ./scenario ./methodcheck` — Expected: PASS; `golangci-lint run ./scenario` — 0 issues (gochecksumtype over `ResidualReason`).
- [ ] **Step 5: Commit** `git commit -m "feat(scenario): ForAcceptance variants per cover client; AcceptanceResidual (CrossClient | EventEntered | LaterOutcome)"`.

---

### Task 4.2: N‑ATP activity and `AcceptancePlan`

**Files:**
- Modify: `framework-go/methodcheck/project.go:729-737` (`PhaseArtifacts`), `:795-801` (`Probe`), `bindings.go:31-57` (`PlanRef`), `:82-100` (`UnmarshalTestArgValue`), `emitters.go:17` (ids), `validate.go:40-50` (`scenarioFindings`), `rules_testplan.go:144-176` (`bindingFindings` takes an `opLookup` per step)
- Create: `framework-go/methodcheck/rules_acceptance.go`, `rules_acceptance_test.go`, `testdata/atp_clean.json` (tp2_clean.json's project + the todomvc `work-the-list` diagram/view and an `acceptancePlan` binding `work-the-list-P1@todoApi`), `atp_unbound.json`, `atp_badref.json`
- Modify: `method-assets/lifecycles.json` (new lifecycle `testing:acceptancePlan`), `lifecycles_test.go:15-18` (`wantLifecycleTypes`), `assets/claude/agents/test-engineer.md` (`tools:` + `mcp__aiarch-state__listAcceptanceScenarios`), create `assets/claude/commands/acceptance-test-plan.md`; regenerate `stepmanifest.gen.go` (`go run ./cmd/gen-stepmanifest`)
- App: `.aiarch/state/project.json` `$defs` (`AcceptancePlanRecord`, `PhaseArtifacts.acceptancePlan`, `PhaseArtifactPayload.AcceptancePlan`, `Probe.as`, `TestArgValue` `x-go-sumtype` variant `PlanRef` + `$defs.PlanRef`, `TestingVariant` enum/varnames gain 5 `TestVariantAcceptancePlan`), `server/internal/resourceaccess/projectstate/projectstateaccess.go:3038-3047` (`applyPhaseArtifactsPayload`), `:8115-8120` (`testingVariantNames` + `"acceptancePlan"`), `:9393-9405` (`deriveVariant`: `N-ATP` prefix), `:12735-12739` (`scopeAndChecks` arm `ms.Phase == "acceptance_plan"` → `rule:atp`), new `AcceptanceContract`, `AcceptanceScenarios`, `AcceptanceResidual`, `AcceptanceRoute`; `server/internal/engine/estimation/estimationengine.go:1781` (`deriveActivities` + N-ATP); `server/cmd/aiarch-state-mcp/scenarioverbs.go` (`listAcceptanceScenarios`), `tools.go:195` (register beside `listComponentScenarios`), `constructverbs.go:428-454` (`recordPhaseArtifact`: `acceptancePlan` forces key `acceptance`), `:649-654` (`testPlanShape` sibling `acceptancePlanShape`); tests `server/internal/acceptance_test.go`, `derivedplan_test.go`, `cmd/aiarch-state-mcp/constructverbs_test.go`

**Interfaces (Produces):**
```go
// methodcheck
type PhaseArtifacts struct {
	TestPlan       map[string]TestPlanRecord `json:"testPlan"`
	UIDesign       map[string]UIDesignRecord `json:"uiDesign"`
	AcceptancePlan *AcceptancePlanRecord     `json:"acceptancePlan,omitempty"` // strict decode as TestPlanRecord (bindings.go:272)
}
type AcceptancePlanRecord struct {
	Principals   map[string]TestPrincipal `json:"principals"`
	Bindings     []ScenarioBinding        `json:"bindings"`               // keyed by variant id "<id>@<client>"
	BrowserActor string                   `json:"browserActor,omitempty"` // the actor whose browser every web step and web probe runs in
	AuthoredAt   string                   `json:"authoredAt,omitempty"`
}
type Probe struct { For, Component, Operation string; Inputs []TestArg; Expect TestExpect; As string `json:"as,omitempty"` } // As: the actor the probe runs as (defaults to the step's)
type PlanRef struct { Component, Scenario string; Seq int; Input string }  // kind "planRef": the literal bound to input Input of step Seq of the client plan Component/Scenario
const TestArgPlanRef TestArgValueKind = "planRef"
func ResolvePlanRef(p Project, v PlanRef) (LiteralValue, bool)        // the named literal; false when the plan, binding, step or input is missing or is not a literal
// rules_acceptance.go (every finding SeverityError)
const (
	ruleATPBound   RuleID = "ATP-BOUND"   // every residual variant (scenario.AcceptanceResidual over ScenarioInput's Client contracts) has exactly one binding; a binding names a residual variant
	ruleATPRef     RuleID = "ATP-REF"     // every planRef resolves (ResolvePlanRef) and the referenced input's schema param equals the referencing one; a planRef inside a client plan (testPlan) is refused
	ruleATPBrowser RuleID = "ATP-BROWSER" // a web step or a probe on a browser surface (env.isWeb) runs as BrowserActor: BrowserActor set, in Principals, and every such step's/probe's actor equals it
	ruleATPProbeAs RuleID = "ATP-PROBE-AS" // Probe.As names a Principals entry; the probe's operation is readOnly (TP-PROBE) on the named component
)
func acceptancePlanFindings(p Project, env tpEnv, all []scenario.Scenario) []Finding // TP-* v2 over each binding via bindingFindings with opLookup by stimulus Input.To, then ATP-*
```
```go
// app projectstate
const AcceptanceContract = "acceptance"
func AcceptanceScenarios(p *Project) ([]scenario.Scenario, error)   // scenario.ForAcceptance(Derive(ScenarioInputOf(p)), ClientContractKeys(p)) filtered to AcceptanceResidual ids
func AcceptanceResidual(p *Project) ([]scenario.Residual, error)
type AcceptanceRoute struct { Scenario string; Components []string; ClientActivity string } // Components: every From/To that is a component across the dynamic-view steps of the variant's path nodes, path order, distinct; ClientActivity: the committed ActivityItem whose ComponentID is the variant's first actor stimulus's To ("" when none)
func AcceptanceRoute(p *Project, variant string) (AcceptanceRoute, error)
// estimation: deriveActivities appends DerivedActivity{Name: "N-ATP", Title: "Use-case acceptance test plan", EffortDays: 5, RiskBucket: defaultRiskFor(5), WorkerClass: "test-engineer", Coding: false, Derived: true} for every non-empty System
```
- `lifecycles.json` gains:
```json
{"type": "testing:acceptancePlan",
 "phases": [{"id": "acceptance_plan", "label": "Acceptance Test Plan", "weight": 100, "gate": "stpReview", "exitCriterion": "Every residual use-case scenario is bound and the ATP-* and TP-* rules pass"}],
 "tasks": [
  {"id": "stp", "kind": "dispatch", "title": "Acceptance Test Plan", "phase": "acceptance_plan", "dependsOn": [], "command": "acceptance-test-plan", "workerClass": "test-engineer", "artifactKind": "ATP"},
  {"id": "stpReview", "kind": "review", "title": "Acceptance Plan Review", "phase": "acceptance_plan", "dependsOn": ["stp"], "reviews": "stp"}]}
```
- `acceptance-test-plan.md`: the `service-test-plan.md` shape (`# /acceptance-test-plan <activityId>`; `**Agent + skills.**` line naming `test-engineer` and `[[the-method-testing]]`, `[[the-method-project-state]]`; exactly one `run_checks` line above `## Steps`); steps: `listAcceptanceScenarios` (the residual with each variant's reason), bind only those `[ATP-BOUND]`, shared step inputs as `{"kind":"planRef","model":{"component","scenario","seq","input"}}` `[ATP-REF]`, a cross-actor observation as a probe `as` that actor `[ATP-PROBE-AS]` (a browser surface's probe is an `OpenList`-style read-only web operation with one `navigate` action and the observables), `browserActor` `[ATP-BROWSER]`, `skip {until: <client activity>}` when the surface's UI design is not committed, `recordPhaseArtifact` mapKey `acceptance` payload `{"acceptancePlan": {...}}`, `publishDraft`, stop. `RenderCommand` takes one `$ARGUMENTS` token (`<activity_id>`): `commandArgs` (`projectstateaccess.go`, used at `:12387`) passes `[activityID]` when `item.ComponentID == ""`.
- `scopeAndChecks`: `case ms.Phase == "acceptance_plan": return stateOnly, []SpecCheck{{ID: "rule:atp", Kind: CheckKindRule, Title: "Acceptance plan rules (ATP-*, TP-*)", Source: SpecCheckSource{RulePrefixes: []string{"ATP-", "TP-"}}, TimeoutSec: ruleCheckTimeoutSec}}, nil`.
- `listAcceptanceScenarios` (read verb, modes `shared`, `ReadOnlyHint`): JSON `{"scenarios": CanonicalJSON(AcceptanceScenarios), "residual": [{scenario, reason: {kind, model}}]}`.

- [ ] **Step 1: Write the failing tests.**
```go
// framework-go/methodcheck/rules_acceptance_test.go (loadProject(t, "atp_clean.json") from scenario_adapter_test.go:14)
func TestATP_CleanFixtureIsClean(t *testing.T)                 // ValidateProject: no ATP-*/TP-* finding
func TestATP_Bound_FiresOnUnboundResidual(t *testing.T)        // atp_unbound.json (no acceptancePlan at all, committed architecture): one ATP-BOUND per residual variant, naming "work-the-list-P1@todoApi" and its reason text "later outcome out-team-member after n1a"
func TestATP_Bound_FiresOnNonResidualBinding(t *testing.T)     // a binding of "come-back-to-an-intact-list-P1@todoWebApp" → ATP-BOUND "not residual: covered by the todoWebApp projection"
func TestATP_Ref_FiresOnDanglingAndOnNonLiteral(t *testing.T)  // atp_badref.json: planRef to seq 9 → ATP-REF; planRef to a step whose input is a ref → ATP-REF "must name a literal"
func TestATP_Ref_RefusedInsideAClientPlan(t *testing.T)        // a testPlan step with a planRef input → ATP-REF
func TestATP_Browser_FiresWhenWebProbeActorDiffers(t *testing.T) // probe on todoWebApp as "team-agent" with browserActor "team-member" → ATP-BROWSER; browserActor "" with a web probe → ATP-BROWSER
func TestATP_ProbeAs_NeedsAPrincipal(t *testing.T)             // as: "nobody" → ATP-PROBE-AS
func TestBindings_PlanRefDecode(t *testing.T)                   // {"kind":"planRef","model":{"component":"todoApi","scenario":"work-the-list-P1","seq":1,"input":"title"}} → PlanRef; ResolvePlanRef over atp_clean → the literal
func TestAcceptancePlan_DecodesStrictly(t *testing.T)           // an acceptancePlan carrying "hook" → DecodeProject error naming TP-HOOK
// method-assets
func TestLifecycles_AcceptancePlanIsOneDispatchAndOneGate(t *testing.T) // LifecycleFor("testing:acceptancePlan"): stp (dispatch, command acceptance-test-plan, test-engineer, ATP) then stpReview; wantLifecycleTypes gains it; ValidateLifecycles clean
// app server/internal/acceptance_test.go (package internal_test; fixture decoded with projectstate.DecodeProjectJSON(conformance.Fixture("todomvc"), "") as taskspec_test.go:546 decodes the own project)
func TestAcceptanceScenarios_TodomvcResidual(t *testing.T)      // AcceptanceResidual over the todomvc fixture: every entry's scenario has use case work-the-list or resume-the-list; contains "work-the-list-P1@todoApi"; none for come-back-to-an-intact-list, drive-the-list-from-agent-tooling, mark-a-todo-completed-or-active
func TestAcceptanceRoute_NamesPathComponentsAndClientActivity(t *testing.T) // AcceptanceRoute(p, "work-the-list-P1@todoApi").Components == [todo-api, list-manager, access-granting-engine, todo-list-access, todo-list-store, list-ruling-engine, message-bus] (as the view's steps order them; pin the exact slice from the fixture) and ClientActivity == "C-todo-api"
func TestTaskSpecForATPIsStateOnlyWithATPRules(t *testing.T)   // TaskSpecFor(p, "N-ATP", "stp") over a project whose slot 9 holds N-ATP: Identity.Command == "acceptance-test-plan", EditScope.StateOnly, checks == [rule:atp]
// server/internal/derivedplan_test.go
func TestDerivedPlan_EmitsNATP(t *testing.T)                     // estimation.NewEstimationEngine().DerivePlan(fweng.Context{}, SystemView{one manager, one client uiSurface}, ActivityListDeltas{}): an activity N-ATP (test-engineer, 5 days, Coding false, Derived true) whose only dependency is M0; an empty SystemView derives no N-ATP
// server/cmd/aiarch-state-mcp/constructverbs_test.go
func TestRecordPhaseArtifact_AcceptancePlanIsKeyedAcceptance(t *testing.T) // mapKey "whatever" + {"acceptancePlan": ...} lands at .phaseArtifacts.acceptancePlan; a refused payload names acceptancePlanShape
func TestListAcceptanceScenarios_ReturnsResidualWithReasons(t *testing.T)
```
- [ ] **Step 2: Run** `cd framework-go && GOWORK=off go test ./methodcheck -run 'TestATP|TestBindings_PlanRef|TestAcceptancePlan_' -v`; `cd ../method-assets && GOWORK=off go test ./ -run TestLifecycles_AcceptancePlan`; app: `cd <app>/server && GOMODCACHE=/Users/davidmarne/.claude/jobs/a7cfb4f6/tmp/gomodcache-app3 GOWORK=off go test ./internal/ -run 'TestAcceptance|TestTaskSpecForATP|TestDerivedPlan_EmitsNATP' && GOWORK=off go test ./cmd/aiarch-state-mcp -run 'TestRecordPhaseArtifact_AcceptancePlan|TestListAcceptanceScenarios'` — Expected: FAIL.
- [ ] **Step 3: Implement.** Platform first (methodcheck types, rules, `emittedRuleIDs` gains the four ids, `scenarioFindings` appends `acceptancePlanFindings`; lifecycle + command + charter tool + `go run ./cmd/gen-stepmanifest`; `TestEveryStepCommandRunsItsChecks`, `TestCommandWritesAreGranted` and `TestTestPlanAndContractPromptsCarryTheirRules` must admit the new command). App edits land on the app branch but are **committed in Task 4.5** against the released platform (until then run the app packages with `GOWORK=<worktree go.work>`, as Plan 3 did). `$defs`: `TestArgValue.x-go-sumtype.variants` gains `{"kind": "planRef", "type": "PlanRef"}`; `make gen-models` regenerates `contract.gen.go` (`newTestArgValueForKind` gains the arm) and the fakes.
- [ ] **Step 4: Run** `GOWORK=off go test ./methodcheck ./scenario` (framework-go; includes `TestDefaultCoverage_EveryAutomatedRuleHasEmitter`), `GOWORK=off go test ./` (method-assets), the two app packages above (workspace mode) — PASS; `golangci-lint run ./...` in framework-go and method-assets — 0.
- [ ] **Step 5: Commit** (platform) `git commit -m "feat(methodcheck,method-assets): acceptancePlan record with planRef and probe-as; ATP-BOUND/REF/BROWSER/PROBE-AS; testing:acceptancePlan lifecycle and /acceptance-test-plan"`. App commit deferred to Task 4.5.

---

### Task 4.3: Per-request principals (local profile)

**Basis (D8):** the allowlisted `X-Dev-Principal` header, `LocalDevPrincipals` and its non-local refusal shipped in Plan 3 Task 3.3 (`httpgen/wiring.go:108-150`, `configgen/emit.go:240-270`; pinned by `TestAuthMiddleware_DevPrincipalHeaderFromAllowlist` and `TestConfig_LocalDevPrincipals`), and `LaunchDevstack` allowlists every plan principal (`scenariohost/devstack.go:93-150`). `TestDevPrincipals_RefusedUnderNonLocalProfile` is therefore `TestConfig_LocalDevPrincipals` (`configgen/devprincipals_test.go:93`) and is not re-written. This task delivers what the acceptance runs still lack.

**Files:**
- Modify: `framework-go-app-generator/testgen/tsruntime/runtime.ts:58-64` (`ContractCall.as`), `:120-123` (`headers(as?)`), `:175-190` (`callContract` passes `as`); create `mcpCall`; test `testgen/emit_acceptance_test.go` (Task 4.4 compiles the runtime; here `TestRuntime_TypeChecks` runs `npx tsc --noEmit` over `tsruntime` as Plan 2 Task 2.10 did)
- Modify: `framework-go-scenariohost/cmd/webscenarios/main.go:235-262` (`startReal`), `:283-320` (`surfacePrincipals` → `planPrincipals`); test `cmd/webscenarios/main_test.go`
- Create: `framework-go-conformance/acceptance_test.go` (tag `conformance`; the file Task 4.5 extends)

**Interfaces (Produces):**
```ts
// runtime.ts
export type ContractCall = { component: string; operation: string; verb: string; path: string; args: Record<string, unknown>; expect: Expectation; as?: string }; // as: the subject sent as X-Dev-Principal (absent: the launch's process principal)
function headers(as?: string): Record<string, string>;                 // as ?? process.env.AIARCH_DEV_PRINCIPAL
export type ToolCall = { tool: string; args: Record<string, unknown>; expect: Expectation; as?: string };
export async function mcpCall(testInfo: TestInfo, call: ToolCall): Promise<unknown>; // POST <baseURL>/mcp: initialize (protocolVersion "2025-06-18", the same handshake devstack.Smoke makes, smoke.go:202-245), notifications/initialized, tools/call; keeps Mcp-Session-Id; isError → expect.fail with the tool text; returns structuredContent ?? the text
```
```go
// cmd/webscenarios
func planPrincipals(module, config string) (principals []scenariohost.Principal, browser string, err error) // config under generated/acceptance/: phaseArtifacts.acceptancePlan.{principals, browserActor}, the browserActor's principal first (an acceptance plan with web specs and no browserActor is an error naming ATP-BROWSER); any other config: the surface plan's principals as today, browser = the first by name
```
- `startReal` launches with `planPrincipals` and sets `AIARCH_DEV_PRINCIPAL` to `browser`'s subject (the SPA's own fetches carry no header, so the browser identity is the process principal).

- [ ] **Step 1: Write the failing tests.**
```go
// cmd/webscenarios/main_test.go
func TestWebscenarios_AcceptancePrincipalsBrowserActorFirst(t *testing.T) // module with phaseArtifacts.acceptancePlan{principals: team-agent(agent-1), team-member(alice), browserActor: team-member} and -config generated/acceptance/playwright.config.gen.ts → the stubbed launch sees [alice, agent-1] and AIARCH_DEV_PRINCIPAL=alice; without browserActor and with a spec file under generated/acceptance → error mentions ATP-BROWSER
// framework-go-conformance/acceptance_test.go  //go:build conformance
func TestDevPrincipals_PerRequestHeaderOnlyWhenAllowlisted(t *testing.T)  // built fixture with impl (harvested(t) from clients_test.go:52): h := scenariohost.Start(t); app := h.LaunchDevstack(t, dir, Principal{Subject: "alice"}, Principal{Subject: "agent-1", Roles: []string{"agent"}, OnBehalfOf: "alice", Intents: ...}); raw GET of todoApi's OpenList route with header X-Dev-Principal: agent-1 → 200 and the list is alice's; header mallory → 401 body "dev principal not allowed"; no header → served as alice (the process principal)
```
`testgen/emit_acceptance_test.go`: `TestRuntime_TypeChecks` (`npx tsc --noEmit -p` the tsconfig Plan 2 Task 2.10 used for `tsruntime`).
- [ ] **Step 2: Run** `cd framework-go-scenariohost && GOWORK=off go test ./cmd/webscenarios -run TestWebscenarios_AcceptancePrincipals`; `cd ../framework-go-app-generator && GOWORK=off go test ./testgen -run TestRuntime_TypeChecks` — FAIL (`mcpCall` undefined once Task 4.4's spec imports it; `planPrincipals` undefined).
- [ ] **Step 3: Implement** `runtime.ts`, `planPrincipals`, `startReal`.
- [ ] **Step 4: Run** the two packages — PASS; `cd ../framework-go-conformance && GOWORK=off go test -tags conformance ./ -run TestDevPrincipals_PerRequestHeaderOnlyWhenAllowlisted -timeout 60m` — PASS (needs docker, temporal CLI, npm).
- [ ] **Step 5: Commit** `git commit -m "feat(tsruntime,webscenarios): per-call X-Dev-Principal from the browser runtime (as); mcpCall; acceptance plan principals with the browser actor first"`.

---

### Task 4.4: M‑ACCEPT milestone, `job` task kind, gate, routing

**Files:**
- Modify: `method-assets/lifecycles.json` (new lifecycle `milestone:acceptance`), `lifecycles.go:30-34` (`LifecycleTaskJob`), `:292-304` (`checkTaskFields`), `taskspec.go:35-60` (`MethodSpecFor` job arm), `lifecycles_test.go:15-18, 256-280, 336-357` (type set; `LinearTypes` admits a job at an even index; `CommandsAreDispatchable` skips jobs), `assets/workflows/aiarch-construct.yml.tmpl` (`Resolve the task spec` emits `kind`; agent-only steps conditioned)
- Modify: `framework-go-app-generator/testgen/testgen.go:133-170` (`Generate` emits the acceptance half), `emit_mcp.go:182-273` (`anonymousPrincipal`, `toolDialect` reused per step with a session per (client, actor)); create `testgen/emit_acceptance.go` (routing: Go when no web step/probe, else Playwright), `emit_acceptance_playwright.go`, `emit_acceptance_http.go`, `emit_acceptance_mcpwire.go`; test `emit_acceptance_test.go`
- App: `.aiarch/state/project.json` `$defs` (`ActivityType` enum/varnames gain 11 `ActivityTypeMilestone`; `TestingVariant` gains 6 `TestVariantAcceptance`; both `ActivityTaskKind` defs gain `"job"`/`ActivityTaskJob`), `server/internal/resourceaccess/projectstate/projectstateaccess.go:8072-8084` (`activityTypeNames` + `"milestone"`), `:8115-8120` (`"acceptance"`), `:9420-9435` (`milestoneActivityTypes = {"M-ACCEPT": {ActivityTypeMilestone, TestVariantAcceptance}}` checked after `designActivityType`), `:9995-10000` (`LifecycleKeyFor`: `t == ActivityTypeTesting || t == ActivityTypeMilestone`), `:12216` (`TaskSpecSchemaVersion = 6`), `:12281` (`SpecIdentity.Kind` comment `dispatch | review | job`), `:12727-12768` (`scopeAndChecks` job arm), `:12670` (`specModel` unchanged: no command-less? a job has a command — return `SpecModel{}` when `ms.Kind == job`); `server/internal/engine/estimation/estimationengine.go:1781` (M-ACCEPT activity), `:2234-2275` (`applyDeltas`: `acceptanceDependency` before the sink); `server/internal/manager/delivery/deliveryactivity.go:270-305` (`strategySlotJob`, `jobTaskStrategy`), `:1902-1925` (`strategySlotFor` job arm), `:2747-2756` (`evidenceHoldsTheGate` + `acceptanceGateHeld`), `:6667-6690` (`acceptanceGate`, `lifecycleRunsAcceptance`), `:6908-6990` (`acceptanceGateHeld`, `holdAcceptanceGate`), `deliverymanager.go:10880` (`ActivityTaskKind(t.Kind)` admits job), `csWorkflows` hooks (`AcceptanceScenarios func(*projectstate.Project) ([]scenario.Scenario, error)` beside `Scenarios`); `server/internal/resourceaccess/agenticjob/agenticjobaccess.go:1830-1960` (`dispatch`: job arm before the claude lookup), `:2171-2195` (`runJob` beside `gateWithoutAgent`), `:2251` (`resolveTaskSpec` reads `identity.kind`); `server/cmd/aiarch-state-mcp/checks.go:402-440` (`newScenarioBatch` → `scenarioBatches`), `:440-490` (`runScenarios` loops batches); `server/cmd/gen-lifecycles/main.go:182` (`LifecycleTaskKind` union + `'job'`), `webApp/src/components/activity/lifecycleGraphTypes.ts:20`, `LifecycleGraph.tsx:123-126, 450` (`KIND_LABEL.job = 'Venue job'`; the icon by an exhaustive `switch (n.kind)` with `default: assertNever(n.kind)`); create `server/internal/acceptance/doc.go`; tests `server/internal/{jobtask_test.go,derivedplan_test.go,acceptance_test.go}`, `cmd/aiarch-state-mcp/checks_test.go`

**Interfaces (Produces):**
```json
{"type": "milestone:acceptance",
 "phases": [{"id": "acceptance", "label": "Acceptance", "weight": 100, "gate": "testing", "exitCriterion": "An acceptance run at the current composed revision covers every residual use-case scenario, all pass, and the results render"}],
 "tasks": [
  {"id": "integration", "kind": "job", "title": "Acceptance run", "phase": "acceptance", "dependsOn": [], "command": "acceptance-run", "artifactKind": "AcceptanceRun"},
  {"id": "testing", "kind": "review", "title": "Acceptance", "phase": "acceptance", "dependsOn": ["integration"], "reviews": "integration"}]}
```
```go
// method-assets
const LifecycleTaskJob = "job"        // a venue job: the task spec's checks run and record a CheckRun; no agent, no prompt, no workerClass; command is the venue's routing slug
// checkTaskFields: job needs a command and an artifactKind and must not carry a workerClass; MethodSpecFor returns the identity (Kind "job", Command, ArtifactKind, ExitCriterion) before ManifestFor
// app projectstate
const ActivityTypeMilestone ActivityType = 11   // wire "milestone"
const TestVariantAcceptance TestingVariant = 6  // wire "acceptance"; LifecycleKeyFor(Milestone, Acceptance) == "milestone:acceptance"
// scopeAndChecks, case ms.Kind == methodassets.LifecycleTaskJob:
//   scope stateOnly; checks = atpRuleCheck() + bootChecks(p.ModuleDir) + commandCheck("gen-tests-check", ...) + one SpecCheck{ID: "scenario:acceptance:<variant>", Kind: CheckKindScenario, Source: {Scenario: variant, Contract: AcceptanceContract, UseCase}} per AcceptanceScenarios(p); sortChecks
// estimation
// deriveActivities appends DerivedActivity{Name: "M-ACCEPT", Title: "Use-case acceptance (M-ACCEPT)", EffortDays: 0, RiskBucket: 1, WorkerClass: "system-architect", Coding: false, Derived: true}
func acceptanceDependency(acts []DerivedActivity, deps []NetworkDependency, ms []NetworkMilestone) NetworkDependency // {Activity: "M-ACCEPT", DependsOn: sorted(networkTerminals(acts without M-ACCEPT, deps, ms) ∪ {"N-ATP"})}; applyDeltas appends it to allDeps before projectEndMilestoneFor, so MEND.DependsOn == ["M-ACCEPT"]
// delivery
const strategySlotJob = "job"
type jobTaskStrategy struct{ wf *csWorkflows }
func (s jobTaskStrategy) Produce(ctx workflow.Context, tc taskContext) (producedSubject, error) // carryWalkFeedback; attemptID, obs, err := wf.dispatchConstructionOnce(ctx, tc); err → {AttemptID, OutcomeFailed, Detail}, err; proj := readActivityBranchProject; run, ok := selectCheckRun(&proj, activityID, integrationTaskID, "", []string{attemptID}, true); ok → {AttemptID, OutcomePassed, AttemptRecorded: true, EpisodeID: episodeIDOf(obs), Detail: "acceptance run " + run.ID + " recorded"}; !ok → {AttemptID, OutcomeFailed, AttemptRecorded: true, Detail: dispatchJobFailedDetail(jobLabelConstruction, tc.Task.ID, obs)}, nil
func acceptanceGate(lc methodassets.Lifecycle, t methodassets.LifecycleTask) bool // t.Kind == review && t.ID == "testing" && some task of lc has ArtifactKind == "AcceptanceRun"
func (wf *csWorkflows) acceptanceGateHeld(ctx, in, lc, t, ws, state, gate) (bool, error) // revision := judgedSubject(ws, t).AttemptID; proj := readActivityBranchProject; residual := wf.AcceptanceScenarios(&proj); run, found := selectCheckRun(&proj, activityID, integrationTaskID, "", []string{revision}, false); ev := testingGateEvidence{Contract: AcceptanceContract, Projected: len(residual)} (+ Run/Files/IndexErr from joinTestRun as readTestingGateEvidence does); reason := testingGateHold(ev, revision); "" → gate.checkRunID = run.ID, false; else holdAcceptanceGate
func (wf *csWorkflows) holdAcceptanceGate(ctx, in, t, state, gate, ev, reason string, proj *projectstate.Project) error // the testing hold's verdict + ONE change-request comment anchored at the run whose Text is reason + "\n\n" + per failing scenario (status != pass, Missing included): "<variant>: components on path: a, b, c — route: OverrideActivity(<ClientActivity>, retry): a new attempt on its integration task from a fresh branch off main" (projectstate.AcceptanceRoute); a variant with no ClientActivity says "no client activity derives for <client>"
```
- `strategySlotFor`: `case methodassets.LifecycleTaskJob: if t.Command == "" || t.ArtifactKind == "" → FailedPrecondition; return strategySlotJob`; the registry gains `strategySlotJob: jobTaskStrategy`.
- Local executor: `dispatch()` reads the resolved spec's `identity.kind` (`resolveTaskSpec` parses it as it parses `model.id`); for `job` it registers the gate, skips `seatPromptSurface`/claude, and runs `go a.runJob(run, completionScope{...})`: `runGates` (edit-scope passes on an untouched branch; `run-checks --gate`; `recordCheckRun`), then `run.status = localRunSucceeded` iff every gate is green, else `localRunFailed` with the diagnostic; the `a.claudeBin == ""` refusal moves below the job arm.
- GH template: `Resolve the task spec` adds `kind="$(jq -er .identity.kind spec.json)"` and `echo "kind=${kind}" >> "$GITHUB_OUTPUT"` and skips the model/builtin shape checks for a job; `Materialize the .claude prompt surface`, `Write the aiarch-state MCP config`, `Generate the component's models`, `Run the task's generators` and `Run construction step` gain `if: ${{ steps.spec.outputs.kind != 'job' }}` (the models step keeps its `contracts != ''` conjunction); `Generate the scenario tests` keeps `integration == 'true'` (a job's task id is `integration`, so the acceptance tests regenerate); the gate, upload, record and `Fail on a red check` steps are unchanged. The dispatch's `component_id` for a job is the activity id (the input is required and validated as an id; nothing reads it for a job: `pipelineSpec.ComponentID` falls back to `ActivityID` when empty, in `submitConstructionJob`).
- testgen: `Generate` after `routePlans`: when `p.PhaseArtifacts.AcceptancePlan != nil`, `emitAcceptance(&out, cfg, web, p, all)`: residual := `scenario.AcceptanceResidual(all, clientKeys(p))`; for every binding (ATP-BOUND holds, so every residual variant has one): `acceptancePlan{Key: "acceptance", Scenarios: bound}` where each step's `Op` resolves on the contract of its stimulus's `Input.To`, `planRef` inputs inlined through `methodcheck.ResolvePlanRef`; a variant with a web step or a web probe (`env.isWeb` on the step's `Input.To` / the probe's component) is emitted by `emit_acceptance_playwright.go` into `uitests/generated/acceptance/<variant>.spec.gen.ts` (+ `playwright.config.gen.ts` over `playwrightResultsDir("acceptance")`, `runtime.gen.ts`; elements imported from `../<surface>/elements.gen` per surface); every other variant into `internal/acceptance/acceptance_scenarios.gen_test.go` (package `acceptance_test`, `TestMain(m) { scenariohost.Main(m, "acceptance") }`, `h.RunScenario(t, "acceptance", variant, ...)`, `app := h.LaunchDevstack(t, "", <every plan principal>)`, one `scenariohost.MCPSession`/`RESTClient` per (client, actor) the steps and probes use — `emit_acceptance_mcpwire.go` and `emit_acceptance_http.go` are the two step dialects, each a `stepDialect` (`emit_steps.go:14-21`) whose `stepCall` names the session of the step's client and actor and whose `probeBody` runs a probe `as` its actor over that actor's session of the probe's client (a non-client probe component is called in-process as today)). In a Playwright acceptance spec a non-web step is `await mcpCall(testInfo, { as: principals['team-agent'], tool, args, expect })` or `await contractCall(testInfo, { ..., as })`, a web step is `specWriter.step`, and a web probe is `navigate` + the probe's observables (`observableLine`), all as the browser actor; a web probe `as` an actor other than `browserActor` is a generation error naming ATP-BROWSER. Acceptance variants are not null-discriminated (E22).
- Check runner: `scenarioBatches(r, proj, contract, checks) []scenarioBatch` — one batch as today for a contract; for `AcceptanceContract` up to two: the Go batch (`dir` module, `./internal/acceptance/`, `iface` "Acceptance", index `test-results/acceptance`) over the variants with no `uitests/generated/acceptance/<variant>.spec.gen.ts`, and the web batch (`WebScenariosArgv(moduleDir, "acceptance", "-grep", ...)`, index `uitests/test-results/acceptance`) over the rest; `runScenarios` runs each and reads each batch's index for its own ids.

- [ ] **Step 1: Write the failing tests.**
```go
// method-assets/lifecycles_test.go
func TestLifecycles_AcceptanceIsAJobAndItsReview(t *testing.T)      // LifecycleFor("milestone:acceptance"): tasks [integration (kind job, command acceptance-run, artifactKind AcceptanceRun, no workerClass), testing (review, reviews integration)]; ValidateLifecycles clean; a job with a workerClass or without an artifactKind is a ValidateLifecycle defect
func TestMethodSpecForJobHasNoPrompt(t *testing.T)                   // MethodSpecFor("milestone:acceptance", "integration", nil): Kind "job", Command "acceptance-run", Prompt/Charter/Agent/Model ""
func TestConstructTemplate_JobSkipsTheAgent(t *testing.T)            // the rendered construct template (as construct_injection_test.go renders it): each of the five agent-only steps carries `steps.spec.outputs.kind != 'job'`; "Run the checks" and "Record the check run" do not
// framework-go-app-generator/testgen/emit_acceptance_test.go (fixture: the todomvc project.json read through the conformance module's testdata by relative path, as composegen tests do, with an acceptancePlan added in-test: work-the-list-P1@todoApi (REST step from planRef todoApi/work-the-list-P1/1; probe for out-team-member on todoWebApp.OpenList as team-member with a navigate action and a `text` observable; browserActor team-member) and resume-the-list-P1@todoApi (probe for out-team-member on listManager.OpenList as team-member — no web → Go))
func TestAcceptance_WebBearingVariantIsAPlaywrightSpec(t *testing.T) // Generated has uitests/generated/acceptance/work-the-list-P1@todoApi.spec.gen.ts containing `await contractCall(testInfo, {` with `as: principals['team-agent']` and a `page.goto` + `toHaveText` line for the probe; no Go test for it
func TestAcceptance_MachineOnlyVariantIsGo(t *testing.T)             // internal/acceptance/acceptance_scenarios.gen_test.go has TestScenario_resume_the_list_P1_todoApi, `scenariohost.Main(m, "acceptance")`, `h.RunScenario(t, "acceptance", "resume-the-list-P1@todoApi"`, a RESTClient as team-agent and, for the probe, an MCP or in-process call as team-member
func TestAcceptance_PlanRefInlinesTheLiteral(t *testing.T)            // the REST step's args carry the client plan's literal, not a planRef
func TestAcceptance_WebProbeAsOtherActorRefused(t *testing.T)         // browserActor team-member, a web probe as team-agent → Generate error names ATP-BROWSER and the variant
func TestAcceptance_GeneratedGoCompiles(t *testing.T)                 // the compile harness of compile_harness_test.go over the acceptance file
// app server/internal/jobtask_test.go
func TestTaskSpecForJobHasNoAgent(t *testing.T)                       // TaskSpecFor(p, "M-ACCEPT", "integration") over todomvc with slot 9 holding M-ACCEPT (ComponentID ""): Identity.Kind == "job", Prompt.Text == "", Model.ID == "", Tools empty, EditScope.StateOnly; checks start rule:atp, then command:build-server, command:build-devstack, command:gen-tests-check, smoke:composition, then one scenario:acceptance:<variant> per AcceptanceScenarios(p), sorted; TaskSpecSchemaVersion == 6
func TestClassifyActivity_MilestoneAndATP(t *testing.T)               // ClassifyActivity("M-ACCEPT", "system-architect", false, false, false) == (ActivityTypeMilestone, TestVariantAcceptance); LifecycleKeyFor == "milestone:acceptance"; ClassifyActivity("N-ATP", "test-engineer", false, false, false) == (ActivityTypeTesting, TestVariantAcceptancePlan) → "testing:acceptancePlan"; both keys resolve with methodassets.LifecycleFor
func TestPump_JobTaskRunsWithoutAgent(t *testing.T)                    // NewLocalExecAgenticJobAccess(repo, project, stateMCPBin, version, timeout) with PATH holding no claude, over a seeded repo whose main carries the todomvc state with slot 9 + an acceptancePlan (composedRepo of freshcomposition_test.go:227 extended): SubmitAgenticJob of a lifecycle dispatch {activity_id M-ACCEPT, task_id integration, command acceptance-run, spec_hash of TaskSpecFor} is accepted; ObserveAgenticJob reaches a terminal phase; the branch activity/M-ACCEPT holds a CheckRun{Activity: "M-ACCEPT", Task: "integration", Revision: the dispatched revision} whose results name every scenario:acceptance:<variant> check; the run's phase is Succeeded iff every result passed
// server/internal/derivedplan_test.go
func TestDerivedPlan_MAcceptPrecedesMEND(t *testing.T)                // DerivePlan over SystemView{manager, engine, client uiSurface, vendor resource}: activity M-ACCEPT (EffortDays 0, Derived) depends on every other terminal (C-<manager>... U-SPA-<client>, R-<resource>) and on N-ATP; the MEND milestone's DependsOn == ["M-ACCEPT"]; an additive activity with no dependents joins M-ACCEPT's DependsOn
// server/internal/acceptance_test.go (viewManager from delivery_views_test.go:41 with the todomvc project + slot 9 holding M-ACCEPT)
func TestAcceptanceGate_RequiresRunAtComposedRevision(t *testing.T)   // withCheckRun(p, CheckRun{Activity: "M-ACCEPT", Task: "integration", Revision: "M-ACCEPT:integration:1", Results: one pass per residual variant under scenario:acceptance:}): QueryProjectView testRun for M-ACCEPT with revision 2 (withRevision) is NotFound / the view's TestRuns[0].Run.Revision != the asked attempt; with the matching revision the view is complete (Missing, Unknown empty, Contract "acceptance")
func TestAcceptanceGate_HoldNamesPathComponents(t *testing.T)         // a CheckRun whose scenario:acceptance:work-the-list-P1@todoApi result is fail: the DeliveryTestRunView for "acceptance" marks it failed and AcceptanceRoute(p, that id) names [todo-api, list-manager, ...] and ClientActivity "C-todo-api"; the hold text (holdAcceptanceGate's comment, pinned through the venue-free compose in testingGateHold's "did not pass — failed: work-the-list-P1@todoApi" + AcceptanceRoute) carries both
// server/cmd/aiarch-state-mcp/checks_test.go
func TestScenarioBatches_SplitAcceptanceByGeneratedFile(t *testing.T) // a module dir with internal/acceptance/acceptance_scenarios.gen_test.go and uitests/generated/acceptance/a@web.spec.gen.ts: checks for a@web and b@api → two batches: the Go batch argv runs ./internal/acceptance/ with -run over TestScenario_b_api, index test-results/acceptance; the web batch argv == WebScenariosArgv(moduleDir, "acceptance", "-grep", webScenarioGrep([a@web])); a plain contract still yields one batch
```
- [ ] **Step 2: Run** `cd method-assets && GOWORK=off go test ./ -run 'TestLifecycles_AcceptanceIsAJob|TestMethodSpecForJob|TestConstructTemplate_JobSkips'`; `cd ../framework-go-app-generator && GOWORK=off go test ./testgen -run TestAcceptance_`; app (workspace mode until Task 4.5): `go test ./internal/ -run 'TestTaskSpecForJob|TestClassifyActivity_Milestone|TestPump_JobTask|TestDerivedPlan_MAccept|TestAcceptanceGate_' && go test ./cmd/aiarch-state-mcp -run TestScenarioBatches_` — Expected: FAIL.
- [ ] **Step 3: Implement** in the order method-assets (kind, lifecycle, `MethodSpecFor`, template, `lifecycles_test.go` amendments, `go run ./cmd/gen-stepmanifest` unchanged) → testgen (emitters, runtime import of `mcpCall`) → app (`$defs` + `make gen-models` + `make gen-client` + `make gen-lifecycles`; projectstate; estimation; delivery strategy/gate/hooks; local executor; check runner; webApp `switch`/`assertNever`; `internal/acceptance/doc.go`). `gochecksumtype` holds every `switch` over `ActivityTaskKind`/`LifecycleTaskKind` consumers in Go (`webApp` by `assertNever`). If `roundArtifactKind`'s lower-cased `acceptanceRun`/`atp` is refused by an `ArtifactKind` codec (`grep -n 'artifactKindNames' server/internal/resourceaccess/projectstate/projectstateaccess.go`), add both wire names there in the same change.
- [ ] **Step 4: Run** `GOWORK=off go test ./` (method-assets), `GOWORK=off go test ./testgen ./cmd/testgen` (app-generator), app packages above + `cd server && GOWORK=off go test ./internal/ ./cmd/aiarch-state-mcp` (workspace mode) — PASS; `golangci-lint run ./...` in method-assets, app-generator and (app) `make lint` — 0; `cd webApp && npm run typecheck && npx eslint src` — clean.
- [ ] **Step 5: Commit** (platform) `git commit -m "feat(method-assets,testgen): job task kind and milestone:acceptance; acceptance tests emitted per residual variant (Go over the wire, Playwright with mcpCall/contractCall as); construct workflow skips the agent for a job"`. App commit deferred to Task 4.5.

---

### Task 4.5: Release, pin, proof

**Platform release (literal release step):** at the fpc/main commit holding 4.1–4.4 tag `framework-go/v0.24.0`, `framework-go-app-generator/v0.26.0`, `framework-go-scenariohost/v0.7.0`, `method-assets/v0.26.0` (its `go.mod.tmpl`/`go.sum.tmpl` pin app-generator v0.26.0 and scenariohost v0.7.0 first; the scaffold go.sum is regenerated against the real proxy AFTER the app-generator and scenariohost tags are pushed — lesson from Plans 2–3 — so method-assets is tagged in a second step), `framework-go-conformance/v0.3.0`; `git push origin fpc/main --tags` — if the push is refused record `git push origin fpc/main --tags` under blockedActions; the lead pushes. Release gate: Platform Checks green with `.github/workflows/platform-checks.yml:102` run regex gaining `|TestConformance_Acceptance$|TestDevPrincipals_PerRequestHeaderOnlyWhenAllowlisted`.

**Files (platform):**
- Create: `framework-go-conformance/testdata/todomvc/impl/webApp/src/todo-web-app/{main.tsx,routes/App.tsx,components/Footer.tsx,components/NewTodoInput.tsx,components/StatusStrip.tsx,components/TodoItem.tsx,hooks/useHashView.ts,hooks/useTodoList.ts}` (verbatim from the harvest, D9; imports adapted to the scaffold as `impl/` Go files were, README records every edit)
- Modify: `testdata/todomvc/project.json` (`.phaseArtifacts.acceptancePlan`: principals `team-member` alice / `team-agent` agent-1 (onBehalfOf alice, the seven intents), `browserActor: "team-member"`, one binding per residual variant of `work-the-list` and `resume-the-list`: the `@todoApi`/`@todoMcpServer` variants step through the client plan by `planRef` and probe `out-team-member` on `todoWebApp.OpenList` as team-member (navigate `#/` + the list's `text`/`count` observables), the `@todoWebApp` variants step through the web plan and probe `out-team-agent` on `todoMcpServer.ListOpenList` as team-agent; slots 9/10 gain `N-ATP` and `M-ACCEPT` and MEND depends on M-ACCEPT, as the app's derivation now emits them), `README.md` (§"Acceptance plan and the harvested SPA (FPC plan 4)")
- Modify: `framework-go-conformance/acceptance_test.go` (tag `conformance`)

**Files (app):**
- `server/go.mod`: pins → framework-go v0.24.0, app-generator v0.26.0, scenariohost v0.7.0, method-assets v0.26.0, conformance v0.3.0; `GOWORK=off go mod tidy`.
- Everything Task 4.2 and 4.4 staged on the app branch (`$defs`, regenerated code, projectstate, estimation, delivery, agenticjob, checks, gen-lifecycles, webApp).
- `.aiarch/state/project.json`: slots 9/10 re-materialized with N-ATP and M-ACCEPT (`make derived-plan-write` is a dead target — `TestWriteDerivedPlan` no longer exists — so the re-materialization goes through the Manager's own staging seam as that target documents, run as a one-off test under `server/internal/` or through the design rail's MCP verbs; never a hand edit); `ruleWaivers` gains `ATP-BOUND` (expires 2026-12-31: the own residual is unbound until the scheduled own-plan rebinding, ruling 8).
- `Makefile.scenarios.mk` (app root) re-seated from the released template if the template changed (it does not in this plan).

**Proof (platform, `framework-go-conformance/acceptance_test.go`, tag `conformance`):**
```go
func TestConformance_Acceptance(t *testing.T) // built fixture with impl (harvested(t)); the fixture's residual (AcceptanceResidual over the fixture's methodcheck project) is non-empty and every residual variant has a binding (ATP-BOUND clean under ValidateProject); the Go half: `go test ./internal/acceptance/ -run 'TestScenario_'` passes with a results.json verdict per Go variant; the web half: `go tool webscenarios -C uitests -config generated/acceptance/playwright.config.gen.ts -project scenarios` passes with a verdict per web variant (the harvested SPA shows the agent's REST/MCP write after a navigate); DockerLeftovers empty
func TestConformance_Acceptance_RedRoutesToTheClient(t *testing.T) // the same build with the harvested TodoItem.tsx mutated in memory so the completed state is never rendered (checked={false}); the web variant work-the-list-P1@todoApi whose probe asserts the marked todo is checked FAILS; recording that run through `aiarch-state-mcp record-check-run` is the app's (below); here the proof is the verdict file naming the variant as fail and AcceptanceRoute(fixture, variant) (via the app) naming todo-web-app among the components and C-todo-api as the client activity
```
The app's half of the red proof: `server/cmd/aiarch-state-mcp/acceptancecheck_test.go` `TestAcceptanceJob_RedRunHoldsAndNamesTheRoute` — over the composed repo (`composedRepoFrom` of `freshcomposition_test.go:233` with the fixture's acceptancePlan and slot 9), `run-checks --gate` for M-ACCEPT/integration with the mutated SPA → `results.json` fails `scenario:acceptance:work-the-list-P1@todoApi`; `record-check-run` lands the CheckRun; `viewManager(p)`'s test-run view for M-ACCEPT marks the variant failed, and `projectstate.AcceptanceRoute` names `todo-web-app` on the path and `C-todo-api` as the route.

- [ ] **Step 1: Release** the platform as above (tags local; push or record). Before tagging: `cd framework-go-conformance && GOWORK=off go test -tags conformance ./ -run 'TestConformance_Acceptance|TestDevPrincipals_PerRequestHeaderOnlyWhenAllowlisted' -timeout 100m` — PASS, and `GOWORK=off go test -tags conformance ./ -run 'TestConformance_(Chain|Boot|ClientsOverWire|Discriminate_Web)$' -timeout 115m` — still PASS with the harvested SPA seated (the browser scenarios that were TP-BROKEN against the scaffold may now pass: `TestConformance_Discriminate_Web` pins which are broken — re-pin from the run and record the new count in the README).
- [ ] **Step 2: Bump pins + tidy;** `cd <app>/server && GOMODCACHE=/Users/davidmarne/.claude/jobs/a7cfb4f6/tmp/gomodcache-app3 GOWORK=off go build ./... && GOWORK=off go vet ./...`.
- [ ] **Step 3: App edits** (schema → `make gen-models`, `make gen-client`, `make gen-lifecycles`; the Task 4.2/4.4 code; slots 9/10; waiver; `make gen-tests` — the own project records no acceptancePlan, so no acceptance file is generated, and `gen-tests-check` stays green).
- [ ] **Step 4: Run** `GOWORK=off go test -count=1 -p 1 ./internal/ ./cmd/aiarch-state-mcp` — PASS (`TestTaskSpecForJobHasNoAgent`, `TestPump_JobTaskRunsWithoutAgent`, `TestDerivedPlan_MAcceptPrecedesMEND`, `TestAcceptanceGate_*`, `TestAcceptanceJob_RedRunHoldsAndNamesTheRoute`, `TestConformanceFixture_RecordsThroughVerbs` with the fixture's acceptancePlan recorded through `recordPhaseArtifact`); then the plan's full suite once: `cd server && GOWORK=off go test -p 1 -timeout 180m ./...`, `make method-check`, `make gen-tests-check`, `make gen-models-check`, `make gen-lifecycles-check`, `make test`; `cd webApp && npm run typecheck && npx eslint src` — all green; `TestAdmit_PassesOwnProject` green with the own derived plan carrying `N-ATP` and `M-ACCEPT`.
- [ ] **Step 5: Commit** (app) `git commit -m "feat(app): use-case acceptance — N-ATP and M-ACCEPT derived, job task kind run by the venue, acceptance plan and gate with routing; pins Plan 4 release"`.

**Earmarks (not in this plan):** E21 step-granular failure attribution in a scenario verdict (the route names the first actor stimulus's client; a failing probe on another surface is named only as a component on the path). E22 a null-subject run over the acceptance variants (TP-DISCRIMINATE covers the client plans only). E23 `derived-plan-check`/`derived-plan-write` are dead Makefile targets since the delivery hand tests left under ARCH-TEST-COMPOSE; the own slots 9/10 re-materialization needs a sanctioned writer. E24 one browser identity per launch: a residual variant whose web steps and web probes belong to two actors is refused (ATP-BROWSER); a second browser context as a second allowlisted principal needs the SPA to send `X-Dev-Principal`.

## Done when
Platform + app released and pinned; `TestConformance_Acceptance` green; own project: `N-ATP` and `M-ACCEPT` appear in the derived plan (drift gate amended via MCP in the same change, GC‑5).
