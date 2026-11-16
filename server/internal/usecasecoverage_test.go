package internal_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	review "github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// The PM coverage view (FPC Task 2.14): what the product manager reads at the core-use-cases
// critique to judge that every path the customer's raw use case promises reaches a surface and
// ends in an outcome the actor sees. It is derived on read from scenario.DeriveReport; nothing
// of it is stored. These tests live here, not beside the Manager, because a component package
// holds only its generated scenarios file and its hooks file (TestFileLayout).

// rewordBeforeAmendments is the RUN-021 reword-a-todo diagram as the core-use-cases step left it:
// the actor's abandon branch (n3) is a note with no call into any Client, so its path carries an
// actor's decision and no stimulus reaches a Client (SCN-ACTOR-PATH-CLIENT). Only the accepted
// path ends in a sendSignal (n11) the actor sees.
func rewordBeforeAmendments() scenario.Input {
	node := func(id, kind, label, lane string) scenario.Node {
		return scenario.Node{ID: id, Kind: kind, Label: label, RoleName: lane}
	}
	n1 := node("n1", "start", "Activate a todo's title for rewording", "Team Member")
	n1.LinkedActorID = "team-member"
	n2 := node("n2", "decision", "Reword committed or abandoned?", "Team Member")
	n2.DecidedBy = "team-member"
	return scenario.Input{
		Actors: map[string]bool{"team-member": true},
		Diagrams: []scenario.Diagram{{
			UseCaseID: "reword-a-todo", Title: "Reword a todo",
			Nodes: []scenario.Node{
				n1, n2,
				node("n3", "note", "Abandoned (e.g. Escape): the todo keeps its original title", "Team Member"),
				node("n4", "action", "Judge whether the new wording is an acceptable title", "List Keeping"),
				node("n5", "decision", "Title acceptable?", "List Keeping"),
				node("n6", "action", "Retitle the todo in place", "List Keeping"),
				node("n7", "action", "Remove the todo under the blank reword rule", "List Keeping"),
				node("n11", "sendSignal", "Todo shows the new wording", "Team Member"),
				node("n8", "merge", "", "List Keeping"),
				node("n9", "merge", "", "List Keeping"),
				node("n10", "end", "", "List Keeping"),
			},
			Edges: []scenario.Edge{
				{From: "n1", To: "n2"}, {From: "n2", To: "n3", Guard: "[abandoned]"}, {From: "n2", To: "n4", Guard: "[else]"},
				{From: "n4", To: "n5"}, {From: "n5", To: "n6", Guard: "[acceptable]"}, {From: "n5", To: "n7", Guard: "[else]"},
				{From: "n6", To: "n11"}, {From: "n11", To: "n8"}, {From: "n7", To: "n8"},
				{From: "n8", To: "n9"}, {From: "n3", To: "n9"}, {From: "n9", To: "n10"},
			},
		}},
		Views: []scenario.View{{UseCaseID: "reword-a-todo", Steps: []scenario.Step{{NodeID: "n4", Calls: []scenario.Call{
			{From: "team-member", To: "todo-web-app", Mode: "sync", Label: "RewordTodo — commits new wording for a todo"},
			{From: "todo-web-app", To: "list-manager", Mode: "sync", Label: "rewordTodo(todoId, wording) → todo | removed"},
		}}}}},
		Contracts: map[string]scenario.Contract{
			"todo-web-app": {Component: "todo-web-app", Client: true, Ops: []scenario.Op{{Name: "RewordTodo", Params: []string{"todoId", "wording"}}}},
			"list-manager": {Component: "list-manager", Ops: []scenario.Op{{Name: "rewordTodo", Params: []string{"todoId", "wording"}}}},
		},
	}
}

func pathWithGuards(t *testing.T, uc projectstate.UseCaseCoverage, guards ...string) projectstate.PathCoverage {
	t.Helper()
	for _, p := range uc.Paths {
		if slices.Equal(p.Guards, guards) {
			return p
		}
	}
	t.Fatalf("no path of %s with guards %v in %+v", uc.UseCase, guards, uc.Paths)
	return projectstate.PathCoverage{}
}

