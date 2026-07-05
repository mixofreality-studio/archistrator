package projectdesign

// prompts_test.go — unit coverage for the Manager-owned Phase-2 draft-prompt composition.
// The prompts now shrink to role + task + doctrine + comment context: all reads and writes
// of the project state go through the aiarch-state MCP tools, and putDraftModel validates
// the model through the FULL server codec (the F36 Phase-2 shape-stall killer), so the
// prompt no longer carries slot-placement directives or per-kind shape dumps.

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// draftedPhase2Kinds are the Phase-2 artifact kinds the architect role actually DRAFTS through
// architectDraftPrompt (SdpReview is excluded — it is assembled deterministically by the
// workflow, see prompts.go package doc).
var draftedPhase2Kinds = []projectstate.ArtifactKind{
	projectstate.KindPlanningAssumptions,
	projectstate.KindActivityList,
	projectstate.KindNetwork,
	projectstate.KindNormalSolution,
	projectstate.KindSubcriticalSolution,
	projectstate.KindCompressedSolution,
	projectstate.KindDecompressedSolution,
	projectstate.KindRiskModel,
}

// draftFor composes the first-draft prompt for a kind (no feedback, no ledger, no amendment).
func draftFor(kind projectstate.ArtifactKind) string {
	return architectDraftPrompt(kind, projectstate.Project{}, "", nil, 0)
}

// F68, made STRUCTURAL: the prompt no longer carries a slot-placement directive — putDraftModel
// writes to the ambient kind's slot, so the four Solution siblings that share one model type can
// never be positionally cross-written. Every drafted prompt must state the job fixes the slot and
// must NOT carry the old numeric slot-key directive.
func Test_DraftPrompt_NoSlotPlacementDirective(t *testing.T) {
	for _, kind := range draftedPhase2Kinds {
		prompt := draftFor(kind)
		if strings.Contains(prompt, "slot keyed exactly") || strings.Contains(strings.ToUpper(prompt), "SLOT PLACEMENT") {
			t.Fatalf("kind %s: prompt must not carry a slot-placement directive; got:\n%s", kind, prompt)
		}
		if !strings.Contains(prompt, "putDraftModel") {
			t.Fatalf("kind %s: prompt must direct the agent to submit via putDraftModel; got:\n%s", kind, prompt)
		}
		if !strings.Contains(prompt, "never choose a slot") {
			t.Fatalf("kind %s: prompt must state the job fixes the slot; got:\n%s", kind, prompt)
		}
	}
}

// The prompt no longer carries the per-kind typed-shape dump (putDraftModel validates the model
// through the codec, so a wrong shape is rejected in-loop). No drafted prompt may carry the
// SCHEMA CONFORMANCE block, but each must direct reads/writes through the aiarch-state tools.
func Test_DraftPrompt_NoShapeDump_UsesTools(t *testing.T) {
	for _, kind := range draftedPhase2Kinds {
		prompt := draftFor(kind)
		if strings.Contains(prompt, "SCHEMA CONFORMANCE") {
			t.Errorf("%s prompt must not carry the typed-shape dump anymore; got:\n%s", kind, prompt)
		}
		// No hand-editing / JSON-path / git instructions leak into the prompt.
		if strings.Contains(prompt, ".aiarch/state/project.json") || strings.Contains(prompt, ".serviceContracts") {
			t.Errorf("%s prompt must not reference the on-disk state file/schema paths; got:\n%s", kind, prompt)
		}
		// Reads go through getCommittedSlot; the finish is publishDraft.
		if !strings.Contains(prompt, "getCommittedSlot") || !strings.Contains(prompt, "publishDraft") {
			t.Errorf("%s prompt must direct the agent to the aiarch-state tools; got:\n%s", kind, prompt)
		}
	}
}

