package projectstate

// Phase-2 typed artifact models — the head-state slot models the projectDesignManager
// co-authors in Phase 2 (projectStateAccess.md §3.6, projectDesignManager.md §3).
//
// These are the STORED slot models (each implements ArtifactModel, routed to its
// named slot by Kind()). They are distinct from the transient assembled-option value
// types in estimation.go (ProjectOption et al.) that the Manager feeds the Engines.
//
// Grammar is intentionally lean-but-real: enough to assemble the four ProjectOptions
// (from PlanningAssumptions + ActivityList + Network + the per-option Solution) and
// to join the three Engine outputs into the SDP-review rows. Fields are additive.
//
// Each model keeps the pointer-receiver KindXxx + isArtifactModel() convention used
// by every other model in this package (system.go, models_phase1.go).

// PlanningAssumptions holds the Phase-2 planning assumptions artifact: the resources,
// calendar, infrastructure, declared usage, and settlement terms the project network
// and the SDP-review estimates are built on. (projectStateAccess.md §3.6)
type PlanningAssumptions struct {
	Resources           []string           `json:"resources"`           // named staff/resources available
	CalendarDaysPerWeek float64            `json:"calendarDaysPerWeek"` // working days/week (5 normal, 2 moonlight, …)
	InfrastructureKind  InfrastructureKind `json:"infrastructureKind"`
	DeclaredUsage       UsageAssumption    `json:"declaredUsage"`
	Terms               SettlementTerms    `json:"terms"`
	Notes               string             `json:"notes"`
}

// Kind implements ArtifactModel.
func (p *PlanningAssumptions) Kind() ArtifactKind { return KindPlanningAssumptions }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (p *PlanningAssumptions) isArtifactModel() {}

// ActivityItem is one activity in the activity list — effort in 5-day quanta, its
// worker class, whether it is coding (vs noncoding/integration), and its Fibonacci
// risk bucket.
type ActivityItem struct {
	Name        string  `json:"name"`
	EffortDays  float64 `json:"effortDays"`
	WorkerClass string  `json:"workerClass"`
	Coding      bool    `json:"coding"`
	RiskBucket  int     `json:"riskBucket"` // 1,2,3,5,8,13 (Fibonacci)
	// Title is the human-readable activity description (e.g. "Build Web Client") —
	// additive, omitempty for back-compat with documents that pre-date it. Name stays
	// the network id (the load-bearing dependency/head-state key); Title is display-only.
	Title string `json:"title,omitempty"`
}

// ActivityList holds the Phase-2 activity list artifact — the coding + noncoding
// activities in 5-day quanta. (projectStateAccess.md §3.6)
type ActivityList struct {
	Activities []ActivityItem `json:"activities"`
}

// Kind implements ArtifactModel.
func (a *ActivityList) Kind() ArtifactKind { return KindActivityList }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (a *ActivityList) isArtifactModel() {}

// NetworkDependency declares that one activity depends on a set of predecessors.
type NetworkDependency struct {
	Activity  string   `json:"activity"`
	DependsOn []string `json:"dependsOn"`
}

// Network holds the Phase-2 project network artifact — the activity dependencies and
// the computed critical path (the activity names on it). (projectStateAccess.md §3.6)
//
// AUTHORED vs COMPUTED (2026-06-19, founder gate — move CPM compute server-side):
//   - Dependencies, CriticalPath, Milestones are AUTHORED inputs (stored, co-authored
//     in Phase 2; the SPA must never invent them).
//   - Computed, Summary are the COMPUTE-AT-READ block the projectManager populates by
//     running constructionEstimationEngine.ComputeNetwork over (Dependencies ×
//     ActivityList) on every read. They are `omitempty` so the AUTHORED document on
//     disk never carries them — they exist only on the wire the SPA reads. The web
//     client's former client-side CPM (toNetworkView) is RETIRED in favour of these.
type Network struct {
	// --- AUTHORED inputs (stored on disk) ---
	Dependencies []NetworkDependency `json:"dependencies"`
	CriticalPath []string            `json:"criticalPath"` // activity names on the critical path
	// Milestones are the authored zero-duration event nodes (M0–M5 + N-DOGFOOD): the
	// id/name/public/dependsOn are authored; OnCriticalPath + EventTime are computed at
	// read. omitempty so a network with none round-trips unchanged.
	Milestones []NetworkMilestone `json:"milestones,omitempty"`

	// --- COMPUTED block (compute-at-read; absent on disk, present on the wire) ---
	// Computed is the per-activity CPM result, keyed by activity id. Populated only by
	// the projectManager's compute-at-read pass; nil/absent in the stored document.
	Computed map[string]NetworkNodeCompute `json:"computed,omitempty"`
	// Summary is the project-level CPM roll-up. Populated only at read; nil on disk.
	Summary *NetworkSummary `json:"summary,omitempty"`
}

