package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// TestPutDraftModel_ValidWritesAmbientKind proves a valid model is accepted, written into
// the AMBIENT slot only (never another), at Committed status (the status the CI gate and
// the server read-back require).
func TestPutDraftModel_ValidWritesAmbientKind(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	model := `{"items":[{"name":"Pricing model","rationale":"Pricing changes over time.","axis":"sameCustomerOverTime"}]}`
	if err := s.putDraftModel([]byte(model)); err != nil {
		t.Fatalf("valid draft rejected: %v", err)
	}
	if !s.wroteState {
		t.Fatalf("wroteState not set after a successful draft")
	}
	slot := readBackSlot(t, s, projectstate.KindVolatilities)
	if slot.Status != projectstate.ReviewCommitted {
		t.Fatalf("ambient slot status = %d, want Committed(%d)", slot.Status, projectstate.ReviewCommitted)
	}
	if slot.Model == nil || slot.Model.Kind() != projectstate.KindVolatilities {
		t.Fatalf("ambient slot model not written correctly: %+v", slot.Model)
	}
	// No sibling slot was touched.
	if other := readBackSlot(t, s, projectstate.KindMission); other.Status != projectstate.ReviewNone {
		t.Fatalf("a non-ambient slot (Mission) was mutated: status %d", other.Status)
	}
}

// TestPutDraftModel_BadEnumRejected is the F36 killer: free prose in a closed-enum field
// is rejected by the strict codec HERE (server read-back parity), and nothing is written.
func TestPutDraftModel_BadEnumRejected(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	bad := `{"items":[{"name":"Pricing","rationale":"x","axis":"it changes sometimes"}]}`
	err := s.putDraftModel([]byte(bad))
	if err == nil {
		t.Fatalf("expected a validation error for a free-prose enum value")
	}
	if !strings.Contains(err.Error(), "schema") && !strings.Contains(err.Error(), "read-back") {
		t.Fatalf("error not actionable about the schema: %v", err)
	}
	if s.wroteState {
		t.Fatalf("wroteState set despite a rejected draft")
	}
	slot := readBackSlot(t, s, projectstate.KindVolatilities)
	if slot.Status != projectstate.ReviewNone {
		t.Fatalf("slot was mutated despite rejection: status %d", slot.Status)
	}
}

// TestPutDraftModel_MethodRuleRejected proves the methodcheck CI gate is wired: a
// codec-valid but Method-invalid model (zero core use cases) is rejected with the rule id.
func TestPutDraftModel_MethodRuleRejected(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindCoreUseCases)
	// Decodes fine, but CUC-CARD requires 2–6 core use cases.
	err := s.putDraftModel([]byte(`{"decisions":[]}`))
	if err == nil {
		t.Fatalf("expected a methodcheck rejection for zero core use cases")
	}
	if !strings.Contains(err.Error(), "Method rule") {
		t.Fatalf("error does not mention Method rules: %v", err)
	}
	if s.wroteState {
		t.Fatalf("wroteState set despite a methodcheck rejection")
	}
}

// TestRespondToReviewComment sets a response + addressed status on a matched entry.
func TestRespondToReviewComment(t *testing.T) {
	p := minimalProject()
	p.Volatilities = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model:  &projectstate.Volatilities{Items: []projectstate.Volatility{{Name: "X", Rationale: "y", Axis: projectstate.AxisSameCustomerOverTime}}},
		ReviewThread: []projectstate.ReviewComment{
			{ID: "r1c1", Text: "clarify the axis", Status: projectstate.ReviewCommentOpen},
		},
	}
	s, _ := seedProject(t, p, jobModeDraft, projectstate.KindVolatilities)

	if err := s.respondToReviewComment("r1c1", "Reworded the rationale."); err != nil {
		t.Fatalf("respond: %v", err)
	}
	slot := readBackSlot(t, s, projectstate.KindVolatilities)
	if slot.ReviewThread[0].Response != "Reworded the rationale." {
		t.Fatalf("response not recorded: %q", slot.ReviewThread[0].Response)
	}
	if slot.ReviewThread[0].Status != projectstate.ReviewCommentAddressed {
		t.Fatalf("status not moved to addressed: %q", slot.ReviewThread[0].Status)
	}
	// Unknown id is an error.
	if err := s.respondToReviewComment("nope", "x"); err == nil {
		t.Fatalf("expected error for unknown comment id")
	}
}

// TestSetCritiqueVerdict covers approve/revise validation and the carrier write.
func TestSetCritiqueVerdict(t *testing.T) {
	p := minimalProject()
	p.Mission = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: &projectstate.MissionStatement{}}
	s, _ := seedProject(t, p, jobModeCritique, projectstate.KindMission)

	if err := s.setCritiqueVerdict("revise", "Tighten the vision sentence."); err != nil {
		t.Fatalf("revise verdict: %v", err)
	}
	slot := readBackSlot(t, s, projectstate.KindMission)
	if slot.CritiqueVerdict != projectstate.CritiqueVerdictRevise || slot.CritiqueNotes == "" {
		t.Fatalf("revise carrier not written: %+v", slot)
	}

	// approve clears notes.
	if err := s.setCritiqueVerdict("approve", "ignored"); err != nil {
		t.Fatalf("approve verdict: %v", err)
	}
	slot = readBackSlot(t, s, projectstate.KindMission)
	if slot.CritiqueVerdict != projectstate.CritiqueVerdictApprove || slot.CritiqueNotes != "" {
		t.Fatalf("approve carrier wrong: %+v", slot)
	}

	// Unknown verdict + revise-without-notes are rejected.
	if err := s.setCritiqueVerdict("maybe", ""); err == nil {
		t.Fatalf("expected error for unknown verdict")
	}
	if err := s.setCritiqueVerdict("revise", "  "); err == nil {
		t.Fatalf("expected error for revise with no notes")
	}
}

// TestPutDraftModel_NormalizesThroughCodec confirms the written model round-trips through
// the codec (the bytes on disk are exactly what the server would produce).
func TestPutDraftModel_NormalizesThroughCodec(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	if err := s.putDraftModel([]byte(`{"items":[{"name":"N","rationale":"r","axis":"allCustomersAtOneTime"}]}`)); err != nil {
		t.Fatalf("draft: %v", err)
	}
	// The persisted model must marshal with the canonical enum wire name.
	slot := readBackSlot(t, s, projectstate.KindVolatilities)
	b, _ := json.Marshal(slot.Model)
	if !strings.Contains(string(b), "allCustomersAtOneTime") {
		t.Fatalf("enum not normalized to its wire name: %s", b)
	}
}
