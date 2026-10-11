package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// newSessionFromFixture decodes a real, committed project.json (archistrator's own,
// by default) through the strict server codec and seeds a construct-mode Session
// over a copy of it in a temp state root — so the scenario verbs are exercised
// against the dog-food project, not a hand-built diagram.
func newSessionFromFixture(t *testing.T, path string) *Session {
	t.Helper()
	return newSessionFromEditedFixture(t, path, func(*projectstate.Project) {})
}

// newSessionFromEditedFixture is newSessionFromFixture with one typed edit applied
// to the decoded project before it is seeded — how a test stands up "the dog-food
// project, except slot N is not committed".
func newSessionFromEditedFixture(t *testing.T, path string, edit func(*projectstate.Project)) *Session {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	p, ok, err := projectstate.DecodeProjectJSON(raw, projectstate.ProjectID("archistrator"))
	if err != nil || !ok {
		t.Fatalf("decode fixture %s: ok=%v err=%v", path, ok, err)
	}
	edit(&p)
	s, _ := seedProject(t, p, jobModeConstruct, projectstate.KindCoreUseCases)
	return s
}

func TestListComponentScenariosReturnsProjectedSet(t *testing.T) {
	s := newSessionFromFixture(t, "../../../.aiarch/state/project.json")
	out, err := s.listComponentScenarios("deliveryManager")
	if err != nil {
		t.Fatal(err)
	}
	var got []scenario.Scenario
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("deliveryManager must receive stimuli in execute-a-project-activity")
	}
	for _, sc := range got {
		for _, st := range sc.Stimuli {
			if st.Input.To != "deliverymanager" {
				t.Fatalf("leaked stimulus %+v", st)
			}
		}
	}
}

func TestListComponentScenariosRequiresAComponent(t *testing.T) {
	s := newSessionFromFixture(t, "../../../.aiarch/state/project.json")
	if _, err := s.listComponentScenarios("  "); err == nil {
		t.Fatal("an empty component must be refused, not projected onto nothing")
	}
}

func TestListComponentScenariosIsRegisteredReadOnlyInConstructAndDesignModes(t *testing.T) {
	for _, mode := range []string{jobModeConstruct, jobModeDraft} {
		s := &Session{Mode: mode}
		var found *composedVerb
		for _, v := range composedVerbs(s) {
			if v.name == "listComponentScenarios" {
				v := v
				found = &v
			}
		}
		if found == nil {
			t.Fatalf("listComponentScenarios is not in the composed-verb registry")
		}
		if !containsStr(found.modes, mode) {
			t.Fatalf("listComponentScenarios must be available in mode %q (shared); modes=%v", mode, found.modes)
		}
	}
}

// TestListComponentScenariosIsEmptyWhenCoreUseCasesAreNotCommitted pins the verb to
// the same gate methodcheck applies: on a branch whose slot 4 is rejected (or
// drafting), the TP-* rules and testgen see NO scenarios, so the verb must bind
// none either — one canonical id set, not two.
func TestListComponentScenariosIsEmptyWhenCoreUseCasesAreNotCommitted(t *testing.T) {
	s := newSessionFromEditedFixture(t, "../../../.aiarch/state/project.json", func(p *projectstate.Project) {
		p.CoreUseCases.Status = projectstate.ReviewRejected
	})
	out, err := s.listComponentScenarios("deliveryManager")
	if err != nil {
		t.Fatal(err)
	}
	var got []scenario.Scenario
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("slot 4 rejected: methodcheck derives nothing, the verb returned %d scenarios", len(got))
	}
}
