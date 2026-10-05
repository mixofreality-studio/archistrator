package main

// recordtestrun_test.go — the `record-test-run` ONE-SHOT subcommand the construction
// venue runs after the scenario tests: it turns test-results/<component>/results.json
// into a projectstate.TestRun on .testingState.testRuns (deterministic component
// testing design, §6.1/§6.2).

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// writeResultsIndex writes DIR/billingManager/results.json with body and returns DIR.
func writeResultsIndex(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	writeResultsIndexUnder(t, dir, body)
	return dir
}

// writeResultsIndexUnder writes dir/billingManager/results.json with body.
func writeResultsIndexUnder(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "billingManager"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "billingManager", "results.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// recordTestRunFlags is the full required flag set, over results dir.
func recordTestRunFlags(dir string) []string {
	return []string{
		"--activity", "C-BG",
		"--component", "billingManager",
		"--run-id", "1",
		"--artifact", "a",
		"--results", dir,
		"--revision", "abc123",
	}
}

// TestRecordTestRun_EmptyDirFails: an empty test-results/ directory (no
// <component>/results.json) is a LOUD failure, never an empty passing run (Review
// Focus 5).
func TestRecordTestRun_EmptyDirFails(t *testing.T) {
	dir := t.TempDir()
	err := runRecordTestRun(recordTestRunFlags(dir), nil)
	if err == nil || !strings.Contains(err.Error(), "no results.json") {
		t.Fatalf("want loud failure, got %v", err)
	}
}

// TestRecordTestRun_WritesRun: a well-formed results.json becomes a TestRun carrying
// the flags verbatim and one ScenarioResult per entry, delivered to the sink.
func TestRecordTestRun_WritesRun(t *testing.T) {
	dir := writeResultsIndex(t, `[{"scenario":"bill-the-user-for-usage-P1","status":"pass","durationMs":5}]`)
	var got projectstate.TestRun
	err := runRecordTestRun(recordTestRunFlags(dir), func(r projectstate.TestRun) error { got = r; return nil })
	if err != nil || got.ID != "1" || len(got.Scenarios) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	if got.Activity != "C-BG" || got.Component != "billingManager" || got.Artifact != "a" || got.Revision != "abc123" {
		t.Fatalf("flags not carried verbatim: %+v", got)
	}
	sc := got.Scenarios[0]
	if sc.Scenario != "bill-the-user-for-usage-P1" || sc.Status != "pass" || sc.DurationMs != 5 {
		t.Fatalf("scenario not carried: %+v", sc)
	}
	if got.StartedAt == nil || got.EndedAt == nil || got.EndedAt.Before(*got.StartedAt) {
		t.Fatalf("started/ended must come from the file mtimes: %+v", got)
	}
}

// TestRecordTestRun_EmptyResultsFails: a results.json with ZERO entries is the same
// hole as a missing one — nothing ran — so it is refused rather than recorded as a
// vacuously passing run.
func TestRecordTestRun_EmptyResultsFails(t *testing.T) {
	dir := writeResultsIndex(t, `[]`)
	called := false
	err := runRecordTestRun(recordTestRunFlags(dir), func(projectstate.TestRun) error { called = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "no scenarios") {
		t.Fatalf("want loud failure, got %v", err)
	}
	if called {
		t.Fatal("an empty run must never reach the sink")
	}
}

// TestRecordTestRun_RevisionFromEnv: --revision is optional when AIARCH_REVISION is
// set (the venue stamps it); with NEITHER the run is refused, because a run without
// a revision can never be matched against the construction revision by the gate.
func TestRecordTestRun_RevisionFromEnv(t *testing.T) {
	dir := writeResultsIndex(t, `[{"scenario":"s1","status":"fail","durationMs":1}]`)
	noRevision := recordTestRunFlags(dir)[:10]

	t.Setenv(envRevision, "")
	err := runRecordTestRun(noRevision, func(projectstate.TestRun) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "revision") {
		t.Fatalf("want a revision error, got %v", err)
	}

	t.Setenv(envRevision, "deadbeef")
	var got projectstate.TestRun
	if err := runRecordTestRun(noRevision, func(r projectstate.TestRun) error { got = r; return nil }); err != nil {
		t.Fatal(err)
	}
	if got.Revision != "deadbeef" {
		t.Fatalf("revision must fall back to %s: %+v", envRevision, got)
	}
}

// TestRecordTestRun_RejectsUnknownStatus: a status outside pass|fail|skip is a
// malformed results.json, refused before anything is written.
func TestRecordTestRun_RejectsUnknownStatus(t *testing.T) {
	dir := writeResultsIndex(t, `[{"scenario":"s1","status":"green","durationMs":1}]`)
	err := runRecordTestRun(recordTestRunFlags(dir), func(projectstate.TestRun) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("want a status error, got %v", err)
	}
}

// TestRecordTestRun_NilSinkWritesTestingState: the nil sink is the REAL write —
// the same construction-mutation path recordTestingState uses — so the run lands on
// .testingState.testRuns of the checked-out project.json, and a second record with
// the same id REPLACES rather than duplicates.
func TestRecordTestRun_NilSinkWritesTestingState(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeConstruct, 0)
	dir := filepath.Join(s.StateRoot, "test-results")
	writeResultsIndexUnder(t, dir, `[{"scenario":"s1","status":"pass","durationMs":2}]`)
	t.Setenv(envJobMode, "")
	t.Setenv(envArtifactKind, "")
	t.Setenv(envStateRoot, s.StateRoot)
	t.Setenv(envProjectID, string(s.ProjectID))
	t.Setenv(envActivityID, "")

	for range 2 {
		if err := runRecordTestRun(recordTestRunFlags(dir), nil); err != nil {
			t.Fatalf("record-test-run: %v", err)
		}
	}
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	if proj.TestingState == nil || len(proj.TestingState.TestRuns) != 1 {
		t.Fatalf("want exactly one run on .testingState.testRuns, got %+v", proj.TestingState)
	}
	run := proj.TestingState.TestRuns[0]
	if run.ID != "1" || run.Activity != "C-BG" || run.Component != "billingManager" || len(run.Scenarios) != 1 {
		t.Fatalf("run not written as recorded: %+v", run)
	}
}

// TestOneShotDispatch_RecordTestRun: `record-test-run` is a one-shot beside
// seat-assets / step-tools — it is handled (never falls through to the MCP server)
// and its failures carry the subcommand name.
func TestOneShotDispatch_RecordTestRun(t *testing.T) {
	handled, err := runOneShotCommand([]string{"record-test-run", "--results", t.TempDir()})
	if !handled {
		t.Fatal("record-test-run must be a one-shot subcommand")
	}
	if err == nil || !strings.HasPrefix(err.Error(), "record-test-run:") {
		t.Fatalf("want a prefixed failure, got %v", err)
	}
}

// writeContractResults writes dir/<contract>/results.json with one passing scenario.
func writeContractResults(t *testing.T, dir, contract string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, contract), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[{"scenario":"` + contract + `-P1","status":"pass","durationMs":1}]`
	if err := os.WriteFile(filepath.Join(dir, contract, "results.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRecordTestRun_KebabComponentFindsTheContractResults: the venue passes the
// activity's component kebab ("billing-manager") while the scenario host writes under
// the contract name ("billingManager"); the run is found and recorded under the
// contract name (DCT plan 5 r2: a real integration run could not find its results).
func TestRecordTestRun_KebabComponentFindsTheContractResults(t *testing.T) {
	dir := writeResultsIndex(t, `[{"scenario":"s1","status":"pass","durationMs":1}]`)
	flags := recordTestRunFlags(dir)
	flags[3] = "billing-manager"
	var got []projectstate.TestRun
	if err := runRecordTestRun(flags, func(r projectstate.TestRun) error { got = append(got, r); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Component != "billingManager" || got[0].ID != "1" {
		t.Fatalf("want one run of billingManager with the run id verbatim, got %+v", got)
	}
}

// TestRecordTestRun_RecordsEveryFacetOfTheComponent: one integration runs every
// contract of its component (projectStateAccess's facets), so the real write records
// one run per facet, each id suffixed with its contract, and refuses when a facet the
// project binds a plan for has no results.
func TestRecordTestRun_RecordsEveryFacetOfTheComponent(t *testing.T) {
	p := minimalProject()
	p.ServiceContracts = map[string]projectstate.ServiceContract{
		"projectStateAccess":      {Component: "projectStateAccess"},
		"activityExecutionAccess": {Component: "projectStateAccess"},
		"reviewEngine":            {Component: "reviewEngine"},
	}
	s, _ := seedProject(t, p, jobModeConstruct, 0)
	dir := filepath.Join(s.StateRoot, "test-results")
	writeContractResults(t, dir, "projectStateAccess")
	writeContractResults(t, dir, "activityExecutionAccess")
	writeContractResults(t, dir, "reviewEngine")
	t.Setenv(envJobMode, "")
	t.Setenv(envArtifactKind, "")
	t.Setenv(envStateRoot, s.StateRoot)
	t.Setenv(envProjectID, "")
	t.Setenv(envActivityID, "")

	flags := recordTestRunFlags(dir)
	flags[1], flags[3] = "C-project-state-access", "project-state-access"
	if err := runRecordTestRun(flags, nil); err != nil {
		t.Fatalf("record-test-run: %v", err)
	}
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	if proj.ID != p.ID {
		t.Fatalf("the document's id must survive the write: %q, want %q", proj.ID, p.ID)
	}
	var ids []string
	for _, r := range proj.TestingState.TestRuns {
		ids = append(ids, r.ID+"="+r.Component+"@"+r.Activity)
	}
	want := "1-activityExecutionAccess=activityExecutionAccess@C-project-state-access 1-projectStateAccess=projectStateAccess@C-project-state-access"
	if got := strings.Join(ids, " "); got != want {
		t.Fatalf("runs = %s, want %s", got, want)
	}
}

// seedCommittedCheckout seats archistrator's own committed project.json in a
// construct checkout with no AIARCH_PROJECT_ID, and returns the checkout's
// test-results dir and the committed bytes.
func seedCommittedCheckout(t *testing.T) (string, []byte) {
	t.Helper()
	_, raw := copyCommittedProject(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, statePathPrefix), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, statePathPrefix, projectFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envJobMode, "")
	t.Setenv(envArtifactKind, "")
	t.Setenv(envStateRoot, root)
	t.Setenv(envProjectID, "")
	t.Setenv(envActivityID, "")
	return filepath.Join(root, "test-results"), raw
}

// TestRecordTestRun_ABuiltComponentNeedsItsActivity: a run names the activity whose
// integration produced it; a component the plan builds may not record without one.
func TestRecordTestRun_ABuiltComponentNeedsItsActivity(t *testing.T) {
	dir, _ := seedCommittedCheckout(t)
	writeContractResults(t, dir, "reviewEngine")
	flags := recordTestRunFlags(dir)
	flags[1], flags[3] = "", "reviewEngine"
	if err := runRecordTestRun(flags, nil); err == nil || !strings.Contains(err.Error(), `built by activity "C-review-engine"`) {
		t.Fatalf("an activity-less run of a component the plan builds must be refused, got %v", err)
	}
}

// TestRecordTestRun_KeepsTheCommittedDocument: recording onto archistrator's own
// committed project.json, with no AIARCH_PROJECT_ID (DCT plan 5 r2: the write renamed
// the project after the checkout directory and zeroed updatedAt), changes nothing but
// .testingState.testRuns. messageBus is a provided utility: the plan builds no
// activity for it, so its run carries none.
func TestRecordTestRun_KeepsTheCommittedDocument(t *testing.T) {
	dir, raw := seedCommittedCheckout(t)
	writeContractResults(t, dir, "messageBus")
	flags := recordTestRunFlags(dir)
	flags[1], flags[3] = "", "messageBus"
	if err := runRecordTestRun(flags, nil); err != nil {
		t.Fatalf("record-test-run: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(filepath.Dir(dir), statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	var before, got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &got); err != nil {
		t.Fatal(err)
	}
	for k, v := range before {
		if k != "testingState" && !bytes.Equal(v, got[k]) {
			t.Errorf("member %q changed by recording a run", k)
		}
	}
	if len(got) != len(before) {
		t.Errorf("members: %d before, %d after", len(before), len(got))
	}
	var ts struct {
		TestRuns []projectstate.TestRun `json:"testRuns"`
	}
	if err := json.Unmarshal(got["testingState"], &ts); err != nil {
		t.Fatal(err)
	}
	if last := ts.TestRuns[len(ts.TestRuns)-1]; last.Component != "messageBus" || last.Activity != "" || last.ID != "1" {
		t.Fatalf("the messageBus run must be recorded with no activity: %+v", last)
	}
}
