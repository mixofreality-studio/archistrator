package construction

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	projectstatefake "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate/fake"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// ---------------------------------------------------------------------------
// Test helpers (fake Temporal client + constructor shim)
// ---------------------------------------------------------------------------

// fakeTemporalClient captures the last SignalWorkflow call. It embeds
// client.Client so the struct satisfies the interface without implementing
// every method; any unimplemented method panics if reached (none should be
// in these unit tests).
type fakeTemporalClient struct {
	client.Client
	lastWorkflowID string
	lastSignalName string
	lastSignalArg  any
}

func (f *fakeTemporalClient) SignalWorkflow(_ context.Context, workflowID string, _ string, signalName string, arg any) error {
	f.lastWorkflowID = workflowID
	f.lastSignalName = signalName
	f.lastSignalArg = arg
	return nil
}

// newTestConstructionManager wires a fake temporal client into a bare
// constructionManager (all other deps nil — only used for pre-Temporal checks
// and signal dispatch tests).
func newTestConstructionManager(c client.Client) *constructionManager {
	return newConstructionManager(c, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
}

// testCtx returns a minimal fwmanager.Context backed by context.Background.
func testCtx() fwmanager.Context {
	return fwmanager.Context{Context: context.Background()}
}

// These tests cover the façade-boundary pre-condition checks the contract puts on
// the five public ops (constructionManager.md §2/§3.5). They run BEFORE any
// Temporal client call, so they need no cluster and no client — a nil client is
// safe because the checks short-circuit first.

func asConstructionError(t *testing.T, err error) *fwmanager.Error {
	t.Helper()
	var ce *fwmanager.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *constructionError, got %T: %v", err, err)
	}
	return ce
}

// ---- ExecuteNextActivity (op 2.1) ------------------------------------------

func Test_ExecuteNextActivity_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	_, err := m.ExecuteNextActivity(fwmanager.Context{Context: context.Background()}, ProjectID(""), "tick-1")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_ExecuteNextActivity_EmptyTickID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	_, err := m.ExecuteNextActivity(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- RunReplanSweep (op 2.2) ------------------------------------------------

func Test_RunReplanSweep_EmptyTickID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	_, err := m.RunReplanSweep(fwmanager.Context{Context: context.Background()}, nil, "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_RunReplanSweep_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	nilID := ProjectID("")
	_, err := m.RunReplanSweep(fwmanager.Context{Context: context.Background()}, &nilID, "tick-1")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an explicit nil projectId, got %s", got)
	}
}

// ---- PauseProject (op 2.3) --------------------------------------------------

func Test_PauseProject_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	err := m.PauseProject(fwmanager.Context{Context: context.Background()}, ProjectID(""), "reason")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_PauseProject_EmptyReason(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	err := m.PauseProject(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an empty pause reason, got %s", got)
	}
}

// ---- OverrideActivity (op 2.4) ----------------------------------------------

func Test_OverrideActivity_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	err := m.OverrideActivity(fwmanager.Context{Context: context.Background()}, ProjectID(""), "C-1", ActivityOverride{Kind: OverrideRetry})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_OverrideActivity_EmptyActivityID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	err := m.OverrideActivity(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), "", ActivityOverride{Kind: OverrideRetry})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an empty activityId, got %s", got)
	}
}

func Test_OverrideActivity_UnknownOverrideKind(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	err := m.OverrideActivity(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), "C-1", ActivityOverride{Kind: OverrideUnknown})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an unknown override kind, got %s", got)
	}
}

// ---- GetSessionState (op 2.5) -----------------------------------------------

func Test_GetSessionState_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	_, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, ProjectID(""), nil)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_GetSessionState_EmptyActivityID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	empty := ActivityID("")
	_, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), &empty)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an explicit empty activityId, got %s", got)
	}
}

// ---- workflow id derivation -------------------------------------------------

func Test_WorkflowIDDerivation(t *testing.T) {
	pid := ProjectID("11111111-1111-1111-1111-111111111111")

	if got := pumpWorkflowID(pid, "t1"); got != string(pid)+":nextActivity:t1" {
		t.Fatalf("pump id: %q", got)
	}
	if got := constructActivityWorkflowID(pid, "C-9"); got != string(pid)+":C-9" {
		t.Fatalf("child id: %q", got)
	}
	if got := replanSweepWorkflowID(&pid, "t2"); got != string(pid)+":replanSweep:t2" {
		t.Fatalf("sweep id: %q", got)
	}
	if got := replanSweepWorkflowID(nil, "t3"); got != ":all:replanSweep:t3" {
		t.Fatalf("all-sweep id: %q", got)
	}
	if got := pauseTargetWorkflowID(pid); got != string(pid)+":construction" {
		t.Fatalf("pause target id: %q", got)
	}
}

// ---- OverrideKind / activityKind String coverage --------------

func Test_OverrideKind_String(t *testing.T) {
	cases := map[OverrideKind]string{
		OverrideTakeover: "Takeover", OverrideRetry: "Retry",
		OverrideSkip: "Skip", OverrideReassign: "Reassign", OverrideUnknown: "Unknown",
	}
	for k, want := range cases {
		if got := overrideKindName(k); got != want {
			t.Fatalf("overrideKindName(%d) = %q, want %q", int(k), got, want)
		}
	}
}

// ---- SubmitPhaseDecision (op 2.6) -------------------------------------------

func TestSubmitPhaseDecision_SignalsActivityWorkflowWithPhase(t *testing.T) {
	fc := &fakeTemporalClient{}
	m := newTestConstructionManager(fc)
	if err := m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", "detailed_design", PhaseApprove, nil); err != nil {
		t.Fatalf("SubmitPhaseDecision: %v", err)
	}
	if fc.lastWorkflowID != "proj-1:C-Orders" || fc.lastSignalName != signalPhaseDecision {
		t.Fatalf("wfID=%q signal=%q", fc.lastWorkflowID, fc.lastSignalName)
	}
	sig, ok := fc.lastSignalArg.(phaseDecisionSignal)
	if !ok || sig.Phase != "detailed_design" || sig.Decision != PhaseApprove {
		t.Fatalf("payload=%+v", fc.lastSignalArg)
	}
}

func TestSubmitPhaseDecision_SendBackRequiresFeedbackNotes(t *testing.T) {
	m := newTestConstructionManager(&fakeTemporalClient{})
	err := m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", "detailed_design", PhaseSendBack, nil)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for SendBack without feedback, got %s", got)
	}
	err = m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", "detailed_design", PhaseSendBack, &ReviewFeedback{Notes: ""})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for SendBack with empty notes, got %s", got)
	}
}

func TestSubmitPhaseDecision_EmptyProjectID(t *testing.T) {
	m := newTestConstructionManager(nil)
	err := m.SubmitPhaseDecision(testCtx(), "", "C-Orders", "detailed_design", PhaseApprove, nil)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func TestSubmitPhaseDecision_EmptyActivityID(t *testing.T) {
	m := newTestConstructionManager(nil)
	err := m.SubmitPhaseDecision(testCtx(), "proj-1", "", "detailed_design", PhaseApprove, nil)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func TestHydrateConstructionActivity_ServicePhases(t *testing.T) {
	got := hydrateConstructionActivity("C-Orders", projectstate.ActivityItem{Coding: true, EffortDays: 5}, nil)
	want := []projectstate.ActivityMethodPhase{
		projectstate.MethodPhaseRequirements, projectstate.MethodPhaseDetailedDesign,
		projectstate.MethodPhaseTestPlan, projectstate.MethodPhaseConstruction,
		projectstate.MethodPhaseIntegration,
	}
	if len(got.Phases) != len(want) {
		t.Fatalf("phases len = %d, want %d", len(got.Phases), len(want))
	}
	for i := range want {
		if got.Phases[i] != want[i] {
			t.Errorf("phase[%d] = %q, want %q", i, got.Phases[i], want[i])
		}
	}
}

func TestHydrateConstructionActivity_TestingPlanIsThreePhases(t *testing.T) {
	got := hydrateConstructionActivity("N-STP", projectstate.ActivityItem{Coding: true}, nil)
	want := []projectstate.ActivityMethodPhase{
		projectstate.MethodPhaseRequirements, projectstate.MethodPhaseConstruction,
		projectstate.MethodPhaseIntegration,
	}
	if len(got.Phases) != len(want) {
		t.Fatalf("N-STP phases len = %d, want %d", len(got.Phases), len(want))
	}
	for i := range want {
		if got.Phases[i] != want[i] {
			t.Errorf("phase[%d] = %q, want %q", i, got.Phases[i], want[i])
		}
	}
}

func TestDispatchInputsForIncludesCommand(t *testing.T) {
	// A service construction phase -> service-construction command.
	in := dispatchInputsFor(pipelineSpec{
		ActivityID:  "C-BM",
		ComponentID: "billingManager",
		Phase:       "construction",
	})
	if in["command"] != "service-construction" {
		t.Errorf("command = %q, want service-construction", in["command"])
	}
	if in["activity_id"] != "C-BM" || in["component_id"] != "billingManager" {
		t.Errorf("activity/component passthrough wrong: %+v", in)
	}
	if in["phase"] != "construction" {
		t.Errorf("phase = %q, want construction", in["phase"])
	}

	// A testing harness detailed-design phase -> testing-harness-detailed-design.
	in2 := dispatchInputsFor(pipelineSpec{
		ActivityID: "N-STH",
		Phase:      "detailed_design",
	})
	if in2["command"] != "testing-harness-detailed-design" {
		t.Errorf("command = %q, want testing-harness-detailed-design", in2["command"])
	}
}

// constructWorkflowBody reads archistrator's OWN aiarch-construct.yml, located relative
// to this test file (robust to the test's working directory). It is the static workflow
// the construction dispatch (dispatchInputsFor) drives.
func constructWorkflowBody(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// server/internal/manager/construction → repo root is four levels up.
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", ".github", "workflows", "aiarch-construct.yml")
	b, err := os.ReadFile(path) // #nosec G304 -- fixed repo-relative test fixture path
	if err != nil {
		t.Fatalf("read aiarch-construct.yml: %v", err)
	}
	return string(b)
}

// TestConstructWorkflowWiresStateMcp asserts aiarch-construct.yml wires the aiarch-state
// MCP server (the twin of sourcecontrol.TestDesignWorkflowWiresStateMcp): it obtains the
// binary, bakes the construction ambient session context as env on the MCP process, and
// passes --mcp-config to claude-code-action.
func TestConstructWorkflowWiresStateMcp(t *testing.T) {
	body := constructWorkflowBody(t)

	// Obtains the MCP binary — `go install <module>@<pin>` of the published state-MCP
	// (the construct workflow runs from a seated scaffold with no in-repo server source, so
	// it installs the pinned module rather than building ./cmd/aiarch-state-mcp). Mirrors
	// the design twin (sourcecontrol.TestDesignWorkflowWiresStateMcp).
	if !strings.Contains(body, `go install github.com/mixofreality-studio/archistrator/server/cmd/aiarch-state-mcp@"${AIARCH_STATE_MCP_PIN}"`) {
		t.Errorf("construct workflow must `go install` the pinned aiarch-state MCP server; got:\n%s", body)
	}

	// The MCP config bakes the CONSTRUCTION ambient context.
	for _, key := range []string{
		"AIARCH_PROJECT_ID", "AIARCH_JOB_MODE", "AIARCH_COMPONENT_ID",
		"AIARCH_ACTIVITY_ID", "AIARCH_TARGET_BRANCH", "AIARCH_STATE_ROOT",
	} {
		if !strings.Contains(body, key) {
			t.Errorf("MCP config must set %s on the aiarch-state server process", key)
		}
	}

	// Construct job mode (the construction session context is keyed by component/activity,
	// not an artifact kind).
	if !strings.Contains(body, `"AIARCH_JOB_MODE": "construct"`) {
		t.Error("MCP config must set AIARCH_JOB_MODE to construct")
	}

	// --mcp-config wires the server into the Claude CLI.
	if !strings.Contains(body, "--mcp-config") {
		t.Error("claude-code-action must wire the aiarch-state MCP server via --mcp-config")
	}
}

// TestConstructWorkflowKeepsLoadBearingAnchors guards the dispatch contract the MCP wiring
// must not have disturbed: the idempotency run-name anchor and the additive dispatch
// inputs.
func TestConstructWorkflowKeepsLoadBearingAnchors(t *testing.T) {
	body := constructWorkflowBody(t)
	for _, anchor := range []string{
		"run-name: aiarch-cp-${{ inputs.idempotency_token }}",
		"idempotency_token:",
		"activity_id:",
		"component_id:",
		"/${{ inputs.command }} ${{ inputs.component_id }} ${{ inputs.activity_id }}",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("construct workflow lost a load-bearing anchor: %q", anchor)
		}
	}
}

// repoRoot returns the repository root, located relative to this test file.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// server/internal/manager/construction → repo root is four levels up.
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
}

// TestConstructionPromptsUseStateTools asserts the central the-method-project-state
// skill mandates the aiarch-state write tools, and every construction /
// detailed-design command carries the "state changes through the tools" note. This
// is the prompt-side guard that construction agents record state THROUGH the tools,
// not by hand-editing project.json.
func TestConstructionPromptsUseStateTools(t *testing.T) {
	root := repoRoot(t)

	skill := readFileT(t, filepath.Join(root, ".claude", "skills", "the-method-project-state", "SKILL.md"))
	for _, want := range []string{
		"STATE CHANGES GO THROUGH THE `aiarch-state` MCP TOOLS",
		"recordServiceContract",
		"recordPhaseArtifact",
		"recordTestingState",
		"publishDraft",
	} {
		if !strings.Contains(skill, want) {
			t.Errorf("the-method-project-state skill must reference %q", want)
		}
	}

	cmdDir := filepath.Join(root, ".claude", "commands")
	var commands []string
	for _, f := range []string{"deployment", "documentation", "frontend", "service", "testing-harness", "testing-perf", "testing-qa"} {
		commands = append(commands, f+"-detailed-design.md")
	}
	for _, f := range []string{"deployment", "documentation", "frontend", "service", "testing-harness", "testing-perf", "testing-plan", "testing-qa", "testing-systemtest"} {
		commands = append(commands, f+"-construction.md")
	}
	const noteMarker = "State changes go through the `aiarch-state` MCP tools"
	for _, c := range commands {
		body := readFileT(t, filepath.Join(cmdDir, c))
		if !strings.Contains(body, noteMarker) {
			t.Errorf("%s must carry the aiarch-state tool note", c)
		}
	}
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- fixed repo-relative test fixture path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// fakeReviewPolicyTransition is a minimal fake satisfying projectstate.ConstructionTransitionAccess
// for UpdateReviewPolicy tests. It embeds the interface (unimplemented methods panic
// if reached — intentional) and only implements the two verbs UpdateReviewPolicy exercises:
// ReadProject (to supply the current version) and RecordReviewPolicy (the write verb).
type fakeReviewPolicyTransition struct {
	projectstate.ConstructionTransitionAccess
	version    projectstate.Version
	lastPolicy *projectstate.ReviewPolicy
}

func (f *fakeReviewPolicyTransition) RecordReviewPolicy(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, policy projectstate.ReviewPolicy, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.version++
	f.lastPolicy = &policy
	return f.version, nil
}

// TestUpdateReviewPolicy asserts that UpdateReviewPolicy maps the ReviewPolicyInput
// through projectstate.ReviewPolicyFromGateIDs and calls RecordReviewPolicy with the
// resulting typed ReviewPolicy. The ad-hoc gate id "svc-contract" maps to
// MethodPhaseDetailedDesign for the "service" activity type; after the call,
// RequiresHuman("service", MethodPhaseDetailedDesign) must be true on the persisted policy.
func TestUpdateReviewPolicy(t *testing.T) {
	fake := &fakeReviewPolicyTransition{version: 7}
	// The version read moved to the base projectStateAccess port (constructionTransitionAccess.ReadProject
	// pruned); RecordReviewPolicy stays on constructionTransition.
	ps := &projectstatefake.FakeProjectStateAccess{
		ReadProjectFn: func(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
			return projectstate.Project{Version: 7}, nil
		},
	}
	m := newConstructionManager(nil, ps, nil, nil, nil, nil, nil, fake, nil, nil, nil, nil, 0, "", nil)

	err := m.UpdateReviewPolicy(testCtx(), "proj-1", ReviewPolicyInput{
		GatedPhasesByType: map[string][]string{
			"service": {"svc-contract"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateReviewPolicy: %v", err)
	}
	if fake.lastPolicy == nil {
		t.Fatal("RecordReviewPolicy was not called")
	}
	// "svc-contract" is the ad-hoc gate id that maps to MethodPhaseDetailedDesign
	// via projectstate.gateIDToPhase; ReviewPolicyFromGateIDs must translate it.
	if !fake.lastPolicy.RequiresHuman("service", projectstate.MethodPhaseDetailedDesign) {
		t.Fatalf("expected service/detailed_design to require human, got policy=%+v", fake.lastPolicy)
	}
}

// =============================================================================
// C-MCN-GIT wiring tests. They drive the REAL constructionManager per-activity
// workflow (ConstructActivityWorkflow + its real Activity wrappers + the real
// rail→record sequencing in gitforward.go) over the Temporal in-memory test env,
// with the git-forward slice WIRED. They assert the resulting ActivityGit head-state
// at each lifecycle transition (branch-open → CI → arch-approved → merged) through the
// real Manager seam, and the idempotent-retry invariant (re-running a record step does
// NOT double-record / corrupt the row).
//
// Per [[project_aiarch_testing_no_bdd]] (black-box, wire-level, anti-cheat §7): the
// observable is the recorded head-state side effects on a faithful store — NOT internal
// calls. The GitStatus seam is backed by stubGitStatus, an in-memory store that
// implements the SAME partial-map-key upsert + idempotency-dedup semantics the real
// *projectstate.GitStore proves under gitactivity_test.go (the real store is exercised
// there against a throwaway on-disk repo; here the Manager WIRING is the system under
// test). The rail is a controllable double returning scripted opaque handles + CI.
// =============================================================================

// ---- stubRail: a controllable IPullRequestRail double -----------------------

// stubRail returns scripted opaque handles + a scripted CI rollup, and records every
// call so the test can assert the rail was driven in the expected order. It honors the
// frozen rail surface (opaque returns; the Manager records them).
type stubRail struct {
	mu sync.Mutex

	prRef    string
	ciRollup sourcecontrol.CheckState
	merged   bool

	opened    []string // branch names OpenBranch saw
	prOpened  []sourcecontrol.PullRequestSpec
	statuses  int
	reviews   []sourcecontrol.ReviewSubmission
	merges    int
	credMints int
}

func (r *stubRail) GetInstallationToken(_ fwra.Context, _ sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.credMints++
	return sourcecontrol.RepoCredential{Bytes: []byte("tok")}, nil
}

// OpenBranch returns the ZERO BranchRef: the frozen rail surface exposes
// RepoRefFromString / PullRequestRefFromString but NO BranchRefFromString, so a test
// double cannot mint a non-empty opaque BranchRef. The Manager records whatever
// BranchRef.String() yields (here ""); in production the real rail returns a populated
// handle. Assertions therefore key on the branch NAME + the PR ref (both
// test-constructable), NOT on BranchRef content. (Noted as a minor contract gap in
// C-MCN-GIT.md — non-blocking; the wiring records the rail's return verbatim.)
func (r *stubRail) OpenBranch(_ fwra.Context, _ sourcecontrol.RepoRef, branch sourcecontrol.BranchName, _ sourcecontrol.RepoCredential) (sourcecontrol.BranchRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opened = append(r.opened, string(branch))
	return sourcecontrol.BranchRef(""), nil
}

func (r *stubRail) OpenPullRequest(_ fwra.Context, _ sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prOpened = append(r.prOpened, spec)
	return sourcecontrol.PullRequestRefFromString(r.prRef), nil
}

func (r *stubRail) GetPullRequestStatus(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses++
	return sourcecontrol.PullRequestStatus{CheckRollup: r.ciRollup, ApprovalCount: 1, Mergeable: true}, nil
}

func (r *stubRail) PostReview(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, review sourcecontrol.ReviewSubmission, _ sourcecontrol.RepoCredential) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reviews = append(r.reviews, review)
	return nil
}

func (r *stubRail) MergePullRequest(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.MergeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.merges++
	return sourcecontrol.MergeResult{Commit: "main-sha", Merged: r.merged}, nil
}

// The remaining SourceControlAccess ops are outside the PR-rail lifecycle the git-forward
// spine drives; the stub satisfies the full contract with inert implementations so it can
// back the GENERATED rail Activities.
func (r *stubRail) AdoptProjectRepo(_ fwra.Context, _ sourcecontrol.RepoAdoptionSpec) (sourcecontrol.RepoRef, error) {
	return sourcecontrol.RepoRef(""), nil
}

func (r *stubRail) CommitManagedFiles(_ fwra.Context, _ sourcecontrol.RepoRef, _ []sourcecontrol.ManagedFile, _ sourcecontrol.RepoCredential) (sourcecontrol.CommitRef, error) {
	return sourcecontrol.CommitRef(""), nil
}

func (r *stubRail) ConfigureBranchProtection(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.RepoCredential) error {
	return nil
}

func (r *stubRail) InstallAuthorizeApp(_ fwra.Context, _ sourcecontrol.AccountRef) (sourcecontrol.Installation, error) {
	return sourcecontrol.Installation(""), nil
}

func (r *stubRail) SyncManagedScaffold(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.RepoCredential) (bool, error) {
	return false, nil
}

var _ sourcecontrol.SourceControlAccess = (*stubRail)(nil)

// ---- stubGitStatus: an in-memory git head-state mirror ----------------------

// stubGitStatus faithfully reproduces the real GitStore's per-activity record
// semantics: a partial-map-key upsert keyed by activityID, the PR-tolerant branch-open
// fusing, the CICheck=Pending birth, and dedup-first idempotency on idempotencyKey (a
// retried key returns the prior Version with NO second apply). It exposes the recorded
// rows so the test asserts the head-state the real workflow produced.
type stubGitStatus struct {
	mu sync.Mutex

	rows    map[string]projectstate.ActivityGitStatus
	cons    map[string]projectstate.ActivityConstructionStatus // per-activity construction lifecycle (Task 3)
	version projectstate.Version
	dedup   map[fwra.IdempotencyKey]projectstate.Version
	applies int // count of NON-deduped (real) applies — proves no double-apply
}

func newStubGitStatus(seed projectstate.Version) *stubGitStatus {
	return &stubGitStatus{
		rows:    map[string]projectstate.ActivityGitStatus{},
		cons:    map[string]projectstate.ActivityConstructionStatus{},
		version: seed,
		dedup:   map[fwra.IdempotencyKey]projectstate.Version{},
	}
}

// apply is the shared upsert path: dedup-first, then a partial map-key mutation +
// version bump. It mirrors gitstore.applyMutation (dedup-first; modeRequireExisting is
// irrelevant here since the project is seeded).
func (s *stubGitStatus) apply(key fwra.IdempotencyKey, activityID string, mutate func(g *projectstate.ActivityGitStatus)) (projectstate.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "empty activityID")
	}
	if v, ok := s.dedup[key]; ok {
		return v, nil // dedup-first: no second apply
	}
	s.applies++
	g := s.rows[activityID]
	g.ActivityID = activityID
	mutate(&g)
	s.rows[activityID] = g
	s.version++
	s.dedup[key] = s.version
	return s.version, nil
}

func (s *stubGitStatus) RecordActivityBranchOpened(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID, branch, branchRef, prRef, crLabel string, isRevert bool, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return s.apply(key, activityID, func(g *projectstate.ActivityGitStatus) {
		g.BranchName = branch
		g.BranchRef = branchRef
		if prRef != "" {
			g.PullRequestRef = prRef
		}
		if crLabel != "" {
			g.CRLabel = crLabel
		}
		if isRevert {
			g.IsRevert = true
		}
		// CICheck=Pending on first birth (real store sets it when first).
		if g.CICheck == 0 {
			g.CICheck = projectstate.CICheckPending
		}
	})
}

func (s *stubGitStatus) RecordActivityCIObserved(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, ci projectstate.CICheckState, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return s.apply(key, activityID, func(g *projectstate.ActivityGitStatus) { g.CICheck = ci })
}

func (s *stubGitStatus) RecordActivityArchApproved(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return s.apply(key, activityID, func(g *projectstate.ActivityGitStatus) { g.ArchApproved = true })
}

func (s *stubGitStatus) RecordActivityMerged(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return s.apply(key, activityID, func(g *projectstate.ActivityGitStatus) { g.Merged = true })
}

func (s *stubGitStatus) RecordActivityStarted(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "empty activityID")
	}
	if v, ok := s.dedup[key]; ok {
		return v, nil
	}
	s.applies++
	c := s.cons[activityID]
	c.ActivityID = activityID
	c.Phase = projectstate.ActivityConstructionRunning
	s.cons[activityID] = c
	s.version++
	s.dedup[key] = s.version
	return s.version, nil
}

