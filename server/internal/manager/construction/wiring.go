package construction

// This file is the PACKAGE-PROVIDED composition seam (C-MCN reconcile). deps.go
// declares two consumer interfaces whose method signatures reference UNEXPORTED
// mirror types — WorkerAccess.Generate takes workerGenerateSpec and
// DurableExecutionAccess.RegisterSchedule takes scheduleSpec. Those types are
// unexported by the frozen contract, so NO package outside `construction` can
// implement either interface: the composition root cannot build a `Deps{Workers:…}`
// nor call `RegisterSchedules`. deps.go's own comment promises "the composition
// root adapts the concrete worker.WorkerAccess to this consumer interface", but the
// unexported seam type makes that physically impossible across the package
// boundary (flagged as a frozen-deps.go gap in implementation/log/C-MCN-reconcile.md).
//
// Rather than amend the frozen deps.go / contract files, this NEW in-package file
// provides the missing exported wiring: it can name the unexported seam types and
// translate them to/from the real worker.WorkerAccess (a legal Manager→ResourceAccess
// downward edge, exactly like the existing artifact/projectstate imports). The five
// frozen public ops and every consumer interface are untouched.

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	workeraccess "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/worker"
)

// WireDeps assembles a construction.Deps from the real collaborators at the
// composition root. The three Engines and the pipeline/artifact/projectstate RAs
// are supplied as their EXPORTED consumer interfaces (the composition root adapts
// the concrete engine/RA packages to them — those interfaces use only exported
// types, so cmd/server can build them). The generic typed worker is supplied as
// the concrete workerAccess port and adapted HERE to the unexported WorkerAccess
// seam (the one adaptation that cannot live outside this package).
//
// nextEligible is the Manager's own pure next-activity selection over committed
// head-state; handOffPolicy / interventionPolicy are the project's committed
// policy snapshots the Manager feeds the Engines by value.
func WireDeps(
	handOff HandOffEngine,
	interventionEng InterventionEngine,
	reviewEng ReviewEngine,
	projectState ProjectStateAccess,
	pipeline ConstructionPipelineAccess,
	artifacts ArtifactAccess,
	workers workeraccess.WorkerAccess,
	nextEligible func(proj projectstate.Project) (ConstructionActivity, bool),
	handOffPolicy HandOffPolicy,
	interventionPolicy InterventionPolicy,
) Deps {
	return Deps{
		HandOff:              handOff,
		Intervention:         interventionEng,
		Review:               reviewEng,
		ProjectState:         projectState,
		Pipeline:             pipeline,
		Artifacts:            artifacts,
		Workers:              workerSeamAdapter{inner: workers},
		NextEligibleActivity: nextEligible,
		HandOffPolicy:        handOffPolicy,
		InterventionPolicy:   interventionPolicy,
	}
}

// workerSeamAdapter bridges the concrete generic typed worker (workeraccess.WorkerAccess)
// to the Manager's unexported WorkerAccess consumer seam. It lives in-package
// because the seam's Generate signature names the unexported workerGenerateSpec.
type workerSeamAdapter struct{ inner workeraccess.WorkerAccess }

var _ WorkerAccess = workerSeamAdapter{}

func (a workerSeamAdapter) Generate(ctx context.Context, spec workerGenerateSpec, idempotencyKey fwra.IdempotencyKey) ([]byte, error) {
	raw, err := a.inner.Generate(ctx, workeraccess.GenerateSpec{
		WorkerClass: workeraccess.WorkerClass(spec.WorkerClass),
		Prompt:      spec.Prompt,
	}, idempotencyKey)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (a workerSeamAdapter) Cancel(ctx context.Context, idempotencyKey fwra.IdempotencyKey) error {
	return a.inner.Cancel(ctx, idempotencyKey)
}

// WithGitForward AUGMENTS an assembled Deps with the optional git-forward slice
// (C-MCN-GIT): the PR rail, the per-activity git head-state mirror, and the
// per-project repo resolver. Kept as a separate augmentation (not new WireDeps
// params) so the existing composition-root WireDeps call is source-stable; when the
// GitStore + sourceControlAccess are wired at the composition root (a follow-up — the
// live store today is the Postgres *Store, which has neither the cred-threaded git
// Record verbs nor a credential surface), the root calls this to light up the slice.
// nil deps leave the slice dormant.
//
// rail/gitStatus are supplied as their EXPORTED consumer interfaces (the concrete
// *sourcecontrol.Access / *projectstate.GitStore satisfy them structurally). repo is
// the Manager's per-project RepoRef resolver (deterministic per-project repo name).
func (d Deps) WithGitForward(
	rail SourceControlRail,
	gitStatus GitActivityStatusAccess,
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool),
) Deps {
	d.Rail = rail
	d.GitStatus = gitStatus
	d.Repo = repo
	return d
}
