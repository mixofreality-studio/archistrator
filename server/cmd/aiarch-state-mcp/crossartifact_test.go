package main

// crossartifact_test.go — the app-side cross-artifact rules (crossartifact.go) +
// their staleness/slot-scope severity wiring (staleness.go). Pins the gtdapp drift
// class end-to-end through the real `validate` gate:
//
//   - a System amended (renamed + added components) over an UNRECONCILED committed
//     activityList → ACT-COMPONENT-COVERAGE errors + ACT-UNKNOWN-COMPONENT warning,
//     gate FAILS in whole-document mode;
//   - the SAME drift with the activityList slot stale-flagged → advisory downgrade,
//     gate PASSES (reconciliation pending by design);
//   - a fully covering activity list → clean pass, zero ACT findings;
//   - rateCard orphan / defaulted classes → PA-RATECARD-KEYS / PA-RATECARD-DEFAULTED
//     advisories (never gate-blocking).

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// coveragePair is a shorthand coding activity in the gtdapp `<componentId>-coding`
// naming convention.
func codingActivity(name, title string) projectstate.ActivityItem {
	return projectstate.ActivityItem{Name: name, Title: title, Coding: true, WorkerClass: "junior-developer", EffortDays: 5, RiskBucket: 2}
}

// driftFixtureProject reproduces the gtdapp drift shape on the deadlock fixture's
// System (WebClient/GtdManager/TaskAccess/AgentAccess/TaskDB, known-clean against the
// framework System rules): the committed activityList covers WebClient + GtdManager,
// still carries a STALE `persistence-access-coding` activity (the pre-rename name — it
// derives no component), and has NOTHING for TaskAccess or AgentAccess. Integration +
// noncoding activities are present and must stay exempt. staleActivityList steers the
// activityList slot's StaleBasis flag.
func driftFixtureProject(staleActivityList bool) projectstate.Project {
	p := minimalProject()
	p.Mission = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.MissionStatement{
			Vision:     "A platform for capturing and clarifying commitments.",
			Objectives: []projectstate.Objective{{Number: 1, Statement: "Quick turnaround of new workflows."}},
			Mission:    "Design and build components the team assembles into GTD applications.",
		},
	}
	p.SystemDesign = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model:  amendedSystemModel(),
	}
	p.ActivityList = projectstate.ArtifactSlot{
		Status:     projectstate.ReviewCommitted,
		StaleBasis: staleActivityList,
		Model: &projectstate.ActivityList{Activities: []projectstate.ActivityItem{
			codingActivity("web-client-coding", "Detailed Design & Construction — WebClient"),
			codingActivity("gtd-manager-coding", "Detailed Design & Construction — GtdManager"),
			// The pre-rename residue: derives NO component in the amended System.
			codingActivity("persistence-access-coding", "Detailed Design & Construction — Persistence Access"),
			// Exempt shapes: integration (coding) + noncoding provisioning.
			codingActivity("integrate-app-server-components", "Integration — wire the components together"),
			{Name: "provision-task-db", Title: "Provision Task DB", Coding: false, WorkerClass: "senior-developer", EffortDays: 5, RiskBucket: 1},
		}},
	}
	return p
}

// coveringActivityList returns an activity list with exactly one coding activity per
// code component of amendedSystemModel — the clean-pass counterpart.
func coveringActivityList() *projectstate.ActivityList {
	return &projectstate.ActivityList{Activities: []projectstate.ActivityItem{
		codingActivity("web-client-coding", "Detailed Design & Construction — WebClient"),
		codingActivity("gtd-manager-coding", "Detailed Design & Construction — GtdManager"),
		codingActivity("task-access-coding", "Detailed Design & Construction — TaskAccess"),
		codingActivity("agent-access-coding", "Detailed Design & Construction — AgentAccess"),
		codingActivity("integrate-app-server-components", "Integration — wire the components together"),
		{Name: "provision-task-db", Title: "Provision Task DB", Coding: false, WorkerClass: "senior-developer", EffortDays: 5, RiskBucket: 1},
	}}
}

