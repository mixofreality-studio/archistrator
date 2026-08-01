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
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"
)

// This file is the appgen migration's registered-Temporal-name gate: it replaces
// the original spec's unimplementable "the registered name set must never
// change" freeze with something implementable — any future rename (workflow OR
// activity) becomes a deliberate, reviewable diff against the golden list below,
// instead of a silent drift nobody notices until a production worker fails to
// find a workflow/activity type.
//
// All five Managers now have ZERO custom Activities (B7-B10): every registered
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
// and activity name registered across all five Managers. A mismatch here means
// the registered Temporal name set changed — review the diff, confirm it is
// deliberate (e.g. an activity rename, which IS allowed per the ratified "clean
// cut" — Global Constraints), and update this literal.
var registeredTemporalNamesGolden = []string{
	"agenticJobAccess.cancelAgenticJob",
	"agenticJobAccess.cancelAgenticJob",
	"agenticJobAccess.cancelAgenticJob",
	"agenticJobAccess.observeAgenticJob",
	"agenticJobAccess.observeAgenticJob",
	"agenticJobAccess.observeAgenticJob",
	"agenticJobAccess.submitAgenticJob",
	"agenticJobAccess.submitAgenticJob",
	"agenticJobAccess.submitAgenticJob",
	"artifactAccess.retrieveConstructionOutput",
	"artifactAccess.retrieveConstructionOutput",
	"artifactAccess.retrieveOutputTree",
	"artifactAccess.retrieveOutputTree",
	"artifactAccess.storeConstructionOutput",
	"artifactAccess.storeConstructionOutput",
	"billingCloseCycle",
	"billingOnboardPayment",
	"billingRegisterCustomer",
	"billingShortfallSweep",
	"billingStateAccess.bindGatewayLive",
	"billingStateAccess.readBilling",
	"billingStateAccess.readPersistentlyDelinquentCustomers",
	"billingStateAccess.registerCustomer",
	"billingStateAccess.resettleCycle",
	"billingStateAccess.settleCycle",
	"constructionConstructActivity",
	"constructionProjectSupervision",
	"constructionPumpNextActivity",
	"constructionReplanSweep",
	"constructionTransitionAccess.recordActivityExited",
	"constructionTransitionAccess.recordActivityFailed",
	"constructionTransitionAccess.recordChangeReviewed",
	"constructionTransitionAccess.recordOperatorPaused",
	"constructionTransitionAccess.recordPhaseArtifactProduced",
	"constructionTransitionAccess.recordPhaseCompleted",
	"constructionTransitionAccess.recordPhaseStarted",
	"constructionTransitionAccess.recordReviewPolicy",
	"constructionTransitionAccess.recordServiceContractProduced",
	"designSessionAccess.commitArtifactWithProvenance",
	"designSessionAccess.commitArtifactWithProvenance",
	"designSessionAccess.commitArtifactWithProvenance",
	"designSessionAccess.readProjectOnBranch",
	"designSessionAccess.readProjectOnBranch",
	"designSessionAccess.readProjectOnBranch",
	"designSessionAccess.reconcileBranchFromMain",
	"designSessionAccess.reconcileBranchFromMain",
	"designSessionAccess.reconcileBranchFromMain",
	"designSessionAccess.rejectArtifactOnBranchWithComments",
	"designSessionAccess.rejectArtifactOnBranchWithComments",
	"designSessionAccess.rejectArtifactOnBranchWithComments",
	"designSessionAccess.seedReviewCommentsOnBranch",
	"designSessionAccess.seedReviewCommentsOnBranch",
	"designSessionAccess.seedReviewCommentsOnBranch",
	"designSessionAccess.setReviewCommentStatusOnBranch",
	"designSessionAccess.setReviewCommentStatusOnBranch",
	"designSessionAccess.setReviewCommentStatusOnBranch",
	"designSessionAccess.stageArtifactForReviewOnBranch",
	"designSessionAccess.stageArtifactForReviewOnBranch",
	"designSessionAccess.stageArtifactForReviewOnBranch",
	"designSessionAccess.withdrawArtifactOnBranch",
	"designSessionAccess.withdrawArtifactOnBranch",
	"designSessionAccess.withdrawArtifactOnBranch",
	"gitActivityStatusAccess.recordActivityArchApproved",
	"gitActivityStatusAccess.recordActivityBranchOpened",
	"gitActivityStatusAccess.recordActivityCIObserved",
	"gitActivityStatusAccess.recordActivityCompleted",
	"gitActivityStatusAccess.recordActivityMerged",
	"gitActivityStatusAccess.recordActivityStarted",
	"merchantGatewayAccess.chargeCustomer",
	"merchantGatewayAccess.validateStoredInstrument",
	"messageBus.deliverSignal",
	"messageBus.deliverSignal",
	"messageBus.deliverSignal",
	"messageBus.registerSchedule",
	"messageBus.registerSchedule",
	"messageBus.registerSchedule",
	"operatedRuntimeAccess.getApplicationHealth",
	"operatedRuntimeAccess.getSloStatus",
	"operatedRuntimeAccess.publishDesiredState",
	"operatedRuntimeAccess.readComputeAttribution",
	"operatedRuntimeAccess.wirePaymentConfig",
	"operatedRuntimeAccess.withdraw",
	"operatedSystemStateAccess.publishDesiredState",
	"operatedSystemStateAccess.readInFlightOperatedApps",
	"operatedSystemStateAccess.readOperatedSystem",
	"operatedSystemStateAccess.recordDelinquencyAction",
	"operatedSystemStateAccess.recordRuntimeStatusChange",
	"operatedSystemStateAccess.withdrawSystem",
	"operationsCostProjection",
	"operationsDelinquencyEnforcement",
	"operationsDeploy",
	"operationsOperatedSystemView",
	"operationsReconcile",
	"operationsWithdraw",
	"projectDesignCoAuthor",
	"projectDesignPhaseAdvance",
	"projectDesignSDPReview",
	"projectStateAccess.acknowledgeStaleBasis",
	"projectStateAccess.acknowledgeStaleBasis",
	"projectStateAccess.acknowledgeStaleBasis",
	"projectStateAccess.advancePhase",
	"projectStateAccess.advancePhase",
	"projectStateAccess.advancePhase",
	"projectStateAccess.commitArtifact",
	"projectStateAccess.commitArtifact",
	"projectStateAccess.commitArtifact",
	"projectStateAccess.createProject",
	"projectStateAccess.createProject",
	"projectStateAccess.createProject",
	"projectStateAccess.listProjects",
	"projectStateAccess.listProjects",
	"projectStateAccess.listProjects",
	"projectStateAccess.readProject",
	"projectStateAccess.readProject",
	"projectStateAccess.readProject",
	"projectStateAccess.readProjectVersion",
	"projectStateAccess.readProjectVersion",
	"projectStateAccess.readProjectVersion",
	"projectStateAccess.setOperatingModel",
	"projectStateAccess.setOperatingModel",
	"projectStateAccess.setOperatingModel",
	"projectStateAccess.setResearchInput",
	"projectStateAccess.setResearchInput",
	"projectStateAccess.setResearchInput",
	"revenueLedgerAccess.readRange",
	"revenueLedgerAccess.recordInboundRevenue",
	"revenueLedgerAccess.recordReversal",
	"sourceControlAccess.adoptProjectRepo",
	"sourceControlAccess.adoptProjectRepo",
	"sourceControlAccess.adoptProjectRepo",
	"sourceControlAccess.commitManagedFiles",
	"sourceControlAccess.commitManagedFiles",
	"sourceControlAccess.commitManagedFiles",
	"sourceControlAccess.configureBranchProtection",
	"sourceControlAccess.configureBranchProtection",
	"sourceControlAccess.configureBranchProtection",
	"sourceControlAccess.getInstallationToken",
	"sourceControlAccess.getInstallationToken",
	"sourceControlAccess.getInstallationToken",
	"sourceControlAccess.getPullRequestStatus",
	"sourceControlAccess.getPullRequestStatus",
	"sourceControlAccess.getPullRequestStatus",
	"sourceControlAccess.installAuthorizeApp",
	"sourceControlAccess.installAuthorizeApp",
	"sourceControlAccess.installAuthorizeApp",
	"sourceControlAccess.mergePullRequest",
	"sourceControlAccess.mergePullRequest",
	"sourceControlAccess.mergePullRequest",
	"sourceControlAccess.openBranch",
	"sourceControlAccess.openBranch",
	"sourceControlAccess.openBranch",
	"sourceControlAccess.openPullRequest",
	"sourceControlAccess.openPullRequest",
	"sourceControlAccess.openPullRequest",
	"sourceControlAccess.postReview",
	"sourceControlAccess.postReview",
	"sourceControlAccess.postReview",
	"sourceControlAccess.syncManagedScaffold",
	"sourceControlAccess.syncManagedScaffold",
	"sourceControlAccess.syncManagedScaffold",
	"systemDesignCoAuthor",
	"systemDesignPhase",
	"systemDesignPhaseAdvance",
	"usageAccess.readRange",
	"usageAccess.readRange",
	"usageAccess.recordComputeUsage",
	"usageAccess.recordComputeUsage",
	"usageAccess.recordFinalUsage",
	"usageAccess.recordFinalUsage",
}

