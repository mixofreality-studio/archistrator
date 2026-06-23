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
	"strconv"
	"strings"
	"time"

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

// Access is the concrete, GitHub-Actions-backed implementation of the
// ConstructionPipelineAccess port (constructionPipelineAccess.md §6). It derives a
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
type Access struct {
	client ghActionsClient
	// resolveAttempts / resolveDelay bound the post-dispatch run-resolution poll
	// (GitHub creates the run asynchronously after a 204 dispatch). Defaults applied
	// in New when zero; tests may inject a faster clock-free fake whose dispatch
	// creates the run synchronously (one attempt suffices).
	resolveAttempts int
	resolveDelay    time.Duration
}

// compile-time proof the concrete impl satisfies the port.
var _ ConstructionPipelineAccess = (*Access)(nil)

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

// New builds an Access over the supplied GitHub-Actions client seam. The
// composition root (cmd/server/main.go) constructs the concrete seam via
// NewActionsClient (which carries the App identity + the target repo + workflow
// file) and passes it here; tests pass a fake ghActionsClient.
//
// CONSTRUCTOR SIGNATURE FOR main.go WIRING:
//
//	seam, err := constructionpipeline.NewActionsClient(constructionpipeline.ActionsConfig{
//	    AppID:         cfg.GitHubAppID,
//	    PrivateKeyPEM: cfg.GitHubAppPrivateKeyPEM,
//	    APIBaseURL:    cfg.GitHubAPIBaseURL,   // "" == github.com
//	    Owner:         cfg.ConstructionRepoOwner,
//	    Repo:          cfg.ConstructionRepoName,
//	    WorkflowFile:  cfg.ConstructionWorkflowFile, // e.g. "aiarch-construct.yml"
//	    Ref:           cfg.ConstructionRef,          // e.g. "main"
//	})
//	cp, err := constructionpipeline.New(seam)
func New(client ghActionsClient) (*Access, error) {
	if client == nil {
		return nil, fwra.New(fwra.ContractMisuse, "constructionpipeline.New: nil actions client")
	}
	return &Access{
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
func (a *Access) SubmitConstructionPipeline(ctx context.Context, spec PipelineSpec, idempotencyKey fwra.IdempotencyKey) (PipelineHandle, error) {
	if idempotencyKey.IsZero() {
		return PipelineHandle{}, fwra.New(fwra.ContractMisuse, "SubmitConstructionPipeline: empty idempotencyKey")
	}
	if err := validateSpec(spec); err != nil {
		return PipelineHandle{}, err
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
		return PipelineHandle{}, err
	}
	if live := liveOrSucceeded(existing); len(live) > 0 {
		return a.converge(ctx, tgt, live)
	}

	// 2. DISPATCH — no run yet for this key. The spec's optional DispatchInputs ride
	//    into the runtime's input map; the RA-controlled idempotency token is merged
	//    in LAST by the seam, so it wins any collision (stays RA-controlled).
	if err := a.client.dispatch(ctx, tgt, token, runName, spec.DispatchInputs); err != nil {
		return PipelineHandle{}, err
	}

	// 3. RESOLVE — GitHub creates the run asynchronously after a 204; poll (bounded)
	//    until the run carrying our run-name appears.
	runs, err := a.resolveAfterDispatch(ctx, tgt, runName)
	if err != nil {
		return PipelineHandle{}, err
	}
	if len(runs) == 0 {
		// Dispatched but the run never surfaced within the resolve window — transient
		// (GitHub may still be creating it); the Manager retries the whole submit,
		// which is idempotent (the probe will then find it).
		return PipelineHandle{}, fwra.New(fwra.Transient, "SubmitConstructionPipeline: dispatched run did not surface within resolve window")
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
func (a *Access) converge(ctx context.Context, tgt ghTarget, runs []ghRun) (PipelineHandle, error) {
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
			return PipelineHandle{}, err
		}
	}
	return a.handleFor(tgt, canonical.id), nil
}

// resolveAfterDispatch polls listRunsByName until the dispatched run appears or the
// bounded attempt budget is exhausted (GitHub's dispatch→run-creation is eventually
// consistent). The fake's dispatch creates the run synchronously, so one attempt
// resolves it in tests; production uses the default budget.
func (a *Access) resolveAfterDispatch(ctx context.Context, tgt ghTarget, runName string) ([]ghRun, error) {
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
func (a *Access) ObserveConstructionPipeline(ctx context.Context, handle PipelineHandle) (PipelineObservation, error) {
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
func (a *Access) CancelConstructionPipeline(ctx context.Context, handle PipelineHandle) error {
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
func (a *Access) handleFor(tgt ghTarget, runID int64) PipelineHandle {
	base := "run/" + strconv.FormatInt(runID, 10)
	if tgt.isZero() {
		return PipelineHandle{opaque: base}
	}
	return PipelineHandle{opaque: base + handleSep + tgt.owner + "/" + tgt.repo + "/" + tgt.workflowFile}
}

// runIDFromHandle unpacks the run id AND the OPTIONAL per-project target from an
// opaque handle. A zero/malformed handle is a caller pre-condition violation →
// fwra.ContractMisuse. A handle with no "@<target>" segment returns a ZERO ghTarget
// (the construction-repo default — the seam substitutes its configured repo).
func (a *Access) runIDFromHandle(handle PipelineHandle) (int64, ghTarget, error) { //nolint:gocyclo // parses handle segments; each segment type adds a branch
	if handle.IsZero() {
		return 0, ghTarget{}, fwra.New(fwra.ContractMisuse, "constructionpipeline: zero PipelineHandle")
	}
	runPart, targetPart, hasTarget := strings.Cut(handle.opaque, handleSep)
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
