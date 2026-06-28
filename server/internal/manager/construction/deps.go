package construction

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// This file declares the Manager's CONSUMER-SIDE dependency interfaces (the Go
// "accept interfaces" idiom). Per the senior hand-off, several collaborators are
// not yet built (their own C-* construction activities have not run), so this
// Manager is built against their FROZEN CONTRACTS as interfaces it declares here,
// and unit-tested with fakes:
//
//   - HandOffEngine               — handOffEngine.md §2/§3 (FROZEN; not yet built)
//   - InterventionEngine          — interventionEngine.md §2/§3 (FROZEN; not yet built)
//   - ReviewEngine                — reviewEngine seam (hand-run; OQ-4 — see C-MCN.md)
//   - ConstructionPipelineAccess  — constructionPipelineAccess.md §2/§3 (FROZEN; not yet built)
//
// The collaborators that DO exist are imported directly and consumed via narrow
// consumer interfaces declared here so the test fakes stay small and so the
// concrete RA types satisfy them structurally:
//
//   - ProjectStateAccess (read + the additive construction-transition verbs)  — exists
//   - ArtifactAccess (StoreConstructionOutput / RetrieveConstructionOutput)   — exists
//   - WorkerAccess (the generic typed worker: Generate / Cancel)             — exists
//   - DurableExecutionAccess (RegisterSchedule)                              — exists
//
// The data types each not-yet-built Engine/RA exchanges are declared here in the
// Manager-local seam form mirroring the frozen contract, to be replaced by an
// import of the owning package when that component lands. This keeps the Method
// discipline "models live in their owning RA/Engine" intact: when the owner ships,
// these local mirrors are deleted and the import substituted; no public façade op
// changes (constructionManager.md OQ-1/OQ-7).

// ===========================================================================
// projectStateAccess — EXISTS. Narrow consumer interface (read + the additive
// Phase-3 construction-transition verbs). The concrete *projectstate.Store
// satisfies this.
// ===========================================================================

