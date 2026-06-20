package sourcecontrol

// github.go is the concrete GitHub-App-backed implementation of both
// sourceControlAccess contracts. It is the ONLY place this RA speaks GitHub: it
// delegates every wire call to the framework-go-infrastructure-github satellite
// (behind the githubClient seam) and translates between the provider-neutral
// contract value types and the satellite's GitHub-flavoured signatures. No GitHub
// lexeme crosses back across the port; no Temporal is imported; no other RA is
// called.
//
// REPO ADDRESSING (provider-opaque): a RepoRef wraps "account|fullName"
// internally (account = the org login, fullName = owner/repo). Callers treat the
// whole thing as opaque. The package splits it back apart here to drive the
// satellite — the only place owner/repo is a known shape.
//
// TOKEN CACHING (contract #1 §2.3 / D-SC Q4 ruling — PERMITTED, in-seam only):
// GetInstallationToken serves a still-valid cached installation token within THIS
// seam's own process to avoid GitHub's token-mint rate limit. The cache is NEVER
// shared across seams (that would be a covert RA→RA channel); it is a pure
// memoization of "the credential I would mint anyway". A token is treated as
// valid only with a safety margin before its real ExpiresAt.

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	fwgithub "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// workflowPathPrefix is the bounded path-prefix the workflow file must live under.
// Files under it are aiarch-managed (the agentic design workflow).
const workflowPathPrefix = ".github/workflows/"

// scaffoldRootPaths is the exact set of non-workflow repo-root files CommitManagedFiles
// is permitted to seat — the go-test gate scaffold. Together with workflowPathPrefix
// these form the managed-file ALLOWLIST: the verb seats ONLY aiarch-managed files
// (the design workflow + the go.mod and method-test that make `go test ./...` the
// merge gate), never arbitrary content (§2.6, Non-goal #2). A path that is neither
// under .github/workflows/ NOR one of these roots is a ContractMisuse.
var scaffoldRootPaths = map[string]bool{
	"go.mod":                true,
	"aiarch_method_test.go": true,
}

// isManagedFilePath reports whether path is on the managed-file allowlist: under
// .github/workflows/ OR one of the known scaffold roots. This is the single
// gatekeeper that keeps CommitManagedFiles from becoming a "commit any file" smell.
func isManagedFilePath(path string) bool {
	if strings.HasPrefix(path, workflowPathPrefix) {
		return true
	}
	return scaffoldRootPaths[path]
}

// tokenSafetyMargin keeps a cached token from being served when it is within this
// window of expiry — so a consuming seam never gets a credential that expires
// mid-call. The Manager also re-mints before ExpiresAt; this is the in-seam guard.
const tokenSafetyMargin = 60 * time.Second

// repoRefSep separates the account login from the provider full name inside the
// opaque RepoRef. It is an internal encoding detail; callers never see it.
const repoRefSep = "|"

// projectRepoTopic is the GitHub topic every archistrator-ADOPTED project repo
// carries (applied at adopt time, 2026-06-15). It is the SOLE catalog-membership
// signal the discover-by-enumeration ListProjects path filters on (replacing the
// deleted cross-project registry index): a repo with this topic IS an aiarch
// project, and the project id IS the (user-supplied) repo name (name-as-identity).
//
// SENIOR-REVIEW AMENDMENT A1 (§10.1): because adopted repos are USER-NAMED (no
// "aiarch-" prefix), the old "aiarch-" name-prefix defensive fallback in isProjectRepo
// and the TrimPrefix in ProjectRepoRef.ProjectID() are DROPPED — the topic is the
// only membership signal, and the project id is the whole repo name.
const projectRepoTopic = "aiarch-project"

