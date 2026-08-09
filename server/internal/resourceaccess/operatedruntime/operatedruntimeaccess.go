// Package operatedruntime is the operatedRuntimeAccess component of the
// ResourceAccess layer — the port over the OPERATED apps' runtime substrate
// (deploy/start/stop/inspect of built-app workloads), as opposed to the
// archistrator server's own runtime.
package operatedruntime

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

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
//     functional (Task 7): they render the desired state (below) and commit it to the
//     GitOps repository ArgoCD watches, via a clone → mutate → commit-if-changed → push
//     sequence where git itself — `git diff --cached --quiet`, never a hand-rolled string
//     comparison — decides whether anything changed. GetApplicationHealth is also
//     functional (Task 9): it reads the Argo Application CR the cluster is actually
//     converging, via an in-cluster ServiceAccount — see the "Health reads" section below.
//     GetSloStatus and ReadComputeAttribution are DELIBERATELY inert (spec D7, see their
//     own doc comments) — not stubs awaiting a backend, but permanent-until-a-real-backend-
//     lands behaviour: an SLO/cost backend needs infrastructure (Prometheus, per-app SLO
//     definitions, a defensible attribution formula) that does not exist yet. WirePaymentConfig
//     still has no backend (N-DEP follow-up) and returns an EXPLICIT fwra.Unknown naming
//     that follow-up and the dry-run escape hatch — NOT a silent generated stub.
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

