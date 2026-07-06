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
