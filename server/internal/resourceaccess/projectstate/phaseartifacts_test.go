package projectstate

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPhaseArtifacts_RoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	pa := PhaseArtifacts{
		SRS: map[string]SRSRecord{
			"projectExport": {Component: "projectExport", Content: "# SRS\n1. export project state", AuthoredAt: &now},
		},
		TestPlan: map[string]TestPlanRecord{
			"projectExport": {Component: "projectExport", Content: "## Test Plan\n- verify export", AuthoredAt: &now},
		},
		IntegrationNote: map[string]IntegrationNoteRecord{
			"projectExport": {Component: "projectExport", Content: "integrated OK", AuthoredAt: &now},
		},
	}
	b, err := json.Marshal(pa)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got PhaseArtifacts
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SRS["projectExport"].Content != pa.SRS["projectExport"].Content {
		t.Errorf("SRS content mismatch after round-trip")
	}
	if got.TestPlan["projectExport"].Component != "projectExport" {
		t.Errorf("TestPlan component mismatch after round-trip")
	}
}

func TestTestingState_RoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	ts := TestingState{
		QualityGates: []QualityGate{
			{ActivityType: "C-PE", Phase: "construction", When: "before", Mode: "escalate"},
		},
		Defects: []DefectRecord{
			{ID: "D-001", Title: "null pointer in export", Severity: "high", FiledAt: &now},
		},
		TestRuns: []TestRun{
			{ID: "TR-001", StartedAt: &now, Passed: 42, Failed: 0},
		},
	}
	b, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TestingState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.QualityGates) != 1 || got.QualityGates[0].Mode != "escalate" {
		t.Errorf("QualityGates mismatch after round-trip")
	}
	if len(got.Defects) != 1 || got.Defects[0].Severity != "high" {
		t.Errorf("Defects mismatch after round-trip")
	}
	if len(got.TestRuns) != 1 || got.TestRuns[0].Passed != 42 {
		t.Errorf("TestRuns mismatch after round-trip")
	}
}

func TestProject_PhaseArtifacts_Field(t *testing.T) {
	p := Project{}
	if p.PhaseArtifacts != nil {
		t.Error("PhaseArtifacts should be nil by default")
	}
	now := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	p.PhaseArtifacts = &PhaseArtifacts{
		SRS: map[string]SRSRecord{"c": {Component: "c", Content: "x", AuthoredAt: &now}},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal Project: %v", err)
	}
	var got Project
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal Project: %v", err)
	}
	if got.PhaseArtifacts == nil || got.PhaseArtifacts.SRS["c"].Content != "x" {
		t.Error("PhaseArtifacts not round-tripped through Project")
	}
}

