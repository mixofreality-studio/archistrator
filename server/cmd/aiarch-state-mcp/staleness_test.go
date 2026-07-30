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
		Model: &projectstate.DeploymentOperationsModel{
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
	oc := p.OperationalConcepts.Model.(*projectstate.DeploymentOperationsModel)
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
// applies the same policies — the deadlock would otherwise hit putDraftModel before CI
// ever ran. The System draft that ADDS a component the topology does not yet package is
// accepted in BOTH cases: stale opconcepts (the staleness policy) and fresh opconcepts
// (the slot-scoped fallback — DEP is OperationalConcepts-attributed, a slot the ambient
// System session cannot write).
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

	// Stale counterpart → the amendment goes through (the staleness policy).
	stale := deadlockFixtureProject(true)
	s, _ := seedProject(t, stale, jobModeDraft, projectstate.KindSystem)
	if err := s.putDraftModel(draft); err != nil {
		t.Fatalf("System amendment must be accepted while opconcepts is stale-basis, got: %v", err)
	}
	slot := readBackSlot(t, s, projectstate.KindSystem)
	if slot.Status != projectstate.ReviewCommitted || slot.Model == nil {
		t.Fatalf("amended System not written: %+v", slot)
	}

	// Fresh counterpart → the amendment STILL goes through, via the slot-scoped
	// fallback: putDraftModel's ambient slot is System, and the DEP join rules are
	// OperationalConcepts-attributed — a slot this session cannot write. (Before
	// slot-scoping this case was rejected; the staleness policy alone left the
	// direct-amendment-on-a-fresh-base residual, which slot-scoping closes.)
	fresh := deadlockFixtureProject(false)
	s2, _ := seedProject(t, fresh, jobModeDraft, projectstate.KindSystem)
	if err := s2.putDraftModel(draft); err != nil {
		t.Fatalf("System amendment must be accepted under slot-scoping even with fresh opconcepts, got: %v", err)
	}
}

// TestPutDraftModel_AmbientSlotErrorStillRejects — the matrix row that must NOT relax:
// an error attributed to the AMBIENT slot itself (a Glossary draft carrying a
// non-canonical Four-Questions category, GLOSS-FOURQ) rejects the draft at full
// severity, staleness elsewhere notwithstanding. (The sibling case — an ambient
// CoreUseCases draft rejected on its own CUC-CARD — is pinned by
// TestPutDraftModel_MethodRuleRejected.)
func TestPutDraftModel_AmbientSlotErrorStillRejects(t *testing.T) {
	draft := []byte(`{"items":[{"term":"Inbox","definition":"where captured items land","category":"Bogus"}]}`)
	s, _ := seedProject(t, deadlockFixtureProject(true), jobModeDraft, projectstate.KindGlossary)
	err := s.putDraftModel(draft)
	if err == nil {
		t.Fatalf("a Glossary draft with a non-canonical category must be rejected (ambient-slot error)")
	}
	if !strings.Contains(err.Error(), "GLOSS-FOURQ") {
		t.Fatalf("rejection must name the ambient-slot rule, got: %v", err)
	}
	if s.wroteState {
		t.Fatalf("wroteState set despite an ambient-slot rejection")
	}
}

// ---------------------------------------------------------------------------------
// SLOT-SCOPED severity (policy 2) — the grandfathered-committed-data deadlock fix.
// ---------------------------------------------------------------------------------

// withGlossaryDefect adds a committed Glossary slot carrying a GLOSS-FOURQ error (a
// non-canonical category) to the fixture — the grandfathered sibling-slot defect shape
// (gtdapp live: UC-VARIATION-REF errors in the committed CoreUseCases slot).
func withGlossaryDefect(p projectstate.Project) projectstate.Project {
	p.Glossary = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Glossary{Items: []projectstate.GlossaryItem{
			{Term: "Inbox", Definition: "where captured items land", Category: "Bogus"},
		}},
	}
	return p
}

