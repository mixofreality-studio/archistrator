// Package construction is the constructionManager component of the aiarch server's
// Manager layer — the use-case façade that drives a project through Phase 3 of
// The Method (Construction), per the senior-frozen contract
// designs/aiarch/implementation/contracts/constructionManager.md (C-MCN).
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal / Query), it registers the nextActivity (30s) and
// replanSweep (5m) Schedules at startup, defines one Activity per ResourceAccess
// call, owns the Signal/Query handlers and the in-workflow primitives
// (awaitSignal / startTimer / executeChild), and derives the idempotency key
// "${workflowId}:${activityId}" passed down to each RA verb. Temporal lives ONLY
// in this component; the downstream Engines (handOffEngine, interventionEngine,
// reviewEngine — pure, in-workflow, by value) and ResourceAccess ports
// (projectStateAccess, artifactAccess, workerAccess, constructionPipelineAccess,
// durableExecutionAccess) import no Temporal.
//
// The FIVE frozen public ops (constructionManager.md §2):
//   - ExecuteNextActivity — Workflow (entry; scheduler-triggered pump; per-activity child)
//   - RunReplanSweep      — Workflow (entry; scheduler-triggered variance sweep)
//   - PauseProject        — Signal (operatorPauseRequested)
//   - OverrideActivity    — Signal (operatorOverride)
//   - GetSessionState     — Query (sessionState, read-only)
//
// File layout (mirrors internal/manager/systemdesign):
//   - constructionmanager.go : the Manager that translates public ops into Temporal client calls (§6.2)
//   - contract.go            : the public façade types + the consumer-side dep interfaces (§3, §5)
//   - workflow.go            : the Workflows deps struct + workflow bodies + signal/query handlers (§6.3, §6.6)
//   - activities.go          : the Manager-owned Activity wrappers, as methods on Workflows (§6.4)
//   - errors.go              : the port-error -> Temporal-error translation (§6.4)
//   - worker.go              : worker registration of workflows + activities + Schedules (§6.1)
//
// 2026-05-29 agent-role rework note (constructionManager.md top note + workerAccess.md
// §0b): the worker-text → typed-ConstructionOutput parse is NOT a "future
// constructionEngine" / Dispatch-FileUpload concern — workerAccess is now the
// generic typed worker (Generate / GenerateTypedData[T] / Cancel). This Manager's
// SEQUENCE owns the per-step prompt and asks worker.GenerateTypedData[artifact.ConstructionOutput]
// (Manager-Activity-wrapped) for the produced change, and worker.Cancel for the
// operator-pause / takeover abandon path (the DSL-static Cancel(key) edge). The
// five frozen public ops are stable across this; see C-MCN.md completion notes.
package construction

