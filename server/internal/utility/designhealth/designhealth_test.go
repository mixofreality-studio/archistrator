package designhealth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// ---------------------------------------------------------------------------
// GREEN FIXTURE — the committed, reconciled project.json.
//
// The live tier's Error-severity rules are calibrated to pass on the committed
// archistrator design: a clean design must never trip the authoring gate. This
// test asserts (a) ZERO Error findings (methodcheck's Verdict semantics: only
// Error fails), and (b) that the advisory rules are actually RUNNING — a rule
// that silently returns nothing on the real state would be a false green.
// ---------------------------------------------------------------------------

func TestGreenFixtureNoErrors(t *testing.T) {
	raw := readCommittedProjectJSON(t)
	findings := EvaluateRaw(raw)
	if len(findings) == 0 {
		t.Fatal("EvaluateRaw returned no findings at all — the rule layer is not running against the committed state")
	}

	var errs []methodcheck.Finding
	for _, f := range findings {
		if f.Severity == methodcheck.SeverityError {
			errs = append(errs, f)
		}
	}
	if len(errs) > 0 {
		for _, f := range errs {
			t.Errorf("unexpected ERROR finding on the committed (green) state: %s — %s", f.RuleID, f.Message)
		}
	}
}

// TestGreenFixtureAdvisoriesFire pins the specific advisories the reconciled
// state is known to carry, so a future rule regression (a rule going silent, or a
// severity flip that would red the gate) is caught here rather than in production.
func TestGreenFixtureAdvisoriesFire(t *testing.T) {
	raw := readCommittedProjectJSON(t)
	findings := EvaluateRaw(raw)
	got := indexBySeverity(findings)

	slots, perr := parseSlots(raw)
	if perr != nil {
		t.Fatalf("parseSlots on the committed state: %v", perr)
	}

	// systemDesignManager carries 13 ops — past the App-C max of 12 (Warning).
	assertPresent(t, got, RuleContractOpMax, methodcheck.SeverityWarning)
	// Objective-coverage advisory: ERA-DEPENDENT. While any objective is referenced
	// by neither an objectiveLinks entry nor a legacy justifyingObjective, the
	// orphaned-business-need advisory fires at Warning; once every objective is
	// linked (the committed state is converging on the typed objectiveLinks home)
	// it must stay silent.
	if objectiveCoverageComplete(slots) {
		assertAbsent(t, got, RuleObjCoverage)
	} else {
		assertPresent(t, got, RuleObjCoverage, methodcheck.SeverityWarning)
	}
	// the volatility count is reported informationally.
	assertPresent(t, got, RuleCardVolatility, methodcheck.SeverityInfo)
	// Encapsulation-join advisory: ERA-DEPENDENT. Until the typed
	// encapsulatesVolatilities join is authored on the committed components, facet
	// groups make several volatility names match multiple blurbs (Info). Once ANY
	// component carries the typed list the join is authoritative and shared owners
	// are legitimate — no ambiguity finding may fire at all.
	if slots.typedEncapsulationActive() {
		assertAbsent(t, got, RuleVolEncapAmbig)
	} else {
		assertPresent(t, got, RuleVolEncapAmbig, methodcheck.SeverityInfo)
	}

	// The facet join, promoted from the family-D gate, must find NO fossil on the
	// reconciled state (all four facets resolve + layer-match).
	assertAbsent(t, got, RuleContractFacet)
	// The reconciled state pruned the ReadProject facet dead-op duplicate (it lived
	// on constructionTransitionAccess), so the clean state carries no dead-op
	// duplicate. (The rule itself stays proven by its negative fixture.)
	assertAbsent(t, got, RuleContractDeadOp)
	// No dangling volatility trace, no unarchitected core use case.
	assertAbsent(t, got, RuleVolTrace)
	assertAbsent(t, got, RuleCovUCDynamic)
	assertAbsent(t, got, RuleObjResolve)
}

// ---------------------------------------------------------------------------
// NEGATIVE FIXTURES — one per rule family, each a minimal project.json that
// makes exactly the target rule fire at its designed severity. The docs are
// intentionally small; a fixture may incidentally trip another rule, so each
// case asserts only that ITS target finding is present.
// ---------------------------------------------------------------------------

