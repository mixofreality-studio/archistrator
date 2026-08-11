# Compression Moves & Predicate Attachment — Stage 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the compressed solution's `criticalSpeedup` fudge factor with Löwy's actual compression — network mutation — and close the two vocabulary gaps Stage 1 left behind.

**Architecture:** Two layers, deliberately separate. **Layer 1** adds a closed `gates` predicate so an authored entry can precede derived work (`N-CI` before every coding activity), closing spec conditions C3/C4 — this affects *the plan*, slot 9/10. **Layer 2** adds a typed `moves[]` union (`simulator`, `designFirst`, `topResources`, `split`) applied to the materialized plan to produce an *option's* network — slots 11–14 only. Moves may redirect and remove derived edges; predicates may only add `additive → derived` edges. Everything runs through the existing `estimationEngine`; `ComputeNetwork` / `EstimateForOption` are untouched.

**Tech Stack:** Go 1.26, `GOWORK=off`, schema-first codegen (`cmd/modelgen` reads `.serviceContracts` in `project.json`), `framework-go/engine` (`fweng`), hand-edited state gated by `aiarch-state-mcp validate`.

**Spec:** `docs/superpowers/specs/2026-08-10-compression-moves-stage2-design.md` (`523e7ef`)
**Stage 1 spec, with its post-execution amendment block:** `docs/superpowers/specs/2026-08-09-derived-activity-list-design.md`

## Global Constraints

- **All Go commands run from `server/` with `GOWORK=off`.** `golangci-lint` needs it explicitly too; `go test` and `make` targets set it themselves.
- **Run `GOWORK=off make claude-assets` once per fresh worktree**, or `TestConstructionPromptsUseStateTools` fails on a missing SKILL.md (the `.claude` skills tree is gitignored and materialized).
- **Layer gates live in `server/internal/arch_test.go`** (`TestFileLayout`, `TestMethodLayering`, `TestGeneratedOnlyPublic`) and are **not** loaded by package-scoped test runs. Run `GOWORK=off go test ./internal/` **every task**. A file-layout regression went unnoticed for four tasks in Stage 1.
- **Layer file-layout standard, live with zero waivers.** A leaf layer package may contain only one handwritten implementation file (`<pkg>engine.go` / `<pkg>manager.go` / `<pkg>access.go`), one file per Temporal workflow (managers only), and one test file (`engine_test.go` / `manager_test.go` / `access_test.go`). **Create no new `.go` files.** Large files are an explicitly sanctioned consequence.
- **`contract.gen.go` and `fake/fake.gen.go` are generated.** Change `.serviceContracts` in `project.json`, then `GOWORK=off make gen-models`. Never hand-edit. `gen-models-check` diffs against the git index — `git add` first.
- **`modelgen` emits a Go pointer iff the field is absent from that `$def`'s `required` array.** Not the null-union. Decide requiredness deliberately.
- **Engine purity (hard gate):** `internal/engine/estimation` imports only `fweng` and the standard library. No I/O, clock, randomness, goroutines, or global mutable state.
- **State editing:** `json.dump(d, f, indent=2, ensure_ascii=False)` + trailing newline, no `sort_keys`, not `indent=1` — anything else rewrites all 28k lines. Bump the top-level `version`. Then `cd server && GOWORK=off go build -o /tmp/aiarch-state-mcp ./cmd/aiarch-state-mcp && cd .. && /tmp/aiarch-state-mcp validate --root .` — **errors must stay 0** (baseline 45 advisory / 0 errors). Judge the diff by proportionality and contiguity, not raw line count.
- **`make derived-plan-check` must stay GREEN throughout.** Moves live on option slots; they must never alter slot 9 or slot 10. A RED gate after a Layer-2 task means something touched the plan.
- **Lint:** `revive` flags unused parameters and enforces `package-comments` per file; `gocyclo` caps cyclomatic complexity at **15** on production *and* test files — **measure before extracting** a helper and report the number. `gosec` is disabled for `_test.go` only. **Never edit `.golangci.yml`** — it is converged by the managed-scaffold sync.
- **Worker classes are a fixed roster**, spelled exactly: `system-architect`, `product-manager`, `project-manager`, `senior-developer`, `junior-developer`, `ui-designer`, `ux-reviewer`, `qa-engineer`, `test-engineer`, `software-tester`. Effort in 5-day quanta, ≤35. Risk buckets Fibonacci (1, 2, 3, 5, 8, 13).
- **`server/aiarch-state-mcp` is a tracked prebuilt binary.** A bare `go build ./cmd/aiarch-state-mcp` in `server/` silently clobbers it — always use `-o /tmp/...`. If it shows modified, `git checkout --` it.
- **Verify any "pre-existing failure" claim against `origin/main`**, never against branch HEAD — stashing your own changes cannot distinguish a failure introduced by an earlier task on the same branch.
- **Test quality is this workstream's documented weak spot.** Every real defect in Stage 1 was a test that could not fail. For each test ask: *would this go RED if the behaviour it names were removed?* Where a test is load-bearing, prove it by mutation — break the behaviour, observe RED, restore, observe GREEN — and put both outcomes in the report.

