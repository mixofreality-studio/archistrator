package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// TestScenarioCompose_EmitsForOwnModel runs the platform's scenariocompose emitter over
// archistrator's own project.json with the knobs appgen.json carries (Task 2.11d). The local
// profile is model-derived and (Task 2.11e) no Manager depends on composition-root policy, so
// the generated test subject builds every Manager: zero refusals. A refusal, or an error, means
// a binding's local arm or a Manager dependency is still hook-supplied.
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
	// Task 2.11e removed the last two composition-root policy dependencies (deliveryManager's repo
	// closure and repoBase string, now the sourceControlAccess ProjectRepo verb), so the generated
	// subject builds every Manager of the own model: no refusal remains.
	for _, r := range refused {
		t.Errorf("manager left unbuilt: %s", r)
	}
}
