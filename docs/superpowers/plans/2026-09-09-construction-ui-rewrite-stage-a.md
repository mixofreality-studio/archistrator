# Construction UI Rewrite — Stage A: honest data model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the server tell the truth about construction — a 12-task Method vocabulary, an append-only `TaskAttempt` ledger with mandatory provenance, an honest classifier that refuses to guess, and a layer projection — so the three UI lenses in Stages B–D can be built without fabricating anything.

**Architecture:** All new domain types live in the `projectstate` ResourceAccess (`projectstateaccess.go`), which is the git-as-DB driver. Wire types are NOT hand-written: they are declared in `.aiarch/state/project.json` → `.serviceContracts.systemDesignManager.$defs` and emitted to Go by `make gen-models`, to OpenAPI by `make gen-client`, and to TypeScript by `npm run gen:api` in `webApp/`. The `gen-uiprofiles` generator carries the phase/task vocabulary into `lifecycleTemplates.gen.ts`. Every generator has a `-check` drift gate in CI; a task that changes a generator input must run the generator in the same commit.

**Tech Stack:** Go 1.26 (`GOWORK=off` always — `go.work` points at stale sibling checkouts), standard `testing` package, Temporal, TypeScript 5.9 / React 19 / Vite, `openapi-typescript`.

**Spec:** `docs/superpowers/specs/2026-09-09-construction-ui-rewrite-design.md`

## Global Constraints

- **Always build and test with `GOWORK=off`.** A plain `go build` fails to compile against the stale sibling platform checkouts. Every Go command in this plan already carries it.
- **Run Go commands from `server/`.** Run `npm` commands from `webApp/`.
- **Never hand-edit a `*.gen.go` or `*.gen.ts` file.** Change the generator or the contract in `project.json`, then run the generator.
- **Never hand-edit `.aiarch/state/project.json` state slots.** Contract `$defs` edits are the one exception in this stage and are called out explicitly in Task 6.
- **`project.json` is compiler input**, not just state: it drives the Go contract layer, the OpenAPI doc, the TS client, the Temporal layer, and the composition root.
- **The bare word `phase` is banned** as a new identifier. Use `ProjectPhase` (1/2/3), `LifecyclePhase` (the App-A five), `MethodTask` (the Fig A-1 twelve), or `TaskAttempt` (one execution). The two pre-existing blessed types `projectstate.Phase` and `ActivityMethodPhase` keep their names.
- **`RecordOrigin`'s zero value MUST be `synthesized`.** A dropped or missing provenance stamp must fail suspicious, never blessed.
- **The join key is `attemptId = "<activityId>:<task>:<n>"`**, `n` 1-based. It is the ledger primary key, the UI click target, and the episode `TargetRef`. Its format must be identical everywhere it is produced.
- **No LLM output may determine** an activity's existence, position, layer, edge, or lifecycle state.
- **Do not touch** `EstimationEngine.DerivePlan`, its transitive reduction, or `make derived-plan-check`.
- **Do not touch** `awaitPhaseDecision` in `constructactivity.go` (verdict persistence is a later wave). Task 10 is the single deliberate exception in that file and touches only `episodeRecordFor`.
- **Weights live only in `ProfileFor`.** Never in `project.json`, never per-task.
- Commit after every task. Use `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>` in commit messages.

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `server/internal/resourceaccess/projectstate/methodtask.go` *(new)* | The 12-task Fig A-1 vocabulary, phase→tasks mapping, gate identification, `AttemptID` formatting | 1 |
| `server/internal/resourceaccess/projectstate/methodtask_test.go` *(new)* | Unit tests for the vocabulary | 1 |
| `server/internal/resourceaccess/projectstate/provenance_origin.go` *(new)* | `RecordOrigin`, `AttemptProvenance`, contagion helper | 2 |
| `server/internal/resourceaccess/projectstate/provenance_origin_test.go` *(new)* | Zero-value and contagion tests | 2 |
| `server/internal/resourceaccess/projectstate/projectstateaccess.go` | `TaskAttempt` struct on `ActivityConstructionStatus`; `RecordTaskAttempt`; strict `ClassifyType`; `CoarseBuildStatus` all-phases rule; `LayerBandFor` | 2–5, 11 |
| `server/internal/resourceaccess/projectstate/access_test.go` | Tests for the above | 2–5, 11 |
| `.aiarch/state/project.json` → `.serviceContracts.systemDesignManager.$defs` | Wire `$defs` for `PhaseCompletion.Label`, `TaskAttempt`, `AttemptProvenance`, `EvidenceRef` | 6 |
| `server/internal/manager/systemdesign/systemdesignmanager.go` | `phasesToContract` restores `Label`; `constructionRowsToContract` emits attempts, unclassified, layer | 7, 11 |
| `server/cmd/gen-uiprofiles/main.go` | Stale source path; 5 testing variants; task vocabulary; `CanonicalPhase`→`LifecyclePhase` | 8 |
| `webApp/src/components/construction/lifecycleTemplates.gen.ts` | *(generated)* | 8 |
| `webApp/src/components/construction/lifecycleTemplates.ts` | Drop `activeIdxFor`; re-export `LifecyclePhase` | 8 |
| `webApp/src/contracts/wire.ts` | `mapConstructionRow` reads `Phases` and `Attempts` | 9 |
| `webApp/src/contracts/types.ts` | `TaskAttemptRow`, `currentLifecyclePhase` rename | 9 |
| `server/internal/manager/construction/constructactivity.go` | `episodeRecordFor` stamps the attempt key into `TargetRef` | 10 |
| `server/cmd/backfill-attempts/main.go` *(new)* | One-shot synthesis tool; output is a committed artifact | 12 |
| `server/internal/arch/` gate config | Ban bare `phase`/`Phase` identifiers | 13 |

---

### Task 1: The 12-task Figure A-1 vocabulary

**Files:**
- Create: `server/internal/resourceaccess/projectstate/methodtask.go`
- Test: `server/internal/resourceaccess/projectstate/methodtask_test.go`

**Interfaces:**
- Consumes: `ActivityMethodPhase` constants (`MethodPhaseRequirements`, `MethodPhaseTestPlan`, `MethodPhaseDetailedDesign`, `MethodPhaseConstruction`, `MethodPhaseIntegration`) and `Profile` / `ProfileFor` from `projectstateaccess.go`.
- Produces: `type MethodTask string`; the 12 constants; `TasksForPhase(ActivityMethodPhase) []MethodTask`; `GateTaskFor(ActivityMethodPhase) MethodTask`; `IsGateTask(MethodTask) bool`; `IsConditionalTask(MethodTask) bool`; `PhaseForTask(MethodTask) ActivityMethodPhase`; `TasksForProfile(Profile) []MethodTask`; `AttemptID(activityID string, t MethodTask, n int) string`.

- [ ] **Step 1: Write the failing test**

Create `server/internal/resourceaccess/projectstate/methodtask_test.go`:

```go
package projectstate

import "testing"

func TestTasksForPhase_MatchesFigureA2Grouping(t *testing.T) {
	cases := []struct {
		phase ActivityMethodPhase
		want  []MethodTask
	}{
		{MethodPhaseRequirements, []MethodTask{TaskSRS, TaskSRSReview}},
		{MethodPhaseTestPlan, []MethodTask{TaskSTP, TaskSTPReview}},
		{MethodPhaseDetailedDesign, []MethodTask{TaskSomeConstruction, TaskDetailedDesign, TaskDesignReview}},
		{MethodPhaseConstruction, []MethodTask{TaskConstruction, TaskTestClient, TaskCodeReview}},
		{MethodPhaseIntegration, []MethodTask{TaskIntegration, TaskTesting}},
	}
	for _, c := range cases {
		got := TasksForPhase(c.phase)
		if len(got) != len(c.want) {
			t.Fatalf("TasksForPhase(%v) = %v, want %v", c.phase, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("TasksForPhase(%v)[%d] = %q, want %q", c.phase, i, got[i], c.want[i])
			}
		}
	}
}

func TestTasksForPhase_TwelveTasksTotal(t *testing.T) {
	all := map[MethodTask]bool{}
	for _, p := range []ActivityMethodPhase{
		MethodPhaseRequirements, MethodPhaseTestPlan, MethodPhaseDetailedDesign,
		MethodPhaseConstruction, MethodPhaseIntegration,
	} {
		for _, task := range TasksForPhase(p) {
			all[task] = true
		}
	}
	if len(all) != 12 {
		t.Errorf("total distinct tasks = %d, want 12 (Figure A-1)", len(all))
	}
}

func TestGateTaskFor_IsTheBinaryExitCriterion(t *testing.T) {
	cases := map[ActivityMethodPhase]MethodTask{
		MethodPhaseRequirements:    TaskSRSReview,
		MethodPhaseTestPlan:        TaskSTPReview,
		MethodPhaseDetailedDesign:  TaskDesignReview,
		MethodPhaseConstruction:    TaskCodeReview,
		MethodPhaseIntegration:     TaskTesting,
	}
	for phase, want := range cases {
		if got := GateTaskFor(phase); got != want {
			t.Errorf("GateTaskFor(%v) = %q, want %q", phase, got, want)
		}
		if !IsGateTask(want) {
			t.Errorf("IsGateTask(%q) = false, want true", want)
		}
	}
}

func TestIsConditionalTask_OnlySomeConstructionAndTestClient(t *testing.T) {
	if !IsConditionalTask(TaskSomeConstruction) {
		t.Error("someConstruction must be conditional-emit")
	}
	if !IsConditionalTask(TaskTestClient) {
		t.Error("testClient must be conditional-emit")
	}
	if IsConditionalTask(TaskDetailedDesign) {
		t.Error("detailedDesign must be invariant, not conditional")
	}
}

func TestPhaseForTask_RoundTrips(t *testing.T) {
	for _, p := range []ActivityMethodPhase{
		MethodPhaseRequirements, MethodPhaseTestPlan, MethodPhaseDetailedDesign,
		MethodPhaseConstruction, MethodPhaseIntegration,
	} {
		for _, task := range TasksForPhase(p) {
			if got := PhaseForTask(task); got != p {
				t.Errorf("PhaseForTask(%q) = %v, want %v", task, got, p)
			}
		}
	}
}

func TestTasksForProfile_PerTypeCounts(t *testing.T) {
	cases := []struct {
		name string
		typ  ActivityType
		want int
	}{
		{"service", ActivityTypeService, 12},
		{"frontend", ActivityTypeFrontend, 12},
		{"deployment", ActivityTypeDeployment, 8},
		{"documentation", ActivityTypeDocumentation, 8},
		{"uiDesign", ActivityTypeUIDesign, 5},
		{"integration", ActivityTypeIntegration, 2},
	}
	for _, c := range cases {
		got := TasksForProfile(ProfileFor(c.typ, TestVariantPlan))
		if len(got) != c.want {
			t.Errorf("%s: TasksForProfile len = %d, want %d (got %v)", c.name, len(got), c.want, got)
		}
	}
}

func TestAttemptID_Format(t *testing.T) {
	got := AttemptID("C-billing-manager", TaskDesignReview, 2)
	want := "C-billing-manager:designReview:2"
	if got != want {
		t.Errorf("AttemptID = %q, want %q", got, want)
	}
}
```

Note the expected counts: `deployment` and `documentation` carry 3 lifecycle phases (DetailedDesign 3 tasks + Construction 3 + Integration 2 = 8), `uiDesign` carries 2 (Requirements 2 + DetailedDesign 3 = 5), `integration` carries 1 (Integration 2). Service and Frontend carry all five phases = 12.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestTasksForPhase -v
```

Expected: FAIL — `undefined: TasksForPhase`, `undefined: TaskSRS`, etc.

- [ ] **Step 3: Write the implementation**

Create `server/internal/resourceaccess/projectstate/methodtask.go`:

```go
package projectstate

import "fmt"

// MethodTask is one of the twelve internal tasks of Figure A-1 (Löwy, Righting
// Software, Appendix A) — the unit BELOW a lifecycle phase and ABOVE an attempt.
//
// Figure A-1 is not a linear chain. After SRS Review the graph forks: the Test Plan
// branch (STP → STP Review) runs in parallel with the Detailed Design → Construction
// branch, and the two rejoin at Testing. Construction and Test Client are built in
// tandem (a bidirectional edge in the figure).
//
// Naming follows the R3 ruling: the bare word "phase" is banned; this level is a
// TASK, the level above it is a LifecyclePhase, one execution of a task is an Attempt.
type MethodTask string

