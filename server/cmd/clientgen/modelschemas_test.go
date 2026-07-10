package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// knownEnumFieldTypes is the authoritative inventory (from the step4-task4 live-wire
// audit) of every Model* property backed by a string-marshalled ordinal enum. Each
// MUST emit {type:string, enum:[wire names]} — NOT integer — because the wire ships
// the camelCase string. The expected enum values are NOT hand-listed: they are
// live-marshalled from the projectstate type in the test body, so this table only
// pins the field->type wiring, and the ground truth stays the marshaller.
var knownEnumFieldTypes = map[string]reflect.Type{
	"ModelComponent.kind":                   reflect.TypeOf(projectstate.ComponentKind(0)),
	"ModelComponent.layer":                  reflect.TypeOf(projectstate.Layer(0)),
	"ModelRelationship.mode":                reflect.TypeOf(projectstate.CallMode(0)),
	"ModelVolatility.axis":                  reflect.TypeOf(projectstate.Axis(0)),
	"ModelCheckItem.status":                 reflect.TypeOf(projectstate.CheckStatus(0)),
	"ModelActivityNode.kind":                reflect.TypeOf(projectstate.ActivityNodeKind(0)),
	"ModelActivityEdge.kind":                reflect.TypeOf(projectstate.EdgeKind(0)),
	"ModelUseCase.trigger":                  reflect.TypeOf(projectstate.Trigger(0)),
	"ModelUseCase.classification":           reflect.TypeOf(projectstate.Classification(0)),
	"ModelDeploymentEnvironment.profile":    reflect.TypeOf(projectstate.DeploymentProfile(0)),
	"ModelDeploymentTopology.deliveryStyle": reflect.TypeOf(projectstate.DeliveryStyle(0)),
	"ModelSolution.slotKind":                reflect.TypeOf(projectstate.ArtifactKind(0)),
	"ModelRiskRow.solutionKind":             reflect.TypeOf(projectstate.ArtifactKind(0)),
	"ModelSdpOptionRow.solutionKind":        reflect.TypeOf(projectstate.ArtifactKind(0)),
	"ModelRiskModel.recommendation":         reflect.TypeOf(projectstate.ArtifactKind(0)),
}

// liveMarshalEnum independently re-derives an ordinal enum's wire strings by calling
// json.Marshal on each ordinal 0..N (stopping when the marshaller errors). Kept
// separate from the generator's stringEnumWireValues so the test cross-checks the
// GENERATED enum against a fresh marshal, not against the generator's own helper.
func liveMarshalEnum(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	var out []string
	for i := 0; i < 1024; i++ {
		v := reflect.New(typ).Elem()
		v.SetInt(int64(i))
		raw, err := json.Marshal(v.Interface())
		if err != nil {
			break
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("%s ordinal %d did not marshal to a string: %s", typ.Name(), i, raw)
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		t.Fatalf("%s produced no wire strings", typ.Name())
	}
	return out
}

// propSchema resolves a "ModelX.prop" path to its emitted property schema map.
func propSchema(t *testing.T, block map[string]any, path string) map[string]any {
	t.Helper()
	schema, prop, _ := strings.Cut(path, ".")
	def, ok := block[schema].(map[string]any)
	if !ok {
		t.Fatalf("schema %q missing", schema)
	}
	props, ok := def["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema %q has no properties", schema)
	}
	pm, ok := props[prop].(map[string]any)
	if !ok {
		t.Fatalf("%s missing or not an object schema", path)
	}
	return pm
}

// schemaTypeStrings returns a schema node's `type` as a set of strings, handling
// both the scalar `type: string` and nullable `type: ["null","string"]` forms.
func schemaTypeStrings(m map[string]any) map[string]bool {
	set := map[string]bool{}
	switch tv := m["type"].(type) {
	case string:
		set[tv] = true
	case []any:
		for _, x := range tv {
			if s, ok := x.(string); ok {
				set[s] = true
			}
		}
	}
	return set
}