---

## File Structure

**Modified (everything folds into existing files — no new `.go` files):**
- `.aiarch/state/project.json` — `.serviceContracts.estimationEngine` (`$defs` + ops), then slots 11–16
- `server/internal/engine/estimation/estimationengine.go` — predicate expansion, move application, coherence assertions
- `server/internal/engine/estimation/engine_test.go` — all tests
- `server/internal/manager/projectdesign/projectdesignmanager.go` — boundary conversion for the new types
- `server/internal/manager/projectdesign/manager_test.go` — boundary + state tests
- `server/internal/engine/estimation/testdata/system_view.json` — only if a fixture component is needed

---

### Task 1: Predicate attachment — contract surface

Adds the `gates` selector to the two additive types. Implementation is a stub; Task 2 fills it in.

**Files:**
- Modify: `.aiarch/state/project.json` → `.serviceContracts.estimationEngine`
- Regenerate: `server/internal/engine/estimation/contract.gen.go`, `fake/fake.gen.go`

**Interfaces:**
- Consumes: the existing `AdditiveActivity`, `AdditiveMilestone`.
- Produces: generated type `ActivitySelector`, and a `gates` field on both additive types. Tasks 2–3 code against these exact names.

- [ ] **Step 1: Add the `ActivitySelector` def**

Hand-edit `.serviceContracts.estimationEngine.$defs`, adding:

```json
{
  "ActivitySelector": {
    "type": "object",
    "properties": {
      "coding": {"type": "boolean"},
      "prefix": {"type": "string"},
      "componentKind": {"type": "string"},
      "provisioning": {"type": "string"}
    },
    "additionalProperties": false
  }
}
```

All four are optional, so `modelgen` emits pointers — `nil` means "this facet is not constrained". A selector with **every** facet nil matches everything and must be rejected at validation time (Task 2), not silently treated as a wildcard.

- [ ] **Step 2: Add `gates` to both additive types**

In `$defs.AdditiveActivity.properties` and `$defs.AdditiveMilestone.properties`, add:

```json
{"gates": {"$ref": "#/$defs/ActivitySelector"}}
```

Leave both `required` arrays unchanged — `gates` is optional, so it generates as `*ActivitySelector` and `nil` means "gates nothing", which is the correct default for the additives that only declare their own predecessors.

- [ ] **Step 3: Validate the state edit**

```bash
cd server && GOWORK=off go build -o /tmp/aiarch-state-mcp ./cmd/aiarch-state-mcp && cd ..
/tmp/aiarch-state-mcp validate --root .
git diff --stat .aiarch/state/project.json
```
Expected: `PASS (… 0 errors)`, contiguous diff, `version` bumped.

- [ ] **Step 4: Regenerate and confirm the shapes**

```bash
cd server && GOWORK=off make gen-models
grep -n "type ActivitySelector" -A 8 internal/engine/estimation/contract.gen.go
grep -n "Gates" internal/engine/estimation/contract.gen.go
```
Expected: `ActivitySelector` with four pointer fields; `Gates *ActivitySelector` on both additive types.

- [ ] **Step 5: Verify gates and commit**

```bash
cd server
GOWORK=off go build ./... && GOWORK=off go test ./internal/engine/estimation/...
GOWORK=off go test ./internal/
GOWORK=off make encapsulation-check
git add . && GOWORK=off make gen-models-check
```
All must pass. Then commit contract + regenerated output together.

---

### Task 2: Predicate expansion

Expands each `gates` selector into `additive → derived` edges. This closes spec conditions C3 and C4.

**Files:**
- Modify: `server/internal/engine/estimation/estimationengine.go`
- Modify: `server/internal/engine/estimation/engine_test.go`

**Interfaces:**
- Consumes: `ActivitySelector`, `Gates` (Task 1); the existing `deriveActivities`, `applyDeltas`.
- Produces: `expandSelector(sel ActivitySelector, acts []DerivedActivity, system SystemView) []string` returning the matching activity names, sorted; wired into `applyDeltas` so gated edges land in `DerivedPlan.Dependencies`.

- [ ] **Step 1: Write the failing tests**

Append to `engine_test.go`:

