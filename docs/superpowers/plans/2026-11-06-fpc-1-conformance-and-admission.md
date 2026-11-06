# FPC Plan 1 — Conformance Pipeline and Tier‑0 Admission

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A $0 CI pipeline that pushes a real todomvc fixture through every generator, builds, boots and probes the composed app; and a single loadability admission (`projectmodel` problems, all at once) that every write of `project.json` must pass.

**Architecture:** `framework-go-projectmodel` becomes the one loader every generator and the projectstate RA share, returning *all* problems. A new `framework-go-app-generator/conformance` package drives the exact command chain a scaffolded app's Makefile runs, against embedded fixtures, with generated null implementations so nothing agentic is needed. The platform CI gates releases on it; the app re-records the fixture through its own verbs so fixture and recorder cannot drift.

**Tech Stack:** Go 1.25/1.26 (`GOWORK=off`), modelgen/appgen/testgen/webgen CLIs (`go tool`), Docker (Postgres 16, Temporal dev server), Node/npm for the SPA typecheck, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-06-first-pass-correctness-design.md` §W7 Tier 0, §W8, §4b (fixture provenance, Tier‑1 placement). Index: `2026-11-06-fpc-0-index.md` (Global Constraints apply).

**Repos:** platform = `archistrator-platform` (work on a branch from origin/main 9784892d); app = `archistrator` (branch from origin/main b31bc72f). Local checkouts to use: `benchwt/loop/archistrator-platform` and this worktree.

## Global Constraints (plan-local additions)

- Fixture ids are the RUN-021 harvest's as-is (`todo-web-app`, `list-manager`, …); never hand-rename.
- Generators are invoked as commands, never as Go calls, in the conformance driver, so the pipeline exercises `cmd/*` exactly as an app does (`Makefile.scenarios.mk.tmpl` and `Makefile` targets).
- No `go build` or `tsc` inside any record verb (Tier 0 is loadability only; Tier 1/2 are Plan 6).

## Review Focus

1. **A contract with a lowercase operation name** (FIX-028): `projectmodel.Load` must report it as a problem; Task 1.2 test `TestValidateContract_UnexportedOpName`.
2. **A contract with `operations: []` or `null`** (FIX-041/042, RUN-013): Task 1.2 `TestValidateContract_ZeroOperations`.
3. **A built contract without `goPackage`** (FIX-048): Task 1.2 `TestValidateContract_MissingGoPackage`.
4. **A composed app that compiles but does not serve `/mcp` or the SPA** (FIX-050/051): Task 1.7 `TestConformance_Boot`.
5. **A second boot that leaves a Postgres volume behind** (FIX-063): Task 1.7 `TestConformance_BootTwice_NoOrphans`.

---

## File map

Platform (`archistrator-platform`):
- Create `framework-go-projectmodel/problems.go` — `Problem`, `Problems`, `(Problems) Error()`.
- Modify `framework-go-projectmodel/validate.go`, `contract.go`, `projectmodel.go` — `Load` collects every problem; new exported `ValidateContract`, `ValidateSystem`, `ValidateDeployment`.
- Create `framework-go-projectmodel/testdata/problems/*.json` — one fixture per problem class.
- Create `framework-go-app-generator/modelgen/emit_sumtype_test.go` + `modelgen/testdata/sumtype/` — pins the existing `x-go-sumtype` emitter (Plan 2 depends on it).
- Create `framework-go-app-generator/nullgen/` — `nullgen.go`, `nullgen_test.go`: null implementations + seeded `cmd/server/hooks.go` for any model.
- Create `framework-go-app-generator/cmd/nullgen/main.go`.
- Create `framework-go-app-generator/conformance/` — `fixtures.go` (embed), `chain.go` (the command chain), `boot.go` (devstack boot + probes), `conformance_test.go`.
- Create `framework-go-app-generator/testdata/conformance/todomvc/project.json`, `.../README.md`; `testdata/conformance/greenfield/project.json`.
- Modify `.github/workflows/platform-checks.yml` — job `conformance`.
- Modify `method-assets/assets/scaffold/Makefile.scenarios.mk.tmpl` — target `gen-null`.

App (`archistrator`):
- Modify `server/internal/resourceaccess/projectstate/projectstateaccess.go` — `admit(p *Project) error` called from the single serialisation point before every git write.
- Modify `server/cmd/aiarch-state-mcp/constructverbs.go:71-111` — `applyConstructionMutation` reports all `projectmodel` problems, not the first.
- Create `server/cmd/aiarch-state-mcp/conformance_test.go` — re-records the embedded fixture through the verbs.
- Modify `server/go.mod` — pin bumps.

---

### Task 1.1: Pin modelgen's sum-type emitter with a test

modelgen already emits sealed sum types from `x-go-sumtype` (`modelgen/emit_sumtype.go:13-63`, descriptor `descriptors.go:49-82`) but has no test. Plan 2 builds every new choice on it.

**Files:**
- Create: `framework-go-app-generator/modelgen/testdata/sumtype/contract.json`
- Create: `framework-go-app-generator/modelgen/emit_sumtype_test.go`

**Interfaces:**
- Produces: the guarantee that a `$def` of the form `{"oneOf":[{"$ref":"#/$defs/A"},{"$ref":"#/$defs/B"}], "x-go-sumtype": {"iface":"Shape","marker":"isShape","discriminatorKey":"kind","kindEnum":"ShapeKind","variants":[{"kind":"a","type":"A","kindConst":"ShapeKindA"},{"kind":"b","type":"B","kindConst":"ShapeKindB"}]}}` emits `//sumtype:decl type Shape interface{ Kind() ShapeKind; isShape() }`, `UnmarshalShape([]byte) (Shape, error)` refusing unknown kinds, wire `{"kind":"a","model":{...}}`.

- [ ] **Step 1: Write the fixture** `testdata/sumtype/contract.json`: a minimal contract (`component`, `layer: engine`, `goPackage: internal/engine/shape`, `interface` with one op `Area(shape Shape) float64`) whose `$defs` contain `A{"side":number}`, `B{"radius":number}` and the `Shape` def above.

- [ ] **Step 2: Write the failing tests** in `emit_sumtype_test.go`:

```go
func TestSumType_EmitsSealedInterface(t *testing.T)   // generated source contains "//sumtype:decl", "type Shape interface", "func (*A) isShape()", "func (*B) Kind() ShapeKind"
func TestSumType_UnmarshalRefusesUnknownKind(t *testing.T) // compile the generated package into a temp module with `go vet`, then run a tiny program: UnmarshalShape(`{"kind":"c","model":{}}`) returns an error containing `unknown kind "c"`
func TestSumType_RoundTrip(t *testing.T)                 // MarshalShape(&A{Side:2}) == `{"kind":"a","model":{"side":2}}` and back
```
Use the existing modelgen test harness for running the generator over a fixture (look at `modelgen/*_test.go` for the helper that emits to a temp dir; reuse it).

- [ ] **Step 3: Run** `cd framework-go-app-generator && GOWORK=off go test ./modelgen -run TestSumType -v` — Expected: FAIL (fixture/tests missing or assertions red).

- [ ] **Step 4: Fix only what the tests expose** in `emit_sumtype.go` (expected: nothing or small; if `UnmarshalX` does not name the unknown kind, make its error `fmt.Errorf("unknown kind %q for %s", kind, iface)`).

- [ ] **Step 5: Run** the same command — Expected: PASS.

- [ ] **Step 6: Commit** `git add modelgen/testdata/sumtype modelgen/emit_sumtype_test.go [modelgen/emit_sumtype.go]` / `git commit -m "test(modelgen): pin x-go-sumtype sealed interface, unknown-kind refusal and envelope round trip"`

---

### Task 1.2: `projectmodel` reports every problem (Tier 0 loadability)

**Files:**
- Create: `framework-go-projectmodel/problems.go`
- Modify: `framework-go-projectmodel/validate.go:22-60`, `contract.go` (`ParseContract`), `projectmodel.go:41` (`Load`)
- Create: `framework-go-projectmodel/testdata/problems/{unexported-op,zero-ops,missing-gopackage,unknown-field,bad-layer}.json`
- Test: `framework-go-projectmodel/validate_test.go` (extend)

**Interfaces:**
- Produces:
  ```go
  type Problem struct { Path string `json:"path"`; Code string `json:"code"`; Message string `json:"message"` } // Path like "serviceContracts.messageBus.interface.operations"
  type Problems []Problem
  func (p Problems) Error() string          // one line per problem, sorted by Path then Code
  func Load(raw []byte) (*Model, error)     // unchanged signature; err is Problems when any problem exists
  func ValidateContract(key string, raw json.RawMessage) Problems
  func ValidateSystem(raw json.RawMessage) Problems
  func ValidateDeployment(raw json.RawMessage, system *System) Problems
  ```
- Codes (closed set, const block in problems.go): `PM-OP-UNEXPORTED`, `PM-OPS-EMPTY`, `PM-GOPACKAGE-MISSING`, `PM-FIELD-UNKNOWN`, `PM-LAYER-UNKNOWN`, `PM-DEP-TARGET`, `PM-REL-ENDS`, `PM-DEPLOY-REF`.

- [ ] **Step 1: Write the failing tests** (table-driven, one fixture per code):

```go
func TestValidateContract_UnexportedOpName(t *testing.T)   // fixture unexported-op.json → Problems contains Code PM-OP-UNEXPORTED, Path "…operations[0].name"
func TestValidateContract_ZeroOperations(t *testing.T)     // zero-ops.json (operations: []) and null variant → PM-OPS-EMPTY
func TestValidateContract_MissingGoPackage(t *testing.T)   // missing-gopackage.json with layer engine, no stub → PM-GOPACKAGE-MISSING; a `stub: true` contract → no problem
func TestValidateContract_UnknownField(t *testing.T)       // unknown-field.json → PM-FIELD-UNKNOWN (decode with DisallowUnknownFields)
func TestLoad_ReportsAllProblemsAtOnce(t *testing.T)       // a project.json with three defective contracts → err is Problems with len ≥ 3, not the first only
```

- [ ] **Step 2: Run** `cd framework-go-projectmodel && GOWORK=off go test ./ -run 'TestValidateContract|TestLoad_ReportsAll' -v` — Expected: FAIL (undefined: Problems / ValidateContract).

- [ ] **Step 3: Implement** `problems.go`; make `ParseContract` and `(m *Model) validate()` append to a `Problems` instead of returning on first error; `Load` returns `(m, probs)` with `probs == nil` as `error(nil)` (guard the typed-nil trap: return `error(nil)` explicitly when `len(probs)==0`).

- [ ] **Step 4: Run** the tests — Expected: PASS. Then `GOWORK=off go test ./...` in projectmodel — Expected: PASS (existing single-error tests may need `errors.As(err, &Problems{})`; adjust those assertions, never the behaviour).

- [ ] **Step 5: Commit** `git commit -m "feat(projectmodel): Load reports every problem; exported ValidateContract/System/Deployment with closed problem codes"`

---

### Task 1.3: Generators share the loader (no private re-validation)

Every generator in `framework-go-app-generator` and `framework-go-http-generator`/`mcp-generator` already imports `projectmodel`; some re-check layer spelling or op names privately (FIX-028 class). Make `projectmodel.Load` the only gate.

**Files:**
- Modify: each `cmd/*/main.go` in `framework-go-app-generator` (appgen, modelgen, testgen, webgen) and `framework-go-http-generator/cmd/httpgen`, `framework-go-mcp-generator/cmd/mcpgen` — on `projectmodel.Load` error, print `Problems` one per line and exit 1 before any emission.
- Test: `framework-go-app-generator/cmd/appgen/main_test.go` (new): `TestAppgen_RefusesWithAllProblems` runs the binary via `go run` against `projectmodel/testdata/problems/zero-ops.json`-style project and asserts stderr lists `PM-OPS-EMPTY` and exit code 1.

- [ ] **Step 1: Write the failing test.**
- [ ] **Step 2: Run** `GOWORK=off go test ./cmd/appgen -run TestAppgen_Refuses -v` — Expected: FAIL.
- [ ] **Step 3: Implement** a shared helper `func loadOrExit(path string) *projectmodel.Model` in `framework-go-app-generator/internal/cli/load.go` used by every cmd; `grep -rn "CanonicalLayer\|unexported\|operations) == 0" --include=*.go` and delete any private duplicate check that `Load` now covers (keep anything `Load` does not cover; list what you kept in the commit body).
- [ ] **Step 4: Run** `GOWORK=off go test ./...` in app-generator — Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "refactor(generators): projectmodel.Load is the only loader; print every problem and exit 1"`