func TestCoverageView_ListsDoorAndOutcomesPerPath(t *testing.T) {
	clients := map[string]projectstate.CoverageClient{"todowebapp": {ID: "todo-web-app", Surface: "web"}}
	got, err := projectstate.UseCaseCoverageFor(rewordBeforeAmendments(), clients)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].UseCase != "reword-a-todo" || len(got[0].Paths) != 3 {
		t.Fatalf("coverage %+v, want one use case with the three derived paths", got)
	}
	uc := got[0]

	// The abandon path: an actor's decision, no stimulus into any Client, so no door and the
	// derivation's own defect code on the row. Its outcome list is empty, and the table says so.
	abandoned := pathWithGuards(t, uc, "[abandoned]")
	if abandoned.Client != "" || abandoned.Door != "" || len(abandoned.OutcomeNodes) != 0 {
		t.Errorf("abandoned path %+v must have no door and no outcome", abandoned)
	}
	// The actor's decision at n2 is also an input no contracted component receives, so the
	// derivation reports both defects of the path, in node then code order.
	if want := []string{scenario.ProblemActorPathClient, scenario.ProblemUnaddressed}; !slices.Equal(abandoned.Problems, want) {
		t.Errorf("abandoned path problems %v, want %v", abandoned.Problems, want)
	}

	// The accepted path: the actor asks through the web client and is told the new wording; the
	// label is the diagram's, verbatim.
	accepted := pathWithGuards(t, uc, "[else]", "[acceptable]")
	if accepted.Client != "todo-web-app" || accepted.Door != "web" {
		t.Errorf("accepted path door %q/%q, want todo-web-app/web", accepted.Client, accepted.Door)
	}
	if !slices.Equal(accepted.OutcomeNodes, []string{"Todo shows the new wording"}) {
		t.Errorf("accepted path outcomes %v", accepted.OutcomeNodes)
	}
	// The accepted path reaches the Client, but the actor's decision at n2 has no step of its own
	// (CC-DECIDED-BY-ACTOR-STEP): the derivation still reports it, so the PM sees it on the row.
	if want := []string{scenario.ProblemUnaddressed}; !slices.Equal(accepted.Problems, want) {
		t.Errorf("accepted path problems %v, want %v", accepted.Problems, want)
	}

	// The refused path reaches the same door and ends in nothing the actor sees: the table must
	// show an empty outcome column, which is what the PM sends back.
	refused := pathWithGuards(t, uc, "[else]", "[else]")
	if refused.Door != "web" || len(refused.OutcomeNodes) != 0 {
		t.Errorf("refused path %+v", refused)
	}
}

