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
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// SourceControlAccess is the component's ResourceAccess port — the Go-surface
// name the layer convention requires (a *Access-suffixed exported interface,
// every method error-returning). It is the SINGLE merged port (founder decision
// 2026-06-25): the two former contract faces — ISourceControlLifecycle (lifecycle
// establishment) and IPullRequestRail (the git-forward branch→PR→gate→merge rail)
// — are now ONE flat interface listing all ten ops. The merge keeps a single
// composition-root port + the arch-layer naming rule, and gives the codegen a
// concrete method set to reflect (the schema-first pipeline regenerates the flat
// interface into contract.gen.go).
//
// Contract #1 — lifecycle (sourceControlAccess.md, FROZEN), FOUR atomic verbs:
//   - InstallAuthorizeApp — discover/confirm aiarch's standing authorization on
//     an account; NotFound (the contract's "NotInstalled") if the user has not
//     installed the App. Idempotent on account.
//   - AdoptProjectRepo — verify the user's EXISTING repo is reachable under the
//     App installation, then tag it (aiarch-project topic + project-title
//     description) and return its RepoRef. PERMISSIVE-RESUME (founder ruling
//     2026-06-16): SUCCEEDS regardless of repo content; the ONLY error is
//     NotUnderInstallation (the App must be installed). Idempotent on the repo name.
//   - GetInstallationToken — mint (or serve an in-seam-cached, still-valid) short
//     lived RepoCredential the OTHER GitHub-fronting seams authenticate with.
//     Returned-not-recorded; mint-on-demand.
//   - CommitManagedFiles — seat the aiarch-MANAGED project scaffold (the
//     claude-code-action DESIGN workflow under .github/workflows/ PLUS the go-test
//     gate scaffold: go.mod + the aiarch_method_test.go that runs methodcheck.Check)
//     in ONE birth seat. Each file's path must be on the managed-file ALLOWLIST;
//     each file is overwrite-if-changed (byte-identical → no-op).
//
// Contract #2 — PR rail (sourceControlAccess-pullrequestrail.md, FROZEN), SIX
// verbs: OpenBranch / OpenPullRequest / GetPullRequestStatus / PostReview /
// MergePullRequest / ConfigureBranchProtection. Every provider-touching verb
// takes a Manager-threaded RepoCredential (§1.1). The merge AUTHORITY (when to
// merge) is interventionEngine; this seam only PERFORMS the merge and ENFORCES
// the rail.
//
// Every method takes the ResourceAccess call Context (fwra.Context) as its first
// param — the established RA seam (worker/artifact/constructionpipeline/
// durableexecution): it embeds context.Context and carries the caller's
// SecurityPrincipal + IdempotencyKey. The generator prepends it; the schema
// captures only the data params. The interface is generated into contract.gen.go
// from contract.schema.json — DO NOT hand-edit the generated copy.

// ---------------------------------------------------------------------------
// §3 Data contracts (contract #1 §3) — provider-opaque value types.
// ---------------------------------------------------------------------------

// ProjectID is the logical project a repo serves. Provider-opaque string identity;
// the package never parses it. It is the idempotency anchor for the deterministic
// repo name.

// AccountRef is the provider-neutral identity of the user's source-control
// account/org. Provider-opaque: it maps to a GitHub org login / installation
// INSIDE this seam; the caller never names an installation id.

// RepoAdoptionSpec is the provider-NEUTRAL description of the user's EXISTING repo
// to ADOPT (2026-06-15; REPLACES RepoSpec). RepoName is the USER-SUPPLIED identity
// (name-as-identity: project name == repo name); Title is the human display name
// applied as the repo description on adopt. NO owner/repo/visibility/default-branch
// lexeme is a contract field.

// RepoName is the user-supplied repo name == the project identity (the adopt
// idempotency anchor). The repo MUST already exist; AdoptProjectRepo never creates it.

// Account is the account the repo lives under (the App installation's org).

// Title is the human project title, applied as the repo description on adopt.

// Hints are optional provider-opaque hints; opaque at the boundary.

// ManagedFile is the provider-NEUTRAL description of one aiarch-MANAGED project file
// to seat at birth (CommitManagedFiles). Path MUST be on the managed-file allowlist
// (under .github/workflows/, OR a known scaffold root — go.mod / the method test
// file); any other path is a ContractMisuse (this verb seats ONLY aiarch-managed
// files, never arbitrary content). 2026-06-16 generalization of WorkflowFile: the
// single-file workflow seat became a fileset so the agentic workflow + the go-test
// gate scaffold (go.mod + aiarch_method_test.go) land together at project birth.

// Path is the repo-relative path. Must satisfy the managed-file allowlist
// (e.g. ".github/workflows/aiarch-design.yml", "go.mod", "aiarch_method_test.go").

// Content is the exact file bytes to land on the default branch.

