package main

// ownproject_test.go — archistrator's own committed state is admitted by the same loaders every
// generator and the write path apply, and holds no Method Error once the project's own rule
// waivers and the gate severity policies are applied. The conformance fixtures prove the
// recorder against the platform's projects; this proves the one project this repository keeps.

import (
	"os"
	"path/filepath"
	"testing"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestAdmit_PassesOwnProject(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".aiarch", "state", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projectmodel.Load(raw); err != nil {
		t.Fatalf("projectmodel.Load refuses the own project:\n%v", err)
	}
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(raw, anyProject)
	if err != nil {
		t.Fatalf("the server codec refuses the own project: %v", err)
	}
	mp, ok, err := methodcheck.DecodeProject(raw)
	if err != nil || !ok {
		t.Fatalf("methodcheck.DecodeProject: ok=%v err=%v", ok, err)
	}
	if _, err := methodcheck.DeriveScenarios(mp); err != nil {
		t.Fatalf("methodcheck.DeriveScenarios: %v", err)
	}
	findings, err := methodcheck.ValidateProjectJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if errs := filterErrorFindings(applyGateSeverityPolicies(proj, 0, false, findings)); len(errs) > 0 {
		t.Fatalf("the own project carries %d Method Error(s):\n%s", len(errs), formatFindings(errs))
	}
}
