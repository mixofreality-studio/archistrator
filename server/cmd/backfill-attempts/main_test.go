package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestAttemptsFromContract_IsBackfilledWithABasis(t *testing.T) {
	got := attemptsFor("C-artifact-access", projectstate.ActivityTypeService, evidence{HasServiceContract: true, ContractRef: "artifactAccess"})

	var designReview *projectstate.TaskAttempt
	for i := range got {
		if got[i].Task == projectstate.TaskDesignReview {
			designReview = &got[i]
		}
	}
	if designReview == nil {
		t.Fatal("no designReview attempt derived from a frozen contract")
	}
	if designReview.Provenance.Origin != projectstate.OriginBackfilled {
		t.Errorf("origin = %q, want backfilled — a frozen contract is real evidence", designReview.Provenance.Origin)
	}
	if designReview.Provenance.Basis == "" {
		t.Error("backfilled attempt has no basis; Validate() would reject it")
	}
	if err := designReview.Provenance.Validate(); err != nil {
		t.Errorf("provenance invalid: %v", err)
	}
}

func TestAttemptsFor_NoEvidenceProducesNoAttempts(t *testing.T) {
	got := attemptsFor("C-nothing", projectstate.ActivityTypeService, evidence{})
	if len(got) != 0 {
		t.Errorf("attemptsFor(no evidence) = %d attempts, want 0 — absence must stay absent, not become synthesized rows", len(got))
	}
}

func TestAttemptsFor_EveryAttemptCarriesAValidProvenance(t *testing.T) {
	got := attemptsFor("C-x", projectstate.ActivityTypeService, evidence{HasServiceContract: true, ContractRef: "x", HasMergedCode: true, GitRef: "abc123"})
	for _, a := range got {
		if err := a.Provenance.Validate(); err != nil {
			t.Errorf("%s: %v", a.AttemptID, err)
		}
		if a.AttemptID != projectstate.AttemptID("C-x", a.Task, a.Attempt) {
			t.Errorf("attemptId %q does not match AttemptID()", a.AttemptID)
		}
	}
}

// wantExactTasks asserts that got is EXACTLY the named tasks, once each — no extra task,
// no missing one, no duplicate. It names the offender rather than reporting a count
// mismatch, because the failure this guards against is a task nobody meant to add.
func wantExactTasks(t *testing.T, got []projectstate.TaskAttempt, want ...projectstate.MethodTask) {
	t.Helper()
	wanted := map[projectstate.MethodTask]bool{}
	for _, task := range want {
		wanted[task] = true
	}
	seen := map[projectstate.MethodTask]int{}
	for _, a := range got {
		seen[a.Task]++
		if !wanted[a.Task] {
			t.Errorf("derived an attempt at %q (%s) — only the three ruled inferences may "+
				"produce attempts, and %q is outside every one of them", a.Task, a.AttemptID, a.Task)
		}
		if projectstate.IsConditionalTask(a.Task) {
			t.Errorf("derived an attempt at the CONDITIONAL task %q (%s) — conditional tasks are "+
				"emitted only when a real attempt exists; inventing one asserts a pre-design spike "+
				"or a test client that may never have happened", a.Task, a.AttemptID)
		}
	}
	for task := range wanted {
		switch seen[task] {
		case 1:
		case 0:
			t.Errorf("no attempt derived at %q", task)
		default:
			t.Errorf("task %q derived %d times, want exactly 1", task, seen[task])
		}
	}
	if len(got) != len(want) {
		t.Errorf("derived %d attempts, want exactly %d", len(got), len(want))
	}
}

