package construction

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// deps.go declares the Manager's INTERNAL downstream seams (unexported) plus the
// hand-written domain VALUE types the Manager's workflow vocabulary uses. Per the
// founder DI model (2026-06-28) the constructionManager's GENERATED constructor
// (contract.gen.go: NewConstructionManager) takes the dependencies' PUBLISHED
// interfaces directly; RegisterWorker (worker.go) folds those published interfaces
// into the unexported seams below (the former composition-root adapters are FOLDED
// into this package — adapters.go). The seams stay unexported so the package's only
// public surface is the generated interface + models + NewConstructionManager +
// RegisterWorker (+ the workflows struct the Temporal worker needs).
//
// How each seam is reached differs by determinism class:
//   - the three Engines (handOff / intervention / review) are PURE, deterministic,
//     called DIRECTLY in-workflow (no Activity wrapper — replay-safe);
//   - the ResourceAccess ports (projectState read / constructionTransition write /
//     gitActivityStatus / pipeline / artifacts / workers / rail) are I/O and reached
//     through Temporal Activities (activities.go / gitactivities.go).
//
// projectState reads are satisfied DIRECTLY by the published
// projectstate.ProjectStateAccess (narrowed to the two read verbs here); the
// construction-transition writes are satisfied DIRECTLY by the published
// projectstate.ConstructionTransitionAccess (cred-threaded). The git head-state seam
// composes the two published git facets (GitActivityStatusAccess +
// GitActivityConstructionAccess). The Engines + pipeline/artifact/worker seams are
// served by the folded adapters that bridge the published engine/RA shapes.

// ===========================================================================
// projectState read seam — the two whole-aggregate read verbs the Manager needs.
// rc-based: the published projectstate.ProjectStateAccess satisfies it directly
// (interface narrowing); the Manager builds the rc Context inside the read Activity.
// ===========================================================================

type projectStateReader interface {
	ReadProject(rc fwra.Context, projectID projectstate.ProjectID) (projectstate.Project, error)
	ReadProjectVersion(rc fwra.Context, projectID projectstate.ProjectID) (projectstate.Version, error)
}

// ===========================================================================
// construction-transition write seam — the additive Phase-3 head-state transition
// verbs (cred-threaded). The published projectstate.ConstructionTransitionAccess
// satisfies this directly (it is a superset). The Manager threads the rail-minted
// credential into every write (empty/zero in the dev/dry-run profile, which the
// local git store ignores).
// ===========================================================================

