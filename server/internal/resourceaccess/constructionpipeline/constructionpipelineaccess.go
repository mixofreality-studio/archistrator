package constructionpipeline

// actions.go is the GITHUB-ACTIONS-backed realisation of the
// ConstructionPipelineAccess port (constructionPipelineAccess.md §6 infrastructure
// mapping) — the C-CP-R rework that swapped the construction-pipeline runtime from
// Argo Workflows on Kubernetes to the USER'S GitHub Actions (the 2026-06-09 pivot:
// the user's GitHub + Actions, no Argo). It REPLACES the former argo.go /
// argo_http_client.go.
//
// THE LOAD-BEARING LAYER RULE is unchanged from the frozen contract: this RA's
// PUBLIC surface (constructionpipeline.go) carries ZERO GitHub-Actions lexemes
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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

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
	// place the github.com lexeme lives). Surfaced on a terminal-failure observation so
	// the Manager can deep-link the operator to the failed run (QA F15 gap 2b). Empty
	// when the realisation cannot resolve it (e.g. the fake in a test that does not set it).
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
	// design-dispatch inputs — constructionPipelineAccess.md §0d.6). The seam merges the
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
// ConstructionPipelineAccess port (constructionPipelineAccess.md §6). It is
// UNEXPORTED — the package's only public surface is the generated
// ConstructionPipelineAccess interface + models + the generated
// NewGitHubActionsConstructionPipelineAccess constructor (plus the value-type
// behaviour free functions). It derives a
// deterministic dedup token + run name from the caller-supplied idempotencyKey,
// converges concurrent submits on the lowest-run-id canonical run, and maps a run's
// status+conclusion back to an infrastructure-neutral PipelineObservation.
//
// The struct imports NO Temporal (layer rule, constructionPipelineAccess.md §2):
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
var _ ConstructionPipelineAccess = (*access)(nil)

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
// the hand-written core both the generated NewGitHubActionsConstructionPipelineAccess
// constructor (via newGitHubActionsConstructionPipelineAccess, which wires the
// concrete ghActionsRESTClient seam over the App identity) and the in-package tests
// (which pass a fake ghActionsClient) build through. Returns the concrete *access so
// the in-package tests can tune resolveAttempts/resolveDelay; the public path returns
// the ConstructionPipelineAccess interface.
func newAccess(client ghActionsClient) (*access, error) {
	if client == nil {
		return nil, fwra.New(fwra.ContractMisuse, "constructionpipeline.NewGitHubActionsConstructionPipelineAccess: nil actions client")
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

// SubmitConstructionPipeline converges the caller-supplied idempotencyKey on a
// single canonical GitHub Actions run and returns its handle (non-blocking on
// completion). Re-submitting the same key returns the SAME handle without launching
// a second effective run (constructionPipelineAccess.md §2.1). The convergence
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
func (a *access) SubmitConstructionPipeline(rc fwra.Context, spec PipelineSpec) (PipelineHandle, error) {
	// The cross-cutting ctx + idempotencyKey now ride the ResourceAccess call Context
	// (fwra.Context embeds context.Context and carries the caller-supplied
	// IdempotencyKey); the package still never reads Temporal — the key is an ordinary
	// value carried on rc, exactly as before. This keeps the component Temporal-free.
	ctx := rc.Context
	idempotencyKey := rc.IdempotencyKey
	if idempotencyKey.IsZero() {
		return "", fwra.New(fwra.ContractMisuse, "SubmitConstructionPipeline: empty idempotencyKey")
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
		return "", fwra.New(fwra.Transient, "SubmitConstructionPipeline: dispatched run did not surface within resolve window")
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

// ObserveConstructionPipeline reads the canonical run's status+conclusion and maps
// it to an infrastructure-neutral PipelineObservation
// (constructionPipelineAccess.md §2.2). Pure read; no side effects. An unknown /
// GC'd handle surfaces as fwra.NotFound.
func (a *access) ObserveConstructionPipeline(rc fwra.Context, handle PipelineHandle) (PipelineObservation, error) {
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

// CancelConstructionPipeline requests cancellation of the canonical run. Cancelling
// an already-terminal / already-cancelled / unknown run is a no-op SUCCESS — the
// desired post-condition ("no further steps will start") already holds, which makes
// cancel safe to retry against the operator-pause race
// (constructionPipelineAccess.md §2.3). The seam maps GitHub's 409/404 to success.
func (a *access) CancelConstructionPipeline(rc fwra.Context, handle PipelineHandle) error {
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
		return 0, ghTarget{}, fwra.New(fwra.ContractMisuse, "constructionpipeline: zero PipelineHandle")
	}
	runPart, targetPart, hasTarget := strings.Cut(string(handle), handleSep)
	kind, rest, ok := strings.Cut(runPart, "/")
	if !ok || kind != "run" || rest == "" {
		return 0, ghTarget{}, fwra.New(fwra.ContractMisuse, "constructionpipeline: malformed PipelineHandle")
	}
	id, perr := strconv.ParseInt(rest, 10, 64)
	if perr != nil {
		return 0, ghTarget{}, fwra.New(fwra.ContractMisuse, "constructionpipeline: malformed PipelineHandle run id")
	}
	if !hasTarget {
		return id, ghTarget{}, nil
	}
	// "<owner>/<repo>/<workflowFile>" — split into exactly three non-empty parts.
	owner, restTarget, ok1 := strings.Cut(targetPart, "/")
	repo, workflowFile, ok2 := strings.Cut(restTarget, "/")
	if !ok1 || !ok2 || owner == "" || repo == "" || workflowFile == "" {
		return 0, ghTarget{}, fwra.New(fwra.ContractMisuse, "constructionpipeline: malformed PipelineHandle target")
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
// infrastructure-neutral PipelineObservation (constructionPipelineAccess.md §6).
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
	// On any terminal non-success (Failed / Cancelled) surface the run's URL as the
	// operator's "why" pointer (QA F15 gap 2b) — the deep-link the Manager threads onto
	// the failed card. Empty when the realisation could not resolve it.
	if obs.Phase == PhaseFailed || obs.Phase == PhaseCancelled {
		obs.RunURL = run.htmlURL
	}
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
// Manager's intervention decision (constructionPipelineAccess.md §2.2 / Non-goal #4
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

// ---- from actions_http_client.go ----

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

// newGitHubActionsConstructionPipelineAccess is the hand-written, unexported builder
// behind the generated NewGitHubActionsConstructionPipelineAccess constructor
// (option-1 delegated DI). It wires the token-caching ghActionsRESTClient seam over
// the framework *fwgithub.AppClient + the repo/workflow config, then the access impl,
// returning the ConstructionPipelineAccess interface so the concrete impl + its seam
// stay unexported. The composition root (cmd/server/main.go) builds the App client via
// fwgithub.NewAppClient and passes it here.
func newGitHubActionsConstructionPipelineAccess(app *fwgithub.AppClient, owner, repo, workflowFile, ref string, installationID int64) (ConstructionPipelineAccess, error) {
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
		return nil, fwra.New(fwra.ContractMisuse, "constructionpipeline: nil github app client")
	}
	if strings.TrimSpace(owner) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "constructionpipeline: empty Owner")
	}
	if strings.TrimSpace(repo) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "constructionpipeline: empty Repo")
	}
	if strings.TrimSpace(workflowFile) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "constructionpipeline: empty WorkflowFile")
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
	// run-name anchor stays RA-controlled — constructionPipelineAccess.md §0d.6).
	inputs := make(map[string]string, len(dispatchInputs)+1)
	for k, v := range dispatchInputs {
		inputs[k] = v
	}
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

// ---- from behavior.go ----

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

// PipelinePhaseString returns the stable name (logs, audit). Replaces the former
// PipelinePhase.String() method (the generated contract type carries no methods).
func PipelinePhaseString(p PipelinePhase) string {
	if n, ok := phaseNames[p]; ok {
		return n
	}
	return "Pending"
}

// PipelinePhaseIsTerminal reports whether the phase is one a running pipeline can no
// longer leave (Succeeded / Failed / Cancelled). Cancelling or re-observing a
// terminal pipeline is stable. Replaces the former PipelinePhase.IsTerminal() method.
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

// StepOutcomeString returns the stable name (logs, audit). Replaces the former
// StepOutcome.String() method.
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
// fall-back-to-default case). Replaces the former RepoTarget.IsZero() method.
func RepoTargetIsZero(t RepoTarget) bool { return t.Owner == "" && t.Name == "" }

// ---- from constructionpipeline.go ----
// Package constructionpipeline is the constructionPipelineAccess component of the
// aiarch server's ResourceAccess layer — the INFRASTRUCTURE-OPAQUE port over the
// construction-task face of WorkflowRuntime volatility
// (constructionPipelineAccess.md). It is the only component permitted to call the
// constructionPipelineRuntime Resource (architecture.dsl line 284).
//
// THE LOAD-BEARING LAYER RULE (constructionPipelineAccess.md §1, §3;
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
// Idempotency on the write verb (SubmitConstructionPipeline) is carried by a
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

// ConstructionPipelineAccess is the infrastructure-opaque port over the
// containerised construction-pipeline runtime (constructionPipelineAccess.md §2).
// Three atomic verbs, every one importing no Temporal:
//
//   - SubmitConstructionPipeline — submit one construction pipeline (compile /
//     test / lint / package / …) and return its handle. It does NOT block for the
//     pipeline to finish (a multi-minute-to-hour run); the pipeline runs
//     asynchronously on the infrastructure. Deterministic on the caller-supplied
//     idempotencyKey: re-submitting with the same key converges on the SAME handle
//     (the infrastructure rejects the duplicate name and "already exists" is mapped
//     to success returning the existing handle).
//   - ObserveConstructionPipeline — pull-shaped, side-effect-free point-in-time
//     read of a pipeline's lifecycle phase, per-step outcomes, and (on terminal
//     failure) the failing step + an infrastructure-neutral diagnostic. An unknown
//     / GC'd handle surfaces as fwra.NotFound.
//   - CancelConstructionPipeline — idempotent-on-intent cancel. Cancelling a
//     terminal / already-cancelled / unknown pipeline is a no-op SUCCESS (the
//     desired post-condition — "no further steps will start" — already holds),
//     which makes cancel safe to retry against the operator-pause race.

// ProjectID is the logical project a construction pipeline serves
// (constructionPipelineAccess.md §3). Infrastructure-opaque string identity; the
// package never parses it.

// ConstructionActivityID is the construction activity this pipeline serves
// (constructionPipelineAccess.md §3). Infrastructure-opaque.

// ArtifactRef is the opaque reference to the input tree the pipeline materialises
// into its workspace (constructionPipelineAccess.md §3). It is produced by
// artifactAccess and treated as opaque here — this contract carries pipeline
// OUTCOME, never artifact bytes (Non-goal #3); inputs flow in by reference.

// ToolchainRef is a LOGICAL toolchain identity, e.g. "go-1.23", "node-20"
// (constructionPipelineAccess.md §3). The infrastructure mapping resolves it (the
// GitHub-Actions runtime realises it inside the dispatched construction workflow);
// callers never name an image.

// ResourceRequest is a LOGICAL CPU/mem/GPU request for a step
// (constructionPipelineAccess.md §3). The infrastructure mapping translates it to
// the runtime's own resource model; callers never name a runtime-specific quantity.

// CPUMillis is the requested CPU in milli-cores (e.g. 500 == half a core); 0
// lets the infrastructure apply its default.

// MemMiB is the requested memory in MiB; 0 lets the infrastructure default.

// GPUs is the requested GPU count; 0 == none.

// PipelineStep is one logical step in the construction pipeline
// (constructionPipelineAccess.md §3). Infrastructure-neutral: it names a logical
// toolchain and a command, not a container image or a runtime manifest fragment.

// Name is the logical step name: "compile", "test", "lint", "package", …. It
// is the join key for StepDependency.From/To and is echoed back on
// StepObservation.Name. Must be unique within a PipelineSpec.

// Toolchain is the logical toolchain identity the step runs under.

// Command is the command (argv) to run inside the step container.

// Resources is the logical resource request for the step.

// CacheKeys are the logical build-cache keys this step reads/writes (the only
// cache knob exposed; the infrastructure maps them to cache volumes —
// constructionPipelineAccess.md Non-goal #5).

// StepDependency is a step-to-step ordering edge (To runs after From), forming a
// DAG over PipelineSpec.Steps (constructionPipelineAccess.md §3). An empty Edges
// slice means LINEAR execution over Steps in order — the simple case is free.

// From is the upstream PipelineStep.Name.

// To is the downstream PipelineStep.Name (runs after From).

// PipelineSpec is the infrastructure-neutral description of the construction
// pipeline to run (constructionPipelineAccess.md §3). It is a LOGICAL DAG, never a
// runtime manifest; the package maps it to the runtime internally (the
// GitHub-Actions realisation triggers the user's aiarch construction workflow for
// the activity — actions.go). The Argo realisation translated the same PipelineSpec
// to an Argo Workflow manifest; a future Tekton/hosted-CI runtime would translate
// the same PipelineSpec unchanged. This is the ResourceAccess volatility promise.

// ProjectID is the project this pipeline serves.

// ActivityID is the construction activity this pipeline serves.

// Steps is the set of pipeline steps (non-empty; each names a resolvable
// toolchain and a command).

// Edges is the step-to-step DAG; empty == linear over Steps.

// WorkspaceRef is the input tree the pipeline materialises into the workspace
// (opaque, from artifactAccess).

// DispatchInputs is an OPTIONAL, infrastructure-neutral bag of EXTRA
// dispatch-time inputs the runtime forwards into the launched job alongside the
// RA-controlled idempotency token (constructionPipelineAccess.md §0d.6 — the
// additive D-MSD-Δ flag). It is ADDITIVE and defaulted-empty: the existing
// construction caller (UC3) leaves it nil and is untouched. The DESIGN-dispatch
// caller (the UC1/UC2 design Managers — a NEW caller of the FROZEN submit verb)
// populates it with the agentic DESIGN job's parameters:
//   {"artifact_kind", "design_prompt", "target_branch", "prior_state_ref"}
// (the exact workflow_dispatch input names the aiarch-design.yml template
// declares — C-WF-DESIGN). These keys ride into the runtime's input map.
//
// RA-CONTROLLED IDEMPOTENCY TOKEN IS RESERVED. The RA continues to reserve and
// stamp the idempotency-token input ITSELF (derived from the caller-supplied
// idempotencyKey). DispatchInputs MUST NOT carry the idempotency-token key; if
// it does, the RA's value WINS (the RA merges the token in LAST, overwriting any
// caller-supplied collision) so the load-bearing dedup/run-name anchor can never
// be spoofed through this additive field. Keys are passed through verbatim
// otherwise; the package does not parse or validate their values.

// TargetRepo is an OPTIONAL, infrastructure-neutral per-call override of the repo
// the pipeline dispatches to / is observed/cancelled in (the additive
// per-project-design-dispatch field, sibling to DispatchInputs). It is ADDITIVE and
// defaulted-zero: the existing UC3 construction caller leaves it zero and dispatches
// to the configured CONSTRUCTION repo + workflow file (zero change). The DESIGN
// caller (the UC1/UC2 design Managers) sets it to the PER-PROJECT repo so the
// agentic DESIGN job runs in the user's own repo (where aiarch-design.yml was
// committed at project birth), NOT the central construction repo. The owner/repo
// are LOGICAL coordinates (a user/org login + a repo name); the package never parses
// them — the seam realisation maps them to the provider's address.
//
// HANDLE SELF-DESCRIPTION. A non-zero TargetRepo (and WorkflowFile) is ENCODED into
// the returned PipelineHandle, so a later Observe/Cancel re-addresses the SAME
// per-project repo + workflow even though those verbs carry only the handle (the
// run-name dedup anchor + observe/cancel must poll the per-project repo's runs, not
// the construction repo's). A zero TargetRepo encodes the legacy "run/<id>" handle
// (the construction repo is the Access's configured default), so existing UC3
// handles round-trip byte-identically.

// WorkflowFile is an OPTIONAL per-call override of the workflow file the pipeline
// dispatches (e.g. "aiarch-design.yml"). ADDITIVE and defaulted-empty: empty ⇒ the
// Access's configured construction workflow file ("aiarch-construct.yml"). The
// DESIGN caller sets it to the design workflow file so the per-project repo's
// aiarch-design.yml is dispatched, not aiarch-construct.yml.

// RepoTarget is the OPTIONAL, infrastructure-neutral per-call repo override on
// PipelineSpec (the additive per-project-design-dispatch field). Owner is the
// user/org login; Name is the repo name. Both empty == "no override" (fall back to
// the Access's configured construction repo). The package treats these as logical
// coordinates and never parses them; the seam realisation addresses the provider.

// Owner is the repo owner (user or org login).

// Name is the repo name.

// PipelineHandle is an OPAQUE, immutable identity for one submitted construction
// pipeline (constructionPipelineAccess.md §3). Callers compare by value and never
// parse it; a Manager that records a pipeline reference in head-state persists its
// string value, never an infrastructure id. Infrastructure-opaque: today it wraps
// the canonical GitHub Actions run id internally ("run/<id>"), never exposed as such.
//
// It is a NAMED SCALAR (the established "behavioral value type → generated scalar +
// free functions" pattern, same as artifactAccess's OutputPath): the codegen
// represents it cleanly as a $def named scalar, and its behaviour lives in
// behavior.go as free functions (PipelineHandleString / ParsePipelineHandle /
// PipelineHandleEqual / PipelineHandleIsZero). The opaque token the impl packs
// ("run/<id>" / "run/<id>@owner/repo/wf") IS the string value.

// PipelinePhase is the infrastructure-neutral lifecycle phase of a pipeline
// (constructionPipelineAccess.md §3).

// PhasePending — submitted, not yet started.

// PhaseRunning — one or more steps in flight.

// PhaseSucceeded — all steps succeeded (terminal).

// PhaseFailed — a step failed (terminal).

// PhaseCancelled — cancelled via CancelConstructionPipeline (terminal).

// StepOutcome is the infrastructure-neutral outcome of a single step
// (constructionPipelineAccess.md §3).

// StepPending — the step has not started.

// StepRunning — the step is in flight.

// StepSucceeded — the step completed successfully.

// StepFailed — the step failed.

// StepSkipped — the step was skipped (e.g. an upstream failed).

// StepObservation is the per-step outcome inside a PipelineObservation
// (constructionPipelineAccess.md §3).

// Name is the logical step name (matches a PipelineStep.Name).

// Outcome is the step's infrastructure-neutral outcome.

// PipelineObservation is a point-in-time, infrastructure-neutral view of a
// pipeline's progress (constructionPipelineAccess.md §3). It carries OUTCOME, not
// artifacts — the pipeline's produced bytes are staged to the artifact store by
// the Manager via artifactAccess, not transported here (Non-goal #3).

// Handle is the pipeline this observation describes.

// Phase is the lifecycle phase.

// Steps is the per-step outcomes (in spec order).

// FailedStep names the first failing step; empty unless Phase == PhaseFailed.

// Diagnostic is an infrastructure-neutral failure summary (NOT raw logs —
// Non-goal #4); empty on success.

// StartedAt is when the pipeline started; zero while still Pending.

// FinishedAt is when the pipeline reached a terminal phase; nil while running.

// Error is the shared ResourceAccess error model (framework-go), re-exported as an
// alias so this component's contract reads in its own terms while every RA
// component shares one fixed enum. Construct with fwra.New / fwra.Wrap using the
// shared kinds. The contract's logical error vocabulary
// (constructionPipelineAccess.md §3 PipelineAccessError) maps onto the shared
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
// Manager escalates to interventionEngine, constructionPipelineAccess.md §6 OQ4)
// maps to fwra.QuotaExhausted, whose DefaultRetryable() is false — preserving the
// "non-retryable + escalate" classification the senior review confirmed.
type Error = fwra.Error

// ---- from variant.go ----

// variant.go holds the DRY-RUN variant stub for constructionPipelineAccess — the
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
// NewGitHubActionsConstructionPipelineAccess (contract.gen.go); the composition root
// builds the shared *fwgithub.AppClient satellite and passes it in.

// NewDryRunConstructionPipelineAccess returns the in-memory dry-run pipeline stub.
func NewDryRunConstructionPipelineAccess() ConstructionPipelineAccess {
	return dryRunPipeline{}
}

type dryRunPipeline struct{}

var _ ConstructionPipelineAccess = dryRunPipeline{}

func (dryRunPipeline) SubmitConstructionPipeline(_ fwra.Context, spec PipelineSpec) (PipelineHandle, error) {
	return PipelineHandle("dryrun:" + string(spec.ActivityID)), nil
}

func (dryRunPipeline) ObserveConstructionPipeline(_ fwra.Context, handle PipelineHandle) (PipelineObservation, error) {
	return PipelineObservation{Handle: handle, Phase: PhaseSucceeded}, nil
}

func (dryRunPipeline) CancelConstructionPipeline(_ fwra.Context, _ PipelineHandle) error {
	return nil
}