// mustRegisteredNames builds the given Manager with an all-nil/zero-value
// dependency set (safe: every New*Manager constructor is a bare field-assignment
// builder, and WorkerManifest() only stores deps — it never calls a method on
// them) and drives it through the REAL RegisterManagerWorker entrypoint against a
// fakeRegistry, returning every workflow + activity name it registers.
func mustRegisteredNames(t *testing.T) []string {
	t.Helper()

	var reg fakeRegistry

	billingMgr := billing.NewBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	billing.RegisterManagerWorker(&reg, billingMgr)

	constructionMgr := construction.NewConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	construction.RegisterManagerWorker(&reg, constructionMgr)

	operationsMgr := operations.NewOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	operations.RegisterManagerWorker(&reg, operationsMgr)

	projectDesignMgr := projectdesign.NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	projectdesign.RegisterManagerWorker(&reg, projectDesignMgr)

	systemDesignMgr := systemdesign.NewSystemDesignManager(nil, nil, nil, nil, nil, nil, nil, "")
	systemdesign.RegisterManagerWorker(&reg, systemDesignMgr)

	all := append([]string{}, reg.workflows...)
	all = append(all, reg.activities...)
	return all
}

// TestRegisteredTemporalNamesGolden collects every registered workflow +
// activity name across all five Managers and compares the sorted set against
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

// TestRegisteredTemporalNamesGolden_FrozenWorkflowNames asserts the 20
// externally-referenced workflow names (Global Constraints) are present
// verbatim — these are the ones external Temporal starters key off, and must
// never silently disappear even if the golden above is updated for an
// activity-only rename.
func TestRegisteredTemporalNamesGolden_FrozenWorkflowNames(t *testing.T) {
	frozen := []string{
		"billingOnboardPayment",
		"billingRegisterCustomer",
		"billingCloseCycle",
		"billingShortfallSweep",
		"constructionPumpNextActivity",
		"constructionConstructActivity",
		"constructionReplanSweep",
		"constructionProjectSupervision",
		"projectDesignCoAuthor",
		"projectDesignSDPReview",
		"projectDesignPhaseAdvance",
		"systemDesignPhase",
		"systemDesignCoAuthor",
		"systemDesignPhaseAdvance",
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
