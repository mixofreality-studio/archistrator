package project

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ports.go holds the HAND-WRITTEN consumer-side dependency ports this Manager
// declares (dependency inversion / the layer model: a Manager depends on interfaces
// IT declares, which the concrete ResourceAccess satisfies structurally in the
// composition root). These are NOT part of the generated contract surface
// (contract.gen.go): they reference the projectstate aggregate type directly so the
// concrete projectstate ResourceAccess satisfies them WITHOUT a composition-root
// adapter — the Manager converts the projectstate value shapes into its OWN contract
// types (ProjectState/ProjectSummary/…) at the boundary.

// ---------------------------------------------------------------------------
// Narrow projectStateAccess port — the head-state verbs this Manager consumes (a
// subset of projectstate.ProjectStateAccess). Declared in projectstate's OWN types
// so the concrete RA satisfies it structurally; the Manager maps to its contract
// types after the call.
// ---------------------------------------------------------------------------

// ProjectStateAccess is the narrow head-state port the projectManager consumes.
// projectstate.ProjectStateAccess satisfies it structurally.
type ProjectStateAccess interface {
	CreateProject(rc fwra.Context, projectID projectstate.ProjectID, owner projectstate.OwnerScope, name string) (projectstate.Version, error)
	ListProjects(rc fwra.Context, owner projectstate.OwnerScope) ([]projectstate.ProjectSummary, error)
	ReadProject(rc fwra.Context, projectID projectstate.ProjectID) (projectstate.Project, error)
}

// ---------------------------------------------------------------------------
// Narrow sourceControlAccess port — the lifecycle verbs project birth needs. The
// opaque value types it names (RepoSpec / RepoRef / RepoCredential) are the SAME
// provider-neutral types the RA exposes, threaded through a tiny composition-root
// adapter so this package imports no GitHub vocabulary and no sibling-RA concrete.
// ---------------------------------------------------------------------------

// RepoSpec is the provider-NEUTRAL description of the user's EXISTING repo to ADOPT
// at project birth. RepoName is the user-supplied identity (project name == repo
// name, name-as-identity).
type RepoSpec struct {
	// RepoName is the user-supplied repo name == the project identity (the adopt
	// idempotency anchor). The repo MUST already exist; AdoptProjectRepo never creates it.
	RepoName string
	// Account is the provider-neutral source-control account/org the repo lives under.
	// Empty means "the RA's composition-root default account".
	Account string
	// Title is the human project name applied as the repo description.
	Title string
}

// RepoRef is an opaque, provider-neutral handle to the adopted per-project repo.
type RepoRef interface {
	// IsZero reports whether the ref addresses no repo.
	IsZero() bool
	// String returns the canonical printable form (logging/audit only).
	String() string
}

// RepoCredential is an opaque, short-lived bearer credential the Manager MINTS from
// the adopted repo and THREADS into the seating verb. Fully opaque to the Manager.
type RepoCredential interface {
	// IsZero reports whether the credential addresses no repo / is unset.
	IsZero() bool
}

// SourceControlAccess is the narrow source-control lifecycle port the projectManager
// consumes at project birth: ADOPT the user's repo and SEAT it for agentic dispatch
// (caller-home ratified == project birth) BEFORE the head-state row is created. Every
// write verb is idempotent (adopt re-converges on the repo name; the workflow file is
// overwrite-if-changed), so a retry after a partial failure re-converges.
type SourceControlAccess interface {
	// AdoptProjectRepo adopts the user's existing repo under the App installation and
	// tags it. Permissive: succeeds regardless of content; only NotUnderInstallation is terminal.
	AdoptProjectRepo(ctx context.Context, spec RepoSpec, key fwra.IdempotencyKey) (RepoRef, error)
	// MintRepoCredential mints the short-lived credential SeatAgenticWorkflow needs to
	// commit the workflow file. The Manager threads the result into SeatAgenticWorkflow.
	MintRepoCredential(ctx context.Context, repo RepoRef) (RepoCredential, error)
	// SeatAgenticWorkflow commits the claude-code-action DESIGN workflow file into the
	// repo's .github/workflows/.
	SeatAgenticWorkflow(ctx context.Context, repo RepoRef, cred RepoCredential, key fwra.IdempotencyKey) error
}
