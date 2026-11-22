package main

import (
	"encoding/json"
	"fmt"
	"reflect"
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

// TestRespondToReviewComment appends an agent utterance and clears a reopen.
func TestRespondToReviewComment(t *testing.T) {
	s := newTestSession(t)
	seedThread(t, s, projectstate.ReviewComment{ID: "r1c1", Status: projectstate.ReviewCommentOpen, Reopened: true})

	if err := s.respondToReviewComment("r1c1", "Reworded the rationale."); err != nil {
		t.Fatalf("respond: %v", err)
	}

	got := readThread(t, s)
	if len(got[0].Replies) != 1 {
		t.Fatalf("want 1 reply, got %d", len(got[0].Replies))
	}
	if got[0].Replies[0].Text != "Reworded the rationale." {
		t.Fatalf("reply text = %q", got[0].Replies[0].Text)
	}
	if got[0].Replies[0].AuthorRole == "" {
		t.Fatal("reply must carry an author role")
	}
	if got[0].Reopened {
		t.Fatal("an agent reply must clear the sticky Reopened bit")
	}

	// A second response appends rather than overwriting — this is a thread.
	if err := s.respondToReviewComment("r1c1", "Also split objective 3."); err != nil {
		t.Fatalf("second respond: %v", err)
	}
	if got := readThread(t, s); len(got[0].Replies) != 2 {
		t.Fatalf("want 2 replies after a second response, got %d", len(got[0].Replies))
	}

	if err := s.respondToReviewComment("nope", "x"); err == nil {
		t.Fatal("expected an error for an unknown comment id")
	}
}

// TestSetCritiqueVerdict covers approve/revise validation, the carrier write and the
// shared review-note budget.
func TestSetCritiqueVerdict(t *testing.T) {
	atLimit := strings.Repeat("é", projectstate.MaxReviewNoteRunes)
	overLimit := atLimit + "é"
	cases := []struct {
		name         string
		verdict      string
		notes        string
		wantErr      []string // substrings; nil = success
		wantVerdict  string
		wantNotes    string
		wantWrote    bool
		priorRevised bool // seed a revise first, so approve-clears is observable
	}{
		{name: "revise stores notes", verdict: "revise", notes: "Tighten the vision sentence.",
			wantVerdict: projectstate.CritiqueVerdictRevise, wantNotes: "Tighten the vision sentence.", wantWrote: true},
		{name: "approve clears long notes", verdict: "approve", notes: overLimit, priorRevised: true,
			wantVerdict: projectstate.CritiqueVerdictApprove, wantNotes: "", wantWrote: true},
		{name: "unknown verdict", verdict: "maybe", wantErr: []string{"approve"}},
		{name: "revise without notes", verdict: "revise", notes: "  ", wantErr: []string{"requires notes"}},
		{name: "exactly at limit accepted whole", verdict: "revise", notes: atLimit,
			wantVerdict: projectstate.CritiqueVerdictRevise, wantNotes: atLimit, wantWrote: true},
		{name: "one multi-byte rune over refused", verdict: "revise", notes: overLimit,
			wantErr: []string{"4000", "4001"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := minimalProject()
			p.Mission = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: &projectstate.MissionStatement{}}
			s, _ := seedProject(t, p, jobModeCritique, projectstate.KindMission)
			if tc.priorRevised {
				if err := s.setCritiqueVerdict("revise", "earlier note"); err != nil {
					t.Fatalf("seed revise: %v", err)
				}
				s.wroteState = false
			}
			err := s.setCritiqueVerdict(tc.verdict, tc.notes)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error containing %v", tc.wantErr)
				}
				for _, sub := range tc.wantErr {
					if !strings.Contains(err.Error(), sub) {
						t.Fatalf("error %q lacks %q", err, sub)
					}
				}
				if s.wroteState {
					t.Fatal("a refused verdict must not write")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if s.wroteState != tc.wantWrote {
				t.Fatalf("wroteState = %v, want %v", s.wroteState, tc.wantWrote)
			}
			slot := readBackSlot(t, s, projectstate.KindMission)
			if slot.CritiqueVerdict != tc.wantVerdict || slot.CritiqueNotes != tc.wantNotes {
				t.Fatalf("carrier wrong: verdict=%q notes len=%d", slot.CritiqueVerdict, len(slot.CritiqueNotes))
			}
		})
	}
}

// TestCritiqueNotesBudgetTagMatchesConstant pins the literal budget in the tool's
// jsonschema tag to projectstate.MaxReviewNoteRunes so the two cannot drift.
func TestCritiqueNotesBudgetTagMatchesConstant(t *testing.T) {
	f, ok := reflect.TypeOf(setCritiqueVerdictInput{}).FieldByName("Notes")
	if !ok {
		t.Fatal("Notes field missing")
	}
	want := fmt.Sprintf("at most %d characters", projectstate.MaxReviewNoteRunes)
	if tag := f.Tag.Get("jsonschema"); !strings.Contains(tag, want) {
		t.Fatalf("Notes jsonschema tag %q must contain %q", tag, want)
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
