package main

// This file holds the COMPOSITION-ROOT adapter that bridges the concrete
// sourceControlAccess ResourceAccess (internal/resourceaccess/sourcecontrol) to
// the projectManager's narrow consumer-mirror port
// (internal/manager/project.SourceControlAccess). The projectManager declares the
// lifecycle verbs it needs (adopt + seat-for-agentic-dispatch) over its OWN
// package-local provider-neutral types (project.RepoSpec / project.RepoRef /
// project.RepoCredential), per the "accept interfaces" idiom and the layer model's
// dependency-inversion rule: a Manager depends on an interface IT declares, not on
// a sibling RA's concrete type. The concrete sourcecontrol.Access names its own
// (equally provider-neutral) RepoAdoptionSpec / RepoRef / RepoCredential, so the
// two surfaces are mechanically bridged HERE — the one place that imports both —
// never by editing the frozen RA contract or the Manager port.
//
// main.go is OUTSIDE internal/, so this adapter may freely import the concrete
// sourcecontrol package; it imports no Temporal (the projectManager owns none).
//
// C-PM-Δ (2026-06-15): the Manager port re-cut from the single ProvisionProjectRepo
// verb to the adopt-then-seat surface. The 2026-06-15 correction then REMOVED the
// write-token bridge (aiarch does no secret management; CLAUDE_CODE_OAUTH_TOKEN is
// user-provisioned via the Claude Code GitHub App). 2026-06-16: the seat translation
// now seats the full MANAGED SCAFFOLD (design workflow + go-test gate) via the RA's
// generalized CommitManagedFiles. So this bridge is THREE translations (adopt / mint
// / seat-scaffold). The two opaque handles the Manager
// threads between them (RepoRef, RepoCredential) are carried across the seam by the
// tiny adoptedRepoRef / mintedCredential wrappers below, which hold the concrete RA
// value and hand it back on the seating call — so the Manager stays in its own opaque
// interface types and the concrete never leaks up.

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// sourceControlAdapter adapts *sourcecontrol.Access to project.SourceControlAccess.
// It carries the adopt + seating verbs project birth needs.
type sourceControlAdapter struct{ inner sourcecontrol.SourceControlAccess }

var _ project.SourceControlAccess = sourceControlAdapter{}

// adoptedRepoRef wraps the concrete sourcecontrol.RepoRef so it both satisfies the
// Manager's opaque project.RepoRef interface (IsZero/String) AND carries the
// concrete value back into the seating verbs without the Manager ever seeing it.
type adoptedRepoRef struct{ ref sourcecontrol.RepoRef }

func (r adoptedRepoRef) IsZero() bool   { return sourcecontrol.RepoRefIsZero(r.ref) }
func (r adoptedRepoRef) String() string { return sourcecontrol.RepoRefString(r.ref) }

// mintedCredential wraps the concrete sourcecontrol.RepoCredential so it satisfies
// the Manager's opaque project.RepoCredential interface and carries the concrete
// credential back into the seating verbs. The Manager neither parses nor logs it.
type mintedCredential struct{ cred sourcecontrol.RepoCredential }

func (c mintedCredential) IsZero() bool { return sourcecontrol.RepoCredentialIsZero(c.cred) }

// AdoptProjectRepo translates the Manager's provider-neutral RepoSpec to the RA's
// RepoAdoptionSpec and invokes the RA's strict-adopt AdoptProjectRepo. NAME-AS-
// IDENTITY (C-PM-Δ): spec.RepoName is the user-supplied repo name == the project
// identity, threaded verbatim. The opaque RA RepoRef is wrapped so the Manager sees
// only its narrow interface.
func (a sourceControlAdapter) AdoptProjectRepo(
	ctx context.Context,
	spec project.RepoSpec,
	key fwra.IdempotencyKey,
) (project.RepoRef, error) {
	ref, err := a.inner.AdoptProjectRepo(fwra.Context{Context: ctx, IdempotencyKey: key}, sourcecontrol.RepoAdoptionSpec{
		RepoName: spec.RepoName, // name-as-identity: the project id IS the repo name
		Account:  sourcecontrol.AccountRef(spec.Account),
		Title:    spec.Title,
	})
	if err != nil {
		return nil, err
	}
	return adoptedRepoRef{ref: ref}, nil
}

// MintRepoCredential mints the short-lived installation credential (RA
// GetInstallationToken) the seating verbs need, wrapping it as the Manager's opaque
// project.RepoCredential.
func (a sourceControlAdapter) MintRepoCredential(
	ctx context.Context,
	repo project.RepoRef,
) (project.RepoCredential, error) {
	cred, err := a.inner.GetInstallationToken(fwra.Context{Context: ctx}, concreteRepo(repo))
	if err != nil {
		return nil, err
	}
	return mintedCredential{cred: cred}, nil
}