// The drafting DOCTRINE (the how-to for each kind) survives the shrink — it is what the prompt
// carries besides role + tools. Spot-check the representative doctrine lines per kind.
func Test_DraftPrompt_KeepsDoctrine(t *testing.T) {
	cases := map[projectstate.ArtifactKind]string{
		projectstate.KindPlanningAssumptions: "explicit planning assumptions",
		projectstate.KindNetwork:             "critical path",
		projectstate.KindNormalSolution:      "minimum staffing",
		projectstate.KindSubcriticalSolution: "deliberately understaffed",
		projectstate.KindCompressedSolution:  "shorter duration",
		projectstate.KindRiskModel:           "criticality risk",
	}
	for kind, want := range cases {
		if prompt := draftFor(kind); !strings.Contains(prompt, want) {
			t.Errorf("%s prompt must keep its drafting doctrine (missing %q); got:\n%s", kind, want, prompt)
		}
	}
}

// ActivityList doctrine — the base list is ONE coding activity per component (detailed design and
// construction are internal lifecycle phases of that single activity, NOT separate network nodes);
// integration and noncoding activities remain separate. This doctrine must survive the shrink.
func Test_ActivityListPrompt_OneCodingActivityPerComponent(t *testing.T) {
	prompt := draftFor(projectstate.KindActivityList)
	if !strings.Contains(prompt, "ONE coding activity per component") {
		t.Errorf("activity-list prompt must state one coding activity per component; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "internal lifecycle phases") {
		t.Errorf("activity-list prompt must frame detailed-design/construction as internal lifecycle phases; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "NOT separate network nodes") {
		t.Errorf("activity-list prompt must forbid splitting a component into separate network nodes; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Integration") || !strings.Contains(prompt, "noncoding") {
		t.Errorf("activity-list prompt must keep integration and noncoding activities separate; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "5-day quanta") {
		t.Errorf("activity-list prompt must state effort in 5-day quanta; got:\n%s", prompt)
	}
}

// The redraft prompt weaves in each OPEN review-ledger comment and states the response contract
// via the respondToReviewComment tool (not an in-file reviewThread edit). Addressed/waived
// comments are not listed.
func Test_DraftPrompt_WeavesOpenReviewLedger(t *testing.T) {
	thread := []projectstate.ReviewComment{
		{ID: "r1c1", Anchor: "$.resources", AnchorText: "the resources", Text: "name the contractors", Status: projectstate.ReviewCommentOpen},
		{ID: "r1c2", Text: "already fixed", Status: projectstate.ReviewCommentAddressed, Response: "done"},
	}
	prompt := architectDraftPrompt(projectstate.KindPlanningAssumptions, projectstate.Project{}, "", thread, 0)

	for _, want := range []string{"r1c1", "$.resources", "name the contractors", "respondToReviewComment", "STAYS OPEN"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("open-comment redraft prompt missing %q; got:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "r1c2") {
		t.Errorf("addressed comments must not be listed in the redraft prompt; got:\n%s", prompt)
	}
}

// A first draft (no ledger) carries no review-ledger block.
func Test_DraftPrompt_NoLedgerBlockWhenEmpty(t *testing.T) {
	prompt := draftFor(projectstate.KindNetwork)
	if strings.Contains(prompt, "durable review ledger") {
		t.Errorf("first-draft prompt must not carry a review-ledger block; got:\n%s", prompt)
	}
}

// Non-drafted kinds (the deterministically-assembled SdpReview and Phase-1 kinds) carry no
// shape/enum block — the guidance is scoped to the agent-drafted Phase-2 kinds.
func Test_DraftPrompt_NoShapeBlockOnNonDraftedKinds(t *testing.T) {
	for _, kind := range []projectstate.ArtifactKind{projectstate.KindSdpReview, projectstate.KindMission} {
		if prompt := draftFor(kind); strings.Contains(prompt, "SCHEMA CONFORMANCE") {
			t.Errorf("%s prompt must not carry a typed-shape block; got:\n%s", kind, prompt)
		}
	}
}
