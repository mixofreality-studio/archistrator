package main

// activityrecords.go — the APP-SIDE ACTIVITY-RECORD rules: the activity execution ledger
// (.activityExecution) checked against what the checkout holds and the runs it recorded.
// They catch the two stale-record shapes the DCT wave produced by hand (D5 had to
// reconcile both in project.json):
//
//	REC-BUILT-UNRECORDED     (Error) a coding activity whose component is BUILT on this
//	                                 checkout — its hand-written implementation file and a
//	                                 generated scenarios file are both present — while its
//	                                 row records no completion: no binary exit and no
//	                                 passed Construction gate (codeReview). The code says
//	                                 the work happened; the ledger says it did not, so the
//	                                 pump would dispatch the activity again and earned value
//	                                 under-reads.
//	REC-TESTING-UNQUALIFIED  (Error) an activity whose lifecycle binds a test plan, whose
//	                                 latest `testing` attempt PASSED, without the CheckRun
//	                                 that pass must stand on (spec 2026-10-02 §1 T4, §6.5):
//	                                 the pass cites no run, or cites a run
//	                                 .testingState.checkRuns does not hold, or the run is
//	                                 another activity's, was recorded at a revision other
//	                                 than the integration attempt the round judged, its
//	                                 scenario results miss or add scenarios against
//	                                 ForComponent(c), or one holds a status that is not
//	                                 `pass`. Rule 4 of §6.5 (the
//	                                 artifact still renders) is a venue fact no checkout
//	                                 holds; the gate that wrote the citation checked it.
//
// WHY APP-SIDE AND WHY ONLY AT `validate`. The built arm needs REPO facts — which files
// sit in a component's package — that neither the platform's methodcheck (a document
// validator) nor the pure DesignHealthEngine can see. So the rule takes the checkout as
// an fs.FS and runs where a checkout root is a parameter: the `validate` subcommand
// (--root) and, for archistrator's own state, `make method-check`
// (TestMethodActivityRecords). It is deliberately NOT appended at the in-loop draft and
// construct-mutation gates (applyGateSeverityPolicies): a construct session's own
// activity branch legitimately holds the built component before main's row records the
// codeReview pass the server writes there, so judging the session's working tree as a
// whole document would refuse the venue's own record-check-run mid-integration.
//
// Attribution: REC-* is construction-owned state (projectstate.AttribActivityExecution in
// projectstate's rule attribution table). A design session (`validate --slot`) cannot write an activity row, so
// for it a REC-* Error is pre-existing other-owner data and downgrades; the
// whole-document gate keeps it at Error.

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// The two activity-record rule ids.
const (
	ruleBuiltUnrecorded    methodcheck.RuleID = "REC-BUILT-UNRECORDED"
	ruleTestingUnqualified methodcheck.RuleID = "REC-TESTING-UNQUALIFIED"
)

// scenarioProjectionStatusPass is the one status a qualifying run may hold for every
// scenario (spec §6.5 rule 3; a skip is not a pass, §3.1).
const scenarioProjectionStatusPass = "pass"

// scenariosGenSuffix is the generated scenarios file every Go component package carries
// once its tests are generated (testgen: <stereotype>_scenarios.gen_test.go).
const scenariosGenSuffix = "_scenarios.gen_test.go"

// componentScenarioProjector projects the canonical scenario set onto one component —
// projectstate.ComponentScenarios in production, the same projection the testing gate
// joins a run against.
type componentScenarioProjector func(p *projectstate.Project, component string) ([]scenario.Scenario, error)

// checkout is the repo facts the built arm reads: the checkout root as an fs.FS, and the
// directory inside it that holds the Go module the contracts' goPackage paths are
// relative to (archistrator keeps it under server/; a scaffolded app at the root).
type checkout struct {
	fsys   fs.FS
	module string
}

// newCheckout resolves the Go module directory of the checkout rooted at fsys: server/
// when it holds a go.mod, else the root itself.
func newCheckout(fsys fs.FS) checkout {
	if _, err := fs.Stat(fsys, "server/go.mod"); err == nil {
		return checkout{fsys: fsys, module: "server"}
	}
	return checkout{fsys: fsys, module: "."}
}

