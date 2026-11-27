package main

// recordtestplan_as_test.go — pins that an arrange contractCall's `as` (the plan
// principal the call is made as) survives a test-plan write and read at the
// projectstate record level: strict decode, re-encode, and the splice the writer
// uses. methodcheck admission is deliberately not involved.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const arrangeAsRecord = `{"component":"todoListManager","principals":{"owner":{"subject":"u1","roles":["member"]}},` +
	`"bindings":[{"scenario":"s1","arrange":[{"kind":"contractCall","model":{"component":"todoListManager",` +
	`"operation":"CreateList","inputs":[],"expect":{"result":"","errorExpected":false,"errorCode":""},"as":"owner"}}],"steps":[]}]}`

// arrangeAs returns the `as` of the first binding's first arrange call.
func arrangeAs(t *testing.T, plan *projectstate.TestPlanRecord) *string {
	t.Helper()
	if plan == nil || len(plan.Bindings) != 1 || len(plan.Bindings[0].Arrange) != 1 {
		t.Fatalf("plan lost its binding or arrange call: %+v", plan)
	}
	cc, ok := plan.Bindings[0].Arrange[0].(*projectstate.ContractCall)
	if !ok {
		t.Fatalf("arrange call is %T, want *ContractCall", plan.Bindings[0].Arrange[0])
	}
	return cc.As
}

func TestRecordTestPlan_ArrangeContractCallKeepsAsAcrossWriteAndRead(t *testing.T) {
	var pl projectstate.PhaseArtifactPayload
	if err := decodeDraftStrict([]byte(`{"testPlan":`+arrangeAsRecord+`}`), &pl); err != nil {
		t.Fatalf("strict decode refused a contractCall carrying as: %v", err)
	}
	if as := arrangeAs(t, pl.TestPlan); as == nil || *as != "owner" {
		t.Fatalf("decode dropped as: %v", as)
	}
	enc, err := json.Marshal(pl.TestPlan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(enc), `"as":"owner"`) {
		t.Fatalf("re-encode dropped as: %s", enc)
	}
	var back projectstate.TestPlanRecord
	if err := json.Unmarshal(enc, &back); err != nil {
		t.Fatal(err)
	}
	if as := arrangeAs(t, &back); as == nil || *as != "owner" {
		t.Fatalf("read-back dropped as: %v", as)
	}
}

func TestRecordTestPlan_ArrangeContractCallWithoutAsOmitsIt(t *testing.T) {
	rec := strings.Replace(arrangeAsRecord, `,"as":"owner"`, "", 1)
	var pl projectstate.PhaseArtifactPayload
	if err := decodeDraftStrict([]byte(`{"testPlan":`+rec+`}`), &pl); err != nil {
		t.Fatal(err)
	}
	if as := arrangeAs(t, pl.TestPlan); as != nil {
		t.Fatalf("absent as decoded to %q", *as)
	}
	enc, err := json.Marshal(pl.TestPlan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(enc), `"as"`) {
		t.Fatalf("absent as was encoded: %s", enc)
	}
}