// ManagedCommitMessage is the commit message CommitManagedFiles uses when it seats
// the managed-file bundle at project birth. (The per-file Message of the old
// WorkflowFile is gone — one bundle, one message.)
const ManagedCommitMessage = "chore(aiarch): seat aiarch-managed project scaffold (design workflow + go-test gate)"

// Installation is an opaque handle confirming aiarch holds a standing
// authorization on an account. Provider-opaque (today: a GitHub installation id,
// never surfaced as such).
//
// It is a NAMED SCALAR (the established opaque-handle sub-pattern, same as
// durableexecution's ExecutionHandle / constructionpipeline's PipelineHandle): the
// codegen represents it cleanly as a $def named scalar, and its behaviour lives in
// behavior.go as free functions (InstallationString / InstallationIsZero). The
// opaque installation id the impl packs IS the string value.

// RepoRef is an opaque, provider-neutral handle to one provisioned per-project
// repo — the value the Manager threads to the GitTarget seams' verbs.
// Provider-opaque (today: "account|owner/repo", never parsed by callers).
//
// NAMED SCALAR (opaque-handle sub-pattern): its behaviour (RepoRefString /
// RepoRefEqual / RepoRefIsZero / RepoRefFromString / RepoRefOwnerRepo) lives in
// behavior.go as free functions.

// RepoCredential is an opaque, SHORT-LIVED bearer credential authorizing
// content/CI/manifest operations on a RepoRef. Provider-NEUTRAL: carries NO ghs_…
// prefix, NO installation id, NO App JWT. The Manager threads .Bytes into the
// consuming seams as a caller-supplied parameter (§1.1) and re-mints before
// ExpiresAt. Returned, never recorded.

// Bytes is the opaque bearer secret; the consuming seam presents it, never
// parses it. Treated as write-only at every consumer (never logged/persisted).

// ExpiresAt is when the Manager re-mints (calls GetInstallationToken again).

// RepoCredentialIsZero reports whether the credential is empty. (Replaces the
// former RepoCredential.IsZero() method — the generated struct carries no methods.)
func RepoCredentialIsZero(c RepoCredential) bool { return len(c.Bytes) == 0 }

// CommitRef is an opaque, provider-neutral handle to the commit CommitManagedFiles
// produced (2026-06-15; generalized 2026-06-16). Provider-opaque (today: a commit
// sha, never parsed by callers). The Manager may carry it for traceability / to
// assert the managed scaffold landed. When the bundle is seated by sequential
// per-file commits, this is the LAST file's resulting commit ref.
//
// NAMED SCALAR (opaque-handle sub-pattern): its behaviour (CommitRefString /
// CommitRefIsZero) lives in behavior.go as free functions.

// ---------------------------------------------------------------------------
// §3 Data contracts (contract #2 §3) — PR-rail value types.
// ---------------------------------------------------------------------------

// BranchName is the provider-neutral name of a working branch (Manager-derived,
// per-activity). Provider-opaque: maps to a git ref name INSIDE the seam.

// PullRequestSpec is the provider-NEUTRAL description of a proposal. Base is
// `main` in the flat git-forward model. Labels (e.g. a cr-NN change-request
// group) ride in Hints — not first-class fields.

// ReviewVerdict is the provider-neutral review verdict the App relays.

// ReviewApprove is the "architecture +1".

// ReviewRequestChanges requests changes.

// ReviewComment is a non-deciding comment.

// ReviewSubmission is the provider-neutral review the App relays.

// BranchRef is an opaque, provider-neutral handle to a cut branch.
//
// NAMED SCALAR (opaque-handle sub-pattern): its behaviour (BranchRefString /
// BranchRefIsZero) lives in behavior.go as free functions.

// PullRequestRef is an opaque, provider-neutral handle to one open proposal — the
// value the Manager carries across GetPullRequestStatus / PostReview /
// MergePullRequest. Provider-opaque (today: a PR number, never parsed by callers).
//
// NAMED SCALAR (opaque-handle sub-pattern): its behaviour (PullRequestRefString /
// PullRequestRefEqual / PullRequestRefIsZero / PullRequestRefFromString) lives in
// behavior.go as free functions.

// MergeResult is an opaque, provider-neutral handle to a completed merge: the
// resulting main commit ref + a merged flag.

// Commit is the opaque resulting main-tip ref; presented, never parsed.

// Merged is true on success / already-merged.

// CheckState is the provider-neutral CI rollup the merge gate reads.

// CheckPending — at least one check still running, none failed.

// CheckSuccess — all checks concluded successfully (or none present).

// CheckFailure — at least one check failed.

// PullRequestStatus is the typed-but-provider-opaque reflection of CI + approvals
// the merge gate reads. It is a REFLECTION the Manager feeds interventionEngine —
// NOT the gate.

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
