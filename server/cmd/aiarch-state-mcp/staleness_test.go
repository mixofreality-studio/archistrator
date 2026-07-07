package main

// staleness_test.go — the staleness-aware Method-gate policy (staleness.go +
// validate.go): the cross-artifact deadlock fix. Proves the exact founder-ratified
// semantics:
//
//   - stale OperationalConcepts → System×OperationalConcepts DEP errors downgrade to
//     Warning (gate passes; putDraftModel accepts the System amendment);
//   - NOT-stale OperationalConcepts + the same drift → the DEP errors persist (gate
//     fails; putDraftModel rejects);
//   - single-/same-artifact errors are NEVER downgraded, stale or not.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// deadlockFixtureProject builds the smallest committed head-state that reproduces the
// live gtdapp deadlock shape: Mission + System + OperationalConcepts committed, the
// deployment topology covering the ORIGINAL component set, and the System AMENDED to
// carry one extra container-eligible component (AgentAccess) that no environment
// instances — exactly the state a System amendment session leaves on its branch.
// staleOpConcepts steers the OperationalConcepts slot's StaleBasis flag.
func deadlockFixtureProject(staleOpConcepts bool) projectstate.Project {
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
	p.OperationalConcepts = projectstate.ArtifactSlot{
		Status:     projectstate.ReviewCommitted,
		StaleBasis: staleOpConcepts,
		Model: &projectstate.OperationalConcepts{
			Decisions: []projectstate.OperationalDecision{
				{Topic: "communication topology", Decision: "direct calls, no bus", JustifyingObjective: 1},
			},
			Deployment: originalDeployment(),
		},
	}
	return p
}

// amendedSystemModel is the post-amendment System: the original chain (WebClient →
// GtdManager → TaskAccess → TaskDB) PLUS the added AgentAccess the deployment topology
// does not yet package — the DEP-COVERAGE / DEP-GRAPH-IDENTITY trigger.
func amendedSystemModel() *projectstate.System {
	return &projectstate.System{
		Components: []projectstate.Component{
			{ID: "c1", Name: "WebClient", Kind: projectstate.CompClient, Layer: projectstate.LayerClient, Encapsulates: "presentation volatility"},
			{ID: "m1", Name: "GtdManager", Kind: projectstate.CompManager, Layer: projectstate.LayerManager, Encapsulates: "workflow volatility"},
			{ID: "ra1", Name: "TaskAccess", Kind: projectstate.CompResourceAccess, Layer: projectstate.LayerResourceAccess, Encapsulates: "task storage volatility", AtomicBusinessVerbs: []string{"capture"}},
			{ID: "ra2", Name: "AgentAccess", Kind: projectstate.CompResourceAccess, Layer: projectstate.LayerResourceAccess, Encapsulates: "agent substrate volatility", AtomicBusinessVerbs: []string{"delegate"}},
			{ID: "r1", Name: "TaskDB", Kind: projectstate.CompResource, Layer: projectstate.LayerResource, Encapsulates: "durable task store"},
		},
		Relationships: []projectstate.Relationship{
			{From: "c1", To: "m1", Mode: projectstate.CallSync, Label: "capture"},
			{From: "m1", To: "ra1", Mode: projectstate.CallSync, Label: "store"},
			{From: "m1", To: "ra2", Mode: projectstate.CallSync, Label: "delegate"},
			{From: "ra1", To: "r1", Mode: projectstate.CallSync, Label: "persist"},
			{From: "ra2", To: "r1", Mode: projectstate.CallSync, Label: "persist"},
		},
	}
}

// originalDeployment covers the PRE-amendment component set only: one container
// packaging WebClient/GtdManager/TaskAccess, instanced in both required environments
// (deliveryStyle cloud → cloud + test), with TaskDB present as infrastructure. It is
// valid against the pre-amendment System and drifted against amendedSystemModel.
func originalDeployment() projectstate.DeploymentTopology {
	env := func(profile projectstate.DeploymentProfile, title string) projectstate.DeploymentEnvironment {
		return projectstate.DeploymentEnvironment{
			Profile: profile,
			Title:   title,
			Nodes: []projectstate.DeploymentNode{{
				Name:                "app-node",
				ContainerInstances:  []projectstate.ContainerInstance{{ContainerKey: "app"}},
				InfrastructureNodes: []projectstate.InfrastructureNode{{Name: "TaskDB", Technology: "postgres"}},
			}},
		}
	}
	return projectstate.DeploymentTopology{
		DeliveryStyle: projectstate.StyleCloud,
		Containers: []projectstate.DeployContainer{{
			Key: "app", Name: "App", Technology: "go",
			Components: []string{"WebClient", "GtdManager", "TaskAccess"},
		}},
		Environments: []projectstate.DeploymentEnvironment{
			env(projectstate.ProfileCloud, "cloud"),
			env(projectstate.ProfileTest, "test"),
		},
	}
}

