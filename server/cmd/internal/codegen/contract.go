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
