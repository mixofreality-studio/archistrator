package main

// run013_test.go replays RUN-013's wall. testdata/fresh/run013.project.json is its
// activity/C-message-bus state with the bus's contract taken out, and
// run013.contract.message-bus.json is that contract exactly as its detailed-design agent passed it
// to the verb: the operations under interface.ops, not interface.operations. A lenient decode
// dropped the key, the verb wrote a Utility contract with no operations as passing, and
// construction got an empty MessageBus interface it could not build against.
//
// testdata/fresh/lenient-records.json holds phase-artifact payloads archived agents passed
// verbatim, whose text or bindings sat under a key the record does not declare and was dropped.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	run013Component = "message-bus"
	run013Activity  = "C-message-bus"
)

func run013Raw(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run013.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func run013Session(t *testing.T) *Session {
	t.Helper()
	s := run010Session(t, run013Raw(t), run013Component)
	s.ActivityID = run013Activity
	return s
}

// run013Contract is the bus's contract as RUN-013's agent passed it.
func run013Contract(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run013.contract.message-bus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	iface, _ := c["interface"].(map[string]any)
	if _, ok := iface["ops"]; !ok {
		t.Fatal("the fixture lost RUN-013's interface.ops")
	}
	return c
}

func TestRun013ContractWithOpsKeyIsRefused(t *testing.T) {
	t.Run("the verbatim contract is refused naming the key", func(t *testing.T) {
		s := run013Session(t)
		err := s.recordServiceContract(run013Contract(t))
		if err == nil || !strings.Contains(err.Error(), `"ops"`) {
			t.Fatalf("interface.ops must be refused naming the key, got: %v", err)
		}
		if s.wroteState {
			t.Fatal("a refused contract must write nothing")
		}
	})
	t.Run("the corrected contract is accepted with both operations", func(t *testing.T) {
		s := run013Session(t)
		c := run013Contract(t)
		iface := c["interface"].(map[string]any)
		iface["operations"] = iface["ops"]
		delete(iface, "ops")
		if err := s.recordServiceContract(c); err != nil {
			t.Fatalf("the corrected contract: %v", err)
		}
		proj, _, err := s.readProject()
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, op := range proj.ServiceContracts["messageBus"].Interface.Operations {
			names = append(names, op.Name)
		}
		if strings.Join(names, ",") != "Publish,Subscribe" {
			t.Fatalf("stored operations = %v, want Publish, Subscribe", names)
		}
	})
	t.Run("a contract with no operations is refused", func(t *testing.T) {
		s := run013Session(t)
		c := run013Contract(t)
		c["interface"] = map[string]any{"name": "MessageBus"}
		err := s.recordServiceContract(c)
		if err == nil || !strings.Contains(err.Error(), "interface.operations") || !strings.Contains(err.Error(), run013Component) {
			t.Fatalf("a contract with no operations must be refused, got: %v", err)
		}
		if s.wroteState {
			t.Fatal("a refused contract must write nothing")
		}
	})
}

// lenientRecord is one archived agent's recordPhaseArtifact call.
type lenientRecord struct {
	Run     string         `json:"run"`
	Trace   string         `json:"trace"`
	MapKey  string         `json:"mapKey"`
	Payload map[string]any `json:"payload"`
}

func lenientRecords(t *testing.T) map[string]lenientRecord {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "lenient-records.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recs map[string]lenientRecord
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatal(err)
	}
	return recs
}

// Each archived payload is refused naming the key it put its text or bindings under, and nothing
// is written. RUN-011 and RUN-012 committed seven integration notes with empty content this way.
func TestRecordPhaseArtifactRefusesArchivedLenientPayloads(t *testing.T) {
	recs := lenientRecords(t)
	for name, wantKey := range map[string]string{
		"run012IntegrationNoteSummary":  "summary",
		"run012IntegrationNoteMarkdown": "markdown",
		"run012TestPlanScenarios":       "scenarios",
		"run008SrsSections":             "callers",
	} {
		t.Run(name, func(t *testing.T) {
			rec, ok := recs[name]
			if !ok {
				t.Fatalf("the fixture lost %s", name)
			}
			s := run013Session(t)
			err := s.recordPhaseArtifact(rec.MapKey, rec.Payload)
			if err == nil || !strings.Contains(err.Error(), `"`+wantKey+`"`) {
				t.Fatalf("%s (%s) must be refused naming %q, got: %v", name, rec.Trace, wantKey, err)
			}
			if _, plan := rec.Payload["testPlan"]; plan && !strings.Contains(err.Error(), testPlanShape) {
				t.Fatalf("a refused testPlan must name its shape, got: %v", err)
			}
			if s.wroteState {
				t.Fatal("a refused record must write nothing")
			}
		})
	}
}

// A content-bearing record with no content is refused naming where its text goes; the corrected
// {component, content} forms are accepted.
func TestRecordPhaseArtifactRefusesBlankContent(t *testing.T) {
	for _, field := range []string{"SRS", "IntegrationNote"} {
		t.Run(field, func(t *testing.T) {
			s := run013Session(t)
			err := s.recordPhaseArtifact(run013Component, map[string]any{field: map[string]any{"component": run013Component, "content": "  \n"}})
			if err == nil || !strings.Contains(err.Error(), `goes in "content"`) || !strings.Contains(err.Error(), field) {
				t.Fatalf("a blank %s must be refused naming content, got: %v", field, err)
			}
			if s.wroteState {
				t.Fatal("a refused record must write nothing")
			}
			if err := s.recordPhaseArtifact(run013Component, map[string]any{field: map[string]any{"component": run013Component, "content": "# Message bus\n\nThe note."}}); err != nil {
				t.Fatalf("the corrected %s: %v", field, err)
			}
		})
	}
	s := run013Session(t)
	if err := s.recordPhaseArtifact(run013Component, map[string]any{"IntegrationNote": map[string]any{"component": run013Component, "content": "# Wired"}}); err != nil {
		t.Fatal(err)
	}
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	if got := proj.PhaseArtifacts.IntegrationNote[run013Component].Content; got != "# Wired" {
		t.Fatalf("stored note content = %q", got)
	}
}

// The venue's record-test-plan gate decodes the record as strictly as the verb.
func TestGateTestPlanRecordRefusesAnUnknownField(t *testing.T) {
	rec, err := json.Marshal(lenientRecords(t)["run012TestPlanScenarios"].Payload["testPlan"])
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gateTestPlanRecord(run013Raw(t), "todo-list-access", rec); err == nil || !strings.Contains(err.Error(), `"scenarios"`) || !strings.Contains(err.Error(), testPlanShape) {
		t.Fatalf("a plan under scenarios must be refused naming the key, got: %v", err)
	}
}
