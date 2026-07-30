// Package agenticjob is the agenticJobAccess component of the
// ResourceAccess layer — the port over the construction-pipeline runtime that
// dispatches and observes agentic construction jobs (GitHub Actions realisation
// below; see agenticJobAccess.md).
package agenticjob

// actions.go is the GITHUB-ACTIONS-backed realisation of the
// AgenticJobAccess port (agenticJobAccess.md §6 infrastructure
// mapping) — the C-CP-R rework that swapped the construction-pipeline runtime from
// Argo Workflows on Kubernetes to the USER'S GitHub Actions (the 2026-06-09 pivot:
// the user's GitHub + Actions, no Argo). It REPLACES the former argo.go /
// argo_http_client.go.
//
// THE LOAD-BEARING LAYER RULE is unchanged from the frozen contract: this RA's
// PUBLIC surface (agenticjob.go) carries ZERO GitHub-Actions lexemes
// (workflow_dispatch, workflow_run, run id, ref, owner/repo) and imports NO
// Temporal. The three atomic, infrastructure-opaque business verbs — submit /
// observe / cancel one construction pipeline — are unchanged. ALL GitHub-Actions
// vocabulary is confined to (a) the ghActionsClient seam below + its concrete
// realisation in actions_http_client.go, and (b) the github satellite
// (framework-go-infrastructure-github/actions.go). A GitHub-Actions type never
// crosses the port.
//
// §6 INFRA MAPPING AS BUILT (the table the frozen contract anticipated):
//   - submit  → resolve-then-workflow_dispatch (with the idempotency-token input),
//               then resolve the canonical run.
//   - observe → list/get the run, map status+conclusion → PipelineObservation.
//   - cancel  → cancel the canonical run (already-terminal/absent == success).
//
// THE IDEMPOTENCY CONVERGENCE (the hard design point — the GitHub-Actions analog of
// Argo's "reject duplicate Workflow name"): GitHub's workflow_dispatch has NO
// duplicate dedup, so the contract's convergence guarantee (§2.1, §6: same spec +
// same key ⇒ SAME handle, duplicate == already-exists success) is reconstructed
// here on a DETERMINISTIC anchor:
//
//  1. derive a deterministic dedup token from the caller-supplied idempotencyKey
//     (sha256 → hex; same key ⇒ same token), exactly as the Argo path derived a
//     deterministic Workflow name from the key.
//  2. PROBE: list runs carrying run-name "aiarch-cp-"+token. If ≥1 exists, do NOT
//     dispatch — return the CANONICAL run (deterministically the LOWEST run id).
//  3. else DISPATCH with the token input, then resolve the run(s) carrying the name
//     (bounded retry for GitHub's dispatch→run-creation eventual consistency).
//  4. SELECT the canonical run = lowest run id — a TOTAL ORDER both racers compute
//     identically over the same observable run set, so two concurrent submits with
//     the same key CONVERGE on the same handle even if both raced past the probe and
//     both dispatched.
//  5. RECONCILE: cancel every NON-canonical sibling carrying the token, so the
//     convergence is not merely handle-equal but run-EFFECTIVE — exactly one run
//     proceeds. Cancelling a sibling is idempotent (already-terminal/absent ==
//     success), so the reconcile is safe under the race and under retry.
//
// This genuinely converges WITHOUT any atomic dedup primitive: the lowest-run-id
// total order is the convergence point; the sibling-cancel collapses the transient
// double-run. The hard exit gate TestSubmitIdempotencyConvergence proves it.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ---------------------------------------------------------------------------
// Internal client seam
// ---------------------------------------------------------------------------

// ghRun is the package-internal, infrastructure-neutral-AT-THE-SEAM view of one
// GitHub Actions run the RA reads. It mirrors the satellite's WorkflowRun but lives
// in the RA package so the seam (and its fake) carry no satellite import — the
// concrete realisation in actions_http_client.go bridges satellite→ghRun. NONE of
// these fields crosses the public port.
type ghRun struct {
	id         int64
	name       string
	status     string // "queued" | "in_progress" | "completed" | …
	conclusion string // "success" | "failure" | "cancelled" | … (when completed)
	// htmlURL is the run's browser URL, built by the concrete realisation (the ONLY
	// place the github.com lexeme lives). Surfaced on EVERY observation so the Manager
	// can deep-link the operator to the live run while it drafts (QA F-GTD-6) and to
	// the failed run on a terminal failure (QA F15 gap 2b). Empty when the realisation
	// cannot resolve it (e.g. the fake in a test that does not set it).
	htmlURL string
}

// ghTarget is the per-CALL repo + workflow-file the seam addresses (the additive
// per-project-design-dispatch override). A ZERO ghTarget means "use the seam's
// configured default" (the construction repo + aiarch-construct.yml) — the
// concrete realisation substitutes its configured Owner/Repo/WorkflowFile when a
// field is empty, so the existing UC3 caller is byte-for-byte unchanged. A non-zero
// ghTarget routes the call to the per-project repo + aiarch-design.yml. NONE of
// these fields crosses the public port — they are derived inside the RA from the
// public PipelineSpec.TargetRepo / WorkflowFile and the PipelineHandle encoding.
type ghTarget struct {
	owner        string
	repo         string
	workflowFile string
}

// isZero reports whether the target carries no override (fall back to the seam's
// configured default).
func (t ghTarget) isZero() bool {
	return t.owner == "" && t.repo == "" && t.workflowFile == ""
}

// ghActionsClient is the INTERNAL seam over the minimal set of GitHub Actions REST
// operations this RA needs. It is the ONLY thing the RA's verbs call, so the RA can
// be unit-tested with a fake and never needs a live GitHub. The concrete
// realisation (ghActionsRESTClient in actions_http_client.go) delegates to the
// github satellite's AppClient (App-JWT → installation token minted internally,
// then the Actions REST calls). A future hosted-CI realisation would implement this
// same seam.
//
// EVERY GitHub-Actions lexeme is confined to this seam, its concrete realisation,
// and the satellite. The seam carries the dedup-token RUN NAME (runName) as an
// opaque string the RA computes; the seam never derives it.
//
// Each verb takes a ghTarget: the per-call repo + workflow-file the call addresses.
// A ZERO ghTarget means the seam's configured default (the construction repo +
// aiarch-construct.yml). This is what lets the DESIGN caller retarget the dispatch /
// observe / cancel at the per-project repo without changing the FROZEN public surface
// (the override rides on the additive PipelineSpec.TargetRepo + the handle encoding).
type ghActionsClient interface {
	// listRunsByName returns every run whose display name == runName (the
	// idempotency anchor) in the targeted repo+workflow. An empty result is not an error.
	listRunsByName(ctx context.Context, tgt ghTarget, runName string) ([]ghRun, error)
	// dispatch triggers a workflow_dispatch in the targeted repo+workflow carrying the
	// idempotency token (which the dispatched aiarch workflow stamps into the run name as
	// runName) PLUS the caller's optional extra DispatchInputs (the additive D-MSD-Δ
	// design-dispatch inputs — agenticJobAccess.md §0d.6). The seam merges the
	// extra inputs FIRST and the RA-controlled idempotency token LAST, so the token always
	// wins a key collision and stays RA-controlled. It does NOT return a run id — GitHub
	// creates the run asynchronously; the RA resolves it via listRunsByName.
	dispatch(ctx context.Context, tgt ghTarget, idempotencyToken, runName string, dispatchInputs map[string]string) error
	// getRun fetches one run by id in the targeted repo. A missing run surfaces as
	// *fwra.Error NotFound.
	getRun(ctx context.Context, tgt ghTarget, runID int64) (ghRun, error)
	// cancelRun requests cancellation of runID in the targeted repo. Cancelling an
	// already-terminal / absent run is success (the seam maps GitHub's 409/404 to nil).
	cancelRun(ctx context.Context, tgt ghTarget, runID int64) error
}

// ---------------------------------------------------------------------------
// The GitHub-Actions-backed ResourceAccess implementation
// ---------------------------------------------------------------------------

// access is the concrete, GitHub-Actions-backed implementation of the
// AgenticJobAccess port (agenticJobAccess.md §6). It is
// UNEXPORTED — the package's only public surface is the generated
// AgenticJobAccess interface + models + the generated
// NewGitHubActionsAgenticJobAccess constructor (plus the value-type
// behaviour free functions). It derives a
// deterministic dedup token + run name from the caller-supplied idempotencyKey,
// converges concurrent submits on the lowest-run-id canonical run, and maps a run's
// status+conclusion back to an infrastructure-neutral PipelineObservation.
//
// The struct imports NO Temporal (layer rule, agenticJobAccess.md §2):
// the idempotencyKey arrives as an ordinary parameter, never read from ambient
// context. All GitHub-Actions coupling is confined to the ghActionsClient seam.
//
// AUTH (§6 Auth model, as reworked for GitHub Actions): the contract surface is
// agnostic to the auth model and the frozen 3-op surface carries NO credential
// parameter (the Argo model acquired a k8s ServiceAccount token INTERNALLY). The
// GitHub-Actions analog preserves that exactly: the installation-token-minting
// credential (the App identity) is supplied at CONSTRUCTION to NewActionsClient and
// the concrete seam mints/refreshes the installation token INTERNALLY (via the
// satellite AppClient). The RA never threads a credential through its surface and
// never calls a sibling RA (NoSideways) — the auth maps cleanly without changing
// the frozen surface. See implementation/log/C-CP-R.md §auth.
type access struct {
	client ghActionsClient
	// resolveAttempts / resolveDelay bound the post-dispatch run-resolution poll
	// (GitHub creates the run asynchronously after a 204 dispatch). Defaults applied
	// in New when zero; tests may inject a faster clock-free fake whose dispatch
	// creates the run synchronously (one attempt suffices).
	resolveAttempts int
	resolveDelay    time.Duration
}

// compile-time proof the concrete impl satisfies the port.
var _ AgenticJobAccess = (*access)(nil)

const (
	defaultResolveAttempts = 10
	defaultResolveDelay    = 500 * time.Millisecond
)

// liveOrSucceeded keeps the runs that are not terminally failed — still queued /
// in_progress, or completed with conclusion "success". Terminally failed/cancelled
// runs are dropped so a dead prior attempt cannot block re-dispatch under the
// deterministic per-activity dedup token.
func liveOrSucceeded(runs []ghRun) []ghRun {
	var out []ghRun
	for _, r := range runs {
		if r.status != "completed" || r.conclusion == "success" {
			out = append(out, r)
		}
	}
	return out
}

// newAccess builds an access over the supplied GitHub-Actions client seam. It is
// the hand-written core both the generated NewGitHubActionsAgenticJobAccess
// constructor (via newGitHubActionsAgenticJobAccess, which wires the
// concrete ghActionsRESTClient seam over the App identity) and the in-package tests
// (which pass a fake ghActionsClient) build through. Returns the concrete *access so
// the in-package tests can tune resolveAttempts/resolveDelay; the public path returns
// the AgenticJobAccess interface.
func newAccess(client ghActionsClient) (*access, error) {
	if client == nil {
		return nil, fwra.New(fwra.ContractMisuse, "agenticjob.NewGitHubActionsAgenticJobAccess: nil actions client")
	}
	return &access{
		client:          client,
		resolveAttempts: defaultResolveAttempts,
		resolveDelay:    defaultResolveDelay,
	}, nil
}

// runNamePrefix is the deterministic run-name prefix the dispatched aiarch workflow
// stamps from the idempotency-token input. It MUST equal the satellite's
// RunNamePrefix; the concrete seam bridges them. Kept here as a package constant so
// the RA + its fake share it without a satellite import.
const runNamePrefix = "aiarch-cp-"

// SubmitAgenticJob converges the caller-supplied idempotencyKey on a
// single canonical GitHub Actions run and returns its handle (non-blocking on
// completion). Re-submitting the same key returns the SAME handle without launching
// a second effective run (agenticJobAccess.md §2.1). The convergence
// mechanism is documented in the file header (probe → dispatch → resolve → select
// lowest-id canonical → cancel siblings).
//
// NOTE the spec's Steps/Edges/Toolchains/Commands are NOT translated into a
// manifest here (unlike the Argo path): on GitHub Actions the construction recipe
// lives in the user's repo as the dispatched aiarch workflow FILE; this RA triggers
// that workflow for the activity. The spec's ProjectID/ActivityID/WorkspaceRef ride
// as the idempotency identity (the key the Manager derives) + the workflow's own
// checkout. A non-empty, well-formed spec is still required (a malformed spec is a
// caller pre-condition violation → ContractMisuse), preserving the contract's §2.1
// pre-conditions.
func (a *access) SubmitAgenticJob(rc fwra.Context, spec PipelineSpec) (PipelineHandle, error) {
	// The cross-cutting ctx + idempotencyKey now ride the ResourceAccess call Context
	// (fwra.Context embeds context.Context and carries the caller-supplied
	// IdempotencyKey); the package still never reads Temporal — the key is an ordinary
	// value carried on rc, exactly as before. This keeps the component Temporal-free.
	ctx := rc.Context
	idempotencyKey := rc.IdempotencyKey
	if idempotencyKey.IsZero() {
		return "", fwra.New(fwra.ContractMisuse, "SubmitAgenticJob: empty idempotencyKey")
	}
	if err := validateSpec(spec); err != nil {
		return "", err
	}
	token := dedupToken(idempotencyKey)
	runName := runNamePrefix + token

	// The OPTIONAL per-call repo + workflow-file override (the additive
	// per-project-design-dispatch field). Zero ⇒ the seam's configured default (the
	// construction repo + aiarch-construct.yml) — the existing UC3 caller is unchanged.
	// A non-zero target routes dispatch/observe/cancel at the per-project repo and is
	// ENCODED into the returned handle so a later Observe/Cancel re-addresses it.
	tgt := ghTarget{owner: spec.TargetRepo.Owner, repo: spec.TargetRepo.Name, workflowFile: spec.WorkflowFile}

	// 1. PROBE — converge on an already-launched run for this key without dispatching.
	//    Terminally-FAILED/cancelled prior attempts are ignored: the dedup token is
	//    deterministic per activity, so a dead run would otherwise pin this activity to
	//    its failure forever (the probe would converge on the failure and never
	//    re-dispatch). Only a live (queued/in_progress) or succeeded run counts as
	//    "already dispatched, don't duplicate"; a failed one allows a fresh dispatch.
	existing, err := a.client.listRunsByName(ctx, tgt, runName)
	if err != nil {
		return "", err
	}
	if live := liveOrSucceeded(existing); len(live) > 0 {
		return a.converge(ctx, tgt, live)
	}

	// 2. DISPATCH — no run yet for this key. The spec's optional DispatchInputs ride
	//    into the runtime's input map; the RA-controlled idempotency token is merged
	//    in LAST by the seam, so it wins any collision (stays RA-controlled).
	if err := a.client.dispatch(ctx, tgt, token, runName, spec.DispatchInputs); err != nil {
		return "", err
	}

	// 3. RESOLVE — GitHub creates the run asynchronously after a 204; poll (bounded)
	//    until the run carrying our run-name appears.
	runs, err := a.resolveAfterDispatch(ctx, tgt, runName)
	if err != nil {
		return "", err
	}
	if len(runs) == 0 {
		// Dispatched but the run never surfaced within the resolve window — transient
		// (GitHub may still be creating it); the Manager retries the whole submit,
		// which is idempotent (the probe will then find it).
		return "", fwra.New(fwra.Transient, "SubmitAgenticJob: dispatched run did not surface within resolve window")
	}

	// 4 + 5. SELECT canonical + RECONCILE siblings. Prefer the live/succeeded runs so a
	//    stale FAILED run sharing this deterministic run-name (a dead prior attempt) is
	//    not picked as canonical (lowest id) over the run we just dispatched. If every
	//    run is terminally failed, fall back to the full set (don't lose the handle).
	candidates := liveOrSucceeded(runs)
	if len(candidates) == 0 {
		candidates = runs
	}
	return a.converge(ctx, tgt, candidates)
}

// converge selects the canonical run (lowest id — the deterministic total order all
// racers compute identically) and cancels every non-canonical sibling carrying the
// same dedup name, then returns the canonical handle. Sibling cancellation is
// idempotent, so the reconcile is safe under the concurrent-double-dispatch race and
// under retry.
func (a *access) converge(ctx context.Context, tgt ghTarget, runs []ghRun) (PipelineHandle, error) {
	canonical := lowestID(runs)
	for _, r := range runs {
		if r.id == canonical.id {
			continue
		}
		// Best-effort, idempotent sibling cancel. A transient failure here does NOT
		// fail the submit: the canonical handle is already determined and stable; the
		// orphan sibling, if it lingers, produces no committed side effect the Manager
		// holds (the Manager only ever carries the canonical handle). We still surface
		// a hard (non-transient) error so a genuine auth/contract fault is visible.
		if err := a.client.cancelRun(ctx, tgt, r.id); err != nil && !isTransient(err) {
			return "", err
		}
	}
	return a.handleFor(tgt, canonical.id), nil
}

