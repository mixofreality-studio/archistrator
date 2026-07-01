package projectstate

import (
	"time"
)

// activityconstructionstatus.go holds the per-activity construction head-state types
// (Task 1: seed-archistrator-design-state). It mirrors the gitactivitystatus.go
// pattern precisely: a typed enum + a status record keyed by ActivityID, stored in
// Project.ActivityConstruction, populated only in Phase 3.
//
// DESIGN: this is the dry-run construction pump's foundation. The Phase enum captures
// the coarse lifecycle (not started / running / done); the timestamps (StartedAt,
// CompletedAt) are SERVER-RESOLVED at commit time (this is RA code, time.Now() is fine
// here — no Temporal). The map is lazily allocated: nil until the first Record* verb.

// ActivityConstructionPhase is the coarse per-activity construction lifecycle.
type ActivityConstructionPhase int

const (
	// ActivityConstructionNotStarted is the zero value — the activity has not yet
	// been dispatched by the construction pump.
	ActivityConstructionNotStarted ActivityConstructionPhase = iota
	// ActivityConstructionRunning — the activity's construction agent is in progress.
	ActivityConstructionRunning
	// ActivityConstructionDone — the activity's construction completed (agent finished).
	ActivityConstructionDone
	// ActivityConstructionFailed — the activity's construction reached a terminal
	// FAILURE (a cancelled/failed/timed-out pipeline, an exhausted variance budget, or
	// an escalation that timed out). Distinct from Done: the work did NOT integrate.
	// This is a STORED terminal — the CoarsePhase deriver short-circuits on it so it is
	// never recomputed back to Running/Done (see CoarsePhase's guard).
	ActivityConstructionFailed
)

// String returns the canonical wire name for the construction phase (used in JSON
// and log output). Mirrors CICheckState.String() and ActivityOutcome.String().
func (p ActivityConstructionPhase) String() string {
	switch p {
	case ActivityConstructionRunning:
		return "running"
	case ActivityConstructionDone:
		return "done"
	case ActivityConstructionFailed:
		return "failed"
	default:
		return "notStarted"
	}
}

// FailureReason is the closed enum of terminal-failure causes recorded on an
// activity's construction head-state when it reaches ActivityConstructionFailed.
// It lets the console explain WHY the activity is no longer pending (a cancelled
// run, an exhausted retry budget, an escalation nobody answered, …) rather than
// leaving it stuck Running forever.
type FailureReason int

const (
	// FailureReasonUnknown is the zero value (no failure recorded).
	FailureReasonUnknown FailureReason = iota
	// PipelineFailed — the construction pipeline reached a terminal FAILURE conclusion.
	PipelineFailed
	// PipelineCancelled — the construction pipeline run was cancelled.
	PipelineCancelled
	// PipelineTimedOut — the construction pipeline timed out (or the observe poll
	// budget was exhausted without a terminal phase).
	PipelineTimedOut
	// VarianceExhausted — the supervision loop exhausted its variance/retry budget.
	VarianceExhausted
	// EscalationTimedOut — an escalation waited for an operator override that never
	// came within the bounded escalation-wait window.
	EscalationTimedOut
)

// String returns the canonical wire name for the failure reason.
func (r FailureReason) String() string {
	switch r {
	case PipelineFailed:
		return "pipelineFailed"
	case PipelineCancelled:
		return "pipelineCancelled"
	case PipelineTimedOut:
		return "pipelineTimedOut"
	case VarianceExhausted:
		return "varianceExhausted"
	case EscalationTimedOut:
		return "escalationTimedOut"
	default:
		return "unknown"
	}
}

// PhaseCompletion is one App-A internal phase record within an activity's Phases slice.
// Binary exit (App A §1.1): Completed=true only when the review gate passed. Weight
// is the fraction of activity progress this phase carries (sums to 100 per type).
// ArtifactRef is a pointer into phaseArtifacts/serviceContracts/Produced — set by
// RecordPhaseCompleted.
type PhaseCompletion struct {
	Phase       ActivityMethodPhase `json:"phase"`
	Weight      int                 `json:"weight"`
	Label       string              `json:"label,omitempty"`
	Completed   bool                `json:"completed,omitempty"`
	CompletedAt *time.Time          `json:"completedAt,omitempty"`
	ArtifactRef string              `json:"artifactRef,omitempty"`
}

