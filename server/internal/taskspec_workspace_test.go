package internal_test

import (
	"slices"
	"testing"
)

// TestTaskSpecDesignHealthEngineOwnsThePlatformRules pins that archistrator can direct a
// construction task at a declared workspace repo: the component that owns the Design
// Conformance Rules owns the platform's methodcheck tree, so its edit scope admits that tree
// (repo-qualified, never an unqualified or escaping path), its checks run in that repo's own
// module, and the project's workspace build proves the app still builds against the edit.
func TestTaskSpecDesignHealthEngineOwnsThePlatformRules(t *testing.T) {
	s := deriveSpec(t, ownProject(t), "C-design-health-engine", "construction")
	for _, p := range []string{
		"@archistrator-platform/framework-go/methodcheck/rules_uc_door.go",
		"@archistrator-platform/framework-go/methodcheck/sub/x.go",
		"server/internal/engine/designhealth/designhealth.go",
	} {
		if !scopeAdmits(s.EditScope, p) {
			t.Errorf("the edit scope of C-design-health-engine construction must admit %s: %+v", p, s.EditScope)
		}
	}
	for _, p := range []string{
		"@archistrator-platform/framework-go/arch/arch.go",
		"@archistrator-platform/method-assets/skills/x.md",
		"framework-go/methodcheck/rules_uc_door.go",
	} {
		if scopeAdmits(s.EditScope, p) {
			t.Errorf("the edit scope of C-design-health-engine construction must not admit %s: %+v", p, s.EditScope)
		}
	}
	const wsDir = "@archistrator-platform/framework-go/methodcheck"
	for _, id := range []string{"gofmt", "build", "vet", "lint", "test"} {
		c := checkByID(t, s, "command:"+id+"-ws-archistrator-platform-framework-go-methodcheck")
		if c.Source.Dir == nil || *c.Source.Dir != wsDir {
			t.Errorf("%s runs in %v, want the workspace directory %q", c.ID, c.Source.Dir, wsDir)
		}
	}
	ws := checkByID(t, s, "command:build-workspace")
	if ws.Source.Dir == nil || *ws.Source.Dir != "server" {
		t.Errorf("the workspace build runs in %v, want the module dir \"server\"", ws.Source.Dir)
	}
	if !slices.Contains(ws.Source.Argv, "GOWORK=../go.work") {
		t.Errorf("the workspace build must resolve the project's go.work: %v", ws.Source.Argv)
	}
}