func (s *stubGitStatus) RecordActivityCompleted(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "empty activityID")
	}
	if v, ok := s.dedup[key]; ok {
		return v, nil
	}
	s.applies++
	c := s.cons[activityID]
	c.ActivityID = activityID
	c.Phase = projectstate.ActivityConstructionDone
	s.cons[activityID] = c
	s.version++
	s.dedup[key] = s.version
	return s.version, nil
}

func (s *stubGitStatus) row(activityID string) (projectstate.ActivityGitStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.rows[activityID]
	return g, ok
}

// constructionPhase returns the recorded construction lifecycle phase for activityID.
func (s *stubGitStatus) constructionPhase(activityID string) (projectstate.ActivityConstructionPhase, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cons[activityID]
	return c.Phase, ok
}

var _ projectstate.GitActivityStatusAccess = (*stubGitStatus)(nil)

// ---- helpers ----------------------------------------------------------------

// registerGenRail registers the GENERATED PR-rail Activities (backed by the stub rail)
// under their generated registered names — the names the generated invoker surface
// (wf.Acts.Rail*) dispatches by. The workflow reaches the rail only through those invokers.
func registerGenRail(env *testsuite.TestWorkflowEnvironment, rail sourcecontrol.SourceControlAccess) {
	acts := &genActivities{Rail: rail}
	env.RegisterActivityWithOptions(acts.RailGetInstallationToken, activity.RegisterOptions{Name: "sourceControlAccess.getInstallationToken"})
	env.RegisterActivityWithOptions(acts.RailOpenBranch, activity.RegisterOptions{Name: "sourceControlAccess.openBranch"})
	env.RegisterActivityWithOptions(acts.RailOpenPullRequest, activity.RegisterOptions{Name: "sourceControlAccess.openPullRequest"})
	env.RegisterActivityWithOptions(acts.RailGetPullRequestStatus, activity.RegisterOptions{Name: "sourceControlAccess.getPullRequestStatus"})
	env.RegisterActivityWithOptions(acts.RailPostReview, activity.RegisterOptions{Name: "sourceControlAccess.postReview"})
	env.RegisterActivityWithOptions(acts.RailMergePullRequest, activity.RegisterOptions{Name: "sourceControlAccess.mergePullRequest"})
}

// registerConstructGit registers the per-activity workflow + ALL activities including
// the git-forward ones — every one GENERATED (B8 + follow-up): the pipeline/
// designSession-read/projectState-version/constructionTransition/rail surfaces (via
// fakes) and the gitActivityStatusAccess Record* activities backed by git (NOT ps —
// the git-forward tests wire wfDeps.GitStatus to a SEPARATE stubGitStatus store,
// distinct from ps, so the registration backing must match; this is deliberately NOT
// built by delegating to registerConstruct, which always backs gitActivityStatusAccess
// with ps).
func registerConstructGit(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps *fakeProjectState, git *stubGitStatus, rail sourcecontrol.SourceControlAccess) {
	env.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: executionKindConstructActivity})
	registerGenPipeline(env, &fakePipeline{phase: PipelineSucceeded})
	registerGenDesignSessionRead(env, ps)
	registerGenProjectStateVersion(env, ps)
	registerGenConstructionTransition(env, ps)
	registerGenGitStatus(env, git)
	registerGenRail(env, rail)
}

// gitWiredWorkflows builds a workflows with the git-forward slice wired to the supplied
// rail + git store, a fixed repo resolver, and the happy-path engine fakes. The rail is
// reached through the generated invoker surface (Acts); RailEnabled + the repo resolver +
// the GitStatus mirror are what light up the PR-rail lifecycle.
func gitWiredWorkflows(_ *fakeProjectState, rail *stubRail, git *stubGitStatus, mergeable bool) *workflows {
	rail.merged = mergeable
	d := wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		// git-forward slice wired: the rail is registered as GENERATED Activities and
		// gated on RailEnabled; the GitStatus mirror + repo resolver light the lifecycle.
		RailEnabled: true,
		GitStatus:   git,
		Repo: func(_ ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|owner/repo-1"), true
		},
	}
	return newWorkflows(d)
}

// gitSampleActivity carries a cr-NN label + revert flag so the recorded row's CR fields
// are asserted end-to-end.
func gitSampleActivity() constructionActivity {
	a := sampleActivity()
	a.ActivityID = "C-MST"
	a.CRLabel = "cr-021"
	return a
}

// ---- Tests ------------------------------------------------------------------

// gitLifecycleAssertRailDriven asserts the full-lifecycle rail choreography for the
// C-MST activity: branch + PR opened (with the cr label riding in Hints), one +1
// (Approve) relayed, one merge performed.
func gitLifecycleAssertRailDriven(t *testing.T, rail *stubRail) {
	t.Helper()
	if len(rail.opened) == 0 || rail.opened[0] != "activity/C-MST" {
		t.Fatalf("want OpenBranch(activity/C-MST), got %v", rail.opened)
	}
	if len(rail.prOpened) != 1 || rail.prOpened[0].Base != mainBranch {
		t.Fatalf("want one OpenPullRequest with base=main, got %+v", rail.prOpened)
	}
	if string(rail.prOpened[0].Hints) != "cr-021" {
		t.Fatalf("cr label must ride in PR Hints, got %q", rail.prOpened[0].Hints)
	}
	if len(rail.reviews) != 1 || rail.reviews[0].Verdict != sourcecontrol.ReviewApprove {
		t.Fatalf("want one +1 (Approve) relayed, got %+v", rail.reviews)
	}
	if rail.merges != 1 {
		t.Fatalf("want one MergePullRequest, got %d", rail.merges)
	}
}

// gitLifecycleAssertHeadStateRow asserts the recorded ActivityGit[C-MST] row mirrors
// the full lifecycle (branch/PR handles, CR label, CI success, arch +1, merged).
func gitLifecycleAssertHeadStateRow(t *testing.T, git *stubGitStatus) {
	t.Helper()
	g, ok := git.row("C-MST")
	if !ok {
		t.Fatal("ActivityGit[C-MST] was never recorded")
	}
	if g.BranchName != "activity/C-MST" || g.PullRequestRef != "pr-7" {
		t.Fatalf("branch/PR handles wrong: %+v", g)
	}
	if g.CRLabel != "cr-021" {
		t.Fatalf("CR label not recorded: %+v", g)
	}
	if g.CICheck != projectstate.CICheckSuccess {
		t.Fatalf("CICheck = %v, want Success", g.CICheck)
	}
	if !g.ArchApproved {
		t.Fatalf("ArchApproved not recorded: %+v", g)
	}
	if !g.Merged {
		t.Fatalf("Merged not recorded: %+v", g)
	}
}

// The full git-forward lifecycle records branch-open → CI(success) → arch-approved →
// merged onto the per-activity head-state, in order, through the real Manager workflow.
func Test_GitForward_FullLifecycle_RecordsHeadState(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 5, Phase: 2}, version: 5}
	rail := &stubRail{prRef: "pr-7", ciRollup: sourcecontrol.CheckSuccess}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true /*mergeable*/)
	registerConstructGit(env, wf, ps, git, rail)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: pid, ActivityID: "C-MST", Activity: gitSampleActivity(),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	// Rail was driven: branch + PR opened, CI read, +1 relayed, merge performed.
	gitLifecycleAssertRailDriven(t, rail)

	// Head-state mirror reflects the full lifecycle.
	gitLifecycleAssertHeadStateRow(t, git)

	// Task 3: the per-activity construction lifecycle recorded Running (started) then
	// Done (completed) through the same git-wired spine.
	phase, ok := git.constructionPhase("C-MST")
	if !ok {
		t.Fatal("ActivityConstruction[C-MST] was never recorded (started/completed)")
	}
	if phase != projectstate.ActivityConstructionDone {
		t.Fatalf("construction phase = %v, want Done (completed) after a happy-path spine", phase)
	}
}

// Task 3: the per-activity construction head-state flips to Running at the top of the
// spine and Done at the end — the records the pump's eligibility selection reads. A
// happy-path git-wired run leaves the activity Done so dependents unblock.
func Test_Construction_StartedThenCompleted_RecordedOnHeadState(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 5, Phase: 2}, version: 5}
	rail := &stubRail{prRef: "pr-1", ciRollup: sourcecontrol.CheckSuccess}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true /*mergeable*/)
	registerConstructGit(env, wf, ps, git, rail)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: pid, ActivityID: "C-MST", Activity: gitSampleActivity(),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	phase, ok := git.constructionPhase("C-MST")
	if !ok {
		t.Fatal("no construction head-state recorded")
	}
	if phase != projectstate.ActivityConstructionDone {
		t.Fatalf("want Done after a completed activity, got %v", phase)
	}
}

// A CI failure is mirrored as Failure (the dumb reflection); the lifecycle still
// proceeds to record the rest (CI is NOT a gate at this seam — the gate is
// interventionEngine, modeled by the merge mergeable flag, not CI).
func Test_GitForward_CIFailure_MirroredNotGated(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}, version: 1}
	rail := &stubRail{prRef: "pr-1", ciRollup: sourcecontrol.CheckFailure}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true)
	registerConstructGit(env, wf, ps, git, rail)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: pid, ActivityID: "C-CI", Activity: constructionActivity{ActivityID: "C-CI", Kind: activityKindConstruction, ComponentID: "c"},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	g, ok := git.row("C-CI")
	if !ok {
		t.Fatal("row never recorded")
	}
	if g.CICheck != projectstate.CICheckFailure {
		t.Fatalf("CICheck = %v, want Failure mirrored", g.CICheck)
	}
}

// The dormant slice (rail/git unwired) leaves the spine untouched: no git rows recorded
// and the activity still completes the non-git records.
func Test_GitForward_Dormant_WhenUnwired(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}, version: 1}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		// no git-forward slice — RailEnabled=false, GitStatus/Repo nil.
	})
	registerConstruct(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: pid, ActivityID: "C-NO-GIT", Activity: sampleActivity(),
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// Non-git spine still recorded the binary exit.
	if len(ps.exited) != 1 {
		t.Fatalf("dormant slice must still complete the non-git spine, got exited=%v", ps.exited)
	}
}

// Idempotent retry: re-running the branch-opened record step with the SAME idempotency
// key returns the prior Version and does NOT double-apply (the dedup-first invariant the
// crash-safe workflow relies on). Driven directly through the Activity wrapper against
// the stub store (the workflow-replay path uses the same key derivation).
func Test_GitForward_RecordActivity_IdempotentRetry_NoDoubleApply(t *testing.T) {
	git := newStubGitStatus(10)
	ctx := context.Background()
	pid := projectstate.ProjectID(uuid.NewString())

	// Same idempotency key twice (a workflow retry re-runs the same Activity id).
	key := fwra.IdempotencyKey("wf-1:branch")
	v1, err := git.RecordActivityBranchOpened(fwra.Context{Context: ctx}, pid, 10, "C-MST", "activity/C-MST", "ref", "pr-1", "cr-021", false, projectstate.RepoCredential{}, key)
	if err != nil {
		t.Fatalf("first record: %v", err)
	}
	v2, err := git.RecordActivityBranchOpened(fwra.Context{Context: ctx}, pid, 0 /*stale*/, "C-MST", "activity/C-MST", "ref", "pr-1", "cr-021", false, projectstate.RepoCredential{}, key)
	if err != nil {
		t.Fatalf("idempotent re-record: %v", err)
	}
	if v1 != v2 {
		t.Fatalf("idempotent re-record version = %d, want prior %d", v2, v1)
	}
	if git.applies != 1 {
		t.Fatalf("dedup-first must apply exactly once, applied %d times (DOUBLE APPLY)", git.applies)
	}
}

// workflowGitForwardOrder asserts the rail+record order at the workflow level by
// checking that, on a successful lifecycle, the head-state row ends in the terminal
// (merged) state — i.e. every step ran and recorded in sequence without a gap.
func Test_GitForward_RecordsConvergeMonotonically(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 2, Phase: 2}, version: 2}
	rail := &stubRail{prRef: "pr-9", ciRollup: sourcecontrol.CheckSuccess}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true)
	registerConstructGit(env, wf, ps, git, rail)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: pid, ActivityID: "C-MONO", Activity: constructionActivity{ActivityID: "C-MONO", Kind: activityKindConstruction, ComponentID: "c"},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	g, _ := git.row("C-MONO")
	if g.BranchName == "" || g.CICheck != projectstate.CICheckSuccess || !g.ArchApproved || !g.Merged {
		t.Fatalf("lifecycle did not converge through all record steps: %+v", g)
	}
	// At least 4 distinct applies (branch, ci, approve, merge) landed.
	if git.applies < 4 {
		t.Fatalf("want >=4 record applies across the lifecycle, got %d", git.applies)
	}
}

// makeCommittedNetwork builds a minimal committed ArtifactSlot holding a *projectstate.Network.
func makeCommittedNetwork(deps []projectstate.NetworkDependency) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{
			Dependencies: deps,
		},
	}
}

// makeCommittedNetworkWithMilestones builds a committed Network ArtifactSlot carrying
// both the authored activity dependencies AND the authored milestones — the
// combination the live benchmark drain (N-PERF/N-IT/N-UAT/N-HARD/N-DEPLOY/N-DOC all
// gating on M3/M4) needs to reproduce.
func makeCommittedNetworkWithMilestones(deps []projectstate.NetworkDependency, milestones []projectstate.NetworkMilestone) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{
			Dependencies: deps,
			Milestones:   milestones,
		},
	}
}

// makeCommittedActivityList builds a minimal committed ArtifactSlot holding a *projectstate.ActivityList.
func makeCommittedActivityList(items []projectstate.ActivityItem) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.ActivityList{
			Activities: items,
		},
	}
}

// makeCommittedSystemDesign builds a minimal committed ArtifactSlot holding a
// *projectstate.System with the given components — the authored-componentId lookup
// target nextEligibleActivity consults instead of ServiceContracts.
func makeCommittedSystemDesign(comps []projectstate.Component) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model:  &projectstate.System{Components: comps},
	}
}

var todoComponents = []projectstate.Component{
	{ID: "todo-list-manager", Name: "TodoListManager", Layer: projectstate.LayerManager},
	{ID: "todo-owner-client", Name: "TodoOwnerClient", Layer: projectstate.LayerClient},
}

func projWithActivities(acts []projectstate.ActivityItem, deps []projectstate.NetworkDependency) projectstate.Project {
	return projectstate.Project{
		Phase:        projectstate.PhaseConstruction,
		Network:      makeCommittedNetwork(deps),
		ActivityList: makeCommittedActivityList(acts),
		SystemDesign: makeCommittedSystemDesign(todoComponents),
	}
}

// The regression the whole change exists for: a fresh project with NO service
// contracts must still dispatch its first coding activity.
func TestNextEligibleActivity_DispatchesWithNoServiceContracts(t *testing.T) {
	proj := projWithActivities(
		[]projectstate.ActivityItem{{
			Name: "C-TLM", Title: "TodoListManager — settlement manager",
			Coding: true, EffortDays: 13, ComponentID: "todo-list-manager",
		}},
		[]projectstate.NetworkDependency{{Activity: "C-TLM", DependsOn: []string{}}},
	)
	// Deliberately nil: ServiceContracts must play no part in selection.
	proj.ServiceContracts = nil

	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("want verdictDispatch, got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
	}
	if sel.Activity.ComponentID != "todo-list-manager" {
		t.Fatalf("want componentID todo-list-manager, got %q", sel.Activity.ComponentID)
	}
	if sel.Activity.Layer != "manager" {
		t.Fatalf("want layer hydrated to manager, got %q", sel.Activity.Layer)
	}
	if sel.Activity.Kind != activityKindConstruction {
		t.Fatalf("want activityKindConstruction, got %v", sel.Activity.Kind)
	}
}

// Nonstructural coding (ch.13 Table 13-2) and noncoding activities are LEGAL
// with no componentId and dispatch with an empty component_id.
func TestNextEligibleActivity_ComponentlessActivitiesDispatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		item projectstate.ActivityItem
	}{
		{"nonstructural coding", projectstate.ActivityItem{Name: "I-UC1", Title: "Integrate use case 1", Coding: true}},
		{"noncoding", projectstate.ActivityItem{Name: "N-STP", Title: "System Test Plan", Coding: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := projWithActivities(
				[]projectstate.ActivityItem{tc.item},
				[]projectstate.NetworkDependency{{Activity: tc.item.Name, DependsOn: []string{}}},
			)
			sel := nextEligibleActivity(proj)
			if sel.Verdict != verdictDispatch {
				t.Fatalf("want verdictDispatch, got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
			}
			if sel.Activity.ComponentID != "" {
				t.Fatalf("want empty componentID, got %q", sel.Activity.ComponentID)
			}
		})
	}
}

// The ONE blocked condition: a non-empty componentId naming no committed component.
func TestNextEligibleActivity_UnknownComponentIsBlocked(t *testing.T) {
	proj := projWithActivities(
		[]projectstate.ActivityItem{{
			Name: "C-TLM", Title: "TodoListManager", Coding: true, ComponentID: "todo-list-managr",
		}},
		[]projectstate.NetworkDependency{{Activity: "C-TLM", DependsOn: []string{}}},
	)
	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictBlocked {
		t.Fatalf("want verdictBlocked, got %v", sel.Verdict)
	}
	if sel.BlockedActivityID != "C-TLM" {
		t.Fatalf("want blocked activity C-TLM, got %q", sel.BlockedActivityID)
	}
	// The detail must name both the activity and the unresolvable id — it is the
	// only thing an operator sees in the console.
	if !strings.Contains(sel.BlockedReason, "C-TLM") || !strings.Contains(sel.BlockedReason, "todo-list-managr") {
		t.Fatalf("blocked reason must name the activity and the bad id, got %q", sel.BlockedReason)
	}
}

func TestNextEligibleActivity_NothingEligibleIsQuiescent(t *testing.T) {
	// A is authored into the ActivityList (a real, in-flight predecessor of B) so
	// this fixture stays realistic under the milestone-aware resolver: an id that
	// resolves to neither an activity nor a milestone is now a reported plan defect
	// (verdictBlocked), not silently "just not done yet". A is Running (not
	// NotStarted, so it is not itself a candidate this tick, and not Done, so it
	// does not satisfy B) — the fixture now represents "the only candidate's
	// dependency genuinely isn't satisfied yet" instead of leaning on an
	// unauthored id.
	proj := projWithActivities(
		[]projectstate.ActivityItem{
			{Name: "A", Title: "A", Coding: false},
			{Name: "B", Title: "B", Coding: false},
		},
		[]projectstate.NetworkDependency{
			{Activity: "A", DependsOn: []string{}},
			{Activity: "B", DependsOn: []string{"A"}},
		},
	)
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionRunning},
	}
	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictQuiescent {
		t.Fatalf("want verdictQuiescent, got %v", sel.Verdict)
	}
}

