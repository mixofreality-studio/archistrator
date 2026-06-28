package systemdesign

import (
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
)

// SystemDesignManager is the generated service-contract interface for this component
// — the public use-case surface of the systemDesignManager façade
// (systemDesignManager.md §2). Each op leads with the Manager-layer call Context
// (fwmanager.Context, embedding context.Context + the Principal); the
// *systemDesignManager derives ctx := rc.Context inside. The concrete
// *systemDesignManager satisfies it; the internal dependency seams + the Temporal
// Workflows struct stay hand-written and are NOT part of this contract.

// Compile-time proof the concrete systemDesignManager satisfies the generated
// SystemDesignManager port. Each op leads with the Manager-layer call Context
// (fwmanager.Context); the *systemDesignManager derives ctx := rc.Context inside.
var _ SystemDesignManager = (*systemDesignManager)(nil)

// systemDesignManager is the systemDesignManager façade. It exposes the public
// use-case ops (systemDesignManager.md §2) and OWNS Temporal. The 2026-05-29 re-cut
// adds startSystemDesign (parent kickoff). The Temporal-backed ops:
//   - StartSystemDesign  — Workflow (entry, parent SystemDesignPhaseWorkflow)
//   - RequestArtifactDraft — Workflow (entry, child CoAuthorArtifactWorkflow gate)
//   - SubmitReviewDecision — Signal (reviewDecision, to the child gate)
//   - AdvancePhase         — Workflow (entry, short-lived seal)
//   - GetSessionState      — Query (sessionState, read-only)
//
// Rendering is no longer a Manager concern: server-side rendering was removed
// (the client renders typed models). The Manager exposes no RenderArtifact op and
// holds no RenderingEngine.
//
// The façade methods use only the Temporal client + projectStateAccess (for the
// StartSystemDesign ResearchInput precondition + the sync SetResearchInput write op).
// It ALSO stores the three Worker-side deps it was constructed with — the published
// constructionpipeline.ConstructionPipelineAccess (design-job dispatch), the published
// sourcecontrol.SourceControlAccess (the PR rail), and the per-project repo resolver —
// so RegisterWorker can wire them (via the package's folded adapters) into the
// hand-written Temporal Workflows. The former exported consumer-mirror interfaces +
// the composition-root adapters are RETIRED; the manager now depends on the deps'
// PUBLISHED interfaces and adapts them internally (Option-B boundary mapping).
//
// Pre-condition checks the contract puts on the façade (Phase-1 kind, non-empty
// projectId, Reject-requires-feedback, ResearchInput present) are enforced here before
// any downstream call (§2, §3).
type systemDesignManager struct {
	client       client.Client
	projectState projectstate.ProjectStateAccess
	pipeline     constructionpipeline.ConstructionPipelineAccess
	rail         sourcecontrol.SourceControlAccess
	repo         func(projectID ProjectID) (sourcecontrol.RepoRef, bool)
}

// newSystemDesignManager is the hand-written, unexported builder the generated
// NewSystemDesignManager constructor delegates to. It wires the Temporal client + the
// published deps into the façade. The façade itself uses only client + projectState;
// pipeline/rail/repo are stored for RegisterWorker (rail may be nil — a dev server
// with no source-control credentials runs the design spine repo-less).
func newSystemDesignManager(c client.Client, ps projectstate.ProjectStateAccess, pipeline constructionpipeline.ConstructionPipelineAccess, rail sourcecontrol.SourceControlAccess, repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)) *systemDesignManager {
	return &systemDesignManager{client: c, projectState: ps, pipeline: pipeline, rail: rail, repo: repo}
}

