# Lifecycle Consolidation Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse the nine bespoke per-type "phase sets" into ONE canonical 5-phase lifecycle, expressed as per-type *profiles* (canonical phase-id + weight + display label), and make the construction pump walk the activity's profile instead of a hardwired `servicePhases`.

**Architecture:** Introduce a `Profile` value in the `projectstate` package that maps an `(ActivityType, TestingVariant)` to a subset of the five canonical phases (`requirements → detailed_design → test_plan → construction → integration`) with per-phase weights and display labels. `phaseSetFor` becomes a thin adapter over `ProfileFor`. Bespoke `MethodPhase*` constants and `phaseSetForTestingVariant` are deleted. The activity's resolved phase list is attached to the pump's `constructionActivity` at hydrate time (via new `DeriveType`/`DeriveVariant`), and the pump walks that list. The service path is behavior-identical (its profile is the canonical five).

**Tech Stack:** Go 1.25.4, stdlib `testing` (table-driven, no testify), Temporal `testsuite` for the pump test.

## Global Constraints

- Server is de-workspaced for test/vet: run everything with `GOWORK=off` from the `server/` directory. Single package: `GOWORK=off go test ./internal/<pkg>/`. Full: `make test` (= `GOWORK=off go test ./...`).
- Tests: stdlib `testing` only, table-driven, `t.Errorf`/`t.Fatalf`. No testify, no mock frameworks.
- Canonical phase ids are FROZEN and kept: `requirements`, `detailed_design`, `test_plan`, `construction`, `integration`. All bespoke phase ids (`ux_requirements`, `ui_design`, `provisioning_spec`, `convergence_verification`, `doc_outline`, `doc_review`, `use_case_trace`, `plan_authoring`, `plan_review`, `harness_design`, `harness_construction`, `coverage`, `harness_review`, `perf_scenario_design`, `rig_construction`, `rig_review`, `smoke_pass`, `use_case_execution`, `regression_suite`, `defect_resolution`, `sign_off`, `gate_definition`, `process_audit`) are DELETED.
- Lint gate — a golangci-lint v2 rollout is IN PROGRESS and owned by the founder. There are ~89 pre-existing issues that are NOT ours to fix. So NEW code must be clean via a delta check, not the whole-repo run: from `server/`, `PATH="/opt/homebrew/bin:$PATH" GOWORK=off golangci-lint run --new-from-rev=main ./...` (only flags issues in lines this branch changed). Do NOT run/rely on `make lint` (whole-repo, trips the pre-existing 89). Do NOT modify or clobber `server/.golangci.yml` or `.superpowers/sdd/golangci-bumps.md` — the founder owns that rollout. Relevant new-code linters: `gochecksumtype` (keep any `switch` over the sealed `ActivityType` exhaustive), `gocognit`/`gocyclo`/`nestif`/`funlen` (keep new funcs small — the pump already carries `//nolint:gocognit`; do not add branches that push it over), `gosec`. Also run `make encapsulation-check` (`TestGeneratedOnlyPublic`) and `make sumtype-check`. `Profile`/`ProfilePhase`/`ProfileFor`/`DeriveType`/`DeriveVariant` ARE intended public API.
- Service path must stay behavior-identical: a service activity walks the same five canonical phase ids in the same order; only the store's per-phase `Label`/`Weight` metadata is additive.
- Profiles carry `phases + weights + labels` ONLY in this plan. The `rolePerPhase` / `artifactKind` / `reviewExperience` fields from the design spec are added in Plan 2 (UI). Do not add unused fields now (unused-field lint + YAGNI).

---

### Task 1: Profile model + profile table

Introduce the `Profile` type and the full per-type profile table in a new focused file, plus the `Label` field on `PhaseCompletion`. This is pure data + pure functions — no I/O.

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/activityconstructionstatus.go` (add `Label` to `PhaseCompletion`, struct at lines 98-104)
- Create: `server/internal/resourceaccess/projectstate/activityprofile.go`
- Test: `server/internal/resourceaccess/projectstate/activityprofile_test.go`

**Interfaces:**
- Produces:
  - `type ProfilePhase struct { Phase ActivityMethodPhase; Weight int; Label string }`
  - `type Profile struct { Phases []ProfilePhase }`
  - `func (pr Profile) toPhaseCompletions() []PhaseCompletion` (unexported; used by Task 2)
  - `func (pr Profile) PhaseIDs() []ActivityMethodPhase` (exported; used by Task 4)
  - `func ProfileFor(t ActivityType, v TestingVariant) Profile`
  - `PhaseCompletion.Label string` json:`label,omitempty`

- [ ] **Step 1: Add the `Label` field to `PhaseCompletion`**

In `activityconstructionstatus.go`, change the struct (lines 98-104) to:

```go
type PhaseCompletion struct {
	Phase       ActivityMethodPhase `json:"phase"`
	Weight      int                 `json:"weight"`
	Label       string              `json:"label,omitempty"`
	Completed   bool                `json:"completed,omitempty"`
	CompletedAt *time.Time          `json:"completedAt,omitempty"`
	ArtifactRef string              `json:"artifactRef,omitempty"`
}
```

- [ ] **Step 2: Write the failing profile-table test**

Create `activityprofile_test.go`:

```go
package projectstate

