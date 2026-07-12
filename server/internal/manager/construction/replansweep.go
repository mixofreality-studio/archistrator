package construction

import (
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ===========================================================================
// ReplanSweepWorkflow — op 2.2 (scheduler-triggered, 5m). Flags over-threshold
// variances; NO dispatch, NO auto-replan.
// ===========================================================================

// replanSweepInput is the start payload for ReplanSweepWorkflow.
type replanSweepInput struct {
	ProjectID *ProjectID // nil ⇒ sweep all in-flight projects
}

func (wf *workflows) ReplanSweepWorkflow(ctx workflow.Context, in replanSweepInput) (ReplanSweepResult, error) {
	// v1: the sweep reads the named project's head-state (the all-projects sweep is
	// a future fan-out — constructionManager.md §2.2). It surfaces over-threshold
	// variances; it never dispatches and never auto-replans. With no project named
	// (the all-sweep) it returns an empty (quiet) result — the per-project fan-out
	// is the documented follow-up, not a new façade op.
	if in.ProjectID == nil {
		return ReplanSweepResult{}, nil
	}

	proj, err := wf.readProject(ctx, *in.ProjectID)
	if err != nil {
		if isReadNotFound(err) {
			return ReplanSweepResult{}, nil
		}
		return ReplanSweepResult{}, err
	}

	flagged := wf.flagVariances(proj)
	return ReplanSweepResult{FlaggedVariances: flagged}, nil
}

// flagVariances surfaces over-threshold variances for the project. v1 surfaces an
// empty set unless an eligibility/variance helper is wired (the head-state
// variance-aggregate fill is the D-PA follow-up); the sweep's role is to SURFACE,
// never to auto-replan.
func (wf *workflows) flagVariances(_ projectstate.Project) []FlaggedVariance {
	return nil
}
