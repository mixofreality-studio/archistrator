// Package operatedruntime is the operatedRuntimeAccess component of the
// ResourceAccess layer — the port over the OPERATED apps' runtime substrate
// (deploy/start/stop/inspect of built-app workloads), as opposed to the
// archistrator server's own runtime.
package operatedruntime

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/google/uuid"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// operatedruntime.go is the hand-written, unexported surface behind the generated
// NewProfiledOperatedRuntimeAccess constructor (option-1 delegated DI, infra "Profiled").
// operatedRuntimeAccess fronts the tenant runtime — the GitOps/kubernetes deployment
// substrate plus the observability backend the operationsManager publishes desired state
// to and observes convergence from (operatedRuntimeAccess §2). There is no
// framework-go-infrastructure-kubernetes client yet, so this RA ships two PROFILES behind
// the one generated constructor, selected by the composition root:
//
//   - LOCAL / dry-run (RuntimeProfileLocal): a deterministic, side-effect-free impl for
//     dogfood / dev / systemtests. Publishes and withdraws are accepted no-ops; the
//     observe reads report a fixed Healthy / SLO-met snapshot; ReadComputeAttribution
//     invents NO usage facts (empty RuntimeEventID ⇒ the Manager appends nothing). This
//     mirrors the construction DRYRUN profile (cmd/server/construction_dryrun.go).
//
//   - REAL (RuntimeProfileReal): the production impl. PublishDesiredState and Withdraw are
//     now functional (Task 7): they render the desired state (below) and commit it to the
//     GitOps repository ArgoCD watches, via a clone → mutate → commit-if-changed → push
//     sequence where git itself — `git diff --cached --quiet`, never a hand-rolled string
//     comparison — decides whether anything changed. The observability half of this RA
//     (GetApplicationHealth, GetSloStatus, ReadComputeAttribution) and WirePaymentConfig
//     still have no backend (N-DEP Argo/kubernetes-observability follow-up) and return an
//     EXPLICIT fwra.Unknown naming that follow-up and the dry-run escape hatch — NOT a
//     silent generated stub.
//
// The package imports NO Temporal (layer rule): the idempotency key arrives as an
// ordinary parameter on each write.

// RuntimeProfile selects which operatedRuntimeAccess implementation the generated
// NewProfiledOperatedRuntimeAccess constructor builds.
type RuntimeProfile int

const (
	// RuntimeProfileUnknown is the zero value; rejected by the builder (a caller must
	// pick a profile explicitly).
	RuntimeProfileUnknown RuntimeProfile = iota
	// RuntimeProfileLocal selects the deterministic, side-effect-free dry-run impl.
	RuntimeProfileLocal
	// RuntimeProfileReal selects the production impl over the real GitOps/kubernetes
	// backend (skeleton until that backend lands — see the package doc).
	RuntimeProfileReal
)

// RuntimeConfig carries the REAL profile's backing-infrastructure configuration (the
// composition root binds it from env). Empty for the LOCAL profile.
type RuntimeConfig struct {
	// GitOpsRepoURL is the GitOps repository the real profile commits rendered desired
	// state to (ArgoCD watches it). Empty ⇒ unconfigured; the real verbs surface that in
	// their diagnostic.
	GitOpsRepoURL string
	// GitOpsToken authenticates pushes to GitOpsRepoURL when it is an http(s) remote (a
	// GitHub App installation token in production). Empty ⇒ the push is attempted
	// unauthenticated, which is fine for the file:// remotes tests use and fails loudly
	// against a real remote. No credential is wired to this field yet — that is a
	// composition-root follow-up outside this task's scope (see the file doc); the field
	// exists so wiring one later needs no further code change.
	GitOpsToken string
	// SelfManagedAppID identifies the one operated app Withdraw must refuse to delete —
	// archistrator's own control plane (D8's third guard; see Withdraw's doc). Withdraw's
	// generated signature (contract.gen.go) carries only an appID, not a
	// RuntimeDesiredState, so there is no SelfManaged flag to read at the call site; this
	// config-injected identity is how the determination is made instead, mirroring
	// GitOpsRepoURL/GitOpsToken above rather than widening the interface. The zero value
	// (uuid.Nil) disables the guard, which is what every non-self-managed deployment and
	// every existing test that does not set it wants.
	SelfManagedAppID uuid.UUID
}

// newProfiledOperatedRuntimeAccess is the hand-written builder behind the generated
// NewProfiledOperatedRuntimeAccess constructor. It selects the impl by profile. Only an
// unset/unknown profile is a construction error (programmer misconfiguration); the REAL
// profile constructs successfully and defers its unimplemented-backend diagnostic to the
// verb calls, so the server still boots as it does today.
func newProfiledOperatedRuntimeAccess(profile RuntimeProfile, config RuntimeConfig) (OperatedRuntimeAccess, error) {
	switch profile {
	case RuntimeProfileLocal:
		return dryRunOperatedRuntime{}, nil
	case RuntimeProfileReal:
		return realOperatedRuntime{config: config}, nil
	case RuntimeProfileUnknown:
		fallthrough
	default:
		return nil, fwra.New(fwra.ContractMisuse,
			"operatedruntime.NewProfiledOperatedRuntimeAccess: unset RuntimeProfile (pick RuntimeProfileLocal or RuntimeProfileReal)")
	}
}

// ---------------------------------------------------------------------------
// LOCAL / dry-run profile — deterministic, side-effect-free.
// ---------------------------------------------------------------------------

// dryRunOperatedRuntime is the deterministic local impl. Writes are accepted no-ops;
// reads report a fixed Healthy / SLO-met snapshot; ReadComputeAttribution invents no
// usage facts. No cluster, no GitOps commit, no observability query.
type dryRunOperatedRuntime struct{}

var _ OperatedRuntimeAccess = dryRunOperatedRuntime{}

func (dryRunOperatedRuntime) PublishDesiredState(_ fwra.Context, _ uuid.UUID, _ RuntimeDesiredState, _ fwra.IdempotencyKey) error {
	return nil
}

// Withdraw is an accepted no-op — matching the contract's NotFound⇒success withdraw
// semantics (there is nothing to prune in the dry-run).
func (dryRunOperatedRuntime) Withdraw(_ fwra.Context, _ uuid.UUID, _ fwra.IdempotencyKey) error {
	return nil
}

func (dryRunOperatedRuntime) WirePaymentConfig(_ fwra.Context, _ uuid.UUID, _ GatewayBinding, _ fwra.IdempotencyKey) error {
	return nil
}

// GetApplicationHealth reports a deterministic Healthy — a freshly published dry-run app
// "converges" instantly.
func (dryRunOperatedRuntime) GetApplicationHealth(_ fwra.Context, _ uuid.UUID) (RuntimeStatus, error) {
	return RuntimeStatusHealthy, nil
}

func (dryRunOperatedRuntime) GetSloStatus(_ fwra.Context, _ uuid.UUID) (SloStatus, error) {
	return SloStatus{SloMet: true, Detail: "dry-run: SLO met"}, nil
}

// ReadComputeAttribution returns an empty attribution (zero RuntimeEventID) so the
// reconcile tick appends NO usage — the dry-run does not fabricate billing facts.
func (dryRunOperatedRuntime) ReadComputeAttribution(_ fwra.Context, _ uuid.UUID, _ AttributionWindow) (ComputeAttribution, error) {
	return ComputeAttribution{}, nil
}

// ---------------------------------------------------------------------------
// REAL profile — GitOps commit path live (Task 7); observability backend still N-DEP.
// ---------------------------------------------------------------------------

// realOperatedRuntime is the production impl. PublishDesiredState and Withdraw commit to
// the real GitOps repository (below). GetApplicationHealth, GetSloStatus,
// ReadComputeAttribution, and WirePaymentConfig still have no observability/payment
// backend, so they return an explicit, diagnosable fwra.Unknown (fail-fast, non-retryable
// — preserving the wire behaviour the operationsManager façade maps to a 503) naming the
// N-DEP follow-up and the dry-run escape hatch.
type realOperatedRuntime struct {
	config RuntimeConfig
}

var _ OperatedRuntimeAccess = realOperatedRuntime{}

// notImplemented is the shared, explicit real-profile diagnostic. It names the missing
// backend, the follow-up, and the dry-run escape hatch, and flags an unset GitOps target
// when that is the proximate misconfiguration.
func (r realOperatedRuntime) notImplemented(verb string) error {
	msg := "operatedruntime real profile: " + verb +
		" requires the GitOps/kubernetes backend, which is not yet implemented (follow-up N-DEP, pairs with the Argo deployment work); " +
		"set ARCHISTRATOR_OPERATIONS_DRYRUN=true for the deterministic local profile"
	if r.config.GitOpsRepoURL == "" {
		msg += " (note: ARCHISTRATOR_OPERATED_RUNTIME_GITOPS_REPO_URL is also unset)"
	}
	return fwra.New(fwra.Unknown, msg)
}