// ActivityConstructionStatus is the per-activity construction head-state record.
// One per construction-network activity, keyed by ActivityID in
// Project.ActivityConstruction. Additive, populated only in Phase 3.
type ActivityConstructionStatus struct {
	// ActivityID is the network activity id — the map key (NAME-as-identity).
	ActivityID string `json:"activityID"`
	// Type is the canonical activity-type axis (§2.1 design). Replaces Kind.
	Type ActivityType `json:"type,omitempty"`
	// Variant discriminates testing sub-types (only set when Type==ActivityTypeTesting).
	Variant TestingVariant `json:"variant,omitempty"`
	// Phase is the COMPUTED coarse lifecycle (NotStarted/Running/Done). Derived from
	// Phases at read time via CoarsePhase — kept for back-compat with existing readers.
	Phase ActivityConstructionPhase `json:"phase"`
	// Phases is the App-A internal phase set. Set once by phaseSetFor at activity start;
	// individual entries are marked Completed by RecordPhaseCompleted.
	Phases []PhaseCompletion `json:"phases,omitempty"`
	// CurrentPhase is the phase the workflow loop is currently executing.
	CurrentPhase ActivityMethodPhase `json:"currentPhase,omitempty"`
	// StartedAt is the server-resolved timestamp when RecordActivityStarted committed.
	StartedAt *time.Time `json:"startedAt,omitempty"`
	// CompletedAt is the server-resolved timestamp when RecordActivityCompleted committed.
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	// Kind is the legacy field — kept for JSON back-compat with seeded project.json entries.
	// New code reads Type instead. The two fields share the same underlying int encoding
	// (ActivityKind = ActivityType alias from Task 1), so existing seeded values decode correctly.
	Kind ActivityKind `json:"kind,omitempty"`
	// BuildStatus is the COMPUTED finer build-status lens. Derived from Phases+CurrentPhase
	// at read time via CoarseBuildStatus — kept for back-compat.
	BuildStatus ActivityBuildStatus `json:"buildStatus,omitempty"`
	// Produced is the seeded list of artifacts this activity produced (contracts/code).
	Produced []ProducedArtifact `json:"produced,omitempty"`
	// FailureReason is set when Phase == ActivityConstructionFailed — the closed-enum
	// cause of the terminal failure (cancelled/failed/timed-out pipeline, exhausted
	// variance budget, escalation timeout). Zero (FailureReasonUnknown) otherwise.
	FailureReason FailureReason `json:"failureReason,omitempty"`
	// FailureDetail is the human-readable diagnostic captured alongside FailureReason
	// (the pipeline's neutral diagnostic / a short escalation note). Empty otherwise.
	FailureDetail string `json:"failureDetail,omitempty"`
}

// phaseSetFor returns the ordered phase set (with weights) for the given activity type
// and testing variant. The returned slice is a fresh copy — callers may mutate it.
// This is the authoritative App-A phase table (v3 design §1). Pure: no I/O.
// Each set's weights sum to 100.
func phaseSetFor(t ActivityType, v TestingVariant) []PhaseCompletion {
	switch t {
	case ActivityTypeFrontend:
		// §1b — same 5-phase shape and weights as Service; role-swap does not change ids/weights.
		return []PhaseCompletion{
			{Phase: MethodPhaseUXRequirements, Weight: 15},
			{Phase: MethodPhaseUIDesign, Weight: 20},
			{Phase: MethodPhaseTestPlan, Weight: 10},
			{Phase: MethodPhaseConstruction, Weight: 40},
			{Phase: MethodPhaseIntegration, Weight: 15},
		}
	case ActivityTypeTesting:
		return phaseSetForTestingVariant(v)
	case ActivityTypeDeployment:
		// §1d — no DetailedDesign/contract; 3 phases; weights 25+50+25=100.
		return []PhaseCompletion{
			{Phase: MethodPhaseProvisioningSpec, Weight: 25},
			{Phase: MethodPhaseConstruction, Weight: 50},
			{Phase: MethodPhaseConvergenceVerification, Weight: 25},
		}
	case ActivityTypeDocumentation:
		// §1e — 3 phases; weights 20+60+20=100.
		return []PhaseCompletion{
			{Phase: MethodPhaseDocOutline, Weight: 20},
			{Phase: MethodPhaseConstruction, Weight: 60},
			{Phase: MethodPhaseDocReview, Weight: 20},
		}
	default: // ActivityTypeService (§1a) — 5 phases; weights 15+20+10+40+15=100.
		return []PhaseCompletion{
			{Phase: MethodPhaseRequirements, Weight: 15},
			{Phase: MethodPhaseDetailedDesign, Weight: 20},
			{Phase: MethodPhaseTestPlan, Weight: 10},
			{Phase: MethodPhaseConstruction, Weight: 40},
			{Phase: MethodPhaseIntegration, Weight: 15},
		}
	}
}