---

### Task 1.4: App RA admits every write through Tier 0

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — add `func admit(p *Project) error` and call it from the one function that encodes `*Project` for the git write (locate: the internal caller of `EncodeProjectJSON` inside the CAS write path; name the call site in the commit body).
- Modify: `server/cmd/aiarch-state-mcp/constructverbs.go:71-111` — step 3 (`projectmodel.Load`) formats `Problems` fully; step 4 unchanged.
- Test: `server/internal/resourceaccess/projectstate/admit_test.go` (new), `server/cmd/aiarch-state-mcp/constructverbs_test.go` (extend).

**Interfaces:**
- Produces: `admit(p *Project) error` = `projectmodel.Load(EncodeProjectJSON(p))` + `methodcheck.DecodeProject` + `methodcheck.DeriveScenarios` must all succeed; error text is the `Problems` list or the decode/derive error. Any failure refuses the write with no git mutation.

- [ ] **Step 1: Write the failing tests:**

```go
func TestAdmit_RefusesContractWithoutGoPackage(t *testing.T) // build a Project with one engine contract lacking goPackage; admit(p) error contains "PM-GOPACKAGE-MISSING"
func TestAdmit_RefusesZeroOperations(t *testing.T)
func TestAdmit_PassesOwnProject(t *testing.T)                // loads ../../../../.aiarch/state/project.json; admit == nil
func TestRecordServiceContract_RefusalListsEveryProblem(t *testing.T) // through the MCP verb: a contract with lowercase op AND no goPackage → error mentions both codes
```