// TestAttemptsFor_DerivesExactlyTheThreeRuledInferences is the governing rule of this
// tool expressed as a test. There are THREE inferences and no fourth:
//
//   - a frozen contract ALONE → detailed design ran, design review passed. Nothing more:
//     a design that was never built stays two tasks.
//   - merged code ALONE → construction ran, code review passed.
//   - a frozen contract AND merged code → the component is fully implemented, which the
//     founder has ruled means done, reviewed and integrated: the WHOLE profile passes,
//     minus the conditional tasks.
//
// It pins the exact task SET per bucket, not just each attempt's individual validity,
// because a fourth inference added later would be individually valid in every way the
// other tests check — inside the profile's task set, non-empty basis, stamped
// backfilled — and would sail straight through them. The tasks with no evidence and no
// ruling behind them must keep rendering as an honest unknown skeleton; a tool that
// quietly grows another inference fills that skeleton in with history nobody can trace,
// which is the exact failure this work exists to prevent.
//
// wantExactTasks additionally rejects ANY conditional task (someConstruction,
// testClient) in every bucket, so the widened case cannot start asserting a pre-design
// spike or a test client that may never have existed.
func TestAttemptsFor_DerivesExactlyTheThreeRuledInferences(t *testing.T) {
	cases := []struct {
		name string
		ev   evidence
		want []projectstate.MethodTask
	}{
		{
			name: "a frozen contract, and nothing else — NOT widened",
			ev:   evidence{HasServiceContract: true, ContractRef: "artifactAccess"},
			want: []projectstate.MethodTask{projectstate.TaskDetailedDesign, projectstate.TaskDesignReview},
		},
		{
			name: "merged code, and nothing else — NOT widened",
			ev:   evidence{HasMergedCode: true, GitRef: "implementation/log"},
			want: []projectstate.MethodTask{projectstate.TaskConstruction, projectstate.TaskCodeReview},
		},
		{
			name: "both — fully implemented, so the whole profile minus the conditionals",
			ev: evidence{
				HasServiceContract: true, ContractRef: "artifactAccess",
				HasMergedCode: true, GitRef: "implementation/log",
			},
			want: []projectstate.MethodTask{
				projectstate.TaskSRS, projectstate.TaskSRSReview,
				projectstate.TaskDetailedDesign, projectstate.TaskDesignReview,
				projectstate.TaskSTP, projectstate.TaskSTPReview,
				projectstate.TaskConstruction, projectstate.TaskCodeReview,
				projectstate.TaskIntegration, projectstate.TaskTesting,
			},
		},
		{
			name: "no evidence — absence stays absence",
			ev:   evidence{},
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantExactTasks(t, attemptsFor("C-x", projectstate.ActivityTypeService, c.ev), c.want...)
		})
	}
}

// TestAttemptsFor_WidenedIsExactlyTheProfileMinusTheConditionals derives the widened set
// from the profile rather than restating it, so the rule survives a profile change: if a
// thirteenth task joins the service profile tomorrow, the widened bucket must pick it up
// and this test keeps holding — while a hand-listed set would silently go stale.
func TestAttemptsFor_WidenedIsExactlyTheProfileMinusTheConditionals(t *testing.T) {
	for _, typ := range []projectstate.ActivityType{
		projectstate.ActivityTypeService,
		projectstate.ActivityTypeFrontend,
		projectstate.ActivityTypeDeployment,
		projectstate.ActivityTypeDocumentation,
		projectstate.ActivityTypeUIDesign,
		projectstate.ActivityTypeIntegration,
	} {
		var want []projectstate.MethodTask
		for _, task := range projectstate.TasksForProfile(projectstate.ProfileFor(typ, projectstate.TestVariantPlan)) {
			if projectstate.IsConditionalTask(task) {
				continue
			}
			want = append(want, task)
		}
		got := attemptsFor("C-x", typ, evidence{
			HasServiceContract: true, ContractRef: "artifactAccess",
			HasMergedCode: true, GitRef: "implementation/log",
		})
		wantExactTasks(t, got, want...)
	}
}

