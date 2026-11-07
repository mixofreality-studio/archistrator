package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// TestScenarioCompose_EmitsForOwnModel runs the platform's scenariocompose emitter over
// archistrator's own project.json with the knobs appgen.json carries (Task 2.11d). The local
// profile is model-derived, so the only Manager the generated test subject may leave unbuilt is
// deliveryManager, and only for its two composition-root policy dependencies repo and repoBase
// (Task 2.11e moves them behind sourceControlAccess). A refusal of anything else, or an error,
// means a binding's local arm is still hook-supplied.
func TestScenarioCompose_EmitsForOwnModel(t *testing.T) {
	m, err := projectmodel.LoadFile("../../../.aiarch/state/project.json")
	if err != nil {
		t.Fatalf("load own project.json: %v", err)
	}
	raw, err := os.ReadFile("../../appgen.json")
	if err != nil {
		t.Fatal(err)
	}
	var knobs struct {
		Container                  string                              `json:"container"`
		EnvPrefix                  string                              `json:"envPrefix"`
		WebExposedManagers         []string                            `json:"webExposedManagers"`
		VariantHookArgs            map[string][]composegen.HookArgType `json:"variantHookArgs"`
		VariantConstructorNoError  map[string]bool                     `json:"variantConstructorNoError"`
		ScheduleRegistrarComponent string                              `json:"scheduleRegistrarComponent"`
	}
	if err := json.Unmarshal(raw, &knobs); err != nil {
		t.Fatal(err)
	}
	effective := *m
	effective.Deployment = m.EffectiveDeployment(knobs.Container)
	hooks := knobs.VariantHookArgs
	external, returnsError := composegen.ExternalHTTPArgs(&effective)
	for key, args := range external {
		if _, set := hooks[key]; !set {
			hooks[key] = args
		}
	}
	cc := composegen.Config{
		ContainerKey: knobs.Container, ModulePath: "github.com/mixofreality-studio/archistrator/server",
		PackageName: "main", EnvPrefix: knobs.EnvPrefix, WebExposedManagers: knobs.WebExposedManagers,
		VariantHookArgs: hooks, VariantConstructorNoError: knobs.VariantConstructorNoError,
		VariantConstructorReturnsError: returnsError, ScheduleRegistrarComponent: knobs.ScheduleRegistrarComponent,
	}
	src, refused, err := composegen.EmitScenarioCompose(&effective, cc)
	if err != nil {
		t.Fatalf("EmitScenarioCompose over the own model: %v", err)
	}
	if !strings.Contains(string(src), "package scenariocompose") {
		t.Errorf("the emitted subject is not package scenariocompose")
	}
	// The emitter reports one refusal per Manager: the first policy dependency it meets, which is
	// repo (declared before repoBase). Both belong to deliveryManager until Task 2.11e removes them.
	var got []string
	for _, r := range refused {
		if r.Manager != "deliveryManager" || (r.Dependency != "repo" && r.Dependency != "repoBase") {
			t.Errorf("unexpected refusal %s: only deliveryManager's repo/repoBase may remain", r)
		}
		got = append(got, r.Manager+"/"+r.Dependency)
	}
	sort.Strings(got)
	if len(got) != 1 {
		t.Errorf("refusals = %v, want exactly one (deliveryManager)", got)
	}
}
