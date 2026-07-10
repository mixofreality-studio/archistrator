package constructionpipeline

// variant.go holds the DRY-RUN variant stub for constructionPipelineAccess — the
// in-memory profile that backs the UC3 construction Worker when
// ARCHISTRATOR_CONSTRUCTION_DRYRUN=true, folded out of cmd/server (construction_dryrun.go)
// into the owning package. Every submit instantly "succeeds": Submit returns a
// deterministic handle keyed on the activity, Observe always reports Succeeded, Cancel is
// a no-op. No GitHub Actions run fires.
//
// What stays REAL is the construction Manager's Temporal orchestration — the
// self-cascading pump, the per-activity lifecycle, and the per-activity construction
// head-state writes. So "Begin construction" walks the committed network for real WITHOUT
// firing any GitHub Actions run. Local dogfood / demo profile only. Construction
// dispatches real work via the GH-Actions pipeline (agentic-everywhere); there is no
// server-side LLM worker seam.
//
// The REAL GitHub-Actions variant is the generated DI constructor
// NewGitHubActionsConstructionPipelineAccess (contract.gen.go); the composition root
// builds the shared *fwgithub.AppClient satellite and passes it in.

import (
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// NewDryRunConstructionPipelineAccess returns the in-memory dry-run pipeline stub.
func NewDryRunConstructionPipelineAccess() ConstructionPipelineAccess {
	return dryRunPipeline{}
}

type dryRunPipeline struct{}

var _ ConstructionPipelineAccess = dryRunPipeline{}

func (dryRunPipeline) SubmitConstructionPipeline(_ fwra.Context, spec PipelineSpec) (PipelineHandle, error) {
	return PipelineHandle("dryrun:" + string(spec.ActivityID)), nil
}

func (dryRunPipeline) ObserveConstructionPipeline(_ fwra.Context, handle PipelineHandle) (PipelineObservation, error) {
	return PipelineObservation{Handle: handle, Phase: PhaseSucceeded}, nil
}

func (dryRunPipeline) CancelConstructionPipeline(_ fwra.Context, _ PipelineHandle) error {
	return nil
}