- [ ] **Step 2: Run** `cd server && GOWORK=off go test ./internal/resourceaccess/projectstate -run TestAdmit -v` — Expected: FAIL.
- [ ] **Step 3: Implement** `admit` and wire it. The RA may import `projectmodel` and `methodcheck` (it already does elsewhere in the module; confirm with `grep -n methodcheck projectstateaccess.go`).
- [ ] **Step 4: Run** both test packages — Expected: PASS. Then `GOWORK=off go test -p 1 ./internal/resourceaccess/projectstate ./cmd/aiarch-state-mcp` — Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "feat(projectstate): admit every write through projectmodel/methodcheck loadability (Tier 0)"`

---

### Task 1.5: Conformance fixtures (embedded)

**Files:**
- Create: `framework-go-app-generator/testdata/conformance/todomvc/project.json` — copied from `archistrator-bench-scratch/archive/run-20261105T141816Z-04ce59ca/harvest/project.json`, then: strip `activityExecution`, `testingState`, `research` bodies; keep `slots`, `serviceContracts`, `phaseArtifacts.{testPlan,uiDesign}`, `reviewPolicy`, `moduleDir`. Set `id` to `todomvc-conformance`.
- Create: `framework-go-app-generator/testdata/conformance/todomvc/README.md` — provenance (RUN-021, commit b31bc72f, date) and the strip list.
- Create: `framework-go-app-generator/testdata/conformance/greenfield/project.json` — the existing greenfield golden fixture copied (find it under `testdata/`).
- Create: `framework-go-app-generator/conformance/fixtures.go`:

```go
package conformance
//go:embed all:../testdata/conformance   — NOTE: embed cannot climb; instead move the fixtures under conformance/testdata/ and embed "testdata/*"
var fixtureFS embed.FS
func Fixtures() fs.FS                       // sub-FS rooted at testdata
func FixtureNames() []string                // sorted: greenfield, todomvc
func Fixture(name string) ([]byte, error)   // project.json bytes
```
(Resolve the embed note: place fixtures at `framework-go-app-generator/conformance/testdata/<name>/project.json`.)

- Test: `conformance/fixtures_test.go`: `TestFixtures_LoadUnderProjectmodelAndMethodcheck` — every fixture passes `projectmodel.Load`, `methodcheck.DecodeProject`, `methodcheck.ValidateProjectJSON` with zero Error findings.

- [ ] **Step 1: Write the test.** **Step 2: Run** it — Expected: FAIL (no fixtures). **Step 3: Add the fixtures and `fixtures.go`.** If the harvest fails a rule, fix the *fixture data* and record each edit in README.md (the fixture must be admissible as-is by Task 1.4's recorder). **Step 4: Run** — Expected: PASS. **Step 5: Commit** `git commit -m "feat(conformance): embedded todomvc (RUN-021) and greenfield fixtures"`.

---

### Task 1.6: `nullgen` — null implementations and a seeded composition seam

The chain must build and boot with no agent. For every contract, emit a null implementation; for the composition seam, emit a `hooks.go` whose bodies are the composegen seeds plus null constructors.

**Files:**
- Create: `framework-go-app-generator/nullgen/nullgen.go`, `nullgen_test.go`, `cmd/nullgen/main.go`
- Modify: `method-assets/assets/scaffold/Makefile.scenarios.mk.tmpl` — add
  ```
  gen-null:
  	GOWORK=off go tool nullgen -project .aiarch/state/project.json -root .
  ```
  and add `cmd/nullgen` to the scaffolded `go.mod` tool block (`method-assets/assets/scaffold/go.mod.tmpl` or wherever modelgen/testgen are listed).

**Interfaces:**
- Produces:
  ```go
  package nullgen
  type Output struct { Files map[string][]byte } // path → bytes, paths module-relative
  func Emit(m *projectmodel.Model) (Output, error)
  ```
  - For each non-client contract with `goPackage`: `<goPackage>/null.gen.go` declaring `type Null<Iface> struct{}` implementing every operation: result = zero value of the result type, error = `nil` for ops with `error:false`, and for `error:true` ops `nil` too (wiring mode; a typed not-implemented error is Plan 2's null-subject, not this).
  - `cmd/server/hooks.go`: `type appHooks struct{}` with every hook composegen seeds (`ResolveProfile`→"local", `DevConfig` seed verbatim from `composegen/hooks.go:242-244`, `PolicyDecisionPoint` returning a permit-all PDP **only under the local profile** and panicking otherwise, `TokenValidator` nil, `ExtraMounts` no-op), and every `<Comp>Args`/`Finalize<Comp>`/resolver hook returning the null implementation. Header `// Code generated by nullgen for conformance. DO NOT EDIT.`
  - Never overwrite an existing non-generated `hooks.go` (refuse with exit 2) — nullgen is for conformance and fresh scaffolds only.