// TestAttemptsFor_WidenedBasisNamesTheRulingNotJustTheArtifacts is the honesty guard on
// the widened bucket. Most of the tasks it stamps — srs, stp, testing — have no artifact
// behind them at all; the founder's assertion is what says they happened. A basis citing
// only the artifacts would claim those rows were read off disk, which is a false
// statement about where the inference came from, and this codebase has already had to go
// back and fix one of those.
func TestAttemptsFor_WidenedBasisNamesTheRulingNotJustTheArtifacts(t *testing.T) {
	got := attemptsFor("C-AA", projectstate.ActivityTypeService, evidence{
		HasServiceContract: true, ContractRef: "artifactAccess",
		HasMergedCode: true, GitRef: "implementation/log",
	})
	if len(got) == 0 {
		t.Fatal("no attempts derived from contract+code evidence")
	}
	for _, a := range got {
		if !strings.Contains(a.Provenance.Basis, founderRuling) {
			t.Errorf("%s: basis %q does not quote the founder ruling — a basis naming only "+
				"the artifacts would be a false claim for a task with no artifact", a.AttemptID, a.Provenance.Basis)
		}
		if !strings.Contains(a.Provenance.Basis, "serviceContracts[artifactAccess]") {
			t.Errorf("%s: basis %q does not name the contract that establishes fully-implemented", a.AttemptID, a.Provenance.Basis)
		}
		if !strings.Contains(a.Provenance.Basis, "activityGit[implementation/log]") {
			t.Errorf("%s: basis %q does not name the code that establishes fully-implemented", a.AttemptID, a.Provenance.Basis)
		}
		if a.Provenance.Origin != projectstate.OriginBackfilled {
			t.Errorf("%s: origin = %q — a ruling is not an observation", a.AttemptID, a.Provenance.Origin)
		}
	}
}

// TestAttemptsFor_RulingOnlyTasksPointAtNoArtifact pins the click-through: a row that
// exists because of the ruling must not hand the UI a contract to open under a label
// like "Testing". The basis says where it came from; the evidence ref stays empty.
func TestAttemptsFor_RulingOnlyTasksPointAtNoArtifact(t *testing.T) {
	got := attemptsFor("C-AA", projectstate.ActivityTypeService, evidence{
		HasServiceContract: true, ContractRef: "artifactAccess",
		HasMergedCode: true, GitRef: "implementation/log", CodeKind: projectstate.EvidenceArtifact,
	})
	backing := map[projectstate.MethodTask]projectstate.EvidenceKind{
		projectstate.TaskDetailedDesign: projectstate.EvidenceContract,
		projectstate.TaskDesignReview:   projectstate.EvidenceContract,
		projectstate.TaskConstruction:   projectstate.EvidenceArtifact,
		projectstate.TaskCodeReview:     projectstate.EvidenceArtifact,
	}
	for _, a := range got {
		if want := backing[a.Task]; a.Evidence.Kind != want {
			t.Errorf("%s: evidence kind = %q, want %q", a.AttemptID, a.Evidence.Kind, want)
		}
		if backing[a.Task] == projectstate.EvidenceNone && a.Evidence.Ref != "" {
			t.Errorf("%s: evidence ref = %q, want empty — no artifact backs this row", a.AttemptID, a.Evidence.Ref)
		}
	}
}

// TestAttemptsFor_NeverStampsObserved guards the one origin this tool must never write:
// nothing it produces was watched happening.
func TestAttemptsFor_NeverStampsObserved(t *testing.T) {
	got := attemptsFor("C-x", projectstate.ActivityTypeService, evidence{HasServiceContract: true, ContractRef: "x", HasMergedCode: true, GitRef: "abc123"})
	if len(got) == 0 {
		t.Fatal("no attempts derived from contract+code evidence")
	}
	for _, a := range got {
		if a.Provenance.Origin != projectstate.OriginBackfilled {
			t.Errorf("%s: origin = %q, want backfilled", a.AttemptID, a.Provenance.Origin)
		}
	}
}

// TestAttemptsFor_HonoursTheProfileTaskSet checks that a type whose profile has no
// construction phase gets no construction attempts even when code evidence exists. The
// task vocabulary comes from the profile, never from the evidence.
func TestAttemptsFor_HonoursTheProfileTaskSet(t *testing.T) {
	got := attemptsFor("G-SPA", projectstate.ActivityTypeUIDesign, evidence{HasMergedCode: true, GitRef: "implementation/log"})
	for _, a := range got {
		if a.Task == projectstate.TaskConstruction || a.Task == projectstate.TaskCodeReview {
			t.Errorf("uiDesign profile has no construction phase, but %s was derived", a.AttemptID)
		}
	}
}

