package systemdesign

// prompts_test.go — unit coverage for the Manager-owned prompt composition, focused on
// the research-corpus POINTER contract (QA finding F11). The mission-draft prompt must
// POINT the drafting Action at the corpus committed in .aiarch/state/project.json (by
// JSON path + per-source title) and must NEVER inline the (book-sized) source content —
// inlining blew the Temporal payload budget and GitHub's 64KB workflow_dispatch input cap.

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// projWithResearch builds a minimal Project carrying a research corpus whose Content is a
// distinctive, book-sized sentinel we can assert never leaks into the composed prompt.
func projWithResearch(sources ...projectstate.ResearchSource) projectstate.Project {
	return projectstate.Project{
		ID:            projectstate.ProjectID("11111111-1111-1111-1111-111111111111"),
		ResearchInput: projectstate.ResearchInput{Sources: sources},
	}
}

// The mission-draft prompt POINTS at the corpus by JSON path and lists each source TITLE,
// but the source CONTENT must not appear inline (the F11 regression guard).
func Test_MissionPrompt_PointsAtResearch_NeverInlinesContent(t *testing.T) {
	const bigContent = "CORPUS-CONTENT-SENTINEL-should-never-appear-inline " +
		"pretend this is a 600KB book chapter that would blow the 64KB dispatch cap"
	prompt := architectDraftPrompt(
		projectstate.KindMission,
		projWithResearch(
			projectstate.ResearchSource{Title: "Founder brief", Content: bigContent},
			projectstate.ResearchSource{Title: "Competitor analysis", Content: bigContent + " (source 2)"},
		),
		ReviewFeedback{},
	)

	// The corpus CONTENT must never be inlined into the composed prompt.
	if strings.Contains(prompt, "CORPUS-CONTENT-SENTINEL") {
		t.Fatalf("research content leaked into the composed prompt (F11 regression):\n%s", prompt)
	}
	// The prompt must POINT at the committed state by its JSON path.
	if !strings.Contains(prompt, ".aiarch/state/project.json") {
		t.Errorf("prompt must point at .aiarch/state/project.json; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, ".research.Sources") {
		t.Errorf("prompt must name the .research.Sources JSON path; got:\n%s", prompt)
	}
	// The prompt must list each source TITLE (titles are short) so the agent knows what
	// is available to read.
	if !strings.Contains(prompt, "Founder brief") || !strings.Contains(prompt, "Competitor analysis") {
		t.Errorf("prompt must list each source title; got:\n%s", prompt)
	}
}

// The IsZero guard is preserved: with no corpus, no research block is emitted at all.
func Test_MissionPrompt_EmptyCorpus_EmitsNoResearchBlock(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindMission, projWithResearch(), ReviewFeedback{})
	if strings.Contains(prompt, "Research corpus") {
		t.Errorf("empty corpus must emit no research block; got:\n%s", prompt)
	}
}

// writeResearch is the composition unit under the prompt: it points, never inlines, and
// honours the IsZero guard. Direct coverage so the contract holds independent of the
// mission-prompt wrapper.
func Test_writeResearch_PointerForm(t *testing.T) {
	var b strings.Builder
	writeResearch(&b, projectstate.ResearchInput{Sources: []projectstate.ResearchSource{
		{Title: "Customer interviews", Content: "INLINE-SENTINEL long transcript body"},
	}})
	out := b.String()
	if strings.Contains(out, "INLINE-SENTINEL") {
		t.Fatalf("writeResearch inlined source content; got:\n%s", out)
	}
	if !strings.Contains(out, "Customer interviews") {
		t.Errorf("writeResearch must list the source title; got:\n%s", out)
	}
	if !strings.Contains(out, ".research.Sources") {
		t.Errorf("writeResearch must point at the .research.Sources JSON path; got:\n%s", out)
	}

	var empty strings.Builder
	writeResearch(&empty, projectstate.ResearchInput{})
	if empty.Len() != 0 {
		t.Errorf("IsZero guard broken: empty corpus wrote %q", empty.String())
	}
}
