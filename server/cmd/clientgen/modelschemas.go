package main

// modelschemas.go reflects the projectstate slot-model variants (the concrete
// payloads behind the manager contracts' opaque DraftModel.model raw-message
// field) into a shared `Model*`-prefixed block of OpenAPI component schemas, so
// the merged api/openapi.yaml carries the real typed shape of every artifact
// draft instead of `json.RawMessage`.
//
// WHY REFLECT (not fold into a contract): the 5 manager contracts deliberately
// carry the draft OPAQUELY — projectstate's sealed ArtifactModel sum and its 17
// variants are NOT part of any manager's serviceContracts $defs (adding them
// would make modelgen emit duplicate structs — see schemagen's projectstate
// bindByName note). Typing is a gen-client READ concern: we reflect the Go
// structs (projectstate stays the schema authority) and splice the result into
// the merged OAS at clientgen time only. Wire bytes are untouched.
//
// TECHNIQUE (schemagen's proven jsonschema-go approach, minimal helpers copied
// here rather than importing cmd/schemagen): every struct in the transitive
// closure of the 14 root variants gets a `#/components/schemas/Model<Name>`
// $ref stub; each struct body is then hand-assembled by reflecting its fields
// through jsonschema.ForType with that full stub map as TypeSchemas. Because
// every named struct field short-circuits to its stub $ref at the root of its
// own ForType call, recursion (DeploymentNode.Children []DeploymentNode) and
// mutual cycles resolve to intra-namespace $refs with no cycle error — the exact
// case schemagen has to hand-bind. x-go-* extensions are stripped (TS has no use
// for them). Deterministic: struct set + property order derive from the fixed Go
// declarations; yaml.v3 sorts map keys in the final marshal.

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// modelSchemaPrefix namespaces every reflected slot-model schema in the shared
// merged-OAS block, keeping it clear of the per-manager contract prefixes
// (SystemDesign*, ProjectDesign*, …).
const modelSchemaPrefix = "Model"

// modelRef is the components-schemas JSON pointer for a Model<name> schema.
func modelRef(name string) string { return schemaRefPrefix + modelSchemaPrefix + name }

// variantRootTypes enumerates the DISTINCT Go structs behind the artifact-kind
// closed set, authoritatively from projectstate.AllArtifactKinds +
// NewModelForKind. The 17 kinds collapse to 14 structs (the 4 Solution slot
// kinds share *Solution). Returned in first-seen (AllArtifactKinds) order.
func variantRootTypes() ([]reflect.Type, error) {
	seen := map[string]bool{}
	var roots []reflect.Type
	for _, k := range projectstate.AllArtifactKinds() {
		m, ok := projectstate.NewModelForKind(k)
		if !ok {
			return nil, fmt.Errorf("no model for artifact kind %v", k)
		}
		t := reflect.TypeOf(m)
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if seen[t.Name()] {
			continue
		}
		seen[t.Name()] = true
		roots = append(roots, t)
	}
	return roots, nil
}

// modelWellKnown maps foundational non-domain types to a portable JSON shape.
// Unlike schemagen's wellKnownByType these carry NO x-go-* bindings — the merged
// OAS is a TS-codegen input, not a Go-regen source. Used both as ForType
// TypeSchemas (so a field of these types reflects to the portable shape) and as
// closure leaves (never descended into, never emitted as a Model<Name> schema).
func modelWellKnown() map[reflect.Type]*jsonschema.Schema {
	return map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[time.Time]():       {Type: "string", Format: "date-time"},
		reflect.TypeFor[time.Duration]():   {Type: "integer"},
		reflect.TypeFor[uuid.UUID]():       {Type: "string", Format: "uuid"},
		reflect.TypeFor[[]byte]():          {Type: "string", ContentEncoding: "base64"},
		reflect.TypeFor[json.RawMessage](): {},
	}
}