// githubClient is the package-internal seam over the satellite's GitHub-App
// client. It exists so the RA can be unit-tested against a fake without a live
// GitHub, and so the satellite stays the single home of GitHub wire vocabulary.
// The real implementation is *fwgithub.AppClient (adapted in adaptGitHubClient);
// tests substitute a fake.
type githubClient interface {
	FindInstallation(ctx context.Context, account string) (int64, error)
	MintInstallationToken(ctx context.Context, installationID int64) (token string, expiresAt time.Time, err error)
	CreateOrgRepo(ctx context.Context, account, name, instToken string, private bool, opts fwgithub.CreateRepoOptions) (fullName string, alreadyExists bool, err error)
	ListInstallationRepos(ctx context.Context, instToken string) ([]fwgithub.RepoInfo, error)
	SetRepoTopics(ctx context.Context, fullName, instToken string, topics []string) error

	// Adopt back-end: GetRepoMetadata is used ONLY for the reachability check
	// (404 → NotUnderInstallation); the topic/description is applied via SetRepoTopics.
	// (The strict-empty branch-list + .aiarch path-probe primitives were removed by the
	// 2026-06-16 permissive-resume adopt ruling — adopt no longer probes content. The
	// satellite still implements ListRepoBranches/ProbeRepoPathExists; this seam just no
	// longer needs them.)
	GetRepoMetadata(ctx context.Context, fullName, instToken string) (fwgithub.RepoMetadata, error)

	// Agentic-standing back-end (2026-06-15 SC-B; generalized 2026-06-16): seat the
	// managed-file scaffold. CommitManagedFiles drives one PutRepoContentsFile per
	// file in the bundle (design workflow + go-test gate). (The seat-secret primitive
	// was removed in the 2026-06-15 correction — aiarch does no secret management;
	// CLAUDE_CODE_OAUTH_TOKEN is user-provisioned via the Claude Code GitHub App.)
	PutRepoContentsFile(ctx context.Context, fullName, path string, content []byte, message, instToken string) (commitSHA string, changed bool, err error)

	CreateBranch(ctx context.Context, fullName, base, branch, instToken string) (alreadyExists bool, err error)
	OpenPullRequest(ctx context.Context, fullName, head, base, title, body, instToken string) (number int, alreadyExists bool, err error)
	GetPullStatus(ctx context.Context, fullName string, number int, instToken string) (fwgithub.PullStatus, error)
	PostReview(ctx context.Context, fullName string, number int, event, body, instToken string) error
	MergePullRequest(ctx context.Context, fullName string, number int, instToken string) (commit string, alreadyMerged bool, err error)
	ConfigureBranchProtection(ctx context.Context, fullName, branch, appSlug, instToken string) error
}

// *fwgithub.AppClient satisfies githubClient directly (method names + signatures
// match), so the satellite client IS the seam implementation — no adapter needed.
var _ githubClient = (*fwgithub.AppClient)(nil)

// Access is the concrete GitHub-App-backed sourceControlAccess. It implements both
// ISourceControlLifecycle and IPullRequestRail over a single githubClient.
type Access struct {
	client githubClient
	// account is the org login under which repos are provisioned and installations
	// discovered. Provider-opaque to callers (it rides inside AccountRef/RepoRef);
	// the composition root supplies it.
	defaultAccount string
	// appSlug is the App's slug, used as the merge-restriction + bypass actor in
	// ConfigureBranchProtection. A C-SC wiring detail off the contract surface.
	appSlug string
	// repoPrivate selects repo visibility on provision (a provider hint, not a
	// contract field).
	repoPrivate bool
	// now is the clock (overridable in tests for deterministic cache expiry).
	now func() time.Time

	mu         sync.Mutex
	tokenCache map[string]cachedToken // key: RepoRef.String()
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

// compile-time proof Access satisfies both contract faces and the combined port.
var (
	_ SourceControlLifecycle = (*Access)(nil)
	_ PullRequestRail        = (*Access)(nil)
	_ SourceControlAccess    = (*Access)(nil)
)

// New builds the GitHub-App-backed Access. client is the satellite client (or a
// fake in tests); account is the org login; appSlug is the App slug for branch
// protection. It performs no IO.
func New(client githubClient, account, appSlug string, repoPrivate bool) (*Access, error) {
	if client == nil {
		return nil, fwra.New(fwra.ContractMisuse, "New: nil github client")
	}
	if strings.TrimSpace(account) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "New: empty account")
	}
	return &Access{
		client:         client,
		defaultAccount: strings.TrimSpace(account),
		appSlug:        strings.TrimSpace(appSlug),
		repoPrivate:    repoPrivate,
		now:            time.Now,
		tokenCache:     map[string]cachedToken{},
	}, nil
}

