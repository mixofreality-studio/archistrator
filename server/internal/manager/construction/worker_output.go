package construction

import (
	"context"
	"encoding/json"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/artifact"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// generateConstructionOutput asks the worker for a JSON ConstructionOutput and
// unmarshals it (the Manager's SEQUENCE owns the prompt; the worker mechanically
// deserializes — workerAccess.md §0b). A response that cannot be unmarshalled is a
// *workerUnmarshalError (carrying the raw bytes), distinct from a transport error;
// a nil (cancelled) response returns the zero value with nil error.
func generateConstructionOutput(ctx context.Context, w WorkerAccess, spec workerGenerateSpec, key fwra.IdempotencyKey) (artifact.ConstructionOutput, error) {
	var zero artifact.ConstructionOutput
	raw, err := w.Generate(ctx, spec, key)
	if err != nil {
		return zero, err
	}
	if raw == nil {
		// Cancel-then-Generate path: replays as nil bytes with nil error.
		return zero, nil
	}
	var out artifact.ConstructionOutput
	if uErr := json.Unmarshal(raw, &out); uErr != nil {
		return zero, &workerUnmarshalError{Raw: raw, Err: uErr}
	}
	return out, nil
}
