package review

import (
	"errors"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

func asEngineError(t *testing.T, err error) *fweng.Error {
	t.Helper()
	var e *fweng.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *fweng.Error, got %T: %v", err, err)
	}
	return e
}

func validChange() ReviewChange {
	return ReviewChange{ActivityID: "C-1", ComponentID: "handOffEngine", ContentAddress: "addr-1"}
}

// Every recognised kind yields a non-empty, deterministic reviewer set.
func Test_ProposeReviews_PerKind(t *testing.T) {
	cases := map[ReviewArtifactKind]struct {
		wantRole     string
		wantAmend    bool
		wantNonEmpty bool
	}{
		ReviewKindDetailedDesign: {roleArchitect, true, true},
		ReviewKindConstruction:   {roleSeniorReviewer, false, true},
		ReviewKindIntegration:    {roleSeniorReviewer, false, true},
		ReviewKindNoncoding:      {roleArchitect, false, true},
		ReviewKindUIDesign:       {roleUIDesigner, true, true},
		ReviewKindUICode:         {roleSeniorReviewer, false, true},
	}
	e := NewReviewEngine()
	for kind, want := range cases {
		set, err := e.ProposeReviews(fweng.Context{}, validChange(), "handOffEngine", kind, "", nil)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", kind, err)
		}
		if len(set.Reviewers) == 0 {
			t.Fatalf("%s: empty reviewer set", kind)
		}
		if got := set.Reviewers[0].Role; got != want.wantRole {
			t.Fatalf("%s: role = %q, want %q", kind, got, want.wantRole)
		}
		if got := set.Reviewers[0].MayAmend; got != want.wantAmend {
			t.Fatalf("%s: mayAmend = %v, want %v", kind, got, want.wantAmend)
		}
	}
}

// Identical inputs yield identical sets (determinism).
func Test_ProposeReviews_Deterministic(t *testing.T) {
	e := NewReviewEngine()
	a, err := e.ProposeReviews(fweng.Context{}, validChange(), "c", "Construction", "graph", []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.ProposeReviews(fweng.Context{}, validChange(), "c", "Construction", "graph", []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Reviewers) != len(b.Reviewers) || a.Reviewers[0] != b.Reviewers[0] {
		t.Fatalf("non-deterministic: %+v vs %+v", a, b)
	}
}

func Test_ProposeReviews_EmptyActivityID_ContractMisuse(t *testing.T) {
	e := NewReviewEngine()
	_, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ComponentID: "c"}, "c", "Construction", "", nil)
	if got := asEngineError(t, err).Kind; got != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// A component-scoped kind needs a component; the two system-level kinds do not — the
// system test plan and system testing have none, and used to get no reviewers at all.
func Test_ProposeReviews_ComponentIsRequiredOnlyWhereThereIsOne(t *testing.T) {
	e := NewReviewEngine()
	noComponent := ReviewChange{ActivityID: "N-STP"}
	for _, kind := range []ReviewArtifactKind{ReviewKindDetailedDesign, ReviewKindConstruction, ReviewKindUIDesign, ReviewKindUICode} {
		_, err := e.ProposeReviews(fweng.Context{}, noComponent, "", kind, "", nil)
		if got := asEngineError(t, err).Kind; got != fweng.ContractMisuse {
			t.Fatalf("%s with no component: want ContractMisuse, got %s", kind, got)
		}
	}
	for _, kind := range []ReviewArtifactKind{ReviewKindIntegration, ReviewKindNoncoding} {
		set, err := e.ProposeReviews(fweng.Context{}, noComponent, "", kind, "", nil)
		if err != nil || len(set.Reviewers) == 0 {
			t.Fatalf("%s with no component: want reviewers, got %+v, %v", kind, set, err)
		}
	}
}

// The Manager's old argument, pinned: a lifecycle phase's wire name is not a kind.
func Test_ProposeReviews_APhaseWireNameIsNotAKind(t *testing.T) {
	_, err := NewReviewEngine().ProposeReviews(fweng.Context{}, validChange(), "c", "detailed_design", "", nil)
	if got := asEngineError(t, err).Kind; got != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_ProposeReviews_UnknownKind_ContractMisuse(t *testing.T) {
	e := NewReviewEngine()
	_, err := e.ProposeReviews(fweng.Context{}, validChange(), "c", "NotAKind", "", nil)
	if got := asEngineError(t, err).Kind; got != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}