// ---------------------------------------------------------------------------
// RepoRef encoding helpers (the only place owner/repo shape is known here).
// ---------------------------------------------------------------------------

func makeRepoRef(account, fullName string) RepoRef {
	return RepoRef{opaque: account + repoRefSep + fullName}
}

// splitRepoRef recovers (account, fullName) from an opaque RepoRef. A malformed
// ref (no separator / empty parts) is a ContractMisuse the caller's verb surfaces.
func splitRepoRef(r RepoRef) (account, fullName string, err error) {
	parts := strings.SplitN(r.opaque, repoRefSep, 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fwra.New(fwra.ContractMisuse, "sourcecontrol: malformed RepoRef")
	}
	return parts[0], parts[1], nil
}

// deterministicRepoName maps a ProjectID to its repo name. Under name-as-identity
// (2026-06-15, A1 §10.1) the project name == the repo name, so this is the IDENTITY
// map (the old "aiarch-"+id prefix is dropped). It is kept as a named function so
// the projectID→repoName re-derivation (RepoRefForProject / the composition root's
// cloudCredentialMinter) stays a single pure mapping the RA owns; the degeneration
// to identity keeps that re-derivation shape-unchanged (no head-state repo-ref
// column is forced — §10.1 Q7 determination).
func deterministicRepoName(p ProjectID) string {
	return string(p)
}

// ---------------------------------------------------------------------------
// Contract #1 — ISourceControlLifecycle.
// ---------------------------------------------------------------------------

// InstallAuthorizeApp discovers/confirms the installation for `account`. NotFound
// (the contract's NotInstalled) surfaces from the satellite when the App is not
// installed. Idempotent on account (pure discovery).
func (a *Access) InstallAuthorizeApp(ctx context.Context, account AccountRef, _ fwra.IdempotencyKey) (Installation, error) {
	acct := a.resolveAccount(account)
	if acct == "" {
		return Installation{}, fwra.New(fwra.ContractMisuse, "InstallAuthorizeApp: empty account")
	}
	id, err := a.client.FindInstallation(ctx, acct)
	if err != nil {
		return Installation{}, err
	}
	return Installation{opaque: itoa64(id)}, nil
}

