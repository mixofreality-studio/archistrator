package projectdesign

import (
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"go.temporal.io/sdk/workflow"
)

// ===========================================================================
// (C) Phase2AdvanceWorkflow — seals Phase 2 (contract §6.3 C; mirrors
// systemdesign's runPhaseAdvance). The gate: every Phase2RequiredKinds() slot is
// ReviewCommitted AND an option is bound (the committed SdpReview's Recommendation
// is non-empty). No artifactValidationEngine call — there is no Phase-2 verb on the
// frozen surface; the slot-committed + option-bound gate IS the standard check for
// this construction increment (OQ-1/FU-MPD-1: the per-kind Phase-2 validation verbs
// are additive and deferred).
// ===========================================================================

// phaseAdvanceInput is the start payload for Phase2AdvanceWorkflow.
type phaseAdvanceInput struct {
	ProjectID ProjectID
}

func (wf *workflows) Phase2AdvanceWorkflow(ctx workflow.Context, in phaseAdvanceInput) (PhaseAdvanceResult, error) {
	var proj projectstate.Project
	if p, err := wf.readProject(ctx, in.ProjectID); err != nil {
		if !isReadNotFound(err) {
			return PhaseAdvanceResult{}, err
		}
		proj = projectstate.Project{ID: projectstate.ProjectID(in.ProjectID)}
	} else {
		proj = p
	}

	// Gate: every required Phase-2 kind must be Committed, AND an option must be bound.
	var missing []ArtifactKind
	for _, kind := range projectstate.Phase2RequiredKinds() {
		if slotFor(proj, kind).Status != projectstate.ReviewCommitted {
			missing = append(missing, fromPSKind(kind))
		}
	}
	// Option-bound check: the committed SdpReview slot's Model carries a non-empty
	// Recommendation. If the SdpReview slot is itself missing it is already in
	// `missing`; only flag the unbound-option case when the review IS committed.
	if !optionBound(proj) && slotFor(proj, projectstate.KindSdpReview).Status == projectstate.ReviewCommitted {
		missing = append(missing, KindSdpReview)
	}
	if len(missing) > 0 {
		return PhaseAdvanceResult{Advanced: false, MissingArtifacts: missing}, nil
	}

	// Seal Phase 2. AdvancePhase is a MAIN write (Conflict re-read targets main, branch=="").
	if _, err := wf.applyRecovering(ctx, in.ProjectID, "", proj.Version, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ProjectStateAdvancePhase(ctx, projectstate.ProjectID(in.ProjectID), expected)
	}); err != nil {
		return PhaseAdvanceResult{}, err
	}
	return PhaseAdvanceResult{Advanced: true}, nil
}

// optionBound reports whether the project's committed SdpReview binds an option
// (a non-empty Recommendation).
func optionBound(proj projectstate.Project) bool {
	slot := proj.SdpReview
	if slot.Status != projectstate.ReviewCommitted || slot.Model == nil {
		return false
	}
	rev, ok := slot.Model.(*projectstate.SdpReview)
	return ok && rev.Recommendation != ""
}
