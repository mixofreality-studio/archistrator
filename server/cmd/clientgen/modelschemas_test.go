package main

import (
	"strings"
	"testing"
)

// the 14 distinct slot-model root variants (17 artifact kinds; the 4 Solution
// kinds share *Solution).
var wantRootSchemas = []string{
	"ModelMissionStatement",
	"ModelGlossary",
	"ModelScrubbedRequirements",
	"ModelVolatilities",
	"ModelCoreUseCases",
	"ModelSystem",
	"ModelOperationalConcepts",
	"ModelStandardCheck",
	"ModelPlanningAssumptions",
	"ModelActivityList",
	"ModelNetwork",
	"ModelSolution",
	"ModelRiskModel",
	"ModelSdpReview",
}

func TestModelComponentSchemasRoots(t *testing.T) {
	block, err := modelComponentSchemas()
	if err != nil {
		t.Fatalf("modelComponentSchemas: %v", err)
	}
	if len(wantRootSchemas) != 14 {
		t.Fatalf("test fixture wrong: expected 14 roots, listed %d", len(wantRootSchemas))
	}
	for _, name := range wantRootSchemas {
		def, ok := block[name]
		if !ok {
			t.Errorf("missing root schema %q", name)
			continue
		}
		m, ok := def.(map[string]any)
		if !ok || m["type"] != "object" {
			t.Errorf("root schema %q is not an object schema: %#v", name, def)
		}
	}
}

// TestModelComponentSchemasRecursion asserts a self-recursive type resolves to an
// intra-namespace $ref (DeploymentNode.children → items → #/…/ModelDeploymentNode)
// rather than erroring or inlining infinitely.
func TestModelComponentSchemasRecursion(t *testing.T) {
	block, err := modelComponentSchemas()
	if err != nil {
		t.Fatalf("modelComponentSchemas: %v", err)
	}
	node, ok := block["ModelDeploymentNode"].(map[string]any)
	if !ok {
		t.Fatal("ModelDeploymentNode not present — recursion closure missed it")
	}
	props, ok := node["properties"].(map[string]any)
	if !ok {
		t.Fatal("ModelDeploymentNode has no properties")
	}
	children, ok := props["children"].(map[string]any)
	if !ok {
		t.Fatalf("ModelDeploymentNode.children missing: %#v", props)
	}
	items, ok := children["items"].(map[string]any)
	if !ok {
		t.Fatalf("ModelDeploymentNode.children has no items: %#v", children)
	}
	if got := items["$ref"]; got != modelRef("DeploymentNode") {
		t.Errorf("self-ref not intra-namespace: got %v, want %s", got, modelRef("DeploymentNode"))
	}
}

// TestModelComponentSchemasNoGoExtensions asserts the reflected block carries no
// x-go-* codegen metadata (TS codegen input, not Go-regen source).
func TestModelComponentSchemasNoGoExtensions(t *testing.T) {
	block, err := modelComponentSchemas()
	if err != nil {
		t.Fatalf("modelComponentSchemas: %v", err)
	}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch n := v.(type) {
		case map[string]any:
			for k, val := range n {
				if strings.HasPrefix(k, "x-go-") {
					t.Errorf("x-go-* key %q found at %s", k, path)
				}
				walk(path+"/"+k, val)
			}
		case []any:
			for i, item := range n {
				walk(path+"/"+itoa(i), item)
			}
		}
	}
	for name, def := range block {
		walk(name, def)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