// NetworkMilestone is one authored zero-duration event node on the project network
// (M0–M5 + N-DOGFOOD). The id/name/public/dependsOn are AUTHORED; OnCriticalPath and
// EventTime are COMPUTED at read (EventTime = max predecessor earliest-finish; a
// milestone with no predecessors has EventTime 0 — the project-start gate). Milestones
// are EXCLUDED from the risk decomposition (they carry no effort and no risk bucket).
type NetworkMilestone struct {
	// --- AUTHORED ---
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Public    bool     `json:"public"`              // a demo-to-management gate vs an internal hurdle
	DependsOn []string `json:"dependsOn,omitempty"` // predecessor activity ids (the fan-in)
	// --- COMPUTED at read ---
	// POINTERS (not bare bool/float64) so they are ABSENT on the authored on-disk document
	// — a stored milestone carries only id/name/public/dependsOn; a bare bool/float64 would
	// persist a misleading onCriticalPath:false / eventTime:0. The compute-at-read pass
	// sets them non-nil, so they are ALWAYS emitted on the wire (even a computed false / 0),
	// which a bare-type omitempty would wrongly drop. This keeps the authored↔computed split
	// faithful on BOTH the disk and the wire.
	OnCriticalPath *bool    `json:"onCriticalPath,omitempty"`
	EventTime      *float64 `json:"eventTime,omitempty"` // sim-days; = max predecessor earliestFinish (0 with no preds)
}

// NetworkNodeCompute is the per-activity CPM result the compute-at-read pass derives
// for one dependency-graph node. It mirrors the figures the retired client-side
// toNetworkView produced, now authoritative and server-computed.
type NetworkNodeCompute struct {
	EarliestStart  float64 `json:"earliestStart"`
	EarliestFinish float64 `json:"earliestFinish"`
	LatestStart    float64 `json:"latestStart"`
	LatestFinish   float64 `json:"latestFinish"`
	TotalFloat     float64 `json:"totalFloat"`
	FreeFloat      float64 `json:"freeFloat"`
	OnCriticalPath bool    `json:"onCriticalPath"`
	NearCritical   bool    `json:"nearCritical"` // off-CP but within the near-critical float band
	Band           string  `json:"band"`         // float-criticality band: critical|red|yellow|green
	Column         int     `json:"column"`       // topological depth (longest-path layer) for the swimlane layout
}

// NetworkSummary is the project-level CPM roll-up the SPA renders above the graph.
type NetworkSummary struct {
	TotalDurationDays         float64 `json:"totalDurationDays"`         // project duration = longest path
	CriticalPathActivityCount int     `json:"criticalPathActivityCount"` // count of on-CP activities (not the CP day-sum)
	CriticalPathDays          float64 `json:"criticalPathDays"`          // = TotalDurationDays (the longest path is the CP length)
	MaxFloat                  float64 `json:"maxFloat"`                  // the loosest slack across all nodes
	NearCriticalCount         int     `json:"nearCriticalCount"`         // off-CP nodes inside the near-critical band
}

// Kind implements ArtifactModel.
func (n *Network) Kind() ArtifactKind { return KindNetwork }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (n *Network) isArtifactModel() {}

