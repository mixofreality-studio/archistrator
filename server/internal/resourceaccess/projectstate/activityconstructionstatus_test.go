package projectstate

import (
	"encoding/json"
	"testing"
)

func TestActivityConstructionStatus_SeededFacets_RoundTrip(t *testing.T) {
	in := ActivityConstructionStatus{
		ActivityID:  "C-CW",
		Phase:       ActivityConstructionDone,
		Kind:        ActivityKindFrontend,
		BuildStatus: BuildIntegrated,
		Produced: []ProducedArtifact{
			{Kind: "service-contract", Title: "webClient — service contract", Source: "implementation/contracts/webClient.md", Produced: true, Note: "frozen App-B contract"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ActivityConstructionStatus
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Kind != ActivityKindFrontend || out.BuildStatus != BuildIntegrated || len(out.Produced) != 1 || out.Produced[0].Source != "implementation/contracts/webClient.md" {
		t.Fatalf("round-trip lost facets: %+v", out)
	}
}

func TestActivityKind_String(t *testing.T) {
	if ActivityKindService.String() != "service" || ActivityKindFrontend.String() != "frontend" || ActivityKindTesting.String() != "testing" {
		t.Fatalf("kind strings wrong")
	}
}

func TestActivityBuildStatus_String(t *testing.T) {
	if BuildIntegrated.String() != "integrated" || BuildInReview.String() != "in-review" || BuildInConstruction.String() != "in-construction" {
		t.Fatalf("status strings wrong")
	}
}

// ---- Task 1: ActivityType + TestingVariant + ActivityMethodPhase ----

func TestActivityType_String(t *testing.T) {
	cases := []struct {
		k    ActivityType
		want string
	}{
		{ActivityTypeService, "service"},
		{ActivityTypeFrontend, "frontend"},
		{ActivityTypeTesting, "testing"},
		{ActivityTypeDeployment, "deployment"},
		{ActivityTypeDocumentation, "documentation"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("ActivityType(%d).String() = %q, want %q", c.k, got, c.want)
		}
	}
}

func TestActivityType_JSONRoundTrip(t *testing.T) {
	// Verify all 5 values marshal to string names and unmarshal back correctly.
	vals := []ActivityType{
		ActivityTypeService, ActivityTypeFrontend, ActivityTypeTesting,
		ActivityTypeDeployment, ActivityTypeDocumentation,
	}
	for _, v := range vals {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %d: %v", v, err)
		}
		var got ActivityType
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %q: %v", b, err)
		}
		if got != v {
			t.Errorf("round-trip: got %d, want %d", got, v)
		}
	}
}

func TestActivityType_LegacyIntDecode(t *testing.T) {
	// Existing project.json entries have Kind as int (0/1/2); must still decode.
	cases := []struct {
		raw  string
		want ActivityType
	}{
		{"0", ActivityTypeService},
		{"1", ActivityTypeFrontend},
		{"2", ActivityTypeTesting},
	}
	for _, c := range cases {
		var got ActivityType
		if err := json.Unmarshal([]byte(c.raw), &got); err != nil {
			t.Errorf("Unmarshal %q: %v", c.raw, err)
		}
		if got != c.want {
			t.Errorf("Unmarshal %q = %d, want %d", c.raw, got, c.want)
		}
	}
}

func TestTestingVariant_String(t *testing.T) {
	cases := []struct {
		v    TestingVariant
		want string
	}{
		{TestVariantPlan, "plan"},
		{TestVariantHarness, "harness"},
		{TestVariantPerf, "perf"},
		{TestVariantSystemTest, "systemTest"},
		{TestVariantQAProcess, "qaProcess"},
	}
	for _, c := range cases {
		if got := c.v.String(); got != c.want {
			t.Errorf("TestingVariant(%d).String() = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestTestingVariant_JSONRoundTrip(t *testing.T) {
	vals := []TestingVariant{
		TestVariantPlan, TestVariantHarness, TestVariantPerf,
		TestVariantSystemTest, TestVariantQAProcess,
	}
	for _, v := range vals {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %d: %v", v, err)
		}
		var got TestingVariant
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %q: %v", b, err)
		}
		if got != v {
			t.Errorf("round-trip: got %d, want %d", got, v)
		}
	}
}

func TestActivityMethodPhase_Constants(t *testing.T) {
	// ActivityMethodPhase is a string type; String() returns the value itself.
	// Verify all expected canonical phase-id constants are defined and non-empty.
	phases := []ActivityMethodPhase{
		// Service / shared phases
		MethodPhaseRequirements,
		MethodPhaseDetailedDesign,
		MethodPhaseTestPlan,
		MethodPhaseConstruction,
		MethodPhaseIntegration,
		// Frontend-specific
		MethodPhaseUXRequirements,
		MethodPhaseUIDesign,
		// Deployment-specific
		MethodPhaseProvisioningSpec,
		MethodPhaseConvergenceVerification,
		// Documentation-specific
		MethodPhaseDocOutline,
		MethodPhaseDocReview,
	}
	for _, p := range phases {
		if string(p) == "" {
			t.Errorf("ActivityMethodPhase constant is empty string")
		}
		if p.String() != string(p) {
			t.Errorf("ActivityMethodPhase(%q).String() = %q, want %q", string(p), p.String(), string(p))
		}
	}
}

func TestActivityMethodPhase_ServicePhaseIDs(t *testing.T) {
	// Verify the canonical IDs the v3 design specifies for service phase set.
	if MethodPhaseRequirements != "requirements" {
		t.Errorf("MethodPhaseRequirements = %q, want %q", MethodPhaseRequirements, "requirements")
	}
	if MethodPhaseDetailedDesign != "detailed_design" {
		t.Errorf("MethodPhaseDetailedDesign = %q, want %q", MethodPhaseDetailedDesign, "detailed_design")
	}
	if MethodPhaseTestPlan != "test_plan" {
		t.Errorf("MethodPhaseTestPlan = %q, want %q", MethodPhaseTestPlan, "test_plan")
	}
	if MethodPhaseConstruction != "construction" {
		t.Errorf("MethodPhaseConstruction = %q, want %q", MethodPhaseConstruction, "construction")
	}
	if MethodPhaseIntegration != "integration" {
		t.Errorf("MethodPhaseIntegration = %q, want %q", MethodPhaseIntegration, "integration")
	}
}

// ---- Task 2: PhaseCompletion + phaseSetFor + CoarsePhase/CoarseBuildStatus ----

func TestPhaseSetFor_Service(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	if len(phases) != 5 {
		t.Fatalf("Service phase set len = %d, want 5", len(phases))
	}
	wantPhases := []ActivityMethodPhase{
		MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
		MethodPhaseConstruction, MethodPhaseIntegration,
	}
	wantWeights := []int{15, 20, 10, 40, 15}
	total := 0
	for i, p := range phases {
		if p.Phase != wantPhases[i] {
			t.Errorf("Service phase[%d] id = %q, want %q", i, p.Phase, wantPhases[i])
		}
		if p.Weight != wantWeights[i] {
			t.Errorf("Service phase[%d] weight = %d, want %d", i, p.Weight, wantWeights[i])
		}
		if p.Completed {
			t.Errorf("Service phase[%d] should not be Completed initially", i)
		}
		total += p.Weight
	}
	if total != 100 {
		t.Errorf("Service phase weights sum = %d, want 100", total)
	}
}

func TestPhaseSetFor_Frontend(t *testing.T) {
	phases := phaseSetFor(ActivityTypeFrontend, 0)
	if len(phases) != 5 {
		t.Fatalf("Frontend phase set len = %d, want 5", len(phases))
	}
	wantPhases := []ActivityMethodPhase{
		MethodPhaseUXRequirements, MethodPhaseUIDesign, MethodPhaseTestPlan,
		MethodPhaseConstruction, MethodPhaseIntegration,
	}
	wantWeights := []int{15, 20, 10, 40, 15}
	total := 0
	for i, p := range phases {
		if p.Phase != wantPhases[i] {
			t.Errorf("Frontend phase[%d] id = %q, want %q", i, p.Phase, wantPhases[i])
		}
		if p.Weight != wantWeights[i] {
			t.Errorf("Frontend phase[%d] weight = %d, want %d", i, p.Weight, wantWeights[i])
		}
		total += p.Weight
	}
	if total != 100 {
		t.Errorf("Frontend phase weights sum = %d, want 100", total)
	}
}

func TestPhaseSetFor_Deployment(t *testing.T) {
	phases := phaseSetFor(ActivityTypeDeployment, 0)
	if len(phases) != 3 {
		t.Fatalf("Deployment phase set len = %d, want 3", len(phases))
	}
	wantPhases := []ActivityMethodPhase{
		MethodPhaseProvisioningSpec, MethodPhaseConstruction, MethodPhaseConvergenceVerification,
	}
	wantWeights := []int{25, 50, 25}
	total := 0
	for i, p := range phases {
		if p.Phase != wantPhases[i] {
			t.Errorf("Deployment phase[%d] = %v, want %v", i, p.Phase, wantPhases[i])
		}
		if p.Weight != wantWeights[i] {
			t.Errorf("Deployment phase[%d] weight = %d, want %d", i, p.Weight, wantWeights[i])
		}
		total += p.Weight
	}
	if total != 100 {
		t.Errorf("Deployment phase weights sum = %d, want 100", total)
	}
}

func TestPhaseSetFor_Documentation(t *testing.T) {
	phases := phaseSetFor(ActivityTypeDocumentation, 0)
	if len(phases) != 3 {
		t.Fatalf("Documentation phase set len = %d, want 3", len(phases))
	}
	wantPhases := []ActivityMethodPhase{
		MethodPhaseDocOutline, MethodPhaseConstruction, MethodPhaseDocReview,
	}
	wantWeights := []int{20, 60, 20}
	total := 0
	for i, p := range phases {
		if p.Phase != wantPhases[i] {
			t.Errorf("Documentation phase[%d] = %v, want %v", i, p.Phase, wantPhases[i])
		}
		if p.Weight != wantWeights[i] {
			t.Errorf("Documentation phase[%d] weight = %d, want %d", i, p.Weight, wantWeights[i])
		}
		total += p.Weight
	}
	if total != 100 {
		t.Errorf("Documentation phase weights sum = %d, want 100", total)
	}
}

func TestPhaseSetFor_TestingPlan(t *testing.T) {
	phases := phaseSetFor(ActivityTypeTesting, TestVariantPlan)
	if len(phases) != 3 {
		t.Fatalf("Testing/Plan phase set len = %d, want 3", len(phases))
	}
	wantPhases := []ActivityMethodPhase{
		MethodPhaseUseCaseTrace, MethodPhasePlanAuthoring, MethodPhasePlanReview,
	}
	wantWeights := []int{20, 45, 35}
	total := 0
	for i, p := range phases {
		if p.Phase != wantPhases[i] {
			t.Errorf("Testing/Plan phase[%d] = %q, want %q", i, p.Phase, wantPhases[i])
		}
		if p.Weight != wantWeights[i] {
			t.Errorf("Testing/Plan phase[%d] weight = %d, want %d", i, p.Weight, wantWeights[i])
		}
		total += p.Weight
	}
	if total != 100 {
		t.Errorf("Testing/Plan phase weights sum = %d, want 100", total)
	}
}

func TestPhaseSetFor_TestingHarness(t *testing.T) {
	phases := phaseSetFor(ActivityTypeTesting, TestVariantHarness)
	if len(phases) != 4 {
		t.Fatalf("Testing/Harness phase set len = %d, want 4", len(phases))
	}
	wantPhases := []ActivityMethodPhase{
		MethodPhaseHarnessDesign, MethodPhaseHarnessConstruction,
		MethodPhaseCoverage, MethodPhaseHarnessReview,
	}
	wantWeights := []int{15, 50, 20, 15}
	total := 0
	for i, p := range phases {
		if p.Phase != wantPhases[i] {
			t.Errorf("Testing/Harness phase[%d] = %q, want %q", i, p.Phase, wantPhases[i])
		}
		if p.Weight != wantWeights[i] {
			t.Errorf("Testing/Harness phase[%d] weight = %d, want %d", i, p.Weight, wantWeights[i])
		}
		total += p.Weight
	}
	if total != 100 {
		t.Errorf("Testing/Harness phase weights sum = %d, want 100", total)
	}
}

func TestPhaseSetFor_TestingPerf(t *testing.T) {
	phases := phaseSetFor(ActivityTypeTesting, TestVariantPerf)
	if len(phases) != 3 {
		t.Fatalf("Testing/Perf phase set len = %d, want 3", len(phases))
	}
	wantPhases := []ActivityMethodPhase{
		MethodPhasePerfScenarioDesign, MethodPhaseRigConstruction, MethodPhaseRigReview,
	}
	wantWeights := []int{25, 50, 25}
	total := 0
	for i, p := range phases {
		if p.Phase != wantPhases[i] {
			t.Errorf("Testing/Perf phase[%d] = %q, want %q", i, p.Phase, wantPhases[i])
		}
		if p.Weight != wantWeights[i] {
			t.Errorf("Testing/Perf phase[%d] weight = %d, want %d", i, p.Weight, wantWeights[i])
		}
		total += p.Weight
	}
	if total != 100 {
		t.Errorf("Testing/Perf phase weights sum = %d, want 100", total)
	}
}

func TestPhaseSetFor_TestingSystemTest(t *testing.T) {
	phases := phaseSetFor(ActivityTypeTesting, TestVariantSystemTest)
	if len(phases) != 5 {
		t.Fatalf("Testing/SystemTest phase set len = %d, want 5", len(phases))
	}
	wantPhases := []ActivityMethodPhase{
		MethodPhaseSmokePass, MethodPhaseUseCaseExecution,
		MethodPhaseRegressionSuite, MethodPhaseDefectResolution, MethodPhaseSignOff,
	}
	wantWeights := []int{10, 45, 25, 15, 5}
	total := 0
	for i, p := range phases {
		if p.Phase != wantPhases[i] {
			t.Errorf("Testing/SystemTest phase[%d] = %q, want %q", i, p.Phase, wantPhases[i])
		}
		if p.Weight != wantWeights[i] {
			t.Errorf("Testing/SystemTest phase[%d] weight = %d, want %d", i, p.Weight, wantWeights[i])
		}
		total += p.Weight
	}
	if total != 100 {
		t.Errorf("Testing/SystemTest phase weights sum = %d, want 100", total)
	}
}

func TestPhaseSetFor_TestingQAProcess(t *testing.T) {
	phases := phaseSetFor(ActivityTypeTesting, TestVariantQAProcess)
	if len(phases) != 2 {
		t.Fatalf("Testing/QAProcess phase set len = %d, want 2", len(phases))
	}
	wantPhases := []ActivityMethodPhase{
		MethodPhaseGateDefinition, MethodPhaseProcessAudit,
	}
	wantWeights := []int{40, 60}
	total := 0
	for i, p := range phases {
		if p.Phase != wantPhases[i] {
			t.Errorf("Testing/QAProcess phase[%d] = %q, want %q", i, p.Phase, wantPhases[i])
		}
		if p.Weight != wantWeights[i] {
			t.Errorf("Testing/QAProcess phase[%d] weight = %d, want %d", i, p.Weight, wantWeights[i])
		}
		total += p.Weight
	}
	if total != 100 {
		t.Errorf("Testing/QAProcess phase weights sum = %d, want 100", total)
	}
}

func TestPhaseSetFor_AllVariantsSum100(t *testing.T) {
	// Exhaustive weight-sum check for every type/variant combination.
	cases := []struct {
		name string
		t    ActivityType
		v    TestingVariant
	}{
		{"Service", ActivityTypeService, 0},
		{"Frontend", ActivityTypeFrontend, 0},
		{"Testing/Plan", ActivityTypeTesting, TestVariantPlan},
		{"Testing/Harness", ActivityTypeTesting, TestVariantHarness},
		{"Testing/Perf", ActivityTypeTesting, TestVariantPerf},
		{"Testing/SystemTest", ActivityTypeTesting, TestVariantSystemTest},
		{"Testing/QAProcess", ActivityTypeTesting, TestVariantQAProcess},
		{"Deployment", ActivityTypeDeployment, 0},
		{"Documentation", ActivityTypeDocumentation, 0},
	}
	for _, c := range cases {
		phases := phaseSetFor(c.t, c.v)
		total := 0
		for _, p := range phases {
			total += p.Weight
		}
		if total != 100 {
			t.Errorf("%s: phase weights sum = %d, want 100", c.name, total)
		}
	}
}

func TestPhaseCompletion_JSONRoundTrip(t *testing.T) {
	// Verify PhaseCompletion marshals/unmarshals correctly including optional fields.
	pc := PhaseCompletion{
		Phase:       MethodPhaseRequirements,
		Weight:      15,
		Completed:   true,
		ArtifactRef: "phaseArtifacts/srs/C-IE",
	}
	b, err := json.Marshal(pc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got PhaseCompletion
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Phase != MethodPhaseRequirements || got.Weight != 15 || !got.Completed || got.ArtifactRef != "phaseArtifacts/srs/C-IE" {
		t.Errorf("round-trip lost data: %+v", got)
	}
}

func TestActivityConstructionStatus_BackCompatNoPhasesField(t *testing.T) {
	// Existing project.json entries without "phases" must still decode (nil Phases is fine).
	raw := `{"activityID":"C-CW","phase":2,"kind":1,"buildStatus":2,"produced":[{"Kind":"service-contract","Title":"webClient","Source":"implementation/contracts/webClient.md","Produced":true}]}`
	var got ActivityConstructionStatus
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal legacy entry: %v", err)
	}
	if got.ActivityID != "C-CW" {
		t.Errorf("ActivityID = %q, want C-CW", got.ActivityID)
	}
	if got.Phase != ActivityConstructionDone {
		t.Errorf("Phase = %v, want Done", got.Phase)
	}
	if got.Kind != ActivityKindFrontend {
		t.Errorf("Kind = %v, want Frontend", got.Kind)
	}
	if got.Phases != nil {
		t.Errorf("Phases should be nil for legacy entry, got %v", got.Phases)
	}
}

func TestCoarsePhase_AllDone(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	for i := range phases {
		phases[i].Completed = true
	}
	if got := CoarsePhase(phases); got != ActivityConstructionDone {
		t.Errorf("CoarsePhase(all done) = %v, want Done", got)
	}
}

func TestCoarsePhase_NoneStarted(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	if got := CoarsePhase(phases); got != ActivityConstructionNotStarted {
		t.Errorf("CoarsePhase(none started) = %v, want NotStarted", got)
	}
}

func TestCoarsePhase_SomeCompleted(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	phases[0].Completed = true
	if got := CoarsePhase(phases); got != ActivityConstructionRunning {
		t.Errorf("CoarsePhase(some completed) = %v, want Running", got)
	}
}

func TestCoarsePhase_EmptyPhases(t *testing.T) {
	if got := CoarsePhase(nil); got != ActivityConstructionNotStarted {
		t.Errorf("CoarsePhase(nil) = %v, want NotStarted", got)
	}
	if got := CoarsePhase([]PhaseCompletion{}); got != ActivityConstructionNotStarted {
		t.Errorf("CoarsePhase([]) = %v, want NotStarted", got)
	}
}

func TestCoarseBuildStatus_Integrated(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	// Mark both Construction and Integration done.
	for i := range phases {
		if phases[i].Phase == MethodPhaseConstruction || phases[i].Phase == MethodPhaseIntegration {
			phases[i].Completed = true
		}
	}
	if got := CoarseBuildStatus(phases, MethodPhaseIntegration); got != BuildIntegrated {
		t.Errorf("CoarseBuildStatus(integration done) = %v, want Integrated", got)
	}
}

func TestCoarseBuildStatus_InReview(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	// Mark only Construction done (not Integration).
	for i := range phases {
		if phases[i].Phase == MethodPhaseConstruction {
			phases[i].Completed = true
		}
	}
	if got := CoarseBuildStatus(phases, MethodPhaseIntegration); got != BuildInReview {
		t.Errorf("CoarseBuildStatus(construction done, integration not) = %v, want InReview", got)
	}
}

func TestCoarseBuildStatus_InConstruction(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	if got := CoarseBuildStatus(phases, MethodPhaseConstruction); got != BuildInConstruction {
		t.Errorf("CoarseBuildStatus(nothing done) = %v, want InConstruction", got)
	}
}

func TestTestingVariantPhaseIDs_WireValues(t *testing.T) {
	// Verify testing-variant phase-id constants have the correct snake_case wire values.
	cases := []struct {
		c    ActivityMethodPhase
		want string
	}{
		{MethodPhaseUseCaseTrace, "use_case_trace"},
		{MethodPhasePlanAuthoring, "plan_authoring"},
		{MethodPhasePlanReview, "plan_review"},
		{MethodPhaseHarnessDesign, "harness_design"},
		{MethodPhaseHarnessConstruction, "harness_construction"},
		{MethodPhaseCoverage, "coverage"},
		{MethodPhaseHarnessReview, "harness_review"},
		{MethodPhasePerfScenarioDesign, "perf_scenario_design"},
		{MethodPhaseRigConstruction, "rig_construction"},
		{MethodPhaseRigReview, "rig_review"},
		{MethodPhaseSmokePass, "smoke_pass"},
		{MethodPhaseUseCaseExecution, "use_case_execution"},
		{MethodPhaseRegressionSuite, "regression_suite"},
		{MethodPhaseDefectResolution, "defect_resolution"},
		{MethodPhaseSignOff, "sign_off"},
		{MethodPhaseGateDefinition, "gate_definition"},
		{MethodPhaseProcessAudit, "process_audit"},
	}
	for _, c := range cases {
		if string(c.c) != c.want {
			t.Errorf("constant %q = %q, want %q", c.want, string(c.c), c.want)
		}
	}
}
