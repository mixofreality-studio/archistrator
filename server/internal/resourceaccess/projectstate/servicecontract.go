package projectstate

import "encoding/json"

// servicecontract.go holds the typed service-contract corpus model for the
// construction head-state. project.json (.serviceContracts) is the OWNER of each
// built component's service contract; the value is a contract DOCUMENT — the same
// shape the per-component `contract.schema.json` carries (a `title`, a `$defs` map
// of JSON Schemas, and an `interface` describing the RPC surface), plus the
// self-describing metadata (component key, Method layer, target Go package) that
// makes the document buildable by modelgen straight from project.json.
//
// DESIGN: data shapes round-trip as raw JSON (json.RawMessage) so the stored
// document is byte-identical across an EncodeProjectJSON → DecodeProjectJSON cycle;
// the Go encoder's MarshalIndent pass re-indents every raw node uniformly, so the
// committed project.json is canonical. The Interface mirrors codegen.Interface but
// keeps each schema node as raw JSON for the same byte-fidelity reason.

// ServiceContract is one component's service contract, stored as a contract
// document in Project.ServiceContracts (keyed by component name). It is the OWNER
// of the contract for the built components — the per-component contract.schema.json
// is a render-on-read of this entry (and is removed in a later stage). Additive,
// nil until seeded.
type ServiceContract struct {
	// Component is the contract key/name (e.g. "artifactAccess").
	Component string `json:"component"`
	// Layer is the Method layer (Client | Manager | Engine | ResourceAccess | Utility).
	Layer string `json:"layer"`
	// GoPackage is the target Go package for codegen (e.g.
	// "internal/resourceaccess/artifact"). Empty for un-migrated stub entries.
	GoPackage string `json:"goPackage,omitempty"`
	// Title is the contract document title (e.g. "artifact contract").
	Title string `json:"title"`
	// Defs is the document's `$defs` — each value is a JSON Schema, stored raw so
	// it round-trips exactly. Omitted when empty (un-migrated stubs).
	Defs map[string]json.RawMessage `json:"$defs,omitempty"`
	// Interface is the component's interface (the RPC surface): name, layer, ops.
	Interface ContractInterface `json:"interface"`
}

// ContractInterface mirrors codegen.Interface: the generated Go interface's name,
// its Method layer, and its operations.
type ContractInterface struct {
	Name       string              `json:"name"`
	Layer      string              `json:"layer"`
	Operations []ContractOperation `json:"operations"`
}

// ContractOperation is one method on the interface: its name, ordered parameters,
// an optional result schema, and whether it returns an error.
type ContractOperation struct {
	Name string `json:"name"`
	// Params is the ordered parameter list. A nil slice encodes as `null` (no
	// omitempty) to match the contract-document shape codegen writes.
	Params []ContractParam `json:"params"`
	// Result is the result type's JSON Schema node (raw); omitted when the op has
	// no result.
	Result json.RawMessage `json:"result,omitempty"`
	Error  bool            `json:"error"`
}

// ContractParam is one operation parameter. Schema is a JSON Schema node (raw) —
// either a `$ref` into the contract's `$defs` or an inline schema. Pointer marks a
// nullable pointer parameter.
type ContractParam struct {
	Name    string          `json:"name"`
	Pointer bool            `json:"pointer,omitempty"`
	Schema  json.RawMessage `json:"schema"`
}
