package estimation

import (
	"errors"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

func planFor(t *testing.T, deltas ActivityListDeltas) DerivedPlan {
	t.Helper()
	plan, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), deltas)
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	return plan
}

func activityNamed(plan DerivedPlan, name string) (DerivedActivity, bool) {
	for _, a := range plan.Activities {
		if a.Name == name {
			return a, true
		}
	}
	return DerivedActivity{}, false
}

func TestDerivePlanWithNoDeltasReturnsTheBaseline(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{})
	if len(plan.Activities) == 0 {
		t.Fatal("DerivePlan returned no activities for a populated System")
	}
	if _, ok := activityNamed(plan, "C-order-manager"); !ok {
		t.Error("baseline missing C-order-manager")
	}
	if len(plan.Dependencies) == 0 {
		t.Error("baseline produced no dependency edges")
	}
}

func TestOverrideReplacesEffortAndRisk(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{Overrides: []ActivityOverride{{
		Activity: "C-order-manager", EffortDays: ptrFloat(35), RiskBucket: ptrInt(8),
		Justification: "orchestrates five downstream contracts; the band midpoint is optimistic",
	}}})
	a, ok := activityNamed(plan, "C-order-manager")
	if !ok {
		t.Fatal("C-order-manager missing after override")
	}
	if a.EffortDays != 35 || a.RiskBucket != 8 {
		t.Errorf("override not applied: %+v", a)
	}
	if !a.Derived {
		t.Error("an overridden activity is still a DERIVED activity")
	}
}

// An override naming no derived activity is the zombie failure mode: the live committed
// list carries C-HE, C-WIA and R-WIT against components that do not exist. Loud failure.
func TestOverrideOfUnknownActivityIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Overrides: []ActivityOverride{{Activity: "C-hand-off-engine", Justification: "x"}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an override of an unknown activity, got %v", err)
	}
}

func TestOverrideWithoutJustificationIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Overrides: []ActivityOverride{{Activity: "C-order-manager", EffortDays: ptrFloat(35)}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an unjustified override, got %v", err)
	}
}

func TestOverrideBreakingTheQuantumIsRejected(t *testing.T) {
	for _, bad := range []float64{7, 11, 40} {
		_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
			Overrides: []ActivityOverride{{
				Activity: "C-order-manager", EffortDays: ptrFloat(bad), Justification: "j",
			}},
		})
		if err == nil {
			t.Errorf("override effort %v should be rejected (quantum 5, cap 35)", bad)
		}
	}
}

func TestAdditiveActivityIsAppended(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{Additive: []AdditiveActivity{{
		Name: "N-SCHEMA", Title: "Schema design for owned stores",
		EffortDays: 10, RiskBucket: 2, WorkerClass: "system-architect",
		DependsOn:     []string{"C-order-access"},
		Justification: "owned stores carry schema work no component activity covers",
	}}})
	a, ok := activityNamed(plan, "N-SCHEMA")
	if !ok {
		t.Fatal("additive activity not appended")
	}
	if a.Derived {
		t.Error("an additive activity must NOT be flagged Derived")
	}
	var found bool
	for _, d := range plan.Dependencies {
		if d.Activity == "N-SCHEMA" && len(d.DependsOn) == 1 && d.DependsOn[0] == "C-order-access" {
			found = true
		}
	}
	if !found {
		t.Error("the additive activity's own incident edge was not emitted")
	}
}

// C2: an additive carrying a componentId is a covert per-component exclusion or
// replacement channel. It is exactly how C-HE and C-WIA would come back.
func TestAdditiveWithComponentIDIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "N-X", Title: "x", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "junior-developer", Justification: "j",
			ComponentID: ptrString("order-manager"),
		}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an additive carrying a componentId, got %v", err)
	}
}

// An additive may not shadow a derived activity — that is an exclusion in disguise.
func TestAdditiveCollidingWithADerivedNameIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "C-order-manager", Title: "shadow", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "junior-developer", Justification: "j",
		}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an additive shadowing a derived activity, got %v", err)
	}
}

// C3: an additive declares its OWN incident edges only. Pointing at a nonexistent
// activity would inject a dangling node into the CPM solve.
func TestAdditiveEdgeToUnknownActivityIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "N-X", Title: "x", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "junior-developer", Justification: "j",
			DependsOn: []string{"C-does-not-exist"},
		}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an additive edge to an unknown activity, got %v", err)
	}
}

func TestAdditiveWithOffRosterWorkerClassIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "N-X", Title: "x", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "Platform-DevOps-Engineer", Justification: "j",
		}},
	})
	if err == nil {
		t.Fatal("an off-roster worker class must be rejected; it would silently ride default token rates")
	}
}

// C4: M5 "v1 Production Live" depends entirely on additive noncoding, so it cannot
// derive — it is authored as an additive milestone.
func TestAdditiveMilestoneIsAppended(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "N-DEP", Title: "Production deployment", EffortDays: 10, RiskBucket: 3,
			WorkerClass: "senior-developer", Justification: "deployment is componentless project work",
		}},
		AdditiveMilestones: []AdditiveMilestone{{
			Id: "M5", DependsOn: []string{"N-DEP", "N-IT"},
			Justification: "v1 production live gates on deployment plus the terminal system-testing gate",
		}},
	})
	var found bool
	for _, m := range plan.Milestones {
		if m.Id == "M5" {
			found = true
		}
	}
	if !found {
		t.Error("additive milestone M5 was not appended")
	}
}

