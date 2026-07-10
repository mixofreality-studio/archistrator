package projectstate

// gitadapter.go holds the cred-BINDING adapter + the LOCAL deployment ports + the
// deployment VARIANT CONSTRUCTORS for projectStateAccess — the composition-root policy
// that used to live in cmd/server (buildDesignProjectState + projectstate_git_adapter.go)
// folded into the owning package (step-8 fold).
//
// THE CONTRACT-SHAPE GAP (I-GIT-DESIGN). The git substrate, *GitStore, satisfies the
// cred-threaded GitProjectStateAccess: every provider-touching verb carries an extra
// `cred RepoCredential`. The design Managers consume the NO-cred ProjectStateAccess
// (CreateProject / ListProjects / Stage / Commit / Reject / Withdraw / AdvancePhase /
// SetResearchInput / ReadProject WITHOUT a cred). The two surfaces cannot be mechanically
// substituted.
//
// RESOLUTION — a cred-BINDING adapter. projectStateGitAdapter presents the Managers'
// existing no-cred ProjectStateAccess and, per call, MINTS the project-scoped
// RepoCredential (via a CredentialMinter port) and injects it into the GitStore verb.
// This keeps the Manager→RA contract honest and places the credential threading EXACTLY
// where architecture.dsl puts it: "the Manager mints the credential via
// getInstallationToken(repo) and threads cred into projectStateAccess."
//
// NO SIDEWAYS EDGE. The CredentialMinter + ProjectCatalog are function/interface PORTS the
// composition root supplies — the projectstate RA NEVER imports or calls sourceControlAccess
// (D-SC §1.1 returned-not-recorded). The LOCAL profile's ports (localCredentialMinter,
// localProjectCatalog, gitRepoLocator) need no GitHub and live HERE; the CLOUD profile's
// sourcecontrol-backed ports stay at the composition root (importing sourcecontrol would be
// a forbidden RA→RA sideways edge) and are passed into NewGitHubProjectStateAccess.