```go
// A gates selector expands to edges additive -> each MATCHING derived activity. This is
// the channel spec conditions C3 and C4 described and Stage 1 never delivered: an
// AdditiveActivity could declare its own predecessors but could not GATE derived work.
// N-CI before every coding activity is the motivating real case — you cannot build a
// component before CI exists.
func TestGatesSelectorExpandsToCodingActivities(t *testing.T) {
	deltas := ActivityListDeltas{Additive: []AdditiveActivity{{
		Name: "N-CI", Title: "Build/CI pipeline", EffortDays: 5, RiskBucket: 2,
		WorkerClass: "senior-developer",
		Gates:         &ActivitySelector{Coding: boolPtr(true)},
		Justification: "no component can be built before CI exists",
	}}}
	plan := planFor(t, deltas)

	var gated int
	for _, d := range plan.Dependencies {
		for _, p := range d.DependsOn {
			if p == "N-CI" {
				gated++
			}
		}
	}
	var coding int
	for _, a := range plan.Activities {
		if a.Coding && a.Name != "N-CI" {
			coding++
		}
	}
	if coding == 0 {
		t.Fatal("fixture has no coding activities — the test would be vacuous")
	}
	if gated != coding {
		t.Errorf("N-CI gates %d activities, want all %d coding activities", gated, coding)
	}
}

// A selector matching NOTHING is an error, not a silent no-op. A predicate that quietly
// matches zero activities is the vacuity defect that ran through Stage 1's tests, in
// data form — it would look like a working attachment and gate nothing.
func TestGatesSelectorMatchingNothingIsRejected(t *testing.T) {
	a := legalAdditive()
	a.Gates = &ActivitySelector{Prefix: ptrString("Z-NO-SUCH-PREFIX-")}
	mustReject(t, "selector matching zero activities",
		ActivityListDeltas{Additive: []AdditiveActivity{a}})
}

// An entirely empty selector matches everything and is almost certainly an authoring
// mistake; require at least one facet.
func TestGatesSelectorWithNoFacetIsRejected(t *testing.T) {
	a := legalAdditive()
	a.Gates = &ActivitySelector{}
	mustReject(t, "selector with no facet set",
		ActivityListDeltas{Additive: []AdditiveActivity{a}})
}

// Facets AND together.
func TestGatesSelectorFacetsCombine(t *testing.T) {
	sys := edgeSystem()
	acts := deriveActivities(sys)
	got := expandSelector(ActivitySelector{
		ComponentKind: ptrString("resourceAccess"), Coding: boolPtr(true),
	}, acts, sys)
	if len(got) == 0 {
		t.Fatal("fixture has no coding resourceAccess activities — test would be vacuous")
	}
	for _, name := range got {
		if !strings.HasPrefix(name, "C-") {
			t.Errorf("selector matched %q, which is not a coding activity", name)
		}
	}
}

// A gated edge must never point at the additive itself — that is a self-cycle, and the
// CPM solve would not terminate cleanly.
func TestGatesSelectorNeverGatesItself(t *testing.T) {
	deltas := ActivityListDeltas{Additive: []AdditiveActivity{{
		Name: "N-GATE", Title: "gate", EffortDays: 5, RiskBucket: 2,
		WorkerClass: "senior-developer", Coding: true,
		Gates:         &ActivitySelector{Coding: boolPtr(true)},
		Justification: "j",
	}}}
	plan := planFor(t, deltas)
	for _, d := range plan.Dependencies {
		if d.Activity != "N-GATE" {
			continue
		}
		for _, p := range d.DependsOn {
			if p == "N-GATE" {
				t.Error("N-GATE gates itself — self-cycle")
			}
		}
	}
}

// Expansion must be stable: the same input yields the same sorted edge set.
func TestGatesSelectorExpansionIsDeterministic(t *testing.T) {
	sys := edgeSystem()
	acts := deriveActivities(sys)
	sel := ActivitySelector{Coding: boolPtr(true)}
	first := expandSelector(sel, acts, sys)
	for i := 0; i < 10; i++ {
		next := expandSelector(sel, acts, sys)
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("expansion varies across runs: %v vs %v", first, next)
		}
	}
	if !sort.StringsAreSorted(first) {
		t.Errorf("expansion not sorted: %v", first)
	}
}

func boolPtr(b bool) *bool { return &b }
```

> `ptrString` already exists in this file; `boolPtr` may too — check before adding, and do not redeclare.

- [ ] **Step 2: Run them to confirm they fail**

`cd server && GOWORK=off go test ./internal/engine/estimation/ -run TestGatesSelector -v`
Expected: FAIL — `undefined: expandSelector`, and the `Gates` field unused.

- [ ] **Step 3: Implement expansion**

Append to `estimationengine.go`:

