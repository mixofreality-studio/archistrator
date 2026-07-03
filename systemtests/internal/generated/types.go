// Package generated holds the MECHANICALLY GENERATED wire-test step tables
// derived from the committed System Test Plan
// (.aiarch/state/project.json → .testingState.systemTestPlan). The generator
// lives OUTSIDE this module (server/cmd/gen-systemtests, which decodes
// project.json via the projectstate ResourceAccess) and writes *.gen.go files
// into this directory only — this file is the ONE hand-written exception,
// because it is the stable contract both the generator and the systemtests
// runner (systemtests/usecases) compile against.
//
// Constitution (R1/R3, [[the-method-testing]] §7 + systemtests/constitution):
// every file under this module — including generated ones — must import
// nothing beyond stdlib. StepCase is a plain data record; the *_table.gen.go
// files that populate the Registry below reference ONLY types declared here,
// so they need no imports at all.
package generated

// InputArg is one concrete input argument to a generated step's operation
// call — a 1:1 transcription of projectstate.TestArg from the committed plan
// (Name, a concrete Value carried as JSON/text, and an optional SchemaRef
// naming the contract param's type). Kept as an ordered slice (not
// map[string]string) on StepCase: argument ORDER is part of the plan's
// authored record and map iteration order is not stable, and SchemaRef is
// preserved for a future generator smart enough to type-check inputs against
// the component's published contract.
type InputArg struct {
	Name      string
	Value     string
	SchemaRef string
}

// StepCase is one row in a generated system-test step table: ONE manager-
// operation call from a TestStep in the committed plan, DENORMALIZED with its
// owning TestCase's identity (ScenarioID/UseCase/CaseID/CaseKind/CaseTitle) so
// a []StepCase slice is self-describing — a consumer (the runner, or any
// future tool) never needs a second lookup to know which scenario/use-case/
// case a step belongs to. This mirrors projectstate.TestStep + the case/
// scenario fields of projectstate.TestCase/TestScenario it was folded from.
type StepCase struct {
	// Case identity — repeated verbatim on every step belonging to this case.
	ScenarioID string // e.g. "STP-UC1"
	UseCase    string // core use case id the scenario traces to
	CaseID     string // e.g. "STP-UC1-H1"
	CaseKind   string // "happy" | "negative" | "boundary"
	CaseTitle  string

	// Step identity + call, 1:1 with projectstate.TestStep.
	Seq       int    // 1-based order within the case
	Component string // manager/component owning the operation, e.g. "systemDesignManager"
	Operation string // manager method name, e.g. "RequestArtifactDraft"
	Inputs    []InputArg

	// Expected outcome, 1:1 with projectstate.TestExpect.
	ExpectResult      string // expected result value/shape (empty when error-expected)
	ExpectError       bool   // true -> the call is expected to fail
	ExpectedErrorCode string // expected error code / type

	Assertion string // human-readable assertion carried through for test output
}

// Registry accumulates every generated case's step table, keyed by CaseID
// (e.g. "STP-UC1-H1"). Each stp_*_table.gen.go file populates it from its
// own init(). Consumers (the runner) range this map rather than importing a
// fixed set of file-local var names, so the generator is free to add, rename,
// or remove per-scenario files without the runner tracking filenames.
var Registry = map[string][]StepCase{}

// ScenarioOrder preserves each scenario's case order AS AUTHORED in the
// System Test Plan (map iteration over Registry would scramble it), keyed by
// ScenarioID (e.g. "STP-UC1") to its ordered []CaseID. Later cases in a
// scenario are frequently written as a continuation of state earlier cases in
// the SAME scenario left behind (the plan's authored narrative), so a runner
// that wants to replay a scenario faithfully walks this order, not sorted
// CaseIDs.
var ScenarioOrder = map[string][]string{}

// register is called from each generated file's init(). It panics on a
// duplicate CaseID: two generated files claiming the same case id is a
// generator bug (or a stale hand-edit), not a runtime condition to recover
// from — fail loud, fail at process start.
func register(caseID string, rows []StepCase) {
	if _, dup := Registry[caseID]; dup {
		panic("generated: duplicate case id registered: " + caseID)
	}
	Registry[caseID] = rows
}

// registerScenario is called once per generated file from its init().
func registerScenario(scenarioID string, caseIDsInOrder []string) {
	if _, dup := ScenarioOrder[scenarioID]; dup {
		panic("generated: duplicate scenario id registered: " + scenarioID)
	}
	ScenarioOrder[scenarioID] = caseIDsInOrder
}
