# Deterministic Component Testing — Plan 2 of 3: Archistrator mechanism

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the platform pieces from Plan 1 into archistrator: the schema and codec for bindings and the new node kinds, the MCP verbs the agents and the venue call, removal of N-STP/N-IT and the system test plan, test-run fetch from GitHub artifacts, the testing-task gate, and the results panel in the webApp.

**Architecture:** `project.json` `$defs` is the source of truth; `make gen` regenerates `contract.gen.go`, handlers, tools and OAS. The server derives scenarios on read through `methodcheck.DeriveScenarios` over the projectstate adapter. Test runs are written by the venue through a new `record-test-run` subcommand of the aiarch-state MCP binary and read back by `artifactAccess` (GitHub zip in cloud, directory in local). The gate on the `testing` task lives in `runGate`.

**Tech Stack:** Go 1.25 (Temporal SDK, pgx), React + MUI + TanStack Query, Playwright, generated OAS → `schema.ts`.

**Spec:** `docs/superpowers/specs/2026-10-02-deterministic-component-testing-design.md`. Plan 1 (platform) must be released first; this plan bumps pins to `framework-go v0.12.0`, `app-generator v0.11.0`, `infrastructure-github v0.2.0`, `method-assets v0.10.0`, and adds `framework-go-scenariohost v0.1.0`.

## Global Constraints

- **Work in a separate git worktree** (`git worktree add ../archistrator-dct deterministic-component-testing`): another session is editing `server/internal/resourceaccess/projectstate` and `server/internal/manager/delivery` on the main tree. Never `git add -A`/`-a`; stage explicit paths.
- All builds and tests `GOWORK=off` (Makefile targets already do this).
- One model edit per wave: all `$defs` edits land in Task 1 and `make gen` runs once there; later tasks must not touch `$defs`.
- Enum ordinals never renumber: `ActivityNodeKind` appends `NodeSendSignal=15`, `NodeObjectNode=16`; `TestingVariant` keeps `Harness=1, Perf=2, QAProcess=4` and retires 0 and 3 (constants removed, ordinals never reused).
- Gates are never weakened: `TestFileLayout` stays at zero waivers. It will be **red from Task 1 until Plan 3 purges the in-package tests**; the branch does not merge until green.
- `.aiarch/state/project.json` is state; schema edits there are hand edits, data edits go through the MCP verbs or the Plan 3 workflow.
- **Concurrent main-tree work to merge against** (from session archistrator-6b, 2026-10-02): `NewDeliveryManager(...)` gained a positional `designAdapter` arg (index 13); project birth now seeds slots 9/10 via `seedDesignPrefixPlan` in `deliverymanager.go` and costs `1 + birthPlanSeedWrites` versions; the resume path backfills the design prefix. Task 4's derivation change therefore alters what every project is **born** with — rebase the worktree onto main before Task 4 and run the birth/resume tests (`TestIRADelta_ResumeFromExistingAiarchState`, the `assertBirthDesignPrefix` tests) with N-STP/N-IT gone. Safety tag on main: `safety/bench-wave-20261002`.
- The SPA sends `frame-ancestors 'none'` everywhere (`vite.config.ts`, `nginx.conf`, enforced by `cmd/server/frame_denial_nginx_test.go`); the trace viewer opens in a **new tab**, never an iframe.

## Review Focus

1. A `TestRun` recorded for an **older revision** than the activity's current construction revision must not pass the gate — pinned in Task 7 (`TestTestingGate_StaleRevisionHolds`).
2. A `results.json` whose scenario set is a **strict subset** of `ForComponent` (a scenario silently not run) must hold the gate naming the missing ids — Task 7.
3. `DownloadArtifact` returning an **expired** artifact (GitHub 410) must surface as a rendered "expired" state, not a 500 — Task 5 (`TestGetTestRunIndex_Expired`).
4. The webApp must render a run whose `videoRef` is absent (server component) without a broken player — Task 9 test.
5. `record-test-run` invoked with an empty `test-results/` directory must fail loudly (exit 1) rather than record an empty passing run — Task 3 (`TestRecordTestRun_EmptyDirFails`).

---

## File Structure