// Solution holds one Phase-2 solution-option artifact — the option's defining knobs
// (staffing cap, calendar, worker-class rates, optional schedule buffer). Duration,
// cost, and risk are NOT stored here: they are computed by the estimate Engines from
// the assembled ProjectOption and joined into the SDP review.
//
// The four solution slots (NormalSolution, DecompressedSolution, SubcriticalSolution,
// CompressedSolution) are the SAME struct distinguished by SlotKind, so the generic
// stageArtifactForReview routing works for all four without a type switch.
// (projectStateAccess.md §3.2 §3.6)
type Solution struct {
	SlotKind            ArtifactKind     `json:"slotKind"` // one of the four KindXxxSolution
	StaffingCap         int              `json:"staffingCap"`
	CalendarDaysPerWeek float64          `json:"calendarDaysPerWeek"`
	ClassRates          map[string]Money `json:"classRates"` // build cost per person-day, by worker class
	BufferDays          float64          `json:"bufferDays"` // schedule buffer (decompressed-normal); 0 otherwise
}

// NewSolution constructs a Solution for the given slot kind. slotKind must be one of
// the four KindXxxSolution constants.
func NewSolution(slotKind ArtifactKind) *Solution {
	return &Solution{SlotKind: slotKind}
}

// Kind implements ArtifactModel. Returns SlotKind so the generic
// stageArtifactForReview verb can route all four solution slots without a type
// switch. (projectStateAccess.md §3.6 "kind tag")
func (s *Solution) Kind() ArtifactKind { return s.SlotKind }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (s *Solution) isArtifactModel() {}

// RiskRow is the per-option risk decomposition (criticality + activity risk →
// composite) used in the SDP-review time-risk curve.
type RiskRow struct {
	SolutionKind    ArtifactKind `json:"solutionKind"`
	CriticalityRisk float64      `json:"criticalityRisk"`
	ActivityRisk    float64      `json:"activityRisk"`
	Composite       float64      `json:"composite"`
}

// RiskModel holds the Phase-2 risk model artifact — the per-option criticality +
// activity risk decomposition. (projectStateAccess.md §3.6)
type RiskModel struct {
	Rows []RiskRow `json:"rows"`
}

// Kind implements ArtifactModel.
func (r *RiskModel) Kind() ArtifactKind { return KindRiskModel }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (r *RiskModel) isArtifactModel() {}

// SdpOptionRow is one row of the SDP-review options table — the JOIN of the three
// Engine outputs for one option, flattened to plain values (the Manager joins the
// Engine value objects; this slot model never imports the Engine output types, which
// would be an upward dependency).
type SdpOptionRow struct {
	OptionID             OptionID     `json:"optionId"`
	SolutionKind         ArtifactKind `json:"solutionKind"`
	DurationDays         float64      `json:"durationDays"`         // estimationEngine: construction-side duration
	BuildCost            Money        `json:"buildCost"`            // estimationEngine: construction-side build cost
	CompositeRisk        float64      `json:"compositeRisk"`        // estimationEngine: composite construction risk
	ProjectedMonthlyCost Money        `json:"projectedMonthlyCost"` // operationEstimationEngine: operation cost at declared load
	ExpectedPerCycleNet  Money        `json:"expectedPerCycleNet"`  // operationEstimationEngine: payout(+)/shortfall(-) forecast
	RevenueSharePercent  float64      `json:"revenueSharePercent"`  // settlementEngine: projected revenue-share regime rate
}

// SdpReview holds the Phase-2 SDP review artifact — the options table (the four joined
// rows) plus the architect's recommendation. This is the model surfaced at the
// option-commitment gate. (projectStateAccess.md §3.6, projectDesignManager.md §6.3)
type SdpReview struct {
	Options        []SdpOptionRow `json:"options"`
	Recommendation OptionID       `json:"recommendation"` // the option the assembly recommends
	Rationale      string         `json:"rationale"`
}

// Kind implements ArtifactModel.
func (s *SdpReview) Kind() ArtifactKind { return KindSdpReview }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (s *SdpReview) isArtifactModel() {}
