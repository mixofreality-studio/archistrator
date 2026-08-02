// Package projectdesign is the projectDesignManager component of the aiarch
// server's Manager layer — the use-case façade that drives a project through
// Phase 2 of The Method (Project Design), per the senior-passed contract
// designs/aiarch/implementation/contracts/projectDesignManager.md (D-MPD,
// APPROVED — FROZEN 2026-05-29). It is the Phase-2 TWIN of the systemdesign
// package (D-MSD) and mirrors it file-by-file.
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal / Query), it defines and registers one Activity
// per ResourceAccess call, owns the Signal/Query handlers, and derives the
// idempotency key "${workflowId}:${activityId}" passed down to each RA verb.
// Temporal lives ONLY in this component; the downstream Engines
// (estimation, operationestimation, settlement) and ResourceAccess (projectstate,
// worker) ports are Temporal-free, and the three estimate Engines are PURE — called
// DIRECTLY from workflow code, never wrapped in an Activity (contract §6.3/§6.4).
//
// SCHEMA-FIRST (full encapsulation): this component OWNS its contract I/O types.
// The public surface (ProjectDesignManager port + the I/O value types) is GENERATED
// into contract.gen.go from this component's `.serviceContracts` entry in
// .aiarch/state/project.json (edit that entry + `make gen`; do
// NOT hand-edit the generated surface). The generated contract imports NEITHER the
// projectstate ResourceAccess NOR Temporal: projectdesign mirrors the head-state
// value shapes (ProjectID / ArtifactKind / OptionID) as its OWN named types and
// field-maps from projectstate at the Manager boundary (the systemdesign precedent).
// The staged typed DRAFT (and the assembled SDP review) is carried OPAQUELY — a
// {kind, model} envelope (DraftModel) — so projectdesign never regenerates or shares
// projectstate's sealed ArtifactModel sum or its 17 variants.
//
// The consumer-side dependency interfaces (AgenticJobAccess /
// SourceControlRail), the Temporal workflows struct + workflow inputs/signals, and
// the internal SDP-assembly (assembleSdpReview over projectstate.Project) stay
// HAND-WRITTEN and are NOT part of the generated contract.
//
// File layout within the package:
//   - projectdesignmanager.go : the Manager + the ProjectDesignManager port (§6.2)
//   - contract.go             : the public façade types (§2, §3) — generated surface
//   - behavior.go             : free functions over the contract value types
//   - workflow.go             : the workflows deps struct + workflow bodies + signal/query handlers (§6.3)
//   - activities.go           : the Manager-owned Activity wrappers, as methods on workflows (§6.4)
//   - errors.go               : the port-error -> Temporal-error translation (§6.4)
//   - prompts.go              : the Phase-2 architect-role draft prompt corpus
//   - worker.go               : worker registration of workflows + activities (§6.1)
package projectdesign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"path"
	"strings"
	"sync/atomic"
	"time"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	billing "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// ProjectDesignManager is the generated service-contract interface for this component
// — the public use-case surface of the projectDesignManager façade
// (projectDesignManager.md §2). Each op leads with the Manager-layer call Context
// (fwmanager.Context, embedding context.Context + the Principal); the *projectDesignManager derives
// ctx := rc.Context inside. The concrete *projectDesignManager satisfies it; the consumer-side
// dependency seams (agenticJobAccess / sourceControlRail) + the Temporal
// workflows struct stay hand-written and are NOT part of this contract.

// Compile-time proof the concrete projectDesignManager satisfies the generated
// ProjectDesignManager port.
var _ ProjectDesignManager = (*projectDesignManager)(nil)

// projectDesignManager is the projectDesignManager façade. It exposes the public
// use-case ops (projectDesignManager.md §2) and OWNS Temporal. It is the Phase-2 twin
// of the systemdesign Manager. The Temporal-backed ops:
//   - RequestArtifactDraft   — Workflow (entry, per-artifact CoAuthorPhase2ArtifactWorkflow)
//   - RequestSDPCommit       — Workflow (entry, AssembleSDPReviewWorkflow)
//   - SubmitSDPDecision      — Signal (sdpDecision, to the SDP-review workflow)
//   - AdvanceToConstruction  — Workflow (entry, short-lived Phase-2 seal)
//   - GetSessionState        — Query (sessionState, read-only)
//
// plus SubmitReviewDecision — Signal (reviewDecision, the per-artifact OQ-3 gate).
//
// Each op leads with the Manager-layer call Context (fwmanager.Context, embedding
// context.Context + the Principal); the *projectDesignManager derives ctx :=
// rc.Context inside. Pre-condition checks the contract puts on the façade (Phase-2
// kind, non-empty projectId, Commit-requires-optionId, RejectAll-requires-feedback)
// are enforced here before any downstream call (§2, §3).
//
// The façade methods themselves use ONLY the Temporal client. It ALSO stores the
// Worker-side deps it was constructed with — the published
// projectstate.ProjectStateAccess (head-state read-back + thin writes), the published
// agenticjob.AgenticJobAccess (Phase-2 design-job dispatch), the
// published sourcecontrol.SourceControlAccess (the PR rail), the three estimation
// Engines (the in-workflow SDP-assembly join), and the per-project repo resolver — so
// RegisterWorker can wire them (via the package's folded adapters) into the
// hand-written Temporal workflows. The former exported consumer-mirror interfaces +
// the composition-root adapters are RETIRED; the manager now depends on the deps'
// PUBLISHED interfaces and adapts them internally (Option-B boundary mapping).
type projectDesignManager struct {
	client       client.Client
	projectState projectstate.ProjectStateAccess
	pipeline     agenticjob.AgenticJobAccess
	rail         sourcecontrol.SourceControlAccess
	estimator    estimation.EstimationEngine
	opEstimator  operationestimation.OperationEstimationEngine
	settlement   billing.BillingEngine

	// designSession (B6) is the generated designSessionAccess dep. Since B9, every
	// branch-scoped design flow EXCEPT StageArtifactForReview reaches it through the
	// generated invoker surface (invokers.gen.go/activities.gen.go, via wf.Acts) —
	// read-back, commit/reject/withdraw, and the review-ledger set/seed verbs. Stage
	// alone stays on the manager-local capability-fallback custom Activity
	// (activities_custom.go): the generated invoker's `model` parameter is the sealed
	// projectstate.ArtifactModel interface, which Temporal's default JSON DataConverter
	// cannot decode across the wire (verified; see activities_custom.go's file doc).
	designSession projectstate.DesignSessionAccess

	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	// episodes (SP1 capture-seam) is the generated episodeAccess dep — the agentic-
	// episode ledger every terminal design dispatch appends to. The WORKFLOW paths reach
	// it through the generated invoker surface (wf.Acts.EpisodesAppendEpisode); this
	// field is held for two reasons: to thread it into genActivities, and because the
	// answer-job capture (answerEpisodeWatch) runs MANAGER-SIDE, outside any workflow,
	// and must call the RA directly.
	episodes episode.EpisodeAccess
}

// newProjectDesignManager is the hand-written, unexported builder the generated
// NewProjectDesignManager constructor delegates to. It wires the Temporal client + the
// published deps into the façade. The façade itself uses only client; projectState /
// pipeline / rail / the three estimators / repo are stored for RegisterWorker (rail
// may be nil — a dev server with no source-control credentials runs the design spine
// repo-less).
func newProjectDesignManager(
	c client.Client,
	projectState projectstate.ProjectStateAccess,
	pipeline agenticjob.AgenticJobAccess,
	rail sourcecontrol.SourceControlAccess,
	estimator estimation.EstimationEngine,
	opEstimator operationestimation.OperationEstimationEngine,
	settle billing.BillingEngine,
	designSession projectstate.DesignSessionAccess,
	episodes episode.EpisodeAccess,
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool),
) *projectDesignManager {
	return &projectDesignManager{
		client:        c,
		projectState:  projectState,
		pipeline:      pipeline,
		rail:          rail,
		estimator:     estimator,
		opEstimator:   opEstimator,
		settlement:    settle,
		designSession: designSession,
		episodes:      episodes,
		repo:          repo,
	}
}

// RequestArtifactDraft — op 2.1. Temporal Workflow (entry; StartWorkflow /
// signal-with-start), workflow id {projectId}:{artifactKind}. Idempotent on the id.
//
// Pre: projectID non-nil; kind is a Phase-2 kind AND != KindSdpReview (the SDP
// review is assembled via RequestSDPCommit, not co-authored). The spine-ordering gate
// (the requested kind's immediate Phase-2 predecessor must be Committed) is enforced
// here on head-state — the wire-side mirror of the SPA's Phase-2 buildSpine step lock —
// so a raw API/MCP caller cannot draft out of order (the CoAuthorPhase2ArtifactWorkflow
// itself never gated ordering; it drafts immediately). The first Phase-2 kind
// (planningAssumptions) has no Phase-2 predecessor.
// amendmentIndexFor PROMOTED to projectstate.AmendmentIndexFor (code-health-phase-bd task
// D3) — byte-identical pure resolver, no longer duplicated with systemdesign's twin. It
// returns the AMENDMENT index for a draft request against slot: the count of prior
// commits, used as the …-amend-N branch suffix and the "revision N" prompt framing, and
// the signal that gates the amendment path (fresh -amend-N branch, amendment prompt, and
// review-ledger SEED of the reopening feedback).

func (m *projectDesignManager) RequestArtifactDraft(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, feedback *ReviewFeedback) (SessionRef, error) {
	ctx := rc.Context
	if projectID == "" {
		return "", newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) {
		return "", newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-2 kind")
	}
	if kind == KindSdpReview {
		return "", newError(fwmanager.FailedPrecondition, "use requestSDPCommit for the SDP review")
	}

	// Spine-ordering gate (Phase-2 twin of the systemdesign Manager). A Phase-2 kind
	// may only be drafted once its immediate predecessor in the Phase-2 sequence is
	// Committed (the same order the SPA's PHASE2_ORDER locks by).
	if err := m.checkPhase2Predecessor(ctx, projectID, kind); err != nil {
		return "", err
	}

	// F-R2 (Phase-2 port): the generating guard + WEDGED-run supersede. Refuse the request
	// while the live session is Drafting/Redrafting (a buffered redraft signal would later
	// stale-consume a recovery gate), and TERMINATE a wedged RUNNING run so the SignalWithStart
	// below starts a fresh run instead of binding the signal to a corpse. Ports the 2026-07-16
	// systemdesign fixes that were never mirrored here.
	if err := m.prepareForDraftRequest(rc, projectID, kind); err != nil {
		return "", err
	}

	// F38 BACK-EDGE / AMENDMENT (Phase-2 twin). A draft request on an already-COMMITTED
	// Phase-2 artifact is the legal amendment path: fresh session on a …-amend-N branch
	// (N = the slot's prior commit count) with the reopening feedback seeded into its ledger.
	// A non-committed slot keeps today's behavior (active session redraft / fresh draft).
	amendment := 0
	if proj, rerr := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID)); rerr == nil {
		amendment = projectstate.AmendmentIndexFor(slotFor(proj, toPSKind(kind)))
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		// F-R2 (Phase-2 port): a session whose previous run CLOSED (committed/withdrawn →
		// amendment/fresh draft, or died abnormally) must be revivable — this SignalWithStart
		// STARTS a brand-new run. ALLOW_DUPLICATE is the server default; pinned explicitly
		// because the dead-session recovery path depends on it (a stricter policy silently
		// turns "Retry" into a no-op 200).
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}
	in := coAuthorInput{ProjectID: projectID, ArtifactKind: kind, Feedback: feedback, Amendment: amendment}

	// F47: DELIVER the feedback via the redraft SIGNAL, not a bare ExecuteWorkflow. A draft
	// request against an ALREADY-RUNNING session (the retry-at-failed-gate path — the session
	// is suspended at StageDraftFailed awaiting a decision) resolves USE_EXISTING to the running
	// run; a plain ExecuteWorkflow returns that handle WITHOUT delivering `in`, so the request's
	// feedback was silently DROPPED and the redraft repeated the same mistake. SignalWithStart
	// delivers the redraft signal (carrying the feedback) to the running session's gate AND, when
	// no run is live (fresh start / amendment on a committed→closed slot), starts a new run with
	// `in` (whose Feedback the spine seeds into the first prompt). This mirrors the systemdesign
	// Manager. The gate MERGES the signal feedback with any retained feedback (request wins).
	we, err := m.client.SignalWithStartWorkflow(ctx, wfID, signalRedraft, redraftSignal{Feedback: feedback}, opts, executionKindCoAuthor, in)
	if err != nil {
		return "", mapStartError(err)
	}
	// F-R2 (Phase-2 port): NO FALSE 200s. SignalWithStart's return alone cannot distinguish
	// "fresh run started" from "signal bound to something that will never act", so VERIFY the
	// session's latest execution is now live. Best-effort — only a confirmed abnormal-closed
	// latest run is refused (a Describe blip never masks a genuine start).
	if err := m.verifySessionRevived(ctx, wfID); err != nil {
		return "", err
	}
	return newSessionRef(we.GetID()), nil
}

// verifySessionRevived confirms the co-author session's LATEST execution is not sitting
// abnormally CLOSED right after a SignalWithStart (F-R2 Phase-2 port) — the honest-error
// backstop for the false-200 revival failure. Describe errors are ignored (best-effort; the
// start already durably succeeded).
func (m *projectDesignManager) verifySessionRevived(ctx context.Context, wfID string) error {
	desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, "")
	if derr != nil {
		return nil
	}
	if status := desc.GetWorkflowExecutionInfo().GetStatus(); isAbnormalClosedStatus(status) {
		return newError(fwmanager.Infrastructure,
			"the design session could not be revived — the previous session ended abnormally and no fresh run started; restart the phase or try again")
	}
	return nil
}

// wedgedSupersedeReason is the Temporal termination reason recorded when Retry supersedes a
// WEDGED design run (F-R2 Phase-2 port). Human-readable so the run's close event explains why.
const wedgedSupersedeReason = "superseded by Retry: workflow task stuck in failed state"

// prepareForDraftRequest is the pre-SignalWithStart gate for RequestArtifactDraft (F-R2
// Phase-2 port; mirrors systemdesign). It probes the live session directly (Describe + Query)
// so it can SUPERSEDE a WEDGED run — one whose workflow task is perpetually failing shows
// RUNNING to Describe but rejects the sessionState query with the wedged signature, and a
// SignalWithStart with USE_EXISTING would only BUFFER the redraft signal on that corpse
// forever. On exactly that shape, TERMINATE the wedged run (tolerating a NotFound race) so the
// subsequent SignalWithStart starts a fresh run. Termination is gated STRICTLY on the wedged
// classification — a transient query fault falls through to the normal receptive check and
// surfaces as today's error, never a terminate. Every non-wedged outcome keeps the established
// checkDraftRequestReceptive behavior.
func (m *projectDesignManager) prepareForDraftRequest(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) error {
	ctx := rc.Context
	wfID := coAuthorWorkflowID(projectID, kind)
	desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, "")
	if derr != nil {
		if isNotFound(derr) {
			return nil // no session yet — this request starts the first one
		}
		// A Describe blip (non-NotFound): fall back to the query-based receptive check
		// rather than masking a transient fault as receptive.
		return m.checkDraftRequestReceptive(rc, projectID, kind)
	}
	// A non-RUNNING execution (abnormal-closed / completed / paused) is receptive: the
	// SignalWithStart either revives a fresh run or the durable slot is already terminal —
	// none of those is a live Drafting/Redrafting the redraft signal could stale-consume.
	if desc.GetWorkflowExecutionInfo().GetStatus() != enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return nil
	}
	// A RUNNING execution: query its live stage — this ONE query ALSO detects the WEDGED shape.
	enc, qerr := m.client.QueryWorkflow(ctx, wfID, "", querySessionState)
	if qerr != nil {
		if isWorkflowTaskFailedQueryErr(qerr) {
			// Wedged RUNNING run — supersede it so the SignalWithStart starts a fresh run.
			// Tolerate a NotFound (it closed between the query and here); any other terminate
			// fault is surfaced so the caller never silently binds the signal to the corpse.
			if terr := m.client.TerminateWorkflow(ctx, wfID, "", wedgedSupersedeReason); terr != nil && !isNotFound(terr) {
				return newError(fwmanager.Infrastructure,
					"could not supersede the stuck design session before retrying: "+terr.Error())
			}
			return nil // proceed to SignalWithStart (starts a fresh run)
		}
		if isNotFound(qerr) {
			return nil // raced to closed between Describe and Query — the start revives it
		}
		return mapQueryError(qerr) // transient — surface, never terminate
	}
	var view SessionStateView
	if err := enc.Get(&view); err != nil {
		return newError(fwmanager.Infrastructure, err.Error())
	}
	// The generating guard: a live Drafting/Redrafting session is NOT receptive (a redraft
	// signal would sit buffered and later stale-consume a recovery gate).
	if view.Stage == StageDrafting || view.Stage == StageRedrafting {
		return newError(fwmanager.FailedPrecondition,
			"a draft is already generating for this artifact (currently "+sessionStageLabel(view.Stage)+") — wait for it to finish before requesting another")
	}
	return nil
}