- `.aiarch/state/project.json` — `$defs` edits (Task 1).
- `server/internal/resourceaccess/projectstate/` — `contract.gen.go` (regen), `projectstateaccess.go` (codec names, `deriveVariant`, type deletions, scenario adapter), `scenario_adapter.go` (new).
- `server/internal/engine/estimation/estimationengine.go` — N-STP/N-IT removal.
- `server/cmd/aiarch-state-mcp/` — `scenarioverbs.go` (new: `listComponentScenarios`), `tools.go` (register), `recordtestrun.go` (new subcommand).
- `server/internal/resourceaccess/artifact/` — `contract.gen.go` (regen from `$defs`), `artifactaccess.go` (test-run ops), `testrun_cloud.go`, `testrun_local.go` (new).
- `server/internal/manager/delivery/` — `deliverymanager.go` (`queryTestRunView`, `ComponentScenarios`), `deliveryactivity.go` (gate), `testinggate.go` (new).
- `server/cmd/server/hooks.go` — `mountRoutes` adds the file endpoint.
- `server/internal/arch_test.go` — `HooksImportAllowlist`.
- `webApp/src/` — `hooks/useTestRun.ts`, `components/construction/renderers/TestRunPanel.tsx`, `components/construction/renderers/ScenarioPlanView.tsx`, `components/activity/taskArtifactFor.ts`, `components/construction/artifactRenderers.tsx`, `contracts/types.ts`, `contracts/wire.ts`, `eslint.config.js`, `package.json`.
- Deleted: `systemtests/`, `server/cmd/gen-systemtests`, `server/cmd/gen-uitests-fixtures`, `server/cmd/gen-uitests-episodes`, `.github/workflows/systemtests.yml`, `webApp/src/components/construction/renderers/SystemTestRunView.tsx`, `systemTestRunSummary.ts(+test)`.

---

### Task 0: Worktree, pins, and the red baseline

**Files:**
- Modify: `server/go.mod` (pins), `go.work` (add scenariohost path), `server/internal/arch_test.go:362` (`appArchSpec`)

- [ ] **Step 1: Create the worktree and branch**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git worktree add ../archistrator-dct -b deterministic-component-testing main
cd ../archistrator-dct
```

All later steps run in `../archistrator-dct`.

- [ ] **Step 2: Bump pins**

In `server/go.mod`: `framework-go v0.12.0`, `framework-go-app-generator v0.11.0`, `framework-go-infrastructure-github v0.2.0`, `method-assets v0.10.0`; add `framework-go-scenariohost v0.1.0`. `go.work`: add `../archistrator-platform/framework-go-scenariohost`. Run `cd server && GOWORK=off go mod tidy`.

- [ ] **Step 3: Record the expected red**

Run: `cd server && GOWORK=off go test ./internal/ -run TestFileLayout 2>&1 | grep -c scenario-tests-only`
Expected: 21 (the in-package component test files). Paste the count into the commit message; it is the number Plan 3 must drive to 0.

- [ ] **Step 4: appArchSpec**

```go
spec.HooksImportAllowlist = []string{modulePrefix + "/internal/resourceaccess/projectstate/fake", modulePrefix + "/internal/testsupport"}
```

(add only prefixes that exist; `internal/testsupport` may not — omit if absent).

- [ ] **Step 5: Commit**

```bash
git add server/go.mod server/go.sum go.work server/internal/arch_test.go
git commit -m "dct: pin platform v0.12/v0.11/v0.2/v0.10 + scenariohost; TestFileLayout red at 21 (expected until Plan 3)"
```

---

### Task 1: Schema — node kinds, bindings, test runs, retired variants

**Files:**
- Modify: `.aiarch/state/project.json` → `.serviceContracts.projectStateAccess["$defs"]`
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` (`activityNodeKindNames` ~7115; `ActivityNode` 4808; `deriveVariant` 8523-8539; delete 5253-5370 test types; `PhaseArtifactPayload` 2080; `applyTestingStatePayload` 2558; `TestPlanRecord` sites 2083/2495)
- Modify: `server/internal/resourceaccess/projectstate/access_test.go` (1133, 4802 TestPlanRecord uses)
- Regenerate: `make gen`

**Interfaces (Go, generated from `$defs`):**

```go
// ActivityNodeKind: existing 0..14 + NodeSendSignal = 15, NodeObjectNode = 16
// TestingVariant: TestVariantHarness = 1, TestVariantPerf = 2, TestVariantQAProcess = 4 (0 and 3 retired)
type TestPlanRecord struct {
	Component  string            `json:"component"`
	Bindings   []ScenarioBinding `json:"bindings"`
	AuthoredAt *time.Time        `json:"authoredAt"`
}
type ScenarioBinding struct { Scenario string; Steps []StepBinding; Skip *SkipReason }
type SkipReason struct { Reason, Until string }
type StepBinding struct { Seq int; Operation string; Inputs []TestArg; Expect TestExpect; Probes []Probe; Unobservable []string; Hook bool }
type Probe struct { For string; Component string; Operation string; Inputs []TestArg; Expect TestExpect }
type TestArg struct { Name, Value, SchemaRef string }        // moved into $defs
type TestExpect struct { Result string; ErrorExpected bool; ErrorCode string } // moved into $defs
type TestRun struct { ID, Activity, Component, Revision, Artifact string; StartedAt, EndedAt *time.Time; Scenarios []ScenarioResult }
type ScenarioResult struct { Scenario, Status string; DurationMs int; VideoRef, TraceRef string }
type TestingState struct { HarnessModule *HarnessModule; PerfHarness *PerfHarness; QualityGates []QualityGate; QualityAuditReport string; TestRuns []TestRun; Defects []DefectRecord }
```

