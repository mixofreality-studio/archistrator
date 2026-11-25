package main

// run028gate_test.go pins ISS-028-2: a state-recording task's prefix-scoped rule check (rule:tp,
// rule:atp) agrees with the state write gate. The RUN-028 System carries 8
// UC-OUTCOME-AT-ASKING-SURFACE Errors outside ATP-/TP-; the check passed while every
// recordPhaseArtifact of the acceptance plan was refused for them.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func atpCheck() review.SpecCheck {
	return review.SpecCheck{
		ID: "rule:atp", Kind: "rule", TimeoutSec: 60,
		Source: review.SpecCheckSource{RulePrefixes: []string{"ATP-", "TP-"}},
	}
}

func runRuleATP(t *testing.T, state []byte) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, statePathPrefix), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, statePathPrefix, projectFile), state, 0o644); err != nil {
		t.Fatal(err)
	}
	r := checkRunner{root: root, outDir: t.TempDir(), now: time.Now}
	res := r.Run(context.Background(), []review.SpecCheck{atpCheck()})
	log, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "rule_atp.log"))
	return res[0].Status, string(log)
}

func TestRun028RuleATPAgreesWithTheWriteGate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(run028Root, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	proj, _, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	wantErrs, err := writeGateErrors(raw, proj)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, f := range wantErrs {
		want = append(want, string(f.RuleID))
	}
	slices.Sort(want)

	// The write is refused, naming the rule.
	s, _ := seedProject(t, proj, jobModeConstruct, 0)
	s.ActivityID = "N-ATP"
	werr := s.recordPhaseArtifact("acceptance", map[string]any{
		"acceptancePlan": map[string]any{"principals": map[string]any{}, "bindings": []any{}},
	})
	if werr == nil || !strings.Contains(werr.Error(), "UC-OUTCOME-AT-ASKING-SURFACE") {
		t.Fatalf("the write must be refused naming UC-OUTCOME-AT-ASKING-SURFACE, got: %v", werr)
	}

	// The check fails with the same set of Error rule ids.
	status, log := runRuleATP(t, raw)
	if status != "fail" {
		t.Fatalf("rule:atp passed while the write is refused; log:\n%s", log)
	}
	_, after, ok := strings.Cut(log, "the state write gate also refuses (")
	if !ok {
		t.Fatalf("the log lacks the write-gate line:\n%s", log)
	}
	got := gateLogRuleIDs(after)
	if !slices.Equal(got, want) {
		t.Fatalf("check Error rule ids %v != write-gate Error rule ids %v", got, want)
	}
	if !slices.Contains(got, "UC-OUTCOME-AT-ASKING-SURFACE") {
		t.Fatalf("log must name UC-OUTCOME-AT-ASKING-SURFACE:\n%s", log)
	}
	for _, id := range got {
		if !strings.Contains(werr.Error(), id) {
			t.Errorf("the refusal does not name %s", id)
		}
	}
}

func TestRuleATPCleanDocumentStillPasses(t *testing.T) {
	b, err := projectstate.EncodeProjectJSON(minimalProject())
	if err != nil {
		t.Fatal(err)
	}
	if status, log := runRuleATP(t, b); status != "pass" {
		t.Fatalf("a document with no whole-document Error must pass; status %s, log:\n%s", status, log)
	}
}

// gateLogRuleIDs is the sorted rule ids of the ERROR lines under the write-gate line of a log.
func gateLogRuleIDs(after string) []string {
	var got []string
	for _, ln := range strings.Split(after, "\n")[1:] {
		if i, j := strings.Index(ln, "["), strings.Index(ln, "]"); strings.HasPrefix(ln, "ERROR") && i >= 0 && j > i {
			got = append(got, ln[i+1:j])
		}
	}
	slices.Sort(got)
	return got
}