import (
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// ---------------------------------------------------------------------------
// Public data contracts (constructionManager.md §3) — the Client surface.
// Infrastructure-opaque: no Temporal id is exposed here. The typed Method /
// construction models are referenced from their owning ResourceAccess packages,
// not redefined (memory: Method data models live in their owning RA).
// ---------------------------------------------------------------------------

// ProjectID is the project aggregate identifier; a string newtype whose value IS
// the adopted repo name (name-as-identity, C-PM-Δ 2026-06-15), canonical in
// projectStateAccess (constructionManager.md §3.0). Re-exported as an alias so the
// façade reads in its own terms while staying one-source-of-truth.
type ProjectID = projectstate.ProjectID

// ActivityID identifies one Phase-3 construction/detailed-design/integration
// activity in the project's committed network (constructionManager.md §3.0). A
// string id (network.yaml activity ids land in head-state).
type ActivityID = string

// PumpResult is the result of one ExecuteNextActivity tick (constructionManager.md
// §3.1). {Dispatched: false} is the NORMAL "no eligible activity this tick" answer
// (not an error) — the 30s pump is mostly quiet between activity completions.
type PumpResult struct {
	Dispatched bool        `json:"dispatched"`
	ActivityID *ActivityID `json:"activityId,omitempty"`
}

// ReplanSweepResult is the result of one RunReplanSweep tick
// (constructionManager.md §3.2). A possibly-empty list of flagged variances. The
// sweep SURFACES, never auto-replans.
type ReplanSweepResult struct {
	FlaggedVariances []FlaggedVariance `json:"flaggedVariances,omitempty"`
}

// FlaggedVariance is one over-threshold variance surfaced to the operator
// dashboard (constructionManager.md §3.2). No auto-replan.
type FlaggedVariance struct {
	ProjectID  ProjectID  `json:"projectId"`
	ActivityID ActivityID `json:"activityId"`
	Summary    string     `json:"summary"`
}

// ActivityOverride is the operator's manual steer on one activity
// (constructionManager.md §3.3). Fed into the SAME decide→execute machinery as the
// automatic interventionEngine.DecideOnVariance path — the operator overrides the
// platform's automatic decision; the Manager is the executor either way.
type ActivityOverride struct {
	Kind  OverrideKind `json:"kind"`
	Notes string       `json:"notes"`
}

// OverrideKind is the closed set of operator override steers (constructionManager.md §3.3).
type OverrideKind int

const (
	// OverrideUnknown is the zero value (rejected as ContractMisuse).
	OverrideUnknown OverrideKind = iota
	// OverrideTakeover: platform takes over — re-dispatch under a changed
	// arrangement / reset the durable execution.
	OverrideTakeover
	// OverrideRetry: re-enter the dispatch path for this activity.
	OverrideRetry
	// OverrideSkip: record the activity exited with an operator-skip outcome.
	OverrideSkip
	// OverrideReassign: re-cast the worker class (operator-chosen).
	OverrideReassign
)

// String returns the canonical name for an override kind.
func (k OverrideKind) String() string {
	switch k {
	case OverrideTakeover:
		return "Takeover"
	case OverrideRetry:
		return "Retry"
	case OverrideSkip:
		return "Skip"
	case OverrideReassign:
		return "Reassign"
	default:
		return "Unknown"
	}
}

// ConstructionStage is the TECHNICAL construction-session progress stage
// (constructionManager.md §3.4). The *business* "which activities are committed /
// how far along" rollup is a head-state read off projectStateAccess (CQRS split,
// §6.6), not this enum.
type ConstructionStage int

const (
	// ConstructionStageUnknown is the zero value.
	ConstructionStageUnknown ConstructionStage = iota
	// StageDispatching: worker-class cast; work dispatched; pipeline not yet submitted.
	StageDispatching
	// StagePipelineRunning: construction pipeline submitted + observing.
	StagePipelineRunning
	// StageReviewing: output staged; review fan-out in flight.
	StageReviewing
	// StageAwaitingTakeover: variance escalated; awaiting operator override/takeover.
	StageAwaitingTakeover
	// StagePaused: operator-paused (applyPausePolicy executed); terminal-until-resume.
	StagePaused
	// StageExited: recordActivityExited applied; terminal for this activity.
	StageExited
)

// ConstructionSessionView is a point-in-time, read-only view of one construction
// session's TECHNICAL progress (constructionManager.md §3.4). It is the answer to
// GetSessionState (a Temporal Query), NOT the business-state read.
type ConstructionSessionView struct {
	ProjectID     ProjectID         `json:"projectId"`
	ActivityID    *ActivityID       `json:"activityId,omitempty"`
	Stage         ConstructionStage `json:"stage"`
	PipelinePhase *PipelinePhase    `json:"pipelinePhase,omitempty"` // nil when no pipeline in flight
	ReviewSet     *ReviewSet        `json:"reviewSet,omitempty"`     // nil before review
	Variance      *FlaggedVariance  `json:"variance,omitempty"`      // nil when none
}

// ---------------------------------------------------------------------------
// Façade error model (constructionManager.md §3.5).
// CALLER/PROGRAMMER errors at the façade boundary — distinct from the workflow's
// own failure handling (Temporal RetryPolicy + the intervention/variance
// alternative paths inside the workflow body). Kinds used: ContractMisuse,
// FailedPrecondition, NotFound, Unauthorized, Infrastructure.
// ---------------------------------------------------------------------------

// ConstructionError is the typed façade error (constructionManager.md §3.5). It is
// an alias for fwmanager.Error so errors.As(&ConstructionError) call sites work.
type ConstructionError = fwmanager.Error

func newError(kind fwmanager.Kind, detail string) *fwmanager.Error {
	return fwmanager.New(kind, detail)
}