type constructionTransitionAccess interface {
	RecordChangeReviewed(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityExited(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, outcome projectstate.ActivityOutcome, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityFailed(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, reason projectstate.FailureReason, detail string, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordOperatorPaused(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, reason string, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordReviewPolicy(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, policy projectstate.ReviewPolicy, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordPhaseStarted(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, phase projectstate.ActivityMethodPhase, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordPhaseCompleted(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, phase projectstate.ActivityMethodPhase, artifactRef string, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordServiceContractProduced(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, component string, contract projectstate.ServiceContract, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordPhaseArtifactProduced(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, mapKey string, payload projectstate.PhaseArtifactPayload, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
}

// ===========================================================================
// git-forward slice (C-MCN-GIT). The construction Manager is the ONLY component
// touching both the PR rail (sourceControlAccess) and the per-activity git
// head-state mirror. Both are Manager→RA downward edges; the cred is Manager-minted
// via GetInstallationToken and threaded IN.
//
// The PR-rail consumer seam (the former sourceControlRail) is RETIRED — the rail ops
// are GENERATED (activities.gen.go) and reached through the generated invoker surface
// (genInvokers.Rail*); the workflow-side value mapping lives in gitforward.go. The git
// head-state mirror below stays a CUSTOM (plain-goType) seam.
// ===========================================================================

// gitActivityStatusAccess composes the two published per-activity git head-state
// facets — projectstate.GitActivityStatusAccess (branch/CI/+1/merge) and
// projectstate.GitActivityConstructionAccess (started/completed). The concrete
// *projectstate.GitStore (and the composition-root git adapter) satisfy both, so the
// builder type-asserts the gitActivityStatus dep onto this combined seam.
type gitActivityStatusAccess interface {
	RecordActivityBranchOpened(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID, branch, branchRef, prRef, crLabel string, isRevert bool, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityCIObserved(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, ci projectstate.CICheckState, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityArchApproved(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityMerged(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityStarted(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityCompleted(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
}

// ===========================================================================
// handOffEngine seam — pure, deterministic, called DIRECTLY in-workflow. The folded
// handoffAdapter (adapters.go) bridges the published handoff.HandOffEngine to it.
// ===========================================================================

type handOffEngine interface {
	PickWorkerClass(activity constructionActivity, policy handOffPolicy) (workerClass, error)
}

// constructionActivity is the by-value activity snapshot the Manager feeds the
// handOffEngine. CRLabel/IsRevert are the git-forward per-activity facts threaded
// into the PR open + the head-state mirror.
type constructionActivity struct {
	ActivityID   string
	Kind         activityKind
	ComponentID  string
	Layer        string
	EstimateDays float64
	CRLabel      string
	IsRevert     bool
	Phases       []projectstate.ActivityMethodPhase
}

// activityTypeName returns the canonical activity-type wire name
// ("service"/"frontend"/"testing") derived from the activity id. These are the exact
// keys the ReviewPolicy's GatedPhasesByType map is keyed by (and the keys the webApp
// PolicyPanel must emit) — the gate consults RequiresHuman(activityTypeName(), phase).
func (a constructionActivity) activityTypeName() string {
	return projectstate.DeriveType(a.ActivityID).String()
}

// activityKind is the Manager-local activity-kind vocabulary.
type activityKind int

const (
	activityKindUnknown activityKind = iota
	activityKindDetailedDesign
	activityKindConstruction
	activityKindIntegration
	activityKindNoncoding
)

// String returns the canonical name for an activity kind.
func (k activityKind) String() string {
	switch k {
	case activityKindUnknown:
		// zero-value sentinel, not a real activity kind — same as any unmapped value.
		return "Unknown"
	case activityKindDetailedDesign:
		return "DetailedDesign"
	case activityKindConstruction:
		return "Construction"
	case activityKindIntegration:
		return "Integration"
	case activityKindNoncoding:
		return "Noncoding"
	default:
		return "Unknown"
	}
}

// workerClass is the Manager-local cast worker arrangement.
type workerClass int

const (
	workerClassUnknown workerClass = iota
	aiWorker
	humanSeniorWorker
	humanJuniorWorker
	// architectOnly means skip dispatch and await the architect.
	architectOnly
)

// String returns the canonical worker-class name (used as the worker's logical class).
func (c workerClass) String() string {
	switch c {
	case workerClassUnknown:
		// zero-value sentinel, not a real worker class — same as any unmapped value.
		return "unknown"
	case aiWorker:
		return "ai"
	case humanSeniorWorker:
		return "humanSenior"
	case humanJuniorWorker:
		return "humanJunior"
	case architectOnly:
		return "architectOnly"
	default:
		return "unknown"
	}
}

// handOffPolicy is the committed policy snapshot (by value).
type handOffPolicy struct {
	PreferAI         bool
	SeniorOnlyLayers []string
}

// interventionMode is the coarse intervention regime the composition root translates
// into intervention.InterventionMode.
type interventionMode int

const (
	// no mode set (zero value).
	_ interventionMode = iota
	// interventionModeEscalateEverything — every variance escalates to an operator.
	interventionModeEscalateEverything
	// interventionModeTiered — severity tiers + retry budgets decide retry vs
	// escalate vs takeover before flipping to a human (the autonomous-retry default).
	interventionModeTiered
)

// interventionPolicy is the committed policy snapshot fed by value to the Engine.
type interventionPolicy struct {
	Mode        interventionMode
	RetryBudget int
	SLATier     string
}

// ===========================================================================
// interventionEngine seam — pure, deterministic, called DIRECTLY in-workflow. The
// Engine DECIDES; the Manager EXECUTES. The folded interventionAdapter bridges the
// published intervention.InterventionEngine to it.
// ===========================================================================

type interventionEngine interface {
	DecideOnVariance(variance constructionVariance) (varianceDirective, error)
	ApplyPausePolicy(projectID string, ctx pauseRequestContext) (pausePlan, error)
}

// constructionVariance is the by-value variance the Manager feeds the Engine.
type constructionVariance struct {
	ActivityID      string
	Kind            varianceKind
	Detail          string
	AttemptCount    int
	OperatorSourced bool
}

// varianceKind is the Manager-local variance taxonomy.
type varianceKind int

const (
	varianceKindUnknown varianceKind = iota
	varianceScheduleOverrun
	variancePipelineFailed
	varianceReviewFailed
	varianceWorkerRefused
	varianceOperatorOverride
)

// varianceDirective is the Engine's decision.
type varianceDirective int

const (
	directiveUnknown varianceDirective = iota
	directiveRetry
	directiveEscalate
	directiveTakeover
)

// pauseRequestContext is the by-value pause request.
type pauseRequestContext struct {
	Reason string
}

// pausePlan is the plan the Manager EXECUTES.
type pausePlan struct {
	PipelinesToCancel []string
	RecordPaused      bool
	NotifyTargets     []string
}

// ===========================================================================
// reviewEngine seam — pure, deterministic, called DIRECTLY in-workflow. Returns the
// reviewer set the Manager fans out. The folded reviewAdapter bridges the published
// review.ReviewEngine to it.
// ===========================================================================

type reviewEngine interface {
	ProposeReviews(change reviewChange, componentID string, artifactKind string, architectureGraph string, contracts []string) (ReviewSet, error)
}

// reviewChange is the by-value description of the produced change under review.
type reviewChange struct {
	ActivityID  string
	ComponentID string
	// ContentAddress points at the staged construction output (artifactAccess).
	ContentAddress string
}

// ===========================================================================
// constructionPipeline value vocabulary — the Manager's infrastructure-neutral
// dispatch spec / handle / observation. The pipeline ops are GENERATED and reached
// through the generated invoker surface (genInvokers.Pipeline*); these neutral types
// feed the workflow-side composition/mapping helpers (workflow.go) that bridge to the
// contract constructionpipeline.PipelineSpec / PipelineHandle / PipelineObservation.
// ===========================================================================

// pipelineSpec is the Manager's infrastructure-neutral dispatch spec.
type pipelineSpec struct {
	ActivityID  string
	ComponentID string
	RepoURL     string
	Ref         string
	// Phase is the ActivityMethodPhase.String() for the current activity phase.
	Phase string
	// Role is the WorkerClass.String() for the assigned worker role.
	Role string
}

// pipelineHandle is the Manager's opaque handle.
type pipelineHandle struct {
	Name string
}

// pipelineObservation is the Manager's neutral pipeline observation.
type pipelineObservation struct {
	Phase      PipelinePhase
	Diagnostic string
}
