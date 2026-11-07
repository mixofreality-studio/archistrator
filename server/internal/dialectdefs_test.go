package internal_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// TestDialectDefsAreProjectStateAccessDefs holds the one web-dialect $defs document
// (uidialect.defs.json, embedded in projectstate) equal to the copy projectStateAccess carries for
// its own record verbs and to the copy every browser Client contract carries. The copies are
// authored separately (the contracts are data, the document is embedded), so only this test keeps
// them from drifting.
func TestDialectDefsAreProjectStateAccessDefs(t *testing.T) {
	var dialect map[string]any
	if err := json.Unmarshal(projectstate.BrowserDialectDefs(), &dialect); err != nil {
		t.Fatalf("uidialect.defs.json: %v", err)
	}
	if len(dialect) == 0 {
		t.Fatal("uidialect.defs.json carries no $defs")
	}
	p := ownProject(t)
	browsers := 0
	for key, sc := range p.ServiceContracts {
		_, speaksDialect := sc.Defs["UIAction"]
		if key != "projectStateAccess" && !speaksDialect {
			continue
		}
		if key != "projectStateAccess" {
			browsers++
		}
		for name, want := range dialect {
			raw, ok := sc.Defs[name]
			if !ok {
				t.Errorf("%s: $defs lacks the dialect definition %s", key, name)
				continue
			}
			var got any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Errorf("%s: $defs.%s is not JSON: %v", key, name, err)
				continue
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: $defs.%s differs from uidialect.defs.json", key, name)
			}
		}
	}
	if browsers == 0 {
		t.Error("no browser contract carries the dialect: the test would hold nothing to the document")
	}
}
