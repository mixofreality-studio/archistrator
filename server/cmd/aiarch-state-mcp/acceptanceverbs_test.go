package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-conformance"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// The acceptance plan's two verbs (FPC plan 4 Task 4.2): listAcceptanceScenarios reads the
// residual the plan must bind, and recordPhaseArtifact files the one acceptance plan under the
// key `acceptance` whatever mapKey the agent sent.

func TestRecordPhaseArtifact_AcceptancePlanIsKeyedAcceptance(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeConstruct, 0)
	s.ActivityID = "N-ATP"
	err := s.recordPhaseArtifact("whatever", map[string]any{
		"acceptancePlan": map[string]any{
			"principals":   map[string]any{"member": map[string]any{"subject": "m-1", "roles": []any{"member"}}},
			"bindings":     []any{},
			"browserActor": "member",
		},
	})
	if err != nil {
		t.Fatalf("recordPhaseArtifact: %v", err)
	}
	proj, _, _ := s.readProject()
	if proj.PhaseArtifacts == nil || proj.PhaseArtifacts.AcceptancePlan == nil {
		t.Fatalf("the acceptance plan is not at .phaseArtifacts.acceptancePlan: %+v", proj.PhaseArtifacts)
	}
	if got := proj.PhaseArtifacts.AcceptancePlan.BrowserActor; got == nil || *got != "member" {
		t.Errorf("browserActor = %v", got)
	}
	if len(proj.PhaseArtifacts.TestPlan) != 0 {
		t.Errorf("the plan belongs to no component, so no per-component map may hold it: %+v", proj.PhaseArtifacts.TestPlan)
	}
}

func TestRecordPhaseArtifact_RefusedAcceptancePlanNamesItsShape(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeConstruct, 0)
	s.ActivityID = "N-ATP"
	err := s.recordPhaseArtifact("acceptance", map[string]any{
		"acceptancePlan": map[string]any{
			"principals": map[string]any{},
			"bindings":   []any{map[string]any{"scenario": "x@y", "arrange": []any{}, "steps": []any{}, "hook": true}},
		},
	})
	if err == nil {
		t.Fatal("a binding carrying hook must be refused")
	}
	if !strings.Contains(err.Error(), "acceptancePlan's shape is") || !strings.Contains(err.Error(), "browserActor") {
		t.Errorf("the refusal must name the acceptance plan's shape: %v", err)
	}
	if s.wroteState {
		t.Error("a refused plan must leave the checkout untouched")
	}
}

// acceptanceListing is the JSON listAcceptanceScenarios returns.
type acceptanceListing struct {
	Scenarios []scenario.Scenario `json:"scenarios"`
	Residual  []struct {
		Scenario string `json:"scenario"`
		Reason   struct {
			Kind  string          `json:"kind"`
			Model json.RawMessage `json:"model"`
		} `json:"reason"`
	} `json:"residual"`
}

func TestListAcceptanceScenarios_ReturnsResidualWithReasons(t *testing.T) {
	raw, err := conformance.Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	p, ok, err := projectstate.DecodeProjectJSON(raw, projectstate.ProjectID("todomvc"))
	if err != nil || !ok {
		t.Fatalf("decode the todomvc fixture: ok=%v err=%v", ok, err)
	}
	s, _ := seedProject(t, p, jobModeConstruct, projectstate.KindCoreUseCases)
	out, err := s.listAcceptanceScenarios()
	if err != nil {
		t.Fatal(err)
	}
	var got acceptanceListing
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(got.Scenarios) == 0 || len(got.Residual) == 0 {
		t.Fatalf("the todomvc residual is not empty: %s", out)
	}
	listed := map[string]bool{}
	for _, sc := range got.Scenarios {
		listed[sc.ID] = true
	}
	for _, r := range got.Residual {
		if !listed[r.Scenario] {
			t.Errorf("residual %s has no scenario in the list", r.Scenario)
		}
	}
	// the todoApi variant is residual because the member's SPA shows the agent's write
	for _, r := range got.Residual {
		if r.Scenario == "work-the-list-P1@todoApi" && r.Reason.Kind == "laterOutcome" {
			if string(r.Reason.Model) != `{"outcomeNode":"out-team-member","afterStimulus":"n1a"}` {
				t.Errorf("the todoApi variant's reason model = %s", r.Reason.Model)
			}
			return
		}
	}
	t.Errorf("work-the-list-P1@todoApi must be residual as a laterOutcome: %s", out)
}

func TestListAcceptanceScenariosIsRegisteredReadOnlyInConstructMode(t *testing.T) {
	s := &Session{Mode: jobModeConstruct}
	found := false
	for _, v := range composedVerbs(s) {
		if v.name != "listAcceptanceScenarios" {
			continue
		}
		found = true
		want := []string{jobModeDraft, jobModeCritique, jobModeAnswer, jobModeConstruct}
		got := slices.Clone(v.modes)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("listAcceptanceScenarios must be a shared verb (all four job modes): got %v want %v", got, want)
		}
	}
	if !found {
		t.Fatal("listAcceptanceScenarios is not in the composed-verb registry")
	}

	res, err := connectInMemory(t, s).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "listAcceptanceScenarios" {
			continue
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("listAcceptanceScenarios must carry ReadOnlyHint: %+v", tool.Annotations)
		}
		return
	}
	t.Fatal("listAcceptanceScenarios is not served in construct mode")
}
