package construction

import (
	"fmt"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ===========================================================================
// PumpSweepWorkflow — Schedule-triggered entry (30s), platform-wide (Task 7c).
//
// NOT one of the five frozen public façade ops (constructionManager.md §2) —
// ExecuteNextActivity stays the single-project entry a caller (or this sweep)
// drives. A Temporal Schedule action carries a FIXED workflow type + FIXED args on
// every firing (messagebus.go's RegisterSchedule), so it cannot itself vary
// pumpInput.ProjectID per tick the way ExecuteNextActivity's client-driven call
// does. PumpSweepWorkflow is the thin fan-out this forces: enumerate every
// construction-phase project (projectStateAccess.listProjects), then start — or,
// if a prior tick's cascade for that project is still running, leave alone — that
// project's own PumpNextActivityWorkflow, unchanged. Mirrors ReplanSweepWorkflow's
// structure (replansweep.go) and billingManager's ShortfallSweepWorkflow's
// enumerate-then-fan-out shape (shortfallsweep.go).
// ===========================================================================

// pumpSweepInput is the start payload for PumpSweepWorkflow — platform-wide, no
// scope to carry (contrast replanSweepInput's optional *ProjectID narrowing: the
// pump sweep has no single-project mode, since the whole point is enumeration).
type pumpSweepInput struct{}

// pumpSweepResult is this tick's fan-out outcome. Deliberately UNEXPORTED:
// PumpSweepWorkflow is not a façade op (nothing reads its result through
// ConstructionManager — the Schedule fires it and no caller awaits it), so it
// carries no service-contract entry, unlike the generated, exported PumpResult /
// ReplanSweepResult the frozen façade ops return.
type pumpSweepResult struct {
	// PumpedProjects is every construction-phase project this tick started (or
	// found already cascading) a pump for.
	PumpedProjects []ProjectID
}

// pumpSweepOwnerScope is the placeholder OwnerScope the sweep enumerates under.
// projectStateAccess.ListProjects requires a non-empty owner (fwra.ContractMisuse
// otherwise), but BOTH real catalog implementations discard the value entirely:
// the local arm (localProjectCatalog.ListProjectRepos, projectstateaccess.go)
// takes it as `_ OwnerScope`, and the cloud arm (sourcecontrol.access.
// ListProjectRepos) resolves purely through the GitHub-App installation account,
// never the caller's owner string. ListProjects's own doc comment (projectstate
// access.go) even anticipates exactly this: "a caller may pass a wildcard/
// placeholder scope (e.g. \"{}\")". Any non-empty constant behaves identically;
// this one is chosen for readability in logs/traces.
const pumpSweepOwnerScope = projectstate.OwnerScope("platform-sweep")

// pumpSweepChildWorkflowID derives the STABLE (tick-invariant) per-project pump id
// the sweep starts its child against: "{projectId}:nextActivity". Deliberately
// DIFFERENT in shape from pumpWorkflowID's client-driven "{projectId}:nextActivity:
// {tickId}" (which always carries a third, non-empty tickId segment) — the two id
// spaces can never collide. The fixed id is what lets a still-cascading prior
// tick's execution absorb a redundant firing (a benign "already started" — see
// PumpSweepWorkflow) instead of a duplicate cascade racing the same project.
func pumpSweepChildWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:nextActivity", projectID)
}

func (wf *workflows) PumpSweepWorkflow(ctx workflow.Context, _ pumpSweepInput) (pumpSweepResult, error) {
	logger := workflow.GetLogger(ctx)

	summaries, err := wf.Acts.ProjectStateListProjects(ctx, pumpSweepOwnerScope)
	if err != nil {
		return pumpSweepResult{}, err
	}

	result := pumpSweepResult{PumpedProjects: []ProjectID{}}
	for _, s := range summaries {
		// Eligibility mirrors nextEligibleActivity's own Phase gate exactly (the
		// per-project pump is already a quiet no-op for any other phase) —
		// filtering HERE just avoids spawning a quiet no-op child every tick for
		// every system-design/project-design-phase project on the platform.
		if s.Phase != projectstate.PhaseConstruction {
			continue
		}
		projectID := ProjectID(s.ProjectID)
		cctx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:        pumpSweepChildWorkflowID(projectID),
			ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_ABANDON,
		})
		child := workflow.ExecuteChildWorkflow(cctx, executionKindPump, pumpInput{ProjectID: projectID})
		// Wait only for the START ack (NOT completion — the pump's own self-cascade
		// can run long; the sweep must stay short so the 30s cadence is not blocked).
		var childWE workflow.Execution
		if serr := child.GetChildWorkflowExecution().Get(ctx, &childWE); serr != nil {
			if temporal.IsWorkflowExecutionAlreadyStartedError(serr) {
				// This project's prior tick is still cascading — exactly the outcome
				// wanted (no duplicate cascade racing the same project), not a failure.
				logger.Info("pump sweep: project already cascading, skipped", "projectId", string(projectID))
				continue
			}
			return pumpSweepResult{}, serr
		}
		result.PumpedProjects = append(result.PumpedProjects, projectID)
	}
	logger.Info("pump sweep tick complete", "pumped", len(result.PumpedProjects))
	return result, nil
}