```go
// expandSelector returns the names of the derived activities a gates selector matches,
// sorted. Facets AND together; a nil facet does not constrain.
//
// The selector language is deliberately CLOSED — four facets, no expressions. It exists
// so an authored entry can gate derived work (N-CI before every coding activity) without
// enumerating activity names, which would be hand-maintenance that silently goes stale
// the moment a component is added. That staleness is the defect this whole workstream
// removes; re-introducing it in the attachment channel would be self-defeating.
func expandSelector(sel ActivitySelector, acts []DerivedActivity, system SystemView) []string {
	kindOf := make(map[string]string, len(system.Components))
	provOf := make(map[string]string, len(system.Components))
	for _, c := range system.Components {
		kindOf[c.ID] = c.Kind
		provOf[c.ID] = c.Provisioning
	}

	out := make([]string, 0, len(acts))
	for _, a := range acts {
		if sel.Coding != nil && a.Coding != *sel.Coding {
			continue
		}
		if sel.Prefix != nil && !strings.HasPrefix(a.Name, *sel.Prefix) {
			continue
		}
		if sel.ComponentKind != nil && kindOf[a.ComponentID] != *sel.ComponentKind {
			continue
		}
		if sel.Provisioning != nil && provOf[a.ComponentID] != *sel.Provisioning {
			continue
		}
		out = append(out, a.Name)
	}
	sort.Strings(out)
	return out
}

// selectorHasFacet reports whether a selector constrains anything at all. An empty
// selector matches every activity, which is almost always an authoring mistake rather
// than an intent to gate the entire plan.
func selectorHasFacet(sel ActivitySelector) bool {
	return sel.Coding != nil || sel.Prefix != nil ||
		sel.ComponentKind != nil || sel.Provisioning != nil
}
```

Add `"strings"` to the import block if absent.

- [ ] **Step 4: Wire expansion into delta application**

In `applyDeltas`, after every additive is in the index and **before** additive edges are validated, expand each additive's `Gates`:

- reject with `fweng.ContractMisuse` if `!selectorHasFacet(*a.Gates)`;
- expand; reject if the expansion is **empty** — message must say the selector matched zero activities and name the selector's facets;
- for each matched name, append `matched depends on <additive>` — i.e. the additive becomes the **predecessor**, so the edge is added to the *matched activity's* dependency row, never the additive's;
- skip a match equal to the additive's own name (self-gate guard).

Apply the same treatment to `AdditiveMilestone.Gates`, expanding into the milestone's `DependsOn`.

- [ ] **Step 5: Run the tests**

`cd server && GOWORK=off go test ./internal/engine/estimation/ -run TestGatesSelector -v`
Expected: PASS (6 tests).

- [ ] **Step 6: Prove the empty-match rejection is load-bearing**

Temporarily remove the empty-expansion rejection, confirm `TestGatesSelectorMatchingNothingIsRejected` goes RED, restore, confirm GREEN and a byte-identical `estimationengine.go`. Record both outcomes in the report — the point of that guard is to catch a predicate that silently gates nothing, so a test that cannot catch its removal is worthless.

- [ ] **Step 7: Full verification and commit**

```bash
cd server
GOWORK=off go test ./internal/engine/estimation/...
GOWORK=off go test ./internal/
GOWORK=off go test ./...
GOWORK=off golangci-lint run ./internal/engine/estimation/...
GOWORK=off make encapsulation-check
GOWORK=off make derived-plan-check
```
`derived-plan-check` must stay GREEN — no additive is committed yet, so the derived plan is unchanged. Then commit.

---

### Task 3: Re-solve the four options against the 165-day network

Slots 11–16 were computed against the superseded 115-day network and are flagged `staleBasis`. Every duration, cost and risk figure in them is wrong. There is no honest baseline to compress against until this lands.

**Files:**
- Modify: `.aiarch/state/project.json` → slots 11–16
- Modify: `server/internal/manager/projectdesign/manager_test.go`

**Interfaces:**
- Consumes: the committed slot 9/10 plan; the existing `EstimateForOption`, `ComputeNetwork`, and the SDP-assembly code in `assemblesdpreview.go`.
- Produces: refreshed option/risk/SDP slots, and a test asserting they agree with a live re-solve.

- [ ] **Step 1: Record the current figures before changing anything**

Read slots 11–16 and write the existing duration / cost / composite-risk / `included` / `exclusionReason` for all four options into your report. This is the before-state; the change is meaningless without it.

- [ ] **Step 2: Re-solve**

Drive the existing engine over the committed plan for each of the four option shapes (`staffingCap`/`bufferDays`/`criticalSpeedup` are unchanged — only the underlying network moved). Write the refreshed figures into slots 11–14, the refreshed rows into slot 15, and the refreshed narrative figures into slot 16.

**Do not change any option's three scalars.** This task re-solves; it does not re-design. If a refreshed figure looks wrong, report it rather than adjusting an input to make it look right.

