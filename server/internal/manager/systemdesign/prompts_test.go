package systemdesign

// prompts_test.go — unit coverage for the Manager-owned prompt composition, focused on
// the research-corpus POINTER contract (QA finding F11). The mission-draft prompt must
// POINT the drafting Action at the corpus committed in .aiarch/state/project.json (by
// JSON path + per-source title) and must NEVER inline the (book-sized) source content —
// inlining blew the Temporal payload budget and GitHub's 64KB workflow_dispatch input cap.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// F68, made STRUCTURAL: the prompt no longer carries a slot-placement directive at all —
// putDraftModel writes to the ambient kind's slot, so the agent can never mis-place it
// positionally. Every Phase-1 draft prompt must state that the job fixes the slot and must
// NOT carry the old numeric slot-key directive.
func Test_DraftPrompt_NoSlotPlacementDirective(t *testing.T) {
	for _, kind := range projectstate.Phase1RequiredKinds() {
		prompt := architectDraftPrompt(kind, projectstate.Project{}, ReviewFeedback{}, nil, 0)
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

// projWithResearch builds a minimal Project carrying a research corpus whose Content is a
// distinctive, book-sized sentinel we can assert never leaks into the composed prompt.
func projWithResearch(sources ...projectstate.ResearchSourceRef) projectstate.Project {
	return projectstate.Project{
		ID:       projectstate.ProjectID("11111111-1111-1111-1111-111111111111"),
		Research: projectstate.ResearchCorpus{Sources: sources},
	}
}

// The mission-draft prompt directs the agent to the research corpus via the aiarch-state
// tools (listResearchSources / getResearchSource) and never inlines content or enumerates
// file paths (the corpus can be book-sized; F11/F42 guard).
func Test_MissionPrompt_PointsAtResearchTools_NeverInlinesContent(t *testing.T) {
	prompt := architectDraftPrompt(
		projectstate.KindMission,
		projWithResearch(
			projectstate.ResearchSourceRef{Title: "Founder brief", Path: ".aiarch/state/research/00-founder-brief.txt", ContentBytes: 620000},
			projectstate.ResearchSourceRef{Title: "Competitor analysis", Path: ".aiarch/state/research/01-competitor-analysis.txt", ContentBytes: 42000},
		),
		ReviewFeedback{},
		nil,
		0,
	)

	// The prompt must direct the agent to the research tools, not a file/JSON path.
	if !strings.Contains(prompt, "listResearchSources") || !strings.Contains(prompt, "getResearchSource") {
		t.Errorf("prompt must direct the agent to listResearchSources + getResearchSource; got:\n%s", prompt)
	}
	// No file paths and no JSON path are enumerated in the prompt anymore.
	if strings.Contains(prompt, ".aiarch/state/research/") || strings.Contains(prompt, ".research.Sources") {
		t.Errorf("prompt must not enumerate research file/JSON paths (the tools do that); got:\n%s", prompt)
	}
	// The (book-sized) content must never be inline.
	if strings.Contains(prompt, "expect any research content inline") == false {
		t.Errorf("prompt must state research content is not inline; got:\n%s", prompt)
	}
}

// The mission draft prompt must NOT instruct the architect to express the mission in
// component / system-architecture terms and must NOT pre-decide a decomposition
// (founder ruling 2026-07-05, QA finding F27). The old prompt said the mission "is
// expressed in terms of the system's COMPONENTS" — that contradicted the PM critique's
// doctrine and made the draft<->critique loop non-convergent. Guard against regressing to it.
func Test_MissionPrompt_ForbidsComponentLanguage(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindMission, projWithResearch(
		projectstate.ResearchSourceRef{Title: "Founder brief", Path: ".aiarch/state/research/00-founder-brief.txt"},
	), ReviewFeedback{}, nil, 0)

	// The prompt must NOT tell the architect to express the mission in component terms.
	if strings.Contains(prompt, "terms of the system's COMPONENTS") {
		t.Errorf("mission prompt must not instruct component-language framing (F27 regression); got:\n%s", prompt)
	}
	// It must instead direct the architect to the business capability / user-facing value.
	if !strings.Contains(prompt, "BUSINESS CAPABILITY") || !strings.Contains(prompt, "USER-FACING VALUE") {
		t.Errorf("mission prompt must frame the mission as business capability and user-facing value; got:\n%s", prompt)
	}
	// It must forbid architecture / decomposition terminology explicitly.
	if !strings.Contains(prompt, "MUST NOT use the words component") {
		t.Errorf("mission prompt must forbid component/architecture terminology; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "volatility analysis") {
		t.Errorf("mission prompt must defer structural boundaries to volatility analysis; got:\n%s", prompt)
	}
}

// The PM critique for the Mission must ENFORCE the same no-component-language doctrine the
// draft prompt instructs, so the draft<->critique loop converges (F27). It must not read
// as generic ratification for the mission kind.
func Test_MissionCritiquePrompt_EnforcesNoComponentLanguage(t *testing.T) {
	critique := pmCritiquePrompt(projectstate.KindMission, nil)
	if !strings.Contains(critique, "component") {
		t.Errorf("mission critique must name the component-language rule it enforces; got:\n%s", critique)
	}
	if !strings.Contains(critique, "volatility analysis") {
		t.Errorf("mission critique must defer decomposition to volatility analysis; got:\n%s", critique)
	}

	// A non-mission critique carries no mission-specific doctrine (generic ratification only).
	glossary := pmCritiquePrompt(projectstate.KindGlossary, nil)
	if strings.Contains(glossary, "Mission doctrine you MUST enforce") {
		t.Errorf("non-mission critique must not carry the mission doctrine block; got:\n%s", glossary)
	}
}

// The IsZero guard is preserved: with no corpus, no research block is emitted at all.
func Test_MissionPrompt_EmptyCorpus_EmitsNoResearchBlock(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindMission, projWithResearch(), ReviewFeedback{}, nil, 0)
	if strings.Contains(prompt, "Research corpus") {
		t.Errorf("empty corpus must emit no research block; got:\n%s", prompt)
	}
}

// writeResearch is the composition unit under the prompt: it points, never inlines, and
// honours the IsZero guard. Direct coverage so the contract holds independent of the
// mission-prompt wrapper.
func Test_writeResearch_ToolForm(t *testing.T) {
	var b strings.Builder
	writeResearch(&b, projectstate.ResearchCorpus{Sources: []projectstate.ResearchSourceRef{
		{Title: "Customer interviews", Path: ".aiarch/state/research/00-customer-interviews.txt", ContentBytes: 12345},
	}})
	out := b.String()
	// It directs the agent to the research tools and inlines neither paths nor content.
	if !strings.Contains(out, "listResearchSources") || !strings.Contains(out, "getResearchSource") {
		t.Errorf("writeResearch must direct the agent to the research tools; got:\n%s", out)
	}
	if strings.Contains(out, ".aiarch/state/research/") || strings.Contains(out, ".research.Sources") {
		t.Errorf("writeResearch must not enumerate research paths; got:\n%s", out)
	}

	var empty strings.Builder
	writeResearch(&empty, projectstate.ResearchCorpus{})
	if empty.Len() != 0 {
		t.Errorf("IsZero guard broken: empty corpus wrote %q", empty.String())
	}
}

// wireNameOf marshals a projectstate enum value to its canonical camelCase wire name via
// the SAME MarshalJSON the server codec uses, so the enum-conformance assertions below are
// derived from the source of truth (projectstate/enumjson.go) rather than a hand-copy that
// could drift from it.
func wireNameOf(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal enum %v: %v", v, err)
	}
	return strings.Trim(string(b), `"`)
}