// TestValidate_ActivityCoverageDriftFails — the gtdapp drift with a FRESH (not stale)
// activityList: the gate must FAIL on the uncovered components and surface the
// stale-name warning; the integration + noncoding activities stay exempt.
func TestValidate_ActivityCoverageDriftFails(t *testing.T) {
	root := seedValidateRoot(t, driftFixtureProject(false))
	var out bytes.Buffer
	err := runValidate([]string{"--root", root}, &out)
	if err == nil {
		t.Fatalf("validate must FAIL on uncovered components with a fresh activityList, log:\n%s", out.String())
	}
	log := out.String()
	for _, want := range []string{"ACT-COMPONENT-COVERAGE", "ra1", "TaskAccess", "ra2", "AgentAccess"} {
		if !strings.Contains(log, want) {
			t.Errorf("gate log must name %q, got:\n%s", want, log)
		}
	}
	if !strings.Contains(log, "ACT-UNKNOWN-COMPONENT") || !strings.Contains(log, "persistence-access-coding") {
		t.Errorf("gate log must carry the stale-name ACT-UNKNOWN-COMPONENT warning, got:\n%s", log)
	}
	// Covered components and exempt activities produce no findings. The live
	// design-health tier (DH-* rules) is appended at this seam too, but it does not
	// flag this fixture's covered components: the GtdManager "m1" orchestrates two
	// ResourceAccess components, which is a valid Method design (DH-GRAPH-MANAGER-EMPTY
	// fires only on a manager orchestrating nothing), so the whole-log check holds.
	for _, unwanted := range []string{`"c1"`, `"m1"`, "integrate-app-server-components", "provision-task-db"} {
		if strings.Contains(log, unwanted) {
			t.Errorf("gate log must NOT flag %q, got:\n%s", unwanted, log)
		}
	}
}

// TestValidate_StaleActivityListDowngradesCoverage — the same drift while the
// activityList slot is stale-flagged: reconciliation is pending by design, the
// coverage errors downgrade to annotated warnings, the gate PASSES.
func TestValidate_StaleActivityListDowngradesCoverage(t *testing.T) {
	root := seedValidateRoot(t, driftFixtureProject(true))
	var out bytes.Buffer
	if err := runValidate([]string{"--root", root}, &out); err != nil {
		t.Fatalf("validate must PASS with a stale activityList, got: %v\n%s", err, out.String())
	}
	log := out.String()
	if !strings.Contains(log, "ACT-COMPONENT-COVERAGE") {
		t.Fatalf("gate log must still SURFACE the coverage findings (advisory), got:\n%s", log)
	}
	if strings.Contains(log, "ERROR") {
		t.Fatalf("no ERROR finding may survive the downgrade, got:\n%s", log)
	}
	if !strings.Contains(log, "downgraded to warning") || !strings.Contains(log, "activityList slot is flagged stale-basis") {
		t.Fatalf("the downgrade must be stated with its reason in the gate log, got:\n%s", log)
	}
}

