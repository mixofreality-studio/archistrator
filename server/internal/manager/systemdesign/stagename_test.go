package systemdesign

import "testing"

// stagename_test.go — F72 stageName label. The bare Stage int's enum values differ across
// managers (systemdesign StageAwaitingReview == 2), so a human-readable StageName label ships
// alongside it. sessionStageLabel is the single authoritative map; withStageName stamps it.

func TestSessionStageLabel_Map(t *testing.T) {
	cases := map[SessionStage]string{
		SessionStageUnknown: "not started",
		StageDrafting:       "drafting",
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
	// systemdesign StageAwaitingReview is the int 2 — the label disambiguates it.
	if int(StageAwaitingReview) != 2 {
		t.Fatalf("guard: expected systemdesign StageAwaitingReview == 2, got %d", int(StageAwaitingReview))
	}
	v := withStageName(SessionStateView{Stage: StageAwaitingReview})
	if v.StageName != "awaiting review" {
		t.Fatalf("withStageName StageName = %q, want %q", v.StageName, "awaiting review")
	}
	if v.Stage != StageAwaitingReview {
		t.Fatalf("withStageName must not alter the Stage int, got %d", int(v.Stage))
	}
}