// PublishDesiredState renders d and commits it to the GitOps repo (see the GitOps commit
// path section below). Content-idempotent: republishing identical desired state produces
// no new commit, because gitOpsCommit stages the write and lets `git diff --cached
// --quiet` decide whether anything changed.
func (r realOperatedRuntime) PublishDesiredState(_ fwra.Context, appID uuid.UUID, d RuntimeDesiredState, idempotencyKey fwra.IdempotencyKey) error {
	if d.AppName == "" {
		return fwra.New(fwra.ContractMisuse, "operatedruntime real profile: publishDesiredState: empty RuntimeDesiredState.AppName")
	}
	msg := fmt.Sprintf("operatedruntime: publish %s (appId=%s, idempotencyKey=%s)", d.AppName, appID, idempotencyKey)
	return r.gitOpsCommit(msg, gitOpsPublish(appID, d))
}

// Withdraw refuses the self-managed app outright — the third of three paths to deleting
// archistrator's own control plane, and the only one left open by prune:false and the
// omitted Argo finalizer (renderApplication). Withdraw deletes the Application object
// itself; an operator clicking Withdraw on archistrator, in archistrator's own console,
// would take down the thing servicing the click, with no archistrator left to undo it.
// Tearing archistrator down is a deliberate kubectl operation by a human who means it, not
// a button — see RuntimeConfig.SelfManagedAppID for how this verb (whose generated
// signature carries only an appID) determines the target without a RuntimeDesiredState.
//
// Otherwise, Withdraw resolves appID to the AppName it was last published under (via the
// GitOps repo's own bookkeeping, see gitOpsFindAppByID) and removes that app's rendered
// tree, Application, and marker. An appID with no match — never published, or already
// withdrawn — is success: nothing is staged, so gitOpsCommit commits nothing, matching the
// contract's NotFound⇒success withdraw semantics.
func (r realOperatedRuntime) Withdraw(_ fwra.Context, appID uuid.UUID, idempotencyKey fwra.IdempotencyKey) error {
	if r.config.SelfManagedAppID != uuid.Nil && appID == r.config.SelfManagedAppID {
		return fwra.New(fwra.ContractMisuse,
			"operatedruntime real profile: withdraw refused for "+appID.String()+
				" — it is the self-managed app (archistrator's own control plane); deleting its "+
				"Argo Application would take down the thing that would have to undo the deletion; "+
				"tear it down with a deliberate kubectl operation instead")
	}

	msg := fmt.Sprintf("operatedruntime: withdraw %s (idempotencyKey=%s)", appID, idempotencyKey)
	return r.gitOpsCommit(msg, func(workdir string) error {
		appName, found, err := gitOpsFindAppByID(workdir, appID)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		return gitOpsWithdraw(appName)(workdir)
	})
}

func (r realOperatedRuntime) WirePaymentConfig(_ fwra.Context, _ uuid.UUID, _ GatewayBinding, _ fwra.IdempotencyKey) error {
	return r.notImplemented("wirePaymentConfig")
}

func (r realOperatedRuntime) GetApplicationHealth(_ fwra.Context, _ uuid.UUID) (RuntimeStatus, error) {
	return RuntimeStatusUnknown, r.notImplemented("getApplicationHealth")
}

func (r realOperatedRuntime) GetSloStatus(_ fwra.Context, _ uuid.UUID) (SloStatus, error) {
	return SloStatus{}, r.notImplemented("getSloStatus")
}

func (r realOperatedRuntime) ReadComputeAttribution(_ fwra.Context, _ uuid.UUID, _ AttributionWindow) (ComputeAttribution, error) {
	return ComputeAttribution{}, r.notImplemented("readComputeAttribution")
}

// ---------------------------------------------------------------------------
// GitOps commit path — PublishDesiredState / Withdraw's real backend (Task 7,
// docs/superpowers/specs/2026-08-07-operations-argocd-deployment-design.md).
//
// Both verbs clone config.GitOpsRepoURL to a scratch directory, mutate the working tree,
// stage everything, and commit ONLY IF `git diff --cached --quiet` reports a change. That
// is the entire content-idempotency mechanism: git decides whether the rendered bytes
// differ from what is already committed — never a hand-rolled comparison of rendered
// strings against file contents, which would drift from what git itself considers a
// change. This is load-bearing: the operationsManager reconcile loop calls
// PublishDesiredState on a fixed schedule regardless of whether anything changed, so a
// non-idempotent implementation would produce a commit every tick, forever.
//
// Every rendered object except the Application lands under k8s/argocd/apps/<app>/ — the
// directory Argo's own Application.source.path points at (platformGitOpsAppPath, see
// renderApplication). The Application itself, which GOVERNS that directory, is committed
// one level up at k8s/argocd/applications/<app>.yaml, so pruning/removing an app's own
// tree can never touch the object that governs it.
//
// SEAM, flagged rather than silently absorbed: Withdraw's generated signature
// (contract.gen.go) takes only an appID — no RuntimeDesiredState, no AppName. Two facts
// therefore have to come from somewhere other than the call arguments, and both are
// resolved here without widening that generated interface:
//
//  1. WHICH APP TO REMOVE. PublishDesiredState is the only place AppName and appID are
//     ever seen together, so it writes a small sidecar marker —
//     k8s/argocd/applications/<app>.appid, containing the appID — alongside the
//     Application file. The marker is deliberately NOT part of render()'s output: render
//     stays pure and golden-tested (Task 4-6), and this bookkeeping belongs to the commit
//     path alone. Withdraw reads the applications directory, finds the marker whose
//     contents equal its appID, and recovers AppName from the marker's filename. No
//     match is exactly the NotFound⇒success case: nothing is staged, so gitOpsCommit
//     commits nothing.
//  2. WHETHER THE TARGET IS THE SELF-MANAGED APP. RuntimeConfig.SelfManagedAppID is set
//     once by the composition root, mirroring how GitOpsRepoURL/GitOpsToken are already
//     config-injected rather than threaded through every call. Withdraw compares its
//     appID argument against it BEFORE touching git at all — cheap, fails fast, and means
//     the guard cannot be bypassed by a repo that has no marker file yet (e.g. a stale or
//     hand-edited repo state).
// ---------------------------------------------------------------------------

const (
	// gitOpsApplicationsPath is the directory the Application object for every app lives
	// in — one level above the directory it governs (gitOpsAppDir), so deleting or
	// pruning an app's own tree can never touch the object that governs it.
	gitOpsApplicationsPath = "k8s/argocd/applications/"
	// gitOpsMarkerSuffix names the sidecar file PublishDesiredState writes next to each
	// Application, recording the appID that produced it — see SEAM note 1 above.
	gitOpsMarkerSuffix = ".appid"
)

// authenticatedGitOpsURL injects token as HTTP basic-auth into repoURL when both are
// non-empty and repoURL is http(s) — the shape a GitHub App installation token is
// presented in. file:// and other local/ssh transports carry no token and pass through
// unchanged. No credential is wired to RuntimeConfig.GitOpsToken yet (composition-root
// follow-up, out of this task's scope — see RuntimeConfig's doc), so this branch is
// exercised only once one is; until then GitOpsToken is empty and this is a no-op.
func authenticatedGitOpsURL(repoURL, token string) string {
	if token == "" {
		return repoURL
	}
	u, err := url.Parse(repoURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return repoURL
	}
	u.User = url.UserPassword("x-access-token", token)
	return u.String()
}

// runGit runs `git <args...>` with the given working directory (ignored when empty — used
// for the initial clone, which has no working dir of its own yet) and wraps combined
// output into the error on failure, for a debuggable diagnostic. Mirrors
// agenticjob.runGit / sourcecontrol.gitLocalRun.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...) //nolint:gosec // fixed trusted binary, internally-derived args (repo URL/branch names)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// gitOpsHasStagedChanges reports whether dir's index differs from HEAD, via `git diff
// --cached --quiet`'s exit code (0 ⇒ no difference, 1 ⇒ difference, anything else ⇒ a real
// error). This check — not a comparison of rendered strings against file contents — is
// the entire content-idempotency mechanism described in the section doc above.
func gitOpsHasStagedChanges(dir string) (bool, error) {
	cmd := exec.Command("git", "diff", "--cached", "--quiet") //nolint:gosec // fixed trusted binary, fixed args
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, fmt.Errorf("git diff --cached --quiet: %w", err)
}

// gitOpsAppDir is the directory every rendered Manifest except the Application lands in.
func gitOpsAppDir(workdir, appName string) string {
	return filepath.Join(workdir, platformGitOpsAppPath, appName)
}

// gitOpsApplicationFile is where the Application object for appName is committed — one
// level above gitOpsAppDir, see the section doc.
func gitOpsApplicationFile(workdir, appName string) string {
	return filepath.Join(workdir, gitOpsApplicationsPath, appName+".yaml")
}

// gitOpsMarkerFile is the sidecar PublishDesiredState writes recording which appID
// produced appName's Application — see SEAM note 1 above.
func gitOpsMarkerFile(workdir, appName string) string {
	return filepath.Join(workdir, gitOpsApplicationsPath, appName+gitOpsMarkerSuffix)
}

// gitOpsManifestFileName derives a stable, collision-free filename for a rendered
// Manifest. Kind is part of the name because a workload's Deployment and Service share a
// Name.
func gitOpsManifestFileName(m Manifest) string {
	return strings.ToLower(m.Kind) + "-" + m.Name + ".yaml"
}

