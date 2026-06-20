package projectstate

// Phase-2 estimation INPUT value types (projectStateAccess.md §3.6; projectDesignManager.md §3).
//
// ProjectOption is the value snapshot the projectDesignManager ASSEMBLES from the
// committed Phase-2 head-state slots (PlanningAssumptions + ActivityList + Network +
// the per-option Solution) and feeds BY VALUE to the three estimate Engines
// (estimationEngine, operationEstimationEngine, settlementEngine). It is NOT an
// ArtifactModel and is NOT a stored slot — it never appears in the Project
// aggregate. The Engines read it; they never re-fetch it (Engines do no I/O).
//
// These value types are CANONICAL HERE (the Phase-2 project models owned by
// projectStateAccess) so the downward Engine import (engine → projectstate, the
// same edge artifactValidationEngine already uses) carries them without an upward
// dependency. The Engine OUTPUT value objects (ConstructionEstimate / OperationForecast
// / Projection) live in their owning Engine packages.

// Money is an exact integer-minor-units amount plus an ISO-4217 currency. NEVER a
// float (settlementEngine.md §3). Signed: a positive net is a payout, a negative
// net is a shortfall charge. (A package-local money type for the Phase-2 estimation
// path; the cross-component canonical-Money consolidation into framework-go is a
// noted follow-up, out of scope for C-MPD.)
type Money struct {
	MinorUnits int64  `json:"minorUnits"` // signed minor units, e.g. 1299 == 12.99
	Currency   string `json:"currency"`   // ISO-4217, e.g. "USD"
}

// OptionID identifies one assembled ProjectOption within an SDP review.
type OptionID string

// InfrastructureKind is the opaque discriminator the operationEstimationEngine
// pivots on (operationEstimationEngine.md §3). The launch infrastructure is
// Go + Temporal + Postgres; future kinds are additive.
type InfrastructureKind int

const (
	InfrastructureKindUnknown InfrastructureKind = iota
	// InfrastructureKindGoTemporalPostgres is the launch infrastructure.
	InfrastructureKindGoTemporalPostgres
)

// UsageAssumption is the customer's DECLARED expectation of end-user load, fed to
// operationEstimationEngine.estimateForOption for the operation-side forecast
// (operationEstimationEngine.md §3).
type UsageAssumption struct {
	ExpectedDailyActiveUsers int     `json:"expectedDailyActiveUsers"`
	RequestsPerMinute        float64 `json:"requestsPerMinute"`
	AvgPayloadBytes          int     `json:"avgPayloadBytes"`
}

// RevenueShareKind is the closed set of aiarch revenue-share regimes
// (settlementEngine.md §3). Launch is a flat 10% cut.
type RevenueShareKind int

const (
	RevenueShareUnknown RevenueShareKind = iota
	RevenueShareLaunchFlat10
	RevenueShareNegotiatedRate
)

// ComputeCostKind is the closed set of compute pass-through pricing regimes
// (settlementEngine.md §3).
type ComputeCostKind int

const (
	ComputeCostUnknown ComputeCostKind = iota
	ComputeCostFlatMarkup
	ComputeCostTieredFloors
)

// ScheduleKind is the settlement cadence (settlementEngine.md §3).
type ScheduleKind int

const (
	ScheduleUnknown ScheduleKind = iota
	ScheduleMonthly
	ScheduleWeekly
	ScheduleDaily
)

// SettlementTerms is the customer's settlement-terms snapshot carried BY VALUE on
// the option (settlementEngine.md §3; operationEstimationEngine OQ-2/FU-OE-A — the
// option carries the terms). settlementEngine.projectCommitTimeRevenueShareAndComputeCost
// reads only this.
type SettlementTerms struct {
	RevenueShare         RevenueShareKind `json:"revenueShare"`
	RevenueSharePercent  float64          `json:"revenueSharePercent"` // e.g. 10.0 for launch flat 10%
	ComputeCost          ComputeCostKind  `json:"computeCost"`
	ComputeMarkupPercent float64          `json:"computeMarkupPercent"` // markup on metered compute cost
	Schedule             ScheduleKind     `json:"schedule"`
}

// OptionActivity is one activity in an option's CPM network — effort in 5-day
// quanta, its worker class, whether it sits on the critical path, and its
// Fibonacci risk bucket. (Named OptionActivity to avoid colliding with the
// activity-diagram ActivityNode in usecase.go.)
type OptionActivity struct {
	ActivityID     string  `json:"activityId"`
	EffortDays     float64 `json:"effortDays"`
	WorkerClass    string  `json:"workerClass"`
	OnCriticalPath bool    `json:"onCriticalPath"`
	RiskBucket     int     `json:"riskBucket"` // 1,2,3,5,8,13 (Fibonacci) — higher == riskier
}

// ActivityNetwork is the option's activity graph as the Engine needs it: the flat
// activity set with effort, worker class, critical-path membership and risk bucket.
type ActivityNetwork struct {
	Activities []OptionActivity `json:"activities"`
}

// WorkerMix is the option's worker-class build-cost rates (per person-day) plus
// the staffing cap that bounds parallelism.
type WorkerMix struct {
	ClassRates  map[string]Money `json:"classRates"`  // build cost per person-day, by worker class
	StaffingCap int              `json:"staffingCap"` // max concurrent staff (parallelism bound)
}

// ProjectOption is one of the four assembled solution options (normal /
// decompressed-normal / subcritical / compressed). The Manager assembles it from
// the committed Phase-2 slots and feeds it by value to the three Engines.
type ProjectOption struct {
	OptionID            OptionID           `json:"optionId"`
	SolutionKind        ArtifactKind       `json:"solutionKind"` // one of the four KindXxxSolution
	Network             ActivityNetwork    `json:"network"`
	WorkerMix           WorkerMix          `json:"workerMix"`
	CalendarDaysPerWeek float64            `json:"calendarDaysPerWeek"`
	Terms               SettlementTerms    `json:"terms"`
	DeclaredUsage       UsageAssumption    `json:"declaredUsage"`
	InfrastructureKind  InfrastructureKind `json:"infrastructureKind"`
}
