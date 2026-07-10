package projectstate

import (
	"encoding/json"
	"testing"
)

// reconcile_test.go — coverage for the F80 deterministic project.json reconciler.

// projDoc renders a minimal project.json document with the given slot entries.
// Each entry is keyed by kind ordinal, mirroring the on-disk slotsMap shape.
func projDoc(t *testing.T, slots map[int]map[string]any) []byte {
	t.Helper()
	doc := map[string]any{"schemaVersion": 1, "slots": slots}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}
	return b
}

func committedMissionSlot(text string) map[string]any {
	return map[string]any{
		"status": int(ReviewCommitted), "kind": int(KindMission),
		"model": map[string]any{"vision": text, "objectives": []any{}, "statement": text},
	}
}

func awaitingVolatilitiesSlot(name string) map[string]any {
	return map[string]any{
		"status": int(ReviewAwaitingReview), "kind": int(KindVolatilities),
		"model": map[string]any{"items": []any{
			map[string]any{"name": name, "rationale": "r", "axis": "sameCustomerOverTime"},
		}},
	}
}

// The reconciler takes main's document + overlays the session's own slot from the branch.
// Main-side advances to OTHER slots survive; the session's slot wins.
func TestReconcileSlotOntoBase(t *testing.T) {
	// main: Mission committed at "v2" (advanced while the session ran) + Volatilities
	// committed at an OLD value.
	base := projDoc(t, map[int]map[string]any{
		int(KindMission):      committedMissionSlot("v2-advanced-on-main"),
		int(KindVolatilities): awaitingVolatilitiesSlot("old-committed"),
	})
	// session branch: the in-flight Volatilities draft (AwaitingReview) + a STALE Mission
	// (still at v1, from when the branch was cut).
	ours := projDoc(t, map[int]map[string]any{
		int(KindMission):      committedMissionSlot("v1-stale-on-branch"),
		int(KindVolatilities): awaitingVolatilitiesSlot("in-flight-draft"),
	})

	reconciled, err := ReconcileSlotOntoBase(base, ours, ProjectID("p"), KindVolatilities)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	proj, ok, err := DecodeProjectJSON(reconciled, ProjectID("p"))
	if err != nil || !ok {
		t.Fatalf("decode reconciled: ok=%v err=%v", ok, err)
	}
	// The session's OWN slot (Volatilities) is the branch's in-flight draft.
	vol, isVol := proj.Volatilities.Model.(*Volatilities)
	if !isVol || len(vol.Items) != 1 || vol.Items[0].Name != "in-flight-draft" {
		t.Fatalf("volatilities slot must be the branch draft, got: %+v", proj.Volatilities.Model)
	}
	// Every OTHER slot comes from main (the concurrent advance survives).
	mission, isMission := proj.Mission.Model.(*MissionStatement)
	if !isMission || mission.Vision != "v2-advanced-on-main" {
		t.Fatalf("mission slot must be main's advanced value, got: %+v", proj.Mission.Model)
	}
}

func TestReconcileSlotOntoBase_RejectsUndecodableInput(t *testing.T) {
	base := projDoc(t, map[int]map[string]any{int(KindMission): committedMissionSlot("v")})
	if _, err := ReconcileSlotOntoBase([]byte("{not json"), base, ProjectID("p"), KindMission); err == nil {
		t.Fatal("an undecodable base must be rejected")
	}
	if _, err := ReconcileSlotOntoBase(base, []byte("{not json"), ProjectID("p"), KindMission); err == nil {
		t.Fatal("an undecodable ours must be rejected")
	}
}

// OverlaySlotFromBranchOntoMain is the in-memory twin; it mutates main in place.
func TestOverlaySlotFromBranchOntoMain(t *testing.T) {
	main := Project{}
	main.Mission = ArtifactSlot{Status: ReviewCommitted, Model: &MissionStatement{Vision: "main-mission"}}
	main.Volatilities = ArtifactSlot{Status: ReviewCommitted, Model: &Volatilities{Items: []Volatility{{Name: "old"}}}}

	branch := Project{}
	branch.Volatilities = ArtifactSlot{Status: ReviewAwaitingReview, Model: &Volatilities{Items: []Volatility{{Name: "draft"}}}}

	if err := OverlaySlotFromBranchOntoMain(&main, &branch, KindVolatilities); err != nil {
		t.Fatalf("overlay: %v", err)
	}
	vol, _ := main.Volatilities.Model.(*Volatilities)
	if vol == nil || len(vol.Items) != 1 || vol.Items[0].Name != "draft" {
		t.Fatalf("volatilities must be the branch draft, got %+v", main.Volatilities.Model)
	}
	if main.Volatilities.Status != ReviewAwaitingReview {
		t.Fatalf("the whole slot (incl. status) must overlay, got status %v", main.Volatilities.Status)
	}
	mission, _ := main.Mission.Model.(*MissionStatement)
	if mission == nil || mission.Vision != "main-mission" {
		t.Fatalf("mission must be untouched, got %+v", main.Mission.Model)
	}
}
