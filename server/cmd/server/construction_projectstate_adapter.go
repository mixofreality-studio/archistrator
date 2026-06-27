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

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// pgConstructionPS bridges the Postgres *projectstate.Store to the construction
// Manager's ctx-based construction.ProjectStateAccess mirror, after the in-scope
// ProjectStateAccess port refactor moved Store.ReadProject onto rc fwra.Context.
// The construction mirror (out of scope for that refactor) still consumes a
// ctx-based ReadProject, so this shim re-wraps just that one verb (ctx →
// fwra.Context{Context: ctx}); the embedded *Store supplies the ctx-based
// construction-transition Record* verbs unchanged.
type pgConstructionPS struct {
	*projectstate.Store
}

var _ construction.ProjectStateAccess = pgConstructionPS{}

func (a pgConstructionPS) ReadProject(ctx context.Context, projectID projectstate.ProjectID) (projectstate.Project, error) {
	return a.Store.ReadProject(fwra.Context{Context: ctx}, projectID)
}

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

func (a constructionProjectStateAdapter) RecordActivityFailed(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, reason projectstate.FailureReason, detail string, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error) {
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RecordActivityFailed(ctx, projectID, expectedVersion, activityID, reason, detail, cred, idempotencyKey)
}

func (a constructionProjectStateAdapter) RecordOperatorPaused(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, reason string, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error) {
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RecordOperatorPaused(ctx, projectID, expectedVersion, reason, cred, idempotencyKey)
}

func (a constructionProjectStateAdapter) RecordPhaseStarted(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, phase projectstate.ActivityMethodPhase, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error) {
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RecordPhaseStarted(ctx, projectID, expectedVersion, activityID, phase, cred, idempotencyKey)
}

func (a constructionProjectStateAdapter) RecordPhaseCompleted(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, phase projectstate.ActivityMethodPhase, artifactRef string, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error) {
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RecordPhaseCompleted(ctx, projectID, expectedVersion, activityID, phase, artifactRef, cred, idempotencyKey)
}

func (a constructionProjectStateAdapter) RecordServiceContractProduced(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, component string, contract projectstate.ServiceContract, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error) {
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RecordServiceContractProduced(ctx, projectID, expectedVersion, component, contract, cred, idempotencyKey)
}

func (a constructionProjectStateAdapter) RecordPhaseArtifactProduced(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, mapKey string, payload projectstate.PhaseArtifactPayload, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error) {
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RecordPhaseArtifactProduced(ctx, projectID, expectedVersion, activityID, mapKey, payload, cred, idempotencyKey)
}
