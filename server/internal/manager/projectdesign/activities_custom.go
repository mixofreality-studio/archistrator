package projectdesign

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// activities_custom.go holds the ONE remaining CUSTOM Manager-owned Temporal Activity
// (B9 rewire): StageArtifactForReviewActivity. Every other custom Activity this file
// used to hold — ReadProjectActivity, ReadProjectOnBranchActivity, CommitArtifactActivity,
// RejectArtifactActivity, WithdrawArtifactActivity — MIGRATED onto the generated
// designSessionAccess invoker surface (invokers.gen.go, reached via wf.Acts). The
// capability-fallback chains those bodies used to run inline (BranchAware / Ledger /
// Provenance type-assertions over ProjectStateAccess) now live INSIDE the RA
// (projectstate/designsession.go, B4) — the migrated call sites just call the invoker.
//
// StageArtifactForReviewActivity is KEPT CUSTOM — BLOCKED, not migrated (B9 finding,
// verified, not assumed): the generated designSessionAccess.stageArtifactForReviewOnBranch
// invoker (and its main-path sibling projectStateAccess.stageArtifactForReview) declare
// their `model` parameter as the raw projectstate.ArtifactModel — a SEALED, closed
// interface (Kind() + unexported isArtifactModel()). Temporal's default JSON
// DataConverter cannot decode an activity argument declared as a non-empty interface: a
// nil interface target has no concrete type to allocate into, so
// encoding/json.Unmarshal fails with "json: cannot unmarshal object into Go value of
// type projectstate.ArtifactModel". This was VERIFIED empirically against this exact
// generated activity (DesignSessionStageArtifactForReviewOnBranch) over the REAL
// testsuite.WorkflowTestSuite activity-dispatch path (the same path production uses) —
// not assumed from the read-side precedent. It is the SAME defect class B8 found and
// named for construction's ReadProject / the sealed ArtifactModel slots inside Project
// (see projectstate/construction_transition_port.go's own warning), now recurring on
// the WRITE side for the ONE contract op whose parameter (not just its return) carries
// the sealed sum. Bridging it would require temporalgen to emit ModelEnvelope (the
// concrete, JSON-friendly {kind, model} wire shape — projectstate/envelope.go) as the
// op's parameter type instead of ArtifactModel — a schema-first codegen change to the
// designSessionAccess/projectStateAccess contracts, out of scope for this workflow-
// rewire task (same "out of scope" ruling B8 gave its own blocked item).
//
// The RA dependency (ProjectState) lives as a field on workflows (see workflow.go) and
// is reached "on the struct", but the call runs inside a Temporal Activity because the
// operation is I/O / non-deterministic and would break replay determinism if invoked on
// the workflow goroutine directly.
//
// Each WRITE Activity body derives the idempotency key "${workflowId}:${runId}:${activityId}"
// from the Temporal activity context (so the RA layer never reads Temporal context) and
// runs the port result through the generic error mapper mapErr.

// activityIdempotencyKey derives "${workflowId}:${runId}:${activityId}" from the
// running Activity's info. The ActivityID is unique per activity invocation WITHIN
// one workflow run, giving the stable, distinct key each logical write needs on a
// transient auto-retry (same run, same ActivityID ⇒ same key ⇒ the RA/ledger
// collapses the retry).
//
// The RunID is REQUIRED in the key because ActivityIDs restart from 1 on every NEW
// Temporal run of the SAME workflow ID (an amendment session, or a post-withdraw
// restart, executes under the same deterministic workflow ID). Without the RunID the
// dispatch token "${workflowId}:${activityId}" of a fresh session COLLIDES with a
// predecessor session's token: the constructionpipeline RA's run-name dedup
// (aiarch-cp-<sha256(key)>) would then converge on the predecessor's already-completed
// GitHub run, so no new run is dispatched, observe sees the stale success, and
// OpenPullRequest 422s on the zero-new-commit branch. The RunID is a property of the
// workflow EXECUTION (fixed for the run's lifetime, identical across activity retries,
// unchanged by replay) — it is replay-stable, unlike wall-clock. It scopes EVERY
// per-session write key to its session, so a new run's Commit/Stage/Reject never dedups
// against a predecessor run's ledger entry either. Same key content still hashes to a
// valid 32-hex aiarch-cp-<token> run name, so the RA run-name + concurrency-group
// contract is unchanged.
//
// This is the SAME 3-part run-scoped format the generated layer's own
// genActivityIdempotencyKey derives (activities.gen.go) — no format change for the
// surviving custom Activity.
func activityIdempotencyKey(ctx context.Context) fwra.IdempotencyKey {
	info := activity.GetInfo(ctx)
	return composeIdempotencyKey(info.WorkflowExecution.ID, info.WorkflowExecution.RunID, info.ActivityID)
}

// composeIdempotencyKey is the pure derivation of the run-scoped key. Kept separate so
// the run-scoping (distinct RunID ⇒ distinct key, even for the same workflowID +
// ActivityID) is unit-testable without a live Activity context. The RA hashes this whole
// string into the aiarch-cp-<token> run name, so the exact separator/order is not a wire
// contract — only that two sessions of the same workflow ID never collide.
func composeIdempotencyKey(workflowID, runID, activityID string) fwra.IdempotencyKey {
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%s:%s", workflowID, runID, activityID))
}

// stageArtifactForReviewArgs carries the TYPED model into its slot (the model is
// carried as an envelope across the Temporal boundary — codec.go) — the mechanical
// bridge the generated invoker's raw-ArtifactModel parameter cannot provide (see the
// file doc above).
type stageArtifactForReviewArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	Model           modelEnvelope
	// Branch is the OPTIONAL session-branch override (I-DESIGN-DISPATCH §2a). Empty ⇒
	// the AwaitingReview thin-write lands on main exactly as today (every existing
	// caller/test leaves it empty and is unperturbed). Non-empty ⇒ the staged-slot
	// status flip rides over the session branch the draft lives on.
	Branch string
}

// StageArtifactForReviewActivity wraps the branch-aware/main-path stage verb. The
// BranchAware capability fallback (branch supplied + supported → stage on branch; else
// main) is the SAME chain designSessionAccess.StageArtifactForReviewOnBranch runs
// internally (projectstate/designsession.go) — this body is functionally identical to
// what the generated Activity would do, MINUS the interface-across-the-wire landmine
// (see file doc): the model rides as a concrete modelEnvelope, decoded here, then
// handed to the SAME wf.ProjectState capability chain the RA wraps.
func (wf *workflows) StageArtifactForReviewActivity(ctx context.Context, a stageArtifactForReviewArgs) (projectstate.Version, error) {
	model, err := a.Model.Decode()
	if err != nil {
		return 0, fwmanager.MapError(err)
	}
	if ba, ok := wf.ProjectState.(projectstate.BranchAwareProjectStateAccess); ok && a.Branch != "" {
		return mapErr(ba.StageArtifactForReviewOnBranch(ctx, a.ProjectID, a.ExpectedVersion, a.Branch, model, activityIdempotencyKey(ctx)))
	}
	return mapErr(wf.ProjectState.StageArtifactForReview(fwra.Context{Context: ctx, IdempotencyKey: activityIdempotencyKey(ctx)}, a.ProjectID, a.ExpectedVersion, model))
}
