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

// ActivityKind is the construction-activity kind lens (ux-mock parity): the three
// life-cycle shapes a construction activity can take. SEEDED by the bootstrap
// generator from the component layer; not a runtime-pump fact.
type ActivityKind int

const (
	// ActivityKindService — a Manager/Engine/ResourceAccess/Client component build.
	ActivityKindService ActivityKind = iota
	// ActivityKindFrontend — a SPA / web UI surface build.
	ActivityKindFrontend
	// ActivityKindTesting — a system-test / CI activity.
	ActivityKindTesting
)

// String returns the canonical wire name (matches the ux-mock ActivityKind union).
func (k ActivityKind) String() string {
	switch k {
	case ActivityKindFrontend:
		return "frontend"
	case ActivityKindTesting:
		return "testing"
	default:
		return "service"
	}
}

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
