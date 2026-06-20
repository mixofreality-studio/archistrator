package main

// construction_projectstate_adapter.go is the COMPOSITION-ROOT adapter that lets the
// constructionManager (UC3) read + write the per-project head-state in the GIT
// substrate (the SAME git-local / GitHub store the UC1/UC2 design Managers use via
// projectstate_git_adapter.go) while consuming its OWN no-credential
// construction.ProjectStateAccess consumer port.
//
// THE LOAD-BEARING FIX. Before this, the construction Manager's ProjectState dep was
// hard-wired to the Postgres *projectstate.Store while the dogfooded `archistrator`
// project lives in the git-local store — so the Manager could not see the project and
// the pump was inert. This adapter binds the same credentialMinter the design adapter
// uses over the cred-threaded *projectstate.GitStore and presents the cred-LESS
// construction.ProjectStateAccess (ReadProject + the three Phase-3 transition verbs),
// minting the project-scoped credential just-in-time exactly as projectStateGitAdapter
// does for the design surface. The credential threading lives at the composition root
// (architecture.dsl:582-583); the projectstate RA never calls sourceControlAccess.
//
// The per-activity construction STATUS records (RecordActivityStarted/Completed) are
// NOT on this port — they are the cred-threaded git head-state verbs the Manager
// reaches through construction.GitActivityStatusAccess (the *GitStore satisfies it
// directly) with the Manager threading the credential itself (zero/ignored in the
// local profile). See main.go's WithGitForward wiring.

import (
	"context"

	"github.com/davidmarne/archistrator/server/internal/manager/construction"
	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// constructionProjectStateAdapter binds a credentialMinter over the cred-threaded
// *projectstate.GitStore and presents the construction Manager's no-cred
// ProjectStateAccess. Each verb mints the project-scoped credential just-in-time
// (LOCAL: the no-op local credential; CLOUD: the in-seam GitHub installation token).
type constructionProjectStateAdapter struct {
	store  *projectstate.GitStore
	minter credentialMinter
}

var _ construction.ProjectStateAccess = (*constructionProjectStateAdapter)(nil)

func (a constructionProjectStateAdapter) ReadProject(ctx context.Context, projectID projectstate.ProjectID) (projectstate.Project, error) {
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return projectstate.Project{}, err
	}
	return a.store.ReadProject(ctx, projectID, cred)
}

func (a constructionProjectStateAdapter) RecordChangeReviewed(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error) {
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RecordChangeReviewed(ctx, projectID, expectedVersion, activityID, cred, idempotencyKey)
}

func (a constructionProjectStateAdapter) RecordActivityExited(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, outcome projectstate.ActivityOutcome, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error) {
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RecordActivityExited(ctx, projectID, expectedVersion, activityID, outcome, cred, idempotencyKey)
}

func (a constructionProjectStateAdapter) RecordOperatorPaused(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, reason string, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error) {
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RecordOperatorPaused(ctx, projectID, expectedVersion, reason, cred, idempotencyKey)
}
