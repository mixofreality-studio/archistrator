package systemdesign

// statevalidationfindings_test.go — coverage for the app-side read-back finding
// generators (architect ratification 2026-07-05) and the AdvancePhase pre-seal gates.
// Each rule gets a valid fixture (no finding) and a violating fixture (finding with the
// canonical rule id). A final test confirms a pre-existing VIOLATING committed state
// decodes WITHOUT erroring and only then surfaces findings (the critical read-safety
// invariant: reads never hard-fail; violations render as findings until an amendment).

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func compE(id, name string, kind projectstate.ComponentKind, layer projectstate.Layer, enc string) projectstate.Component {
	return projectstate.Component{ID: id, Name: name, Kind: kind, Layer: layer, Encapsulates: enc}
}

func rel(from, to string, mode projectstate.CallMode, label string) projectstate.Relationship {
	return projectstate.Relationship{From: from, To: to, Mode: mode, Label: label}
}

func hasRule(fs []Finding, id string, sev Severity) bool {
	for _, f := range fs {
		if string(f.RuleID) == id && f.Severity == sev {
			return true
		}
	}
	return false
}

// ---- SYS-RA-ORPHAN ----

func Test_raOrphan_HealthyReachesResource(t *testing.T) {
	sys := &projectstate.System{
		Components: []projectstate.Component{
			compE("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess, "the order store"),
			compE("store", "OrderStore", projectstate.CompResource, projectstate.LayerResource, ""),
		},
		Relationships: []projectstate.Relationship{rel("ra", "store", projectstate.CallSync, "reads")},
	}
	if f := raOrphanFindings(KindSystem, sys); len(f) != 0 {
		t.Fatalf("an RA that reaches a resource is not orphan, got: %+v", f)
	}
}

func Test_raOrphan_NoResourceEdgeFlagged(t *testing.T) {
	sys := &projectstate.System{
		Components: []projectstate.Component{
			compE("mgr", "OrderManager", projectstate.CompManager, projectstate.LayerManager, "the order workflow"),
			compE("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess, "the order store"),
		},
		Relationships: []projectstate.Relationship{rel("mgr", "ra", projectstate.CallSync, "loads")},
	}
	if !hasRule(raOrphanFindings(KindSystem, sys), "SYS-RA-ORPHAN", SeverityError) {
		t.Fatal("an RA with no outbound edge to a resource must be flagged SYS-RA-ORPHAN")
	}
}

func Test_raOrphan_ExternalTargetSatisfies(t *testing.T) {
	// An edge to an id that is not a modeled component is a documented external system.
	sys := &projectstate.System{
		Components: []projectstate.Component{
			compE("ra", "GitHubAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess, "GitHub"),
		},
		Relationships: []projectstate.Relationship{rel("ra", "github.com", projectstate.CallQueued, "calls")},
	}
	if f := raOrphanFindings(KindSystem, sys); len(f) != 0 {
		t.Fatalf("an RA reaching an external target is not orphan, got: %+v", f)
	}
}

// ---- SYS-ENCAPSULATES ----

func Test_encapsulates_EmptyClientError_EmptyResourceWarning(t *testing.T) {
	sys := &projectstate.System{Components: []projectstate.Component{
		compE("c", "WebClient", projectstate.CompClient, projectstate.LayerClient, ""),
		compE("r", "GitRepo", projectstate.CompResource, projectstate.LayerResource, ""),
		compE("m", "OrderManager", projectstate.CompManager, projectstate.LayerManager, "the order workflow"),
	}}
	f := encapsulatesFindings(KindSystem, sys)
	if !hasRule(f, "SYS-ENCAPSULATES", SeverityError) {
		t.Fatal("an empty-encapsulates client must be an ERROR finding")
	}
	if !hasRule(f, "SYS-ENCAPSULATES", SeverityWarning) {
		t.Fatal("an empty-encapsulates resource must be a WARNING finding")
	}
	// The non-empty manager raises nothing.
	for _, fi := range f {
		if strings.Contains(fi.Message, "OrderManager") {
			t.Fatalf("a non-empty manager must not be flagged, got: %+v", fi)
		}
	}
}

// ---- SYS-REL-DUP ----

func Test_relDup_ExactDuplicateError(t *testing.T) {
	sys := &projectstate.System{Relationships: []projectstate.Relationship{
		rel("a", "b", projectstate.CallSync, "x"),
		rel("a", "b", projectstate.CallSync, "x"),
	}}
	if !hasRule(relDupFindings(KindSystem, sys), "SYS-REL-DUP", SeverityError) {
		t.Fatal("an exact (from,to,mode) duplicate must be a SYS-REL-DUP error")
	}
}

func Test_relDup_LabelSplitWarning(t *testing.T) {
	sys := &projectstate.System{Relationships: []projectstate.Relationship{
		rel("a", "b", projectstate.CallSync, "reads"),
		rel("a", "b", projectstate.CallQueued, "writes"),
	}}
	f := relDupFindings(KindSystem, sys)
	if hasRule(f, "SYS-REL-DUP", SeverityError) {
		t.Fatal("distinct-mode edges are not an exact duplicate")
	}
	if !hasRule(f, "SYS-REL-DUP", SeverityWarning) {
		t.Fatal("a same-pair label split must be a SYS-REL-DUP warning")
	}
}

// ---- DV-CHAIN-CONNECTED ----

func Test_dvChain_ConnectedClean(t *testing.T) {
	sys := &projectstate.System{
		Components: []projectstate.Component{
			compE("web", "WebClient", projectstate.CompClient, projectstate.LayerClient, ""),
			compE("mgr", "OrderManager", projectstate.CompManager, projectstate.LayerManager, "wf"),
		},
		DynamicViews: []projectstate.DynamicView{{
			UseCaseID:    "uc1",
			Participants: []string{"web", "mgr"},
			Edges:        []projectstate.Relationship{rel("web", "mgr", projectstate.CallSync, "")},
		}},
	}
	if f := dvChainFindings(KindSystem, sys); len(f) != 0 {
		t.Fatalf("a connected client-rooted chain is clean, got: %+v", f)
	}
}

func Test_dvChain_DisconnectedWarning(t *testing.T) {
	sys := &projectstate.System{
		Components: []projectstate.Component{
			compE("web", "WebClient", projectstate.CompClient, projectstate.LayerClient, ""),
			compE("mgr", "OrderManager", projectstate.CompManager, projectstate.LayerManager, "wf"),
			compE("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess, "store"),
		},
		DynamicViews: []projectstate.DynamicView{{
			UseCaseID:    "uc1",
			Participants: []string{"web", "mgr", "ra"},
			Edges:        []projectstate.Relationship{rel("web", "mgr", projectstate.CallSync, "")},
		}},
	}
	if !hasRule(dvChainFindings(KindSystem, sys), "DV-CHAIN-CONNECTED", SeverityWarning) {
		t.Fatal("an unreachable participant must warn DV-CHAIN-CONNECTED")
	}
}

func Test_dvChain_NoClientRootWarning(t *testing.T) {
	sys := &projectstate.System{
		Components: []projectstate.Component{
			compE("mgr", "OrderManager", projectstate.CompManager, projectstate.LayerManager, "wf"),
			compE("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess, "store"),
		},
		DynamicViews: []projectstate.DynamicView{{
			UseCaseID:    "uc1",
			Participants: []string{"mgr", "ra"},
			Edges:        []projectstate.Relationship{rel("mgr", "ra", projectstate.CallSync, "")},
		}},
	}
	if !hasRule(dvChainFindings(KindSystem, sys), "DV-CHAIN-CONNECTED", SeverityWarning) {
		t.Fatal("a chain with no client root must warn DV-CHAIN-CONNECTED")
	}
}

// ---- UC-VARIATION-REF ----

func ucDecision(id, name string, class projectstate.Classification, variationOf, rejection string) projectstate.UseCaseDecision {
	uc := projectstate.UseCase{
		ID:             projectstate.UseCaseID(id),
		Name:           name,
		Trigger:        projectstate.TriggerClientAction,
		Classification: class,
	}
	if variationOf != "" {
		v := projectstate.UseCaseID(variationOf)
		uc.VariationOf = &v
	}
	return projectstate.UseCaseDecision{UseCase: uc, RejectionReason: rejection}
}

func Test_variationRef_ValidClean(t *testing.T) {
	cuc := &projectstate.CoreUseCases{Decisions: []projectstate.UseCaseDecision{
		ucDecision("base", "Base", projectstate.ClassCore, "", ""),
		ucDecision("var", "Variation", projectstate.ClassNonCore, "base", "narrower slice"),
	}}
	if f := variationRefFindings(KindCoreUseCases, cuc); len(f) != 0 {
		t.Fatalf("a well-formed variation set is clean, got: %+v", f)
	}
}

func Test_variationRef_Violations(t *testing.T) {
	cuc := &projectstate.CoreUseCases{Decisions: []projectstate.UseCaseDecision{
		ucDecision("base", "Base", projectstate.ClassCore, "nonsense", ""), // core with variationOf
		ucDecision("v1", "V1", projectstate.ClassNonCore, "ghost", "why"),  // unresolved variationOf
		ucDecision("v2", "V2", projectstate.ClassNonCore, "base", ""),      // empty rejectionReason
	}}
	f := variationRefFindings(KindCoreUseCases, cuc)
	if !hasRule(f, "UC-VARIATION-REF", SeverityError) {
		t.Fatal("expected UC-VARIATION-REF errors")
	}
	var coreVar, unresolved, noReason bool
	for _, fi := range f {
		switch {
		case strings.Contains(fi.Message, "core use case") && strings.Contains(fi.Message, "base, not a variation"):
			coreVar = true
		case strings.Contains(fi.Message, "does not resolve"):
			unresolved = true
		case strings.Contains(fi.Message, "empty rejectionReason"):
			noReason = true
		}
	}
	if !coreVar || !unresolved || !noReason {
		t.Fatalf("missing a violation class: coreVar=%v unresolved=%v noReason=%v (%+v)", coreVar, unresolved, noReason, f)
	}
}

// ---- GLOSS-FOURQ ----

func Test_glossaryFourQ_NonCanonicalError_And_CoverageWarning(t *testing.T) {
	g := &projectstate.Glossary{Items: []projectstate.GlossaryItem{
		{Term: "User", Category: "Who"},
		{Term: "Bogus", Category: "Nonsense"},
	}}
	f := glossaryFourQFindings(KindGlossary, g)
	if !hasRule(f, "GLOSS-FOURQ", SeverityError) {
		t.Fatal("a non-canonical category must be a GLOSS-FOURQ error")
	}
	// What/How/Where uncovered → warnings.
	if !hasRule(f, "GLOSS-FOURQ", SeverityWarning) {
		t.Fatal("uncovered Four-Questions categories must warn")
	}
}

func Test_glossaryFourQ_FullCoverageClean(t *testing.T) {
	g := &projectstate.Glossary{Items: []projectstate.GlossaryItem{
		{Term: "A", Category: "Who"}, {Term: "B", Category: "What"},
		{Term: "C", Category: "How"}, {Term: "D", Category: "Where"},
	}}
	if f := glossaryFourQFindings(KindGlossary, g); len(f) != 0 {
		t.Fatalf("full canonical coverage is clean, got: %+v", f)
	}
}

// ---- SR-ID-UNIQUE ----

func Test_scrubbedID_Violations(t *testing.T) {
	sr := &projectstate.ScrubbedRequirements{Items: []projectstate.Requirement{
		{ID: "R1", Statement: "ok"},
		{ID: "", Statement: "no id"},
		{ID: "R1", Statement: "dup id"},
		{ID: "R2", Statement: ""},
	}}
	f := scrubbedIDFindings(KindScrubbedRequirements, sr)
	var empty, dup, noStmt bool
	for _, fi := range f {
		if fi.RuleID != "SR-ID-UNIQUE" || fi.Severity != SeverityError {
			t.Fatalf("unexpected finding %+v", fi)
		}
		switch {
		case strings.Contains(fi.Message, "empty id"):
			empty = true
		case strings.Contains(fi.Message, "duplicated"):
			dup = true
		case strings.Contains(fi.Message, "empty statement"):
			noStmt = true
		}
	}
	if !empty || !dup || !noStmt {
		t.Fatalf("missing a violation class: empty=%v dup=%v noStmt=%v", empty, dup, noStmt)
	}
}

func Test_scrubbedID_Clean(t *testing.T) {
	sr := &projectstate.ScrubbedRequirements{Items: []projectstate.Requirement{
		{ID: "R1", Statement: "a"}, {ID: "R2", Statement: "b"},
	}}
	if f := scrubbedIDFindings(KindScrubbedRequirements, sr); len(f) != 0 {
		t.Fatalf("unique non-empty ids are clean, got: %+v", f)
	}
}

// ---- OPC-TOPIC-COVERAGE ----

func Test_opcTopic_MissingTopicInfo(t *testing.T) {
	op := &projectstate.OperationalConcepts{Decisions: []projectstate.OperationalDecision{
		{Topic: "communication topology"},
		{Topic: "layering style"},
		{Topic: "project state storage"},
	}}
	f := opcTopicFindings(KindOperationalConcepts, op)
	// Only sync/queued is unaddressed → exactly one info nudge, addressing "sync/queued".
	if len(f) != 1 || f[0].Severity != SeverityInfo || !strings.Contains(f[0].Message, `"sync/queued"`) {
		t.Fatalf("expected a single OPC-TOPIC-COVERAGE nudge for sync/queued, got: %+v", f)
	}
}

// ---- AdvancePhase pre-seal gates ----

func Test_standardCheckFailItems(t *testing.T) {
	proj := projectstate.Project{}
	proj.StandardCheck = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.StandardCheck{Items: []projectstate.CheckItem{
			{Section: "S1", Guideline: "closed architecture", Status: projectstate.CheckPass},
			{Section: "S2", Guideline: "no design in a vacuum", Status: projectstate.CheckFail},
		}},
	}
	fails := standardCheckFailItems(proj)
	if len(fails) != 1 || !strings.Contains(fails[0], "no design in a vacuum") {
		t.Fatalf("expected one fail item naming the failing guideline, got: %v", fails)
	}
	// A pass/waived-only check is fail-free.
	proj.StandardCheck.Model = &projectstate.StandardCheck{Items: []projectstate.CheckItem{
		{Status: projectstate.CheckPass}, {Status: projectstate.CheckWaived},
	}}
	if f := standardCheckFailItems(proj); len(f) != 0 {
		t.Fatalf("a fail-free standard check must yield no items, got: %v", f)
	}
}

func Test_staleCommittedPhase1Kinds_NamesCause(t *testing.T) {
	proj := projectstate.Project{}
	proj.Volatilities = projectstate.ArtifactSlot{
		Status:          projectstate.ReviewCommitted,
		StaleBasis:      true,
		StaleBasisCause: &projectstate.StaleCause{UpstreamKind: "mission", UpstreamRevision: 2},
		Model:           &projectstate.Volatilities{},
	}
	got := staleCommittedPhase1Kinds(proj)
	if len(got) != 1 || !strings.Contains(got[0], "mission rev 2") {
		t.Fatalf("stale kind must name its cause (mission rev 2), got: %v", got)
	}
}

// ---- read-safety: a pre-existing VIOLATING committed state decodes, then yields findings ----

func Test_ViolatingCommittedState_DecodesThenFindings(t *testing.T) {
	// A System with an ORPHAN ResourceAccess (no edge to a resource) and an
	// empty-encapsulates CLIENT — both finding-class violations, NOT codec failures.
	sys := &projectstate.System{
		Components: []projectstate.Component{
			compE("web", "WebClient", projectstate.CompClient, projectstate.LayerClient, ""), // empty client
			compE("mgr", "OrderManager", projectstate.CompManager, projectstate.LayerManager, "the order workflow"),
			compE("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess, "the order store"),
			compE("store", "OrderStore", projectstate.CompResource, projectstate.LayerResource, ""),
		},
		Relationships: []projectstate.Relationship{
			rel("web", "mgr", projectstate.CallSync, "places"),
			rel("mgr", "ra", projectstate.CallSync, "loads"),
			// NOTE: no ra → store edge, so ra is orphan.
		},
	}
	p := projectstate.Project{ID: "p"}
	p.SystemDesign = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: sys, Revisions: 1}

	raw, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// CRITICAL INVARIANT: reading a violating committed state must NOT hard-fail.
	got, ok, err := projectstate.DecodeProjectJSON(raw, "p")
	if err != nil || !ok {
		t.Fatalf("a violating committed state must still decode (read-safety): ok=%v err=%v", ok, err)
	}
	model := got.SystemDesign.Model
	if !hasRule(raOrphanFindings(KindSystem, model), "SYS-RA-ORPHAN", SeverityError) {
		t.Fatal("orphan RA must surface as a finding on the decoded committed state")
	}
	if !hasRule(encapsulatesFindings(KindSystem, model), "SYS-ENCAPSULATES", SeverityError) {
		t.Fatal("empty-encapsulates client must surface as a finding on the decoded committed state")
	}
}
