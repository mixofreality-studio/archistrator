# FPC Plan 2 — Declarative Test Model, Derivation Integrity, Semantic UI Dialect

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Agents can no longer write test code or a vacuous test record. Every scenario the use cases imply reaches the actor; every binding is a schema-complete record of sum-typed values that the platform turns into tests which assert, before and after, what the user sees; a null subject proves every scenario can fail.

**Architecture:** Three layers change together in one platform release. (1) `framework-go/scenario` attaches actor-visible outcomes to the actor's stimulus and refuses silent drops. (2) The binding model in `methodcheck/project.go` and the app's `projectStateAccess` `$defs` loses `Hook`/`Unobservable` and gains sum-typed `TestArgValue`, `ArrangeCall`, `UIAction`, `UIObservable`, `UITarget`, plus `TestPrincipals` and a typed `UIDesignRecord`; methodcheck rules make every incomplete record an Error. (3) `testgen` emits all test code including subject construction (from a generated `scenariocompose` package), principals, `$ref` threading, arrange, pre/post differential and a null-subject run; the Playwright emitter locates by role/name from a generated element inventory and asserts Layer A immediately under an injected latency.

**Tech Stack:** Go (modelgen `x-go-sumtype` → sealed interfaces + `gochecksumtype`), TypeScript/Playwright 1.x (discriminated unions, `assertNever`), methodcheck rules, scenariohost, method-assets prompts.

**Spec:** `2026-11-06-first-pass-correctness-design.md` §W2, §W3, §W4, §4b (immediacy from action kind; web null = server op; single substrate set), §6 rulings 2, 3, 8, 10. Index `fpc-0-index.md` GC‑1/GC‑2 govern every type here.

**Depends on:** Plan 1 (sum-type emitter pinned; conformance chain to prove this plan at $0).

## Global Constraints (plan-local)

- **Wire form of every sum type** is modelgen's envelope: `{"kind": "<variant>", "model": {...}}`. The same shape is used in `project.json`, in the methodcheck mirror types, and in TypeScript. No ad-hoc `oneOf` without `x-go-sumtype`.
- **Rule severity under ruling 8:** every new rule is Error. The app's `applyGateSeverityPolicies` downgrades a rule to Warning only when `project.ruleWaivers[]` names it with an unexpired `expires` (Task 2.11). Archistrator's own project gets waivers dated 2026-12-31 for the rules Task 2.11 lists.
- **No prompt carries a rule.** Each prompt edit in Task 2.12 cites the rule id that enforces the sentence.

## Review Focus

1. **Actor-decided decision with no step** (RUN-021 Escape path): Task 2.3 `TestCC_DecidedByActorStep_Fires` + Task 2.2 `TestDerive_UnaddressedIsReported`.
2. **Binding whose only assertion is already true before the action** (RUN-021 delete): Task 2.8 `TestGo_DifferentialFailsVacuous`, Task 2.9 `TestDiscriminate_SurvivorIsAnError`.
3. **`check` bound to an element whose design role is `button`**: Task 2.5 `TestTP_UITarget_RoleAgreement`.
4. **Checkbox that flips only after the server answers**: Task 2.10 `TestPlaywright_LayerA_ImmediateUnderLatency` (generated spec asserts `toBeChecked` with 250 ms timeout while routes are delayed 1500 ms).
5. **A step with no action input** (does nothing, asserts a pre-existing state): Task 2.5 `TestTP_UIAction_ExactlyOne`.

---

## File map

Platform:
- Modify `framework-go/scenario/scenario.go`, `derive.go`, `classify.go`; create `scenario/problems.go`; fixtures `scenario/testdata/{reword_abandon,immediate}.json` + goldens.
- Modify `framework-go/methodcheck/project.go` (binding types), `rules_testplan.go`, `rules_callchain.go`, `emitters.go`; replace `uidialect.go` with `uidialect.go` (sum types) + `uidesign.go`; create `rules_uidesign.go`, `rules_testplan_v2_test.go`, `rules_uidesign_test.go`; fixtures `testdata/tp2_*.json`.
- Modify `framework-go-app-generator/testgen/{testgen.go,emit_go.go,emit_hooks.go→delete,emit_mcp.go,emit_rest.go,emit_playwright.go}`; create `testgen/emit_arrange.go`, `emit_null.go`, `emit_elements_ts.go`, `emit_surface_ts.go`, `tsruntime/` (embedded `runtime.gen.ts` source); create `framework-go-app-generator/composegen/scenariocompose.go` (+ test).
- Modify `framework-go-app-generator/cmd/testgen/main.go` (`-discriminate`), `framework-go-app-generator/transportgen` or `composegen/emit_http.go` (null-op middleware under local profile).
- Modify `framework-go/arch/filelayout.go` (`ARCH-TEST-COMPOSE`, hooks files forbidden).
- Modify `framework-go-scenariohost/host.go` (+`callcontext.go`): `PrincipalContext`, `AdvanceClock`, `FailNext`, `SeedResource`.
- Modify `method-assets/assets/claude/commands/core-use-cases-critique.md`, `{service,frontend}-test-plan.md`, `{service,frontend}-integration.md`, `{service,frontend}-construction.md`, `frontend-detailed-design.md`, `skills/the-method-testing/SKILL.md`, `assets/scaffold/Makefile.scenarios.mk.tmpl`, `assets/scaffold/aiarch_arch_test.go.tmpl`, `assets/scaffold/golangci.yml`.

App:
- Modify `.aiarch/state/project.json` (`$defs` of `projectStateAccess`; own testPlan data conformed; `ruleWaivers`), `server/internal/resourceaccess/projectstate/projectstateaccess.go` (`browserContractDefs` 13178-13199 → `$ref`s to the new defs; `RecordBrowserContract`; `recordServiceContract` sets `readOnly`; `frontendScopeAndChecks`/`serviceScopeAndChecks` scope + checks), `server/cmd/aiarch-state-mcp/{constructverbs.go,staleness.go,checks.go,browserdialect_test.go}`, `server/go.mod`.
- Regenerate `server/internal/resourceaccess/projectstate/contract.gen.go`, fakes.

---

### Task 2.1: Binding model v2 in methodcheck (mirror types + strict decode)

**Files:**
- Modify: `framework-go/methodcheck/project.go:726-790`
- Create: `framework-go/methodcheck/bindings.go` (sum types, hand-written in the same shape modelgen emits, with `//sumtype:decl`)
- Test: `framework-go/methodcheck/bindings_test.go`

