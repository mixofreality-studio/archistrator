// Package artifact is the artifactAccess component of the aiarch server's
// ResourceAccess layer — the Temporal-free port over the content-addressable
// store for PHASE-3 CONSTRUCTION OUTPUTS only (artifactAccess.md, re-cut
// 2026-05-26). It mediates every write into and read out of the construction
// store: generated source trees, compiled build artifacts, helm charts, k8s
// manifests, deployable bundles. It does NOT store Phase-1/2 Method artifacts —
// those are typed domain models in projectStateAccess.
//
// Per The Method's layer model ([[the-method-layers]]): ResourceAccess
// components import NO Temporal. The calling Manager wraps each verb below in a
// Manager-owned Temporal Activity and passes the idempotencyKey in as an
// ordinary parameter; this package never reads Temporal context.
//
// The shared value types ConstructionOutput / OutputTree / OutputPath are owned
// HERE (construction.go) — this is the RA that stores them. workerAccess —
// which PRODUCES a ConstructionOutput — imports them as a downward edge.
//
// Derived faithfully from the frozen artifactAccess.md contract (Phase-3 CAS).
package artifact

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ArtifactAccess is the Temporal-free port over the Phase-3 construction-output
// content-addressable store (artifactAccess.md §2). Three atomic construction
// business verbs; the content address is a plain string compared by value (==).
//
//   - StoreConstructionOutput establishes a content-addressable identity for one
//     output. Storing identical content returns the SAME address (no duplicate);
//     storing different content yields a NEW address (the prior is retained —
//     immutable history). The caller-supplied idempotencyKey goes into the infra
//     commit message; this method never reads Temporal.
//   - RetrieveConstructionOutput is a pure by-address read; an unknown address
//     surfaces as fwra.NotFound. Byte-identical across retries.
//   - RetrieveOutputTree returns the flat path->content-address snapshot at a
//     tree-root address; an unknown address surfaces as fwra.NotFound.
type ArtifactAccess interface {
	StoreConstructionOutput(ctx context.Context, content ConstructionOutput, idempotencyKey fwra.IdempotencyKey) (contentAddress string, err error)
	RetrieveConstructionOutput(ctx context.Context, contentAddress string) (ConstructionOutput, error)
	RetrieveOutputTree(ctx context.Context, contentAddress string) (OutputTree, error)
}

// Error is the shared ResourceAccess error model (framework-go), re-exported as
// an alias so this component's contract reads in its own terms while every RA
// component shares one fixed enum. Construct with fwra.New / fwra.Wrap using the
// shared kinds (fwra.NotFound, fwra.Conflict, fwra.Auth, fwra.Transient,
// fwra.Infrastructure, fwra.ContractMisuse).
type Error = fwra.Error
