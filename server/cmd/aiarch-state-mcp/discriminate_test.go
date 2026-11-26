package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const discriminateOut = `noise before
TP-DISCRIMINATE todoListAccess createList CreateList
TP-BROKEN todoListAccess renameList RenameList
skipped todoListAccess archiveList
  TP-DISCRIMINATE   todoListAccess   deleteList   DeleteList  
TP-DISCRIMINATE too few
TP-DISCRIMINATE a b c d e
`

func discriminateRunner(out string, runErr error) checkRunner {
	return checkRunner{root: "", outDir: "", now: time.Now,
		exec: func(context.Context, string, []string, ...string) ([]byte, error) {
			return []byte(out), runErr
		}}
}

func runDiscriminateCommand(t *testing.T, id, out string, runErr error) CheckResult {
	t.Helper()
	r := discriminateRunner(out, runErr)
	r.root, r.outDir = t.TempDir(), t.TempDir()
	return r.Run(context.Background(), []review.SpecCheck{
		{ID: id, Kind: projectstate.CheckKindCommand, Source: review.SpecCheckSource{Argv: []string{"make", "test-discriminate"}}, TimeoutSec: 5},
	})[0]
}

func TestDiscriminateSurvivorsParsed(t *testing.T) {
	res := runDiscriminateCommand(t, projectstate.DiscriminateCheckID, discriminateOut, errors.New("exit status 1"))
	want := []projectstate.DiscriminateSurvivor{
		{Contract: "todoListAccess", Scenario: "createList", Operation: "CreateList"},
		{Contract: "todoListAccess", Scenario: "deleteList", Operation: "DeleteList"},
	}
	if res.Status != checkFail || !slices.Equal(res.Survivors, want) {
		t.Fatalf("status %q survivors %+v, want fail and exactly %+v in order", res.Status, res.Survivors, want)
	}
}

func TestDiscriminatePassingRunHasNoSurvivors(t *testing.T) {
	res := runDiscriminateCommand(t, projectstate.DiscriminateCheckID, "all scenarios discriminate\n", nil)
	if res.Status != checkPass || res.Survivors != nil {
		t.Fatalf("status %q survivors %+v, want pass and nil", res.Status, res.Survivors)
	}
}

func TestDiscriminateOtherCommandCarriesNoSurvivors(t *testing.T) {
	res := runDiscriminateCommand(t, "command:vet", discriminateOut, errors.New("exit status 1"))
	if res.Survivors != nil {
		t.Fatalf("command:vet carries survivors %+v", res.Survivors)
	}
}

