package main

// bindingmodel_test.go — FPC plan 2 Task 2.11: the app's side of binding model v2. A rule waiver
// downgrades exactly the rule it names until it expires (ruling 8); a UI design is a typed record
// of screens and elements, never prose alone; a recorded contract says which of its operations only
// observe, so a probe can be held to one.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestWaiver_DowngradesNamedRuleUntilExpiry(t *testing.T) {
	proj := minimalProject()
	proj.RuleWaivers = []projectstate.RuleWaiver{
		{Rule: "TP-PROBE", Reason: "ruling 8: own-project rebinding scheduled", Expires: "2026-12-31"},
		{Rule: "TP-STATE", Reason: "no expiry given", Expires: "someday"},
	}
	findings := func() []methodcheck.Finding {
		return []methodcheck.Finding{
			{RuleID: "TP-PROBE", Severity: methodcheck.SeverityError, Message: "probe"},
			{RuleID: "TP-STATE", Severity: methodcheck.SeverityError, Message: "state"},
			{RuleID: "TP-HOOK", Severity: methodcheck.SeverityError, Message: "hook"},
		}
	}
	at := func(day string) func() time.Time {
		return func() time.Time {
			d, err := time.Parse("2006-01-02", day)
			if err != nil {
				t.Fatal(err)
			}
			return d
		}
	}
	defer func(prev func() time.Time) { waiverToday = prev }(waiverToday)

	for _, day := range []string{"2026-11-06", "2026-12-31"} {
		waiverToday = at(day)
		got := applyGateSeverityPolicies(proj, 0, false, findings())
		if got[0].Severity != methodcheck.SeverityWarning || !strings.Contains(got[0].Message, "ruling 8") {
			t.Errorf("%s: a waived rule must downgrade and say why, got %v %q", day, got[0].Severity, got[0].Message)
		}
		if got[1].Severity != methodcheck.SeverityError {
			t.Errorf("%s: a waiver with no readable expiry must not downgrade, got %v", day, got[1].Severity)
		}
		if got[2].Severity != methodcheck.SeverityError {
			t.Errorf("%s: a rule no waiver names must keep its severity, got %v", day, got[2].Severity)
		}
	}

	waiverToday = at("2027-01-01")
	got := applyGateSeverityPolicies(proj, 0, false, findings())
	if got[0].Severity != methodcheck.SeverityError {
		t.Errorf("an expired waiver must not downgrade, got %v", got[0].Severity)
	}
}

