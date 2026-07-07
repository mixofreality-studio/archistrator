package projectdesign

import (
	"context"
	"fmt"
	"path"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
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
// Rail migration to the generated invoker surface.
//
// The six PR-rail verbs (GetInstallationToken/mint, OpenBranch, OpenPullRequest,
// GetPullRequestStatus, PostReview, MergePullRequest) are GENERATED (activities.gen.go)
// and reached through the generated invoker surface (wf.Acts.Rail*) from the workflow-side
// helpers in gitsession.go. The folded railAdapterImpl + the plain-ctx sourceControlRail
// seam + the per-verb Activity wrappers are RETIRED; the workflow-side value mapping
// (opaque-handle *FromString/*String marshalling, PullRequestStatus→pullRequestStatusView,
// the ReviewApprove verdict now supplied at the call site) lives in gitsession.go. The
// per-op ActivityOptions presets (mintCredActivityOptions / railActivityOptions, below)
// feed the manager's option hook (workermanifest.go).
//
// SyncManagedScaffold STAYS a CUSTOM Activity (SyncManagedScaffoldActivity): it wraps
// sourcecontrol.SyncManagedScaffold, a free-function composition helper the generated
// layer has no single contract op for. It reaches the published rail directly via wf.Rail.
// ===========================================================================

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

// pullRequestStatusView is the Manager-local Activity-boundary projection of the rail's
// PullRequestStatus — the merge-guard reflection the workflow reads before approve/merge.
type pullRequestStatusView struct {
	CheckGreen    bool
	ApprovalCount int
	Mergeable     bool
}

// ===========================================================================
// SyncManagedScaffold — the ONE CUSTOM rail Activity (free-function composition helper).
// ===========================================================================

// syncScaffoldArgs bundles the managed-scaffold sync inputs across the Activity boundary.
type syncScaffoldArgs struct {
	RepoRef string
	Cred    railCredEnvelope
}

// SyncManagedScaffoldActivity wraps sourceControlRail.SyncManagedScaffold — the
// MANAGED-SCAFFOLD SYNC that runs before every design-job dispatch (beginSession):
// the seated aiarch-design.yml is converged onto the CURRENT template rendering on the
// default branch (drift → one refresh commit; identical → no-op). Returns whether the
// seated copy drifted. A failure here BLOCKS the dispatch: the caller must never run a
// design job against a scaffold it could not prove current (mirrors systemdesign).
func (wf *workflows) SyncManagedScaffoldActivity(ctx context.Context, a syncScaffoldArgs) (bool, error) {
	changed, err := sourcecontrol.SyncManagedScaffold(ctx, wf.Rail, sourcecontrol.RepoRefFromString(a.RepoRef), a.Cred.toRail())
	if err != nil {
		return false, fwmanager.MapError(err)
	}
	return changed, nil
}

// ===========================================================================
// Provider-neutral naming + Activity option presets (mirrors gitnaming.go).
// ===========================================================================

// mainBranch is the flat git-forward base every design PR targets (op-concepts §15).
const mainBranch = "main"

// designPRTitle / designPRBody are the human-facing PR text the Manager owns.
func designPRTitle(kind ArtifactKind) string {
	return fmt.Sprintf("aiarch: Phase-2 design %s", artifactKindString(kind))
}

func designPRBody(kind ArtifactKind) string {
	return fmt.Sprintf("Automated agentic design draft of %s (aiarch project-design).", artifactKindString(kind))
}

// designArchApprovalBody is the +1 relay's review body — the architect's in-app
// approval relayed onto the PR (the "architecture +1").
func designArchApprovalBody(kind ArtifactKind) string {
	return fmt.Sprintf("architecture +1 relayed for %s", artifactKindString(kind))
}

// mintCredActivityOptions — the credential mint (generated sourceControlAccess.
// getInstallationToken). A rejected/expired App identity is terminal. Feeds the manager's
// option hook (workermanifest.go).
func mintCredActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.Auth),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	}
}

// railActivityOptions — the PR-rail verbs (the generated sourceControlAccess rail ops AND
// the custom SyncManagedScaffold). Auth + a merge Conflict (not-mergeable) + bad input are
// terminal; transport/rate-limit retry. Feeds the manager's option hook for the generated
// verbs; the ctx-wrapper railOpts applies it at the custom SyncManagedScaffold call site.
func railActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.Auth),
				fwmanager.RAErrType(fwra.NotFound),
				fwmanager.RAErrType(fwra.Conflict),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	}
}

// railOpts is the ctx-wrapper the CUSTOM SyncManagedScaffold Activity call site applies
// directly (the generated rail verbs get railActivityOptions via the option hook).
func railOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, railActivityOptions())
}
