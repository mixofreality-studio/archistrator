# First-Pass Correctness — core design flaws behind "all components green, app fails"

**Date:** 2026-11-06 · **Status:** draft for founder ruling · **Scope:** archistrator (app), archistrator-platform (framework-go, app-generator, method-assets, scenariohost), archistrator-bench (oracle only, not fixed here)

**Inputs.** `archistrator-bench/analysis/todomvc/2026-11-06-loop-status.md`; run archives RUN-017/019/021 under `archistrator-bench-scratch/archive/`; the fix ledger FIX-028..067; three code sweeps over the loop worktrees (`benchwt/loop/{archistrator,archistrator-platform}` = origin/main b31bc72f / 9784892d); three system-architect rulings and one product-manager opinion taken today; the DCT spec `2026-10-02-deterministic-component-testing-design.md` and its amendments A/B.

**Goal (founder).** The framework must force agents to write correct code: *if the gates pass, the app works.* Fix the core, not the symptom.

---

## 1. The answer

RUN-021 delivered 11/11 activities with every component's scenario tests green, and the independent browser suite found 7 of 11 user behaviours broken (plus one missing surface). This is not a gap in a prompt. It is the predictable output of the current verification design:

1. **The thesis is false.** DCT §1 / T5 says "there is no system test; the union of passing per-component scenario runs is the proof that the app works." Löwy ch. 4: *the feature is always an aspect of integration, not implementation.* RUN-021 is the counter-example.
2. **The oracle never reaches the actor.** The use cases were a sufficient oracle (RUN-021's use cases covered all 11 behaviours), but the derivation from use case to executable test drops the user-visible outcome: after Amendment A1 every component-to-component call is a stimulus, and expected outputs/states attach to the *last* stimulus, which is usually a ResourceAccess call. The client test ends up asserting nothing the user sees. The web emitter then emits probes, `unobservable` and `errorExpected` as comments.
3. **Agents own both the action and the assertion.** `hook:true` is unrestricted and, in Playwright, runs *between* the action and the generated `expect`. RUN-021: 33/33 SPA steps, 45/45 Manager, 45/45 MCP, 33/33 REST steps were hooked. The SPA hook awaited every list request plus 300 ms of quiet before any assertion, which is exactly what hides a checkbox that flips only after the server answers. In Go the hook *returns the result* that `expect()` judges, so a hook can return the expectation without calling the component. Hooks are not agent laziness: the generated `callContext` passes a zero principal and the binding language has no step-output references, so `hook:true` is the only legal binding for most steps.
4. **Expectations are optional.** Empty `Expect.Result` means "no error only" (10 of 11 messageBus steps). `{}` subset-matches anything. `unobservable` satisfies TP-STATE. RUN-021's delete test asserted a count that was already true before the delete; nothing compares against the pre-state.
5. **The dialect cannot say what the user sees.** Targets are global `data-testid`; no role/name, no row scoping, no count/absence, and every observable auto-retries, so "eventually correct" passes. A title rendered as a `<button>` passes every testid assertion.
6. **The behaviour-bearing design artifact is prose.** `UIDesignRecord{Surface, Content string}`. RUN-021's design mandated "P3 No optimistic mutation", traced to no use case, while the construction prompt (FIX-062) says the opposite. Nothing typed could catch the contradiction; under `vibes` nobody reads it.
7. **The composed app is never run before "delivered".** Composition checks compile (`-o /dev/null`). REST/MCP client scenarios run handlers in-process under `permitAll`, so the composition root's real policy (RUN-015's 403) is never exercised. Only the SPA agent's hand-written hooks happened to boot devstack.
8. **The use cases are written from the server's side.** (PM) "Mark a todo completed" is "set the one todo's completed state → end"; the change is sent to "Other Surfaces" and never back to the surface that asked. "Immediately" and "at a glance" from the brief appear on no node. The PM's critique ratified it ("no customer expectation breaks") because nothing showed which outcome the member sees, on which surface. **Silent derivation holes.** RUN-021's reword use case models the Escape-abandon path as `start → decision(decidedBy: actor) → note → merge → end`. The actor's action is the `start` node, the outcome is a `note`, no step carries a call, so the path yields zero stimuli and `ForComponent` drops it without a finding. Filter-all/filter-completed likewise had no test.
9. **Upstream, writes are admitted by shape, not by their consumers**, and generators were never exercised on a fresh project. 15 of the 40 fixes (5 "record-time validation too loose" + 10 "generator defect/omission") were each discovered by a paid run.