// TestNextEligibleActivity_Chain exercises the A→B→C network with progressively
// committed construction status entries.
func TestNextEligibleActivity_Chain(t *testing.T) {
	// Network: A has no deps; B dependsOn A; C dependsOn B.
	network := []projectstate.NetworkDependency{
		{Activity: "A", DependsOn: []string{}},
		{Activity: "B", DependsOn: []string{"A"}},
		{Activity: "C", DependsOn: []string{"B"}},
	}
	activities := []projectstate.ActivityItem{
		{Name: "A", Title: "A", EffortDays: 5, WorkerClass: "AI", Coding: true, RiskBucket: 2, ComponentID: "comp-a"},
		{Name: "B", Title: "B", EffortDays: 3, WorkerClass: "AI", Coding: true, RiskBucket: 1, ComponentID: "comp-b"},
		{Name: "C", Title: "C", EffortDays: 8, WorkerClass: "Human", Coding: false, RiskBucket: 3},
	}

	// Selection now resolves the authored ComponentID against the committed
	// systemDesign — ServiceContracts play no part. C is noncoding (no ComponentID).
	base := projectstate.Project{
		Phase:        projectstate.PhaseConstruction,
		Network:      makeCommittedNetwork(network),
		ActivityList: makeCommittedActivityList(activities),
		SystemDesign: makeCommittedSystemDesign([]projectstate.Component{
			{ID: "comp-a", Name: "A", Layer: projectstate.LayerManager},
			{ID: "comp-b", Name: "B", Layer: projectstate.LayerManager},
		}),
	}

	// ---- Case 1: empty ActivityConstruction → A is eligible (no deps). ----
	proj := base
	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("case 1: expected verdictDispatch, got %v", sel.Verdict)
	}
	if sel.Activity.ActivityID != "A" {
		t.Fatalf("case 1: expected A, got %q", sel.Activity.ActivityID)
	}
	if sel.Activity.EstimateDays != 5 {
		t.Fatalf("case 1: expected EstimateDays=5, got %f", sel.Activity.EstimateDays)
	}

	// ---- Case 2: A Done → B is eligible. ----
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionDone},
	}
	sel = nextEligibleActivity(proj)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("case 2: expected verdictDispatch, got %v", sel.Verdict)
	}
	if sel.Activity.ActivityID != "B" {
		t.Fatalf("case 2: expected B, got %q", sel.Activity.ActivityID)
	}
	if sel.Activity.EstimateDays != 3 {
		t.Fatalf("case 2: expected EstimateDays=3, got %f", sel.Activity.EstimateDays)
	}

	// ---- Case 3: A Done, B Running → nothing eligible (C blocked; B running). ----
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionDone},
		"B": {ActivityID: "B", Phase: projectstate.ActivityConstructionRunning},
	}
	sel = nextEligibleActivity(proj)
	if sel.Verdict != verdictQuiescent {
		t.Fatalf("case 3: expected verdictQuiescent, got %v", sel.Verdict)
	}

	// ---- Case 4: A Done, B Done → C is eligible. ----
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionDone},
		"B": {ActivityID: "B", Phase: projectstate.ActivityConstructionDone},
	}
	sel = nextEligibleActivity(proj)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("case 4: expected verdictDispatch, got %v", sel.Verdict)
	}
	if sel.Activity.ActivityID != "C" {
		t.Fatalf("case 4: expected C, got %q", sel.Activity.ActivityID)
	}
	if sel.Activity.EstimateDays != 8 {
		t.Fatalf("case 4: expected EstimateDays=8, got %f", sel.Activity.EstimateDays)
	}
}

// TestNextEligibleActivity_MilestoneDependencySatisfied reproduces the live benchmark
// drain: an activity (N-DOC) depends on a MILESTONE (M4), not an activity. M4 is
// itself authored with its own dependsOn — I-UC-CONSULT and I-UC-AMEND, both Done.
// The milestone has genuinely been reached; before the fix, allDepsDone looked M4 up
// in the activity-status map, never found it (milestones never get a head-state
// record), and treated it as permanently unsatisfied — this is the exact false block
// that stranded 6 activities in the drain. This test FAILS on the old allDepsDone.
func TestNextEligibleActivity_MilestoneDependencySatisfied(t *testing.T) {
	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "I-UC-CONSULT", DependsOn: []string{}},
				{Activity: "I-UC-AMEND", DependsOn: []string{}},
				{Activity: "N-DOC", DependsOn: []string{"M4"}},
			},
			[]projectstate.NetworkMilestone{
				{ID: "M4", Name: "M4", DependsOn: []string{"I-UC-CONSULT", "I-UC-AMEND"}},
			},
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "I-UC-CONSULT", Coding: true},
			{Name: "I-UC-AMEND", Coding: true},
			{Name: "N-DOC", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
		ActivityConstruction: map[string]projectstate.ActivityConstructionStatus{
			"I-UC-CONSULT": {ActivityID: "I-UC-CONSULT", Phase: projectstate.ActivityConstructionDone},
			"I-UC-AMEND":   {ActivityID: "I-UC-AMEND", Phase: projectstate.ActivityConstructionDone},
		},
	}

	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("want verdictDispatch (M4 reached via its own dependsOn), got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
	}
	if sel.Activity.ActivityID != "N-DOC" {
		t.Fatalf("want N-DOC dispatched, got %q", sel.Activity.ActivityID)
	}
}

// TestNextEligibleActivity_MilestoneDependencyNotSatisfied is the mirror case: M4's
// own dependsOn are NOT all Done (I-UC-AMEND still pending), so the milestone has
// genuinely not been reached and N-DOC must stay ineligible.
func TestNextEligibleActivity_MilestoneDependencyNotSatisfied(t *testing.T) {
	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "I-UC-CONSULT", DependsOn: []string{}},
				{Activity: "I-UC-AMEND", DependsOn: []string{}},
				{Activity: "N-DOC", DependsOn: []string{"M4"}},
			},
			[]projectstate.NetworkMilestone{
				{ID: "M4", Name: "M4", DependsOn: []string{"I-UC-CONSULT", "I-UC-AMEND"}},
			},
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "I-UC-CONSULT", Coding: true},
			{Name: "I-UC-AMEND", Coding: true},
			{Name: "N-DOC", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
		ActivityConstruction: map[string]projectstate.ActivityConstructionStatus{
			"I-UC-CONSULT": {ActivityID: "I-UC-CONSULT", Phase: projectstate.ActivityConstructionDone},
			"I-UC-AMEND":   {ActivityID: "I-UC-AMEND", Phase: projectstate.ActivityConstructionRunning},
		},
	}

	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictQuiescent {
		t.Fatalf("want verdictQuiescent (M4 not yet reached), got %v activity=%q", sel.Verdict, sel.Activity.ActivityID)
	}
}

// TestNextEligibleActivity_MilestoneDependsOnMilestone exercises the recursive case:
// M4 depends on M3 (a milestone, not an activity), and M3 depends on an activity
// that's Done. N-DOC (dependsOn M4) must resolve all the way through M4 -> M3 -> A.
func TestNextEligibleActivity_MilestoneDependsOnMilestone(t *testing.T) {
	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "A", DependsOn: []string{}},
				{Activity: "N-DOC", DependsOn: []string{"M4"}},
			},
			[]projectstate.NetworkMilestone{
				{ID: "M3", Name: "M3", DependsOn: []string{"A"}},
				{ID: "M4", Name: "M4", DependsOn: []string{"M3"}},
			},
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "A", Coding: true},
			{Name: "N-DOC", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
		ActivityConstruction: map[string]projectstate.ActivityConstructionStatus{
			"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionDone},
		},
	}

	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("want verdictDispatch through M4->M3->A, got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
	}
	if sel.Activity.ActivityID != "N-DOC" {
		t.Fatalf("want N-DOC dispatched, got %q", sel.Activity.ActivityID)
	}

	// Unsatisfied at the bottom of the chain (A not Done) must propagate all the way
	// back up through both milestone hops.
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionRunning},
	}
	sel = nextEligibleActivity(proj)
	if sel.Verdict != verdictQuiescent {
		t.Fatalf("want verdictQuiescent (A not done, M3/M4 not reached), got %v", sel.Verdict)
	}
}

// TestNextEligibleActivity_MilestoneCycleTerminates proves the cycle guard: an
// authored network can (however wrongly) declare M-X depends on M-Y depends on M-X.
// Resolution must terminate — reported as verdictBlocked, not an infinite recursion —
// and the test itself is bounded so a regression that reintroduces unbounded
// recursion fails as a hang the test runner's own timeout catches, not silently.
func TestNextEligibleActivity_MilestoneCycleTerminates(t *testing.T) {
	done := make(chan pumpSelection, 1)
	go func() {
		proj := projectstate.Project{
			Phase: projectstate.PhaseConstruction,
			Network: makeCommittedNetworkWithMilestones(
				[]projectstate.NetworkDependency{
					{Activity: "N-DOC", DependsOn: []string{"M-X"}},
				},
				[]projectstate.NetworkMilestone{
					{ID: "M-X", Name: "M-X", DependsOn: []string{"M-Y"}},
					{ID: "M-Y", Name: "M-Y", DependsOn: []string{"M-X"}},
				},
			),
			ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
				{Name: "N-DOC", Coding: false},
			}),
			SystemDesign: makeCommittedSystemDesign(nil),
		}
		done <- nextEligibleActivity(proj)
	}()

	select {
	case sel := <-done:
		if sel.Verdict != verdictBlocked {
			t.Fatalf("want verdictBlocked on a milestone cycle, got %v", sel.Verdict)
		}
		if !strings.Contains(sel.BlockedReason, "cycle") {
			t.Fatalf("blocked reason must call out the cycle, got %q", sel.BlockedReason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nextEligibleActivity did not terminate on a milestone dependency cycle (infinite recursion)")
	}
}

// TestNextEligibleActivity_UnknownDependencyIdIsBlocked: an authored dependency id
// that names neither a known activity nor a known milestone is a genuine plan defect
// — loud (verdictBlocked), never silently treated as "just not satisfied yet" (which
// would be indistinguishable from an ordinary quiescent tick).
func TestNextEligibleActivity_UnknownDependencyIdIsBlocked(t *testing.T) {
	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "N-DOC", DependsOn: []string{"M-GHOST"}},
			},
			nil,
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "N-DOC", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
	}

	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictBlocked {
		t.Fatalf("want verdictBlocked for an unresolvable dependency id, got %v", sel.Verdict)
	}
	if sel.BlockedActivityID != "N-DOC" {
		t.Fatalf("want blocked activity N-DOC, got %q", sel.BlockedActivityID)
	}
	if !strings.Contains(sel.BlockedReason, "M-GHOST") {
		t.Fatalf("blocked reason must name the unresolvable id, got %q", sel.BlockedReason)
	}
}

// TestNextEligibleActivity_DependencyDefectDoesNotBlockUnrelatedWork: a dependency
// defect on ONE not-yet-eligible activity must not halt the pump entirely — an
// UNRELATED, currently-eligible activity still dispatches this tick. The defect only
// escalates to verdictBlocked once it is the thing actually stalling progress (see
// TestNextEligibleActivity_UnknownDependencyIdIsBlocked).
func TestNextEligibleActivity_DependencyDefectDoesNotBlockUnrelatedWork(t *testing.T) {
	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "A", DependsOn: []string{}},
				{Activity: "N-DOC", DependsOn: []string{"M-GHOST"}},
			},
			nil,
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "A", Coding: true},
			{Name: "N-DOC", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
	}

	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("want verdictDispatch for unrelated eligible activity A, got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
	}
	if sel.Activity.ActivityID != "A" {
		t.Fatalf("want A dispatched despite N-DOC's dependency defect, got %q", sel.Activity.ActivityID)
	}
}

// TestNextEligibleActivity_UncommittedSlots exercises the nil/uncommitted guard.
func TestNextEligibleActivity_UncommittedSlots(t *testing.T) {
	activities := []projectstate.ActivityItem{
		{Name: "A", EffortDays: 5, WorkerClass: "AI", Coding: true, RiskBucket: 2},
	}

	// Uncommitted Network slot (zero value ArtifactSlot).
	t.Run("uncommitted_network", func(t *testing.T) {
		proj := projectstate.Project{
			ActivityList: makeCommittedActivityList(activities),
		}
		sel := nextEligibleActivity(proj)
		if sel.Verdict != verdictQuiescent {
			t.Fatalf("expected verdictQuiescent for uncommitted network, got %v", sel.Verdict)
		}
	})

	// Uncommitted ActivityList slot.
	t.Run("uncommitted_activity_list", func(t *testing.T) {
		proj := projectstate.Project{
			Network: makeCommittedNetwork([]projectstate.NetworkDependency{
				{Activity: "A", DependsOn: []string{}},
			}),
		}
		sel := nextEligibleActivity(proj)
		if sel.Verdict != verdictQuiescent {
			t.Fatalf("expected verdictQuiescent for uncommitted activity list, got %v", sel.Verdict)
		}
	})

	// Both uncommitted (zero-value project).
	t.Run("both_uncommitted", func(t *testing.T) {
		sel := nextEligibleActivity(projectstate.Project{})
		if sel.Verdict != verdictQuiescent {
			t.Fatalf("expected verdictQuiescent for zero-value project, got %v", sel.Verdict)
		}
	})
}

// TestNextEligibleActivity_RequiresConstructionPhase verifies the pump refuses to
// select work until the project has been sealed into Phase 3 (AdvanceToConstruction)
// — committed Network+ActivityList alone, with the project still in system- or
// project-design, must not dispatch construction activities.
func TestNextEligibleActivity_RequiresConstructionPhase(t *testing.T) {
	network := []projectstate.NetworkDependency{{Activity: "A", DependsOn: []string{}}}
	activities := []projectstate.ActivityItem{
		{Name: "A", Title: "A", EffortDays: 5, WorkerClass: "AI", Coding: true, RiskBucket: 2, ComponentID: "comp-a"},
	}
	base := projectstate.Project{
		Network:      makeCommittedNetwork(network),
		ActivityList: makeCommittedActivityList(activities),
		SystemDesign: makeCommittedSystemDesign([]projectstate.Component{
			{ID: "comp-a", Name: "A", Layer: projectstate.LayerManager},
		}),
	}

	for _, tc := range []struct {
		name  string
		phase projectstate.Phase
	}{
		{"system_design", projectstate.PhaseSystemDesign},
		{"project_design", projectstate.PhaseProjectDesign},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := base
			proj.Phase = tc.phase
			if sel := nextEligibleActivity(proj); sel.Verdict != verdictQuiescent {
				t.Fatalf("expected verdictQuiescent before the construction seal (phase %v), got %v", tc.phase, sel.Verdict)
			}
		})
	}

	t.Run("construction", func(t *testing.T) {
		proj := base
		proj.Phase = projectstate.PhaseConstruction
		sel := nextEligibleActivity(proj)
		if sel.Verdict != verdictDispatch || sel.Activity.ActivityID != "A" {
			t.Fatalf("expected A eligible once sealed into construction, got verdict=%v id=%q", sel.Verdict, sel.Activity.ActivityID)
		}
	})
}

// TestNextEligibleActivity_ProjectExportDogfood exercises the dogfood activity
// C-PE (projectExport endpoint) introduced in Spec 2. C-PE depends on C-CW (Build Web Client)
// and D-MPD (Detailed design — projectDesignManager), which are both Phase=2 (Done)
// in the live project. This test uses a synthetic project where both deps are Done
// and C-PE is NotStarted, verifying nextEligibleActivity selects it.
//
// Reconciliation note: selection now resolves the authored ComponentID against the
// committed systemDesign, not a service-contract key — C-PE names component
// "projectExport" directly.
func TestNextEligibleActivity_ProjectExportDogfood(t *testing.T) {
	network := []projectstate.NetworkDependency{
		{Activity: "C-CW", DependsOn: []string{}},
		{Activity: "D-MPD", DependsOn: []string{}},
		{Activity: "C-PE", DependsOn: []string{"C-CW", "D-MPD"}},
	}
	activities := []projectstate.ActivityItem{
		{Name: "C-CW", EffortDays: 30, WorkerClass: "junior-developer", Coding: true, RiskBucket: 8},
		{Name: "D-MPD", EffortDays: 5, WorkerClass: "senior-developer", Coding: false, RiskBucket: 2},
		{Name: "C-PE", Title: "C-PE", EffortDays: 3, WorkerClass: "junior-developer", Coding: true, RiskBucket: 1, ComponentID: "projectExport"},
	}
	proj := projectstate.Project{
		Phase:        projectstate.PhaseConstruction,
		Network:      makeCommittedNetwork(network),
		ActivityList: makeCommittedActivityList(activities),
		SystemDesign: makeCommittedSystemDesign([]projectstate.Component{
			{ID: "projectExport", Name: "projectExport", Layer: projectstate.LayerManager},
		}),
		ActivityConstruction: map[string]projectstate.ActivityConstructionStatus{
			"C-CW":  {ActivityID: "C-CW", Phase: projectstate.ActivityConstructionDone},
			"D-MPD": {ActivityID: "D-MPD", Phase: projectstate.ActivityConstructionDone},
			// C-PE is absent (zero value = NotStarted)
		},
	}
	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("expected C-PE to be eligible, got verdict=%v (blocked=%q)", sel.Verdict, sel.BlockedReason)
	}
	if sel.Activity.ActivityID != "C-PE" {
		t.Fatalf("expected ActivityID=C-PE, got %q", sel.Activity.ActivityID)
	}
	if sel.Activity.ComponentID != "projectExport" {
		t.Fatalf("expected ComponentID=projectExport, got %q", sel.Activity.ComponentID)
	}
	if sel.Activity.EstimateDays != 3 {
		t.Fatalf("expected EstimateDays=3, got %f", sel.Activity.EstimateDays)
	}
	if sel.Activity.Kind != activityKindConstruction {
		t.Fatalf("expected Kind=activityKindConstruction (Coding=true), got %v", sel.Activity.Kind)
	}
}

// TestNextEligibleActivity_HydratedFields checks that the returned constructionActivity
// is fully hydrated from the ActivityList item and the resolved systemDesign component.
func TestNextEligibleActivity_HydratedFields(t *testing.T) {
	network := []projectstate.NetworkDependency{
		{Activity: "X", DependsOn: []string{}},
	}
	activities := []projectstate.ActivityItem{
		{Name: "X", Title: "X", EffortDays: 13, WorkerClass: "HumanSenior", Coding: true, RiskBucket: 5, ComponentID: "comp-x"},
	}
	proj := projectstate.Project{
		Phase:        projectstate.PhaseConstruction,
		Network:      makeCommittedNetwork(network),
		ActivityList: makeCommittedActivityList(activities),
		SystemDesign: makeCommittedSystemDesign([]projectstate.Component{
			{ID: "comp-x", Name: "X", Layer: projectstate.LayerEngine},
		}),
	}
	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("expected verdictDispatch, got %v", sel.Verdict)
	}
	if sel.Activity.ActivityID != "X" {
		t.Fatalf("expected ActivityID=X, got %q", sel.Activity.ActivityID)
	}
	if sel.Activity.EstimateDays != 13 {
		t.Fatalf("expected EstimateDays=13, got %f", sel.Activity.EstimateDays)
	}
	// Kind is determined by Coding flag: Coding=true → activityKindConstruction.
	if sel.Activity.Kind != activityKindConstruction {
		t.Fatalf("expected Kind=activityKindConstruction, got %v", sel.Activity.Kind)
	}
	if sel.Activity.ComponentID != "comp-x" {
		t.Fatalf("expected ComponentID=comp-x, got %q", sel.Activity.ComponentID)
	}
	if sel.Activity.Layer != "engine" {
		t.Fatalf("expected Layer=engine, got %q", sel.Activity.Layer)
	}
}

// TestPipelineAdapter_DispatchInputs asserts that dispatchInputsFor maps
// ActivityID → "activity_id" and ComponentID → "component_id".
// pipelineAdapter.inner is the agenticjob.AgenticJobAccess interface (not a concrete struct,
// interface), so we test the pure mapping helper directly — no fake adapter needed.
func TestPipelineAdapter_DispatchInputs(t *testing.T) {
	inputs := dispatchInputsFor(pipelineSpec{
		ActivityID:  "C-PE",
		ComponentID: "projectExport",
	})
	if inputs["activity_id"] != "C-PE" {
		t.Fatalf("expected activity_id=C-PE, got %q", inputs["activity_id"])
	}
	if inputs["component_id"] != "projectExport" {
		t.Fatalf("expected component_id=projectExport, got %q", inputs["component_id"])
	}
}

// TestDispatchInputsFor_WithPhase asserts that a non-empty Phase field on
// pipelineSpec is emitted as the "phase" key in the dispatch inputs map
// (REQ-2 + Plan 1 Task 6).
func TestDispatchInputsFor_WithPhase(t *testing.T) {
	spec := pipelineSpec{
		ActivityID:  "C-PE",
		ComponentID: "projectExport",
		Phase:       "requirements",
	}
	got := dispatchInputsFor(spec)
	cases := map[string]string{
		"activity_id":  "C-PE",
		"component_id": "projectExport",
		"phase":        "requirements",
	}
	for k, want := range cases {
		if got[k] != want {
			t.Errorf("dispatchInputsFor[%q] = %q, want %q", k, got[k], want)
		}
	}
}

// TestDispatchInputsFor_EmptyPhaseOmitted asserts that an empty Phase value is
// NOT emitted — callers that do not set it get only activity_id/component_id,
// so existing workflow dispatches that rely on workflow-declared defaults are unaffected.
func TestDispatchInputsFor_EmptyPhaseOmitted(t *testing.T) {
	spec := pipelineSpec{
		ActivityID:  "C-PE",
		ComponentID: "projectExport",
		// Phase intentionally empty
	}
	got := dispatchInputsFor(spec)
	if _, ok := got["phase"]; ok {
		t.Error("phase key should not be present when Phase is empty")
	}
}

// fakeQueryClient scripts QueryWorkflow with an error, for the F20 pre-phase read test.
// It embeds client.Client so any unimplemented method panics.
type fakeQueryClient struct {
	client.Client
	queryErr error
}

func (f *fakeQueryClient) QueryWorkflow(_ context.Context, _ string, _ string, _ string, _ ...any) (converter.EncodedValue, error) {
	return nil, f.queryErr
}

// ---- F20: clean not-found altitude on the pre-phase construction read ------

// Before construction starts the pump workflow does not exist; Temporal's raw
// "workflow not found for ID: gtdapp:construction" must NOT reach the client. Map it to
// a clean, user-altitude NotFound.
func Test_GetSessionState_BeforeConstruction_CleanNotFound(t *testing.T) {
	fc := &fakeQueryClient{queryErr: serviceerror.NewNotFound("workflow not found for ID: gtdapp:construction")}
	m := newTestConstructionManager(fc)

	_, err := m.GetSessionState(testCtx(), ProjectID("gtdapp"), nil)
	e := asConstructionError(t, err)
	if e.Kind != fwmanager.NotFound {
		t.Fatalf("want NotFound, got %d", e.Kind)
	}
	if strings.Contains(e.Detail, "workflow not found") || strings.Contains(e.Detail, "gtdapp:construction") {
		t.Fatalf("Temporal internals leaked to the client: %q", e.Detail)
	}
	if !strings.Contains(e.Detail, "construction has not started") {
		t.Fatalf("want a user-altitude message, got %q", e.Detail)
	}
}

// QA 2026-07-19 (poll-404 wizard reset twin): a namespace-not-found from a wrong/foreign
// Temporal backend must NOT map to the authoritative "construction has not started"
// NotFound — the polled console trusts that 404 and drops its session view. It stays an
// Infrastructure fault the client tolerates.
func Test_GetSessionState_NamespaceNotFound_IsInfrastructureNot404(t *testing.T) {
	fc := &fakeQueryClient{queryErr: serviceerror.NewNamespaceNotFound("default")}
	m := newTestConstructionManager(fc)

	_, err := m.GetSessionState(testCtx(), ProjectID("gtdapp"), nil)
	e := asConstructionError(t, err)
	if e.Kind == fwmanager.NotFound {
		t.Fatalf("namespace-not-found (wrong Temporal backend) must not claim construction absence, got NotFound %q", e.Detail)
	}
	if e.Kind != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure, got %d (detail %q)", e.Kind, e.Detail)
	}
}