// The twelve Figure A-1 tasks.
const (
	TaskSRS              MethodTask = "srs"
	TaskSRSReview        MethodTask = "srsReview"
	TaskSTP              MethodTask = "stp"
	TaskSTPReview        MethodTask = "stpReview"
	TaskSomeConstruction MethodTask = "someConstruction"
	TaskDetailedDesign   MethodTask = "detailedDesign"
	TaskDesignReview     MethodTask = "designReview"
	TaskConstruction     MethodTask = "construction"
	TaskTestClient       MethodTask = "testClient"
	TaskCodeReview       MethodTask = "codeReview"
	TaskIntegration      MethodTask = "integration"
	TaskTesting          MethodTask = "testing"
)

// phaseTasks is the Figure A-2 grouping: which tasks make up each lifecycle phase.
// Order within a phase is execution order.
var phaseTasks = map[ActivityMethodPhase][]MethodTask{
	MethodPhaseRequirements:   {TaskSRS, TaskSRSReview},
	MethodPhaseTestPlan:       {TaskSTP, TaskSTPReview},
	MethodPhaseDetailedDesign: {TaskSomeConstruction, TaskDetailedDesign, TaskDesignReview},
	MethodPhaseConstruction:   {TaskConstruction, TaskTestClient, TaskCodeReview},
	MethodPhaseIntegration:    {TaskIntegration, TaskTesting},
}

// gateTasks is the binary exit criterion per phase (App A: "the Construction phase is
// complete once you have had the code review, not simply when the code is checked in").
var gateTasks = map[ActivityMethodPhase]MethodTask{
	MethodPhaseRequirements:   TaskSRSReview,
	MethodPhaseTestPlan:       TaskSTPReview,
	MethodPhaseDetailedDesign: TaskDesignReview,
	MethodPhaseConstruction:   TaskCodeReview,
	MethodPhaseIntegration:    TaskTesting,
}

// conditionalTasks are emitted ONLY when a real attempt record exists for them.
// someConstruction is Löwy's pre-design spike and our agentic detailed-design dispatch
// is a single episode; testClient is the tandem partner of Construction and often does
// not exist for a deployment or a doc. Rendering a row for work that never happened is
// the "view states something false" failure this stage exists to remove.
var conditionalTasks = map[MethodTask]bool{
	TaskSomeConstruction: true,
	TaskTestClient:       true,
}

// TasksForPhase returns the Figure A-1 tasks belonging to a lifecycle phase, in
// execution order. An unknown phase returns nil.
func TasksForPhase(p ActivityMethodPhase) []MethodTask {
	src := phaseTasks[p]
	if len(src) == 0 {
		return nil
	}
	out := make([]MethodTask, len(src))
	copy(out, src)
	return out
}

// GateTaskFor returns the task whose success IS the phase's binary exit criterion.
func GateTaskFor(p ActivityMethodPhase) MethodTask { return gateTasks[p] }

// IsGateTask reports whether a task is some phase's binary exit criterion.
func IsGateTask(t MethodTask) bool {
	for _, gate := range gateTasks {
		if gate == t {
			return true
		}
	}
	return false
}

// IsConditionalTask reports whether a task is emitted only when an attempt exists.
func IsConditionalTask(t MethodTask) bool { return conditionalTasks[t] }

// PhaseForTask returns the lifecycle phase a task belongs to (the empty phase when
// the task is unknown).
func PhaseForTask(t MethodTask) ActivityMethodPhase {
	for p, tasks := range phaseTasks {
		for _, candidate := range tasks {
			if candidate == t {
				return p
			}
		}
	}
	return ""
}

// TasksForProfile returns every task an activity with this profile can have, in phase
// order then execution order. This is the ROW SET of the list view: it is derived from
// the profile, never from storage, so the tasks that have not happened still render.
func TasksForProfile(pr Profile) []MethodTask {
	out := make([]MethodTask, 0, 12)
	for _, ph := range pr.Phases {
		out = append(out, TasksForPhase(ph.Phase)...)
	}
	return out
}