Point 1 is the root. Points 2–8 are the mechanisms through which an incorrect app satisfies every gate (8 covers both the server-side use cases and the silent drop). Point 9 is why reaching acceptance cost 13 paid runs.

---

## 2. Evidence summary

| Claim | Where |
|---|---|
| Thesis / T5 | DCT spec §1; `method-assets/.../the-method-testing/SKILL.md:30-32` |
| Outcome attaches to last stimulus (RA) | spec §11 A1 text; `framework-go/scenario/derive.go` `stimulusWalk.cur()`, `observe()` |
| Web probes/unobservable/errorExpected are comments | `app-generator/testgen/emit_playwright.go:84-89,150-151` |
| Hook runs between action and assert | `emit_playwright.go` step emission; stub text "perform step … and assert its observables" |
| Go hook returns the judged result | `emit_go.go:277-295` (`outN, errN = hook(...)` then `expect`) |
| Zero principal forces hooks | `emit_go.go` `callContext`; `frontend-test-plan.md` "no step-output reference … mark hook: true" |
| Empty Expect = no-error only | `emit_go.go:591`; `rules_testplan.go:613-615` |
| Dialect: testid only; auto-retry | `methodcheck/uidialect.go`; `emit_playwright.go:129` |
| UI design is prose | `projectstate/contract.gen.go:1034-1038` |
| Composition compile-only; permitAll | `projectstateaccess.go:12919-12933`; `emit_mcp.go:343-349` |
| Reword Escape path dropped | RUN-021 `harvest/project.json` use case `reword-a-todo` (nodes n1–n3 carry no step); generated specs are P2/P3 only |
| RUN-021 counts | plans: todoWebApp 33/33 hook, listManager 45/45, todoMcpServer 45/45, todoApi 33/33; final SPA check run 24/24 pass |
| Acceptance after harness fixes | ledger L89-90: 7/15 ($0 replay); remaining 7 are the `.check()` tests + MCP Apps |
| Fix tally | ledger FIX-028..067: generator 10, harness/host 8, orchestration 7, record validation 5, gate scope 3, prompt 3, shape-not-behaviour 2, hygiene 2 |
| Design nondeterminism | RUN-015..023 component sets: MCP Apps client present in 5/9, REST client absent in 2/9 |

Two caveats on the evidence. (a) Part of the 7 `.check()` failures may be the bench suite's choice of `locator.check()` (which requires a synchronous flip) against a design that deliberately chose server-confirmed state; the delivered `TodoItem` also sets `disabled={busy}`. The architect's ruling is that this must not matter: acceptance derives from the use cases, not from the bench. (b) The loop-status doc's "required behaviours" line is wrong: that artifact was retired 2026-08-30 (ch. 4) and no run committed it. The use cases are the oracle, and this plan keeps it that way.

---

## 3. Design principles (what "solid" means)

- **PR-1 The use case is the oracle, mechanically.** Every elementary path of every use case yields a falsifiable scenario, observed at the actor's surface, run against the composed app. A path that yields no actor stimulus or no actor-observable outcome is a design error, never a silent drop.
- **PR-2 The platform owns the action and the assertion.** Agents supply data (bindings, arrange calls, principals), never code that runs inside a test. There are no hook bodies.
- **PR-3 Every assertion must be able to fail.** Expectations are mandatory, discriminating (pre/post differential), and proven by a null-subject mutation run.
- **PR-4 Anything the platform requires of a built app is a rule in code.** Prompts explain rules; they never carry them. A registry ratchets prompt-only doctrine to zero.
- **PR-5 The composed app is the unit of "integrated".** Every integration boots the whole system through its generated run contract; machine clients are tested over the wire with the real policy.
- **PR-6 Artifacts are admitted by their consumers.** A write is refused if any generator that reads it cannot load it. Ahead-of-time plans (manifests) are the single source for edit scope and drift.
- **PR-7 Everything generated is proven at $0.** A platform conformance pipeline pushes fixture projects through scaffold → generators → build → boot → wire scenarios on every PR, so generator defects never reach a paid run.

---

## 4. Workstreams

Each workstream names the ruling it rests on (SA-1: oracle/derivation/composition; SA-2: anti-cheat/dialect/UI design; SA-3: admission/manifest/pipeline; PM: product manager) and the concrete shape. Rule ids are proposals; final ids follow methodcheck conventions.

### W1 — Amendment C to DCT: system acceptance is back, as a platform gate (SA-1, founder ruling required)

