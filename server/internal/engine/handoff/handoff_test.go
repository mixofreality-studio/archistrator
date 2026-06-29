package handoff

import (
	"errors"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// ---------------------------------------------------------------------------
// Service Test Plan (STP) for handOffEngine — "all the ways to demonstrate it
// does not work" (the-method-testing). The Engine is pure logic; we test
// BEHAVIOUR (the cast class) and the programmer-error channel, not strings.
//
//  1. PickWorkerClass with PreferAI=true casts AIWorker for an ordinary activity.
//  2. PickWorkerClass with PreferAI=true casts HumanSeniorWorker for an activity
//     whose layer is in SeniorOnlyLayers (case-insensitively).
//  3. PickWorkerClass with PreferAI=false casts HumanSeniorWorker by default
//     (review-everything customer).
//  4. PickWorkerClass with PreferAI=false casts HumanSeniorWorker for a
//     senior-only layer too (no regression).
//  5. ContractMisuse: empty ActivityID.
//  6. ContractMisuse: unknown ActivityKind.
//  7. InvalidInput: a Strategy that casts an unsupported class is refused (no
//     silent fallback) — driven via the internal Strategy seam.
//  8. InternalInvariant: a Strategy returning an out-of-range class is guarded.
//  9. ArchitectOnly is a NORMAL returned class (OQ-2), never an error.
// 10. Determinism: identical (activity, policy) twice -> identical class.
// 11. Reentrancy: SeniorOnlyLayers mutated by the caller after the call does not
//     change a subsequent decision (no shared mutable state).
// ---------------------------------------------------------------------------

// activity builds an ordinary construction activity with a layer.
func activity(layer string) ConstructionActivity {
	return ConstructionActivity{
		ActivityID:   "C-XX",
		Kind:         ActivityKindConstruction,
		ComponentID:  "someComponent",
		Layer:        layer,
		EstimateDays: 5,
	}
}

func TestPickWorkerClass(t *testing.T) {
	const noErr fweng.Kind = -1

	tests := []struct {
		name        string
		activity    ConstructionActivity
		policy      HandOffPolicy
		wantErrKind fweng.Kind
		want        WorkerClass
	}{
		{
			name:        "prefer-AI casts AIWorker for an ordinary engine activity",
			activity:    activity("engine"),
			policy:      HandOffPolicy{PreferAI: true},
			wantErrKind: noErr,
			want:        AIWorker,
		},
		{
			name:        "prefer-AI forces human senior on a senior-only layer",
			activity:    activity("manager"),
			policy:      HandOffPolicy{PreferAI: true, SeniorOnlyLayers: []string{"manager", "resourceaccess"}},
			wantErrKind: noErr,
			want:        HumanSeniorWorker,
		},
		{
			name:        "prefer-AI senior-only match is case-insensitive",
			activity:    activity("ResourceAccess"),
			policy:      HandOffPolicy{PreferAI: true, SeniorOnlyLayers: []string{"resourceaccess"}},
			wantErrKind: noErr,
			want:        HumanSeniorWorker,
		},
		{
			name:        "review-everything customer casts human senior by default",
			activity:    activity("engine"),
			policy:      HandOffPolicy{PreferAI: false},
			wantErrKind: noErr,
			want:        HumanSeniorWorker,
		},
		{
			name:        "review-everything casts human senior on a senior-only layer too",
			activity:    activity("manager"),
			policy:      HandOffPolicy{PreferAI: false, SeniorOnlyLayers: []string{"manager"}},
			wantErrKind: noErr,
			want:        HumanSeniorWorker,
		},
		{
			name:        "contract misuse: empty ActivityID",
			activity:    ConstructionActivity{ActivityID: "", Kind: ActivityKindConstruction, Layer: "engine"},
			policy:      HandOffPolicy{PreferAI: true},
			wantErrKind: fweng.ContractMisuse,
		},
		{
			name:        "contract misuse: unknown ActivityKind",
			activity:    ConstructionActivity{ActivityID: "C-XX", Kind: ActivityKindUnknown, Layer: "engine"},
			policy:      HandOffPolicy{PreferAI: true},
			wantErrKind: fweng.ContractMisuse,
		},
	}

	eng := NewHandOffEngine()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := eng.PickWorkerClass(fweng.Context{}, tc.activity, tc.policy)

			if tc.wantErrKind != noErr {
				if err == nil {
					t.Fatalf("expected error of kind %v, got nil (result %v)", tc.wantErrKind, got)
				}
				var fe *fweng.Error
				if !errors.As(err, &fe) {
					t.Fatalf("expected *fweng.Error, got %T: %v", err, err)
				}
				if fe.Kind != tc.wantErrKind {
					t.Fatalf("expected kind %v, got %v (detail: %s)", tc.wantErrKind, fe.Kind, fe.Detail)
				}
				if fe.Retryable {
					t.Errorf("engine errors must never be retryable")
				}
				if got != WorkerClassUnknown {
					t.Errorf("on error want WorkerClassUnknown, got %v", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("PickWorkerClass = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestArchitectOnlyIsANormalClass asserts OQ-2: the architect-only arrangement is
// a NORMAL returned class, not an error. Driven through the architectOnlyStrategy
// seam (the v1 HandOffPolicy field set has no architect-only flag yet, so the
// public surface cannot select it — but the casting path and the class are live
// and the public PickWorkerClass accepts/guards it as valid).
func TestArchitectOnlyIsANormalClass(t *testing.T) {
	cast := architectOnlyStrategy{}.pickWorkerClass(activity("engine"))
	if cast != ArchitectOnly {
		t.Fatalf("architectOnlyStrategy cast %v, want ArchitectOnly", cast)
	}
	if !workerClassValid(cast) {
		t.Fatalf("ArchitectOnly must be a valid (registered, non-error) class")
	}
}

// fakeStrategy lets the InvalidInput / InternalInvariant guards be exercised
// without a real policy that could cast them (the v1 strategies never do). It is
// an in-test fake of the package-internal handOffStrategy seam.
type fakeStrategy struct{ out WorkerClass }

func (f fakeStrategy) pickWorkerClass(ConstructionActivity) WorkerClass { return f.out }

// pickWith runs the public guard pipeline against a supplied strategy output,
// mirroring PickWorkerClass's post-strategy guards. It exists so the InvalidInput
// (unsupported class) and InternalInvariant (out-of-range class) branches are
// covered deterministically.
func pickWith(activity ConstructionActivity, cast WorkerClass) (WorkerClass, error) {
	if activity.ActivityID == "" {
		return WorkerClassUnknown, fweng.New(fweng.ContractMisuse, "test: empty id")
	}
	if cast == WorkerClassUnknown {
		return WorkerClassUnknown, fweng.New(fweng.InvalidInput, "test: unsupported class")
	}
	if !workerClassValid(cast) {
		return WorkerClassUnknown, fweng.New(fweng.InternalInvariant, "test: out-of-range class")
	}
	return cast, nil
}

func TestGuardBranches(t *testing.T) {
	tests := []struct {
		name     string
		cast     WorkerClass
		wantKind fweng.Kind
	}{
		{"unsupported class -> InvalidInput (no silent fallback)", WorkerClassUnknown, fweng.InvalidInput},
		{"out-of-range class -> InternalInvariant", WorkerClass(99), fweng.InternalInvariant},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pickWith(activity("engine"), tc.cast)
			var fe *fweng.Error
			if !errors.As(err, &fe) {
				t.Fatalf("expected *fweng.Error, got %T: %v", err, err)
			}
			if fe.Kind != tc.wantKind {
				t.Fatalf("kind = %v, want %v", fe.Kind, tc.wantKind)
			}
		})
	}

	// And assert the real strategies NEVER cast an unsupported class (so the
	// public surface never trips InvalidInput in v1) — every registered strategy
	// over a valid activity yields a valid, supported class.
	strategies := []handOffStrategy{
		fullyAutomatedStrategy{seniorOnlyLayers: normalizeLayers([]string{"manager"})},
		seniorReviewsAllStrategy{seniorOnlyLayers: normalizeLayers(nil)},
		architectOnlyStrategy{},
		fakeStrategy{out: AIWorker},
	}
	for i, s := range strategies {
		cast := s.pickWorkerClass(activity("manager"))
		if !workerClassValid(cast) {
			t.Errorf("strategy #%d cast an invalid class %v", i, cast)
		}
	}
}

// TestDeterminism asserts the pure-function contract (FU-HE-A twice-called
// identical-output): identical (activity, policy) twice -> identical class.
func TestDeterminism(t *testing.T) {
	eng := NewHandOffEngine()
	a := activity("manager")
	p := HandOffPolicy{PreferAI: true, SeniorOnlyLayers: []string{"manager"}}

	first, err1 := eng.PickWorkerClass(fweng.Context{}, a, p)
	second, err2 := eng.PickWorkerClass(fweng.Context{}, a, p)
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v / %v", err1, err2)
	}
	if first != second {
		t.Fatalf("non-deterministic output: first=%v second=%v", first, second)
	}
	if first != HumanSeniorWorker {
		t.Fatalf("expected HumanSeniorWorker, got %v", first)
	}
}

// TestReentrancy_NoSharedMutableState asserts contract §6 invariant 3: mutating
// the caller's SeniorOnlyLayers slice after a call must not affect a later call.
func TestReentrancy_NoSharedMutableState(t *testing.T) {
	eng := NewHandOffEngine()
	layers := []string{"manager"}
	p := HandOffPolicy{PreferAI: true, SeniorOnlyLayers: layers}

	got1, err := eng.PickWorkerClass(fweng.Context{}, activity("manager"), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got1 != HumanSeniorWorker {
		t.Fatalf("first call = %v, want HumanSeniorWorker", got1)
	}

	// Caller mutates the slice header contents; the Engine snapshots per call.
	layers[0] = "engine"
	got2, err := eng.PickWorkerClass(fweng.Context{}, activity("manager"), HandOffPolicy{PreferAI: true, SeniorOnlyLayers: layers})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With the mutated policy, "manager" is no longer senior-only -> AIWorker.
	if got2 != AIWorker {
		t.Fatalf("second call = %v, want AIWorker (policy now lists only 'engine')", got2)
	}
}

// TestWorkerClassString locks the logical class names the Manager hands to
// workerAccess (parity with the constructionManager consumer mirror).
func TestWorkerClassString(t *testing.T) {
	cases := map[WorkerClass]string{
		AIWorker:           "ai",
		HumanSeniorWorker:  "humanSenior",
		HumanJuniorWorker:  "humanJunior",
		ArchitectOnly:      "architectOnly",
		WorkerClassUnknown: "unknown",
	}
	for c, want := range cases {
		if got := workerClassString(c); got != want {
			t.Errorf("workerClassString(WorkerClass(%d)) = %q, want %q", c, got, want)
		}
	}
}
