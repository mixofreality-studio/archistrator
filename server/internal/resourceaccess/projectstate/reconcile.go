package projectstate

import "fmt"

// reconcile.go holds the DETERMINISTIC project.json reconciliation the design rail uses
// when a session branch has diverged from main (F80).
//
// A design session branch is AwaitingReview while main can keep advancing (staleness
// acknowledgements, question seeds, a sibling artifact committing) — every one of those
// touches the SAME single file, .aiarch/state/project.json. A plain `git merge` of main
// into the session branch then conflicts on that file, which today dead-ends the
// redraft/answer refresh job (a RED merge-conflict) and leaves the PR mergeable_state=
// dirty so the approve-time merge cannot complete.
//
// The resolution is deterministic because project.json is a SERVER-OWNED,
// SINGLE-WRITER-PER-SLOT document: a design session legitimately owns exactly ONE slot —
// the artifact kind it is authoring — and never writes any other slot. So the reconciled
// document is unambiguously main's document (which carries every OTHER slot's latest
// committed content) with the session's OWN slot overlaid from the session-branch
// document. No content is guessed and no human merge is needed.

// ReconcileSlotOntoBase returns the reconciled project.json for a diverged design session
// branch: the `base` document (main's latest project.json) with the session's OWN slot —
// the one named by `kind` — overlaid from the `ours` document (the session-branch
// project.json). It is the single deterministic resolver shared by the workflow refresh
// step (via the aiarch-state-mcp `reconcile` subcommand) and the approve-time merge
// window (via the server's branch-reconcile activity), so both paths reconcile
// identically.
//
// The overlay is the WHOLE ArtifactSlot for `kind` (status, model, notes, the PM-critique
// carrier, the durable review thread, revisions, staleBasis): the session branch is the
// authoritative home of the in-flight draft AND its review ledger, so its slot wins
// wholesale, while every other slot comes from `base` (which may have advanced). Both
// documents are decoded and the result re-encoded through the SAME strict server codec,
// so a reconciled document is byte-for-byte what the server accepts on read-back (and it
// re-runs the F81 required-field gate over both inputs).
func ReconcileSlotOntoBase(base, ours []byte, projectID ProjectID, kind ArtifactKind) ([]byte, error) {
	baseProj, ok, err := DecodeProjectJSON(base, projectID)
	if err != nil {
		return nil, fmt.Errorf("reconcile: decode base (main) project state: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("reconcile: base (main) carried no project document")
	}
	ourProj, ok, err := DecodeProjectJSON(ours, projectID)
	if err != nil {
		return nil, fmt.Errorf("reconcile: decode session-branch project state: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("reconcile: session branch carried no project document")
	}

	baseSlot, ok := slotPtr(&baseProj, kind)
	if !ok {
		return nil, fmt.Errorf("reconcile: no slot for artifact kind %s", kind)
	}
	ourSlot, ok := slotPtr(&ourProj, kind)
	if !ok {
		return nil, fmt.Errorf("reconcile: no slot for artifact kind %s", kind)
	}
	// Overlay the session's OWN slot onto main's document. Every other slot is left as
	// main has it (the whole point: pick up main's concurrent advances), and this slot
	// takes the session-branch value wholesale (the in-flight draft + its review ledger).
	*baseSlot = *ourSlot

	return EncodeProjectJSON(baseProj)
}

// OverlaySlotFromBranchOntoMain is the in-memory twin of ReconcileSlotOntoBase for the
// server's branch-reconcile activity, which already holds decoded Projects (read via
// ReadProject / ReadProjectOnBranch) rather than raw bytes. It mutates `mainProj` in
// place, overlaying the session's OWN slot (kind) from `branchProj`, so the caller can
// commit the reconciled aggregate to the branch tip through the normal branch write path.
func OverlaySlotFromBranchOntoMain(mainProj *Project, branchProj *Project, kind ArtifactKind) error {
	mainSlot, ok := slotPtr(mainProj, kind)
	if !ok {
		return fmt.Errorf("reconcile: no slot for artifact kind %s", kind)
	}
	branchSlot, ok := slotPtr(branchProj, kind)
	if !ok {
		return fmt.Errorf("reconcile: no slot for artifact kind %s", kind)
	}
	*mainSlot = *branchSlot
	return nil
}