// AdoptProjectRepo verifies the user's EXISTING repo (spec.RepoName under
// spec.Account) is reachable under the App installation, then tags it
// (aiarch-project topic + spec.Title as description) and returns its RepoRef. It
// REPLACES ProvisionProjectRepo (2026-06-15 agentic pivot): aiarch no longer
// CREATES the repo — the user supplies the repo.
//
// PERMISSIVE-RESUME ADOPT (founder ruling 2026-06-16, REPLACES the strict-empty
// policy). adopt succeeds REGARDLESS of repo content: a README, a claude.yml (from
// the Claude Code GitHub App install), an existing .aiarch/ tree from a prior run —
// all fine. The emptiness probe and the RepoNotEmpty/Conflict hard-fail are GONE.
// "If the repo already has .aiarch/, just re-initialize the project with that repo
// from its current progress" — the RESUME is handled one layer up (the projectState
// CreateProject reads the committed state and returns it). The only real error this
// verb keeps is NotUnderInstallation (the App MUST be installed on the repo):
//
//   - not under the installation        → NotFound  (surfaced "NotUnderInstallation")
//   - under the installation (ANY content) → SUCCESS, apply topic + description
//   - empty RepoName / Account          → ContractMisuse (before any wire call)
//
// Idempotent on the repo name: re-adopting an already-tagged repo re-applies the
// topic/description (converged → effective no-op).
func (a *Access) AdoptProjectRepo(ctx context.Context, spec RepoAdoptionSpec, _ fwra.IdempotencyKey) (RepoRef, error) {
	if strings.TrimSpace(spec.RepoName) == "" {
		return RepoRef{}, fwra.New(fwra.ContractMisuse, "AdoptProjectRepo: empty RepoName")
	}
	acct := a.resolveAccount(spec.Account)
	if acct == "" {
		return RepoRef{}, fwra.New(fwra.ContractMisuse, "AdoptProjectRepo: empty account")
	}
	instToken, err := a.installationTokenForAccount(ctx, acct)
	if err != nil {
		return RepoRef{}, err
	}
	fullName := acct + "/" + spec.RepoName

	// 1. Reachability under the installation — the one real error. GetRepoMetadata 404s
	//    (→ NotFound) when the repo is not reachable by the installation token;
	//    re-surface as the actionable NotUnderInstallation onboarding block.
	if _, err := a.client.GetRepoMetadata(ctx, fullName, instToken); err != nil {
		if kindOfErr(err) == fwra.NotFound {
			return RepoRef{}, fwra.New(fwra.NotFound,
				"AdoptProjectRepo: NotUnderInstallation — repo "+fullName+" is not reachable under the aiarch App installation (install the App on this repo, or move it under the installed org)")
		}
		return RepoRef{}, err
	}

	// 2. Adopt regardless of content: apply the project-title description + the
	//    aiarch-project topic. This is the only mutation. Idempotent: re-applying a
	//    converged topic/description is an effective no-op. The repo's pre-existing
	//    content (README/claude.yml/.aiarch from a prior run) is NOT probed and NOT a
	//    blocker — RESUME (loading any committed project state) is handled by
	//    projectStateAccess.CreateProject, not here.
	if err := a.client.SetRepoTopics(ctx, fullName, instToken, []string{projectRepoTopic}); err != nil {
		return RepoRef{}, err
	}
	return makeRepoRef(acct, fullName), nil
}

// ProjectRepoRef is the discovery row ListProjectRepos returns: the per-project repo
// the catalog read maps to a ProjectSummary. Under name-as-identity (2026-06-15, A1)
// the repo Name IS the project id. It carries the (user-supplied) repo name, the
// repo's full name, and the repo description (the project title set at adopt) +
// topics — provider-NEUTRAL value fields the projectstate RA consumes WITHOUT
// touching GitHub itself.
type ProjectRepoRef struct {
	// Name is the user-supplied repo name == the project id (name-as-identity).
	Name string
	// FullName is owner/name (used to address the repo for the per-project state read).
	FullName string
	// Description is the human title set at adopt (the project name) — lets the
	// catalog render a title without a per-repo project.json read.
	Description string
	// Topics are the repo's topics (carries projectRepoTopic for an aiarch project).
	Topics []string
}

// ProjectID returns the logical project id — the WHOLE repo name (name-as-identity,
// A1 §10.1: the "aiarch-" TrimPrefix is dropped; adopted repos are user-named).
func (r ProjectRepoRef) ProjectID() string {
	return r.Name
}

// ListProjectRepos enumerates the archistrator-managed project repos under `account`
// by listing the GitHub App installation's repositories and filtering to the
// aiarch-project topic (the SOLE membership signal — A1 §10.1 dropped the "aiarch-"
// name-prefix fallback, since adopted repos are user-named). This is the
// discover-by-enumeration catalog seam that REPLACES the
// deleted cross-project registry index: the set of project repos IS the catalog. The
// cred is unused in the cloud path (the installation token is minted in-seam, like
// ProvisionProjectRepo), but is accepted to mirror the provider-neutral cred-threaded
// shape the projectstate RA's other verbs use; a zero cred is permitted here because
// the in-seam mint owns the credential for enumeration.
func (a *Access) ListProjectRepos(ctx context.Context, account AccountRef) ([]ProjectRepoRef, error) {
	acct := a.resolveAccount(account)
	if acct == "" {
		return nil, fwra.New(fwra.ContractMisuse, "ListProjectRepos: empty account")
	}
	instToken, err := a.installationTokenForAccount(ctx, acct)
	if err != nil {
		return nil, err
	}
	repos, err := a.client.ListInstallationRepos(ctx, instToken)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectRepoRef, 0, len(repos))
	for _, r := range repos {
		if !isProjectRepo(r) {
			continue
		}
		out = append(out, ProjectRepoRef{
			Name:        r.Name,
			FullName:    r.FullName,
			Description: r.Description,
			Topics:      r.Topics,
		})
	}
	return out, nil
}