// checkDraftRequestReceptive is the manager-side generating guard for RequestArtifactDraft
// (F-R2 Phase-2 port): reject the request while the live session's stage is Drafting or
// Redrafting — a redraft signal sent then is consumable by NO open gate and would sit buffered
// until it stale-consumes a later recovery gate. The stage is read through GetSessionState —
// the SAME Describe-then-Query path (a dead run synthesizes StageDraftFailed, a COMPLETED run
// is rebuilt from the durable slot, a live run answers the sessionState query) — so the refusal
// always agrees with what the founder sees on screen. NotFound (no session yet) is receptive:
// the request STARTS the first session. Purely a manager-side precondition — replay-safe.
func (m *projectDesignManager) checkDraftRequestReceptive(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) error {
	view, err := m.GetSessionState(rc, projectID, kind)
	if err != nil {
		var me *fwmanager.Error
		if errors.As(err, &me) && me.Kind == fwmanager.NotFound {
			return nil // no session yet — this request starts one
		}
		return err
	}
	switch view.Stage {
	case StageDrafting, StageRedrafting:
		return newError(fwmanager.FailedPrecondition,
			"a draft is already generating for this artifact (currently "+sessionStageLabel(view.Stage)+") — wait for it to finish before requesting another")
	default:
		return nil
	}
}

// checkPhase2Predecessor enforces the Phase-2 spine-ordering gate for a draft request:
// the requested kind's immediate predecessor (per phase2PredecessorKind) must be
// Committed on head-state. Returns nil when the gate is satisfied — the first Phase-2
// kind (planningAssumptions) has no predecessor, so it always passes without a read,
// mirroring the SPA which unlocks planningAssumptions without a sealed Phase 1; a
// redraft of an already in-review / Committed kind also passes (its predecessor is
// committed by construction). Returns FailedPrecondition naming the uncommitted
// predecessor otherwise. Extracted so the gate is unit-testable without a Temporal
// client. Only checks the Phase-2 order (slots 8..16); Phase-1 sealing is the
// Phase2AdvanceWorkflow's concern.
func (m *projectDesignManager) checkPhase2Predecessor(ctx context.Context, projectID ProjectID, kind ArtifactKind) error {
	pred, ok := phase2PredecessorKind(kind)
	if !ok {
		return nil
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isRAReadNotFound(err) {
			// A brand-new project with no head-state row: no slot is committed, so
			// the predecessor is by definition uncommitted.
			return newError(fwmanager.FailedPrecondition, predecessorNotCommittedMsg(pred))
		}
		return mapReadProjectError(err)
	}
	if slotFor(proj, toPSKind(pred)).Status != projectstate.ReviewCommitted {
		return newError(fwmanager.FailedPrecondition, predecessorNotCommittedMsg(pred))
	}
	return nil
}

// RequestSDPCommit — op 2.2. Temporal Workflow (entry; StartWorkflow /
// signal-with-start), workflow id {projectId}:sdpReview. Idempotent on the id
// (UseExisting): a redundant start (or a replan re-entry) reuses the running
// SDP-review workflow.
func (m *projectDesignManager) RequestSDPCommit(rc fwmanager.Context, projectID ProjectID) (SessionRef, error) {
	ctx := rc.Context
	if projectID == "" {
		return "", newError(fwmanager.ContractMisuse, "empty projectId")
	}

	wfID := sdpReviewWorkflowID(projectID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	in := sdpReviewInput{ProjectID: projectID}

	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindSDPReview, in)
	if err != nil {
		return "", mapStartError(err)
	}
	return newSessionRef(we.GetID()), nil
}

// SubmitSDPDecision — op 2.3. Temporal Signal (SignalWorkflow to workflow id
// {projectId}:sdpReview, signal sdpDecision).
//
// Validate: decision ∈ {SDPCommit, SDPRejectAll}; SDPCommit requires a non-empty
// optionID (ContractMisuse otherwise); SDPRejectAll requires feedback with
// non-empty Notes (ContractMisuse otherwise).
func (m *projectDesignManager) SubmitSDPDecision(rc fwmanager.Context, projectID ProjectID, decision SDPDecision, optionID *OptionID, feedback *ReviewFeedback) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	switch decision {
	case SDPCommit:
		if optionID == nil || *optionID == "" {
			return newError(fwmanager.ContractMisuse, "Commit requires a non-empty optionId")
		}
	case SDPRejectAll:
		if feedback == nil || feedback.Notes == "" {
			return newError(fwmanager.ContractMisuse, "RejectAll requires feedback")
		}
	case SDPDecisionUnknown:
		// The zero value: a caller that forgot to set Decision, not a legitimate
		// SDP outcome. Reject explicitly rather than falling through silently.
		return newError(fwmanager.ContractMisuse, "unknown SDP decision")
	default:
		return newError(fwmanager.ContractMisuse, "unknown SDP decision")
	}

	wfID := sdpReviewWorkflowID(projectID)
	// PM-P2-4: capture the acting identity for the SdpReview commit's approvedBy provenance.
	sig := sdpDecisionSignal{Decision: decision, OptionID: optionID, Feedback: feedback, Approver: principalLabel(rc.Principal)}
	if err := m.client.SignalWorkflow(ctx, wfID, "", signalSDPDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// SubmitReviewDecision — the per-artifact Phase-2 review gate (OQ-3). Temporal
// Signal (SignalWorkflow to workflow id {projectId}:{artifactKind}, signal
// reviewDecision). feedback required when decision == Reject. kind must be a
// Phase-2 kind other than the SDP review.
func (m *projectDesignManager) SubmitReviewDecision(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, decision ReviewDecision, feedback *ReviewFeedback) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) || kind == KindSdpReview {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a co-authored Phase-2 kind")
	}
	switch decision {
	case ReviewApprove, ReviewWithdraw:
		// ok
	case ReviewReject:
		if feedback == nil || feedback.Notes == "" {
			return newError(fwmanager.ContractMisuse, "Reject requires feedback")
		}
	case ReviewDecisionUnknown:
		// The zero value: a caller that forgot to set Decision, not a legitimate
		// review outcome. Reject explicitly rather than falling through silently.
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	default:
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	}

	wfID := coAuthorWorkflowID(projectID, kind)

	// F19: precondition — inspect the live session stage BEFORE signaling. A bare
	// SignalWorkflow is fire-and-forget: an approve/reject delivered while the session
	// is drafting, already committed, or was never started is silently BUFFERED or
	// dropped by the workflow (at the failed-recovery gate ReviewApprove is explicitly
	// ignored), yet the op returns success {} — a no-op masquerading as a decision.
	// Query the stage first and refuse a decision the current gate cannot honor with a
	// FailedPrecondition naming the actual stage. (Mirrors systemdesign's F19 fix.)
	view, live, err := m.reviewGateView(ctx, wfID)
	if err != nil {
		return err
	}
	if perr := checkReviewPrecondition(decision, view.Stage); perr != nil {
		return perr
	}
	// DEAD-SESSION HONESTY (F-R2 Phase-2 port). An abnormally-CLOSED or WEDGED run synthesizes a
	// StageDraftFailed view (so the SPA renders the failed card), which PASSES the reject/
	// withdraw precondition above — but a signal to that corpse can never be honored (Temporal
	// refuses it, or the wedged run never processes it). Refuse with an actionable
	// FailedPrecondition instead: the ONLY lever on a dead session is requestArtifactDraft
	// ("Retry"), which starts a fresh run. Ordered AFTER the precondition so a never-started
	// session keeps its "not started" message (checkReviewPrecondition refuses at
	// SessionStageUnknown). NOTE (F-R2 asymmetry with systemdesign 2.1e): the systemdesign twin
	// additionally honors a Withdraw against a dead session whose slot is staged on MAIN; the
	// spec's 2.1f enumeration did not list that scoped withdraw for Phase-2, so it is NOT ported
	// here — flagged for the architect.
	if !live {
		return newError(fwmanager.FailedPrecondition,
			"the design session for this artifact is no longer running (it ended abnormally) — review decisions cannot reach it. Use \"Retry\" to start a fresh session, then decide on its review gate")
	}
	// REVIEW LEDGER (review-ledger §4): approve is blocked while any comment is still open —
	// the reviewer must address (redraft) or waive each first. The message lists the open ids.
	if decision == ReviewApprove {
		if open := openReviewCommentViewIDs(view.ReviewThread); len(open) > 0 {
			return newError(fwmanager.FailedPrecondition,
				fmt.Sprintf("cannot approve: %d review comment(s) still open (%s) — address or waive them first", len(open), strings.Join(open, ", ")))
		}
	}

	// PM-P2-4: capture the acting reviewer identity for the commit's approvedBy provenance.
	sig := reviewDecisionSignal{Decision: decision, Feedback: feedback, Approver: principalLabel(rc.Principal)}
	if err := m.client.SignalWorkflow(ctx, wfID, "", signalReviewDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// reviewGateView returns the session's full gate view (stage + durable review thread) for the
// F19 review precondition AND the review-ledger approve/waive preconditions, plus whether a
// LIVE workflow can still honor a signal (F-R2 Phase-2 port). Same dead-workflow defense as
// GetSessionState: a CLOSED-ABNORMAL run reports StageDraftFailed with live=false (a signal to
// it can never be honored), a WEDGED run likewise (live=false), a missing execution reports
// SessionStageUnknown, and a live run is read from the authoritative sessionState query.
func (m *projectDesignManager) reviewGateView(ctx context.Context, wfID string) (SessionStateView, bool, error) {
	describeLive := false
	if desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, ""); derr == nil {
		if status := desc.GetWorkflowExecutionInfo().GetStatus(); isAbnormalClosedStatus(status) {
			return SessionStateView{Stage: StageDraftFailed}, false, nil
		}
		describeLive = true
	} else if isNotFound(derr) {
		return SessionStateView{Stage: SessionStageUnknown}, false, nil
	}
	enc, err := m.client.QueryWorkflow(ctx, wfID, "", querySessionState)
	if err != nil {
		if isNotFound(err) {
			return SessionStateView{Stage: SessionStageUnknown}, false, nil
		}
		// F-R2: a WEDGED run cannot honor a signal any more than a closed one — return
		// live=false with the failed stage so the !live refusal (which points the human at
		// Retry) fires instead of a raw 5xx. Only when Describe CONFIRMED the run live; a
		// Describe blip + task-failed stays a retryable Infrastructure error.
		if describeLive && isWorkflowTaskFailedQueryErr(err) {
			return SessionStateView{Stage: StageDraftFailed}, false, nil
		}
		return SessionStateView{}, false, mapQueryError(err)
	}
	var view SessionStateView
	if err := enc.Get(&view); err != nil {
		return SessionStateView{}, false, newError(fwmanager.Infrastructure, err.Error())
	}
	return view, true, nil
}

