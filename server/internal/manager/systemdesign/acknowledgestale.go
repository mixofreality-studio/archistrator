package systemdesign

import (
	"fmt"
	"hash/fnv"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// acknowledgestale.go implements the F45 per-slot staleness-acknowledge op: a reviewer marks
// a stale COMMITTED artifact "reviewed — unaffected", clearing its StaleBasis flag WITHOUT a
// redraft (which, for an unaffected artifact, would be a byte-identical no-op that dies at the
// no-change gate). The clear + a durable staleAck audit entry commit atomically on main.

const acknowledgeStaleMaxAttempts = 5

// AcknowledgeStaleBasis clears the committed slot's StaleBasis and records the reviewer's
// note as a staleAck audit entry. Synchronous OCC write (mirrors SetResearchInput).
func (m *systemDesignManager) AcknowledgeStaleBasis(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, note string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase1(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-1 kind")
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
		return mapSetResearchInputError(err) // shares the ContractMisuse/NotFound/else mapping
	}
	return fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "AcknowledgeStaleBasis: exhausted conflict retries")
}

func acknowledgeStaleIdempotencyKey(projectID ProjectID, kind ArtifactKind, note string) fwra.IdempotencyKey {
	h := fnv.New64a()
	_, _ = h.Write([]byte(note))
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%d:ackStale:%x", projectID, int(kind), h.Sum64()))
}