Revises DCT §1 thesis and T5; §7's N-IT removal stands (no agent-run system-testing activity returns).

- `scenario.ForAcceptance(project)` in `framework-go/scenario`: every path, actor-originated stimuli only (`Kind != "call"`), carrying `ActorOutcome` (W2).
- **N-ATP** (use-case acceptance test plan): a test-engineer activity, parallel with construction, depends on `.coreUseCases`, `.systemDesign`, committed `uiDesign`. Output `AcceptancePlan{Bindings []ScenarioBinding}` keyed by unprojected scenario id, reusing `ScenarioBinding`/`StepBinding`. Added to `estimationengine` derivation and `lifecycles.json`.
- **M-ACCEPT**: a zero-agent-effort node feeding the project-end milestone; its gate (DeliveryManager `runGate`, new `milestone:acceptance` lifecycle task) requires a TestRun at the composed revision covering every acceptance scenario, all pass, artifacts render.
- testgen gains `emit_acceptance_{playwright,http,mcpwire}`, run against the booted devstack (W5).
- Failure routing: the gate comment names the components on the failing path's dynamic view; the scope-change rail reopens the activity. A red light with no route is not a gate.
- Size it to what the client projection cannot cover once W2/W5 land: cross-client paths (MCP write seen in the SPA), event-entered paths (timeEvent, queued Manager→Manager), and outcomes observed at a later stimulus ("come back intact" = reload / second session). Do not duplicate the client suite.

### W2 — Derivation integrity: outcomes reach the actor; no silent drops (SA-1)

