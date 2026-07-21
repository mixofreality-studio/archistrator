// Package sourcecontrol is the sourceControlAccess component of the aiarch
// server's ResourceAccess layer — the PROVIDER-OPAQUE port over the
// GitHub-App-lifecycle volatility (contract #1, ISourceControlLifecycle) and the
// PR-merge-rail face of GitTarget (contract #2, IPullRequestRail). It is the only
// component permitted to perform GitHub-App-lifecycle operations and the
// branch→PR→gate→merge rail (architecture.dsl: the sole sourceControlAccess ->
// github edge).
//
// THE LOAD-BEARING LAYER RULES (sourceControlAccess.md §1/§5,
// sourceControlAccess-pullrequestrail.md §1/§5; [[the-method-layers]]):
//
//   - PROVIDER OPACITY. The public surface carries ZERO GitHub wire/data lexemes
//     (installation_token, ghs_…, installation id, App JWT, owner/repo,
//     workflow_dispatch, /pulls, /merge, required_status_checks). The opaque
//     value types (AccountRef, RepoAdoptionSpec, ManagedFile,
//     Installation, RepoRef, RepoCredential, CommitRef,
//     BranchName/Ref, PullRequestSpec/Ref/Status, MergeResult, ReviewSubmission)
//     wrap the vendor ids; callers never parse them. ALL GitHub vocabulary lives
//     inside the framework-go-infrastructure-github satellite and this package's
//     github.go translation/error-mapping — never on the port.
//
//   - NO RA→RA CALL. getInstallationToken RETURNS a short-lived RepoCredential;
//     it is never stored across seams nor handed to another RA. The calling
//     Manager threads it into the IPullRequestRail verbs (and the GitTarget
//     seams) as a caller-supplied `cred` parameter, exactly as it threads
//     idempotencyKey. This component imports and calls no other ResourceAccess.
//
//   - NO TEMPORAL. Every method is plain Go; the calling Manager wraps each call
//     in a Temporal Activity it owns and chooses retry/timeout there. Errors carry
//     an accurate fwra.Retryable flag (seeded from kind); the component never
//     reads Temporal context.
//
//   - IDEMPOTENCY via deterministic names / desired-state: re-installing,
//     re-provisioning, re-opening a branch/PR, re-merging, and re-applying branch
//     protection are no-op successes. The optional caller-supplied
//     fwra.IdempotencyKey is carried for traceability only.
//
// The concrete GitHub-App-backed implementation (the UNEXPORTED access impl, built by
// the generated NewGitHubSourceControlAccess constructor) lives in github.go; the
// vendor REST/JWT wire code lives in the framework-go-infrastructure-github
// satellite behind the githubClient seam — the ONLY place this RA speaks GitHub.
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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

// workflowPathPrefix is the bounded path-prefix the workflow file must live under.
// Files under it are aiarch-managed (the agentic design workflow).
const workflowPathPrefix = ".github/workflows/"

// claudePathPrefix is the bounded path-prefix of the methodassets prompt surface —
// the .claude/ agents/commands/skills tree (plus the module's seat manifest) the
// managed scaffold seats and the managed-scaffold sync converges (spec §5 amendment,
// 2026-07-13). Files under it are aiarch-managed method-assets content.
const claudePathPrefix = ".claude/"

// scaffoldRootPaths is the exact set of non-workflow repo-root files CommitManagedFiles
// is permitted to seat — the go-test gate scaffold. Together with workflowPathPrefix
// and claudePathPrefix these form the managed-file ALLOWLIST: the verb seats ONLY
// aiarch-managed files (the workflows + the methodassets .claude tree + the go.mod and
// method-test that make `go test ./...` the merge gate + the internal/.gitkeep that
// keeps the method gate's `./internal/...` load pattern from hard-erroring on a fresh
// repo), never arbitrary content (§2.6, Non-goal #2). A path that is neither under
// .github/workflows/ NOR a clean .claude/ path NOR one of these roots is a
// ContractMisuse.
//
// internal/.gitkeep is listed as a LITERAL path (not an internal/ prefix) to keep the
// allowlist tight: only the single seeded placeholder is permitted, never arbitrary
// files under internal/.
//
// .golangci.yml is the shared lint baseline the seated go-checks workflow runs
// against (method-assets scaffold, 2026-07-19); it seats at birth and is server-owned
// policy, so it also rides the sync (see managedSyncFiles).
var scaffoldRootPaths = map[string]bool{
	"go.mod":                true,
	"aiarch_method_test.go": true,
	"internal/.gitkeep":     true,
	".golangci.yml":         true,
}

// isManagedFilePath reports whether p is on the managed-file allowlist: under
// .github/workflows/, a clean path under .claude/ (the methodassets prompt surface),
// OR one of the known scaffold roots. This is the single gatekeeper that keeps
// CommitManagedFiles from becoming a "commit any file" smell.
func isManagedFilePath(p string) bool {
	if strings.HasPrefix(p, workflowPathPrefix) {
		return true
	}
	if isManagedClaudePath(p) {
		return true
	}
	return scaffoldRootPaths[p]
}

// isManagedClaudePath reports whether p is a CLEAN path under the managed .claude/
// tree. Path traversal can never ride the prefix onto the allowlist: the path must
// equal its own path.Clean (so ".claude/../x" — which cleans to "x" — and any
// embedded "a/.." segment are rejected structurally), must not be absolute, and must
// carry no ".." segment (belt-and-braces; Clean already removes them).
func isManagedClaudePath(p string) bool {
	if !strings.HasPrefix(p, claudePathPrefix) {
		return false
	}
	if p != path.Clean(p) || path.IsAbs(p) {
		return false
	}
	return !slices.Contains(strings.Split(p, "/"), "..")
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
	// (404 → NotUnderInstallation); the aiarch-project topic is applied (best-effort)
	// via SetRepoTopics. (No description-set primitive exists on this seam — spec.Title
	// is not written to the repo; see AdoptProjectRepo.)
	// (The strict-empty branch-list + .aiarch path-probe primitives were removed by the
	// 2026-06-16 permissive-resume adopt ruling — adopt no longer probes content. The
	// satellite still implements ListRepoBranches/ProbeRepoPathExists; this seam just no
	// longer needs them.)
	GetRepoMetadata(ctx context.Context, fullName, instToken string) (fwgithub.RepoMetadata, error)

	// Agentic-standing back-end (2026-06-15 SC-B; generalized 2026-06-16; trees-API
	// transport 2026-07-17): seat/sync the managed-file scaffold. The converge COMPARES
	// via ONE recursive tree read (GetRepoTree — the entries carry git blob ids the RA
	// diffs against locally computed fwgithub.GitBlobSHA values, no per-file Contents
	// GET) and WRITES via ONE atomic multi-file commit (CommitFilesAtomic: blobs → tree
	// → commit → unforced ref fast-forward), replacing the old one-Contents-PUT-per-file
	// loop (~100 requests + one commit per drifted file — the App-quota burn). (The
	// seat-secret primitive was removed in the 2026-06-15 correction — aiarch does no
	// secret management; CLAUDE_CODE_OAUTH_TOKEN is user-provisioned via the Claude
	// Code GitHub App.)
	GetRepoTree(ctx context.Context, fullName, ref string, recursive bool, instToken string) (fwgithub.RepoTree, error)
	CommitFilesAtomic(ctx context.Context, fullName, branch, message string, files map[string][]byte, committer fwgithub.CommitSignature, instToken string) (commitSHA string, err error)

	// GetRepoContentsFile reads one file's bytes off the default branch (satellite
	// v0.1.4). It backs the managed-scaffold sync FAST-PATH: read the seated
	// .claude/.method-assets-manifest.json and compare its version against the
	// server's methodassets pin instead of converging every .claude/** file on
	// every design dispatch. A missing file is found=false, NOT an error.
	GetRepoContentsFile(ctx context.Context, fullName, path, instToken string) (content []byte, found bool, err error)

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

// access is the concrete GitHub-App-backed sourceControlAccess. It implements the
// single merged SourceControlAccess port (all ten ops) over a single githubClient.
// It is UNEXPORTED (option-1 generated-DI): the package's only public surface is the
// generated SourceControlAccess interface + models + the generated
// NewGitHubSourceControlAccess constructor (plus the small hand-written catalog/locator
// surface — CatalogAccess + ProjectRepoRef — the projectstate catalog
// consumes, and the established free-function/named-scalar/error-alias exceptions).
type access struct {
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
	// scaffoldSynced memoizes, per repo, the methodassets version whose FULL
	// sync-scoped file set this process has successfully converged (F-QA2-36
	// torn-state guard — see managedSyncMemo). key: RepoRef.String().
	scaffoldSynced map[string]string
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

// compile-time proof access satisfies the merged port + the catalog/locator surface.
var (
	_ SourceControlAccess = (*access)(nil)
	_ CatalogAccess       = (*access)(nil)
)

// newGitHubSourceControlAccess is the hand-written, unexported builder behind the
// generated NewGitHubSourceControlAccess constructor (option-1 delegated DI). client is
// the framework GitHub-App satellite client (*fwgithub.AppClient, which satisfies the
// unexported githubClient seam directly; tests point a real AppClient at a fake GitHub
// HTTP boundary); defaultAccount is the org login; appSlug is the App slug for branch
// protection. It performs no IO and returns the SourceControlAccess interface so the
// concrete impl stays unexported.
func newGitHubSourceControlAccess(client *fwgithub.AppClient, defaultAccount, appSlug string, repoPrivate bool) (SourceControlAccess, error) {
	if client == nil {
		return nil, fwra.New(fwra.ContractMisuse, "NewGitHubSourceControlAccess: nil github client")
	}
	if strings.TrimSpace(defaultAccount) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "NewGitHubSourceControlAccess: empty account")
	}
	return &access{
		client:         client,
		defaultAccount: strings.TrimSpace(defaultAccount),
		appSlug:        strings.TrimSpace(appSlug),
		repoPrivate:    repoPrivate,
		now:            time.Now,
		tokenCache:     map[string]cachedToken{},
		scaffoldSynced: map[string]string{},
	}, nil
}

// ---------------------------------------------------------------------------
// RepoRef encoding helpers (the only place owner/repo shape is known here).
// ---------------------------------------------------------------------------

func makeRepoRef(account, fullName string) RepoRef {
	return RepoRef(account + repoRefSep + fullName)
}