// =============================================================================
// constructionManager workflow unit tests over the Temporal in-memory test
// environment (testsuite.WorkflowTestSuite). The three Engines (handOffEngine,
// interventionEngine, reviewEngine) and the four ResourceAccess ports
// (projectStateAccess, agenticJobAccess, artifactAccess, workerAccess)
// are constructed as interface test doubles (fakes) — the not-yet-built deps are
// driven against their FROZEN CONTRACTS as the Manager-declared consumer interfaces
// (deps.go). These run with no Docker and no dev server (the real-infrastructure
// exercise is a later integration activity).
//
// They assert the UC3 spine (cast → dispatch → submit/observe → stage → review →
// recordChangeReviewed → recordActivityExited), the no-eligible-activity quiet
// tick, the pause branch (NCUC2), the operator-override branch, and the key
// error/variance/conflict paths — per [[the-method-testing]] (black-box where the
// observable is the workflow result/recorded side effects).
// =============================================================================

// ---- Fakes (interface test doubles for the downstream deps) -----------------

// fakeProjectState records the additive Phase-3 transition calls + serves a
// scripted head-state. It satisfies the Manager's ProjectStateAccess consumer
// interface (deps.go) — the read + the three additive transition verbs.
type fakeProjectState struct {
	mu sync.Mutex

	project  projectstate.Project
	notFound bool

	// conflictFirst, when >0, returns fwra.Conflict on the first N transition
	// calls (across all transition verbs) before succeeding — drives the §6.5
	// re-read→re-apply loop.
	conflictFirst int

	reviewed  []string
	exited    []exitCall
	failed    []failCall
	paused    []string
	phaseDone []phaseCompletedCall

	version projectstate.Version
}

// phaseCompletedCall records one RecordPhaseCompleted transition (the gate's durable
// per-phase completion record). The gate tests assert on it via phaseCompleted.
type phaseCompletedCall struct {
	activityID string
	phase      string
}

// phaseCompleted reports whether RecordPhaseCompleted landed for (activityID, phase).
func (f *fakeProjectState) phaseCompleted(activityID, phase string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.phaseDone {
		if c.activityID == activityID && c.phase == phase {
			return true
		}
	}
	return false
}

type exitCall struct {
	activityID string
	outcome    projectstate.ActivityOutcome
}

type failCall struct {
	activityID string
	reason     projectstate.FailureReason
	detail     string
}

func (f *fakeProjectState) ReadProject(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notFound {
		return projectstate.Project{}, fwra.New(fwra.NotFound, "no row yet")
	}
	return f.project, nil
}

func (f *fakeProjectState) ReadProjectVersion(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notFound {
		return 0, fwra.New(fwra.NotFound, "no row yet")
	}
	return f.project.Version, nil
}

func (f *fakeProjectState) bump() projectstate.Version {
	f.version++
	f.project.Version = f.version
	return f.version
}

func (f *fakeProjectState) maybeConflict() error {
	if f.conflictFirst > 0 {
		f.conflictFirst--
		// Advance the served head version so the re-read sees a newer value.
		f.version++
		f.project.Version = f.version
		return fwra.New(fwra.Conflict, "stale version")
	}
	return nil
}

func (f *fakeProjectState) RecordChangeReviewed(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.reviewed = append(f.reviewed, activityID)
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityExited(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, outcome projectstate.ActivityOutcome, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.exited = append(f.exited, exitCall{activityID: activityID, outcome: outcome})
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityFailed(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, reason projectstate.FailureReason, detail string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.failed = append(f.failed, failCall{activityID: activityID, reason: reason, detail: detail})
	return f.bump(), nil
}

func (f *fakeProjectState) RecordOperatorPaused(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, reason string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.paused = append(f.paused, reason)
	return f.bump(), nil
}

func (f *fakeProjectState) RecordReviewPolicy(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.ReviewPolicy, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	return f.bump(), nil
}

func (f *fakeProjectState) RecordPhaseStarted(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ActivityMethodPhase, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	return f.bump(), nil
}

func (f *fakeProjectState) RecordPhaseCompleted(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, phase projectstate.ActivityMethodPhase, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.phaseDone = append(f.phaseDone, phaseCompletedCall{activityID: activityID, phase: phase.String()})
	return f.bump(), nil
}

// ---- gitActivityStatusAccess seam (per-activity construction head-state) ----
// The gate tests wire GitStatus so gitOn is true (the LOCAL/dry-run profile — no PR
// rail), which is what drives the phase-started/completed head-state records. These
// are no-op bumps; only RecordPhaseCompleted (a construction-transition verb, above)
// carries the assertion the gate tests read.

func (f *fakeProjectState) RecordActivityBranchOpened(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _, _, _, _, _ string, _ bool, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityCIObserved(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.CICheckState, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityArchApproved(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityMerged(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityStarted(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityCompleted(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordServiceContractProduced(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ServiceContract, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	return f.bump(), nil
}

func (f *fakeProjectState) RecordPhaseArtifactProduced(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ string, _ projectstate.PhaseArtifactPayload, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	return f.bump(), nil
}

var _ projectstate.GitActivityStatusAccess = (*fakeProjectState)(nil)

// fakeConstructionTransition widens fakeProjectState onto the FULL
// projectstate.ConstructionTransitionAccess contract (B8): the nine write verbs are
// inherited verbatim (byte-identical signatures); ReadProject gets the extra `cred`
// parameter the fake's own 2-arg ReadProject never carried, so this adapter widens the
// signature by ignoring cred and delegating.
type fakeConstructionTransition struct {
	*fakeProjectState
}

func (f fakeConstructionTransition) ReadProject(rc fwra.Context, projectID projectstate.ProjectID, _ projectstate.RepoCredential) (projectstate.Project, error) {
	return f.fakeProjectState.ReadProject(rc, projectID)
}

var _ projectstate.ConstructionTransitionAccess = fakeConstructionTransition{}

// fakeFullProjectState widens fakeProjectState onto the FULL projectstate.ProjectStateAccess
// contract so the test env can (a) register the GENERATED ProjectStateReadProjectVersion
// activity (B8: migrated off the custom ReadProjectVersionActivity) and (b) serve as the
// BASE of the real projectstate.NewDesignSessionAccess wrapper backing the GENERATED
// designSessionAccess.readProjectOnBranch activity (B8 follow-up: the pump's
// whole-aggregate read; branch "" reads main via ReadProject — the generated
// ProjectStateAccess contract requires ReadProjectOnBranch("") to behave exactly as
// ReadProject, C2 fold code-health-phase-a). ReadProject/ReadProjectVersion are inherited
// verbatim (byte-identical signatures); the remaining eight ops are never exercised by
// these workflow tests — each is an inert stub, matching the stubRail precedent
// (gitforward_test.go) for satisfying an unused portion of a wide contract.
type fakeFullProjectState struct {
	*fakeProjectState
}

func (f fakeFullProjectState) ReadProjectOnBranch(rc fwra.Context, projectID projectstate.ProjectID, _ string) (projectstate.Project, error) {
	return f.ReadProject(rc, projectID)
}

func (fakeFullProjectState) StageArtifactForReviewOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactModel, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) RejectArtifactOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) WithdrawArtifactOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) RejectArtifactOnBranchWithComments(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, int64, []projectstate.ReviewComment, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) SetReviewCommentStatusOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) SeedReviewCommentsOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, int64, []projectstate.ReviewComment, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) ReconcileBranchFromMain(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) AcknowledgeStaleBasis(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) AdvancePhase(fwra.Context, projectstate.ProjectID, projectstate.Version) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) CommitArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) CreateProject(fwra.Context, projectstate.ProjectID, projectstate.OwnerScope, string) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) ListProjects(fwra.Context, projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	return nil, nil
}

func (fakeFullProjectState) RejectArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) SetOperatingModel(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.OperatingModel) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) SetResearchInput(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ResearchInput) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) StageArtifactForReview(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactModel) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) WithdrawArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string) (projectstate.Version, error) {
	return 0, nil
}

var _ projectstate.ProjectStateAccess = fakeFullProjectState{}

// contractPipelinePhase maps the Manager-neutral PipelinePhase the fakes are scripted
// with back onto the contract agenticjob.PipelinePhase the GENERATED observe
// activity returns (reverse of managerPipelinePhase) — so the test literals stay written
// in the Manager vocabulary while the fakes honor the contract interface.
func contractPipelinePhase(p PipelinePhase) agenticjob.PipelinePhase {
	switch p {
	case PipelinePending:
		return agenticjob.PhasePending
	case PipelineRunning:
		return agenticjob.PhaseRunning
	case PipelineSucceeded:
		return agenticjob.PhaseSucceeded
	case PipelineFailed:
		return agenticjob.PhaseFailed
	case PipelineCancelled:
		return agenticjob.PhaseCancelled
	default:
		return agenticjob.PhasePending
	}
}

// fakePipeline serves a scripted terminal observation after one running poll. It honors
// the FROZEN agenticjob.AgenticJobAccess contract (the GENERATED
// pipeline Activities are backed by it); the workflow reaches it through the generated
// invoker surface.
type fakePipeline struct {
	mu sync.Mutex

	phase     PipelinePhase // terminal phase to serve
	diag      string
	submitted []agenticjob.PipelineSpec
	cancelled []agenticjob.PipelineHandle
	polls     int

	// episode, when set, rides EVERY observation — the local executor's mined
	// EpisodeSummary (SP1 capture-seam). nil mirrors the GitHub-Actions arm / a lost run.
	episode *agenticjob.EpisodeSummary
	// runURL, when set, marks the observation as coming from the REMOTE venue (the
	// GitHub-Actions arm stamps the run's html URL; the local executor never does).
	runURL string
}

func (p *fakePipeline) SubmitAgenticJob(_ fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submitted = append(p.submitted, spec)
	return agenticjob.PipelineHandle("wf-" + string(spec.ActivityID)), nil
}

func (p *fakePipeline) ObserveAgenticJob(_ fwra.Context, _ agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.polls++
	ph := p.phase
	if ph == PipelinePhaseUnknown {
		ph = PipelineSucceeded
	}
	return agenticjob.PipelineObservation{
		Phase:      contractPipelinePhase(ph),
		Diagnostic: p.diag,
		RunURL:     p.runURL,
		Episode:    p.episode,
	}, nil
}

func (p *fakePipeline) CancelAgenticJob(_ fwra.Context, handle agenticjob.PipelineHandle) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelled = append(p.cancelled, handle)
	return nil
}

var _ agenticjob.AgenticJobAccess = (*fakePipeline)(nil)

// fakeIntervention returns scripted directives/plans. Satisfies the PUBLISHED
// intervention.InterventionEngine directly (Task 6 — no Manager-local seam); only
// DecideOnVariance/ApplyPausePolicy are exercised by these tests — DecideOnHealth and
// DecideOnSettlementFailure (operations'/billing's verbs) are unused stubs.
//
// directive's zero value is intervention.VarianceRetry (the published enum's own
// zero, unlike the retired local varianceDirective's directiveUnknown=0 sentinel) —
// DecideOnVariance returns it as-is with no special-casing; every test that leaves
// directive unset (&fakeIntervention{}) never actually calls DecideOnVariance (quiet
// ticks / drained network / pause branch / quiet sweep), so this is unobserved.
type fakeIntervention struct {
	directive intervention.VarianceDirective
	plan      intervention.PausePlan
}

func (i *fakeIntervention) DecideOnVariance(_ fweng.Context, _ intervention.ConstructionVariance) (intervention.VarianceDirective, error) {
	return i.directive, nil
}

func (i *fakeIntervention) ApplyPausePolicy(_ fweng.Context, _ intervention.PauseRequestContext) (intervention.PausePlan, error) {
	return i.plan, nil
}

func (i *fakeIntervention) DecideOnHealth(_ fweng.Context, _ intervention.HealthChange) (intervention.HealthDirective, error) {
	return intervention.HealthRetry, nil
}

func (i *fakeIntervention) DecideOnSettlementFailure(_ fweng.Context, _ intervention.SettlementFailure) (intervention.SettlementFailureDirective, error) {
	return intervention.SettlementRetry, nil
}

var _ intervention.InterventionEngine = (*fakeIntervention)(nil)

// fakeReview returns a scripted reviewer set. Satisfies the PUBLISHED
// review.ReviewEngine directly (Task 6 — no Manager-local seam). The scripted `set`
// is the ENGINE's own review.ReviewSet — reviewSetFromEngine (adapters.go) bridges it
// onto the façade ReviewSet the same way production does.
type fakeReview struct {
	set review.ReviewSet
}

func (r *fakeReview) ProposeReviews(_ fweng.Context, _ review.ReviewChange, _ string, _ string, _ string, _ []string) (review.ReviewSet, error) {
	return r.set, nil
}

var _ review.ReviewEngine = (*fakeReview)(nil)

// ---- helpers ----------------------------------------------------------------

// registerGenPipeline registers the GENERATED pipeline Activities (backed by the contract
// pipeline fake) under their generated registered names — the names the generated invoker
// surface (wf.Acts.Pipeline*) dispatches by. The workflow reaches the pipeline only through
// those invokers now.
func registerGenPipeline(env *testsuite.TestWorkflowEnvironment, pipe agenticjob.AgenticJobAccess) {
	acts := &genActivities{Pipeline: pipe}
	env.RegisterActivityWithOptions(acts.PipelineSubmitAgenticJob, activity.RegisterOptions{Name: "agenticJobAccess.submitAgenticJob"})
	env.RegisterActivityWithOptions(acts.PipelineObserveAgenticJob, activity.RegisterOptions{Name: "agenticJobAccess.observeAgenticJob"})
	env.RegisterActivityWithOptions(acts.PipelineCancelAgenticJob, activity.RegisterOptions{Name: "agenticJobAccess.cancelAgenticJob"})
}

// registerGenProjectStateVersion registers the GENERATED ProjectStateReadProjectVersion
// activity (B8: migrated off the custom ReadProjectVersionActivity) under its generated
// registered name, backed by ps widened onto the full ProjectStateAccess contract
// (fakeFullProjectState — only ReadProjectVersion is ever exercised through this seam).
func registerGenProjectStateVersion(env *testsuite.TestWorkflowEnvironment, ps *fakeProjectState) {
	acts := &genActivities{ProjectState: fakeFullProjectState{ps}}
	env.RegisterActivityWithOptions(acts.ProjectStateReadProjectVersion, activity.RegisterOptions{Name: "projectStateAccess.readProjectVersion"})
}

// registerGenConstructionTransition registers the GENERATED constructionTransitionAccess
// Record* activities (B8: migrated off the custom Record*Activity methods,
// activities_custom.go) under their generated registered names, backed by ps widened onto
// the full ConstructionTransitionAccess contract (fakeConstructionTransition).
func registerGenConstructionTransition(env *testsuite.TestWorkflowEnvironment, ps *fakeProjectState) {
	acts := &genActivities{ConstructionTransition: fakeConstructionTransition{ps}}
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordChangeReviewed, activity.RegisterOptions{Name: "constructionTransitionAccess.recordChangeReviewed"})
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordActivityExited, activity.RegisterOptions{Name: "constructionTransitionAccess.recordActivityExited"})
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordActivityFailed, activity.RegisterOptions{Name: "constructionTransitionAccess.recordActivityFailed"})
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordOperatorPaused, activity.RegisterOptions{Name: "constructionTransitionAccess.recordOperatorPaused"})
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordPhaseStarted, activity.RegisterOptions{Name: "constructionTransitionAccess.recordPhaseStarted"})
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordPhaseCompleted, activity.RegisterOptions{Name: "constructionTransitionAccess.recordPhaseCompleted"})
}

// registerGenGitStatus registers the GENERATED gitActivityStatusAccess Record* activities
// (B8: migrated off the custom RecordActivity*Activity methods, gitactivities.go) under
// their generated registered names, backed by gs — either ps (already the full
// projectstate.GitActivityStatusAccess contract — no widening needed) or, for the
// git-forward tests, the separate stubGitStatus store (gitforward_test.go).
func registerGenGitStatus(env *testsuite.TestWorkflowEnvironment, gs projectstate.GitActivityStatusAccess) {
	acts := &genActivities{GitStatus: gs}
	env.RegisterActivityWithOptions(acts.GitStatusRecordActivityBranchOpened, activity.RegisterOptions{Name: "gitActivityStatusAccess.recordActivityBranchOpened"})
	env.RegisterActivityWithOptions(acts.GitStatusRecordActivityCIObserved, activity.RegisterOptions{Name: "gitActivityStatusAccess.recordActivityCIObserved"})
	env.RegisterActivityWithOptions(acts.GitStatusRecordActivityArchApproved, activity.RegisterOptions{Name: "gitActivityStatusAccess.recordActivityArchApproved"})
	env.RegisterActivityWithOptions(acts.GitStatusRecordActivityMerged, activity.RegisterOptions{Name: "gitActivityStatusAccess.recordActivityMerged"})
	env.RegisterActivityWithOptions(acts.GitStatusRecordActivityStarted, activity.RegisterOptions{Name: "gitActivityStatusAccess.recordActivityStarted"})
	env.RegisterActivityWithOptions(acts.GitStatusRecordActivityCompleted, activity.RegisterOptions{Name: "gitActivityStatusAccess.recordActivityCompleted"})
}

// registerGenDesignSessionRead registers the GENERATED designSessionAccess
// readProjectOnBranch activity (B8 follow-up: the pump's whole-aggregate read — the
// former custom ReadProjectActivity) under its generated registered name. It is backed
// by the REAL projectstate.NewDesignSessionAccess wrapper over ps (widened onto the full
// base contract via fakeFullProjectState), so the workflow's branch "" read exercises
// the production empty-branch→base.ReadProject fallback chain (designsession.go) and
// the shared ProjectEnvelope encode/decode round trip end-to-end.
func registerGenDesignSessionRead(env *testsuite.TestWorkflowEnvironment, ps *fakeProjectState) {
	acts := &genActivities{DesignSession: projectstate.NewDesignSessionAccess(fakeFullProjectState{ps})}
	env.RegisterActivityWithOptions(acts.DesignSessionReadProjectOnBranch, activity.RegisterOptions{Name: "designSessionAccess.readProjectOnBranch"})
}

// fakeEpisodes is the episodeAccess test double: it RECORDS every appended record and
// counts every attempt, so a test can assert both what was written and how many times the
// append was retried. failN>0 fails the first failN attempts; failAlways fails every one.
// It also HONOURS the call context, so an append handed an already-cancelled context is
// caught by the test rather than silently blessed (the production realisations ignore it).
type fakeEpisodes struct {
	mu         sync.Mutex
	appended   []episode.EpisodeRecord
	attempts   int
	failN      int
	failAlways bool

	// listRecords/listErr back ListEpisodes (Task 9 facet reads); lastQuery
	// captures the query the manager actually issued, for TargetRef-semantics
	// assertions.
	listRecords []episode.EpisodeRecord
	listErr     error
	lastQuery   episode.EpisodeQuery

	// traceEvents/traceErr back ReadTraceEvents; lastTrace* captures the args
	// the manager actually issued.
	traceEvents        []json.RawMessage
	traceErr           error
	lastTraceProjectID episode.ProjectID
	lastTraceEpisodeID string
}

func (f *fakeEpisodes) AppendEpisode(rc fwra.Context, _ episode.ProjectID, record episode.EpisodeRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if rc.Context != nil && rc.Err() != nil {
		return fwra.New(fwra.Infrastructure, "append called with a dead context: "+rc.Err().Error())
	}
	if f.failAlways || f.attempts <= f.failN {
		return fwra.New(fwra.Infrastructure, "episode ledger unavailable")
	}
	f.appended = append(f.appended, record)
	return nil
}

func (f *fakeEpisodes) ListEpisodes(_ fwra.Context, query episode.EpisodeQuery) ([]episode.EpisodeRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuery = query
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listRecords, nil
}

func (f *fakeEpisodes) ReadTraceEvents(_ fwra.Context, projectID episode.ProjectID, episodeID string) ([]json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastTraceProjectID = projectID
	f.lastTraceEpisodeID = episodeID
	if f.traceErr != nil {
		return nil, f.traceErr
	}
	return f.traceEvents, nil
}

func (f *fakeEpisodes) records() []episode.EpisodeRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]episode.EpisodeRecord(nil), f.appended...)
}

func (f *fakeEpisodes) attemptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts
}

var _ episode.EpisodeAccess = (*fakeEpisodes)(nil)

// registerGenEpisodes registers the GENERATED episodeAccess Activities under their
// registered names. The production Worker registers them for every construction
// execution, so the test env must too — otherwise the capture seam's append would fail
// as unregistered on EVERY workflow test rather than exercising the real path.
// eps is variadic purely so the ~dozen existing register* call sites stay untouched: a
// test that cares about episodes passes its recorder, every other test gets a silent one.
func registerGenEpisodes(env *testsuite.TestWorkflowEnvironment, eps []*fakeEpisodes) {
	var acc episode.EpisodeAccess = &fakeEpisodes{}
	if len(eps) > 0 && eps[0] != nil {
		acc = eps[0]
	}
	acts := &genActivities{Episodes: acc}
	env.RegisterActivityWithOptions(acts.EpisodesAppendEpisode, activity.RegisterOptions{Name: "episodeAccess.appendEpisode"})
}

// registerConstruct registers the per-activity child workflow + its Activities — ALL
// generated (B8 + follow-up): the pipeline/designSession-read/projectState-version/
// constructionTransition/gitStatus surfaces, each backed by the fakes.
func registerConstruct(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps *fakeProjectState, pipe agenticjob.AgenticJobAccess, eps ...*fakeEpisodes) {
	env.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: executionKindConstructActivity})
	registerGenPipeline(env, pipe)
	registerGenEpisodes(env, eps)
	registerGenDesignSessionRead(env, ps)
	registerGenProjectStateVersion(env, ps)
	registerGenConstructionTransition(env, ps)
	// Phase-gate + per-activity construction-status records (fire only when gitOn;
	// the gate tests wire GitStatus so these must be registered).
	registerGenGitStatus(env, ps)
}