// QA F36 is now handled by putDraftModel's in-loop codec validation, so the CoreUseCases
// prompt no longer carries the closed-enum wire-name dump (a schema dump). It must NOT carry
// the SCHEMA CONFORMANCE block anymore — the agent learns the exact enum values from
// putDraftModel's rejection, not a prompt-side enumeration.
func Test_CoreUseCasesPrompt_NoClosedEnumDump(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindCoreUseCases, projectstate.Project{}, ReviewFeedback{}, nil, 0)
	if strings.Contains(prompt, "SCHEMA CONFORMANCE") {
		t.Errorf("prompt must not carry the closed-enum schema dump anymore; got:\n%s", prompt)
	}
	// The prompt still carries the drafting DOCTRINE (how to abstract core use cases).
	if !strings.Contains(prompt, "ABSTRACTION") {
		t.Errorf("prompt must still carry the core-use-case drafting doctrine; got:\n%s", prompt)
	}
}

// Founder ruling 2026-07-05: EVERY use case (core AND supporting) must carry a non-empty
// activity diagram — a start node plus at least one action step. The CoreUseCases draft
// prompt must state that hard requirement and must NOT carry the old "purely linear ⇒ leave
// activity null" exemption that let the committed gtdapp draft ship diagram-less core use
// cases.
func Test_CoreUseCasesPrompt_RequiresActivityDiagramForEveryUseCase(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindCoreUseCases, projectstate.Project{}, ReviewFeedback{}, nil, 0)

	// The retired exemption must be gone: no wording that a linear use case may leave
	// "activity" null / omit the diagram.
	for _, banned := range []string{
		"may leave \"activity\" null",
		"purely linear use case may leave",
	} {
		if strings.Contains(prompt, banned) {
			t.Errorf("CoreUseCases prompt must not carry the retired null-activity exemption %q; got:\n%s", banned, prompt)
		}
	}

	// The hard requirement must be present: every use case carries a non-empty activity,
	// core AND supporting/nonCore, with at minimum a start node and an action step.
	lower := strings.ToLower(prompt)
	for _, required := range []string{
		"every use case",
		"non-empty",
		"incomplete draft",
		"start node",
		"action node",
	} {
		if !strings.Contains(lower, required) {
			t.Errorf("CoreUseCases prompt must state the activity-diagram requirement (missing %q); got:\n%s", required, prompt)
		}
	}
	// The requirement must explicitly reach supporting/nonCore use cases, not only core.
	if !strings.Contains(lower, "supporting") && !strings.Contains(lower, "noncore") {
		t.Errorf("CoreUseCases prompt must extend the activity requirement to supporting (nonCore) use cases; got:\n%s", prompt)
	}
}

