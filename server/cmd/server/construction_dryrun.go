package main

// construction_dryrun.go holds the IN-MEMORY stubs that back the UC3 construction
// Worker when ARCHISTRATOR_CONSTRUCTION_DRYRUN=true (config.go). They replace the
// three EXTERNAL-effect dependencies of the per-activity construction spine — the
// GitHub-Actions pipeline (constructionPipelineAccess), the content-addressable
// output store (artifactAccess), and the LLM worker (workerAccess) — with instant,
// deterministic, side-effect-free successes.
//
// Per the founder DI model these now satisfy each dependency's PUBLISHED interface
// directly (the construction Manager's consumer mirrors were folded into the manager
// package), so they are handed straight to construction.NewConstructionManager.
//
// What stays REAL: the construction Manager's Temporal orchestration — the
// self-cascading pump, the per-activity lifecycle, and the per-activity construction
// head-state writes that drive the eligibility cascade. So "Begin construction" walks
// the committed network for real WITHOUT firing any GitHub Actions run, committing any
// build artifact to a remote, or calling any LLM. Local dogfood / demo profile only.
//
// These live in cmd/server (outside internal/) so they may freely import the concrete
// RA packages; none imports Temporal (the Manager owns it).

import (
	"encoding/json"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	workeraccess "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/worker"
)

// ---------------------------------------------------------------------------
// dryRunPipeline — constructionpipeline.ConstructionPipelineAccess stub. Every submit
// instantly "succeeds": Submit returns a deterministic handle keyed on the activity,
// Observe always reports Succeeded, Cancel is a no-op. No GitHub Actions run fires.
// ---------------------------------------------------------------------------

type dryRunPipeline struct{}

var _ constructionpipeline.ConstructionPipelineAccess = dryRunPipeline{}

func (dryRunPipeline) SubmitConstructionPipeline(_ fwra.Context, spec constructionpipeline.PipelineSpec) (constructionpipeline.PipelineHandle, error) {
	return constructionpipeline.PipelineHandle("dryrun:" + string(spec.ActivityID)), nil
}

func (dryRunPipeline) ObserveConstructionPipeline(_ fwra.Context, handle constructionpipeline.PipelineHandle) (constructionpipeline.PipelineObservation, error) {
	return constructionpipeline.PipelineObservation{Handle: handle, Phase: constructionpipeline.PhaseSucceeded}, nil
}

func (dryRunPipeline) CancelConstructionPipeline(_ fwra.Context, _ constructionpipeline.PipelineHandle) error {
	return nil
}

// ---------------------------------------------------------------------------
// dryRunArtifacts — artifact.ArtifactAccess stub. Store returns a deterministic fake
// content address; Retrieve returns a minimal valid output. Nothing is committed.
// ---------------------------------------------------------------------------

type dryRunArtifacts struct{}

var _ artifact.ArtifactAccess = dryRunArtifacts{}

func (dryRunArtifacts) StoreConstructionOutput(rc fwra.Context, _ artifact.ConstructionOutput) (string, error) {
	return "dryrun-addr:" + string(rc.IdempotencyKey), nil
}

func (dryRunArtifacts) RetrieveConstructionOutput(_ fwra.Context, _ string) (artifact.ConstructionOutput, error) {
	return artifact.ConstructionOutput{Bytes: []byte("dry-run construction output"), MIMEType: "text/plain"}, nil
}

func (dryRunArtifacts) RetrieveOutputTree(_ fwra.Context, contentAddress string) (artifact.OutputTree, error) {
	return artifact.OutputTree{Root: contentAddress, Entries: map[string]string{}}, nil
}

// ---------------------------------------------------------------------------
// dryRunWorker — workeraccess.WorkerAccess stub. Generate returns bytes that decode
// into a valid artifact.ConstructionOutput; Cancel is a no-op. No LLM is called.
// ---------------------------------------------------------------------------

type dryRunWorker struct{}

var _ workeraccess.WorkerAccess = dryRunWorker{}

func (dryRunWorker) Generate(_ fwra.Context, spec workeraccess.GenerateSpec) (json.RawMessage, error) {
	out := artifact.ConstructionOutput{
		Bytes:    []byte("dry-run output for worker class " + string(spec.WorkerClass)),
		MIMEType: "text/plain",
	}
	return json.Marshal(out)
}

func (dryRunWorker) GenerateToolTurn(_ fwra.Context, _ workeraccess.ToolTurnSpec) (workeraccess.AssistantTurn, error) {
	return workeraccess.AssistantTurn{StopReason: "end_turn"}, nil
}

func (dryRunWorker) Cancel(_ fwra.Context) error {
	return nil
}