func registerPump(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps *fakeProjectState, pipe agenticjob.AgenticJobAccess, eps ...*fakeEpisodes) {
	env.RegisterWorkflowWithOptions(wf.PumpNextActivityWorkflow, workflow.RegisterOptions{Name: executionKindPump})
	env.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: executionKindConstructActivity})
	registerGenEpisodes(env, eps)
	// The pump now waits for child COMPLETION (self-cascade), so the per-activity
	// child runs end-to-end and ALL its activities must be registered.
	registerGenPipeline(env, pipe)
	registerGenDesignSessionRead(env, ps)
	registerGenProjectStateVersion(env, ps)
	registerGenConstructionTransition(env, ps)
}

func registerSupervision(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps *fakeProjectState, pipe agenticjob.AgenticJobAccess, eps ...*fakeEpisodes) {
	env.RegisterWorkflowWithOptions(wf.ProjectSupervisionWorkflow, workflow.RegisterOptions{Name: executionKindProjectSupervision})
	registerGenPipeline(env, pipe)
	registerGenEpisodes(env, eps)
	registerGenDesignSessionRead(env, ps)
	registerGenProjectStateVersion(env, ps)
	registerGenConstructionTransition(env, ps)
}

func registerReplanSweep(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps *fakeProjectState) {
	env.RegisterWorkflowWithOptions(wf.ReplanSweepWorkflow, workflow.RegisterOptions{Name: executionKindReplanSweep})
	registerGenDesignSessionRead(env, ps)
}

// fakeProjectLister widens fakeFullProjectState with a SCRIPTED ListProjects — the
// one surface PumpSweepWorkflow's enumeration depends on. Every other method falls
// through to fakeFullProjectState's stubs (never exercised by the sweep itself).
type fakeProjectLister struct {
	fakeFullProjectState
	summaries []projectstate.ProjectSummary
}

func (f fakeProjectLister) ListProjects(fwra.Context, projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	return f.summaries, nil
}

var _ projectstate.ProjectStateAccess = fakeProjectLister{}

// registerPumpSweep registers PumpSweepWorkflow + projectStateAccess.listProjects
// (backed by lister) alongside everything PumpNextActivityWorkflow needs for its
// per-project child dispatch (registerPump's own set) — the sweep starts that exact
// workflow as an ABANDON-policy child per eligible project.
func registerPumpSweep(env *testsuite.TestWorkflowEnvironment, wf *workflows, lister fakeProjectLister, ps *fakeProjectState, pipe agenticjob.AgenticJobAccess, eps ...*fakeEpisodes) {
	env.RegisterWorkflowWithOptions(wf.PumpSweepWorkflow, workflow.RegisterOptions{Name: executionKindPumpSweep})
	acts := &genActivities{ProjectState: lister}
	env.RegisterActivityWithOptions(acts.ProjectStateListProjects, activity.RegisterOptions{Name: "projectStateAccess.listProjects"})
	registerPump(env, wf, ps, pipe, eps...)
}

func sampleActivity() constructionActivity {
	return constructionActivity{
		ActivityID:  "C-XYZ",
		Kind:        activityKindConstruction,
		ComponentID: "comp-1",
		Layer:       "engine",
		Phases:      projectstate.ProfileFor(projectstate.ActivityTypeService, 0).PhaseIDs(),
	}
}

// ---- Tests: per-activity spine (ConstructActivityWorkflow) ------------------

// The happy-path UC3 spine: cast → dispatch → submit/observe(succeeded) → stage →
// review(empty set) → recordChangeReviewed → recordActivityExited(Completed).
func Test_Construct_HappyPath_RecordsReviewedAndExited(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.reviewed) != 1 || ps.reviewed[0] != "C-XYZ" {
		t.Fatalf("want one recordChangeReviewed(C-XYZ), got %v", ps.reviewed)
	}
	if len(ps.exited) != 1 || ps.exited[0].activityID != "C-XYZ" || ps.exited[0].outcome != projectstate.ActivityOutcomeCompleted {
		t.Fatalf("want one recordActivityExited(C-XYZ, Completed), got %v", ps.exited)
	}
	// The App-A phase-walk dispatches one pipeline per phase (Requirements →
	// Detailed Design → Test Plan → Construction → Integration).
	if len(pipe.submitted) != len(sampleActivity().Phases) {
		t.Fatalf("want %d pipeline submits (one per App-A phase), got %d", len(sampleActivity().Phases), len(pipe.submitted))
	}
}

// Test_Construct_VenueSwitch_TargetsProjectRepo pins the gh-mode venue switch (B5):
// when the per-project Repo resolver resolves a RepoRef, every construction pipeline
// dispatch carries the DECODED {Owner,Name} TargetRepo + the construct workflow file
// so the agentic construction job runs in the PROJECT's own repo, not the central repo.
func Test_Construct_VenueSwitch_TargetsProjectRepo(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		// Repo resolves — the venue switch fires INDEPENDENTLY of the PR rail
		// (RailEnabled/GitStatus unwired here, so the git-forward slice stays dormant).
		Repo: func(_ ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|acme/gtdapp"), true
		},
	})
	registerConstruct(env, wf, ps, pipe)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(pipe.submitted) == 0 {
		t.Fatal("expected at least one pipeline submit")
	}
	for i, spec := range pipe.submitted {
		if spec.TargetRepo.Owner != "acme" || spec.TargetRepo.Name != "gtdapp" {
			t.Fatalf("submit[%d]: want TargetRepo{acme,gtdapp}, got %+v", i, spec.TargetRepo)
		}
		if spec.WorkflowFile != constructWorkflowFileName {
			t.Fatalf("submit[%d]: want WorkflowFile %q, got %q", i, constructWorkflowFileName, spec.WorkflowFile)
		}
	}
}

// Test_Construct_VenueSwitch_FallbackToCentralRepo pins the legacy fallback: when the
// Repo resolver is absent (nil) OR does not resolve, the dispatch carries a ZERO
// TargetRepo + empty WorkflowFile, so agenticJobAccess.resolveTarget falls
// back to the configured central construction repo (the pre-B5 behavior, preserved for
// unresolvable projects).
func Test_Construct_VenueSwitch_FallbackToCentralRepo(t *testing.T) {
	cases := []struct {
		name string
		repo func(ProjectID) (sourcecontrol.RepoRef, bool)
	}{
		{"resolver absent", nil},
		{"resolver misses", func(_ ProjectID) (sourcecontrol.RepoRef, bool) { return sourcecontrol.RepoRef(""), false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ts testsuite.WorkflowTestSuite
			env := ts.NewTestWorkflowEnvironment()

			ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
			pipe := &fakePipeline{phase: PipelineSucceeded}
			wf := newWorkflows(wfDeps{
				Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
				Review:       &fakeReview{},
				Repo:         tc.repo,
			})
			registerConstruct(env, wf, ps, pipe)

			env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
				ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
			})

			if !env.IsWorkflowCompleted() {
				t.Fatal("workflow did not complete")
			}
			if err := env.GetWorkflowError(); err != nil {
				t.Fatalf("workflow error: %v", err)
			}
			if len(pipe.submitted) == 0 {
				t.Fatal("expected at least one pipeline submit")
			}
			for i, spec := range pipe.submitted {
				if !agenticjob.RepoTargetIsZero(spec.TargetRepo) {
					t.Fatalf("submit[%d]: want ZERO TargetRepo (central-repo fallback), got %+v", i, spec.TargetRepo)
				}
				if spec.WorkflowFile != "" {
					t.Fatalf("submit[%d]: want empty WorkflowFile (central-repo fallback), got %q", i, spec.WorkflowFile)
				}
			}
		})
	}
}

// runPumpWith builds the fakePipeline-backed Temporal test environment, executes
// ConstructActivityWorkflow with the supplied activity, and returns the pipeline
// double so the caller can inspect pipe.submitted.
func runPumpWith(t *testing.T, act constructionActivity) *fakePipeline {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: ActivityID(act.ActivityID), Activity: act,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	return pipe
}

// Test_Construct_TestingPlanWalksThreePhases proves that a testing-plan activity
// (3 canonical phases) drives exactly 3 pipeline submissions, not the service 5.
func Test_Construct_TestingPlanWalksThreePhases(t *testing.T) {
	act := constructionActivity{
		ActivityID:  "N-STP",
		Kind:        activityKindConstruction,
		ComponentID: "system",
		Phases:      projectstate.ProfileFor(projectstate.ActivityTypeTesting, projectstate.TestVariantPlan).PhaseIDs(),
	}
	if len(act.Phases) != 3 {
		t.Fatalf("precondition: testing-plan phases = %d, want 3", len(act.Phases))
	}
	pipe := runPumpWith(t, act)
	if len(pipe.submitted) != 3 {
		t.Fatalf("submitted %d pipelines, want 3", len(pipe.submitted))
	}
}

// architectOnly skips dispatch + pipeline and awaits an operator override; a Skip
// override exits the activity with the operator-skip outcome and no worker dispatch.
// A failed pipeline → variance → DecideOnVariance(Takeover): the takeover re-dispatches;
// with the pipeline now succeeding the activity completes normally on the next loop. (The
// prior phase pipeline is already terminal at intervention time, so takeover abandons
// nothing — the LLM worker-cancel seam is retired under agentic-everywhere.)
func Test_Construct_PipelineFailed_Takeover_ThenCompletes(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1, Phase: 2}}
	// The pipeline fails on the first run, then a flippable fake makes it succeed.
	pipe := &flippablePipeline{first: PipelineFailed, rest: PipelineSucceeded}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceTakeover},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-PF", Activity: sampleActivity(),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.exited) != 1 || ps.exited[0].outcome != projectstate.ActivityOutcomeCompleted {
		t.Fatalf("want a completed exit after takeover+re-dispatch, got %v", ps.exited)
	}
}

// flippablePipeline serves `first` on the first terminal observation, then `rest`.
type flippablePipeline struct {
	mu        sync.Mutex
	first     PipelinePhase
	rest      PipelinePhase
	submits   int
	cancelled []agenticjob.PipelineHandle
}

func (p *flippablePipeline) SubmitAgenticJob(_ fwra.Context, _ agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submits++
	return agenticjob.PipelineHandle("wf"), nil
}

func (p *flippablePipeline) ObserveAgenticJob(_ fwra.Context, _ agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.submits <= 1 {
		return agenticjob.PipelineObservation{Phase: contractPipelinePhase(p.first), Diagnostic: "boom"}, nil
	}
	return agenticjob.PipelineObservation{Phase: contractPipelinePhase(p.rest)}, nil
}

func (p *flippablePipeline) CancelAgenticJob(_ fwra.Context, handle agenticjob.PipelineHandle) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelled = append(p.cancelled, handle)
	return nil
}

var _ agenticjob.AgenticJobAccess = (*flippablePipeline)(nil)

// The §6.5 Conflict discipline: a recordChangeReviewed that returns fwra.Conflict
// twice before succeeding drives the workflow-level re-read→re-apply loop; the
// activity still completes (reviewed + exited recorded).
func Test_Construct_ConflictOnRecord_ReReadReApply_Succeeds(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1, Phase: 2}, conflictFirst: 2}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-CONF", Activity: sampleActivity(),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.reviewed) != 1 {
		t.Fatalf("conflict loop must converge to exactly one recorded reviewed, got %v", ps.reviewed)
	}
	if len(ps.exited) != 1 {
		t.Fatalf("want one recorded exit after the conflict loop, got %v", ps.exited)
	}
}

// ---- Tests: pump (PumpNextActivityWorkflow) ---------------------------------

// No eligible activity ⇒ PumpResult{Dispatched:false} — a normal quiet tick, not
// an error (no NextEligibleActivity helper wired).
func Test_Pump_NoEligibleActivity_QuietTick(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
		NextEligibleActivity: nil,
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: ProjectID(ps.project.ID)})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump error: %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if res.Dispatched {
		t.Fatalf("want Dispatched:false on a quiet tick, got %+v", res)
	}
}

// A brand-new project (ReadProject NotFound) is also a quiet tick, not an error.
func Test_Pump_ProjectNotFound_QuietTick(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{notFound: true}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: ProjectID(uuid.NewString())})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump error: %v", err)
	}
	var res PumpResult
	_ = env.GetWorkflowResult(&res)
	if res.Dispatched {
		t.Fatal("a not-found project must be a quiet tick")
	}
}

// An eligible activity ⇒ the pump runs the per-activity child to COMPLETION, then
// SELF-CASCADES via ContinueAsNew (Task 3). The test env surfaces ContinueAsNew as a
// *workflow.ContinueAsNewError carrying the next pumpInput. The child's spine ran
// end-to-end (one reviewed + one completed exit recorded).
func Test_Pump_EligibleActivity_RunsChild_ThenContinueAsNew(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project) pumpSelection {
			return pumpSelection{Verdict: verdictDispatch, Activity: sampleActivity()}
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if !env.IsWorkflowCompleted() {
		t.Fatal("pump did not complete")
	}
	// A successful eligible dispatch self-cascades: the terminal "error" is a
	// ContinueAsNewError carrying the next tick's pumpInput (NOT a real failure).
	err := env.GetWorkflowError()
	var canErr *workflow.ContinueAsNewError
	if !errors.As(err, &canErr) {
		t.Fatalf("want a ContinueAsNewError (self-cascade), got %v", err)
	}
	// The child ran end-to-end exactly once.
	if len(ps.exited) != 1 || ps.exited[0].activityID != "C-XYZ" {
		t.Fatalf("want the child to have recorded one exit for C-XYZ, got %v", ps.exited)
	}
}

// An eligible dispatch surfaces THIS tick's synchronous dispatch decision via the
// queryPumpDispatch Query — the value ExecuteNextActivity returns to a scheduler-style
// caller WITHOUT awaiting the background self-cascade drain. Even though the pump
// self-cascades (ends this run in ContinueAsNew), its final per-run state carries the
// decided dispatch of the eligible activity.
func Test_Pump_EligibleActivity_SurfacesSyncDispatchDecision(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project) pumpSelection {
			return pumpSelection{Verdict: verdictDispatch, Activity: sampleActivity()}
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	enc, err := env.QueryWorkflow(queryPumpDispatch)
	if err != nil {
		t.Fatalf("query pump dispatch decision: %v", err)
	}
	var d pumpDispatch
	if err := enc.Get(&d); err != nil {
		t.Fatalf("decode pump dispatch decision: %v", err)
	}
	if !d.Decided {
		t.Fatalf("want a decided dispatch decision, got %+v", d)
	}
	if !d.Dispatched || d.ActivityID == nil || *d.ActivityID != "C-XYZ" {
		t.Fatalf("want Dispatched:true for C-XYZ, got %+v", d)
	}
}

// A drained (quiescent) tick surfaces a DECIDED, non-dispatching decision via the
// queryPumpDispatch Query — the {Dispatched:false} answer ExecuteNextActivity returns
// on a quiet tick.
func Test_Pump_DrainedNetwork_SurfacesQuiescentDecision(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project) pumpSelection {
			return pumpSelection{Verdict: verdictQuiescent}
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	enc, err := env.QueryWorkflow(queryPumpDispatch)
	if err != nil {
		t.Fatalf("query pump dispatch decision: %v", err)
	}
	var d pumpDispatch
	if err := enc.Get(&d); err != nil {
		t.Fatalf("decode pump dispatch decision: %v", err)
	}
	if !d.Decided {
		t.Fatalf("want a decided decision on a drained tick, got %+v", d)
	}
	if d.Dispatched || d.ActivityID != nil {
		t.Fatalf("want a quiescent decision (Dispatched:false, nil activity), got %+v", d)
	}
}

// A drained network (nextEligible returns false) ⇒ the pump goes QUIET WITHOUT
// ContinueAsNew (the cascade ends) — Dispatched:false, no error.
func Test_Pump_DrainedNetwork_QuietNoContinueAsNew(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project) pumpSelection {
			return pumpSelection{Verdict: verdictQuiescent} // network drained
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("drained pump must be a clean quiet tick, got %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if res.Dispatched {
		t.Fatalf("a drained network must go quiet (Dispatched:false), got %+v", res)
	}
}

// A blocked activity is recorded as a TERMINAL, app-visible failure — not a silent
// skip, and not a workflow error (a failed Temporal execution is invisible in the
// console; the head-state record IS the escalation).
func Test_Pump_BlockedActivity_RecordsTerminalFailure(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	const reason = `activity C-TLM names component "todo-list-managr", which is not in the committed systemDesign — amend the committed activityList`
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project) pumpSelection {
			return pumpSelection{
				Verdict:              verdictBlocked,
				BlockedActivityID:    "C-TLM",
				BlockedFailureReason: projectstate.ComponentUnresolved,
				BlockedReason:        reason,
			}
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if !env.IsWorkflowCompleted() {
		t.Fatal("pump did not complete")
	}
	// The cascade ENDS — no ContinueAsNew, and no error surfaced to Temporal.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a blocked activity must not fail the workflow, got %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if res.Dispatched {
		t.Fatalf("want Dispatched:false, got %+v", res)
	}
	// The durable, operator-visible record: the offending activity, the new terminal
	// reason, and the operator-facing detail carried through VERBATIM (it names both
	// the activity and the unresolvable component id — that is what a human reads off
	// the failed node).
	if len(ps.failed) != 1 {
		t.Fatalf("want exactly one failure record, got %v", ps.failed)
	}
	want := failCall{activityID: "C-TLM", reason: projectstate.ComponentUnresolved, detail: reason}
	if ps.failed[0] != want {
		t.Fatalf("failure record mismatch:\n got %+v\nwant %+v", ps.failed[0], want)
	}
	// Nothing was dispatched: no child recorded an exit.
	if len(ps.exited) != 0 {
		t.Fatalf("no child should have run, got %v", ps.exited)
	}
	// The façade's synchronous read sees a DECIDED, non-dispatching tick — the pump
	// must not look like it is still deciding.
	enc, err := env.QueryWorkflow(queryPumpDispatch)
	if err != nil {
		t.Fatalf("query pump dispatch decision: %v", err)
	}
	var d pumpDispatch
	if err := enc.Get(&d); err != nil {
		t.Fatalf("decode pump dispatch decision: %v", err)
	}
	if d != (pumpDispatch{Decided: true}) {
		t.Fatalf("want a decided non-dispatch decision, got %+v", d)
	}
}

// Test_Pump_DependencyUnresolved_RecordsDistinctFailureReason proves a dangling
// dependency id records DependencyUnresolved — a DIFFERENT FailureReason ordinal from
// ComponentUnresolved (Test_Pump_BlockedActivity_RecordsTerminalFailure above) and from
// DependencyCycle (below) — asserted on the recorded failure record itself, not just a
// log line. Mirrors Test_Pump_BlockedActivity_RecordsTerminalFailure's shape, wired
// through nextEligibleActivity's real dependency-defect classification instead of a
// hand-built pumpSelection, so it also exercises resolveDependencySatisfied end to end.
func Test_Pump_DependencyUnresolved_RecordsDistinctFailureReason(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention:         &fakeIntervention{},
		Review:               &fakeReview{},
		NextEligibleActivity: nextEligibleActivity,
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "N-DOC", DependsOn: []string{"M-GHOST"}},
			},
			nil,
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "N-DOC", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
	}
	ps.project.Network = proj.Network
	ps.project.ActivityList = proj.ActivityList
	ps.project.SystemDesign = proj.SystemDesign

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a blocked activity must not fail the workflow, got %v", err)
	}
	if len(ps.failed) != 1 {
		t.Fatalf("want exactly one failure record, got %v", ps.failed)
	}
	got := ps.failed[0]
	if got.activityID != "N-DOC" {
		t.Fatalf("want failure recorded against N-DOC, got %+v", got)
	}
	if got.reason != projectstate.DependencyUnresolved {
		t.Fatalf("want DependencyUnresolved, got %v", got.reason)
	}
	if !strings.Contains(got.detail, "M-GHOST") {
		t.Fatalf("failure detail must name the dangling id, got %q", got.detail)
	}
}

// Test_Pump_DependencyCycle_RecordsDistinctFailureReason proves a milestone dependency
// cycle records DependencyCycle — distinct from both ComponentUnresolved and
// DependencyUnresolved — asserted on the recorded failure record.
func Test_Pump_DependencyCycle_RecordsDistinctFailureReason(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention:         &fakeIntervention{},
		Review:               &fakeReview{},
		NextEligibleActivity: nextEligibleActivity,
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "N-DOC", DependsOn: []string{"M-X"}},
			},
			[]projectstate.NetworkMilestone{
				{ID: "M-X", Name: "M-X", DependsOn: []string{"M-Y"}},
				{ID: "M-Y", Name: "M-Y", DependsOn: []string{"M-X"}},
			},
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "N-DOC", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
	}
	ps.project.Network = proj.Network
	ps.project.ActivityList = proj.ActivityList
	ps.project.SystemDesign = proj.SystemDesign

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a blocked activity must not fail the workflow, got %v", err)
	}
	if len(ps.failed) != 1 {
		t.Fatalf("want exactly one failure record, got %v", ps.failed)
	}
	got := ps.failed[0]
	if got.activityID != "N-DOC" {
		t.Fatalf("want failure recorded against N-DOC, got %+v", got)
	}
	if got.reason != projectstate.DependencyCycle {
		t.Fatalf("want DependencyCycle, got %v", got.reason)
	}
	if !strings.Contains(got.detail, "cycle") {
		t.Fatalf("failure detail must call out the cycle, got %q", got.detail)
	}
}

// A pause Signal delivered to the (cascading) pump halts it BEFORE any dispatch: the
// pump goes quiet WITHOUT ContinueAsNew and WITHOUT starting a child, even though an
// activity is eligible. The resume path re-triggers the pump.
func Test_Pump_PauseSignal_HaltsCascade_NoDispatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project) pumpSelection {
			return pumpSelection{Verdict: verdictDispatch, Activity: sampleActivity()} // an activity IS eligible — but the pause wins
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	// Deliver the pause Signal so it is already queued when the pump checks (the pump's
	// non-blocking ReceiveAsync observes it at the top, before any dispatch).
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, operatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, 0)

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("paused pump must be a clean quiet tick (no ContinueAsNew), got %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if res.Dispatched {
		t.Fatalf("a paused pump must NOT dispatch, got %+v", res)
	}
	// The child's spine never ran: nothing recorded exited.
	if len(ps.exited) != 0 {
		t.Fatalf("a paused pump must not run any activity, got exits %v", ps.exited)
	}
}

// ---- Tests: pause branch (ProjectSupervisionWorkflow / NCUC2) ---------------