- [ ] **Step 3: Clear the staleness that is now genuinely resolved**

Slots 11–16 carry `staleBasis: true` and `staleBasisCause.upstreamRevision: 3`. Having re-solved against network revision 3, clear those flags — that is what they are for. Leave any staleness whose cause you have *not* addressed.

- [ ] **Step 4: Add the standing test**

Append to `manager_test.go` a test that re-solves each option from the committed plan and asserts the stored figures match. This is the same shape as `derived-plan-check`: the options become generated-and-gated rather than hand-maintained, so they cannot silently drift from the network again.

**Prove it bites:** perturb one stored duration, confirm RED, restore, confirm GREEN.

- [ ] **Step 5: Report the recommendation change**

The risk model previously excluded the compressed option at composite 0.797 and recommended `decompressedSolution`. State plainly in your report whether that still holds against the 165-day network, and whether the recommendation changed. **This is the headline result of the task** — do not bury it.

- [ ] **Step 6: Verify and commit**

Validator `0 errors`; `make derived-plan-check` GREEN (slots 9/10 untouched); full suite; layer gates.

---

### Task 4: The `moves[]` contract surface

**Files:**
- Modify: `.aiarch/state/project.json` → `.serviceContracts.estimationEngine`
- Regenerate: `contract.gen.go`, `fake/fake.gen.go`

**Interfaces:**
- Produces: generated types `SimulatorMove`, `DesignFirstMove`, `TopResourcesMove`, `SplitMove`, `CompressionMove`, and a `moves` field on `ProjectOption`. Tasks 5–6 code against these.

- [ ] **Step 1: Add the move `$defs`**

```json
{
  "SimulatorMove": {
    "type": "object",
    "properties": {
      "target": {"type": "string"},
      "dependents": {"type": ["null", "array"], "items": {"type": "string"}},
      "effortDays": {"type": "number"},
      "riskBucket": {"type": "integer"},
      "justification": {"type": "string"}
    },
    "required": ["target", "dependents", "effortDays", "riskBucket", "justification"],
    "additionalProperties": false
  },
  "DesignFirstMove": {
    "type": "object",
    "properties": {
      "target": {"type": "string"},
      "designEffortDays": {"type": "number"},
      "justification": {"type": "string"}
    },
    "required": ["target", "designEffortDays", "justification"],
    "additionalProperties": false
  },
  "TopResourcesMove": {
    "type": "object",
    "properties": {
      "speedup": {"type": "number"},
      "targets": {"type": ["null", "array"], "items": {"type": "string"}},
      "justification": {"type": "string"}
    },
    "required": ["speedup", "justification"],
    "additionalProperties": false
  },
  "SplitMove": {
    "type": "object",
    "properties": {
      "target": {"type": "string"},
      "parts": {"type": "integer"},
      "justification": {"type": "string"}
    },
    "required": ["target", "parts", "justification"],
    "additionalProperties": false
  },
  "CompressionMove": {
    "type": "object",
    "properties": {
      "simulator": {"$ref": "#/$defs/SimulatorMove"},
      "designFirst": {"$ref": "#/$defs/DesignFirstMove"},
      "topResources": {"$ref": "#/$defs/TopResourcesMove"},
      "split": {"$ref": "#/$defs/SplitMove"}
    },
    "additionalProperties": false
  }
}
```

`CompressionMove` is a tagged union by presence: exactly one facet non-nil. All four are optional, so they generate as pointers — that is what makes "exactly one" checkable at runtime (Task 5).

- [ ] **Step 2: Add `moves` to `ProjectOption`**

Add `{"moves": {"type": ["null", "array"], "items": {"$ref": "#/$defs/CompressionMove"}}}` to `$defs.ProjectOption.properties`, leaving `required` unchanged.

- [ ] **Step 3: Validate, regenerate, verify, commit** — as Task 1 Steps 3–5.

---

### Task 5: Apply `simulator` and `designFirst`

The two moves that mutate the graph. `topResources` and `split` follow in Task 7.

**Files:**
- Modify: `server/internal/engine/estimation/estimationengine.go`
- Modify: `server/internal/engine/estimation/engine_test.go`

**Interfaces:**
- Consumes: `CompressionMove` and friends (Task 4); the materialized `DerivedPlan`.
- Produces: `applyMoves(plan DerivedPlan, moves []CompressionMove) (DerivedPlan, error)`. Task 6 asserts coherence over its output; Task 8's search calls it.

- [ ] **Step 1: Write the failing tests**

Append to `engine_test.go`:

