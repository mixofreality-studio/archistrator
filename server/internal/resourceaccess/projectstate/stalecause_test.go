package projectstate

import (
	"testing"
)

// stalecause_test.go — the ADDITIVE stale-cause recording on the F38 staleness rail.
// commitTransition, when an upstream slot re-commits, must flag every already-committed
// downstream slot StaleBasis AND record WHY (the upstream kind + its new revision).

// committedSlot is a tiny helper: an already-committed slot at a given revision.
func committedSlot(m ArtifactModel, rev int64) ArtifactSlot {
	return ArtifactSlot{Status: ReviewCommitted, Model: m, Revisions: rev}
}

func TestCommitTransition_RecordsStaleCauseOnDownstream(t *testing.T) {
	// Volatilities (upstream) and CoreUseCases (downstream) both already committed.
	p := &Project{}
	p.Volatilities = committedSlot(&Volatilities{Items: []Volatility{{Name: "V", Axis: AxisSameCustomerOverTime}}}, 1)
	p.CoreUseCases = committedSlot(&CoreUseCases{}, 1)

	// Re-commit (amend) Volatilities.
	if err := commitTransition(KindVolatilities)(p); err != nil {
		t.Fatalf("commitTransition: %v", err)
	}

	if !p.CoreUseCases.StaleBasis {
		t.Fatal("downstream CoreUseCases must be flagged stale after an upstream amendment")
	}
	c := p.CoreUseCases.StaleBasisCause
	if c == nil {
		t.Fatal("downstream slot must carry a stale cause")
	}
	if c.UpstreamKind != KindVolatilities.WireName() {
		t.Fatalf("cause upstream kind = %q, want %q", c.UpstreamKind, KindVolatilities.WireName())
	}
	if c.UpstreamRevision != 2 {
		t.Fatalf("cause upstream revision = %d, want 2 (revision after the amendment)", c.UpstreamRevision)
	}
	// The upstream slot that re-committed clears its own staleness/cause.
	if p.Volatilities.StaleBasis || p.Volatilities.StaleBasisCause != nil {
		t.Fatal("the re-committed upstream slot must clear its own staleness and cause")
	}
}

func TestCommitTransition_ClearsStaleCauseOnReconcile(t *testing.T) {
	p := &Project{}
	p.CoreUseCases = committedSlot(&CoreUseCases{}, 1)
	p.CoreUseCases.StaleBasis = true
	p.CoreUseCases.StaleBasisCause = &StaleCause{UpstreamKind: "volatilities", UpstreamRevision: 2}

	// Re-committing CoreUseCases itself IS the reconcile.
	if err := commitTransition(KindCoreUseCases)(p); err != nil {
		t.Fatalf("commitTransition: %v", err)
	}
	if p.CoreUseCases.StaleBasis || p.CoreUseCases.StaleBasisCause != nil {
		t.Fatal("re-committing a slot must clear its own StaleBasis AND StaleBasisCause")
	}
}

func TestStaleCause_RoundTripsThroughCodec(t *testing.T) {
	p := Project{ID: "p"}
	p.CoreUseCases = committedSlot(&CoreUseCases{Decisions: []UseCaseDecision{{
		UseCase: UseCase{
			Name:           "UC",
			Trigger:        TriggerClientAction,
			Classification: ClassCore,
			Activity: &ActivityDiagram{
				Nodes: []ActivityNode{{ID: "s", Kind: NodeStart}, {ID: "a", Kind: NodeAction, Label: "do"}},
				Edges: []ActivityEdge{{From: "s", To: "a", Kind: EdgeControlFlow}},
			},
		},
	}}}, 1)
	p.CoreUseCases.StaleBasis = true
	p.CoreUseCases.StaleBasisCause = &StaleCause{UpstreamKind: "volatilities", UpstreamRevision: 3}

	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, ok, err := DecodeProjectJSON(raw, "p")
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	c := got.CoreUseCases.StaleBasisCause
	if c == nil {
		t.Fatal("stale cause must survive the encode → decode round-trip")
	}
	if c.UpstreamKind != "volatilities" || c.UpstreamRevision != 3 {
		t.Fatalf("stale cause round-trip mismatch: %+v", *c)
	}
}
