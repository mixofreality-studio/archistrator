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

	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// sourceControlAdapter adapts *sourcecontrol.Access to project.SourceControlAccess.
// It carries the adopt + seating verbs project birth needs.
type sourceControlAdapter struct{ inner *sourcecontrol.Access }

var _ project.SourceControlAccess = sourceControlAdapter{}

// adoptedRepoRef wraps the concrete sourcecontrol.RepoRef so it both satisfies the
// Manager's opaque project.RepoRef interface (IsZero/String) AND carries the
// concrete value back into the seating verbs without the Manager ever seeing it.
type adoptedRepoRef struct{ ref sourcecontrol.RepoRef }

func (r adoptedRepoRef) IsZero() bool   { return r.ref.IsZero() }
func (r adoptedRepoRef) String() string { return r.ref.String() }

// mintedCredential wraps the concrete sourcecontrol.RepoCredential so it satisfies
// the Manager's opaque project.RepoCredential interface and carries the concrete
// credential back into the seating verbs. The Manager neither parses nor logs it.
type mintedCredential struct{ cred sourcecontrol.RepoCredential }

func (c mintedCredential) IsZero() bool { return c.cred.IsZero() }

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
	ref, err := a.inner.AdoptProjectRepo(ctx, sourcecontrol.RepoAdoptionSpec{
		RepoName: spec.RepoName, // name-as-identity: the project id IS the repo name
		Account:  sourcecontrol.AccountRef(spec.Account),
		Title:    spec.Title,
	}, key)
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
	cred, err := a.inner.GetInstallationToken(ctx, concreteRepo(repo))
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
	_, err = a.inner.CommitManagedFiles(ctx, concreteRepo(repo), files, concreteCred(cred), key)
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
	return sourcecontrol.RepoRef{}
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
