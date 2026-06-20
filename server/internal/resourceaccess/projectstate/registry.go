package projectstate

// AllArtifactKinds returns every defined ArtifactKind in the stable slot order
// (Phase 1 then Phase 2). This is the single enumeration of the closed set —
// used by codecs and the coverage test to ensure a new kind added to the iota
// block is also covered by NewModelForKind.
func AllArtifactKinds() []ArtifactKind {
	return []ArtifactKind{
		// Phase 1
		KindMission,
		KindGlossary,
		KindScrubbedRequirements,
		KindVolatilities,
		KindCoreUseCases,
		KindSystem,
		KindOperationalConcepts,
		KindStandardCheck,
		// Phase 2
		KindPlanningAssumptions,
		KindActivityList,
		KindNetwork,
		KindNormalSolution,
		KindSubcriticalSolution,
		KindCompressedSolution,
		KindDecompressedSolution,
		KindRiskModel,
		KindSdpReview,
	}
}

// NewModelForKind returns a freshly-allocated zero-value concrete pointer that
// implements ArtifactModel for kind, suitable for JSON unmarshalling into. For
// the four Solution slot kinds (KindNormalSolution, KindSubcriticalSolution,
// KindCompressedSolution, KindDecompressedSolution) the returned *Solution has
// SlotKind pre-set to kind so the slot identity is preserved across a codec
// round-trip even if the persisted JSON omits the field.
//
// Returns (nil, false) for any kind not in AllArtifactKinds().
// This is the canonical factory: both the Temporal payload codec and the JSONB
// codec delegate here so a new ArtifactKind missed from this switch is caught
// by TestNewModelForKindCoversAllKinds rather than silently crashing at runtime.
func NewModelForKind(kind ArtifactKind) (ArtifactModel, bool) {
	switch kind {
	case KindMission:
		return &MissionStatement{}, true
	case KindGlossary:
		return &Glossary{}, true
	case KindScrubbedRequirements:
		return &ScrubbedRequirements{}, true
	case KindVolatilities:
		return &Volatilities{}, true
	case KindCoreUseCases:
		return &CoreUseCases{}, true
	case KindSystem:
		return &System{}, true
	case KindOperationalConcepts:
		return &OperationalConcepts{}, true
	case KindStandardCheck:
		return &StandardCheck{}, true
	case KindPlanningAssumptions:
		return &PlanningAssumptions{}, true
	case KindActivityList:
		return &ActivityList{}, true
	case KindNetwork:
		return &Network{}, true
	case KindNormalSolution,
		KindSubcriticalSolution,
		KindCompressedSolution,
		KindDecompressedSolution:
		return &Solution{SlotKind: kind}, true
	case KindRiskModel:
		return &RiskModel{}, true
	case KindSdpReview:
		return &SdpReview{}, true
	default:
		return nil, false
	}
}
