package systemdesign

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// Classification is derived from the Phase-2 activity-list metadata (worker class
// + coding) plus contract presence — NOT the id prefix alone (the N-* namespace
// conflates testing, infra, deployment, and documentation).
func TestConstructionRowsToContract_ClassifiesFromWorkerClass(t *testing.T) {
	rows := map[string]projectstate.ActivityConstructionStatus{
		"N-IT":    {ActivityID: "N-IT"},                                                                        // software-tester, noncoding → testing:systemTest
		"N-SC":    {ActivityID: "N-SC", Produced: []projectstate.ProducedArtifact{{Kind: "service-contract"}}}, // built a contract → service
		"N-CI":    {ActivityID: "N-CI"},                                                                        // senior-developer, noncoding → deployment
		"N-ADR":   {ActivityID: "N-ADR"},                                                                       // system-architect, noncoding → documentation
		"C-BE":    {ActivityID: "C-BE"},                                                                        // junior-developer, coding → service
		"U-SPA-1": {ActivityID: "U-SPA-1"},                                                                     // U-SPA prefix → frontend
	}
	meta := map[string]projectstate.ActivityItem{
		"N-IT":    {Name: "N-IT", WorkerClass: "software-tester", Coding: false},
		"N-SC":    {Name: "N-SC", WorkerClass: "senior-developer", Coding: false},
		"N-CI":    {Name: "N-CI", WorkerClass: "senior-developer", Coding: false},
		"N-ADR":   {Name: "N-ADR", WorkerClass: "system-architect", Coding: false},
		"C-BE":    {Name: "C-BE", WorkerClass: "junior-developer", Coding: true},
		"U-SPA-1": {Name: "U-SPA-1", WorkerClass: "junior-developer", Coding: true},
	}
	got := constructionRowsToContract(rows, meta)
	cases := []struct {
		id       string
		wantType ActivityType
		wantVar  TestingVariant
	}{
		{"N-IT", ActivityType(int(projectstate.ActivityTypeTesting)), TestingVariant(int(projectstate.TestVariantSystemTest))},
		{"N-SC", ActivityType(int(projectstate.ActivityTypeService)), 0},
		{"N-CI", ActivityType(int(projectstate.ActivityTypeDeployment)), 0},
		{"N-ADR", ActivityType(int(projectstate.ActivityTypeDocumentation)), 0},
		{"C-BE", ActivityType(int(projectstate.ActivityTypeService)), 0},
		{"U-SPA-1", ActivityType(int(projectstate.ActivityTypeFrontend)), 0},
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
