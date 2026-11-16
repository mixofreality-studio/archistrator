# FPC Plan 6 — Generator Manifests, Tiered Admission, Typed Failure Causes, Doctrine Registry

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Expanded to step detail 2026-11-15 from the code at platform `fpc/main` @906a7484 and app `worktree-first-pass-correctness-plan` @42e9626a (Plans 1–5 and Plan 2 Task 2.14 landed and released: projectmodel v0.6.0, framework-go v0.25.0, app-generator v0.27.0, scenariohost v0.8.0, method-assets v0.28.0, conformance v0.4.1; CI 7 jobs incl. four conformance proofs + conformance-mcpapp, all required by release-gate).

**Goal:** Edit scope and drift read one computed manifest; artifacts are admitted by their consumers before a paid run can see them; host faults never consume attempts; every rule the platform requires of a built app is enforced in code and the registry proves it.

**Architecture:** Each generator exposes `Plan(model, selection) []Output` ahead of emission; the review Engine (ruling 5) consumes `Plan` to derive task specs, edit scope and checks, out of the projectstate RA. Admission gains Tier 1 (emit + `go/types` over generated packages) on record and Tier 2 (full build chain) at commit. The agentic-job RA classifies failures into a sum type the delivery Manager's policy switches on exhaustively. method-assets holds a doctrine registry whose CI ratchets prompt-only rules to zero. **Founder rulings 5, 6, 7 required** — adopted per the spec's recommendations (5: task-spec/scope/check derivation moves out of the projectstate RA into the review Engine; 6: Tier‑1 admission runs generators in-process on record; 7: conformance gates platform releases and pin bumps, already true via `release-gate`).

**Spec:** §W7 (manifests, admission, typed causes), §W8 (registry); SA-3 rulings R1–R5 and defects 1–5; §4b (Tier‑1 placement, `reachedModel` on GH). **Depends on:** Plans 1–5 (consumers to register).

## Global Constraints (plan-local)
- `FailureCause`, `FaultClass`, `Output.Role`, `AdmissionTier` are sum types/enums with exhaustive switches.
- The server rail never runs `go build`; Tier 1 typechecks only packages whose imports are framework-go + generated siblings.

## Review Focus
1. **Generated file outside every task's edit scope** (FIX-053): Task 6.2 `TestEditScope_EveryPlannedPathIsInSomeTaskScope`.
2. **Agent hand-edits a generated file inside scope** (drift masked): Task 6.2 `TestGate_GeneratedPathMustEqualRegeneration`.
3. **Network outage counted as NoProgress** (FIX-065): Task 6.4 `TestAgenticJob_NoFingerprintWhenModelUnreached`.
4. **Agent claims green while gate red** (FIX-054/018): Task 6.4 `TestGateRed_TruthFromRunnerResults`.
5. **A normative prompt sentence with no enforcing rule**: Task 6.5 `TestRegistry_BuiltAppRulesEnforcedOrWaived`.

---

## Commands (every task)

- Platform: `P=/Users/davidmarne/mixofrealitystudio/fpcwt/archistrator-platform` (branch `fpc/main`). Each module tests standalone with `GOWORK=off` from its own directory; the sibling `replace` directives (`framework-go-app-generator/go.mod:57-61`, `framework-go-scenariohost/go.mod:118-…`, `framework-go-conformance/go.mod:125-…`) make an unreleased change in a sibling visible to its dependents. Commit with `git -c user.name="David Marne" -c user.email="davemarne@gmail.com" commit` (the repo-local identity is "uitests"); stage explicit paths only.
- App: `A=/Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/first-pass-correctness-plan`; every go/make/npm command runs with `GOMODCACHE=/Users/davidmarne/.claude/jobs/a7cfb4f6/tmp/gomodcache-app9 ASDF_NODEJS_VERSION=25.2.1` (never `~/go/pkg/mod`). Until Task 6.6 pins the release, app packages that need an unreleased platform change run in workspace mode: `GOWORK=$A/go.work` (it `use`s `../archistrator-platform/*`, a symlink to `$P`; Task 6.3 Step 1 adds `framework-go-conformance` to it). App edits are staged on the branch and **committed only in Task 6.6** (Plans 3–5 precedent), except that each task's own app commit is allowed where the step says so.
- Tests only on the packages a task touches (GC‑7); the full suite once, in Task 6.6.

## Detail-phase decisions (2026-11-15; each derived from the code named, the lead may override)