// StartSystemDesign — op 2.0 (2026-05-29). Temporal Workflow (entry;
// StartWorkflow, id {projectId}:systemDesign) starting the PARENT
// SystemDesignPhaseWorkflow, which drives the seven Phase-1 steps in fixed Method
// order, spawns the per-step child gate, auto-advances on each human Approve, and
// seals Phase 1.
//
// Pre-condition (systemDesignManager.md §2.0): the project exists and its
// ResearchInput slot is PRESENT (read via projectStateAccess.ReadProject) — else
// FailedPrecondition ("research not populated"). Idempotent on the id (a redundant
// start returns the running SessionRef). The ResearchInput is woven into the
// mission-draft prompt at step 1 (inside the child gate's draft step).
//
// SYNC from the Client's POV: returns once the parent start is durably accepted,
// not once Phase 1 completes (it spans days of human review; the SPA polls
// getSessionState / reads head-state).
func (m *systemDesignManager) StartSystemDesign(rc fwmanager.Context, projectID ProjectID) (SessionRef, error) {
	ctx := rc.Context
	if projectID == "" {
		return "", newError(fwmanager.ContractMisuse, "empty projectId")
	}

	// Pre-condition: ResearchInput must be present. A brand-new project with no row
	// (fwra.NotFound) likewise fails the precondition — research has not been set.
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isResearchReadNotFound(err) {
			return "", newError(fwmanager.FailedPrecondition, "research not populated (project has no state)")
		}
		return "", mapReadProjectError(err)
	}
	if proj.ResearchInput.IsZero() {
		return "", newError(fwmanager.FailedPrecondition, "research not populated")
	}

	wfID := systemDesignPhaseWorkflowID(projectID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindPhase, PhaseInput{ProjectID: projectID})
	if err != nil {
		return "", mapStartError(err)
	}
	return NewSessionRef(we.GetID()), nil
}

// isResearchReadNotFound reports whether a ReadProject error is the brand-new
// project NotFound (no row yet) — which, for StartSystemDesign, is itself a
// FailedPrecondition (research not set), not an infrastructure fault.
func isResearchReadNotFound(err error) bool {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		return raErr.Kind == fwra.NotFound
	}
	return false
}

