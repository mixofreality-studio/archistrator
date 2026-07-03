package projectstate

import "time"

// phasearchitects.go holds the typed named artifact and testing-state records
// owned by the projectstate RA (feedback_method_models_owned_by_ra). All records
// are plain Go structs with json tags; they live in project.json under
// .phaseArtifacts and .testingState respectively.

// --- Phase artifact records ---

// SRSRecord is the Requirements phase artifact for a service or deployment activity.
type SRSRecord struct {
	Component  string     `json:"component"`
	Content    string     `json:"content"`
	AuthoredAt *time.Time `json:"authoredAt,omitempty"`
}

// TestPlanRecord is the TestPlan phase artifact (per-service/frontend slice).
// Author per Correction 1: the constructing developer (junior under senior hand-off),
// NOT the test-engineer. System-level test activities use TestingState.SystemTestPlan.
type TestPlanRecord struct {
	Component  string     `json:"component"`
	Content    string     `json:"content"`
	AuthoredAt *time.Time `json:"authoredAt,omitempty"`
}

// IntegrationNoteRecord is the Integration phase artifact produced when the
// senior-developer integrates the component and merges the integration PR.
type IntegrationNoteRecord struct {
	Component  string     `json:"component"`
	Content    string     `json:"content"`
	AuthoredAt *time.Time `json:"authoredAt,omitempty"`
}

// UXRequirementsRecord is the Requirements phase artifact for frontend activities.
type UXRequirementsRecord struct {
	Surface    string     `json:"surface"`
	Content    string     `json:"content"`
	AuthoredAt *time.Time `json:"authoredAt,omitempty"`
}

// UIDesignRecord is the DetailedDesign phase artifact for frontend activities
// (UI designs, wireframes, component specs). Review: founder + ux-reviewer + PM + architect.
type UIDesignRecord struct {
	Surface    string     `json:"surface"`
	Content    string     `json:"content"`
	AuthoredAt *time.Time `json:"authoredAt,omitempty"`
}

// ProvisioningSpecRecord is the Requirements phase artifact for deployment activities.
type ProvisioningSpecRecord struct {
	Resource   string     `json:"resource"`
	Content    string     `json:"content"`
	AuthoredAt *time.Time `json:"authoredAt,omitempty"`
}

// DeployNoteRecord is the Integration phase artifact for deployment activities
// (convergence verification output).
type DeployNoteRecord struct {
	Resource   string     `json:"resource"`
	Content    string     `json:"content"`
	AuthoredAt *time.Time `json:"authoredAt,omitempty"`
}

// DocOutlineRecord is the Requirements phase artifact for documentation activities.
type DocOutlineRecord struct {
	Doc        string     `json:"doc"`
	Content    string     `json:"content"`
	AuthoredAt *time.Time `json:"authoredAt,omitempty"`
}

// DocNoteRecord is the Integration phase artifact for documentation activities
// (review completion note).
type DocNoteRecord struct {
	Doc        string     `json:"doc"`
	Content    string     `json:"content"`
	AuthoredAt *time.Time `json:"authoredAt,omitempty"`
}

// PhaseArtifacts holds all phase-scoped artifacts produced during Phase-3 construction.
// Keyed by component/surface/resource/doc name (the same key used in ServiceContracts).
// Additive: nil until the first RecordPhaseArtifactProduced call.
type PhaseArtifacts struct {
	SRS              map[string]SRSRecord              `json:"srs,omitempty"`
	TestPlan         map[string]TestPlanRecord         `json:"testPlan,omitempty"`
	IntegrationNote  map[string]IntegrationNoteRecord  `json:"integrationNote,omitempty"`
	UXRequirements   map[string]UXRequirementsRecord   `json:"uxRequirements,omitempty"`
	UIDesign         map[string]UIDesignRecord         `json:"uiDesign,omitempty"`
	ProvisioningSpec map[string]ProvisioningSpecRecord `json:"provisioningSpec,omitempty"`
	DeployNote       map[string]DeployNoteRecord       `json:"deployNote,omitempty"`
	DocOutline       map[string]DocOutlineRecord       `json:"docOutline,omitempty"`
	DocNote          map[string]DocNoteRecord          `json:"docNote,omitempty"`
}

// --- Testing state records (§1c / design §2.3) ---

// QualityGate is one human-escalation gate defined by the N-QA activity (§4).
// When the construction Manager encounters a gate matching the current activity+phase,
// it consults interventionEngine: Before mode pauses before dispatch; After mode
// pauses after merge; OnReviewFail forces escalate on any review failure.
type QualityGate struct {
	ActivityType string `json:"activityType"` // e.g. "C-PE" or ActivityType.String()
	Phase        string `json:"phase"`        // ActivityMethodPhase.String()
	When         string `json:"when"`         // "before" | "after" | "onReviewFail"
	Mode         string `json:"mode"`         // "escalate" | "takeover"
}

// DefectRecord is one defect filed during system testing (N-IT / §1c).
type DefectRecord struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Severity   string     `json:"severity"` // "critical" | "high" | "medium" | "low"
	FiledAt    *time.Time `json:"filedAt,omitempty"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
	Note       string     `json:"note,omitempty"`
}

// TestRun is one system-test execution record (N-IT / §1c).
type TestRun struct {
	ID        string     `json:"id"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Passed    int        `json:"passed"`
	Failed    int        `json:"failed"`
	Note      string     `json:"note,omitempty"`
}