// uiDesignSession is a construct session on the web-app surface whose contract has not been derived
// yet, as TestRecordingAUIDesignDerivesTheSurfaceContract builds it.
func uiDesignSession(t *testing.T) *Session {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(composedState(t), &top); err != nil {
		t.Fatal(err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
		t.Fatal(err)
	}
	delete(contracts, "webApp")
	raw, err := json.Marshal(contracts)
	if err != nil {
		t.Fatal(err)
	}
	top["serviceContracts"] = raw
	state, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(state, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := seedProject(t, proj, jobModeConstruct, 0)
	s.ActivityID, s.ComponentID = freshSurfaceActivity, "web-app"
	return s
}

func TestRecordUIDesign_TypedRefusesNoScreens(t *testing.T) {
	screen := map[string]any{"id": "list", "route": "#/", "elements": []any{
		map[string]any{"id": "new-todo", "role": "textbox", "name": "What needs to be done?", "repeats": false},
		map[string]any{"id": "todo-list", "role": "list", "name": "", "repeats": false},
		map[string]any{"id": "todo-item", "role": "listitem", "name": "Todo", "within": "todo-list", "repeats": true},
		map[string]any{"id": "todo-toggle", "role": "checkbox", "name": "Toggle todo", "within": "todo-item", "repeats": true},
	}}
	design := func(mutate func(map[string]any)) map[string]any {
		d := map[string]any{"surface": "web-app", "content": "the concept", "screens": []any{screen}}
		if mutate != nil {
			mutate(d)
		}
		return map[string]any{"UIDesign": d}
	}

	for name, tc := range map[string]struct {
		payload map[string]any
		want    string
	}{
		"no screens at all":    {design(func(d map[string]any) { delete(d, "screens") }), "screens"},
		"an empty screen list": {design(func(d map[string]any) { d["screens"] = []any{} }), "screens"},
		"prose only":           {map[string]any{"UIDesign": map[string]any{"surface": "web-app", "content": "the concept"}}, "screens"},
		"a screen with an empty elements array": {design(func(d map[string]any) {
			d["screens"] = []any{map[string]any{"id": "list", "route": "#/", "elements": []any{}}}
		}), "elements"},
		"a screen with no elements field": {design(func(d map[string]any) {
			d["screens"] = []any{map[string]any{"id": "list", "route": "#/"}}
		}), "elements"},
		"a role outside the closed set": {design(func(d map[string]any) {
			d["screens"] = []any{map[string]any{"id": "list", "route": "#/", "elements": []any{
				map[string]any{"id": "go", "role": "clickable", "name": "Go", "repeats": false}}}}
		}), "role"},
		"a field the element does not declare": {design(func(d map[string]any) {
			d["screens"] = []any{map[string]any{"id": "list", "route": "#/", "elements": []any{
				map[string]any{"id": "go", "role": "button", "name": "Go", "repeats": false, "testid": "go"}}}}
		}), "testid"},
	} {
		t.Run(name, func(t *testing.T) {
			s := uiDesignSession(t)
			err := s.recordPhaseArtifact("web-app", tc.payload)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want a refusal naming %q, got: %v", tc.want, err)
			}
			if s.wroteState {
				t.Fatal("a refused design must write nothing")
			}
		})
	}

	s := uiDesignSession(t)
	if err := s.recordPhaseArtifact("web-app", design(nil)); err != nil {
		t.Fatalf("a typed design must record: %v", err)
	}
	got, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	rec := got.PhaseArtifacts.UIDesign["web-app"]
	if len(rec.Screens) != 1 || len(rec.Screens[0].Elements) != 4 {
		t.Fatalf("recorded design = %+v", rec)
	}
	if e := rec.Screens[0].Elements[3]; e.Role != projectstate.UIRoleCheckbox || e.Within == nil || *e.Within != "todo-item" || !e.Repeats {
		t.Errorf("element did not round-trip: %+v", e)
	}
	requireDerivedShape(t, got.ServiceContracts["webApp"])
}

func TestRecordServiceContract_SetsReadOnlyFromVerb(t *testing.T) {
	// Through the verb: the bus records with its one write operation marked as a write.
	s := run016Session(t)
	if err := s.recordServiceContract(run016Contract(t)); err != nil {
		t.Fatal(err)
	}
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	ops := proj.ServiceContracts["messageBus"].Interface.Operations
	if len(ops) == 0 || ops[0].Name != "Publish" || ops[0].ReadOnly {
		t.Fatalf("Publish records as a write, got %+v", ops)
	}

	// The classification itself, per verb prefix and layer.
	sc := projectstate.ServiceContract{Layer: "ResourceAccess"}
	for _, name := range []string{"GetCursor", "ReadLog", "ListBacklog", "QueryDepth", "ObserveLag", "RetrieveRow", "FetchPage", "Publish", "RecordAck"} {
		sc.Interface.Operations = append(sc.Interface.Operations, projectstate.ContractOperation{Name: name, ReadOnly: name == "Publish"})
	}
	markReadOnlyOperations(&sc)
	for _, o := range sc.Interface.Operations {
		want := o.Name != "Publish" && o.Name != "RecordAck"
		if o.ReadOnly != want {
			t.Errorf("%s: readOnly = %v, want %v", o.Name, o.ReadOnly, want)
		}
	}
	engine := projectstate.ServiceContract{Layer: "Engine", Interface: projectstate.ContractInterface{Operations: []projectstate.ContractOperation{{Name: "Decide"}}}}
	markReadOnlyOperations(&engine)
	if !engine.Interface.Operations[0].ReadOnly {
		t.Error("an Engine only computes: every operation observes")
	}
	if projectstate.OperationReadOnly("ResourceAccess", "Store") || !projectstate.OperationReadOnly("Engine", "Store") {
		t.Error("an Engine only computes; a ResourceAccess without a read verb writes")
	}
}
