package internal_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
)

// This file is the appgen migration's registered-Temporal-name gate: it replaces
// the original spec's unimplementable "the registered name set must never
// change" freeze with something implementable — any future rename (workflow OR
// activity) becomes a deliberate, reviewable diff against the golden list below,
// instead of a silent drift nobody notices until a production worker fails to
// find a workflow/activity type.
//
// All three Managers now have ZERO custom Activities (B7-B10): every registered
// workflow and activity comes from the generated worker.gen.go's RegisterWorker,
// fed by each Manager's hand-written WorkerManifest() (workermanifest.go). The
// collection approach here calls the REAL WorkerManifest()/RegisterManagerWorker
// path — not a static AST parse — because recon showed it is strictly less
// fragile: every New*Manager constructor (contract.gen.go) is a bare field-
// assignment builder (no nil-dep validation, no method calls on the deps), and
// every WorkerManifest() body (workermanifest.go) only stores deps into structs
// — it never calls a method on them either. So a manager built from an all-
// nil/zero-value dependency set is safe to hand to the real RegisterManagerWorker
// entrypoint, and a fake worker.Worker (embedding the interface as nil and
// overriding only the two Register*WithOptions methods RegisterWorker in
// worker.gen.go actually calls) captures the exact registered name set with zero
// duplication of the generated registration logic. An AST/parse-based collector
// (the arch_activitynames_test.go precedent) would have to hand-encode each
// manager's own mix of workermanifest.go Name literals plus worker.gen.go's
// generated RegisterActivityWithOptions Name literals — this exercises the real
// code instead.

// fakeRegistry is a minimal worker.Worker double: it embeds the interface as a
// nil value (so any method besides the two overridden below panics if reached —
// RegisterWorker in worker.gen.go never calls anything else) and records every
// registered workflow/activity name.
type fakeRegistry struct {
	worker.Worker
	workflows  []string
	activities []string
}

func (f *fakeRegistry) RegisterWorkflowWithOptions(_ any, options workflow.RegisterOptions) {
	f.workflows = append(f.workflows, options.Name)
}

func (f *fakeRegistry) RegisterActivityWithOptions(_ any, options activity.RegisterOptions) {
	f.activities = append(f.activities, options.Name)
}