// phaseSetForTestingVariant returns the phase set for a specific N-* testing
// activity variant (v3 design §1c + testing-lifecycle-research.md).
func phaseSetForTestingVariant(v TestingVariant) []PhaseCompletion {
	switch v {
	case TestVariantHarness:
		// N-STH: Harness Design 15 → Harness Construction 50 → Coverage 20 → Harness Review 15 = 100
		return []PhaseCompletion{
			{Phase: MethodPhaseHarnessDesign, Weight: 15},
			{Phase: MethodPhaseHarnessConstruction, Weight: 50},
			{Phase: MethodPhaseCoverage, Weight: 20},
			{Phase: MethodPhaseHarnessReview, Weight: 15},
		}
	case TestVariantPerf:
		// N-PERF: Perf Scenario Design 25 → Rig Construction 50 → Rig Review 25 = 100
		return []PhaseCompletion{
			{Phase: MethodPhasePerfScenarioDesign, Weight: 25},
			{Phase: MethodPhaseRigConstruction, Weight: 50},
			{Phase: MethodPhaseRigReview, Weight: 25},
		}
	case TestVariantSystemTest:
		// N-IT: Smoke 10 → Use-Case Execution 45 → Regression 25 → Defect Resolution 15 → Sign-off 5 = 100
		return []PhaseCompletion{
			{Phase: MethodPhaseSmokePass, Weight: 10},
			{Phase: MethodPhaseUseCaseExecution, Weight: 45},
			{Phase: MethodPhaseRegressionSuite, Weight: 25},
			{Phase: MethodPhaseDefectResolution, Weight: 15},
			{Phase: MethodPhaseSignOff, Weight: 5},
		}
	case TestVariantQAProcess:
		// N-QA: Gate Definition 40 → Process Audit 60 = 100
		return []PhaseCompletion{
			{Phase: MethodPhaseGateDefinition, Weight: 40},
			{Phase: MethodPhaseProcessAudit, Weight: 60},
		}
	default: // TestVariantPlan (N-STP): Use-Case Trace 20 → Plan Authoring 45 → Plan Review 35 = 100
		return []PhaseCompletion{
			{Phase: MethodPhaseUseCaseTrace, Weight: 20},
			{Phase: MethodPhasePlanAuthoring, Weight: 45},
			{Phase: MethodPhasePlanReview, Weight: 35},
		}
	}
}

// CoarsePhaseFor is the stored-phase-aware compute-at-read entry point: a stored
// terminal-FAILURE phase (ActivityConstructionFailed) is STICKY and short-circuits —
// it is never recomputed back to Running/Done from the Phases slice (a late
// phase-completion record after a RecordActivityFailed must not resurrect the
// activity). Otherwise it falls through to CoarsePhase over the Phases slice.
func CoarsePhaseFor(stored ActivityConstructionPhase, phases []PhaseCompletion) ActivityConstructionPhase {
	if stored == ActivityConstructionFailed {
		return ActivityConstructionFailed
	}
	return CoarsePhase(phases)
}

