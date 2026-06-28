package main

// projectstate_git_adapter.go holds the COMPOSITION-ROOT adapter that lets the
// design Managers (systemDesignManager, projectDesignManager, projectManager) write
// the UC1/UC2 head-state to the per-project GIT repo (projectStateAccess.md §REWORK,
// gitstore.go) while STILL consuming the unchanged no-credential
// projectstate.ProjectStateAccess port they were built against.
//
// THE CONTRACT-SHAPE GAP (I-GIT-DESIGN). The git substrate, *projectstate.GitStore,
// satisfies the cred-threaded projectstate.GitProjectStateAccess: every
// provider-touching verb carries an extra `cred projectstate.RepoCredential`. The
// Managers consume the NO-cred projectstate.ProjectStateAccess (CreateProject /
// ListProjects / Stage / Commit / Reject / Withdraw / AdvancePhase / SetResearchInput /
// ReadProject WITHOUT a cred). The two surfaces cannot be mechanically substituted.
//
// RESOLUTION — option (b), a cred-BINDING adapter at the composition root. This file
// presents the Managers' existing no-cred ProjectStateAccess and, per call, MINTS the
// project-scoped RepoCredential and injects it into the GitStore verb. This keeps the
// Manager→RA contract honest (the Managers, their Activities, and the projectstate RA
// itself are untouched — ZERO churn to internal/ design-manager code) and places the
// credential threading EXACTLY where architecture.dsl:582-583 puts it: "the Manager
// mints the credential via getInstallationToken(repo) and threads cred into
// projectStateAccess." main.go is the Manager's wiring agent for that threading; the
// projectstate RA NEVER calls sourceControlAccess (no sideways RA edge — the cred is a
// caller-supplied parameter, D-SC §1.1 returned-not-recorded).
//
// CREDENTIAL SOURCE (two profiles):
//   - LOCAL: the credential is projectstate.LocalRepoCredential() — a trivially-valid
//     no-op the git-data layer reads as "attach no HTTP auth" (file:// remote). No
//     sourceControlAccess needed.
//   - CLOUD: the credential is sourceControlAccess.GetInstallationTokenForProject(
//     account, projectID), the short-lived GitHub-App installation token, in-seam
//     cached. The deterministic per-project RepoRef encoding stays INSIDE the RA.
//
// main.go is OUTSIDE internal/, so this adapter (like sourcecontrol_adapter.go) may
// freely import the concrete projectstate + sourcecontrol packages and the github
// satellite; it imports no Temporal.

