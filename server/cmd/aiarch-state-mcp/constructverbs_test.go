package main

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// projectWithComponent is minimalProject with a System naming one component: the
// architecture id the construct job carries (kebab) and the contract key its service
// contract is recorded under.
func projectWithComponent(id, contractKey string) projectstate.Project {
	p := minimalProject()
	c := projectstate.Component{ID: id, Name: "Billing Engine", Kind: projectstate.CompEngine, Layer: projectstate.LayerEngine, Encapsulates: "Billing rules volatility"}
	if contractKey != "" {
		c.ContractKey = &contractKey
	}
	p.SystemDesign = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: &projectstate.System{Components: []projectstate.Component{c}}}
	return p
}

// TestRecordServiceContract_WritesAndValidates proves the detailed-design write lands
// in .serviceContracts under the AMBIENT component's contract key (not its architecture
// id) and passes the codec + methodcheck.
func TestRecordServiceContract_WritesAndValidates(t *testing.T) {
	s, _ := seedProject(t, projectWithComponent("billing-engine", "billingEngine"), jobModeConstruct, 0)
	s.ComponentID = "billing-engine"
	if err := s.recordServiceContract(map[string]any{
		"component": "billingEngine",
		"layer":     "Engine",
		"title":     "Billing Engine",
	}); err != nil {
		t.Fatalf("recordServiceContract: %v", err)
	}
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if proj.ServiceContracts["billingEngine"].Component != "billingEngine" {
		t.Fatalf("contract not written under the ambient component's contract key: %+v", proj.ServiceContracts)
	}
	if _, wrong := proj.ServiceContracts["billing-engine"]; wrong {
		t.Fatalf("contract written under the architecture id, not the contract key: %+v", proj.ServiceContracts)
	}
	if !s.wroteState {
		t.Fatal("wroteState must latch so publishDraft will not no-op")
	}
}

// TestRecordServiceContract_MissingAmbientComponent proves the verb refuses when the
// construct job carries no component (a non-component activity).
func TestRecordServiceContract_MissingAmbientComponent(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeConstruct, 0)
	err := s.recordServiceContract(map[string]any{"component": "x", "layer": "Engine"})
	if err == nil || !strings.Contains(err.Error(), "AIARCH_COMPONENT_ID") {
		t.Fatalf("expected missing-ambient-component error, got: %v", err)
	}
}

// TestRecordServiceContract_RefusesComponentWithoutContractKey proves a component the
// System gives no contract key (or does not name) has nothing recorded for it.
func TestRecordServiceContract_RefusesComponentWithoutContractKey(t *testing.T) {
	for name, p := range map[string]projectstate.Project{
		"no contract key":   projectWithComponent("billing-engine", ""),
		"unknown component": projectWithComponent("other-engine", "otherEngine"),
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := seedProject(t, p, jobModeConstruct, 0)
			s.ComponentID = "billing-engine"
			err := s.recordServiceContract(map[string]any{"component": "billingEngine", "layer": "Engine", "title": "Billing Engine"})
			if err == nil || !strings.Contains(err.Error(), "billing-engine") {
				t.Fatalf("expected a refusal naming the component, got: %v", err)
			}
			if s.wroteState {
				t.Fatal("a refused contract must not latch wroteState or write anything")
			}
		})
	}
}

// TestRecordServiceContract_RejectsMalformed proves a type-mismatched contract is
// rejected by the strict codec with nothing written.
func TestRecordServiceContract_RejectsMalformed(t *testing.T) {
	s, _ := seedProject(t, projectWithComponent("billing-engine", "billingEngine"), jobModeConstruct, 0)
	s.ComponentID = "billing-engine"
	// component must be a string; a number fails the strict decode.
	err := s.recordServiceContract(map[string]any{"component": 123})
	if err == nil || !strings.Contains(err.Error(), "does not conform") {
		t.Fatalf("expected a schema-conformance rejection, got: %v", err)
	}
	if s.wroteState {
		t.Fatal("a rejected contract must not latch wroteState or write anything")
	}
}