- `Stimulus.ActorOutcome{ExpectedOutputs, ExpectedStates, Hints}` filled in `observe()`: every `sendSignal`/inferred output to an actor and every `objectNode` attaches to the nearest preceding *actor-originated* stimulus. The A1 attachment (`Outcome`) stays for RA/Engine tests. Client projections and `ForAcceptance` carry `ActorOutcome`; Engine/RA projections ignore it.
- `Stimulus.ExpectedOutputs[].Immediate` derived: true when the actor's expected output follows the actor's action with no system call between them on the diagram (feeds W4's latency harness).
- **CC-DECIDED-BY-ACTOR-STEP** (Error): a decision/switch with `decidedBy` = actor must carry a step with an actor→Client call (the guard is a client action). This is the RUN-021 Escape path.
- **SCN-UNADDRESSED** (Error, in `DeriveReport` and design health): an input stimulus with `To == ""` or to a non-contracted id is a defect, never dropped.
- **SCN-ACTOR-PATH-CLIENT** (Error): every path containing an actor-classified node yields ≥1 stimulus into a Client.
- **UC-PATH-OUTCOME** (Error): every path from an actor stimulus to `end` carries ≥1 actor-observable outcome (`sendSignal` or `objectNode`), so a `note` cannot stand in for the result (RUN-021 n3).
- **UC-START-NOT-ACTION** (Warning→Error after migration): a `start` node in an actor lane with an action-like label is refused; the actor's action is an `action` node.
- **UC-OUTCOME-AT-ASKING-SURFACE** (Error): an actor-observable outcome must be delivered to the actor through the Client that received the stimulus (PM: RUN-021's "Work the List" sent the change to "Other Surfaces" and never back to the surface that asked, so "the member sees the box checked and the count drop" had no node to live on). Cross-surface propagation is additional, never a substitute.
- **TP-CLIENT-CONTRACTED** (Error): every Client has a contract (B1 intent, now enforced).
- **PM coverage view (render-on-read, not a slot).** The core-use-cases critique renders a platform-computed view: every use case × path × guards × the outcome node(s) × the surface (door) it runs through, plus a reverse list of paths. The PM maps the brief's clauses onto it in the critique and may send back on "clause with no observable outcome node" or "path with no clause" (invented scope: RUN-021's access-refusal branches and live cross-surface sync were never asked for). Timing/visibility qualifiers from the brief ("immediately", "at a glance") must appear verbatim on the outcome node. Nothing is stored beyond the use cases themselves; this does not reinstate Required Behaviors. Under `vibes` the PM's sendBack on this critique must still block (today `required:false` and the operator approved over it).
- Re-check RUN-021's reword and filter diagrams against these rules as the fixture for the golden test.

### W3 — The anti-cheat test model: no hook bodies, mandatory discriminating expectations (SA-2, SA-1)

**Model changes (`$defs` in projectstateaccess; modelgen regen; own project.json conformed per B11):**
- Delete `StepBinding.Hook`. Replace with:
  - `TestPlanRecord.TestPrincipals{actor → {subject, roles}}`: the actor lane maps to a principal; generated `callContext` carries it; web/REST/MCP harness sets the dev principal from it.
  - `TestArg.Value` may be `{"$ref": "step<seq>.result.<jsonpath>"}` or `probe<seq>.<n>.result…`. **TP-ARG-REF**: the referenced step is earlier and the path exists in that op's result schema. Testgen threads it.
  - `ScenarioBinding.Arrange []ArrangeCall{component, operation, inputs, expect}`: real contract ops emitted before step 1 (direct call in Go; generated REST client for web). **TP-ARRANGE**: op exists, args type-check, and arrange may not call the scenario's own stimulus op on the component under test.
- Delete `StepBinding.Unobservable` (Amendment A7: an output no surface can observe is a design defect; add the read-only op).
- `ContractOperation.ReadOnly bool` derived from verb class. **TP-PROBE** requires `ReadOnly`, a non-empty probe `Expect`, and that the probe op's component touches `For`'s object in the dynamic view.

**Rules (all Error, all on the non-overridable floor):**
- **TP-HOOK**: no binding carries `hook`; testgen refuses to emit a hooks file with step/probe stubs.
- **TP-EXPECT-REQUIRED**: every non-error step has a non-empty `Result` or ≥1 probe with non-empty `Expect`; `{}`, `[]`, `null`, zero-observable UI arrays are refused (`ParseUIObservation("[]")` is an error).
- **TP-ACTOR-OBSERVED**: on a Client stimulus every `ActorOutcome` output/state is bound to an observable or a probe.
- **TP-FALSIFIABLE** (emitter property, golden-tested): every asserted observable is evaluated before and after the stimulus; the generated test fails as "vacuous" if the post-condition already held. Bindings may supply an explicit `Before` observable when the differential is wrong.
- **TP-DISCRIMINATE** (null-subject mutation): testgen also emits, per step, a variant where the stimulus op is a generated null implementation (zero result, nil error; for web the route returns 2xx with no state change). Each scenario must fail under at least one null variant of its own stimulus ops. Go layers: inside `gen-tests-check`. Web: in the testing job.
- **TP-UI-ACTION**: every web stimulus has exactly one action input; non-action inputs are refused, not commented.
- Web probes execute through the app's generated REST client (same server, same principal); `errorExpected` web steps must carry the UI observable of the error.

**Composition for tests (SA-2 Q6):**
- app-generator emits `compose.gen.go` from the same model as `main.gen.go`: scenariohost-facing constructors (`NewOrderManager(h *scenariohost.Host) contract.OrderManager`) with production wiring over the host's substrates. `newSubject`, cross-component probes and `replayWorkflows` are generated; nothing is FILL.
- **ARCH-TEST-COMPOSE**: a scenario package may not declare any type implementing a contract interface.
- The hooks drift check pins bodies, not symbols: the only surviving test file is byte-equal to its generated form.

**Author separation by edit scope (SA-2 Q5).** The lifecycle graph needs no change (B12 fork/join holds). Construction and integration exclude `uitests/generated/**`, `uitests/*.ts` (except lockfile), `**/*_scenarios.gen_test.go`, `**/*_hooks_test.go`; `stp` writes only `.phaseArtifacts.testPlan[*]`. Enforced in the job's post-run cumulative diff (path allowlist per task kind). `hooks.before` never receives `page`; arrange runs over the machine client, then the spec opens a fresh context. SKILL.md R5 and `frontend-integration.md` steps 4–5 are rewritten accordingly; `reportPlanDefect` stays, bounded to two rounds, and a re-derived binding must pass TP-DISCRIMINATE before it stands.

### W4 — Semantic dialect, typed UI design, platform interaction policy (SA-2, PM)

- **Target** = `{role, name?, within?: Target, nth?, exact?}` with `role` from a closed ARIA enum in `uidialect.go` (button, checkbox, textbox, listitem, list, link, heading, status, dialog, …). Emits `getByRole(...)` scoped by `.filter({has})`/`within`. `testid` leaves the dialect: the design element id (below) is the binding key and the locator derives from its role/name. A plan never names the DOM.
- **Observables add** `count{target,n}`, `absent`, `hidden`, `enabled`, `disabled`, `value{target,text}`, `focused`. Each states one thing.
- **Immediate outcomes are discriminating, not timeout-0.** The generated Playwright harness installs `page.route` delaying every app→server request by a fixed amount (e.g. 1500 ms); observables derived `Immediate` (W2) assert with a timeout well below it (e.g. 250 ms); eventual outcomes keep the normal timeout. This is the optimistic-update check RUN-021 lacked, with no prose involved.
- **Typed UI design**: `UIDesignRecord{Surface, Screens[]{id, route, elements[]{id, role, name, within?, repeats?}}, Content}`; `Content` is narrative only.
  - **TP-UI-TARGET** (Error): every action/observable target resolves to a design element and the role agrees with the action (`check` → checkbox/switch; `fill` → textbox; `count` → `repeats`).
  - **UID-ROUTE**: every `navigate` value is a screen route.
  - testgen emits `uitests/generated/<surface>/elements.gen.ts` and `surface.spec.gen.ts` (run first: open each screen, assert every non-repeating element exists by role and name). This catches title-as-button before any scenario runs.
- **InteractionPolicy is a platform constant** in method-assets, in two layers (PM ruling):
  - **Layer A, always on, no design override:** a control's own state shows the user's gesture at once (a clicked checkbox is checked; a committed edit shows the new text; a deleted row is gone); in-flight calls do not disable the control or block unrelated input. The design chooses only how to reconcile (roll back with a notice, or pending style). Override requires a reason the customer stated (e.g. "a payment must not look sent until it is") traced to an objective.
  - **Layer B, app-specific:** whether derived data (counts, other rows, filtered views) waits for the server. Derived from the use case: an `Immediate` outcome (W2) on a derived element asserts it at once; otherwise eventual.
  - Rendered into the design prompt as non-negotiable; enforced by the latency harness plus a generated "unrelated input during in-flight call" check per screen. The design record has no field that can override Layer A; a design stating otherwise is contradicted by a failing generated test. UI-design review rejects untraced behaviour constraints (RUN-021's "P3" and "S-ROW-BUSY" traced to nothing the customer said).
- **Accessible semantics are a platform quality bar, always on** (PM): native roles, every control has an accessible name that says what it does, WCAG AA. The role/name enum of the dialect and the `surface.spec.gen.ts` check are the enforcement; the bench's literal locator regexes are not requirements.

### W5 — The composed app is the unit of integration (SA-1)

- **Tier 1, smoke on every integration task** (every component, not only clients), replacing the compile-only checks at `projectstateaccess.go:12919`: boot devstack through the generated run contract → `/readyz` 200 → SPA index served → `/mcp` initialize handshake → teardown (process, containers, volumes). Teardown is owned by scenariohost/devstack, never by hooks (RUN-021 leaked ~55 servers and ~55 containers; RUN-022 died of the disk).
- **Tier 2, machine clients over the wire**: `emit_mcp.go`/`emit_rest.go` run against the booted devstack with harness identity through the real composition-root policy. In-process `permitAll` already violated T2. A client activity cannot reach `integrated` until Tier 2 is green.
- **Composition-root policy gets an owner**: `ownListPolicy`-class code in hand-written `hooks.go` is today outside every edit scope. It is derived from the model (identity/authorization in the operating model) and generated by composegen; until then Tier 2 exposes a gap nobody can fix.
- Boot twice in the conformance pipeline (W8) and assert no orphaned volumes (FIX-063 class).

### W6 — Required surfaces and MCP Apps as a generated Client (SA-1, PM)

- `Surface` enum on Client components: `{web, rest, mcpTools, mcpApp}`.
- `.mission.deliverySurfaces []{kind: Surface, audience: human|machine, source: <brief quote>}`, written by the architect, ratified by the PM against the brief's wording, traced to an objective (usually Integration). It is a channel constraint, not a behaviour list, and does not reinstate Required Behaviors. (PM rejected both alternatives: mission prose cannot be checked, and an "MCP host" actor bends the use cases.)
- **SYS-SURFACE-REQUIRED** (Error): every delivery surface has ≥1 Client whose *kind* matches: a human surface needs `uiSurface: true`; `mcpApp` needs a UI surface embedded through MCP, not the MCP tool server. (RUN-021 claimed MCP Apps coverage with `todo-mcp-server`, `uiSurface: false`.)
- **SYS-SURFACE-ENTRY** (Error, per-use-case waiver): each core use case's actor entry has an `alt` call per required surface that actor uses (today's "draw both surfaces" doctrine, made enforceable).
- **GEN-SURFACE-SUPPORTED** (Error at design): a surface the generator cannot build is refused at design time. Today this refuses `mcpApp` honestly instead of failing at acceptance, and forces the generator work.
- An MCP Apps UI is a Client (own presentation volatility: host iframe, host CSP, host bridge). It is modelled symmetrically with SPA↔REST (served by and calling through the MCP tool surface), never Client→Client. The generator owns `OpUI` derivation, `ui://` resources (list/read, serving the built bundle), CSP/resource metadata, and the host-bridge scaffold; the agent owns only the UI bundle. Acceptance needs a harness host; start from the 2026-07-15 basic-host E2E.

### W7 — Admission, manifests, one identity, typed failure causes (SA-3)

**Admission in three tiers (one admission function per artifact kind; both write rails call it):**
- **Tier 0, loadability, every write, ms, refuse.** Each generator's Load-time validation moves into `framework-go-projectmodel` as `projectmodel.Validate<Kind>`; generators call the same function at Load; the projectstate RA enforces it as a write invariant (stored data's own contract, not business logic, so no up-call). Covers exported op names, layer spelling, zero operations, missing `goPackage`, unknown fields, empty statements.
- **Tier 1, emit + typecheck, on `recordServiceContract` / `recordPhaseArtifact` / stage-for-review, ~1–3 s.** Each generator exposes a pure in-memory `Emit(project) fs.FS`; the existing validation Engine emits every registered consumer (`appgen.Consumers(kind)`, platform-declared) and typechecks with `go/types`. Called by the delivery Manager (server rail) and the verb layer (agent rail) before the RA write.
- **Tier 2, `go build` / `tsc` / compose, at commit / phase-advance only** (the W8 chain on the real project).
- All findings returned at once (the one-error-at-a-time ping-pong measured 10.5% of a run). Findings split into *local* (refuse) and *incomplete* (accept with warning, enforce at Tier 2) so design order cannot deadlock.