// AttemptID is THE join key of the construction model: the ledger primary key, the UI
// click target, the sub-graph node label, the unit provenance applies to, and the
// TargetRef stamped onto every EpisodeRecord. n is 1-based per (activity, task).
//
// Its format must be identical everywhere it is produced. Do not inline it.
func AttemptID(activityID string, t MethodTask, n int) string {
	return fmt.Sprintf("%s:%s:%d", activityID, t, n)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestTasks|TestGateTask|TestIsConditional|TestPhaseForTask|TestAttemptID' -v
```

Expected: PASS, all 7 tests.

- [ ] **Step 5: Commit**

```bash
git add server/internal/resourceaccess/projectstate/methodtask.go \
        server/internal/resourceaccess/projectstate/methodtask_test.go
git commit -m "feat(construction): the twelve Figure A-1 tasks and the attempt join key

Adds MethodTask — the level between a lifecycle phase and one execution.
All twelve tasks, no review folds: the AI-task/review-task alternation is
what lets the UI decide between opening an episode and opening the artifact
under review, and the retry rule has no 'preceding task' to name without it.

someConstruction and testClient are conditional-emit.
AttemptID is the one join key the later capture wave depends on.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: Provenance with a suspicious zero value

**Files:**
- Create: `server/internal/resourceaccess/projectstate/provenance_origin.go`
- Test: `server/internal/resourceaccess/projectstate/provenance_origin_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `type RecordOrigin string` with `OriginSynthesized` (zero value), `OriginBackfilled`, `OriginObserved`; `type AttemptProvenance struct{ Origin RecordOrigin; Generator string; GeneratedAt *time.Time; Basis string }`; `func WorstOrigin(...RecordOrigin) RecordOrigin`; `func (p AttemptProvenance) Validate() error`.

- [ ] **Step 1: Write the failing test**

Create `server/internal/resourceaccess/projectstate/provenance_origin_test.go`:

```go
package projectstate

import (
	"encoding/json"
	"testing"
)

// The whole design: a dropped or absent stamp must fail SUSPICIOUS, never blessed.
func TestRecordOrigin_ZeroValueIsSynthesized(t *testing.T) {
	var zero RecordOrigin
	if zero != OriginSynthesized {
		t.Fatalf("zero RecordOrigin = %q, want %q — a missing stamp must never read as observed", zero, OriginSynthesized)
	}
}

func TestAttemptProvenance_ZeroStructIsSynthesized(t *testing.T) {
	var p AttemptProvenance
	if p.Origin != OriginSynthesized {
		t.Errorf("zero AttemptProvenance.Origin = %q, want %q", p.Origin, OriginSynthesized)
	}
}

func TestAttemptProvenance_DecodingAbsentOriginIsSynthesized(t *testing.T) {
	var p AttemptProvenance
	if err := json.Unmarshal([]byte(`{"generator":"x"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Origin != OriginSynthesized {
		t.Errorf("absent origin decoded to %q, want %q", p.Origin, OriginSynthesized)
	}
}

func TestAttemptProvenance_ValidateRejectsUnknownOrigin(t *testing.T) {
	p := AttemptProvenance{Origin: RecordOrigin("observed-ish")}
	if err := p.Validate(); err == nil {
		t.Error("Validate() accepted an unknown origin, want error")
	}
}

func TestAttemptProvenance_ValidateRequiresBasisForBackfilled(t *testing.T) {
	p := AttemptProvenance{Origin: OriginBackfilled}
	if err := p.Validate(); err == nil {
		t.Error("Validate() accepted backfilled with no basis, want error")
	}
	p.Basis = "serviceContracts[artifactAccess]"
	if err := p.Validate(); err != nil {
		t.Errorf("Validate() rejected backfilled with a basis: %v", err)
	}
}

// Contagion: a value derived from any synthesized input is itself synthesized.
func TestWorstOrigin_Contagion(t *testing.T) {
	cases := []struct {
		name string
		in   []RecordOrigin
		want RecordOrigin
	}{
		{"all observed", []RecordOrigin{OriginObserved, OriginObserved}, OriginObserved},
		{"one backfilled", []RecordOrigin{OriginObserved, OriginBackfilled}, OriginBackfilled},
		{"one synthesized wins", []RecordOrigin{OriginObserved, OriginBackfilled, OriginSynthesized}, OriginSynthesized},
		{"empty is observed", nil, OriginObserved},
	}
	for _, c := range cases {
		if got := WorstOrigin(c.in...); got != c.want {
			t.Errorf("%s: WorstOrigin(%v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestRecordOrigin|TestAttemptProvenance|TestWorstOrigin' -v
```

Expected: FAIL — `undefined: RecordOrigin`.

- [ ] **Step 3: Write the implementation**

Create `server/internal/resourceaccess/projectstate/provenance_origin.go`:

```go
package projectstate

import (
	"fmt"
	"time"
)

// RecordOrigin says how a TaskAttempt came to exist. It is REQUIRED on every attempt
// and is never omitempty.
//
// The zero value is OriginSynthesized on purpose. A missing or dropped stamp must fail
// SUSPICIOUS, never blessed — an omitempty field whose zero value meant "observed" is
// exactly how a fabricated row would launder itself through a round-trip.
//
// Three values, not two: some rows genuinely can be reconstructed from real evidence
// (a frozen contract in .serviceContracts, a merged commit). Those are not lies and
// must not be tarred as fakes; they were also not observed.
type RecordOrigin string

// The three origins. OriginSynthesized MUST remain the empty string.
const (
	// OriginSynthesized — fabricated for the UI wave; no event backs it. ZERO VALUE.
	OriginSynthesized RecordOrigin = ""
	// OriginBackfilled — reconstructed from real evidence recorded elsewhere.
	OriginBackfilled RecordOrigin = "backfilled"
	// OriginObserved — written by the running system from a real event.
	OriginObserved RecordOrigin = "observed"
)

// AttemptProvenance carries the reason, not just the flag: what produced this record
// and what it was derived from.
//
// This is deliberately NOT the existing Provenance type in projectstateaccess.go — that
// one answers a different question (who approved a design commit). Do not overload it.
type AttemptProvenance struct {
	// Origin is required; the zero value is OriginSynthesized.
	Origin RecordOrigin `json:"origin"`
	// Generator names the producing tool and sha, e.g. "cmd/backfill-attempts@a1b2c3d".
	Generator string `json:"generator,omitempty"`
	// GeneratedAt is when the record was produced (not when the work happened).
	GeneratedAt *time.Time `json:"generatedAt,omitempty"`
	// Basis names what this was derived from, e.g. "serviceContracts[artifactAccess]".
	// Empty for pure fiction; REQUIRED for OriginBackfilled.
	Basis string `json:"basis,omitempty"`
}

// Validate enforces the closed enum and the backfilled-needs-a-basis rule.
func (p AttemptProvenance) Validate() error {
	switch p.Origin {
	case OriginSynthesized, OriginObserved:
	case OriginBackfilled:
		if p.Basis == "" {
			return fmt.Errorf("provenance: origin %q requires a non-empty basis", p.Origin)
		}
	default:
		return fmt.Errorf("provenance: unknown origin %q", p.Origin)
	}
	return nil
}

// originRank orders origins worst-first for the contagion rule.
func originRank(o RecordOrigin) int {
	switch o {
	case OriginSynthesized:
		return 0
	case OriginBackfilled:
		return 1
	case OriginObserved:
		return 2
	default:
		return 0 // unknown is as bad as synthesized
	}
}

// WorstOrigin implements the contagion rule: a value derived from any synthesized input
// is itself synthesized. PhaseCompletion.Completed, activity progress and project earned
// value all inherit the worst origin among their inputs.
//
// Without this, rows are honestly badged while the header launders a fabricated
// aggregate — the single worst lie available to this work.
//
// No inputs means nothing was derived from anything unknown: OriginObserved.
func WorstOrigin(origins ...RecordOrigin) RecordOrigin {
	worst := OriginObserved
	for _, o := range origins {
		if originRank(o) < originRank(worst) {
			worst = o
		}
	}
	return worst
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestRecordOrigin|TestAttemptProvenance|TestWorstOrigin' -v
```

Expected: PASS, all 6 tests.

- [ ] **Step 5: Commit**

```bash
git add server/internal/resourceaccess/projectstate/provenance_origin.go \
        server/internal/resourceaccess/projectstate/provenance_origin_test.go
git commit -m "feat(construction): provenance whose zero value is 'synthesized'

RecordOrigin is required on every TaskAttempt and its zero value is
OriginSynthesized, so a dropped stamp fails suspicious rather than blessed.

WorstOrigin implements the contagion rule: anything derived from a synthesized
input is itself synthesized, which is what stops an aggregate from laundering a
fabricated number behind honestly-badged rows.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: The `TaskAttempt` ledger on the store record

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` (add types near `PhaseCompletion`, ~line 6997; add `Attempts` field to `ActivityConstructionStatus`, ~line 7009)
- Test: `server/internal/resourceaccess/projectstate/access_test.go` (append)

**Interfaces:**
- Consumes: `MethodTask`, `AttemptID` (Task 1); `AttemptProvenance`, `RecordOrigin`, `WorstOrigin` (Task 2).
- Produces: `type TaskOutcome string` (`OutcomePending`, `OutcomePassed`, `OutcomeRejected`, `OutcomeFailed`, `OutcomeSkipped`); `type TaskActor string` (`ActorAgent`, `ActorHuman`, `ActorSystem`); `type EvidenceKind string` (`EvidenceNone`, `EvidenceEpisode`, `EvidenceArtifact`, `EvidenceContract`, `EvidenceGit`); `type EvidenceRef struct{ Kind EvidenceKind; Ref string }`; `type TaskAttempt struct{...}`; `ActivityConstructionStatus.Attempts []TaskAttempt`; `func LatestAttempt([]TaskAttempt, MethodTask) (TaskAttempt, bool)`; `func PhaseCompleteFromAttempts([]TaskAttempt, ActivityMethodPhase) bool`.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/resourceaccess/projectstate/access_test.go`:

```go
func TestLatestAttempt_ReturnsHighestAttemptNumber(t *testing.T) {
	attempts := []TaskAttempt{
		{AttemptID: AttemptID("C-x", TaskDetailedDesign, 1), Task: TaskDetailedDesign, Attempt: 1, Outcome: OutcomePassed},
		{AttemptID: AttemptID("C-x", TaskDesignReview, 1), Task: TaskDesignReview, Attempt: 1, Outcome: OutcomeRejected},
		{AttemptID: AttemptID("C-x", TaskDetailedDesign, 2), Task: TaskDetailedDesign, Attempt: 2, Outcome: OutcomePassed},
	}
	got, ok := LatestAttempt(attempts, TaskDetailedDesign)
	if !ok {
		t.Fatal("LatestAttempt(detailedDesign) not found")
	}
	if got.Attempt != 2 {
		t.Errorf("LatestAttempt(detailedDesign).Attempt = %d, want 2", got.Attempt)
	}
}

func TestLatestAttempt_MissingTaskReportsNotFound(t *testing.T) {
	if _, ok := LatestAttempt(nil, TaskCodeReview); ok {
		t.Error("LatestAttempt(nil) reported found, want not found")
	}
}

// App A's binary exit, verbatim: a phase is complete iff its GATE task's latest
// attempt passed. A passed non-gate task earns nothing.
func TestPhaseCompleteFromAttempts_RequiresTheGateTask(t *testing.T) {
	// Construction task passed but code review has not happened.
	attempts := []TaskAttempt{
		{AttemptID: AttemptID("C-x", TaskConstruction, 1), Task: TaskConstruction, Attempt: 1, Outcome: OutcomePassed},
	}
	if PhaseCompleteFromAttempts(attempts, MethodPhaseConstruction) {
		t.Error("construction phase reported complete without the code review gate")
	}
	attempts = append(attempts, TaskAttempt{
		AttemptID: AttemptID("C-x", TaskCodeReview, 1), Task: TaskCodeReview, Attempt: 1, Outcome: OutcomePassed,
	})
	if !PhaseCompleteFromAttempts(attempts, MethodPhaseConstruction) {
		t.Error("construction phase reported incomplete after the code review passed")
	}
}

func TestPhaseCompleteFromAttempts_RejectedGateIsNotComplete(t *testing.T) {
	attempts := []TaskAttempt{
		{AttemptID: AttemptID("C-x", TaskCodeReview, 1), Task: TaskCodeReview, Attempt: 1, Outcome: OutcomePassed},
		{AttemptID: AttemptID("C-x", TaskCodeReview, 2), Task: TaskCodeReview, Attempt: 2, Outcome: OutcomeRejected},
	}
	if PhaseCompleteFromAttempts(attempts, MethodPhaseConstruction) {
		t.Error("phase reported complete when the LATEST gate attempt was rejected")
	}
}

func TestTaskAttempt_ProvenanceIsNotOmitempty(t *testing.T) {
	a := TaskAttempt{AttemptID: "C-x:srs:1", Task: TaskSRS, Attempt: 1}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"provenance"`) {
		t.Errorf("marshalled TaskAttempt omitted provenance: %s", b)
	}
}
```

If `encoding/json` and `strings` are not already imported in `access_test.go`, add them.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestLatestAttempt|TestPhaseCompleteFromAttempts|TestTaskAttempt' -v
```

Expected: FAIL — `undefined: TaskAttempt`.

- [ ] **Step 3: Write the implementation**

In `server/internal/resourceaccess/projectstate/projectstateaccess.go`, immediately AFTER the `PhaseCompletion` struct (which ends around line 7004), insert:

```go
// TaskOutcome is the terminal state of one attempt at a Figure A-1 task.
type TaskOutcome string

// The five task outcomes. OutcomePending is the zero value: an attempt that has been
// started and has not yet resolved.
const (
	OutcomePending  TaskOutcome = ""
	OutcomePassed   TaskOutcome = "passed"
	OutcomeRejected TaskOutcome = "rejected"
	OutcomeFailed   TaskOutcome = "failed"
	OutcomeSkipped  TaskOutcome = "skipped"
)

// TaskActor is who performed an attempt.
type TaskActor string

// The three actors.
const (
	ActorAgent  TaskActor = "agent"
	ActorHuman  TaskActor = "human"
	ActorSystem TaskActor = "system"
)

// EvidenceKind discriminates what an attempt's evidence points at. It is also the UI's
// click dispatch: an episode opens the agentic episode detail, an artifact or contract
// opens the thing under review. No second lookup table.
type EvidenceKind string

// The evidence kinds.
const (
	EvidenceNone     EvidenceKind = ""
	EvidenceEpisode  EvidenceKind = "episode"
	EvidenceArtifact EvidenceKind = "artifact"
	EvidenceContract EvidenceKind = "contract"
	EvidenceGit      EvidenceKind = "git"
)

// EvidenceRef points an attempt at what it produced or reviewed.
type EvidenceRef struct {
	Kind EvidenceKind `json:"kind,omitempty"`
	Ref  string       `json:"ref,omitempty"`
}

// TaskAttempt is one execution of one Figure A-1 task. Attempts form an APPEND-ONLY
// ledger on ActivityConstructionStatus.
//
// Append-only, not a task-with-attempts-array, for three reasons: the tasks that did
// NOT happen must still render, and their row set comes from TasksForProfile rather
// than from storage (a stored skeleton would duplicate ProfileFor in the data and let
// the two drift — that has already happened once and been cleaned up); appending an
// identified record converges safely under Temporal retry where a nested array mutation
// is a read-modify-write; and ArtifactSlot.reviewThread is existing precedent for a
// round-numbered append-only ledger.
//
// Löwy's retry rule ("a failing review causes the developer to repeat the preceding
// internal task") renders directly from this: detailedDesign#1 passed →
// designReview#1 rejected → detailedDesign#2 passed → designReview#2 passed.
type TaskAttempt struct {
	// AttemptID is "<activityId>:<task>:<n>" — always produced by AttemptID().
	AttemptID string `json:"attemptId"`
	// Task is one of the twelve Figure A-1 tasks.
	Task MethodTask `json:"task"`
	// Phase is denormalized from PhaseForTask(Task) for query convenience.
	Phase ActivityMethodPhase `json:"phase"`
	// Attempt is 1-based, per (activity, task).
	Attempt int `json:"attempt"`
	// Actor is who performed this attempt.
	Actor TaskActor `json:"actor,omitempty"`
	// StartedAt is when the attempt began.
	StartedAt *time.Time `json:"startedAt,omitempty"`
	// EndedAt is when it resolved; nil while Outcome is OutcomePending.
	EndedAt *time.Time `json:"endedAt,omitempty"`
	// Outcome is the terminal state; the zero value is OutcomePending.
	Outcome TaskOutcome `json:"outcome,omitempty"`
	// Evidence points at what this attempt produced or reviewed.
	Evidence EvidenceRef `json:"evidence,omitempty"`
	// Provenance is REQUIRED and never omitempty — see AttemptProvenance.
	Provenance AttemptProvenance `json:"provenance"`
}

// LatestAttempt returns the highest-numbered attempt at a task.
func LatestAttempt(attempts []TaskAttempt, t MethodTask) (TaskAttempt, bool) {
	var best TaskAttempt
	found := false
	for _, a := range attempts {
		if a.Task != t {
			continue
		}
		if !found || a.Attempt > best.Attempt {
			best, found = a, true
		}
	}
	return best, found
}

// PhaseCompleteFromAttempts implements App A's binary exit criterion verbatim: a phase
// is complete iff its GATE task's LATEST attempt passed.
//
// This is why PhaseCompletion.Completed becomes derived rather than written, and why a
// passed non-gate task earns no fractional credit — "the Construction phase is complete
// once you have had the code review, not simply when the code is checked in."
func PhaseCompleteFromAttempts(attempts []TaskAttempt, p ActivityMethodPhase) bool {
	gate := GateTaskFor(p)
	if gate == "" {
		return false
	}
	latest, ok := LatestAttempt(attempts, gate)
	return ok && latest.Outcome == OutcomePassed
}

// AttemptsWorstOrigin is the contagion roll-up for one activity's ledger.
func AttemptsWorstOrigin(attempts []TaskAttempt) RecordOrigin {
	origins := make([]RecordOrigin, 0, len(attempts))
	for _, a := range attempts {
		origins = append(origins, a.Provenance.Origin)
	}
	return WorstOrigin(origins...)
}
```

Then add the field to `ActivityConstructionStatus`, immediately after the `Phases` field:

```go
	// Attempts is the APPEND-ONLY Figure A-1 task ledger. Phases above is derived
	// from it (PhaseCompleteFromAttempts); Attempts is the record of what happened.
	Attempts []TaskAttempt `json:"attempts,omitempty"`
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestLatestAttempt|TestPhaseCompleteFromAttempts|TestTaskAttempt' -v
cd server && GOWORK=off go build ./...
```

Expected: PASS, and a clean build.

- [ ] **Step 5: Commit**

```bash
git add server/internal/resourceaccess/projectstate/projectstateaccess.go \
        server/internal/resourceaccess/projectstate/access_test.go
git commit -m "feat(construction): append-only TaskAttempt ledger

TaskAttempt records one execution of one Figure A-1 task, appended to
ActivityConstructionStatus.Attempts. Provenance is required and never omitempty.

PhaseCompleteFromAttempts implements App A's binary exit verbatim — a phase is
complete iff its GATE task's latest attempt passed — which is what makes
PhaseCompletion.Completed derived rather than written, and what denies
fractional credit to a passed non-gate task.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: Kill the lenient classifier fallback

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go:7627-7639` (`ClassifyType`)
- Modify: `server/internal/manager/systemdesign/systemdesignmanager.go:3361` (the one call site)
- Test: `server/internal/resourceaccess/projectstate/access_test.go` (append)

**Interfaces:**
- Consumes: `ClassifyActivity` (unchanged).
- Produces: `func ClassifyType(id, workerClass string, coding, hasServiceContract bool) (ActivityType, bool)` — the second return is `ok`. Callers that get `ok == false` must render the row as Unclassified with **zero** lifecycle sub-rows.

This is the single most damaging defect: the id join between slot 9 (`C-agentic-job-access`) and `.activityConstruction` (`C-AA`) matches only 9 of 69 rows, so 60 rows get a zero `ActivityItem`, fall to the no-rule arm of `ClassifyActivity`, and are silently typed `Deployment`. The type selects the profile, which selects the task vocabulary, which IS the row set of the list view — so a wrong type means every sub-row under that activity is a fabricated lifecycle.

Patching the ids is explicitly rejected: it would restore a *plausible* lifecycle for rows whose classification is still a guess.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/resourceaccess/projectstate/access_test.go`:

```go
func TestClassifyType_UnclassifiableReportsNotOK(t *testing.T) {
	// An unknown workerClass with coding=false matches no rule in ClassifyActivity.
	_, ok := ClassifyType("C-AA", "", false, false)
	if ok {
		t.Error("ClassifyType on an unclassifiable row reported ok=true; it must refuse to guess")
	}
}

func TestClassifyType_NoLenientDeploymentFallback(t *testing.T) {
	typ, ok := ClassifyType("C-AA", "", false, false)
	if ok && typ == ActivityTypeDeployment {
		t.Error("ClassifyType still falls back to Deployment for an unclassifiable row")
	}
}

func TestClassifyType_ServiceContractStillWins(t *testing.T) {
	typ, ok := ClassifyType("C-AA", "", false, true)
	if !ok || typ != ActivityTypeService {
		t.Errorf("ClassifyType(hasServiceContract=true) = (%v, %v), want (Service, true)", typ, ok)
	}
}

func TestClassifyType_ClassifiableRowsStillResolve(t *testing.T) {
	cases := []struct {
		id, workerClass string
		coding          bool
		want            ActivityType
	}{
		{"C-billing-manager", "junior-developer", true, ActivityTypeService},
		{"U-SPA-billing-manager", "ui-designer", true, ActivityTypeFrontend},
		{"G-SPA", "ui-designer", false, ActivityTypeUIDesign},
		{"N-IT", "software-tester", false, ActivityTypeTesting},
		{"I-billing", "senior-developer", false, ActivityTypeIntegration},
	}
	for _, c := range cases {
		typ, ok := ClassifyType(c.id, c.workerClass, c.coding, false)
		if !ok {
			t.Errorf("%s: ClassifyType reported not-ok for a classifiable row", c.id)
			continue
		}
		if typ != c.want {
			t.Errorf("%s: ClassifyType = %v, want %v", c.id, typ, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestClassifyType -v
```

Expected: FAIL — `assignment mismatch: 2 variables but ClassifyType returns 1 value`.

- [ ] **Step 3: Write the implementation**

Replace `ClassifyType` in `projectstateaccess.go` (its doc comment and body) with:

```go
// ClassifyType is the VIEW-MODEL classification lens over ClassifyActivity. It adds the
// one signal a read-time view has and a dispatch does not: hasServiceContract, a
// backward-looking corpus observation that an activity DID build a component, which
// wins over every rule below it.
//
// It is STRICT. It used to be lenient — an unclassifiable activity fell back to
// Deployment so that the row would still render. That fallback silently mistyped 60 of
// 69 committed construction records, because the slot-9 activity ids
// ("C-agentic-job-access") and the .activityConstruction keys ("C-AA") intersect in
// only 9 places, so the ActivityItem lookup returned a zero value for the rest.
//
// The type selects the profile, which selects the task vocabulary, which IS the row set
// of the construction list view. A wrong type therefore fabricates an entire lifecycle.
// The second return value is ok: when false the caller MUST render the row as
// Unclassified with NO lifecycle sub-rows at all. An honest blank beats a plausible lie.
func ClassifyType(id, workerClass string, coding, hasServiceContract bool) (ActivityType, bool) {
	if hasServiceContract {
		return ActivityTypeService, true
	}
	typ, _, err := ClassifyActivity(id, workerClass, coding)
	if err != nil {
		return ActivityTypeService, false
	}
	return typ, true
}
```

Then update the one call site in `server/internal/manager/systemdesign/systemdesignmanager.go`, in `constructionRowsToContract`. Replace:

```go
		typ := projectstate.ClassifyType(r.ActivityID, meta.WorkerClass, meta.Coding, rowHasServiceContract(r))
		var variant TestingVariant
		if typ == projectstate.ActivityTypeTesting {
```

with:

```go
		typ, classified := projectstate.ClassifyType(r.ActivityID, meta.WorkerClass, meta.Coding, rowHasServiceContract(r))
		var variant TestingVariant
		if classified && typ == projectstate.ActivityTypeTesting {
```

and add `Classified: classified,` to the `ActivityConstructionStatus{...}` literal it builds. That wire field is declared in Task 6; until then the Go build will fail on the unknown field, so **temporarily hold the `Classified:` line** and add it in Task 7. For this task, only the two lines above change.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && GOWORK=off go build ./... && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestClassifyType -v
```

Expected: clean build, PASS on all 4 tests.

- [ ] **Step 5: Commit**

```bash
git add server/internal/resourceaccess/projectstate/projectstateaccess.go \
        server/internal/resourceaccess/projectstate/access_test.go \
        server/internal/manager/systemdesign/systemdesignmanager.go
git commit -m "fix(construction): ClassifyType refuses to guess

The lenient Deployment fallback silently mistyped 60 of 69 committed
construction records: slot-9 ids (C-agentic-job-access) and
.activityConstruction keys (C-AA) intersect in only 9 places, so the
ActivityItem lookup returned a zero value and every miss fell through
ClassifyActivity's no-rule arm into Deployment.

Type selects profile selects task vocabulary selects the row set of the list
view, so a wrong type fabricates a whole lifecycle. ClassifyType now returns
(type, ok); an unclassifiable row renders Unclassified with no sub-rows.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: Wire the dead read-time derivations and require all phases for Integrated

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go:7108-7126` (`CoarseBuildStatus`)
- Modify: `server/internal/manager/systemdesign/systemdesignmanager.go:3369-3370` (use `CoarsePhaseFor` / `CoarseBuildStatusFor`)
- Test: `server/internal/resourceaccess/projectstate/access_test.go`

**Interfaces:**
- Consumes: `PhaseCompletion`, `CoarsePhaseFor`, `CoarseBuildStatusFor` (all pre-existing).
- Produces: `CoarseBuildStatus` with an all-phases-complete rule. No signature change.

`G-SPA` reports Integrated at 85% because of three separate bugs. This task fixes two of them: the read path passes the STORED `Phase`/`BuildStatus` straight through even though `CoarsePhaseFor`/`CoarseBuildStatusFor` exist and are documented as the compute-at-read entry points (they are dead code today), and `CoarseBuildStatus` returns `Integrated` on Integration-done alone without checking earlier phases.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/resourceaccess/projectstate/access_test.go`:

```go
// The G-SPA bug: Integration done while Requirements never completed must NOT read
// as Integrated.
func TestCoarseBuildStatus_RequiresAllPhasesForIntegrated(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	for i := range phases {
		// Everything except Requirements.
		if phases[i].Phase != MethodPhaseRequirements {
			phases[i].Completed = true
		}
	}
	if got := CoarseBuildStatus(phases, MethodPhaseIntegration); got == BuildIntegrated {
		t.Error("CoarseBuildStatus reported Integrated with Requirements incomplete")
	}
}

func TestCoarseBuildStatus_AllPhasesCompleteIsIntegrated(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	for i := range phases {
		phases[i].Completed = true
	}
	if got := CoarseBuildStatus(phases, MethodPhaseIntegration); got != BuildIntegrated {
		t.Errorf("CoarseBuildStatus(all done) = %v, want Integrated", got)
	}
}

// A uiDesign profile has no Integration phase at all; completing its two phases must
// still read as Integrated rather than being stuck in construction forever.
func TestCoarseBuildStatus_ProfileWithoutIntegrationCanIntegrate(t *testing.T) {
	phases := phaseSetFor(ActivityTypeUIDesign, 0)
	for i := range phases {
		phases[i].Completed = true
	}
	if got := CoarseBuildStatus(phases, MethodPhaseDetailedDesign); got != BuildIntegrated {
		t.Errorf("uiDesign all-phases-done = %v, want Integrated", got)
	}
}

func TestCoarseBuildStatusFor_StoredFailureIsStillSticky(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	for i := range phases {
		phases[i].Completed = true
	}
	if got := CoarseBuildStatusFor(BuildFailed, phases, MethodPhaseIntegration); got != BuildFailed {
		t.Errorf("CoarseBuildStatusFor(stored=Failed) = %v, want Failed (sticky)", got)
	}
}
```

The existing `TestCoarseBuildStatus_Integrated` will now fail, because it marks only Construction and Integration done. **Update it** to mark all phases complete and rename it to `TestCoarseBuildStatus_IntegratedWhenAllPhasesDone`, since its old assertion encodes the bug being fixed.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestCoarseBuildStatus -v
```

Expected: FAIL on `TestCoarseBuildStatus_RequiresAllPhasesForIntegrated` — got Integrated, want anything else.

- [ ] **Step 3: Write the implementation**

Replace `CoarseBuildStatus` in `projectstateaccess.go`:

```go
// CoarseBuildStatus derives the ActivityBuildStatus from the phase set (compute-at-read).
//
// Rules: ALL profile phases complete → BuildIntegrated; the Construction phase complete
// but not all → BuildInReview; otherwise → BuildInConstruction.
//
// The all-phases rule is deliberate. The old rule returned Integrated on Integration-done
// alone, which is how G-SPA came to report Integrated at 85% with Requirements never
// completed. Requiring every phase in the activity's own profile makes that state
// impossible rather than merely unlikely — and it works for profiles that carry no
// Integration phase at all (uiDesign, documentation), which the old rule left permanently
// stuck in construction.
//
// The second parameter is retained for signature compatibility and is unused: coarse
// status is derived solely from phase completion.
func CoarseBuildStatus(phases []PhaseCompletion, _ ActivityMethodPhase) ActivityBuildStatus {
	if len(phases) == 0 {
		return BuildInConstruction
	}
	allDone := true
	constructionDone := false
	for _, p := range phases {
		if !p.Completed {
			allDone = false
		}
		if p.Phase == MethodPhaseConstruction && p.Completed {
			constructionDone = true
		}
	}
	if allDone {
		return BuildIntegrated
	}
	if constructionDone {
		return BuildInReview
	}
	return BuildInConstruction
}
```

Then in `server/internal/manager/systemdesign/systemdesignmanager.go`, inside `constructionRowsToContract`, replace the two straight-through assignments:

```go
			Phase:         ActivityConstructionPhase(int(r.Phase)),
```
```go
			BuildStatus:   ActivityBuildStatus(int(r.BuildStatus)),
```

with the compute-at-read entry points that already exist and are currently dead:

```go
			Phase:         ActivityConstructionPhase(int(projectstate.CoarsePhaseFor(r.Phase, r.Phases))),
```
```go
			BuildStatus:   ActivityBuildStatus(int(projectstate.CoarseBuildStatusFor(r.BuildStatus, r.Phases, r.CurrentPhase))),
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && GOWORK=off go build ./... && GOWORK=off go test ./internal/resourceaccess/projectstate/ ./internal/manager/systemdesign/ -v 2>&1 | tail -30
```

Expected: clean build; PASS on the CoarseBuildStatus tests. If a systemdesign test asserted the old stored-passthrough behaviour, update it — its assertion encodes the bug.

- [ ] **Step 5: Commit**

```bash
git add server/internal/resourceaccess/projectstate/projectstateaccess.go \
        server/internal/resourceaccess/projectstate/access_test.go \
        server/internal/manager/systemdesign/systemdesignmanager.go
git commit -m "fix(construction): derive coarse status at read; require all phases for Integrated

Two of the three G-SPA 'Integrated at 85%' bugs.

CoarsePhaseFor/CoarseBuildStatusFor existed, were documented as the
compute-at-read entry points, and were dead code — the read path passed stored
values straight through, so every stored/derived contradiction was silent.

CoarseBuildStatus returned Integrated on Integration-done alone. It now requires
every phase in the activity's own profile, which also unsticks uiDesign and
documentation profiles that carry no Integration phase.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: Declare the wire contract and regenerate

**Files:**
- Modify: `.aiarch/state/project.json` → `.serviceContracts.systemDesignManager.$defs`
- Regenerate: `server/internal/manager/systemdesign/contract.gen.go`, `server/api/openapi.yaml`, `server/internal/client/**`, `webApp/src/contracts/schema.ts`, `webApp/src/contracts/enums.gen.ts`, `webApp/src/api/ops.gen.ts`

**Interfaces:**
- Consumes: the Go store types from Tasks 1–3 (shape only — the wire types are declared independently).
- Produces: wire `TaskAttempt`, `AttemptProvenance`, `EvidenceRef`; `PhaseCompletion.Label`; `ActivityConstructionStatus.Attempts`, `.Classified`, `.WorstOrigin`, `.Layer`, `.LayerBand`.

This is the ONE task in this stage that edits `project.json`, and it edits only the contract `$defs` — never a state slot.

- [ ] **Step 1: Read the existing contract shape**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator && python3 -c "
import json
d=json.load(open('.aiarch/state/project.json'))
defs=d['serviceContracts']['systemDesignManager']['\$defs']
print(json.dumps({k:defs[k] for k in ['PhaseCompletion','ActivityConstructionStatus'] if k in defs}, indent=2))
"
```

Note the existing conventions: property names are PascalCase where the Go field is PascalCase, `required` lists presence-only fields, `additionalProperties` is `false`, and `x-go-type`/`x-go-import` carry non-scalar Go types.

- [ ] **Step 2: Add the new `$defs` and fields**

Edit `.aiarch/state/project.json`. Under `.serviceContracts.systemDesignManager.$defs`, add three new definitions:

```json
"AttemptProvenance": {
  "type": "object",
  "properties": {
    "origin": { "type": "string" },
    "generator": { "type": "string" },
    "generatedAt": {
      "type": ["null", "string"],
      "format": "date-time",
      "x-go-import": "time",
      "x-go-type": "time.Time"
    },
    "basis": { "type": "string" }
  },
  "required": ["origin"],
  "additionalProperties": false
},
"EvidenceRef": {
  "type": "object",
  "properties": {
    "kind": { "type": "string" },
    "ref": { "type": "string" }
  },
  "required": ["kind", "ref"],
  "additionalProperties": false
},
"TaskAttempt": {
  "type": "object",
  "properties": {
    "attemptId": { "type": "string" },
    "task": { "type": "string" },
    "phase": { "$ref": "#/$defs/ActivityMethodPhase" },
    "attempt": { "type": "integer" },
    "actor": { "type": "string" },
    "startedAt": {
      "type": ["null", "string"],
      "format": "date-time",
      "x-go-import": "time",
      "x-go-type": "time.Time"
    },
    "endedAt": {
      "type": ["null", "string"],
      "format": "date-time",
      "x-go-import": "time",
      "x-go-type": "time.Time"
    },
    "outcome": { "type": "string" },
    "evidence": { "$ref": "#/$defs/EvidenceRef" },
    "provenance": { "$ref": "#/$defs/AttemptProvenance" }
  },
  "required": ["attemptId", "task", "phase", "attempt", "outcome", "evidence", "provenance"],
  "additionalProperties": false
}
```

Add `"Label": { "type": "string" }` to `PhaseCompletion.properties`, and add `"Label"` to its `required` list. Without this the wire has nowhere to carry the per-type phase label, which is why a Frontend activity currently renders the Service labels.

Add to `ActivityConstructionStatus.properties`:

```json
"attempts": { "type": "array", "items": { "$ref": "#/$defs/TaskAttempt" } },
"classified": { "type": "boolean" },
"worstOrigin": { "type": "string" },
"layer": { "type": "string" },
"layerBand": { "type": "string" }
```

and add `"classified"`, `"worstOrigin"`, `"layer"`, `"layerBand"` to its `required` list. **Do not add `attempts` to `required`** — an activity with no ledger is legitimate. `classified` and `worstOrigin` ARE required: a dropped `classified` must not read as `true`, and a dropped `worstOrigin` must not read as observed.

Verify the JSON is still valid:

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator && python3 -c "import json; json.load(open('.aiarch/state/project.json')); print('valid')"
```

- [ ] **Step 3: Regenerate every downstream artifact**

```bash
cd server && GOWORK=off make gen-models && GOWORK=off make gen-client
cd ../webApp && npm run gen:api && npm run gen:ops
```

- [ ] **Step 4: Verify the generated surface and the drift gates**

```bash
cd server && GOWORK=off make gen-models-check && GOWORK=off make gen-client-check
grep -n "type TaskAttempt struct" -A 14 internal/manager/systemdesign/contract.gen.go
grep -n "Label" internal/manager/systemdesign/contract.gen.go | head -5
cd ../webApp && npm run typecheck
```

Expected: both `-check` targets exit 0 (they re-run the generator and diff); `TaskAttempt` present in `contract.gen.go` with all 10 fields; `PhaseCompletion` carries `Label`; `tsc` clean. The Go build will still fail in `systemdesignmanager.go` if you added the `Classified:` line early — that is wired in Task 7.

- [ ] **Step 5: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add .aiarch/state/project.json \
        server/internal/manager/systemdesign/contract.gen.go \
        server/api/openapi.yaml server/internal/client \
        webApp/src/contracts/schema.ts webApp/src/contracts/enums.gen.ts webApp/src/api/ops.gen.ts
git commit -m "feat(contracts): TaskAttempt, AttemptProvenance, EvidenceRef on the wire

Declares the attempt ledger in .serviceContracts.systemDesignManager and
regenerates Go, OpenAPI and TS. Also adds PhaseCompletion.Label, without which
a Frontend activity renders the Service phase labels.

classified and worstOrigin are REQUIRED: a dropped classified flag must not
read as true, and a dropped origin must not read as observed.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 7: Emit attempts, labels and classification through the manager

**Files:**
- Modify: `server/internal/manager/systemdesign/systemdesignmanager.go` (`phasesToContract` ~3407, `constructionRowsToContract` ~3350)
- Test: `server/internal/manager/systemdesign/manager_test.go` (append)

**Interfaces:**
- Consumes: wire `TaskAttempt`/`AttemptProvenance`/`EvidenceRef` (Task 6); `ClassifyType` 2-value form (Task 4); `AttemptsWorstOrigin` (Task 3).
- Produces: `func attemptsToContract([]projectstate.TaskAttempt) []TaskAttempt`; `constructionRowsToContract` populating `Attempts`, `Classified`, `WorstOrigin`, and `Phases` with `Label`.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/manager/systemdesign/manager_test.go`:

```go
func TestPhasesToContract_CarriesLabel(t *testing.T) {
	in := []projectstate.PhaseCompletion{
		{Phase: projectstate.MethodPhaseRequirements, Weight: 15, Label: "UX Requirements"},
	}
	got := phasesToContract(in)
	if len(got) != 1 {
		t.Fatalf("phasesToContract len = %d, want 1", len(got))
	}
	if got[0].Label != "UX Requirements" {
		t.Errorf("Label = %q, want %q — a Frontend activity must not render Service labels", got[0].Label, "UX Requirements")
	}
}

func TestAttemptsToContract_PreservesTheJoinKeyAndProvenance(t *testing.T) {
	in := []projectstate.TaskAttempt{{
		AttemptID:  projectstate.AttemptID("C-x", projectstate.TaskDesignReview, 2),
		Task:       projectstate.TaskDesignReview,
		Phase:      projectstate.MethodPhaseDetailedDesign,
		Attempt:    2,
		Outcome:    projectstate.OutcomeRejected,
		Evidence:   projectstate.EvidenceRef{Kind: projectstate.EvidenceContract, Ref: "billingStateAccess"},
		Provenance: projectstate.AttemptProvenance{Origin: projectstate.OriginObserved},
	}}
	got := attemptsToContract(in)
	if len(got) != 1 {
		t.Fatalf("attemptsToContract len = %d, want 1", len(got))
	}
	if got[0].AttemptId != "C-x:designReview:2" {
		t.Errorf("AttemptId = %q, want %q", got[0].AttemptId, "C-x:designReview:2")
	}
	if got[0].Provenance.Origin != string(projectstate.OriginObserved) {
		t.Errorf("Provenance.Origin = %q, want %q", got[0].Provenance.Origin, projectstate.OriginObserved)
	}
	if got[0].Evidence.Kind != string(projectstate.EvidenceContract) {
		t.Errorf("Evidence.Kind = %q, want contract", got[0].Evidence.Kind)
	}
}

func TestAttemptsToContract_EmptyIsNil(t *testing.T) {
	if got := attemptsToContract(nil); got != nil {
		t.Errorf("attemptsToContract(nil) = %v, want nil", got)
	}
}
```

Adjust the generated field casing (`AttemptId` vs `AttemptID`) to whatever `contract.gen.go` actually emitted in Task 6 — read the file before writing the test.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && GOWORK=off go test ./internal/manager/systemdesign/ -run 'TestPhasesToContract_CarriesLabel|TestAttemptsToContract' -v
```

Expected: FAIL — `undefined: attemptsToContract`, and `Label` mismatch.

- [ ] **Step 3: Write the implementation**

In `systemdesignmanager.go`, add `Label: ph.Label,` to the struct literal inside `phasesToContract`.

Add a new mapper beside it:

```go
// attemptsToContract maps the append-only Figure A-1 task ledger onto the wire.
// Provenance is copied verbatim and never defaulted — the zero origin means
// "synthesized" and must survive the boundary as such.
func attemptsToContract(attempts []projectstate.TaskAttempt) []TaskAttempt {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]TaskAttempt, 0, len(attempts))
	for _, a := range attempts {
		out = append(out, TaskAttempt{
			AttemptId: a.AttemptID,
			Task:      string(a.Task),
			Phase:     ActivityMethodPhase(string(a.Phase)),
			Attempt:   int64(a.Attempt),
			Actor:     string(a.Actor),
			StartedAt: a.StartedAt,
			EndedAt:   a.EndedAt,
			Outcome:   string(a.Outcome),
			Evidence:  EvidenceRef{Kind: string(a.Evidence.Kind), Ref: a.Evidence.Ref},
			Provenance: AttemptProvenance{
				Origin:      string(a.Provenance.Origin),
				Generator:   a.Provenance.Generator,
				GeneratedAt: a.Provenance.GeneratedAt,
				Basis:       a.Provenance.Basis,
			},
		})
	}
	return out
}
```

In `constructionRowsToContract`, add to the `ActivityConstructionStatus{...}` literal:

```go
			Attempts:    attemptsToContract(r.Attempts),
			Classified:  classified,
			WorstOrigin: string(projectstate.AttemptsWorstOrigin(r.Attempts)),
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && GOWORK=off go build ./... && GOWORK=off go test ./internal/manager/systemdesign/ -v 2>&1 | tail -20
```

Expected: clean build, all systemdesign tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/manager/systemdesign/systemdesignmanager.go \
        server/internal/manager/systemdesign/manager_test.go
git commit -m "feat(construction): emit attempts, phase labels and classification on the wire

phasesToContract silently dropped Label, so every non-Service activity rendered
the Service phase names. attemptsToContract carries the ledger with provenance
copied verbatim — the zero origin means synthesized and must survive the
boundary as such.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 8: Regenerate the UI profile table — 5 testing variants, the task vocabulary, honest naming

**Files:**
- Modify: `server/cmd/gen-uiprofiles/main.go`
- Regenerate: `webApp/src/components/construction/lifecycleTemplates.gen.ts`
- Modify: `webApp/src/components/construction/lifecycleTemplates.ts`

**Interfaces:**
- Consumes: `ProfileFor`, `TasksForPhase`, `GateTaskFor`, `IsConditionalTask`, `CommandFor` (Tasks 1 and pre-existing).
- Produces: generated `LifecyclePhase` type (renamed from `CanonicalPhase`), `GeneratedPhase` gaining `tasks: readonly GeneratedTask[]`, per-testing-variant profile consts, and `GENERATED_TESTING_VARIANTS`.

Three defects in one touch: the generator's header cites `internal/resourceaccess/projectstate/activityprofile.go`, **which does not exist** (`ProfileFor` moved to `projectstateaccess.go:7322`); it emits only `TestVariantPlan` though the server carries five variants with materially different weights, and testing is 7 of 40 activities; and `CanonicalPhase` violates the R3 naming ruling.

- [ ] **Step 1: Write the failing check**

There is no Go test for this generator; its gate is `make gen-uiprofiles-check`. Write the assertion as a shell check to run after Step 3:

```bash
# Expected AFTER the change (all must print a match):
grep -c "export type LifecyclePhase" webApp/src/components/construction/lifecycleTemplates.gen.ts
grep -c "TESTING_HARNESS_PHASES\|TESTING_PERF_PHASES\|TESTING_SYSTEM_TEST_PHASES\|TESTING_QA_PROCESS_PHASES" webApp/src/components/construction/lifecycleTemplates.gen.ts
grep -c "tasks:" webApp/src/components/construction/lifecycleTemplates.gen.ts
grep -c "activityprofile.go" webApp/src/components/construction/lifecycleTemplates.gen.ts   # must be 0
```

- [ ] **Step 2: Confirm the current state fails those checks**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
grep -c "CanonicalPhase" webApp/src/components/construction/lifecycleTemplates.gen.ts   # currently >0
grep -c "activityprofile.go" webApp/src/components/construction/lifecycleTemplates.gen.ts  # currently 1
grep -c "tasks:" webApp/src/components/construction/lifecycleTemplates.gen.ts            # currently 0
```

- [ ] **Step 3: Write the implementation**

In `server/cmd/gen-uiprofiles/main.go`:

(a) Fix the stale source path in **both** the package doc (line ~4) and `writeHeader` (line ~85): replace `internal/resourceaccess/projectstate/activityprofile.go` with `internal/resourceaccess/projectstate/projectstateaccess.go`. Also delete the package-doc paragraph claiming testing emits one representative profile — it is being fixed here.

(b) Extend `kinds` with the four remaining testing variants:

```go
var kinds = []kindSpec{
	{"service", "SERVICE_PHASES", projectstate.ActivityTypeService, projectstate.TestVariantPlan},
	{"frontend", "FRONTEND_PHASES", projectstate.ActivityTypeFrontend, projectstate.TestVariantPlan},
	{"testing", "TESTING_PHASES", projectstate.ActivityTypeTesting, projectstate.TestVariantPlan},
	{"deployment", "DEPLOYMENT_PHASES", projectstate.ActivityTypeDeployment, projectstate.TestVariantPlan},
	{"documentation", "DOCUMENTATION_PHASES", projectstate.ActivityTypeDocumentation, projectstate.TestVariantPlan},
	{"uiDesign", "UI_DESIGN_PHASES", projectstate.ActivityTypeUIDesign, projectstate.TestVariantPlan},
	{"integration", "INTEGRATION_PHASES", projectstate.ActivityTypeIntegration, projectstate.TestVariantPlan},
}

// testingVariants emits the FIVE testing profiles the server actually carries. The
// generator used to emit only TestVariantPlan, so an N-IT activity rendered the N-STP
// shape — wrong for four of the five, and testing is 7 of 40 committed activities.
var testingVariants = []struct {
	tsVariant string
	constName string
	variant   projectstate.TestingVariant
}{
	{"plan", "TESTING_PLAN_PHASES", projectstate.TestVariantPlan},
	{"harness", "TESTING_HARNESS_PHASES", projectstate.TestVariantHarness},
	{"perf", "TESTING_PERF_PHASES", projectstate.TestVariantPerf},
	{"systemTest", "TESTING_SYSTEM_TEST_PHASES", projectstate.TestVariantSystemTest},
	{"qaProcess", "TESTING_QA_PROCESS_PHASES", projectstate.TestVariantQAProcess},
}
```

(c) Emit the Figure A-1 task vocabulary inside each phase. In `writePhasesConst`, replace the `fields` construction with one that appends a `tasks` array:

```go
func writePhasesConst(b *strings.Builder, constName string, t projectstate.ActivityType, v projectstate.TestingVariant) {
	profile := projectstate.ProfileFor(t, v)

	fmt.Fprintf(b, "export const %s: readonly GeneratedPhase[] = [\n", constName)
	for _, p := range profile.Phases {
		id := projectstate.CommandFor(t, v, p.Phase)
		b.WriteString("  {\n")
		fmt.Fprintf(b, "    id: %s,\n", tsString(id))
		fmt.Fprintf(b, "    phase: %s,\n", tsString(string(p.Phase)))
		fmt.Fprintf(b, "    name: %s,\n", tsString(p.Label))
		fmt.Fprintf(b, "    weight: %d,\n", p.Weight)
		b.WriteString("    tasks: [\n")
		for _, task := range projectstate.TasksForPhase(p.Phase) {
			fmt.Fprintf(b, "      { task: %s, gate: %t, conditional: %t },\n",
				tsString(string(task)),
				projectstate.GateTaskFor(p.Phase) == task,
				projectstate.IsConditionalTask(task))
		}
		b.WriteString("    ],\n")
		b.WriteString("  },\n")
	}
	b.WriteString("];\n\n")
}
```

Update the two call sites (`writePhasesConst(&b, k.constName, k.activityType, k.variant)` for `kinds`, and a second loop over `testingVariants` passing `projectstate.ActivityTypeTesting`).

(d) In `writeHeader`, rename the type and add the task interface:

```go
/** Canonical Method lifecycle phase (Righting Software Appendix A / Table A-1). */
export type LifecyclePhase =
  | 'requirements'
  | 'detailed_design'
  | 'test_plan'
  | 'construction'
  | 'integration';

/** One Figure A-1 task within a lifecycle phase. */
export interface GeneratedTask {
  task: string;
  /** True when this task's success IS the phase's binary exit criterion (App A). */
  gate: boolean;
  /** True when the task is emitted only if a real attempt record exists. */
  conditional: boolean;
}

/** One canonical-phase entry in a kind's profile — id/phase/name/weight/tasks. */
export interface GeneratedPhase {
  id: string;
  phase: LifecyclePhase;
  name: string;
  /** % contribution (App A Table A-1); weights sum to 100 per kind. */
  weight: number;
  tasks: readonly GeneratedTask[];
}
```

(e) Add a `GENERATED_TESTING_VARIANTS` record after `GENERATED_TEMPLATES`:

```go
func writeTestingVariantsRecord(b *strings.Builder) {
	b.WriteString("export const GENERATED_TESTING_VARIANTS: Record<string, readonly GeneratedPhase[]> = {\n")
	for _, v := range testingVariants {
		fmt.Fprintf(b, "  %s: %s,\n", v.tsVariant, v.constName)
	}
	b.WriteString("};\n")
}
```

Then in `webApp/src/components/construction/lifecycleTemplates.ts`: rename every `CanonicalPhase` reference to `LifecyclePhase`, delete `activeIdxFor` and `STATUS_TARGET_PHASE` entirely (the spec's explicit ruling — anything derived that way is reconstructed or unknown, never presented unmarked), and update `phaseStateFor` to take real `PhaseCompletion` data. If `ActivityLifecyclePanel.tsx` still calls `activeIdxFor`, change it to read the row's real `phases` array and mark nothing active when the array is absent.

- [ ] **Step 4: Regenerate and verify**

```bash
cd server && GOWORK=off make gen-uiprofiles && GOWORK=off make gen-uiprofiles-check
cd /Users/davidmarne/mixofrealitystudio/archistrator
grep -c "export type LifecyclePhase" webApp/src/components/construction/lifecycleTemplates.gen.ts   # 1
grep -c "activityprofile.go" webApp/src/components/construction/lifecycleTemplates.gen.ts           # 0
grep -c "TESTING_HARNESS_PHASES" webApp/src/components/construction/lifecycleTemplates.gen.ts       # >=1
grep -c "activeIdxFor" webApp/src/components/construction/lifecycleTemplates.ts                     # 0
cd webApp && npx prettier --check src/components/construction/lifecycleTemplates.gen.ts && npm run typecheck
```

Expected: `gen-uiprofiles-check` exits 0; all four greps match; prettier and tsc clean.

- [ ] **Step 5: Commit**

```bash
git add server/cmd/gen-uiprofiles/main.go \
        webApp/src/components/construction/lifecycleTemplates.gen.ts \
        webApp/src/components/construction/lifecycleTemplates.ts \
        webApp/src/components/construction/ActivityLifecyclePanel.tsx
git commit -m "feat(construction): generate all five testing profiles and the task vocabulary

Three defects in one touch. The generator cited activityprofile.go, which does
not exist. It emitted only TestVariantPlan, so an N-IT activity rendered the
N-STP shape — wrong for four of five, and testing is 7 of 40 activities.
CanonicalPhase violated the naming ruling and is now LifecyclePhase.

Also deletes activeIdxFor, which reverse-engineered an 'active phase' from the
coarse BuildStatus and presented the guess unmarked. That inference is exactly
what this work exists to stop.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 9: Read attempts and phases in the SPA wire layer

**Files:**
- Modify: `webApp/src/contracts/wire.ts` (`mapConstructionRow` ~line 360)
- Modify: `webApp/src/contracts/types.ts` (`ConstructionRow` ~line 645)
- Test: `webApp/src/contracts/constructionAdapters.test.ts` (append)

**Interfaces:**
- Consumes: generated `schema.ts` types from Task 6.
- Produces: `type TaskAttemptRow`, `type PhaseRow`; `ConstructionRow` gaining `attempts: TaskAttemptRow[]`, `phases: PhaseRow[]`, `classified: boolean`, `worstOrigin: RecordOriginRow`, and `phase` renamed to `currentLifecyclePhase`.

`mapConstructionRow` never reads `w.Phases` even though the server emits it and `schema.ts` carries it — so there is currently nothing for the tree to expand.

- [ ] **Step 1: Write the failing test**

Append to `webApp/src/contracts/constructionAdapters.test.ts`:

```ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mapConstructionRow } from './wire.ts';

void test('mapConstructionRow reads the Phases array the server already emits', () => {
  const row = mapConstructionRow({
    ActivityID: 'C-x',
    Phases: [
      { Phase: 'requirements', Weight: 15, Label: 'UX Requirements', Completed: true, ArtifactRef: '' },
    ],
    Attempts: [],
    Classified: true,
    WorstOrigin: 'observed',
  } as never);
  assert.equal(row.phases.length, 1);
  assert.equal(row.phases[0]?.label, 'UX Requirements');
  assert.equal(row.phases[0]?.completed, true);
});

void test('mapConstructionRow reads the attempt ledger with its join key', () => {
  const row = mapConstructionRow({
    ActivityID: 'C-x',
    Phases: [],
    Attempts: [
      {
        attemptId: 'C-x:designReview:2',
        task: 'designReview',
        phase: 'detailed_design',
        attempt: 2,
        outcome: 'rejected',
        evidence: { kind: 'contract', ref: 'billingStateAccess' },
        provenance: { origin: 'observed' },
      },
    ],
    Classified: true,
    WorstOrigin: 'observed',
  } as never);
  assert.equal(row.attempts.length, 1);
  assert.equal(row.attempts[0]?.attemptId, 'C-x:designReview:2');
  assert.equal(row.attempts[0]?.evidence.kind, 'contract');
});

// The zero value must survive the boundary as "synthesized", never as observed.
void test('mapConstructionRow treats an absent provenance origin as synthesized', () => {
  const row = mapConstructionRow({
    ActivityID: 'C-x',
    Phases: [],
    Attempts: [
      { attemptId: 'C-x:srs:1', task: 'srs', phase: 'requirements', attempt: 1, outcome: '', evidence: {}, provenance: {} },
    ],
    Classified: true,
    WorstOrigin: '',
  } as never);
  assert.equal(row.attempts[0]?.provenance.origin, 'synthesized');
  assert.equal(row.worstOrigin, 'synthesized');
});

void test('mapConstructionRow treats an absent Classified flag as unclassified', () => {
  const row = mapConstructionRow({ ActivityID: 'C-x', Phases: [], Attempts: [] } as never);
  assert.equal(row.classified, false);
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd webApp && node --test src/contracts/constructionAdapters.test.ts
```

Expected: FAIL — `row.phases` is undefined.

- [ ] **Step 3: Write the implementation**

In `webApp/src/contracts/types.ts`, add beside `ConstructionRow`:

```ts
/** How a task-attempt record came to exist. '' on the wire means synthesized. */
export type RecordOriginRow = 'synthesized' | 'backfilled' | 'observed';

export interface AttemptProvenanceRow {
  origin: RecordOriginRow;
  generator?: string;
  generatedAt?: string;
  basis?: string;
}

export interface EvidenceRefRow {
  kind: '' | 'episode' | 'artifact' | 'contract' | 'git';
  ref: string;
}

/** One execution of one Figure A-1 task. */
export interface TaskAttemptRow {
  attemptId: string;
  task: string;
  phase: string;
  attempt: number;
  actor?: string;
  startedAt?: string;
  endedAt?: string;
  outcome: '' | 'passed' | 'rejected' | 'failed' | 'skipped';
  evidence: EvidenceRefRow;
  provenance: AttemptProvenanceRow;
}

/** One App-A lifecycle phase with its Table A-1 weight and binary exit state. */
export interface PhaseRow {
  phase: string;
  weight: number;
  label: string;
  completed: boolean;
  completedAt?: string;
}
```

Add to `ConstructionRow`: `phases: PhaseRow[]`, `attempts: TaskAttemptRow[]`, `classified: boolean`, `worstOrigin: RecordOriginRow`. Rename its `phase` field to `currentLifecyclePhase` and update every reference (`npm run typecheck` will list them).

In `webApp/src/contracts/wire.ts`, inside `mapConstructionRow`, add:

```ts
    phases: (w.Phases ?? []).map((p) => ({
      phase: String(p.Phase ?? ''),
      weight: Number(p.Weight ?? 0),
      label: String(p.Label ?? ''),
      completed: Boolean(p.Completed),
      completedAt: p.CompletedAt ?? undefined,
    })),
    attempts: (w.Attempts ?? []).map(mapTaskAttempt),
    // A dropped flag must not read as classified.
    classified: w.Classified === true,
    worstOrigin: mapOrigin(w.WorstOrigin),
```

and the two helpers beside it:

```ts
/**
 * The wire carries '' for the zero origin, which means SYNTHESIZED. Mapping it to
 * anything else — or defaulting an unknown value to 'observed' — is precisely how a
 * fabricated row would launder itself across the boundary.
 */
function mapOrigin(o: string | null | undefined): RecordOriginRow {
  return o === 'observed' || o === 'backfilled' ? o : 'synthesized';
}

function mapTaskAttempt(a: NonNullable<WireConstructionRow['Attempts']>[number]): TaskAttemptRow {
  return {
    attemptId: String(a.attemptId ?? ''),
    task: String(a.task ?? ''),
    phase: String(a.phase ?? ''),
    attempt: Number(a.attempt ?? 0),
    actor: a.actor || undefined,
    startedAt: a.startedAt ?? undefined,
    endedAt: a.endedAt ?? undefined,
    outcome: (a.outcome ?? '') as TaskAttemptRow['outcome'],
    evidence: { kind: (a.evidence?.kind ?? '') as EvidenceRefRow['kind'], ref: a.evidence?.ref ?? '' },
    provenance: {
      origin: mapOrigin(a.provenance?.origin),
      generator: a.provenance?.generator || undefined,
      generatedAt: a.provenance?.generatedAt ?? undefined,
      basis: a.provenance?.basis || undefined,
    },
  };
}
```

Note `wire.ts`'s documented guard: Go `nil` maps and slices serialize as JSON `null` while `schema.ts` omits `| null`. The `?? []` and `?? undefined` above are that guard, not defensive noise.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd webApp && node --test 'src/contracts/**/*.test.ts' && npm run typecheck && npx eslint src/contracts/
```

Expected: PASS on all four new tests; tsc and eslint clean.

- [ ] **Step 5: Commit**

```bash
git add webApp/src/contracts/wire.ts webApp/src/contracts/types.ts \
        webApp/src/contracts/constructionAdapters.test.ts
git commit -m "feat(construction): read phases and the attempt ledger in the SPA

mapConstructionRow never read w.Phases even though the server emits it and
schema.ts carries it, so there was nothing for the tree to expand.

mapOrigin maps '' and every unknown value to 'synthesized'. Defaulting to
observed is how a fabricated row would launder itself across the boundary.
ConstructionRow.phase, which held a lifecycle phase under the most
collision-prone name in the codebase, is now currentLifecyclePhase.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 10: Stamp the attempt key onto every episode

**Files:**
- Modify: `server/internal/manager/construction/constructactivity.go` (`episodeRecordFor` ~line 264, and its caller `runPipeline` ~line 1262)
- Test: `server/internal/manager/construction/manager_test.go` (append)

**Interfaces:**
- Consumes: `projectstate.AttemptID`, `projectstate.MethodTask` (Task 1).
- Produces: `episodeRecordFor` gaining a `task MethodTask` and `attempt int` parameter; `EpisodeRecord.TargetRef` carrying `<activityId>:<task>:<n>`.

**This is the one change that cannot wait.** Every episode written between now and the capture wave is otherwise permanently unjoinable — the information that would join it exists only at write time and cannot be backfilled. Capture itself stays deferred; only the key changes.

**Do not touch `awaitPhaseDecision` in this file.** Verdict persistence is a later wave.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/manager/construction/manager_test.go`:

```go
func TestEpisodeRecordFor_TargetRefCarriesTheAttemptKey(t *testing.T) {
	rec := episodeRecordFor(episodeInput{
		ActivityID: "C-billing-manager",
		ProjectID:  "archistrator",
	}, projectstate.TaskDetailedDesign, 2)

	want := "C-billing-manager:detailedDesign:2"
	if rec.TargetRef != want {
		t.Errorf("TargetRef = %q, want %q — a bare activityId makes the episode permanently unjoinable", rec.TargetRef, want)
	}
}

func TestEpisodeRecordFor_FirstAttemptIsOne(t *testing.T) {
	rec := episodeRecordFor(episodeInput{ActivityID: "C-x"}, projectstate.TaskConstruction, 1)
	if rec.TargetRef != "C-x:construction:1" {
		t.Errorf("TargetRef = %q, want C-x:construction:1", rec.TargetRef)
	}
}
```

Read `episodeRecordFor`'s actual signature and input struct name before writing this — adapt the call to match.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && GOWORK=off go test ./internal/manager/construction/ -run TestEpisodeRecordFor -v
```

Expected: FAIL — too many arguments, or `TargetRef` is the bare activity id.

- [ ] **Step 3: Write the implementation**

In `episodeRecordFor`, add the two parameters and replace the `TargetRef` assignment:

```go
// episodeRecordFor builds the EpisodeRecord for one dispatched agentic task.
//
// TargetRef carries the ATTEMPT key ("<activityId>:<task>:<n>"), not the bare activity
// id. Episode capture itself is deferred, but the key cannot be: an episode written
// with a bare activity id is permanently unjoinable to the task that produced it,
// because the joining information exists only here, at write time.
func episodeRecordFor(in episodeInput, task projectstate.MethodTask, attempt int) episode.EpisodeRecord {
	// ... existing field population unchanged ...
	rec.TargetRef = projectstate.AttemptID(string(in.ActivityID), task, attempt)
	return rec
}
```

At the call site in `runPipeline`, derive the task from the lifecycle phase it already has in hand, and the attempt number from the redraft counter it already maintains:

```go
	task := projectstate.GateTaskFor(phase)
	if task == "" {
		// A non-gate dispatch: the phase's first task is the work task.
		if tasks := projectstate.TasksForPhase(phase); len(tasks) > 0 {
			task = tasks[0]
		}
	}
	rec := episodeRecordFor(in, task, redraft+1)
```

Adapt `redraft` to whatever the local counter is actually named; it is 0-based, so the attempt number is `redraft+1`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && GOWORK=off go build ./... && GOWORK=off go test ./internal/manager/construction/ -v 2>&1 | tail -20
```

Expected: clean build, all construction tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/manager/construction/constructactivity.go \
        server/internal/manager/construction/manager_test.go
git commit -m "fix(construction): stamp the attempt key onto every episode

episodeRecordFor wrote TargetRef as the bare activityId. Its caller already had
the lifecycle phase and the redraft count in hand, so the attribution was
available and discarded.

Episode capture stays deferred, but the key cannot: an episode written with a
bare activityId is permanently unjoinable to the task that produced it, because
the joining information exists only at write time and cannot be backfilled.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 11: The activity layer projection

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` (add near `ClassifyActivity`)
- Modify: `server/internal/manager/systemdesign/systemdesignmanager.go` (`constructionRowsToContract`)
- Test: `server/internal/resourceaccess/projectstate/access_test.go`

**Interfaces:**
- Consumes: `.systemDesign` component layers (already on the wire), activity ids.
- Produces: `func LayerForActivity(activityID string, componentLayer string) (layer string, band string)`. `band` is `"layered"` when the activity belongs in the Method layer stack and `"projectWide"` when it does not.

The trap this closes: `managerSPAActivityFor` sets `ComponentID = <manager>.ID`, so the naive `componentId → component.layer` join places `U-SPA-billing-manager` — a Client-layer surface — on the Manager row. **The layer of an activity is not the layer of its component.**

Server-side, not TS, because the same rule must hold for the Structurizr render-on-read and the MCP tool output. Duplicating it in TypeScript re-creates the hand-mirror defect class this codebase has already paid for twice.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/resourceaccess/projectstate/access_test.go`:

```go
func TestLayerForActivity_SPASurfaceIsAClientNotItsManagersLayer(t *testing.T) {
	// U-SPA-billing-manager carries componentId = the MANAGER, but it is a Client surface.
	layer, band := LayerForActivity("U-SPA-billing-manager", "manager")
	if layer != "client" {
		t.Errorf("layer = %q, want client — a SPA surface is not its manager's layer", layer)
	}
	if band != "layered" {
		t.Errorf("band = %q, want layered", band)
	}
}

func TestLayerForActivity_CodingActivityTakesItsComponentLayer(t *testing.T) {
	layer, band := LayerForActivity("C-billing-manager", "manager")
	if layer != "manager" || band != "layered" {
		t.Errorf("LayerForActivity = (%q, %q), want (manager, layered)", layer, band)
	}
}

func TestLayerForActivity_ResourceActivityIsResource(t *testing.T) {
	if layer, _ := LayerForActivity("R-github", ""); layer != "resource" {
		t.Errorf("layer = %q, want resource", layer)
	}
}

func TestLayerForActivity_ComponentlessActivitiesAreProjectWide(t *testing.T) {
	for _, id := range []string{"N-IT", "N-STP", "G-SPA", "U-SPA-S"} {
		layer, band := LayerForActivity(id, "")
		if band != "projectWide" {
			t.Errorf("%s: band = %q, want projectWide", id, band)
		}
		if layer != "" {
			t.Errorf("%s: layer = %q, want empty — no fake layer for a cross-cutting activity", id, layer)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestLayerForActivity -v
```

Expected: FAIL — `undefined: LayerForActivity`.

- [ ] **Step 3: Write the implementation**

Add to `projectstateaccess.go`, beside `ClassifyActivity`:

```go
// Layer bands. An activity is either positioned in the Method layer stack or it is
// cross-cutting and belongs in the project-wide band beside it.
const (
	LayerBandLayered     = "layered"
	LayerBandProjectWide = "projectWide"
)

// LayerForActivity returns the Method layer an activity is DRAWN in, plus its band.
//
// This is not the same as the layer of the activity's component, and the difference is
// load-bearing. managerSPAActivityFor sets ComponentID to the MANAGER's id, so joining
// componentId → component.layer would draw U-SPA-billing-manager — a Client-layer
// surface — on the Manager row.
//
// It lives on the server, not in TypeScript, because the same rule must hold for the
// Structurizr render-on-read and the MCP tool output. Mirroring it in TS re-creates the
// hand-mirror defect class this codebase has already paid for twice.
//
// A componentless activity gets NO fake layer: it renders in the project-wide band,
// following the ratified utilities-sidebar convention (side band, no lines drawn into
// the layered stack).
func LayerForActivity(activityID, componentLayer string) (string, string) {
	upper := strings.ToUpper(activityID)
	switch {
	case upper == "U-SPA-S" || upper == "G-SPA":
		return "", LayerBandProjectWide
	case strings.HasPrefix(upper, "N-"):
		return "", LayerBandProjectWide
	case strings.HasPrefix(upper, "U-SPA-"):
		return "client", LayerBandLayered
	case strings.HasPrefix(upper, "R-"):
		return "resource", LayerBandLayered
	case componentLayer != "":
		return componentLayer, LayerBandLayered
	default:
		return "", LayerBandProjectWide
	}
}
```

Note the ordering: the `U-SPA-S` and `G-SPA` cases must precede the `U-SPA-` prefix case, or `U-SPA-S` would be typed as a Client-layer surface.

In `constructionRowsToContract`, resolve the component layer from the committed system design and populate the two fields:

```go
			Layer:     layer,
			LayerBand: band,
```

where `layer, band := projectstate.LayerForActivity(r.ActivityID, componentLayerByID[meta.ComponentID])`, and `componentLayerByID` is built once per call from the committed `.systemDesign` components (empty map when no system design is committed, exactly as `activityMetaByID` handles a missing activity list).

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && GOWORK=off go build ./... && GOWORK=off go test ./internal/resourceaccess/projectstate/ ./internal/manager/systemdesign/ -run 'TestLayerForActivity|TestConstructionRows' -v
```

Expected: clean build, all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/resourceaccess/projectstate/projectstateaccess.go \
        server/internal/resourceaccess/projectstate/access_test.go \
        server/internal/manager/systemdesign/systemdesignmanager.go
git commit -m "feat(construction): server-side activity layer projection

The layer of an activity is not the layer of its component:
managerSPAActivityFor sets ComponentID to the manager's id, so a naive join
would draw U-SPA-billing-manager, a Client-layer surface, on the Manager row.

Server-side because the same rule must hold for the Structurizr render-on-read
and the MCP tool output. Componentless activities get no fake layer — they land
in the project-wide band.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 12: The backfill tool — synthesis as a reviewable artifact

**Files:**
- Create: `server/cmd/backfill-attempts/main.go`
- Create: `server/cmd/backfill-attempts/main_test.go`
- Modify: `.aiarch/state/project.json` (the OUTPUT of the tool — a separate commit)

**Interfaces:**
- Consumes: `TaskAttempt`, `AttemptProvenance`, `TasksForProfile`, `AttemptID`, `ProfileFor`, `ClassifyType` (Tasks 1–4).
- Produces: a one-shot CLI that reads `project.json`, derives attempts for activities with recoverable evidence, and writes them back with an explicit provenance stamp.

**Rule 4 of the provenance contract: synthesis is a committed artifact, never a render-time behaviour.** No server code path may fabricate a row at request time. This tool produces a diff a human reviews and can revert in one commit.

- [ ] **Step 1: Write the failing test**

Create `server/cmd/backfill-attempts/main_test.go`:

```go
package main

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestAttemptsFromContract_IsBackfilledWithABasis(t *testing.T) {
	got := attemptsFor("C-artifact-access", projectstate.ActivityTypeService, evidence{HasServiceContract: true, ContractRef: "artifactAccess"})

	var designReview *projectstate.TaskAttempt
	for i := range got {
		if got[i].Task == projectstate.TaskDesignReview {
			designReview = &got[i]
		}
	}
	if designReview == nil {
		t.Fatal("no designReview attempt derived from a frozen contract")
	}
	if designReview.Provenance.Origin != projectstate.OriginBackfilled {
		t.Errorf("origin = %q, want backfilled — a frozen contract is real evidence", designReview.Provenance.Origin)
	}
	if designReview.Provenance.Basis == "" {
		t.Error("backfilled attempt has no basis; Validate() would reject it")
	}
	if err := designReview.Provenance.Validate(); err != nil {
		t.Errorf("provenance invalid: %v", err)
	}
}

func TestAttemptsFor_NoEvidenceProducesNoAttempts(t *testing.T) {
	got := attemptsFor("C-nothing", projectstate.ActivityTypeService, evidence{})
	if len(got) != 0 {
		t.Errorf("attemptsFor(no evidence) = %d attempts, want 0 — absence must stay absent, not become synthesized rows", len(got))
	}
}

func TestAttemptsFor_EveryAttemptCarriesAValidProvenance(t *testing.T) {
	got := attemptsFor("C-x", projectstate.ActivityTypeService, evidence{HasServiceContract: true, ContractRef: "x", HasMergedCode: true, GitRef: "abc123"})
	for _, a := range got {
		if err := a.Provenance.Validate(); err != nil {
			t.Errorf("%s: %v", a.AttemptID, err)
		}
		if a.AttemptID != projectstate.AttemptID("C-x", a.Task, a.Attempt) {
			t.Errorf("attemptId %q does not match AttemptID()", a.AttemptID)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && GOWORK=off go test ./cmd/backfill-attempts/ -v
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Write the implementation**

Create `server/cmd/backfill-attempts/main.go`:

```go
// cmd/backfill-attempts derives TaskAttempt records for activities whose construction
// history is recoverable from evidence that already exists in project.json — a frozen
// service contract, a merged commit, a produced-artifact note.
//
// It is a ONE-SHOT tool whose output is a REVIEWABLE COMMITTED DIFF. No server code
// path may fabricate an attempt at request time; that is rule 4 of the provenance
// contract, and it is what stops "the UI generates plausible history" from becoming
// permanent architecture.
//
// Every attempt it writes is stamped OriginBackfilled with a basis naming the evidence.
// Activities with NO evidence get NO attempts: absence stays absence. The list view
// renders those as an honest unknown skeleton, which is the point.
//
// Usage (from server/):
//
//	GOWORK=off go run ./cmd/backfill-attempts -repo .. [-dry-run]
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// evidence is what we could actually find for one activity.
type evidence struct {
	HasServiceContract bool
	ContractRef        string
	HasMergedCode      bool
	GitRef             string
	ProducedNote       string
}

// generatorID identifies this tool in every provenance stamp it writes.
var generatorID = "cmd/backfill-attempts"

// attemptsFor derives the recoverable attempts for one activity.
//
// The mapping is deliberately conservative — only two inferences are made, and each
// names the artifact it was read from:
//   - a frozen service contract means Detailed Design ran and Design Review passed
//   - merged code means Construction ran and Code Review passed
//
// Everything else stays unknown. We do NOT infer SRS, STP, Test Client, Integration or
// Testing from anything, because no evidence for them exists in the committed state.
func attemptsFor(activityID string, typ projectstate.ActivityType, ev evidence) []projectstate.TaskAttempt {
	profile := projectstate.ProfileFor(typ, projectstate.TestVariantPlan)
	allowed := map[projectstate.MethodTask]bool{}
	for _, task := range projectstate.TasksForProfile(profile) {
		allowed[task] = true
	}

	now := time.Now().UTC()
	out := []projectstate.TaskAttempt{}

	add := func(task projectstate.MethodTask, ref projectstate.EvidenceRef, basis string) {
		if !allowed[task] {
			return
		}
		out = append(out, projectstate.TaskAttempt{
			AttemptID: projectstate.AttemptID(activityID, task, 1),
			Task:      task,
			Phase:     projectstate.PhaseForTask(task),
			Attempt:   1,
			Actor:     projectstate.ActorAgent,
			Outcome:   projectstate.OutcomePassed,
			Evidence:  ref,
			Provenance: projectstate.AttemptProvenance{
				Origin:      projectstate.OriginBackfilled,
				Generator:   generatorID,
				GeneratedAt: &now,
				Basis:       basis,
			},
		})
	}

	if ev.HasServiceContract {
		ref := projectstate.EvidenceRef{Kind: projectstate.EvidenceContract, Ref: ev.ContractRef}
		basis := fmt.Sprintf("serviceContracts[%s]", ev.ContractRef)
		add(projectstate.TaskDetailedDesign, ref, basis)
		add(projectstate.TaskDesignReview, ref, basis)
	}
	if ev.HasMergedCode {
		ref := projectstate.EvidenceRef{Kind: projectstate.EvidenceGit, Ref: ev.GitRef}
		basis := fmt.Sprintf("activityGit[%s]", ev.GitRef)
		add(projectstate.TaskConstruction, ref, basis)
		add(projectstate.TaskCodeReview, ref, basis)
	}
	return out
}

func main() {
	repo := flag.String("repo", "..", "path to the repository root containing .aiarch/state/project.json")
	dryRun := flag.Bool("dry-run", false, "report what would be written without writing")
	flag.Parse()

	if err := run(*repo, *dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "backfill-attempts: %v\n", err)
		os.Exit(1)
	}
}
```

Implement `run(repo string, dryRun bool) error` to: read `<repo>/.aiarch/state/project.json`; for each entry in `.activityConstruction`, resolve its type via `ClassifyType` (skipping any row where `ok` is false — an unclassified row gets no attempts); build its `evidence` from `Produced` (a `service-contract` kind gives `HasServiceContract` and its `Source`; a `code` kind gives `HasMergedCode`); call `attemptsFor`; write the result into that record's `attempts` array; and marshal the file back with the same 2-space indentation. Under `-dry-run`, print a per-activity count and write nothing.

- [ ] **Step 4: Run tests, then run the tool for real**

```bash
cd server && GOWORK=off go test ./cmd/backfill-attempts/ -v
cd server && GOWORK=off go run ./cmd/backfill-attempts -repo .. -dry-run
```

Expected: 3 tests PASS. The dry run should report attempts for roughly the 29 activities that have a service contract, and **zero** for the rest.

Then run it for real and inspect the diff:

```bash
cd server && GOWORK=off go run ./cmd/backfill-attempts -repo ..
cd .. && git diff --stat .aiarch/state/project.json
git diff .aiarch/state/project.json | head -60
```

Every added attempt must show `"origin": "backfilled"` and a non-empty `"basis"`. If any shows an absent origin, the marshalling dropped it — fix before committing.

- [ ] **Step 5: Commit — the tool and its output separately**

```bash
git add server/cmd/backfill-attempts/
git commit -m "feat(construction): backfill-attempts, synthesis as a reviewable artifact

Derives TaskAttempt records only from evidence that already exists in
project.json — a frozen contract means detailed design ran and design review
passed; merged code means construction ran and code review passed. Nothing else
is inferred, because no evidence for anything else exists.

Activities with no evidence get NO attempts: absence stays absence, and the
list view renders an honest unknown skeleton. Every record is stamped
OriginBackfilled with a basis naming the artifact it came from.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"

git add .aiarch/state/project.json
git commit -m "chore(state): backfill task attempts from existing contract evidence

Output of cmd/backfill-attempts. Every added record is OriginBackfilled with a
basis; no record is synthesized. Revertible in one commit.

NOT FOR MERGE TO MAIN without an explicit founder decision recorded here.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 13: Gate the naming ruling

**Files:**
- Modify: the construction-view arch gate config (find it with the grep in Step 1)
- Test: the arch gate's own test file

**Interfaces:**
- Consumes: the existing paramguard-style banned-string gate (74 strings, mutation-tested).
- Produces: bare `phase` / `Phase` added to the banned identifier list, with `projectstate.Phase` and `ActivityMethodPhase` exempted.

A naming ruling with no gate is a naming suggestion.

- [ ] **Step 1: Locate the existing gate**

```bash
cd server && grep -rn "paramguard\|bannedStrings\|banned" --include=*.go internal/arch/ | head -20
grep -rn "74" --include=*_test.go internal/arch/ | head -5
```

Read the gate's structure and its test before editing. Follow its existing pattern exactly — do not invent a second mechanism.

- [ ] **Step 2: Write the failing test**

Add a case to the gate's test table asserting that a newly declared identifier named exactly `phase` or `Phase` in the construction packages is rejected, and that `projectstate.Phase` and `ActivityMethodPhase` are accepted. Match the file's existing table style.

- [ ] **Step 3: Run test to verify it fails**

```bash
cd server && GOWORK=off go test ./internal/arch/ -v 2>&1 | tail -20
```

Expected: FAIL — the banned list does not yet contain the new entries.

- [ ] **Step 4: Write the implementation and re-run**

Add the entries plus the two exemptions. Then:

```bash
cd server && GOWORK=off go test ./internal/arch/ -v 2>&1 | tail -20
cd server && GOWORK=off make method-check
```

Expected: PASS, and `method-check` clean.

- [ ] **Step 5: Commit**

```bash
git add server/internal/arch/
git commit -m "chore(arch): ban the bare identifier 'phase' in construction code

Four levels were called 'phase' and three of them collided. The ruling keeps
projectstate.Phase (project 1/2/3) and ActivityMethodPhase (the App-A five) and
bans the unqualified word for anything new; Task and Attempt name the two
levels below.

A naming ruling with no gate is a naming suggestion.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Stage A exit criteria

Run all of these before declaring Stage A done:

```bash
cd server
GOWORK=off go build ./...
GOWORK=off go test ./... 2>&1 | tail -30
GOWORK=off make gen-models-check gen-client-check gen-uiprofiles-check
GOWORK=off make derived-plan-check      # must still pass — we did not touch DerivePlan
GOWORK=off make method-check
GOWORK=off make lint

cd ../webApp
npm run typecheck
npx eslint src/
npm test
npm run build
```

Then verify against the live app (server on `:8888`, SPA on `:5199`, both already running from this working tree — restart the Go server to pick up the changes):

```bash
# Rebuild and restart the server, then confirm the wire carries the new fields:
curl -s 'http://localhost:8888/api/v1/systemDesignManager/getProject?projectId=archistrator' \
  | python3 -c "
import json,sys
d=json.load(sys.stdin)
rows=d.get('ActivityConstruction') or {}
print('rows:', len(rows))
withAttempts=[k for k,v in rows.items() if v.get('attempts')]
print('rows with attempts:', len(withAttempts))
unclassified=[k for k,v in rows.items() if v.get('classified') is False]
print('unclassified:', len(unclassified))
labels=[p.get('Label') for v in rows.values() for p in (v.get('Phases') or [])]
print('phase labels present:', sum(1 for l in labels if l))
"
```

Expected: ~69 rows; roughly 29 with attempts; **~60 unclassified** (that is the honest number, not a regression); phase labels non-empty wherever a `Phases` array exists.

**Stage A is done when the server tells the truth.** Stages B (list lens), C (tasks lens) and D (graph lens) consume the types generated here and get their own plans.

## Self-review notes

- **Spec coverage.** R1 → Task 1. R2 → Tasks 3, 6, 7, 9. R3 → Tasks 8, 9, 13. R4 → no task needed (the ruling is "change nothing"; weights already live in `ProfileFor`). R5 → Task 11. R6 must-fix set → Tasks 4 (classifier), 5 (G-SPA bugs b and c), 7 (dropped `Label`), 10 (episode key, the overruled-into-scope item), 12 (provenance stamp). R6's G-SPA bug (a) — the row seeded with the wrong profile — is fixed as a data consequence of Task 12's re-derivation plus Task 4's honest classification; verify it in the exit criteria. R7 → Tasks 2, 6, 9, 12. The status-vocabulary split (`BuildStatus` × `Readiness`, delete `in-detailed-design`) is **deferred to Stage B**, because it is a client-side union in `constructionAdapters.ts` consumed only by the views being rewritten — splitting it before its consumers exist would be churn.
- **Type consistency.** `AttemptID` is the function; `attemptId`/`AttemptId` is the field (Go store camelCase JSON tag; wire casing per whatever `gen-models` emits in Task 6 — Task 7's test says to read `contract.gen.go` first). `LifecyclePhase` replaces `CanonicalPhase` everywhere from Task 8 onward. `ConstructionRow.phase` becomes `currentLifecyclePhase` in Task 9.
- **Known ordering hazard.** Task 4 changes `ClassifyType`'s arity but the `Classified` wire field does not exist until Task 6. Task 4's Step 3 calls this out explicitly and defers the `Classified:` line to Task 7.
