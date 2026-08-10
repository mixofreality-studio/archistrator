# Derived Activity List & Network — Stage 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Phase-2 activity list and network a deterministic derivation from the committed System, with agents authoring only justified deltas.

**Architecture:** A new `DerivePlan` operation on the existing `estimationEngine` (Engine layer — pure, no I/O) turns a slim `SystemView` plus an `ActivityListDeltas` document into a materialized `DerivedPlan` (activities + dependency edges + milestones). `projectDesignManager` converts `projectstate` types to the Engine's own types at the call boundary (Option B encapsulation, exactly as `toEstimationOption` already does). Slots 9 and 10 stop storing materialized lists and store deltas; readers get the materialized plan via render-on-read. The derivation is built and proven at parity against the live committed list **before** any storage changes, so the system stays green at every commit.

**Tech Stack:** Go 1.26, `GOWORK=off`, schema-first codegen (`cmd/modelgen` reads `.serviceContracts` in `project.json` and emits `contract.gen.go` + fakes), `framework-go/engine` (`fweng`) error model, `aiarch-state` MCP tools for all state writes.

**Spec:** `docs/superpowers/specs/2026-08-09-derived-activity-list-design.md` (commit `b519025`)

## Global Constraints

- **Stage 1 only.** Part 2 of the spec (compression moves) gets its own plan. `criticalSpeedup` must **NOT** be removed or altered in this plan — it is the only compression lever until the move catalog lands.
- **All Go commands run from `server/` with `GOWORK=off`.** Test: `GOWORK=off go test ./...`. Regen: `GOWORK=off make gen-models`.
- **State edits are hand-edits, gated by `validate`.** The `aiarch-state` MCP server is a **CI-only** rail (wired via `--mcp-config` in `.github/workflows/aiarch-*.yml`, rooted by the `AIARCH_STATE_ROOT` env var); it is not registered in local sessions and its write tools are unavailable here. Local structural changes are hand-edits plus a reconciliation commit — the precedent the finish-construction wave set.

  **Every task that touches `.aiarch/state/project.json` MUST run the validator afterward** and treat a non-zero error count as a failure:

  ```bash
  cd server && GOWORK=off go build -o /tmp/aiarch-state-mcp ./cmd/aiarch-state-mcp
  cd .. && /tmp/aiarch-state-mcp validate --root .
  ```

  Expected tail: `aiarch-state validate: PASS (<N> advisory finding(s), 0 errors)`. This runs the **same** rule set `putDraftModel` applies in-loop — framework methodcheck plus the app-side ACT-*/PA-*/DH-* tiers — so the gate is not weakened by editing directly. **Baseline before this plan: 55 advisory findings, 0 errors.** Advisory count may move as ACT-* warnings clear; the error count must stay 0.

- **Editing `project.json` without reformatting it.** It is one ~921 KB / 28 k-line JSON document, and this recipe round-trips **byte-identically** (verified 2026-08-09) — anything else produces a diff of tens of thousands of spurious lines:

  ```python
  import json
  d = json.load(open('.aiarch/state/project.json'))
  # ... mutate d ...
  json.dump(d, open('.aiarch/state/project.json', 'w'), indent=2, ensure_ascii=False)
  open('.aiarch/state/project.json', 'a').write('\n')   # trailing newline
  ```

  Do **not** pass `sort_keys=True` and do **not** use `indent=1`; both rewrite the whole file. After any state edit, confirm the diff is scoped:

  ```bash
  git diff --stat .aiarch/state/project.json
  ```

  The test is **proportionality, not an absolute line count**: the diff should contain only your intended insertions plus the `version` bump, in contiguous hunks, with no stray reindentation elsewhere in the file. A genuinely large addition legitimately produces a large diff — 9 new `$defs` pretty-printed at 2-space indent came to 317 lines in Task 1, which was correct. What indicates a formatting accident is a diff touching regions you did not edit, or one approaching the file's full 28,334 lines. If you see that, revert with `git checkout -- .aiarch/state/project.json` and redo with the recipe above.

  Also bump the document's top-level `version` integer — the rail increments it on every write. (595 before Task 1; each state-editing task bumps it again.)
- **`contract.gen.go` and `fake/fake.gen.go` are generated. Never hand-edit them.** Change the contract in `project.json` `.serviceContracts`, then run `make gen-models`.
- **Engine purity is a hard gate.** `internal/engine/estimation` must import ONLY `fweng` and stdlib. No `projectstate` import, no I/O, no `time.Now()`, no `math/rand`, no goroutines, no global mutable state. `make encapsulation-check` and `make method-check` must stay green.
- **Worker classes are a fixed roster**, spelled exactly: `system-architect`, `product-manager`, `project-manager`, `senior-developer`, `junior-developer`, `ui-designer`, `ux-reviewer`, `qa-engineer`, `test-engineer`, `software-tester`.
- **Lint rules that bite this work** (`.golangci.yml`, converged by managed-scaffold sync — **never edit it to make a finding go away**):
  - `revive` flags **unused parameters**. Do not carry a parameter "for symmetry" or future use; drop it. The plan's signatures are already minimal for this reason.
  - `gocyclo` caps cyclomatic complexity at **15**, on production **and test** files. `deriveActivities` and `deriveDependencies` are the two functions most at risk. If either trips the cap, extract a named helper per emission rule (e.g. `codingActivityFor`, `provisioningActivityFor`, `spaActivitiesFor`) rather than restructuring the logic — the rules are meant to read one-to-one against the derivation table.
  - `gosec` is disabled for `_test.go` only; `testdata` reads in non-test files would trip it.
- **Effort quantum:** every derived `effortDays` is a multiple of 5 and ≤ 35. **Risk buckets** are Fibonacci: 1, 2, 3, 5, 8, 13.
- **Activity ID prefixes are load-bearing** (downstream classifiers key on them): `C-` per-component coding, `U-SPA*` SPA construction, `G-` UI-design, `I-` integration, `R-` resource provisioning, `N-` noncoding, `S-` simulator (stage 2), `D-` design-first (stage 2 only, never in the base list).
- **Commit after every task.** End commit messages with `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.

---

## File Structure

> **BINDING — the layer file-layout standard** (`docs/superpowers/specs/2026-07-11-layer-file-layout-standard-design.md`, enforced by `TestFileLayout` in `server/internal/arch_test.go`, **live with zero waivers**). A leaf layer package may contain ONLY:
>
> - **Rule 1** — one handwritten contract-implementation file: `<pkg>engine.go` / `<pkg>manager.go` / `<pkg>access.go`. It holds *all* contract methods plus everything shared — strategy tables, behaviour free-functions, codecs, adapters. **Large files are an accepted consequence** and explicitly sanctioned by the standard.
> - **Rule 2** — one file per Temporal workflow (**managers only**; not applicable to Engines or ResourceAccess).
> - **Rule 3** — one handwritten test file: `engine_test.go` / `manager_test.go` / `access_test.go`.
> - **Rule 4** — no other handwritten `.go` files. Generated `*.gen.go`, `contract.schema.json`, and non-Go assets (including `testdata/`) are exempt.
>
> This plan originally specified per-concern files (`derive.go`, `derive_edges.go`, `deriveplan.go`, `activityalias.go`, …), every one of which violates Rule 4. **Do not create them.** The rule is machine-checked; `make lint` and the estimation-package test run will NOT catch it, because the gate lives in `internal/arch_test.go` — only `GOWORK=off go test ./internal/` or a full `./...` run fires it.

**Created:**
- `server/internal/engine/estimation/testdata/system_view.json` — frozen slim `SystemView` snapshot of the live slot 5. (`testdata/` is exempt from the layout rule.)

**Modified (all new code folds into these existing files — Rules 1 and 3):**
- `server/internal/engine/estimation/estimationengine.go` — **all** derivation code: activity emission, worker-class/effort/risk tables, edge derivation and transitive reduction, milestone derivation, delta validation and application, and `DerivePlan` itself.
- `server/internal/engine/estimation/engine_test.go` — **all** estimation tests, appended to the existing CPM tests.
- `server/internal/manager/projectdesign/projectdesignmanager.go` — the `projectstate` → `estimation` conversion boundary and the materialize-on-read entry point.
- `server/internal/manager/projectdesign/manager_test.go` — conversion tests.
- `server/internal/resourceaccess/projectstate/projectstateaccess.go` — historical short-name → derived canonical id alias map.
- `server/internal/resourceaccess/projectstate/access_test.go` — alias round-trip test.

**Modified:**
- `.aiarch/state/project.json` `.serviceContracts.estimationEngine` — new `$defs` + `DerivePlan` op (Task 1).
- `.aiarch/state/project.json` `.serviceContracts.projectStateAccess.$defs.Component` — `constructionProfile`, `provisioning`, `uiSurface` (Task 7).
- `server/internal/engine/estimation/contract.gen.go`, `fake/fake.gen.go` — regenerated, never hand-edited.
- `server/internal/resourceaccess/projectstate/contract.gen.go` — regenerated (Task 7).
- `server/internal/resourceaccess/projectstate/projectstateaccess.go` — `ActivityListDeltas` model + slot read path (Tasks 5, 10).
- `server/cmd/aiarch-state-mcp/crossartifact.go`, `staleness.go` — ACT-* deletions (Task 11).
- `.claude/skills/the-method-activity-list/SKILL.md` — doctrine update (Task 12).

---

### Task 1: Extend the estimationEngine contract with the derivation surface

Adds the Engine's own types and the `DerivePlan` operation, then regenerates. The implementation is a stub returning an empty plan; Tasks 2–6 fill it in. This task exists separately because the generated types are the vocabulary every later task speaks.

**Files:**
- Modify: `.aiarch/state/project.json` → `.serviceContracts.estimationEngine` (hand-edit per the Global Constraints recipe, then `validate`)
- Regenerate: `server/internal/engine/estimation/contract.gen.go`, `server/internal/engine/estimation/fake/fake.gen.go`
- Create: `server/internal/engine/estimation/derive.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: the generated types `SystemComponent`, `SystemRelationship`, `SystemView`, `ActivityOverride`, `AdditiveActivity`, `AdditiveMilestone`, `ActivityListDeltas`, `DerivedActivity`, `DerivedPlan`, and the method `EstimationEngineImpl.DerivePlan(fweng.Context, SystemView, ActivityListDeltas) (DerivedPlan, error)`. Every later task uses these exact names.

`AdditiveActivity` carries a `componentId` field **that is always rejected** (Task 5). It exists so a component-bound additive fails with an explanatory contract-misuse error naming the reason, rather than as an opaque schema decode failure — the author needs to be told *why* the vocabulary forbids it.

**The two `componentId` fields are deliberately asymmetric — do not "harmonize" them:**

| Field | Shape | Why |
|---|---|---|
| `DerivedActivity.ComponentID` | **required** → plain `string` | Engine *output* that this code constructs. Empty means "no component", exactly as the canonical `projectstate.ActivityItem.ComponentID` (a plain `string`) already models it. A pointer would force nil-handling across Tasks 3, 4, 5 and 8 for no semantic gain. |
| `AdditiveActivity.ComponentID` | **optional** → `*string` | Author *input*, and the always-rejected field. nil-vs-supplied is load-bearing: a caller passing `"componentId": ""` must still be rejected, and with a plain `string` that is indistinguishable from absent and would slip through the guard. |

- [ ] **Step 1: Read the existing contract entry**

Run:
```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
python3 -c "
import json; d=json.load(open('.aiarch/state/project.json'))
print(json.dumps(d['serviceContracts']['estimationEngine'], indent=1))" > /tmp/estimation-contract-before.json
wc -l /tmp/estimation-contract-before.json
```

Note the shape: `component`, `layer`, `goPackage`, `title`, `$defs`, `interface`. You will add to `$defs` and append one operation to `interface.operations`.

- [ ] **Step 2: Add the new `$defs`**

Hand-edit `.serviceContracts.estimationEngine.$defs` with these definitions **added** (keep every existing def unchanged), using the byte-identical round-trip recipe from the Global Constraints:

```json
{
  "SystemComponent": {
    "type": "object",
    "properties": {
      "id": {"type": "string", "x-go-name": "ID"},
      "name": {"type": "string"},
      "kind": {"type": "string"},
      "constructionProfile": {"type": "string"},
      "provisioning": {"type": "string"},
      "uiSurface": {"type": "boolean"}
    },
    "required": ["id", "name", "kind", "constructionProfile", "provisioning", "uiSurface"],
    "additionalProperties": false
  },
  "SystemRelationship": {
    "type": "object",
    "properties": {
      "from": {"type": "string"},
      "to": {"type": "string"}
    },
    "required": ["from", "to"],
    "additionalProperties": false
  },
  "SystemView": {
    "type": "object",
    "properties": {
      "components": {"type": ["null", "array"], "items": {"$ref": "#/$defs/SystemComponent"}},
      "relationships": {"type": ["null", "array"], "items": {"$ref": "#/$defs/SystemRelationship"}},
      "coreUseCaseIds": {"type": ["null", "array"], "items": {"type": "string"}, "x-go-name": "CoreUseCaseIDs"}
    },
    "required": ["components"],
    "additionalProperties": false
  },
  "ActivityOverride": {
    "type": "object",
    "properties": {
      "activity": {"type": "string"},
      "effortDays": {"type": ["null", "number"]},
      "riskBucket": {"type": ["null", "integer"]},
      "justification": {"type": "string"}
    },
    "required": ["activity", "justification"],
    "additionalProperties": false
  },
  "AdditiveActivity": {
    "type": "object",
    "properties": {
      "name": {"type": "string"},
      "title": {"type": "string"},
      "effortDays": {"type": "number"},
      "riskBucket": {"type": "integer"},
      "workerClass": {"type": "string"},
      "coding": {"type": "boolean"},
      "dependsOn": {"type": ["null", "array"], "items": {"type": "string"}},
      "componentId": {"type": "string", "x-go-name": "ComponentID"},
      "justification": {"type": "string"}
    },
    "required": ["name", "title", "effortDays", "riskBucket", "workerClass", "coding", "justification"],
    "additionalProperties": false
  },
  "AdditiveMilestone": {
    "type": "object",
    "properties": {
      "id": {"type": "string"},
      "dependsOn": {"type": ["null", "array"], "items": {"type": "string"}},
      "justification": {"type": "string"}
    },
    "required": ["id", "justification"],
    "additionalProperties": false
  },
  "ActivityListDeltas": {
    "type": "object",
    "properties": {
      "overrides": {"type": ["null", "array"], "items": {"$ref": "#/$defs/ActivityOverride"}},
      "additive": {"type": ["null", "array"], "items": {"$ref": "#/$defs/AdditiveActivity"}},
      "additiveMilestones": {"type": ["null", "array"], "items": {"$ref": "#/$defs/AdditiveMilestone"}}
    },
    "additionalProperties": false
  },
  "DerivedActivity": {
    "type": "object",
    "properties": {
      "name": {"type": "string"},
      "title": {"type": "string"},
      "effortDays": {"type": "number"},
      "riskBucket": {"type": "integer"},
      "workerClass": {"type": "string"},
      "coding": {"type": "boolean"},
      "componentId": {"type": "string", "x-go-name": "ComponentID"},
      "derived": {"type": "boolean"}
    },
    "required": ["name", "title", "effortDays", "riskBucket", "workerClass", "coding", "componentId", "derived"],
    "additionalProperties": false
  },
  "DerivedPlan": {
    "type": "object",
    "properties": {
      "activities": {"type": ["null", "array"], "items": {"$ref": "#/$defs/DerivedActivity"}},
      "dependencies": {"type": ["null", "array"], "items": {"$ref": "#/$defs/NetworkDependency"}},
      "milestones": {"type": ["null", "array"], "items": {"$ref": "#/$defs/NetworkMilestone"}}
    },
    "required": ["activities", "dependencies"],
    "additionalProperties": false
  }
}
```

Note: `NetworkDependency` and `NetworkMilestone` already exist in this contract's `$defs` — reuse them, do not redefine.

- [ ] **Step 3: Append the `DerivePlan` operation**