// A kind whose drafted model carries NO closed enum (Mission) must NOT get the enum block —
// the guidance is scoped so unrelated prompts stay lean.
func Test_MissionPrompt_HasNoClosedEnumBlock(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindMission, projectstate.Project{}, ReviewFeedback{}, nil, 0)
	if strings.Contains(prompt, "SCHEMA CONFORMANCE") {
		t.Errorf("mission prompt must not carry the closed-enum block; got:\n%s", prompt)
	}
}

// The System draft prompt no longer dumps the ComponentKind / CallMode enum wire names
// (putDraftModel validates them). It must NOT carry the SCHEMA CONFORMANCE block, but it must
// still carry the decomposition DOCTRINE.
func Test_SystemPrompt_NoClosedEnumDump(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindSystem, projectstate.Project{}, ReviewFeedback{}, nil, 0)
	if strings.Contains(prompt, "SCHEMA CONFORMANCE") {
		t.Errorf("System prompt must not carry the closed-enum schema dump anymore; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Decompose the system by VOLATILITY") {
		t.Errorf("System prompt must still carry the decomposition doctrine; got:\n%s", prompt)
	}
}

// The StandardCheck draft task is SCOPED to the system-design gate (founder ruling
// 2026-07-05, observed on gtdapp: 52 pass / 59 waived). The Phase-1 check must walk ONLY
// the design directives + the System Design guideline section, and must EXCLUDE the
// project-design / project-tracking directives and guideline sections entirely — it must
// NOT emit them as waived (phase-inapplicable is out-of-scope, not a conscious exception).
// WAIVED stays reserved for genuine, justified exceptions to in-scope items. Those
// out-of-scope items are checked at the Phase-2 SDP gate.
func Test_StandardCheckPrompt_ScopedToSystemDesignGate(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindStandardCheck, projectstate.Project{}, ReviewFeedback{}, nil, 0)

	// It must scope the walk to the in-scope system-design items (directives + SYS section).
	for _, want := range []string{
		"ONLY the items checkable",
		"design directives",
		"System Design guideline section",
		"decompose based on volatility",
		"closed-layer rules",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("StandardCheck prompt missing in-scope marker %q; got:\n%s", want, prompt)
		}
	}

	// It must EXPLICITLY exclude the project-design / project-tracking parts as out of scope,
	// and forbid emitting them as waived — routing them to the Phase-2 SDP gate instead.
	for _, want := range []string{
		"OUT OF SCOPE",
		"do NOT emit them as waived",
		"phase-inapplicable",
		"Phase-2 SDP gate",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("StandardCheck prompt missing scope-exclusion marker %q; got:\n%s", want, prompt)
		}
	}

	// The old blanket "Walk the App C design-standard checklist" framing (walk the WHOLE
	// standard, waive later-phase items) must be gone — that is what produced the waiver
	// pollution the founder ruled against.
	if strings.Contains(prompt, "Walk the App C design-standard checklist.") {
		t.Errorf("StandardCheck prompt must not carry the old whole-standard walk framing; got:\n%s", prompt)
	}

	// WAIVED must be framed as reserved for genuine in-scope exceptions, not phase scope.
	if !strings.Contains(prompt, "reserved for genuine") {
		t.Errorf("StandardCheck prompt must reserve WAIVED for genuine in-scope exceptions; got:\n%s", prompt)
	}

	// The closed-enum status block (pass/waived/fail) is still carried for this kind.
	statuses := []projectstate.CheckStatus{
		projectstate.CheckPass, projectstate.CheckWaived, projectstate.CheckFail,
	}
	for _, s := range statuses {
		name := wireNameOf(t, s)
		if !strings.Contains(prompt, name) {
			t.Errorf("StandardCheck prompt missing CheckStatus wire name %q; got:\n%s", name, prompt)
		}
	}
}

