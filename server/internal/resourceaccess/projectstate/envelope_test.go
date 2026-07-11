package projectstate

import (
	"encoding/json"
	"strings"
	"testing"
)

// envelope_test.go ports the codec-mechanism tests down from the two Managers that
// used to duplicate this wire discipline (projectdesign/codec.go, systemdesign/codec.go)
// now that ModelEnvelope/ProjectEnvelope/EncodeModel/EncodeProject/Decode live here
// (envelope.go). The Manager-specific policy tests (whether a given Manager opts INTO
// carrying the Research corpus; the interaction with a Manager's own local slotFor/
// ArtifactKind) stay in projectdesign/systemdesign — see
// Test_encodeProject_DropsResearchCorpus (projectdesign),
// Test_encodeProject_SlimsResearchContentAcrossActivityBoundary and
// Test_projectEnvelope_PreservesReviewThread (both Managers) — this file covers the
// shared mechanism itself.

func TestModelEnvelope_RoundTrip_NilModel(t *testing.T) {
	env, err := EncodeModel(nil)
	if err != nil {
		t.Fatalf("EncodeModel(nil): %v", err)
	}
	model, err := env.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if model != nil {
		t.Fatalf("Decode of a nil-encoded envelope must yield a nil model, got %T", model)
	}
}

func TestModelEnvelope_RoundTrip_ConcreteModel(t *testing.T) {
	mission := &MissionStatement{Vision: "ENVELOPE-SENTINEL vision"}
	env, err := EncodeModel(mission)
	if err != nil {
		t.Fatalf("EncodeModel: %v", err)
	}
	if env.Kind != KindMission {
		t.Fatalf("Kind = %s, want %s", env.Kind, KindMission)
	}
	if !strings.Contains(string(env.Model), "ENVELOPE-SENTINEL") {
		t.Fatalf("encoded model JSON must carry the field value, got %s", env.Model)
	}
	decoded, err := env.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got, ok := decoded.(*MissionStatement)
	if !ok {
		t.Fatalf("Decode: got %T, want *MissionStatement", decoded)
	}
	if got.Vision != mission.Vision {
		t.Fatalf("Vision = %q, want %q", got.Vision, mission.Vision)
	}
}

func TestModelEnvelope_Decode_UnknownKindErrors(t *testing.T) {
	env := ModelEnvelope{Kind: ArtifactKind(9999), Model: json.RawMessage(`{}`)}
	if _, err := env.Decode(); err == nil {
		t.Fatal("Decode with an out-of-range Kind must error, got nil")
	}
}

func TestModelEnvelope_Decode_SolutionSlotKindReapplied(t *testing.T) {
	sol := &Solution{SlotKind: KindNormalSolution}
	env, err := EncodeModel(sol)
	if err != nil {
		t.Fatalf("EncodeModel: %v", err)
	}
	// Force the envelope's discriminator to a DIFFERENT Solution slot to prove Decode
	// re-applies the envelope's own Kind rather than trusting whatever SlotKind the
	// JSON happened to carry.
	env.Kind = KindSubcriticalSolution
	decoded, err := env.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got, ok := decoded.(*Solution)
	if !ok {
		t.Fatalf("Decode: got %T, want *Solution", decoded)
	}
	if got.SlotKind != KindSubcriticalSolution {
		t.Fatalf("SlotKind = %s, want %s (the envelope's own Kind is authoritative)", got.SlotKind, KindSubcriticalSolution)
	}
}

func TestEncodeProject_SkipsUnpopulatedSlots(t *testing.T) {
	p := Project{ID: "proj-1", Version: 3, Phase: 1}
	env, err := EncodeProject(p)
	if err != nil {
		t.Fatalf("EncodeProject: %v", err)
	}
	if len(env.Slots) != 0 {
		t.Fatalf("an all-empty Project must encode zero slots, got %d", len(env.Slots))
	}
	if env.ID != p.ID || env.Version != p.Version || env.Phase != p.Phase {
		t.Fatalf("identity fields must survive encoding: got %+v", env)
	}
}