// gitOpsCommit is the shared clone → mutate → stage → commit-if-changed → push sequence
// both PublishDesiredState and Withdraw run. mutate receives the clone's working-tree
// root and makes whatever filesystem changes the caller wants committed; gitOpsCommit
// owns everything else, including the content-idempotency check.
func (r realOperatedRuntime) gitOpsCommit(commitMessage string, mutate func(workdir string) error) error {
	if r.config.GitOpsRepoURL == "" {
		return fwra.New(fwra.Unknown,
			"operatedruntime real profile: GitOps commit requires ARCHISTRATOR_OPERATED_RUNTIME_GITOPS_REPO_URL; "+
				"set ARCHISTRATOR_OPERATIONS_DRYRUN=true for the deterministic local profile")
	}

	workdir, err := os.MkdirTemp("", "archistrator-gitops-*")
	if err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: create scratch clone dir")
	}
	defer func() { _ = os.RemoveAll(workdir) }()

	remote := authenticatedGitOpsURL(r.config.GitOpsRepoURL, r.config.GitOpsToken)
	if _, cerr := runGit("", "clone", remote, workdir); cerr != nil {
		return fwra.Wrap(fwra.Infrastructure, cerr, "operatedruntime.gitops: clone")
	}
	// Local to this scratch clone only — never touches the caller's global git config,
	// and the clone carries none of its own (identity config lives in .git/config, which
	// clone does not copy from the remote).
	if _, cerr := runGit(workdir, "config", "user.email", "archistrator-operations@archistrator.dev"); cerr != nil {
		return fwra.Wrap(fwra.Infrastructure, cerr, "operatedruntime.gitops: config user.email")
	}
	if _, cerr := runGit(workdir, "config", "user.name", "archistrator-operations"); cerr != nil {
		return fwra.Wrap(fwra.Infrastructure, cerr, "operatedruntime.gitops: config user.name")
	}

	if err := mutate(workdir); err != nil {
		return err
	}

	if _, aerr := runGit(workdir, "add", "-A"); aerr != nil {
		return fwra.Wrap(fwra.Infrastructure, aerr, "operatedruntime.gitops: add")
	}
	changed, derr := gitOpsHasStagedChanges(workdir)
	if derr != nil {
		return fwra.Wrap(fwra.Infrastructure, derr, "operatedruntime.gitops: diff --cached")
	}
	if !changed {
		// Identical content: no commit, no push. This — git's own verdict, not a
		// hand-rolled string comparison — is the content-idempotency guarantee.
		return nil
	}
	if _, cerr := runGit(workdir, "commit", "-m", commitMessage); cerr != nil {
		return fwra.Wrap(fwra.Infrastructure, cerr, "operatedruntime.gitops: commit")
	}

	pushArgs := []string{"push"}
	if strings.HasPrefix(r.config.GitOpsRepoURL, "file://") {
		// Test-only escape hatch: t.TempDir() scratch repos are ordinary (non-bare)
		// working copies with a branch checked out, and git refuses by default to push
		// into a non-bare repo's checked-out branch. Real GitOps remotes are bare
		// (GitHub), where that restriction never applies and this flag is never sent.
		// updateInstead also refreshes the scratch repo's working tree in place, which is
		// what lets tests assert on files directly under t.TempDir() after publish.
		pushArgs = append(pushArgs, "--receive-pack=git -c receive.denyCurrentBranch=updateInstead receive-pack")
	}
	pushArgs = append(pushArgs, "origin", "HEAD:main")
	if _, perr := runGit(workdir, pushArgs...); perr != nil {
		return fwra.Wrap(fwra.Infrastructure, perr, "operatedruntime.gitops: push")
	}
	return nil
}

// gitOpsPublish is PublishDesiredState's mutate closure. It renders d and replaces
// gitOpsAppDir / gitOpsApplicationFile / gitOpsMarkerFile wholesale, so a manifest render()
// no longer emits (e.g. Postgres disabled after being enabled) is removed too, not just
// the objects that changed.
func gitOpsPublish(appID uuid.UUID, d RuntimeDesiredState) func(string) error {
	return func(workdir string) error {
		manifests, err := render(d)
		if err != nil {
			return err
		}

		appDir := gitOpsAppDir(workdir, d.AppName)
		if err := os.RemoveAll(appDir); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: reset "+appDir)
		}
		if err := os.MkdirAll(appDir, 0o750); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: create "+appDir)
		}

		var application *Manifest
		for i := range manifests {
			m := &manifests[i]
			if m.Kind == "Application" {
				application = m
				continue
			}
			path := filepath.Join(appDir, gitOpsManifestFileName(*m))
			if err := os.WriteFile(path, []byte(m.YAML), 0o600); err != nil {
				return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: write "+path)
			}
		}
		if application == nil {
			// Can't happen — renderApplication always contributes exactly one — but
			// asserted explicitly rather than silently committing a runtime tree with no
			// governing Application.
			return fwra.New(fwra.ContractMisuse, "operatedruntime.gitops: render produced no Application manifest for "+d.AppName)
		}

		applicationsDir := filepath.Join(workdir, gitOpsApplicationsPath)
		if err := os.MkdirAll(applicationsDir, 0o750); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: create "+applicationsDir)
		}
		appFile := gitOpsApplicationFile(workdir, d.AppName)
		if err := os.WriteFile(appFile, []byte(application.YAML), 0o600); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: write "+appFile)
		}
		markerFile := gitOpsMarkerFile(workdir, d.AppName)
		if err := os.WriteFile(markerFile, []byte(appID.String()+"\n"), 0o600); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: write "+markerFile)
		}
		return nil
	}
}

// gitOpsFindAppByID resolves appID to the AppName PublishDesiredState last published it
// under, by reading the .appid markers under gitOpsApplicationsPath (SEAM note 1 above).
// found is false — not an error — when no marker matches, which is what lets Withdraw
// implement the contract's NotFound⇒success semantics as an ordinary no-op mutation
// (nothing staged ⇒ gitOpsCommit commits nothing).
func gitOpsFindAppByID(workdir string, appID uuid.UUID) (appName string, found bool, err error) {
	dir := filepath.Join(workdir, gitOpsApplicationsPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: list "+dir)
	}
	want := appID.String()
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), gitOpsMarkerSuffix)
		if e.IsDir() || !ok {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304 -- e.Name() comes from os.ReadDir's own listing of dir, not external input
		if rerr != nil {
			return "", false, fwra.Wrap(fwra.Infrastructure, rerr, "operatedruntime.gitops: read "+e.Name())
		}
		if strings.TrimSpace(string(raw)) == want {
			return name, true, nil
		}
	}
	return "", false, nil
}

// gitOpsWithdraw is Withdraw's mutate closure for an app that WAS found: it removes
// appName's rendered tree, its Application, and its marker. Removing a path that is
// already gone is not an error (os.RemoveAll is idempotent that way already; os.Remove is
// made so explicitly below) — matching the contract's NotFound⇒success semantics all the
// way through, not just at the gitOpsFindAppByID lookup.
func gitOpsWithdraw(appName string) func(string) error {
	return func(workdir string) error {
		if err := os.RemoveAll(gitOpsAppDir(workdir, appName)); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: remove app dir")
		}
		if err := os.Remove(gitOpsApplicationFile(workdir, appName)); err != nil && !os.IsNotExist(err) {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: remove application file")
		}
		if err := os.Remove(gitOpsMarkerFile(workdir, appName)); err != nil && !os.IsNotExist(err) {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: remove marker file")
		}
		return nil
	}
}

// variant.go holds the deployment-profile VARIANT CONSTRUCTORS for
// operatedRuntimeAccess — the step-8 A2 composegen seam. The model's
// operatedRuntimeAccess binding selects a variant per profile (cloud -> Real,
// local -> Local); the generated composition root calls the matching no-arg,
// no-error variant constructor. Both are thin wrappers over the generated
// NewProfiledOperatedRuntimeAccess whose only error is an unknown profile enum —
// which the explicit Real/Local selection never produces, so it is panic-guarded
// as a can't-happen.
//
// RESIDUAL (P1): the REAL profile's RuntimeConfig.GitOpsRepoURL
// (ARCHISTRATOR_OPERATED_RUNTIME_GITOPS_REPO_URL) is DROPPED here — the real
// GitOps/kubernetes backend is an unbuilt skeleton (follow-up N-DEP), so the
// empty RuntimeConfig is behavior-identical today (the real verbs surface their
// unimplemented-backend diagnostic regardless). When N-DEP lands, thread the URL
// via a VariantHookArgs hook (mirroring the github variants).

// NewRealOperatedRuntimeAccess builds the REAL-profile operatedRuntimeAccess (the
// production GitOps/kubernetes backend; skeleton until N-DEP). Infra-free at the
// composition root today — the RuntimeConfig is empty (see the file doc).
func NewRealOperatedRuntimeAccess() OperatedRuntimeAccess {
	rt, err := NewProfiledOperatedRuntimeAccess(RuntimeProfileReal, RuntimeConfig{})
	if err != nil {
		panic("operatedruntime.NewRealOperatedRuntimeAccess: " + err.Error())
	}
	return rt
}

// NewLocalOperatedRuntimeAccess builds the LOCAL/dry-run operatedRuntimeAccess: a
// deterministic, side-effect-free impl for the local dogfood profile.
func NewLocalOperatedRuntimeAccess() OperatedRuntimeAccess {
	rt, err := NewProfiledOperatedRuntimeAccess(RuntimeProfileLocal, RuntimeConfig{})
	if err != nil {
		panic("operatedruntime.NewLocalOperatedRuntimeAccess: " + err.Error())
	}
	return rt
}

