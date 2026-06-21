package main

// construction_dryrun.go holds the IN-MEMORY stubs that back the UC3 construction
// Worker when ARCHISTRATOR_CONSTRUCTION_DRYRUN=true (config.go). They replace the
// three EXTERNAL-effect dependencies of the per-activity construction spine — the
// GitHub-Actions pipeline (constructionPipelineAccess), the content-addressable
// output store (artifactAccess), and the LLM worker (workerAccess) — with instant,
// deterministic, side-effect-free successes.
//
// What stays REAL: the construction Manager's Temporal orchestration — the
// self-cascading pump (PumpNextActivityWorkflow), the per-activity lifecycle
// (ConstructActivityWorkflow: dispatch → pipeline → stage → review → record), and the
// per-activity construction head-state writes (RecordActivityStarted/Completed) that
// drive the eligibility cascade. So "Begin construction" walks the committed network
// for real and the tracker animates eligible→in-construction→integrated — WITHOUT
// firing any GitHub Actions run, committing any build artifact to a remote, or calling
// any LLM. This is the local dogfood / demo profile, NEVER the production default.
//
// These live in cmd/server (outside internal/) so they may freely import the concrete
// construction / artifact / worker packages; none imports Temporal (the Manager owns it).

import (
	"context"
	"encoding/json"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	workeraccess "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/worker"
)

// ---------------------------------------------------------------------------
// dryRunPipeline — construction.ConstructionPipelineAccess stub. Every submit
// instantly "succeeds": Submit returns a deterministic handle keyed on the activity,
// Observe always reports Succeeded, Cancel is a no-op. No GitHub Actions run fires.
// ---------------------------------------------------------------------------

type dryRunPipeline struct{}

var _ construction.ConstructionPipelineAccess = dryRunPipeline{}

func (dryRunPipeline) SubmitConstructionPipeline(_ context.Context, spec construction.PipelineSpec, _ fwra.IdempotencyKey) (construction.PipelineHandle, error) {
	return construction.PipelineHandle{Name: "dryrun:" + spec.ActivityID}, nil
}

func (dryRunPipeline) ObserveConstructionPipeline(_ context.Context, _ construction.PipelineHandle) (construction.PipelineObservation, error) {
	return construction.PipelineObservation{Phase: construction.PipelineSucceeded}, nil
}

func (dryRunPipeline) CancelConstructionPipeline(_ context.Context, _ construction.PipelineHandle) error {
	return nil
}

// ---------------------------------------------------------------------------
// dryRunArtifacts — construction.ArtifactAccess stub. Store returns a deterministic
// fake content address; Retrieve returns a minimal valid output. Nothing is committed.
// ---------------------------------------------------------------------------

type dryRunArtifacts struct{}

var _ construction.ArtifactAccess = dryRunArtifacts{}

func (dryRunArtifacts) StoreConstructionOutput(_ context.Context, _ artifact.ConstructionOutput, key fwra.IdempotencyKey) (string, error) {
	return "dryrun-addr:" + string(key), nil
}

func (dryRunArtifacts) RetrieveConstructionOutput(_ context.Context, _ string) (artifact.ConstructionOutput, error) {
	return artifact.ConstructionOutput{Bytes: []byte("dry-run construction output"), MIMEType: "text/plain"}, nil
}

// ---------------------------------------------------------------------------
// dryRunWorker — workeraccess.WorkerAccess stub. Generate returns bytes that decode
// into a valid artifact.ConstructionOutput (so the Manager's GenerateTypedData +
// the stage/review steps accept it); GenerateToolTurn ends the turn immediately;
// Cancel is a no-op. No LLM is called.
// ---------------------------------------------------------------------------

type dryRunWorker struct{}

var _ workeraccess.WorkerAccess = dryRunWorker{}

func (dryRunWorker) Generate(_ context.Context, spec workeraccess.GenerateSpec, _ fwra.IdempotencyKey) (json.RawMessage, error) {
	// Marshal a real ConstructionOutput so it round-trips through the Manager's
	// generateConstructionOutput unmarshal (a refused/unmarshal terminal would route
	// the activity into intervention, stalling the cascade).
	out := artifact.ConstructionOutput{
		Bytes:    []byte("dry-run output for worker class " + string(spec.WorkerClass)),
		MIMEType: "text/plain",
	}
	return json.Marshal(out)
}

func (dryRunWorker) GenerateToolTurn(_ context.Context, _ workeraccess.ToolTurnSpec, _ fwra.IdempotencyKey) (workeraccess.AssistantTurn, error) {
	return workeraccess.AssistantTurn{StopReason: "end_turn"}, nil
}

func (dryRunWorker) Cancel(_ context.Context, _ fwra.IdempotencyKey) error {
	return nil
}