// resolveAfterDispatch polls listRunsByName until the dispatched run appears or the
// bounded attempt budget is exhausted (GitHub's dispatch→run-creation is eventually
// consistent). The fake's dispatch creates the run synchronously, so one attempt
// resolves it in tests; production uses the default budget.
func (a *access) resolveAfterDispatch(ctx context.Context, tgt ghTarget, runName string) ([]ghRun, error) {
	attempts := a.resolveAttempts
	if attempts <= 0 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		runs, err := a.client.listRunsByName(ctx, tgt, runName)
		if err != nil {
			return nil, err
		}
		if len(runs) > 0 {
			return runs, nil
		}
		if i == attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fwra.Wrap(fwra.Transient, ctx.Err(), "resolveAfterDispatch: context cancelled")
		case <-time.After(a.resolveDelay):
		}
	}
	return nil, nil
}

// ObserveAgenticJob reads the canonical run's status+conclusion and maps
// it to an infrastructure-neutral PipelineObservation
// (agenticJobAccess.md §2.2). Pure read; no side effects. An unknown /
// GC'd handle surfaces as fwra.NotFound.
func (a *access) ObserveAgenticJob(rc fwra.Context, handle PipelineHandle) (PipelineObservation, error) {
	ctx := rc.Context
	runID, tgt, err := a.runIDFromHandle(handle)
	if err != nil {
		return PipelineObservation{}, err
	}
	run, err := a.client.getRun(ctx, tgt, runID)
	if err != nil {
		return PipelineObservation{}, err
	}
	return observationFrom(handle, run), nil
}

// CancelAgenticJob requests cancellation of the canonical run. Cancelling
// an already-terminal / already-cancelled / unknown run is a no-op SUCCESS — the
// desired post-condition ("no further steps will start") already holds, which makes
// cancel safe to retry against the operator-pause race
// (agenticJobAccess.md §2.3). The seam maps GitHub's 409/404 to success.
func (a *access) CancelAgenticJob(rc fwra.Context, handle PipelineHandle) error {
	ctx := rc.Context
	runID, tgt, err := a.runIDFromHandle(handle)
	if err != nil {
		// A malformed handle is a caller pre-condition violation, not a cancel no-op.
		return err
	}
	if err := a.client.cancelRun(ctx, tgt, runID); err != nil {
		if isNotFound(err) {
			return nil // already gone == cancelled (idempotent-on-intent success)
		}
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Deterministic dedup token + handle
// ---------------------------------------------------------------------------

// dedupToken derives a deterministic, run-name-safe token from the caller-supplied
// idempotencyKey (the GitHub-Actions analog of the deterministic Argo Workflow
// name). The key may contain any characters (the Manager builds it from
// workflowId:activityId), so we hash it to a fixed-length lowercase-hex suffix —
// safe inside a run name and length-bounded. Same key ⇒ same token ⇒ same run name
// ⇒ the probe/resolve converges.
func dedupToken(key fwra.IdempotencyKey) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16]) // 32 hex chars
}

// handleSep separates the run id from the OPTIONAL encoded per-project target inside
// the opaque handle. When a per-call TargetRepo/WorkflowFile override is in play the
// handle is "run/<id>@<owner>/<repo>/<workflowFile>" so a later Observe/Cancel
// re-addresses the SAME per-project repo + workflow; the legacy default (no override)
// stays "run/<id>" so existing UC3 handles round-trip byte-identically.
const handleSep = "@"

// handleFor packs the canonical run id (and the OPTIONAL per-project target) into the
// opaque handle. When tgt is zero (the construction-repo default) the handle is the
// legacy "run/<id>" — the owner/repo are implicit to this Access (configured once).
// When tgt is non-zero (a per-project DESIGN dispatch) the target is APPENDED so the
// stateless Observe/Cancel can re-address the per-project repo from the handle alone.
// Callers never parse the handle (they compare by value); only this RA reads it back.
func (a *access) handleFor(tgt ghTarget, runID int64) PipelineHandle {
	base := "run/" + strconv.FormatInt(runID, 10)
	if tgt.isZero() {
		return PipelineHandle(base)
	}
	return PipelineHandle(base + handleSep + tgt.owner + "/" + tgt.repo + "/" + tgt.workflowFile)
}

// runIDFromHandle unpacks the run id AND the OPTIONAL per-project target from an
// opaque handle. A zero/malformed handle is a caller pre-condition violation →
// fwra.ContractMisuse. A handle with no "@<target>" segment returns a ZERO ghTarget
// (the construction-repo default — the seam substitutes its configured repo).
func (a *access) runIDFromHandle(handle PipelineHandle) (int64, ghTarget, error) {
	if PipelineHandleIsZero(handle) {
		return 0, ghTarget{}, fwra.New(fwra.ContractMisuse, "agenticjob: zero PipelineHandle")
	}
	runPart, targetPart, hasTarget := strings.Cut(string(handle), handleSep)
	kind, rest, ok := strings.Cut(runPart, "/")
	if !ok || kind != "run" || rest == "" {
		return 0, ghTarget{}, fwra.New(fwra.ContractMisuse, "agenticjob: malformed PipelineHandle")
	}
	id, perr := strconv.ParseInt(rest, 10, 64)
	if perr != nil {
		return 0, ghTarget{}, fwra.New(fwra.ContractMisuse, "agenticjob: malformed PipelineHandle run id")
	}
	if !hasTarget {
		return id, ghTarget{}, nil
	}
	// "<owner>/<repo>/<workflowFile>" — split into exactly three non-empty parts.
	owner, restTarget, ok1 := strings.Cut(targetPart, "/")
	repo, workflowFile, ok2 := strings.Cut(restTarget, "/")
	if !ok1 || !ok2 || owner == "" || repo == "" || workflowFile == "" {
		return 0, ghTarget{}, fwra.New(fwra.ContractMisuse, "agenticjob: malformed PipelineHandle target")
	}
	return id, ghTarget{owner: owner, repo: repo, workflowFile: workflowFile}, nil
}

// lowestID returns the run with the smallest id — the deterministic canonical
// selector. runs is non-empty by caller contract.
func lowestID(runs []ghRun) ghRun {
	out := runs[0]
	for _, r := range runs[1:] {
		if r.id < out.id {
			out = r
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Spec validation (caller pre-condition — §2.1)
// ---------------------------------------------------------------------------

// validateSpec enforces the contract's §2.1 pre-condition that the spec is
// well-formed (non-empty step graph, unique/non-empty step names, no dangling
// edge). A violation is a caller pre-condition violation → fwra.ContractMisuse. The
// GitHub-Actions path does not translate the steps to a manifest (the recipe lives
// in the user's workflow file), but it still validates the spec so a malformed
// submission is rejected deterministically, exactly as the Argo path did.
func validateSpec(spec PipelineSpec) error {
	if len(spec.Steps) == 0 {
		return fwra.New(fwra.ContractMisuse, "PipelineSpec has no steps")
	}
	seen := make(map[string]struct{}, len(spec.Steps))
	for _, st := range spec.Steps {
		if strings.TrimSpace(st.Name) == "" {
			return fwra.New(fwra.ContractMisuse, "PipelineSpec: step with empty name")
		}
		if _, dup := seen[st.Name]; dup {
			return fwra.New(fwra.ContractMisuse, "PipelineSpec: duplicate step name "+st.Name)
		}
		seen[st.Name] = struct{}{}
	}
	for _, e := range spec.Edges {
		if _, ok := seen[e.From]; !ok {
			return fwra.New(fwra.ContractMisuse, "PipelineSpec: edge From names unknown step "+e.From)
		}
		if _, ok := seen[e.To]; !ok {
			return fwra.New(fwra.ContractMisuse, "PipelineSpec: edge To names unknown step "+e.To)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// GitHub Actions run → PipelineObservation mapping (caller-opaque)
// ---------------------------------------------------------------------------

// observationFrom maps a GitHub Actions run's status+conclusion to an
// infrastructure-neutral PipelineObservation (agenticJobAccess.md §6).
// GitHub's run-level model has no per-step breakdown on the run object (jobs are a
// separate fetch the contract does not need — Non-goal #4 keeps observe a single
// cohesive read), so Steps is left empty and the observation carries phase + (on
// failure) a neutral diagnostic, which is exactly the Manager's intervention input.
func observationFrom(handle PipelineHandle, run ghRun) PipelineObservation {
	obs := PipelineObservation{
		Handle: handle,
		Phase:  mapPhase(run.status, run.conclusion),
	}
	if obs.Phase == PhaseFailed {
		obs.Diagnostic = neutralDiagnostic(run.conclusion)
	}
	// Surface the run's URL on EVERY observation the realisation resolved it for:
	// while the run is Pending/Running it is the caller's live "view the run"
	// deep-link (QA F-GTD-6 — the generating view's link to the in-flight CI run);
	// on a terminal non-success it is the operator's "why" pointer (QA F15 gap 2b)
	// the Manager threads onto the failed card. Empty when the realisation could
	// not resolve it.
	obs.RunURL = run.htmlURL
	return obs
}

// mapPhase maps GitHub's (status, conclusion) pair to the infrastructure-neutral
// PipelinePhase. status ∈ {queued/requested/waiting/pending, in_progress,
// completed}; conclusion (on completed) ∈ {success, failure, cancelled, skipped,
// timed_out, …}. A cancelled run is the contract's terminal PhaseCancelled; any
// non-success terminal conclusion is PhaseFailed.
func mapPhase(status, conclusion string) PipelinePhase {
	switch status {
	case "completed":
		switch conclusion {
		case "success":
			return PhaseSucceeded
		case "cancelled":
			return PhaseCancelled
		default:
			// failure, timed_out, startup_failure, action_required, neutral, skipped,
			// or an empty/unknown conclusion on a completed run → Failed (terminal).
			return PhaseFailed
		}
	case "in_progress":
		return PhaseRunning
	case "queued", "requested", "waiting", "pending", "":
		return PhasePending
	default:
		return PhasePending
	}
}

// neutralDiagnostic builds an infrastructure-neutral failure summary for the
// Manager's intervention decision (agenticJobAccess.md §2.2 / Non-goal #4
// — a SUMMARY, never a log firehose). It names the terminal outcome with no
// GitHub/Actions lexeme (the conclusion words success/failure/timed_out etc. are
// generic CI vocabulary, not GitHub-proprietary).
func neutralDiagnostic(conclusion string) string {
	switch conclusion {
	case "failure", "":
		return "construction pipeline failed"
	case "timed_out":
		return "construction pipeline timed out"
	case "startup_failure":
		return "construction pipeline failed to start"
	case "action_required":
		return "construction pipeline requires manual action"
	default:
		return "construction pipeline did not succeed: " + conclusion
	}
}

// ---------------------------------------------------------------------------
// Error helpers
// ---------------------------------------------------------------------------

// isNotFound reports whether err is (or wraps) an *fwra.Error of kind fwra.NotFound.
func isNotFound(err error) bool { return fwraKindIs(err, fwra.NotFound) }

// isTransient reports whether err is (or wraps) an *fwra.Error of kind
// fwra.Transient.
func isTransient(err error) bool { return fwraKindIs(err, fwra.Transient) }

// fwraKindIs reports whether err is (or wraps) an *fwra.Error of the given kind.
func fwraKindIs(err error, kind fwra.Kind) bool {
	var fe *fwra.Error
	if errors.As(err, &fe) {
		return fe.Kind == kind
	}
	return false
}

// actions_http_client.go is the concrete ghActionsClient — the ONLY place this RA
// speaks to GitHub Actions, by delegating to the github satellite's AppClient
// (framework-go-infrastructure-github). It is the C-CP-R analog of the former
// argo_http_client.go: the seam realisation that holds the infrastructure
// connection + auth and confines every GitHub-Actions wire detail.
//
// AUTH (the reworked §6 Auth model — internal, surface-preserving): this client
// holds the GitHub App identity (App id + RSA private key, via the satellite
// AppClient) and the target installation. It mints/refreshes the short-lived
// INSTALLATION TOKEN INTERNALLY (App-JWT → MintInstallationToken) and presents it
// on every Actions call. The token is NEVER threaded through the RA's contract
// surface and the RA NEVER calls a sibling RA to obtain it (NoSideways). This is the
// exact discipline the Argo path used (a k8s ServiceAccount token acquired inside
// the package) — re-expressed for GitHub. A short token cache avoids minting on
// every call; an expired/rejected token is re-minted on the next call and surfaces
// as fwra.Auth if the App lacks permission.

// appClient is the satellite surface this seam depends on — declared as an
// interface so the seam realisation is unit-testable against a satellite fake if
// ever needed, and so the dependency is explicit. The satellite *AppClient
// satisfies it.
type appClient interface {
	FindInstallation(ctx context.Context, account string) (int64, error)
	MintInstallationToken(ctx context.Context, installationID int64) (string, time.Time, error)
	DispatchWorkflow(ctx context.Context, owner, repo, workflowFile, ref string, inputs map[string]string, instToken string) error
	ListRunsByName(ctx context.Context, owner, repo, workflowFile, runName, instToken string) ([]fwgithub.WorkflowRun, error)
	GetRun(ctx context.Context, owner, repo string, runID int64, instToken string) (fwgithub.WorkflowRun, error)
	CancelRun(ctx context.Context, owner, repo string, runID int64, instToken string) error
}

// ghActionsRESTClient is the concrete ghActionsClient over the github satellite.
type ghActionsRESTClient struct {
	app          appClient
	owner        string
	repo         string
	workflowFile string
	ref          string

	mu             sync.Mutex
	installationID int64
	token          string
	tokenExpiry    time.Time
}

var _ ghActionsClient = (*ghActionsRESTClient)(nil)

// tokenRefreshSkew re-mints the installation token a little before its hard expiry.
const tokenRefreshSkew = 60 * time.Second

// newGitHubActionsAgenticJobAccess is the hand-written, unexported builder
// behind the generated NewGitHubActionsAgenticJobAccess constructor
// (option-1 delegated DI). It wires the token-caching ghActionsRESTClient seam over
// the framework *fwgithub.AppClient + the repo/workflow config, then the access impl,
// returning the AgenticJobAccess interface so the concrete impl + its seam
// stay unexported. The composition root (cmd/server/main.go) builds the App client via
// fwgithub.NewAppClient and passes it here.
func newGitHubActionsAgenticJobAccess(app *fwgithub.AppClient, owner, repo, workflowFile, ref string, installationID int64) (AgenticJobAccess, error) {
	seam, err := newActionsRESTClient(app, owner, repo, workflowFile, ref, installationID)
	if err != nil {
		return nil, err
	}
	return newAccess(seam)
}

// newActionsRESTClient builds the concrete GitHub-Actions seam from the App client +
// repo binding. It validates config eagerly (a missing field is a configuration error
// surfaced as fwra.ContractMisuse) but performs no network IO; the installation token
// is minted lazily on first use.
func newActionsRESTClient(app *fwgithub.AppClient, owner, repo, workflowFile, ref string, installationID int64) (*ghActionsRESTClient, error) {
	if app == nil {
		return nil, fwra.New(fwra.ContractMisuse, "agenticjob: nil github app client")
	}
	if strings.TrimSpace(owner) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "agenticjob: empty Owner")
	}
	if strings.TrimSpace(repo) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "agenticjob: empty Repo")
	}
	if strings.TrimSpace(workflowFile) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "agenticjob: empty WorkflowFile")
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "main"
	}
	return &ghActionsRESTClient{
		app:            app,
		owner:          owner,
		repo:           repo,
		workflowFile:   workflowFile,
		ref:            ref,
		installationID: installationID,
	}, nil
}

// installationToken returns a valid installation token, minting/refreshing it
// internally. Thread-safe; a cached token is reused until shortly before expiry.
func (c *ghActionsRESTClient) installationToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-tokenRefreshSkew)) {
		return c.token, nil
	}
	if c.installationID == 0 {
		id, err := c.app.FindInstallation(ctx, c.owner)
		if err != nil {
			return "", err
		}
		c.installationID = id
	}
	tok, exp, err := c.app.MintInstallationToken(ctx, c.installationID)
	if err != nil {
		return "", err
	}
	c.token = tok
	c.tokenExpiry = exp
	return tok, nil
}

// resolveTarget applies the per-call ghTarget over this client's configured default
// (the construction repo + aiarch-construct.yml). An EMPTY field on the target falls
// back to the configured value, so a ZERO ghTarget reproduces the legacy UC3
// behavior exactly (owner/repo/workflowFile all default), while a per-project DESIGN
// dispatch overrides all three. owner/repo address the per-project repo; workflowFile
// selects aiarch-design.yml. ref stays the client's configured ref (the design branch
// is carried as the dispatch target_branch input, not the workflow ref).
func (c *ghActionsRESTClient) resolveTarget(tgt ghTarget) (owner, repo, workflowFile string) {
	owner, repo, workflowFile = c.owner, c.repo, c.workflowFile
	if tgt.owner != "" {
		owner = tgt.owner
	}
	if tgt.repo != "" {
		repo = tgt.repo
	}
	if tgt.workflowFile != "" {
		workflowFile = tgt.workflowFile
	}
	return owner, repo, workflowFile
}

