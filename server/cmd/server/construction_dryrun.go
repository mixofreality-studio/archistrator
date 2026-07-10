package main

// construction_dryrun.go holds the IN-MEMORY stubs that back the UC3 construction
// Worker when ARCHISTRATOR_CONSTRUCTION_DRYRUN=true (config.go). They replace the
// two EXTERNAL-effect dependencies of the per-activity construction spine — the
// GitHub-Actions pipeline (constructionPipelineAccess) and the content-addressable
// output store (artifactAccess) — with instant, deterministic, side-effect-free
// successes. Construction dispatches real work via the GH-Actions pipeline
// (agentic-everywhere); there is no server-side LLM worker seam.
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
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
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
