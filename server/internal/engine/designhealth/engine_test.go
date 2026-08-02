package designhealth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

	// CC-* call-chain family (2026-07-30 callchain-realization; batch 1 landed
	// 2026-08-01 Task 8, batch 2 landed 2026-08-01 Task 9, batch 3 landed
	// 2026-08-01 Task 10 — ALL 16 dynamic views now realized). Eligible node
	// count (action + timeEvent + acceptEvent nodes — the step-REQUIRING
	// kinds) was 124 corpus-wide across the 16 use cases; Task 10's three
	// slot-4 honesty amendments (download's work-merges/git-clone/provider-
	// archive/hold-source, onboard's create-connected, cost-projection's
	// report-unknown — each converted action → note, zero edge edits) removed
	// 6 eligible nodes, so the corpus-wide base is now 118. Batch 1+2 realized
	// 81 of those (uc1-drive-system-design 14, uc2-commit-project-option 7,
	// uc3-execute-construction-activity 11, uc4-operate-delivered-system 12,
	// uc5-bill-user-for-usage 7, var-manage-projects 7, var-track-weekly-
	// progress 7, var-replan-scope-change 10, var-retry-declined-invoice 6).
	// Batch 3 realizes the remaining 37 across the final seven views:
	// var-onboard-new-customer (5 eligible post-Amendment-B),
	// var-add-use-case (7), var-view-state-log (4),
	// var-download-source (1 eligible post-Amendment-A),
	// var-view-cost-projection (5 eligible post-Amendment-C),
	// var-ask-review-question (7), var-send-back-redraft (8). 81 + 37 = 118 =
	// the full corpus. So:
	//
	//   * CC-COVERAGE fires ZERO times — every step-REQUIRING activity node in
	//     the committed state is now realized by exactly one step. 16/16
	//     dynamic views realized.
	//   * CC-TRIGGER-EVENT stays at 0 — no batch touched a slot-4 diagram
	//     entry's trigger shape.
	//   * every step-walking rule (CC-STEP-*, CC-ENDPOINT-RESOLVES, CC-ACTOR-*,
	//     CC-PATH-CONNECTED) stays SILENT — genuinely clean on all sixteen
	//     realized views. That is the realization's success criterion, so the
	//     assertAbsent block below is now load-bearing over all sixteen views:
	//     a regression in the walker shows up here.
	//
	// The exact counts mirror the platform framework-go/methodcheck gate's
	// inventory on this same committed document (verified 2026-08-01 against
	// `aiarch-state-mcp validate --slot System`, which runs both tiers and
	// reports 2x these counts) — a drift here means this mirror parses the
	// slot-4 activity / slot-5 step shapes differently than the platform gate.
	assertAbsent(t, got, RuleCCCoverage)
	// CC-TRIGGER-EVENT (Task 7, 2026-08-01): the 5 timer/busMessage use cases
	// that lacked a matching event entry each now declare one (execute's
	// pump-fires timeEvent, operate's schedule-fires timeEvent, bill's
	// period-elapses timeEvent, retry's charge-declined timeEvent, replan's
	// replan-triggered acceptEvent) — the rule is silent on the committed state.
	assertAbsent(t, got, RuleCCTriggerEvent)
	var ccCoverageCount, ccTriggerCount int
	ccCoverageUseCases := map[string]bool{}
	for _, f := range findings {
		switch f.RuleID {
		case RuleCCCoverage:
			ccCoverageCount++
			if f.Location != nil {
				ccCoverageUseCases[f.Location.Section] = true
			}
		case RuleCCTriggerEvent:
			ccTriggerCount++
		}
	}
	if ccCoverageCount != 0 {
		t.Errorf("CC-COVERAGE fired %d times on the committed state, want 0 (all 16 dynamic views are realized — matches the platform gate's inventory; investigate any drift, don't just re-pin)", ccCoverageCount)
	}
	if len(ccCoverageUseCases) != 0 {
		t.Errorf("CC-COVERAGE fired across %d use cases, want 0 — all 16 committed use cases' dynamic views are realized (drive-system-design; batch 1's commit/execute/operate/bill; batch 2's manage-projects/track-weekly/replan/retry; batch 3's onboard/add-use-case/view-log/download/cost-projection/ask/send-back)", len(ccCoverageUseCases))
	}
	if ccCoverageUseCases["useCase drive-system-design"] {
		t.Error("CC-COVERAGE fired for drive-system-design, whose dynamic view is fully realized by the PoC design amendment — the realization or the rule has drifted")
	}
	// Batch 1 named-culprit guards (Task 8, F6): a regression that un-realizes
	// any of the four batch-1 views, or that the walker stops recognizing as
	// realized, must name itself here rather than only moving the tally.
	if ccCoverageUseCases["useCase commit-to-a-project-option"] {
		t.Error("CC-COVERAGE fired for commit-to-a-project-option, whose dynamic view is fully realized by the batch-1 design amendment — the realization or the rule has drifted")
	}
	if ccCoverageUseCases["useCase execute-a-construction-activity"] {
		t.Error("CC-COVERAGE fired for execute-a-construction-activity, whose dynamic view is fully realized by the batch-1 design amendment — the realization or the rule has drifted")
	}
	if ccCoverageUseCases["useCase operate-a-delivered-system"] {
		t.Error("CC-COVERAGE fired for operate-a-delivered-system, whose dynamic view is fully realized by the batch-1 design amendment — the realization or the rule has drifted")
	}
	if ccCoverageUseCases["useCase bill-the-user-for-usage"] {
		t.Error("CC-COVERAGE fired for bill-the-user-for-usage, whose dynamic view is fully realized by the batch-1 design amendment — the realization or the rule has drifted")
	}
	// Batch 2 named-culprit guards (Task 9, F6): a regression that un-realizes
	// any of the four batch-2 views, or that the walker stops recognizing as
	// realized, must name itself here rather than only moving the tally.
	if ccCoverageUseCases["useCase manage-projects"] {
		t.Error("CC-COVERAGE fired for manage-projects, whose dynamic view is fully realized by the batch-2 design amendment — the realization or the rule has drifted")
	}
	if ccCoverageUseCases["useCase track-weekly-project-progress"] {
		t.Error("CC-COVERAGE fired for track-weekly-project-progress, whose dynamic view is fully realized by the batch-2 design amendment — the realization or the rule has drifted")
	}
	if ccCoverageUseCases["useCase replan-under-scope-change"] {
		t.Error("CC-COVERAGE fired for replan-under-scope-change, whose dynamic view is fully realized by the batch-2 design amendment — the realization or the rule has drifted")
	}
	if ccCoverageUseCases["useCase retry-a-declined-service-invoice"] {
		t.Error("CC-COVERAGE fired for retry-a-declined-service-invoice, whose dynamic view is fully realized by the batch-2 design amendment — the realization or the rule has drifted")
	}
	// Batch 3 named-culprit guards (Task 10, F7): with ccCoverageCount pinned
	// at 0, these guards carry the WHOLE regression duty for the final seven
	// views — a regression that un-realizes any of them must name itself here
	// rather than only moving a tally that would otherwise stay silent at 0.
	if ccCoverageUseCases["useCase onboard-a-new-customer"] {
		t.Error("CC-COVERAGE fired for onboard-a-new-customer, whose dynamic view is fully realized by the batch-3 design amendment — the realization or the rule has drifted")
	}
	if ccCoverageUseCases["useCase add-a-use-case-to-an-in-flight-project"] {
		t.Error("CC-COVERAGE fired for add-a-use-case-to-an-in-flight-project, whose dynamic view is fully realized by the batch-3 design amendment — the realization or the rule has drifted")
	}
	if ccCoverageUseCases["useCase view-the-project-state-log"] {
		t.Error("CC-COVERAGE fired for view-the-project-state-log, whose dynamic view is fully realized by the batch-3 design amendment — the realization or the rule has drifted")
	}
	if ccCoverageUseCases["useCase download-generated-source-code"] {
		t.Error("CC-COVERAGE fired for download-generated-source-code, whose dynamic view is fully realized by the batch-3 design amendment — the realization or the rule has drifted")
	}
	if ccCoverageUseCases["useCase view-operating-cost-projection"] {
		t.Error("CC-COVERAGE fired for view-operating-cost-projection, whose dynamic view is fully realized by the batch-3 design amendment — the realization or the rule has drifted")
	}
	if ccCoverageUseCases["useCase ask-a-clarifying-question-during-review"] {
		t.Error("CC-COVERAGE fired for ask-a-clarifying-question-during-review, whose dynamic view is fully realized by the batch-3 design amendment — the realization or the rule has drifted")
	}
	if ccCoverageUseCases["useCase send-back-change-requests-for-a-redraft"] {
		t.Error("CC-COVERAGE fired for send-back-change-requests-for-a-redraft, whose dynamic view is fully realized by the batch-3 design amendment — the realization or the rule has drifted")
	}
	if ccTriggerCount != 0 {
		t.Errorf("CC-TRIGGER-EVENT fired %d times on the committed state, want 0 (Task 7 gave each of the 5 timer/busMessage use cases a matching event entry)", ccTriggerCount)
	}
	// Vacuous check retained for all 16 views; genuinely clean on every one now
	// that all 16 dynamic views are realized.
	assertAbsent(t, got, RuleCCViewUseCase)
	assertAbsent(t, got, RuleCCStepNode)
	assertAbsent(t, got, RuleCCStepUnique)
	assertAbsent(t, got, RuleCCStepNonempty)
	assertAbsent(t, got, RuleCCEndpoint)
	assertAbsent(t, got, RuleCCActorEdge)
	assertAbsent(t, got, RuleCCActorLane)
	assertAbsent(t, got, RuleCCPathConnected)

	// CC-DECIDED-BY / CUC-ACTOR-REQUIRED (2026-07-31 rollout rulings): expect
	// ZERO findings on the committed state. Every clientAction use case in the
	// committed set declares at least one actor, and no committed node carries
	// a decidedBy or a call an alt tag — both fields are new and no authored
	// data uses them yet. A finding here means either the data drifted or the
	// decode/resolution joins drifted from the platform tier's.
	assertAbsent(t, got, RuleCUCActorRequired)
	assertAbsent(t, got, RuleCCDecidedBy)
}