Append to `interface.operations` (the contract goes from 3 to 4 operations — within App B's 3–5 guidance):

```json
{
  "name": "DerivePlan",
  "params": [
    {"name": "system", "schema": {"$ref": "#/$defs/SystemView"}},
    {"name": "deltas", "schema": {"$ref": "#/$defs/ActivityListDeltas"}}
  ],
  "result": {"$ref": "#/$defs/DerivedPlan"},
  "error": true
}
```

- [ ] **Step 3b: Validate the state edit**

Run:
```bash
cd server && GOWORK=off go build -o /tmp/aiarch-state-mcp ./cmd/aiarch-state-mcp && cd ..
/tmp/aiarch-state-mcp validate --root .
git diff --stat .aiarch/state/project.json
```
Expected: `PASS (… 0 errors)`, and a diff of tens of lines, not thousands.

- [ ] **Step 4: Regenerate**

Run:
```bash
cd server && GOWORK=off make gen-models
```
Expected: `contract.gen.go` and `fake/fake.gen.go` both change. Confirm the new types exist:
```bash
grep -n "type SystemView\|type DerivedPlan\|type ActivityListDeltas\|DerivePlan" internal/engine/estimation/contract.gen.go
```
Expected: all four appear.

- [ ] **Step 5: Write the stub implementation**

Create `server/internal/engine/estimation/derive.go`:

```go
// Package-local file: derive.go implements DerivePlan — the deterministic Phase-2
// derivation of the activity list and network from the committed System (Löwy ch. 11;
// Fig 11-4 → Fig 11-5 is literally a transitive reduction over the component dependency
// chart). The activity inventory is NOT authored: it falls out of the architecture, and
// the only authored input is the ActivityListDeltas document (justified effort/risk
// overrides plus genuinely componentless additive activities).
//
// Purity, as for every op on this Engine: no I/O, no clock, no randomness, no globals.
// Identical inputs → identical DerivedPlan, always. All iteration over maps is sorted.
package estimation

import (
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// DerivePlan derives the full activity list and network from the committed System and
// applies the authored deltas.
//
// An EMPTY system is a normal DOMAIN result (an empty plan) — a project may be read
// before its architecture is committed. The *fweng.Error channel is reserved for
// contract misuse: a delta that the vocabulary forbids (an override naming no derived
// activity, an additive carrying a componentId, a missing justification).
func (EstimationEngineImpl) DerivePlan(_ fweng.Context, system SystemView, deltas ActivityListDeltas) (DerivedPlan, error) {
	if len(system.Components) == 0 {
		return DerivedPlan{Activities: nil, Dependencies: nil, Milestones: nil}, nil
	}
	return DerivedPlan{Activities: nil, Dependencies: nil, Milestones: nil}, nil
}
```

- [ ] **Step 6: Verify it compiles and the gates hold**

Run:
```bash
cd server && GOWORK=off go build ./... && GOWORK=off go test ./internal/engine/estimation/... && GOWORK=off make encapsulation-check
```
Expected: build OK, existing estimation tests PASS, encapsulation check PASS.

- [ ] **Step 7: Verify the codegen drift gate is clean**

Run:
```bash
cd server && GOWORK=off make gen-models-check
```
Expected: no diff (you already regenerated and committed the output in the same change).

- [ ] **Step 8: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add .aiarch/state/project.json server/internal/engine/estimation/
git commit -m "$(cat <<'EOF'
feat(estimation): add DerivePlan contract surface

Adds SystemView / ActivityListDeltas / DerivedPlan defs and the DerivePlan
operation to the estimationEngine contract. Implementation is a stub; the
derivation lands in the following tasks.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Worker-class, effort, and risk default tables

Three pure lookup tables. They are separated from the emission logic (Task 3) because they encode Method doctrine that gets tuned independently of which activities exist.

**Files:**
- Modify: `server/internal/engine/estimation/derive.go`
- Create: `server/internal/engine/estimation/derive_test.go`

**Interfaces:**
- Consumes: `SystemComponent` (Task 1).
- Produces: `workerClassFor(prefix string) string`, `noncodingInventoryClass(name string) string`, `defaultEffortFor(kind string) float64`, `defaultRiskFor(effortDays float64) int64`. Tasks 3 and 5 call all four.

- [ ] **Step 1: Write the failing tests**

Create `server/internal/engine/estimation/derive_test.go`:

```go
package estimation

import "testing"

func TestWorkerClassFor(t *testing.T) {
	cases := []struct {
		prefix, want string
	}{
		{"C", "junior-developer"},
		{"U", "junior-developer"},
		{"R", "senior-developer"},
		{"I", "senior-developer"},
		{"G", "ui-designer"},
	}
	for _, c := range cases {
		if got := workerClassFor(c.prefix); got != c.want {
			t.Errorf("workerClassFor(%q) = %q, want %q", c.prefix, got, c.want)
		}
	}
}

// The always-emit noncoding inventory has FIXED worker classes per Löwy ch. 9's three
// distinct quality roles — test engineer (builds harnesses), software tester (runs
// system testing), QA engineer (process). They must never collapse into one class.
func TestWorkerClassForNoncodingInventory(t *testing.T) {
	cases := map[string]string{
		"N-STP":   "test-engineer",
		"N-STH":   "test-engineer",
		"N-PERF":  "test-engineer",
		"N-RTH":   "senior-developer",
		"N-SMOKE": "senior-developer",
		"N-QA":    "qa-engineer",
		"N-IT":    "software-tester",
	}
	for name, want := range cases {
		if got := noncodingInventoryClass(name); got != want {
			t.Errorf("noncodingInventoryClass(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestDefaultEffortFor(t *testing.T) {
	cases := []struct {
		kind string
		want float64
	}{
		{"manager", 25},
		{"engine", 15},
		{"resourceAccess", 10},
		{"client", 25},
		{"utility", 10},
		{"resource", 10},
	}
	for _, c := range cases {
		if got := defaultEffortFor(c.kind); got != c.want {
			t.Errorf("defaultEffortFor(%q) = %v, want %v", c.kind, got, c.want)
		}
	}
}

// Every default must satisfy App C §4.4: a 5-day quantum, no god activity (>35d).
func TestDefaultEffortObeysQuantumAndCap(t *testing.T) {
	for _, kind := range []string{"manager", "engine", "resourceAccess", "client", "utility", "resource"} {
		e := defaultEffortFor(kind)
		if e <= 0 || e > 35 {
			t.Errorf("defaultEffortFor(%q) = %v, out of (0,35]", kind, e)
		}
		if int(e)%5 != 0 {
			t.Errorf("defaultEffortFor(%q) = %v, breaks the 5-day quantum", kind, e)
		}
	}
}

func TestDefaultRiskFor(t *testing.T) {
	cases := []struct {
		effort float64
		want   int64
	}{
		{5, 2}, {10, 2}, {15, 3}, {20, 3}, {25, 5}, {30, 5}, {35, 5},
	}
	for _, c := range cases {
		if got := defaultRiskFor(c.effort); got != c.want {
			t.Errorf("defaultRiskFor(%v) = %d, want %d", c.effort, got, c.want)
		}
	}
}

// Risk buckets are Fibonacci (1,2,3,5,8,13). A non-Fibonacci default would corrupt
// every downstream activity-risk roll-up.
func TestDefaultRiskIsFibonacci(t *testing.T) {
	fib := map[int64]bool{1: true, 2: true, 3: true, 5: true, 8: true, 13: true}
	for e := 5.0; e <= 35; e += 5 {
		if r := defaultRiskFor(e); !fib[r] {
			t.Errorf("defaultRiskFor(%v) = %d, not a Fibonacci bucket", e, r)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && GOWORK=off go test ./internal/engine/estimation/ -run 'TestWorkerClass|TestDefaultEffort|TestDefaultRisk' -v`
Expected: FAIL — `undefined: workerClassFor`, `undefined: noncodingInventoryClass`, `undefined: defaultEffortFor`, `undefined: defaultRiskFor`.

- [ ] **Step 3: Implement the tables**

Append to `server/internal/engine/estimation/derive.go`:

```go
// workerClassFor maps an activity ID prefix (plus the component kind for coding
// activities) to its worker class. The roster is FIXED — an unknown class silently
// rides default token rates in the cost engines and misclassifies in every downstream
// view, so this function only ever returns a roster member.
//
// Verified against the 69 hand-authored activities in the committed list: prefix+kind
// predicts workerClass with ZERO exceptions, which is what makes it derivable.
func workerClassFor(prefix string) string {
	switch prefix {
	case "C", "U":
		return "junior-developer" // junior builds components and the SPA
	case "R", "I":
		return "senior-developer" // senior integrates and owns provisioning
	case "G":
		return "ui-designer"
	default:
		return "senior-developer"
	}
}

// noncodingInventoryClass returns the fixed worker class for a member of the always-emit
// noncoding inventory. Löwy ch. 9 keeps three DISTINCT quality roles — the test engineer
// (writes code to break the system), the software tester (runs system testing), and the
// QA engineer (senior, process: "what will it take to assure quality?"). Do not collapse
// them. The regression harness is developer-owned, deliberately NOT the test engineer's.
func noncodingInventoryClass(name string) string {
	switch name {
	case "N-STP", "N-STH", "N-PERF":
		return "test-engineer"
	case "N-RTH", "N-SMOKE":
		return "senior-developer"
	case "N-QA":
		return "qa-engineer"
	case "N-IT":
		return "software-tester"
	default:
		return "senior-developer"
	}
}

// defaultEffortFor returns the band-MIDPOINT effort default for a component, in whole
// 5-day quanta. These are the bands the-method-activity-list already states.
//
// Deliberately NOT signal-driven (op counts, volatility counts, graph degree): service
// contracts are Phase-3 artifacts and do not exist when this runs, a regression over
// slot-5 metadata is the false precision App C §4.4 forbids ("strive for accuracy, not
// precision"), and it would make the baseline churn whenever a relationship is edited.
// Roughly half of these get overridden by a justified delta — that is the design intent:
// the agent's judgment is spent on the exceptions, not on transcription.
func defaultEffortFor(kind string) float64 {
	switch kind {
	case "manager":
		return 25
	case "engine":
		return 15
	case "resourceAccess":
		return 10
	case "client":
		return 25
	case "utility":
		return 10
	case "resource":
		return 10
	default:
		return 10
	}
}

// defaultRiskFor maps an effort band to a Fibonacci risk bucket. Dumb on purpose — it
// carries no more information than the effort does, which is honest: at Phase-2 time
// nothing better is knowable without the agent's judgment, and that arrives as an
// override.
func defaultRiskFor(effortDays float64) int64 {
	switch {
	case effortDays <= 10:
		return 2
	case effortDays <= 20:
		return 3
	default:
		return 5
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd server && GOWORK=off go test ./internal/engine/estimation/ -run 'TestWorkerClass|TestDefaultEffort|TestDefaultRisk' -v`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/internal/engine/estimation/
git commit -m "$(cat <<'EOF'
feat(estimation): worker-class, effort and risk default tables

Layer-band midpoints per App C 4.4 (quantum 5, cap 35) and Fibonacci risk
buckets. Signal-driven estimation rejected: service contracts are Phase-3
artifacts and do not exist at derivation time.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Derive the activity set

Emits every derived activity from the System. This is the heart of the change.

**Files:**
- Modify: `server/internal/engine/estimation/derive.go`
- Modify: `server/internal/engine/estimation/derive_test.go`

**Interfaces:**
- Consumes: `workerClassFor`, `noncodingInventoryClass`, `defaultEffortFor`, `defaultRiskFor` (Task 2); `SystemView`, `SystemComponent`, `DerivedActivity` (Task 1).
- Produces: `deriveActivities(system SystemView) []DerivedActivity` — sorted by activity `Name`, deterministic. Task 4 consumes the returned slice; Task 5 applies deltas to it.

- [ ] **Step 1: Write the failing tests**

Append to `server/internal/engine/estimation/derive_test.go`:

```go
// sampleSystem is a miniature but complete System: one handwritten manager, one engine,
// one resourceAccess, one GENERATED client that carries a UI surface, one vendor
// resource, one owned resource, and one utility. It exercises every emission rule.
func sampleSystem() SystemView {
	return SystemView{
		Components: []SystemComponent{
			{ID: "order-manager", Name: "OrderManager", Kind: "manager", ConstructionProfile: "handwritten"},
			{ID: "pricing-engine", Name: "PricingEngine", Kind: "engine", ConstructionProfile: "handwritten"},
			{ID: "order-access", Name: "OrderAccess", Kind: "resourceAccess", ConstructionProfile: "handwritten"},
			{ID: "web-client", Name: "WebClient", Kind: "client", ConstructionProfile: "generated", UiSurface: true},
			{ID: "stripe", Name: "Stripe", Kind: "resource", Provisioning: "vendor"},
			{ID: "order-db", Name: "OrderDB", Kind: "resource", Provisioning: "owned"},
			{ID: "logging", Name: "Logging", Kind: "utility", ConstructionProfile: "handwritten"},
		},
		CoreUseCaseIDs: []string{"UC1", "UC2"},
	}
}

func names(acts []DerivedActivity) map[string]DerivedActivity {
	m := make(map[string]DerivedActivity, len(acts))
	for _, a := range acts {
		m[a.Name] = a
	}
	return m
}

func TestDeriveActivitiesEmitsOneCodingActivityPerHandwrittenComponent(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	for _, want := range []string{"C-order-manager", "C-pricing-engine", "C-order-access", "C-logging"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing derived coding activity %q", want)
		}
	}
	if a := got["C-order-manager"]; a.ComponentID != "order-manager" || !a.Coding || a.EffortDays != 25 {
		t.Errorf("C-order-manager = %+v, want componentID order-manager, coding, 25d", a)
	}
}

// The platform GENERATES the whole transport tier (REST handlers, typed clients, MCP
// tools, OAS) from the committed contracts. Planning work the generator does is the
// defect this rule exists to prevent — and the live committed list violates it three
// times (C-CW, C-CM, C-CS).
func TestDeriveActivitiesEmitsNoCodingActivityForGeneratedComponents(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	if _, ok := got["C-web-client"]; ok {
		t.Error("emitted a coding activity for a generated-transport client; the generator does that work")
	}
}

// R-* is one per VENDOR resource. Owned stores (the schema/deploy work rides additive
// noncoding) get none — the live list gets this wrong in both directions.
func TestDeriveActivitiesEmitsProvisioningOnlyForVendorResources(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	if _, ok := got["R-stripe"]; !ok {
		t.Error("missing R-stripe for the vendor resource")
	}
	if _, ok := got["R-order-db"]; ok {
		t.Error("emitted R-order-db for an OWNED store; its work rides additive noncoding")
	}
	if a := got["R-stripe"]; a.Coding || a.WorkerClass != "senior-developer" {
		t.Errorf("R-stripe = %+v, want noncoding senior-developer", a)
	}
}

// One U-SPA per Manager: a Client calls Managers, a use case IS a Manager, and the
// verbs-as-tools doctrine makes a manager's generated tool surface its widget set. Plus
// the always-emit scaffold, and G-SPA sequenced before the UI work.
func TestDeriveActivitiesEmitsOneSPAActivityPerManagerPlusScaffold(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	if _, ok := got["U-SPA-order-manager"]; !ok {
		t.Error("missing U-SPA-order-manager")
	}
	if _, ok := got["U-SPA-S"]; !ok {
		t.Error("missing the always-emit U-SPA-S scaffold")
	}
	if _, ok := got["G-SPA"]; !ok {
		t.Error("missing G-SPA for a system with a UI surface")
	}
	if a := got["G-SPA"]; a.WorkerClass != "ui-designer" {
		t.Errorf("G-SPA worker class = %q, want ui-designer", a.WorkerClass)
	}
}

// uiSurface is a SEPARATE axis from constructionProfile: web-client is generated
// (no C-*) AND carries a UI surface (SPA work is real). Collapsing them loses the SPA.
func TestDeriveActivitiesNoSPAWorkWithoutAUISurface(t *testing.T) {
	sys := sampleSystem()
	for i := range sys.Components {
		sys.Components[i].UiSurface = false
	}
	got := names(deriveActivities(sys))
	for _, unwanted := range []string{"U-SPA-S", "G-SPA", "U-SPA-order-manager"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("emitted %q for a system with no UI surface", unwanted)
		}
	}
}

func TestDeriveActivitiesEmitsOneIntegrationActivityPerCoreUseCase(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	for _, want := range []string{"I-UC1", "I-UC2"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %q", want)
		}
	}
}

func TestDeriveActivitiesAlwaysEmitsTheTestingInventory(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	for _, want := range []string{"N-STP", "N-STH", "N-RTH", "N-SMOKE", "N-QA", "N-PERF", "N-IT"} {
		a, ok := got[want]
		if !ok {
			t.Errorf("missing always-emit %q", want)
			continue
		}
		if a.WorkerClass != noncodingInventoryClass(want) {
			t.Errorf("%s worker class = %q, want %q", want, a.WorkerClass, noncodingInventoryClass(want))
		}
	}
}

// Purity: identical input must give a byte-identical, stably ordered result. Map
// iteration order leaking into the output would make every downstream CPM solve
// nondeterministic.
func TestDeriveActivitiesIsDeterministicAndSorted(t *testing.T) {
	first := deriveActivities(sampleSystem())
	for i := 0; i < 20; i++ {
		next := deriveActivities(sampleSystem())
		if len(next) != len(first) {
			t.Fatalf("length varies across runs: %d vs %d", len(next), len(first))
		}
		for j := range first {
			if next[j].Name != first[j].Name {
				t.Fatalf("order varies across runs at %d: %q vs %q", j, next[j].Name, first[j].Name)
			}
		}
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].Name >= first[i].Name {
			t.Fatalf("not sorted ascending by name at %d: %q then %q", i, first[i-1].Name, first[i].Name)
		}
	}
}

// Every derived activity must be plan-legal on its face (App C 4.4 + the fixed roster).
func TestDeriveActivitiesAllObeyTheEstimationRules(t *testing.T) {
	roster := map[string]bool{
		"system-architect": true, "product-manager": true, "project-manager": true,
		"senior-developer": true, "junior-developer": true, "ui-designer": true,
		"ux-reviewer": true, "qa-engineer": true, "test-engineer": true, "software-tester": true,
	}
	fib := map[int64]bool{1: true, 2: true, 3: true, 5: true, 8: true, 13: true}
	for _, a := range deriveActivities(sampleSystem()) {
		if int(a.EffortDays)%5 != 0 || a.EffortDays <= 0 || a.EffortDays > 35 {
			t.Errorf("%s effort %v breaks the quantum/cap rule", a.Name, a.EffortDays)
		}
		if !roster[a.WorkerClass] {
			t.Errorf("%s worker class %q is not on the fixed roster", a.Name, a.WorkerClass)
		}
		if !fib[a.RiskBucket] {
			t.Errorf("%s risk bucket %d is not Fibonacci", a.Name, a.RiskBucket)
		}
		if !a.Derived {
			t.Errorf("%s is not flagged Derived", a.Name)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && GOWORK=off go test ./internal/engine/estimation/ -run TestDeriveActivities -v`
Expected: FAIL — `undefined: deriveActivities`.

- [ ] **Step 3: Implement the emission**

Append to `server/internal/engine/estimation/derive.go` (add `"sort"` to the import block):

```go
// alwaysEmitNoncoding is the standard testing / QA inventory emitted for EVERY project
// (the-method-activity-list Step 2b). Unit testing alone is "borderline useless" (Löwy
// ch. 2); the load-bearing verification is full regression of the integrated system, so
// the harnesses are planned work, not an afterthought. Fixed efforts — these do not
// scale with the architecture.
var alwaysEmitNoncoding = []struct {
	Name   string
	Title  string
	Effort float64
}{
	{"N-QA", "Quality-assurance process and gates", 10},
	{"N-PERF", "Performance testing", 15},
	{"N-RTH", "Regression test harness", 15},
	{"N-SMOKE", "Daily build and smoke", 5},
	{"N-STH", "System test harness", 20},
	{"N-STP", "System test plan (all core use cases)", 15},
	{"N-IT", "System testing (terminal gate)", 30},
}

// isCodeLayer reports whether a component kind gets a coding activity at all. Resources
// are provisioned, never coded by us.
func isCodeLayer(kind string) bool {
	switch kind {
	case "client", "manager", "engine", "resourceAccess", "utility":
		return true
	}
	return false
}

// deriveActivities emits the full derived activity set for the System, sorted by name.
//
// Emission rules (each one a mechanical consequence of the architecture):
//
//	C-<id>          one per code-layer component with constructionProfile != "generated"
//	(none)          generated-transport components — the generator does that work
//	R-<id>          one per Resource with provisioning == "vendor"
//	(none)          owned stores — schema/deploy work arrives as additive noncoding
//	U-SPA-<manager> one per Manager, when any component declares a UI surface
//	U-SPA-S         the SPA scaffold, when any component declares a UI surface
//	G-SPA           the UI-design concept, when any component declares a UI surface
//	I-UC<n>         one per core use case
//	N-*             the always-emit testing inventory
func deriveActivities(system SystemView) []DerivedActivity {
	out := make([]DerivedActivity, 0, len(system.Components)*2)

	uiSurface := false
	for _, c := range system.Components {
		if c.UiSurface {
			uiSurface = true
			break
		}
	}

	for _, c := range system.Components {
		switch {
		case isCodeLayer(c.Kind) && c.ConstructionProfile != "generated":
			effort := defaultEffortFor(c.Kind)
			out = append(out, DerivedActivity{
				Name:        "C-" + c.ID,
				Title:       "Build " + c.Name,
				EffortDays:  effort,
				RiskBucket:  defaultRiskFor(effort),
				WorkerClass: workerClassFor("C"),
				Coding:      true,
				ComponentID: c.ID,
				Derived:     true,
			})
		case c.Kind == "resource" && c.Provisioning == "vendor":
			effort := defaultEffortFor(c.Kind)
			out = append(out, DerivedActivity{
				Name:        "R-" + c.ID,
				Title:       "Provision " + c.Name,
				EffortDays:  effort,
				RiskBucket:  defaultRiskFor(effort),
				WorkerClass: workerClassFor("R"),
				Coding:      false,
				ComponentID: c.ID,
				Derived:     true,
			})
		}

		// One SPA construction activity per Manager. A screen that crosses managers is
		// the exception (it arrives as an additive delta), not a reason to weaken this.
		if uiSurface && c.Kind == "manager" {
			out = append(out, DerivedActivity{
				Name:        "U-SPA-" + c.ID,
				Title:       "SPA screens for " + c.Name,
				EffortDays:  20,
				RiskBucket:  defaultRiskFor(20),
				WorkerClass: workerClassFor("U"),
				Coding:      true,
				ComponentID: c.ID,
				Derived:     true,
			})
		}
	}

	if uiSurface {
		out = append(out,
			DerivedActivity{
				Name: "U-SPA-S", Title: "SPA scaffold, auth wiring and design system",
				EffortDays: 10, RiskBucket: defaultRiskFor(10),
				WorkerClass: workerClassFor("U"), Coding: true, Derived: true,
			},
			DerivedActivity{
				Name: "G-SPA", Title: "UI design concepts for the SPA",
				EffortDays: 15, RiskBucket: defaultRiskFor(15),
				WorkerClass: workerClassFor("G"), Coding: false, Derived: true,
			},
		)
	}

	for _, uc := range system.CoreUseCaseIDs {
		out = append(out, DerivedActivity{
			Name: "I-" + uc, Title: "Integrate " + uc,
			EffortDays: 10, RiskBucket: defaultRiskFor(10),
			WorkerClass: workerClassFor("I"), Coding: false, Derived: true,
		})
	}

	for _, n := range alwaysEmitNoncoding {
		out = append(out, DerivedActivity{
			Name: n.Name, Title: n.Title,
			EffortDays: n.Effort, RiskBucket: defaultRiskFor(n.Effort),
			WorkerClass: noncodingInventoryClass(n.Name), Coding: false, Derived: true,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd server && GOWORK=off go test ./internal/engine/estimation/ -run TestDeriveActivities -v`
Expected: PASS (8 tests).

- [ ] **Step 5: Run the whole package and the purity gates**

Run: `cd server && GOWORK=off go test ./internal/engine/estimation/... && GOWORK=off make encapsulation-check`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/internal/engine/estimation/
git commit -m "$(cat <<'EOF'
feat(estimation): derive the activity set from the System

One coding activity per handwritten code-layer component, none for
generated transport, R-* per vendor resource only, one U-SPA per manager
plus scaffold, I-* per core use case, and the always-emit testing
inventory. Deterministic and sorted.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Derive network edges, transitive reduction, and milestones

Löwy Fig 11-4 → Fig 11-5 is literally a transitive reduction. This task implements it plus the fixed pattern edges and derived milestones.

**Files:**
- Create: `server/internal/engine/estimation/derive_edges.go`
- Create: `server/internal/engine/estimation/derive_edges_test.go`

**Interfaces:**
- Consumes: `deriveActivities` (Task 3); `SystemView`, `NetworkDependency`, `NetworkMilestone`, `DerivedActivity` (Task 1).
- Produces: `transitiveReduction(edges map[string][]string) map[string][]string`, `deriveDependencies(system SystemView, acts []DerivedActivity) []NetworkDependency`, `deriveMilestones(system SystemView, acts []DerivedActivity) []NetworkMilestone`. Task 5 consumes the latter two; Task 6 asserts on all three.

- [ ] **Step 1: Write the failing tests**

Create `server/internal/engine/estimation/derive_edges_test.go`:

```go
package estimation

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Löwy Fig 11-4 → Fig 11-5: Client A depends on Manager A AND Security; Manager A also
// depends on Security. The Client→Security edge is INHERITED through Manager A and must
// be eliminated. This is the canonical worked example in ch. 11 §1.2.
func TestTransitiveReductionEliminatesInheritedDependencies(t *testing.T) {
	in := map[string][]string{
		"client-a":  {"manager-a", "security"},
		"manager-a": {"security"},
	}
	got := transitiveReduction(in)
	want := map[string][]string{
		"client-a":  {"manager-a"},
		"manager-a": {"security"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("transitiveReduction = %v, want %v", got, want)
	}
}

// A longer inherited chain: A→B→C→D plus the direct A→D and A→C shortcuts. Only the
// immediate edge survives at each hop.
func TestTransitiveReductionEliminatesMultiHopInheritance(t *testing.T) {
	in := map[string][]string{
		"a": {"b", "c", "d"},
		"b": {"c"},
		"c": {"d"},
	}
	got := transitiveReduction(in)
	want := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"d"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("transitiveReduction = %v, want %v", got, want)
	}
}

// A diamond has no redundant edge — both paths are load-bearing and must survive.
func TestTransitiveReductionKeepsDiamondEdges(t *testing.T) {
	in := map[string][]string{
		"top":   {"left", "right"},
		"left":  {"bottom"},
		"right": {"bottom"},
	}
	got := transitiveReduction(in)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("transitiveReduction dropped a load-bearing diamond edge: %v", got)
	}
}

// Determinism: predecessor lists must come back sorted, never in map order.
func TestTransitiveReductionIsSorted(t *testing.T) {
	in := map[string][]string{"x": {"c", "a", "b"}}
	got := transitiveReduction(in)
	if !sort.StringsAreSorted(got["x"]) {
		t.Errorf("predecessors not sorted: %v", got["x"])
	}
}

// A cycle is bad input, but the reduction must TERMINATE rather than hang or overflow —
// a malformed committed System must never wedge the derivation.
func TestTransitiveReductionTerminatesOnCycles(t *testing.T) {
	in := map[string][]string{"a": {"b"}, "b": {"a"}}
	_ = transitiveReduction(in) // must simply return
}

func edgeSystem() SystemView {
	sys := sampleSystem()
	sys.Relationships = []SystemRelationship{
		{From: "order-manager", To: "pricing-engine"},
		{From: "order-manager", To: "order-access"},
		{From: "pricing-engine", To: "order-access"},
		{From: "order-access", To: "order-db"},
		// order-access -> logging exists so C-order-access appears as a dependent at
		// all; without it the dangling-edge test below is vacuous (its loop body never
		// runs, because the only edge out of order-access is dropped before it becomes
		// a key). Adding it changes no other expectation: logging is a handwritten
		// utility with its own C-* activity, and it introduces no new inherited path.
		{From: "order-access", To: "logging"},
	}
	return sys
}

func depsByActivity(deps []NetworkDependency) map[string][]string {
	m := make(map[string][]string, len(deps))
	for _, d := range deps {
		m[d.Activity] = d.DependsOn
	}
	return m
}

// Architecture edges become activity edges, reduced. order-manager→order-access is
// inherited via pricing-engine and must not survive.
func TestDeriveDependenciesMapsRelationshipsAndReduces(t *testing.T) {
	got := depsByActivity(deriveDependencies(edgeSystem(), deriveActivities(edgeSystem())))
	if !reflect.DeepEqual(got["C-order-manager"], []string{"C-pricing-engine"}) {
		t.Errorf("C-order-manager dependsOn = %v, want [C-pricing-engine] after reduction", got["C-order-manager"])
	}
	if !reflect.DeepEqual(got["C-pricing-engine"], []string{"C-order-access"}) {
		t.Errorf("C-pricing-engine dependsOn = %v, want [C-order-access]", got["C-pricing-engine"])
	}
}

// An edge pointing at a component with NO derived activity (an owned store, a generated
// client) must be dropped, not emitted as a dangling reference into the CPM solve.
// The ComponentID contract this whole file depends on: deriveDependencies indexes
// activities by ComponentID to rewrite architecture edges as activity edges, so a
// componentless activity that carried a stray ComponentID would silently capture edges
// meant for a real component. Asserted here, at the consumer, rather than in Task 3.
func TestComponentIDIsSetOnlyOnComponentBoundActivities(t *testing.T) {
	for _, a := range deriveActivities(edgeSystem()) {
		componentBound := strings.HasPrefix(a.Name, "C-") || strings.HasPrefix(a.Name, "R-") ||
			(strings.HasPrefix(a.Name, "U-SPA-") && a.Name != "U-SPA-S")
		switch {
		case componentBound && a.ComponentID == "":
			t.Errorf("%s is component-bound but carries no ComponentID", a.Name)
		case !componentBound && a.ComponentID != "":
			t.Errorf("%s is componentless but carries ComponentID %q; it would capture edges meant for that component",
				a.Name, a.ComponentID)
		}
	}
}

// An edge pointing at a component with NO derived activity (an owned store, a generated
// client) must be dropped, not emitted as a dangling reference into the CPM solve — a
// dangling predecessor injects a zero-duration phantom node and silently distorts the
// critical path.
//
// Two things this test has to get right, both of which an earlier version got wrong: it
// must not be VACUOUS (C-order-access has to actually appear as a dependent, or the loop
// never runs), and it must assert on RESOLVABILITY rather than on specific names — a
// dropped !okTo guard emits the EMPTY STRING as a predecessor, not a readable id like
// "C-order-db", so a literal comparison sails straight past the regression.
func TestDeriveDependenciesDropsEdgesToComponentsWithNoActivity(t *testing.T) {
	sys := edgeSystem()
	acts := deriveActivities(sys)
	known := make(map[string]bool, len(acts))
	for _, a := range acts {
		known[a.Name] = true
	}
	deps := deriveDependencies(sys, acts)
	got := depsByActivity(deps)

	// Vacuity guard: if order-access ever stops appearing as a dependent, this test
	// proves nothing and must fail loudly rather than pass silently.
	if _, ok := got["C-order-access"]; !ok {
		t.Fatal("fixture went vacuous: C-order-access has no dependency row, so a leaked dangling edge could not be observed")
	}

	for _, d := range deps {
		for _, p := range d.DependsOn {
			switch {
			case p == "":
				t.Errorf("activity %q has an EMPTY predecessor — an edge to a component with no derived activity leaked through", d.Activity)
			case !known[p]:
				t.Errorf("activity %q depends on %q, which is not a derived activity; dangling predecessors inject zero-duration phantom nodes into the CPM solve", d.Activity, p)
			}
		}
	}

	// And specifically: the owned store must never have produced an activity edge.
	for _, p := range got["C-order-access"] {
		if strings.Contains(p, "order-db") {
			t.Errorf("edge to the owned store order-db survived: %v", got["C-order-access"])
		}
	}
}

// Fixed pattern edges: the UI design gates SPA construction, the scaffold gates the
// per-manager screens, the test plan gates the harness, and every integration gates the
// terminal system-testing activity.
func TestDeriveDependenciesEmitsFixedPatternEdges(t *testing.T) {
	sys := edgeSystem()
	got := depsByActivity(deriveDependencies(sys, deriveActivities(sys)))
	assertContains := func(activity, want string) {
		t.Helper()
		for _, p := range got[activity] {
			if p == want {
				return
			}
		}
		t.Errorf("%s dependsOn %v, missing %q", activity, got[activity], want)
	}
	assertContains("U-SPA-order-manager", "G-SPA")
	assertContains("U-SPA-order-manager", "U-SPA-S")
	assertContains("N-STH", "N-STP")
	assertContains("N-IT", "I-UC1")
	assertContains("N-IT", "I-UC2")
}

// M0 is the SDP-review milestone: Löwy makes it an explicit forced dependency so that
// no construction activity starts before the review. M1-M3 are layer-completion
// milestones, M4 is use-cases-demonstrable.
func TestDeriveMilestones(t *testing.T) {
	sys := edgeSystem()
	ms := deriveMilestones(sys, deriveActivities(sys))
	byID := make(map[string]NetworkMilestone, len(ms))
	for _, m := range ms {
		byID[m.Id] = m
	}
	for _, want := range []string{"M0", "M1", "M2", "M3", "M4"} {
		if _, ok := byID[want]; !ok {
			t.Errorf("missing derived milestone %q", want)
		}
	}
	if got := byID["M4"].DependsOn; !reflect.DeepEqual(got, []string{"I-UC1", "I-UC2"}) {
		t.Errorf("M4 dependsOn = %v, want the integration set", got)
	}
	if got := byID["M3"].DependsOn; !reflect.DeepEqual(got, []string{"C-order-manager"}) {
		t.Errorf("M3 (managers complete) dependsOn = %v, want [C-order-manager]", got)
	}
	// M5 (v1 Production Live) depends entirely on additive noncoding, so it is NOT
	// derived — it arrives as an additive delta.
	if _, ok := byID["M5"]; ok {
		t.Error("M5 must not be derived; it depends entirely on additive noncoding")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && GOWORK=off go test ./internal/engine/estimation/ -run 'TestTransitiveReduction|TestDeriveDependencies|TestDeriveMilestones' -v`
Expected: FAIL — `undefined: transitiveReduction`, `undefined: deriveDependencies`, `undefined: deriveMilestones`.

- [ ] **Step 3: Implement edge derivation**

Create `server/internal/engine/estimation/derive_edges.go`:

```go
// derive_edges.go turns the derived activity set plus the System's relationships into
// the project network: dependency edges (transitively reduced, exactly as Löwy Fig 11-4
// → Fig 11-5) and the derived milestones.
//
// "Even with a simple system having only two use cases, the dependency chart is
// cluttered and hard to analyze. A simple technique you can leverage to reduce the
// complexity is to eliminate dependencies that duplicate inherited dependencies."
// (ch. 11 §1.2) — that technique is transitive reduction, and it is code, not judgment.
package estimation

import "sort"

// transitiveReduction removes every edge that is implied by a longer path. An edge
// u→v is redundant when v is reachable from u through some OTHER direct successor of u.
//
// Cycle-safe: reachability is a visited-set BFS, so a malformed (cyclic) committed
// System yields a defensible result instead of hanging. Output predecessor lists are
// sorted, so the derivation stays deterministic.
func transitiveReduction(edges map[string][]string) map[string][]string {
	// reachable reports whether dst is reachable from src WITHOUT using the direct
	// src→skip edge.
	reachable := func(src, dst, skip string) bool {
		visited := map[string]bool{src: true}
		queue := make([]string, 0, len(edges[src]))
		for _, n := range edges[src] {
			if n != skip {
				queue = append(queue, n)
			}
		}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if cur == dst {
				return true
			}
			if visited[cur] {
				continue
			}
			visited[cur] = true
			queue = append(queue, edges[cur]...)
		}
		return false
	}

	out := make(map[string][]string, len(edges))
	for node, preds := range edges {
		kept := make([]string, 0, len(preds))
		for _, p := range preds {
			if !reachable(node, p, p) {
				kept = append(kept, p)
			}
		}
		if len(kept) == 0 {
			continue
		}
		sort.Strings(kept)
		out[node] = kept
	}
	return out
}

// deriveDependencies builds the network edges: the transitively reduced architecture
// edges, plus the fixed pattern edges that no relationship expresses.
func deriveDependencies(system SystemView, acts []DerivedActivity) []NetworkDependency {
	// activityForComponent indexes the CODING/PROVISIONING activity per component, so an
	// architecture edge can be rewritten as an activity edge. Components with no derived
	// activity (owned stores, generated transport) are simply absent, and edges into them
	// are dropped rather than emitted as dangling references.
	activityForComponent := map[string]string{}
	for _, a := range acts {
		if a.ComponentID == "" {
			continue
		}
		switch a.Name[:2] {
		case "C-", "R-":
			activityForComponent[a.ComponentID] = a.Name
		}
	}

	raw := map[string][]string{}
	for _, r := range system.Relationships {
		from, okFrom := activityForComponent[r.From]
		to, okTo := activityForComponent[r.To]
		if !okFrom || !okTo || from == to {
			continue
		}
		raw[from] = append(raw[from], to)
	}
	reduced := transitiveReduction(raw)

	// Fixed pattern edges — mechanical sequencing the architecture graph cannot state.
	present := map[string]bool{}
	var useCases []string
	var spaScreens []string
	for _, a := range acts {
		present[a.Name] = true
		if len(a.Name) > 2 && a.Name[:2] == "I-" {
			useCases = append(useCases, a.Name)
		}
		if len(a.Name) > 6 && a.Name[:6] == "U-SPA-" && a.Name != "U-SPA-S" {
			spaScreens = append(spaScreens, a.Name)
		}
	}
	sort.Strings(useCases)
	sort.Strings(spaScreens)

	addEdge := func(activity, pred string) {
		if !present[activity] || !present[pred] {
			return
		}
		reduced[activity] = append(reduced[activity], pred)
	}
	for _, s := range spaScreens {
		addEdge(s, "G-SPA")
		addEdge(s, "U-SPA-S")
	}
	addEdge("N-STH", "N-STP")
	addEdge("N-RTH", "N-STP")
	for _, uc := range useCases {
		addEdge("N-IT", uc)
	}

	out := make([]NetworkDependency, 0, len(reduced))
	for activity, preds := range reduced {
		sort.Strings(preds)
		out = append(out, NetworkDependency{Activity: activity, DependsOn: preds})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Activity < out[j].Activity })
	return out
}

// deriveMilestones emits M0-M4. M0 is the SDP-review forced dependency (ch. 11 "About
// Milestones": "none of the construction activities should start before the SDP
// review"). M1-M3 are layer completions, M4 is use-cases-demonstrable.
//
// M5 (v1 Production Live) is deliberately NOT derived: it depends entirely on additive
// noncoding activities, so it arrives as an additive delta.
func deriveMilestones(system SystemView, acts []DerivedActivity) []NetworkMilestone {
	kindByComponent := map[string]string{}
	for _, c := range system.Components {
		kindByComponent[c.ID] = c.Kind
	}

	var provisioning, engines, managers, integrations []string
	for _, a := range acts {
		switch {
		case len(a.Name) > 2 && a.Name[:2] == "R-":
			provisioning = append(provisioning, a.Name)
		case len(a.Name) > 2 && a.Name[:2] == "I-":
			integrations = append(integrations, a.Name)
		case len(a.Name) > 2 && a.Name[:2] == "C-":
			switch kindByComponent[a.ComponentID] {
			case "engine":
				engines = append(engines, a.Name)
			case "manager":
				managers = append(managers, a.Name)
			}
		}
	}
	for _, s := range [][]string{provisioning, engines, managers, integrations} {
		sort.Strings(s)
	}

	return []NetworkMilestone{
		{Id: "M0"}, // SDP Review Approved — the forced dependency, no fan-in
		{Id: "M1", DependsOn: provisioning},
		{Id: "M2", DependsOn: engines},
		{Id: "M3", DependsOn: managers},
		{Id: "M4", DependsOn: integrations},
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd server && GOWORK=off go test ./internal/engine/estimation/ -run 'TestTransitiveReduction|TestDeriveDependencies|TestDeriveMilestones' -v`
Expected: PASS (9 tests).

- [ ] **Step 5: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/internal/engine/estimation/
git commit -m "$(cat <<'EOF'
feat(estimation): derive network edges and milestones

Transitive reduction over the System relationships (Lowy Fig 11-4 to
11-5), plus fixed pattern edges and derived milestones M0-M4. M5 stays
additive: it depends entirely on additive noncoding.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Apply and validate deltas

The delta vocabulary is numbers + additive only. This task enforces that and wires `DerivePlan` to the real derivation.

**Files:**
- Create: `server/internal/engine/estimation/derive_deltas.go`
- Create: `server/internal/engine/estimation/derive_deltas_test.go`
- Modify: `server/internal/engine/estimation/derive.go` (replace the Task 1 stub body)

**Interfaces:**
- Consumes: `deriveActivities` (Task 3), `deriveDependencies`, `deriveMilestones` (Task 4).
- Produces: `applyDeltas(base []DerivedActivity, deps []NetworkDependency, ms []NetworkMilestone, deltas ActivityListDeltas) (DerivedPlan, error)`; and the completed `DerivePlan`, which Task 8 calls from the Manager.

- [ ] **Step 1: Write the failing tests**

Create `server/internal/engine/estimation/derive_deltas_test.go`:

```go
package estimation

import (
	"errors"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

func planFor(t *testing.T, deltas ActivityListDeltas) DerivedPlan {
	t.Helper()
	plan, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), deltas)
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	return plan
}

func activityNamed(plan DerivedPlan, name string) (DerivedActivity, bool) {
	for _, a := range plan.Activities {
		if a.Name == name {
			return a, true
		}
	}
	return DerivedActivity{}, false
}

func TestDerivePlanWithNoDeltasReturnsTheBaseline(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{})
	if len(plan.Activities) == 0 {
		t.Fatal("DerivePlan returned no activities for a populated System")
	}
	if _, ok := activityNamed(plan, "C-order-manager"); !ok {
		t.Error("baseline missing C-order-manager")
	}
	if len(plan.Dependencies) == 0 {
		t.Error("baseline produced no dependency edges")
	}
}

func TestOverrideReplacesEffortAndRisk(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{Overrides: []ActivityOverride{{
		Activity: "C-order-manager", EffortDays: ptrFloat(35), RiskBucket: ptrInt(8),
		Justification: "orchestrates five downstream contracts; the band midpoint is optimistic",
	}}})
	a, ok := activityNamed(plan, "C-order-manager")
	if !ok {
		t.Fatal("C-order-manager missing after override")
	}
	if a.EffortDays != 35 || a.RiskBucket != 8 {
		t.Errorf("override not applied: %+v", a)
	}
	if !a.Derived {
		t.Error("an overridden activity is still a DERIVED activity")
	}
}

// An override naming no derived activity is the zombie failure mode: the live committed
// list carries C-HE, C-WIA and R-WIT against components that do not exist. Loud failure.
func TestOverrideOfUnknownActivityIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Overrides: []ActivityOverride{{Activity: "C-hand-off-engine", Justification: "x"}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an override of an unknown activity, got %v", err)
	}
}

func TestOverrideWithoutJustificationIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Overrides: []ActivityOverride{{Activity: "C-order-manager", EffortDays: ptrFloat(35)}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an unjustified override, got %v", err)
	}
}

func TestOverrideBreakingTheQuantumIsRejected(t *testing.T) {
	for _, bad := range []float64{7, 11, 40} {
		_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
			Overrides: []ActivityOverride{{
				Activity: "C-order-manager", EffortDays: ptrFloat(bad), Justification: "j",
			}},
		})
		if err == nil {
			t.Errorf("override effort %v should be rejected (quantum 5, cap 35)", bad)
		}
	}
}

func TestAdditiveActivityIsAppended(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{Additive: []AdditiveActivity{{
		Name: "N-SCHEMA", Title: "Schema design for owned stores",
		EffortDays: 10, RiskBucket: 2, WorkerClass: "system-architect",
		DependsOn: []string{"C-order-access"},
		Justification: "owned stores carry schema work no component activity covers",
	}}})
	a, ok := activityNamed(plan, "N-SCHEMA")
	if !ok {
		t.Fatal("additive activity not appended")
	}
	if a.Derived {
		t.Error("an additive activity must NOT be flagged Derived")
	}
	var found bool
	for _, d := range plan.Dependencies {
		if d.Activity == "N-SCHEMA" && len(d.DependsOn) == 1 && d.DependsOn[0] == "C-order-access" {
			found = true
		}
	}
	if !found {
		t.Error("the additive activity's own incident edge was not emitted")
	}
}

// C2: an additive carrying a componentId is a covert per-component exclusion or
// replacement channel. It is exactly how C-HE and C-WIA would come back.
//
// AdditiveActivity.ComponentID is a *string on purpose (author input, where nil-vs-
// supplied is load-bearing) so the literal needs ptrString, NOT a bare string. The empty
// string is deliberately still a rejection: a caller writing `"componentId": ""` is
// supplying the field, and a plain string type could not tell that from absent.
func TestAdditiveWithComponentIDIsRejected(t *testing.T) {
	for _, cid := range []string{"order-manager", ""} {
		_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
			Additive: []AdditiveActivity{{
				Name: "N-X", Title: "x", EffortDays: 5, RiskBucket: 2,
				WorkerClass: "junior-developer", Justification: "j",
				ComponentID: ptrString(cid),
			}},
		})
		var fe *fweng.Error
		if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
			t.Fatalf("componentId=%q: want ContractMisuse for an additive carrying a componentId, got %v", cid, err)
		}
	}
}

func ptrString(s string) *string { return &s }

// An additive may not shadow a derived activity — that is an exclusion in disguise.
func TestAdditiveCollidingWithADerivedNameIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "C-order-manager", Title: "shadow", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "junior-developer", Justification: "j",
		}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an additive shadowing a derived activity, got %v", err)
	}
}

// C3: an additive declares its OWN incident edges only. Pointing at a nonexistent
// activity would inject a dangling node into the CPM solve.
func TestAdditiveEdgeToUnknownActivityIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "N-X", Title: "x", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "junior-developer", Justification: "j",
			DependsOn: []string{"C-does-not-exist"},
		}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an additive edge to an unknown activity, got %v", err)
	}
}

func TestAdditiveWithOffRosterWorkerClassIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "N-X", Title: "x", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "Platform-DevOps-Engineer", Justification: "j",
		}},
	})
	if err == nil {
		t.Fatal("an off-roster worker class must be rejected; it would silently ride default token rates")
	}
}

// C4: M5 "v1 Production Live" depends entirely on additive noncoding, so it cannot
// derive — it is authored as an additive milestone.
func TestAdditiveMilestoneIsAppended(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "N-DEP", Title: "Production deployment", EffortDays: 10, RiskBucket: 3,
			WorkerClass: "senior-developer", Justification: "deployment is componentless project work",
		}},
		AdditiveMilestones: []AdditiveMilestone{{
			Id: "M5", DependsOn: []string{"N-DEP", "N-IT"},
			Justification: "v1 production live gates on deployment plus the terminal system-testing gate",
		}},
	})
	var found bool
	for _, m := range plan.Milestones {
		if m.Id == "M5" {
			found = true
		}
	}
	if !found {
		t.Error("additive milestone M5 was not appended")
	}
}

func TestAdditiveMilestoneShadowingADerivedOneIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		AdditiveMilestones: []AdditiveMilestone{{Id: "M0", Justification: "j"}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an additive milestone shadowing a derived one, got %v", err)
	}
}

// An empty System is a normal DOMAIN result (a project read before its architecture is
// committed), never an error.
func TestDerivePlanOnEmptySystemIsAnEmptyPlanNotAnError(t *testing.T) {
	plan, err := NewEstimationEngine().DerivePlan(fweng.Context{}, SystemView{}, ActivityListDeltas{})
	if err != nil {
		t.Fatalf("empty System must be a domain result, got error %v", err)
	}
	if len(plan.Activities) != 0 {
		t.Errorf("empty System produced %d activities", len(plan.Activities))
	}
}

// mustReject asserts the deltas are refused with a ContractMisuse. Every caller varies
// exactly ONE field away from an otherwise-legal delta, so the named guard is the only
// one eligible — a rejection test that passed because an EARLIER guard fired would not
// be testing what its name claims.
func mustReject(t *testing.T, what string, deltas ActivityListDeltas) {
	t.Helper()
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), deltas)
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("%s: want ContractMisuse, got %v", what, err)
	}
}

// legalAdditive passes every guard, so a caller can break exactly one field and know
// which guard fired.
func legalAdditive() AdditiveActivity {
	return AdditiveActivity{
		Name: "N-X", Title: "x", EffortDays: 5, RiskBucket: 2,
		WorkerClass: "senior-developer", Justification: "j",
	}
}

// The deferred two-pass validation exists so two additives may legally depend on each
// other: appendAdditives indexes EVERY additive before validateAdditiveEdges checks any
// edge. Inlining the edge check into the append loop would reject this valid input — and
// that regression is invisible to every other test in this file, because their edge
// targets are all derived baseline activities that exist before additive processing.
// The reference is FORWARD on purpose: N-FIRST depends on N-SECOND, appended after it.
func TestAdditivesMayDependOnEachOther(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{Additive: []AdditiveActivity{
		{
			Name: "N-FIRST", Title: "first", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "senior-developer", DependsOn: []string{"N-SECOND"},
			Justification: "depends on a sibling additive declared after it",
		},
		{
			Name: "N-SECOND", Title: "second", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "senior-developer",
			Justification: "the forward-referenced sibling",
		},
	}})
	if _, ok := activityNamed(plan, "N-FIRST"); !ok {
		t.Error("N-FIRST missing from the plan")
	}
	if _, ok := activityNamed(plan, "N-SECOND"); !ok {
		t.Error("N-SECOND missing from the plan")
	}
	var found bool
	for _, d := range plan.Dependencies {
		if d.Activity == "N-FIRST" && len(d.DependsOn) == 1 && d.DependsOn[0] == "N-SECOND" {
			found = true
		}
	}
	if !found {
		t.Error("N-FIRST -> N-SECOND edge missing; the two-pass ordering has regressed to single-pass")
	}
}

func TestOverrideWithNonFibonacciRiskIsRejected(t *testing.T) {
	mustReject(t, "override with risk bucket 4", ActivityListDeltas{Overrides: []ActivityOverride{{
		Activity: "C-order-manager", RiskBucket: ptrInt(4), Justification: "j",
	}}})
}

func TestAdditiveWithoutJustificationIsRejected(t *testing.T) {
	a := legalAdditive()
	a.Justification = ""
	mustReject(t, "additive without justification", ActivityListDeltas{Additive: []AdditiveActivity{a}})
}

func TestAdditiveWithIllegalEffortIsRejected(t *testing.T) {
	for _, bad := range []float64{7, 40} {
		a := legalAdditive()
		a.EffortDays = bad
		mustReject(t, "additive with off-quantum or oversized effort",
			ActivityListDeltas{Additive: []AdditiveActivity{a}})
	}
}

func TestAdditiveWithNonFibonacciRiskIsRejected(t *testing.T) {
	a := legalAdditive()
	a.RiskBucket = 4
	mustReject(t, "additive with risk bucket 4", ActivityListDeltas{Additive: []AdditiveActivity{a}})
}

func TestAdditiveMilestoneWithoutJustificationIsRejected(t *testing.T) {
	mustReject(t, "additive milestone without justification", ActivityListDeltas{
		AdditiveMilestones: []AdditiveMilestone{{Id: "M9"}},
	})
}

func TestAdditiveMilestoneWithDanglingDependencyIsRejected(t *testing.T) {
	mustReject(t, "additive milestone with dangling dependsOn", ActivityListDeltas{
		AdditiveMilestones: []AdditiveMilestone{{
			Id: "M9", DependsOn: []string{"N-DOES-NOT-EXIST"}, Justification: "j",
		}},
	})
}

func ptrFloat(f float64) *float64 { return &f }
func ptrInt(i int64) *int64       { return &i }
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && GOWORK=off go test ./internal/engine/estimation/ -run 'TestDerivePlan|TestOverride|TestAdditive' -v`
Expected: FAIL — the stub returns an empty plan, so `TestDerivePlanWithNoDeltasReturnsTheBaseline` fails first.

> **Note:** if the generated `ActivityOverride` uses non-pointer `EffortDays float64` / `RiskBucket int64` (modelgen renders `["null","number"]` as a pointer; verify with `grep -n "type ActivityOverride" -A6 internal/engine/estimation/contract.gen.go`), adjust `ptrFloat`/`ptrInt` usage accordingly and treat the zero value as "not set". Do not hand-edit `contract.gen.go`.

- [ ] **Step 3: Implement delta validation and application**

Create `server/internal/engine/estimation/derive_deltas.go`:

```go
// derive_deltas.go enforces and applies the authored delta vocabulary.
//
// The vocabulary is CLOSED and deliberately narrow — numbers plus additive activities:
//
//   - an OVERRIDE may replace effortDays / riskBucket on a DERIVED activity, and must
//     carry a written justification;
//   - an ADDITIVE may append an activity that maps to NO single component, declaring its
//     own incident edges.
//
// There is no exclusion and no derived-to-derived edge override, on purpose. An
// exclusion asserts that a committed component requires no work — which is either false
// or an admission that it should not be a component. A wrong exclusion is SILENT where a
// wrong derivation is LOUD, and the silent form is exactly how C-HE, C-WIA and R-WIT
// survived in the committed plan against components that no longer exist. If a derived
// edge is wrong, the System relationship is wrong: fix the architecture.
package estimation

import (
	"sort"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// workerRoster is the fixed Method team. An unknown class silently rides default token
// rates in the cost engines and misclassifies in every downstream view, so it is
// rejected rather than defaulted.
var workerRoster = map[string]bool{
	"system-architect": true, "product-manager": true, "project-manager": true,
	"senior-developer": true, "junior-developer": true, "ui-designer": true,
	"ux-reviewer": true, "qa-engineer": true, "test-engineer": true, "software-tester": true,
}

var fibonacciBuckets = map[int64]bool{1: true, 2: true, 3: true, 5: true, 8: true, 13: true}

// legalEffort enforces App C §4.4: a 5-day quantum, no god activity.
func legalEffort(d float64) bool {
	return d > 0 && d <= 35 && float64(int(d)) == d && int(d)%5 == 0
}

// applyDeltas overlays the authored deltas on the derived baseline.
func applyDeltas(base []DerivedActivity, deps []NetworkDependency, ms []NetworkMilestone, deltas ActivityListDeltas) (DerivedPlan, error) {
	index := make(map[string]int, len(base))
	for i, a := range base {
		index[a.Name] = i
	}
	acts := make([]DerivedActivity, len(base))
	copy(acts, base)

	for _, o := range deltas.Overrides {
		i, ok := index[o.Activity]
		if !ok {
			return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
				"DerivePlan: override names activity "+o.Activity+" which the System does not derive; "+
					"if the work is real the architecture is missing a component, and if the component is gone the override is a zombie")
		}
		if o.Justification == "" {
			return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
				"DerivePlan: override of "+o.Activity+" carries no justification; the delta document is the entire human-review surface and every line must defend itself")
		}
		if o.EffortDays != nil {
			if !legalEffort(*o.EffortDays) {
				return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
					"DerivePlan: override of "+o.Activity+" breaks the 5-day quantum or the 35-day god-activity cap")
			}
			acts[i].EffortDays = *o.EffortDays
		}
		if o.RiskBucket != nil {
			if !fibonacciBuckets[*o.RiskBucket] {
				return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
					"DerivePlan: override of "+o.Activity+" sets a non-Fibonacci risk bucket")
			}
			acts[i].RiskBucket = *o.RiskBucket
		}
	}

	extraDeps := make([]NetworkDependency, 0, len(deltas.Additive))
	for _, a := range deltas.Additive {
		if _, clash := index[a.Name]; clash {
			return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
				"DerivePlan: additive activity "+a.Name+" shadows a derived activity; that is an exclusion in disguise")
		}
		if a.ComponentID != nil {
			return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
				"DerivePlan: additive activity "+a.Name+" carries a componentId; additive is for genuinely componentless work, "+
					"and a component-bound additive is a covert exclusion/replacement channel")
		}
		if a.Justification == "" {
			return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
				"DerivePlan: additive activity "+a.Name+" carries no justification")
		}
		if !legalEffort(a.EffortDays) {
			return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
				"DerivePlan: additive activity "+a.Name+" breaks the 5-day quantum or the 35-day god-activity cap")
		}
		if !fibonacciBuckets[a.RiskBucket] {
			return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
				"DerivePlan: additive activity "+a.Name+" sets a non-Fibonacci risk bucket")
		}
		if !workerRoster[a.WorkerClass] {
			return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
				"DerivePlan: additive activity "+a.Name+" names worker class "+a.WorkerClass+
					", which is not on the fixed Method roster; an unknown class silently rides default token rates")
		}
		acts = append(acts, DerivedActivity{
			Name: a.Name, Title: a.Title, EffortDays: a.EffortDays, RiskBucket: a.RiskBucket,
			WorkerClass: a.WorkerClass, Coding: a.Coding, Derived: false,
		})
		index[a.Name] = len(acts) - 1
		if len(a.DependsOn) > 0 {
			preds := make([]string, len(a.DependsOn))
			copy(preds, a.DependsOn)
			sort.Strings(preds)
			extraDeps = append(extraDeps, NetworkDependency{Activity: a.Name, DependsOn: preds})
		}
	}

	// Additive edges are validated only AFTER every additive exists, so two additives may
	// legally depend on each other.
	for _, d := range extraDeps {
		for _, p := range d.DependsOn {
			if _, ok := index[p]; !ok {
				return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
					"DerivePlan: additive activity "+d.Activity+" depends on "+p+
						", which is not an activity in the plan; an additive declares its OWN incident edges only")
			}
		}
	}

	// Additive milestones (C4). M0-M4 derive; M5 "v1 Production Live" depends entirely on
	// additive noncoding and therefore cannot derive — it is authored here. A derived
	// milestone may still ACQUIRE predecessors from additive activities, which is why
	// dependsOn is validated against the full post-additive activity set.
	milestones := make([]NetworkMilestone, len(ms))
	copy(milestones, ms)
	derivedMilestone := make(map[string]bool, len(ms))
	for _, m := range ms {
		derivedMilestone[m.Id] = true
	}
	for _, am := range deltas.AdditiveMilestones {
		if derivedMilestone[am.Id] {
			return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
				"DerivePlan: additive milestone "+am.Id+" shadows a derived milestone")
		}
		if am.Justification == "" {
			return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
				"DerivePlan: additive milestone "+am.Id+" carries no justification")
		}
		for _, p := range am.DependsOn {
			if _, ok := index[p]; !ok {
				return DerivedPlan{}, fweng.New(fweng.ContractMisuse,
					"DerivePlan: additive milestone "+am.Id+" depends on "+p+", which is not an activity in the plan")
			}
		}
		preds := make([]string, len(am.DependsOn))
		copy(preds, am.DependsOn)
		sort.Strings(preds)
		milestones = append(milestones, NetworkMilestone{Id: am.Id, DependsOn: preds})
		derivedMilestone[am.Id] = true
	}
	sort.Slice(milestones, func(i, j int) bool { return milestones[i].Id < milestones[j].Id })

	allDeps := make([]NetworkDependency, 0, len(deps)+len(extraDeps))
	allDeps = append(allDeps, deps...)
	allDeps = append(allDeps, extraDeps...)
	sort.Slice(allDeps, func(i, j int) bool { return allDeps[i].Activity < allDeps[j].Activity })
	sort.Slice(acts, func(i, j int) bool { return acts[i].Name < acts[j].Name })

	return DerivedPlan{Activities: acts, Dependencies: allDeps, Milestones: milestones}, nil
}
```

- [ ] **Step 4: Wire `DerivePlan` to the real derivation**

In `server/internal/engine/estimation/derive.go`, replace the stub body of `DerivePlan` with:

```go
func (EstimationEngineImpl) DerivePlan(_ fweng.Context, system SystemView, deltas ActivityListDeltas) (DerivedPlan, error) {
	if len(system.Components) == 0 {
		return DerivedPlan{Activities: nil, Dependencies: nil, Milestones: nil}, nil
	}
	acts := deriveActivities(system)
	deps := deriveDependencies(system, acts)
	ms := deriveMilestones(system, acts)
	return applyDeltas(acts, deps, ms, deltas)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd server && GOWORK=off go test ./internal/engine/estimation/... -v`
Expected: all PASS, including the pre-existing CPM tests.

- [ ] **Step 6: Verify engine purity still holds**

Run:
```bash
cd server && GOWORK=off make encapsulation-check
grep -n "projectstate\|net/http\|os\.\|time\.Now" internal/engine/estimation/derive*.go || echo "engine purity OK"
```
Expected: encapsulation check PASS, purity grep prints "engine purity OK".

- [ ] **Step 7: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/internal/engine/estimation/
git commit -m "$(cat <<'EOF'
feat(estimation): apply and validate the delta vocabulary

Numbers plus additive only. No exclusions and no derived-edge overrides:
a wrong exclusion is silent where a wrong derivation is loud, which is
how C-HE/C-WIA/R-WIT survived against components that no longer exist.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Golden parity test against the live committed System

The load-bearing test. It proves the deriver reproduces a plan a human architect actually authored and reviewed, modulo the four corrections the spec predicts.

**Files:**
- Create: `server/internal/engine/estimation/testdata/system_view.json`
- Create: `server/internal/engine/estimation/derive_parity_test.go`

**Interfaces:**
- Consumes: `DerivePlan` (Task 5).
- Produces: nothing consumed by later tasks — this is a verification gate.

- [ ] **Step 1: Extract the live System into a frozen fixture**

Run:
```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
python3 - <<'PY'
import json
d = json.load(open('.aiarch/state/project.json'))
sysm = d['slots']['5']['model']
# constructionProfile / provisioning / uiSurface do not exist on slot 5 yet (Task 7
# adds them). Stamp the values Task 7 will author, so parity can be proven FIRST.
generated = {'web-client', 'mcp-client', 'scheduler-client'}
vendor = {'github', 'merchant-gateway', 'construction-pipeline-runtime', 'operated-runtime'}
comps = []
for c in sysm['components']:
    # All three attributes are REQUIRED on the Engine's slim view, so emit every one on
    # every component — the Engine receives fully resolved values and never re-defaults.
    comps.append({
        'id': c['id'],
        'name': c['name'],
        'kind': c['kind'],
        'constructionProfile': 'generated' if c['id'] in generated else 'handwritten',
        'provisioning': ('vendor' if c['id'] in vendor else 'owned') if c['kind'] == 'resource' else 'owned',
        'uiSurface': c['id'] == 'web-client',
    })
rels = [{'from': r['from'], 'to': r['to']} for r in sysm.get('relationships', [])]
out = {'components': comps, 'relationships': rels,
       'coreUseCaseIds': ['UC1', 'UC2', 'UC3', 'UC4', 'UC5']}
json.dump(out, open('server/internal/engine/estimation/testdata/system_view.json', 'w'), indent=1, sort_keys=True)
print('components', len(comps), 'relationships', len(rels))
PY
```
Expected: `components 37 relationships <N>`.

> If `relationships` entries use different field names than `from`/`to`, inspect one with `python3 -c "import json;d=json.load(open('.aiarch/state/project.json'));print(d['slots']['5']['model']['relationships'][0])"` and adjust the extraction. The fixture must end with exactly `from` and `to`.

- [ ] **Step 2: Write the failing parity test**

Create `server/internal/engine/estimation/derive_parity_test.go`:

```go
package estimation

import (
	"encoding/json"
	"os"
	"testing"
)

// loadSystemFixture reads the frozen slim view of the live committed System (37
// components) with the Task-7 typed attributes stamped on.
func loadSystemFixture(t *testing.T) SystemView {
	t.Helper()
	raw, err := os.ReadFile("testdata/system_view.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var sv SystemView
	if err := json.Unmarshal(raw, &sv); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(sv.Components) != 37 {
		t.Fatalf("fixture has %d components, want the live 37", len(sv.Components))
	}
	return sv
}

func parityPlan(t *testing.T) DerivedPlan {
	t.Helper()
	plan, err := NewEstimationEngine().DerivePlan(fweng.Context{}, loadSystemFixture(t), ActivityListDeltas{})
	if err != nil {
		t.Fatalf("DerivePlan over the live System: %v", err)
	}
	return plan
}

func parityNames(plan DerivedPlan) map[string]bool {
	m := make(map[string]bool, len(plan.Activities))
	for _, a := range plan.Activities {
		m[a.Name] = true
	}
	return m
}

// Correction 1 — the three zombie activities (C-HE / C-WIA / R-WIT in the committed
// plan, all Done+Integrated against components that no longer exist) must not appear.
//
// HONEST SCOPE: this test cannot prove the deriver "excludes" them, because there is no
// exclusion branch to exercise — deriveActivities emits C-*/R-* names ONLY by iterating
// system.Components, so a component that does not exist can never produce an activity.
// Corrections 2 and 3 below DO have real guard branches (constructionProfile ==
// "generated", provisioning != "vendor") and their tests genuinely exercise them.
//
// What this test is actually worth: (a) a fixture-staleness tripwire, failing if a
// future re-extraction reintroduces one of these three components, and (b) a structural
// tripwire against any regression that sourced activity names from somewhere other than
// system.Components. The real evidence for Correction 1 is the derived-vs-committed
// diff (49 derived vs the committed 69), not this assertion in isolation.
func TestParityDropsTheZombieActivities(t *testing.T) {
	got := parityNames(parityPlan(t))
	for _, zombie := range []string{"C-hand-off-engine", "C-work-item-access", "R-work-item-tracker"} {
		if got[zombie] {
			t.Errorf("derived the zombie activity %q", zombie)
		}
	}
}

// Correction 2: the generated transport tier gets no coding activity. C-CW, C-CM and
// C-CS are committed today in violation of standing doctrine.
func TestParityDropsGeneratedClientCodingActivities(t *testing.T) {
	got := parityNames(parityPlan(t))
	for _, c := range []string{"C-web-client", "C-mcp-client", "C-scheduler-client"} {
		if got[c] {
			t.Errorf("derived %q for a generated-transport client", c)
		}
	}
}

// Correction 3: R-* only for vendor resources. The four owned stores get none.
func TestParityEmitsProvisioningOnlyForVendorResources(t *testing.T) {
	got := parityNames(parityPlan(t))
	for _, want := range []string{"R-github", "R-merchant-gateway", "R-construction-pipeline-runtime", "R-operated-runtime"} {
		if !got[want] {
			t.Errorf("missing vendor provisioning activity %q", want)
		}
	}
	for _, unwanted := range []string{"R-project-git-repo", "R-operated-system-state", "R-billing-state", "R-usage-log"} {
		if got[unwanted] {
			t.Errorf("derived %q for an OWNED store; its work rides additive noncoding", unwanted)
		}
	}
}

// Correction 4: one U-SPA per manager. Five managers, five activities, plus scaffold.
func TestParityEmitsOneSPAActivityPerManager(t *testing.T) {
	got := parityNames(parityPlan(t))
	for _, m := range []string{"system-design-manager", "project-design-manager", "construction-manager", "operations-manager", "billing-manager"} {
		if !got["U-SPA-"+m] {
			t.Errorf("missing U-SPA-%s", m)
		}
	}
	if !got["U-SPA-S"] || !got["G-SPA"] {
		t.Error("missing the always-emit scaffold / UI-design activities")
	}
}

// Every code-layer component that is not generated transport must be covered exactly
// once. This is the invariant that ACT-COMPONENT-COVERAGE used to enforce as a gate and
// that derivation now makes true by construction.
func TestParityCoversEveryHandwrittenCodeComponentExactlyOnce(t *testing.T) {
	sys := loadSystemFixture(t)
	plan := parityPlan(t)
	count := map[string]int{}
	for _, a := range plan.Activities {
		if a.Coding && a.ComponentID != "" && len(a.Name) > 2 && a.Name[:2] == "C-" {
			count[a.ComponentID]++
		}
	}
	for _, c := range sys.Components {
		if !isCodeLayer(c.Kind) || c.ConstructionProfile == "generated" {
			continue
		}
		if count[c.ID] != 1 {
			t.Errorf("component %s has %d coding activities, want exactly 1", c.ID, count[c.ID])
		}
	}
}

// No dependency edge may name an activity that does not exist — a dangling predecessor
// silently corrupts the CPM solve (it contributes a zero-duration phantom node).
func TestParityHasNoDanglingDependencyEdges(t *testing.T) {
	plan := parityPlan(t)
	known := parityNames(plan)
	for _, m := range plan.Milestones {
		known[m.Id] = true
	}
	for _, d := range plan.Dependencies {
		if !known[d.Activity] {
			t.Errorf("dependency row for unknown activity %q", d.Activity)
		}
		for _, p := range d.DependsOn {
			if !known[p] {
				t.Errorf("activity %q depends on unknown %q", d.Activity, p)
			}
		}
	}
}

// The derived plan must feed the EXISTING CPM solve without adaptation — that is the
// point of deriving into the same shapes ComputeNetwork already consumes.
func TestParityPlanSolvesThroughComputeNetwork(t *testing.T) {
	plan := parityPlan(t)
	items := make([]ActivityItem, 0, len(plan.Activities))
	for _, a := range plan.Activities {
		items = append(items, ActivityItem{Name: a.Name, EffortDays: a.EffortDays})
	}
	sol, err := NewEstimationEngine().ComputeNetwork(fweng.Context{},
		ActivityList{Activities: items},
		Network{Dependencies: plan.Dependencies, Milestones: plan.Milestones})
	if err != nil {
		t.Fatalf("ComputeNetwork over the derived plan: %v", err)
	}
	if len(sol.Nodes) == 0 {
		t.Fatal("ComputeNetwork produced no nodes for the derived plan")
	}
	if sol.Summary.TotalDurationDays <= 0 {
		t.Errorf("derived plan solves to a non-positive duration %v", sol.Summary.TotalDurationDays)
	}
	if sol.Summary.CriticalPathActivityCount == 0 {
		t.Error("derived plan has no critical path; the edge derivation produced a disconnected graph")
	}
}
```

- [ ] **Step 3: Run the parity tests**

Run: `cd server && GOWORK=off go test ./internal/engine/estimation/ -run TestParity -v`
Expected: PASS (7 tests).

- [ ] **Step 4: Record the derived-vs-committed diff for the record**

Run:
```bash
cd server && GOWORK=off go test ./internal/engine/estimation/ -run TestParity -v 2>&1 | tail -30
```
Then write the activity-count comparison into the commit message body (derived count vs the committed 69) so the reviewer can see the shape of the change.

- [ ] **Step 5: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/internal/engine/estimation/
git commit -m "$(cat <<'EOF'
test(estimation): golden parity against the live committed System

Proves the deriver reproduces a plan a human architect authored and
reviewed, modulo the four predicted corrections: three zombies dropped,
three generated-client activities dropped, R-* re-derived from vendor
provisioning, U-SPA re-derived per manager. Also proves the derived plan
feeds the existing ComputeNetwork solve unchanged.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Add the typed attributes to the committed System

Until now the attributes lived only in the test fixture. This task makes them real on slot 5.

**Files:**
- Modify: `.aiarch/state/project.json` → `.serviceContracts.projectStateAccess.$defs.Component` (hand-edit)
- Regenerate: `server/internal/resourceaccess/projectstate/contract.gen.go`
- Modify: `.aiarch/state/project.json` → `slots["5"].model.components` (hand-edit; slot 5 stays `status: 2` / committed — this is a reconciliation amendment, the shape the finish-construction wave used)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `projectstate.Component.ConstructionProfile string`, `.Provisioning string`, `.UiSurface bool`. Task 8's conversion reads all three.

- [ ] **Step 1: Extend the Component schema**

Hand-edit `.serviceContracts.projectStateAccess.$defs.Component.properties` to add these (keep every existing property and the existing `required` list unchanged — all three are optional so old documents stay valid):

```json
{
  "constructionProfile": {"type": ["null", "string"]},
  "provisioning": {"type": ["null", "string"]},
  "uiSurface": {"type": ["null", "boolean"]}
}
```

- [ ] **Step 2: Regenerate and verify**

Run:
```bash
cd server && GOWORK=off make gen-models
grep -n "ConstructionProfile\|Provisioning\|UiSurface" internal/resourceaccess/projectstate/contract.gen.go
```
Expected: all three fields appear on `Component`.

- [ ] **Step 3: Verify the build and the full test suite still pass**

Run: `cd server && GOWORK=off go build ./... && GOWORK=off go test ./...`
Expected: PASS. The fields are additive and optional, so nothing should break.

- [ ] **Step 4: Author the slot-5 amendment**

Hand-edit `slots["5"].model.components`, stamping the attributes on. Leave the slot's `status` untouched (it stays committed — this is a reconciliation amendment, not a new draft cycle):

- `constructionProfile: "generated"` on `web-client`, `mcp-client`, `scheduler-client` (their substance is generated transport: REST handlers, typed clients, MCP tool surfaces, OAS).
- `constructionProfile: "handwritten"` on every other code-layer component (managers, engines, resourceAccess, utilities).
- `provisioning: "vendor"` on `github`, `merchant-gateway`, `construction-pipeline-runtime`, `operated-runtime`.
- `provisioning: "owned"` on `project-git-repo`, `operated-system-state`, `billing-state`, `usage-log`.
- `uiSurface: true` on `web-client` only.

`uiSurface` and `constructionProfile` are **separate axes**: `web-client` is `generated` AND `uiSurface: true`. Its Go transport tier is generator output, while the browser SPA in front of it is real handwritten work — which is what the `U-SPA-*` set accounts for.

Then run the validator and the scoped-diff check from the Global Constraints. `0 errors` is required; the ACT-* advisory count should be unchanged by this task.

- [ ] **Step 5: Verify the amendment landed and matches the fixture**

Run:
```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
python3 - <<'PY'
import json
d = json.load(open('.aiarch/state/project.json'))
live = {c['id']: c for c in d['slots']['5']['model']['components']}
fix = {c['id']: c for c in json.load(open('server/internal/engine/estimation/testdata/system_view.json'))['components']}
bad = 0
for cid, f in fix.items():
    l = live.get(cid)
    if l is None:
        print('MISSING in live:', cid); bad += 1; continue
    for k in ('constructionProfile', 'provisioning', 'uiSurface'):
        if f.get(k) != l.get(k):
            print(f'{cid}.{k}: fixture={f.get(k)!r} live={l.get(k)!r}'); bad += 1
print('OK — fixture and live agree' if bad == 0 else f'{bad} mismatches')
PY
```
Expected: `OK — fixture and live agree`. If not, fix the amendment (the fixture encodes the intended values).

- [ ] **Step 6: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add .aiarch/state/project.json server/internal/resourceaccess/projectstate/
git commit -m "$(cat <<'EOF'
feat(projectstate): typed constructionProfile/provisioning/uiSurface

Doctrine facts move into the System as typed attributes rather than plan
deltas. The generated-transport rule and the vendor/owned resource split
become architectural statements, reviewed as architecture.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 10a: Apply the founder's derivation rulings (provided utilities, no I-*)

Founder rulings taken 2026-08-09 after reviewing the full derived list. Three changes to what the derivation emits, plus their consequences.

**Ruling 1 — utilities backed by off-the-shelf platform components get no coding activity.** All four of this project's utilities are provided, per their own `encapsulates` text: `security` is "Keycloak-backed AuthN/AuthZ", `diagnostics` is a "telemetry reporting surface (OTel)", `logging` is a structured logging sink, and `message-bus` encapsulates the Workflow Execution Substrate over Temporal. Planning construction work for them is the same defect as planning work the generator does.

> Löwy's Table 11-1 *does* give Logging and Security coding activities (6 and 7) — because in that example they are being built. Here they are configured. The rule is about who builds it, not about the layer.

Introduce a third `constructionProfile` value, `"provided"` (platform / third-party supplied), alongside `"generated"`. Both mean **no `C-*` activity**. Unauthored still defaults to `"handwritten"`, so an unknown value conservatively emits work rather than silently dropping it.

**Ruling 2 — drop the `I-*` integration activities entirely.** They have no basis in the worked example: Table 11-1 contains no integration activities, only activity 21 (System Testing, 30d) depending on 5, 19 and 20. `I-UC*` came from `the-method-activity-list`'s draft-job doctrine, which contradicts that same skill's own Step 2 (integration per *component cluster*). The stronger argument is double-counting: App A makes **Integration a phase of every activity's own lifecycle**, so a separate `I-*` activity charges the same work twice.

**Ruling 3 — slot 9 stores the derived list only.** The 14 componentless additives are cut.

**Consequences that must be carried through:**
- **Milestone M4** ("Use Cases Demonstrable") loses its entire fan-in and is removed. M0–M3 remain.
- **`N-IT` re-bases**: its predecessors become the client/SPA construction activities instead of the `I-*` set — structurally what Table 11-1's activity 21 does.
- **`SystemView.CoreUseCaseIDs` becomes dead** and is removed from the contract, along with the `useCaseIDs` parameter on `MaterializeActivityPlan`.
- Derived count goes 49 → **40**.

**Files:**
- Modify: `.aiarch/state/project.json` → `.serviceContracts.estimationEngine` (drop `coreUseCaseIds` from `SystemView`), then regenerate
- Modify: `server/internal/engine/estimation/estimationengine.go` — emission rules, milestones, pattern edges
- Modify: `server/internal/engine/estimation/engine_test.go` — every affected test
- Modify: `server/internal/engine/estimation/testdata/system_view.json` — stamp `provided` on the four utilities
- Modify: `server/internal/manager/projectdesign/projectdesignmanager.go` + `manager_test.go` — drop the `useCaseIDs` parameter

**Steps:**

- [ ] **Step 1: Update the emission rules.** In the coding-activity rule, skip a component whose `ConstructionProfile` is `"generated"` **or** `"provided"`. Delete the integration-activity emission and its helper. Keep the comment explaining *why* provided components are skipped, citing the Table 11-1 contrast above.

- [ ] **Step 2: Update milestones and pattern edges.** Remove M4. Remove the `I-* → N-IT` pattern edges and replace them with edges from the SPA construction activities (`U-SPA-*`) to `N-IT`, so the terminal gate still has a real fan-in.

- [ ] **Step 3: Drop `coreUseCaseIds`.** Remove it from the `SystemView` `$def` in `.serviceContracts.estimationEngine`, bump `version`, run `make gen-models`, and remove the now-dead `useCaseIDs` parameter from `MaterializeActivityPlan` and its callers/tests. Validate state (`0 errors`).

- [ ] **Step 4: Update the fixture and every affected test.** Stamp `constructionProfile: "provided"` on `security`, `logging`, `diagnostics`, `message-bus` in `testdata/system_view.json`. Update the parity tests: the derived count becomes **40**; add assertions that no `C-security` / `C-logging` / `C-diagnostics` / `C-message-bus` is emitted, and that no `I-*` activity is emitted at all. **Both new assertions must be reachable** — the fixture contains all four utilities and five core use cases existed before, so removing either guard must turn the test red. Verify that by mutation and report it.

- [ ] **Step 5: Verify.** `GOWORK=off go test ./internal/engine/estimation/...`, `./internal/manager/projectdesign/...`, `./internal/` (layer gates), `./...`, plus `golangci-lint run ./...`, `make encapsulation-check`, and `aiarch-state-mcp validate --root .` (errors must stay 0).

- [ ] **Step 6: Commit.**

---

### Task 10b: Write the computed plan into slot 9, and gate it against drift

The founder's ruling replaced the original "store deltas, materialize on read" design. That design required every reader to materialize, which is impossible to do cleanly: the layer order is Manager → Engine → ResourceAccess, so `projectstate` can call neither the Manager (where `MaterializeActivityPlan` lives) nor the Engine (which derives). Two live readers — the construction pump's `committedPlanInputs` and SDP-review's `committedActivityList` — would have silently read an empty plan.

**The replacement, which is also how this repo already handles generated artefacts:** slot 9 stores the **computed** activity list, so every existing reader keeps working unchanged. The overrides that produced it are stored alongside as the authored input. A `make` target re-derives and fails on any difference — the same pattern as `contract.gen.go` and `make gen-models-check`.

That drift gate is what makes `ACT-COMPONENT-COVERAGE` deletable in Task 11: re-deriving and comparing is strictly stronger than checking coverage.

**Content:** the 40 derived activities, with the effort/risk **overrides applied** so the committed plan's numbers survive (dropping them would reset every estimate to a band midpoint — every ResourceAccess 10d, every engine 15d — discarding the architect's real sizing judgement). The 14 componentless additives are cut per the founder's ruling. Slot 10 gets the derived dependency rows and milestones M0–M3.