// isProjectRepo decides whether a listed repo is an archistrator project repo: it
// carries the aiarch-project topic. A1 §10.1: the topic is the SOLE membership
// signal (the "aiarch-" name-prefix fallback is dropped — adopted repos are
// user-named, so a name prefix is meaningless).
func isProjectRepo(r fwgithub.RepoInfo) bool {
	for _, tp := range r.Topics {
		if tp == projectRepoTopic {
			return true
		}
	}
	return false
}

// kindOfErr extracts the fwra.Kind from an error (Unknown if not an fwra.Error).
func kindOfErr(err error) fwra.Kind {
	var fe *fwra.Error
	if errors.As(err, &fe) {
		return fe.Kind
	}
	return fwra.Unknown
}

// GetInstallationToken mints (or serves a still-valid in-seam-cached) short-lived
// RepoCredential scoped to `repo`. Returned, never recorded sideways.
func (a *Access) GetInstallationToken(ctx context.Context, repo RepoRef) (RepoCredential, error) {
	if repo.IsZero() {
		return RepoCredential{}, fwra.New(fwra.ContractMisuse, "GetInstallationToken: zero RepoRef")
	}
	acct, _, err := splitRepoRef(repo)
	if err != nil {
		return RepoCredential{}, err
	}

	// In-seam cache (D-SC Q4): serve a still-valid token (with a safety margin).
	if tok, ok := a.cachedToken(repo.String()); ok {
		return RepoCredential{Bytes: []byte(tok.token), ExpiresAt: tok.expiresAt}, nil
	}

	id, err := a.client.FindInstallation(ctx, acct)
	if err != nil {
		return RepoCredential{}, err
	}
	token, expiresAt, err := a.client.MintInstallationToken(ctx, id)
	if err != nil {
		return RepoCredential{}, err
	}
	a.storeToken(repo.String(), cachedToken{token: token, expiresAt: expiresAt})
	return RepoCredential{Bytes: []byte(token), ExpiresAt: expiresAt}, nil
}

// CommitManagedFiles seats the aiarch-MANAGED project scaffold — the
// claude-code-action design workflow PLUS the go-test gate (go.mod +
// aiarch_method_test.go) — at project birth. Every file's path is enforced against
// the managed-file ALLOWLIST (under .github/workflows/, OR a known scaffold root) so
// this verb can never become a general "commit any file" smell (§2.6, Non-goal #2);
// a path off the allowlist is a ContractMisuse.
//
// The bundle is seated by SEQUENTIAL per-file Contents PUTs (each
// overwrite-if-changed: a byte-identical file is a no-op). This trades a single
// atomic Git-tree commit for the existing, well-tested Contents primitive — each PUT
// is independently idempotent, so a retry after a partial seat re-converges every
// file. Files are committed in a STABLE order (sorted by path) for deterministic
// commit history. The returned CommitRef is the LAST file's resulting commit.
func (a *Access) CommitManagedFiles(ctx context.Context, repo RepoRef, files []ManagedFile, cred RepoCredential, _ fwra.IdempotencyKey) (CommitRef, error) {
	_, fullName, err := a.requireRepoCred(repo, cred)
	if err != nil {
		return CommitRef{}, err
	}
	if len(files) == 0 {
		return CommitRef{}, fwra.New(fwra.ContractMisuse, "CommitManagedFiles: empty fileset")
	}

	// Validate the whole set BEFORE any write, so an off-allowlist or empty-content
	// file rejects the bundle atomically at the pre-condition (no partial seat from a
	// bad input). Sort a copy by path for a deterministic commit order.
	ordered := make([]ManagedFile, len(files))
	copy(ordered, files)
	for _, f := range ordered {
		if strings.TrimSpace(f.Path) == "" {
			return CommitRef{}, fwra.New(fwra.ContractMisuse, "CommitManagedFiles: empty path")
		}
		if !isManagedFilePath(f.Path) {
			return CommitRef{}, fwra.New(fwra.ContractMisuse,
				"CommitManagedFiles: path "+f.Path+" is not an aiarch-managed file (must be under "+workflowPathPrefix+" or a scaffold root: go.mod / aiarch_method_test.go)")
		}
		if len(f.Content) == 0 {
			return CommitRef{}, fwra.New(fwra.ContractMisuse, "CommitManagedFiles: empty content for "+f.Path)
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })

	var last CommitRef
	for _, f := range ordered {
		commitSHA, _, perr := a.client.PutRepoContentsFile(ctx, fullName, f.Path, f.Content, ManagedCommitMessage, credStr(cred))
		if perr != nil {
			// A concurrent-write race surfaces as Conflict; per §3 this is the ONE
			// retryable Conflict on this seam (retry-by-re-read). Override Retryable=true.
			if fe := asFwraError(perr); fe != nil && fe.Kind == fwra.Conflict {
				fe.Retryable = true
				return CommitRef{}, fe
			}
			return CommitRef{}, perr
		}
		last = CommitRef{opaque: commitSHA}
	}
	return last, nil
}