// stringEnumTypes is the authoritative set of projectstate ordinal-enum Go types
// that carry a custom string MarshalJSON — the 14 in enumjson.go plus ArtifactKind
// (identity.go). Every such type is declared `type X int`, so jsonschema.ForType
// reflects the STATIC int type and emits `integer`; but the wire actually ships the
// canonical camelCase STRING (Layer(0) -> "client"). This slice enumerates the type
// IDENTITIES only — the string values are never hand-listed here; they are produced
// by live-marshalling each ordinal (see stringEnumWireValues), so the emitted enum
// is ground-truth-by-construction and cannot drift from the marshaller.
//
// (ActivityKind is a type alias for ActivityType — same reflect.Type — so listing
// ActivityType covers both.)
func stringEnumTypes() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[projectstate.Axis](),
		reflect.TypeFor[projectstate.RejectionClass](),
		reflect.TypeFor[projectstate.CheckStatus](),
		reflect.TypeFor[projectstate.ComponentKind](),
		reflect.TypeFor[projectstate.Layer](),
		reflect.TypeFor[projectstate.CallMode](),
		reflect.TypeFor[projectstate.Trigger](),
		reflect.TypeFor[projectstate.Classification](),
		reflect.TypeFor[projectstate.ActivityNodeKind](),
		reflect.TypeFor[projectstate.DeliveryStyle](),
		reflect.TypeFor[projectstate.DeploymentProfile](),
		reflect.TypeFor[projectstate.EdgeKind](),
		reflect.TypeFor[projectstate.ActivityType](),
		reflect.TypeFor[projectstate.TestingVariant](),
		reflect.TypeFor[projectstate.ArtifactKind](),
	}
}

