package main

// writerefusal_test.go pins ISS-028-2 part 1 on the aiarch-state side: a write the Method write gate
// refuses is appended, structured and attributed to the design slot that owns each rule, to the sidecar
// the venue names in AIARCH_REFUSALS_FILE, so the venue can tell supervision about it as a typed cause
// instead of leaving it in the agent's prose.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// refusedRun028Write makes the RUN-028 acceptance-plan write the gate refuses for
// UC-OUTCOME-AT-ASKING-SURFACE, on a session whose sidecar is sidecar ("" for none).
func refusedRun028Write(t *testing.T, sidecar string) error {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(run028Root, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	proj, _, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := seedProject(t, proj, jobModeConstruct, 0)
	s.ActivityID = "N-ATP"
	s.RefusalsFile = sidecar
	return s.recordPhaseArtifact("acceptance", map[string]any{
		"acceptancePlan": map[string]any{"principals": map[string]any{}, "bindings": []any{}},
	})
}

func readRefusals(t *testing.T, path string) []agenticjob.WriteRefused {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("the refusals sidecar was not written: %v", err)
	}
	defer func() { _ = f.Close() }()
	var out []agenticjob.WriteRefused
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var wr agenticjob.WriteRefused
		if err := json.Unmarshal(sc.Bytes(), &wr); err != nil {
			t.Fatalf("sidecar line %q is not a WriteRefused: %v", sc.Text(), err)
		}
		out = append(out, wr)
	}
	return out
}

func TestRefusedWriteIsAppendedToTheSidecar(t *testing.T) {
	sidecar := filepath.Join(t.TempDir(), "refusals.jsonl")
	werr := refusedRun028Write(t, sidecar)
	if werr == nil || !strings.Contains(werr.Error(), "UC-OUTCOME-AT-ASKING-SURFACE") {
		t.Fatalf("the write must still be refused, naming UC-OUTCOME-AT-ASKING-SURFACE, got: %v", werr)
	}
	got := readRefusals(t, sidecar)
	if len(got) != 1 {
		t.Fatalf("the sidecar holds %d refusals, want 1: %+v", len(got), got)
	}
	if got[0].Verb == "" {
		t.Error("the refusal names no verb")
	}
	wantKind, wantClass := projectstate.AttributeRule("UC-OUTCOME-AT-ASKING-SURFACE")
	if wantClass != projectstate.AttribSlot {
		t.Fatalf("UC-OUTCOME-AT-ASKING-SURFACE is no slot-owned rule (%v): the fixture is stale", wantClass)
	}
	var seen bool
	for _, f := range got[0].Findings {
		if f.Severity != "error" {
			t.Errorf("finding %s has severity %q: the write gate keeps only Errors", f.RuleID, f.Severity)
		}
		if f.Message == "" {
			t.Errorf("finding %s has no message", f.RuleID)
		}
		if f.RuleID == "UC-OUTCOME-AT-ASKING-SURFACE" {
			seen = true
			if f.OwnerSlot != wantKind.WireName() {
				t.Errorf("owner slot = %q, want %q", f.OwnerSlot, wantKind.WireName())
			}
		}
		if _, class := projectstate.AttributeRule(f.RuleID); class != projectstate.AttribSlot && f.OwnerSlot != "" {
			t.Errorf("finding %s is owned by no slot yet names owner %q", f.RuleID, f.OwnerSlot)
		}
	}
	if !seen {
		t.Errorf("the refusal does not carry UC-OUTCOME-AT-ASKING-SURFACE: %+v", got[0].Findings)
	}
}

// A session with no sidecar (the cloud arm, a hand-run session) refuses exactly as before and writes nothing.
func TestRefusedWriteWithoutASidecarIsStillRefused(t *testing.T) {
	if werr := refusedRun028Write(t, ""); werr == nil {
		t.Fatal("the write must be refused")
	}
}
