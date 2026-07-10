package systemdesign

// layerdegenerate_test.go — coverage for the app-side SYSTEM-LAYER-DEGENERATE gate the
// sessionState read-back applies to a System draft (F81). A layer-degenerate system
// (zero Managers / zero ResourceAccess, or a component whose name stereotype contradicts
// its layer) surfaces as ERROR findings on the review panel. This is the review-panel
// twin of methodcheck's SYSTEM-LAYER-DEGENERATE.

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func comp(id, name string, kind projectstate.ComponentKind, layer projectstate.Layer) projectstate.Component {
	return projectstate.Component{ID: id, Name: name, Kind: kind, Layer: layer}
}

// A healthy system with a Manager and a ResourceAccess and consistent names raises no
// degeneracy finding.
func Test_systemLayerDegenerate_HealthySystemClean(t *testing.T) {
	sys := &projectstate.System{Components: []projectstate.Component{
		comp("c", "WebClient", projectstate.CompClient, projectstate.LayerClient),
		comp("m", "OrderManager", projectstate.CompManager, projectstate.LayerManager),
		comp("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess),
	}}
	if f := systemLayerDegenerateFindings(KindSystem, sys); len(f) != 0 {
		t.Fatalf("healthy system should be clean, got: %+v", f)
	}
}

// The live F81 corruption: every component defaulted to client (kind+layer both omitted).
// Zero Managers AND zero ResourceAccess AND every stereotyped name contradicts client.
func Test_systemLayerDegenerate_AllClientFlagged(t *testing.T) {
	sys := &projectstate.System{Components: []projectstate.Component{
		comp("m", "OrderManager", projectstate.CompClient, projectstate.LayerClient),
		comp("e", "PricingEngine", projectstate.CompClient, projectstate.LayerClient),
		comp("ra", "OrderAccess", projectstate.CompClient, projectstate.LayerClient),
	}}
	f := systemLayerDegenerateFindings(KindSystem, sys)
	if len(f) == 0 {
		t.Fatal("an all-client system must be flagged")
	}
	var zeroMgr, zeroRA, nameMismatch int
	for _, fi := range f {
		if fi.RuleID != "SYSTEM-LAYER-DEGENERATE" {
			t.Fatalf("unexpected rule id %q", fi.RuleID)
		}
		switch {
		case strings.Contains(fi.Message, "zero Managers"):
			zeroMgr++
		case strings.Contains(fi.Message, "zero ResourceAccess"):
			zeroRA++
		case strings.Contains(fi.Message, "ends in"):
			nameMismatch++
		}
	}
	if zeroMgr != 1 || zeroRA != 1 {
		t.Fatalf("expected one zero-managers and one zero-resourceAccess finding, got mgr=%d ra=%d", zeroMgr, zeroRA)
	}
	if nameMismatch != 3 {
		t.Fatalf("expected 3 name/layer mismatch findings (Manager, Engine, Access), got %d", nameMismatch)
	}
}

// Zero managers alone (has RA) still trips the structure rule.
func Test_systemLayerDegenerate_ZeroManagers(t *testing.T) {
	sys := &projectstate.System{Components: []projectstate.Component{
		comp("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess),
	}}
	f := systemLayerDegenerateFindings(KindSystem, sys)
	if len(f) != 1 || !strings.Contains(f[0].Message, "zero Managers") {
		t.Fatalf("expected exactly the zero-managers finding, got: %+v", f)
	}
}

// A single name/layer contradiction with otherwise-healthy structure trips only the
// name rule.
func Test_systemLayerDegenerate_NameLayerMismatch(t *testing.T) {
	sys := &projectstate.System{Components: []projectstate.Component{
		comp("m", "OrderManager", projectstate.CompManager, projectstate.LayerManager),
		comp("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess),
		// A component named "…Engine" but sitting in the client layer.
		comp("e", "PricingEngine", projectstate.CompEngine, projectstate.LayerClient),
	}}
	f := systemLayerDegenerateFindings(KindSystem, sys)
	if len(f) != 1 {
		t.Fatalf("expected exactly one finding, got: %+v", f)
	}
	if !strings.Contains(f[0].Message, "PricingEngine") || !strings.Contains(f[0].Message, "engine") {
		t.Fatalf("finding should name the offending component and its expected layer, got: %v", f[0].Message)
	}
}

// A "…Store"/"…Resource" name implies the resource layer.
func Test_systemLayerDegenerate_StoreImpliesResource(t *testing.T) {
	if want, suffix, mismatch := nameLayerMismatch("EventStore", projectstate.LayerClient); !mismatch || want != projectstate.LayerResource || suffix != "Store" {
		t.Fatalf("EventStore in client layer should mismatch to resource, got want=%v suffix=%q mismatch=%v", want, suffix, mismatch)
	}
	if _, _, mismatch := nameLayerMismatch("EventStore", projectstate.LayerResource); mismatch {
		t.Fatal("EventStore in resource layer should be consistent")
	}
}

// A name with no recognized stereotype suffix never mismatches.
func Test_systemLayerDegenerate_UnstereotypedNameOK(t *testing.T) {
	if _, _, mismatch := nameLayerMismatch("Utilities", projectstate.LayerUtility); mismatch {
		t.Fatal("an unstereotyped name must not mismatch")
	}
}

// The rule is inert for non-System artifacts.
func Test_systemLayerDegenerate_NonSystemInert(t *testing.T) {
	if f := systemLayerDegenerateFindings(KindCoreUseCases, &projectstate.CoreUseCases{}); f != nil {
		t.Fatalf("rule must be inert for non-System artifacts, got: %+v", f)
	}
}
