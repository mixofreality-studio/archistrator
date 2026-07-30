package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
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
	"ModelComponent.kind":                   reflect.TypeFor[projectstate.ComponentKind](),
	"ModelComponent.layer":                  reflect.TypeFor[projectstate.Layer](),
	"ModelRelationship.mode":                reflect.TypeFor[projectstate.CallMode](),
	"ModelVolatility.axis":                  reflect.TypeFor[projectstate.Axis](),
	"ModelCheckItem.status":                 reflect.TypeFor[projectstate.CheckStatus](),
	"ModelActivityNode.kind":                reflect.TypeFor[projectstate.ActivityNodeKind](),
	"ModelActivityEdge.kind":                reflect.TypeFor[projectstate.EdgeKind](),
	"ModelUseCase.trigger":                  reflect.TypeFor[projectstate.Trigger](),
	"ModelUseCase.classification":           reflect.TypeFor[projectstate.Classification](),
	"ModelDeploymentEnvironment.profile":    reflect.TypeFor[projectstate.DeploymentProfile](),
	"ModelDeploymentTopology.deliveryStyle": reflect.TypeFor[projectstate.DeliveryStyle](),
	"ModelSolution.slotKind":                reflect.TypeFor[projectstate.ArtifactKind](),
	"ModelRiskRow.solutionKind":             reflect.TypeFor[projectstate.ArtifactKind](),
	"ModelSdpOptionRow.solutionKind":        reflect.TypeFor[projectstate.ArtifactKind](),
	"ModelRiskModel.recommendation":         reflect.TypeFor[projectstate.ArtifactKind](),
}

// liveMarshalEnum independently re-derives an ordinal enum's wire strings by calling
// json.Marshal on each ordinal 0..N (stopping when the marshaller errors). Kept
// separate from the generator's stringEnumWireValues so the test cross-checks the
// GENERATED enum against a fresh marshal, not against the generator's own helper.
func liveMarshalEnum(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	var out []string
	for i := range 1024 {
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

	live := liveMarshalEnum(t, reflect.TypeFor[projectstate.Layer]())
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

// TestNoModelEnumFieldTypedAsInteger does NOT walk the whole emitted block
// (despite the name) — it re-checks the SAME knownEnumFieldTypes inventory paths
// as TestKnownEnumFieldsAreStringEnums above, with a narrower, cheaper assertion
// (type only, ignoring enum contents): a belt-and-suspenders duplicate in case
// that test's fuller equality check is ever weakened. It gives NO guard against a
// field whose enum TYPE is missing from the inventory entirely — that gap is what
// TestStringEnumTypesRegistryComplete (below) closes, by source-walking
// projectstate for every int-backed type with a custom MarshalJSON method and
// asserting each one is registered in modelschemas.go's stringEnumTypes().
func TestNoModelEnumFieldTypedAsInteger(t *testing.T) {
	block, err := modelComponentSchemas()
	if err != nil {
		t.Fatalf("modelComponentSchemas: %v", err)
	}
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
	"ModelDeploymentOperationsModel",
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

// --- TestStringEnumTypesRegistryComplete: a source-walked future-proofing guard
// (step4-task4 review) that stringEnumTypes() in modelschemas.go can never
// silently miss a new string-marshalled ordinal enum. -----------------------

// projectstateSourceDir resolves the absolute path to the projectstate package
// directory, relative to THIS test file's own location (via runtime.Caller),
// so the walk below works regardless of the directory `go test` is invoked
// from.
func projectstateSourceDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to resolve this test file's own path")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "internal", "resourceaccess", "projectstate")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolve projectstate source dir: %v", err)
	}
	if fi, statErr := os.Stat(abs); statErr != nil || !fi.IsDir() {
		t.Fatalf("projectstate source dir not found at %s (layout assumption stale?): %v", abs, statErr)
	}
	return abs
}

// intKindGoIdents is the set of Go builtin identifiers an "int-based" type
// declaration (`type X <ident>`) can name. Every projectstate ordinal enum in
// stringEnumTypes() today is `type X int`; the set is intentionally a little
// wider so the walk also catches a future enum declared over a differently
// sized int kind.
var intKindGoIdents = map[string]bool{
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "byte": true, "rune": true,
}

// receiverTypeName extracts the bare type name a method's receiver names,
// unwrapping a leading pointer (`*X` -> `X`). Returns "" if the receiver shape
// is anything else (shouldn't happen for a well-formed Go file).
func receiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) != 1 {
		return ""
	}
	expr := recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

// isMarshalJSONSignature reports whether a func decl's signature matches
// json.Marshaler's single method — `MarshalJSON() ([]byte, error)` — so the
// walk below doesn't false-positive on some unrelated method that happens to be
// named MarshalJSON with a different shape.
func isMarshalJSONSignature(fn *ast.FuncDecl) bool {
	if fn.Name.Name != "MarshalJSON" {
		return false
	}
	sig := fn.Type
	if sig.Params != nil && len(sig.Params.List) > 0 {
		return false
	}
	if sig.Results == nil || len(sig.Results.List) != 2 {
		return false
	}
	arr, ok := sig.Results.List[0].Type.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false // not a slice (or is a fixed-size array)
	}
	if elt, ok := arr.Elt.(*ast.Ident); !ok || elt.Name != "byte" {
		return false
	}
	ident, ok := sig.Results.List[1].Type.(*ast.Ident)
	return ok && ident.Name == "error"
}