// splitRepoRef recovers (account, fullName) from an opaque RepoRef. A malformed
// ref (no separator / empty parts) is a ContractMisuse the caller's verb surfaces.
func splitRepoRef(r RepoRef) (account, fullName string, err error) {
	parts := strings.SplitN(string(r), repoRefSep, 2)
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

// AppSlug reports the configured GitHub App slug — the Bot actor the seated DESIGN
// workflow allow-lists via allowed_bots (the design workflow is always
// workflow_dispatch'ed by this App, a Bot actor, which claude-code-action refuses
// unless allow-listed). It is OFF the frozen SourceControlAccess contract: a wiring
// detail on the hand-written auxiliary surface, reached by the birth-scaffold seam via
// the package-level RailAppSlug helper. Empty when the App slug is unconfigured (a
// repo-less dev server) — RailAppSlug then yields "" and allowed_bots is omitted.
func (a *access) AppSlug() string {
	return a.appSlug
}

// ---------------------------------------------------------------------------
// Contract #1 — ISourceControlLifecycle.
// ---------------------------------------------------------------------------

// InstallAuthorizeApp discovers/confirms the installation for `account`. NotFound
// (the contract's NotInstalled) surfaces from the satellite when the App is not
// installed. Idempotent on account (pure discovery).
func (a *access) InstallAuthorizeApp(rc fwra.Context, account AccountRef) (Installation, error) {
	// The cross-cutting ctx (and the optional IdempotencyKey, carried for traceability)
	// now ride the ResourceAccess call Context. fwra.Context embeds context.Context;
	// this verb is idempotent on the account, so it reads only ctx here.
	ctx := rc.Context
	acct := a.resolveAccount(account)
	if acct == "" {
		return "", fwra.New(fwra.ContractMisuse, "InstallAuthorizeApp: empty account")
	}
	id, err := a.client.FindInstallation(ctx, acct)
	if err != nil {
		return "", err
	}
	return Installation(itoa64(id)), nil
}

// AdoptProjectRepo verifies the user's EXISTING repo (spec.RepoName under
// spec.Account) is reachable under the App installation, then BEST-EFFORT tags it
// with the aiarch-project topic and returns its RepoRef. It REPLACES
// ProvisionProjectRepo (2026-06-15 agentic pivot): aiarch no longer CREATES the
// repo — the user supplies the repo. (NB: spec.Title is NOT applied as a repo
// description here — this seam has no description-set primitive; the topic is the
// SOLE mutation. The catalog renders the title from the committed project state,
// not from the repo description. The old doc claim "+ spec.Title as description"
// was aspirational and never wired.)
//
// PERMISSIVE-RESUME ADOPT (founder ruling 2026-06-16, REPLACES the strict-empty
// policy). adopt succeeds REGARDLESS of repo content: a README, a claude.yml (from
// the Claude Code GitHub App install), an existing .aiarch/ tree from a prior run —
// all fine. The emptiness probe and the RepoNotEmpty/Conflict hard-fail are GONE.
// "If the repo already has .aiarch/, just re-initialize the project with that repo
// from its current progress" — the RESUME is handled one layer up (the projectState
// CreateProject reads the committed state and returns it).
//
// BEST-EFFORT TOPIC TAGGING (founder ruling 2026-07-04, SUPERSEDES part of the
// 2026-06-16 adopt policy). Onboarding now requires the USER to create the repo and
// install the aiarch App on it FIRST, and the App must NOT be required to hold the
// `administration` permission "for the time being". A GitHub App needs
// administration:write to set repo topics (PUT /repos/{repo}/topics), so on an
// installation carrying only contents:write + metadata:read the topic PUT 403s. That
// 403 must NOT sink the whole adoption (the live failure that motivated this: a valid
// installation, reachable repo, but SetRepoTopics 403 → CreateProject 503'd). So:
//
//   - reachability (GetRepoMetadata) stays a HARD error — the App MUST be installed:
//   - not under the installation        → NotFound  (surfaced "NotUnderInstallation")
//   - topic tagging is BEST-EFFORT:
//   - SetRepoTopics fails with Auth (401/403, missing permission) → WARN + PROCEED
//   - SetRepoTopics fails Transient/Infrastructure/other (real outage) → HARD error
//     (a genuine outage must not be masked as a silent skip)
//   - under the installation (ANY content) → SUCCESS (topic applied, or skipped-with-WARN)
//   - empty RepoName / Account          → ContractMisuse (before any wire call)
//
// EARMARK (temporary): best-effort tagging is a stopgap until the App permission story
// is settled. The consequence is real — cloudProjectCatalog discovers projects BY the
// aiarch-project topic, so a repo whose topic never applied is invisible to catalog
// enumeration. Until the App can be granted administration (or topics move to a
// contents-permission mechanism), onboarding tells the user to add the topic manually
// (see webApp CreateProjectDialog "BEFORE YOU ADOPT").
//
// Idempotent on the repo name: re-adopting an already-tagged repo re-applies the topic
// (converged → effective no-op); a permission-blocked re-adopt is a WARN no-op.
func (a *access) AdoptProjectRepo(rc fwra.Context, spec RepoAdoptionSpec) (RepoRef, error) {
	ctx := rc.Context
	if strings.TrimSpace(spec.RepoName) == "" {
		return "", fwra.New(fwra.ContractMisuse, "AdoptProjectRepo: empty RepoName")
	}
	acct := a.resolveAccount(spec.Account)
	if acct == "" {
		return "", fwra.New(fwra.ContractMisuse, "AdoptProjectRepo: empty account")
	}
	instToken, err := a.installationTokenForAccount(ctx, acct)
	if err != nil {
		return "", err
	}
	fullName := acct + "/" + spec.RepoName

	// 1. Reachability under the installation — the one real error. GetRepoMetadata 404s
	//    (→ NotFound) when the repo is not reachable by the installation token;
	//    re-surface as the actionable NotUnderInstallation onboarding block.
	if _, err := a.client.GetRepoMetadata(ctx, fullName, instToken); err != nil {
		if kindOfErr(err) == fwra.NotFound {
			return "", fwra.New(fwra.NotFound,
				"AdoptProjectRepo: NotUnderInstallation — repo "+fullName+" is not reachable under the aiarch App installation (install the App on this repo, or move it under the installed org)")
		}
		return "", err
	}

	// 2. Adopt regardless of content: BEST-EFFORT apply the aiarch-project topic. This
	//    is the only mutation. Idempotent: re-applying a converged topic is an effective
	//    no-op. The repo's pre-existing content (README/claude.yml/.aiarch from a prior
	//    run) is NOT probed and NOT a blocker — RESUME (loading any committed project
	//    state) is handled by projectStateAccess.CreateProject, not here.
	//
	//    Founder ruling 2026-07-04: the App is NOT required to hold `administration`
	//    for the time being, but setting topics needs administration:write, so this PUT
	//    can 403 (→ fwra.Auth) on a contents+metadata-only installation. Degrade ONLY on
	//    Auth (a permission/credential terminal): WARN and proceed with adoption. Every
	//    other failure — Transient/RateLimited/Infrastructure/Unknown — is a real outage
	//    and stays a HARD error so a genuine failure is never masked as a silent skip.
	if err := a.client.SetRepoTopics(ctx, fullName, instToken, []string{projectRepoTopic}); err != nil {
		if kindOfErr(err) != fwra.Auth {
			return "", err
		}
		slog.WarnContext(ctx,
			"AdoptProjectRepo: topic tagging skipped — App lacks permission to set repo topics (needs administration:write); proceeding with adoption. The user must add the topic manually for cloud catalog discovery",
			"repo", fullName, "topic", projectRepoTopic, "cause", err.Error())
	}
	return makeRepoRef(acct, fullName), nil
}

// CatalogAccess is the small hand-written catalog/locator/token surface the
// projectStateAccess catalog + git-credential minter consume at the COMPOSITION ROOT
// (cmd/server). These ops are deliberately NOT on the merged SourceControlAccess contract:
// ListProjectRepos returns the provider-neutral ProjectRepoRef catalog rows; RepoRefForProject
// is a pure re-derivation of the deterministic per-project repo ref (no IO); the
// GetInstallationToken*For* verbs mint installation-scoped credentials the catalog enumeration
// + per-project git reads run under. They keep their plain context.Context signatures (the
// catalog/minter call them with ctx, not the RA call Context). The unexported access impl
// satisfies this alongside SourceControlAccess — it is the AUXILIARY hand-written public
// surface the option-1 sweep keeps off the frozen 10-op contract (reported, not forced onto
// the generated interface).
type CatalogAccess interface {
	ListProjectRepos(ctx context.Context, account AccountRef) ([]ProjectRepoRef, error)
	RepoRefForProject(account AccountRef, projectID ProjectID) (RepoRef, error)
	GetInstallationTokenForProject(ctx context.Context, account AccountRef, projectID ProjectID) (RepoCredential, error)
	GetInstallationTokenForAccount(ctx context.Context, account AccountRef) (RepoCredential, error)
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
func (a *access) ListProjectRepos(ctx context.Context, account AccountRef) ([]ProjectRepoRef, error) {
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
	return slices.Contains(r.Topics, projectRepoTopic)
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
func (a *access) GetInstallationToken(rc fwra.Context, repo RepoRef) (RepoCredential, error) {
	ctx := rc.Context
	if RepoRefIsZero(repo) {
		return RepoCredential{}, fwra.New(fwra.ContractMisuse, "GetInstallationToken: zero RepoRef")
	}
	acct, _, err := splitRepoRef(repo)
	if err != nil {
		return RepoCredential{}, err
	}

	// In-seam cache (D-SC Q4): serve a still-valid token (with a safety margin).
	if tok, ok := a.cachedToken(RepoRefString(repo)); ok {
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
	a.storeToken(RepoRefString(repo), cachedToken{token: token, expiresAt: expiresAt})
	return RepoCredential{Bytes: []byte(token), ExpiresAt: expiresAt}, nil
}

// CommitManagedFiles seats the aiarch-MANAGED project scaffold — the
// claude-code-action design workflow PLUS the go-test gate (go.mod +
// aiarch_method_test.go) PLUS the internal/.gitkeep placeholder — at project birth.
// Every file's path is enforced against
// the managed-file ALLOWLIST (under .github/workflows/, OR a known scaffold root) so
// this verb can never become a general "commit any file" smell (§2.6, Non-goal #2);
// a path off the allowlist is a ContractMisuse.
//
// TREES-API TRANSPORT (2026-07-17; replaces the sequential per-file Contents-PUT
// loop): the bundle is compared via ONE recursive tree read (blob-SHA diff computed
// locally) and every drifted file lands in ONE atomic git-data commit — see
// putManagedFiles. A bundle already byte-identical on the branch writes nothing.
// The returned CommitRef is the single resulting commit (or the converged tip's
// opaque tree token on a no-op).
func (a *access) CommitManagedFiles(rc fwra.Context, repo RepoRef, files []ManagedFile, cred RepoCredential) (CommitRef, error) {
	ref, _, err := a.putManagedFiles(rc.Context, repo, files, ManagedCommitMessage, cred)
	return ref, err
}

// SyncManagedFiles is the hand-written auxiliary MANAGED-SCAFFOLD SYNC surface
// (2026-07-06), OFF the frozen contract like AppSlug / GetInstallationTokenForProject:
// the same allowlist-guarded, tree-diff + atomic-commit converge as CommitManagedFiles, but
// with a CALLER-SUPPLIED commit message (a sync commit names the file + the pin it
// refreshed to, not the birth-seat message) and an explicit changed report (true ⇔ at
// least one file drifted and a commit was written; false ⇔ byte-identical, no commit).
// Reached via the sourcecontrol.SyncManagedScaffold package helper (managedFileSyncer).
func (a *access) SyncManagedFiles(ctx context.Context, repo RepoRef, files []ManagedFile, message string, cred RepoCredential) (CommitRef, bool, error) {
	if strings.TrimSpace(message) == "" {
		return "", false, fwra.New(fwra.ContractMisuse, "SyncManagedFiles: empty commit message")
	}
	return a.putManagedFiles(ctx, repo, files, message, cred)
}

// ReadManagedFile is the hand-written auxiliary MANAGED-FILE READ surface (B4
// sync fast-path), OFF the frozen contract like SyncManagedFiles / AppSlug: read one
// aiarch-managed file's bytes off the repo's default branch. A missing file is
// found=false (not an error). The SAME allowlist gatekeeper as the write path guards
// it, so this can never become a "read any file" smell. Reached via the
// sourcecontrol.SyncManagedScaffold package helper (managedFileReader), which reads
// the seated .claude/.method-assets-manifest.json to fingerprint the seated .claude
// tree by version instead of converging ~100 files on every design dispatch.
func (a *access) ReadManagedFile(ctx context.Context, repo RepoRef, p string, cred RepoCredential) ([]byte, bool, error) {
	_, fullName, err := a.requireRepoCred(repo, cred)
	if err != nil {
		return nil, false, err
	}
	if !isManagedFilePath(p) {
		return nil, false, fwra.New(fwra.ContractMisuse,
			"ReadManagedFile: path "+p+" is not an aiarch-managed file")
	}
	return a.client.GetRepoContentsFile(ctx, fullName, p, credStr(cred))
}

// ManagedScaffoldSynced / RecordManagedScaffoldSynced implement the managedSyncMemo
// auxiliary surface (F-QA2-36): the per-process record of which repos this access has
// proven current by a FULL sync-set converge, keyed by repo and methodassets version
// (a server upgrade naturally invalidates every entry). Same off-contract discovery
// pattern as SyncManagedFiles / ReadManagedFile / AppSlug.
func (a *access) ManagedScaffoldSynced(repo RepoRef, version string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.scaffoldSynced[RepoRefString(repo)] == version
}

func (a *access) RecordManagedScaffoldSynced(repo RepoRef, version string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.scaffoldSynced[RepoRefString(repo)] = version
}

// managedScaffoldBranch is the branch the managed-scaffold seat/sync converges —
// "main", the rail-wide default-branch assumption this RA already bakes into
// OpenBranch (cuts from main) and ConfigureBranchProtection (protects main).
const managedScaffoldBranch = "main"

// putManagedFiles is the SHARED seat/sync write behind CommitManagedFiles (the frozen
// birth-seat verb, fixed ManagedCommitMessage) and SyncManagedFiles (the auxiliary
// sync, caller message + drift report). One implementation, one allowlist gatekeeper.
//
// TREES-API TRANSPORT (2026-07-17; replaces the fetch-compare-put Contents loop that
// cost ~1 GET + up to 1 PUT/commit PER FILE — the App-quota burn):
//
//   - COMPARE: ONE recursive GetRepoTree of the branch head. The tree entries carry
//     git blob ids, and the expected id of each desired file is computed LOCALLY
//     (fwgithub.GitBlobSHA over the embedded bytes), so the whole diff is in-memory —
//     zero per-file reads. A NotFound tree (unborn branch / fresh repo) means
//     everything drifts. A truncated listing (repos beyond the recursive-tree cap)
//     is not a sound diff base, so every file is treated as drifted — the write is
//     converging and idempotent, so over-writing is always correct, just less lazy.
//   - WRITE: ONE CommitFilesAtomic carrying ALL drifted files — INCLUDING the seat
//     manifest — as a single commit (blobs → tree-on-base → commit → unforced ref
//     fast-forward). TRUE ATOMICITY replaces the old manifest-last ordering
//     (F-QA2-36): nothing is reachable until the final ref update, so an interrupted
//     write leaves the old manifest AND tree fully intact and the next sync retries
//     the whole converge; a torn manifest-over-half-old-tree state is structurally
//     impossible.
//
// A concurrent-writer ref race surfaces as Conflict; per §3 this is the ONE
// retryable Conflict on this seam (retry-by-re-read).
func (a *access) putManagedFiles(ctx context.Context, repo RepoRef, files []ManagedFile, message string, cred RepoCredential) (CommitRef, bool, error) {
	_, fullName, err := a.requireRepoCred(repo, cred)
	if err != nil {
		return "", false, err
	}
	if len(files) == 0 {
		return "", false, fwra.New(fwra.ContractMisuse, "CommitManagedFiles: empty fileset")
	}

	if err := validateManagedFileSet(files); err != nil {
		return "", false, err
	}

	// COMPARE — one tree read; blob-SHA diff computed locally.
	seated := map[string]string{}
	treeSHA := ""
	unsound := false // absent or truncated tree → every file drifts
	tree, terr := a.client.GetRepoTree(ctx, fullName, managedScaffoldBranch, true, credStr(cred))
	switch {
	case terr == nil:
		treeSHA = tree.SHA
		unsound = tree.Truncated
		for _, e := range tree.Entries {
			if e.Type == "blob" {
				seated[e.Path] = e.SHA
			}
		}
	case kindOfErr(terr) == fwra.NotFound:
		unsound = true // unborn branch / fresh repo: nothing seated yet
	default:
		return "", false, terr
	}

	drifted := map[string][]byte{}
	for _, f := range files {
		if unsound || seated[f.Path] != fwgithub.GitBlobSHA(f.Content) {
			drifted[f.Path] = f.Content
		}
	}
	if len(drifted) == 0 {
		// Fully converged — NO write, no commit. The returned CommitRef is the
		// converged tip's tree id: an OPAQUE existing-state token (callers never
		// parse a CommitRef), kept non-zero so idempotent re-seats still return a
		// handle without paying an extra head-resolving request.
		return CommitRef(treeSHA), false, nil
	}

	// WRITE — one atomic commit carrying every drifted file (manifest included).
	commitSHA, cerr := a.client.CommitFilesAtomic(ctx, fullName, managedScaffoldBranch, message, drifted, fwgithub.CommitSignature{}, credStr(cred))
	if cerr != nil {
		if fe := asFwraError(cerr); fe != nil && fe.Kind == fwra.Conflict {
			fe.Retryable = true
			return "", false, fe
		}
		return "", false, cerr
	}
	return CommitRef(commitSHA), true, nil
}

// validateManagedFileSet checks putManagedFiles' whole fileset BEFORE any wire
// call, so an off-allowlist or empty-content file rejects the bundle atomically
// at the pre-condition.
func validateManagedFileSet(files []ManagedFile) error {
	for _, f := range files {
		if strings.TrimSpace(f.Path) == "" {
			return fwra.New(fwra.ContractMisuse, "CommitManagedFiles: empty path")
		}
		if !isManagedFilePath(f.Path) {
			return fwra.New(fwra.ContractMisuse,
				"CommitManagedFiles: path "+f.Path+" is not an aiarch-managed file (must be under "+workflowPathPrefix+", a clean path under "+claudePathPrefix+", or a scaffold root: go.mod / aiarch_method_test.go / internal/.gitkeep)")
		}
		if len(f.Content) == 0 {
			return fwra.New(fwra.ContractMisuse, "CommitManagedFiles: empty content for "+f.Path)
		}
	}
	return nil
}

// SyncManagedScaffold is the CONTRACT op (B5) promoting the former free-function
// composition helper sourcecontrol.SyncManagedScaffold (agenticdesign.go) onto the
// generated SourceControlAccess interface. It converges the seated design workflow
// (aiarch-design.yml on the repo's default branch) onto the CURRENT template
// rendering — the managed-scaffold sync the design Managers run before every
// design-job dispatch: drifted → one refresh commit naming the new state-MCP pin
// (changed=true); already current → no commit (changed=false). The body is a
// LITERAL delegation to the free function, which stays the single owner of the
// converge semantics (template rendering, sync message, drift report): on this
// concrete access the free function's managedFileSyncer type-assertion resolves
// to (*access).SyncManagedFiles and RailAppSlug reads a.appSlug via AppSlug(),
// so the delegation takes the exact optimized path with no duplicated logic to
// drift.
func (a *access) SyncManagedScaffold(rc fwra.Context, repo RepoRef, cred RepoCredential) (bool, error) {
	return SyncManagedScaffold(rc.Context, a, repo, cred)
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
func (a *access) RepoRefForProject(account AccountRef, projectID ProjectID) (RepoRef, error) {
	acct := a.resolveAccount(account)
	if acct == "" {
		return "", fwra.New(fwra.ContractMisuse, "RepoRefForProject: empty account")
	}
	if strings.TrimSpace(string(projectID)) == "" {
		return "", fwra.New(fwra.ContractMisuse, "RepoRefForProject: empty projectID")
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
func (a *access) GetInstallationTokenForProject(ctx context.Context, account AccountRef, projectID ProjectID) (RepoCredential, error) {
	repo, err := a.RepoRefForProject(account, projectID)
	if err != nil {
		return RepoCredential{}, err
	}
	// GetInstallationToken is now an RA-context verb; wrap the plain ctx into the
	// ResourceAccess call Context at this in-RA convenience seam (this helper is not
	// itself a contract op, so it keeps its plain-ctx signature for its callers).
	return a.GetInstallationToken(fwra.Context{Context: ctx}, repo)
}

// installationTokenForAccount mints an installation token for an account (used by
// the lifecycle write verbs, which are scoped to the account rather than a repo).
// It does not consult the per-repo cache (no RepoRef exists yet at provision time).
func (a *access) installationTokenForAccount(ctx context.Context, account string) (string, error) {
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
func (a *access) GetInstallationTokenForAccount(ctx context.Context, account AccountRef) (RepoCredential, error) {
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
func (a *access) OpenBranch(rc fwra.Context, repo RepoRef, branch BranchName, cred RepoCredential) (BranchRef, error) {
	ctx := rc.Context
	_, fullName, err := a.requireRepoCred(repo, cred)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(branch)) == "" {
		return "", fwra.New(fwra.ContractMisuse, "OpenBranch: empty branch")
	}
	if _, err := a.client.CreateBranch(ctx, fullName, "main", string(branch), credStr(cred)); err != nil {
		return "", err
	}
	return BranchRef(string(branch)), nil
}

// OpenPullRequest proposes head→base into main. An existing open PR for the
// head→base pair is an idempotent success returning the existing ref.
func (a *access) OpenPullRequest(rc fwra.Context, repo RepoRef, spec PullRequestSpec, cred RepoCredential) (PullRequestRef, error) {
	ctx := rc.Context
	_, fullName, err := a.requireRepoCred(repo, cred)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(spec.Head)) == "" {
		return "", fwra.New(fwra.ContractMisuse, "OpenPullRequest: empty head")
	}
	base := string(spec.Base)
	if strings.TrimSpace(base) == "" {
		base = "main"
	}
	if string(spec.Head) == base {
		return "", fwra.New(fwra.ContractMisuse, "OpenPullRequest: head == base")
	}
	number, _, err := a.client.OpenPullRequest(ctx, fullName, string(spec.Head), base, spec.Title, spec.Body, credStr(cred))
	if err != nil {
		return "", err
	}
	return PullRequestRef(itoa(number)), nil
}

// GetPullRequestStatus reads the dumb reflection of CI-green + approvals.
func (a *access) GetPullRequestStatus(rc fwra.Context, repo RepoRef, pr PullRequestRef, cred RepoCredential) (PullRequestStatus, error) {
	ctx := rc.Context
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
		ApprovalCount: int64(st.ApprovalCount),
		Mergeable:     st.Mergeable,
	}, nil
}

// PostReview relays the in-app human architecture approval as a real PR review.
//
// SELF-APPROVAL DEGRADE (2026-07-06): when the session PR is authored by the App
// itself (every AMENDMENT PR — the App opens it via OpenPullRequest under its own
// installation token, unlike an initial-phase PR opened by the in-job claude[bot]
// action), GitHub structurally forbids the App from approving its own PR and returns
// 422 → fwra.ContractMisuse. That +1 is pure rail ceremony: the human architecture
// decision already came through the product-gate approval, so a forbidden self-approval
// must NOT be an error — it is a no-op success, mirroring the AdoptProjectRepo
// Auth-degrade above (degrade on ONE terminal kind, hard-error on every other).
//
// The check is deliberately narrowed to the SATELLITE call's ContractMisuse: the local
// requireRepoCred / prNumber validation returns its own ContractMisuse ABOVE this, so a
// genuine bad-argument programmer error still errors; only the wire 422 degrades. Within
// PostReview(APPROVE) with an aiarch-controlled request body, the reviews endpoint's only
// reachable 422 is the self-approval rejection.
//
// DETECTION — why kind, not author or body text: the pinned satellite (fwgithub v0.1.3)
// surfaces no PR author on any read seam (pullRequestDTO / PullStatus carry no user
// login), so an AppSlug()-vs-author comparison is impossible without a coordinated
// platform release; and ClassifyStatus maps the 422 to a bare ContractMisuse, DROPPING
// the GitHub "Can not approve your own pull request" body — so body-text matching is
// impossible too. The ContractMisuse kind is the only signal that reaches the RA.
func (a *access) PostReview(rc fwra.Context, repo RepoRef, pr PullRequestRef, review ReviewSubmission, cred RepoCredential) error {
	ctx := rc.Context
	_, fullName, err := a.requireRepoCred(repo, cred)
	if err != nil {
		return err
	}
	number, err := prNumber(pr)
	if err != nil {
		return err
	}
	if err := a.client.PostReview(ctx, fullName, number, reviewEvent(review.Verdict), review.Body, credStr(cred)); err != nil {
		if kindOfErr(err) != fwra.ContractMisuse {
			return err
		}
		slog.WarnContext(ctx,
			"PostReview: session PR is App-authored — GitHub forbids self-approval; skipping ceremonial +1, product-gate approval stands",
			"repo", fullName, "pr", number, "appSlug", a.appSlug, "cause", err.Error())
	}
	return nil
}

// MergePullRequest performs the gated merge. The when-to-merge authority is
// interventionEngine; this verb only performs. Already-merged is an idempotent
// success; not-mergeable / conflict surface as Conflict for the Manager to route.
func (a *access) MergePullRequest(rc fwra.Context, repo RepoRef, pr PullRequestRef, cred RepoCredential) (MergeResult, error) {
	ctx := rc.Context
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
func (a *access) ConfigureBranchProtection(rc fwra.Context, repo RepoRef, cred RepoCredential) error {
	ctx := rc.Context
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
func (a *access) resolveAccount(account AccountRef) string {
	if s := strings.TrimSpace(string(account)); s != "" {
		return s
	}
	return a.defaultAccount
}

// requireRepoCred validates a (repo, cred) pair common to every PR-rail verb and
// returns the decoded (account, fullName).
func (a *access) requireRepoCred(repo RepoRef, cred RepoCredential) (account, fullName string, err error) {
	if RepoRefIsZero(repo) {
		return "", "", fwra.New(fwra.ContractMisuse, "sourcecontrol: zero RepoRef")
	}
	if RepoCredentialIsZero(cred) {
		return "", "", fwra.New(fwra.ContractMisuse, "sourcecontrol: empty RepoCredential")
	}
	return splitRepoRef(repo)
}

func (a *access) cachedToken(key string) (cachedToken, bool) {
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

func (a *access) storeToken(key string, tok cachedToken) {
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
	case fwgithub.RollupPending: // the zero value
		return CheckPending
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
	case ReviewComment: // non-deciding comment
		return "COMMENT"
	default:
		return "COMMENT"
	}
}

func prNumber(pr PullRequestRef) (int, error) {
	if PullRequestRefIsZero(pr) {
		return 0, fwra.New(fwra.ContractMisuse, "sourcecontrol: zero PullRequestRef")
	}
	n, err := strconv.Atoi(string(pr))
	if err != nil {
		return 0, fwra.New(fwra.ContractMisuse, "sourcecontrol: malformed PullRequestRef")
	}
	return n, nil
}

func itoa(n int) string     { return strconv.Itoa(n) }
func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// agenticdesign.go supplies the aiarch-MANAGED project scaffold archistrator-server
// seats into each user project repo at project birth (CommitManagedFiles). Since the
// B4 delegation the scaffold is RENDERED by the platform method-assets module
// (methodassets.ScaffoldFiles) and wrapped here in the RA's provider-neutral
// ManagedFile value type; it comprises:
//
//   1. .github/workflows/aiarch-design.yml + aiarch-construct.yml — the
//      claude-code-action DESIGN and CONSTRUCTION workflows. They are COMMITTED by
//      the server (not hand-installed) and TEMPLATED with the configured GitHub App
//      slug so the claude-code-action step allow-lists that bot (allowed_bots) —
//      both workflows are ALWAYS workflow_dispatch'ed by the aiarch App (a Bot
//      actor), which the action refuses unless allow-listed. The slug varies per
//      deployment, so it is not hardcoded.
//   2. go.mod — `module <REPO_MODULE>` (github.com/<owner>/<repo>, derived from the
//      adopted RepoRef) + a go directive + `require github.com/mixofreality-studio/archistrator-platform/framework-go`
//      pinned to methodassets.FrameworkGoVersion, so a `go test` in the repo
//      resolves methodcheck.
//   3. aiarch_method_test.go — the single test calling methodcheck.Check (the
//      all-in-one Method gate). Since 2026-07-06 the DESIGN workflow's REQUIRED
//      `validate` job runs `aiarch-state-mcp validate` (the pinned, self-updating
//      binary carrying the same design rules + the staleness-aware cross-artifact
//      severity policy) INSTEAD of this test; the scaffold remains seated for the
//      product repo's OWN CI once it has Go code (arch layer rules + design↔code
//      alignment need go/packages over the product module, which an installed
//      binary cannot run).
//   4. internal/.gitkeep — a placeholder that keeps the internal/ directory PRESENT
//      in a fresh repo. The seated method test (3) runs methodcheck.Check, whose
//      arch.MethodSpec loads the `./internal/...` package pattern; on an empty birth
//      repo that pattern HARD-ERRORS ("lstat ./internal/: no such file or directory")
//      and reddens the required check on every fresh project until the directory is
//      hand-added. Seating an empty-ish internal/.gitkeep makes internal/ exist at
//      birth so the load pattern resolves (to zero packages, a valid no-op) and the
//      gate is green from the first commit. It is a static one-liner (not templated):
//      git needs a non-empty tracked file, and CommitManagedFiles rejects empty
//      content, so it carries a single explanatory comment line.
//   5. (REMOVED from the committed set — runtime materialization, founder-ratified
//      2026-07-17.) The full .claude agents/commands/skills tree + its seat manifest
//      — the PROMPT SURFACE the seated workflows' thin slash-command dispatch routes
//      into — is NO LONGER repo-committed into operated repos. Both seated workflows
//      materialize it into the RUNNER CHECKOUT at job start instead
//      (`aiarch-state-mcp seat-assets --dest .`), from the SAME pinned module
//      generation as the state-MCP binary (StateMcpModulePin is the provenance), so
//      the prompt surface can never drift from the validators. A legacy repo's
//      stale committed .claude copy is force-overwritten by that step (the render
//      runs after checkout and overwrites owned paths); proactive DELETION of
//      legacy committed .claude trees is a deliberate follow-up, not done here.
//
// This asset accessor lives DIRECTLY in the sourceControlAccess package (not a
// sub-package) on purpose: the rendered assets are consumed only by this RA's own
// frozen CommitManagedFiles verb and are wrapped in this RA's own ManagedFile value
// type, so they are part of the sourceControlAccess component, not a peer of it. A
// sub-package would classify as a SECOND ResourceAccess component and its import of
// the ManagedFile type would be a forbidden RA→RA sideways edge (the-method-layers);
// folding it in keeps a single, correctly-classified RA.
//
// It adds NO ResourceAccess verb and speaks NO GitHub wire lexicon: it is a pure
// asset accessor. The COMMIT is performed by the C-PM-Δ caller through the
// already-built CommitManagedFiles verb; the DISPATCH is performed by the design
// Managers (C-MSD-Δ / C-MPD-Δ) through the frozen
// constructionPipelineAccess.SubmitConstructionPipeline verb. The workflow_dispatch
// input names the workflow template declares are a CONTRACT with those Managers —
// see designs/aiarch/implementation/log/C-WF-DESIGN.md.

// The scaffold ASSETS themselves (both workflow templates, the go.mod / method-test
// templates, and the full .claude agents/commands/skills tree) live in the platform
// method-assets module (B4 delegation, 2026-07-13) — this RA no longer embeds or
// renders any template of its own. methodassets.ScaffoldFiles is the single rendering
// both the birth seat and the sync converge against, so the server, the materializer,
// and archistrator's own dogfood repo can never disagree about the target bytes.

// DesignWorkflowPath is the path under .github/workflows/ the DESIGN workflow is
// committed to. It satisfies the managed-file allowlist's .github/workflows/ prefix.
const DesignWorkflowPath = ".github/workflows/aiarch-design.yml"

// scaffoldManifestPath is the seat manifest methodassets.ScaffoldFiles emits alongside
// the .claude tree ({version, files[] sorted}, version == methodassets.Version()). The
// managed-scaffold sync FAST-PATH reads THIS ONE FILE off the repo and compares the
// version — NEVER the files list: a materializer-written manifest may transiently
// carry retained orphans (self-healing prune), so the list is not a stable fingerprint.
// Because it IS the currency fingerprint, putManagedFiles lands it in the SAME single
// atomic commit as every drifted content file (trees-API transport, 2026-07-17 —
// supersedes the F-QA2-36 manifest-last ordering): it can never assert a version whose
// content files did not land with it.
const scaffoldManifestPath = ".claude/.method-assets-manifest.json"

// GoModPath / MethodTestPath are the repo-root scaffold paths the go-test gate is
// seated to. They MUST match the sourcecontrol managed-file allowlist scaffold roots
// (github.go scaffoldRootPaths) so CommitManagedFiles accepts them.
const (
	GoModPath      = "go.mod"
	MethodTestPath = "aiarch_method_test.go"
)

// internalGitkeepPath is the repo-root placeholder that keeps the internal/ directory
// present at project birth so the seated method test's arch.MethodSpec `./internal/...`
// load pattern resolves (to zero packages) instead of hard-erroring on a missing dir.
// It MUST match the sourcecontrol managed-file allowlist scaffold roots (github.go
// scaffoldRootPaths) so CommitManagedFiles accepts it — the allowlist lists this
// LITERAL path, not an internal/ prefix, so it stays tight.
const internalGitkeepPath = "internal/.gitkeep"

// internalGitkeepContent is the static, non-empty content of the internal/.gitkeep
// placeholder. git needs a tracked file (a bare empty directory cannot be committed)
// and CommitManagedFiles rejects empty content, so the file carries a single comment
// line explaining why it exists.
const internalGitkeepContent = "# keeps internal/ present for the Method arch gate (./internal/... load pattern)\n"

// (The former GoVersion / FrameworkGoVersion consts were DELETED with the B4
// delegation: the seated go.mod's language + framework-go pins are owned by the
// method-assets module — methodassets.GoVersion / methodassets.FrameworkGoVersion —
// and version with the assets they template into.)

// StateMcpModulePath is the Go package path of the local stdio project-state MCP server
// the DESIGN workflow launches inside the GitHub Actions job (cmd/aiarch-state-mcp). The
// binary IS ProjectStateAccess code (agentic-managers spec §Construction application): it
// operates on the checked-out working tree and validates every drafted model through the
// SAME projectstate codec + methodcheck the server uses on read-back. It lives in the
// archistrator SERVER module (not framework-go) because it must reuse the strict codec in
// server/internal — a package only that module can import. The workflow obtains it the
// SAME way the seated go.mod scaffold obtains framework-go: `go install <path>@<pin>`
// resolved via GOPROXY (a published module), so it carries the identical trust/access
// profile. Since 2026-07-06 the workflow's REQUIRED `validate` job also runs this
// binary's `validate` subcommand as the Method-invariant PR gate (staleness-aware
// cross-artifact severity — the amendment-deadlock fix), so the gate's rule stack
// self-updates with this pin via the managed-scaffold sync.
const StateMcpModulePath = "github.com/mixofreality-studio/archistrator/server/cmd/aiarch-state-mcp"

// StateMcpModulePin is the version the workflow installs the state-MCP binary at. It must
// be a git ref GOPROXY can resolve for the PUBLIC archistrator repo — a full commit SHA
// (resolved to its pseudo-version), a tag (server/vX.Y.Z → the @vX.Y.Z form here), or a
// branch name.
//
// SOURCE OF TRUTH (managed-scaffold sync, 2026-07-06): this SOURCE CONSTANT is the single
// place the control plane declares which state-MCP binary generation its validators and
// prompts are compatible with. The RELEASE PROCESS updates it when the binary's codec /
// methodcheck rules change, in the same commit that changes them — the two can never
// version independently because they live in one module. It is a `var` (not `const`) so a
// release pipeline may also stamp it at build time via
// `-ldflags "-X .../sourcecontrol.StateMcpModulePin=<sha>"`; the in-source default below
// is what an unstamped build seats and syncs.
//
// A full SHA is pinned (NOT `@main`): GOPROXY caches branch→pseudo-version resolutions,
// so `@main` can silently serve a stale binary for hours — the exact drift class the
// managed-scaffold sync exists to eliminate. Seated workflow copies rendered with an
// older pin are refreshed by the design Managers' sync-on-dispatch (SyncManagedScaffold)
// before every design job, so a seated repo can no longer run against a pin this server's
// validators do not understand.
//
// TOOL-SURFACE COUPLING (F-QA2-23). The materialized method-assets prompts name
// aiarch-state MCP tools the agents must call; the binary installed AT THIS PIN is what
// registers them. When the prompts start referencing a NEW tool, this pin must move to a
// PUSHED commit whose binary registers it — otherwise every design/construction job
// bails on a nonexistent tool (the getCritique incident). The source-tree side is
// enforced by TestPromptSurfaceToolReferencesExistInRegistry
// (cmd/aiarch-state-mcp/promptsurface_test.go): a prompt-referenced tool must exist at
// HEAD, so a correct pin bump to a commit at-or-after the tool's introduction closes the
// skew. TestStateMcpPinIsFullCommitSHA (below, access_test.go) enforces the pin's shape.
var StateMcpModulePin = "14c0db589abc84a034dd6772892a8d0a2b47ff6e"

// NOTE (2026-06-15 correction): the embedded DESIGN workflow reads
// ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }} to authenticate claude-code-action, but that
// Actions secret is provisioned by the Claude Code GitHub App when the USER runs
// /install-github-app on their repo — aiarch does NOT seat it.

// managedScaffoldData maps the RA's repo coordinates onto methodassets.ScaffoldData —
// the SINGLE place the repo→render-context derivation lives. Under name-as-identity
// (A1 §10.1, deterministicRepoName is the identity map) the repo name IS the project
// id AND the display name, so all three fields derive from the one RepoRef.
// StateMcpModulePath / StateMcpModulePin stay SERVER-owned (the pin is
// ldflags-stampable) and are threaded into the module's rendering here.
func managedScaffoldData(repo RepoRef, appSlug string) (methodassets.ScaffoldData, error) {
	owner, name, err := RepoRefOwnerRepo(repo)
	if err != nil {
		return methodassets.ScaffoldData{}, err
	}
	return methodassets.ScaffoldData{
		ModulePath:            fmt.Sprintf("github.com/%s/%s", owner, name),
		AppSlug:               appSlug,
		ProjectID:             name,
		Owner:                 owner,
		Name:                  name,
		StateMcpModulePath:    StateMcpModulePath,
		StateMcpModuleVersion: StateMcpModulePin,
	}, nil
}

// renderManagedScaffold renders the REPO-COMMITTED managed scaffold via
// methodassets.ScaffoldFiles as a path-sorted []ManagedFile: both workflows +
// go.mod + method test + internal/.gitkeep. Two deviations from the module's full
// rendering:
//
//   - the .claude tree + its seat manifest are EXCLUDED (runtime materialization,
//     founder-ratified 2026-07-17): operated repos do not commit the prompt
//     surface — the seated workflows render it into the runner checkout at job
//     start (`aiarch-state-mcp seat-assets --dest .`) from the SAME pinned module
//     generation as the state-MCP binary, so the pin is the provenance and a
//     committed copy cannot drift. (Legacy repos' already-committed .claude trees
//     are left in place — cleanup is a follow-up — and are force-overwritten in
//     the checkout by the workflow's post-checkout seat-assets step.)
//   - the module emits internal/.gitkeep EMPTY (a plain git placeholder), but the
//     managed-file write path rejects empty content, so the seat carries the
//     explanatory one-liner instead — same placeholder, non-empty bytes.
func renderManagedScaffold(repo RepoRef, appSlug string) ([]ManagedFile, error) {
	data, err := managedScaffoldData(repo, appSlug)
	if err != nil {
		return nil, err
	}
	rendered, err := methodassets.ScaffoldFiles(data)
	if err != nil {
		return nil, fmt.Errorf("sourcecontrol: render managed scaffold: %w", err)
	}
	rendered[internalGitkeepPath] = []byte(internalGitkeepContent)

	files := make([]ManagedFile, 0, len(rendered))
	for p, b := range rendered {
		if strings.HasPrefix(p, claudePathPrefix) {
			// Runtime-materialized by the workflows' seat-assets step, never
			// repo-committed (2026-07-17) — includes the seat manifest.
			continue
		}
		files = append(files, ManagedFile{Path: p, Content: b})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// DesignWorkflowFile returns the claude-code-action DESIGN workflow rendered with
// the configured App slug as a provider-neutral ManagedFile — EXACTLY the file the
// birth seat commits (the design workflow templates only the App slug + the
// state-MCP pins, never the repo coordinates, so it renders repo-independently).
// Exported (2026-07-06 managed-scaffold sync) as the stable single-file rendering
// anchor: seat, sync, and tests compare against this one rendering, so they can
// never disagree about the target bytes.
func DesignWorkflowFile(appSlug string) (ManagedFile, error) {
	rendered, err := methodassets.ScaffoldFiles(methodassets.ScaffoldData{
		AppSlug:               appSlug,
		StateMcpModulePath:    StateMcpModulePath,
		StateMcpModuleVersion: StateMcpModulePin,
	})
	if err != nil {
		return ManagedFile{}, fmt.Errorf("sourcecontrol: render design workflow: %w", err)
	}
	content, ok := rendered[DesignWorkflowPath]
	if !ok || len(content) == 0 {
		return ManagedFile{}, fmt.Errorf("sourcecontrol: methodassets scaffold is missing %s", DesignWorkflowPath)
	}
	return ManagedFile{Path: DesignWorkflowPath, Content: content}, nil
}

// ManagedScaffoldFiles returns the FULL aiarch-managed project scaffold bundle to
// seat at project birth: both agentic workflows (design + construct), the go-test
// gate (go.mod + aiarch_method_test.go), and the internal/.gitkeep placeholder (the
// method gate's arch.MethodSpec `./internal/...` load pattern hard-errors on a
// missing internal/ dir, so the placeholder makes it exist at birth). The C-PM-Δ
// caller hands the returned slice to CommitManagedFiles, which seats the whole set
// in one birth seat. (It deliberately does NOT include .aiarch/state/project.json —
// projectStateAccess.CreateProject owns seeding that. Nor, since the 2026-07-17
// runtime-materialization ratification, the .claude prompt surface — the seated
// workflows render it into the runner checkout per job via
// `aiarch-state-mcp seat-assets`; see renderManagedScaffold.)
//
// appSlug is the deployment's GitHub App slug (from the composition root). The caller
// reads it off the rail with RailAppSlug so it is never hardcoded. An EMPTY slug
// (unconfigured dev server) renders a design workflow without allowed_bots (a
// human-dispatched run still works) — but note the CONSTRUCT workflow templates the
// slug unguarded, so construction dispatch requires a configured slug.
//
// An empty/malformed RepoRef (owner/repo unresolvable) is a ContractMisuse the caller
// surfaces — the module path cannot be templated without the repo coordinates.
func ManagedScaffoldFiles(repo RepoRef, appSlug string) ([]ManagedFile, error) {
	return renderManagedScaffold(repo, appSlug)
}

// managedSyncFiles returns the SYNC-SCOPED subset of the managed scaffold — the
// seated workflows plus the .golangci.yml lint baseline. go.mod /
// aiarch_method_test.go / internal/.gitkeep are BIRTH-ONLY: go.mod is
// user-evolved after birth (their requires) and re-seating it would destroy
// user content, so the sync never touches those roots. .golangci.yml is the
// opposite case — server-owned lint policy the seated go-checks workflow runs
// against, with no legitimate app-local edits — so it converges like the
// workflows do. The .claude prompt surface left this set with the 2026-07-17
// runtime-materialization ratification (it left the whole committed scaffold —
// see renderManagedScaffold): the workflows themselves materialize it into the
// runner checkout per job, so there is no committed prompt copy to converge.
func managedSyncFiles(repo RepoRef, appSlug string) ([]ManagedFile, error) {
	all, err := renderManagedScaffold(repo, appSlug)
	if err != nil {
		return nil, err
	}
	out := make([]ManagedFile, 0, len(all))
	for _, f := range all {
		if strings.HasPrefix(f.Path, workflowPathPrefix) || f.Path == ".golangci.yml" {
			out = append(out, f)
		}
	}
	return out, nil
}

// syncManagedScaffoldMessage is the commit message a managed-scaffold SYNC commit
// carries (distinct from the birth-seat ManagedCommitMessage): it names the two
// generations the refreshed rendering carries — the method-assets module version
// (the .claude prompt surface) and the state-MCP pin (the workflows' validator
// binary) — so the repo history records exactly when the scaffold was brought
// current and to which generations.
func syncManagedScaffoldMessage() string {
	return fmt.Sprintf("aiarch: sync managed scaffold to method-assets@%s aiarch-state-mcp@%s",
		methodassets.Version(), StateMcpModulePin)
}

// managedFileSyncer is the OPTIONAL hand-written auxiliary sync surface a concrete
// SourceControlAccess may expose (the GitHub-backed access does — see
// (*access).SyncManagedFiles): the seat write with a caller-supplied commit message
// and an explicit drifted/converged report. Same discovery pattern as RailAppSlug.
type managedFileSyncer interface {
	SyncManagedFiles(ctx context.Context, repo RepoRef, files []ManagedFile, message string, cred RepoCredential) (CommitRef, bool, error)
}

// managedFileReader is the OPTIONAL hand-written auxiliary read surface a concrete
// SourceControlAccess may expose (the GitHub-backed access does — see
// (*access).ReadManagedFile): read one managed file's bytes off the default branch,
// found=false on a missing file. Same discovery pattern as managedFileSyncer; a rail
// without it (a test fake) simply never takes the sync fast-path.
type managedFileReader interface {
	ReadManagedFile(ctx context.Context, repo RepoRef, path string, cred RepoCredential) ([]byte, bool, error)
}

// managedSyncMemo is the OPTIONAL per-process sync-verification memo a concrete
// SourceControlAccess may expose (the GitHub-backed access does — see
// (*access).ManagedScaffoldSynced): has THIS process already converged the FULL
// sync-scoped set for repo at this methodassets version? Same discovery pattern as
// managedFileSyncer / managedFileReader.
//
// WHY (F-QA2-36, torn-state belt-and-braces): the seat manifest carries a version
// and a files list but NO per-file content hashes — so a manifest whose version
// matches cannot, by itself, prove the seated tree's CONTENT. A live repo
// (gtdapp) was torn exactly this way: an interrupted pre-fix sync wrote the manifest
// first, died mid-loop, and every later dispatch's version-only fast-path trusted
// the lying manifest and skipped the half-old tree forever. The memo makes the
// fast-path's trust EARNED per process: the first sync of a repo in a process
// lifetime always runs the full converge (byte-exact verification AND repair in one
// pass — since the trees-API transport, a current tree costs ONE recursive tree
// read, no commits), and only then may later syncs skip the tree on a
// manifest-version hit. A rail without the memo simply never takes the fast path.
// (The 2026-07-17 trees-API transport also made a NEW tear structurally impossible —
// the manifest lands in the same atomic commit as the content — so the memo now
// guards only pre-transport repos and out-of-band tampering.)
type managedSyncMemo interface {
	ManagedScaffoldSynced(repo RepoRef, version string) bool
	RecordManagedScaffoldSynced(repo RepoRef, version string)
}

// scaffoldSyncedThisProcess / recordScaffoldSynced adapt the optional memo surface:
// absent memo ⇒ never verified / record is a no-op.
func scaffoldSyncedThisProcess(rail SourceControlAccess, repo RepoRef) bool {
	m, ok := rail.(managedSyncMemo)
	return ok && m.ManagedScaffoldSynced(repo, methodassets.Version())
}

func recordScaffoldSynced(rail SourceControlAccess, repo RepoRef) {
	if m, ok := rail.(managedSyncMemo); ok {
		m.RecordManagedScaffoldSynced(repo, methodassets.Version())
	}
}

// SyncManagedScaffold ensures the SEATED scaffold — both agentic workflows
// (aiarch-design.yml + aiarch-construct.yml) on the repo's DEFAULT branch — matches
// the CURRENT methodassets rendering. It is the managed-scaffold sync the design
// Managers run BEFORE every design-job dispatch (2026-07-06; closes the
// CreateProject-seats-once drift: the birth seat's constant idempotency key means a
// seated copy was otherwise never refreshed, so server upgrades stranded live repos
// on stale prompts/pins the new validators reject).
//
// SINCE 2026-07-17 (runtime-materialization ratification) the sync set is the TWO
// WORKFLOW FILES ONLY: the .claude prompt surface is no longer repo-committed —
// the seated workflows render it into the runner checkout per job
// (`aiarch-state-mcp seat-assets`), so there is no committed prompt copy to
// converge. The .claude fast-path machinery below (memo + seated-manifest
// fingerprint) is therefore VESTIGIAL — with a workflows-only set both branches
// converge the identical files and the outcome is never wrong, it can only waste
// one manifest GET on legacy repos — and is EARMARKED for removal as a follow-up
// (kept now to avoid churning the converge surface a concurrent transport rework
// owns).
//
// FAST-PATH (B4; hardened for F-QA2-36; kept under the 2026-07-17 trees-API
// transport with UNCHANGED semantics): the sync may skip the .claude tree — but
// only when BOTH proofs hold:
//
//  1. THIS PROCESS has already converged the FULL sync set for this repo at this
//     methodassets version (managedSyncMemo) — byte-exact verification-and-repair,
//     which is what heals a repo torn by a pre-fix interrupted sync whose manifest
//     lies (the gtdapp incident: manifest written first, loop died mid-tree, every
//     version-only fast-path thereafter trusted it and the tear never self-healed);
//     AND
//  2. the seated .claude/.method-assets-manifest.json (managedFileReader) carries
//     exactly this server's methodassets pin — VERSION ONLY, never the files list
//     (a materializer-written manifest may transiently carry retained orphans) —
//     which catches cross-process drift the in-process memo cannot see. Since
//     putManagedFiles lands the manifest in the SAME atomic commit as the content
//     files, an interrupted converge leaves the old (or no) manifest behind and this
//     check misses until the converge completes.
//
// On both proofs the sync converges ONLY the two workflow files: their rendering
// also carries the SERVER-owned (ldflags-stampable) StateMcpModulePin, which the
// module version cannot fingerprint, so the workflows are always converged. Anything
// less — first sync of a process, absent/unreadable/mismatched manifest, or a rail
// without the read surface — takes the FULL seat: every sync-scoped file through the
// same putManagedFiles converge (a current file costs a read, never a commit).
//
// Either way the write is ONE tree-read compare + ONE atomic commit (trees-API
// transport; a byte-identical set short-circuits, no empty commits):
//   - anything drifted → a single commit under the sync message (naming both
//     generations), changed=true
//   - everything identical → NO commit, changed=false (a fast-path hit where the
//     workflows are also current is exactly this)
//
// SCOPE: the prompt surface ONLY (spec §5 amendment, 2026-07-13). The rest of the
// birth scaffold (go.mod / aiarch_method_test.go / internal/.gitkeep) is BIRTH-ONLY —
// go.mod is user-evolved after birth (their requires) and re-seating it would destroy
// user content.
//
// When the concrete rail lacks the auxiliary sync surface (a test fake), it falls back
// to the FROZEN CommitManagedFiles verb — the identical converge semantics under the
// seat message — reporting changed=false (the frozen verb does not report drift).
// A sync error means the seated scaffold could not be proven current; the caller MUST
// fail the dispatch (never dispatch against a known-stale scaffold).
func SyncManagedScaffold(ctx context.Context, rail SourceControlAccess, repo RepoRef, cred RepoCredential) (bool, error) {
	if rail == nil {
		return false, fmt.Errorf("sourcecontrol: SyncManagedScaffold: nil rail")
	}
	files, err := managedSyncFiles(repo, RailAppSlug(rail))
	if err != nil {
		return false, err
	}
	fullSet := true
	// Memo first (free), manifest read second (one API call): a memo miss always
	// takes the full converge, so the manifest read would be wasted.
	if scaffoldSyncedThisProcess(rail, repo) && seatedManifestVersionCurrent(ctx, rail, repo, cred) {
		// Fast path: this process has verified the seated .claude tree byte-exact at
		// this version AND the manifest still fingerprints it current — converge only
		// the workflows (server-owned pins ride in them).
		workflows := files[:0]
		for _, f := range files {
			if strings.HasPrefix(f.Path, workflowPathPrefix) {
				workflows = append(workflows, f)
			}
		}
		files = workflows
		fullSet = false
	}
	if s, ok := rail.(managedFileSyncer); ok {
		_, changed, serr := s.SyncManagedFiles(ctx, repo, files, syncManagedScaffoldMessage(), cred)
		if serr == nil && fullSet {
			recordScaffoldSynced(rail, repo)
		}
		return changed, serr
	}
	_, err = rail.CommitManagedFiles(fwra.Context{Context: ctx}, repo, files, cred)
	if err == nil && fullSet {
		recordScaffoldSynced(rail, repo)
	}
	return false, err
}

// seatedManifestVersionCurrent reports whether the SEATED scaffold manifest can be
// read AND carries exactly this server's methodassets version. Any failure to prove
// currency — a rail without the read surface, a read error, a missing manifest
// (pre-B4 repos), or a corrupt/mismatched document — answers false, and the caller
// converges the full sync set instead (which is always correct, just slower). The
// comparison is VERSION ONLY by design; see SyncManagedScaffold.
func seatedManifestVersionCurrent(ctx context.Context, rail SourceControlAccess, repo RepoRef, cred RepoCredential) bool {
	r, ok := rail.(managedFileReader)
	if !ok {
		return false
	}
	b, found, err := r.ReadManagedFile(ctx, repo, scaffoldManifestPath, cred)
	if err != nil || !found {
		return false
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &m) != nil {
		return false
	}
	return m.Version != "" && m.Version == methodassets.Version()
}

// RailAppSlug reads the configured GitHub App slug off a SourceControlAccess when the
// concrete implementation exposes it (the GitHub-backed access does — see
// (*access).AppSlug). A rail that does not — e.g. a test fake or a repo-less dev
// server (nil rail) — yields "" so the seated design workflow omits allowed_bots (a
// human-dispatched run still works). This keeps the slug's source of truth inside the
// RA: the birth-scaffold caller obtains it FROM the rail rather than threading it
// through the Manager's generated constructor.
func RailAppSlug(rail SourceControlAccess) string {
	if p, ok := rail.(interface{ AppSlug() string }); ok {
		return p.AppSlug()
	}
	return ""
}

// behavior.go carries the FREE-FUNCTION behaviour of the named-scalar / enum /
// struct value types in this component's contract — the established "behavioral
// value type → generated scalar + free functions" pattern (same as
// durableexecution's ExecutionHandle/ExecutionStatus and constructionpipeline's
// PipelineHandle/PipelinePhase/RepoTarget). The opaque-handle value types
// (Installation, RepoRef, CommitRef, BranchRef, PullRequestRef) are generated as
// $def named scalars (contract.gen.go); the RepoCredential struct + the CheckState
// enum are generated as a struct / enum. Their methods would not survive codegen,
// so they live here as free functions the impl + callers call. The opaque token
// the impl packs IS the string value, so the handle behaviour is a thin, parse-free
// pass over that value.

// ---------------------------------------------------------------------------
// Installation behaviour (free functions over the generated named scalar).
// ---------------------------------------------------------------------------

// InstallationString returns the canonical printable form (logs, audit). Replaces
// the former Installation.String() method.
func InstallationString(i Installation) string { return string(i) }

// InstallationIsZero reports whether the handle addresses no installation. Replaces
// the former Installation.IsZero() method.
func InstallationIsZero(i Installation) bool { return i == "" }

// ---------------------------------------------------------------------------
// RepoRef behaviour (free functions over the generated named scalar).
// ---------------------------------------------------------------------------

// RepoRefString returns the canonical printable form.
func RepoRefString(r RepoRef) string { return string(r) }

// RepoRefEqual reports value equality of two repo refs.
func RepoRefEqual(a, b RepoRef) bool { return a == b }

// RepoRefIsZero reports whether the ref addresses no repo.
func RepoRefIsZero(r RepoRef) bool { return r == "" }

// RepoRefFromString reconstructs a RepoRef from the exact RepoRefString form a
// prior AdoptProjectRepo returned (a Manager re-materialising a persisted handle).
// Pure value reconstruction; a malformed ref is rejected by the verb that consumes
// it.
func RepoRefFromString(s string) RepoRef { return RepoRef(s) }

// RepoRefOwnerRepo decodes the RepoRef into its provider owner + repo coordinates —
// the ONLY public accessor of the otherwise-opaque owner/repo encoding. It exists
// so a caller that must address the repo on a DIFFERENT infrastructure port than
// this RA (the per-project-design-dispatch: the constructionPipelineAccess seam
// dispatches the agentic DESIGN job to the per-project repo) can resolve the
// owner/repo WITHOUT re-implementing this RA's private RepoRef encoding. A malformed
// ref is a ContractMisuse the caller surfaces. This is the single seam where
// owner/repo leaves the RA, deliberately scoped to the cross-port dispatch target.
func RepoRefOwnerRepo(r RepoRef) (owner, repo string, err error) {
	_, fullName, serr := splitRepoRef(r)
	if serr != nil {
		return "", "", serr
	}
	o, n, ok := strings.Cut(fullName, "/")
	if !ok || o == "" || n == "" {
		return "", "", fwra.New(fwra.ContractMisuse, "sourcecontrol: RepoRef full name is not owner/repo")
	}
	return o, n, nil
}

// ---------------------------------------------------------------------------
// RepoCredential behaviour (free function over the generated struct).
// ---------------------------------------------------------------------------

// (RepoCredentialIsZero lives in sourcecontrol.go next to the type.)

// ---------------------------------------------------------------------------
// CommitRef behaviour (free functions over the generated named scalar).
// ---------------------------------------------------------------------------

// CommitRefString returns the canonical printable form.
func CommitRefString(c CommitRef) string { return string(c) }

// CommitRefIsZero reports whether the ref addresses no commit.
func CommitRefIsZero(c CommitRef) bool { return c == "" }

// ---------------------------------------------------------------------------
// BranchRef behaviour (free functions over the generated named scalar).
// ---------------------------------------------------------------------------

// BranchRefString returns the canonical printable form.
func BranchRefString(b BranchRef) string { return string(b) }

// BranchRefIsZero reports whether the ref addresses no branch.
func BranchRefIsZero(b BranchRef) bool { return b == "" }

// ---------------------------------------------------------------------------
// PullRequestRef behaviour (free functions over the generated named scalar).
// ---------------------------------------------------------------------------

// PullRequestRefString returns the canonical printable form.
func PullRequestRefString(p PullRequestRef) string { return string(p) }

// PullRequestRefEqual reports value equality of two PR refs.
func PullRequestRefEqual(a, b PullRequestRef) bool { return a == b }

// PullRequestRefIsZero reports whether the ref addresses no PR.
func PullRequestRefIsZero(p PullRequestRef) bool { return p == "" }

// PullRequestRefFromString reconstructs a PullRequestRef from a persisted
// PullRequestRefString form (a Manager re-materialising a handle across an Activity
// boundary).
func PullRequestRefFromString(s string) PullRequestRef { return PullRequestRef(s) }

// ---------------------------------------------------------------------------
// CheckState behaviour (free function over the generated enum).
// ---------------------------------------------------------------------------

var checkStateNames = map[CheckState]string{
	CheckPending: "Pending", CheckSuccess: "Success", CheckFailure: "Failure",
}

// CheckStateString returns the stable name (logs, audit). A free function
// because the generated contract type carries no methods.
func CheckStateString(s CheckState) string {
	if n, ok := checkStateNames[s]; ok {
		return n
	}
	return "Pending"
}

// ManagedCommitMessage is the commit message CommitManagedFiles uses when it seats
// the managed-file bundle at project birth. One bundle, one message.
const ManagedCommitMessage = "chore(aiarch): seat aiarch-managed project scaffold (design workflow + go-test gate)"

// RepoCredential.Bytes is the opaque bearer secret; every consuming seam presents
// it, never parses it, and treats it as write-only (never logged/persisted).

// RepoCredentialIsZero reports whether the credential is empty.
func RepoCredentialIsZero(c RepoCredential) bool { return len(c.Bytes) == 0 }

// Error is the shared ResourceAccess error model (framework-go), re-exported as an
// alias so this component reads in its own terms while every RA shares one fixed
// enum. The contract's logical error vocabulary maps onto the shared kinds:
//
//   - Transient      → fwra.Transient        (retryable: github 5xx / network blip / rate-limit)
//   - Auth           → fwra.Auth             (terminal: App JWT rejected / installation revoked /
//     insufficient permission — incl. missing contents:write)
//   - NotFound       → fwra.NotFound         (terminal: app not installed / repo not under the installation
//     ["NotUnderInstallation"] / unknown repo|branch|PR)
//   - Conflict       → fwra.Conflict         (the merge rail's not-mergeable; the ONE retryable Conflict is
//     commitManagedFiles' concurrent-write race, Retryable overridden true. adoptProjectRepo never
//     returns Conflict — adopt succeeds regardless of repo content.)
//   - ContractMisuse → fwra.ContractMisuse   (terminal: empty AccountRef/RepoName/RepoRef / empty fileset /
//     a managed-file path off the allowlist (.github/workflows/ or a scaffold root) / zero cred / bad input)
//
// AdoptProjectRepo maps only "repo not reachable under the installation" to
// NotFound (surfaced as "NotUnderInstallation", terminal); an existing
// .aiarch/state is RESUMED by projectStateAccess.createProject. AlreadyMerged
// (mergePullRequest) and the PR-rail already-exists (openBranch / openPullRequest)
// map to SUCCESS inside the seam (framework-go has no AlreadyExists kind — the
// idempotent-success path returns the existing handle).
type Error = fwra.Error

// variant.go holds the deployment VARIANT CONSTRUCTOR for sourceControlAccess —
// the composition-root policy that used to live in cmd/server (buildSourceControl)
// folded into the owning package. The shared *fwgithub.AppClient satellite stays
// OUTSIDE (built once at the composition root and shared with
// constructionPipeline / artifactAccess); the variant takes it in.
//
// NewGitHubSourceControl returns BOTH published surfaces the composition root wires:
//   - CatalogAccess: the catalog/locator/token surface the projectStateAccess
//     git cred minter + catalog (CLOUD profile) consume;
//   - SourceControlAccess: the generated interface the design Managers' adapters + the
//     PR-rail consume.
//
// The unexported impl satisfies both, so the catalog surface is a type-assertion of the
// generated interface — folded here so the assertion is no longer a caller concern.

// NewGitHubSourceControl builds the GitHub-App-backed sourceControlAccess over the shared
// *fwgithub.AppClient and returns both published surfaces (catalog + generated interface).
func NewGitHubSourceControl(client *fwgithub.AppClient, account, appSlug string, repoPrivate bool) (CatalogAccess, SourceControlAccess, error) {
	scAccess, err := NewGitHubSourceControlAccess(client, account, appSlug, repoPrivate)
	if err != nil {
		return nil, nil, err
	}
	scConcrete := scAccess.(CatalogAccess)
	return scConcrete, scAccess, nil
}

// ===========================================================================
// GITLOCAL REALISATION (F-R3 local design PR rail). Logically its own file
// (gitlocalsourcecontrol.go) but kept in this component's single impl file per the
// layer file-layout standard (one <component>access.go). See its header below.
// ===========================================================================

// gitlocalsourcecontrol.go is the LOCAL-GIT realisation of the SourceControlAccess port
// (F-R3 local design PR rail) — the THIRD arm alongside the GitHub-backed realisation
// (sourcecontrolaccess.go) and any dry-run stub. It backs the design managers' branch /
// PR / review / merge lifecycle when the project runs on the LOCAL profile (a file:// bare
// repo, the SAME one the projectstate GitStore + the constructionpipeline local executor
// operate on), with NO GitHub App, NO network, and NO PR store.
//
// WHY IT EXISTS: on the local profile the cloud PR rail is unavailable (no GitHub App,
// no Actions, no PR objects), yet the design spine still wants a branch-backed session
// lifecycle — draft onto a session branch, gate on checks+mergeable, then merge to main.
// This realisation provides that entirely over LOCAL git mechanics (throwaway clone +
// push + merge --no-ff + branch delete), the SAME primitives constructionpipeline's
// mergeActivityBranch + the projectstate GitStore already use. A local "PR" is not a
// stored object — it is a pure git construct (a head branch proposed into main), so the
// PullRequestRef deterministically ENCODES the head branch ("local#<head>") and every
// PR verb re-derives the head from it; there is nothing to persist.
//
// OP-BY-OP LOCAL SEMANTICS (see each method):
//   - GetInstallationToken → a static dummy credential (never a real token; every verb
//     ignores cred — auth is the local filesystem).
//   - OpenBranch → create the branch off main if missing; idempotent no-op re-open.
//   - OpenPullRequest → return "local#<head>" (deterministic, idempotent on head).
//   - GetPullRequestStatus → CheckSuccess unconditionally (no local CI; the design job's
//     validators already ran in-process during drafting) + a REAL Mergeable computed by a
//     trial merge in a throwaway clone; ApprovalCount 0 (mergeOnApprove reads
//     CheckGreen+Mergeable only — coauthorartifact.go).
//   - PostReview → no-op success (the +1 is audit ceremony; provenance rides
//     CommitArtifactWithProvenance on main).
//   - MergePullRequest → merge head→main --no-ff, push, delete the branch; already-merged
//     is delete-only idempotent re-entry; a conflict is an honest fwra.Conflict with
//     NOTHING pushed (the manager's F80c reconcile + re-approve path then applies).
//   - SyncManagedScaffold → (false, nil) no-op (the local executor seats .claude itself).
//   - Birth ops (CreateProject, rail non-nil): AdoptProjectRepo returns the deterministic
//     local RepoRef (the shared repo already exists); CommitManagedFiles /
//     ConfigureBranchProtection are no-ops; InstallAuthorizeApp is unreachable
//     (ContractMisuse) — there is no GitHub App funnel locally.
//
// DUPLICATED GIT HELPERS: the tiny `git` subprocess plumbing below is deliberately
// re-implemented rather than shared with constructionpipeline (an RA may not import a
// sibling RA — NoSideways), the SAME accepted-duplication precedent that file's own
// header cites.

const (
	// gitLocalMainBranch is the default branch every session branch forks from and merges
	// into — the SAME "main" convention the GitStore + local executor fix.
	gitLocalMainBranch = "main"
	// gitLocalAccount is the synthetic account login stamped into the local RepoRef
	// ("local|local/<projectID>"), so the ref decodes cleanly through RepoRefOwnerRepo /
	// designRepoTarget exactly as a GitHub "owner|owner/repo" ref does.
	gitLocalAccount = "local"
	// gitLocalPRPrefix encodes a local "PR" as its head branch: "local#<head>". A local PR
	// has no stored object, so this deterministic ref IS the PR — idempotent on head, and
	// every PR verb re-derives the head from it.
	gitLocalPRPrefix = "local#"
	// gitLocalCredentialToken is the opaque bytes of the dummy credential
	// GetInstallationToken returns. It is never presented to any real service — local git
	// auth is the filesystem — but must be non-empty so RepoCredentialIsZero is false.
	gitLocalCredentialToken = "local"
	// gitLocalScaffoldSentinel is the sentinel CommitRef CommitManagedFiles returns: the
	// local executor seats the managed scaffold (.claude) itself, so there is nothing to
	// commit here, but the contract wants a non-zero CommitRef.
	gitLocalScaffoldSentinel = "local-scaffold-noop"
)

// gitLocalFarExpiry is the ExpiresAt on the dummy credential — deliberately far in the
// future so the spine's token-refresh check never fires (the local credential never
// expires; there is nothing to re-mint).
var gitLocalFarExpiry = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

// gitLocalAccess is the concrete local-git SourceControlAccess. It holds only the shared
// repo URL and a mutex serialising this instance's own throwaway-clone git operations
// against that repo (V1 assumes one local developer driving one session at a time — the
// same scope constructionpipeline's localExecAccess documents).
type gitLocalAccess struct {
	repoURL string
	gitMu   sync.Mutex
}

var _ SourceControlAccess = (*gitLocalAccess)(nil)

// NewGitLocalSourceControlAccess builds the local-git SourceControlAccess over the shared
// repo at repoURL (a file:// bare repo or plain path — the SAME value the projectstate
// GitStore + the constructionpipeline local executor are configured with). Like the other
// GitLocal* variant constructors (projectstate.NewGitLocalProjectStateAccess) it panics on
// a misconfiguration the composition root cannot recover from (empty repoURL), rather than
// threading an error through the variant-constructor signature.
func NewGitLocalSourceControlAccess(repoURL string) SourceControlAccess {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		panic("sourcecontrol.NewGitLocalSourceControlAccess: empty repoURL")
	}
	return &gitLocalAccess{repoURL: repoURL}
}

// GitLocalRepoRefForProject mints the deterministic local RepoRef for a project —
// "local|local/<projectID>" (account "local", full name "local/<projectID>"). It is a PURE
// resolver (no IO) so the composition-root Repo resolvers can derive the ref for a project
// without an AdoptProjectRepo round-trip, and it decodes cleanly through RepoRefOwnerRepo →
// (owner "local", repo "<projectID>") exactly as designRepoTarget expects.
func GitLocalRepoRefForProject(projectID ProjectID) RepoRef {
	return makeRepoRef(gitLocalAccount, gitLocalAccount+"/"+string(projectID))
}

// ---------------------------------------------------------------------------
// Credential + birth ops.
// ---------------------------------------------------------------------------

// GetInstallationToken returns a STATIC dummy credential. There is no installation and no
// token minting locally — auth is the filesystem — so this hands back non-empty opaque
// bytes with a far-future expiry, which every verb below ignores.
func (a *gitLocalAccess) GetInstallationToken(_ fwra.Context, repo RepoRef) (RepoCredential, error) {
	if RepoRefIsZero(repo) {
		return RepoCredential{}, fwra.New(fwra.ContractMisuse, "gitlocal GetInstallationToken: zero RepoRef")
	}
	return RepoCredential{Bytes: []byte(gitLocalCredentialToken), ExpiresAt: gitLocalFarExpiry}, nil
}

// AdoptProjectRepo returns the deterministic local RepoRef for the project. The shared
// repo already EXISTS (it is the configured local repo the GitStore/executor use), so there
// is nothing to create or probe — only the name-as-identity RepoName (== projectID) is
// validated and mapped to the ref GitLocalRepoRefForProject would mint.
func (a *gitLocalAccess) AdoptProjectRepo(_ fwra.Context, spec RepoAdoptionSpec) (RepoRef, error) {
	name := strings.TrimSpace(spec.RepoName)
	if name == "" {
		return "", fwra.New(fwra.ContractMisuse, "gitlocal AdoptProjectRepo: empty RepoName")
	}
	return GitLocalRepoRefForProject(ProjectID(name)), nil
}

// CommitManagedFiles is a no-op success returning a sentinel CommitRef. The managed
// scaffold (.claude prompt surface) is seated by the constructionpipeline local executor at
// dispatch time, not committed to the repo, so there is nothing to commit here.
func (a *gitLocalAccess) CommitManagedFiles(_ fwra.Context, repo RepoRef, _ []ManagedFile, _ RepoCredential) (CommitRef, error) {
	if RepoRefIsZero(repo) {
		return "", fwra.New(fwra.ContractMisuse, "gitlocal CommitManagedFiles: zero RepoRef")
	}
	return CommitRef(gitLocalScaffoldSentinel), nil
}

// ConfigureBranchProtection is a no-op success: there is no hosted branch-protection to
// provision on a local repo (the App-only-merger backstop is a GitHub construct).
func (a *gitLocalAccess) ConfigureBranchProtection(_ fwra.Context, repo RepoRef, _ RepoCredential) error {
	if RepoRefIsZero(repo) {
		return fwra.New(fwra.ContractMisuse, "gitlocal ConfigureBranchProtection: zero RepoRef")
	}
	return nil
}

// InstallAuthorizeApp is unreachable on the local profile: the App-installation funnel is a
// GitHub-only onboarding step. Reaching it is a wiring bug, surfaced as ContractMisuse.
func (a *gitLocalAccess) InstallAuthorizeApp(_ fwra.Context, _ AccountRef) (Installation, error) {
	return "", fwra.New(fwra.ContractMisuse, "gitlocal InstallAuthorizeApp: unreachable on the local profile (no GitHub App funnel)")
}

// SyncManagedScaffold is a no-op reporting NO drift/commit: the local executor materialises
// the .claude prompt surface itself (seat-assets), so there is no seated workflow file to
// converge here.
func (a *gitLocalAccess) SyncManagedScaffold(_ fwra.Context, repo RepoRef, _ RepoCredential) (bool, error) {
	if RepoRefIsZero(repo) {
		return false, fwra.New(fwra.ContractMisuse, "gitlocal SyncManagedScaffold: zero RepoRef")
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// Branch / PR / review / merge lifecycle (over local git).
// ---------------------------------------------------------------------------

// OpenBranch creates branch off main if it does not yet exist; a re-open of an existing
// branch is an idempotent no-op success (mirroring the GitHub CreateBranch semantics the
// spine relies on). Performed in a throwaway clone: the branch ref is created at main's tip
// and pushed to the shared repo.
func (a *gitLocalAccess) OpenBranch(_ fwra.Context, repo RepoRef, branch BranchName, _ RepoCredential) (BranchRef, error) {
	if RepoRefIsZero(repo) {
		return "", fwra.New(fwra.ContractMisuse, "gitlocal OpenBranch: zero RepoRef")
	}
	b := strings.TrimSpace(string(branch))
	if b == "" {
		return "", fwra.New(fwra.ContractMisuse, "gitlocal OpenBranch: empty branch")
	}
	a.gitMu.Lock()
	defer a.gitMu.Unlock()

	cloneDir, cleanup, err := a.cloneMain()
	if err != nil {
		return "", err
	}
	defer cleanup()

	// Idempotent: the branch already exists in the shared repo → no-op success.
	if _, verr := gitLocalRun(cloneDir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+b); verr == nil {
		return BranchRef(b), nil
	}
	// Create off main: the clone's HEAD is origin/main's tip; push it to the new ref.
	if out, perr := gitLocalRun(cloneDir, "push", "origin", "HEAD:refs/heads/"+b); perr != nil {
		return "", fwra.Wrap(fwra.Infrastructure, perr, "gitlocal OpenBranch: create branch "+b+" failed: "+strings.TrimSpace(out))
	}
	return BranchRef(b), nil
}

// OpenPullRequest returns a deterministic local PR ref encoding the head branch
// ("local#<head>"). A local PR has no stored object — it is the head branch proposed into
// base — so this is pure and idempotent on head; the title/body are LOGGED (audit), never
// persisted.
func (a *gitLocalAccess) OpenPullRequest(rc fwra.Context, repo RepoRef, spec PullRequestSpec, _ RepoCredential) (PullRequestRef, error) {
	if RepoRefIsZero(repo) {
		return "", fwra.New(fwra.ContractMisuse, "gitlocal OpenPullRequest: zero RepoRef")
	}
	head := strings.TrimSpace(string(spec.Head))
	if head == "" {
		return "", fwra.New(fwra.ContractMisuse, "gitlocal OpenPullRequest: empty head")
	}
	base := strings.TrimSpace(string(spec.Base))
	if base == "" {
		base = gitLocalMainBranch
	}
	if head == base {
		return "", fwra.New(fwra.ContractMisuse, "gitlocal OpenPullRequest: head == base")
	}
	slog.InfoContext(logCtx(rc), "gitlocal OpenPullRequest (local PR = head branch; no PR object stored)",
		"repo", RepoRefString(repo), "head", head, "base", base, "title", spec.Title)
	return PullRequestRefFromString(gitLocalPRPrefix + head), nil
}

// GetPullRequestStatus reports the local rail's gate inputs. CheckRollup is CheckSuccess
// UNCONDITIONALLY: there is no local CI, and the design job's validators (putDraftModel's
// in-loop codec+methodcheck, and the aiarch-state-mcp `validate` the executor runs) already
// gated the draft IN-PROCESS before this read — so the required-check trust boundary is
// satisfied without a hosted check run. Mergeable is REAL, computed by a trial merge of the
// head into main in a throwaway clone. ApprovalCount is 0 (mergeOnApprove reads
// CheckGreen+Mergeable only; the approval +1 is ceremony relayed via PostReview).
func (a *gitLocalAccess) GetPullRequestStatus(_ fwra.Context, repo RepoRef, pr PullRequestRef, _ RepoCredential) (PullRequestStatus, error) {
	if RepoRefIsZero(repo) {
		return PullRequestStatus{}, fwra.New(fwra.ContractMisuse, "gitlocal GetPullRequestStatus: zero RepoRef")
	}
	head, err := gitLocalPRHead(pr)
	if err != nil {
		return PullRequestStatus{}, err
	}
	a.gitMu.Lock()
	defer a.gitMu.Unlock()
	mergeable, err := a.trialMerge(head)
	if err != nil {
		return PullRequestStatus{}, err
	}
	return PullRequestStatus{CheckRollup: CheckSuccess, ApprovalCount: 0, Mergeable: mergeable}, nil
}

// PostReview is a no-op success. The human architecture approval already came through the
// product gate; the PR-review +1 is pure ceremony/audit relay with no local PR object to
// attach it to, and the durable provenance rides CommitArtifactWithProvenance on main.
func (a *gitLocalAccess) PostReview(_ fwra.Context, repo RepoRef, _ PullRequestRef, _ ReviewSubmission, _ RepoCredential) error {
	if RepoRefIsZero(repo) {
		return fwra.New(fwra.ContractMisuse, "gitlocal PostReview: zero RepoRef")
	}
	return nil
}

// MergePullRequest merges the PR's head branch into main with a real --no-ff merge commit
// and deletes the branch, in a throwaway clone + push. Already-merged (or already
// merged-and-deleted) is delete-only idempotent re-entry. A conflict is an honest
// fwra.Conflict with NOTHING pushed — the shared repo is untouched, so the manager's F80c
// reconcileDivergedBranch + re-approve path applies unchanged.
func (a *gitLocalAccess) MergePullRequest(_ fwra.Context, repo RepoRef, pr PullRequestRef, _ RepoCredential) (MergeResult, error) {
	if RepoRefIsZero(repo) {
		return MergeResult{}, fwra.New(fwra.ContractMisuse, "gitlocal MergePullRequest: zero RepoRef")
	}
	head, err := gitLocalPRHead(pr)
	if err != nil {
		return MergeResult{}, err
	}
	a.gitMu.Lock()
	defer a.gitMu.Unlock()
	commit, err := a.mergeHeadIntoMain(head)
	if err != nil {
		return MergeResult{}, err
	}
	return MergeResult{Commit: commit, Merged: true}, nil
}

// ---------------------------------------------------------------------------
// git plumbing (throwaway-clone + push mechanics — duplicated per NoSideways;
// mirrors constructionpipeline.mergeActivityBranch's style).
// ---------------------------------------------------------------------------

// cloneMain clones the shared repo's main into a throwaway temp dir and returns the clone
// dir + a cleanup func removing the whole temp tree. A clone failure (repo absent / no
// main) is an Infrastructure error.
func (a *gitLocalAccess) cloneMain() (cloneDir string, cleanup func(), err error) {
	parentDir, mkErr := os.MkdirTemp("", "aiarch-scm-*")
	if mkErr != nil {
		return "", func() {}, fwra.Wrap(fwra.Infrastructure, mkErr, "gitlocal: create work dir")
	}
	cleanup = func() { _ = os.RemoveAll(parentDir) }
	cloneDir = filepath.Join(parentDir, "clone")
	if out, cErr := gitLocalRun("", "clone", "--branch", gitLocalMainBranch, a.repoURL, cloneDir); cErr != nil {
		cleanup()
		return "", func() {}, fwra.Wrap(fwra.Infrastructure, cErr, "gitlocal: clone failed: "+strings.TrimSpace(out))
	}
	return cloneDir, cleanup, nil
}

// trialMerge reports whether head merges cleanly into main, computed by a --no-commit
// trial merge in a throwaway clone (aborted either way — the clone is discarded). A head
// already merged into main is trivially mergeable; a head that no longer exists (merged +
// deleted, or never created) is reported NOT mergeable rather than erroring, since it
// cannot be merged. Only an infrastructure fault (clone failure) returns an error.
func (a *gitLocalAccess) trialMerge(head string) (bool, error) {
	cloneDir, cleanup, err := a.cloneMain()
	if err != nil {
		return false, err
	}
	defer cleanup()

	remoteHead := "refs/remotes/origin/" + head
	if _, verr := gitLocalRun(cloneDir, "rev-parse", "--verify", "--quiet", remoteHead); verr != nil {
		return false, nil // head gone → nothing to merge
	}
	if _, anc := gitLocalRun(cloneDir, "merge-base", "--is-ancestor", remoteHead, "HEAD"); anc == nil {
		return true, nil // already merged → trivially mergeable
	}
	_, mErr := gitLocalRun(cloneDir, "-c", "user.name=aiarch", "-c", "user.email=aiarch@local",
		"merge", "--no-commit", "--no-ff", remoteHead)
	_, _ = gitLocalRun(cloneDir, "merge", "--abort") // best-effort; the clone is thrown away regardless
	return mErr == nil, nil
}

// mergeHeadIntoMain performs the real merge of head into main (throwaway clone + push +
// branch delete) and returns main's resulting tip SHA. Already-merged (ancestor, or
// already-deleted) is delete-only idempotent re-entry. A conflict is fwra.Conflict with
// nothing pushed; a clone/push/delete fault is Infrastructure.
func (a *gitLocalAccess) mergeHeadIntoMain(head string) (string, error) {
	cloneDir, cleanup, err := a.cloneMain()
	if err != nil {
		return "", err
	}
	defer cleanup()

	remoteHead := "refs/remotes/origin/" + head
	headExists := false
	if _, verr := gitLocalRun(cloneDir, "rev-parse", "--verify", "--quiet", remoteHead); verr == nil {
		headExists = true
	}

	// Idempotent re-entry: the head was merged AND deleted by a prior successful call —
	// nothing to merge, main already carries the work.
	if !headExists {
		return gitLocalHeadSHA(cloneDir)
	}

	// Already merged but the branch survived (a prior attempt merged then crashed before
	// the delete) → skip straight to the branch delete, no second merge commit.
	if _, anc := gitLocalRun(cloneDir, "merge-base", "--is-ancestor", remoteHead, "HEAD"); anc == nil {
		if out, derr := gitLocalRun(cloneDir, "push", "origin", "--delete", head); derr != nil {
			return "", fwra.Wrap(fwra.Infrastructure, derr, "gitlocal merge: already merged but branch delete failed: "+strings.TrimSpace(out))
		}
		return gitLocalHeadSHA(cloneDir)
	}

	// Real --no-ff merge with a pinned author identity (independent of host git config).
	mergeOut, mErr := gitLocalRun(cloneDir, "-c", "user.name=aiarch", "-c", "user.email=aiarch@local",
		"merge", "--no-ff", "-m", "aiarch: merge "+head, remoteHead)
	if mErr != nil {
		_, _ = gitLocalRun(cloneDir, "merge", "--abort") // best-effort; clone is throwaway
		if strings.Contains(mergeOut, "CONFLICT") {
			return "", fwra.New(fwra.Conflict, "gitlocal merge: conflict merging "+head+" into "+gitLocalMainBranch+"; nothing pushed")
		}
		return "", fwra.Wrap(fwra.Infrastructure, mErr, "gitlocal merge: merge of "+head+" failed: "+strings.TrimSpace(mergeOut))
	}
	if out, perr := gitLocalRun(cloneDir, "push", "origin", gitLocalMainBranch); perr != nil {
		return "", fwra.Wrap(fwra.Infrastructure, perr, "gitlocal merge: push of merged "+gitLocalMainBranch+" failed: "+strings.TrimSpace(out))
	}
	sha, err := gitLocalHeadSHA(cloneDir)
	if err != nil {
		return "", err
	}
	// The merge IS landed; a delete failure is recoverable — a retry takes the
	// already-merged delete-only path above.
	if out, derr := gitLocalRun(cloneDir, "push", "origin", "--delete", head); derr != nil {
		return "", fwra.Wrap(fwra.Infrastructure, derr, "gitlocal merge: merged but branch "+head+" could not be deleted: "+strings.TrimSpace(out))
	}
	return sha, nil
}

// gitLocalHeadSHA resolves the clone's current HEAD tip SHA (the landed merge commit, or
// main's tip on an idempotent re-entry).
func gitLocalHeadSHA(cloneDir string) (string, error) {
	out, err := gitLocalRun(cloneDir, "rev-parse", "HEAD")
	if err != nil {
		return "", fwra.Wrap(fwra.Infrastructure, err, "gitlocal: rev-parse HEAD")
	}
	return strings.TrimSpace(out), nil
}

// gitLocalPRHead decodes the head branch from a local PullRequestRef ("local#<head>"). A
// ref that is not local-shaped / empty is a caller pre-condition violation.
func gitLocalPRHead(pr PullRequestRef) (string, error) {
	head, ok := strings.CutPrefix(PullRequestRefString(pr), gitLocalPRPrefix)
	if !ok || strings.TrimSpace(head) == "" {
		return "", fwra.New(fwra.ContractMisuse, "gitlocal: malformed local PullRequestRef "+PullRequestRefString(pr))
	}
	return head, nil
}

// gitLocalRun runs `git <args...>` with the given working dir (ignored when empty — used
// for the initial clone) and wraps combined output into the error on failure, for a
// debuggable diagnostic. Mirrors constructionpipeline.runGit.
func gitLocalRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...) //nolint:gosec // fixed trusted binary, internally-derived args (repo URL / branch names)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// logCtx returns the call's context.Context for structured logging, defaulting to
// context.Background when the RA context carries none.
func logCtx(rc fwra.Context) context.Context {
	if rc.Context != nil {
		return rc.Context
	}
	return context.Background()
}
