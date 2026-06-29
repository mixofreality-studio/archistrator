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
package constructionpipeline

import (
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

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
