package harness

// enums.go is the SINGLE source that bridges the harness's transport-agnostic
// STRING vocabulary (the wire enum names the use-case steps assert on:
// "mission", "drafting", "assemblingSdp", ...) to the generated SDK's TYPED enum
// consts (internal/sdk). The SDK owns the wire ordinals now — every table below
// keys on an sdk const, so there is NO hand-maintained ordinal literal anywhere
// in the harness (the 11 ordinal tables httptransport.go used to carry are
// deleted). Both wire transports (httptransport.go, mcptransport.go) delegate
// their enum encode/decode here, so the two surfaces speak identical strings.
//
// Wire names are the RAW wire vocabulary (e.g. PipelineCancelled stays
// "cancelled", RuntimeStatusHealthy stays "healthy") — the harness asserts on
// the literal contract, unlike webApp which folds/renames some for display.
// Only ProjectSessionStage ordinal 2 is hand-mapped: the SDK varname
// ProjectDesignStageAssemblingSDP would mechanically lower-first to
// "assemblingSDP", but the wire contract is "assemblingSdp".

import (
	"github.com/mixofreality-studio/archistrator/systemtests/internal/sdk"
)

// TestOwner is the fixed OwnerScope the harness mints every project under. Owner
// is a required, non-empty CreateProject arg but is NOT consulted by
// authenticatedOnlyPDP (any authenticated principal may act on any resource —
// see server/cmd/server/authz.go) and the Transport interface's CreateProject
// has no owner parameter, so a single constant value is sufficient for a
// black-box test. Exported so a test can pass the SAME scope to ListProjects.
const TestOwner = "systemtest"

// testOwner is the unexported alias every existing call site in this package uses.
const testOwner = TestOwner