// GetDeploymentResourceHealth reports every model key desired declares as Healthy — the
// same "a freshly published dry-run app converges instantly" fiction GetApplicationHealth
// keeps, extended per-resource: render(desired) rather than a live cluster is the only
// source of model keys in this profile, so there is nothing to fail closed against.
func (dryRunOperatedRuntime) GetDeploymentResourceHealth(_ fwra.Context, _ uuid.UUID, desired RuntimeDesiredState) ([]ModelKeyHealth, error) {
	manifests, err := render(desired)
	if err != nil {
		return nil, err
	}
	var out []ModelKeyHealth
	for _, m := range manifests {
		for _, key := range m.ModelKeys {
			out = append(out, ModelKeyHealth{ModelKey: key, Status: RuntimeStatusHealthy})
		}
	}
	return out, nil
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
// the real GitOps repository (below); GetApplicationHealth reads the Argo Application CR
// live (Task 9, see the "Health reads" section below). GetSloStatus and
// ReadComputeAttribution are deliberately inert (spec D7 — see their own doc comments),
// not awaiting a backend. WirePaymentConfig still has no backend, so it returns an
// explicit, diagnosable fwra.Unknown (fail-fast, non-retryable — preserving the wire
// behaviour the operationsManager façade maps to a 503) naming the N-DEP follow-up and the
// dry-run escape hatch.
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
	// AppName is a PATH SEGMENT here — gitOpsAppDir joins it into the clone and
	// gitOpsPublish os.RemoveAll's the result — and it originates, several layers up, in
	// a user-supplied repository name. It is validated at the point of use, before any
	// filesystem call, rather than trusted from the caller: a "../.." or an absolute path
	// arriving here would delete outside the app's own directory in a repository that
	// holds the whole fleet's manifests.
	if !validAppName(d.AppName) {
		return fwra.New(fwra.ContractMisuse,
			"operatedruntime real profile: publishDesiredState: RuntimeDesiredState.AppName "+strconv.Quote(d.AppName)+
				" is not a DNS-1123 label (lowercase letters, digits and interior hyphens, at most 63 characters); it names a Kubernetes namespace and a directory in the GitOps repository, and must be a single safe path segment")
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
// a button.
//
// The guard is FAIL-CLOSED and reads no config: Withdraw's generated signature
// (contract.gen.go) carries only an appID, not a RuntimeDesiredState, so an earlier
// version of this verb decided self-managed-ness from a RuntimeConfig field that the
// composition root could simply forget to set — a check that quietly does nothing on its
// zero value is not a guard. This version determines it by resolving appID to the
// Application object it actually governs and reading that object's OWN committed
// syncPolicy shape (gitOpsResolveAppByID / gitOpsSelfManaged — see the GitOps commit path
// section below): the same signal applicationTmpl branches on, not a second,
// independently-settable copy of it that could drift or be left unset. If an Application
// IS found for appID but its self-managed status cannot be read off it (an unrecognizable
// syncPolicy shape), Withdraw refuses rather than guess (gitOpsResolveAppByID's doc has
// exactly which shapes those are).
//
// Otherwise, Withdraw removes that app's rendered tree and its Application. An appID with
// no match at all is success: nothing is staged, so gitOpsCommit commits nothing, matching
// the contract's NotFound⇒success withdraw semantics. KNOWN LIMITATION, not fixed here:
// "no match" covers both "never published" and "a hand-edit stripped the annotation from
// an app this system DID publish" — this guard can only find apps it published itself
// (publish always annotates, and hard-errors if it cannot, see gitOpsPublish), so an
// unannotated-but-real Application reads as already-withdrawn rather than as an error.
// That is the destructively-safe direction (nothing gets deleted) and it is what the
// contract's NotFound⇒success already promises, but it is a genuine operational surprise
// — a stray hand-edit silently makes an app un-withdrawable through this path — and is
// recorded here rather than left implicit. A stricter rule ("any unannotated Application
// ⇒ ambiguous ⇒ refuse") was considered and rejected: the real GitOps repo legitimately
// carries many hand-written Applications this system never published and will never
// annotate (aiarch-*, temporal, cedar-playground, …), and that rule would refuse every
// withdraw forever, not just the one hand-edited case it was meant to catch.
func (r realOperatedRuntime) Withdraw(_ fwra.Context, appID uuid.UUID, idempotencyKey fwra.IdempotencyKey) error {
	msg := fmt.Sprintf("operatedruntime: withdraw %s (idempotencyKey=%s)", appID, idempotencyKey)
	return r.gitOpsCommit(msg, func(workdir string) error {
		resolved, found, err := gitOpsResolveAppByID(workdir, appID)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if resolved.SelfManaged {
			return fwra.New(fwra.ContractMisuse,
				"operatedruntime real profile: withdraw refused for "+resolved.AppName+" ("+appID.String()+
					") — its committed Argo Application is self-managed (archistrator's own control plane); "+
					"deleting it would take down the thing that would have to undo the deletion; "+
					"tear it down with a deliberate kubectl operation instead")
		}
		appName := resolved.AppName
		return gitOpsWithdraw(appName)(workdir)
	})
}

func (r realOperatedRuntime) WirePaymentConfig(_ fwra.Context, _ uuid.UUID, _ GatewayBinding, _ fwra.IdempotencyKey) error {
	return r.notImplemented("wirePaymentConfig")
}

// GetApplicationHealth reads the Argo Application CR that governs appID's deployment (see
// the "Health reads" section below for how appID resolves to that CR) and folds
// status.health.status through mapArgoHealth. A CR that genuinely cannot be found — the
// cluster LIST succeeded, nothing matched — means the app has never synced: that is
// RuntimeStatusPending, not an error (the one deliberate exception; see the section doc).
// Every other failure mode — unreachable API, missing/unreadable ServiceAccount
// credential, unparseable response — returns RuntimeStatusUnknown alongside a non-nil
// error. It never falls through to reporting Healthy: that is the fail-open pattern this
// plan already rejected twice (a renderer that skipped auth on a blank client ID; a
// delete-guard that disarmed on unset config) — unknown must mean unknown here too.
func (r realOperatedRuntime) GetApplicationHealth(rc fwra.Context, appID uuid.UUID) (RuntimeStatus, error) {
	app, _, found, err := argoFindApplicationByAppID(rc.Context, appID)
	if err != nil {
		return RuntimeStatusUnknown, err
	}
	if !found {
		return RuntimeStatusPending, nil
	}
	return mapArgoHealth(app.Status.Health.Status), nil
}

// GetDeploymentResourceHealth resolves appID to its Application the same way
// GetApplicationHealth does (argoFindApplicationByAppID), then joins its
// status.resources[] against desired's own re-rendered manifest set
// (parseModelKeyHealth/joinModelKeyHealth, below) to report a RuntimeStatus PER MODEL
// KEY rather than one app-level rollup. A CR that has genuinely never synced (found is
// false, not an error) has no live resources for anything render() would emit, so it
// degrades every rendered manifest the SAME way a partially-synced app would —
// joinModelKeyHealth's own no-match rule, never a special-cased Healthy or Pending
// here. Every other failure — unreachable API, missing/unreadable ServiceAccount
// credential, unparseable response — propagates as a non-nil error and a nil result,
// never a silently reported Healthy (the fail-open pattern this plan has rejected
// three times over: a renderer skipping auth on a blank client ID, a delete-guard
// disarming on unset config, and app-level health defaulting to Healthy on read
// failure — task-10-brief).
func (r realOperatedRuntime) GetDeploymentResourceHealth(rc fwra.Context, appID uuid.UUID, desired RuntimeDesiredState) ([]ModelKeyHealth, error) {
	_, raw, found, err := argoFindApplicationByAppID(rc.Context, appID)
	if err != nil {
		return nil, err
	}
	if !found {
		manifests, rerr := render(desired)
		if rerr != nil {
			return nil, rerr
		}
		// Never synced: no live resources, and no Application to report a rollup —
		// every model key, the Application's own included, is fail-closed Degraded.
		return joinModelKeyHealth(manifests, nil, RuntimeStatusDegraded), nil
	}
	return parseModelKeyHealth(raw, desired)
}

// GetSloStatus is DELIBERATELY inert (spec D7) — this is not a stub waiting to be filled
// in. The existing 30-second Temporal Schedule calls this path on every reconcile tick; a
// real SLO verdict needs a Prometheus client and per-app SLO definitions that do not exist
// yet (spec §11 — out of scope), and an error returned here would fail that Schedule every
// 30 seconds, forever, with no operator action able to clear it. Reporting a fixed "no SLO
// configured, so none is violated" verdict keeps the Schedule green and is an honest
// description of reality: no SLO IS configured. Do not "fix" this by wiring up a
// half-finished SLO read — replace it only alongside a real Prometheus-backed
// implementation, at which point it becomes a live read like GetApplicationHealth above.
func (r realOperatedRuntime) GetSloStatus(_ fwra.Context, _ uuid.UUID) (SloStatus, error) {
	return SloStatus{SloMet: true, Detail: "SLO monitoring not configured"}, nil
}

// ReadComputeAttribution is DELIBERATELY inert (spec D7) — this is not a stub waiting to
// be filled in. Its result feeds BILLING: a defensible attribution formula needs real
// usage data and pricing rules that do not exist yet (spec §11 — out of scope), and
// inventing a number here would produce a real charge from a fabricated usage fact — worse
// than an error, because nothing downstream would flag it as wrong. An empty
// ComputeAttribution (zero RuntimeEventID) is the same "invents nothing" contract the
// LOCAL/dry-run profile already keeps (dryRunOperatedRuntime.ReadComputeAttribution
// above): the Manager appends no usage record for a zero RuntimeEventID. Do not "fix" this
// by wiring up a plausible-looking estimate — replace it only alongside a real attribution
// formula the business is willing to bill against.
func (r realOperatedRuntime) ReadComputeAttribution(_ fwra.Context, _ uuid.UUID, _ AttributionWindow) (ComputeAttribution, error) {
	return ComputeAttribution{}, nil
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
// therefore have to come from somewhere other than the call arguments, and BOTH are
// resolved by reading the ONE committed artifact Withdraw is about to delete — never from
// config, and never from a state store this component would have to keep in sync itself:
//
//  1. WHICH APP TO REMOVE. PublishDesiredState is the only place AppName and appID are
//     ever seen together, so it stamps an archistrator.dev/app-id annotation (holding
//     appID) directly onto the Application object it commits (withAppIDAnnotation) —
//     no sidecar file. The annotation is added by string-splicing the ALREADY-RENDERED
//     Application YAML, not by render() itself: render stays pure and golden-tested
//     (Task 4-6), and this bookkeeping belongs to the commit path alone. Withdraw scans
//     gitOpsApplicationsPath for the Application carrying a matching annotation and
//     recovers AppName from THAT FILE's own name (gitOpsApplicationFile's convention,
//     appName+".yaml") — see gitOpsResolveAppByID. No match anywhere is exactly the
//     NotFound⇒success case: nothing is staged, so gitOpsCommit commits nothing — but see
//     the KNOWN LIMITATION on Withdraw's own doc comment for what "no match" actually
//     covers (it is not only "never published").
//  2. WHETHER THE TARGET IS THE SELF-MANAGED APP — the load-bearing one. An earlier
//     version of this file decided this from a RuntimeConfig field the composition root
//     could simply never set, so the guard silently did nothing everywhere it was never
//     wired — the same failure-open bug class as the auth check Task 5 rejected. Reading
//     no config field cannot have that failure mode, so gitOpsSelfManaged derives it from
//     the SAME Application object located in (1)'s own committed spec.syncPolicy shape:
//     applicationTmpl emits spec.syncPolicy.automated for every TENANT app and omits it —
//     and only it — for the self-managed one (see applicationTmpl below), so this reads
//     the exact signal Argo itself acts on, not an independent copy of it that could drift
//     or be forgotten. gitOpsSelfManaged is a plain string scan of the raw committed text,
//     not a YAML parse — this package carries no YAML dependency, see its own doc comment
//     for why. What "fail closed" actually covers here, precisely: an entry that does not
//     carry a matching annotation at all (garbage, unrelated, or simply a different app)
//     is silently skipped and the scan moves on to the next candidate — it is NOT treated
//     as an error, because with a plain text scan there is nothing to fail to parse.
//     Fail-closed applies ONLY once a match IS found: an Application whose syncPolicy does
//     not resemble either shape render() ever produces, or whose text contains more than
//     one Application document (multi-doc input — see gitOpsSelfManaged), refuses the
//     determination outright (ContractMisuse) rather than guess which document's
//     syncPolicy governs the matched annotation.
// ---------------------------------------------------------------------------

const (
	// gitOpsApplicationsPath is the directory the Application object for every app lives
	// in — one level above the directory it governs (gitOpsAppDir), so deleting or
	// pruning an app's own tree can never touch the object that governs it.
	gitOpsApplicationsPath = "k8s/argocd/applications/"
	// gitOpsAppIDAnnotation is the metadata.annotations key withAppIDAnnotation stamps
	// onto every committed Application, and gitOpsResolveAppByID matches Withdraw's
	// appID argument against — see SEAM note 1 above.
	gitOpsAppIDAnnotation = "archistrator.dev/app-id"
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

// validAppName reports whether s is a DNS-1123 label: 1–63 characters of lowercase
// letters, digits and interior hyphens. Kubernetes requires it of the namespace and
// object names an app name becomes, and it simultaneously guarantees s is a single safe
// path segment for the GitOps repository — it can contain no "/", no "\" and no "." (so
// neither "." nor ".."), and cannot be empty or absolute. Every filesystem path this
// file builds from an app name is gated on it: PublishDesiredState checks the incoming
// desired state, and gitOpsResolveAppByID checks the name it derives from a committed
// file's own name before Withdraw removes anything.
//
// The operations Manager applies the identical rule to the project id at assembly time
// (its own validAppName, deploy.go). This copy is the load-bearing one — it is the check
// standing between a caller-supplied string and os.RemoveAll — and is deliberately not
// factored across the layer boundary: this package's public surface is generated-only.
func validAppName(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			// ok anywhere.
		case c == '-' && i != 0 && i != len(s)-1:
			// ok in the interior only.
		default:
			return false
		}
	}
	return true
}

// gitOpsAppDir is the directory every rendered manifest except the Application lands in.
func gitOpsAppDir(workdir, appName string) string {
	return filepath.Join(workdir, platformGitOpsAppPath, appName)
}

// gitOpsApplicationFile is where the Application object for appName is committed — one
// level above gitOpsAppDir, see the section doc.
func gitOpsApplicationFile(workdir, appName string) string {
	return filepath.Join(workdir, gitOpsApplicationsPath, appName+".yaml")
}

// gitOpsManifestFileName derives a stable, collision-free filename for a rendered
// manifest. Kind is part of the name because a workload's Deployment and Service share a
// Name.
func gitOpsManifestFileName(m manifest) string {
	return strings.ToLower(m.Kind) + "-" + m.Name + ".yaml"
}

// gitOpsAppIDAnnotationValue extracts the archistrator.dev/app-id annotation's value from
// a committed Application's raw text, via a direct line scan rather than a general YAML
// parse. This component carries no YAML library dependency — the app's operator-curated
// production dependency allowlist (internal/arch_test.go's AllowedImportPrefixes) has none
// on it, and widening that menu is explicitly reserved to an archistrator operator, not
// something a component may grant itself just because a YAML parser would be convenient
// here. The exact indentation matched ("    "+key+": ") is withAppIDAnnotation's own
// writer, so a round trip through this file always finds what it wrote. ok is false when
// no line matches that exact shape.
func gitOpsAppIDAnnotationValue(raw string) (value string, ok bool) {
	prefix := "    " + gitOpsAppIDAnnotation + ": "
	for _, line := range strings.Split(raw, "\n") {
		if v, cut := strings.CutPrefix(line, prefix); cut {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

// gitOpsSelfManaged reports whether a committed Application's raw text is self-managed,
// straight from its own committed spec.syncPolicy shape — see SEAM note 2 in the section
// doc above for why this, not a config flag, is the guard. It is a line scan for the
// EXACT two shapes applicationTmpl's two branches ever produce (see applicationTmpl
// above): the first substantive line after "  syncPolicy:" (skipping the self-managed
// branch's explanatory "    #..." comment lines) is either "    automated:" (tenant — an
// automated block always precedes syncOptions) or "    syncOptions:" directly (self-
// managed — no automated block precedes it). Matching by POSITION, not by searching the
// whole document for the substring "automated:", is deliberate: a coincidental match
// elsewhere in the file (e.g. inside a comment) must never be read as "not self-managed",
// since that is the dangerous direction to get wrong. ok is false — refuse, don't guess —
// when neither exact shape is found; that is the fail-closed half of the guard, not
// merely a parse nicety.
//
// Multi-document input is refused outright, for the same reason: anchoring on the FIRST
// "  syncPolicy:" block found is only safe when raw is a single document. In a
// hand-edited file holding more than one, that first block could belong to a tenant
// document while the archistrator.dev/app-id annotation gitOpsResolveAppByID matched
// belongs to a self-managed document further down — the last remaining way a
// self-managed Application could misread as a tenant. Impossible from this renderer
// (render() emits one Application manifest, and gitOpsPublish writes one Application per
// file) but cheap to close outright — see gitOpsHasMultipleDocuments.
func gitOpsSelfManaged(raw string) (selfManaged bool, ok bool) {
	if gitOpsHasMultipleDocuments(raw) {
		return false, false
	}
	const marker = "\n  syncPolicy:\n"
	i := strings.Index(raw, marker)
	if i < 0 {
		return false, false
	}
	for _, line := range strings.Split(raw[i+len(marker):], "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		switch line {
		case "    automated:":
			return false, true
		case "    syncOptions:":
			return true, true
		default:
			return false, false
		}
	}
	return false, false
}

// gitOpsHasMultipleDocuments reports whether raw looks like more than one YAML document:
// a "---" document-separator line, or more than one top-level "kind: Application" line.
// Either signal alone is sufficient and deliberately generous (a false positive only ever
// costs an extra refusal, never a wrong deletion) — see gitOpsSelfManaged's doc for why
// this check exists.
func gitOpsHasMultipleDocuments(raw string) bool {
	applications := 0
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "---") {
			return true
		}
		if trimmed == "kind: Application" {
			applications++
			if applications > 1 {
				return true
			}
		}
	}
	return false
}

// withAppIDAnnotation splices an archistrator.dev/app-id annotation into applicationYAML
// (applicationTmpl's rendered output) directly under its fixed compare-options
// annotation line — a targeted string insertion, not a YAML unmarshal/marshal round
// trip, so the exact hand-authored formatting an operator reads in the Argo UI (spacing,
// key order, the absence of any reordering) survives untouched apart from this one added
// line. render() itself never sees appID and stays pure — this runs only in the commit
// path, after render has already produced the Application manifest.
func withAppIDAnnotation(applicationYAML string, appID uuid.UUID) (string, error) {
	const marker = "argocd.argoproj.io/compare-options: ServerSideDiff=true\n"
	i := strings.Index(applicationYAML, marker)
	if i < 0 {
		// Can't happen — applicationTmpl always emits this exact line — but asserted
		// rather than silently committing an Application with no appID annotation, which
		// Withdraw could then never resolve back to this app (a permanent, silent
		// NotFound for an app that is very much still running).
		return "", fwra.New(fwra.ContractMisuse,
			"operatedruntime.gitops: rendered Application is missing its compare-options annotation line; cannot attach the app-id annotation")
	}
	insertAt := i + len(marker)
	return applicationYAML[:insertAt] + "    " + gitOpsAppIDAnnotation + ": " + appID.String() + "\n" + applicationYAML[insertAt:], nil
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

	// origin is always bare in every environment this runs against — the real GitOps
	// remote (GitHub) and every test remote alike (tests use `git init --bare`, see
	// access_test.go's newScratchRepo) — so a plain push needs no non-bare-repo
	// workaround. Keeping that concern entirely out of production code was a review
	// finding on an earlier version of this function.
	if _, perr := runGit(workdir, "push", "origin", "HEAD:main"); perr != nil {
		return fwra.Wrap(fwra.Infrastructure, perr, "operatedruntime.gitops: push")
	}
	return nil
}

// gitOpsPublish is PublishDesiredState's mutate closure. It renders d and replaces
// gitOpsAppDir / gitOpsApplicationFile wholesale, so a manifest render() no longer emits
// (e.g. Postgres disabled after being enabled) is removed too, not just the objects that
// changed. The Application it writes is annotated with appID (withAppIDAnnotation) — the
// only place this file stamps that mapping; there is no separate sidecar to keep in sync.
func gitOpsPublish(appID uuid.UUID, d RuntimeDesiredState) func(string) error {
	return func(workdir string) error {
		manifests, err := render(d)
		if err != nil {
			return err
		}
		if err := gitOpsRefuseSelfManagedDowngrade(workdir, d); err != nil {
			return err
		}

		appDir := gitOpsAppDir(workdir, d.AppName)
		if err := os.RemoveAll(appDir); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: reset "+appDir)
		}
		if err := os.MkdirAll(appDir, 0o750); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: create "+appDir)
		}

		var application *manifest
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
		annotated, aerr := withAppIDAnnotation(application.YAML, appID)
		if aerr != nil {
			return aerr
		}

		applicationsDir := filepath.Join(workdir, gitOpsApplicationsPath)
		if err := os.MkdirAll(applicationsDir, 0o750); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: create "+applicationsDir)
		}
		appFile := gitOpsApplicationFile(workdir, d.AppName)
		if err := os.WriteFile(appFile, []byte(annotated), 0o600); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: write "+appFile)
		}
		return nil
	}
}