// The operator-pause branch: applyPausePolicy returns a plan naming a pipeline to
// cancel + RecordPaused; the Manager EXECUTES the pipeline cancel + recordOperatorPaused.
// (The LLM worker-cancel abandon step is retired under agentic-everywhere; the in-flight
// dispatch is the GH-Actions pipeline, cancelled via the pause plan's PipelinesToCancel.)
func Test_Pause_AppliesPolicy_CancelsPipeline_RecordsPaused(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 2, Phase: 2}}
	pipe := &fakePipeline{}
	wf := newWorkflows(wfDeps{
		Review:       &fakeReview{},
		Intervention: &fakeIntervention{plan: intervention.PausePlan{PipelinesToCancel: []intervention.PipelineRef{"wf-C-1"}, RecordPaused: true}},
	})
	registerSupervision(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, operatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, time.Millisecond)

	env.ExecuteWorkflow(executionKindProjectSupervision, projectSupervisionInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("supervision error: %v", err)
	}
	if len(pipe.cancelled) != 1 {
		t.Fatalf("want one pipeline cancel from the pause plan, got %d", len(pipe.cancelled))
	}
	if len(ps.paused) != 1 || ps.paused[0] != "operator halt" {
		t.Fatalf("want one recordOperatorPaused(operator halt), got %v", ps.paused)
	}
}

// Test_Pause_RealInterventionEngine_PolicyThreaded_ApplyPausePolicySucceeds is the
// seam-cleanup Task 6 regression: runPauseBranch (signals.go) now threads the Manager's
// configured InterventionPolicy into PauseRequestContext.Policy. Against the REAL engine
// (intervention.NewInterventionEngine(), the production wiring — cmd/server/main.gen.go
// via WorkerManifest, workermanifest.go) this is load-bearing, not cosmetic:
// ApplyPausePolicy dispatches on ctx.Policy.Mode (strategy.go strategyFor), and
// InterventionModeUnknown (the zero value) has NO registered strategy — see the
// companion negative test below for the failure that zero value produces. The retired
// pauseRequestContext adapter never populated Policy at all. This test drives the real
// engine with the Manager's actual configured policy (constructionInterventionPolicy,
// the same builder WorkerManifest uses) and asserts the pause branch completes: a real
// PausePlan, not a guaranteed "unknown policy mode" error.
func Test_Pause_RealInterventionEngine_PolicyThreaded_ApplyPausePolicySucceeds(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 2, Phase: 2}}
	pipe := &fakePipeline{}
	// The REAL engine (not fakeIntervention) + the Manager's real config-derived policy
	// builder — the exact production wiring, so a regression that drops Policy threading
	// in runPauseBranch would fail THIS test with "unknown policy mode", not silently pass.
	wf := newWorkflows(wfDeps{
		Review:             &fakeReview{},
		Intervention:       intervention.NewInterventionEngine(),
		InterventionPolicy: constructionInterventionPolicy(""), // default: Tiered, RetryBudget 2
	})
	registerSupervision(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, operatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, time.Millisecond)

	env.ExecuteWorkflow(executionKindProjectSupervision, projectSupervisionInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("real-engine pause branch must succeed once Policy is threaded, got %v", err)
	}
	if len(ps.paused) != 1 || ps.paused[0] != "operator halt" {
		t.Fatalf("want one recordOperatorPaused(operator halt) from the real engine's PausePlan, got %v", ps.paused)
	}
}

// Test_ApplyPausePolicy_ZeroValuePolicy_IsTheOldBug pins the shape of the latent
// production defect the seam-cleanup Task 6 rewrite fixed (see task-6-report.md's
// "Correction + disclosure" section). The retired pauseRequestContext adapter never
// populated PauseRequestContext.Policy, so it always shipped the zero value
// (InterventionMode 0 == InterventionModeUnknown). Against the REAL engine that is not a
// no-op: ApplyPausePolicy resolves a strategy from ctx.Policy.Mode (strategy.go
// strategyFor) and InterventionModeUnknown has no registered strategy, so every operator
// pause request would have failed. This test documents WHY the Policy threading in
// runPauseBranch (signals.go) matters and pins the old-bug shape so it cannot silently
// return: if Policy threading is ever dropped again, Test_Pause_RealInterventionEngine_*
// above fails loudly with exactly this error.
func Test_ApplyPausePolicy_ZeroValuePolicy_IsTheOldBug(t *testing.T) {
	e := intervention.NewInterventionEngine()
	_, err := e.ApplyPausePolicy(fweng.Context{Context: context.Background()}, intervention.PauseRequestContext{
		ProjectID: "p1",
		Reason:    "operator halt",
		// Policy left at its zero value — exactly what the retired adapter sent.
	})
	if err == nil {
		t.Fatalf("expected the zero-value Policy (InterventionModeUnknown) to fail with \"unknown policy mode\", got nil error")
	}
	var ee *fweng.Error
	if !errors.As(err, &ee) {
		t.Fatalf("expected *fweng.Error, got %T: %v", err, err)
	}
	if ee.Kind != fweng.InvalidInput {
		t.Fatalf("error kind = %v, want %v (detail %q)", ee.Kind, fweng.InvalidInput, ee.Detail)
	}
	if ee.Detail != "unknown policy mode" {
		t.Fatalf("error detail = %q, want %q — pin the old-bug shape exactly", ee.Detail, "unknown policy mode")
	}
}

// ---- Tests: replan sweep (ReplanSweepWorkflow) ------------------------------

// A quiet sweep returns an empty result (no auto-replan).
func Test_ReplanSweep_QuietSweep_EmptyResult(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
	})
	registerReplanSweep(env, wf, ps)

	env.ExecuteWorkflow(executionKindReplanSweep, replanSweepInput{ProjectID: &pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("sweep error: %v", err)
	}
	var res ReplanSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode sweep result: %v", err)
	}
	if len(res.FlaggedVariances) != 0 {
		t.Fatalf("want an empty quiet sweep, got %v", res.FlaggedVariances)
	}
}

// ---- Tests: pump sweep (PumpSweepWorkflow, Task 7c) -------------------------

// No projects on the platform ⇒ an empty, quiet sweep.
func Test_PumpSweep_NoProjects_EmptyResult(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{Version: 1, Phase: 2}}
	lister := fakeProjectLister{fakeFullProjectState: fakeFullProjectState{ps}}
	wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{}, Review: &fakeReview{}})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v", err)
	}
	var res pumpSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump sweep result: %v", err)
	}
	if len(res.PumpedProjects) != 0 {
		t.Fatalf("want an empty sweep with no projects, got %v", res.PumpedProjects)
	}
}

// Only construction-phase projects are pumped; system-design/project-design-phase
// projects are skipped WITHOUT starting a child pump for them (the eligibility
// filter mirrors nextEligibleActivity's own Phase gate).
func Test_PumpSweep_FiltersToConstructionPhaseOnly(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	constructionProjectID := projectstate.ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{Version: 1, Phase: 2}}
	lister := fakeProjectLister{
		fakeFullProjectState: fakeFullProjectState{ps},
		summaries: []projectstate.ProjectSummary{
			{ProjectID: projectstate.ProjectID(uuid.NewString()), Phase: projectstate.PhaseSystemDesign},
			{ProjectID: projectstate.ProjectID(uuid.NewString()), Phase: projectstate.PhaseProjectDesign},
			{ProjectID: constructionProjectID, Phase: projectstate.PhaseConstruction},
		},
	}
	wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{}, Review: &fakeReview{}})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v", err)
	}
	var res pumpSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump sweep result: %v", err)
	}
	if len(res.PumpedProjects) != 1 || res.PumpedProjects[0] != ProjectID(constructionProjectID) {
		t.Fatalf("want exactly the one construction-phase project pumped, got %v", res.PumpedProjects)
	}
}

// boolPtr is a tiny test helper — ProjectSummary.OperatorPaused is *bool
// (generated, omitempty).
func boolPtr(b bool) *bool { return &b }

// Fix round 1 (Task 7c live-firing review), FINDING 2: a paused project is
// EXCLUDED from the fan-out even though it is otherwise eligible
// (construction-phase); an unpaused construction-phase project alongside it is
// still pumped — the pause check must not over- or under-fire.
func Test_PumpSweep_ExcludesPausedProject_IncludesUnpaused(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pausedID := projectstate.ProjectID(uuid.NewString())
	activeID := projectstate.ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{Version: 1, Phase: 2}}
	lister := fakeProjectLister{
		fakeFullProjectState: fakeFullProjectState{ps},
		summaries: []projectstate.ProjectSummary{
			{ProjectID: pausedID, Phase: projectstate.PhaseConstruction, OperatorPaused: boolPtr(true)},
			{ProjectID: activeID, Phase: projectstate.PhaseConstruction, OperatorPaused: boolPtr(false)},
		},
	}
	wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{}, Review: &fakeReview{}})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v", err)
	}
	var res pumpSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump sweep result: %v", err)
	}
	if len(res.PumpedProjects) != 1 || res.PumpedProjects[0] != ProjectID(activeID) {
		t.Fatalf("want only the unpaused project pumped (paused excluded), got %v", res.PumpedProjects)
	}
}

// A nil OperatorPaused (the zero value ListProjects reports for a
// never-paused project, per its own omitempty convention) must NOT be
// mistaken for "paused" — it means "not paused", same as an explicit false.
func Test_PumpSweep_NilOperatorPaused_TreatedAsNotPaused(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := projectstate.ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: pid, Version: 1, Phase: 2}}
	lister := fakeProjectLister{
		fakeFullProjectState: fakeFullProjectState{ps},
		summaries:            []projectstate.ProjectSummary{{ProjectID: pid, Phase: projectstate.PhaseConstruction, OperatorPaused: nil}},
	}
	wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{}, Review: &fakeReview{}})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v", err)
	}
	var res pumpSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump sweep result: %v", err)
	}
	if len(res.PumpedProjects) != 1 || res.PumpedProjects[0] != ProjectID(pid) {
		t.Fatalf("want the nil-OperatorPaused project pumped (nil != paused), got %v", res.PumpedProjects)
	}
}

// An eligible construction-phase project gets a per-project child pump started
// (PumpNextActivityWorkflow, unchanged) — this test proves the fan-out actually
// reaches and runs that workflow (a quiet tick: no eligible activity wired), not
// just that the sweep enumerates.
func Test_PumpSweep_ConstructionPhaseProject_StartsChildPump(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := projectstate.ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: pid, Version: 1, Phase: 2}}
	lister := fakeProjectLister{
		fakeFullProjectState: fakeFullProjectState{ps},
		summaries:            []projectstate.ProjectSummary{{ProjectID: pid, Phase: projectstate.PhaseConstruction}},
	}
	wf := newWorkflows(wfDeps{
		Intervention:         &fakeIntervention{},
		Review:               &fakeReview{},
		NextEligibleActivity: nil, // every started child pump goes quiet immediately
	})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v", err)
	}
	var res pumpSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump sweep result: %v", err)
	}
	if len(res.PumpedProjects) != 1 || res.PumpedProjects[0] != ProjectID(pid) {
		t.Fatalf("want the one construction-phase project pumped, got %v", res.PumpedProjects)
	}
}

// pumpSweepChildWorkflowID must be a DIFFERENT shape from pumpWorkflowID's
// client-driven id (which always carries a non-empty tickId segment) — the two id
// spaces must never collide.
func Test_PumpSweepChildWorkflowID_DiffersFromClientDrivenPumpWorkflowID(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	sweepID := pumpSweepChildWorkflowID(pid)
	for _, tick := range []string{"t1", "2026-08-01T00:00:00Z"} {
		if clientID := pumpWorkflowID(pid, tick); clientID == sweepID {
			t.Fatalf("pumpSweepChildWorkflowID(%q) == pumpWorkflowID(%q, %q) — id spaces must never collide", pid, pid, tick)
		}
	}
}

// Fix round 1 (Task 7c live-firing review), FINDING 6: the "prior tick still
// cascading → collapse, not fail" branch (temporal.
// IsWorkflowExecutionAlreadyStartedError in PumpSweepWorkflow) IS reachable in
// the mocked TestWorkflowEnvironment — the SDK's test env tracks running child
// workflows by ID and rejects a second start against a STILL-RUNNING one with
// a real ChildWorkflowExecutionAlreadyStartedError, exactly like the real
// server (internal_workflow_testsuite.go checks runningWorkflows on start).
// §10d of the earlier report was WRONG to call this untestable — the cheap
// trigger is simply two ProjectSummary entries sharing one ProjectID in the
// SAME tick: the first starts the child; by the time the loop reaches the
// second (same stable pumpSweepChildWorkflowID, since it depends only on
// ProjectID), that child has not yet completed, so the second start collides
// for real and the collapse branch runs.
func Test_PumpSweep_DuplicateProjectIDInOneTick_SecondCollapsesOntoFirst(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := projectstate.ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: pid, Version: 1, Phase: 2}}
	lister := fakeProjectLister{
		fakeFullProjectState: fakeFullProjectState{ps},
		summaries: []projectstate.ProjectSummary{
			{ProjectID: pid, Phase: projectstate.PhaseConstruction},
			{ProjectID: pid, Phase: projectstate.PhaseConstruction}, // duplicate — same stable child id
		},
	}
	wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{}, Review: &fakeReview{}})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v — the collapse branch must not propagate the already-started error", err)
	}
	var res pumpSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump sweep result: %v", err)
	}
	// The duplicate collapses onto the first (not appended twice, not failed) —
	// per pumpSweepResult.PumpedProjects's doc: a collapsed entry never appears.
	if len(res.PumpedProjects) != 1 || res.PumpedProjects[0] != ProjectID(pid) {
		t.Fatalf("want exactly one pump for the duplicate id (the second collapses onto the first), got %v", res.PumpedProjects)
	}
}

// ---- Tests: RegisterSchedules (Task 7c) -------------------------------------

// fakeScheduleBus records every RegisterSchedule call. Satisfies messagebus.MessageBus.
type fakeScheduleBus struct {
	mu    sync.Mutex
	specs []messagebus.ScheduleSpec
	ids   []messagebus.ScheduleID
}

func (b *fakeScheduleBus) DeliverSignal(fwra.Context, messagebus.ExecutionID, messagebus.SignalName, messagebus.ExecutionPayload) error {
	return nil
}

func (b *fakeScheduleBus) RegisterSchedule(_ fwra.Context, scheduleID messagebus.ScheduleID, spec messagebus.ScheduleSpec) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ids = append(b.ids, scheduleID)
	b.specs = append(b.specs, spec)
	return nil
}

var _ messagebus.MessageBus = (*fakeScheduleBus)(nil)

// RegisterSchedules must register exactly the two platform-wide Schedules — the
// pump sweep (30s, targeting PumpSweepWorkflow) and the replan sweep (5m, targeting
// ReplanSweepWorkflow) — with the right ids/workflow-types/intervals.
func Test_RegisterSchedules_RegistersPumpSweepAndReplanSweep(t *testing.T) {
	bus := &fakeScheduleBus{}

	if err := RegisterSchedules(context.Background(), bus); err != nil {
		t.Fatalf("RegisterSchedules: %v", err)
	}

	if len(bus.ids) != 2 {
		t.Fatalf("want 2 Schedules registered, got %d: %v", len(bus.ids), bus.ids)
	}
	byID := make(map[messagebus.ScheduleID]messagebus.ScheduleSpec, len(bus.ids))
	for i, id := range bus.ids {
		byID[id] = bus.specs[i]
	}

	pumpSpec, ok := byID[messagebus.ScheduleID(scheduleIDPumpSweep)]
	if !ok {
		t.Fatalf("missing pump-sweep Schedule %q; got ids %v", scheduleIDPumpSweep, bus.ids)
	}
	if string(pumpSpec.ExecutionKind) != executionKindPumpSweep {
		t.Fatalf("pump-sweep ExecutionKind = %q, want %q", pumpSpec.ExecutionKind, executionKindPumpSweep)
	}
	if pumpSpec.Cadence.Every != pumpSweepIntervalSecs*time.Second {
		t.Fatalf("pump-sweep interval = %v, want %ds", pumpSpec.Cadence.Every, pumpSweepIntervalSecs)
	}

	replanSpec, ok := byID[messagebus.ScheduleID(scheduleIDReplanSweep)]
	if !ok {
		t.Fatalf("missing replan-sweep Schedule %q; got ids %v", scheduleIDReplanSweep, bus.ids)
	}
	if string(replanSpec.ExecutionKind) != executionKindReplanSweep {
		t.Fatalf("replan-sweep ExecutionKind = %q, want %q", replanSpec.ExecutionKind, executionKindReplanSweep)
	}
	if replanSpec.Cadence.Every != replanSweepIntervalSecs*time.Second {
		t.Fatalf("replan-sweep interval = %v, want %ds", replanSpec.Cadence.Every, replanSweepIntervalSecs)
	}
}

// ---- Tests: conditional per-phase approval gate (Task 6) --------------------

// newFakeProjectStateWithPolicy builds a fakeProjectState whose served project
// carries the given committed ReviewPolicy (the gate's start-snapshot source).
func newFakeProjectStateWithPolicy(policy projectstate.ReviewPolicy) *fakeProjectState {
	return &fakeProjectState{project: projectstate.Project{
		ID:           projectstate.ProjectID(uuid.NewString()),
		Version:      1,
		Phase:        2,
		ReviewPolicy: policy,
	}}
}

// gateDeps builds a wfDeps for the gate tests: GitStatus is wired to the fake project
// state so gitOn is true → the phase records fire (the PR rail stays dormant, so
// branch/PR/merge are no-ops). The read/transition seams are registration-side only
// now (registerConstruct backs the generated activities with the same ps).
func gateDeps(ps *fakeProjectState) wfDeps {
	return wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		GitStatus:    ps,
	}
}

// newFakePipeline is the default all-phases-succeed pipeline double.
func newFakePipeline() *fakePipeline { return &fakePipeline{phase: PipelineSucceeded} }

// failOncePipeline fails the pipeline exactly once for the named phase, then serves
// success for it (and every other phase). It correlates the observed phase via the
// last-submitted spec (runPipeline submits then immediately observes, sequentially).
type failOncePipeline struct {
	mu        sync.Mutex
	failPhase string
	failed    map[string]bool
	lastPhase string
	submitted []agenticjob.PipelineSpec
}

func newFakePipelineFailingOnce(phase string) *failOncePipeline {
	return &failOncePipeline{failPhase: phase, failed: map[string]bool{}}
}

func (p *failOncePipeline) SubmitAgenticJob(_ fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submitted = append(p.submitted, spec)
	// The phase rides in DispatchInputs (the neutral pipelineSpec.Phase is composed into
	// the contract spec's DispatchInputs["phase"] by the workflow-side helper).
	p.lastPhase = spec.DispatchInputs["phase"]
	return agenticjob.PipelineHandle("wf-" + string(spec.ActivityID)), nil
}

func (p *failOncePipeline) ObserveAgenticJob(_ fwra.Context, _ agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ph := p.lastPhase
	if ph == p.failPhase && !p.failed[ph] {
		p.failed[ph] = true
		return agenticjob.PipelineObservation{Phase: agenticjob.PhaseFailed, Diagnostic: "forced one-time failure"}, nil
	}
	return agenticjob.PipelineObservation{Phase: agenticjob.PhaseSucceeded}, nil
}

func (p *failOncePipeline) CancelAgenticJob(_ fwra.Context, _ agenticjob.PipelineHandle) error {
	return nil
}

var _ agenticjob.AgenticJobAccess = (*failOncePipeline)(nil)

// Empty ReviewPolicy → no suspend, all phases dispatch. Byte-for-byte today's behavior.
func Test_Construct_EmptyPolicy_NoGate_WalksAllPhases(t *testing.T) {
	pipe := runPumpWith(t, sampleActivity()) // fakeProjectState default policy = empty
	if len(pipe.submitted) != 5 {
		t.Fatalf("empty policy submitted %d, want 5", len(pipe.submitted))
	}
}

// A gated phase suspends until the matching-phase Approve arrives, which records the
// phase completion to head-state.
func Test_Construct_GatedPhase_ApproveRecordsCompleted(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseDetailedDesign},
	}})
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 30*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !ps.phaseCompleted("C-Orders", "detailed_design") {
		t.Error("expected RecordPhaseCompleted(detailed_design) after approval")
	}
}

// The gate is phase-multiplexed: a decision for a DIFFERENT phase is ignored; only the
// matching-phase decision releases the gate.
func Test_Construct_GatedPhase_StaleSignalIgnored(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseDetailedDesign},
	}})
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "requirements", Decision: PhaseApprove}) // wrong phase
	}, 10*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 40*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !ps.phaseCompleted("C-Orders", "detailed_design") {
		t.Error("gate must release only on the matching-phase decision")
	}
}

func Test_Construct_VarianceRetry_DoesNotReGateApprovedPhase(t *testing.T) {
	// THE resumability guarantee: approve an early gated phase, then force a LATER phase's
	// pipeline to fail once (→ variance retry re-walks from index 0). The already-approved
	// phase must NOT re-suspend — the in-memory completedPhases set skips it.
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseRequirements}, // gate phase 0
	}})
	pipe := newFakePipelineFailingOnce("test_plan") // phase 2 fails once, then succeeds
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	approvals := 0
	env.RegisterDelayedCallback(func() {
		approvals++
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "requirements", Decision: PhaseApprove})
	}, 20*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// If the retry re-gated phase 0, the workflow would block waiting for a 2nd approval that
	// never comes (test times out) — reaching completion with a single approval proves it did not.
	if approvals != 1 {
		t.Fatalf("expected exactly 1 approval (phase 0 not re-gated on retry), got %d", approvals)
	}
}

// Test_Construct_VarianceRetry_NonGit_DoesNotReGateApprovedPhase is the non-git
// counterpart of the variance-retry resumability test. It wires NO GitStatus (gitOn=false)
// so the ONLY barrier preventing re-gating on variance re-walk is the in-memory
// completedPhases mark. Because the head-state write in completePhase is gitOn-gated,
// the test ONLY passes if the mark is unconditional (outside the gitOn branch). If the
// mark were inside an "if gitOn" block, the mark would not be set, the re-walk would
// re-gate phase 0, and the workflow would deadlock (no 2nd approval scheduled).
func Test_Construct_VarianceRetry_NonGit_DoesNotReGateApprovedPhase(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseRequirements}, // gate phase 0
	}})
	pipe := newFakePipelineFailingOnce("test_plan") // phase 2 fails once, then succeeds
	// GitStatus intentionally unwired ⇒ gitOn=false. The in-memory completedPhases mark
	// is the ONLY re-gate barrier (no head-state completion record exists on re-walk).
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe)
	approvals := 0
	env.RegisterDelayedCallback(func() {
		approvals++
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "requirements", Decision: PhaseApprove})
	}, 20*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// If the completedPhases mark were gitOn-gated the workflow would block waiting for a
	// 2nd approval that never comes (deadlock). Reaching completion with exactly one approval
	// proves the mark is unconditional and the variance re-walk skipped phase 0.
	if approvals != 1 {
		t.Fatalf("expected exactly 1 approval (phase 0 not re-gated on non-git retry), got %d", approvals)
	}
}

