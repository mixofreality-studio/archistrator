package projectstate

// stalecause.go carries the ADDITIVE stale-cause record for the F38 staleness rail.
//
// When an upstream artifact re-commits (an amendment), commitTransition flags every
// already-committed DOWNSTREAM slot StaleBasis. StaleBasis alone answers "is this slot's
// basis shifted?" but not "shifted BY WHAT?" — so the UI can only show a bare "stale"
// chip. StaleCause records the CAUSE: which upstream slot amended, and its NEW revision
// after that amendment, so the read model can say e.g. "Volatilities rev 2 changed after
// this was committed".
//
// It is purely additive and backward-compatible:
//   - omitempty everywhere: a non-stale slot carries no cause, and the on-disk shape is
//     byte-identical for every slot the rail never touched.
//   - NO migration: a slot that went stale BEFORE this field existed reads back with a
//     nil cause (StaleBasis true, StaleBasisCause nil). Absent cause is allowed — readers
//     treat it as "stale, cause unknown".
type StaleCause struct {
	// UpstreamKind is the wire name of the upstream artifact kind whose amendment shifted
	// this slot's basis (e.g. "volatilities").
	UpstreamKind string `json:"upstreamKind"`
	// UpstreamRevision is that upstream slot's commit/revision count AFTER the amendment
	// that caused this slot to go stale (e.g. 2 — "rev 2 changed after this was committed").
	UpstreamRevision int64 `json:"upstreamRevision"`
}