// TestValidate_ActivityCoverageCleanPass — one coding activity per code component:
// zero ACT findings, gate passes.
func TestValidate_ActivityCoverageCleanPass(t *testing.T) {
	p := driftFixtureProject(false)
	p.ActivityList.Model = coveringActivityList()
	root := seedValidateRoot(t, p)
	var out bytes.Buffer
	if err := runValidate([]string{"--root", root}, &out); err != nil {
		t.Fatalf("validate must PASS with full coverage, got: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "ACT-") {
		t.Fatalf("a fully covering activity list must produce zero ACT findings, got:\n%s", out.String())
	}
}

// TestValidate_ActCoverageSlotScoping — the slot-scoped fallback: for a session whose
// ambient slot is System (the amendment that CREATED the drift), the ActivityList-
// attributed ACT errors are other-slot findings and downgrade (PASS); for the
// ActivityList's own amendment session they keep full severity (FAIL).
func TestValidate_ActCoverageSlotScoping(t *testing.T) {
	root := seedValidateRoot(t, driftFixtureProject(false))

	var out bytes.Buffer
	if err := runValidate([]string{"--root", root, "--slot", "System"}, &out); err != nil {
		t.Fatalf("--slot System must PASS (ACT errors are other-slot), got: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "pre-existing on the activityList slot") {
		t.Fatalf("the slot-scope downgrade must name the owning slot, got:\n%s", out.String())
	}

	out.Reset()
	if err := runValidate([]string{"--root", root, "--slot", "ActivityList"}, &out); err == nil {
		t.Fatalf("--slot ActivityList must FAIL on its own slot's coverage errors, log:\n%s", out.String())
	}
}

// TestDeriveActivityComponent_JoinConvention pins the activity→component join:
// normalized longest-key containment over the activity name AND title (title
// truncated at '('), against component IDs, Names, and ContractKeys. This join no
// longer mirrors the construction pump's dispatch join — dispatch now resolves via
// the AUTHORED ActivityItem.ComponentID field, exact-matched (constructionmanager.go);
// resolveComponentID/eligibility.go, the function this once pinned parity with, was
// deleted by construction-dispatch-componentid. See crossartifact.go's package
// comment for the full explanation.
func TestDeriveActivityComponent_JoinConvention(t *testing.T) {
	contractKey := "billingGatewayAccess"
	comps := []projectstate.Component{
		{ID: "webapp-client", Name: "Webapp Client", Kind: projectstate.CompClient},
		{ID: "estimation-engine", Name: "EstimationEngine", Kind: projectstate.CompEngine},
		{ID: "operation-estimation-engine", Name: "OperationEstimationEngine", Kind: projectstate.CompEngine},
		{ID: "merchant-gateway-access", Name: "MerchantGatewayAccess", Kind: projectstate.CompResourceAccess, ContractKey: &contractKey},
		{ID: "security", Name: "Security", Kind: projectstate.CompUtility},
	}
	cases := []struct {
		name, title string
		want        string
		found       bool
	}{
		// gtdapp shape: name == "<componentId>-coding".
		{"webapp-client-coding", "Detailed Design & Construction — Webapp Client", "webapp-client", true},
		// archistrator shape: opaque corpus id, "Build <ComponentName>" title.
		{"C-EE", "Build EstimationEngine", "estimation-engine", true},
		// Longest match wins: the operation-estimation activity must NOT land on
		// estimation-engine.
		{"operation-estimation-engine-coding", "Build OperationEstimationEngine", "operation-estimation-engine", true},
		// Title truncates at '(' exactly like dispatch.
		{"C-SE", "Build Security utility (cappuccino-machine test)", "security", true},
		// ContractKey is a join key too (the design-side declaration of the dispatch key).
		{"C-BG", "Build Billing Gateway Access", "merchant-gateway-access", true},
		// A renamed-away activity derives nothing.
		{"persistence-access-coding", "Detailed Design & Construction — Persistence Access", "", false},
	}
	for _, c := range cases {
		got, found := deriveActivityComponent(projectstate.ActivityItem{Name: c.name, Title: c.title}, comps)
		if got != c.want || found != c.found {
			t.Errorf("deriveActivityComponent(%q, %q) = (%q, %v), want (%q, %v)", c.name, c.title, got, found, c.want, c.found)
		}
	}
}

// TestValidate_RateCardOrphanAndDefaulted — an orphan rateCard key warns, resource
// classes riding the silent default are listed, and neither blocks the gate.
func TestValidate_RateCardOrphanAndDefaulted(t *testing.T) {
	p := driftFixtureProject(false)
	p.ActivityList.Model = coveringActivityList() // keep the ACT rules quiet
	p.PlanningAssumptions = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.PlanningAssumptions{
			Resources:           []string{"Architect", "Capture-Engineer"},
			CalendarDaysPerWeek: 5,
			// Healthy enums so PA-INFRA-KIND / PA-TERMS-REGIME stay quiet — this test
			// exercises ONLY the rateCard rules.
			InfrastructureKind: projectstate.InfrastructureKindGoTemporalPostgres,
			RateCard: map[string]projectstate.WorkerRateSpec{
				"Architect": {ModelID: "opus", MegatokensInPerDay: 1, MegatokensOutPerDay: 1},
				// The gtdapp-live orphan shape: hyphens dropped relative to the resource.
				"CaptureEngineer": {ModelID: "sonnet", MegatokensInPerDay: 1, MegatokensOutPerDay: 1},
			},
		},
	}
	root := seedValidateRoot(t, p)
	var out bytes.Buffer
	if err := runValidate([]string{"--root", root}, &out); err != nil {
		t.Fatalf("rateCard advisories must never block the gate, got: %v\n%s", err, out.String())
	}
	log := out.String()
	if !strings.Contains(log, "PA-RATECARD-KEYS") || !strings.Contains(log, `"CaptureEngineer"`) {
		t.Errorf("gate log must warn on the orphan rateCard key, got:\n%s", log)
	}
	if strings.Contains(log, `"Architect"`) {
		t.Errorf("a matching rateCard key must NOT warn, got:\n%s", log)
	}
	if !strings.Contains(log, "PA-RATECARD-DEFAULTED") || !strings.Contains(log, "Capture-Engineer") {
		t.Errorf("gate log must list the silently-defaulted resource class, got:\n%s", log)
	}
}

// TestValidate_RateCardAllKeyed — every resource keyed, no orphans: zero PA findings.
func TestValidate_RateCardAllKeyed(t *testing.T) {
	p := minimalProject()
	p.PlanningAssumptions = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.PlanningAssumptions{
			Resources:           []string{"Architect"},
			CalendarDaysPerWeek: 5,
			// Healthy enums so PA-INFRA-KIND / PA-TERMS-REGIME stay quiet here too.
			InfrastructureKind: projectstate.InfrastructureKindGoTemporalPostgres,
			RateCard: map[string]projectstate.WorkerRateSpec{
				"Architect": {ModelID: "opus", MegatokensInPerDay: 1, MegatokensOutPerDay: 1},
			},
		},
	}
	root := seedValidateRoot(t, p)
	var out bytes.Buffer
	if err := runValidate([]string{"--root", root}, &out); err != nil {
		t.Fatalf("validate must PASS, got: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "PA-RATECARD") {
		t.Fatalf("a fully keyed rateCard must produce zero PA findings, got:\n%s", out.String())
	}
}

// TestApplyActivityListStaleDowngrades_Unit pins the pure policy — the exact mirror of
// TestApplyStaleBasisDowngrades_Unit for the System×ActivityList join: only Error
// findings of the ACT join-rule set downgrade, only when the activityList slot is
// stale; PA-* and framework rules pass through untouched in both cases.
func TestApplyActivityListStaleDowngrades_Unit(t *testing.T) {
	findings := []methodcheck.Finding{
		{RuleID: "ACT-COMPONENT-COVERAGE", Severity: methodcheck.SeverityError, Message: "uncovered component"},
		{RuleID: "ACT-UNKNOWN-COMPONENT", Severity: methodcheck.SeverityWarning, Message: "stale name"},
		{RuleID: "PA-RATECARD-KEYS", Severity: methodcheck.SeverityWarning, Message: "orphan key"},
		{RuleID: "CUC-CARD", Severity: methodcheck.SeverityError, Message: "zero core use cases"},
	}

	fresh := projectstate.Project{}
	if got := applyActivityListStaleDowngrades(fresh, findings); !equalFindings(got, findings) {
		t.Fatalf("not-stale project must pass findings through untouched, got: %+v", got)
	}

	stale := projectstate.Project{ActivityList: projectstate.ArtifactSlot{StaleBasis: true}}
	got := applyActivityListStaleDowngrades(stale, findings)
	if got[0].Severity != methodcheck.SeverityWarning || !strings.Contains(got[0].Message, "downgraded") {
		t.Fatalf("ACT-COMPONENT-COVERAGE error must downgrade to an annotated warning, got: %+v", got[0])
	}
	if got[1] != findings[1] || got[2] != findings[2] {
		t.Fatalf("sub-Error findings must pass through untouched, got: %+v / %+v", got[1], got[2])
	}
	if got[3].Severity != methodcheck.SeverityError {
		t.Fatalf("a rule outside the join set (CUC-CARD) must never downgrade, got: %+v", got[3])
	}
	if findings[0].Severity != methodcheck.SeverityError {
		t.Fatalf("applyActivityListStaleDowngrades must not mutate its input")
	}
}

// TestAttributeRule_AppSideRules pins the slot attribution of the app-side rules:
// ACT-* is owned by the activityList slot, PA-* by planningAssumptions — so a session
// amending a DIFFERENT slot is never deadlocked by them.
func TestAttributeRule_AppSideRules(t *testing.T) {
	cases := []struct {
		rule methodcheck.RuleID
		kind projectstate.ArtifactKind
	}{
		{"ACT-COMPONENT-COVERAGE", projectstate.KindActivityList},
		{"ACT-UNKNOWN-COMPONENT", projectstate.KindActivityList},
		{"PA-RATECARD-KEYS", projectstate.KindPlanningAssumptions},
		{"PA-RATECARD-DEFAULTED", projectstate.KindPlanningAssumptions},
	}
	for _, c := range cases {
		kind, class := attributeRule(c.rule)
		if class != attribSlot || kind != c.kind {
			t.Errorf("attributeRule(%s) = (%v, %v), want (%v, attribSlot)", c.rule, kind, class, c.kind)
		}
	}
}

// TestPAEnumHoles covers PA-INFRA-KIND / PA-TERMS-REGIME — the zero-value enum holes
// that killed the gtdapp SDP assembly twice (2026-07-11).
func TestPAEnumHoles(t *testing.T) {
	pa := &projectstate.PlanningAssumptions{
		Resources:          []string{"Architect"},
		InfrastructureKind: projectstate.InfrastructureKindUnknown,
		Terms: projectstate.SettlementTerms{
			RevenueSharePercent:  15,
			ComputeMarkupPercent: 20,
			// all three regime enums left at Unknown
		},
	}
	proj := projectstate.Project{}
	proj.PlanningAssumptions.Status = projectstate.ReviewCommitted
	proj.PlanningAssumptions.Model = pa

	got := paEnumHoleFindings(proj)
	byRule := map[string]int{}
	for _, f := range got {
		byRule[string(f.RuleID)]++
	}
	if byRule["PA-INFRA-KIND"] != 1 {
		t.Errorf("PA-INFRA-KIND findings = %d, want 1 (got %+v)", byRule["PA-INFRA-KIND"], got)
	}
	// revenueShare hole + computeCost hole; schedule hole needs an AUTHORED regime,
	// which Unknown regimes don't provide — so exactly 2 PA-TERMS-REGIME here.
	if byRule["PA-TERMS-REGIME"] != 2 {
		t.Errorf("PA-TERMS-REGIME findings = %d, want 2 (got %+v)", byRule["PA-TERMS-REGIME"], got)
	}

	// Healthy shape (gtdapp's repaired PA): no findings.
	pa.InfrastructureKind = projectstate.InfrastructureKindGoTemporalPostgres
	pa.Terms.RevenueShare = projectstate.RevenueShareNegotiatedRate
	pa.Terms.ComputeCost = projectstate.ComputeCostFlatMarkup
	pa.Terms.Schedule = projectstate.ScheduleMonthly
	if got := paEnumHoleFindings(proj); len(got) != 0 {
		t.Errorf("healthy PA produced findings: %+v", got)
	}

	// Regimes authored but schedule unknown → exactly the schedule finding.
	pa.Terms.Schedule = projectstate.ScheduleUnknown
	got = paEnumHoleFindings(proj)
	if len(got) != 1 || got[0].RuleID != "PA-TERMS-REGIME" {
		t.Errorf("schedule hole findings = %+v, want single PA-TERMS-REGIME", got)
	}
}