import (
	"context"
	"encoding/json"
	"fmt"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ---------------------------------------------------------------------------
// VARIANT CONSTRUCTORS — the two live projectStateAccess deployment profiles.
// ---------------------------------------------------------------------------

// NewGitLocalProjectStateAccess builds the LOCAL git projectStateAccess: file:// on-disk
// repos, no credential. The per-project repo URL is taken verbatim (the embedded profile
// drives one throwaway on-disk repo); the catalog is discovered by scanning that repo.
func NewGitLocalProjectStateAccess(repoURL string) (ProjectStateAccess, error) {
	locator := gitRepoLocator{
		branch:            "main",
		perProjectRepoURL: func(ProjectID) string { return repoURL },
	}
	store, err := NewGitStore(locator, true /* local */)
	if err != nil {
		return nil, err
	}
	// Discover-by-enumeration over the single on-disk project repo (no GitHub
	// installation API in local mode — founder ruling 2026-06-14).
	store = store.WithCatalog(localProjectCatalog{repoURL: repoURL, branch: "main"})
	return &projectStateGitAdapter{store: store, minter: localCredentialMinter{}}, nil
}

// NewGitHubProjectStateAccess builds the CLOUD git projectStateAccess: the per-project
// repos live under the account as <webHost>/<account>/<name>.git, where <name> IS the
// project identity (name-as-identity, C-PA-AD 2026-06-15). The catalog + credential minter
// are sourcecontrol-backed PORTS supplied by the composition root (importing sourcecontrol
// here would be a forbidden RA→RA sideways edge); the installation token is minted in-seam
// through the minter.
func NewGitHubProjectStateAccess(webHost, account string, catalog ProjectCatalog, minter CredentialMinter) (ProjectStateAccess, error) {
	locator := gitRepoLocator{
		branch: "main",
		perProjectRepoURL: func(projectID ProjectID) string {
			return cloudPerProjectRepoURL(webHost, account, projectID.String())
		},
	}
	store, err := NewGitStore(locator, false /* cloud */)
	if err != nil {
		return nil, err
	}
	// Discover-by-enumeration: list the GitHub App installation's aiarch-project repos
	// (founder ruling 2026-06-14 — the registry index repo is removed).
	store = store.WithCatalog(catalog)
	return &projectStateGitAdapter{store: store, minter: minter}, nil
}

// GitConstructionPorts type-asserts a ProjectStateAccess built by the Git* variants back to
// the construction-transition + git-activity-status ports its backing *GitStore serves
// (the construction Manager shares the design head-state store; status cascade live). ok is
// false for a non-git ProjectStateAccess. This encapsulates the composition root's former
// private-field type-assertion so the adapter stays unexported.
func GitConstructionPorts(psa ProjectStateAccess) (ConstructionTransitionAccess, GitActivityStatusAccess, bool) {
	if a, ok := psa.(*projectStateGitAdapter); ok {
		return a.store, a.store, true
	}
	return nil, nil, false
}

// cloudPerProjectRepoURL composes the clone URL of a project's per-project repo in the
// CLOUD profile. Under NAME-AS-IDENTITY (C-PA-AD, 2026-06-15) the project identity IS the
// (user-supplied) repo name, so the URL is <webHost>/<account>/<name>.git — the old
// "aiarch-<id>" prefix is DROPPED. This MUST agree with the repo-name the per-project
// credential is scoped to (the credential minter scopes the installation token to
// <account>/<name>.git), so the locator (this URL) and the credential scope address the
// SAME adopted repo verbatim.
func cloudPerProjectRepoURL(webHost, account, name string) string {
	return fmt.Sprintf("%s/%s/%s.git", webHost, account, name)
}

// ---------------------------------------------------------------------------
// CredentialMinter — the port the adapter mints per-call credentials through.
// ---------------------------------------------------------------------------

// CredentialMinter mints the per-project RepoCredential the GitStore's head-state
// writes/reads need. LOCAL returns the no-op local credential; CLOUD mints via the
// sourcecontrol-backed minter the composition root supplies. It is a PORT (not a sibling
// RA the store calls) — no sideways edge.
type CredentialMinter interface {
	// CredentialFor mints the credential a per-project verb runs under.
	CredentialFor(ctx context.Context, projectID ProjectID) (RepoCredential, error)
	// CatalogCredential mints the credential ListProjects runs under (the enumeration +
	// its N+1 per-project head-state reads). CLOUD: an installation-scoped token covering
	// every project repo. LOCAL: the no-op local credential.
	CatalogCredential(ctx context.Context) (RepoCredential, error)
}

// localCredentialMinter is the LOCAL on-disk-git profile: every project repo resolves to
// the trivially-valid local credential, no GitHub.
type localCredentialMinter struct{}

func (localCredentialMinter) CredentialFor(context.Context, ProjectID) (RepoCredential, error) {
	return LocalRepoCredential(), nil
}

func (localCredentialMinter) CatalogCredential(context.Context) (RepoCredential, error) {
	return LocalRepoCredential(), nil
}

// ---------------------------------------------------------------------------
// projectStateGitAdapter — the cred-binding adapter over *GitStore.
// ---------------------------------------------------------------------------

// projectStateGitAdapter binds a CredentialMinter over the cred-threaded *GitStore and
// presents the no-cred ProjectStateAccess the design Managers consume. Each verb mints the
// appropriate-scoped credential just-in-time and injects it: per-project verbs use
// CredentialFor(projectID); the catalog read (ListProjects) uses CatalogCredential.
type projectStateGitAdapter struct {
	store  *GitStore
	minter CredentialMinter
}

var _ ProjectStateAccess = (*projectStateGitAdapter)(nil)

func (a *projectStateGitAdapter) CreateProject(rc fwra.Context, projectID ProjectID, owner OwnerScope, name string) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.CreateProject(ctx, projectID, owner, name, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) ListProjects(rc fwra.Context, owner OwnerScope) ([]ProjectSummary, error) {
	ctx := rc.Context
	// Discover-by-enumeration: the catalog seam (wired into the GitStore via WithCatalog)
	// enumerates the project repos itself; the per-project head-state reads inside
	// ListProjects mint their own per-project credential through this same minter. Pass the
	// owner's catalog credential (cloud: a token scoped to the installation; local: no-op).
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
func (a *projectStateGitAdapter) catalogCredential(ctx context.Context) (RepoCredential, error) {
	return a.minter.CatalogCredential(ctx)
}

func (a *projectStateGitAdapter) StageArtifactForReview(rc fwra.Context, projectID ProjectID, expectedVersion Version, model ArtifactModel) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.StageArtifactForReview(ctx, projectID, expectedVersion, model, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) CommitArtifact(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.CommitArtifact(ctx, projectID, expectedVersion, kind, cred, rc.IdempotencyKey)
}

// Compile-time proof the git adapter also serves the commit-provenance extension (PM-P2-4):
// the design Managers record committedAt/approvedBy/draftedBy atomically with the commit.
var _ ProvenanceCommitProjectStateAccess = (*projectStateGitAdapter)(nil)

// CommitArtifactWithProvenance is the provenance-recording Commit (PM-P2-4): the cred is
// minted just-in-time, exactly like the no-cred CommitArtifact, and the acting/rail identity
// threaded from the manager approve→commit path is stamped onto the committed slot.
func (a *projectStateGitAdapter) CommitArtifactWithProvenance(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, approvedBy, draftedBy string) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.CommitArtifactWithProvenance(ctx, projectID, expectedVersion, kind, approvedBy, draftedBy, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) RejectArtifact(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, notes string) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RejectArtifact(ctx, projectID, expectedVersion, kind, notes, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) WithdrawArtifact(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, notes string) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.WithdrawArtifact(ctx, projectID, expectedVersion, kind, notes, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) AdvancePhase(rc fwra.Context, projectID ProjectID, expectedVersion Version) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.AdvancePhase(ctx, projectID, expectedVersion, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) SetResearchInput(rc fwra.Context, projectID ProjectID, expectedVersion Version, research ResearchInput) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.SetResearchInput(ctx, projectID, expectedVersion, research, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) SetOperatingModel(rc fwra.Context, projectID ProjectID, expectedVersion Version, model OperatingModel) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.SetOperatingModel(ctx, projectID, expectedVersion, model, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) ReadProject(rc fwra.Context, projectID ProjectID) (Project, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return Project{}, err
	}
	return a.store.ReadProject(ctx, projectID, cred)
}

// ReadProjectVersion serves the cheap version-only read over the git substrate. The git
// head-state read still hydrates the whole project.json blob, but the verb keeps the
// Manager↔RA contract honest: the Temporal Activity returns only the uint64 Version across
// the boundary. Absence stays fwra.NotFound via the underlying ReadProject.
func (a *projectStateGitAdapter) ReadProjectVersion(rc fwra.Context, projectID ProjectID) (Version, error) {
	p, err := a.ReadProject(rc, projectID)
	if err != nil {
		return 0, err
	}
	return p.Version, nil
}

// Compile-time proof the git adapter also serves the branch-aware extension the design
// Managers consume during the AwaitingReview window (I-DESIGN-DISPATCH §2a).
var _ BranchAwareProjectStateAccess = (*projectStateGitAdapter)(nil)

// ReadProjectOnBranch is the branch-aware read-back (I-DESIGN-DISPATCH §2a). An empty
// branch reads the default/main exactly as ReadProject; a non-empty branch reads the
// not-yet-merged draft on the session branch. The cred is minted just-in-time.
func (a *projectStateGitAdapter) ReadProjectOnBranch(ctx context.Context, projectID ProjectID, branch string) (Project, error) {
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return Project{}, err
	}
	return a.store.ReadProjectOnBranch(ctx, projectID, branch, cred)
}

