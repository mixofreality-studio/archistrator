package construction

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
)

// gitnaming.go holds the Manager's provider-NEUTRAL, DETERMINISTIC naming + the git
// Activity option presets for the git-forward slice (C-MCN-GIT). The names are
// Manager-derived (the branch/PR/label vocabulary the rail maps to a git ref INSIDE
// the seam); determinism is load-bearing for the rail's deterministic-name idempotency
// (a workflow retry re-opening the same branch/PR is a no-op in the rail).

// mainBranch is the flat git-forward base every per-activity PR targets
// (op-concepts §15 — branch per activity, no long-lived integration branch).
const mainBranch = "main"

// activityBranchName derives the provider-neutral per-activity branch name
// "activity/<activityID>" (D-PA-GIT GIT.1 example). Deterministic in the activity id.
func activityBranchName(activityID ActivityID) string {
	return "activity/" + string(activityID)
}

// prTitle / prBody are the human-facing PR text the Manager's sequence owns.
func prTitle(activityID ActivityID) string {
	return fmt.Sprintf("aiarch: construction activity %s", activityID)
}

func prBody(activity constructionActivity) string {
	return fmt.Sprintf("Automated construction of component %s (%s, layer %s).",
		activity.ComponentID, activityKindName(activity.Kind), activity.Layer)
}

// activityKindName returns the canonical activity-kind name — a free-function
// replacement for the retired Manager-local activityKind.String() method (methods
// cannot be added to the published handoff.ActivityKind from this package). Produces
// the IDENTICAL strings the former Stringer did (PR body text — zero behavior change).
func activityKindName(k handoff.ActivityKind) string {
	switch k {
	case handoff.ActivityKindUnknown:
		// zero-value sentinel, not a real activity kind — same as any unmapped value.
		return "Unknown"
	case handoff.ActivityKindDetailedDesign:
		return "DetailedDesign"
	case handoff.ActivityKindConstruction:
		return "Construction"
	case handoff.ActivityKindIntegration:
		return "Integration"
	case handoff.ActivityKindNoncoding:
		return "Noncoding"
	default:
		return "Unknown"
	}
}

// archApprovalBody is the +1 relay's review body — the architect's in-app
// architecture sign-off relayed onto the PR.
func archApprovalBody(activityID ActivityID) string {
	return fmt.Sprintf("architecture +1 relayed for %s", activityID)
}

// crLabelHints encodes the cr-NN change-request group label into the rail's opaque
// PullRequestSpec.Hints (labels ride in Hints, not a first-class field —
// sourcecontrol.go §3). Empty label ⇒ nil hints.
func crLabelHints(crLabel string) []byte {
	if crLabel == "" {
		return nil
	}
	return []byte(crLabel)
}

// ---------------------------------------------------------------------------
// git Activity option presets (constructionManager.md §6.4 pattern). Concrete
// RetryPolicy / timeout choices live here, in the Manager.
// ---------------------------------------------------------------------------

// mintCredActivityOptions — the credential mint preset VALUE the manifest's Opts hook
// (workermanifest.go) applies to the GENERATED getInstallationToken invoker. A
// rejected/expired App identity is terminal (fwra.Auth); transport blips retry.
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

// railActivityOptions — the PR-rail verbs preset VALUE (OpenBranch / OpenPullRequest /
// GetPullRequestStatus / PostReview / MergePullRequest), applied to the GENERATED rail
// invokers via the manifest's Opts hook. Auth + a merge Conflict (not-mergeable) + bad
// input are terminal; transport/rate-limit retry.
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