import "testing"

func canonicalIDsAllowed(p ActivityMethodPhase) bool {
	switch p {
	case MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
		MethodPhaseConstruction, MethodPhaseIntegration:
		return true
	}
	return false
}

func TestProfileFor_AllCanonicalIDsAndSum100(t *testing.T) {
	cases := []struct {
		name    string
		typ     ActivityType
		variant TestingVariant
		wantLen int
	}{
		{"service", ActivityTypeService, 0, 5},
		{"frontend", ActivityTypeFrontend, 0, 5},
		{"deployment", ActivityTypeDeployment, 0, 3},
		{"documentation", ActivityTypeDocumentation, 0, 3},
		{"testing_plan", ActivityTypeTesting, TestVariantPlan, 3},
		{"testing_harness", ActivityTypeTesting, TestVariantHarness, 3},
		{"testing_perf", ActivityTypeTesting, TestVariantPerf, 3},
		{"testing_systemtest", ActivityTypeTesting, TestVariantSystemTest, 3},
		{"testing_qa", ActivityTypeTesting, TestVariantQAProcess, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pr := ProfileFor(c.typ, c.variant)
			if len(pr.Phases) != c.wantLen {
				t.Fatalf("%s: len = %d, want %d", c.name, len(pr.Phases), c.wantLen)
			}
			total := 0
			for _, p := range pr.Phases {
				if !canonicalIDsAllowed(p.Phase) {
					t.Errorf("%s: non-canonical phase id %q", c.name, p.Phase)
				}
				if p.Label == "" {
					t.Errorf("%s: phase %q has empty label", c.name, p.Phase)
				}
				total += p.Weight
			}
			if total != 100 {
				t.Errorf("%s: weight sum = %d, want 100", c.name, total)
			}
		})
	}
}

func TestProfileFor_ServiceIsCanonicalFive(t *testing.T) {
	got := ProfileFor(ActivityTypeService, 0).PhaseIDs()
	want := []ActivityMethodPhase{
		MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
		MethodPhaseConstruction, MethodPhaseIntegration,
	}
	if len(got) != len(want) {
		t.Fatalf("service PhaseIDs len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("service PhaseIDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestProfileFor_TestingPlanRelabelsCanonicalIDs(t *testing.T) {
	pr := ProfileFor(ActivityTypeTesting, TestVariantPlan)
	want := []ProfilePhase{
		{MethodPhaseRequirements, 20, "Use-Case Trace"},
		{MethodPhaseConstruction, 45, "Plan Authoring"},
		{MethodPhaseIntegration, 35, "Plan Review"},
	}
	if len(pr.Phases) != len(want) {
		t.Fatalf("plan profile len = %d, want %d", len(pr.Phases), len(want))
	}
	for i, w := range want {
		if pr.Phases[i] != w {
			t.Errorf("plan phase[%d] = %+v, want %+v", i, pr.Phases[i], w)
		}
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestProfileFor -v`
Expected: FAIL — `undefined: ProfileFor`, `undefined: ProfilePhase`.

- [ ] **Step 4: Create the profile table**

Create `activityprofile.go`:

```go
package projectstate

// ProfilePhase pairs a canonical ActivityMethodPhase with its per-profile weight
// and human-facing display label. The phase id is ALWAYS one of the five canonical
// ids (Requirements/DetailedDesign/TestPlan/Construction/Integration) so the shared
// earned-value/progress formula (Appendix A) stays uniform across all activity
// types; only the label and weight vary per profile.
type ProfilePhase struct {
	Phase  ActivityMethodPhase
	Weight int
	Label  string
}

// Profile is the per-activity-type preset over the ONE canonical lifecycle: an
// ordered subset of the five canonical phases with weights and display labels.
// It is NOT a distinct lifecycle — it is weights + labels + a phase subset over
// the single shared phase vocabulary (Righting Software, Appendix A / Table A-1).
type Profile struct {
	Phases []ProfilePhase
}

// PhaseIDs returns the ordered canonical phase ids for this profile — the sequence
// the construction pump dispatches.
func (pr Profile) PhaseIDs() []ActivityMethodPhase {
	ids := make([]ActivityMethodPhase, len(pr.Phases))
	for i, p := range pr.Phases {
		ids[i] = p.Phase
	}
	return ids
}

// toPhaseCompletions materializes the profile into the store's PhaseCompletion
// slice (seeded, all Completed=false).
func (pr Profile) toPhaseCompletions() []PhaseCompletion {
	out := make([]PhaseCompletion, len(pr.Phases))
	for i, p := range pr.Phases {
		out[i] = PhaseCompletion{Phase: p.Phase, Weight: p.Weight, Label: p.Label}
	}
	return out
}

// ProfileFor returns the canonical-phase profile for an activity type (and testing
// variant, meaningful only when t == ActivityTypeTesting). All ids are canonical;
// bespoke phase ids are gone. Weights sum to 100 within each profile.
func ProfileFor(t ActivityType, v TestingVariant) Profile {
	switch t {
	case ActivityTypeFrontend:
		// Code-as-design: design-heavy, construction is data-wiring.
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 15, "UX Requirements"},
			{MethodPhaseDetailedDesign, 25, "Design"},
			{MethodPhaseTestPlan, 10, "Flows"},
			{MethodPhaseConstruction, 35, "Construction"},
			{MethodPhaseIntegration, 15, "Integration"},
		}}
	case ActivityTypeTesting:
		return profileForTestingVariant(v)
	case ActivityTypeDeployment:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 25, "Provisioning Spec"},
			{MethodPhaseConstruction, 50, "Construction"},
			{MethodPhaseIntegration, 25, "Convergence Verification"},
		}}
	case ActivityTypeDocumentation:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 20, "Outline"},
			{MethodPhaseConstruction, 60, "Authoring"},
			{MethodPhaseIntegration, 20, "Doc Review"},
		}}
	default: // ActivityTypeService — the canonical five.
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 15, "Requirements"},
			{MethodPhaseDetailedDesign, 20, "Detailed Design"},
			{MethodPhaseTestPlan, 10, "Test Plan"},
			{MethodPhaseConstruction, 40, "Construction"},
			{MethodPhaseIntegration, 15, "Integration"},
		}}
	}
}