**Manifests and scope:**
- Each generator exposes `Plan(project, selection) []Output{path, role: generated|seam}` computable before emission; a generator test asserts `Emit` writes exactly `Plan`'s generated paths.
- Edit scope = agent-owned globs ∪ seams ∪ `Plan` of the generators the task's `make gen-*` runs. Drift = regenerate to scratch and diff over the same set. One source, two readers. Generated paths in the cumulative branch diff must be byte-equal to regeneration even when in scope.
- Check results keyed `(checkID, contractKey)` end to end; component→contracts joined in exactly one function.
- Task-spec / edit-scope / check derivation leave the 13.5k-line projectstate RA for an existing Engine that consumes `Plan` (SA-3 defect 1; adding a component needs its own ruling).

**Typed failure causes (confirms the FIX-067 ruling):**
- Agentic-job result `{reachedModel bool, cause, faultClass ∈ {host, agent, gate}, gateFailures []checkID}`; host = modelUnreachable | rateLimited | runnerLost | infraTimeout; agent = agentGaveUp | noProgress | toolRefusedByPolicy; gate = gateRed; plus unknown.
- `reachedModel` is measured by the agentic-job RA from the model transport, never self-reported; a progress fingerprint is published only when true (FIX-065).
- Classification lives in the RA (substrate volatility); policy in the delivery Manager: host faults never consume attempts, bounded wall-clock backoff then human escalation; `gateRed` takes its truth from the recorded runner results, never the agent's claim; `unknown` consumes an attempt and is surfaced.
- The bench classifier reads the recorded enum, not log regexes.