func (c *ghActionsRESTClient) listRunsByName(ctx context.Context, tgt ghTarget, runName string) ([]ghRun, error) {
	tok, err := c.installationToken(ctx)
	if err != nil {
		return nil, err
	}
	owner, repo, workflowFile := c.resolveTarget(tgt)
	runs, err := c.app.ListRunsByName(ctx, owner, repo, workflowFile, runName, tok)
	if err != nil {
		return nil, err
	}
	out := make([]ghRun, 0, len(runs))
	for _, r := range runs {
		out = append(out, toGHRun(r))
	}
	return out, nil
}

func (c *ghActionsRESTClient) dispatch(ctx context.Context, tgt ghTarget, idempotencyToken, _ string, dispatchInputs map[string]string) error {
	tok, err := c.installationToken(ctx)
	if err != nil {
		return err
	}
	// Merge the caller's optional extra inputs FIRST, then stamp the RA-controlled
	// idempotency token LAST so it wins any key collision (the load-bearing dedup /
	// run-name anchor stays RA-controlled — agenticJobAccess.md §0d.6).
	inputs := make(map[string]string, len(dispatchInputs)+1)
	maps.Copy(inputs, dispatchInputs)
	inputs[fwgithub.DispatchInputKeyIdempotency] = idempotencyToken
	owner, repo, workflowFile := c.resolveTarget(tgt)
	return c.app.DispatchWorkflow(ctx, owner, repo, workflowFile, c.ref, inputs, tok)
}

func (c *ghActionsRESTClient) getRun(ctx context.Context, tgt ghTarget, runID int64) (ghRun, error) {
	tok, err := c.installationToken(ctx)
	if err != nil {
		return ghRun{}, err
	}
	owner, repo, _ := c.resolveTarget(tgt)
	run, err := c.app.GetRun(ctx, owner, repo, runID, tok)
	if err != nil {
		return ghRun{}, err
	}
	gr := toGHRun(run)
	// Build the run's browser URL here — the concrete GitHub realisation is the only
	// place the github.com lexeme lives (the satellite's WorkflowRun does not carry it).
	gr.htmlURL = runHTMLURL(owner, repo, run.ID)
	return gr, nil
}

// runHTMLURL builds a GitHub Actions run's browser URL from its owner/repo/run id. The
// github.com host lexeme is confined to this concrete realisation (the seam/core stay
// host-agnostic). Empty when owner or repo is unknown.
func runHTMLURL(owner, repo string, runID int64) string {
	if owner == "" || repo == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/actions/runs/%d", owner, repo, runID)
}

func (c *ghActionsRESTClient) cancelRun(ctx context.Context, tgt ghTarget, runID int64) error {
	tok, err := c.installationToken(ctx)
	if err != nil {
		return err
	}
	owner, repo, _ := c.resolveTarget(tgt)
	return c.app.CancelRun(ctx, owner, repo, runID, tok)
}

// toGHRun bridges the satellite's WorkflowRun to the seam's package-internal ghRun.
func toGHRun(r fwgithub.WorkflowRun) ghRun {
	return ghRun{
		id:         r.ID,
		name:       r.Name,
		status:     string(r.Status),
		conclusion: string(r.Conclusion),
	}
}

// behavior.go carries the FREE-FUNCTION behaviour of the named-scalar value types
// in this component's contract — the established "behavioral value type →
// generated scalar + free functions" pattern (same as artifactAccess's OutputPath
// and the handoff enums). PipelineHandle is generated as a $def named scalar
// (`type PipelineHandle string`, contract.gen.go); its methods would not survive
// codegen, so they live here as free functions the impl + callers call. The
// opaque token the impl packs ("run/<id>" / "run/<id>@owner/repo/wf") IS the
// string value, so the behaviour is a thin, parse-free pass over that value.

// PipelineHandleString returns the canonical printable form of a handle (for logs,
// audit events, and persistence). It is the round-trip inverse of
// ParsePipelineHandle.
func PipelineHandleString(h PipelineHandle) string { return string(h) }

// ParsePipelineHandle reconstructs a PipelineHandle from the exact string form a
// prior Submit/Observe returned (the round-trip inverse of PipelineHandleString).
// A caller that PERSISTS a handle as a plain string (a Manager recording a pipeline
// reference in head-state, or a Temporal Manager serialising the handle across an
// Activity boundary) re-materialises the value-type handle for a later
// Observe/Cancel. It is a pure value reconstruction — no validation here; an
// unaddressable / malformed handle is rejected by the verb that consumes it
// (Observe/Cancel map a bad handle to ContractMisuse/NotFound). Additive: it adds
// no new business op and leaves the three-verb port surface unchanged. (Replaces
// the former HandleFromString method.)
func ParsePipelineHandle(s string) PipelineHandle { return PipelineHandle(s) }

// PipelineHandleEqual reports value equality of two handles.
func PipelineHandleEqual(a, b PipelineHandle) bool { return a == b }

// PipelineHandleIsZero reports whether the handle is the zero value (no pipeline
// addressed).
func PipelineHandleIsZero(h PipelineHandle) bool { return h == "" }

// ---------------------------------------------------------------------------
// PipelinePhase behaviour (free functions over the generated enum)
// ---------------------------------------------------------------------------

var phaseNames = map[PipelinePhase]string{
	PhasePending: "Pending", PhaseRunning: "Running", PhaseSucceeded: "Succeeded",
	PhaseFailed: "Failed", PhaseCancelled: "Cancelled",
}

// PipelinePhaseString returns the stable name (logs, audit). A free function
// because the generated contract type carries no methods.
func PipelinePhaseString(p PipelinePhase) string {
	if n, ok := phaseNames[p]; ok {
		return n
	}
	return "Pending"
}