- [ ] **Step 1: Failing codec test**

Append to `access_test.go` (package `projectstate` today — it will be purged in Plan 3; fine to extend now):

```go
func TestActivityNodeKindCodecKnowsIOKinds(t *testing.T) {
	for name, want := range map[string]ActivityNodeKind{"sendSignal": NodeSendSignal, "objectNode": NodeObjectNode} {
		var k ActivityNodeKind
		if err := json.Unmarshal([]byte(`"`+name+`"`), &k); err != nil || k != want {
			t.Fatalf("%s: %v %v", name, k, err)
		}
	}
	n := ActivityNode{ID: "x", Kind: NodeObjectNode, Label: "Invoice", InState: "[paid]"}
	b, _ := json.Marshal(n)
	if !strings.Contains(string(b), `"inState":"[paid]"`) { t.Fatalf("%s", b) }
}
func TestDeriveVariantNoLongerKnowsPlanOrSystemTest(t *testing.T) {
	if _, ok := deriveVariant("N-STP"); ok { t.Fatal("N-STP must not classify") }
	if _, ok := deriveVariant("N-IT"); ok { t.Fatal("N-IT must not classify") }
	if v, ok := deriveVariant("N-PERF"); !ok || v != TestVariantPerf { t.Fatal(v) }
}
```

(`deriveVariant` changes signature to `(TestingVariant, bool)`; update its one caller in `ClassifyActivity` ~8570-8640 to treat `!ok` as not-Testing.)

- [ ] **Step 2: Run** — `cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestActivityNodeKindCodec|TestDeriveVariant'` → FAIL (compile).

- [ ] **Step 3: Edit `$defs`** (with `jq` or by hand, then `make gen`):

- `ActivityNodeKind.enum` → `[0..16]`; `x-enum-varnames` + `"NodeSendSignal","NodeObjectNode"`.
- `TestingVariant`: `"enum":[1,2,4]`, `"x-enum-varnames":["TestVariantHarness","TestVariantPerf","TestVariantQAProcess"]`.
- New defs `TestArg`, `TestExpect`, `Probe`, `StepBinding`, `SkipReason`, `ScenarioBinding`, `ScenarioResult`, `TestRun` as above (`additionalProperties:false`, `required` on every non-pointer field; `time.Time` via `x-go-type` like `TestPlanRecord.authoredAt` today).
- `TestPlanRecord.properties`: drop `content`; add `bindings: {"type":"array","items":{"$ref":"#/$defs/ScenarioBinding"}}`; `required: ["component","bindings"]`.
- `ActivityNode` is hand-written (not in `$defs`): add `InState string \`json:"inState,omitempty"\`` and `Anchor string \`json:"anchor,omitempty"\``.

Run `make gen-models` → `contract.gen.go` regenerates. Then in `projectstateaccess.go`:
- `activityNodeKindNames`: add `NodeSendSignal: "sendSignal", NodeObjectNode: "objectNode"`.
- `BookEnumerated()` (4792): unchanged (`k <= NodeNote`).
- `requireActivityNodes` (6245): accept the two kinds; objectNode requires non-empty `inState`.
- Delete `TestStep`, `TestCase`, `TestScenario`, `SystemTestPlan` and move `TestRun`/`TestArg`/`TestExpect` to the generated file (delete the hand copies). `TestingState` loses `SystemTestPlan`.
- `PhaseArtifactPayload`: drop `SystemTestPlan`; keep `TestRun *TestRun`.
- `applyTestingStatePayload`: drop the SystemTestPlan branch; `TestRun` **replaces** an existing run with the same `ID` instead of appending blindly.
- `deriveVariant` → `(TestingVariant, bool)`; remove the N-STP and N-IT rows and the `default: TestVariantPlan`.

- [ ] **Step 4: Full regen + build**

Run: `cd server && make gen && GOWORK=off go build ./... && GOWORK=off go vet ./...` → clean. `make gen-models-check gen-client-check gen-internal-tools-check` → clean. Then the two tests → PASS.

- [ ] **Step 5: Commit**

