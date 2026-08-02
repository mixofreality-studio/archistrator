package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
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
// newAppHooks + FinalizeAgenticJobAccess — end-to-end selection proof
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

	got := h.FinalizeAgenticJobAccess(cfg, nil)
	if got == nil {
		t.Fatal("FinalizeAgenticJobAccess returned nil — the local executor arm did not select")
	}
	if got != h.localPipeline {
		t.Fatal("FinalizeAgenticJobAccess did not select h.localPipeline")
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
	got := h.FinalizeAgenticJobAccess(cfg, nil)
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

// ---------------------------------------------------------------------------
// warnPartialGithubAppCreds — Fix-subagent Task 6, item 5: a partially-set
// GitHub App identity (some but not all of AppID/PrivateKeyPEM/Account) must
// WARN naming exactly what's missing and state that construction routes to
// the local executor, distinct from the milder "fully repo-less" warning
// that fires when NONE of the three are set.
// ---------------------------------------------------------------------------

func TestNewAppHooks_PartialGithubAppCreds_WarnsNamingMissingSettings(t *testing.T) {
	pathWithFakeClaudeOnly(t) // resolveWorkerProvider's claude preflight must pass

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &Config{
		ProjectStateGitLocal:   true,
		ProjectStateGitRepoURL: "file://" + t.TempDir(),
		ConstructionDryRun:     true, // avoid the unrelated state-mcp-binary boot failure
		GithubAppAppID:         "12345",
		// deliberately NO GithubAppPrivateKeyPEM / GithubAppAccount — partial creds.
	}

	if _, err := newAppHooks(cfg, logger); err != nil {
		t.Fatalf("newAppHooks: %v", err)
	}

	got := buf.String()
	for _, want := range []string{
		"PARTIALLY configured",
		"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM",
		"ARCHISTRATOR_GITHUB_ACCOUNT",
		"local executor",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output missing %q; got: %s", want, got)
		}
	}
	// The var that WAS set (AppID) must not be reported as missing.
	if strings.Contains(got, "missing=\"ARCHISTRATOR_GITHUB_APP_ID,") {
		t.Fatalf("expected the SET var (ARCHISTRATOR_GITHUB_APP_ID) to NOT be listed as missing; got: %s", got)
	}
}

func TestNewAppHooks_NoGithubAppCreds_DoesNotFirePartialWarning(t *testing.T) {
	pathWithFakeClaudeOnly(t)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := &Config{
		ProjectStateGitLocal:   true,
		ProjectStateGitRepoURL: "file://" + t.TempDir(),
		ConstructionDryRun:     true,
		// no GitHub App settings at all — the intentional repo-less case.
	}

	if _, err := newAppHooks(cfg, logger); err != nil {
		t.Fatalf("newAppHooks: %v", err)
	}

	got := buf.String()
	if strings.Contains(got, "PARTIALLY configured") {
		t.Fatalf("expected the fully-repo-less boot to NOT fire the partial-creds warning; got: %s", got)
	}
	if !strings.Contains(got, "NOT configured") {
		t.Fatalf("expected the repo-less \"NOT configured\" warning; got: %s", got)
	}
}

// ---------------------------------------------------------------------------
// FinalizeMessageBus / dryRunConstructionScheduleGate — CONSTRUCTION_DRYRUN=true
// must skip registering construction's own platform-wide Schedules (pump sweep /
// replan sweep) while leaving billing/operations Schedule registration and every
// DeliverSignal call unaffected.
// ---------------------------------------------------------------------------

// fakeMessageBus records every RegisterSchedule/DeliverSignal call it receives.
// Satisfies messagebus.MessageBus.
type fakeMessageBus struct {
	scheduleCalls []messagebus.ScheduleSpec
	signalCalls   int
}

func (b *fakeMessageBus) DeliverSignal(fwra.Context, messagebus.ExecutionID, messagebus.SignalName, messagebus.ExecutionPayload) error {
	b.signalCalls++
	return nil
}

func (b *fakeMessageBus) RegisterSchedule(_ fwra.Context, _ messagebus.ScheduleID, spec messagebus.ScheduleSpec) error {
	b.scheduleCalls = append(b.scheduleCalls, spec)
	return nil
}

var _ messagebus.MessageBus = (*fakeMessageBus)(nil)