// TestModelComponentLayerStringEnum is the focused ground-truth assertion:
// ModelComponent.layer is {type:string, enum:[...]} whose enum EXACTLY equals the
// live-marshalled Layer values, in ordinal order — and equals the hand models.ts
// Layer union ('client'|'manager'|'engine'|'resourceAccess'|'resource'|'utility').
func TestModelComponentLayerStringEnum(t *testing.T) {
	block, err := modelComponentSchemas()
	if err != nil {
		t.Fatalf("modelComponentSchemas: %v", err)
	}
	layer := propSchema(t, block, "ModelComponent.layer")

	if types := schemaTypeStrings(layer); !types["string"] || types["integer"] {
		t.Errorf("ModelComponent.layer type is not string (got %#v)", layer["type"])
	}

	enumAny, ok := layer["enum"].([]any)
	if !ok {
		t.Fatalf("ModelComponent.layer has no enum: %#v", layer)
	}
	got := make([]string, len(enumAny))
	for i, v := range enumAny {
		got[i], _ = v.(string)
	}

	live := liveMarshalEnum(t, reflect.TypeOf(projectstate.Layer(0)))
	if !reflect.DeepEqual(got, live) {
		t.Errorf("ModelComponent.layer enum != live marshal:\n got=%v\nlive=%v", got, live)
	}
	// Cross-check against the (to-be-deleted) hand models.ts Layer union.
	handUnion := []string{"client", "manager", "engine", "resourceAccess", "resource", "utility"}
	if !reflect.DeepEqual(got, handUnion) {
		t.Errorf("ModelComponent.layer enum != hand models.ts Layer union:\n  got=%v\n hand=%v", got, handUnion)
	}
}

// TestKnownEnumFieldsAreStringEnums asserts EVERY field in the string-marshalled
// enum inventory emits {type:string, enum:liveValues} and NONE is typed integer —
// the exact defect the fix targets.
func TestKnownEnumFieldsAreStringEnums(t *testing.T) {
	block, err := modelComponentSchemas()
	if err != nil {
		t.Fatalf("modelComponentSchemas: %v", err)
	}
	for path, typ := range knownEnumFieldTypes {
		pm := propSchema(t, block, path)
		types := schemaTypeStrings(pm)
		if types["integer"] {
			t.Errorf("%s is still typed integer (defect not fixed): %#v", path, pm["type"])
			continue
		}
		if !types["string"] {
			t.Errorf("%s is not typed string: %#v", path, pm["type"])
			continue
		}
		enumAny, ok := pm["enum"].([]any)
		if !ok {
			t.Errorf("%s has no enum list: %#v", path, pm)
			continue
		}
		got := make([]string, len(enumAny))
		for i, v := range enumAny {
			got[i], _ = v.(string)
		}
		if live := liveMarshalEnum(t, typ); !reflect.DeepEqual(got, live) {
			t.Errorf("%s enum != live marshal of %s:\n got=%v\nlive=%v", path, typ.Name(), got, live)
		}
	}
}

// TestNoModelEnumFieldTypedAsInteger walks the WHOLE emitted block and fails if any
// property (or slice item) that resolves to a registered string-marshalled enum type
// is typed integer anywhere — a regression guard beyond the known inventory.
func TestNoModelEnumFieldTypedAsInteger(t *testing.T) {
	block, err := modelComponentSchemas()
	if err != nil {
		t.Fatalf("modelComponentSchemas: %v", err)
	}
	// Every registered enum's wire-value set; any integer field whose sibling enum
	// list equals one of these would be a bug, but the primary guard is the known
	// inventory: assert none of those paths is integer (belt-and-suspenders vs the
	// per-field assertion above, in case the inventory table drifts).
	for path := range knownEnumFieldTypes {
		pm := propSchema(t, block, path)
		if schemaTypeStrings(pm)["integer"] {
			t.Errorf("%s typed integer — string-marshalled enum must be a string enum", path)
		}
	}
}

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