// asFwraError returns the underlying *fwra.Error or nil.
func asFwraError(err error) *fwra.Error {
	var fe *fwra.Error
	if errors.As(err, &fe) {
		return fe
	}
	return nil
}

// RepoRefForProject reconstructs the opaque RepoRef of the per-project repo this
// RA would have provisioned for (account, projectID) WITHOUT a wire call — it is a
// pure re-derivation of the SAME deterministic owner/repo encoding ProvisionProjectRepo
// returns ("account|owner/aiarch-<projectID>"). It exists so a repo-driving Manager's
// composition-root wiring can mint a project-scoped credential (GetInstallationToken)
// when it holds only the projectID, never reaching into this RA's private repo-name
// encoding from outside. Idempotency-anchored: the same project always resolves to the
// same ref. Empty account/projectID is a ContractMisuse.
func (a *Access) RepoRefForProject(account AccountRef, projectID ProjectID) (RepoRef, error) {
	acct := a.resolveAccount(account)
	if acct == "" {
		return RepoRef{}, fwra.New(fwra.ContractMisuse, "RepoRefForProject: empty account")
	}
	if strings.TrimSpace(string(projectID)) == "" {
		return RepoRef{}, fwra.New(fwra.ContractMisuse, "RepoRefForProject: empty projectID")
	}
	fullName := acct + "/" + deterministicRepoName(projectID)
	return makeRepoRef(acct, fullName), nil
}

// GetInstallationTokenForProject mints (or serves the in-seam-cached) short-lived
// RepoCredential for the per-project repo of (account, projectID). It is a thin
// convenience over RepoRefForProject + GetInstallationToken: a repo-driving Manager's
// wiring frequently holds the projectID rather than a previously-returned RepoRef
// (e.g. when threading the credential into projectStateAccess on every head-state
// verb), and re-deriving the deterministic ref here keeps that encoding inside the RA.
func (a *Access) GetInstallationTokenForProject(ctx context.Context, account AccountRef, projectID ProjectID) (RepoCredential, error) {
	repo, err := a.RepoRefForProject(account, projectID)
	if err != nil {
		return RepoCredential{}, err
	}
	return a.GetInstallationToken(ctx, repo)
}

// installationTokenForAccount mints an installation token for an account (used by
// the lifecycle write verbs, which are scoped to the account rather than a repo).
// It does not consult the per-repo cache (no RepoRef exists yet at provision time).
func (a *Access) installationTokenForAccount(ctx context.Context, account string) (string, error) {
	id, err := a.client.FindInstallation(ctx, account)
	if err != nil {
		return "", err
	}
	token, _, err := a.client.MintInstallationToken(ctx, id)
	return token, err
}