// gitOpsRefuseSelfManagedDowngrade is the FOURTH self-destruction guard (2026-08-08
// final review, fix 5), and the only one that is not derived from the same single fact
// as the other three.
//
// prune:false, the omitted Argo finalizer, and Withdraw's refusal all follow from ONE
// boolean — RuntimeDesiredState.SelfManaged, set by ONE string comparison in the Manager
// (appName == "archistrator"). They are three expressions of one fact, not three
// independent guards. So a publish that arrives with SelfManaged false for a path where
// a self-managed Application already sits rewrites that Application in TENANT shape —
// prune:true, selfHeal:true, and the cascade-delete finalizer restored — disarming all
// three in a single commit, with the next Argo sync free to prune archistrator's own
// control plane.
//
// The evidence needed to refuse is already in the clone: the previously committed
// Application at this app's own path. If it exists, is determinably self-managed, and
// the incoming desired state is NOT, refuse. A downgrade from self-managed to tenant
// shape is never a legitimate automated operation — turning the platform's own
// deployment into a prunable tenant is a deliberate act for a human at a terminal.
//
// UNDETERMINABLE shape refuses too, on the same fail-closed reasoning gitOpsSelfManaged
// already applies for Withdraw: if the committed file's syncPolicy is not one this
// renderer could have produced, we cannot rule out that it is self-managed, and guessing
// wrong in this direction is the expensive one. An incoming SELF-MANAGED publish never
// refuses (there is no downgrade to make), so archistrator can always republish itself.
func gitOpsRefuseSelfManagedDowngrade(workdir string, d RuntimeDesiredState) error {
	if d.SelfManaged {
		return nil
	}
	appFile := gitOpsApplicationFile(workdir, d.AppName)
	raw, err := os.ReadFile(appFile) // #nosec G304 -- appFile is built from a validated app name (validAppName), not from raw input
	if err != nil {
		if os.IsNotExist(err) {
			return nil // first publish for this app — nothing to downgrade.
		}
		return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: read "+appFile)
	}
	selfManaged, ok := gitOpsSelfManaged(string(raw))
	if !ok {
		return fwra.New(fwra.ContractMisuse,
			"operatedruntime.gitops: refusing to publish "+d.AppName+
				": the committed Argo Application at "+appFile+" has a syncPolicy shape this renderer could not have produced, so it cannot be confirmed NOT self-managed; "+
				"overwriting it could turn archistrator's own control plane into a prunable tenant")
	}
	if selfManaged {
		return fwra.New(fwra.ContractMisuse,
			"operatedruntime.gitops: refusing to publish "+d.AppName+
				": the committed Argo Application at "+appFile+" is self-managed (prune:false, manual sync, no cascade finalizer) and this desired state is not; "+
				"rewriting it in tenant shape would disarm all three self-destruction guards in one commit. "+
				"A self-managed to tenant downgrade is a deliberate human operation, never an automated publish")
	}
	return nil
}