func profileForTestingVariant(v TestingVariant) Profile {
	switch v {
	case TestVariantHarness:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 15, "Harness Design"},
			{MethodPhaseConstruction, 70, "Harness Construction"},
			{MethodPhaseIntegration, 15, "Harness Review"},
		}}
	case TestVariantPerf:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 25, "Perf Scenario Design"},
			{MethodPhaseConstruction, 50, "Rig Construction"},
			{MethodPhaseIntegration, 25, "Rig Review"},
		}}
	case TestVariantSystemTest:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 10, "Smoke Pass"},
			{MethodPhaseConstruction, 45, "Use-Case Execution"},
			{MethodPhaseIntegration, 45, "Regression & Sign-off"},
		}}
	case TestVariantQAProcess:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 40, "Gate Definition"},
			{MethodPhaseConstruction, 60, "Process Audit"},
		}}
	default: // TestVariantPlan (N-STP)
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 20, "Use-Case Trace"},
			{MethodPhaseConstruction, 45, "Plan Authoring"},
			{MethodPhaseIntegration, 35, "Plan Review"},
		}}
	}
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestProfileFor -v`
Expected: PASS (all three functions).

- [ ] **Step 6: Commit**

```bash
git add server/internal/resourceaccess/projectstate/activityprofile.go \
        server/internal/resourceaccess/projectstate/activityprofile_test.go \
        server/internal/resourceaccess/projectstate/activityconstructionstatus.go
git commit -m "feat(projectstate): canonical-phase Profile model + per-type profile table"
```

---

### Task 2: Retire `phaseSetFor` internals + delete bespoke phase ids

Make `phaseSetFor` a thin adapter over `ProfileFor`, delete `phaseSetForTestingVariant` and all bespoke `MethodPhase*` constants, and rewrite the model tests that hard-asserted the old ids/weights.

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/activityconstructionstatus.go` (replace `phaseSetFor` lines 150-186; delete `phaseSetForTestingVariant` lines 190-229; delete bespoke constant blocks lines 414-471, keep canonical block lines 404-410)
- Modify: `server/internal/resourceaccess/projectstate/activityconstructionstatus_test.go` (rewrite the nine `TestPhaseSetFor_*`, `TestPhaseSetFor_AllVariantsSum100`, `TestActivityMethodPhase_Constants`, `TestTestingVariantPhaseIDs_WireValues`)

**Interfaces:**
- Consumes: `ProfileFor`, `Profile.toPhaseCompletions` (Task 1)
- Produces: `func phaseSetFor(t ActivityType, v TestingVariant) []PhaseCompletion` (same signature — callers in `gitconstruction.go:163,198` unchanged)

