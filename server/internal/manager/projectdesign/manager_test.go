package projectdesign

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// These tests cover the façade-boundary pre-condition checks the contract puts on
// the public ops (projectDesignManager.md §2/§3). They run BEFORE any Temporal
// client call, so they need no cluster and no client — a nil client is safe
// because the checks short-circuit first.

func asProjectDesignError(t *testing.T, err error) *fwmanager.Error {
	t.Helper()
	var pde *fwmanager.Error
	if !errors.As(err, &pde) {
		t.Fatalf("expected *ProjectDesignError, got %T: %v", err, err)
	}
	return pde
}

// ---- RequestArtifactDraft ---------------------------------------------------

func Test_RequestArtifactDraft_EmptyProjectID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.RequestArtifactDraft(fwmanager.Context{Context: context.Background()}, ProjectID(""), KindPlanningAssumptions, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_RequestArtifactDraft_Phase1Kind_FailedPrecondition(t *testing.T) {
	m := NewManager(nil)
	// A Phase-1 kind is a Client bug for the Phase-2 Manager.
	_, err := m.RequestArtifactDraft(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindMission, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for a Phase-1 kind, got %d", got)
	}
}

func Test_RequestArtifactDraft_SdpReviewKind_FailedPrecondition(t *testing.T) {
	m := NewManager(nil)
	// The SDP review is assembled, not co-authored.
	_, err := m.RequestArtifactDraft(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindSdpReview, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for KindSdpReview, got %d", got)
	}
}

// ---- RequestSDPCommit -------------------------------------------------------

func Test_RequestSDPCommit_EmptyProjectID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.RequestSDPCommit(fwmanager.Context{Context: context.Background()}, ProjectID(""))
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

// ---- SubmitSDPDecision ------------------------------------------------------

func Test_SubmitSDPDecision_EmptyProjectID(t *testing.T) {
	m := NewManager(nil)
	err := m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, ProjectID(""), SDPCommit, nil, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_SubmitSDPDecision_CommitRequiresOptionID(t *testing.T) {
	m := NewManager(nil)
	pid := ProjectID(uuid.NewString())

	// nil optionId.
	err := m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, pid, SDPCommit, nil, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for Commit without optionId, got %d", got)
	}

	// empty optionId.
	empty := OptionID("")
	err = m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, pid, SDPCommit, &empty, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for Commit with empty optionId, got %d", got)
	}
}

func Test_SubmitSDPDecision_RejectAllRequiresFeedback(t *testing.T) {
	m := NewManager(nil)
	pid := ProjectID(uuid.NewString())

	err := m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, pid, SDPRejectAll, nil, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for RejectAll without feedback, got %d", got)
	}

	err = m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, pid, SDPRejectAll, nil, &ReviewFeedback{Notes: ""})
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for RejectAll with empty notes, got %d", got)
	}
}

func Test_SubmitSDPDecision_UnknownDecision(t *testing.T) {
	m := NewManager(nil)
	err := m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), SDPDecisionUnknown, nil, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for unknown decision, got %d", got)
	}
}

// ---- SubmitReviewDecision (per-artifact OQ-3 gate) --------------------------

func Test_SubmitReviewDecision_EmptyProjectID(t *testing.T) {
	m := NewManager(nil)
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(""), KindPlanningAssumptions, ReviewApprove, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_SubmitReviewDecision_RejectRequiresFeedback(t *testing.T) {
	m := NewManager(nil)
	pid := ProjectID(uuid.NewString())
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, pid, KindActivityList, ReviewReject, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for Reject without feedback, got %d", got)
	}

	err = m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, pid, KindActivityList, ReviewReject, &ReviewFeedback{Notes: ""})
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for Reject with empty notes, got %d", got)
	}
}

func Test_SubmitReviewDecision_WrongPhaseKind(t *testing.T) {
	m := NewManager(nil)
	// A Phase-1 kind is a Client bug for the Phase-2 Manager.
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindMission, ReviewApprove, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for a Phase-1 kind, got %d", got)
	}
}

func Test_SubmitReviewDecision_SdpReviewKind_FailedPrecondition(t *testing.T) {
	m := NewManager(nil)
	// The SDP review is not gated via the per-artifact reviewDecision signal.
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindSdpReview, ReviewApprove, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for KindSdpReview, got %d", got)
	}
}

func Test_SubmitReviewDecision_UnknownDecision(t *testing.T) {
	m := NewManager(nil)
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindNetwork, ReviewDecisionUnknown, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for unknown decision, got %d", got)
	}
}

// ---- AdvanceToConstruction --------------------------------------------------

func Test_AdvanceToConstruction_EmptyProjectID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.AdvanceToConstruction(fwmanager.Context{Context: context.Background()}, ProjectID(""))
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

// ---- GetSessionState --------------------------------------------------------

func Test_GetSessionState_EmptyProjectID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, ProjectID(""), KindPlanningAssumptions)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}
