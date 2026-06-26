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
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// The ArtifactAccess port interface and its I/O value types (ConstructionOutput,
// OutputTree, and the named scalar OutputPath) are now GENERATED from
// contract.schema.json into contract.gen.go (schema-first; edit the schema and run
// `make gen`). Each method takes the ResourceAccess call Context `rc fwra.Context`
// first — it embeds context.Context and carries the Principal + IdempotencyKey, so
// the cross-cutting ctx/idempotencyKey that the hand-written surface passed
// explicitly now ride the context. The design rationale not captured by the
// generated signatures:
//
//   - StoreConstructionOutput establishes a content-addressable identity for one
//     output. Storing identical content returns the SAME address (no duplicate);
//     storing different content yields a NEW address (the prior is retained —
//     immutable history). The caller-supplied rc.IdempotencyKey goes into the infra
//     commit message; this method never reads Temporal.
//   - RetrieveConstructionOutput is a pure by-address read; an unknown address
//     surfaces as fwra.NotFound. Byte-identical across retries.
//   - RetrieveOutputTree returns the flat path->content-address snapshot at a
//     tree-root address; an unknown address surfaces as fwra.NotFound.

// Error is the shared ResourceAccess error model (framework-go), re-exported as
// an alias so this component's contract reads in its own terms while every RA
// component shares one fixed enum. Construct with fwra.New / fwra.Wrap using the
// shared kinds (fwra.NotFound, fwra.Conflict, fwra.Auth, fwra.Transient,
// fwra.Infrastructure, fwra.ContractMisuse).
type Error = fwra.Error