// ---------------------------------------------------------------------------
// Renderer — RuntimeDesiredState -> plain Kubernetes manifests (spec D2/D3/D4,
// docs/superpowers/specs/2026-08-07-operations-argocd-deployment-design.md).
//
// The complete object set per app: Deployment + Service for the server and
// webapp workloads, the CNPG Cluster, the gateway HTTPRoutes and Envoy
// SecurityPolicy/BackendTrafficPolicy, the Keycloak realm/client CR, and the
// Argo Application itself. All of it lives in this one file and is tested from
// one test file — TestFileLayout is zero-waiver (one impl file, one test file
// per ResourceAccess package).
//
// render is PURE: no I/O, no clock, no randomness. That purity is load-bearing
// twice over — it is what makes PublishDesiredState content-idempotent (Task 7
// diffs rendered bytes, not timestamps) and what lets the health overlay
// re-derive its model-key -> (kind, name, namespace) map on demand by
// re-rendering, instead of persisting a mapping table that could drift from
// the renderer (spec §6).
//
// The `platform*` constants below are archistrator-specific values that exist
// because spec D6 scopes this first slice to archistrator-only: its own
// GitHub App id/account, its own Temporal/Keycloak/OTel collector service
// coordinates. They are not derivable from RuntimeDesiredState today because
// nothing upstream of the renderer carries them yet. When built-app onboarding
// lands (spec §11, out of scope here), these become per-app fields threaded
// through RuntimeDesiredState rather than literals baked into the renderer —
// EARMARK, do not let it go unnoticed if this file is extended for a second
// operated app.
const (
	// platformTemporalHostPort is the in-cluster Temporal frontend every
	// operated app's server container talks to today (one shared Temporal
	// namespace-per-app is assumed downstream via the app's own name).
	platformTemporalHostPort = "temporal-frontend.temporal.svc:7233"
	// platformGithubAppID / platformGithubAccount identify the GitHub App
	// identity archistrator's own server uses for its self-managed
	// construction dispatch and CLOUD projectStateAccess git substrate.
	platformGithubAppID   = "4029529"
	platformGithubAccount = "davidmarne"
	// platformKeycloakServiceHost is the in-cluster Keycloak service the
	// server validates bearer tokens' JWKS against.
	platformKeycloakServiceHost = "keycloak-service.keycloak.svc.cluster.local:8080"
	// platformOtelCollectorEndpoint is the in-cluster OTel collector every
	// operated app's server ships traces to.
	platformOtelCollectorEndpoint = "otel-collector-traces-collector.observability.svc.cluster.local:4317"
	// platformDevSubject / platformDevRoles are the dev-mode identity
	// rendered (but inert while ARCHISTRATOR_AUTH_DEV_MODE=false) so a
	// namespace can be flipped to dev auth without a redeploy.
	platformDevSubject = "dev-architect"
	platformDevRoles   = "drive-phase approve-artifact"

	// serverContainerPort / webAppContainerPort are the two workloads' fixed
	// listen ports — not part of RuntimeDesiredState because they are a
	// property of the built images (cmd/server's HTTP listener, the webapp's
	// static-asset nginx), not an operator or design knob.
	serverContainerPort = 8080
	webAppContainerPort = 80

	// platformGitOpsRepoURL is the repository ArgoCD watches and the renderer
	// commits to (Task 7). Every production Application already points here.
	platformGitOpsRepoURL = "https://github.com/davidmarne/aiarchmultiplatform.git"
	// platformGitOpsAppPath is the directory prefix each app's rendered
	// manifests are committed under; the Application's source path is this
	// plus the app name.
	platformGitOpsAppPath = "k8s/argocd/apps/"

	// platformGatewayName / platformGatewayNamespace identify the ONE shared
	// Envoy Gateway (one LoadBalancer) fronting the whole cluster. No app owns
	// gateway infrastructure — each contributes only HTTPRoutes and Envoy
	// policies in its own namespace, attaching to the shared gateway's
	// per-app listener, which that gateway opens via allowedRoutes. Because
	// the backends always live in the app's own namespace (the same namespace
	// as the routes), no ReferenceGrant is ever needed: the production chart's
	// referencegrant.yaml is gated on a cross-namespace backend and renders
	// nothing today.
	platformGatewayName      = "gateway"
	platformGatewayNamespace = "gtd"

	// platformPostgresImage is the CNPG operand image every app's cluster runs.
	platformPostgresImage = "ghcr.io/cloudnative-pg/postgresql:16"
	// platformPostgresStorageSize is the per-app volume size; the storage
	// CLASS is a desired-state field, the size is not a design knob yet.
	platformPostgresStorageSize = "10Gi"
	// platformPostgresOwner is the role CNPG creates during bootstrap and
	// whose credentials land in the "<cluster>-app" Secret the server reads.
	platformPostgresOwner = "app"

	// platformKeycloakCRName / platformKeycloakNamespace locate the cluster's
	// single Keycloak deployment. A KeycloakRealmImport resolves keycloakCRName
	// in its OWN namespace, and the operator requires any placeholder Secret to
	// be in that same namespace — so the realm CR is namespaced to Keycloak's
	// namespace, not the app's.
	platformKeycloakCRName    = "keycloak"
	platformKeycloakNamespace = "keycloak"
	// oidcClientSecretKey is the key Envoy Gateway reads the OIDC client secret
	// from inside the referenced Secret. Fixed by Envoy Gateway, not by us.
	oidcClientSecretKey = "client-secret"
	// oidcCallbackPath / oidcLogoutPath are the edge OIDC filter's two reserved
	// paths. The callback is deliberately NOT its own HTTPRoute — see
	// gatewayRoutes.
	oidcCallbackPath = "/oauth2/callback"
	oidcLogoutPath   = "/logout"
)

// oidcScopes are the scopes the edge requests. Package-level (not a const —
// Go has no const slices) and never mutated, so the render stays deterministic.
var oidcScopes = []string{"openid", "email", "profile"}

// Manifest is one rendered Kubernetes object plus the deployment-model node(s)
// it came from. ModelKeys is what lets the health overlay attribute a live
// resource back to a diagram node without storing a separate mapping table.
type Manifest struct {
	// ModelKeys are the deployment-model nodes this object answers to. Usually
	// one; the Postgres Cluster (Task 5) answers to all three database nodes,
	// since production runs one cluster serving all three logical stores and
	// every one of those diagram nodes must colour from its health. Kept
	// sorted so the render stays byte-deterministic.
	ModelKeys []string
	Kind      string
	Name      string
	Namespace string
	YAML      string
}

// envVar is one container env entry: either a literal Value, or a
// SecretName/SecretKey pair rendered as a valueFrom.secretKeyRef — never both.
// Optional mirrors Kubernetes' secretKeyRef.optional, used for secrets created
// out-of-band that may not exist yet (the pod must still start).
type envVar struct {
	Name       string
	Value      string
	SecretName string
	SecretKey  string
	Optional   bool
}

// resourceQuantity is one k8s resources.limits/requests entry (e.g. cpu:
// 500m). A {Name, Value} pair rather than dedicated CPU/Memory fields so the
// deployment template renders both quantities with a single range.
type resourceQuantity struct {
	Name  string
	Value string
}

// resourceQuantities returns the standard [cpu, memory] pair the deployment
// template's resources.limits and resources.requests blocks both expect.
func resourceQuantities(cpu, memory string) []resourceQuantity {
	return []resourceQuantity{
		{Name: "cpu", Value: cpu},
		{Name: "memory", Value: memory},
	}
}

// workloadData is the template input shared by the server and webapp
// Deployments. The two workloads differ enough (env, security posture,
// probes, resources) that one template with conditionals is clearer than two
// near-duplicate templates — see renderServerWorkload / renderWebAppWorkload
// for how each side populates it.
type workloadData struct {
	Name            string // Deployment/Service name, e.g. "archistrator-server"
	ContainerName   string // container name — NOT always == Name (webapp's is "webapp", matching the production chart)
	Namespace       string
	Image           string
	Replicas        int64
	Port            int
	PortName        string
	ImagePullPolicy string

	// PodSecurity / ContainerSecurity gate the hardened securityContext blocks
	// production applies to the server only (the webapp's nginx image does
	// not run as the same non-root distroless posture).
	PodSecurity       bool
	ContainerSecurity bool

	Env []envVar

	// ResourceLimits / ResourceRequests are {cpu, memory} pairs, in that order.
	// Built by resourceQuantities rather than four separate scalar fields so
	// the template has one range over {Name, Value} instead of four near-
	// duplicate lines.
	ResourceLimits   []resourceQuantity
	ResourceRequests []resourceQuantity

	LivenessPath          string
	ReadinessPath         string
	ProbeInitialDelay     int
	ProbePeriod           int
	ProbeTimeout          int // 0 omits timeoutSeconds (matches the webapp golden, which relies on the k8s default)
	ProbeFailureThreshold int // 0 omits failureThreshold, same reasoning
}

