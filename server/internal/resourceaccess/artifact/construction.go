package artifact

// This file documents the shared Phase-3 construction-output value types. They are
// now GENERATED from contract.schema.json into contract.gen.go (schema-first; edit
// the schema and run `make gen`). They live HERE in artifactAccess (the RA that
// STORES them); workerAccess — which PRODUCES a ConstructionOutput — references them
// directly. The shared value vocabulary is owned by the RA that fronts the resource
// it describes (per The Method's layer model: ResourceAccess owns its value types
// and exposes them on its port).
//
// The generated types and their design rationale (not captured by the generated
// declarations):
//
//   - ConstructionOutput is a Phase-3 build product (opaque bytes + advisory MIME).
//     Canonical shared value type per artifactAccess.md §3; owned here, referenced
//     by workerAccess (which produces a ConstructionOutput as a downward import —
//     the worker RA can read the artifact RA's value types because they sit at the
//     same layer and the producer→store value flow is the natural direction).
//   - OutputTree is a frozen path->content-address snapshot (artifactAccess.md §3).
//     Its Entries map is generated as map[string]string (JSON Schema map keys are
//     always strings); the impl bridges the logical OutputPath key at the boundary.
//   - OutputPath is a logical, slash-separated path within an OutputTree
//     (artifactAccess.md §3). Infrastructure-opaque.
