package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/client/web"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
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

// ---------------------------------------------------------------------------
// ExtraMounts — GET /api/v1/capabilities + the local-profile operations
// unmount (operations-argocd-deployment Task 11, spec D9: the local profile
// holds no deployment credential and must not APPEAR to operate).
// ---------------------------------------------------------------------------

func newTestAppHooks() *appHooks {
	return &appHooks{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestExtraMounts_Capabilities_ReportsProfile(t *testing.T) {
	cases := []struct {
		name     string
		gitLocal bool
		want     bool
	}{
		{"local profile reports operations disabled", true, false},
		{"cloud profile reports operations enabled", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestAppHooks()
			cfg := &Config{ProjectStateGitLocal: tc.gitLocal}
			root := http.NewServeMux()
			h.ExtraMounts(root, cfg, web.DevConfig{Enabled: true}, nil, WebManagers{})

			ts := httptest.NewServer(root)
			defer ts.Close()

			resp, err := http.Get(ts.URL + "/api/v1/capabilities")
			if err != nil {
				t.Fatalf("GET /api/v1/capabilities: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			var got capabilitiesResponse
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Operations != tc.want {
				t.Fatalf("Operations = %v, want %v", got.Operations, tc.want)
			}
		})
	}
}

// TestExtraMounts_LocalProfile_UnmountsOperationsRoutes proves the local
// profile does not merely HIDE the operations nav — it unmounts the routes
// server-side, exactly as if they were never registered. Mirrors main.gen.go's
// real mux shape: root.Handle("/", genServer) runs BEFORE ExtraMounts, so a
// stand-in "genServer" here proves whether a request actually reached the
// underlying handler.
func TestExtraMounts_LocalProfile_UnmountsOperationsRoutes(t *testing.T) {
	h := newTestAppHooks()
	cfg := &Config{ProjectStateGitLocal: true}
	root := http.NewServeMux()
	var hit bool
	root.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	h.ExtraMounts(root, cfg, web.DevConfig{Enabled: true}, nil, WebManagers{})

	ts := httptest.NewServer(root)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/operations/query-operated-system-view/some-id")
	if err != nil {
		t.Fatalf("GET operations route: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (operations routes must be unmounted on local)", resp.StatusCode)
	}
	if hit {
		t.Fatal("request reached the underlying handler — operations route was NOT unmounted")
	}

	// A non-operations /api/v1/ request must still reach the underlying handler.
	resp2, err := http.Get(ts.URL + "/api/v1/construction/whatever")
	if err != nil {
		t.Fatalf("GET non-operations route: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if !hit {
		t.Fatal("expected a non-operations /api/v1/ request to still reach the underlying handler")
	}
}

// TestExtraMounts_CloudProfile_OperationsRoutesStayMounted proves the local-only
// shadow is not accidentally blanket: a cloud profile boot must leave
// /api/v1/operations/... routed through to the real handler.
func TestExtraMounts_CloudProfile_OperationsRoutesStayMounted(t *testing.T) {
	h := newTestAppHooks()
	cfg := &Config{ProjectStateGitLocal: false}
	root := http.NewServeMux()
	var hit bool
	root.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	h.ExtraMounts(root, cfg, web.DevConfig{Enabled: true}, nil, WebManagers{})

	ts := httptest.NewServer(root)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/operations/query-operated-system-view/some-id")
	if err != nil {
		t.Fatalf("GET operations route: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if !hit {
		t.Fatal("expected the cloud profile to leave operations routes mounted")
	}
}

// ---------------------------------------------------------------------------
// D13 — the project → operated-app identity (2026-08-08 final review, fix 1).
// ---------------------------------------------------------------------------

// TestOperatedAppIDForProject_IsDeterministicAndDistinct: the derivation is a pure
// function — the same project id yields the same operated app id in this process and
// any other, with no lookup and nothing stored — and different projects never collide.
// The pinned literal is the point of the test: nothing reads the id back from storage,
// so if this value ever changes, every already-registered head-state row is orphaned
// and every console URL an operator has bookmarked stops resolving.
func TestOperatedAppIDForProject_IsDeterministicAndDistinct(t *testing.T) {
	const want = "b663aadc-9cc3-5069-b2bb-d360de9c6a10" // archistrator — frozen 2026-08-08
	got := OperatedAppIDForProject("archistrator")
	if got.String() != want {
		t.Fatalf("OperatedAppIDForProject(%q) = %s, want %s — changing the derivation orphans every registered operated app", "archistrator", got, want)
	}
	if again := OperatedAppIDForProject("archistrator"); again != got {
		t.Fatalf("derivation is not deterministic: %s then %s", got, again)
	}
	if other := OperatedAppIDForProject("gtdapp"); other == got {
		t.Fatal("two different projects derived the same operated app id")
	}
	if got == uuid.Nil {
		t.Fatal("derivation produced the nil UUID, which every façade rejects as empty")
	}
}

// TestExtraMounts_OperatedAppID_AnswersTheDerivedID: the route the webApp reads answers
// the SAME derivation, not a second implementation of it — which is the whole reason the
// browser does not hand-roll UUIDv5.
func TestExtraMounts_OperatedAppID_AnswersTheDerivedID(t *testing.T) {
	h := newTestAppHooks()
	root := http.NewServeMux()
	h.ExtraMounts(root, &Config{ProjectStateGitLocal: false}, web.DevConfig{Enabled: true}, nil, WebManagers{})

	ts := httptest.NewServer(root)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/projects/archistrator/operated-app-id")
	if err != nil {
		t.Fatalf("GET operated-app-id: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got operatedAppIDResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.OperatedAppID != OperatedAppIDForProject("archistrator").String() {
		t.Fatalf("OperatedAppID = %q, want %q", got.OperatedAppID, OperatedAppIDForProject("archistrator"))
	}
}

// TestProjectScopedOperationsManager_RefusesAMismatchedID: a registration whose id is
// not the derived one is refused BEFORE it creates a head-state row nothing can ever
// address again (every consumer derives the id; none reads it back). The derived id
// passes straight through.
func TestProjectScopedOperationsManager_RefusesAMismatchedID(t *testing.T) {
	inner := &fakeRegisterOperationsManager{}
	m := projectScopedOperationsManager{OperationsManager: inner}
	rc := fwmanager.Context{Context: context.Background()}

	if _, err := m.RegisterOperatedApp(rc, uuid.New(), uuid.New(), "archistrator", "bundle-1"); err == nil {
		t.Fatal("a mismatched operatedAppId must be refused")
	}
	if inner.calls != 0 {
		t.Fatal("a refused registration must never reach the manager")
	}

	if _, err := m.RegisterOperatedApp(rc, OperatedAppIDForProject("archistrator"), uuid.New(), "archistrator", "bundle-1"); err != nil {
		t.Fatalf("the derived id must be accepted: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("want one delegated registration, got %d", inner.calls)
	}
}

// fakeRegisterOperationsManager records RegisterOperatedApp delegation. Every other op
// is carried by the embedded nil interface — this test never calls them, and a panic
// would be the correct failure if it ever did.
type fakeRegisterOperationsManager struct {
	operations.OperationsManager
	calls int
}

func (f *fakeRegisterOperationsManager) RegisterOperatedApp(_ fwmanager.Context, _ uuid.UUID, _ uuid.UUID, _ string, _ string) (operations.Version, error) {
	f.calls++
	return 1, nil
}