// seedValidateRoot encodes p through the real codec into a temp checkout root and
// returns the root path — the shape `aiarch-state-mcp validate --root <root>` reads.
func seedValidateRoot(t *testing.T, p projectstate.Project) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, statePathPrefix), 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	b, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("encode fixture project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, statePathPrefix, projectFile), b, 0o644); err != nil {
		t.Fatalf("write fixture project: %v", err)
	}
	return root
}

// TestValidate_StaleOpConceptsDowngradesJoinRules — the deadlock fix: with the
// OperationalConcepts slot flagged stale-basis, the System×OperationalConcepts join
// errors (DEP-COVERAGE / DEP-GRAPH-IDENTITY) downgrade to warnings and the gate PASSES,
// with the downgrade stated honestly in the gate log.
func TestValidate_StaleOpConceptsDowngradesJoinRules(t *testing.T) {
	root := seedValidateRoot(t, deadlockFixtureProject(true))
	var out bytes.Buffer
	if err := runValidate([]string{"--root", root}, &out); err != nil {
		t.Fatalf("validate must PASS with stale opconcepts, got: %v\n%s", err, out.String())
	}
	log := out.String()
	if !strings.Contains(log, "DEP-COVERAGE") {
		t.Fatalf("gate log must still SURFACE the DEP-COVERAGE finding (advisory), got:\n%s", log)
	}
	if strings.Contains(log, "ERROR") {
		t.Fatalf("no ERROR finding may survive the downgrade, got:\n%s", log)
	}
	if !strings.Contains(log, "downgraded to warning") || !strings.Contains(log, "stale-basis") {
		t.Fatalf("the downgrade must be stated with its reason in the gate log, got:\n%s", log)
	}
	if !strings.Contains(log, "PASS") {
		t.Fatalf("gate log must state the PASS verdict, got:\n%s", log)
	}
}