// stringEnumWireValues live-marshals an ordinal enum type's values in ordinal
// order, starting at 0 and stopping at the first ordinal whose MarshalJSON errors.
// Both marshalEnum (enumjson.go) and ArtifactKind.MarshalJSON (identity.go) return
// an error for an out-of-range ordinal ("has no wire name"), which bounds the loop
// without a hand-maintained count — the marshaller itself defines the valid range.
// A generous safety bound guards against a hypothetical future all-ordinals-valid
// marshaller. Returns the decoded wire strings in ordinal order.
func stringEnumWireValues(t reflect.Type) ([]string, error) {
	const safetyBound = 1024
	var out []string
	for i := range safetyBound {
		v := reflect.New(t).Elem()
		v.SetInt(int64(i))
		raw, err := json.Marshal(v.Interface())
		if err != nil {
			break // ordinal i is out of range — the enum has values 0..i-1
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("string-enum %s ordinal %d marshalled to non-string %q", t.Name(), i, raw)
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("string-enum %s produced no wire values (ordinal 0 failed to marshal)", t.Name())
	}
	return out, nil
}

// stringEnumRegistry builds reflect.Type -> ordered wire-string enum list for every
// stringEnumTypes() entry, by live-marshalling. This is the lookup the per-field
// emission consults to override the reflected `integer` with the true string enum.
func stringEnumRegistry() (map[reflect.Type][]any, error) {
	reg := make(map[reflect.Type][]any, len(stringEnumTypes()))
	for _, t := range stringEnumTypes() {
		vals, err := stringEnumWireValues(t)
		if err != nil {
			return nil, err
		}
		anyVals := make([]any, len(vals))
		for i, s := range vals {
			anyVals[i] = s
		}
		reg[t] = anyVals
	}
	return reg, nil
}

// enumFieldWireValues reports whether a struct field's Go type is — after
// unwrapping a leading pointer and/or an outer slice/array (whose element may also
// be a pointer) — one of the registered string-marshalled enum types. It returns
// the ordered wire-string enum values, whether the enum sits inside a slice/array
// wrapper, and ok. Non-enum fields return ok=false.
func enumFieldWireValues(ft reflect.Type, reg map[reflect.Type][]any) (vals []any, isSlice bool, ok bool) {
	for ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	if v, found := reg[ft]; found {
		return v, false, true
	}
	if ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array {
		et := ft.Elem()
		for et.Kind() == reflect.Pointer {
			et = et.Elem()
		}
		if v, found := reg[et]; found {
			return v, true, true
		}
	}
	return nil, false, false
}

// swapIntegerToStringEnum rewrites a reflected scalar schema map (which ForType
// produced as an `integer`, possibly nullable) into a string enum in place. It
// preserves the field's nullability: `type: integer` becomes `type: string`, and
// `type: ["null","integer"]` becomes `type: ["null","string"]`. The ordered wire
// values are attached as `enum`.
func swapIntegerToStringEnum(m map[string]any, enumVals []any) {
	switch tv := m["type"].(type) {
	case string:
		m["type"] = "string"
	case []any:
		for i, x := range tv {
			if x == "integer" {
				tv[i] = "string"
			}
		}
	}
	m["enum"] = enumVals
}

// modelClosure returns every struct reflect.Type reachable from the roots by
// following exported struct fields (through pointers, slices, arrays and
// string-keyed maps), stopping at well-known leaves and non-struct
// (scalar/enum) types. Sorted by name for a stable set.
func modelClosure(roots []reflect.Type, wk map[reflect.Type]*jsonschema.Schema) []reflect.Type {
	structs := map[reflect.Type]bool{}
	var walkType func(ft reflect.Type)
	var addStruct func(t reflect.Type)

	walkType = func(ft reflect.Type) {
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if _, ok := wk[ft]; ok { // well-known leaf (time.Time, uuid.UUID, []byte, …)
			return
		}
		switch ft.Kind() {
		case reflect.Struct:
			addStruct(ft)
		case reflect.Slice, reflect.Array, reflect.Map:
			walkType(ft.Elem())
		case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
			reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func,
			reflect.Interface, reflect.Pointer, reflect.String, reflect.UnsafePointer:
			// Scalar / named-enum / interface leaf — inlined by ForType as its base
			// type; never emitted as a Model<Name> schema. (Pointers are already
			// deref'd above; listed for exhaustiveness.)
		}
	}
	addStruct = func(t reflect.Type) {
		if structs[t] {
			return
		}
		structs[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			if name, _, found := strings.Cut(f.Tag.Get("json"), ","); name == "-" && !found {
				continue // json:"-"
			}
			walkType(f.Type)
		}
	}

	for _, r := range roots {
		addStruct(r)
	}
	out := make([]reflect.Type, 0, len(structs))
	for t := range structs {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// modelComponentSchemas reflects the slot-model closure into a map of
// `Model<Name>` component schemas (generic map[string]any values ready to splice
// into components.schemas), with every nested struct reference pointing at
// #/components/schemas/Model<T> — self-references and cycles included.
func modelComponentSchemas() (map[string]any, error) {
	roots, err := variantRootTypes()
	if err != nil {
		return nil, err
	}
	wk := modelWellKnown()
	closure := modelClosure(roots, wk)

	// Live-marshalled registry of the string-encoded ordinal enums, so any field of
	// one of those types emits its true camelCase string enum rather than the
	// integer ForType reflects from the static `type X int`.
	enumReg, err := stringEnumRegistry()
	if err != nil {
		return nil, err
	}

	// Full TypeSchemas map: every closure struct → its Model<Name> $ref stub,
	// plus the well-known portable shapes. Passed to ForType per field so any
	// named struct field short-circuits to its stub (no recursion into the body,
	// so no cycle error) and any well-known field reflects to its portable shape.
	typeSchemas := map[reflect.Type]*jsonschema.Schema{}
	maps.Copy(typeSchemas, wk)
	for _, t := range closure {
		typeSchemas[t] = &jsonschema.Schema{Ref: modelRef(t.Name())}
	}

	out := make(map[string]any, len(closure))
	for _, t := range closure {
		body, err := modelStructBody(t, typeSchemas, enumReg)
		if err != nil {
			return nil, fmt.Errorf("reflect model %s: %w", t.Name(), err)
		}
		out[modelSchemaPrefix+t.Name()] = body
	}
	return out, nil
}

// fieldJSON mirrors encoding/json's field handling: the wire name and the
// optionality settings (omitempty/omitzero). omit is true for unexported fields
// or an explicit json:"-".
type fieldJSON struct {
	name      string
	omit      bool
	omitempty bool
}

func fieldJSONInfo(f reflect.StructField) fieldJSON {
	if !f.IsExported() {
		return fieldJSON{omit: true}
	}
	info := fieldJSON{name: f.Name}
	if tag, ok := f.Tag.Lookup("json"); ok {
		name, rest, found := strings.Cut(tag, ",")
		if name == "-" && !found {
			return fieldJSON{omit: true}
		}
		if name != "" {
			info.name = name
		}
		for s := range strings.SplitSeq(rest, ",") {
			if s == "omitempty" || s == "omitzero" {
				info.omitempty = true
			}
		}
	}
	return info
}

// modelStructBody hand-assembles one struct's object schema as a generic map:
// {type:object, additionalProperties:false, properties, required}. Each property
// is jsonschema.ForType'd with the shared stub map (so nested structs become
// $refs), round-tripped to a generic value, and stripped of x-go-* keys. The
// struct type is NOT itself in scope for its own reflection here — we iterate its
// fields directly, so a self-referential field resolves via its stub with no
// cycle error.
func modelStructBody(t reflect.Type, typeSchemas map[reflect.Type]*jsonschema.Schema, enumReg map[reflect.Type][]any) (map[string]any, error) {
	props := map[string]any{}
	var required []any
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		info := fieldJSONInfo(f)
		if info.omit {
			continue
		}
		// Reflect the field's type using jsonschema.ForType.
		fs, err := reflectFieldSchema(f, typeSchemas)
		if err != nil {
			return nil, err
		}
		if fs == nil {
			continue
		}
		// Round-trip through JSON to produce a generic map, stripping x-go-* keys.
		generic, err := schemaToGeneric(fs)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", f.Name, err)
		}
		// Override the reflected integer with the true string enum if this is a
		// string-marshalled ordinal enum type, preserving nullability.
		applyStringEnumOverride(f, generic, enumReg)
		// Wrap non-omitempty pointer-to-struct fields in anyOf:[<$ref>, {type:null}]
		// so the schema admits the wire's null value (e.g., UseCase.Activity).
		generic = wrapNullableStructPointer(f, info, generic)
		props[info.name] = generic
		if !info.omitempty {
			required = append(required, info.name)
		}
	}
	body := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           props,
	}
	if len(required) > 0 {
		body["required"] = required
	}
	return body, nil
}

