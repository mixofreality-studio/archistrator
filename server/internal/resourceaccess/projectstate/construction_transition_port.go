package projectstate

// ConstructionTransitionAccess (contract.gen.go) is the Port interface for the
// Phase-3 construction transition verbs (App-C §6 adjudicated: 10 ops ≤ 12 cap,
// per conformance gate lifecycle-2 T3 analysis). The interface itself is
// GENERATED from project.json's .serviceContracts.constructionTransitionAccess
// entry (authored via contract.constructionTransitionAccess.schema.json), so
// like the sibling ProjectStateAccess it takes the RA-layer call context
// (rc fwra.Context) as every op's first parameter.
//
// WARNING — ReadProject (the 10th op) is IN-PROCESS-ONLY (B8-follow-up ruling): it
// returns the raw Project aggregate, whose ArtifactSlots carry the SEALED
// ArtifactModel interface that Temporal's default JSON data converter cannot decode
// across an Activity boundary. The op nevertheless has a generated Temporal activity
// ("constructionTransitionAccess.readProject", activities.gen.go/worker.gen.go —
// codegen emits one per contract op unconditionally) and a generated invoker
// (genInvokers.ConstructionTransitionReadProject) — DO NOT invoke either from a
// workflow: the result would decode with every slot's Model silently nil-ed/mangled.
// Workflows needing the whole aggregate must read through
// designSessionAccess.readProjectOnBranch, which returns the codec-safe
// ProjectEnvelope (envelope.go). The op is KEPT on the contract because it has a live
// in-process consumer: constructionManager.UpdateReviewPolicy
// (internal/manager/construction/constructionmanager.go) reads the current head
// Version through it before RecordReviewPolicy — a façade-side direct call that
// never crosses Temporal.

// Compile-time assertion: GitStore must satisfy the full 10-op port.
var _ ConstructionTransitionAccess = (*GitStore)(nil)