// ProjectStateAccess is the Manager's narrow consumer view of projectStateAccess:
// the whole-aggregate read plus the additive Phase-3 transition verbs
// (constructionManager.md §5.3; projectstate/construction.go). No cred parameter
// (the Manager-consumer view; cred is threaded separately via GitActivityStatusAccess).
type ProjectStateAccess interface {
	ReadProject(ctx context.Context, projectID projectstate.ProjectID) (projectstate.Project, error)
	ReadProjectVersion(ctx context.Context, projectID projectstate.ProjectID) (projectstate.Version, error)
	RecordChangeReviewed(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityExited(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, outcome projectstate.ActivityOutcome, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityFailed(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, reason projectstate.FailureReason, detail string, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordOperatorPaused(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, reason string, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordPhaseStarted(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, phase projectstate.ActivityMethodPhase, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordPhaseCompleted(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, phase projectstate.ActivityMethodPhase, artifactRef string, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordServiceContractProduced(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, component string, contract projectstate.ServiceContract, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordPhaseArtifactProduced(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, mapKey string, payload projectstate.PhaseArtifactPayload, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
}

// ===========================================================================
// git-forward slice (C-MCN-GIT) — TWO additive consumer interfaces, optional/
// nil-tolerant. The construction Manager is the ONLY component that touches both
// the PR rail (sourceControlAccess) and the per-activity git head-state mirror
// (projectStateAccess §GIT-HEAD-STATE) — D-PA-GIT §5. Both are Manager→RA downward
// edges (legal per [[the-method-layers]]); the `cred` is Manager-minted via
// GetInstallationToken and threaded IN (RA imports no Temporal). When neither is
// wired (the live Postgres-store composition that predates the GitStore), the
// git-forward lifecycle is dormant and the existing non-git spine is unchanged.
// ===========================================================================

// SourceControlRail is the Manager's consumer view of the FROZEN IPullRequestRail
// face of sourceControlAccess (sourceControlAccess-pullrequestrail.md) plus the one
// lifecycle op the Manager needs to mint the credential (GetInstallationToken). The
// concrete *sourcecontrol.Access satisfies this structurally. Every provider-touching
// verb takes a Manager-threaded RepoCredential; the returns are opaque handles the
// Manager records via the git head-state verbs. ConfigureBranchProtection is the
// schedulerClient/provisioning concern, NOT consumed on the per-activity spine, so it
// is deliberately absent from this narrow consumer view.
type SourceControlRail interface {
	GetInstallationToken(ctx context.Context, repo sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error)
	OpenBranch(ctx context.Context, repo sourcecontrol.RepoRef, branch sourcecontrol.BranchName, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) (sourcecontrol.BranchRef, error)
	OpenPullRequest(ctx context.Context, repo sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) (sourcecontrol.PullRequestRef, error)
	GetPullRequestStatus(ctx context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, cred sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error)
	PostReview(ctx context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, review sourcecontrol.ReviewSubmission, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) error
	MergePullRequest(ctx context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) (sourcecontrol.MergeResult, error)
}

// GitActivityStatusAccess is the Manager's consumer view of the additive
// per-activity git head-state Record verbs (projectStateAccess.md §GIT-HEAD-STATE;
// projectstate/gitactivity.go). The concrete *projectstate.GitStore satisfies it.
// Each carries the Manager-threaded cred + expectedVersion + idempotencyKey and is
// idempotent + ref-CAS-convergent (the workflow re-reads HEAD on Conflict and
// re-applies with the SAME key — no double-record).
//
// NOTE the cred type is projectstate.RepoCredential, NOT sourcecontrol's: the two RAs
// keep STRUCTURALLY-IDENTICAL-BUT-DISTINCT credential types (the NoSideways layer rule
// forbids projectstate importing sourcecontrol — projectstate/credential.go). The
// Manager is the seam that converts the rail's sourcecontrol.RepoCredential into the
// projectstate.RepoCredential it threads here (convertCred, gitforward.go).
type GitActivityStatusAccess interface {
	RecordActivityBranchOpened(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID, branch, branchRef, prRef, crLabel string, isRevert bool, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityCIObserved(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, ci projectstate.CICheckState, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityArchApproved(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityMerged(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)

	// RecordActivityStarted / RecordActivityCompleted mark the per-activity
	// construction lifecycle (Task 1: projectstate/gitactivityconstruction.go) —
	// Running at the top of the per-activity spine, Done at its end. They mirror the
	// four git head-state verbs above EXACTLY (cred-threaded, expectedVersion CAS,
	// idempotent on key); the concrete *projectstate.GitStore satisfies them. They
	// power the construction pump's eligibility selection (nextEligibleActivity reads
	// proj.ActivityConstruction): Started flips the activity out of NotStarted so the
	// pump does not re-dispatch it, Completed flips it to Done so dependents unblock.
	RecordActivityStarted(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
	RecordActivityCompleted(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, activityID string, cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey) (projectstate.Version, error)
}

// ===========================================================================
// artifactAccess — EXISTS. The Manager consumes the frozen interface directly.
// ===========================================================================

// ArtifactAccess is the Manager's consumer view of artifactAccess (the frozen
// content-addressable store for Phase-3 construction outputs, artifactAccess.md).
// The content address is a plain string compared by value (artifactAccess.md §2).
type ArtifactAccess interface {
	StoreConstructionOutput(ctx context.Context, output artifact.ConstructionOutput, idempotencyKey fwra.IdempotencyKey) (contentAddress string, err error)
	RetrieveConstructionOutput(ctx context.Context, contentAddress string) (artifact.ConstructionOutput, error)
}

// ===========================================================================
// workerAccess — EXISTS (the GENERIC typed worker, workerAccess.md §0b). The
// Manager's SEQUENCE owns the prompt and asks GenerateTypedData[ConstructionOutput];
// Cancel is the operator-pause / takeover abandon path (the DSL-static Cancel(key)
// edge). NOTE: the contract's "Dispatch(spec,key)→FileUpload" passages are
// SUPERSEDED by this generic surface (see contract top note + C-MCN.md).
// ===========================================================================

// WorkerAccess is the Manager's consumer view of the generic typed worker. Only
// Generate (the raw round-trip the GenerateTypedData[T] helper wraps) and Cancel
// are needed.
type WorkerAccess interface {
	Generate(ctx context.Context, spec workerGenerateSpec, idempotencyKey fwra.IdempotencyKey) ([]byte, error)
	Cancel(ctx context.Context, idempotencyKey fwra.IdempotencyKey) error
}

// ===========================================================================
// durableExecutionAccess — EXISTS. Only RegisterSchedule is a contract op this
// Manager calls (at startup). The in-workflow primitives (awaitSignal / startTimer
// / executeChild) are the Manager's OWN workflow code (D-DA category A), NOT RA
// methods — they live in workflow.go.
// ===========================================================================

// DurableExecutionAccess is the Manager's consumer view: the one startup op.
type DurableExecutionAccess interface {
	RegisterSchedule(ctx context.Context, spec scheduleSpec) error
}

// ===========================================================================
// handOffEngine — FROZEN, NOT YET BUILT. Consumer interface + local mirrors of its
// frozen types (handOffEngine.md §2/§3). DECIDE: the Manager feeds activity+policy
// BY VALUE and acts on the returned WorkerClass.
// ===========================================================================

// HandOffEngine mirrors handOffEngine.md §2 — pure, deterministic, called DIRECTLY
// in-workflow (no Activity, no idempotency key).
type HandOffEngine interface {
	PickWorkerClass(activity ConstructionActivity, policy HandOffPolicy) (WorkerClass, error)
}

// ConstructionActivity mirrors handOffEngine.md §3 (by-value activity snapshot).
// CRLabel/IsRevert are the git-forward (C-MCN-GIT) per-activity facts the Manager
// threads into the PR open + the head-state mirror: the cr-NN change-request group
// label (op-concepts §15 — a label across the CR activity PRs, "" when not a CR
// activity) and whether this activity's PR carries inverse commits (a revert PR). They
// are not handOff inputs; they are inert in the non-git spine.
type ConstructionActivity struct {
	ActivityID   string
	Kind         ActivityKind
	ComponentID  string
	Layer        string
	EstimateDays float64
	CRLabel      string
	IsRevert     bool
}

// ActivityKind mirrors handOffEngine.md §3.
type ActivityKind int

const (
	ActivityKindUnknown ActivityKind = iota
	ActivityKindDetailedDesign
	ActivityKindConstruction
	ActivityKindIntegration
	ActivityKindNoncoding
)

// String returns the canonical name for an activity kind.
func (k ActivityKind) String() string {
	switch k {
	case ActivityKindDetailedDesign:
		return "DetailedDesign"
	case ActivityKindConstruction:
		return "Construction"
	case ActivityKindIntegration:
		return "Integration"
	case ActivityKindNoncoding:
		return "Noncoding"
	default:
		return "Unknown"
	}
}

// WorkerClass mirrors handOffEngine.md §3 (the cast worker arrangement).
type WorkerClass int

const (
	WorkerClassUnknown WorkerClass = iota
	AIWorker
	HumanSeniorWorker
	HumanJuniorWorker
	// ArchitectOnly means skip dispatch and await the architect (handOffEngine OQ-2).
	ArchitectOnly
)

// String returns the canonical worker-class name (used as the worker's logical class).
func (c WorkerClass) String() string {
	switch c {
	case AIWorker:
		return "ai"
	case HumanSeniorWorker:
		return "humanSenior"
	case HumanJuniorWorker:
		return "humanJunior"
	case ArchitectOnly:
		return "architectOnly"
	default:
		return "unknown"
	}
}

// HandOffPolicy mirrors handOffEngine.md §3 (committed policy snapshot, by value).
type HandOffPolicy struct {
	PreferAI         bool
	SeniorOnlyLayers []string
}

// InterventionMode mirrors interventionEngine.md §3 — the coarse intervention regime
// the composition root translates into intervention.InterventionMode. The Manager
// holds it as an opaque policy value; the casting RULE behind each mode is
// package-internal to the Engine.
type InterventionMode int

const (
	// InterventionModeUnknown — no mode set (zero value).
	InterventionModeUnknown InterventionMode = iota
	// InterventionModeEscalateEverything — every variance escalates to an operator
	// (the supervised regime). Pairs with EscalationWaitTimeout == 0 (wait-forever).
	InterventionModeEscalateEverything
	// InterventionModeTiered — severity tiers + retry budgets decide retry vs
	// escalate vs takeover before flipping to a human (the autonomous-retry default).
	InterventionModeTiered
)

// InterventionPolicy mirrors interventionEngine.md §3 (committed policy snapshot,
// fed BY VALUE to the Engine). The casting RULE is package-internal to the Engine;
// the Manager holds the opaque policy value.
type InterventionPolicy struct {
	// Mode is the coarse intervention regime (Tiered default vs EscalateEverything
	// supervised). The composition root reads it instead of hard-coding the regime.
	Mode        InterventionMode
	RetryBudget int
	SLATier     string
}

// ===========================================================================
// interventionEngine — FROZEN, NOT YET BUILT. Consumer interface + local mirrors
// (interventionEngine.md §2/§3). DECIDE → the Manager EXECUTES.
// ===========================================================================

// InterventionEngine mirrors interventionEngine.md §2 — pure, deterministic,
// called DIRECTLY in-workflow. The Engine DECIDES; the Manager EXECUTES.
type InterventionEngine interface {
	DecideOnVariance(variance ConstructionVariance) (VarianceDirective, error)
	ApplyPausePolicy(projectID string, ctx PauseRequestContext) (PausePlan, error)
}

// ConstructionVariance mirrors interventionEngine.md §3.
type ConstructionVariance struct {
	ActivityID string
	Kind       VarianceKind
	Detail     string
	// AttemptCount is the number of supervision-loop attempts so far on this activity
	// (the loop's `attempt` counter). The Tiered intervention policy keys retry-budget
	// exhaustion on it (composition root threads it into intervention.ConstructionVariance);
	// EscalateEverything ignores it.
	AttemptCount    int
	OperatorSourced bool
}

// VarianceKind mirrors interventionEngine.md §3.
type VarianceKind int

const (
	VarianceKindUnknown VarianceKind = iota
	VarianceScheduleOverrun
	VariancePipelineFailed
	VarianceReviewFailed
	VarianceWorkerRefused
	VarianceOperatorOverride
)

// VarianceDirective mirrors interventionEngine.md §3 (the Engine's decision).
type VarianceDirective int

const (
	DirectiveUnknown VarianceDirective = iota
	DirectiveRetry
	DirectiveEscalate
	DirectiveTakeover
)

// PauseRequestContext mirrors interventionEngine.md §3.
type PauseRequestContext struct {
	Reason string
}

// PausePlan mirrors interventionEngine.md §3 (the plan the Manager EXECUTES).
type PausePlan struct {
	PipelinesToCancel []string
	RecordPaused      bool
	NotifyTargets     []string
}

// ===========================================================================
// reviewEngine — HAND-RUN (OQ-4). Consumer interface + local mirrors of the
// proposeReviews(change, componentId, artifactKind, architectureGraph, contracts)
// → ReviewSet seam (constructionManager.md §5.3 / §9 OQ-4). NO standalone contract
// file exists; this is the contract-edge gap routed to the architect.
// ===========================================================================

// ReviewEngine mirrors the reviewEngine seam — pure, deterministic, called
// DIRECTLY in-workflow. Returns the reviewer set the Manager fans out.
type ReviewEngine interface {
	ProposeReviews(change ReviewChange, componentID string, artifactKind string, architectureGraph string, contracts []string) (ReviewSet, error)
}

// ReviewChange is the by-value description of the produced change under review.
type ReviewChange struct {
	ActivityID  string
	ComponentID string
	// ContentAddress points at the staged construction output (artifactAccess).
	ContentAddress string
}

// ReviewSet / Reviewer are GENERATED into contract.gen.go (port I/O reached via
// ConstructionSessionView). The hand-written ReviewEngine consumer mirror above
// returns those generated types directly.

// ===========================================================================
// constructionPipelineAccess — FROZEN, NOT YET BUILT. Consumer interface + local
// mirrors (constructionPipelineAccess.md §2/§3). Each verb is Activity-wrapped.
// ===========================================================================

// ConstructionPipelineAccess mirrors constructionPipelineAccess.md §2.
type ConstructionPipelineAccess interface {
	SubmitConstructionPipeline(ctx context.Context, spec PipelineSpec, idempotencyKey fwra.IdempotencyKey) (PipelineHandle, error)
	ObserveConstructionPipeline(ctx context.Context, handle PipelineHandle) (PipelineObservation, error)
	CancelConstructionPipeline(ctx context.Context, handle PipelineHandle) error
}

// PipelineSpec mirrors constructionPipelineAccess.md §3 (infrastructure-neutral).
type PipelineSpec struct {
	ActivityID  string
	ComponentID string
	RepoURL     string
	Ref         string
	// Phase is the ActivityMethodPhase.String() for the current activity phase
	// being dispatched. Empty when the pipeline does not correspond to a specific
	// method phase (e.g. a legacy whole-activity dispatch).
	Phase string
	// Role is the WorkerClass.String() for the assigned worker role (handOffEngine
	// output). Empty when the role is determined by the pipeline infrastructure.
	Role string
}

// PipelineHandle mirrors constructionPipelineAccess.md §3.
type PipelineHandle struct {
	Name string
}

// PipelinePhase is GENERATED into contract.gen.go (reached via ConstructionSessionView);
// PipelineObservation below carries it on the hand-written constructionPipelineAccess
// consumer mirror.

// PipelineObservation mirrors constructionPipelineAccess.md §3.
type PipelineObservation struct {
	Phase      PipelinePhase
	Diagnostic string
}

// ===========================================================================
// Local seam types for the EXISTING workerAccess + durableExecutionAccess consumer
// interfaces (kept minimal; mirror the real package shapes structurally so the
// concrete RA types are adaptable at the composition root).
// ===========================================================================

// workerGenerateSpec mirrors worker.GenerateSpec's caller-owned fields the Manager
// fills (WorkerClass logical name + the assembled Prompt). The composition root
// adapts the concrete worker.WorkerAccess to this consumer interface.
type workerGenerateSpec struct {
	WorkerClass string
	Prompt      string
}

// scheduleSpec mirrors durableexecution.ScheduleSpec for the one startup op.
type scheduleSpec struct {
	ID           string
	WorkflowType string
	TaskQueue    string
	IntervalSecs int
	WorkflowID   string
}
