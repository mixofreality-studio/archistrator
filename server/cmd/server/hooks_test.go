package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// registerConstruction — the construction Worker gate (local-first-init-funnel
// Task 6 adds the local-without-creds arm alongside dry-run and the cloud
// GitHub-Actions arm).
// ---------------------------------------------------------------------------

func TestRegisterConstruction_DryRun_AlwaysRegisters(t *testing.T) {
	cfg := &Config{ConstructionDryRun: true}
	if !registerConstruction(cfg) {
		t.Fatal("expected DRYRUN=true to always register")
	}
}

func TestRegisterConstruction_Local_NoGitHubCreds_RegistersWithArtifactRepo(t *testing.T) {
	cfg := &Config{
		ConstructionDryRun:     false,
		ProjectStateGitLocal:   true,
		ProjectStateGitRepoURL: "file:///tmp/proj.git",
		ArtifactRepoURL:        "file:///tmp/proj.git",
		// deliberately NO ConstructionRepoOwner/Name — the local executor needs
		// none of the GitHub-Actions construction-repo config.
	}
	if !registerConstruction(cfg) {
		t.Fatal("expected local profile + artifact repo (no GH creds) to register the construction Worker for the local executor")
	}
}

func TestRegisterConstruction_Local_NoArtifactRepo_DoesNotRegister(t *testing.T) {
	cfg := &Config{
		ConstructionDryRun:     false,
		ProjectStateGitLocal:   true,
		ProjectStateGitRepoURL: "file:///tmp/proj.git",
		ArtifactRepoURL:        "",
	}
	if registerConstruction(cfg) {
		t.Fatal("expected local profile with no artifact repo to leave the Worker unregistered")
	}
}

func TestRegisterConstruction_Cloud_NoGitHubRepo_DoesNotRegister(t *testing.T) {
	cfg := &Config{
		ConstructionDryRun:   false,
		ProjectStateGitLocal: false,
		ArtifactRepoURL:      "https://example/artifacts.git",
		// no ConstructionRepoOwner/Name — cloud arm still needs them (Task 6 does
		// not relax the cloud requirement).
	}
	if registerConstruction(cfg) {
		t.Fatal("expected cloud profile with no GitHub construction repo to leave the Worker unregistered (unchanged pre-Task-6 behavior)")
	}
}

func TestRegisterConstruction_Cloud_WithGitHubRepoAndArtifact_Registers(t *testing.T) {
	cfg := &Config{
		ConstructionDryRun:    false,
		ProjectStateGitLocal:  false,
		ArtifactRepoURL:       "https://example/artifacts.git",
		ConstructionRepoOwner: "acme",
		ConstructionRepoName:  "construction",
	}
	if !registerConstruction(cfg) {
		t.Fatal("expected cloud profile with GitHub construction repo + artifact repo to register (unchanged pre-Task-6 behavior)")
	}
}

// ---------------------------------------------------------------------------
// locateStateMCPBinary discovery order: override → sibling of this executable →
// PATH → actionable error.
// ---------------------------------------------------------------------------

func TestLocateStateMCPBinary_Override(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "aiarch-state-mcp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write fixture bin: %v", err)
	}
	t.Setenv(stateMCPBinEnvOverride, bin)

	got, err := locateStateMCPBinary()
	if err != nil {
		t.Fatalf("locateStateMCPBinary: %v", err)
	}
	if got != bin {
		t.Fatalf("got %q, want override %q", got, bin)
	}
}

func TestLocateStateMCPBinary_OverridePointsNowhere_Errors(t *testing.T) {
	t.Setenv(stateMCPBinEnvOverride, filepath.Join(t.TempDir(), "does-not-exist"))
	if _, err := locateStateMCPBinary(); err == nil {
		t.Fatal("expected an error for a non-existent override path")
	}
}

func TestLocateStateMCPBinary_NotFound_ActionableError(t *testing.T) {
	t.Setenv(stateMCPBinEnvOverride, "")
	t.Setenv("PATH", t.TempDir()) // empty dir — guarantees no aiarch-state-mcp resolves
	_, err := locateStateMCPBinary()
	if err == nil {
		t.Fatal("expected an error when the binary cannot be found anywhere")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected a non-empty, actionable error message")
	}
}

// ---------------------------------------------------------------------------
// localProjectID — name-as-identity derivation from the repo path.
// ---------------------------------------------------------------------------