// TestValidate_FreshOpConceptsDepDriftFails — the counterfactual: the SAME drift with a
// NOT-stale OperationalConcepts slot keeps its Error severity and fails the gate. The
// downgrade is keyed to the rail's staleness semantics, not to the DEP rules per se.
func TestValidate_FreshOpConceptsDepDriftFails(t *testing.T) {
	root := seedValidateRoot(t, deadlockFixtureProject(false))
	var out bytes.Buffer
	err := runValidate([]string{"--root", root}, &out)
	if err == nil {
		t.Fatalf("validate must FAIL when opconcepts is not stale, log:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "Method rule violation") {
		t.Fatalf("failure must name the Method rule violations, got: %v", err)
	}
	if !strings.Contains(out.String(), "DEP-COVERAGE") || !strings.Contains(out.String(), "ERROR") {
		t.Fatalf("gate log must carry the DEP-COVERAGE error, got:\n%s", out.String())
	}
}

// TestValidate_SameArtifactErrorNeverDowngraded — a SAME-artifact deployment error (a
// containerInstance referencing an undeclared container, DEP-CONTAINER-REF) keeps full
// severity even while the slot is stale: staleness excuses only the cross-artifact
// join, never internal incoherence of the draft itself.
func TestValidate_SameArtifactErrorNeverDowngraded(t *testing.T) {
	p := deadlockFixtureProject(true)
	oc := p.OperationalConcepts.Model.(*projectstate.OperationalConcepts)
	oc.Deployment.Environments[0].Nodes[0].ContainerInstances = append(
		oc.Deployment.Environments[0].Nodes[0].ContainerInstances,
		projectstate.ContainerInstance{ContainerKey: "ghost"},
	)
	root := seedValidateRoot(t, p)
	var out bytes.Buffer
	err := runValidate([]string{"--root", root}, &out)
	if err == nil {
		t.Fatalf("validate must FAIL on a same-artifact error despite staleness, log:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "DEP-CONTAINER-REF") {
		t.Fatalf("gate log must carry the DEP-CONTAINER-REF error, got:\n%s", out.String())
	}
}

// TestValidate_MissingAndEmptyStateAreCleanPasses mirrors methodcheck.Check's posture:
// a repo without committed .aiarch state is a clean pass, never a red gate.
func TestValidate_MissingAndEmptyStateAreCleanPasses(t *testing.T) {
	var out bytes.Buffer
	if err := runValidate([]string{"--root", t.TempDir()}, &out); err != nil {
		t.Fatalf("missing state file must be a clean pass, got: %v", err)
	}
	if !strings.Contains(out.String(), "nothing to validate") {
		t.Fatalf("clean pass must say so, got:\n%s", out.String())
	}
}

// TestApplyStaleBasisDowngrades_Unit pins the pure policy: only Error findings of the
// join-rule set downgrade, only when the slot is stale; warnings/info and non-join
// rules pass through untouched in both cases.
func TestApplyStaleBasisDowngrades_Unit(t *testing.T) {
	findings := []methodcheck.Finding{
		{RuleID: "DEP-COVERAGE", Severity: methodcheck.SeverityError, Message: "missing coverage"},
		{RuleID: "DEP-CONTAINER-REF", Severity: methodcheck.SeverityError, Message: "ghost container"},
		{RuleID: "DEP-RESOURCE-PRESENT", Severity: methodcheck.SeverityWarning, Message: "resource absent"},
		{RuleID: "CUC-CARD", Severity: methodcheck.SeverityError, Message: "zero core use cases"},
	}

	fresh := projectstate.Project{}
	if got := applyStaleBasisDowngrades(fresh, findings); !equalFindings(got, findings) {
		t.Fatalf("not-stale project must pass findings through untouched, got: %+v", got)
	}

	stale := projectstate.Project{OperationalConcepts: projectstate.ArtifactSlot{StaleBasis: true}}
	got := applyStaleBasisDowngrades(stale, findings)
	if got[0].Severity != methodcheck.SeverityWarning || !strings.Contains(got[0].Message, "downgraded") {
		t.Fatalf("DEP-COVERAGE error must downgrade to an annotated warning, got: %+v", got[0])
	}
	if got[1].Severity != methodcheck.SeverityError {
		t.Fatalf("same-artifact DEP-CONTAINER-REF must keep Error severity, got: %+v", got[1])
	}
	if got[2] != findings[2] {
		t.Fatalf("an already-Warning finding must pass through untouched, got: %+v", got[2])
	}
	if got[3].Severity != methodcheck.SeverityError {
		t.Fatalf("a single-artifact rule (CUC-CARD) must never downgrade, got: %+v", got[3])
	}
	// The input slice is never mutated (the policy returns a copy).
	if findings[0].Severity != methodcheck.SeverityError {
		t.Fatalf("applyStaleBasisDowngrades must not mutate its input")
	}
}

func equalFindings(a, b []methodcheck.Finding) bool {
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

// TestPutDraftModel_SystemAmendmentUnblockedByStaleOpConcepts proves the IN-LOOP gate
// applies the same policy — the deadlock would otherwise hit putDraftModel before CI
// ever ran. With opconcepts stale, the System draft that ADDS a component the topology
// does not yet package is accepted; with opconcepts fresh, it is rejected with the
// join-rule id visible.
func TestPutDraftModel_SystemAmendmentUnblockedByStaleOpConcepts(t *testing.T) {
	draft := []byte(`{
		"components": [
			{"id":"c1","name":"WebClient","kind":"client","layer":"client","encapsulates":"presentation volatility"},
			{"id":"m1","name":"GtdManager","kind":"manager","layer":"manager","encapsulates":"workflow volatility"},
			{"id":"ra1","name":"TaskAccess","kind":"resourceAccess","layer":"resourceAccess","encapsulates":"task storage volatility","atomicBusinessVerbs":["capture"]},
			{"id":"ra2","name":"AgentAccess","kind":"resourceAccess","layer":"resourceAccess","encapsulates":"agent substrate volatility","atomicBusinessVerbs":["delegate"]},
			{"id":"r1","name":"TaskDB","kind":"resource","layer":"resource","encapsulates":"durable task store"}
		],
		"relationships": [
			{"from":"c1","to":"m1","mode":"sync","label":"capture"},
			{"from":"m1","to":"ra1","mode":"sync","label":"store"},
			{"from":"m1","to":"ra2","mode":"sync","label":"delegate"},
			{"from":"ra1","to":"r1","mode":"sync","label":"persist"},
			{"from":"ra2","to":"r1","mode":"sync","label":"persist"}
		],
		"dynamicViews": []
	}`)

	// Stale counterpart → the amendment goes through (the deadlock fix).
	stale := deadlockFixtureProject(true)
	s, _ := seedProject(t, stale, jobModeDraft, projectstate.KindSystem)
	if err := s.putDraftModel(draft); err != nil {
		t.Fatalf("System amendment must be accepted while opconcepts is stale-basis, got: %v", err)
	}
	slot := readBackSlot(t, s, projectstate.KindSystem)
	if slot.Status != projectstate.ReviewCommitted || slot.Model == nil {
		t.Fatalf("amended System not written: %+v", slot)
	}

	// Fresh counterpart → the same draft is rejected with the join rule named.
	fresh := deadlockFixtureProject(false)
	s2, _ := seedProject(t, fresh, jobModeDraft, projectstate.KindSystem)
	err := s2.putDraftModel(draft)
	if err == nil {
		t.Fatalf("System amendment must be rejected when opconcepts is NOT stale")
	}
	if !strings.Contains(err.Error(), "DEP-COVERAGE") {
		t.Fatalf("rejection must name the DEP join rule, got: %v", err)
	}
	if s2.wroteState {
		t.Fatalf("wroteState set despite a methodcheck rejection")
	}
}
