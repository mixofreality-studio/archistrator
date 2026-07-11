package projectdesign

import (
	"errors"
	"fmt"
	"hash/fnv"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// acknowledgestale.go implements the F45 per-slot staleness-acknowledge op for Project Design
// (twin of the systemdesign impl): a reviewer marks a stale COMMITTED Phase-2 artifact
// "reviewed — unaffected", clearing its StaleBasis WITHOUT a redraft, with a durable staleAck
// audit entry — both committed atomically on main.

const acknowledgeStaleMaxAttempts = 5

func (m *projectDesignManager) AcknowledgeStaleBasis(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, note string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
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
	if err := m.refuseAckDuringLiveSession(rc, projectID, kind); err != nil {
		return err
	}
	sa, ok := m.projectState.(projectstate.StaleAckProjectStateAccess)
	if !ok {
		return newError(fwmanager.FailedPrecondition, "stale-basis acknowledge not supported by this substrate")
	}
	key := acknowledgeStaleIdempotencyKey(projectID, kind, note)
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)

	var lastErr error
	for attempt := 0; attempt < acknowledgeStaleMaxAttempts; attempt++ {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			return mapReadProjectError(err)
		}
		_, err = sa.AcknowledgeStaleBasis(ctx, psID, proj.Version, psKind, note, key)
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
// (a dead run synthesizes StageDraftFailed; a COMPLETED run is rebuilt from the durable
// slot) — so ack gating always agrees with what the reviewer sees on screen. A NotFound
// (no session ever ran for this slot) passes.
func (m *projectDesignManager) refuseAckDuringLiveSession(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) error {
	view, err := m.GetSessionState(rc, projectID, kind)
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
// OWNS the slot (its branch/PR is open or recoverable): drafting / assembling /
// awaiting review / redrafting, plus the StageDraftFailed recovery gate (the session is
// suspended there with its branch and PR intact — a Retry resumes it). The terminal
// stages (committed / withdrawn / refused) and the unknown zero value are NOT live.
func sessionStageIsLive(s SessionStage) bool {
	switch s {
	case StageDrafting, StageAssemblingSDP, StageAwaitingReview, StageRedrafting, StageDraftFailed:
		return true
	case SessionStageUnknown, StageCommitted, StageWithdrawn, StageRefused:
		return false
	default:
		return false
	}
}

func acknowledgeStaleIdempotencyKey(projectID ProjectID, kind ArtifactKind, note string) fwra.IdempotencyKey {
	h := fnv.New64a()
	_, _ = h.Write([]byte(note))
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%d:ackStale:%x", projectID, int(kind), h.Sum64()))
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