func TestLocalProjectID(t *testing.T) {
	cases := map[string]string{
		"file:///home/dev/myproj":     "myproj",
		"file:///home/dev/myproj.git": "myproj",
		"file:///home/dev/myproj/":    "myproj",
		"file:///":                    "local",
		"":                            "local",
		"file:///a/b/c/remote.git":    "remote",
	}
	for in, want := range cases {
		if got := localProjectID(in); got != want {
			t.Errorf("localProjectID(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// newAppHooks + FinalizeConstructionPipelineAccess — end-to-end selection proof
// that the wiring actually reaches the local executor for local-without-creds +
// DRYRUN=false, and still prefers the GitHub-backed real pipeline when creds ARE
// present ("creds present keeps the existing behavior").
// ---------------------------------------------------------------------------

func TestNewAppHooks_LocalNoCreds_DryRunFalse_BuildsLocalPipeline(t *testing.T) {
	repoDir := t.TempDir()
	stateMCPBin := filepath.Join(t.TempDir(), "aiarch-state-mcp")
	if err := os.WriteFile(stateMCPBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write fixture bin: %v", err)
	}
	t.Setenv(stateMCPBinEnvOverride, stateMCPBin)

	cfg := &Config{
		ProjectStateGitLocal:   true,
		ProjectStateGitRepoURL: "file://" + repoDir,
		ConstructionDryRun:     false,
		// deliberately NO GitHub App creds.
	}
	logger := slog.New(slog.DiscardHandler)

	h, err := newAppHooks(cfg, logger)
	if err != nil {
		t.Fatalf("newAppHooks: %v", err)
	}
	if h.localPipeline == nil {
		t.Fatal("expected h.localPipeline to be built for a local, no-creds, DRYRUN=false boot")
	}
	if h.realPipeline != nil {
		t.Fatal("expected h.realPipeline to stay nil with no GitHub creds configured")
	}

	got := h.FinalizeConstructionPipelineAccess(cfg, nil)
	if got == nil {
		t.Fatal("FinalizeConstructionPipelineAccess returned nil — the local executor arm did not select")
	}
	if got != h.localPipeline {
		t.Fatal("FinalizeConstructionPipelineAccess did not select h.localPipeline")
	}
}

// pathWithFakeClaudeOnly sets PATH to a fresh dir containing ONLY a `claude`
// shim (so resolveWorkerProvider's boot-time `claude --version` preflight
// passes) — guaranteeing aiarch-state-mcp is NOT resolvable via PATH, isolating
// these tests to the state-mcp discovery/registration path they exercise.
func pathWithFakeClaudeOnly(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	claude := filepath.Join(dir, "claude")
	if err := os.WriteFile(claude, []byte("#!/bin/sh\necho 'Claude Code 1.0.0 (test shim)'\nexit 0\n"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write claude shim: %v", err)
	}
	t.Setenv("PATH", dir)
}

func TestNewAppHooks_LocalNoCreds_DryRunTrue_MissingBinaryIsNonFatal(t *testing.T) {
	t.Setenv(stateMCPBinEnvOverride, "")
	pathWithFakeClaudeOnly(t) // aiarch-state-mcp not resolvable; claude preflight passes

	cfg := &Config{
		ProjectStateGitLocal:   true,
		ProjectStateGitRepoURL: "file://" + t.TempDir(),
		ConstructionDryRun:     true, // must NOT fail boot even though the binary is missing
	}
	logger := slog.New(slog.DiscardHandler)

	h, err := newAppHooks(cfg, logger)
	if err != nil {
		t.Fatalf("newAppHooks: expected DRYRUN=true to degrade gracefully, got error: %v", err)
	}
	if h.localPipeline != nil {
		t.Fatal("expected h.localPipeline to stay nil when the state-mcp binary could not be found")
	}
	// The dry-run stub still wins regardless.
	got := h.FinalizeConstructionPipelineAccess(cfg, nil)
	if got == nil {
		t.Fatal("expected the dry-run stub, got nil")
	}
}

func TestNewAppHooks_LocalNoCreds_DryRunFalse_MissingBinaryFailsBoot(t *testing.T) {
	t.Setenv(stateMCPBinEnvOverride, "")
	pathWithFakeClaudeOnly(t)

	cfg := &Config{
		ProjectStateGitLocal:   true,
		ProjectStateGitRepoURL: "file://" + t.TempDir(),
		ConstructionDryRun:     false,
	}
	logger := slog.New(slog.DiscardHandler)

	if _, err := newAppHooks(cfg, logger); err == nil {
		t.Fatal("expected newAppHooks to fail fast: DRYRUN=false + local profile genuinely needs the state-mcp binary")
	}
}