// registeredTemporalNamesGolden is the committed, sorted set of every workflow
// and activity name registered across the three Managers that own a Temporal
// worker (delivery, operations, billing). A mismatch here means the registered
// Temporal name set changed — review the diff, confirm it is deliberate, and
// update this literal.
//
// STAGE 4a TOOK IT FROM 231 TO 139: NINETY-TWO REMOVALS. Stage 3's twenty-four
// additions were safe to ship on their own, because an OLD worker simply never
// serves a name it does not know and the new one does. Removals are the opposite
// direction and they are why the drain is non-negotiable: a worker built from an
// older commit can serve a workflow THIS one started, but a worker built from
// this commit cannot serve one an older worker started. Nothing here shrank by
// deleting a verb — the three design/construction Managers registered the SAME
// RA dep surface three times over (each Manager's generated RegisterWorker
// registers that Manager's whole dep set), and one Manager registers it once.
// Every workflow TYPE name is unchanged (R2); what went is the duplication.
//
// Drain the old task queues (system-design, project-design, construction) and
// delete the old Schedules (construction:pumpSweep, construction:replanSweep)
// BEFORE this deploys — see Task 10. Stage 4b2 adds delivery:replanSweep to that
// deletion list: the workflow behind it is gone, so the Schedule now fires into a
// type no worker serves, and an unregistered Schedule is not a deleted one.
//
// Deterministic component testing (Plan 2 Task 1) ADDS four: artifactAccess gained
// getTestRunIndex and readTestRunFile, registered by the two Managers that depend on
// the RA (delivery, operations). Additions are the safe direction — nothing to drain.
//
// Plan 5 round 1 ADDS four: projectStateAccess gained recordSdpReviewOutcome and
// recordTrackingPoint (the M0 non-commit outcomes and the weekly tracking points),
// registered by the two Managers that depend on the RA. Additions again: nothing to drain.
var registeredTemporalNamesGolden = []string{
	"activityExecutionAccess.appendReviewVerdict",
	"activityExecutionAccess.decideReviewRound",
	"activityExecutionAccess.openActivity",
	"activityExecutionAccess.openReviewRound",
	"activityExecutionAccess.readActivityExecution",
	"activityExecutionAccess.recordActivityOutcome",
	"activityExecutionAccess.recordAttemptOutcome",
	"activityExecutionAccess.recordOperatorNote",
	"activityExecutionAccess.setReviewCommentStatus",
	"activityExecutionAccess.stageTaskOutput",
	"agenticJobAccess.observeAgenticJob",
	"agenticJobAccess.submitAgenticJob",
	"artifactAccess.getTestRunIndex",
	"artifactAccess.getTestRunIndex",
	"artifactAccess.readTestRunFile",
	"artifactAccess.readTestRunFile",
	"artifactAccess.retrieveConstructionOutput",
	"artifactAccess.retrieveConstructionOutput",
	"artifactAccess.storeConstructionOutput",
	"artifactAccess.storeConstructionOutput",
	"billingCloseCycle",
	"billingOnboardPayment",
	"billingRegisterCustomer",
	"billingShortfallSweep",
	"billingStateAccess.bindGatewayLive",
	"billingStateAccess.readBilling",
	"billingStateAccess.readCycle",
	"billingStateAccess.readPersistentlyDelinquentCustomers",
	"billingStateAccess.registerCustomer",
	"billingStateAccess.resettleCycle",
	"billingStateAccess.settleCycle",
	"constructionProjectSupervision",
	"constructionPumpNextActivity",
	"constructionPumpSweep",
	"deliveryActivity",
	"deliveryRoundSweep",
	"designSessionAccess.commitArtifactWithProvenance",
	"designSessionAccess.readProjectOnBranch",
	"designSessionAccess.reconcileBranchFromMain",
	"designSessionAccess.seedReviewCommentsOnBranch",
	"designSessionAccess.stageArtifactForReviewOnBranch",
	"episodeAccess.appendEpisode",
	"episodeAccess.listEpisodes",
	"episodeAccess.readTraceEvents",
	"gitActivityStatusAccess.recordActivityBranchOpened",
	"gitActivityStatusAccess.recordActivityCIObserved",
	"merchantGatewayAccess.chargeCustomer",
	"merchantGatewayAccess.readStoredInstrument",
	"merchantGatewayAccess.validateStoredInstrument",
	"messageBus.deliverSignal",
	"messageBus.deliverSignal",
	"messageBus.deliverSignal",
	"messageBus.describeSchedule",
	"messageBus.describeSchedule",
	"messageBus.describeSchedule",
	"messageBus.registerSchedule",
	"messageBus.registerSchedule",
	"messageBus.registerSchedule",
	"operatedRuntimeAccess.getApplicationHealth",
	"operatedRuntimeAccess.getDeploymentResourceHealth",
	"operatedRuntimeAccess.getSloStatus",
	"operatedRuntimeAccess.publishDesiredState",
	"operatedRuntimeAccess.readComputeAttribution",
	"operatedRuntimeAccess.withdraw",
	"operatedSystemStateAccess.publishDesiredState",
	"operatedSystemStateAccess.readInFlightOperatedApps",
	"operatedSystemStateAccess.readOperatedSystem",
	"operatedSystemStateAccess.recordDelinquencyAction",
	"operatedSystemStateAccess.recordRuntimeStatusChange",
	"operatedSystemStateAccess.registerOperatedSystem",
	"operatedSystemStateAccess.withdrawSystem",
	"operationsCostProjection",
	"operationsDelinquencyEnforcement",
	"operationsDeploy",
	"operationsDeploymentHealth",
	"operationsOperatedSystemView",
	"operationsReconcile",
	"operationsRegister",
	"operationsWithdraw",
	"projectStateAccess.acknowledgeStaleBasis",
	"projectStateAccess.acknowledgeStaleBasis",
	"projectStateAccess.advancePhase",
	"projectStateAccess.advancePhase",
	"projectStateAccess.createProject",
	"projectStateAccess.createProject",
	"projectStateAccess.listProjects",
	"projectStateAccess.listProjects",
	"projectStateAccess.readProject",
	"projectStateAccess.readProject",
	"projectStateAccess.readProjectVersion",
	"projectStateAccess.readProjectVersion",
	"projectStateAccess.recordOperatorPaused",
	"projectStateAccess.recordOperatorPaused",
	"projectStateAccess.recordOperatorResumed",
	"projectStateAccess.recordOperatorResumed",
	"projectStateAccess.recordReviewPolicy",
	"projectStateAccess.recordReviewPolicy",
	"projectStateAccess.recordSdpReviewOutcome",
	"projectStateAccess.recordSdpReviewOutcome",
	"projectStateAccess.recordTrackingPoint",
	"projectStateAccess.recordTrackingPoint",
	"projectStateAccess.setOperatingModel",
	"projectStateAccess.setOperatingModel",
	"projectStateAccess.setResearchInput",
	"projectStateAccess.setResearchInput",
	"sourceControlAccess.adoptProjectRepo",
	"sourceControlAccess.commitManagedFiles",
	"sourceControlAccess.getInstallationToken",
	"sourceControlAccess.getPullRequestStatus",
	"sourceControlAccess.mergePullRequest",
	"sourceControlAccess.openBranch",
	"sourceControlAccess.openPullRequest",
	"sourceControlAccess.postReview",
	"sourceControlAccess.readManagedFile",
	"sourceControlAccess.syncManagedScaffold",
	"usageAccess.readRange",
	"usageAccess.readRange",
	"usageAccess.recordComputeUsage",
	"usageAccess.recordComputeUsage",
}

