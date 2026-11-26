package main

// run030_admit_test.go pins projectstate.AdmitUpstreamDefects, the check the delivery Manager
// re-applies to the report it reads from the branch, on RUN-030's committed slot 4. It is
// called directly: the verb's StampUpstreamRuleRefs repeats the family check, so a test that
// goes through reportUpstreamDefect cannot see the Manager-side admission, and projectstate
// admits only generated scenario tests (TestFileLayout), none of which bind this function.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func run030Project(t *testing.T) *projectstate.Project {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run030.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, ok, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil || !ok {
		t.Fatalf("the RUN-030 project does not decode: %v", err)
	}
	return &p
}

func run030Stamped(rule, uc, scenario string) projectstate.UpstreamRuleRef {
	return projectstate.UpstreamRuleRef{RuleID: rule, UseCase: uc, Scenario: scenario, Finding: "the refusal's message"}
}

func TestRun030AdmitUpstreamDefectsAdmits(t *testing.T) {
	p := run030Project(t)
	route := methodassets.UpstreamRoute{ActivityType: "requirements", Gate: run014Gate}
	rules := []projectstate.UpstreamRuleRef{run030Stamped(run030Rule, run030UseCase, "")}

	defects, err := projectstate.AdmitUpstreamDefects(p, route, nil, rules)
	if err != nil || len(defects) != 1 {
		t.Fatalf("a stamped core-use-case rule finding at a committed use case must be admitted: %v %v", defects, err)
	}
	f, ok := defects[0].(projectstate.UseCaseRuleFinding)
	if !ok || f.RuleID != run030Rule || f.UseCaseID != run030UseCase || f.Finding != "the refusal's message" {
		t.Errorf("the admitted defect = %+v", defects[0])
	}

	nodes := []projectstate.UpstreamNodeRef{{UseCase: run030UseCase, Node: "apply-change"}}
	if defects, err = projectstate.AdmitUpstreamDefects(p, route, nodes, rules); err != nil || len(defects) != 2 {
		t.Errorf("a report may name both classes: %v %v", defects, err)
	}
}

func TestRun030AdmitUpstreamDefectsRefuses(t *testing.T) {
	p := run030Project(t)
	route := methodassets.UpstreamRoute{ActivityType: "requirements", Gate: run014Gate}
	one := func(r projectstate.UpstreamRuleRef) []projectstate.UpstreamRuleRef {
		return []projectstate.UpstreamRuleRef{r}
	}
	for name, tc := range map[string]struct {
		nodes []projectstate.UpstreamNodeRef
		rules []projectstate.UpstreamRuleRef
		want  string
	}{
		"a rule outside the family":    {nil, one(run030Stamped("CC-COVERAGE", run030UseCase, "")), "not a core-use-case rule"},
		"an unknown rule":              {nil, one(run030Stamped("NO-SUCH-RULE", run030UseCase, "")), "not a core-use-case rule"},
		"a finding the verb never set": {nil, one(projectstate.UpstreamRuleRef{RuleID: run030Rule, UseCase: run030UseCase}), "carries no finding"},
		"a use case that is not there": {nil, one(run030Stamped(run030Rule, "ghost", "")), "no committed use case"},
		"the same finding twice": {nil, []projectstate.UpstreamRuleRef{
			run030Stamped(run030Rule, run030UseCase, ""), run030Stamped(run030Rule, run030UseCase, "")}, "twice"},
		"nothing named": {nil, nil, "at least one"},
		"a decision as an unrealizable node": {[]projectstate.UpstreamNodeRef{{UseCase: run030UseCase, Node: "which-surface"}}, nil,
			"no realization obligation"},
	} {
		if _, err := projectstate.AdmitUpstreamDefects(p, route, tc.nodes, tc.rules); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want a refusal naming %q", name, err, tc.want)
		}
	}

	uncommitted := *p
	uncommitted.CoreUseCases.Status = projectstate.ReviewNone
	if _, err := projectstate.AdmitUpstreamDefects(&uncommitted, route, nil, one(run030Stamped(run030Rule, run030UseCase, ""))); err == nil ||
		!strings.Contains(err.Error(), "not committed") {
		t.Errorf("with no committed core use cases nothing is admitted: %v", err)
	}
	other := methodassets.UpstreamRoute{ActivityType: "requirements", Gate: "missionReview"}
	if _, err := projectstate.AdmitUpstreamDefects(p, other, nil, one(run030Stamped(run030Rule, run030UseCase, ""))); err == nil {
		t.Error("a gate that does not judge the core use cases admits no use-case defects")
	}
}

func TestRun030StampUpstreamRuleRefsRefusals(t *testing.T) {
	p := run030Project(t)
	ref := projectstate.UpstreamRuleRef{RuleID: run030Rule, UseCase: run030UseCase}
	if _, err := projectstate.StampUpstreamRuleRefs(p, []projectstate.UpstreamRuleRef{ref}, nil); err == nil || !strings.Contains(err.Error(), "reports no Error") {
		t.Errorf("a ref no refusal reported must not be stamped: %v", err)
	}
	outside := projectstate.UpstreamRuleRef{RuleID: "CC-COVERAGE", UseCase: run030UseCase}
	if _, err := projectstate.StampUpstreamRuleRefs(p, []projectstate.UpstreamRuleRef{outside}, nil); err == nil || !strings.Contains(err.Error(), "not a core-use-case rule") {
		t.Errorf("a rule outside the family must not be stamped: %v", err)
	}
}
