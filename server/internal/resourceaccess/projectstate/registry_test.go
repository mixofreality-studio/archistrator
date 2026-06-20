package projectstate

import "testing"

// TestNewModelForKindCoversAllKinds is the build-time guard: every kind in
// AllArtifactKinds() must have a factory case in NewModelForKind, and the
// returned model must report the same kind (or, for the four Solution slots, the
// *Solution must have SlotKind set to the requested kind). A new ArtifactKind
// constant added to the iota without a corresponding factory case fails here
// rather than silently crashing at runtime in the codec.
func TestNewModelForKindCoversAllKinds(t *testing.T) {
	solutionKinds := map[ArtifactKind]bool{
		KindNormalSolution:       true,
		KindSubcriticalSolution:  true,
		KindCompressedSolution:   true,
		KindDecompressedSolution: true,
	}

	for _, k := range AllArtifactKinds() {
		model, ok := NewModelForKind(k)
		if !ok {
			t.Errorf("NewModelForKind(%s): returned ok=false; add a factory case", k)
			continue
		}
		if model == nil {
			t.Errorf("NewModelForKind(%s): returned nil model with ok=true", k)
			continue
		}
		if solutionKinds[k] {
			sol, isSol := model.(*Solution)
			if !isSol {
				t.Errorf("NewModelForKind(%s): expected *Solution, got %T", k, model)
				continue
			}
			if sol.SlotKind != k {
				t.Errorf("NewModelForKind(%s): Solution.SlotKind = %s, want %s", k, sol.SlotKind, k)
			}
		} else {
			if got := model.Kind(); got != k {
				t.Errorf("NewModelForKind(%s): model.Kind() = %s, want %s", k, got, k)
			}
		}
	}

	// An out-of-range kind must return (nil, false).
	if model, ok := NewModelForKind(ArtifactKind(9999)); ok || model != nil {
		t.Errorf("NewModelForKind(9999): expected (nil, false), got (%v, %v)", model, ok)
	}
}

// TestArtifactKindString checks the stable human-readable names emitted in
// error messages and arch-test output.
func TestArtifactKindString(t *testing.T) {
	if got := KindSystem.String(); got != "System" {
		t.Fatalf("KindSystem.String() = %q, want %q", got, "System")
	}
	if got := KindScrubbedRequirements.String(); got != "ScrubbedRequirements" {
		t.Fatalf("KindScrubbedRequirements.String() = %q, want %q", got, "ScrubbedRequirements")
	}
	unknown := ArtifactKind(999)
	if got := unknown.String(); got != "ArtifactKind(999)" {
		t.Fatalf("unknown ArtifactKind.String() = %q, want %q", got, "ArtifactKind(999)")
	}
}

// TestArtifactKindIsPhase1 covers the Phase-1 partition used by the Manager gate.
func TestArtifactKindIsPhase1(t *testing.T) {
	if !KindSystem.IsPhase1() {
		t.Fatal("KindSystem is Phase 1")
	}
	if KindNetwork.IsPhase1() {
		t.Fatal("KindNetwork is Phase 2")
	}
	if len(Phase1RequiredKinds()) == 0 {
		t.Fatal("Phase1RequiredKinds must be non-empty")
	}
}