// The redraft prompt must weave in each OPEN review-ledger comment (id + anchor + anchorText
// + text) and state the response-carrier contract, and must NOT list addressed/waived
// comments (review-ledger §3).
func Test_ArchitectDraftPrompt_WeavesOpenReviewLedger(t *testing.T) {
	thread := []projectstate.ReviewComment{
		{ID: "r1c1", Anchor: "$.vision", AnchorText: "the old vision", Text: "sharpen the vision", Status: projectstate.ReviewCommentOpen},
		{ID: "r1c2", Anchor: "$.mission", AnchorText: "the mission text", Text: "already fixed", Status: projectstate.ReviewCommentAddressed, Response: "done"},
		{ID: "r1c3", Anchor: "", AnchorText: "", Text: "dismissed nit", Status: projectstate.ReviewCommentWaived},
	}
	prompt := architectDraftPrompt(projectstate.KindMission, projWithResearch(), ReviewFeedback{}, thread, 0)

	// The OPEN comment is woven in with its id, anchor, anchorText, and text.
	for _, want := range []string{"r1c1", "$.vision", "the old vision", "sharpen the vision"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("open-comment prompt missing %q; got:\n%s", want, prompt)
		}
	}
	// The response contract is stated via the respondToReviewComment tool.
	for _, want := range []string{"respondToReviewComment", "response", "STAYS OPEN"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("response contract missing %q; got:\n%s", want, prompt)
		}
	}
	// Addressed + waived comments are NOT listed (only open ones block/redraft).
	if strings.Contains(prompt, "r1c2") || strings.Contains(prompt, "r1c3") {
		t.Errorf("non-open comments must not be listed in the redraft prompt; got:\n%s", prompt)
	}
}

// The first draft (no ledger) must not emit a review-ledger block.
func Test_ArchitectDraftPrompt_NoLedgerBlockWhenEmpty(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindMission, projWithResearch(), ReviewFeedback{}, nil, 0)
	if strings.Contains(prompt, "durable review ledger") {
		t.Errorf("first-draft prompt must not carry a review-ledger block; got:\n%s", prompt)
	}
}
