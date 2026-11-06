package main

// conformance_test.go proves the platform's embedded conformance fixtures (the todomvc project
// harvested from RUN-021, and the greenfield one) are not a second, hand-kept copy of what this
// app's recorder accepts. Each fixture is stripped to the state a project is born with, then every
// contract and phase artifact it holds is recorded again through the same verbs a construction
// agent calls. If the recorder and the fixture drift, one of them is wrong, and this test says which.

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/conformance"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// conformanceFixtureDoc is the part of a fixture this test replays: the contracts and the
// phase artifacts the recorder is asked to accept again.
type conformanceFixtureDoc struct {
	ServiceContracts map[string]map[string]any `json:"serviceContracts"`
	PhaseArtifacts   struct {
		TestPlan map[string]map[string]any `json:"testPlan"`
		UIDesign map[string]map[string]any `json:"uiDesign"`
	} `json:"phaseArtifacts"`
}

// conformanceBirth is the fixture with everything the verbs record taken out: the state a
// project has once its System is committed and nothing has been constructed.
func conformanceBirth(t *testing.T, raw []byte) projectstate.Project {
	t.Helper()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	delete(doc, "serviceContracts")
	delete(doc, "phaseArtifacts")
	stripped, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(stripped, anyProject)
	if err != nil {
		t.Fatalf("the stripped fixture does not decode: %v", err)
	}
	return proj
}

// conformanceComponentOf is the System component whose contract is filed under key.
func conformanceComponentOf(t *testing.T, p projectstate.Project, key string) projectstate.Component {
	t.Helper()
	sys, ok := p.SystemDesign.Model.(*projectstate.System)
	if !ok || sys == nil {
		t.Fatal("the fixture carries no System model")
	}
	for _, c := range sys.Components {
		if k, has := projectstate.ContractKeyFor(c); has && k == key {
			return c
		}
	}
	t.Fatalf("no System component files its contract under %q", key)
	return projectstate.Component{}
}

// conformanceRecordOrder lists the contract keys in the order a construction agent records them:
// a Manager's constructor takes the contracts it depends on, so a Manager follows every
// Utility, ResourceAccess and Engine. Within a kind the order is the keys', for a stable run.
func conformanceRecordOrder(t *testing.T, p projectstate.Project, contracts map[string]map[string]any) []string {
	t.Helper()
	rank := map[projectstate.ComponentKind]int{
		projectstate.CompUtility: 0, projectstate.CompResourceAccess: 1, projectstate.CompEngine: 2,
		projectstate.CompManager: 3, projectstate.CompClient: 4,
	}
	keys := slices.Sorted(maps.Keys(contracts))
	slices.SortStableFunc(keys, func(a, b string) int {
		return rank[conformanceComponentOf(t, p, a).Kind] - rank[conformanceComponentOf(t, p, b).Kind]
	})
	return keys
}

// admitConformance is the Tier 0 admission the write path applies (projectstate's unexported
// admit), restated from its three loaders over the bytes on disk.
func admitConformance(t *testing.T, s *Session) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(s.StateRoot, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projectmodel.Load(raw); err != nil {
		t.Fatalf("projectmodel.Load refuses the re-recorded project:\n%v", err)
	}
	mp, ok, err := methodcheck.DecodeProject(raw)
	if err != nil {
		t.Fatalf("methodcheck.DecodeProject: %v", err)
	}
	if !ok {
		t.Fatal("methodcheck.DecodeProject found no project in the re-recorded state")
	}
	if _, err := methodcheck.DeriveScenarios(mp); err != nil {
		t.Fatalf("methodcheck.DeriveScenarios: %v", err)
	}
}

func TestConformanceFixture_RecordsThroughVerbs(t *testing.T) {
	names := conformance.FixtureNames()
	if len(names) == 0 {
		t.Fatal("the platform embeds no conformance fixtures")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			raw, err := conformance.Fixture(name)
			if err != nil {
				t.Fatal(err)
			}
			var fx conformanceFixtureDoc
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			birth := conformanceBirth(t, raw)
			s, _ := seedProject(t, birth, jobModeConstruct, 0)

			// A Client authors no contract: a machine client's is derived when its Manager's is
			// recorded, a browser surface's when its UI design is. Those are asserted, not recorded.
			var derived []string
			for _, key := range conformanceRecordOrder(t, birth, fx.ServiceContracts) {
				c := conformanceComponentOf(t, birth, key)
				if c.Kind == projectstate.CompClient {
					derived = append(derived, key)
					continue
				}
				s.ComponentID, s.ActivityID = c.ID, "C-"+c.ID
				if err := s.recordServiceContract(fx.ServiceContracts[key]); err != nil {
					t.Fatalf("recordServiceContract(%s): %v", key, err)
				}
			}

			for _, mapKey := range slices.Sorted(maps.Keys(fx.PhaseArtifacts.UIDesign)) {
				s.ComponentID, s.ActivityID = mapKey, "C-"+mapKey
				if err := s.recordPhaseArtifact(mapKey, map[string]any{"uiDesign": fx.PhaseArtifacts.UIDesign[mapKey]}); err != nil {
					t.Fatalf("recordPhaseArtifact(uiDesign %s): %v", mapKey, err)
				}
			}

			for _, key := range slices.Sorted(maps.Keys(fx.PhaseArtifacts.TestPlan)) {
				c := conformanceComponentOf(t, birth, key)
				s.ComponentID, s.ActivityID = c.ID, "C-"+c.ID
				if err := s.recordPhaseArtifact(key, map[string]any{"testPlan": fx.PhaseArtifacts.TestPlan[key]}); err != nil {
					t.Fatalf("recordPhaseArtifact(testPlan %s): %v", key, err)
				}
			}

			proj, _, err := s.readProject()
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range derived {
				if _, ok := proj.ServiceContracts[key]; !ok {
					t.Errorf("the client contract %q was not derived by the verbs", key)
				}
			}
			admitConformance(t, s)
		})
	}
}