// PipelinePhaseIsTerminal reports whether the phase is one a running pipeline can no
// longer leave (Succeeded / Failed / Cancelled). Cancelling or re-observing a
// terminal pipeline is stable.
func PipelinePhaseIsTerminal(p PipelinePhase) bool {
	switch p {
	case PhaseSucceeded, PhaseFailed, PhaseCancelled:
		return true
	case PhasePending, PhaseRunning: // not yet terminal — pipeline still in flight
		return false
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// StepOutcome behaviour (free function over the generated enum)
// ---------------------------------------------------------------------------

var stepOutcomeNames = map[StepOutcome]string{
	StepPending: "Pending", StepRunning: "Running", StepSucceeded: "Succeeded",
	StepFailed: "Failed", StepSkipped: "Skipped",
}

// StepOutcomeString returns the stable name (logs, audit). A free function
// because the generated contract type carries no methods.
func StepOutcomeString(o StepOutcome) string {
	if n, ok := stepOutcomeNames[o]; ok {
		return n
	}
	return "Pending"
}

// ---------------------------------------------------------------------------
// RepoTarget behaviour (free function over the generated struct)
// ---------------------------------------------------------------------------

// RepoTargetIsZero reports whether the target addresses no repo (the
// fall-back-to-default case).
func RepoTargetIsZero(t RepoTarget) bool { return t.Owner == "" && t.Name == "" }

// Package agenticjob is the agenticJobAccess component of the
// aiarch server's ResourceAccess layer — the INFRASTRUCTURE-OPAQUE port over the
// construction-task face of WorkflowRuntime volatility
// (agenticJobAccess.md). It is the only component permitted to call the
// constructionPipelineRuntime Resource (architecture.dsl line 284).
//
// THE LOAD-BEARING LAYER RULE (agenticJobAccess.md §1, §3;
// [[the-method-layers]] "Temporal mapping"): this RA fronts the USER'S GitHub
// Actions (the 2026-06-09 pivot; the C-CP-R rework swapped the runtime from Argo
// Workflows on Kubernetes to GitHub Actions), yet its PUBLIC surface carries ZERO
// GitHub-Actions lexemes (workflow_dispatch, workflow_run, run id, ref, owner/repo)
// and imports NO Temporal. Three atomic, infrastructure-opaque business verbs —
// submit / observe / cancel one construction pipeline at a time. The GitHub-Actions
// vocabulary is confined to actions.go (the seam + status mapping + idempotency
// convergence) and actions_http_client.go (the concrete seam over the github
// satellite behind it), and to the github satellite itself.
//
// Idempotency on the write verb (SubmitAgenticJob) is carried by a
// CALLER-SUPPLIED idempotencyKey (the deterministic continuity token), never read
// from ambient Temporal context — the same move artifactAccess /
// durableExecutionAccess use. GitHub's workflow_dispatch has no duplicate dedup, so
// the package derives a deterministic dedup token + run name from that key and
// CONVERGES concurrent/replayed submits on a single canonical run (lowest run id),
// cancelling non-canonical siblings — the GitHub-Actions analog of Argo's "reject
// duplicate name". Re-submitting the same key returns the SAME handle. Passing the
// key in — rather than reading the runtime's ambient id — is what keeps this
// component Temporal-free. See the actions.go file header for the convergence proof.
//
// The concrete GitHub-Actions-backed implementation lives in actions.go; its
// satellite-delegating seam (App-JWT → installation-token minted INTERNALLY, then
// the Actions REST calls) lives in actions_http_client.go and is the ONLY place
// this RA speaks GitHub Actions, never leaking a GitHub type back across the port.

// Error is the shared ResourceAccess error model (framework-go), re-exported as an
// alias so this component's contract reads in its own terms while every RA
// component shares one fixed enum. Construct with fwra.New / fwra.Wrap using the
// shared kinds. The contract's logical error vocabulary
// (agenticJobAccess.md §3 PipelineAccessError) maps onto the shared
// kinds as follows:
//
//   - ErrTransient       → fwra.Transient        (retryable: GitHub 429 / 5xx)
//   - ErrAuth            → fwra.Auth             (terminal: App-JWT / installation token / permission denied)
//   - ErrNotFound        → fwra.NotFound         (terminal: run unknown / GC'd; SUCCESS for cancel)
//   - ErrCapacity        → fwra.QuotaExhausted   (terminal: runtime capacity stall; escalate)
//   - ErrContractMisuse  → fwra.ContractMisuse   (terminal: malformed spec / bad dispatch request)
//   - ErrInfrastructure  → fwra.Infrastructure   (escalate: unclassifiable infra-internal error)
//
// The error KINDS are infrastructure-neutral and unchanged across the C-CP-R Argo→
// GitHub-Actions rework; only the underlying fault sources differ (GitHub REST
// status codes now drive the classification, via the satellite's ClassifyStatus).
// The contract's ErrCapacity (a HARD, non-retryable runtime-capacity stall the
// Manager escalates to interventionEngine, agenticJobAccess.md §6 OQ4)
// maps to fwra.QuotaExhausted, whose DefaultRetryable() is false — preserving the
// "non-retryable + escalate" classification the senior review confirmed.
type Error = fwra.Error

// variant.go holds the DRY-RUN variant stub for agenticJobAccess — the
// in-memory profile that backs the UC3 construction Worker when
// ARCHISTRATOR_CONSTRUCTION_DRYRUN=true, folded out of cmd/server (construction_dryrun.go)
// into the owning package. Every submit instantly "succeeds": Submit returns a
// deterministic handle keyed on the activity, Observe always reports Succeeded, Cancel is
// a no-op. No GitHub Actions run fires.
//
// What stays REAL is the construction Manager's Temporal orchestration — the
// self-cascading pump, the per-activity lifecycle, and the per-activity construction
// head-state writes. So "Begin construction" walks the committed network for real WITHOUT
// firing any GitHub Actions run. Local dogfood / demo profile only. Construction
// dispatches real work via the GH-Actions pipeline (agentic-everywhere); there is no
// server-side LLM worker seam.
//
// The REAL GitHub-Actions variant is the generated DI constructor
// NewGitHubActionsAgenticJobAccess (contract.gen.go); the composition root
// builds the shared *fwgithub.AppClient satellite and passes it in.

// NewDryRunAgenticJobAccess returns the in-memory dry-run pipeline stub.
func NewDryRunAgenticJobAccess() AgenticJobAccess {
	return dryRunPipeline{}
}

type dryRunPipeline struct{}

var _ AgenticJobAccess = dryRunPipeline{}

func (dryRunPipeline) SubmitAgenticJob(_ fwra.Context, spec PipelineSpec) (PipelineHandle, error) {
	return PipelineHandle("dryrun:" + string(spec.ActivityID)), nil
}

func (dryRunPipeline) ObserveAgenticJob(_ fwra.Context, handle PipelineHandle) (PipelineObservation, error) {
	return PipelineObservation{Handle: handle, Phase: PhaseSucceeded}, nil
}

func (dryRunPipeline) CancelAgenticJob(_ fwra.Context, _ PipelineHandle) error {
	return nil
}

// localexec.go is the LOCAL-EXECUTOR realisation of the AgenticJobAccess
// port (local-first-init-funnel Task 6, docs/superpowers/plans/2026-07-19-local-
// first-init-funnel.md) — the THIRD construction-dispatch arm alongside the
// GitHub-Actions-backed realisation (defined earlier in this file) and the
// in-memory dry-run stub
// (NewDryRunAgenticJobAccess). Selected by cmd/server/hooks.go's
// FinalizeAgenticJobAccess for a LOCAL-profile boot with NO GitHub App
// creds configured (orthogonal to the projectstate substrate profile, exactly as
// hooks.go documents for the existing local-WITH-creds arm): "dispatch" means spawn
// a headless `claude` subprocess directly on the developer's own machine, riding
// their own Claude Code subscription auth (ambient — no ANTHROPIC_API_KEY, mirrors
// framework-go-infrastructure-llm/claudecli.go's ClaudeCLIClient), instead of
// triggering a GitHub Actions workflow_dispatch.
//
// SAME THREE-VERB PORT, SAME PROMPT CONTRACT: this realisation satisfies the exact
// AgenticJobAccess surface the GitHub-Actions realisation does (Submit/
// Observe/Cancel) — the
// Manager (constructactivity.go) neither knows nor cares which arm it is talking
// to. The prompt handed to `claude` — "/<command> <component_id> <activity_id>" —
// is byte-for-byte the SAME thin slash-command contract aiarch-construct.yml's
// `prompt:` step passes claude-code-action, and it attaches the SAME construct-verb
// rig (cmd/aiarch-state-mcp) via --mcp-config, with the SAME AIARCH_* ambient-context
// env the workflow's "Write the aiarch-state MCP config" step stamps
// (cmd/aiarch-state-mcp/session.go's envProjectID/envJobMode/envComponentID/
// envActivityID/envTargetBranch/envStateRoot).
//
// THREE JOB SHAPES ON THE ONE SUBMIT SURFACE (the discriminator):
// SubmitAgenticJob serves THREE distinct dispatch shapes, discriminated off
// DispatchInputs (never off ActivityID, which is CONSTRUCT-ONLY):
//
//   - CONSTRUCT (the default) — a headless claude run on the activity/<id> branch.
//     Carries component_id + activity_id; prompt "/<command> <component> <activity>";
//     AIARCH_JOB_MODE=construct. Requires a non-empty ActivityID.
//   - DESIGN (submitDesignJob) — the local-executor counterpart of the seated
//     aiarch-design.yml draft job. Discriminated by a NON-EMPTY "job_mode" dispatch
//     input (every design job the Phase-1/Phase-2 design Managers send stamps it —
//     draft/critique/answer; a construct dispatch NEVER does). Carries NO ActivityID —
//     a design job works on a typed Method ARTIFACT slot (fixed by artifact_kind), not
//     a component+activity, so its parameters ride DispatchInputs — the SAME thing the
//     GitHub-Actions arm already does (it dispatches design jobs with an empty
//     ActivityID; only the step graph is validated). The prompt is exactly "/<command>"
//     (no args) and the AIARCH_* envelope is the EXACT set aiarch-design.yml stamps on
//     its MCP-config step — PROJECT_ID / ARTIFACT_KIND / JOB_MODE / TARGET_BRANCH /
//     STATE_ROOT — so a design job behaves identically on both rails. The worktree is on
//     target_branch (the design SESSION branch); on the LOCAL profile the branch-staging
//     rail is dormant, so the executor CREATES the session branch off main on first use
//     (the stand-in for the cloud's server-side beginSession/OpenBranch) and re-attaches
//     to its tip on a mid-session redraft/critique/answer — see designDispatchPlan +
//     addWorktree for the field-by-field mirror, the cloud-vs-local branch-staging
//     asymmetry, and why prior_state_ref is NOT in the ambient env.
//   - LOCAL MERGE (submitMergeJob) — discriminated by DispatchInputs["job"]="merge";
//     not a claude run at all (see below).
//
// WHY EVERY DISPATCH GETS A GIT WORKTREE (the worktree-per-activity rework,
// superseding the original fresh-clone-per-dispatch): SubmitAgenticJob
// runs `git worktree add` against the configured repo's LOCAL filesystem path (the
// constructor now REQUIRES a local file:// / plain-path repoURL — a worktree cannot
// span the network), creating (off main) or re-attaching to the deterministic
// "activity/<activityID>" branch — the SAME naming convention constructactivity.go's
// (unexported) activityBranchName derives, duplicated here because the two packages
// cannot share an unexported symbol; keep the format in sync if it ever changes —
// then runs claude with the worktree as cwd. The worktree lives in a throwaway temp
// dir (NEVER inside the user's checkout; the OS temp reaper is the cleanup backstop
// for anything a crash leaves behind), and commits made there advance the SHARED
// repo's refs DIRECTLY — there is NO push step anymore. Partial progress on a failed
// run is therefore durable by construction (every commit already lives in the shared
// repo). PhaseSucceeded requires a clean claude exit AND the activity branch ref
// having ADVANCED (rev-parse before/after) AND the worktree removed — a clean exit
// that committed nothing is a FAILURE, not a fake success. The worktree is removed
// (`git worktree remove --force`) on completion and cancel paths alike, and the
// constructor runs `git worktree prune` once at startup to clear stale metadata a
// crashed prior process left. This is a FOUNDER-ACCEPTED isolation tradeoff — speed
// (no full clone per dispatch) over clone isolation: the agent's git subprocesses
// now write into the shared repo's own .git (worktree metadata, objects, refs); see
// the SECURITY POSTURE doc block below. The dormant git-forward PR rail note still
// holds: RailEnabled needs sourceControlAccess, which requires the same GitHub creds
// this arm is selected for the ABSENCE of (constructactivity.go's gitEnabled) —
// nobody else creates the activity branch in this profile, so this realisation must.
//
// GIT-FORWARD MERGE (local-merge-and-policy Commit 1 — closes the former
// local-mode v1 gap): with the PR rail dormant, the Manager now finishes an
// activity by dispatching the MERGE JOB through this same frozen Submit surface
// (DispatchInputs["job"]=DispatchJobMerge — submitMergeJob below): a --no-ff
// merge of activity/<id> into the default branch + branch delete, performed in a
// throwaway clone and pushed (the projectstate GitStore's own write mechanism).
// The merge DECISION stays the Manager's (ReviewPolicy.EffectiveGate + the
// Task-7 risk floor gate the merge behind human approval); this realisation only
// executes it — the same decide/perform split the cloud PR rail has.
//
// NO FAKE SUCCESS STATES: every PipelinePhase this realisation reports is derived
// from an ACTUAL subprocess outcome (exit code, timeout, explicit cancel) or an
// actual observed git ref movement — there is no code path that reports Succeeded
// without a clean claude exit AND the activity branch ref having genuinely advanced
// in the shared repo (verified via rev-parse before/after) AND the worktree removed.

// defaultLocalRunTimeout bounds one claude invocation — the SAME 25-minute budget
// aiarch-construct.yml's job enforces, for the same documented reason (that
// workflow's timeout-minutes comment: "12 min was observed cutting off substantive
// components... mid-implement before commit+PR; 25 gives runway").
const defaultLocalRunTimeout = 25 * time.Minute

// localExecWaitDelay bounds how long the awaitCompletion goroutine waits for
// claude's stdout/stderr pipes to close AFTER Cancel (SIGTERM) fired — the SAME
// os/exec grandchild-pipe gotcha framework-go-infrastructure-llm/claudecli.go's
// claudeCLIWaitDelay documents (claude may itself spawn tool-call subprocesses that
// inherit the pipe). Larger than that provider's 2s because a construction run's
// own tool subprocesses (go build, go test, git) may need a moment to unwind.
const localExecWaitDelay = 5 * time.Second

// localHandlePrefix distinguishes a local-executor handle from actions.go's
// "run/<id>" GitHub-Actions handle shape — the two realisations are never mixed
// (a given AgenticJobAccess instance is exactly one arm), but keeping the
// shapes visually distinct aids debugging shared logs/audit trails.
const localHandlePrefix = "local:"

// localRunStatus is the in-memory run-status vocabulary the poll loop's Observe
// calls map subprocess/git outcomes into.
type localRunStatus int

const (
	localRunRunning localRunStatus = iota
	localRunSucceeded
	localRunFailed
	localRunCancelled
)

// localRun is one tracked construction dispatch: an in-memory process record (per
// the brief — "'runs' tracked as local process records (in-memory + state
// commits)"; the state commits are the construct-verb MCP tool calls claude itself
// makes through cmd/aiarch-state-mcp, not this struct). mu guards every field below
// status after construction.
type localRun struct {
	handle PipelineHandle

	mu         sync.Mutex
	status     localRunStatus
	diagnostic string
	cancel     context.CancelFunc // cancels the claude subprocess's bounded run context
}

// localExecAccess is the concrete local-executor AgenticJobAccess. It
// imports NO Temporal (layer rule, same as access — the idempotencyKey arrives as
// an ordinary rc.IdempotencyKey parameter).
type localExecAccess struct {
	repoURL     string // the configured repo URL (file:// or plain local path — see localRepoPath)
	repoPath    string // the shared repo's LOCAL filesystem path, derived from repoURL — the worktree host
	projectID   string // AIARCH_PROJECT_ID stamped on the state-mcp process (name-as-identity)
	stateMCPBin string // resolved absolute path to the compiled cmd/aiarch-state-mcp binary
	runTimeout  time.Duration

	// gitMu serializes THIS instance's own worktree-add/rev-parse/worktree-remove
	// git subprocesses against the shared repo — worktree add/remove mutate the
	// shared .git/worktrees metadata, so keeping this instance's own git calls
	// sequential avoids racing our own metadata writes. V1 assumes one local
	// developer driving one construction pump at a time (known limitation, not a
	// deadlock/corruption risk within that scope).
	gitMu sync.Mutex

	mu   sync.Mutex
	runs map[string]*localRun // keyed by dedupToken(idempotencyKey)
}

var _ AgenticJobAccess = (*localExecAccess)(nil)

// NewLocalExecAgenticJobAccess builds the local-executor realisation.
// repoURL must address a LOCAL repo — a file:// URL or a plain filesystem path
// (local mode always passes the same value the projectstate GitStore was
// configured with — cfg.ProjectStateGitRepoURL, a file:// path): the
// worktree-per-activity executor operates `git worktree` directly on that path,
// so a network URL is a configuration error (ContractMisuse). projectID is the
// AIARCH_PROJECT_ID value stamped on the state-mcp process (mirrors the
// workflow's github.event.repository.name; hooks.go derives it from the repo
// path's basename, name-as-identity); stateMCPBin is the resolved absolute path to
// the compiled cmd/aiarch-state-mcp binary (existence checked eagerly — a missing
// binary is a configuration error, not a per-dispatch surprise). runTimeout bounds
// one claude invocation (<=0 defaults to defaultLocalRunTimeout).
//
// Startup hygiene: runs `git worktree prune` once against the shared repo to
// clear STALE worktree registrations a crashed prior process left behind (the
// worktree dirs live under the OS temp dir, so the dirs themselves may already
// be gone while .git/worktrees metadata lingers and would block re-attaching
// the activity branch). Best-effort: a prune failure (e.g. the repo does not
// exist yet) is NOT a constructor error — the same fault surfaces per-dispatch
// with a proper diagnostic.
func NewLocalExecAgenticJobAccess(repoURL, projectID, stateMCPBin string, runTimeout time.Duration) (AgenticJobAccess, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return nil, fwra.New(fwra.ContractMisuse, "agenticjob.NewLocalExecAgenticJobAccess: empty repoURL")
	}
	repoPath, err := localRepoPath(repoURL)
	if err != nil {
		return nil, err
	}
	stateMCPBin = strings.TrimSpace(stateMCPBin)
	if stateMCPBin == "" {
		return nil, fwra.New(fwra.ContractMisuse, "agenticjob.NewLocalExecAgenticJobAccess: empty stateMCPBin")
	}
	if _, err := os.Stat(stateMCPBin); err != nil {
		return nil, fwra.Wrap(fwra.ContractMisuse, err, "agenticjob.NewLocalExecAgenticJobAccess: aiarch-state-mcp binary not found")
	}
	if runTimeout <= 0 {
		runTimeout = defaultLocalRunTimeout
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		projectID = "local"
	}
	_, _ = runGit(repoPath, "worktree", "prune") // best-effort startup hygiene, see doc comment
	return &localExecAccess{
		repoURL:     repoURL,
		repoPath:    repoPath,
		projectID:   projectID,
		stateMCPBin: stateMCPBin,
		runTimeout:  runTimeout,
		runs:        map[string]*localRun{},
	}, nil
}

// localRepoPath derives the shared repo's local filesystem path from the
// configured repoURL: a file:// URL is stripped to its path; a plain path is
// used as-is; anything with a non-file scheme (or scp-like host syntax) is a
// configuration error — a git worktree cannot span the network.
func localRepoPath(repoURL string) (string, error) {
	if p, ok := strings.CutPrefix(repoURL, "file://"); ok && p != "" {
		return p, nil
	}
	if strings.Contains(repoURL, "://") {
		return "", fwra.New(fwra.ContractMisuse, "agenticjob.NewLocalExecAgenticJobAccess: worktree executor requires a local file:// or plain-path repoURL, got "+repoURL)
	}
	return repoURL, nil
}

// localBranchName derives the SAME deterministic per-activity branch name
// constructactivity.go's activityBranchName computes ("activity/<id>") — see the
// file-header note on why this is duplicated rather than shared.
func localBranchName(activityID string) string { return "activity/" + activityID }

// SubmitAgenticJob converges the caller-supplied idempotencyKey on a
// single in-memory run record and returns its handle, spawning the claude
// subprocess ONLY on the first submit for a given key (idempotent convergence,
// mirroring actions.go's SubmitAgenticJob — same contract, different
// executor). A pre-spawn failure (git clone/checkout, missing dispatch inputs)
// returns an error and leaves NO run record, so a retry with the SAME key tries
// again cleanly; once the subprocess has started, Submit returns success and the
// eventual outcome is observable only via ObserveAgenticJob.
func (a *localExecAccess) SubmitAgenticJob(rc fwra.Context, spec PipelineSpec) (PipelineHandle, error) {
	if rc.IdempotencyKey.IsZero() {
		return "", fwra.New(fwra.ContractMisuse, "SubmitAgenticJob: empty idempotencyKey")
	}
	if err := validateSpec(spec); err != nil {
		return "", err
	}

	// DESIGN-JOB ARM (Phase-1 systemdesign + Phase-2 projectdesign; draft/critique/
	// answer). A design dispatch is discriminated by a NON-EMPTY "job_mode" dispatch
	// input: every design job the design Managers send stamps it (dispatchInputJobMode
	// in {systemdesign,projectdesign}manager.go → "draft"/"critique"/"answer"), while a
	// construct dispatch NEVER sets it (it carries component_id/activity_id instead) and
	// the local merge job is discriminated separately below by DispatchInputJobKey.
	// Design jobs carry NO ActivityID — their parameters ride DispatchInputs — so they
	// MUST branch BEFORE the construct/merge ActivityID requirement. This is exactly what
	// the GitHub-Actions arm already does (it dispatches design jobs with an empty
	// ActivityID; its Submit validates only the step graph, never ActivityID), so the two
	// rails agree on "ActivityID is construct-only".
	if jobMode := strings.TrimSpace(spec.DispatchInputs[dispatchInputJobModeKey]); jobMode != "" {
		return a.submitDesignJob(rc, spec, jobMode)
	}

	activityID := strings.TrimSpace(string(spec.ActivityID))
	if activityID == "" {
		return "", fwra.New(fwra.ContractMisuse, "SubmitAgenticJob: empty ActivityID")
	}
	// Policy-gated local merge job (local-merge-and-policy Commit 1): a dispatch
	// carrying DispatchInputs["job"]="merge" is NOT a claude run — it merges the
	// activity branch into the repo's default branch (and deletes the branch),
	// synchronously, recording a terminal run the Manager's existing observe poll
	// reads. The Manager sends this ONLY in the local (rail-dormant) profile,
	// AFTER the ReviewPolicy.EffectiveGate hold (if any) has cleared — the RA
	// stays policy-free (it executes; the Manager decides). A merge conflict is a
	// FAILED run with a diagnostic (the Manager's intervention path), never a
	// partial merge — the merge happens in a throwaway clone and nothing is
	// pushed unless it completed cleanly.
	if spec.DispatchInputs[DispatchInputJobKey] == DispatchJobMerge {
		return a.submitMergeJob(rc, activityID)
	}
	command := strings.TrimSpace(spec.DispatchInputs[dispatchInputCommandKey])
	if command == "" {
		return "", fwra.New(fwra.ContractMisuse, `SubmitAgenticJob: missing DispatchInputs["command"]`)
	}
	componentID := spec.DispatchInputs[dispatchInputComponentIDKey]

	return a.submitClaudeRun(rc, constructDispatchPlan(a.projectID, activityID, command, componentID))
}

// submitDesignJob is the DESIGN arm of SubmitAgenticJob — the local-executor
// counterpart of aiarch-design.yml's draft job. It validates the design dispatch inputs
// (command + target_branch are REQUIRED; a missing one is ContractMisuse, exactly as the
// construct arm rejects a missing command), builds the design dispatch plan, and hands
// off to the SAME convergence + spawn machinery the construct arm uses (submitClaudeRun).
// It deliberately does NOT require ActivityID — see the SubmitAgenticJob
// discriminator note and designDispatchPlan.
func (a *localExecAccess) submitDesignJob(rc fwra.Context, spec PipelineSpec, jobMode string) (PipelineHandle, error) {
	command := strings.TrimSpace(spec.DispatchInputs[dispatchInputCommandKey])
	if command == "" {
		return "", fwra.New(fwra.ContractMisuse, `SubmitAgenticJob: missing DispatchInputs["command"] for a design job`)
	}
	targetBranch := strings.TrimSpace(spec.DispatchInputs[dispatchInputTargetBranchKey])
	if targetBranch == "" {
		return "", fwra.New(fwra.ContractMisuse, `SubmitAgenticJob: missing DispatchInputs["target_branch"] for a design job`)
	}
	artifactKind := strings.TrimSpace(spec.DispatchInputs[dispatchInputArtifactKindKey])
	return a.submitClaudeRun(rc, designDispatchPlan(a.projectID, jobMode, command, targetBranch, artifactKind))
}

// submitClaudeRun converges the caller-supplied idempotencyKey on a single in-memory
// run record and spawns the claude subprocess via the shared dispatch core from a
// fully-built localDispatchPlan. It is the common tail of BOTH the construct arm and
// the design arm (submitDesignJob): the arms differ ONLY in the plan they build
// (branch + create-off-main policy + AIARCH_* rig + prompt); the convergence + spawn +
// pre-spawn-rollback is identical. A pre-spawn failure leaves NO run record, so a retry
// with the SAME key tries again cleanly; once the subprocess has started, Submit returns
// success and the eventual outcome is observable only via ObserveAgenticJob
// (mirroring actions.go's probe-then-dispatch convergence, different executor).
func (a *localExecAccess) submitClaudeRun(rc fwra.Context, plan localDispatchPlan) (PipelineHandle, error) {
	token := dedupToken(rc.IdempotencyKey)
	handle := PipelineHandle(localHandlePrefix + token)

	a.mu.Lock()
	if existing, ok := a.runs[token]; ok {
		a.mu.Unlock()
		return existing.handle, nil
	}
	run := &localRun{handle: handle, status: localRunRunning}
	a.runs[token] = run
	a.mu.Unlock()

	if err := a.dispatch(run, plan); err != nil {
		a.mu.Lock()
		delete(a.runs, token)
		a.mu.Unlock()
		return "", err
	}
	return handle, nil
}

// localDispatchPlan carries the per-ARM variance the shared dispatch core (dispatch,
// below) needs to spawn ONE headless claude run. The construct arm and the design arm
// share every mechanism — worktree add, --mcp-config + Tier-2 sandbox --settings
// authoring, subprocess spawn, awaitCompletion's ref-advanced success gate — and differ
// ONLY in these fields. Building the plan is the arm's whole job; running it is
// arm-agnostic.
type localDispatchPlan struct {
	// branch is the git branch the worktree checks out and claude commits onto: the
	// construct arm's activity/<id>, or the design arm's session branch (target_branch).
	// BOTH arms lean on addWorktree's re-attach-or-create-off-main behavior: on the local
	// (rail-dormant) profile the executor is the only git-owning party, so a missing branch
	// is created fresh off main on first use — see addWorktree for the cloud-vs-local
	// asymmetry.
	branch string
	// worktreeLabel names the branch class in the worktree-add error diagnostic
	// ("activity-branch" / "design session-branch").
	worktreeLabel string
	// rig is the AIARCH_* ambient-context envelope stamped on BOTH claude's own process
	// env (claudeSubprocessEnv) AND the attached aiarch-state MCP server
	// (writeStateMCPConfig), EXCLUDING AIARCH_STATE_ROOT — dispatch stamps that = the
	// worktree dir it creates (not known until dispatch runs).
	rig map[string]string
	// prompt is the exact claude -p prompt. Construct: "/<command> <component> <activity>"
	// (aiarch-construct.yml's prompt step). Design: "/<command>" with no args
	// (aiarch-design.yml's draft step).
	prompt string
}

// constructDispatchPlan builds the plan for a CONSTRUCTION dispatch: the activity branch
// (created off main if absent — nobody else opens it in the local profile), the construct
// AIARCH_* rig (JOB_MODE=construct + component/activity), and the "/<command> <component>
// <activity>" prompt — the exact shape aiarch-construct.yml's prompt step + MCP-config
// env emit.
func constructDispatchPlan(projectID, activityID, command, componentID string) localDispatchPlan {
	branch := localBranchName(activityID)
	return localDispatchPlan{
		branch:        branch,
		worktreeLabel: "activity-branch",
		rig: map[string]string{
			"AIARCH_PROJECT_ID":    projectID,
			"AIARCH_JOB_MODE":      "construct",
			"AIARCH_COMPONENT_ID":  componentID,
			"AIARCH_ACTIVITY_ID":   activityID,
			"AIARCH_TARGET_BRANCH": branch,
		},
		prompt: "/" + command + " " + componentID + " " + activityID,
	}
}

// designDispatchPlan builds the plan for a DESIGN dispatch, MIRRORING the seated
// aiarch-design.yml draft job field-for-field:
//
//   - Worktree on target_branch (the design SESSION branch). The cloud draft job checks
//     out prior_state_ref, then its "Refresh the session branch from main" step does
//     `git checkout -B target_branch origin/target_branch` — so the working tree the
//     agent actually drafts on is the session-branch tip. Worktree-on-target_branch is
//     the faithful local equivalent. CLOUD-vs-LOCAL BRANCH-STAGING ASYMMETRY: on the
//     cloud rail the session branch is created SERVER-SIDE before dispatch (systemdesign
//     beginSession → sourceControlAccess.OpenBranch). On the LOCAL profile that rail is
//     DORMANT (sourceControlAccess is nil — no branch/PR ops), so nothing stages it and
//     the executor creates it off main on FIRST use (addWorktree). A mid-session redraft/
//     critique/answer re-attaches to the existing branch's tip. See addWorktree.
//   - The AIARCH_* envelope is the EXACT set aiarch-design.yml's "Write the aiarch-state
//     MCP config" step stamps (and that cmd/aiarch-state-mcp/session.go reads):
//     AIARCH_PROJECT_ID, AIARCH_ARTIFACT_KIND, AIARCH_JOB_MODE, AIARCH_TARGET_BRANCH,
//     AIARCH_STATE_ROOT (the last stamped by dispatch = the worktree dir). NO
//     AIARCH_COMPONENT_ID / AIARCH_ACTIVITY_ID (those are construct-only).
//   - prior_state_ref is NOT in the envelope: the design template stamps no such var
//     (session.go reads none), it is the cloud CHECKOUT ref only, and the local
//     worktree-on-target_branch already reproduces the same working state — so this arm
//     does not read it.
//   - The prompt is exactly "/<command>" (no args) — the aiarch-design.yml draft step's
//     prompt, vs the construct step's "/<command> <component> <activity>".
func designDispatchPlan(projectID, jobMode, command, targetBranch, artifactKind string) localDispatchPlan {
	return localDispatchPlan{
		branch:        targetBranch,
		worktreeLabel: "design session-branch",
		rig: map[string]string{
			"AIARCH_PROJECT_ID":    projectID,
			"AIARCH_ARTIFACT_KIND": artifactKind,
			"AIARCH_JOB_MODE":      jobMode,
			"AIARCH_TARGET_BRANCH": targetBranch,
		},
		prompt: "/" + command,
	}
}

// Design/construct DispatchInputs keys — the wire contract with the seated workflow
// templates' workflow_dispatch.inputs (method-assets aiarch-design.yml.tmpl /
// products/.../aiarch-construct.yml) and the Managers that SET them
// ({systemdesign,projectdesign,construction} dispatchInput* constants). This realisation
// reads them as bare literals — the yml template is the single source of truth (the RA
// owns no shared Go constant with the Managers), and a named constant here documents each
// key + its meaning. (The local-only "job"/"merge" job-routing vocabulary the RA DOES own
// — DispatchInputJobKey / DispatchJobMerge — is exported instead, because the Manager sets
// it and it appears in no yml template.)
const (
	// dispatchInputJobModeKey is the DESIGN-vs-construct discriminator: every design
	// dispatch (draft/critique/answer) sets it; a construct dispatch never does (it
	// carries component_id/activity_id instead). A non-empty value routes Submit to the
	// design arm. Its value is also stamped as AIARCH_JOB_MODE in the design envelope.
	dispatchInputJobModeKey = "job_mode"
	// dispatchInputArtifactKindKey is the typed Method artifact kind a design job drafts/
	// critiques (e.g. "Mission", "Volatilities"); stamped as AIARCH_ARTIFACT_KIND.
	dispatchInputArtifactKindKey = "artifact_kind"
	// dispatchInputTargetBranchKey is the design SESSION branch the job worktrees on and
	// commits to; stamped as AIARCH_TARGET_BRANCH.
	dispatchInputTargetBranchKey = "target_branch"
	// dispatchInputCommandKey is the .claude slash-command slug both arms run; the design
	// prompt is exactly "/<command>", the construct prompt appends the component+activity.
	dispatchInputCommandKey = "command"
	// dispatchInputComponentIDKey is the construct-arm component the run targets; stamped
	// as AIARCH_COMPONENT_ID and appended to the construct prompt.
	dispatchInputComponentIDKey = "component_id"
)

// dispatch performs the synchronous prep (git-worktree-add the plan's branch, write
// the ephemeral --mcp-config + sandbox --settings, start claude) and, once the
// subprocess is genuinely running, hands the rest off to awaitCompletion in a
// goroutine. Any failure here is a genuine, pre-spawn submit failure. It is the SHARED
// core of both the construct arm and the design arm — everything arm-specific (branch,
// create-off-main policy, AIARCH_* rig, prompt) arrives in the plan; this function is
// arm-agnostic.
func (a *localExecAccess) dispatch(run *localRun, plan localDispatchPlan) error {
	branch := plan.branch

	// The worktree lives under a throwaway parent temp dir (NEVER inside the
	// user's checkout): the OS temp reaper is the backstop for crash leftovers,
	// and `git worktree prune` (constructor) clears the matching metadata. The
	// worktree itself is a SUBDIR of the parent ("wt") because `git worktree add`
	// wants a path it can own; git disambiguates the .git/worktrees entry name
	// itself if two dispatches' "wt" basenames collide.
	parentDir, err := os.MkdirTemp("", "aiarch-construct-*")
	if err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "localexec: create work dir")
	}
	workDir := filepath.Join(parentDir, "wt")
	ownWorkDir := true
	defer func() {
		if ownWorkDir {
			a.gitMu.Lock()
			removeWorktree(a.repoPath, workDir)
			a.gitMu.Unlock()
			_ = os.RemoveAll(parentDir)
		}
	}()

	a.gitMu.Lock()
	beforeSHA, gitDir, addErr := addWorktree(a.repoPath, branch, workDir)
	a.gitMu.Unlock()
	if addErr != nil {
		return fwra.Wrap(fwra.Infrastructure, addErr, "localexec: add "+plan.worktreeLabel+" worktree")
	}

	// Materialize the .claude prompt surface into the worktree BEFORE spawning claude —
	// the local mirror of the step BOTH seated workflow templates run right after checkout
	// ("Materialize the .claude prompt surface", aiarch-{design,construct}.yml.tmpl:
	// `aiarch-state-mcp seat-assets --dest .`). Operated repos gitignore .claude and rely
	// on this runtime render; without it the worktree has NO .claude/commands, the thin
	// "/<command>" slash-command dispatch does not resolve, and claude exits in ~20ms
	// ("Unknown command: /<command>", num_turns=0) having committed nothing — the observed
	// local-arm no-commit failure. Pre-spawn, so a failure returns cleanly (the deferred
	// worktree cleanup runs) and a retry re-seats from scratch, exactly like the worktree-add
	// failure above.
	if err := a.seatPromptSurface(workDir); err != nil {
		return err
	}

	// mcpConfigDir is the SAME out-of-clone ephemeral dir that holds BOTH the
	// --mcp-config file (writeStateMCPConfig) AND the Tier-2 --settings sandbox
	// file (writeSandboxSettings) — neither needs to be reachable from INSIDE
	// the sandboxed Bash tool's filesystem view, and keeping both outside the
	// clone means an agent running `git add -A` in workDir can never pick
	// either up (the same reasoning writeStateMCPConfig's own doc comment
	// already gives for the mcp-config file).
	mcpConfigDir, err := os.MkdirTemp("", "aiarch-mcp-cfg-*")
	if err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "localexec: create mcp config dir")
	}
	ownMCPDir := true
	defer func() {
		if ownMCPDir {
			_ = os.RemoveAll(mcpConfigDir)
		}
	}()

	// rig is the SAME fixed AIARCH_* ambient-context envelope stamped on BOTH
	// the attached aiarch-state MCP server's env (writeStateMCPConfig) and this
	// process's OWN env allowlist (claudeSubprocessEnv) — see the SECURITY
	// POSTURE doc block above claudeArgv for why both matter. The arm built every
	// entry except AIARCH_STATE_ROOT, which is stamped here = the worktree dir this
	// dispatch just created (not knowable until now); the plan's rig map is created
	// fresh per submit, so mutating it is race-free.
	rig := plan.rig
	rig["AIARCH_STATE_ROOT"] = workDir
	mcpConfigPath, err := writeStateMCPConfig(mcpConfigDir, a.stateMCPBin, rig)
	if err != nil {
		return err
	}
	// Worktree-mode filesystem scope (see the SECURITY POSTURE doc block): the
	// sandboxed Bash tool must be able to write (a) the worktree itself (cwd —
	// also the sandbox's own default, declared explicitly so the posture does not
	// depend on cwd inference) and (b) the SHARED repo's git dir — a worktree's
	// `git commit` writes .git/worktrees/<id> metadata, shared objects, and the
	// activity branch ref, all of which live OUTSIDE the worktree cwd.
	sandboxSettingsPath, err := writeSandboxSettings(mcpConfigDir, sandboxAllowedDomains(a.repoURL), []string{workDir, gitDir})
	if err != nil {
		return err
	}

	runCtx, runCancel := context.WithTimeout(context.Background(), a.runTimeout)
	cmd := exec.CommandContext(runCtx, "claude", claudeArgv(plan.prompt, mcpConfigPath, sandboxSettingsPath)...) //nolint:gosec // fixed trusted binary name + internal-only args, mirrors claudecli.go
	cmd.Dir = workDir
	cmd.Env = claudeSubprocessEnv(rig)
	// SIGTERM-then-bounded-pipe-close, the SAME shutdown mechanism serverchild.go's
	// startServerChild / claudecli.go's Generate already use for a supervised
	// subprocess — reused here per the task's explicit precedent guidance.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = localExecWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		runCancel()
		return fwra.Wrap(fwra.Infrastructure, err, "localexec: failed to start claude subprocess")
	}

	run.mu.Lock()
	run.cancel = runCancel
	run.mu.Unlock()

	// Ownership of parentDir (the worktree) / mcpConfigDir passes to awaitCompletion.
	ownWorkDir = false
	ownMCPDir = false
	go a.awaitCompletion(runCtx, run, cmd, runCancel, branch, beforeSHA, parentDir, workDir, mcpConfigDir, &stdout, &stderr)
	return nil
}

