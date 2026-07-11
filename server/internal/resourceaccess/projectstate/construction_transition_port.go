package projectstate

// ConstructionTransitionAccess (contract.gen.go) is the Port interface for the
// Phase-3 construction transition verbs (App-C §6 adjudicated: 10 ops ≤ 12 cap,
// per conformance gate lifecycle-2 T3 analysis). The interface itself is
// GENERATED from project.json's .serviceContracts.constructionTransitionAccess
// entry (authored via contract.constructionTransitionAccess.schema.json), so
// like the sibling ProjectStateAccess it takes the RA-layer call context
// (rc fwra.Context) as every op's first parameter.

// Compile-time assertion: GitStore must satisfy the full 10-op port.
var _ ConstructionTransitionAccess = (*GitStore)(nil)