### W8 — $0 conformance pipeline and the doctrine registry (SA-3)

- Fixtures under `framework-go-app-generator/testdata/conformance/<name>/` (`project.json`, bindings, optional `impl/`), starting with todomvc hand-keyed from RUN-021's harvest, plus greenfield. Each fixture is also re-recorded through the real record verbs (Tier 0+1) in an archistrator test so fixture and recorder cannot drift.
- Chain on every platform PR: scaffold → modelgen → clientgen → composegen → testgen → webgen → `go build` → `tsc` → boot devstack → `/readyz` → `/mcp` tools/list → SPA GET → generated client scenarios over the wire, with **null-subject** stubs generated from the contracts (wiring mode: routes reachable, error envelopes per contract, dev principal works). Release-tag job additionally runs with harvested `impl/` bodies.
- Coverage check: every value of the contract feature enums (layer, transport, client surface, binding kind, queued Manager→Manager, external RA) appears in ≥1 fixture.
- This pipeline gates releases of app-generator, framework-go, method-assets, and archistrator's pin bump. Goldens remain change-review aids, not correctness tests.
- **Doctrine registry** (method-assets): `{id, statement, scope: builtApp|agentConduct|judgement, enforcedBy: [ruleRef], promptRefs, waiver?{reason, owner, expiry}}`. CI: every `ruleRef` resolves to an exported rule with a violating fixture; `builtApp` entries with no enforcement fail unless waived; waiver count only decreases; every enforced rule is explained by a prompt; prompts cite ids inline so a new normative sentence without an id is linted. Seed from: optimistic controls, no IdP resource, every Manager Temporal-hosted, clients have contracts, use check-not-click, no hooks for UI actions, testids match design ids, both surfaces drawn.

