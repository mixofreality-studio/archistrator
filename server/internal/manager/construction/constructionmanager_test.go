package construction

import (
	"context"
	"testing"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// fakeReviewPolicyTransition is a minimal fake satisfying projectstate.ConstructionTransitionAccess
// for UpdateReviewPolicy tests. It embeds the interface (unimplemented methods panic
// if reached — intentional) and only implements the two verbs UpdateReviewPolicy exercises:
// ReadProject (to supply the current version) and RecordReviewPolicy (the write verb).
type fakeReviewPolicyTransition struct {
	projectstate.ConstructionTransitionAccess
	version    projectstate.Version
	lastPolicy *projectstate.ReviewPolicy
}

func (f *fakeReviewPolicyTransition) ReadProject(_ context.Context, _ projectstate.ProjectID, _ projectstate.RepoCredential) (projectstate.Project, error) {
	return projectstate.Project{Version: f.version}, nil
}

func (f *fakeReviewPolicyTransition) RecordReviewPolicy(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, policy projectstate.ReviewPolicy, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.version++
	f.lastPolicy = &policy
	return f.version, nil
}

// TestUpdateReviewPolicy asserts that UpdateReviewPolicy maps the ReviewPolicyInput
// through projectstate.ReviewPolicyFromGateIDs and calls RecordReviewPolicy with the
// resulting typed ReviewPolicy. The ad-hoc gate id "svc-contract" maps to
// MethodPhaseDetailedDesign for the "service" activity type; after the call,
// RequiresHuman("service", MethodPhaseDetailedDesign) must be true on the persisted policy.
func TestUpdateReviewPolicy(t *testing.T) {
	fake := &fakeReviewPolicyTransition{version: 7}
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fake, nil, 0, "")

	err := m.UpdateReviewPolicy(testCtx(), "proj-1", ReviewPolicyInput{
		GatedPhasesByType: map[string][]string{
			"service": {"svc-contract"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateReviewPolicy: %v", err)
	}
	if fake.lastPolicy == nil {
		t.Fatal("RecordReviewPolicy was not called")
	}
	// "svc-contract" is the ad-hoc gate id that maps to MethodPhaseDetailedDesign
	// via projectstate.gateIDToPhase; ReviewPolicyFromGateIDs must translate it.
	if !fake.lastPolicy.RequiresHuman("service", projectstate.MethodPhaseDetailedDesign) {
		t.Fatalf("expected service/detailed_design to require human, got policy=%+v", fake.lastPolicy)
	}
}
