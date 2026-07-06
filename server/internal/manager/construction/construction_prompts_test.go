package construction

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot returns the repository root, located relative to this test file.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// server/internal/manager/construction → repo root is four levels up.
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
}

// TestConstructionPromptsUseStateTools asserts the Step-4 cutover held: the central
// the-method-project-state skill mandates the aiarch-state write tools, and every
// construction / detailed-design command carries the "state changes through the tools"
// note. This is the prompt-side guard that construction agents record state THROUGH the
// tools, not by hand-editing project.json.
func TestConstructionPromptsUseStateTools(t *testing.T) {
	root := repoRoot(t)

	skill := readFileT(t, filepath.Join(root, ".claude", "skills", "the-method-project-state", "SKILL.md"))
	for _, want := range []string{
		"STATE CHANGES GO THROUGH THE `aiarch-state` MCP TOOLS",
		"recordServiceContract",
		"recordPhaseArtifact",
		"recordTestingState",
		"publishDraft",
	} {
		if !strings.Contains(skill, want) {
			t.Errorf("the-method-project-state skill must reference %q after the tool cutover", want)
		}
	}

	cmdDir := filepath.Join(root, ".claude", "commands")
	var commands []string
	for _, f := range []string{"deployment", "documentation", "frontend", "service", "testing-harness", "testing-perf", "testing-qa"} {
		commands = append(commands, f+"-detailed-design.md")
	}
	for _, f := range []string{"deployment", "documentation", "frontend", "service", "testing-harness", "testing-perf", "testing-plan", "testing-qa", "testing-systemtest"} {
		commands = append(commands, f+"-construction.md")
	}
	const noteMarker = "State changes go through the `aiarch-state` MCP tools"
	for _, c := range commands {
		body := readFileT(t, filepath.Join(cmdDir, c))
		if !strings.Contains(body, noteMarker) {
			t.Errorf("%s must carry the aiarch-state tool-cutover note", c)
		}
	}
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- fixed repo-relative test fixture path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