**Files:**
- Modify: `.aiarch/state/project.json` → `slots["9"].model`, `slots["10"].model`, and a new sibling holding the authored overrides
- Modify: `server/Makefile` — a `derived-plan-check` target
- Modify: `server/internal/manager/projectdesign/manager_test.go` — the drift test

**Steps:**

- [ ] **Step 1: Read the current slot 9 fully** before changing it. It is the record of a real, reviewed plan; understand every entry.

- [ ] **Step 2: Author the overrides.** For every derived activity whose committed effort/risk differs from the band default, one override carrying the committed number and a written justification. Use this precedence, and record in the report how many landed in each tier:
  1. The committed **title** explains it — use that.
  2. An **observable, checkable property** explains it — contract operation count in `.serviceContracts`, `encapsulatesVolatilities` length, relationship degree. State the fact and its consequence, e.g. *"systemDesignManager carries 16 contract operations, past the App-C maximum of 12 — the largest manager surface in the system, so 35d rather than the 25d band midpoint."* Verifiable, not narrative.
  3. **Nothing distinguishes it** — say so honestly: *"Carried over from the committed activity list; the original sizing rationale was not recorded. Retained rather than reset to the band default so the reviewed plan's numbers survive the move to derivation — flagged for re-estimation."*

  **Never invent a rationale.** An after-the-fact justification presented as the original reasoning is a forgery, and this is going into committed state. Where an activity deviates *opposite* to its same-kind peers (`C-BM`, `R-BG`), say exactly that and flag it unexplained.

