package systemdesign

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// catalog_provenance_test.go — PM-P2-4 exposure. slotsToContract projects a committed slot's
// stored Provenance onto the ArtifactSlotView.Provenance read model; an uncommitted / pre-
// provenance slot exposes nil (omitempty on the wire).
func TestSlotsToContract_ExposesProvenance(t *testing.T) {
	var p projectstate.Project
	p.Mission = projectstate.ArtifactSlot{
		Status:    projectstate.ReviewCommitted,
		Model:     &projectstate.MissionStatement{Vision: "v", Mission: "m"},
		Revisions: 1,
		Provenance: &projectstate.Provenance{
			CommittedAt: "2026-07-06T08:30:00Z",
			ApprovedBy:  "alice",
			DraftedBy:   "agentic-design-rail",
		},
	}
	// Volatilities left uncommitted (no provenance) to prove the nil-safe path.

	slots := slotsToContract(p)

	var mission, volatilities *ArtifactSlotView
	for i := range slots {
		switch slots[i].Kind {
		case projectstate.KindMission.WireName():
			mission = &slots[i]
		case projectstate.KindVolatilities.WireName():
			volatilities = &slots[i]
		}
	}
	if mission == nil || volatilities == nil {
		t.Fatal("expected mission + volatilities slots in the contract view")
	}
	if mission.Provenance == nil {
		t.Fatal("committed mission slot must expose provenance")
	}
	if mission.Provenance.CommittedAt != "2026-07-06T08:30:00Z" ||
		mission.Provenance.ApprovedBy != "alice" ||
		mission.Provenance.DraftedBy != "agentic-design-rail" {
		t.Fatalf("provenance view mismatch: %+v", *mission.Provenance)
	}
	if volatilities.Provenance != nil {
		t.Fatalf("uncommitted slot must expose nil provenance, got %+v", *volatilities.Provenance)
	}
}