// CoarseBuildStatusFor is the stored-status-aware compute-at-read entry point: a
// stored terminal-FAILURE build status (BuildFailed) is STICKY and short-circuits —
// it is never recomputed back to in-construction/in-review/integrated. Otherwise it
// falls through to CoarseBuildStatus over the Phases slice.
func CoarseBuildStatusFor(stored ActivityBuildStatus, phases []PhaseCompletion, current ActivityMethodPhase) ActivityBuildStatus {
	if stored == BuildFailed {
		return BuildFailed
	}
	return CoarseBuildStatus(phases, current)
}

// CoarsePhase derives the coarse ActivityConstructionPhase from the Phases slice
// (compute-at-read; kept for back-compat). Empty/nil phases → NotStarted.
// A stored terminal-failure phase is preserved by callers via CoarsePhaseFor /
// the applyPhaseCompletion guard, NOT here (this derives purely from Phases).
func CoarsePhase(phases []PhaseCompletion) ActivityConstructionPhase {
	if len(phases) == 0 {
		return ActivityConstructionNotStarted
	}
	allDone := true
	anyDone := false
	for _, p := range phases {
		if p.Completed {
			anyDone = true
		} else {
			allDone = false
		}
	}
	if allDone {
		return ActivityConstructionDone
	}
	if anyDone {
		return ActivityConstructionRunning
	}
	return ActivityConstructionNotStarted
}

// CoarseBuildStatus derives the ActivityBuildStatus from the phase set and current
// phase (compute-at-read; kept for back-compat). Rules: Integration phase done →
// BuildIntegrated; Construction phase done but Integration not → BuildInReview;
// otherwise → BuildInConstruction.
// The `current` parameter is reserved for future use (fine-grained phase display)
// and is currently ignored; coarse status is derived solely from Phases completion.
func CoarseBuildStatus(phases []PhaseCompletion, current ActivityMethodPhase) ActivityBuildStatus {
	constructionDone := false
	integrationDone := false
	for _, p := range phases {
		if p.Phase == MethodPhaseConstruction && p.Completed {
			constructionDone = true
		}
		if p.Phase == MethodPhaseIntegration && p.Completed {
			integrationDone = true
		}
	}
	if integrationDone {
		return BuildIntegrated
	}
	if constructionDone {
		return BuildInReview
	}
	return BuildInConstruction
}

// ActivityType is the canonical persisted activity-type axis (what kind of thing
// is built). Replaces the 3-value ActivityKind; used to derive the phase set a
// C-* activity walks (v3 design §1 tables). SEEDED by the bootstrap generator.
// Existing project.json Kind values 0/1/2 decode verbatim (Service/Frontend/Testing);
// 3/4 (Deployment/Documentation) are additive with no data migration needed.
type ActivityType int

const (
	// ActivityTypeService — a Manager/Engine/ResourceAccess/Client component build.
	ActivityTypeService ActivityType = iota
	// ActivityTypeFrontend — a SPA / web UI surface build.
	ActivityTypeFrontend
	// ActivityTypeTesting — a system-test / CI activity (variant selected by TestingVariant).
	ActivityTypeTesting
	// ActivityTypeDeployment — a devops / provisioning activity (R-* prefix, coding=false).
	ActivityTypeDeployment
	// ActivityTypeDocumentation — a tech-writing / ADR / runbook activity (N-ADR etc.).
	ActivityTypeDocumentation
)

// String returns the canonical wire name.
func (t ActivityType) String() string {
	switch t {
	case ActivityTypeFrontend:
		return "frontend"
	case ActivityTypeTesting:
		return "testing"
	case ActivityTypeDeployment:
		return "deployment"
	case ActivityTypeDocumentation:
		return "documentation"
	default:
		return "service"
	}
}

// ActivityKind is a type alias for ActivityType kept for JSON back-compat.
// Existing project.json entries seeded with Kind use the same integer encoding;
// the legacy 3 values (0=Service/1=Frontend/2=Testing) decode correctly through
// ActivityType. New code should use ActivityType; ActivityKind remains valid as
// a field type so no renaming is required at call sites.
type ActivityKind = ActivityType

