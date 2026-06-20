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
