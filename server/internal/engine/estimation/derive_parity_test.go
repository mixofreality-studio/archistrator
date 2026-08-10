package estimation

import (
	"encoding/json"
	"os"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// loadSystemFixture reads the frozen slim view of the live committed System (37
// components) with the Task-7 typed attributes stamped on.
func loadSystemFixture(t *testing.T) SystemView {
	t.Helper()
	raw, err := os.ReadFile("testdata/system_view.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var sv SystemView
	if err := json.Unmarshal(raw, &sv); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(sv.Components) != 37 {
		t.Fatalf("fixture has %d components, want the live 37", len(sv.Components))
	}
	return sv
}

func parityPlan(t *testing.T) DerivedPlan {
	t.Helper()
	plan, err := NewEstimationEngine().DerivePlan(fweng.Context{}, loadSystemFixture(t), ActivityListDeltas{})
	if err != nil {
		t.Fatalf("DerivePlan over the live System: %v", err)
	}
	return plan
}

func parityNames(plan DerivedPlan) map[string]bool {
	m := make(map[string]bool, len(plan.Activities))
	for _, a := range plan.Activities {
		m[a.Name] = true
	}
	return m
}

// Correction 1 — the three zombie activities (C-HE / C-WIA / R-WIT in the committed
// plan, all Done+Integrated against components that no longer exist) must not appear.
//
// HONEST SCOPE: this test cannot prove the deriver "excludes" them, because there is no
// exclusion branch to exercise — deriveActivities emits C-*/R-* names ONLY by iterating
// system.Components, so a component that does not exist can never produce an activity.
// Corrections 2 and 3 below DO have real guard branches (constructionProfile ==
// "generated", provisioning != "vendor") and their tests genuinely exercise them.
//
// What this test is actually worth: (a) a fixture-staleness tripwire, failing if a
// future re-extraction reintroduces one of these three components, and (b) a structural
// tripwire against any regression that sourced activity names from somewhere other than
// system.Components. The real evidence for Correction 1 is the derived-vs-committed
// diff in the task report, not this assertion in isolation.
func TestParityDropsTheZombieActivities(t *testing.T) {
	got := parityNames(parityPlan(t))
	for _, zombie := range []string{"C-hand-off-engine", "C-work-item-access", "R-work-item-tracker"} {
		if got[zombie] {
			t.Errorf("derived the zombie activity %q", zombie)
		}
	}
}

// Correction 2: the generated transport tier gets no coding activity. C-CW, C-CM and
// C-CS are committed today in violation of standing doctrine.
func TestParityDropsGeneratedClientCodingActivities(t *testing.T) {
	got := parityNames(parityPlan(t))
	for _, c := range []string{"C-web-client", "C-mcp-client", "C-scheduler-client"} {
		if got[c] {
			t.Errorf("derived %q for a generated-transport client", c)
		}
	}
}

// Correction 3: R-* only for vendor resources. The four owned stores get none.
func TestParityEmitsProvisioningOnlyForVendorResources(t *testing.T) {
	got := parityNames(parityPlan(t))
	for _, want := range []string{"R-github", "R-merchant-gateway", "R-construction-pipeline-runtime", "R-operated-runtime"} {
		if !got[want] {
			t.Errorf("missing vendor provisioning activity %q", want)
		}
	}
	for _, unwanted := range []string{"R-project-git-repo", "R-operated-system-state", "R-billing-state", "R-usage-log"} {
		if got[unwanted] {
			t.Errorf("derived %q for an OWNED store; its work rides additive noncoding", unwanted)
		}
	}
}

// Correction 4: one U-SPA per manager. Five managers, five activities, plus scaffold.
func TestParityEmitsOneSPAActivityPerManager(t *testing.T) {
	got := parityNames(parityPlan(t))
	for _, m := range []string{"system-design-manager", "project-design-manager", "construction-manager", "operations-manager", "billing-manager"} {
		if !got["U-SPA-"+m] {
			t.Errorf("missing U-SPA-%s", m)
		}
	}
	if !got["U-SPA-S"] || !got["G-SPA"] {
		t.Error("missing the always-emit scaffold / UI-design activities")
	}
}

// Every code-layer component that is not generated transport must be covered exactly
// once. This is the invariant that ACT-COMPONENT-COVERAGE used to enforce as a gate and
// that derivation now makes true by construction.
func TestParityCoversEveryHandwrittenCodeComponentExactlyOnce(t *testing.T) {
	sys := loadSystemFixture(t)
	plan := parityPlan(t)
	count := map[string]int{}
	for _, a := range plan.Activities {
		if a.Coding && a.ComponentID != "" && len(a.Name) > 2 && a.Name[:2] == "C-" {
			count[a.ComponentID]++
		}
	}
	for _, c := range sys.Components {
		if !isCodeLayer(c.Kind) || c.ConstructionProfile == "generated" {
			continue
		}
		if count[c.ID] != 1 {
			t.Errorf("component %s has %d coding activities, want exactly 1", c.ID, count[c.ID])
		}
	}
}

// No dependency edge may name an activity that does not exist — a dangling predecessor
// silently corrupts the CPM solve (it contributes a zero-duration phantom node).
func TestParityHasNoDanglingDependencyEdges(t *testing.T) {
	plan := parityPlan(t)
	known := parityNames(plan)
	for _, m := range plan.Milestones {
		known[m.Id] = true
	}
	for _, d := range plan.Dependencies {
		if !known[d.Activity] {
			t.Errorf("dependency row for unknown activity %q", d.Activity)
		}
		for _, p := range d.DependsOn {
			if !known[p] {
				t.Errorf("activity %q depends on unknown %q", d.Activity, p)
			}
		}
	}
}

// The derived plan must feed the EXISTING CPM solve without adaptation — that is the
// point of deriving into the same shapes ComputeNetwork already consumes.
func TestParityPlanSolvesThroughComputeNetwork(t *testing.T) {
	plan := parityPlan(t)
	items := make([]ActivityItem, 0, len(plan.Activities))
	for _, a := range plan.Activities {
		items = append(items, ActivityItem{Name: a.Name, EffortDays: a.EffortDays})
	}
	sol, err := NewEstimationEngine().ComputeNetwork(fweng.Context{},
		ActivityList{Activities: items},
		Network{Dependencies: plan.Dependencies, Milestones: plan.Milestones})
	if err != nil {
		t.Fatalf("ComputeNetwork over the derived plan: %v", err)
	}
	if len(sol.Nodes) == 0 {
		t.Fatal("ComputeNetwork produced no nodes for the derived plan")
	}
	if sol.Summary.TotalDurationDays <= 0 {
		t.Errorf("derived plan solves to a non-positive duration %v", sol.Summary.TotalDurationDays)
	}
	if sol.Summary.CriticalPathActivityCount == 0 {
		t.Error("derived plan has no critical path; the edge derivation produced a disconnected graph")
	}
}
