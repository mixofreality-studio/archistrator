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
- `Output.HooksOnce` is deleted from `testgen.Output`. `Drift` loses `hooksDrift`/`symbolDrift`.
- Machine clients (`emit_mcp.go`/`emit_rest.go`): subject = `serveTools(t, h, scenariocompose.New<Iface>(t, h))` with `permitAll` **removed** — the generated server uses `scenariocompose.Security(h)` (the app's generated PDP over the dev principal from `Principals`). Over-the-wire execution against devstack is Plan 3; here the in-process server must at least use the real generated authz rows.

- [ ] **Step 1: Write the failing tests:**
```go
func TestGo_NoHooksFileEmitted(t *testing.T)
func TestGo_PrincipalFromPlan(t *testing.T)            // generated source contains `ManagerContextAs(t, principal("team-member"))`
func TestGo_RefThreading(t *testing.T)                 // `in2.TodoId = out1.Id`
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
- `hooks.ts` is no longer emitted; `Drift` reports any `hooks.ts` as orphan.

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