// seatPromptSurface materializes the .claude prompt surface (commands/skills/agents +
// the seat manifest) into the worktree by running the executor's OWN aiarch-state-mcp
// binary's `seat-assets --dest <workDir>` one-shot — the EXACT step both seated workflow
// templates run before claude ("Materialize the .claude prompt surface",
// method-assets aiarch-{design,construct}.yml.tmpl: `aiarch-state-mcp seat-assets --dest .`
// with `.` = the runner checkout root = this worktree). BOTH arms need it: construct AND
// design run the same thin "/<command>" slash-command dispatch, which only resolves once
// .claude/commands exists in the working tree.
//
// This runs the executor's OWN trusted binary (a.stateMCPBin — the same one attached as
// the aiarch-state MCP server), NOT the agent, so it is deliberately NOT sandboxed and gets
// only a minimal env (seatAssetsEnv: PATH/HOME). seat-assets reads NO AIARCH_* ambient
// context — it renders embedded files to --dest and exits. It writes to the working tree
// and makes NO commit, so its output cannot advance the branch ref (the ref-advanced
// success gate counts ONLY the agent's own aiarch-state commit); and in an operated /
// scaffolded repo the .claude/{commands,agents,skills/the-method*} paths are gitignored,
// so the render stays uncommitted exactly like the cloud runner.
func (a *localExecAccess) seatPromptSurface(workDir string) error {
	cmd := exec.Command(a.stateMCPBin, "seat-assets", "--dest", workDir) //nolint:gosec // the executor's OWN trusted binary (a.stateMCPBin) + fixed internal args, not the agent
	cmd.Dir = workDir
	cmd.Env = seatAssetsEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fwra.Wrap(fwra.Infrastructure, fmt.Errorf("%w: %s", err, outputTail(string(out), localExecDetailMaxBytes)), "localexec: seat .claude prompt surface")
	}
	return nil
}

// seatAssetsEnv is the minimal env for the seat-assets one-shot: PATH + HOME only. Unlike
// the MCP server / claude subprocess (which carry the AIARCH_* rig), seat-assets renders
// embedded files to --dest and reads no ambient context, so nothing else needs to cross.
func seatAssetsEnv() []string {
	out := make([]string, 0, 2)
	if v := os.Getenv("PATH"); v != "" {
		out = append(out, "PATH="+v)
	}
	if v := os.Getenv("HOME"); v != "" {
		out = append(out, "HOME="+v)
	}
	return out
}

// awaitCompletion blocks on the claude subprocess, verifies its commits advanced
// the activity branch ref in the SHARED repo (worktree commits land there
// directly — no push; partial progress on a failed run is already durable),
// removes the worktree (completion AND cancel paths alike — the SIGTERM'd cancel
// path flows through here too, since cmd.Wait returns either way), cleans up the
// temp dirs, then records the terminal outcome. If CancelAgenticJob
// already recorded localRunCancelled before this observes completion, that
// outcome wins (Wait()'s own error, expected after a SIGTERM, is not allowed to
// overwrite an explicit cancel with "Failed").
//
// On EVERY failing outcome it also surfaces what claude actually said — the
// bounded detail onto the diagnostic, the full output onto a durable log dir —
// see the LOCAL-RUN OBSERVABILITY block below for why that is load-bearing.
func (a *localExecAccess) awaitCompletion(
	runCtx context.Context,
	run *localRun,
	cmd *exec.Cmd,
	runCancel context.CancelFunc,
	branch, beforeSHA string,
	parentDir, workDir, mcpConfigDir string,
	stdout, stderr *bytes.Buffer,
) {
	waitErr := cmd.Wait()
	runCancel()

	a.gitMu.Lock()
	afterSHA, revErr := revParseBranch(a.repoPath, branch)
	removeErr := removeWorktree(a.repoPath, workDir)
	a.gitMu.Unlock()

	_ = os.RemoveAll(parentDir)
	_ = os.RemoveAll(mcpConfigDir)

	// An explicit cancel already recorded the terminal outcome: short-circuit
	// BEFORE the diagnostic/durable-log work below, because a cancelled run's
	// non-zero Wait() is expected operator intent, not a fault worth an enriched
	// diagnostic or a log artifact. Re-checked under the final lock (a cancel
	// landing DURING the window below must still win) — this peek only avoids
	// doing the work, it is not the authority on the outcome.
	run.mu.Lock()
	alreadyCancelled := run.status == localRunCancelled
	run.mu.Unlock()
	if alreadyCancelled {
		return
	}

	timedOut := waitErr != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded)
	status, diagnostic, stderrShown := localRunOutcome(waitErr, revErr, removeErr, timedOut, beforeSHA, afterSHA, stderr.String())
	if status == localRunFailed {
		// The failure-path IO (temp-dir write) deliberately runs BEFORE the record
		// lock, matching this function's existing discipline: every other blocking
		// step (git subprocesses, RemoveAll) also completes before the lock, so a
		// concurrent ObserveAgenticJob poll never waits on filesystem IO.
		stderrForDetail := stderr.String()
		if stderrShown {
			// classifyLocalExecFailure already embedded the stderr tail in the
			// leading sentence — do not print it twice.
			stderrForDetail = ""
		}
		if detail := claudeOutputDetail(stdout.String(), stderrForDetail); detail != "" {
			diagnostic += localExecDetailSeparator + detail
		}
		logFailedRun(branch, diagnostic, stdout.Bytes(), stderr.Bytes())
	}

	run.mu.Lock()
	defer run.mu.Unlock()
	if run.status == localRunCancelled {
		return
	}
	run.status = status
	run.diagnostic = diagnostic
}

