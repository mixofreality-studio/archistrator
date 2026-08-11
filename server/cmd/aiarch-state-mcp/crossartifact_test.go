package main

// crossartifact_test.go — the app-side cross-artifact rules (crossartifact.go) +
// their staleness/slot-scope severity wiring (staleness.go).
//
//   - the ACT-* System×ActivityList coverage join (ACT-COMPONENT-COVERAGE /
//     ACT-UNKNOWN-COMPONENT) is RETIRED 2026-08-09 — coverage moved from validation
//     to derivation (TestACTRulesAreRetired pins that it never fires again, and
//     TestPARulesSurviveTheACTDeletion pins that the deletion did not take out the
//     unrelated PA-* family sharing this file);
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

// driftFixtureProject builds the gtdapp drift shape on the deadlock fixture's System
// (WebClient/GtdManager/TaskAccess/AgentAccess/TaskDB, known-clean against the
// framework System rules), with a committed activityList still carrying the same
// stale-name residue and uncovered components the retired ACT-* rules used to catch
// (kept as a realistic fixture shape, not because anything still reads it that way),
// plus a committed PlanningAssumptions carrying an orphan rateCard key so PA-* findings
// are reachable off this fixture too. staleActivityList steers the activityList slot's
// StaleBasis flag (still exercised by the non-ACT staleness/slot-scope policy tests).
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
			codingActivity("persistence-access-coding", "Detailed Design & Construction — Persistence Access"),
			codingActivity("integrate-app-server-components", "Integration — wire the components together"),
			{Name: "provision-task-db", Title: "Provision Task DB", Coding: false, WorkerClass: "senior-developer", EffortDays: 5, RiskBucket: 1},
		}},
	}
	p.PlanningAssumptions = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.PlanningAssumptions{
			Resources:           []string{"Architect"},
			CalendarDaysPerWeek: 5,
			InfrastructureKind:  projectstate.InfrastructureKindGoTemporalPostgres,
			RateCard: map[string]projectstate.WorkerRateSpec{
				"Architect": {ModelID: "opus", MegatokensInPerDay: 1, MegatokensOutPerDay: 1},
				// The gtdapp-live orphan shape: no matching Resources entry.
				"CaptureEngineer": {ModelID: "sonnet", MegatokensInPerDay: 1, MegatokensOutPerDay: 1},
			},
		},
	}
	return p
}

// coveringActivityList returns an activity list with exactly one coding activity per
// code component of amendedSystemModel.
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

// ACT-* is deleted: coverage is now true by construction (the activity list is DERIVED
// from the committed System), which is strictly stronger than a validation gate. A
// lingering ACT-* finding would reject writes for drift that can no longer exist.
func TestACTRulesAreRetired(t *testing.T) {
	p := driftFixtureProject(true)
	findings := appendAppSideCrossArtifactFindings(p, nil)
	for _, f := range findings {
		if strings.HasPrefix(string(f.RuleID), "ACT-") {
			t.Errorf("ACT-* rule %q still fires; coverage is now structural", f.RuleID)
		}
	}
}

// The PA-* and DH-* families share this file and enforcement seam and must SURVIVE the
// ACT-* deletion.
func TestPARulesSurviveTheACTDeletion(t *testing.T) {
	p := driftFixtureProject(false)
	findings := appendAppSideCrossArtifactFindings(p, nil)
	var sawPA bool
	for _, f := range findings {
		if strings.HasPrefix(string(f.RuleID), "PA-") {
			sawPA = true
		}
	}
	if !sawPA {
		t.Error("no PA-* finding survived; the deletion took out an unrelated rule family")
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

// TestAttributeRule_AppSideRules pins the slot attribution of the app-side rules that
// survive the ACT-* deletion: PA-* is owned by the planningAssumptions slot — so a
// session amending a DIFFERENT slot is never deadlocked by it.
func TestAttributeRule_AppSideRules(t *testing.T) {
	cases := []struct {
		rule methodcheck.RuleID
		kind projectstate.ArtifactKind
	}{
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