// TestCC_AllRulesAreErrorSeverity pins ccLiveSeverity post-flip (Task 12,
// 2026-08-01, "gates: live tier flips to Error"): the whole CC-* family (and its
// CoreUseCases-attributed sibling CUC-ACTOR-REQUIRED) is now the HARD GATE — a
// firing rule is SeverityError, mirroring the platform's
// framework-go/methodcheck TestCC_AllRulesAreErrorSeverity. There is no
// PoC-advisory posture left to pin honestly by name; unlike the platform tier
// this app-side engine never carried a test asserting the OLD (Warning) posture
// by name, so there is nothing to rename — this test is new.
func TestCC_AllRulesAreErrorSeverity(t *testing.T) {
	doc := withSlots(
		sysDoc(comps(comp("c", "client"), comp("m", "manager")),
			rels(rel("c", "m", "sync")),
			dvs(dvSteps("uc-a", step("ghost-node", edge("c", "m", "sync"))))),
		useCasesDoc(ucCase("uc-a", "clientAction", ucActors(),
			actDiagram(
				actNodes(actNode("start", "start"), actNode("act", "action"), actNode("end", "end")),
				actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
	)
	findings := EvaluateRaw(mustMarshal(t, doc))
	got := indexBySeverity(findings)
	assertPresent(t, got, RuleCCStepNode, methodcheck.SeverityError)
	if ccLiveSeverity != methodcheck.SeverityError {
		t.Fatalf("every CC-* rule must be the hard gate (SeverityError) post-flip, got %v", ccLiveSeverity)
	}
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

		// ---- CC-* call-chain correspondence family (2026-07-30 callchain-realization) ----
		{
			name: "call-chain: view references unresolvable useCaseId",
			doc: withSlots(
				sysDoc(comps(comp("c", "client"), comp("m", "manager")),
					rels(rel("c", "m", "sync")),
					dvs(dvSteps("uc-typo", step("act", edge("c", "m", "sync"))))),
				useCasesDoc(ucCase("uc-real", "clientAction", ucActors(ucActor("user")),
					actDiagram(
						actNodes(actNode("start", "start"), actNode("act", "action"), actNode("end", "end")),
						actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
			),
			wantRule: RuleCCViewUseCase, wantSev: ccLiveSeverity,
		},
		{
			name: "call-chain: step keys a node the diagram does not declare",
			doc: withSlots(
				sysDoc(comps(comp("c", "client"), comp("m", "manager")),
					rels(rel("c", "m", "sync")),
					dvs(dvSteps("uc-a", step("ghost-node", edge("c", "m", "sync"))))),
				useCasesDoc(ucCase("uc-a", "clientAction", ucActors(),
					actDiagram(
						actNodes(actNode("start", "start"), actNode("act", "action"), actNode("end", "end")),
						actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
			),
			wantRule: RuleCCStepNode, wantSev: ccLiveSeverity,
		},
		{
			name: "call-chain: duplicate step for the same activity node",
			doc: withSlots(
				sysDoc(comps(comp("c", "client"), comp("m", "manager")),
					rels(rel("c", "m", "sync")),
					dvs(dvSteps("uc-b",
						step("act", edge("c", "m", "sync")),
						step("act", edge("c", "m", "sync"))))),
				useCasesDoc(ucCase("uc-b", "clientAction", ucActors(),
					actDiagram(
						actNodes(actNode("start", "start"), actNode("act", "action"), actNode("end", "end")),
						actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
			),
			wantRule: RuleCCStepUnique, wantSev: ccLiveSeverity,
		},
		{
			name: "call-chain: action node realized by no step",
			doc: withSlots(
				sysDoc(comps(comp("c", "client"), comp("m", "manager")),
					rels(rel("c", "m", "sync")),
					dvs(dvSteps("uc-c"))),
				useCasesDoc(ucCase("uc-c", "clientAction", ucActors(),
					actDiagram(
						actNodes(actNode("start", "start"), actNode("act", "action"), actNode("end", "end")),
						actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
			),
			wantRule: RuleCCCoverage, wantSev: ccLiveSeverity,
		},
		{
			name: "call-chain: realized step makes no call",
			doc: withSlots(
				sysDoc(comps(comp("c", "client"), comp("m", "manager")),
					rels(rel("c", "m", "sync")),
					dvs(dvSteps("uc-d", step("act")))),
				useCasesDoc(ucCase("uc-d", "clientAction", ucActors(),
					actDiagram(
						actNodes(actNode("start", "start"), actNode("act", "action"), actNode("end", "end")),
						actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
			),
			wantRule: RuleCCStepNonempty, wantSev: ccLiveSeverity,
		},
		{
			name: "call-chain: call endpoint resolves to nothing",
			doc: withSlots(
				sysDoc(comps(comp("c", "client"), comp("m", "manager")),
					rels(rel("c", "m", "sync")),
					dvs(dvSteps("uc-e", step("act", edge("c", "ghost-component", "sync"))))),
				useCasesDoc(ucCase("uc-e", "clientAction", ucActors(),
					actDiagram(
						actNodes(actNode("start", "start"), actNode("act", "action"), actNode("end", "end")),
						actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
			),
			wantRule: RuleCCEndpoint, wantSev: ccLiveSeverity,
		},
		{
			name: "call-chain: actor calls a non-Client component",
			doc: withSlots(
				sysDoc(comps(comp("c", "client"), comp("m", "manager")),
					rels(rel("c", "m", "sync")),
					dvs(dvSteps("uc-f", step("act", edge("user", "m", "sync"))))),
				useCasesDoc(ucCase("uc-f", "clientAction", ucActors(ucActor("user")),
					actDiagram(
						actNodes(actNode("start", "start"), actNode("act", "action"), actNode("end", "end")),
						actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
			),
			wantRule: RuleCCActorEdge, wantSev: ccLiveSeverity,
		},
		{
			name: "call-chain: lane-linked node's step never touches its actor",
			doc: withSlots(
				sysDoc(comps(comp("c", "client"), comp("m", "manager")),
					rels(rel("c", "m", "sync")),
					dvs(dvSteps("uc-g", step("act", edge("c", "m", "sync"))))),
				useCasesDoc(ucCase("uc-g", "clientAction", ucActors(ucActor("user")),
					actDiagram(
						actNodes(actNode("start", "start"), actNodeLane("act", "action", "User", "user"), actNode("end", "end")),
						actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
			),
			wantRule: RuleCCActorLane, wantSev: ccLiveSeverity,
		},
		{
			name: "call-chain: timer-triggered use case has no timeEvent entry",
			doc: withSlots(
				sysDoc(comps(comp("c", "client"), comp("m", "manager")),
					rels(rel("c", "m", "sync")),
					dvs(dvSteps("uc-h", step("act", edge("c", "m", "sync"))))),
				useCasesDoc(ucCase("uc-h", "timer", ucActors(),
					actDiagram(
						actNodes(actNode("start", "start"), actNode("act", "action"), actNode("end", "end")),
						actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
			),
			wantRule: RuleCCTriggerEvent, wantSev: ccLiveSeverity,
		},
		{
			name: "call-chain: later step calls from a component the chain never reached",
			doc: withSlots(
				sysDoc(comps(comp("c", "client"), comp("m", "manager"), comp("ra", "resourceAccess"), comp("res", "resource")),
					rels(rel("c", "m", "sync"), rel("m", "ra", "sync"), rel("ra", "res", "sync")),
					dvs(dvSteps("uc-i",
						step("a1", edge("user", "c", "sync"), edge("c", "m", "sync")),
						step("a2", edge("ra", "res", "sync"))))),
				useCasesDoc(ucCase("uc-i", "clientAction", ucActors(ucActor("user")),
					actDiagram(
						actNodes(actNode("start", "start"), actNode("a1", "action"), actNode("a2", "action"), actNode("end", "end")),
						actEdges(actEdge("start", "a1"), actEdge("a1", "a2"), actEdge("a2", "end"))))),
			),
			wantRule: RuleCCPathConnected, wantSev: ccLiveSeverity,
		},
		{
			// CUC-ACTOR-REQUIRED (rollout rulings 2026-07-31): a clientAction use
			// case must name who initiates it.
			name: "call-chain: clientAction use case declares no actors",
			doc: withSlots(
				sysDoc(comps(comp("m", "manager")), rels(), nil),
				useCasesDoc(ucCase("uc-no-actor", "clientAction", ucActors(),
					actDiagram(
						actNodes(actNode("start", "start"), actNode("act", "action"), actNode("end", "end")),
						actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
			),
			wantRule: RuleCUCActorRequired, wantSev: ccLiveSeverity,
		},
		{
			// CC-DECIDED-BY (rollout rulings 2026-07-31), placement half: a
			// decidedBy on a non-decision/switch node is misplaced even though the
			// value itself would otherwise resolve.
			name: "call-chain: decidedBy on a non-decision node",
			doc: withSlots(
				sysDoc(comps(comp("c", "client"), comp("m", "manager")),
					rels(rel("c", "m", "sync")),
					dvs(dvSteps("uc-decided", step("act", edge("c", "m", "sync"))))),
				useCasesDoc(ucCase("uc-decided", "clientAction", ucActors(ucActor("user")),
					actDiagram(
						actNodes(actNode("start", "start"), actNodeDecided("act", "action", "m"), actNode("end", "end")),
						actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
			),
			wantRule: RuleCCDecidedBy, wantSev: ccLiveSeverity,
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

// TestCUCActorRequired pins CUC-ACTOR-REQUIRED (founder ruling R-A, rollout
// rulings 2026-07-31) across its full trigger matrix: a clientAction use case
// must name who initiates it (fires with zero actors, silent with one);
// timer- and busMessage-triggered use cases are started by the clock or the
// bus and legitimately declare none.
func TestCUCActorRequired(t *testing.T) {
	build := func(id, trigger string, actors []any) []byte {
		return mustMarshal(t, withSlots(
			sysDoc(comps(comp("m", "manager")), rels(), nil),
			useCasesDoc(ucCase(id, trigger, actors,
				actDiagram(
					actNodes(actNode("start", "start"), actNode("act", "action"), actNode("end", "end")),
					actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
		))
	}

	// clientAction with zero actors → fires at ccLiveSeverity, use-case-scoped
	// section (the grammar the spec mandates, deliberately not the "use case
	// %d (%s)" grammar the older CUC-* checks would use).
	findings := EvaluateRaw(build("uc-solo", "clientAction", ucActors()))
	got := indexBySeverity(findings)
	assertPresent(t, got, RuleCUCActorRequired, ccLiveSeverity)
	found := false
	for _, f := range findings {
		if f.RuleID == RuleCUCActorRequired {
			found = true
			if f.Location == nil || f.Location.Section != "useCase uc-solo" {
				t.Errorf("CUC-ACTOR-REQUIRED section must be %q, got %+v", "useCase uc-solo", f.Location)
			}
		}
	}
	if !found {
		t.Fatal("CUC-ACTOR-REQUIRED did not fire at all")
	}

	// clientAction WITH an actor → silent.
	got = indexBySeverity(EvaluateRaw(build("uc-has-actor", "clientAction", ucActors(ucActor("user")))))
	assertAbsent(t, got, RuleCUCActorRequired)

	// timer-triggered with zero actors → the clock starts it, not a person.
	got = indexBySeverity(EvaluateRaw(build("uc-timer", "timer", ucActors())))
	assertAbsent(t, got, RuleCUCActorRequired)

	// busMessage-triggered with zero actors → same reasoning.
	got = indexBySeverity(EvaluateRaw(build("uc-bus", "busMessage", ucActors())))
	assertAbsent(t, got, RuleCUCActorRequired)
}

// TestCCDecidedBy pins CC-DECIDED-BY's two halves (rollout rulings
// 2026-07-31): PLACEMENT (only a decision/switch node resolves a branch, so
// only those kinds may carry a decidedBy) and RESOLUTION (the value resolves
// exactly like a call endpoint — against the System's components UNION the
// owning use case's actors; naming neither is dangling, naming both is
// ambiguous).
func TestCCDecidedBy(t *testing.T) {
	// decidedDiagram builds start -> d(decision) -> act -> end, with decidedBy
	// attached to the node named by attachTo — "d" for RESOLUTION scenarios,
	// "act" for the PLACEMENT scenario.
	decidedDiagram := func(attachTo, decidedBy string) map[string]any {
		node := func(id, kind string) map[string]any {
			if id == attachTo {
				return actNodeDecided(id, kind, decidedBy)
			}
			return actNode(id, kind)
		}
		return actDiagram(
			actNodes(node("start", "start"), node("d", "decision"), node("act", "action"), node("end", "end")),
			actEdges(actEdge("start", "d"), actEdge("d", "act"), actEdge("act", "end")))
	}
	build := func(actors []any, decidedByAttachTo, decidedByValue string) []byte {
		return mustMarshal(t, withSlots(
			sysDoc(comps(comp("c", "client"), comp("m", "manager")),
				rels(rel("c", "m", "sync")),
				dvs(dvSteps("uc-dec", step("act", edge("c", "m", "sync"))))),
			useCasesDoc(ucCase("uc-dec", "clientAction", actors, decidedDiagram(decidedByAttachTo, decidedByValue))),
		))
	}

	// Placement: decidedBy on an action node fires even though the value
	// ("m") would otherwise resolve to a System Component.
	got := indexBySeverity(EvaluateRaw(build(ucActors(ucActor("user")), "act", "m")))
	assertPresent(t, got, RuleCCDecidedBy, ccLiveSeverity)

	// Unresolvable: neither a component nor an actor.
	got = indexBySeverity(EvaluateRaw(build(ucActors(ucActor("user")), "d", "nobody")))
	assertPresent(t, got, RuleCCDecidedBy, ccLiveSeverity)

	// Resolves to a System Component → silent.
	got = indexBySeverity(EvaluateRaw(build(ucActors(ucActor("user")), "d", "m")))
	assertAbsent(t, got, RuleCCDecidedBy)

	// Resolves to the owning use case's actor → silent.
	got = indexBySeverity(EvaluateRaw(build(ucActors(ucActor("user")), "d", "user")))
	assertAbsent(t, got, RuleCCDecidedBy)

	// Ambiguous: the decider id collides with BOTH a Component and an actor.
	findings := EvaluateRaw(build(ucActors(ucActor("c")), "d", "c"))
	got = indexBySeverity(findings)
	assertPresent(t, got, RuleCCDecidedBy, ccLiveSeverity)
	found := false
	for _, f := range findings {
		if f.RuleID == RuleCCDecidedBy {
			found = true
			if f.Location == nil || f.Location.Section != "useCase uc-dec" {
				t.Errorf("CC-DECIDED-BY section must be %q, got %+v", "useCase uc-dec", f.Location)
			}
		}
	}
	if !found {
		t.Fatal("CC-DECIDED-BY did not fire for the ambiguous case")
	}
}

// TestCCPathConnectedAltGroupBothSeedReached pins that alternative groups
// (rollout rulings 2026-07-31) are a PRESENTATION grouping that changes no
// CC-* verdict: every call of an alt group seeds the reached set, so a later
// call continuing from EITHER alternative's endpoint is connected. The
// fixture enters through two equivalent surfaces (a web and an MCP client)
// tagged as one alt group, then calls the Manager from each — if an
// alternative failed to seed, the second entry's continuation would be
// reported as a disconnect.
func TestCCPathConnectedAltGroupBothSeedReached(t *testing.T) {
	doc := withSlots(
		sysDoc(
			comps(comp("web-client", "client"), comp("mcp-client", "client"), comp("m", "manager")),
			rels(rel("web-client", "m", "sync"), rel("mcp-client", "m", "sync")),
			dvs(dvSteps("uc-alt", step("act",
				altEdge("user", "web-client", "sync", "entry"),
				altEdge("user", "mcp-client", "sync", "entry"),
				altEdge("web-client", "m", "sync", "drive"),
				altEdge("mcp-client", "m", "sync", "drive"),
			))),
		),
		useCasesDoc(ucCase("uc-alt", "clientAction", ucActors(ucActor("user")),
			actDiagram(
				actNodes(actNode("start", "start"), actNode("act", "action"), actNode("end", "end")),
				actEdges(actEdge("start", "act"), actEdge("act", "end"))))),
	)
	findings := EvaluateRaw(mustMarshal(t, doc))
	for _, f := range findings {
		if strings.HasPrefix(string(f.RuleID), "CC-") {
			t.Errorf("an alt-grouped both-surface entry is fully connected; no CC-* finding may fire, got %s: %s", f.RuleID, f.Message)
		}
	}
}

// TestChainEntryManagerAltGroupToDifferentManagersFires pins the one thing an
// alternative group does NOT get to do (rollout rulings 2026-07-31). Alt is
// inert for the CC-* verdicts and for per-edge legality, but the per-view
// §6a/§6b chain rules (rules_chains.go) read a view's calls as concurrent: two
// alternatives entering two different Managers is one Client driving two
// Managers, and RuleChainEntryManager — this package's analog of the
// platform's DV-SINGLE-MGR — says so. Alternatives are two doors into ONE
// chain (same Manager), not two chains.
func TestChainEntryManagerAltGroupToDifferentManagersFires(t *testing.T) {
	doc := sysDoc(
		comps(comp("c", "client"), comp("m1", "manager"), comp("m2", "manager")),
		rels(rel("c", "m1", "sync"), rel("c", "m2", "sync")),
		dvs(dynView("uc-alt-two-mgrs", altEdge("c", "m1", "sync", "entry"), altEdge("c", "m2", "sync", "entry"))),
	)
	got := indexBySeverity(EvaluateRaw(mustMarshal(t, doc)))
	assertPresent(t, got, RuleChainEntryManager, methodcheck.SeverityWarning)
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

// altEdge builds a call edge carrying an alt (surface-alternative group) tag
// — the CC-* family's alt-inertness tests and the RuleChainEntryManager
// alt-cardinality pin both join on it.
func altEdge(from, to, mode, alt string) map[string]any {
	m := edge(from, to, mode)
	m["alt"] = alt
	return m
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

// ---- CC-* call-chain fixture builders (step-keyed dynamic views + the slot-4
// activity/trigger/actor shape the correspondence family joins against) ----

// dvSteps builds a step-keyed dynamic view fixture: useCaseId and key both set
// to useCaseID, plus the given callStep-shaped steps (see step()). Unlike
// dynView (which wraps its edges into a single synthetic "step1"), the CALLER
// picks each step's activity-node key — the shape the CC-* rules join on.
func dvSteps(useCaseID string, steps ...map[string]any) map[string]any {
	ss := make([]any, len(steps))
	for i, s := range steps {
		ss[i] = s
	}
	return map[string]any{"useCaseId": useCaseID, "key": useCaseID, "steps": ss}
}

// step builds one callStep fixture: the activity node it realizes, plus its
// ordered calls (built with edge(from, to, mode)).
func step(nodeID string, es ...map[string]any) map[string]any {
	calls := make([]any, len(es))
	for i, e := range es {
		calls[i] = e
	}
	return map[string]any{"activityNodeId": nodeID, "calls": calls}
}

// useCasesDoc builds a coreUseCases slot (kind 4) fixture from full use-case
// fixtures (see ucCase) — the slot-4 activity/trigger/actor surface the CC-*
// family joins a dynamic view against.
func useCasesDoc(ucs ...map[string]any) slotSpec {
	arr := make([]any, len(ucs))
	for i, u := range ucs {
		arr[i] = map[string]any{"useCase": u}
	}
	return slot("4", 4, map[string]any{"decisions": arr})
}

// ucCase builds one full use-case fixture: id, trigger, its actor roster (see
// ucActors/ucActor), and its activity diagram (see actDiagram).
func ucCase(id, trigger string, actors []any, activity map[string]any) map[string]any {
	return map[string]any{"id": id, "trigger": trigger, "actors": actors, "activity": activity}
}

func ucActor(id string) map[string]any {
	return map[string]any{"id": id}
}

func ucActors(as ...map[string]any) []any {
	out := make([]any, len(as))
	for i, a := range as {
		out[i] = a
	}
	return out
}

// actDiagram builds an activity-diagram fixture from node/edge lists (see
// actNodes/actEdges).
func actDiagram(nodes, edges []any) map[string]any {
	return map[string]any{"nodes": nodes, "edges": edges}
}

func actNode(id, kind string) map[string]any {
	return map[string]any{"id": id, "kind": kind}
}

// actNodeLane builds an activity node carrying a swim-lane actor link
// (roleName + linkedActorId) — the CC-ACTOR-LANE join key.
func actNodeLane(id, kind, roleName, linkedActorID string) map[string]any {
	return map[string]any{"id": id, "kind": kind, "roleName": roleName, "linkedActorId": linkedActorID}
}

// actNodeDecided builds an activity node carrying a decidedBy attribution —
// the CC-DECIDED-BY join key.
func actNodeDecided(id, kind, decidedBy string) map[string]any {
	return map[string]any{"id": id, "kind": kind, "decidedBy": decidedBy}
}

func actNodes(ns ...map[string]any) []any {
	out := make([]any, len(ns))
	for i, n := range ns {
		out[i] = n
	}
	return out
}

func actEdge(from, to string) map[string]any {
	return map[string]any{"from": from, "to": to}
}

func actEdges(es ...map[string]any) []any {
	out := make([]any, len(es))
	for i, e := range es {
		out[i] = e
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

// ---------------------------------------------------------------------------
// activityPaths WALKER — ported from
// framework-go/methodcheck/activitypaths_test.go (2026-07-30
// callchain-realization / 2026-07-31 rollout rulings bounding pass). One
// behavior per Test func, inline activityDiagram literal fixtures — the
// platform's own idiom for its walker test file, adopted verbatim here since
// this package had no walker-specific test file of its own before this port.
// ---------------------------------------------------------------------------

func TestPaths_LinearStartActionEnd(t *testing.T) {
	a := activityDiagram{
		Nodes: []activityNode{
			{ID: "s", Kind: "start"}, {ID: "a", Kind: "action"}, {ID: "e", Kind: "end"},
		},
		Edges: []activityEdge{{From: "s", To: "a"}, {From: "a", To: "e"}},
	}
	got := activityPaths(a)
	if len(got) != 1 {
		t.Fatalf("want 1 path, got %d: %+v", len(got), got)
	}
	want := []string{"s", "a", "e"}
	if !equalStrings(got[0].Nodes, want) {
		t.Fatalf("want nodes %v, got %v", want, got[0].Nodes)
	}
	if got[0].Entry.NodeID != "s" || got[0].Entry.Kind != "start" {
		t.Fatalf("want entry {s start}, got %+v", got[0].Entry)
	}
}

func TestPaths_DecisionBranches(t *testing.T) {
	a := activityDiagram{
		Nodes: []activityNode{{ID: "s", Kind: "start"}, {ID: "d", Kind: "decision"},
			{ID: "x", Kind: "action"}, {ID: "y", Kind: "action"}, {ID: "e", Kind: "end"}},
		Edges: []activityEdge{{From: "s", To: "d"}, {From: "d", To: "x", Guard: "[yes]"},
			{From: "d", To: "y", Guard: "[no]"}, {From: "x", To: "e"}, {From: "y", To: "e"}},
	}
	got := activityPaths(a)
	if len(got) != 2 {
		t.Fatalf("want 2 paths, got %d: %v", len(got), got)
	}
}

// TestPaths_LoopBackEdgeTraversedAtMostOnce builds a two-decision loop —
// d1 -> a -> d2, with d2's back-edge returning into "a" a SECOND time (via a
// different edge than the one that reached "a" the first time) — and asserts
// the walk is finite and the loop body ("a") never appears more than twice in
// any one path.
func TestPaths_LoopBackEdgeTraversedAtMostOnce(t *testing.T) {
	a := activityDiagram{
		Nodes: []activityNode{
			{ID: "s", Kind: "start"}, {ID: "d1", Kind: "decision"}, {ID: "a", Kind: "action"},
			{ID: "d2", Kind: "decision"}, {ID: "e", Kind: "end"},
		},
		Edges: []activityEdge{
			{From: "s", To: "d1"},
			{From: "d1", To: "a", Guard: "[enter]"},
			{From: "d1", To: "e", Guard: "[skip]"},
			{From: "a", To: "d2"},
			{From: "d2", To: "a", Guard: "[again]"}, // back-edge into the loop body
			{From: "d2", To: "e", Guard: "[done]"},
		},
	}
	got := activityPaths(a)
	if len(got) == 0 {
		t.Fatalf("want at least one path, got 0")
	}
	sawTwice := false
	for _, p := range got {
		count := 0
		for _, id := range p.Nodes {
			if id == "a" {
				count++
			}
		}
		if count > 2 {
			t.Fatalf("loop body must appear at most twice in any path, got %d in %v", count, p.Nodes)
		}
		if count == 2 {
			sawTwice = true
		}
	}
	if !sawTwice {
		t.Fatalf("expected at least one path where the back-edge is actually taken (loop body appears twice), got %+v", got)
	}
}

// TestPaths_ForkWithoutJoinConcatenatesBranches: a fork with two branches and
// no join must yield ONE path with both branches' walks concatenated in
// declared edge order — the fork does not branch into separate paths.
func TestPaths_ForkWithoutJoinConcatenatesBranches(t *testing.T) {
	a := activityDiagram{
		Nodes: []activityNode{
			{ID: "s", Kind: "start"}, {ID: "f", Kind: "fork"},
			{ID: "x", Kind: "action"}, {ID: "y", Kind: "action"},
		},
		Edges: []activityEdge{
			{From: "s", To: "f"}, {From: "f", To: "x"}, {From: "f", To: "y"},
		},
	}
	got := activityPaths(a)
	if len(got) != 1 {
		t.Fatalf("want 1 path, got %d: %+v", len(got), got)
	}
	want := []string{"s", "f", "x", "y"}
	if !equalStrings(got[0].Nodes, want) {
		t.Fatalf("want nodes %v (branches concatenated in declared order), got %v", want, got[0].Nodes)
	}
}

// TestPaths_TwoEndNodesBothTerminate: a decision branching to two DISTINCT end
// nodes must produce two paths, each terminating at its own end node.
func TestPaths_TwoEndNodesBothTerminate(t *testing.T) {
	a := activityDiagram{
		Nodes: []activityNode{
			{ID: "s", Kind: "start"}, {ID: "d", Kind: "decision"},
			{ID: "e1", Kind: "end"}, {ID: "e2", Kind: "end"},
		},
		Edges: []activityEdge{
			{From: "s", To: "d"},
			{From: "d", To: "e1", Guard: "[yes]"},
			{From: "d", To: "e2", Guard: "[no]"},
		},
	}
	got := activityPaths(a)
	if len(got) != 2 {
		t.Fatalf("want 2 paths, got %d: %+v", len(got), got)
	}
	ends := map[string]bool{}
	for _, p := range got {
		ends[p.Nodes[len(p.Nodes)-1]] = true
	}
	if !ends["e1"] || !ends["e2"] {
		t.Fatalf("want paths terminating at both e1 and e2, got %+v", got)
	}
}

// TestPaths_EventNodeIsItsOwnEntry: an edge-less timeEvent node is its own
// entry — a diagram with a start-rooted path AND a timeEvent-rooted path must
// yield both, with Entry.Kind distinguishing them.
func TestPaths_EventNodeIsItsOwnEntry(t *testing.T) {
	a := activityDiagram{
		Nodes: []activityNode{
			{ID: "s", Kind: "start"}, {ID: "act", Kind: "action"}, {ID: "e", Kind: "end"},
			{ID: "tick", Kind: "timeEvent"},
		},
		Edges: []activityEdge{{From: "s", To: "act"}, {From: "act", To: "e"}},
	}
	got := activityPaths(a)
	if len(got) != 2 {
		t.Fatalf("want 2 paths (one per entry), got %d: %+v", len(got), got)
	}
	var sawStart, sawEvent bool
	for _, p := range got {
		switch p.Entry.Kind {
		case "start":
			sawStart = true
			if !equalStrings(p.Nodes, []string{"s", "act", "e"}) {
				t.Fatalf("start-rooted path wrong, got %v", p.Nodes)
			}
		case "timeEvent":
			sawEvent = true
			if !equalStrings(p.Nodes, []string{"tick"}) {
				t.Fatalf("event-rooted path must be just the event node itself, got %v", p.Nodes)
			}
		}
	}
	if !sawStart || !sawEvent {
		t.Fatalf("want both a start-rooted and an event-rooted path, got %+v", got)
	}
}

// TestPaths_ForkBranchWithInternalDecisionCrossProducts is the fix-round-1
// regression test for the confirmed double-charge/silent-drop defect: fork
// branch 1 leads into its OWN internal 2-way decision (d), fork branch 2 is a
// plain terminal action (y) with no branching of its own. The two must
// CROSS-PRODUCT — branch 1's 2 alternatives x branch 2's 1 alternative — into
// exactly 2 final combinations, enumerated in declared edge order.
func TestPaths_ForkBranchWithInternalDecisionCrossProducts(t *testing.T) {
	a := activityDiagram{
		Nodes: []activityNode{
			{ID: "s", Kind: "start"}, {ID: "f", Kind: "fork"},
			{ID: "d", Kind: "decision"}, {ID: "x1", Kind: "action"}, {ID: "x2", Kind: "action"},
			{ID: "y", Kind: "action"},
		},
		Edges: []activityEdge{
			{From: "s", To: "f"},
			{From: "f", To: "d"}, // branch 1: has its OWN internal decision
			{From: "f", To: "y"}, // branch 2: plain terminal, no branching
			{From: "d", To: "x1", Guard: "[p]"},
			{From: "d", To: "x2", Guard: "[q]"},
		},
	}
	got := activityPaths(a)
	if len(got) != 2 {
		t.Fatalf("want 2 cross-producted combinations, got %d: %+v", len(got), got)
	}
	want0 := []string{"s", "f", "d", "x1", "y"}
	want1 := []string{"s", "f", "d", "x2", "y"}
	if !equalStrings(got[0].Nodes, want0) {
		t.Fatalf("combination 0: want %v (declared order), got %v", want0, got[0].Nodes)
	}
	if !equalStrings(got[1].Nodes, want1) {
		t.Fatalf("combination 1: want %v (declared order), got %v", want1, got[1].Nodes)
	}
}

// TestPaths_CapBoundaryTruncatesDeterministically builds a diagram whose
// FULL, uncapped path count is comfortably over maxActivityPaths — via
// BREADTH (one start node with N > maxActivityPaths direct, single-hop
// branches to N distinct end nodes), not depth. It asserts activityPaths
// returns EXACTLY maxActivityPaths paths and that they are precisely the
// first maxActivityPaths in declared edge order.
func TestPaths_CapBoundaryTruncatesDeterministically(t *testing.T) {
	n := maxActivityPaths + 8
	nodes := []activityNode{{ID: "s", Kind: "start"}}
	edges := make([]activityEdge, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("e%d", i)
		nodes = append(nodes, activityNode{ID: id, Kind: "end"})
		edges = append(edges, activityEdge{From: "s", To: id})
	}
	a := activityDiagram{Nodes: nodes, Edges: edges}

	got := activityPaths(a)
	if len(got) != maxActivityPaths {
		t.Fatalf("want exactly the cap (%d) paths, got %d", maxActivityPaths, len(got))
	}
	for i, p := range got {
		want := []string{"s", fmt.Sprintf("e%d", i)}
		if !equalStrings(p.Nodes, want) {
			t.Fatalf("path %d: want %v (contiguous declared-order prefix), got %v", i, want, p.Nodes)
		}
	}
}

// TestPaths_AcceptEventIsItsOwnEntry mirrors TestPaths_EventNodeIsItsOwnEntry
// for the OTHER UML event kind, "acceptEvent".
func TestPaths_AcceptEventIsItsOwnEntry(t *testing.T) {
	a := activityDiagram{Nodes: []activityNode{{ID: "signal", Kind: "acceptEvent"}}}
	got := activityPaths(a)
	if len(got) != 1 {
		t.Fatalf("want 1 path, got %d: %+v", len(got), got)
	}
	if got[0].Entry.NodeID != "signal" || got[0].Entry.Kind != "acceptEvent" {
		t.Fatalf("want entry {signal acceptEvent}, got %+v", got[0].Entry)
	}
	if !equalStrings(got[0].Nodes, []string{"signal"}) {
		t.Fatalf("want nodes [signal], got %v", got[0].Nodes)
	}
}

// TestPaths_EventNodeWithIncomingEdgeIsStillARoot proves an event node is a
// root "wherever it sits" — even with an incoming edge from elsewhere in the
// diagram, the event node must STILL be enumerated as its own entry, in
// addition to whatever reaches it via that incoming edge.
func TestPaths_EventNodeWithIncomingEdgeIsStillARoot(t *testing.T) {
	a := activityDiagram{
		Nodes: []activityNode{
			{ID: "p", Kind: "action"}, {ID: "evt", Kind: "acceptEvent"},
			{ID: "a", Kind: "action"}, {ID: "e", Kind: "end"},
		},
		Edges: []activityEdge{
			{From: "p", To: "evt"}, // incoming edge into the event node
			{From: "evt", To: "a"}, {From: "a", To: "e"},
		},
	}
	got := activityPaths(a)
	if len(got) != 1 {
		t.Fatalf("want 1 path (evt is the only entry; p is not start/event so it roots nothing), got %d: %+v", len(got), got)
	}
	if got[0].Entry.NodeID != "evt" || got[0].Entry.Kind != "acceptEvent" {
		t.Fatalf("want entry {evt acceptEvent}, got %+v", got[0].Entry)
	}
	if !equalStrings(got[0].Nodes, []string{"evt", "a", "e"}) {
		t.Fatalf("want nodes [evt a e], got %v", got[0].Nodes)
	}
}

func equalStrings(a, b []string) bool {
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

// TestPaths_BudgetBoundsNestedForkDecision is the compute-bound regression: a
// nested fork×decision diagram — ONE fork of 8 branches, each branch its own
// 5-way decision — whose complete cross-product is 5^8 = 390,625
// combinations, ~760x the output cap. The walker must stop WORKING once the
// budget is spent rather than truncate the output after fully enumerating:
// the assertion is that the budget actually bound (exhausted), that the
// output still honors the cap, and that the path completed BEFORE the
// blowup — here the start-rooted entry, declared first — is returned rather
// than lost.
func TestPaths_BudgetBoundsNestedForkDecision(t *testing.T) {
	nodes := []activityNode{
		{ID: "s", Kind: "start"}, {ID: "sa", Kind: "action"}, {ID: "se", Kind: "end"},
		{ID: "tick", Kind: "timeEvent"}, {ID: "f", Kind: "fork"},
	}
	edges := []activityEdge{{From: "s", To: "sa"}, {From: "sa", To: "se"}, {From: "tick", To: "f"}}
	for b := 0; b < 8; b++ {
		d := fmt.Sprintf("d%d", b)
		nodes = append(nodes, activityNode{ID: d, Kind: "decision", Label: "branch?"})
		edges = append(edges, activityEdge{From: "f", To: d})
		for i := 0; i < 5; i++ {
			leaf := fmt.Sprintf("a%d-%d", b, i)
			nodes = append(nodes, activityNode{ID: leaf, Kind: "action", Label: leaf})
			edges = append(edges, activityEdge{From: d, To: leaf, Guard: "[g]"})
		}
	}

	got, exhausted := boundedActivityPaths(activityDiagram{Nodes: nodes, Edges: edges})
	if !exhausted {
		t.Fatalf("a 5^8 cross-product must exhaust the %d-step budget; it enumerated fully instead", maxWalkWork)
	}
	if len(got) > maxActivityPaths {
		t.Fatalf("want at most the cap (%d) paths, got %d", maxActivityPaths, len(got))
	}
	if len(got) == 0 || !equalStrings(got[0].Nodes, []string{"s", "sa", "se"}) {
		t.Fatalf("the entry completed BEFORE the blowup must survive the budget; got %+v", got)
	}
}

// TestPaths_PureForkOnlyBlowupYieldsEmptyResult pins binding port note 3
// (2026-07-31 rollout rulings): when the diagram's ONLY entry leads into a
// fork that never finishes folding, the pre-existing all-or-nothing-fork
// design (walkFork returns nil the moment any branch or fold comes back
// empty) means the walk degrades to exhausted=true with a length-0 result —
// NOT a partial answer. This is deliberate and must not be "fixed" by a
// future change: it is the same nested fork×decision shape as the test above
// with the benign start-rooted entry removed, so there is nothing else for
// the walk to return.
func TestPaths_PureForkOnlyBlowupYieldsEmptyResult(t *testing.T) {
	nodes := []activityNode{{ID: "tick", Kind: "timeEvent"}, {ID: "f", Kind: "fork"}}
	edges := []activityEdge{{From: "tick", To: "f"}}
	for b := 0; b < 8; b++ {
		d := fmt.Sprintf("d%d", b)
		nodes = append(nodes, activityNode{ID: d, Kind: "decision", Label: "branch?"})
		edges = append(edges, activityEdge{From: "f", To: d})
		for i := 0; i < 5; i++ {
			leaf := fmt.Sprintf("a%d-%d", b, i)
			nodes = append(nodes, activityNode{ID: leaf, Kind: "action", Label: leaf})
			edges = append(edges, activityEdge{From: d, To: leaf, Guard: "[g]"})
		}
	}

	got, exhausted := boundedActivityPaths(activityDiagram{Nodes: nodes, Edges: edges})
	if !exhausted {
		t.Fatalf("a 5^8 fork-only cross-product must exhaust the %d-step budget", maxWalkWork)
	}
	if len(got) != 0 {
		t.Fatalf("a pure-fork-only blowup with no other entry must yield the empty result (all-or-nothing fork, pre-existing design), got %d paths", len(got))
	}
}

// decisionChainDiagram builds the DECISION-shaped blowup: stages
// reconverging 2-way decisions in a row (d -> {a|b} -> m -> next d), so the
// true path count is 2^stages and every path runs the full length of the
// chain. This is the shape that dominates real committed data (decisions
// vastly outnumber forks), and the one a fork-only adversarial test misses
// entirely.
func decisionChainDiagram(stages int) activityDiagram {
	nodes := []activityNode{{ID: "s", Kind: "start"}}
	var edges []activityEdge
	prev := "s"
	for i := 0; i < stages; i++ {
		d, a, b, m := fmt.Sprintf("d%d", i), fmt.Sprintf("a%d", i), fmt.Sprintf("b%d", i), fmt.Sprintf("m%d", i)
		nodes = append(nodes,
			activityNode{ID: d, Kind: "decision", Label: "branch?"},
			activityNode{ID: a, Kind: "action", Label: a},
			activityNode{ID: b, Kind: "action", Label: b},
			activityNode{ID: m, Kind: "merge"})
		edges = append(edges,
			activityEdge{From: prev, To: d},
			activityEdge{From: d, To: a, Guard: "[y]"},
			activityEdge{From: d, To: b, Guard: "[n]"},
			activityEdge{From: a, To: m}, activityEdge{From: b, To: m})
		prev = m
	}
	nodes = append(nodes, activityNode{ID: "e", Kind: "end"})
	edges = append(edges, activityEdge{From: prev, To: "e"})
	return activityDiagram{Nodes: nodes, Edges: edges}
}

// allocatedBytes reports how many bytes fn allocated. Coarse by design — the
// numbers it guards differ by an order of magnitude, not by a few percent.
func allocatedBytes(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestPaths_BudgetBoundsDecisionChain is the DECISION-shaped counterpart to
// the fork test above (binding port note 1: charging assembly by LENGTH at
// every copy site — branchOverEdges' carry AND crossProduct's spend — is what
// keeps a 22-stage reconverging decision chain (4.2M true paths, no fork
// anywhere) from spiking memory the way a count-only charge did before the
// platform's fix-round-1). The ceiling asserted here is deliberately loose
// (an order of magnitude under the platform's pre-fix >1GB measurement,
// several times over its post-fix 63MB) so it catches the regression CLASS
// without pinning an allocator profile.
func TestPaths_BudgetBoundsDecisionChain(t *testing.T) {
	a := decisionChainDiagram(22)
	var got []activityPath
	var exhausted bool
	alloc := allocatedBytes(func() { got, exhausted = boundedActivityPaths(a) })

	if !exhausted {
		t.Fatalf("2^22 paths must exhaust the %d-step budget; it enumerated fully instead", maxWalkWork)
	}
	if len(got) > maxActivityPaths {
		t.Fatalf("want at most the cap (%d) paths, got %d", maxActivityPaths, len(got))
	}
	if len(got) == 0 {
		t.Fatalf("a decision blowup must still return the paths it completed (port note 1: carry), got none")
	}
	const ceiling = 256 << 20
	if alloc > ceiling {
		t.Fatalf("a decision-shaped blowup allocated %dMB, over the %dMB ceiling; the budget is bounding walk COUNT again rather than materialization",
			alloc>>20, uint64(ceiling)>>20)
	}
}

// TestPaths_BudgetTruncationMatchesTheCapPrefix pins what a budget-truncated
// answer costs the caller on a realistic shape: nothing. A 12-decision chain
// has 4,096 true paths (8x the output cap) and enumerating it fully costs
// 3.0M steps, so the budget truncates it — but the paths it returns are
// nevertheless bit-identical to the first maxActivityPaths of the unbounded
// enumeration.
func TestPaths_BudgetTruncationMatchesTheCapPrefix(t *testing.T) {
	a := decisionChainDiagram(12)

	unbounded := newWalker(a)
	unbounded.remaining = 1 << 40 // effectively unbudgeted
	var full []activityPath
	for _, entry := range diagramEntries(a) {
		for _, walk := range unbounded.walkFrom(entry.NodeID, map[int]bool{}) {
			full = append(full, activityPath{Entry: entry, Nodes: walk.seq})
		}
	}
	if len(full) != 4096 {
		t.Fatalf("fixture: want 4096 true paths, got %d", len(full))
	}

	got, exhausted := boundedActivityPaths(a)
	if !exhausted {
		t.Fatalf("fixture: a 3.0M-step enumeration must trip the %d-step budget", maxWalkWork)
	}
	if len(got) != maxActivityPaths {
		t.Fatalf("a budget-truncated decision chain must still fill the cap, got %d paths", len(got))
	}
	for i := range got {
		if !equalStrings(got[i].Nodes, full[i].Nodes) {
			t.Fatalf("path %d differs from the unbounded prefix:\n got %v\nwant %v", i, got[i].Nodes, full[i].Nodes)
		}
	}
}

// TestPaths_BudgetDoesNotBindOnOrdinaryDiagram is the other half of the
// bound: a diagram of the shape real authoring produces — a 3-way fork whose
// branches each carry their own 3-way decision, 27 complete combinations —
// must enumerate FULLY, with the budget untouched. The bound exists to stop
// pathological blowups, not to silently truncate ordinary designs.
func TestPaths_BudgetDoesNotBindOnOrdinaryDiagram(t *testing.T) {
	nodes := []activityNode{{ID: "s", Kind: "start"}, {ID: "f", Kind: "fork"}}
	edges := []activityEdge{{From: "s", To: "f"}}
	for b := 0; b < 3; b++ {
		d := fmt.Sprintf("d%d", b)
		nodes = append(nodes, activityNode{ID: d, Kind: "decision", Label: "branch?"})
		edges = append(edges, activityEdge{From: "f", To: d})
		for i := 0; i < 3; i++ {
			leaf := fmt.Sprintf("a%d-%d", b, i)
			nodes = append(nodes, activityNode{ID: leaf, Kind: "action", Label: leaf})
			edges = append(edges, activityEdge{From: d, To: leaf, Guard: "[g]"})
		}
	}

	got, exhausted := boundedActivityPaths(activityDiagram{Nodes: nodes, Edges: edges})
	if exhausted {
		t.Fatalf("an ordinary 27-combination diagram must not exhaust the budget")
	}
	if len(got) != 27 {
		t.Fatalf("want all 27 cross-producted combinations, got %d", len(got))
	}
}