// TestAttributeRule_Table pins the rule→slot attribution: the subject slot owns the
// finding (including rules that READ other slots), STP-* belongs to the construction-
// owned testing state, and an unknown rule id has no attribution (full severity).
func TestAttributeRule_Table(t *testing.T) {
	cases := []struct {
		rule  methodcheck.RuleID
		kind  projectstate.ArtifactKind
		class ruleAttribution
	}{
		{"GLOSS-FOURQ", projectstate.KindGlossary, attribSlot},
		{"SR-ID-UNIQUE", projectstate.KindScrubbedRequirements, attribSlot},
		{"VOL-GLOSS", projectstate.KindVolatilities, attribSlot},
		{"CUC-CARD", projectstate.KindCoreUseCases, attribSlot},
		{"UC-VARIATION-REF", projectstate.KindCoreUseCases, attribSlot},
		{"USECASE-DYNAMIC-MISSING", projectstate.KindSystem, attribSlot},
		{"SYSTEM-LAYER-DEGENERATE", projectstate.KindSystem, attribSlot},
		{"SYS-NAME-UNIQUE", projectstate.KindSystem, attribSlot},
		{"DV-STATIC-COVERAGE", projectstate.KindSystem, attribSlot},
		{"ARCH-CHAINCOV", projectstate.KindSystem, attribSlot},
		{"APPC-CARD-SUB-MGR", projectstate.KindSystem, attribSlot},
		{"OPC-OBJREF", projectstate.KindOperationalConcepts, attribSlot},
		{"DEP-COVERAGE", projectstate.KindOperationalConcepts, attribSlot},
		{"STD-WAIVE", projectstate.KindStandardCheck, attribSlot},
		{"STP-OP-EXISTS", 0, attribTesting},
		// The DH-* live tier is System-attributed as one family — including the
		// slot3↔slot5 volatility-join rules, so a Volatilities amendment is never
		// deadlocked by them while the System amendment that fixes them keeps full
		// severity. DH-COMP-VOL-DANGLING (typed encapsulatesVolatilities dangling
		// reference) must participate exactly like DH-VOL-ENCAP-MISSING / DH-VOL-TRACE.
		{"DH-VOL-ENCAP-MISSING", projectstate.KindSystem, attribSlot},
		{"DH-VOL-TRACE", projectstate.KindSystem, attribSlot},
		{"DH-COMP-VOL-DANGLING", projectstate.KindSystem, attribSlot},
		{"TOTALLY-UNKNOWN", 0, attribNone},
	}
	for _, c := range cases {
		kind, class := attributeRule(c.rule)
		if class != c.class || (class == attribSlot && kind != c.kind) {
			t.Errorf("attributeRule(%s) = (%v, %v), want (%v, %v)", c.rule, kind, class, c.kind, c.class)
		}
	}
}

// TestApplySlotScopeDowngrades_Unit pins the pure policy matrix for one ambient slot:
// other-slot error → annotated warning naming the owning slot; ambient-slot error stays
// error; unattributed error stays error; testing-state error → warning; sub-Error
// findings pass through.
func TestApplySlotScopeDowngrades_Unit(t *testing.T) {
	findings := []methodcheck.Finding{
		{RuleID: "UC-VARIATION-REF", Severity: methodcheck.SeverityError, Message: "dangling variationOf"},
		{RuleID: "SYS-NAME-UNIQUE", Severity: methodcheck.SeverityError, Message: "duplicate name"},
		{RuleID: "TOTALLY-UNKNOWN", Severity: methodcheck.SeverityError, Message: "structural"},
		{RuleID: "STP-OP-EXISTS", Severity: methodcheck.SeverityError, Message: "plan step names no op"},
		{RuleID: "DEP-RESOURCE-PRESENT", Severity: methodcheck.SeverityWarning, Message: "resource absent"},
	}
	got := applySlotScopeDowngrades(projectstate.KindSystem, findings)
	if got[0].Severity != methodcheck.SeverityWarning || !strings.Contains(got[0].Message, "coreUseCases") {
		t.Fatalf("other-slot error must downgrade naming the owning slot, got: %+v", got[0])
	}
	if got[1].Severity != methodcheck.SeverityError {
		t.Fatalf("ambient-slot error must keep Error severity, got: %+v", got[1])
	}
	if got[2].Severity != methodcheck.SeverityError {
		t.Fatalf("an unattributed (structural/unknown) error must never downgrade, got: %+v", got[2])
	}
	if got[3].Severity != methodcheck.SeverityWarning || !strings.Contains(got[3].Message, "testingState") {
		t.Fatalf("a testing-state error must downgrade for a design session, got: %+v", got[3])
	}
	if got[4] != findings[4] {
		t.Fatalf("sub-Error findings must pass through untouched, got: %+v", got[4])
	}
	if findings[0].Severity != methodcheck.SeverityError {
		t.Fatalf("applySlotScopeDowngrades must not mutate its input")
	}

	// The slot3↔slot5 volatility-join direction: a session amending the Volatilities
	// slot cannot write the System's typed encapsulatesVolatilities lists, so a
	// DH-COMP-VOL-DANGLING Error (System-attributed) must downgrade for it — the
	// rename/removal amendment is never deadlocked by the stale component-side edge.
	dangling := []methodcheck.Finding{{RuleID: "DH-COMP-VOL-DANGLING", Severity: methodcheck.SeverityError, Message: "stale typed entry"}}
	fromVolSession := applySlotScopeDowngrades(projectstate.KindVolatilities, dangling)
	if fromVolSession[0].Severity != methodcheck.SeverityWarning || !strings.Contains(fromVolSession[0].Message, "system") {
		t.Fatalf("DH-COMP-VOL-DANGLING must downgrade for a Volatilities-ambient session naming the system slot, got: %+v", fromVolSession[0])
	}
	fromSystemSession := applySlotScopeDowngrades(projectstate.KindSystem, dangling)
	if fromSystemSession[0].Severity != methodcheck.SeverityError {
		t.Fatalf("DH-COMP-VOL-DANGLING must keep Error severity for the System-ambient session that can fix it, got: %+v", fromSystemSession[0])
	}
}

