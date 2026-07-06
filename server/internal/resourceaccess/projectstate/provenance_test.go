package projectstate

import (
	"testing"
)

// provenance_test.go — the ADDITIVE commit-provenance record (PM-P2-4). commitTransition
// stamps a supplied Provenance onto the committed slot; a nil prov leaves it untouched. The
// record survives the substrate codec round-trip; the store-level verb server-resolves
// committedAt from the clock.

func TestCommitTransition_StampsProvenance(t *testing.T) {
	p := &Project{}
	p.Mission = ArtifactSlot{Status: ReviewAwaitingReview, Model: &MissionStatement{Vision: "v", Mission: "m"}}

	prov := &Provenance{CommittedAt: "2026-07-06T00:00:00Z", ApprovedBy: "alice", DraftedBy: "agentic-design-rail"}
	if err := commitTransition(KindMission, prov)(p); err != nil {
		t.Fatalf("commitTransition: %v", err)
	}
	got := p.Mission.Provenance
	if got == nil {
		t.Fatal("committed slot must carry the supplied provenance")
	}
	if got.CommittedAt != "2026-07-06T00:00:00Z" || got.ApprovedBy != "alice" || got.DraftedBy != "agentic-design-rail" {
		t.Fatalf("provenance not stamped as supplied: %+v", *got)
	}
}

func TestCommitTransition_NilProvenanceLeavesSlotUntouched(t *testing.T) {
	p := &Project{}
	p.Mission = ArtifactSlot{Status: ReviewAwaitingReview, Model: &MissionStatement{Vision: "v", Mission: "m"}}

	// A plain commit (nil prov) records no provenance — absent provenance is allowed.
	if err := commitTransition(KindMission, nil)(p); err != nil {
		t.Fatalf("commitTransition: %v", err)
	}
	if p.Mission.Provenance != nil {
		t.Fatalf("nil prov must leave provenance absent, got %+v", *p.Mission.Provenance)
	}
}

func TestProvenance_RoundTripsThroughCodec(t *testing.T) {
	p := Project{ID: "p"}
	p.Mission = ArtifactSlot{
		Status:     ReviewCommitted,
		Model:      &MissionStatement{Vision: "v", Mission: "m"},
		Revisions:  1,
		Provenance: &Provenance{CommittedAt: "2026-07-06T12:34:56Z", ApprovedBy: "bob", DraftedBy: "agentic-design-rail (amend-2)"},
	}

	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, ok, err := DecodeProjectJSON(raw, "p")
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	pr := got.Mission.Provenance
	if pr == nil {
		t.Fatal("provenance must survive the encode → decode round-trip")
	}
	if pr.CommittedAt != "2026-07-06T12:34:56Z" || pr.ApprovedBy != "bob" || pr.DraftedBy != "agentic-design-rail (amend-2)" {
		t.Fatalf("provenance round-trip mismatch: %+v", *pr)
	}
}
