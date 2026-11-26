package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
)

// subjectRel is the generated scenario test subject every integration's generated tests import.
const subjectRel = "internal/scenariocompose/compose.gen.go"

// subjectFixture is a born project whose main holds the canonical scenario test subject, with the
// generator's own output as the expected text of every merge's result.
func subjectFixture(t *testing.T) (repo string, access agenticjob.AgenticJobAccess, canonical string) {
	t.Helper()
	repo, _, _, access = workspaceFixtureOf(t, freshState(t))
	inRepo(t, repo, "go", "tool", "appgen", "scenariocompose", "-project", ".aiarch/state/project.json", "-root", ".")
	raw, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(subjectRel)))
	if err != nil {
		t.Fatalf("the generator wrote no subject: %v", err)
	}
	inRepo(t, repo, "git", "add", "--", subjectRel)
	inRepo(t, repo, "git", "commit", "-q", "-m", "the scenario test subject")
	return repo, access, string(raw)
}

// editedSubject is canonical with its first line edited, the anchor every parallel integration
// edits its own way, so two such edits conflict as text.
func editedSubject(canonical, marker string) string {
	first, rest, _ := strings.Cut(canonical, "\n")
	return first + " " + marker + "\n" + rest
}

// editsSubject commits to branch the subject edited with marker.
func editsSubject(t *testing.T, repo, branch, canonical, marker string) {
	t.Helper()
	onBranch(t, repo, branch, map[string]string{subjectRel: editedSubject(canonical, marker)})
}

func requireCanonicalSubject(t *testing.T, repo, ref, canonical string) {
	t.Helper()
	got := gitOut(t, repo, "show", ref+":"+subjectRel)
	if got != strings.TrimSpace(canonical) {
		t.Fatalf("%s: the scenario test subject is not the regenerated one:\n%s", ref, got)
	}
}

// TestLocalMergeRegeneratesTheScenarioSubjectOfParallelIntegrations: two integrations (an Engine
// and a ResourceAccess) each edit the generated subject at the same anchor. Both land, the text
// conflict is unioned rather than refused, and main ends with the subject the generator writes
// for the merged tree, not either side's text and not a union of both.
func TestLocalMergeRegeneratesTheScenarioSubjectOfParallelIntegrations(t *testing.T) {
	repo, access, canonical := subjectFixture(t)
	editsSubject(t, repo, "activity/C-engine", canonical, "// engine integration")
	editsSubject(t, repo, "activity/C-access", canonical, "// access")
	// main's subject is one a sibling's landing left, so each branch's edit conflicts with it.
	commitOnMain(t, repo, map[string]string{subjectRel: editedSubject(canonical, "// landed")})

	for _, activity := range []string{"C-engine", "C-access"} {
		if obs := mergeJob(t, access, activity); obs.Phase != agenticjob.PhaseSucceeded {
			t.Fatalf("merging %s did not succeed: %+v", activity, obs)
		}
	}
	requireCanonicalSubject(t, repo, "main", canonical)
}

// TestLocalRefreshRegeneratesTheScenarioSubjectMainChanged: main moves the subject under a branch
// that edited it too; the refresh unions the sides and the branch's tip holds the regenerated
// subject, so the branch's own gate sees the subject the merged tree needs.
func TestLocalRefreshRegeneratesTheScenarioSubjectMainChanged(t *testing.T) {
	repo, access, canonical := subjectFixture(t)
	editsSubject(t, repo, "activity/C-engine", canonical, "// engine integration")
	commitOnMain(t, repo, map[string]string{subjectRel: editedSubject(canonical, "// landed integration")})

	if obs := refreshJob(t, access, "C-engine"); obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("the refresh did not succeed: %+v", obs)
	}
	requireCanonicalSubject(t, repo, "activity/C-engine", canonical)
}
