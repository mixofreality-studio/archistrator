package systemdesign

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestConstructionRowsToContract_DerivesClassificationFromID(t *testing.T) {
	rows := map[string]projectstate.ActivityConstructionStatus{
		"N-IT":    {ActivityID: "N-IT"},
		"U-SPA-1": {ActivityID: "U-SPA-1"},
		"C-BE":    {ActivityID: "C-BE"},
		"N-STP":   {ActivityID: "N-STP"},
	}
	got := constructionRowsToContract(rows)
	cases := []struct {
		id       string
		wantType ActivityType
		wantVar  TestingVariant
	}{
		{"N-IT", ActivityType(int(projectstate.ActivityTypeTesting)), TestingVariant(int(projectstate.TestVariantSystemTest))},
		{"U-SPA-1", ActivityType(int(projectstate.ActivityTypeFrontend)), TestingVariant(0)},
		{"C-BE", ActivityType(int(projectstate.ActivityTypeService)), TestingVariant(0)},
		{"N-STP", ActivityType(int(projectstate.ActivityTypeTesting)), TestingVariant(int(projectstate.TestVariantPlan))},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			r, ok := got[c.id]
			if !ok {
				t.Fatalf("%s: missing from output", c.id)
			}
			if r.Type != c.wantType {
				t.Errorf("%s: Type = %d, want %d", c.id, r.Type, c.wantType)
			}
			if r.Variant != c.wantVar {
				t.Errorf("%s: Variant = %d, want %d", c.id, r.Variant, c.wantVar)
			}
		})
	}
}