// TestRecordPhaseArtifact_Writes proves a phase-artifact payload routes into
// .phaseArtifacts under the mapKey via the shared server routing.
func TestRecordPhaseArtifact_Writes(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeConstruct, 0)
	s.ActivityID = "C-BE"
	if err := s.recordPhaseArtifact("billing-engine", map[string]any{
		"SRS": map[string]any{"component": "billing-engine", "content": "the requirements"},
	}); err != nil {
		t.Fatalf("recordPhaseArtifact: %v", err)
	}
	proj, _, _ := s.readProject()
	if proj.PhaseArtifacts == nil || proj.PhaseArtifacts.SRS["billing-engine"].Content != "the requirements" {
		t.Fatalf("SRS not written under mapKey: %+v", proj.PhaseArtifacts)
	}
}

// TestRecordTestingState_Writes proves a testing-state payload routes into
// .testingState (project-level; mapKey unused).
func TestRecordTestingState_Writes(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeConstruct, 0)
	s.ActivityID = "N-QA"
	if err := s.recordTestingState(map[string]any{"QualityAuditReport": "audit passed"}); err != nil {
		t.Fatalf("recordTestingState: %v", err)
	}
	proj, _, _ := s.readProject()
	if proj.TestingState == nil || proj.TestingState.QualityAuditReport != "audit passed" {
		t.Fatalf("testing state not written: %+v", proj.TestingState)
	}
}

// TestRecordTestingState_RefusesTheRunHalf: a CheckRun is the venue's evidence, written
// by record-check-run. The testing gate judges "the venue's evidence, not an agent's
// claim", and an agent-facing verb that accepted a CheckRun payload could record a
// fabricated all-pass run stamped with the judged attempt id and pass the gate with it.
// The refusal names the venue's subcommand, and NOTHING is written.
func TestRecordTestingState_RefusesTheRunHalf(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeConstruct, 0)
	s.ActivityID = "C-BE"
	err := s.recordTestingState(map[string]any{"CheckRun": map[string]any{
		"id": "fab-1", "activity": "C-BE", "task": "integration", "revision": "C-BE:integration:1", "specHash": "",
		"artifact": "check-results-C-BE-fab-1", "results": []any{map[string]any{"checkId": "scenario:billingEngine:s1", "kind": "scenario", "status": "pass", "durationMs": 1}},
	}})
	if err == nil || !strings.Contains(err.Error(), "record-check-run") {
		t.Fatalf("the run half must be refused naming the venue's record-check-run, got: %v", err)
	}
	proj, _, _ := s.readProject()
	if proj.TestingState != nil && len(proj.TestingState.CheckRuns) != 0 {
		t.Fatalf("a refused run must not be written: %+v", proj.TestingState)
	}
	if s.wroteState {
		t.Fatal("a refused run must leave the checkout untouched")
	}
}

// TestRecordPhaseArtifact_MissingAmbientActivity proves the phase/testing verbs refuse
// without the ambient activity.
func TestRecordPhaseArtifact_MissingAmbientActivity(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeConstruct, 0)
	if err := s.recordPhaseArtifact("k", map[string]any{"SRS": map[string]any{"component": "c", "content": "x"}}); err == nil ||
		!strings.Contains(err.Error(), "AIARCH_ACTIVITY_ID") {
		t.Fatalf("expected missing-ambient-activity error, got: %v", err)
	}
	if err := s.recordTestingState(map[string]any{"QualityAuditReport": "x"}); err == nil ||
		!strings.Contains(err.Error(), "AIARCH_ACTIVITY_ID") {
		t.Fatalf("expected missing-ambient-activity error, got: %v", err)
	}
}

// TestConstructModeToolSet proves the construct fallback set: the three record verbs +
// the ambient-independent reads + publishDraft, and NOT the design verbs (putDraftModel,
// setCritiqueVerdict) nor the ambient-Kind reads (getDraftSlot, getReviewThread, getCritique).
func TestConstructModeToolSet(t *testing.T) {
	s := &Session{Mode: jobModeConstruct}
	names := map[string]bool{}
	for _, v := range composedVerbs(s) {
		if containsStr(v.modes, jobModeConstruct) {
			names[v.name] = true
		}
	}
	for _, want := range []string{"recordServiceContract", "recordPhaseArtifact", "recordTestingState", "getCommittedSlot", "publishDraft"} {
		if !names[want] {
			t.Errorf("construct mode must expose %q", want)
		}
	}
	for _, absent := range []string{"putDraftModel", "setCritiqueVerdict", "getDraftSlot", "getReviewThread", "getCritique"} {
		if names[absent] {
			t.Errorf("construct mode must NOT expose %q", absent)
		}
	}
	_ = projectstate.Project{}
}