```go
// A simulator breaks a dependency edge deliberately: the named dependents build against
// S-<target> instead of waiting for C-<target>. This is the one thing the PLAN's
// vocabulary forbids and an OPTION's requires — the architecture still says the
// dependency exists; the schedule defers it and repays it at integration.
func TestSimulatorMoveRedirectsDependents(t *testing.T) {
	base := planFor(t, ActivityListDeltas{})
	moved, err := applyMoves(base, []CompressionMove{{Simulator: &SimulatorMove{
		Target:     "C-order-manager",
		Dependents: []string{"C-pricing-engine"},
		EffortDays: 5, RiskBucket: 2,
		Justification: "an order-manager fake is a thin stub; pricing can build against it",
	}}})
	if err != nil {
		t.Fatalf("applyMoves: %v", err)
	}
	if _, ok := activityNamed(moved, "S-C-order-manager"); !ok {
		t.Fatal("simulator activity not inserted")
	}
	deps := depsByActivity(moved.Dependencies)
	for _, p := range deps["C-pricing-engine"] {
		if p == "C-order-manager" {
			t.Error("dependent still waits on the real target; the edge was not redirected")
		}
	}
	var onSim bool
	for _, p := range deps["C-pricing-engine"] {
		if p == "S-C-order-manager" {
			onSim = true
		}
	}
	if !onSim {
		t.Error("dependent does not depend on the simulator")
	}
}

// A simulator must never be free: the deferred dependency is repaid downstream. A move
// that eliminates its own integration debt is compression by accounting fraud.
func TestSimulatorMoveKeepsIntegrationDebt(t *testing.T) {
	base := planFor(t, ActivityListDeltas{})
	moved, err := applyMoves(base, []CompressionMove{{Simulator: &SimulatorMove{
		Target: "C-order-manager", Dependents: []string{"C-pricing-engine"},
		EffortDays: 5, RiskBucket: 2, Justification: "j",
	}}})
	if err != nil {
		t.Fatalf("applyMoves: %v", err)
	}
	if _, ok := activityNamed(moved, "C-order-manager"); !ok {
		t.Error("the real target was removed; a simulator defers a dependency, never deletes it")
	}
	deps := depsByActivity(moved.Dependencies)
	var repaid bool
	for _, p := range deps["N-IT"] {
		if p == "C-order-manager" {
			repaid = true
		}
	}
	if !repaid {
		t.Error("integration debt not repaid: nothing downstream waits for the real target")
	}
}

// designFirst splits one activity in two so dependents can start against the frozen
// contract. This is the ONLY legitimate source of a D-* activity — it never belongs in
// the base list.
func TestDesignFirstMoveSplitsTheActivity(t *testing.T) {
	base := planFor(t, ActivityListDeltas{})
	moved, err := applyMoves(base, []CompressionMove{{DesignFirst: &DesignFirstMove{
		Target: "C-order-manager", DesignEffortDays: 5,
		Justification: "the contract is small; freezing it early unblocks two dependents",
	}}})
	if err != nil {
		t.Fatalf("applyMoves: %v", err)
	}
	d, ok := activityNamed(moved, "D-C-order-manager")
	if !ok {
		t.Fatal("design activity not created")
	}
	if d.EffortDays != 5 {
		t.Errorf("design effort = %v, want 5", d.EffortDays)
	}
	c, ok := activityNamed(moved, "C-order-manager")
	if !ok {
		t.Fatal("build activity missing after split")
	}
	if c.EffortDays != base.mustEffort(t, "C-order-manager")-5 {
		t.Errorf("build effort %v does not account for the extracted design effort", c.EffortDays)
	}
	deps := depsByActivity(moved.Dependencies)
	var buildOnDesign bool
	for _, p := range deps["C-order-manager"] {
		if p == "D-C-order-manager" {
			buildOnDesign = true
		}
	}
	if !buildOnDesign {
		t.Error("build does not depend on its own design activity")
	}
}

// A move naming an activity that is not in the plan is rejected — a typo must not
// silently produce a no-op compression that still claims a duration saving.
func TestMoveOnUnknownActivityIsRejected(t *testing.T) {
	base := planFor(t, ActivityListDeltas{})
	_, err := applyMoves(base, []CompressionMove{{Simulator: &SimulatorMove{
		Target: "C-does-not-exist", Dependents: []string{"C-pricing-engine"},
		EffortDays: 5, RiskBucket: 2, Justification: "j",
	}}})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for a move on an unknown activity, got %v", err)
	}
}

// The union carries exactly one facet.
func TestMoveWithZeroOrTwoFacetsIsRejected(t *testing.T) {
	base := planFor(t, ActivityListDeltas{})
	for _, m := range []CompressionMove{
		{},
		{Simulator: &SimulatorMove{Target: "C-order-manager", Dependents: []string{"C-pricing-engine"}, EffortDays: 5, RiskBucket: 2, Justification: "j"},
			DesignFirst: &DesignFirstMove{Target: "C-order-manager", DesignEffortDays: 5, Justification: "j"}},
	} {
		if _, err := applyMoves(base, []CompressionMove{m}); err == nil {
			t.Error("a move with zero or two facets must be rejected")
		}
	}
}

// Every move carries a written justification, same rule as an override.
func TestMoveWithoutJustificationIsRejected(t *testing.T) {
	base := planFor(t, ActivityListDeltas{})
	_, err := applyMoves(base, []CompressionMove{{Simulator: &SimulatorMove{
		Target: "C-order-manager", Dependents: []string{"C-pricing-engine"},
		EffortDays: 5, RiskBucket: 2,
	}}})
	if err == nil {
		t.Error("a move without a justification must be rejected")
	}
}
```

