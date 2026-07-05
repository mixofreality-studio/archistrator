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

// projWithResearch builds a minimal Project carrying a research corpus whose Content is a
// distinctive, book-sized sentinel we can assert never leaks into the composed prompt.
func projWithResearch(sources ...projectstate.ResearchSourceRef) projectstate.Project {
	return projectstate.Project{
		ID:       projectstate.ProjectID("11111111-1111-1111-1111-111111111111"),
		Research: projectstate.ResearchCorpus{Sources: sources},
	}
}

// The mission-draft prompt POINTS at each corpus source's FILE PATH and lists each source
// TITLE, but the source CONTENT must not appear inline (F42 files-not-JSON; F11 guard). The
// persisted corpus carries only {Title, Path} pointers — content lives in the repo files.
func Test_MissionPrompt_PointsAtResearchFiles_NeverInlinesContent(t *testing.T) {
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

	// The prompt must POINT at each source's FILE PATH (the file-path pointer form).
	if !strings.Contains(prompt, ".aiarch/state/research/00-founder-brief.txt") ||
		!strings.Contains(prompt, ".aiarch/state/research/01-competitor-analysis.txt") {
		t.Errorf("prompt must point at each source's research file path; got:\n%s", prompt)
	}
	// It must direct the agent to read each source from its FILE (not a project.json path).
	if !strings.Contains(prompt, "FILE") {
		t.Errorf("prompt must instruct reading each source from its file; got:\n%s", prompt)
	}
	// The old JSON-path pointer form is gone (F42).
	if strings.Contains(prompt, ".research.Sources") {
		t.Errorf("prompt must not use the old .research.Sources JSON-path form (F42); got:\n%s", prompt)
	}
	// The prompt must list each source TITLE so the agent knows what is available to read.
	if !strings.Contains(prompt, "Founder brief") || !strings.Contains(prompt, "Competitor analysis") {
		t.Errorf("prompt must list each source title; got:\n%s", prompt)
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
func Test_writeResearch_PointerForm(t *testing.T) {
	var b strings.Builder
	writeResearch(&b, projectstate.ResearchCorpus{Sources: []projectstate.ResearchSourceRef{
		{Title: "Customer interviews", Path: ".aiarch/state/research/00-customer-interviews.txt", ContentBytes: 12345},
	}})
	out := b.String()
	if !strings.Contains(out, "Customer interviews") {
		t.Errorf("writeResearch must list the source title; got:\n%s", out)
	}
	// F42 file-path pointer form: title → file path, and NOT the old JSON-path form.
	if !strings.Contains(out, ".aiarch/state/research/00-customer-interviews.txt") {
		t.Errorf("writeResearch must point at the source's research file path; got:\n%s", out)
	}
	if strings.Contains(out, ".research.Sources") {
		t.Errorf("writeResearch must not use the old .research.Sources JSON-path form (F42); got:\n%s", out)
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

// QA F36: the CoreUseCases draft prompt must enumerate the CLOSED-ENUM wire names (Trigger,
// Classification) and carry the schema-conformance pointer. The root cause of F36 was free
// prose committed into the "trigger" closed enum — the CI validate check accepted it (its
// Go mirror types the field as a free string) but the server codec rejected it on read-back.
// Telling the drafting agent the exact allowed wire names here is the only pre-read-back
// defense. The expected names are DERIVED from the codec so this stays in lockstep with it.
func Test_CoreUseCasesPrompt_EnumeratesClosedEnumWireNames(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindCoreUseCases, projectstate.Project{}, ReviewFeedback{}, nil, 0)

	// The schema-conformance pointer line: point at the checked-out state + name the enum rule.
	if !strings.Contains(prompt, "SCHEMA CONFORMANCE") {
		t.Errorf("prompt must carry the SCHEMA CONFORMANCE pointer line; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, ".aiarch/state/project.json") {
		t.Errorf("prompt must point at the checked-out state (.aiarch/state/project.json); got:\n%s", prompt)
	}
	if !strings.Contains(strings.ToLower(prompt), "wire name") {
		t.Errorf("prompt must instruct that enum fields accept only their wire names; got:\n%s", prompt)
	}

	// Every Trigger + Classification wire name must be enumerated verbatim (anti-drift:
	// derived from the codec's MarshalJSON, not a hand-copy).
	triggers := []projectstate.Trigger{
		projectstate.TriggerClientAction, projectstate.TriggerTimer, projectstate.TriggerBusMessage,
	}
	for _, tr := range triggers {
		name := wireNameOf(t, tr)
		if !strings.Contains(prompt, name) {
			t.Errorf("CoreUseCases prompt missing Trigger wire name %q; got:\n%s", name, prompt)
		}
	}
	classes := []projectstate.Classification{projectstate.ClassCore, projectstate.ClassNonCore}
	for _, c := range classes {
		name := wireNameOf(t, c)
		if !strings.Contains(prompt, name) {
			t.Errorf("CoreUseCases prompt missing Classification wire name %q; got:\n%s", name, prompt)
		}
	}

	// The failing free-prose trigger from the live incident must be framed as INVALID — the
	// prompt explicitly says the trigger is one of the fixed names, not a free-text sentence.
	if !strings.Contains(prompt, "NOT a free-text sentence") {
		t.Errorf("prompt must warn that trigger is not free text; got:\n%s", prompt)
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

// The System draft prompt carries the ComponentKind + relationship-mode wire names.
func Test_SystemPrompt_EnumeratesComponentKindAndMode(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindSystem, projectstate.Project{}, ReviewFeedback{}, nil, 0)
	kinds := []projectstate.ComponentKind{
		projectstate.CompClient, projectstate.CompManager, projectstate.CompEngine,
		projectstate.CompResourceAccess, projectstate.CompResource, projectstate.CompUtility,
	}
	for _, k := range kinds {
		name := wireNameOf(t, k)
		if !strings.Contains(prompt, name) {
			t.Errorf("System prompt missing ComponentKind wire name %q; got:\n%s", name, prompt)
		}
	}
	modes := []projectstate.CallMode{projectstate.CallSync, projectstate.CallQueued, projectstate.CallEventPubSub}
	for _, m := range modes {
		name := wireNameOf(t, m)
		if !strings.Contains(prompt, name) {
			t.Errorf("System prompt missing CallMode wire name %q; got:\n%s", name, prompt)
		}
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
	// The response-carrier contract is stated (mirrors the critique carrier).
	for _, want := range []string{"reviewThread", "response", "STAYS OPEN"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("response-carrier contract missing %q; got:\n%s", want, prompt)
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