```bash
git add .aiarch/state/project.json server/internal/resourceaccess/projectstate/ server/internal/client server/api/openapi.yaml 'server/internal/**/contract.gen.go' server/internal/resourceaccess/projectstate/toolcatalog.gen.go
git commit -m "model(dct): the one edit — sendSignal/objectNode kinds, typed scenario bindings, TestRun; retire SystemTestPlan and variants 0/3"
```

---

### Task 2: Server-side scenario derivation + `listComponentScenarios`

**Files:**
- Create: `server/internal/resourceaccess/projectstate/scenario_adapter.go`
- Create: `server/cmd/aiarch-state-mcp/scenarioverbs.go`
- Modify: `server/cmd/aiarch-state-mcp/tools.go` (`composedVerbs`), `server/cmd/aiarch-state-mcp/scenarioverbs_test.go` (new)

**Interfaces:**

```go
// projectstate
func ScenarioInputOf(p *Project) (scenario.Input, error) // mirrors methodcheck.ScenarioInput over the typed Project
func ComponentScenarios(p *Project, component string) ([]scenario.Scenario, error)

// MCP verb (modes: construct + shared), read-only
// listComponentScenarios {component} → canonical JSON of ComponentScenarios
```

- [ ] **Step 1: Failing test** (`scenarioverbs_test.go`, package main — allowed, cmd/ is not a component):

```go
func TestListComponentScenariosReturnsProjectedSet(t *testing.T) {
	s := newSessionFromFixture(t, "../../.aiarch/state/project.json") // existing fixture helper pattern in cmd/aiarch-state-mcp tests
	out, err := s.listComponentScenarios("deliveryManager")
	if err != nil { t.Fatal(err) }
	var got []scenario.Scenario
	if err := json.Unmarshal([]byte(out), &got); err != nil { t.Fatal(err) }
	if len(got) == 0 { t.Fatal("deliveryManager must receive stimuli in execute-a-project-activity") }
	for _, sc := range got { for _, st := range sc.Stimuli { if st.Input.To != "deliverymanager" { t.Fatalf("leaked stimulus %+v", st) } } }
}
```

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement**

`scenario_adapter.go` builds `scenario.Input` exactly like Plan 1 Task 3's adapter but from the typed `Project` (use cases from the committed core-use-cases slot via the existing slot accessor used by `ValidateModelIdentities`; actors from `UseCase.Actors`; views from the system slot's `DynamicViews`; contracts from `p.ServiceContracts[*].Interface.Operations`). Node kind → wire name via `activityNodeKindNames[k]`.

`scenarioverbs.go`:

```go
type listComponentScenariosInput struct {
	Component string `json:"component" jsonschema:"contract key or slot-5 id of the component whose projected scenarios to list"`
}

func (s *Session) listComponentScenarios(component string) (string, error) {
	p, err := s.readProjectForVerb()
	if err != nil {
		return "", err
	}
	sc, err := projectstate.ComponentScenarios(p, component)
	if err != nil {
		return "", err
	}
	b, err := scenario.CanonicalJSON(sc)
	return string(b), err
}
```

Register in `composedVerbs` with `modes: shared`, `ReadOnlyHint: true`, description: "Deterministic scenarios projected onto one component (inputs into it, expected outputs/states, hints). Bind each one in the test plan; never invent scenarios."

- [ ] **Step 4: Run** → PASS. `make gen-internal-tools-check` unaffected (composed verbs are hand-registered).

- [ ] **Step 5: Commit** — explicit paths; message `dct: listComponentScenarios verb + server-side scenario adapter`.

---

### Task 3: `record-test-run` subcommand

**Files:**
- Create: `server/cmd/aiarch-state-mcp/recordtestrun.go`, `recordtestrun_test.go`
- Modify: `server/cmd/aiarch-state-mcp/main.go` (subcommand dispatch beside `seat-assets`/`step-tools`)

**Interfaces:**

```
aiarch-state-mcp record-test-run --activity A --component C --run-id R --artifact NAME --results DIR [--revision REV]
```
Reads `DIR/<component>/results.json` (shape = `scenariohost.ScenarioResult[]`), builds `projectstate.TestRun{ID: R, Activity: A, Component: C, Revision: REV or env AIARCH_REVISION, Artifact: NAME, StartedAt/EndedAt from file mtimes, Scenarios}` and writes it with `PhaseArtifactPayload{TestRun: &run}` through the same construction-mutation path `recordPhaseArtifact` uses (target branch from env as the construct mode already does).

- [ ] **Step 1: Failing tests**