// builtFiles reports the files that make a component BUILT on this checkout: the
// hand-written implementation <goPackage>/<lowercase interface>.go (the file-layout
// standard's impl file) and every generated <goPackage>/*_scenarios.gen_test.go. Both must
// be present; built=false otherwise. A read failure other than absence is an error — the
// rule cannot judge a package it cannot read.
func (c checkout) builtFiles(sc projectstate.ServiceContract) (files []string, built bool, err error) {
	dir := path.Join(c.module, sc.GoPackage)
	impl := path.Join(dir, strings.ToLower(sc.Interface.Name)+".go")
	if _, serr := fs.Stat(c.fsys, impl); serr != nil {
		if errors.Is(serr, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("stat %s: %w", impl, serr)
	}
	gens, gerr := fs.Glob(c.fsys, path.Join(dir, "*"+scenariosGenSuffix))
	if gerr != nil {
		return nil, false, fmt.Errorf("glob %s: %w", dir, gerr)
	}
	if len(gens) == 0 {
		return nil, false, nil
	}
	sort.Strings(gens)
	return append([]string{impl}, gens...), true, nil
}

// activityRecordFindings runs REC-BUILT-UNRECORDED and REC-TESTING-UNQUALIFIED over the
// committed activity list. Like every cross-artifact rule it fires only over COMMITTED
// slots: with no committed ActivityList and System there is no activity to judge.
func activityRecordFindings(proj projectstate.Project, repo checkout, project componentScenarioProjector) ([]methodcheck.Finding, error) {
	list, sys, ok := committedActivityPlan(proj)
	if !ok {
		return nil, nil
	}
	components := make(map[string]projectstate.Component, len(sys.Components))
	for _, c := range sys.Components {
		components[c.ID] = c
	}
	var out []methodcheck.Finding
	for i, item := range list.Activities {
		row, hasRow := proj.ActivityExecution[item.Name]
		var resolved []projectstate.PhaseCompletion
		if hasRow {
			_, _, resolved, _ = projectstate.ResolveConstructionRow(row, item)
		}
		loc := &methodcheck.Location{Ordinal: i, Section: "activityExecution." + item.Name}

		if f, err := builtUnrecorded(proj, item, components, row, hasRow, resolved, repo); err != nil {
			return nil, fmt.Errorf("%s: %w", item.Name, err)
		} else if f != "" {
			out = append(out, methodcheck.Finding{RuleID: ruleBuiltUnrecorded, Severity: methodcheck.SeverityError, Message: f, Location: loc})
		}
		if f := testingUnqualified(&proj, item, row, resolved, project); f != "" {
			out = append(out, methodcheck.Finding{RuleID: ruleTestingUnqualified, Severity: methodcheck.SeverityError, Message: f, Location: loc})
		}
	}
	return out, nil
}

// committedActivityPlan returns the committed ActivityList and System, ok=false when either
// slot is not committed (nothing to join yet, never a violation).
func committedActivityPlan(proj projectstate.Project) (*projectstate.ActivityList, *projectstate.System, bool) {
	if proj.ActivityList.Status != projectstate.ReviewCommitted || proj.SystemDesign.Status != projectstate.ReviewCommitted {
		return nil, nil, false
	}
	list, lok := proj.ActivityList.Model.(*projectstate.ActivityList)
	sys, sok := proj.SystemDesign.Model.(*projectstate.System)
	if !lok || !sok || list == nil || sys == nil {
		return nil, nil, false
	}
	return list, sys, true
}

// builtUnrecorded is the REC-BUILT-UNRECORDED message for one activity, "" when the rule
// holds. Only an activity that builds a Go component is in its reach: the component's own
// contract (filed under projectstate.ContractKeyFor) names the package the implementation
// and generated scenarios live in. A Resource is provisioned, not built — skipped by its
// KIND; every other component has a key, so a fresh project's components are in reach the
// moment their contract is recorded. A client with no goPackage is a UI app whose code
// this arm does not read.
func builtUnrecorded(
	proj projectstate.Project, item projectstate.ActivityItem, components map[string]projectstate.Component,
	row projectstate.ActivityExecution, hasRow bool, resolved []projectstate.PhaseCompletion, repo checkout,
) (string, error) {
	component, ok := components[item.ComponentID]
	if !ok {
		return "", nil
	}
	key, keyed := projectstate.ContractKeyFor(component)
	if !keyed {
		return "", nil
	}
	own, ok := proj.ServiceContracts[key]
	if !ok || own.GoPackage == "" || own.Interface.Name == "" {
		return "", nil
	}
	files, built, err := repo.builtFiles(own)
	if err != nil || !built {
		return "", err
	}
	if hasRow && (row.CompletedAt != nil || phaseCompleted(resolved, projectstate.MethodPhaseConstruction)) {
		return "", nil
	}
	record := "has no .activityExecution row"
	if hasRow {
		record = "records neither a binary exit (completedAt) nor a passed Construction gate (codeReview)"
	}
	return fmt.Sprintf(
		"activity %s builds component %q, which is built on this checkout (%s), but its record %s — the ledger says the work never happened, so the pump would dispatch it again; record the construction it did (its attempts) or remove the code",
		item.Name, component.ID, strings.Join(files, " + "), record), nil
}

// phaseCompleted reports whether the resolved phase set carries phase as completed.
func phaseCompleted(resolved []projectstate.PhaseCompletion, phase projectstate.ActivityMethodPhase) bool {
	return slices.ContainsFunc(resolved, func(pc projectstate.PhaseCompletion) bool { return pc.Phase == phase && pc.Completed })
}

// testingUnqualified is the REC-TESTING-UNQUALIFIED message for one activity, "" when the
// rule holds. It judges the LATEST testing attempt (highest attempt number, the attempt
// App A's completion rule reads) and only when it passed, on a lifecycle that binds a test
// plan — the resolved phase set carries test_plan.
func testingUnqualified(
	proj *projectstate.Project, item projectstate.ActivityItem, row projectstate.ActivityExecution,
	resolved []projectstate.PhaseCompletion, project componentScenarioProjector,
) string {
	if !slices.ContainsFunc(resolved, func(pc projectstate.PhaseCompletion) bool { return pc.Phase == projectstate.MethodPhaseTestPlan }) {
		return ""
	}
	pass, at, ok := latestTesting(row.Attempts)
	if !ok || pass.Outcome != projectstate.OutcomePassed {
		return ""
	}
	reasons := runReasons(proj, item, pass, judgedIntegration(row.Attempts[:at]), project)
	if len(reasons) == 0 {
		return ""
	}
	return fmt.Sprintf("activity %s: testing attempt %s passed without a qualifying CheckRun — %s (T4: the testing task passes only on a full green run at the judged revision)",
		item.Name, pass.AttemptID, strings.Join(reasons, "; "))
}

// latestTesting returns the testing attempt with the highest attempt number and its index
// in the ledger.
func latestTesting(attempts []projectstate.TaskAttempt) (projectstate.TaskAttempt, int, bool) {
	at := -1
	for i, a := range attempts {
		if a.Task == projectstate.TaskTesting && (at < 0 || a.Attempt > attempts[at].Attempt) {
			at = i
		}
	}
	if at < 0 {
		return projectstate.TaskAttempt{}, 0, false
	}
	return attempts[at], at, true
}

// judgedIntegration is the integration attempt a testing round judged: the last
// integration attempt the append-only ledger holds before that round's attempt. "" when
// there is none.
func judgedIntegration(before []projectstate.TaskAttempt) string {
	for i := len(before) - 1; i >= 0; i-- {
		if before[i].Task == projectstate.TaskIntegration {
			return before[i].AttemptID
		}
	}
	return ""
}

// runReasons lists why the run a testing pass cites does not qualify; empty when it does.
// It mirrors the testing gate (delivery Manager, testingGateHold) on everything the
// committed document holds: the citation, the run's activity and revision, set equality
// with the projection, and every status `pass`.
func runReasons(proj *projectstate.Project, item projectstate.ActivityItem, pass projectstate.TaskAttempt, judged string, project componentScenarioProjector) []string {
	if pass.Evidence.Kind != projectstate.EvidenceCheckRun || pass.Evidence.Ref == "" {
		return []string{fmt.Sprintf("it cites no CheckRun (evidence %q %q)", pass.Evidence.Kind, pass.Evidence.Ref)}
	}
	run, ok := recordedRun(proj, pass.Evidence.Ref)
	if !ok {
		return []string{fmt.Sprintf("it cites CheckRun %q, which .testingState.checkRuns does not hold", pass.Evidence.Ref)}
	}
	var reasons []string
	if run.Activity != item.Name {
		reasons = append(reasons, fmt.Sprintf("CheckRun %s belongs to activity %q", run.ID, run.Activity))
	}
	switch {
	case judged == "":
		reasons = append(reasons, "the ledger holds no integration attempt for the round to have judged")
	case run.Revision != judged:
		reasons = append(reasons, fmt.Sprintf("CheckRun %s was recorded at revision %q, not at the judged integration attempt %q", run.ID, run.Revision, judged))
	}
	projected, err := project(proj, item.ComponentID)
	if err != nil {
		return append(reasons, fmt.Sprintf("the scenarios of component %q cannot be projected: %v", item.ComponentID, err))
	}
	scenarios := scenarioResultsOf(run)
	missing, unknown := scenarioSetDiff(projected, scenarios)
	if len(missing) > 0 {
		reasons = append(reasons, fmt.Sprintf("CheckRun %s never ran projected scenario(s) %s", run.ID, strings.Join(missing, ", ")))
	}
	if len(unknown) > 0 {
		reasons = append(reasons, fmt.Sprintf("CheckRun %s reports scenario(s) no projection holds: %s", run.ID, strings.Join(unknown, ", ")))
	}
	if bad := notPassed(scenarios); len(bad) > 0 {
		reasons = append(reasons, fmt.Sprintf("CheckRun %s did not pass: %s", run.ID, strings.Join(bad, ", ")))
	}
	return reasons
}

// recordedRun returns the run .testingState.checkRuns holds under id.
func recordedRun(proj *projectstate.Project, id string) (projectstate.CheckRun, bool) {
	if proj.TestingState == nil {
		return projectstate.CheckRun{}, false
	}
	i := slices.IndexFunc(proj.TestingState.CheckRuns, func(r projectstate.CheckRun) bool { return r.ID == id })
	if i < 0 {
		return projectstate.CheckRun{}, false
	}
	return proj.TestingState.CheckRuns[i], true
}

// scenarioResult is one scenario's verdict out of a CheckRun.
type scenarioResult struct {
	Scenario, Status string
}

// scenarioResultsOf is the run's scenario verdicts: its scenario-kind results, each
// check id "scenario:<contract>:<scenario id>" reduced to the scenario id.
func scenarioResultsOf(run projectstate.CheckRun) []scenarioResult {
	var out []scenarioResult
	for _, r := range run.Results {
		if r.Kind != projectstate.CheckKindScenario {
			continue
		}
		id := strings.TrimPrefix(r.CheckId, projectstate.CheckKindScenario+":")
		if _, rest, ok := strings.Cut(id, ":"); ok {
			id = rest
		}
		out = append(out, scenarioResult{Scenario: id, Status: r.Status})
	}
	return out
}

// scenarioSetDiff returns the projected scenario ids the run lacks and the run's ids no
// projection holds, each sorted.
func scenarioSetDiff(projected []scenario.Scenario, results []scenarioResult) (missing, unknown []string) {
	ran := make(map[string]bool, len(results))
	for _, r := range results {
		ran[r.Scenario] = true
	}
	want := make(map[string]bool, len(projected))
	for _, sc := range projected {
		want[sc.ID] = true
		if !ran[sc.ID] {
			missing = append(missing, sc.ID)
		}
	}
	for _, r := range results {
		if !want[r.Scenario] {
			unknown = append(unknown, r.Scenario)
		}
	}
	sort.Strings(missing)
	sort.Strings(unknown)
	return missing, unknown
}

// notPassed lists every result whose status is not `pass`, as "<id>=<status>".
func notPassed(results []scenarioResult) []string {
	var bad []string
	for _, r := range results {
		if r.Status != scenarioProjectionStatusPass {
			bad = append(bad, r.Scenario+"="+r.Status)
		}
	}
	sort.Strings(bad)
	return bad
}