- [ ] **Step 1: Write the failing tests:**
```go
func TestNullgen_EmitsNullForEveryGoContract(t *testing.T)  // over the todomvc fixture: one null.gen.go per non-client contract; each declares Null<Iface>
func TestNullgen_HooksCoverEverySeam(t *testing.T)          // run composegen over the fixture to a temp dir; `go vet ./cmd/server` passes with nullgen's hooks.go in place (compiles against the generated Hooks interface)
func TestNullgen_RefusesToOverwriteHandwrittenHooks(t *testing.T)
```
- [ ] **Step 2: Run** `GOWORK=off go test ./nullgen -v` — Expected: FAIL.
- [ ] **Step 3: Implement** `Emit` by walking `m.Contracts` and reusing composegen's hook catalogue (`composegen.deriveHooks` output; export a `composegen.HookList(m) []Hook{Name, Signature, Seed}` if it is unexported — add that export in the same commit).
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "feat(nullgen): null implementations and seeded hooks.go so a fresh model builds and boots"`

---

### Task 1.7: The conformance chain, build, boot, probe

**Files:**
- Create: `framework-go-app-generator/conformance/chain.go`, `boot.go`, `conformance_test.go`

**Interfaces:**
- Produces:
  ```go
  type Report struct { Fixture, Dir string; Steps []StepResult }          // StepResult{Name, Argv []string, ExitCode int, Stderr string, Duration time.Duration}
  func RunChain(ctx context.Context, fixture string, dir string) (Report, error)   // scaffold → gen-models → gen (clients/temporal/config/compose) → gen-null → gen-tests → webgen → go build ./... → go vet ./... → npm ci && npm run typecheck (webApp)
  type Booted struct { BaseURL string; Stop func() error }
  func Boot(ctx context.Context, dir string) (Booted, error)               // `go run ./cmd/devstack`; waits for GET /readyz == 200 (timeout 120s)
  func Probe(ctx context.Context, b Booted) error                          // GET / == 200 and body contains "<html"; POST /mcp initialize then tools/list → ≥1 tool
  func DockerLeftovers(ctx context.Context, before, after []string) []string // names in after not in before (docker volume ls + docker ps -a)
  ```
- The chain runs the **scaffolded Makefile targets by name** (`make gen-models`, `make gen`, `make gen-null`, `make gen-tests`, `make gen-ops`…) in the scaffolded module, so the pipeline follows whatever method-assets ships. Scaffolding uses method-assets' seat function (find `scaffold.go:128`'s caller; expose `scaffold.Seat(dir string, m *projectmodel.Model) error` if needed).

- [ ] **Step 1: Write the failing tests** (`//go:build conformance` tag, so unit CI stays fast):
```go
func TestConformance_Chain(t *testing.T)                 // for each FixtureNames(): RunChain succeeds; every StepResult.ExitCode == 0
func TestConformance_Boot(t *testing.T)                  // todomvc: Boot, Probe, Stop — all nil
func TestConformance_BootTwice_NoOrphans(t *testing.T)   // snapshot docker, Boot+Stop twice, DockerLeftovers == empty
```
- [ ] **Step 2: Run** `GOWORK=off go test -tags conformance ./conformance -run TestConformance_Chain -v -timeout 30m` — Expected: FAIL (chain undefined).
- [ ] **Step 3: Implement** `chain.go` (exec with `GOWORK=off`, cwd = scaffold dir, capture stderr), `boot.go` (process group kill on Stop; `docker` CLI for leftovers).
- [ ] **Step 4: Run** all three — Expected: PASS locally with Docker Desktop running. Record wall time in the commit body.
- [ ] **Step 5: Commit** `git commit -m "feat(conformance): command chain, boot, probes and orphan check over embedded fixtures"`