- [ ] **Step 3: Write slots 9 and 10.** Slot 9 = the 40 computed activities with overrides applied. Slot 10 = derived dependencies + milestones M0–M3. Both stay `status: 2` (committed) — a reconciliation amendment, not a new draft cycle. **The three zombies and the three generated-client codings must not appear in any form.**

- [ ] **Step 4: Add the drift gate.** A `make derived-plan-check` target that re-derives from the committed System and fails if the result differs from what slot 9 holds. Back it with a Go test in `manager_test.go` so it runs in the normal suite too. **Prove it fails**: perturb one stored activity's effort, confirm RED, restore, confirm GREEN. That proof is the deliverable.

- [ ] **Step 5: Verify.** `GOWORK=off go test ./...` (many packages read this state), `./internal/` layer gates, `golangci-lint run ./...`, `make encapsulation-check`, and `aiarch-state-mcp validate --root .`. Errors must stay **0**; the advisory count is expected to **drop** as hand-authored activities disappear — report which findings went.

- [ ] **Step 6: Commit**, recording in the message the 69 → 40 reconciliation and the 19 orphaned construction rows (14 additives + 5 integration) as a deliberate consequence.

---

### Task 7b: Consolidate the estimation package to the layer file-layout standard

Tasks 3–6 created seven handwritten files in `server/internal/engine/estimation/`. Every one violates Rule 4 of the layer file-layout standard, and `TestFileLayout` — live with **zero waivers** — fails on the branch while passing on `main`. It went undetected for four tasks because those tasks ran only `go test ./internal/engine/estimation/...`; the gate lives in `internal/arch_test.go`, which that path never loads.