// mustRegisteredNames builds the given Manager with an all-nil/zero-value
// dependency set (safe: every New*Manager constructor is a bare field-assignment
// builder, and WorkerManifest() only stores deps — it never calls a method on
// them) and drives it through the REAL RegisterManagerWorker entrypoint against a
// fakeRegistry, returning every workflow + activity name it registers.
func mustRegisteredNames(t *testing.T) []string {
	t.Helper()

	var reg fakeRegistry

	billingMgr := billing.NewBillingManager(nil, nil, nil, nil, nil, nil, nil)
	billing.RegisterManagerWorker(&reg, billingMgr)

	deliveryMgr := delivery.NewDeliveryManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil, "")
	delivery.RegisterManagerWorker(&reg, deliveryMgr)

	operationsMgr := operations.NewOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	operations.RegisterManagerWorker(&reg, operationsMgr)

	all := append([]string{}, reg.workflows...)
	all = append(all, reg.activities...)
	return all
}

// TestRegisteredTemporalNamesGolden collects every registered workflow +
// activity name across all three Managers and compares the sorted set against
// the committed golden. A mismatch prints the full got-vs-want diff so a
// deliberate rename is a one-line update, not an archaeology exercise.
func TestRegisteredTemporalNamesGolden(t *testing.T) {
	got := mustRegisteredNames(t)
	sort.Strings(got)

	want := append([]string{}, registeredTemporalNamesGolden...)
	sort.Strings(want)

	if !equalStringSlices(got, want) {
		t.Fatalf("registered Temporal name set changed — update registeredTemporalNamesGolden if this is deliberate:\n%s",
			diffStringSlices(got, want))
	}
}