---

### Task 1.8: CI wiring and release gating

**Files:**
- Modify: `.github/workflows/platform-checks.yml` — new job `conformance` (needs: go; ubuntu-latest; services: none, Docker is on the runner; steps: setup-go, setup-node 22, `GOWORK=off go test -tags conformance ./conformance -v -timeout 40m` in `framework-go-app-generator`).
- Modify: the release/tag workflow(s) in the platform repo (find the one that tags modules) — require the `conformance` job to have passed on the same SHA before tagging `framework-go-app-generator`, `framework-go`, `method-assets`.
- Modify: `method-assets/assets/workflows/go-checks.yml.tmpl` — no change (apps run their own gen-*-check); note in the commit body why.

- [ ] **Step 1: Add the job**; push the branch; **Step 2: Verify** the `conformance` job runs green on the PR (`gh run watch`). **Step 3: Commit** `git commit -m "ci(platform): conformance job gates releases"`.

---

### Task 1.9: App re-records the fixture through its own verbs

**Files:**
- Create: `server/cmd/aiarch-state-mcp/conformance_test.go`

- [ ] **Step 1: Write the test:**
```go
func TestConformanceFixture_RecordsThroughVerbs(t *testing.T)
// for each conformance.FixtureNames(): start a fresh local project (the existing test helper that births a project in a temp git repo — reuse the one used by constructverbs_test.go),
// then for each serviceContracts entry call recordServiceContract(key, raw); for each phaseArtifacts.testPlan[c] call recordPhaseArtifact(c, payload); for each uiDesign call recordPhaseArtifact.
// Assert: no verb returns an error; the resulting project passes admit().
```
- [ ] **Step 2: Run** `GOWORK=off go test ./cmd/aiarch-state-mcp -run TestConformanceFixture -v` — Expected: FAIL until pins are bumped (Step 3) and any fixture/recorder mismatch is fixed on the fixture side (README note) or, if the recorder is wrong, in the recorder.
- [ ] **Step 3: Release + pin**: tag `framework-go-projectmodel`, `framework-go-app-generator`, `method-assets` (next minor each; GC-6), bump `server/go.mod` lines 9/11/13, `GOWORK=off go mod tidy`.
- [ ] **Step 4: Run** the test — Expected: PASS. Then full app suite once: `cd server && GOWORK=off go test -p 1 -timeout 90m ./...` — Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "test(aiarch-state-mcp): conformance fixtures record through the real verbs; pins bumped"` and push the app branch.

---

## Done when

- Platform CI job `conformance` is green on main after the release tags.
- App main (via this branch) pins the new releases, `make test` green, and `TestConformanceFixture_RecordsThroughVerbs` passes.
- Replaying the FIX-028/041/048 defect shapes against `projectmodel.Load` reports all codes at once (Task 1.2 tests are the record).