---

## 4b. Builder decisions (my calls after the builder check; override freely)

- **One substrate set per test run, owned by scenariohost.** scenariohost provisions Temporal, Postgres, FakeGitHub and the git repo, then launches the generated `devstack` with those substrates injected through its env contract. In-process Go component scenarios use `compose.gen.go` over the same substrates (same wiring as `main.gen.go`). Client scenarios and their probes go over the wire (REST/MCP), never direct calls, so stimulus and probe always see the same state. The host's doubles are not an authored deployment profile; nothing new in `.operationalConcepts`.
- **Layer A immediacy derives from the action kind, not the diagram.** `check` ⇒ `checked` asserted immediately; `fill`/`press Enter` ⇒ `value`/`text`; a delete action ⇒ `absent`. The derived `Immediate` flag (W2) governs Layer B derived data only. Diagram authors draw outcomes, not render timing.
- **Web null-subject nulls the server op.** Under Layer A the control still flips, so TP-DISCRIMINATE forces every web scenario to carry at least one server-confirmed observable (reload, second context, or a REST probe). This is binding doctrine, stated in `frontend-test-plan.md` and enforced by the rule.
- **N-ATP binds only the residual.** The platform computes the residual (`ForAcceptance` minus paths fully covered by one client projection): cross-client, event-entered, and later-stimulus-outcome paths. Shared actor steps reuse the client plan's bindings by `$ref`. No second full binding job.
- **M-ACCEPT is a milestone with an agent-less `job` task kind** (new, alongside `dispatch`/`review`), run by the venue (GH workflow step or local), gated in `runGate`. Reopen = a new attempt on the named activity's integration task from a fresh branch off main.
- **Tier 1 on the server rail** = emit + parse + `go/types` over the *generated* packages only (they import framework-go and each other, which the server already has). The full typecheck including app packages runs on the venue rail at Tier 2.
- **Host Engine for task-spec/scope/check derivation = the review Engine** (it already encapsulates ReviewPolicy and gate computation; "what a task must change and prove" is its volatility). It may import app-generator's `Plan` as a platform library. Alternative if the founder objects: estimation (it already derives activities).
- **Per-request principals in the local profile.** The generated local auth edge accepts a per-request dev principal header when a `<ENV>_DEV_PRINCIPALS` allowlist is set (test-only); multi-actor scenarios do not boot one devstack per actor.
- **Fixture provenance.** RUN-021's harvest (`archistrator-bench-scratch/archive/run-20261105T141816Z-04ce59ca/harvest/`) is available locally; `project.json`, bindings and the agent-written `impl/` bodies are committed to the platform `testdata/conformance/todomvc/`. Greenfield is the fallback fixture if the harvest fails the new rules before they are tuned.
- **`reachedModel` on the GitHub venue** comes from an artifact contract: the construct workflow uploads the action's execution log; the agentic-job RA derives `reachedModel` from it; a missing artifact yields `unknown`.
- **Arrange primitives beyond contract ops** are typed host-level `ArrangeCall` kinds: `advanceClock{duration}`, `failNext{component, operation, error}`, `seedResource{resource, fixture}`. Declarative, platform-executed, no agent code. Anything not expressible this way is a design change (A7). Pending ruling 8.

## 5. Non-goals

- No Required Behaviors slot or any requirements artifact (founder, 2026-08-30).
- The bench's TodoMVC suite does not become the oracle; it stays an independent check. Its own inconsistency (toggle-all uses `click`, seven tests use `check`) is the bench's problem.
- No change to the PR/merge flow, budget caps, or cost optimisation.
- No new Manager or Engine; where a home is proposed it is an existing one.

## 6. Founder rulings required