// TestArg is one concrete input argument to a step's operation call: the param
// name, a concrete VALUE (JSON-encoded text, so the N-STH harness generator can emit
// runnable code), and an optional schemaRef naming the contract param's type.
type TestArg struct {
	Name      string `json:"name"`
	Value     string `json:"value"`               // concrete value as JSON/text, e.g. "\"approve\""
	SchemaRef string `json:"schemaRef,omitempty"` // contract param type name ($def), optional
}

// TestExpect is the expected outcome of a step: either an expected result value
// (Result, JSON/text) OR an expected error (ErrorExpected + ErrorCode). A negative
// case asserts the system produces the specific failure; a happy step asserts the
// result. Per Righting Software App A: cover "every parameter, condition, and
// error-handling path".
type TestExpect struct {
	Result        string `json:"result,omitempty"`    // expected result value/shape (empty when error-expected)
	ErrorExpected bool   `json:"errorExpected"`       // true → the call is expected to fail
	ErrorCode     string `json:"errorCode,omitempty"` // expected error code / type
}

// TestStep is one black-box step in a system-test case: a single manager operation
// call with its concrete inputs and expected outcome. It is TRANSPORT-AGNOSTIC — it
// names the {component, operation} (the manager method), NOT an HTTP route, because
// the REST/MCP/(future) gRPC clients are all generated bindings of the same manager
// operation. The N-STH harness generator turns these steps into per-transport code.
type TestStep struct {
	Seq       int        `json:"seq"`                 // 1-based order within the case
	Component string     `json:"component"`           // manager/component that owns the operation
	Operation string     `json:"operation"`           // manager method name (e.g. "startSystemDesign")
	Status    string     `json:"status,omitempty"`    // last-run result: "" | "red" (failing) | "green" (passing)
	Inputs    []TestArg  `json:"inputs,omitempty"`    // concrete input arguments
	Expect    TestExpect `json:"expect"`              // expected result or expected error
	Assertion string     `json:"assertion,omitempty"` // human-readable assertion
}

// TestCase is one falsification attempt within a scenario: an ordered sequence of
// manager-operation calls asserting a specific expected outcome — a happy path, a
// negative (error-path) case, or a boundary case. Per Righting Software ch.14 the
// value of the plan is the adversarial coverage ("all the ways to break the system
// and prove it does not work"), so negative/boundary cases are first-class.
type TestCase struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // "happy" | "negative" | "boundary"
	Title string `json:"title"`
	// Proves is "what this proves" — the failure mode this case is designed to expose.
	Proves          string     `json:"proves,omitempty"`
	ExpectedOutcome string     `json:"expectedOutcome,omitempty"` // overall success OR the specific expected failure
	Steps           []TestStep `json:"steps,omitempty"`
}

// TestScenario is one black-box system-test scenario: a core use case and the set of
// test cases (happy + adversarial) that prove it holds — or catch where it breaks —
// end-to-end, black-box at the client surface.
type TestScenario struct {
	ID      string `json:"id"`
	UseCase string `json:"useCase"` // the core use case this scenario traces to
	Title   string `json:"title"`
	// Description is the "what this proves and why it matters" — the failure modes
	// the scenario is designed to expose (system-level QC per Righting Software
	// ch.14: prove the integrated system fails, don't unit-test in isolation).
	Description string     `json:"description,omitempty"`
	Cases       []TestCase `json:"cases,omitempty"`
}

// SystemTestPlan is the output of the N-STP activity (§1c TestVariantPlan).
type SystemTestPlan struct {
	UseCaseIndex []string       `json:"useCaseIndex,omitempty"` // traced UC ids
	Entries      []string       `json:"entries,omitempty"`      // plan entry descriptions
	Scenarios    []TestScenario `json:"scenarios,omitempty"`    // black-box operation sequences
	Status       string         `json:"status,omitempty"`       // "" | "approved"
	ApprovedAt   *time.Time     `json:"approvedAt,omitempty"`
}

// HarnessModule is the output of the N-STH activity (§1c TestVariantHarness).
type HarnessModule struct {
	RepoRef    string     `json:"repoRef,omitempty"` // corpus path / PR ref
	Status     string     `json:"status,omitempty"`  // "" | "approved"
	ApprovedAt *time.Time `json:"approvedAt,omitempty"`
}

// PerfHarness is the output of the N-PERF activity (§1c TestVariantPerf).
type PerfHarness struct {
	RepoRef    string     `json:"repoRef,omitempty"`
	Status     string     `json:"status,omitempty"`
	ApprovedAt *time.Time `json:"approvedAt,omitempty"`
}

// TestingState holds the project-level testing artifacts produced by N-* activities.
// Additive: nil until the first testing activity produces output.
type TestingState struct {
	SystemTestPlan     *SystemTestPlan `json:"systemTestPlan,omitempty"`
	HarnessModule      *HarnessModule  `json:"harnessModule,omitempty"`
	PerfHarness        *PerfHarness    `json:"perfHarness,omitempty"`
	QualityGates       []QualityGate   `json:"qualityGates,omitempty"`
	QualityAuditReport string          `json:"qualityAuditReport,omitempty"`
	TestRuns           []TestRun       `json:"testRuns,omitempty"`
	Defects            []DefectRecord  `json:"defects,omitempty"`
}