// SetReviewCommentStatus applies a human status transition to one durable review-ledger
// comment (review-ledger §4): waive an OPEN comment to dismiss it, or reopen an ADDRESSED
// comment to send it back for another redraft. Mirrors SubmitReviewDecision's F19 shape — a
// synchronous precondition check via the sessionState query before signaling the (fire-and-
// forget) branch mutation.
func (m *projectDesignManager) SetReviewCommentStatus(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, commentID string, status string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) || kind == KindSdpReview {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a co-authored Phase-2 kind")
	}
	if commentID == "" {
		return newError(fwmanager.ContractMisuse, "empty commentId")
	}
	switch status {
	case projectstate.ReviewCommentWaived, projectstate.ReviewCommentOpen:
		// waive (open->waived) or reopen (addressed->open) — the only human-authored transitions.
	default:
		return newError(fwmanager.ContractMisuse, "status must be \"waived\" (to dismiss an open comment) or \"open\" (to reopen an addressed comment)")
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	view, live, err := m.reviewGateView(ctx, wfID)
	if err != nil {
		return err
	}
	// A dead (abnormally-closed/wedged) session synthesizes StageDraftFailed and a
	// never-started one SessionStageUnknown — both are !AwaitingReview, so folding !live into
	// this check refuses them with the same honest message (no separate !live branch needed).
	if view.Stage != StageAwaitingReview || !live {
		return newError(fwmanager.FailedPrecondition,
			"cannot change a review comment: the design is not awaiting review (current stage: "+sessionStageLabel(view.Stage)+")")
	}
	if perr := checkCommentTransition(view.ReviewThread, commentID, status); perr != nil {
		return perr
	}

	sig := setCommentStatusSignal{CommentID: commentID, Status: status}
	if err := m.client.SignalWorkflow(ctx, wfID, "", signalSetCommentStatus, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// openReviewCommentViewIDs returns the ids of every OPEN CHANGE-REQUEST in a wire thread —
// the approve blocker set. Open QUESTIONS are excluded (a soft approve-gate warning, never a
// hard block; question-comments §approve).
func openReviewCommentViewIDs(thread []ReviewCommentView) []string {
	var ids []string
	for _, c := range thread {
		if c.Status == projectstate.ReviewCommentOpen && c.Type != projectstate.ReviewCommentTypeQuestion {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// checkCommentTransition validates a human status transition against the live thread: the
// comment must exist and the transition must be legal (open->waived, addressed->open).
func checkCommentTransition(thread []ReviewCommentView, id, status string) error {
	for _, c := range thread {
		if c.ID != id {
			continue
		}
		switch {
		case c.Status == projectstate.ReviewCommentOpen && status == projectstate.ReviewCommentWaived:
			return nil
		case c.Status == projectstate.ReviewCommentAddressed && status == projectstate.ReviewCommentOpen:
			return nil
		default:
			return newError(fwmanager.FailedPrecondition,
				fmt.Sprintf("cannot change comment %s from %q to %q (allowed: open->waived, addressed->open)", id, c.Status, status))
		}
	}
	return newError(fwmanager.FailedPrecondition, "review comment "+id+" not found in the thread")
}

// checkReviewPrecondition enforces that the submitted decision is meaningful at the
// session's current stage (F19): approve is honored only at StageAwaitingReview;
// reject and withdraw are honored at StageAwaitingReview OR the StageDraftFailed
// recovery gate (where reject means retry-with-feedback — see awaitDraftFailedRecovery).
// Any other stage yields a FailedPrecondition naming the actual stage.
func checkReviewPrecondition(decision ReviewDecision, stage SessionStage) error {
	switch decision {
	case ReviewApprove:
		if stage != StageAwaitingReview {
			return newError(fwmanager.FailedPrecondition,
				"cannot approve: the design is not awaiting review (current stage: "+sessionStageLabel(stage)+")")
		}
	case ReviewReject:
		if stage != StageAwaitingReview && stage != StageDraftFailed {
			return newError(fwmanager.FailedPrecondition,
				"cannot send back: the design is not at a review or recovery gate (current stage: "+sessionStageLabel(stage)+")")
		}
	case ReviewWithdraw:
		if stage != StageAwaitingReview && stage != StageDraftFailed {
			return newError(fwmanager.FailedPrecondition,
				"cannot withdraw: no review or recovery gate is open (current stage: "+sessionStageLabel(stage)+")")
		}
	case ReviewDecisionUnknown:
		// Unreachable: SubmitReviewDecision rejects the zero value as ContractMisuse
		// before reaching the precondition. Guarded for switch-exhaustiveness.
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	}
	return nil
}

// sessionStageLabel renders a SessionStage as a short human label for the precondition
// messages.
func sessionStageLabel(s SessionStage) string {
	switch s {
	case SessionStageUnknown:
		return "not started"
	case StageDrafting:
		return "drafting"
	case StageAssemblingSDP:
		return "assembling SDP"
	case StageAwaitingReview:
		return "awaiting review"
	case StageRedrafting:
		return "redrafting"
	case StageCommitted:
		return "committed"
	case StageWithdrawn:
		return "withdrawn"
	case StageRefused:
		return "refused"
	case StageDraftFailed:
		return "draft failed"
	}
	// Unreachable for the nine defined SessionStage values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a
	// defensive fallback for an out-of-range ordinal.
	return "unknown"
}

// AdvanceToConstruction — op 2.4. Temporal Workflow (entry; StartWorkflow,
// workflow id {projectId}:phaseAdvance). Returns the gating outcome.
//
// F55 STALE-SLOT GATE (Phase-2 twin). A back-edge amendment flags every downstream committed
// slot StaleBasis. Sealing Phase 2 over a stale committed slot silently advances to
// construction on a shifted basis. Before starting the seal workflow, refuse with
// FailedPrecondition naming the stale in-scope (Phase-2) slots — UNLESS the caller explicitly
// acknowledges (acknowledgeStale). The message names the slots so a consumer knows what to
// reconcile.
func (m *projectDesignManager) AdvanceToConstruction(rc fwmanager.Context, projectID ProjectID, acknowledgeStale bool) (PhaseAdvanceResult, error) {
	ctx := rc.Context
	if projectID == "" {
		return PhaseAdvanceResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}

	if !acknowledgeStale {
		if proj, rerr := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID)); rerr == nil {
			if stale := staleCommittedPhase2Kinds(proj); len(stale) > 0 {
				return PhaseAdvanceResult{}, newError(fwmanager.FailedPrecondition,
					fmt.Sprintf("cannot advance to construction: %d committed artifact(s) are stale and must be reconciled first (%s). Re-run the design for each, or advance anyway by acknowledging the staleness.",
						len(stale), strings.Join(stale, ", ")))
			}
		}
	}

	wfID := phaseAdvanceWorkflowID(projectID)
	opts := client.StartWorkflowOptions{
		ID:        wfID,
		TaskQueue: TaskQueue,
	}
	in := phaseAdvanceInput{ProjectID: projectID}

	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindPhaseAdvance, in)
	if err != nil {
		return PhaseAdvanceResult{}, mapStartError(err)
	}

	var result PhaseAdvanceResult
	if err := we.Get(ctx, &result); err != nil {
		return PhaseAdvanceResult{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return result, nil
}

// GetSessionState — op 2.5. Temporal Query (QueryWorkflow, query sessionState,
// read-only). When kind == KindSdpReview, queries {projectId}:sdpReview; otherwise
// {projectId}:{kind}.
func (m *projectDesignManager) GetSessionState(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) (SessionStateView, error) {
	ctx := rc.Context
	if projectID == "" {
		return SessionStateView{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	var wfID string
	if kind == KindSdpReview {
		wfID = sdpReviewWorkflowID(projectID)
	} else {
		wfID = coAuthorWorkflowID(projectID, kind)
	}

	// F15/F28 + P0-2 (query-side defense, Phase-2 twin). A CoAuthor/SDP workflow answers the
	// sessionState Query by HISTORY-REPLAY even after it has CLOSED, returning its last in-
	// memory stage. For a run that died ABNORMALLY that replayed value lies "drafting in
	// progress" and wedges the SPA on an infinite "GENERATING" screen; for a run that closed
	// NORMALLY (COMPLETED) after committing (or withdrawing) it can ALSO be a stale mid-flight
	// StageDrafting — the same wedge on a SUCCESSFUL, long-committed artifact. Describe the
	// execution first: an abnormal-closed run synthesizes an honest StageDraftFailed view; a
	// COMPLETED run is rebuilt from the durable slot on main (committed slot → StageCommitted +
	// the committed model; any other terminal → honest terminal, never Drafting). A RUNNING /
	// CONTINUED_AS_NEW run (incl. an amendment's fresh run) falls through to the live query,
	// which is authoritative for those. A Describe error other than NotFound is best-effort:
	// fall through to the query rather than masking a transient Describe blip as a failure.
	//
	// describeLive (F-R2) records that Describe CONFIRMED a live execution — only then is a
	// task-failed query below trustworthy as the WEDGED signal (a Describe blip is not).
	describeLive := false
	if desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, ""); derr == nil {
		switch status := desc.GetWorkflowExecutionInfo().GetStatus(); {
		case isAbnormalClosedStatus(status):
			// F-R2 durable-slot-first: a run can die AFTER its artifact landed on main (a
			// died amendment attempt, or a death just after CommitArtifact), so consult the
			// durable slot before falling back to the failed card (see abnormalClosedSessionView).
			view, err := m.abnormalClosedSessionView(ctx, projectID, kind, status)
			if err != nil {
				return SessionStateView{}, err
			}
			return withStageName(view), nil
		case status == enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
			view, err := m.completedSessionView(ctx, projectID, kind)
			if err != nil {
				return SessionStateView{}, err
			}
			return withStageName(view), nil
		}
		// Describe succeeded and the run is neither abnormal-closed nor completed — a LIVE
		// execution (RUNNING / CONTINUED_AS_NEW / PAUSED). A task-failed query below is now
		// trustworthy as the wedged signal.
		describeLive = true
	} else if isNotFound(derr) {
		return SessionStateView{}, newError(fwmanager.NotFound, "project design has not started for this project")
	}

	enc, err := m.client.QueryWorkflow(ctx, wfID, "", querySessionState)
	if err != nil {
		// F20 (error altitude): before Phase 2 the co-author/SDP workflow does not
		// exist, and Temporal's raw "workflow not found for ID: <proj>:<n>" leaks the
		// internal execution id to the client. Map that to a clean, user-altitude
		// NotFound; other query faults keep their generic mapping.
		if isNotFound(err) {
			return SessionStateView{}, newError(fwmanager.NotFound, "project design has not started for this project")
		}
		// F-R2: a WEDGED run (workflow task perpetually failing) shows RUNNING to the Describe
		// above but rejects this query with the wedged signature. Do NOT surface a 5xx that
		// leaves the SPA on an infinite GENERATING screen — synthesize the honest failed card
		// so the human can Retry, which supersedes the stuck run (prepareForDraftRequest). Only
		// when Describe CONFIRMED the run live; a Describe blip + task-failed stays a retryable
		// Infrastructure error.
		if describeLive && isWorkflowTaskFailedQueryErr(err) {
			return withStageName(wedgedSessionView(projectID, kind)), nil
		}
		return SessionStateView{}, mapQueryError(err)
	}
	var view SessionStateView
	if err := enc.Get(&view); err != nil {
		return SessionStateView{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return withStageName(view), nil
}

// withStageName stamps the F72 human-readable StageName label alongside the bare Stage int
// on the public SessionStateView, using sessionStageLabel as the single authoritative map
// (the Phase-2 stage enum values DIFFER from Phase-1's, so the label removes the ambiguity).
// Applied at the GetSessionState boundary; StageName is purely additive to the wire shape.
func withStageName(v SessionStateView) SessionStateView {
	v.StageName = sessionStageLabel(v.Stage)
	return v
}

// principalLabel renders a security.Principal as a short human-facing label for PM-P2-4
// provenance (approvedBy): username (GitHub login / preferred_username), else email, else
// display name, else the opaque subject (dev-mode identity). Empty when no identity was
// resolved — the commit then records no approvedBy (absent provenance is allowed).
func principalLabel(p security.Principal) string {
	switch {
	case p.Username != "":
		return p.Username
	case p.Email != "":
		return p.Email
	case p.Name != "":
		return p.Name
	default:
		return p.Subject
	}
}

// staleCommittedPhase2Kinds returns the wire names of every COMMITTED Phase-2 slot that carries
// StaleBasis (a back-edge amendment invalidated its basis) — the set AdvanceToConstruction must
// refuse to seal over unless the caller acknowledges. Order follows Phase2RequiredKinds so the
// message reads deterministically.
func staleCommittedPhase2Kinds(proj projectstate.Project) []string {
	var stale []string
	for _, kind := range projectstate.Phase2RequiredKinds() {
		slot := slotFor(proj, kind)
		if slot.Status == projectstate.ReviewCommitted && slot.StaleBasis {
			stale = append(stale, kind.WireName())
		}
	}
	return stale
}

// isAbnormalClosedStatus reports whether a workflow-execution status is a CLOSED-ABNORMAL
// terminal state — the session died without a clean commit/withdraw. A normally COMPLETED
// or still-RUNNING (or CONTINUED_AS_NEW) execution is NOT abnormal.
func isAbnormalClosedStatus(s enumspb.WorkflowExecutionStatus) bool {
	switch s {
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return true
	case enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED,
		enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
		enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW,
		enumspb.WORKFLOW_EXECUTION_STATUS_PAUSED:
		return false
	default:
		return false
	}
}

// failedSessionView synthesizes the human-visible failed view for a session whose workflow
// died abnormally. It reuses StageDraftFailed — the SAME terminal-failure stage the live
// anti-wedge gate uses — so the SPA renders its existing "design job failed → retry / withdraw"
// card. Carries a neutral human FailureReason.
func failedSessionView(projectID ProjectID, kind ArtifactKind, status enumspb.WorkflowExecutionStatus) SessionStateView {
	reason := terminatedSessionReason(status)
	return SessionStateView{
		ProjectID:     projectID,
		ArtifactKind:  kind,
		Stage:         StageDraftFailed,
		Draft:         DraftModel{Kind: artifactKindWireName(kind)},
		FailureReason: &reason,
	}
}

// completedSessionView derives the honest session view for a CoAuthor/SDP run that closed
// NORMALLY (COMPLETED). The replayed sessionState query is NOT trusted for such a run (it can
// return a stale mid-flight stage — the P0-2 "GENERATING forever" wedge on an already-committed
// artifact), so the view is rebuilt from the DURABLE slot on main.
func (m *projectDesignManager) completedSessionView(ctx context.Context, projectID ProjectID, kind ArtifactKind) (SessionStateView, error) {
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return SessionStateView{}, mapReadProjectError(err)
	}
	return committedSessionView(projectID, kind, slotFor(proj, toPSKind(kind)))
}

// committedSessionView projects the durable slot of a COMPLETED session onto a
// SessionStateView. A committed slot renders the committed view (StageCommitted + the committed
// model + the durable review thread). A withdrawn slot renders StageWithdrawn. Any other
// terminal-but-uncommitted state renders an honest StageDraftFailed terminal carrying a neutral
// reason — NEVER StageDrafting, so the SPA never wedges on an infinite "GENERATING" spinner.
func committedSessionView(projectID ProjectID, kind ArtifactKind, slot projectstate.ArtifactSlot) (SessionStateView, error) {
	switch slot.Status {
	case projectstate.ReviewCommitted:
		draft, err := draftModelFor(kind, slot.Model)
		if err != nil {
			return SessionStateView{}, newError(fwmanager.Infrastructure, err.Error())
		}
		return SessionStateView{
			ProjectID:    projectID,
			ArtifactKind: kind,
			Stage:        StageCommitted,
			Draft:        draft,
			ReviewThread: reviewThreadToView(slot.ReviewThread),
		}, nil
	case projectstate.ReviewWithdrawn:
		return SessionStateView{
			ProjectID:    projectID,
			ArtifactKind: kind,
			Stage:        StageWithdrawn,
			Draft:        DraftModel{Kind: artifactKindWireName(kind)},
		}, nil
	case projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected:
		// Any non-committed / non-withdrawn terminal status renders the honest
		// StageDraftFailed view (never StageDrafting — the anti-wedge rule).
		fallthrough
	default:
		reason := "the design session ended without committing an artifact. Retry to start a fresh draft."
		return SessionStateView{
			ProjectID:     projectID,
			ArtifactKind:  kind,
			Stage:         StageDraftFailed,
			Draft:         DraftModel{Kind: artifactKindWireName(kind)},
			FailureReason: &reason,
		}, nil
	}
}

// wedgedSessionView synthesizes the honest failed card for a WEDGED run (F-R2): the workflow
// task is perpetually failing, so the sessionState query cannot answer even though the run
// still reports RUNNING to Describe. It reuses StageDraftFailed (the SPA's retry/withdraw
// card), with copy promising that Retry supersedes the stuck session — which
// prepareForDraftRequest actually does (terminate-then-SignalWithStart).
func wedgedSessionView(projectID ProjectID, kind ArtifactKind) SessionStateView {
	reason := "the design session hit an internal fault and cannot answer — Retry to start a fresh draft (the stuck session will be superseded)"
	return SessionStateView{
		ProjectID:     projectID,
		ArtifactKind:  kind,
		Stage:         StageDraftFailed,
		Draft:         DraftModel{Kind: artifactKindWireName(kind)},
		FailureReason: &reason,
	}
}

// abnormalClosedSessionView derives the honest view for a session whose workflow ended
// ABNORMALLY (FAILED/TERMINATED/TIMED_OUT/CANCELED). Durable-slot-first (F-R2): a run can die
// AFTER its artifact landed on main (a died amendment attempt, or a death just after
// CommitArtifact), so consult main's slot before falling back to the failed card:
//
//   - Committed → the committed view (StageCommitted + the model) CARRYING a FailureReason so
//     the last session's abnormal end stays visible; the committed view's amend affordance IS
//     the retry, so this un-deadlocks the died-amendment case with ZERO writes.
//   - Withdrawn → the withdrawn view.
//   - anything else (the run died before committing) → today's failed card, preserving the
//     anti-wedge fix for a first-draft death.
//
// If the durable slot cannot be consulted — the store is unavailable (nil) or the read
// faults — it falls back to the failed card rather than erroring or panicking: never wedge on
// a recovery read (the failed card still offers Retry), matching the pre-fix behavior exactly.
func (m *projectDesignManager) abnormalClosedSessionView(ctx context.Context, projectID ProjectID, kind ArtifactKind, status enumspb.WorkflowExecutionStatus) (SessionStateView, error) {
	if m.projectState == nil {
		return failedSessionView(projectID, kind, status), nil
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return failedSessionView(projectID, kind, status), nil
	}
	slot := slotFor(proj, toPSKind(kind))
	switch slot.Status {
	case projectstate.ReviewCommitted, projectstate.ReviewWithdrawn:
		view, verr := committedSessionView(projectID, kind, slot)
		if verr != nil {
			return SessionStateView{}, verr
		}
		if slot.Status == projectstate.ReviewCommitted {
			reason := "the last design session ended unexpectedly (" + workflowStatusLabel(status) + "); the committed model shown is unaffected"
			view.FailureReason = &reason
		}
		return view, nil
	default:
		return failedSessionView(projectID, kind, status), nil
	}
}

// terminatedSessionReason renders the neutral human "why" for a session whose workflow died
// abnormally.
func terminatedSessionReason(status enumspb.WorkflowExecutionStatus) string {
	return "the design session ended unexpectedly and is no longer running (" + workflowStatusLabel(status) + "). Retry to start a fresh draft."
}

// workflowStatusLabel maps an abnormal-closed status to a short, infrastructure-neutral label
// for the failed card.
func workflowStatusLabel(s enumspb.WorkflowExecutionStatus) string {
	switch s {
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return "the job failed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "the job timed out"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return "the job was terminated"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return "the job was canceled"
	case enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED,
		enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
		enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW,
		enumspb.WORKFLOW_EXECUTION_STATUS_PAUSED:
		return "the job stopped"
	}
	// Unreachable for the nine defined enumspb.WorkflowExecutionStatus values above
	// (the exhaustive linter enforces that every real variant has its own case);
	// kept as a defensive fallback for an out-of-range ordinal (e.g. a future
	// Temporal SDK addition not yet triaged here).
	return "the job stopped"
}

// --- error mapping at the façade boundary -----------------------------------

// isRAReadNotFound reports whether err is a RAW projectStateAccess fwra.NotFound
// (a brand-new / unknown project) returned DIRECTLY on the sync façade read path —
// distinct from workflow.go's isReadNotFound, which inspects the Temporal-wrapped
// ApplicationError on the replayed Activity path.
func isRAReadNotFound(err error) bool {
	var raErr *fwra.Error
	return errors.As(err, &raErr) && raErr.Kind == fwra.NotFound
}

// mapReadProjectError converts a projectStateAccess.ReadProject error on the sync
// spine-ordering-gate read path into a fwmanager.Error: fwra.NotFound → NotFound
// (unknown project), everything else → Infrastructure. (fwra.NotFound is handled
// specially by the RequestArtifactDraft caller as an uncommitted predecessor; this
// mapper covers the non-NotFound faults.)
func mapReadProjectError(err error) error {
	if isRAReadNotFound(err) {
		return newError(fwmanager.NotFound, err.Error())
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

func mapStartError(err error) error {
	// A "workflow already started" race under UseExisting policy is benign; the
	// SDK returns the existing handle without error. Any error here is treated as a
	// infrastructure fault.
	return newError(fwmanager.Infrastructure, err.Error())
}

func mapSignalError(err error) error {
	if isNotFound(err) {
		return newError(fwmanager.NotFound, err.Error())
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

func mapQueryError(err error) error {
	if isNotFound(err) {
		return newError(fwmanager.NotFound, err.Error())
	}
	// A session whose workflow task is FAILING (e.g. a deploy-time non-determinism
	// fault being retried) rejects queries with the raw Temporal internals
	// "Unable to query workflow due to Workflow Task in failed state" (observed on
	// the systemdesign twin, gtdapp:5). Same error-hygiene rule as the 065a9e7
	// not-found cleanup: clients get a clean, actionable Detail.
	if isWorkflowTaskFailedQueryErr(err) {
		return newError(fwmanager.Infrastructure,
			"design session state is temporarily unavailable — the session hit an internal fault and is being retried by the server; try again shortly")
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// isWorkflowTaskFailedQueryErr reports whether a QueryWorkflow error is the WEDGED-RUN
// signature (F-R2): the workflow task is perpetually failing, so Temporal rejects the
// sessionState query with "...Workflow Task in failed state" EVEN THOUGH
// DescribeWorkflowExecution still reports the run RUNNING. This classification (1) lets
// GetSessionState / reviewGateView synthesize an honest failed view instead of a 5xx, and
// (2) authorizes RequestArtifactDraft to TERMINATE the wedged run before starting a fresh one.
// A transient query timeout/Unavailable does NOT match — it must never trigger a terminate.
func isWorkflowTaskFailedQueryErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Workflow Task in failed state")
}

// isNotFound reports whether the Temporal error indicates the addressed execution
// does not exist — typed as *serviceerror.NotFound, the canonical "no such
// workflow" error the SDK returns (mirrors systemdesign's matcher).
//
// QA 2026-07-19 (poll-404 wizard reset): the old substring match ("not found")
// classified *serviceerror.NamespaceNotFound — the server talking to a
// wrong/foreign Temporal backend — as the authoritative session/execution
// NotFound, which clients trust and act on destructively. Only the typed
// execution-NotFound may claim absence; everything else stays Infrastructure.
func isNotFound(err error) bool {
	var notFound *serviceerror.NotFound
	return errors.As(err, &notFound)
}

// ---------------------------------------------------------------------------
// Identity / domain scalars (projectdesign's OWN named types — value-identical to
// projectstate; the Manager converts at the projectStateAccess boundary). They are
// PURE DATA on the generated surface; behavior lives in behavior.go as free
// functions so contract.gen.go imports no projectstate.
// ---------------------------------------------------------------------------

// ProjectID is the project aggregate identifier — its value IS the user-supplied
// adopted repo name (name-as-identity). Mirrors projectstate.ProjectID.

// OptionID names one project-design option in the SDP review (the architect commits
// one at the option-commitment gate). Mirrors projectstate.OptionID.

// ArtifactKind is the closed artifact-slot enum. The ordinals MIRROR
// projectstate.ArtifactKind so int(...) conversion at the boundary is
// meaning-preserving; behavior (WireName/IsPhase2/...) lives in behavior.go as free
// functions over a projectstate conversion so the generated type stays pure data.

// ---- Phase 1 (carried for ordinal parity with projectstate; not driven here) ----

// ---- Phase 2 ----

// ---------------------------------------------------------------------------
// Session reference + review surface.
// ---------------------------------------------------------------------------

// SessionRef is an opaque, infrastructure-opaque reference to a running Phase-2
// session (an artifact-co-authoring session or the SDP-review session — contract
// §3.1). It wraps the underlying durable-execution identity as an opaque string the
// Client persists/echoes and never parses. Construction is via the newSessionRef
// free function (behavior.go).

// ReviewDecision is the architect's commit-authority decision at the per-artifact
// Phase-2 review gate (contract §10 OQ-3 — Phase-2 artifacts ARE individually
// gated, mirroring Phase 1).

// commit the typed model in its slot
// loop back to draft with feedback
// abandon the draft

// ReviewFeedback is the architect's free-text rejection/withdraw rationale
// (contract §3.2). Required on Reject and on an SDP RejectAll; optional on
// Withdraw; ignored on Approve.

// SDPDecision is the architect's decision at the option-commitment gate
// (contract §3.2). Commit binds the named option; RejectAll re-enters Phase 2
// with feedback to produce a fresh SDP review.

// bind the named option, commit the review
// record the rejected outcome; re-assemble with feedback

// PhaseAdvanceResult is the gating outcome of advanceToConstruction
// (contract §3.3). A non-Advanced result is the NORMAL "you still owe artifacts
// X, Y / no option bound" answer (not an error).

// ---------------------------------------------------------------------------
// Session read view (getSessionState) + the OPAQUE staged-draft envelope.
//
// DraftModel is the discriminated {kind, model} envelope the staged typed draft /
// assembled SdpReview is carried as — IDENTICAL on the wire to the systemdesign
// DraftModel envelope. The model is carried OPAQUELY as raw JSON: projectdesign
// never names the concrete projectstate model types or the sealed ArtifactModel sum
// here.
// ---------------------------------------------------------------------------

// DraftModel is the opaque {kind, model} envelope carrying the staged typed draft (or
// the assembled SdpReview) as raw JSON. Model is omitted when no draft is staged.
// Kind is the canonical camelCase wire name (e.g. "planningAssumptions").

// SessionStage collapses the technical workflow state into the handful of stages
// the UI needs (contract §3.4). StageAssemblingSDP sits between drafting and
// awaiting-review for the SDP-review session.

// worker dispatched; typed model not yet produced
// SDP-review workflow: assembling options + joining Engine outputs
// model staged (status AwaitingReview); suspended on the review signal
// architect rejected; looping back with feedback
// commitArtifact applied; terminal for this kind/option
// withdrawArtifact applied; terminal
// worker refused/cancelled and could not produce a model; terminal
// StageDraftFailed (agentic-pivot D-MPD-Δ, §3.4 — the twin of systemDesignManager
// StageDraftFailed) is the human-visible, human-actionable stage the session lands
// in when the dispatched agentic Phase-2 DESIGN job reaches a TYPED terminal failure
// phase. It carries the job's neutral Diagnostic in FailureReason. Surfaced by
// getSessionState so the SPA renders an actionable failure and NEVER a perpetual
// StageDrafting / StageAssemblingSDP spinner (the anti-wedge requirement).

// SessionStateView is a point-in-time, read-only view of one Phase-2 session's
// TECHNICAL progress (contract §3.4) — the answer to getSessionState (a Temporal
// Query), NOT the business-state read. The staged TYPED draft / assembled SdpReview
// is carried OPAQUELY via DraftModel; Findings explain "why it's being redrafted".

// Draft is the staged typed draft / SdpReview awaiting review, carried as the
// opaque {kind, model} envelope (model nil before the first stage).

// FailureReason is a short, human, non-leaking explanation set ONLY when Stage is
// StageDraftFailed (a terminal Phase-2 design-job failure). It gives the SPA a
// message + recovery affordance instead of a wedged "generating" screen. Empty
// (nil) otherwise.

// ---------------------------------------------------------------------------
// Façade error model (projectDesignManager.md §3.5).
// These are CALLER/PROGRAMMER errors at the façade boundary — distinct from the
// workflow's own failure handling. Kinds follow the framework-go standard set.
// ---------------------------------------------------------------------------

func newError(kind fwmanager.Kind, detail string) *fwmanager.Error {
	return fwmanager.New(kind, detail)
}

// behavior.go holds the FREE FUNCTIONS that carry behavior over the contract value
// types. The generated contract surface (contract.gen.go) is PURE DATA — enums and
// structs with no methods — so any logic over a contract value (the canonical-name
// lookups that used to be methods on the projectstate enums, the opaque SessionRef
// constructor) lives here as a free function.
//
// projectdesign's OWN ArtifactKind mirrors projectstate.ArtifactKind ordinal-for-
// ordinal, so its behavior is derived by a meaning-preserving int conversion to the
// canonical projectstate type rather than re-implemented here. This is the Phase-2
// twin of systemdesign/behavior.go.

// newSessionRef constructs a SessionRef from an infrastructure identity. Internal to
// the Manager; Clients only ever receive and echo SessionRefs.
func newSessionRef(opaque string) SessionRef { return SessionRef(opaque) }

// toPSKind converts projectdesign's OWN ArtifactKind to the canonical
// projectstate.ArtifactKind (ordinal-preserving) for behavior + RA-boundary calls.
func toPSKind(k ArtifactKind) projectstate.ArtifactKind { return projectstate.ArtifactKind(k) }

// fromPSKind converts a canonical projectstate.ArtifactKind to projectdesign's OWN
// ArtifactKind (ordinal-preserving) at the read boundary.
func fromPSKind(k projectstate.ArtifactKind) ArtifactKind { return ArtifactKind(k) }

// artifactKindString returns the PascalCase Go-identifier name for an ArtifactKind
// (the dispatch-input + PR-title + diagnostic form). Mirrors projectstate String().
func artifactKindString(k ArtifactKind) string { return toPSKind(k).String() }

// artifactKindWireName returns the canonical camelCase wire name for an ArtifactKind.
func artifactKindWireName(k ArtifactKind) string { return toPSKind(k).WireName() }

// artifactKindIsPhase2 reports whether the kind belongs to The Method's Phase 2.
func artifactKindIsPhase2(k ArtifactKind) bool { return toPSKind(k).IsPhase2() }

// phase2RequiredKinds returns the ordered set of Phase-2 artifact kinds (projectdesign's
// OWN type), mirroring projectstate.Phase2RequiredKinds() — the same order the SPA's
// PHASE2_ORDER locks steps by.
func phase2RequiredKinds() []ArtifactKind {
	ps := projectstate.Phase2RequiredKinds()
	out := make([]ArtifactKind, 0, len(ps))
	for _, k := range ps {
		out = append(out, fromPSKind(k))
	}
	return out
}

// phase2PredecessorKind returns the Phase-2 kind that must be Committed immediately
// before `kind` may be drafted — the wire-side mirror of the SPA's Phase-2 buildSpine
// step lock. The first required kind (planningAssumptions) has no predecessor and
// returns (_, false); a kind not in the Phase-2 set likewise returns (_, false).
func phase2PredecessorKind(kind ArtifactKind) (ArtifactKind, bool) {
	req := phase2RequiredKinds()
	for i, k := range req {
		if k == kind {
			if i == 0 {
				return 0, false
			}
			return req[i-1], true
		}
	}
	return 0, false
}

// predecessorNotCommittedMsg is the FailedPrecondition detail naming the uncommitted
// predecessor that blocks the requested draft (by its canonical camelCase wire name).
func predecessorNotCommittedMsg(pred ArtifactKind) string {
	return fmt.Sprintf("predecessor artifact %q must be committed before this kind can be drafted", artifactKindWireName(pred))
}

// strPtrOrNil maps a failure-reason string to the optional contract field: nil for
// the empty string (omitted on the wire), &s otherwise (the project notesPtr pattern).
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// This file used to OWN the Manager's serialization of the sealed
// projectstate.ArtifactModel sum across the Temporal Activity boundary. That wire
// codec (modelEnvelope/slotEnvelope/projectEnvelope + EncodeModel/EncodeProject/Decode)
// is now PROMOTED DOWN into projectstate (envelope.go) — designSessionAccess absorbed
// the branch/ledger/provenance capability chains this Manager's custom activities
// (activities_custom.go) used to run over optional ProjectStateAccess extensions, and
// the envelope moved with them so ReadProjectOnBranch can return it directly (a
// concrete, Temporal-serializable projection).
//
// The three type names below are ALIASES to the projectstate types, so every existing
// declaration/field/call site in this package keeps compiling unchanged EXCEPT the
// Decode method call sites: aliasing preserves type identity but not method-name
// casing, and the promoted methods are EXPORTED (Decode, not decode) — those call
// sites were updated in lockstep with this move.
type (
	modelEnvelope   = projectstate.ModelEnvelope
	projectEnvelope = projectstate.ProjectEnvelope
)

// draftModelFor builds the OPAQUE public DraftModel envelope ({kind, model}) the
// session read carries the staged typed draft (or assembled SdpReview) as. Kind is
// the artifactKind's canonical camelCase wire name (always set, so the SPA gets
// {"kind":"planningAssumptions"} even before a draft is staged); Model is the concrete
// model's own JSON, omitted when nil. This is the public-surface twin of modelEnvelope
// (the Temporal/Activity carrier) — the same {kind, model} wire shape the SPA decodes,
// with Kind as a plain string so the generated contract carries no projectstate
// ArtifactKind.
func draftModelFor(kind ArtifactKind, model projectstate.ArtifactModel) (DraftModel, error) {
	env := DraftModel{Kind: artifactKindWireName(kind)}
	if model != nil {
		raw, err := json.Marshal(model)
		if err != nil {
			return DraftModel{}, fmt.Errorf("encode draft model %s: %w", model.Kind(), err)
		}
		rm := json.RawMessage(raw)
		env.Model = &rm
	}
	return env, nil
}

// encodeModel delegates to the promoted projectstate.EncodeModel. Kept as a
// package-level wrapper (rather than rewriting every call site to the qualified name)
// so this move stays a minimal, mechanical diff.
func encodeModel(model projectstate.ArtifactModel) (modelEnvelope, error) {
	return projectstate.EncodeModel(model)
}

// encodeProject wraps the head-state aggregate for the Temporal boundary, delegating
// to the promoted projectstate.EncodeProject.
//
// F16 (payload slimming): the Phase-1 ResearchInput corpus is DELIBERATELY NOT
// carried here — projectstate.EncodeProject leaves ProjectEnvelope.Research nil by
// default and this wrapper does NOT opt in (unlike systemdesign's own encodeProject).
// A research source can be a whole book (660KB observed), and every projectdesign
// Activity payload crosses the Temporal boundary — dead weight that pushes toward
// Temporal's 2MB kill threshold. Phase-2 project design never reads the corpus (unlike
// systemdesign, whose mission-draft step legitimately weaves it in — that Manager's
// envelope opts in), so dropping the field costs nothing here.
func encodeProject(p projectstate.Project) (projectEnvelope, error) {
	return projectstate.EncodeProject(p)
}

// findings.go owns the SESSION-TRANSIENT validation-finding value types this Manager
// surfaces on its getSessionState read (SessionStateView.Findings). The SPA renders
// findings[] to explain "why it's being redrafted". They are part of this component's
// OWN generated contract surface (registered in cmd/schemagen) — pure data, no methods.
//
// Defined LOCALLY (mirroring systemdesign/findings.go) because a Manager importing
// another Manager is a sideways edge the layer model forbids (TestMethodLayering
// NoSideways); systemdesign and projectdesign each own their own copy.
//
// WIRE: severity is a camelCase STRING name ("info"|"warning"|"error"). It is a STRING
// enum (the value IS the wire name) so the generated type is pure data AND the wire
// form is byte-identical — f.severity === 'error' / 'warning' in the SPA decodes
// unchanged.

// Severity is a finding severity. Only SeverityError fails a verdict; Warning/Info
// ride along advisory. The value IS its canonical camelCase wire name.

// RuleID is the stable, namespaced id of a validation rule. Stable across runs for
// finding-diff and worker-prompt continuity.

// Location locates a finding within a typed model. NO Line field: the input is a
// typed model, not bytes.

// stable position used for deterministic finding ordering
// human-readable locus, e.g. "Objective 4"

// Finding is a single machine-checkable rule violation surfaced to the SPA.

// human-readable; safe to weave into a redraft prompt; no PII
// optional; where in the model the finding sits

// acknowledgestale.go implements the F45 per-slot staleness-acknowledge op for Project Design
// (twin of the systemdesign impl): a reviewer marks a stale COMMITTED Phase-2 artifact
// "reviewed — unaffected", clearing its StaleBasis WITHOUT a redraft, with a durable staleAck
// audit entry — both committed atomically on main.

const acknowledgeStaleMaxAttempts = 5

func (m *projectDesignManager) AcknowledgeStaleBasis(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, note string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-2 kind")
	}
	// F-GTD-12: an acknowledge is a MAIN-branch write (the StaleBasis clear + the staleAck
	// entry commit on main). While a co-author session is LIVE for this slot — on a committed
	// slot that is by definition an in-flight AMENDMENT — that main write turns the session's
	// review PR merge-DIRTY, so the eventual approve's merge fails with a Conflict and the
	// workflow bounces back to AwaitingReview looking like a silent no-op to the reviewer.
	// Refuse up front: reconcile RIDES the amendment (its merge clears the staleness).
	if err := m.refuseAckDuringLiveSession(rc, projectID, kind); err != nil {
		return err
	}
	key := acknowledgeStaleIdempotencyKey(projectID, kind, note)
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)

	var lastErr error
	for range acknowledgeStaleMaxAttempts {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			return mapReadProjectError(err)
		}
		_, err = m.projectState.AcknowledgeStaleBasis(fwra.Context{Context: ctx}, psID, proj.Version, psKind, note, key)
		if err == nil {
			return nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue
		}
		return mapStaleAckError(err)
	}
	return fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "AcknowledgeStaleBasis: exhausted conflict retries")
}

// refuseAckDuringLiveSession is the F-GTD-12 guard: while the target kind has a LIVE
// co-author (amendment) session, the acknowledge is refused with a FailedPrecondition
// (the wire's 409/"failed_precondition" conflict shape). Liveness is read through
// GetSessionState — the SAME Describe-then-Query path the review gate and the SPA trust
// (a dead run synthesizes StageDraftFailed; a COMPLETED run is rebuilt from the durable
// slot) — so ack gating always agrees with what the reviewer sees on screen. A NotFound
// (no session ever ran for this slot) passes.
func (m *projectDesignManager) refuseAckDuringLiveSession(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) error {
	view, err := m.GetSessionState(rc, projectID, kind)
	if err != nil {
		var me *fwmanager.Error
		if errors.As(err, &me) && me.Kind == fwmanager.NotFound {
			return nil
		}
		return err
	}
	if !sessionStageIsLive(view.Stage) {
		return nil
	}
	return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
		"cannot mark this artifact reviewed: its amendment session is still open (currently %s). Reconcile rides the amendment — acknowledging now would commit to main and merge-conflict the amendment's review PR. Approve or withdraw the session first.",
		sessionStageLabel(view.Stage)))
}

// sessionStageIsLive reports whether a co-author session stage means the session still
// OWNS the slot (its branch/PR is open or recoverable): drafting / assembling /
// awaiting review / redrafting, plus the StageDraftFailed recovery gate (the session is
// suspended there with its branch and PR intact — a Retry resumes it). The terminal
// stages (committed / withdrawn / refused) and the unknown zero value are NOT live.
func sessionStageIsLive(s SessionStage) bool {
	switch s {
	case StageDrafting, StageAssemblingSDP, StageAwaitingReview, StageRedrafting, StageDraftFailed:
		return true
	case SessionStageUnknown, StageCommitted, StageWithdrawn, StageRefused:
		return false
	default:
		return false
	}
}

func acknowledgeStaleIdempotencyKey(projectID ProjectID, kind ArtifactKind, note string) fwra.IdempotencyKey {
	h := fnv.New64a()
	_, _ = h.Write([]byte(note))
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%d:ackStale:%x", projectID, int(kind), h.Sum64()))
}

// mapStaleAckError surfaces the RA's ContractMisuse (uncommitted / unknown kind) and NotFound
// (unknown project) as their manager equivalents; everything else is Infrastructure.
func mapStaleAckError(err error) error {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.Unknown, fwra.Transient, fwra.RateLimited, fwra.Infrastructure,
			fwra.Auth, fwra.Conflict, fwra.QuotaExhausted, fwra.ContentPolicy:
			// "everything else is Infrastructure" per the doc comment above.
			return newError(fwmanager.Infrastructure, err.Error())
		default:
			return newError(fwmanager.Infrastructure, err.Error())
		}
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// askquestions.go implements the question-comments op for Project Design (twin of the
// systemdesign implementation; founder-ratified 2026-07-05): AskQuestions appends clarifying
// QUESTIONS to a Phase-2 artifact's review ledger WITHOUT a redraft and dispatches a
// lightweight ANSWER job so the addressed role answers each in place. Open questions do NOT
// block approve; asking works on a committed artifact (main) and on a live session (branch).

const askQuestionsMaxAttempts = 5

// Dispatch inputs for the design jobs. Project Design has no PM-critique, so its dispatch
// path historically carried no job_mode; under thin dispatch the MCP scopes its ambient
// mode on this input, so BOTH the draft and answer jobs now set it — jobModeDraft on the
// workflow-side draft dispatch (dispatch.go), jobModeAnswer on this manager-side answer job.
const (
	dispatchInputJobMode = "job_mode"
	jobModeDraft         = "draft"
	jobModeAnswer        = "answer"
)

// AskQuestions — the Project-Design question-comments op. See the systemdesign twin for the
// full contract; the only differences are the Phase-2 kind gate and the Phase-2 slotFor.
//
// DISPATCH RECOVERY (F82): the answer job is BEST-EFFORT — the questions are seeded durably
// first, then a lightweight answer job is dispatched. A dispatch MISS (pipeline/repo not
// configured, repo unresolved, or a workflow_dispatch fault) is now LOGGED LOUDLY server-side
// (it was previously discarded, and the construction-pipeline RA has no logger, so a miss
// vanished — an open question that would never be answered with zero operator signal). To
// RECOVER a dropped dispatch, simply CALL AskQuestions AGAIN with the same questions: the seed
// is idempotent on its content key, so NO ledger entry is duplicated (the existing entries'
// round is reused so the minted ids still match), while the answer-job dispatch RE-FIRES via a
// per-call-unique key.
func (m *projectDesignManager) AskQuestions(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, addressee string, questions []AnchoredComment) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-2 kind")
	}
	switch addressee {
	case projectstate.ReviewAddresseePM, projectstate.ReviewAddresseeArchitect:
		// ok
	default:
		return newError(fwmanager.ContractMisuse, "addressee must be \"pm\" or \"architect\"")
	}
	qs := questionsToLedger(addressee, questions)
	if len(qs) == 0 {
		return newError(fwmanager.ContractMisuse, "no questions to ask (every question needs text)")
	}

	branch := m.resolveQuestionBranch(rc, projectID, kind)
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)
	key := askQuestionsIdempotencyKey(projectID, kind, branch, qs)

	var lastErr error
	for range askQuestionsMaxAttempts {
		proj, err := m.readProjectMaybeBranch(ctx, psID, branch)
		if err != nil {
			return mapReadProjectError(err)
		}
		thread := slotFor(proj, psKind).ReviewThread
		round := nextQuestionRound(thread)
		if r, ok := existingQuestionRound(thread, qs); ok {
			// A prior ask already seeded these exact questions (its answer-job dispatch may
			// have been dropped — F82). Reuse their round so the minted ids match the EXISTING
			// ledger entries, and the re-fired answer job answers the right comments.
			round = r
		}
		_, err = m.designSession.SeedReviewCommentsOnBranch(fwra.Context{Context: ctx}, psID, proj.Version, branch, psKind, round, qs, key)
		if err == nil {
			minted := make([]projectstate.ReviewComment, len(qs))
			for i := range qs {
				minted[i] = qs[i]
				minted[i].ID = projectstate.ReviewCommentID(round, i)
			}
			m.dispatchAnswerJob(ctx, projectID, kind, branch, addressee, minted)
			return nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue
		}
		return mapReadProjectError(err)
	}
	return fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "AskQuestions: exhausted conflict retries")
}

// resolveQuestionBranch — twin of the systemdesign impl (see there for the full F73 rationale).
// A GENUINELY ACTIVE session (co-author workflow OPEN and in a non-terminal stage) keeps its
// ledger on the session branch; every closed/completed/withdrawn/failed/absent run falls back
// to main (""). Resolution reuses the P0-2 Describe-first machinery via GetSessionState rather
// than a bare sessionState Query, which would REPLAY a dead run's stale live stage and wrongly
// resolve an abandoned amendment's leftover branch.
func (m *projectDesignManager) resolveQuestionBranch(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) string {
	view, err := m.GetSessionState(rc, projectID, kind)
	if err != nil || !isLiveSessionStage(view.Stage) {
		return ""
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: rc.Context}, projectstate.ProjectID(projectID))
	if err != nil {
		return ""
	}
	return projectstate.DesignBranch(projectstate.ProjectID(projectID), toPSKind(kind), projectstate.AmendmentIndexFor(slotFor(proj, toPSKind(kind))))
}