func TestAdditiveMilestoneShadowingADerivedOneIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		AdditiveMilestones: []AdditiveMilestone{{Id: "M0", Justification: "j"}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an additive milestone shadowing a derived one, got %v", err)
	}
}

// An empty System is a normal DOMAIN result (a project read before its architecture is
// committed), never an error.
func TestDerivePlanOnEmptySystemIsAnEmptyPlanNotAnError(t *testing.T) {
	plan, err := NewEstimationEngine().DerivePlan(fweng.Context{}, SystemView{}, ActivityListDeltas{})
	if err != nil {
		t.Fatalf("empty System must be a domain result, got error %v", err)
	}
	if len(plan.Activities) != 0 {
		t.Errorf("empty System produced %d activities", len(plan.Activities))
	}
}

// The deferred two-pass validation exists so two additives may legally depend on each
// other: appendAdditives indexes EVERY additive before validateAdditiveEdges checks any
// edge. Inlining the edge check into the append loop would reject this valid input — and
// that regression is invisible to every other test in this file, because their edge
// targets are all derived baseline activities that exist before additive processing.
// The reference is FORWARD on purpose: N-FIRST depends on N-SECOND, which is appended
// after it.
func TestAdditivesMayDependOnEachOther(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{Additive: []AdditiveActivity{
		{
			Name: "N-FIRST", Title: "first", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "senior-developer", DependsOn: []string{"N-SECOND"},
			Justification: "depends on a sibling additive declared after it",
		},
		{
			Name: "N-SECOND", Title: "second", EffortDays: 5, RiskBucket: 2,
			WorkerClass:   "senior-developer",
			Justification: "the forward-referenced sibling",
		},
	}})
	if _, ok := activityNamed(plan, "N-FIRST"); !ok {
		t.Error("N-FIRST missing from the plan")
	}
	if _, ok := activityNamed(plan, "N-SECOND"); !ok {
		t.Error("N-SECOND missing from the plan")
	}
	var found bool
	for _, d := range plan.Dependencies {
		if d.Activity == "N-FIRST" && len(d.DependsOn) == 1 && d.DependsOn[0] == "N-SECOND" {
			found = true
		}
	}
	if !found {
		t.Error("N-FIRST -> N-SECOND edge missing; the two-pass ordering has regressed to single-pass")
	}
}

// mustReject asserts the deltas are refused with a ContractMisuse. Every caller varies
// exactly ONE field away from an otherwise-legal delta, so the named guard is the only
// one eligible — a rejection test that passed because an EARLIER guard fired would not
// be testing what its name claims.
func mustReject(t *testing.T, what string, deltas ActivityListDeltas) {
	t.Helper()
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), deltas)
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("%s: want ContractMisuse, got %v", what, err)
	}
}

// legalAdditive passes every guard, so a caller can break exactly one field and know
// which guard fired.
func legalAdditive() AdditiveActivity {
	return AdditiveActivity{
		Name: "N-X", Title: "x", EffortDays: 5, RiskBucket: 2,
		WorkerClass: "senior-developer", Justification: "j",
	}
}

func TestOverrideWithNonFibonacciRiskIsRejected(t *testing.T) {
	mustReject(t, "override with risk bucket 4", ActivityListDeltas{Overrides: []ActivityOverride{{
		Activity: "C-order-manager", RiskBucket: ptrInt(4), Justification: "j",
	}}})
}

func TestAdditiveWithoutJustificationIsRejected(t *testing.T) {
	a := legalAdditive()
	a.Justification = ""
	mustReject(t, "additive without justification", ActivityListDeltas{Additive: []AdditiveActivity{a}})
}

func TestAdditiveWithIllegalEffortIsRejected(t *testing.T) {
	for _, bad := range []float64{7, 40} {
		a := legalAdditive()
		a.EffortDays = bad
		mustReject(t, "additive with off-quantum or oversized effort",
			ActivityListDeltas{Additive: []AdditiveActivity{a}})
	}
}

func TestAdditiveWithNonFibonacciRiskIsRejected(t *testing.T) {
	a := legalAdditive()
	a.RiskBucket = 4
	mustReject(t, "additive with risk bucket 4", ActivityListDeltas{Additive: []AdditiveActivity{a}})
}

func TestAdditiveMilestoneWithoutJustificationIsRejected(t *testing.T) {
	mustReject(t, "additive milestone without justification", ActivityListDeltas{
		AdditiveMilestones: []AdditiveMilestone{{Id: "M9"}},
	})
}

func TestAdditiveMilestoneWithDanglingDependencyIsRejected(t *testing.T) {
	mustReject(t, "additive milestone with dangling dependsOn", ActivityListDeltas{
		AdditiveMilestones: []AdditiveMilestone{{
			Id: "M9", DependsOn: []string{"N-DOES-NOT-EXIST"}, Justification: "j",
		}},
	})
}

func ptrFloat(f float64) *float64 { return &f }
func ptrInt(i int64) *int64       { return &i }
func ptrString(s string) *string  { return &s }