// gitOpsResolvedApp is what gitOpsResolveAppByID reports about the Application whose
// archistrator.dev/app-id annotation matches the appID being withdrawn.
type gitOpsResolvedApp struct {
	AppName     string
	SelfManaged bool
}

// gitOpsResolveAppByID scans every Application committed under gitOpsApplicationsPath and
// returns the one annotated archistrator.dev/app-id == appID (gitOpsAppIDAnnotationValue),
// deriving AppName from that file's own name (gitOpsApplicationFile's convention,
// appName+".yaml") and SelfManaged directly from that SAME object's committed syncPolicy
// shape (gitOpsSelfManaged — see SEAM note 2 in the section doc above; this is the
// fail-closed guard, not a side detail).
//
// found is false — not an error — only when no Application anywhere carries a matching
// annotation, which is what lets Withdraw implement the contract's NotFound⇒success
// semantics as an ordinary no-op mutation (nothing staged ⇒ gitOpsCommit commits
// nothing). Once a file DOES match the annotation, though, its self-managed shape must be
// determinable or the whole call fails loudly (ContractMisuse) rather than defaulting
// either way — see gitOpsSelfManaged's doc for why that default would be the dangerous
// direction to get wrong.
func gitOpsResolveAppByID(workdir string, appID uuid.UUID) (resolved gitOpsResolvedApp, found bool, err error) {
	dir := filepath.Join(workdir, gitOpsApplicationsPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return gitOpsResolvedApp{}, false, nil
		}
		return gitOpsResolvedApp{}, false, fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: list "+dir)
	}

	want := appID.String()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, rerr := os.ReadFile(path) // #nosec G304 -- e.Name() comes from os.ReadDir's own listing of dir, not external input
		if rerr != nil {
			return gitOpsResolvedApp{}, false, fwra.Wrap(fwra.Infrastructure, rerr, "operatedruntime.gitops: read "+path)
		}
		if v, ok := gitOpsAppIDAnnotationValue(string(raw)); !ok || v != want {
			continue
		}
		selfManaged, ok := gitOpsSelfManaged(string(raw))
		if !ok {
			return gitOpsResolvedApp{}, false, fwra.New(fwra.ContractMisuse,
				"operatedruntime.gitops: "+path+" matches appId "+want+" but its syncPolicy shape is not one render() could have produced; refusing to determine self-managed status")
		}
		// The app name is derived from a FILE NAME in the repository, and it is about to
		// be joined into a path Withdraw calls os.RemoveAll on. A file called "..yaml"
		// would trim to "." and remove the whole apps directory; the same rule that gates
		// publish gates this derived name too, rather than trusting the repository's
		// contents.
		appName := strings.TrimSuffix(e.Name(), ".yaml")
		if !validAppName(appName) {
			return gitOpsResolvedApp{}, false, fwra.New(fwra.ContractMisuse,
				"operatedruntime.gitops: "+path+" matches appId "+want+" but its file name does not yield a valid app name (a DNS-1123 label); refusing to derive a filesystem path from it")
		}
		return gitOpsResolvedApp{AppName: appName, SelfManaged: selfManaged}, true, nil
	}
	return gitOpsResolvedApp{}, false, nil
}

