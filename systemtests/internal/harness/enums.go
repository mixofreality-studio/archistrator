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

var systemStageWire = map[sdk.SessionStage]string{
	sdk.SessionStageUnknown: "unknown",
	sdk.StageDrafting:       "drafting",
	sdk.StageAwaitingReview: "awaitingReview",
	sdk.StageRedrafting:     "redrafting",
	sdk.StageCommitted:      "committed",
	sdk.StageWithdrawn:      "withdrawn",
	sdk.StageRefused:        "refused",
	sdk.StageDraftFailed:    "draftFailed",
}

func systemStageName(s sdk.SessionStage) string {
	if name, ok := systemStageWire[s]; ok {
		return name
	}
	return "unknown"
}

// projectStageWire has ONE more stage than the system-design enum
// (assemblingSdp at ordinal 2). ProjectDesignStageAssemblingSDP is HAND-MAPPED
// to "assemblingSdp" — the mechanical lower-first of the SDK varname would give
// "assemblingSDP" (mirrors webApp/scripts/gen-enums.mjs NON_MECHANICAL).
var projectStageWire = map[sdk.ProjectSessionStage]string{
	sdk.ProjectSessionStageUnknown: "unknown",
	sdk.ProjectStageDrafting:       "drafting",
	sdk.ProjectStageAssemblingSDP:  "assemblingSdp",
	sdk.ProjectStageAwaitingReview: "awaitingReview",
	sdk.ProjectStageRedrafting:     "redrafting",
	sdk.ProjectStageCommitted:      "committed",
	sdk.ProjectStageWithdrawn:      "withdrawn",
	sdk.ProjectStageRefused:        "refused",
	sdk.ProjectStageDraftFailed:    "draftFailed",
}

func projectStageName(s sdk.ProjectSessionStage) string {
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

// ---------------------------------------------------------------------------
// STAGE 4a — THE ACTIVITY/TASK ADDRESS OF AN ARTIFACT KIND
//
// The twelve-op deliveryManager addresses work by {activityID, taskID}, not by
// artifact kind: a draft is a DISPATCH task of a design activity and a verdict is
// its REVIEW task. The tables below are the harness's copy of that address, and
// they mirror the committed method-assets lifecycles exactly — requirements owns
// the four Phase-1 drafts, architecture owns the System draft, and projectDesign
// owns the single computed sdpReview gate.
// ---------------------------------------------------------------------------

// designActivityFor is the activity that owns an artifact kind's tasks.
func designActivityFor(kind string) sdk.ActivityID {
	switch kind {
	case "mission", "glossary", "scrubbedRequirements", "volatilities", "coreUseCases":
		return "requirements"
	case "system", "operationalConcepts", "standardCheck":
		return "architecture"
	default:
		return "projectDesign"
	}
}

// draftTaskFor is the DISPATCH task that produces an artifact kind.
func draftTaskFor(kind string) string {
	if t, ok := designDraftTasks[kind]; ok {
		return t
	}
	return "sdpReview"
}

// reviewTaskFor is the REVIEW task that judges an artifact kind's draft.
func reviewTaskFor(kind string) string {
	if t, ok := designReviewTasks[kind]; ok {
		return t
	}
	return "sdpReview"
}

var designDraftTasks = map[string]string{
	"mission":      "missionDraft",
	"glossary":     "glossaryDraft",
	"volatilities": "volatilitiesDraft",
	"coreUseCases": "coreUseCasesDraft",
	"system":       "architectureDraft",
}

var designReviewTasks = map[string]string{
	"mission":      "missionReview",
	"glossary":     "glossaryReview",
	"volatilities": "volatilitiesReview",
	"coreUseCases": "coreUseCasesReview",
	"system":       "architectureReview",
}

// sdpReviewDecision maps the SDP vocabulary onto the merged ReviewDecision: the M0
// gate's commit is an approve and its reject-all is a reject.
func sdpReviewDecision(name string) sdk.ReviewDecision {
	if name == "rejectAll" {
		return sdk.ReviewReject
	}
	return sdk.ReviewApprove
}

// phaseReviewDecision maps the construction gate's vocabulary onto the merged
// ReviewDecision: approve stays approve and sendBack is a reject.
func phaseReviewDecision(name string) sdk.ReviewDecision {
	if name == "sendBack" {
		return sdk.ReviewReject
	}
	return sdk.ReviewApprove
}

// activityViewStateName is the ActivityView's coarse state as its raw wire string —
// the enum is a STRING enum (notStarted | running | awaitingReview | done | failed), so
// the value IS the name and no ordinal table is needed.
func activityViewStateName(s sdk.ActivityViewState) string { return string(s) }

// ---------------------------------------------------------------------------
// STAGE 4a — the reverse of designActivityFor/draftTaskFor: the plan's steps
// address a TASK, and the harness's transport-agnostic vocabulary still speaks
// artifact kinds, so the runner needs the task -> kind direction too.
// ---------------------------------------------------------------------------

// ArtifactKindForTask is the artifact a design lifecycle task is about — "" when the
// task id names no design artifact (a construction lifecycle phase, which the
// construction gate takes verbatim).
func ArtifactKindForTask(taskID string) string {
	for kind, draft := range designDraftTasks {
		if draft == taskID {
			return kind
		}
	}
	for kind, review := range designReviewTasks {
		if review == taskID {
			return kind
		}
	}
	if taskID == "sdpReview" {
		return "sdpReview"
	}
	return ""
}

// IsPhase2ArtifactKind reports whether a kind belongs to Project Design (Phase 2) — the
// split QueryProjectView's session arm routes on, and the one the harness's two
// GetSessionState reads mirror.
func IsPhase2ArtifactKind(kind string) bool {
	return int(artifactKind(kind)) >= int(sdk.KindPlanningAssumptions)
}

// ReviewAdvanceOrdinal is the merged ReviewDecision's phase-seal ordinal, exported so
// the generated-table runner can recognise an advance without re-declaring the enum.
const ReviewAdvanceOrdinal = sdk.ReviewAdvance
