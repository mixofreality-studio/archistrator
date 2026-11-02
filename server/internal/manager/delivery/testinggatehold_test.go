package delivery

import (
	"strings"
	"testing"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

// Nothing projected is a hold and never a pass, so a Client that reaches this gate with no
// scenario parks instead of finishing with nothing proved.
func TestTestingGateHoldsWithNothingProjected(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   testingGateEvidence
		want string
	}{
		{"no contract key", testingGateEvidence{Contract: "web-app", Unresolved: true}, "no contractKey"},
		{"no scenario", testingGateEvidence{Contract: "webApp"}, "no scenario is projected"},
		{"no run", testingGateEvidence{Contract: "webApp", Projected: 1}, "no integration check run"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := testingGateHold(tc.ev, "attempt-1"); !strings.Contains(got, tc.want) {
				t.Errorf("got %q, want it to name %q", got, tc.want)
			}
		})
	}
}

// A machine client's lifecycle binds no test plan, so its testing review does not wait on a
// scenario run nothing would ever record. Its proof is a check of its integration task.
func TestMachineClientLifecycleHasNoScenarioGate(t *testing.T) {
	lc, ok := methodassets.LifecycleFor("machineClient")
	if !ok {
		t.Fatal("no machineClient lifecycle")
	}
	if lifecycleBindsTestPlan(lc) {
		t.Fatal("a machine client has no test plan")
	}
	for _, task := range lc.Tasks {
		if scenarioTestingGate(lc, task) {
			t.Errorf("%s must not be a scenario gate", task.ID)
		}
	}
	if service, _ := methodassets.LifecycleFor("service"); !lifecycleBindsTestPlan(service) {
		t.Error("a service keeps its plan and its gate")
	}
}