// gitOpsWithdraw is Withdraw's mutate closure for an app that WAS found and confirmed NOT
// self-managed: it removes appName's rendered tree and its Application. Removing a path
// that is already gone is not an error (os.RemoveAll is idempotent that way already;
// os.Remove is made so explicitly below) — matching the contract's NotFound⇒success
// semantics all the way through, not just at the gitOpsResolveAppByID lookup.
func gitOpsWithdraw(appName string) func(string) error {
	return func(workdir string) error {
		if err := os.RemoveAll(gitOpsAppDir(workdir, appName)); err != nil {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: remove app dir")
		}
		if err := os.Remove(gitOpsApplicationFile(workdir, appName)); err != nil && !os.IsNotExist(err) {
			return fwra.Wrap(fwra.Infrastructure, err, "operatedruntime.gitops: remove application file")
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
// RESIDUAL (P1), updated for Task 7 — this note previously described an unbuilt
// backend; that is now stale. PublishDesiredState/Withdraw's GitOps commit path IS
// implemented (see the GitOps commit path section above), but RuntimeConfig.GitOpsRepoURL
// / GitOpsToken are STILL DROPPED here — this constructor passes an empty RuntimeConfig,
// so both real verbs still surface the "unconfigured" diagnostic in production today.
// Composition-root wiring (an env var → VariantHookArgs, mirroring the github variants)
// is a separate follow-up, out of Task 7's scope.

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

// manifest is one rendered Kubernetes object plus the deployment-model node(s)
// it came from. ModelKeys is what lets the health overlay attribute a live
// resource back to a diagram node without storing a separate mapping table.
//
// UNEXPORTED (Task 10): an earlier draft of this plan exported manifest and
// resourceHealth so the Manager layer could join them itself. Task 9's review
// rejected that — a Manager-side join would have reintroduced the exact
// Kubernetes-vocabulary leak decision D11 removed. The join now lives here,
// in parseModelKeyHealth below, where both inputs already are; the Manager
// only ever sees the substrate-neutral ModelKeyHealth/RuntimeStatus the
// GetDeploymentResourceHealth verb returns.
type manifest struct {
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
func renderServerWorkload(d RuntimeDesiredState) ([]manifest, error) {
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
func renderWebAppWorkload(d RuntimeDesiredState) ([]manifest, error) {
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
func renderWorkloadManifests(wd workloadData, modelKeys []string) ([]manifest, error) {
	depYAML, err := renderTemplate(deploymentTmpl, wd)
	if err != nil {
		return nil, err
	}
	svcYAML, err := renderTemplate(serviceTmpl, wd)
	if err != nil {
		return nil, err
	}
	return []manifest{
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
func renderPostgres(d RuntimeDesiredState) ([]manifest, error) {
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
	return []manifest{{ModelKeys: keys, Kind: "Cluster", Name: cd.Name, Namespace: cd.Namespace, YAML: yaml}}, nil
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
func renderGateway(d RuntimeDesiredState) ([]manifest, error) {
	keys := sortedModelKeys([]string{d.GatewayModelKey})
	if len(keys) == 0 || keys[0] == "" {
		return nil, fwra.New(fwra.ContractMisuse,
			"operatedruntime.render: RuntimeDesiredState.GatewayModelKey is empty — the routes and Envoy policies would be unattributable to the gateway node, which could then never show health")
	}

	var out []manifest
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
			manifest{ModelKeys: keys, Kind: "HTTPRoute", Name: r.Name, Namespace: r.Namespace, YAML: routeYAML},
			manifest{ModelKeys: keys, Kind: "BackendTrafficPolicy", Name: r.Name + "-policy", Namespace: r.Namespace, YAML: policyYAML},
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
	out = append(out, manifest{ModelKeys: keys, Kind: "SecurityPolicy", Name: sp.Name, Namespace: sp.Namespace, YAML: yaml})
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
func renderKeycloakRealm(d RuntimeDesiredState) ([]manifest, error) {
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
	return []manifest{{
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
func renderApplication(d RuntimeDesiredState) ([]manifest, error) {
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
	return []manifest{{ModelKeys: keys, Kind: "Application", Name: d.AppName, Namespace: "argocd", YAML: yaml}}, nil
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
func render(d RuntimeDesiredState) ([]manifest, error) {
	var out []manifest

	for _, section := range []func(RuntimeDesiredState) ([]manifest, error){
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

// ---------------------------------------------------------------------------
// Health reads — Argo Application CR (Task 9, spec D7/D10,
// docs/superpowers/specs/2026-08-07-operations-argocd-deployment-design.md).
//
// GetApplicationHealth (above) is the one REAL-profile observability verb with a live
// backend today: it reads the Argo Application object straight off the Kubernetes API via
// the in-cluster ServiceAccount every server pod already carries — no API token to mint or
// distribute (spec §3). This is a credential entirely separate from RuntimeConfig's
// GitOpsToken (spec §7): read-only Argo-CR access via the cluster API, not git write
// access to the GitOps repo. GetSloStatus and ReadComputeAttribution stay deliberately
// inert (D7, see their own doc comments above) — this section implements ONLY the health
// half.
//
// appID -> Application resolution mirrors Withdraw's own scheme (see the GitOps commit
// path section's SEAM note 1, above): PublishDesiredState stamps an archistrator.dev/app-id
// annotation onto the Application it commits (withAppIDAnnotation), and that annotation is
// ordinary object metadata — it survives Argo's sync onto the live in-cluster object
// unchanged. argoFindApplicationByAppID therefore does not need, and deliberately avoids
// depending on, the GitOps repo credential at all: it LISTs every Application in the argocd
// namespace via the cluster API and matches the annotation client-side — the in-cluster
// counterpart of gitOpsAppIDAnnotationValue's own text scan of the committed YAML. No match
// found is exactly the "never synced" case (see GetApplicationHealth's own doc comment),
// not an error.
//
// Fail-closed is the entire point of this section — the same rule Task 5's auth check and
// Task 7's self-managed guard were both rewritten to satisfy after failing open on a zero
// value. An unreachable API server, a missing or unreadable ServiceAccount token/CA cert,
// or a response this code cannot parse must all surface as an explicit, non-nil error —
// NEVER as a silently reported Healthy. RuntimeStatusUnknown is the return value paired
// with that error; only a CR that is genuinely, confirmedly absent (the LIST itself
// succeeded) reports RuntimeStatusPending with a nil error.
// ---------------------------------------------------------------------------

const (
	// argoNamespace is where every Application object lives in the cluster — the
	// in-cluster counterpart of gitOpsApplicationsPath; applicationTmpl always sets
	// metadata.namespace: argocd.
	argoNamespace = "argocd"
	// inClusterTokenPath / inClusterCACertPath are the fixed paths every Kubernetes pod's
	// projected ServiceAccount mounts to — the same "in-cluster config" convention
	// client-go's InClusterConfig reads, reimplemented here in stdlib net/http rather than
	// importing client-go: TestMethodLayering's operator-curated dependency allowlist
	// (internal/arch_test.go's AllowedImportPrefixes) carries no k8s.io/ entry, and
	// widening it is reserved to an archistrator operator — this component may not grant
	// itself a new production dependency just because a real client would be convenient
	// here (mirrors the no-YAML-library decision on gitOpsAppIDAnnotationValue's doc
	// comment).
	inClusterTokenPath  = "/var/run/secrets/kubernetes.io/serviceaccount/token" //nolint:gosec // G101 false positive: a fixed mount PATH, not a credential value
	inClusterCACertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// argoApplication is the minimal shape this file reads off the Argo Application CR's JSON:
// metadata.annotations to resolve appID -> Application (see the section doc above), and
// status.health / status.resources for RuntimeStatus and per-resource health respectively.
// Every other field on the real object (spec.source, spec.destination, status.sync, …) is
// intentionally unparsed — this is a read path, not a round-trip, and adding a field here
// is cheap whenever a later task needs one.
type argoApplication struct {
	Metadata struct {
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Status struct {
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
		Resources []struct {
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
			// Status is the entry's SYNC status ("Synced" / "OutOfSync"); Health is a
			// separate object that is absent entirely for kinds Argo has no health
			// check for. Two different facts, and the per-resource fold needs both —
			// see mapArgoResourceHealth.
			Status string `json:"status"`
			Health struct {
				Status string `json:"status"`
			} `json:"health"`
		} `json:"resources"`
	} `json:"status"`
}

// argoApplicationListEnvelope is the LIST envelope the Kubernetes API wraps a collection
// of Application items in. Items are kept as raw JSON (json.RawMessage), not
// fully-decoded structs: argoFindApplicationByAppID needs both the matched item's
// DECODED form (GetApplicationHealth's app-level rollup) and its own RAW bytes
// (GetDeploymentResourceHealth's per-resource join, parseModelKeyHealth, which re-parses
// status.resources[] from exactly what the cluster returned) — one LIST body, one decode
// pass, two consumers.
type argoApplicationListEnvelope struct {
	Kind  string            `json:"kind"`
	Items []json.RawMessage `json:"items"`
}

// parseApplicationListEnvelope decodes body and asserts it is actually the LIST envelope
// (kind: ApplicationList) the Kubernetes API returns for a successful LIST call — the
// Task 10 fold-in of a Task 9 Minor finding. Before this check, a 200 response carrying
// valid-but-unexpected JSON (no top-level "items" key at all, say) unmarshaled silently
// into a zero-Items envelope, which argoFindApplicationByAppID could not then distinguish
// from a genuine "no Application has synced yet" — an unexpected payload reading as the
// same benign RuntimeStatusPending a never-synced app reports. Asserting the envelope's
// own kind makes that distinction explicit: only a confirmed ApplicationList reaches the
// annotation search below; anything else is a real, diagnosable error.
func parseApplicationListEnvelope(body []byte) (argoApplicationListEnvelope, error) {
	var envelope argoApplicationListEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return argoApplicationListEnvelope{}, fwra.Wrap(fwra.Infrastructure, err, "operatedruntime: parse Argo Application list response")
	}
	if envelope.Kind != "ApplicationList" {
		return argoApplicationListEnvelope{}, fwra.New(fwra.Infrastructure,
			fmt.Sprintf("operatedruntime: parse Argo Application list response: unexpected shape (kind=%q, want ApplicationList)", envelope.Kind))
	}
	return envelope, nil
}

// resourceHealth is one entry of an Argo Application's status.resources[] — PER-RESOURCE
// health, as opposed to the app-level rollup mapArgoHealth folds. joinModelKeyHealth
// (below) joins this against the model-key map re-derived by re-rendering, reusing
// mapArgoHealth's own OBSERVED/absent-status distinction for the per-resource fold too —
// there is no separate Progressing/Pending-aware mapping for resources: an individual
// resource mid-rollout is exactly as unsafe to report Healthy as the whole app is (D10).
type resourceHealth struct {
	Kind      string
	Name      string
	Namespace string
	Health    string
	// Sync is the entry's status.resources[].status — "Synced", "OutOfSync", or empty.
	// It is what makes a HEALTHLESS entry readable at all (mapArgoResourceHealth): for a
	// kind Argo cannot grade, whether it is synced is the only fact about it there is.
	Sync string
}

// mapArgoHealth folds an Argo Application's status.health.status into RuntimeStatus. Only
// the literal "Healthy" is green; every other OBSERVED value — "Progressing" included —
// maps to RuntimeStatusDegraded, never RuntimeStatusHealthy: an app mid-rollout, syncing,
// missing a resource, or suspended is not safe to report as healthy. Per spec D10 this is
// a deliberate asymmetry, not a bug — the diagram is meant to read red mid-rollout until it
// settles, in favour of glance-readability over an amber "maybe fine" state. An EMPTY
// status is a different fact from an observed-and-bad one — Argo has not evaluated health
// at all yet — so it maps to RuntimeStatusUnknown, not Degraded: collapsing "no verdict
// yet" into "bad verdict" would make Unknown unreachable from a live read.
func mapArgoHealth(status string) RuntimeStatus {
	switch status {
	case "":
		return RuntimeStatusUnknown
	case "Healthy":
		return RuntimeStatusHealthy
	default:
		return RuntimeStatusDegraded
	}
}

// parseResourceHealth extracts status.resources[] from a raw Argo Application JSON
// document (testdata/argo/*.json fixtures are exactly this shape). Used both by this
// file's own tests and, per spec §6, by the later deployment-diagram health-overlay task
// that joins the result against the re-derived model-key map.
func parseResourceHealth(raw []byte) ([]resourceHealth, error) {
	var app argoApplication
	if err := json.Unmarshal(raw, &app); err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "operatedruntime: parse Argo Application status.resources")
	}
	out := make([]resourceHealth, 0, len(app.Status.Resources))
	for _, r := range app.Status.Resources {
		out = append(out, resourceHealth{Kind: r.Kind, Name: r.Name, Namespace: r.Namespace, Health: r.Health.Status, Sync: r.Status})
	}
	return out, nil
}

// mapArgoResourceHealth folds ONE entry of status.resources[] into RuntimeStatus. It
// differs from mapArgoHealth (the app-level rollup) in exactly one case, and that case
// is the point of it: a per-resource entry with an ABSENT health field means Argo has no
// health CHECK for that kind, not that the resource is unhealthy or unevaluated.
//
// Argo only reports health for kinds it has a checker for — built-ins plus the Lua
// scripts it ships. Four of the objects this renderer emits have none: SecurityPolicy and
// BackendTrafficPolicy (Envoy Gateway), KeycloakRealmImport, and HTTPRoute. They appear in
// status.resources[] with a sync status and no health at all. Folding that through
// mapArgoHealth gave RuntimeStatusUnknown, which the diagram paints red — so the gateway
// and identity-provider nodes read permanently unhealthy on a perfectly healthy cluster.
//
// For an ungradeable kind, then, SYNC IS THE ONLY FACT THERE IS, and it is the fact that
// matters: archistrator's own Application is manual-sync by design (the D8 self-guard), so
// the window between a Deploy publish and the operator pressing Sync is not an edge case —
// it is a normal state on every deploy. Reporting green for an OutOfSync object in that
// window would claim a change landed when it has not, which is the single failure this
// overlay exists to prevent. Only an explicitly "Synced" entry may
// be green: OutOfSync, an unrecognized value, and a MISSING sync status all fold to
// Degraded, because none of them is evidence the object is applied.
//
// A gradeable kind keeps Argo's own health verdict unchanged — Argo grades those against
// the live object, and second-guessing it here would be this file inventing a rule.
//
// The fail-closed case remains ABSENCE from status.resources[] entirely (joinModelKeyHealth's
// no-match rule): a genuinely different fact — the renderer emitted an object the cluster
// does not have at all.
func mapArgoResourceHealth(health, sync string) RuntimeStatus {
	if health == "" {
		if sync == "Synced" {
			return RuntimeStatusHealthy
		}
		return RuntimeStatusDegraded
	}
	return mapArgoHealth(health)
}

// joinModelKeyHealth is the health-overlay join itself (task-10-brief Step 4/spec §6):
// match each of manifests' (Kind, Name, Namespace) against resources, and emit one
// ModelKeyHealth per ModelKey a manifest carries — fanning out where a manifest carries
// several (the Postgres Cluster carries all three database-role model keys, spec §5.1a).
//
// FAIL-CLOSED: a manifest render() emits with NO matching entry in resources reports
// RuntimeStatusDegraded, never RuntimeStatusHealthy and never a silently dropped
// ModelKeyHealth. It should exist on the cluster and does not — that absence is exactly
// what red is for, not something to paper over as "nothing to report." resources being
// nil/empty (the never-synced case, GetDeploymentResourceHealth above) degrades every
// manifest the same way, by the same rule — there is no separate branch for it.
//
// THE APPLICATION IS NOT JOINED AGAINST ITS OWN RESOURCE LIST (2026-08-08 final review,
// fix 2). An Argo Application never lists ITSELF among status.resources[] — that list is
// what the Application manages, not what it is. Joining it therefore hit the fail-closed
// no-match rule on every read, painting the app's namespace node (the node the
// Application carries, renderApplication) permanently red no matter how healthy the
// deployment was. The Application is the thing REPORTING, so its model keys take its own
// app-level rollup instead, passed in as applicationStatus — the honest verdict for "how
// is this whole app doing", and the same value GetApplicationHealth returns. The
// never-synced path passes RuntimeStatusDegraded, keeping that case fail-closed.
func joinModelKeyHealth(manifests []manifest, resources []resourceHealth, applicationStatus RuntimeStatus) []ModelKeyHealth {
	type identity struct{ Kind, Name, Namespace string }
	byIdentity := make(map[identity]resourceHealth, len(resources))
	for _, r := range resources {
		byIdentity[identity{Kind: r.Kind, Name: r.Name, Namespace: r.Namespace}] = r
	}

	var out []ModelKeyHealth
	for _, m := range manifests {
		status := RuntimeStatusDegraded // fail-closed default: rendered, but no live match.
		switch {
		case m.Kind == "Application":
			status = applicationStatus
		default:
			if r, ok := byIdentity[identity{Kind: m.Kind, Name: m.Name, Namespace: m.Namespace}]; ok {
				status = mapArgoResourceHealth(r.Health, r.Sync)
			}
		}
		for _, key := range m.ModelKeys {
			out = append(out, ModelKeyHealth{ModelKey: key, Status: status})
		}
	}
	return out
}

// parseModelKeyHealth re-renders desired — render's purity is what makes this safe (spec
// §6): re-deriving the model-key -> (kind, name, namespace) map on demand, rather than
// persisting one that could drift from the renderer — and joins the result against raw
// (an Argo Application CR's JSON body, the same shape parseResourceHealth reads) via
// joinModelKeyHealth. The RA-verb boundary function GetDeploymentResourceHealth calls
// this directly when the CR was found; it is exported to this file's tests as the
// pure, HTTP-free join to exercise (task-10-brief Step 2/4).
func parseModelKeyHealth(raw []byte, desired RuntimeDesiredState) ([]ModelKeyHealth, error) {
	manifests, err := render(desired)
	if err != nil {
		return nil, err
	}
	var app argoApplication
	if uerr := json.Unmarshal(raw, &app); uerr != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, uerr, "operatedruntime: parse Argo Application")
	}
	resources, err := parseResourceHealth(raw)
	if err != nil {
		return nil, err
	}
	// The Application's own model keys take its app-level rollup, not a resource match
	// it can never have — see joinModelKeyHealth.
	return joinModelKeyHealth(manifests, resources, mapArgoHealth(app.Status.Health.Status)), nil
}

// inClusterHTTPClient builds an *http.Client trusting the cluster's own CA and returns the
// API server's base URL plus the ServiceAccount bearer token — the pieces of the standard
// in-cluster config convention (KUBERNETES_SERVICE_HOST/PORT env vars, the projected token
// and CA cert files) that client-go's InClusterConfig also reads, reimplemented in stdlib
// because client-go itself is off the allowlist (see the section doc above). Every failure
// here is explicit: a pod either has its ServiceAccount projected or it does not, and no
// amount of waiting changes that without a redeploy — but it is still surfaced as
// fwra.Infrastructure (retryable=true by default) rather than ever silently reporting
// health, because "backing infra unavailable" is exactly what it is from the caller's
// perspective, and the Schedule retrying costs nothing (spec D7).
func inClusterHTTPClient() (client *http.Client, apiServer, token string, err error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, "", "", fwra.New(fwra.Infrastructure,
			"operatedruntime: not running in-cluster (KUBERNETES_SERVICE_HOST/KUBERNETES_SERVICE_PORT unset); "+
				"health reads require the in-cluster ServiceAccount")
	}

	tokenBytes, terr := os.ReadFile(inClusterTokenPath)
	if terr != nil {
		return nil, "", "", fwra.Wrap(fwra.Infrastructure, terr, "operatedruntime: read in-cluster ServiceAccount token")
	}
	caBytes, cerr := os.ReadFile(inClusterCACertPath)
	if cerr != nil {
		return nil, "", "", fwra.Wrap(fwra.Infrastructure, cerr, "operatedruntime: read in-cluster ServiceAccount CA cert")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caBytes) {
		return nil, "", "", fwra.New(fwra.Infrastructure, "operatedruntime: in-cluster ServiceAccount CA cert is not valid PEM")
	}

	client = &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
	}
	return client, "https://" + net.JoinHostPort(host, port), strings.TrimSpace(string(tokenBytes)), nil
}

// argoFindApplicationByAppID lists every Application in the argocd namespace via the
// cluster API and returns the one whose archistrator.dev/app-id annotation matches appID —
// the in-cluster counterpart of gitOpsResolveAppByID's own annotation match (see the
// section doc above for why this reads the cluster rather than the GitOps repo). found is
// false — not an error — only when the LIST itself succeeded, was confirmed to be a
// well-formed ApplicationList (parseApplicationListEnvelope), and genuinely no Application
// carries a matching annotation: that is "this app has never synced," the one case
// GetApplicationHealth maps to RuntimeStatusPending rather than an error. Any failure to
// reach the API, read its response, or parse the body is a real error and is never
// conflated with "not found."
//
// raw is the matched item's OWN raw JSON bytes (nil when found is false) — kept alongside
// the decoded app so GetDeploymentResourceHealth's per-resource join (parseModelKeyHealth)
// re-parses status.resources[] from exactly what the cluster returned, the same shape
// parseResourceHealth's own fixtures use, rather than a re-marshaled copy of app that could
// silently diverge from it.
//
// ctx propagates the caller's fwra.Context.Context (Task 10 fold-in of a Task 9 Minor
// finding: this request used to be built with the context-less http.NewRequest, which
// meant an activity deadline or cancellation was never honoured by the outbound call).
func argoFindApplicationByAppID(ctx context.Context, appID uuid.UUID) (app argoApplication, raw []byte, found bool, err error) {
	client, apiServer, token, cerr := inClusterHTTPClient()
	if cerr != nil {
		return argoApplication{}, nil, false, cerr
	}

	listURL := apiServer + "/apis/argoproj.io/v1alpha1/namespaces/" + argoNamespace + "/applications"
	req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if rerr != nil {
		return argoApplication{}, nil, false, fwra.Wrap(fwra.Infrastructure, rerr, "operatedruntime: build Argo Application list request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, derr := client.Do(req)
	if derr != nil {
		return argoApplication{}, nil, false, fwra.Wrap(fwra.Infrastructure, derr, "operatedruntime: list Argo Applications")
	}
	defer func() { _ = resp.Body.Close() }()

	body, berr := io.ReadAll(resp.Body)
	if berr != nil {
		return argoApplication{}, nil, false, fwra.Wrap(fwra.Infrastructure, berr, "operatedruntime: read Argo Application list response")
	}
	if resp.StatusCode != http.StatusOK {
		return argoApplication{}, nil, false, fwra.New(fwra.Infrastructure,
			fmt.Sprintf("operatedruntime: list Argo Applications: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
	}

	envelope, perr := parseApplicationListEnvelope(body)
	if perr != nil {
		return argoApplication{}, nil, false, perr
	}

	want := appID.String()
	for _, item := range envelope.Items {
		var a argoApplication
		if uerr := json.Unmarshal(item, &a); uerr != nil {
			return argoApplication{}, nil, false, fwra.Wrap(fwra.Infrastructure, uerr, "operatedruntime: parse Argo Application list item")
		}
		if a.Metadata.Annotations[gitOpsAppIDAnnotation] == want {
			return a, []byte(item), true, nil
		}
	}
	return argoApplication{}, nil, false, nil
}