This is a regression introduced by this plan. It is fixed by moving code, not by weakening the gate: `.golangci.yml`, `arch_test.go`, and the standard itself are all off-limits.

**Files:**
- Delete (folding their contents into the two files below): `derive.go`, `derive_edges.go`, `derive_deltas.go`, `derive_test.go`, `derive_edges_test.go`, `derive_deltas_test.go`, `derive_parity_test.go`
- Modify: `server/internal/engine/estimation/estimationengine.go` — receives all three implementation files' contents
- Modify: `server/internal/engine/estimation/engine_test.go` — receives all four test files' contents
- Untouched: `testdata/system_view.json` (exempt), `contract.gen.go`, `fake/fake.gen.go`

**Interfaces:** unchanged. This task moves code between files and changes nothing else — every function keeps its name, signature, and body.

- [ ] **Step 1: Confirm the gate fails, and exactly why**

```bash
cd server && GOWORK=off go test ./internal/ -run TestFileLayout
```
Expected: FAIL, naming the seven files. Record the output — it is the before-state.

- [ ] **Step 2: Fold the three implementation files into `estimationengine.go`**

Append the full contents of `derive.go`, `derive_edges.go` and `derive_deltas.go` to `estimationengine.go`, minus their `package` clauses and import blocks. Merge their imports into the existing block (`sort` is likely already there; add what is missing). Each file's header comment becomes a section comment introducing that block of functions — keep the prose, it carries the Löwy citations and the reasoning for the delta vocabulary. Delete the three files.