**Interfaces (Produces):**
```go
type TestPlanRecord struct { Component string; Principals map[string]TestPrincipal `json:"principals"`; Bindings []ScenarioBinding }
type TestPrincipal struct { Subject string `json:"subject"`; Roles []string `json:"roles"` }
type ScenarioBinding struct { Scenario string; Arrange []ArrangeCall `json:"arrange"`; Steps []StepBinding; Skip *SkipReason }
type StepBinding struct { Seq int; Operation string; Inputs []TestArg; Expect TestExpect; Probes []Probe; Before *TestExpect `json:"before,omitempty"` }
type TestArg struct { Name string; Value TestArgValue; SchemaRef string }
//sumtype:decl
type TestArgValue interface { Kind() TestArgValueKind; isTestArgValue() }
type LiteralValue struct { JSON json.RawMessage `json:"json"` }                   // kind "literal"
type RefValue struct { Step int `json:"step"`; Path string `json:"path"`; Probe *int `json:"probe,omitempty"` } // kind "ref": step<N>.result.<path> or probe<N>.<n>.result.<path>
//sumtype:decl
type ArrangeCall interface { Kind() ArrangeKind; isArrangeCall() }
type ContractCall struct { Component, Operation string; Inputs []TestArg; Expect TestExpect }  // "contractCall"
type AdvanceClock struct { Duration string `json:"duration"` }                                 // "advanceClock" (Go duration string)
type FailNext struct { Component, Operation, ErrorCode string }                                // "failNext"
type SeedResource struct { Resource string; Fixture json.RawMessage }                          // "seedResource"
type Probe struct { For, Component, Operation string; Inputs []TestArg; Expect TestExpect }   // unchanged shape
func UnmarshalTestArgValue(raw []byte) (TestArgValue, error)   // envelope {"kind","model"}; unknown kind → error
func UnmarshalArrangeCall(raw []byte) (ArrangeCall, error)
```
- `TestExpect` unchanged. `ContractOperation` gains `ReadOnly bool `json:"readOnly"``.
- `DecodeProject` decodes `phaseArtifacts.testPlan` with `DisallowUnknownFields`: a record carrying `hook` or `unobservable` is a decode error naming the field (this *is* TP-HOOK's enforcement; the rule id is reported by Task 2.4 for the message).

- [ ] **Step 1: Write the failing tests:**
```go
func TestBindings_DecodeLiteralAndRef(t *testing.T)        // envelope JSON → LiteralValue / RefValue
func TestBindings_UnknownKindRefused(t *testing.T)          // {"kind":"hook"} → error `unknown kind "hook"`
func TestBindings_LegacyHookFieldRefused(t *testing.T)      // a StepBinding JSON with "hook": true → DecodeProject error mentions "hook"
func TestBindings_SwitchIsExhaustive(t *testing.T)          // compile-time only: a helper `describe(v TestArgValue) string` with a switch over both variants; `go vet` + gochecksumtype in CI covers it — the test just calls it
```
- [ ] **Step 2: Run** `cd framework-go && GOWORK=off go test ./methodcheck -run TestBindings -v` — Expected: FAIL.
- [ ] **Step 3: Implement** `bindings.go`; update `project.go`; update `testdata/tp_clean.json`, `tp_drift.json`, `tp_legacy.json`, `scenario_project.json` to the v2 shape (a tiny `cmd/tpmigrate` throwaway is fine; delete it after).
- [ ] **Step 4: Run** `GOWORK=off go test ./methodcheck` — Expected: PASS (existing TP tests now exercise v2 fixtures). `golangci-lint run ./methodcheck` — Expected: 0 issues (gochecksumtype on).
- [ ] **Step 5: Commit** `git commit -m "feat(methodcheck): binding model v2 — sum-typed args/arrange, principals, no hook/unobservable"`

---

### Task 2.2: Scenario derivation — actor outcomes, immediacy, no silent drops

**Files:**
- Modify: `framework-go/scenario/scenario.go`, `derive.go:130-230,358`, `classify.go:47`
- Create: `framework-go/scenario/problems.go`
- Create: `framework-go/scenario/testdata/reword_abandon.json` (the RUN-021 `reword-a-todo` diagram + view, verbatim from the harvest: nodes n1–n10, edges, the single view with steps n4/n6/n7), `testdata/immediate.json`
- Test: `framework-go/scenario/derive_test.go` (extend)

**Interfaces (Produces):**
```go
type Stimulus struct { Seq int; Node string; Input InputEv; Outcome; ActorOutcome Outcome `json:"actorOutcome"` }
type Output struct { Node, Kind, To, Label string; Immediate bool `json:"immediate"` } // Immediate: no component call between the actor's node and this output on the path
type Problem struct { UseCase, Scenario, Node, Code, Message string }   // codes: SCN-UNADDRESSED, SCN-ACTOR-PATH-CLIENT
type Report struct { Scenarios []Scenario; StartRedundant []StartRedundancy; Problems []Problem }
func DeriveReport(in Input) (Report, error)       // never drops a stimulus; To=="" or non-contracted To → Problem SCN-UNADDRESSED and the stimulus is KEPT with Input.To as written
func ForComponent(all []Scenario, component string) []Scenario   // unchanged signature; client projections keep ActorOutcome, engine/RA projections zero it
func ForComponentAs(all []Scenario, component string, client bool) []Scenario // explicit; ForComponent delegates with client=false — testgen/methodcheck call the explicit form
```
- `observe()` attaches: every `sendSignal` / inferred-to-actor output and every `objectNode` to **both** `cur()` (unchanged A1 behaviour) and the nearest preceding stimulus whose `Input.Kind != "call"` (`ActorOutcome`). Preconditions unchanged.
- A path containing an actor-classified node (per `classify`) with zero stimuli into any contracted Client → `Problem SCN-ACTOR-PATH-CLIENT` (the scenario is still emitted, with no stimuli, so the id is stable and the problem names it).

- [ ] **Step 1: Write the failing tests:**
```go
func TestDerive_RewordAbandon_Reported(t *testing.T)    // load reword_abandon: Report.Problems contains {UseCase:"reword-a-todo", Scenario:"reword-a-todo-P1", Code:"SCN-ACTOR-PATH-CLIENT"}; Scenarios still has P1..P3
func TestDerive_ActorOutcomeOnClientStimulus(t *testing.T) // immediate.json: a path actor→client(call)→manager→RA→sendSignal(to actor); the client stimulus's ActorOutcome.ExpectedOutputs has the sendSignal; the RA stimulus's Outcome has it too (A1 kept)
func TestDerive_ImmediateFlag(t *testing.T)             // immediate.json second path: actor action → objectNode (no call) → sendSignal: Output.Immediate == true; the first path's output Immediate == false
func TestDerive_UnaddressedIsReported(t *testing.T)     // a decision decidedBy actor with no step: Problems has SCN-UNADDRESSED naming the node; the stimulus is present with To == ""
func TestForComponent_ClientKeepsActorOutcome(t *testing.T)
```
Add golden tests `TestDerive_RewordAbandonGolden` and `TestDerive_ImmediateGolden` (`golden(t, name, b)`); regenerate with `-update` once green.
- [ ] **Step 2: Run** `GOWORK=off go test ./scenario -v` — Expected: FAIL.
- [ ] **Step 3: Implement.** `stimulusWalk` gains `lastActor *Outcome`; `inputEvent` no longer returns `To:""` silently — it returns the event and records a Problem via a `problems *[]Problem` carried on the walk. Keep `ForComponent` behaviour for existing callers; add the explicit form.
- [ ] **Step 4: Run** `GOWORK=off go test ./scenario ./methodcheck` — Expected: PASS (methodcheck golden `scenario_project` may need `-update`; inspect the diff: only `actorOutcome`/`immediate` fields should appear).
- [ ] **Step 5: Commit** `git commit -m "feat(scenario): actor outcomes on actor stimuli, Immediate flag, SCN-UNADDRESSED/SCN-ACTOR-PATH-CLIENT problems; nothing is dropped silently"`

---

### Task 2.3: Use-case and call-chain rules (CC/UC)

**Files:**
- Modify: `framework-go/methodcheck/rules_callchain.go` (`ccContext.findings()` :180), `rules.go` (`useCaseIORules`), `emitters.go:16-85`
- Test: `rules_callchain_test.go`, `rules_usecase_io_test.go` (new)

**Rules (all `SeverityError`):**
| id | func | statement |
|---|---|---|
| `CC-DECIDED-BY-ACTOR-STEP` | `cc.decidedByActorStep()` | a `decision`/`switch` with `decidedBy` ∈ actors has a step containing ≥1 call `From` = that actor, `To` = a Client |
| `UC-PATH-OUTCOME` | `pathOutcomeFindings(c CoreUseCases, s System)` | every elementary path with ≥1 actor-classified node carries ≥1 `sendSignal` or `objectNode` after its first actor node |
| `UC-OUTCOME-AT-ASKING-SURFACE` | `outcomeAtAskingSurface(...)` | for each path, the Client of the first actor stimulus receives ≥1 `sendSignal` (`linkedActorId` = that actor) or there is an `objectNode` whose step has a call `To` = that Client |
| `UC-START-NOT-ACTION` | `startNotAction(c)` | a `start` node in an actor lane (`linkedActorId` set or lane role resolves to an actor) with a non-empty label → finding (the actor's action is an `action` node) |
| `TP-CLIENT-CONTRACTED` | in `clientContractFindings` | every `System` component with `kind == client` has a `serviceContracts` entry |
| `SCN-UNADDRESSED`, `SCN-ACTOR-PATH-CLIENT` | `scenarioProblemFindings(r scenario.Report)` in validate.go | mirror `Report.Problems` as findings |

- [ ] **Step 1: Write the failing tests** using the existing inline CC fixture helpers (`sysWith`, `ucWith`, `ccView`, …) and a new `ucPathFixture(t)` built from `scenario/testdata/reword_abandon.json`:
```go
func TestCC_DecidedByActorStep_Fires(t *testing.T) / _Passes
func TestUC_PathOutcome_FiresOnNoteOnlyPath(t *testing.T)      // reword_abandon P1 → finding names "reword-a-todo-P1"
func TestUC_OutcomeAtAskingSurface_FiresWhenOnlyOtherSurfaces(t *testing.T)
func TestUC_StartNotAction_Fires(t *testing.T)                  // n1 "Activate a todo's title…" as start in Team Member lane
func TestTP_ClientContracted_Fires(t *testing.T)
func TestScenarioProblems_AreFindings(t *testing.T)
```
- [ ] **Step 2: Run** `GOWORK=off go test ./methodcheck -run 'TestCC_DecidedBy|TestUC_|TestTP_ClientContracted|TestScenarioProblems' -v` — Expected: FAIL.
- [ ] **Step 3: Implement**; append each id to `emittedRuleIDs()`; add passing fixtures.
- [ ] **Step 4: Run** `GOWORK=off go test ./methodcheck` (includes `TestDefaultCoverage_EveryAutomatedRuleHasEmitter`) — Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "feat(methodcheck): CC-DECIDED-BY-ACTOR-STEP, UC-PATH-OUTCOME, UC-OUTCOME-AT-ASKING-SURFACE, UC-START-NOT-ACTION, TP-CLIENT-CONTRACTED, SCN-* findings"`

---

### Task 2.4: Test-plan rules v2 (TP-*)

**Files:**
- Modify: `framework-go/methodcheck/rules_testplan.go` (`stepFindings` :163, `probeFindings` :191, `stateFindings` :213, `expectFindings` :604), `emitters.go`
- Test: `rules_testplan_v2_test.go` (new), fixtures `testdata/tp2_clean.json`, `tp2_vacuous.json`

**Rules (Error):**
| id | statement |
|---|---|
| `TP-HOOK` | emitted by `DecodeProject` failure text for `hook`/`unobservable` (Task 2.1); also: a `ScenarioBinding.Arrange` `ContractCall` whose `Component` is the plan's component and `Operation` equals any of the scenario's stimulus ops |
| `TP-EXPECT-REQUIRED` | for every non-`ErrorExpected` step: `Expect.Result` non-empty and not `{}`/`[]`/`null` **or** ≥1 probe whose `Expect.Result` is non-empty; every probe `Expect.Result` non-empty |
| `TP-ARG-REF` | a `RefValue` names an earlier `Step` (and `Probe` index if set) and `Path` resolves in that op's result schema (walk `properties` / `$ref` into contract `$defs`; arrays by `[]`) |
| `TP-ARRANGE` | `ContractCall` op exists on the named component and args type-check (reuse `argFindings`); `FailNext` names a real op; `AdvanceClock.Duration` parses; `SeedResource.Resource` names a Resource component |
| `TP-PROBE` (tightened) | probe op has `ReadOnly == true`; probe `Expect.Result` non-empty; `For` names a node on the scenario path whose step has a call `To` = probe `Component` |
| `TP-STATE` (tightened) | every `Outcome.ExpectedStates` and `ExpectedOutputs` of the projected stimulus has a probe with `For` = its node (no `unobservable` escape) |
| `TP-ACTOR-OBSERVED` | on a Client stimulus: every `ActorOutcome` output/state is covered by an `Expect` UI observable (web) or a probe (machine clients) |
| `TP-PRINCIPAL` | every actor that originates a stimulus in the component's scenarios has a `Principals[actor]` entry with non-empty `Subject` |

- [ ] **Step 1: Write the failing tests** (one Fires + one Passes per rule, mutate `tp2_clean.json` via `binding(t,p,comp,scn)`):
```go
func TestTP_ExpectRequired_FiresOnEmptyAndBraces(t *testing.T)
func TestTP_ArgRef_FiresOnForwardRef(t *testing.T) / _FiresOnBadPath
func TestTP_Arrange_FiresOnSelfStimulus(t *testing.T)   // TP-HOOK text
func TestTP_Probe_RequiresReadOnly(t *testing.T)
func TestTP_State_NoUnobservableEscape(t *testing.T)
func TestTP_ActorObserved_Fires(t *testing.T)
func TestTP_Principal_Fires(t *testing.T)
```
- [ ] **Step 2: Run** — Expected: FAIL. **Step 3: Implement**; register ids (and the three previously unregistered ones: `TP-CLIENT-OPS`, `TP-LABEL-OP`, `CLIENT-MACHINE-MIRROR`). **Step 4: Run** `GOWORK=off go test ./methodcheck` — Expected: PASS. **Step 5: Commit** `git commit -m "feat(methodcheck): TP rules v2 — mandatory expectations, refs, arrange, read-only probes, actor observed, principals"`.

---

### Task 2.5: Semantic UI dialect and typed UI design

**Files:**
- Replace: `framework-go/methodcheck/uidialect.go` (keep the file name; rewrite contents)
- Create: `framework-go/methodcheck/uidesign.go`, `rules_uidesign.go`, `uidialect_test.go` (rewrite), `rules_uidesign_test.go`
- Modify: `rules_testplan.go` `uiDialectFindings` :764, `observesUI` :796

**Interfaces (Produces):**
```go
type UIRole string // closed: button, checkbox, textbox, listitem, list, link, heading, status, dialog, navigation, main, form, option, combobox, switch, tab, tabpanel, table, row, cell, img
func UIRoles() []UIRole
type UITarget struct { Element string `json:"element"`; Nth *int `json:"nth,omitempty"` } // Element = UIDesign element id; the locator is derived from the design (role, name, within)
//sumtype:decl
type UIAction interface { Kind() UIActionKind; isUIAction() }
// variants (kind → fields): Navigate{Route string} · Click{Target} · Dblclick{Target} · Fill{Target; Value string} · Press{Target; Key string} · Check{Target} · Uncheck{Target} · Hover{Target} · Select{Target; Option string}
//sumtype:decl
type UIObservable interface { Kind() UIObservableKind; isUIObservable() }
// variants: Visible{Target} · Hidden{Target} · Absent{Target} · Text{Target; Text string} · Value{Target; Value string} · Checked{Target} · Unchecked{Target} · Enabled{Target} · Disabled{Target} · Focused{Target} · Count{Target; N int} · URL{Route string}
func UnmarshalUIAction([]byte) (UIAction, error); func UnmarshalUIObservable([]byte) (UIObservable, error)
func ParseUIObservation(raw string) ([]UIObservable, error)   // array only; empty array → error "observation asserts nothing"
func ActionRoles(k UIActionKind) []UIRole   // exhaustive switch: check/uncheck→{checkbox,switch}; fill→{textbox,combobox}; select→{combobox}; press→any; click/dblclick/hover→any
func LayerAObservables(a UIAction) []UIObservable // exhaustive: Check→Checked{Target}; Uncheck→Unchecked; Fill→Value{Target,Value}; Select→Value; others→nil

type UIDesignRecord struct { Surface string; Screens []UIScreen; Content string }
type UIScreen struct { ID, Route string; Elements []UIElement }
type UIElement struct { ID string; Role UIRole; Name string; Within string `json:"within,omitempty"`; Repeats bool `json:"repeats"` }
func (d UIDesignRecord) Element(id string) (UIElement, UIScreen, bool)
```
**Rules (Error):** `UID-ROLE` (role in closed set), `UID-NAME` (non-empty `Name` for every role except list/main/navigation/form), `UID-ID-UNIQUE`, `UID-WITHIN` (resolves to an element on the same screen), `UID-ROUTE` (every `Navigate.Route` and `URL.Route` is a screen route), `TP-UI-TARGET` (every target `Element` resolves in the surface's design; `ActionRoles(kind)` contains the element's role; `Count` requires `Repeats`), `TP-UI-ACTION` (exactly one input named `action` per web step).

- [ ] **Step 1: Write the failing tests** (`uidialect_test.go`: envelope decode for each variant, unknown kind, empty observation; `rules_uidesign_test.go`: each UID-* Fires/Passes; `TestTP_UITarget_RoleAgreement` — `check` on an element with role `button` fires; `TestTP_UIAction_ExactlyOne`).
- [ ] **Step 2: Run** `GOWORK=off go test ./methodcheck -run 'TestUI|TestUID|TestTP_UI' -v` — Expected: FAIL.
- [ ] **Step 3: Implement**; `Project.PhaseArtifacts` gains `UIDesign map[string]UIDesignRecord `json:"uiDesign"``; `uiDialectFindings` now resolves targets through the design (`findDesign(p, surface)`).
- [ ] **Step 4: Run** `GOWORK=off go test ./methodcheck` — Expected: PASS. **Step 5: Commit** `git commit -m "feat(methodcheck): sum-typed UI dialect with design-resolved targets; typed UIDesignRecord and UID-*/TP-UI-* rules"`.

---

### Task 2.6: `scenariocompose` — generated subject construction (no FILL)

**Files:**
- Create: `framework-go-app-generator/composegen/scenariocompose.go`, `scenariocompose_test.go`
- Modify: `framework-go/arch/filelayout.go` (rule `ARCH-TEST-COMPOSE`; delete `_hooks_test.go` from the allowed set), `framework-go-scenariohost/callcontext.go` (`PrincipalContext`)

**Interfaces (Produces):**
- `composegen.EmitScenarioCompose(m *projectmodel.Model) ([]byte, error)` → `internal/scenariocompose/compose.gen.go`, package `scenariocompose`, one `func New<Iface>(t *testing.T, h *scenariohost.Host) <pkg>.<Iface>` per non-client contract, built with the **same wiring `main.gen.go` uses** over `h.TemporalHostPort`, `h.PostgresURL`, `h.GitHub`, `h.Repo` (map substrates by the deployment model's infra bindings; a binding with no scenariohost substrate is a generation error naming it), plus `func Workflows() scenariohost.Workflows` registering every Manager workflow, and `func Null<Iface>(ops ...string) <pkg>.<Iface>` wrapping `New<Iface>` so the named ops return the zero result (Task 2.9).
- **Client constructors (amended after Task 2.8 review; delivered with it):** for every MCP/REST client contract whose generated handler package exists (`internal/client/web/<pkg>` for REST, `internal/client/<leaf>/<pkg>` for an MCP tool table, the Manager it fronts being exposed to it exactly as `composegen`/`clientgen` decide), `func New<ClientIface>(t *testing.T, h *scenariohost.Host) *<handlerpkg>.Handler` returning the generated `Handler{Manager: <the real Manager>, Security: edgeSecurity()}`. `edgeSecurity()` is `security.New(security.WithPolicyDecisionPoint(generatedPolicy{}))`, where `generatedPolicy` is deny-by-default and permits exactly the `(verb, scope)` rows of the same `httpgen.PlanOps` derivation `authz.gen.go` is written from, on the principal's own scope (the app's hand `Hooks.PolicyDecisionPoint` lives in `package main` and cannot be imported; the generated table is what the generated transports can be held to). Test: `TestScenarioCompose_ClientConstructorHasRealPolicy`; compile proof: `TestScenarioCompose_CompilesAgainstScenariohost` (real `clientgen` output) and testgen's `TestMCP_GeneratedFileCompiles` (real `EmitScenarioCompose` output).
- scenariohost: `func (h *Host) PrincipalContext(t *testing.T, p Principal) context.Context` where `type Principal struct{ Subject string; Roles []string }`; `ResourceAccessContext/EngineContext/ManagerContext` gain a `Principal` parameter variant `…ContextAs(t, p)`.
- arch `ARCH-TEST-COMPOSE` (Error): under `internal/{manager,engine,resourceaccess,client,utility}/**` the only `_test.go` files are `<stereotype>_scenarios.gen_test.go` and `null.gen_test.go`; any `*_hooks_test.go` is a violation; and no file in a `_test` package declares a type with a method set matching a contract interface (check via `go/types` implements against the generated interfaces).

- [ ] **Step 1: Write the failing tests:** `TestScenarioCompose_EveryContractHasConstructor` (todomvc fixture), `TestScenarioCompose_CompilesAgainstScenariohost` (temp module + `go vet`), `TestFileLayout_HooksFileIsViolation`, `TestArchTestCompose_InFileFakeIsViolation`.
- [ ] **Step 2: Run** — Expected: FAIL. **Step 3: Implement** (reuse composegen's wiring emitter; factor the per-component constructor call into a shared function used by both `main.gen.go` and `compose.gen.go`). **Step 4: Run** app-generator + framework-go + scenariohost tests — PASS. **Step 5: Commit** `git commit -m "feat(composegen): generated scenariocompose constructors and null wrappers; arch forbids hooks files and in-file fakes"`.

---

### Task 2.7: scenariohost arrange primitives

**Files:**
- Modify: `framework-go-scenariohost/host.go`; create `arrange.go`, `arrange_test.go`

**Interfaces (Produces):**
```go
func (h *Host) AdvanceClock(t *testing.T, d time.Duration)                 // Temporal dev server time-skipping (dev server supports it) + a host clock the generated RA contexts read via fwra.Clock
func (h *Host) FailNext(t *testing.T, component, op, errorCode string)     // registers a one-shot fault; scenariocompose's constructors wrap every component with a fault interceptor that checks the registry
func (h *Host) SeedResource(t *testing.T, resource string, fixture json.RawMessage) // dispatches by resource kind: postgres → exec SQL fixture {"sql": "..."}; github → h.GitHub.Seed(fixture); gitRepo → h.Repo.Seed(fixture); unknown kind → t.Fatalf
```
- [ ] **Step 1: Write the failing tests** (`-short`-safe where possible): `TestAdvanceClock_ShiftsRAClock`, `TestFailNext_OneShot`, `TestSeedResource_UnknownKindFatal`.
- [ ] **Step 2–5:** run, implement, run, commit `git commit -m "feat(scenariohost): AdvanceClock, FailNext, SeedResource arrange primitives"`.

---

### Task 2.8: testgen Go emitter v2 — principals, refs, arrange, differential, no hooks

**Files:**
- Modify: `framework-go-app-generator/testgen/{testgen.go,emit_go.go,emit_mcp.go,emit_rest.go}`; delete `emit_hooks.go`; create `emit_arrange.go`
- Test: `testgen/testgen_test.go`, `emit_go_v2_test.go` (new) — fixture `testdata/project_tp.json` migrated to v2 (Task 2.1 shape)

**Generated shape (per scenario, Go):**
```go
func TestScenario_<id>(t *testing.T) {
  h := scenariohost.Start(t)
  h.RunScenario(t, "<key>", "<id>", func(t *testing.T) {
    var subject <pkg>.<Iface> = scenariocompose.New<Iface>(t, h)
    // arrange: one line per ArrangeCall (exhaustive switch in the emitter)
    // step 1
    in1 := <param type>{...literal...}; in1.X = step0 out ref...   // $ref threading from earlier outN
    pre := probeSet(t, h, /*probes of step 1*/)                       // differential: evaluate every probe BEFORE
    out1, err1 := subject.<Op>(h.ManagerContextAs(t, principal("<actor>")), in1)
    expect(t, "<id>", 1, out1, err1, `<expect json>`)
    post := probeSet(...)
    differential(t, "<id>", 1, pre, post)                             // fails "vacuous" if every probe expectation already held in pre
  })
}
```
- The Go emitters never emit a hooks file. `Drift` loses `hooksDrift`/`symbolDrift`. `Output.HooksOnce` is **narrowed here and deleted in Task 2.10**: it survives Task 2.8 only to carry the web client's `hooks.ts` (the Playwright emitter is Task 2.10's), and `TestGo_NoHooksFileEmitted` pins that it never holds a Go file.
- Machine clients (`emit_mcp.go`/`emit_rest.go`): subject = `serveTools(t, h, scenariocompose.New<ClientIface>(t, h))` with `permitAll` **removed** — `New<ClientIface>` (Task 2.6, client constructors) returns the generated handlers over the real Manager, authorized by the generated authorization rows (there is no `scenariocompose.Security(h)`; the policy is built inside the constructor). Over-the-wire execution against devstack is Plan 3; here the in-process server uses the real generated authz rows.
- Ref threading is emitted in its runtime form `refInto(t, "<id>", <seq>, outN, "<path>", &inM.<Param>)` (and `threadRef` for tool-call arguments), not the static `inM.X = outN.Y`: step results are `any` and modelgen's lint-folded field names (`Id`→`ID`) and nested/array paths are not knowable in the emitter. A mis-spelled path therefore fails at test run time, so **`TP-ARG-REF` (Task 2.4) must resolve every ref path against the result schema at record time**; a bad path never reaches a generated test.

- [ ] **Step 1: Write the failing tests:**
```go
func TestGo_NoHooksFileEmitted(t *testing.T)
func TestGo_PrincipalFromPlan(t *testing.T)            // generated source contains `ManagerContextAs(t, principal("team-member"))`
func TestGo_RefThreading(t *testing.T)                 // `refInto(t, "<id>", 2, out1, "", &in2.Order)` (runtime form; see the ref-threading note above)
func TestGo_ArrangeExhaustive(t *testing.T)            // each of the four ArrangeCall kinds emits its line; a compile-time exhaustive switch
func TestGo_DifferentialFailsVacuous(t *testing.T)     // compile+run the generated test for a fixture whose probe expectation holds before the step against a null subject → output contains "vacuous"
func TestMCP_NoPermitAll(t *testing.T)
```
- [ ] **Step 2–5:** run (FAIL), implement, run (PASS: `GOWORK=off go test ./testgen`), commit `git commit -m "feat(testgen): Go emitter v2 — generated subjects, principals, refs, arrange, pre/post differential; hooks deleted"`.

---

### Task 2.9: Null-subject discrimination (`TP-DISCRIMINATE`)

**Files:**
- Create: `framework-go-app-generator/testgen/emit_null.go`; modify `cmd/testgen/main.go` (`-discriminate`), `method-assets/assets/scaffold/Makefile.scenarios.mk.tmpl` (`test-discriminate`), `composegen/emit_http.go` (null-op middleware)

**Interfaces (Produces):**
- Generated Go: `null.gen_test.go` in each scenario package: `TestMain` reads `AIARCH_NULL_OPS` (comma list `<Iface>.<Op>`); when set, `scenariocompose.New<Iface>` is replaced by `scenariocompose.Null<Iface>(ops...)` for the package's subject.
- `testgen -discriminate -project … -root …`: for every (scenario, stimulus op) pair runs `go test -run ^TestScenario_<id>$ ./<pkg>/` with `AIARCH_NULL_OPS=<Iface>.<op>`; a **passing** run is a survivor → prints `TP-DISCRIMINATE <key> <id> <op>` and exits 1. Web: runs `npx playwright test --grep <id>` with `AIARCH_NULL_OPS` exported to the booted server (Plan 3 wires the boot; until then the web half is skipped with a printed notice).
- Generated server (local profile only): `cmd/server/null.gen.go` middleware — when `AIARCH_NULL_OPS` names a REST/MCP op, the handler returns the op's zero result with 200 and does not call the Manager.
- Makefile: `test-discriminate: GOWORK=off go tool testgen $(TESTGEN_ARGS) -discriminate`.

- [ ] **Step 1: Write the failing tests:** `TestNull_EmitsTestMainSwitch`, `TestDiscriminate_SurvivorIsAnError` (fixture scenario whose expectation holds under null → exit 1 with the id), `TestDiscriminate_AllFailIsGreen`, `TestNullMiddleware_LocalOnly` (refuses to install under non-local profile).
- [ ] **Step 2–5:** run, implement, run, commit `git commit -m "feat(testgen): null-subject discrimination run; local-profile null-op middleware"`.

---

### Task 2.10: Playwright emitter v2 — elements, surface check, latency, Layer A, arrange over REST

**Files:**
- Modify: `framework-go-app-generator/testgen/emit_playwright.go`; create `emit_elements_ts.go`, `emit_surface_ts.go`, `tsruntime/runtime.ts` (embedded), `emit_playwright_v2_test.go`

**Generated files per web surface (`uitests/generated/<key>/`):**
- `elements.gen.ts`: `export const elements = { <elementId>: { role: "<role>", name: "<name>", within?: "<elementId>", repeats: bool } } as const; export function locate(page, id, nth?)` → `page.getByRole(role, {name, exact:true})`, scoped by `within` via `locate(page, within).getByRole(...)`, `.nth(nth)` when given.
- `runtime.gen.ts` (copied from `tsruntime/runtime.ts`): `installLatency(page, ms)` (`page.route('**/*', route => setTimeout(() => route.continue(), ms))` for same-origin API requests only, installed **before** `goto`), `immediate = { timeout: Number(process.env.AIARCH_UI_IMMEDIATE_MS ?? 250) }`, `assertNever(x: never): never`, `arrange(call: ArrangeCall)` — a `switch (call.kind)` with `default: assertNever(call)` performing `contractCall` via `fetch` against `clientRoutes` (from `restRouteTable`) with header `X-Dev-Principal` (Plan 4 enables multi-principal; until then the single process principal), `advanceClock`/`failNext`/`seedResource` via `POST /__scenariohost/<kind>` (a local-profile-only endpoint Plan 3 adds; until then emitted as a `test.skip` reason).
- `surface.spec.gen.ts`: for each screen: `goto(route)`, then for each non-repeating element `await expect(locate(page, id)).toBeAttached()`.
- `<id>.spec.gen.ts`: `installLatency(page, Number(process.env.AIARCH_UI_LATENCY_MS ?? 1500))` → arrange → per step: action line from an exhaustive map over `UIAction` kinds → **Layer A assertions** from `LayerAObservables(action)` with `immediate` → observables from the binding, those whose `Target` matches an `ActorOutcome` output with `Immediate` also get `immediate`, the rest default timeout → probes executed via `fetch` and compared (`expect(json).toMatchObject(...)`).
- `playwright.config.gen.ts`: unchanged plus `globalSetup` that runs `surface.spec.gen.ts` first (or `dependencies` project).
- `hooks.ts` is no longer emitted; `Drift` reports any `hooks.ts` as orphan. **This task owns the deletion deferred from Task 2.8:** delete `testgen.Output.HooksOnce`, `Drift`'s `hooks.ts` presence check and the CLI's (`cmd/testgen`) `HooksOnce` write/orphan path; the field must not exist once this task lands.

- [ ] **Step 1: Write the failing tests:**
```go
func TestPlaywright_LocatesByRoleFromDesign(t *testing.T)        // spec contains `locate(page, "todo-checkbox")` and elements.gen.ts maps it to role "checkbox"
func TestPlaywright_LayerA_ImmediateUnderLatency(t *testing.T)   // for a `check` action: spec has `await expect(locate(page,"todo-checkbox")).toBeChecked(immediate)` and `installLatency(`
func TestPlaywright_SurfaceSpecCoversEveryElement(t *testing.T)
func TestPlaywright_ActionSwitchExhaustive(t *testing.T)         // Go-side: adding a UIActionKind without an emitter entry fails compile (gochecksumtype) — test enumerates `methodcheck.UIActionKinds()` and asserts an emitter exists for each
func TestPlaywright_NoHooksEmitted(t *testing.T)
func TestPlaywright_EmptyObservationRefusedAtGeneration(t *testing.T)
```
- [ ] **Step 2–5:** run, implement, run (`GOWORK=off go test ./testgen`; plus `cd framework-web && npx tsc --noEmit` over `tsruntime/runtime.ts`), commit `git commit -m "feat(testgen): Playwright v2 — role locators from the UI design, surface check, latency harness, Layer A immediacy, declarative arrange"`.

---

### Task 2.11: App — schema, regen, verbs, waivers, own-project conformance

**Files:**
- Modify: `.aiarch/state/project.json` — `serviceContracts.projectStateAccess.$defs`: replace `StepBinding`, `ScenarioBinding`, `TestArg`, `TestPlanRecord`, add `TestPrincipal`, `TestArgValue` (+`x-go-sumtype`), `ArrangeCall` (+ variants), `UITarget`, `UIAction`, `UIObservable`, `UIObservation` (array, `minItems:1`), `UIRole`, `UIDesignRecord`, `UIScreen`, `UIElement`; root: `ruleWaivers: [{rule, reason, expires}]`.
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go:13178-13199` — `browserContractDefs` becomes `$ref`s to `projectStateAccess` defs (single source; keep `TestBrowserContractDialect` green by comparing with `methodcheck.UIActionKinds()`); `recordServiceContract` path sets `operations[].readOnly` from the verb-prefix classifier used by the internal tool catalog (`grep -n "mutation verb\|verbClass" toolcatalog*.go`); `RecordBrowserContract` unchanged except defs.
- Modify: `server/cmd/aiarch-state-mcp/staleness.go:70` `applyGateSeverityPolicies` — downgrade to Warning when `p.RuleWaivers` names the rule and `expires` ≥ today.
- Modify: `server/cmd/aiarch-state-mcp/constructverbs.go` — `recordPhaseArtifact` kind `uiDesign` decodes the typed record (strict); `refuseBlankContent` extends to `Screens == 0`.
- Modify: `projectstateaccess.go:12791-13066` scope + checks — construction/integration edit scope **excludes** `uitests/**` and `**/*_scenarios.gen_test.go`; integration adds `command:gen-tests-check` (already), `command:test-discriminate`, `command:surface-check` (`npx playwright test -c generated/<c>/playwright.config.gen.ts --grep surface`), and `scenariocompose` regeneration `command:gen-compose-check` already covers `internal/scenariocompose/**/*.gen.*` once `compositionPaths` includes it.
- Data edit (own project): a one-off `cmd/tpmigrate` (deleted after) that converts every own testPlan record: `Value` string → `{"kind":"literal","model":{"json":<value>}}`, drop `hook`/`unobservable`, add `arrange: []`, `principals: {}`; add `ruleWaivers` for `TP-EXPECT-REQUIRED`, `TP-ACTOR-OBSERVED`, `TP-PRINCIPAL`, `TP-STATE`, `TP-PROBE`, `UC-PATH-OUTCOME`, `UC-OUTCOME-AT-ASKING-SURFACE`, `UC-START-NOT-ACTION`, `CC-DECIDED-BY-ACTOR-STEP`, `SCN-ACTOR-PATH-CLIENT`, `TP-DISCRIMINATE` with reason "ruling 8: own-project rebinding scheduled" and `expires: "2026-12-31"`.
- Release + pin: tag `framework-go`, `framework-go-app-generator`, `framework-go-scenariohost`, `method-assets`; bump pins; `make gen-models` (regenerates `contract.gen.go` with the sum types); `make gen-tests` (regenerates own scenario files; hooks files deleted — `git rm` every `*_hooks_test.go` and `uitests/generated/*/hooks.ts`).

