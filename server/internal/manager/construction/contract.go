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
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// ---------------------------------------------------------------------------
// Public data contracts (constructionManager.md §3) — the Client surface.
//
// SCHEMA-FIRST: the port I/O models (PumpResult, ReplanSweepResult, FlaggedVariance,
// ActivityOverride, ConstructionSessionView, ReviewSet, Reviewer), the enums
// (OverrideKind, ConstructionStage, PipelinePhase), the named-string scalars
// (ProjectID, ActivityID) AND the ConstructionManager port interface are GENERATED
// into contract.gen.go from contract.schema.json (edit the schema + `make gen`; do
// NOT hand-edit the generated surface).
//
// FULL ENCAPSULATION: the generated contract carries this component's OWN
// named-string ProjectID / ActivityID and imports NO projectstate and NO Temporal.
// The Manager (which OWNS Temporal) converts to/from projectStateAccess's
// projectstate.ProjectID at the RA boundary (workflow.go / signals.go /
// gitforward.go). The consumer-side dependency interfaces (deps.go) and the Temporal
// Workflows struct stay hand-written and are NOT part of this contract.
// ---------------------------------------------------------------------------

// Compile-time proof the concrete Manager satisfies the generated ConstructionManager
// port. Each op leads with the Manager-layer call Context (fwm.Context); the *Manager
// derives ctx := rc.Context inside (constructionmanager.go). The *ProjectID /
// *ActivityID pointer params are load-bearing (nil ⇒ sweep-all / project-level query).
var _ ConstructionManager = (*constructionManager)(nil)

// overrideKindName returns the canonical name for an override kind. Kept as a FREE
// FUNCTION (not a method) so the generated OverrideKind scalar carries no behavior
// (the contract surface is pure data).
func overrideKindName(k OverrideKind) string {
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

// ---------------------------------------------------------------------------
// Façade error model (constructionManager.md §3.5).
// CALLER/PROGRAMMER errors at the façade boundary — distinct from the workflow's
// own failure handling (Temporal RetryPolicy + the intervention/variance
// alternative paths inside the workflow body). Kinds used: ContractMisuse,
// FailedPrecondition, NotFound, Unauthorized, Infrastructure.
// ---------------------------------------------------------------------------

// ConstructionError is the typed façade error (constructionManager.md §3.5). It is
// an alias for fwm.Error so errors.As(&ConstructionError) call sites work.
type ConstructionError = fwm.Error

func newError(kind fwm.Kind, detail string) *fwm.Error {
	return fwm.New(kind, detail)
}