- [ ] **Step 1: Replace `phaseSetFor` body**

In `activityconstructionstatus.go`, replace the whole function (lines 150-186) with:

```go
// phaseSetFor returns the seeded PhaseCompletion slice for an activity type/variant.
// It is a thin adapter over ProfileFor — the single source of truth for the phase
// tables. Kept for its existing call sites in gitconstruction.go.
func phaseSetFor(t ActivityType, v TestingVariant) []PhaseCompletion {
	return ProfileFor(t, v).toPhaseCompletions()
}
```

Then DELETE `phaseSetForTestingVariant` entirely (old lines 190-229).

- [ ] **Step 2: Delete the bespoke phase-id constants**

In `activityconstructionstatus.go`, KEEP the canonical block (lines 404-410: `MethodPhaseRequirements`, `MethodPhaseDetailedDesign`, `MethodPhaseTestPlan`, `MethodPhaseConstruction`, `MethodPhaseIntegration`). DELETE every other `MethodPhase*` const block (the Frontend, Deployment, Documentation, and all Testing-variant blocks, old lines 414-471).

- [ ] **Step 3: Rewrite the affected model tests**

In `activityconstructionstatus_test.go`:
- DELETE `TestPhaseSetFor_Frontend`, `_Deployment`, `_Documentation`, `_TestingPlan`, `_TestingHarness`, `_TestingPerf`, `_TestingSystemTest`, `_TestingQAProcess` and `TestPhaseSetFor_AllVariantsSum100` (they assert deleted ids). Their coverage is now in `activityprofile_test.go`.
- KEEP `TestPhaseSetFor_Service` but it now also validates labels; replace it with:

```go
func TestPhaseSetFor_Service(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	wantPhases := []ActivityMethodPhase{
		MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
		MethodPhaseConstruction, MethodPhaseIntegration,
	}
	wantWeights := []int{15, 20, 10, 40, 15}
	if len(phases) != len(wantPhases) {
		t.Fatalf("service phase set len = %d, want %d", len(phases), len(wantPhases))
	}
	total := 0
	for i, p := range phases {
		if p.Phase != wantPhases[i] {
			t.Errorf("phase[%d] = %q, want %q", i, p.Phase, wantPhases[i])
		}
		if p.Weight != wantWeights[i] {
			t.Errorf("phase[%d] weight = %d, want %d", i, p.Weight, wantWeights[i])
		}
		if p.Label == "" {
			t.Errorf("phase[%d] %q has empty label", i, p.Phase)
		}
		if p.Completed {
			t.Errorf("phase[%d] seeded Completed=true", i)
		}
		total += p.Weight
	}
	if total != 100 {
		t.Errorf("weight sum = %d, want 100", total)
	}
}
```

- REPLACE `TestActivityMethodPhase_Constants` (old lines 146-171) so it only asserts the five canonical constants:

```go
func TestActivityMethodPhase_Constants(t *testing.T) {
	cases := map[ActivityMethodPhase]string{
		MethodPhaseRequirements:   "requirements",
		MethodPhaseDetailedDesign: "detailed_design",
		MethodPhaseTestPlan:       "test_plan",
		MethodPhaseConstruction:   "construction",
		MethodPhaseIntegration:    "integration",
	}
	for p, want := range cases {
		if p.String() != want {
			t.Errorf("%v.String() = %q, want %q", p, p.String(), want)
		}
	}
}
```

- DELETE `TestTestingVariantPhaseIDs_WireValues` (old lines 555-584 — asserts deleted testing phase ids).

- [ ] **Step 4: Run the package tests to verify they pass**

Run: `GOWORK=off go test ./internal/resourceaccess/projectstate/ -v`
Expected: PASS. If the compiler reports a lingering reference to a deleted `MethodPhase*` constant, grep it (`grep -rn "MethodPhaseUX\|MethodPhaseUI\|MethodPhaseHarness\|MethodPhasePlan\|MethodPhaseUseCase\|MethodPhaseSmoke\|MethodPhaseGate\|MethodPhaseProcess\|MethodPhasePerf\|MethodPhaseRig\|MethodPhaseCoverage\|MethodPhaseProvisioning\|MethodPhaseConvergence\|MethodPhaseDoc\|MethodPhaseRegression\|MethodPhaseDefect\|MethodPhaseSignOff" server/`) and remove the reference (there should be none in non-test code per reconnaissance).

- [ ] **Step 5: Commit**

```bash
git add server/internal/resourceaccess/projectstate/activityconstructionstatus.go \
        server/internal/resourceaccess/projectstate/activityconstructionstatus_test.go
git commit -m "refactor(projectstate): phaseSetFor delegates to ProfileFor; delete bespoke phase ids"
```

---

### Task 3: Populate `Type`/`Variant` — close the seeding gap