// reflectFieldSchema reflects a single struct field via jsonschema.ForType using
// the shared type stubs, handling errors. Returns the raw jsonschema.Schema if
// successful (may be nil for invalid/ignored types, which the caller checks).
func reflectFieldSchema(f reflect.StructField, typeSchemas map[reflect.Type]*jsonschema.Schema) (*jsonschema.Schema, error) {
	fs, err := jsonschema.ForType(f.Type, &jsonschema.ForOptions{
		IgnoreInvalidTypes: true,
		TypeSchemas:        typeSchemas,
	})
	if err != nil {
		return nil, fmt.Errorf("field %s: %w", f.Name, err)
	}
	// fs may be nil for invalid types (e.g., non-string-keyed maps); the caller
	// checks for nil and skips the field (matched original ForType behavior).
	return fs, nil
}

// schemaToGeneric round-trips a jsonschema.Schema through JSON marshal/unmarshal
// to produce a generic map[string]any value suitable for YAML emission,
// stripping x-go-* extensions in the process.
func schemaToGeneric(fs *jsonschema.Schema) (any, error) {
	raw, err := json.Marshal(fs)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	stripGoExtensions(generic)
	return generic, nil
}

// applyStringEnumOverride checks if the field is a string-marshalled enum and
// rewrites the generic schema to use the true wire-value enum instead of the
// reflected integer type, preserving nullability. Returns true if a rewrite occurred.
func applyStringEnumOverride(f reflect.StructField, generic any, enumReg map[reflect.Type][]any) bool {
	enumVals, isSlice, ok := enumFieldWireValues(f.Type, enumReg)
	if !ok {
		return false
	}
	gm, gok := generic.(map[string]any)
	if !gok {
		return false
	}
	if isSlice {
		if items, iok := gm["items"].(map[string]any); iok {
			swapIntegerToStringEnum(items, enumVals)
			return true
		}
	} else {
		swapIntegerToStringEnum(gm, enumVals)
		return true
	}
	return false
}

// wrapNullableStructPointer wraps a non-omitempty pointer-to-struct field in
// anyOf:[<$ref>, {type:null}] so the schema admits the wire's null value. Returns
// the wrapped schema, or the original if no wrapping was needed.
func wrapNullableStructPointer(f reflect.StructField, info fieldJSON, generic any) any {
	if f.Type.Kind() != reflect.Pointer || info.omitempty {
		return generic
	}
	gm, gok := generic.(map[string]any)
	if !gok {
		return generic
	}
	if _, hasRef := gm["$ref"]; !hasRef {
		return generic
	}
	return map[string]any{"anyOf": []any{gm, map[string]any{"type": "null"}}}
}