- [ ] **Step 1: Write the failing tests:** `TestWaiver_DowngradesNamedRuleUntilExpiry`, `TestRecordUIDesign_TypedRefusesNoScreens`, `TestRecordServiceContract_SetsReadOnlyFromVerb`, `TestBrowserContractDialect` (updated), `TestEditScope_ConstructionExcludesUitests`.
- [ ] **Step 2: Run** `cd server && GOWORK=off go test ./cmd/aiarch-state-mcp ./internal/resourceaccess/projectstate -run 'TestWaiver|TestRecordUIDesign|TestRecordServiceContract_Sets|TestBrowserContractDialect|TestEditScope_Construction' -v` — Expected: FAIL.
- [ ] **Step 3: Implement** schema → `make gen-models` → verbs → waivers → scope; run the data migration; `git rm` the hooks files; `make gen-tests`.
- [ ] **Step 4: Run** `GOWORK=off go test -p 1 ./internal/resourceaccess/projectstate ./cmd/aiarch-state-mcp ./internal/manager/delivery` — Expected: PASS; `make method-check` — Expected: PASS with Warnings only for waived rules (print the count in the commit body). Note: own-project scenario tests (`TestScenario_*`) now construct subjects via `scenariocompose`; run `make test-scenarios` for one Engine package and one RA package to prove the generated constructors wire; if a package's wiring is not derivable, the fix is in composegen's substrate mapping (Task 2.6), not a hand edit.
- [ ] **Step 5: Commit** `git commit -m "feat(app): binding model v2 schema, typed UI design, readOnly ops, rule waivers, uitests out of construction scope; own plans migrated under ruling 8"`.