func TestNegativeFixturesEachRuleFires(t *testing.T) {
	cases := []struct {
		name     string
		doc      map[string]any
		wantRule methodcheck.RuleID
		wantSev  methodcheck.Severity
	}{
		{
			name: "graph up-call: engine calls manager",
			doc: sysDoc(
				comps(comp("e", "engine"), comp("m", "manager")),
				rels(rel("e", "m", "sync")),
				nil),
			wantRule: RuleGraphUpcall, wantSev: methodcheck.SeverityError,
		},
		{
			name: "graph sideways sync: manager calls manager sync",
			doc: sysDoc(
				comps(comp("m1", "manager"), comp("m2", "manager")),
				rels(rel("m1", "m2", "sync")),
				nil),
			wantRule: RuleGraphSidewaysSync, wantSev: methodcheck.SeverityError,
		},
		{
			name: "graph client entry: client calls engine",
			doc: sysDoc(
				comps(comp("c", "client"), comp("e", "engine")),
				rels(rel("c", "e", "sync")),
				nil),
			wantRule: RuleGraphClientEntry, wantSev: methodcheck.SeverityError,
		},
		{
			name: "graph queued target: queued call into an engine",
			doc: sysDoc(
				comps(comp("m", "manager"), comp("e", "engine")),
				rels(rel("m", "e", "queued")),
				nil),
			wantRule: RuleGraphQueuedTarget, wantSev: methodcheck.SeverityError,
		},
		{
			name: "graph engine IO: engine calls resourceAccess",
			doc: sysDoc(
				comps(comp("e", "engine"), comp("ra", "resourceAccess")),
				rels(rel("e", "ra", "sync")),
				nil),
			wantRule: RuleGraphEngineIO, wantSev: methodcheck.SeverityWarning,
		},
		{
			name: "graph utility unreachable",
			doc: sysDoc(
				comps(comp("m", "manager"), comp("u", "utility")),
				rels(),
				nil),
			wantRule: RuleGraphUtilReach, wantSev: methodcheck.SeverityWarning,
		},
		{
			// A manager calling only ResourceAccess is a LEGITIMATE Method design (IO
			// orchestration), so it must NOT fire; the rule fires only when a manager
			// orchestrates nothing at all (no engine and no RA edge — an empty component).
			name: "graph manager orchestrates nothing",
			doc: sysDoc(
				comps(comp("m", "manager"), comp("u", "utility")),
				rels(rel("m", "u", "sync")),
				nil),
			wantRule: RuleGraphMgrEmpty, wantSev: methodcheck.SeverityWarning,
		},
		{
			name:     "cardinality: six managers exceeds five",
			doc:      sysDoc(comps(comp("m1", "manager"), comp("m2", "manager"), comp("m3", "manager"), comp("m4", "manager"), comp("m5", "manager"), comp("m6", "manager")), rels(), nil),
			wantRule: RuleCardManagers, wantSev: methodcheck.SeverityWarning,
		},
		{
			name:     "cardinality: four engines exceeds the 2-3 band",
			doc:      sysDoc(comps(comp("m", "manager"), comp("e1", "engine"), comp("e2", "engine"), comp("e3", "engine"), comp("e4", "engine")), rels(), nil),
			wantRule: RuleCardEngines, wantSev: methodcheck.SeverityWarning,
		},
		{
			// 5 RA + 4 Resources = 9 combined (> 8) while each individual count stays
			// well under the >12 alarms — only the combined-band rule may fire.
			name: "cardinality: nine combined RA plus Resources exceeds eight",
			doc: sysDoc(comps(
				comp("ra1", "resourceAccess"), comp("ra2", "resourceAccess"), comp("ra3", "resourceAccess"), comp("ra4", "resourceAccess"), comp("ra5", "resourceAccess"),
				comp("r1", "resource"), comp("r2", "resource"), comp("r3", "resource"), comp("r4", "resource")), rels(), nil),
			wantRule: RuleCardRAResources, wantSev: methodcheck.SeverityWarning,
		},
		{
			name: "cardinality: seven utilities exceeds the half-dozen",
			doc: sysDoc(comps(
				comp("u1", "utility"), comp("u2", "utility"), comp("u3", "utility"), comp("u4", "utility"), comp("u5", "utility"), comp("u6", "utility"), comp("u7", "utility")), rels(), nil),
			wantRule: RuleCardUtilities, wantSev: methodcheck.SeverityInfo,
		},
		{
			name:     "cardinality: single-manager system is unvalidatable",
			doc:      sysDoc(comps(comp("m", "manager"), comp("e", "engine")), rels(), nil),
			wantRule: RuleCardManagersMin, wantSev: methodcheck.SeverityInfo,
		},
		{
			name: "use case: core decision missing essenceRationale",
			doc: withSlots(
				sysDoc(comps(comp("m", "manager")), rels(), dvs(dynView("uc-core", edge("c", "m", "sync")))),
				slot("4", 4, ucDecisions(ucDecision("uc-core", "core"))),
			),
			wantRule: RuleUCEssenceMissing, wantSev: methodcheck.SeverityWarning,
		},
		{
			name: "chain: two client-entry managers",
			doc: sysDoc(
				comps(comp("c", "client"), comp("m1", "manager"), comp("m2", "manager")),
				rels(rel("c", "m1", "sync")),
				dvs(dynView("uc-x", edge("c", "m1", "sync"), edge("c", "m2", "sync")))),
			wantRule: RuleChainEntryManager, wantSev: methodcheck.SeverityWarning,
		},
		{
			name: "chain: two queued manager hops",
			doc: sysDoc(
				comps(comp("c", "client"), comp("m1", "manager"), comp("m2", "manager"), comp("m3", "manager")),
				rels(rel("c", "m1", "sync"), rel("m1", "m2", "queued"), rel("m1", "m3", "queued")),
				dvs(dynView("uc-y", edge("c", "m1", "sync"), edge("m1", "m2", "queued"), edge("m1", "m3", "queued")))),
			wantRule: RuleChainQueuedManager, wantSev: methodcheck.SeverityWarning,
		},
		{
			name: "coverage: core use case with no dynamic view",
			doc: withSlots(
				sysDoc(comps(comp("m", "manager")), rels(), nil),
				slot("4", 4, map[string]any{"decisions": []any{
					map[string]any{"useCase": map[string]any{"id": "uc-orphan", "classification": "core"}},
				}}),
			),
			wantRule: RuleCovUCDynamic, wantSev: methodcheck.SeverityError,
		},
		{
			name: "coverage: volatility encapsulated by nothing",
			doc: withSlots(
				sysDoc(compsWithBlurb(compBlurb("m", "manager", "encapsulates the Widget Policy")), rels(), nil),
				slot("3", 3, map[string]any{"items": []any{
					map[string]any{"name": "Totally Unowned Volatility"},
				}}),
			),
			wantRule: RuleVolEncapMissing, wantSev: methodcheck.SeverityError,
		},
		{
			name: "coverage: typed join — volatility in no component's typed list",
			doc: withSlots(
				sysDoc(comps(compVols("m", "manager", "A")), rels(), nil),
				slot("3", 3, map[string]any{"items": []any{
					map[string]any{"name": "A"},
					map[string]any{"name": "B"},
				}}),
			),
			wantRule: RuleVolEncapMissing, wantSev: methodcheck.SeverityError,
		},
		{
			name: "join: typed entry dangles after a volatility rename",
			doc: withSlots(
				sysDoc(comps(compVols("m", "manager", "Ghost Vol")), rels(), nil),
				slot("3", 3, map[string]any{"items": []any{map[string]any{"name": "A"}}}),
			),
			wantRule: RuleCompVolDangling, wantSev: methodcheck.SeverityError,
		},
		{
			name: "coverage: manager encapsulating no volatility",
			doc: withSlots(
				sysDoc(comps(compVols("ra", "resourceAccess", "A"), comp("m", "manager")), rels(), nil),
				slot("3", 3, map[string]any{"items": []any{map[string]any{"name": "A"}}}),
			),
			wantRule: RuleCompNoVolatility, wantSev: methodcheck.SeverityWarning,
		},
		{
			name: "coverage: fallback ambiguity — name matches several blurbs",
			doc: withSlots(
				sysDoc(compsWithBlurb(
					compBlurb("m1", "manager", "owns the Widget Policy"),
					compBlurb("m2", "engine", "also computes the Widget Policy")), rels(), nil),
				slot("3", 3, map[string]any{"items": []any{map[string]any{"name": "Widget Policy"}}}),
			),
			wantRule: RuleVolEncapAmbig, wantSev: methodcheck.SeverityInfo,
		},
		{
			name: "coverage: volatility trace to missing requirement",
			doc: withSlots(
				sysDoc(comps(comp("m", "manager")), rels(), nil),
				slot("2", 2, map[string]any{"items": []any{map[string]any{"id": "R-001"}}}),
				slot("3", 3, map[string]any{"items": []any{
					map[string]any{"name": "V", "traces": []any{"R-999"}},
				}}),
			),
			wantRule: RuleVolTrace, wantSev: methodcheck.SeverityError,
		},
		{
			name: "coverage: justifyingObjective resolves to nothing",
			doc: withSlots(
				sysDoc(comps(comp("m", "manager")), rels(), nil),
				slot("0", 0, map[string]any{"objectives": []any{map[string]any{"number": 1}}}),
				slot("6", 6, map[string]any{"decisions": []any{
					map[string]any{"justifyingObjective": 42},
				}}),
			),
			wantRule: RuleObjResolve, wantSev: methodcheck.SeverityError,
		},
		{
			name: "coverage: objectiveLinks number resolves to nothing",
			doc: withSlots(
				sysDoc(comps(comp("m", "manager")), rels(), nil),
				slot("0", 0, map[string]any{"objectives": []any{map[string]any{"number": 1}}}),
				slot("6", 6, map[string]any{"objectiveLinks": map[string]any{
					"deploymentScenario": []any{42},
				}}),
			),
			wantRule: RuleObjResolve, wantSev: methodcheck.SeverityError,
		},
		{
			name: "coverage: unreferenced objective is an orphaned business need",
			doc: withSlots(
				sysDoc(comps(comp("m", "manager")), rels(), nil),
				slot("0", 0, map[string]any{"objectives": []any{
					map[string]any{"number": 1},
					map[string]any{"number": 2},
				}}),
				slot("6", 6, map[string]any{"objectiveLinks": map[string]any{
					"deploymentScenario": []any{1},
				}}),
			),
			wantRule: RuleObjCoverage, wantSev: methodcheck.SeverityWarning,
		},
		{
			name: "contract: op count hard reject at twenty",
			doc: withContracts(
				sysDoc(comps(comp("m", "manager")), rels(), nil),
				map[string]any{"godManager": contractEntry("godManager", "Manager", "internal/manager/god", opNames(20))},
			),
			wantRule: RuleContractOpReject, wantSev: methodcheck.SeverityError,
		},
		{
			name: "contract: fossil facet resolves to nothing",
			doc: withContracts(
				sysDoc(comps(comp("real-manager", "manager")), rels(), nil),
				map[string]any{"ghostAccess": contractEntry("nonexistentComponent", "ResourceAccess", "internal/resourceaccess/ghost", opNames(2))},
			),
			wantRule: RuleContractFacet, wantSev: methodcheck.SeverityError,
		},
		{
			// Same op name, SAME param signature across a facet group → exact
			// duplicate → Error.
			name: "contract: dead op EXACT duplicate (same name + signature)",
			doc: withContracts(
				sysDoc(comps(comp("project-state-access", "resourceAccess")), rels(), nil),
				map[string]any{
					"projectStateAccess":           contractEntryOps("projectStateAccess", "ResourceAccess", "internal/resourceaccess/projectstate", opWithParams("ReadProject", "projectID"), opWithParams("WriteProject", "projectID")),
					"constructionTransitionAccess": contractEntryOps("projectStateAccess", "ResourceAccess", "internal/resourceaccess/projectstate", opWithParams("ReadProject", "projectID"), opWithParams("RecordActivity", "activityID")),
				},
			),
			wantRule: RuleContractDeadOp, wantSev: methodcheck.SeverityError,
		},
		{
			// Same op name, DIFFERING param signature (the reconciled ReadProject
			// shape: {projectID} vs {projectID, cred}) → name collision → Warning.
			name: "contract: dead op NAME collision (differing signature)",
			doc: withContracts(
				sysDoc(comps(comp("project-state-access", "resourceAccess")), rels(), nil),
				map[string]any{
					"projectStateAccess":           contractEntryOps("projectStateAccess", "ResourceAccess", "internal/resourceaccess/projectstate", opWithParams("ReadProject", "projectID")),
					"constructionTransitionAccess": contractEntryOps("projectStateAccess", "ResourceAccess", "internal/resourceaccess/projectstate", opWithParams("ReadProject", "projectID", "cred")),
				},
			),
			wantRule: RuleContractDeadOp, wantSev: methodcheck.SeverityWarning,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.doc)
			if err != nil {
				t.Fatalf("marshal fixture: %v", err)
			}
			findings := EvaluateRaw(raw)
			for _, f := range findings {
				if f.RuleID == tc.wantRule {
					if f.Severity != tc.wantSev {
						t.Fatalf("%s fired at severity %v, want %v (msg: %s)", tc.wantRule, f.Severity, tc.wantSev, f.Message)
					}
					return // found at the right severity
				}
			}
			t.Fatalf("expected rule %s to fire, but it did not. findings: %s", tc.wantRule, renderFindings(findings))
		})
	}
}