// StageArtifactForReviewOnBranch is the branch-aware AwaitingReview thin-write
// (I-DESIGN-DISPATCH §2a): an empty branch behaves exactly as StageArtifactForReview
// (main); a non-empty branch lands the staged-slot status flip on the session branch the
// draft lives on.
func (a *projectStateGitAdapter) StageArtifactForReviewOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, model ArtifactModel, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.StageArtifactForReviewOnBranch(ctx, projectID, expectedVersion, branch, model, cred, idempotencyKey)
}

// RejectArtifactOnBranch is the branch-aware Reject (I-DESIGN-DISPATCH §2a): an empty
// branch behaves exactly as RejectArtifact (main); a non-empty branch lands the Rejected
// status flip + notes on the session branch the draft was staged on. The cred is minted
// just-in-time.
func (a *projectStateGitAdapter) RejectArtifactOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RejectArtifactOnBranch(ctx, projectID, expectedVersion, branch, kind, notes, cred, idempotencyKey)
}

// WithdrawArtifactOnBranch is the branch-aware Withdraw (I-DESIGN-DISPATCH §2a): an empty
// branch behaves exactly as WithdrawArtifact (main); a non-empty branch lands the Withdrawn
// status flip + notes on the session branch the draft was staged on. The cred is minted
// just-in-time.
func (a *projectStateGitAdapter) WithdrawArtifactOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.WithdrawArtifactOnBranch(ctx, projectID, expectedVersion, branch, kind, notes, cred, idempotencyKey)
}

// Compile-time proof the git adapter also serves the durable review-ledger extension the
// design Managers consume during the AwaitingReview window (review-ledger feature).
var _ LedgerProjectStateAccess = (*projectStateGitAdapter)(nil)

// RejectArtifactOnBranchWithComments is the review-ledger Reject: it lands the Rejected
// status flip + notes AND appends the reviewer's comments to the slot's durable ReviewThread
// in one atomic commit on the session branch (empty branch ⇒ main). The cred is minted
// just-in-time.
func (a *projectStateGitAdapter) RejectArtifactOnBranchWithComments(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, round int64, comments []ReviewComment, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RejectArtifactOnBranchWithComments(ctx, projectID, expectedVersion, branch, kind, notes, round, comments, cred, idempotencyKey)
}