// localRunOutcome maps one finished claude invocation onto its terminal run
// status + the RA's OWN leading diagnostic sentence. Pure (no locks, no IO, no
// context) so the whole outcome table is directly testable and so
// awaitCompletion stays a thin sequence of steps.
//
// The third return value reports whether the diagnostic ALREADY carries a stderr
// excerpt (only classifyLocalExecFailure adds one), which is how awaitCompletion
// avoids printing stderr twice when it appends the claude-output detail.
func localRunOutcome(
	waitErr, revErr, removeErr error,
	timedOut bool,
	beforeSHA, afterSHA, stderrText string,
) (localRunStatus, string, bool) {
	switch {
	case timedOut:
		return localRunFailed, "construction pipeline timed out", false
	case waitErr != nil:
		return localRunFailed, classifyLocalExecFailure(waitErr, stderrText), true
	case revErr != nil:
		// claude exited 0 but the durable post-condition cannot even be VERIFIED —
		// NOT a success (no fake success states). ("target branch" reads honestly for
		// both arms: the construct activity/<id> branch and the design session branch.)
		return localRunFailed, "run completed but the target branch could not be verified: " + revErr.Error(), false
	case afterSHA == beforeSHA:
		// claude exited 0 but committed NOTHING — the target branch ref did not
		// advance, so the durable post-condition (work landed on the branch) does not
		// hold. The worktree-rework analog of the former push-failure failure, and the
		// local mirror of aiarch-design.yml's "Verify claude pushed a commit" guard.
		// THE canonical black-box case: without the appended claude-output detail this
		// sentence is ALL the operator ever sees.
		return localRunFailed, "run completed but produced no commits on the target branch", false
	case removeErr != nil:
		// The commits ARE durable (refs advanced in the shared repo), but a
		// worktree that cannot be removed still holds the target branch checked
		// out and would block the NEXT worktree add — surface it rather than
		// declaring clean success over a wedged workspace.
		return localRunFailed, "run committed but the worktree could not be removed: " + removeErr.Error(), false
	default:
		return localRunSucceeded, "", false
	}
}

// localRunLostDiagnostic is the terminal-failure diagnostic ObserveAgenticJob
// reports for a well-formed handle whose in-memory run record is GONE. The local executor
// keeps run records ONLY in memory (a.runs), so a server RESTART mid-run loses them while
// the Manager's Temporal workflow still holds the PipelineHandle and keeps polling. Unlike
// the cloud arm (a GitHub Actions run is durable + re-observable across restarts), a lost
// LOCAL run is UNRECOVERABLE — reporting it terminal-failed (not a perpetual running/
// pending) is what lets the observe loop reach StageDraftFailed and the human Retry/
// Withdraw gate instead of spinning to the maxObservePolls (1h) ceiling (F-R1).
const localRunLostDiagnostic = "local run record not found — the server restarted while this run was in flight; the run cannot be recovered, retry to re-dispatch"

// ObserveAgenticJob reads the in-memory run record and maps its status to the
// infrastructure-neutral PipelinePhase. A malformed/foreign-shaped handle is ContractMisuse.
//
// RESTART-LOST RUN (F-R1): a WELL-FORMED handle whose run record is MISSING from a.runs is
// NOT a live run — the local executor registers the record synchronously in Submit before
// returning the handle, so within one process a handle always has its record. A missing
// record therefore means the process RESTARTED and lost the in-memory map. That run can
// never reappear, so it is reported as a TERMINAL PhaseFailed observation (nil error,
// localRunLostDiagnostic) — NOT fwra.NotFound and NOT a still-running phase — so the
// Manager's observe loop hits its terminal-phase branch and routes to StageDraftFailed
// rather than looping to the maxObservePolls ceiling. This DIVERGES DELIBERATELY from
// actions.go's cloud arm (durable, re-observable runs → a GC'd handle is fwra.NotFound):
// the divergence exists precisely because in-memory local runs are not restart-durable.
func (a *localExecAccess) ObserveAgenticJob(_ fwra.Context, handle PipelineHandle) (PipelineObservation, error) {
	token, ok := localTokenFromHandle(handle)
	if !ok {
		return PipelineObservation{}, fwra.New(fwra.ContractMisuse, "agenticjob(localexec): malformed PipelineHandle")
	}
	a.mu.Lock()
	run, ok := a.runs[token]
	a.mu.Unlock()
	if !ok {
		return PipelineObservation{Handle: handle, Phase: PhaseFailed, Diagnostic: localRunLostDiagnostic}, nil
	}

	run.mu.Lock()
	defer run.mu.Unlock()
	obs := PipelineObservation{Handle: run.handle, Phase: localPhase(run.status)}
	if run.status == localRunFailed {
		obs.Diagnostic = run.diagnostic
	}
	return obs, nil
}

// CancelAgenticJob requests cancellation of a running local dispatch.
// Cancelling an unknown/already-terminal run is a no-op SUCCESS (idempotent-on-
// intent, same contract as actions.go's CancelAgenticJob).
func (a *localExecAccess) CancelAgenticJob(_ fwra.Context, handle PipelineHandle) error {
	token, ok := localTokenFromHandle(handle)
	if !ok {
		return fwra.New(fwra.ContractMisuse, "agenticjob(localexec): malformed PipelineHandle")
	}
	a.mu.Lock()
	run, ok := a.runs[token]
	a.mu.Unlock()
	if !ok {
		return nil
	}

	run.mu.Lock()
	if run.status != localRunRunning {
		run.mu.Unlock()
		return nil
	}
	run.status = localRunCancelled
	cancel := run.cancel
	run.mu.Unlock()

	if cancel != nil {
		cancel() // triggers cmd.Cancel (SIGTERM), bounded by cmd.WaitDelay
	}
	return nil
}

// localPhase maps the in-memory run status onto the port's PipelinePhase.
func localPhase(s localRunStatus) PipelinePhase {
	switch s {
	case localRunRunning:
		return PhaseRunning
	case localRunSucceeded:
		return PhaseSucceeded
	case localRunFailed:
		return PhaseFailed
	case localRunCancelled:
		return PhaseCancelled
	default:
		return PhasePending
	}
}

// localTokenFromHandle unpacks the dedup token from a "local:<token>" handle. A
// zero/malformed/foreign-shaped handle (e.g. an actions.go "run/<id>" handle
// crossing into the wrong realisation) returns ok=false.
func localTokenFromHandle(h PipelineHandle) (string, bool) {
	s := string(h)
	if !strings.HasPrefix(s, localHandlePrefix) {
		return "", false
	}
	token := strings.TrimPrefix(s, localHandlePrefix)
	if token == "" {
		return "", false
	}
	return token, true
}

// classifyLocalExecFailure builds a diagnostic for a non-zero/never-started claude
// exit — Step 3's "map subprocess exit codes into the run-status vocabulary the
// poll loop expects". Unlike claudecli.go's classifyClaudeCLIFailureText this does
// NOT attempt fault-KIND classification (Auth/QuotaExhausted/Transient) — Observe's
// contract carries only a PipelinePhase + a free-text Diagnostic (the Manager's
// intervention path treats every PhaseFailed uniformly, exactly as actions.go's
// neutralDiagnostic does for a failed GitHub Actions run), so a plain, honest
// description (including a bounded stderr tail for debuggability) is what the
// contract needs.
func classifyLocalExecFailure(runErr error, stderrText string) string {
	tail := outputTail(stderrText, localExecDetailMaxBytes)
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if tail != "" {
			return fmt.Sprintf("construction pipeline failed: claude exited %d: %s", exitErr.ExitCode(), tail)
		}
		return fmt.Sprintf("construction pipeline failed: claude exited %d", exitErr.ExitCode())
	}
	return "construction pipeline failed to start: " + runErr.Error()
}

// outputTail bounds a diagnostic's subprocess-output excerpt to the LAST n bytes
// (Non-goal: this is a summary, never a log firehose — the same discipline
// actions.go's neutralDiagnostic doc comment cites from
// agenticJobAccess.md). Used for stderr AND for unstructured stdout:
// when a process dies mid-flight, what it printed LAST is what explains it.
// Rune-safe — the excerpt reaches a UI panel, so it never splits a multi-byte
// character into a replacement glyph.
func outputTail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	tail := s[len(s)-n:]
	for len(tail) > 0 && !utf8.ValidString(tail) {
		tail = tail[1:] // drop the leading fragment of a split rune
	}
	return "…" + tail
}

// outputHead bounds an excerpt to the FIRST n bytes — the counterpart to
// outputTail, used for STRUCTURED messages (claude's JSON result/error text),
// where the informative part is the opening clause and the tail is trailing
// prose. Rune-safe for the same reason as outputTail.
func outputHead(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	head := s[:n]
	for len(head) > 0 && !utf8.ValidString(head) {
		head = head[:len(head)-1] // drop the trailing fragment of a split rune
	}
	return head + "…"
}

// ---------------------------------------------------------------------------
// LOCAL-RUN OBSERVABILITY — surfacing what claude actually said.
//
// THE BLACK-BOX FAILURE MODE this exists to kill: awaitCompletion used to
// DISCARD the subprocess's stdout outright (a bare `_ = stdout`), so a local run
// that failed to advance the branch surfaced to the operator as exactly ONE
// sentence — "run completed but produced no commits on the target branch" — with
// nothing to distinguish an MCP server that failed to attach, an unresolved
// slash command, an auth terminal, a sandbox denial, and an agent that simply
// decided there was nothing to do. Those demand completely different operator
// responses, and the ONE artifact that tells them apart was being thrown away.
//
// claudeArgv passes --output-format json, so stdout is a machine-readable result
// envelope. The helpers below mine it along two complementary channels:
//
//   - the DIAGNOSTIC gets a hard-bounded, single-line clause (this string is
//     rendered in the web UI's failure panel, so it must stay presentable);
//   - a DURABLE LOG DIR gets the full, untruncated stdout+stderr for post-mortem,
//     because the bounded clause is by construction lossy.
//
// Scope discipline: this is OBSERVABILITY ONLY. Nothing here changes claudeArgv,
// the sandbox settings, the env allowlist, or the escape hatch (see the SECURITY
// POSTURE block below), and nothing here logs the AIARCH_* rig or the process
// env — only claude's OWN output is surfaced.
//
// KNOWN INTERACTION (accepted, not hidden): construction's deriveFailureReason
// classifies a diagnostic by substring ("timed out"), so appended agent prose
// containing that phrase could tip a non-timeout failure into the timed-out
// FailureReason bucket. The leading sentence is never rewritten and the detail
// is always appended AFTER it, which keeps the common cases right; tightening
// that matcher is a Manager-side change, deliberately not made from this RA.
// ---------------------------------------------------------------------------

const (
	// localExecDetailSeparator joins the RA's own failure sentence to the
	// claude-derived detail. The leading sentence is NEVER rewritten or replaced —
	// downstream consumers match on its vocabulary — so the panel reads as
	// "<what went wrong> — claude output: <what claude said>".
	localExecDetailSeparator = " — claude output: "
	// localExecDetailMaxBytes hard-bounds BOTH the appended detail and the stderr
	// tail classifyLocalExecFailure already embedded. The full text lives in the
	// durable log dir instead.
	localExecDetailMaxBytes = 500
	// localExecSubtypeMaxBytes bounds the envelope's subtype label so a
	// pathological value can never crowd out the message it labels.
	localExecSubtypeMaxBytes = 64
)

