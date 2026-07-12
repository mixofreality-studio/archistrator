package construction

import (
	"context"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// activities_custom.go holds the ONE remaining CUSTOM Manager-owned Temporal Activity
// wrapper the generated temporalgen layer cannot emit: the projectEnvelope-codec
// whole-aggregate read (ReadProjectActivity). B8 (custom activities → generated, clean
// cut) migrated every OTHER activity that used to live here — ReadProjectVersionActivity
// and the six constructionTransitionAccess head-state transition writes
// (RecordChangeReviewed / RecordActivityExited / RecordActivityFailed /
// RecordOperatorPaused / RecordPhaseStarted / RecordPhaseCompleted) — onto the GENERATED
// invoker surface (invokers.gen.go: genInvokers.ProjectStateReadProjectVersion /
// genInvokers.ConstructionTransition*), called directly from workflow.go / signals.go.
// The six git head-state RecordActivity* writes that used to live in gitactivities.go
// migrated the same way onto genInvokers.GitStatus*; gitactivities.go now holds only its
// non-activity value carriers (railCredEnvelope, pullRequestStatusView, mapCheckState).
//
// ReadProjectActivity is NOT migrated. The generated designSessionAccess.readProjectOnBranch
// invoker (construction would call it with branch="" to read main) returns
// projectstate.ProjectEnvelope — a STRUCTURALLY NARROWER wire type than construction's own
// projectEnvelope (codec.go): ProjectEnvelope's Slots map carries Network/ActivityList
// (via the shared slotTable) but has NO field for ActivityConstruction / ServiceContracts
// / ReviewPolicy, which are top-level projectstate.Project fields outside slotTable() (see
// envelope.go's own doc comment, which flags construction's codec as "structurally
// different ... stays local"). The pump's eligibility selection (eligibility.go:
// nextEligibleActivity) reads ActivityConstruction/ServiceContracts on EVERY tick —
// decoding through the generated ProjectEnvelope would silently and permanently lose
// them (every activity would look NotStarted forever), a correctness regression rather
// than the sanctioned "activity name changed" clean-cut. B8 therefore keeps
// ReadProjectActivity custom; see the task-B8 report for the full analysis and the
// BLOCKED disclosure.
//
// It is a METHOD ON THE workflows STRUCT (construction's Activity receiver has always
// been the workflows struct). It runs inside a Temporal Activity because the read is I/O
// / non-deterministic and would break replay determinism if invoked on the workflow
// goroutine. It is registered via the manifest's CustomActivities under its existing
// stable name (workermanifest.go) and invoked by method value from the workflow body
// (workflow.go: readProject).

// ---- ReadProjectActivity (wraps projectStateAccess.readProject) -------------
// Pure whole-aggregate read; no idempotency key (constructionManager.md §6.4).
func (wf *workflows) ReadProjectActivity(ctx context.Context, projectID projectstate.ProjectID) (projectEnvelope, error) {
	proj, err := wf.ProjectState.ReadProject(fwra.Context{Context: ctx}, projectID)
	if err != nil {
		return projectEnvelope{}, fwmanager.MapError(err)
	}
	return encodeProject(proj), nil
}