Rule 1 explicitly sanctions the resulting size: *"Large files are an accepted consequence — `projectstateaccess.go` folds ~40 source files."*

- [ ] **Step 3: Fold the four test files into `engine_test.go`**

Append the contents of `derive_test.go`, `derive_edges_test.go`, `derive_deltas_test.go` and `derive_parity_test.go` to the existing `engine_test.go`, minus `package` clauses, merging imports. Watch for helper-name collisions with the pre-existing CPM tests (`usd`, `approx`, `eps`, `serialChainOption`). If one collides, rename the NEWER helper and say so in the report — never delete or weaken an existing test to resolve a collision. Delete the four files.

- [ ] **Step 4: Verify the gate passes and nothing else moved**

```bash
cd server
GOWORK=off go test ./internal/ -run 'TestFileLayout|TestMethodLayering'
GOWORK=off go test ./internal/engine/estimation/...
GOWORK=off golangci-lint run ./internal/engine/estimation/...
GOWORK=off make encapsulation-check
```
All must pass. The estimation test count must be **identical** to before the fold — report the before and after numbers explicitly. A dropped test is the main risk in this task.

- [ ] **Step 5: Commit**

```bash
git add -A server/internal/engine/estimation/
git commit -m "$(cat <<'EOF'
refactor(estimation): fold derivation into the layer file-layout standard

Tasks 3-6 created seven handwritten files; the standard allows exactly one
implementation file (estimationengine.go) and one test file
(engine_test.go). TestFileLayout is live with zero waivers and was failing
on this branch while passing on main.

Code moved only — no signature, behaviour, or test-assertion changes.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Wire the Manager to DerivePlan

The Engine imports no `projectstate`, so the Manager converts at the call boundary — the same Option B pattern `toEstimationOption` already uses.

**Files:**
- Modify: `server/internal/manager/projectdesign/projectdesignmanager.go` (append — Rule 1; do NOT create deriveplan.go)
- Modify: `server/internal/manager/projectdesign/manager_test.go` (append — Rule 3)

**Interfaces:**
- Consumes: `estimation.SystemView`, `estimation.ActivityListDeltas`, `estimation.DerivedPlan`, `EstimationEngine.DerivePlan` (Tasks 1, 5); `projectstate.Component` attributes (Task 7).
- Produces: `toEstimationSystemView(sys projectstate.System) estimation.SystemView` and `toProjectStateActivityList(plan estimation.DerivedPlan) projectstate.ActivityList`. Task 10 calls both from the read path.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/manager/projectdesign/manager_test.go` (Rule 3):