// Test_Construct_GatedPhase_SendBackRedraftsThenApprove proves that SendBack redrafts
// the gated phase in place (re-runs its pipeline) and never enters the variance path.
// After the redraft, PhaseApprove completes the phase and the workflow exits normally.
func Test_Construct_GatedPhase_SendBackRedraftsThenApprove(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseDetailedDesign}, // gate phase 1
	}})
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)

	// First signal: SendBack (redraft). The gate re-runs detailed_design's pipeline then
	// loops back to StageAwaitingApproval without entering the variance path.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{
			Phase:    "detailed_design",
			Decision: PhaseSendBack,
			Feedback: &ReviewFeedback{Notes: "needs revision"},
		})
	}, 30*time.Second)
	// Second signal: Approve. Completes the phase; the workflow runs remaining phases
	// and exits normally.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 60*time.Second)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// SendBack must NOT trigger the variance path (no recordActivityFailed call).
	if len(ps.failed) != 0 {
		t.Fatalf("SendBack must not enter the variance path; got failed: %v", ps.failed)
	}
	// Phase completed record landed (gitOn=true via gateDeps).
	if !ps.phaseCompleted("C-Orders", "detailed_design") {
		t.Error("expected RecordPhaseCompleted(detailed_design) after SendBack+Approve")
	}
	// Activity exited Completed (not Skipped or Failed).
	if len(ps.exited) != 1 || ps.exited[0].outcome != projectstate.ActivityOutcomeCompleted {
		t.Fatalf("want one Completed exit after SendBack+Approve, got %v", ps.exited)
	}
}

// Test_Construct_EmptyPolicy_NonGit_NoPhaseRecords is the "pure vibes = today" guarantee
// (brief B6): an empty ReviewPolicy AND gitOn=false must write ZERO phase head-state records.
// This is the strict inertness proof: with no gating and no git, the construction loop must
// behave exactly as it did before the gate feature was introduced — no RecordPhaseStarted or
// RecordPhaseCompleted calls, phaseDone must be empty. This is distinct from the non-git
// variance-retry tests (which carry a non-empty policy) and from the empty-policy walk test
// (which only checks pipeline submissions, not head-state writes).
func Test_Construct_EmptyPolicy_NonGit_NoPhaseRecords(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(uuid.NewString()),
		Version: 3,
		Phase:   2,
		// ReviewPolicy is zero value — no gating at all.
	}}
	pipe := &fakePipeline{phase: PipelineSucceeded}
	// GitStatus intentionally NOT wired → gitOn=false.
	// With no gating and no git, the loop must produce no phase head-state records.
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe)

	act := sampleActivity()
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID:  ProjectID(ps.project.ID),
		ActivityID: ActivityID(act.ActivityID),
		Activity:   act,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// Core assertion: empty policy + non-git → no phase records whatsoever.
	if len(ps.phaseDone) != 0 {
		t.Fatalf("empty-policy non-git path wrote %d phase record(s), want 0: %v", len(ps.phaseDone), ps.phaseDone)
	}
	// Sanity check: all phases still dispatched (behavior unchanged vs. pre-gate).
	if len(pipe.submitted) != len(act.Phases) {
		t.Fatalf("empty-policy non-git submitted %d pipelines, want %d", len(pipe.submitted), len(act.Phases))
	}
}

// ---- Tests: preset resolution + non-overridable floor (Task 7) --------------

// vibesPreset builds a ReviewPolicy with the "vibes" preset (auto-approve everything
// short of the non-overridable floor). ReviewPolicy.Preset is *string (modelgen's
// optional-scalar convention).
func vibesPreset() projectstate.ReviewPolicy {
	p := projectstate.ReviewPresetVibes
	return projectstate.ReviewPolicy{Preset: &p}
}

// deployTouchingContract builds a ServiceContract whose Interface carries a
// deploy-shaped operation — trips ContractTouchesReviewFloor.
func deployTouchingContract() projectstate.ServiceContract {
	return projectstate.ServiceContract{
		Component: "comp-1",
		Interface: projectstate.ContractInterface{
			Operations: []projectstate.ContractOperation{{Name: "DeployService"}},
		},
	}
}

// Test_Construct_VibesPreset_NoSuspend_WalksAllPhases proves the brief's Step-1
// "vibes auto-approves a draft commit" scenario end-to-end through the real
// construction workflow: under the "vibes" preset, with no floor-touching contract
// committed, every phase dispatches with NO suspend and NO signal — the workflow
// completes on its own, exactly like the empty-policy "pure vibes" case.
func Test_Construct_VibesPreset_NoSuspend_WalksAllPhases(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(vibesPreset()) // no ServiceContracts committed
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if !env.IsWorkflowCompleted() {
		t.Fatal("vibes preset must not suspend — workflow should complete without any signal")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// Phases + the auto-dispatched LOCAL MERGE job (local-merge-and-policy Commit 1):
	// under vibes in the rail-dormant git profile the activity finishes with a
	// policy-auto-approved merge of activity/<id> into main.
	if len(pipe.submitted) != len(sampleActivity().Phases)+1 {
		t.Fatalf("vibes preset submitted %d pipelines, want %d (phases + auto-merge)", len(pipe.submitted), len(sampleActivity().Phases)+1)
	}
	last := pipe.submitted[len(pipe.submitted)-1]
	if last.DispatchInputs[agenticjob.DispatchInputJobKey] != agenticjob.DispatchJobMerge {
		t.Fatalf("last submit must be the merge job, got inputs %v", last.DispatchInputs)
	}
}

// Test_Construct_VibesPreset_FloorSuspendsWithoutApproval proves the brief's Step-1
// "floor gate still blocks a flagged dispatch" scenario: even under "vibes", a
// construction-phase dispatch of an activity whose committed contract touches
// deploy/spend/schema genuinely SUSPENDS — with NO approval signal registered, the
// workflow must record requirements/detailed_design/test_plan completed but NEVER
// construction (the test environment's own runaway-test guard eventually forces the
// still-blocked execution to a ScheduleToClose deadline error, so IsWorkflowCompleted
// alone can't distinguish suspended-forever from truly-inert; the phase-completion
// record is the real red/green signal — an inert, not-actually-gated phase would have
// recorded "construction" too).
func Test_Construct_VibesPreset_FloorSuspendsWithoutApproval(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(vibesPreset())
	ps.project.ServiceContracts = map[string]projectstate.ServiceContract{"comp-1": deployTouchingContract()}
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	// Deliberately NO signal registered — the workflow must never reach the
	// construction phase's completion record on its own.
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	for _, phase := range []string{"requirements", "detailed_design", "test_plan"} {
		if !ps.phaseCompleted("C-Orders", phase) {
			t.Fatalf("expected %s to complete before the floor-gated phase", phase)
		}
	}
	if ps.phaseCompleted("C-Orders", "construction") {
		t.Fatal("floor gate must suspend construction dispatch — it must NOT complete when no approval ever arrives, even under vibes")
	}
}

// Test_Construct_VibesPreset_FloorBlocksFlaggedDispatch proves the release path: an
// explicit approval on the construction phase releases the floor's suspend and
// records the phase completion.
func Test_Construct_VibesPreset_FloorBlocksFlaggedDispatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(vibesPreset())
	ps.project.ServiceContracts = map[string]projectstate.ServiceContract{"comp-1": deployTouchingContract()}
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "construction", Decision: PhaseApprove})
	}, 30*time.Second)
	// The risk floor ALSO holds the local merge (local-merge-and-policy Commit 1):
	// a second approval on the "merge" gate key releases it.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: mergeGateKey, Decision: PhaseApprove})
	}, 60*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !ps.phaseCompleted("C-Orders", "construction") {
		t.Error("floor gate must suspend construction dispatch until an explicit approval, even under vibes")
	}
}

// Test_Construct_VibesPreset_NoFloor_NoDeadlock is the negative control for the floor
// test above: WITHOUT registering any approval signal, a vibes-preset activity whose
// contract does NOT touch the floor must complete on its own (proves the floor test's
// suspend is caused by the contract, not by vibes gating construction generally).
func Test_Construct_VibesPreset_NoFloor_NoDeadlock(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(vibesPreset())
	ps.project.ServiceContracts = map[string]projectstate.ServiceContract{"comp-1": {
		Component: "comp-1",
		Interface: projectstate.ContractInterface{
			Operations: []projectstate.ContractOperation{{Name: "GenerateArtifact"}},
		},
	}}
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if !env.IsWorkflowCompleted() {
		t.Fatal("a non-floor-touching contract must not suspend construction dispatch under vibes")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
}

// ---- Tests: policy-gated LOCAL merge step (local-merge-and-policy Commit 1) --

// checkpointsPreset builds a ReviewPolicy with the "checkpoints" preset.
func checkpointsPreset() projectstate.ReviewPolicy {
	p := projectstate.ReviewPresetCheckpoints
	return projectstate.ReviewPolicy{Preset: &p}
}

// mergeSubmits returns the merge-job submissions the fake pipeline captured.
func mergeSubmits(specs []agenticjob.PipelineSpec) []agenticjob.PipelineSpec {
	var out []agenticjob.PipelineSpec
	for _, s := range specs {
		if s.DispatchInputs[agenticjob.DispatchInputJobKey] == agenticjob.DispatchJobMerge {
			out = append(out, s)
		}
	}
	return out
}

// Test_Construct_LocalMerge_CheckpointsHoldsUntilMergeApproval proves the
// checkpoints/full hold: after the gated phases are approved, the activity
// holds AGAIN at the merge gate (keyed "merge") and dispatches the merge job
// only on Approve.
func Test_Construct_LocalMerge_CheckpointsHoldsUntilMergeApproval(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(checkpointsPreset())
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	// checkpoints gates detailed_design + construction; the merge gate holds last.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 20*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "construction", Decision: PhaseApprove})
	}, 40*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: mergeGateKey, Decision: PhaseApprove})
	}, 60*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	merges := mergeSubmits(pipe.submitted)
	if len(merges) != 1 {
		t.Fatalf("expected exactly 1 merge-job submit after merge approval, got %d", len(merges))
	}
	if got := merges[0].DispatchInputs["activity_id"]; got != "C-Orders" {
		t.Fatalf("merge job activity_id = %q, want C-Orders", got)
	}
	if len(ps.exited) != 1 {
		t.Fatalf("expected the activity to exit after the approved merge, exits = %d", len(ps.exited))
	}
}

// Test_Construct_LocalMerge_CheckpointsHoldsWithoutMergeApproval is the negative
// control: with the phase approvals delivered but NO merge approval, the merge
// job is never dispatched and the activity never exits (the Temporal test env's
// runaway watchdog eventually forces the suspended run down, exactly as the
// Task-7 floor-suspend test documents — the real red/green signal is the
// absence of the merge submit + the exit record).
func Test_Construct_LocalMerge_CheckpointsHoldsWithoutMergeApproval(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(checkpointsPreset())
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 20*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "construction", Decision: PhaseApprove})
	}, 40*time.Second)
	// Deliberately NO "merge" approval.
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if got := mergeSubmits(pipe.submitted); len(got) != 0 {
		t.Fatalf("merge job must NOT dispatch without merge approval, got %d submits", len(got))
	}
	if len(ps.exited) != 0 {
		t.Fatalf("activity must NOT exit while the merge gate holds, exits = %d", len(ps.exited))
	}
}

// Test_Construct_LocalMerge_NonGitProfile_NoMergeSubmit: with GitStatus unwired
// (gitOn=false — no git slice at all), the merge step is a no-op: phases only.
func Test_Construct_LocalMerge_NonGitProfile_NoMergeSubmit(t *testing.T) {
	pipe := runPumpWith(t, sampleActivity()) // wires NO GitStatus
	if got := mergeSubmits(pipe.submitted); len(got) != 0 {
		t.Fatalf("non-git profile must not dispatch a merge job, got %d", len(got))
	}
}

// mergeConflictPipeline serves Succeeded for phase dispatches and a FAILED
// "merge conflict" observation for merge-job dispatches — the deterministic-
// conflict double for the intervention-path test.
type mergeConflictPipeline struct {
	mu        sync.Mutex
	submitted []agenticjob.PipelineSpec
}

func (p *mergeConflictPipeline) SubmitAgenticJob(_ fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submitted = append(p.submitted, spec)
	if spec.DispatchInputs[agenticjob.DispatchInputJobKey] == agenticjob.DispatchJobMerge {
		return agenticjob.PipelineHandle("merge-" + string(spec.ActivityID)), nil
	}
	return agenticjob.PipelineHandle("wf-" + string(spec.ActivityID)), nil
}

func (p *mergeConflictPipeline) ObserveAgenticJob(_ fwra.Context, handle agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
	if strings.HasPrefix(string(handle), "merge-") {
		return agenticjob.PipelineObservation{
			Phase:      agenticjob.PhaseFailed,
			Diagnostic: "local merge: merge conflict merging activity/C-Orders into main",
		}, nil
	}
	return agenticjob.PipelineObservation{Phase: agenticjob.PhaseSucceeded}, nil
}

func (p *mergeConflictPipeline) CancelAgenticJob(_ fwra.Context, _ agenticjob.PipelineHandle) error {
	return nil
}

var _ agenticjob.AgenticJobAccess = (*mergeConflictPipeline)(nil)

// Test_Construct_LocalMerge_ConflictRoutesToIntervention proves a merge conflict
// flows through the SAME variance machinery as a failed phase pipeline: under a
// Retry directive the deterministic conflict exhausts the variance budget and
// the activity records a terminal FAILURE (VarianceExhausted) — never a fake
// completed exit, and no partial merge is ever recorded.
func Test_Construct_LocalMerge_ConflictRoutesToIntervention(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(vibesPreset())
	pipe := &mergeConflictPipeline{}
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.failed) != 1 || ps.failed[0].reason != projectstate.VarianceExhausted {
		t.Fatalf("expected a terminal VarianceExhausted failure record, got %+v", ps.failed)
	}
	if len(ps.exited) != 0 {
		t.Fatalf("a conflicted merge must not record a completed exit, exits = %+v", ps.exited)
	}
}

// ---- SetReviewPolicy (op 2.8, local-merge-and-policy Commit 2) --------------

// setReviewPolicyManager wires a constructionManager over the generated
// FakeConstructionTransitionAccess for the preset write-path tests.
func setReviewPolicyManager(ps projectstate.ProjectStateAccess, ct projectstate.ConstructionTransitionAccess) *constructionManager {
	return newConstructionManager(nil, ps, nil, nil, nil, nil, nil, ct, nil, nil, nil, nil, 0, "", nil)
}

func Test_SetReviewPolicy_EmptyProjectID(t *testing.T) {
	m := setReviewPolicyManager(nil, nil)
	err := m.SetReviewPolicy(fwmanager.Context{Context: context.Background()}, ProjectID(""), projectstate.ReviewPresetVibes)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// The write path is a CLOSED vocabulary: an unknown preset is rejected before
// any RA call (the fake would panic if touched — nil Fns). This is the fix for
// the documented fail-open corner: EffectiveGate's read path treats an
// unrecognized preset as the legacy explicit-map fallback (empty map → gates
// NOTHING), so a typo must never be persistable.
func Test_SetReviewPolicy_RejectsUnknownPreset(t *testing.T) {
	m := setReviewPolicyManager(nil, &projectstatefake.FakeConstructionTransitionAccess{})
	for _, preset := range []string{"", "vibess", "YOLO", "Full", "VIBES"} {
		err := m.SetReviewPolicy(fwmanager.Context{Context: context.Background()}, ProjectID("p1"), preset)
		if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
			t.Fatalf("preset %q: want ContractMisuse, got %s", preset, got)
		}
	}
}

// A valid preset writes through the projectstate CAS: read the current version,
// record the policy at that expectedVersion with the preset set and the
// committed GatedPhasesByType map PRESERVED (SetReviewPolicy and
// UpdateReviewPolicy own disjoint halves of ReviewPolicy).
func Test_SetReviewPolicy_WritesPresetPreservingGateMap(t *testing.T) {
	existing := map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseDetailedDesign},
	}
	var recorded *projectstate.ReviewPolicy
	var recordedVersion projectstate.Version
	// The read moved to the base projectStateAccess port (constructionTransitionAccess.ReadProject
	// was pruned — both call sites passed empty cred); RecordReviewPolicy stays on constructionTransition.
	ps := &projectstatefake.FakeProjectStateAccess{
		ReadProjectFn: func(_ fwra.Context, projectID projectstate.ProjectID) (projectstate.Project, error) {
			return projectstate.Project{
				ID:           projectID,
				Version:      7,
				ReviewPolicy: projectstate.ReviewPolicy{GatedPhasesByType: existing},
			}, nil
		},
	}
	ct := &projectstatefake.FakeConstructionTransitionAccess{
		RecordReviewPolicyFn: func(_ fwra.Context, _ projectstate.ProjectID, expectedVersion projectstate.Version, policy projectstate.ReviewPolicy, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
			recorded = &policy
			recordedVersion = expectedVersion
			return expectedVersion + 1, nil
		},
	}
	m := setReviewPolicyManager(ps, ct)
	for _, preset := range []string{projectstate.ReviewPresetVibes, projectstate.ReviewPresetCheckpoints, projectstate.ReviewPresetFull} {
		recorded = nil
		if err := m.SetReviewPolicy(fwmanager.Context{Context: context.Background()}, ProjectID("p1"), preset); err != nil {
			t.Fatalf("SetReviewPolicy(%q): %v", preset, err)
		}
		if recorded == nil || recorded.Preset == nil || *recorded.Preset != preset {
			t.Fatalf("preset %q not recorded: %+v", preset, recorded)
		}
		if len(recorded.GatedPhasesByType["service"]) != 1 {
			t.Fatalf("GatedPhasesByType must be preserved, got %+v", recorded.GatedPhasesByType)
		}
		if recordedVersion != 7 {
			t.Fatalf("CAS expectedVersion = %d, want the read version 7", recordedVersion)
		}
	}
}

func Test_SetReviewPolicy_UnknownProject_NotFound(t *testing.T) {
	ps := &projectstatefake.FakeProjectStateAccess{
		ReadProjectFn: func(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
			return projectstate.Project{}, fwra.New(fwra.NotFound, "no such project")
		},
	}
	m := setReviewPolicyManager(ps, nil)
	err := m.SetReviewPolicy(fwmanager.Context{Context: context.Background()}, ProjectID("ghost"), projectstate.ReviewPresetVibes)
	if got := asConstructionError(t, err).Kind; got != fwmanager.NotFound {
		t.Fatalf("want NotFound, got %s", got)
	}
}