// readProjectMaybeBranch reads the head-state aggregate from the given branch. The
// on-branch read moved onto the designSessionAccess facet (Wave 1 reconciliation), which
// ships the aggregate as a ProjectEnvelope across the Manager-Temporal boundary; decode it
// back to the concrete Project here. branch=="" reads main exactly as ReadProject.
func (m *projectDesignManager) readProjectMaybeBranch(ctx context.Context, psID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	env, err := m.designSession.ReadProjectOnBranch(fwra.Context{Context: ctx}, psID, branch)
	if err != nil {
		return projectstate.Project{}, err
	}
	return env.Decode()
}

// isLiveSessionStage reports whether a co-author session is live (its ledger lives on the
// session branch, not main).
func isLiveSessionStage(stage SessionStage) bool {
	switch stage {
	case StageDrafting, StageAwaitingReview, StageRedrafting, StageRefused:
		return true
	case SessionStageUnknown, StageAssemblingSDP, StageCommitted, StageWithdrawn, StageDraftFailed:
		return false
	default:
		return false
	}
}

func questionsToLedger(addressee string, questions []AnchoredComment) []projectstate.ReviewComment {
	out := make([]projectstate.ReviewComment, 0, len(questions))
	for _, q := range questions {
		if strings.TrimSpace(q.Text) == "" {
			continue
		}
		out = append(out, projectstate.ReviewComment{
			Anchor:     q.JSONPath,
			AnchorText: q.AnchorText,
			Text:       q.Text,
			AuthorRole: reviewAuthorRole,
			Type:       projectstate.ReviewCommentTypeQuestion,
			Addressee:  addressee,
		})
	}
	return out
}

