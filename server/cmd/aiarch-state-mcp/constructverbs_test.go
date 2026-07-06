package main

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// TestRecordServiceContract_WritesAndValidates proves the detailed-design write lands
// in .serviceContracts under the AMBIENT component and passes the codec + methodcheck.
func TestRecordServiceContract_WritesAndValidates(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeConstruct, 0)
	s.ComponentID = "billingEngine"
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
		t.Fatalf("contract not written under the ambient component: %+v", proj.ServiceContracts)
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

// TestRecordServiceContract_RejectsMalformed proves a type-mismatched contract is
// rejected by the strict codec with nothing written.
func TestRecordServiceContract_RejectsMalformed(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeConstruct, 0)
	s.ComponentID = "billingEngine"
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
// setCritiqueVerdict) nor the ambient-Kind reads (getDraftSlot, getReviewThread).
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
	for _, absent := range []string{"putDraftModel", "setCritiqueVerdict", "getDraftSlot", "getReviewThread"} {
		if names[absent] {
			t.Errorf("construct mode must NOT expose %q", absent)
		}
	}
	_ = projectstate.Project{}
}