// requestArtifactDraft — op 2.1. Temporal SIGNAL-WITH-START on workflow id
// {projectId}:{artifactKind}. This is BOTH the first-draft kickoff AND the
// "Retry draft" recovery lever:
//
//   - First request (no running session): starts the CoAuthorArtifactWorkflow,
//     which drafts immediately. The buffered redraft signal is harmless (the fresh
//     run does not await the refused gate before it drafts).
//   - Retry on a REFUSED session (Bug B; the session ended a draft attempt in the
//     queryable StageRefused state after a terminal worker fault): the redraft
//     signal is delivered to the still-live, suspended workflow, which re-enters the
//     draft loop in place — no new workflow run, the getSessionState Query stays
//     continuously available.
//   - Retry on an otherwise-running session: idempotent — the existing execution is
//     reused (UseExisting), and the redraft signal is consumed only if/when that
//     session is at the refused gate.
//
// Signal-with-start is the one call that covers all three (start-if-absent, signal
// the existing run otherwise), preserving the §2.1 idempotent-on-id post-condition.
//
// RequestArtifactDraft is the exported public op.
func (m *systemDesignManager) RequestArtifactDraft(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, feedback *ReviewFeedback) (SessionRef, error) {
	ctx := rc.Context
	if projectID == "" {
		return "", newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !ArtifactKindIsPhase1(kind) {
		return "", newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-1 kind")
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	opts := client.StartWorkflowOptions{
		ID:        wfID,
		TaskQueue: TaskQueue,
		// Idempotent on the id: a redundant start of an already-running session
		// reuses the existing execution rather than failing or duplicating
		// (systemDesignManager.md §2.1 post-condition). The signal rides along.
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	in := CoAuthorInput{ProjectID: projectID, ArtifactKind: kind, Feedback: feedback}

	we, err := m.client.SignalWithStartWorkflow(ctx, wfID, SignalRedraft, RedraftSignal{Feedback: feedback}, opts, ExecutionKindCoAuthor, in)
	if err != nil {
		return "", mapStartError(err)
	}
	return NewSessionRef(we.GetID()), nil
}

// submitReviewDecision — op 2.2. Temporal Signal (SignalWorkflow to workflow id
// {projectId}:{artifactKind}, signal reviewDecision). feedback required when
// decision == Reject.
//
// SubmitReviewDecision is the exported public op.
func (m *systemDesignManager) SubmitReviewDecision(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, decision ReviewDecision, feedback *ReviewFeedback) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !ArtifactKindIsPhase1(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-1 kind")
	}
	switch decision {
	case ReviewApprove, ReviewWithdraw:
		// ok
	case ReviewReject:
		if feedback == nil || feedback.Notes == "" {
			return newError(fwmanager.ContractMisuse, "Reject requires feedback")
		}
	default:
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	sig := ReviewDecisionSignal{Decision: decision, Feedback: feedback}
	if err := m.client.SignalWorkflow(ctx, wfID, "", SignalReviewDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// advancePhase — op 2.3. Temporal Workflow (entry; StartWorkflow, workflow id
// {projectId}:phaseAdvance). Returns the gating outcome.
//
// AdvancePhase is the exported public op.
func (m *systemDesignManager) AdvancePhase(rc fwmanager.Context, projectID ProjectID) (PhaseAdvanceResult, error) {
	ctx := rc.Context
	if projectID == "" {
		return PhaseAdvanceResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}

	wfID := phaseAdvanceWorkflowID(projectID)
	opts := client.StartWorkflowOptions{
		ID:        wfID,
		TaskQueue: TaskQueue,
	}
	in := PhaseAdvanceInput{ProjectID: projectID}

	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindPhaseAdvance, in)
	if err != nil {
		return PhaseAdvanceResult{}, mapStartError(err)
	}

	var result PhaseAdvanceResult
	if err := we.Get(ctx, &result); err != nil {
		return PhaseAdvanceResult{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return result, nil
}

// getSessionState — op 2.4. Temporal Query (QueryWorkflow, query sessionState,
// read-only). Returns a point-in-time technical view without mutating state.
//
// GetSessionState is the exported public op.
func (m *systemDesignManager) GetSessionState(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) (SessionStateView, error) {
	ctx := rc.Context
	if projectID == "" {
		return SessionStateView{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	wfID := coAuthorWorkflowID(projectID, kind)

	enc, err := m.client.QueryWorkflow(ctx, wfID, "", QuerySessionState)
	if err != nil {
		return SessionStateView{}, mapQueryError(err)
	}
	var view SessionStateView
	if err := enc.Get(&view); err != nil {
		return SessionStateView{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return view, nil
}

// SetResearchInput — op 2.6 (2026-05-30). SYNCHRONOUS, non-Temporal: it records
// the Phase-1 ResearchInput Method INPUT so a fresh project can satisfy the
// StartSystemDesign ResearchInput-present precondition through the UI. A single
// idempotent head-state write via projectStateAccess.SetResearchInput, with no
// Temporal primitive (no workflow, signal, gate, or slot transition).
//
// Body (systemDesignManager.md §2.6): read the current head Version via
// ReadProject, derive a stable idempotencyKey for "set research input on this
// project", and write. On the RA's fwra.Conflict (a concurrent writer bumped the
// version under us) re-read and re-apply on the sync path, bounded. There is NO
// workflow, signal, gate, or slot transition — ResearchInput is a Method INPUT,
// not a co-authored artifact (no AwaitingReview/Committed lifecycle).
//
// Returns the resulting head Version (the SPA may use it for optimistic display;
// the frozen surface is the write itself).
func (m *systemDesignManager) SetResearchInput(rc fwmanager.Context, projectID ProjectID, research ResearchInput) (Version, error) {
	ctx := rc.Context
	if projectID == "" {
		return 0, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if researchIsZero(research) {
		return 0, newError(fwmanager.ContractMisuse, "empty research (no sources)")
	}

	key := researchInputIdempotencyKey(projectID, research)
	psID := projectstate.ProjectID(projectID)
	psResearch := toPSResearch(research)

	// Sync-path optimistic-concurrency loop. The first write uses the head Version
	// just read; on a Conflict (a concurrent mutation bumped the row) re-read and
	// re-apply. Bounded so a pathological write-storm cannot spin forever.
	var lastErr error
	for attempt := 0; attempt < setResearchInputMaxAttempts; attempt++ {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			return 0, mapReadProjectError(err)
		}

		newVersion, err := m.projectState.SetResearchInput(fwra.Context{Context: ctx, IdempotencyKey: key}, psID, proj.Version, psResearch)
		if err == nil {
			return Version(newVersion), nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue // re-read head Version, re-apply (same idempotencyKey)
		}
		return 0, mapSetResearchInputError(err)
	}
	return 0, fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "projectStateAccess.SetResearchInput: exhausted conflict retries")
}

// setResearchInputMaxAttempts bounds the sync-path re-read/re-apply loop.
const setResearchInputMaxAttempts = 5

// researchInputIdempotencyKey derives the stable logical idempotency key for
// "set research input on this project". Unlike the workflow Activities (which key
// by "${workflowId}:${activityId}"), this sync op has no Temporal context, so the
// key is derived from the project id plus a content fingerprint: a retried write
// of the SAME research collapses to a no-op in the RA dedup ledger, while a
// genuinely new research payload is a distinct logical mutation.
func researchInputIdempotencyKey(projectID ProjectID, research ResearchInput) fwra.IdempotencyKey {
	h := fnv.New64a()
	for _, s := range research.Sources {
		_, _ = h.Write([]byte(s.Title))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(s.Content))
		_, _ = h.Write([]byte{0})
	}
	return fwra.IdempotencyKey(fmt.Sprintf("%s:setResearchInput:%x", projectID, h.Sum64()))
}

// mapSetResearchInputError converts projectStateAccess SetResearchInput errors
// into fwmanager.Error on the sync write path. fwra.NotFound → NotFound (no
// project aggregate yet — the caller may need to open it first); fwra.ContractMisuse
// → ContractMisuse; everything else (incl. unrecovered Conflict) → Infrastructure
// with retryability preserved.
func mapSetResearchInputError(err error) error {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		default:
			mapped := fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess.SetResearchInput")
			mapped.Retryable = raErr.Retryable
			return mapped
		}
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// isRAConflict reports whether err is the RA optimistic-concurrency conflict
// (fwra.Conflict) returned DIRECTLY on the sync path — the signal to re-read the
// head Version and re-apply. (Distinct from workflow.go's isConflict, which
// inspects the Temporal-wrapped ApplicationError on the replayed Activity path.)
func isRAConflict(err error) bool {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		return raErr.Kind == fwra.Conflict
	}
	return false
}

// mapReadProjectError converts projectStateAccess errors into fwmanager.Error
// for the sync read op. fwra.NotFound → NotFound (a brand-new / unknown project),
// other fwra.* errors → Infrastructure with the original retryability preserved.
func mapReadProjectError(err error) error {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		default:
			return fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess.ReadProject")
		}
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// --- error mapping at the façade boundary -----------------------------------

func mapStartError(err error) error {
	// A "workflow already started" race under UseExisting policy is benign; the
	// SDK surfaces it as *serviceerror.WorkflowExecutionAlreadyStarted, but with
	// UseExisting the ExecuteWorkflow returns the existing handle without error.
	// Any error here is treated as a infrastructure fault.
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
	return newError(fwmanager.Infrastructure, err.Error())
}

// isNotFound reports whether the Temporal error indicates the addressed
// execution does not exist.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	// serviceerror.NotFound is the canonical "no such workflow" error; matched by
	// string to avoid a hard import of the api serviceerror package surface here.
	return errors.Is(err, errNotFoundSentinel) ||
		strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "NotFound")
}

var errNotFoundSentinel = errors.New("not found")
