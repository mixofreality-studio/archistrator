# Layer-Package File Layout Standard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enforce the ratified file-layout standard (spec: `docs/superpowers/specs/2026-07-11-layer-file-layout-standard-design.md`) — one contract-impl file, one file per workflow, one test file, no other handwritten files, no handwritten Temporal activities — across every layer package, with a `framework-go/arch` FileLayout gate landing last.

**Architecture:** Three workstreams. (1) Platform: a new pure-core `CheckFileLayout` in `framework-go/arch` + removal of the `CustomActivities` escape hatch from temporalgen. (2) App: absorb all 29 custom activities by promoting their capability ports to contract-backed RA components in `project.json` so the existing generator emits them (clean cut — **drain in-flight Temporal workflows before deploying**; activity names/payloads change, ratified 2026-07-11). (3) App: mechanical file folds per package, then flip the gate.

**Tech Stack:** Go, `golang.org/x/tools/go/packages`, go/ast, Temporal Go SDK, temporalgen/modelgen/contractfold codegen pipeline, git-as-DB `project.json`.

## Global Constraints

- Two repos: app `/Users/davidmarne/mixofrealitystudio/archistrator` (`server/` module), platform `/Users/davidmarne/mixofrealitystudio/archistrator-platform`.
- App builds/tests ALWAYS with `GOWORK=off` against **published** platform tags (server/Makefile discipline). Local platform-change testing happens inside the platform repo (its own `go.work`).
- Platform releases are annotated per-module tags: `framework-go/vX.Y.Z`, `framework-go-app-generator/vX.Y.Z`. Pin points: `archistrator/server/go.mod` (framework-go v0.4.5 at line 113, framework-go-app-generator v0.5.1) and `const FrameworkGoVersion = "v0.4.4"` in `server/internal/resourceaccess/sourcecontrol/agenticdesign.go:126`.
- Never weaken existing gates (arch/lint/encapsulation/methoddesign). No waivers in the final FileLayout gate.
- Contract op-count ceilings (App-C §6.2c): target 3–5, max 12, reject ≥20. Every new RA component below stays ≤12.
- Registered Temporal WORKFLOW names must not change (external starters reference them): `billingOnboardPayment`, `billingRegisterCustomer`, `billingCloseCycle`, `billingShortfallSweep`, `constructionPumpNextActivity`, `constructionConstructActivity`, `constructionReplanSweep`, `constructionProjectSupervision`, `projectDesignCoAuthor`, `projectDesignSDPReview`, `projectDesignPhaseAdvance`, `systemDesignPhase`, `systemDesignCoAuthor`, `systemDesignPhaseAdvance`, `operationsDeploy`, `operationsReconcile`, `operationsWithdraw`, `operationsCostProjection`, `operationsOperatedSystemView`, `operationsDelinquencyEnforcement`. Activity names DO change (clean cut, ratified).
- Every fold/move commit is behavior-free: `GOWORK=off go build ./... && GOWORK=off go test ./...` (run in `server/`) must pass identically before and after.
- Commit trailers: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` (plus session link per harness rules).

## Standard reference (what the gate enforces)

Per leaf package under `server/internal/{manager,engine,resourceaccess,client}/...`, handwritten `.go` files are limited to:

| # | File | Notes |
|---|---|---|
| 1 | `<leaf><stereotype>.go` — `billingmanager.go`, `estimationengine.go`, `projectstateaccess.go`, `systemdesignclient.go` | ALL contract methods + all shared/non-workflow code |
| 2 | `tolower(WorkflowFuncName - "Workflow").go` — `deploy.go`, `pumpnextactivity.go` | Manager layer only; exactly one `workflow.Context` func per file |
| 3 | `<stereotype>_test.go` — `manager_test.go`, `engine_test.go`, `access_test.go`, `client_test.go` | only test file |

Exempt: `*.gen.go`, all non-`.go` files. Forbidden anywhere handwritten: `RegisterActivity*` calls, `workflow.Context` funcs outside manager layer.

---

## Phase A — Platform: FileLayout check + temporalgen escape-hatch removal

### Task A1: FileLayout pure core in framework-go/arch

**Files:**
- Create: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go/arch/filelayout.go`
- Create: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go/arch/filelayout_test.go`
- Create fixture packages under: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go/arch/testdata/layoutapp/` (new nested module, mirroring `testdata/gensurfaceapp/` — own `go.mod` `module example.com/layoutapp`, plus `go.work` exclusion is unnecessary: tests set `GOWORK=off`)
- Modify: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go/arch/arch.go` (add `FileStereotype` to `Layer`, set defaults in `MethodSpec`)

**Interfaces:**
- Produces: `Layer.FileStereotype string` field; pure core `func fileLayoutViolations(pkgs []*packages.Package, spec Spec) []fileLayoutViolation` with `type fileLayoutViolation struct { Pkg, File, Rule, Detail string }`; consumed by Task A2's wrapper.
- Consumes: existing `Layer`/`Spec`/`makeLayerIndex` from `arch.go`, load pattern from `gensurface.go` (`NeedName|NeedFiles|NeedCompiledGoFiles|NeedSyntax|NeedTypes|NeedTypesInfo`).

- [ ] **Step 1: Add `FileStereotype` to `Layer` and default it in `MethodSpec`**

In `arch.go`, extend the struct (after `IfaceSuffix`):

```go
// FileStereotype is the layer's file-layout stereotype ("manager", "engine",
// "access", "client"). When non-empty, CheckFileLayout enforces the closed
// hand-file set on every leaf package of this layer: <leaf><stereotype>.go,
// per-workflow files (Manager layer only), <stereotype>_test.go, *.gen.go.
// Empty disables file-layout enforcement for the layer.
FileStereotype string
```

In `MethodSpec` set: Client → `"client"`, Manager → `"manager"`, Engine → `"engine"`, ResourceAccess → `"access"`, Utility → `""` (utilities stay freeform — the app has no internal utility packages; revisit only if one appears).

- [ ] **Step 2: Write the failing tests (fixture-observation pattern, like `gensurface_test.go`)**

Fixture `testdata/layoutapp/internal/`:
- `manager/goodmgr/`: `goodmgrmanager.go` (any type + method), `deploy.go` (one `func (w *wfs) DeployWorkflow(ctx workflow.Context) error`)… fixture modules must not depend on the real Temporal SDK; mirror gensurfaceapp's approach — declare a local `package workflow` stub inside the fixture module (`internal/workflow/workflow.go` with `type Context interface{}`) and import that. The check matches the parameter type by NAME `workflow.Context` (selector expr `workflow.Context`), not by import path, precisely so fixtures and consumers both work. Also `manager_test.go`, `worker.gen.go`.
- `manager/badmgr/`: `helpers.go` (violation: not in allowed set), `workflow.go` declaring TWO workflow funcs (violation: multi-workflow file + name mismatch), `badmgr_test.go` (violation: wrong test-file name), `regcall.go` containing a `w.RegisterActivityWithOptions(nil, opts)` call (violation: hand registration).
- `engine/goodeng/`: `goodengengine.go`, `engine_test.go`.
- `engine/badeng/`: `badengengine.go` + `pure.go` with `func Run(ctx workflow.Context)` (violation: workflow func outside manager layer).

Test cases in `filelayout_test.go` (pure-core observation via a `hasLayoutViolation(vs, file, rule)` helper):

```go
func TestFileLayoutViolations(t *testing.T) {
	pkgs := loadLayoutPkgs(t) // GOWORK=off, packages.Load from testdata/layoutapp
	spec := layoutSpec()      // MethodSpec-shaped, ModulePrefix "example.com/layoutapp/internal/"
	vs := fileLayoutViolations(pkgs, spec)
	for _, want := range []struct{ file, rule string }{
		{"helpers.go", "file-not-allowed"},
		{"workflow.go", "workflow-file-multiple-funcs"},
		{"workflow.go", "workflow-file-name"},
		{"badmgr_test.go", "test-file-name"},
		{"regcall.go", "hand-activity-registration"},
		{"pure.go", "workflow-func-outside-manager"},
	} {
		if !hasLayoutViolation(vs, want.file, want.rule) {
			t.Errorf("missing violation %s in %s", want.rule, want.file)
		}
	}
	for _, v := range vs {
		if strings.Contains(v.Pkg, "goodmgr") || strings.Contains(v.Pkg, "goodeng") {
			t.Errorf("clean package flagged: %+v", v)
		}
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go && go test ./arch/ -run TestFileLayoutViolations -v`
Expected: FAIL — `fileLayoutViolations` undefined.

- [ ] **Step 4: Implement `filelayout.go`**

Core shape (mirror `gensurface.go` file iteration):

```go
package arch

type fileLayoutViolation struct{ Pkg, File, Rule, Detail string }

func fileLayoutViolations(pkgs []*packages.Package, spec Spec) []fileLayoutViolation {
	idx := makeLayerIndex(spec)
	var out []fileLayoutViolation
	for _, p := range pkgs {
		layer, ok := layerFor(idx, spec, p.PkgPath) // reuse classification from checkPackage
		if !ok || layer.FileStereotype == "" {
			continue
		}
		leaf := path.Base(p.PkgPath)
		implFile := leaf + layer.FileStereotype + ".go"
		testFile := layer.FileStereotype + "_test.go"
		for i, fpath := range p.CompiledGoFiles {
			base := filepath.Base(fpath)
			if strings.HasSuffix(base, ".gen.go") {
				continue
			}
			f := p.Syntax[i]
			wfFuncs := workflowFuncs(f) // funcs with a workflow.Context param (selector-typed)
			// hand-registration ban applies to EVERY handwritten file, any layer:
			for _, call := range registerActivityCalls(f) {
				out = append(out, fileLayoutViolation{p.PkgPath, base, "hand-activity-registration", call})
			}
			switch {
			case strings.HasSuffix(base, "_test.go"):
				if base != testFile {
					out = append(out, fileLayoutViolation{p.PkgPath, base, "test-file-name", "want " + testFile})
				}
			case base == implFile:
				if len(wfFuncs) > 0 {
					out = append(out, fileLayoutViolation{p.PkgPath, base, "workflow-in-impl-file", wfFuncs[0]})
				}
			case len(wfFuncs) > 0:
				if layer.Name != spec.TemporalLayer {
					out = append(out, fileLayoutViolation{p.PkgPath, base, "workflow-func-outside-manager", wfFuncs[0]})
					continue
				}
				if len(wfFuncs) > 1 {
					out = append(out, fileLayoutViolation{p.PkgPath, base, "workflow-file-multiple-funcs", strings.Join(wfFuncs, ",")})
				}
				want := strings.ToLower(strings.TrimSuffix(wfFuncs[0], "Workflow")) + ".go"
				if base != want {
					out = append(out, fileLayoutViolation{p.PkgPath, base, "workflow-file-name", "want " + want})
				}
			default:
				out = append(out, fileLayoutViolation{p.PkgPath, base, "file-not-allowed",
					"handwritten files are limited to " + implFile + ", per-workflow files, " + testFile})
			}
		}
	}
	return out
}
```

Helpers (same file): `workflowFuncs(f *ast.File) []string` — every `*ast.FuncDecl` any of whose params has type `*ast.SelectorExpr` with `X` ident `workflow` and `Sel` `Context`; `registerActivityCalls(f *ast.File) []string` — every `*ast.CallExpr` whose fun is a selector named `RegisterActivity` or `RegisterActivityWithOptions` (mirror the AST matching in the app's `arch_activitynames_test.go`, which this platform check subsumes for hand files). `layerFor` — extract the existing per-package layer classification out of `checkPackage` into a shared helper rather than duplicating prefix matching.

Note `Tests:false` in `packages.Load` excludes `_test.go` files — `CheckFileLayout` must load with `Tests: true` (or glob the package dir with `os.ReadDir` for `_test.go` names; prefer `os.ReadDir` on `filepath.Dir(p.CompiledGoFiles[0])` — simpler, no double-load, and external `_test` packages come along free). Implement test-file-name checking via `os.ReadDir`; document the choice in the file header.

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./arch/ -run TestFileLayoutViolations -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
git add framework-go/arch && git commit -m "arch: FileLayout pure core — closed hand-file set per layer package"
```

### Task A2: `CheckFileLayout` wrapper + methodcheck default-on

**Files:**
- Modify: `framework-go/arch/filelayout.go` (add wrapper)
- Modify: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go/methodcheck/check.go:145-168` (`runLayerAndAlignmentChecks`)
- Test: `framework-go/arch/filelayout_test.go`, `framework-go/methodcheck/` existing test suite

**Interfaces:**
- Produces: `func CheckFileLayout(t *testing.T, spec Spec)` — public, sibling to `Check`/`CheckGeneratedSurface`; runs pure core, `t.Errorf` per violation.
- Consumed by: app `arch_test.go` (Task D1) and `methodcheck.Check` (default-on — generated consumer repos inherit on their next framework-go pin bump; existing repos are insulated by their seated go.mod pin, which scaffold sync deliberately never rewrites).

- [ ] **Step 1: Failing test** — add `TestCheckFileLayoutPassesClean(t)` running the wrapper against only the clean fixture packages (subdirectory pattern load), expecting no errors; run, expect FAIL (undefined).
- [ ] **Step 2: Implement wrapper** (load packages exactly as the pure-core test does, from `spec.ModuleRoot`/`spec.Patterns`), wire one call into `runLayerAndAlignmentChecks` next to the `arch.CheckGeneratedSurface` call at `methodcheck/check.go:155`, unconditional (default-on).
- [ ] **Step 3: Run the FULL platform suite** — `cd /Users/davidmarne/mixofrealitystudio/archistrator-platform && go test ./...`. Expected: PASS. If methodcheck's own fixtures violate the layout, fix the FIXTURES to comply (they are the reference consumers) — do not special-case the check.
- [ ] **Step 4: Commit** — `git commit -m "arch+methodcheck: CheckFileLayout entry point, default-on in methodcheck"`.

### Task A3: temporalgen — delete the `CustomActivities` escape hatch

**Files:**
- Modify: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go-app-generator/temporalgen/worker.go` (emitted `genWorkerManifest` struct lines 15-36; `RegisterWorker` loop lines 88-107)
- Modify goldens: `framework-go-app-generator/temporalgen/testdata/greenfield.worker.gen.go.golden`, sample `framework-go-app-generator/internal/sample/order/worker.gen.go`
- Test: `framework-go-app-generator/temporalgen/temporalgen_test.go`

- [ ] **Step 1: Failing test** — add `TestNoCustomActivitiesSurface` asserting the emitted `worker.gen.go` bytes contain neither `CustomActivities` nor `genRegisteredActivity`. Run `go test ./temporalgen/ -run TestNoCustomActivitiesSurface`; expected FAIL.
- [ ] **Step 2: Remove** the `CustomActivities []genRegisteredActivity` field, the `genRegisteredActivity` type, and the registration loop from the emitted template in `worker.go`.
- [ ] **Step 3: Regenerate goldens + sample** — `go test ./temporalgen/ -update` (per `checkGolden`), then `go test ./...` in `framework-go-app-generator`. Expected: PASS including `TestSampleInSync` compile-proof.
- [ ] **Step 4: Commit** — `git commit -m "temporalgen: remove CustomActivities escape hatch — all activities are generated (rule 5)"`.

### Task A4: Platform release tags

- [ ] **Step 1:** Tag and push (versions relative to current `framework-go/v0.4.5`, `framework-go-app-generator/v0.5.1`):

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
git tag -a framework-go/v0.5.0 -m "arch: CheckFileLayout closed hand-file set; methodcheck default-on"
git tag -a framework-go-app-generator/v0.6.0 -m "temporalgen: CustomActivities escape hatch removed"
git push origin main framework-go/v0.5.0 framework-go-app-generator/v0.6.0
```

**Do NOT bump the app's pins yet** — framework-go pin bumps at Task D1 (flip), app-generator pin bumps at Task B7 (after workflow rewires).

---

## Phase B — App: absorb the 29 custom activities (clean cut)

**Cutover doctrine (ratified 2026-07-11):** activity names and payload shapes change; in-flight Temporal workflow executions will not survive the deploy. Deployment of the release built from this phase requires draining (completing or terminating) in-flight workflows first; Schedules (`closeBillingCycle:<customerId>`, hourly `shortfallSweep`) resume on new code automatically. Task D2 records this in release notes.

### Task B1: Multi-component-per-package contract tooling

New RA components must share existing Go packages (their types and impls live there; RA→RA imports are banned). Today `modelgen` emits exactly one `contract.gen.go` per goPackage and each `.serviceContracts` entry carries one interface doc.

**Files:**
- Recon then modify: `/Users/davidmarne/mixofrealitystudio/archistrator/server/cmd/modelgen/main.go` + its lib `server/cmd/internal/codegen` (read first — the output map is already keyed by goPackage; the needed change is MERGING multiple contract entries that share a goPackage into the one emitted `contract.gen.go`, deterministic order by contract key)
- Modify: `/Users/davidmarne/mixofrealitystudio/archistrator/server/cmd/contractfold/main.go` — teach `foldAll` a `contract.<key>.schema.json` sibling convention for secondary components sharing a directory (primary keeps `contract.schema.json`)
- Test: whatever test files sit beside those cmds today (read first; if none, add `server/cmd/internal/codegen/multicomponent_test.go` with a two-components-one-package fixture model)

- [ ] **Step 1:** Read `server/cmd/internal/codegen` and confirm merge semantics; write a failing test: two `.serviceContracts` entries with the same `goPackage` produce ONE `contract.gen.go` containing both interfaces, no duplicate `$defs`-derived types.
- [ ] **Step 2:** Implement the merge (and the contractfold filename convention). Run `GOWORK=off go test ./cmd/...` in `server/`. Expected: PASS.
- [ ] **Step 3:** `make gen-models gen-models-check` (in `server/`) — Expected: no drift (proves no behavior change for existing single-component packages).
- [ ] **Step 4:** Commit — `refactor(codegen): support multiple contract components per Go package`.

### Task B2: Promote `constructionTransitionAccess` to a contract component

**Files:**
- Modify: `.aiarch/state/project.json` → new `.serviceContracts.constructionTransitionAccess` entry (layer ResourceAccess, goPackage `internal/resourceaccess/projectstate`, component key `constructionTransitionAccess`) — author via `server/internal/resourceaccess/projectstate/contract.constructionTransitionAccess.schema.json` + `go run ./cmd/contractfold --key constructionTransitionAccess --dir internal/resourceaccess/projectstate` (exact flag names: read contractfold's usage block first, lines 20-30)
- The interface ALREADY exists with final signatures: `server/internal/resourceaccess/projectstate/construction_transition_port.go:15-26` (10 ops, ≤12 ✓). The contract doc must reproduce those signatures exactly; `idempotencyKey` params use `{"x-go-type": "fwra.IdempotencyKey", "x-go-import": "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"}` so temporalgen auto-injects the run-scoped key (deps.go `isKeyParam` mechanism); `cred RepoCredential` and enums use `$ref` into `$defs` (types already exist in the projectstate `$defs` universe — check `.serviceContracts.projectStateAccess.$defs` and reuse/copy nodes as contractfold requires self-contained docs)

Schema fragment shape (first op; remaining nine follow the same pattern from the port file):

```json
{
  "$id": "archistrator://contract/projectstate/constructiontransition",
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "constructionTransitionAccess contract",
  "interface": {
    "name": "ConstructionTransitionAccess",
    "layer": "resourceaccess",
    "operations": [
      {
        "name": "RecordChangeReviewed",
        "params": [
          {"name": "projectID", "schema": {"$ref": "#/$defs/ProjectID"}},
          {"name": "expectedVersion", "schema": {"$ref": "#/$defs/Version"}},
          {"name": "activityID", "schema": {"type": "string"}},
          {"name": "cred", "schema": {"$ref": "#/$defs/RepoCredential"}},
          {"name": "idempotencyKey", "schema": {"x-go-type": "fwra.IdempotencyKey", "x-go-import": "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"}}
        ],
        "result": {"$ref": "#/$defs/Version"},
        "error": true
      }
    ]
  },
  "$defs": {}
}
```

- [ ] **Step 1:** Author the schema doc (all 10 ops from `construction_transition_port.go`), fold it, run `make gen-models`. Expected: `contract.gen.go` gains the `ConstructionTransitionAccess` interface; the handwritten declaration in `construction_transition_port.go` now DUPLICATES it — delete the handwritten interface, keep the `var _ ConstructionTransitionAccess = (*GitStore)(nil)` assertion (move it into the impl file during Phase C fold; for now leave the file).
- [ ] **Step 2:** `GOWORK=off go build ./... && GOWORK=off go test ./internal/resourceaccess/projectstate/` — Expected: PASS.
- [ ] **Step 3:** Commit — `contracts: promote constructionTransitionAccess to contract-backed component`.

### Task B3: Promote `gitActivityStatusAccess` (6 ops)

Same procedure as B2. Source of truth for signatures: the port interface behind `wf.GitStatus` — find it with `grep -rn "RecordActivityBranchOpened" server/internal/resourceaccess/projectstate/` (it lives in `gitactivitystatus.go`). Ops: `RecordActivityBranchOpened`, `RecordActivityCIObserved`, `RecordActivityArchApproved`, `RecordActivityMerged`, `RecordActivityStarted`, `RecordActivityCompleted`. Schema file: `contract.gitActivityStatusAccess.schema.json`, same `$defs` handling, same key/cred conventions.

- [ ] Author + fold + `make gen-models` + delete duplicated handwritten interface + build/test + commit (same steps as B2).

### Task B4: Promote `designSessionAccess` (8 ops) + move the envelope codec down

The branch/ledger/provenance/reconcile capability extensions collapse into ONE contract component whose IMPL owns the capability-fallback chains that today live inside the custom activities (primary port, else base `ProjectStateAccess` op, else `fwra.NotFound`/NonRetryable — copy each chain verbatim from `manager/systemdesign/activities_custom.go:81-247`, which is the superset).

Ops (8 ≤ 12 ✓), signatures derived from the manager activity bodies:
1. `ReadProjectOnBranch(projectID, branch string) (ProjectEnvelope, error)` — subsumes both `ReadProjectActivity` (branch="") and `ReadProjectOnBranchActivity`
2. `StageArtifactForReviewOnBranch(projectID, expectedVersion, kind, payload, branch, idempotencyKey) (Version, error)`
3. `CommitArtifactWithProvenance(projectID, expectedVersion, kind, branch, provenance, idempotencyKey) (Version, error)`
4. `RejectArtifactOnBranchWithComments(projectID, expectedVersion, kind, branch, comments, idempotencyKey) (Version, error)`
5. `WithdrawArtifactOnBranch(projectID, expectedVersion, kind, branch, idempotencyKey) (Version, error)`
6. `ReconcileBranchFromMain(projectID, expectedVersion, branch, idempotencyKey) (Version, error)`
7. `SetReviewCommentStatusOnBranch(...)` and 8. `SeedReviewCommentsOnBranch(...)` — signatures from `manager/systemdesign/reviewledger.go`

**Envelope codec moves down:** `projectEnvelope`/`modelEnvelope` + `encodeProject` currently duplicated in `manager/{construction,projectdesign,systemdesign}/codec.go` become ONE projectstate-owned wire type `projectstate.ProjectEnvelope` with `EncodeProject(Project) ProjectEnvelope` / `(ProjectEnvelope) Decode() (Project, error)` — read the three codec.go files first; they are near-identical copies (diff them; reconcile any drift deliberately and note it in the commit). `ReadProjectOnBranch` RETURNS the envelope so the Temporal payload stays a concrete projection.

- [ ] **Step 1:** Move codec: create the projectstate-owned type + tests (port the existing codec tests from the manager packages), leave temporary type aliases in each manager (`type projectEnvelope = projectstate.ProjectEnvelope`) so this commit stays compile-green. Run full `GOWORK=off go test ./...`. Commit.
- [ ] **Step 2:** Author `contract.designSessionAccess.schema.json` (envelope + comment/provenance types enter `$defs` or bind via `x-go-type`), fold, `make gen-models`.
- [ ] **Step 3:** Implement `DesignSessionAccess` on the projectstate store: each method is the capability-fallback chain copied from the systemdesign custom activities (minus Temporal/`mapErr` — error mapping stays in the generated activity layer via `fwmanager.MapError`). Unit-test the fallback chains in the projectstate package (fake store exposing only the base interface → verify fallback path taken; full store → primary path).
- [ ] **Step 4:** Build/test, commit — `contracts: designSessionAccess — branch/ledger/provenance/reconcile capability chains move into RA`.

### Task B5: Promote `revenueLedgerAccess` (3 ops) + `sourceControl.syncManagedScaffold`

- Revenue: ops `RecordInboundRevenue`, `RecordReversal`, `ReadRange` (from `manager/billing/activities_custom.go`, seam types `revenueEntrySeam`→`$defs` types). goPackage: `internal/resourceaccess/billingstate` (it currently has ONLY `contract.gen.go` — the noop seam impl moves here from the manager as the contract impl). Component key: `revenueLedgerAccess`.
- Scaffold: `SyncManagedScaffold` is a composition helper (`sourcecontrol.SyncManagedScaffold` free function reaching the published rail) — promote to an op on the EXISTING `sourceControlAccess` contract (check its current op count in `.serviceContracts` first; if the +1 breaches 12, instead make it a one-op `scaffoldSyncAccess` component on the sourcecontrol package). The manager-side `railCredEnvelope.toRail()` translation moves into the op's params (cred fields become explicit contract params).
- [ ] Author + fold + gen-models + move impls + build/test + commit (one commit per component).

### Task B6: Thread new deps + regenerate the Temporal layer

- [ ] **Step 1:** Add `Deps` entries to the five manager `.serviceContracts` entries in `project.json`: construction gains `constructionTransition` (component `constructionTransitionAccess`), `gitStatus` (`gitActivityStatusAccess`), `designSession` (`designSessionAccess`); projectdesign + systemdesign gain `designSession` (+ scaffold access dep per B5's outcome); billing gains `revenueLedger` (`revenueLedgerAccess`). Match the existing Dep JSON shape (see how `projectStateAccess` deps are declared on the construction manager entry).
- [ ] **Step 2:** `make gen-temporal` (still on app-generator v0.5.1 — `CustomActivities` field still exists and stays temporarily). Expected: `activities.gen.go`/`invokers.gen.go`/`worker.gen.go` in each manager gain the new activities (`constructionTransitionAccess.recordChangeReviewed`, `designSessionAccess.readProjectOnBranch`, …). Build must stay green — nothing calls them yet.
- [ ] **Step 3:** Run the methoddesign gate now, not at the end: `GOWORK=off go test -tags methoddesign ./internal/ -run TestMethodDesignArtifacts`. If ALIGN/contract rules fire on the new components (components in `.serviceContracts` without `.systemDesign` counterparts), add the components to `.systemDesign` through the aiarch-state MCP tools (the sanctioned writer — NOT hand-edits to the systemDesign slot), mirroring how existing RA components are modeled. This is a design amendment; keep it a separate commit with the gate output quoted in the message.
- [ ] **Step 4:** Commit — `contracts: thread new RA deps through manager contracts; regen temporal layer`.

### Task B7 (billing) / B8 (construction) / B9 (projectdesign) / B10 (systemdesign): rewire workflows, delete custom activities

Uniform procedure per manager (operations has none). Using construction (B8) as the worked example; B7/B9/B10 differ only in the tables from the inventory:

- [ ] **Step 1:** In each workflow body, replace every `workflow.ExecuteActivity(ctx, wf.XxxActivity, args)` (or wrapped equivalent) with the generated invoker call `wf.Acts.ConstructionTransitionRecordChangeReviewed(ctx, projectID, expectedVersion, activityID, cred)` — positional params, arg-bundle structs dissolve at call sites, cred envelopes pass through the contract's `RepoCredential` param (the `railCredEnvelope.toProjectState()` translation moved down in B4/B5). `ReadProjectActivity`/`ReadProjectOnBranchActivity` call sites both become `wf.Acts.DesignSessionReadProjectOnBranch(ctx, projectID, branch)` (branch `""` on main). Check the generated invoker method names in `invokers.gen.go` before writing calls — the prefix is the Dep FIELD name, not the component key.
- [ ] **Step 2:** Empty the `CustomActivities:` slice in the manager's `workermanifest.go`; delete `activities_custom.go` (+ `gitactivities.go` in construction; the 3 activities in `gitrail.go`/`reviewledger.go` for projectdesign/systemdesign — delete the activity funcs, KEEP the non-activity value carriers/PR-text builders in place for Phase C folding). Delete now-unused arg structs and `activityIdempotencyKey`.
- [ ] **Step 3:** `GOWORK=off go test ./internal/manager/<pkg>/` — workflow replay tests (`workflow_test.go`) will fail where they mock the OLD activity names; update mocks to the new generated names/signatures. This is the one intended behavior change: quote before/after activity names in the commit message.
- [ ] **Step 4:** Full `GOWORK=off go test ./...`. Commit per manager — `refactor(<pkg>): custom activities → generated (clean cut)`.

### Task B11: Registered-names golden + app-generator pin bump

- [ ] **Step 1:** New test `server/internal/registered_names_test.go` (root `internal_test` package, beside `arch_activitynames_test.go`): calls each manager's `WorkerManifest()`, collects all registered workflow names and activity names, compares against a committed golden literal (sorted). Purpose: any future rename is a deliberate diff, replacing the spec's original (unimplementable) name-freeze. Write the test, generate the golden from current output, verify the 20 workflow names in Global Constraints appear verbatim.
- [ ] **Step 2:** Bump `framework-go-app-generator` to `v0.6.0` in `server/go.mod`, `go mod tidy`, `make gen-temporal` — the `CustomActivities` field disappears from `worker.gen.go`; remove the now-dangling empty `CustomActivities:` composite entries in the five `workermanifest.go` files.
- [ ] **Step 3:** `make gen-temporal-check && GOWORK=off go test ./...` — Expected: PASS. Commit.

---

## Phase C — App: mechanical folds (behavior-free, one commit per package/group)

**Fold procedure** (identical everywhere; "fold A,B,C into T"):
1. `cat B.go C.go >> T.go` keeping T's package clause/imports first; delete the duplicated `package`/`import` blocks from the appended text; run `goimports -w T.go` to merge imports; `gofmt -l .` must be clean.
2. `git rm` the source files in the same commit (history: one rename per package may use `git mv` for the largest source file so `git log --follow` tracks it — pick the biggest file as T's seed).
3. Verify: `GOWORK=off go build ./... && GOWORK=off go test ./...` — identical results to pre-fold (no test additions/removals; `go test -count=1 ./internal/<pkg>/ -v | grep -c "^=== RUN"` must match before/after).

### Task C1: Engines (7 packages)

| Package | Fold into `<pkg>engine.go` (seed = largest) | Tests → `engine_test.go` |
|---|---|---|
| autoscaler | autoscaler.go(seed), behavior.go, strategy.go | autoscaler_test.go |
| billing | billing.go | billing_test.go |
| estimation | estimation.go, earnedvalue.go, network.go(seed), resourceschedule.go | earnedvalue_test.go, estimation_test.go, network_test.go |
| handoff | handoff.go(seed), behavior.go, strategy.go | handoff_test.go |
| intervention | intervention.go(seed), strategy.go | intervention_test.go |
| operationestimation | operationestimation.go | operationestimation_test.go |
| review | review.go | review_test.go |

- [ ] One commit per engine package: `git mv <seed>.go <pkg>engine.go`, fold, merge tests (`git mv` the single/largest test to `engine_test.go`, append others), verify, commit.

### Task C2: ResourceAccess — small packages (8)

Fold into `<pkg>access.go` / `access_test.go`: artifact (→`artifactaccess.go`; files: artifact.go, construction.go, gitstore.go(seed), helpers.go, variant.go; test gitstore_test.go→access_test.go), constructionpipeline (actions.go(seed), actions_http_client.go, behavior.go, constructionpipeline.go, variant.go), durableexecution (temporal.go(seed), behavior.go, durableexecution.go, registry.go; tests durableexecution_test.go + temporal_integration_test.go → access_test.go — NO build tags exist on these, confirmed; if the integration test needs gating it already self-gates at runtime), operatedruntime, operatedsystemstate (postgres.go → operatedsystemstateaccess.go; schema.sql stays), sourcecontrol (github.go(seed), agenticdesign.go, behavior.go, sourcecontrol.go, variant.go; assets/ stays), usage (usage.go, postgres.go(seed)). billingstate + merchantgateway: whatever hand impl files exist after B5 fold likewise (billingstate gains the revenue seam impl → `billingstateaccess.go`).

- [ ] One commit per package, same procedure/verification as C1.

### Task C3: ResourceAccess — projectstate mega-fold

~40 hand files + ~30 test files → `projectstateaccess.go` + `access_test.go`. Seed: `gitstore.go` (1222 lines).

- [ ] **Step 1:** Fold in dependency-stable order (order within a Go file is semantically irrelevant; choose readable grouping): store/adapter cluster (gitstore, gitadapter, gitconstruction, construction, reconcile, provenance, registry, identity, credential), typed-model cluster (models_phase1, models_phase2, system, usecase, servicecontract, phaseartifacts, operatingmodel, artifactmodel, estimation, research, toolpalette, modelfields, slotcodec, enumjson), status/progress cluster (activityconstructionstatus, activityprofile, constructionprogress, gitactivity, gitactivityconstruction, gitactivitystatus, stalecause, corpusderive, commandfor, reviewthread, reviewpolicy, construction_transition_port residue), then projectstate.go.
- [ ] **Step 2:** Merge all `_test.go` into `access_test.go` (same clustering). Duplicate test-helper names across files WILL collide (`newFakeX`, `mustY`) — resolve by deleting exact duplicates, renaming true variants with a cluster suffix. Count `=== RUN` before/after; must match.
- [ ] **Step 3:** Verify + commit. This is the largest single commit of the migration; commit message lists every source filename for archaeology.

### Task C4: Clients

- [ ] Only handwritten file under client/: `client/mcp/systemdesign/f26_output_schema_test.go` → rename `git mv` to `client_test.go`. Verify + commit.

### Task C5–C9: Managers (operations, billing, construction, projectdesign, systemdesign — in that order, simplest first)

Per manager: (a) split `workflow.go` into per-workflow files; (b) fold everything else into `<pkg>manager.go`; (c) merge tests into `manager_test.go`.

Workflow file map (function → file), from the registration inventory:

- **operations**: DeployWorkflow→`deploy.go`, ReconcileWorkflow→`reconcile.go`, WithdrawWorkflow→`withdraw.go`, CostProjectionWorkflow→`costprojection.go`, ViewWorkflow→`view.go`, DelinquencyEnforcementWorkflow→`delinquencyenforcement.go`
- **billing**: OnboardWorkflow→`onboard.go`, RegisterCustomerWorkflow→`registercustomer.go`, CloseCycleWorkflow→`closecycle.go`, ShortfallSweepWorkflow→`shortfallsweep.go`
- **construction**: PumpNextActivityWorkflow→`pumpnextactivity.go`, ConstructActivityWorkflow→`constructactivity.go`, ReplanSweepWorkflow→`replansweep.go`, ProjectSupervisionWorkflow→`projectsupervision.go`
- **projectdesign**: CoAuthorPhase2ArtifactWorkflow→`coauthorphase2artifact.go`, AssembleSDPReviewWorkflow→`assemblesdpreview.go`, Phase2AdvanceWorkflow→`phase2advance.go`
- **systemdesign**: SystemDesignPhaseWorkflow→`systemdesignphase.go`, CoAuthorArtifactWorkflow→`coauthorartifact.go`, PhaseAdvanceWorkflow→`phaseadvance.go`

Helper placement rule (the ONLY judgment call in Phase C): a helper func/type used by exactly one workflow moves into that workflow's file; used by ≥2 workflows or by contract methods → `<pkg>manager.go`. Determine mechanically: `grep -n "helperName" *.go` per candidate before placing. NO `workflow.Context`-taking helper may end up in `<pkg>manager.go` (the gate forbids it — nested workflow-context helpers like phase step funcs go with their single calling workflow; a `workflow.Context` helper shared by ≥2 workflows would violate the gate — if one exists, inline it into a non-context form or duplicate per-workflow; flag it in the commit).

Fold-into-`<pkg>manager.go` lists (everything else per inventory): billing: adapters.go, behavior.go, contract.go, deps.go, errors.go, signals.go, workermanifest.go; construction: adapters.go, codec.go(residue), contract.go, deps.go, eligibility.go, errors.go, gitforward.go, gitnaming.go, signals.go, workermanifest.go; operations: adapters.go, behavior.go, contract.go, deps.go, signals.go, workermanifest.go; projectdesign: acknowledgestale.go, airates.go, askquestions.go, behavior.go, codec.go(residue), contract.go, dispatch.go, errors.go, findings.go, gitrail.go(residue), gitsession.go, prompts.go, workermanifest.go; systemdesign: acknowledgestale.go, askquestions.go, behavior.go, catalog.go, codec.go(residue), contract.go, dispatch.go, errors.go, findings.go, gitrail.go(residue), gitsession.go, prompts.go, reviewledger.go(residue), statevalidationfindings.go, workermanifest.go — MINUS whatever the helper-placement rule assigns to a single workflow's file.

- [ ] Per manager: split, fold, merge tests (billing 2, construction 11, operations 2, projectdesign 10, systemdesign 20 test files → `manager_test.go`), verify (`=== RUN` count identical, full suite green), one commit each.

---

## Phase D — Flip the gate + verify

### Task D1: framework-go pin bump + wire the gate

- [ ] **Step 1:** `server/go.mod`: framework-go `v0.4.5` → `v0.5.0`; `go mod tidy`.
- [ ] **Step 2:** Add to `server/internal/arch_test.go`:

```go
// TestFileLayout enforces the 2026-07-11 layer file-layout standard (one impl
// file, one file per workflow, one test file, generated-only otherwise) —
// docs/superpowers/specs/2026-07-11-layer-file-layout-standard-design.md.
func TestFileLayout(t *testing.T) {
	arch.CheckFileLayout(t, appArchSpec())
}
```

- [ ] **Step 3:** Prune `arch_activitynames_test.go`'s `workermanifest.go` registration exemption (the file no longer exists; the platform check also covers hand registration now — keep the app test's `ExecuteActivity` string-literal rule, it is complementary).
- [ ] **Step 4:** Bump `const FrameworkGoVersion` in the folded sourcecontrol access file (was `agenticdesign.go:126`) `v0.4.4` → `v0.5.0` so newly seated consumer repos inherit the gate. Note in the commit: existing consumer repos keep their seated pin; they adopt the gate when THEY bump framework-go, and their layer packages may need the same migration then.
- [ ] **Step 5:** `GOWORK=off go test ./...` in `server/` — TestFileLayout green over the whole tree, zero waivers. Commit.

### Task D2: Full verification sweep

- [ ] **Step 1:** `cd server && make gen && git diff --exit-code` (all drift gates: models/client/temporal/sdk/config/uiprofiles).
- [ ] **Step 2:** `GOWORK=off go test ./...` (server), `make lint`, `GOWORK=off go test -tags methoddesign ./internal/ -run TestMethodDesignArtifacts`.
- [ ] **Step 3:** Registered-names golden green; eyeball the 20 workflow names unchanged.
- [ ] **Step 4:** Local boot per the run-app-locally procedure (GOWORK=off, CONSTRUCTION_DRYRUN, local-git substrate on branch main) and execute one workflow end-to-end (systemdesign phase advance or construction pump dry-run) against real state; confirm activities resolve under their new names in the Temporal dev-server UI.
- [ ] **Step 5:** Release notes / VERSION bump per the repo's release convention (`release: archistrator-server vNEXT`), including the DRAIN REQUIREMENT paragraph: in-flight workflow executions must be drained before deploying this version; Schedules resume automatically.
- [ ] **Step 6:** Update the spec's §3 hard-constraint paragraph to record the ratified clean-cut deviation (activity names change; workflow names frozen; golden test is the new stability mechanism) and commit spec+plan together.

---

## Deviations from the spec (ratified in-session 2026-07-11)

1. **Activity-name freeze dropped** — payload shapes and workflow code paths change regardless, so name preservation bought no replay compatibility. Clean cut + drain-before-deploy instead; workflow names stay frozen; a registered-names golden test replaces the freeze.
2. **temporalgen needs no generation extension** — promotion to contract-backed components rides the existing generator; the only generator change is DELETING the CustomActivities escape hatch.
3. **New RA components share existing Go packages** (RA→RA import ban makes separate packages impossible without type relocation); enabled by app-owned multi-component contract tooling (Task B1).