// TestEvidenceFromRow_ReadsOnlyProducedArtifacts pins the resolution rule: evidence
// comes from the produced list, and a row that produced neither a contract nor code
// yields nothing at all.
func TestEvidenceFromRow_ReadsOnlyProducedArtifacts(t *testing.T) {
	contracts := map[string]bool{"artifactAccess": true}

	withContract := projectstate.ActivityConstructionStatus{
		ActivityID: "C-AA",
		Produced: []projectstate.ProducedArtifact{
			{Kind: "service-contract", Source: "implementation/contracts/artifactAccess.md", Produced: true},
			{Kind: "code", Source: "implementation/log", Produced: true},
		},
	}
	ev := evidenceFromRow(withContract, contracts)
	if !ev.HasServiceContract || ev.ContractRef != "artifactAccess" {
		t.Errorf("contract evidence = %+v, want the live serviceContracts key", ev)
	}
	if ev.ContractBasis != "" {
		t.Errorf("a live contract key needs no basis override, got %q", ev.ContractBasis)
	}
	if !ev.HasMergedCode || ev.GitRef != "implementation/log" {
		t.Errorf("code evidence = %+v, want the produced source", ev)
	}

	noteOnly := projectstate.ActivityConstructionStatus{
		ActivityID: "N-QA",
		Produced: []projectstate.ProducedArtifact{
			{Kind: "note", Source: "the-method-review-routing", Produced: true},
		},
	}
	if ev := evidenceFromRow(noteOnly, contracts); ev.HasServiceContract || ev.HasMergedCode {
		t.Errorf("a note is not construction evidence, got %+v", ev)
	}
}

// TestEvidenceFromRow_UnresolvedContractGetsATruthfulBasis covers the six committed rows
// whose frozen contract file names a component that no longer has a .serviceContracts
// entry (settlementEngine, handOffEngine, …). Pointing their basis at serviceContracts[…]
// would be a dangling reference, which is exactly the class of lie this stage forbids.
func TestEvidenceFromRow_UnresolvedContractGetsATruthfulBasis(t *testing.T) {
	row := projectstate.ActivityConstructionStatus{
		ActivityID: "C-BE",
		Produced: []projectstate.ProducedArtifact{
			{Kind: "service-contract", Source: "implementation/contracts/settlementEngine.md", Produced: true},
		},
	}
	ev := evidenceFromRow(row, map[string]bool{"billingEngine": true})
	if ev.ContractBasis == "" {
		t.Fatal("an unresolved contract must carry an explicit basis, not fall back to serviceContracts[…]")
	}
	got := attemptsFor("C-BE", projectstate.ActivityTypeService, ev)
	for _, a := range got {
		if err := a.Provenance.Validate(); err != nil {
			t.Errorf("%s: %v", a.AttemptID, err)
		}
		if a.Provenance.Basis != ev.ContractBasis {
			t.Errorf("%s: basis = %q, want the override %q", a.AttemptID, a.Provenance.Basis, ev.ContractBasis)
		}
	}
}

// TestEvidenceFromRow_SourcelessArtifactNamesNoPath guards a future corpus, not today's:
// a produced entry with no source must not be described by a path it does not have.
// path.Base("") is ".", which would sail through Validate() as a non-empty basis while
// pointing at nothing.
func TestEvidenceFromRow_SourcelessArtifactNamesNoPath(t *testing.T) {
	row := projectstate.ActivityConstructionStatus{
		ActivityID: "C-XX",
		Produced: []projectstate.ProducedArtifact{
			{Kind: "service-contract", Produced: true},
			{Kind: "code", Produced: true},
		},
	}
	ev := evidenceFromRow(row, map[string]bool{".": true, "": true})
	if ev.ContractRef != "" || ev.GitRef != "" {
		t.Errorf("refs = %q/%q, want empty — there is no path to name", ev.ContractRef, ev.GitRef)
	}
	for _, basis := range []string{ev.ContractBasis, ev.CodeBasis} {
		if strings.Contains(basis, ".") && strings.Contains(basis, "=") {
			t.Errorf("basis %q invented a path for a sourceless artifact", basis)
		}
		if basis == "" {
			t.Error("basis is empty; Validate() would reject the attempt")
		}
	}
	if ev.ContractBasis != "activityConstruction[C-XX].produced[service-contract]" {
		t.Errorf("contract basis = %q", ev.ContractBasis)
	}
	for _, a := range attemptsFor("C-XX", projectstate.ActivityTypeService, ev) {
		if err := a.Provenance.Validate(); err != nil {
			t.Errorf("%s: %v", a.AttemptID, err)
		}
	}
}