// The view the Manager serves is the same derivation over the committed design, joined to each
// path's scenario: nothing stored, every row a scenario DeriveReport reports.
func TestCoverageView_ManagerServesTheDerivationOfTheCommittedDesign(t *testing.T) {
	proj := *ownProject(t)
	in, err := projectstate.ScenarioInputOf(&proj)
	if err != nil {
		t.Fatal(err)
	}
	report, err := scenario.DeriveReport(in)
	if err != nil {
		t.Fatal(err)
	}
	pid := "archistrator"
	v, err := viewManager(proj, nil).QueryProjectView(viewCtx(), delivery.ProjectViewQuery{Kind: delivery.ProjectViewUseCaseCoverage, ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != delivery.ProjectViewUseCaseCoverage || v.UseCaseCoverage == nil || len(v.UseCaseCoverage.UseCases) == 0 {
		t.Fatalf("view %+v carries no coverage", v)
	}
	scenarios := map[string]scenario.Scenario{}
	for _, s := range report.Scenarios {
		scenarios[s.ID] = s
	}
	paths := 0
	for _, uc := range v.UseCaseCoverage.UseCases {
		for _, p := range uc.Paths {
			paths++
			s, ok := scenarios[p.Scenario]
			if !ok || s.UseCase != uc.UseCase {
				t.Fatalf("row %s/%s is no derived scenario", uc.UseCase, p.Scenario)
			}
			var want []string
			for _, pr := range report.Problems {
				if pr.Scenario == p.Scenario {
					want = append(want, pr.Code)
				}
			}
			if !slices.Equal(p.Problems, want) {
				t.Errorf("%s problems %v, want the report's %v", p.Scenario, p.Problems, want)
			}
			if p.Guards == nil || p.OutcomeNodes == nil || p.Problems == nil {
				t.Errorf("%s: a nil list reaches the wire: %+v", p.Scenario, p)
			}
		}
	}
	if paths != len(report.Scenarios) {
		t.Fatalf("view lists %d paths, DeriveReport has %d scenarios", paths, len(report.Scenarios))
	}
}

func TestCoverageView_NeedsAProject(t *testing.T) {
	if _, err := viewManager(*ownProject(t), nil).QueryProjectView(viewCtx(),
		delivery.ProjectViewQuery{Kind: delivery.ProjectViewUseCaseCoverage}); err == nil {
		t.Fatal("a useCaseCoverage query with no projectId answered")
	}
}

// The floor under the PM's critique at the core use cases: a send-back holds the gate for a human
// under EVERY preset, "vibes" included, where a clean design draft would otherwise auto-approve.
func TestRequiresHuman_PMSendBackHoldsUnderVibes(t *testing.T) {
	eng := review.NewReviewEngine()
	propose := func(preset, phase string, pmSendBack bool) review.ReviewSet {
		t.Helper()
		set, err := eng.ProposeReviews(fweng.Context{}, review.ReviewChange{ActivityID: "requirements"},
			review.ActivityTypeRequirements, phase, "", review.ReviewPolicy{Preset: preset}, pmSendBack, nil)
		if err != nil {
			t.Fatal(err)
		}
		return set
	}
	for _, preset := range []string{"vibes", "checkpoints", "full", ""} {
		set := propose(preset, "coreUseCases", true)
		if !set.RequiresHuman || !strings.Contains(set.Reason, "pmCoverageSendBack") {
			t.Errorf("preset %q: the PM send-back did not hold the gate: %+v", preset, set)
		}
	}
	if set := propose("vibes", "coreUseCases", false); set.RequiresHuman {
		t.Errorf("vibes with no send-back must still auto-approve a clean draft: %+v", set)
	}
	// The floor is the core use cases' alone: the mission's critique has no such row.
	if set := propose("vibes", "mission", true); set.RequiresHuman {
		t.Errorf("the floor must not leak to the mission gate: %+v", set)
	}
}

// An approve over the PM's send-back is an override, and an override is on the record with a
// reason: the hold is a blocking system comment the reviewer resolves, and the approve itself
// must then carry the reason it is overriding for.
func TestApproveOverPMSendBack_NeedsAResolvedHoldAndARecordedReason(t *testing.T) {
	hold := projectstate.PMCoverageHoldComment("path reword-a-todo-P1 reaches no Client")
	hold.ID, hold.Status = "r1c1", projectstate.ReviewCommentOpen
	if !projectstate.PMCoverageHoldIn([]projectstate.ReviewComment{hold}) {
		t.Fatal("the hold comment is not recognised in its own thread")
	}
	if !projectstate.ReviewCommentBlocksApprove(hold) {
		t.Fatal("the hold must block an approve while it is open (the reviewer resolves it first)")
	}
	other := projectstate.ReviewComment{ID: "r1c2", Status: projectstate.ReviewCommentOpen, AuthorRole: "architect", Anchor: "$.coreUseCases"}
	if projectstate.PMCoverageHoldIn([]projectstate.ReviewComment{other}) {
		t.Fatal("an ordinary comment at the same anchor is not the hold")
	}

	resolved := hold
	resolved.Status = projectstate.ReviewCommentResolved
	thread := []projectstate.ReviewComment{resolved}
	for _, notes := range []string{"", "   \n"} {
		if why := projectstate.ApproveOverPMCoverageRefusal(thread, notes); why == "" || !strings.Contains(why, "reason") {
			t.Errorf("an approve over the PM's send-back with notes %q was not refused with a reason demand: %q", notes, why)
		}
	}
	if why := projectstate.ApproveOverPMCoverageRefusal(thread, "the abandon path is covered by the web client's own Escape test"); why != "" {
		t.Errorf("an approve with a reason was refused: %q", why)
	}
	// A thread with no hold is nobody's override: an ordinary approve needs no reason.
	if why := projectstate.ApproveOverPMCoverageRefusal(nil, ""); why != "" {
		t.Errorf("an approve with no PM hold was refused: %q", why)
	}
}

var _ = fwmanager.Context{}