// GetInstallationTokenForAccount mints a short-lived installation-scoped
// RepoCredential for `account` — a credential covering EVERY repo under the App
// installation, not one repo. It is the credential the catalog enumeration's
// per-project head-state reads run under (ListProjects fans a single installation
// token across all discovered project repos). Unlike the per-repo verbs it is not
// cached (it is not addressed by a RepoRef); the catalog read mints it once per call.
func (a *Access) GetInstallationTokenForAccount(ctx context.Context, account AccountRef) (RepoCredential, error) {
	acct := a.resolveAccount(account)
	if acct == "" {
		return RepoCredential{}, fwra.New(fwra.ContractMisuse, "GetInstallationTokenForAccount: empty account")
	}
	id, err := a.client.FindInstallation(ctx, acct)
	if err != nil {
		return RepoCredential{}, err
	}
	token, expiresAt, err := a.client.MintInstallationToken(ctx, id)
	if err != nil {
		return RepoCredential{}, err
	}
	return RepoCredential{Bytes: []byte(token), ExpiresAt: expiresAt}, nil
}

// ---------------------------------------------------------------------------
// Contract #2 — IPullRequestRail.
// ---------------------------------------------------------------------------

// OpenBranch cuts `branch` from main. An existing branch is an idempotent success.
func (a *Access) OpenBranch(ctx context.Context, repo RepoRef, branch BranchName, cred RepoCredential, _ fwra.IdempotencyKey) (BranchRef, error) {
	_, fullName, err := a.requireRepoCred(repo, cred)
	if err != nil {
		return BranchRef{}, err
	}
	if strings.TrimSpace(string(branch)) == "" {
		return BranchRef{}, fwra.New(fwra.ContractMisuse, "OpenBranch: empty branch")
	}
	if _, err := a.client.CreateBranch(ctx, fullName, "main", string(branch), credStr(cred)); err != nil {
		return BranchRef{}, err
	}
	return BranchRef{opaque: string(branch)}, nil
}

// OpenPullRequest proposes head→base into main. An existing open PR for the
// head→base pair is an idempotent success returning the existing ref.
func (a *Access) OpenPullRequest(ctx context.Context, repo RepoRef, spec PullRequestSpec, cred RepoCredential, _ fwra.IdempotencyKey) (PullRequestRef, error) {
	_, fullName, err := a.requireRepoCred(repo, cred)
	if err != nil {
		return PullRequestRef{}, err
	}
	if strings.TrimSpace(string(spec.Head)) == "" {
		return PullRequestRef{}, fwra.New(fwra.ContractMisuse, "OpenPullRequest: empty head")
	}
	base := string(spec.Base)
	if strings.TrimSpace(base) == "" {
		base = "main"
	}
	if string(spec.Head) == base {
		return PullRequestRef{}, fwra.New(fwra.ContractMisuse, "OpenPullRequest: head == base")
	}
	number, _, err := a.client.OpenPullRequest(ctx, fullName, string(spec.Head), base, spec.Title, spec.Body, credStr(cred))
	if err != nil {
		return PullRequestRef{}, err
	}
	return PullRequestRef{opaque: itoa(number)}, nil
}

// GetPullRequestStatus reads the dumb reflection of CI-green + approvals.
func (a *Access) GetPullRequestStatus(ctx context.Context, repo RepoRef, pr PullRequestRef, cred RepoCredential) (PullRequestStatus, error) {
	_, fullName, err := a.requireRepoCred(repo, cred)
	if err != nil {
		return PullRequestStatus{}, err
	}
	number, err := prNumber(pr)
	if err != nil {
		return PullRequestStatus{}, err
	}
	st, err := a.client.GetPullStatus(ctx, fullName, number, credStr(cred))
	if err != nil {
		return PullRequestStatus{}, err
	}
	return PullRequestStatus{
		CheckRollup:   mapRollup(st.Rollup),
		ApprovalCount: st.ApprovalCount,
		Mergeable:     st.Mergeable,
	}, nil
}

// PostReview relays the in-app human architecture approval as a real PR review.
func (a *Access) PostReview(ctx context.Context, repo RepoRef, pr PullRequestRef, review ReviewSubmission, cred RepoCredential, _ fwra.IdempotencyKey) error {
	_, fullName, err := a.requireRepoCred(repo, cred)
	if err != nil {
		return err
	}
	number, err := prNumber(pr)
	if err != nil {
		return err
	}
	return a.client.PostReview(ctx, fullName, number, reviewEvent(review.Verdict), review.Body, credStr(cred))
}

