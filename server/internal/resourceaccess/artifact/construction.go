package artifact

// This file homes the shared Phase-3 construction-output value types. They live
// HERE in artifactAccess (the RA that STORES them); workerAccess — which PRODUCES
// a ConstructionOutput — references them directly. The shared value vocabulary is
// owned by the RA that fronts the resource it describes (per The Method's layer
// model: ResourceAccess owns its value types and exposes them on its port).

// ConstructionOutput is a Phase-3 build product (opaque bytes + advisory MIME).
// Canonical shared value type per artifactAccess.md §3; owned here, referenced by
// workerAccess (which produces a ConstructionOutput as a downward import — the
// worker RA can read the artifact RA's value types because they sit at the same
// layer and the producer→store value flow is the natural direction).
type ConstructionOutput struct {
	Bytes    []byte
	MIMEType string
}

// OutputTree is a frozen path->content-address snapshot (artifactAccess.md §3).
type OutputTree struct {
	Root    string
	Entries map[OutputPath]string
}

// OutputPath is a logical, slash-separated path within an OutputTree
// (artifactAccess.md §3). Infrastructure-opaque.
type OutputPath string
