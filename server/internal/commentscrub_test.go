package internal_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// commentscrub_test.go is the CI gate that keeps the flattening-seam / orphaned-doc /
// history-narration comment noise scrubbed in code-health-phase-bd from reappearing.
// It greps every tracked, non-generated .go file for a small, fixed set of patterns
// that are ALWAYS noise — never a load-bearing constraint — so this list is
// deliberately narrow: it does NOT ban "founder ruling" or "Task " generically,
// because those phrases legitimately co-occur with a kept rule (e.g. "founder
// ruling 2026-06-16: X must Y"). Only the patterns below are pure PR/history
// narration or dead leftovers with no forward constraint, in every observed case.
var noisePatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"seam marker", regexp.MustCompile(`---- from `)},
	{"history narration (\"Replaces the former\")", regexp.MustCompile(`Replaces the former`)},
	{"history narration (disclosure marker)", regexp.MustCompile(`\bB[0-9]+ disclosure\b`)},
	{"history narration (\"pre-migration\")", regexp.MustCompile(`pre-migration`)},
	{"history narration (\"cutover\")", regexp.MustCompile(`cutover`)},
	{"editor-note attribution (\"memory:\")", regexp.MustCompile(`\bmemory:`)},
}

// TestNoCommentNoiseReappears fails if any always-noise pattern reappears in the
// tracked, non-generated .go tree under server/. Each pattern is a class the
// code-health-phase-bd scrub removed in full: a flattening-seam marker left over
// from an old file split, "Replaces the former X" / "B<N> disclosure" / "pre-
// migration" / "cutover" PR narration with no forward constraint, or a "memory:"
// editor-note attribution. If a real constraint needs to travel with one of these
// words, rephrase it to state the constraint on its own (e.g. "founder ruling
// 2026-06-16: X must Y" -> "X must Y") rather than reintroducing the historical
// framing.
func TestNoCommentNoiseReappears(t *testing.T) {
	repoRoot := findRepoRootFromCwd(t)
	files := gitLsFiles(t, repoRoot)

	for _, f := range files {
		if !strings.HasPrefix(f, "server/") || !strings.HasSuffix(f, ".go") {
			continue
		}
		if strings.HasSuffix(f, ".gen.go") {
			continue
		}

		path := repoRoot + "/" + f
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			for _, p := range noisePatterns {
				if p.re.MatchString(line) {
					t.Errorf("%s:%d: %s pattern found (%q) — state the constraint (if any) directly, without the historical framing, instead of reintroducing this noise pattern",
						f, i+1, p.name, strings.TrimSpace(line))
				}
			}
		}
	}
}