// TestProjectDoc_PhaseArtifacts_RoundTrip verifies that a Project with populated
// PhaseArtifacts and TestingState encodes via encodeProjectDoc (the canonical 2-space
// json.MarshalIndent path) and decodes back equal via decodeProjectDoc.
func TestProjectDoc_PhaseArtifacts_RoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	p := Project{
		ID:    ProjectID("test-proj-001"),
		Name:  "roundtrip test",
		Phase: PhaseConstruction,
		PhaseArtifacts: &PhaseArtifacts{
			SRS: map[string]SRSRecord{
				"authManager": {Component: "authManager", Content: "# SRS", AuthoredAt: &now},
			},
			UXRequirements: map[string]UXRequirementsRecord{
				"homeScreen": {Surface: "homeScreen", Content: "UX reqs", AuthoredAt: &now},
			},
			ProvisioningSpec: map[string]ProvisioningSpecRecord{
				"postgres": {Resource: "postgres", Content: "spec", AuthoredAt: &now},
			},
			DocOutline: map[string]DocOutlineRecord{
				"api-guide": {Doc: "api-guide", Content: "outline", AuthoredAt: &now},
			},
		},
		TestingState: &TestingState{
			SystemTestPlan: &SystemTestPlan{
				UseCaseIndex: []string{"UC1", "UC2"},
				Entries:      []string{"smoke pass", "export flow"},
				Status:       "approved",
				ApprovedAt:   &now,
			},
			HarnessModule: &HarnessModule{RepoRef: "corpus/tests/harness", Status: "approved"},
			PerfHarness:   &PerfHarness{RepoRef: "corpus/tests/perf", Status: ""},
			QualityGates: []QualityGate{
				{ActivityType: "C-IE", Phase: "construction", When: "before", Mode: "escalate"},
			},
			QualityAuditReport: "All gates green.",
			TestRuns: []TestRun{
				{ID: "TR-001", StartedAt: &now, Passed: 100, Failed: 2, Note: "initial run"},
			},
			Defects: []DefectRecord{
				{ID: "D-001", Title: "export returns 500", Severity: "critical", FiledAt: &now},
			},
		},
	}

	encoded, err := encodeProjectDoc(&p, now)
	if err != nil {
		t.Fatalf("encodeProjectDoc: %v", err)
	}

	got, ok, err := decodeProjectDoc(encoded, p.ID)
	if err != nil {
		t.Fatalf("decodeProjectDoc: %v", err)
	}
	if !ok {
		t.Fatal("decodeProjectDoc: project not found")
	}

	// PhaseArtifacts round-trip
	if got.PhaseArtifacts == nil {
		t.Fatal("PhaseArtifacts is nil after round-trip")
	}
	if got.PhaseArtifacts.SRS["authManager"].Content != "# SRS" {
		t.Errorf("PhaseArtifacts.SRS content mismatch: got %q", got.PhaseArtifacts.SRS["authManager"].Content)
	}
	if got.PhaseArtifacts.UXRequirements["homeScreen"].Surface != "homeScreen" {
		t.Errorf("PhaseArtifacts.UXRequirements surface mismatch")
	}
	if got.PhaseArtifacts.ProvisioningSpec["postgres"].Resource != "postgres" {
		t.Errorf("PhaseArtifacts.ProvisioningSpec resource mismatch")
	}
	if got.PhaseArtifacts.DocOutline["api-guide"].Content != "outline" {
		t.Errorf("PhaseArtifacts.DocOutline content mismatch")
	}

	// TestingState round-trip
	if got.TestingState == nil {
		t.Fatal("TestingState is nil after round-trip")
	}
	if got.TestingState.SystemTestPlan == nil || got.TestingState.SystemTestPlan.Status != "approved" {
		t.Errorf("TestingState.SystemTestPlan status mismatch")
	}
	if len(got.TestingState.SystemTestPlan.UseCaseIndex) != 2 {
		t.Errorf("TestingState.SystemTestPlan.UseCaseIndex len mismatch: got %d", len(got.TestingState.SystemTestPlan.UseCaseIndex))
	}
	if got.TestingState.HarnessModule == nil || got.TestingState.HarnessModule.RepoRef != "corpus/tests/harness" {
		t.Errorf("TestingState.HarnessModule mismatch")
	}
	if got.TestingState.QualityAuditReport != "All gates green." {
		t.Errorf("TestingState.QualityAuditReport mismatch")
	}
	if len(got.TestingState.QualityGates) != 1 || got.TestingState.QualityGates[0].Mode != "escalate" {
		t.Errorf("TestingState.QualityGates mismatch")
	}
	if len(got.TestingState.TestRuns) != 1 || got.TestingState.TestRuns[0].Passed != 100 {
		t.Errorf("TestingState.TestRuns mismatch")
	}
	if len(got.TestingState.Defects) != 1 || got.TestingState.Defects[0].Severity != "critical" {
		t.Errorf("TestingState.Defects mismatch")
	}
}

// TestProjectDoc_BackCompat_NoPhaseArtifacts verifies that an existing project.json
// without phaseArtifacts or testingState decodes cleanly to nil/empty containers
// (backward compatibility).
func TestProjectDoc_BackCompat_NoPhaseArtifacts(t *testing.T) {
	// Minimal project.json as it would appear before Task 3 fields were added.
	raw := []byte(`{
  "id": "legacy-project",
  "version": 1,
  "phase": 0,
  "owner": "testowner",
  "name": "legacy project",
  "research": {},
  "slots": {}
}`)
	got, ok, err := decodeProjectDoc(raw, ProjectID("legacy-project"))
	if err != nil {
		t.Fatalf("decodeProjectDoc on legacy JSON: %v", err)
	}
	if !ok {
		t.Fatal("project not found in legacy JSON")
	}
	if got.PhaseArtifacts != nil {
		t.Errorf("PhaseArtifacts should be nil for legacy project.json, got %+v", got.PhaseArtifacts)
	}
	if got.TestingState != nil {
		t.Errorf("TestingState should be nil for legacy project.json, got %+v", got.TestingState)
	}
}
