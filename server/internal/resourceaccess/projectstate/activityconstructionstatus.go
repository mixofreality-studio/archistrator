package projectstate

import "time"

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
)

// String returns the canonical wire name for the construction phase (used in JSON
// and log output). Mirrors CICheckState.String() and ActivityOutcome.String().
func (p ActivityConstructionPhase) String() string {
	switch p {
	case ActivityConstructionRunning:
		return "running"
	case ActivityConstructionDone:
		return "done"
	default:
		return "notStarted"
	}
}

// ActivityConstructionStatus is the per-activity construction head-state record.
// One per construction-network activity, keyed by ActivityID in
// Project.ActivityConstruction. Mirrors ActivityGitStatus in shape and posture:
// additive, populated only in Phase 3, server-resolved timestamps.
type ActivityConstructionStatus struct {
	// ActivityID is the network activity id — the map key (NAME-as-identity).
	ActivityID string
	// Phase is the coarse construction lifecycle for this activity.
	Phase ActivityConstructionPhase
	// StartedAt is the server-resolved timestamp when RecordActivityStarted committed.
	// nil until the activity has been started.
	StartedAt *time.Time
	// CompletedAt is the server-resolved timestamp when RecordActivityCompleted committed.
	// nil until the activity has completed.
	CompletedAt *time.Time
	// Kind is the seeded construction-activity kind (service/frontend/testing).
	Kind ActivityKind
	// BuildStatus is the seeded finer build-status lens.
	BuildStatus ActivityBuildStatus
	// Produced is the seeded list of artifacts this activity produced (contracts/code).
	Produced []ProducedArtifact
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
)

// String returns the canonical wire name (matches the ux-mock BuildStatus union).
func (s ActivityBuildStatus) String() string {
	switch s {
	case BuildInReview:
		return "in-review"
	case BuildIntegrated:
		return "integrated"
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