- **D1 — one manifest per *consumer*, not per generator package.** Generators take heterogeneous inputs (`modelgen.Generate(projectJSON []byte, Config)` keyed by **goPackage dir**, `modelgen/modelgen.go:93-127`; `testgen.Generate(projectJSON []byte, Config) (Output, error)`, `testgen/testgen.go:136`; `nullgen.Emit(projectJSON []byte, Options) (Output, error)`, `nullgen/nullgen.go:58`; `composegen/configgen/rungen/temporalgen.Generate(m *projectmodel.Model, cfg)`; `clientgen.Generate(Project, Config)`; `webgen.Generate(Inputs) ([]File, error)` over the OAS + tool tables clientgen writes), and the **placement** of their outputs lives in the CLIs (`cmd/appgen/main.go:270-530`: `prefixed` under `cmd/server`, `filepath.Join(c.GoPackage, name)`, `rungen.Dir`, `composegen.ScenarioComposeFile`, the once-seeded `hooks.go`). A `Plan` per package would duplicate that placement a second time. So the manifest is per consumer, in the importable `framework-go-app-generator/appgen` package (it exists: `appgen/surfaces.go`), and **every CLI writes exactly its consumer's `Emit`** (one source, two readers: the CLIs and the manifest). `Plan(in)` is the paths and roles of `Emit` over `in` with `Tree == nil` ("every contract with a goPackage counts as built"), i.e. the emission of a fully built tree: a superset of any partial emission (`wireOnlyBuilt`, `cmd/appgen/main.go:192-205`, only ever drops). Output paths are **repo-relative** (`Input.ModuleDir` is joined in; webgen's are under `webApp/`, the one web project at the repo root, `projectstateaccess.go:13689`).
- **D2 — the consumer table.** Consumers (names are the CLI invocations the task specs already run): `modelgen` (`make gen-models`), `nullgen` (`make gen-null`), `appgen temporal`, `appgen config`, `appgen compose` (composegen + rungen launcher + `internal/scenariocompose/compose.gen.go` + the `cmd/server/hooks.go` **Seam**), `appgen clients`, `testgen` (`make gen-tests`), `webgen bootstrap` (per browser Client; every output a **Seam** except an mcpApp's host-bridge seat `webApp/src/<id>/main.tsx`, which is **Generated** — folds earmark E30's edit-scope half), `webgen generate`. `appgen.ArtifactKind` values are the record verbs' kinds: `serviceContract`, `testPlan`, `uiDesign`, `acceptancePlan`, `system`, `operationalConcepts`, `coreUseCases`. Kind → consumers: serviceContract → modelgen, nullgen, appgen temporal, appgen clients, appgen compose, testgen, webgen generate; testPlan/uiDesign/acceptancePlan/coreUseCases → testgen; system → appgen config, appgen compose, appgen clients, webgen bootstrap, testgen; operationalConcepts → appgen config, appgen compose.
- **D3 — the review Engine's file layout.** `arch.CheckFileLayout` limits a component package's hand-written files to `<leaf><stereotype>.go` (`reviewengine.go`) plus workflow files, and its tests to `engine_scenarios.gen_test.go` + `null.gen_test.go` (`framework-go/arch/filelayout.go:140-200`); `CheckGeneratedSurface` (`internal/arch_test.go:448`, `encapAllowlist()`) forbids exported hand-written identifiers there. So all moved derivation code goes into `server/internal/engine/review/reviewengine.go`, unexported; behaviour tests live in `server/internal/taskspec_test.go` (package `internal_test`, where `TaskSpecFor`'s tests already are) and `server/cmd/aiarch-state-mcp/*_test.go`, through the contract.
- **D4 — the contract shape of the move.** An Engine may not import projectstate (F3; `reviewengine.go` package doc). The derivation splits at the RA's own knowledge:
  - **stays in projectstate** (it reads its own document's classification, rate card and slot attribution): activity classification (`ResolveConstructionRow`, `LifecycleKeyFor`), the model + estimate (`specModel`, `taskShare`, `taskEstimate`, `priceOf`), the slot rule attribution (`ArtifactKindFromWireName`, `RulePrefixesFor`), `TaskSpecSchemaVersion` and `StateToolVersion` (bumped 6 → 7). New exported `projectstate.TaskFactsFor(p *Project, activityID, taskID string) (TaskFacts, error)` returns them.
  - **moves to the review Engine** ("what a task must change and prove"): identity/prompt/tools from `methodassets.MethodSpecFor`, edit scope, activity edit scope, checks, generators, contracts, the canonical hash. The Engine reads the project as raw JSON (the designhealth precedent: `EvaluateDesignHealth(rc, project string)`, `designhealthengine.go:252`) through platform libraries only: `projectmodel.Load`, `methodcheck.DecodeProject`, `methodcheck.ScenarioInput` + `scenario.Derive/ForComponent/ForAcceptance/AcceptanceResidual`, `methodassets`, `appgen.Plan`.
  - Contract ops (all pure, deterministic, in-workflow like `ProposeReviews`): `DeriveTaskSpec(rc fweng.Context, project string, facts TaskFacts) (TaskSpec, error)`, `TaskSpecHash(rc fweng.Context, spec TaskSpec) (string, error)` (the one canonical hash every gate verifies through; the plan's single op cannot be verified by a reader that is not the Engine), `AdmitArtifact(rc fweng.Context, project string, kind string) ([]AdmissionProblem, error)` (Task 6.3).
  - `TaskSpec` and its parts become reviewEngine `$defs` with **property names equal to today's JSON tags** (`activity`, `task`, `identity`, `model`, `prompt`, `tools`, `editScope`, `activityEditScope`, `checks`, `contracts`, `generate`, `hash`, …; `projectstateaccess.go:12830-12945`): the venue template reads `spec.json` with jq (`.hash`, `.identity.kind`, `.model.id`, `.tools.builtin`, `aiarch-construct.yml.tmpl:272-360`). `projectstate.TaskSpec`, `SpecIdentity`, …, `SpecCheckSource` are **deleted** (B10, "break freely"); every reader uses `review.TaskSpec`.
- **D5 — parity before change.** The Engine copy is first proven hash-identical to `projectstate.TaskSpecFor` for every (activity, task) of the own project, the six fresh-project fixtures (`cmd/aiarch-state-mcp/testdata/fresh/run0{08,09,10,11,13,16}.project.json`) and the conformance todomvc fixture; only then are the callers switched and the old code deleted; only then do the intentional changes (D6, D7) land, each bumping `TaskSpecSchemaVersion`.
- **D6 — edit scope and drift from `Plan`.** `compositionPaths` (`projectstateaccess.go:13629-13640`, five `<dir>/**/*.gen.*` globs) is replaced by the literal `Generated` paths of the task's consumers' `Plan`, and seams (`cmd/server/hooks.go`) by their `Seam` outputs. `TaskSpec` gains `generated []string` (sorted, repo-relative: every `Generated` path of the consumers the task runs — the contracts' `modelgen`, `appgen temporal` for a Manager, the four composition consumers at a composing integration, `webgen generate`/`bootstrap` for a browser surface). The edit-scope gate holds every changed path in `generated` byte-equal to regeneration (rule id `EDIT-GENERATED-DRIFT`), even when a glob admits it.
- **D7 — check identity.** Scenario checks are keyed `(checkId, contract)`: `CheckResult` ($defs, `projectstate/contract.gen.go:339-348`) gains `contract string` (omitempty; set by `run-checks` from `SpecCheck.source.contract`), and every reader keys on it instead of cutting the id string (`scenarioIDOf`/`scenarioContractsOf`, `deliverymanager.go:2440-2465`). The component→contracts join exists three times today with two different matching rules: `componentContracts` (`projectstateaccess.go:13436`, exact), `gateContracts` (`deliverymanager.go:14813`, exact), `componentContracts` (`cmd/aiarch-state-mcp/constructverbs.go:594`, `scenario.Normalize`). One join replaces them: `methodcheck.ContractsOf(key string, components map[string]string) []string` (platform, pure; `components` = contract key → its `component` field; exact match, as the gate's comment rules).
- **D8 — Tier 1 is pure.** An Engine does no I/O (`reviewengine.go` doctrine), and the server image is `distroless/static` with no toolchain or module cache (`server/Dockerfile:46`). So Tier 1 typechecks with `go/types` and an importer that resolves **only the emitted siblings** from the in-memory emission; every other import (stdlib, framework-go, third party) is an *opaque* package, and a type error whose operand is a selector on an opaque package is dropped (it is Tier 2's). What remains is what the generators did to each other: parse errors, duplicate declarations, sibling references to undefined or mis-typed declarations. Budget: the call runs in-workflow on the server rail, under Temporal's 1 s deadlock detector, so a test pins it under 500 ms on the todomvc fixture.
- **D9 — Tier 2 is a venue check.** A new check kind `admission` (`CheckKindAdmission = "admission"` beside `CheckKindSmoke`, `projectstateaccess.go:12754-12759`) run in-process by `run-checks` (`checks.go` `checkRunner`, like `runSmoke` :275) calling `conformance.RunChainOn(ctx, "project", raw, scratch)` (`framework-go-conformance/chain.go:106`) over the checkout's `project.json`. The app already requires `framework-go-conformance` (`server/go.mod:10`). It is added to the specs of the tasks whose artifact a merge **commits** (`detailed_design`, `test_plan`, `acceptance_plan`, and the UI-design task), never to a record verb.
- **D10 — failure causes on the observation.** The agentic-job result is `PipelineObservation` (`agenticjob/contract.gen.go:49-60`): it gains `ReachedModel bool`, `Cause FailureCause` (x-go-sumtype, the `TestArgValue` convention: `{"kind","model"}` envelope, pointer-receiver variants, `FailureCauseKind` enum), `FaultClass` (string enum `host|agent|gate|unknown`) and `GateFailures []string` (check ids). `NoProgress` is a `FailureCause` variant the **Manager** sets (only it sees two attempts, `progressRepeated`, `deliverymanager.go:13574`); the RA never emits it. The attempt ledger records the cause: `projectstate.TaskAttemptInput` and the stored `TaskAttempt` (`projectstateaccess.go:8575`) gain `cause` (the same sum, mirrored in projectStateAccess `$defs`), which is what the bench's `classify.ts` reads.
- **D11 — `reachedModel`.** Local: true iff the mined stream holds ≥1 assistant event whose `message.model` is not `"<synthetic>"` with `usage.output_tokens > 0` (the miner already distinguishes the synthetic stand-in, `agenticjobaccess.go:3603-3613`). GitHub: the construct workflow uploads claude-code-action's `execution_file` as artifact `execution-log-<activity>-<run>-<attempt>`; the RA lists and downloads it (`fwgithub.AppClient.ListArtifacts/DownloadArtifact`, `framework-go-infrastructure-github/actions.go:326,359`) and runs the same miner over its JSON array; a missing artifact gives `Cause = Unknown`, `ReachedModel = false`.
- **D12 — where the registry lives.** method-assets has **no** Go dependencies (`method-assets/go.mod`), so it holds the data (`method-assets/doctrine/registry.json`, embedded, read by `methodassets.DoctrineRegistry()`) and the prompt-side tests; the rule-resolution test lives in `framework-go-conformance` (which already requires framework-go, app-generator, scenariohost and method-assets, and gates every release). A `ruleRef` is a sum type: `methodcheck{rule, firedBy}` | `arch{rule, firedBy}` | `generatorInvariant{module, package, test}`; `firedBy` names the existing Go test that makes the rule fire on a violating fixture (the methodcheck convention, GC‑4).
- **D13 — the suite speed-up (Task 6.5b) is the clean half of E16/E18.** testgen's `-discriminate` runs one `go test` process per (scenario, operation) (`cmd/testgen/discriminate.go:144-160`), each paying the package build-link and the scenario host's boot; the host is per process (`scenariohost.Main` / `run`, `host.go:139-189`). Clean: one process per package for all of a package's runs, switching the null operations between subtests in-process (`t.Setenv("AIARCH_NULL_OPS", …)` reaches a client scenario's launched app too, which inherits the test's environment, `emit_mcp.go:140`). **Not clean, kept as earmark E18:** one devstack boot shared across a package's *client* scenarios — every launch is the isolation unit (its own database and Temporal namespace, `scenariohost/devstack.go:38-49`) and the generated client scenarios assume a fresh one, so sharing a launch makes scenario order matter.

### Earmarks folded into this plan (from the Plans 2–5 status blocks)
| earmark | where it lands |
|---|---|
| E16 (null run cheaper; one null run instead of a process per mutant) | Task 6.5b (D13) |
| E18 (one devstack boot per package's client scenarios) | Task 6.5b covers the per-package process; the shared boot stays an earmark (D13) |
| E20 second half (Plan 6's manifests own what is generated) | Task 6.1 (the manifest); the *unused generated symbol* registry is not in this plan |
| E30 edit-scope half (the mcpApp host-bridge seat is generated) | Task 6.1 (D2: `Generated` role) + Task 6.2 (`EDIT-GENERATED-DRIFT`); its prompt line in Task 6.5 |

Every other earmark is listed at the end ("Earmarks (not in this plan)").

## File map

Platform:
- Create `framework-go-app-generator/plan/plan.go`, `plan_test.go`; `appgen/consumers.go`, `appgen/compose.go` (the generation bodies moved out of `cmd/appgen/main.go`), `appgen/consumers_test.go`, `appgen/admit.go`, `appgen/admit_test.go`.
- Modify `framework-go-app-generator/cmd/{appgen,modelgen,nullgen,testgen,webgen}/main.go` (each writes its consumer's `Emit`), `cmd/testgen/discriminate.go` (+`discriminate_test.go`), `testgen/emit_null.go` (+`emit_null_test.go`), `testgen/testgen.go` (`Discrimination`).
- Modify `framework-go/methodcheck/emitters.go` (`EmittedRuleIDs`), create `framework-go/methodcheck/contracts.go` (`ContractsOf`) + `contracts_test.go`; modify `framework-go/arch/{filelayout.go,contractdup.go,scenariocompose_testonly.go}` and create `framework-go/arch/ruleids.go` (+`ruleids_test.go`).
- Create `method-assets/doctrine/registry.json`, `doctrine/waivers.max`, `doctrine/promptlint.max`, `method-assets/doctrine.go`, `method-assets/doctrine_test.go`; modify `method-assets/assets/workflows/aiarch-construct.yml.tmpl` (+`workflows_test.go`), the prompts the registry cites.
- Create `framework-go-conformance/doctrine_test.go`, `framework-go-conformance/manifests_test.go`.

App:
- `.aiarch/state/project.json`: reviewEngine `$defs` + ops (D4, Task 6.3), agenticJobAccess `$defs` (D10), projectStateAccess `$defs` (`CheckResult.contract`, `TaskAttemptInput.cause`, `FailureCause`), System slot 5 dynamic view `execute-a-project-activity` (the two new review-engine calls), reviewEngine test plan.
- `server/internal/engine/review/reviewengine.go` (+ regenerated `contract.gen.go`, `engine_scenarios.gen_test.go`), `server/internal/resourceaccess/projectstate/projectstateaccess.go` (delete the task-spec region `:12754-13946` except D4's kept half; `TaskFactsFor`; `CheckKindAdmission`), `server/internal/resourceaccess/agenticjob/agenticjobaccess.go`, `server/internal/manager/delivery/{deliverymanager.go,deliveryactivity.go}`, `server/cmd/aiarch-state-mcp/{taskspeccmd.go,runchecks.go,checks.go,editscope.go,recordcheckrun.go,constructverbs.go,state.go}`, `server/internal/arch_test.go` (`encapAllowlist`: `TaskFactsFor`, `TaskFacts`, drop the deleted names), `server/internal/taskspec_test.go`, `server/go.mod`, `go.work`.
- Create `server/cmd/aiarch-state-mcp/{generateddrift_test.go,admission_test.go,failurecause_test.go}`, `server/internal/{checkidentity_test.go,variance_test.go}`.

---

### Task 6.1: Generator `Plan()` manifests

**Files:**
- Create: `framework-go-app-generator/plan/plan.go`, `plan/plan_test.go`
- Create: `framework-go-app-generator/appgen/consumers.go`, `appgen/compose.go`, `appgen/consumers_test.go`
- Modify: `framework-go-app-generator/cmd/appgen/main.go` (move `config` → `appgen.Config`, `wireOnlyBuilt`/`isBuilt`/`holdsGenerated`/`hasGeneratedContract`/`holdsCode`/`managersOf`/`genTemporal`/`withDeployment`/`genConfig`/`genCompose`/`composeConfig`/`genClients`/`mcpApps`/`managerOpNames`/`mcpAppClients`/`containerOf`/`prefixed` into `appgen/compose.go`, reading the tree through `fs.FS` instead of `os`; the CLI keeps flag parsing, `readConfig`, `refuseUndeclaredEnv`, `reportRefusals`, `emit`, `checkDrift`, `writeGenerated`), `cmd/modelgen/main.go`, `cmd/nullgen/main.go`, `cmd/testgen/main.go`, `cmd/webgen/main.go` (each calls its consumer's `Emit` and writes/compares exactly its files)

**Interfaces (Produces):**
```go
// framework-go-app-generator/plan/plan.go
package plan

// OutputRole is who owns a planned path after the generator runs.
//
//sumtype:decl
type OutputRole interface{ isOutputRole() }

// Generated is rewritten on every run; the committed bytes must equal regeneration.
type Generated struct{}

// Seam is written once when absent (cmd/server/hooks.go, the webgen bootstrap seats); the app owns it after.
type Seam struct{}

func (Generated) isOutputRole() {}
func (Seam) isOutputRole()      {}

// Output is one path a consumer writes, repo-relative and slash-separated.
type Output struct {
	Path string
	Role OutputRole
}

// File is an Output with its content.
type File struct {
	Output
	Content []byte
}

// RoleName is "generated" or "seam" (exhaustive switch, no default).
func RoleName(r OutputRole) string

// Paths returns the sorted paths of outs whose role is r's type.
func Paths(outs []Output, r OutputRole) []string
```
```go
// framework-go-app-generator/appgen/consumers.go
package appgen

// ArtifactKind is a record verb's artifact kind (D2).
type ArtifactKind string

const (
	KindServiceContract     ArtifactKind = "serviceContract"
	KindTestPlan            ArtifactKind = "testPlan"
	KindUIDesign            ArtifactKind = "uiDesign"
	KindAcceptancePlan      ArtifactKind = "acceptancePlan"
	KindSystem              ArtifactKind = "system"
	KindOperationalConcepts ArtifactKind = "operationalConcepts"
	KindCoreUseCases        ArtifactKind = "coreUseCases"
)

func ArtifactKinds() []ArtifactKind // the seven, in that order

// Config is appgen.json (moved verbatim from cmd/appgen's unexported config, fields and JSON tags unchanged).
type Config struct { /* Container, EnvPrefix, Managers, WebExposedManagers, CallerKeyedOps, VariantHookArgs,
	VariantConstructorNoError, ScheduleRegistrarComponent, WebDir, Clients{Title, App, Exposed, OpDocs, OAS} */ }

// DefaultConfig is the zero appgen.json's effective knobs (Container "server", EnvPrefix "APP").
func DefaultConfig() Config

// Input is what a consumer reads.
type Input struct {
	Project    []byte // the project.json bytes
	ModulePath string // the go.mod module line
	ModuleDir  string // the Go module's repo-relative dir ("" or "server"); every Output.Path is repo-relative
	Config     Config
	Tree       fs.FS // the module tree as it stands, rooted at ModuleDir; nil = every contract with a goPackage counts as built
}

// Consumer is one generator invocation the platform runs over an artifact.
type Consumer struct {
	Name  string                          // "modelgen", "nullgen", "appgen temporal", … (D2)
	Kinds []ArtifactKind                  // the artifact kinds it reads
	Emit  func(in Input) ([]plan.File, error)
}

// Plan is c's manifest: the Outputs of c.Emit over in with Tree == nil, sorted by path.
func (c Consumer) Plan(in Input) ([]plan.Output, error)

func AllConsumers() []Consumer                 // the nine of D2, in CLI order
func Consumers(kind ArtifactKind) []Consumer   // those whose Kinds contain kind; nil for an unknown kind
func ConsumerNamed(name string) (Consumer, bool)
```

- [ ] **Step 1: Write the failing tests.**
```go
// plan/plan_test.go
func TestRoleName_Exhaustive(t *testing.T) {
	if RoleName(Generated{}) != "generated" || RoleName(Seam{}) != "seam" {
		t.Fatalf("role names: %q %q", RoleName(Generated{}), RoleName(Seam{}))
	}
}
func TestPaths_FiltersByRoleAndSorts(t *testing.T) {
	outs := []Output{{"b.go", Generated{}}, {"hooks.go", Seam{}}, {"a.go", Generated{}}}
	if got := Paths(outs, Generated{}); !slices.Equal(got, []string{"a.go", "b.go"}) {
		t.Fatalf("Paths = %v", got)
	}
}

// appgen/consumers_test.go — fixtures: internal/testfixtures.Fixture(t, "greenfield") and
// testdata/archistrator.project.json (both already used by composegen/modelgen goldens).
func TestConsumers_EveryArtifactKindHasAtLeastOne(t *testing.T) {
	for _, k := range ArtifactKinds() {
		if len(Consumers(k)) == 0 {
			t.Errorf("artifact kind %q has no consumer", k)
		}
	}
	if Consumers("nope") != nil {
		t.Error("an unknown kind must have no consumers")
	}
}
func TestPlan_MatchesEmit(t *testing.T) {
	// For each consumer and each fixture: Emit over a tree where every contract package holds
	// a non-generated .go file (fstest.MapFS built from the model) writes exactly Plan's paths,
	// and every written path has the role Plan gives it.
	for _, c := range AllConsumers() {
		t.Run(c.Name, func(t *testing.T) {
			in := fixtureInput(t, "greenfield", builtTree(t, "greenfield"))
			planned, err := c.Plan(in)
			if err != nil { t.Fatal(err) }
			files, err := c.Emit(in)
			if err != nil { t.Fatal(err) }
			if got, want := outputsOf(files), planned; !reflect.DeepEqual(got, want) {
				t.Fatalf("Emit wrote %v, Plan says %v", got, want)
			}
		})
	}
}
func TestPlan_CoversPartialEmit(t *testing.T) {
	// With an empty tree (nothing built) Emit writes a subset of Plan (wireOnlyBuilt only drops).
}
func TestPlan_IsRepoRelative(t *testing.T) {
	// ModuleDir "server": every Go output starts with "server/", every webgen output with "webApp/".
}
func TestPlan_HooksIsASeam_McpAppSeatIsGenerated(t *testing.T) {
	// "appgen compose" plans server/cmd/server/hooks.go as Seam; "webgen bootstrap" over a System with an
	// mcpApp Client plans webApp/src/<id>/main.tsx as Generated and webApp/src/<id>/routes/App.tsx as Seam.
}
func TestCLIsWriteTheirConsumersEmit(t *testing.T) {
	// For appgen compose/clients/config/temporal, modelgen, nullgen, testgen: run the CLI's run(args, out)
	// into a temp module seeded from the fixture, then the set of files written == c.Emit(in) paths and
	// bytes (the CLI and the manifest are one source).
}
```
- [ ] **Step 2: Run** `cd $P/framework-go-app-generator && GOWORK=off go test ./plan ./appgen -run 'TestRoleName|TestPaths|TestConsumers|TestPlan_|TestCLIs' -v` — Expected: FAIL (`undefined: plan.Output`, `undefined: AllConsumers`).
- [ ] **Step 3: Implement.** `plan/plan.go` as above. `appgen/compose.go`: the moved bodies, with every `os.ReadDir/os.Stat` replaced by `fs.ReadDir/fs.Stat` over `in.Tree` (nil tree → `isBuilt` returns true for every contract with a goPackage). `appgen/consumers.go`: one `Emit` per D2 consumer — `modelgen` places `<goPkg>/contract.gen.go` exactly as `cmd/modelgen` does today (`cmd/modelgen/main.go:170-186`); `nullgen` maps `nullgen.Output.Files` (hooks.go → Seam); `appgen compose` returns the composegen files under `cmd/server`, the launcher under `rungen.Dir`, `composegen.ScenarioComposeFile`, and `composegen.HooksSeed` as a Seam at `cmd/server/hooks.go`; `testgen` maps `Output.Generated`; `webgen bootstrap` iterates the System's built browser Clients (`IsBrowserSurface`) calling `webgen.Bootstrap(app, id, kind)` (roles per D2); `webgen generate` runs `appgen clients` in memory first and feeds its `api/openapi.yaml` and tool tables to `webgen.Generate` (config: `in.Tree`'s `webApp/webgen.json` if present, else `webgen.DefaultConfig`). Every path joined with `in.ModuleDir` except webgen's (`webApp/…`). Then each CLI: `cmd/appgen` keeps `-check` semantics by comparing `Generated` files and writing a `Seam` only when absent (`seedHooks`' rule); `cmd/modelgen`, `cmd/nullgen`, `cmd/testgen` (write and `Drift`), `cmd/webgen generate|bootstrap` call `appgen.ConsumerNamed(...)`.
- [ ] **Step 4: Run** `GOWORK=off go test ./plan ./appgen ./cmd/... ./composegen ./testgen ./webgen` — Expected: PASS (existing CLI tests `cmd/appgen/main_test.go`, `refuse_test.go`, `scenariocompose_test.go` unchanged and green). `golangci-lint run ./plan ./appgen ./cmd/...` — 0 issues (gochecksumtype covers `OutputRole`).
- [ ] **Step 5: Commit** `git commit -m "feat(app-generator): plan package and appgen.Consumers — one manifest per consumer; every CLI writes its consumer's Emit"`.

---

### Task 6.2: Task-spec derivation moves to the review Engine and reads `Plan`

**Files:**
- Modify: `.aiarch/state/project.json` — `serviceContracts.reviewEngine.$defs` (+`TaskFacts`, `SlotRules`, `TaskSpec`, `SpecIdentity`, `SpecModel`, `SpecEstimate`, `SpecPrompt`, `SpecTools`, `SpecEditScope`, `SpecScopeComponent`, `SpecCheck`, `SpecCheckSource`, `SpecGenerator`; property names = today's JSON tags, D4), `interface.operations` (+`DeriveTaskSpec`, `TaskSpecHash`); `serviceContracts.projectStateAccess.$defs.CheckResult` (+`contract`); slot 5 dynamic view `execute-a-project-activity`: the step before the dispatch (`delivery-manager → agentic-job-access submitAgenticJob(command, workerClass, …)`, step index 3 today) gains the call `delivery-manager → review-engine` labelled `DeriveTaskSpec(project, facts) → TaskSpec — what the task must change and prove`; the reviewEngine test plan gains bindings for the scenarios that call now projects (or the dated skip `until: "2026-12-31"` Plan 2 used, if a binding needs arranged state).
- Modify: `server/internal/engine/review/reviewengine.go` (the moved derivation, unexported, D3); regenerate `contract.gen.go`, `engine_scenarios.gen_test.go`, fakes.
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — delete `TaskSpec`…`SpecCheckSource` (:12830-12945), `TaskSpecFor` (:12950), `activityEditScope` (:13094), `validateEditScope`, `ComputeHash`, `normalized`, `scopeAndChecks` (:13317) and every helper only they use (:13368-13946: `atpRuleCheck`, `acceptanceJobChecks`, `slotRuleCheck`, `projectRuleCheck`, `componentContracts`, `generatedContractsOf`, `modelgenGenerates`, `statePathFromModule`, `goPackagesOf`, `inModule`, `packageGlobs`, `commandCheck`, `serviceScopeAndChecks`, `isManagerComponent`, `composesServer`, `compositionGenerators`, `appgenArgv`, `compositionPaths`, `compositionChecks`, `bootChecks`, `webDirFromModule`, `generatorsOf`, `compositionGeneratorsSpec`, `archCheck`, `genModelsCheck`, `webProjectSeats`, `uitestsSeats`, `surfaceEntryFile`, `frontendScopeAndChecks`, `discriminateTimeoutSec`, `scenarioChecks`, `checkKindOrder`, `sortChecks`) — after D5's parity step; keep and export per D4: `TaskFactsFor`, `ValidEditScopeGlob`, `IsTestCodePath`, `EditScopeGlobMatch` (gate-side matchers), `WebScenariosArgv`, the check-kind and timeout constants, `TaskSpecSchemaVersion` (→ 7), `StateToolVersion`.
- Modify: `server/cmd/aiarch-state-mcp/taskspeccmd.go:27-110` (`runTaskSpec`, `readSpecFile` → `review.TaskSpec`, hash via `TaskSpecHash`), `runchecks.go:315`, `checks.go` (all `projectstate.SpecCheck` → `review.SpecCheck`; `finish` sets `CheckResult.Contract`), `editscope.go` (`outsideScope(scope review.SpecEditScope, …)`; the drift check), `recordcheckrun.go:100` (`readCheckResults(path string, spec review.TaskSpec)`), `constructverbs.go:594` (`componentContracts` → `methodcheck.ContractsOf`).
- Modify: `server/internal/manager/delivery/deliveryactivity.go:1639-1660` and `deliverymanager.go:14454-14500` (TaskSpecFor → TaskFactsFor + `wf.Review.DeriveTaskSpec` / `m.Review…`), `deliverymanager.go:2433-2465` (`scenarioIDOf`, `scenarioContractsOf` key on `res.Contract`), `:14813` (`gateContracts` → `methodcheck.ContractsOf`), `joinTestRun` (`:14711`).
- Create: `framework-go/methodcheck/contracts.go`, `contracts_test.go` (platform).
- Modify: `server/internal/taskspec_test.go` (every `projectstate.TaskSpecFor` call → the `deriveSpec(t, p, activity, task)` helper below), `server/internal/arch_test.go` `encapAllowlist()` (:1044: drop `TaskSpecFor` and the deleted names; add `TaskFactsFor`, `TaskFacts`).
- Create: `server/cmd/aiarch-state-mcp/generateddrift_test.go`, `server/internal/checkidentity_test.go`.

**Interfaces (Produces):**
```go
// framework-go/methodcheck/contracts.go
// ContractsOf names the contracts a component owns, sorted: the contract under key plus every contract
// whose component field is key (exact match). None when key is "". components maps every contract key
// to its component field. The ONE component→contracts join (FPC D7).
func ContractsOf(key string, components map[string]string) []string

// server/internal/resourceaccess/projectstate (kept half of the derivation, D4)
type TaskFacts struct {
	Activity, Task, ComponentID, LifecycleKey string
	Surface                                   Surface   // the Client's surface; "" for none
	Model                                     SpecModelFacts
	SlotRules                                 SlotRuleFacts
}
type SpecModelFacts struct{ ID, Source string; TokensIn, TokensOut int64; USD float64 }
type SlotRuleFacts struct{ Slot string; Prefixes []string } // the task's artifact slot and its rule families ("" / nil: whole project)
func TaskFactsFor(p *Project, activityID, taskID string) (TaskFacts, error) // the error texts TaskSpecFor returns today for an unknown activity, an unclassifiable one, a missing lifecycle or task

// server/internal/engine/review/contract.gen.go (generated from the new $defs)
type ReviewEngine interface {
	ProposeReviews(rc fweng.Context, change ReviewChange, activityType ActivityType, lifecyclePhase string, componentID string, policy ReviewPolicy, floorTouched bool, contracts []string) (ReviewSet, error)
	DeriveTaskSpec(rc fweng.Context, project string, facts TaskFacts) (TaskSpec, error)
	TaskSpecHash(rc fweng.Context, spec TaskSpec) (string, error)
}
// TaskSpec carries every field it has today plus Generated []string `json:"generated"` (D6).
// Errors: ContractMisuse for an empty project, a project that does not decode, or facts naming no lifecycle/task;
// FailedPrecondition with today's messages ("machine client %q has no contract…", "browser surface %q has no scenario…").
```

- [ ] **Step 1: Write the failing tests (platform join).**
```go
// framework-go/methodcheck/contracts_test.go
func TestContractsOf_KeyAndFacets(t *testing.T) {
	comps := map[string]string{"projectStateAccess": "", "activityExecutionAccess": "projectStateAccess", "listManager": ""}
	if got := ContractsOf("projectStateAccess", comps); !slices.Equal(got, []string{"activityExecutionAccess", "projectStateAccess"}) {
		t.Fatalf("ContractsOf = %v", got)
	}
	if got := ContractsOf("ghost", comps); !slices.Equal(got, []string{"ghost"}) { // a named key with no contract is still named
		t.Fatalf("unrecorded key = %v", got)
	}
	if ContractsOf("", comps) != nil { t.Fatal("empty key must name nothing") }
}
func TestContractsOf_ExactMatchOnly(t *testing.T) {
	if got := ContractsOf("listManager", map[string]string{"listManager": "", "x": "list-manager"}); !slices.Equal(got, []string{"listManager"}) {
		t.Fatalf("a normalized spelling must not join: %v", got)
	}
}
```
Run `cd $P/framework-go && GOWORK=off go test ./methodcheck -run TestContractsOf -v` — FAIL (`undefined: ContractsOf`). Implement; run — PASS. Commit `git commit -m "feat(methodcheck): ContractsOf — the one component→contracts join"`.

- [ ] **Step 2: Contract and parity test (app, workspace mode).** Add the reviewEngine `$defs`/ops and `CheckResult.contract` to `.aiarch/state/project.json` (hand edit of the contract entries, then `make gen-models gen-fakes`); amend the `execute-a-project-activity` view; `make method-check` — no new Error (ALIGN-*/CC-*/TP-* as before; a TP-* finding for the new reviewEngine stimulus is fixed by binding it, or by Plan 2's dated skip). Write the failing parity test:
```go
// server/internal/taskspec_test.go
// deriveSpec is TaskFactsFor + the Engine, the path every caller takes after this task.
func deriveSpec(t *testing.T, p *projectstate.Project, activity, task string) review.TaskSpec

func TestDeriveTaskSpec_ParityWithTaskSpecFor(t *testing.T) {
	// For the own project, cmd/aiarch-state-mcp/testdata/fresh/run0{08,09,10,11,13,16}.project.json and the
	// conformance todomvc fixture (conformance.Fixture("todomvc")): for every committed activity and every task
	// of its lifecycle, json.Marshal(deriveSpec(...)) with generated == nil equals json.Marshal(projectstate.TaskSpecFor(...))
	// and the two hashes are equal; an error from one is the same error text from the other.
}
```
Run `cd $A/server && GOWORK=$A/go.work go test ./internal/ -run TestDeriveTaskSpec_Parity -v` — FAIL (`DeriveTaskSpec` returns the generated `ContractMisuse` stub / undefined `TaskFactsFor`).
- [ ] **Step 3: Implement the copy.** `projectstate.TaskFactsFor` (the classification half of `TaskSpecFor` :12950-12985 + `specModel` + `slotRuleCheck`'s attribution). In `reviewengine.go`: `func (reviewEngineImpl) DeriveTaskSpec(rc fweng.Context, project string, facts TaskFacts) (TaskSpec, error)` decoding once (`projectmodel.Load`, `methodcheck.DecodeProject`, `methodcheck.ScenarioInput` → `scenario.Derive`) into an unexported `taskProject` holding: the System's components (`ContractKey`, `Kind`, `Surface`), each contract's `GoPackage`/`Layer`/`Component`, `ModuleDir` (the document's `moduleDir`), the derived scenarios, the acceptance residual; then the moved functions verbatim over it (`scopeAndChecks`, `serviceScopeAndChecks`, `frontendScopeAndChecks`, …), with `componentContracts` = `methodcheck.ContractsOf`. `TaskSpecHash` = today's `ComputeHash` (`normalized`, `Hash = ""`, sha256 of `json.Marshal`). Run the parity test — PASS. Run `GOWORK=$A/go.work go test ./internal/ -run 'TestTaskSpec|TestEditScope|TestIntegrationChecks|TestDiscriminateBudget' ` — PASS (old path still in place).
- [ ] **Step 4: Switch callers, delete the old code.** `taskspeccmd.go:45`, `runchecks.go:315`, `deliveryactivity.go:1648`, `deliverymanager.go:14483` call `TaskFactsFor` then `DeriveTaskSpec` (the Manager encodes `proj` with `projectstate.EncodeProjectJSON`); every `readSpecFile` hash check calls `TaskSpecHash`; `constructverbs.go:594` and `gateContracts` call `methodcheck.ContractsOf`; delete the projectstate region listed under Files and `TestDeriveTaskSpec_ParityWithTaskSpecFor` (its job is done); rewrite `internal/taskspec_test.go`'s helpers onto `deriveSpec`. Run `GOWORK=$A/go.work go build ./... && GOWORK=$A/go.work go test ./internal/ ./cmd/aiarch-state-mcp/ -run 'TestTaskSpec|TestEditScope|TestIntegrationChecks|TestDiscriminateBudget|TestRunChecks|TestRecordCheckRun|TestTaskSpecCmd' && GOWORK=$A/go.work go test ./internal/arch_test.go ./internal/ -run 'TestGeneratedOnlyPublic|TestFileLayout|TestMethodLayering'` — PASS.
- [ ] **Step 5: Write the failing manifest/drift/identity tests.**
```go
// server/internal/taskspec_test.go
func TestEditScope_EveryPlannedPathIsInSomeTaskScope(t *testing.T) {
	// todomvc fixture (conformance.Fixture("todomvc")), ModuleDir as the fixture states it: every Generated
	// path of every consumer's Plan (appgen.AllConsumers, Input{Tree: nil}) that projectstate.IsTestCodePath
	// does not exclude is matched (projectstate.EditScopeGlobMatch) by the EditScope of at least one task of
	// at least one committed activity, over every lifecycle type the fixture's slot 9 holds. Report every
	// unmatched path at once.
}
func TestTaskSpec_GeneratedIsThePlanOfItsConsumers(t *testing.T) {
	// C-list-manager integration: spec.Generated == sorted Generated paths of modelgen (its contracts),
	// appgen temporal, appgen config/compose/clients; no "**/*.gen.*" glob remains in spec.EditScope.Extra.
}

// server/cmd/aiarch-state-mcp/generateddrift_test.go
func TestGate_GeneratedPathMustEqualRegeneration(t *testing.T) {
	// A git repo seeded from the todomvc fixture with its generated tree written (appgen.Consumers' Emit);
	// commit base; edit one byte of server/cmd/server/main.gen.go (inside the integration task's scope) and
	// commit head; `edit-scope --spec <integration spec> --base <base> --head <head> --no-main` exits 1 and
	// stderr contains "EDIT-GENERATED-DRIFT" and "cmd/server/main.gen.go". Regenerating instead (no hand edit)
	// exits 0.
}

// server/internal/checkidentity_test.go
func TestCheckIdentity_SingleJoin(t *testing.T) {
	// (a) Source scan of the non-test .go files under cmd/aiarch-state-mcp, internal/manager/delivery,
	// internal/resourceaccess/projectstate, internal/engine/review: zero matches of
	// `\.Component\s*==|==\s*\w+\.Component\b` (every join goes through methodcheck.ContractsOf).
	// (b) A CheckRun whose scenario results carry Contract is joined by delivery's view under that contract
	// even when two contracts share a scenario id (results for "a:x" and "b:x" both land, each under its own).
}
```
Run `GOWORK=$A/go.work go test ./internal/ ./cmd/aiarch-state-mcp/ -run 'TestEditScope_EveryPlanned|TestTaskSpec_GeneratedIs|TestGate_GeneratedPath|TestCheckIdentity' -v` — Expected: FAIL (FIX-053 paths unmatched under the glob scope, e.g. a planned `server/internal/scenariocompose/compose.gen.go` outside the Manager's construction task, no `EDIT-GENERATED-DRIFT`, `Contract` unset).
- [ ] **Step 6: Implement D6/D7.** In `reviewengine.go`: `generatedOf(tp taskProject, ms methodassets.MethodSpec, facts TaskFacts) []plan.Output` = the `Plan` of the consumers the task runs (`spec.Contracts` → `modelgen` restricted to them, `spec.Generate` → their consumers by name); scope = agent-owned globs (unchanged) ∪ `plan.Paths(outs, plan.Seam{})` ∪ `plan.Paths(outs, plan.Generated{})`; `compositionPaths` deleted. `TaskSpec.Generated = plan.Paths(outs, plan.Generated{})`. `editscope.go`: `generatedDrift(root string, spec review.TaskSpec, changed []string) ([]string, error)` — for every changed path in `spec.Generated`, run the owning consumers' `Emit` over `appgen.Input{Project: <head's project.json>, Tree: os.DirFS(<head checkout module dir>)}` and compare bytes; a difference is reported as `EDIT-GENERATED-DRIFT <path>` and exits 1 like an out-of-scope path. `checks.go` `finish` sets `CheckResult.Contract = c.Source.Contract`; `scenarioIDOf(res, contract)` = `res.Kind == scenario && res.Contract == contract` then the id suffix. Bump `TaskSpecSchemaVersion` to 7.
- [ ] **Step 7: Run** `GOWORK=$A/go.work go test ./internal/ ./cmd/aiarch-state-mcp/ -run 'TestEditScope|TestTaskSpec|TestGate_|TestCheckIdentity|TestRunChecks|TestRecordCheckRun' && GOWORK=$A/go.work go test ./internal/engine/review/ ./internal/manager/delivery/` — Expected: PASS.
- [ ] **Step 8: Commit** (app, explicit paths) `git commit -m "refactor(review): task-spec derivation moves to the review Engine (ruling 5); edit scope and drift read the generator manifests; check identity (checkId, contract)"`.

---

### Task 6.3: Tier 1 and Tier 2 admission

**Files:**
- Create: `framework-go-app-generator/appgen/admit.go`, `appgen/admit_test.go` (platform: the pure Tier‑1 core, so the conformance proof and the app share it)
- Modify: `.aiarch/state/project.json` reviewEngine `$defs` (+`AdmissionProblem`, `AdmissionSeverity` enum `local|incomplete`, `AdmissionTier` enum `tier1|tier2`) and ops (+`AdmitArtifact`); view `execute-a-project-activity`: the step that stages the task output (`delivery-manager → project-state-access StageTaskOutput…`) gains `delivery-manager → review-engine AdmitArtifact(project, kind) → problems — Tier 1: every generator that reads the artifact emits and typechecks it`.
- Modify: `server/internal/engine/review/reviewengine.go` (`AdmitArtifact` over `appgen.AdmitTier1`)
- Modify: `server/cmd/aiarch-state-mcp/constructverbs.go:148-195` (`recordServiceContract`), `:428-457` (`recordPhaseArtifact`), `state.go:41-130` (`putDraftModel`), `checks.go` (`runAdmission`), `projectstate/projectstateaccess.go` (`CheckKindAdmission`, `admissionCheckTimeoutSec = 1800`; the engine adds the check per D9), `internal/manager/delivery/deliveryactivity.go:1037,1892` and `deliverymanager.go:1713` (Tier 1 before each staging write)
- Modify: `go.work` (+`../archistrator-platform/framework-go-conformance`)
- Create: `server/cmd/aiarch-state-mcp/admission_test.go`

**Interfaces (Produces):**
```go
// framework-go-app-generator/appgen/admit.go
type Severity interface{ isSeverity() } //sumtype:decl
type Local struct{}      // refuse the write
type Incomplete struct{} // accept with a warning; Tier 2 enforces
type Problem struct {
	Consumer, Path, Message string
	Severity                Severity
}
// AdmitTier1 emits every consumer of kind over in (Tree nil), parses every emitted .go file and typechecks
// each emitted package with go/types against an importer that resolves only emitted siblings (D8). It returns
// every problem at once, sorted by consumer then path. A consumer whose Emit fails is one problem: Incomplete
// when its error names an id absent from the project (a component, contract or operation the design has not
// recorded yet), else Local.
func AdmitTier1(in Input, kind ArtifactKind) []Problem

// review contract additions
type AdmissionProblem struct { Consumer, Path, Message string; Severity AdmissionSeverity; Tier AdmissionTier } // JSON names lowerCamel
AdmitArtifact(rc fweng.Context, project string, kind string) ([]AdmissionProblem, error) // Tier 1 only; an unknown kind → nil, nil
```

- [ ] **Step 1: Write the failing tests (platform).**
```go
// appgen/admit_test.go
func TestAdmitTier1_RefusesContractGeneratorsCannotType(t *testing.T) {
	// greenfield fixture with one contract operation's result $ref renamed to a $def that exists but whose
	// modelgen type is not the one temporalgen's invoker returns (an emitted-sibling type mismatch): one Local
	// problem whose Consumer is "appgen temporal" and whose Path is the invoker file.
}
func TestAdmitTier1_IncompleteIsWarning(t *testing.T) {
	// a Manager contract whose deps name a component the System does not have yet: the consumer's Emit fails
	// naming it → Severity Incomplete, and no Local problem.
}
func TestAdmit_AllFindingsAtOnce(t *testing.T) {
	// two independent defects in two contracts → two problems from one call (no early return).
}
func TestAdmitTier1_OpaqueImportsAreNotFindings(t *testing.T) {
	// the clean greenfield fixture admits with zero problems although no stdlib, framework-go or Temporal
	// package is resolvable by the importer.
}
func TestAdmitTier1_UnderWorkflowBudget(t *testing.T) {
	// over testdata/archistrator.project.json, kind serviceContract: three runs, the median under 500 ms (D8).
}
```
Run `cd $P/framework-go-app-generator && GOWORK=off go test ./appgen -run 'TestAdmit' -v` — FAIL (`undefined: AdmitTier1`). Implement (`go/parser.ParseFile` per emitted file; `types.Config{Importer: siblingImporter, Error: collect}`; an opaque package is `types.NewPackage(path, lastSeg)` marked complete; drop a `types.Error` whose node (located via `ast.Inspect` at its position) is a `SelectorExpr` whose `X` is an `Ident` bound to an opaque `PkgName`); run — PASS. Commit `git commit -m "feat(appgen): AdmitTier1 — emit every consumer of an artifact and typecheck the generated siblings, all findings at once"`.
- [ ] **Step 2: Write the failing tests (app, workspace mode; add `framework-go-conformance` to `go.work`).**
```go
// server/cmd/aiarch-state-mcp/admission_test.go
func TestRecordServiceContract_RefusesTier1Local(t *testing.T) {
	// a session over the todomvc fixture records a list-manager contract with the AdmitTier1 Local defect of
	// Step 1: the verb errors, the message lists every problem ("appgen temporal", the path), and project.json
	// is byte-unchanged.
}
func TestRecordServiceContract_Tier1IncompleteIsAWarning(t *testing.T) {
	// the Incomplete case: the write lands and the tool result text contains "Tier 1 (incomplete, enforced at Tier 2)".
}
func TestPutDraftModel_RunsTier1ForTheSystem(t *testing.T)    // the System slot is a consumer kind: a Local problem refuses the draft
func TestRecordPhaseArtifact_TestPlanRunsTier1(t *testing.T)   // testgen is the consumer
func TestAdmitTier2_RunsAtCommitOnly(t *testing.T) {
	// (a) the specs of the fixture's detailed_design, test_plan and acceptance_plan tasks carry a check of
	// Kind "admission"; no construction/integration task's spec does; (b) none of the three verbs above runs it
	// (a fake RunChainOn counter stays 0 across their calls); (c) run-checks over a spec holding it runs
	// conformance.RunChainOn once over the checkout's project.json and reports "fail" with the failing step's
	// stderr tail when the chain fails.
}
```
Run `cd $A/server && GOWORK=$A/go.work go test ./cmd/aiarch-state-mcp/ -run 'TestRecordServiceContract_|TestPutDraftModel_RunsTier1|TestRecordPhaseArtifact_TestPlan|TestAdmitTier2' -v` — FAIL.
- [ ] **Step 3: Implement.** Contract + `make gen-models gen-fakes`; `AdmitArtifact` maps `appgen.AdmitTier1` (Input from the raw project: `ModulePath` from the document's module, `ModuleDir` from `moduleDir`). The three verbs, after encoding the candidate bytes and before `writeProjectBytes`/`applyConstructionMutation`'s write: `problems := review.NewReviewEngine().AdmitArtifact(...)`; any `local` → return an error listing all; `incomplete` → appended to the tool's success text. Server rail: before each staging write (`deliveryactivity.go:1037,1892`, `deliverymanager.go:1713`), `wf.Review.AdmitArtifact` over the encoded staged project for the envelope's kind (`ModelEnvelope.Kind` wire name); a `local` problem fails the stage as a task-output refusal with the problems as its detail (`workflow.GetVersion(ctx, "fpc6-tier1-admission", workflow.DefaultVersion, 1)` guards it). Tier 2: the Engine appends `{ID: "admission:tier2", Kind: "admission", Title: "The project builds through the conformance chain (Tier 2)", TimeoutSec: admissionCheckTimeoutSec}` to the D9 tasks; `checks.go` `runAdmission` = `conformance.RunChainOn(ctx, "project", raw, t.TempDir-equivalent scratch)` with `checkKindOrder[admission] = 2` (with smoke).
- [ ] **Step 4: Run** `GOWORK=$A/go.work go test ./cmd/aiarch-state-mcp/ -run 'TestRecord|TestPutDraftModel|TestAdmitTier2|TestRunChecks' && GOWORK=$A/go.work go test ./internal/ -run 'TestTaskSpec|TestEditScope' && GOWORK=$A/go.work go test ./internal/manager/delivery/ ./internal/engine/review/` — Expected: PASS.
- [ ] **Step 5: Commit** (app) `git commit -m "feat(admission): Tier 1 at every record (both rails), Tier 2 as the venue's admission check at commit"`.

---

### Task 6.4: Typed failure causes

**Files:**
- Modify: `.aiarch/state/project.json` — agenticJobAccess `$defs`: `FailureCause` (x-go-sumtype `{iface: FailureCause, marker: isFailureCause, discriminatorKey: kind, kindEnum: FailureCauseKind}`, variants `modelUnreachable→ModelUnreachable{detail}`, `rateLimited→RateLimited{detail}`, `runnerLost→RunnerLost{detail}`, `infraTimeout→InfraTimeout{detail}`, `agentGaveUp→AgentGaveUp{detail}`, `noProgress→NoProgress{fingerprint}`, `toolRefusedByPolicy→ToolRefusedByPolicy{paths []string}`, `gateRed→GateRed{checks []string}`, `unknown→UnknownCause{detail}`), `FaultClass` (string enum `host|agent|gate|unknown`), `PipelineObservation` + `ReachedModel bool`, `Cause *FailureCause` (omitempty), `FaultClass FaultClass`, `GateFailures []string`; projectStateAccess `$defs`: the same `FailureCause` sum, `TaskAttemptInput.cause` (optional).
- Modify: `server/internal/resourceaccess/agenticjob/agenticjobaccess.go` — `episodeAccumulator` gains `reachedModel bool` (set in `observeAssistant` :3578 when the model is not `"<synthetic>"` and `usage.output_tokens > 0`); `localRun` gains `reachedModel`, `cause`; `awaitCompletion` (:3802) classifies; `runGates` (:2671) records the red check ids and the edit-scope verdict separately; fingerprint stored only when `reachedModel`; `ObserveAgenticJob` (:3991) publishes; the GH arm: `ghActionsClient` (:149) gains `listArtifacts`/`downloadArtifact`, `ObserveAgenticJob` (:398) on a terminal run reads `execution-log-*` and `check-results-*`.
- Modify: `method-assets/assets/workflows/aiarch-construct.yml.tmpl:624-660` (the claude step gets `id: agent`; new step `Upload the agent's execution log`, `if: ${{ always() && steps.agent.outputs.execution_file != '' }}`, `actions/upload-artifact@v4`, `name: execution-log-${{ inputs.activity_id }}-${{ github.run_id }}-${{ github.run_attempt }}`, `path: ${{ steps.agent.outputs.execution_file }}`, `retention-days: 90`), `method-assets/workflows_test.go`
- Modify: `server/internal/manager/delivery/deliverymanager.go:12635-12661` (`csPipelineObservation` + `ReachedModel`, `Cause`, `GateFailures`), `deliveryactivity.go:5129-5140` (`observePipeline` maps them), `:1161-1230` (`produceConstructionChange`), `:1335-1430` (`runTaskVariance`), `:6063-6093` (`resolveWorkAttempt` records `Cause`), `projectstate` `RecordAttemptOutcome` (:11309) stores it
- Create: `server/cmd/aiarch-state-mcp/failurecause_test.go`, `server/internal/variance_test.go`

**Interfaces (Produces):**
```go
// agenticjob (generated): type FailureCause interface{ Kind() FailureCauseKind; isFailureCause() } and the nine *variant types;
// MarshalFailureCause / UnmarshalFailureCause refuse an unknown kind.
// agenticjob (hand, unexported): func classifyLocal(run localOutcome) (FailureCause, FaultClass)
//   timedOut && !reached → *InfraTimeout; run lost (localRunLostDiagnostic) → *RunnerLost;
//   !reached && stderr/stream says 429 / "rate limit" → *RateLimited; !reached → *ModelUnreachable;
//   reached && edit-scope gate red → *ToolRefusedByPolicy{paths}; reached && checks red → *GateRed{checks};
//   reached && agent exit≠0 or no commits → *AgentGaveUp; anything else failed → *UnknownCause.
// delivery (hand, unexported):
func faultClassOf(c agenticjob.FailureCause) faultClass // exhaustive type switch, no default
func (wf *csWorkflows) gateTruth(ctx workflow.Context, tc taskContext, attemptID string) (failed []string, recorded bool) // readActivityBranchProject + selectCheckRun(&proj, activity, task, "", []string{attemptID}, true)
const hostFaultBackoffStart = time.Minute; const hostFaultBackoffMax = 15 * time.Minute; const hostFaultWindow = 2 * time.Hour
```

- [ ] **Step 1: Write the failing tests.**
```go
// server/cmd/aiarch-state-mcp/failurecause_test.go — the local executor with a fake `claude` on PATH
// (acceptancejob_test.go's pathWithout/stateBinary helpers; a script printing stream-json lines then exiting).
func TestAgenticJob_NoFingerprintWhenModelUnreached(t *testing.T) {
	// fake claude prints {"type":"assistant","message":{"model":"<synthetic>","content":[{"type":"text","text":"API Error: Connection error."}]}}
	// and {"type":"result","subtype":"success","is_error":true} and exits 1; the integration task's checks then go red.
	// ObserveAgenticJob terminal: Phase PhaseFailed, ReachedModel false, Cause is *agenticjob.ModelUnreachable,
	// FaultClass "host", ProgressFingerprint == nil.
}
func TestAgenticJob_RateLimitedIsHost(t *testing.T)    // stderr "429 rate_limit_error": *RateLimited, FaultClass host
func TestAgenticJob_RedChecksAreGateRed(t *testing.T) // a real assistant turn (model "claude-opus-5-5", output_tokens 12) + red checks:
	// ReachedModel true, *GateRed whose Checks equal the non-pass checkIds of check-results/results.json, GateFailures equal, fingerprint set.
func TestAgenticJob_RunnerLostAfterRestart(t *testing.T) // a fresh access observing a foreign local handle: *RunnerLost, host
func TestAgenticJob_GitHubMissingExecutionLogIsUnknown(t *testing.T) // GH arm over a fake ghActionsClient with no execution-log artifact: *UnknownCause, ReachedModel false

// server/internal/variance_test.go — the Temporal testsuite harness of pmsendbackhold_test.go, delivery with a fake agenticJobAccess.
func TestVariance_HostFaultNeverConsumesAttempt(t *testing.T) {
	// the fake answers 12 consecutive *ModelUnreachable failures then success: the task succeeds; the attempt ledger
	// records 13 attempts, 12 with cause.kind "modelUnreachable"; the variance budget (maxVarianceAttempts = 10) was
	// never reached; the test clock advanced by the backoff (1,2,4,8,15,15,… minutes).
}
func TestVariance_HostFaultEscalatesAfterTheWindow(t *testing.T) // unbroken host faults for > hostFaultWindow → the escalation (escalateTaskVariance), not VarianceExhausted
func TestVariance_AgentFaultConsumes(t *testing.T)                // 10 *AgentGaveUp → VarianceExhausted, as today
func TestGateRed_TruthFromRunnerResults(t *testing.T) {
	// the fake reports *GateRed{checks: ["command:build"]} but the CheckRun recorded on the branch for that attempt has
	// "command:build" pass and "scenario:listManager:x" fail → the recorded cause is GateRed{checks: ["scenario:listManager:x"]};
	// a GateRed with no recorded CheckRun is recorded as UnknownCause and consumes an attempt.
}
func TestCause_SwitchExhaustive(t *testing.T) {
	// for every agenticjob.FailureCauseKind constant (the generated kind list): newFailureCauseForKind succeeds and
	// faultClassOf returns host for the four host kinds, agent for the three agent kinds, gate for gateRed, unknown for unknown.
	// (gochecksumtype in `make lint` is the compile-time half.)
}
```
- [ ] **Step 2: Run** `cd $A/server && GOWORK=$A/go.work go test ./cmd/aiarch-state-mcp/ -run 'TestAgenticJob_' -v && GOWORK=$A/go.work go test ./internal/ -run 'TestVariance_|TestGateRed_|TestCause_' -v` — Expected: FAIL (`undefined: agenticjob.ModelUnreachable`).
- [ ] **Step 3: Implement.** Schema + `make gen-models gen-fakes`. RA: classification per the table above, fingerprint only when `reachedModel` (so a model-unreached red never compares equal). GH arm: on a terminal observation, `listArtifacts(runID)`; `execution-log-*` → `parseEpisodeStream` over the JSON array's elements written as lines; `check-results-*` → `results.json` non-pass ids → `GateFailures`; neither → `UnknownCause`. Template: the `id: agent` and upload step; `workflows_test.go` `TestWorkflowTemplatesUploadTheExecutionLog` asserts the step, its `if`, and the artifact name. Manager: `observePipeline` maps; `produceConstructionChange` keeps two counters — `dispatches` (every dispatch, numbered attempts as today) and `consumed` (checked against `maxVarianceAttempts`) — and before `runTaskVariance` switches on `faultClassOf(obs.Cause)`: `host` → `workflow.Sleep(backoff)` (doubling from `hostFaultBackoffStart` to `hostFaultBackoffMax`), `continue` without incrementing `consumed`, and once the first host fault of the streak is older than `hostFaultWindow` → `escalateTaskVariance`; `gate` → `gateTruth` decides (`recorded` false → `UnknownCause`); `agent`/`unknown` → `consumed++` then today's `runTaskVariance`; a repeated fingerprint (reached model only) records `*NoProgress`. Everything behind `workflow.GetVersion(ctx, "fpc6-failure-cause", workflow.DefaultVersion, 1)`. `resolveWorkAttempt` sets `rec.Cause`. Bench: `archistrator-bench/src/…/classify.ts` should read `activityExecution.<id>.attempts[].cause.kind` — **noted for the bench repo, not done here.**
- [ ] **Step 4: Run** the Step 2 commands — Expected: PASS; `cd $P/method-assets && GOWORK=off go test ./ -run TestWorkflowTemplates` — PASS; `GOWORK=$A/go.work go test ./internal/manager/delivery/ ./internal/resourceaccess/agenticjob/` — PASS; `make lint` (gochecksumtype) — 0 issues.
- [ ] **Step 5: Commit** platform `git commit -m "feat(method-assets): the construct workflow uploads the agent's execution log"`; app `git commit -m "feat(delivery): typed failure causes — reachedModel measured by the agentic-job RA, host faults never consume an attempt, gateRed from the recorded run"`.

---

### Task 6.5: Doctrine registry

**Files:**
- Create: `method-assets/doctrine/registry.json`, `method-assets/doctrine/waivers.max`, `method-assets/doctrine/promptlint.max`, `method-assets/doctrine.go`, `method-assets/doctrine_test.go`
- Create: `framework-go/arch/ruleids.go`, `ruleids_test.go`; modify `framework-go/arch/filelayout.go` (hoist the violation rule strings `hand-activity-registration`, `workflow-in-impl-file`, `file-not-allowed`, `workflow-func-outside-manager`, `workflow-file-multiple-funcs`, `workflow-file-name`, `scenario-tests-only`, `test-package-not-external` to named constants beside `ruleTestCompose`), `contractdup.go:81-84`, `scenariocompose_testonly.go:14`
- Modify: `framework-go/methodcheck/emitters.go:17` (export)
- Create: `framework-go-conformance/doctrine_test.go`
- Modify: the prompts each enforced entry needs a citation in (`method-assets/assets/claude/{commands,agents,skills}/**`), incl. `commands/frontend-construction.md` (E30's prompt line: "the mcpApp host-bridge seat `src/<id>/main.tsx` is generated; never edit it `[EDIT-GENERATED-DRIFT]`")

**Interfaces (Produces):**
```go
// framework-go/methodcheck/emitters.go
func EmittedRuleIDs() []RuleID // sorted; emittedRuleIDs() stays the map the coverage test reads
// framework-go/arch/ruleids.go
func RuleIDs() []string // every Rule string an arch violation can carry, sorted

// method-assets/doctrine.go
type Scope string // "builtApp" | "agentConduct" | "judgement"
//sumtype:decl
type RuleRef interface{ isRuleRef() }
type MethodcheckRule struct{ Rule, FiredBy string }                  // kind "methodcheck"
type ArchRule struct{ Rule, FiredBy string }                         // kind "arch"
type GeneratorInvariant struct{ Module, Package, Test string }       // kind "generatorInvariant"
type Waiver struct{ Reason, Owner, Expires string }                  // Expires YYYY-MM-DD
type DoctrineEntry struct {
	ID, Statement string
	Scope         Scope
	EnforcedBy    []RuleRef
	PromptRefs    []string // "<assets-relative file>#<anchor text>"
	Waiver        *Waiver
}
func DoctrineRegistry() ([]DoctrineEntry, error) // decodes the embedded doctrine/registry.json strictly; unknown ref kind → error
```
Seed entries (each `builtApp` unless noted; `enforcedBy` names the existing firing test):
| id | statement | enforcedBy |
|---|---|---|
| `layer-a-interaction` | a control's own state flips immediately under latency | generatorInvariant `framework-go-app-generator/testgen.TestPlaywright_LayerA_ImmediateUnderLatency` |
| `a11y-always-on` | every designed element has a role and an accessible name | methodcheck `UID-ROLE`, `UID-NAME` (their `rules_uidesign_test.go` Fires tests) |
| `no-idp-resource` (FIX-055) | the platform owns identity; a design has no IdP Resource | none exists today → `waiver {reason: "FIX-055 is prompt-only; a SYS rule is owed", owner: "founder", expires: "2026-12-31"}` |
| `managers-temporal-hosted` (FIX-035) | every Manager is Temporal-hosted | generatorInvariant `framework-go-app-generator/temporalgen.TestGenerateGreenfieldGolden` (`temporalgen_test.go`; temporalgen emits every Manager's worker — if the golden does not fail on a Manager left out, a dated waiver instead) |
| `clients-contracted` | every built Client has a contract | methodcheck `TP-CLIENT-CONTRACTED` (`TestTP_ClientContracted_Fires`) |
| `no-hooks` | agents write no test code | methodcheck `TP-HOOK`; arch `ARCH-TEST-COMPOSE` (`TestFileLayout_HooksFileIsViolation`) |
| `targets-are-design-elements` | a UI step targets a design element whose role takes the action | methodcheck `TP-UI-TARGET` (`TestTP_UITarget_RoleAgreement`) |
| `both-surfaces-drawn` | every required surface is entered in every use case | methodcheck `SYS-SURFACE-ENTRY` (`TestSYS_SurfaceEntry_FiresWithoutAlt`) |
| `required-surfaces` | the System has a Client for each delivery surface | methodcheck `SYS-SURFACE-REQUIRED` (`TestSYS_SurfaceRequired_FiresOnMissingRest`) |
| `composition-boots` | the composed app boots and answers /readyz, the SPA and /mcp | generatorInvariant `framework-go-app-generator/devstack.TestSmoke_ReadyzMustAnswer200` (`smoke_test.go:120`) |
| `discriminating-tests` | every scenario fails against a null subject | generatorInvariant `framework-go-app-generator/cmd/testgen.TestDiscriminate_SurvivorIsAnError` |
| `generated-files-untouched` (agentConduct) | an agent never edits a generated path | generatorInvariant `framework-go-app-generator/appgen.TestPlan_MatchesEmit` + the app gate `EDIT-GENERATED-DRIFT` (cited in prompts) |
The implementer greps each `firedBy` name before writing it (`grep -rn "func <Name>(" $P/<module>`); a seed whose enforcing test does not exist gets a dated waiver, never an invented name.

- [ ] **Step 1: Write the failing tests.**
```go
// framework-go/arch/ruleids_test.go
func TestRuleIDs_CoverEveryViolationRule(t *testing.T) {
	// parse filelayout.go, contractdup.go, scenariocompose_testonly.go; every composite literal of a
	// *Violation type's Rule field is a constant listed by RuleIDs(); RuleIDs() is sorted and unique.
}
// method-assets/doctrine_test.go
func TestRegistry_Decodes(t *testing.T)                     // DoctrineRegistry() errors on an unknown ref kind and on an unknown field
func TestRegistry_WaiversOnlyDecrease(t *testing.T)         // count of entries with Waiver <= doctrine/waivers.max; every Expires parses
func TestRegistry_EnforcedRulesArePrompted(t *testing.T)    // each enforced methodcheck/arch rule id appears as `[ID]` in ≥1 file under assets/claude
func TestRegistry_PromptRefsResolve(t *testing.T)           // each promptRef's file exists and contains its anchor text
func TestPromptLint_NormativeSentencesCiteARule(t *testing.T) {
	// over assets/claude/commands/*.md and assets/claude/agents/*.md: sentences matching (?i)\b(must|never|always|every|required)\b
	// without a `[A-Z][A-Z0-9-]+]` citation; their count <= doctrine/promptlint.max (a ratchet that only decreases).
}
// framework-go-conformance/doctrine_test.go (untagged: no Docker)
func TestRegistry_BuiltAppRulesEnforcedOrWaived(t *testing.T) {
	// every builtApp entry has EnforcedBy or a Waiver with Expires >= today; every MethodcheckRule.Rule is in
	// methodcheck.EmittedRuleIDs(); every ArchRule.Rule is in arch.RuleIDs(); every FiredBy / GeneratorInvariant.Test
	// names a `func <Name>(t *testing.T)` declared in the named package of the platform checkout (platformRoot(),
	// chain.go:58) — the firing test is the rule's violating fixture (GC-4).
}
```
- [ ] **Step 2: Run** `cd $P/framework-go && GOWORK=off go test ./arch -run TestRuleIDs -v`; `cd $P/method-assets && GOWORK=off go test ./ -run 'TestRegistry|TestPromptLint' -v`; `cd $P/framework-go-conformance && GOWORK=off go test ./ -run TestRegistry_BuiltApp -v` — Expected: FAIL (undefined symbols / missing registry).
- [ ] **Step 3: Implement** `EmittedRuleIDs`, `RuleIDs` (+ the hoisted constants), `doctrine.go` (embed `doctrine/registry.json`), the registry with the seed table, `waivers.max` = the seed's waiver count, `promptlint.max` = the lint's count on the current prompts (recorded in the commit body), the prompt citations `TestRegistry_EnforcedRulesArePrompted` needs (the E30 line included). Re-seat the app's `.claude/` later (Task 6.6 `make claude-assets`).
- [ ] **Step 4: Run** the Step 2 commands — Expected: PASS; `GOWORK=off go test ./methodcheck ./arch` in framework-go and `GOWORK=off go test ./` in method-assets — PASS.
- [ ] **Step 5: Commit** `git commit -m "feat(doctrine): registry of built-app doctrine with its enforcing rules; waiver and prompt-lint ratchets; methodcheck.EmittedRuleIDs, arch.RuleIDs"`.

---

### Task 6.5b: Suite speed-up (E16/E18)

Measured cost (Plan 2 E16): the full app suite takes ~2 h; `cmd/aiarch-state-mcp` takes ~73 min because each integration gate's `command:test-discriminate` runs the module-wide null run as one `go test` per (scenario, operation) — 206 runs on todomvc (78 scenarios, 128 stimulus operations), ~5 s each, and three gates repeat it. D13 says what is clean.

**Files:**
- Modify: `framework-go-app-generator/testgen/emit_null.go` (the generated `null.gen_test.go` gains `TestDiscriminate` and the package's scenario table), `testgen/testgen.go` (`Discrimination.Batch string` — the package a run batches into; "" for a web scenario), `testgen/emit_null_test.go`
- Modify: `framework-go-app-generator/cmd/testgen/discriminate.go` (per-package batches), `cmd/testgen/discriminate_test.go`
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` (`DiscriminateRunTimeoutSec` doc; new `DiscriminateInProcessRunSec = 5`, `DiscriminatePackageSec = 30`) and the Engine's `discriminateTimeoutSec` (moved in Task 6.2), `server/internal/taskspec_test.go` (`TestDiscriminateBudgetScalesWithTheModulesRuns`)

**Interfaces (Produces):**
```go
// generated null.gen_test.go (shape):
// scenarioTests are the package's scenario tests by name, for the null-subject run's in-process driver.
var scenarioTests = map[string]func(*testing.T){"TestScenario_<id>": TestScenario_<id>, …}
// TestDiscriminate runs the null-subject run's list (AIARCH_DISCRIMINATE: a JSON file of
// [{"id": "<test>#<nullOps>", "test": "TestScenario_<id>", "nullOps": "<Iface>.<Op>,…"}]) as sequential
// subtests in this one process: each sets nullOps and AIARCH_NULL_OPS (t.Setenv, so a launched app inherits
// it), runs the scenario's test function, and restores both. Without the variable it skips.
func TestDiscriminate(t *testing.T)

// cmd/testgen/discriminate.go
// runBatch runs one package's entries in one `go test -count=1 -json -run '^TestDiscriminate$' ./<pkg>/`
// with AIARCH_DISCRIMINATE set, and classifies each entry from its subtest's -json events (pass → ranPassed,
// fail with its own "--- FAIL: TestDiscriminate/<id>" → ranFailed, skip/missing → ranInconclusive).
func runBatch(o options, pkg string, entries []testgen.Discrimination, run goTestRunner) (map[string]runOutcome, string)
```

- [ ] **Step 1: Write the failing tests.**
```go
// testgen/emit_null_test.go
func TestNull_EmitsInProcessDriver(t *testing.T) {
	// the generated null.gen_test.go contains `func TestDiscriminate(t *testing.T)`, a scenarioTests entry for every
	// TestScenario_ function the package's scenarios file declares, and `t.Setenv("AIARCH_NULL_OPS"`; it compiles
	// (the compile harness of compile_harness_test.go).
}
// cmd/testgen/discriminate_test.go (fake goTestRunner, the existing pattern)
func TestDiscriminate_OneProcessPerPackagePhase(t *testing.T) {
	// a package with 3 scenarios × 2 operations: exactly two runner calls for it (baselines, then the mutants of the
	// passing baselines), both `-run ^TestDiscriminate$`; the verdicts equal the per-run driver's on the same fake outcomes.
}
func TestDiscriminate_BrokenBaselineRunsNoMutants(t *testing.T) // a failing baseline → TP-BROKEN, its mutants absent from the second batch
func TestDiscriminate_HostBootFailureRetriesOnlyThoseEntries(t *testing.T) // entries whose output matches hostBootFailure rerun (≤ hostBootAttempts) in a smaller batch
func TestDiscriminate_WebStaysPerRun(t *testing.T) // web Discriminations still go through `go tool webscenarios` one per run
func TestDiscriminate_SurvivorIsAnError(t *testing.T) // unchanged assertion, now through runBatch: a passing mutant prints "TP-DISCRIMINATE <c> <s> <op>" and exits 1
```
- [ ] **Step 2: Run** `cd $P/framework-go-app-generator && GOWORK=off go test ./testgen ./cmd/testgen -run 'TestNull_|TestDiscriminate_' -v` — Expected: FAIL.
- [ ] **Step 3: Implement** the generated driver (sequential subtests; no `t.Parallel`), `Discrimination.Batch`, `runBatch` (temp list file per batch; parse `go test -json` `Action`/`Test`/`Output` events), and the two-phase loop in `discriminate`. Budget: `discriminateTimeoutSec` = Σ over Go packages of (`DiscriminatePackageSec` + runs × `DiscriminateInProcessRunSec`) + Σ client runs × (`DiscriminateInProcessRunSec` + `DevstackBootSec`) + web runs × (`DiscriminateRunTimeoutSec` + `DevstackBootSec`), floor `scenarioCheckTimeoutSec`; `TaskSpecSchemaVersion` → 8.
- [ ] **Step 4: Run** Step 2's command — PASS; `GOWORK=off go test ./testgen ./cmd/...` — PASS. Then measure (app, workspace mode): `cd $A/server && /usr/bin/time GOWORK=$A/go.work go test -count=1 -p 1 -timeout 120m ./cmd/aiarch-state-mcp/ -run 'TestFreshManagerIntegrationComposesTheServer$' -v` before (on the pre-task commit, via `git -C $P stash`-free `git worktree add --detach /tmp/fpc6-before <sha>`) and after; record both wall times in the commit body; `GOWORK=$A/go.work go test ./internal/ -run TestDiscriminateBudgetScalesWithTheModulesRuns` — PASS. Expected: the Manager gate's null run drops from ~17 min to the package-count bound (record the measured figure; no target is asserted).
- [ ] **Step 5: Commit** platform `git commit -m "perf(testgen): the null-subject run is one process per package per phase, switching null operations in-process (E16)"`; app (staged with the rest) — the budget constants.

---

### Task 6.6: Release, pin, proof, paid run

- Release framework-go-app-generator + method-assets; pin; own project.json amended for the review-Engine op; `make test` green.
- Conformance: `TestConformance_Manifests` (every path the chain writes is in some `Plan`).
- Then spec §7.7: one paid todomvc run. Acceptance criterion: COMPLETE by the bench's definition, or every bench failure maps to a red archistrator gate that fired first. Record in `ledger.jsonl` and the memory file.

**Files (platform):** create `framework-go-conformance/manifests_test.go` (tag `conformance`); modify `.github/workflows/platform-checks.yml` (job `conformance-manifests`, modelled on `conformance-proof`, running `GOWORK=off go test -tags conformance ./ -run 'TestConformance_Manifests$' -v -timeout 60m`) and `.github/workflows/release-gate.yml` (requires it beside the five); `go.mod` of app-generator, scenariohost, conformance; `method-assets/assets/scaffold/{go.mod.tmpl,go.sum.tmpl}`.
**Files (app):** `server/go.mod`/`go.sum` pins; everything Tasks 6.2–6.5b staged; `.claude/` re-seated.

```go
// framework-go-conformance/manifests_test.go (tag conformance)
// TestConformance_Manifests runs the chain over todomvc (RunChain into t.TempDir()) and walks the scaffold:
// every file whose bytes the chain's generator steps wrote (the set of paths that differ from the scaffold's
// own seed, excluding the impl overlay, node_modules, dist and test-results) is a planned path of some
// appgen.AllConsumers() Plan over the fixture (Input{Tree: nil}), with the role Plan gives it; and every
// planned Generated path exists in the scaffold after the chain.
func TestConformance_Manifests(t *testing.T)
```

- [ ] **Step 1: Proof (platform).** Write `TestConformance_Manifests`; run `cd $P/framework-go-conformance && GOWORK=off go test -tags conformance ./ -run 'TestConformance_Manifests$' -v -timeout 60m` — FAIL before Task 6.1 lands on the branch / PASS after; then the existing proofs `GOWORK=off go test -tags conformance ./ -run 'TestConformance_(Chain|Boot|ClientsOverWire|Discriminate_Web|McpAppSurface)$|TestConformance_Acceptance$' -v -timeout 150m` — PASS; untagged `GOWORK=off go test ./` — PASS (`TestRegistry_BuiltAppRulesEnforcedOrWaived`). Add the CI job. Commit `git commit -m "test(conformance): TestConformance_Manifests — every path the chain writes is planned; CI job conformance-manifests"`.
- [ ] **Step 2: Release (literal release step), tagging locally.** Per this run's release rules: tag each module locally at the current `fpc/main` commit; **never** query the proxy for a version whose tag is not pushed; never move a pushed tag; record each wave's push command under blockedActions (`git -C $P push origin fpc/main --tags`) and stop until the lead has pushed it, because the next wave tidies against the real proxy. Waves (dependency order):
  1. `framework-go/v0.26.0` (methodcheck `ContractsOf`/`EmittedRuleIDs`, arch `RuleIDs`). projectmodel is unchanged (stays v0.6.0).
  2. `framework-go-app-generator`: `go.mod` requires framework-go v0.26.0; `GOWORK=off GOFLAGS=-mod=mod GOMODCACHE=$(mktemp -d) go mod tidy` against the real proxy (after wave 1 is pushed); commit; tag `framework-go-app-generator/v0.28.0`.
  3. `framework-go-scenariohost`: requires app-generator v0.28.0, framework-go v0.26.0; tidy as above; commit; tag `framework-go-scenariohost/v0.9.0`.
  4. `method-assets`: `assets/scaffold/go.mod.tmpl` pins app-generator v0.28.0, scenariohost v0.9.0, framework-go v0.26.0, projectmodel v0.6.0; `go.sum.tmpl` regenerated against the real proxy with a fresh GOMODCACHE **after waves 2–3 are pushed**; `GOWORK=off go test ./`; commit; tag `method-assets/v0.29.0`.
  5. `framework-go-conformance`: requires method-assets v0.29.0 and the three above; tidy; commit; tag `framework-go-conformance/v0.5.0`. Release gate: the six conformance jobs (incl. `conformance-manifests`) green on that commit.
- [ ] **Step 3: Pin the app** (after wave 5 is pushed). `cd $A/server && GOWORK=off go get github.com/mixofreality-studio/archistrator-platform/framework-go@v0.26.0 …/framework-go-app-generator@v0.28.0 …/framework-go-scenariohost@v0.9.0 …/method-assets@v0.29.0 …/framework-go-conformance@v0.5.0 && GOWORK=off go mod tidy` (env per Commands); `GOWORK=off go build ./... && GOWORK=off go vet ./...`; `make claude-assets`.
- [ ] **Step 4: App gates.** `make gen-models gen-fakes gen-client gen-internal-tools gen-tests`; `cd webApp && npm run gen:api`; `cd $A/server && GOWORK=off go test -count=1 -p 1 ./internal/ ./cmd/aiarch-state-mcp ./internal/manager/delivery ./internal/engine/review ./internal/resourceaccess/agenticjob` — PASS (Tasks 6.2–6.5b's tests by name: `TestEditScope_EveryPlannedPathIsInSomeTaskScope`, `TestGate_GeneratedPathMustEqualRegeneration`, `TestCheckIdentity_SingleJoin`, `TestRecordServiceContract_RefusesTier1Local`, `TestAdmitTier2_RunsAtCommitOnly`, `TestAgenticJob_NoFingerprintWhenModelUnreached`, `TestVariance_HostFaultNeverConsumesAttempt`, `TestGateRed_TruthFromRunnerResults`, `TestCause_SwitchExhaustive`, `TestDiscriminateBudgetScalesWithTheModulesRuns`).
- [ ] **Step 5: Own project.** `make method-check`: the reviewEngine contract and the `execute-a-project-activity` view carry the new ops (ALIGN-*, CC-* green); any new TP-*/UC-* Error on the own project gets `{rule, reason: "ruling 8: own-project rebinding scheduled", expires: "2026-12-31"}` in `ruleWaivers` (as Plans 2, 4, 5 did), counted in the commit body.
- [ ] **Step 6: Full suite once.** `cd $A/server && GOWORK=off go test -p 1 -timeout 180m ./...`; `make method-check gen-tests-check gen-models-check gen-lifecycles-check gen-client-check gen-internal-tools-check lint test`; `cd $A/webApp && npm run typecheck && npx eslint src` — all green; record the suite's wall time beside Task 6.5b's measurement.
- [ ] **Step 7: Commit** (app, explicit paths — never `server/cmd/aiarch-state-mcp/acceptancecheck_test.go`) `git commit -m "feat(app): FPC plan 6 — manifests drive edit scope and drift, Tier 1/2 admission, typed failure causes, task-spec derivation in the review Engine; pins Plan 6 release"`.
- [ ] **Step 8: The paid run — prepared, NOT launched by this workflow.** Stop here and record under blockedActions exactly what the lead runs (bench `experiments/PROTOCOL.md` §3.2–3.6):
  - Preconditions: host on AC power (`pmset -g batt` shows "AC Power"); Docker up (`docker info` exits 0); > 40 GiB free on the volume holding `archistrator-bench-scratch` (`df -g`); Playwright chromium installed (`npx --prefix /Users/davidmarne/mixofrealitystudio/archistrator-bench playwright install chromium`); both trees clean at the Step 7 app commit and the wave-5 platform commit; the rate-window probe (`claude -p --output-format stream-json` one turn: `rate_limit_info.resetsAt`/`rateLimitType`) leaves room for a 2–3 h segment.
  - Build on the exact commits: `P2=/Users/davidmarne/mixofrealitystudio/benchwt/fpc6-<ts>`; `git -C /Users/davidmarne/mixofrealitystudio/archistrator worktree add --detach $P2/archistrator <appSha>`; `git -C /Users/davidmarne/mixofrealitystudio/archistrator-platform worktree add --detach $P2/archistrator-platform <platformSha>`; `npm ci --prefix $P2/archistrator/webApp`; `(cd $P2/archistrator && bash scripts/build-local.sh)`; `test -z "$(git -C $P2/archistrator status --porcelain)" && test -z "$(git -C $P2/archistrator-platform status --porcelain)"`; `./aiarch-state-mcp version` equals the server's `StateToolVersion` (spec-schema 8).
  - $0 smoke: `cd /Users/davidmarne/mixofrealitystudio/archistrator-bench && npm run bench -- run todomvc --archistrator $P2/archistrator --dry-run --scratch /Users/davidmarne/mixofrealitystudio/archistrator-bench-scratch`.
  - Launch: `cd /Users/davidmarne/mixofrealitystudio/archistrator-bench && DISABLE_AUTOUPDATER=1 nohup caffeinate -dimsu npm run bench -- run todomvc --archistrator $P2/archistrator --scratch /Users/davidmarne/mixofrealitystudio/archistrator-bench-scratch > /Users/davidmarne/mixofrealitystudio/archistrator-bench-scratch/bench-<ts>.log 2>&1 < /dev/null & disown` — never as a harness background task; `cost-watch --ceiling 150 --interval 60`; **budget cap $150** (stop and ask above it); kill the run's Temporal PID at the end; evidence freeze per §3.6.
  - Acceptance criterion (spec §7.7): COMPLETE by the bench's definition, or every bench failure maps to a red archistrator gate that fired first. Record the run in `archistrator-bench/analysis/todomvc/ledger.jsonl` (with that mapping) and in the memory file.

**Earmarks (not in this plan):**
- E1 cloud RA→RA ports via hooks (`hooks.go:937-962`); E2 cloud github-app arms share an AppClient via hooks; E3 non-identity `Finalize*` on the local profile — the local executor should be a declared local variant of agenticJobAccess (Task 6.4 classifies the executor as it is); E4 the local-hooked refusal in `composegen.Generate`; E5 the kinds table from WorkflowDecl + dynamic views; E6 scenariohost github-app over FakeGitHub; E7 one database per Postgres key; E8 delivery hand-test staging order; E9 composition-root env defaults routed through `Config`; E10 duplicate `ExecutionKinds` refused at boot (Plan 2 Task 2.11i).
- E11 `StepBindingView.hook/unobservable` in the deliveryManager and mcpClient view contracts; E12 the own project's 277 dated-skip rebindings; E13 own-project `make test-discriminate` survivors; E14 sandbox tests over the pinned toolchain (Plan 2).
- E15 scenariohost delegation claim (Plan 3 Task 3.0 status).
- E16 cross-gate reuse of one null run's verdicts (the three integration gates of one tree) — not built: Task 6.5b cuts each run's cost instead; E17 a model-declared clock for `advanceClock` over the wire; E18 one devstack boot shared by a package's client scenarios (D13: needs per-scenario isolation inside one boot); E19 the own `mcpClient` scenarios need the own local profile (E3); E20 `scenariocompose` emits nothing for clients' unused symbols — a registry of unused generated symbols (Plan 3).
- E21 step-granular failure attribution; E22 null-subject run over acceptance variants; E23 dead `derived-plan-check`/`derived-plan-write` targets and no sanctioned slot-9 writer; E24 one browser identity per launch (Plan 4).
- E25 the UI dialect's missing type-and-submit act and multi-act steps; E26 the fixture SPA's owner-less open-list (conformance `testdata/todomvc/README.md:232-235`).
- E28 acceptance variants through an MCP Apps host; E29 the own hand-written MCP Apps shell (Plan 5).
- FIX-055's rule (no IdP Resource): waived in the registry until a SYS rule exists.
- The bench's `classify.ts` reads `attempts[].cause.kind` (bench repo; Task 6.4 notes it).

## Done when
All six plans released and pinned; the paid run recorded with its mapping of bench failures to archistrator gates.