// TestManagerWithOnlyResourceAccessIsValid pins the merits fix for
// DH-GRAPH-MANAGER-EMPTY: a Manager that orchestrates ResourceAccess but no Engine
// is a legitimate Method design (IO orchestration), and must produce NO finding. The
// Method does not require an Engine per Manager; only a Manager orchestrating NOTHING
// is a defect. (This is the exact shape of the driftFixtureProject GtdManager that
// previously over-fired.)
func TestManagerWithOnlyResourceAccessIsValid(t *testing.T) {
	doc := sysDoc(
		comps(comp("c", "client"), comp("m", "manager"), comp("ra1", "resourceAccess"), comp("ra2", "resourceAccess"), comp("r", "resource")),
		rels(rel("c", "m", "sync"), rel("m", "ra1", "sync"), rel("m", "ra2", "sync"), rel("ra1", "r", "sync"), rel("ra2", "r", "sync")),
		nil)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, f := range EvaluateRaw(raw) {
		if f.RuleID == RuleGraphMgrEmpty {
			t.Errorf("DH-GRAPH-MANAGER-EMPTY must NOT fire on a manager that orchestrates ResourceAccess: %s", f.Message)
		}
	}
}

// TestDriftFixtureShapeDoesNotFlagCoveredComponents reproduces the shape of
// cmd/aiarch-state-mcp's driftFixtureProject (the amendedSystemModel: a WebClient
// "c1" → GtdManager "m1" → TaskAccess/AgentAccess "ra1"/"ra2" → TaskDB "r1", plus a
// Mission objective) and asserts the live tier produces NO finding that names the
// covered components c1/m1. This is the coexistence guarantee the validate-gate test
// TestValidate_ActivityCoverageDriftFails relies on now that the seam appends DH-*
// findings: m1 orchestrates two ResourceAccess components (a valid Method design), so
// DH-GRAPH-MANAGER-EMPTY must stay silent.
func TestDriftFixtureShapeDoesNotFlagCoveredComponents(t *testing.T) {
	doc := withSlots(
		sysDoc(
			comps(comp("c1", "client"), comp("m1", "manager"), comp("ra1", "resourceAccess"), comp("ra2", "resourceAccess"), comp("r1", "resource")),
			rels(rel("c1", "m1", "sync"), rel("m1", "ra1", "sync"), rel("m1", "ra2", "sync"), rel("ra1", "r1", "sync"), rel("ra2", "r1", "sync")),
			nil),
		slot("0", 0, map[string]any{"objectives": []any{map[string]any{"number": 1}}}),
	)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, f := range EvaluateRaw(raw) {
		if strings.Contains(f.Message, `"m1"`) || strings.Contains(f.Message, `"c1"`) ||
			(f.Location != nil && (strings.Contains(f.Location.Section, "m1") || strings.Contains(f.Location.Section, "c1"))) {
			t.Errorf("no live-tier finding may name a covered component c1/m1 on this valid shape, got %s: %s", f.RuleID, f.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// TYPED volatility↔component join (Component.encapsulatesVolatilities)
// ---------------------------------------------------------------------------

// TestTypedEncapsulationJoinAuthoritative pins the typed-join regime: the moment ANY
// component carries a non-empty typed encapsulatesVolatilities list, the typed join
// is authoritative for the WHOLE system — blurb mentions stop counting as ownership,
// shared typed owners are legitimate (no ambiguity finding), and a volatility owned
// by zero typed lists is a real Error gap.
func TestTypedEncapsulationJoinAuthoritative(t *testing.T) {
	doc := withSlots(
		sysDoc(comps(
			compVols("m", "manager", "Shared Vol"),
			compVols("e", "engine", "Shared Vol"),
			compBlurb("ra", "resourceAccess", "prose that mentions Blurb Vol at length"),
		), rels(), nil),
		slot("3", 3, map[string]any{"items": []any{
			map[string]any{"name": "Shared Vol"},
			map[string]any{"name": "Blurb Vol"},
		}}),
	)
	findings := EvaluateRaw(mustMarshal(t, doc))
	got := indexBySeverity(findings)

	// Two typed owners of "Shared Vol" are a legitimate shared component/facet
	// group under the ratified doctrine — never an ambiguity finding.
	assertAbsent(t, got, RuleVolEncapAmbig)
	// "Blurb Vol" is mentioned only in a blurb; under the authoritative typed join
	// that is no longer ownership, so the encapsulation gap is a real Error.
	assertPresent(t, got, RuleVolEncapMissing, methodcheck.SeverityError)
	var missing []string
	for _, f := range findings {
		if f.RuleID == RuleVolEncapMissing {
			missing = append(missing, f.Message)
		}
	}
	if len(missing) != 1 || !strings.Contains(missing[0], `"Blurb Vol"`) {
		t.Errorf("exactly the blurb-only volatility must be missing under the typed join, got: %v", missing)
	}
	// Both typed entries name committed volatilities — nothing dangles.
	assertAbsent(t, got, RuleCompVolDangling)
}

// TestFallbackJoinPreserved pins that a state with NO typed field anywhere keeps the
// interim name-in-blurb regime exactly: substring ownership, multi-match → Info
// ambiguity, no-match → Error gap, and no typed-join rules firing.
func TestFallbackJoinPreserved(t *testing.T) {
	doc := withSlots(
		sysDoc(compsWithBlurb(
			compBlurb("m1", "manager", "owns the Widget Policy end to end"),
			compBlurb("m2", "engine", "computes over the widget policy"),
		), rels(), nil),
		slot("3", 3, map[string]any{"items": []any{
			map[string]any{"name": "Widget Policy"},
			map[string]any{"name": "Unowned Vol"},
		}}),
	)
	findings := EvaluateRaw(mustMarshal(t, doc))
	got := indexBySeverity(findings)
	assertPresent(t, got, RuleVolEncapAmbig, methodcheck.SeverityInfo)
	assertPresent(t, got, RuleVolEncapMissing, methodcheck.SeverityError)
	assertAbsent(t, got, RuleCompVolDangling)
	// Both components' blurbs mention a committed name → ownership under fallback.
	assertAbsent(t, got, RuleCompNoVolatility)
}

// TestCompVolDangling pins the dangling-reference rule: exact-name semantics (a case
// difference is rename residue, not a match) and the empty-volatilities skip.
func TestCompVolDangling(t *testing.T) {
	base := func(volItems []any) map[string]any {
		return withSlots(
			sysDoc(comps(compVols("m", "manager", "widget policy")), rels(), nil),
			slot("3", 3, map[string]any{"items": volItems}),
		)
	}

	// Case-different entry: the join is exact-name → dangling reference.
	got := indexBySeverity(EvaluateRaw(mustMarshal(t, base([]any{map[string]any{"name": "Widget Policy"}}))))
	assertPresent(t, got, RuleCompVolDangling, methodcheck.SeverityError)

	// Empty volatilities slot: nothing to join against → every volatility-join rule
	// stays silent (the same posture as the other joins).
	got = indexBySeverity(EvaluateRaw(mustMarshal(t, base([]any{}))))
	assertAbsent(t, got, RuleCompVolDangling)
	assertAbsent(t, got, RuleCompNoVolatility)
	assertAbsent(t, got, RuleVolEncapMissing)
}

// TestCompNoVolatility pins the reverse anti-functional-decomposition check across
// both ownership regimes and the layer exemptions: Manager/Engine/ResourceAccess must
// encapsulate ≥1 volatility; Client/Resource/Utility are exempt.
func TestCompNoVolatility(t *testing.T) {
	// TYPED regime: ownership is a non-empty typed list.
	typedDoc := withSlots(
		sysDoc(comps(
			compVols("m", "manager", "Widget Policy"),
			comp("e", "engine"),
			comp("ra", "resourceAccess"),
			comp("c", "client"),
			comp("r", "resource"),
			comp("u", "utility"),
		), rels(), nil),
		slot("3", 3, map[string]any{"items": []any{map[string]any{"name": "Widget Policy"}}}),
	)
	assertNoVolWarnings(t, EvaluateRaw(mustMarshal(t, typedDoc)), []string{"component e", "component ra"})

	// FALLBACK regime: ownership is a blurb mentioning ≥1 committed name (containsFold).
	fallbackDoc := withSlots(
		sysDoc(compsWithBlurb(
			compBlurb("m", "manager", "owns the widget policy lifecycle"),
			compBlurb("e", "engine", "pure computation, cites nothing"),
			comp("ra", "resourceAccess"),
			comp("c", "client"),
		), rels(), nil),
		slot("3", 3, map[string]any{"items": []any{map[string]any{"name": "Widget Policy"}}}),
	)
	assertNoVolWarnings(t, EvaluateRaw(mustMarshal(t, fallbackDoc)), []string{"component e", "component ra"})
}

// assertNoVolWarnings asserts DH-COMP-NO-VOLATILITY fired at Warning for exactly the
// given Location.Sections.
func assertNoVolWarnings(t *testing.T, findings []methodcheck.Finding, wantSections []string) {
	t.Helper()
	var gotSections []string
	for _, f := range findings {
		if f.RuleID != RuleCompNoVolatility {
			continue
		}
		if f.Severity != methodcheck.SeverityWarning {
			t.Errorf("DH-COMP-NO-VOLATILITY must be Warning, got %v (msg: %s)", f.Severity, f.Message)
		}
		if f.Location != nil {
			gotSections = append(gotSections, f.Location.Section)
		}
	}
	sort.Strings(gotSections)
	want := append([]string(nil), wantSections...)
	sort.Strings(want)
	if strings.Join(gotSections, "|") != strings.Join(want, "|") {
		t.Errorf("DH-COMP-NO-VOLATILITY sections = %v, want %v", gotSections, want)
	}
}

// TestCardinalityBandBoundaries pins the ch. 4 smallest-set band edges: counts AT
// the band ceiling (3 Engines, 8 combined RA+Resources, 6 Utilities, ≥2 Managers)
// stay silent — the bands are strict over-bounds, not at-bounds.
func TestCardinalityBandBoundaries(t *testing.T) {
	doc := sysDoc(comps(
		comp("m1", "manager"), comp("m2", "manager"),
		comp("e1", "engine"), comp("e2", "engine"), comp("e3", "engine"),
		comp("ra1", "resourceAccess"), comp("ra2", "resourceAccess"), comp("ra3", "resourceAccess"), comp("ra4", "resourceAccess"),
		comp("r1", "resource"), comp("r2", "resource"), comp("r3", "resource"), comp("r4", "resource"),
		comp("u1", "utility"), comp("u2", "utility"), comp("u3", "utility"), comp("u4", "utility"), comp("u5", "utility"), comp("u6", "utility"),
	), rels(), nil)
	got := indexBySeverity(EvaluateRaw(mustMarshal(t, doc)))
	assertAbsent(t, got, RuleCardEngines)
	assertAbsent(t, got, RuleCardRAResources)
	assertAbsent(t, got, RuleCardUtilities)
	assertAbsent(t, got, RuleCardManagersMin)
}

// TestCardManagersMinSkips pins the DH-CARD-MANAGERS-MIN skip semantics: a system
// with ZERO components (mid-draft) is not a single-Manager system — the rule stays
// entirely silent rather than nagging an architecture that does not exist yet.
func TestCardManagersMinSkips(t *testing.T) {
	got := indexBySeverity(EvaluateRaw(mustMarshal(t, sysDoc(comps(), rels(), nil))))
	assertAbsent(t, got, RuleCardManagersMin)
}

// TestUCEssenceRationale pins DH-UC-ESSENCE-MISSING across the decision shapes:
// a core decision must carry a non-empty essenceRationale (nil, empty, and
// whitespace-only all fire at Warning); nonCore decisions are the rejectionReason
// side and never fire; an empty/uncommitted coreUseCases slot skips entirely.
func TestUCEssenceRationale(t *testing.T) {
	build := func(decisions ...map[string]any) []byte {
		return mustMarshal(t, withSlots(
			sysDoc(comps(comp("m", "manager")), rels(), nil),
			slot("4", 4, ucDecisions(decisions...)),
		))
	}

	// Non-empty rationale → silent.
	got := indexBySeverity(EvaluateRaw(build(ucDecisionEssence("uc-1", "core", "the differentiating revenue stream"))))
	assertAbsent(t, got, RuleUCEssenceMissing)

	// Field absent → Warning.
	got = indexBySeverity(EvaluateRaw(build(ucDecision("uc-1", "core"))))
	assertPresent(t, got, RuleUCEssenceMissing, methodcheck.SeverityWarning)

	// Explicit null → Warning.
	null := ucDecision("uc-1", "core")
	null["essenceRationale"] = nil
	got = indexBySeverity(EvaluateRaw(build(null)))
	assertPresent(t, got, RuleUCEssenceMissing, methodcheck.SeverityWarning)

	// Whitespace-only → Warning.
	got = indexBySeverity(EvaluateRaw(build(ucDecisionEssence("uc-1", "core", "   "))))
	assertPresent(t, got, RuleUCEssenceMissing, methodcheck.SeverityWarning)

	// nonCore without a rationale → silent (that side carries rejectionReason).
	got = indexBySeverity(EvaluateRaw(build(ucDecision("uc-2", "nonCore"))))
	assertAbsent(t, got, RuleUCEssenceMissing)

	// Empty decisions list (slot present but empty) → skip.
	got = indexBySeverity(EvaluateRaw(build()))
	assertAbsent(t, got, RuleUCEssenceMissing)

	// Slot absent entirely → skip.
	got = indexBySeverity(EvaluateRaw(mustMarshal(t, sysDoc(comps(comp("m", "manager")), rels(), nil))))
	assertAbsent(t, got, RuleUCEssenceMissing)
}

// TestObjectiveLinksJoin pins the objective↔architecture traceability join (ch. 5)
// across BOTH reference sources: the post-reshape typed home
// DeploymentOperationsModel.objectiveLinks (knob name → objective numbers) and the
// legacy decisions[].justifyingObjective path older states still carry. Resolution
// (DH-OBJ-RESOLVE, Error) and coverage (DH-OBJ-COVERAGE, Warning) must treat the
// two sources as one referenced set, and empty/absent objectiveLinks must be
// tolerated, never a parse failure.
func TestObjectiveLinksJoin(t *testing.T) {
	build := func(opModel map[string]any) []byte {
		return mustMarshal(t, withSlots(
			sysDoc(comps(comp("m", "manager")), rels(), nil),
			slot("0", 0, map[string]any{"objectives": []any{
				map[string]any{"number": 1},
				map[string]any{"number": 2},
			}}),
			slot("6", 6, opModel),
		))
	}

	// Every objective referenced via the typed objectiveLinks map → both joins silent.
	got := indexBySeverity(EvaluateRaw(build(map[string]any{
		"objectiveLinks": map[string]any{"deploymentScenario": []any{1, 2}},
	})))
	assertAbsent(t, got, RuleObjResolve)
	assertAbsent(t, got, RuleObjCoverage)

	// A dangling number arriving via objectiveLinks → DH-OBJ-RESOLVE Error, exactly
	// as via the legacy path, and the finding names its source knob.
	findings := EvaluateRaw(build(map[string]any{
		"objectiveLinks": map[string]any{"constructionVenue": []any{1, 2, 42}},
	}))
	got = indexBySeverity(findings)
	assertPresent(t, got, RuleObjResolve, methodcheck.SeverityError)
	knobNamed := false
	for _, f := range findings {
		if f.RuleID == RuleObjResolve && strings.Contains(f.Message, "constructionVenue") && strings.Contains(f.Message, "42") {
			knobNamed = true
		}
	}
	if !knobNamed {
		t.Errorf("DH-OBJ-RESOLVE for an objectiveLinks dangle must name the knob and the number, findings: %s", renderFindings(findings))
	}

	// Legacy decisions[].justifyingObjective still harvests (older states).
	got = indexBySeverity(EvaluateRaw(build(map[string]any{"decisions": []any{
		map[string]any{"justifyingObjective": 1},
		map[string]any{"justifyingObjective": 2},
	}})))
	assertAbsent(t, got, RuleObjResolve)
	assertAbsent(t, got, RuleObjCoverage)

	// The two sources MERGE into one referenced set: decisions cover objective 1,
	// objectiveLinks covers objective 2 → full coverage.
	got = indexBySeverity(EvaluateRaw(build(map[string]any{
		"decisions":      []any{map[string]any{"justifyingObjective": 1}},
		"objectiveLinks": map[string]any{"scalingPolicy": []any{2}},
	})))
	assertAbsent(t, got, RuleObjResolve)
	assertAbsent(t, got, RuleObjCoverage)

	// Empty objectiveLinks map → tolerated; nothing referenced, so the coverage
	// advisory fires at Warning (an orphaned business need, non-blocking).
	got = indexBySeverity(EvaluateRaw(build(map[string]any{"objectiveLinks": map[string]any{}})))
	assertAbsent(t, got, RuleObjResolve)
	assertPresent(t, got, RuleObjCoverage, methodcheck.SeverityWarning)

	// objectiveLinks absent entirely → same tolerance.
	got = indexBySeverity(EvaluateRaw(build(map[string]any{})))
	assertAbsent(t, got, RuleObjResolve)
	assertPresent(t, got, RuleObjCoverage, methodcheck.SeverityWarning)
}

// TestEvaluateRawTolerates checks the structural-degradation contract: a document
// the published parser rejects yields no findings (the framework gate owns it),
// never a panic.
func TestEvaluateRawTolerates(t *testing.T) {
	if f := EvaluateRaw([]byte("not json")); f != nil {
		t.Errorf("non-JSON input should yield nil findings, got %d", len(f))
	}
	if f := EvaluateRaw([]byte(`{}`)); len(f) != 0 {
		t.Errorf("empty document should yield no findings, got %d", len(f))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func readCommittedProjectJSON(t *testing.T) []byte {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		candidate := filepath.Join(dir, ".aiarch", "state", "project.json")
		if _, err := os.Stat(candidate); err == nil {
			raw, rerr := os.ReadFile(candidate)
			if rerr != nil {
				t.Fatalf("read %s: %v", candidate, rerr)
			}
			return raw
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate .aiarch/state/project.json walking up from the test cwd")
		}
		dir = parent
	}
}

// objectiveCoverageComplete reports whether every declared objective number is
// referenced by at least one objectiveLinks entry or legacy justifyingObjective —
// the era predicate for whether DH-OBJ-COVERAGE may fire on the live,
// concurrently-edited committed state.
func objectiveCoverageComplete(s slotData) bool {
	referenced := map[int]bool{}
	for _, n := range s.justifyingObjectives {
		referenced[n] = true
	}
	for _, l := range s.objectiveLinkRefs {
		referenced[l.Number] = true
	}
	for n := range s.objectiveNumbers() {
		if !referenced[n] {
			return false
		}
	}
	return true
}

func indexBySeverity(fs []methodcheck.Finding) map[methodcheck.RuleID]methodcheck.Severity {
	// keep the HIGHEST severity seen per rule id (Error > Warning > Info).
	out := map[methodcheck.RuleID]methodcheck.Severity{}
	for _, f := range fs {
		if cur, ok := out[f.RuleID]; !ok || f.Severity > cur {
			out[f.RuleID] = f.Severity
		}
	}
	return out
}

func assertPresent(t *testing.T, got map[methodcheck.RuleID]methodcheck.Severity, id methodcheck.RuleID, sev methodcheck.Severity) {
	t.Helper()
	s, ok := got[id]
	if !ok {
		t.Errorf("expected rule %s to fire on the committed state, but it did not", id)
		return
	}
	if s != sev {
		t.Errorf("rule %s fired at severity %v on the committed state, want %v", id, s, sev)
	}
}

func assertAbsent(t *testing.T, got map[methodcheck.RuleID]methodcheck.Severity, id methodcheck.RuleID) {
	t.Helper()
	if _, ok := got[id]; ok {
		t.Errorf("rule %s should NOT fire on the committed (green) state, but it did", id)
	}
}

func renderFindings(fs []methodcheck.Finding) string {
	if len(fs) == 0 {
		return "(none)"
	}
	out := ""
	for _, f := range fs {
		out += "\n  [" + severityWord(f.Severity) + "] " + string(f.RuleID) + ": " + f.Message
	}
	return out
}

func severityWord(s methodcheck.Severity) string {
	switch s {
	case methodcheck.SeverityError:
		return "ERROR"
	case methodcheck.SeverityWarning:
		return "WARN"
	default:
		return "INFO"
	}
}

// --- fixture builders (project.json fragments as Go maps) ---

func comp(id, kind string) map[string]any {
	return map[string]any{"id": id, "kind": kind, "name": id, "layer": kind}
}

func compBlurb(id, kind, blurb string) map[string]any {
	m := comp(id, kind)
	m["encapsulates"] = blurb
	return m
}

// compVols builds a component carrying the typed encapsulatesVolatilities join.
func compVols(id, kind string, vols ...string) map[string]any {
	m := comp(id, kind)
	vs := make([]any, len(vols))
	for i, v := range vols {
		vs[i] = v
	}
	m["encapsulatesVolatilities"] = vs
	return m
}

// ucDecisions builds a core-use-cases slot model from decision maps.
func ucDecisions(ds ...map[string]any) map[string]any {
	arr := make([]any, len(ds))
	for i, d := range ds {
		arr[i] = d
	}
	return map[string]any{"decisions": arr}
}

// ucDecision builds one decision with no essenceRationale field.
func ucDecision(id, classification string) map[string]any {
	return map[string]any{"useCase": map[string]any{"id": id, "classification": classification}}
}

// ucDecisionEssence builds one decision carrying an essenceRationale.
func ucDecisionEssence(id, classification, essence string) map[string]any {
	d := ucDecision(id, classification)
	d["essenceRationale"] = essence
	return d
}

func mustMarshal(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return raw
}

func comps(cs ...map[string]any) []any {
	out := make([]any, len(cs))
	for i, c := range cs {
		out[i] = c
	}
	return out
}

func compsWithBlurb(cs ...map[string]any) []any { return comps(cs...) }

func rel(from, to, mode string) map[string]any {
	return map[string]any{"from": from, "to": to, "mode": mode, "label": from + "->" + to}
}

func rels(rs ...map[string]any) []any {
	out := make([]any, len(rs))
	for i, r := range rs {
		out[i] = r
	}
	return out
}

func edge(from, to, mode string) map[string]any {
	return map[string]any{"from": from, "to": to, "mode": mode, "label": from + "->" + to}
}

func dynView(useCaseID string, es ...map[string]any) map[string]any {
	calls := make([]any, len(es))
	for i, e := range es {
		calls[i] = e
	}
	steps := []any{map[string]any{"activityNodeId": "step1", "calls": calls}}
	return map[string]any{"useCaseId": useCaseID, "key": useCaseID, "steps": steps}
}

func dvs(vs ...map[string]any) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v
	}
	return out
}

// sysDoc builds a minimal project.json with a systemDesign (kind 5) slot.
func sysDoc(components, relationships, dynamicViews []any) map[string]any {
	model := map[string]any{
		"components":    components,
		"relationships": relationships,
	}
	if dynamicViews != nil {
		model["dynamicViews"] = dynamicViews
	}
	return map[string]any{
		"slots": map[string]any{
			"5": map[string]any{"kind": 5, "status": 2, "revisions": 1, "model": model},
		},
	}
}

// slot is a (key, kind, model) triple for withSlots.
type slotSpec struct {
	key   string
	kind  int
	model map[string]any
}

func slot(key string, kind int, model map[string]any) slotSpec {
	return slotSpec{key: key, kind: kind, model: model}
}

// withSlots adds extra slots to a base doc built by sysDoc.
func withSlots(base map[string]any, specs ...slotSpec) map[string]any {
	slots := base["slots"].(map[string]any)
	for _, s := range specs {
		slots[s.key] = map[string]any{"kind": s.kind, "status": 2, "model": s.model}
	}
	return base
}

// withContracts adds a serviceContracts map to a base doc.
func withContracts(base map[string]any, contracts map[string]any) map[string]any {
	base["serviceContracts"] = contracts
	return base
}

// contractEntry builds a serviceContracts entry with a minimal interface.
func contractEntry(component, layer, goPackage string, ops []string) map[string]any {
	operations := make([]any, len(ops))
	for i, name := range ops {
		operations[i] = map[string]any{"name": name, "params": []any{}}
	}
	return map[string]any{
		"component": component,
		"layer":     layer,
		"goPackage": goPackage,
		"title":     component + " contract",
		"interface": map[string]any{"name": component, "layer": layer, "operations": operations},
	}
}

// opWithParams builds one interface operation with the given ordered param names.
func opWithParams(name string, params ...string) map[string]any {
	ps := make([]any, len(params))
	for i, p := range params {
		ps[i] = map[string]any{"name": p}
	}
	return map[string]any{"name": name, "params": ps}
}

// contractEntryOps builds a serviceContracts entry from explicit operation maps
// (each from opWithParams) — the param-carrying counterpart to contractEntry, for
// the signature-sensitive dead-op tests.
func contractEntryOps(component, layer, goPackage string, ops ...map[string]any) map[string]any {
	operations := make([]any, len(ops))
	for i, o := range ops {
		operations[i] = o
	}
	return map[string]any{
		"component": component,
		"layer":     layer,
		"goPackage": goPackage,
		"title":     component + " contract",
		"interface": map[string]any{"name": component, "layer": layer, "operations": operations},
	}
}

// opNames returns n distinct operation names Op0..Op(n-1).
func opNames(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = "Op" + itoa(i)
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
