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
