package main

// seatassets_test.go — the `seat-assets` subcommand (runtime prompt
// materialization, founder-ratified 2026-07-17): the design/construction CI jobs
// run `aiarch-state-mcp seat-assets --dest .` against the runner checkout at job
// start, so the .claude prompt surface comes from the SAME pinned module
// generation as this binary instead of being repo-committed.

import (
	"os"
	"path/filepath"
	"testing"
)

// seatAssetsSentinels is a representative cross-section of the rendered surface:
// one command, one Method skill, one role agent, and the seat manifest. Their
// presence with non-empty content proves the full commands/skills/agents tree +
// manifest rendered, without coupling the test to the exact ~100-file inventory
// (which versions with the method-assets module).
var seatAssetsSentinels = []string{
	".claude/commands/mission-draft.md",
	".claude/skills/the-method-volatility-identification/SKILL.md",
	".claude/agents/product-manager.md",
	".claude/.method-assets-manifest.json",
}

// TestSeatAssetsRendersPromptSurface: `seat-assets --dest <dir>` renders the full
// .claude surface into dest — sentinel files exist with non-empty content.
func TestSeatAssetsRendersPromptSurface(t *testing.T) {
	dest := t.TempDir()
	if err := runSeatAssets([]string{"--dest", dest}); err != nil {
		t.Fatalf("runSeatAssets: %v", err)
	}
	for _, rel := range seatAssetsSentinels {
		b, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("sentinel %s not rendered: %v", rel, err)
		}
		if len(b) == 0 {
			t.Fatalf("sentinel %s rendered empty", rel)
		}
	}
}

// TestSeatAssetsOverwritesStaleCheckoutCopy: the render FORCE-OVERWRITES an
// existing file at an owned path. This is load-bearing for the runtime-
// materialization doctrine: the workflow step runs AFTER the session-branch
// checkout, so a stale committed .claude copy in a legacy repo must not shadow
// the pinned generation's rendering.
func TestSeatAssetsOverwritesStaleCheckoutCopy(t *testing.T) {
	dest := t.TempDir()
	stalePath := filepath.Join(dest, ".claude", "commands", "mission-draft.md")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := []byte("STALE COMMITTED PROMPT BODY — must not survive seat-assets\n")
	if err := os.WriteFile(stalePath, stale, 0o600); err != nil {
		t.Fatalf("seed stale copy: %v", err)
	}

	if err := runSeatAssets([]string{"--dest", dest}); err != nil {
		t.Fatalf("runSeatAssets: %v", err)
	}
	got, err := os.ReadFile(stalePath)
	if err != nil {
		t.Fatalf("read rendered file: %v", err)
	}
	if string(got) == string(stale) {
		t.Fatal("seat-assets left the stale checkout copy in place — the render must force-overwrite owned paths")
	}
	if len(got) == 0 {
		t.Fatal("rendered mission-draft.md is empty")
	}
}

// TestSeatAssetsIsIdempotent: a second run over the same dest succeeds and the
// surface is unchanged (converging render, no error on pre-existing output).
func TestSeatAssetsIsIdempotent(t *testing.T) {
	dest := t.TempDir()
	if err := runSeatAssets([]string{"--dest", dest}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(dest, ".claude", ".method-assets-manifest.json"))
	if err != nil {
		t.Fatalf("read manifest after first run: %v", err)
	}
	if err := runSeatAssets([]string{"--dest", dest}); err != nil {
		t.Fatalf("second run: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(dest, ".claude", ".method-assets-manifest.json"))
	if err != nil {
		t.Fatalf("read manifest after second run: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("seat-assets is not idempotent: manifest changed across identical runs")
	}
}

// TestSeatAssetsRequiresDest: an empty/missing --dest is rejected with an error
// (never an implicit render into the process working directory).
func TestSeatAssetsRequiresDest(t *testing.T) {
	if err := runSeatAssets(nil); err == nil {
		t.Fatal("runSeatAssets without --dest must error")
	}
	if err := runSeatAssets([]string{"--dest", "  "}); err == nil {
		t.Fatal("runSeatAssets with blank --dest must error")
	}
}