func TestProjectEnvelope_RoundTrip_PreservesSlotFields(t *testing.T) {
	p := Project{
		ID:      "proj-1",
		Version: 5,
		Phase:   1,
		Mission: ArtifactSlot{
			Status:          ReviewAwaitingReview,
			Model:           &MissionStatement{Vision: "round-trip vision"},
			Notes:           "reviewer notes",
			CritiqueVerdict: CritiqueVerdictRevise,
			CritiqueNotes:   "tighten the vision sentence",
			ReviewThread: []ReviewComment{
				{ID: "r0c1", Text: "split this", AuthorRole: "architect", Round: 0, Status: ReviewCommentOpen},
			},
		},
	}

	env, err := EncodeProject(p)
	if err != nil {
		t.Fatalf("EncodeProject: %v", err)
	}
	se, ok := env.Slots[KindMission]
	if !ok {
		t.Fatal("Mission slot must be present in the encoded envelope")
	}
	if se.Status != ReviewAwaitingReview || se.Notes != "reviewer notes" {
		t.Fatalf("Status/Notes must survive encoding, got %+v", se)
	}
	if se.CritiqueVerdict != CritiqueVerdictRevise || se.CritiqueNotes != "tighten the vision sentence" {
		t.Fatalf("CritiqueVerdict/CritiqueNotes must survive encoding, got %+v", se)
	}
	if len(se.ReviewThread) != 1 || se.ReviewThread[0].ID != "r0c1" {
		t.Fatalf("ReviewThread must survive encoding, got %+v", se.ReviewThread)
	}

	back, err := env.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if back.Mission.Status != ReviewAwaitingReview || back.Mission.Notes != "reviewer notes" {
		t.Fatalf("Status/Notes must survive the round trip, got %+v", back.Mission)
	}
	if back.Mission.CritiqueVerdict != CritiqueVerdictRevise || back.Mission.CritiqueNotes != "tighten the vision sentence" {
		t.Fatalf("CritiqueVerdict/CritiqueNotes must survive the round trip, got %+v", back.Mission)
	}
	if len(back.Mission.ReviewThread) != 1 || back.Mission.ReviewThread[0].Text != "split this" {
		t.Fatalf("ReviewThread must survive the round trip, got %+v", back.Mission.ReviewThread)
	}
	mission, ok := back.Mission.Model.(*MissionStatement)
	if !ok || mission.Vision != "round-trip vision" {
		t.Fatalf("Model must survive the round trip, got %+v", back.Mission.Model)
	}
}

// TestProjectEnvelope_ResearchIsNilByDefault proves the F16 payload-slimming
// contract at the shared-codec level: EncodeProject never populates Research, so a
// caller (projectdesign) that never opts in gets a wire payload with NO "research"
// key at all — a plain (non-pointer) struct field's `omitempty` would NOT suppress
// the key, which is exactly why the field is a pointer.
func TestProjectEnvelope_ResearchIsNilByDefault(t *testing.T) {
	p := Project{
		ID: "proj-1",
		Research: ResearchCorpus{Sources: []ResearchSourceRef{
			{Title: "RESEARCH-SENTINEL", Path: ".aiarch/state/research/00.txt", ContentBytes: 660_000},
		}},
	}
	env, err := EncodeProject(p)
	if err != nil {
		t.Fatalf("EncodeProject: %v", err)
	}
	if env.Research != nil {
		t.Fatalf("EncodeProject must leave Research nil unless the caller opts in, got %+v", env.Research)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if strings.Contains(string(raw), "research") {
		t.Fatalf("a nil Research pointer must not appear in the wire payload at all, got: %s", raw)
	}
	back, err := env.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !back.Research.IsZero() {
		t.Fatal("Research must not survive the round trip when the envelope never carried it")
	}
}

// TestProjectEnvelope_ResearchOptIn proves the opt-in path a Manager (systemdesign)
// uses: assigning env.Research after EncodeProject carries the corpus through the
// wire payload and back.
func TestProjectEnvelope_ResearchOptIn(t *testing.T) {
	p := Project{
		ID: "proj-1",
		Research: ResearchCorpus{Sources: []ResearchSourceRef{
			{Title: "The Founder Brief", Path: ".aiarch/state/research/00.txt", ContentBytes: 660_000},
		}},
	}
	env, err := EncodeProject(p)
	if err != nil {
		t.Fatalf("EncodeProject: %v", err)
	}
	env.Research = &p.Research

	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if !strings.Contains(string(raw), "The Founder Brief") {
		t.Fatalf("an opted-in Research must appear in the wire payload, got: %s", raw)
	}

	back, err := env.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if back.Research.IsZero() {
		t.Fatal("Research must survive the round trip when the envelope opted in")
	}
	if len(back.Research.Sources) != 1 || back.Research.Sources[0].Title != "The Founder Brief" {
		t.Fatalf("Research sources must survive the round trip, got %+v", back.Research.Sources)
	}
}