**Status 2026-11-07:** blocked at Step 3 (`make gen-tests`): nothing emits `internal/scenariocompose/compose.gen.go` into an app, and `EmitScenarioCompose` refuses hook-supplied variant args. App WIP preserved on branch `wip/fpc-2-task-2-11` @63129ac4. Tasks 2.11a–2.11e below unblock it; then resume at Step 3 after cherry-picking 63129ac4 onto the task branch.

**Status 2026-11-07 (2.11 resume, review round 1; local-only run, workspace mode, release + pin NOT done):** own-project scenarios are green or dated-skipped. Every own-project binding whose first step needed hook-seeded state (hooks deleted by this task) carries `skip: {reason: "ruling 8: needs arranged state; own-project rebinding scheduled", until: "2026-12-31"}` (277 bindings across 21 components); agenticJobAccess's 20 bindings carry a skip naming earmark E3 (no local-profile arm, so scenariocompose builds no subject). 88 own scenarios run and pass (`go test -p 1 ./internal/... -run TestScenario_`: exit 0). Note `skip.until` is a date here although methodcheck's `SkipReason.until` documents an activity id; TP-SKIP accepts it because the date names no integrated activity. Earmarks:
- **E11** the `deliveryManager` and `mcpClient` view contracts (`$defs.StepBindingView`) still carry `hook` and `unobservable` as required fields, hard-coded to false/[] by `bindingToContract` (ruling 2 deleted both from the record). Owed: drop them from the two view contracts and the webApp pass that reads them.
- **E12** the own project's rebinding (arrange calls or seeded principals for the 277 skipped bindings, then remove each skip) is owed before 2026-12-31; testgen refuses (an Error naming the component) a recorded plan with unskipped bindings for a component that has no local-profile subject, so that drop can no longer be silent.
- **E13** `TP-DISCRIMINATE` is testgen's `-discriminate` verdict, not a methodcheck finding, so a `ruleWaivers` entry for it was inert and has been removed (with `UC-START-NOT-ACTION`, which fires nothing on the own project). `make test-discriminate` cannot run on the own project until release + pin: testgen's null-subject run builds each package with `GOWORK=off` against the pinned `framework-go-scenariohost`, which lacks `Host.Input`. After the pin, run it and fix every survivor; there is no waiver path.
- **E14** the sandbox tests that resolve the pinned (pre-v2) platform (`TestFresh*`, `TestRun009ManagerPath` remainder, `TestMachineClientsCarryEveryContractShape` remainder, `TestRun010SeatedGateLoadsTheState` under an exported go.work) are release-gated: the two run009 ones skip with a dated reason (`requireReleasedToolchain`, owed until 2026-12-31 and a failure after); `TestConformanceFixture_RecordsThroughVerbs` fails on the platform conformance fixture's own v1 plan under the new Error rules (platform fixture migration owed).

