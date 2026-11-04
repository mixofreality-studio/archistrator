package main

// run011_mission_test.go replays RUN-011's mission draft (trace 2521c8d78dc777b29a378854ab9827ee):
// its agent sent every objective as {"text": ...}, where an Objective is {number, statement}. A
// lenient decode dropped "text", and 7 objectives of {number: 0, statement: ""} were committed
// with a reply that the draft had passed the codec and the Method rules.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestPutDraftModelRefusesAnUnknownField(t *testing.T) {
	draft, err := os.ReadFile(filepath.Join("testdata", "fresh", "run011.mission-draft.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindMission)
	err = s.putDraftModel(draft)
	if err == nil || !strings.Contains(err.Error(), `"text"`) || !strings.Contains(err.Error(), "call putDraftModel again") {
		t.Fatalf("RUN-011's mission draft must be refused, naming the field: %v", err)
	}
	if s.wroteState {
		t.Fatal("a refused draft writes nothing")
	}
	if slot := readBackSlot(t, s, projectstate.KindMission); slot.Status != projectstate.ReviewNone || slot.Model != nil {
		t.Fatalf("the mission slot was written: %+v", slot)
	}

	fixed := strings.ReplaceAll(string(draft), `{"text":`, `{"statement":`)
	if err := s.putDraftModel([]byte(fixed)); err != nil {
		t.Fatalf("the same draft with its objectives named as the schema names them: %v", err)
	}
}

// Trailing data after the model is refused, as json.Unmarshal refused it.
func TestPutDraftModelRefusesTrailingData(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	model := `{"items":[{"name":"Pricing model","rationale":"Pricing changes over time.","axis":"sameCustomerOverTime"}]} {}`
	if err := s.putDraftModel([]byte(model)); err == nil {
		t.Fatal("a model followed by more JSON must be refused")
	}
	if s.wroteState {
		t.Fatal("a refused draft writes nothing")
	}
}