// TestRegisteredTemporalNamesGolden_FrozenWorkflowNames asserts the 15
// externally-referenced workflow names (Global Constraints) are present
// verbatim — these are the ones external Temporal starters key off, and must
// never silently disappear even if the golden above is updated for an
// activity-only rename.
//
// STAGE 4b1 TASK 13 REMOVED SEVEN of them — constructionConstructActivity,
// systemDesignPhase, systemDesignCoAuthor, systemDesignPhaseAdvance, projectDesignCoAuthor,
// projectDesignSDPReview and projectDesignPhaseAdvance — and the removal is legitimate for
// exactly one reason, which is worth stating because it is the ONLY one that discharges the
// guarantee this list encodes. The list exists so an externally-started workflow name cannot
// vanish silently: a client or a Schedule outside this codebase holds the name, so a rename or
// a deletion leaves it starting a type no worker serves. A DRAIN is what makes that harmless —
// after it there is no in-flight execution and no external starter left for these seven,
// because the twelve-op façade stopped naming them and the pump stopped starting them. The
// drain sequence is in docs/bugs/2026-09-24-stage3-rail-earmarks.md, and nothing may deploy
// this wave before it has run.
//
// The count in the first paragraph is 22 - 7 = 15, and it is stated because a doc that says 22
// beside a list of 15 is the next reader's first wrong assumption.
//
// STAGE 4b1 ADDED deliveryRoundSweep (Task 6), and a Schedule target belongs here for
// the same reason a client-started workflow does: a Temporal Schedule holds the workflow
// TYPE name as LIVE NAMESPACE STATE, so once delivery:roundSweep exists every firing
// starts that name from outside this codebase. A rename this test did not catch would
// leave the Schedule firing into a name no worker serves — a dead sweep that reports no
// error at all, which is the same silent failure the Schedule-id consts in
// deliverymanager.go warn about.
//
// STAGE 4b1 ALSO ADDED deliveryActivity (Task 8), and it belongs here for a third reason
// again: it is the type a CONTINUE-AS-NEW re-starts by name from inside the workflow, and
// a live walk's own re-start reads the name out of the running image rather than out of a
// client. A rename mid-wave would therefore break the continue of every in-flight walk —
// the run would end with a NewContinueAsNewError naming a type no worker serves — which is
// the same silent failure a renamed Schedule target causes, arriving from inside instead of
// outside. Frozen here from its FIRST commit, before the pump starts it (Task 9), because
// the cheapest moment to freeze a name is before anything depends on it.
//
// STAGE 4b2 REMOVED constructionReplanSweep AND ADDED constructionPumpSweep, and the two
// moves have the same justification read in opposite directions.
//
// This list exists so an externally-held workflow TYPE name cannot vanish silently: a
// client or a SCHEDULE outside this codebase holds the name, and a rename or a deletion
// leaves it starting a type no worker serves. constructionReplanSweep leaves it because the
// workflow is GONE and its Schedule is deleted in the same drain this wave already
// requires — there is no external starter left to protect. constructionPumpSweep joins it
// because its Schedule (delivery:pumpSweep, 30s) has held its type name as live namespace
// state since stage 4a and this list never said so; that was an omission by this list's own
// argument, earmarked at stage 4b1 and closed here.
//
// The count is 15 - 1 + 1 = 15.
func TestRegisteredTemporalNamesGolden_FrozenWorkflowNames(t *testing.T) {
	frozen := []string{
		"billingOnboardPayment",
		"billingRegisterCustomer",
		"billingCloseCycle",
		"billingShortfallSweep",
		"constructionPumpNextActivity",
		"constructionPumpSweep",
		"constructionProjectSupervision",
		"deliveryRoundSweep",
		"deliveryActivity",
		"operationsDeploy",
		"operationsReconcile",
		"operationsWithdraw",
		"operationsCostProjection",
		"operationsOperatedSystemView",
		"operationsDelinquencyEnforcement",
	}

	got := mustRegisteredNames(t)
	set := make(map[string]bool, len(got))
	for _, n := range got {
		set[n] = true
	}

	var missing []string
	for _, name := range frozen {
		if !set[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("frozen external workflow name(s) missing from the registered set: %v", missing)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// diffStringSlices renders a compact got-vs-want diff: names only in got
// (prefixed +), names only in want (prefixed -).
func diffStringSlices(got, want []string) string {
	gotSet := make(map[string]bool, len(got))
	for _, g := range got {
		gotSet[g] = true
	}
	wantSet := make(map[string]bool, len(want))
	for _, w := range want {
		wantSet[w] = true
	}

	var lines []string
	for _, g := range got {
		if !wantSet[g] {
			lines = append(lines, fmt.Sprintf("+ %s", g))
		}
	}
	for _, w := range want {
		if !gotSet[w] {
			lines = append(lines, fmt.Sprintf("- %s", w))
		}
	}
	sort.Strings(lines)

	var b strings.Builder
	fmt.Fprintf(&b, "got %d name(s), want %d name(s)\n", len(got), len(want))
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}