1. **Amendment C** to DCT: revise §1 thesis and T5; adopt `ForAcceptance`, N-ATP, M-ACCEPT (W1).
2. **Delete `Hook` and `Unobservable`** from the binding model; agents never write test code (W3).
3. **InteractionPolicy Layer A as a platform constant** the design cannot override, and accessible semantics always on (W4). PM ratified both.
4. **`deliverySurfaces` on the mission** checked by component kind, and GEN-SURFACE-SUPPORTED refusing `mcpApp` until the generator exists (W6). PM ratified the typed list.
4a. **PM coverage view on the core-use-cases critique, with a blocking sendBack even under `vibes`** (W2). This changes what `vibes` auto-approves.
5. **Task-spec / scope / check derivation move from the projectstate RA to an Engine** (W7).
6. **Tier 1 admission runs generators in-process on every record** (W7) — accepts ~5 min per 2 h run.
7. **Conformance pipeline gates platform releases and pin bumps** (W8).
8. **Own-project migration scope (sizes the plan ±2×).** Archistrator's own committed plans have 33 components, 818 steps, 504 `hook:true`, 63 `unobservable`; its ~58 use cases predate `sendSignal`/`objectNode` and model outcomes server-side, so UC-PATH-OUTCOME and UC-OUTCOME-AT-ASKING-SURFACE would red nearly all of them. Recommendation: new rules are **Error for projects born after the release and Warning under a dated waiver for archistrator's own project**; the own-plan rebinding and use-case re-authoring are a separate, scheduled workflow after step 2, not part of this wave. The alternative (B11 strictly: conform own project in the same change) roughly doubles the wave.
9. **Self-conformance of W4/W6 to archistrator's own SPA.** Layer A contradicts archistrator's non-undoable server-confirmed actions (approve, merge, dispatch), and GEN-SURFACE-SUPPORTED would red its own mission if it lists MCP Apps before the generator exists. Recommendation: Layer A override for archistrator's own project with the stated reason "non-undoable operations are server-confirmed" traced to an objective; W6 waiver with expiry on the own project until the mcpApp generator lands.
10. **Arrange primitives** (§4b last bullet): typed host-level kinds vs "design change only". Recommendation: the three typed kinds; anything else is a design change.

## 7. Sequencing

Order by paid-run risk removed per unit of work, with a $0 proof at every step. Each step is a platform release + pin bump unless noted.

1. **W8 pipeline skeleton + W7 Tier 0** — the test bed for everything else; immediately catches the FIX-028/032/034/039/048/049/050/051/063 classes at $0.
2. **W2 derivation + W3 model/rules + W4 dialect/typed UI design** — one platform release (schema, methodcheck, testgen, scenariohost, method-assets); archistrator schema regen + own project.json conformed (B11). Proof: replay RUN-021's harvest through the new chain: reword-P1 exists and is addressed; discard-P1 fails under null subject; the toggle scenario asserts `checked` immediately; the SPA plan fails TP-HOOK until rebound.
3. **W5 composition smoke + over-the-wire clients + policy ownership.** Proof: W8 fixture boots twice, clients pass over the wire under the real policy.
4. **W1 Amendment C** (acceptance derivation, N-ATP, M-ACCEPT). Proof: fixture acceptance run green; a deliberately broken fixture (non-optimistic checkbox) goes red at M-ACCEPT and routes to the client activity.
5. **W6 surfaces + MCP Apps generator.** Proof: fixture with `requiredSurfaces` incl. `mcpApp` generates and the basic-host E2E loads the ui:// resource.
6. **W7 manifests/identity/typed causes + W8 doctrine registry.**
7. **One paid run.** Acceptance criterion for the plan: the run completes by the bench's definition, or every acceptance failure maps to a red archistrator gate (i.e. archistrator caught it first).

## 8. Risks and open points

- **Latency harness flakiness**: a fixed delay plus a short timeout is deterministic only if the app never calls the server before the user acts; the harness must install `route` before navigation and the `Immediate` derivation must be conservative (W4).
- **Null-subject for web** doubles browser time; acceptable at one worker, but the testing job budget needs a figure.
- **Re-recording fixtures through the recorder** couples platform CI to archistrator's RA; keep it as an archistrator test, not a platform one.
- **Migration**: deleting `Hook`/`Unobservable` breaks archistrator's own committed test plans (504 hooked steps, 63 unobservable across 33 components). See ruling 8: Warning-under-waiver for the own project, rebinding scheduled as its own workflow.
- **The 7 `.check()` failures** should be re-measured with `click()` + retrying `toBeChecked()` on the RUN-021 build before the next paid run, to separate "design chose server-confirmed state" from "the list never refreshes". Either way W4 makes the platform's position explicit.