// SeedReviewCommentsOnBranch is the F38 amendment ledger-seed (append open comments, no
// status change). The cred is minted just-in-time, exactly like the other ledger verbs.
func (a *projectStateGitAdapter) SeedReviewCommentsOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, round int64, comments []ReviewComment, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.SeedReviewCommentsOnBranch(ctx, projectID, expectedVersion, branch, kind, round, comments, cred, idempotencyKey)
}

// SetReviewCommentStatusOnBranch applies a human status transition to one ledger entry on
// the session branch (empty branch ⇒ main). The cred is minted just-in-time.
func (a *projectStateGitAdapter) SetReviewCommentStatusOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, commentID string, status string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.SetReviewCommentStatusOnBranch(ctx, projectID, expectedVersion, branch, kind, commentID, status, cred, idempotencyKey)
}

var _ StaleAckProjectStateAccess = (*projectStateGitAdapter)(nil)
var _ ReconcilingProjectStateAccess = (*projectStateGitAdapter)(nil)

// AcknowledgeStaleBasis clears a committed slot's StaleBasis + records the reviewer's
// "reviewed — unaffected" audit entry on main (F45). The cred is minted just-in-time.
func (a *projectStateGitAdapter) AcknowledgeStaleBasis(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, note string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.AcknowledgeStaleBasis(ctx, projectID, expectedVersion, kind, note, cred, idempotencyKey)
}

// ReconcileBranchFromMain is the branch-reconcile verb (F80c): it overlays main's slots
// (bar the session's own) onto the session-branch tip so a diverged PR becomes mergeable.
// The cred is minted just-in-time.
func (a *projectStateGitAdapter) ReconcileBranchFromMain(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.ReconcileBranchFromMain(ctx, projectID, expectedVersion, branch, kind, cred, idempotencyKey)
}

// ---------------------------------------------------------------------------
// RepoLocator — resolves a projectID to a per-project git store handle. It is the seam
// where the deployment profile supplies the concrete URL scheme
// (github.com/<account>/<name>.git in cloud — name-as-identity, C-PA-AD; a file:// path in
// LOCAL). A plain function-backed value, NOT a sibling RA — no sideways edge.
// ---------------------------------------------------------------------------

// gitRepoLocator builds *fwgithub.GitStore handles for per-project repos.
// perProjectRepoURL maps a projectID to its clone URL. The cross-project registry repo is
// GONE (founder ruling 2026-06-14): the catalog is discovered by enumeration.
type gitRepoLocator struct {
	branch            string
	perProjectRepoURL func(projectID ProjectID) string
}

func (l gitRepoLocator) ProjectRepo(projectID ProjectID) (*fwgithub.GitStore, error) {
	url := l.perProjectRepoURL(projectID)
	if url == "" {
		return nil, fwra.New(fwra.ContractMisuse, fmt.Sprintf("gitRepoLocator: empty repo URL for project %s", projectID))
	}
	return fwgithub.NewGitStore(url, l.branch)
}

// ProjectRepoOnBranch satisfies BranchRepoLocator (I-DESIGN-DISPATCH §2a): it binds the
// per-project GitStore handle to a CALLER-SUPPLIED branch instead of the locator's default.
// The design Managers thread the SESSION BRANCH here so the read-back + the AwaitingReview
// thin-write ride over the branch the Action committed the draft on; after merge they pass
// "" (the default ProjectRepo, main).
func (l gitRepoLocator) ProjectRepoOnBranch(projectID ProjectID, branch string) (*fwgithub.GitStore, error) {
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
// ProjectCatalog — the discover-by-enumeration seam ListProjects consumes. The LOCAL
// profile's catalog needs no GitHub and lives here; the CLOUD catalog is a
// sourcecontrol-backed port supplied by the composition root.
// ---------------------------------------------------------------------------

// localProjectCatalog enumerates the LOCAL on-disk-git profile's project repos. In the
// local/embedded profile there is no GitHub installation API; the local substrate is a
// single fixed per-project repo URL (the embedded profile drives ONE project at a time
// through the wedge). The catalog reads the one known repo's project.json head and yields
// its id+title if present. scanRepoURL is the file:// URL of that repo.
type localProjectCatalog struct {
	repoURL string
	branch  string
}

func (c localProjectCatalog) ListProjectRepos(ctx context.Context, _ OwnerScope, cred RepoCredential) ([]ProjectCatalogRef, error) {
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
	// NAME-AS-IDENTITY (C-PM-Δ): the stored id IS the project identity (the repo name),
	// carried verbatim — no uuid.Parse.
	return []ProjectCatalogRef{{ProjectID: ProjectID(doc.ID), Title: doc.Name}}, nil
}
