package main

// recordtestplan_test.go — the `record-test-plan` ONE-SHOT subcommand: the single
// writer for one component's test plan (deterministic component testing design, §3).
// It splices the record into .phaseArtifacts.testPlan[<component>] byte-for-byte and
// refuses any write that leaves a TP-* error for that component or adds an error the
// baseline did not have.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// committedProject is archistrator's own committed state document, the fixture the
// gate is measured against (its per-component plans are real, bound records).
const committedProject = "../../../.aiarch/state/project.json"

// copyCommittedProject copies the committed project.json into a temp dir and returns
// the copy's path and its bytes.
func copyCommittedProject(t *testing.T) (string, []byte) {
	t.Helper()
	raw, err := os.ReadFile(committedProject)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, raw
}

// committedPlan returns the committed testPlan record for component, as its bytes
// appear in the document (key order and escaping kept).
func committedPlan(t *testing.T, raw []byte, component string) json.RawMessage {
	t.Helper()
	var doc struct {
		PhaseArtifacts struct {
			TestPlan map[string]json.RawMessage `json:"testPlan"`
		} `json:"phaseArtifacts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	rec, ok := doc.PhaseArtifacts.TestPlan[component]
	if !ok {
		t.Fatalf("the committed project.json has no testPlan for %s", component)
	}
	return rec
}

// writeRecord writes rec into a temp file and returns its path.
func writeRecord(t *testing.T, rec []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(path, rec, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRecordTestPlan_IsAOneShotSubcommand: the writer is reached through the
// aiarch-state-mcp binary, not a command of its own (server/cmd is a closed set).
func TestRecordTestPlan_IsAOneShotSubcommand(t *testing.T) {
	handled, err := runOneShotCommand([]string{"record-test-plan"})
	if !handled {
		t.Fatal("record-test-plan is not dispatched as a one-shot subcommand")
	}
	if err == nil || !strings.Contains(err.Error(), "record-test-plan: --project is required") {
		t.Fatalf("want the missing-flag refusal prefixed with the subcommand, got %v", err)
	}
}

// TestRecordTestPlan_ReRecordingACommittedPlanIsByteIdentical: re-recording a
// component's committed plan verbatim passes the gate and leaves every byte of the
// document as it was — the write is a splice, never a lossy codec re-encode.
func TestRecordTestPlan_ReRecordingACommittedPlanIsByteIdentical(t *testing.T) {
	path, raw := copyCommittedProject(t)
	rec := writeRecord(t, committedPlan(t, raw, "autoscalerEngine"))
	var out bytes.Buffer
	if err := runRecordTestPlan([]string{"--project", path, "--component", "autoscalerEngine", "--record", rec}, &out); err != nil {
		t.Fatalf("re-recording the committed plan was refused: %v", err)
	}
	if !strings.Contains(out.String(), "recorded testPlan[autoscalerEngine]") {
		t.Fatalf("no confirmation on stdout: %q", out.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("re-recording the committed plan changed the document")
	}
}

// TestRecordTestPlan_RefusesATPErrorForTheComponent: a record that drops the
// component's bindings leaves TP-BOUND errors for it, so the write is refused and the
// document is untouched.
func TestRecordTestPlan_RefusesATPErrorForTheComponent(t *testing.T) {
	path, raw := copyCommittedProject(t)
	var plan map[string]any
	if err := json.Unmarshal(committedPlan(t, raw, "autoscalerEngine"), &plan); err != nil {
		t.Fatal(err)
	}
	plan["bindings"] = []any{}
	unbound, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	rec := writeRecord(t, unbound)
	err = runRecordTestPlan([]string{"--project", path, "--component", "autoscalerEngine", "--record", rec}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "TP-BOUND") || !strings.Contains(err.Error(), "for autoscalerEngine") {
		t.Fatalf("want a TP-BOUND refusal for autoscalerEngine, got %v", err)
	}
	got, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("a refused record still wrote the document")
	}
}

// TestRecordTestPlan_RefusesARecordNamingAnotherComponent: the record's component must
// be the one the writer was asked to record.
func TestRecordTestPlan_RefusesARecordNamingAnotherComponent(t *testing.T) {
	path, raw := copyCommittedProject(t)
	rec := writeRecord(t, committedPlan(t, raw, "autoscalerEngine"))
	err := runRecordTestPlan([]string{"--project", path, "--component", "reviewEngine", "--record", rec}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "does not name reviewEngine") {
		t.Fatalf("want a component-mismatch refusal, got %v", err)
	}
}

// TestSpliceTestPlan_InsertsANewComponentAtItsSortedPosition: a component with no
// testPlan key yet is inserted among the sorted keys; every other byte is kept.
func TestSpliceTestPlan_InsertsANewComponentAtItsSortedPosition(t *testing.T) {
	doc := "{\n  \"phaseArtifacts\": {\n    \"testPlan\": {\n      \"a\": {\n        \"component\": \"a\"\n      },\n      \"c\": {\n        \"component\": \"c\"\n      }\n    }\n  }\n}\n"
	got, err := spliceTestPlan([]byte(doc), "b", []byte(`{"component":"b"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"phaseArtifacts\": {\n    \"testPlan\": {\n      \"a\": {\n        \"component\": \"a\"\n      },\n      \"b\": {\n        \"component\": \"b\"\n      },\n      \"c\": {\n        \"component\": \"c\"\n      }\n    }\n  }\n}\n"
	if string(got) != want {
		t.Fatalf("splice:\n%s\nwant:\n%s", got, want)
	}
	got, err = spliceTestPlan([]byte(doc), "d", []byte(`{"component":"d"}`))
	if err != nil {
		t.Fatal(err)
	}
	want = "{\n  \"phaseArtifacts\": {\n    \"testPlan\": {\n      \"a\": {\n        \"component\": \"a\"\n      },\n      \"c\": {\n        \"component\": \"c\"\n      },\n      \"d\": {\n        \"component\": \"d\"\n      }\n    }\n  }\n}\n"
	if string(got) != want {
		t.Fatalf("append splice:\n%s\nwant:\n%s", got, want)
	}
}

// TestSpliceTestPlan_ReplacesAnExistingComponentInPlace: an existing entry is replaced
// in place, its neighbours untouched.
func TestSpliceTestPlan_ReplacesAnExistingComponentInPlace(t *testing.T) {
	doc := "{\n  \"phaseArtifacts\": {\n    \"testPlan\": {\n      \"a\": {\n        \"component\": \"a\"\n      },\n      \"b\": {\n        \"component\": \"b\",\n        \"bindings\": []\n      }\n    }\n  }\n}\n"
	got, err := spliceTestPlan([]byte(doc), "a", []byte(`{"component":"a","bindings":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"phaseArtifacts\": {\n    \"testPlan\": {\n      \"a\": {\n        \"component\": \"a\",\n        \"bindings\": []\n      },\n      \"b\": {\n        \"component\": \"b\",\n        \"bindings\": []\n      }\n    }\n  }\n}\n"
	if string(got) != want {
		t.Fatalf("splice:\n%s\nwant:\n%s", got, want)
	}
}
