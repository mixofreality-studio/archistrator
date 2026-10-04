package delivery

import (
	"context"
	"encoding/json"
	"errors"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// This file holds the operator-supervision Signal payloads + the project-level
// supervision workflow that hosts the operator-pause branch (constructionManager.md
// §6.3 PauseProjectBranch + §6.2). The pause Signal (operatorPauseRequested) is a
// signal-with-start against {projectId}:construction; the supervision workflow
// resumes on awaitSignal, runs interventionEngine.applyPausePolicy → pausePlan
// (DECIDE), then the Manager EXECUTES the plan (recordOperatorPaused, then the relay
// to the pump; a pause cancels nothing in flight).

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
func (wf *csWorkflows) ProjectSupervisionWorkflow(ctx workflow.Context, in projectSupervisionInput) error {
	state := &constructState{projectID: in.ProjectID, stage: StageDispatching}
	if err := workflow.SetQueryHandler(ctx, querySessionState, func() (ConstructionSessionView, error) {
		return ConstructionSessionView{ProjectID: in.ProjectID, Stage: state.stage}, nil
	}); err != nil {
		return err
	}

	// RECEIVED INTO `any`, NOT INTO operatorPauseSignal (stage 4b3 Task 2). The producer
	// that reaches THIS execution sends a struct today, so nothing was being dropped — but
	// operatorPauseRequested is ALSO a messageBus.deliverSignal name (relayPauseToPump, just
	// below), and a bus payload is raw []byte tagged binary/plain, which a concrete-struct
	// target can hold not at all: the SDK logs "Corrupted signal" and drops the message
	// where no workflow can see it. WHICH producer reaches WHICH execution id is not a fact
	// this file can establish, so the wire form is decided by the NAME, and the name has a
	// bus producer. pauseSignalReason is the same normaliser the pump's own pause channel
	// uses, so the one signal name keeps one decode.
	pauseCh := workflow.GetSignalChannel(ctx, signalOperatorPauseRequested)
	var raw any
	pauseCh.Receive(ctx, &raw)
	reason, decoded := pauseSignalReason(raw)
	if !decoded {
		// A PAUSE WHOSE BODY CANNOT BE READ IS STILL A PAUSE. The channel NAME carries the
		// operator's intent, which is why this is the opposite of the lease channels' rule
		// (an undecodable lease message names no activity, so the pump drops it —
		// pumpDecodeSignal). The reason is lost; the pause is not.
		workflow.GetLogger(ctx).Error("supervision: the pause payload could not be read; pausing with no stated reason",
			"projectId", string(in.ProjectID))
	}

	return wf.runPauseBranch(ctx, in.ProjectID, reason, state)
}

// runPauseBranch runs the NCUC2 operator-pause branch: applyPausePolicy (DECIDE)
// then EXECUTE the plan (constructionManager.md §6.3). A pause cancels nothing in
// flight: it is recorded and relayed to the pump, whose cascade stops after the
// activity it is running. The plan's cancel list (and the agentic-job cancel verb it
// fed) was deleted in dct4 — nothing ever filled it, so no job was ever cancelled.
func (wf *csWorkflows) runPauseBranch(ctx workflow.Context, projectID ProjectID, reason string, state *constructState) error {
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
		// RECORD → RELAY (I2 ruling, 2026-09-12).
		//   - RECORD FIRST: the pause is durable in head-state before anything else, so a
		//     pump the 30s sweep (re)starts at any point from here on — including inside
		//     the relay window — reads it at its recorded-pause gate (pumpnextactivity.go)
		//     and goes quiet. If a later step fails, the pause STAYS recorded.
		//   - RELAY: PauseProject's signal lands HERE, on {projectId}:construction; a
		//     pump already cascading holds a head-state snapshot from before the record,
		//     so the relayed signal is what stops it (after its current activity).
		if err := wf.recordOperatorPaused(ctx, projectID, reason, plan); err != nil {
			return err
		}
		if err := wf.relayPauseToPump(ctx, projectID, reason); err != nil {
			return err
		}
	} else {
		// DefaultVersion: the pre-change sequence — record, no relay. (Its cancel step
		// scheduled no command: the cancel list was always empty.)
		if err := wf.recordOperatorPaused(ctx, projectID, reason, plan); err != nil {
			return err
		}
	}

	state.stage = StagePaused
	return nil
}

// recordOperatorPaused records the operator-paused head-state transition when the
// plan asks for it.
func (wf *csWorkflows) recordOperatorPaused(ctx workflow.Context, projectID ProjectID, reason string, plan intervention.PausePlan) error {
	if !plan.RecordPaused {
		return nil
	}
	headVersion := wf.readVersion(ctx, projectID)
	_, err := wf.applyRecovering(ctx, projectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		// Project-level pause has no per-activity minted cred: it is a projectStateAccess
		// verb, and that facet's cred-binding adapter mints just-in-time.
		return wf.Acts.ProjectStateRecordOperatorPaused(ctx, projectstate.ProjectID(projectID), expected, reason)
	})
	return err
}

// relayPauseToPump delivers the operator pause to the project's ONE pump
// (pumpWorkflowID) through the GENERATED messageBus.deliverSignal invoker. No pump
// running is the normal case for a paused-between-cascades project: the target is
// gone (never started, or closed quiet), which messageBus reports as RA NotFound —
// tolerated, since there is no cascade to stop. Every other delivery failure
// propagates.
func (wf *csWorkflows) relayPauseToPump(ctx workflow.Context, projectID ProjectID, reason string) error {
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