// TestDiscriminateSurvivorsSurviveFailedOnlyAndRecord: --failed-only keeps a passing check's
// previous result unchanged, and the results file round-trips survivors exactly.
func TestDiscriminateSurvivorsSurviveFailedOnlyAndRecord(t *testing.T) {
	dir := t.TempDir()
	prev := []CheckResult{{CheckId: projectstate.DiscriminateCheckID, Kind: projectstate.CheckKindCommand, Status: checkFail,
		Survivors: []projectstate.DiscriminateSurvivor{{Contract: "c", Scenario: "s", Operation: "O"}}}}
	if err := writeCheckResults(dir, prev); err != nil {
		t.Fatal(err)
	}
	got, err := readPrevResults(dir)
	if err != nil || !slices.Equal(got[0].Survivors, prev[0].Survivors) {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	// a check that passed before is not re-run and keeps its verdict whole
	passed := []CheckResult{{CheckId: "command:vet", Kind: projectstate.CheckKindCommand, Status: checkPass}, prev[0]}
	r := discriminateRunner("", nil)
	r.root, r.outDir = t.TempDir(), filepath.Join(dir, "out")
	checks := []review.SpecCheck{
		{ID: "command:vet", Kind: projectstate.CheckKindCommand, Source: review.SpecCheckSource{Argv: []string{"x"}}},
		{ID: projectstate.DiscriminateCheckID, Kind: projectstate.CheckKindCommand, Source: review.SpecCheckSource{Argv: []string{"y"}}},
	}
	out := runSpecChecks(context.Background(), r, checks, passed, true)
	if len(out) != 2 || out[0].Status != checkPass || out[1].Survivors != nil {
		t.Fatalf("failed-only: %+v", out)
	}
	// and a still-red previous discriminate result re-runs and is judged afresh
	r = discriminateRunner("TP-DISCRIMINATE c s O\n", errors.New("exit status 1"))
	r.root, r.outDir = t.TempDir(), filepath.Join(dir, "out2")
	out = runSpecChecks(context.Background(), r, checks, passed, true)
	if len(out[1].Survivors) != 1 {
		t.Fatalf("failed-only rerun: %+v", out[1])
	}
}

// ---- AdmitPlanDefects ----

const pdContract = "todoListAccess"

func pdScenario(id, status string) projectstate.CheckResult {
	c := pdContract
	return projectstate.CheckResult{CheckId: "scenario:" + pdContract + ":" + id, Kind: projectstate.CheckKindScenario, Status: status, Contract: &c}
}

func pdCmd(id, status string, survivors ...string) projectstate.CheckResult {
	r := projectstate.CheckResult{CheckId: "command:" + id, Kind: projectstate.CheckKindCommand, Status: status}
	for _, s := range survivors {
		r.Survivors = append(r.Survivors, projectstate.DiscriminateSurvivor{Contract: pdContract, Scenario: s, Operation: "Op"})
	}
	return r
}

func pdReport(scenarios ...string) []projectstate.PlanDefectReport {
	return []projectstate.PlanDefectReport{{ID: "r1:" + pdContract, Contract: pdContract, Scenarios: scenarios, Baseline: map[string]string{}}}
}

func pdRun(results ...projectstate.CheckResult) *projectstate.CheckRun {
	return &projectstate.CheckRun{Results: results}
}

func TestAdmitPlanDefects(t *testing.T) {
	noVerdict := true
	upheldPrior := []projectstate.PlanDefectReport{{Contract: pdContract, Scenarios: []string{"a"}, Baseline: map[string]string{pdContract + ":a": "d1"}}}
	cases := []struct {
		name    string
		reports []projectstate.PlanDefectReport
		prior   []projectstate.PlanDefectReport
		run     *projectstate.CheckRun
		current map[string]string
		want    string // "" admitted, else a substring of the refusal
		today   string // what today's delivery predicate says: "same" or the difference
	}{
		{name: "no run", reports: pdReport("a"), run: nil, want: "no check run", today: "same"},
		{name: "old shape admitted", reports: pdReport("a"),
			run:  pdRun(pdCmd("build", "pass"), pdScenario("a", "fail"), pdScenario("b", "pass")),
			want: "", today: "same"},
		{name: "survivor-only report admitted", reports: pdReport("a"),
			run: pdRun(pdCmd("build", "pass"), pdScenario("a", "pass"), pdScenario("b", "pass"),
				pdCmd("test-discriminate", "fail", "a")),
			want: "", today: "REFUSED today (named but passed; discriminate red and not named)"},
		{name: "RUN-031 shape: survivors plus verdict failures admitted", reports: pdReport("a", "c"),
			run: pdRun(pdCmd("build", "pass"), pdScenario("a", "pass"), pdScenario("b", "pass"), pdScenario("c", "fail"),
				pdCmd("test-discriminate", "fail", "a")),
			want: "", today: "REFUSED today (named but passed; discriminate red and not named)"},
		{name: "survivor with unrelated red refused", reports: pdReport("a"),
			run: pdRun(pdCmd("vet", "fail"), pdScenario("a", "pass"), pdScenario("b", "pass"),
				pdCmd("test-discriminate", "fail", "a")),
			want: "command:vet is red and not named", today: "same (refused)"},
		{name: "unnamed survivor refused", reports: pdReport("a"),
			run: pdRun(pdScenario("a", "pass"), pdScenario("b", "pass"),
				pdCmd("test-discriminate", "fail", "a", "b")),
			want: "scenario:" + pdContract + ":b", today: "same (refused)"},
		{name: "named passed non-survivor refused", reports: pdReport("a"),
			run:  pdRun(pdScenario("a", "pass"), pdScenario("b", "pass"), pdCmd("test-discriminate", "pass")),
			want: "named but passed", today: "same (refused)"},
		{name: "named passed while another survived refused", reports: pdReport("a"),
			run: pdRun(pdScenario("a", "pass"), pdScenario("b", "pass"),
				pdCmd("test-discriminate", "fail", "b")),
			want: "named but passed", today: "same (refused)"},
		{name: "discriminate red without survivors refused", reports: pdReport("a"),
			run:  pdRun(pdScenario("a", "fail"), pdScenario("b", "pass"), pdCmd("test-discriminate", "fail")),
			want: "command:test-discriminate is red and not named", today: "same (refused)"},
		{name: "discriminate timeout with survivors refused", reports: pdReport("a"),
			run: pdRun(pdScenario("a", "pass"), pdScenario("b", "pass"),
				pdCmd("test-discriminate", "timeout", "a")),
			want: "not a recorded failure", today: "same (refused)"},
		{name: "skip not named refused", reports: pdReport("a"),
			run:  pdRun(pdScenario("a", "fail"), pdScenario("b", "pass"), pdCmd("build", "skip")),
			want: "was skipped", today: "same (refused)"},
		{name: "named timeout refused", reports: pdReport("a"),
			run:  pdRun(pdScenario("a", "timeout"), pdScenario("b", "pass")),
			want: "not a recorded failure", today: "same (refused)"},
		{name: "named no verdict refused", reports: pdReport("a"),
			run: func() *projectstate.CheckRun {
				a := pdScenario("a", "fail")
				a.NoVerdict = &noVerdict
				return pdRun(a, pdScenario("b", "pass"))
			}(),
			want: "no verdict", today: "same (refused)"},
		{name: "named with no result refused", reports: pdReport("a", "z"),
			run:  pdRun(pdScenario("a", "fail"), pdScenario("b", "pass")),
			want: "holds no failure", today: "same (refused)"},
		{name: "every scenario red refused", reports: pdReport("a", "b"),
			run:  pdRun(pdScenario("a", "fail"), pdScenario("b", "fail")),
			want: "every scenario of " + pdContract + " failed", today: "same (refused)"},
		{name: "upheld unchanged refused", reports: pdReport("a"), prior: upheldPrior,
			run:     pdRun(pdScenario("a", "fail"), pdScenario("b", "pass")),
			current: map[string]string{pdContract + ":a": "d1"},
			want:    "upheld", today: "same (refused)"},
		{name: "changed binding since is admitted", reports: pdReport("a"), prior: upheldPrior,
			run:     pdRun(pdScenario("a", "fail"), pdScenario("b", "pass")),
			current: map[string]string{pdContract + ":a": "d2"},
			want:    "", today: "same"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := projectstate.AdmitPlanDefects(c.reports, c.prior, c.run, c.current)
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("refused %v; today: %s", err, c.today)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("got %v, want a refusal containing %q", err, c.want)
			}
			t.Logf("today's delivery predicate: %s", c.today)
		})
	}
}
