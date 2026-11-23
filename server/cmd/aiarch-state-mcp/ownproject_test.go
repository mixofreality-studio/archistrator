package main

// ownproject_test.go — archistrator's own committed state is admitted by the same loaders every
// generator and the write path apply, and holds no Method Error once the project's own rule
// waivers and the gate severity policies are applied. The conformance fixtures prove the
// recorder against the platform's projects; this proves the one project this repository keeps.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// The unchanged own project is admitted by Tier 1 under the config and module path it is built
// with, and refused under the library defaults: the two documents change the answer. This is what
// lets a contract amendment in this repository go through the recorder at all (under the defaults
// every record is refused for the messageBus constructor whatever it records). A mutant of
// AdmitArtifact that ignores either document fails here.
func TestAdmitTier1_OwnProjectAdmitsUnderItsOwnModuleDocuments(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".aiarch", "state", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(filepath.Join("..", "..", "appgen.json"))
	if err != nil {
		t.Fatal(err)
	}
	goMod, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	local := func(config, mod string) []string {
		t.Helper()
		problems, err := admitArtifact(string(raw), "serviceContract", config, mod)
		if err != nil {
			t.Fatalf("admitArtifact: %v", err)
		}
		var out []string
		for _, p := range problems {
			if p.Severity == review.AdmissionLocal {
				out = append(out, p.Consumer+": "+p.Message)
			}
		}
		return out
	}
	if got := local(string(cfg), string(goMod)); len(got) != 0 {
		t.Errorf("the own project has %d Local Tier 1 problems under its own appgen.json and go.mod:\n%s", len(got), strings.Join(got, "\n"))
	}
	if got := local("", ""); len(got) == 0 {
		t.Error("the own project admits under the library defaults: the module's documents change nothing, so a mutant that drops them passes")
	}
	if got := local("", string(goMod)); len(got) == 0 {
		t.Error("the own project admits without its appgen.json: the config is not what is under test")
	}
	if got := local(string(cfg), ""); len(got) == 0 {
		t.Error("the own project admits without its go.mod: the module path is not what is under test")
	}
}

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
	findings, err := methodcheck.ValidateProjectJSON(raw, methodcheckOptions())
	if err != nil {
		t.Fatal(err)
	}
	if errs := filterErrorFindings(applyGateSeverityPolicies(proj, 0, false, findings)); len(errs) > 0 {
		t.Fatalf("the own project carries %d Method Error(s):\n%s", len(errs), formatFindings(errs))
	}
}