```go
func TestRecordTestRun_EmptyDirFails(t *testing.T) {
	dir := t.TempDir()
	err := runRecordTestRun([]string{"--activity", "C-BG", "--component", "billingManager", "--run-id", "1", "--artifact", "a", "--results", dir}, nil)
	if err == nil || !strings.Contains(err.Error(), "no results.json") { t.Fatalf("want loud failure, got %v", err) }
}
func TestRecordTestRun_WritesRun(t *testing.T) {
	dir := t.TempDir(); os.MkdirAll(filepath.Join(dir, "billingManager"), 0o755)
	os.WriteFile(filepath.Join(dir, "billingManager", "results.json"), []byte(`[{"scenario":"bill-the-user-for-usage-P1","status":"pass","durationMs":5}]`), 0o644)
	var got projectstate.TestRun
	err := runRecordTestRun([]string{...same flags...}, func(r projectstate.TestRun) error { got = r; return nil })
	if err != nil || got.ID != "1" || len(got.Scenarios) != 1 { t.Fatalf("%v %+v", err, got) }
}
```

(`runRecordTestRun(args, sink)` takes an injectable sink; `nil` sink = real session write.)

- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** per the interface (flag parsing with `flag.NewFlagSet`). **Step 4: Run** → PASS. **Step 5: Commit** `dct: record-test-run subcommand for the venue`.

---

### Task 4: Estimation — remove N-STP/N-IT and the sink rule

**Files:**
- Modify: `server/internal/engine/estimation/estimationengine.go` (1360-1367 `noncodingInventoryClass`; 1432-1439 `alwaysEmitNoncoding`; 1444 const; 1868-1891 `addSinkEdges` + its call site)
- Modify: `server/internal/engine/estimation/engine_test.go` (in-package today; extend — purged in Plan 3)

- [ ] **Step 1: Failing tests**

```go
func TestDerivedPlanHasNoSystemTestActivities(t *testing.T) {
	plan := derivePlanFromFixture(t) // existing helper; else load .aiarch/state/project.json via the engine's DerivePlan
	for _, a := range plan.Activities { if a.Name == "N-STP" || a.Name == "N-IT" { t.Fatal(a.Name) } }
	for _, d := range plan.Dependencies { for _, p := range d.DependsOn { if p == "N-IT" { t.Fatal("N-IT dependency survived") } } }
}
func TestNetworkWithMultipleSinksComputesFloat(t *testing.T) {
	// two terminal activities; critical path must still be computed (longest chain), float of the shorter sink > 0
	plan := derivePlanFromFixture(t)
	net := computeNetworkFor(t, plan)
	if len(net.CriticalPath) == 0 { t.Fatal("critical path empty with multiple sinks") }
}
```

- [ ] **Step 2: Run** → FAIL. **Step 3: Implement**: delete `alwaysEmitNoncoding` entries (keep the slice var if other code ranges it; it becomes empty — if nothing else uses it, delete the var and its loop), delete `systemTestingActivity`, delete `addSinkEdges` and its call, drop the N-STP/N-IT rows from `noncodingInventoryClass`. Check `ComputeNetwork` for an assumption of a single sink (search `sink`/`terminal` in the engine); if the forward/backward pass takes `maxFinish` over all nodes it already handles multiple sinks. **Step 4:** PASS + `go vet`. **Step 5: Commit** `dct: derive no N-STP/N-IT; terminal activities end the network`.

---

### Task 5: artifactAccess — test-run index and files

**Files:**
- Modify: `.aiarch/state/project.json` — **NO**: `$defs` already final. The artifactAccess contract's `interface.operations` gains two ops (this is a contract edit, not a `$defs` edit; it is allowed here because it's the same wave's one model commit? — **No.** Fold these two ops into Task 1's model edit. Re-order: do Task 5's contract lines in Task 1, implement here.)
- Modify: `server/internal/resourceaccess/artifact/artifactaccess.go`, create `testrun_cloud.go`, `testrun_local.go`, extend `access_test.go`
- Modify: `server/cmd/server/hooks.go:867` (`ArtifactAccessGitHubCloudArgs` passes `h.appClient` instead of raw creds) and `main.gen.go` regen if the args tuple changes (composegen reads the deployment model; if the tuple is model-driven, add it in Task 1).

**Interfaces (ops added in Task 1):**

```go
GetTestRunIndex(rc fwra.Context, run TestRunRef) (TestRunIndex, error)
OpenTestRunFile(rc fwra.Context, run TestRunRef, path string) (ConstructionOutput, error)

type TestRunRef struct { RunID string; Artifact string }   // cloud: Artifact = GH artifact name, RunID = GH run id; local: Artifact = directory
type TestRunIndex struct { Results []byte /* results.json */; Files []string; Expired bool }
```