import (
	"context"
	"encoding/json"
	"fmt"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// credentialMinter mints the per-project RepoCredential the GitStore's head-state
// writes/reads need. LOCAL returns the no-op local credential; CLOUD mints via
// sourceControlAccess. The cross-project registry credential is GONE (the registry
// repo is removed — founder ruling 2026-06-14); the catalog read no longer needs a
// control-repo credential because ListProjects enumerates via the catalog seam.
type credentialMinter interface {
	credentialFor(ctx context.Context, projectID projectstate.ProjectID) (projectstate.RepoCredential, error)
	// catalogCredential mints the credential ListProjects runs under (the enumeration
	// + its N+1 per-project head-state reads). CLOUD: an installation-scoped token
	// covering every project repo. LOCAL: the no-op local credential.
	catalogCredential(ctx context.Context) (projectstate.RepoCredential, error)
}

// localCredentialMinter is the LOCAL on-disk-git profile: every project repo resolves
// to the trivially-valid local credential, no GitHub.
type localCredentialMinter struct{}

func (localCredentialMinter) credentialFor(context.Context, projectstate.ProjectID) (projectstate.RepoCredential, error) {
	return projectstate.LocalRepoCredential(), nil
}

func (localCredentialMinter) catalogCredential(context.Context) (projectstate.RepoCredential, error) {
	return projectstate.LocalRepoCredential(), nil
}

// cloudCredentialMinter is the CLOUD profile: it mints a repo-scoped GitHub-App
// installation token via the concrete sourceControlAccess (which owns the
// deterministic repo-name encoding), then folds it into the provider-neutral
// projectstate.RepoCredential the GitStore consumes. The two RepoCredential value
// types are SHAPE-MATCHED (both {Bytes, ExpiresAt}); the fold is the one place the
// composition root bridges them, exactly as sourcecontrol_adapter.go bridges the
// RepoSpec/RepoRef shapes.
type cloudCredentialMinter struct {
	sc      sourcecontrol.SourceControlCatalogAccess
	account sourcecontrol.AccountRef
}

func (m cloudCredentialMinter) credentialFor(ctx context.Context, projectID projectstate.ProjectID) (projectstate.RepoCredential, error) {
	cred, err := m.sc.GetInstallationTokenForProject(ctx, m.account, sourcecontrol.ProjectID(projectID.String()))
	if err != nil {
		return projectstate.RepoCredential{}, err
	}
	return projectstate.RepoCredential{Bytes: cred.Bytes, ExpiresAt: cred.ExpiresAt}, nil
}

func (m cloudCredentialMinter) catalogCredential(ctx context.Context) (projectstate.RepoCredential, error) {
	cred, err := m.sc.GetInstallationTokenForAccount(ctx, m.account)
	if err != nil {
		return projectstate.RepoCredential{}, err
	}
	return projectstate.RepoCredential{Bytes: cred.Bytes, ExpiresAt: cred.ExpiresAt}, nil
}

// projectStateGitAdapter binds a credentialMinter over the cred-threaded
// *projectstate.GitStore and presents the no-cred projectstate.ProjectStateAccess the
// design Managers consume. Each verb mints the appropriate-scoped credential
// just-in-time and injects it: per-project verbs use credentialFor(projectID); the
// catalog read (ListProjects) uses credentialForRegistry (the registry control repo).
type projectStateGitAdapter struct {
	store  *projectstate.GitStore
	minter credentialMinter
}

var _ projectstate.ProjectStateAccess = (*projectStateGitAdapter)(nil)

func (a *projectStateGitAdapter) CreateProject(rc fwra.Context, projectID projectstate.ProjectID, owner projectstate.OwnerScope, name string) (projectstate.Version, error) {
	ctx := rc.Context
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.CreateProject(ctx, projectID, owner, name, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) ListProjects(rc fwra.Context, owner projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	ctx := rc.Context
	// Discover-by-enumeration: the catalog seam (wired into the GitStore via WithCatalog)
	// enumerates the project repos itself; the per-project head-state reads inside
	// ListProjects mint their own per-project credential through this same minter (the
	// store re-enters ReadProject which the cloud catalog supplies a project cred for).
	// A single owner-scoped credential drives the enumeration; the store's per-project
	// reads use the credential the catalog seam threads. Pass the owner's catalog
	// credential (cloud: a token scoped to the installation; local: the no-op).
	cred, err := a.catalogCredential(ctx)
	if err != nil {
		return nil, err
	}
	return a.store.ListProjects(ctx, owner, cred)
}

// catalogCredential mints the credential the ListProjects enumeration + its per-project
// head-state reads run under. LOCAL is the no-op local credential. CLOUD mints an
// installation-scoped token (the project-repo reads inside ListProjects share the same
// installation token — the App installation spans every project repo under the account).
func (a *projectStateGitAdapter) catalogCredential(ctx context.Context) (projectstate.RepoCredential, error) {
	return a.minter.catalogCredential(ctx)
}

func (a *projectStateGitAdapter) StageArtifactForReview(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, model projectstate.ArtifactModel) (projectstate.Version, error) {
	ctx := rc.Context
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.StageArtifactForReview(ctx, projectID, expectedVersion, model, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) CommitArtifact(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, kind projectstate.ArtifactKind) (projectstate.Version, error) {
	ctx := rc.Context
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.CommitArtifact(ctx, projectID, expectedVersion, kind, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) RejectArtifact(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, kind projectstate.ArtifactKind, notes string) (projectstate.Version, error) {
	ctx := rc.Context
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RejectArtifact(ctx, projectID, expectedVersion, kind, notes, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) WithdrawArtifact(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, kind projectstate.ArtifactKind, notes string) (projectstate.Version, error) {
	ctx := rc.Context
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.WithdrawArtifact(ctx, projectID, expectedVersion, kind, notes, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) AdvancePhase(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version) (projectstate.Version, error) {
	ctx := rc.Context
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.AdvancePhase(ctx, projectID, expectedVersion, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) SetResearchInput(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, research projectstate.ResearchInput) (projectstate.Version, error) {
	ctx := rc.Context
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.SetResearchInput(ctx, projectID, expectedVersion, research, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) ReadProject(rc fwra.Context, projectID projectstate.ProjectID) (projectstate.Project, error) {
	ctx := rc.Context
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return projectstate.Project{}, err
	}
	return a.store.ReadProject(ctx, projectID, cred)
}

// ReadProjectVersion serves the cheap version-only read over the git substrate. The
// git head-state read still hydrates the whole project.json blob, but the verb keeps
// the Manager↔RA contract honest: the Temporal Activity returns only the uint64
// Version across the boundary, not the entire encoded aggregate (architect's fast-
// follow). Absence stays fwra.NotFound via the underlying ReadProject.
func (a *projectStateGitAdapter) ReadProjectVersion(rc fwra.Context, projectID projectstate.ProjectID) (projectstate.Version, error) {
	p, err := a.ReadProject(rc, projectID)
	if err != nil {
		return 0, err
	}
	return p.Version, nil
}

// Compile-time proof the git adapter also serves the branch-aware extension the design
// Managers consume during the AwaitingReview window (I-DESIGN-DISPATCH §2a).
var _ projectstate.BranchAwareProjectStateAccess = (*projectStateGitAdapter)(nil)

// ReadProjectOnBranch is the branch-aware read-back (I-DESIGN-DISPATCH §2a). An empty
// branch reads the default/main exactly as ReadProject; a non-empty branch reads the
// not-yet-merged draft on the session branch. The cred is minted just-in-time, exactly
// like the no-cred ReadProject.
func (a *projectStateGitAdapter) ReadProjectOnBranch(ctx context.Context, projectID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return projectstate.Project{}, err
	}
	return a.store.ReadProjectOnBranch(ctx, projectID, branch, cred)
}

// StageArtifactForReviewOnBranch is the branch-aware AwaitingReview thin-write
// (I-DESIGN-DISPATCH §2a): an empty branch behaves exactly as StageArtifactForReview
// (main); a non-empty branch lands the staged-slot status flip on the session branch
// the draft lives on.
func (a *projectStateGitAdapter) StageArtifactForReviewOnBranch(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, model projectstate.ArtifactModel, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error) {
	cred, err := a.minter.credentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.StageArtifactForReviewOnBranch(ctx, projectID, expectedVersion, branch, model, cred, idempotencyKey)
}

// ---------------------------------------------------------------------------
// RepoLocator — resolves a projectID to a per-project git store handle. It is the seam
// where the composition root supplies the concrete URL scheme
// (github.com/<account>/<name>.git in cloud — name-as-identity, C-PA-AD; a file:// path
// in LOCAL). A plain function-backed value, NOT a sibling RA — no sideways edge.
// ---------------------------------------------------------------------------

// gitRepoLocator builds *fwgithub.GitStore handles for per-project repos.
// perProjectRepoURL maps a projectID to its clone URL. The cross-project registry repo
// is GONE (founder ruling 2026-06-14): the catalog is discovered by enumeration, not a
// dedicated index repo.
type gitRepoLocator struct {
	branch            string
	perProjectRepoURL func(projectID projectstate.ProjectID) string
}

func (l gitRepoLocator) ProjectRepo(projectID projectstate.ProjectID) (*fwgithub.GitStore, error) {
	url := l.perProjectRepoURL(projectID)
	if url == "" {
		return nil, fwra.New(fwra.ContractMisuse, fmt.Sprintf("gitRepoLocator: empty repo URL for project %s", projectID))
	}
	return fwgithub.NewGitStore(url, l.branch)
}

// ProjectRepoOnBranch satisfies projectstate.BranchRepoLocator (I-DESIGN-DISPATCH §2a):
// it binds the per-project GitStore handle to a CALLER-SUPPLIED branch instead of the
// locator's default. The design Managers thread the SESSION BRANCH here so the
// read-back + the AwaitingReview thin-write ride over the branch the Action committed
// the draft on; after merge they pass "" (the default ProjectRepo, main).
func (l gitRepoLocator) ProjectRepoOnBranch(projectID projectstate.ProjectID, branch string) (*fwgithub.GitStore, error) {
	if branch == "" {
		return l.ProjectRepo(projectID)
	}
	url := l.perProjectRepoURL(projectID)
	if url == "" {
		return nil, fwra.New(fwra.ContractMisuse, fmt.Sprintf("gitRepoLocator: empty repo URL for project %s", projectID))
	}
	return fwgithub.NewGitStore(url, branch)
}

// ---------------------------------------------------------------------------
// ProjectCatalog — the discover-by-enumeration seam ListProjects consumes (replaces
// the deleted registry index). Two profiles, function-backed values, NOT sibling RAs.
// ---------------------------------------------------------------------------

// cloudProjectCatalog enumerates the account's aiarch-project repos via the concrete
// sourceControlAccess (which owns the GitHub installation-repo listing + topic filter —
// the topic is the SOLE membership signal post-A1), then maps each ProjectRepoRef to the
// projectstate.ProjectCatalogRef the store consumes, carrying the repo NAME as the
// project identity (name-as-identity, C-PA-AD 2026-06-15 — r.ProjectID() returns the
// WHOLE user-supplied repo name) and the description as the display title. The store's
// no-sideways discipline is preserved: this is a composition-root value the store calls
// as a port, not the store reaching into a sibling RA.
//
// NAME-AS-IDENTITY (C-PM-Δ, 2026-06-15): projectstate.ProjectID is now a string newtype
// whose value IS the adopted repo name, so the catalog carries r.ProjectID() (the WHOLE
// repo name) VERBATIM as the identity. The prior uuid.Parse skip — which silently dropped
// every user-NAMED repo from the landing grid while ProjectID was a uuid.UUID — is GONE
// (C-PA-AD entanglement resolved). Discovery still filters on the aiarch-project topic
// inside ListProjectRepos; this seam just stops re-parsing the name.
type cloudProjectCatalog struct {
	sc      sourcecontrol.SourceControlCatalogAccess
	account sourcecontrol.AccountRef
}

func (c cloudProjectCatalog) ListProjectRepos(ctx context.Context, _ projectstate.OwnerScope, _ projectstate.RepoCredential) ([]projectstate.ProjectCatalogRef, error) {
	repos, err := c.sc.ListProjectRepos(ctx, c.account)
	if err != nil {
		return nil, err
	}
	out := make([]projectstate.ProjectCatalogRef, 0, len(repos))
	for _, r := range repos {
		// r.ProjectID() is the WHOLE repo name (name-as-identity, A1) — carried verbatim
		// as the string ProjectID. A user-named repo is no longer skipped.
		out = append(out, projectstate.ProjectCatalogRef{
			ProjectID: projectstate.ProjectID(r.ProjectID()),
			Title:     r.Description,
		})
	}
	return out, nil
}

// localProjectCatalog enumerates the LOCAL on-disk-git profile's project repos. In the
// local/embedded profile there is no GitHub installation API; the local substrate is a
// single fixed per-project repo URL (cfg.ProjectStateGitRepoURL — the embedded profile
// drives ONE project at a time through the wedge). The catalog therefore reads the one
// known repo's project.json head and yields its id+title if present. scanRepoURL is the
// file:// URL of that repo; the store then re-reads it for phase/progress.
//
// (A multi-repo local layout — a base dir of aiarch-* repos — would scan the directory;
// the current local profile is single-repo, so the on-disk enumeration is the one repo.)
type localProjectCatalog struct {
	repoURL string
	branch  string
}

func (c localProjectCatalog) ListProjectRepos(ctx context.Context, _ projectstate.OwnerScope, cred projectstate.RepoCredential) ([]projectstate.ProjectCatalogRef, error) {
	if c.repoURL == "" {
		return nil, nil
	}
	store, err := fwgithub.NewGitStore(c.repoURL, c.branch)
	if err != nil {
		return nil, err
	}
	snap, err := store.ReadSubtree(ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		return nil, err
	}
	raw, ok := snap.Files["project.json"]
	if !ok {
		return nil, nil // repo exists but no project committed yet — empty catalog
	}
	var doc struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if jerr := json.Unmarshal(raw, &doc); jerr != nil {
		return nil, jerr
	}
	if doc.ID == "" {
		return nil, nil // committed project.json with no id — nothing to surface
	}
	_ = cred
	// NAME-AS-IDENTITY (C-PM-Δ): the stored id IS the project identity (the repo
	// name), carried verbatim — no uuid.Parse.
	return []projectstate.ProjectCatalogRef{{ProjectID: projectstate.ProjectID(doc.ID), Title: doc.Name}}, nil
}