// railLifecycleEnabled derives wfDeps.RailEnabled (WorkerManifest). The repo resolver
// is part of the derivation: the local profile binds the GitLocal sourceControlAccess
// for the DESIGN managers while construction stays repo-less there, and that
// rail-without-repo boot MUST read rail-dormant — RailEnabled=true would make
// runLocalMergeStep skip and nothing would merge local activity branches.
func Test_RailLifecycleEnabled_RequiresRailAndRepo(t *testing.T) {
	repo := func(ProjectID) (sourcecontrol.RepoRef, bool) { return sourcecontrol.RepoRef("acct|acct/p"), true }
	cases := []struct {
		name string
		rail sourcecontrol.SourceControlAccess
		repo func(ProjectID) (sourcecontrol.RepoRef, bool)
		want bool
	}{
		{"rail+repo (cloud / creds-ful local)", &stubRail{}, repo, true},
		{"rail without repo (local GitLocal rail; construction repo-less)", &stubRail{}, nil, false},
		{"repo without rail", nil, repo, false},
		{"neither", nil, nil, false},
	}
	for _, tc := range cases {
		if got := railLifecycleEnabled(tc.rail, tc.repo); got != tc.want {
			t.Fatalf("%s: railLifecycleEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ---- Tests: episode capture seam (SP1 Task 7) -------------------------------
//
// The invariant under test: every terminal observation of an agentic construction
// dispatch produces EXACTLY ONE ledger record — the mined summary when there is one, an
// explicit GAP record when there is not — and the append can never take the business
// workflow down with it.

// captureSeamSummary is the mined summary the local executor reports on a terminal
// observation. Every field is checked for VERBATIM carry-through below.
func captureSeamSummary() *agenticjob.EpisodeSummary {
	model := "claude-sonnet-4"
	cost := 0.42
	turns := int64(7)
	trace := ".aiarch/traces/ep-1.jsonl"
	streamed := agenticjob.EpisodeUsage{In: 90, Out: 40, CacheRead: 5, CacheCreate: 2}
	return &agenticjob.EpisodeSummary{
		EpisodeID:      "ep-1",
		Model:          &model,
		Usage:          agenticjob.EpisodeUsage{In: 100, Out: 50, CacheRead: 10, CacheCreate: 3},
		StreamedUsage:  &streamed,
		CostUSD:        &cost,
		NumTurns:       &turns,
		ToolCallCounts: map[string]int64{"Read": 4, "Edit": 2},
		SubagentSpans:  []agenticjob.SubagentSpan{{ToolUseID: "tu-1"}},
		StartedAt:      time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC),
		EndedAt:        time.Date(2026, 8, 2, 10, 5, 0, 0, time.UTC),
		Outcome:        agenticjob.EpisodeSucceeded,
		TracePath:      &trace,
	}
}

// assertSummaryCarriedVerbatim proves the whole mined summary reached the ledger
// unchanged — the capture seam enriches, it never recomputes. Split into a scalar half
// and a pointer half purely to keep each assertion block readable.
func assertSummaryCarriedVerbatim(t *testing.T, rec episode.EpisodeRecord, want *agenticjob.EpisodeSummary) {
	t.Helper()
	if rec.EpisodeID != want.EpisodeID {
		t.Errorf("EpisodeID = %q, want %q", rec.EpisodeID, want.EpisodeID)
	}
	if rec.Usage != episode.EpisodeUsage(want.Usage) {
		t.Errorf("Usage = %+v, want %+v", rec.Usage, want.Usage)
	}
	if !rec.StartedAt.Equal(want.StartedAt) || !rec.EndedAt.Equal(want.EndedAt) {
		t.Errorf("times = %v..%v, want the run's own clock %v..%v", rec.StartedAt, rec.EndedAt, want.StartedAt, want.EndedAt)
	}
	if rec.Outcome != episode.EpisodeSucceeded {
		t.Errorf("Outcome = %d, want EpisodeSucceeded", rec.Outcome)
	}
	if rec.ToolCallCounts["Read"] != want.ToolCallCounts["Read"] || rec.ToolCallCounts["Edit"] != want.ToolCallCounts["Edit"] {
		t.Errorf("ToolCallCounts = %v, want %v", rec.ToolCallCounts, want.ToolCallCounts)
	}
	if len(rec.SubagentSpans) != len(want.SubagentSpans) || rec.SubagentSpans[0].ToolUseID != want.SubagentSpans[0].ToolUseID {
		t.Errorf("SubagentSpans = %+v, want %+v", rec.SubagentSpans, want.SubagentSpans)
	}
	if rec.GapReason != nil {
		t.Errorf("a mined episode carries no GapReason, got %q", *rec.GapReason)
	}
	assertOptionalFieldsCarriedVerbatim(t, rec, want)
}

// assertOptionalFieldsCarriedVerbatim covers the summary's nil-able half.
func assertOptionalFieldsCarriedVerbatim(t *testing.T, rec episode.EpisodeRecord, want *agenticjob.EpisodeSummary) {
	t.Helper()
	if rec.StreamedUsage == nil || *rec.StreamedUsage != episode.EpisodeUsage(*want.StreamedUsage) {
		t.Errorf("StreamedUsage = %+v, want %+v", rec.StreamedUsage, want.StreamedUsage)
	}
	if rec.Model == nil || *rec.Model != *want.Model {
		t.Errorf("Model = %v, want %q", rec.Model, *want.Model)
	}
	if rec.CostUSD == nil || *rec.CostUSD != *want.CostUSD {
		t.Errorf("CostUSD = %v, want %v", rec.CostUSD, *want.CostUSD)
	}
	if rec.NumTurns == nil || *rec.NumTurns != *want.NumTurns {
		t.Errorf("NumTurns = %v, want %v", rec.NumTurns, *want.NumTurns)
	}
	if rec.TracePath == nil || *rec.TracePath != *want.TracePath {
		t.Errorf("TracePath = %v, want %q", rec.TracePath, *want.TracePath)
	}
}

// A terminal observation carrying a mined summary becomes exactly one EpisodeRecord per
// dispatched phase, with the summary copied verbatim and the Manager-known
// Kind/TargetRef/Lineage stamped on.
func Test_Construct_TerminalObservation_PersistsEpisodeRecord(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded, episode: captureSeamSummary()}
	eps := &fakeEpisodes{}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe, eps)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	got := eps.records()
	// One episode per dispatched App-A phase — the capture rides the same choke point
	// the dispatch does.
	if len(got) != len(sampleActivity().Phases) {
		t.Fatalf("want one appended episode per dispatched phase (%d), got %d", len(sampleActivity().Phases), len(got))
	}
	rec := got[0]
	if rec.Kind != episode.EpisodeKindConstruction {
		t.Errorf("Kind = %d, want EpisodeKindConstruction", rec.Kind)
	}
	if rec.TargetRef != "C-XYZ" {
		t.Errorf("TargetRef = %q, want the activity id", rec.TargetRef)
	}
	if rec.Lineage == nil || rec.Lineage.WorkflowID == "" || rec.Lineage.RunID == "" {
		t.Fatalf("Lineage must carry the durable execution identity, got %+v", rec.Lineage)
	}
	if rec.Lineage.ActivityID == nil || *rec.Lineage.ActivityID != "C-XYZ" {
		t.Errorf("Lineage.ActivityID = %v, want the Method activity id", rec.Lineage.ActivityID)
	}
	assertSummaryCarriedVerbatim(t, rec, captureSeamSummary())
}

// A terminal LOCAL-venue observation with NO summary must still produce a record: an
// explicit gap naming what went missing. A missing record is never silently missing.
func Test_Construct_TerminalObservation_MissingSummary_PersistsGapRecord(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded} // no episode mined, no run URL ⇒ local venue
	eps := &fakeEpisodes{}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe, eps)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	got := eps.records()
	if len(got) != len(sampleActivity().Phases) {
		t.Fatalf("a summary-less terminal observation must still be recorded: want %d, got %d", len(sampleActivity().Phases), len(got))
	}
	rec := got[0]
	if rec.Outcome != episode.EpisodeGap {
		t.Errorf("Outcome = %d, want EpisodeGap", rec.Outcome)
	}
	if rec.GapReason == nil || !strings.Contains(*rec.GapReason, "no episode summary") {
		t.Errorf("GapReason must name the missing summary, got %v", rec.GapReason)
	}
	if rec.EpisodeID == "" || strings.ContainsAny(rec.EpisodeID, ":/ ") {
		t.Errorf("a synthesized gap id must be non-empty and store-safe, got %q", rec.EpisodeID)
	}
	if rec.Kind != episode.EpisodeKindConstruction || rec.TargetRef != "C-XYZ" {
		t.Errorf("a gap still carries its Kind/TargetRef, got kind=%d ref=%q", rec.Kind, rec.TargetRef)
	}
}

// A REMOTE-venue (GitHub-Actions) run mines no episode in v1, so a nil summary there is
// expected rather than lost: it must NOT produce a gap record.
func Test_Construct_RemoteVenue_WritesNoGapRecord(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded, runURL: "https://github.com/acme/repo/actions/runs/7"}
	eps := &fakeEpisodes{}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe, eps)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if got := eps.records(); len(got) != 0 {
		t.Fatalf("the remote venue mines no episode in v1 — it must write nothing, got %+v", got)
	}
}

// A permanently-failing ledger must NOT take construction down: the append is retried
// inside its own envelope and then logged and dropped.
func Test_Construct_EpisodeAppendFailure_DoesNotFailBusinessFlow(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded, episode: captureSeamSummary()}
	eps := &fakeEpisodes{failAlways: true}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe, eps)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("a failing episode ledger must not stall the construction workflow")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failing episode ledger must not fail the construction workflow, got %v", err)
	}
	// The business exit still landed, untouched by the bookkeeping fault.
	if len(ps.exited) != 1 || ps.exited[0].outcome != projectstate.ActivityOutcomeCompleted {
		t.Fatalf("business flow must still record its exit, got %v", ps.exited)
	}
	// And the append really was RETRIED (more attempts than dispatches), not given up on
	// after one try.
	if eps.attemptCount() <= len(sampleActivity().Phases) {
		t.Fatalf("want the append retried within its envelope, got %d attempts across %d dispatches",
			eps.attemptCount(), len(sampleActivity().Phases))
	}
	if len(eps.records()) != 0 {
		t.Fatalf("nothing can land in a permanently-failing ledger, got %+v", eps.records())
	}
}

// The local MERGE job spawns no agent at all, so a summary-less terminal observation
// there is not a loss and must never be recorded as a gap. This rides the real
// policy-gated merge path (the same setup as the local-merge tests above), so the merge
// job genuinely dispatches and is genuinely observed.
func Test_Construct_MergeJob_WritesNoGapRecord(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(checkpointsPreset())
	pipe := newFakePipeline()
	eps := &fakeEpisodes{}
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe, eps)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 20*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "construction", Decision: PhaseApprove})
	}, 40*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: mergeGateKey, Decision: PhaseApprove})
	}, 60*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// Sanity: the merge job really did dispatch and be observed on this path.
	if len(mergeSubmits(pipe.submitted)) != 1 {
		t.Fatalf("this test only means something if the merge job dispatched; got %d", len(mergeSubmits(pipe.submitted)))
	}
	// Exactly one record per AGENTIC phase dispatch — the merge adds none.
	if n := len(eps.records()); n != len(sampleActivity().Phases) {
		t.Fatalf("the merge job must add no ledger record: want %d (one per agentic phase), got %d",
			len(sampleActivity().Phases), n)
	}
}

// ---------------------------------------------------------------------------
// Episode facet read ops (SP1 capture-seam, Task 9): ListEpisodesForActivity +
// GetEpisodeTimeline. Plain methods — no Temporal env needed.
// ---------------------------------------------------------------------------

// episodeMgr builds a constructionManager exercising ONLY the episode facet read
// ops (Task 9): every other dep stays nil since those ops touch only episodes.
func episodeMgr(eps episode.EpisodeAccess) ConstructionManager {
	return newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, eps, 0, "", nil)
}

// sampleEpisodeRecord returns a fully-populated ledger record (every optional
// field set) so the view mapping can be checked field-by-field.
func sampleEpisodeRecord(id, targetRef string) episode.EpisodeRecord {
	started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	ended := started.Add(90 * time.Second)
	workerClass := "senior-developer"
	model := "claude-sonnet-5"
	costUSD := 0.42
	numTurns := int64(7)
	gapReason := "the run reported a gap episode"
	tracePath := ".aiarch/state/episodes/traces/" + id + ".jsonl"
	streamed := episode.EpisodeUsage{In: 100, Out: 50, CacheRead: 10, CacheCreate: 5}
	return episode.EpisodeRecord{
		EpisodeID: id,
		Kind:      episode.EpisodeKindConstruction,
		TargetRef: targetRef,
		Lineage: &episode.EpisodeLineage{
			WorkflowID: "wf-1",
			RunID:      "run-1",
			ActivityID: strPtr("C-XYZ"),
		},
		WorkerClass:    &workerClass,
		Model:          &model,
		Usage:          episode.EpisodeUsage{In: 1000, Out: 500, CacheRead: 100, CacheCreate: 50},
		StreamedUsage:  &streamed,
		CostUSD:        &costUSD,
		NumTurns:       &numTurns,
		ToolCallCounts: map[string]int64{"Read": 3, "Edit": 2},
		SubagentSpans: []episode.SubagentSpan{
			{ToolUseID: "toolu_1", StartedAt: timePtr(started.Add(time.Second)), EndedAt: timePtr(started.Add(2 * time.Second))},
		},
		StartedAt: started,
		EndedAt:   ended,
		Outcome:   episode.EpisodeGap,
		GapReason: &gapReason,
		TracePath: &tracePath,
	}
}

func strPtr(s string) *string        { return &s }
func timePtr(t time.Time) *time.Time { return &t }

// assertSampleEpisodeRecordView asserts got is sampleEpisodeRecord(id, targetRef)
// mapped field-for-field onto this contract's own EpisodeRecordView. Split into
// three helpers (identity/usage/outcome) purely to keep each under the gocyclo
// budget — together they are one assertion.
func assertSampleEpisodeRecordView(t *testing.T, got EpisodeRecordView, id, targetRef string) {
	t.Helper()
	assertSampleEpisodeRecordIdentity(t, got, id, targetRef)
	assertSampleEpisodeRecordUsage(t, got)
	assertSampleEpisodeRecordOutcome(t, got, id)
}

func assertSampleEpisodeRecordIdentity(t *testing.T, got EpisodeRecordView, id, targetRef string) {
	t.Helper()
	if got.EpisodeID != id {
		t.Fatalf("EpisodeID = %q, want %q", got.EpisodeID, id)
	}
	if got.Kind != EpisodeKindConstruction {
		t.Fatalf("Kind = %v, want EpisodeKindConstruction", got.Kind)
	}
	if got.TargetRef != targetRef {
		t.Fatalf("TargetRef = %q, want %q", got.TargetRef, targetRef)
	}
	if got.Lineage == nil || got.Lineage.WorkflowID != "wf-1" || got.Lineage.RunID != "run-1" || got.Lineage.ActivityID == nil || *got.Lineage.ActivityID != "C-XYZ" {
		t.Fatalf("Lineage = %+v, want {wf-1 run-1 C-XYZ}", got.Lineage)
	}
	if got.WorkerClass == nil || *got.WorkerClass != "senior-developer" {
		t.Fatalf("WorkerClass = %v", got.WorkerClass)
	}
	if got.Model == nil || *got.Model != "claude-sonnet-5" {
		t.Fatalf("Model = %v", got.Model)
	}
}

func assertSampleEpisodeRecordUsage(t *testing.T, got EpisodeRecordView) {
	t.Helper()
	if got.Usage != (EpisodeUsage{In: 1000, Out: 500, CacheRead: 100, CacheCreate: 50}) {
		t.Fatalf("Usage = %+v", got.Usage)
	}
	if got.StreamedUsage == nil || *got.StreamedUsage != (EpisodeUsage{In: 100, Out: 50, CacheRead: 10, CacheCreate: 5}) {
		t.Fatalf("StreamedUsage = %v", got.StreamedUsage)
	}
	if got.CostUSD == nil || *got.CostUSD != 0.42 {
		t.Fatalf("CostUSD = %v", got.CostUSD)
	}
	if got.NumTurns == nil || *got.NumTurns != 7 {
		t.Fatalf("NumTurns = %v", got.NumTurns)
	}
	if got.ToolCallCounts["Read"] != 3 || got.ToolCallCounts["Edit"] != 2 {
		t.Fatalf("ToolCallCounts = %v", got.ToolCallCounts)
	}
	if len(got.SubagentSpans) != 1 || got.SubagentSpans[0].ToolUseID != "toolu_1" {
		t.Fatalf("SubagentSpans = %+v", got.SubagentSpans)
	}
}

func assertSampleEpisodeRecordOutcome(t *testing.T, got EpisodeRecordView, id string) {
	t.Helper()
	if got.Outcome != EpisodeGap {
		t.Fatalf("Outcome = %v, want EpisodeGap", got.Outcome)
	}
	if got.GapReason == nil || *got.GapReason != "the run reported a gap episode" {
		t.Fatalf("GapReason = %v", got.GapReason)
	}
	if got.TracePath == nil || *got.TracePath != ".aiarch/state/episodes/traces/"+id+".jsonl" {
		t.Fatalf("TracePath = %v", got.TracePath)
	}
}

// ---- ListEpisodesForActivity -------------------------------------------------

func Test_ListEpisodesForActivity_MapsRecordsWithTargetRef(t *testing.T) {
	eps := &fakeEpisodes{listRecords: []episode.EpisodeRecord{
		sampleEpisodeRecord("ep-1", "C-Orders"),
	}}
	m := episodeMgr(eps)

	got, err := m.ListEpisodesForActivity(testCtx(), ProjectID("proj-1"), "C-Orders")
	if err != nil {
		t.Fatalf("ListEpisodesForActivity: unexpected error: %v", err)
	}
	if eps.lastQuery.ProjectID != episode.ProjectID("proj-1") {
		t.Fatalf("ListEpisodes query ProjectID = %q, want proj-1", eps.lastQuery.ProjectID)
	}
	if eps.lastQuery.TargetRef == nil || *eps.lastQuery.TargetRef != "C-Orders" {
		t.Fatalf("ListEpisodes query TargetRef = %v, want *\"C-Orders\" (scoped by activityID)", eps.lastQuery.TargetRef)
	}
	if len(got) != 1 {
		t.Fatalf("ListEpisodesForActivity: got %d records, want 1", len(got))
	}
	assertSampleEpisodeRecordView(t, got[0], "ep-1", "C-Orders")
}

func Test_ListEpisodesForActivity_EmptyProjectID_ContractMisuse(t *testing.T) {
	m := episodeMgr(&fakeEpisodes{})
	_, err := m.ListEpisodesForActivity(testCtx(), ProjectID(""), "C-Orders")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_ListEpisodesForActivity_EmptyActivityID_ContractMisuse(t *testing.T) {
	m := episodeMgr(&fakeEpisodes{})
	_, err := m.ListEpisodesForActivity(testCtx(), ProjectID("proj-1"), "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_ListEpisodesForActivity_RAError_MapsInfrastructure(t *testing.T) {
	eps := &fakeEpisodes{listErr: fwra.New(fwra.Infrastructure, "ledger unavailable")}
	m := episodeMgr(eps)
	_, err := m.ListEpisodesForActivity(testCtx(), ProjectID("proj-1"), "C-Orders")
	if got := asConstructionError(t, err).Kind; got != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure, got %s", got)
	}
}

// ---- GetEpisodeTimeline ------------------------------------------------------

func Test_GetEpisodeTimeline_StitchesSequentialEventsWithTypes(t *testing.T) {
	eps := &fakeEpisodes{
		listRecords: []episode.EpisodeRecord{sampleEpisodeRecord("ep-1", "C-Orders")},
		traceEvents: []json.RawMessage{
			json.RawMessage(`{"type":"system","subtype":"init"}`),
			json.RawMessage(`{"type":"assistant"}`),
			json.RawMessage(`not-json`),
		},
	}
	m := episodeMgr(eps)

	got, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "ep-1")
	if err != nil {
		t.Fatalf("GetEpisodeTimeline: unexpected error: %v", err)
	}
	// GetEpisodeTimeline resolves the record by scanning EVERY target on the
	// project (episodeAccess has no by-id lookup) — the query must carry no
	// TargetRef.
	if eps.lastQuery.TargetRef != nil {
		t.Fatalf("ListEpisodes query TargetRef = %v, want nil (whole-project scan)", eps.lastQuery.TargetRef)
	}
	assertSampleEpisodeRecordView(t, got.Record, "ep-1", "C-Orders")

	if eps.lastTraceProjectID != episode.ProjectID("proj-1") || eps.lastTraceEpisodeID != "ep-1" {
		t.Fatalf("ReadTraceEvents args = (%q, %q), want (proj-1, ep-1)", eps.lastTraceProjectID, eps.lastTraceEpisodeID)
	}
	if len(got.Events) != 3 {
		t.Fatalf("Events: got %d, want 3", len(got.Events))
	}
	wantTypes := []string{"system", "assistant", "unknown"}
	for i, ev := range got.Events {
		if ev.Seq != int64(i+1) {
			t.Fatalf("Events[%d].Seq = %d, want %d (1-based sequential)", i, ev.Seq, i+1)
		}
		if ev.EventType != wantTypes[i] {
			t.Fatalf("Events[%d].EventType = %q, want %q", i, ev.EventType, wantTypes[i])
		}
		if ev.Raw == nil || string(*ev.Raw) != string(eps.traceEvents[i]) {
			t.Fatalf("Events[%d].Raw = %v, want %s (carried verbatim)", i, ev.Raw, eps.traceEvents[i])
		}
	}
}

func Test_GetEpisodeTimeline_UnknownEpisodeID_NotFound(t *testing.T) {
	eps := &fakeEpisodes{listRecords: []episode.EpisodeRecord{sampleEpisodeRecord("ep-1", "C-Orders")}}
	m := episodeMgr(eps)

	_, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "ep-does-not-exist")
	if got := asConstructionError(t, err).Kind; got != fwmanager.NotFound {
		t.Fatalf("want NotFound, got %s", got)
	}
	if eps.lastTraceEpisodeID != "" {
		t.Fatalf("ReadTraceEvents must not be called when the episode is unresolved, got episodeID=%q", eps.lastTraceEpisodeID)
	}
}

// Test_GetEpisodeTimeline_GapRecord_ReturnsEmptyTimeline is the review-round F3
// fix: a gap record (built through the REAL write-path helper episodeGapRecord —
// captureEpisode's own gap-construction call, constructactivity.go) has no
// trace file (TracePath stays nil). Before this fix GetEpisodeTimeline
// unconditionally called ReadTraceEvents, which fails NotFound for a gap —
// indistinguishable from an unknown episodeID. The never-silent gap doctrine
// says a gap is a PRESENT outcome: the record must resolve, with an empty (not
// nil, not erroring) timeline.
func Test_GetEpisodeTimeline_GapRecord_ReturnsEmptyTimeline(t *testing.T) {
	gap := episodeGapRecord(episode.EpisodeKindConstruction, "C-Orders", nil, "gap-ep-1", "the run reported a gap episode", time.Now().UTC())
	if gap.TracePath != nil {
		t.Fatalf("sanity: episodeGapRecord must leave TracePath nil, got %v", gap.TracePath)
	}
	eps := &fakeEpisodes{listRecords: []episode.EpisodeRecord{gap}}
	m := episodeMgr(eps)

	got, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "gap-ep-1")
	if err != nil {
		t.Fatalf("GetEpisodeTimeline: unexpected error for a gap record: %v", err)
	}
	if got.Record.EpisodeID != "gap-ep-1" || got.Record.Outcome != EpisodeGap {
		t.Fatalf("Record = %+v, want the gap record itself", got.Record)
	}
	if len(got.Events) != 0 {
		t.Fatalf("Events = %+v, want empty (no trace file for a gap)", got.Events)
	}
	if eps.lastTraceEpisodeID != "" {
		t.Fatalf("ReadTraceEvents must not be called when TracePath is nil, got episodeID=%q", eps.lastTraceEpisodeID)
	}
}

// Test_GetEpisodeTimeline_TraceFileNotFound_ReturnsEmptyTimeline covers the
// sibling case: TracePath IS set (a non-gap record) but the RA can no longer
// resolve it (e.g. a pruned local trace file) — ReadTraceEvents returns
// fwra.NotFound. Same empty-timeline treatment, not an error.
func Test_GetEpisodeTimeline_TraceFileNotFound_ReturnsEmptyTimeline(t *testing.T) {
	rec := sampleEpisodeRecord("ep-1", "C-Orders")
	eps := &fakeEpisodes{
		listRecords: []episode.EpisodeRecord{rec},
		traceErr:    fwra.New(fwra.NotFound, "no trace file for episode ep-1"),
	}
	m := episodeMgr(eps)

	got, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "ep-1")
	if err != nil {
		t.Fatalf("GetEpisodeTimeline: unexpected error when the trace file is gone: %v", err)
	}
	if len(got.Events) != 0 {
		t.Fatalf("Events = %+v, want empty (trace file unresolvable)", got.Events)
	}
}

func Test_GetEpisodeTimeline_EmptyProjectID_ContractMisuse(t *testing.T) {
	m := episodeMgr(&fakeEpisodes{})
	_, err := m.GetEpisodeTimeline(testCtx(), ProjectID(""), "ep-1")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_GetEpisodeTimeline_EmptyEpisodeID_ContractMisuse(t *testing.T) {
	m := episodeMgr(&fakeEpisodes{})
	_, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_GetEpisodeTimeline_ListError_MapsInfrastructure(t *testing.T) {
	eps := &fakeEpisodes{listErr: fwra.New(fwra.Infrastructure, "ledger unavailable")}
	m := episodeMgr(eps)
	_, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "ep-1")
	if got := asConstructionError(t, err).Kind; got != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure, got %s", got)
	}
}

func Test_GetEpisodeTimeline_TraceReadError_MapsInfrastructure(t *testing.T) {
	eps := &fakeEpisodes{
		listRecords: []episode.EpisodeRecord{sampleEpisodeRecord("ep-1", "C-Orders")},
		traceErr:    fwra.New(fwra.Infrastructure, "trace file unavailable"),
	}
	m := episodeMgr(eps)
	_, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "ep-1")
	if got := asConstructionError(t, err).Kind; got != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure, got %s", got)
	}
}