---

### Task 2.11a: Model-derived local profile — `Substrate` catalog, `git-repo`, `Dep.Setting` (platform: projectmodel, configgen, rungen)

**Ruling (system-architect, 2026-11-07, replaces the 2.11 Step 4 remedy):** the scenario subject is the *local-profile composition*, the same profile `cmd/devstack` boots, and it must be derived completely from the model with nothing hook-supplied. Production keeps `hooks.go` only for cloud-profile policy. Neither relocating the seam to an importable package (breaks PR-2/GC-1, illegal under the layout standard, `newAppHooks` cannot run in a test) nor an args-provider interface (FILL by another name) is acceptable.

**Files:**
- Modify: `framework-go-projectmodel/deployment.go` (`Substrate` enum: `SubstrateTemporal`, `SubstratePostgres`, `SubstrateGitHubApp`, `SubstrateKeycloak`, `SubstrateOtel`, `SubstrateExternalHTTP` (exists :328), new `SubstrateGitRepo = "git-repo"`; `func Substrates() []Substrate`; `func (Substrate) Inputs() []SubstrateInput` — move the catalog from `framework-go-app-generator/internal/deploynaming.SubstrateCatalog`; `func InfraFieldName(key, input string) string`; `func InfraEnvName(prefix string, d InfraDecl, input string) string`; `const LocalProfile = "local"`), `contract.go:29` (`Dep.Setting string `json:"setting,omitempty"``), `validate.go` (unknown substrate → `PM-SUBSTRATE-UNKNOWN`; `Dep.Setting` must name a deployment setting of compatible type: duration↔`time.Duration`, string, bool, int → `PM-DEP-SETTING`).
- Modify: `framework-go-app-generator/configgen/*` and `rungen/rungen.go` to read the promoted catalog (`deploynaming` becomes a re-export or is deleted); rungen provisions a temporary bare repo per local `git-repo` key, as it does Temporal/Postgres (`rungen.go:8`).
- Tests: `projectmodel/validate_test.go` `TestValidate_UnknownSubstrate`, `TestValidate_DepSettingResolves`, `TestSubstrates_EveryKindHasInputs`; configgen golden for a `git-repo` field `ProjectStateGitRepoURL` and env `<PREFIX>_PROJECT_STATE_GIT_REPO_URL`; rungen test `TestRungen_ProvisionsGitRepo`.