func nextQuestionRound(thread []projectstate.ReviewComment) int64 {
	var maxRound int64
	for _, c := range thread {
		if c.Round > maxRound {
			maxRound = c.Round
		}
	}
	return maxRound + 1
}

func askQuestionsIdempotencyKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment) fwra.IdempotencyKey {
	h := fnv.New64a()
	_, _ = h.Write([]byte(branch))
	_, _ = h.Write([]byte{0})
	for _, q := range qs {
		_, _ = h.Write([]byte(q.Addressee))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(q.Anchor))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(q.Text))
		_, _ = h.Write([]byte{0})
	}
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%d:askQuestions:%x", projectID, int(kind), h.Sum64()))
}

// answerJobDispatchSeq makes each explicit AskQuestions call produce a UNIQUE answer-job
// dispatch key, so a re-ask RE-FIRES the answer job (the RA dedups on the whole key, so a
// content-only key would swallow the re-fire — F82). AskQuestions is a direct, non-retried
// manager op (exactly one dispatch per successful call), so a per-call nonce cannot
// double-fire a single logical ask; it only enables the re-ask recovery.
var answerJobDispatchSeq atomic.Uint64

// answerJobDispatchKey derives a per-call-unique answer-job idempotency key from the
// content base plus a monotonic nonce (see answerJobDispatchSeq).
func answerJobDispatchKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment) fwra.IdempotencyKey {
	base := askQuestionsIdempotencyKey(projectID, kind, branch, qs)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:answerJob:%d", base, answerJobDispatchSeq.Add(1)))
}

