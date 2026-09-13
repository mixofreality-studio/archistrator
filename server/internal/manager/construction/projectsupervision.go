package construction

import (
	"context"
	"encoding/json"
	"errors"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// This file holds the operator-supervision Signal payloads + the project-level
// supervision workflow that hosts the operator-pause branch (constructionManager.md
// §6.3 PauseProjectBranch + §6.2). The pause Signal (operatorPauseRequested) is a
// signal-with-start against {projectId}:construction; the supervision workflow
// resumes on awaitSignal, runs interventionEngine.applyPausePolicy → pausePlan
// (DECIDE), then the Manager EXECUTES the plan (cancelAgenticJob per
// pipeline + recordOperatorPaused).

// projectSupervisionInput is the start payload for the project-level supervision
// workflow. It is started (signal-with-start) by the pause Signal.
type projectSupervisionInput struct {
	ProjectID ProjectID
}

// ProjectSupervisionWorkflow hosts the project-level operator-pause branch and the
// project-level sessionState Query. It is a long-lived workflow keyed
// {projectId}:construction; the pause Signal is signal-with-start against it. This
// is a Manager-owned WORKFLOW TYPE (implementation), not a public façade op — the
// five public ops are unchanged (constructionManager.md §6.2).
func (wf *workflows) ProjectSupervisionWorkflow(ctx workflow.Context, in projectSupervisionInput) error {
	state := &constructState{projectID: in.ProjectID, stage: StageDispatching}
	if err := workflow.SetQueryHandler(ctx, querySessionState, func() (ConstructionSessionView, error) {
		return ConstructionSessionView{ProjectID: in.ProjectID, Stage: state.stage}, nil
	}); err != nil {
		return err
	}

	pauseCh := workflow.GetSignalChannel(ctx, signalOperatorPauseRequested)
	var sig operatorPauseSignal
	pauseCh.Receive(ctx, &sig)

	return wf.runPauseBranch(ctx, in.ProjectID, sig.Reason, state)
}

// runPauseBranch runs the NCUC2 operator-pause branch: applyPausePolicy (DECIDE)
// then EXECUTE the plan (constructionManager.md §6.3). InFlightPipelines is left
// unset (zero value) — the retired pauseRequestContext mirror never populated it
// either (zero behavior change).
func (wf *workflows) runPauseBranch(ctx workflow.Context, projectID ProjectID, reason string, state *constructState) error {
	plan, perr := wf.Intervention.ApplyPausePolicy(fweng.Context{Context: context.Background()}, intervention.PauseRequestContext{
		ProjectID: intervention.ProjectID(projectID),
		Reason:    reason,
		// Policy threading added in the seam cleanup — the retired adapter omitted it,
		// which made the real engine reject every pause with "unknown policy mode".
		// Deliberate fix, see seam-cleanup Task 6 disclosure (task-6-report.md) and
		// Test_Pause_RealInterventionEngine_PolicyThreaded_ApplyPausePolicySucceeds /
		// Test_ApplyPausePolicy_ZeroValuePolicy_IsTheOldBug (workflow_test.go).
		Policy: wf.InterventionPolicy,
	})
	if perr != nil {
		return fwmanager.MapError(perr)
	}

	// EXECUTE. GetVersion ("pause-relays-to-pump") pins supervision runs already inside
	// this branch at deploy to the pre-change sequence.
	if workflow.GetVersion(ctx, "pause-relays-to-pump", workflow.DefaultVersion, 1) >= 1 {
		// RECORD → RELAY → CANCEL (I2 ruling, 2026-09-12).
		//   - RECORD FIRST: the pause is durable in head-state before anything else, so a
		//     pump the 30s sweep (re)starts at any point from here on — including inside
		//     the relay window — reads it at its recorded-pause gate (pumpnextactivity.go)
		//     and goes quiet. If a later step fails, the pause STAYS recorded.
		//   - RELAY: PauseProject's signal lands HERE, on {projectId}:construction; a
		//     pump already cascading holds a head-state snapshot from before the record,
		//     so the relayed signal is what stops it (after its current activity).
		//   - CANCEL the in-flight pipelines the plan names, last.
		if err := wf.recordOperatorPaused(ctx, projectID, reason, plan); err != nil {
			return err
		}
		if err := wf.relayPauseToPump(ctx, projectID, reason); err != nil {
			return err
		}
		if err := wf.cancelPlannedPipelines(ctx, plan); err != nil {
			return err
		}
	} else {
		// DefaultVersion: the pre-change sequence, exactly as main had it — cancel, then
		// record, no relay.
		if err := wf.cancelPlannedPipelines(ctx, plan); err != nil {
			return err
		}
		if err := wf.recordOperatorPaused(ctx, projectID, reason, plan); err != nil {
			return err
		}
	}

	state.stage = StagePaused
	return nil
}

// cancelPlannedPipelines cancels each in-flight pipeline the pause plan names
// (GENERATED cancel invoker). PipelineRef is a published named-string type; cast to the
// Manager's own opaque pipelineHandle.Name (string) — NotifyTargets/ResumeHint stay
// unread, same as the retired pausePlan mirror never converting them into anything
// downstream read.
func (wf *workflows) cancelPlannedPipelines(ctx workflow.Context, plan intervention.PausePlan) error {
	for _, pid := range plan.PipelinesToCancel {
		if err := wf.cancelPipeline(ctx, pipelineHandle{Name: string(pid)}); err != nil {
			return err
		}
	}
	return nil
}

// recordOperatorPaused records the operator-paused head-state transition when the
// plan asks for it.
func (wf *workflows) recordOperatorPaused(ctx workflow.Context, projectID ProjectID, reason string, plan intervention.PausePlan) error {
	if !plan.RecordPaused {
		return nil
	}
	headVersion := wf.readVersion(ctx, projectID)
	_, err := wf.applyRecovering(ctx, projectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		// Project-level pause has no per-activity minted cred; the cred-binding git
		// adapter mints just-in-time and the local store ignores an empty cred.
		return wf.Acts.ConstructionTransitionRecordOperatorPaused(ctx, projectstate.ProjectID(projectID), expected, reason, railCredEnvelope{}.toProjectState())
	})
	return err
}