// MergePullRequest performs the gated merge. The when-to-merge authority is
// interventionEngine; this verb only performs. Already-merged is an idempotent
// success; not-mergeable / conflict surface as Conflict for the Manager to route.
func (a *Access) MergePullRequest(ctx context.Context, repo RepoRef, pr PullRequestRef, cred RepoCredential, _ fwra.IdempotencyKey) (MergeResult, error) {
	_, fullName, err := a.requireRepoCred(repo, cred)
	if err != nil {
		return MergeResult{}, err
	}
	number, err := prNumber(pr)
	if err != nil {
		return MergeResult{}, err
	}
	commit, alreadyMerged, err := a.client.MergePullRequest(ctx, fullName, number, credStr(cred))
	if err != nil {
		return MergeResult{}, err
	}
	if alreadyMerged {
		return MergeResult{Merged: true}, nil
	}
	return MergeResult{Commit: commit, Merged: true}, nil
}

// ConfigureBranchProtection provisions the App-only-merger backstop on main.
func (a *Access) ConfigureBranchProtection(ctx context.Context, repo RepoRef, cred RepoCredential, _ fwra.IdempotencyKey) error {
	_, fullName, err := a.requireRepoCred(repo, cred)
	if err != nil {
		return err
	}
	return a.client.ConfigureBranchProtection(ctx, fullName, "main", a.appSlug, credStr(cred))
}

// ---------------------------------------------------------------------------
// Internal helpers.
// ---------------------------------------------------------------------------

// resolveAccount picks the caller-supplied account or falls back to the
// composition-root default.
func (a *Access) resolveAccount(account AccountRef) string {
	if s := strings.TrimSpace(string(account)); s != "" {
		return s
	}
	return a.defaultAccount
}

// requireRepoCred validates a (repo, cred) pair common to every PR-rail verb and
// returns the decoded (account, fullName).
func (a *Access) requireRepoCred(repo RepoRef, cred RepoCredential) (account, fullName string, err error) {
	if repo.IsZero() {
		return "", "", fwra.New(fwra.ContractMisuse, "sourcecontrol: zero RepoRef")
	}
	if cred.IsZero() {
		return "", "", fwra.New(fwra.ContractMisuse, "sourcecontrol: empty RepoCredential")
	}
	return splitRepoRef(repo)
}

func (a *Access) cachedToken(key string) (cachedToken, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	tok, ok := a.tokenCache[key]
	if !ok {
		return cachedToken{}, false
	}
	if a.now().Add(tokenSafetyMargin).After(tok.expiresAt) {
		delete(a.tokenCache, key)
		return cachedToken{}, false
	}
	return tok, true
}

func (a *Access) storeToken(key string, tok cachedToken) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokenCache[key] = tok
}

func credStr(cred RepoCredential) string { return string(cred.Bytes) }

func mapRollup(r fwgithub.CheckRollup) CheckState {
	switch r {
	case fwgithub.RollupSuccess:
		return CheckSuccess
	case fwgithub.RollupFailure:
		return CheckFailure
	default:
		return CheckPending
	}
}

func reviewEvent(v ReviewVerdict) string {
	switch v {
	case ReviewApprove:
		return "APPROVE"
	case ReviewRequestChanges:
		return "REQUEST_CHANGES"
	default:
		return "COMMENT"
	}
}

func prNumber(pr PullRequestRef) (int, error) {
	if pr.IsZero() {
		return 0, fwra.New(fwra.ContractMisuse, "sourcecontrol: zero PullRequestRef")
	}
	n, err := strconv.Atoi(pr.opaque)
	if err != nil {
		return 0, fwra.New(fwra.ContractMisuse, "sourcecontrol: malformed PullRequestRef")
	}
	return n, nil
}

func itoa(n int) string     { return strconv.Itoa(n) }
func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