// invert flips a const→wire map into a wire→const map for the encode direction.
func invert[K comparable, V comparable](m map[K]V) map[V]K {
	out := make(map[V]K, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}

// --- ArtifactKind (shared by system-design + project-design) -----------------

var artifactKindWire = map[sdk.ArtifactKind]string{
	sdk.KindMission:              "mission",
	sdk.KindGlossary:             "glossary",
	sdk.KindScrubbedRequirements: "scrubbedRequirements",
	sdk.KindVolatilities:         "volatilities",
	sdk.KindCoreUseCases:         "coreUseCases",
	sdk.KindSystem:               "system",
	sdk.KindOperationalConcepts:  "operationalConcepts",
	sdk.KindStandardCheck:        "standardCheck",
	sdk.KindPlanningAssumptions:  "planningAssumptions",
	sdk.KindActivityList:         "activityList",
	sdk.KindNetwork:              "network",
	sdk.KindNormalSolution:       "normalSolution",
	sdk.KindSubcriticalSolution:  "subcriticalSolution",
	sdk.KindCompressedSolution:   "compressedSolution",
	sdk.KindDecompressedSolution: "decompressedSolution",
	sdk.KindRiskModel:            "riskModel",
	sdk.KindSdpReview:            "sdpReview",
}

var artifactKindByWire = invert(artifactKindWire)

// artifactKind encodes a wire kind name into the SDK enum a request expects. An
// unknown name maps to KindMission (0) — callers only pass known names.
func artifactKind(name string) sdk.ArtifactKind { return artifactKindByWire[name] }

// artifactKindNameOf decodes an SDK ArtifactKind into its wire name, defaulting
// to "mission" for an out-of-range value (matches the retired ordinal table).
func artifactKindNameOf(k sdk.ArtifactKind) string {
	if name, ok := artifactKindWire[k]; ok {
		return name
	}
	return "mission"
}

// ArtifactKindName decodes a plan step's numeric ArtifactKind ordinal (0..16,
// shared by systemDesignManager and projectDesignManager) into the wire kind
// name a Transport method's `kind string` parameter expects.
func ArtifactKindName(ordinal int) string { return artifactKindNameOf(sdk.ArtifactKind(ordinal)) }

// --- ReviewDecision ----------------------------------------------------------

var reviewDecisionWire = map[sdk.ReviewDecision]string{
	sdk.ReviewApprove:  "approve",
	sdk.ReviewReject:   "reject",
	sdk.ReviewWithdraw: "withdraw",
}

var reviewDecisionByWire = invert(reviewDecisionWire)

func reviewDecision(name string) sdk.ReviewDecision { return reviewDecisionByWire[name] }

// ReviewDecisionName decodes a plan step's numeric ReviewDecision ordinal (0
// unknown,1 approve,2 reject,3 withdraw) into the decision name
// Transport.SubmitReview / SubmitProjectReview expect.
func ReviewDecisionName(ordinal int) string { return reviewDecisionWire[sdk.ReviewDecision(ordinal)] }

// --- SDPDecision -------------------------------------------------------------

var sdpDecisionWire = map[sdk.SDPDecision]string{
	sdk.SDPCommit:    "commit",
	sdk.SDPRejectAll: "rejectAll",
}

var sdpDecisionByWire = invert(sdpDecisionWire)

func sdpDecision(name string) sdk.SDPDecision { return sdpDecisionByWire[name] }

// SDPDecisionName decodes a plan step's numeric SDPDecision ordinal (0
// unknown,1 commit,2 rejectAll) into the decision name
// Transport.SubmitSDPDecision expects.
func SDPDecisionName(ordinal int) string { return sdpDecisionWire[sdk.SDPDecision(ordinal)] }

// --- PhaseDecision (construction) --------------------------------------------

var phaseDecisionWire = map[sdk.PhaseDecision]string{
	sdk.PhaseApprove:  "approve",
	sdk.PhaseSendBack: "sendBack",
}

var phaseDecisionByWire = invert(phaseDecisionWire)

func phaseDecision(name string) sdk.PhaseDecision { return phaseDecisionByWire[name] }

// --- DesiredStateReason / PatchKind (operations) -----------------------------

var desiredStateReasonWire = map[sdk.DesiredStateReason]string{
	sdk.ReasonDeployAfterConstruction: "deployAfterConstruction",
	sdk.ReasonOperator:                "operator",
	sdk.ReasonAutoscale:               "autoscale",
	sdk.ReasonDelinquency:             "delinquency",
}

var desiredStateReasonByWire = invert(desiredStateReasonWire)

func desiredStateReason(name string) sdk.DesiredStateReason { return desiredStateReasonByWire[name] }

var patchKindWire = map[sdk.PatchKind]string{
	sdk.PatchFullBundle: "fullBundle",
	sdk.PatchScale:      "scale",
	sdk.PatchPolicy:     "policy",
}

var patchKindByWire = invert(patchKindWire)

func patchKind(name string) sdk.PatchKind { return patchKindByWire[name] }

// --- stage decoders (response-only) ------------------------------------------

var systemStageWire = map[sdk.SystemDesignSessionStage]string{
	sdk.SystemDesignSessionStageUnknown: "unknown",
	sdk.SystemDesignStageDrafting:       "drafting",
	sdk.SystemDesignStageAwaitingReview: "awaitingReview",
	sdk.SystemDesignStageRedrafting:     "redrafting",
	sdk.SystemDesignStageCommitted:      "committed",
	sdk.SystemDesignStageWithdrawn:      "withdrawn",
	sdk.SystemDesignStageRefused:        "refused",
	sdk.SystemDesignStageDraftFailed:    "draftFailed",
}

func systemStageName(s sdk.SystemDesignSessionStage) string {
	if name, ok := systemStageWire[s]; ok {
		return name
	}
	return "unknown"
}

// projectStageWire has ONE more stage than the system-design enum
// (assemblingSdp at ordinal 2). ProjectDesignStageAssemblingSDP is HAND-MAPPED
// to "assemblingSdp" — the mechanical lower-first of the SDK varname would give
// "assemblingSDP" (mirrors webApp/scripts/gen-enums.mjs NON_MECHANICAL).
var projectStageWire = map[sdk.ProjectDesignSessionStage]string{
	sdk.ProjectDesignSessionStageUnknown: "unknown",
	sdk.ProjectDesignStageDrafting:       "drafting",
	sdk.ProjectDesignStageAssemblingSDP:  "assemblingSdp",
	sdk.ProjectDesignStageAwaitingReview: "awaitingReview",
	sdk.ProjectDesignStageRedrafting:     "redrafting",
	sdk.ProjectDesignStageCommitted:      "committed",
	sdk.ProjectDesignStageWithdrawn:      "withdrawn",
	sdk.ProjectDesignStageRefused:        "refused",
	sdk.ProjectDesignStageDraftFailed:    "draftFailed",
}

func projectStageName(s sdk.ProjectDesignSessionStage) string {
	if name, ok := projectStageWire[s]; ok {
		return name
	}
	return "unknown"
}

var constructionStageWire = map[sdk.ConstructionStage]string{
	sdk.ConstructionStageUnknown: "unknown",
	sdk.StageDispatching:         "dispatching",
	sdk.StagePipelineRunning:     "pipelineRunning",
	sdk.StageReviewing:           "reviewing",
	sdk.StageAwaitingTakeover:    "awaitingTakeover",
	sdk.StagePaused:              "paused",
	sdk.StageExited:              "exited",
	sdk.StageAwaitingApproval:    "awaitingApproval",
}

func constructionStageName(s sdk.ConstructionStage) string {
	if name, ok := constructionStageWire[s]; ok {
		return name
	}
	return "unknown"
}

var pipelinePhaseWire = map[sdk.PipelinePhase]string{
	sdk.PipelinePhaseUnknown: "unknown",
	sdk.PipelinePending:      "pending",
	sdk.PipelineRunning:      "running",
	sdk.PipelineSucceeded:    "succeeded",
	sdk.PipelineFailed:       "failed",
	sdk.PipelineCancelled:    "cancelled",
}

func pipelinePhaseName(p sdk.PipelinePhase) string {
	if name, ok := pipelinePhaseWire[p]; ok {
		return name
	}
	return "unknown"
}

var runtimeStatusWire = map[sdk.RuntimeStatusSeam]string{
	sdk.RuntimeStatusUnknown:   "unknown",
	sdk.RuntimeStatusPending:   "pending",
	sdk.RuntimeStatusHealthy:   "healthy",
	sdk.RuntimeStatusDegraded:  "degraded",
	sdk.RuntimeStatusWithdrawn: "withdrawn",
}

func runtimeStatusName(r sdk.RuntimeStatusSeam) string {
	if name, ok := runtimeStatusWire[r]; ok {
		return name
	}
	return "unknown"
}

// decodeMissingArtifacts decodes a PhaseAdvanceResult's []ArtifactKind into the
// wire kind names a caller asserts on.
func decodeMissingArtifacts(kinds []sdk.ArtifactKind) []string {
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, artifactKindNameOf(k))
	}
	return out
}

// strPtrVal dereferences an optional wire string pointer, defaulting to "".
func strPtrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