// TestValidate_SlotScoping is the CLI matrix over one committed state carrying BOTH a
// sibling-slot defect (GLOSS-FOURQ in the committed Glossary) AND the DEP drift with
// FRESH opconcepts (no staleness help):
//
//   - --slot System        → both are other-slot → downgrade → PASS
//   - --slot Glossary      → the glossary defect is the AMBIENT slot → FAIL
//   - no --slot            → whole-document mode → FAIL (pre-slot-scoping behavior)
func TestValidate_SlotScoping(t *testing.T) {
	root := seedValidateRoot(t, withGlossaryDefect(deadlockFixtureProject(false)))

	var out bytes.Buffer
	if err := runValidate([]string{"--root", root, "--slot", "System"}, &out); err != nil {
		t.Fatalf("--slot System must PASS (all errors are other-slot), got: %v\n%s", err, out.String())
	}
	log := out.String()
	if !strings.Contains(log, "GLOSS-FOURQ") || !strings.Contains(log, "DEP-COVERAGE") {
		t.Fatalf("gate log must still SURFACE the downgraded findings, got:\n%s", log)
	}
	if !strings.Contains(log, "pre-existing on the glossary slot") {
		t.Fatalf("the slot-scope downgrade must name the owning slot, got:\n%s", log)
	}

	out.Reset()
	if err := runValidate([]string{"--root", root, "--slot", "Glossary"}, &out); err == nil {
		t.Fatalf("--slot Glossary must FAIL on its own slot's GLOSS-FOURQ error, log:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "ERROR") || !strings.Contains(out.String(), "GLOSS-FOURQ") {
		t.Fatalf("ambient-slot error must stay an ERROR in the log, got:\n%s", out.String())
	}

	out.Reset()
	if err := runValidate([]string{"--root", root}, &out); err == nil {
		t.Fatalf("whole-document mode (no --slot) must keep failing, log:\n%s", out.String())
	}
}

// TestValidate_SlotScopingCombinesWithStaleness — both policies together on the live
// gtdapp shape: stale opconcepts + DEP drift + a sibling-slot glossary defect, ambient
// System. The DEP errors take the staleness downgrade (first, more specific), the
// glossary error takes the slot-scope downgrade, and the gate passes.
func TestValidate_SlotScopingCombinesWithStaleness(t *testing.T) {
	root := seedValidateRoot(t, withGlossaryDefect(deadlockFixtureProject(true)))
	var out bytes.Buffer
	if err := runValidate([]string{"--root", root, "--slot", "System"}, &out); err != nil {
		t.Fatalf("combined policies must PASS, got: %v\n%s", err, out.String())
	}
	log := out.String()
	if !strings.Contains(log, "stale-basis") {
		t.Fatalf("DEP findings must carry the staleness rationale, got:\n%s", log)
	}
	if !strings.Contains(log, "pre-existing on the glossary slot") {
		t.Fatalf("the glossary finding must carry the slot-scope rationale, got:\n%s", log)
	}
	if strings.Contains(log, "ERROR") {
		t.Fatalf("no ERROR may survive the combined policies, got:\n%s", log)
	}
}

// TestValidate_BadSlotFlagRejected — a --slot value that is not a Method artifact kind
// is a usage error, never a silent whole-document run.
func TestValidate_BadSlotFlagRejected(t *testing.T) {
	root := seedValidateRoot(t, deadlockFixtureProject(true))
	if err := runValidate([]string{"--root", root, "--slot", "NotAKind"}, &bytes.Buffer{}); err == nil {
		t.Fatal("an unrecognized --slot value must be rejected")
	}
}
