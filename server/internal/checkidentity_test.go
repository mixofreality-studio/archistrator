package internal_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// TestCheckIdentity_VerdictIsKeyedByItsContract: a scenario verdict belongs to the contract it
// carries, not to whatever the check id's text reads as. Two results with the same scenario id, one
// recorded for the viewed contract and one for another, land one each; a result whose id names the
// viewed contract but which carries another's (or none) is not the viewed contract's verdict.
func TestCheckIdentity_VerdictIsKeyedByItsContract(t *testing.T) {
	proj := *ownProject(t)
	projected, err := projectstate.ComponentScenarios(&proj, viewContract)
	if err != nil || len(projected) < 3 {
		t.Fatalf("projection of %s: %d scenarios, %v", viewContract, len(projected), err)
	}
	prefix := projectstate.CheckKindScenario + ":" + viewContract + ":"
	run := projectstate.CheckRun{ID: "identity", Activity: viewActivity, Task: "integration",
		Revision: viewActivity + ":integration:1", EndedAt: endedAt(2100), Artifact: "artifact-identity",
		Results: []projectstate.CheckResult{
			{CheckId: prefix + projected[0].ID, Kind: projectstate.CheckKindScenario, Contract: ptr(viewContract), Status: "pass"},
			{CheckId: projectstate.CheckKindScenario + ":otherContract:" + projected[0].ID, Kind: projectstate.CheckKindScenario, Contract: ptr("otherContract"), Status: "fail"},
			{CheckId: prefix + projected[1].ID, Kind: projectstate.CheckKindScenario, Contract: ptr("otherContract"), Status: "pass"},
			{CheckId: prefix + projected[2].ID, Kind: projectstate.CheckKindScenario, Status: "pass"},
		}}
	pid, activity, contract := "archistrator", viewActivity, viewContract
	v, err := viewManager(withCheckRun(proj, run), nil).QueryProjectView(viewCtx(), delivery.ProjectViewQuery{
		Kind: delivery.ProjectViewTestRun, ProjectID: &pid, ActivityID: &activity, ComponentID: &contract})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for _, sc := range v.TestRun.Scenarios {
		byID[sc.Id] = sc.Status
	}
	if got := byID[projected[0].ID]; got != "pass" {
		t.Errorf("%s: the viewed contract's own verdict is %q, want pass (the other contract's red one is not its)", projected[0].ID, got)
	}
	for _, sc := range projected[1:3] {
		if got := byID[sc.ID]; got != "missing" {
			t.Errorf("%s: %q, want missing: its result does not carry the viewed contract", sc.ID, got)
		}
	}
}

// TestCheckIdentity_SingleJoin: the component-to-contracts join exists once, in the platform
// (methodcheck.ContractsOf), and every reader that needs it calls it. The three joins that once
// matched a component to its contracts by hand, with two different rules, are gone: no
// non-test file of the layers that read specs, checks and verdicts compares a contract's
// Component field itself.
func TestCheckIdentity_SingleJoin(t *testing.T) {
	// The shapes the three hand joins took: a service contract's Component compared with a key,
	// raw or through scenario.Normalize.
	join := regexp.MustCompile(`\bsc\.Component\s*==\s*key\b|Normalize\(sc\.Component\)\s*==\s*\w+\.Normalize\(key\)`)
	for _, dir := range []string{
		"../cmd/aiarch-state-mcp", "manager/delivery", "resourceaccess/projectstate", "engine/review",
	} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "testdata" {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(raw), "\n") {
				if code, _, _ := strings.Cut(line, "//"); join.MatchString(code) {
					t.Errorf("%s:%d joins a contract to its component by hand (use methodcheck.ContractsOf): %s", path, i+1, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