// stripGoExtensions recursively deletes every x-go-* codegen extension key from a
// decoded schema value. The reflected slot-model schemas carry none by
// construction (we never inject go-name/int-width annotations, and modelWellKnown
// has no x-go-* bindings); this is a belt-and-suspenders guarantee that the
// Model* block is free of Go-only metadata the TS codegen would choke on.
func stripGoExtensions(node any) {
	switch v := node.(type) {
	case map[string]any:
		for k := range v {
			if strings.HasPrefix(k, "x-go-") {
				delete(v, k)
			}
		}
		for _, val := range v {
			stripGoExtensions(val)
		}
	case []any:
		for _, item := range v {
			stripGoExtensions(item)
		}
	}
}

// modelVariantRefs returns the 14 root-variant $ref schema nodes, sorted by
// Model<Name>, for the DraftModel.model oneOf splice.
func modelVariantRefs() ([]any, error) {
	roots, err := variantRootTypes()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(roots))
	for _, t := range roots {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	refs := make([]any, 0, len(names))
	for _, n := range names {
		refs = append(refs, map[string]any{"$ref": modelRef(n)})
	}
	return refs, nil
}

// isOpaqueModelField reports whether a decoded schema property is the manager
// contracts' opaque raw-message DRAFT-MODEL pattern:
//
//	"model": {type: ["null"], x-go-type: json.RawMessage, x-go-import: encoding/json}
//
// The marker is the PROPERTY NAME ("model" — DraftModel.model / ArtifactSlotModel.model,
// the one place a manager contract holds "whichever of the 14 slot models this is"), not
// the shape alone. The shape by itself is NOT unique to draft models: Task 9's episode
// TimelineEvent.raw uses the identical json.RawMessage encoding for a raw trace-event
// line (an opaque byte blob with no relationship to the 14 slot models whatsoever), and
// shape-only detection silently repointed it onto the 14-variant oneOf too (review
// finding, SP1 capture-seam Task 9 fix round) — the OAS declared TimelineEvent.raw to be
// ModelGlossary|ModelMission|…, and openapi-typescript would hand every downstream
// consumer that garbage union. propName lets the caller narrow to the one property this
// pattern actually means "the slot's opaque model" for.
func isOpaqueModelField(propName string, v any) bool {
	if propName != "model" {
		return false
	}
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	if got, _ := m["x-go-type"].(string); got != "json.RawMessage" {
		return false
	}
	list, ok := m["type"].([]any)
	if !ok || len(list) != 1 {
		return false
	}
	s, _ := list[0].(string)
	return s == "null"
}

// spliceModelSchemas adds the reflected Model* block to a merged
// components.schemas map (collision with an existing prefixed name is an error),
// then repoints every opaque raw-message draft property (by pattern) to a
// `oneOf` over the 14 sorted variant refs, preserving optionality (the field is
// left out of `required`, unchanged). Returns the sorted "<Schema>.<property>"
// paths it repointed, for the caller's count assertion / report.
func spliceModelSchemas(schemas map[string]any) ([]string, error) {
	block, err := modelComponentSchemas()
	if err != nil {
		return nil, err
	}
	for name, def := range block {
		if _, exists := schemas[name]; exists {
			return nil, fmt.Errorf("reflected slot-model schema %q collides with an existing merged schema name", name)
		}
		schemas[name] = def
	}

	refs, err := modelVariantRefs()
	if err != nil {
		return nil, err
	}

	var repointed []string
	for schemaName, def := range schemas {
		if strings.HasPrefix(schemaName, modelSchemaPrefix) {
			continue // never rewrite within our own reflected block
		}
		obj, ok := def.(map[string]any)
		if !ok {
			continue
		}
		props, ok := obj["properties"].(map[string]any)
		if !ok {
			continue
		}
		for propName, propVal := range props {
			if !isOpaqueModelField(propName, propVal) {
				continue
			}
			// Fresh slice per field so decoders never alias one another.
			oneOf := make([]any, len(refs))
			copy(oneOf, refs)
			props[propName] = map[string]any{"oneOf": oneOf}
			repointed = append(repointed, schemaName+"."+propName)
		}
	}
	sort.Strings(repointed)
	return repointed, nil
}
