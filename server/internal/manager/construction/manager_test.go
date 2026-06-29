package construction

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// These tests cover the façade-boundary pre-condition checks the contract puts on
// the five public ops (constructionManager.md §2/§3.5). They run BEFORE any
// Temporal client call, so they need no cluster and no client — a nil client is
// safe because the checks short-circuit first.

func asConstructionError(t *testing.T, err error) *fwmanager.Error {
	t.Helper()
	var ce *fwmanager.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *constructionError, got %T: %v", err, err)
	}
	return ce
}

// ---- ExecuteNextActivity (op 2.1) ------------------------------------------

func Test_ExecuteNextActivity_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "")
	_, err := m.ExecuteNextActivity(fwmanager.Context{Context: context.Background()}, ProjectID(""), "tick-1")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_ExecuteNextActivity_EmptyTickID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "")
	_, err := m.ExecuteNextActivity(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- RunReplanSweep (op 2.2) ------------------------------------------------

func Test_RunReplanSweep_EmptyTickID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "")
	_, err := m.RunReplanSweep(fwmanager.Context{Context: context.Background()}, nil, "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_RunReplanSweep_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "")
	nilID := ProjectID("")
	_, err := m.RunReplanSweep(fwmanager.Context{Context: context.Background()}, &nilID, "tick-1")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an explicit nil projectId, got %s", got)
	}
}

// ---- PauseProject (op 2.3) --------------------------------------------------

func Test_PauseProject_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "")
	err := m.PauseProject(fwmanager.Context{Context: context.Background()}, ProjectID(""), "reason")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_PauseProject_EmptyReason(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "")
	err := m.PauseProject(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an empty pause reason, got %s", got)
	}
}

// ---- OverrideActivity (op 2.4) ----------------------------------------------

func Test_OverrideActivity_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "")
	err := m.OverrideActivity(fwmanager.Context{Context: context.Background()}, ProjectID(""), "C-1", ActivityOverride{Kind: OverrideRetry})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_OverrideActivity_EmptyActivityID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "")
	err := m.OverrideActivity(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), "", ActivityOverride{Kind: OverrideRetry})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an empty activityId, got %s", got)
	}
}

func Test_OverrideActivity_UnknownOverrideKind(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "")
	err := m.OverrideActivity(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), "C-1", ActivityOverride{Kind: OverrideUnknown})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an unknown override kind, got %s", got)
	}
}

// ---- GetSessionState (op 2.5) -----------------------------------------------

func Test_GetSessionState_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "")
	_, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, ProjectID(""), nil)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_GetSessionState_EmptyActivityID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "")
	empty := ActivityID("")
	_, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), &empty)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an explicit empty activityId, got %s", got)
	}
}

// ---- workflow id derivation -------------------------------------------------

func Test_WorkflowIDDerivation(t *testing.T) {
	pid := ProjectID("11111111-1111-1111-1111-111111111111")

	if got := pumpWorkflowID(pid, "t1"); got != string(pid)+":nextActivity:t1" {
		t.Fatalf("pump id: %q", got)
	}
	if got := constructActivityWorkflowID(pid, "C-9"); got != string(pid)+":C-9" {
		t.Fatalf("child id: %q", got)
	}
	if got := replanSweepWorkflowID(&pid, "t2"); got != string(pid)+":replanSweep:t2" {
		t.Fatalf("sweep id: %q", got)
	}
	if got := replanSweepWorkflowID(nil, "t3"); got != ":all:replanSweep:t3" {
		t.Fatalf("all-sweep id: %q", got)
	}
	if got := pauseTargetWorkflowID(pid); got != string(pid)+":construction" {
		t.Fatalf("pause target id: %q", got)
	}
}

// ---- OverrideKind / WorkerClass / activityKind String coverage --------------

func Test_OverrideKind_String(t *testing.T) {
	cases := map[OverrideKind]string{
		OverrideTakeover: "Takeover", OverrideRetry: "Retry",
		OverrideSkip: "Skip", OverrideReassign: "Reassign", OverrideUnknown: "Unknown",
	}
	for k, want := range cases {
		if got := overrideKindName(k); got != want {
			t.Fatalf("overrideKindName(%d) = %q, want %q", int(k), got, want)
		}
	}
}