func TestFinalizeMessageBus_DryRun_SkipsConstructionSchedules(t *testing.T) {
	cfg := &Config{ConstructionDryRun: true}
	inner := &fakeMessageBus{}
	h := &appHooks{logger: slog.New(slog.DiscardHandler)}

	wrapped := h.FinalizeMessageBus(cfg, inner)

	// The two construction kinds must be skipped (no delegation to inner).
	if err := wrapped.RegisterSchedule(fwra.Context{}, "construction:pumpSweep", messagebus.ScheduleSpec{ExecutionKind: "constructionPumpSweep"}); err != nil {
		t.Fatalf("RegisterSchedule(pumpSweep): %v", err)
	}
	if err := wrapped.RegisterSchedule(fwra.Context{}, "construction:replanSweep", messagebus.ScheduleSpec{ExecutionKind: "constructionReplanSweep"}); err != nil {
		t.Fatalf("RegisterSchedule(replanSweep): %v", err)
	}
	if len(inner.scheduleCalls) != 0 {
		t.Fatalf("expected construction schedules NOT registered under DRYRUN=true, got %d calls: %v", len(inner.scheduleCalls), inner.scheduleCalls)
	}

	// billing/operations kinds must still pass through.
	if err := wrapped.RegisterSchedule(fwra.Context{}, "billing:shortfallSweep", messagebus.ScheduleSpec{ExecutionKind: "billingShortfallSweep"}); err != nil {
		t.Fatalf("RegisterSchedule(billing): %v", err)
	}
	if err := wrapped.RegisterSchedule(fwra.Context{}, "operations:reconcile", messagebus.ScheduleSpec{ExecutionKind: "operationsReconcile"}); err != nil {
		t.Fatalf("RegisterSchedule(operations): %v", err)
	}
	if len(inner.scheduleCalls) != 2 {
		t.Fatalf("expected billing+operations schedules to register unaffected, got %d calls: %v", len(inner.scheduleCalls), inner.scheduleCalls)
	}

	// DeliverSignal must always pass through, regardless of dry-run.
	if err := wrapped.DeliverSignal(fwra.Context{}, "wf-1", "sig", messagebus.ExecutionPayload{}); err != nil {
		t.Fatalf("DeliverSignal: %v", err)
	}
	if inner.signalCalls != 1 {
		t.Fatalf("expected DeliverSignal to pass through unaffected, got %d calls", inner.signalCalls)
	}
}

func TestFinalizeMessageBus_NotDryRun_RegistersConstructionSchedules(t *testing.T) {
	cfg := &Config{ConstructionDryRun: false}
	inner := &fakeMessageBus{}
	h := &appHooks{logger: slog.New(slog.DiscardHandler)}

	wrapped := h.FinalizeMessageBus(cfg, inner)

	// Identity: the raw inner value comes back, so construction schedules
	// register normally.
	if err := wrapped.RegisterSchedule(fwra.Context{}, "construction:pumpSweep", messagebus.ScheduleSpec{ExecutionKind: "constructionPumpSweep"}); err != nil {
		t.Fatalf("RegisterSchedule(pumpSweep): %v", err)
	}
	if len(inner.scheduleCalls) != 1 {
		t.Fatalf("expected construction schedules to register under DRYRUN=false, got %d calls", len(inner.scheduleCalls))
	}
}

// constructionExecutionKinds must resolve to exactly the two kinds bound to
// construction.TaskQueue in MessageBusTemporalArgs's table, staying in sync
// automatically as that table evolves.
func TestConstructionExecutionKinds_MatchesConstructionTaskQueue(t *testing.T) {
	h := &appHooks{}
	kinds := h.constructionExecutionKinds(&Config{})

	want := map[messagebus.ExecutionKind]bool{
		"constructionPumpSweep":   true,
		"constructionReplanSweep": true,
	}
	if len(kinds) != len(want) {
		t.Fatalf("got %d construction kinds, want %d: %v", len(kinds), len(want), kinds)
	}
	for k := range want {
		if !kinds[k] {
			t.Fatalf("expected construction kind %q in the set, got %v", k, kinds)
		}
	}

	// Sanity: every kind resolved really does map to construction.TaskQueue in
	// the underlying table (guards against the filter drifting from its intent).
	table := h.MessageBusTemporalArgs(&Config{})
	for k := range kinds {
		if table[k].TaskQueue != construction.TaskQueue {
			t.Fatalf("kind %q resolved with TaskQueue %q, want %q", k, table[k].TaskQueue, construction.TaskQueue)
		}
	}
}