```go
package projectdesign

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestToEstimationSystemViewCarriesTheTypedAttributes(t *testing.T) {
	sys := projectstate.System{
		Components: []projectstate.Component{
			{ID: "order-manager", Name: "OrderManager", Kind: projectstate.CompManager},
			{ID: "web-client", Name: "WebClient", Kind: projectstate.CompClient},
		},
	}
	sys.Components[0].ConstructionProfile = strPtr("handwritten")
	sys.Components[1].ConstructionProfile = strPtr("generated")
	sys.Components[1].UiSurface = boolPtr(true)

	got := toEstimationSystemView(sys)
	if len(got.Components) != 2 {
		t.Fatalf("converted %d components, want 2", len(got.Components))
	}
	byID := map[string]estimation.SystemComponent{}
	for _, c := range got.Components {
		byID[c.ID] = c
	}
	if byID["order-manager"].Kind != "manager" {
		t.Errorf("manager kind = %q, want %q", byID["order-manager"].Kind, "manager")
	}
	if byID["web-client"].ConstructionProfile != "generated" || !byID["web-client"].UiSurface {
		t.Errorf("web-client = %+v, want generated with a UI surface", byID["web-client"])
	}
}

// A component with no authored constructionProfile must default to handwritten — the
// conservative direction. Defaulting to "generated" would silently delete real work.
func TestToEstimationSystemViewDefaultsToHandwritten(t *testing.T) {
	sys := projectstate.System{Components: []projectstate.Component{
		{ID: "x", Name: "X", Kind: projectstate.CompEngine},
	}}
	got := toEstimationSystemView(sys)
	if got.Components[0].ConstructionProfile != "handwritten" {
		t.Errorf("default profile = %q, want handwritten", got.Components[0].ConstructionProfile)
	}
}

func TestToProjectStateActivityListRoundTrips(t *testing.T) {
	plan := estimation.DerivedPlan{Activities: []estimation.DerivedActivity{
		{Name: "C-x", Title: "Build X", EffortDays: 15, RiskBucket: 3,
			WorkerClass: "junior-developer", Coding: true, ComponentID: "x", Derived: true},
	}}
	got := toProjectStateActivityList(plan)
	if len(got.Activities) != 1 {
		t.Fatalf("converted %d activities, want 1", len(got.Activities))
	}
	a := got.Activities[0]
	if a.Name != "C-x" || a.EffortDays != 15 || a.RiskBucket != 3 ||
		a.WorkerClass != "junior-developer" || !a.Coding || a.ComponentID != "x" {
		t.Errorf("converted activity = %+v", a)
	}
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && GOWORK=off go test ./internal/manager/projectdesign/ -run 'TestToEstimation|TestToProjectState' -v`
Expected: FAIL — `undefined: toEstimationSystemView`, `undefined: toProjectStateActivityList`.

- [ ] **Step 3: Implement the conversion boundary**

Append to `server/internal/manager/projectdesign/projectdesignmanager.go` (Rule 1 — do NOT create a separate file):

```go
// deriveplan.go is the projectstate ↔ estimation conversion boundary for the Phase-2
// derivation (Option B full encapsulation: the Engine redefines every domain type it
// uses as its own generated def and imports NO projectstate, so the Manager maps
// field-by-field here — exactly as toEstimationOption already does for EstimateForOption).
package projectdesign

import (
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// componentKindName renders the canonical component kind as the plain string the Engine's
// slim view speaks. The Engine deliberately does not import projectstate's enum.
func componentKindName(k projectstate.ComponentKind) string {
	switch k {
	case projectstate.CompClient:
		return "client"
	case projectstate.CompManager:
		return "manager"
	case projectstate.CompEngine:
		return "engine"
	case projectstate.CompResourceAccess:
		return "resourceAccess"
	case projectstate.CompResource:
		return "resource"
	case projectstate.CompUtility:
		return "utility"
	}
	return ""
}

func derefString(p *string, fallback string) string {
	if p == nil || *p == "" {
		return fallback
	}
	return *p
}

func derefBool(p *bool) bool { return p != nil && *p }

// toEstimationSystemView converts the canonical System to the Engine's OWN slim view.
// Only what the derivation reads crosses: identity, kind, and the three typed doctrine
// attributes.
//
// An unauthored constructionProfile defaults to "handwritten" — the CONSERVATIVE
// direction. Defaulting to "generated" would silently delete real planned work, which is
// the one failure mode this whole design exists to prevent.
func toEstimationSystemView(sys projectstate.System) estimation.SystemView {
	comps := make([]estimation.SystemComponent, 0, len(sys.Components))
	for _, c := range sys.Components {
		comps = append(comps, estimation.SystemComponent{
			ID:                  c.ID,
			Name:                c.Name,
			Kind:                componentKindName(c.Kind),
			ConstructionProfile: derefString(c.ConstructionProfile, "handwritten"),
			Provisioning:        derefString(c.Provisioning, "owned"),
			UiSurface:           derefBool(c.UiSurface),
		})
	}
	rels := make([]estimation.SystemRelationship, 0, len(sys.Relationships))
	for _, r := range sys.Relationships {
		rels = append(rels, estimation.SystemRelationship{From: r.From, To: r.To})
	}
	return estimation.SystemView{Components: comps, Relationships: rels}
}

// toProjectStateActivityList converts the derived plan back to the canonical
// ActivityList shape every existing reader already consumes (the SPA catalog projection,
// the construction pump, earned value).
func toProjectStateActivityList(plan estimation.DerivedPlan) projectstate.ActivityList {
	acts := make([]projectstate.ActivityItem, 0, len(plan.Activities))
	for _, a := range plan.Activities {
		acts = append(acts, projectstate.ActivityItem{
			Name:        a.Name,
			Title:       a.Title,
			EffortDays:  a.EffortDays,
			RiskBucket:  int(a.RiskBucket),
			WorkerClass: a.WorkerClass,
			Coding:      a.Coding,
			ComponentID: a.ComponentID,
		})
	}
	return projectstate.ActivityList{Activities: acts}
}
```

> **Note:** the `CoreUseCaseIDs` field is left unset here. Wire it from the committed `.coreUseCases` slot when the read path is assembled in Task 10 — `toEstimationSystemView` takes only the System.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd server && GOWORK=off go test ./internal/manager/projectdesign/ -run 'TestToEstimation|TestToProjectState' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Run the full suite**

Run: `cd server && GOWORK=off go test ./... && GOWORK=off make encapsulation-check && GOWORK=off make method-check`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/internal/manager/projectdesign/
git commit -m "$(cat <<'EOF'
feat(projectdesign): projectstate to estimation conversion for DerivePlan

Option B boundary conversion, mirroring toEstimationOption. Unauthored
constructionProfile defaults to handwritten — the conservative direction,
since defaulting to generated would silently delete real planned work.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: Historical activity-ID alias map

Derived ids are `C-<component-id>`; the 69 rows in `.activityConstruction` are keyed by hand-chosen short names. The founder ratified an alias map over a key rewrite — rewriting Done+Integrated construction records to gain cosmetic key consistency is risk with no payoff.

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` (append — Rule 1; do NOT create activityalias.go)
- Modify: `server/internal/resourceaccess/projectstate/access_test.go` (append — Rule 3)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `ResolveActivityAlias(historical string) (canonical string, ok bool)` and `HistoricalAliasFor(canonical string) (historical string, ok bool)`. Task 10's read path calls both.

- [ ] **Step 1: Generate the alias pairs from live state**

Run:
```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
python3 - <<'PY'
import json
d = json.load(open('.aiarch/state/project.json'))
al = d['slots']['9']['model']['activities']
pairs = []
for a in al:
    cid = a.get('componentId')
    if not cid:
        continue
    prefix = a['name'].split('-')[0]
    if prefix not in ('C', 'R'):
        continue          # U-SPA-* re-derive per manager; not a 1:1 rename
    pairs.append((a['name'], f'{prefix}-{cid}'))
for h, c in sorted(pairs):
    print(f'\t"{h}": "{c}",')
print('# pairs:', len(pairs))
PY
```

The output is Go **map-literal** syntax (`"C-BM": "C-billing-manager",`), ready to paste straight into the `activityAliases` map in Step 4.
Copy the emitted lines into Step 3's table. Note only `C-*`/`R-*` with a `componentId` get a 1:1 alias — the `U-SPA-*` set is re-derived per manager and the three zombies have no canonical counterpart by design.

- [ ] **Step 2: Write the failing test**

Append to `server/internal/resourceaccess/projectstate/access_test.go` (Rule 3):

```go
package projectstate

import "testing"

func TestResolveActivityAliasMapsHistoricalShortNames(t *testing.T) {
	canonical, ok := ResolveActivityAlias("C-BM")
	if !ok {
		t.Fatal("C-BM did not resolve; every historical construction key must resolve")
	}
	if canonical != "C-billing-manager" {
		t.Errorf("C-BM resolved to %q, want C-billing-manager", canonical)
	}
}

func TestResolveActivityAliasIsInjective(t *testing.T) {
	seen := map[string]string{}
	for historical, canonical := range activityAliases {
		if prior, dup := seen[canonical]; dup {
			t.Errorf("canonical id %q is claimed by both %q and %q", canonical, prior, historical)
		}
		seen[canonical] = historical
	}
}

func TestHistoricalAliasForRoundTrips(t *testing.T) {
	for historical, canonical := range activityAliases {
		got, ok := HistoricalAliasFor(canonical)
		if !ok || got != historical {
			t.Errorf("round trip failed for %q: got %q, ok=%v", historical, got, ok)
		}
	}
}