// TestRenderRows_PreservesKeyOrderAndIndentation is the fidelity guard the writer relies
// on: re-rendering the untouched rows must reproduce the committed bytes exactly, so the
// only thing a real run can change is the attempts it adds.
func TestRenderRows_PreservesKeyOrderAndIndentation(t *testing.T) {
	// C-ZZ sorts after C-AA, and its buildStatus precedes its phase — neither the map
	// order nor the struct order. Both must survive a rewrite untouched.
	const src = `{
    "C-ZZ": {
      "activityID": "C-ZZ",
      "buildStatus": 2,
      "phase": 2
    },
    "C-AA": {
      "activityID": "C-AA",
      "phase": 2
    }
  }`
	order, rows, err := decodeRows([]byte(src))
	if err != nil {
		t.Fatalf("decodeRows: %v", err)
	}
	if len(order) != 2 || order[0] != "C-ZZ" {
		t.Fatalf("order = %v, want the document order [C-ZZ C-AA]", order)
	}
	out, err := renderRows(order, rows)
	if err != nil {
		t.Fatalf("renderRows: %v", err)
	}
	if string(out) != src {
		t.Errorf("re-render is not byte-identical:\n got %s\nwant %s", out, src)
	}
}

// TestWithAttempts_InsertsInPlaceWithoutDisturbingTheRecord pins where the new key
// lands and that nothing else about the record moves.
func TestWithAttempts_InsertsInPlaceWithoutDisturbingTheRecord(t *testing.T) {
	const record = `{"activityID":"C-AA","phase":2,"buildStatus":2,"produced":[]}`
	got, err := withAttempts(json.RawMessage(record), attemptsFor("C-AA", projectstate.ActivityTypeService,
		evidence{HasServiceContract: true, ContractRef: "artifactAccess"}))
	if err != nil {
		t.Fatalf("withAttempts: %v", err)
	}
	pairs, err := objectPairs(got)
	if err != nil {
		t.Fatalf("objectPairs: %v", err)
	}
	var keys []string
	for _, p := range pairs {
		keys = append(keys, p.key)
	}
	want := []string{"activityID", "phase", "attempts", "buildStatus", "produced"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", keys, want)
	}
	if !strings.Contains(string(got), `"origin":"backfilled"`) {
		t.Errorf("marshalled record dropped the origin stamp: %s", got)
	}
}

// TestWithAttempts_ReplacesAnExistingLedger keeps a second run from shadowing the first
// with a duplicate key instead of replacing it.
func TestWithAttempts_ReplacesAnExistingLedger(t *testing.T) {
	const record = `{"activityID":"C-AA","phase":2,"attempts":[],"buildStatus":2}`
	got, err := withAttempts(json.RawMessage(record), attemptsFor("C-AA", projectstate.ActivityTypeService,
		evidence{HasServiceContract: true, ContractRef: "artifactAccess"}))
	if err != nil {
		t.Fatalf("withAttempts: %v", err)
	}
	pairs, err := objectPairs(got)
	if err != nil {
		t.Fatalf("objectPairs: %v", err)
	}
	seen := 0
	for _, p := range pairs {
		if p.key == "attempts" {
			seen++
			if string(p.value) == "[]" {
				t.Error("attempts was not replaced with the derived ledger")
			}
		}
	}
	if seen != 1 {
		t.Errorf("attempts appears %d times, want exactly 1", seen)
	}
}