// ActivityKindService / ActivityKindFrontend / ActivityKindTesting are preserved
// as aliases to the ActivityType constants so existing code referencing the old
// three-value names continues to compile without modification.
const (
	ActivityKindService  = ActivityTypeService
	ActivityKindFrontend = ActivityTypeFrontend
	ActivityKindTesting  = ActivityTypeTesting
)

// TestingVariant discriminates the five N-* testing activity sub-types. Only
// meaningful when ActivityType == ActivityTypeTesting. Variant is chosen from the
// activity name prefix (N-STP → Plan, N-STH → Harness, N-PERF → Perf,
// N-IT → SystemTest, N-QA → QAProcess).
type TestingVariant int

const (
	TestVariantPlan       TestingVariant = iota // N-STP: system test plan
	TestVariantHarness                          // N-STH: test harness construction
	TestVariantPerf                             // N-PERF: performance rig
	TestVariantSystemTest                       // N-IT: system test execution (terminal/critical)
	TestVariantQAProcess                        // N-QA: QA process definition
)

// String returns the canonical wire name.
func (v TestingVariant) String() string {
	switch v {
	case TestVariantHarness:
		return "harness"
	case TestVariantPerf:
		return "perf"
	case TestVariantSystemTest:
		return "systemTest"
	case TestVariantQAProcess:
		return "qaProcess"
	default:
		return "plan"
	}
}

// ActivityMethodPhase is one App-A internal phase within a construction activity.
// It is a canonical lowercase phase-id string (not an ordinal enum). The ordered
// phase SET for a given ActivityType is defined by phaseSetFor (Task 2); this file
// only declares the type and all known phase-id constants.
//
// Using a string type (rather than int) means the JSON wire encoding is the
// constant value itself — no MarshalJSON/UnmarshalJSON boilerplate needed.
type ActivityMethodPhase string

// String returns the phase id (the underlying string value).
func (p ActivityMethodPhase) String() string { return string(p) }

// Service / shared phase ids (v3 design §1a and §1b).
// NOTE: the "Phase" prefix is shared with the project-lifecycle Phase type
// (artifactmodel.go). To avoid name collision, these constants use the
// "MethodPhase" prefix.
const (
	MethodPhaseRequirements   ActivityMethodPhase = "requirements"    // SRS / UX requirements / provisioning spec / doc outline
	MethodPhaseDetailedDesign ActivityMethodPhase = "detailed_design" // service contract (Service only); maps to DD cast
	MethodPhaseTestPlan       ActivityMethodPhase = "test_plan"       // test plan slice (Service/Frontend only)
	MethodPhaseConstruction   ActivityMethodPhase = "construction"    // code / manifest / harness / doc authoring
	MethodPhaseIntegration    ActivityMethodPhase = "integration"     // integration + convergence verification
)

// Frontend-specific phase ids (v3 design §1b — replaces MethodPhaseRequirements
// with UX requirements and adds UI Design before test plan).
const (
	MethodPhaseUXRequirements ActivityMethodPhase = "ux_requirements" // UX requirements (frontend variant of requirements)
	MethodPhaseUIDesign       ActivityMethodPhase = "ui_design"       // UI design artifact + ux-reviewer gate
)

// Deployment-specific phase ids (v3 design §1d — provisioning has no DD/contract phase).
const (
	MethodPhaseProvisioningSpec        ActivityMethodPhase = "provisioning_spec"        // R-* spec before manifest construction
	MethodPhaseConvergenceVerification ActivityMethodPhase = "convergence_verification" // post-apply convergence check
)

// Documentation-specific phase ids (v3 design §1e).
const (
	MethodPhaseDocOutline ActivityMethodPhase = "doc_outline" // doc outline artifact (tech-writer + architect gate)
	MethodPhaseDocReview  ActivityMethodPhase = "doc_review"  // final doc review pass
)

// Testing-variant phase ids (v3 design §1c + testing-lifecycle-research.md).
// These are DEDICATED constants for the five N-* testing activity sub-types;
// they do NOT reuse the service/frontend phase ids so each variant's phase set
// is unambiguous on the wire and in the UI.

