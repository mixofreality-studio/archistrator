package projectstate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// repoCommandsDir walks up from this test file to the repo root (the dir holding
// .claude) and returns .claude/commands.
func repoCommandsDir(t *testing.T) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, ".claude", "commands")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not locate .claude/commands above test file")
	return ""
}

// TestEveryProfilePhaseHasCommandFile asserts the command matrix is exactly the
// flattening of ProfileFor: every (profile, phase) has a .claude/commands/<name>.md.
func TestEveryProfilePhaseHasCommandFile(t *testing.T) {
	cmds := repoCommandsDir(t)
	seen := map[string]bool{}
	for _, combo := range allProfileCombos() {
		for _, p := range ProfileFor(combo.t, combo.v).PhaseIDs() {
			name := CommandFor(combo.t, combo.v, p)
			seen[name] = true
			path := filepath.Join(cmds, name+".md")
			if _, err := os.Stat(path); err != nil {
				t.Errorf("missing command file for (%v,%v,%q): %s.md", combo.t, combo.v, p, name)
			}
		}
	}
	// Sanity: the matrix is exactly 30 distinct commands.
	if len(seen) != 30 {
		t.Errorf("expected 30 distinct commands, got %d", len(seen))
	}
}
