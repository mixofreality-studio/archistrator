package systemdesign

import (
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
// target and the project-birth workflow-file seat can never drift. This is the
// workflow file the design dispatch selects in place of the construction default
// (aiarch-construct.yml).
var designWorkflowFileName = path.Base(sourcecontrol.DesignWorkflowPath)

// gitrail.go is the PR-rail consumer port the design Manager uses to wire the agentic
// DESIGN draft onto the git-forward branch→PR→read-back→+1→merge model
// (I-DESIGN-DISPATCH §2b). It holds the non-Activity value carriers (railCredEnvelope,
// pullRequestStatusView), the provider-neutral PR text builders, and the ActivityOptions
// presets the workflow-side helpers in gitsession.go consume — it holds NO Temporal
// Activities of its own (B10: every rail verb, including syncManagedScaffold, is
// GENERATED and reached through the generated invoker surface, wf.Acts.Rail*).
//
// SUBSET. The design spine needs only the rail verbs the settled flow uses:
// getInstallationToken (mint), openBranch (ensure the session branch), openPullRequest
// (head=sessionBranch, base=main), getPullRequestStatus (the merge guard),
// postReview (the architecture +1 relay), mergePullRequest (the App-mediated merge),
// syncManagedScaffold (the pre-dispatch scaffold-drift refresh). configureBranchProtection
// is a project-birth concern (FU-DD-3), unused here.
//
// DORMANT-WHEN-UNWIRED. The whole rail is OPTIONAL/nil-tolerant exactly like the
// construction git-forward slice: when wf.Rail == nil or wf.Repo == nil (or no repo
// resolves for the project) the CoAuthor workflow runs UNCHANGED — read-back/stage on
// main, no branch/PR ops — so every existing test and the Postgres/non-git composition
// are unperturbed.

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
// Provider-neutral naming + Activity option presets (mirrors gitnaming.go).
// ===========================================================================

// mainBranch is the flat git-forward base every design PR targets (op-concepts §15).
const mainBranch = "main"

// designPRTitle / designPRBody are the human-facing PR text the Manager owns.
func designPRTitle(kind ArtifactKind) string {
	return fmt.Sprintf("aiarch: design %s", artifactKindString(kind))
}

func designPRBody(kind ArtifactKind) string {
	return fmt.Sprintf("Automated agentic design draft of %s (aiarch system-design).", artifactKindString(kind))
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

// railActivityOptions — the generated sourceControlAccess PR-rail ops, including
// syncManagedScaffold (B10). Auth + a merge Conflict (not-mergeable) + bad input are
// terminal; transport/rate-limit retry. Feeds the manager's option hook (workermanifest.go),
// keyed by each op's generated activity name.
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