// collectIntKindTypeDecls scans one file's top-level type declarations and
// records, into intKindTypes, the name of every `type X <intKindGoIdents>`
// declaration (e.g. `type Axis int`). Generic type declarations are skipped —
// no projectstate enum here uses type params.
func collectIntKindTypeDecls(d *ast.GenDecl, intKindTypes map[string]bool) {
	if d.Tok != token.TYPE {
		return
	}
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || ts.TypeParams != nil {
			continue
		}
		ident, ok := ts.Type.(*ast.Ident)
		if !ok || !intKindGoIdents[ident.Name] {
			continue
		}
		intKindTypes[ts.Name.Name] = true
	}
}

// collectMarshalJSONReceiver records, into marshalJSONReceivers, the receiver
// type name of d IF d is a genuine `func (recv X) MarshalJSON() ([]byte, error)`
// method declaration.
func collectMarshalJSONReceiver(d *ast.FuncDecl, marshalJSONReceivers map[string]bool) {
	if d.Recv == nil || !isMarshalJSONSignature(d) {
		return
	}
	if recvName := receiverTypeName(d.Recv); recvName != "" {
		marshalJSONReceivers[recvName] = true
	}
}

// isProjectstateSourceFile reports whether name is a non-test .go source file
// that should be included in the intBackedMarshalJSONTypeNames walk.
func isProjectstateSourceFile(e os.DirEntry) bool {
	name := e.Name()
	return !e.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

// intBackedMarshalJSONTypeNames source-parses every non-test .go file directly
// in dir (go/parser + go/ast, stdlib only — no go/packages dependency needed)
// and returns the set of type names that are BOTH declared with an int-kind
// underlying type (`type X int`, …) AND carry a custom
// `func (recv X) MarshalJSON() ([]byte, error)` method — i.e. every ordinal enum
// that customizes its own JSON encoding to a string. That is exactly the
// pattern the OAS reflector (modelschemas.go's stringEnumTypes() +
// swapIntegerToStringEnum) must be told about, or the type's fields silently
// reflect as `integer` in the merged OAS instead of the true wire string enum.
func intBackedMarshalJSONTypeNames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read projectstate dir %s: %v", dir, err)
	}

	intKindTypes := map[string]bool{}
	marshalJSONReceivers := map[string]bool{}

	fset := token.NewFileSet()
	for _, e := range entries {
		if !isProjectstateSourceFile(e) {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				collectIntKindTypeDecls(d, intKindTypes)
			case *ast.FuncDecl:
				collectMarshalJSONReceiver(d, marshalJSONReceivers)
			}
		}
	}

	out := map[string]bool{}
	for typeName := range marshalJSONReceivers {
		if intKindTypes[typeName] {
			out[typeName] = true
		}
	}
	return out
}

// TestStringEnumTypesRegistryComplete is the future-proofing guard the
// step4-task4 review asked for: it source-walks the WHOLE projectstate package
// (not a hand-maintained list, nor a count assertion — a genuine walk) for every
// `type X int` (or other int-kind) declaration that also carries a custom
// `MarshalJSON() ([]byte, error)` method, i.e. every ordinal enum that marshals
// itself as a wire string, and asserts that type is registered in
// modelschemas.go's stringEnumTypes(). Cross-checked in both directions: a type
// the walk finds but the registry doesn't know about would silently reflect as
// `integer` in the merged OAS (the exact class of defect
// TestKnownEnumFieldsAreStringEnums fixed for the known 14 — this is what stops
// enum #15 from repeating it); a type the registry claims but the walk can no
// longer find flags a stale or renamed registry entry.
//
// If this test fails because you added a NEW string-marshalled ordinal enum:
// add it to BOTH enumjson.go (the MarshalJSON method) and stringEnumTypes() in
// modelschemas.go — the walk here only tells you one of the two is missing, it
// can't register the type for you.
func TestStringEnumTypesRegistryComplete(t *testing.T) {
	dir := projectstateSourceDir(t)
	found := intBackedMarshalJSONTypeNames(t, dir)
	if len(found) == 0 {
		t.Fatal("source walk found zero int-backed MarshalJSON types — parsing assumption is broken (expected to find at least the known enums)")
	}

	registered := map[string]bool{}
	for _, rt := range stringEnumTypes() {
		registered[rt.Name()] = true
	}

	var missing []string
	for typeName := range found {
		if !registered[typeName] {
			missing = append(missing, typeName)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("stringEnumTypes() in modelschemas.go is missing %d int-backed, JSON-string-marshalled projectstate type(s): %v — "+
			"add each to stringEnumTypes() or the merged OAS will silently reflect it as `integer`", len(missing), missing)
	}

	var stale []string
	for typeName := range registered {
		if !found[typeName] {
			stale = append(stale, typeName)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("stringEnumTypes() registers %d projectstate type(s) the source walk no longer finds as an int-backed type with a MarshalJSON method (renamed/removed?): %v", len(stale), stale)
	}
}