Add `DeriveType` and `DeriveVariant` (name-prefix derivation) and have the seed generator write `cs.Type`/`cs.Variant`, so `phaseSetFor(cs.Type, cs.Variant)` finally resolves per-type at seed time.

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/corpusderive.go` (alongside `DeriveKind` at lines 23-34)
- Modify: `server/cmd/seed-construction/main.go` (row seed at lines 76-82)
- Test: `server/internal/resourceaccess/projectstate/corpusderive_test.go` (create if absent, else append)

**Interfaces:**
- Produces:
  - `func DeriveType(activityID string) ActivityType`
  - `func DeriveVariant(activityID string) TestingVariant`

- [ ] **Step 1: Write the failing derivation test**

Append to (or create) `corpusderive_test.go`:

```go
package projectstate

import "testing"

func TestDeriveType_Prefixes(t *testing.T) {
	cases := map[string]ActivityType{
		"U-SPA-Home": ActivityTypeFrontend,
		"N-STP":      ActivityTypeTesting,
		"N-IT":       ActivityTypeTesting,
		"C-Orders":   ActivityTypeService,
		"E-Pricing":  ActivityTypeService,
	}
	for id, want := range cases {
		if got := DeriveType(id); got != want {
			t.Errorf("DeriveType(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestDeriveVariant_TestingPrefixes(t *testing.T) {
	cases := map[string]TestingVariant{
		"N-STP":  TestVariantPlan,
		"N-STH":  TestVariantHarness,
		"N-PERF": TestVariantPerf,
		"N-IT":   TestVariantSystemTest,
		"N-QA":   TestVariantQAProcess,
		"N-OTHER": TestVariantPlan, // unknown N- falls back to Plan
	}
	for id, want := range cases {
		if got := DeriveVariant(id); got != want {
			t.Errorf("DeriveVariant(%q) = %v, want %v", id, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `GOWORK=off go test ./internal/resourceaccess/projectstate/ -run "TestDerive" -v`
Expected: FAIL — `undefined: DeriveType`, `undefined: DeriveVariant`.

- [ ] **Step 3: Implement the derivations**

In `corpusderive.go`, add:

```go
// DeriveType maps an activity id prefix to its canonical ActivityType. Mirrors
// DeriveKind's prefix logic (U-SPA → Frontend, N- → Testing, else Service) but is
// the forward-looking name (DeriveKind is retained for the legacy Kind field).
func DeriveType(activityID string) ActivityType {
	id := strings.ToUpper(activityID)
	switch {
	case strings.HasPrefix(id, "U-SPA"):
		return ActivityTypeFrontend
	case strings.HasPrefix(id, "N-"):
		return ActivityTypeTesting
	default:
		return ActivityTypeService
	}
}

// DeriveVariant maps a testing activity id prefix to its TestingVariant. Meaningful
// only when DeriveType == ActivityTypeTesting; unknown N- ids fall back to Plan.
// Order matters: N-STH / N-STP share the "N-ST" stem, so match the longer first.
func DeriveVariant(activityID string) TestingVariant {
	id := strings.ToUpper(activityID)
	switch {
	case strings.HasPrefix(id, "N-STH"):
		return TestVariantHarness
	case strings.HasPrefix(id, "N-STP"):
		return TestVariantPlan
	case strings.HasPrefix(id, "N-PERF"):
		return TestVariantPerf
	case strings.HasPrefix(id, "N-IT"):
		return TestVariantSystemTest
	case strings.HasPrefix(id, "N-QA"):
		return TestVariantQAProcess
	default:
		return TestVariantPlan
	}
}
```

Confirm `strings` is already imported in `corpusderive.go` (it is — used by `DeriveKind`).

- [ ] **Step 4: Seed `Type`/`Variant` in the generator**

In `server/cmd/seed-construction/main.go`, change the row seed (lines 76-82) to also set `Type` and `Variant`:

```go
rows[id] = projectstate.ActivityConstructionStatus{
	ActivityID:  id,
	Phase:       phase,
	Kind:        projectstate.DeriveKind(id, comp),
	Type:        projectstate.DeriveType(id),
	Variant:     projectstate.DeriveVariant(id),
	BuildStatus: status,
	Produced:    projectstate.DeriveProduced(cp, comp),
}
```

- [ ] **Step 5: Run derivation + build the seed command**

Run: `GOWORK=off go test ./internal/resourceaccess/projectstate/ -run "TestDerive" -v`
Expected: PASS.
Run: `GOWORK=off go build ./cmd/seed-construction/`
Expected: builds clean.

- [ ] **Step 6: Commit**

```bash
git add server/internal/resourceaccess/projectstate/corpusderive.go \
        server/internal/resourceaccess/projectstate/corpusderive_test.go \
        server/cmd/seed-construction/main.go
git commit -m "feat(projectstate): DeriveType/DeriveVariant; seed populates Type+Variant"
```

---

### Task 4: Attach the resolved phase list to the pump's activity

Give `constructionActivity` a `Phases` field carrying the canonical phase ids for that activity, populated at hydrate time from `DeriveType`/`DeriveVariant`/`ProfileFor`. This crosses the Temporal boundary, so it must be a plain serializable slice.

**Files:**
- Modify: `server/internal/manager/construction/deps.go` (`constructionActivity` struct, lines 134-142)
- Modify: `server/internal/manager/construction/adapters.go` (`hydrateConstructionActivity`, lines 201-212)
- Test: `server/internal/manager/construction/adapters_test.go` (create if absent, else append)

**Interfaces:**
- Consumes: `projectstate.DeriveType`, `projectstate.DeriveVariant`, `projectstate.ProfileFor`, `Profile.PhaseIDs` (Task 1/3)
- Produces: `constructionActivity.Phases []projectstate.ActivityMethodPhase`

- [ ] **Step 1: Write the failing hydrate test**

Append to (or create) `adapters_test.go`:

```go
package construction

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestHydrateConstructionActivity_ServicePhases(t *testing.T) {
	got := hydrateConstructionActivity("C-Orders", projectstate.ActivityItem{Coding: true, EffortDays: 5}, "comp-1")
	want := []projectstate.ActivityMethodPhase{
		projectstate.MethodPhaseRequirements, projectstate.MethodPhaseDetailedDesign,
		projectstate.MethodPhaseTestPlan, projectstate.MethodPhaseConstruction,
		projectstate.MethodPhaseIntegration,
	}
	if len(got.Phases) != len(want) {
		t.Fatalf("phases len = %d, want %d", len(got.Phases), len(want))
	}
	for i := range want {
		if got.Phases[i] != want[i] {
			t.Errorf("phase[%d] = %q, want %q", i, got.Phases[i], want[i])
		}
	}
}

func TestHydrateConstructionActivity_TestingPlanIsThreePhases(t *testing.T) {
	got := hydrateConstructionActivity("N-STP", projectstate.ActivityItem{Coding: true}, "")
	want := []projectstate.ActivityMethodPhase{
		projectstate.MethodPhaseRequirements, projectstate.MethodPhaseConstruction,
		projectstate.MethodPhaseIntegration,
	}
	if len(got.Phases) != len(want) {
		t.Fatalf("N-STP phases len = %d, want %d", len(got.Phases), len(want))
	}
	for i := range want {
		if got.Phases[i] != want[i] {
			t.Errorf("phase[%d] = %q, want %q", i, got.Phases[i], want[i])
		}
	}
}
```

(Confirm the module path in the import matches the repo — grep an existing test in `server/internal/manager/construction/` for the exact `projectstate` import path and copy it verbatim.)

- [ ] **Step 2: Run to verify it fails**

Run: `GOWORK=off go test ./internal/manager/construction/ -run TestHydrateConstructionActivity -v`
Expected: FAIL — `got.Phases undefined`.

- [ ] **Step 3: Add the `Phases` field**

In `deps.go`, extend the struct (lines 134-142):

```go
type constructionActivity struct {
	ActivityID   string
	Kind         activityKind
	ComponentID  string
	Layer        string
	EstimateDays float64
	CRLabel      string
	IsRevert     bool
	Phases       []projectstate.ActivityMethodPhase
}
```

(Confirm `projectstate` is imported in `deps.go`; it is used elsewhere in the package — add the import if this specific file lacks it.)

- [ ] **Step 4: Populate `Phases` in hydrate**

In `adapters.go`, update `hydrateConstructionActivity` (lines 201-212):

```go
func hydrateConstructionActivity(activityID string, item projectstate.ActivityItem, componentID string) constructionActivity {
	kind := activityKindNoncoding
	if item.Coding {
		kind = activityKindConstruction
	}
	typ := projectstate.DeriveType(activityID)
	variant := projectstate.DeriveVariant(activityID)
	return constructionActivity{
		ActivityID:   activityID,
		Kind:         kind,
		ComponentID:  componentID,
		EstimateDays: item.EffortDays,
		Phases:       projectstate.ProfileFor(typ, variant).PhaseIDs(),
	}
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `GOWORK=off go test ./internal/manager/construction/ -run TestHydrateConstructionActivity -v`
Expected: PASS (both functions).

- [ ] **Step 6: Commit**

```bash
git add server/internal/manager/construction/deps.go \
        server/internal/manager/construction/adapters.go \
        server/internal/manager/construction/adapters_test.go
git commit -m "feat(construction): hydrate resolves activity profile phase list"
```

---

### Task 5: Pump walks the activity's phase list

Replace the hardwired `servicePhases` loop with a walk over `in.Activity.Phases`, and update the pump test.

**Files:**
- Modify: `server/internal/manager/construction/workflow.go` (loop line 445; `servicePhases` var lines 518-524; doc-comments 435-443, 514-517)
- Modify: `server/internal/manager/construction/workflow_test.go` (pump assertion ~line 421; `sampleActivity` ~line 381)

**Interfaces:**
- Consumes: `constructionActivity.Phases` (Task 4)

- [ ] **Step 1: Update the pump loop**

In `workflow.go`, change the loop header (line 445) from `for _, phase := range servicePhases {` to:

```go
	for _, phase := range in.Activity.Phases {
```

- [ ] **Step 2: Add a defensive fallback + retire `servicePhases`**

Immediately BEFORE the loop (before line 444 `phaseFailed := false`), add a guard so an activity that arrived without a resolved profile still walks the canonical service five (belt-and-suspenders for any pre-Task-4 in-flight workflow):

```go
	if len(in.Activity.Phases) == 0 {
		in.Activity.Phases = projectstate.ProfileFor(projectstate.ActivityTypeService, 0).PhaseIDs()
	}
```

Then DELETE the `servicePhases` var (lines 518-524) since nothing references it. Update the doc-comment at lines 435-443 to describe "walk the activity's profile phases" instead of the hardcoded service five.

- [ ] **Step 3: Update the pump test**

In `workflow_test.go`:
- `sampleActivity()` (~line 381) — add the resolved phases so the sample matches production hydrate:

```go
func sampleActivity() constructionActivity {
	return constructionActivity{
		ActivityID:  "C-XYZ",
		Kind:        activityKindConstruction,
		ComponentID: "comp-1",
		Layer:       "engine",
		Phases:      projectstate.ProfileFor(projectstate.ActivityTypeService, 0).PhaseIDs(),
	}
}
```

- The pump assertion (~line 421) — change `len(servicePhases)` to the activity's phase count:

```go
	if len(pipe.submitted) != len(sampleActivity().Phases) {
		t.Fatalf("submitted %d pipelines, want %d", len(pipe.submitted), len(sampleActivity().Phases))
	}
```

- Add a test proving a non-service profile drives a different phase count. Append:

```go
func Test_Construct_TestingPlanWalksThreePhases(t *testing.T) {
	// Arrange the spine exactly as the happy-path pump test does, but with a
	// testing-plan activity (3 canonical phases instead of 5). Reuse the existing
	// test harness/setup helper the happy-path test uses; substitute the activity:
	act := constructionActivity{
		ActivityID:  "N-STP",
		Kind:        activityKindConstruction,
		ComponentID: "system",
		Phases:      projectstate.ProfileFor(projectstate.ActivityTypeTesting, projectstate.TestVariantPlan).PhaseIDs(),
	}
	if len(act.Phases) != 3 {
		t.Fatalf("precondition: testing-plan phases = %d, want 3", len(act.Phases))
	}
	// ... drive the same fakePipeline-backed environment used by
	// Test_Construct_HappyPath_* with `act`, then assert:
	//   len(pipe.submitted) == 3
	// (Copy the environment setup from the existing happy-path test verbatim —
	// do not invent a new harness.)
}
```

> Implementer note: the happy-path pump test (`workflow_test.go:389-424`) already builds the `fakePipeline` + `NewTestWorkflowEnvironment` scaffold. Clone that setup for this test, swapping in `act`; the ONLY new assertion is `len(pipe.submitted) == 3`. If cloning the scaffold balloons the test, extract the shared setup into a helper `runPumpWith(t, act) *fakePipeline` in the test file and call it from both.

- [ ] **Step 4: Run the construction package tests**

Run: `GOWORK=off go test ./internal/manager/construction/ -v`
Expected: PASS. Watch for `undefined: servicePhases` (delete any remaining reference) and gocognit on the pump function (the loop change is net-neutral; if lint trips, the guard added in Step 2 is the only new branch — keep it a single `if`).

- [ ] **Step 5: Commit**

```bash
git add server/internal/manager/construction/workflow.go \
        server/internal/manager/construction/workflow_test.go
git commit -m "refactor(construction): pump walks the activity profile, not hardwired servicePhases"
```

---

### Task 6: Full-suite green + migration verification

Prove the whole refactor is behavior-preserving: service path unchanged, no bespoke phase ids remain, legacy decode intact. This is the task that resolves spec §8's migration question with evidence.

**Files:**
- Test: `server/internal/resourceaccess/projectstate/gitconstruction_test.go` (verify seeding tests still pass; adjust label assertions if any)
- Test: `server/internal/resourceaccess/projectstate/enumjson_test.go` (legacy int decode — should already pass untouched)

- [ ] **Step 1: Confirm no bespoke phase-id strings survive anywhere**

Run:
```bash
grep -rn "ux_requirements\|ui_design\|provisioning_spec\|convergence_verification\|doc_outline\|doc_review\|use_case_trace\|plan_authoring\|plan_review\|harness_design\|harness_construction\|harness_review\|perf_scenario_design\|rig_construction\|rig_review\|smoke_pass\|use_case_execution\|regression_suite\|defect_resolution\|sign_off\|gate_definition\|process_audit" server/ --include=*.go
```
Expected: no matches (the labels use spaces/capitals, e.g. `"Use-Case Trace"`, so they won't match these snake_case ids). If a match appears in non-generated code, remove it.

- [ ] **Step 2: Verify the seeding tests still pass (service path identical)**

Run: `GOWORK=off go test ./internal/resourceaccess/projectstate/ -run "TestRecordPhaseStarted\|TestRecordPhaseCompleted" -v`
Expected: PASS. `RecordPhaseStarted` seeds `phaseSetFor(cs.Type, cs.Variant)` → for a zero-Type legacy row that decodes as Service, this is the canonical five (unchanged). If a seeding test asserts an exact `PhaseCompletion` literal, add the now-present `Label` field to the expected value.

- [ ] **Step 3: Verify legacy decode is intact (no data migration needed)**

Run: `GOWORK=off go test ./internal/resourceaccess/projectstate/ -run "TestActivityType_LegacyIntDecode\|TestActivityType\|TestTestingVariant" -v`
Expected: PASS. This confirms the finding: legacy `project.json` rows (bare-int `kind`, empty `type`) still decode; because `Type` was never populated historically, every persisted `Phases` slice already uses canonical ids — there is nothing to remap.

- [ ] **Step 4: Full server suite + vet + delta-lint**

Run (from `server/`):
```bash
GOWORK=off go test ./...
GOWORK=off go vet ./...
make sumtype-check
make encapsulation-check
PATH="/opt/homebrew/bin:$PATH" GOWORK=off golangci-lint run --new-from-rev=main ./...
```
Expected: `go test`/`go vet` green; `sumtype-check`/`encapsulation-check` green; the delta-lint reports ZERO new issues (pre-existing issues elsewhere are the founder's rollout, not ours — the `--new-from-rev=main` scope excludes them). Do NOT run `make lint` (whole-repo). `encapsulation-check` (`TestGeneratedOnlyPublic`) must still pass with the new exported `Profile`/`ProfilePhase`/`ProfileFor`/`DeriveType`/`DeriveVariant`; if it flags them, they are legitimately public API — follow the guidance the test prints for registering intended-public symbols (do NOT unexport them; the pump in another package consumes `ProfileFor`/`PhaseIDs`). If the delta-lint flags a new `gocognit`/`funlen` issue in your code, refactor it small; do NOT edit `.golangci.yml`.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "test(projectstate): verify service path identical + legacy decode; no phase-id migration needed"
```

---

## Self-Review (completed during authoring)

**Spec coverage** (spec §3 target model + §3.1 + §6 step 1):
- One canonical lifecycle + profile mechanism → Task 1 (`Profile`/`ProfileFor`), Task 2 (delete bespoke).
- Canonical ids kept, labels per-profile → Task 1 (`ProfilePhase.Label`), Task 2 (canonical constants only).
- Pump reads profile not hardwired `servicePhases` → Task 5.
- Close the Type/Variant population gap (reconnaissance finding) → Task 3, Task 4.
- Migration = no-op, verified → Task 6.
- Review routing (`review.go`) is intentionally UNTOUCHED — it keys on artifactKind strings, not phase ids; it changes in Plan 2 (UI) when `reviewExperience` becomes real. Noted so the gap is deliberate, not missed.
- `rolePerPhase`/`artifactKind`/`reviewExperience` profile fields deferred to Plan 2 (Global Constraints) — YAGNI.

**Placeholder scan:** the only prose-with-ellipsis is Task 5 Step 3's second test, which explicitly instructs cloning the existing happy-path scaffold rather than inventing one (the concrete assertion — `len(pipe.submitted) == 3` — is given). All code steps contain complete code.

**Type consistency:** `ProfilePhase{Phase, Weight, Label}`, `Profile{Phases}`, `Profile.PhaseIDs() []ActivityMethodPhase`, `constructionActivity.Phases []projectstate.ActivityMethodPhase`, `DeriveType(string) ActivityType`, `DeriveVariant(string) TestingVariant`, `phaseSetFor(ActivityType, TestingVariant) []PhaseCompletion` — names/signatures match across Tasks 1→6.
