package projectdesign

import "testing"

// stagename_test.go — F72 stageName label for the Phase-2 manager. Phase-2's Stage enum
// values DIFFER from Phase-1's (projectdesign StageAwaitingReview == 3, systemdesign == 2), so
// the human-readable StageName label removes the cross-manager ambiguity. sessionStageLabel is
// the single authoritative map; withStageName stamps it.

func TestSessionStageLabel_Map(t *testing.T) {
	cases := map[SessionStage]string{
		SessionStageUnknown: "not started",
		StageDrafting:       "drafting",
		StageAssemblingSDP:  "assembling SDP",
		StageAwaitingReview: "awaiting review",
		StageRedrafting:     "redrafting",
		StageCommitted:      "committed",
		StageWithdrawn:      "withdrawn",
		StageRefused:        "refused",
		StageDraftFailed:    "draft failed",
	}
	for stage, want := range cases {
		if got := sessionStageLabel(stage); got != want {
			t.Errorf("sessionStageLabel(%d) = %q, want %q", int(stage), got, want)
		}
	}
}

func TestWithStageName_StampsLabel(t *testing.T) {
	// projectdesign StageAwaitingReview is the int 3 (vs systemdesign's 2) — the label is the
	// portable disambiguator across the two managers' divergent enums.
	if int(StageAwaitingReview) != 3 {
		t.Fatalf("guard: expected projectdesign StageAwaitingReview == 3, got %d", int(StageAwaitingReview))
	}
	v := withStageName(SessionStateView{Stage: StageAwaitingReview})
	if v.StageName != "awaiting review" {
		t.Fatalf("withStageName StageName = %q, want %q", v.StageName, "awaiting review")
	}
	if v.Stage != StageAwaitingReview {
		t.Fatalf("withStageName must not alter the Stage int, got %d", int(v.Stage))
	}
}