- [ ] **Step 1: Failing tests** (package `artifact` — purged in Plan 3)

```go
func TestGetTestRunIndex_Local(t *testing.T) { dir := writeResultsDir(t); a := NewGitLocalArtifactAccess("file://x"); idx, err := a.GetTestRunIndex(ctx, TestRunRef{Artifact: dir}); /* Files contains "billingManager/results.json"; Results non-empty */ }
func TestGetTestRunIndex_Cloud_FetchesZipOnce(t *testing.T) { fa := ghtestinfra.StartActions(); fa.AddArtifact(42, "test-results-C-BG-9", zipOf(t, map[string][]byte{"billingManager/results.json": []byte("[]")})); a := newCloudForTest(fa.BaseURL); _, _ = a.GetTestRunIndex(ctx, TestRunRef{RunID: "9", Artifact: "test-results-C-BG-9"}); _, _ = a.OpenTestRunFile(ctx, ref, "billingManager/results.json"); if fa.DownloadCount(42) != 1 { t.Fatal("zip must be cached per run") } }
func TestGetTestRunIndex_Expired(t *testing.T) { fa.AddExpiredArtifact(43, "x"); idx, err := a.GetTestRunIndex(ctx, ref43); if err != nil || !idx.Expired { t.Fatal() } }
```

