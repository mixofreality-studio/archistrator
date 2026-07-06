package projectstate

import (
	"encoding/json"
	"testing"
)

// A slot COMMITTED before the Revisions field existed persists with the revisions key
// omitted (zero-value). Decoding it must GRANDFATHER Revisions to 1 — a committed artifact
// is by definition revision 1 — so the amendment index (max(1,Revisions)) selects a real
// -amend-N branch and, crucially, a re-commit lands at 2 (not 1), keeping successive
// -amend-N branch names unique. A never-committed slot must stay at 0 so its FIRST commit
// still lands at 1.
func Test_decodeSlotsMap_GrandfathersPreFieldCommittedRevisions(t *testing.T) {
	missionJSON, err := json.Marshal(&MissionStatement{Vision: "v", Mission: "m"})
	if err != nil {
		t.Fatalf("marshal mission: %v", err)
	}
	w := map[string]slotJSON{
		// Pre-field COMMITTED slot: revisions omitted ⇒ entry.Revisions == 0.
		"committed": {Kind: int(KindMission), Status: int(ReviewCommitted), Model: missionJSON},
		// NON-committed slot (awaiting review): must NOT be grandfathered.
		"awaiting": {Kind: int(KindGlossary), Status: int(ReviewAwaitingReview), Model: mustJSON(t, &Glossary{})},
	}
	var p Project
	if err := decodeSlotsMap(w, &p); err != nil {
		t.Fatalf("decodeSlotsMap: %v", err)
	}
	if p.Mission.Revisions != 1 {
		t.Fatalf("pre-field COMMITTED slot must grandfather to Revisions 1, got %d", p.Mission.Revisions)
	}
	if p.Glossary.Revisions != 0 {
		t.Fatalf("a non-committed slot must stay at Revisions 0 (its first commit lands at 1), got %d", p.Glossary.Revisions)
	}
}

// End-to-end for item 3: a pre-field committed slot, once GRANDFATHERED on read to Revisions 1,
// lands at 2 on its first re-commit (amendment) via commitTransition's ++ — never at 1 — so the
// pre-field slot's second amendment gets a UNIQUE -amend-2 branch instead of colliding on -amend-1.
func Test_commitTransition_PreFieldReCommitLandsAtTwo(t *testing.T) {
	missionJSON, err := json.Marshal(&MissionStatement{Vision: "v", Mission: "m"})
	if err != nil {
		t.Fatalf("marshal mission: %v", err)
	}
	var p Project
	if err := decodeSlotsMap(map[string]slotJSON{
		"committed": {Kind: int(KindMission), Status: int(ReviewCommitted), Model: missionJSON},
	}, &p); err != nil {
		t.Fatalf("decodeSlotsMap: %v", err)
	}
	// Grandfathered base is 1.
	if p.Mission.Revisions != 1 {
		t.Fatalf("grandfathered base must be Revisions 1, got %d", p.Mission.Revisions)
	}
	// A re-commit (the amendment merge → CommitArtifact) bumps to 2.
	if err := commitTransition(KindMission, nil)(&p); err != nil {
		t.Fatalf("commitTransition: %v", err)
	}
	if p.Mission.Revisions != 2 {
		t.Fatalf("a pre-field slot's re-commit must land at 2 (first commit was 1), got %d", p.Mission.Revisions)
	}
}

// A never-committed slot's FIRST commit still lands at 1 (the grandfather only floors
// COMMITTED slots, so it does not inflate a genuine first commit).
func Test_commitTransition_FirstCommitLandsAtOne(t *testing.T) {
	missionJSON, err := json.Marshal(&MissionStatement{Vision: "v", Mission: "m"})
	if err != nil {
		t.Fatalf("marshal mission: %v", err)
	}
	var p Project
	if err := decodeSlotsMap(map[string]slotJSON{
		"awaiting": {Kind: int(KindMission), Status: int(ReviewAwaitingReview), Model: missionJSON},
	}, &p); err != nil {
		t.Fatalf("decodeSlotsMap: %v", err)
	}
	if p.Mission.Revisions != 0 {
		t.Fatalf("a never-committed slot must decode at Revisions 0, got %d", p.Mission.Revisions)
	}
	if err := commitTransition(KindMission, nil)(&p); err != nil {
		t.Fatalf("commitTransition: %v", err)
	}
	if p.Mission.Revisions != 1 {
		t.Fatalf("a genuine first commit must land at 1, got %d", p.Mission.Revisions)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