// deploymentTmpl renders one Deployment. Field order and structure mirror
// testdata/golden/production/archistrator-server.yaml and
// archistrator-webapp.yaml; the conditionals are exactly the two workloads'
// differences (env block, security contexts, probe timeout/failureThreshold).
var deploymentTmpl = template.Must(template.New("deployment").Parse(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    app.kubernetes.io/name: {{ .Name }}
    app.kubernetes.io/instance: {{ .Name }}
    app.kubernetes.io/managed-by: archistrator-operatedRuntimeAccess
spec:
  replicas: {{ .Replicas }}
  selector:
    matchLabels:
      app.kubernetes.io/name: {{ .Name }}
      app.kubernetes.io/instance: {{ .Name }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: {{ .Name }}
        app.kubernetes.io/instance: {{ .Name }}
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchLabels:
                  app.kubernetes.io/name: {{ .Name }}
                  app.kubernetes.io/instance: {{ .Name }}
              topologyKey: kubernetes.io/hostname
{{- if .PodSecurity }}
      securityContext:
        fsGroup: 65532
        runAsGroup: 65532
        runAsNonRoot: true
        runAsUser: 65532
{{- end }}
      containers:
      - name: {{ .ContainerName }}
        image: "{{ .Image }}"
        imagePullPolicy: {{ .ImagePullPolicy }}
{{- if .ContainerSecurity }}
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: true
{{- end }}
        ports:
        - name: {{ .PortName }}
          containerPort: {{ .Port }}
          protocol: TCP
{{- if .Env }}
        env:
{{- range .Env }}
        - name: {{ .Name }}
{{- if .SecretName }}
          valueFrom:
            secretKeyRef:
              name: {{ .SecretName }}
              key: {{ .SecretKey }}
{{- if .Optional }}
              optional: true
{{- end }}
{{- else }}
          value: "{{ .Value }}"
{{- end }}
{{- end }}
{{- end }}
        resources:
          limits:
{{- range .ResourceLimits }}
            {{ .Name }}: {{ .Value }}
{{- end }}
          requests:
{{- range .ResourceRequests }}
            {{ .Name }}: {{ .Value }}
{{- end }}
        livenessProbe:
          httpGet:
            path: {{ .LivenessPath }}
            port: {{ .PortName }}
          initialDelaySeconds: {{ .ProbeInitialDelay }}
          periodSeconds: {{ .ProbePeriod }}
{{- if .ProbeTimeout }}
          timeoutSeconds: {{ .ProbeTimeout }}
{{- end }}
{{- if .ProbeFailureThreshold }}
          failureThreshold: {{ .ProbeFailureThreshold }}
{{- end }}
        readinessProbe:
          httpGet:
            path: {{ .ReadinessPath }}
            port: {{ .PortName }}
          initialDelaySeconds: {{ .ProbeInitialDelay }}
          periodSeconds: {{ .ProbePeriod }}
{{- if .ProbeTimeout }}
          timeoutSeconds: {{ .ProbeTimeout }}
{{- end }}
{{- if .ProbeFailureThreshold }}
          failureThreshold: {{ .ProbeFailureThreshold }}
{{- end }}
`))

// serviceTmpl renders one ClusterIP Service fronting a workload Deployment.
// Mirrors the golden's service.yaml shape (port/targetPort/selector); the
// label set trades the Helm-specific labels (helm.sh/chart,
// app.kubernetes.io/version) for a fixed managed-by, since this renderer is
// not Helm-templated and has no chart version to report.
var serviceTmpl = template.Must(template.New("service").Parse(`apiVersion: v1
kind: Service
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    app.kubernetes.io/name: {{ .Name }}
    app.kubernetes.io/instance: {{ .Name }}
    app.kubernetes.io/managed-by: archistrator-operatedRuntimeAccess
spec:
  type: ClusterIP
  ports:
    - port: {{ .Port }}
      targetPort: {{ .PortName }}
      protocol: TCP
      name: {{ .PortName }}
  selector:
    app.kubernetes.io/name: {{ .Name }}
    app.kubernetes.io/instance: {{ .Name }}
`))

// renderTemplate executes tmpl over data into a string. Shared by every
// manifest kind — the only thing that varies is which *template.Template and
// which typed data get passed in.
func renderTemplate(tmpl *template.Template, data any) (string, error) {
	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		return "", fwra.New(fwra.Unknown, "operatedruntime.render: "+tmpl.Name()+": "+err.Error())
	}
	return b.String(), nil
}

// serverEnv builds the server container's env block, reproducing
// testdata/golden/production/archistrator-server.yaml's env: entries
// variable-for-variable (Task 4 brief requirement) — see the file-level doc
// comment for which values come from RuntimeDesiredState versus the
// archistrator-only platform constants above. Order matches the golden file
// so a human diffing the two can read them side by side.
func serverEnv(d RuntimeDesiredState, serverName string) []envVar {
	env := []envVar{
		{Name: "ARCHISTRATOR_LISTEN_ADDR", Value: fmt.Sprintf(":%d", serverContainerPort)},
		{Name: "ARCHISTRATOR_SHUTDOWN_TIMEOUT", Value: "20s"},
	}

	if d.Postgres.Enabled {
		postgresSecret := d.AppName + "-postgres-app"
		postgresHost := d.AppName + "-postgres-rw." + d.Namespace + ".svc"
		env = append(env,
			envVar{Name: "DATABASE_USERNAME", SecretName: postgresSecret, SecretKey: "username"},
			envVar{Name: "DATABASE_PASSWORD", SecretName: postgresSecret, SecretKey: "password"},
			envVar{Name: "ARCHISTRATOR_POSTGRES_URL", Value: fmt.Sprintf(
				"postgres://$(DATABASE_USERNAME):$(DATABASE_PASSWORD)@%s:5432/%s?sslmode=disable",
				postgresHost, d.AppName)},
		)
	}

	githubSecret := d.AppName + "-github-app-secret"
	env = append(env,
		envVar{Name: "ARCHISTRATOR_TEMPORAL_HOSTPORT", Value: platformTemporalHostPort},
		envVar{Name: "ARCHISTRATOR_TEMPORAL_NAMESPACE", Value: d.AppName},
		envVar{Name: "ARCHISTRATOR_GITHUB_APP_ID", Value: platformGithubAppID},
		envVar{Name: "ARCHISTRATOR_GITHUB_ACCOUNT", Value: platformGithubAccount},
		envVar{Name: "ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM", SecretName: githubSecret, SecretKey: "privateKey", Optional: true},
		envVar{Name: "ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL", Value: "false"},
		envVar{Name: "ARCHISTRATOR_GITHUB_WEBHOOK_SECRET", SecretName: githubSecret, SecretKey: "webhookSecret", Optional: true},
		envVar{Name: "ARCHISTRATOR_KEYCLOAK_JWKS_URL", Value: fmt.Sprintf(
			"http://%s/realms/%s/protocol/openid-connect/certs", platformKeycloakServiceHost, d.AppName)},
		envVar{Name: "ARCHISTRATOR_KEYCLOAK_ISSUER", Value: d.OIDC.Issuer},
		envVar{Name: "ARCHISTRATOR_AUTH_DEV_MODE", Value: "false"},
		envVar{Name: "ARCHISTRATOR_DEV_SUBJECT", Value: platformDevSubject},
		envVar{Name: "ARCHISTRATOR_DEV_ROLES", Value: platformDevRoles},
		envVar{Name: "OTEL_SERVICE_NAME", Value: serverName},
		envVar{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: platformOtelCollectorEndpoint},
		envVar{Name: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: "grpc"},
		envVar{Name: "OTEL_EXPORTER_OTLP_INSECURE", Value: "true"},
	)
	return env
}

// renderServerWorkload builds the server's Deployment + Service manifests.
func renderServerWorkload(d RuntimeDesiredState) ([]Manifest, error) {
	name := d.AppName + "-server"
	wd := workloadData{
		Name:                  name,
		ContainerName:         name,
		Namespace:             d.Namespace,
		Image:                 d.Server.Image,
		Replicas:              d.Server.Replicas,
		Port:                  serverContainerPort,
		PortName:              "http",
		ImagePullPolicy:       "Always",
		PodSecurity:           true,
		ContainerSecurity:     true,
		Env:                   serverEnv(d, name),
		ResourceLimits:        resourceQuantities("500m", "512Mi"),
		ResourceRequests:      resourceQuantities("100m", "128Mi"),
		LivenessPath:          "/healthz",
		ReadinessPath:         "/readyz",
		ProbeInitialDelay:     15,
		ProbePeriod:           10,
		ProbeTimeout:          5,
		ProbeFailureThreshold: 3,
	}
	return renderWorkloadManifests(wd, []string{d.Server.ModelKey})
}

// renderWebAppWorkload builds the webapp's Deployment + Service manifests.
// The webapp serves static assets via nginx: no env block, no hardened
// security contexts, matching testdata/golden/production/archistrator-webapp.yaml
// exactly on those points. Its container name is "webapp", not the Deployment
// name — the golden chart names it that way and downstream tooling (e.g.
// `kubectl logs deploy/archistrator-webapp -c webapp`) depends on it.
func renderWebAppWorkload(d RuntimeDesiredState) ([]Manifest, error) {
	name := d.AppName + "-webapp"
	wd := workloadData{
		Name:              name,
		ContainerName:     "webapp",
		Namespace:         d.Namespace,
		Image:             d.WebApp.Image,
		Replicas:          d.WebApp.Replicas,
		Port:              webAppContainerPort,
		PortName:          "http",
		ImagePullPolicy:   "Always",
		ResourceLimits:    resourceQuantities("100m", "128Mi"),
		ResourceRequests:  resourceQuantities("10m", "64Mi"),
		LivenessPath:      "/health",
		ReadinessPath:     "/health",
		ProbeInitialDelay: 5,
		ProbePeriod:       10,
	}
	return renderWorkloadManifests(wd, []string{d.WebApp.ModelKey})
}

// renderWorkloadManifests executes the Deployment and Service templates over
// wd and wraps the results as Manifests carrying modelKeys. Shared by both
// workloads so the Kind/Name/Namespace bookkeeping lives in exactly one place.
func renderWorkloadManifests(wd workloadData, modelKeys []string) ([]Manifest, error) {
	depYAML, err := renderTemplate(deploymentTmpl, wd)
	if err != nil {
		return nil, err
	}
	svcYAML, err := renderTemplate(serviceTmpl, wd)
	if err != nil {
		return nil, err
	}
	return []Manifest{
		{ModelKeys: modelKeys, Kind: "Deployment", Name: wd.Name, Namespace: wd.Namespace, YAML: depYAML},
		{ModelKeys: modelKeys, Kind: "Service", Name: wd.Name, Namespace: wd.Namespace, YAML: svcYAML},
	}, nil
}

// ---------------------------------------------------------------------------
// CNPG Postgres cluster.
// ---------------------------------------------------------------------------

// clusterData is the CNPG Cluster template input. Mirrors
// testdata/golden/production/archistrator-postgres.yaml field for field.
type clusterData struct {
	Name      string
	Namespace string
	Instances int64
	Image     string

	// ResourceRequests / ResourceLimits are ordered {name, quantity} pairs
	// rather than dedicated fields, matching the deployment template's shape.
	ResourceRequests []resourceQuantity
	ResourceLimits   []resourceQuantity

	StorageSize  string
	StorageClass string

	Database string
	Owner    string
	// PostInitSQL runs as superuser against the `postgres` database at
	// bootstrap, and ONLY at bootstrap — CNPG never replays it against an
	// existing cluster.
	PostInitSQL []string
}

// quotedResourceQuantities returns the [memory, cpu] pair the CNPG Cluster
// template expects, in that order and quoted, as the production chart writes
// them. The quantities carry their own quotes because CNPG's chart quotes them
// and the golden diff is byte-level.
func quotedResourceQuantities(mem, cpu string) []resourceQuantity {
	return []resourceQuantity{
		{Name: "memory", Value: `"` + mem + `"`},
		{Name: "cpu", Value: `"` + cpu + `"`},
	}
}

var clusterTmpl = template.Must(template.New("cluster").Parse(`apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
spec:
  instances: {{ .Instances }}
  imageName: {{ .Image }}
  monitoring:
    enablePodMonitor: true
  resources:
    requests:
{{- range .ResourceRequests }}
      {{ .Name }}: {{ .Value }}
{{- end }}
    limits:
{{- range .ResourceLimits }}
      {{ .Name }}: {{ .Value }}
{{- end }}
  storage:
    size: {{ .StorageSize }}
{{- if .StorageClass }}
    storageClass: {{ .StorageClass }}
{{- end }}
  bootstrap:
    initdb:
      database: {{ .Database }}
      owner: {{ .Owner }}
{{- if .PostInitSQL }}
      postInitSQL:
{{- range .PostInitSQL }}
        - {{ . }}
{{- end }}
{{- end }}
`))

// renderPostgres builds the app's CNPG Cluster, or nothing when the app brings
// its own database.
//
// The Cluster carries EVERY database-role model key, not one of them: one
// physical cluster serves all the app's logical stores, and each of those is
// its own node on the deployment diagram, so all of them must colour from this
// single resource's health (spec §5.1a). Keys are sorted so the render stays
// byte-deterministic regardless of the order assembly produced them in.
func renderPostgres(d RuntimeDesiredState) ([]Manifest, error) {
	if !d.Postgres.Enabled {
		return nil, nil
	}
	keys := sortedModelKeys(d.Postgres.ModelKeys)
	if len(keys) == 0 {
		return nil, fwra.New(fwra.ContractMisuse,
			"operatedruntime.render: Postgres.Enabled with no ModelKeys — the rendered Cluster would be unattributable to any deployment-diagram node, leaving every database node permanently uncoloured")
	}

	cd := clusterData{
		Name:             d.AppName + "-postgres",
		Namespace:        d.Namespace,
		Instances:        d.Postgres.Instances,
		Image:            platformPostgresImage,
		ResourceRequests: quotedResourceQuantities("512Mi", "500m"),
		ResourceLimits:   quotedResourceQuantities("1Gi", "1000m"),
		StorageSize:      platformPostgresStorageSize,
		StorageClass:     d.Postgres.StorageClass,
		Database:         d.AppName,
		Owner:            platformPostgresOwner,
	}
	if d.SelfManaged {
		// archistrator's own cluster additionally hosts the `gitea` database
		// backing its self-hosted git substrate. That database is NOT a node
		// on the deployment model — it is a production fact the model cannot
		// currently express — so it is keyed off SelfManaged rather than off
		// the model. EARMARK: when built-app onboarding lands, companion
		// databases become a modeled list on PostgresSpec instead of this
		// branch. Tenant apps get no extra database.
		cd.PostInitSQL = []string{"CREATE DATABASE gitea OWNER app;"}
	}

	yaml, err := renderTemplate(clusterTmpl, cd)
	if err != nil {
		return nil, err
	}
	return []Manifest{{ModelKeys: keys, Kind: "Cluster", Name: cd.Name, Namespace: cd.Namespace, YAML: yaml}}, nil
}

// ---------------------------------------------------------------------------
// Gateway routes and Envoy policies.
// ---------------------------------------------------------------------------

// routeSpec is one HTTPRoute plus the BackendTrafficPolicy attached to it.
type routeSpec struct {
	Name             string
	Namespace        string
	Host             string
	GatewayName      string
	GatewayNamespace string
	Listener         string
	Path             string
	BackendName      string
	BackendPort      int
	// BrowserFacing marks the routes the OIDC SecurityPolicy attaches to.
	BrowserFacing bool
}

var httpRouteTmpl = template.Must(template.New("httproute").Parse(`apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
spec:
  hostnames:
  - {{ .Host }}
  parentRefs:
  - name: {{ .GatewayName }}
    namespace: {{ .GatewayNamespace }}
    sectionName: {{ .Listener }}
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: {{ .Path }}
    backendRefs:
    - name: {{ .BackendName }}
      port: {{ .BackendPort }}
`))

var backendTrafficPolicyTmpl = template.Must(template.New("backendtrafficpolicy").Parse(`apiVersion: gateway.envoyproxy.io/v1alpha1
kind: BackendTrafficPolicy
metadata:
  name: {{ .Name }}-policy
  namespace: {{ .Namespace }}
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: {{ .Name }}
  loadBalancer:
    type: RoundRobin
`))

// gatewayRoutes returns the app's complete route set, in a fixed order.
//
// Envoy matches the MOST SPECIFIC PathPrefix first, and that fact makes one
// ABSENCE here load-bearing: there is deliberately NO dedicated /oauth2 route.
// The OIDC filter that the SecurityPolicy installs on the `/` (webapp) route
// intercepts /oauth2/callback itself — it exchanges the authorization code and
// sets the session cookie, and never forwards the callback to a backend. A
// dedicated /oauth2 route would be more specific than `/`, would win the match,
// and would steal the callback away from the only route carrying the policy.
// The filter would then never run and no session would ever be established.
// Login breaks, and nothing in the rendered manifests looks wrong. Do NOT add
// an /oauth2 route "for completeness" — TestRender_HasNoDedicatedOAuth2Route
// exists to stop exactly that.
//
// /healthz and /readyz are separate routes solely so they can stay OUTSIDE the
// SecurityPolicy's targetRefs: probes must answer without a session.
func gatewayRoutes(d RuntimeDesiredState) []routeSpec {
	base := routeSpec{
		Namespace:        d.Namespace,
		Host:             d.Host,
		GatewayName:      platformGatewayName,
		GatewayNamespace: platformGatewayNamespace,
		// Per-app listener on the shared gateway (host <app>.<domain>).
		Listener: "https-" + d.AppName,
	}
	server := d.AppName + "-server"
	webapp := d.AppName + "-webapp"

	mk := func(suffix, path, backend string, port int, browserFacing bool) routeSpec {
		r := base
		r.Name = d.AppName + "-" + suffix + "-route"
		r.Path = path
		r.BackendName = backend
		r.BackendPort = port
		r.BrowserFacing = browserFacing
		return r
	}
	return []routeSpec{
		// Browser SPA — full authorization-code redirect login, and the route
		// that owns /oauth2/callback via its OIDC filter (see the doc above).
		mk("webapp", "/", webapp, webAppContainerPort, true),
		// API — the edge validates the Keycloak JWT (defense in depth) and
		// forwards the Authorization header unchanged; the Go server validates
		// that same bearer token itself.
		mk("api", "/api", server, serverContainerPort, true),
		// Liveness / readiness — unauthenticated by construction.
		mk("healthz", "/healthz", server, serverContainerPort, false),
		mk("readyz", "/readyz", server, serverContainerPort, false),
	}
}

// securityPolicyData is the Envoy SecurityPolicy template input: one OIDC
// provider plus the routes it attaches to.
type securityPolicyData struct {
	Name      string
	Namespace string
	// TargetRouteNames are the browser-facing routes, sorted so the render is
	// deterministic and the diff against production is stable.
	TargetRouteNames []string
	JWTProviderName  string
	Issuer           string
	JWKSURI          string
	ClientID         string
	ClientSecretRef  string
	RedirectURL      string
	LogoutPath       string
	CookieName       string
	Scopes           []string
}

// securityPolicyTmpl keeps the production chart's explanatory comments in the
// rendered output: they explain a routing subtlety that the YAML alone does not
// reveal, and the operator reading a manual-sync diff in the Argo UI is exactly
// who needs them.
var securityPolicyTmpl = template.Must(template.New("securitypolicy").Parse(`apiVersion: gateway.envoyproxy.io/v1alpha1
kind: SecurityPolicy
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
spec:
  targetRefs:
    # Only browser-facing routes get the OIDC redirect + JWT validation:
    #   * webapp route — browser SPA, full authorization-code redirect login.
    #   * api  route   — validates the Keycloak JWT at the edge (defense in depth)
    #                    and forwards the Authorization header unchanged
    #                    (oidc.passThroughAuthHeader); the Go server independently
    #                    validates that same bearer access token.
    # The healthz and readyz routes are intentionally NOT targeted, so they stay
    # unauthenticated (health endpoints must answer probes without a session).
    # The /oauth2/callback path is captured by the webapp route: the OIDC filter
    # installed on that route intercepts the callback to complete login, which is
    # why no dedicated /oauth2 route exists.
{{- range .TargetRouteNames }}
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: {{ . }}
{{- end }}
  jwt:
    providers:
      - name: {{ .JWTProviderName }}
        issuer: "{{ .Issuer }}"
        remoteJWKS:
          uri: "{{ .JWKSURI }}"
        # No claimToHeaders projection: the Go server validates the forwarded
        # bearer access token itself and maps claims to its principal, so the edge
        # does not project claims into headers.
  oidc:
    provider:
      issuer: "{{ .Issuer }}"
    clientID: "{{ .ClientID }}"
    clientSecret:
      name: {{ .ClientSecretRef }}
    redirectURL: "{{ .RedirectURL }}"
    logoutPath: "{{ .LogoutPath }}"
    forwardAccessToken: true
    passThroughAuthHeader: true
    cookieNames:
      idToken: {{ .CookieName }}
    scopes:
{{- range .Scopes }}
      - "{{ . }}"
{{- end }}
    denyRedirect:
      headers:
        - name: "Accept"
          value: "application/json"
`))

// renderGateway builds the HTTPRoutes, their BackendTrafficPolicies, and the
// OIDC SecurityPolicy.
//
// Every one of these carries the GATEWAY node's model key, not the namespace's.
// These objects are what the gateway node on the deployment diagram shows green
// or red from — the shared Envoy Gateway's own health is not the app's to
// report, but the routes and policies the app contributes to it are. Stamping
// them with the namespace key would leave the gateway node permanently
// uncoloured and would misattribute route health to the namespace.
func renderGateway(d RuntimeDesiredState) ([]Manifest, error) {
	keys := sortedModelKeys([]string{d.GatewayModelKey})
	if len(keys) == 0 || keys[0] == "" {
		return nil, fwra.New(fwra.ContractMisuse,
			"operatedruntime.render: RuntimeDesiredState.GatewayModelKey is empty — the routes and Envoy policies would be unattributable to the gateway node, which could then never show health")
	}

	var out []Manifest
	var browserFacing []string
	for _, r := range gatewayRoutes(d) {
		routeYAML, err := renderTemplate(httpRouteTmpl, r)
		if err != nil {
			return nil, err
		}
		policyYAML, err := renderTemplate(backendTrafficPolicyTmpl, r)
		if err != nil {
			return nil, err
		}
		out = append(out,
			Manifest{ModelKeys: keys, Kind: "HTTPRoute", Name: r.Name, Namespace: r.Namespace, YAML: routeYAML},
			Manifest{ModelKeys: keys, Kind: "BackendTrafficPolicy", Name: r.Name + "-policy", Namespace: r.Namespace, YAML: policyYAML},
		)
		if r.BrowserFacing {
			browserFacing = append(browserFacing, r.Name)
		}
	}

	// An app with no OIDC client is not an app with optional authentication —
	// it is an app whose front door would be published with authentication
	// REMOVED. Returning the routes without the SecurityPolicy would render
	// exactly that, and it would look like a complete manifest set. Fail like
	// every other guard here instead.
	if d.OIDC.ClientID == "" {
		return nil, fwra.New(fwra.ContractMisuse,
			"operatedruntime.render: OIDC.ClientID is empty — rendering the routes without the SecurityPolicy would publish the app's front door with authentication removed")
	}
	sort.Strings(browserFacing)
	sp := securityPolicyData{
		Name:             d.AppName + "-oidc-policy",
		Namespace:        d.Namespace,
		TargetRouteNames: browserFacing,
		JWTProviderName:  "keycloak-" + d.AppName,
		Issuer:           d.OIDC.Issuer,
		JWKSURI:          d.OIDC.Issuer + "/protocol/openid-connect/certs",
		ClientID:         d.OIDC.ClientID,
		ClientSecretRef:  d.OIDC.ClientSecretRef,
		RedirectURL:      oidcRedirectURL(d),
		LogoutPath:       oidcLogoutPath,
		CookieName:       titleFirst(d.AppName) + "IdToken",
		Scopes:           oidcScopes,
	}
	yaml, err := renderTemplate(securityPolicyTmpl, sp)
	if err != nil {
		return nil, err
	}
	out = append(out, Manifest{ModelKeys: keys, Kind: "SecurityPolicy", Name: sp.Name, Namespace: sp.Namespace, YAML: yaml})
	return out, nil
}

// oidcRedirectURL is the single source of the OAuth2 callback URL. Both the
// edge SecurityPolicy and the Keycloak client's redirectUris read it from here:
// if those two ever disagreed the manifests would still be individually valid
// and login would still be broken, so they are not allowed to be written twice.
func oidcRedirectURL(d RuntimeDesiredState) string {
	return "https://" + d.Host + oidcCallbackPath
}

// ---------------------------------------------------------------------------
// Keycloak realm + confidential OIDC client (spec D12).
// ---------------------------------------------------------------------------

// realmImportData is the KeycloakRealmImport template input.
type realmImportData struct {
	Name           string
	Namespace      string
	KeycloakCRName string

	// PlaceholderEnv is the environment-variable name the operator projects
	// the client secret into; SecretPlaceholder is the "${NAME}" reference the
	// realm body uses. The secret VALUE never appears in the CR.
	PlaceholderEnv    string
	SecretPlaceholder string
	ClientSecretRef   string
	ClientSecretKey   string

	Realm       string
	ClientID    string
	RootURL     string
	RedirectURI string
	WebOrigin   string
	// PostLogoutRedirectURIs is scoped to the app's own origin — Envoy sends a
	// post-logout redirect and Keycloak rejects any URI not registered here.
	PostLogoutRedirectURIs string
}

// realmImportTmpl renders a KeycloakRealmImport for the app's realm and its one
// confidential OIDC client.
//
// API group/version: k8s.keycloak.org/v2alpha1 — the ONLY version served by the
// keycloak-k8s-resources release the cluster pins (26.4.2). That release ships
// exactly two CRDs, Keycloak and KeycloakRealmImport; there is no separate
// client CR, so the client is carried inside the realm representation.
//
// The realm body is deliberately MINIMAL: realm identity, and one client whose
// clientId and redirectUris must agree with the edge SecurityPolicy. Every
// additional realm setting is another thing that can be silently wrong on an
// object with no production counterpart to diff against.
var realmImportTmpl = template.Must(template.New("realmimport").Parse(`apiVersion: k8s.keycloak.org/v2alpha1
kind: KeycloakRealmImport
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
spec:
  keycloakCRName: {{ .KeycloakCRName }}
  # The client secret reaches Keycloak as an environment-variable placeholder
  # projected from an existing Secret. The value is never written into this CR.
  # The Secret must live in this namespace (the operator's constraint), which is
  # also why this CR is namespaced to Keycloak rather than to the app.
  placeholders:
    {{ .PlaceholderEnv }}:
      secret:
        name: {{ .ClientSecretRef }}
        key: {{ .ClientSecretKey }}
  realm:
    realm: {{ .Realm }}
    displayName: {{ .Realm }}
    enabled: true
    clients:
    - clientId: {{ .ClientID }}
      name: {{ .ClientID }}
      protocol: openid-connect
      enabled: true
      publicClient: false
      bearerOnly: false
      clientAuthenticatorType: client-secret
      secret: "{{ .SecretPlaceholder }}"
      standardFlowEnabled: true
      implicitFlowEnabled: false
      directAccessGrantsEnabled: false
      serviceAccountsEnabled: false
      fullScopeAllowed: true
      rootUrl: {{ .RootURL }}
      baseUrl: {{ .RootURL }}
      redirectUris:
      - {{ .RedirectURI }}
      webOrigins:
      - {{ .WebOrigin }}
      attributes:
        post.logout.redirect.uris: "{{ .PostLogoutRedirectURIs }}"
`))

// renderKeycloakRealm builds the app's realm and confidential OIDC client.
//
// This is the one rendered object with NO production counterpart to diff
// against — production's realm and client are hand-managed in the admin
// console. Its unit tests are the entire safety net, and a wrong realm name,
// clientId, or redirect URI breaks login for the whole app.
//
// Two properties of the operator constrain what this CR can deliver, and both
// are the operator's semantics, not a shortcut taken here:
//
//   - Import is CREATE-ONLY. If a realm of this name already exists, the
//     operator leaves it untouched, and changes made in the admin console are
//     never synced back. So this CR PROVISIONS a new app's realm; it does not
//     reconcile an existing one. archistrator's own realm already exists, so
//     applying this to production is a no-op there by design.
//   - The placeholder Secret must be in the CR's own namespace, which must be
//     the Keycloak CR's namespace. The app's OIDC client-secret Secret
//     therefore has to exist in Keycloak's namespace as well as in the app's
//     (where the edge SecurityPolicy reads it).
func renderKeycloakRealm(d RuntimeDesiredState) ([]Manifest, error) {
	// Same reasoning as renderGateway's guard: no client means no realm and no
	// edge policy, which is an unauthenticated app, not a simpler one.
	if d.OIDC.ClientID == "" {
		return nil, fwra.New(fwra.ContractMisuse,
			"operatedruntime.render: OIDC.ClientID is empty — there is no client to provision a realm for, and the app's front door would be published unauthenticated")
	}
	if d.OIDC.ModelKey == "" {
		return nil, fwra.New(fwra.ContractMisuse,
			"operatedruntime.render: OIDC.ModelKey is empty — the rendered Keycloak realm/client CR would be unattributable to the identity-provider node")
	}

	realm := realmFromIssuer(d.OIDC.Issuer)
	if realm == "" {
		return nil, fwra.New(fwra.ContractMisuse,
			"operatedruntime.render: OIDC.Issuer "+d.OIDC.Issuer+" has no /realms/<name> segment; the realm name must come from the issuer or the edge and the realm disagree about which realm this is")
	}

	env := oidcSecretPlaceholderName(d.AppName)
	rd := realmImportData{
		Name:              d.AppName + "-realm",
		Namespace:         platformKeycloakNamespace,
		KeycloakCRName:    platformKeycloakCRName,
		PlaceholderEnv:    env,
		SecretPlaceholder: "${" + env + "}",
		ClientSecretRef:   d.OIDC.ClientSecretRef,
		ClientSecretKey:   oidcClientSecretKey,
		Realm:             realm,
		ClientID:          d.OIDC.ClientID,
		RootURL:           "https://" + d.Host,
		RedirectURI:       oidcRedirectURL(d),
		WebOrigin:         "https://" + d.Host,
		// Scoped to the app's own origin: Envoy's logout sends a post-logout
		// redirect back to the app, and Keycloak rejects any URI not
		// registered here.
		PostLogoutRedirectURIs: "https://" + d.Host + "/*",
	}
	yaml, err := renderTemplate(realmImportTmpl, rd)
	if err != nil {
		return nil, err
	}
	return []Manifest{{
		ModelKeys: []string{d.OIDC.ModelKey},
		Kind:      "KeycloakRealmImport",
		Name:      rd.Name,
		Namespace: rd.Namespace,
		YAML:      yaml,
	}}, nil
}

// realmFromIssuer extracts the realm name from a Keycloak issuer URL
// (https://host/realms/<name>). Deriving it rather than accepting it separately
// is what guarantees the realm the CR creates is the realm the edge and the
// server validate tokens against.
func realmFromIssuer(issuer string) string {
	const marker = "/realms/"
	i := strings.LastIndex(issuer, marker)
	if i < 0 {
		return ""
	}
	return strings.Trim(issuer[i+len(marker):], "/")
}

// oidcSecretPlaceholderName derives the environment-variable name the Keycloak
// operator projects the client secret into. Deterministic and shell-safe: ASCII
// letters/digits upper-cased, everything else folded to "_".
func oidcSecretPlaceholderName(appName string) string {
	var b strings.Builder
	for _, r := range appName {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String() + "_OIDC_CLIENT_SECRET"
}

// titleFirst upper-cases the first character, leaving the rest alone. Used for
// the OIDC session cookie name.
func titleFirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ---------------------------------------------------------------------------
// Argo Application.
// ---------------------------------------------------------------------------

// applicationData is the Argo Application template input.
type applicationData struct {
	AppName   string
	Namespace string
	RepoURL   string
	Path      string
	// SelfManaged switches sync from automated+prune+selfHeal to MANUAL with
	// prune disabled. See the template's comment.
	SelfManaged bool
}

// applicationTmpl renders the Argo Application that governs everything else the
// renderer emits.
//
// The compare-options annotation asks Argo for a server-side diff so the API
// server computes differences with CRD-schema knowledge (CNPG Cluster and the
// Gateway API / Envoy CRDs all carry controller-owned defaults and status).
// Without it those objects report false OutOfSync — which matters most in the
// self-managed case, where a human is reading that diff before clicking Sync.
//
// The resources-finalizer is emitted for TENANT apps only. On a tenant app,
// cascading delete is the point: withdrawing an app should take its resources
// with it. On the SELF-MANAGED app it is a second route to the same disaster
// prune: false exists to prevent — deleting or renaming the Application object
// would cascade-delete every resource archistrator runs on, and there would be
// no archistrator left to repair it. Both routes have to be closed for the
// guard to mean anything.
var applicationTmpl = template.Must(template.New("application").Parse(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{ .AppName }}
  namespace: argocd
  annotations:
    argocd.argoproj.io/compare-options: ServerSideDiff=true
{{- if not .SelfManaged }}
  finalizers:
  - resources-finalizer.argocd.argoproj.io
{{- end }}
spec:
  project: default
  source:
    repoURL: {{ .RepoURL }}
    targetRevision: main
    path: {{ .Path }}
    directory:
      recurse: true
  destination:
    server: https://kubernetes.default.svc
    namespace: {{ .Namespace }}
  syncPolicy:
{{- if .SelfManaged }}
    # SELF-MANAGED: archistrator renders the manifests that govern archistrator.
    # Sync is manual and prune is disabled so a renderer bug can never delete the
    # control plane. A human reads the diff in the Argo UI and clicks Sync.
    syncOptions:
    - CreateNamespace=true
    - ServerSideApply=true
{{- else }}
    automated:
      prune: true
      selfHeal: true
    syncOptions:
    - CreateNamespace=true
    - ServerSideApply=true
{{- end }}
`))

// renderApplication builds the Argo Application for the app.
//
// This one keeps the app's namespace-node key, unlike the gateway objects: the
// Application governs the WHOLE app, not any single piece of infrastructure in
// it, so the namespace node is the honest owner of its health.
func renderApplication(d RuntimeDesiredState) ([]Manifest, error) {
	keys := sortedModelKeys([]string{d.ModelKey})
	if len(keys) == 0 || keys[0] == "" {
		return nil, fwra.New(fwra.ContractMisuse,
			"operatedruntime.render: RuntimeDesiredState.ModelKey is empty — the Argo Application would be unattributable to any deployment-diagram node")
	}
	ad := applicationData{
		AppName:     d.AppName,
		Namespace:   d.Namespace,
		RepoURL:     platformGitOpsRepoURL,
		Path:        platformGitOpsAppPath + d.AppName,
		SelfManaged: d.SelfManaged,
	}
	yaml, err := renderTemplate(applicationTmpl, ad)
	if err != nil {
		return nil, err
	}
	// The Application object itself lives in argocd; its DESTINATION is the
	// app's own namespace (spec §5.3 invariant).
	return []Manifest{{ModelKeys: keys, Kind: "Application", Name: d.AppName, Namespace: "argocd", YAML: yaml}}, nil
}

// sortedModelKeys returns a sorted COPY of keys — a copy because sorting the
// caller's slice in place would mutate RuntimeDesiredState and make a second
// render of the same value observably different from the first.
func sortedModelKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	out := append([]string(nil), keys...)
	sort.Strings(out)
	return out
}