- [ ] **Step 2: Run** → FAIL. **Step 3: Implement**: cloud variant lists artifacts for `RunID`, finds by name, `Expired` → return `{Expired:true}`; else `DownloadArtifact` → write zip to `os.UserCacheDir()/archistrator/testruns/<runid>.zip` (once), index with `archive/zip`; `OpenTestRunFile` reads one entry (reject `..`). Local variant walks the directory. `FakeActions` gets `AddExpiredArtifact` + `DownloadCount` (platform patch → requires `infrastructure-github v0.2.1`; if you'd rather not re-release, implement the count via `fa.Requests` filtering, which exists). **Step 4:** PASS. **Step 5: Commit** `dct: artifactAccess test-run index/file ops (GitHub zip cached per run; local dir)`.

---

### Task 6: DeliveryManager — `testRun` view and file endpoint

**Files:**
- Modify (ops/schemas in Task 1's model edit): `deliveryManager.QueryProjectView` kind enum gains `testRun`; `ProjectView` gains `testRun *DeliveryTestRunView`; query gains `activityId` (exists) and `runId?`.
- Modify: `server/internal/manager/delivery/deliverymanager.go` (`QueryProjectView` switch at 11141; new `queryTestRunView`)
- Modify: `server/cmd/server/hooks.go` `mountRoutes` (583): `GET /api/v1/delivery/test-run/{runID}/file?path=`
- Test: `server/internal/manager/delivery/manager_test.go` (in-package; purged in Plan 3)

**Interfaces:**

```go
type DeliveryTestRunView struct {
	Run       projectstate.TestRun
	Scenarios []TestRunScenarioView // joined: scenario title/guards from ComponentScenarios + result + stdout/stderr refs
	Missing   []string              // projected ids with no result
	Unknown   []string              // results with no projected scenario
	Expired   bool
	Artifact  string
}
```

- [ ] **Step 1: Failing test**: a fixture project with one `TestRun` for `C-BG` whose results omit one projected scenario → `queryTestRunView` returns `Missing` = that id and `Scenarios[i].Status == "pass"` for the rest.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement**: latest run for `activityId` (or by `runId`), `ComponentScenarios(p, run.Component)`, join by id, `artifact.GetTestRunIndex` for `Expired`. File endpoint: authorize with the same PDP action as `query-project-view`, `OpenTestRunFile`, set `Content-Type` by extension (`video/webm`, `application/zip`, `application/json`, `text/plain`), stream. **Step 4:** PASS; `make gen-client` already done in Task 1 so `openapi.yaml` has the view. **Step 5: Commit** `dct: testRun project view + test-run file endpoint`.

---

### Task 7: The testing gate

**Files:**
- Create: `server/internal/manager/delivery/testinggate.go`
- Modify: `server/internal/manager/delivery/deliveryactivity.go` `runGate` (2270): before `passRound`, when `t.ID == "testing"`.
- Test: `manager_test.go` (Temporal testsuite as the existing gate tests do)

**Interfaces:**

```go
// testingGateHold returns "" when the gate may pass, else the hold reason (becomes a
// system ReviewComment with AuthorRole "system" anchored at $.testRuns[id=<run>]).
func (m *constructionManager) testingGateHold(rc fwra.Context, projectID projectstate.ProjectID, activityID, component, currentRevision string) string
```

Rules (spec §6.5): latest run for activity exists; `run.Revision == currentRevision`; `Missing`/`Unknown` empty; every status `pass` (skips hold with "skipped: <ids>"); `GetTestRunIndex` ok and not expired; for client components every `VideoRef`/`TraceRef` present in `Files`.

- [ ] **Step 1: Failing tests**: `TestTestingGate_PassesWhenAllGreen`, `TestTestingGate_StaleRevisionHolds`, `TestTestingGate_MissingScenarioHolds` (names the id), `TestTestingGate_SkipHolds`, `TestTestingGate_ExpiredArtifactHolds`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** + wire into `runGate` via an activity call (gate evaluation reads RA, so it runs as a Temporal activity like `proposeReviewSet`); on hold: `appendVerdict` with the system comment and `awaitTaskDecision` as the existing hold path does. **Step 4:** PASS. **Step 5: Commit** `dct: testing task passes only on a full green run at the current revision`.

---

### Task 8: Remove the system test machinery

**Files:**
- Delete: `systemtests/` (module), `server/cmd/gen-systemtests/`, `server/cmd/gen-uitests-fixtures/`, `server/cmd/gen-uitests-episodes/`, `.github/workflows/systemtests.yml`, `uitests/tests/**` (all hand-written specs incl. `preview/`, `meta/`, `seed/`), `uitests/support/`, `uitests/preview-fixtures/`, `uitests/testdata/`
- Modify: `go.work` (drop `./systemtests`), `server/Makefile` (drop `gen-systemtests` targets if any), `uitests/package.json` scripts (`test:construction`, `regen:*`, `check:*` removed; add `"test:scenarios": "playwright test -c generated/playwright.config.gen.ts"` after Plan 3 generates it), `.github/workflows/uitests.yml` (jobs run `npm run test:scenarios`), `webApp/package.json` (`build:preview` + `vite.preview.config.ts` + `previewShell` stay only if something else uses them — `grep -rn previewShell webApp/src` → if only uitests used it, delete `src/previewShell/` and the script)
- Modify: `server/cmd/backfill-attempts/main.go:138-142` (drop N-STP/N-IT handling)

- [ ] **Step 1:** `grep -rn "systemTestPlan\|SystemTestPlan\|N-STP\|N-IT\|gen-systemtests\|systemtests" --include='*.go' --include='*.ts' --include='*.tsx' --include='*.yml' --include='Makefile' . | grep -v node_modules` → list; every hit is deleted or rewritten in this task.
- [ ] **Step 2:** Delete + edit. `GOWORK=off go build ./... && go vet ./...` in `server/`. `cd webApp && npm run typecheck`.
- [ ] **Step 3:** Re-run the grep → zero hits outside `docs/`.
- [ ] **Step 4: Commit** `dct: remove systemtests, STP generators, hand-written uitests, N-STP/N-IT residue`.

---

### Task 9: webApp — plan view, results panel, trace viewer

**Files:**
- Modify: `webApp/src/contracts/types.ts` (`TestingStateView` → `{testRuns: TestRunView[]|null; defects}`; `TestRunView` → spec shape; add `TestRunPanelView`, `ScenarioBindingView`), `wire.ts` (mappers), `components/activity/taskArtifactFor.ts` (`ArtifactBodyKind` −`testing:plan` −`testing:systemTest`; `service`/`frontend` gain phases `test_plan` → `{kind:'classified'}` for the `stp`/`stpReview` tasks and `integration` for `testing`), `components/construction/artifactRenderers.tsx` (map `service`/`frontend` to a phase-aware renderer: `test_plan` → `ScenarioPlanView`, `integration` → `TestRunPanel`), `components/activity/ArtifactPanel.tsx:216` (pass `phase` into `Renderer`)
- Create: `webApp/src/hooks/useTestRun.ts`, `components/construction/renderers/ScenarioPlanView.tsx` (repurposes `ScenarioBrowser` to render `ComponentScenarios + bindings`: id, title, guards, stimuli with op/inputs/expect/probes; anchors `$.phaseArtifacts.testPlan[<comp>].bindings[scenario=<id>]`), `components/construction/renderers/TestRunPanel.tsx`
- Delete: `SystemTestRunView.tsx`, `systemTestRunSummary.ts`, `TestPlanView.tsx` (replaced)
- Modify: `webApp/package.json` (`"postinstall": "node scripts/copy-trace-viewer.mjs"` copying `node_modules/playwright-core/lib/vite/traceViewer` → `public/trace-viewer/`; add `playwright-core` devDependency), `webApp/eslint.config.js` (rule: `files: ['src/**/*.test.{ts,tsx}']` → `'no-restricted-syntax'` error "scenario tests only; see spec T2" — applies once Plan 3 deletes the 131 files), `webApp/.gitignore` (+`public/trace-viewer/`)

**Interfaces:**

```ts
export function useTestRun(projectId: string, activityId: string | undefined): UseQueryResult<TestRunPanelView>
export function useComponentScenarios(projectId: string, componentId: string | undefined): UseQueryResult<ScenarioView[]>  // via queryProjectView kind 'scenarios' — add the kind in Task 1's model edit
export interface TestRunPanelView { run: TestRunView; scenarios: TestRunScenarioView[]; missing: string[]; unknown: string[]; expired: boolean; fileUrl: (path: string) => string }
```

`TestRunPanel` layout: header (`StatTile` ×3 pass/fail/skip, run id, revision, "Open in venue" link); `CommentableList` of scenarios (status `Chip`, title, guards); selected scenario: MUI `Tabs` → "Output" (pre-wrap stdout/stderr from `go-test.jsonl` filtered by test name `TestScenario_<id>`, fetched through `fileUrl('go-test.jsonl')`) and, when `videoRef`, "Video" (`<video controls src={fileUrl(videoRef)}>`), plus a button "Open trace" → `window.open('/trace-viewer/index.html?trace=' + encodeURIComponent(fileUrl(traceRef)), '_blank')`. `expired` → `UnavailablePanel` reason "Artifact expired; re-run integration".

- [ ] **Step 1: Failing tests** — the node unit-test rig still exists until Plan 3; write `TestRunPanel.logic.test.ts` for the pure helpers (`summarize(scenarios) → {pass,fail,skip}`, `goTestOutputFor(jsonl, id)`), and a `uitests/generated`-independent Playwright check is **not** written here (spec T2: the webApp's own tests become generated scenario specs in Plan 3). Pin Review Focus 4: `summarize` + `tabsFor(scenario)` returns `['Output']` when `videoRef` is empty.
- [ ] **Step 2:** FAIL → **Step 3:** implement → **Step 4:** `npm run check` (typecheck, lint, format, test) green → **Step 5: Commit** `dct(webApp): scenario plan view, test-run panel with video + self-hosted trace viewer`.

---

### Task 10: CI, docs, and the merge gate list

**Files:**
- Modify: `.github/workflows/server-checks.yml` (add `make gen-tests-check` after `gen-models-check`), `.github/workflows/aiarch-construct.yml` (re-render from method-assets v0.10.0 via `seat-assets`/scaffold — or copy the template's new steps verbatim), `server/Makefile` (`include Makefile.scenarios.mk`; `gen:` adds `gen-tests`), `CLAUDE.md` (testing section: "component tests are generated; see spec")
- Create: `docs/bugs/2026-10-02-dct-earmarks.md` — spec §10 earmarks + anything deferred during Tasks 0-9.

- [ ] **Step 1:** Edits. **Step 2:** `make gen && make gen-models-check gen-client-check gen-internal-tools-check gen-lifecycles-check` clean; `GOWORK=off go test ./... -short` — everything green **except** `TestFileLayout` (still 21; Plan 3) — paste the exact failing list into the earmarks file as the Plan 3 checklist. **Step 3: Commit** `dct: CI wiring, earmarks, Plan-3 checklist (21 files)`.

---

## Self-review

- **Spec coverage:** §3 → Task 1; §3.2 verbs → Tasks 2, 3; §6.1–6.3 → Tasks 3, 5, 6; §6.4 → Task 9; §6.5 → Task 7; §7 archistrator rows → Tasks 4, 8, 9; §9.2 → whole plan. §5.3 drift → Task 10 (`gen-tests-check`); the generation itself runs in Plan 3 per component.
- **Type consistency:** `TestRun`/`ScenarioResult` field names match Plan 1's `scenariohost.ScenarioResult` JSON (`scenario,status,durationMs,videoRef,traceRef`) and `methodcheck.TestRun`. `TestRunRef{RunID, Artifact}` is used by Tasks 5, 6, 7. `queryProjectView` kinds added (`testRun`, `scenarios`) are declared in Task 1's one model edit.
- **Placeholder scan:** Task 5's first bullet talked itself into a reorder; the resolution stands: the artifactAccess and deliveryManager contract ops/views are part of Task 1's single `$defs`/contract edit. Executors do Task 1 with Tasks 5 and 6's interface blocks open.
- **Review Focus:** 1,2 → Task 7; 3 → Task 5; 4 → Task 9; 5 → Task 3.
- **Known red:** `TestFileLayout` (21) and `uitests` (no specs) until Plan 3.
