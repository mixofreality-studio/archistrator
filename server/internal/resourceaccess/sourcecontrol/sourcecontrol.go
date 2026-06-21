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
// The concrete GitHub-App-backed implementation (Access) lives in github.go; the
// vendor REST/JWT wire code lives in the framework-go-infrastructure-github
// satellite behind the githubClient seam — the ONLY place this RA speaks GitHub.
package sourcecontrol

import (
	"context"
	"strings"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// SourceControlAccess is the component's ResourceAccess port — the Go-surface
// name the layer convention requires (a *Access-suffixed exported interface,
// every method error-returning). It is the union of the two FROZEN contract
// faces: ISourceControlLifecycle (lifecycle establishment) and IPullRequestRail
// (the git-forward branch→PR→gate→merge rail). The two faces are kept as their
// own named interfaces below so the contract vocabulary stays visible; this
// embeds both for the single composition-root port + the arch-layer naming rule.
type SourceControlAccess interface {
	SourceControlLifecycle
	PullRequestRail
}

// ---------------------------------------------------------------------------
// Contract #1 — ISourceControlLifecycle (sourceControlAccess.md, FROZEN).
// ---------------------------------------------------------------------------

// SourceControlLifecycle is the provider-opaque repo-standing-establishment port
// (2026-06-15 agentic-pivot re-cut: 3 → 5 ops; 2026-06-15 correction: → 4 ops —
// writeActionsSecret REMOVED). FOUR atomic verbs, each importing no Temporal —
// each a facet of "aiarch establishes and holds standing in the user's own repo":
//
//   - InstallAuthorizeApp — discover/confirm aiarch's standing authorization on
//     an account; NotFound (the contract's "NotInstalled") if the user has not
//     installed the App. Idempotent on account. (UNCHANGED.)
//   - AdoptProjectRepo — verify the user's EXISTING repo is reachable under the
//     App installation, then tag it (aiarch-project topic + project-title
//     description) and return its RepoRef. PERMISSIVE-RESUME (founder ruling
//     2026-06-16): SUCCEEDS regardless of repo content (README/claude.yml/a prior
//     .aiarch tree are all fine); the ONLY error is NotUnderInstallation (the App
//     must be installed). The strict-empty RepoNotEmpty/Conflict hard-fail is GONE.
//     A repo with committed .aiarch/state is RESUMED by projectStateAccess.createProject,
//     not rejected here. Idempotent on the repo name. (REPLACES ProvisionProjectRepo
//     and the prior strict-empty policy.)
//   - GetInstallationToken — mint (or serve an in-seam-cached, still-valid) short
//     lived RepoCredential the OTHER GitHub-fronting seams authenticate with.
//     Returned-not-recorded; mint-on-demand; no idempotency key (read-shaped).
//     (UNCHANGED.)
//   - CommitManagedFiles — seat the aiarch-MANAGED project scaffold (the
//     claude-code-action DESIGN workflow under .github/workflows/ PLUS the go-test
//     gate scaffold: go.mod + the aiarch_method_test.go that runs methodcheck.Check)
//     in ONE birth seat. Each file's path must be on the managed-file ALLOWLIST
//     (under .github/workflows/, OR a known scaffold root: go.mod / the method test
//     file) — arbitrary paths are rejected (it seats ONLY aiarch-managed files).
//     Each file is overwrite-if-changed (byte-identical → no-op). 2026-06-16 generalization
//     of CommitAgenticWorkflowFile: instead of adding a 5th op, the single-file
//     workflow seat became a fileset seat so the workflow + the go-test gate land
//     together at birth. (SC-B, generalized.)
//
// CORRECTION (2026-06-15, founder ruling): writeActionsSecret was REMOVED. aiarch
// does NO secret management. The CLAUDE_CODE_OAUTH_TOKEN repo Actions secret is
// provisioned by the Claude Code GitHub App when the USER runs /install-github-app
// on their repo (an OAuth-flow secret, not an API-uploadable value) — a user
// onboarding prerequisite, never touched by aiarch.
type SourceControlLifecycle interface {
	InstallAuthorizeApp(ctx context.Context, account AccountRef, key fwra.IdempotencyKey) (Installation, error)
	AdoptProjectRepo(ctx context.Context, spec RepoAdoptionSpec, key fwra.IdempotencyKey) (RepoRef, error)
	GetInstallationToken(ctx context.Context, repo RepoRef) (RepoCredential, error)
	CommitManagedFiles(ctx context.Context, repo RepoRef, files []ManagedFile, cred RepoCredential, key fwra.IdempotencyKey) (CommitRef, error)
}

// ---------------------------------------------------------------------------
// Contract #2 — IPullRequestRail (sourceControlAccess-pullrequestrail.md, FROZEN).
// ---------------------------------------------------------------------------

// PullRequestRail is the provider-opaque branch / PR / gate / merge port — the
// git-forward rail. Every provider-touching verb takes a Manager-threaded
// RepoCredential (§1.1). The merge AUTHORITY (when to merge) is interventionEngine;
// this seam only PERFORMS the merge (MergePullRequest) and ENFORCES the rail
// (ConfigureBranchProtection).
type PullRequestRail interface {
	OpenBranch(ctx context.Context, repo RepoRef, branch BranchName, cred RepoCredential, key fwra.IdempotencyKey) (BranchRef, error)
	OpenPullRequest(ctx context.Context, repo RepoRef, spec PullRequestSpec, cred RepoCredential, key fwra.IdempotencyKey) (PullRequestRef, error)
	GetPullRequestStatus(ctx context.Context, repo RepoRef, pr PullRequestRef, cred RepoCredential) (PullRequestStatus, error)
	PostReview(ctx context.Context, repo RepoRef, pr PullRequestRef, review ReviewSubmission, cred RepoCredential, key fwra.IdempotencyKey) error
	MergePullRequest(ctx context.Context, repo RepoRef, pr PullRequestRef, cred RepoCredential, key fwra.IdempotencyKey) (MergeResult, error)
	ConfigureBranchProtection(ctx context.Context, repo RepoRef, cred RepoCredential, key fwra.IdempotencyKey) error
}

// ---------------------------------------------------------------------------
// §3 Data contracts (contract #1 §3) — provider-opaque value types.
// ---------------------------------------------------------------------------

// ProjectID is the logical project a repo serves. Provider-opaque string identity;
// the package never parses it. It is the idempotency anchor for the deterministic
// repo name.
type ProjectID string

// AccountRef is the provider-neutral identity of the user's source-control
// account/org. Provider-opaque: it maps to a GitHub org login / installation
// INSIDE this seam; the caller never names an installation id.
type AccountRef string

// RepoAdoptionSpec is the provider-NEUTRAL description of the user's EXISTING repo
// to ADOPT (2026-06-15; REPLACES RepoSpec). RepoName is the USER-SUPPLIED identity
// (name-as-identity: project name == repo name); Title is the human display name
// applied as the repo description on adopt. NO owner/repo/visibility/default-branch
// lexeme is a contract field.
type RepoAdoptionSpec struct {
	// RepoName is the user-supplied repo name == the project identity (the adopt
	// idempotency anchor). The repo MUST already exist; AdoptProjectRepo never creates it.
	RepoName string
	// Account is the account the repo lives under (the App installation's org).
	Account AccountRef
	// Title is the human project title, applied as the repo description on adopt.
	Title string
	// Hints are optional provider-opaque hints; opaque at the boundary.
	Hints []byte
}

// ManagedFile is the provider-NEUTRAL description of one aiarch-MANAGED project file
// to seat at birth (CommitManagedFiles). Path MUST be on the managed-file allowlist
// (under .github/workflows/, OR a known scaffold root — go.mod / the method test
// file); any other path is a ContractMisuse (this verb seats ONLY aiarch-managed
// files, never arbitrary content). 2026-06-16 generalization of WorkflowFile: the
// single-file workflow seat became a fileset so the agentic workflow + the go-test
// gate scaffold (go.mod + aiarch_method_test.go) land together at project birth.
type ManagedFile struct {
	// Path is the repo-relative path. Must satisfy the managed-file allowlist
	// (e.g. ".github/workflows/aiarch-design.yml", "go.mod", "aiarch_method_test.go").
	Path string
	// Content is the exact file bytes to land on the default branch.
	Content []byte
}

// ManagedCommitMessage is the commit message CommitManagedFiles uses when it seats
// the managed-file bundle at project birth. (The per-file Message of the old
// WorkflowFile is gone — one bundle, one message.)
const ManagedCommitMessage = "chore(aiarch): seat aiarch-managed project scaffold (design workflow + go-test gate)"

// Installation is an opaque handle confirming aiarch holds a standing
// authorization on an account. Provider-opaque (today: a GitHub installation id,
// never surfaced as such).
type Installation struct {
	opaque string
}

// String returns the canonical printable form (logs, audit).
func (i Installation) String() string { return i.opaque }

// IsZero reports whether the handle addresses no installation.
func (i Installation) IsZero() bool { return i.opaque == "" }

// RepoRef is an opaque, provider-neutral handle to one provisioned per-project
// repo — the value the Manager threads to the GitTarget seams' verbs.
// Provider-opaque (today: owner/repo plus the owning account, never parsed by
// callers).
type RepoRef struct {
	opaque string
}

// String returns the canonical printable form.
func (r RepoRef) String() string { return r.opaque }

// Equal reports value equality of two repo refs.
func (r RepoRef) Equal(other RepoRef) bool { return r.opaque == other.opaque }

// IsZero reports whether the ref addresses no repo.
func (r RepoRef) IsZero() bool { return r.opaque == "" }

// RepoRefFromString reconstructs a RepoRef from the exact String() form a prior
// ProvisionProjectRepo returned (a Manager re-materialising a persisted handle).
// Pure value reconstruction; a malformed ref is rejected by the verb that
// consumes it.
func RepoRefFromString(s string) RepoRef { return RepoRef{opaque: s} }

// OwnerRepo decodes the RepoRef into its provider owner + repo coordinates — the ONLY
// public accessor of the otherwise-opaque owner/repo encoding. It exists so a caller
// that must address the repo on a DIFFERENT infrastructure port than this RA (the
// per-project-design-dispatch: the constructionPipelineAccess seam dispatches the
// agentic DESIGN job to the per-project repo) can resolve the owner/repo WITHOUT
// re-implementing this RA's private RepoRef encoding. A malformed ref is a
// ContractMisuse the caller surfaces. This is the single seam where owner/repo leaves
// the RA, deliberately scoped to the cross-port dispatch target.
func (r RepoRef) OwnerRepo() (owner, repo string, err error) {
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

// RepoCredential is an opaque, SHORT-LIVED bearer credential authorizing
// content/CI/manifest operations on a RepoRef. Provider-NEUTRAL: carries NO ghs_…
// prefix, NO installation id, NO App JWT. The Manager threads .Bytes into the
// consuming seams as a caller-supplied parameter (§1.1) and re-mints before
// ExpiresAt. Returned, never recorded.
type RepoCredential struct {
	// Bytes is the opaque bearer secret; the consuming seam presents it, never
	// parses it. Treated as write-only at every consumer (never logged/persisted).
	Bytes []byte
	// ExpiresAt is when the Manager re-mints (calls GetInstallationToken again).
	ExpiresAt time.Time
}

// IsZero reports whether the credential is empty.
func (c RepoCredential) IsZero() bool { return len(c.Bytes) == 0 }

// CommitRef is an opaque, provider-neutral handle to the commit CommitManagedFiles
// produced (2026-06-15; generalized 2026-06-16). Provider-opaque (today: a commit
// sha, never parsed by callers). The Manager may carry it for traceability / to
// assert the managed scaffold landed. When the bundle is seated by sequential
// per-file commits, this is the LAST file's resulting commit ref.
type CommitRef struct {
	opaque string
}

// String returns the canonical printable form.
func (c CommitRef) String() string { return c.opaque }

// IsZero reports whether the ref addresses no commit.
func (c CommitRef) IsZero() bool { return c.opaque == "" }

// ---------------------------------------------------------------------------
// §3 Data contracts (contract #2 §3) — PR-rail value types.
// ---------------------------------------------------------------------------

// BranchName is the provider-neutral name of a working branch (Manager-derived,
// per-activity). Provider-opaque: maps to a git ref name INSIDE the seam.
type BranchName string

// PullRequestSpec is the provider-NEUTRAL description of a proposal. Base is
// `main` in the flat git-forward model. Labels (e.g. a cr-NN change-request
// group) ride in Hints — not first-class fields.
type PullRequestSpec struct {
	Head  BranchName
	Base  BranchName
	Title string
	Body  string
	Hints []byte
}

// ReviewVerdict is the provider-neutral review verdict the App relays.
type ReviewVerdict int

const (
	// ReviewApprove is the "architecture +1".
	ReviewApprove ReviewVerdict = iota
	// ReviewRequestChanges requests changes.
	ReviewRequestChanges
	// ReviewComment is a non-deciding comment.
	ReviewComment
)

// ReviewSubmission is the provider-neutral review the App relays.
type ReviewSubmission struct {
	Verdict ReviewVerdict
	Body    string
}

// BranchRef is an opaque, provider-neutral handle to a cut branch.
type BranchRef struct {
	opaque string
}

// String returns the canonical printable form.
func (b BranchRef) String() string { return b.opaque }

// IsZero reports whether the ref addresses no branch.
func (b BranchRef) IsZero() bool { return b.opaque == "" }

// PullRequestRef is an opaque, provider-neutral handle to one open proposal — the
// value the Manager carries across GetPullRequestStatus / PostReview /
// MergePullRequest. Provider-opaque (today: a PR number, never parsed by callers).
type PullRequestRef struct {
	opaque string
}

// String returns the canonical printable form.
func (p PullRequestRef) String() string { return p.opaque }

// Equal reports value equality of two PR refs.
func (p PullRequestRef) Equal(other PullRequestRef) bool { return p.opaque == other.opaque }

// IsZero reports whether the ref addresses no PR.
func (p PullRequestRef) IsZero() bool { return p.opaque == "" }

// PullRequestRefFromString reconstructs a PullRequestRef from a persisted String()
// form (a Manager re-materialising a handle across an Activity boundary).
func PullRequestRefFromString(s string) PullRequestRef { return PullRequestRef{opaque: s} }

// MergeResult is an opaque, provider-neutral handle to a completed merge: the
// resulting main commit ref + a merged flag.
type MergeResult struct {
	// Commit is the opaque resulting main-tip ref; presented, never parsed.
	Commit string
	// Merged is true on success / already-merged.
	Merged bool
}

// CheckState is the provider-neutral CI rollup the merge gate reads.
type CheckState int

const (
	// CheckPending — at least one check still running, none failed.
	CheckPending CheckState = iota
	// CheckSuccess — all checks concluded successfully (or none present).
	CheckSuccess
	// CheckFailure — at least one check failed.
	CheckFailure
)

var checkStateNames = map[CheckState]string{
	CheckPending: "Pending", CheckSuccess: "Success", CheckFailure: "Failure",
}

// String returns the stable name (logs, audit).
func (s CheckState) String() string {
	if n, ok := checkStateNames[s]; ok {
		return n
	}
	return "Pending"
}

// PullRequestStatus is the typed-but-provider-opaque reflection of CI + approvals
// the merge gate reads. It is a REFLECTION the Manager feeds interventionEngine —
// NOT the gate.
type PullRequestStatus struct {
	CheckRollup   CheckState
	ApprovalCount int
	Mergeable     bool
}

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
//     commitManagedFiles' concurrent-write race, Retryable overridden true.
//     adoptProjectRepo NO LONGER returns Conflict — the 2026-06-16 permissive-resume
//     ruling removed the strict-empty RepoNotEmpty hard-fail.)
//   - ContractMisuse → fwra.ContractMisuse   (terminal: empty AccountRef/RepoName/RepoRef / empty fileset /
//     a managed-file path off the allowlist (.github/workflows/ or a scaffold root) / zero cred / bad input)
//
// 2026-06-16 (permissive-resume): AdoptProjectRepo maps ONLY "repo not reachable
// under the installation" to NotFound (surfaced as "NotUnderInstallation", terminal).
// The strict-empty RepoNotEmpty/Conflict mapping is GONE — adopt succeeds regardless
// of content; an existing .aiarch/state is RESUMED by projectStateAccess.createProject.
// (The old AlreadyExists-on-provisionProjectRepo → success mapping was already gone
// with the provision→adopt swap.) AlreadyMerged (mergePullRequest) and the PR-rail
// already-exists (openBranch / openPullRequest) are still mapped to SUCCESS inside
// the seam (framework-go has no AlreadyExists kind — the idempotent-success path
// returns the existing handle).
type Error = fwra.Error