// render turns a typed desired state into the ordered manifest set. Pure: no
// I/O, no clock, no randomness — both PublishDesiredState's content-idempotent
// commit (Task 7) and the health overlay's on-demand model-key re-derivation
// (spec §6) depend on re-rendering the same input producing byte-identical
// output. Nothing below may iterate a map in an output path.
func render(d RuntimeDesiredState) ([]Manifest, error) {
	var out []Manifest

	for _, section := range []func(RuntimeDesiredState) ([]Manifest, error){
		renderServerWorkload,
		renderWebAppWorkload,
		renderPostgres,
		renderGateway,
		renderKeycloakRealm,
		renderApplication,
	} {
		ms, err := section(d)
		if err != nil {
			return nil, err
		}
		out = append(out, ms...)
	}

	// (Kind, Namespace, Name) is the full identity of a Kubernetes object, and
	// SliceStable keeps emission order as the last tiebreak. Sorting on
	// (Kind, Name) alone with an UNstable sort is deterministic only for as
	// long as no two objects share a Kind and Name — true of today's set by
	// luck, not by construction, and a cross-namespace collision (the Keycloak
	// CR already renders outside the app's namespace) would silently reorder
	// the output between renders. That would break both the content-idempotent
	// publish and the health overlay's re-derived model-key map.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