// claudeResultEnvelope is the SUBSET of claude's `--output-format json` result
// object this package reads. Decoding is deliberately tolerant: every field is
// optional, an unknown shape yields "" rather than an error, and a field claude
// stops emitting simply disappears from the diagnostic. This is a debugging
// convenience on a failure path — surviving upstream shape drift matters far
// more than decoding strictly.
//
// Result is the agent's own closing message (a JSON string in practice); Error
// may be either a string or a structured object, so both are held as raw JSON
// and rendered by jsonText.
type claudeResultEnvelope struct {
	Subtype string          `json:"subtype"`
	IsError bool            `json:"is_error"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

// claudeOutputDetail distils a finished claude invocation's captured output into
// ONE bounded, single-line, human-readable clause for the diagnostic, trying the
// most informative source first:
//
//  1. the JSON result envelope (structured: the subtype + the agent's own
//     result/error text — what an operator actually needs);
//  2. raw stdout, when claude died before emitting valid JSON (a crash, a
//     startup failure, a usage error) — the envelope is absent exactly when
//     something went badly enough to be worth reading verbatim;
//  3. stderr, only when stdout yielded nothing at all. Callers whose leading
//     sentence ALREADY carries a stderr tail pass stderrText="" to avoid
//     duplicating it.
//
// Returns "" when the process produced no output whatsoever — the caller then
// leaves its leading sentence untouched rather than appending an empty clause.
func claudeOutputDetail(stdoutText, stderrText string) string {
	if detail := envelopeDetail(stdoutText); detail != "" {
		return detail
	}
	if raw := singleLine(stdoutText); raw != "" {
		return outputTail(raw, localExecDetailMaxBytes)
	}
	return outputTail(singleLine(stderrText), localExecDetailMaxBytes)
}

// envelopeDetail renders the human clause from claude's JSON result envelope, or
// "" when stdout carries no decodable envelope (claude may die before emitting
// one). The whole of stdout is tried first; failing that, the LAST JSON-object
// line is tried, because claude can print warnings/progress ahead of the final
// envelope.
func envelopeDetail(stdoutText string) string {
	raw := strings.TrimSpace(stdoutText)
	if raw == "" {
		return ""
	}
	env, ok := decodeClaudeEnvelope(raw)
	if !ok {
		lines := strings.Split(raw, "\n")
		for i := len(lines) - 1; i >= 0 && !ok; i-- {
			if line := strings.TrimSpace(lines[i]); strings.HasPrefix(line, "{") {
				env, ok = decodeClaudeEnvelope(line)
			}
		}
	}
	if !ok {
		return ""
	}
	return env.detail()
}

// detail renders one envelope as "<subtype>: <message>", degrading to whichever
// half is present. The subtype alone is still worth surfacing: on a no-commit
// run, a bare "success" tells the operator claude believed it was DONE (an agent
// or prompt problem) rather than that it broke (an infrastructure problem).
func (env claudeResultEnvelope) detail() string {
	label := outputHead(env.Subtype, localExecSubtypeMaxBytes)
	if label == "" && env.IsError {
		label = "error"
	}
	text := singleLine(jsonText(env.Result))
	if text == "" {
		text = singleLine(jsonText(env.Error))
	}
	switch {
	case label != "" && text != "":
		return label + ": " + outputHead(text, localExecDetailMaxBytes-len(label)-2)
	case text != "":
		return outputHead(text, localExecDetailMaxBytes)
	default:
		return label
	}
}

// decodeClaudeEnvelope parses one candidate JSON object. A JSON value that is
// not an object (a bare string/number/array) fails to decode into the struct and
// is reported as not-an-envelope, which is what routes the caller to the raw
// fallback.
func decodeClaudeEnvelope(s string) (claudeResultEnvelope, bool) {
	var env claudeResultEnvelope
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		return claudeResultEnvelope{}, false
	}
	return env, true
}

// jsonText renders an envelope field that may be EITHER a JSON string (the usual
// `result`) or an arbitrary JSON value (a structured `error`) as plain text: a
// string is unquoted, anything else passes through as its compact JSON so a
// structured error is still readable rather than silently dropped.
func jsonText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(string(raw))
}

// singleLine collapses every whitespace run (newlines, tabs, indentation) into a
// single space. The diagnostic lands in a ONE-LINE failure panel, and an agent's
// multi-paragraph result text would otherwise wreck that layout.
func singleLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// localExecLogDirPattern names the durable post-mortem log dir (os.MkdirTemp
// pattern). Grep-friendly on purpose: the operator finds every kept run with one
// glob under the OS temp dir.
const localExecLogDirPattern = "aiarch-localexec-logs-*"

// persistFailedRunOutput writes the claude subprocess's FULL stdout+stderr to a
// fresh temp dir that deliberately OUTLIVES the run.
//
// LIFECYCLE — and why this is not simply the run's own parent temp dir: that dir
// hosts the git worktree and is removed on EVERY path (keeping it would strand a
// removed worktree's files and defeat the executor's own hygiene), so the logs
// go to a SEPARATE dir that nothing in this package ever deletes. Nothing is
// written inside the user's checkout — same rule as the worktree and mcp-config
// dirs. The OS temp reaper is the only collector, exactly as it already is for
// crash-leftover worktrees. A SUCCESSFUL run writes NOTHING, so normal operation
// accumulates no log litter; only genuine failures leave a trail.
//
// A partially-written dir is still returned (with the error) so the caller can
// point the operator at whatever DID land.
func persistFailedRunOutput(stdout, stderr []byte) (string, error) {
	dir, err := os.MkdirTemp("", localExecLogDirPattern)
	if err != nil {
		return "", err
	}
	// stdout is the --output-format json result envelope; stderr is claude's own
	// diagnostic stream. Both verbatim — this is the un-truncated counterpart to
	// the bounded diagnostic clause.
	if err := os.WriteFile(filepath.Join(dir, "stdout.json"), stdout, 0o600); err != nil {
		return dir, err
	}
	if err := os.WriteFile(filepath.Join(dir, "stderr.log"), stderr, 0o600); err != nil {
		return dir, err
	}
	return dir, nil
}

// logFailedRun emits the server-side record of a failed local dispatch: the SAME
// enriched diagnostic the web UI shows, PLUS the durable log path. Both matter —
// the Managers log only the phase transition and the diagnostic they were handed,
// so without this the detail would be reconstructible from neither the log nor
// the UI once the run record is GC'd.
//
// Deliberately logged at ERROR: a local run that could not land its work is an
// operator-actionable fault, not routine noise (a CANCELLED run never reaches
// here — awaitCompletion short-circuits it).
//
// NOTHING from the run's environment is logged: not the AIARCH_* rig, not the
// process env allowlist, not the sandbox settings — only claude's own output.
func logFailedRun(branch, diagnostic string, stdout, stderr []byte) {
	logDir, err := persistFailedRunOutput(stdout, stderr)
	switch {
	case err != nil && logDir == "":
		slog.Error("localexec: construction run failed; the durable output log could NOT be written",
			"branch", branch, "diagnostic", diagnostic, "cause", err.Error())
	case err != nil:
		slog.Error("localexec: construction run failed; the durable output log is INCOMPLETE",
			"branch", branch, "diagnostic", diagnostic, "outputLog", logDir, "cause", err.Error())
	default:
		slog.Error("localexec: construction run failed; full claude output kept for post-mortem",
			"branch", branch, "diagnostic", diagnostic, "outputLog", logDir)
	}
}

// ---------------------------------------------------------------------------
// SECURITY POSTURE (founder ruling — sandboxed-by-default local execution).
// This is the load-bearing doc block for the executor spawn site (dispatch,
// above): what confines an autonomous, --dangerously-skip-permissions
// headless `claude` run on the developer's OWN machine, what does not, and
// why. The founder's explicit ruling for this fix: Tier 1 (process/env/git
// isolation) PLUS Tier 2 (claude's native OS sandbox) are the FIXED, non-
// negotiable default for every local dispatch; a heavier Tier 3
// (devcontainer/VM around the WHOLE claude process, not just its Bash tool)
// is EXPLICITLY DEFERRED as a future opt-in — not required, not built here.
//
// WHAT CONFINES a dispatch, and how:
//
//   - cwd (Tier 1, WEAKENED by the worktree rework — founder-accepted): every
//     dispatch adds a throwaway git WORKTREE of the shared repo in a temp dir
//     (dispatch's workDir) and runs claude there; the worktree is removed
//     afterward (awaitCompletion) — no persistent working tree accumulates
//     state across dispatches. Unlike the original fresh-clone design, the
//     worktree is NOT an isolated copy: its .git file points INTO the shared
//     repo's own .git, so the run's git operations act on the user's real
//     repository object store and refs directly. This is a deliberate,
//     FOUNDER-ACCEPTED isolation tradeoff — speed (no full clone per
//     dispatch) over clone isolation.
//   - env (Tier 1): claudeSubprocessEnv below is a CONSTRUCTED ALLOWLIST —
//     PATH, HOME, TERM, and the AIARCH_* rig vars, nothing else — replacing
//     the former full-parent-env passthrough. A stray secret exported into
//     the archistrator-server process's OWN environment can no longer reach
//     an autonomous, permission-bypassed agent process.
//   - --strict-mcp-config (Tier 1): only the ONE ephemeral aiarch-state MCP
//     server this dispatch writes (writeStateMCPConfig) is attached; ambient
//     user-level (~/.claude.json) or project-level (.mcp.json in whatever
//     repo happens to be checked out on this machine) MCP configuration is
//     ignored, so a construction run can never inherit extra tool surface
//     the operator configured for their OWN interactive sessions.
//   - Tier 2 OS sandbox (claudeArgv + writeSandboxSettings below): claude's
//     BUILT-IN native sandbox — macOS Seatbelt (nothing to install) or Linux/
//     WSL2 bubblewrap — is turned on via an ephemeral --settings file
//     (sandbox.enabled=true). It confines the Bash TOOL's subprocess tree at
//     the OS level: filesystem writes restricted to the working directory +
//     session temp dir (the sandbox's own default) PLUS, since the worktree
//     rework, two explicit sandbox.filesystem.allowWrite entries: the
//     worktree dir and the SHARED repo's .git dir — a worktree's git
//     operations write .git/worktrees metadata, shared objects, and the
//     activity branch ref there, all outside cwd. Widening the sandbox to
//     the user's real .git means a sandboxed Bash command can in principle
//     touch OTHER refs/objects of that repository too (git refuses hooks/
//     config writes for a worktree's shared .git under its native carve-out,
//     but our explicit allowWrite covers the whole dir) — this is the SAME
//     founder-accepted speed-over-clone-isolation tradeoff called out in the
//     cwd bullet above, stated here honestly rather than hidden. Network
//     remains DENIED BY DEFAULT except the allowlisted domains
//     (sandboxAllowedDomains: Anthropic's own API domain + the git remote's
//     host when repoURL is not file://; the worktree executor only ever
//     addresses a local repo, so in practice the list is the API domain).
//     sandbox.failIfUnavailable=true + allowUnsandboxedCommands=false close
//     the loop: if the OS sandbox cannot initialize, claude refuses to run
//     at all rather than silently degrading unsandboxed — see claudeArgv's
//     own THE INVARIANT doc comment for the full fail-closed argument.
//
// WHAT DOES NOT CONFINE (documented honestly, not swept under the rug):
//
//   - Claude's BUILT-IN file tools (Read/Edit/Write) are NOT covered by the
//     OS sandbox at all — per Anthropic's own sandboxing docs, OS-level
//     enforcement applies ONLY to the Bash tool and its subprocess tree;
//     Edit/Write go through Claude's permission system directly, which
//     --dangerously-skip-permissions bypasses entirely. cwd confinement
//     (Tier 1, above) is therefore the ONLY thing bounding where those
//     tools write in practice — there is no OS-level backstop for them the
//     way there is for Bash. This is Anthropic's documented sandbox SCOPE,
//     not a gap this package introduces.
//   - Toolchain build/module caches (Go's GOCACHE/GOPATH, npm's cache, ...)
//     sit OUTSIDE the sandbox's default filesystem-write allowlist (workDir
//     + session temp only); a construction task whose Bash tool calls
//     invoke a toolchain that insists on writing elsewhere may fail under
//     the sandbox. Widening sandbox.filesystem.allowWrite for specific
//     known-safe cache paths is a plausible v1.1 follow-up once real
//     construction runs show which paths are actually needed —
//     deliberately NOT guessed at here.
//   - claude's OWN model/API network traffic runs in the (unsandboxed)
//     PARENT process, never inside the Bash-tool sandbox boundary — the
//     network allowlist above governs ONLY what Bash-tool subprocesses can
//     reach, not claude's own calls to Anthropic's API.
//   - sandbox.credentials (masking/denying specific credential files or env
//     vars from the Bash tool) is NOT configured — the env allowlist above
//     already keeps this process from handing claude's Bash tool anything
//     beyond PATH/HOME/TERM/AIARCH_*, so there is no ambient credential in
//     the child's OWN env left to mask; a future caller that widens the env
//     allowlist should reconsider this.
//   - Tier 3 (devcontainer/VM) is NOT implemented — see the founder-ruling
//     paragraph above; tracked as a future opt-in, not a v1 requirement.
// ---------------------------------------------------------------------------

// localExecAllowUnsandboxedEnv is the ONLY escape hatch from THE INVARIANT
// (claudeArgv below): default unset/false — sandboxed-by-default is
// FAIL-CLOSED. Set ARCHISTRATOR_LOCAL_EXEC_ALLOW_UNSANDBOXED=true ONLY for a
// deployment/platform where the native OS sandbox genuinely cannot run at
// all (see the SECURITY POSTURE block above). This is an OPERATOR opt-out
// read fresh per dispatch, never a code-path default.
const localExecAllowUnsandboxedEnv = "ARCHISTRATOR_LOCAL_EXEC_ALLOW_UNSANDBOXED"

// allowUnsandboxedFromEnv reports whether the operator has explicitly set the
// escape hatch. Mirrors the plain os.Getenv-driven policy reads already
// established in this package (e.g. claudeSubprocessEnv's ANTHROPIC_API_KEY
// stripping precedent) rather than threading a new constructor parameter
// through NewLocalExecAgenticJobAccess for a rarely-used override.
func allowUnsandboxedFromEnv() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(localExecAllowUnsandboxedEnv)), "true")
}

// claudeArgv builds the exact argv for the headless claude invocation. This is
// the ONLY function in the package that constructs the flag list — there is
// no second, unguarded call site.
//
// THE INVARIANT (founder security-posture ruling, sandboxed-by-default —
// see the SECURITY POSTURE doc block above): --dangerously-skip-permissions
// is passed ONLY together with an ACTIVE Tier-2 sandbox
// (--settings sandboxSettingsPath, whose sandbox.enabled=true +
// failIfUnavailable=true + allowUnsandboxedCommands=false — see
// writeSandboxSettings). If claude's own sandbox init then fails (missing
// bubblewrap, unsupported platform, a corrupted settings file, ...),
// failIfUnavailable makes CLAUDE ITSELF refuse to run unsandboxed — the
// process exits non-zero and the failure flows through the SAME
// classifyLocalExecFailure path every other claude failure already takes,
// surfacing as PhaseFailed with an actionable diagnostic (the stderr tail).
// THERE IS NO SILENT UNSANDBOXED FALLBACK. The ONLY way to run without the
// sandbox is the operator-set ARCHISTRATOR_LOCAL_EXEC_ALLOW_UNSANDBOXED=true
// escape hatch (localExecAllowUnsandboxedEnv) — for a platform where the
// mechanism cannot run at all. Even with the escape hatch active,
// --dangerously-skip-permissions is STILL required (headless construction
// has no human present to approve tool calls), so the escape hatch trades
// OS-level containment for NONE, not for a lesser tier — use it only when
// genuinely necessary, and never add a code path that appends
// --dangerously-skip-permissions outside this function.
func claudeArgv(prompt, mcpConfigPath, sandboxSettingsPath string) []string {
	args := []string{"--dangerously-skip-permissions"}
	if allowUnsandboxedFromEnv() {
		// Escape hatch active: THE INVARIANT's pairing is deliberately broken
		// here, and ONLY here, by explicit operator opt-out — see
		// localExecAllowUnsandboxedEnv's doc comment.
	} else {
		args = append(args, "--settings", sandboxSettingsPath)
	}
	return append(args,
		"--mcp-config", mcpConfigPath,
		"--strict-mcp-config", // Tier 1: ignore ambient user/project MCP config; attach ONLY mcpConfigPath.
		"--output-format", "json",
		"-p", prompt,
	)
}

// sandboxSettingsJSON / sandboxConfigJSON / sandboxNetworkJSON mirror the
// minimal shape `claude --settings <file>` expects for Tier-2 native OS
// sandboxing. Discovered from the installed claude CLI (`claude --help`
// documents --settings <file-or-json>; the settings.json `sandbox` key
// shape was confirmed against Anthropic's current sandboxing docs and
// empirically verified with a live `claude --settings ... --dangerously-
// skip-permissions --strict-mcp-config -p ...` invocation against this
// exact JSON shape):
//
//   - sandbox.enabled            turns the OS sandbox on for this run.
//   - sandbox.failIfUnavailable  makes claude refuse to START rather than
//     silently degrading to unsandboxed when the mechanism cannot init
//     (missing bubblewrap, unsupported platform, ...) — THIS is what makes
//     THE INVARIANT (claudeArgv above) hold even on a platform that lacks
//     the sandbox: claude itself fails fast, so this package never has to
//     independently detect "sandbox unavailable" by parsing stderr text.
//   - sandbox.allowUnsandboxedCommands=false disables claude's OWN internal
//     dangerouslyDisableSandbox per-command retry escape hatch (its model
//     would otherwise be allowed to retry a sandbox-denied Bash command
//     WITHOUT the sandbox) — belt-and-braces against the outer
//     --dangerously-skip-permissions bypass, which would otherwise
//     auto-approve that retry with no prompt at all.
//   - sandbox.network.allowedDomains is the ONLY network the sandboxed Bash
//     tool's subprocesses can reach; see sandboxAllowedDomains below.
type sandboxSettingsJSON struct {
	Sandbox sandboxConfigJSON `json:"sandbox"`
}

type sandboxConfigJSON struct {
	Enabled                  bool                   `json:"enabled"`
	FailIfUnavailable        bool                   `json:"failIfUnavailable"`
	AllowUnsandboxedCommands bool                   `json:"allowUnsandboxedCommands"`
	Network                  *sandboxNetworkJSON    `json:"network,omitempty"`
	Filesystem               *sandboxFilesystemJSON `json:"filesystem,omitempty"`
}

type sandboxNetworkJSON struct {
	AllowedDomains []string `json:"allowedDomains,omitempty"`
}

// sandboxFilesystemJSON mirrors the documented sandbox.filesystem settings key
// (Claude Code sandboxing docs, "Configure sandboxing"): allowWrite entries are
// path prefixes (absolute here) the sandboxed Bash tool may write beneath, ON
// TOP of the sandbox's own defaults (cwd + session temp). Worktree mode uses it
// for the worktree dir + the shared repo's git dir — see writeSandboxSettings's
// caller (dispatch) and the SECURITY POSTURE doc block for the tradeoff.
type sandboxFilesystemJSON struct {
	AllowWrite []string `json:"allowWrite,omitempty"`
}

// writeSandboxSettings writes the ephemeral --settings file (OUTSIDE the
// cloned working tree — dir is the SAME out-of-clone temp dir dispatch also
// writes the --mcp-config file into, and the sandbox itself denies writes to
// settings.json paths as a matter of course, so this file could not be
// tampered with from inside the sandbox even if it DID live in the clone)
// that turns on Tier-2 sandboxing for this run — always enabled=true,
// failIfUnavailable=true, allowUnsandboxedCommands=false (see the type's own
// doc comment for why each matters). allowedDomains is the Bash tool's
// network allowlist (sandboxAllowedDomains); an empty list means Bash
// subprocess commands get NO outbound network at all, which is exactly
// correct for the common local-mode case (repoURL is file://, so there is no
// remote to reach and no allowlist entry beyond the fixed API domain).
// allowWritePaths is the worktree-mode filesystem widening (the worktree dir +
// the shared repo's git dir — see dispatch); empty omits the filesystem block
// entirely, keeping the sandbox's own default write scope.
func writeSandboxSettings(dir string, allowedDomains, allowWritePaths []string) (string, error) {
	cfg := sandboxSettingsJSON{Sandbox: sandboxConfigJSON{
		Enabled:                  true,
		FailIfUnavailable:        true,
		AllowUnsandboxedCommands: false,
	}}
	if len(allowedDomains) > 0 {
		cfg.Sandbox.Network = &sandboxNetworkJSON{AllowedDomains: allowedDomains}
	}
	if len(allowWritePaths) > 0 {
		cfg.Sandbox.Filesystem = &sandboxFilesystemJSON{AllowWrite: allowWritePaths}
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fwra.Wrap(fwra.Infrastructure, err, "localexec: marshal sandbox settings")
	}
	f, err := os.CreateTemp(dir, "aiarch-sandbox-*.json")
	if err != nil {
		return "", fwra.Wrap(fwra.Infrastructure, err, "localexec: create sandbox settings file")
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(body); err != nil {
		return "", fwra.Wrap(fwra.Infrastructure, err, "localexec: write sandbox settings file")
	}
	return f.Name(), nil
}

// anthropicAPIDomains is the Tier-2 sandbox's fixed network-allowlist entry
// for Anthropic's own API surface — included per the founder's "network
// limited to the API + git remote" ruling, defense-in-depth against a future
// claude version routing more of its own traffic through the (currently
// Bash-tool-only) sandbox boundary. As documented today (claude 2.1.x),
// claude's own model/API calls run in the unsandboxed parent process, so
// this entry is a no-op for the CLI's CURRENT actual behavior — it costs
// nothing to declare and is exactly correct if that scope ever widens.
var anthropicAPIDomains = []string{"api.anthropic.com"}

// sandboxAllowedDomains derives the Tier-2 sandbox's network allowlist:
// anthropicAPIDomains, PLUS the git remote's host, ONLY when repoURL is a
// real network URL (not file://, which local mode always passes — see the
// package doc comment's "WHY EVERY DISPATCH CLONES FRESH" note). A
// malformed/unparsable repoURL yields just the fixed API domain rather than
// failing the dispatch — the sandbox's own deny-by-default network policy
// stays SAFE either way; an unparsable host just means git-over-network
// inside the Bash tool cannot reach anything, no worse than the local-mode
// file:// norm.
func sandboxAllowedDomains(repoURL string) []string {
	domains := append([]string{}, anthropicAPIDomains...)
	u, err := url.Parse(repoURL)
	if err != nil || u.Scheme == "" || u.Scheme == "file" || u.Hostname() == "" {
		return domains
	}
	return append(domains, u.Hostname())
}

// claudeSubprocessEnv builds the CHILD claude process's environment as a
// CONSTRUCTED ALLOWLIST (Tier 1, see the SECURITY POSTURE doc block above) —
// replacing the former full-parent-env passthrough (which forwarded every
// secret/token exported into the archistrator-server process's OWN
// environment straight into an autonomous, permission-bypassed agent).
// EXACTLY these entries cross from the parent, each individually justified:
//
//   - PATH — required to resolve git/go/npm/... binaries this dispatch's own
//     `git` calls need, and that the sandboxed Bash tool's commands invoke
//     by name.
//   - HOME — required by git (global config lookup, credential helpers) and
//     most toolchains' cache/config directory resolution; without it even
//     basic subprocess tooling misbehaves.
//   - TERM — some CLI tooling (color detection, progress bars) misbehaves
//     under an unset TERM; carried through unchanged so subprocess output
//     stays sane for diagnostics.
//   - USER (and LOGNAME for parity) — the OS username. REQUIRED by claude's
//     headless SUBSCRIPTION-auth path: with no ANTHROPIC_API_KEY and no
//     ~/.claude/.credentials.json on the box, the operator's `claude` login
//     lives only in the OS login keychain, and that credential lookup is
//     USER-scoped — without USER the child claude reports "Not logged in ·
//     Please run /login" and EVERY local draft fails pre-work (empirically
//     bisected: stripped env → "Not logged in"; stripped+USER → authenticates).
//     The OS username is NOT a secret (same low-sensitivity class as HOME), so
//     forwarding it does not weaken the "no secrets to the autonomous agent"
//     posture the allowlist exists for. LOGNAME is the same value under a second
//     conventional name (some tools read it instead of USER); neither carries a
//     credential. This is the ONE var that separates a working local login from
//     a dead one.
//   - the AIARCH_* rig vars (rig, passed in from dispatch) — the SAME fixed
//     ambient-context envelope also carried on the attached aiarch-state MCP
//     server's OWN env (writeStateMCPConfig), so anything that inspects this
//     TOP-LEVEL process's env (rather than only the MCP server subprocess's)
//     sees identical values.
//
// Nothing else crosses from the parent: no ANTHROPIC_API_KEY (this local
// executor rides the operator's own `claude` OAuth session, never a
// forwarded key — same rationale as
// framework-go-infrastructure-llm/claudecli.go's claudeCLIEnv), no
// GITHUB_TOKEN, no cloud credentials, no arbitrary operator-exported
// secret. A stray secret exported into archistrator-server's own
// environment can no longer leak into an autonomous,
// --dangerously-skip-permissions agent process.
func claudeSubprocessEnv(rig map[string]string) []string {
	out := make([]string, 0, 5+len(rig))
	if v := os.Getenv("PATH"); v != "" {
		out = append(out, "PATH="+v)
	}
	if v := os.Getenv("HOME"); v != "" {
		out = append(out, "HOME="+v)
	}
	if v := os.Getenv("TERM"); v != "" {
		out = append(out, "TERM="+v)
	}
	// The OS username — required by claude's headless subscription-auth keychain
	// lookup (USER-scoped); LOGNAME forwards the same value for tools that read it
	// instead. Neither is a secret. See the doc comment's per-var justification.
	if v := os.Getenv("USER"); v != "" {
		out = append(out, "USER="+v)
	}
	if v := os.Getenv("LOGNAME"); v != "" {
		out = append(out, "LOGNAME="+v)
	}
	for k, v := range rig {
		out = append(out, k+"="+v)
	}
	return out
}

// ---------------------------------------------------------------------------
// git plumbing (raw `git` subprocess calls — no new library dependency; mirrors
// systemtests/internal/harness/localgit.go's style, production-hardened: errors
// are returned, never t.Fatalf'd).
// ---------------------------------------------------------------------------

// localMainBranch is the flat git-forward base every activity branch forks from —
// the SAME "main" convention constructactivity.go's mainBranch constant fixes.
const localMainBranch = "main"

// addWorktree adds a git worktree for `branch` at workDir, operating directly on the
// shared repo at repoPath. If the branch already EXISTS (a prior construct phase's
// activity branch, or a mid-session design branch a prior draft/critique/answer job
// opened) the worktree re-attaches to its tip. If it does NOT exist, the branch is
// created fresh off main. BOTH arms rely on this identical behavior.
//
// CREATE-OFF-MAIN IS THE LOCAL STAND-IN FOR THE CLOUD BRANCH-STAGING RAIL (the
// cloud-vs-local asymmetry). On the cloud rail a design session branch is created
// SERVER-SIDE before dispatch (systemdesign beginSession → sourceControlAccess.OpenBranch;
// the Action's refresh-from-main step then rebases it on origin/main); the construct rail
// likewise opens activity/<id> upstream. On the LOCAL profile BOTH those rails are DORMANT
// (sourceControlAccess is nil — "disabled session, no branch/PR ops"), so nothing stages
// the branch and the executor — the only git-owning party locally — creates it off main on
// first use. A fresh branch off main == the cloud's refreshed-from-main state, so the
// drafting/constructing agent sees the same committed base either way.
//
// EARMARK (NOT implemented — acceptable single-user-local gap): the cloud refresh ALSO
// merges origin/main into an EXISTING session branch on every job (stale-base heal / the
// F82 self-heal). The local arm does not re-merge main into an existing branch, so a
// long-lived local session branch can drift from a main that advanced under it. Fine for
// the single-developer-one-pump local profile; revisit if local mode grows concurrent
// writers or long-lived branches.
//
// Returns the branch's tip SHA at attach time (the rev-parse "before" anchor
// PhaseSucceeded's ref-advanced check compares against) and the shared repo's absolute
// git dir (the sandbox filesystem-scope entry — worktree commits write there).
func addWorktree(repoPath, branch, workDir string) (beforeSHA, gitDir string, err error) {
	if verifyBranchExists(repoPath, branch) {
		if _, aerr := runGit(repoPath, "worktree", "add", workDir, branch); aerr != nil {
			return "", "", aerr
		}
	} else if _, aerr := runGit(repoPath, "worktree", "add", "-b", branch, workDir, localMainBranch); aerr != nil {
		return "", "", aerr
	}
	beforeSHA, err = revParseBranch(repoPath, branch)
	if err != nil {
		return "", "", err
	}
	out, err := runGit(repoPath, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", "", err
	}
	return beforeSHA, strings.TrimSpace(out), nil
}

// verifyBranchExists reports whether refs/heads/<branch> resolves in the shared repo.
func verifyBranchExists(repoPath, branch string) bool {
	_, err := runGit(repoPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// revParseBranch resolves the branch's current tip SHA in the shared repo.
func revParseBranch(repoPath, branch string) (string, error) {
	out, err := runGit(repoPath, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// removeWorktree force-removes the worktree registration + directory from the
// shared repo. --force because the worktree may carry uncommitted debris a
// failed/cancelled run left behind — the commits that matter already live in
// the shared repo's refs.
func removeWorktree(repoPath, workDir string) error {
	_, err := runGit(repoPath, "worktree", "remove", "--force", workDir)
	return err
}

// ---------------------------------------------------------------------------
// Policy-gated local merge job (local-merge-and-policy Commit 1). The Manager —
// the ONLY policy authority (ReviewPolicy.EffectiveGate + the Task-7 risk
// floor) — dispatches this job through the frozen Submit surface via
// DispatchInputs["job"]="merge" once its gate has cleared; this RA only
// EXECUTES the merge, mirroring the division of labor the PR rail has in the
// cloud profile (interventionEngine/policy DECIDES, the executor PERFORMS).
// ---------------------------------------------------------------------------

// DispatchInputJobKey is the DispatchInputs key that selects a non-default job
// for a local-executor dispatch. Absent/empty means the normal construction
// dispatch (a headless claude run).
const DispatchInputJobKey = "job"

// DispatchJobMerge is the DispatchInputJobKey value selecting the local merge
// job: merge activity/<id> into the repo's default branch (--no-ff) and delete
// the activity branch. Local-executor realisation only; the Manager never sends
// it to the GitHub-Actions arm (the PR rail owns merges there).
const DispatchJobMerge = "merge"

// submitMergeJob converges the caller-supplied idempotencyKey on a single merge
// run (same in-memory convergence as the claude dispatch path) and performs the
// merge SYNCHRONOUSLY — a local git merge is fast and bounded, so by the time
// Submit returns the run is terminal and the Manager's first observe reads the
// outcome. A merge failure (conflict included) is a FAILED run with a
// diagnostic, not a Submit error: retrying the Submit would re-run a
// deterministic conflict pointlessly, while the recorded failure flows into the
// Manager's existing intervention path.
func (a *localExecAccess) submitMergeJob(rc fwra.Context, activityID string) (PipelineHandle, error) {
	token := dedupToken(rc.IdempotencyKey)
	handle := PipelineHandle(localHandlePrefix + token)

	a.mu.Lock()
	if existing, ok := a.runs[token]; ok {
		a.mu.Unlock()
		return existing.handle, nil
	}
	run := &localRun{handle: handle, status: localRunRunning}
	a.runs[token] = run
	a.mu.Unlock()

	diag, ok := a.mergeActivityBranch(localBranchName(activityID))

	run.mu.Lock()
	defer run.mu.Unlock()
	if run.status == localRunCancelled {
		return handle, nil
	}
	if ok {
		run.status = localRunSucceeded
	} else {
		run.status = localRunFailed
		run.diagnostic = diag
	}
	return handle, nil
}

// mergeActivityBranch merges the activity branch into the repo's default branch
// with a real --no-ff merge commit and deletes the branch, via a THROWAWAY
// CLONE + push — the SAME write mechanism the projectstate GitStore uses for
// every server-side git write, so a non-bare shared repo (with
// receive.denyCurrentBranch=updateInstead) has its checked-out working tree
// updated by git's own receive machinery rather than left stale by a raw
// update-ref. Nothing is pushed unless the merge completed cleanly: a conflict
// aborts in the clone and the shared repo is untouched (no partial merge, ever).
// Idempotent under retry: an activity branch already an ancestor of the default
// branch (a prior attempt merged but crashed before deleting) skips straight to
// the branch delete. Returns ok=false with an operator-readable diagnostic on
// any failure.
func (a *localExecAccess) mergeActivityBranch(branch string) (diagnostic string, ok bool) {
	a.gitMu.Lock()
	defer a.gitMu.Unlock()

	parentDir, err := os.MkdirTemp("", "aiarch-merge-*")
	if err != nil {
		return "local merge: create work dir: " + err.Error(), false
	}
	defer func() { _ = os.RemoveAll(parentDir) }()
	cloneDir := filepath.Join(parentDir, "clone")

	if out, err := runGit("", "clone", "--branch", localMainBranch, a.repoURL, cloneDir); err != nil {
		return "local merge: clone failed: " + outputTail(out, 500), false
	}
	remoteBranch := "origin/" + branch
	if _, err := runGit(cloneDir, "rev-parse", "--verify", "--quiet", "refs/remotes/"+remoteBranch); err != nil {
		return "local merge: activity branch " + branch + " not found in the shared repo", false
	}

	// Already merged (a prior attempt landed the merge but failed before the
	// branch delete)? Skip straight to the delete — no second merge commit.
	if _, err := runGit(cloneDir, "merge-base", "--is-ancestor", remoteBranch, "HEAD"); err == nil {
		if out, derr := runGit(cloneDir, "push", "origin", "--delete", branch); derr != nil {
			return "local merge: branch " + branch + " already merged but could not be deleted: " + outputTail(out, 500), false
		}
		return "", true
	}

	// The merge commit needs an author identity independent of whatever global
	// config the host machine carries — pin one explicitly (-c), same spirit as
	// the workflow agent's own bot identity.
	mergeOut, err := runGit(cloneDir,
		"-c", "user.name=aiarch", "-c", "user.email=aiarch@local",
		"merge", "--no-ff", "-m", "aiarch: merge "+branch, remoteBranch)
	if err != nil {
		// Abort cleanly (best-effort — a conflict leaves MERGE_HEAD in the clone;
		// the clone is throwaway either way) and surface the conflict through the
		// failure diagnostic. NOTHING was pushed: the shared repo is untouched.
		_, _ = runGit(cloneDir, "merge", "--abort")
		if strings.Contains(mergeOut, "CONFLICT") {
			return "local merge: merge conflict merging " + branch + " into " + localMainBranch + ": " + outputTail(mergeOut, 500), false
		}
		return "local merge: merge of " + branch + " failed: " + outputTail(mergeOut, 500), false
	}
	if out, err := runGit(cloneDir, "push", "origin", localMainBranch); err != nil {
		return "local merge: push of merged " + localMainBranch + " failed: " + outputTail(out, 500), false
	}
	if out, err := runGit(cloneDir, "push", "origin", "--delete", branch); err != nil {
		// The merge IS landed; a retry takes the already-merged path above and
		// re-attempts only the delete.
		return "local merge: merged but branch " + branch + " could not be deleted: " + outputTail(out, 500), false
	}
	return "", true
}

// runGit runs `git <args...>` with the given working directory (ignored when
// empty — used for the initial clone, which has no working dir of its own yet)
// and returns its combined output wrapped into the error on failure, for a
// debuggable diagnostic.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...) //nolint:gosec // fixed trusted binary, internally-derived args (repo URL/branch names), mirrors systemtests/internal/harness/localgit.go
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// ---------------------------------------------------------------------------
// --mcp-config authoring — the SAME envelope shape aiarch-construct.yml's "Write
// the aiarch-state MCP config" step emits, over the SAME AIARCH_* env vocabulary
// cmd/aiarch-state-mcp/session.go reads.
// ---------------------------------------------------------------------------

// mcpServerConfigJSON / mcpConfigFileJSON mirror the minimal shape `claude
// --mcp-config` expects: {"mcpServers": {"<name>": {"command": ..., "env": {...}}}}.
type mcpServerConfigJSON struct {
	Command string            `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
}

type mcpConfigFileJSON struct {
	MCPServers map[string]mcpServerConfigJSON `json:"mcpServers"`
}

// writeStateMCPConfig writes the ephemeral --mcp-config file OUTSIDE the cloned
// working tree (in dir, a SEPARATE temp dir from workDir) so an agent running
// `git add -A` inside the repo clone can never accidentally pick it up. rig is
// the SAME fixed AIARCH_* envelope dispatch also stamps on claude's own process
// env (claudeSubprocessEnv), mirroring the seated workflow's MCP-config env for
// whichever arm built it:
//   - CONSTRUCT (aiarch-construct.yml): AIARCH_JOB_MODE=construct + AIARCH_COMPONENT_ID/
//     AIARCH_ACTIVITY_ID from the dispatch (constructDispatchPlan).
//   - DESIGN (aiarch-design.yml): AIARCH_JOB_MODE=<draft|critique|answer> +
//     AIARCH_ARTIFACT_KIND (designDispatchPlan).
//
// Both carry AIARCH_PROJECT_ID + AIARCH_TARGET_BRANCH (the branch this realisation just
// checked out) and AIARCH_STATE_ROOT pointed at the worktree (workDir) so
// cmd/aiarch-state-mcp reads/writes the SAME .aiarch/state/project.json claude's own git
// commits land next to.
func writeStateMCPConfig(dir, stateMCPBin string, rig map[string]string) (string, error) {
	cfg := mcpConfigFileJSON{MCPServers: map[string]mcpServerConfigJSON{
		"aiarch-state": {
			Command: stateMCPBin,
			Env:     rig,
		},
	}}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fwra.Wrap(fwra.Infrastructure, err, "localexec: marshal mcp config")
	}
	f, err := os.CreateTemp(dir, "aiarch-mcp-*.json")
	if err != nil {
		return "", fwra.Wrap(fwra.Infrastructure, err, "localexec: create mcp config file")
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(body); err != nil {
		return "", fwra.Wrap(fwra.Infrastructure, err, "localexec: write mcp config file")
	}
	return f.Name(), nil
}