> `mustEffort` is a small test helper you add alongside: it looks up an activity's effort in a plan and calls `t.Fatalf` when absent.

- [ ] **Step 2: Run them to confirm they fail** — `undefined: applyMoves`.

- [ ] **Step 3: Implement `applyMoves`**

Semantics, in order:

1. Validate every move: exactly one facet; a justification; the target exists in the plan; every named dependent exists and actually depends on the target today (a dependent that does not is an authoring error, not a silent skip).
2. `simulator`: append `S-<target>` (worker class `junior-developer`, the supplied effort/risk); for each dependent, replace `target` with `S-<target>` in its dependency row; append `target` to the terminal gate's predecessors so the deferred dependency is repaid.
3. `designFirst`: append `D-<target>` with `designEffortDays`; reduce `target`'s effort by the same amount; move every dependent's edge from `target` to `D-<target>`; make `target` depend on `D-<target>`.
4. Return a new `DerivedPlan` — **never mutate the input**. Sort activities and dependency rows before returning.

**`gocyclo` will very likely trip on a single `applyMoves`** — measure first, then extract one helper per move kind (`applySimulator`, `applyDesignFirst`) and report the measured number and whether it genuinely tripped.

- [ ] **Step 4: Run the tests** — expected PASS (6 tests).

- [ ] **Step 5: Prove the integration-debt guard is load-bearing**

Temporarily remove the "repay at the terminal gate" step, confirm `TestSimulatorMoveKeepsIntegrationDebt` goes RED, restore, confirm GREEN and byte-identical production code. Both outcomes in the report.

- [ ] **Step 6: Verify and commit** — package tests, `./internal/`, full suite, lint, encapsulation, `derived-plan-check` GREEN (slot 9/10 untouched).

---

### Task 6: The schedule-coherence invariant

Stage 1's C1 defect: a derived network passed every consistency check while asserting the system was fully tested 50 days before the managers it tests existed. Compression rewires far more aggressively, so every option's network is **solved and asserted**.

**Files:**
- Modify: `server/internal/engine/estimation/estimationengine.go`
- Modify: `server/internal/engine/estimation/engine_test.go`

**Interfaces:**
- Consumes: `applyMoves` (Task 5), the existing `ComputeNetwork`.
- Produces: `assertScheduleCoherent(plan DerivedPlan, sol NetworkSolution) error` — the four invariants. Task 8's loop calls it after every candidate move.

- [ ] **Step 1: Write the failing tests**

Four tests, one per invariant, each constructing a plan that violates exactly that invariant and asserting rejection:

