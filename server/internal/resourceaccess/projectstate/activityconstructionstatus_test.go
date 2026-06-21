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
