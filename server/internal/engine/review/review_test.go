package review

import (
	"errors"
	"testing"

	fweng "github.com/davidmarne/archistrator-platform/framework-go/engine"
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
	cases := map[string]struct {
		wantRole     string
		wantAmend    bool
		wantNonEmpty bool
	}{
		"DetailedDesign": {RoleArchitect, true, true},
		"Construction":   {RoleSeniorReviewer, false, true},
		"Integration":    {RoleSeniorReviewer, false, true},
		"Noncoding":      {RoleArchitect, false, true},
		"UIDesign":       {RoleUIDesigner, true, true},
		"UICode":         {RoleSeniorReviewer, false, true},
	}
	e := New()
	for kind, want := range cases {
		set, err := e.ProposeReviews(validChange(), "handOffEngine", kind, "", nil)
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
	e := New()
	a, err := e.ProposeReviews(validChange(), "c", "Construction", "graph", []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.ProposeReviews(validChange(), "c", "Construction", "graph", []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Reviewers) != len(b.Reviewers) || a.Reviewers[0] != b.Reviewers[0] {
		t.Fatalf("non-deterministic: %+v vs %+v", a, b)
	}
}

func Test_ProposeReviews_EmptyActivityID_ContractMisuse(t *testing.T) {
	e := New()
	_, err := e.ProposeReviews(ReviewChange{ComponentID: "c"}, "c", "Construction", "", nil)
	if got := asEngineError(t, err).Kind; got != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_ProposeReviews_EmptyComponent_ContractMisuse(t *testing.T) {
	e := New()
	_, err := e.ProposeReviews(ReviewChange{ActivityID: "C-1"}, "", "Construction", "", nil)
	if got := asEngineError(t, err).Kind; got != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_ProposeReviews_UnknownKind_ContractMisuse(t *testing.T) {
	e := New()
	_, err := e.ProposeReviews(validChange(), "c", "NotAKind", "", nil)
	if got := asEngineError(t, err).Kind; got != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}