- [ ] **Step 1: Write the failing tests.** **Step 2: Run** (`GOWORK=off go test ./` in projectmodel; `./configgen ./rungen` in app-generator) — FAIL. **Step 3: Implement.** **Step 4: Run** — PASS; `GOWORK=off go test ./...` in both modules — PASS. **Step 5: Commit** `git commit -m "feat(projectmodel): Substrate catalog with git-repo, LocalProfile, infra naming, Dep.Setting; configgen/rungen consume it"`.

### Task 2.11b: One `ctorArgs` for both composition roots; `localArm`; registrar kinds; `appgen compose` emits scenariocompose (platform: composegen, cmd/appgen)

**Files:**
- Modify: `framework-go-app-generator/composegen/{scenariocompose.go,catalog.go,resolve.go,hooks.go}`, `cmd/appgen/main.go:248-275` (`genCompose`).

**Interfaces (Produces):**
```go
type argSource interface{ /* two impls: rootArgs (cfg.<Field>, tc, ctx,pool) and scenarioArgs (s.temporal(), s.t.Context(), s.postgres(), s.input("<key>","<INPUT>")) */ }
func ctorArgs(arm variantArm, src argSource) ([]string, error)   // exhaustive switch over projectmodel.Substrates(); replaces variantArgsForSubstrate (catalog.go:29-44) and scenarioArmArgs (scenariocompose.go:263-292)
func localArm(rb) (variantArm, bool, error)                        // the binding's projectmodel.LocalProfile arm; no local arm + required → error (same text as main.gen.go "no ResourceAccess variant for the active profile"); optional/optional-dormant → nil; hooked local arm → error "the local profile is the run contract and must be model-derived; declare its inputs as infrastructure or binding settings"
```
- `managerArg` and main.gen.go's manager-dependency resolution honour `Dep.Setting`: `cfg.<Field>` in production, `settingLiteral(default)` in scenario; a dependency with a setting link emits no resolver hook.
- Registrar kinds: for `Config.ScheduleRegistrarComponent` the registrar's constructor takes, after its infra args, one `<mgrAlias>.ExecutionKinds()` per Manager that depends on it, in manager-key order; both roots emit it (app side: `NewTemporalMessageBus(cl, tables ...map[ExecutionKind]KindBinding)`).
- `genCompose` adds `out[composegen.ScenarioComposeFile], err = composegen.EmitScenarioCompose(effective, cc)`; covered by `-check` drift; the file exists even with zero test plans (`Workflows()` feeds replay).
- **Rulings (review of 2.11b, 2026-11-07):**
  - *A policy dependency is a refusal, not a compose failure.* `func EmitScenarioCompose(m, cfg) ([]byte, []Refusal, error)`: a Manager whose constructor dependency is composition-root policy the model cannot express (a func-typed resolver, a derived string) is left unbuilt in the subject (no `New<Iface>`, `Null<Iface>`, handler or `Workflows()` registration), a Manager depending on a refused Manager is refused with it, and the set `{Manager, Dependency, Reason}` is returned beside the file. `genCompose` writes `compose.gen.go` and prints each refusal (`appgen: scenariocompose: manager left unbuilt: …`); main.gen.go, config and devstack are regenerated regardless. Every other failure (a bad setting, a hooked local arm, no binding) stays a generation error. `TestScenarioCompose_UnresolvedDependencyIsARefusal` asserts the manager and dependency are named; `TestScenarioCompose_RefusalCascadesToDependentManagers` the cascade. The greenfield sandbox gate (`TestNullgen_HooksCoverEverySeam/greenfield`) runs, with orderManager refused.
  - *The ExternalHTTP exception is deliberate.* `localArm` rejects a hooked local arm EXCEPT an `ExternalHTTP` variant the model fully determines (`variantArm.externalInfra`): the declared URL input plus `&http.Client{Timeout: 30 * time.Second}`, the same seed `hooks.go` writes. The hook file is generated only if absent, so a hand edit of that hook body is composition-root policy that the subject does not see; that divergence is accepted for 2.11b and is the reason Plan 3's `-check` of hooks (E4) must compare an ExternalHTTP hook body against `externalHTTPSeed`.
  - *Ordering dependency on 2.11c.* A config-valued local infra (git-repo, github-app, keycloak, external-http) is spelled `s.h.Input(s.t, "<key>", "<substrate>", "<INPUT>")`, which compiles only once Task 2.11c adds `Host.Input`; 2.11b has no release, and 2.11c must land (with the compile-sandbox case it lists) before any release or before 2.11d regenerates the app.
- Tests: `TestCtorArgs_BothRootsSameShape`, `TestScenarioCompose_LocalArmOnly`, `TestScenarioCompose_OptionalDormantNoLocalArmIsNil`, `TestScenarioCompose_LocalHookedArmIsError`, `TestCompose_DepSettingBothRoots`, `TestCompose_RegistrarKindsFromManagers`, `TestCompose_WritesScenarioCompose`, `TestScenarioCompose_UnresolvedDependencyIsARefusal`, `TestScenarioCompose_RefusalCascadesToDependentManagers`, `TestReportRefusals_NamesEveryUnbuiltManager`.

- [ ] **Step 1: Write the failing tests.** **Step 2: Run** `GOWORK=off go test ./composegen ./cmd/appgen` — FAIL. **Step 3: Implement.** **Step 4: Run** — PASS; `GOWORK=off go test ./...` + `golangci-lint run ./...` in app-generator — PASS/0. **Step 5: Commit** `git commit -m "feat(composegen): shared ctorArgs, local-arm scenario wiring, Dep.Setting, registrar kinds; appgen compose emits scenariocompose"`.

### Task 2.11c: scenariohost is the one infra→substrate mapping; arch keeps test substrates out of the binary (platform: scenariohost, framework-go/arch)

**Files:**
- Modify: `framework-go-scenariohost/host.go` (+ `go.mod` → projectmodel):
```go
func (h *Host) Input(t *testing.T, infraKey string, s projectmodel.Substrate, input string) string // exhaustive: temporal HOSTPORT/NAMESPACE; postgres URL; git-repo URL → h.GitRepo(t, infraKey).URL (one bare repo per key, lazy, reuse startLocalGitRepo host.go:278); otel none; github-app/keycloak/external-http → t.Fatalf naming the missing double
func (h *Host) GitRepo(t *testing.T, infraKey string) LocalGitRepo
func (h *Host) Env(t *testing.T, prefix string, d *projectmodel.Deployment) []string // for every LocalProfile InfraDecl: InfraEnvName(...)=Input(...); Plan 3's devstack launch consumes this
```
- Modify: `framework-go/arch/arch.go` — new Error rule `ARCH-SCENARIOCOMPOSE-TEST-ONLY`: only `_test` packages may import `<module>/internal/scenariocompose` or `framework-go-scenariohost`; `cmd/` included. `internal/scenariocompose` stays exempt only while all-generated (arch.go:316-334); no new exemptions.
- Tests: `TestInput_EverySubstrateHasAnArm` (iterates `projectmodel.Substrates()`), `TestEnv_LocalInfraOnly`, `TestGitRepo_OnePerKey`; a compile-sandbox case in composegen over a config-valued local arm (a `git-repo` infra) proving the emitted `s.h.Input(s.t, key, "git-repo", "URL")` builds and vets against the real `Host.Input` (2.11b emits it before it exists); arch `TestArchScenarioComposeTestOnly_Fires` with a violating fixture + passing fixture.

- [ ] **Step 1: Write the failing tests.** **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4: Run** scenariohost + framework-go `./arch` — PASS. **Step 5: Commit** `git commit -m "feat(scenariohost): Input/GitRepo/Env — the single infra→substrate mapping; arch: ARCH-SCENARIOCOMPOSE-TEST-ONLY"`.

### Task 2.11d: Release 2.11a–c and conform archistrator's own model and seam (platform release; app)