// N-STP (Test Plan) phase ids.
const (
	MethodPhaseUseCaseTrace  ActivityMethodPhase = "use_case_trace" // test-engineer traces every core use case for failure modes
	MethodPhasePlanAuthoring ActivityMethodPhase = "plan_authoring" // test-engineer authors the full test plan entries
	MethodPhasePlanReview    ActivityMethodPhase = "plan_review"    // system-architect + PM + qa-engineer all-pass gate
)

// N-STH (Test Harness) phase ids.
const (
	MethodPhaseHarnessDesign       ActivityMethodPhase = "harness_design"       // transport choices + module structure
	MethodPhaseHarnessConstruction ActivityMethodPhase = "harness_construction" // harness code; connects + executes ≥1 use case
	MethodPhaseCoverage            ActivityMethodPhase = "coverage"             // fault injection + coverage map against test plan
	MethodPhaseHarnessReview       ActivityMethodPhase = "harness_review"       // system-architect + qa-engineer pass
)

// N-PERF (Performance Test Rig) phase ids.
const (
	MethodPhasePerfScenarioDesign ActivityMethodPhase = "perf_scenario_design" // latency+throughput scenarios + targets
	MethodPhaseRigConstruction    ActivityMethodPhase = "rig_construction"     // rig executes under load; baseline captured
	MethodPhaseRigReview          ActivityMethodPhase = "rig_review"           // system-architect + qa-engineer pass
)

// N-IT (System Testing, terminal) phase ids.
const (
	MethodPhaseSmokePass        ActivityMethodPhase = "smoke_pass"         // system boots; harness connects
	MethodPhaseUseCaseExecution ActivityMethodPhase = "use_case_execution" // every use case exercised end-to-end
	MethodPhaseRegressionSuite  ActivityMethodPhase = "regression_suite"   // developer-owned N-RTH regression suite clean
	MethodPhaseDefectResolution ActivityMethodPhase = "defect_resolution"  // all P0/P1 defects closed + re-run pass
	MethodPhaseSignOff          ActivityMethodPhase = "sign_off"           // system-architect + PM binary pass
)

// N-QA (QA Process Setup + Audit) phase ids.
const (
	MethodPhaseGateDefinition ActivityMethodPhase = "gate_definition" // binary exit criteria + defect taxonomy defined
	MethodPhaseProcessAudit   ActivityMethodPhase = "process_audit"   // qa-engineer + architect: all gates run, confirmed
)

// ActivityBuildStatus is the finer build-status lens (ux-mock parity) for activities
// that have a corpus presence. Coarser eligible/blocked/not-started are DERIVED in the
// webApp from the network + done-set and are not seeded here.
type ActivityBuildStatus int

const (
	// BuildInConstruction — a construction log exists, work in progress (zero value).
	BuildInConstruction ActivityBuildStatus = iota
	// BuildInReview — a construction log exists without a passing review.
	BuildInReview
	// BuildIntegrated — construction log + a passing review exist.
	BuildIntegrated
	// BuildFailed — the build reached a terminal FAILURE (paired with
	// ActivityConstructionFailed). The work did not integrate; the node is failed,
	// not in-construction/in-review/integrated.
	BuildFailed
)

// String returns the canonical wire name (matches the ux-mock BuildStatus union).
func (s ActivityBuildStatus) String() string {
	switch s {
	case BuildInReview:
		return "in-review"
	case BuildIntegrated:
		return "integrated"
	case BuildFailed:
		return "failed"
	default:
		return "in-construction"
	}
}

// ProducedArtifact is one artifact a construction activity produced (a frozen service
// contract, the built code). SEEDED from the corpus (a contract file / a construction
// log). Mirrors the ux-mock ActivityArtifact card fields.
type ProducedArtifact struct {
	Kind     string // "service-contract" | "code"
	Title    string
	Source   string // corpus-relative path, e.g. "implementation/contracts/webClient.md"
	Produced bool
	Note     string
}

// ConstructionProgress holds the project-level construction tracking framing scalars
// (ux-mock CONSTRUCTION_SUMMARY subset). Seeded; EV is derived, not stored.
type ConstructionProgress struct {
	Week           int
	TotalWeeks     int
	HandOffModel   string
	SupervisionCap int
}