// dispatchAnswerJob fires the BEST-EFFORT answer job for the freshly-seeded questions and
// LOGS the outcome loudly (F82). A dispatch miss previously vanished (the error was discarded
// and the construction-pipeline RA has no logger); now every failure mode is logged at ERROR
// (or WARN when the rail is simply not configured) with the projectID/kind/addressee/branch,
// and a success at INFO. The questions are already recorded, so a miss is recoverable by
// re-calling AskQuestions (see the op doc) — never silent.
func (m *projectDesignManager) dispatchAnswerJob(ctx context.Context, projectID ProjectID, kind ArtifactKind, branch, addressee string, qs []projectstate.ReviewComment) {
	log := slog.Default().With(
		"op", "projectdesign.AskQuestions.dispatchAnswerJob",
		"projectID", string(projectID), "artifactKind", artifactKindString(kind),
		"addressee", addressee, "branch", branch)
	if m.pipeline == nil || m.repo == nil {
		log.Warn("answer job NOT dispatched: design pipeline/repo not configured (rail dormant) — the question is recorded but will not be auto-answered")
		return
	}
	repoRef, ok := m.repo(projectID)
	if !ok {
		log.Error("answer job NOT dispatched: could not resolve the project repo — the question is recorded but will not be auto-answered; re-run AskQuestions to retry")
		return
	}
	// MANAGED-SCAFFOLD SYNC (sync-on-dispatch): an answer job runs the same seated
	// aiarch-design.yml (and installs the same aiarch-state-mcp binary) as a draft, so it
	// too must never run against a stale scaffold. Failure keeps the answer-job miss
	// semantics: recorded question, loud log, no dispatch — re-run AskQuestions to retry.
	if m.rail != nil {
		cred, cerr := m.rail.GetInstallationToken(fwra.Context{Context: ctx}, repoRef)
		if cerr != nil {
			log.Error("answer job NOT dispatched: could not mint the repo credential for the managed-scaffold sync; re-run AskQuestions to retry", "err", cerr.Error())
			return
		}
		if _, serr := sourcecontrol.SyncManagedScaffold(ctx, m.rail, repoRef, cred); serr != nil {
			log.Error("answer job NOT dispatched: managed-scaffold sync failed — the seated design workflow could not be proven current; re-run AskQuestions to retry", "err", serr.Error())
			return
		}
	}
	// Direct manager-side dispatch (NOT a Temporal workflow): the answer job is a
	// fire-and-forget submit over the PUBLISHED agenticJobAccess RA. The
	// RepoRef→RepoTarget decode + the placeholder step graph that the retired
	// pipelineDispatchAdapter added are inlined here (the workflow-side twin is
	// dispatchDesignJob in dispatch.go).
	target, terr := designRepoTarget(sourcecontrol.RepoRefString(repoRef))
	if terr != nil {
		log.Error("answer job NOT dispatched: could not resolve the target repo for the answer job; re-run AskQuestions to retry", "err", terr.Error())
		return
	}
	// The addressee rides the .claude command NAME now (design-answer vs design-answer-pm)
	// rather than a composed answer prompt. An empty slug is contract misuse — an addressee
	// that is neither "architect" nor "pm"; keep the answer-job miss semantics (recorded
	// question, loud log, no dispatch).
	command := projectstate.DesignCommandFor(toPSKind(kind), projectstate.DesignJobModeAnswer, addressee)
	if command == "" {
		log.Error("answer job NOT dispatched: no design-answer command slug for the addressee (contract misuse — expected \"architect\" or \"pm\")")
		return
	}
	inputs := map[string]string{
		dispatchInputArtifactKind:  artifactKindString(kind),
		dispatchInputCommand:       command,
		dispatchInputTargetBranch:  branch,
		dispatchInputPriorStateRef: "",
		dispatchInputJobMode:       jobModeAnswer,
	}
	spec := agenticjob.PipelineSpec{
		ProjectID: agenticjob.ProjectID(projectID),
		Steps: []agenticjob.PipelineStep{{
			Name:      "design",
			Toolchain: agenticjob.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: inputs,
		TargetRepo:     target,
		WorkflowFile:   designWorkflowFileName,
	}
	key := answerJobDispatchKey(projectID, kind, branch, qs)
	handle, err := m.pipeline.SubmitAgenticJob(fwra.Context{Context: ctx, IdempotencyKey: key}, spec)
	if err != nil {
		log.Error("answer job dispatch FAILED — the question is recorded but not auto-answered; re-run AskQuestions with the same question to retry",
			"err", err.Error(), "key", string(key))
		return
	}
	log.Info("answer job dispatched", "key", string(key))
	m.watchAnswerEpisode(ctx, projectID, kind, handle, log)
}

// ---------------------------------------------------------------------------
// Episode capture for the ANSWER job (SP1 capture-seam, Task 7)
// ---------------------------------------------------------------------------
//
// The answer job is the ONE agentic dispatch this Manager makes outside a Temporal
// workflow: AskQuestions submits it fire-and-forget and returns. Nothing observes it, so
// without this watch every answer episode — real tokens, really spent — would be invisible
// to the ledger.
//
// NON-DURABLE BY CONSTRUCTION, and that is accepted: this is a plain goroutine in the
// server process. A restart between the dispatch and the terminal observation loses the
// watch and therefore the record — no gap line either, because nothing is left to write
// one. Only the WORKFLOW-side capture paths carry the durable never-silent guarantee; the
// answer job is auxiliary (it gates nothing) and did not warrant its own workflow.

const (
	// answerEpisodePollInterval spaces the manager-side observe loop. Same order as the
	// workflow-side observePollInterval — an answer job is the same kind of agentic run.
	answerEpisodePollInterval = 15 * time.Second
	// answerEpisodeWatchWindow is the hard deadline on the watch. Past it the episode is
	// recorded as an explicit GAP rather than watched forever by a leaked goroutine.
	answerEpisodeWatchWindow = 30 * time.Minute
	// answerEpisodeAppendWindow bounds the ledger append that follows the watch. It is a
	// SEPARATE budget from answerEpisodeWatchWindow on purpose — see run().
	answerEpisodeAppendWindow = 30 * time.Second
	// answerEpisodeAppendAttempts / answerEpisodeAppendBackoff are this path's stand-in
	// for the Temporal retry envelope the workflow-side append rides. Small and bounded:
	// a local sidecar append that fails three times in a row is not transient.
	answerEpisodeAppendAttempts = 3
	answerEpisodeAppendBackoff  = 250 * time.Millisecond
)

// watchAnswerEpisode spawns the bounded manager-side watch for one dispatched answer job.
// It detaches from the CALLER'S context on purpose: ctx is the AskQuestions request
// context and is cancelled the moment that call returns, while the job it dispatched runs
// for minutes afterwards. WithoutCancel keeps the request's values (tracing, principal)
// and drops only the cancellation.
func (m *projectDesignManager) watchAnswerEpisode(ctx context.Context, projectID ProjectID, kind ArtifactKind, handle agenticjob.PipelineHandle, log *slog.Logger) {
	w := answerEpisodeWatch{
		pipeline: m.pipeline,
		episodes: m.episodes,
		poll:     answerEpisodePollInterval,
		window:   answerEpisodeWatchWindow,
		log:      log,
	}
	go w.run(context.WithoutCancel(ctx), projectID, artifactKindString(kind), handle)
}

// answerEpisodeWatch is the bounded observe-then-append loop behind watchAnswerEpisode,
// broken out with its timings injected so it can be exercised deterministically in tests.
type answerEpisodeWatch struct {
	pipeline agenticjob.AgenticJobAccess
	episodes episode.EpisodeAccess
	poll     time.Duration
	window   time.Duration
	log      *slog.Logger
}

// run polls handle to a terminal phase (or to the window's end) and appends the ONE ledger
// record the dispatch owes. Blocking — watchAnswerEpisode spawns it.
func (w answerEpisodeWatch) run(ctx context.Context, projectID ProjectID, targetRef string, handle agenticjob.PipelineHandle) {
	if w.pipeline == nil || w.episodes == nil {
		return
	}
	watchCtx, cancelWatch := context.WithTimeout(ctx, w.window)
	defer cancelWatch()

	obs, terminal := w.observeToTerminal(watchCtx, handle)
	if terminal && episodeVenueIsRemote(obs.RunURL) {
		// Remote venue mines no episode in v1 — nothing was lost, so record nothing.
		return
	}
	rec := w.answerRecord(obs, terminal, targetRef, handle)

	// THE APPEND MUST NOT RIDE watchCtx. On the DEADLINE path observeToTerminal returned
	// precisely BECAUSE watchCtx expired, so appending under it would hand the ledger an
	// already-cancelled context — making the gap record the deadline exists to write the
	// one write guaranteed to fail. Derive a fresh, cancellation-free budget from the
	// caller's context instead. (Today's AppendEpisode realisations ignore the context
	// entirely, so this is latent rather than live; a store that honours it would turn the
	// never-silent guarantee into a silent loss on exactly the path that needs it most.)
	appendCtx, cancelAppend := context.WithTimeout(context.WithoutCancel(ctx), answerEpisodeAppendWindow)
	defer cancelAppend()
	w.appendRecord(appendCtx, projectID, rec, handle)
}

// appendRecord writes the record with a small BOUNDED retry. The workflow-side capture
// gets Temporal's retry envelope for free; this path has none, so without it a single
// transient store stumble would lose the episode outright.
func (w answerEpisodeWatch) appendRecord(ctx context.Context, projectID ProjectID, rec episode.EpisodeRecord, handle agenticjob.PipelineHandle) {
	key := fwra.IdempotencyKey("answerEpisode:" + string(handle))
	var err error
	for attempt := 1; attempt <= answerEpisodeAppendAttempts; attempt++ {
		err = w.episodes.AppendEpisode(fwra.Context{Context: ctx, IdempotencyKey: key},
			episode.ProjectID(projectID), rec)
		if err == nil {
			return
		}
		if attempt == answerEpisodeAppendAttempts ||
			!waitOrDone(ctx, time.Duration(attempt)*answerEpisodeAppendBackoff) {
			break
		}
	}
	w.log.Error("answer-job episode NOT recorded: ledger append failed after its bounded retry",
		"episodeId", rec.EpisodeID, "attempts", answerEpisodeAppendAttempts, "err", err.Error())
}

// waitOrDone sleeps for d, returning false the moment ctx is done instead.
func waitOrDone(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// observeToTerminal polls the dispatched job until it reaches a terminal phase, the window
// closes, or the RA faults. terminal=false means the second or third — the caller turns
// that into a gap record.
func (w answerEpisodeWatch) observeToTerminal(ctx context.Context, handle agenticjob.PipelineHandle) (agenticjob.PipelineObservation, bool) {
	var last agenticjob.PipelineObservation
	cancelGrace := 0
	for {
		obs, err := w.pipeline.ObserveAgenticJob(fwra.Context{Context: ctx}, handle)
		if err != nil {
			return last, false
		}
		last = obs
		if terminal, done := w.classify(obs, &cancelGrace); done {
			return obs, terminal
		}
		if !waitOrDone(ctx, w.poll) {
			return last, false
		}
	}
}

// classify decides whether THIS observation ends the watch. A terminal observation with a
// summary always does. A terminal observation WITHOUT one ends it too — except for the
// CANCEL RACE, where the phase flips synchronously while the agent subprocess is still
// unwinding: that gets maxLateEpisodePolls further polls (the same grace the workflow-side
// capture gives it) before the run is written off.
func (w answerEpisodeWatch) classify(obs agenticjob.PipelineObservation, cancelGrace *int) (terminal, done bool) {
	if !designPipelinePhase(obs.Phase).IsTerminal() {
		return false, false
	}
	if obs.Episode != nil || obs.Phase != agenticjob.PhaseCancelled {
		return true, true
	}
	if *cancelGrace >= maxLateEpisodePolls {
		return true, true
	}
	*cancelGrace++
	return false, false
}

// answerRecord composes the ledger record for a watched answer job: the mined summary, or
// an explicit GAP naming which of the two ways it went missing.
func (w answerEpisodeWatch) answerRecord(obs agenticjob.PipelineObservation, terminal bool, targetRef string, handle agenticjob.PipelineHandle) episode.EpisodeRecord {
	// Lineage is nil BY DESIGN: this dispatch has no durable execution behind it.
	if terminal && obs.Episode != nil {
		return episodeRecordFromSummary(*obs.Episode, episode.EpisodeKindAnswer, targetRef, nil, obs.Diagnostic)
	}
	reason := episodeMissingSummaryReason
	if !terminal {
		reason = "answer job did not reach a terminal phase within the manager-side watch window"
	}
	return episodeGapRecord(episode.EpisodeKindAnswer, targetRef, nil,
		"gap-"+episodeIDSafe(string(handle)),
		episodeGapReason(reason, obs.Diagnostic), time.Now().UTC())
}

// ---------------------------------------------------------------------------
// Episode record composition — shared by the workflow-side capture
// (coauthorphase2artifact.go) and the answer-job watch above.
// ---------------------------------------------------------------------------

// episodeMissingSummaryReason is the GapReason for the "the run terminated and reported
// no episode at all" case — the one the never-silent rule exists for.
const episodeMissingSummaryReason = "terminal observation carried no episode summary"

// episodeRecordFromSummary copies a mined EpisodeSummary onto an EpisodeRecord field for
// field — VERBATIM, no recomputation — and stamps the Manager-known Kind/TargetRef/
// Lineage the RA cannot know. WorkerClass is left unset: a design dispatch carries the
// artifact kind and the job mode, never the Phase-2 activity list's workerClass, so there
// is no honest value to put here. diagnostic supplies the GapReason when the RA itself
// reported a GAP outcome (a restart-lost run recovered from its orphaned trace) — the
// observation's diagnostic IS the explanation there, since EpisodeSummary carries no
// reason field.
func episodeRecordFromSummary(s agenticjob.EpisodeSummary, kind episode.EpisodeKind, targetRef string, lineage *episode.EpisodeLineage, diagnostic string) episode.EpisodeRecord {
	rec := episode.EpisodeRecord{
		EpisodeID:      s.EpisodeID,
		Kind:           kind,
		TargetRef:      targetRef,
		Lineage:        lineage,
		Model:          s.Model,
		Usage:          episode.EpisodeUsage(s.Usage),
		CostUSD:        s.CostUSD,
		NumTurns:       s.NumTurns,
		ToolCallCounts: s.ToolCallCounts,
		SubagentSpans:  episodeSubagentSpans(s.SubagentSpans),
		StartedAt:      s.StartedAt,
		EndedAt:        s.EndedAt,
		Outcome:        episodeOutcomeFrom(s.Outcome),
		TracePath:      s.TracePath,
	}
	if s.StreamedUsage != nil {
		u := episode.EpisodeUsage(*s.StreamedUsage)
		rec.StreamedUsage = &u
	}
	if rec.Outcome == episode.EpisodeGap {
		reason := episodeGapReason("the run reported a gap episode", diagnostic)
		rec.GapReason = &reason
	}
	return rec
}

// episodeGapRecord composes the SYNTHESIZED gap record for a dispatch that produced no
// summary at all. now is supplied by the caller (workflow.Now on the replay-deterministic
// workflow paths) because the run's own clock is exactly what was lost.
func episodeGapRecord(kind episode.EpisodeKind, targetRef string, lineage *episode.EpisodeLineage, episodeID, reason string, now time.Time) episode.EpisodeRecord {
	return episode.EpisodeRecord{
		EpisodeID: episodeID,
		Kind:      kind,
		TargetRef: targetRef,
		Lineage:   lineage,
		StartedAt: now,
		EndedAt:   now,
		Outcome:   episode.EpisodeGap,
		GapReason: &reason,
	}
}

// episodeGapReason joins the Manager's own reason to the observation's diagnostic when
// the RA supplied one, so a gap says both WHAT was lost and what the rail reported.
func episodeGapReason(reason, diagnostic string) string {
	if strings.TrimSpace(diagnostic) == "" {
		return reason
	}
	return reason + " — " + diagnostic
}

// episodeSubagentSpans re-types the mined subagent spans onto the ledger contract's own
// span type (identical shapes, distinct contracts — contracts are self-contained).
func episodeSubagentSpans(in []agenticjob.SubagentSpan) []episode.SubagentSpan {
	if len(in) == 0 {
		return nil
	}
	out := make([]episode.SubagentSpan, 0, len(in))
	for _, s := range in {
		out = append(out, episode.SubagentSpan(s))
	}
	return out
}

// episodeOutcomeFrom maps the observation contract's outcome onto the ledger contract's.
// Written as a TOTAL switch rather than a numeric cast so a future divergence between the
// two independently-versioned contracts is a compile-time conversation, not silent drift.
func episodeOutcomeFrom(o agenticjob.EpisodeOutcome) episode.EpisodeOutcome {
	switch o {
	case agenticjob.EpisodeSucceeded:
		return episode.EpisodeSucceeded
	case agenticjob.EpisodeFailed:
		return episode.EpisodeFailed
	case agenticjob.EpisodeCancelled:
		return episode.EpisodeCancelled
	case agenticjob.EpisodeGap:
		return episode.EpisodeGap
	default:
		return episode.EpisodeGap
	}
}

// episodeIDSafe rewrites s into the [A-Za-z0-9._-] alphabet episodeAccess requires of an
// EpisodeID. A rejected id is ContractMisuse — non-retryable — so a gap record seeded from
// a raw pipeline handle (which carries a ':') would be dropped on the floor, exactly
// defeating the never-silent rule the gap record exists to serve.
func episodeIDSafe(s string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, s)
	if safe == "" {
		return "unknown"
	}
	return safe
}

// existingQuestionRound returns the round of an EARLIER identical seeding of qs (matched by
// addressee + anchor + text of the first question), so a re-ask reuses that round rather than
// minting a fresh one — keeping the minted ids aligned with the already-seeded ledger entries
// (F82 re-dispatch correctness). ok=false when these questions were never seeded.
func existingQuestionRound(thread []projectstate.ReviewComment, qs []projectstate.ReviewComment) (int64, bool) {
	if len(qs) == 0 {
		return 0, false
	}
	first := qs[0]
	for _, c := range thread {
		if c.Type == projectstate.ReviewCommentTypeQuestion &&
			c.Addressee == first.Addressee && c.Anchor == first.Anchor && c.Text == first.Text {
			return c.Round, true
		}
	}
	return 0, false
}

// isRAConflict reports whether err is the RA's stale-version Conflict on this sync write
// path (the fwra.Error form; the workflow's isConflict is for temporal-wrapped errors).
func isRAConflict(err error) bool {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		return raErr.Kind == fwra.Conflict
	}
	return false
}

// pipelineDefaultToolchain is the placeholder toolchain stamped on the logical design
// step (the real design recipe lives in the user's aiarch-design.yml workflow file).
const pipelineDefaultToolchain = "go-1.23"

// designRepoTarget decodes an opaque per-project RepoRef String() into the RA's
// infrastructure-neutral RepoTarget{Owner, Name}. Empty ⇒ a zero RepoTarget (the RA
// falls back to the configured construction repo); a malformed ref surfaces the RA's
// ContractMisuse. Uses sourcecontrol's own OwnerRepo accessor so the RepoRef encoding
// stays owned by sourceControlAccess (no encoding leak here).
//
// NOT promotable to projectstate (code-health-phase-bd task D3 verification): it needs
// agenticjob.RepoTarget + sourcecontrol.RepoRefOwnerRepo/RepoRefFromString —
// both sibling ResourceAccess packages, and TestMethodLayering forbids RA→RA sideways
// imports (the RA-layer analog of "no Manager→Manager sideways"). Stays duplicated
// per-manager alongside designBranch's twin.
func designRepoTarget(repoRef string) (agenticjob.RepoTarget, error) {
	if repoRef == "" {
		return agenticjob.RepoTarget{}, nil
	}
	owner, name, err := sourcecontrol.RepoRefOwnerRepo(sourcecontrol.RepoRefFromString(repoRef))
	if err != nil {
		return agenticjob.RepoTarget{}, err
	}
	return agenticjob.RepoTarget{Owner: owner, Name: name}, nil
}

// designBranch PROMOTED to projectstate.DesignBranch (code-health-phase-bd task D3) —
// byte-identical pure resolver, no longer duplicated with systemdesign's twin.

// ===========================================================================
// Dispatch inputs (C-WF-DESIGN workflow_dispatch schema). These exact key names are
// the binding contract with aiarch-design.yml's workflow_dispatch.inputs.
// idempotency_token is RA-controlled and is NOT set here.
// ===========================================================================

const (
	dispatchInputArtifactKind = "artifact_kind"
	// dispatchInputCommand carries the .claude command slug the seated design job runs
	// (DesignCommandFor). It REPLACES the retired design_prompt input: the Method Phase-2
	// doctrine that used to be composed into a prompt now lives in the command's
	// method-assets, so the Manager ships only the command NAME, not prose.
	dispatchInputCommand       = "command"
	dispatchInputTargetBranch  = "target_branch"
	dispatchInputPriorStateRef = "prior_state_ref"
)

// dispatchActivityOptions is the option preset for the generated
// agenticJobAccess.submitAgenticJob Activity (consumed by the
// manager's option hook — workermanifest.go). A transient submit error (ErrTransient /
// Retryable) auto-retries via this RetryPolicy; a terminal RA fault (ContractMisuse / Auth
// / QuotaExhausted) is non-retryable and surfaces to the workflow body. A PhaseFailed is
// NOT a dispatch error — it is a successful observation of a failed job (§0.5.4).
func dispatchActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:     30 * time.Second,
		MaxAttempts: 5,
		TerminalRA:  []fwra.Kind{fwra.ContractMisuse, fwra.Auth, fwra.QuotaExhausted},
	}.Options()
}

