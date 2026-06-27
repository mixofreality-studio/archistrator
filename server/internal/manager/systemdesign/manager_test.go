package systemdesign

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// These tests cover the façade-boundary pre-condition checks the contract puts on
// the four public ops (systemDesignManager.md §2/§3). They run BEFORE any Temporal
// client call, so they need no cluster and no client — a nil client is safe
// because the checks short-circuit first.

func asSystemDesignError(t *testing.T, err error) *fwmanager.Error {
	t.Helper()
	var sde *fwmanager.Error
	if !errors.As(err, &sde) {
		t.Fatalf("expected *SystemDesignError, got %T: %v", err, err)
	}
	return sde
}

// ---- StartSystemDesign (op 2.0, 2026-05-29) façade preconditions ------------

func Test_StartSystemDesign_EmptyProjectID(t *testing.T) {
	m := NewManager(nil, nil)
	_, err := m.StartSystemDesign(context.Background(), ProjectID(""))
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

// ResearchInput absent (a project with no row) -> FailedPrecondition. The
// precondition check short-circuits before any Temporal client call, so a nil
// client is safe.
func Test_StartSystemDesign_ResearchAbsent_FailedPrecondition(t *testing.T) {
	ps := &renderFakeProjectState{readErr: fwra.New(fwra.NotFound, "no row yet")}
	m := NewManager(nil, ps)
	_, err := m.StartSystemDesign(context.Background(), ProjectID(uuid.NewString()))
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for absent research (no project row), got %d", got)
	}
}

// A project that exists but has an empty ResearchInput -> FailedPrecondition.
func Test_StartSystemDesign_ResearchEmpty_FailedPrecondition(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	ps := &renderFakeProjectState{project: projectstate.Project{ID: pid}} // zero ResearchInput
	m := NewManager(nil, ps)
	_, err := m.StartSystemDesign(context.Background(), pid)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for empty research, got %d", got)
	}
}

func Test_RequestArtifactDraft_EmptyProjectID(t *testing.T) {
	m := NewManager(nil, nil)
	_, err := m.RequestArtifactDraft(context.Background(), ProjectID(""), projectstate.KindMission, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_RequestArtifactDraft_WrongPhaseKind(t *testing.T) {
	m := NewManager(nil, nil)
	// A Phase-2 kind is a Client bug for the Phase-1 Manager.
	_, err := m.RequestArtifactDraft(context.Background(), ProjectID(uuid.NewString()), projectstate.KindSdpReview, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition, got %d", got)
	}
}

func Test_SubmitReviewDecision_RejectRequiresFeedback(t *testing.T) {
	m := NewManager(nil, nil)
	pid := ProjectID(uuid.NewString())
	err := m.SubmitReviewDecision(context.Background(), pid, projectstate.KindMission, ReviewReject, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for Reject without feedback, got %d", got)
	}

	// Reject with empty notes is also misuse.
	err = m.SubmitReviewDecision(context.Background(), pid, projectstate.KindMission, ReviewReject, &ReviewFeedback{Notes: ""})
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for Reject with empty notes, got %d", got)
	}
}

func Test_SubmitReviewDecision_UnknownDecision(t *testing.T) {
	m := NewManager(nil, nil)
	err := m.SubmitReviewDecision(context.Background(), ProjectID(uuid.NewString()), projectstate.KindMission, ReviewDecisionUnknown, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for unknown decision, got %d", got)
	}
}

func Test_SubmitReviewDecision_WrongPhaseKind(t *testing.T) {
	m := NewManager(nil, nil)
	err := m.SubmitReviewDecision(context.Background(), ProjectID(uuid.NewString()), projectstate.KindActivityList, ReviewApprove, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition, got %d", got)
	}
}

func Test_AdvancePhase_EmptyProjectID(t *testing.T) {
	m := NewManager(nil, nil)
	_, err := m.AdvancePhase(context.Background(), ProjectID(""))
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_GetSessionState_EmptyProjectID(t *testing.T) {
	m := NewManager(nil, nil)
	_, err := m.GetSessionState(context.Background(), ProjectID(""), projectstate.KindMission)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

// SessionRef is opaque: it round-trips and compares by value, never parsed.
func Test_SessionRef_OpaqueValueSemantics(t *testing.T) {
	a := NewSessionRef("proj-1:1")
	b := NewSessionRef("proj-1:1")
	c := NewSessionRef("proj-1:2")
	if !a.Equal(b) {
		t.Fatal("equal refs should compare equal")
	}
	if a.Equal(c) {
		t.Fatal("different refs should not compare equal")
	}
	if a.String() != "proj-1:1" {
		t.Fatalf("unexpected String(): %q", a.String())
	}
}

// ---- minimal test doubles for the façade-precondition tests ----------------

// renderFakeProjectState serves a scripted ReadProject result. Other verbs panic
// — these façade-precondition tests only ever exercise the read path.
type renderFakeProjectState struct {
	project projectstate.Project
	readErr error
}

func (f *renderFakeProjectState) ReadProject(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
	if f.readErr != nil {
		return projectstate.Project{}, f.readErr
	}
	return f.project, nil
}

func (f *renderFakeProjectState) StageArtifactForReview(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactModel) (projectstate.Version, error) {
	panic("renderFakeProjectState.StageArtifactForReview must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) CommitArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind) (projectstate.Version, error) {
	panic("renderFakeProjectState.CommitArtifact must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) RejectArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string) (projectstate.Version, error) {
	panic("renderFakeProjectState.RejectArtifact must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) WithdrawArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string) (projectstate.Version, error) {
	panic("renderFakeProjectState.WithdrawArtifact must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) AdvancePhase(fwra.Context, projectstate.ProjectID, projectstate.Version) (projectstate.Version, error) {
	panic("renderFakeProjectState.AdvancePhase must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) SetResearchInput(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ResearchInput) (projectstate.Version, error) {
	panic("renderFakeProjectState.SetResearchInput must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) CreateProject(fwra.Context, projectstate.ProjectID, projectstate.OwnerScope, string) (projectstate.Version, error) {
	panic("renderFakeProjectState.CreateProject must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) ListProjects(fwra.Context, projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	panic("renderFakeProjectState.ListProjects must not be called by these façade-precondition tests")
}

// compile-time conformance.
var _ projectstate.ProjectStateAccess = (*renderFakeProjectState)(nil)

// IsPhase1 covers the Phase-1 subset gate the contract uses.
func Test_ArtifactKind_IsPhase1(t *testing.T) {
	phase1 := []projectstate.ArtifactKind{
		projectstate.KindMission, projectstate.KindGlossary, projectstate.KindScrubbedRequirements,
		projectstate.KindVolatilities, projectstate.KindCoreUseCases, projectstate.KindSystem,
		projectstate.KindOperationalConcepts, projectstate.KindStandardCheck,
	}
	for _, k := range phase1 {
		if !k.IsPhase1() {
			t.Fatalf("kind %s should be Phase 1", k)
		}
	}
	notPhase1 := []projectstate.ArtifactKind{
		projectstate.KindSdpReview, projectstate.KindActivityList,
		projectstate.KindNetwork, projectstate.KindRiskModel,
	}
	for _, k := range notPhase1 {
		if k.IsPhase1() {
			t.Fatalf("kind %s should NOT be Phase 1", k)
		}
	}
}
