// Package delivery is the deliveryManager component of the aiarch server's
// Manager layer — the ONE Manager of the Project Delivery Workflow volatility
// (B-02, B-03, B-04, B-13, B-14). It replaces internal/manager/systemdesign,
// internal/manager/projectdesign and internal/manager/construction, which were
// three choreographies of one workflow: an activity of the committed project
// network becomes eligible, an agent produces its artifact, agents and humans
// review it, and the activity advances through its own task DAG.
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal / Query), it defines and registers one Activity
// per ResourceAccess call, owns the Signal/Query handlers, and derives the
// idempotency key "${workflowId}:${activityId}" passed down to each RA verb.
// Temporal lives ONLY in this component; the downstream Engines and
// ResourceAccess ports are Temporal-free.
//
// SCHEMA-FIRST (full encapsulation): this component OWNS its contract I/O types.
// The public surface (DeliveryManager port + the I/O value types) is GENERATED
// into contract.gen.go from this component's `.serviceContracts.deliveryManager`
// entry in .aiarch/state/project.json (edit that entry + `make gen`; do NOT
// hand-edit the generated surface).
//
// STAGE 4a IS A MOVE, NOT A REWRITE. The three rails' bodies are here verbatim,
// each under a banner naming the file it came from, and the twelve contract ops
// are a THIN DISPATCHER over the forty implementations they already had. The
// only edits the merge forced are:
//   - the framework-go/manager import alias, which the construction rail spelled
//     `fwm` and the two design rails `fwmanager`; one file can carry one alias,
//     so the construction block reads `fwmanager` here;
//   - the package-private name collisions: byte-identical twins collapsed to one
//     copy (marked in place), everything whose body differed prefixed by rail
//     (`sd` / `pd` / `cs`) — the full 199-row rename table, so a reader of this file
//     can find the symbol the pre-merge blame names, is tracked at
//     docs/superpowers/plans/2026-09-25-stage4a-rename-table.md.
//
// Nineteen Temporal replay fixtures assert that nothing else changed.
//
// File layout within the package (arch.CheckFileLayout, framework-go/arch):
//   - deliverymanager.go  : the Manager + the twelve-op dispatcher (the ONE impl file)
//   - contract.gen.go     : the generated public façade (port + I/O value types)
//   - activities.gen.go / invokers.gen.go / worker.gen.go : the generated Temporal surface
//   - <workflow>.go       : exactly one file per registered workflow entry function
//   - manager_test.go     : the ONE test file the layout rule allows
package delivery

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"maps"
	"math"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	billing "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/designhealth"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// ---------------------------------------------------------------------------
// SYSTEM-DESIGN RAIL — moved verbatim from internal/manager/systemdesign/
// systemdesignmanager.go at stage 4a. Bodies are unchanged; only package-private
// names that collided with another rail were renamed (the collision table is in
// docs/superpowers/plans/2026-09-25-activity-experience-stage4a.md, Task 6 Step 2b,
// and in this commit's message). 4b replaces this block with the generic DAG child.
// ---------------------------------------------------------------------------

// isResearchReadNotFound reports whether a ReadProject error is the brand-new
// project NotFound (no row yet) — which, for StartSystemDesign, is itself a
// FailedPrecondition (research not set), not an infrastructure fault.
func isResearchReadNotFound(err error) bool {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		return raErr.Kind == fwra.NotFound
	}
	return false
}

// requestArtifactDraft — op 2.1. Temporal SIGNAL-WITH-START on workflow id
// {projectId}:{artifactKind}. This is BOTH the first-draft kickoff AND the
// "Retry draft" recovery lever:
//
//   - First request (no running session): starts the CoAuthorArtifactWorkflow,
//     which drafts immediately. The buffered ride-along redraft signal is harmless:
//     the fresh run does not await a recovery gate before it drafts, and the
//     failed-gate entry DRAIN discards it if the first draft fails (it must never
//     auto-consume the human gate — QA incident 2026-07-15).
//   - Retry on a REFUSED session (Bug B; the session ended a draft attempt in the
//     queryable StageRefused state after a terminal worker fault): the redraft
//     signal is delivered to the still-live, suspended workflow, which re-enters the
//     draft loop in place — no new workflow run, the getSessionState Query stays
//     continuously available.
//   - A session that is currently DRAFTING/REDRAFTING is NOT receptive: the request
//     is refused with FailedPrecondition (checkDraftRequestReceptive) — a signal
//     sent then would buffer and later stale-consume a recovery gate.
//
// Signal-with-start is the one call that covers all three (start-if-absent, signal
// the existing run otherwise), preserving the §2.1 idempotent-on-id post-condition.
//
// RequestArtifactDraft is the exported public op.
// amendmentIndexFor PROMOTED to projectstate.AmendmentIndexFor (code-health-phase-bd task
// D3) — byte-identical pure resolver, no longer duplicated with projectdesign's twin. It
// returns the AMENDMENT index for a draft request against slot: the count of prior
// commits, used as the …-amend-N branch suffix and the "revision N" prompt framing, and
// the signal that gates the amendment path (fresh -amend-N branch, amendment prompt, and
// review-ledger SEED of the reopening feedback).

// sessionStageLabel renders a SessionStage as a short human label for the precondition
// messages.
func sessionStageLabel(s SessionStage) string {
	switch s {
	case SessionStageUnknown:
		return "not started"
	case StageDrafting:
		return "drafting"
	case StageAwaitingReview:
		return "awaiting review"
	case StageRedrafting:
		return "redrafting"
	case StageCommitted:
		return "committed"
	case StageWithdrawn:
		return "withdrawn"
	case StageRefused:
		return "refused"
	case StageDraftFailed:
		return "draft failed"
	}
	// Unreachable for the eight defined SessionStage values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a
	// defensive fallback for an out-of-range ordinal.
	return "unknown"
}

// staleCommittedPhase1Kinds returns the wire names of every COMMITTED Phase-1 slot that
// carries StaleBasis (a back-edge amendment invalidated its basis) — the set AdvancePhase must
// refuse to seal over unless the caller acknowledges. Order follows the canonical Phase-1
// spine so the message reads deterministically. A non-committed slot is never "stale" here (it
// isn't part of the seal), so only committed slots are inspected.
func staleCommittedPhase1Kinds(proj projectstate.Project) []string {
	var stale []string
	for _, kind := range phase1RequiredKinds() {
		slot := slotFor(proj, kind)
		if slot.Status == projectstate.ReviewCommitted && slot.StaleBasis {
			label := artifactKindWireName(kind)
			// STALE-UNACKED cause thread: name WHAT shifted the basis when the amendment
			// recorded it (absent for slots that went stale before the cause field existed).
			if c := slot.StaleBasisCause; c != nil {
				label = fmt.Sprintf("%s (basis changed by %s rev %d)", label, c.UpstreamKind, c.UpstreamRevision)
			}
			stale = append(stale, label)
		}
	}
	return stale
}

// standardCheckFailItems returns a human label for every FAIL item in the COMMITTED
// standard-check slot (STD-FAIL-OPEN). Empty when the standard check is not committed or
// carries no fail item — Phase 1 may seal only when the gate is fail-free.
func standardCheckFailItems(proj projectstate.Project) []string {
	slot := slotFor(proj, KindStandardCheck)
	if slot.Status != projectstate.ReviewCommitted {
		return nil
	}
	sc, ok := slot.Model.(*projectstate.StandardCheck)
	if !ok || sc == nil {
		return nil
	}
	var fails []string
	for i, it := range sc.Items {
		if it.Status != projectstate.CheckFail {
			continue
		}
		label := strings.TrimSpace(it.Guideline)
		if label == "" {
			label = strings.TrimSpace(it.Section)
		}
		if label == "" {
			label = fmt.Sprintf("item %d", i+1)
		}
		fails = append(fails, label)
	}
	return fails
}

// completedSessionView derives the honest session view for a CoAuthor run that closed
// NORMALLY (COMPLETED). The replayed sessionState query is NOT trusted for such a run
// (it can return a stale mid-flight stage — the P0-2 "GENERATING forever" wedge on an
// already-committed artifact), so the view is rebuilt from the DURABLE slot on main.
func (m *deliveryManager) designCompletedSessionView(ctx context.Context, projectID ProjectID, kind ArtifactKind) (SessionStateView, error) {
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return SessionStateView{}, mapReadProjectError(err)
	}
	return committedSessionView(projectID, kind, slotFor(proj, kind))
}

// committedSessionView projects the durable slot of a COMPLETED session onto a
// SessionStateView. A committed slot renders the committed view (StageCommitted + the
// committed model + the durable review thread) — the same {kind, model} shape the SPA
// consumes for a live session. A withdrawn slot renders StageWithdrawn. Any other
// terminal-but-uncommitted state (the run completed without landing a commit) renders an
// honest StageDraftFailed terminal carrying a neutral reason — NEVER StageDrafting, so
// the SPA never wedges on an infinite "GENERATING" spinner for a dead session.
func committedSessionView(projectID ProjectID, kind ArtifactKind, slot projectstate.ArtifactSlot) (SessionStateView, error) {
	switch slot.Status {
	case projectstate.ReviewCommitted:
		draft, err := draftModelFor(kind, slot.Model)
		if err != nil {
			return SessionStateView{}, newError(fwmanager.Infrastructure, err.Error())
		}
		return SessionStateView{
			ProjectID:    projectID,
			ArtifactKind: kind,
			Stage:        StageCommitted,
			Draft:        draft,
			ReviewThread: reviewThreadToView(slot.ReviewThread),
		}, nil
	case projectstate.ReviewWithdrawn:
		return SessionStateView{
			ProjectID:    projectID,
			ArtifactKind: kind,
			Stage:        StageWithdrawn,
			Draft:        DraftModel{Kind: artifactKindWireName(kind)},
		}, nil
	case projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected:
		// Any non-committed / non-withdrawn terminal status renders the honest
		// StageDraftFailed view (never StageDrafting — the anti-wedge rule).
		fallthrough
	default:
		reason := "the design session ended without committing an artifact. Retry to start a fresh draft."
		return SessionStateView{
			ProjectID:     projectID,
			ArtifactKind:  kind,
			Stage:         StageDraftFailed,
			Draft:         DraftModel{Kind: artifactKindWireName(kind)},
			FailureReason: &reason,
		}, nil
	}
}

// SetResearchInput — op 2.6 (2026-05-30). SYNCHRONOUS, non-Temporal: it records
// the Phase-1 ResearchInput Method INPUT so a fresh project can satisfy the
// StartSystemDesign ResearchInput-present precondition through the UI. A single
// idempotent head-state write via projectStateAccess.SetResearchInput, with no
// Temporal primitive (no workflow, signal, gate, or slot transition).
//
// Body (systemDesignManager.md §2.6): read the current head Version via
// ReadProject, derive a stable idempotencyKey for "set research input on this
// project", and write. On the RA's fwra.Conflict (a concurrent writer bumped the
// version under us) re-read and re-apply on the sync path, bounded. There is NO
// workflow, signal, gate, or slot transition — ResearchInput is a Method INPUT,
// not a co-authored artifact (no AwaitingReview/Committed lifecycle).
//
// Returns the resulting head Version (the SPA may use it for optimistic display;
// the frozen surface is the write itself).
func (m *deliveryManager) SetResearchInput(rc fwmanager.Context, projectID ProjectID, research ResearchInput) (Version, error) {
	ctx := rc.Context
	if projectID == "" {
		return 0, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if researchIsZero(research) {
		return 0, newError(fwmanager.ContractMisuse, "empty research (no sources)")
	}
	if problem := researchSourceProblem(research); problem != "" {
		return 0, newError(fwmanager.ContractMisuse, problem)
	}

	key := researchInputIdempotencyKey(projectID, research)
	psID := projectstate.ProjectID(projectID)
	psResearch := toPSResearch(research)

	// Sync-path optimistic-concurrency loop. The first write uses the head Version
	// just read; on a Conflict (a concurrent mutation bumped the row) re-read and
	// re-apply. Bounded so a pathological write-storm cannot spin forever.
	var lastErr error
	for range setResearchInputMaxAttempts {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			return 0, mapReadProjectError(err)
		}

		newVersion, err := m.projectState.SetResearchInput(fwra.Context{Context: ctx, IdempotencyKey: key}, psID, proj.Version, psResearch)
		if err == nil {
			return Version(newVersion), nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue // re-read head Version, re-apply (same idempotencyKey)
		}
		return 0, mapSetResearchInputError(err)
	}
	return 0, fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "projectStateAccess.SetResearchInput: exhausted conflict retries")
}

// setResearchInputMaxAttempts bounds the sync-path re-read/re-apply loop.
const setResearchInputMaxAttempts = 5

// researchInputIdempotencyKey derives the stable logical idempotency key for
// "set research input on this project". Unlike the workflow Activities (which key
// by "${workflowId}:${activityId}"), this sync op has no Temporal context, so the
// key is derived from the project id plus a content fingerprint: a retried write
// of the SAME research collapses to a no-op in the RA dedup ledger, while a
// genuinely new research payload is a distinct logical mutation.
func researchInputIdempotencyKey(projectID ProjectID, research ResearchInput) fwra.IdempotencyKey {
	h := fnv.New64a()
	for _, s := range research.Sources {
		_, _ = h.Write([]byte(s.Title))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(s.Content))
		_, _ = h.Write([]byte{0})
	}
	return fwra.IdempotencyKey(fmt.Sprintf("%s:setResearchInput:%x", projectID, h.Sum64()))
}

// mapSetResearchInputError converts projectStateAccess SetResearchInput errors
// into fwmanager.Error on the sync write path. fwra.NotFound → NotFound (no
// project aggregate yet — the caller may need to open it first); fwra.ContractMisuse
// → ContractMisuse; everything else (incl. unrecovered Conflict) → Infrastructure
// with retryability preserved.
func mapSetResearchInputError(err error) error {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		case fwra.Unknown, fwra.Transient, fwra.RateLimited, fwra.Infrastructure,
			fwra.Auth, fwra.Conflict, fwra.QuotaExhausted, fwra.ContentPolicy:
			// "Everything else (incl. unrecovered Conflict) → Infrastructure with
			// retryability preserved" per the doc comment above. These 8 kinds
			// carry no distinct handling on this sync write path: Auth/QuotaExhausted/
			// ContentPolicy are terminal-but-not-actionable-by-the-caller here, and
			// Conflict that reaches this far means the RA's own retry-on-conflict
			// loop gave up — surface it as Infrastructure so the caller's generic
			// retry policy applies, same as Transient/RateLimited/Unknown.
			mapped := fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess.SetResearchInput")
			mapped.Retryable = raErr.Retryable
			return mapped
		default:
			mapped := fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess.SetResearchInput")
			mapped.Retryable = raErr.Retryable
			return mapped
		}
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// isRAConflict reports whether err is the RA optimistic-concurrency conflict
// (fwra.Conflict) returned DIRECTLY on the sync path — the signal to re-read the
// head Version and re-apply. (Distinct from workflow.go's isConflict, which
// inspects the Temporal-wrapped ApplicationError on the replayed Activity path.)
func isRAConflict(err error) bool {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		return raErr.Kind == fwra.Conflict
	}
	return false
}

// mapReadProjectError converts projectStateAccess errors into fwmanager.Error
// for the sync read op. fwra.NotFound → NotFound (a brand-new / unknown project),
// other fwra.* errors → Infrastructure with the original retryability preserved.
func mapReadProjectError(err error) error {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		case fwra.Unknown, fwra.Transient, fwra.RateLimited, fwra.Infrastructure,
			fwra.Auth, fwra.Conflict, fwra.QuotaExhausted, fwra.ContentPolicy:
			// Same "everything else → Infrastructure" rationale as
			// mapSetResearchInputError above: no distinct handling for these
			// kinds on this sync read path.
			return fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess.ReadProject")
		default:
			return fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess.ReadProject")
		}
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// --- error mapping at the façade boundary -----------------------------------

func mapStartError(err error) error {
	// A "workflow already started" race under UseExisting policy is benign; the
	// SDK surfaces it as *serviceerror.WorkflowExecutionAlreadyStarted, but with
	// UseExisting the ExecuteWorkflow returns the existing handle without error.
	// Any error here is treated as a infrastructure fault.
	return newError(fwmanager.Infrastructure, err.Error())
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
func mapSignalError(err error) error {
	if isNotFound(err) {
		return newError(fwmanager.NotFound, err.Error())
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// isNotFound reports whether the Temporal error indicates the addressed
// execution does not exist — typed as *serviceerror.NotFound, the canonical
// "no such workflow" error the SDK returns.
//
// QA 2026-07-19 (poll-404 wizard reset): this used to substring-match "not
// found"/"NotFound" over ANY error, which classified *serviceerror.
// NamespaceNotFound ("Namespace default is not found" — the server talking to
// a wrong/foreign Temporal backend, observed live when the systemtests dev
// server took over the shared port) as the authoritative "no active design
// session" NotFound. The SPA trusts that 404 and resets the wizard, so a
// backend-identity fault destroyed client state. Only the typed
// execution-NotFound may claim session absence; everything else stays an
// Infrastructure fault the client tolerates.
func isNotFound(err error) bool {
	var notFound *serviceerror.NotFound
	return errors.As(err, &notFound)
}

// AnchoredComment's JSONPath is OPAQUE guidance text the architect anchors a
// "send back" comment to in the typed artifact model — the server does not
// evaluate it.

// PhaseAdvanceResult is the gating outcome of AdvancePhase: a non-Advanced result
// is the NORMAL "you still owe artifacts X, Y" answer, not an error.

// DraftModel (the staged-draft envelope on SessionStateView) is IDENTICAL on the
// wire to the project ArtifactSlotModel envelope, so the SPA decodes a draft the
// same way regardless of which read produced it.

// StageDraftFailed is the human-visible, human-actionable stage the session lands
// in when the dispatched agentic DESIGN job reaches a TYPED terminal failure phase
// (PhaseFailed / PhaseCancelled). It carries the job's neutral Diagnostic in
// FailureReason. Surfaced by getSessionState so the SPA renders "your design job
// failed: <diagnostic> — retry or withdraw" and NEVER a perpetual StageDrafting
// spinner.

// ---------------------------------------------------------------------------
// PM-critique value types (systemDesignManager.md §3.6). OWNED by this Manager and
// used ONLY internally (the workflow / readBackCritique) — NOT part of the public
// port surface, so they stay hand-written and are NOT in the generated contract.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Façade error model (systemDesignManager.md §3.5). CALLER/PROGRAMMER errors at the
// façade boundary — distinct from the workflow's own failure handling.
// ---------------------------------------------------------------------------

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
func newError(kind fwmanager.Kind, detail string) *fwmanager.Error {
	return fwmanager.New(kind, detail)
}

// behavior.go holds the FREE FUNCTIONS that carry behavior over the contract value
// types. The generated contract surface (contract.gen.go) is PURE DATA — enums and
// structs with no methods — so any logic over a contract value (the canonical-name
// lookups that used to be methods on the projectstate enums, the opaque SessionRef
// constructor) lives here as a free function.
//
// systemdesign's OWN ArtifactKind mirrors projectstate.ArtifactKind ordinal-for-
// ordinal, so its behavior is derived by a meaning-preserving int conversion to the
// canonical projectstate type rather than re-implemented here.

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// newSessionRef constructs a SessionRef from an infrastructure identity. Internal to
// the Manager; Clients only ever receive and echo SessionRefs.
func newSessionRef(opaque string) SessionRef { return SessionRef(opaque) }

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// toPSKind converts systemdesign's OWN ArtifactKind to the canonical
// projectstate.ArtifactKind (ordinal-preserving) for behavior + RA-boundary calls.
func toPSKind(k ArtifactKind) projectstate.ArtifactKind { return projectstate.ArtifactKind(k) }

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// artifactKindString returns the PascalCase Go-identifier name for an ArtifactKind
// (the dispatch-input + PR-title + diagnostic form). Mirrors projectstate String().
func artifactKindString(k ArtifactKind) string { return toPSKind(k).String() }

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// artifactKindWireName returns the canonical camelCase wire name for an ArtifactKind.
func artifactKindWireName(k ArtifactKind) string { return toPSKind(k).WireName() }

// artifactKindIsPhase1 reports whether the kind belongs to The Method's Phase 1.
func artifactKindIsPhase1(k ArtifactKind) bool { return toPSKind(k).IsPhase1() }

// phase1RequiredKinds returns the ordered set of Phase-1 artifact kinds (systemdesign's
// OWN type), mirroring projectstate.Phase1RequiredKinds().
func phase1RequiredKinds() []ArtifactKind {
	ps := projectstate.Phase1RequiredKinds()
	out := make([]ArtifactKind, 0, len(ps))
	for _, k := range ps {
		out = append(out, ArtifactKind(k))
	}
	return out
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// strPtrOrNil maps a failure-reason string to the optional contract field: nil for
// the empty string (omitted on the wire), &s otherwise (the project notesPtr pattern).
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// researchIsZero reports whether the ResearchInput is unprovided (no Sources). The
// SetResearchInput pre-condition rejects a zero value.
func researchIsZero(r ResearchInput) bool { return len(r.Sources) == 0 }

// researchSourceProblem reports the first per-source shape violation in a
// non-empty ResearchInput as a clean, client-facing detail string naming the
// offending source by its 1-based position (e.g. `research source 2: title must
// not be empty`). It returns "" when every source carries a non-whitespace title
// AND non-whitespace content. The empty-corpus (no sources at all) case is handled
// separately by researchIsZero — this function assumes at least one source and
// validates the shape of each. Whitespace-only fields are treated as empty so a
// source cannot smuggle a blank title/content past the gate with a stray space.
func researchSourceProblem(r ResearchInput) string {
	for i, s := range r.Sources {
		pos := i + 1 // 1-based, client-facing
		if strings.TrimSpace(s.Title) == "" {
			return fmt.Sprintf("research source %d: title must not be empty", pos)
		}
		if strings.TrimSpace(s.Content) == "" {
			return fmt.Sprintf("research source %d: content must not be empty", pos)
		}
	}
	return ""
}

// toPSResearch converts the contract ResearchInput to projectstate.ResearchInput at
// the projectStateAccess boundary.
func toPSResearch(r ResearchInput) projectstate.ResearchInput {
	sources := make([]projectstate.ResearchSource, 0, len(r.Sources))
	for _, s := range r.Sources {
		sources = append(sources, projectstate.ResearchSource{Title: s.Title, Content: s.Content})
	}
	return projectstate.ResearchInput{Sources: sources}
}

// findings.go owns the SESSION-TRANSIENT validation-finding value types this Manager
// surfaces on its getSessionState read (SessionStateView.Findings). The SPA renders
// findings[] to explain "why it's being redrafted" (the PM-critique-unresolved
// warning is one). They are part of this component's OWN generated contract surface
// (registered in cmd/schemagen) — pure data, no methods.
//
// WIRE: severity is a camelCase STRING name ("info"|"warning"|"error") — a string
// enum keeps the generated type pure data (no custom MarshalJSON) while the wire
// form stays byte-identical for the SPA (f.severity === 'error' / 'warning').

// Severity is a finding severity. Only SeverityError fails a verdict; Warning/Info
// ride along advisory. The value IS its canonical camelCase wire name.

// RuleID is the stable, namespaced id of a validation rule. Stable across runs for
// finding-diff and worker-prompt continuity.

// Location locates a finding within a typed model. NO Line field: the input is a
// typed model, not bytes.

// stable position used for deterministic finding ordering
// human-readable locus, e.g. "core use case 3"

// Finding is a single machine-checkable rule violation surfaced to the SPA.

// human-readable; safe to weave into a redraft prompt; no PII
// optional; where in the model the finding sits

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// draftModelFor builds the OPAQUE public DraftModel envelope ({kind, model}) the
// session read carries the staged typed draft as. Kind is the artifactKind's canonical
// camelCase wire name (always set, so the SPA gets {"kind":"mission"} even before a
// draft is staged); Model is the concrete model's own JSON, omitted when nil. This is
// the public-surface twin of modelEnvelope (the Temporal/Activity carrier) — the same
// {kind, model} wire shape the SPA decodes, with Kind as a plain string so the
// generated contract carries no projectstate ArtifactKind.
func draftModelFor(kind ArtifactKind, model projectstate.ArtifactModel) (DraftModel, error) {
	env := DraftModel{Kind: artifactKindWireName(kind)}
	if model != nil {
		raw, err := json.Marshal(model)
		if err != nil {
			return DraftModel{}, fmt.Errorf("encode draft model %s: %w", model.Kind(), err)
		}
		rm := json.RawMessage(raw)
		env.Model = &rm
	}
	return env, nil
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
const acknowledgeStaleMaxAttempts = 5

// AcknowledgeStaleBasis clears the committed slot's StaleBasis and records the reviewer's
// note as a staleAck audit entry. Synchronous OCC write (mirrors SetResearchInput).
func (m *deliveryManager) ackDesignStaleBasis(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, note string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if strings.TrimSpace(note) == "" {
		return newError(fwmanager.ContractMisuse, "an acknowledgement requires a non-empty note — it is the reviewer's durable justification, and it also keys the idempotency of the ack")
	}
	if !artifactKindIsPhase1(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-1 kind")
	}
	// F-GTD-12: an acknowledge is a MAIN-branch write (the StaleBasis clear + the staleAck
	// entry commit on main). While a co-author session is LIVE for this slot — on a committed
	// slot that is by definition an in-flight AMENDMENT — that main write turns the session's
	// review PR merge-DIRTY, so the eventual approve's merge fails with a Conflict and the
	// workflow bounces back to AwaitingReview looking like a silent no-op to the reviewer.
	// Refuse up front: reconcile RIDES the amendment (its merge clears the staleness).
	if err := m.refuseDesignAckDuringLiveSession(rc, projectID, kind); err != nil {
		return err
	}
	key := acknowledgeStaleIdempotencyKey(projectID, kind, note)
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)

	var lastErr error
	for range acknowledgeStaleMaxAttempts {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			return mapReadProjectError(err)
		}
		_, err = m.projectState.AcknowledgeStaleBasis(fwra.Context{Context: ctx}, psID, proj.Version, psKind, note, key)
		if err == nil {
			return nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue
		}
		return mapSetResearchInputError(err) // shares the ContractMisuse/NotFound/else mapping
	}
	return fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "AcknowledgeStaleBasis: exhausted conflict retries")
}

// refuseAckDuringLiveSession is the F-GTD-12 guard (Phase-1 twin of the projectdesign
// impl): while the target kind has a LIVE co-author (amendment) session, the acknowledge
// is refused with a FailedPrecondition (the wire's 409/"failed_precondition" conflict
// shape). Liveness is read through GetSessionState — the SAME Describe-then-Query path
// the review gate and the SPA trust (a dead run synthesizes StageDraftFailed; a COMPLETED
// run is rebuilt from the durable slot) — so ack gating always agrees with what the
// reviewer sees on screen. A NotFound (no session ever ran for this slot) passes.
func (m *deliveryManager) refuseDesignAckDuringLiveSession(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) error {
	view, err := m.designCompletedSessionView(rc, projectID, kind)
	if err != nil {
		var me *fwmanager.Error
		if errors.As(err, &me) && me.Kind == fwmanager.NotFound {
			return nil
		}
		return err
	}
	if !sessionStageIsLive(view.Stage) {
		return nil
	}
	return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
		"cannot mark this artifact reviewed: its amendment session is still open (currently %s). Reconcile rides the amendment — acknowledging now would commit to main and merge-conflict the amendment's review PR. Approve or withdraw the session first.",
		sessionStageLabel(view.Stage)))
}

// sessionStageIsLive reports whether a co-author session stage means the session still
// OWNS the slot (its branch/PR is open or recoverable): drafting / awaiting review /
// redrafting, plus the StageDraftFailed recovery gate (the session is suspended there
// with its branch and PR intact — a Retry resumes it). The terminal stages (committed /
// withdrawn / refused) and the unknown zero value are NOT live.
func sessionStageIsLive(s SessionStage) bool {
	switch s {
	case StageDrafting, StageAwaitingReview, StageRedrafting, StageDraftFailed:
		return true
	case SessionStageUnknown, StageCommitted, StageWithdrawn, StageRefused:
		return false
	default:
		return false
	}
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
func acknowledgeStaleIdempotencyKey(projectID ProjectID, kind ArtifactKind, note string) fwra.IdempotencyKey {
	h := fnv.New64a()
	_, _ = h.Write([]byte(note))
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%d:ackStale:%x", projectID, int(kind), h.Sum64()))
}

// askquestions.go implements the question-comments op (founder-ratified 2026-07-05):
// AskQuestions appends one or more clarifying QUESTIONS to an artifact's review ledger
// WITHOUT sending the draft back for a redraft, and dispatches a lightweight ANSWER job so
// the addressed role (pm / architect) answers each in place via the aiarch-state MCP's
// respondToReviewComment. Unlike change-request comments, open questions do NOT block
// approve (they surface as a soft warning at the approve gate). It works on a COMMITTED
// artifact too — seeding a question-only thread on main without opening an amendment
// session — and on a live AwaitingReview session (appending on that session's branch).

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// askQuestionsMaxAttempts bounds the sync-path OCC re-read/re-apply loop.
const askQuestionsMaxAttempts = 5

// AskQuestions — the question-comments op. Appends the given questions to the artifact's
// durable review ledger as type="question" entries addressed to `addressee`, then
// dispatches an answer job. Synchronous (no Temporal workflow): the append is the durable,
// user-visible effect; the answer job is best-effort (a dispatch miss leaves the questions
// recorded and unanswered, exactly as if the addressee has not answered yet).
//
// DISPATCH RECOVERY (F82): a dispatch MISS is now LOGGED LOUDLY server-side (it was
// previously discarded, and the construction-pipeline RA has no logger, so a miss vanished
// with zero operator signal). To RECOVER a dropped dispatch, simply CALL AskQuestions AGAIN
// with the same questions: the seed is idempotent on its content key, so NO ledger entry is
// duplicated (the existing entries' round is reused so the minted ids still match), while the
// answer-job dispatch RE-FIRES via a per-call-unique key.
func (m *deliveryManager) askDesignQuestions(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, addressee string, questions []AnchoredComment) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase1(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-1 kind")
	}
	switch addressee {
	case projectstate.ReviewAddresseePM, projectstate.ReviewAddresseeArchitect:
		// ok
	default:
		return newError(fwmanager.ContractMisuse, "addressee must be \"pm\" or \"architect\"")
	}
	// A QUESTION THREAD IS A CONVERSATION (comment-margin task 5b). An ask carrying a
	// replyTo is a FOLLOW-UP on a thread the agent already answered, not a new question — so
	// this door ROUTES it into that thread instead of refusing it. (requestArtifactDraft
	// still refuses one: it seeds round-0 threads before any thread is loaded, so it could
	// only re-file a reply as a fresh unanchored comment.) The batch is partitioned ONCE
	// here, before the ledger read, because both the emptiness refusal and the idempotency
	// key must see the whole batch; the replyTo targets are checked against the live thread
	// inside the loop. `at` is stamped once, outside the loop, so an OCC retry re-applies the
	// identical utterance rather than duplicating it.
	at := time.Now().UTC().Format(time.RFC3339)
	freshAsks, replies := partitionIncomingComments(questions, at)
	qs := questionsToLedger(addressee, freshAsks)
	// A REPLY-ONLY batch is a legitimate ask — the follow-up IS the question this round.
	if len(qs) == 0 && len(replies) == 0 {
		return newError(fwmanager.ContractMisuse, "no questions to ask (every question needs text)")
	}

	// MAIN, BESIDE THE SLOT (ratified 4b2; 4b1 Q6). A question's thread OUTLIVES the draft
	// it is about: activity/{activityId} is squashed away at merge, while the slot's thread
	// on main is what the SPA reads, what the answer job answers, and what a reader finds
	// six months later. The resolver that used to ask for a session branch was dead BY TYPE
	// — isLiveSessionStage's true-set and committedSessionView's output-set are disjoint —
	// so main was already the only reachable answer; this is the answer stated rather than
	// arrived at. The join to the draft survives without the branch: the comment names its
	// round.
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)
	key := askQuestionsIdempotencyKey(projectID, kind, "", qs, replies)

	// Sync-path optimistic-concurrency loop (mirrors SetResearchInput): read the head
	// version on main, compute a fresh question round from the live thread so the minted
	// ids never collide with prior entries, and append. Re-read on Conflict.
	var lastErr error
	for range askQuestionsMaxAttempts {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			return mapReadProjectError(err)
		}
		thread := slotFor(proj, kind).ReviewThread
		// A replyTo naming no thread on this artifact is a hard refusal, never a silent new
		// thread — the same rule the change-request door applies, run here against the thread
		// just read (the RA would surface a bare NotFound from deep inside the append).
		if perr := checkReplyTargets(ledgerCommentIDs(thread), questions); perr != nil {
			return perr
		}
		round := nextQuestionRound(thread)
		if r, ok := existingQuestionRound(thread, qs); ok {
			// A prior ask already seeded these exact questions (its answer-job dispatch may
			// have been dropped — F82). Reuse their round so the minted ids match the EXISTING
			// ledger entries, and the re-fired answer job answers the right comments.
			round = r
		}
		_, err = m.designSession.SeedReviewCommentsOnBranch(fwra.Context{Context: ctx}, psID, proj.Version, "", psKind, round, qs, replies, key)
		if err == nil {
			// Best-effort dispatch of the answer job. A dispatch failure is logged by the
			// pipeline access; the questions are already durably recorded, so we do not fail
			// the op — the addressee can be re-prompted, and the SPA already shows the asks.
			// Stamp the deterministic minted ids onto a copy so the answer prompt can name
			// each question by the id the addressee must call respondToReviewComment with.
			minted := make([]projectstate.ReviewComment, len(qs))
			for i := range qs {
				minted[i] = qs[i]
				minted[i].ID = projectstate.ReviewCommentID(round, i)
			}
			// A REPLY is answered by the role its THREAD is addressed to, not by whoever the
			// caller named — see answerJobAddressee. Dispatching the other role's command would
			// start a session that finds nothing addressed to it, leaving the follow-up
			// unanswered forever with no signal.
			dispatchTo, mixed := answerJobAddressee(thread, addressee, len(qs), replies)
			if mixed {
				slog.Default().Warn("askQuestions: this batch needs BOTH answer roles (a fresh question for one, a reply on the other's thread) — only one answer job is dispatched, so the other half stays unanswered until it is re-asked on its own",
					"op", "systemdesign.AskQuestions", "projectID", string(projectID),
					"artifactKind", artifactKindString(kind), "dispatchedTo", dispatchTo)
			}
			m.dispatchAnswerJob(ctx, projectID, kind, "", dispatchTo, minted)
			return nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue
		}
		return mapReadProjectError(err)
	}
	return fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "AskQuestions: exhausted conflict retries")
}

// resolveQuestionBranch, isLiveSessionStage and readProjectMaybeBranch are DELETED (stage
// 4b2): the resolver asked isLiveSessionStage — true only for {Drafting, AwaitingReview,
// Redrafting, Refused} — of a view committedSessionView produces, and that producer emits
// only {Committed, Withdrawn, DraftFailed}. Disjoint sets, so the session-branch arm was
// false for every possible input and every question already resolved to main; the branch-
// taking read collapsed onto ReadProject with it. Both callers now say main out loud, with
// the ratification at the site.
// Test_QuestionBranch_TheLiveStageAndTheDerivedStagesAreDisjoint keeps the argument.

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// questionsToLedger converts inbound anchored questions into the projectstate.ReviewComment
// shape the append verb stamps, marking each type="question" + addressee. An empty-text
// question is dropped (defensive). Id / round / open status / empty response are minted in
// appendReviewComments.
func questionsToLedger(addressee string, questions []AnchoredComment) []projectstate.ReviewComment {
	out := make([]projectstate.ReviewComment, 0, len(questions))
	for _, q := range questions {
		if strings.TrimSpace(q.Text) == "" {
			continue
		}
		out = append(out, projectstate.ReviewComment{
			Anchor:     q.JSONPath,
			AnchorText: q.AnchorText,
			Text:       q.Text,
			AuthorRole: reviewAuthorRole,
			Type:       projectstate.ReviewCommentTypeQuestion,
			Addressee:  addressee,
		})
	}
	return out
}

// answerJobAddressee decides which role the answer job must be dispatched to, given the batch
// that was just appended. A FRESH question is addressed by its asker, so the caller's argument
// decides. A REPLY is NOT: it lands in a thread that already has its own addressee, and both
// answer commands select only "the OPEN questions addressed to YOU" — so dispatching the other
// role's command starts a session that finds nothing to do, and the follow-up sits unanswered
// forever with no signal anywhere. The client cannot defend this (it does not know the thread's
// addressee either), so the thread the reply names decides. A reply onto a thread with no
// addressee at all (a change-request) falls back to the caller's.
//
// mixed reports that the batch genuinely needs BOTH roles — a fresh question for one and a
// reply on the other's thread, which the SPA can produce because it groups by the STAGED
// addressee, not by the target thread's. One dispatch cannot serve both, so the caller's
// addressee is kept (today's behaviour for the fresh half) and the caller logs the disagreement
// rather than silently answering half the batch.
func answerJobAddressee(thread []projectstate.ReviewComment, callerAddressee string, freshCount int, replies []projectstate.ReviewReply) (string, bool) {
	addresseeOf := make(map[string]string, len(thread))
	for _, c := range thread {
		addresseeOf[c.ID] = c.Addressee
	}
	chosen, mixed := "", false
	note := func(a string) {
		switch {
		case a == "":
		case chosen == "":
			chosen = a
		case chosen != a:
			mixed = true
		}
	}
	if freshCount > 0 {
		note(callerAddressee)
	}
	for _, r := range replies {
		if a := addresseeOf[r.CommentID]; a != "" {
			note(a)
			continue
		}
		note(callerAddressee)
	}
	if chosen == "" {
		chosen = callerAddressee
	}
	return chosen, mixed
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// nextQuestionRound returns a round number one past the highest round already present in the
// thread (min 1), so appendReviewComments mints fresh, non-colliding ids for a new batch of
// questions regardless of how many reject/amendment rounds preceded them.
func nextQuestionRound(thread []projectstate.ReviewComment) int64 {
	var maxRound int64
	for _, c := range thread {
		if c.Round > maxRound {
			maxRound = c.Round
		}
	}
	return maxRound + 1
}

// askQuestionsIdempotencyKey derives the stable logical key for "ask this batch of questions
// on this artifact/branch". Content-derived (no Temporal context on this sync op), so a
// retried identical Ask collapses to a no-op in the RA dedup ledger while a genuinely new
// batch is a distinct mutation.
//
// The REPLY half of the batch is hashed too (task 5b): a follow-up on an existing question
// thread adds no fresh entry, so a qs-only key would give two different follow-ups — or a
// follow-up and a bare re-ask — the SAME key, and the RA would swallow the second as a
// duplicate. The utterance's `at` is deliberately excluded: it is wall-clock on this sync op,
// and including it would defeat the re-ask dedup the key exists for.
func askQuestionsIdempotencyKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment, replies []projectstate.ReviewReply) fwra.IdempotencyKey {
	h := fnv.New64a()
	_, _ = h.Write([]byte(branch))
	_, _ = h.Write([]byte{0})
	for _, q := range qs {
		_, _ = h.Write([]byte(q.Addressee))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(q.Anchor))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(q.Text))
		_, _ = h.Write([]byte{0})
	}
	for _, r := range replies {
		_, _ = h.Write([]byte(r.CommentID))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(r.Text))
		_, _ = h.Write([]byte{0})
	}
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%d:askQuestions:%x", projectID, int(kind), h.Sum64()))
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// answerJobDispatchSeq makes each explicit AskQuestions call produce a UNIQUE answer-job
// dispatch key, so a re-ask RE-FIRES the answer job (the RA dedups on the whole key, so a
// content-only key would swallow the re-fire — F82). AskQuestions is a direct, non-retried
// manager op (exactly one dispatch per successful call), so a per-call nonce cannot
// double-fire a single logical ask; it only enables the re-ask recovery.
var answerJobDispatchSeq atomic.Uint64

// answerJobDispatchKey derives a per-call-unique answer-job idempotency key from the content
// base plus a monotonic nonce (see answerJobDispatchSeq). The reply half is not folded into
// the base: the nonce ALREADY makes every dispatch key distinct, which is this key's whole
// purpose, so a reply-only ask still fires its own answer job.
func answerJobDispatchKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment) fwra.IdempotencyKey {
	base := askQuestionsIdempotencyKey(projectID, kind, branch, qs, nil)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:answerJob:%d", base, answerJobDispatchSeq.Add(1)))
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// existingQuestionRound returns the round of an EARLIER identical seeding of qs (matched by
// addressee + anchor + text of the first question), so a re-ask reuses that round rather than
// minting a fresh one — keeping the minted ids aligned with the already-seeded ledger entries
// (F82 re-dispatch correctness). ok=false when these questions were never seeded.
func existingQuestionRound(thread []projectstate.ReviewComment, qs []projectstate.ReviewComment) (int64, bool) {
	if len(qs) == 0 {
		return 0, false
	}
	first := qs[0]
	for _, c := range thread {
		if c.Type == projectstate.ReviewCommentTypeQuestion &&
			c.Addressee == first.Addressee && c.Anchor == first.Anchor && c.Text == first.Text {
			return c.Round, true
		}
	}
	return 0, false
}

// dispatchAnswerJob dispatches ONE lightweight agentic ANSWER job (job_mode=answer) to the
// per-project design repo so the addressed role answers each question in place via the
// aiarch-state MCP. Best-effort and fire-and-forget (it does NOT wait for the job — questions
// are auxiliary and never gate anything). F82: every outcome is LOGGED LOUDLY server-side — a
// miss (rail not configured, repo unresolved, or a submit fault) was previously discarded and
// the construction-pipeline RA has no logger, so it vanished with zero operator signal. A miss
// is recoverable by re-calling AskQuestions (see the op doc) — never silent.
func (m *deliveryManager) dispatchAnswerJob(ctx context.Context, projectID ProjectID, kind ArtifactKind, branch, addressee string, qs []projectstate.ReviewComment) {
	log := slog.Default().With(
		"op", "systemdesign.AskQuestions.dispatchAnswerJob",
		"projectID", string(projectID), "artifactKind", artifactKindString(kind),
		"addressee", addressee, "branch", branch)
	if m.pipeline == nil || m.repo == nil {
		log.Warn("answer job NOT dispatched: design pipeline/repo not configured (rail dormant) — the question is recorded but will not be auto-answered")
		return
	}
	repoRef, ok := m.repo(projectID)
	if !ok {
		log.Error("answer job NOT dispatched: could not resolve the project repo — the question is recorded but will not be auto-answered; re-run AskQuestions to retry")
		return
	}
	// MANAGED-SCAFFOLD SYNC (sync-on-dispatch): an answer job runs the same seated
	// aiarch-design.yml (and installs the same aiarch-state-mcp binary) as a draft, so it
	// too must never run against a stale scaffold. Failure keeps the answer-job miss
	// semantics: recorded question, loud log, no dispatch — re-run AskQuestions to retry.
	if m.rail != nil {
		cred, cerr := m.rail.GetInstallationToken(fwra.Context{Context: ctx}, repoRef)
		if cerr != nil {
			log.Error("answer job NOT dispatched: could not mint the repo credential for the managed-scaffold sync; re-run AskQuestions to retry", "err", cerr.Error())
			return
		}
		if _, serr := sourcecontrol.SyncManagedScaffold(ctx, m.rail, repoRef, cred); serr != nil {
			log.Error("answer job NOT dispatched: managed-scaffold sync failed — the seated design workflow could not be proven current; re-run AskQuestions to retry", "err", serr.Error())
			return
		}
	}
	// Direct manager-side dispatch (NOT a Temporal workflow): the answer job is a
	// fire-and-forget submit over the PUBLISHED agenticJobAccess RA. The
	// RepoRef→RepoTarget decode + the placeholder step graph the retired pipelineDispatchAdapter
	// added are inlined here (the workflow-side twin is dispatchDesignJob, in this file since
	// stage 4b1 Task 13 folded the co-author spine's survivors in).
	target, terr := designRepoTarget(sourcecontrol.RepoRefString(repoRef))
	if terr != nil {
		log.Error("answer job NOT dispatched: could not resolve the target repo for the answer job; re-run AskQuestions to retry", "err", terr.Error())
		return
	}
	// The addressee rides the .claude command NAME now (design-answer vs design-answer-pm)
	// rather than a composed answer prompt. An empty slug is contract misuse — an addressee
	// that is neither "architect" nor "pm"; keep the answer-job miss semantics (recorded
	// question, loud log, no dispatch).
	command := projectstate.DesignCommandFor(toPSKind(kind), projectstate.DesignJobModeAnswer, addressee)
	if command == "" {
		log.Error("answer job NOT dispatched: no design-answer command slug for the addressee (contract misuse — expected \"architect\" or \"pm\")")
		return
	}
	inputs := map[string]string{
		dispatchInputArtifactKind:  artifactKindString(kind),
		dispatchInputCommand:       command,
		dispatchInputTargetBranch:  branch,
		dispatchInputPriorStateRef: "",
		dispatchInputJobMode:       jobModeAnswer,
	}
	spec := agenticjob.PipelineSpec{
		ProjectID: agenticjob.ProjectID(projectID),
		Steps: []agenticjob.PipelineStep{{
			Name:      "design",
			Toolchain: agenticjob.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: inputs,
		TargetRepo:     target,
		WorkflowFile:   designWorkflowFileName,
	}
	key := answerJobDispatchKey(projectID, kind, branch, qs)
	handle, err := m.pipeline.SubmitAgenticJob(fwra.Context{Context: ctx, IdempotencyKey: key}, spec)
	if err != nil {
		log.Error("answer job dispatch FAILED — the question is recorded but not auto-answered; re-run AskQuestions with the same question to retry",
			"err", err.Error(), "key", string(key))
		return
	}
	log.Info("answer job dispatched", "key", string(key))
	m.watchAnswerEpisode(ctx, projectID, kind, handle, log)
}

// ---------------------------------------------------------------------------
// Episode capture for the ANSWER job (SP1 capture-seam, Task 7)
// ---------------------------------------------------------------------------
//
// The answer job is the ONE agentic dispatch this Manager makes outside a Temporal
// workflow: AskQuestions submits it fire-and-forget and returns. Nothing observes it, so
// without this watch every answer episode — real tokens, really spent — would be invisible
// to the ledger.
//
// NON-DURABLE BY CONSTRUCTION, and that is accepted: this is a plain goroutine in the
// server process. A restart between the dispatch and the terminal observation loses the
// watch and therefore the record — no gap line either, because nothing is left to write
// one. Only the WORKFLOW-side capture paths carry the durable never-silent guarantee; the
// answer job is auxiliary (it gates nothing) and did not warrant its own workflow.

const (
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	// answerEpisodePollInterval spaces the manager-side observe loop. Same order as the
	// workflow-side observePollInterval — an answer job is the same kind of agentic run.
	answerEpisodePollInterval = 15 * time.Second
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	// answerEpisodeWatchWindow is the hard deadline on the watch. Past it the episode is
	// recorded as an explicit GAP rather than watched forever by a leaked goroutine.
	answerEpisodeWatchWindow = 30 * time.Minute
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	// answerEpisodeAppendWindow bounds the ledger append that follows the watch. It is a
	// SEPARATE budget from answerEpisodeWatchWindow on purpose — see run().
	answerEpisodeAppendWindow = 30 * time.Second
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	// answerEpisodeAppendAttempts / answerEpisodeAppendBackoff are this path's stand-in
	// for the Temporal retry envelope the workflow-side append rides. Small and bounded:
	// a local sidecar append that fails three times in a row is not transient.
	answerEpisodeAppendAttempts = 3
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	answerEpisodeAppendBackoff = 250 * time.Millisecond
)

// watchAnswerEpisode spawns the bounded manager-side watch for one dispatched answer job.
// It detaches from the CALLER'S context on purpose: ctx is the AskQuestions request
// context and is cancelled the moment that call returns, while the job it dispatched runs
// for minutes afterwards. WithoutCancel keeps the request's values (tracing, principal)
// and drops only the cancellation.
func (m *deliveryManager) watchAnswerEpisode(ctx context.Context, projectID ProjectID, kind ArtifactKind, handle agenticjob.PipelineHandle, log *slog.Logger) {
	w := answerEpisodeWatch{
		pipeline: m.pipeline,
		episodes: m.episodes,
		poll:     answerEpisodePollInterval,
		window:   answerEpisodeWatchWindow,
		log:      log,
	}
	go w.run(context.WithoutCancel(ctx), projectID, artifactKindString(kind), handle)
}

// answerEpisodeWatch is the bounded observe-then-append loop behind watchAnswerEpisode,
// broken out with its timings injected so it can be exercised deterministically in tests.
type answerEpisodeWatch struct {
	pipeline agenticjob.AgenticJobAccess
	episodes episode.EpisodeAccess
	poll     time.Duration
	window   time.Duration
	log      *slog.Logger
}

// run polls handle to a terminal phase (or to the window's end) and appends the ONE ledger
// record the dispatch owes. Blocking — watchAnswerEpisode spawns it.
func (w answerEpisodeWatch) run(ctx context.Context, projectID ProjectID, targetRef string, handle agenticjob.PipelineHandle) {
	if w.pipeline == nil || w.episodes == nil {
		return
	}
	watchCtx, cancelWatch := context.WithTimeout(ctx, w.window)
	defer cancelWatch()

	obs, terminal := w.observeToTerminal(watchCtx, handle)
	if terminal && episodeVenueIsRemote(obs.RunURL) {
		// Remote venue mines no episode in v1 — nothing was lost, so record nothing.
		return
	}
	rec := w.answerRecord(obs, terminal, targetRef, handle)

	// THE APPEND MUST NOT RIDE watchCtx. On the DEADLINE path observeToTerminal returned
	// precisely BECAUSE watchCtx expired, so appending under it would hand the ledger an
	// already-cancelled context — making the gap record the deadline exists to write the
	// one write guaranteed to fail. Derive a fresh, cancellation-free budget from the
	// caller's context instead. (Today's AppendEpisode realisations ignore the context
	// entirely, so this is latent rather than live; a store that honours it would turn the
	// never-silent guarantee into a silent loss on exactly the path that needs it most.)
	appendCtx, cancelAppend := context.WithTimeout(context.WithoutCancel(ctx), answerEpisodeAppendWindow)
	defer cancelAppend()
	w.appendRecord(appendCtx, projectID, rec, handle)
}

// appendRecord writes the record with a small BOUNDED retry. The workflow-side capture
// gets Temporal's retry envelope for free; this path has none, so without it a single
// transient store stumble would lose the episode outright.
func (w answerEpisodeWatch) appendRecord(ctx context.Context, projectID ProjectID, rec episode.EpisodeRecord, handle agenticjob.PipelineHandle) {
	key := fwra.IdempotencyKey("answerEpisode:" + string(handle))
	var err error
	for attempt := 1; attempt <= answerEpisodeAppendAttempts; attempt++ {
		err = w.episodes.AppendEpisode(fwra.Context{Context: ctx, IdempotencyKey: key},
			episode.ProjectID(projectID), rec)
		if err == nil {
			return
		}
		if attempt == answerEpisodeAppendAttempts ||
			!waitOrDone(ctx, time.Duration(attempt)*answerEpisodeAppendBackoff) {
			break
		}
	}
	w.log.Error("answer-job episode NOT recorded: ledger append failed after its bounded retry",
		"episodeId", rec.EpisodeID, "attempts", answerEpisodeAppendAttempts, "err", err.Error())
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// waitOrDone sleeps for d, returning false the moment ctx is done instead.
func waitOrDone(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// observeToTerminal polls the dispatched job until it reaches a terminal phase, the
// window closes, or the RA faults. terminal=false means the second or third — the caller
// turns that into a gap record.
func (w answerEpisodeWatch) observeToTerminal(ctx context.Context, handle agenticjob.PipelineHandle) (agenticjob.PipelineObservation, bool) {
	var last agenticjob.PipelineObservation
	cancelGrace := 0
	for {
		obs, err := w.pipeline.ObserveAgenticJob(fwra.Context{Context: ctx}, handle)
		if err != nil {
			return last, false
		}
		last = obs
		if terminal, done := w.classify(obs, &cancelGrace); done {
			return obs, terminal
		}
		if !waitOrDone(ctx, w.poll) {
			return last, false
		}
	}
}

// classify decides whether THIS observation ends the watch. A terminal observation with a
// summary always does. A terminal observation WITHOUT one ends it too — except for the
// CANCEL RACE, where the phase flips synchronously while the agent subprocess is still
// unwinding: that gets maxLateEpisodePolls further polls (the same grace the workflow-side
// capture gives it) before the run is written off.
func (w answerEpisodeWatch) classify(obs agenticjob.PipelineObservation, cancelGrace *int) (terminal, done bool) {
	if !designPipelinePhase(obs.Phase).IsTerminal() {
		return false, false
	}
	if obs.Episode != nil || obs.Phase != agenticjob.PhaseCancelled {
		return true, true
	}
	if *cancelGrace >= maxLateEpisodePolls {
		return true, true
	}
	*cancelGrace++
	return false, false
}

// answerRecord composes the ledger record for a watched answer job: the mined summary, or
// an explicit GAP naming which of the two ways it went missing.
func (w answerEpisodeWatch) answerRecord(obs agenticjob.PipelineObservation, terminal bool, targetRef string, handle agenticjob.PipelineHandle) episode.EpisodeRecord {
	// Lineage is nil BY DESIGN: this dispatch has no durable execution behind it.
	if terminal && obs.Episode != nil {
		return episodeRecordFromSummary(*obs.Episode, episode.EpisodeKindAnswer, targetRef, nil, obs.Diagnostic)
	}
	reason := episodeMissingSummaryReason
	if !terminal {
		reason = "answer job did not reach a terminal phase within the manager-side watch window"
	}
	return episodeGapRecord(episode.EpisodeKindAnswer, targetRef, nil,
		"gap-"+episodeIDSafe(string(handle)),
		episodeGapReason(reason, obs.Diagnostic), time.Now().UTC())
}

// ---------------------------------------------------------------------------
// Episode record composition — shared by the workflow-side capture
// (coauthorartifact.go) and the answer-job watch above.
// ---------------------------------------------------------------------------

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeMissingSummaryReason is the GapReason for the "the run terminated and reported
// no episode at all" case — the one the never-silent rule exists for.
const episodeMissingSummaryReason = "terminal observation carried no episode summary"

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeRecordFromSummary copies a mined EpisodeSummary onto an EpisodeRecord field for
// field — VERBATIM, no recomputation — and stamps the Manager-known Kind/TargetRef/
// Lineage the RA cannot know. WorkerClass is left unset: a design dispatch carries the
// artifact kind and the job mode, never the Phase-2 activity list's workerClass, so there
// is no honest value to put here. diagnostic supplies the GapReason when the RA itself
// reported a GAP outcome (a restart-lost run recovered from its orphaned trace) — the
// observation's diagnostic IS the explanation there, since EpisodeSummary carries no
// reason field.
func episodeRecordFromSummary(s agenticjob.EpisodeSummary, kind episode.EpisodeKind, targetRef string, lineage *episode.EpisodeLineage, diagnostic string) episode.EpisodeRecord {
	rec := episode.EpisodeRecord{
		EpisodeID:      s.EpisodeID,
		Kind:           kind,
		TargetRef:      targetRef,
		Lineage:        lineage,
		Model:          s.Model,
		Usage:          episode.EpisodeUsage(s.Usage),
		CostUSD:        s.CostUSD,
		NumTurns:       s.NumTurns,
		ToolCallCounts: s.ToolCallCounts,
		SubagentSpans:  episodeSubagentSpans(s.SubagentSpans),
		StartedAt:      s.StartedAt,
		EndedAt:        s.EndedAt,
		Outcome:        episodeOutcomeFrom(s.Outcome),
		TracePath:      s.TracePath,
	}
	if s.StreamedUsage != nil {
		u := episode.EpisodeUsage(*s.StreamedUsage)
		rec.StreamedUsage = &u
	}
	if rec.Outcome == episode.EpisodeGap {
		reason := episodeGapReason("the run reported a gap episode", diagnostic)
		rec.GapReason = &reason
	}
	return rec
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeGapRecord composes the SYNTHESIZED gap record for a dispatch that produced no
// summary at all. now is supplied by the caller (workflow.Now on the replay-deterministic
// workflow paths) because the run's own clock is exactly what was lost.
func episodeGapRecord(kind episode.EpisodeKind, targetRef string, lineage *episode.EpisodeLineage, episodeID, reason string, now time.Time) episode.EpisodeRecord {
	return episode.EpisodeRecord{
		EpisodeID: episodeID,
		Kind:      kind,
		TargetRef: targetRef,
		Lineage:   lineage,
		StartedAt: now,
		EndedAt:   now,
		Outcome:   episode.EpisodeGap,
		GapReason: &reason,
	}
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeGapReason joins the Manager's own reason to the observation's diagnostic when
// the RA supplied one, so a gap says both WHAT was lost and what the rail reported.
func episodeGapReason(reason, diagnostic string) string {
	if strings.TrimSpace(diagnostic) == "" {
		return reason
	}
	return reason + " — " + diagnostic
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeSubagentSpans re-types the mined subagent spans onto the ledger contract's own
// span type (identical shapes, distinct contracts — contracts are self-contained).
func episodeSubagentSpans(in []agenticjob.SubagentSpan) []episode.SubagentSpan {
	if len(in) == 0 {
		return nil
	}
	out := make([]episode.SubagentSpan, 0, len(in))
	for _, s := range in {
		out = append(out, episode.SubagentSpan(s))
	}
	return out
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeOutcomeFrom maps the observation contract's outcome onto the ledger contract's.
// Written as a TOTAL switch rather than a numeric cast so a future divergence between the
// two independently-versioned contracts is a compile-time conversation, not silent drift.
func episodeOutcomeFrom(o agenticjob.EpisodeOutcome) episode.EpisodeOutcome {
	switch o {
	case agenticjob.EpisodeSucceeded:
		return episode.EpisodeSucceeded
	case agenticjob.EpisodeFailed:
		return episode.EpisodeFailed
	case agenticjob.EpisodeCancelled:
		return episode.EpisodeCancelled
	case agenticjob.EpisodeGap:
		return episode.EpisodeGap
	default:
		return episode.EpisodeGap
	}
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeIDSafe rewrites s into the [A-Za-z0-9._-] alphabet episodeAccess requires of an
// EpisodeID. A rejected id is ContractMisuse — non-retryable — so a gap record seeded from
// a raw pipeline handle (which carries a ':') would be dropped on the floor, exactly
// defeating the never-silent rule the gap record exists to serve.
func episodeIDSafe(s string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, s)
	if safe == "" {
		return "unknown"
	}
	return safe
}

// catalog.go holds the three CATALOG / cross-phase typed-read ops folded onto the
// systemDesignManager from the former projectManager (dissolved 2026-06-28): a
// project's permanent identity IS its living system design, so the project CATALOG
// + the cross-phase typed head-state read belong on this Manager. These ops own NO
// Temporal workflow; they are thin synchronous reads/writes over the published
// projectStateAccess (head state), sourceControlAccess (project-birth adopt + seat),
// and the estimationEngine (compute-at-read CPM + EV/SPI).
//
// SCHEMA-FIRST: the public surface (the 3 ops + the ProjectState projection types)
// is GENERATED into contract.gen.go from project.json .serviceContracts; this file
// is the hand-written impl on the unexported *systemDesignManager. The generated
// contract imports neither projectstate nor Temporal — the aggregate value shapes
// are field-mapped to the Manager's OWN contract types at the boundary, and the
// per-slot artifact MODEL is carried OPAQUELY as an {kind, raw-json} envelope.

// CreateProject births a new project. NAME-AS-IDENTITY (C-PM-Δ): the USER supplies
// the repo name, which IS the project identity (project name == repo name). The
// supplied name is validated, then — IN ORDER, preserving the I-RA call-order
// guarantee + idempotent re-convergence — the Manager:
//
//  1. ADOPTS the user's existing repo (sourceControlAccess.AdoptProjectRepo).
//  2. SEATS the agentic-design workflow file: mint a short-lived credential, then
//     commit the claude-code-action DESIGN workflow file.
//  3. creates the head-state row (projectStateAccess.CreateProject), STRICTLY AFTER
//     the above, keyed on the repo name as identity.
//
// Returns the project id (== the adopted repo name). Validation errors (empty
// owner/name) surface as ContractMisuse before any RA call. Every write is idempotent
// — a retry after a partial failure RE-CONVERGES rather than duplicating. The rail
// (sourceControlAccess) is optional: nil ⇒ repo-less create (a dev server with no
// GitHub App credentials).
func (m *deliveryManager) CreateProject(rc fwmanager.Context, owner OwnerScope, name string) (ProjectID, error) {
	ctx := rc.Context
	if owner == "" {
		return "", newError(fwmanager.ContractMisuse, "empty owner")
	}
	if name == "" {
		return "", newError(fwmanager.ContractMisuse, "empty name")
	}

	// NAME-AS-IDENTITY: the user-supplied name IS the project identity == repo name.
	projectID := ProjectID(name)
	key := createProjectIdempotencyKey(projectID)

	// Adopt the user's existing repo + seat the workflow file FIRST (project birth,
	// before the head-state row). Skipped only when source-control is unconfigured
	// (nil) — a repo-less dev server. Every step is idempotent; a retry re-converges.
	if m.rail != nil {
		repo, err := m.rail.AdoptProjectRepo(fwra.Context{Context: ctx, IdempotencyKey: key}, sourcecontrol.RepoAdoptionSpec{
			RepoName: name, // name-as-identity: the project id IS the repo name
			Title:    name,
		})
		if err != nil {
			return "", sdMapRAError(err, "sourceControlAccess.AdoptProjectRepo")
		}
		cred, err := m.rail.GetInstallationToken(fwra.Context{Context: ctx}, repo)
		if err != nil {
			return "", sdMapRAError(err, "sourceControlAccess.GetInstallationToken")
		}
		files, err := sourcecontrol.ManagedScaffoldFiles(repo, sourcecontrol.RailAppSlug(m.rail))
		if err != nil {
			return "", sdMapRAError(err, "sourceControlAccess.ManagedScaffoldFiles")
		}
		if _, err := m.rail.CommitManagedFiles(fwra.Context{Context: ctx, IdempotencyKey: key}, repo, files, cred); err != nil {
			return "", sdMapRAError(err, "sourceControlAccess.CommitManagedFiles")
		}
	}

	if _, err := m.projectState.CreateProject(fwra.Context{Context: ctx, IdempotencyKey: key},
		projectstate.ProjectID(projectID), projectstate.OwnerScope(owner), name); err != nil {
		return "", sdMapRAError(err, "projectStateAccess.CreateProject")
	}
	return projectID, nil
}

// SetOperatingModel records the project-level WHO-OPERATES choice (founder ruling
// 2026-07-05). SYNCHRONOUS, non-Temporal, mirroring SetResearchInput: a single
// idempotent head-state write via projectStateAccess.SetOperatingModel with a bounded
// sync optimistic-concurrency loop (re-read the head Version, re-apply on Conflict). The
// UI/MCP calls it at creation — after CreateProject, before StartSystemDesign — to pick
// self-operated (the default the project is born with) or archistrator-operated (which
// constrains the deployment design to the platform palette). Returns the head Version.
func (m *deliveryManager) SetOperatingModel(rc fwmanager.Context, projectID ProjectID, model OperatingModel) (Version, error) {
	ctx := rc.Context
	if projectID == "" {
		return 0, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	psModel := projectstate.OperatingModel(string(model))
	if !psModel.Valid() {
		return 0, newError(fwmanager.ContractMisuse, fmt.Sprintf("unknown operating model %q", string(model)))
	}

	key := fwra.IdempotencyKey(fmt.Sprintf("%s:setOperatingModel:%s", projectID, model))
	psID := projectstate.ProjectID(projectID)

	var lastErr error
	for range setOperatingModelMaxAttempts {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			return 0, sdMapRAError(err, "projectStateAccess.ReadProject")
		}
		newVersion, err := m.projectState.SetOperatingModel(fwra.Context{Context: ctx, IdempotencyKey: key}, psID, proj.Version, psModel)
		if err == nil {
			return Version(newVersion), nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue // re-read head Version, re-apply (same idempotencyKey)
		}
		return 0, sdMapRAError(err, "projectStateAccess.SetOperatingModel")
	}
	return 0, fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "projectStateAccess.SetOperatingModel: exhausted conflict retries")
}

// setOperatingModelMaxAttempts bounds the sync-path re-read/re-apply loop.
const setOperatingModelMaxAttempts = 5

// createProjectIdempotencyKey derives the stable logical idempotency key for "create
// this project". The project id IS the user-supplied repo name and unique per
// project, so it is itself the natural dedup token.
func createProjectIdempotencyKey(projectID ProjectID) fwra.IdempotencyKey {
	return fwra.IdempotencyKey(fmt.Sprintf("%s:createProject", projectID))
}

// ListProjects returns the landing-grid catalog for owner, newest-first (the RA's
// ordering). A pass-through over projectStateAccess.ListProjects, mapped to the
// contract ProjectSummary.
func (m *deliveryManager) ListProjects(rc fwmanager.Context, owner OwnerScope) ([]ProjectSummary, error) {
	ctx := rc.Context
	if owner == "" {
		return nil, newError(fwmanager.ContractMisuse, "empty owner")
	}
	summaries, err := m.projectState.ListProjects(fwra.Context{Context: ctx}, projectstate.OwnerScope(owner))
	if err != nil {
		return nil, sdMapRAError(err, "projectStateAccess.ListProjects")
	}
	out := make([]ProjectSummary, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, summaryToContract(s))
	}
	return out, nil
}

// GetProject returns the full typed head-state for one project, mapping the
// projectstate.Project aggregate's named typed slots into the contract ProjectState.
// fwra.NotFound passes through as fwmanager.NotFound.
func (m *deliveryManager) GetProject(rc fwmanager.Context, projectID ProjectID) (ProjectState, error) {
	ctx := rc.Context
	if projectID == "" {
		return ProjectState{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		// A NotFound for an unknown project must NOT leak the internal git call chain
		// (e.g. "resourceaccess: github.GitStore.clone: repository not found: repository
		// not found: Repository not found." — the message stutters as each layer re-wraps
		// its own "not found" text). Map it to a single, clean, project-scoped Detail;
		// the full cause chain is preserved on Cause for the server-side log.
		if raErr := (*fwra.Error)(nil); errors.As(err, &raErr) && raErr.Kind == fwra.NotFound {
			return ProjectState{}, fwmanager.Wrap(fwmanager.NotFound, err, fmt.Sprintf("project %q not found", projectID))
		}
		return ProjectState{}, sdMapRAError(err, "projectStateAccess.ReadProject")
	}
	m.computeNetworkAtRead(&proj)
	m.computeDeploymentEdgesAtRead(&proj)
	return m.projectStateToContract(proj), nil
}

// GetDesignHealth returns the LIVE design-health read-model for one project: the
// mechanical Method-rule findings evaluated render-on-read over the COMMITTED
// project.json, PLUS the committed waiver / attestation ledgers, stamped with the
// state revision the findings ran against. It never mutates state: a clean design
// returns empty finding/waiver/attestation slices. This is the getDesignHealth
// VIEW op (ui.view "design-health" — the view slug, not the component id); the
// DesignHealthEngine call below is the code that BACKS the
// SystemDesignManager → DesignHealthEngine architecture edge, an ordinary
// downward M→E call.
func (m *deliveryManager) GetDesignHealth(rc fwmanager.Context, projectID ProjectID) (DesignHealth, error) {
	ctx := rc.Context
	if projectID == "" {
		return DesignHealth{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		// Same clean NotFound mapping as GetProject — do not leak the git call chain.
		if raErr := (*fwra.Error)(nil); errors.As(err, &raErr) && raErr.Kind == fwra.NotFound {
			return DesignHealth{}, fwmanager.Wrap(fwmanager.NotFound, err, fmt.Sprintf("project %q not found", projectID))
		}
		return DesignHealth{}, sdMapRAError(err, "projectStateAccess.ReadProject")
	}

	// Live findings: re-serialize the committed aggregate to its canonical
	// project.json bytes (the same bytes-in contract putDraftModel and CI feed) and
	// run the shared live-tier rule engine over them.
	raw, err := projectstate.EncodeProjectJSON(proj)
	if err != nil {
		return DesignHealth{}, fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess.EncodeProjectJSON")
	}
	live, err := m.designHealth.EvaluateDesignHealth(fweng.Context{Context: ctx}, raw)
	if err != nil {
		return DesignHealth{}, fwmanager.Wrap(fwmanager.Infrastructure, err, "designHealthEngine.EvaluateDesignHealth")
	}
	findings := findingsToContract(live)

	// Committed ledgers: waivers live on BOTH the systemDesign slot (App-C standard
	// items) and the volatilities slot; attestations live on the systemDesign slot.
	// Initialized non-nil so the required contract arrays serialize as [] not null.
	waivers := []CheckItem{}
	attestations := []CheckItem{}
	if sys, ok := slotFor(proj, KindSystem).Model.(*projectstate.System); ok && sys != nil {
		waivers = append(waivers, checkItemsToContract(sys.Waivers)...)
		attestations = append(attestations, checkItemsToContract(sys.Attestations)...)
	}
	if vol, ok := slotFor(proj, KindVolatilities).Model.(*projectstate.Volatilities); ok && vol != nil {
		waivers = append(waivers, checkItemsToContract(vol.Waivers)...)
	}

	return DesignHealth{
		Findings:            findings,
		Waivers:             waivers,
		Attestations:        attestations,
		EvaluatedAtRevision: int64(proj.Version),
	}, nil
}

// findingsToContract maps the platform methodcheck.Finding values the live-tier
// rule engine mints into the systemDesignManager contract Finding VIEW shape. The
// slice is always non-nil so the required contract array serializes as [] not null.
func findingsToContract(in []methodcheck.Finding) []Finding {
	out := make([]Finding, 0, len(in))
	for _, f := range in {
		fc := Finding{
			RuleID:   RuleID(f.RuleID),
			Severity: severityToContract(f.Severity),
			Message:  f.Message,
		}
		if f.Location != nil {
			fc.Location = &Location{Ordinal: int64(f.Location.Ordinal), Section: f.Location.Section}
		}
		out = append(out, fc)
	}
	return out
}

// severityToContract renders a methodcheck severity ordinal as its VIEW string
// (info/warning/error) — the contract Severity is a string enum where methodcheck's
// is an int, so the value is translated by the same names the wire form uses.
func severityToContract(s methodcheck.Severity) Severity {
	switch s {
	case methodcheck.SeverityError:
		return SeverityError
	case methodcheck.SeverityWarning:
		return SeverityWarning
	case methodcheck.SeverityInfo:
		return SeverityInfo
	default:
		return SeverityInfo
	}
}

// checkItemsToContract maps committed RA CheckItem ledger entries (waivers /
// attestations) to the contract CheckItem VIEW shape, translating the RA int
// CheckStatus enum to its wire string exactly as GetProject converts its enums.
func checkItemsToContract(in []projectstate.CheckItem) []CheckItem {
	out := make([]CheckItem, 0, len(in))
	for _, it := range in {
		out = append(out, CheckItem{
			Section:       it.Section,
			Guideline:     it.Guideline,
			Status:        checkStatusToView(it.Status),
			Justification: it.Justification,
		})
	}
	return out
}

// checkStatusToView maps the RA CheckStatus int enum to its VIEW wire string,
// mirroring projectstate's own checkStatusNames (pass/waived/fail).
func checkStatusToView(s projectstate.CheckStatus) string {
	switch s {
	case projectstate.CheckWaived:
		return "waived"
	case projectstate.CheckFail:
		return "fail"
	case projectstate.CheckPass:
		return "pass"
	default:
		return "pass"
	}
}

// sdMapRAError translates a projectStateAccess / sourceControlAccess error into the
// Manager façade error model. fwra.NotFound → NotFound; fwra.ContractMisuse →
// ContractMisuse; everything else (incl. Conflict — a thin read/catalog op has no
// optimistic-concurrency loop to recover it) → Infrastructure with the original
// retryability preserved. label identifies the ACTUAL failing dependency+op (e.g.
// "sourceControlAccess.AdoptProjectRepo") — CreateProject fans across two RAs, so a
// fixed label would misattribute a source-control fault to projectStateAccess. It is
// the opaque Detail returned to the client; the full cause chain stays server-side
// (Cause), surfaced only in the composition-root log.
func sdMapRAError(err error, label string) error {
	if err == nil {
		return nil
	}
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		case fwra.Unknown, fwra.Transient, fwra.RateLimited, fwra.Infrastructure,
			fwra.Auth, fwra.Conflict, fwra.QuotaExhausted, fwra.ContentPolicy:
			// "Everything else... → Infrastructure" per the doc comment above.
			mapped := fwmanager.Wrap(fwmanager.Infrastructure, err, label)
			mapped.Retryable = raErr.Retryable
			return mapped
		default:
			mapped := fwmanager.Wrap(fwmanager.Infrastructure, err, label)
			mapped.Retryable = raErr.Retryable
			return mapped
		}
	}
	// A non-fwra error (e.g. ManagedScaffoldFiles scaffold assembly) still carries
	// its cause for the server log while keeping the client Detail opaque (label).
	return fwmanager.Wrap(fwmanager.Infrastructure, err, label)
}

// computeDeploymentEdgesAtRead populates each deployment environment's
// COMPUTE-AT-READ block with the relationships derived from the committed System
// model. NO-OP when either slot is absent — a project that has not reached its
// architecture yet has nothing to derive from, and the authored edges (if any)
// still serve on their own.
func (m *deliveryManager) computeDeploymentEdgesAtRead(p *projectstate.Project) {
	if m.designHealth == nil {
		return
	}
	op, ok := p.OperationalConcepts.Model.(*projectstate.DeploymentOperationsModel)
	if !ok || op == nil || len(op.Deployment.Environments) == 0 {
		return
	}
	sys, ok := p.SystemDesign.Model.(*projectstate.System)
	if !ok || sys == nil {
		return
	}

	derived, err := m.designHealth.DeriveDeploymentEdges(
		fweng.Context{Context: context.Background()},
		toMethodcheckSystem(*sys),
		toMethodcheckTopology(op.Deployment),
	)
	if err != nil {
		return // degenerate input guard — serve the authored edges unenriched
	}

	for i := range op.Deployment.Environments {
		env := &op.Deployment.Environments[i]
		edges, has := derived[env.Profile.String()]
		if !has {
			env.Computed = nil
			continue
		}
		env.Computed = &projectstate.DeploymentEnvironmentComputed{
			DerivedRelationships: fromMethodcheckRelationships(edges),
		}
	}
}

// toMethodcheckSystem maps the committed System onto the platform's string-typed
// model. Only the fields derivation reads are carried: it joins components to
// containers by NAME and to infrastructure by name slug, and needs each
// component's kind to exempt the utilities.
func toMethodcheckSystem(s projectstate.System) methodcheck.System {
	out := methodcheck.System{
		Components:    make([]methodcheck.Component, 0, len(s.Components)),
		Relationships: make([]methodcheck.Relationship, 0, len(s.Relationships)),
	}
	for _, c := range s.Components {
		out.Components = append(out.Components, methodcheck.Component{
			ID: c.ID, Name: c.Name, Kind: c.Kind.String(),
		})
	}
	for _, r := range s.Relationships {
		out.Relationships = append(out.Relationships, methodcheck.Relationship{
			From: r.From, To: r.To, Mode: r.Mode.String(), Label: r.Label,
		})
	}
	return out
}

// toMethodcheckTopology maps the committed deployment topology onto the
// platform's model. The AUTHORED relationships are deliberately NOT carried:
// derivation must not see them, or a re-read would fold its own previous output
// back into its input.
func toMethodcheckTopology(t projectstate.DeploymentTopology) methodcheck.DeploymentTopology {
	out := methodcheck.DeploymentTopology{
		DeliveryStyle: t.DeliveryStyle.String(),
		Containers:    make([]methodcheck.DeployContainer, 0, len(t.Containers)),
		Environments:  make([]methodcheck.DeploymentEnvironment, 0, len(t.Environments)),
	}
	for _, c := range t.Containers {
		out.Containers = append(out.Containers, methodcheck.DeployContainer{
			Key: c.Key, Name: c.Name, Technology: c.Technology,
			Description: c.Description, Components: c.Components,
			Surface: string(c.Surface),
		})
	}
	for _, env := range t.Environments {
		out.Environments = append(out.Environments, methodcheck.DeploymentEnvironment{
			Profile: env.Profile.String(),
			Title:   env.Title,
			Nodes:   toMethodcheckNodes(env.Nodes),
		})
	}
	return out
}

func toMethodcheckNodes(nodes []projectstate.DeploymentNode) []methodcheck.DeploymentNode {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]methodcheck.DeploymentNode, 0, len(nodes))
	for _, n := range nodes {
		mapped := methodcheck.DeploymentNode{
			Key: n.Key, Name: n.Name, Technology: n.Technology,
			Description: n.Description, Instances: n.Instances,
			Children: toMethodcheckNodes(n.Children),
		}
		for _, ci := range n.ContainerInstances {
			mapped.ContainerInstances = append(mapped.ContainerInstances, methodcheck.ContainerInstance{
				Key: ci.Key, ContainerKey: ci.ContainerKey, Note: ci.Note,
			})
		}
		for _, in := range n.InfrastructureNodes {
			mapped.InfrastructureNodes = append(mapped.InfrastructureNodes, methodcheck.InfrastructureNode{
				Key: in.Key, Name: in.Name, Technology: in.Technology,
				Description: in.Description, Role: string(in.Role),
			})
		}
		for _, ss := range n.SoftwareSystemInstances {
			mapped.SoftwareSystemInstances = append(mapped.SoftwareSystemInstances, methodcheck.SoftwareSystemInstance{
				Key: ss.Key, Name: ss.Name, Technology: ss.Technology,
				Description: ss.Description, Role: string(ss.Role),
			})
		}
		out = append(out, mapped)
	}
	return out
}

// fromMethodcheckRelationships maps the derived edges back onto the served model.
// The mode round-trips through the wire name so the served edge carries the same
// sync/queued vocabulary the System relationship did.
func fromMethodcheckRelationships(edges []methodcheck.DeploymentRelationship) []projectstate.DeploymentRelationship {
	out := make([]projectstate.DeploymentRelationship, 0, len(edges))
	for _, e := range edges {
		out = append(out, projectstate.DeploymentRelationship{
			From: e.From, To: e.To, Label: e.Label, Technology: e.Technology,
			Mode: callModeFromWireName(e.Mode),
		})
	}
	return out
}

// callModeFromWireName resolves the platform model's camelCase mode name back to
// the typed CallMode, defaulting to sync for a name this build does not know.
func callModeFromWireName(name string) projectstate.CallMode {
	switch name {
	case "queued":
		return projectstate.CallQueued
	case "eventPubSub":
		return projectstate.CallEventPubSub
	default:
		return projectstate.CallSync
	}
}

// ---------------------------------------------------------------------------
// Compute-at-read enrichment (INTERNAL impl). Operates on the projectstate.Project
// aggregate BEFORE mapping to the contract.
// ---------------------------------------------------------------------------

// computeNetworkAtRead populates the Network slot's COMPUTE-AT-READ block (per-node CPM
// figures, criticality bands, milestone event times, summary) by running the
// estimationEngine.ComputeNetwork over the AUTHORED network × activity list.
// NO-OP when the estimator is nil or the Network slot has no authored model.
func (m *deliveryManager) computeNetworkAtRead(p *projectstate.Project) {
	if m.estimator == nil {
		return
	}
	net, ok := p.Network.Model.(*projectstate.Network)
	if !ok || net == nil {
		return
	}
	var activities projectstate.ActivityList
	if al, alok := p.ActivityList.Model.(*projectstate.ActivityList); alok && al != nil {
		activities = *al
	}

	solution, err := m.estimator.ComputeNetwork(fweng.Context{Context: context.Background()}, toEstimationActivityList(activities), toEstimationNetwork(*net))
	if err != nil {
		return // degenerate input guard — serve the authored network unenriched
	}

	computed := make(map[string]projectstate.NetworkNodeCompute, len(solution.Nodes))
	for id, n := range solution.Nodes {
		computed[id] = projectstate.NetworkNodeCompute{
			EarliestStart:  n.EarliestStart,
			EarliestFinish: n.EarliestFinish,
			LatestStart:    n.LatestStart,
			LatestFinish:   n.LatestFinish,
			TotalFloat:     n.TotalFloat,
			FreeFloat:      n.FreeFloat,
			OnCriticalPath: n.OnCriticalPath,
			NearCritical:   n.NearCritical,
			Band:           n.Band,
			Column:         int(n.Column),
		}
	}
	net.Computed = computed

	// Overwrite the served criticalPath[] with the engine's computed float-0 ACTIVITY
	// set (the authored criticalPath[] may be stale). Sorted for a deterministic wire order.
	computedCP := make([]string, 0, len(solution.Nodes))
	for id, n := range solution.Nodes {
		if n.OnCriticalPath {
			computedCP = append(computedCP, id)
		}
	}
	sort.Strings(computedCP)
	net.CriticalPath = computedCP

	net.Summary = &projectstate.NetworkSummary{
		TotalDurationDays:         solution.Summary.TotalDurationDays,
		CriticalPathActivityCount: int(solution.Summary.CriticalPathActivityCount),
		CriticalPathDays:          solution.Summary.CriticalPathDays,
		MaxFloat:                  solution.Summary.MaxFloat,
		NearCriticalCount:         int(solution.Summary.NearCriticalCount),
	}

	// Merge the computed milestone facets back onto the authored milestone rows (matched
	// by id), preserving authored id/name/public/dependsOn order.
	computedByID := make(map[string]estimation.NetworkMilestoneSolution, len(solution.Milestones))
	for _, ms := range solution.Milestones {
		computedByID[ms.ID] = ms
	}
	for i := range net.Milestones {
		if ms, found := computedByID[net.Milestones[i].ID]; found {
			onCP := ms.OnCriticalPath
			event := ms.EventTime
			net.Milestones[i].OnCriticalPath = &onCP
			net.Milestones[i].EventTime = &event
		}
	}
}

// toEstimationActivityList converts the canonical projectstate.ActivityList to the
// estimationEngine's OWN SLIM ActivityList at the call boundary.
func toEstimationActivityList(al projectstate.ActivityList) estimation.ActivityList {
	out := estimation.ActivityList{Activities: make([]estimation.ActivityItem, 0, len(al.Activities))}
	for _, a := range al.Activities {
		out.Activities = append(out.Activities, estimation.ActivityItem{Name: a.Name, EffortDays: a.EffortDays})
	}
	return out
}

// toEstimationNetwork converts the canonical projectstate.Network to the
// estimationEngine's OWN SLIM Network at the call boundary.
func toEstimationNetwork(net projectstate.Network) estimation.Network {
	deps := make([]estimation.NetworkDependency, 0, len(net.Dependencies))
	for _, d := range net.Dependencies {
		deps = append(deps, estimation.NetworkDependency{Activity: d.Activity, DependsOn: d.DependsOn})
	}
	var milestones []estimation.NetworkMilestone
	if len(net.Milestones) > 0 {
		milestones = make([]estimation.NetworkMilestone, 0, len(net.Milestones))
		for _, mlst := range net.Milestones {
			milestones = append(milestones, estimation.NetworkMilestone{Id: mlst.ID, DependsOn: mlst.DependsOn})
		}
	}
	return estimation.Network{Dependencies: deps, Milestones: milestones}
}

// ---------------------------------------------------------------------------
// projectstate → contract conversions (the Manager boundary).
// ---------------------------------------------------------------------------

// phaseLabels is the SINGLE SOURCE OF TRUTH mapping the 0-indexed project
// lifecycle Phase to its human-readable label (PM-P2-5: clients kept misreading
// the bare int). Kept aligned with the Phase enum in contract.gen.go — 0/1/2.
var phaseLabels = map[Phase]string{
	PhaseSystemDesign:  "system-design",
	PhaseProjectDesign: "project-design",
	PhaseConstruction:  "construction",
}

// phaseName returns the human-readable label for a Phase, or "" when the phase
// is outside the known 0/1/2 range — a map miss yields the zero value, so an
// out-of-range Phase reads as empty rather than a fabricated label.
func phaseName(p Phase) string {
	return phaseLabels[p]
}

// summaryToContract maps a projectstate.ProjectSummary onto the contract ProjectSummary.
func summaryToContract(s projectstate.ProjectSummary) ProjectSummary {
	phase := Phase(int(s.Phase))
	return ProjectSummary{
		ProjectID: ProjectID(s.ProjectID),
		Name:      s.Name,
		Owner:     OwnerScope(s.Owner),
		Phase:     phase,
		PhaseName: phaseName(phase),
		// ConstructionComplete (Task 13 fix round 1): the RA-level derived flag
		// (projectstate.isConstructionComplete, surfaced on ListProjects) carried
		// straight through — this manager's ProjectSummary is what the SPA catalog
		// actually reads (the RA's own ProjectSummary never crosses the client
		// boundary), so the field must be copied here for the browser to see it.
		ConstructionComplete: s.ConstructionComplete,
		CommittedCount:       int64(s.CommittedCount),
		TotalCount:           int64(s.TotalCount),
		UpdatedAt:            s.UpdatedAt,
	}
}

// projectStateToContract maps the head-state Project aggregate to the contract
// ProjectState transport shape. Read-time projections (each git row's prUrl/prNumber
// composed from the per-project repo base + the opaque ref, and the EV/SPI earned-value
// curve from m.estimator) are sourced server-side here rather than re-derived by the webClient.
func (m *deliveryManager) projectStateToContract(p projectstate.Project) ProjectState {
	phase := Phase(int(p.Phase))
	return ProjectState{
		ProjectID: ProjectID(p.ID),
		Name:      p.Name,
		Owner:     OwnerScope(p.Owner),
		Phase:     phase,
		PhaseName: phaseName(phase),
		Version:   int64(p.Version),
		// OrDefault: a pre-field project (empty model) reads as self-operated on the
		// wire — the back-compat default — so the SPA never sees an empty operating model.
		OperatingModel:      OperatingModel(string(p.OperatingModel.OrDefault())),
		Research:            researchToContract(p.Research),
		Slots:               slotsToContract(p),
		GitRows:             m.gitRowsToContract(ProjectID(p.ID), p.ActivityGit),
		ActivityExecution:   constructionRowsToContract(p.ActivityExecution, activityMetaByID(p), componentLayerByID(p), constructionPlanFor(p)),
		ConstructionStarted: constructionStartedFor(p.ActivityExecution),
		// The recorded operator pause, passed through as stored (plan B1.7): the console
		// offers Resume in Begin's place while it holds.
		OperatorPaused:       p.OperatorPaused,
		PauseReason:          pauseReasonToContract(p.PauseReason),
		ConstructionProgress: m.constructionProgressToContract(p),
		ServiceContracts:     serviceContractsToContract(p.ServiceContracts),
		ReviewPolicy:         reviewPolicyToContract(p.ReviewPolicy),
		TestingState:         testingStateToContract(p.TestingState),
	}
}

// pauseReasonToContract is the pause reason on the wire: omitted when there is none.
func pauseReasonToContract(reason string) *string {
	if reason == "" {
		return nil
	}
	return &reason
}

// testingStateToContract converts the head-state TestingState to the contract
// view. Returns nil when absent so the field is omitted from the read.
func testingStateToContract(ts *projectstate.TestingState) *TestingStateView {
	if ts == nil {
		return nil
	}
	runs := make([]TestRunView, len(ts.TestRuns))
	for i, r := range ts.TestRuns {
		runs[i] = TestRunView{Id: r.ID, Passed: int64(r.Passed), Failed: int64(r.Failed), Note: r.Note}
	}
	defects := make([]DefectView, len(ts.Defects))
	for i, d := range ts.Defects {
		defects[i] = DefectView{Id: d.ID, Title: d.Title, Severity: d.Severity, Note: d.Note}
	}
	return &TestingStateView{TestRuns: runs, Defects: defects, SystemTestPlan: systemTestPlanToContract(ts.SystemTestPlan)}
}

// systemTestPlanToContract maps the black-box operation-sequence scenarios of the
// system test plan. Returns nil when there is no plan or no scenarios (the plan's
// prose/index fields are not part of this view — only the renderable sequences).
func systemTestPlanToContract(p *projectstate.SystemTestPlan) *SystemTestPlanView {
	if p == nil || len(p.Scenarios) == 0 {
		return nil
	}
	scenarios := make([]TestScenarioView, len(p.Scenarios))
	for i, s := range p.Scenarios {
		cases := make([]TestCaseView, len(s.Cases))
		for j, c := range s.Cases {
			steps := make([]TestStepView, len(c.Steps))
			for k, st := range c.Steps {
				inputs := make([]TestArgView, len(st.Inputs))
				for m, a := range st.Inputs {
					inputs[m] = TestArgView{Name: a.Name, Value: a.Value, SchemaRef: a.SchemaRef}
				}
				steps[k] = TestStepView{
					Seq:       int64(st.Seq),
					Component: st.Component,
					Operation: st.Operation,
					Status:    st.Status,
					Inputs:    inputs,
					Expect:    TestExpectView{Result: st.Expect.Result, ErrorExpected: st.Expect.ErrorExpected, ErrorCode: st.Expect.ErrorCode},
					Assertion: st.Assertion,
				}
			}
			cases[j] = TestCaseView{Id: c.ID, Kind: c.Kind, Title: c.Title, Proves: c.Proves, ExpectedOutcome: c.ExpectedOutcome, Steps: steps}
		}
		scenarios[i] = TestScenarioView{Id: s.ID, UseCase: s.UseCase, Title: s.Title, Description: s.Description, Cases: cases}
	}
	return &SystemTestPlanView{Scenarios: scenarios}
}

// reviewPolicyToContract converts the head-state ReviewPolicy to the contract
// ReviewPolicyView. Returns nil when the policy is empty (no gates configured
// AND no preset chosen) — matching EncodeProject's own emptiness gate, so the
// webApp's preset control (local-merge-and-policy Commit 3) reads the committed
// preset back rather than always showing an unset dial.
func reviewPolicyToContract(p projectstate.ReviewPolicy) *ReviewPolicyView {
	if len(p.GatedPhasesByType) == 0 && p.Preset == nil {
		return nil
	}
	byType := make(map[string][]string, len(p.GatedPhasesByType))
	for typ, phases := range p.GatedPhasesByType {
		strs := make([]string, len(phases))
		for i, ph := range phases {
			strs[i] = string(ph)
		}
		byType[typ] = strs
	}
	return &ReviewPolicyView{GatedPhasesByType: byType, Preset: p.Preset}
}

// researchToContract maps the Phase-1 research corpus onto the read view. F22
// (read-model slimming): the corpus Content — a source can be a whole 660KB book —
// is deliberately NOT shipped on the project read. GetProject is polled at 1.5s by
// the construction console and paid on every HomeBase/design load, yet the SPA never
// renders corpus content; carrying it made a single read ~686KB. We keep the sources
// array shape (title stays, so the UI can list what is loaded) but EMPTY the content
// and surface each source's byte-size as ContentBytes so the UI can still show "N KB
// loaded". The full corpus is read from git by the design Action, not through this
// endpoint — see setResearchInput (write path) which is unchanged.
func researchToContract(r projectstate.ResearchCorpus) ResearchInput {
	sources := make([]ResearchSource, 0, len(r.Sources))
	for _, s := range r.Sources {
		// F42: the corpus is persisted as pointers now — ContentBytes comes straight off the
		// stored pointer (no Content to measure); Content stays empty on the read model.
		n := s.ContentBytes
		sources = append(sources, ResearchSource{Title: s.Title, Content: "", ContentBytes: &n})
	}
	return ResearchInput{Sources: sources}
}

// slotsToContract emits one ArtifactSlotView per defined ArtifactKind in the stable
// slot order, deriving each slot's Stage from its stored ArtifactReviewStatus and
// carrying its typed Model OPAQUELY (the {kind, raw-json} envelope).
func slotsToContract(p projectstate.Project) []ArtifactSlotView {
	kinds := projectstate.AllArtifactKinds()
	slots := make([]ArtifactSlotView, 0, len(kinds))
	for _, kind := range kinds {
		slot := slotForKind(p, kind)
		slots = append(slots, ArtifactSlotView{
			Kind:  kind.WireName(),
			Stage: stageForStatus(slot.Status),
			Model: encodeSlotModel(kind, slot.Model),
			Notes: notesPtr(slot.Notes),
			// F38: surface the staleness chip + the amendment (commit) count so the SPA can
			// flag "basis shifted — reconcile" and show the revision. Both omitempty on the wire.
			StaleBasis:      staleBasisPtr(slot.StaleBasis),
			StaleBasisCause: staleBasisCauseView(slot.StaleBasisCause),
			Revisions:       revisionsPtr(slot.Revisions),
			// PM-P2-4: surface the committed-slot provenance (who/when/rail) under the
			// committed strip. nil (omitempty) for uncommitted / pre-provenance slots.
			Provenance: provenanceView(slot.Provenance),
		})
	}
	return slots
}

// encodeSlotModel carries the slot's typed model OPAQUELY: the canonical camelCase
// kind wire name + the concrete model's own JSON (nil when the slot is empty).
func encodeSlotModel(kind projectstate.ArtifactKind, m projectstate.ArtifactModel) ArtifactSlotModel {
	env := ArtifactSlotModel{Kind: kind.WireName()}
	if m != nil {
		if raw, err := json.Marshal(m); err == nil {
			rm := json.RawMessage(raw)
			env.Model = &rm
		}
	}
	return env
}

// notesPtr maps an architect-notes string to the optional contract field.
func notesPtr(notes string) *string {
	if notes == "" {
		return nil
	}
	n := notes
	return &n
}

// staleBasisPtr surfaces the F38 staleness chip only when the slot is actually stale
// (omitempty on the wire: absent ⇒ not stale).
func staleBasisPtr(stale bool) *bool {
	if !stale {
		return nil
	}
	b := true
	return &b
}

// StaleCauseView is the read-model projection of projectstate.StaleCause: WHY a committed
// slot went stale (the upstream slot kind + its new revision), so the SPA can say
// "Volatilities rev 2 changed after this was committed". Absent when the slot is not stale
// or went stale before the cause was recorded (no back-fill).
type StaleCauseView struct {
	UpstreamKind     string `json:"upstreamKind"`
	UpstreamRevision int64  `json:"upstreamRevision"`
}

// staleBasisCauseView projects the stored stale-cause onto the read model, nil-safe
// (omitempty on the wire: absent ⇒ not stale or cause unknown).
func staleBasisCauseView(c *projectstate.StaleCause) *StaleCauseView {
	if c == nil {
		return nil
	}
	return &StaleCauseView{UpstreamKind: c.UpstreamKind, UpstreamRevision: c.UpstreamRevision}
}

// revisionsPtr surfaces the F38 commit/amendment count only once the slot has been
// committed at least once (omitempty on the wire: absent ⇒ 0).
func revisionsPtr(n int64) *int64 {
	if n == 0 {
		return nil
	}
	v := n
	return &v
}

// ProvenanceView is the read-model projection of projectstate.Provenance (PM-P2-4): WHO
// committed a slot / WHEN / which rail drafted it, so the SPA can render a muted
// "committed <date> · approved by X · drafted by Y" line under the committed strip. Absent
// (nil, omitempty on the wire) for an uncommitted slot or one committed before provenance
// was recorded (no back-fill). Each field is independently optional.
type ProvenanceView struct {
	CommittedAt string `json:"committedAt,omitempty"`
	ApprovedBy  string `json:"approvedBy,omitempty"`
	DraftedBy   string `json:"draftedBy,omitempty"`
}

// provenanceView projects the stored commit provenance onto the read model, nil-safe
// (omitempty on the wire: absent ⇒ not committed or provenance unknown).
func provenanceView(p *projectstate.Provenance) *ProvenanceView {
	if p == nil {
		return nil
	}
	return &ProvenanceView{CommittedAt: p.CommittedAt, ApprovedBy: p.ApprovedBy, DraftedBy: p.DraftedBy}
}

// stageForStatus maps the stored per-slot ArtifactReviewStatus to the contract stage.
func stageForStatus(s projectstate.ArtifactReviewStatus) ArtifactStage {
	switch s {
	case projectstate.ReviewNone:
		// No review has happened yet (slot not yet drafted) — same as the
		// fallback for any other not-yet-meaningful status.
		return ArtifactStageEmpty
	case projectstate.ReviewAwaitingReview:
		return ArtifactStageAwaitingReview
	case projectstate.ReviewCommitted:
		return ArtifactStageCommitted
	case projectstate.ReviewRejected:
		return ArtifactStageRejected
	case projectstate.ReviewWithdrawn:
		return ArtifactStageWithdrawn
	default:
		return ArtifactStageEmpty
	}
}

// gitRowsToContract maps the per-activity git head-state map (honest-empty: nil in ⇒
// nil out). It composes each row's READ-TIME prUrl/prNumber projections from the
// PER-PROJECT repo base + the opaque pullRequestRef — the durable aggregate stays
// provider-opaque; prUrl/prNumber are pure read-time projections, never stored.
//
// Since the venue switch (0df2ce0) gh-mode construction PRs open in the PROJECT's own
// repo, not the central construction repo, so the base is resolved per-project via
// projectRepoBase(projectID) (which falls back to the central m.repoBase exactly when
// the dispatch resolver is nil or misses — links stay central-pointing precisely when
// dispatch does).
func (m *deliveryManager) gitRowsToContract(projectID ProjectID, rows map[string]projectstate.ActivityGitStatus) map[string]ActivityGitStatus {
	if len(rows) == 0 {
		return nil
	}
	base := m.deliveryProjectRepoBase(projectID)
	out := make(map[string]ActivityGitStatus, len(rows))
	for id, g := range rows {
		prNumber, prURL := projectPRRef(g.PullRequestRef, base)
		out[id] = ActivityGitStatus{
			ActivityID:     g.ActivityID,
			BranchName:     g.BranchName,
			BranchRef:      g.BranchRef,
			PullRequestRef: g.PullRequestRef,
			PrNumber:       int64(prNumber),
			PrURL:          prURL,
			CICheck:        CICheckState(int(g.CICheck)),
			ArchApproved:   g.ArchApproved,
			Merged:         g.Merged,
			CRLabel:        g.CRLabel,
			IsRevert:       g.IsRevert,
			UpdatedAt:      g.UpdatedAt,
		}
	}
	return out
}

// projectPRRef is the SINGLE server-side site that turns the OPAQUE pullRequestRef into
// the SPA's two read-time render fields (D-PA-GIT-PRURL-ruling R1/R2). It isolates BOTH
// the "the opaque ref is a decimal PR number" assumption AND the GitHub "/pull/<n>" URL
// grammar to one place — the durable aggregate stays provider-opaque.
//
//   - prNumber: strconv.Atoi(ref). Zero (→ omitted by the web wire's omitempty) when ref
//     is "" (branch-only first touch) or unparseable — never panics, never fabricates.
//   - prURL: <repoBase>/pull/<ref>, ONLY when ref != "" AND repoBase != "". Otherwise "".
func projectPRRef(ref, repoBase string) (prNumber int, prURL string) {
	if ref == "" {
		return 0, ""
	}
	if n, err := strconv.Atoi(ref); err == nil {
		prNumber = n
	}
	if repoBase != "" {
		prURL = repoBase + "/pull/" + ref
	}
	return prNumber, prURL
}

// projectRepoBase resolves the WEB base each git row's prUrl is composed against FOR THIS
// PROJECT. Since the venue switch (0df2ce0) gh-mode construction PRs open in the project's
// OWN repo, so a per-project base must be projected rather than reusing the central
// construction repo's base (m.repoBase) — those URLs would otherwise point at the wrong
// repo and lie. The host stays the same as the configured central base (github.com or the
// GHES web root); only owner/repo swap to the project's own.
//
// Fallback (mirrors the dispatch fallback so links stay central-pointing EXACTLY when
// dispatch stays central): the central m.repoBase is returned verbatim when the resolver
// is nil, misses the project, yields a malformed ref, or the central base has no host to
// borrow (unconfigured ⇒ "" ⇒ prUrl omitted downstream).
func (m *deliveryManager) deliveryProjectRepoBase(projectID ProjectID) string {
	if m.repo == nil {
		return m.repoBase
	}
	repoRef, ok := m.repo(projectID)
	if !ok {
		return m.repoBase
	}
	// A GITLOCAL PROJECT HAS NO WEB HOST (review fix round 1, finding 2). The deterministic local
	// venue is a FILESYSTEM path, not a forge, and it is invisible here by accident rather than by
	// construction: GitLocalRepoRefForProject mints a WELL-FORMED owner|owner/repo ref, so
	// RepoRefOwnerRepo decodes it happily and the composed base came out as
	// "<configured host>/local/<projectId>" — a plausible-looking URL that 404s, stamped onto every
	// prUrl of every activity of every local boot. R6's single recognition point is what settles it,
	// and it is the same question railLifecycleEnabled asks two lines from the same resolver.
	if isGitLocalVenue(projectID, repoRef) {
		return ""
	}
	owner, name, err := sourcecontrol.RepoRefOwnerRepo(repoRef)
	if err != nil {
		return m.repoBase
	}
	host := repoWebHost(m.repoBase)
	if host == "" {
		return m.repoBase
	}
	return host + "/" + owner + "/" + name
}

// repoWebHost recovers the <host> prefix from a <host>/<owner>/<repo> web base by
// stripping the final two path segments. The host retains its scheme (https://…); a
// GHES subpath host (e.g. https://ghe.example.com/prefix) is preserved because only the
// trailing owner/repo pair is removed. "" in (unconfigured central base) ⇒ "" out.
func repoWebHost(repoBase string) string {
	s := strings.TrimRight(repoBase, "/")
	i := strings.LastIndex(s, "/")
	if i < 0 {
		return ""
	}
	s = s[:i] // drop /<repo>
	i = strings.LastIndex(s, "/")
	if i < 0 {
		return ""
	}
	return s[:i] // drop /<owner>
}

// worstOriginFor is the wire worstOrigin: the ledger's roll-up on a recorded row, and
// omitted (nil) on a planned-no-record one, which has no ledger to roll up and whose
// empty-ledger seed ("observed") would read as a claim about a row nothing backs.
func worstOriginFor(recorded bool, attempts []projectstate.TaskAttempt) *string {
	if !recorded {
		return nil
	}
	o := string(projectstate.AttemptsWorstOrigin(attempts))
	return &o
}

// constructionRowsToContract maps the per-activity construction head-state map
// (honest-empty: nil in ⇒ nil out). activityMeta carries the Phase-2 activity-list
// metadata (worker class + coding + componentId) keyed by activity id, used to
// classify each activity's ActivityType (see projectstate.ClassifyType) — the N-* id
// namespace alone is too coarse — and to resolve its layer-stack projection.
// componentLayer is the id -> Method-layer-name lookup built once per call from the
// committed .systemDesign components (see componentLayerByID); empty when no system
// design is committed.
//
// The committed activity list is authoritative for WHAT EXISTS (construction UI spec,
// "slot 9 is authoritative for what exists"): every listed activity is emitted, and a
// listed activity with no stored head-state yet is emitted as a PLANNED-NO-RECORD row —
// "not started / no record", never Done. It goes through exactly the same resolution
// as a stored row, from the zero-value head-state: the server classifies it (so its
// profile's phases and tasks can be drawn), and because it has neither stored phases
// nor a ledger it resolves to no completions, so HasBuildEvidence is false and Phases
// and Attempts are empty.
//
// That row is NOT indistinguishable from a stored one, and the wire must not pretend it
// is. BuildStatus and Phase are required, non-omitempty enums, so it still carries
// their zero values (BuildInConstruction / phase 0) — values the contract marks
// meaningless while HasBuildEvidence is false, and which an MCP reader with no such
// gate would otherwise report as six activities "in construction". So the row says
// what it is instead: Recorded is false, and WorstOrigin (whose empty-ledger seed is
// "observed") is omitted. Without this, an activity nobody has touched yet was absent
// from the view altogether rather than shown as not started.
// A stored row the list no longer names is still emitted (it classifies as whatever
// its metadata allows, which for an unlisted id is nothing).
func constructionRowsToContract(
	rows map[string]projectstate.ActivityExecution,
	activityMeta map[string]projectstate.ActivityItem,
	componentLayer map[string]string,
	plan constructionPlan,
) map[string]ActivityConstructionStatus {
	if len(rows) == 0 && len(activityMeta) == 0 {
		return nil
	}
	all := make(map[string]projectstate.ActivityExecution, len(rows)+len(activityMeta))
	for id := range activityMeta {
		all[id] = projectstate.ActivityExecution{ActivityID: id}
	}
	maps.Copy(all, rows)
	out := make(map[string]ActivityConstructionStatus, len(all))
	for id, r := range all {
		// Recorded is the one explicit wire signal that a stored head-state row backs
		// this row. Everything else a planned-no-record row carries is either
		// honestly empty (no phases, no attempts) or a zero value the contract marks
		// meaningless; Recorded lets a reader tell the two populations apart without
		// re-deriving it from those zeros.
		_, recorded := rows[id]
		meta := activityMeta[id]
		typ, typVariant, resolved, classified := classifiedRowView(r, activityMeta[id])
		// Type/Kind/Variant/Phases/BuildStatus/Phase form a DISCRIMINATED UNION with
		// Classified: an activity the classifier refused to type asserts nothing about
		// what it is, how far its lifecycle has run, or what its coarse status is.
		// ClassifyType's failure return is ActivityTypeService — which is also the
		// generated enum's zero — so passing typ through unconditionally rendered the
		// unclassifiable rows (about 60 under the legacy activity list D9 deleted) as
		// Service builds carrying a Service
		// lifecycle skeleton. That is a different lie, not the honest blank the design
		// requires ("the caller MUST render the row as Unclassified with NO lifecycle
		// sub-rows at all" — and, per the same reasoning, no coarse status chip
		// either: a row with no sub-rows to justify it must not assert "Integrated").
		// The generated enums are closed unions with no Unclassified member, so honesty
		// is expressed by OMISSION rather than by a sentinel: Classified is the tag,
		// and Type/Kind/Variant/Phase/BuildStatus are left at their zero values and
		// MUST NOT be read while it is false; Phases — the half of the claim the view
		// actually renders sub-rows for — is empty rather than a seeded skeleton.
		var (
			wireType    ActivityType
			variant     TestingVariant
			phases      []PhaseCompletion
			coarsePhase ActivityConstructionPhase
			buildStatus ActivityBuildStatus
		)
		if classified {
			wireType = ActivityType(int(typ))
			if typ == projectstate.ActivityTypeTesting {
				variant = TestingVariant(int(typVariant))
			}
			// The row's coarse BuildStatus/Phase must be derived from the SAME phase
			// completions phasesToContract actually emits below — never from the raw
			// stored r.Phases directly. projectstate.ResolveConstructionRow (through
			// classifiedRowView) applies the identical profile-wins, ledger-preferred-over-stored
			// resolution once; both the emitted Phases and the coarse derivation read
			// off its result, so neither a partial attempt ledger nor a stored slice
			// that contradicts the profile can make the coarse chip disagree with the
			// very phase ticks rendered beneath it.
			phases = phasesToContract(resolved)
			coarsePhase = ActivityConstructionPhase(int(projectstate.CoarsePhaseFor(r, resolved)))
			buildStatus = ActivityBuildStatus(int(projectstate.CoarseBuildStatusFor(r, resolved)))
		}
		layer, band := projectstate.LayerForActivity(componentLayer[meta.ComponentID])
		out[id] = ActivityConstructionStatus{
			ActivityID:    r.ActivityID,
			Type:          wireType,
			Kind:          wireType,
			Variant:       variant,
			Phase:         coarsePhase,
			Phases:        phases,
			CurrentPhase:  ActivityMethodPhase(string(projectstate.CurrentLifecyclePhase(resolved))),
			StartedAt:     r.StartedAt,
			CompletedAt:   r.CompletedAt,
			BuildStatus:   buildStatus,
			Produced:      producedToContract(r.Produced),
			FailureReason: FailureReason(int(r.FailureReason)),
			FailureDetail: r.FailureDetail,
			Attempts:      attemptsToContract(r.Attempts),
			Classified:    classified,
			// The SECOND half of the discriminated union, and deliberately not the
			// same bit as Classified: a row can be perfectly well classified and
			// still have NO record that any work happened on it. Every
			// planned-no-record row is exactly that (six of the committed 29 today) —
			// neither stored phases nor an attempt ledger — so
			// projectstate.ResolvePhaseCompletions returns nil for them and
			// CoarseBuildStatusFor falls through to its zero value,
			// BuildInConstruction. That is a named, non-omitempty member the SPA
			// rendered as a confident "In construction" chip over work that had not
			// begun, and it also short-circuited the SPA's network-derived
			// eligible/blocked readiness for those rows.
			//
			// Derived from the SAME `resolved` slice the coarse status and the
			// emitted Phases read off — never recomputed from r.Phases/r.Attempts —
			// so the flag cannot drift from the claim it gates. len(resolved) > 0
			// means the profile materialized, which happens exactly when the row had
			// stored phases or a ledger. An unclassified row resolves to nil too, so
			// it reports no evidence as well; the consumer distinguishes the two
			// cases by Classified.
			HasBuildEvidence: len(resolved) > 0,
			Recorded:         recorded,
			// OMITTED on an unrecorded row, and MEANINGFUL ONLY ALONGSIDE A NON-EMPTY
			// Attempts ledger on a recorded one (the contract's own field descriptions
			// say both, and they reach the MCP output schema). Over an empty ledger the
			// value is the aggregate seed "observed", which read on its own says
			// "recorded" about a row where nothing was recorded; a planned-no-record
			// row has no ledger by construction, so it carries no origin at all.
			WorstOrigin:   worstOriginFor(recorded, r.Attempts),
			Layer:         layer,
			LayerBand:     band,
			PendingResume: pendingResumeFor(id, r, meta, resolved, rows, activityMeta, plan),
			OperatorNotes: operatorNotesToContract(r.OperatorNotes),
		}
	}
	return out
}

// operatorNotesToContract maps a row's stored operator notes onto the wire as they are:
// recorded order, and the delivery stamp only where the store holds one. Nothing is
// derived.
func operatorNotesToContract(notes []projectstate.OperatorNote) []OperatorNote {
	if len(notes) == 0 {
		return nil
	}
	out := make([]OperatorNote, 0, len(notes))
	for _, n := range notes {
		v := OperatorNote{
			NoteID:      n.NoteID,
			Kind:        OperatorNoteKind(int(n.Kind)),
			Text:        n.Text,
			RecordedAt:  n.RecordedAt,
			DeliveredAt: n.DeliveredAt,
		}
		if n.Gate != "" {
			g := n.Gate
			v.Gate = &g
		}
		if n.DeliveredToAttemptID != "" {
			a := n.DeliveredToAttemptID
			v.DeliveredToAttemptID = &a
		}
		for _, c := range n.Comments {
			v.Comments = append(v.Comments, NoteComment{JSONPath: c.JSONPath, Text: c.Text})
		}
		out = append(out, v)
	}
	return out
}

// constructionPlan is the committed network's dependency edges and milestones, indexed
// once per read, for pendingResumeFor. The zero value (no network) has no edges.
type constructionPlan struct {
	depsByActivity map[string][]string
	milestones     map[string]projectstate.NetworkMilestone
}

// constructionPlanFor indexes the network the project holds, read the way
// activityMetaByID reads the activity list: whatever model the slot carries.
func constructionPlanFor(p projectstate.Project) constructionPlan {
	network, ok := p.Network.Model.(*projectstate.Network)
	if !ok || network == nil {
		return constructionPlan{}
	}
	deps := make(map[string][]string, len(network.Dependencies))
	for _, d := range network.Dependencies {
		deps[d.Activity] = d.DependsOn
	}
	return constructionPlan{depsByActivity: deps, milestones: projectstate.MilestonesByID(network)}
}

// The wire reasons a PendingDependency carries (the contract's PendingDependency.reason).
const (
	pendingReasonNotBuilt            = "notBuilt"
	pendingReasonBuiltNotIntegrated  = "builtNotIntegrated"
	pendingReasonMilestoneNotReached = "milestoneNotReached"
	pendingReasonUnresolved          = "unresolved"
)

// isPendingResume reports whether a row is integration-pending (architect (D), D.3): no
// pump wrote it (projectstate.PumpWroteRow), yet its effective state is Running — which,
// for a row no pump wrote, means its attempt ledger holds some phases complete and not
// others. Nothing runs it and nothing reviews it, so it is not in flight.
func isPendingResume(r projectstate.ActivityExecution, meta projectstate.ActivityItem) bool {
	if projectstate.PumpWroteRow(r) {
		return false
	}
	effective, _ := projectstate.EffectiveConstructionPhase(r, meta)
	return effective == projectstate.ActivityConstructionRunning
}

// pendingResumeFor is the wire pendingResume for one row, or nil when the row is not
// integration-pending. fromPhase is the first profile phase its resolved phase set does
// not hold complete (resolved is the row's profile-ordered ResolveConstructionRow set);
// waitsOn is every direct network dependency the pump's own rule
// (projectstate.ResolveDependencySatisfied) does not find satisfied, in authored order,
// and is empty, never nil, when the row is next in line.
func pendingResumeFor(
	id string,
	r projectstate.ActivityExecution,
	meta projectstate.ActivityItem,
	resolved []projectstate.PhaseCompletion,
	rows map[string]projectstate.ActivityExecution,
	activityMeta map[string]projectstate.ActivityItem,
	plan constructionPlan,
) *PendingResume {
	if !isPendingResume(r, meta) {
		return nil
	}
	from := projectstate.CurrentLifecyclePhase(resolved)
	if from == "" {
		return nil
	}
	waitsOn := []PendingDependency{}
	for _, dep := range plan.depsByActivity[id] {
		res := projectstate.ResolveDependencySatisfied(dep, activityMeta, rows, plan.milestones, map[string]bool{})
		if res.Satisfied && res.ProblemReason == "" {
			continue
		}
		waitsOn = append(waitsOn, PendingDependency{Id: dep, Reason: pendingReasonFor(dep, res, rows, activityMeta, plan)})
	}
	return &PendingResume{FromPhase: ActivityMethodPhase(string(from)), WaitsOn: waitsOn}
}

// pendingReasonFor names why one unsatisfied dependency is unsatisfied, in the order
// projectstate.ResolveDependencySatisfied itself tries: a plan defect, a milestone, then
// an activity — which is "built but not integrated" when it is itself integration-pending
// (the backfill's own wording), and "not built" otherwise.
func pendingReasonFor(
	dep string,
	res projectstate.DependencyResolution,
	rows map[string]projectstate.ActivityExecution,
	activityMeta map[string]projectstate.ActivityItem,
	plan constructionPlan,
) string {
	if res.ProblemReason != "" {
		return pendingReasonUnresolved
	}
	if _, isMilestone := plan.milestones[dep]; isMilestone {
		return pendingReasonMilestoneNotReached
	}
	if r, exists := rows[dep]; exists && isPendingResume(r, activityMeta[dep]) {
		return pendingReasonBuiltNotIntegrated
	}
	return pendingReasonNotBuilt
}

// constructionStartedFor answers the Begin-versus-Resume question — has construction
// started for this project? — from the STORED head-state, once, on the server.
//
// True iff some stored row carries state only the construction pump writes (a start
// time, a coarse phase past NotStarted, a phase set, a recorded failure) or an attempt
// the running system OBSERVED. A reconstructed attempt never counts, whatever its
// outcome: the backfill wrote 214 backfilled attempts onto 23 activities no pump ever
// ran, and counting them read "Resume construction" on a project whose pump had never
// started. A planned-no-record row cannot count either — it is not a stored row.
//
// It replaces the SPA probing one construction-session endpoint per committed activity
// on every load (29 GETs), which also stopped answering once Temporal retention expired.
func constructionStartedFor(rows map[string]projectstate.ActivityExecution) bool {
	for _, r := range rows {
		if rowCarriesPumpState(r) || hasObservedAttempt(r.Attempts) {
			return true
		}
	}
	return false
}

// rowCarriesPumpState reports whether a stored row holds any head fact only the pump
// writes: the start stamp, the exit stamp, or a recorded failure. The coarse roll-up and
// the phase set it also used to name are DERIVED now (spec §5.3), and a derivation is not
// evidence that anything ran.
func rowCarriesPumpState(r projectstate.ActivityExecution) bool {
	return r.StartedAt != nil ||
		r.CompletedAt != nil ||
		r.FailureReason != projectstate.FailureReasonUnknown ||
		r.FailureDetail != ""
}

// hasObservedAttempt reports whether the ledger holds an attempt of origin observed.
func hasObservedAttempt(attempts []projectstate.TaskAttempt) bool {
	for _, a := range attempts {
		if a.Provenance.Origin == projectstate.OriginObserved {
			return true
		}
	}
	return false
}

// activityMetaByID builds the id → ActivityItem lookup from the committed
// Phase-2 activity list (empty map when no list is committed).
func activityMetaByID(p projectstate.Project) map[string]projectstate.ActivityItem {
	out := map[string]projectstate.ActivityItem{}
	if al, ok := p.ActivityList.Model.(*projectstate.ActivityList); ok && al != nil {
		for _, a := range al.Activities {
			out[a.Name] = a
		}
	}
	return out
}

// componentLayerByID builds the id → Method-layer-name lookup from the committed
// .systemDesign components (empty map when no system design is committed, exactly as
// activityMetaByID tolerates a missing activity list). It feeds LayerForActivity in
// constructionRowsToContract, keyed by each row's componentId — never by its activity id.
func componentLayerByID(p projectstate.Project) map[string]string {
	out := map[string]string{}
	if sys, ok := p.SystemDesign.Model.(*projectstate.System); ok && sys != nil {
		for _, c := range sys.Components {
			out[c.ID] = c.Layer.String()
		}
	}
	return out
}

// classifiedRowView resolves everything constructionRowsToContract and computeEVAtRead
// must agree about for ONE stored construction row: its classified type/variant and the
// reconciled phase set both of them derive from. Sharing it is the point — the EV curve
// used to read r.Phases raw and with no classified check, so an unclassified row could
// contribute to the curve while the row beside it refused to assert a status at all.
//
// The resolution itself lives in projectstate (ResolveConstructionRow), so the
// construction pump reads a row exactly as this view renders it. This is a pure call.
func classifiedRowView(
	r projectstate.ActivityExecution,
	meta projectstate.ActivityItem,
) (typ projectstate.ActivityType, variant projectstate.TestingVariant, resolved []projectstate.PhaseCompletion, classified bool) {
	return projectstate.ResolveConstructionRow(r, meta)
}

// phasesToContract maps the App-A internal phase-completion records onto the wire.
// The caller (constructionRowsToContract) passes the ALREADY-RESOLVED phase set from
// projectstate.ResolveConstructionRow (through classifiedRowView) — this function
// performs no further derivation, so it
// cannot drift from the coarse BuildStatus/Phase computed alongside it.
func phasesToContract(phases []projectstate.PhaseCompletion) []PhaseCompletion {
	if len(phases) == 0 {
		return nil
	}
	out := make([]PhaseCompletion, 0, len(phases))
	for _, ph := range phases {
		out = append(out, PhaseCompletion{
			Phase:       ActivityMethodPhase(string(ph.Phase)),
			Weight:      int64(ph.Weight),
			Completed:   ph.Completed,
			CompletedAt: ph.CompletedAt,
			ArtifactRef: ph.ArtifactRef,
			Label:       ph.Label,
		})
	}
	return out
}

// attemptsToContract maps the append-only Figure A-1 task ledger onto the wire.
// Provenance is copied VERBATIM and never defaulted — the zero origin means
// "synthesized" and must survive the boundary as such; a mapper that filled in a
// missing origin would launder a fabricated row into an observed one.
func attemptsToContract(attempts []projectstate.TaskAttempt) []TaskAttempt {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]TaskAttempt, 0, len(attempts))
	for _, a := range attempts {
		out = append(out, TaskAttempt{
			AttemptId: a.AttemptID,
			Task:      string(a.Task),
			Phase:     ActivityMethodPhase(string(a.Phase)),
			Attempt:   int64(a.Attempt),
			Actor:     strPtrOrNil(string(a.Actor)),
			StartedAt: a.StartedAt,
			EndedAt:   a.EndedAt,
			Outcome:   string(a.Outcome),
			Evidence:  EvidenceRef{Kind: string(a.Evidence.Kind), Ref: a.Evidence.Ref},
			Provenance: AttemptProvenance{
				Origin:      string(a.Provenance.Origin),
				Generator:   strPtrOrNil(a.Provenance.Generator),
				GeneratedAt: a.Provenance.GeneratedAt,
				Basis:       strPtrOrNil(a.Provenance.Basis),
			},
		})
	}
	return out
}

// producedToContract maps the produced-artifact cards.
func producedToContract(produced []projectstate.ProducedArtifact) []ProducedArtifact {
	if len(produced) == 0 {
		return nil
	}
	out := make([]ProducedArtifact, 0, len(produced))
	for _, p := range produced {
		out = append(out, ProducedArtifact{Kind: p.Kind, Title: p.Title, Source: p.Source, Produced: p.Produced, Note: p.Note})
	}
	return out
}

// constructionProgressToContract maps the project-level Phase-3 framing scalars
// (nil in ⇒ nil out) AND computes the EV/SPI earned-value curve server-side via the
// estimationEngine (compute-at-read).
func (m *deliveryManager) constructionProgressToContract(p projectstate.Project) *ConstructionProgress {
	cp := p.ConstructionProgress
	if cp == nil {
		return nil
	}
	return &ConstructionProgress{
		Week:           int64(cp.Week),
		TotalWeeks:     int64(cp.TotalWeeks),
		HandOffModel:   cp.HandOffModel,
		SupervisionCap: int64(cp.SupervisionCap),
		EV:             m.computeEVAtRead(p, int64(cp.TotalWeeks)),
		Points:         evPointsToContract(cp.Points),
	}
}

// evPointsToContract surfaces the recorded weekly earned-value observation series
// (the ground-truth points captured by the-method-project-tracking, stored on
// .constructionProgress.points) onto the read view. Distinct from computeEVAtRead's
// estimator-derived curve: these are what the team ACTUALLY earned each week.
func evPointsToContract(pts []projectstate.EvPoint) []EvPoint {
	if len(pts) == 0 {
		return nil
	}
	out := make([]EvPoint, 0, len(pts))
	for _, p := range pts {
		out = append(out, EvPoint{
			Week:       int64(p.Week),
			EarnedPct:  p.EarnedPct,
			PlannedPct: p.PlannedPct,
			Note:       p.Note,
			AcPct:      p.AcPct,
		})
	}
	return out
}

// computeEVAtRead computes the EV/SPI earned-value curve via the
// estimationEngine.ComputeEarnedValue over the AUTHORED activity list ×
// network, the integrated activity set, the calendar days/week, and the total-week
// framing. Zero EVCurve when the estimator is nil or inputs are degenerate.
func (m *deliveryManager) computeEVAtRead(p projectstate.Project, totalWeeks int64) EVCurve {
	if m.estimator == nil {
		return EVCurve{}
	}
	var activities projectstate.ActivityList
	if al, ok := p.ActivityList.Model.(*projectstate.ActivityList); ok && al != nil {
		activities = *al
	}
	var network projectstate.Network
	if net, ok := p.Network.Model.(*projectstate.Network); ok && net != nil {
		network = *net
	}

	// The integrated set is read through the SAME derivation the rest of the read path
	// uses (classifiedRowView + CoarseBuildStatusFor), not off the raw stored
	// BuildStatus and not off the raw stored r.Phases. Both paths used to read stored
	// and therefore agreed; once constructionRowsToContract began deriving, a raw read
	// here let the EV/SPI curve contradict the build status rendered beside it on the
	// same screen — and, with no `classified` check, let a row the classifier refused
	// to type contribute to the curve while the row beside it refused to assert a
	// status at all.
	activityMeta := activityMetaByID(p)
	integrated := make([]string, 0, len(p.ActivityExecution))
	for id, r := range p.ActivityExecution {
		_, _, resolved, classified := classifiedRowView(r, activityMeta[id])
		if !classified {
			continue
		}
		if projectstate.CoarseBuildStatusFor(r, resolved) == projectstate.BuildIntegrated {
			integrated = append(integrated, id)
		}
	}

	curve, err := m.estimator.ComputeEarnedValue(
		fweng.Context{Context: context.Background()},
		toEstimationActivityList(activities),
		toEstimationNetwork(network),
		integrated,
		totalWeeks,
		int64(calendarDaysPerWeek(p)),
	)
	if err != nil {
		return EVCurve{}
	}
	return EVCurve{Weeks: curve.Weeks, Earned: curve.Earned, Planned: curve.Planned, SPI: curve.SPI}
}

// calendarDaysPerWeek reads the working days/week from the PlanningAssumptions slot,
// defaulting to the standard 5-day workweek when the slot is absent or non-positive.
func calendarDaysPerWeek(p projectstate.Project) int {
	if pa, ok := p.PlanningAssumptions.Model.(*projectstate.PlanningAssumptions); ok && pa != nil && pa.CalendarDaysPerWeek > 0 {
		return int(pa.CalendarDaysPerWeek)
	}
	return 5
}

// serviceContractsToContract maps the typed service-contract corpus (honest-empty:
// nil in ⇒ nil out) onto the web-transport ServiceContract DTO. The contract
// DOCUMENT (its `interface` operations resolved against the document's `$defs`) is
// the source of truth: each op's parameters become input ContractStructs, its result
// becomes an output ContractStruct, and — when the op can fail — the layer's typed
// error becomes a final output box. Every struct's fields are resolved from the
// referenced `$def`'s properties (order-preserved). This is what feeds the SPA's
// «interface» diagram boxes; nothing is fabricated and nothing is served empty.
func serviceContractsToContract(scs map[string]projectstate.ServiceContract) map[string]ServiceContract {
	if len(scs) == 0 {
		return nil
	}
	out := make(map[string]ServiceContract, len(scs))
	for name, sc := range scs {
		layerErr := layerErrorName(sc.Layer)
		anyError := false
		for _, op := range sc.Interface.Operations {
			if op.Error {
				anyError = true
				break
			}
		}
		errorModel := ""
		if anyError {
			errorModel = "Operations fail with " + layerErr + " — the typed " + sc.Layer + " fault."
		}
		out[name] = ServiceContract{
			Component:     sc.Component,
			Layer:         sc.Layer,
			Stereotype:    sc.Title,
			Ops:           opsFromInterface(sc.Interface, sc.Defs, layerErr),
			DataContracts: dataContractNames(sc.Defs),
			ErrorModel:    errorModel,
		}
	}
	return out
}

// opsFromInterface derives the transport op list from the contract document's
// interface, resolving each op's params/result/error against the document's `$defs`
// into the input/output ContractStructs + a `name(params) → (result, error)`
// signature the SPA diagram renders. Returns nil for an empty interface.
func opsFromInterface(iface projectstate.ContractInterface, defs map[string]json.RawMessage, layerErr string) []ContractOp {
	if len(iface.Operations) == 0 {
		return nil
	}
	out := make([]ContractOp, 0, len(iface.Operations))
	for _, op := range iface.Operations {
		inputs := make([]ContractStruct, 0, len(op.Params))
		for _, p := range op.Params {
			inputs = append(inputs, structFromSchema(p.Name, p.Schema, defs))
		}
		var outputs []ContractStruct
		if len(op.Result) > 0 {
			outputs = append(outputs, structFromSchema("result", op.Result, defs))
		}
		if op.Error {
			outputs = append(outputs, ContractStruct{
				Name:   layerErr,
				Fields: []GoField{{Name: "fault", Type: layerErr}},
			})
		}
		out = append(out, ContractOp{
			Signature: opSignature(op, layerErr),
			Inputs:    inputs,
			Outputs:   outputs,
		})
	}
	return out
}

// opSignature renders one operation as `name(p: T, …) → (Result, error)`, using the
// same `→` separator the SPA signature parser recognises. Pointer params are starred.
func opSignature(op projectstate.ContractOperation, layerErr string) string {
	params := make([]string, 0, len(op.Params))
	for _, p := range op.Params {
		t := schemaTypeName(p.Schema, defaultTypeName)
		if p.Pointer {
			t = "*" + t
		}
		params = append(params, p.Name+": "+t)
	}
	sig := op.Name + "(" + strings.Join(params, ", ") + ")"
	var rets []string
	if len(op.Result) > 0 {
		rets = append(rets, schemaTypeName(op.Result, defaultTypeName))
	}
	if op.Error {
		rets = append(rets, layerErr)
	}
	switch len(rets) {
	case 0:
		// no declared return
	case 1:
		sig += " → " + rets[0]
	default:
		sig += " → (" + strings.Join(rets, ", ") + ")"
	}
	return sig
}

// structFromSchema resolves one JSON Schema node into a ContractStruct: the box is
// titled with the node's resolved Go-ish type name, and its fields are the referenced
// `$def`'s (or inline object's) properties. A scalar / array / external type has no
// sub-fields, so it carries a single self-field named selfName so no box is empty.
func structFromSchema(selfName string, raw json.RawMessage, defs map[string]json.RawMessage) ContractStruct {
	typeName := schemaTypeName(raw, selfName)
	fields := objectFields(raw, defs)
	if len(fields) == 0 {
		fields = []GoField{{Name: selfName, Type: typeName}}
	}
	return ContractStruct{Name: typeName, Fields: fields}
}

const defaultTypeName = "value"

// layerErrorName maps a Method layer to its framework error type, the typed fault
// every op on that layer returns on failure.
func layerErrorName(layer string) string {
	switch strings.ToLower(layer) {
	case "resourceaccess":
		return "fwra.Error"
	case "engine":
		return "fweng.Error"
	case "manager":
		return "fwm.Error"
	default:
		return "error"
	}
}

// dataContractNames returns the document's `$defs` names (the data contracts),
// sorted for a deterministic wire order. nil when there are none.
func dataContractNames(defs map[string]json.RawMessage) []string {
	if len(defs) == 0 {
		return nil
	}
	names := make([]string, 0, len(defs))
	for k := range defs {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// schemaTypeName resolves a JSON Schema node to a Go-ish type name: an array → []T,
// an explicit x-go-type → that, a `$ref` → its base name, otherwise the mapped
// primitive. fallback is returned when the node is empty / unrecognised.
func schemaTypeName(raw json.RawMessage, fallback string) string {
	if len(raw) == 0 {
		return fallback
	}
	var n struct {
		Ref     string          `json:"$ref"`
		Type    json.RawMessage `json:"type"`
		Items   json.RawMessage `json:"items"`
		XGoType string          `json:"x-go-type"`
	}
	if err := json.Unmarshal(raw, &n); err != nil {
		return fallback
	}
	if len(n.Items) > 0 {
		return "[]" + schemaTypeName(n.Items, fallback)
	}
	if n.XGoType != "" {
		return n.XGoType
	}
	if n.Ref != "" {
		return refBase(n.Ref)
	}
	return primitiveTypeName(n.Type, fallback)
}

// primitiveTypeName maps a JSON Schema `type` (a string OR a ["null", T] union) to a
// Go-ish primitive name.
func primitiveTypeName(rawType json.RawMessage, fallback string) string {
	if len(rawType) == 0 {
		return fallback
	}
	kind := ""
	var single string
	if err := json.Unmarshal(rawType, &single); err == nil {
		kind = single
	} else {
		var union []string
		if err := json.Unmarshal(rawType, &union); err == nil {
			for _, k := range union {
				if k != "null" {
					kind = k
					break
				}
			}
		}
	}
	switch kind {
	case "string":
		return "string"
	case "integer":
		return "int"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "object":
		return "object"
	case "array":
		return "[]any"
	case "":
		return fallback
	default:
		return kind
	}
}

// refBase returns the trailing name of a JSON Schema `$ref` (e.g. "#/$defs/Foo" → "Foo").
func refBase(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

// objectFields resolves a schema node's properties into ordered GoFields. It follows
// a single `$ref` into defs, then reads the resolved object's `properties` in
// declaration order (json.Decoder token stream preserves key order). Non-object
// nodes (scalars, arrays, enums) have no properties → nil.
func objectFields(raw json.RawMessage, defs map[string]json.RawMessage) []GoField {
	if len(raw) == 0 {
		return nil
	}
	var head struct {
		Ref        string          `json:"$ref"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil
	}
	if head.Ref != "" {
		target, ok := defs[refBase(head.Ref)]
		if !ok {
			return nil
		}
		return objectFields(target, defs)
	}
	if len(head.Properties) == 0 {
		return nil
	}
	return orderedProperties(head.Properties)
}

// orderedProperties decodes a JSON Schema `properties` object into ordered GoFields,
// preserving the on-disk key order. Each field's type is resolved from its schema and
// its name honours an `x-go-name` override when present.
func orderedProperties(props json.RawMessage) []GoField {
	dec := json.NewDecoder(bytes.NewReader(props))
	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil
	}
	var fields []GoField
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fields
		}
		key, _ := keyTok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return fields
		}
		name := key
		var override struct {
			XGoName string `json:"x-go-name"`
		}
		if json.Unmarshal(val, &override) == nil && override.XGoName != "" {
			name = override.XGoName
		}
		fields = append(fields, GoField{Name: name, Type: schemaTypeName(val, key)})
	}
	return fields
}

// slotForKind reads the named slot for kind off the Project aggregate. The kind→slot
// mapping is split by lifecycle phase (system-design vs project-design kinds) purely to
// keep each switch under the gocyclo gate; the union covers every ArtifactKind.
func slotForKind(p projectstate.Project, kind projectstate.ArtifactKind) projectstate.ArtifactSlot {
	if slot, ok := designSlotForKind(p, kind); ok {
		return slot
	}
	return planSlotForKind(p, kind)
}

// designSlotForKind maps the Phase-1 (system-design) kinds to their Project slots.
func designSlotForKind(p projectstate.Project, kind projectstate.ArtifactKind) (projectstate.ArtifactSlot, bool) {
	switch kind {
	case projectstate.KindMission:
		return p.Mission, true
	case projectstate.KindGlossary:
		return p.Glossary, true
	case projectstate.KindScrubbedRequirements:
		return p.ScrubbedRequirements, true
	case projectstate.KindVolatilities:
		return p.Volatilities, true
	case projectstate.KindCoreUseCases:
		return p.CoreUseCases, true
	case projectstate.KindSystem:
		return p.SystemDesign, true
	case projectstate.KindOperationalConcepts:
		return p.OperationalConcepts, true
	case projectstate.KindStandardCheck:
		return p.StandardCheck, true
	case projectstate.KindPlanningAssumptions, projectstate.KindActivityList, projectstate.KindNetwork,
		projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution, projectstate.KindCompressedSolution,
		projectstate.KindDecompressedSolution, projectstate.KindRiskModel, projectstate.KindSdpReview:
		// project-design kinds — resolved by planSlotForKind.
		return projectstate.ArtifactSlot{}, false
	default:
		return projectstate.ArtifactSlot{}, false
	}
}

// planSlotForKind maps the Phase-2 (project-design) kinds to their Project slots.
func planSlotForKind(p projectstate.Project, kind projectstate.ArtifactKind) projectstate.ArtifactSlot {
	switch kind {
	case projectstate.KindPlanningAssumptions:
		return p.PlanningAssumptions
	case projectstate.KindActivityList:
		return p.ActivityList
	case projectstate.KindNetwork:
		return p.Network
	case projectstate.KindNormalSolution:
		return p.NormalSolution
	case projectstate.KindSubcriticalSolution:
		return p.SubcriticalSolution
	case projectstate.KindCompressedSolution:
		return p.CompressedSolution
	case projectstate.KindDecompressedSolution:
		return p.DecompressedSolution
	case projectstate.KindRiskModel:
		return p.RiskModel
	case projectstate.KindSdpReview:
		return p.SdpReview
	case projectstate.KindMission, projectstate.KindGlossary, projectstate.KindScrubbedRequirements,
		projectstate.KindVolatilities, projectstate.KindCoreUseCases, projectstate.KindSystem,
		projectstate.KindOperationalConcepts, projectstate.KindStandardCheck:
		// system-design kinds — designSlotForKind resolved them before this helper runs.
		return projectstate.ArtifactSlot{}
	default:
		return projectstate.ArtifactSlot{}
	}
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// pipelineDefaultToolchain names the placeholder toolchain stamped on the design
// dispatch's logical step graph; the real design recipe lives in the user's
// aiarch-design.yml workflow file, so the step is only present to satisfy the RA's
// non-empty-step-graph pre-condition.
const pipelineDefaultToolchain = "go-1.23"

// ===========================================================================
// Workflow-side pipeline helpers. The temporalgen migration routes the submit/observe
// design-job pair through the GENERATED agenticJobAccess invokers (wf.Acts.
// PipelineSubmit/ObserveAgenticJob); the value mapping that lived on the folded
// pipelineDispatchAdapter — the RepoRef→RepoTarget decode, the PipelineSpec composition,
// and the RA-phase→neutral-phase mapping — is now these PURE workflow-side helpers
// (mirrors the child's own dispatch path). The idempotency key is stamped INSIDE the
// generated submit Activity (genActivityIdempotencyKey, the same run-scoped 3-part scheme
// the old hand-derived key used), so the redraft-vs-auto-retry distinction is unchanged.
// The former EXPORTED consumer-mirror interface + the folded pipelineDispatchAdapter +
// the neutral pipelineSpec/pipelineHandle carriers are RETIRED.
// ===========================================================================

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// designRepoTarget decodes an opaque per-project RepoRef String() into the RA's
// infrastructure-neutral RepoTarget{Owner, Name} for the per-project-design-dispatch.
// An empty repoRef is the dormant-rail case → a zero RepoTarget (the RA falls back to
// the configured construction repo). A malformed ref surfaces as the RA's
// ContractMisuse (the dispatch Activity maps it to a terminal error). It uses
// sourcecontrol's own OwnerRepo accessor so the RepoRef encoding stays owned by
// sourceControlAccess (no encoding leak here).
//
// NOT promotable to projectstate (code-health-phase-bd task D3 verification): it needs
// agenticjob.RepoTarget + sourcecontrol.RepoRefOwnerRepo/RepoRefFromString —
// both sibling ResourceAccess packages, and TestMethodLayering forbids RA→RA sideways
// imports (the RA-layer analog of "no Manager→Manager sideways"). Stays duplicated
// per-manager alongside designBranch's twin.
func designRepoTarget(repoRef string) (agenticjob.RepoTarget, error) {
	if repoRef == "" {
		return agenticjob.RepoTarget{}, nil
	}
	owner, name, err := sourcecontrol.RepoRefOwnerRepo(sourcecontrol.RepoRefFromString(repoRef))
	if err != nil {
		return agenticjob.RepoTarget{}, err
	}
	return agenticjob.RepoTarget{Owner: owner, Name: name}, nil
}

// ===========================================================================
// Dispatch inputs (C-WF-DESIGN workflow_dispatch schema). These exact key names
// are the binding contract with aiarch-design.yml's workflow_dispatch.inputs.
// idempotency_token is RA-controlled and is NOT set here.
// ===========================================================================

const (
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	dispatchInputArtifactKind = "artifact_kind"
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	// dispatchInputCommand carries the .claude command slug the seated design job runs
	// (DesignCommandFor). It REPLACES the retired design_prompt input: the Method doctrine
	// that used to be composed into a prompt now lives in the command's method-assets, so
	// the Manager ships only the command NAME, not prose.
	dispatchInputCommand = "command"
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	dispatchInputTargetBranch = "target_branch"
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	dispatchInputPriorStateRef = "prior_state_ref"
	// dispatchInputJobMode discriminates a DRAFT job (the Action commits the typed
	// Kind model into the slot) from a CRITIQUE job (the Action commits the slot's
	// critiqueVerdict / critiqueNotes read-back carrier — D-MSD-Δ amendment). The
	// Action template branches its commit-target instruction on this value. Defaulted
	// to "draft" in the template so a job dispatched without it (e.g. a UC2 draft)
	// behaves exactly as before.
	dispatchInputJobMode = "job_mode"
)

// Job-mode dispatch values. These exact strings are a contract with the
// aiarch-design.yml template's job_mode input.
const (
	jobModeDraft    = "draft"
	jobModeCritique = "critique"
	// jobModeAnswer is the question-comments answer job: the addressed role (pm/architect)
	// answers open QUESTION ledger entries in place via respondToReviewComment (no
	// putDraftModel, no setCritiqueVerdict). Like critique, it does NOT open a PR.
	jobModeAnswer = "answer"
)

// designBranch and the projectstate resolver it was promoted into are both GONE (stage 4b2):
// the design-branch scheme is retired — there is one activity branch per activity now.

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// appendEpisodeRetryWindow is the HARD wall-clock bound on the episode-append's own retry
// envelope. Attempts are UNCAPPED inside it (bookkeeping must not lose to a transient
// store fault) but they cannot run forever, because the workflow WAITS on this activity.
//
// bounded-latency ruling 2026-08-02: local sidecar append failing >2m is not transient;
// business outcome must not stall on telemetry.
const appendEpisodeRetryWindow = 2 * time.Minute

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// appendEpisodeActivityOptions is the episode-append preset — DELIBERATELY its own
// envelope, independent of every business preset (§capture-seam): a generous per-attempt
// timeout, UNCAPPED attempts inside appendEpisodeRetryWindow (MaxAttempts unset ⇒
// Temporal treats it as unlimited), and ContractMisuse terminal (a malformed record will
// never become well-formed by retrying — the caller logs it instead).
func appendEpisodeActivityOptions() workflow.ActivityOptions {
	o := fwmanager.ActivityPreset{
		Timeout:    30 * time.Second,
		TerminalRA: []fwra.Kind{fwra.ContractMisuse},
	}.Options()
	o.ScheduleToCloseTimeout = appendEpisodeRetryWindow
	return o
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// designWorkflowFileName is the per-project DESIGN workflow file the agentic design
// dispatch must target (per-project-design-dispatch) — the BASENAME of
// sourcecontrol.DesignWorkflowPath (".github/workflows/aiarch-design.yml"), i.e.
// "aiarch-design.yml". Derived from the RA's single source of truth so the dispatch
// target and the project-birth workflow-file seat can never drift. This is the
// workflow file the design dispatch selects in place of the construction default
// (aiarch-construct.yml).
var designWorkflowFileName = path.Base(sourcecontrol.DesignWorkflowPath)

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// mintCredActivityOptions — the credential mint (generated sourceControlAccess.
// getInstallationToken). A rejected/expired App identity is terminal. Feeds the manager's
// option hook (workermanifest.go).
func mintCredActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    15 * time.Second,
		TerminalRA: []fwra.Kind{fwra.Auth, fwra.ContractMisuse},
	}.Options()
}

// scaffoldSyncActivityOptions carries a StartToClose long enough for a FULL scaffold
// converge — ~100 file reads plus up to a whole-tree of contents-API writes on a torn or
// version-bumped repo. F-QA2-36's addendum is the incident it exists for: the shared
// 30-second rail deadline expired mid-loop and the sync only progressed through
// retry-persisted writes. The sync is resumable and idempotent (the manifest is written
// LAST), so a long deadline is safe where a short one is not.
func scaffoldSyncActivityOptions() workflow.ActivityOptions {
	o := railActivityOptions()
	o.StartToCloseTimeout = 5 * time.Minute
	return o
}

// mutateActivityOptions is the preset for the head-state MUTATION ops the child reaches —
// designSessionAccess.commitArtifactWithProvenance (the design slot commit) and
// projectStateAccess.advancePhase (the Phase-1 seal). Retry Transient through the Activity
// RetryPolicy; Conflict is deliberately NOT terminal, because the workflow-level
// re-read→re-apply loop is what resolves it (applyRecovering) rather than a Temporal retry
// re-issuing the same stale expected version forever. Terminal on ContractMisuse.
func mutateActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    15 * time.Second,
		TerminalRA: []fwra.Kind{fwra.ContractMisuse},
	}.Options()
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
func railActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    30 * time.Second,
		TerminalRA: []fwra.Kind{fwra.Auth, fwra.NotFound, fwra.Conflict, fwra.ContractMisuse},
	}.Options()
}

// reviewledger.go holds the durable review-ledger seam for the systemDesign Manager
// (review-ledger feature, founder-ratified 2026-07-05): the projectstate.ReviewComment
// ↔ ReviewCommentView projection the sessionState Query surfaces, the open-comment gate
// the approve precondition reads, and the SetReviewCommentStatus branch mutation. The
// ledger STORAGE + transition rules live in projectstate (reviewthread.go); the branch
// mutation itself is the GENERATED designSessionAccess.setReviewCommentStatusOnBranch /
// seedReviewCommentsOnBranch invoker (B10) — this file is only the Manager-side wiring
// (the wire-view projections, the reject/seed comment shaping, and the workflow-side
// apply/reload helpers).

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// reviewAuthorRole is the role stamped on every comment the architect files at the
// System-Design review gate. The ledger records WHO filed each comment; in the design
// phase the reviewer at the gate is always the architect.
const reviewAuthorRole = "architect"

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// toReviewCommentView projects one stored ledger entry onto its wire view.
func toReviewCommentView(c projectstate.ReviewComment) ReviewCommentView {
	return ReviewCommentView{
		ID:         c.ID,
		Anchor:     c.Anchor,
		AnchorText: c.AnchorText,
		Text:       c.Text,
		AuthorRole: c.AuthorRole,
		Round:      c.Round,
		Status:     c.Status,
		Replies:    toViewReplies(c.Replies),
		Reopened:   c.Reopened,
		Type:       c.Type,
		Addressee:  c.Addressee,
	}
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// toViewReplies projects a stored entry's utterance history onto the wire shape. The
// DEPRECATED scalar ReviewComment.Response is deliberately NOT read here: the shared
// decode point already migrates a legacy response into a synthesized first reply
// (migrateLegacyReviewThread, design §3.5), so reading it again would double-render it.
func toViewReplies(in []projectstate.ReviewCommentReply) []ReviewCommentReply {
	if len(in) == 0 {
		return nil
	}
	out := make([]ReviewCommentReply, 0, len(in))
	for _, r := range in {
		out = append(out, ReviewCommentReply{ID: r.ID, AuthorRole: r.AuthorRole, Text: r.Text, At: r.At})
	}
	return out
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// reviewThreadToView projects the durable ledger onto the wire thread the sessionState
// Query returns (nil stays nil so the omitempty wire shape is unchanged for slots that
// never carried a comment).
func reviewThreadToView(thread []projectstate.ReviewComment) []ReviewCommentView {
	if len(thread) == 0 {
		return nil
	}
	out := make([]ReviewCommentView, 0, len(thread))
	for _, c := range thread {
		out = append(out, toReviewCommentView(c))
	}
	return out
}

// ---------------------------------------------------------------------------
// Shared Temporal identity constants (systemDesignManager.md §6.1/§6.2/§6.5).
// TaskQueue is defined in the generated worker.gen.go.
// ---------------------------------------------------------------------------

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply
// loop (D-PA §6/§7). A stale expectedVersion surfaces as fwra.Conflict
// (non-retryable per the fixed framework enum). The idempotency key is stable per
// Activity invocation, so a re-apply that races a prior committed attempt
// collapses to an idempotent no-op success. The bound guards a write-contention
// pathology. A pure in-workflow guard.
const maxMutateConflictAttempts = 20

// Activity option presets (systemDesignManager.md §6.4). Concrete RetryPolicy / timeout
// choices live here, in the Manager. Each preset is exposed as an ActivityOptions VALUE,
// consumed by the generated-invoker option hook (workermanifest.go activityOptions),
// keyed by the generated activity name — every Activity this Manager executes is
// generated (B10), so no ctx-wrapper form is needed anymore.

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// raConflictErrType is the canonical Temporal Type() a head-state mutation
// Activity surfaces when the optimistic-concurrency token (expectedVersion) is
// stale. The workflow recovers with the bounded re-read→re-apply loop.
var raConflictErrType = fwmanager.RAErrType(fwra.Conflict)

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// raNotFoundErrType is the canonical Temporal Type() the ReadProject Activity
// surfaces when the addressed aggregate has NO row yet — a brand-new project.
var raNotFoundErrType = fwmanager.RAErrType(fwra.NotFound)

// raAuthErrType is the canonical Temporal Type() a rail Activity surfaces for an Auth
// fault. The platform github ClassifyStatus conflates GitHub secondary RATE-LIMIT 403s
// with real permission denials — both become fwra.Auth — and marks the result
// NON-RETRYABLE, so the bounded rail retry (QA F35 + F-QA2-49) has to run WORKFLOW-SIDE.
var raAuthErrType = fwmanager.RAErrType(fwra.Auth)

// isRailAuthFault reports whether err is a rail Auth fault — the rate-limit-403-as-Auth
// that railWithAuthRetry absorbs within a bounded budget.
func isRailAuthFault(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raAuthErrType
	}
	return false
}

// railAuthRetry* bound the workflow-side rail retry (railWithAuthRetry).
//
// F-QA2-49: GitHub SECONDARY rate limits demand a >=60s cool-down before any retry can
// succeed, so the original ~30s budget (5s → 10s → 15s) expired ENTIRELY INSIDE the
// cool-down window after an API-heavy job — observed live as three openPR attempts across
// 15s, all 403, then StageDraftFailed, with a manual retry 15 minutes later succeeding
// first try. Four attempts over ~7 minutes outlast a secondary-rate-limit window and stay
// bounded, so a GENUINE permission denial still reaches the caller's containment.
const (
	railAuthRetryMaxAttempts = 4
	railAuthRetryBaseBackoff = 60 * time.Second
	railAuthRetryMaxBackoff  = 240 * time.Second
)

// railCredRenewSkew is how far AHEAD of a credential's stated expiry liveCred re-mints.
// A token that expires while a rail Activity is in flight 403s exactly as an expired one
// does, and the mint is cheap next to the walk it protects, so the check buys a margin
// rather than racing the clock.
const railCredRenewSkew = 5 * time.Minute

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// isConflict reports whether err is a head-state mutation's stale-version Conflict.
func isConflict(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raConflictErrType
	}
	return false
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// isReadNotFound reports whether err is the ReadProject Activity's "no row yet"
// NotFound (a brand-new project).
func isReadNotFound(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raNotFoundErrType
	}
	return false
}

// ===========================================================================
// THE ROW RE-READ, and the Conflict that is not a race (stage 4b1, ruling R-A).
//
// ONE copy serves the systemDesign + projectDesign + construction rails: all three
// applyRecovering loops call terminalAfterRowReread, so the discriminator exists once
// even though the three loops keep their own receivers, branch parameter and logging.
// ===========================================================================

// changeRowConflictReread fences the row re-read applyRecovering gained in stage 4b1.
// Pre-change executions keep their recorded sequence — project version only — because the
// row read is a NEW durable command inside the loop, and a history that conflicted once
// would otherwise replay into a command it never made.
const changeRowConflictReread = "row-conflict-reread"

// terminalConflictErrType is the Temporal Type() applyRecovering raises when a Conflict
// cannot be a version conflict, because RE-READING CHANGED NOTHING.
//
// Why the re-read is the discriminator and not an error class: fwra.Kind is
// platform-fixed (framework-go/resourceaccess/errors.go), every Conflict reaches a
// workflow as nothing but that Kind's name (fwmanager.RAErrType, see raConflictErrType),
// and matching a store's message text from a workflow would couple the two across a
// release. The three terminality Conflicts this catches — OpenActivity on an exited row,
// and AppendReviewVerdict / DecideReviewRound on a decided round — differ from a genuine
// version conflict in exactly one OBSERVABLE way: nothing moves when you look again. So
// we look again, and when neither the project version nor the row version moved we fail
// with the honest cause instead of burning twenty attempts to report the wrong one.
//
// WHERE THE SENTENCE CAN BE WRONG, and it is the message and not the verdict: on a design
// rail a mutation targeting MAIN can carry an `expected` read from the SESSION BRANCH (QA
// F29), so the number it holds may exceed main's and re-reading main moves nothing —
// forever. That is a MIS-ADDRESSED CAS, not a store refusing a transition, yet it reaches
// here looking identical and is reported as "the store is refusing". The non-retryable
// OUTCOME is right either way (no number of retries fixes a token read off the wrong
// substrate) and it now arrives in one attempt rather than twenty, but an operator reading
// the sentence on a branch-targeted design mutation should suspect the branch before the
// store. Telling the two apart would mean re-reading on the branch the mutation targets,
// which is the F29 question itself and not this fence's.
const terminalConflictErrType = "MutateTerminalConflict"

// terminalConflictMessage is the one sentence that terminal carries. It names what was
// asked (both versions) and what the answer means (a refusal, not a race), because the
// operator reading it cannot re-run the re-read the workflow already did.
const terminalConflictMessage = "head-state conflict is terminal: neither the project version nor the activity row moved on re-read, so the store is refusing this transition rather than racing it"

// isTerminalConflict reports whether err is that terminal. Callers that legitimately race
// to a terminal state — the round sweep withdrawing a round someone else just decided —
// treat it as success rather than as a failure.
func isTerminalConflict(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == terminalConflictErrType
	}
	return false
}

// rowAccessor is the PER-RUN binding the row re-read needs: WHICH activity row this
// execution writes, what version it believes that row is at, and how to re-seed that
// belief once the store has been asked again.
//
// It rides the RUN's workflow.Context and NOT the workflows receiver. The receiver
// (csWorkflows / workflows / pdWorkflows) is built ONCE per worker — WorkerManifest hands
// its method values to RegisterWorkflow — and is therefore shared by every execution on
// that worker: accessor FIELDS there would be a data race between concurrent children and
// would let one activity's row version re-seed another activity's CAS. A context value is
// per-execution, deterministic, and emits no command, so it also costs no fence of its own
// and leaves all three applyRecovering signatures and their ~49 call sites untouched.
type rowAccessor struct {
	// activityID is the row the run writes. Empty means the run holds no row.
	activityID string
	// version reads the run's copy of the row's version (constructState /
	// coAuthorState / pdCoAuthorState activityVersion — the CAS token it passes).
	version func() int64
	// setVersion re-seeds that copy from what the store just reported.
	setVersion func(int64)
}

// rowAccessorKey is the private context key. A struct{} type (not a string) so nothing
// outside this package can collide with it or read the binding out.
//
// The four funcs that READ or WRITE this binding take a workflow.Context and so cannot
// live in the impl file at all (arch.CheckFileLayout's workflow-in-impl-file rule): they
// sit in deliveryactivity.go, the file the wave keeps — withRowAccessor,
// rowAccessorFrom, rereadRowVersion and terminalAfterRowReread.
type rowAccessorKey struct{}

// slotFor returns the named Project slot for a Phase-1 kind.
func slotFor(proj projectstate.Project, kind ArtifactKind) projectstate.ArtifactSlot {
	switch kind {
	case KindMission:
		return proj.Mission
	case KindGlossary:
		return proj.Glossary
	case KindScrubbedRequirements:
		return proj.ScrubbedRequirements
	case KindVolatilities:
		return proj.Volatilities
	case KindCoreUseCases:
		return proj.CoreUseCases
	case KindSystem:
		return proj.SystemDesign
	case KindOperationalConcepts:
		return proj.OperationalConcepts
	case KindStandardCheck:
		return proj.StandardCheck
	case KindPlanningAssumptions, KindActivityList, KindNetwork, KindNormalSolution,
		KindSubcriticalSolution, KindCompressedSolution, KindDecompressedSolution,
		KindRiskModel, KindSdpReview:
		// Phase-2 kinds have no Phase-1 slot here — same zero-value fallback as
		// the default below (this func is only ever called with Phase-1 kinds).
		return projectstate.ArtifactSlot{}
	default:
		return projectstate.ArtifactSlot{}
	}
}

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the systemDesignManager
// impl. It supplies the genWorkerManifest RegisterWorker consumes: the three workflow
// bodies under their registered names, the per-activity option-preset hook, and the
// genActivities dep threading. It also hosts the external RegisterManagerWorker
// entrypoint the composition root calls (cmd/server/main.go). This Manager has ZERO
// custom Temporal Activities (B10: the last ones — the projectEnvelope-codec reads, the
// head-state mutation writes carrying the BranchAware/Ledger/Provenance/Reconciling
// capability type-assertions, the review-ledger branch mutations, and the free-function
// managed-scaffold sync — were deleted when their call sites migrated onto the generated
// designSessionAccess / sourceControlAccess.syncManagedScaffold invokers); every Activity
// is generated and registered by the generated RegisterWorker.
//
// The Engine dependencies are called DIRECTLY in-workflow (deterministic, by value) and
// are NOT Activities; the durable-execution in-workflow primitives (awaitSignal /
// startTimer) are the Manager's own code.

// ListEpisodesForArtifact returns every episode record (design/review/rework runs,
// or gaps) captured against one System-Design artifact, in episodeAccess's own
// (append) order. A pass-through over episodeAccess.ListEpisodes scoped by
// TargetRef=artifactKind, mapped to the contract EpisodeRecordView.
func (m *deliveryManager) listArtifactEpisodes(rc fwmanager.Context, projectID ProjectID, artifactKind ArtifactKind) ([]EpisodeRecordView, error) {
	ctx := rc.Context
	if projectID == "" {
		return nil, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	// TargetRef on the ledger is the PascalCase artifactKindString(kind) form —
	// exactly what the capture-seam write path (episodeRecordFromSummary via
	// dispatchAndObserve) stamps as TargetRef, NOT the wire-name form. artifactKind
	// is typed as the contract's own ArtifactKind enum (not a bare string) so this
	// conversion cannot be skipped by a caller that only has the wire name.
	targetRef := artifactKindString(artifactKind)
	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{
		ProjectID: episode.ProjectID(projectID),
		TargetRef: &targetRef,
	})
	if err != nil {
		return nil, sdMapRAError(err, "episodeAccess.ListEpisodes")
	}
	return sdEpisodeRecordViews(records), nil
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// isEpisodeTraceNotFound reports whether err is episodeAccess.ReadTraceEvents's
// fwra.NotFound ("no trace file for episode ...") — the signal that a record
// with a stamped TracePath still has nothing to read (a gap's trace was never
// written, or a local trace file was pruned). Distinct from sdMapRAError's general
// NotFound handling: here it is NOT an error at all, it means "empty timeline".
func isEpisodeTraceNotFound(err error) bool {
	var raErr *fwra.Error
	return errors.As(err, &raErr) && raErr.Kind == fwra.NotFound
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// findEpisodeRecord returns the record whose EpisodeID matches id, if any.
func findEpisodeRecord(records []episode.EpisodeRecord, id string) (episode.EpisodeRecord, bool) {
	for _, r := range records {
		if r.EpisodeID == id {
			return r, true
		}
	}
	return episode.EpisodeRecord{}, false
}

// sdEpisodeRecordViews maps a slice of ledger records onto the contract view type.
func sdEpisodeRecordViews(records []episode.EpisodeRecord) []EpisodeRecordView {
	out := make([]EpisodeRecordView, 0, len(records))
	for _, r := range records {
		out = append(out, sdEpisodeRecordToView(r))
	}
	return out
}

// sdEpisodeRecordToView maps one episodeAccess ledger record onto this contract's
// OWN copy of the view shape (EpisodeRecordView mirrors episodeAccess.EpisodeRecord
// field-for-field; contracts are self-contained, so this is an intentional
// duplicate of the mapping episodeAccess itself owns, not a shared function).
func sdEpisodeRecordToView(r episode.EpisodeRecord) EpisodeRecordView {
	v := EpisodeRecordView{
		EpisodeID:      r.EpisodeID,
		Kind:           episodeViewKind(r.Kind),
		TargetRef:      r.TargetRef,
		WorkerClass:    r.WorkerClass,
		Model:          r.Model,
		Usage:          EpisodeUsage(r.Usage),
		CostUSD:        r.CostUSD,
		NumTurns:       r.NumTurns,
		ToolCallCounts: r.ToolCallCounts,
		StartedAt:      r.StartedAt,
		EndedAt:        r.EndedAt,
		Outcome:        sdEpisodeViewOutcome(r.Outcome),
		GapReason:      r.GapReason,
		TracePath:      r.TracePath,
	}
	if r.Lineage != nil {
		l := EpisodeLineage(*r.Lineage)
		v.Lineage = &l
	}
	if r.StreamedUsage != nil {
		u := EpisodeUsage(*r.StreamedUsage)
		v.StreamedUsage = &u
	}
	if len(r.SubagentSpans) > 0 {
		spans := make([]SubagentSpan, 0, len(r.SubagentSpans))
		for _, s := range r.SubagentSpans {
			spans = append(spans, SubagentSpan(s))
		}
		v.SubagentSpans = spans
	}
	return v
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeViewKind maps the episodeAccess RA's Kind onto this contract's own copy
// of the enum. Written as a TOTAL switch rather than a numeric cast so a future
// divergence between the two independently-versioned contracts is a compile-time
// conversation, not silent drift (mirrors episodeOutcomeFrom above).
func episodeViewKind(k episode.EpisodeKind) EpisodeKind {
	switch k {
	case episode.EpisodeKindDesign:
		return EpisodeKindDesign
	case episode.EpisodeKindConstruction:
		return EpisodeKindConstruction
	case episode.EpisodeKindReview:
		return EpisodeKindReview
	case episode.EpisodeKindRework:
		return EpisodeKindRework
	case episode.EpisodeKindAnswer:
		return EpisodeKindAnswer
	default:
		// Unreachable for the five defined episode.EpisodeKind values above (the
		// exhaustive linter enforces that every real variant has its own case);
		// kept as a defensive fallback for an out-of-range ordinal.
		return EpisodeKindDesign
	}
}

// sdEpisodeViewOutcome maps the episodeAccess RA's Outcome onto this contract's own
// copy of the enum. Same total-switch rationale as episodeViewKind.
func sdEpisodeViewOutcome(o episode.EpisodeOutcome) EpisodeOutcome {
	switch o {
	case episode.EpisodeSucceeded:
		return EpisodeSucceeded
	case episode.EpisodeFailed:
		return EpisodeFailed
	case episode.EpisodeCancelled:
		return EpisodeCancelled
	case episode.EpisodeGap:
		return EpisodeGap
	default:
		// Unreachable for the four defined episode.EpisodeOutcome values above;
		// defensive fallback for an out-of-range ordinal (mirrors episodeOutcomeFrom's
		// own default, which also lands on the "gap" reading — the safe direction).
		return EpisodeGap
	}
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeTimelineEvents stitches the raw trace lines mined from episodeAccess into
// sequenced TimelineEvents: seq is 1-based and positional (the ledger's own
// ordering — trace files are append-only), eventType is lifted from each line's
// top-level "type" field (the same field streamEvent.Type reads off the CLI's
// stream-json protocol), and raw is carried through verbatim for the UI.
func episodeTimelineEvents(raw []json.RawMessage) []TimelineEvent {
	events := make([]TimelineEvent, 0, len(raw))
	for i := range raw {
		events = append(events, TimelineEvent{
			Seq:       int64(i + 1),
			EventType: episodeTraceEventType(raw[i]),
			Raw:       &raw[i],
		})
	}
	return events
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeTraceEventType extracts the "type" field from one raw trace event line.
// A line that fails to decode, or decodes with no "type", maps to "unknown"
// rather than failing the whole timeline — a partially-corrupt trace still owes
// every OTHER event its identity.
func episodeTraceEventType(raw json.RawMessage) string {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.Type == "" {
		return "unknown"
	}
	return probe.Type
}

// ---------------------------------------------------------------------------
// PROJECT-DESIGN RAIL — moved verbatim from internal/manager/projectdesign/
// projectdesignmanager.go at stage 4a. Bodies are unchanged; only package-private
// names that collided with another rail were renamed (the collision table is in
// docs/superpowers/plans/2026-09-25-activity-experience-stage4a.md, Task 6 Step 2b,
// and in this commit's message). 4b replaces this block with the generic DAG child.
// ---------------------------------------------------------------------------

// ProjectDesignManager is the generated service-contract interface for this component
// — the public use-case surface of the projectDesignManager façade
// (projectDesignManager.md §2). Each op leads with the Manager-layer call Context
// (fwmanager.Context, embedding context.Context + the Principal); the *projectDesignManager derives
// ctx := rc.Context inside. The concrete *projectDesignManager satisfies it; the consumer-side
// dependency seams (agenticJobAccess / sourceControlRail) + the Temporal
// pdWorkflows struct stay hand-written and are NOT part of this contract.

// pdCheckNoReplyTo refuses a batch carrying ANY replyTo (CONTROLLER RULING P13). Phase-2 reply
// ROUTING is a Stage-2 deliverable: neither this Manager nor its co-author workflow can append
// an utterance into an existing thread, so a replyTo that arrived here could only be converted
// into a fresh unanchored comment — silently detaching the reply from the conversation it
// answers, which is the exact loss design §3.7 exists to prevent. Until Stage 2 routes it,
// refuse loudly: an obvious ContractMisuse beats a silent corruption of the ledger.
//
// THE REFUSAL STAYS, AND THE CLIENT FOLDS (stage-4a pre-final ruling, mirroring R2's
// "the asymmetry is honest until 4b"): the M0 gate DOES offer replies, so the SPA folds a
// Phase-2 reply's text into the comment or question it sends and drops the replyTo it
// cannot route (webApp reviewBatch.ts decisionFeedbackFor / askEntriesFor). The Manager
// does NOT fold on the caller's behalf — a server that quietly rewrote a routed reply
// into a fresh thread would be the silent detachment this check exists to refuse.
func pdCheckNoReplyTo(incoming []AnchoredComment) error {
	for _, c := range incoming {
		if c.ReplyTo != "" {
			return newError(fwmanager.ContractMisuse,
				"replyTo is not supported on Phase-2 (project design) yet — threaded replies are a Stage-2 deliverable; until then a Phase-2 comment can only open a new thread (offending replyTo: "+c.ReplyTo+")")
		}
	}
	return nil
}

// pdSessionStageLabel renders a ProjectSessionStage as a short human label for the precondition
// messages.
func pdSessionStageLabel(s ProjectSessionStage) string {
	switch s {
	case ProjectSessionStageUnknown:
		return "not started"
	case ProjectStageDrafting:
		return "drafting"
	case ProjectStageAssemblingSDP:
		return "assembling SDP"
	case ProjectStageAwaitingReview:
		return "awaiting review"
	case ProjectStageRedrafting:
		return "redrafting"
	case ProjectStageCommitted:
		return "committed"
	case ProjectStageWithdrawn:
		return "withdrawn"
	case ProjectStageRefused:
		return "refused"
	case ProjectStageDraftFailed:
		return "draft failed"
	}
	// Unreachable for the nine defined ProjectSessionStage values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a
	// defensive fallback for an out-of-range ordinal.
	return "unknown"
}

// AdvanceToConstruction — op 2.4. Temporal Workflow (entry; StartWorkflow,
// workflow id {projectId}:phaseAdvance:projectDesign). Returns the gating outcome.
//
// F55 STALE-SLOT GATE (Phase-2 twin). A back-edge amendment flags every downstream committed
// slot StaleBasis. Sealing Phase 2 over a stale committed slot silently advances to
// construction on a shifted basis. Before starting the seal workflow, refuse with
// FailedPrecondition naming the stale in-scope (Phase-2) slots — UNLESS the caller explicitly
// acknowledges (acknowledgeStale). The message names the slots so a consumer knows what to
// reconcile.
func (m *deliveryManager) AdvanceToConstruction(rc fwmanager.Context, projectID ProjectID, acknowledgeStale bool) (PhaseAdvanceResult, error) {
	ctx := rc.Context
	if projectID == "" {
		return PhaseAdvanceResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !acknowledgeStale {
		if proj, rerr := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID)); rerr == nil {
			if stale := staleCommittedPhase2Kinds(proj); len(stale) > 0 {
				return PhaseAdvanceResult{}, newError(fwmanager.FailedPrecondition,
					fmt.Sprintf("cannot advance to construction: %d committed artifact(s) are stale and must be reconciled first (%s). Re-run the design for each, or advance anyway by acknowledging the staleness.",
						len(stale), strings.Join(stale, ", ")))
			}
		}
	}
	return m.sealPhase(ctx, projectID, projectstate.PhaseConstruction, phase2SealGate)
}

// phase2SealGate is Phase 2's seal condition: every Phase2RequiredKinds() slot is committed
// AND an option is BOUND (the committed SdpReview's Recommendation is non-empty). There is no
// artifactValidationEngine call — there is no Phase-2 verb on the frozen surface, so the
// slot-committed + option-bound gate IS the standard check for this increment.
func phase2SealGate(proj projectstate.Project) []ArtifactKind {
	var missing []ArtifactKind
	for _, kind := range projectstate.Phase2RequiredKinds() {
		if pdSlotFor(proj, kind).Status != projectstate.ReviewCommitted {
			missing = append(missing, fromPSKind(kind))
		}
	}
	// Only flag the unbound option when the review IS committed; otherwise it is already missing.
	if !optionBound(proj) && pdSlotFor(proj, projectstate.KindSdpReview).Status == projectstate.ReviewCommitted {
		missing = append(missing, KindSdpReview)
	}
	return missing
}

// optionBound reports whether the project's committed SdpReview binds an option (a non-empty
// Recommendation).
func optionBound(proj projectstate.Project) bool {
	slot := proj.SdpReview
	if slot.Status != projectstate.ReviewCommitted || slot.Model == nil {
		return false
	}
	rev, ok := slot.Model.(*projectstate.SdpReview)
	return ok && rev.Recommendation != ""
}

// sealSystemDesignPhase is the operator's explicit Phase-1 seal, and since stage 4b1 Task 13
// it is a SYNCHRONOUS Manager-side write rather than a short-lived Temporal workflow.
//
// WHY THE WORKFLOW WENT AND NOTHING WAS LOST. PhaseAdvanceWorkflow existed to run one gate
// read plus one AdvancePhase from inside a durable execution, and it was started and
// immediately awaited by this very op — a workflow whose whole life was the caller's own
// request. The seal's real home is now the CHILD: sealSystemDesign asks this same question
// after every design slot commit, so a project that finishes its design walk seals itself.
// What is left here is the OPERATOR's override for a project whose seal did not fire, and for
// that a synchronous RA write is strictly more honest — the caller gets the answer rather than
// a workflow id they then have to poll.
//
// The two PRE-SEAL gates over head state are carried verbatim, because both are refusals a
// seal must make and neither is the child's:
//
//   - STD-FAIL-OPEN: a committed standard check that still carries a FAIL item means the
//     Phase-1 design gate is red, and sealing over it would advance on an unmet standard. A
//     fail is not a staleness the caller can wave through, so this gate ignores acknowledgeStale.
//   - STALE-UNACKED (F55): a committed slot whose basis a back-edge amendment invalidated is
//     refused unless the caller explicitly acknowledges it.
func (m *deliveryManager) sealSystemDesignPhase(rc fwmanager.Context, projectID ProjectID, acknowledgeStale bool) (PhaseAdvanceResult, error) {
	ctx := rc.Context
	if projectID == "" {
		return PhaseAdvanceResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if proj, rerr := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID)); rerr == nil {
		if fails := standardCheckFailItems(proj); len(fails) > 0 {
			return PhaseAdvanceResult{}, newError(fwmanager.FailedPrecondition,
				fmt.Sprintf("cannot advance phase: the system-design standard check has %d failing item(s) (%s); resolve or waive them before sealing Phase 1.",
					len(fails), strings.Join(fails, "; ")))
		}
		if !acknowledgeStale {
			if stale := staleCommittedPhase1Kinds(proj); len(stale) > 0 {
				return PhaseAdvanceResult{}, newError(fwmanager.FailedPrecondition,
					fmt.Sprintf("cannot advance phase: %d committed artifact(s) are stale and must be reconciled first (%s). Re-run the design for each, or advance anyway by acknowledging the staleness.",
						len(stale), strings.Join(stale, ", ")))
			}
		}
	}
	return m.sealPhase(ctx, projectID, projectstate.PhaseProjectDesign, phase1SealGate)
}

// phase1SealGate is Phase 1's seal condition: every Phase1RequiredKinds() slot is committed.
// Per the agentic pivot (§0d.5) there is no in-workflow re-validation — validity is the
// required CI check inside the Action (a slot only reaches ReviewCommitted after its design
// job's CI validation went green AND the architect approved), so the all-committed gate IS the
// seal condition.
func phase1SealGate(proj projectstate.Project) []ArtifactKind {
	var missing []ArtifactKind
	for _, kind := range phase1RequiredKinds() {
		if slotFor(proj, kind).Status != projectstate.ReviewCommitted {
			missing = append(missing, kind)
		}
	}
	return missing
}

// sealPhase is the ONE seal body the two phases share: read, ask the phase's own gate which
// required slots are missing, and — when none is — advance once under optimistic concurrency.
//
// A missing slot is NOT an error: it is the answer (Advanced=false plus the list), which is
// what the SPA renders as "these artifacts still have to land". Only the write can fail.
//
// sealsInto NAMES THE PHASE THIS SEAL PRODUCES, and it is the guard, not decoration. A seal
// is reachable CONCURRENTLY with the child's own seal of the same phase: the child's
// gate-passed handler advances on an M0 approve (passRound → completeProjectDesign →
// advanceToConstruction) while the SPA's approve returns and the façade is called on the same
// decision. Without this read-back guard the façade's gate would pass a SECOND time over the
// already-sealed project and issue a second AdvancePhase. The child guards itself the same
// way (advanceToConstruction re-reads and early-returns on `Phase >= PhaseConstruction`); the
// façade now does too, so whichever writer wins, the loser is an honest no-op rather than a
// second increment. The RA's own ceiling (projectstate.AdvancePhase) is the backstop under
// both, for the window between this read and that write.
//
// Already-sealed is Advanced=false with NO missing artifacts — "nothing was owed and nothing
// was done" — which is the only answer that is neither a lie (Advanced=true claims a write
// that did not happen) nor an error (there is nothing wrong with sealing twice).
func (m *deliveryManager) sealPhase(
	ctx context.Context, projectID ProjectID, sealsInto projectstate.Phase, gate func(projectstate.Project) []ArtifactKind,
) (PhaseAdvanceResult, error) {
	psID := projectstate.ProjectID(projectID)
	var lastErr error
	for range acknowledgeStaleMaxAttempts {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			if isResearchReadNotFound(err) {
				// A project with no row has committed nothing, so the gate's whole list is missing.
				return PhaseAdvanceResult{Advanced: false, MissingArtifacts: gate(projectstate.Project{ID: psID})}, nil
			}
			return PhaseAdvanceResult{}, mapReadProjectError(err)
		}
		if proj.Phase >= sealsInto {
			return PhaseAdvanceResult{Advanced: false}, nil
		}
		if missing := gate(proj); len(missing) > 0 {
			return PhaseAdvanceResult{Advanced: false, MissingArtifacts: missing}, nil
		}
		if _, err := m.projectState.AdvancePhase(fwra.Context{Context: ctx}, psID, proj.Version); err != nil {
			if isRAConflict(err) {
				lastErr = err
				continue
			}
			return PhaseAdvanceResult{}, mapRAError(err, "projectStateAccess.AdvancePhase")
		}
		return PhaseAdvanceResult{Advanced: true}, nil
	}
	return PhaseAdvanceResult{}, fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "advancePhase: exhausted conflict retries")
}

// staleCommittedPhase2Kinds returns the wire names of every COMMITTED Phase-2 slot that carries
// StaleBasis (a back-edge amendment invalidated its basis) — the set AdvanceToConstruction must
// refuse to seal over unless the caller acknowledges. Order follows Phase2RequiredKinds so the
// message reads deterministically.
func staleCommittedPhase2Kinds(proj projectstate.Project) []string {
	var stale []string
	for _, kind := range projectstate.Phase2RequiredKinds() {
		slot := pdSlotFor(proj, kind)
		if slot.Status == projectstate.ReviewCommitted && slot.StaleBasis {
			stale = append(stale, kind.WireName())
		}
	}
	return stale
}

// completedSessionView derives the honest session view for a CoAuthor/SDP run that closed
// NORMALLY (COMPLETED). The replayed sessionState query is NOT trusted for such a run (it can
// return a stale mid-flight stage — the P0-2 "GENERATING forever" wedge on an already-committed
// artifact), so the view is rebuilt from the DURABLE slot on main.
func (m *deliveryManager) planCompletedSessionView(ctx context.Context, projectID ProjectID, kind ArtifactKind) (ProjectSessionStateView, error) {
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return ProjectSessionStateView{}, pdMapReadProjectError(err)
	}
	return pdCommittedSessionView(projectID, kind, pdSlotFor(proj, toPSKind(kind)))
}

// pdCommittedSessionView projects the durable slot of a COMPLETED session onto a
// ProjectSessionStateView. A committed slot renders the committed view (ProjectStageCommitted + the committed
// model + the durable review thread). A withdrawn slot renders ProjectStageWithdrawn. Any other
// terminal-but-uncommitted state renders an honest ProjectStageDraftFailed terminal carrying a neutral
// reason — NEVER ProjectStageDrafting, so the SPA never wedges on an infinite "GENERATING" spinner.
func pdCommittedSessionView(projectID ProjectID, kind ArtifactKind, slot projectstate.ArtifactSlot) (ProjectSessionStateView, error) {
	switch slot.Status {
	case projectstate.ReviewCommitted:
		draft, err := draftModelFor(kind, slot.Model)
		if err != nil {
			return ProjectSessionStateView{}, newError(fwmanager.Infrastructure, err.Error())
		}
		return ProjectSessionStateView{
			ProjectID:    projectID,
			ArtifactKind: kind,
			Stage:        ProjectStageCommitted,
			Draft:        draft,
			ReviewThread: reviewThreadToView(slot.ReviewThread),
		}, nil
	case projectstate.ReviewWithdrawn:
		return ProjectSessionStateView{
			ProjectID:    projectID,
			ArtifactKind: kind,
			Stage:        ProjectStageWithdrawn,
			Draft:        DraftModel{Kind: artifactKindWireName(kind)},
		}, nil
	case projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected:
		// Any non-committed / non-withdrawn terminal status renders the honest
		// ProjectStageDraftFailed view (never ProjectStageDrafting — the anti-wedge rule).
		fallthrough
	default:
		reason := "the design session ended without committing an artifact. Retry to start a fresh draft."
		return ProjectSessionStateView{
			ProjectID:     projectID,
			ArtifactKind:  kind,
			Stage:         ProjectStageDraftFailed,
			Draft:         DraftModel{Kind: artifactKindWireName(kind)},
			FailureReason: &reason,
		}, nil
	}
}

// isRAReadNotFound reports whether err is a RAW projectStateAccess fwra.NotFound
// (a brand-new / unknown project) returned DIRECTLY on the sync façade read path —
// distinct from workflow.go's isReadNotFound, which inspects the Temporal-wrapped
// ApplicationError on the replayed Activity path.
func isRAReadNotFound(err error) bool {
	var raErr *fwra.Error
	return errors.As(err, &raErr) && raErr.Kind == fwra.NotFound
}

// pdMapReadProjectError converts a projectStateAccess.ReadProject error on the sync
// spine-ordering-gate read path into a fwmanager.Error: fwra.NotFound → NotFound
// (unknown project), everything else → Infrastructure. (fwra.NotFound is handled
// specially by the RequestArtifactDraft caller as an uncommitted predecessor; this
// mapper covers the non-NotFound faults.)
func pdMapReadProjectError(err error) error {
	if isRAReadNotFound(err) {
		return newError(fwmanager.NotFound, err.Error())
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// fromPSKind converts a canonical projectstate.ArtifactKind to projectdesign's OWN
// ArtifactKind (ordinal-preserving) at the read boundary.
func fromPSKind(k projectstate.ArtifactKind) ArtifactKind { return ArtifactKind(k) }

// artifactKindIsPhase2 reports whether the kind belongs to The Method's Phase 2.
func artifactKindIsPhase2(k ArtifactKind) bool { return toPSKind(k).IsPhase2() }

func (m *deliveryManager) ackPlanStaleBasis(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, note string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if strings.TrimSpace(note) == "" {
		return newError(fwmanager.ContractMisuse, "an acknowledgement requires a non-empty note — it is the reviewer's durable justification, and it also keys the idempotency of the ack")
	}
	if !artifactKindIsPhase2(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-2 kind")
	}
	// F-GTD-12: an acknowledge is a MAIN-branch write (the StaleBasis clear + the staleAck
	// entry commit on main). While a co-author session is LIVE for this slot — on a committed
	// slot that is by definition an in-flight AMENDMENT — that main write turns the session's
	// review PR merge-DIRTY, so the eventual approve's merge fails with a Conflict and the
	// workflow bounces back to AwaitingReview looking like a silent no-op to the reviewer.
	// Refuse up front: reconcile RIDES the amendment (its merge clears the staleness).
	if err := m.refusePlanAckDuringLiveSession(rc, projectID, kind); err != nil {
		return err
	}
	key := acknowledgeStaleIdempotencyKey(projectID, kind, note)
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)

	var lastErr error
	for range acknowledgeStaleMaxAttempts {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			return pdMapReadProjectError(err)
		}
		_, err = m.projectState.AcknowledgeStaleBasis(fwra.Context{Context: ctx}, psID, proj.Version, psKind, note, key)
		if err == nil {
			return nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue
		}
		return mapStaleAckError(err)
	}
	return fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "AcknowledgeStaleBasis: exhausted conflict retries")
}

// refuseAckDuringLiveSession is the F-GTD-12 guard: while the target kind has a LIVE
// co-author (amendment) session, the acknowledge is refused with a FailedPrecondition
// (the wire's 409/"failed_precondition" conflict shape). Liveness is read through
// GetSessionState — the SAME Describe-then-Query path the review gate and the SPA trust
// (a dead run synthesizes ProjectStageDraftFailed; a COMPLETED run is rebuilt from the durable
// slot) — so ack gating always agrees with what the reviewer sees on screen. A NotFound
// (no session ever ran for this slot) passes.
func (m *deliveryManager) refusePlanAckDuringLiveSession(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) error {
	view, err := m.planCompletedSessionView(rc, projectID, kind)
	if err != nil {
		var me *fwmanager.Error
		if errors.As(err, &me) && me.Kind == fwmanager.NotFound {
			return nil
		}
		return err
	}
	if !pdSessionStageIsLive(view.Stage) {
		return nil
	}
	return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
		"cannot mark this artifact reviewed: its amendment session is still open (currently %s). Reconcile rides the amendment — acknowledging now would commit to main and merge-conflict the amendment's review PR. Approve or withdraw the session first.",
		pdSessionStageLabel(view.Stage)))
}

// pdSessionStageIsLive reports whether a co-author session stage means the session still
// OWNS the slot (its branch/PR is open or recoverable): drafting / assembling /
// awaiting review / redrafting, plus the ProjectStageDraftFailed recovery gate (the session is
// suspended there with its branch and PR intact — a Retry resumes it). The terminal
// stages (committed / withdrawn / refused) and the unknown zero value are NOT live.
func pdSessionStageIsLive(s ProjectSessionStage) bool {
	switch s {
	case ProjectStageDrafting, ProjectStageAssemblingSDP, ProjectStageAwaitingReview, ProjectStageRedrafting, ProjectStageDraftFailed:
		return true
	case ProjectSessionStageUnknown, ProjectStageCommitted, ProjectStageWithdrawn, ProjectStageRefused:
		return false
	default:
		return false
	}
}

// mapStaleAckError surfaces the RA's ContractMisuse (uncommitted / unknown kind) and NotFound
// (unknown project) as their manager equivalents; everything else is Infrastructure.
func mapStaleAckError(err error) error {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.Unknown, fwra.Transient, fwra.RateLimited, fwra.Infrastructure,
			fwra.Auth, fwra.Conflict, fwra.QuotaExhausted, fwra.ContentPolicy:
			// "everything else is Infrastructure" per the doc comment above.
			return newError(fwmanager.Infrastructure, err.Error())
		default:
			return newError(fwmanager.Infrastructure, err.Error())
		}
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// askquestions.go implements the question-comments op for Project Design (twin of the
// systemdesign implementation; founder-ratified 2026-07-05): AskQuestions appends clarifying
// QUESTIONS to a Phase-2 artifact's review ledger WITHOUT a redraft and dispatches a
// lightweight ANSWER job so the addressed role answers each in place. Open questions do NOT
// block approve; asking works on a committed artifact (main) and on a live session (branch).

// Dispatch inputs for the design jobs. Project Design has no PM-critique, so its dispatch
// path historically carried no job_mode; under thin dispatch the MCP scopes its ambient
// mode on this input, so BOTH the draft and answer jobs now set it — pdJobModeDraft on the
// workflow-side draft dispatch, pdJobModeAnswer on this manager-side answer job.
const (
	pdDispatchInputJobMode = "job_mode"
	pdJobModeDraft         = "draft"
	pdJobModeAnswer        = "answer"
)

// AskQuestions — the Project-Design question-comments op. See the systemdesign twin for the
// full contract; the only differences are the Phase-2 kind gate and the Phase-2 pdSlotFor.
//
// DISPATCH RECOVERY (F82): the answer job is BEST-EFFORT — the questions are seeded durably
// first, then a lightweight answer job is dispatched. A dispatch MISS (pipeline/repo not
// configured, repo unresolved, or a workflow_dispatch fault) is now LOGGED LOUDLY server-side
// (it was previously discarded, and the construction-pipeline RA has no logger, so a miss
// vanished — an open question that would never be answered with zero operator signal). To
// RECOVER a dropped dispatch, simply CALL AskQuestions AGAIN with the same questions: the seed
// is idempotent on its content key, so NO ledger entry is duplicated (the existing entries'
// round is reused so the minted ids still match), while the answer-job dispatch RE-FIRES via a
// per-call-unique key.
func (m *deliveryManager) askPlanQuestions(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, addressee string, questions []AnchoredComment) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-2 kind")
	}
	switch addressee {
	case projectstate.ReviewAddresseePM, projectstate.ReviewAddresseeArchitect:
		// ok
	default:
		return newError(fwmanager.ContractMisuse, "addressee must be \"pm\" or \"architect\"")
	}
	// RULING P13 + design §3.7: a question OPENS its own thread, so a replyTo has no meaning
	// here and questionsToLedger would drop it. See pdCheckNoReplyTo.
	if perr := pdCheckNoReplyTo(questions); perr != nil {
		return perr
	}
	qs := questionsToLedger(addressee, questions)
	if len(qs) == 0 {
		return newError(fwmanager.ContractMisuse, "no questions to ask (every question needs text)")
	}

	// MAIN, BESIDE THE SLOT (ratified 4b2; 4b1 Q6) — see the Phase-1 twin above for the whole
	// argument. In short: the resolver that used to ask for a session branch was dead BY TYPE,
	// and main is also the RIGHT answer, because a question's thread outlives the draft it is
	// about.
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)
	key := pdAskQuestionsIdempotencyKey(projectID, kind, "", qs)

	var lastErr error
	for range askQuestionsMaxAttempts {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			return pdMapReadProjectError(err)
		}
		thread := pdSlotFor(proj, psKind).ReviewThread
		round := nextQuestionRound(thread)
		if r, ok := existingQuestionRound(thread, qs); ok {
			// A prior ask already seeded these exact questions (its answer-job dispatch may
			// have been dropped — F82). Reuse their round so the minted ids match the EXISTING
			// ledger entries, and the re-fired answer job answers the right comments.
			round = r
		}
		_, err = m.designSession.SeedReviewCommentsOnBranch(fwra.Context{Context: ctx}, psID, proj.Version, "", psKind, round, qs, nil, key)
		if err == nil {
			minted := make([]projectstate.ReviewComment, len(qs))
			for i := range qs {
				minted[i] = qs[i]
				minted[i].ID = projectstate.ReviewCommentID(round, i)
			}
			m.dispatchAnswerJob(ctx, projectID, kind, "", addressee, minted)
			return nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue
		}
		return pdMapReadProjectError(err)
	}
	return fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "AskQuestions: exhausted conflict retries")
}

func pdAskQuestionsIdempotencyKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment) fwra.IdempotencyKey {
	h := fnv.New64a()
	_, _ = h.Write([]byte(branch))
	_, _ = h.Write([]byte{0})
	for _, q := range qs {
		_, _ = h.Write([]byte(q.Addressee))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(q.Anchor))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(q.Text))
		_, _ = h.Write([]byte{0})
	}
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%d:askQuestions:%x", projectID, int(kind), h.Sum64()))
}

// pdAnswerEpisodeWatch is the bounded observe-then-append loop behind watchAnswerEpisode,
// broken out with its timings injected so it can be exercised deterministically in tests.
type pdAnswerEpisodeWatch struct {
	pipeline agenticjob.AgenticJobAccess
	episodes episode.EpisodeAccess
	poll     time.Duration
	window   time.Duration
	log      *slog.Logger
}

// run polls handle to a terminal phase (or to the window's end) and appends the ONE ledger
// record the dispatch owes. Blocking — watchAnswerEpisode spawns it.
func (w pdAnswerEpisodeWatch) run(ctx context.Context, projectID ProjectID, targetRef string, handle agenticjob.PipelineHandle) {
	if w.pipeline == nil || w.episodes == nil {
		return
	}
	watchCtx, cancelWatch := context.WithTimeout(ctx, w.window)
	defer cancelWatch()

	obs, terminal := w.observeToTerminal(watchCtx, handle)
	if terminal && episodeVenueIsRemote(obs.RunURL) {
		// Remote venue mines no episode in v1 — nothing was lost, so record nothing.
		return
	}
	rec := w.answerRecord(obs, terminal, targetRef, handle)

	// THE APPEND MUST NOT RIDE watchCtx. On the DEADLINE path observeToTerminal returned
	// precisely BECAUSE watchCtx expired, so appending under it would hand the ledger an
	// already-cancelled context — making the gap record the deadline exists to write the
	// one write guaranteed to fail. Derive a fresh, cancellation-free budget from the
	// caller's context instead. (Today's AppendEpisode realisations ignore the context
	// entirely, so this is latent rather than live; a store that honours it would turn the
	// never-silent guarantee into a silent loss on exactly the path that needs it most.)
	appendCtx, cancelAppend := context.WithTimeout(context.WithoutCancel(ctx), answerEpisodeAppendWindow)
	defer cancelAppend()
	w.appendRecord(appendCtx, projectID, rec, handle)
}

// appendRecord writes the record with a small BOUNDED retry. The workflow-side capture
// gets Temporal's retry envelope for free; this path has none, so without it a single
// transient store stumble would lose the episode outright.
func (w pdAnswerEpisodeWatch) appendRecord(ctx context.Context, projectID ProjectID, rec episode.EpisodeRecord, handle agenticjob.PipelineHandle) {
	key := fwra.IdempotencyKey("answerEpisode:" + string(handle))
	var err error
	for attempt := 1; attempt <= answerEpisodeAppendAttempts; attempt++ {
		err = w.episodes.AppendEpisode(fwra.Context{Context: ctx, IdempotencyKey: key},
			episode.ProjectID(projectID), rec)
		if err == nil {
			return
		}
		if attempt == answerEpisodeAppendAttempts ||
			!waitOrDone(ctx, time.Duration(attempt)*answerEpisodeAppendBackoff) {
			break
		}
	}
	w.log.Error("answer-job episode NOT recorded: ledger append failed after its bounded retry",
		"episodeId", rec.EpisodeID, "attempts", answerEpisodeAppendAttempts, "err", err.Error())
}

// observeToTerminal polls the dispatched job until it reaches a terminal phase, the window
// closes, or the RA faults. terminal=false means the second or third — the caller turns
// that into a gap record.
func (w pdAnswerEpisodeWatch) observeToTerminal(ctx context.Context, handle agenticjob.PipelineHandle) (agenticjob.PipelineObservation, bool) {
	var last agenticjob.PipelineObservation
	cancelGrace := 0
	for {
		obs, err := w.pipeline.ObserveAgenticJob(fwra.Context{Context: ctx}, handle)
		if err != nil {
			return last, false
		}
		last = obs
		if terminal, done := w.classify(obs, &cancelGrace); done {
			return obs, terminal
		}
		if !waitOrDone(ctx, w.poll) {
			return last, false
		}
	}
}

// classify decides whether THIS observation ends the watch. A terminal observation with a
// summary always does. A terminal observation WITHOUT one ends it too — except for the
// CANCEL RACE, where the phase flips synchronously while the agent subprocess is still
// unwinding: that gets maxLateEpisodePolls further polls (the same grace the workflow-side
// capture gives it) before the run is written off.
func (w pdAnswerEpisodeWatch) classify(obs agenticjob.PipelineObservation, cancelGrace *int) (terminal, done bool) {
	if !pdDesignPipelinePhase(obs.Phase).IsTerminal() {
		return false, false
	}
	if obs.Episode != nil || obs.Phase != agenticjob.PhaseCancelled {
		return true, true
	}
	if *cancelGrace >= maxLateEpisodePolls {
		return true, true
	}
	*cancelGrace++
	return false, false
}

// answerRecord composes the ledger record for a watched answer job: the mined summary, or
// an explicit GAP naming which of the two ways it went missing.
func (w pdAnswerEpisodeWatch) answerRecord(obs agenticjob.PipelineObservation, terminal bool, targetRef string, handle agenticjob.PipelineHandle) episode.EpisodeRecord {
	// Lineage is nil BY DESIGN: this dispatch has no durable execution behind it.
	if terminal && obs.Episode != nil {
		return episodeRecordFromSummary(*obs.Episode, episode.EpisodeKindAnswer, targetRef, nil, obs.Diagnostic)
	}
	reason := episodeMissingSummaryReason
	if !terminal {
		reason = "answer job did not reach a terminal phase within the manager-side watch window"
	}
	return episodeGapRecord(episode.EpisodeKindAnswer, targetRef, nil,
		"gap-"+episodeIDSafe(string(handle)),
		episodeGapReason(reason, obs.Diagnostic), time.Now().UTC())
}

// ---------------------------------------------------------------------------
// Episode record composition — shared by the workflow-side capture
// (coauthorphase2artifact.go) and the answer-job watch above.
// ---------------------------------------------------------------------------

// designBranch and the projectstate resolver it was promoted into are both GONE (stage 4b2):
// the design-branch scheme is retired — there is one activity branch per activity now.

// ===========================================================================
// Dispatch inputs (C-WF-DESIGN workflow_dispatch schema). These exact key names are
// the binding contract with aiarch-design.yml's workflow_dispatch.inputs.
// idempotency_token is RA-controlled and is NOT set here.
// ===========================================================================

// reviewledger.go holds the durable review-ledger seam for the projectDesign Manager
// (review-ledger feature, founder-ratified 2026-07-05) — the structural twin of the
// systemDesign Manager's reviewledger.go. Ledger STORAGE + transition rules live in
// projectstate (reviewthread.go); this is the Manager-side wiring: the ReviewComment ↔
// ReviewCommentView projection and the open-comment gate. The SetReviewCommentStatus /
// SeedReviewComments branch-mutation Activities MIGRATED (B9) onto the generated
// designSessionAccess.setReviewCommentStatusOnBranch / seedReviewCommentsOnBranch
// invokers (invokers.gen.go, reached via wf.Acts) — the ledger-extension fallback those
// custom bodies ran now lives inside the RA (projectstate/designsession.go).

// ---------------------------------------------------------------------------
// Shared Temporal identity constants (projectDesignManager.md §6.1/§6.2/§6.5).
// TaskQueue is defined in the generated worker.gen.go.
// ---------------------------------------------------------------------------

// slotAccessors is the flat kind→slot dispatch behind pdSlotFor, in table form so the
// exhaustive gate (check: map) still fails on a missing ArtifactKind exactly like the
// former switch did.
var slotAccessors = map[projectstate.ArtifactKind]func(projectstate.Project) projectstate.ArtifactSlot{
	projectstate.KindMission:              func(p projectstate.Project) projectstate.ArtifactSlot { return p.Mission },
	projectstate.KindGlossary:             func(p projectstate.Project) projectstate.ArtifactSlot { return p.Glossary },
	projectstate.KindScrubbedRequirements: func(p projectstate.Project) projectstate.ArtifactSlot { return p.ScrubbedRequirements },
	projectstate.KindVolatilities:         func(p projectstate.Project) projectstate.ArtifactSlot { return p.Volatilities },
	projectstate.KindCoreUseCases:         func(p projectstate.Project) projectstate.ArtifactSlot { return p.CoreUseCases },
	projectstate.KindSystem:               func(p projectstate.Project) projectstate.ArtifactSlot { return p.SystemDesign },
	projectstate.KindOperationalConcepts:  func(p projectstate.Project) projectstate.ArtifactSlot { return p.OperationalConcepts },
	projectstate.KindStandardCheck:        func(p projectstate.Project) projectstate.ArtifactSlot { return p.StandardCheck },
	projectstate.KindPlanningAssumptions:  func(p projectstate.Project) projectstate.ArtifactSlot { return p.PlanningAssumptions },
	projectstate.KindActivityList:         func(p projectstate.Project) projectstate.ArtifactSlot { return p.ActivityList },
	projectstate.KindNetwork:              func(p projectstate.Project) projectstate.ArtifactSlot { return p.Network },
	projectstate.KindNormalSolution:       func(p projectstate.Project) projectstate.ArtifactSlot { return p.NormalSolution },
	projectstate.KindSubcriticalSolution:  func(p projectstate.Project) projectstate.ArtifactSlot { return p.SubcriticalSolution },
	projectstate.KindCompressedSolution:   func(p projectstate.Project) projectstate.ArtifactSlot { return p.CompressedSolution },
	projectstate.KindDecompressedSolution: func(p projectstate.Project) projectstate.ArtifactSlot { return p.DecompressedSolution },
	projectstate.KindRiskModel:            func(p projectstate.Project) projectstate.ArtifactSlot { return p.RiskModel },
	projectstate.KindSdpReview:            func(p projectstate.Project) projectstate.ArtifactSlot { return p.SdpReview },
}

// pdSlotFor returns the named Project slot for a kind (Phase 1 + Phase 2). Internal
// (operates on the canonical projectstate.ArtifactKind); own-kind callers convert via
// toPSKind at the boundary. An unknown kind yields the zero slot, as before.
func pdSlotFor(proj projectstate.Project, kind projectstate.ArtifactKind) projectstate.ArtifactSlot {
	accessor, ok := slotAccessors[kind]
	if !ok {
		return projectstate.ArtifactSlot{}
	}
	return accessor(proj)
}

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the projectDesignManager
// impl. It supplies the genWorkerManifest RegisterWorker consumes: the three workflow
// bodies under their registered names, the per-activity option-preset hook, and the
// genActivities dep threading. It also hosts the external RegisterManagerWorker
// entrypoint the composition root calls (cmd/server/main.go). This Manager has ZERO
// custom Temporal Activities (B9 + its follow-up ruling: the last one,
// StageArtifactForReviewActivity, was deleted when the designSessionAccess Stage op's
// model param became the codable ModelEnvelope at the schema) — every Activity is
// generated and registered by the generated RegisterWorker.
//
// The three estimate Engines (Estimation / OperationEst / Settlement) are called DIRECTLY
// in-workflow (deterministic, by value) and are NOT Activities; the durable-execution
// in-workflow primitives (awaitSignal / startTimer) are the Manager's own code.

// Stage 4a: ONE copy now serves the projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeViewOutcome maps the episodeAccess RA's Outcome onto this contract's own
// copy of the enum. Same total-switch rationale as episodeViewKind.
func episodeViewOutcome(o episode.EpisodeOutcome) EpisodeOutcome {
	switch o {
	case episode.EpisodeSucceeded:
		return EpisodeSucceeded
	case episode.EpisodeFailed:
		return EpisodeFailed
	case episode.EpisodeCancelled:
		return EpisodeCancelled
	case episode.EpisodeGap:
		return EpisodeGap
	default:
		// Unreachable for the four defined episode.EpisodeOutcome values above;
		// defensive fallback for an out-of-range ordinal (the "gap" reading is the
		// safe direction).
		return EpisodeGap
	}
}

// Stage 4a: ONE copy now serves the projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// mapRAError translates an episodeAccess error into the Manager façade error
// model. fwra.NotFound → NotFound; fwra.ContractMisuse → ContractMisuse;
// everything else → Infrastructure with the original retryability preserved.
// label identifies the actual failing op (the opaque Detail returned to the
// client; the full cause chain stays server-side on Cause). Mirrors
// systemDesignManager's mapRAError of the same name (I4: accepted triplication —
// the ratified DesignManager merge collapses this copy).
func mapRAError(err error, label string) error {
	if err == nil {
		return nil
	}
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		case fwra.Unknown, fwra.Transient, fwra.RateLimited, fwra.Infrastructure,
			fwra.Auth, fwra.Conflict, fwra.QuotaExhausted, fwra.ContentPolicy:
			// "Everything else... → Infrastructure" per the doc comment above.
			mapped := fwmanager.Wrap(fwmanager.Infrastructure, err, label)
			mapped.Retryable = raErr.Retryable
			return mapped
		default:
			mapped := fwmanager.Wrap(fwmanager.Infrastructure, err, label)
			mapped.Retryable = raErr.Retryable
			return mapped
		}
	}
	// A non-fwra error still carries its cause for the server log while keeping
	// the client Detail opaque (label).
	return fwmanager.Wrap(fwmanager.Infrastructure, err, label)
}

// --- Phase-2 activity-list derivation: the projectstate <-> estimation conversion
// boundary (Option B full encapsulation: the estimation Engine redefines every domain
// type it uses as its own generated def and imports NO projectstate, so the Manager
// maps field-by-field here — exactly as toEstimationOption above does for
// EstimateForOption). Task 10 wires toEstimationSystemView + toProjectStateActivityList
// into the read path; DerivePlan itself (Task 5) and its call are exercised there. ---

// derefString returns *p, or fallback when p is nil or points at the empty string —
// the shared "unauthored optional -> concrete default" rule this boundary applies to
// every optional projectstate.Component attribute the Engine needs resolved.
func derefString(p *string, fallback string) string {
	if p == nil || *p == "" {
		return fallback
	}
	return *p
}

// derefBool returns *p, or false when p is nil — projectstate's UiSurface is an
// unauthored-means-absent tri-state; the Engine only ever sees a resolved bool.
func derefBool(p *bool) bool { return p != nil && *p }

// toEstimationSystemView converts the canonical System to the estimation Engine's OWN
// slim SystemView at the call boundary. Only what the derivation reads crosses:
// identity, kind, and the four typed doctrine attributes (constructionProfile,
// provisioning, uiSurface, buildStatus) — the fourth of which decides whether there is
// anything to build at all.
//
// An unauthored constructionProfile defaults to "handwritten" — the CONSERVATIVE
// direction. Defaulting to "generated" would silently delete real planned work, which
// is the one failure mode this whole derivation design exists to prevent. An
// unauthored provisioning defaults to "owned" (no vendor assumed). An unauthored
// buildStatus defaults to "" — the same conservative direction: only an explicit
// "planned" says the component has no code to build yet.
func toEstimationSystemView(sys projectstate.System) estimation.SystemView {
	comps := make([]estimation.SystemComponent, 0, len(sys.Components))
	for _, c := range sys.Components {
		comps = append(comps, estimation.SystemComponent{
			ID:                  c.ID,
			Name:                c.Name,
			Kind:                c.Kind.String(),
			ConstructionProfile: derefString(c.ConstructionProfile, "handwritten"),
			Provisioning:        derefString(c.Provisioning, "owned"),
			UiSurface:           derefBool(c.UiSurface),
			BuildStatus:         derefString(c.BuildStatus, ""),
		})
	}
	rels := make([]estimation.SystemRelationship, 0, len(sys.Relationships))
	for _, r := range sys.Relationships {
		rels = append(rels, estimation.SystemRelationship{From: r.From, To: r.To})
	}
	return estimation.SystemView{Components: comps, Relationships: rels}
}

// toProjectStateActivityList converts the Engine's derived plan back to the canonical
// ActivityList shape every existing reader already consumes (the SPA catalog
// projection, the construction pump, earned value). Only the Activities cross here —
// DerivedPlan's Dependencies/Milestones feed the Network artifact, not this slot.
func toProjectStateActivityList(plan estimation.DerivedPlan) projectstate.ActivityList {
	acts := make([]projectstate.ActivityItem, 0, len(plan.Activities))
	for _, a := range plan.Activities {
		acts = append(acts, projectstate.ActivityItem{
			Name:        a.Name,
			Title:       a.Title,
			EffortDays:  a.EffortDays,
			RiskBucket:  int(a.RiskBucket),
			WorkerClass: a.WorkerClass,
			Coding:      a.Coding,
			ComponentID: a.ComponentID,
		})
	}
	return projectstate.ActivityList{Activities: acts}
}

// toProjectStateMilestones converts the Engine's derived milestones to the projectstate
// shape (I1, 2026-08-10). Only Id/DependsOn cross here — Name and Public are display-only
// authoring decorations with no derivation source (DerivedPlan carries no opinion on
// them, same as AdditiveMilestone in the delta vocabulary), so they are left at their
// zero value; a caller rendering these for a human MUST overlay the existing committed
// Name/Public rather than take them from here. What this DOES make comparable — the
// thing the drift gate (TestDerivedPlanMatchesCommittedState) actually needs — is the
// STRUCTURE: which milestones exist and what they depend on.
func toProjectStateMilestones(plan estimation.DerivedPlan) []projectstate.NetworkMilestone {
	out := make([]projectstate.NetworkMilestone, 0, len(plan.Milestones))
	for _, m := range plan.Milestones {
		out = append(out, projectstate.NetworkMilestone{ID: m.Id, DependsOn: m.DependsOn})
	}
	return out
}

// MaterializeActivityPlan is the single render-on-read entry point for the Phase-2 plan:
// it derives the baseline from the committed System, applies the authored deltas, and
// returns the canonical shapes every existing reader already consumes.
//
// No core-use-case ids are consumed here: the founder's 2026-08-09 ruling drops I-*
// integration activities entirely (App A makes integration a phase of every activity's
// own lifecycle, so a separate I-* would charge the same work twice), which was the only
// thing that ever read them. SystemView.CoreUseCaseIDs is gone from the contract
// (Task 10a); do not resurrect a use-case-id parameter here to feed it.
//
// Milestones are returned alongside activities/dependencies (I1, 2026-08-10) so a caller
// — chiefly the derived-plan drift gate — can compare the FULL derived network, not just
// the activity list: before this, a slot-5 relationship edit that only shifted milestone
// fan-in re-derived nothing the gate could see.
//
// A delta that violates the vocabulary fails the READ, loudly. Silently dropping a bad
// delta would be the zombie failure mode returning by another door.
func MaterializeActivityPlan(
	sys projectstate.System,
	deltas estimation.ActivityListDeltas,
) (projectstate.ActivityList, []projectstate.NetworkDependency, []projectstate.NetworkMilestone, error) {
	view := toEstimationSystemView(sys)

	plan, err := estimation.NewEstimationEngine().DerivePlan(fweng.Context{}, view, deltas)
	if err != nil {
		return projectstate.ActivityList{}, nil, nil, err
	}

	deps := make([]projectstate.NetworkDependency, 0, len(plan.Dependencies))
	for _, d := range plan.Dependencies {
		deps = append(deps, projectstate.NetworkDependency{Activity: d.Activity, DependsOn: d.DependsOn})
	}
	return toProjectStateActivityList(plan), deps, toProjectStateMilestones(plan), nil
}

// materializePhase2Draft is the PRODUCTION caller of MaterializeActivityPlan — the
// render-on-read `the-method-activity-list` mandates: "the server applies the deltas onto
// the derived baseline (DerivePlan) and stages the result (StageArtifactForReview)". For
// KindActivityList and KindNetwork it REPLACES the model the drafting agent committed on
// the session branch with the plan derived from the COMMITTED System (slot 5); every other
// kind passes through byte-identical, so this is inert for the other fifteen slots.
//
// DerivePlan yields three things and all three are written: slot 9 takes the activities,
// slot 10 takes the dependencies and milestones (see materializeNetwork). Until
// 2026-09-12 the dependencies and milestones were discarded here, so slot 10 was whatever
// the drafting agent typed, kept honest only by the CI drift gate — a real first run
// would not have staged the derived network.
//
// Without it slot 9 is whatever the agent typed, and on a freshly drafted project that is
// `{"activities": null}` — after which assembleSdpReview dies with "option network has
// zero activities" and the construction pump has nothing to dispatch. The one project
// that DOES hold a materialized slot 9 (this repo's own) got there by a hand backfill;
// this is that backfill made mechanical.
//
// DELTAS ARE EMPTY, AND THAT IS A KNOWN CONTRACT GAP, not an oversight. The wire model for
// this slot is projectstate.ActivityList — `activities` and nothing else — so the authored
// ActivityListDeltas vocabulary (overrides / additive / additiveMilestones) has no way to
// reach here: an agent that commits a deltas document has its unknown fields dropped by
// the codec and lands the empty list above. The authored deltas live in the root-level
// `activityListOverrides` sibling, which projectDoc does not carry, so the Manager cannot
// read them either (2026-08-10 spec amendment §1). Deriving deltas by DIFFING the drafted
// list against the baseline is deliberately NOT done — that would silently re-bless the
// hand-typed materialized list the doctrine forbids and re-open the zombie-activity door
// the derivation closed. Closing the gap needs a contract change to the slot's model.
func materializePhase2Draft(
	proj projectstate.Project,
	kind projectstate.ArtifactKind,
	draft projectstate.ArtifactModel,
) (projectstate.ArtifactModel, error) {
	if kind != projectstate.KindActivityList && kind != projectstate.KindNetwork {
		return draft, nil
	}

	sysSlot := pdSlotFor(proj, projectstate.KindSystem)
	if sysSlot.Status != projectstate.ReviewCommitted || sysSlot.Model == nil {
		return nil, fwmanager.New(fwmanager.FailedPrecondition,
			"cannot materialize the Phase-2 plan: the systemDesign slot is not committed — the whole Phase-2 plan derives from it, so Phase 1 must be approved before a plan is staged")
	}
	sys, ok := sysSlot.Model.(*projectstate.System)
	if !ok {
		return nil, wrongModelType(projectstate.KindSystem, sysSlot.Model)
	}

	list, deps, milestones, err := MaterializeActivityPlan(*sys, estimation.ActivityListDeltas{})
	if err != nil {
		return nil, fwmanager.Wrap(fwmanager.FailedPrecondition, err,
			"cannot materialize the Phase-2 plan from the committed systemDesign")
	}
	// LOUD, never a silent empty list: a System with no components derives no activities
	// from the architecture at all. Staging that would reproduce exactly the zero-activity
	// failure this seam exists to prevent, one phase later and with no trace of where it
	// came from.
	//
	// The condition is the COMPONENT count, not len(list.Activities): the derivation emits
	// the design prefix (requirements/architecture/projectDesign) for every system
	// including the empty one — a project before its architecture is committed still owes
	// the work that produces it — so the activity list is never empty and the old form of
	// this guard would now be dead code. It is the same reachable rule stated where it is
	// still true: the empty System was always the only input that produced an empty list
	// (a fully suppressed architecture still derives N-STP and N-IT).
	if len(sys.Components) == 0 {
		return nil, fwmanager.New(fwmanager.FailedPrecondition,
			"the committed systemDesign holds ZERO components, so it derives ZERO construction activities — fix slot 5 before staging a plan")
	}
	if kind == projectstate.KindActivityList {
		return &list, nil
	}

	authored, ok := draft.(*projectstate.Network)
	if !ok {
		return nil, wrongModelType(projectstate.KindNetwork, draft)
	}
	var authoredNet projectstate.Network
	if authored != nil {
		authoredNet = *authored
	}
	net, err := materializeNetwork(list, deps, milestones, authoredNet)
	if err != nil {
		return nil, err
	}
	return &net, nil
}

// materializeNetwork builds slot 10 from a derived plan. Dependencies and the milestone
// SET with its fan-in are the derivation's, verbatim — a milestone the draft authored but
// the derivation does not produce is dropped, and M0 keeps the [projectDesign] fan-in the
// derivation gives it (the SDP review is what that activity ends with), never whatever
// the draft typed.
// Each milestone's Name and Public are carried across from the authored network by id:
// they are display decorations with no derivation source (see toProjectStateMilestones).
// criticalPath is recomputed by ComputeNetwork over the derived graph and written as the
// alphabetically-sorted zero-float activity set (projectstate.Network.CriticalPath).
//
// A derived milestone with no authored decoration matching its id — the drafting agent
// omitted it, or typo'd the id — has no Name to carry across. NetworkMilestone.Name has no
// non-emptiness check anywhere else, and the founder ruled (2026-08-13) that non-emptiness
// is enforced in Go code, not by a schema minLength: so this is refused LOUDLY, naming the
// anonymous milestone, rather than silently committing it with an empty Name.
//
// It is the one function both the co-author staging seam and the drift gate
// (TestDerivedPlanMatchesCommittedState) run, so what a real first run stages and what CI
// holds slot 10 to can never disagree.
func materializeNetwork(
	list projectstate.ActivityList,
	deps []projectstate.NetworkDependency,
	milestones []projectstate.NetworkMilestone,
	authored projectstate.Network,
) (projectstate.Network, error) {
	decorations := make(map[string]projectstate.NetworkMilestone, len(authored.Milestones))
	for _, m := range authored.Milestones {
		decorations[m.ID] = m
	}
	outMilestones := make([]projectstate.NetworkMilestone, 0, len(milestones))
	for _, m := range milestones {
		a := decorations[m.ID]
		// The guard and the stored value read the SAME trimmed name: a guard that
		// refuses "   " but then stores "  Engines Complete " would commit the padding
		// it just judged meaningless.
		name := strings.TrimSpace(a.Name)
		if name == "" {
			return projectstate.Network{}, newError(fwmanager.ContractMisuse,
				fmt.Sprintf("derived milestone %q has no authored Name — the draft must author a Name (and Public) decoration for this milestone id", m.ID))
		}
		outMilestones = append(outMilestones, projectstate.NetworkMilestone{
			ID: m.ID, Name: name, Public: a.Public, DependsOn: m.DependsOn,
		})
	}

	cp, err := derivedCriticalPath(list, deps, milestones)
	if err != nil {
		return projectstate.Network{}, err
	}
	return projectstate.Network{Dependencies: deps, CriticalPath: cp, Milestones: outMilestones}, nil
}

// derivedCriticalPath solves the derived network with the estimation Engine's
// ComputeNetwork and returns the sorted set of activities it places on the critical path.
// Milestones are zero-duration nodes in the same solve but are not activities, so they
// never appear in the result (ComputeNetwork reports them separately).
func derivedCriticalPath(
	list projectstate.ActivityList,
	deps []projectstate.NetworkDependency,
	milestones []projectstate.NetworkMilestone,
) ([]string, error) {
	acts := estimation.ActivityList{Activities: make([]estimation.ActivityItem, 0, len(list.Activities))}
	for _, a := range list.Activities {
		acts.Activities = append(acts.Activities, estimation.ActivityItem{Name: a.Name, EffortDays: a.EffortDays})
	}
	nw := estimation.Network{Dependencies: make([]estimation.NetworkDependency, 0, len(deps))}
	for _, d := range deps {
		nw.Dependencies = append(nw.Dependencies, estimation.NetworkDependency{Activity: d.Activity, DependsOn: d.DependsOn})
	}
	for _, m := range milestones {
		nw.Milestones = append(nw.Milestones, estimation.NetworkMilestone{Id: m.ID, DependsOn: m.DependsOn})
	}

	sol, err := estimation.NewEstimationEngine().ComputeNetwork(fweng.Context{}, acts, nw)
	if err != nil {
		return nil, fwmanager.Wrap(fwmanager.FailedPrecondition, err,
			"cannot compute the critical path of the derived network")
	}
	cp := make([]string, 0, len(sol.Nodes))
	for id, n := range sol.Nodes {
		if n.OnCriticalPath {
			cp = append(cp, id)
		}
	}
	sort.Strings(cp)
	return cp, nil
}

// ===========================================================================
// DETERMINISTIC PROJECT DESIGN (stage 4b1 Task 9, spec §6/R7). Everything below
// is PURE — no workflow.Context, no clock, no RNG, no map iteration that reaches
// an output — so the generic child's compute strategy can run it in-workflow and a
// unit test can run it without Temporal. Its workflow half (sdpComputeStrategy,
// computeProjectPlan) is in deliveryactivity.go, where the file-layout gate puts
// anything holding a workflow.Context.
//
// WHAT IT REPLACES: four agent-drafted Solution slots, an agent-drafted risk model
// and an agent-assembled SDP review. Slots 11-16 have carried `revisions: 1` and
// `staleBasis: true` since the day they were written, because there was never
// anything for an agent to judge in them — the options are The Method's doctrine
// and the risk is what the estimation Engine already returns.
// ===========================================================================

// sdpEngines is the three estimate Engines the Project-Design compute calls, held as
// ONE value so they travel together through the strategy registry (architect ruling
// R9-5: a strategy's engines are THREADED, never reached for). They are called
// DIRECTLY in-workflow — deterministic, by value, no I/O — exactly as
// AssembleSDPReviewWorkflow has always called them, so they are not Activities.
type sdpEngines struct {
	Estimation   estimation.EstimationEngine
	OperationEst operationestimation.OperationEstimationEngine
	Settlement   billing.BillingEngine
}

// wired reports whether all three Engines are present. A compute with a missing Engine
// must refuse LOUDLY rather than nil-panic inside a workflow task, which retries forever.
func (e sdpEngines) wired() bool {
	return e.Estimation != nil && e.OperationEst != nil && e.Settlement != nil
}

// solutionDials is everything a Solution slot contributes to an assembled option.
// MEASURED, not assumed: assembleOption reads sol.StaffingCap, sol.BufferDays and
// sol.CriticalSpeedup and NOTHING ELSE — its ClassRates come from deriveClassRates(pa,
// classes) and the per-option CalendarDaysPerWeek "cheat" (compressed silently
// switching 2 -> 5 d/wk) was retired by the Phase-2 rework's F5. So the four
// agent-drafted solution slots were four sets of three numbers.
type solutionDials struct {
	StaffingCap     int
	BufferDays      float64
	CriticalSpeedup float64
}

// subcriticalStaffingCut / compressedCriticalSpeedup / decompressedBufferDays are the
// three numbers with a judgement in them.
//
// subcriticalStaffingCut is two fewer agents than normal, matching this repo's committed
// 6 -> 4. A cap below 1 is clamped, because a subcritical option with no staff has no
// duration to be costlier than.
const (
	subcriticalStaffingCut    = 2
	compressedCriticalSpeedup = 1.8
	decompressedBufferDays    = 20.0
)

// derivedSolutionDials is The Method's four options as doctrine, not as drafts.
//
//	normal        — minimum staffing for unimpeded critical-path progress (ch. 12).
//	subcritical   — deliberately understaffed: LONGER, COSTLIER and RISKIER than
//	                normal, whose whole purpose is disproving "fewer people = cheaper".
//	compressed    — same staffing, critical path sped up; the >30% exclusion guard in
//	                recommendOption is what keeps it out of the death zone.
//	decompressed  — normal, deliberately extended, to drop criticality risk toward the
//	                tipping point without consuming the float by cutting staff.
//
// TWO PARAMETERS, because every dial set is relative to the NORMAL option's cap: a
// subcritical option is not "four agents", it is "two fewer than whatever normal needs".
//
// The values reproduce this repo's committed slots 11-14 EXACTLY (cap 6/4/6/6, buffer
// 0/0/0/20, speedup 1/1/1.8/1), which is the acceptance: the compute must not change the
// plan on the state it is first run against, or its first output would be
// indistinguishable from a regression.
func derivedSolutionDials(kind projectstate.ArtifactKind, staffingCap int) (solutionDials, bool) {
	switch kind {
	case projectstate.KindNormalSolution:
		return solutionDials{StaffingCap: staffingCap, BufferDays: 0, CriticalSpeedup: 1}, true
	case projectstate.KindSubcriticalSolution:
		cut := max(staffingCap-subcriticalStaffingCut, 1)
		return solutionDials{StaffingCap: cut, BufferDays: 0, CriticalSpeedup: 1}, true
	case projectstate.KindCompressedSolution:
		return solutionDials{StaffingCap: staffingCap, BufferDays: 0, CriticalSpeedup: compressedCriticalSpeedup}, true
	case projectstate.KindDecompressedSolution:
		return solutionDials{StaffingCap: staffingCap, BufferDays: decompressedBufferDays, CriticalSpeedup: 1}, true
	case projectstate.KindMission, projectstate.KindGlossary, projectstate.KindScrubbedRequirements,
		projectstate.KindVolatilities, projectstate.KindCoreUseCases, projectstate.KindSystem,
		projectstate.KindOperationalConcepts, projectstate.KindStandardCheck,
		projectstate.KindPlanningAssumptions, projectstate.KindActivityList, projectstate.KindNetwork,
		projectstate.KindRiskModel, projectstate.KindSdpReview:
		// Not a solution slot. Named exhaustively rather than defaulted (the designSlotForKind
		// precedent) so a new ArtifactKind fails the gate here and its author has to decide
		// whether The Method gives it a dial set.
		return solutionDials{}, false
	default:
		return solutionDials{}, false
	}
}

// derivedSolution renders one Solution slot from its dials and the DERIVED class rates.
//
// ClassRates IS EMITTED, and the first draft of this function was wrong to drop it. The
// argument for dropping it was that assembleOption reads none of it — the per-day rate of a
// worker class comes from deriveClassRates over the planning assumptions' rate card — so
// re-emitting the committed slots' authored map would make the derivation depend on a stale
// field. That half is right. What it missed is a READER outside the compute:
// webApp/src/components/project/SolutionView.tsx renders a "BUILD-COST RATES" block from it,
// with an AuthoredBadge and a per-rate comment anchor (solutionAnchor(kind,
// 'classRates.<class>')), fed by projectAdapters.ts. Dropping the field would have made all
// four Solution views read "No class rates specified." the moment M0 approved, and taken
// their comment anchors with them — review aids are first-class, and losing one silently is
// exactly what that house rule forbids.
//
// So the resolution is DERIVE IT IN PLACE rather than choose between a stale map and none:
// the rates come from the SAME deriveClassRates the option assembly uses, over the resolved
// planning assumptions and the activity list's own worker classes. The screen keeps
// rendering, the numbers are derived, and nothing depends on an authored field.
func derivedSolution(
	kind projectstate.ArtifactKind, dials solutionDials, classRates map[string]projectstate.Money,
) *projectstate.Solution {
	return &projectstate.Solution{
		SlotKind:    kind,
		ClassRates:  classRates,
		StaffingCap: dials.StaffingCap,
		// Zero, matching every committed slot: the calendar is a SHARED planning assumption
		// for every option since F5 retired the per-option cheat.
		CalendarDaysPerWeek: 0,
		BufferDays:          dials.BufferDays,
		CriticalSpeedup:     dials.CriticalSpeedup,
	}
}

// normalStaffingCap is the cap the whole dial table is relative to: the committed normal
// solution's, when there is one, else derivedNormalStaffingCap.
//
// It READS the committed slot rather than deriving a cap from the network, because "the
// minimum staffing that keeps the critical path unimpeded" is a founder fact about how
// many agent streams a human can supervise (this repo's own planning notes cap it at 3
// in flight), not something the graph answers. A project with no committed normal
// solution gets the documented default and the caller records that it defaulted.
func normalStaffingCap(proj projectstate.Project) (int, bool) {
	sol, err := committedSolution(proj, projectstate.KindNormalSolution)
	if err != nil || sol.StaffingCap < 1 {
		return derivedNormalStaffingCap, false
	}
	return sol.StaffingCap, true
}

// derivedNormalStaffingCap is the normal option's cap for a project that has never had
// one. SIX, which is this repo's own committed value and the number the dial table's
// acceptance is stated against; it is a starting point the founder edits, not a claim
// about their team.
const derivedNormalStaffingCap = 6

// riskModelFrom joins the per-option risk the estimation Engine already computed into the
// RiskModel slot, with the App-C exclusion zones the SDP review already applies.
//
// There was never anything for an agent to judge here: EstimateForOption returns
// criticality risk, activity risk and their composite per option, and sdpOptionInBand
// already decides inclusion from the same numbers. This is the join, and it uses the SAME
// thresholds recommendOption uses, so the committed risk model and the committed
// recommendation can no longer disagree.
//
// THREE PARAMETERS, and the third is an ArtifactKind rather than an OptionID on purpose:
// RiskModel.Recommendation is an ArtifactKind — the same vocabulary Rows[].SolutionKind
// uses — while SdpReview.Recommendation is an OptionID. The two slots name the chosen
// option in two vocabularies and the join must not mix them, so the CALLER maps
// recommendOption's OptionID onto the winning row's SolutionKind before calling here.
func riskModelFrom(
	rows []projectstate.SdpOptionRow,
	risks map[projectstate.ArtifactKind]estimation.RiskScore,
	recommendation projectstate.ArtifactKind,
) projectstate.RiskModel {
	normalDur := 0.0
	for _, r := range rows {
		if r.SolutionKind == projectstate.KindNormalSolution {
			normalDur = r.DurationDays
		}
	}
	out := projectstate.RiskModel{
		Rows:              make([]projectstate.RiskRow, 0, len(rows)),
		TooRiskyThreshold: riskTooRisky,
		OverSafeThreshold: riskOverSafe,
		MaxCompressionPct: maxCompression,
		Recommendation:    recommendation,
	}
	for _, r := range rows {
		score := risks[r.SolutionKind]
		included := sdpOptionInBand(r, normalDur)
		out.Rows = append(out.Rows, projectstate.RiskRow{
			SolutionKind:    r.SolutionKind,
			CriticalityRisk: score.CriticalityRisk,
			ActivityRisk:    score.ActivityRisk,
			Composite:       score.Composite,
			DurationDays:    r.DurationDays,
			TotalCost:       r.BuildCost,
			Included:        included,
			ExclusionReason: sdpExclusionReason(r, normalDur, included),
		})
	}
	return out
}

// sdpExclusionReason is the sentence an EXCLUDED row carries, in the same words the
// committed slot holds. An included row carries none: a reason beside an included option
// is the reader's first wrong assumption.
func sdpExclusionReason(r projectstate.SdpOptionRow, normalDur float64, included bool) string {
	switch {
	case included:
		return ""
	case r.CompositeRisk > riskTooRisky:
		return fmt.Sprintf("composite risk %.3f exceeds the %g ceiling", r.CompositeRisk, riskTooRisky)
	case r.CompositeRisk < riskOverSafe:
		return fmt.Sprintf("composite risk %.3f is below the %g floor — the option is over-safe", r.CompositeRisk, riskOverSafe)
	case normalDur > 0 && r.DurationDays < normalDur:
		return fmt.Sprintf("compressed %.0f%% below the normal option, past the %.0f%% death-zone bound",
			100*(normalDur-r.DurationDays)/normalDur, 100*maxCompression)
	}
	return "outside the App C exclusion zones"
}

// The assumption FAMILIES defaultPlanningAssumptions can fill, named as the M0 screen
// will read them back. They are the strings the attempt's Detail carries, so they are
// customer-facing prose and not identifiers.
const (
	assumedResources  = "the roster of roles"
	assumedCalendar   = "the working calendar"
	assumedRates      = "the agent rate card"
	assumedIndirect   = "the indirect daily rate"
	assumedUsage      = "the declared load"
	assumedTerms      = "the billing terms"
	assumedInfra      = "the infrastructure kind"
	assumedStaffing   = "the normal option's staffing cap"
	assumedEverything = "every planning assumption"
)

// defaultPlanningAssumptions is what the compute assumes when slot 8 is uncommitted.
// R-E's controller override, 2026-09-26: an absent slot 8 DEFAULTS and the compute
// PROCEEDS — it must not raise SDPInputsIncomplete. A project that cannot reach its own
// cost-approval gate cannot be told what it would cost, and refusing at M0 refuses the
// one screen that exists to ask the question. (The historical refusal was correct while
// slot 8 had an agent to draft it; stage 4b1 deletes that rail.)
//
// Every number has a source, and none of them is invented here:
//
//	Resources            the WORKER CLASSES the derived plan actually uses, read off the
//	                     activity list — Löwy ch. 7's staffing question answered by the
//	                     plan rather than guessed ahead of it. Never a head count: the
//	                     option's StaffingCap is the cap, and this is the roster of ROLES
//	                     the network needs. Sorted, so the output is replay-stable.
//	CalendarDaysPerWeek  5 — the book's nominal working week (App. A's day is a working
//	                     day). This repo's own committed value is 2, which is a FOUNDER
//	                     fact about a solo founder's availability and precisely the kind
//	                     of thing a default must not pretend to know. The default applies
//	                     only when the slot is ABSENT; it never overrides present data.
//	IndirectDailyRate    defaultIndirectDailyRate ($50/day, assemblesdpreview.go's F6
//	                     constant) — the overhead that accrues per calendar day
//	                     regardless of which agents are active, and what makes a
//	                     subcritical option demonstrably costlier.
//	RateCard             defaultRateSpec per class — the SAME helper deriveClassRates
//	                     already falls back to for a class the authored card omitted. So
//	                     the whole-slot default is the per-class rule applied to every
//	                     class, not a second rule that can drift from it.
//	InfrastructureKind   the platform's ONE registered infrastructure. MEASURED, and the
//	                     brief's "else the zero value" is wrong twice over:
//	                     DeploymentOperationsModel carries NO InfrastructureKind member to
//	                     read (it holds a ScenarioKind and infra building blocks), and the
//	                     zero value is InfrastructureKindUnknown, which
//	                     operationEstimationEngine.costModelFor REFUSES outright ("the
//	                     Engine never falls back to a default Strategy"). Defaulting to
//	                     the zero value would therefore fail the very compute R-E exists
//	                     to let through. GoTemporalPostgres is not a choice among
//	                     alternatives — it is the only kind with a cost model — so
//	                     assuming it assumes nothing a founder could have decided
//	                     differently today.
//	DeclaredUsage        one user, one request a minute, a 4 KiB payload: the smallest
//	                     load that is not zero, because a zero-load option has no
//	                     operating cost to compare and the operating half of the M0
//	                     headline would read $0.
//	Terms                RevenueShare 0 / ComputeCost tieredFloors / Schedule monthly —
//	                     the platform's shipped billing posture (docs/billing-setup.md;
//	                     the 2026-06-09 MoR reversal), not a per-project choice.
//
// It returns the ASSUMPTIONS and the family names it filled, so the M0 screen can say
// "cost computed on an assumed calendar and rate card" rather than presenting an
// assumption as a decision. Rendering that sentence is Task 14's; RECORDING it is this
// task's, on the attempt.
//
// It takes ONLY the activity list. The brief also passes the whole project, for
// InfrastructureKind — measured, that read does not exist (see the InfrastructureKind note
// above), and a parameter no body reads is exactly the lie proposeReviewSet's doc calls out
// about its retired architectureGraph argument: one the compiler cannot catch.
func defaultPlanningAssumptions(al projectstate.ActivityList) (projectstate.PlanningAssumptions, []string) {
	// ONE roster derivation, shared with resolveCostFamilies' per-family fills. Two copies of
	// "which worker classes does this plan use" is two answers to keep in step, and the
	// whole-slot default and the per-family default must never disagree about the roster.
	roles := workerClassesOf(al)
	card := make(map[string]projectstate.WorkerRateSpec, len(roles))
	for _, c := range roles {
		card[c] = defaultRateSpec(c)
	}
	pa := projectstate.PlanningAssumptions{
		Resources:           roles,
		CalendarDaysPerWeek: defaultCalendarDaysPerWeek,
		InfrastructureKind:  defaultInfrastructureKind,
		DeclaredUsage:       defaultDeclaredUsage(),
		Terms:               defaultSettlementTerms(),
		Notes:               defaultPlanningAssumptionsNote,
		IndirectDailyRate:   defaultIndirectDailyRate,
		RateCard:            card,
	}
	return pa, []string{
		assumedResources, assumedCalendar, assumedRates, assumedIndirect,
		assumedUsage, assumedTerms, assumedInfra,
	}
}

// defaultDeclaredUsage is one user, one request a minute, a 4 KiB payload.
func defaultDeclaredUsage() projectstate.UsageAssumption {
	return projectstate.UsageAssumption{
		ExpectedDailyActiveUsers: 1,
		RequestsPerMinute:        1,
		AvgPayloadBytes:          4096,
	}
}

// defaultSettlementTerms is the platform's shipped billing posture: NO revenue share, a
// tiered-floors compute cost, billed monthly (docs/billing-setup.md; the 2026-06-09
// merchant-of-record reversal).
//
// RevenueShareNegotiatedRate at ZERO PERCENT, and this needs its reason stated because the
// obvious encoding is wrong: "no revenue share" has NO member of its own in the
// RevenueShareKind vocabulary — the zero value is RevenueShareUnknown, and billingEngine's
// money-safety guard REFUSES it outright ("settling real money under an unregistered
// revenue-share regime is a financial-correctness hazard… the Engine NEVER silently falls
// back"). That guard is right, and it is why this cannot be the zero value. A negotiated
// rate of 0% is the vocabulary's only truthful way to say "a share was agreed and it is
// nothing"; the projection echoes 0% either way.
//
// EARMARKED: the vocabulary wants a RevenueShareNone member, which is a project.json edit
// plus codegen. Until it exists this is the encoding, and it is written down HERE rather
// than guessed at each call site.
func defaultSettlementTerms() projectstate.SettlementTerms {
	return projectstate.SettlementTerms{
		RevenueShare:        projectstate.RevenueShareNegotiatedRate,
		RevenueSharePercent: 0,
		ComputeCost:         projectstate.ComputeCostTieredFloors,
		Schedule:            projectstate.ScheduleMonthly,
	}
}

// resolvePlanningAssumptions is what the compute actually reads: the COMMITTED slot 8 where
// it holds a usable value, and The Method's default for each family where it does not.
//
// PER FAMILY, not all-or-nothing, and that distinction is the whole of R-E read carefully.
// "Defaults when absent" is not "defaults when the slot is missing": a field whose value is
// its vocabulary's UNKNOWN member is absent in the only sense that matters, because no
// Engine can price it. MEASURED on this repo's own state, which is why this function exists
// at all: slot 8 is committed and its `terms.revenueShare` is 0 — RevenueShareUnknown — so
// billingEngine refuses every option and the SDP assembly cannot run at all. That is why
// slots 11-16 have carried staleBasis since the billing reversal: nothing could re-derive
// them. Refusing at M0 over a field that is zero BY DESIGN is exactly the failure R-E
// removes.
//
// It NEVER replaces a NAMED value. A committed calendar of two days a week stays two days a
// week; a committed FlatMarkup regime stays FlatMarkup. Only the unknown members and the
// uncommitted slot are filled, and every fill is recorded.
func resolvePlanningAssumptions(
	proj projectstate.Project,
	al projectstate.ActivityList,
) (projectstate.PlanningAssumptions, []string) {
	pa, err := committedPlanningAssumptions(proj)
	if err != nil {
		return defaultPlanningAssumptions(al)
	}
	var defaulted []string
	if pa.Terms.RevenueShare == projectstate.RevenueShareUnknown || pa.Terms.ComputeCost == projectstate.ComputeCostUnknown {
		// The percents ride with the regime: a percent kept from an unregistered regime would
		// be a number with no rule behind it.
		pa.Terms = defaultSettlementTerms()
		defaulted = append(defaulted, assumedTerms)
	}
	if pa.InfrastructureKind == projectstate.InfrastructureKindUnknown {
		pa.InfrastructureKind = defaultInfrastructureKind
		defaulted = append(defaulted, assumedInfra)
	}
	if pa.CalendarDaysPerWeek <= 0 {
		pa.CalendarDaysPerWeek = defaultCalendarDaysPerWeek
		defaulted = append(defaulted, assumedCalendar)
	}
	// PER FIELD, not per family, and the `&&` this replaces was a real hole: a committed slot
	// naming requestsPerMinute: 5 and expectedDailyActiveUsers: 0 kept a ZERO-USER load —
	// neither defaulted nor recorded — so the operating cost was computed against no users at
	// all and the M0 screen said nothing had been assumed. Each of the three numbers is its own
	// answer, so each is filled and recorded on its own.
	defaults := defaultDeclaredUsage()
	if pa.DeclaredUsage.ExpectedDailyActiveUsers <= 0 {
		pa.DeclaredUsage.ExpectedDailyActiveUsers = defaults.ExpectedDailyActiveUsers
		defaulted = append(defaulted, assumedUsage)
	}
	if pa.DeclaredUsage.RequestsPerMinute <= 0 {
		pa.DeclaredUsage.RequestsPerMinute = defaults.RequestsPerMinute
		defaulted = appendOnce(defaulted, assumedUsage)
	}
	if pa.DeclaredUsage.AvgPayloadBytes <= 0 {
		pa.DeclaredUsage.AvgPayloadBytes = defaults.AvgPayloadBytes
		defaulted = appendOnce(defaulted, assumedUsage)
	}
	return resolveCostFamilies(pa, al, defaulted)
}

// resolveCostFamilies is the other three families resolvePlanningAssumptions owes, split out
// so each function stays one readable list of rules (gocyclo).
//
// THE RATE CARD, THE INDIRECT RATE AND THE RESOURCES were missed by the first pass, and each
// silently priced the plan wrong rather than refusing: an EMPTY rate card makes
// deriveClassRates fall back to defaultRateSpec per class WITHOUT the fill being recorded, so
// the M0 screen claims the founder's own rates; a ZERO IndirectDailyRate makes indirectDailyRateOf
// substitute its own default, again unrecorded; and an empty Resources list is the roster every
// option's staffing is read against. The rule is the same one the four families above follow —
// fill what the vocabulary cannot price, record every fill, never replace a named value.
func resolveCostFamilies(
	pa projectstate.PlanningAssumptions, al projectstate.ActivityList, defaulted []string,
) (projectstate.PlanningAssumptions, []string) {
	roles := workerClassesOf(al)
	if len(pa.Resources) == 0 {
		pa.Resources = roles
		defaulted = append(defaulted, assumedResources)
	}
	if len(pa.RateCard) == 0 {
		card := make(map[string]projectstate.WorkerRateSpec, len(roles))
		for _, c := range roles {
			card[c] = defaultRateSpec(c)
		}
		pa.RateCard = card
		defaulted = append(defaulted, assumedRates)
	}
	if pa.IndirectDailyRate.MinorUnits <= 0 {
		pa.IndirectDailyRate = defaultIndirectDailyRate
		defaulted = append(defaulted, assumedIndirect)
	}
	return pa, defaulted
}

// appendOnce keeps the defaulted list a SET of families: three zero usage numbers are one
// assumption to a reader, and the M0 copy line would otherwise name it three times.
func appendOnce(list []string, family string) []string {
	if slices.Contains(list, family) {
		return list
	}
	return append(list, family)
}

// workerClassesOf is the activity list's worker classes, unique and SORTED. Sorted because it
// seeds both the default Resources roster and the default rate card, and a map walk there
// would put a different order in the committed document on every run.
func workerClassesOf(al projectstate.ActivityList) []string {
	seen := map[string]struct{}{}
	for _, a := range al.Activities {
		if a.WorkerClass != "" {
			seen[a.WorkerClass] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// defaultCalendarDaysPerWeek is App. A's nominal working week. It is FIVE and not this
// repo's committed two: two is a founder fact, and a default that guessed it would tell
// every other project's founder their own availability.
const defaultCalendarDaysPerWeek = 5.0

// defaultPlanningAssumptionsNote is what the slot itself says about where it came from,
// so a reader of the committed document is never left guessing which numbers a human
// chose.
const defaultPlanningAssumptionsNote = "Derived defaults: no planning assumptions were authored, so the platform assumed " +
	"The Method's nominal 5-day working week, the default agent rate card and indirect daily rate, " +
	"the smallest non-zero declared load, and the platform's shipped billing posture. " +
	"Edit this slot to replace any of them with a fact about your own team."

// defaultInfrastructureKind is the platform's ONE registered infrastructure. See
// defaultPlanningAssumptions' InfrastructureKind note: the zero value is a refusal, not a
// default, so this is the only value a default can hold.
const defaultInfrastructureKind = projectstate.InfrastructureKindGoTemporalPostgres

// defaultedDetail is the sentence the attempt records when the compute had to assume
// something, and the EMPTY STRING when it did not. An empty Detail is the honest answer
// for a project whose founder authored their assumptions: a note saying "nothing was
// assumed" is noise on every well-formed project.
func defaultedDetail(defaulted []string) string {
	if len(defaulted) == 0 {
		return ""
	}
	return "the plan's cost was computed on ASSUMED values for " + strings.Join(defaulted, ", ") +
		" — the founder authored none, so these are the platform's documented defaults and not decisions"
}

// computedSlot is one slot the Project-Design compute produced, in the order it is
// staged. A SLICE and not a map: staging order is a durable command sequence, and a map
// walk there would be non-determinism.
type computedSlot struct {
	Kind  projectstate.ArtifactKind
	Model projectstate.ArtifactModel
}

// projectDesignComputedKinds is the EIGHT slots the compute writes, in staging order. It
// is the one list the compute stages from and the M0 gate commits, so the two cannot
// disagree about what Project Design produced.
//
// Slot 8 (planningAssumptions) is deliberately NOT here: it is the one authored input the
// compute READS. It carries the founder's resources, calendar, infrastructure kind,
// declared usage, settlement terms and rate card — business input no engine can derive.
func projectDesignComputedKinds() []projectstate.ArtifactKind {
	out := []projectstate.ArtifactKind{projectstate.KindActivityList, projectstate.KindNetwork}
	out = append(out, projectstate.SolutionKinds()...)
	return append(out, projectstate.KindRiskModel, projectstate.KindSdpReview)
}

// computeProjectPlanSlots is the WHOLE deterministic Project Design, as a pure function
// of the committed project state.
//
//	slots 9,10   activityList + network — MaterializeActivityPlan, the SAME derivation
//	             `make derived-plan-write` drives and `derived-plan-check` gates, so the
//	             committed plan and the child's plan cannot diverge.
//	slots 11-14  the four Solution dial-sets — derivedSolutionDials.
//	slot 15      riskModel — riskModelFrom, over the risk EstimateForOption already
//	             returns per option.
//	slot 16      sdpReview — assembleSdpReviewOver, unchanged and kept whole.
//
// The ONE precondition that cannot be defaulted is the ARCHITECTURE: a plan derived from
// an uncommitted slot 5 would be a plan for nothing, so that stays a FailedPrecondition
// naming it (materializePhase2Draft raises it).
//
// It returns the slots AND the assumption families it had to default, which the attempt's
// Detail records.
func computeProjectPlanSlots(
	proj projectstate.Project,
	eng sdpEngines,
) ([]computedSlot, []string, error) {
	if !eng.wired() {
		return nil, nil, newError(fwmanager.FailedPrecondition,
			"the Project-Design compute has no estimate Engines wired; nothing can price the plan")
	}
	listModel, lErr := materializePhase2Draft(proj, projectstate.KindActivityList, nil)
	if lErr != nil {
		return nil, nil, lErr
	}
	list, ok := listModel.(*projectstate.ActivityList)
	if !ok {
		return nil, nil, wrongModelType(projectstate.KindActivityList, listModel)
	}
	pa, defaulted := resolvePlanningAssumptions(proj, *list)
	netModel, nErr := materializePhase2Draft(proj, projectstate.KindNetwork, committedNetworkDecorations(proj))
	if nErr != nil {
		return nil, nil, nErr
	}
	net, ok := netModel.(*projectstate.Network)
	if !ok {
		return nil, nil, wrongModelType(projectstate.KindNetwork, netModel)
	}

	cap0, capAuthored := normalStaffingCap(proj)
	if !capAuthored {
		defaulted = append(defaulted, assumedStaffing)
	}
	// The per-class $/day rates every derived Solution slot carries, from the SAME derivation
	// the option assembly uses — one answer, two consumers (the Engine's cost math and the
	// SPA's BUILD-COST RATES block).
	classRates := deriveClassRates(pa, workerClassesOf(*list))
	solutions := make(map[projectstate.ArtifactKind]*projectstate.Solution, len(projectstate.SolutionKinds()))
	for _, kind := range projectstate.SolutionKinds() {
		dials, known := derivedSolutionDials(kind, cap0)
		if !known {
			return nil, nil, newError(fwmanager.FailedPrecondition,
				"no Method dial set is derived for solution kind "+kind.String())
		}
		solutions[kind] = derivedSolution(kind, dials, classRates)
	}

	review, risks, rErr := assembleSdpReviewOver(eng, pa, *list, *net, solutions, "")
	if rErr != nil {
		return nil, nil, rErr
	}
	riskModel := riskModelFrom(review.Options, risks, solutionKindOfOption(review.Options, review.Recommendation))

	out := make([]computedSlot, 0, len(projectDesignComputedKinds()))
	out = append(out,
		computedSlot{Kind: projectstate.KindActivityList, Model: list},
		computedSlot{Kind: projectstate.KindNetwork, Model: net},
	)
	for _, kind := range projectstate.SolutionKinds() {
		out = append(out, computedSlot{Kind: kind, Model: solutions[kind]})
	}
	return append(out,
		computedSlot{Kind: projectstate.KindRiskModel, Model: &riskModel},
		computedSlot{Kind: projectstate.KindSdpReview, Model: review},
	), defaulted, nil
}

// committedNetworkDecorations hands materializePhase2Draft the AUTHORED network whose
// milestone Name/Public decorations it carries across. The committed slot 10 is that
// authored document: milestone names have no derivation source, so the re-derivation
// preserves the ones already committed rather than refusing every milestone as anonymous.
// A project with no committed slot 10 passes an empty network, and materializeNetwork's
// own loud refusal names the first anonymous milestone.
func committedNetworkDecorations(proj projectstate.Project) projectstate.ArtifactModel {
	net, err := committedNetwork(proj)
	if err != nil {
		return &projectstate.Network{}
	}
	return &net
}

// solutionKindOfOption maps the SdpReview's chosen OptionID onto the solution KIND the
// RiskModel names it by. The two slots speak two vocabularies (OptionID vs ArtifactKind)
// and this is the one place that crosses between them; an unknown id answers with the
// zero kind rather than guessing, so a mismatch shows up as an empty recommendation
// instead of the wrong option.
func solutionKindOfOption(rows []projectstate.SdpOptionRow, id projectstate.OptionID) projectstate.ArtifactKind {
	for _, r := range rows {
		if r.OptionID == id {
			return r.SolutionKind
		}
	}
	return projectstate.ArtifactKind(0)
}

// ---------------------------------------------------------------------------
// CONSTRUCTION RAIL — moved verbatim from internal/manager/construction/
// constructionmanager.go at stage 4a. Bodies are unchanged; only package-private
// names that collided with another rail were renamed (the collision table is in
// docs/superpowers/plans/2026-09-25-activity-experience-stage4a.md, Task 6 Step 2b,
// and in this commit's message). 4b replaces this block with the generic DAG child.
// ---------------------------------------------------------------------------

// constructionManager is the constructionManager façade — the concrete
// implementation of the GENERATED ConstructionManager interface (contract.gen.go). It
// exposes the five public use-case ops (constructionManager.md §2) and OWNS Temporal.
// The Temporal-backed ops:
//   - ExecuteNextActivity — Workflow (entry; scheduler-triggered pump)
//   - RunReplanSweep      — Workflow (entry; scheduler-triggered variance sweep)
//   - PauseProject        — Signal (operatorPauseRequested)
//   - OverrideActivity    — Signal (operatorOverride, to the per-activity child)
//   - GetSessionState     — Query (sessionState, read-only)
//
// The façade methods use only the Temporal client; the pre-condition checks
// (non-empty ids, non-empty reason, known OverrideKind) are enforced here before any
// Temporal call (§2/§3.5). It ALSO stores the PUBLISHED downstream deps the GENERATED
// constructor was given so RegisterWorker can fold them (adapters.go) into the
// hand-written Temporal csWorkflows. The former exported consumer-mirror interfaces +
// the composition-root adapters are RETIRED; the Manager depends on the deps'
// PUBLISHED interfaces and adapts them internally.
type constructionManager struct {
	client client.Client

	projectState           projectstate.ProjectStateAccess
	artifact               artifact.ArtifactAccess
	intervention           intervention.InterventionEngine
	review                 review.ReviewEngine
	pipeline               agenticjob.AgenticJobAccess
	rail                   sourcecontrol.SourceControlAccess
	constructionTransition projectstate.ConstructionTransitionAccess
	gitActivityStatus      projectstate.GitActivityStatusAccess
	escalationWaitTimeout  time.Duration
	interventionMode       string

	// sdpEngines are the three estimate Engines deterministic Project Design calls (stage
	// 4b1 Task 9). The construction half holds them because the GENERIC child runs on this
	// half's worker, and the child now walks the `projectDesign` lifecycle too — the
	// projectDesignManager's own copies stay where they are for as long as the retired SDP
	// assembly does. Threaded into the csWorkflows via wfDeps.SDPEngines (WorkerManifest).
	sdpEngines sdpEngines

	// repo (B5) is the per-project Repo resolver the gh-mode venue switch dispatches
	// through: projectID → the project's own RepoRef. nil, a miss, or the DESIGN rails'
	// GitLocal ref (isGitLocalVenue) ⇒ that project's construction dispatch falls back to
	// the configured central construction repo AND the PR-rail slice stays dormant
	// (constructRepoTarget / railLifecycleEnabled). A project repo retargets the dispatch
	// (aiarch-construct.yml in the project repo) AND activates the branch→PR rail.
	// Threaded into the csWorkflows via wfDeps.Repo (WorkerManifest).
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	// messageBus (7b) is the generated messageBus Utility dep — the restricted
	// Manager-only signal/schedule surface. Threaded into genActivities so the
	// csWorkflows can reach registerSchedule/deliverSignal through the generated
	// invokers. Task 7c (landed 2026-08-01) added this Manager's RegisterSchedules
	// (the pump tick and the replan sweep) plus the startup wiring — the
	// composition root now threads the real messageBus.MessageBus here, exactly as
	// it does for billing/operations (main.gen.go; CONSTRUCTION_DRYRUN gates only
	// which Schedules that shared bus actually registers, via hooks.go's
	// FinalizeMessageBus/dryRunConstructionScheduleGate).
	messageBus messagebus.MessageBus

	// designSession (B6) is the generated designSessionAccess dep. Since the B8
	// follow-up it is CONSUMED by the csWorkflows: the pump's whole-aggregate read rides
	// the generated designSessionAccess.readProjectOnBranch invoker with branch ""
	// (main) — the shared projectstate.ProjectEnvelope was extended with the
	// construction-fidelity sections (ActivityConstruction / ServiceContracts /
	// ReviewPolicy, envelope.go) that construction's former local codec carried, which
	// is what retired the last custom Activity (ReadProjectActivity).
	designSession projectstate.DesignSessionAccess

	// episodes (SP1 capture-seam) is the generated episodeAccess dep — the agentic-
	// episode ledger every terminal pipeline observation appends to. Reached ONLY
	// through the generated invoker surface (Acts.EpisodesAppendEpisode) inside the
	// csWorkflows; this field exists to thread it into genActivities.
	episodes episode.EpisodeAccess

	// activityExecution (stage 3) is the generated activityExecutionAccess dep — the
	// fifth facet of the one project-state component, owner of the per-activity attempt
	// and review-round ledgers. Taking the dep HERE is what registers its twelve
	// Temporal activities on this Manager's worker, which is the precondition for task
	// 5: the construction child workflow switches onto them behind workflow.GetVersion,
	// with the old branch still calling the deprecated-in-place facets whose activity
	// names the replay fixtures record. Nothing in this wave CALLS these verbs yet.
	activityExecution projectstate.ActivityExecutionAccess
}

// newConstructionManager is the hand-written, unexported builder the generated
// NewConstructionManager constructor delegates to. It wires the Temporal client + the
// published deps into the façade. The façade itself uses only the client; the deps are
// stored for RegisterWorker (worker.go), which folds them into the Temporal csWorkflows.
func newConstructionManager(
	c client.Client,
	projectState projectstate.ProjectStateAccess,
	art artifact.ArtifactAccess,
	interventionEng intervention.InterventionEngine,
	reviewEng review.ReviewEngine,
	pipeline agenticjob.AgenticJobAccess,
	rail sourcecontrol.SourceControlAccess,
	constructionTransition projectstate.ConstructionTransitionAccess,
	gitActivityStatus projectstate.GitActivityStatusAccess,
	designSession projectstate.DesignSessionAccess,
	activityExecution projectstate.ActivityExecutionAccess,
	messageBus messagebus.MessageBus,
	episodes episode.EpisodeAccess,
	escalationWaitTimeout time.Duration,
	interventionMode string,
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool),
	// eng are the three estimate Engines deterministic Project Design calls. ONE trailing
	// struct parameter rather than three positional interfaces, because this constructor
	// already takes sixteen and three more nils at seventeen call sites is a miscount
	// waiting to happen.
	eng sdpEngines,
) *constructionManager {
	return &constructionManager{
		client:                 c,
		projectState:           projectState,
		artifact:               art,
		intervention:           interventionEng,
		review:                 reviewEng,
		pipeline:               pipeline,
		rail:                   rail,
		constructionTransition: constructionTransition,
		gitActivityStatus:      gitActivityStatus,
		designSession:          designSession,
		activityExecution:      activityExecution,
		messageBus:             messageBus,
		episodes:               episodes,
		escalationWaitTimeout:  escalationWaitTimeout,
		interventionMode:       interventionMode,
		repo:                   repo,
		sdpEngines:             eng,
	}
}

// ExecuteNextActivity — op 2.1. Temporal Workflow (entry; client/MCP-driven).
// Starts — or JOINS — the project's ONE PumpNextActivityWorkflow on the construction
// queue, id {projectId}:nextActivity (pumpWorkflowID). The pump reads head-state, and
// on an eligible activity executes a per-activity child workflow
// {projectId}:{activityId}. No eligible activity ⇒ PumpResult{Dispatched:false} (a
// normal quiet tick).
//
// ONE PUMP PER PROJECT (architect pump ruling, 2026-09-12). The pump is the single
// writer walking the project's dependency frontier; two pumps racing the same frontier
// double-dispatch. Every entry — this façade (Begin / MCP) AND PumpSweepWorkflow's
// Schedule fan-out — derives the SAME id from pumpWorkflowID, so:
//   - WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING: a call while the pump runs (its
//     self-cascade chains ContinueAsNew under the same id) JOINS that run instead of
//     starting a second pump.
//   - WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE: once the previous pump CLOSED (drained
//     quiet, paused, or failed), the next call starts a fresh one. NOT
//     ALLOW_DUPLICATE_FAILED_ONLY — a quiet-completed pump must be restartable, or the
//     project could never be pumped again after its first drain.
//
// PAUSED PROJECTS (plan B1.7; founder ruling 2026-09-13, "Begin on a paused project
// refuses"): a recorded operator pause refuses this op with FailedPrecondition — "resume
// it to continue" — BEFORE any pump is started. The pause is an operator decision; a
// caller's Begin silently overriding it would be the same class of override the sweep
// exclusion exists to prevent. ResumeProject is the one way back: it clears the record
// and starts the pump. The pump this op starts no longer carries OperatorDriven, so it
// honours the recorded pause like every other pump (pump-honors-recorded-pause v2).
//
// tickID is a CORRELATION id only (logged here); it no longer shapes the workflow id,
// so it cannot fork a second pump. It stays a required, non-empty input (the contract
// shape is unchanged). SYNC: returns the pump's dispatch decision
// (PumpResult{Dispatched:true, ActivityID}, or {Dispatched:false} when quiescent) as
// soon as the pump run has decided — it does NOT block until the per-activity child (or
// the background self-cascade over the dependency frontier) drains. A caller that
// joined a running pump reads THAT run's decision. The decision is read off the pump
// via the queryPumpDispatch Query while the cascade continues durably in the
// background.
func (m *constructionManager) ExecuteNextActivity(rc fwmanager.Context, projectID ProjectID, tickID string) (PumpResult, error) {
	ctx := rc.Context
	if projectID == "" {
		return PumpResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if tickID == "" {
		return PumpResult{}, newError(fwmanager.ContractMisuse, "empty tickId")
	}

	if err := m.refuseWhilePaused(ctx, projectID); err != nil {
		return PumpResult{}, err
	}

	wfID := pumpWorkflowID(projectID)
	slog.Default().InfoContext(ctx, "construction pump: start-or-join",
		"projectId", string(projectID), "workflowId", wfID, "tickId", tickID)
	we, err := m.startOrJoinPump(ctx, projectID)
	if err != nil {
		return PumpResult{}, csMapStartError(err)
	}
	return m.awaitDispatchDecision(ctx, we, wfID)
}

// startOrJoinPump starts — or JOINS — the project's ONE pump (pumpWorkflowID) with the
// policy pair the one-pump ruling fixes: USE_EXISTING joins a running pump (its
// self-cascade included), ALLOW_DUPLICATE restarts one that closed. The shared start of
// ExecuteNextActivity (which awaits the pump's decision) and ResumeProject (which does
// not). The input carries no OperatorDriven: every new pump honours the recorded pause.
func (m *constructionManager) startOrJoinPump(ctx context.Context, projectID ProjectID) (client.WorkflowRun, error) {
	opts := client.StartWorkflowOptions{
		ID:                       pumpWorkflowID(projectID),
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}
	return m.client.ExecuteWorkflow(ctx, opts, executionKindPump, pumpInput{ProjectID: projectID})
}

// refuseWhilePaused is ExecuteNextActivity's paused precheck (B1.7): a recorded pause
// refuses Begin with FailedPrecondition, naming the operator's reason. A project that
// does not exist cannot be paused (the pump's own read is the quiet tick then); any
// other read fault is Infrastructure — Begin never dispatches past a pause it could
// not rule out.
func (m *constructionManager) refuseWhilePaused(ctx context.Context, projectID ProjectID) error {
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isRANotFound(err) {
			return nil
		}
		return newError(fwmanager.Infrastructure, "read the project before starting construction: "+err.Error())
	}
	if !proj.OperatorPaused {
		return nil
	}
	return newError(fwmanager.FailedPrecondition, pausedDetail(proj.PauseReason))
}

// pausedDetail is the refusal a paused project gets from Begin.
func pausedDetail(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return "construction is paused — resume it to continue"
	}
	return fmt.Sprintf("construction is paused (%s) — resume it to continue", reason)
}

// pumpDispatchPollInterval paces the façade's poll of the pump's synchronous
// dispatch-decision Query. The pump sets its decision after reading head-state and
// picking (or finding no) eligible activity — a couple of activity round-trips — so
// the poll converges within a few iterations.
const pumpDispatchPollInterval = 25 * time.Millisecond

// pumpDispatchWaitBudget bounds the poll, so a run that never reaches its decision point
// cannot hold the caller forever. A var (not a const) only so tests can shorten it.
//
// OPEN (fix round 3, pending a contract ruling): when this budget expires while the run
// is still RUNNING and undecided, the honest answer is "still deciding — the pump is
// running", which is not a failure. PumpResult has no such outcome and fwmanager has no
// non-failure Kind, so saying it on the wire needs a contract change
// (project.json .serviceContracts), which this branch must not make while D9 rewrites
// project.json. Until then the façade returns promptly with a DISTINGUISHABLE
// Infrastructure Detail (pumpStillDecidingDetail) instead of waiting out the terminal
// budget into a generic timeout.
var pumpDispatchWaitBudget = 30 * time.Second

// pumpQueryFailureBudget is how long the dispatch-decision Query may KEEP failing (e.g.
// no worker polling during a rolling restart) before the façade stops retrying it and
// falls back to the bounded terminal wait. A single failed Query is retried, never read
// as "the run is gone". A var only so tests can shorten it.
var pumpQueryFailureBudget = 10 * time.Second

// pumpClosureCheckInterval paces the DescribeWorkflowExecution check that detects a run
// which CLOSED without deciding (it failed before its decision point). Queries against a
// closed run are served by replay and keep answering "not decided", so without this
// check the caller would wait out the whole budget and could lose the run's real error.
// A var only so tests can shorten it.
var pumpClosureCheckInterval = 250 * time.Millisecond

// pumpStillDecidingDetail marks the budget-exhausted, run-still-RUNNING outcome, so a
// caller can tell a slow pump from a failed one (see pumpDispatchWaitBudget's OPEN note).
const pumpStillDecidingDetail = "construction pump is still deciding — it is running, not failed; re-check with GetSessionState"

// awaitDispatchDecision returns THIS run's dispatch outcome as soon as the pump run has
// decided, WITHOUT waiting for the background self-cascade to drain the dependency
// frontier. It polls queryPumpDispatch against the exact run ExecuteWorkflow started or
// joined (pinned RunID), so the answer stays that run's decision even after the pump
// ContinueAsNews into the next cascade iteration. A slow run is not a failed one:
//   - "not decided" answers keep the poll going;
//   - a FAILING Query is retried for up to pumpQueryFailureBudget before falling back to
//     the bounded terminal wait;
//   - a run that CLOSED without deciding surfaces its own terminal result / real error
//     promptly (throttled Describe);
//   - a run still RUNNING and undecided at the budget returns pumpStillDecidingDetail.
func (m *constructionManager) awaitDispatchDecision(ctx context.Context, we client.WorkflowRun, wfID string) (PumpResult, error) {
	runID := we.GetRunID()
	deadline := time.Now().Add(pumpDispatchWaitBudget)
	var queryFailingSince, lastClosureCheck time.Time
	for {
		d, qerr, fatal := m.pollPumpDispatch(ctx, wfID, runID)
		if fatal != nil {
			return PumpResult{}, fatal
		}
		if qerr == nil && d.Decided {
			return PumpResult{Dispatched: d.Dispatched, ActivityID: d.ActivityID}, nil
		}
		now := time.Now()
		switch {
		case qerr == nil:
			queryFailingSince = time.Time{}
		case queryFailingSince.IsZero():
			queryFailingSince = now
		}
		if now.Sub(lastClosureCheck) >= pumpClosureCheckInterval {
			lastClosureCheck = now
			if m.pumpRunClosed(ctx, wfID, runID) {
				return m.terminalPumpResult(ctx, we)
			}
		}
		if qerr != nil && now.Sub(queryFailingSince) >= pumpQueryFailureBudget {
			return m.terminalPumpResult(ctx, we)
		}
		if now.After(deadline) {
			if m.pumpRunClosed(ctx, wfID, runID) {
				return m.terminalPumpResult(ctx, we)
			}
			return PumpResult{}, newError(fwmanager.Infrastructure, pumpStillDecidingDetail)
		}
		select {
		case <-ctx.Done():
			return PumpResult{}, newError(fwmanager.Infrastructure, ctx.Err().Error())
		case <-time.After(pumpDispatchPollInterval):
		}
	}
}

// pumpRPCTimeout bounds EACH Query / Describe RPC the dispatch-decision poll makes — the
// same move terminalPumpResult makes for we.Get. Without it a single hung RPC would block
// past every wall-clock budget (pumpDispatchWaitBudget, pumpQueryFailureBudget), since
// those are only checked between RPCs. A timed-out Query reads as a failing Query
// (retried, then the bounded fallback); a timed-out Describe as "not known closed". A var
// only so tests can shorten it.
var pumpRPCTimeout = 5 * time.Second

// pollPumpDispatch runs one queryPumpDispatch against the pinned run, bounded by
// pumpRPCTimeout. queryErr means the run could not SERVE the Query right now (the caller
// retries); fatal is a decode failure of an answer it did serve — surfaced at once, never
// polled as "not decided".
func (m *constructionManager) pollPumpDispatch(ctx context.Context, wfID, runID string) (d pumpDispatch, queryErr, fatal error) {
	qctx, cancel := context.WithTimeout(ctx, pumpRPCTimeout)
	defer cancel()
	enc, err := m.client.QueryWorkflow(qctx, wfID, runID, queryPumpDispatch)
	if err != nil {
		return pumpDispatch{}, err, nil
	}
	if err := enc.Get(&d); err != nil {
		return pumpDispatch{}, nil, newError(fwmanager.Infrastructure, err.Error())
	}
	return d, nil, nil
}

// pumpRunClosed reports whether the pinned pump run has CLOSED (any status but
// Running — Failed, Canceled, Terminated, TimedOut, or Completed without having decided).
// Bounded by pumpRPCTimeout. A Describe failure, or an empty answer, reads as "not known
// closed" — the poll carries on.
func (m *constructionManager) pumpRunClosed(ctx context.Context, wfID, runID string) bool {
	dctx, cancel := context.WithTimeout(ctx, pumpRPCTimeout)
	defer cancel()
	resp, err := m.client.DescribeWorkflowExecution(dctx, wfID, runID)
	if err != nil {
		return false
	}
	info := resp.GetWorkflowExecutionInfo()
	if info == nil {
		return false
	}
	return info.GetStatus() != enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING
}

// pumpTerminalWaitBudget bounds terminalPumpResult's wait. A var (not a const) only so
// a test can shorten it.
var pumpTerminalWaitBudget = 10 * time.Second

// terminalPumpResult is the safety-net fallback: it awaits the pump's terminal result
// (used only when the dispatch decision never surfaced — a failed run or a run that
// finished before it could be polled).
//
// BOUNDED (fix round, M7): WorkflowRun.Get FOLLOWS the ContinueAsNew chain, so for a
// caller that joined a RUNNING pump (one pump per project) and then hit a query failure,
// an unbounded Get would block until the whole self-cascade drains — hours of
// construction. A failed run returns well inside the budget, so its error still
// surfaces; past the budget the caller gets an Infrastructure error instead of a hang.
func (m *constructionManager) terminalPumpResult(ctx context.Context, we client.WorkflowRun) (PumpResult, error) {
	wctx, cancel := context.WithTimeout(ctx, pumpTerminalWaitBudget)
	defer cancel()
	var result PumpResult
	if err := we.Get(wctx, &result); err != nil {
		return PumpResult{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return result, nil
}

// RunReplanSweep — op 2.2. Temporal Workflow (entry; scheduler-triggered, short).
// Reads in-flight construction state, flags over-threshold variances, surfaces
// them to the operator dashboard — it does NOT auto-replan. An empty result is a
// normal quiet sweep. A nil projectID sweeps all in-flight projects (workflow id
// :all:replanSweep:{tickId}).
func (m *constructionManager) RunReplanSweep(rc fwmanager.Context, projectID *ProjectID, tickID string) (ReplanSweepResult, error) {
	ctx := rc.Context
	if tickID == "" {
		return ReplanSweepResult{}, newError(fwmanager.ContractMisuse, "empty tickId")
	}
	var in replanSweepInput
	if projectID != nil {
		if *projectID == "" {
			return ReplanSweepResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
		}
		pid := *projectID
		in.ProjectID = &pid
	}

	wfID := replanSweepWorkflowID(projectID, tickID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindReplanSweep, in)
	if err != nil {
		return ReplanSweepResult{}, csMapStartError(err)
	}
	var result ReplanSweepResult
	if err := we.Get(ctx, &result); err != nil {
		return ReplanSweepResult{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return result, nil
}

// PauseProject — op 2.3. Temporal Signal (operatorPauseRequested) to the project's
// in-flight construction execution(s). The suspended supervision resumes on its
// awaitSignal and runs the pause branch (interventionEngine.applyPausePolicy →
// pausePlan, then the Manager EXECUTES the cancels/records). The pause branch also
// relays the pause to the project's ONE pump ({projectId}:nextActivity) through
// messageBus.deliverSignal, so a cascading pump stops after its current activity
// instead of dispatching through the pause (runPauseBranch, projectsupervision.go).
// SYNC from the operator's POV: returns once the signal is durably enqueued.
func (m *constructionManager) PauseProject(rc fwmanager.Context, projectID ProjectID, reason string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if reason == "" {
		return newError(fwmanager.ContractMisuse, "empty pause reason")
	}

	wfID := pauseTargetWorkflowID(projectID)
	sig := operatorPauseSignal{ProjectID: projectID, Reason: reason}
	// Signal-with-start: the project-level supervision workflow resumes on its
	// awaitSignal and runs the pause branch; if not running, it is started
	// (constructionManager.md §6.2 — startOrSignalExecution semantics).
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	_, err := m.client.SignalWithStartWorkflow(ctx, wfID, signalOperatorPauseRequested, sig,
		opts, executionKindProjectSupervision, projectSupervisionInput{ProjectID: projectID})
	if err != nil {
		return mapSignalError(err)
	}
	return nil
}

// ResumeProject — op 2.10 (plan B1.7; amendment §B.1). Clears the project's RECORDED
// operator pause and starts (or joins) its pump. A write straight through the RA (the
// SetReviewPolicy pattern): no supervision workflow is involved, because the pause
// branch handles exactly one signal and exits.
//
// Refusals, in a PINNED order: ContractMisuse (empty id) → NotFound (no project) →
// FailedPrecondition, not in construction → FailedPrecondition, not paused (never a
// silent no-op) → FailedPrecondition, a pause is still being applied (its supervision
// run {p}:construction is RUNNING: the pause is recorded but not yet relayed, and a
// resume now could be followed by a relay that stops the fresh pump). Nothing is
// written on any refusal.
//
// The write (RecordOperatorResumed) is retried on a version Conflict up to
// resumeConflictAttempts times, re-reading the version between tries. Then the pump is
// started or joined WITHOUT awaiting its decision. If that start fails, the resume has
// still landed: the record is clear, so the 30s sweep pumps the project. That is logged,
// never returned as an error.
func (m *constructionManager) ResumeProject(rc fwmanager.Context, projectID ProjectID) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isRANotFound(err) {
			return newError(fwmanager.NotFound, err.Error())
		}
		return newError(fwmanager.Infrastructure, err.Error())
	}
	if proj.Phase != projectstate.PhaseConstruction {
		return newError(fwmanager.FailedPrecondition, fmt.Sprintf("project %s is not in construction, so there is no construction to resume", projectID))
	}
	if !proj.OperatorPaused {
		return newError(fwmanager.FailedPrecondition, "construction is not paused — there is nothing to resume")
	}
	if err := m.refuseWhilePauseInFlight(ctx, projectID); err != nil {
		return err
	}
	if err := m.recordResumed(ctx, projectID, proj); err != nil {
		return err
	}
	if _, err := m.startOrJoinPump(ctx, projectID); err != nil {
		slog.Default().WarnContext(ctx, "construction resumed, but the pump did not start now; the 30-second sweep will start it",
			"projectId", string(projectID), "error", err.Error())
	}
	return nil
}

// resumeConflictAttempts bounds ResumeProject's re-read → re-write loop on a version
// Conflict before it answers "changed concurrently; retry".
const resumeConflictAttempts = 3

// refuseWhilePauseInFlight refuses a resume while the project's supervision run — the
// pause branch, {p}:construction — is RUNNING (bounded by pumpRPCTimeout). No such run
// is fine; any other describe fault is Infrastructure.
func (m *constructionManager) refuseWhilePauseInFlight(ctx context.Context, projectID ProjectID) error {
	dctx, cancel := context.WithTimeout(ctx, pumpRPCTimeout)
	defer cancel()
	resp, err := m.client.DescribeWorkflowExecution(dctx, pauseTargetWorkflowID(projectID), "")
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return newError(fwmanager.Infrastructure, "check whether a pause is still being applied: "+err.Error())
	}
	if info := resp.GetWorkflowExecutionInfo(); info != nil && info.GetStatus() == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return newError(fwmanager.FailedPrecondition, "a pause is still being applied — retry in a moment")
	}
	return nil
}

// recordResumed writes RecordOperatorResumed at seen's version, retrying on a Conflict
// (one idempotency key per call, so a retried transport write dedupes). A Conflict means
// the project moved, so every retry RE-CHECKS what the first try was allowed on (I2):
// still in construction, still paused, the SAME pause the operator resumed (its reason
// unchanged), and no pause being applied now. A pause that landed between two tries is
// never cleared by a resume that did not see it.
func (m *constructionManager) recordResumed(ctx context.Context, projectID ProjectID, seen projectstate.Project) error {
	key := fwra.IdempotencyKey("resume:" + string(projectID) + ":" + uuid.NewString())
	version := seen.Version
	for range resumeConflictAttempts {
		_, err := m.constructionTransition.RecordOperatorResumed(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID), version, projectstate.RepoCredential{}, key)
		if err == nil {
			return nil
		}
		if !csIsRAConflict(err) {
			return newError(fwmanager.Infrastructure, err.Error())
		}
		proj, rerr := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
		if rerr != nil {
			return newError(fwmanager.Infrastructure, rerr.Error())
		}
		// The façade's pinned order: still in construction, still paused, no pause being
		// applied now, and only then the same pause.
		if err := resumableStill(projectID, proj); err != nil {
			return err
		}
		if err := m.refuseWhilePauseInFlight(ctx, projectID); err != nil {
			return err
		}
		if proj.PauseReason != seen.PauseReason {
			return newError(fwmanager.FailedPrecondition, fmt.Sprintf("a new pause (%s) landed while resuming — review it, then resume again", proj.PauseReason))
		}
		version = proj.Version
	}
	return newError(fwmanager.FailedPrecondition, "the project changed concurrently while resuming — retry")
}

// resumableStill re-checks, on a re-read, the first two preconditions ResumeProject
// checked on its first read: the project is still in construction and still paused.
// (recordResumed then re-checks the pause in flight, and that the pause is the one the
// operator resumed: a different reason is a different pause the operator has not seen.)
func resumableStill(projectID ProjectID, now projectstate.Project) error {
	switch {
	case now.Phase != projectstate.PhaseConstruction:
		return newError(fwmanager.FailedPrecondition, fmt.Sprintf("project %s left construction while resuming, so there is no construction to resume", projectID))
	case !now.OperatorPaused:
		return newError(fwmanager.FailedPrecondition, "construction is not paused — there is nothing to resume")
	}
	return nil
}

// csIsRAConflict reports whether err is (or wraps) a ResourceAccess version Conflict.
func csIsRAConflict(err error) bool {
	var fe *fwra.Error
	if errors.As(err, &fe) {
		return fe.Kind == fwra.Conflict
	}
	return false
}

// OverrideActivity — op 2.4. Temporal Signal (operatorOverride) to the per-activity
// child workflow {projectId}:{activityId}. The operator's steer is fed through the
// SAME decide→execute machinery as the automatic variance path. SYNC: returns once
// the signal is durably enqueued.
//
// PRECHECK (B1.3, re-pointed at the ledger by stage 4b2 Task 2): after the ContractMisuse
// checks, the op refuses with FailedPrecondition unless the activity's attempt LEDGER names
// an escalated task. The workflow buffers override signals, so an override sent at any other
// time used to be consumed by the activity's NEXT escalation — a steer applied to a situation
// the operator never saw (plan G7). This is honesty at the façade, not a lock: the residual
// check-then-act window is milliseconds, and draining a stale buffered override inside the
// workflow is a command change that is EARMARKED behind its own GetVersion.
//
// ONE WINDOW THE HONESTY NO LONGER COVERS, stated rather than implied (4b2 Task 2 review, F1).
// On intervention.VarianceRetry / VarianceTakeover the child auto-re-dispatches WITHOUT
// entering StageAwaitingTakeover (deliveryactivity.go's variance arm), so between the FAILED
// attempt landing on the row and the retry's own attempt landing, escalatedTaskOf names the
// task while the child is at StageDispatching / StagePipelineRunning. The override is
// ACCEPTED, routed (awaitRoutedOverride applies no filter) and consumed by an unrelated later
// escalation: the operator gets a 200 for a steer with no visible effect. The old stage check
// did not cover this window either (it refused the whole fork case to catch it), and closing
// it for real is the earmarked in-workflow drain, not a second façade guess.
//
// It was the session's single-valued `stage` that answered this until 4b2; see the precheck
// itself for why a FORK made that wrong.
func (m *constructionManager) OverrideActivity(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, override ActivityOverride) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return newError(fwmanager.ContractMisuse, "empty activityId")
	}
	switch override.Kind {
	case OverrideTakeover, OverrideRetry, OverrideSkip, OverrideReassign:
		// ok
	case OverrideUnknown:
		// zero-value sentinel, not a real override kind — same as any unmapped value.
		return newError(fwmanager.ContractMisuse, fmt.Sprintf("unknown override kind %d", int(override.Kind)))
	default:
		return newError(fwmanager.ContractMisuse, fmt.Sprintf("unknown override kind %d", int(override.Kind)))
	}
	if strings.TrimSpace(override.Notes) == "" {
		return newError(fwmanager.ContractMisuse, "an override requires non-empty notes — it is the operator's durable record of WHY the automatic path was steered")
	}
	if err := checkOperatorNoteSize("an override's notes", override.Notes, override.Comments); err != nil {
		return err
	}
	// THE PRECHECK ASKS THE LEDGER, NOT THE VIEW (stage 4b2 Task 2; the open 4b1 defect).
	// constructState.stage is SINGLE-VALUED and a fork holds two gates, so the view reports
	// whichever was entered last and this precheck refused a steer the client correctly
	// offered — on a `service`/`frontend` walk with `stp` escalated and `designReview` at an
	// approval gate, the operator could not reach the branch that was actually waiting on
	// them. The authoritative answer was already being read fifteen lines below, on the
	// success path: escalatedTaskOf scans the row's attempts per TASK, and 4b1 proved the
	// ledger correct on a fork (each review coroutine owns its own *gateLedger).
	//
	// The session read STAYS, for the one thing it still answers that the row cannot: is
	// there a LIVE CHILD. NotFound means the activity is OVER, and the reopen arm below is
	// what an operator looking at a finished activity is asking for. Every other override
	// addresses a dispatch in flight and has nothing to reach. During a rolling deploy a
	// view served by an old worker still answers the liveness question, so the refusal no
	// longer depends on a field an old worker may not carry.
	if _, err := m.activitySession(ctx, projectID, activityID); err != nil {
		if isManagerNotFound(err) {
			return m.reopenActivity(ctx, projectID, activityID, override)
		}
		return err
	}
	// THE OVERRIDE MUST NAME THE TASK IT STEERS (stage 4b1 Task 12; the Task-11 round-2
	// defect D2). The generic child's router forwards by TaskID and DROPS a signal that
	// names none, and since the variance loop moved into the child (Task 11, fix round 1) the
	// escalation WAITS on the escalated task's own inbox — so a task-less override meant
	// every escalation timed out with the operator unable to steer, and under
	// EscalateEverything (a zero window) waited forever while the pump blocked on child.Get.
	// The task is recovered from the LEDGER (escalatedTaskOf), because nothing the operator
	// sends carries it and the session view's gate key for an escalation is `takeover`.
	//
	// escalatedTask refuses FailedPrecondition when the ledger names no escalated task, with
	// the sentence the operator reads. It is the ONE question, asked ONCE — it is both the
	// precheck and the addressing.
	task, err := m.escalatedTask(ctx, projectID, activityID)
	if err != nil {
		return err
	}
	wfID := deliveryActivityWorkflowID(projectID, activityID)
	sig := operatorOverrideSignal{Override: override, TaskID: string(task)}
	if err := m.client.SignalWorkflow(ctx, wfID, "", signalOperatorOverride, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// reopenActivity is the override's REOPEN arm: against a TERMINAL row with NO LIVE CHILD it
// re-arms the row at its CURRENT revision so the pump selects the activity again on its next
// tick (stage 4b1 Task 12, fix round 1; controller ruling 3).
//
// WHAT IT IS FOR. A failed walk, a spent variance budget and an operator's own Skip all leave a
// terminal row, and until the requeue note re-armed one, nothing could ever re-run it: the pump
// refuses any row a pump has written. The activity's ledger is the whole point of re-arming
// rather than re-planning — the next walk seeds every task that PASSED from it
// (seedWalkFromLedger) and re-dispatches only what did not, and its per-dispatch counter
// continues the ledger's numbering (seedTaskAttempts). NOTHING here mints an attempt or a
// revision: this writes head facts, and the walk owns its own numbering.
//
// IT IS ONE WRITE, and that is the fold's own argument: the operator's REASON and the re-arm
// land in the SAME commit (RecordOperatorNote of kind requeue — see reopenTerminalRow in the
// store), so no crash can leave a re-armed activity with nobody's name on it, and none can leave
// a reason filed against an activity that was never re-armed.
//
// It is the ONE override kind that is NOT a steer of something in flight, and every kind
// reaches it: an operator looking at a finished activity is asking for it to run again whatever
// word the SPA put on the button. The store refuses a row that has NOT exited, so a live
// activity whose child merely has not started yet cannot be re-armed through this door.
func (m *constructionManager) reopenActivity(ctx context.Context, projectID ProjectID, activityID ActivityID, override ActivityOverride) error {
	return m.onActivityRow(ctx, projectID, activityID, func(proj projectstate.Project, row projectstate.ActivityExecution) error {
		// TERMINALITY IS PRE-CHECKED HERE, on the row this attempt just read, and the reason is the
		// error KIND the store answers with (Task 12 round 3, minor (e)): a requeue against a live
		// row is refused `fwra.Conflict`, because the arguments are impeccable and only the STATE is
		// wrong. But onActivityRow reads a Conflict as a RACE and retries it, so without this check
		// the operator's refusal arrives as "activity A changed concurrently … re-read it and try
		// again" after three wasted attempts — a sentence about a race that never happened, for a
		// row that is simply still running. The store's Conflict stays the backstop for the genuine
		// race (an activity that exits between this read and the write); this is the honest answer
		// for the case the operator is actually in.
		if phase := projectstate.CoarsePhaseFor(row, nil); phase != projectstate.ActivityConstructionDone &&
			phase != projectstate.ActivityConstructionFailed {
			return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
				"constructionManager.OverrideActivity: activity %s is %v, not finished — a requeue re-arms an activity that already exited, and re-arming a live one would hand a second child the row this one is writing",
				activityID, phase))
		}
		_, err := m.activityExecution.RecordOperatorNote(fwra.Context{Context: ctx},
			projectstate.ProjectID(projectID), proj.Version, row.Version, string(activityID),
			projectstate.OperatorNoteInput{
				NoteID: reopenNoteID(activityID, row),
				Kind:   projectstate.NoteRequeue,
				Gate:   reopenGateKey,
				Text:   override.Notes,
				// The note carries the operator's own anchored comments, exactly as a takeover's does.
				Comments: noteComments(override.Comments),
			}, "", projectstate.RepoCredential{}, rowWriteKey("reopen", projectID, activityID))
		if err != nil && !csIsRAConflict(err) {
			return mapRAError(err, "activityExecutionAccess.RecordOperatorNote")
		}
		return err
	})
}

// reopenGateKey is the gate a requeue note is filed under. It is not a lifecycle task and not
// the takeover gate: a re-open is a decision about the WHOLE activity, and filing it under the
// task that happened to fail would read as a steer of that task.
const reopenGateKey = "reopen"

// reopenNoteID keys the requeue note to the TERMINAL it re-opens — the exit stamp, which is
// unique per terminal and non-nil on every one of them (stampExit writes it for both arms).
//
// It is deliberately NOT a sequence over the note list: the two writes are separate commits, so
// a re-open whose note landed and whose re-arm lost the CAS is retried — and a count-based id
// would mint a SECOND id on that retry and file the operator's reason twice. Keyed by the
// terminal, the retry converges on the same id and the store absorbs it ("one id names one
// note"), while a genuinely later re-open of a re-failed activity has a new exit stamp and so a
// new id.
func reopenNoteID(activityID ActivityID, row projectstate.ActivityExecution) string {
	at := int64(0)
	if row.CompletedAt != nil {
		at = row.CompletedAt.UnixNano()
	}
	return fmt.Sprintf("%s:note:%s:%d", activityID, reopenGateKey, at)
}

// escalatedTask reads the activity's execution row and answers which task the operator's
// override is about (escalatedTaskOf). It refuses with FailedPrecondition naming the missing
// datum rather than sending a signal the router would silently drop.
//
// Since stage 4b2 Task 2 its refusal IS OverrideActivity's precheck — "no escalated task on
// the ledger" and "not steerable" are the same fact, and asking once is what makes a fork's
// escalated branch reachable while its sibling holds a gate.
//
// It is the NARROW read (ReadActivityExecution) and not a whole-project one: the only thing
// it needs is one row's attempt ledger, and an activity with no row has not been dispatched,
// which the store answers NotFound for and this maps to the sentence an operator can act on.
func (m *constructionManager) escalatedTask(ctx context.Context, projectID ProjectID, activityID ActivityID) (projectstate.MethodTask, error) {
	row, err := m.activityExecution.ReadActivityExecution(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID), string(activityID))
	if err != nil {
		if isRANotFound(err) {
			return "", newError(fwmanager.FailedPrecondition, fmt.Sprintf(
				"activity %s has no execution row, so nothing has been dispatched for an override to steer", activityID))
		}
		return "", mapRAError(err, "activityExecutionAccess.ReadActivityExecution")
	}
	task, ok := escalatedTaskOf(row)
	if !ok {
		return "", newError(fwmanager.FailedPrecondition, fmt.Sprintf(
			"activity %s is in flight but no task on its ledger holds a failed attempt, so it is not escalated and there is nothing an override could name — decide a gate with SubmitTaskDecision, or re-read the activity",
			activityID))
	}
	return task, nil
}

// GetSessionState — op 2.5. Temporal Query (sessionState, read-only). Returns a
// point-in-time technical view without mutating state. When activityID is non-nil
// it queries the per-activity child {projectId}:{activityId}; otherwise the
// project-level pump view (constructionManager.md §6.2).
func (m *constructionManager) GetSessionState(rc fwmanager.Context, projectID ProjectID, activityID *ActivityID) (ConstructionSessionView, error) {
	ctx := rc.Context
	if projectID == "" {
		return ConstructionSessionView{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}

	var wfID string
	if activityID != nil {
		if *activityID == "" {
			return ConstructionSessionView{}, newError(fwmanager.ContractMisuse, "empty activityId")
		}
		// THE GENERIC CHILD IS THE ONE PER-ACTIVITY EXECUTION (stage 4b1 Task 12, the Task-11
		// round-2 defect D2). Task 11 re-pointed the pump at DeliveryActivityWorkflow for
		// EVERY activity, and this query kept naming the retired child's id — so every
		// per-activity session read answered NotFound, which is what made both façade
		// prechecks below refuse every decision and every override. There is no version fence
		// here because the Manager is not a workflow: it must address whatever is RUNNING, and
		// after the drain this wave requires (spec §8) that is only ever the generic child.
		wfID = deliveryActivityWorkflowID(projectID, *activityID)
	} else {
		wfID = pauseTargetWorkflowID(projectID)
	}

	enc, err := m.client.QueryWorkflow(ctx, wfID, "", querySessionState)
	if err != nil {
		// F20 (error altitude): before construction starts the pump/per-activity
		// workflow does not exist, and Temporal's raw "workflow not found for ID:
		// <proj>:construction" leaks the internal execution id to the client. Map that
		// to a clean, user-altitude NotFound; other query faults keep their generic
		// mapping.
		if isNotFound(err) {
			// Which session is absent decides the sentence. A per-activity miss means only
			// that the pump has not dispatched THIS activity — construction may well be
			// under way elsewhere in the project, so "construction has not started for this
			// project" was false for it.
			if activityID != nil {
				return ConstructionSessionView{}, newError(fwmanager.NotFound,
					"no construction session for activity "+string(*activityID)+": the pump has not dispatched it")
			}
			return ConstructionSessionView{}, newError(fwmanager.NotFound, "construction has not started for this project")
		}
		return ConstructionSessionView{}, csMapQueryError(err)
	}
	var view ConstructionSessionView
	if err := enc.Get(&view); err != nil {
		return ConstructionSessionView{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return view, nil
}

// QueryActivityView — op 2.13 (unified-activity spec 2026-09-20, stage 0). The Activity
// Experience's single read: one activity's platform-fixed lifecycle (method-assets),
// each task's state and revisions, and the live review set. A PLAIN METHOD like the
// episode reads — no workflow, no signal; it asks the activity's session (the same
// Temporal Query GetSessionState serves) only while the row is Running, so a Done or
// not-started activity reads with Temporal down.
//
// A gate the row holds PERSISTED rounds for is a projection of that ledger (stage 3):
// its verdicts, thread, roster, subject, round number and decision are read, not derived.
// The reconstruction below (normalizeAttempts, deriveTaskViews) survives for the gates
// that have no round — every gate of every row written before the ledger existed — and
// the revision's provenance is what tells a reader which of the two they are looking at.
// An id the committed activity list does not
// hold is NotFound. Since stage 2 that list HOLDS requirements, architecture and
// projectDesign (slot 9 opens with all three), so a design activity reads like any
// other — ClassifyType types all three cleanly (stage 4b1 Task 10 retired the
// not-dispatchable sentinel this lens used to have to tolerate), and LifecycleKeyFor
// resolves all three against method-assets.
//
// The live session is read ONCE and both the attempt list and the gate come out of that
// one read: state rule 1 answers awaitingHuman for the gate task matching the live gate
// even with no revision at all, so a stale gate beside fresh attempts would show a gate
// with no history.
func (m *constructionManager) QueryActivityView(rc fwmanager.Context, projectID ProjectID, activityID ActivityID) (ActivityView, error) {
	ctx := rc.Context
	if projectID == "" {
		return ActivityView{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return ActivityView{}, newError(fwmanager.ContractMisuse, "empty activityId")
	}
	id := string(activityID)
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return ActivityView{}, mapRAError(err, "projectStateAccess.ReadProject")
	}
	item, ok := committedActivityItem(proj, id)
	if !ok {
		return ActivityView{}, newError(fwmanager.NotFound, "no activity "+id+" in the committed activity list")
	}
	row := proj.ActivityExecution[id]
	row.ActivityID = id
	typ, variant, resolved, classified := projectstate.ResolveConstructionRow(row, item)
	if !classified {
		return ActivityView{}, newError(fwmanager.FailedPrecondition, fmt.Sprintf(
			"activity %s (workerClass %q, coding=%v) matches no activity-classification rule, so it has no lifecycle — amend workerClass or coding in the committed activity list",
			id, item.WorkerClass, item.Coding))
	}
	key := projectstate.LifecycleKeyFor(typ, variant)
	lc, ok := methodassets.LifecycleFor(key)
	if !ok {
		return ActivityView{}, newError(fwmanager.Infrastructure, "the platform's method assets carry no lifecycle for activity type "+key)
	}
	coarse, _ := projectstate.EffectiveConstructionPhase(row, item)
	live, err := m.liveSessionFor(ctx, projectID, activityID, coarse)
	if err != nil {
		return ActivityView{}, err
	}
	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{ProjectID: episode.ProjectID(projectID), TargetRef: &id})
	if err != nil {
		return ActivityView{}, mapRAError(err, "episodeAccess.ListEpisodes")
	}
	// The gate is the session's awaitingGate verbatim: a lifecycle-phase id matches a
	// phase, and the merge hold and an escalation simply match none.
	liveGate, _ := liveApprovalGate(live)
	tasks := deriveTaskViews(lc, normalizeAttempts(id, row, resolved, records, live), row.OperatorNotes, row.Reviews, liveGate)
	view := activityViewFrom(activityID, item, typ, variant, lc, resolved, tasks)
	view.State = activityViewState(coarse, live)
	// The roster and the engine's refusal to produce one are the SAME fact about the live
	// gate, so they travel together under the one condition: a refusal without a live gate
	// would explain an absence nobody is looking at.
	if liveGate != "" {
		view.ReviewSet, view.ReviewSetError = live.ReviewSet, live.ReviewSetError
	}
	return view, nil
}

// GetPumpStatus — op 2.9 (plan B1.5). Reports whether the project's ONE construction
// pump ({projectId}:nextActivity, pumpWorkflowID) has a RUNNING execution now. It
// describes the pump id with an EMPTY run id, which reads the latest run — so a pump
// cascading between activities (ContinueAsNew keeps the id) reads as open. No pump for
// the project is {open:false} with no error; any other describe fault is Infrastructure.
// The describe is bounded by pumpRPCTimeout, like the dispatch poll's describes.
//
// It is ONE fact on purpose. It does not fold in the recorded operator pause or any
// activity's live session: the client combines them (a paused project's pump is not
// open; an open pump can be between sessions). It is not the project-level
// GetSessionState either: that targets the supervision execution ({p}:construction),
// which is not the pump.
func (m *constructionManager) GetPumpStatus(rc fwmanager.Context, projectID ProjectID) (PumpStatus, error) {
	if projectID == "" {
		return PumpStatus{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	dctx, cancel := context.WithTimeout(rc.Context, pumpRPCTimeout)
	defer cancel()
	resp, err := m.client.DescribeWorkflowExecution(dctx, pumpWorkflowID(projectID), "")
	if err != nil {
		if isNotFound(err) {
			return PumpStatus{Open: false}, nil
		}
		return PumpStatus{}, newError(fwmanager.Infrastructure, "describe the construction pump: "+err.Error())
	}
	info := resp.GetWorkflowExecutionInfo()
	if info == nil || info.GetStatus() != enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return PumpStatus{Open: false}, nil
	}
	status := PumpStatus{Open: true}
	if ts := info.GetStartTime(); ts != nil {
		started := ts.AsTime()
		status.RunStartedAt = &started
	}
	return status, nil
}

// activityRowWriteAttempts bounds a Manager-side row write's re-read → re-apply loop before
// it answers "changed concurrently; retry". It is the façade twin of the workflow's
// applyRecovering bound and of ResumeProject's resumeConflictAttempts, and it exists for the
// same reason: the CHILD writes the same row while the operator is deciding, so a Conflict
// here is the ordinary case and not an error to surface.
const activityRowWriteAttempts = 3

// onActivityRow is the Manager-side applyRecovering: read the project ONCE (its version AND
// the row's come out of the same read, so the two expectations the facet's CAS pair needs
// can never disagree), apply, and re-read on a version Conflict.
//
// ONE READ PER ATTEMPT, deliberately. The narrow ReadActivityExecution answers the row but
// not the project version the verbs assert on, so a narrow read would be two reads and two
// chances for them to describe different moments.
//
// A Conflict is RETRIED; anything else is returned as it came, because `apply` also carries
// this path's own FailedPrecondition refusals (no round, a decided round, a live gate) and
// re-reading cannot change any of them.
func (m *constructionManager) onActivityRow(
	ctx context.Context, projectID ProjectID, activityID ActivityID,
	apply func(proj projectstate.Project, row projectstate.ActivityExecution) error,
) error {
	var last error
	for range activityRowWriteAttempts {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
		if err != nil {
			if isRANotFound(err) {
				return newError(fwmanager.NotFound, err.Error())
			}
			return newError(fwmanager.Infrastructure, err.Error())
		}
		row, ok := proj.ActivityExecution[string(activityID)]
		if !ok {
			return newError(fwmanager.NotFound, fmt.Sprintf(
				"activity %s has no execution row: the pump has not dispatched it, so it has no ledger to write on", activityID))
		}
		row.ActivityID = string(activityID)
		err = apply(proj, row)
		if err == nil {
			return nil
		}
		if !csIsRAConflict(err) {
			return err
		}
		last = err
	}
	return fwmanager.Wrap(fwmanager.FailedPrecondition, last, fmt.Sprintf(
		"activity %s changed concurrently while the write was applied — re-read it and try again", activityID))
}

// rowWriteKey mints one Manager-side write's idempotency key. It is DELIBERATELY per-call
// (a uuid) and not derived from the write's content: the store's dedup ledger would absorb a
// resolve → reopen → resolve sequence's third write as a replay of the first, and a comment
// really can be resolved twice with a reopen between.
func rowWriteKey(op string, projectID ProjectID, activityID ActivityID) fwra.IdempotencyKey {
	return fwra.IdempotencyKey(op + ":" + string(projectID) + ":" + string(activityID) + ":" + uuid.NewString())
}

// SetTaskCommentStatus is the construction rail's Resolve / Reopen (stage 4a refusal 2 of 5).
// It walks the ROUND's thread through the same transitions the artifact ledger's own
// comment-status verb walks (open→resolved, answered→resolved, resolved→open); the store
// owns those rules and this does not restate them.
//
// It is SYNCHRONOUS and it is the only writer. The design rails answer the same verb with a
// fire-and-forget signal into a per-kind session, which cannot tell the caller that the
// comment does not exist or that the transition is illegal; a construction reviewer gets
// both answers back. The generic child's setCommentStatus arm is NOT mirrored behind this —
// see applyRoundCommentStatus, which records the measurement.
func (m *constructionManager) SetTaskCommentStatus(
	rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID, commentID, status string,
) error {
	if strings.TrimSpace(commentID) == "" {
		return newError(fwmanager.ContractMisuse, "a comment-status decision needs the commentId it is about")
	}
	if strings.TrimSpace(status) == "" {
		return newError(fwmanager.ContractMisuse, "a comment-status decision needs the status to set")
	}
	var mirror string
	if err := m.onActivityRow(rc.Context, projectID, activityID, func(proj projectstate.Project, row projectstate.ActivityExecution) error {
		r, ok := roundOfComment(row, commentID)
		if !ok {
			// The caller's own address is in the sentence, because "no such comment" is most often
			// a client looking at a different task's thread than the one it names.
			return newError(fwmanager.NotFound, fmt.Sprintf(
				"no review comment %s on any round of activity %s (addressed at task %s)", commentID, activityID, taskID))
		}
		mirror = taskOfRound(r)
		_, err := m.activityExecution.SetReviewCommentStatus(fwra.Context{Context: rc.Context},
			projectstate.ProjectID(projectID), proj.Version, row.Version, string(activityID),
			r.RoundID, commentID, status, projectstate.RepoCredential{},
			rowWriteKey("comment-status", projectID, activityID))
		if err != nil && !csIsRAConflict(err) {
			return mapRAError(err, "activityExecutionAccess.SetReviewCommentStatus")
		}
		return err
	}); err != nil {
		return err
	}
	// THE MIRROR SIGNAL, and it is load-bearing since fix round 2: a gate held ONLY because its
	// round carried an open comment auto-passes the moment the last one is resolved, and the CHILD
	// is the only thing that can decide that. Nothing is written by it (the ledger write above is
	// the whole of the write), and a dormant activity is not an error: the status is already
	// recorded, and there is no gate left to release.
	//
	// It is addressed by the ROUND's own task (taskOfRound), not by the task the caller named:
	// the router forwards by TaskID, and the round that holds the comment is the gate whose hold
	// this could release.
	if err := m.signalActivity(rc.Context, projectID, activityID, signalSetCommentStatus,
		setCommentStatusSignal{TaskID: mirror, CommentID: commentID, Status: status}); err != nil {
		if isManagerFailedPrecondition(err) {
			return nil
		}
		return err
	}
	return nil
}

// roundKindOfTask resolves the ARTIFACT KIND a task's round judges, off the lifecycle — the
// same answer the child's openRound stamps on the round (roundArtifactKind), so a Manager write
// and a child write agree about which round they mean (fix round 2, review minor M1).
//
// It answers nil for every CONSTRUCTION task, and that is not because construction tasks name no
// artifactKind — v0.9.0's srs/detailedDesign/construction/integration/stp all do ("SRS",
// "DetailedDesign", …). It is because none of those names is a projectstate.ArtifactKind: they
// name the task's own work product, not one of the seventeen design SLOTS, so ArtifactKindFromWireName
// does not resolve them and the round is kindless — which is exactly what
// ReviewRound.ArtifactKind's optionality means. A DESIGN task's round IS kinded, and passing nil
// for one would resolve `architectureReview` to whichever of its three kinds was written last.
func roundKindOfTask(lc methodassets.Lifecycle, taskID string) *projectstate.ArtifactKind {
	t, ok := lifecycleTaskByID(lc, taskID)
	if !ok {
		return nil
	}
	return roundArtifactKind(lc, t)
}

// isManagerFailedPrecondition reports whether err is (or wraps) this Manager's own
// FailedPrecondition — which signalActivity answers for an activity with no live execution.
func isManagerFailedPrecondition(err error) bool {
	var me *fwmanager.Error
	return errors.As(err, &me) && me.Kind == fwmanager.FailedPrecondition
}

// WithdrawReviewRound pulls a round BACK — decided RoundWithdrawn, judged by nobody (stage
// 4a refusal 3 of 5; Task 4 gave the outcome its wire member and this is what fills it).
//
// IT REFUSES AT A LIVE GATE, and that refusal is the whole design. A withdraw is not a
// verdict: it records that nobody judged this round. Landing it behind a child that is
// AWAITING that very round would strand the walk — the gate keeps waiting, and its eventual
// decision hits DecideReviewRound's terminality Conflict on a round the operator closed. So
// a live gate is answered with Approve, with a send-back, or with a re-dispatch, and the
// withdraw is for the round NO ONE is judging: the activity's execution is gone (a crash, a
// deploy, a give-up) and its last round is still pending. The stranded-round sweep closes
// those on its own schedule; this is the operator's way to do it now, with their own name on
// it rather than the sweep's.
//
// THE CHECK-THEN-ACT WINDOW, and its REAL consequence (fix round 2, review minor M2). Between
// the session read above and the write below, a pump tick can start a child that opens a gate on
// this very round. The write then lands, and the child's own decision hits DecideReviewRound's
// terminality Conflict — at which point the recovery loop does NOT burn its bound: the row's own
// version has not moved, so terminalAfterRowReread recognises the Conflict as TERMINAL rather
// than as a race and short-circuits to a NonRetryable error after one or two attempts. The walk
// FAILS the activity, promptly and legibly. That is no longer unrecoverable: a failed activity is
// re-opened with a requeue note (reopenTerminalRow), the re-run seeds every task that passed, and
// the pump re-selects it (RequeuedAfterExit). The window is milliseconds wide and the outcome is
// a recoverable failure rather than a stranded one.
//
// ROUTING THE WITHDRAW THROUGH THE CHILD'S INBOX would serialize it against the child's own gate
// and is the obvious narrowing — but it does not narrow THIS window, because the window's premise
// is that there IS no child to route through: the only writes this op makes are the ones it makes
// after finding none. A child that starts LATER is unaffected, since it seeds its revision off the
// ledger and opens round n+1 over the withdrawn round rather than colliding with it.
func (m *constructionManager) WithdrawReviewRound(
	rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID string, kind *projectstate.ArtifactKind,
) error {
	ctx := rc.Context
	view, err := m.activitySession(ctx, projectID, activityID)
	switch {
	case err == nil:
		if view.Stage == StageAwaitingApproval && gateNameOf(view) == taskID {
			return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
				"activity %s is awaiting your decision at %s: approve it, send it back, or re-dispatch the task — a withdraw records that nobody judged the round, and pulling it out from under a live gate would strand the activity",
				activityID, taskID))
		}
	case isManagerNotFound(err):
		// NO LIVE EXECUTION is exactly the case this verb is for: the round is pending and
		// nothing is left to judge it.
	default:
		return err
	}
	return m.onActivityRow(ctx, projectID, activityID, func(proj projectstate.Project, row projectstate.ActivityExecution) error {
		r, rerr := latestRoundFor(row, taskID, kind)
		if rerr != nil {
			return rerr
		}
		if r.Outcome != projectstate.RoundPending {
			return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
				"round %d of %s is already decided %q by %s; a withdraw pulls back a round nobody has judged",
				r.Round, taskID, r.Outcome, r.DecidedBy))
		}
		_, err := m.activityExecution.DecideReviewRound(fwra.Context{Context: ctx},
			projectstate.ProjectID(projectID), proj.Version, row.Version, string(activityID),
			r.RoundID, projectstate.RoundWithdrawn, decidedByOperator, projectstate.RepoCredential{},
			rowWriteKey("withdraw", projectID, activityID))
		if err != nil && !csIsRAConflict(err) {
			return mapRAError(err, "activityExecutionAccess.DecideReviewRound")
		}
		return err
	})
}

// AskTaskQuestions records anchored QUESTIONS on the round under review (stage 4a refusal 4
// of 5). Spec §5.3: an Ask is a `ReviewComment.type = question` on the round's thread — not
// a third mechanism beside verdicts and comments — so it rides AppendReviewVerdict, which is
// the verb that lands a judgement AND its comments in ONE commit.
//
// THE VERDICT IT CARRIES IS AN ABSTENTION, because that is what asking IS: the reviewer has
// not judged, they have asked. Recording an approve or a send-back to get the comments onto
// the round would put a verdict in the ledger nobody cast — the same rule criticVerdictFor
// applies to a critic that did not finish.
//
// IT DOES NOT DISPATCH AN ANSWER JOB, and that is measured rather than forgotten: the design
// rails' answer job runs `design-answer`/`design-answer-pm` against an artifact KIND on a
// design branch, and its MCP verb (respondToReviewComment) answers a SLOT's thread and is not
// even registered in the construction job mode. A construction round's thread has no kind and
// no slot, so there is nothing for that job to answer and dispatching one would start a
// session that finds nothing to do. The questions are recorded, the SPA shows them, and a
// human answers them; a construction answer command is earmarked.
func (m *constructionManager) AskTaskQuestions(
	rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID, addressee string,
	kind *projectstate.ArtifactKind, questions []AnchoredComment,
) error {
	ctx := rc.Context
	// `at` is stamped ONCE, outside the loop, so a re-applied write carries the identical
	// utterance rather than a second one a clock apart (the design rail's own rule).
	at := time.Now().UTC().Format(time.RFC3339)
	fresh, replies := partitionIncomingComments(questions, at)
	qs := questionsToLedger(addressee, fresh)
	// A REPLY-ONLY batch is a legitimate ask — the follow-up IS the question this round.
	if len(qs) == 0 && len(replies) == 0 {
		return newError(fwmanager.ContractMisuse, "no questions to ask (every question needs text)")
	}
	return m.onActivityRow(ctx, projectID, activityID, func(proj projectstate.Project, row projectstate.ActivityExecution) error {
		r, rerr := latestRoundFor(row, taskID, kind)
		if rerr != nil {
			return rerr
		}
		// A DECIDED ROUND TAKES NO FURTHER VERDICTS (the store's own terminality rule), so the
		// refusal is stated here with the sentence a reviewer can act on rather than surfaced as
		// a Conflict from inside the append.
		if r.Outcome != projectstate.RoundPending {
			return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
				"round %d of %s is already decided %q: questions land on the round under review, so ask at the next one",
				r.Round, taskID, r.Outcome))
		}
		// A replyTo naming no utterance on THIS round is a hard refusal, never a silent new
		// thread — the same rule the design rails' ask applies, run against the thread just read.
		if perr := checkReplyTargets(ledgerCommentIDs(r.Thread), questions); perr != nil {
			return perr
		}
		_, err := m.activityExecution.AppendReviewVerdict(fwra.Context{Context: ctx},
			projectstate.ProjectID(projectID), proj.Version, row.Version, string(activityID), r.RoundID,
			projectstate.ReviewVerdict{
				ReviewerRole: reviewAuthorRole,
				Actor:        decidedByOperator,
				Verdict:      projectstate.VerdictAbstain,
				Summary:      askSummary(len(qs), len(replies), addressee),
				AttemptID:    judgedAttemptOfRound(string(activityID), row, r),
			}, qs, replies, projectstate.RepoCredential{},
			rowWriteKey("ask", projectID, activityID))
		if err != nil && !csIsRAConflict(err) {
			return mapRAError(err, "activityExecutionAccess.AppendReviewVerdict")
		}
		return err
	})
}

// askSummary is the one line the round's verdict list shows for an ask. It says WHAT was
// asked of WHOM, because the verdict row is what a later reader sees before they open the
// thread — and reviewVerdictPresent keys on the summary, so two different asks on one round
// are two verdicts rather than one absorbed as a replay of the other.
func askSummary(questions, replies int, addressee string) string {
	switch {
	case questions == 0:
		return fmt.Sprintf("asked %d follow-up(s) of %s", replies, addressee)
	case replies == 0:
		return fmt.Sprintf("asked %d question(s) of %s", questions, addressee)
	}
	return fmt.Sprintf("asked %d question(s) and %d follow-up(s) of %s", questions, replies, addressee)
}

// SubmitTaskDecision delivers the operator's verdict to the GENERIC DAG child, addressed BY
// TASK (stage 4a refusal 1 of 5, and the op every construction gate now travels through).
//
// It replaces the retired per-activity phase-decision door for three reasons, each measured:
// the pump starts DeliveryActivityWorkflow under a DIFFERENT id (so the old signal reached
// nothing); the generic child reads `taskDecision` and not the retired phase signal; and the
// gate key is now a lifecycle TASK id, which the old five-phase vocabulary rejected — so an
// approve at `designReview` was a ContractMisuse before it ever left the Manager.
//
// The PRECHECK is the retired op's, verbatim in shape (B1.3): the session must be awaiting a
// human at exactly this task, and a send-back past the redraft budget is refused rather than
// silently ignored. It is honesty and not safety — the child matches decisions by task id
// either way — and during a rolling deploy a view from an old worker carries no gate, so the
// refusal is transient and fails safe.
func (m *constructionManager) SubmitTaskDecision(
	rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID string,
	kind *projectstate.ArtifactKind, decision ReviewDecision, option *OptionID, feedback *ReviewFeedback,
) error {
	ctx := rc.Context
	if err := validateTaskDecision(taskID, decision); err != nil {
		return err
	}
	// A whitespace-only note is an empty one (M3), as it is for an override.
	if decision == ReviewReject && (feedback == nil || strings.TrimSpace(feedback.Notes) == "") {
		return newError(fwmanager.ContractMisuse, "a send-back requires non-empty feedback notes")
	}
	if feedback != nil {
		if err := checkOperatorNoteSize("a send-back note", feedback.Notes, feedback.Comments); err != nil {
			return err
		}
	}
	view, err := m.activitySession(ctx, projectID, activityID)
	if err != nil {
		return err
	}
	if err := precheckTaskDecision(view, activityID, taskID, decision); err != nil {
		return err
	}
	// THE PER-TASK HALF. The merge hold opens no round (see requireOpenRound), so it — and only
	// it — keeps the view check the rest of the gates gave up: after the join there is exactly
	// one human stage live, so the single-valued pair names it correctly.
	if taskID == mergeGateKey {
		if view.Stage != StageAwaitingApproval || gateNameOf(view) != mergeGateKey {
			return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
				"activity %s is at %s/%s, not holding for a merge approval",
				activityID, sessionStageName(view.Stage), gateNameOf(view)))
		}
	} else if err := m.requireOpenRound(ctx, projectID, activityID, taskID, kind); err != nil {
		return err
	}
	// THE APPROVE'S THREAD SETTLEMENT: an open CHANGE REQUEST refuses it, and every ANSWERED
	// thread is swept resolved. Both halves read ONE round, which is why they are one call —
	// see settleThreadsBeforeApprove.
	//
	// The child re-checks the refusal before it decides the round: this is a fire-and-forget
	// signal, so the window between the two is closed there (decideTaskGate) and not here.
	if decision == ReviewApprove {
		if err := m.settleThreadsBeforeApprove(ctx, projectID, activityID, taskID, kind); err != nil {
			return err
		}
	}
	// THE OPTION RIDES THE APPROVE (stage 4b1 Task 13). M0's approve is the one gate decision
	// that carries a CHOICE beside its verdict — which of the four project-design options the
	// founder bought — and the child's gate stamps it on the round it decides. Every other gate
	// passes nil, and the child ignores it for a task that names no option.
	sig := taskDecisionSignal{TaskID: taskID, Decision: decision, OptionID: option, Feedback: feedback, DecidedBy: decidedByOperator}
	return m.signalActivity(ctx, projectID, activityID, signalTaskDecision, sig)
}

// settleThreadsBeforeApprove SETTLES the task's latest round against the approve about to be
// sent, and it does two things rather than one — which is what the name says and the old one
// (refuseApproveOverOpenComments) did not:
//
//  1. an open CHANGE REQUEST REFUSES the approve, in the design rail's own words, because the
//     reviewer asked for something and approving over it would bury the ask. An open QUESTION does
//     NOT block — doctrine makes it a soft warning at the approve gate
//     (ReviewCommentBlocksApprove) — and the AUTOGATE is deliberately stricter, because there is
//     nobody there to be warned (runGate);
//  2. every ANSWERED thread is SWEPT resolved (design §3.4), so accepting a redraft that answered
//     eight change requests does not cost eight Resolve clicks.
//
// WHY ONE FUNCTION AND ONE ROUND READ, stated because a checker that writes is worth explaining
// rather than splitting on reflex: both halves are statements about the SAME thread at the SAME
// moment, and reading the round twice would let the refusal and the sweep see different threads —
// a comment filed between the two reads would be refused by neither and swept by the second. The
// order is load-bearing too: the sweep runs only AFTER the refusal has passed, because a blocked
// reviewer must not come back to a tidied thread and a gate that is still closed.
//
// A task with no round at all passes both halves — there is nothing to have left open, and
// refusing would block the first approve of every gate.
func (m *constructionManager) settleThreadsBeforeApprove(
	ctx context.Context, projectID ProjectID, activityID ActivityID, taskID string, kind *projectstate.ArtifactKind,
) error {
	row, err := m.activityExecution.ReadActivityExecution(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID), string(activityID))
	if err != nil {
		if isRANotFound(err) {
			return nil // no row, no round, nothing open
		}
		return mapRAError(err, "activityExecutionAccess.ReadActivityExecution")
	}
	round, rerr := latestRoundFor(row, taskID, kind)
	if rerr != nil {
		return nil // no round yet: the first approve of this gate has nothing to be blocked by
	}
	if open := projectstate.OpenReviewCommentIDs(round.Thread); len(open) > 0 {
		return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
			"cannot approve: %d review thread(s) still open (%s) — send them back or resolve them first",
			len(open), strings.Join(open, ", ")))
	}
	// APPROVE RESOLVES EVERY ANSWERED THREAD IN ONE GESTURE (design §3.4, restored on BOTH rails
	// by stage 4b1 Task 13's review fix round 1, finding 3). The retired design rail did this at
	// applyReviewLedgerGate and nothing on the generic child did, so accepting a redraft that
	// answered eight change requests left eight ANSWERED threads behind — each still shown as
	// outstanding on the Activity Experience, and each costing the reviewer a Resolve click on a
	// thread they had just accepted the answer to.
	//
	// A thread the reviewer explicitly REOPENED is open, not answered, so it is excluded by
	// construction and keeps blocking above. Resolving is best-effort in the same sense the
	// design rail's was: the approve is the decision and the tidy-up rides behind it, so a
	// failure to resolve one thread is logged and the approve still goes through rather than
	// being refused for a bookkeeping write.
	for _, id := range bulkResolveAnswered(round.Thread) {
		if err := m.SetTaskCommentStatus(fwmanager.Context{Context: ctx}, projectID, activityID, taskID,
			id, projectstate.ReviewCommentResolved); err != nil {
			slog.Default().Warn("approve: an ANSWERED thread could not be bulk-resolved; it stays answered and the approve goes on",
				"op", "delivery.SubmitTaskDecision", "projectID", string(projectID),
				"activityID", string(activityID), "taskID", taskID, "commentID", id, "err", err.Error())
		}
	}
	return nil
}

// bulkResolveAnswered returns the ids of every ANSWERED comment on a round. Approve resolves them
// all in one gesture (design §3.4). A thread the reviewer explicitly REOPENED is OPEN rather than
// answered, so it is excluded here and keeps blocking the approve.
func bulkResolveAnswered(thread []projectstate.ReviewComment) []string {
	var ids []string
	for _, c := range thread {
		if c.Status == projectstate.ReviewCommentAnswered {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// RedraftTask re-dispatches ONE task of a live activity (stage 4a refusal 5 of 5, the
// FailedPrecondition that said construction had no run/re-run op).
//
// It is the one op that makes the child's `redrafts` channel load-bearing, and the whole
// contract is in the payload: the router forwards by TaskID, so an unset id would make this
// a silent no-op. The child's redraft arm withdraws the round nobody judged and re-opens the
// judged pair at revision n+1 — the same thing a send-back does, asked for directly.
//
// IT REFUSES AWAY FROM A GATE. A redraft signal delivered to a task that is mid-dispatch sits
// in that task's inbox and is re-offered when it retires, i.e. it does nothing; and a task
// whose activity has already exited has no inbox at all. Both would present as success. The
// repair for a task whose activity gave up is a RE-OPEN of the activity, which needs a store
// verb this facet does not have (see the task-12 report).
func (m *constructionManager) RedraftTask(
	rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID string, feedback *ReviewFeedback,
) (SessionRef, error) {
	ctx := rc.Context
	// A redraft's feedback is OPTIONAL (nil = re-run with no steer), but a non-nil envelope
	// with empty notes is a third state that steers nothing while telling the agent it was
	// steered — RequestArtifactDraft refuses exactly this shape and this must agree.
	if feedback != nil {
		if strings.TrimSpace(feedback.Notes) == "" {
			return "", newError(fwmanager.ContractMisuse,
				"feedback is present but its notes are empty — omit feedback entirely to re-dispatch with no steer")
		}
		if err := checkOperatorNoteSize("a re-dispatch note", feedback.Notes, feedback.Comments); err != nil {
			return "", err
		}
	}
	view, err := m.activitySession(ctx, projectID, activityID)
	if err != nil {
		return "", err
	}
	// THE SAME TWO-HALF PRECHECK SubmitTaskDecision RUNS, and for the same reason: a
	// re-dispatch answers an OPEN GATE, and which gate is open is a per-TASK fact the
	// single-valued session stage cannot report on a fork. A redraft names no artifact kind,
	// so the round resolves kindless — the same resolution DispatchActivityTask uses.
	// `merge` needs no special case here: a merge hold has no draft to re-dispatch, and the
	// missing round refuses it in exactly those terms.
	if err := precheckTaskDecision(view, activityID, taskID, ReviewDecisionUnknown); err != nil {
		return "", err
	}
	if err := m.requireOpenRound(ctx, projectID, activityID, taskID, nil); err != nil {
		return "", err
	}
	sig := redraftSignal{TaskID: taskID, Feedback: feedback}
	if err := m.signalActivity(ctx, projectID, activityID, lSignalRedraft, sig); err != nil {
		return "", err
	}
	return SessionRef(deliveryActivityWorkflowID(projectID, activityID)), nil
}

// signalActivity delivers one signal to the activity's GENERIC child and maps the one
// failure an operator can act on: no live execution. mapSignalError's generic mapping hides
// that behind a transport sentence, and "the activity is dormant" is the difference between
// "retry" and "start the activity first".
func (m *constructionManager) signalActivity(
	ctx context.Context, projectID ProjectID, activityID ActivityID, name string, payload any,
) error {
	wfID := deliveryActivityWorkflowID(projectID, activityID)
	if err := m.client.SignalWorkflow(ctx, wfID, "", name, payload); err != nil {
		if isNotFound(err) {
			return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
				"activity %s has no live execution: the pump starts one when the activity is eligible, and a signal to a dormant activity would be lost",
				activityID))
		}
		return mapSignalError(err)
	}
	return nil
}

// isManagerNotFound reports whether err is (or wraps) this Manager's own NotFound. The
// per-activity session read answers it for an activity with no live execution, which two of
// the five write paths treat as a legitimate state rather than as a failure.
func isManagerNotFound(err error) bool {
	var me *fwmanager.Error
	return errors.As(err, &me) && me.Kind == fwmanager.NotFound
}

// gateNameOf renders a session's gate for a refusal sentence: the gate it is at, or that
// there is none.
func gateNameOf(v ConstructionSessionView) string {
	if v.AwaitingGate == nil || *v.AwaitingGate == "" {
		return "no gate"
	}
	return *v.AwaitingGate
}

// validateTaskDecision is SubmitTaskDecision's ContractMisuse gate over the (task, decision)
// pair. It is validatePhaseDecision's successor and it deliberately does NOT enumerate the
// keys: the vocabulary is now every lifecycle TASK id of fourteen lifecycles plus the merge
// hold, which this Manager cannot list without resolving the activity's lifecycle — and the
// precheck below it already refuses any key the session is not actually waiting at, which is
// the check that has teeth. The merge rule survives verbatim, because it is about the KEY and
// not about the session: a merge has no draft to send back.
func validateTaskDecision(taskID string, decision ReviewDecision) error {
	if strings.TrimSpace(taskID) == "" {
		return newError(fwmanager.ContractMisuse, "empty taskId")
	}
	if taskID == mergeGateKey && decision != ReviewApprove {
		return newError(fwmanager.ContractMisuse, fmt.Sprintf(
			"the %q gate accepts Approve only — a merge has no draft to send back; steer the activity with OverrideActivity instead", mergeGateKey))
	}
	switch decision {
	case ReviewApprove, ReviewReject:
		return nil
	case ReviewDecisionUnknown, ReviewWithdraw, ReviewSetCommentStatus, ReviewAdvance:
		return newError(fwmanager.ContractMisuse, fmt.Sprintf(
			"decision %d is not a verdict a task gate takes — approve or send back", int(decision)))
	}
	return newError(fwmanager.ContractMisuse, fmt.Sprintf("unknown decision %d", int(decision)))
}

// precheckTaskDecision is SubmitTaskDecision's and RedraftTask's FailedPrecondition gate over
// the activity's session view — precheckPhaseDecision's successor, keyed by TASK.
//
// IT NO LONGER ASKS WHICH GATE THE SESSION IS AT, and that is the whole point (stage 4b2,
// ride-along to Task 3; the same class of defect Task 2 fixed on the steer path, one step
// wider). It used to require `v.Stage == StageAwaitingApproval && gateNameOf(v) == taskID`,
// both read off constructState's SINGLE-VALUED `stage`/`awaitingGate` pair. enterHumanStage
// OVERWRITES that pair on every gate entry and leaveHumanStage clears it, so on an ordinary
// `service` fork — `stp`'s review and `designReview` open at once — only the gate entered
// SECOND was addressable and a decision on its sibling was refused with "activity X is at
// awaitingApproval/designReview, not awaiting stp". That is the ROUTINE approval path, not
// just an override: the operator could not approve one of two gates the screen showed them.
//
// THE DISCRIMINATOR IS THE ROUND (requireOpenRound below), because the round is per TASK and
// the stage is per ACTIVITY. What survives here is only what the stage answers HONESTLY for
// the whole activity rather than for one branch of it:
//
//   - StageExited / StagePaused are WHOLE-ACTIVITY facts (the walk is over, or the operator
//     paused the project). Neither is written by a branch, so neither can name the wrong one.
//   - RedraftExhausted is NOT such a fact — enterPhaseGate writes it per gate entry — and it
//     is left in place only because it narrows the reject path and removing it needs the
//     per-round budget the ledger does not carry. EARMARKED with the per-task session view.
//
// WHAT IS GIVEN UP, said out loud: a decision can now arrive between openRound and the gate
// actually opening (the critic's window), where the stage check used to refuse it. The child
// is built for exactly that — newWalkState seeds walkTaskPending "precisely so an early
// approve survives" and decideTaskGate re-checks the refusal before it decides the round — and
// the view cannot tell that window apart from a sibling branch running, so refusing it here
// meant refusing the fork case too. A guard that cannot distinguish the two must not pretend.
func precheckTaskDecision(v ConstructionSessionView, activityID ActivityID, taskID string, decision ReviewDecision) error {
	switch v.Stage {
	case StageExited:
		return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
			"activity %s has exited: there is no walk left to take a decision at %s — re-open the activity with an override",
			activityID, taskID))
	case StagePaused:
		return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
			"activity %s is paused: resume the project before deciding %s, or the decision waits where nobody can see it",
			activityID, taskID))
	case ConstructionStageUnknown, StageDispatching, StagePipelineRunning, StageReviewing,
		StageAwaitingTakeover, StageAwaitingApproval:
		// Every one of these is a stage a LIVE fork can report while another of its branches
		// holds the gate being decided. None of them is the answer to "is THIS task's gate
		// open" — requireOpenRound is.
	}
	if decision == ReviewReject && v.RedraftExhausted {
		return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
			"the redraft budget for %s of activity %s is spent — approve it or steer the activity with an override", taskID, activityID))
	}
	return nil
}

// requireOpenRound is the PER-TASK half of the decision precheck: the task must hold a review
// round that is still awaiting a verdict. It is the ledger question the stage could not answer
// (see precheckTaskDecision) and it is the same question the three other construction writes
// already ask — WithdrawReviewRound, AskTaskQuestions and SetTaskCommentStatus all resolve
// latestRoundFor and refuse a non-pending outcome, in those words.
//
// THE MERGE HOLD IS THE ONE GATE IT CANNOT ANSWER FOR, and that is data rather than an
// oversight: holdForMergeApproval enters its human stage directly and opens NO round, so the
// ledger holds nothing for `merge`. Its caller keeps the view check for that key alone, which
// is sound there because the merge hold runs AFTER the join — every branch gate is decided by
// then, so the single-valued pair has only one occupant to name.
func (m *constructionManager) requireOpenRound(
	ctx context.Context, projectID ProjectID, activityID ActivityID, taskID string, kind *projectstate.ArtifactKind,
) error {
	row, err := m.activityExecution.ReadActivityExecution(fwra.Context{Context: ctx},
		projectstate.ProjectID(projectID), string(activityID))
	if err != nil {
		if isRANotFound(err) {
			return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
				"activity %s has no execution row: nothing has been dispatched, so %s has no round to decide", activityID, taskID))
		}
		return mapRAError(err, "activityExecutionAccess.ReadActivityExecution")
	}
	row.ActivityID = string(activityID)
	rounds := roundsAtTask(row, taskID, kind)
	if len(rounds) == 0 {
		return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
			"task %s of activity %s has no review round: there is no gate open to decide", taskID, activityID))
	}
	var latest projectstate.ReviewRound
	for _, r := range rounds {
		if r.Outcome == projectstate.RoundPending {
			return nil
		}
		if r.Round >= latest.Round {
			latest = r
		}
	}
	return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
		"round %d of %s is already decided %q by %s — there is no open gate left to answer",
		latest.Round, taskID, latest.Outcome, latest.DecidedBy))
}

// roundsAtTask is the PRECHECK's round resolver, and it is deliberately NOT latestRoundFor.
// latestRoundFor answers "which round must this WRITE land on", so it keys through
// roundGateKey and a caller that names no artifact kind resolves only KINDLESS rounds — which
// is right for a write and wrong for a liveness question: `sdpReview` is the one review task
// in method-assets v0.9.0 that carries a kind of its own, so an M0 approve sent without one
// would find no round and be refused at a gate that is plainly open.
//
// So: kind-matched when the caller named a kind, across EVERY kind at that task when it did
// not. Two kinds can share one gate task (stage 4b1 Task 3) and the signal router forwards by
// TaskID alone, so "is any round at this task still open" is exactly the question the child
// will answer when the signal arrives.
func roundsAtTask(row projectstate.ActivityExecution, taskID string, kind *projectstate.ArtifactKind) []projectstate.ReviewRound {
	var out []projectstate.ReviewRound
	for _, r := range row.Reviews {
		if string(r.TaskID) != taskID {
			continue
		}
		if kind != nil && roundGateKey(r.TaskID, r.ArtifactKind) != roundGateKey(projectstate.MethodTask(taskID), kind) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// maxOperatorNoteRunes caps one operator note — its text plus its anchored comments'
// text and JSONPaths — at the façade (plan B1.4): a send-back's feedback and an
// override's notes are persisted on the activity and carried to the next agent attempt.
const maxOperatorNoteRunes = 4000

// operatorNoteRunes counts a note's characters: its text, and each anchored comment's
// text AND JSONPath (M6), since the rendered note carries both.
func operatorNoteRunes(notes string, comments []AnchoredComment) int {
	n := utf8.RuneCountInString(notes)
	for _, c := range comments {
		n += utf8.RuneCountInString(c.Text) + utf8.RuneCountInString(c.JSONPath)
	}
	return n
}

// checkOperatorNoteSize is the façade's size gate for one note, named by what: at most
// maxOperatorNoteRunes characters, and at most maxOperatorNoteBodyBytes once rendered, so
// the note always reaches the agent whole (the 16 KiB block keeps the newest note whole).
func checkOperatorNoteSize(what, notes string, comments []AnchoredComment) error {
	if err := checkNoReplyTo(what, comments); err != nil {
		return err
	}
	if operatorNoteRunes(notes, comments) > maxOperatorNoteRunes {
		return newError(fwmanager.ContractMisuse, fmt.Sprintf("%s is at most %d characters, anchored comments and their paths included", what, maxOperatorNoteRunes))
	}
	if len(renderNoteBody(notes, noteComments(comments))) > maxOperatorNoteBodyBytes {
		return newError(fwmanager.ContractMisuse, fmt.Sprintf("%s is at most %d bytes once rendered (anchored comments included); shorten it", what, maxOperatorNoteBodyBytes))
	}
	return nil
}

// checkNoReplyTo REFUSES a replyTo on a door that cannot route one — the surviving twin of
// pdCheckNoReplyTo, restored for the three OPERATOR-NOTE doors (an override's notes, a
// send-back note, a re-dispatch note). It lives in checkOperatorNoteSize because that is
// the ONE function all three already share, so the refusal cannot drift between them.
//
// WHAT WAS SILENTLY LOST. Every one of those three doors funnels its comments through
// noteComments, which builds projectstate.NoteComment{JSONPath, Text} — a shape with NO
// ReplyTo member at all. A reviewer who replied inside a thread and sent it as a send-back
// note therefore had their reply re-filed as a fresh, detached, flat note: the anchor
// survived, the conversation it answered did not. That is exactly the loss design §3.7
// exists to prevent, and it is worse than the Phase-2 case the surviving check refuses,
// because here the reader sees a comment that LOOKS filed.
//
// REFUSE RATHER THAN ROUTE, and the reason is structural rather than a preference. These
// doors write to the OPERATOR NOTE ledger, which is a flat delivery queue carried into the
// next dispatch — it has no threads, so there is no thread for a reply to land in. Routing
// would need NoteComment to gain a reply identity AND the note ledger to gain threads,
// which is a contract change (.aiarch/state/project.json), not a guard. A loud
// ContractMisuse naming the offending id is the honest answer until that exists; the
// reviewer can fold the reply's text into the note, which is what the SPA already does for
// the Phase-2 refusal.
func checkNoReplyTo(what string, comments []AnchoredComment) error {
	for _, c := range comments {
		if c.ReplyTo != "" {
			return newError(fwmanager.ContractMisuse,
				what+" cannot carry a threaded reply: it is filed as an operator note, and the note ledger has no thread for a reply to land in — "+
					"fold the reply's text into the comment instead (offending replyTo: "+c.ReplyTo+")")
		}
	}
	return nil
}

// activitySession reads one activity's session through the SAME Query GetSessionState
// serves, with its error mapping: no session is NotFound, any other query fault is
// Infrastructure.
func (m *constructionManager) activitySession(ctx context.Context, projectID ProjectID, activityID ActivityID) (ConstructionSessionView, error) {
	return m.GetSessionState(fwmanager.Context{Context: ctx}, projectID, &activityID)
}

// sessionStageName is a ConstructionStage's wire word, for refusal messages. A free
// function so the generated enum stays pure data (same rule as overrideKindName).
func sessionStageName(s ConstructionStage) string {
	switch s {
	case StageDispatching:
		return "dispatching"
	case StagePipelineRunning:
		return "pipelineRunning"
	case StageReviewing:
		return "reviewing"
	case StageAwaitingTakeover:
		return "awaitingTakeover"
	case StagePaused:
		return "paused"
	case StageExited:
		return "exited"
	case StageAwaitingApproval:
		return "awaitingApproval"
	case ConstructionStageUnknown:
		return "unknown"
	}
	return "unknown"
}

// SetReviewPolicy — op 2.8 (local-merge-and-policy Commit 2). Sets the project's
// review-policy PRESET (the Task-7 sophistication dial: vibes / checkpoints /
// full) while PRESERVING the committed GatedPhasesByType map (UpdateReviewPolicy's
// surface — the two ops write disjoint halves of the same ReviewPolicy).
//
// The preset is validated HERE, at the write path: the reviewEngine's read path
// (ProposeReviews, keyed off this policy's Preset) deliberately treats an
// unrecognized preset as the legacy explicit-map fallback, and with an empty map
// that gates NOTHING — the documented fail-open corner. A
// closed write vocabulary (rejecting unknowns as ContractMisuse) is what keeps a
// typo'd preset from silently degrading a project to "gate nothing".
func (m *constructionManager) SetReviewPolicy(rc fwmanager.Context, projectID ProjectID, preset string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	switch preset {
	case projectstate.ReviewPresetVibes, projectstate.ReviewPresetCheckpoints, projectstate.ReviewPresetFull:
		// closed vocabulary — fall through to the write.
	default:
		return newError(fwmanager.ContractMisuse, fmt.Sprintf("unknown review-policy preset %q (want %q, %q, or %q)",
			preset, projectstate.ReviewPresetVibes, projectstate.ReviewPresetCheckpoints, projectstate.ReviewPresetFull))
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isRANotFound(err) {
			return newError(fwmanager.NotFound, err.Error())
		}
		return newError(fwmanager.Infrastructure, err.Error())
	}
	policy := proj.ReviewPolicy
	policy.Preset = &preset
	if _, err := m.constructionTransition.RecordReviewPolicy(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID), proj.Version, policy, projectstate.RepoCredential{}, fwra.IdempotencyKey(uuid.NewString())); err != nil {
		return newError(fwmanager.Infrastructure, err.Error())
	}
	return nil
}

// isRANotFound reports whether err is (or wraps) a ResourceAccess NotFound —
// the read path's "no such project" signal, mapped to the façade's own NotFound
// so the transport answers 404 rather than 500.
func isRANotFound(err error) bool {
	var fe *fwra.Error
	if errors.As(err, &fe) {
		return fe.Kind == fwra.NotFound
	}
	return false
}

// UpdateReviewPolicy — op 2.7. Persists the per-project ReviewPolicy.
// Converts the input's GatedPhasesByType (map[string][]string of ad-hoc or canonical
// gate ids) via projectstate.ReviewPolicyFromGateIDs to a typed ReviewPolicy, reads the
// current project version, then calls RecordReviewPolicy on the constructionTransition RA.
func (m *constructionManager) UpdateReviewPolicy(rc fwmanager.Context, projectID ProjectID, input ReviewPolicyInput) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return newError(fwmanager.Infrastructure, err.Error())
	}
	policy := projectstate.ReviewPolicyFromGateIDs(input.GatedPhasesByType)
	if _, err := m.constructionTransition.RecordReviewPolicy(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID), proj.Version, policy, projectstate.RepoCredential{}, fwra.IdempotencyKey(uuid.NewString())); err != nil {
		return newError(fwmanager.Infrastructure, err.Error())
	}
	return nil
}

// --- workflow id derivation (continuity tokens; constructionManager.md §6.1) ---

// pumpWorkflowID derives the project's ONE pump workflow id, {projectId}:nextActivity.
// It is the single derivation every pump entry uses — ExecuteNextActivity (Begin /
// MCP) and PumpSweepWorkflow (the 30s Schedule fan-out) — so no two entries can start
// competing pumps over the same dependency frontier (architect pump ruling,
// 2026-09-12). Deliberately tick-invariant: a tick/firing id in the id would give each
// caller its own pump.
func pumpWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:nextActivity", projectID)
}

// replanSweepWorkflowID derives {projectId}:replanSweep:{tickId} or, for the
// all-projects sweep, :all:replanSweep:{tickId}.
func replanSweepWorkflowID(projectID *ProjectID, tickID string) string {
	if projectID == nil {
		return fmt.Sprintf(":all:replanSweep:%s", tickID)
	}
	return fmt.Sprintf("%s:replanSweep:%s", *projectID, tickID)
}

// roundSweepWorkflowID derives the per-project round-sweep child id
// {projectId}:roundSweep:{tickId}. Deliberately TICK-BEARING, the opposite of
// pumpWorkflowID's choice and for the opposite reason: two pumps over one frontier would
// race each other, whereas two sweep ticks are not redundant — the later one reads the
// later ledger — so each firing gets its own child and the already-started tolerance
// collapses only a double firing of the SAME tick.
func roundSweepWorkflowID(projectID ProjectID, tickID string) string {
	return fmt.Sprintf("%s:roundSweep:%s", projectID, tickID)
}

// deliveryActivityWorkflowID derives the GENERIC per-activity child's id
// {projectId}:activity:{activityId} (stage 4b1 Task 8).
//
// The ":activity:" segment is deliberate and is not decoration: the retired child's id is
// {projectId}:{activityId}, so reusing that shape would make the new child collide with an
// in-flight old one on the same activity — Temporal answers AlreadyStarted and the pump
// reads a dispatch that silently did nothing. A distinct segment lets both ids coexist while
// both types are registered.
func deliveryActivityWorkflowID(projectID ProjectID, activityID ActivityID) string {
	return fmt.Sprintf("%s:activity:%s", projectID, activityID)
}

// pauseTargetWorkflowID derives the project-level pump workflow id pause/sweep
// signals + the project-level session query address. The pause Signal targets the
// project's in-flight construction execution; the project-level pump id is the
// stable continuity token for the project's supervision.
func pauseTargetWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:construction", projectID)
}

// --- error mapping at the façade boundary (constructionManager.md §3.5) -------

func csMapStartError(err error) error {
	// A "workflow already started" race under UseExisting policy is benign; any
	// other error is treated as an infrastructure fault at the transport layer.
	return newError(fwmanager.Infrastructure, err.Error())
}

func csMapQueryError(err error) error {
	if isNotFound(err) {
		return newError(fwmanager.NotFound, err.Error())
	}
	// Failing-workflow-task hygiene (mirrors the design managers): a session being
	// retried after a deploy-time fault rejects queries with raw Temporal internals
	// ("Unable to query workflow due to Workflow Task in failed state") — clients
	// get a clean, actionable Detail instead.
	if strings.Contains(err.Error(), "Workflow Task in failed state") {
		return newError(fwmanager.Infrastructure,
			"construction session state is temporarily unavailable — the session hit an internal fault and is being retried by the server; try again shortly")
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// overrideKindName returns the canonical name for an override kind. Kept as a FREE
// FUNCTION (not a method) so the generated OverrideKind scalar carries no behavior
// (the contract surface is pure data).
func overrideKindName(k OverrideKind) string {
	switch k {
	case OverrideUnknown:
		// zero-value sentinel, not a real override kind.
		return "Unknown"
	case OverrideTakeover:
		return "Takeover"
	case OverrideRetry:
		return "Retry"
	case OverrideSkip:
		return "Skip"
	case OverrideReassign:
		return "Reassign"
	}
	// Unreachable for the five defined OverrideKind values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a
	// defensive fallback for an out-of-range ordinal.
	return "Unknown"
}

// ---------------------------------------------------------------------------
// Façade error model (constructionManager.md §3.5).
// CALLER/PROGRAMMER errors at the façade boundary — distinct from the workflow's
// own failure handling (Temporal RetryPolicy + the intervention/variance
// alternative paths inside the workflow body). Kinds used: ContractMisuse,
// FailedPrecondition, NotFound, Unauthorized, Infrastructure.
// ---------------------------------------------------------------------------

// deps.go declares the hand-written domain VALUE types the Manager's workflow
// vocabulary uses. Per the founder DI model (2026-06-28) the constructionManager's
// GENERATED constructor (contract.gen.go: NewConstructionManager) takes the
// dependencies' PUBLISHED interfaces directly. The two Engines (intervention /
// review) are typed as their PUBLISHED contract interfaces DIRECTLY on
// wfDeps/csWorkflows (workflow.go) — no Manager-local seam interface, no adapter (Task 6).
//
// B8 (custom activities → generated, clean cut) + its follow-up removed EVERY
// Manager-local RA seam that used to live here:
//   - constructionTransitionAccess (10 ops) and gitActivityStatusAccess (6 ops): every
//     write verb is reached through the GENERATED invoker surface (invokers.gen.go:
//     genInvokers.ConstructionTransition* / genInvokers.GitStatus*). wfDeps/csWorkflows
//     carry no ConstructionTransition field; GitStatus survives as a plain
//     projectstate.GitActivityStatusAccess-typed field whose ONLY remaining role is
//     the nil-check "is the per-activity git head-state mirror wired" feature flag
//     (gitforward.go's gitEnabled/startedCred), never a direct method call.
//   - projectStateReader (the whole-aggregate read seam behind the last custom
//     Activity, ReadProjectActivity): GONE in the B8 follow-up. The shared
//     projectstate.ProjectEnvelope (envelope.go) was extended with the three
//     construction-fidelity sections the pump reads (ActivityConstruction /
//     ServiceContracts / ReviewPolicy), so the read now rides the GENERATED
//     designSessionAccess.readProjectOnBranch invoker with branch "" (main) and the
//     Manager's former local codec (codec.go) + activities_custom.go are deleted.
//     Construction now has NO custom Temporal Activities at all.
//
// How each dependency kind is reached differs by determinism class:
//   - the two Engines (intervention.InterventionEngine / review.ReviewEngine) are
//     PURE, deterministic, called DIRECTLY in-workflow (no Activity wrapper —
//     replay-safe) with fweng.Context{Context: context.Background()} supplied inline
//     at each call site (workflow.go / signals.go);
//   - the ResourceAccess ports are I/O and reached EXCLUSIVELY through the generated
//     invoker surface (Acts — invokers.gen.go/activities.gen.go).

// constructionActivity is the by-value activity snapshot the Manager's own workflow
// vocabulary uses broadly (eligibility.go, gitforward.go, dispatch) — CRLabel/IsRevert
// are the git-forward per-activity facts threaded into the PR open + the head-state
// mirror, and Phases is the resolved per-activity phase profile. Kind is the
// Manager-owned activityKind (Construction vs Noncoding), fed to activityKindName for
// the PR-body text.

// activityKind classifies a construction activity for display / PR-body purposes
// (Construction vs Noncoding). It was formerly the published handoff.ActivityKind;
// with the handOffEngine removed (agent-class selection is now review policy, not a
// worker-class cast) the Manager owns this small enum. The ordinal set is preserved
// (Unknown=0 … Noncoding=4) so the Temporal-payload wire form is unchanged.
type activityKind int

const (
	activityKindUnknown activityKind = iota
	activityKindDetailedDesign
	activityKindConstruction
	activityKindIntegration
	activityKindNoncoding
)

type constructionActivity struct {
	ActivityID   string
	Kind         activityKind
	ComponentID  string
	Layer        string
	EstimateDays float64
	CRLabel      string
	IsRevert     bool
	Phases       []projectstate.ActivityMethodPhase
	// Type/Variant are the classification the pump resolved ONCE
	// (projectstate.ClassifyActivity) at selection time, which every downstream
	// consumer then CARRIES rather than re-deriving: the Phases above, the review
	// policy's GatedPhasesByType key (activityTypeName), the head-state Type/Variant
	// stamp (RecordActivityStarted) and the per-phase slash command (CommandFor).
	// Classify once, carry the result — a second derivation is a second chance to
	// disagree, which is exactly how a phase profile and its commands drifted apart.
	// The zero pair (Service, Plan) is what a legacy Temporal payload decodes to,
	// preserving pre-change behavior for a workflow already in flight.
	Type    projectstate.ActivityType
	Variant projectstate.TestingVariant
}

// activityTypeName returns the canonical activity-type wire name
// ("service"/"frontend"/"testing"/…) for the activity's STAMPED type. These are the
// exact keys the ReviewPolicy's GatedPhasesByType map is keyed by (and the keys the
// webApp PolicyPanel must emit) — the gate consults the reviewEngine's
// ProposeReviews(activityTypeName(), phase, …), reading its RequiresHuman
// verdict. It reads the stamped Type rather than re-deriving from the id: re-deriving
// would let the gate map be keyed by a different type than the phases being walked.
func (a constructionActivity) activityTypeName() string {
	return a.Type.String()
}

// ===========================================================================
// constructionPipeline value vocabulary — the Manager's infrastructure-neutral
// dispatch spec / handle / observation. The pipeline ops are GENERATED and reached
// through the generated invoker surface (genInvokers.Pipeline*); these neutral types
// feed the workflow-side composition/mapping helpers (workflow.go) that bridge to the
// contract agenticjob.PipelineSpec / PipelineHandle / PipelineObservation.
// ===========================================================================

// pipelineHandle is the Manager's opaque handle.
type pipelineHandle struct {
	Name string
}

// ===========================================================================
// constructionInterventionPolicy resolves the composition-root's raw
// interventionMode STRING config into the published intervention.InterventionPolicy.
// ===========================================================================

func constructionInterventionPolicy(mode string) intervention.InterventionPolicy {
	switch mode {
	case "escalate-everything", "escalateEverything", "supervised":
		return intervention.InterventionPolicy{Mode: intervention.EscalateEverything}
	default:
		return intervention.InterventionPolicy{Mode: intervention.Tiered, RetryBudget: 2}
	}
}

// eligibility.go holds the pump's PURE eligibility selection over committed head-state
// (constructionManager.md §6.3 step 1) — the Manager's own workflow-side selection logic,
// deterministic and replay-safe (called directly in-workflow via the injected
// NextEligibleActivity helper). It was folded out of adapters.go so adapters.go carries
// only the engine boundary adapters; none of this touches Temporal or any RA seam.

// pumpVerdict is the pump's three-state selection outcome. It replaces the former
// (activity, bool) pair, whose false arm conflated "the network is drained" with
// "this activity cannot be dispatched" — the conflation that let a stalled network
// masquerade as a quiescent one for a whole benchmark run.
type pumpVerdict int

const (
	verdictQuiescent pumpVerdict = iota
	verdictDispatch
	verdictBlocked
)

// pumpSelection carries the verdict plus whichever payload it implies: the hydrated
// activity on verdictDispatch, the offending id + operator-facing reason + the
// discriminating FailureReason on verdictBlocked, nothing on verdictQuiescent.
// BlockedFailureReason picks the repair CLASS (componentId vs. dangling dependency
// id vs. dependency cycle); BlockedReason is the human-readable detail WITHIN that
// class — the governing rule is one variant per repair class, detail discriminates
// instances, never classes.
// (SkippedDesign is GONE, with stage 4b1 Task 10. It named every design activity the scan
// walked past on a tick — eligible work the pump could classify and would not dispatch —
// and it existed only so a project whose remaining work was all design did not read as an
// unexplained quiet tick. The pump dispatches those three now, so there is nothing left
// for it to report having declined.)
type pumpSelection struct {
	Activity             constructionActivity
	Verdict              pumpVerdict
	BlockedActivityID    string
	BlockedReason        string
	BlockedFailureReason projectstate.FailureReason
}

// eligibilityRule is which activities the pump's selection may pick. It is chosen by the
// pump's GetVersion(changeLedgerPartialResume) — never by the selection itself — so a
// pump replaying a history recorded under the old rule re-selects exactly what it chose
// then (architect (D), D.2).
type eligibilityRule int

const (
	// eligibleNotStarted is the pre-D1 rule: only an activity whose effective state is
	// NotStarted (isActivityNotStarted).
	eligibleNotStarted eligibilityRule = iota
	// eligibleDispatchable is architect (D), D.1.2: also an integration-pending row, one no
	// pump wrote whose ledger holds some phases complete (isActivityDispatchable).
	eligibleDispatchable
	// eligibleWithDesign additionally admits the THREE DESIGN ACTIVITIES (stage 4b1 Task
	// 10). It is its own rung, behind its own change id, because the rule genuinely changes
	// which activity a tick picks on state that already exists: this repo's own committed
	// slot 9 opens with requirements/architecture/projectDesign and none of the three has an
	// execution row, so a recorded pump history that walked past them and dispatched a
	// construction activity would, under the new rule, select `requirements` (declaration
	// index 0) instead — a DIFFERENT child id, which is a non-determinism error on replay.
	// The same reason changeLedgerPartialResume is version-gated.
	eligibleWithDesign
)

// admitsDesignActivities reports whether this rule lets the pump pick one of the three
// design activities. Read in TWO places — the phase gate and the scan — because the design
// activities' phase floor is their OWN (a `requirements` activity runs while the project is
// still in Phase 1), so the blanket PhaseConstruction gate cannot stand for them.
func (r eligibilityRule) admitsDesignActivities() bool { return r == eligibleWithDesign }

// isDesignLifecycle reports whether this activity type is one of the THREE whose lifecycle
// produces a design artifact — a Phase-1 slot or the Phase-2 plan — rather than a commit.
//
// It answered a second question until stage 4b1 Task 11 ("which child walks this?", as
// runsOnTheDeliveryChild) and no longer does: ONE child walks every lifecycle now, so the only
// live question is the PHASE FLOOR, which is admissibleInPhase's and is genuinely a property of
// what the activity produces — `requirements` and `architecture` write Phase-1 slots and
// `projectDesign` writes the Phase-2 plan, so requiring PhaseConstruction of them would require
// the output before the work.
//
// It asks the TYPE rather than the id, because the id table lives in projectstate and the
// three types are what railFor already reads; and it is exhaustive over ActivityType, so a
// new type must decide where its floor is rather than inheriting an answer.
func isDesignLifecycle(typ projectstate.ActivityType) bool {
	switch typ {
	case projectstate.ActivityTypeRequirements,
		projectstate.ActivityTypeArchitecture,
		projectstate.ActivityTypeProjectDesign:
		return true
	case projectstate.ActivityTypeService,
		projectstate.ActivityTypeFrontend,
		projectstate.ActivityTypeTesting,
		projectstate.ActivityTypeDeployment,
		projectstate.ActivityTypeDocumentation,
		projectstate.ActivityTypeUIDesign,
		projectstate.ActivityTypeIntegration:
		return false
	}
	return false
}

// changeLedgerPartialResume is the ONE change id guarding D1 in both csWorkflows: the pump's
// widened selection and the construct workflow's ledger-aware start seed. A v1 pump only
// ever starts a v1 child, so every execution is wholly old or wholly new.
const changeLedgerPartialResume = "ledger-partial-resume"

// nextEligibleActivity resolves the next eligible construction activity for a project
// from its head-state. An activity is eligible iff the rule admits it (eligibleUnder) and
// every dep is satisfied, both read through projectstate.EffectiveConstructionPhase (the
// stored state where the pump wrote it, the attempt ledger where it did not) — an activity
// dependency requires a Done record, a milestone dependency is satisfied DERIVEDLY (it
// never has a Done record of its own; see projectstate.AllDepsSatisfied /
// projectstate.MilestonesByID). The dependency rule gates the activity's whole remaining
// lifecycle: a row resuming at Integration waits on exactly what a fresh row would.
// Iteration is ActivityList declaration order; the first eligible activity in that order
// is chosen (the candidate-list name tie-break below is currently unreachable, since
// declIdx is already unique per activity).
func nextEligibleActivity(proj projectstate.Project, rule eligibilityRule) pumpSelection {
	// Committed Network+ActivityList alone are not authorization to BUILD: the Phase-2 seal
	// (M0's approve — every plan slot committed, the SDP review approved) is what moves the
	// project into PhaseConstruction. Selecting construction work before that would start
	// building on an unvalidated project design.
	//
	// THE GATE IS NOW PER ACTIVITY (stage 4b1 Task 10), and that is the whole of what the
	// rule change buys: the three DESIGN activities are precisely the work that runs BEFORE
	// the seal — `requirements` and `architecture` in Phase 1, `projectDesign` in Phase 2 —
	// so a blanket construction-only gate made them permanently unselectable and left Task
	// 9's deterministic Project Design inert. A design activity is admitted in any phase; a
	// construction activity still waits for the seal. The chain's ORDER is not this gate's
	// business and never was: slot 10 carries requirements → architecture → projectDesign
	// → M0 → everything else, and AllDepsSatisfied is what reads it.
	if proj.Phase != projectstate.PhaseConstruction && !rule.admitsDesignActivities() {
		return pumpSelection{Verdict: verdictQuiescent}
	}
	network, activityList, ok := committedPlanInputs(proj)
	if !ok {
		return pumpSelection{Verdict: verdictQuiescent}
	}

	// itemByName is both the ActivityItem lookup AND the membership set of authored
	// activity names (the two ideas share exactly one key set, so one map serves both:
	// projectstate.ResolveDependencySatisfied/AllDepsSatisfied below only ever probe it for
	// presence via `_, isActivity := itemByName[depID]`).
	itemByName := make(map[string]projectstate.ActivityItem, len(activityList.Activities))
	for _, item := range activityList.Activities {
		itemByName[item.Name] = item
	}

	depsByActivity := make(map[string][]string, len(network.Dependencies))
	for _, dep := range network.Dependencies {
		depsByActivity[dep.Activity] = dep.DependsOn
	}

	milestones := projectstate.MilestonesByID(network)

	type candidate struct {
		declIdx  int
		activity string
	}
	var candidates []candidate
	// problemActivityID/problemReason/problemKind capture the FIRST authored-dependency
	// defect (an id naming neither an activity nor a milestone, or a milestone cycle)
	// encountered while scanning in declaration order — deterministic, since
	// activityList.Activities is an authored slice, never a map. It is used ONLY as
	// a fallback explanation when nothing else is eligible this tick (below): a
	// defect on an activity that ISN'T currently blocking progress must not halt
	// otherwise-dispatchable work, but a defect that WOULD otherwise present as an
	// ordinary quiet tick must not go unreported — that silent-quiescent disguise is
	// exactly the failure mode this change closes for milestone dependencies.
	var problemActivityID, problemReason string
	var problemKind projectstate.FailureReason
	for i, item := range activityList.Activities {
		name := item.Name
		if !eligibleUnder(rule, name, item, proj.ActivityExecution) {
			continue
		}
		if !admissibleInPhase(proj.Phase, rule, name, item) {
			continue
		}
		res := projectstate.AllDepsSatisfied(depsByActivity[name], itemByName, proj.ActivityExecution, milestones)
		if res.ProblemReason != "" {
			if problemReason == "" {
				problemActivityID, problemReason, problemKind = name, res.ProblemReason, res.ProblemKind
			}
			continue
		}
		if !res.Satisfied {
			continue
		}
		candidates = append(candidates, candidate{declIdx: i, activity: name})
	}
	if len(candidates) == 0 {
		if problemReason != "" {
			return pumpSelection{
				Verdict:              verdictBlocked,
				BlockedActivityID:    problemActivityID,
				BlockedFailureReason: problemKind,
				BlockedReason: fmt.Sprintf(
					"activity %s: %s — terminally failed; amending the committed network alone will NOT restart it (RecordActivityFailed is sticky and there is no reopen/retry path)",
					problemActivityID, problemReason),
			}
		}
		return pumpSelection{Verdict: verdictQuiescent}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].declIdx != candidates[j].declIdx {
			return candidates[i].declIdx < candidates[j].declIdx
		}
		return candidates[i].activity < candidates[j].activity
	})

	chosen := candidates[0].activity
	return dispatchSelectionFor(proj, chosen, itemByName[chosen])
}

// admissibleInPhase is the PER-ACTIVITY phase floor the blanket construction-only gate
// became (stage 4b1 Task 10).
//
// Under the two pre-4b1 rules it is the old behaviour exactly: the caller already refused
// every non-construction phase, and a design activity is walked past silently — the pump has
// no child for it under those rules, and a recorded history that skipped it must keep
// skipping it. (That skip used to be REPORTED, in pumpSelection.SkippedDesign; the report is
// gone with the skip's reason, and the version gate is what keeps the skip itself alive for
// the histories that recorded it.)
//
// Under eligibleWithDesign: a DESIGN activity is admitted in any phase, because its phase
// floor is the phase it produces — `requirements` and `architecture` write Phase-1 slots and
// `projectDesign` writes the Phase-2 plan, so requiring PhaseConstruction of them is
// requiring the output before the work. A CONSTRUCTION activity still requires the seal.
//
// An UNCLASSIFIABLE activity is admitted rather than skipped, deliberately: dispatchSelectionFor
// is where a plan defect becomes verdictBlocked with its repair named, and swallowing it here
// would turn a reportable defect back into a silent quiet tick.
func admissibleInPhase(phase projectstate.Phase, rule eligibilityRule, name string, item projectstate.ActivityItem) bool {
	typ, _, err := projectstate.ClassifyActivity(name, item.WorkerClass, item.Coding)
	if err != nil {
		return true
	}
	if !isDesignLifecycle(typ) {
		return phase == projectstate.PhaseConstruction
	}
	return rule.admitsDesignActivities()
}

// dispatchSelectionFor resolves the CHOSEN activity into its dispatchable selection:
// authored component identity, then classification. Both failure arms are plan
// defects that block terminally rather than dispatch a guess. Folded out of
// nextEligibleActivity so the eligibility scan and the chosen-activity resolution
// each stay under the complexity gate on their own.
func dispatchSelectionFor(proj projectstate.Project, chosen string, item projectstate.ActivityItem) pumpSelection {
	// Component identity is AUTHORED (spec §2.5): its presence declares the activity
	// structural, its absence declares it nonstructural or noncoding. ServiceContracts
	// play NO part in selection — requiring one was the chicken-and-egg that stalled
	// every fresh project, since the contract is produced by the detailed-design PHASE
	// of the very activity being selected.
	var comp *projectstate.Component
	if item.ComponentID != "" {
		comp = lookupComponent(proj, item.ComponentID)
		if comp == nil {
			return pumpSelection{
				Verdict:              verdictBlocked,
				BlockedActivityID:    chosen,
				BlockedFailureReason: projectstate.ComponentUnresolved,
				BlockedReason: fmt.Sprintf(
					"activity %s names component %q, which is not in the committed systemDesign — terminally failed; amending the committed activityList alone will NOT restart it (RecordActivityFailed is sticky and there is no reopen/retry path)",
					chosen, item.ComponentID),
			}
		}
	}
	// Classification is resolved HERE, once, from the three facts the committed plan
	// authored (id, workerClass, coding) — and the resolved pair then rides the
	// dispatched constructionActivity to every consumer. An activity the rules cannot
	// classify has no phase profile and no slash command, so dispatching it would mean
	// guessing: the id-prefix guess is what handed infra activity N-ENV a testing
	// command and killed it with VarianceExhausted. Block instead, as a plan defect.
	typ, variant, cerr := projectstate.ClassifyActivity(chosen, item.WorkerClass, item.Coding)
	// (A design activity used to take its own quiescent arm HERE, because rule 0 classified
	// it and then refused it: reaching this line meant some path had chosen an activity the
	// scan walks past, and going quiet was the only safe answer — verdictBlocked writes the
	// sticky RecordActivityFailed and would have terminally failed the very activity stage 4
	// exists to run. Stage 4b1 Task 10 gave the three a child, so they classify cleanly and
	// dispatch through the same arm as everything else.)
	if cerr != nil {
		return pumpSelection{
			Verdict:              verdictBlocked,
			BlockedActivityID:    chosen,
			BlockedFailureReason: projectstate.ActivityUnclassifiable,
			BlockedReason: fmt.Sprintf(
				"activity %s has workerClass %q with coding=%v, which matches no activity-classification rule, so no phase profile or construction command can be resolved for it — terminally failed; repair by amending workerClass or coding in the committed activity list (RecordActivityFailed is sticky and there is no reopen/retry path)",
				chosen, item.WorkerClass, item.Coding),
		}
	}
	return pumpSelection{Verdict: verdictDispatch, Activity: hydrateConstructionActivity(chosen, item, comp, typ, variant)}
}

// lookupComponent resolves a component id against the committed systemDesign by EXACT
// id match. Returns nil when the slot is uncommitted/unpopulated or no component has
// that id — both are the caller's blocked case. No normalization, no name matching:
// the authored id is the identity (spec §2.1).
func lookupComponent(proj projectstate.Project, id string) *projectstate.Component {
	if proj.SystemDesign.Status != projectstate.ReviewCommitted {
		return nil
	}
	sys, ok := proj.SystemDesign.Model.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	for i := range sys.Components {
		if sys.Components[i].ID == id {
			return &sys.Components[i]
		}
	}
	return nil
}

// committedPlanInputs returns the committed typed Network + ActivityList head-state
// models the eligibility selection reads, or ok=false when either slot is not committed
// or not populated — the pump then has nothing to select.
func committedPlanInputs(proj projectstate.Project) (*projectstate.Network, *projectstate.ActivityList, bool) {
	if proj.Network.Status != projectstate.ReviewCommitted {
		return nil, nil, false
	}
	network, ok := proj.Network.Model.(*projectstate.Network)
	if !ok || network == nil {
		return nil, nil, false
	}
	if proj.ActivityList.Status != projectstate.ReviewCommitted {
		return nil, nil, false
	}
	activityList, ok := proj.ActivityList.Model.(*projectstate.ActivityList)
	if !ok || activityList == nil {
		return nil, nil, false
	}
	return network, activityList, true
}

// eligibleUnder applies the pump's eligibility rule to one activity.
//
// THE RULES ARE CUMULATIVE, and the test must say so rather than name one rung: eligibleWithDesign
// (stage 4b1 Task 10) adds the design admission ON TOP of D1's widened row rule, so a
// `rule == eligibleDispatchable` equality test silently demoted the newest rule to the PRE-D1
// selection — measured, as Test_Pump_IntegrationPendingRow_DispatchesOnlyItsIntegration picking
// the not-started activity over the integration-pending one.
func eligibleUnder(rule eligibilityRule, activityID string, item projectstate.ActivityItem, status map[string]projectstate.ActivityExecution) bool {
	if rule >= eligibleDispatchable {
		return isActivityDispatchable(activityID, item, status)
	}
	return isActivityNotStarted(activityID, item, status)
}

// isActivityDispatchable is the D1 eligibility (architect (D), D.1.2): the activity has
// no construction row, or no pump wrote its row (projectstate.PumpWroteRow is false) and
// its effective state is NotStarted or Running. A row no pump wrote that reads Running is,
// by construction, a ledger-partial row: its recorded phases are complete except some it
// has not run, so it is resumed at its first incomplete phase (loadReviewSnapshot's
// ledger-aware seed). A pump-written row never qualifies — RecordActivityStarted, the
// child's first durable write, makes PumpWroteRow true, so a row leaves this set before
// the pump can look again — and neither does a Done or Failed one.
func isActivityDispatchable(activityID string, item projectstate.ActivityItem, status map[string]projectstate.ActivityExecution) bool {
	s, exists := status[activityID]
	if !exists {
		return true
	}
	if projectstate.PumpWroteRow(s) {
		return false
	}
	// A REQUEUE RE-ARMS THE ROW WHATEVER ITS LEDGER RESOLVES TO (stage 4b1, Task 12 round 3).
	// This is asked BEFORE the ledger derivation on purpose: an operator's re-open clears the
	// four head facts and KEEPS both ledgers — which is what lets the re-run seed its passed
	// tasks — so a walk whose every gate had passed resolves Done off the ledger and the pump
	// would refuse the very activity the operator just re-opened. The post-exit slot-commit
	// window (Completed, slots still AwaitingReview) did not heal in production for exactly
	// that reason. The evidence is the requeue NOTE against the last resolved attempt, so the
	// rule reads two facts the store already holds rather than a fifth head field.
	if projectstate.RequeuedAfterExit(s) {
		return true
	}
	effective, _ := projectstate.EffectiveConstructionPhase(s, item)
	return effective == projectstate.ActivityConstructionNotStarted || effective == projectstate.ActivityConstructionRunning
}

// isActivityNotStarted reports whether the activity has not started: it has no
// construction row, or its EFFECTIVE state is NotStarted. Effective, not stored:
// projectstate.EffectiveConstructionPhase lets the stored Phase win wherever the pump
// wrote it and reads the attempt ledger only where it did not, so a row whose history
// lives in the ledger alone (the backfill's rows: attempts, no stored phase fields) is
// never re-dispatched as if nothing had happened. item is the activity's committed
// ActivityItem — the ledger read needs its classification.
func isActivityNotStarted(activityID string, item projectstate.ActivityItem, status map[string]projectstate.ActivityExecution) bool {
	s, exists := status[activityID]
	if !exists {
		return true
	}
	effective, _ := projectstate.EffectiveConstructionPhase(s, item)
	return effective == projectstate.ActivityConstructionNotStarted
}

// hydrateConstructionActivity populates a constructionActivity from the activity id +
// its ActivityList item. Coding=true → Construction; Coding=false → Noncoding. comp is
// the resolved systemDesign component, or nil for a componentless (nonstructural or
// noncoding) activity — it supplies BOTH the ComponentID passed to the dispatch as
// component_id AND the Layer, which had no populator before this change and printed
// as an empty string into every PR body.
//
// typ/variant are the caller's ALREADY-RESOLVED classification (nextEligibleActivity's
// single ClassifyActivity call), stamped here and carried by every downstream consumer.
//
// IT NO LONGER STAMPS Phases (stage 4b1 Task 11). The field's only readers were the retired
// flat walk (walkPhases, and runAttempt's ProfileFor fallback for a payload that predated the
// stamp); the generic child reads the lifecycle's task DAG and never looks at it. Measured
// before removing it, because a write nobody reads and a read nobody writes are one grep apart:
// the ONLY `Phases:` producers were this line and that fallback, and `ActivityConstructionStatus.Phases`
// — the field a reader might mistake for this one — is a VIEW derivation off
// phasesToContract(resolved), which reads the profile and the attempt ledger and never the
// dispatch payload. So QueryActivityView does NOT read it, the field goes with
// ConstructActivityWorkflow in Task 13, and nothing in a view changes here.
func hydrateConstructionActivity(activityID string, item projectstate.ActivityItem, comp *projectstate.Component, typ projectstate.ActivityType, variant projectstate.TestingVariant) constructionActivity {
	kind := activityKindNoncoding
	if item.Coding {
		kind = activityKindConstruction
	}
	act := constructionActivity{
		ActivityID:   activityID,
		Kind:         kind,
		EstimateDays: item.EffortDays,
		Type:         typ,
		Variant:      variant,
	}
	if comp != nil {
		act.ComponentID = comp.ID
		act.Layer = comp.Layer.String()
	}
	return act
}

// gitactivities.go held the CUSTOM per-activity git head-state Record Activities
// (branch-open / CI-observed / arch-approved / merged / started / completed). B8
// (custom activities → generated, clean cut) migrated all six onto the GENERATED
// invoker surface (invokers.gen.go: genInvokers.GitStatus*), called directly from
// gitforward.go — the projectStateAccess §GIT-HEAD-STATE facet is now a real generated
// contract (projectstate.GitActivityStatusAccess), not a plain-goType dep temporalgen
// has no op for. This file now holds only the git-forward VALUE CARRIERS (Phase C
// folding candidates, per the task brief): the credential envelope, the PR-status
// projection, and the CI-state mapper.
//
// The PR-rail verbs (mint / OpenBranch / OpenPullRequest / GetPullRequestStatus /
// PostReview / MergePullRequest) are likewise GENERATED (activities.gen.go) and reached
// through the generated invoker surface (genInvokers.Rail*); the workflow-side value
// mapping (opaque-handle *FromString/*String marshalling, CheckState→CICheckState,
// cr-label→Hints) lives in gitforward.go.
//
// CRED OPACITY ACROSS THE RA SEAM: the rail returns a sourcecontrol.RepoCredential; the
// git head-state verbs take a projectstate.RepoCredential. These are
// structurally-identical-but-distinct opaque carriers (the NoSideways layer rule keeps
// projectstate from importing sourcecontrol — projectstate/credential.go). The Manager is
// the one seam allowed to touch both, so it converts (railCredEnvelope.toRail /
// toProjectState).

func (c railCredEnvelope) toProjectState() projectstate.RepoCredential {
	return projectstate.RepoCredential{Bytes: c.Bytes, ExpiresAt: c.ExpiresAt}
}

// ---------------------------------------------------------------------------
// git Activity option presets (constructionManager.md §6.4 pattern). Concrete
// RetryPolicy / timeout choices live here, in the Manager.
// ---------------------------------------------------------------------------

// wfDeps bundles every downstream dependency the constructionManager orchestrates,
// assembled by WorkerManifest (workermanifest.go) from the Manager's stored PUBLISHED
// deps and held on the csWorkflows struct. The three Engines are typed as their
// PUBLISHED contract interfaces (no Manager-local seam), called DIRECTLY in-workflow.
// The ResourceAccess layer is reached ENTIRELY through the generated invoker surface
// (Acts) — the whole-aggregate read included (B8 follow-up); the unit tests register
// contract-typed fakes behind the generated activity names. It is a package-internal
// builder input. There is no ProjectState/ConstructionTransition field anymore: the
// reads ride Acts.DesignSessionReadProjectOnBranch / Acts.ProjectStateReadProjectVersion
// and the cred-threaded writes ride Acts.ConstructionTransition* (B8).
type wfDeps struct {
	Intervention intervention.InterventionEngine
	Review       review.ReviewEngine

	// GitStatus is the OPTIONAL per-activity git head-state mirror (C-MCN-GIT). Its
	// writes are reached through the GENERATED invoker surface (Acts.GitStatus*); this
	// field's ONLY remaining role is the nil-check "is the mirror wired" feature flag
	// (gitforward.go's gitEnabled/startedCred) that gates the started/completed records
	// and the branch→PR→CI→+1→merge mirror.
	GitStatus projectstate.GitActivityStatusAccess

	// Acts is the GENERATED workflow-side call surface for the contract-backed RA
	// Activities (pipeline / artifact / rail); its Opts hook applies the per-op presets.
	Acts genInvokers

	// RailEnabled reports whether the PR-rail LIFECYCLE is available for construction
	// ON THIS PROJECT — the rail dep alone is not enough, and neither is a resolver:
	// the local profile binds the GitLocal sourceControlAccess AND (since the three
	// repo hooks collapsed into one) resolves every project to the deterministic
	// GitLocal RepoRef, which is a filesystem venue, not a PR-rail one. Construction
	// keeps its local-merge-job flow there, so such a boot must read as rail-dormant
	// here or runLocalMergeStep would skip and nothing would merge local activity
	// branches. It gates the PR-rail lifecycle (gitEnabled) alongside GitStatus +
	// Repo. Derived ONCE per worker by railLifecycleEnabled; never nil (csNewWorkflows
	// defaults an unwired slice to railDormant).
	RailEnabled func(projectID ProjectID) bool

	// Repo resolves the per-project RepoRef the rail verbs address. nil ⇒ the
	// PR-rail lifecycle is dormant (no repo to open branches/PRs in).
	Repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	// NextEligibleActivity resolves the next eligible construction activity for a
	// project from its head-state (the Manager's own pure selection), under the
	// eligibility rule the pump's GetVersion chose.
	NextEligibleActivity func(proj projectstate.Project, rule eligibilityRule) pumpSelection

	// InterventionPolicy is the project's committed policy snapshot the Manager feeds
	// the interventionEngine by value, typed DIRECTLY as the Engine's own published
	// input. It is resolved ONCE from the composition root's raw interventionMode config
	// via constructionInterventionPolicy (WorkerManifest() — the SAME fixed value every
	// DecideOnVariance / ApplyPausePolicy call fed under the retired per-call adapter
	// conversion).
	InterventionPolicy intervention.InterventionPolicy

	// SDPEngines are the three estimate Engines deterministic Project Design calls (stage
	// 4b1 Task 9). They reach the compute strategy through productionStrategies, not
	// through a package var: a strategy's engines are THREADED (architect ruling R9-5), so
	// a boot that wires none gets a compute that refuses by name rather than one that
	// nil-panics inside a workflow task and retries forever.
	SDPEngines sdpEngines

	// EscalationWaitTimeout bounds how long an escalated/architectOnly activity waits
	// for an operator override before it terminally FAILS the activity. 0 == wait-forever.
	EscalationWaitTimeout time.Duration
}

// csWorkflows is the single constructionManager component struct — the workflow receiver
// (it no longer hosts any Activity methods; every RA op is reached through the
// generated invoker surface, Acts).
type csWorkflows struct {
	Intervention intervention.InterventionEngine
	Review       review.ReviewEngine

	GitStatus projectstate.GitActivityStatusAccess

	Acts genInvokers

	RailEnabled func(projectID ProjectID) bool
	Repo        func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	NextEligibleActivity  func(proj projectstate.Project, rule eligibilityRule) pumpSelection
	InterventionPolicy    intervention.InterventionPolicy
	EscalationWaitTimeout time.Duration

	// Strategies is the generic child's task-strategy table (deliveryactivity.go). It is a
	// FIELD rather than a package function so a test can substitute a stub for a slot whose
	// real implementation arrives in a later task — which is how the DAG's fork/join shape
	// is proved before either dispatch implementation exists. csNewWorkflows defaults it to
	// productionStrategies(), so an unwired composition cannot read a nil map.
	Strategies strategyRegistry

	// Deliveries observes WHICH TASK each routed signal reached (walkState.deliver). Nil in
	// production, and it is an observation seam rather than behaviour because the walk's
	// TERMINAL cannot tell a delivered override from a lost one: a gate that never receives
	// one simply waits for its decision instead, so a case asserting only the terminal would
	// pass with the message gone — which is the defect the router exists to remove.
	Deliveries walkDeliveryRecorder
}

// railDormant is the RailEnabled answer of a boot with no construction PR-rail
// lifecycle at all (no rail dep, no repo resolver, or an unwired git-forward slice):
// dormant for every project. It is what csNewWorkflows substitutes for a zero
// wfDeps.RailEnabled, so the field is never nil and gitEnabled can call it blind.
func railDormant(ProjectID) bool { return false }

// csNewWorkflows builds the csWorkflows receiver from the injected seams.
func csNewWorkflows(d wfDeps) *csWorkflows {
	railEnabled := d.RailEnabled
	if railEnabled == nil {
		railEnabled = railDormant
	}
	return &csWorkflows{
		Intervention:          d.Intervention,
		Review:                d.Review,
		GitStatus:             d.GitStatus,
		Acts:                  d.Acts,
		RailEnabled:           railEnabled,
		Repo:                  d.Repo,
		NextEligibleActivity:  d.NextEligibleActivity,
		InterventionPolicy:    d.InterventionPolicy,
		EscalationWaitTimeout: d.EscalationWaitTimeout,
		// The generic child's strategy table is defaulted HERE, not read lazily, so an
		// unwired composition cannot nil-map-read its way to a walk with no dispatch. The
		// estimate Engines ride the registry constructor, which is what makes the
		// Project-Design compute testable against a substituted Engine.
		Strategies: productionStrategies(d.SDPEngines),
	}
}

// ---------------------------------------------------------------------------
// Activity option presets (constructionManager.md §6.4). Concrete RetryPolicy /
// timeout choices live here, in the Manager.
// ---------------------------------------------------------------------------

// csReadProjectActivityOptions is the read preset VALUE (10s; NotFound+ContractMisuse
// terminal) the manifest's Opts hook (workermanifest.go) applies to the two GENERATED
// read invokers the csWorkflows consume — "projectStateAccess.readProjectVersion" and
// "designSessionAccess.readProjectOnBranch" (the whole-aggregate read) — identically
// for both. NotFound stays terminal so a brand-new project's read fails fast into the
// pump's quiet-tick handling (isReadNotFound) instead of retrying.
func csReadProjectActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    10 * time.Second,
		TerminalRA: []fwra.Kind{fwra.NotFound, fwra.ContractMisuse},
	}.Options()
}

// submitPipelineActivityOptions / observePipelineActivityOptions are the pipeline preset
// VALUES the manifest's Opts hook (workermanifest.go) applies to the GENERATED pipeline
// invokers by registered name (submit 60s Auth/ContractMisuse-terminal;
// observe/cancel 30s NotFound/Auth-terminal).
func submitPipelineActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    60 * time.Second,
		TerminalRA: []fwra.Kind{fwra.Auth, fwra.ContractMisuse},
	}.Options()
}

func observePipelineActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    30 * time.Second,
		TerminalRA: []fwra.Kind{fwra.NotFound, fwra.Auth},
	}.Options()
}

// recordActivityOptions is the head-state Record-verb preset VALUE (10s; ContractMisuse
// terminal only — Conflict must reach the workflow so the §6.5 re-read→re-apply loop can
// recover it) the manifest's Opts hook (workermanifest.go) applies to the GENERATED
// constructionTransitionAccess / gitActivityStatusAccess Record* invokers by registered
// name. Every Record* verb goes through the generated invoker surface, so only the
// VALUE form is needed (no direct-ExecuteActivity call site for this preset).
func recordActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    10 * time.Second,
		TerminalRA: []fwra.Kind{fwra.ContractMisuse},
	}.Options()
}

// stampNoteDeliveredActivityOptions is the delivery stamp's preset (M4): the record
// preset's per-attempt timeout, uncapped attempts inside noteStampRetryWindow, and
// ContractMisuse terminal (the store refusing to stamp one note to two attempts).
func stampNoteDeliveredActivityOptions() workflow.ActivityOptions {
	o := recordActivityOptions()
	o.ScheduleToCloseTimeout = noteStampRetryWindow
	return o
}

// isRAContractMisuse reports whether err is (or wraps) an RA ContractMisuse.
func isRAContractMisuse(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raContractMisuseErrType
	}
	return false
}

// decodeFaultErrType names an IN-WORKFLOW envelope decode failure over committed state —
// the other half of QA F36's decode class (isRAContractMisuse covers the one the Activity
// raises). It is terminal by construction: the stored document does not type, and no retry
// changes a stored document.
const decodeFaultErrType = "ProjectEnvelopeDecodeFailed"

// isDecodeFault reports whether err is that terminal.
func isDecodeFault(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == decodeFaultErrType
	}
	return false
}

// rowAdvanced records that ONE transition applied to the activity's execution row, which
// is exactly what the store stamped on it: BOTH write paths onto a row — the facet's
// (withActivityVersion) and the retired facets' (upsertActivityExecution) — advance the
// stored counter by one per applied transition, and neither advances it on a refusal.
//
// EVERY verb that writes the row calls this, including the retired facets' verbs, which
// still write these rows for the length of the wave (RecordPhaseStarted,
// RecordChangeReviewed, RecordOperatorNote, RecordOperatorNoteDelivered all run on a
// ledger-on execution). A counter that tracked only the new rail would go stale on the
// first write from the old one, and the next CAS would then refuse a caller that is not
// stale at all — a self-inflicted conflict the re-read arm cannot resolve, because
// applyRecovering re-reads the PROJECT version and no row.
func (s *constructState) rowAdvanced() { s.activityVersion++ }

// gateLedger is the execution-ledger identity of ONE gate occurrence: the review task it
// belongs to, its 1-based number, the round id derived from the pair, the subject the
// round judges and who decided it.
//
// The number is BOTH the round's and its gate attempt's, deliberately: one gate
// occurrence is one round and one attempt at the review task, so giving them separate
// counters would let the two ledgers disagree about which review a passing gate came
// from. nextTaskAttempt is the single counter, seeded from whichever of the two ledgers
// has gone further (seedResumeFromLedger).
//
// It is passed to the round writers BY POINTER rather than read off constructState,
// because the generic child (deliveryactivity.go) runs several gates AT ONCE on a fork and
// this holds exactly one: two concurrent gates sharing constructState.gate would each
// overwrite the other's round id and decide the wrong round. The retired rail's walk is
// sequential and passes &state.gate, so its behaviour is unchanged to the byte.
type gateLedger struct {
	task    projectstate.MethodTask
	number  int
	roundID string
	subject projectstate.SubjectRef
	// actor is who passed or rejected the gate — stamped when the round is decided and
	// read back by the gate attempt the completion writes.
	actor projectstate.TaskActor
	// judgedAttemptID is the AttemptID of the work this gate judges — what every verdict on
	// the round names, which is the join that makes a verdict traceable to the work it
	// judged and to the episode that burned it. It lives HERE rather than being read off
	// constructState.workAttemptID for the same reason the rest of this struct does: on a
	// fork two gates judge two different attempts at the same moment.
	judgedAttemptID string
}

// walkRun is the generic child's per-run head-state and git lifecycle. The helpers it
// shares with the retired rail take a *projectstate.Version and a *gitForward by
// PARAMETER, and the walk cannot thread them that way — its function signatures are fixed
// by the tasks that follow it, and its dispatch strategies reach them through
// taskContext.State — so the run's copy lives here, on the state every strategy already
// holds. Zero-valued and unread on the retired rail, which threads its own by pointer.
type walkRun struct {
	// headVersion is the walk's read-your-writes token for the whole document.
	headVersion projectstate.Version
	// gf is the per-activity branch/PR lifecycle, dormant when the git slice is unwired.
	gf gitForward
	// gitOn is startedCred's answer: whether the head-state records fire at all.
	gitOn bool
	// cred is the credential minted ONCE for this activity's git lifecycle.
	cred railCredEnvelope
	// policy is the committed ReviewPolicy this run gates against, snapshotted at start
	// and NEVER re-read mid-walk — the same discipline the retired rail's parameter has.
	policy projectstate.ReviewPolicy
}

// walkTaskState is one task's position in the walk. It is WALK-LOCAL: the durable truth is the
// attempt and round ledgers, and the map is rebuilt from them on resume.
//
// ITS ORDINALS ARE PAYLOAD-VISIBLE across a ContinueAsNew (walkSnapshot.ByTask encodes
// this iota), which puts them under this repo's never-renumber rule: a new state is
// APPENDED, and re-ordering the existing five would silently re-interpret every in-flight
// walk at the moment the new image goes live. Pinned by
// Test_TaskStateOrdinalsNeverRenumber. (Serialising them as strings was the alternative
// and was rejected: it trades one pinned table for a second vocabulary to keep in step
// with the iota, and the pin is three lines.)
type walkTaskState int

const (
	walkTaskPending walkTaskState = iota
	walkTaskRunning
	walkTaskPassed
	walkTaskSentBack
	walkTaskFailed
	// walkTaskExited is APPENDED (stage 4b1 Task 11, fix round 1): the task did not pass and
	// the ACTIVITY is over, with its terminal ALREADY on the ledger — the variance loop's three
	// terminal answers (the budget exhausted, the escalation timed out, the operator skipped).
	// It is not walkTaskFailed, because failWalk would then record a SECOND terminal over the
	// first and report VarianceExhausted where the operator chose Skip; and it is not
	// walkTaskPassed, because nothing was produced. The walk stops WITHOUT a workflow error,
	// which is what the retired supervision loop did (failVarianceExhausted and
	// executeOverride's Skip arm both returned attemptDone with a nil error) and what keeps the
	// pump's cascade alive past an activity that gave up.
	walkTaskExited
)

// producedSubject is what a strategy hands back: the ref a review round will cite, the
// attempt id the ledger recorded, and the attempt's outcome. StagedRef is empty when the
// strategy staged nothing (construction's output is a commit the agent pushed, not a model
// this platform staged) and gateSubjectRef falls through its ladder accordingly.
type producedSubject struct {
	StagedRef string
	AttemptID string
	Outcome   projectstate.TaskOutcome
	Detail    string
	// EpisodeID is the agentic episode the dispatch burned, and it is the CONSTRUCTION arm's
	// evidence (stage 4b1 Task 11). The retired rail's resolveWorkAttempt cited
	// EvidenceEpisode + this id, and dropping it would leave every construction attempt in
	// the new child citing EvidenceNone — the tokens spent would be in the episode ledger
	// with nothing in the task ledger pointing at them, which is the one link the cost views
	// follow. Empty for a design task (its evidence is the STAGED model) and for a compute.
	EpisodeID string
	// AttemptRecorded says the STRATEGY already wrote this task's attempts to the ledger, so
	// runTask must not write a second record over them (stage 4b1 Task 11, fix round 1).
	//
	// The construction arm needs it because ONE lifecycle task can hold SEVERAL dispatches: the
	// variance loop re-dispatches a failed job up to maxVarianceAttempts times, each a numbered
	// attempt of its own off constructState.nextTaskAttempt, while the WALK's revision — which
	// is what tc.Attempt derives from — does not move. Leaving runTask to record would file the
	// last dispatch's outcome under the FIRST attempt's number and erase every retry from the
	// ledger, which is the one thing an operator reading a recovered activity needs to see.
	AttemptRecorded bool
	// ActivityExited says the strategy recorded the ACTIVITY's own terminal and the walk must
	// stop without recording a second one. Only the variance machinery sets it, because only it
	// knows WHICH terminal was reached — Skipped for an operator's skip, Unknown carrying
	// VarianceExhausted or EscalationTimedOut for the two give-ups — and a walk that re-derived
	// one would report the same thing for all three.
	ActivityExited bool
}

// deliveryActivityInput is the start payload for the GENERIC per-activity child.
//
// constructionActivity is REUSED UNRENAMED. It is the pump's classified activity — the
// same id, component, layer, type and variant the generic walk needs — and renaming it to
// something rail-neutral is a class-D rename touching every construction call site, which
// is not in this wave's scope.
//
// Resume is nil on a fresh start and carries the walk across a ContinueAsNew.
type deliveryActivityInput struct {
	ProjectID  ProjectID
	ActivityID ActivityID
	Activity   constructionActivity
	Resume     *walkSnapshot
}

// walkSnapshot is the walk across ContinueAsNew: exported fields, the four walk maps plus
// the undelivered messages, and nothing else. No channel (re-created), no lifecycle
// (re-resolved from the activity), no review policy (re-snapshotted), because anything
// re-derivable must be re-derived rather than carried — a snapshot that carries a
// derivable fact is a second copy to keep in step.
type walkSnapshot struct {
	ByTask   map[string]int             `json:"byTask"`
	Revision map[string]int64           `json:"revision"`
	Feedback map[string]string          `json:"feedback"`
	Produced map[string]producedSubject `json:"produced"`
	Pending  map[string][]routedSignal  `json:"pending"`
}

// The four signal kinds the router forwards. They are the message's discriminator, not a
// wire enum of their own: exactly one of routedSignal's four pointers is non-nil and the
// kind says which.
const (
	routedKindDecision = "decision"
	routedKindStatus   = "status"
	routedKindOverride = "override"
	routedKindRedraft  = "redraft"
)

// routedSignal is one message the router took off a shared signal channel and forwarded to
// ONE task's inbox. Its fields are EXPORTED because walkSnapshot carries the undelivered
// ones across a ContinueAsNew, so they must survive the data converter.
type routedSignal struct {
	Kind     string
	TaskID   string
	Decision *taskDecisionSignal
	Status   *setCommentStatusSignal
	Override *operatorOverrideSignal
	Redraft  *redraftSignal
}

// taskDecisionSignal is the taskDecision payload: a decision aimed at ONE TASK's gate
// inside the generic child. Every field the old phaseDecisionSignal carried is here, keyed
// by task instead of by lifecycle phase.
type taskDecisionSignal struct {
	// TaskID is the lifecycle task whose gate this decides. The router keys on it, so a
	// decision that names none reaches no gate at all.
	TaskID string
	// Decision is the verdict; ReviewApprove and ReviewReject are the two the gate acts on.
	Decision ReviewDecision
	// OptionID is the option an M0 approve commits. Read by Task 9's M0 handler; every
	// other gate leaves it nil.
	OptionID *OptionID
	// Feedback is the reviewer's notes and anchored comments.
	Feedback *ReviewFeedback
	// DecidedBy is the acting identity the round records. Empty falls back to the operator
	// the platform can honestly attribute a decision to.
	DecidedBy string
	// AcknowledgeStale is the reviewer confirming they judged a basis that has since moved.
	// Read by Task 12, which owns the acknowledgeStaleBasis verb.
	AcknowledgeStale bool
}

func (s *constructState) view() (ConstructionSessionView, error) {
	aid := s.activityID
	v := ConstructionSessionView{
		ProjectID:        s.projectID,
		ActivityID:       &aid,
		Stage:            s.stage,
		PipelinePhase:    s.pipelinePhase,
		ReviewSet:        s.reviewSet,
		Variance:         s.variance,
		RedraftExhausted: s.redraftExhausted,
		Attempt:          int64(s.attempt),
		AttemptBudget:    maxVarianceAttempts,
	}
	if s.reviewSetError != "" {
		e := s.reviewSetError
		v.ReviewSetError = &e
	}
	if s.awaitingGate != "" {
		gate, since := s.awaitingGate, s.awaitingSince
		v.AwaitingGate, v.AwaitingSince = &gate, &since
		if s.awaitingUntil != nil {
			until := *s.awaitingUntil
			v.AwaitingUntil = &until
		}
	}
	return v, nil
}

// operatorPauseSignal is the operatorPauseRequested payload (constructionManager.md
// §2.3). The Reason rides on the signal and is safe to log.
type operatorPauseSignal struct {
	ProjectID ProjectID
	Reason    string
}

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the constructionManager
// impl. It supplies the genWorkerManifest RegisterWorker consumes: the four workflow
// bodies under their registered names, the per-activity option-preset hook, and the
// genActivities dep threading. It also hosts the external RegisterManagerWorker
// entrypoint the composition root calls (cmd/server/main.go).
//
// B8 (custom activities → generated, clean cut) + its follow-up migrated ALL of the
// former 14 CUSTOM Activities (activities_custom.go / gitactivities.go, both since
// deleted or reduced to value carriers) onto the GENERATED invoker surface — the last
// one, the whole-aggregate ReadProjectActivity, once the shared
// projectstate.ProjectEnvelope grew the construction-fidelity sections the pump reads
// (envelope.go). Registration is now ENTIRELY automatic via the generated
// RegisterWorker (worker.gen.go), which registers every genActivities op
// unconditionally; construction has NO hand-registered Activities. This also closed a
// real pre-existing defect: since B6 dropped the CustomActivities manifest surface,
// NONE of the 14 custom Activities had been registered in production — any
// workflow.ExecuteActivity call reaching one would have failed with "unable to find
// activity type" on a real worker (the identical systemic gap billing's B7 rewire
// found for its 3 revenue-ledger ops).
//
// The two Engines (intervention / review) are called DIRECTLY in-workflow
// (deterministic, by value) and are NOT Activities; the durableExecutionAccess in-workflow
// primitives (awaitSignal / startTimer / executeChild) are the Manager's own code.

// Schedule ids + cadences (constructionManager.md §6.1; Task 7c). Namespaced with
// the manager's own name (mirroring operations' "operations:operatedStateReconcile"
// over billing's bare "shortfallSweep") since Schedule ids are namespace-global —
// a manager-scoped prefix keeps two managers from ever colliding on one.
//
// STAGE 4a RENAMED THE PREFIX construction: → delivery:, because the manager whose
// name it carries no longer exists: both sweeps now register from, and fire into, the
// ONE delivery Manager and its `delivery` task queue. The workflow TYPE names keep
// their construction* spelling (R2 — a rename there would strand in-flight
// executions); only the two Schedule ids move.
//
// AT CUTOVER THE OLD IDS MUST BE DELETED, and by hand. RegisterSchedules creates an
// absent Schedule and is a harmless no-op on a present one — it cannot MOVE a
// Schedule's task queue, and it never deletes. So `construction:pumpSweep` and
// `construction:replanSweep` survive this release as Schedules whose action targets
// the dead `construction` queue that no worker polls: a silent dead sweep, not an
// error. Worse, re-registering under the SAME id would ADOPT the old Schedule rather
// than replace it, which is precisely why the id had to change instead. Run
// `temporal schedule delete --schedule-id construction:pumpSweep` (and
// `construction:replanSweep`) BEFORE the release, then confirm with
// `temporal schedule list` — see docs/bugs/2026-09-24-stage3-rail-earmarks.md.
const (
	// scheduleIDPumpSweep is the platform-wide pump-sweep Schedule id.
	scheduleIDPumpSweep = "delivery:pumpSweep"
	// pumpSweepIntervalSecs is the pump-sweep cadence — the single tunable knob.
	pumpSweepIntervalSecs = 30

	// scheduleIDReplanSweep is the platform-wide replan-sweep Schedule id.
	scheduleIDReplanSweep = "delivery:replanSweep"
	// replanSweepIntervalSecs is the replan-sweep cadence (5m) — the single tunable knob.
	replanSweepIntervalSecs = 5 * 60

	// scheduleIDRoundSweep is the platform-wide round-sweep Schedule id (stage 4b1 Task
	// 6). It carries the delivery: prefix from the start and has no older id of its own to
	// delete, unlike the two above. The DRAIN note must still list it: it is a third
	// Schedule an operator has to account for in `temporal schedule list`.
	scheduleIDRoundSweep = "delivery:roundSweep"
	// roundSweepIntervalSecs is the round-sweep cadence (5m) — the single tunable knob.
	// Slower than the pump's 30s on purpose: a stranded round is a record that is already
	// wrong and stays wrong, so nothing degrades while it waits, and every tick reads
	// every project.
	roundSweepIntervalSecs = 5 * 60
)

// deliveryActivityOptions returns the option-preset hook the generated invokers consult for the
// contract-backed RA Activities. A name with no entry falls back to the generated
// default (invokers.gen.go). Keyed by the generated registered activity name
// (<componentKey>.<opName>), including the 14 head-state Record*/read presets
// (recordOpts / readProjectOpts's VALUE forms — recordActivityOptions /
// csReadProjectActivityOptions, workflow.go).
func deliveryActivityOptions() func(activityName string) (workflow.ActivityOptions, bool) {
	presets := map[string]workflow.ActivityOptions{
		"agenticJobAccess.submitAgenticJob":        submitPipelineActivityOptions(),
		"agenticJobAccess.observeAgenticJob":       observePipelineActivityOptions(),
		"agenticJobAccess.cancelAgenticJob":        observePipelineActivityOptions(),
		"sourceControlAccess.getInstallationToken": mintCredActivityOptions(),
		"sourceControlAccess.openBranch":           railActivityOptions(),
		"sourceControlAccess.openPullRequest":      railActivityOptions(),
		"sourceControlAccess.getPullRequestStatus": railActivityOptions(),
		"sourceControlAccess.postReview":           railActivityOptions(),
		"sourceControlAccess.mergePullRequest":     railActivityOptions(),
		// B8 (+ follow-up): re-keyed from the retired custom-Activity call sites onto
		// their generated registered names, preserving the identical timeout/retry scope.
		// designSessionAccess.readProjectOnBranch is the whole-aggregate read the pump
		// runs (branch "" ⇒ main) — the former ReadProjectActivity preset.
		"designSessionAccess.readProjectOnBranch":           csReadProjectActivityOptions(),
		"projectStateAccess.readProjectVersion":             csReadProjectActivityOptions(),
		"constructionTransitionAccess.recordChangeReviewed": recordActivityOptions(),
		"constructionTransitionAccess.recordActivityExited": recordActivityOptions(),
		"constructionTransitionAccess.recordActivityFailed": recordActivityOptions(),
		"constructionTransitionAccess.recordOperatorPaused": recordActivityOptions(),
		// (recordPhaseStarted / recordPhaseCompleted went with the retired flat walk, stage 4b1
		// review fix round 2: they were the only two presets in this map naming an activity NO
		// surviving workflow invokes, which Test_DeliveryActivityOptions_EveryInvokedActivityIsTuned
		// found and now guards. The verbs themselves survive on the deprecated facet until 4b2;
		// a preset for a call that cannot happen is configuration nobody can retire.)
		// B1.4: the operator note and its delivery stamp are head-state Record verbs.
		"constructionTransitionAccess.recordOperatorNote": recordActivityOptions(),
		// The delivery stamp has its own bounded envelope (M4): it follows a submit that
		// already dispatched the job, so it retries rather than fail the run.
		"constructionTransitionAccess.recordOperatorNoteDelivered": stampNoteDeliveredActivityOptions(),
		// C.1.4: the managed-scaffold sync before a GitHub-venue dispatch is a rail verb.
		// THE SCAFFOLD SYNC TAKES FIVE MINUTES, NOT THIRTY SECONDS (stage 4b1 Task 13, Step 5).
		//
		// THE MEASUREMENT BEHIND THE RE-TUNE. Two of the three retired rails answered for this
		// activity name and they DISAGREED: the design hooks said scaffoldSyncActivityOptions()
		// (5 min) and this one said railActivityOptions() (30 s). The divergence was INERT only
		// because mf.ActivityOptions had exactly one reader per worker and each rail's workflows
		// consulted their OWN hook — a design dispatch got 5 minutes, a construction dispatch got
		// 30 seconds, and neither ever saw the other's answer. Collapsing the three hooks into
		// this one without re-tuning silently gives EVERY dispatch the 30-second answer, and the
		// generic child now does the design work that needed the long one. F-QA2-36's addendum is
		// the incident: the 30-second deadline expired mid-loop on a torn or version-bumped repo
		// (~100 file reads plus up to a whole-tree of contents-API writes) and the sync only
		// progressed through retry-persisted writes. That is the difference between a slow
		// refresh and a failed session.
		"sourceControlAccess.syncManagedScaffold": scaffoldSyncActivityOptions(),
		// THE TWO ENTRIES THE DESIGN HOOKS OWNED, carried into the one hook. The merge the retired
		// WorkerManifest ran resolved each name against three hooks with construction last-wins,
		// so for a name only the design hooks answered, the DESIGN answer was the merged answer.
		// These are the two such names the surviving child still reaches — the Phase-1 seal's
		// advance and the design slot commit — measured by grepping the generated invoker call
		// sites after the deletion. The design hooks' other seven keys
		// (stageArtifactForReviewOnBranch, rejectArtifactOnBranchWithComments,
		// withdrawArtifactOnBranch, setReviewCommentStatusOnBranch, seedReviewCommentsOnBranch,
		// activityExecutionAccess.setReviewCommentStatus) had their ONLY workflow-side callers in
		// the retired co-author files, so an entry for them here would be a preset for a call
		// that cannot happen.
		//
		// reconcileBranchFromMain LEFT THAT LIST in the final fix wave: restoring the F80c
		// diverged-branch reconcile onto the child's merge guard gave it a workflow caller again
		// (reconcileDivergedBranch), so it needs its preset back. It is a branch MUTATION like
		// the other two, and for the same reason: ContractMisuse is terminal, Conflict is
		// deliberately NOT, because applyRecoveringOnBranch's re-read→re-apply loop is what
		// resolves a stale branch version — a Temporal retry would re-issue the same stale
		// expectedVersion forever.
		"projectStateAccess.advancePhase":                    mutateActivityOptions(),
		"designSessionAccess.commitArtifactWithProvenance":   mutateActivityOptions(),
		"designSessionAccess.reconcileBranchFromMain":        mutateActivityOptions(),
		"gitActivityStatusAccess.recordActivityBranchOpened": recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityCIObserved":   recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityArchApproved": recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityMerged":       recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityStarted":      recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityCompleted":    recordActivityOptions(),
		// SP1 capture-seam: the episode ledger append rides its OWN envelope, never a
		// business one (see appendEpisodeActivityOptions).
		"episodeAccess.appendEpisode": appendEpisodeActivityOptions(),
		// EXECUTION LEDGER (stage 3, changeExecutionLedger): the attempt and review-round
		// writes are head-state Record verbs and take the Record preset for the same
		// reason — ContractMisuse terminal, and Conflict deliberately NOT, so the §6.5
		// re-read→re-apply loop in applyRecovering is what resolves it rather than a
		// Temporal retry re-issuing the same stale expected version forever.
		"activityExecutionAccess.openActivity":          recordActivityOptions(),
		"activityExecutionAccess.recordAttemptOutcome":  recordActivityOptions(),
		"activityExecutionAccess.openReviewRound":       recordActivityOptions(),
		"activityExecutionAccess.appendReviewVerdict":   recordActivityOptions(),
		"activityExecutionAccess.decideReviewRound":     recordActivityOptions(),
		"activityExecutionAccess.recordActivityOutcome": recordActivityOptions(),
		// The ROW READ the Conflict arm makes (stage 4b1, terminalAfterRowReread) takes NO
		// entry here, DELIBERATELY: it rides the generated default, exactly as the two
		// design rails' row read has since stage 3. A preset was considered and rejected —
		// it would have changed the envelope of an existing call on two rails for no
		// measured defect, and the reason to add one does not hold: fwra carries Retryable
		// PER ERROR (framework-go manager.MapError → tagError), so the NotFound this arm maps
		// to NoActivityVersionExpectation already returns on the first attempt without any
		// NonRetryableErrorTypes entry. EARMARK: a fwra.Transient row read retries unbounded
		// under the default, here and on both design rails alike — one envelope question for
		// all three, not a thing to fix on one rail inside this task.
	}
	return func(name string) (workflow.ActivityOptions, bool) {
		o, ok := presets[name]
		return o, ok
	}
}

// railLifecycleEnabled derives wfDeps.RailEnabled: the construction PR-rail LIFECYCLE
// needs the rail dep, the per-project repo resolver, AND a resolved venue that is a
// PR-rail venue at all. The first two alone are not enough (stage 4a fix round 2): the
// ONE repo resolver the merged Manager threads into all three rails answers, on the
// "local" profile with no GitHub App catalog, with the deterministic GitLocal RepoRef —
// the DESIGN rails' local branch → PR → merge venue, and a filesystem venue that
// construction has never dispatched against. Before the three hooks collapsed into one,
// construction was simply handed nil there and read rail-dormant; a local boot that read
// RailEnabled=true instead would mint a rail credential in the construction history AND
// make runLocalMergeStep skip, so nothing would merge local activity branches.
//
// The question is therefore PER PROJECT — the resolver is what carries the answer — and
// the two non-local arms keep their pre-collapse verdicts exactly:
//
//   - a GitLocal ref (the local profile): dormant, so the local merge job owns the merge.
//   - a resolver that MISSES for this project (a cloud catalog with no repo for it):
//     enabled. The lifecycle is dormant for that project anyway (gitEnabled needs an
//     actual ref), and the local merge must NOT substitute for it on a PR-rail boot.
//   - no rail dep or no resolver at all: dormant, as before.
//
// The resolver is a pure name-as-identity derivation on both profiles, so calling it
// inside the workflow is replay-safe (gitEnabled and constructRepoTarget already do).
func railLifecycleEnabled(rail sourcecontrol.SourceControlAccess, repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)) func(projectID ProjectID) bool {
	if rail == nil || repo == nil {
		return railDormant
	}
	return func(projectID ProjectID) bool {
		ref, ok := repo(projectID)
		return !ok || !isGitLocalVenue(projectID, ref)
	}
}

// railEnabled is the construction half's OWN rail answer, derived from the deps it was
// handed. It exists so WorkerManifest and the boot-level test read the SAME derivation.
func (m *constructionManager) railEnabled() func(projectID ProjectID) bool {
	return railLifecycleEnabled(m.rail, m.repo)
}

// WorkerManifest assembles the genWorkerManifest RegisterWorker (worker.gen.go) consumes:
// the four workflow bodies under their registered names, the per-activity option-preset
// hook, and the genActivities threaded from the impl's stored published deps.
func (m *constructionManager) WorkerManifest() genWorkerManifest {
	optsHook := deliveryActivityOptions()
	wf := csNewWorkflows(wfDeps{
		Intervention: m.intervention,
		Review:       m.review,
		// GitStatus's ONLY remaining role is the "is the mirror wired" nil-check feature
		// flag (gitforward.go) — its writes are reached through Acts.GitStatus* (B8), so
		// no type-assertion onto a local seam is needed; m.gitActivityStatus already
		// speaks projectstate.GitActivityStatusAccess directly (constructionmanager.go).
		GitStatus: m.gitActivityStatus,
		Acts:      genInvokers{Opts: optsHook},
		// RailEnabled gates the PR-rail lifecycle (gitEnabled) alongside GitStatus + Repo.
		// Repo (B5) is the per-project venue resolver: a project repo retargets every
		// construction dispatch to it (aiarch-construct.yml) AND activates the branch→PR
		// rail; nothing resolved keeps the central-repo fallback + dormant rail. What the
		// resolver ANSWERS is part of the derivation (not just the runWithGitForward
		// composite) so the local GitLocal venue — the design rails' local PR rail, never
		// a construction venue — keeps runLocalMergeStep firing (wfDeps.RailEnabled doc).
		RailEnabled:           m.railEnabled(),
		Repo:                  m.repo,
		NextEligibleActivity:  nextEligibleActivity,
		InterventionPolicy:    constructionInterventionPolicy(m.interventionMode),
		EscalationWaitTimeout: m.escalationWaitTimeout,
		SDPEngines:            m.sdpEngines,
	})

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindPump, Fn: wf.PumpNextActivityWorkflow},
			{Name: executionKindReplanSweep, Fn: wf.ReplanSweepWorkflow},
			{Name: executionKindProjectSupervision, Fn: wf.ProjectSupervisionWorkflow},
			{Name: executionKindPumpSweep, Fn: wf.PumpSweepWorkflow},
			{Name: executionKindRoundSweep, Fn: wf.RoundSweepWorkflow},
			// The GENERIC per-activity child — the ONE workflow every activity in the product
			// runs on since stage 4b1 Task 13, and the only one the pump starts.
			{Name: executionKindDeliveryActivity, Fn: wf.DeliveryActivityWorkflow},
		},
		ActivityOptions: optsHook,
		Activities: genActivities{
			ProjectState:           m.projectState,
			Artifact:               m.artifact,
			Pipeline:               m.pipeline,
			Rail:                   m.rail,
			ConstructionTransition: m.constructionTransition,
			GitStatus:              m.gitActivityStatus,
			Episodes:               m.episodes,
			DesignSession:          m.designSession,
			MessageBus:             m.messageBus,
			// The execution ledger's twelve activities are registered by worker.gen.go the
			// moment the dep exists; THREADING it is what gives them something to call. It
			// was taken but not threaded while nothing invoked them (stage 3 task 3), which
			// a task-5 write would have found as a nil-receiver panic inside the Activity.
			ActivityExecution: m.activityExecution,
		},
	}
}

// ===========================================================================
// messageBusSeam — mirrors billingManager's/operationsManager's narrow startup
// seam (internal/utility/messagebus). ONLY the startup RegisterSchedule verb is
// consumed here; the workflow-invoked category-B verbs (registerSchedule /
// deliverSignal, reached through the generated invokers per Acts.MessageBus*)
// already speak the real messagebus.MessageBus contract types directly and need
// no adapter. The in-workflow primitives (awaitSignal / startTimer / executeChild)
// are the Manager's OWN workflow code (D-DA category A), NOT bus verbs.
// ===========================================================================

// messageBusSeam is the Manager's consumer view for the STARTUP Schedule
// registration only. UNEXPORTED; the folded adapter below bridges the published
// messagebus.MessageBus to it.
type messageBusSeam interface {
	// RegisterSchedule registers (idempotently, by id) a recurring Schedule.
	RegisterSchedule(ctx context.Context, spec scheduleSpec) error
}

// scheduleSpec mirrors messagebus.ScheduleSpec for the two Schedules this Manager
// registers at startup. The composition root adapts the concrete utility.
type scheduleSpec struct {
	ID           string
	WorkflowType string
	TaskQueue    string
	IntervalSecs int
}

// messageBusAdapter adapts the published messagebus.MessageBus onto messageBusSeam.
// Only the startup RegisterSchedule verb is consumed (the published ScheduleSpec
// resolves the task queue via its KindBinding table, so the seam's TaskQueue is not
// threaded).
type messageBusAdapter struct {
	inner messagebus.MessageBus
}

var _ messageBusSeam = messageBusAdapter{}

func (a messageBusAdapter) RegisterSchedule(ctx context.Context, spec scheduleSpec) error {
	return a.inner.RegisterSchedule(
		fwra.Context{Context: ctx},
		messagebus.ScheduleID(spec.ID),
		messagebus.ScheduleSpec{
			ExecutionKind: messagebus.ExecutionKind(spec.WorkflowType),
			Cadence:       messagebus.Cadence{Every: time.Duration(spec.IntervalSecs) * time.Second},
		},
	)
}

// ListEpisodesForActivity returns every episode record (dispatch runs, or gaps)
// captured against one construction activity, in episodeAccess's own (append)
// order. A pass-through over episodeAccess.ListEpisodes scoped by
// TargetRef=activityID, mapped to the contract EpisodeRecordView.
func (m *constructionManager) ListEpisodesForActivity(rc fwmanager.Context, projectID ProjectID, activityID string) ([]EpisodeRecordView, error) {
	ctx := rc.Context
	if projectID == "" {
		return nil, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return nil, newError(fwmanager.ContractMisuse, "empty activityId")
	}
	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{
		ProjectID: episode.ProjectID(projectID),
		TargetRef: &activityID,
	})
	if err != nil {
		return nil, mapRAError(err, "episodeAccess.ListEpisodes")
	}
	return csEpisodeRecordViews(records), nil
}

// GetEpisodeTimeline returns one episode's full timeline: its ledger record plus
// the sequenced trace events mined from its run. NotFound if episodeID does not
// name a record on this project.
func (m *constructionManager) GetEpisodeTimeline(rc fwmanager.Context, projectID ProjectID, episodeID string) (EpisodeTimeline, error) {
	ctx := rc.Context
	if projectID == "" {
		return EpisodeTimeline{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if episodeID == "" {
		return EpisodeTimeline{}, newError(fwmanager.ContractMisuse, "empty episodeId")
	}
	// ListEpisodes has no by-id lookup (episodeAccess.md — the ledger is append-
	// scanned by TargetRef); querying with no TargetRef and finding the one record
	// whose EpisodeID matches is the only way to resolve one episode across every
	// target on the project.
	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{ProjectID: episode.ProjectID(projectID)})
	if err != nil {
		return EpisodeTimeline{}, mapRAError(err, "episodeAccess.ListEpisodes")
	}
	rec, ok := findEpisodeRecord(records, episodeID)
	if !ok {
		return EpisodeTimeline{}, newError(fwmanager.NotFound, fmt.Sprintf("episode %q not found", episodeID))
	}
	// A GAP record (episode.EpisodeGap — the dispatch that produced no summary at
	// all) has no trace file: TracePath is nil on the ledger record. The
	// never-silent gap doctrine (Task 2/7) treats a gap as a PRESENT, first-class
	// outcome, not an absence — the record itself must always resolve; only its
	// timeline is empty. Skip the RA round-trip entirely when TracePath says
	// there is nothing to read, and treat a NotFound FROM ReadTraceEvents (e.g. a
	// TracePath that no longer resolves) the same way, rather than erroring the
	// whole timeline — either would otherwise be indistinguishable from an
	// unknown episodeID.
	if rec.TracePath == nil || *rec.TracePath == "" {
		return EpisodeTimeline{Record: csEpisodeRecordToView(rec), Events: episodeTimelineEvents(nil)}, nil
	}
	raw, err := m.episodes.ReadTraceEvents(fwra.Context{Context: ctx}, episode.ProjectID(projectID), episodeID)
	if err != nil {
		if isEpisodeTraceNotFound(err) {
			return EpisodeTimeline{Record: csEpisodeRecordToView(rec), Events: episodeTimelineEvents(nil)}, nil
		}
		return EpisodeTimeline{}, mapRAError(err, "episodeAccess.ReadTraceEvents")
	}
	return EpisodeTimeline{
		Record: csEpisodeRecordToView(rec),
		Events: episodeTimelineEvents(raw),
	}, nil
}

// csEpisodeRecordViews maps a slice of ledger records onto the contract view type.
func csEpisodeRecordViews(records []episode.EpisodeRecord) []EpisodeRecordView {
	out := make([]EpisodeRecordView, 0, len(records))
	for _, r := range records {
		out = append(out, csEpisodeRecordToView(r))
	}
	return out
}

// csEpisodeRecordToView maps one episodeAccess ledger record onto this contract's
// OWN copy of the view shape (EpisodeRecordView mirrors episodeAccess.EpisodeRecord
// field-for-field; contracts are self-contained, so this is an intentional
// duplicate of the mapping episodeAccess itself owns, not a shared function).
func csEpisodeRecordToView(r episode.EpisodeRecord) EpisodeRecordView {
	v := EpisodeRecordView{
		EpisodeID:      r.EpisodeID,
		Kind:           csEpisodeViewKind(r.Kind),
		TargetRef:      r.TargetRef,
		WorkerClass:    r.WorkerClass,
		Model:          r.Model,
		Usage:          EpisodeUsage(r.Usage),
		CostUSD:        r.CostUSD,
		NumTurns:       r.NumTurns,
		ToolCallCounts: r.ToolCallCounts,
		StartedAt:      r.StartedAt,
		EndedAt:        r.EndedAt,
		Outcome:        episodeViewOutcome(r.Outcome),
		GapReason:      r.GapReason,
		TracePath:      r.TracePath,
	}
	if r.Lineage != nil {
		l := EpisodeLineage(*r.Lineage)
		v.Lineage = &l
	}
	if r.StreamedUsage != nil {
		u := EpisodeUsage(*r.StreamedUsage)
		v.StreamedUsage = &u
	}
	if len(r.SubagentSpans) > 0 {
		spans := make([]SubagentSpan, 0, len(r.SubagentSpans))
		for _, s := range r.SubagentSpans {
			spans = append(spans, SubagentSpan(s))
		}
		v.SubagentSpans = spans
	}
	return v
}

// csEpisodeViewKind maps the episodeAccess RA's Kind onto this contract's own copy
// of the enum. Written as a TOTAL switch rather than a numeric cast so a future
// divergence between the two independently-versioned contracts is a compile-time
// conversation, not silent drift.
func csEpisodeViewKind(k episode.EpisodeKind) EpisodeKind {
	switch k {
	case episode.EpisodeKindDesign:
		return EpisodeKindDesign
	case episode.EpisodeKindConstruction:
		return EpisodeKindConstruction
	case episode.EpisodeKindReview:
		return EpisodeKindReview
	case episode.EpisodeKindRework:
		return EpisodeKindRework
	case episode.EpisodeKindAnswer:
		return EpisodeKindAnswer
	default:
		// Unreachable for the five defined episode.EpisodeKind values above (the
		// exhaustive linter enforces that every real variant has its own case);
		// kept as a defensive fallback for an out-of-range ordinal.
		return EpisodeKindConstruction
	}
}

// ---------------------------------------------------------------------------
// QueryActivityView — revision derivation (spec 2026-09-20 §2). PURE: no I/O, no
// clock. normalizeAttempts turns today's scattered evidence into one attempt list;
// deriveTaskViews cuts that list into revisions and decides every task's state.
//
// WHY NORMALIZE. The running construct workflow does not write the attempt ledger: it
// counts attempts in memory to mint the AttemptID it stamps on the episode's TargetRef,
// and a gate attempt is written by nothing. Only cmd/backfill-attempts appends to
// ActivityConstructionStatus.Attempts. A live run's history is its episodes, its
// send-back OperatorNotes, its stored phase completions and its live session. Stage 3
// of the unified-activity spec persists revisions; this block is deleted then.
// ---------------------------------------------------------------------------

// Revision outcomes and task states: the wire values of the ActivityView contract.
const (
	revRunning       = "running"
	revAwaitingHuman = "awaitingHuman"
	revPassed        = "passed"
	revSentBack      = "sentBack"
	revFailed        = "failed"
	revSkipped       = "skipped"
	revWithdrawn     = "withdrawn"

	taskPending       = "pending"
	taskLocked        = "locked"
	taskRunning       = "running"
	taskAwaitingHuman = "awaitingHuman"
	taskPassed        = "passed"
	taskSentBack      = "sentBack"
	taskFailed        = "failed"

	normalizeGenerator = "constructionManager.normalizeAttempts"
)

// taskRevision is revision n of one lifecycle task: the n-th work that reached the gate
// (with every failed or retried attempt before it) for a dispatch task, the n-th gate
// attempt for a review task. The two share n.
type taskRevision struct {
	N          int
	Outcome    string
	StartedAt  *time.Time
	EndedAt    *time.Time
	AttemptIDs []string
	EpisodeID  string
	Note       string
	Comments   []projectstate.NoteComment
	Provenance projectstate.RecordOrigin
	// The rest are the PERSISTED round's own facts (stage 3, task 7). A revision the
	// reconstruction produced carries none of them except Round, which it takes from the
	// gate attempt's number — the reconstruction's own de-facto round.
	Round      int64
	Verdicts   []projectstate.ReviewVerdict
	Thread     []projectstate.ReviewComment
	Reviewers  []projectstate.RoundReviewer
	SubjectRef projectstate.SubjectRef
	DecidedBy  string
	DecidedAt  string
}

// taskView is one lifecycle task's derived state and history.
type taskView struct {
	ID        string
	State     string
	Revisions []taskRevision
}

// normalizeAttempts builds the one attempt list the derivation reads (rules N1–N4): the
// ledger verbatim, then a work attempt per episode the ledger does not hold, then the
// dispatch running now, then the gate attempts the send-back notes, the RESOLVED phase
// completions and the live gate imply. Everything it adds is stamped backfilled —
// "reconstructed from real evidence recorded elsewhere" — with the evidence as its basis.
//
// resolved is projectstate.ResolveConstructionRow's third return, never row.Phases: the
// phase set a row HAS and the completion state it is IN are one fact with one rule
// (ResolvePhaseCompletions), and every reader of this row derives from that one answer.
func normalizeAttempts(activityID string, row projectstate.ActivityExecution, resolved []projectstate.PhaseCompletion, episodes []episode.EpisodeRecord, live *ConstructionSessionView) []projectstate.TaskAttempt {
	out := slices.Clone(row.Attempts)
	index := make(map[string]int, len(out))
	for i, a := range out {
		index[a.AttemptID] = i
	}
	for _, ep := range episodes {
		task, n, ok := parseAttemptRef(activityID, ep.TargetRef)
		if !ok {
			continue
		}
		evidence := projectstate.EvidenceRef{Kind: projectstate.EvidenceEpisode, Ref: ep.EpisodeID}
		if i, held := index[ep.TargetRef]; held {
			if out[i].Evidence.Kind == projectstate.EvidenceNone {
				out[i].Evidence = evidence // N1
			}
			continue
		}
		started, ended := ep.StartedAt, ep.EndedAt
		index[ep.TargetRef] = len(out)
		out = append(out, projectstate.TaskAttempt{ // N2
			AttemptID: ep.TargetRef, Task: task, Phase: projectstate.PhaseForTask(task), Attempt: n,
			Actor: projectstate.ActorAgent, StartedAt: &started, EndedAt: &ended,
			Outcome: taskOutcomeOfEpisode(ep.Outcome), Evidence: evidence,
			Provenance: reconstructed("episodes[" + ep.EpisodeID + "]"),
		})
	}
	out = appendRunningAttempt(out, activityID, resolved, live)
	return appendGateAttempts(out, activityID, row, resolved, live)
}

// parseAttemptRef reads "<activityId>:<task>:<n>" (projectstate.AttemptID). A legacy
// TargetRef (the bare activity id) and another activity's ref are not attempts of this one.
func parseAttemptRef(activityID, ref string) (projectstate.MethodTask, int, bool) {
	rest, ok := strings.CutPrefix(ref, activityID+":")
	if !ok {
		return "", 0, false
	}
	name, num, ok := strings.Cut(rest, ":")
	if !ok {
		return "", 0, false
	}
	n, err := strconv.Atoi(num)
	task := projectstate.MethodTask(name)
	if err != nil || n < 1 || projectstate.PhaseForTask(task) == "" {
		return "", 0, false
	}
	return task, n, true
}

// taskOutcomeOfEpisode: an episode that did not succeed — failed, cancelled, or a gap
// with no summary at all — is a failed attempt; the dispatch burned and produced nothing.
func taskOutcomeOfEpisode(o episode.EpisodeOutcome) projectstate.TaskOutcome {
	switch o {
	case episode.EpisodeSucceeded:
		return projectstate.OutcomePassed
	case episode.EpisodeFailed, episode.EpisodeCancelled, episode.EpisodeGap:
		return projectstate.OutcomeFailed
	}
	return projectstate.OutcomeFailed
}

func reconstructed(basis string) projectstate.AttemptProvenance {
	return projectstate.AttemptProvenance{Origin: projectstate.OriginBackfilled, Generator: normalizeGenerator, Basis: basis}
}

func highestAttempt(attempts []projectstate.TaskAttempt, task projectstate.MethodTask) int {
	n := 0
	for _, a := range attempts {
		if a.Task == task && a.Attempt > n {
			n = a.Attempt
		}
	}
	return n
}

// ledgerRejections counts the gate task's rejections ALREADY in out — the ones a real
// run recorded. N4 reconstructs only the send-backs beyond them.
func ledgerRejections(out []projectstate.TaskAttempt, gate projectstate.MethodTask) int {
	n := 0
	for _, a := range out {
		if a.Task == gate && a.Outcome == projectstate.OutcomeRejected {
			n++
		}
	}
	return n
}

// roundsForTask is the row's PERSISTED rounds for one review task, in the APPEND-ONLY
// ledger's own order — which is the order they were opened in, and therefore the order
// they are read as revisions.
//
// IT DOES NOT SORT BY ROUND NUMBER, and the design rails are why. They mint a FOUR-part
// round id (activity:gate:artifactKind:n) because three artifact kinds share the
// architecture gate, and each kind counts ITS OWN rounds — so one row holds two rounds
// numbered 1 at the same gate, and ordering by number would interleave two unrelated
// review histories into one invented sequence. Ledger order is the only total order the
// two kinds share, and it is a real one. Nothing here parses a round id into segments
// either (equality is all any code in this repo asks of one): which artifact a round judges
// is a FIELD on it (roundGateKey), the join to the attempt ledger is by those fields, and a
// revision's number is its position in this order, never the round number — exactly as it
// already is for a gate attempt.
func roundsForTask(rounds []projectstate.ReviewRound, task projectstate.MethodTask) []projectstate.ReviewRound {
	out := make([]projectstate.ReviewRound, 0, len(rounds))
	for _, r := range rounds {
		if r.TaskID == task {
			out = append(out, r)
		}
	}
	return out
}

// roundGateKey is the identity of a GATE: which review task, judging which artifact. A
// review task's id is not enough on its own — `designReview` names a task in eight
// lifecycles and `testing` in nine, each resolving its subject through `reviews` — and
// several kinds are designed to share one lifecycle phase, which is why the design rails'
// RoundID is four-part. A construction round has no kind and keys on the task alone,
// exactly as it always did.
//
// Two arities over one rule, deliberately: the stranded-round sweep needs the gate
// identity WITHOUT a round number, and asking for it by synthesising a zero-Round
// ReviewRound would depend on roundJoinKey's suffix being constant — true by accident.
func roundGateKey(taskID projectstate.MethodTask, kind *projectstate.ArtifactKind) string {
	if kind == nil {
		return string(taskID)
	}
	return string(taskID) + ":" + kind.WireName()
}

// roundJoinKey is the identity a REVISION groups rounds by: the gate, plus the round
// number. This is the fix for the stage-3 entry criterion "two kinds' round 1 would bind
// one gate attempt"; nothing here parses a RoundID.
func roundJoinKey(r projectstate.ReviewRound) string {
	return roundGateKey(r.TaskID, r.ArtifactKind) + ":" + strconv.FormatInt(r.Round, 10)
}

// attemptGateKey is the SAME identity for a gate ATTEMPT. No attempt carries an artifact
// kind today — the design rails record no attempt ledger at all (stage 3 gives them one)
// — so an attempt's key is its task and its number, which is exactly the key a KINDLESS
// round mints. That is the whole fix: a kinded round's key can never equal a kindless
// attempt's, so two kinds' round 1 can no longer both claim gate attempt 1, and the
// construction rail's own join is unchanged to the byte.
func attemptGateKey(a projectstate.TaskAttempt) string {
	return roundGateKey(a.Task, nil) + ":" + strconv.Itoa(a.Attempt)
}

// ---------------------------------------------------------------------------
// THE FOUR LEDGER RESOLVERS the formerly-refused construction write paths share
// (stage 4b1 Task 12). Every one of the five paths has to answer "which round, and whose
// task?" and writing that five times is how five answers drift. They are PURE over one
// row, so the Manager-side ops and the tests state the rule without a Temporal
// environment — and they read FIELDS, never a parsed RoundID (§5.3: nothing may parse one).
// ---------------------------------------------------------------------------

// latestRoundFor resolves (activity, task) to the round a write should land on: the
// HIGHEST round on that task's gate key. It is the one place the five formerly-refused
// construction writes agree about which round they mean, and it REFUSES rather than
// guessing when there is none — a comment resolved against a task that has never been
// reviewed is a caller error, not an empty success.
//
// It keys through roundGateKey, so two artifact kinds sharing a gate task resolve to their
// OWN latest round (stage 4b1 Task 3) rather than to whichever was written last. A tie on
// the round NUMBER is broken by ledger order (the later append wins), which is the only
// total order two kinds at one gate share — the same rule roundsForTask states.
func latestRoundFor(row projectstate.ActivityExecution, taskID string, kind *projectstate.ArtifactKind) (projectstate.ReviewRound, error) {
	want := roundGateKey(projectstate.MethodTask(taskID), kind)
	var best projectstate.ReviewRound
	found := false
	for _, r := range row.Reviews {
		if roundGateKey(r.TaskID, r.ArtifactKind) != want {
			continue
		}
		if !found || r.Round >= best.Round {
			best, found = r, true
		}
	}
	if !found {
		return projectstate.ReviewRound{}, newError(fwmanager.FailedPrecondition, fmt.Sprintf(
			"task %s of activity %s has no review round: there is nothing to write against until it has been reviewed once",
			taskID, row.ActivityID))
	}
	return best, nil
}

// taskOfRound is the task a round belongs to, and it is a FIELD read rather than a derivation:
// OpenReviewRound stamps the review task on every round it opens, so the round → task mapping the
// signal router needs is already data (controller ruling 2).
//
// Its ONE caller is the comment-status mirror signal (SetTaskCommentStatus), and it earns its
// name there: the signal must be addressed by the round that HOLDS the comment, not by the task
// the caller named, because a reviewer resolves at the current gate a comment filed at a previous
// one and the router forwards by TaskID. (Fix round 1 deleted this function when the mirror was
// measured out of existence; fix round 2's held autogate put the mirror — and it — back.)
func taskOfRound(r projectstate.ReviewRound) string { return string(r.TaskID) }

// roundOfComment finds the round whose thread holds commentID. THE COMMENT DECIDES ITS
// ROUND, not the task the caller addressed: a reviewer resolves at the CURRENT gate a
// comment they filed at a previous one (the design rail's own case,
// Test_CoAuthor_ResolveComment_IsMirroredOntoTheRound), so resolving through
// latestRoundFor would land the transition on a thread that does not hold the comment and
// the store would answer NotFound for a comment that plainly exists.
func roundOfComment(row projectstate.ActivityExecution, commentID string) (projectstate.ReviewRound, bool) {
	for _, r := range row.Reviews {
		for _, c := range r.Thread {
			if c.ID == commentID {
				return r, true
			}
		}
	}
	return projectstate.ReviewRound{}, false
}

// escalatedTaskOf answers WHICH TASK an operator override is about, off the attempt
// ledger, because the override carries no task and the generic child's router DROPS a
// signal that names none (stage 4b1 Task 12, controller ruling 2 + the Task-11 round-2
// defect D2).
//
// The rule: the LAST task on the ledger whose highest-numbered attempt FAILED. That is
// exactly the state an escalation leaves behind — dispatchConstructionOnce resolves the
// attempt against its terminal observation BEFORE the intervention Engine is consulted, so
// by the time the operator is asked, the failed dispatch is on the ledger and nothing has
// superseded it. Ledger order breaks the tie a FORK can create (two branches escalated at
// once): the most recent dispatch is the one the operator is looking at, and there is no
// other datum to prefer — the session view carries `takeover` as its gate key, not a task.
//
// It answers false rather than guessing when no attempt failed, and the caller refuses with
// FailedPrecondition naming the missing datum. A task-less signal would be silently dropped
// by the router, which is the one outcome an operator override must never have.
func escalatedTaskOf(row projectstate.ActivityExecution) (projectstate.MethodTask, bool) {
	for i := len(row.Attempts) - 1; i >= 0; i-- {
		task := row.Attempts[i].Task
		if out, _ := latestTaskOutcome(row, task); out == projectstate.OutcomeFailed {
			return task, true
		}
	}
	return "", false
}

// judgedAttemptOfRound is the attempt id a round's own writes must cite: the
// highest-numbered attempt at the task this round JUDGES. AppendReviewVerdict refuses an
// empty attemptId ("a verdict that names no attempt cannot be joined back to what it
// judged"), and the Manager — unlike the child, which holds gate.judgedAttemptID in
// memory — has to recover it from the ledger.
//
// With no such attempt it cites the ROUND's own id, which is minted in the attempt-id shape
// for the gate itself (openRound). That is honest: a round opened over a subject with no
// recorded dispatch is judging the gate's own attempt, and citing the round is strictly
// more useful than citing nothing the store would refuse.
func judgedAttemptOfRound(activityID string, row projectstate.ActivityExecution, r projectstate.ReviewRound) string {
	if _, n := latestTaskOutcome(row, r.Reviews); n > 0 {
		return projectstate.AttemptID(activityID, r.Reviews, n)
	}
	return r.RoundID
}

// roundSweepDecidedBy is who the ledger records for a swept round. It is deliberately
// not an operator and not a role: nobody decided this round, a sweep closed it.
const roundSweepDecidedBy = "platform-sweep"

// strandedRounds returns the PENDING rounds of one row that a later round on the same
// GATE has superseded — in ledger order, so a tick's writes are deterministic.
// A pending round that is its gate's latest is NOT stranded: it may be a live gate
// awaiting a human.
//
// "Same gate" is roundGateKey: the review task AND the artifact kind. Two kinds sharing
// one gate task are two gates here, so neither can strand the other — which is the
// artifactKind field doing its job. This is WHY roundGateKey exists at its own arity:
// asking roundJoinKey for a gate identity would mean synthesising a zero-Round
// ReviewRound and relying on every call getting the same ":0" suffix, which is true by
// accident and not by contract.
//
// It is a PURE function of one row (no workflow context), so the file-layout standard
// puts it here beside roundGateKey rather than in roundsweep.go, and the sweep's tests
// can state the rule without a Temporal environment.
func strandedRounds(row projectstate.ActivityExecution) []projectstate.ReviewRound {
	latest := make(map[string]int64, len(row.Reviews))
	for _, r := range row.Reviews {
		gate := roundGateKey(r.TaskID, r.ArtifactKind)
		if r.Round > latest[gate] {
			latest[gate] = r.Round
		}
	}
	var out []projectstate.ReviewRound
	for _, r := range row.Reviews {
		if r.Outcome != projectstate.RoundPending {
			continue
		}
		if r.Round < latest[roundGateKey(r.TaskID, r.ArtifactKind)] {
			out = append(out, r)
		}
	}
	return out
}

// sortedActivityIDs is the round sweep's walk order over the execution map. Map
// iteration order in a workflow is non-determinism, not a style question: two replays
// of the same history would issue the same writes in a different sequence and the
// second would not match the first's recorded commands.
func sortedActivityIDs(rows map[string]projectstate.ActivityExecution) []string {
	return slices.Sorted(maps.Keys(rows))
}

// sendBackNotesFor is the phase's send-back notes in recorded order (append-only slice
// order IS RecordedAt order).
func sendBackNotesFor(notes []projectstate.OperatorNote, p projectstate.ActivityMethodPhase) []projectstate.OperatorNote {
	out := make([]projectstate.OperatorNote, 0, len(notes))
	for _, note := range notes {
		if note.Kind == projectstate.NoteSendBack && note.Gate == string(p) {
			out = append(out, note)
		}
	}
	return out
}

// lowestAttempt is the smallest attempt number the list holds for the task, and whether
// it holds any at all. A gate whose ledger starts at #2 has room at #1 beneath it.
func lowestAttempt(attempts []projectstate.TaskAttempt, task projectstate.MethodTask) (int, bool) {
	low, held := 0, false
	for _, a := range attempts {
		if a.Task == task && (!held || a.Attempt < low) {
			low, held = a.Attempt, true
		}
	}
	return low, held
}

// appendPreLedgerRejections is N4's reconstruction half: the phase's send-back notes the
// ledger holds no rejection for. They are the OLDEST notes (tails aligned like R4 —
// notes exist only since B1.1, so it is the oldest rejections that have no note and the
// oldest notes that have no recorded rejection), and they are OLDER than every attempt
// the gate has recorded. So they must SORT BEFORE the ledger's own: phaseRevisions orders
// a gate's revisions by .Attempt and reviewEvidenceState reads the LAST one, so numbering
// a reconstruction after the ledger's highest turns a passed, merged gate into sentBack.
// Renumbering a recorded attempt is forbidden (N1 keeps the ledger verbatim), so the
// block is placed immediately BELOW the lowest recorded number instead:
// lowest-unrecorded .. lowest-1, which runs to 0 and below on a gate whose ledger already
// starts at #1. That block is contiguous and strictly below anything recorded, so the
// AttemptIDs stay unique and the placement deterministic, and "≤ 0" reads as exactly what
// it is — an attempt from before this gate kept a ledger. A gate with NO recorded attempt
// has no ledger to sit under and numbers from 1, exactly as it always did.
func appendPreLedgerRejections(out []projectstate.TaskAttempt, activityID string, gate projectstate.MethodTask, p projectstate.ActivityMethodPhase, notes []projectstate.OperatorNote) []projectstate.TaskAttempt {
	unrecorded := len(notes) - ledgerRejections(out, gate)
	if unrecorded <= 0 {
		return out
	}
	n := 1
	if lowest, held := lowestAttempt(out, gate); held {
		n = lowest - unrecorded
	}
	for _, note := range notes[:unrecorded] {
		at := note.RecordedAt
		out = append(out, projectstate.TaskAttempt{
			AttemptID: projectstate.AttemptID(activityID, gate, n), Task: gate, Phase: p, Attempt: n,
			EndedAt: &at, Outcome: projectstate.OutcomeRejected,
			Provenance: reconstructed("operatorNotes[" + note.NoteID + "]"),
		})
		n++
	}
	return out
}

// appendRunningAttempt is N3: the dispatch a live session is running now, which has no
// episode until it ends.
//
// The phase it is running is DERIVED from the resolved set — the first lifecycle phase
// the ledger does not hold complete — rather than read off a stored CurrentPhase. The
// stored field is gone (spec §5.3: it was a second answer to a question the ledger
// already answers), and it was the less trustworthy of the two anyway: it was stamped at
// phase entry and never cleared, so a row that had moved on still named the phase it was
// stamped in. A row whose every phase is complete is running nothing, and returns none.
func appendRunningAttempt(out []projectstate.TaskAttempt, activityID string, resolved []projectstate.PhaseCompletion, live *ConstructionSessionView) []projectstate.TaskAttempt {
	if live == nil || (live.Stage != StageDispatching && live.Stage != StagePipelineRunning) {
		return out
	}
	current := projectstate.CurrentLifecyclePhase(resolved)
	task := projectstate.AgentTaskFor(current)
	if task == "" {
		return out
	}
	for _, a := range out {
		if a.Task == task && a.Outcome == projectstate.OutcomePending {
			return out
		}
	}
	n := highestAttempt(out, task) + 1
	return append(out, projectstate.TaskAttempt{
		AttemptID: projectstate.AttemptID(activityID, task, n), Task: task, Phase: current, Attempt: n,
		Actor: projectstate.ActorAgent, Provenance: reconstructed("session.stage"),
	})
}

// liveApprovalGate is the lifecycle phase a live session awaits approval at, if any. The
// merge hold and an escalation are not phase gates and never match a phase id.
func liveApprovalGate(live *ConstructionSessionView) (string, *time.Time) {
	if live == nil || live.Stage != StageAwaitingApproval || live.AwaitingGate == nil {
		return "", nil
	}
	return *live.AwaitingGate, live.AwaitingSince
}

// appendGateAttempts is N4, over the resolved phase set.
func appendGateAttempts(out []projectstate.TaskAttempt, activityID string, row projectstate.ActivityExecution, resolved []projectstate.PhaseCompletion, live *ConstructionSessionView) []projectstate.TaskAttempt {
	liveGate, liveSince := liveApprovalGate(live)
	// ResolveConstructionRow's reconciled set IS the phase inventory and the completion
	// state, in profile order. There is no second inventory: canonicalMethodPhases was one,
	// and two inventories over one row is exactly what ResolvePhaseCompletions exists to
	// remove ("when the two disagree, the profile wins"). Reading row.Phases here instead
	// reconstructed a passed gate for a phase the row's read-time lifecycle does not have.
	for _, pc := range resolved {
		p := pc.Phase
		gate := projectstate.GateTaskFor(p)
		// ROUNDS BEAT NOTES BEAT NOTHING, per GATE. A gate the row holds a round for is a
		// gate a real run wrote: its revisions are read from that ledger (phaseRevisions),
		// so every reconstruction here would be a second, competing record of the same
		// review — the note-shaped double count Task 1 removed, and its phase-completion
		// and live-gate shaped twins. The rule is per gate, not per row: a row written
		// across the ledger's arrival has rounds at one gate and only notes at an older one.
		if len(roundsForTask(row.Reviews, gate)) > 0 {
			continue
		}
		passed := false
		for _, a := range out {
			passed = passed || (a.Task == gate && a.Outcome == projectstate.OutcomePassed)
		}
		// A send-back note and a RECORDED rejection of the same gate are one event, not
		// two. The workflow records both (the note is how the feedback reaches the next
		// dispatch — PendingOperatorNotes), so reconstructing one attempt per note on top
		// of the ledger would double every revision. Only the notes the ledger has no
		// rejection for are reconstructed, and they go BELOW it — see the function.
		out = appendPreLedgerRejections(out, activityID, gate, p, sendBackNotesFor(row.OperatorNotes, p))
		// The newest attempt continues the ledger's numbering, AFTER the reconstruction
		// (which either sits below the ledger or, on a gate with no ledger at all, IS the
		// numbering so far).
		n := highestAttempt(out, gate)
		add := func(outcome projectstate.TaskOutcome, started, ended *time.Time, basis string) {
			n++
			out = append(out, projectstate.TaskAttempt{
				AttemptID: projectstate.AttemptID(activityID, gate, n), Task: gate, Phase: p, Attempt: n,
				StartedAt: started, EndedAt: ended, Outcome: outcome, Provenance: reconstructed(basis),
			})
		}
		switch {
		case pc.Completed && !passed:
			add(projectstate.OutcomePassed, nil, pc.CompletedAt, "phases["+string(p)+"].completed")
		case liveGate == string(p):
			add(projectstate.OutcomePending, liveSince, nil, "session.awaitingGate")
		}
	}
	return out
}

// phaseSegment is one revision's work: the phase's work-task attempts up to the one
// that reached the gate, plus any conditional-task attempts (someConstruction,
// testClient) made alongside them.
type phaseSegment struct {
	main, extra []projectstate.TaskAttempt
}

func (s phaseSegment) members() []projectstate.TaskAttempt {
	return append(slices.Clone(s.main), s.extra...)
}

// deriveTaskViews returns one view per lifecycle task, in lifecycle order (rules R1–R6
// and the task state table; both are spelled out in the stage-0 plan, Task 7).
//
// rounds is the row's PERSISTED review ledger. Where a gate has rounds they ARE its
// revisions; the note-and-attempt reconstruction survives only for the gates that have
// none, which is every gate of every row written before this ledger existed.
func deriveTaskViews(lc methodassets.Lifecycle, attempts []projectstate.TaskAttempt, notes []projectstate.OperatorNote, rounds []projectstate.ReviewRound, liveGate string) []taskView {
	revs := make(map[string][]taskRevision, len(lc.Tasks))
	gates := make(map[string]bool, len(lc.Phases))
	for _, ph := range lc.Phases {
		work := phaseWorkTask(lc, ph.ID)
		workRevs, gateRevs := phaseRevisions(ph, work, attempts, notes, rounds, liveGate)
		if work != "" {
			revs[work] = workRevs
		}
		revs[ph.Gate], gates[ph.Gate] = gateRevs, true
	}
	byID := make(map[string]methodassets.LifecycleTask, len(lc.Tasks))
	reviewerOf := make(map[string]string, len(lc.Tasks))
	for _, t := range lc.Tasks {
		byID[t.ID] = t
		if t.Kind == methodassets.LifecycleTaskReview && t.Reviews != "" {
			reviewerOf[t.Reviews] = t.ID
		}
	}
	states := make(map[string]string, len(lc.Tasks))
	var stateOf func(id string) string
	stateOf = func(id string) string {
		if s, ok := states[id]; ok {
			return s
		}
		states[id] = taskLocked // a cycle reads locked; a validated lifecycle has none
		t := byID[id]
		s := evidenceState(t, gates[id], revs, reviewerOf, liveGate)
		if s == "" {
			s = taskPending
			for _, dep := range t.DependsOn {
				if stateOf(dep) != taskPassed {
					s = taskLocked // rule 9
					break
				}
			}
		}
		states[id] = s
		return s
	}
	out := make([]taskView, 0, len(lc.Tasks))
	for _, t := range lc.Tasks {
		out = append(out, taskView{ID: t.ID, State: stateOf(t.ID), Revisions: revs[t.ID]})
	}
	return out
}

// phaseWorkTask is the phase's dispatch task ("" for a phase with none, e.g. the
// projectDesign activity's review-only phase).
func phaseWorkTask(lc methodassets.Lifecycle, phaseID string) string {
	for _, t := range lc.Tasks {
		if t.Phase == phaseID && t.Kind == methodassets.LifecycleTaskDispatch {
			return t.ID
		}
	}
	return ""
}

// phaseRevisions cuts one lifecycle phase's attempts into the work task's revisions and
// the gate task's revisions (R1–R4), and chooses which of the two review ledgers the gate
// reads: the PERSISTED rounds where the row holds any for this gate, the reconstruction
// where it holds none.
func phaseRevisions(ph methodassets.LifecyclePhase, work string, attempts []projectstate.TaskAttempt, notes []projectstate.OperatorNote, rounds []projectstate.ReviewRound, liveGate string) (workRevs, gateRevs []taskRevision) {
	var main, extra, gate []projectstate.TaskAttempt
	for _, a := range attempts {
		switch {
		case string(a.Phase) != ph.ID:
		case string(a.Task) == ph.Gate:
			gate = append(gate, a)
		case string(a.Task) == work:
			main = append(main, a)
		default:
			extra = append(extra, a) // R2: a conditional task is a sub-attempt of the work task
		}
	}
	// .Attempt is the total order of a task's attempts, and for a gate it INCLUDES the
	// pre-ledger rejections N4 reconstructs from send-back notes, which carry numbers
	// below the ledger's lowest — 0 and down — precisely so this sort puts them first
	// (appendPreLedgerRejections says why). The revision number below is the position
	// in this order, never the attempt number, so a "≤ 0" attempt is still revision 1.
	byAttempt := func(x, y projectstate.TaskAttempt) int { return cmp.Compare(x.Attempt, y.Attempt) }
	slices.SortStableFunc(main, byAttempt)
	slices.SortStableFunc(gate, byAttempt)
	for i, seg := range foldConditional(cutSegments(main), extra) {
		workRevs = append(workRevs, dispatchRevision(i+1, seg))
	}
	live := liveGate == ph.ID
	persisted := roundsForTask(rounds, projectstate.MethodTask(ph.Gate))
	if len(persisted) == 0 {
		return workRevs, reconstructedReviewRevisions(gate, notes, ph.ID, live)
	}
	// THE BOUNDARY IS INSIDE ONE GATE, not between gates. A row mid-flight when the round
	// ledger arrived has gate attempts the ledger recorded BEFORE any round existed, and
	// the first round it opens is numbered off that ledger (seedResumeFromLedger seeds the
	// counter from both), so it starts at 2 or higher. "Any round wins outright" would
	// drop attempt #1 — a real, recorded send-back — out of the history entirely.
	//
	// So the split is by NUMBER: every gate attempt below the lowest round number is a
	// review from before the rounds and reconstructs as it always did (with its note, and
	// with its own provenance); the rounds take it from there. A gate whose ledger starts
	// at or above the lowest round has no such attempts and this costs it nothing, which
	// is every gate on the design rails — they record no attempts at all.
	pre, joined := splitAtLowestRound(gate, persisted)
	// live is FALSE for the pre-round block on purpose: a session waiting at this gate is
	// waiting at the OPEN ROUND, never at an attempt recorded before the rounds began.
	gateRevs = reconstructedReviewRevisions(pre, notes, ph.ID, false)
	// ONE offset, fixed before the append: the rounds continue the numbering after the
	// whole pre-round block, they do not each start after the one before them.
	before := len(gateRevs)
	for _, rev := range roundRevisions(persisted, joined, live) {
		rev.N += before
		gateRevs = append(gateRevs, rev)
	}
	return workRevs, gateRevs
}

// splitAtLowestRound cuts a gate's recorded attempts at the lowest round number the row
// holds for it: pre is the attempts from before the rounds began, joined is the rest —
// the ones a round can settle (roundRevisions joins them by number).
func splitAtLowestRound(gate []projectstate.TaskAttempt, rounds []projectstate.ReviewRound) (pre, joined []projectstate.TaskAttempt) {
	lowest := rounds[0].Round
	for _, r := range rounds[1:] {
		if r.Round < lowest {
			lowest = r.Round
		}
	}
	for _, a := range gate {
		if int64(a.Attempt) < lowest {
			pre = append(pre, a)
		} else {
			joined = append(joined, a)
		}
	}
	return pre, joined
}

// roundRevisions turns the PERSISTED rounds for one review task into that task's
// revisions. The round is the record: its verdicts, its thread, its roster, its subject,
// its number and its decision are carried verbatim, and no ordering heuristic gets a vote.
//
// THE JOIN TO THE ATTEMPT LEDGER IS BY FIELDS, through roundJoinKey — never by splitting a
// round id into segments, which is a parse nothing in this repo does (equality is all any
// code asks of a round id). The construction rail mints a round's id with
// projectstate.AttemptID, so its <n> IS the gate attempt's number, and a KINDLESS round's
// key is exactly the kindless attempt's: that rail's join is unchanged to the byte.
//
// THE EARMARK THIS CLOSES (stage-3 entry criterion). The old join was the round NUMBER
// alone, and two artifact kinds share one gate while each counts its own rounds — so two
// rounds numbered 1 both bound the one attempt numbered 1, and a reader was shown one
// kind's revision citing the other's evidence. The kind is now a field on the round, so a
// round that judges an artifact keys on (task, kind, n) and cannot collide with an attempt
// recorded without one. While the design rails record no attempt ledger (stage 3 gives
// them one) that means a kinded round cites no attempt — which is the truth: there is none.
// Its successor question, which attempt of a kind a round settled, is answered by the same
// key the day attempts carry kinds too.
//
// A ROUND WITH NO GATE ATTEMPT IS NORMAL, not a gap. The construction rail opens the round
// when the gate is reached and writes the gate attempt only when the round is DECIDED, so
// every gate a human is looking at right now is a pending round with no attempt; a run that
// died in between (constructactivity.go's stated crash window) leaves one pending forever;
// and the design rails record no attempt ledger at all yet. In all three the round alone
// is the revision, and it cites no attempt because none exists.
func roundRevisions(rounds []projectstate.ReviewRound, gate []projectstate.TaskAttempt, live bool) []taskRevision {
	out := make([]taskRevision, 0, len(rounds))
	for i, r := range rounds {
		rev := taskRevision{
			N: i + 1, Outcome: roundOutcome(r.Outcome, live), Round: r.Round,
			Verdicts: r.Verdicts, Thread: r.Thread, Reviewers: r.Reviewers, SubjectRef: r.SubjectRef,
			DecidedBy: r.DecidedBy, DecidedAt: r.DecidedAt, Provenance: r.Provenance.Origin,
			Note: sendBackNote(r), Comments: threadAnchors(r.Thread),
			StartedAt: rfc3339OrNil(r.OpenedAt), EndedAt: rfc3339OrNil(r.DecidedAt),
		}
		join := roundJoinKey(r)
		for _, a := range gate {
			if attemptGateKey(a) == join {
				rev.AttemptIDs = []string{a.AttemptID}
				break
			}
		}
		out = append(out, rev)
	}
	return out
}

// roundOutcome renders a stored round outcome as a revision outcome. Total over the
// vocabulary with no default arm.
//
// RoundWithdrawn has its own wire member from stage 4b1: a round pulled back before
// anyone decided it is deliberate, and `failed` said the revision faulted. The
// construction rail gains a withdraw verb in the same wave (spec §7.2's fifth refused
// path), so this stopped being a design-rail-only rendering question.
//
// Its neighbour, the STRANDED pending round, is closed by the sweep in the same wave:
// a pending round with no live session and a later round on the same gate is stamped
// RoundWithdrawn rather than rendering `running` forever.
func roundOutcome(o projectstate.ReviewRoundOutcome, live bool) string {
	switch o {
	case projectstate.RoundPassed:
		return revPassed
	case projectstate.RoundSentBack:
		return revSentBack
	case projectstate.RoundWithdrawn:
		return revWithdrawn
	case projectstate.RoundPending:
		if live {
			return revAwaitingHuman
		}
		return revRunning
	}
	return revFailed // an outcome outside the vocabulary is not a pass
}

// threadAnchors is the round's thread as the revision's flat anchored comments — the same
// two fields a reconstructed revision offers, so a reader that has only ever known
// `comments` keeps working on a round-backed revision. It is a PROJECTION of `thread`, not
// a second record: the replies, the open/answered/resolved status and the reopen flag live
// there and only there, and a reader that needs them reads them there.
func threadAnchors(thread []projectstate.ReviewComment) []projectstate.NoteComment {
	if len(thread) == 0 {
		return nil
	}
	out := make([]projectstate.NoteComment, 0, len(thread))
	for _, c := range thread {
		out = append(out, projectstate.NoteComment{JSONPath: c.Anchor, Text: c.Text})
	}
	return out
}

// sendBackNote is the round's send-back note: the LAST send-back verdict's summary, which
// is the prose the operator typed into the decision that closed the round. It replaces the
// OperatorNote the reconstruction had to go looking for — same words, read off the record
// that owns them instead of matched to it by position.
//
// ONLY on a round DECIDED sentBack, which is what `note` means on the wire ("omitted unless
// outcome is sentBack"). A round that passed can still hold a send-back verdict — one
// reviewer dissented and the gate went through anyway — and rendering that dissent as the
// revision's send-back note would say the work was returned when it was not. The dissent
// is not lost: it is a row in `verdicts`, which is the whole point of carrying them.
func sendBackNote(r projectstate.ReviewRound) string {
	if r.Outcome != projectstate.RoundSentBack {
		return ""
	}
	note := ""
	for _, v := range r.Verdicts {
		if v.Verdict == projectstate.VerdictSendBack && v.Summary != "" {
			note = v.Summary
		}
	}
	return note
}

// rfc3339OrNil parses a store-stamped timestamp. The round ledger holds its times as
// RFC3339 STRINGS (the store stamps them; the caller never does), and a string that does
// not parse — or an empty one, which is what an undecided round's DecidedAt is — becomes
// no time at all rather than the zero instant.
func rfc3339OrNil(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

// reconstructedReviewRevisions is stage 0's R3/R4 path, kept for the gates of rows that
// predate the round ledger: one revision per gate attempt, with the phase's send-back
// notes matched onto the rejections TAILS ALIGNED (notes exist only since B1.1, so it is
// the oldest rejections that have no note). A gate with ANY persisted round never reaches
// it.
func reconstructedReviewRevisions(gate []projectstate.TaskAttempt, notes []projectstate.OperatorNote, phaseID string, live bool) []taskRevision {
	var sendBacks []projectstate.OperatorNote
	for _, n := range notes {
		if n.Kind == projectstate.NoteSendBack && n.Gate == phaseID {
			sendBacks = append(sendBacks, n)
		}
	}
	rejected := 0
	for _, g := range gate {
		if g.Outcome == projectstate.OutcomeRejected {
			rejected++
		}
	}
	// R4: the j-th rejection takes note j + (len(sendBacks) - rejected) — tails aligned.
	next := len(sendBacks) - rejected
	out := make([]taskRevision, 0, len(gate))
	for i, g := range gate {
		rev := reviewRevision(i+1, g, live)
		if g.Outcome == projectstate.OutcomeRejected {
			if next >= 0 && next < len(sendBacks) {
				rev.Note, rev.Comments = sendBacks[next].Text, sendBacks[next].Comments
			}
			next++
		}
		out = append(out, rev)
	}
	return out
}

// cutSegments is R1: a segment closes at the attempt that reached the gate.
func cutSegments(main []projectstate.TaskAttempt) []phaseSegment {
	var out []phaseSegment
	var cur []projectstate.TaskAttempt
	for _, a := range main {
		cur = append(cur, a)
		if a.Outcome == projectstate.OutcomePassed || a.Outcome == projectstate.OutcomeSkipped {
			out, cur = append(out, phaseSegment{main: cur}), nil
		}
	}
	if len(cur) > 0 {
		out = append(out, phaseSegment{main: cur})
	}
	return out
}

// foldConditional is R2: each conditional-task attempt joins the first revision whose
// reaching attempt ended at or after it started, else the last revision.
func foldConditional(segs []phaseSegment, extra []projectstate.TaskAttempt) []phaseSegment {
	for _, x := range extra {
		if len(segs) == 0 {
			segs = append(segs, phaseSegment{})
		}
		at := len(segs) - 1
		for i, s := range segs {
			if len(s.main) == 0 {
				continue
			}
			reached := s.main[len(s.main)-1]
			if x.StartedAt != nil && reached.EndedAt != nil && !x.StartedAt.After(*reached.EndedAt) {
				at = i
				break
			}
		}
		segs[at].extra = append(segs[at].extra, x)
	}
	return segs
}

func dispatchRevision(n int, seg phaseSegment) taskRevision {
	members := seg.members()
	rev := taskRevision{N: n, Outcome: revFailed, Provenance: projectstate.AttemptsWorstOrigin(members)} // R5
	decisive := members[len(members)-1]
	if len(seg.main) > 0 {
		decisive = seg.main[len(seg.main)-1]
	}
	switch decisive.Outcome {
	case projectstate.OutcomePassed:
		rev.Outcome = revPassed
	case projectstate.OutcomeSkipped:
		rev.Outcome = revSkipped
	case projectstate.OutcomePending, projectstate.OutcomeRejected, projectstate.OutcomeFailed:
		// pending is decided below over every member; a work attempt is never "rejected".
	}
	for _, a := range members {
		rev.AttemptIDs = append(rev.AttemptIDs, a.AttemptID)
		if a.Outcome == projectstate.OutcomePending {
			rev.Outcome = revRunning
		}
	}
	for _, a := range slices.Backward(members) { // R6: main members first, so search them last-to-first
		if a.Evidence.Kind == projectstate.EvidenceEpisode && a.Task == decisive.Task {
			rev.EpisodeID = a.Evidence.Ref
			break
		}
	}
	rev.StartedAt, rev.EndedAt = attemptSpan(members)
	return rev
}

// reviewRevision renders one gate attempt as a revision. n is the ORDINAL of g over the
// gate attempts sorted by Attempt — 1 for the first, 2 for the second — not the attempt's
// own number: the two coincide only while the ledger has no gaps, so a ledger missing
// designReview#2 numbers its third attempt revision 2, and the attempt number stays
// visible inside attemptIds. live marks the occurrence the session is waiting at now.
func reviewRevision(n int, g projectstate.TaskAttempt, live bool) taskRevision {
	// Round: the attempt's own number is the de-facto round of a row that kept no round
	// ledger, and it is what the construction rail's round number IS. A number ≤ 0 is NOT
	// one: appendPreLedgerRejections places a reconstruction from before the ledger
	// beneath the ledger's lowest, which runs to 0 and below, and that number is a sort
	// position rather than a count of reviews. Such a revision carries no round at all,
	// which is the truth — nothing counted this gate's rounds when it happened.
	rev := taskRevision{N: n, AttemptIDs: []string{g.AttemptID},
		Provenance: projectstate.AttemptsWorstOrigin([]projectstate.TaskAttempt{g})}
	if g.Attempt > 0 {
		rev.Round = int64(g.Attempt)
	}
	switch g.Outcome {
	case projectstate.OutcomePending:
		rev.Outcome = revRunning
		if live {
			rev.Outcome = revAwaitingHuman
		}
	case projectstate.OutcomePassed:
		rev.Outcome = revPassed
	case projectstate.OutcomeRejected:
		rev.Outcome = revSentBack
	case projectstate.OutcomeFailed:
		rev.Outcome = revFailed
	case projectstate.OutcomeSkipped:
		rev.Outcome = revSkipped
	}
	rev.StartedAt, rev.EndedAt = attemptSpan([]projectstate.TaskAttempt{g})
	return rev
}

// attemptSpan is R6's times: the earliest start, and the latest end once nothing is pending.
func attemptSpan(members []projectstate.TaskAttempt) (started, ended *time.Time) {
	open := false
	for _, a := range members {
		if a.StartedAt != nil && (started == nil || a.StartedAt.Before(*started)) {
			started = a.StartedAt
		}
		if a.EndedAt != nil && (ended == nil || a.EndedAt.After(*ended)) {
			ended = a.EndedAt
		}
		open = open || a.Outcome == projectstate.OutcomePending
	}
	if open {
		ended = nil
	}
	return started, ended
}

// evidenceState applies state rules 1–8; "" means the task has no evidence and its
// dependsOn decide between locked and pending.
func evidenceState(t methodassets.LifecycleTask, isGate bool, revs map[string][]taskRevision, reviewerOf map[string]string, liveGate string) string {
	if t.Kind == methodassets.LifecycleTaskReview {
		if isGate && liveGate != "" && liveGate == t.Phase {
			return taskAwaitingHuman // 1
		}
		return reviewEvidenceState(revs[t.ID], len(revs[t.Reviews]))
	}
	return dispatchEvidenceState(revs[t.ID], revs[reviewerOf[t.ID]])
}

func reviewEvidenceState(mine []taskRevision, workRevisions int) string {
	if len(mine) == 0 {
		return ""
	}
	switch last := mine[len(mine)-1]; last.Outcome {
	case revRunning, revAwaitingHuman:
		return taskRunning // 2
	case revSentBack:
		if workRevisions > last.N {
			return taskPending // 3: the redraft is under way
		}
		return taskSentBack // 4
	case revFailed:
		return taskFailed // 6
	}
	return taskPassed // 7
}

func dispatchEvidenceState(mine, reviewer []taskRevision) string {
	var verdict *taskRevision
	if len(reviewer) > 0 {
		verdict = &reviewer[len(reviewer)-1]
	}
	if len(mine) == 0 {
		if verdict != nil && verdict.Outcome == revPassed {
			return taskPassed // 8: the gate is the exit criterion; silence is not denial
		}
		return ""
	}
	last := mine[len(mine)-1]
	switch {
	case last.Outcome == revRunning:
		return taskRunning // 2
	case verdict != nil && verdict.Outcome == revSentBack && last.N <= verdict.N:
		return taskSentBack // 5
	case last.Outcome == revFailed:
		return taskFailed // 6
	}
	return taskPassed // 7
}

// committedActivityItem finds an activity in the committed Phase-2 activity list.
func committedActivityItem(proj projectstate.Project, id string) (projectstate.ActivityItem, bool) {
	_, list, ok := committedPlanInputs(proj)
	if !ok {
		return projectstate.ActivityItem{}, false
	}
	for _, it := range list.Activities {
		if it.Name == id {
			return it, true
		}
	}
	return projectstate.ActivityItem{}, false
}

// liveSessionFor asks the activity's session only while its row is Running: a
// not-started, done or failed activity has no gate to show, and must read with Temporal
// down. No session (never dispatched, or past retention) is not an error.
func (m *constructionManager) liveSessionFor(ctx context.Context, projectID ProjectID, activityID ActivityID, coarse projectstate.ActivityConstructionPhase) (*ConstructionSessionView, error) {
	if coarse != projectstate.ActivityConstructionRunning {
		return nil, nil //nolint:nilnil // "no live session" is a value here, not a failure
	}
	v, err := m.activitySession(ctx, projectID, activityID)
	if err != nil {
		var fe *fwmanager.Error
		if errors.As(err, &fe) && fe.Kind == fwmanager.NotFound {
			return nil, nil //nolint:nilnil // see above
		}
		return nil, err
	}
	return &v, nil
}

// activityViewState folds the row's effective coarse state and the live session into
// the view's five states. KNOWN STAGE-0 GAP: the coarse state comes from the row, the
// task states from the evidence, and a Running row that stored no CurrentPhase has no
// running task to show — the activity reads running while every task reads pending or
// locked. That is honest about what today's records hold; stage 3 stores the revisions
// and the two stop being separate derivations.
func activityViewState(coarse projectstate.ActivityConstructionPhase, live *ConstructionSessionView) ActivityViewState {
	switch coarse {
	case projectstate.ActivityConstructionNotStarted:
		return ActivityViewNotStarted
	case projectstate.ActivityConstructionDone:
		return ActivityViewDone
	case projectstate.ActivityConstructionFailed:
		return ActivityViewFailed
	case projectstate.ActivityConstructionRunning:
		if live != nil && (live.Stage == StageAwaitingApproval || live.Stage == StageAwaitingTakeover) {
			return ActivityViewAwaitingHuman
		}
	}
	return ActivityViewRunning
}

// activityViewFrom assembles the contract view. Every array is non-nil: the wire carries
// [] for "none", never null.
func activityViewFrom(activityID ActivityID, item projectstate.ActivityItem, typ projectstate.ActivityType, variant projectstate.TestingVariant, lc methodassets.Lifecycle, resolved []projectstate.PhaseCompletion, tasks []taskView) ActivityView {
	states := make(map[string]string, len(tasks))
	revisions := make(map[string][]taskRevision, len(tasks))
	for _, t := range tasks {
		states[t.ID], revisions[t.ID] = t.State, t.Revisions
	}
	view := ActivityView{ActivityID: activityID, Name: item.Title, Type: typ.String(),
		Phases: make([]ActivityLifecyclePhase, 0, len(lc.Phases)), Tasks: make([]ActivityTaskView, 0, len(lc.Tasks))}
	if view.Name == "" {
		view.Name = item.Name
	}
	if typ == projectstate.ActivityTypeTesting {
		view.Variant = strPtrOrNil(variant.String())
	}
	view.ComponentID = strPtrOrNil(item.ComponentID)
	done := make(map[projectstate.ActivityMethodPhase]bool, len(resolved))
	for _, pc := range resolved {
		done[pc.Phase] = pc.Completed
	}
	for _, ph := range lc.Phases {
		// ONE rule: ResolvePhaseCompletions. The gate task's derived STATE is a view of
		// the same evidence, but it is derived through normalizeAttempts' reconstruction
		// and can disagree with the resolver over a partial row — and a screen that
		// disagrees with the pump about whether a phase is done is the defect this
		// collapses.
		view.Phases = append(view.Phases, ActivityLifecyclePhase{
			ID: ph.ID, Label: ph.Label, Weight: int64(ph.Weight), GateTaskID: ph.Gate,
			Completed: done[projectstate.ActivityMethodPhase(ph.ID)],
		})
	}
	for _, t := range lc.Tasks {
		view.Tasks = append(view.Tasks, ActivityTaskView{
			ID: t.ID, Kind: ActivityTaskKind(t.Kind), Title: t.Title, LifecyclePhaseID: t.Phase,
			DependsOn: append([]string{}, t.DependsOn...), Reviews: strPtrOrNil(t.Reviews),
			State: ActivityTaskState(states[t.ID]), Revisions: revisionViews(revisions[t.ID]),
		})
	}
	return view
}

func revisionViews(revs []taskRevision) []TaskRevisionView {
	out := make([]TaskRevisionView, 0, len(revs))
	for _, r := range revs {
		comments := make([]TaskRevisionComment, 0, len(r.Comments))
		for _, c := range r.Comments {
			comments = append(comments, TaskRevisionComment{JSONPath: c.JSONPath, Text: c.Text})
		}
		out = append(out, TaskRevisionView{
			N: int64(r.N), Outcome: TaskRevisionOutcome(r.Outcome), StartedAt: r.StartedAt, EndedAt: r.EndedAt,
			AttemptIDs: append([]string{}, r.AttemptIDs...), EpisodeID: strPtrOrNil(r.EpisodeID),
			CommentCount: int64(len(r.Comments)), Comments: comments, Note: strPtrOrNil(r.Note),
			Verdicts: verdictViews(r.Verdicts), Thread: threadViews(r.Thread), Reviewers: rosterViews(r.Reviewers),
			SubjectRef: subjectRefView(r.SubjectRef), Round: roundNumberOrNil(r.Round),
			DecidedBy: strPtrOrNil(r.DecidedBy), DecidedAt: strPtrOrNil(r.DecidedAt),
			Provenance: revisionProvenance(r.Provenance),
		})
	}
	return out
}

// verdictViews carries the round's verdicts onto the wire. A revision with none — a
// dispatch revision, or one reconstructed from a row that predates the round ledger —
// carries NO array rather than an empty one: "this record holds no verdicts" and "this
// review was decided with no verdict cast" are different facts, and the omitted field is
// the first of them.
func verdictViews(verdicts []projectstate.ReviewVerdict) []ReviewVerdictView {
	if len(verdicts) == 0 {
		return nil
	}
	out := make([]ReviewVerdictView, 0, len(verdicts))
	for _, v := range verdicts {
		out = append(out, ReviewVerdictView{
			ReviewerRole: v.ReviewerRole, Actor: strPtrOrNil(v.Actor), Verdict: verdictKind(v.Verdict),
			Summary: strPtrOrNil(v.Summary), At: v.At, AttemptID: strPtrOrNil(v.AttemptID),
		})
	}
	return out
}

// verdictKind names the stored verdict on the wire. Total over the vocabulary with no
// default arm; an out-of-vocabulary value reaches the wire verbatim rather than being
// blessed into an approval it was not.
func verdictKind(v projectstate.VerdictKind) ReviewVerdictKind {
	switch v {
	case projectstate.VerdictApprove:
		return VerdictApprove
	case projectstate.VerdictSendBack:
		return VerdictSendBack
	case projectstate.VerdictAbstain:
		return VerdictAbstain
	}
	return ReviewVerdictKind(v)
}

// threadViews carries the round's comment thread — replies, status and all — verbatim.
func threadViews(thread []projectstate.ReviewComment) []ReviewThreadComment {
	if len(thread) == 0 {
		return nil
	}
	out := make([]ReviewThreadComment, 0, len(thread))
	for _, c := range thread {
		replies := make([]ReviewThreadReply, 0, len(c.Replies))
		for _, rep := range c.Replies {
			replies = append(replies, ReviewThreadReply{ID: rep.ID, AuthorRole: rep.AuthorRole, Text: rep.Text, At: rep.At})
		}
		out = append(out, ReviewThreadComment{
			ID: c.ID, Anchor: c.Anchor, AnchorText: strPtrOrNil(c.AnchorText), Text: c.Text,
			AuthorRole: c.AuthorRole, Round: c.Round, Status: c.Status, Replies: replies,
			Reopened: c.Reopened, Type: c.Type, Addressee: strPtrOrNil(c.Addressee),
		})
	}
	return out
}

// rosterViews carries the roster the round was opened with.
func rosterViews(seats []projectstate.RoundReviewer) []ReviewRosterSeat {
	if len(seats) == 0 {
		return nil
	}
	out := make([]ReviewRosterSeat, 0, len(seats))
	for _, s := range seats {
		out = append(out, ReviewRosterSeat{Role: s.Role, Actor: s.Actor, Required: s.Required})
	}
	return out
}

// subjectRefView carries what the round judged. A revision with no subject at all — every
// reconstructed one, since a pre-ledger row recorded none — omits the field rather than
// shipping an empty ref that reads like a subject nobody can open.
func subjectRefView(s projectstate.SubjectRef) *ReviewSubjectRef {
	if s.Ref == "" {
		return nil
	}
	return &ReviewSubjectRef{Kind: string(s.Kind), Ref: s.Ref}
}

// roundNumberOrNil omits the round on a revision that has none: a dispatch revision, and
// a reconstruction placed beneath the ledger, whose attempt number is a sort position
// rather than a count of reviews (reviewRevision says why).
func roundNumberOrNil(n int64) *int64 {
	if n == 0 {
		return nil
	}
	return &n
}

// revisionProvenance names the origin on the wire. OriginSynthesized is the EMPTY string
// in storage on purpose (a dropped stamp fails suspicious); the wire spells it out.
func revisionProvenance(o projectstate.RecordOrigin) TaskRevisionProvenance {
	switch o {
	case projectstate.OriginObserved:
		return TaskRevisionObserved
	case projectstate.OriginBackfilled:
		return TaskRevisionBackfilled
	case projectstate.OriginSynthesized:
		return TaskRevisionSynthesized
	}
	return TaskRevisionSynthesized // an unknown origin is as bad as synthesized (originRank)
}

// ---------------------------------------------------------------------------
// THE ONE MANAGER — TWELVE OPS, ONE CHILD
//
// Everything above this banner used to be three rails moved verbatim; stage 4b1
// Task 13 deleted the seven per-kind workflows they drove, so what is left above
// is the Manager-side half the generic child does not own: the catalog reads, the
// two design slot-threads (ask / acknowledge), the two phase seals, and the
// projections. The twelve ops below no longer route on a rail — they route on the
// TASK's artifact kind, which is the same DATA the child's walker reads.
// ---------------------------------------------------------------------------

// deliveryManager is the ONE Manager of the Project Delivery Workflow volatility, and
// since stage 4b1 Task 13 it is no longer a dispatcher over three receivers: the two
// design façades are gone and their surviving bodies are methods on this type. It holds
// ONE inner manager (cs), whose WorkerManifest builds ONE csWorkflows — the generic DAG
// child and the pump quartet.
type deliveryManager struct {
	cs *constructionManager

	// THE MANAGER-SIDE DEPS. Every one of these is read by a body that runs OUTSIDE a
	// workflow — the catalog reads, the two slot-thread writes, the answer-job dispatch and
	// its episode watch, the two phase seals. The workflow-side deps are threaded into
	// csWorkflows by the manifest and are NOT reached from here.
	client            client.Client
	projectState      projectstate.ProjectStateAccess
	pipeline          agenticjob.AgenticJobAccess
	rail              sourcecontrol.SourceControlAccess
	repo              func(projectID ProjectID) (sourcecontrol.RepoRef, bool)
	designSession     projectstate.DesignSessionAccess
	activityExecution projectstate.ActivityExecutionAccess
	episodes          episode.EpisodeAccess

	// estimator + repoBase serve the folded CATALOG ops (GetProject's compute-at-read CPM +
	// EV/SPI, and each git row's prUrl). repoBase "" omits prUrl.
	estimator estimation.EstimationEngine
	repoBase  string

	// designHealth is the DesignHealthEngine behind the designHealth read. It is NOT a
	// generated constructor dep: the component carries no service contract and an Engine is
	// pure and stateless, so the builder constructs it directly rather than threading a
	// parameter no composition root could vary. Re-classifying it is 4b2's.
	designHealth designhealth.Engine
}

var _ DeliveryManager = (*deliveryManager)(nil)

// newDeliveryManager is the hand-written builder the GENERATED NewDeliveryManager
// delegates to. Its parameter list is the contract's `deps` list in order and is
// UNCHANGED by this task (R9 — dropping the deprecated facets is post-drain, 4b2); what
// changed is that it no longer splits that union across three receivers.
func newDeliveryManager(
	c client.Client,
	projectState projectstate.ProjectStateAccess,
	art artifact.ArtifactAccess,
	interventionEng intervention.InterventionEngine,
	reviewEng review.ReviewEngine,
	estimator estimation.EstimationEngine,
	operationEstimator operationestimation.OperationEstimationEngine,
	billingEstimator billing.BillingEngine,
	pipeline agenticjob.AgenticJobAccess,
	rail sourcecontrol.SourceControlAccess,
	constructionTransition projectstate.ConstructionTransitionAccess,
	gitStatus projectstate.GitActivityStatusAccess,
	designSession projectstate.DesignSessionAccess,
	activityExecution projectstate.ActivityExecutionAccess,
	bus messagebus.MessageBus,
	episodes episode.EpisodeAccess,
	escalationWaitTimeout time.Duration,
	interventionMode string,
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool),
	repoBase string,
) *deliveryManager {
	return &deliveryManager{
		cs: newConstructionManager(c, projectState, art, interventionEng, reviewEng, pipeline,
			rail, constructionTransition, gitStatus, designSession, activityExecution, bus,
			episodes, escalationWaitTimeout, interventionMode, repo,
			// The three estimate Engines, handed to the construction half TOO: the generic child
			// runs on its worker and walks the projectDesign lifecycle, whose one task is a
			// server-side computation over them.
			sdpEngines{Estimation: estimator, OperationEst: operationEstimator, Settlement: billingEstimator}),
		client:            c,
		projectState:      projectState,
		pipeline:          pipeline,
		rail:              rail,
		repo:              repo,
		designSession:     designSession,
		activityExecution: activityExecution,
		episodes:          episodes,
		estimator:         estimator,
		repoBase:          repoBase,
		designHealth:      designhealth.NewEngine(),
	}
}

// activityLifecycle is what is LEFT of railFor once the three rails became one child: it
// reads the project, classifies the activity and answers the lifecycle its task ids come
// from. The rail enum it used to return with is gone — every op that needed to know which
// of three choreographies owned an activity now asks the TASK what artifact it is about
// (artifactKindForTask, then phase1Kind), which is the same data the child's walker reads
// and the only thing the split ever stood for. Its one surviving line of judgement,
// methodassets.LifecycleFor(projectstate.LifecycleKeyFor(typ, variant)), is here.
func (m *deliveryManager) activityLifecycle(rc fwmanager.Context, projectID ProjectID, activityID ActivityID) (methodassets.Lifecycle, error) {
	if projectID == "" {
		return methodassets.Lifecycle{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return methodassets.Lifecycle{}, newError(fwmanager.ContractMisuse, "empty activityId")
	}
	id := string(activityID)
	proj, err := m.projectState.ReadProject(fwra.Context{Context: rc.Context}, projectstate.ProjectID(projectID))
	if err != nil {
		return methodassets.Lifecycle{}, mapRAError(err, "projectStateAccess.ReadProject")
	}
	item, ok := committedActivityItem(proj, id)
	if !ok {
		return methodassets.Lifecycle{}, newError(fwmanager.NotFound,
			"no activity "+id+" in the committed activity list")
	}
	typ, variant, err := projectstate.ClassifyActivity(id, item.WorkerClass, item.Coding)
	if err != nil {
		return methodassets.Lifecycle{}, newError(fwmanager.FailedPrecondition, err.Error())
	}
	lc, ok := methodassets.LifecycleFor(projectstate.LifecycleKeyFor(typ, variant))
	if !ok {
		return methodassets.Lifecycle{}, newError(fwmanager.Infrastructure,
			"the platform's method assets carry no lifecycle for activity type "+projectstate.LifecycleKeyFor(typ, variant))
	}
	return lc, nil
}

// artifactKindForTask resolves a task id to the artifact the task is ABOUT. A dispatch
// task names its own; a review task carries `reviews`, naming the dispatch it judges,
// and that dispatch names the artifact. This is the same chain
// webApp/src/components/activity/activityViewToGraph.ts artifactKindOf walks
// client-side; keep the two consistent. (The SPA has a third fallback, on the task's
// revisionGroup — method-assets' Go LifecycleTask carries no such field, so the Go side
// stops at the two-step chain and reports a ContractMisuse rather than guessing.)
func artifactKindForTask(lc methodassets.Lifecycle, taskID string) (ArtifactKind, bool) {
	t, ok := lifecycleTaskByID(lc, taskID)
	if !ok {
		return 0, false
	}
	name := t.ArtifactKind
	if name == "" && t.Reviews != "" {
		if judged, ok := lifecycleTaskByID(lc, t.Reviews); ok {
			name = judged.ArtifactKind
		}
	}
	if name == "" {
		return 0, false
	}
	kind, ok := projectstate.ArtifactKindFromWireName(lowerFirstRune(name))
	if !ok {
		return 0, false
	}
	return ArtifactKind(kind), true
}

func lifecycleTaskByID(lc methodassets.Lifecycle, taskID string) (methodassets.LifecycleTask, bool) {
	for _, t := range lc.Tasks {
		if t.ID == taskID {
			return t, true
		}
	}
	return methodassets.LifecycleTask{}, false
}

// lowerFirstRune turns a lifecycle's PascalCase artifactKind ("CoreUseCases",
// "SdpReview") into the canonical camelCase wire name projectstate keys on.
func lowerFirstRune(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToLower(string(r[0])) + string(r[1:])
}

// phase1Kind reports whether an artifact kind belongs to Phase 1 (KindMission …
// KindStandardCheck) rather than Phase 2 (KindPlanningAssumptions … KindSdpReview).
// It is the split QueryProjectView's `session` and `episodes` reads route on.
func phase1Kind(kind ArtifactKind) bool {
	return kind <= KindStandardCheck
}

// sdpReviewTaskID is the projectDesign lifecycle's ONE task — the single deterministic
// M0 cost-approval gate (spec §6/R7), which is computed rather than dispatched, so it
// carries an artifactKind of its own and no `reviews` target.
const sdpReviewTaskID = "sdpReview"

// requireActivityTask is the emptiness guard every activity-addressed op leads with.
// The contract's `required` is PRESENCE-only (2026-08-13 ruling), so "" arrives as a
// legitimately-sent value and the Manager is what rejects it. Each of the three moved
// rails rejected the same emptiness one hop further down; this hoists the check to the
// one entry point so every op states its own precondition.
func requireActivityTask(projectID ProjectID, activityID ActivityID, taskID string) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return newError(fwmanager.ContractMisuse, "empty activityId")
	}
	if strings.TrimSpace(taskID) == "" {
		return newError(fwmanager.ContractMisuse, "empty taskId")
	}
	return nil
}

// requireActivity is requireActivityTask for the two ops that address an activity but
// no single task of it.
func requireActivity(projectID ProjectID, activityID ActivityID) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return newError(fwmanager.ContractMisuse, "empty activityId")
	}
	return nil
}

// ---- op 1: StartProject ----------------------------------------------------

// StartProject folds the four former catalog/entry ops into one: create the project
// (when no id is given), set the operating model, set the research input, and start the
// first design activity. Every step is SKIPPED when its argument is absent, so the same
// op serves "create and start" and "add research to an existing project".
//
// projectID IS THE CREATE SIGNAL, and it is a *string rather than a *ProjectID on
// purpose. The REST route carries it in the BODY, not the path: the http generator
// routes any param whose schema $refs a scalar-string $def ending in "ID" into a path
// segment and ignores `pointer` (framework-go-http-generator httpgen/plan.go
// isIDPathParam), and net/http's mux cannot match an empty segment — so while it was a
// $ref the "absent id" that MEANS create was unreachable over REST and only the MCP
// surface could create a project. A plain string with x-go-name keeps it out of the
// path, which is the same idiom every other body-carried id in this contract uses
// (ProjectViewQuery.projectId, ReviewDecisionInput.optionId).
func (m *deliveryManager) StartProject(rc fwmanager.Context, owner OwnerScope, name string, projectID *string, model *OperatingModel, research *ResearchInput, start bool) (StartProjectResult, error) {
	var out StartProjectResult
	id := ProjectID("")
	if projectID == nil {
		if owner == "" {
			return out, newError(fwmanager.ContractMisuse, "deliveryManager.StartProject: a new project needs an owner")
		}
		if name == "" {
			return out, newError(fwmanager.ContractMisuse, "deliveryManager.StartProject: a new project needs a name")
		}
		created, err := m.CreateProject(rc, owner, name)
		if err != nil {
			return out, err
		}
		id = created
	} else {
		id = ProjectID(*projectID)
	}
	out.ProjectID = id
	if model != nil {
		v, err := m.SetOperatingModel(rc, id, *model)
		if err != nil {
			return out, err
		}
		out.Version = v
	}
	if research != nil {
		v, err := m.SetResearchInput(rc, id, *research)
		if err != nil {
			return out, err
		}
		out.Version = v
	}
	if out.Version == 0 {
		st, err := m.GetProject(rc, id)
		if err != nil {
			return out, err
		}
		out.Version = Version(st.Version)
	}
	if start {
		ref, err := m.startSystemDesign(rc, id)
		if err != nil {
			return out, err
		}
		out.Session = &ref
	}
	return out, nil
}

// startSystemDesign is the brand-new-project BOOTSTRAP, and since stage 4b1 Task 13 it
// starts the GENERIC CHILD for the reserved `requirements` activity rather than the
// retired SystemDesignPhaseWorkflow.
//
// WHY THE CHILD AND NOT THE PUMP. The pump selects from the COMMITTED activity list, and a
// brand-new project has none — slot 9 is written by M0, which is three activities away. The
// three design activities are RESERVED ids the derived plan always emits
// (requirements → architecture → projectDesign), so the bootstrap can name the first one
// without a plan; from its exit onward the pump's own eligibility (slot 10's
// requirements → architecture edge plus each child's dependsOn walk) carries the project.
//
// The pre-condition is unchanged: the project exists and its ResearchInput slot is PRESENT
// (a brand-new project with no row fails the same precondition — research has not been set),
// because the mission draft weaves the research into its prompt and a session started without
// it drafts from nothing.
//
// The id reuse policy is unchanged too, and for the same reason it was pinned before: a
// RUNNING activity is reused (idempotent start) and a CLOSED one is RESTARTED as a fresh run,
// which the child's own skip-if-committed seed makes safe — it marks every task whose slot is
// already committed passed and resumes at the first open one.
func (m *deliveryManager) startSystemDesign(rc fwmanager.Context, projectID ProjectID) (SessionRef, error) {
	ctx := rc.Context
	if projectID == "" {
		return "", newError(fwmanager.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isResearchReadNotFound(err) {
			return "", newError(fwmanager.FailedPrecondition, "research not populated (project has no state)")
		}
		return "", mapReadProjectError(err)
	}
	if proj.Research.IsZero() {
		return "", newError(fwmanager.FailedPrecondition, "research not populated")
	}
	activityID := ActivityID(bootstrapDesignActivityID)
	opts := client.StartWorkflowOptions{
		ID:                       deliveryActivityWorkflowID(projectID, activityID),
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindDeliveryActivity, deliveryActivityInput{
		ProjectID:  projectID,
		ActivityID: activityID,
		Activity: constructionActivity{
			ActivityID: string(activityID),
			Kind:       activityKindConstruction,
			Type:       projectstate.ActivityTypeRequirements,
		},
	})
	if err != nil {
		return "", mapStartError(err)
	}
	return newSessionRef(we.GetID()), nil
}

// bootstrapDesignActivityID is the FIRST of the three reserved design activity ids the
// derived plan emits, and the only one the bootstrap names: the pump takes over from its
// exit. It is a literal rather than a read of the plan because the bootstrap's whole premise
// is that there is no committed plan yet; projectstate.ClassifyActivity resolves the same
// three prefixes, which is what keeps this name and the plan's own agreeing.
const bootstrapDesignActivityID = "requirements"

// ---- op 2: ExecuteNextActivity ---------------------------------------------

// ExecuteNextActivity is the delivery pump's one tick, forwarded unchanged.
func (m *deliveryManager) ExecuteNextActivity(rc fwmanager.Context, projectID ProjectID, tickID string) (PumpResult, error) {
	if projectID == "" {
		return PumpResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if strings.TrimSpace(tickID) == "" {
		return PumpResult{}, newError(fwmanager.ContractMisuse, "empty tickId")
	}
	return m.cs.ExecuteNextActivity(rc, projectID, tickID)
}

// ---- op 3: DispatchActivityTask --------------------------------------------

// DispatchActivityTask asks for one task of one activity to be produced. Since stage 4b1
// Task 13 that is ONE call for every activity in the product: a `redraft` signal to the
// activity's live generic child carrying the TASK id, which the child's gate turns into a
// new attempt at revision n+1 — the same thing a send-back does, asked for directly.
//
// The three per-rail doors it replaces each signalled a workflow that no longer exists
// (RequestArtifactDraft's signal-with-start on {projectId}:{artifactKind}, and
// RequestSDPCommit's start of the SDP assembly). The lifecycle is still resolved, and
// deliberately: it is what makes an unknown activity a NotFound and an unclassifiable one a
// FailedPrecondition here, rather than a signal to an id nothing is running.
func (m *deliveryManager) DispatchActivityTask(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID string, feedback *ReviewFeedback) (SessionRef, error) {
	if err := requireActivityTask(projectID, activityID, taskID); err != nil {
		return "", err
	}
	lc, err := m.activityLifecycle(rc, projectID, activityID)
	if err != nil {
		return "", err
	}
	if _, ok := lifecycleTaskByID(lc, taskID); !ok {
		return "", newError(fwmanager.ContractMisuse,
			"deliveryManager.DispatchActivityTask: task "+taskID+" is not in activity "+string(activityID)+"'s lifecycle")
	}
	return m.cs.RedraftTask(rc, projectID, activityID, taskID, feedback)
}

// ---- op 4: SubmitReviewDecision --------------------------------------------

// SubmitReviewDecision is the single write behind every gate in the product, and since
// stage 4b1 Task 13 four of its five members are ONE writer for every rail: the generic
// child's gate ledger. Only `advance` still splits, because advancing a PHASE is a
// project-level transition and a construction activity has none — its gates advance its own
// task DAG.
//
// THE DEFECT THIS CLOSES, stated because it was live for four tasks: the two design arms
// signalled the retired per-kind co-author id and the M0 approve signalled the retired SDP
// assembly, and the pump stopped starting either of those workflows in Task 10. Every design
// gate — the M0 approve included — was therefore UNANSWERABLE: the signal went to an id
// nothing was running and the founder's decision vanished with a success.
func (m *deliveryManager) SubmitReviewDecision(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID string, decision ReviewDecisionInput, feedback *ReviewFeedback) error {
	if err := requireActivityTask(projectID, activityID, taskID); err != nil {
		return err
	}
	lc, err := m.activityLifecycle(rc, projectID, activityID)
	if err != nil {
		return err
	}
	if decision.Decision == ReviewAdvance {
		return m.advancePhaseForTask(rc, projectID, lc, taskID, deliveryDerefBool(decision.AcknowledgeStale))
	}
	// M0 HAS NO SEND-BACK (spec §6; stage 4b1 Task 9). The typed refusal lives HERE, at the
	// façade, because this is the one place the founder's decision arrives and therefore the
	// only place a refusal can be shown to them: the SPA has refused it since stage 5
	// (NO_SDP_SEND_BACK) and the server gives the SAME reason, so the screen and the API
	// cannot disagree about why. The generic child's gate refuses it a second time, for a
	// signal that arrives past this guard.
	//
	// Why a refusal and not an absorbed rejection: the plan is DERIVED. There is no draft to
	// re-run and no judged task to re-open, so a send-back has nothing to act on — it would
	// withdraw the round and strand the activity. Changing the plan means amending the
	// Architecture, which recomputes it.
	if taskID == sdpReviewTaskID && decision.Decision == ReviewReject {
		return newError(fwmanager.FailedPrecondition, "deliveryManager.SubmitReviewDecision: "+noSendBackAtM0)
	}
	kind := roundKindOfTask(lc, taskID)
	switch decision.Decision {
	case ReviewApprove:
		return m.cs.SubmitTaskDecision(rc, projectID, activityID, taskID, kind, ReviewApprove, optionIDOf(decision), feedback)
	case ReviewReject:
		return m.cs.SubmitTaskDecision(rc, projectID, activityID, taskID, kind, ReviewReject, nil, feedback)
	case ReviewSetCommentStatus:
		return m.cs.SetTaskCommentStatus(rc, projectID, activityID, taskID,
			deliveryDerefString(decision.CommentID), deliveryDerefString(decision.CommentStatus))
	case ReviewWithdraw:
		return m.cs.WithdrawReviewRound(rc, projectID, activityID, taskID, kind)
	case ReviewDecisionUnknown, ReviewAdvance:
		return newError(fwmanager.ContractMisuse, "deliveryManager.SubmitReviewDecision: unknown decision")
	}
	return newError(fwmanager.ContractMisuse, "deliveryManager.SubmitReviewDecision: unknown decision")
}

// optionIDOf reads ReviewDecisionInput's OPTIONAL optionId as the typed OptionID the M0
// approve carries. It is the ONE decision extra that is not a scalar the zero value already
// means: M0's approve names WHICH of the four project-design options the founder bought, and
// the child's gate stamps it on the round it decides.
func optionIDOf(decision ReviewDecisionInput) *OptionID {
	if decision.OptionID == nil {
		return nil
	}
	o := OptionID(*decision.OptionID)
	return &o
}

// advancePhaseForTask routes the `advance` decision onto the phase the TASK's artifact
// belongs to. A task that names no artifact kind is a construction task, and that refusal is
// not an IOU: advancing a phase is a project-level transition the design halves own.
func (m *deliveryManager) advancePhaseForTask(rc fwmanager.Context, projectID ProjectID, lc methodassets.Lifecycle, taskID string, acknowledgeStale bool) error {
	kind, ok := artifactKindForTask(lc, taskID)
	if !ok {
		return newError(fwmanager.ContractMisuse,
			"deliveryManager.SubmitReviewDecision: a construction activity has no phase to advance — its gates advance its own task DAG")
	}
	// The gating outcome is readable through QueryProjectView(summary); the PhaseAdvanceResult
	// is not part of the twelve-op surface.
	if phase1Kind(kind) {
		_, err := m.sealSystemDesignPhase(rc, projectID, acknowledgeStale)
		return err
	}
	_, err := m.AdvanceToConstruction(rc, projectID, acknowledgeStale)
	return err
}

// ---- op 5: AskQuestions ----------------------------------------------------

// AskQuestions records anchored questions against a task's review thread. Where the thread
// lives is the one thing that still splits, and it splits on DATA rather than on a rail: a
// task that names a design SLOT seeds the slot's own durable review ledger and dispatches the
// answer job the addressed role answers in place; a task that names none is a construction
// task, whose questions land on the activity's review ROUND (spec §5.3: an Ask is a
// ReviewComment.type = question), which is why it needs no artifact kind at all.
func (m *deliveryManager) AskQuestions(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID string, addressee string, questions []AnchoredComment) error {
	if err := requireActivityTask(projectID, activityID, taskID); err != nil {
		return err
	}
	if strings.TrimSpace(addressee) == "" {
		return newError(fwmanager.ContractMisuse, "empty addressee")
	}
	lc, err := m.activityLifecycle(rc, projectID, activityID)
	if err != nil {
		return err
	}
	kind, ok := artifactKindForTask(lc, taskID)
	if !ok {
		return m.cs.AskTaskQuestions(rc, projectID, activityID, taskID, addressee, nil, questions)
	}
	if phase1Kind(kind) {
		return m.askDesignQuestions(rc, projectID, kind, addressee, questions)
	}
	return m.askPlanQuestions(rc, projectID, kind, addressee, questions)
}

// ---- op 6: AcknowledgeStaleBasis -------------------------------------------

// AcknowledgeStaleBasis records the audited "reviewed — unaffected" acknowledgement that
// clears a committed artifact's stale-basis flag without re-opening it.
func (m *deliveryManager) AcknowledgeStaleBasis(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID string, note string) error {
	if err := requireActivityTask(projectID, activityID, taskID); err != nil {
		return err
	}
	if strings.TrimSpace(note) == "" {
		return newError(fwmanager.ContractMisuse, "acknowledging a stale basis requires a note")
	}
	lc, err := m.activityLifecycle(rc, projectID, activityID)
	if err != nil {
		return err
	}
	kind, ok := artifactKindForTask(lc, taskID)
	if !ok {
		// A CONSTRUCTION ACTIVITY HAS NO STALE BASIS TO ACKNOWLEDGE, and this refusal is
		// SEMANTIC rather than an IOU (stage 4b1 Task 12). StaleBasis is a field on an ARTIFACT
		// SLOT and the activity-scoped verb takes the ArtifactKind of the slot it clears; a
		// construction task names no artifact kind in any of the fourteen lifecycles (its work
		// products — srs, detailedDesign, construction, integration, stp — are the task's own
		// outputs, not one of the seventeen design SLOTS, so ArtifactKindFromWireName does not
		// resolve them and the round is kindless, which is exactly what ReviewRound.ArtifactKind's
		// optionality means). A construction activity's outputs are a commit plus
		// .serviceContracts/.phaseArtifacts entries, none of which carries a basis flag, so there
		// is nothing on this activity a note could clear — and clearing the Architecture's flag
		// from here would un-stale that slot for every OTHER activity too, which is the
		// architect's decision on the design half and not this activity's.
		//
		// The SPA agrees by construction: its chip renders only where a SLOT reports staleBasis
		// (ActivityExperienceContainer's staleSlot), so no construction screen can reach this
		// door. If a construction activity is ever given a basis of its own, it wants a field on
		// the row and a verb that names the activity, not this one.
		return newError(fwmanager.FailedPrecondition,
			"deliveryManager.AcknowledgeStaleBasis: task "+taskID+" of activity "+string(activityID)+
				" produces no committed artifact slot, so it has no stale basis to acknowledge — a stale BASIS is a property of a design slot")
	}
	if phase1Kind(kind) {
		return m.ackDesignStaleBasis(rc, projectID, kind, note)
	}
	return m.ackPlanStaleBasis(rc, projectID, kind, note)
}

// ---- op 7: SetProjectRunState ----------------------------------------------

// SetProjectRunState is the operator's pause/resume, as one op over an enum rather than
// two verbs. `paused` carries the reason; `running` clears the recorded pause.
func (m *deliveryManager) SetProjectRunState(rc fwmanager.Context, projectID ProjectID, runState ProjectRunState, reason string) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	// A pause is recorded WITH its reason (the construction rail's own precondition,
	// hoisted); a resume clears the recorded pause and carries none.
	if runState == ProjectPaused && strings.TrimSpace(reason) == "" {
		return newError(fwmanager.ContractMisuse, "pausing a project requires a reason")
	}
	switch runState {
	case ProjectPaused:
		return m.cs.PauseProject(rc, projectID, reason)
	case ProjectRunning:
		return m.cs.ResumeProject(rc, projectID)
	}
	return newError(fwmanager.ContractMisuse, "deliveryManager.SetProjectRunState: unknown run state")
}

// ---- op 8: OverrideActivity ------------------------------------------------

// OverrideActivity is the operator's steer on one escalated activity, forwarded
// unchanged.
func (m *deliveryManager) OverrideActivity(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, override ActivityOverride) error {
	if err := requireActivity(projectID, activityID); err != nil {
		return err
	}
	if strings.TrimSpace(override.Notes) == "" {
		return newError(fwmanager.ContractMisuse, "an operator override requires notes")
	}
	return m.cs.OverrideActivity(rc, projectID, activityID, override)
}

// ---- op 9: ReplanProject ---------------------------------------------------

// ReplanProject is the replan sweep's one tick, forwarded unchanged.
func (m *deliveryManager) ReplanProject(rc fwmanager.Context, projectID *ProjectID, tickID string) (ReplanSweepResult, error) {
	if strings.TrimSpace(tickID) == "" {
		return ReplanSweepResult{}, newError(fwmanager.ContractMisuse, "empty tickId")
	}
	return m.cs.RunReplanSweep(rc, projectID, tickID)
}

// ---- op 10: SetProjectExecutionPolicy --------------------------------------

// SetProjectExecutionPolicy sets the project's review policy. A preset alone names one
// of the shipped presets; an explicit policy overrides it field by field.
func (m *deliveryManager) SetProjectExecutionPolicy(rc fwmanager.Context, projectID ProjectID, policy ExecutionPolicyInput) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if policy.Policy == nil && strings.TrimSpace(policy.Preset) == "" {
		return newError(fwmanager.ContractMisuse, "an execution policy needs a preset or an explicit policy")
	}
	if policy.Policy == nil {
		return m.cs.SetReviewPolicy(rc, projectID, policy.Preset)
	}
	return m.cs.UpdateReviewPolicy(rc, projectID, *policy.Policy)
}

// ---- op 11: QueryProjectView -----------------------------------------------

// QueryProjectView is the ONE read of the twelve. Its kind selects which of the
// thirteen former readers answers, and the query object carries the selector that kind
// needs; a missing selector is a ContractMisuse that names it.
func (m *deliveryManager) QueryProjectView(rc fwmanager.Context, query ProjectViewQuery) (ProjectView, error) {
	out := ProjectView{Kind: query.Kind}
	switch query.Kind {
	case ProjectViewSummary:
		return m.queryProjectSummary(rc, query, out)
	case ProjectViewProjects:
		return m.queryProjectList(rc, query, out)
	case ProjectViewSession:
		return m.querySessionView(rc, query, out)
	case ProjectViewPump:
		return m.queryPumpView(rc, query, out)
	case ProjectViewDesignHealth:
		return m.queryDesignHealthView(rc, query, out)
	case ProjectViewEpisodes:
		return m.queryEpisodesView(rc, query, out)
	case ProjectViewTimeline:
		return m.queryTimelineView(rc, query, out)
	}
	return out, nil
}

// queryProjectSummary answers the `summary` kind: one project's head state.
func (m *deliveryManager) queryProjectSummary(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	id, err := requireProjectID(query, "summary")
	if err != nil {
		return out, err
	}
	st, err := m.GetProject(rc, id)
	if err != nil {
		return out, err
	}
	out.Summary = &st
	return out, nil
}

// queryProjectList answers the `projects` kind: every project of one owner.
func (m *deliveryManager) queryProjectList(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	if query.Owner == nil {
		return out, missingSelector("projects", "owner")
	}
	list, err := m.ListProjects(rc, *query.Owner)
	if err != nil {
		return out, err
	}
	out.Projects = list
	return out, nil
}

// queryPumpView answers the `pump` kind: whether the project's delivery pump is running.
func (m *deliveryManager) queryPumpView(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	id, err := requireProjectID(query, "pump")
	if err != nil {
		return out, err
	}
	st, err := m.cs.GetPumpStatus(rc, id)
	if err != nil {
		return out, err
	}
	out.Pump = &st
	return out, nil
}

// queryDesignHealthView answers the `designHealth` kind: the live Method-rule findings.
func (m *deliveryManager) queryDesignHealthView(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	id, err := requireProjectID(query, "designHealth")
	if err != nil {
		return out, err
	}
	dh, err := m.GetDesignHealth(rc, id)
	if err != nil {
		return out, err
	}
	out.DesignHealth = &dh
	return out, nil
}

// queryTimelineView answers the `timeline` kind: one episode's full trace.
//
// The three rails each carried a byte-identical GetEpisodeTimeline over the same
// episodeAccess; this is the one copy, and stage 4b1 Task 13 deleted the two the router
// never reached (they had been unreachable since the twelve-op surface landed).
func (m *deliveryManager) queryTimelineView(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	id, err := requireProjectID(query, "timeline")
	if err != nil {
		return out, err
	}
	if query.EpisodeID == nil {
		return out, missingSelector("timeline", "episodeId")
	}
	tl, err := m.cs.GetEpisodeTimeline(rc, id, *query.EpisodeID)
	if err != nil {
		return out, err
	}
	out.Timeline = &tl
	return out, nil
}

// querySessionView answers the `session` kind: an artifactKind selects the Phase-1 or
// Phase-2 design session by its phase, an activityId selects the construction session.
//
// THE TWO DESIGN MEMBERS ARE NOW DERIVED (stage 4b1 Task 13, R-J). They used to be a
// Temporal QUERY against the per-kind co-author workflow, with a Describe-first dance to
// synthesize an honest terminal for a run that had closed. That workflow is gone, so there is
// nothing to query — and nothing is lost, because the facts the view carries are durable: the
// STAGE comes from the slot's own review status and the thread comes from the slot's ledger,
// which is the derivation QueryActivityView already runs over .activityExecution. A committed
// slot renders committed, a withdrawn slot withdrawn, and a slot that is neither renders the
// honest draft-failed terminal rather than a "GENERATING" spinner nobody will ever satisfy.
//
// `constructionSession` still comes from the CHILD's live sessionState query, because that
// one is genuinely held in the running workflow: the walk's current stage, its reviewer set
// and its pipeline phase are not written to head state until they are decided.
func (m *deliveryManager) querySessionView(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	id, err := requireProjectID(query, "session")
	if err != nil {
		return out, err
	}
	if query.ArtifactKind != nil {
		if phase1Kind(*query.ArtifactKind) {
			v, err := m.designCompletedSessionView(rc.Context, id, *query.ArtifactKind)
			if err != nil {
				return out, err
			}
			out.Session = &v
			return out, nil
		}
		v, err := m.planCompletedSessionView(rc.Context, id, *query.ArtifactKind)
		if err != nil {
			return out, err
		}
		out.ProjectSession = &v
		return out, nil
	}
	if query.ActivityID != nil {
		act := ActivityID(*query.ActivityID)
		v, err := m.cs.GetSessionState(rc, id, &act)
		if err != nil {
			return out, err
		}
		out.ConstructionSession = &v
		return out, nil
	}
	v, err := m.cs.GetSessionState(rc, id, nil)
	if err != nil {
		return out, err
	}
	out.ConstructionSession = &v
	return out, nil
}

// queryEpisodesView answers the `episodes` kind: an artifactKind lists a design
// artifact's episodes, an activityId lists a construction activity's.
func (m *deliveryManager) queryEpisodesView(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	id, err := requireProjectID(query, "episodes")
	if err != nil {
		return out, err
	}
	if query.ArtifactKind != nil {
		recs, err := m.listArtifactEpisodes(rc, id, *query.ArtifactKind)
		if err != nil {
			return out, err
		}
		out.Episodes = recs
		return out, nil
	}
	if query.ActivityID == nil {
		return out, missingSelector("episodes", "artifactKind or activityId")
	}
	recs, err := m.cs.ListEpisodesForActivity(rc, id, *query.ActivityID)
	if err != nil {
		return out, err
	}
	out.Episodes = recs
	return out, nil
}

func requireProjectID(query ProjectViewQuery, kind string) (ProjectID, error) {
	if query.ProjectID == nil || *query.ProjectID == "" {
		return "", missingSelector(kind, "projectId")
	}
	return ProjectID(*query.ProjectID), nil
}

func missingSelector(kind, selector string) error {
	return newError(fwmanager.ContractMisuse,
		"deliveryManager.QueryProjectView: the "+kind+" view needs "+selector)
}

// ---- op 12: QueryActivityView ----------------------------------------------

// QueryActivityView is the Activity Experience's single read, forwarded unchanged.
func (m *deliveryManager) QueryActivityView(rc fwmanager.Context, projectID ProjectID, activityID ActivityID) (ActivityView, error) {
	if err := requireActivity(projectID, activityID); err != nil {
		return ActivityView{}, err
	}
	return m.cs.QueryActivityView(rc, projectID, activityID)
}

// ---- ONE worker, ONE queue -------------------------------------------------

// WorkerManifest is ONE manifest over ONE csWorkflows: the six surviving workflow entry
// functions under their unchanged names, ONE ActivityOptions hook, and one genActivities
// threading every dep of the merged contract.
//
// Stage 4a merged three manifests here and resolved each activity name against the three
// rails' hooks with construction last-wins. That merge is gone with the rails, and the ONE
// hook carries the two entries only the design hooks answered for plus the re-tuned
// scaffold-sync preset — see deliveryActivityOptions for the measurement.
func (m *deliveryManager) WorkerManifest() genWorkerManifest {
	return m.cs.WorkerManifest()
}

// deliveryDerefBool / deliveryDerefString read ReviewDecisionInput's OPTIONAL extras.
// The contract marks only `decision` required — presence-only, per the 2026-08-13
// strictness ruling — so the extras arrive as pointers and their absence is the zero
// value the forwarded op already means by it.
func deliveryDerefBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

func deliveryDerefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ===========================================================================
// THE SDP ASSEMBLY — moved here from assemblesdpreview.go when stage 4b1 Task 13
// deleted AssembleSDPReviewWorkflow (the retired Project-Design rail's entry).
//
// Everything below is PURE: the deterministic four-option assembly, the three
// Engine calls per option, the join into an SdpReview, the recommendation and the
// exclusion-zone rules. It is the generic child's compute:SdpReview strategy's own
// body (Task 9 split it out as assembleSdpReviewOver so BOTH the child and the
// retired rail could run the identical join; the rail's reader half went with the
// workflow). It lives in deliverymanager.go rather than deliveryactivity.go because
// not one line of it takes a workflow.Context — the file-layout rule (R-C) puts a
// pure helper with the Manager and a context-taker with the child.
// ===========================================================================

// assembleSdpReviewOver runs the three Engines per option and joins them into the
// SdpReview. Pure — no clock, no RNG, no I/O — unit-testable without Temporal and
// replay-safe, and it takes its four inputs BY VALUE so a caller that derived them (the
// generic child's compute) and a caller that read them off head-state (the retired rail)
// run the identical join.
//
// It returns the per-option RiskScore ALONGSIDE the review, because slot 15 needs
// criticality risk and activity risk SEPARATELY while an SdpOptionRow carries only the
// composite. Keeping the whole score here is what lets riskModelFrom be a join rather than
// a second run of the estimation Engine over the same four options.
//
// Iteration is projectstate.SolutionKinds() — a fixed slice — so the row order and the
// Engine call order are deterministic; the solutions map is only ever PROBED by that
// slice's members and never walked.
func assembleSdpReviewOver(
	eng sdpEngines,
	pa projectstate.PlanningAssumptions,
	al projectstate.ActivityList,
	nw projectstate.Network,
	solutions map[projectstate.ArtifactKind]*projectstate.Solution,
	feedback string,
) (*projectstate.SdpReview, map[projectstate.ArtifactKind]estimation.RiskScore, error) {
	rows := make([]projectstate.SdpOptionRow, 0, len(projectstate.SolutionKinds()))
	risks := make(map[projectstate.ArtifactKind]estimation.RiskScore, len(projectstate.SolutionKinds()))
	for _, kind := range projectstate.SolutionKinds() {
		sol := solutions[kind]
		if sol == nil {
			return nil, nil, sdpIncomplete(fwmanager.New(fwmanager.FailedPrecondition,
				"SDP prerequisite "+kind.String()+" is not committed"))
		}
		opt := assembleOption(kind, pa, al, nw, *sol)

		ce, eErr := eng.Estimation.EstimateForOption(fweng.Context{Context: context.Background()}, toEstimationOption(opt))
		if eErr != nil {
			return nil, nil, escalateEngine("estimationEngine", kind, eErr)
		}
		of, oErr := eng.OperationEst.EstimateForOption(
			fweng.Context{Context: context.Background()},
			toOperationOption(opt),
			toOperationUsage(opt.DeclaredUsage),
			operationestimation.InfrastructureKind(opt.InfrastructureKind),
		)
		if oErr != nil {
			return nil, nil, escalateEngine("operationEstimationEngine", kind, oErr)
		}
		proj2, pErr := eng.Settlement.ProjectCommitTimeRevenueShareAndComputeCost(fweng.Context{Context: context.Background()}, toSettlementOption(opt))
		if pErr != nil {
			return nil, nil, escalateEngine("settlementEngine", kind, pErr)
		}

		risks[kind] = ce.Risk
		rows = append(rows, projectstate.SdpOptionRow{
			OptionID:             opt.OptionID,
			SolutionKind:         kind,
			DurationDays:         ce.DurationDays,
			BuildCost:            toProjectStateMoneyFromEstimation(ce.BuildCost),
			CompositeRisk:        ce.Risk.Composite,
			ProjectedMonthlyCost: monthlyCostAtDeclaredLoad(of.UsageCostCurve),
			ExpectedPerCycleNet:  toProjectStateMoney(of.CostSensitivityForecast.ExpectedPerCycleCharge),
			RevenueSharePercent:  proj2.RevenueSharePercent,
		})
	}

	rec, rationale := recommendOption(rows)
	if feedback != "" {
		rationale = rationale + " (re-assembled with architect feedback: " + feedback + ")"
	}
	return &projectstate.SdpReview{Options: rows, Recommendation: rec, Rationale: rationale}, risks, nil
}

// toSettlementOption converts the canonical projectstate option to the
// settlementEngine's OWN ProjectOption snapshot at the call boundary (Option B full
// encapsulation: the Engine redefines every domain type it uses as its own generated
// def and imports no projectstate, so the Manager maps field-by-field here). The
// Engine reads only the option's settlement Terms, so only OptionID + Terms cross.
func toSettlementOption(opt projectstate.ProjectOption) billing.ProjectOption {
	t := opt.Terms
	return billing.ProjectOption{
		OptionID: billing.OptionID(opt.OptionID),
		Terms: billing.BillingTerms{
			RevenueShare:         billing.RevenueShareKind(t.RevenueShare),
			RevenueSharePercent:  t.RevenueSharePercent,
			ComputeCost:          billing.ComputeCostKind(t.ComputeCost),
			ComputeMarkupPercent: t.ComputeMarkupPercent,
			Schedule:             billing.ScheduleKind(t.Schedule),
		},
	}
}

// toOperationOption converts the canonical projectstate option to the
// operationEstimationEngine's OWN slim ProjectOption snapshot at the call boundary
// (Option B full encapsulation: the Engine redefines every domain type it uses as its
// own generated def and imports no projectstate, so the Manager maps field-by-field
// here). The Engine reads only the option's settlement Terms, so only OptionID + Terms
// cross.
func toOperationOption(opt projectstate.ProjectOption) operationestimation.ProjectOption {
	t := opt.Terms
	return operationestimation.ProjectOption{
		OptionID: operationestimation.OptionID(opt.OptionID),
		Terms: operationestimation.SettlementTerms{
			RevenueShare:         operationestimation.RevenueShareKind(t.RevenueShare),
			RevenueSharePercent:  t.RevenueSharePercent,
			ComputeCost:          operationestimation.ComputeCostKind(t.ComputeCost),
			ComputeMarkupPercent: t.ComputeMarkupPercent,
			Schedule:             operationestimation.ScheduleKind(t.Schedule),
		},
	}
}

// toOperationUsage converts the canonical declared-usage snapshot to the
// operationEstimationEngine's OWN UsageAssumption at the call boundary. The integer
// fields widen to int64 in the generated contract def.
func toOperationUsage(u projectstate.UsageAssumption) operationestimation.UsageAssumption {
	return operationestimation.UsageAssumption{
		ExpectedDailyActiveUsers: int64(u.ExpectedDailyActiveUsers),
		RequestsPerMinute:        u.RequestsPerMinute,
		AvgPayloadBytes:          int64(u.AvgPayloadBytes),
	}
}

// assembleOption builds one ProjectOption by value from the committed Phase-2 slots
// (contract §6.3 B step 2). DETERMINISTIC — no clock, no RNG; the activity ordering
// is preserved from the ActivityList so the Engine join replays identically.
func assembleOption(
	kind projectstate.ArtifactKind,
	pa projectstate.PlanningAssumptions,
	al projectstate.ActivityList,
	nw projectstate.Network,
	sol projectstate.Solution,
) projectstate.ProjectOption {
	onCritical := make(map[string]bool, len(nw.CriticalPath))
	for _, name := range nw.CriticalPath {
		onCritical[name] = true
	}

	classSet := map[string]struct{}{}
	activities := make([]projectstate.OptionActivity, 0, len(al.Activities))
	for _, a := range al.Activities {
		classSet[a.WorkerClass] = struct{}{}
		activities = append(activities, projectstate.OptionActivity{
			ActivityID:  a.Name,
			EffortDays:  a.EffortDays,
			WorkerClass: a.WorkerClass,
			// OnCriticalPath/RiskBucket are authored METADATA only — the estimationEngine
			// computes its OWN per-option critical path + float-based risk from the network
			// (Phase-2 rework F2/F3); it no longer trusts these fields for the math.
			OnCriticalPath: onCritical[a.Name],
			RiskBucket:     a.RiskBucket,
		})
	}
	classes := make([]string, 0, len(classSet))
	for c := range classSet {
		classes = append(classes, c)
	}

	// Calendar is a SHARED planning assumption for EVERY option (Phase-2 rework F5): the
	// per-option Solution.CalendarDaysPerWeek "cheat" (compressed silently switching 2→5
	// d/wk) is retired. Compression now comes from a higher StaffingCap (parallelism where
	// the network allows) + the 30% exclusion guard in recommendOption.
	//
	// EARMARK (F5e — deferred): the book's SECOND compression lever, "top resources" (a
	// model-tier upgrade, e.g. junior sonnet→opus, that shortens critical-activity effort
	// at higher $/day), is NOT modeled here — cap-based parallelism cannot shorten below
	// the unconstrained critical path. When implemented, the compressed option would carry
	// a per-class effort/throughput multiplier fed to the estimationEngine.
	return projectstate.ProjectOption{
		OptionID:     projectstate.OptionID(kind.String()),
		SolutionKind: kind,
		Network:      projectstate.ActivityNetwork{Activities: activities},
		// AI-derived per-class $/day rates (Phase-2 rework F11) — not the old flat human rates.
		WorkerMix:           projectstate.WorkerMix{ClassRates: deriveClassRates(pa, classes), StaffingCap: sol.StaffingCap},
		CalendarDaysPerWeek: pa.CalendarDaysPerWeek,
		Terms:               pa.Terms,
		DeclaredUsage:       pa.DeclaredUsage,
		InfrastructureKind:  pa.InfrastructureKind,
		Dependencies:        nw.Dependencies,
		Milestones:          nw.Milestones,
		IndirectDailyRate:   indirectDailyRateOf(pa),
		BufferDays:          sol.BufferDays,
		// Top-resource compression lever (F5e): >1 for the compressed option, which speeds
		// up its critical path (shorter + riskier) at a convex cost premium in the engine.
		CriticalSpeedup: sol.CriticalSpeedup,
	}
}

// monthlyCostAtDeclaredLoad picks the UsageCostCurve point nearest LoadMultiplier==1.0
// (the declared-usage point). Deterministic.
func monthlyCostAtDeclaredLoad(curve operationestimation.UsageCostCurve) projectstate.Money {
	best := operationestimation.Money{}
	bestDist := math.MaxFloat64
	for _, p := range curve.Points {
		d := math.Abs(p.LoadMultiplier - 1.0)
		if d < bestDist {
			bestDist = d
			best = p.ProjectedMonthlyCost
		}
	}
	// Convert the Engine's OWN Money back to the canonical projectstate.Money at the
	// boundary (Option B full encapsulation).
	return toProjectStateMoney(best)
}

// toProjectStateMoney converts the operationEstimationEngine's OWN Money back to the
// canonical projectstate.Money at the call boundary (Option B full encapsulation).
func toProjectStateMoney(m operationestimation.Money) projectstate.Money {
	return projectstate.Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
}

// toEstimationOption converts the canonical projectstate option to the
// estimationEngine's OWN SLIM ProjectOption snapshot at the call boundary
// (Option B full encapsulation: the Engine redefines every domain type it uses as its
// own generated def and imports no projectstate, so the Manager maps field-by-field
// here). The Engine reads only the construction-side network + worker mix + calendar,
// so only those (plus OptionID for audit) cross — the settlement Terms / declared usage
// / infra / solution kind do NOT. The generated WorkerMix.StaffingCap + OptionActivity.
// RiskBucket widen int → int64.
func toEstimationOption(opt projectstate.ProjectOption) estimation.ProjectOption {
	activities := make([]estimation.OptionActivity, 0, len(opt.Network.Activities))
	for _, a := range opt.Network.Activities {
		activities = append(activities, estimation.OptionActivity{
			ActivityId:     a.ActivityID,
			EffortDays:     a.EffortDays,
			WorkerClass:    a.WorkerClass,
			OnCriticalPath: a.OnCriticalPath,
			RiskBucket:     int64(a.RiskBucket),
		})
	}
	rates := make(map[string]estimation.Money, len(opt.WorkerMix.ClassRates))
	for cls, m := range opt.WorkerMix.ClassRates {
		rates[cls] = estimation.Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
	}
	deps := make([]estimation.NetworkDependency, 0, len(opt.Dependencies))
	for _, d := range opt.Dependencies {
		deps = append(deps, estimation.NetworkDependency{Activity: d.Activity, DependsOn: d.DependsOn})
	}
	milestones := make([]estimation.NetworkMilestone, 0, len(opt.Milestones))
	for _, m := range opt.Milestones {
		milestones = append(milestones, estimation.NetworkMilestone{Id: m.ID, DependsOn: m.DependsOn})
	}
	return estimation.ProjectOption{
		OptionId:            estimation.OptionID(opt.OptionID),
		Network:             estimation.ActivityNetwork{Activities: activities, Dependencies: deps, Milestones: milestones},
		WorkerMix:           estimation.WorkerMix{ClassRates: rates, StaffingCap: int64(opt.WorkerMix.StaffingCap)},
		CalendarDaysPerWeek: opt.CalendarDaysPerWeek,
		IndirectDailyRate:   estimation.Money{MinorUnits: opt.IndirectDailyRate.MinorUnits, Currency: opt.IndirectDailyRate.Currency},
		BufferDays:          opt.BufferDays,
		CriticalSpeedup:     opt.CriticalSpeedup,
	}
}

// toProjectStateMoneyFromEstimation converts the estimationEngine's OWN
// Money back to the canonical projectstate.Money at the call boundary (Option B full
// encapsulation).
func toProjectStateMoneyFromEstimation(m estimation.Money) projectstate.Money {
	return projectstate.Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
}

// Exclusion-zone bounds (App C §4.7g–i; the-method-risk-modeling Step 5). Options with
// composite risk above tooRisky or below overSafe are OUT; a compressed option more than
// maxCompression shorter than normal is OUT (death-zone proximity / >30% rule, F8).
const (
	riskTooRisky   = 0.75
	riskOverSafe   = 0.30
	maxCompression = 0.30
)

// recommendOption applies the App C exclusion zones across the option rows, then picks the
// best IN-band option (lowest CompositeRisk, tie-break lowest DurationDays) — this is the
// cost-risk sweet spot the book expects to land on the decompressed-normal (F8). If every
// option is out of band it falls back to the lowest-risk row so the recommendation is never
// empty (management still needs a pointer, with the caveat in the rationale).
func recommendOption(rows []projectstate.SdpOptionRow) (projectstate.OptionID, string) {
	if len(rows) == 0 {
		return "", "no options assembled"
	}
	normalDur := 0.0
	for _, r := range rows {
		if r.SolutionKind == projectstate.KindNormalSolution {
			normalDur = r.DurationDays
		}
	}
	included := func(r projectstate.SdpOptionRow) bool { return sdpOptionInBand(r, normalDur) }
	if best, ok := pickBestSdpOption(rows, included); ok {
		return best.OptionID, fmt.Sprintf(
			"recommend %s: lowest in-band composite risk (%.3f) at %.1f days (App C risk-crossover exclusions applied)",
			best.OptionID, best.CompositeRisk, best.DurationDays)
	}
	best, _ := pickBestSdpOption(rows, func(projectstate.SdpOptionRow) bool { return true })
	return best.OptionID, fmt.Sprintf(
		"recommend %s: ALL options fell outside the App C risk band [%.2f,%.2f]; picked lowest composite risk (%.3f) — review before committing",
		best.OptionID, riskOverSafe, riskTooRisky, best.CompositeRisk)
}

// sdpOptionInBand applies the App C exclusion zones to one row: composite risk outside
// [overSafe, tooRisky] is OUT, and a row compressed more than maxCompression below the
// normal option's duration is OUT (death-zone proximity / >30% rule, F8). normalDur==0
// (no normal row assembled) skips the compression bound — only the risk band applies.
func sdpOptionInBand(r projectstate.SdpOptionRow, normalDur float64) bool {
	if r.CompositeRisk > riskTooRisky || r.CompositeRisk < riskOverSafe {
		return false
	}
	if normalDur > 0 && r.DurationDays < normalDur {
		if (normalDur-r.DurationDays)/normalDur > maxCompression {
			return false
		}
	}
	return true
}

// pickBestSdpOption returns the pred-matching row with the lowest CompositeRisk,
// tie-broken by lowest DurationDays — the cost-risk sweet spot ordering recommendOption
// ranks by. found=false when no row matches pred.
func pickBestSdpOption(rows []projectstate.SdpOptionRow, pred func(projectstate.SdpOptionRow) bool) (projectstate.SdpOptionRow, bool) {
	var best projectstate.SdpOptionRow
	found := false
	for _, r := range rows {
		if !pred(r) {
			continue
		}
		if !found || r.CompositeRisk < best.CompositeRisk ||
			(r.CompositeRisk == best.CompositeRisk && r.DurationDays < best.DurationDays) {
			best = r
			found = true
		}
	}
	return best, found
}

// sdpIncomplete wraps a missing-prerequisite error as a non-retryable terminal.
func sdpIncomplete(cause error) error {
	return temporal.NewNonRetryableApplicationError(
		"sdp inputs incomplete: "+cause.Error(), "SDPInputsIncomplete", cause)
}

// escalateEngine wraps an Engine error as a non-retryable terminal (the option was
// mis-assembled or an engine invariant broke — neither is retryable).
func escalateEngine(engineName string, kind projectstate.ArtifactKind, cause error) error {
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("%s failed for option %s: %s", engineName, kind, cause.Error()),
		"SDPEngineError", cause)
}

// committedModel returns the committed typed model in the slot named by kind, or a
// FailedPrecondition error if the slot is not committed / not populated.
func committedModel(proj projectstate.Project, kind projectstate.ArtifactKind) (projectstate.ArtifactModel, error) {
	slot := pdSlotFor(proj, kind)
	if slot.Status != projectstate.ReviewCommitted || slot.Model == nil {
		return nil, fwmanager.New(fwmanager.FailedPrecondition,
			fmt.Sprintf("SDP prerequisite %s is not committed", kind))
	}
	return slot.Model, nil
}

func committedPlanningAssumptions(proj projectstate.Project) (projectstate.PlanningAssumptions, error) {
	m, err := committedModel(proj, projectstate.KindPlanningAssumptions)
	if err != nil {
		return projectstate.PlanningAssumptions{}, err
	}
	pa, ok := m.(*projectstate.PlanningAssumptions)
	if !ok {
		return projectstate.PlanningAssumptions{}, wrongModelType(projectstate.KindPlanningAssumptions, m)
	}
	return *pa, nil
}

func committedNetwork(proj projectstate.Project) (projectstate.Network, error) {
	m, err := committedModel(proj, projectstate.KindNetwork)
	if err != nil {
		return projectstate.Network{}, err
	}
	nw, ok := m.(*projectstate.Network)
	if !ok {
		return projectstate.Network{}, wrongModelType(projectstate.KindNetwork, m)
	}
	return *nw, nil
}

func committedSolution(proj projectstate.Project, kind projectstate.ArtifactKind) (projectstate.Solution, error) {
	m, err := committedModel(proj, kind)
	if err != nil {
		return projectstate.Solution{}, err
	}
	sol, ok := m.(*projectstate.Solution)
	if !ok {
		return projectstate.Solution{}, wrongModelType(kind, m)
	}
	return *sol, nil
}

func wrongModelType(want projectstate.ArtifactKind, got projectstate.ArtifactModel) error {
	gotKind := "nil"
	if got != nil {
		gotKind = got.Kind().String()
	}
	return fwmanager.New(fwmanager.ContractMisuse,
		fmt.Sprintf("expected a %s model, got %s", want, gotKind))
}

// airates.go derives each project option's per-worker-class build-cost rate from the AI
// rate card (Phase-2 estimation rework F11). Team members are AI AGENTS, not humans, so
// the old flat $800/$500 human day-rates are gone: an agent's cost is the LLM inference
// it burns per agent-day = expected tokens × the Claude API price for the model that
// agent runs.
//
//	rate($/day) = MegatokensInPerDay × price_in + MegatokensOutPerDay × price_out
//
// The role→model mapping is the source-of-truth agent roster in .claude/agents/*.md
// frontmatter (F11c). Phantom worker classes that map to no agent (architect,
// devops-agent, web-engineer-agent) are intentionally absent (F11d).
//
// Pure + deterministic (no clock, no RNG, no I/O) so the SDP assembly stays replay-safe.

// modelPrice is the Claude API price for one model, in USD MINOR UNITS (cents) per
// megatoken (MTok). Source: Anthropic price list (F11b) — fable $10/$50, opus $5/$25,
// sonnet $3/$15, haiku $1/$5 per MTok in/out.
type modelPrice struct {
	inCentsPerMTok  float64
	outCentsPerMTok float64
}

// apiPricing is the per-model Claude API price list, keyed by the frontmatter model id.
var apiPricing = map[string]modelPrice{
	"fable":  {inCentsPerMTok: 1000, outCentsPerMTok: 5000}, // $10 in / $50 out
	"opus":   {inCentsPerMTok: 500, outCentsPerMTok: 2500},  // $5 in / $25 out
	"sonnet": {inCentsPerMTok: 300, outCentsPerMTok: 1500},  // $3 in / $15 out
	"haiku":  {inCentsPerMTok: 100, outCentsPerMTok: 500},   // $1 in / $5 out
}

// priceFamily normalizes a model id to its apiPricing family key. The rate card's
// modelId is authored as a FULL API id ("claude-opus-4-8", "claude-haiku-4-5-20251001")
// while apiPricing is keyed by short family names — the exact-key lookup silently
// priced EVERY full id as sonnet (found live on gtdapp 2026-07-11: the opus architect
// class costed at sonnet rates). Substring match on the lowercased id; unknown ids
// keep the documented sonnet fallback via the caller's miss branch.
func priceFamily(modelID string) string {
	id := strings.ToLower(modelID)
	for _, fam := range [...]string{"fable", "opus", "sonnet", "haiku"} {
		if strings.Contains(id, fam) {
			return fam
		}
	}
	return id
}

// roleModel maps each worker CLASS (agent role) to the model it runs (F11c), taken
// verbatim from .claude/agents/*.md frontmatter. The phantom classes (architect,
// devops-agent, web-engineer-agent) are deliberately NOT here.
var roleModel = map[string]string{
	"system-architect": "fable",
	"project-manager":  "fable",
	"senior-developer": "opus",
	"product-manager":  "opus",
	"ui-designer":      "opus",
	"junior-developer": "sonnet",
	"qa-engineer":      "sonnet",
	"test-engineer":    "sonnet",
	"software-tester":  "sonnet",
	"ux-reviewer":      "sonnet",
}

// Default token throughput per agent-day (F11a). Kept uniform across classes so the cost
// SPREAD between classes comes purely from the model tier (fable roles are the most
// expensive per day, sonnet roles the cheapest). Tunable per-class via
// PlanningAssumptions.RateCard once the state pass authors it.
const (
	defaultMTokInPerDay  = 2.0 // ~2M input tokens / agent-day (context + tool results)
	defaultMTokOutPerDay = 0.5 // ~0.5M output tokens / agent-day (generated code + notes)
)

// defaultModelForClass returns the model a class runs, defaulting an UNKNOWN class (e.g.
// a stale "architect" fixture) to sonnet so rate derivation never fails a valid option.
func defaultModelForClass(class string) string {
	if m, ok := roleModel[class]; ok {
		return m
	}
	return "sonnet"
}

// defaultRateSpec returns the default AI rate spec for a class (uniform throughput on the
// class's mapped model).
func defaultRateSpec(class string) projectstate.WorkerRateSpec {
	return projectstate.WorkerRateSpec{
		ModelID:             defaultModelForClass(class),
		MegatokensInPerDay:  defaultMTokInPerDay,
		MegatokensOutPerDay: defaultMTokOutPerDay,
	}
}

// deriveClassRates computes the per-day build-cost rate for every worker class used by
// the option (F11b). It prefers the authored PlanningAssumptions.RateCard entry, falling
// back to the documented default spec for any class the card omits, so an option always
// assembles even before the state pass authors the card. Deterministic: the output map
// is keyed by class; iteration order is irrelevant.
func deriveClassRates(pa projectstate.PlanningAssumptions, classes []string) map[string]projectstate.Money {
	rates := make(map[string]projectstate.Money, len(classes))
	for _, class := range classes {
		spec, ok := pa.RateCard[class]
		if !ok || spec.ModelID == "" {
			spec = defaultRateSpec(class)
		}
		rates[class] = rateForSpec(spec)
	}
	return rates
}

// rateForSpec turns a rate spec into a USD/day Money via the Claude API price list. An
// unknown model id falls back to sonnet pricing (never panics). Deterministic integer
// truncation (no rounding-mode ambiguity) matches the estimationEngine's cost math.
func rateForSpec(spec projectstate.WorkerRateSpec) projectstate.Money {
	price, ok := apiPricing[priceFamily(spec.ModelID)]
	if !ok {
		price = apiPricing["sonnet"]
	}
	cents := spec.MegatokensInPerDay*price.inCentsPerMTok + spec.MegatokensOutPerDay*price.outCentsPerMTok
	return projectstate.Money{MinorUnits: int64(cents), Currency: "USD"}
}

// defaultIndirectDailyRate is the overhead burn per calendar day used when
// PlanningAssumptions.IndirectDailyRate is unset (F6). $50/day (5000 cents USD) — the
// platform/orchestration overhead that accrues over the schedule regardless of which
// agents are active. Makes a longer (subcritical) option demonstrably costlier.
var defaultIndirectDailyRate = projectstate.Money{MinorUnits: 5000, Currency: "USD"}

// indirectDailyRateOf returns the authored indirect rate, or the documented default when
// unset.
func indirectDailyRateOf(pa projectstate.PlanningAssumptions) projectstate.Money {
	if pa.IndirectDailyRate.MinorUnits != 0 || pa.IndirectDailyRate.Currency != "" {
		return pa.IndirectDailyRate
	}
	return defaultIndirectDailyRate
}

// ===========================================================================
// THE CONSTRUCTION SPINE'S SURVIVING PURE HALF — moved here from
// constructactivity.go with the deletion of ConstructActivityWorkflow. Nothing
// below takes a workflow.Context, which is the file-layout rule (R-C) that split
// the move in two: a context-taker goes to deliveryactivity.go beside the child,
// a pure helper goes here beside the Manager.
//
// isGitLocalVenue is in this half, and it matters: R6's SINGLE recognition point
// for the deterministic local venue, called from railLifecycleEnabled OUTSIDE the
// file it used to live in. constructRepoTarget is here for the same reason — since
// Task 10 it is the venue resolver for BOTH the design and the construction
// dispatch, not one rail's.
// ===========================================================================
// pipelineSpec is the Manager's infrastructure-neutral dispatch spec.
type pipelineSpec struct {
	ProjectID   ProjectID
	ActivityID  string
	ComponentID string
	RepoURL     string
	Ref         string
	// Phase is the ActivityMethodPhase.String() for the current activity phase — which is the
	// SAME wire string a lifecycle phase's id carries, so the generic child passes its
	// tc.Phase.ID here unconverted.
	Phase string
	// Command is the slash command this dispatch runs, when the CALLER already holds it. The
	// generic child does: it is the lifecycle task's own `command` field (stage 4b1 Task 11).
	// Empty means "re-derive it from Type/Variant/Phase", which is the retired flat walk, and
	// dispatchInputsFor is where that fallback lives.
	Command string
	// Type/Variant are the activity's classification, COPIED from the dispatched
	// constructionActivity (classified once by the pump) rather than re-derived from
	// the id here. dispatchInputsFor resolves the slash command from this pair, so the
	// command is by construction drawn from the same pair the phase profile came from.
	Type    projectstate.ActivityType
	Variant projectstate.TestingVariant
	// OperatorNote is the rendered block of the operator notes this agent dispatch carries
	// (renderOperatorNotes, plan B1.4); empty when none is pending.
	OperatorNote string
}

// csPipelineObservation is the Manager's neutral pipeline observation.
type csPipelineObservation struct {
	Phase      PipelinePhase
	Diagnostic string
	// RunURL is the dispatched run's URL when the bound agenticJobAccess realisation
	// resolved one. It is carried for ONE reason here (construction has no run-link
	// view): it is the only VENUE signal a workflow ever sees — the GitHub-Actions arm
	// stamps the run's html URL on every observation, while the local executor and the
	// dry-run stub never set it. See episodeVenueIsRemote.
	RunURL string
	// Episode is the terminal run's captured agentic-episode summary (SP1 capture-seam):
	// the tokens/turns/tools the dispatched agent actually burned. Nil on every
	// non-terminal observation, on the GitHub-Actions arm (which mines no episode in
	// v1), on a non-agentic job (the local merge job spawns no agent), and — legitimately
	// — on a CANCELLED run's FIRST terminal observation, whose summary lands only once
	// the subprocess has unwound (see awaitLateEpisode).
	Episode *agenticjob.EpisodeSummary
}

// constructWorkflowFileName is the per-project CONSTRUCTION workflow file the agentic
// construction job dispatches into (the gh-mode venue switch, B5). It rides on the
// contract PipelineSpec.WorkflowFile alongside the per-project TargetRepo; the RA's
// resolveTarget falls back to the configured central construction workflow file when
// this (and TargetRepo) is zero. The scaffold seats this file in every app repo (B4);
// archistrator's own repo keeps its hand-maintained copy (self-hosting divergence).
const constructWorkflowFileName = "aiarch-construct.yml"

// constructRepoTarget resolves the per-project construction venue: it runs the injected
// Repo resolver and DECODES the opaque RepoRef into the RA's infrastructure-neutral
// RepoTarget{Owner,Name} + the construct workflow file. A nil/unresolving resolver
// yields a ZERO RepoTarget + empty workflow file, so submitPipeline leaves the contract
// fields zero and the RA falls back to the configured central construction repo (the
// pre-B5 legacy behavior, preserved for unresolvable projects). A malformed RepoRef
// surfaces the RA's ContractMisuse — decoded via sourcecontrol's own OwnerRepo accessor
// so the RepoRef encoding stays owned by sourceControlAccess (no encoding leak here).
func (wf *csWorkflows) constructRepoTarget(projectID ProjectID) (agenticjob.RepoTarget, string, error) {
	if wf.Repo == nil {
		return agenticjob.RepoTarget{}, "", nil
	}
	repoRef, ok := wf.Repo(projectID)
	if !ok {
		return agenticjob.RepoTarget{}, "", nil
	}
	// A GITLOCAL ref is not a construction venue (stage 4a fix round 1). Recognising it
	// here reproduces the pre-collapse nil resolver byte-for-byte (zero RepoTarget, empty
	// workflow file ⇒ the RA falls back to the configured central construction repo)
	// without splitting the dep back into two. Round 2 gave railLifecycleEnabled the same
	// recognition, so on a local boot the dispatch AND the rail lifecycle agree.
	if isGitLocalVenue(projectID, repoRef) {
		return agenticjob.RepoTarget{}, "", nil
	}
	owner, name, err := sourcecontrol.RepoRefOwnerRepo(repoRef)
	if err != nil {
		return agenticjob.RepoTarget{}, "", err
	}
	return agenticjob.RepoTarget{Owner: owner, Name: name}, constructWorkflowFileName, nil
}

// isGitLocalVenue reports whether a resolved RepoRef is the DESIGN rails' deterministic
// GitLocal venue for this project — the local profile's filesystem repo, which is not a
// construction venue and carries no PR-rail lifecycle for construction. It is the ONE
// place the recognition lives: the dispatch target (constructRepoTarget) and the rail
// lifecycle (railLifecycleEnabled) must never disagree about it, and the encoding stays
// owned by sourceControlAccess (the ref is compared against what its own pure resolver
// mints, never parsed here).
func isGitLocalVenue(projectID ProjectID, repoRef sourcecontrol.RepoRef) bool {
	return repoRef == sourcecontrol.GitLocalRepoRefForProject(sourcecontrol.ProjectID(projectID))
}

// dispatchInputsFor builds the DispatchInputs bag for a construction pipeline dispatch.
// The `command` input is the thin slash-command the workflow runs, so the workflow itself
// holds no routing logic. component_id is a Manager-resolved passthrough. (Moved
// workflow-side from the retired pipelineAdapter — it only reads workflow state +
// projectstate.CommandFor.)
//
// WHERE THE COMMAND COMES FROM, and why there are two answers for one commit-range (stage
// 4b1 Task 11). The generic child reads the LIFECYCLE TASK's own `command` field and passes
// it on the spec — the platform's data is the source of truth for what a task runs. The
// retired flat walk has no task, only a phase, so it still re-derives from the activity's
// CARRIED type/variant (classified once by the pump). The two agree by construction —
// CommandFor is itself a lookup into the same lifecycle data, pinned by
// Test_ConstructionCommands_MatchTheLifecycleData — and the fallback dies with the flat walk
// in Task 13.
func dispatchInputsFor(spec pipelineSpec) map[string]string {
	m := map[string]string{
		"activity_id":  spec.ActivityID,
		"component_id": spec.ComponentID,
	}
	if spec.Phase != "" {
		m["phase"] = spec.Phase
		m["command"] = spec.Command
		if m["command"] == "" {
			m["command"] = projectstate.CommandFor(spec.Type, spec.Variant, projectstate.ActivityMethodPhase(spec.Phase))
		}
	}
	// The operator's steer rides ONLY when a note is pending (B1.4): every no-note
	// dispatch's inputs stay byte-identical to before, so a seated workflow that predates
	// the operator_note input still accepts them. Both arms read the same key: the
	// GitHub arm passes it through as the construct workflow's input, the local arm
	// stamps it into the aiarch-state rig.
	if spec.OperatorNote != "" {
		m[dispatchInputOperatorNote] = spec.OperatorNote
	}
	return m
}

// dispatchInputOperatorNote is the dispatch input carrying the operator's notes. Like the
// other inputs it is a bare literal: the seated construct workflow template is the source
// of truth for the wire key (agenticjob's own constant documents it on the RA side).
const dispatchInputOperatorNote = "operator_note"

// managerPipelinePhase maps the contract PipelinePhase onto the Manager-neutral
// PipelinePhase (mapped here so a future re-order is safe). Moved workflow-side from the
// retired pipelineAdapter.
func managerPipelinePhase(p agenticjob.PipelinePhase) PipelinePhase {
	switch p {
	case agenticjob.PhasePending:
		return PipelinePending
	case agenticjob.PhaseRunning:
		return PipelineRunning
	case agenticjob.PhaseSucceeded:
		return PipelineSucceeded
	case agenticjob.PhaseFailed:
		return PipelineFailed
	case agenticjob.PhaseCancelled:
		return PipelineCancelled
	default:
		return PipelinePhaseUnknown
	}
}

// csEpisodeIDSeed is the deterministic, replay-stable seed a GAP record's EpisodeID is
// built from — the dispatch handle (unique per dispatch, and already in workflow
// history) with the activity id as the fallback for a zero handle.
func csEpisodeIDSeed(handle pipelineHandle, in constructActivityInput) string {
	if handle.Name != "" {
		return handle.Name
	}
	return string(in.ActivityID)
}

// nextTaskAttempt returns the next 1-based attempt number for t, the Figure A-1 task a
// dispatch's episode attributes to (see runPipeline / runMergePipeline). It is the SAME
// counter across an activity's outer variance retries AND a gated phase's human-paced
// redrafts — both re-enter runPipeline for the SAME phase — so it is the single source
// of the attempt number projectstate.AttemptID needs (Task 10). Lazily initialized so a
// constructState built without ever dispatching a pipeline (ProjectSupervisionWorkflow's)
// allocates nothing. workflow-local and rebuilt deterministically on replay; it starts
// from the row's attempt ledger (seedResumeFromLedger), and no live writer appends to
// that ledger yet.
func (s *constructState) nextTaskAttempt(t projectstate.MethodTask) int {
	if s.taskAttempts == nil {
		s.taskAttempts = map[projectstate.MethodTask]int{}
	}
	s.taskAttempts[t]++
	return s.taskAttempts[t]
}

// gitforward.go is the WORKFLOW-LEVEL wiring of the git-forward (branch→PR→CI→+1→
// merge) lifecycle into the per-activity construction spine (C-MCN-GIT; D-PA-GIT §5).
// It is the ONLY place that composes the two seams the constructionManager alone
// touches: the PR rail (sourceControlAccess / IPullRequestRail) and the per-activity
// git head-state mirror (projectStateAccess §GIT-HEAD-STATE). The division of labor
// (D-PA-GIT §5):
//
//   - the rail OWNS the git provider interaction (cut branch, open PR, read CI,
//     relay +1, perform merge) and RETURNS opaque handles + a status reflection;
//   - this Manager receives the opaque returns and MIRRORS them onto the head-state
//     via the additive Record* verbs;
//   - projectStateAccess stores the opaque strings + typed CI enum — it never calls
//     the rail (RA-never-calls-RA).
//
// The merge AUTHORITY split is preserved: interventionEngine DECIDES when to merge
// (the existing variance machinery), the Manager PERFORMS it here. The +1 is the
// architect's in-app approval; the existing reviewEngine fan-out is the technical
// review and is unchanged — the git +1 relay is the SEPARATE, audit-worthy human
// architecture sign-off the head-state records.
//
// CRASH-SAFETY / IDEMPOTENCY: every rail call is on a deterministic name (idempotent
// in the rail) and every Record* goes through applyRecovering — the workflow-level
// Conflict re-read→re-apply loop (§6.5) — with the per-Activity idempotency key, so a
// workflow retry re-running any step is a no-op (the rail's deterministic-name
// idempotency + the git store's dedup-first ledger). The cred is minted ONCE per
// activity lifecycle and threaded into every rail + record verb.

// gitForward is the per-activity git-lifecycle state the spine carries across its
// steps. It is workflow-local (rebuilt deterministically on replay) and holds the
// opaque handles the rail returned + the credential the Manager minted. headVersion
// is shared with the non-git transition records (read-your-writes; §6.5) — the caller
// passes a pointer to the spine's headVersion so both record families advance one
// monotonic token.
type gitForward struct {
	enabled   bool
	repoRef   sourcecontrol.RepoRef
	cred      railCredEnvelope
	branch    string
	branchRef string
	prRef     string
	crLabel   string
	isRevert  bool
}

// gitEnabled reports whether the git-forward slice is wired AND a repo resolves for
// this project. When false the spine runs unchanged (the live Postgres-store
// composition that predates the GitStore).
func (wf *csWorkflows) gitEnabled(projectID ProjectID) (sourcecontrol.RepoRef, bool) {
	if wf.GitStatus == nil || wf.Repo == nil || !wf.RailEnabled(projectID) {
		return sourcecontrol.RepoRef(""), false
	}
	return wf.Repo(projectID)
}

// activityBranchName derives the provider-neutral per-activity branch name
// "activity/<activityID>" (D-PA-GIT GIT.1 example). Deterministic in the activity id.
func activityBranchName(activityID ActivityID) string {
	return "activity/" + string(activityID)
}

// prTitle / prBody are the human-facing PR text the Manager's sequence owns.
func prTitle(activityID ActivityID) string {
	return fmt.Sprintf("aiarch: construction activity %s", activityID)
}

func prBody(activity constructionActivity) string {
	return fmt.Sprintf("Automated construction of component %s (%s, layer %s).",
		activity.ComponentID, activityKindName(activity.Kind), activity.Layer)
}

// activityKindName returns the canonical activity-kind name — a free function over
// the Manager-owned activityKind enum (the schema-first rule keeps enum types
// method-free, so the Stringer behaviour lives here). Produces the IDENTICAL strings
// the former handoff.ActivityKind Stringer did (PR body text — zero behavior change).
func activityKindName(k activityKind) string {
	switch k {
	case activityKindUnknown:
		// zero-value sentinel, not a real activity kind.
		return "Unknown"
	case activityKindDetailedDesign:
		return "DetailedDesign"
	case activityKindConstruction:
		return "Construction"
	case activityKindIntegration:
		return "Integration"
	case activityKindNoncoding:
		return "Noncoding"
	}
	// Unreachable for the five defined activityKind values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a defensive
	// fallback for an out-of-range ordinal.
	return "Unknown"
}

// archApprovalBody is the +1 relay's review body — the architect's in-app
// architecture sign-off relayed onto the PR.
func archApprovalBody(activityID ActivityID) string {
	return fmt.Sprintf("architecture +1 relayed for %s", activityID)
}

// crLabelHints encodes the cr-NN change-request group label into the rail's opaque
// PullRequestSpec.Hints (labels ride in Hints, not a first-class field —
// sourcecontrol.go §3). Empty label ⇒ nil hints.
func crLabelHints(crLabel string) []byte {
	if crLabel == "" {
		return nil
	}
	return []byte(crLabel)
}

// csPullRequestStatusView is the Manager-local Activity-boundary projection of the
// rail's PullRequestStatus (a reflection the Manager feeds interventionEngine — NOT a
// gate). CheckRollup is the provider-neutral CI rollup the git head-state mirrors.
type csPullRequestStatusView struct {
	CheckRollup   projectstate.CICheckState
	ApprovalCount int
	Mergeable     bool
}

// mapCheckState maps the rail's CheckState onto the git head-state's provider-neutral
// CICheckState (the two enums are aligned-by-identity, mapped here so a future re-order
// is safe). A DUMB reflection — it never gates any Approve control.
func mapCheckState(s sourcecontrol.CheckState) projectstate.CICheckState {
	switch s {
	case sourcecontrol.CheckPending:
		// explicit: pending check state maps directly, same as any unmapped value.
		return projectstate.CICheckPending
	case sourcecontrol.CheckSuccess:
		return projectstate.CICheckSuccess
	case sourcecontrol.CheckFailure:
		return projectstate.CICheckFailure
	default:
		return projectstate.CICheckPending
	}
}

// adapters.go holds the bridges between the Manager's OWN broader domain vocabulary
// (constructionActivity, this component's generated façade ReviewSet/Reviewer) and
// each dependency's PUBLISHED contract shape, for the calls that are NOT identity —
// either because the Manager's own type carries strictly more fields than the Engine
// needs, or because the target is this component's
// OWN generated public façade type with a real field-shape divergence
// (reviewSetFromEngine), or because the Manager derives a real config value from raw
// composition-root config (constructionInterventionPolicy).
//
// The two Engines (intervention.InterventionEngine / review.ReviewEngine) have NO
// adapter STRUCT — the workflow calls their published contracts DIRECTLY (workflow.go /
// signals.go), with fweng.Context{Context: context.Background()} supplied inline at each
// call site.

// ===========================================================================
// reviewEngine — reviewSetFromEngine bridges the published review.ReviewSet/Reviewer
// onto THIS component's OWN generated façade ReviewSet/Reviewer (contract.gen.go,
// off-limits — DO NOT EDIT). A REAL divergence, not an identity mirror: the façade's
// Reviewer.ReferenceArtifact is *string (optional, omitempty) while the Engine's own
// Reviewer.ReferenceArtifact is a plain string (empty ⇒ none) — the nil/empty-string
// boundary is exactly the kind of zero-value divergence that must be bridged
// explicitly, not cast.
// ===========================================================================

func reviewSetFromEngine(set review.ReviewSet) ReviewSet {
	reviewers := make([]Reviewer, 0, len(set.Reviewers))
	for _, r := range set.Reviewers {
		cr := Reviewer{
			Role:        r.Role,
			Perspective: r.Perspective,
			MayAmend:    r.MayAmend,
		}
		if r.ReferenceArtifact != "" {
			ref := r.ReferenceArtifact
			cr.ReferenceArtifact = &ref
		}
		reviewers = append(reviewers, cr)
	}
	// The gate verdict rides onto the façade beside the roster: the two are one answer
	// from one call. Both are *T on the façade (additive optional properties), so a
	// pre-stage-2 client that never reads them decodes unchanged.
	human, reason := set.RequiresHuman, set.Reason
	return ReviewSet{Reviewers: reviewers, RequiresHuman: &human, Reason: &reason}
}

// maxVarianceAttempts bounds the dispatch→review→variance supervision loop
// before the Engine's Escalate/Takeover must terminate it.
const maxVarianceAttempts = 10

// maxPhaseRedrafts bounds a gated phase's human-paced SendBack redraft budget —
// SEPARATE from maxVarianceAttempts. SendBack is NOT a variance: it redrafts THIS
// phase in place; on exhaustion the gate keeps awaiting the human (it never
// re-enters the variance loop or fails the activity).
const maxPhaseRedrafts = 5

// The observe-poll SCHEDULE is the ONE ladder in deliveryactivity.go (observeInterval,
// maxObserveTotalPolls) — R-L, stage 4b1. The ceiling is unchanged and so is the escalation:
// a stuck pipeline still exhausts the budget and routes through handleVariance. Only the
// cadence moved, from a flat 15s x 240 to 4x15s + 9x60s + 10x300s — the same hour, 23 polls
// instead of 240, because one child now holds eleven of these loops instead of one.

// ===========================================================================
// ConstructActivityWorkflow — the per-activity UC3 spine (constructionManager.md
// §6.3). Loop/supervise until exited.
// ===========================================================================

// constructActivityInput is the start payload for the per-activity child workflow.
type constructActivityInput struct {
	ProjectID  ProjectID
	ActivityID ActivityID
	Activity   constructionActivity
}

// seedResumeFromLedger seeds a run's start state from its activity's stored row, read the
// way every other reader reads it (architect (D), D.1.3):
//   - completedPhases from projectstate.ResolvePhaseCompletions over the activity's profile:
//     the attempt ledger decides every phase it has decided (a passed gate completes the
//     phase, a rejected one leaves it incomplete), and a phase whose gate has no attempt
//     is a phase nothing is claimed about.
//   - taskAttempts from the highest attempt number the ledger records per task, so the
//     next dispatch of a task is attempt n+1 and its AttemptID (the episode TargetRef)
//     never collides with one the ledger already holds. A GATE task is counted off BOTH
//     ledgers (stage 3): its number is shared by its attempt and its review round, so a
//     resume that looked only at the attempts could mint a round id the review ledger
//     already holds — and OpenReviewRound is idempotent on that id, so the second gate
//     occurrence would silently vanish into the first.
//
// Pure over values already in workflow history (the snapshot's recorded readProject).
func seedResumeFromLedger(state *constructState, act constructionActivity, acs projectstate.ActivityExecution) {
	profile := projectstate.ProfileFor(act.Type, act.Variant)
	for _, pc := range projectstate.ResolvePhaseCompletions(profile, acs.Attempts) {
		if pc.Completed {
			state.completedPhases[pc.Phase] = true
		}
	}
	for _, a := range acs.Attempts {
		seedTaskCount(state, a.Task, a.Attempt)
	}
	for _, r := range acs.Reviews {
		seedTaskCount(state, r.TaskID, int(r.Round))
	}
}

// seedTaskCount raises the run's per-task counter to n when the ledger has gone further.
func seedTaskCount(state *constructState, task projectstate.MethodTask, n int) {
	if state.taskAttempts == nil {
		state.taskAttempts = map[projectstate.MethodTask]int{}
	}
	if n > state.taskAttempts[task] {
		state.taskAttempts[task] = n
	}
}

// takeoverGateKey is the awaitingGate an escalation waits at (the operator steers with
// OverrideActivity; no phase decision closes it).
const takeoverGateKey = "takeover"

// The closed outcome vocabulary a human stage ends in (the metric's outcome tag and the
// log line's). An override adds its kind: "override:retry", "override:skip", ….
const (
	gateOutcomeApproved          = "approved"
	gateOutcomeSentBack          = "sentBack"
	gateOutcomeSentBackExhausted = "sentBackExhausted"
	gateOutcomeTimedOut          = "timedOut"
	gateOutcomeOverridePrefix    = "override:"
)

// gateMetrics is the handler the gate-wait timer records through: the workflow's own
// metrics handler, which the SDK suppresses on replay (the OTel handler the composition
// root wires). A package-level seam only so a test can capture what is recorded — SDK
// v1.44's test environment exposes no metrics hook.
var gateMetrics = workflow.GetMetricsHandler

// humanGateClass is the bounded gate tag: phase, merge or takeover.
func humanGateClass(gate string) string {
	switch gate {
	case mergeGateKey:
		return "merge"
	case takeoverGateKey:
		return takeoverGateKey
	default:
		return "phase"
	}
}

// mergeGateKey is the gate key the local merge hold suspends on. It is NOT an
// ActivityMethodPhase and it takes Approve only (a merge has no draft to send back —
// validateTaskDecision refuses anything else); the operator releases it through
// SubmitReviewDecision addressed at this key as the task.
const mergeGateKey = "merge"

// ---------------------------------------------------------------------------
// Operator notes (plan B1.4). A note is PENDING from the moment it is recorded until an
// agent dispatch carries it: every pending note rides the NEXT agent dispatch of the
// activity and is stamped delivered to that dispatch's AttemptID (the key its episode
// carries as TargetRef). Recording, carrying, stamping and the scaffold sync are all
// behind ONE change id, so an execution is wholly old or wholly new.
//
// WHAT "DELIVERED" MEANS (B1 fix round, I1). Only a note the dispatch carried IN FULL is
// stamped. When the pending notes exceed maxRenderedOperatorNotesBytes, the OLDEST are
// withheld so the newest steer arrives whole; the block says how many were withheld, and
// they stay pending for a later attempt.
//
// DELIVERY IS AT-LEAST-ONCE (M4). The stamp follows a successful submit, and is its own
// write with its own bounded retry (noteStampRetryWindow). If it still fails, the run
// goes on — the job is already dispatched — and the note stays pending, so the next
// attempt carries it again: an agent may see one note twice, never zero times. A note
// is never carried twice into the SAME attempt (constructState.carriedTo), and the store
// refuses to stamp one note to two attempts.
//
// PENDING NOTES WITH NO DISPATCH AHEAD (M5). A retry whose phases are all complete (the
// local merge only), a takeover the operator finishes by hand, and a skip start no agent
// run, so their notes are kept and not stamped. They are neither expired nor dropped:
// they stay pending on the activity (the console counts them), and ride the activity's
// next agent dispatch if one ever comes (a re-queue). A skip note is never pending.
// ---------------------------------------------------------------------------

// changeOperatorNoteDelivery is the version marker gating note record/carry/stamp and
// the managed-scaffold sync before a GitHub-venue dispatch.
const changeOperatorNoteDelivery = "operator-note-delivery"

// ---------------------------------------------------------------------------
// THE EXECUTION LEDGER (stage 3). Until now this workflow wrote no attempt and no
// review data at all: nextTaskAttempt minted attempt numbers nothing recorded, every
// roster the review engine computed was thrown away once it had been displayed, and a
// send-back left a one-line OperatorNote — no roster, no verdict, no thread, no round
// number, no subject. This is where each of those becomes a real write, through
// activityExecutionAccess's twelve verbs.
//
// ONE CHANGE ID FOR ALL OF IT. Every write below is a Temporal Activity, so the command
// sequence moves at five points, and they are one feature: an execution is either on it
// or off it. Five ids would admit a half-fenced execution that opens a round and never
// decides it. The same argument changeLedgerPartialResume already makes by reusing its
// const at two call sites.
//
// WHAT THE OLD RAIL STOPS DOING BEHIND THE FENCE, so no fact is written twice:
//   - RecordActivityStarted  → OpenActivity (same StartedAt/type/variant, plus the pin)
//   - RecordPhaseCompleted   → the PASSED gate attempt this workflow now writes itself
//     (that verb's whole body is the synthesis of exactly that attempt)
//   - RecordActivityExited / RecordActivityFailed / RecordActivityCompleted
//     → RecordActivityOutcome (the fold of all three)
//   - the NoteSendBack record → the round, with the feedback carried to the redraft
//     workflow-locally (carrySendBackFeedback)
//
// RecordPhaseStarted and RecordChangeReviewed keep being called on BOTH paths: task 3
// retired their bodies in place, so they now write no fact at all and cannot duplicate
// one. Stage 4 deletes them with the fixtures that record them.
// ---------------------------------------------------------------------------

// changeExecutionLedger is the ONE version marker gating every execution-ledger write.
const changeExecutionLedger = "execution-ledger-writes"

// lifecyclePinFor is the lifecycle an activity's task DAG is resolved against for the
// whole of its run: the type key the row's profile is keyed on, and the method-assets
// release that key was read out of. Without it a release landing mid-flight re-shapes an
// activity that is already running, and every attempt and round already on the ledger
// would be read back against a DAG they were never written under.
func lifecyclePinFor(act constructionActivity) projectstate.LifecyclePin {
	return projectstate.LifecyclePin{
		TypeKey:       projectstate.LifecycleKeyFor(act.Type, act.Variant),
		AssetsVersion: methodassets.Version(),
	}
}

// attemptOutcomeFor maps a terminal pipeline phase onto the attempt's terminal. Only a
// SUCCEEDED run passed; everything else — failed, cancelled, or a phase that is not
// terminal at all (the poll budget ran out with the run still going) — is a failed
// attempt, because the task it was dispatched for did not get done.
func attemptOutcomeFor(p PipelinePhase) projectstate.TaskOutcome {
	switch p {
	case PipelineSucceeded:
		return projectstate.OutcomePassed
	case PipelineFailed, PipelineCancelled, PipelinePending, PipelineRunning, PipelinePhaseUnknown:
		return projectstate.OutcomeFailed
	}
	// Unreachable for the six defined PipelinePhase values above; kept as a defensive
	// fallback for an out-of-range ordinal, which is a failure like any other.
	return projectstate.OutcomeFailed
}

// gateSubjectRef names WHAT this round judges — and it must differ from round to round, or
// the ledger cannot say which revision each round looked at.
//
// THE DEFECT THIS REPLACES: the pull request is per-ACTIVITY, so rounds 1 and 2 of one gate
// both cited gf.prRef and the ledger claimed one subject for two different drafts. The fix
// is to name the COMMIT — stagedRef, the ref StageTaskOutput returned for the work task
// this round judges, which advances with every redraft.
//
// The ladder, most specific first — the SAME RULE the design rails' designSubjectRef runs
// (a shared rule, not a shared signature: the two rails have different fallbacks):
//   - a staged ref ⇒ SubjectCommit. The honest answer, and the only rung that moves per
//     revision. EMPTY until the generic child stages construction output; stage 4b1 Task 11
//     fills it, and this signature is widened now so that task re-parameterises nothing.
//   - else a live PR ⇒ SubjectPullRequest. Coarse, but it is a handle a reviewer can open,
//     and it is what the rail had.
//   - else the work ATTEMPT ⇒ SubjectArtifact. Joins to both ledgers and to the episode
//     that burned it.
func gateSubjectRef(gf *gitForward, stagedRef, workAttemptID string) projectstate.SubjectRef {
	if stagedRef != "" {
		return projectstate.SubjectRef{Kind: projectstate.SubjectCommit, Ref: stagedRef}
	}
	if gf.enabled && gf.prRef != "" {
		return projectstate.SubjectRef{Kind: projectstate.SubjectPullRequest, Ref: gf.prRef}
	}
	return projectstate.SubjectRef{Kind: projectstate.SubjectArtifact, Ref: workAttemptID}
}

// roundReviewers is the roster the round persists: the engine's rows, plus the human row
// when the policy requires a person. The engine's rows are NOT Required — the reviewer
// set is advisory in v1 and nothing dispatches it, and a row marked required that nothing
// waits for would make the round claim a gate it never had.
//
// Actor is the role for an engine row because on this rail a role IS the agent charter
// dispatched for it; the human row has no name to give, so it carries the operator the
// platform can honestly attribute the decision to.
func roundReviewers(set ReviewSet) []projectstate.RoundReviewer {
	out := make([]projectstate.RoundReviewer, 0, len(set.Reviewers)+1)
	for _, r := range set.Reviewers {
		out = append(out, projectstate.RoundReviewer{Role: r.Role, Actor: r.Role, Required: false})
	}
	if set.RequiresHuman != nil && *set.RequiresHuman {
		out = append(out, projectstate.RoundReviewer{Role: gateRoleHuman, Actor: gateActorOperator, Required: true})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// roundComments re-types a decision's anchored comments onto the round thread's. The
// ids, the round number and the open/answered status are the store's to mint, so only
// what the operator actually wrote travels.
func roundComments(in []AnchoredComment) []projectstate.ReviewComment {
	if len(in) == 0 {
		return nil
	}
	out := make([]projectstate.ReviewComment, 0, len(in))
	for _, c := range in {
		out = append(out, projectstate.ReviewComment{Anchor: c.JSONPath, Text: c.Text, AuthorRole: gateRoleHuman})
	}
	return out
}

// gateEvidence maps the round's subject onto the attempt's evidence vocabulary, so the
// UI's click dispatch opens the right thing from either ledger.
func gateEvidence(subject projectstate.SubjectRef) (projectstate.EvidenceKind, string) {
	switch subject.Kind {
	case projectstate.SubjectPullRequest, projectstate.SubjectCommit:
		return projectstate.EvidenceGit, subject.Ref
	case projectstate.SubjectArtifact:
		return projectstate.EvidenceArtifact, subject.Ref
	}
	// An unset subject (a round this workflow did not open) cites nothing.
	return projectstate.EvidenceNone, ""
}

// owedSendBackRound is the latest round on the phase's gate WHEN the next dispatch still
// owes it feedback: it was sent back, and nothing has been re-dispatched since.
//
// "since" is read off the ledger rather than remembered, because the run that would
// remember it is the one that died. The send-back verdict names the work attempt it
// judged, so a LATER attempt at that work task is the one honest signal that the redraft
// already went out — whoever sent it. That is also the delivered-once guard: once the
// redraft has run, its attempt outranks the judged one and the steer is not carried twice.
func owedSendBackRound(acs projectstate.ActivityExecution, lifecyclePhase projectstate.ActivityMethodPhase) (projectstate.ReviewRound, bool) {
	gate, work := projectstate.GateTaskFor(lifecyclePhase), projectstate.AgentTaskFor(lifecyclePhase)
	if gate == "" || work == "" {
		return projectstate.ReviewRound{}, false
	}
	var latest projectstate.ReviewRound
	found := false
	for _, r := range acs.Reviews {
		if r.TaskID == gate && (!found || r.Round > latest.Round) {
			latest, found = r, true
		}
	}
	if !found || latest.Outcome != projectstate.RoundSentBack {
		return projectstate.ReviewRound{}, false
	}
	return latest, !redraftDispatched(acs, work, latest)
}

// redraftDispatched reports whether the ledger holds a RESOLVED work attempt later than
// the one the round judged. The judged attempt is found by the id the verdict names; the
// round NUMBER is the fallback, since one counter mints both and they cannot drift.
//
// RESOLVED, not merely present, and the distinction is the whole point. runPipeline opens
// the attempt BEFORE the dispatch it describes, so a later attempt sitting pending is
// exactly the run that died on the way out — the redraft never reached an agent and the
// steer is still owed. Counting it as delivered is how the feedback would be lost in the
// one case this guard exists for.
//
// A run that died while the redraft was actually RUNNING leaves the same pending attempt
// and will carry the note again. That is at-least-once, which is the delivery contract
// operator notes already state: an agent may see one note twice, never zero times.
func redraftDispatched(acs projectstate.ActivityExecution, work projectstate.MethodTask, r projectstate.ReviewRound) bool {
	judged := int(r.Round)
	for _, a := range acs.Attempts {
		if a.AttemptID != "" && a.AttemptID == sendBackJudgedAttempt(r) {
			judged = a.Attempt
			break
		}
	}
	for _, a := range acs.Attempts {
		if a.Task == work && a.Attempt > judged && a.Outcome != projectstate.OutcomePending {
			return true
		}
	}
	return false
}

// sendBackJudgedAttempt is the AttemptID the round's send-back verdict named.
func sendBackJudgedAttempt(r projectstate.ReviewRound) string {
	for _, v := range r.Verdicts {
		if v.Verdict == projectstate.VerdictSendBack {
			return v.AttemptID
		}
	}
	return ""
}

// roundFeedback rebuilds the operator's steer from a decided round: the send-back
// verdict's summary, and the comments on its thread that are still OPEN — which is the
// spec's own definition of pending feedback (§5.3), now that it is answered from the
// round rather than from a note.
func roundFeedback(r projectstate.ReviewRound) *ReviewFeedback {
	fb := &ReviewFeedback{}
	for _, v := range r.Verdicts {
		if v.Verdict == projectstate.VerdictSendBack {
			fb.Notes = v.Summary
			break
		}
	}
	for _, c := range r.Thread {
		if c.Status == projectstate.ReviewCommentOpen {
			fb.Comments = append(fb.Comments, AnchoredComment{JSONPath: c.Anchor, Text: c.Text})
		}
	}
	return fb
}

// maxRenderedOperatorNotesBytes caps the rendered notes block one dispatch carries, far
// under GitHub's 65,535-character workflow_dispatch input cap. The façade caps one note
// (maxOperatorNoteRunes, and maxOperatorNoteBodyBytes once rendered), so one note always
// fits whole: only several pending notes can reach the cap.
const maxRenderedOperatorNotesBytes = 16 << 10

// noteFramingReserveBytes is the room kept for one note's header line and the
// withheld-notes line, so a note the façade accepted always fits the block whole.
const noteFramingReserveBytes = 1 << 10

// maxOperatorNoteBodyBytes caps one note's rendered body (its text and anchored comments,
// as renderNoteBody writes them) at the façade.
const maxOperatorNoteBodyBytes = maxRenderedOperatorNotesBytes - noteFramingReserveBytes

// noteStampRetryWindow bounds the delivery stamp's own retry envelope (M4): attempts are
// uncapped inside it, and past it the run goes on with the note still pending.
const noteStampRetryWindow = 2 * time.Minute

// noteFeedback is one note's operator text and anchored comments, from a send-back's
// feedback or an override.
type noteFeedback struct {
	text     string
	comments []AnchoredComment
}

// feedbackText is a send-back's note; a nil feedback (a signal that bypassed the
// façade) is an empty note, which recordOperatorNote skips.
func feedbackText(f *ReviewFeedback) noteFeedback {
	if f == nil {
		return noteFeedback{}
	}
	return noteFeedback{text: f.Notes, comments: f.Comments}
}

// overrideNoteKind maps an override onto the note kind it records.
func overrideNoteKind(k OverrideKind) (projectstate.OperatorNoteKind, bool) {
	switch k {
	case OverrideRetry:
		return projectstate.NoteRetry, true
	case OverrideTakeover:
		return projectstate.NoteTakeover, true
	case OverrideReassign:
		return projectstate.NoteReassign, true
	case OverrideSkip:
		return projectstate.NoteSkip, true
	case OverrideUnknown:
		return projectstate.OperatorNoteKindUnknown, false
	}
	return projectstate.OperatorNoteKindUnknown, false
}

// operatorNoteID is a note's deterministic id: the activity, this run, and the run's
// note sequence — replay-stable, and unique across runs of the same activity.
func operatorNoteID(activityID ActivityID, runID string, seq int) string {
	return fmt.Sprintf("%s:note:%s:%d", activityID, runID, seq)
}

// noteComments re-types a decision's anchored comments onto the note's.
func noteComments(in []AnchoredComment) []projectstate.NoteComment {
	if len(in) == 0 {
		return nil
	}
	out := make([]projectstate.NoteComment, 0, len(in))
	for _, c := range in {
		out = append(out, projectstate.NoteComment{JSONPath: c.JSONPath, Text: c.Text})
	}
	return out
}

// renderedNotes is what one dispatch carries: the block, the notes it carries IN FULL
// (the only ones stamped delivered), and how many older notes the cap withheld.
type renderedNotes struct {
	block    string
	whole    []projectstate.OperatorNote
	withheld int
}

// notesSeparator sits between two notes, and after the withheld-notes line.
const notesSeparator = "\n\n"

// renderOperatorNotes renders the pending notes as the one block a dispatch carries,
// oldest first, each headed by its id, kind and gate. Within maxRenderedOperatorNotesBytes
// it keeps the NEWEST notes whole and withholds the oldest, naming how many it withheld.
// No notes render nothing.
//
// A single note the block cannot hold whole (only a signal that bypassed the façade's
// maxOperatorNoteBodyBytes can carry one) is carried cut and marked, and is NOT among the
// whole notes: it stays pending rather than be stamped delivered in part.
func renderOperatorNotes(notes []projectstate.OperatorNote) renderedNotes {
	if len(notes) == 0 {
		return renderedNotes{}
	}
	sections := make([]string, len(notes))
	for i, n := range notes {
		sections[i] = renderNoteSection(n)
	}
	start, total := len(notes), 0
	for i := len(notes) - 1; i >= 0; i-- {
		add := len(sections[i])
		if start < len(notes) {
			add += len(notesSeparator)
		}
		framing := 0
		if i > 0 {
			framing = len(withheldNotesLine(i)) + len(notesSeparator)
		}
		if total+add+framing > maxRenderedOperatorNotesBytes {
			break
		}
		total += add
		start = i
	}
	var b strings.Builder
	if start == len(notes) {
		last := len(notes) - 1
		if last > 0 {
			b.WriteString(withheldNotesLine(last) + notesSeparator)
		}
		b.WriteString(sections[last])
		return renderedNotes{block: cutRenderedOperatorNotes(b.String()), withheld: last}
	}
	if start > 0 {
		b.WriteString(withheldNotesLine(start) + notesSeparator)
	}
	b.WriteString(strings.Join(sections[start:], notesSeparator))
	return renderedNotes{block: b.String(), whole: notes[start:], withheld: start}
}

// renderNoteSection renders one note: its header line, then its body.
func renderNoteSection(n projectstate.OperatorNote) string {
	header := fmt.Sprintf("[operator note %s — %s", n.NoteID, operatorNoteKindName(n.Kind))
	if n.Gate != "" {
		header += " at " + n.Gate
	}
	return header + "]\n" + renderNoteBody(n.Text, n.Comments)
}

// renderNoteBody renders a note's text and its anchored comments — the part the façade
// caps at maxOperatorNoteBodyBytes.
func renderNoteBody(text string, comments []projectstate.NoteComment) string {
	var b strings.Builder
	b.WriteString(text)
	for _, c := range comments {
		fmt.Fprintf(&b, "\n  comment on %s: %s", c.JSONPath, c.Text)
	}
	return b.String()
}

// withheldNotesLine opens a block that withholds the n oldest pending notes.
func withheldNotesLine(n int) string {
	if n == 1 {
		return "[1 older operator note is not shown: the notes exceed the 16 KiB one dispatch carries. It stays pending and rides a later attempt; every note is on the activity.]"
	}
	return fmt.Sprintf("[%d older operator notes are not shown: the notes exceed the 16 KiB one dispatch carries. They stay pending and ride a later attempt; every note is on the activity.]", n)
}

// renderedNotesTruncated ends a block cut at maxRenderedOperatorNotesBytes.
const renderedNotesTruncated = "\n[truncated: this operator note exceeds 16 KiB; it stays pending, and the full note is on the activity]"

// cutRenderedOperatorNotes cuts s to maxRenderedOperatorNotesBytes on a rune boundary,
// marking the cut.
func cutRenderedOperatorNotes(s string) string {
	if len(s) <= maxRenderedOperatorNotesBytes {
		return s
	}
	cut := maxRenderedOperatorNotesBytes - len(renderedNotesTruncated)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + renderedNotesTruncated
}

// operatorNoteKindName is a note kind's wire word, for the rendered block.
func operatorNoteKindName(k projectstate.OperatorNoteKind) string {
	switch k {
	case projectstate.NoteSendBack:
		return "sendBack"
	case projectstate.NoteRetry:
		return "retry"
	case projectstate.NoteTakeover:
		return "takeover"
	case projectstate.NoteReassign:
		return "reassign"
	case projectstate.NoteSkip:
		return "skip"
	case projectstate.NoteRequeue:
		return "requeue"
	case projectstate.OperatorNoteKindUnknown:
		return "unknown"
	}
	return "unknown"
}

// proposeReviewSet is the Manager's SINGLE review decision point: it hands the engine
// the activity's TYPE, the lifecycle phase's wire name, the committed policy document
// and the floor flag, and gets back the whole answer — the roster, whether a human must
// sign off, and the one-line reason. The call is to the PURE published
// review.ReviewEngine, directly (deterministic, replay-safe).
//
// The (activity type, lifecycle phase) → review kind table MOVED into
// internal/engine/review when ProposeReviews took the activity type (spec 2026-09-20
// §5.4, stage 2), taking the "no component degrades to a sign-off" rule with it; the
// engine owns the whole table now, so the Manager cannot pass a kind the engine does
// not know.
//
// The contracts are sourced from the start-snapshot project (B5). The architectureGraph
// parameter is GONE: every production call passed "" and the v1 policy ignored it — a
// parameter that is always empty is a lie the compiler cannot catch (earmark: feed the
// committed SystemDesign through `contracts`' successor when the reviewer set becomes
// enforcing). reviewSetFromEngine (adapters.go) bridges the Engine's own ReviewSet onto
// this component's generated façade ReviewSet (contract.gen.go) — a real divergence,
// not an identity mirror.
func (wf *csWorkflows) proposeReviewSet(in constructActivityInput, lifecyclePhase methodassets.LifecyclePhase, policy projectstate.ReviewPolicy, state *constructState) (ReviewSet, error) {
	change := review.ReviewChange{ActivityID: string(in.ActivityID), ComponentID: in.Activity.ComponentID}
	set, err := wf.Review.ProposeReviews(fweng.Context{Context: context.Background()},
		change, review.ActivityType(in.Activity.activityTypeName()), lifecyclePhase.ID,
		in.Activity.ComponentID, engineReviewPolicy(policy), state.floorTouched, state.reviewContracts)
	if err != nil {
		return ReviewSet{}, err
	}
	return reviewSetFromEngine(set), nil
}

// snapshotContractKeys derives the deterministic (sorted) set of contract identifiers
// from the start-snapshot project's committed service contracts — the display input
// for the gate's reviewer set. Sorted so the derived slice is replay-stable (map
// iteration order is randomized).
func snapshotContractKeys(p projectstate.Project) []string {
	if len(p.ServiceContracts) == 0 {
		return nil
	}
	keys := make([]string, 0, len(p.ServiceContracts))
	for k := range p.ServiceContracts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// deriveFailureReason maps a terminal pipeline phase + neutral diagnostic to the
// head-state FailureReason: a cancelled run → PipelineCancelled; a timed-out
// diagnostic (the RA's neutralDiagnostic for timed_out / the poll-budget exhaustion
// synthetic) → PipelineTimedOut; otherwise PipelineFailed.
func deriveFailureReason(phase PipelinePhase, diagnostic string) projectstate.FailureReason {
	if phase == PipelineCancelled {
		return projectstate.PipelineCancelled
	}
	if strings.Contains(diagnostic, "timed out") || strings.Contains(diagnostic, "did not reach a terminal phase") {
		return projectstate.PipelineTimedOut
	}
	return projectstate.PipelineFailed
}

// operatorOverrideSignal is the operatorOverride payload (constructionManager.md
// §2.4). Delivered to the per-activity child {projectId}:{activityId}.
type operatorOverrideSignal struct {
	Override ActivityOverride
	// TaskID names the TASK the override is aimed at, for the generic child's signal
	// router (deliveryactivity.go). It is the field that fixes a real hole the walk's
	// concurrency creates: two tasks in flight means two coroutines, and a shared
	// ReceiveChannel hands each message to exactly ONE of them, so an override meant for a
	// gate would be eaten by a polling sibling and silently lost. Empty on the retired
	// rail, whose walk is sequential and reads the channel directly.
	//
	// EARMARK: OverrideActivity's façade signature names an ACTIVITY and no task, so the
	// Manager cannot fill this until Task 12 widens it; until then an override reaching the
	// child from the façade names no task and is LOGGED loudly rather than broadcast.
	TaskID string
}

// ===========================================================================
// WHAT SURVIVED THE CO-AUTHOR SPINE (pure half) — moved here from
// coauthorartifact.go with CoAuthorArtifactWorkflow's deletion. railCredEnvelope is
// the load-bearing one: every credential-bearing RA call the child makes takes it,
// and its toProjectState half already lived in this file.
// ===========================================================================
// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// raContractMisuseErrType is the canonical Temporal Type() the read Activities surface when
// the committed state DECODES MALFORMED (a closed-enum field carrying free prose, a type
// mismatch) — the projectstate codec now classifies these ContractMisuse (terminal) rather
// than Infrastructure (QA F36). On a pure READ path there is no bad-argument misuse to
// confuse it with (the addressed absence is NotFound), so a ContractMisuse from a read-back
// is unambiguously a decode-of-committed-state failure.
var raContractMisuseErrType = fwmanager.RAErrType(fwra.ContractMisuse)

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// redraftSignal is the redraft signal payload — the "Retry draft" lever delivered
// to a CoAuthorArtifactWorkflow suspended in the StageRefused recovery gate
// (requestArtifactDraft's retry path). Feedback is the optional re-request feedback
// woven into the next draft dispatch.
type redraftSignal struct {
	Feedback *ReviewFeedback
	// TaskID names the TASK to re-draft, for the generic child's signal router
	// (deliveryactivity.go). Empty on both design rails, which hold one session per
	// artifact kind; Task 12's DispatchActivityTask is the sender that fills it.
	TaskID string
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
func signalNotes(f *ReviewFeedback) string {
	if f != nil {
		return f.Notes
	}
	return ""
}

// designPipelinePhase maps the RA's phase to the manager's neutral phase, preserving
// the Cancelled terminal distinctly (the design Manager treats any non-Succeeded
// terminal as a StageDraftFailed gate).
func designPipelinePhase(p agenticjob.PipelinePhase) pipelinePhase {
	switch p {
	case agenticjob.PhasePending:
		return pipelinePending
	case agenticjob.PhaseRunning:
		return pipelineRunning
	case agenticjob.PhaseSucceeded:
		return pipelineSucceeded
	case agenticjob.PhaseFailed:
		return pipelineFailed
	case agenticjob.PhaseCancelled:
		return pipelineCancelled
	default:
		return lPipelinePhaseUnknown
	}
}

// pipelinePhase mirrors agenticJobAccess.md §3 — the infrastructure-
// neutral lifecycle phase the Manager branches on. The terminal trio drives the
// observe loop's exit + the failure path.
type pipelinePhase int

const (
	lPipelinePhaseUnknown pipelinePhase = iota
	pipelinePending
	pipelineRunning
	pipelineSucceeded
	pipelineFailed
	pipelineCancelled
)

// IsTerminal reports whether the phase is one the job can no longer leave.
func (p pipelinePhase) IsTerminal() bool {
	switch p {
	case pipelineSucceeded, pipelineFailed, pipelineCancelled:
		return true
	case lPipelinePhaseUnknown, pipelinePending, pipelineRunning:
		return false
	default:
		return false
	}
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// maxLateEpisodePolls bounds the EXTRA observe polls a CANCELLED run is given before its
// episode is written off as a gap. Cancel flips the RA's phase SYNCHRONOUSLY while the
// agent subprocess is still unwinding, so a cancelled run's FIRST terminal observation
// legitimately carries no summary — it appears on a later poll.
const maxLateEpisodePolls = 4

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// lateEpisodePollInterval spaces the late-episode grace polls. DELIBERATELY tighter than
// the business poll interval: the wait is pure bookkeeping, but the workflow is blocked on
// it, so a cancelled run would otherwise sit visibly "generating" for a further minute
// before landing at its failure gate. Five seconds comfortably clears the executor's own
// subprocess wait, and four of them cap the whole grace window at 20s.
const lateEpisodePollInterval = 5 * time.Second

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeVenueIsRemote reports whether this observation came from the REMOTE
// (GitHub-Actions) venue, which mines no episode summary in v1. The run URL is the only
// venue fact an observation carries: the Actions arm stamps it, and neither the local
// executor nor the dry-run stub ever does. A GH run whose URL the RA could not resolve
// therefore reads as local and earns a gap — deliberately the safe direction (a visible,
// labelled gap beats a silent loss).
func episodeVenueIsRemote(runURL string) bool {
	return runURL != ""
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// railCredEnvelope carries the opaque short-lived credential across the Activity
// boundary. The Bytes are write-only at every consumer (never logged); they ride the
// Temporal payload exactly as the rail returns them.
type railCredEnvelope struct {
	Bytes     []byte
	ExpiresAt time.Time
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
func (c railCredEnvelope) toRail() sourcecontrol.RepoCredential {
	return sourcecontrol.RepoCredential{Bytes: c.Bytes, ExpiresAt: c.ExpiresAt}
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// mainBranch is the flat git-forward base every design PR targets (op-concepts §15).
const mainBranch = "main"

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// setCommentStatusSignal is the SetReviewCommentStatus signal payload. It rides the
// signalSetCommentStatus channel to the CoAuthorArtifactWorkflow suspended at the
// AwaitingReview gate, which applies the branch mutation (open|answered->resolved /
// resolved->open).
type setCommentStatusSignal struct {
	CommentID string
	Status    string
	// TaskID names the TASK whose round holds this comment. The generic child's signal
	// router keys every payload on it (deliveryactivity.go): a walk runs several gates at
	// once, and a status message with no task cannot be forwarded to one of them.
	//
	// The design rails leave it EMPTY and are unaffected — they hold one session per
	// artifact kind and read the shared channel directly — so this is additive and an
	// older buffered signal decodes it as "". EARMARK: SetReviewCommentStatus's façade
	// signature carries no task, so the Manager cannot fill it until Task 12 gives the
	// twelve ops a task id; until then the child's drop path is reachable from the façade
	// and is LOGGED loudly rather than refused.
	TaskID string
}

// reviewerUtteranceRole is the role stamped on a REPLY the human reviewer files into an
// existing thread. It differs from reviewAuthorRole ("architect", the role stamped on the
// comments the reviewer OPENS) because the derive rule reads it: projectstate.isReviewerRole
// treats "architect" and "pm" as AGENT roles, so a reviewer reply stamped "architect" would
// leave the thread reading as answered by its own author. Design §3.2 names this role.
const reviewerUtteranceRole = "architect-user"

// partitionIncomingComments is the thread-INDEPENDENT half of the split: it sorts one batch
// into the entries that open a thread (no replyTo) and the utterances that answer one, with
// no knowledge of which threads exist. Kept separate so a caller that must know the shape of
// a batch BEFORE it reads the ledger (AskQuestions derives its idempotency key and its
// emptiness refusal up front) partitions once and runs checkReplyTargets against the thread
// it later reads — without duplicating the reply-stamping rule.
func partitionIncomingComments(incoming []AnchoredComment, at string) ([]AnchoredComment, []projectstate.ReviewReply) {
	var fresh []AnchoredComment
	var replies []projectstate.ReviewReply
	for _, c := range incoming {
		if c.ReplyTo == "" {
			fresh = append(fresh, c)
			continue
		}
		if strings.TrimSpace(c.Text) == "" {
			continue // an empty utterance is not a reply (mirrors the fresh-comment drop)
		}
		replies = append(replies, projectstate.ReviewReply{
			CommentID:  c.ReplyTo,
			AuthorRole: reviewerUtteranceRole,
			Text:       c.Text,
			At:         at,
		})
	}
	return fresh, replies
}

// checkReplyTargets refuses a batch whose replyTo names no thread on this artifact. Split
// out from splitIncomingComments so the Manager op can run the SAME refusal synchronously
// against the queried wire thread, where a ContractMisuse still reaches the caller (a signal
// payload's error cannot).
func checkReplyTargets(known map[string]bool, incoming []AnchoredComment) error {
	for _, c := range incoming {
		if c.ReplyTo != "" && !known[c.ReplyTo] {
			return newError(fwmanager.ContractMisuse, "replyTo names no thread on this artifact: "+c.ReplyTo)
		}
	}
	return nil
}

// ledgerCommentIDs / viewCommentIDs collect the thread's entry ids from the durable and the
// wire projection respectively — the two shapes checkReplyTargets is asked about.
func ledgerCommentIDs(thread []projectstate.ReviewComment) map[string]bool {
	ids := make(map[string]bool, len(thread))
	for _, c := range thread {
		ids[c.ID] = true
	}
	return ids
}

// designActivityFor maps an artifact kind onto the DESIGN activity the reviewEngine
// keys its rows on: the activity type and the lifecycle phase within it. It is the
// design rail's half of the vocabulary the engine's tables are total over (spec
// 2026-09-20 §5.4, stage 2); the construction rail's half is the ActivityMethodPhase
// wire names.
//
// The requirements activity carries the four business-alignment / volatility steps.
// scrubbedRequirements shares the GLOSSARY phase deliberately: the scrubbing pass runs
// with the glossary inside the-method-requirements-analysis step and shares its gate.
// The architecture activity carries the System draft and the two architect-owned
// documents that hang off it (operational concepts, standard check).
//
// EVERY Phase-2 kind maps to the projectDesign type at the kind's OWN wire name, and
// deliberately NOT to the phase id "sdp". "sdp" is the M0 gate of the projectDesign
// lifecycle, which the engine makes always-human because M0 approves spend; the nine
// Phase-2 artifact DRAFTS are not that gate, and mapping them there would gate nine
// drafts that auto-approve under vibes today. Pinned by
// Test_DesignActivityFor_Phase2KindsAreNotTheSdpGate.
func designActivityFor(kind projectstate.ArtifactKind) (review.ActivityType, string) {
	switch kind {
	case projectstate.KindMission:
		return review.ActivityTypeRequirements, "mission"
	case projectstate.KindGlossary, projectstate.KindScrubbedRequirements:
		return review.ActivityTypeRequirements, "glossary"
	case projectstate.KindVolatilities:
		return review.ActivityTypeRequirements, "volatilities"
	case projectstate.KindCoreUseCases:
		return review.ActivityTypeRequirements, "coreUseCases"
	case projectstate.KindSystem, projectstate.KindOperationalConcepts, projectstate.KindStandardCheck:
		return review.ActivityTypeArchitecture, "architecture"
	case projectstate.KindPlanningAssumptions, projectstate.KindActivityList, projectstate.KindNetwork,
		projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution,
		projectstate.KindCompressedSolution, projectstate.KindDecompressedSolution,
		projectstate.KindRiskModel, projectstate.KindSdpReview:
		return review.ActivityTypeProjectDesign, kind.WireName()
	}
	// An out-of-vocabulary kind cannot reach here through the typed façade; route it to
	// the architect's own step so the engine still answers rather than refusing.
	return review.ActivityTypeArchitecture, "architecture"
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// engineReviewPolicy converts the COMMITTED policy document into the reviewEngine's own
// copy — Preset dereferenced (nil ⇒ "", the legacy/explicit mode), GatedPhasesByType
// re-keyed to the phases' wire names because an Engine may not import projectstate (F3).
//
// BYTE-IDENTICAL COPY in internal/manager/construction/constructactivity.go and
// internal/manager/projectdesign/coauthorphase2artifact.go: three packages, and no
// shared home for a five-line conversion that would not cost an
// internal/arch_test.go allowlist entry. Edit all three together; their parity is
// pinned by Test_EngineReviewPolicy_CarriesTheStoredDocument in each package.
func engineReviewPolicy(p projectstate.ReviewPolicy) review.ReviewPolicy {
	out := review.ReviewPolicy{}
	if p.Preset != nil {
		out.Preset = *p.Preset
	}
	if len(p.GatedPhasesByType) > 0 {
		out.GatedPhasesByType = make(map[string][]string, len(p.GatedPhasesByType))
		for typ, phases := range p.GatedPhasesByType {
			names := make([]string, 0, len(phases))
			for _, ph := range phases {
				names = append(names, ph.String())
			}
			out.GatedPhasesByType[typ] = names
		}
	}
	return out
}

// sameArtifactModel PROMOTED to projectstate.SameArtifactModel
// (code-health-phase-bd task D3) — byte-identical pure comparator, no longer duplicated
// with projectdesign's twin.

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// encodeModel delegates to the promoted projectstate.EncodeModel. Kept as a
// package-level wrapper (rather than rewriting every call site to the qualified name)
// so this move stays a minimal, mechanical diff.
func encodeModel(model projectstate.ArtifactModel) (modelEnvelope, error) {
	return projectstate.EncodeModel(model)
}

// critiqueRoleProductManager / critiqueRoleArchitect are the CritiqueView.Role wire
// labels for the two critique-issuing roles (critiqueCriticFor). They match the SPA's
// ActiveRole wire naming ("productManager" / "architect") so both surfaces name the
// role identically.
const ()

// pdDesignPipelinePhase maps the RA's phase to this Manager's neutral phase, preserving
// the Cancelled terminal distinctly (the design Manager treats any non-Succeeded
// terminal as a ProjectStageDraftFailed gate).
func pdDesignPipelinePhase(p agenticjob.PipelinePhase) pdPipelinePhase {
	switch p {
	case agenticjob.PhasePending:
		return pdPipelinePending
	case agenticjob.PhaseRunning:
		return pdPipelineRunning
	case agenticjob.PhaseSucceeded:
		return pdPipelineSucceeded
	case agenticjob.PhaseFailed:
		return pdPipelineFailed
	case agenticjob.PhaseCancelled:
		return pdPipelineCancelled
	default:
		return pdPipelinePhaseUnknown
	}
}

// pdPipelinePhase mirrors agenticJobAccess.md §3 — the infrastructure-neutral
// lifecycle phase the Manager branches on. The terminal trio drives the observe
// loop's exit + the failure path.
type pdPipelinePhase int

const (
	pdPipelinePhaseUnknown pdPipelinePhase = iota
	pdPipelinePending
	pdPipelineRunning
	pdPipelineSucceeded
	pdPipelineFailed
	pdPipelineCancelled
)

// IsTerminal reports whether the phase is one the job can no longer leave.
func (p pdPipelinePhase) IsTerminal() bool {
	switch p {
	case pdPipelineSucceeded, pdPipelineFailed, pdPipelineCancelled:
		return true
	case pdPipelinePhaseUnknown, pdPipelinePending, pdPipelineRunning:
		return false
	default:
		return false
	}
}

// RegisterManagerWorker registers the ONE delivery worker: the SIX surviving workflow types
// under their existing registered names and the generated Activity set of the merged contract,
// all on task queue "delivery". Stage 4a registered eleven across three rails; stage 4b1 Task
// 13 deleted the seven per-kind ones.
func RegisterManagerWorker(w worker.Worker, m DeliveryManager) {
	impl, ok := m.(*deliveryManager)
	if !ok {
		panic("delivery: RegisterManagerWorker requires a *deliveryManager from NewDeliveryManager")
	}
	RegisterWorker(w, impl.WorkerManifest())
}

// RegisterSchedules registers (idempotently) the THREE platform-wide delivery
// Temporal Schedules at startup via the messageBus utility (constructionManager.md
// §6.1; Task 7c): the pump sweep (30s — targets PumpSweepWorkflow, which fans out to
// every construction-phase project's own PumpNextActivityWorkflow; see this file's
// header + pumpsweep.go), the replan sweep (5m — targets ReplanSweepWorkflow with
// no ProjectID, its existing "sweep all in-flight projects" scope) and, from stage 4b1,
// the round sweep (5m — targets RoundSweepWorkflow with an EMPTY ProjectID, its fan-out
// arm; roundsweep.go). Called once at process start; a re-registration with the same
// id+spec is a harmless no-op (last-writer-wins Update, messagebus.go).
func RegisterSchedules(ctx context.Context, bus messagebus.MessageBus) error {
	adapter := messageBusAdapter{inner: bus}
	if err := adapter.RegisterSchedule(ctx, scheduleSpec{
		ID:           scheduleIDPumpSweep,
		WorkflowType: executionKindPumpSweep,
		TaskQueue:    TaskQueue,
		IntervalSecs: pumpSweepIntervalSecs,
	}); err != nil {
		return err
	}
	if err := adapter.RegisterSchedule(ctx, scheduleSpec{
		ID:           scheduleIDReplanSweep,
		WorkflowType: executionKindReplanSweep,
		TaskQueue:    TaskQueue,
		IntervalSecs: replanSweepIntervalSecs,
	}); err != nil {
		return err
	}
	return adapter.RegisterSchedule(ctx, scheduleSpec{
		ID:           scheduleIDRoundSweep,
		WorkflowType: executionKindRoundSweep,
		TaskQueue:    TaskQueue,
		IntervalSecs: roundSweepIntervalSecs,
	})
}

// ---------------------------------------------------------------------------
// Episode facet read ops (SP1 capture-seam, Task 9 — founder ruling 2026-08-02:
// episode observability is a facet of the existing use cases, not a new
// episodeManager). Both ops are PLAIN METHODS that consult episodeAccess directly
// — no Temporal — the same shape as systemDesignManager.ListProjects/GetProject.
// The whole-project exportEpisodes op is cut from v1 (per-target export is
// client-side, Task 10).
// ---------------------------------------------------------------------------
// constructState is the live technical state backing the sessionState Query.
type constructState struct {
	projectID     ProjectID
	activityID    ActivityID
	stage         ConstructionStage
	pipelinePhase *PipelinePhase
	reviewSet     *ReviewSet
	// reviewSetError is why reviewSet is nil at the current gate ("" when the engine answered).
	reviewSetError string
	variance       *FlaggedVariance

	// completedPhases is the LIVE in-memory skip-guard the phase loop consults so an
	// already-completed phase is never re-dispatched or re-gated. It is SEEDED at
	// workflow start from the start-snapshot activity's PhaseCompletion slice and
	// MARKED unconditionally on EVERY phase completion (Approve / no-gate / inert) —
	// independent of gitOn. This is what stops the outer variance-retry loop (which
	// re-walks phases from index 0) from re-gating an already-approved phase across a
	// non-git execution where no head-state completion record exists to re-read.
	completedPhases map[projectstate.ActivityMethodPhase]bool

	// redraftExhausted reports that the phase gate the workflow is waiting at can take no
	// further SendBack redraft: its human-paced budget (maxPhaseRedrafts) is spent. It does
	// NOT fail the activity or re-enter the variance loop — the gate keeps awaiting the
	// human; the flag surfaces that redrafting is spent. RECOMPUTED on entry to every gate
	// (B1.2): it used to be set once and never reset, so it leaked into every later gate of
	// the same run (plan G5).
	redraftExhausted bool

	// awaitingGate / awaitingSince / awaitingUntil describe the human stage the workflow is
	// in right now (B1.2): which gate (a lifecycle phase's wire name, mergeGateKey or
	// takeoverGateKey), when THIS occurrence of it began, and — for an escalation with a
	// bounded wait — when it gives up. awaitingSince is workflow.Now, so a query served by
	// replay rebuilds the original time, and a redraft re-entering its gate starts a new
	// occurrence. Written only by enterHumanStage and cleared only by leaveHumanStage.
	awaitingGate  string
	awaitingSince time.Time
	awaitingUntil *time.Time

	// attempt is the current supervision attempt, 1-based (set by runAttempt); 0 before
	// the first attempt.
	attempt int

	// reviewContracts is the per-execution set of contract identifiers captured from
	// the start-snapshot project (B5) and fed to reviewEngine.ProposeReviews so the
	// gate's reviewer set is display-populated without re-reading mid-loop.
	reviewContracts []string

	// floorTouched is the Task 7 non-overridable-floor snapshot: whether the
	// activity's committed contract (start-snapshot, B5-style — never re-read
	// mid-loop) touches deploy/spend/schema (projectstate.ContractTouchesReviewFloor).
	// Consulted by runPhaseGate via the reviewEngine's ProposeReviews (this is the
	// floor flag it passes) to force a human gate at MethodPhaseConstruction
	// regardless of preset, including "vibes".
	floorTouched bool

	// mergeCompleted is the LIVE in-memory skip-guard for the local merge step
	// (local-merge-and-policy Commit 1, same discipline as completedPhases):
	// marked once the merge job landed, so a variance retry of a LATER finalize
	// fault does not re-dispatch a merge whose activity branch is already
	// merged and deleted (which would honestly — and wrongly — fail).
	mergeCompleted bool

	// taskAttempts counts, per Figure A-1 task (MethodTask), how many times a pipeline
	// has been dispatched for that task's phase on this activity — seeded at start from
	// the row's attempt ledger (loadReviewSnapshot, v1 of changeLedgerPartialResume), so
	// a new run's AttemptIDs continue the ledger's instead of colliding with them — the join key
	// projectstate.AttemptID needs to attribute an episode to the (activity, task,
	// attempt) it was actually burned on (Task 10, constructactivity.go). It counts
	// across BOTH the outer variance-retry loop and a gated phase's human-paced redraft
	// loop, since both re-enter runPipeline for the SAME phase. Workflow-local (rebuilt
	// deterministically on replay, never persisted); lazily initialized by
	// constructState.nextTaskAttempt so a state that never dispatches a pipeline
	// (ProjectSupervisionWorkflow's) allocates nothing.
	taskAttempts map[projectstate.MethodTask]int

	// noteDelivery is true on an execution that recorded the operator-note-delivery
	// marker (plan B1.4): only then are notes recorded, carried and stamped, and the
	// managed scaffold synced before a GitHub-venue dispatch.
	noteDelivery bool
	// noteSeq numbers the notes this run records (operatorNoteID).
	noteSeq int
	// pendingNotes are the notes the next agent dispatch carries, oldest first: seeded
	// from the row at start (projectstate.PendingOperatorNotes), appended to as notes are
	// recorded, and each dropped once a dispatch carried it whole and it was stamped.
	pendingNotes []projectstate.OperatorNote
	// carriedTo names, per note id, the last attempt a dispatch carried the note into,
	// so a note is never carried twice into the same attempt (M4).
	carriedTo map[string]string

	// executionLedger is true on an execution that recorded the execution-ledger marker
	// (changeExecutionLedger, stage 3): only then does this run WRITE what it does to the
	// per-activity attempt and review-round ledgers. An execution that recorded no marker
	// stays wholly on the retired facet — no activity opened, no attempt recorded, no
	// round opened, no verdict appended — because its history holds no events for those
	// Activities and never will.
	executionLedger bool

	// activityVersion is this run's copy of the per-activity CAS token: the version the
	// activity's own execution row was at the last time this workflow wrote it. It is
	// deliberately NOT headVersion — headVersion is the whole document's token, and two
	// children writing DIFFERENT activities would contend on it while never touching each
	// other's rows. This one is scoped to the row, so it refuses exactly the interleaving
	// that matters and nothing else, which is the guard 4b's parallel pump rests on.
	//
	// Seeded at session start from the row the start snapshot already read
	// (loadReviewSnapshot), 0 for an activity with no row yet — which is
	// projectstate.NoActivityVersionExpectation, the honest posture of a writer about to
	// BIRTH the row. Advanced by rowAdvanced on every applied transition.
	activityVersion int64

	// workAttemptID is the AttemptID of the last AGENT-WORK dispatch runPipeline minted.
	// The gate that follows judges exactly that attempt, so it is what the round cites as
	// its subject and what every verdict on that round names — the join that makes a
	// verdict traceable to the work it judged and to the episode that burned it.
	workAttemptID string

	// stagedRef is the ref of the OUTPUT the last work task staged — the commit the gate
	// that follows actually judges, and the one rung of gateSubjectRef's ladder that moves
	// per revision rather than per activity.
	//
	// (stagedRef and gate went with the retired sequential walk, stage 4b1 Task 13: the
	// generic child stages through producedSubject and carries ONE *gateLedger per review
	// coroutine, because a forked walk holds several gates at once and a single-valued field
	// would make two concurrent gates decide each other's round — R8-7.)

	// walk is the GENERIC child's per-run head-state and git lifecycle (walkRun). Zero and
	// unread on the retired rail, which threads the same facts by parameter.
	walk walkRun

	// ephemeralNotes are the ids of the workflow-local notes that carry a send-back's
	// feedback into the redraft WITHOUT being recorded (stage 3): the round IS the record
	// of the send-back now, so a NoteSendBack beside it would be one fact stored twice.
	// They render into the dispatch block like any other note and are never stamped
	// delivered, because there is no stored note to stamp.
	ephemeralNotes map[string]bool
}

// modelEnvelope/projectEnvelope are ALIASES to the projectstate types (the shared
// wire codec lives in projectstate/envelope.go: EncodeModel/EncodeProject/Decode).
// Aliasing preserves type identity for every existing declaration/field/call site
// in this package; call the promoted methods by their exported names (Decode, not
// decode).
type (
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	modelEnvelope = projectstate.ModelEnvelope
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
)

// Signal and query names (systemDesignManager.md §6.5).
const (
	// signalReviewDecision resumes a suspended CoAuthorArtifactWorkflow at the
	// AwaitingReview gate; backs submitReviewDecision.
	// lSignalRedraft resumes a CoAuthorArtifactWorkflow that ended a draft attempt in
	// the StageRefused terminal-but-live state (a terminal worker fault: the LLM
	// worker is unavailable / out of credits, or produced an unconstructable
	// response). It re-enters the draft loop in the SAME live workflow so the user's
	// "Retry draft" recovers without a fresh run. Backs requestArtifactDraft's retry
	// path (signal-with-start; systemDesignManager.md §2.1).
	lSignalRedraft = "redraft"
	// Stage 4a: ONE copy now serves the systemDesign+construction rails (byte-identical
	// twins, collapsed by the package merge).
	// querySessionState returns a SessionStateView; backs getSessionState.
	querySessionState = "sessionState"
	// signalSetCommentStatus resumes a CoAuthorArtifactWorkflow suspended at the
	// AwaitingReview gate to apply a durable review-ledger status transition
	// (open|answered->resolved / resolved->open) to one comment on the session branch; backs
	// SetReviewCommentStatus (review-ledger feature).
	signalSetCommentStatus = "setCommentStatus"
)

// Signal and query names (constructionManager.md §6.1/§6.2).
const (
	// signalOperatorPauseRequested resumes a suspended construction execution at
	// its awaitSignal; backs PauseProject (NCUC2).
	signalOperatorPauseRequested = "operatorPauseRequested"
	// signalOperatorOverride resumes a per-activity child workflow; backs
	// OverrideActivity.
	signalOperatorOverride = "operatorOverride"
	// signalTaskDecision delivers a decision to ONE TASK's gate inside the generic
	// per-activity child (stage 4b1 Task 8). It is the retired phase-decision signal's
	// successor and not a rename of it: the old signal keyed on a lifecycle PHASE and
	// multiplexed one gate at a time, and the walk runs several gates at once, so the new one
	// keys on the TASK — which is also what lets the router forward it to exactly one coroutine.
	signalTaskDecision = "taskDecision"
	// queryPumpDispatch returns THIS pump run's pumpDispatch decision; backs the
	// synchronous dispatch outcome ExecuteNextActivity returns WITHOUT awaiting the
	// background self-cascade drain (constructionManager.md §2.1).
	queryPumpDispatch = "pumpDispatchDecision"
)

// ExecutionKinds — the registered workflow names (constructionManager.md §6.2).
const (
	// executionKindPump is PumpNextActivityWorkflow — the project's ONE pump,
	// {projectId}:nextActivity, started or joined by ExecuteNextActivity and by the
	// 30s pump sweep (not one execution per tick).
	executionKindPump = "constructionPumpNextActivity"
	// executionKindReplanSweep is the per-tick ReplanSweepWorkflow (the 5m sweep).
	executionKindReplanSweep = "constructionReplanSweep"
	// executionKindProjectSupervision is the long-lived project-level supervision
	// workflow that hosts the operator-pause branch + project-level session Query.
	executionKindProjectSupervision = "constructionProjectSupervision"
	// executionKindPumpSweep is the Schedule-triggered, platform-wide fan-out
	// (the 30s pump sweep; pumpsweep.go) — the actual Schedule target, since a
	// Schedule cannot itself vary executionKindPump's ProjectID per firing.
	executionKindPumpSweep = "constructionPumpSweep"
	// executionKindRoundSweep is the stranded-review-round sweep (the 5m round sweep;
	// roundsweep.go, stage 4b1 Task 6). ONE type for both of its arms: the Schedule
	// fires it with an empty ProjectID (the fan-out) and it starts children of its own
	// type per project (the sweep proper). The name carries the delivery* spelling
	// because it is BORN here — unlike the construction* four above, which keep theirs so
	// a rename cannot strand an in-flight execution.
	executionKindRoundSweep = "deliveryRoundSweep"
	// executionKindDeliveryActivity is the GENERIC per-activity child (stage 4b1 Task 8):
	// ONE workflow type that walks any method-assets lifecycle's task DAG. It carries the
	// delivery* spelling for the same reason the round sweep does — it is BORN here, so no
	// in-flight execution can be stranded by the name it was given.
	executionKindDeliveryActivity = "deliveryActivity"
)

// The gate's ledger vocabulary. A construction gate's human row has no named person
// behind it — the decision signal carries feedback, not an identity — so the ROLE is
// what the round records and "operator" is who the platform can honestly say answered it.
const (
	gateRoleHuman     = "human"
	gateActorOperator = "operator"
	// (gateRoleReviewEngine went with the retired rail's refused-roster abstention, stage 4b1
	// Task 13: the child's runAgentReviewers records a critic's abstention under the reviewer's
	// OWN workerClass, and a roster the engine cannot staff is logged and the gate held rather
	// than given a synthetic role.)
	// A construction gate is closed by a person answering it. (Its twin, decidedByPolicy —
	// "the committed review policy said no person was needed here" — is RETIRED in the final
	// fix wave: it had zero callers repo-wide, because a policy-closed gate records the
	// policy's own reason string rather than a fixed decidedBy word.)
	decidedByOperator = "operator"
)

// resolvedPhaseCompletions is this package's name for projectstate.ResolvePhaseCompletions,
// the profile-wins, ledger-per-phase resolution that classifiedRowView's phase set comes
// from. The rule moved down into projectstate so the construction pump can share it; this
// name stays because the view-model's tests pin the rule against it directly, with an
// explicit profile, and must keep passing unmodified across the move.
func resolvedPhaseCompletions(
	profile projectstate.Profile,
	attempts []projectstate.TaskAttempt,
) []projectstate.PhaseCompletion {
	return projectstate.ResolvePhaseCompletions(profile, attempts)
}