**Release (needs the founder's push if the classifier refuses):** tag `framework-go-projectmodel`, `framework-go`, `framework-go-app-generator`, `framework-go-scenariohost`, `method-assets` at fpc/main HEAD (next minor each); push fpc/main + those tag refs explicitly; bump `server/go.mod` pins; `GOWORK=off go mod tidy`.

**App files:**
- `.aiarch/state/project.json` `slots.6.model.deployment`: add infrastructure `{key:"project-state-git-repo", substrate:"git-repo", profiles:["local"], presence:"required", env:{URL:"ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL"}}` and `{key:"artifact-repo", substrate:"git-repo", profiles:["local"], presence:"optional", env:{URL:"ARCHISTRATOR_ARTIFACT_REPO_URL"}}`; local arms of projectState, gitActivityStatus, activityExecution, designSession, sourceControl, episode → `infra:["project-state-git-repo"]`; artifactAccess → `["artifact-repo"]`; delete binding settings and top-level settings `projectStateGitRepoURL`/`artifactRepoURL` (project.json ~12389/12458/12712/12719). `serviceContracts.deliveryManager.deps`: `escalationWaitTimeout.setting="constructionEscalationTimeout"`, `interventionMode.setting="constructionInterventionMode"`.
- `server/appgen.json` `variantHookArgs`: delete `gitActivityStatusAccess/GitLocal`, `designSessionAccess/GitLocal`, `activityExecutionAccess/GitLocal`, `sourceControlAccess/GitLocal`, `episodeAccess/LocalFS`, `messageBus/Temporal`.
- `server/internal/manager/{delivery,operations,billing}/*manager.go`: `func ExecutionKinds() map[messagebus.ExecutionKind]messagebus.KindBinding` moved from `cmd/server/hooks.go:1152-1197`; `constructionExecutionKinds` (hooks.go:1212) reads `delivery.ExecutionKinds()`.
- `cmd/server/hooks.go`: delete :965-993, :1152-1197, :1292-1298.
- Run `make gen-models gen-temporal` (regenerates main.gen.go/config.gen.go and now `internal/scenariocompose/compose.gen.go`); keep `hooks_test`/`config_parity_test` green; `make method-check`.
- Tests: `TestExecutionKinds_EveryManagerExports`, `TestScenarioCompose_EmitsForOwnModel` (`composegen.EmitScenarioCompose` over own project.json; until 2.11e the ONLY remaining returned refusals may be `deliveryManager`'s `repo`/`repoBase` — assert exactly that set; refusals do not abort `make gen-models gen-temporal`, they are reported).

- [ ] **Step 1: Write the failing tests.** **Step 2: Run** — FAIL. **Step 3: Release + pins** (record blockedActions if the push is refused; never commit a `replace`). **Step 4: App edits + regen.** **Step 5: Run** `GOWORK=off go test -p 1 ./cmd/server/ ./internal/manager/... ./internal/resourceaccess/projectstate` + `make method-check` — PASS. **Step 6: Commit** `git commit -m "feat(app): model-derived local profile — git-repo infra, Dep.Setting, Manager ExecutionKinds; hooks.go loses local-profile args"`.

**Status 2026-11-07 (2.11d, local-only run: Step 3 release/pins not done; workspace mode):** Steps 1-2, 4 and 6 done. Seven settings were added to project.json `slots.6.model.deployment.settings` (`webDistDir`, `devRoles`, `devSubject`, `localExecRunTimeout`, `stateMcpBin`, `webappOrigin`, `webappAssetVersion`) because composegen `resolveBrowserSurfaces` requires `webDistDir`, and configgen's `UndeclaredEnvReads` gate (FIX-049) requires every `<PREFIX>_*` env literal in the hand composition root to be declared. `webappOrigin`/`webappAssetVersion` now carry the hand defaults (`http://localhost:5173`, `dev`) and `appHooks.MCPMount` reads them through `Config`; `devRoles`/`devSubject`/`stateMcpBin`/`localExecRunTimeout` are declared with empty defaults while the hand code still reads them with its own defaults (see Earmark E9). `cmd/devstack` added to `allowedCmd` (generated launcher). Step 5 is NOT fully green; remaining failures and owners: (a) `./internal/manager/delivery` fails every scenario at `arrange StageArtifactForReviewOnBranch operationalConcepts` with PM-DEPLOY-REF "unknown component" (hooks test stages the own deployment model before any serviceContracts exist; platform HEAD's Tier-0 Load refuses it; reproduced at base 0570521d) - pre-existing, see Earmark E8; (b) `./internal/resourceaccess/projectstate` `TestAdmit_PassesOwnProject` and `make method-check` (`TestMethodDesignArtifacts`) fail only on the unmigrated v1 testPlan envelope (also at base) - owed to Task 2.11 Step 3 data migration; (c) `TestSeatedWorkflowsEqualTheTemplates` and `TestCatalogScopedOpsArePinned` in `./internal/` fail (untouched by this diff; the base tree does not compile in workspace mode, so no comparison exists) - owed to the Task 2.11 resume to triage. Platform commit 5ba2d9cb (`fix(testgen): discriminate required no real pass`) in this range is Task 2.13/2.13b work and is reviewed under that ledger.

### Task 2.11e: `sourceControlAccess` project-repo verb; `deliveryManager` drops `repo`/`repoBase` (app; senior-developer contract design)

**Basis:** `hooks.go:1322-1365` hands the Manager a policy closure and a derived string — a Phase-1 defect. The resolver's truth table (catalog when credentials exist; GitLocal on the local profile; otherwise none) is the one `FinalizeSourceControlAccess` applies, so it belongs as a verb on `sourceControlAccess`, which the Manager already depends on.

**Files:**
- `.aiarch/state/project.json` `serviceContracts.sourceControlAccess`: new op in gross terms `ProjectRepo(ctx, projectID) (RepoRef, webURL string, ok bool, err)` (name per the contract's verb conventions; `readOnly: true`); `deliveryManager.deps` drop `repo`, `repoBase`; dynamic views of the use cases that read the repo gain the call (CC-* must pass). Record through the state verbs / data edit per B11.
- `server/internal/resourceaccess/sourcecontrol/*`: GitLocal variant uses `GitLocalRepoRefForProject`; GitHub variant uses its catalog; central construction-repo fallback from `agenticJobAccess` or settings — no closure, no derived string.
- `server/internal/manager/delivery/*`: call the verb where `repo`/`repoBase` were used.
- `cmd/server/hooks.go`: delete :1322-1365.
- Tests: `TestProjectRepo_TruthTable` (pins catalog/local/none exactly as hooks.go:1322-1365 did), `TestScenarioCompose_EmitsForOwnModel` now asserts zero refusals; delivery scenario tests for the affected use cases regenerate and pass.

- [ ] **Step 1: Write the failing tests.** **Step 2: Run** — FAIL. **Step 3: Implement** (contract first, then RA, then Manager, then hooks deletion, then `make gen-models gen-temporal gen-tests`). **Step 4: Run** `GOWORK=off go test -p 1 ./internal/resourceaccess/sourcecontrol ./internal/manager/delivery ./cmd/server/` + `make method-check` — PASS. **Step 5: Commit** `git commit -m "feat(sourcecontrol): ProjectRepo verb replaces the composition-root repo closure; deliveryManager depends on the verb"`. Then resume Task 2.11 at Step 3 (`make gen-tests`) after cherry-picking 63129ac4 from `wip/fpc-2-task-2-11` onto the task branch.

### Task 2.11f: scenariocompose emits client constructors for generated-only client packages (platform: cmd/appgen, composegen)

**Defect (found by the 2.13 proof):** `cmd/appgen/main.go:164-169` `wireOnlyBuilt` drops any contract whose package holds only `.gen.go` files; REST and MCP client packages are fully generated, so `composegen.classifyClients` never sees `todoApi`/`todoMcpServer`, no `New<Client>` constructor is emitted, while testgen's `client_scenarios.gen_test.go` calls `scenariocompose.NewTodoApiList` / `NewTodoMcpServerList` → `go vet` fails in the conformance chain.

**Files:** `framework-go-app-generator/cmd/appgen/main.go` (`wireOnlyBuilt`), `composegen/scenariocompose.go` (`classifyClients`), tests in `composegen/scenariocompose_test.go` and `cmd/appgen`.
**Shape:** a Client contract is "built" when its generated package exists (generated-only is the normal state for machine clients); `wireOnlyBuilt` keeps client contracts whose `goPackage` directory contains `*.gen.go`; `EmitScenarioCompose` emits one `New<Client>(t, h) <pkg>.<Iface>` per machine client over the generated handlers + the generated authz rows (as Task 2.8's `TestMCP_NoPermitAll` expects).
**Tests:** `TestWireOnlyBuilt_KeepsGeneratedOnlyClients`, `TestScenarioCompose_EmitsMachineClientConstructors` (todomvc fixture: `NewTodoApiList`, `NewTodoMcpServerList` present), and the conformance proof's `testgen` subtest extended to `go vet` the generated test package against `compose.gen.go` in a temp module with local replaces (`TestConformance_RUN021Proof/vet`).

- [ ] **Step 1: Write the failing tests.** **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4: Run** `GOWORK=off go test ./composegen ./cmd/appgen` + `go test -tags conformance ./conformance -run 'TestConformance_RUN021Proof/(derive|methodcheck|testgen|vet)'` — PASS. **Step 5: Commit** `git commit -m "fix(appgen,composegen): generated-only client packages are built; scenariocompose emits machine-client constructors"`.

### Task 2.13b: Strong discrimination proof — harvested implementation bodies; discriminate nits (platform)

**Basis (review of 5ba2d9cb):** on the null-build fixture the "real subject" is itself all-null, so `TestConformance_RUN021Proof/discriminate` only proves "no binding is satisfied by zero results"; every behaviour-asserting scenario is `TP-BROKEN` there and never nulled, and the helper discards the broken count. Spec W8 already calls for a release-tag job with harvested `impl/` bodies.

**Files:**
- Create: `framework-go-app-generator/conformance/testdata/todomvc/impl/**` — the RUN-021 harvest's hand-written component bodies (`archistrator-bench-scratch/archive/run-20261105T141816Z-04ce59ca/harvest/app/server/internal/**` non-generated `.go` files for list-manager, engines, todo-list-access, message-bus; and `cmd/server/hooks.go`), adapted only where binding model v2 / Plan 2 generators changed an interface; README records each adaptation.
- Modify: `conformance/chain.go` — `RunChainOn` gains an option `WithImpl(fs.FS)` that overlays `impl/` in place of `null.gen.go` (and the nullgen `hooks.go`) before `go build`; `conformance/run021_test.go` — the `discriminate` helper returns `(scenarios, nullRuns, survivors, broken)` parsed from the summary line `discriminate: N scenario(s), M null run(s), S survivor(s), B broken`; the null-build subtest asserts `broken == <expected count>` and `survivors == 0`; a new `discriminate_impl` subtest runs with the overlay and asserts `broken == 0 && survivors == 0 && scenarios == <generated list-manager count>`.
- Modify: `cmd/testgen/discriminate.go` — `[no test files]` with exit 0 is `ranInconclusive`; `cmd/testgen/main.go` doc for `-discriminate` states the real-subject pre-run, `TP-BROKEN`, exit codes and the inconclusive class. Test: recorder case `TestDiscriminate_NoTestFilesIsInconclusive`.
- Optional (info): arch/methodcheck rule refusing a hand-written workflow `Name` with the `Null` prefix.

- [ ] **Step 1: Write the failing tests.** **Step 2: Run** `GOWORK=off go test ./cmd/testgen -run TestDiscriminate` and `go test -tags conformance ./conformance -run 'TestConformance_RUN021Proof/(discriminate|discriminate_impl)' -timeout 40m` — FAIL. **Step 3: Implement.** **Step 4: Run** — PASS. **Step 5: Commit** `git commit -m "test(conformance): discrimination proof over harvested RUN-021 bodies; discriminate treats no-test-files as inconclusive"`.

**2.13 run-time limits recorded by the fixture README (verify in the chain proof after the release):** the dialect has no type-and-submit (add/reword keep a bare `fill` + `press Enter` as two steps); `TestPrincipal` carries no delegation claim, so agent-actor happy paths may be refused at run time — if so, extend `TestPrincipal` with `onBehalfOf` (schema + rules) as Task 2.11g; Manager scenarios called from a Client run with an anonymous context.

**Earmarks (architect, not in this wave):** E1 cloud RA→RA ports via hooks (hooks.go:937-962) look like a sideways RA dependency; E2 cloud github-app arms share an AppClient via hooks against the per-variant convention; E3 non-identity `Finalize*` on the local profile (artifact, agenticJob, sourceControl, messageBus) means devstack ≠ in-process — resolve before Plan 3 claims parity (the local executor should be a declared local variant of agenticJobAccess); E4 extend the local-hooked refusal to `composegen.Generate`; E5 derive the kinds table from WorkflowDecl + dynamic views; E6 scenariohost github-app over FakeGitHub; E7 one database per Postgres key. E8 delivery `manager_hooks_test` staging order: it stages the own deployment model (`operationalConcepts`) before any `serviceContracts` exist, which platform HEAD's Tier-0 PM-DEPLOY-REF refuses (owner: Task 2.11 resume, before `make gen-tests`). E9 two sources of truth for composition-root env values: `devRoles`/`devSubject`/`stateMcpBin`/`localExecRunTimeout` are declared in the model with empty defaults while `hooks.go`/`config_adapter.go` still read them via `getenvString` with their own defaults (`drive-phase approve-artifact`, `dev-architect`, build-on-demand, built-in budget) - route them through `Config` and move the defaults into the model. E10 `newTemporalMessageBus` merges one `ExecutionKinds` table per Manager and a kind declared by two Managers silently last-wins - refuse a duplicate at boot (`fwra.ContractMisuse` naming the kind) with a unit test.

---

### Task 2.12: method-assets prompts, lifecycle text, Makefile, scaffold

**Files:**
- Modify: `method-assets/assets/claude/commands/service-test-plan.md`, `frontend-test-plan.md` — the binding is a **record**: `listComponentScenarios` → for each stimulus write `inputs` (literal or `$ref`), `expect`, `probes`; declare `principals` per actor; `arrange` from the four kinds; "there are no hooks" `[TP-HOOK]`; "every step asserts" `[TP-EXPECT-REQUIRED]`; targets are design element ids `[TP-UI-TARGET]`; remove every mention of `hook: true`, `unobservable`, `click` vs `check` (the role check now enforces it).
- Modify: `frontend-detailed-design.md` — produce the typed `UIDesignRecord` (screens → elements with role/name/within/repeats) `[UID-*]`; Layer A interaction policy stated as platform-fixed with the rule ids that enforce it (`surface check`, latency harness).
- Modify: `service-integration.md`, `frontend-integration.md` — delete steps 4–5 (hook filling) and the "wrong hook is yours" paragraph; integration runs `make gen-tests gen-tests-check test-scenarios test-discriminate` and `surface-check`; "you write no test code" `[ARCH-TEST-COMPOSE]`.
- Modify: `service-construction.md`, `frontend-construction.md` — "no tests" already; add `[ARCH-TEST-COMPOSE]` citation; frontend: elements carry the design's role and accessible name `[UID-NAME, surface check]`.
- Modify: `skills/the-method-testing/SKILL.md` §2–§4, R5 — "the integration agent wires; it writes no test code"; R4 note that over-the-wire client tests arrive in Plan 3.
- Modify: `assets/scaffold/Makefile.scenarios.mk.tmpl` (`test-discriminate`, `surface-check`), `aiarch_arch_test.go.tmpl` (`arch.CheckTestCompose`), `golangci.yml` unchanged (gochecksumtype already on).
- Test: `method-assets` has a prompt-lint test (`TestDumpRenderedWorkflowsForLint` style) — add `TestPrompts_NoHookOrUnobservableMentions` asserting the strings `hook: true`, `unobservable`, `hooks.ts`, `_hooks_test.go` appear in no command/skill.

- [ ] **Step 1: Write the failing test.** **Step 2: Run** `cd method-assets && GOWORK=off go test ./ -run TestPrompts_NoHook -v` — Expected: FAIL. **Step 3: Edit the prompts.** **Step 4: Run** — PASS. **Step 5: Commit** `git commit -m "docs(method-assets): prompts describe the declarative binding record; no hooks anywhere; Makefile and scaffold gates"`.

---

### Task 2.13: Proof — replay RUN-021 through the new chain

**Files:**
- Modify: `framework-go-app-generator/conformance/testdata/todomvc/project.json` (migrate its testPlan + uiDesign to v2 by hand — this *is* the rebinding exercise for one real app; README records every judgement), `conformance_test.go`

- [ ] **Step 1: Write the test** `TestConformance_RUN021Proof` (tag `conformance`):
  - `scenario.DeriveReport` over the fixture: `reword-a-todo-P1` is present with ≥1 stimulus into `todo-web-app` after the fixture's use case is amended per `UC-START-NOT-ACTION`/`CC-DECIDED-BY-ACTOR-STEP` (amend the fixture diagram: n1 becomes `action` in the Team Member lane with a call to `todo-web-app`; n2's step carries the Escape call; n3 becomes a `sendSignal` to the team member). `Problems` is empty.
  - methodcheck over the fixture: zero Error findings (no waivers in the fixture).
  - `RunChain` succeeds; `test-discriminate` over `list-manager` reports zero survivors; the previously vacuous `discard-a-todo-P1` binding, if left unchanged, is listed as a survivor (assert on a copy of the fixture with the old binding).
  - the generated `mark-a-todo-completed-or-active-P1.spec.gen.ts` contains `toBeChecked(immediate)`.
- [ ] **Step 2: Run** `GOWORK=off go test -tags conformance ./conformance -run TestConformance_RUN021Proof -v -timeout 40m` — Expected: FAIL until the fixture is migrated.
- [ ] **Step 3: Migrate the fixture.** **Step 4: Run** — PASS. **Step 5: Commit** `git commit -m "test(conformance): RUN-021 replayed through binding model v2 — Escape path derived, vacuous delete caught, toggle asserted immediately"`.

---

### Task 2.14: PM coverage view on the core-use-cases critique (blocking sendBack)

**Files:**
- App: `server/internal/manager/delivery/` design-rail query (`queryProjectView{kind:"useCaseCoverage"}`), `server/internal/engine/review/reviewengine.go:427-470` (`requiresHuman`), `webApp/src/.../UseCaseCoveragePanel.tsx` (new, read-only), `method-assets/assets/claude/commands/core-use-cases-critique.md`, `agents/product-manager.md`.

**Interfaces (Produces):**
```go
type UseCaseCoverageView struct { UseCases []UseCaseCoverage }
type UseCaseCoverage struct { UseCase string; Paths []PathCoverage }
type PathCoverage struct { Scenario string; Guards []string; Door string /* client of first actor stimulus */; OutcomeNodes []string /* sendSignal/objectNode labels, verbatim */; Problems []string /* SCN-* codes */ }
```
- Derived on read from `scenario.DeriveReport` (nothing stored). Rendered in the critique prompt as a table and in the webApp on the core-use-cases review task.
- `requiresHuman`: for slot `coreUseCases`, a PM `sendBack` verdict holds the gate under every preset including `vibes` (the floor gains `pmCoverageSendBack`); the operator cannot approve over it without a recorded `override` intervention with a reason.
- Tests: `TestCoverageView_ListsDoorAndOutcomesPerPath` (reword_abandon fixture → P1 has `Problems: [SCN-ACTOR-PATH-CLIENT]` before Task 2.3 amendments), `TestRequiresHuman_PMSendBackHoldsUnderVibes`, `TestCritiquePrompt_RendersCoverageTable`.

- [ ] **Step 1: Write the failing tests.** **Step 2: Run** `GOWORK=off go test ./internal/engine/review ./internal/manager/delivery -run 'TestCoverageView|TestRequiresHuman_PM' -v` — FAIL. **Step 3: Implement** (view in the delivery Manager's existing design query path; floor in reviewengine; panel in webApp; prompt). **Step 4: Run** — PASS; `npm run typecheck` in webApp — PASS. **Step 5: Commit** `git commit -m "feat(review): use-case coverage view for the PM critique; PM sendBack holds the core-use-cases gate under vibes"`.

---

## Done when

- One platform release (framework-go, app-generator, scenariohost, method-assets) tagged with Plan 1's conformance job green, including `TestConformance_RUN021Proof`.
- App pins bumped; `make test` green; `make method-check` shows only waived Warnings; no `*_hooks_test.go` or `hooks.ts` remains in the app.
- `archistrator-first-pass-correctness` memory updated with the tag versions and the waiver expiry.
