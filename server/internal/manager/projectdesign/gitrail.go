package projectdesign

import (
	"context"
	"fmt"
	"path"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/sourcecontrol"
	fwmanager "github.com/davidmarne/archistrator-platform/framework-go/manager"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// designWorkflowFileName is the per-project DESIGN workflow file the agentic design
// dispatch must target (per-project-design-dispatch) — the BASENAME of
// sourcecontrol.DesignWorkflowPath (".github/workflows/aiarch-design.yml"), i.e.
// "aiarch-design.yml". Derived from the RA's single source of truth so the dispatch
// target and the project-birth workflow-file seat can never drift.
var designWorkflowFileName = path.Base(sourcecontrol.DesignWorkflowPath)

// gitrail.go is the PR-rail consumer port + Temporal Activity wrappers the design
// Manager uses to wire the agentic DESIGN draft onto the git-forward branch→PR→read-
// back→+1→merge model (I-DESIGN-DISPATCH §2b). It MIRRORS the construction Manager's
// gitactivities.go / gitnaming.go pattern EXACTLY (same railCredEnvelope cred carrier,
// same Activity-per-rail-verb shape, same deterministic-name idempotency): the cred is
// MINTED by the Manager (MintRepoCredentialActivity → GetInstallationToken, a call
// DOWN) and threaded INTO every rail verb as a parameter; the RA never reads Temporal
// context and never fetches the credential itself ([[feedback_temporal_manager_layer_only]]).
//
// SUBSET. The design spine needs only the rail verbs the settled flow uses:
// GetInstallationToken (mint), OpenBranch (ensure the session branch), OpenPullRequest
// (head=sessionBranch, base=main), GetPullRequestStatus (the merge guard),
// PostReview (the architecture +1 relay), MergePullRequest (the App-mediated merge).
// ConfigureBranchProtection is a project-birth concern (FU-DD-3), absent here.
//
// DORMANT-WHEN-UNWIRED. The whole rail is OPTIONAL/nil-tolerant exactly like the
// construction git-forward slice: when wf.Rail == nil or wf.Repo == nil (or no repo
// resolves for the project) the CoAuthor workflow runs UNCHANGED — read-back/stage on
// main, no branch/PR ops — so every existing test and the Postgres/non-git composition
// are unperturbed.

// ===========================================================================
// Consumer port — the FROZEN IPullRequestRail subset (sourceControlAccess
// pullrequestrail.md) plus the one lifecycle op the Manager needs to mint the
// credential (GetInstallationToken). The concrete *sourcecontrol.Access satisfies
// this structurally; the composition root adapts it (cmd/server). Mirrors the
// construction Manager's SourceControlRail (deps.go).
// ===========================================================================

// SourceControlRail is the design Manager's consumer view of the PR rail. Every
// provider-touching verb takes a Manager-threaded RepoCredential; the returns are
// opaque handles the Manager carries across the Activity boundary as plain strings.
type SourceControlRail interface {
	GetInstallationToken(ctx context.Context, repo sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error)
	OpenBranch(ctx context.Context, repo sourcecontrol.RepoRef, branch sourcecontrol.BranchName, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) (sourcecontrol.BranchRef, error)
	OpenPullRequest(ctx context.Context, repo sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) (sourcecontrol.PullRequestRef, error)
	GetPullRequestStatus(ctx context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, cred sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error)
	PostReview(ctx context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, review sourcecontrol.ReviewSubmission, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) error
	MergePullRequest(ctx context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) (sourcecontrol.MergeResult, error)
}

// ===========================================================================
// Activity-boundary value carriers (mirrors gitactivities.go).
// ===========================================================================

// railCredEnvelope carries the opaque short-lived credential across the Activity
// boundary. The Bytes are write-only at every consumer (never logged); they ride the
// Temporal payload exactly as the rail returns them.
type railCredEnvelope struct {
	Bytes     []byte
	ExpiresAt time.Time
}

func (c railCredEnvelope) toRail() sourcecontrol.RepoCredential {
	return sourcecontrol.RepoCredential{Bytes: c.Bytes, ExpiresAt: c.ExpiresAt}
}

// PullRequestStatusView is the Manager-local Activity-boundary projection of the rail's
// PullRequestStatus — the merge-guard reflection the workflow reads before approve/merge.
type PullRequestStatusView struct {
	CheckGreen    bool
	ApprovalCount int
	Mergeable     bool
}

// ===========================================================================
// PR-rail Activities (the FROZEN IPullRequestRail subset).
// ===========================================================================

// MintRepoCredentialActivity wraps GetInstallationToken — the Manager mints the short-
// lived credential it threads into every other rail verb. Read-shaped (no idempotency
// key); a rejected/expired identity surfaces fwra.Auth (terminal).
func (wf *Workflows) MintRepoCredentialActivity(ctx context.Context, repoRef string) (railCredEnvelope, error) {
	cred, err := wf.Rail.GetInstallationToken(ctx, sourcecontrol.RepoRefFromString(repoRef))
	if err != nil {
		return railCredEnvelope{}, fwmanager.MapError(err)
	}
	return railCredEnvelope{Bytes: cred.Bytes, ExpiresAt: cred.ExpiresAt}, nil
}

// OpenBranchArgs bundles the OpenBranch inputs across the Activity boundary.
type OpenBranchArgs struct {
	RepoRef string
	Branch  string
	Cred    railCredEnvelope
}

// OpenBranchActivity wraps OpenBranch → the opaque BranchRef (its String() form).
// Idempotent on the deterministic session-branch name (re-open is a no-op).
func (wf *Workflows) OpenBranchActivity(ctx context.Context, a OpenBranchArgs) (string, error) {
	br, err := wf.Rail.OpenBranch(ctx,
		sourcecontrol.RepoRefFromString(a.RepoRef),
		sourcecontrol.BranchName(a.Branch),
		a.Cred.toRail(),
		activityIdempotencyKey(ctx),
	)
	if err != nil {
		return "", fwmanager.MapError(err)
	}
	return br.String(), nil
}

// OpenPullRequestArgs bundles the OpenPullRequest inputs across the Activity boundary.
type OpenPullRequestArgs struct {
	RepoRef string
	Head    string
	Base    string
	Title   string
	Body    string
	Cred    railCredEnvelope
}

// OpenPullRequestActivity wraps OpenPullRequest → the opaque PullRequestRef. Idempotent
// on the head branch — if the Action already opened a PR for head, the rail returns the
// existing handle (the server's open is the authoritative handle source for merge).
func (wf *Workflows) OpenPullRequestActivity(ctx context.Context, a OpenPullRequestArgs) (string, error) {
	pr, err := wf.Rail.OpenPullRequest(ctx,
		sourcecontrol.RepoRefFromString(a.RepoRef),
		sourcecontrol.PullRequestSpec{
			Head:  sourcecontrol.BranchName(a.Head),
			Base:  sourcecontrol.BranchName(a.Base),
			Title: a.Title,
			Body:  a.Body,
		},
		a.Cred.toRail(),
		activityIdempotencyKey(ctx),
	)
	if err != nil {
		return "", fwmanager.MapError(err)
	}
	return pr.String(), nil
}

// GetPullRequestStatusArgs bundles the status read inputs.
type GetPullRequestStatusArgs struct {
	RepoRef string
	PRRef   string
	Cred    railCredEnvelope
}

// GetPullRequestStatusActivity wraps GetPullRequestStatus → the merge-guard reflection.
// Pure read.
func (wf *Workflows) GetPullRequestStatusActivity(ctx context.Context, a GetPullRequestStatusArgs) (PullRequestStatusView, error) {
	st, err := wf.Rail.GetPullRequestStatus(ctx,
		sourcecontrol.RepoRefFromString(a.RepoRef),
		sourcecontrol.PullRequestRefFromString(a.PRRef),
		a.Cred.toRail(),
	)
	if err != nil {
		return PullRequestStatusView{}, fwmanager.MapError(err)
	}
	return PullRequestStatusView{
		CheckGreen:    st.CheckRollup == sourcecontrol.CheckSuccess,
		ApprovalCount: st.ApprovalCount,
		Mergeable:     st.Mergeable,
	}, nil
}

// PostReviewArgs bundles the +1-relay inputs.
type PostReviewArgs struct {
	RepoRef string
	PRRef   string
	Body    string
	Cred    railCredEnvelope
}

// PostReviewActivity wraps PostReview — relays the architecture +1 (Approve) to the PR.
// Idempotent on re-post.
func (wf *Workflows) PostReviewActivity(ctx context.Context, a PostReviewArgs) (struct{}, error) {
	err := wf.Rail.PostReview(ctx,
		sourcecontrol.RepoRefFromString(a.RepoRef),
		sourcecontrol.PullRequestRefFromString(a.PRRef),
		sourcecontrol.ReviewSubmission{Verdict: sourcecontrol.ReviewApprove, Body: a.Body},
		a.Cred.toRail(),
		activityIdempotencyKey(ctx),
	)
	if err != nil {
		return struct{}{}, fwmanager.MapError(err)
	}
	return struct{}{}, nil
}

// MergePullRequestArgs bundles the gated-merge inputs.
type MergePullRequestArgs struct {
	RepoRef string
	PRRef   string
	Cred    railCredEnvelope
}

// MergePullRequestActivity wraps MergePullRequest → whether the merge to main landed.
// The Manager PERFORMS the merge; the merge GUARD (CheckGreen) is decided in workflow
// code before this. Idempotent (already-merged maps to Merged=true inside the rail).
func (wf *Workflows) MergePullRequestActivity(ctx context.Context, a MergePullRequestArgs) (bool, error) {
	mr, err := wf.Rail.MergePullRequest(ctx,
		sourcecontrol.RepoRefFromString(a.RepoRef),
		sourcecontrol.PullRequestRefFromString(a.PRRef),
		a.Cred.toRail(),
		activityIdempotencyKey(ctx),
	)
	if err != nil {
		return false, fwmanager.MapError(err)
	}
	return mr.Merged, nil
}

// ===========================================================================
// Provider-neutral naming + Activity option presets (mirrors gitnaming.go).
// ===========================================================================

// mainBranch is the flat git-forward base every design PR targets (op-concepts §15).
const mainBranch = "main"

// designPRTitle / designPRBody are the human-facing PR text the Manager owns.
func designPRTitle(kind ArtifactKind) string {
	return fmt.Sprintf("aiarch: Phase-2 design %s", kind)
}

func designPRBody(kind ArtifactKind) string {
	return fmt.Sprintf("Automated agentic design draft of %s (aiarch project-design).", kind)
}

// designArchApprovalBody is the +1 relay's review body — the architect's in-app
// approval relayed onto the PR (the "architecture +1").
func designArchApprovalBody(kind ArtifactKind) string {
	return fmt.Sprintf("architecture +1 relayed for %s", kind)
}

// mintCredOpts — the credential mint. A rejected/expired App identity is terminal.
func mintCredOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.Auth),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// railOpts — the PR-rail verbs. Auth + a merge Conflict (not-mergeable) + bad input are
// terminal; transport/rate-limit retry.
func railOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.Auth),
				fwmanager.RAErrType(fwra.NotFound),
				fwmanager.RAErrType(fwra.Conflict),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}