// appendEpisodeRetryWindow is the HARD wall-clock bound on the episode-append's own retry
// envelope. Attempts are UNCAPPED inside it (bookkeeping must not lose to a transient
// store fault) but they cannot run forever, because the workflow WAITS on this activity.
//
// bounded-latency ruling 2026-08-02: local sidecar append failing >2m is not transient;
// business outcome must not stall on telemetry.
const appendEpisodeRetryWindow = 2 * time.Minute

// appendEpisodeActivityOptions is the episode-append preset — DELIBERATELY its own
// envelope, independent of every business preset (§capture-seam): a generous per-attempt
// timeout, UNCAPPED attempts inside appendEpisodeRetryWindow (MaxAttempts unset ⇒
// Temporal treats it as unlimited), and ContractMisuse terminal (a malformed record will
// never become well-formed by retrying — the caller logs it instead).
func appendEpisodeActivityOptions() workflow.ActivityOptions {
	o := fwmanager.ActivityPreset{
		Timeout:    30 * time.Second,
		TerminalRA: []fwra.Kind{fwra.ContractMisuse},
	}.Options()
	o.ScheduleToCloseTimeout = appendEpisodeRetryWindow
	return o
}

// observeActivityOptions is the option preset for the generated
// agenticJobAccess.observeAgenticJob Activity. Transient reads retry;
// a NotFound (GC'd handle) is non-retryable and surfaces.
func observeActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    15 * time.Second,
		TerminalRA: []fwra.Kind{fwra.NotFound, fwra.ContractMisuse},
	}.Options()
}

// designWorkflowFileName is the per-project DESIGN workflow file the agentic design
// dispatch must target (per-project-design-dispatch) — the BASENAME of
// sourcecontrol.DesignWorkflowPath (".github/workflows/aiarch-design.yml"), i.e.
// "aiarch-design.yml". Derived from the RA's single source of truth so the dispatch
// target and the project-birth workflow-file seat can never drift.
var designWorkflowFileName = path.Base(sourcecontrol.DesignWorkflowPath)

// mintCredActivityOptions — the credential mint (generated sourceControlAccess.
// getInstallationToken). A rejected/expired App identity is terminal. Feeds the manager's
// option hook (workermanifest.go).
func mintCredActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    15 * time.Second,
		TerminalRA: []fwra.Kind{fwra.Auth, fwra.ContractMisuse},
	}.Options()
}

// railActivityOptions — the PR-rail verbs (the generated sourceControlAccess rail ops,
// INCLUDING syncManagedScaffold since B9). Auth + a merge Conflict (not-mergeable) + bad
// input are terminal; transport/rate-limit retry. Feeds the manager's option hook
// (workermanifest.go) for every generated rail verb.
func railActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    30 * time.Second,
		TerminalRA: []fwra.Kind{fwra.Auth, fwra.NotFound, fwra.Conflict, fwra.ContractMisuse},
	}.Options()
}

// reviewledger.go holds the durable review-ledger seam for the projectDesign Manager
// (review-ledger feature, founder-ratified 2026-07-05) — the structural twin of the
// systemDesign Manager's reviewledger.go. Ledger STORAGE + transition rules live in
// projectstate (reviewthread.go); this is the Manager-side wiring: the ReviewComment ↔
// ReviewCommentView projection and the open-comment gate. The SetReviewCommentStatus /
// SeedReviewComments branch-mutation Activities MIGRATED (B9) onto the generated
// designSessionAccess.setReviewCommentStatusOnBranch / seedReviewCommentsOnBranch
// invokers (invokers.gen.go, reached via wf.Acts) — the ledger-extension fallback those
// custom bodies ran now lives inside the RA (projectstate/designsession.go).

// reviewAuthorRole is the role stamped on every comment the architect files at the
// Project-Design review gate.
const reviewAuthorRole = "architect"

// toReviewCommentView projects one stored ledger entry onto its wire view.
func toReviewCommentView(c projectstate.ReviewComment) ReviewCommentView {
	return ReviewCommentView{
		ID:         c.ID,
		Anchor:     c.Anchor,
		AnchorText: c.AnchorText,
		Text:       c.Text,
		AuthorRole: c.AuthorRole,
		Round:      c.Round,
		Status:     c.Status,
		Response:   c.Response,
		Type:       c.Type,
		Addressee:  c.Addressee,
	}
}

// reviewThreadToView projects the durable ledger onto the wire thread the sessionState
// Query returns (nil stays nil so the omitempty wire shape is unchanged).
func reviewThreadToView(thread []projectstate.ReviewComment) []ReviewCommentView {
	if len(thread) == 0 {
		return nil
	}
	out := make([]ReviewCommentView, 0, len(thread))
	for _, c := range thread {
		out = append(out, toReviewCommentView(c))
	}
	return out
}

// ---------------------------------------------------------------------------
// Shared Temporal identity constants (projectDesignManager.md §6.1/§6.2/§6.5).
// TaskQueue is defined in the generated worker.gen.go.
// ---------------------------------------------------------------------------

// Signal and query names (contract §6.5).
const (
	// signalReviewDecision resumes a suspended CoAuthorPhase2ArtifactWorkflow at
	// the per-artifact AwaitingReview gate; backs submitReviewDecision (OQ-3).
	signalReviewDecision = "reviewDecision"
	// signalSetCommentStatus resumes a suspended CoAuthorPhase2ArtifactWorkflow at the
	// AwaitingReview gate to apply a durable review-ledger status transition
	// (open->waived / addressed->open) to one comment on the session branch; backs
	// SetReviewCommentStatus (review-ledger feature).
	signalSetCommentStatus = "setCommentStatus"
	// signalRedraft resumes a CoAuthorPhase2ArtifactWorkflow that landed in the
	// StageDraftFailed recovery gate (a terminal Phase-2 design-job failure). It
	// re-enters the dispatch loop in the SAME live workflow so the user's "Retry
	// draft" recovers without a fresh run. Backs requestArtifactDraft's retry path
	// (signal-with-start; projectDesignManager.md §2.1 / §0.5.4).
	signalRedraft = "redraft"
	// signalSDPDecision resumes the AssembleSDPReviewWorkflow at the option-commit
	// gate; backs submitSDPDecision.
	signalSDPDecision = "sdpDecision"
	// querySessionState returns a SessionStateView; backs getSessionState.
	querySessionState = "sessionState"
)

// ExecutionKinds for the durable-execution control plane (contract §6.2).
const (
	// executionKindCoAuthor is the per-artifact Phase-2 co-authoring gate.
	executionKindCoAuthor = "projectDesignCoAuthor"
	// executionKindSDPReview is the UC2 SDP-review assembly + option-commit gate.
	executionKindSDPReview = "projectDesignSDPReview"
	// executionKindPhaseAdvance is the short-lived Phase-2 seal gating workflow.
	executionKindPhaseAdvance = "projectDesignPhaseAdvance"
)

// workflows is the single projectDesignManager component struct — the workflow
// receiver. It carries ZERO custom Temporal Activities and NO I/O ResourceAccess dep
// (B9 + its follow-up ruling): every RA op is a GENERATED activity reached through the
// typed invoker surface (Acts), and the last custom Activity
// (StageArtifactForReviewActivity) was deleted when the designSessionAccess Stage op's
// model param became the codable ModelEnvelope at the schema.
//
//   - Estimation, OperationEst, Settlement are PURE, deterministic Engines, so the
//     workflow body calls their verbs DIRECTLY — replay-safe, no Activity wrapper.
//     They STAY server-side in-workflow (§0.5.5 "RETAINED, unchanged"): they are
//     by-value joins, NOT LLM work, and do NOT become agentic dispatches.
//
// 2026-06-15 agentic-pivot re-cut (projectDesignManager.md §0.5 / D-MPD-Δ): the
// Phase-2 plan-DRAFTING mechanism flips from a synchronous worker call to an ASYNC
// dispatch → observe → read-back round-trip. The per-artifact CoAuthorPhase2-
// ArtifactWorkflow no longer calls workerAccess.GenerateTypedData in-process; instead
// the Manager DISPATCHES a claude-code-action DESIGN job via the generated
// agenticJobAccess submit/observe activities, OBSERVES it to a typed terminal
// phase, and READS BACK the typed model the Action committed via the generated
// designSessionAccess.readProjectOnBranch activity. aiarch makes NO synchronous LLM
// call and writes NO draft JSON on the main path.
//
// DROPPED from the draft path (§0.5.5): workerAccess (no synchronous LLM call
// survives; the in-flight cancel is agenticJobAccess.cancel) and
// artifactValidationEngine (Phase-2 validation is the required CI check inside the
// Action, surfaced as the job's terminal phase). Both are removed from this struct.
type workflows struct {
	Estimation   estimation.EstimationEngine
	OperationEst operationestimation.OperationEstimationEngine
	Settlement   billing.BillingEngine

	// Acts is the GENERATED typed invoker surface (invokers.gen.go) — the workflow's call
	// surface for EVERY contract-backed RA op: projectStateAccess readProjectVersion /
	// advancePhase, the agenticJobAccess submit/observe design-job pair, the
	// seven sourceControlAccess PR-rail verbs, and the eight designSessionAccess
	// branch-session verbs. Each invoker consults the manager's per-op preset hook
	// (workermanifest.go activityOptions), keyed by the generated activity name.
	Acts genInvokers

	// Rail + Repo are the OPTIONAL git-forward PR rail (I-DESIGN-DISPATCH §2b). When both
	// are non-nil AND a repo resolves, the per-artifact CoAuthorPhase2ArtifactWorkflow
	// draft path wraps each draft in the settled branch→PR→read-back→+1→merge model + the
	// branch-aware read-back/stage; when nil that path runs UNCHANGED (read-back/stage on
	// main, no branch/PR ops). The AssembleSDPReviewWorkflow (the in-workflow three-Engine
	// join) is UNCHANGED — it gets NO rail (only the per-artifact draft path does).
	//
	// Rail is the PUBLISHED sourceControlAccess RA. The rail verbs are reached through
	// the generated invoker surface (wf.Acts.Rail*); this field is held directly ONLY for
	// the nil/dormant gate (gitEnabled).
	Rail sourcecontrol.SourceControlAccess
	// Repo resolves the per-project RepoRef the rail verbs address. nil ⇒ the rail is
	// dormant. Injected so the repo-resolution policy is swappable without a new RA edge.
	Repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)
}

// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply
// loop. The idempotency key is stable per Activity invocation, so a re-apply that
// races a prior committed attempt collapses to an idempotent no-op success.
const maxMutateConflictAttempts = 20

// Activity option presets (contract §6.4). Concrete RetryPolicy / timeout choices live
// here, in the Manager. Each preset is an ActivityOptions VALUE consumed by the
// generated-invoker option hook in workermanifest.go, keyed by the generated activity
// name. This Manager has ZERO custom Temporal Activities (B9 follow-up — the last one,
// StageArtifactForReviewActivity, was deleted when the contract op's model param became
// the codable ModelEnvelope), so no ctx-wrapper forms remain.

// readProjectActivityOptions is the preset for the read path: since B9 this is EXCLUSIVELY
// the generated designSessionAccess.readProjectOnBranch invoker (both branch=="" main reads
// and branch-aware reads funnel through it), plus the generated
// projectStateAccess.readProjectVersion. Both are keyed onto this VALUE via the
// workermanifest.go option hook.
func readProjectActivityOptions() workflow.ActivityOptions {
	// BOUND the read retries so a RETRYABLE fault (Transient / Infrastructure /
	// RateLimited) cannot loop forever — decode failures of committed state are now
	// TERMINAL (ContractMisuse, below), but a genuine persistent infra outage must
	// still surface rather than wedge invisibly (QA F36, mirrors systemdesign).
	return fwmanager.ActivityPreset{
		Timeout:     10 * time.Second,
		MaxAttempts: 8,
		TerminalRA:  []fwra.Kind{fwra.NotFound, fwra.ContractMisuse},
	}.Options()
}

// mutateActivityOptions is the preset for the head-state mutation Activities: every
// generated designSessionAccess write op (Stage / Commit / Reject / Withdraw / the
// review-ledger Set/Seed verbs) plus the generated projectStateAccess.advancePhase —
// each keyed onto this VALUE via the workermanifest.go option hook. Retry Transient via
// the Activity RetryPolicy; Conflict is handled by the workflow-level re-read→re-apply
// loop. Terminal on ContractMisuse.
func mutateActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    15 * time.Second,
		TerminalRA: []fwra.Kind{fwra.ContractMisuse},
	}.Options()
}

// raConflictErrType is the canonical Temporal Type() a head-state mutation Activity
// surfaces when the optimistic-concurrency token (expectedVersion) is stale.
var raConflictErrType = fwmanager.RAErrType(fwra.Conflict)

// raNotFoundErrType is the canonical Temporal Type() the ReadProject Activity
// surfaces when the addressed aggregate has NO row yet.
var raNotFoundErrType = fwmanager.RAErrType(fwra.NotFound)

// isConflict reports whether err is a head-state mutation's stale-version Conflict.
func isConflict(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raConflictErrType
	}
	return false
}

