// Package codegen holds the shared contract-descriptor types used by the
// schema-first codegen tools (cmd/schemagen writes them, cmd/modelgen reads
// them). They describe the part of a service contract that JSON Schema cannot
// express on its own: the component's interface and its operations (the RPC
// surface). Data shapes live in the contract document's `$defs`; this descriptor
// lives alongside them under the document's `interface` key.
package codegen

import "github.com/google/jsonschema-go/jsonschema"

// Interface is one component's service-contract interface — the generated Go
// interface's name, its Method layer, and its operations. Layer selects the
// per-layer call context (e.g. engine.Context, resourceaccess.Context) the
// generator prepends to every method.
type Interface struct {
	Name       string      `json:"name"`
	Layer      string      `json:"layer"`
	Operations []Operation `json:"operations"`
}

// Operation is one method on the interface: its name, ordered parameters, an
// optional result type, and whether it returns an error.
type Operation struct {
	Name   string             `json:"name"`
	Params []Param            `json:"params"`
	Result *jsonschema.Schema `json:"result,omitempty"`
	Error  bool               `json:"error"`
}

// Param is one operation parameter. Schema is a JSON Schema node — either a
// `$ref` into the contract's `$defs` (for a model type) or an inline primitive /
// array schema. Pointer marks a nullable pointer parameter (e.g. `*ActivityID`),
// where nil is load-bearing; the generator emits `*T`.
type Param struct {
	Name    string             `json:"name"`
	Pointer bool               `json:"pointer,omitempty"`
	Schema  *jsonschema.Schema `json:"schema"`
}

// SumType is the codegen descriptor for a sealed-interface / discriminated-union
// (sum) type. It is carried on a `$def` under the `x-go-sumtype` extension key,
// alongside that def's JSON Schema `oneOf` (the variant `$ref` list). schemagen
// reflects a registered sealed interface into this descriptor (discovering each
// variant's kind STRING by calling its discriminator method via reflection);
// modelgen reads it back to emit the sealed interface, the per-variant marker +
// discriminator methods, and the `{discriminatorKey: <kind>, "model": <v>}`
// envelope codec — byte-identical to the hand-written wire form.
type SumType struct {
	// Iface is the Go type name of the sealed interface (e.g. "ArtifactModel").
	Iface string `json:"iface"`
	// Marker is the unexported marker method name that seals the sum (e.g.
	// "isArtifactModel"). Emitted on every variant so only this package's types
	// satisfy the interface.
	Marker string `json:"marker"`
	// DiscriminatorKey is the envelope JSON key carrying the kind string (e.g.
	// "kind"). The envelope is {discriminatorKey: <kindStr>, "model": <variant>}.
	DiscriminatorKey string `json:"discriminatorKey"`
	// KindEnum is the Go type name of the discriminator returned by each variant's
	// discriminator method (e.g. "ArtifactKind"). Emitted as the interface method's
	// result type and each variant's Kind() return type.
	KindEnum string `json:"kindEnum"`
	// Variants is the ordered set of sum members.
	Variants []SumVariant `json:"variants"`
}

// SumVariant is one member of a sum type: its wire kind string, the variant Go
// struct type name, and the Go const naming its discriminator value.
type SumVariant struct {
	// Kind is the discriminator STRING written on the wire (e.g. "mission"). It is
	// discovered by reflection (calling the variant's discriminator method and
	// mapping the result to its string) so the generated envelope is byte-identical
	// to the hand-written codec.
	Kind string `json:"kind"`
	// Type is the variant's Go struct type name (e.g. "MissionStatement"), also a
	// normal `$def` in the same document.
	Type string `json:"type"`
	// KindConst is the Go const that names this variant's discriminator value (e.g.
	// "KindMission"), returned by the variant's generated Kind() method.
	KindConst string `json:"kindConst"`
}
