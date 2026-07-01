package projectstate

import "testing"

func TestReviewPolicy_EmptyRequiresNoHuman(t *testing.T) {
	var p ReviewPolicy
	if p.RequiresHuman("service", MethodPhaseDetailedDesign) {
		t.Error("empty policy must require no human approval (inert)")
	}
}

func TestReviewPolicy_RequiresHumanForGatedPhase(t *testing.T) {
	p := ReviewPolicy{GatedPhasesByType: map[string][]ActivityMethodPhase{
		"frontend": {MethodPhaseDetailedDesign},
	}}
	if !p.RequiresHuman("frontend", MethodPhaseDetailedDesign) {
		t.Error("frontend/detailed_design should require human")
	}
	if p.RequiresHuman("frontend", MethodPhaseConstruction) {
		t.Error("frontend/construction not gated")
	}
	if p.RequiresHuman("service", MethodPhaseDetailedDesign) {
		t.Error("service not gated")
	}
}

func TestReviewPolicyFromGateIDs_MapsMockIDs(t *testing.T) {
	p := ReviewPolicyFromGateIDs(map[string][]string{"service": {"svc-contract"}})
	if !p.RequiresHuman("service", MethodPhaseDetailedDesign) {
		t.Error("svc-contract must map to detailed_design")
	}
}

// TestProjectDoc_ReviewPolicy_RoundTrip verifies that a Project with a non-empty
// ReviewPolicy encodes and decodes symmetrically via EncodeProjectJSON / DecodeProjectJSON.
func TestProjectDoc_ReviewPolicy_RoundTrip(t *testing.T) {
	p := Project{
		ID:   ProjectID("rp-rt-001"),
		Name: "review policy round-trip",
		ReviewPolicy: ReviewPolicy{
			GatedPhasesByType: map[string][]ActivityMethodPhase{
				"service":  {MethodPhaseDetailedDesign, MethodPhaseIntegration},
				"frontend": {MethodPhaseDetailedDesign},
			},
		},
	}

	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	got, ok, err := DecodeProjectJSON(raw, p.ID)
	if err != nil {
		t.Fatalf("DecodeProjectJSON: %v", err)
	}
	if !ok {
		t.Fatal("DecodeProjectJSON: ok=false")
	}
	if len(got.ReviewPolicy.GatedPhasesByType) != 2 {
		t.Fatalf("ReviewPolicy.GatedPhasesByType len = %d, want 2", len(got.ReviewPolicy.GatedPhasesByType))
	}
	if !got.ReviewPolicy.RequiresHuman("service", MethodPhaseDetailedDesign) {
		t.Error("service/detailed_design lost across round-trip")
	}
	if !got.ReviewPolicy.RequiresHuman("service", MethodPhaseIntegration) {
		t.Error("service/integration lost across round-trip")
	}
	if !got.ReviewPolicy.RequiresHuman("frontend", MethodPhaseDetailedDesign) {
		t.Error("frontend/detailed_design lost across round-trip")
	}
	if got.ReviewPolicy.RequiresHuman("frontend", MethodPhaseConstruction) {
		t.Error("frontend/construction should not be gated after round-trip")
	}
}