// relayPauseToPump delivers the operator pause to the project's ONE pump
// (pumpWorkflowID) through the GENERATED messageBus.deliverSignal invoker. No pump
// running is the normal case for a paused-between-cascades project: the target is
// gone (never started, or closed quiet), which messageBus reports as RA NotFound —
// tolerated, since there is no cascade to stop. Every other delivery failure
// propagates.
func (wf *workflows) relayPauseToPump(ctx workflow.Context, projectID ProjectID, reason string) error {
	payload, err := pumpPausePayload(projectID, reason)
	if err != nil {
		return err
	}
	err = wf.Acts.MessageBusDeliverSignal(ctx,
		messagebus.ExecutionID(pumpWorkflowID(projectID)),
		messagebus.SignalName(signalOperatorPauseRequested),
		payload)
	if err != nil && !isSignalTargetNotFound(err) {
		return err
	}
	return nil
}

// pumpPausePayload is the ONE wire encoding of a pause relayed to the pump: the
// operatorPauseSignal as JSON bytes. messageBus transports the bytes verbatim
// (binary/plain); the pump's pumpPauseRequested decodes them. Deterministic
// (json.Marshal of a plain struct) — safe in-workflow.
func pumpPausePayload(projectID ProjectID, reason string) (messagebus.ExecutionPayload, error) {
	b, err := json.Marshal(operatorPauseSignal{ProjectID: projectID, Reason: reason})
	if err != nil {
		return messagebus.ExecutionPayload{}, err
	}
	return messagebus.ExecutionPayload{Bytes: b}, nil
}

// isSignalTargetNotFound reports whether a messageBus.deliverSignal Activity failed
// because the target execution does not exist (RA NotFound, surfaced through the
// Activity boundary as a non-retryable ApplicationError of that type).
func isSignalTargetNotFound(err error) bool {
	var appErr *temporal.ApplicationError
	return errors.As(err, &appErr) && appErr.Type() == raNotFoundErrType
}

// cancelPipeline calls the GENERATED cancel invoker (idempotent-on-intent in the RA).
func (wf *workflows) cancelPipeline(ctx workflow.Context, handle pipelineHandle) error {
	return wf.Acts.PipelineCancelAgenticJob(ctx, agenticjob.ParsePipelineHandle(handle.Name))
}
