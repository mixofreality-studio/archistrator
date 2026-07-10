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
		reflect.TypeOf(time.Time{}):          {Type: "string", Format: "date-time"},
		reflect.TypeOf(time.Duration(0)):     {Type: "integer"},
		reflect.TypeOf(uuid.UUID{}):          {Type: "string", Format: "uuid"},
		reflect.TypeOf([]byte(nil)):          {Type: "string", ContentEncoding: "base64"},
		reflect.TypeOf(json.RawMessage(nil)): {},
	}
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

	// Full TypeSchemas map: every closure struct → its Model<Name> $ref stub,
	// plus the well-known portable shapes. Passed to ForType per field so any
	// named struct field short-circuits to its stub (no recursion into the body,
	// so no cycle error) and any well-known field reflects to its portable shape.
	typeSchemas := map[reflect.Type]*jsonschema.Schema{}
	for rt, ws := range wk {
		typeSchemas[rt] = ws
	}
	for _, t := range closure {
		typeSchemas[t] = &jsonschema.Schema{Ref: modelRef(t.Name())}
	}

	out := make(map[string]any, len(closure))
	for _, t := range closure {
		body, err := modelStructBody(t, typeSchemas)
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
		for _, s := range strings.Split(rest, ",") {
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
func modelStructBody(t reflect.Type, typeSchemas map[reflect.Type]*jsonschema.Schema) (map[string]any, error) {
	props := map[string]any{}
	var required []any
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		info := fieldJSONInfo(f)
		if info.omit {
			continue
		}
		fs, err := jsonschema.ForType(f.Type, &jsonschema.ForOptions{
			IgnoreInvalidTypes: true,
			TypeSchemas:        typeSchemas,
		})
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", f.Name, err)
		}
		if fs == nil { // invalid type (e.g. non-string-keyed map) — ignored like ForType does
			continue
		}
		raw, err := json.Marshal(fs)
		if err != nil {
			return nil, fmt.Errorf("marshal field %s: %w", f.Name, err)
		}
		var generic any
		if err := json.Unmarshal(raw, &generic); err != nil {
			return nil, fmt.Errorf("decode field %s: %w", f.Name, err)
		}
		stripGoExtensions(generic)
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
// contracts' opaque raw-message draft pattern:
//
//	{type: ["null"], x-go-type: json.RawMessage, x-go-import: encoding/json}
//
// Detected by shape (not by hard-coded field name) so a newly-added opaque draft
// field is typed automatically — the caller asserts the resulting count.
func isOpaqueModelField(v any) bool {
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
			if !isOpaqueModelField(propVal) {
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