// isReadNotFound reports whether err is the ReadProject Activity's "no row yet"
// NotFound (a brand-new project).
func isReadNotFound(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raNotFoundErrType
	}
	return false
}

// coAuthorWorkflowID derives the continuity token for a per-artifact co-authoring
// workflow: {projectId}:{int(kind)} (contract §6.1).
func coAuthorWorkflowID(projectID ProjectID, kind ArtifactKind) string {
	return fmt.Sprintf("%s:%d", projectID, int(kind))
}

// railDraftedBy renders the PM-P2-4 draftedBy provenance: the agentic design rail identity,
// plus the amendment-session marker when this run is a reopening (Amendment > 0).
func railDraftedBy(amendment int) string {
	if amendment > 0 {
		return fmt.Sprintf("agentic-design-rail (amend-%d)", amendment)
	}
	return "agentic-design-rail"
}

// autoApproverVibes is the Approver provenance label recorded on a vibes-policy AUTO-approve at
// the design review gate (the vibes autogate) — it distinguishes a policy-driven approval from
// a human reviewer's in the commit's approvedBy provenance.
const autoApproverVibes = "policy:vibes"

// sdpReviewWorkflowID derives the continuity token: {projectId}:sdpReview.
func sdpReviewWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:sdpReview", projectID)
}

// phaseAdvanceWorkflowID derives the continuity token: {projectId}:phaseAdvance.
func phaseAdvanceWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:phaseAdvance", projectID)
}

// ---------------------------------------------------------------------------
// Internal helpers (deterministic; no clock, no RNG).
// ---------------------------------------------------------------------------

// coAuthorState is the live technical state backing the sessionState Query. Reused
// (in a slightly fuller form) by both the per-artifact and the SDP-review workflows.
type coAuthorState struct {
	projectID    ProjectID
	artifactKind ArtifactKind
	stage        SessionStage
	draft        projectstate.ArtifactModel
	findings     []Finding
	headVersion  projectstate.Version
	// failureReason is set only on StageDraftFailed: the neutral job Diagnostic, the
	// human "why" for the SPA's retry/withdraw screen (the anti-wedge requirement).
	failureReason string
	// reviewThread is the durable review ledger for this artifact (review-ledger feature),
	// refreshed from the session branch after every (re)stage and every waive/reopen so the
	// query + approve gate see the live thread. Nil until a read-back carries comments.
	reviewThread []projectstate.ReviewComment
	// policyAutoApprove and vibesAutogateEnabled drive the VIBES AUTOGATE (F-R3 vibes-everywhere,
	// founder-ratified): when this session's committed ReviewPolicy preset is "vibes"
	// (policyAutoApprove), the review gate AUTO-APPROVES a clean draft (no open change-requests)
	// instead of waiting for a human — honoring ReviewPolicy exactly like construction. Both are
	// snapshot ONCE at session start (coAuthorSessionSetup): policyAutoApprove from the head-state
	// ReviewPolicy.Preset, vibesAutogateEnabled from the "design-vibes-autogate" GetVersion gate
	// (an in-flight session replays DefaultVersion → autogate OFF → the human gate for its whole
	// run). See the review-gate loop in CoAuthorPhase2ArtifactWorkflow.
	policyAutoApprove    bool
	vibesAutogateEnabled bool
	// resumeFromReadBack is the F35-twin checkpoint: set true when a POST-read-back rail step
	// (openPR) faulted and the session landed at the failed gate WITH the draft already
	// committed on the branch. On the next Retry the draft round consumes it and RESUMES from
	// the read-back — SKIPPING the re-dispatch — so it does not redispatch Claude onto a branch
	// that already carries the model (which the no-commit guard would red). Workflow-local,
	// deterministic on replay (set from a recorded Activity error, never wall-clock).
	resumeFromReadBack bool
	// feedbackSeeded reports whether the CURRENT contents of the workflow's feedback variable
	// are already durably in the review ledger. The review-gate REJECT and the AMENDMENT seed
	// fold their feedback into the ledger themselves (feedbackToLedgerComments / seedAmendment
	// Ledger), so they set this true. The MEMORY-ONLY failed-gate paths — a redraft-signal
	// (F47), a Retry-via-Reject AT a failed gate, a faulted reject — only retain the feedback in
	// this workflow variable, so they set it false. Under thin dispatch the drafting agent reads
	// context ONLY via getReviewThread, so before each redraft dispatch a false flag triggers
	// seedFailedGateFeedback to seed the retained anchored comments, while a true flag skips it
	// so an already-seeded path is never double-seeded.
	feedbackSeeded bool
	// decisionSeq counts the review decisions HANDLED at the AwaitingReview gate — one
	// monotonic increment per received reviewDecision signal (F-QA2-44, systemdesign twin
	// parity). Replay-stable: driven purely by the recorded signal order. It keys the
	// per-attempt version gate (gate-decision-token-remint-p2-<seq>) guarding the approve
	// arm's gate-time credential re-mint; see coAuthorApprove for why that gate is
	// per-attempt rather than static. Stays zero for AssembleSDPReviewWorkflow (no gate).
	decisionSeq int
	// activeRole / activeStep / activeRound are the WORKFLOW-LOCAL sub-step indicator
	// backing the honest role-driven loading pill (Plan-3 C2, mirroring systemdesign's C1).
	// They are SET immediately before each dispatch boundary (architect drafting/revising —
	// Phase 2 has NO PM critique, so ActiveRoleProductManager / ActiveStepCritiquing are
	// never stamped here) and CLEARED to none/none/0 the instant that dispatch is observed
	// complete or the session reaches any terminal / AwaitingReview stage. Pure in-workflow
	// state served by view() (NOT boundary-stamped like StageName) — setting it issues NO
	// Temporal history command, so no GetVersion gate is needed (the honesty invariant). Also
	// reused, always at its zero value, by AssembleSDPReviewWorkflow — the SDP assembly is
	// server-side (no role/step to stamp), so its view() naturally reports none/none/0.
	activeRole  ActiveRole
	activeStep  ActiveStep
	activeRound int
}

// markActive stamps the in-flight sub-step (role / step / round) the loading pill renders.
// Pure workflow-local state; no history command.
func (s *coAuthorState) markActive(role ActiveRole, step ActiveStep, round int) {
	s.activeRole = role
	s.activeStep = step
	s.activeRound = round
}

// clearActive resets the sub-step to none/none/0 — the honest "no role is working" state
// the pill falls back to today's plain "DRAFTING…" copy for. Called on observed dispatch
// completion and on every terminal / AwaitingReview stage.
func (s *coAuthorState) clearActive() {
	s.activeRole = ActiveRoleNone
	s.activeStep = ActiveStepNone
	s.activeRound = 0
}

func (s *coAuthorState) view() (SessionStateView, error) {
	dm, err := draftModelFor(s.artifactKind, s.draft)
	if err != nil {
		return SessionStateView{}, err
	}
	return SessionStateView{
		ProjectID:     s.projectID,
		ArtifactKind:  s.artifactKind,
		Stage:         s.stage,
		Draft:         dm,
		Findings:      s.findings,
		FailureReason: strPtrOrNil(s.failureReason),
		ReviewThread:  reviewThreadToView(s.reviewThread),
		ActiveRole:    s.activeRole,
		ActiveStep:    s.activeStep,
		Round:         int64(s.activeRound),
	}, nil
}

func signalNotes(f *ReviewFeedback) string {
	if f != nil {
		return f.Notes
	}
	return ""
}

// slotAccessors is the flat kind→slot dispatch behind slotFor, in table form so the
// exhaustive gate (check: map) still fails on a missing ArtifactKind exactly like the
// former switch did.
var slotAccessors = map[projectstate.ArtifactKind]func(projectstate.Project) projectstate.ArtifactSlot{
	projectstate.KindMission:              func(p projectstate.Project) projectstate.ArtifactSlot { return p.Mission },
	projectstate.KindGlossary:             func(p projectstate.Project) projectstate.ArtifactSlot { return p.Glossary },
	projectstate.KindScrubbedRequirements: func(p projectstate.Project) projectstate.ArtifactSlot { return p.ScrubbedRequirements },
	projectstate.KindVolatilities:         func(p projectstate.Project) projectstate.ArtifactSlot { return p.Volatilities },
	projectstate.KindCoreUseCases:         func(p projectstate.Project) projectstate.ArtifactSlot { return p.CoreUseCases },
	projectstate.KindSystem:               func(p projectstate.Project) projectstate.ArtifactSlot { return p.SystemDesign },
	projectstate.KindOperationalConcepts:  func(p projectstate.Project) projectstate.ArtifactSlot { return p.OperationalConcepts },
	projectstate.KindStandardCheck:        func(p projectstate.Project) projectstate.ArtifactSlot { return p.StandardCheck },
	projectstate.KindPlanningAssumptions:  func(p projectstate.Project) projectstate.ArtifactSlot { return p.PlanningAssumptions },
	projectstate.KindActivityList:         func(p projectstate.Project) projectstate.ArtifactSlot { return p.ActivityList },
	projectstate.KindNetwork:              func(p projectstate.Project) projectstate.ArtifactSlot { return p.Network },
	projectstate.KindNormalSolution:       func(p projectstate.Project) projectstate.ArtifactSlot { return p.NormalSolution },
	projectstate.KindSubcriticalSolution:  func(p projectstate.Project) projectstate.ArtifactSlot { return p.SubcriticalSolution },
	projectstate.KindCompressedSolution:   func(p projectstate.Project) projectstate.ArtifactSlot { return p.CompressedSolution },
	projectstate.KindDecompressedSolution: func(p projectstate.Project) projectstate.ArtifactSlot { return p.DecompressedSolution },
	projectstate.KindRiskModel:            func(p projectstate.Project) projectstate.ArtifactSlot { return p.RiskModel },
	projectstate.KindSdpReview:            func(p projectstate.Project) projectstate.ArtifactSlot { return p.SdpReview },
}

// slotFor returns the named Project slot for a kind (Phase 1 + Phase 2). Internal
// (operates on the canonical projectstate.ArtifactKind); own-kind callers convert via
// toPSKind at the boundary. An unknown kind yields the zero slot, as before.
func slotFor(proj projectstate.Project, kind projectstate.ArtifactKind) projectstate.ArtifactSlot {
	accessor, ok := slotAccessors[kind]
	if !ok {
		return projectstate.ArtifactSlot{}
	}
	return accessor(proj)
}

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the projectDesignManager
// impl. It supplies the genWorkerManifest RegisterWorker consumes: the three workflow
// bodies under their registered names, the per-activity option-preset hook, and the
// genActivities dep threading. It also hosts the external RegisterManagerWorker
// entrypoint the composition root calls (cmd/server/main.go). This Manager has ZERO
// custom Temporal Activities (B9 + its follow-up ruling: the last one,
// StageArtifactForReviewActivity, was deleted when the designSessionAccess Stage op's
// model param became the codable ModelEnvelope at the schema) — every Activity is
// generated and registered by the generated RegisterWorker.
//
// The three estimate Engines (Estimation / OperationEst / Settlement) are called DIRECTLY
// in-workflow (deterministic, by value) and are NOT Activities; the durable-execution
// in-workflow primitives (awaitSignal / startTimer) are the Manager's own code.

// activityOptions returns the option-preset hook the generated invokers consult for the
// contract-backed RA Activities (projectState / pipeline / rail / designSession). A name
// with no entry falls back to the generated default (invokers.gen.go). Keyed by the
// generated registered activity name (<componentKey>.<opName>); every
// designSessionAccess.* entry below uses the same readProjectOpts/mutateOpts preset
// as the equivalent projectStateAccess entry.
func activityOptions() func(activityName string) (workflow.ActivityOptions, bool) {
	presets := map[string]workflow.ActivityOptions{
		"projectStateAccess.readProjectVersion":                  readProjectActivityOptions(),
		"projectStateAccess.advancePhase":                        mutateActivityOptions(),
		"agenticJobAccess.submitAgenticJob":                      dispatchActivityOptions(),
		"agenticJobAccess.observeAgenticJob":                     observeActivityOptions(),
		"sourceControlAccess.getInstallationToken":               mintCredActivityOptions(),
		"sourceControlAccess.openBranch":                         railActivityOptions(),
		"sourceControlAccess.openPullRequest":                    railActivityOptions(),
		"sourceControlAccess.getPullRequestStatus":               railActivityOptions(),
		"sourceControlAccess.postReview":                         railActivityOptions(),
		"sourceControlAccess.mergePullRequest":                   railActivityOptions(),
		"sourceControlAccess.syncManagedScaffold":                railActivityOptions(),
		"designSessionAccess.readProjectOnBranch":                readProjectActivityOptions(),
		"designSessionAccess.stageArtifactForReviewOnBranch":     mutateActivityOptions(),
		"designSessionAccess.commitArtifactWithProvenance":       mutateActivityOptions(),
		"designSessionAccess.rejectArtifactOnBranchWithComments": mutateActivityOptions(),
		"designSessionAccess.withdrawArtifactOnBranch":           mutateActivityOptions(),
		"designSessionAccess.setReviewCommentStatusOnBranch":     mutateActivityOptions(),
		"designSessionAccess.seedReviewCommentsOnBranch":         mutateActivityOptions(),
		// SP1 capture-seam: the episode ledger append rides its OWN envelope, never a
		// business one (see appendEpisodeActivityOptions).
		"episodeAccess.appendEpisode": appendEpisodeActivityOptions(),
	}
	return func(name string) (workflow.ActivityOptions, bool) {
		o, ok := presets[name]
		return o, ok
	}
}

// WorkerManifest assembles the genWorkerManifest RegisterWorker (worker.gen.go) consumes:
// the three workflow bodies under their registered names, the per-activity option-preset
// hook, and the genActivities threaded from the impl's stored published deps.
//
// The workflows receiver holds the generated invoker surface (Acts) — every
// contract-backed RA op (readProjectVersion / advancePhase / submit / observe / the
// seven rail verbs / the eight designSession verbs) is reached through it; the receiver
// carries no RA dep of its own.
func (m *projectDesignManager) WorkerManifest() genWorkerManifest {
	optsHook := activityOptions()

	wf := &workflows{
		Estimation:   m.estimator,
		OperationEst: m.opEstimator,
		Settlement:   m.settlement,
		Acts:         genInvokers{Opts: optsHook},
		// Rail is the PUBLISHED sourceControlAccess: nil ⇒ the PR rail is dormant and the
		// CoAuthor draft path runs the original main-path behavior. Held directly for the
		// gitEnabled gate; the seven rail verbs (including syncManagedScaffold, since B9) go
		// through the generated invoker surface (wf.Acts.Rail*).
		Rail: m.rail,
		Repo: m.repo,
	}

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindCoAuthor, Fn: wf.CoAuthorPhase2ArtifactWorkflow},
			{Name: executionKindSDPReview, Fn: wf.AssembleSDPReviewWorkflow},
			{Name: executionKindPhaseAdvance, Fn: wf.Phase2AdvanceWorkflow},
		},
		ActivityOptions: optsHook,
		Activities: genActivities{
			ProjectState:  m.projectState,
			Pipeline:      m.pipeline,
			Rail:          m.rail,
			DesignSession: m.designSession,
			Episodes:      m.episodes,
		},
	}
}

// RegisterManagerWorker wires the projectDesignManager onto a Temporal Worker polling the
// project-design task queue (projectDesignManager.md §6.1). It preserves the external call
// shape the composition root used before the generated-layer migration, asserting to the
// concrete *projectDesignManager the generated constructor returns and delegating to the
// generated RegisterWorker with the impl's WorkerManifest. Every Activity this Manager's
// workflows execute is generated, so the generated RegisterWorker registers the complete
// set — no explicit custom-Activity registration remains (B9 follow-up).
func RegisterManagerWorker(w worker.Worker, m ProjectDesignManager) {
	impl, ok := m.(*projectDesignManager)
	if !ok {
		panic("projectdesign: RegisterManagerWorker requires a *projectDesignManager from NewProjectDesignManager")
	}
	RegisterWorker(w, impl.WorkerManifest())
}