// SeatAgenticWorkflow seats the aiarch-managed project SCAFFOLD — the embedded
// claude-code-action DESIGN workflow PLUS the go-test gate (go.mod +
// aiarch_method_test.go, templated with the adopted repo's module path + the pinned
// framework-go version) — into the repo via the RA's CommitManagedFiles. WHICH files
// (the design template + the gate scaffold) is the composition root's concern, kept
// off the Manager surface; the Manager still just asks the repo be seated for dispatch.
func (a sourceControlAdapter) SeatAgenticWorkflow(
	ctx context.Context,
	repo project.RepoRef,
	cred project.RepoCredential,
	key fwra.IdempotencyKey,
) error {
	files, err := sourcecontrol.ManagedScaffoldFiles(concreteRepo(repo))
	if err != nil {
		return err
	}
	_, err = a.inner.CommitManagedFiles(fwra.Context{Context: ctx, IdempotencyKey: key}, concreteRepo(repo), files, concreteCred(cred))
	return err
}

// concreteRepo unwraps the Manager's opaque project.RepoRef back to the concrete RA
// RepoRef. The handle always originates from this adapter's AdoptProjectRepo, so the
// type assertion always holds; a foreign ref unwraps to the zero RepoRef (which the
// RA verbs reject as ContractMisuse).
func concreteRepo(repo project.RepoRef) sourcecontrol.RepoRef {
	if w, ok := repo.(adoptedRepoRef); ok {
		return w.ref
	}
	return sourcecontrol.RepoRef("")
}

// concreteCred unwraps the Manager's opaque project.RepoCredential back to the
// concrete RA RepoCredential. Always originates from this adapter's
// MintRepoCredential; a foreign credential unwraps to the zero credential (rejected
// by the RA verbs as ContractMisuse).
func concreteCred(cred project.RepoCredential) sourcecontrol.RepoCredential {
	if w, ok := cred.(mintedCredential); ok {
		return w.cred
	}
	return sourcecontrol.RepoCredential{}
}

// ---------------------------------------------------------------------------
// PR-rail bridge (RA-context adapter).
//
// The design Managers (systemdesign / projectdesign) and the construction Manager
// each declare a ctx-based SourceControlRail consumer port (their own narrow mirror
// of the PR rail). The concrete sourceControlAccess RA now takes the ResourceAccess
// call Context (fwra.Context) on every op — so it no longer STRUCTURALLY satisfies
// those plain-ctx mirrors. railAdapter is the composition-root bridge: it wraps the
// concrete *sourcecontrol.Access and builds fwra.Context{Context, IdempotencyKey} at
// the boundary, exactly like the constructionPipeline / durableExecution adapters in
// construction_adapters.go. The three Manager rails share an identical method set, so
// this one adapter satisfies all three structurally.
// ---------------------------------------------------------------------------

// railAdapter adapts the RA-context *sourcecontrol.Access to the plain-ctx PR-rail
// consumer ports the design + construction Managers declare.
type railAdapter struct{ inner sourcecontrol.SourceControlAccess }

func (r railAdapter) GetInstallationToken(ctx context.Context, repo sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	return r.inner.GetInstallationToken(fwra.Context{Context: ctx}, repo)
}

func (r railAdapter) OpenBranch(ctx context.Context, repo sourcecontrol.RepoRef, branch sourcecontrol.BranchName, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) (sourcecontrol.BranchRef, error) {
	return r.inner.OpenBranch(fwra.Context{Context: ctx, IdempotencyKey: key}, repo, branch, cred)
}

func (r railAdapter) OpenPullRequest(ctx context.Context, repo sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) (sourcecontrol.PullRequestRef, error) {
	return r.inner.OpenPullRequest(fwra.Context{Context: ctx, IdempotencyKey: key}, repo, spec, cred)
}

func (r railAdapter) GetPullRequestStatus(ctx context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, cred sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
	return r.inner.GetPullRequestStatus(fwra.Context{Context: ctx}, repo, pr, cred)
}

func (r railAdapter) PostReview(ctx context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, review sourcecontrol.ReviewSubmission, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) error {
	return r.inner.PostReview(fwra.Context{Context: ctx, IdempotencyKey: key}, repo, pr, review, cred)
}

func (r railAdapter) MergePullRequest(ctx context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) (sourcecontrol.MergeResult, error) {
	return r.inner.MergePullRequest(fwra.Context{Context: ctx, IdempotencyKey: key}, repo, pr, cred)
}