// An unknown key must report ok=false rather than silently returning the input. A silent
// pass-through would make a typo look like a valid activity.
func TestResolveActivityAliasReportsUnknownKeys(t *testing.T) {
	if _, ok := ResolveActivityAlias("C-NOPE"); ok {
		t.Error("an unknown historical key must report ok=false")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestResolveActivityAlias -v`
Expected: FAIL — `undefined: ResolveActivityAlias`.

- [ ] **Step 4: Implement the alias map**

Append to `server/internal/resourceaccess/projectstate/projectstateaccess.go` (Rule 1 — do NOT create a separate file):

```go
// activityalias.go maps the HISTORICAL hand-chosen activity short names (C-BM, C-AA, …)
// to the DERIVED canonical ids (C-<component-id>).
//
// Why an alias map and not a key rewrite: all 69 rows in .activityConstruction are keyed
// by the historical short names and every one is Done+Integrated. Rewriting completed
// construction records to gain cosmetic key consistency is risk with no payoff (founder
// ruling, 2026-08-09). The short name survives as a render label; the canonical id is
// what the derivation produces and what new state keys off.
//
// Only C-* / R-* activities that carried a componentId get a 1:1 alias. The U-SPA-* set
// is re-derived per MANAGER (a different decomposition, not a rename), and the three
// zombie activities (C-HE, C-WIA, R-WIT) have no canonical counterpart by design — they
// name components that do not exist.
package projectstate

// activityAliases maps historical short name → derived canonical id.
var activityAliases = map[string]string{
	// PASTE the generated pairs from Step 1 here.
}

// canonicalToHistorical is the reverse index, built once at init from activityAliases so
// the two directions can never drift apart.
var canonicalToHistorical = func() map[string]string {
	m := make(map[string]string, len(activityAliases))
	for historical, canonical := range activityAliases {
		m[canonical] = historical
	}
	return m
}()

// ResolveActivityAlias maps a historical activity key to its derived canonical id.
// ok is false for an unknown key — never a silent pass-through, which would make a typo
// look like a valid activity.
func ResolveActivityAlias(historical string) (string, bool) {
	c, ok := activityAliases[historical]
	return c, ok
}

// HistoricalAliasFor maps a derived canonical id back to the historical short name that
// existing .activityConstruction rows are keyed by.
func HistoricalAliasFor(canonical string) (string, bool) {
	h, ok := canonicalToHistorical[canonical]
	return h, ok
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestResolveActivityAlias|TestHistoricalAliasFor' -v`
Expected: PASS (4 tests).

- [ ] **Step 6: Verify every historical construction key resolves**

Run:
```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
python3 - <<'PY'
import json, re
d = json.load(open('.aiarch/state/project.json'))
keys = set(d['activityConstruction'].keys())
src = open('server/internal/resourceaccess/projectstate/activityalias.go').read()
aliased = set(re.findall(r'"([^"]+)":\s*"[^"]+",', src))
coding = {k for k in keys if k.split('-')[0] in ('C', 'R')}
missing = coding - aliased - {'C-HE', 'C-WIA', 'R-WIT'}
print('unaliased C-/R- keys (excluding the 3 known zombies):', sorted(missing) or 'none')
PY
```
Expected: `none`. Anything listed is a gap in the alias table — add it.

- [ ] **Step 7: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/internal/resourceaccess/projectstate/
git commit -m "$(cat <<'EOF'
feat(projectstate): historical activity-ID alias map

Derived ids are C-<component-id>; the 69 Done+Integrated construction
rows are keyed by hand-chosen short names. Alias map over key rewrite
per founder ruling — rewriting completed records for cosmetic key
consistency is risk with no payoff.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 10: Store deltas, materialize on read

The flip. Everything before this was additive and behaviour-preserving; this is the change that makes derivation authoritative.

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` (the `ActivityList` slot read path)
- Modify: `server/internal/manager/projectdesign/deriveplan.go`
- Modify: `.aiarch/state/project.json` `slots["9"]` and `slots["10"]` (hand-edit; both stay committed — reconciliation amendment)

**Interfaces:**
- Consumes: `toEstimationSystemView`, `toProjectStateActivityList` (Task 8); `DerivePlan` (Task 5); `ResolveActivityAlias` (Task 9) — **this task is what makes Task 9's alias map load-bearing**; before it, nothing called it.
- Produces: `MaterializeActivityPlan(sys projectstate.System, useCaseIDs []string, deltas estimation.ActivityListDeltas) (projectstate.ActivityList, []projectstate.NetworkDependency, error)` on the Manager — the single render-on-read entry point.

- [ ] **Step 1: Write the failing test**

Append further to `server/internal/manager/projectdesign/manager_test.go`:

```go
func TestMaterializeActivityPlanProducesTheReaderShape(t *testing.T) {
	sys := projectstate.System{Components: []projectstate.Component{
		{ID: "order-manager", Name: "OrderManager", Kind: projectstate.CompManager},
		{ID: "order-access", Name: "OrderAccess", Kind: projectstate.CompResourceAccess},
	}}
	list, deps, err := MaterializeActivityPlan(sys, []string{"UC1"}, estimation.ActivityListDeltas{})
	if err != nil {
		t.Fatalf("MaterializeActivityPlan: %v", err)
	}
	if len(list.Activities) == 0 {
		t.Fatal("materialized an empty activity list for a populated System")
	}
	var sawManager bool
	for _, a := range list.Activities {
		if a.Name == "C-order-manager" && a.ComponentID == "order-manager" {
			sawManager = true
		}
	}
	if !sawManager {
		t.Error("materialized list missing C-order-manager")
	}
	if len(deps) == 0 {
		t.Error("materialized no dependency rows")
	}
}

// loadCommittedStateForTest reads the repo's own committed project document and returns
// the slot-5 System, the core use-case ids, and the historical .activityConstruction
// keys. It reads LIVE state rather than a fixture on purpose: the risk this test guards
// against is one specific historical key having no derived counterpart, which a
// synthetic fixture cannot reproduce.
//
// The path is resolved relative to this test file's package directory
// (server/internal/manager/projectdesign), so it does not depend on the caller's cwd.
func loadCommittedStateForTest(t *testing.T) (projectstate.System, []string, []string) {
	t.Helper()
	const rel = "../../../../.aiarch/state/project.json"
	raw, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read committed project state at %s: %v", rel, err)
	}
	var doc struct {
		Slots map[string]struct {
			Model json.RawMessage `json:"model"`
		} `json:"slots"`
		ActivityConstruction map[string]json.RawMessage `json:"activityConstruction"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode committed project state: %v", err)
	}

	var sys projectstate.System
	if err := json.Unmarshal(doc.Slots["5"].Model, &sys); err != nil {
		t.Fatalf("decode slot 5 (System): %v", err)
	}
	if len(sys.Components) == 0 {
		t.Fatal("slot 5 decoded to zero components - the test would be vacuous")
	}

	// Core use cases live in their own slot; ids only, best effort. If the shape does
	// not match, the derivation simply emits no I-* activities, which does not affect
	// what this test asserts.
	var useCases struct {
		UseCases []struct {
			ID string `json:"id"`
		} `json:"useCases"`
	}
	for _, slot := range doc.Slots {
		if err := json.Unmarshal(slot.Model, &useCases); err == nil && len(useCases.UseCases) > 0 {
			break
		}
	}
	ids := make([]string, 0, len(useCases.UseCases))
	for _, uc := range useCases.UseCases {
		ids = append(ids, uc.ID)
	}

	keys := make([]string, 0, len(doc.ActivityConstruction))
	for k := range doc.ActivityConstruction {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		t.Fatal("activityConstruction decoded to zero keys - the test would be vacuous")
	}
	return sys, ids, keys
}

// THE JOIN THAT MUST NOT BREAK. Once the derivation is authoritative, slot 9 renders
// activities named C-<component-id> while the 69 rows in .activityConstruction remain
// keyed by their hand-chosen short names (C-BM, R-GH, …), every one Done+Integrated.
// Any code joining plan to construction state therefore needs the alias to resolve, and
// nothing else in stage 1 exercises that seam — so it is asserted here, over the REAL
// committed key set rather than a fixture, because the risk is a specific historical key
// having no derived counterpart.
func TestEveryHistoricalConstructionKeyResolvesToADerivedActivity(t *testing.T) {
	sys, useCaseIDs, historicalKeys := loadCommittedStateForTest(t)
	list, _, err := MaterializeActivityPlan(sys, useCaseIDs, estimation.ActivityListDeltas{})
	if err != nil {
		t.Fatalf("MaterializeActivityPlan: %v", err)
	}
	derived := make(map[string]bool, len(list.Activities))
	for _, a := range list.Activities {
		derived[a.Name] = true
	}

	// The zombies (components that no longer exist) and the componentless additive
	// R-DER have no derived counterpart BY DESIGN — a derived list never emits them.
	noCounterpart := map[string]bool{"C-HE": true, "C-WIA": true, "R-WIT": true, "R-DER": true}

	for _, historical := range historicalKeys {
		if noCounterpart[historical] {
			continue
		}
		canonical, ok := projectstate.ResolveActivityAlias(historical)
		if !ok {
			// U-SPA-* and N-* are re-derived or arrive as additive deltas rather than
			// being renamed, so they legitimately have no alias.
			if strings.HasPrefix(historical, "U-SPA-") || strings.HasPrefix(historical, "N-") {
				continue
			}
			t.Errorf("historical construction key %q has no alias; its Done+Integrated history would be orphaned", historical)
			continue
		}
		if !derived[canonical] {
			t.Errorf("historical key %q aliases to %q, which the derivation does not emit", historical, canonical)
		}
	}
}

// A delta document that violates the vocabulary must fail the READ, loudly. A silently
// dropped bad delta is the zombie failure mode returning by another door.
func TestMaterializeActivityPlanPropagatesDeltaErrors(t *testing.T) {
	sys := projectstate.System{Components: []projectstate.Component{
		{ID: "order-manager", Name: "OrderManager", Kind: projectstate.CompManager},
	}}
	_, _, err := MaterializeActivityPlan(sys, nil, estimation.ActivityListDeltas{
		Overrides: []estimation.ActivityOverride{{Activity: "C-gone", Justification: "j"}},
	})
	if err == nil {
		t.Fatal("a delta naming an underived activity must fail the read")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && GOWORK=off go test ./internal/manager/projectdesign/ -run TestMaterializeActivityPlan -v`
Expected: FAIL — `undefined: MaterializeActivityPlan`.

- [ ] **Step 3: Implement the render-on-read entry point**

Append further to `server/internal/manager/projectdesign/projectdesignmanager.go`:

```go
// MaterializeActivityPlan is the single render-on-read entry point for the Phase-2 plan:
// it derives the baseline from the committed System, applies the authored deltas, and
// returns the canonical shapes every existing reader already consumes.
//
// A delta that violates the vocabulary fails the READ, loudly. Silently dropping a bad
// delta would be the zombie failure mode returning by another door.
func MaterializeActivityPlan(
	sys projectstate.System,
	useCaseIDs []string,
	deltas estimation.ActivityListDeltas,
) (projectstate.ActivityList, []projectstate.NetworkDependency, error) {
	view := toEstimationSystemView(sys)
	view.CoreUseCaseIDs = useCaseIDs

	plan, err := estimation.NewEstimationEngine().DerivePlan(fweng.Context{}, view, deltas)
	if err != nil {
		return projectstate.ActivityList{}, nil, err
	}

	deps := make([]projectstate.NetworkDependency, 0, len(plan.Dependencies))
	for _, d := range plan.Dependencies {
		deps = append(deps, projectstate.NetworkDependency{Activity: d.Activity, DependsOn: d.DependsOn})
	}
	return toProjectStateActivityList(plan), deps, nil
}
```

> `estimation.NewEstimationEngine()` is the exported constructor (`contract.gen.go:137`, returns `EstimationEngineImpl{}`); the Engine is stateless, so constructing one per call is free and matches how `engine_test.go` already calls it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd server && GOWORK=off go test ./internal/manager/projectdesign/ -v`
Expected: PASS.

- [ ] **Step 5: Author the slot-9 and slot-10 delta documents**

Hand-edit `slots["9"].model` to replace the materialized activity list with the delta document (and `slots["10"].model` correspondingly), keeping both slots committed. Derive the overrides mechanically from the current committed list: for each derived activity whose committed `effortDays`/`riskBucket` differs from the derived default, emit one override carrying the committed number and a justification naming why that component is off its band midpoint. Emit additive entries for the 11 checklist noncoding activities (`N-SCHEMA`, `N-CI`, `N-SEC`, `N-ADR`, `N-RUN`, `N-REQ`, `N-ARCH`, `N-PLAN`, `N-SC`, `N-DEP`, `N-HARD`), `R-DER`, `U-SPA-6`, and `U-SPA-TEAM`, each with its own incident edges and a justification.

Generate the starting point:
```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
python3 - <<'PY'
import json
d = json.load(open('.aiarch/state/project.json'))
al = d['slots']['9']['model']['activities']
default = {'manager': 25, 'engine': 15, 'resourceAccess': 10, 'client': 25, 'utility': 10, 'resource': 10}
kind = {c['id']: c['kind'] for c in d['slots']['5']['model']['components']}
for a in al:
    cid = a.get('componentId')
    if not cid or not a['name'].startswith('C-'):
        continue
    want = default.get(kind.get(cid), 10)
    if a['effortDays'] != want:
        print(f"OVERRIDE {a['name']} -> C-{cid}: committed {a['effortDays']}d vs derived default {want}d")
PY
```
Every line printed needs an override with a justification. Do **not** invent justifications wholesale — use the committed activity `title` as the evidence for why that component is bigger or smaller than its band.

- [ ] **Step 6: Verify the materialized read reproduces the intended plan**

Run: `cd server && GOWORK=off go test ./... `
Expected: PASS. Then boot the app locally and confirm the Phase-2 screens still render the activity list (see the run-app-locally notes: `GOWORK=off`, `CONSTRUCTION_DRYRUN`).

- [ ] **Step 7: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add .aiarch/state/project.json server/
git commit -m "$(cat <<'EOF'
feat: store activity deltas, materialize the plan on read

Slots 9 and 10 stop storing materialized lists. MaterializeActivityPlan
derives from the committed System and applies the authored deltas; a
delta that violates the vocabulary fails the read loudly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 11: Delete the ACT-* coverage gates

Coverage is now true by construction, which is strictly stronger than a gate. This removes the rule class that has been blocking construction phase-artifact writes since 2026-07-10.

**Files:**
- Modify: `server/cmd/aiarch-state-mcp/crossartifact.go`
- Modify: `server/cmd/aiarch-state-mcp/staleness.go`
- Modify: `server/cmd/aiarch-state-mcp/crossartifact_test.go`

**Interfaces:**
- Consumes: the derivation being authoritative (Task 10).
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Confirm exactly what is in scope**

Run:
```bash
cd server
grep -n "ACT-COMPONENT-COVERAGE\|ACT-UNKNOWN-COMPONENT\|activityCoverageFindings\|deriveActivityComponent\|systemActivityListJoinRules\|activityListStaleDowngradeNote" cmd/aiarch-state-mcp/*.go
```

**In scope for deletion:** `activityCoverageFindings`, `deriveActivityComponent` and its helpers, the `ACT-` entry in `ruleSlotAttributionPrefixes`, `systemActivityListJoinRules`, `activityListStaleDowngradeNote`.

**NOT in scope — do not touch:** `PA-RATECARD-KEYS`, `PA-RATECARD-DEFAULTED`, `PA-INFRA-KIND`, `PA-TERMS-REGIME`, `rateCardFindings`, `paEnumHoleFindings`, and the `designhealth` DH-* tier. They are unrelated rule families that share the same file and enforcement seam.

- [ ] **Step 2: Write the failing test**

Replace the ACT-* assertions in `server/cmd/aiarch-state-mcp/crossartifact_test.go` with a test asserting the rules are gone. Add:

```go
// ACT-* is deleted: coverage is now true by construction (the activity list is DERIVED
// from the committed System), which is strictly stronger than a validation gate. A
// lingering ACT-* finding would reject writes for drift that can no longer exist.
func TestACTRulesAreRetired(t *testing.T) {
	p := driftFixtureProject(true)
	findings := appendAppSideCrossArtifactFindings(p, nil)
	for _, f := range findings {
		if strings.HasPrefix(f.RuleID, "ACT-") {
			t.Errorf("ACT-* rule %q still fires; coverage is now structural", f.RuleID)
		}
	}
}

// The PA-* and DH-* families share this file and enforcement seam and must SURVIVE the
// ACT-* deletion.
func TestPARulesSurviveTheACTDeletion(t *testing.T) {
	p := driftFixtureProject(false)
	findings := appendAppSideCrossArtifactFindings(p, nil)
	var sawPA bool
	for _, f := range findings {
		if strings.HasPrefix(f.RuleID, "PA-") {
			sawPA = true
		}
	}
	if !sawPA {
		t.Error("no PA-* finding survived; the deletion took out an unrelated rule family")
	}
}
```

Add `"strings"` to the test file's imports. If `driftFixtureProject(false)` produces no PA-* finding, adjust the fixture so it carries a rate-card orphan — the point is to prove PA-* still fires.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd server && GOWORK=off go test ./cmd/aiarch-state-mcp/ -run 'TestACTRulesAreRetired|TestPARulesSurvive' -v`
Expected: `TestACTRulesAreRetired` FAILS (ACT-* still fires).

- [ ] **Step 4: Delete the ACT-* rules**

In `crossartifact.go`: remove the `activityCoverageFindings` call from `appendAppSideCrossArtifactFindings`, delete the function, delete `deriveActivityComponent` / `isIntegrationActivity` / `isCodeComponentKind` / `committedActivityList` if they become unused, and delete the ACT-* block from the package doc comment. Replace it with a short note recording that coverage moved from validation to derivation and pointing at the spec.

In `staleness.go`: delete `systemActivityListJoinRules`, `activityListStaleDowngradeNote`, their call sites, and the `{"ACT-", projectstate.KindActivityList, attribSlot}` entry in `ruleSlotAttributionPrefixes`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd server && GOWORK=off go test ./cmd/aiarch-state-mcp/... -v`
Expected: PASS. Fix any test that asserted on the deleted rules by deleting that assertion — do **not** weaken a surviving rule to make a test pass.

- [ ] **Step 6: Verify the whole suite and gates**

Run: `cd server && GOWORK=off go test ./... && GOWORK=off make lint && GOWORK=off make method-check`
Expected: all PASS, no unused-symbol lint errors from the deletion.

- [ ] **Step 7: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/cmd/aiarch-state-mcp/
git commit -m "$(cat <<'EOF'
refactor: retire the ACT-* coverage gates

Coverage is now true by construction: the activity list is derived from
the committed System, so System x ActivityList drift cannot exist. Removes
the fuzzy longest-key-containment join and the staleness downgrade that
propped it up. PA-* and DH-* families untouched.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 12: Update the Method doctrine skills

The draft-job doctrine currently tells the agent to author the list. It must now tell the agent to author the deltas.

**Files:**
- Modify: `.claude/skills/the-method-activity-list/SKILL.md`
- Modify: `.claude/commands/activity-list-draft.md`

**Interfaces:**
- Consumes: the shipped behaviour of Tasks 1–11.
- Produces: nothing.

- [ ] **Step 1: Rewrite the Output section**

In `.claude/skills/the-method-activity-list/SKILL.md`, replace the "Output" section so it states that `.activityList` holds a **delta document**, not a materialized list: the baseline is derived by `estimationEngine.DerivePlan` from the committed System, and the agent authors only justified effort/risk overrides plus genuinely componentless additive activities.

- [ ] **Step 2: Rewrite the Draft-job doctrine section**

Replace the "Draft-job doctrine (CI dispatch)" task statement. The new task is:

1. Read the derived baseline (it is a render-on-read of the committed System).
2. For each derived activity whose band-midpoint default is wrong, author an override with a written justification.
3. Walk the ch. 13 noncoding checklist and author an additive activity for each item that applies, with its own incident edges and a justification.
4. Do **not** author `C-*`, `R-*`, `U-SPA-<manager>`, `G-SPA`, `I-*`, or the always-emit `N-*` inventory — they derive.

State the closed vocabulary explicitly: **no exclusions, no derived-edge overrides.** If a component needs no work it should not be a component; if a derived edge is wrong, the relationship is wrong — amend the System.

- [ ] **Step 3: Update the anti-patterns and exit criteria**

Add an anti-pattern: *"Authoring a derived activity by hand"* — a coding activity typed into the delta document is either a duplicate of the derived one or a zombie; the three zombies (`C-HE`, `C-WIA`, `R-WIT`) are what this prevents.

Update "Exit criteria" to describe the delta document rather than the materialized list.

- [ ] **Step 4: Update the command**

In `.claude/commands/activity-list-draft.md`, update step 3 so the drafted model is the delta document.

- [ ] **Step 5: Verify the materialized-assets drift gate**

Run:
```bash
cd server && GOWORK=off make claude-assets && cd .. && git status --short .claude/
```
Expected: if `.claude` is materialized from method-assets, either the gate passes or it flags the skill as locally modified — resolve per the existing drift-gate convention rather than reverting the edit.

- [ ] **Step 6: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add .claude/
git commit -m "$(cat <<'EOF'
docs(method): activity-list doctrine authors deltas, not the list

The baseline derives from the committed System. The draft job now authors
justified effort/risk overrides and componentless additive activities
only — no exclusions, no derived-edge overrides.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

## Verification checklist

Run before declaring stage 1 complete:

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator/server
GOWORK=off go test ./...              # full suite
GOWORK=off make gen-models-check      # codegen drift
GOWORK=off make encapsulation-check   # engine purity
GOWORK=off make method-check          # Method design gate
GOWORK=off make lint                  # linters
GOWORK=off go test ./internal/         # TestFileLayout + TestMethodLayering — the LAYER GATES.
                                      # Package-scoped runs do NOT fire these; run them per task.
```

All five must pass. Then confirm in the running app that the Phase-2 activity-list and network screens render the materialized plan.

## Out of scope (stage 2)

The compression move catalog (`simulator`, `designFirst`, `topResources`, `split`), the candidate-site search, and surfacing the applied move list in `.sdpReview`. `criticalSpeedup` stays exactly as it is until that plan lands — it is the only compression lever the system has.