1. **Terminal-gate ordering** — the system-testing gate must finish after every activity it gates. Build a plan where the gate has no edge to a manager and assert rejection.
2. **Node completeness** — solved node count equals activities + milestones. Build a plan with an activity carrying no incident edge and assert rejection. *(This is exactly Stage 1's C2: three activities were invisible to the solve because the node universe is built from edges, not from the activity list.)*
3. **Integration debt** — every `S-*` has a downstream activity depending on the target it replaced.
4. **Deferral, not inversion** — if the architecture says `A` depends on `B`, no option may finish `A` before `B` *unless* a simulator stands in for `B`.

Plus a positive test: the unmodified derived plan satisfies all four.

- [ ] **Step 2: Run them to confirm they fail.**

- [ ] **Step 3: Implement `assertScheduleCoherent`** — pure, returns `fweng.ContractMisuse` naming which invariant failed and for which activity. Each message must be actionable: which activity, which invariant, what the solved times were.

- [ ] **Step 4: Run the tests** — expected PASS (5 tests).

- [ ] **Step 5: Prove each invariant is load-bearing.** Four mutations, one per invariant — remove the check, observe RED, restore, observe GREEN. Report all four. An unfalsifiable coherence assertion is worse than none, because it manufactures confidence in exactly the place Stage 1 got burned.

- [ ] **Step 6: Verify and commit.**

---

### Task 7: `topResources` and `split`

**Files:** `estimationengine.go`, `engine_test.go`

- [ ] **Step 1: Tests.** `topResources` reduces the effort of its targets by the speedup factor and is **rejected above 1.5** (the skill's stated range; the committed 1.8 exceeded it). `split` divides one activity into `parts` parallel activities whose combined effort is conserved. Both must satisfy the Task-6 invariants.
- [ ] **Step 2: Run to confirm failure.**
- [ ] **Step 3: Implement**, folding `criticalSpeedup` into `topResources` — retain the `ProjectOption.CriticalSpeedup` field for now so existing option slots keep solving; a move and the scalar must not both apply to the same activity (reject if they do).
- [ ] **Step 4: Run tests. Step 5: Verify and commit.**

---

### Task 8: Candidate search, agent pricing, and the greedy loop

**Files:** `estimationengine.go`, `engine_test.go`, `projectdesignmanager.go`, `manager_test.go`

- [ ] **Step 1: Tests.** `candidateSites(plan, sol)` returns critical-path activities ranked by dependent fan-out, deterministically. The loop halts at each slot-15 threshold (`maxCompressionPct` 0.3, `tooRiskyThreshold` 0.75, `overSafeThreshold` 0.3) and at App C §4.6's caps, and never returns a plan failing `assertScheduleCoherent`. Include a test that a move priced at or above its target's own effort is rejected by the search — an expensive simulator must lose on its merits, with no special rule.
- [ ] **Step 2: Run to confirm failure.**
- [ ] **Step 3: Implement.** Code ranks sites and runs the loop; the **agent prices each candidate** and may veto with a justification. Pricing justifications follow the Stage 1 rule now in doctrine: **ground in a property, not a superlative** — every false justification Stage 1 produced was a superlative, each falsified by one counterexample.
- [ ] **Step 4: Run tests. Step 5: Verify and commit.**

---

### Task 9: Surface applied moves in the SDP review

Directive 7 requires management see *what* the compression buys, not a multiplier.

**Files:** `.aiarch/state/project.json` (slot 16), `projectdesignmanager.go`, `manager_test.go`

- [ ] **Step 1: Test** that `.sdpReview` carries, per option, the applied move list with each move's kind, target, cost and justification — and that an option with moves cannot render without them.
- [ ] **Step 2: Run to confirm failure. Step 3: Implement. Step 4: Run tests.**
- [ ] **Step 5: Re-solve and re-write slots 13 and 15–16** with real compression applied, and **report the new recommendation**. If a genuine compressed option now clears the 0.75 risk ceiling, that is the result this entire two-stage workstream was for — state it plainly with the numbers.
- [ ] **Step 6: Verify and commit.** Validator `0 errors`; `derived-plan-check` GREEN.

---

### Task 10: Doctrine

**Files:** `/Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets/assets/claude/skills/the-method-compressed-solution/SKILL.md` (and `the-method-activity-list/SKILL.md` for the `gates` selector)

The `.claude` tree in this repo is **gitignored and materialized** from `method-assets`; edits there do not survive `make claude-assets`. The real source is the platform repo.

- [ ] **Step 1:** Record the closed move catalogue, the plan-vs-option distinction (derived edges immutable in the plan, mutable in an option), the four coherence invariants, and the `gates` predicate.
- [ ] **Step 2:** Record that `criticalSpeedup` alone was a defect — a scalar that changes the schedule while asserting nothing about how — and that the compressed option it produced was excluded by the risk model as too risky.
- [ ] **Step 3:** Commit in `archistrator-platform`, tag `method-assets/v0.5.0`, push both, bump the pin in `server/go.mod`, re-run `make claude-assets`, and **verify the doctrine survives re-materialization**.
- [ ] **Step 4:** Commit the pin bump.

---

## Verification checklist

```bash
cd server
GOWORK=off go test ./...                 # full suite
GOWORK=off go test ./internal/           # LAYER GATES — package-scoped runs miss these
GOWORK=off make gen-models-check         # codegen drift (git add first)
GOWORK=off make encapsulation-check      # engine purity
GOWORK=off make method-check             # Method design gate
GOWORK=off make lint                     # linters
GOWORK=off make derived-plan-check       # the plan still matches the architecture
cd .. && /tmp/aiarch-state-mcp validate --root .   # 0 errors
```

## Out of scope

- **I4 — the enforcement-point regression** carried from Stage 1: coverage checking used to fire in the MCP `putDraftModel` write path; the drift gate is a Go test run by CI. Needs a founder ruling, not a fix here.
- Re-designing the four option shapes. Task 3 re-solves them against the corrected network; it does not change any option's staffing, buffer or speedup inputs.
