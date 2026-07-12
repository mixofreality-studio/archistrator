package operations

import (
	"context"

	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

// ===========================================================================
// ViewWorkflow — op 2.7 (short-lived read-only operator view). NO mutation.
// Composes the EXISTING read Activities into one OperatedSystemView
// (operationsRead-ruling.md §A). No new Activities, no new RA verbs.
// ===========================================================================

// viewInput is the start payload for ViewWorkflow.
type viewInput struct {
	OperatedAppID operatedAppID
}

// ViewWorkflow drives the U-SPA-4 operator read view (operationsRead-ruling.md §A):
//  1. ReadOperatedSystemActivity  → head-state phase (RuntimePhase) + inFlight.
//  2. GetApplicationHealthActivity → observed health snapshot phase.
//  3. GetSloStatusActivity         → SLO posture (rolled into the health snapshot + one row).
//  4. ReadUsageRangeActivity + operationEstimationEngine.ProjectForOperatedApp (nil
//     what-if) → CurrentRunRate (run-rate only).
//
// The autoscaler mode is sourced directly from the committed policy snapshot the
// Manager carries in its OWN façade currency (wf.AutoscalerPolicy.Mode,
// autoscalerPolicy above — zero value AutoscalerModeUnknown for an unconfigured
// policy; no bridge needed here). The autoscaler DECISION history and the
// per-phase RecentEvents are NOT exposed by an existing frozen RA read verb (head-state
// exposes Status/Version/InFlight only); per the ruling's Construction note they are
// surfaced empty here and a one-line follow-up is flagged to the architect — NO new RA
// verb is invented. ALL reads, NO write Activity, NO version bump.
func (wf *workflows) ViewWorkflow(ctx workflow.Context, in viewInput) (OperatedSystemView, error) {
	op, err := wf.readOperatedSystem(ctx, in.OperatedAppID)
	if err != nil {
		return OperatedSystemView{}, err
	}

	health, herr := wf.getApplicationHealth(ctx, in.OperatedAppID)
	if herr != nil {
		return OperatedSystemView{}, herr
	}

	slo, serr := wf.getSloStatus(ctx, in.OperatedAppID)
	if serr != nil {
		return OperatedSystemView{}, serr
	}

	// Run-rate only (no what-if points) — same usage read the cost-projection path uses.
	appID := in.OperatedAppID
	events, uerr := wf.readUsageRange(ctx, usage.UsageRangeQuery{
		CustomerID:    wf.CustomerID,
		CycleID:       usage.CycleID(wf.CurrentCycleID),
		OperatedAppID: &appID,
	})
	if uerr != nil {
		return OperatedSystemView{}, uerr
	}
	projection, perr := wf.Estimation.ProjectForOperatedApp(
		fweng.Context{Context: context.Background()},
		observedUsageFromEvents(events),
		infrastructureKindForEstimation(wf.InfrastructureKind),
		nil, // run-rate only
	)
	if perr != nil {
		return OperatedSystemView{}, fwmgr.MapError(perr)
	}

	// OperatedSystemView / HealthSnapshotView keep the generated RuntimeStatusSeam field
	// type (this package's own façade output contract); bridge from the canonical
	// operatedsystemstate.RuntimeStatus via the surviving runtimeStatusFromState.
	view := OperatedSystemView{
		OperatedAppID: in.OperatedAppID,
		Phase:         runtimeStatusFromState(op.Status),
		InFlight:      op.InFlight,
		Health: HealthSnapshotView{
			SloMet: slo.SloMet,
			Detail: slo.Detail,
			Phase:  runtimeStatusFromState(health),
		},
		// One SLO row from the observed SLO posture. The frozen operatedRuntimeAccess SLO
		// read collapses to one posture (getSloStatus); per-component rows beyond this are
		// behind a not-yet-exposed read and are surfaced as the single rollup row.
		Slos: []SloRowView{{
			Component: "app",
			Objective: slo.Detail,
			SloMet:    slo.SloMet,
			Healthy:   health == operatedsystemstate.RuntimeStatusHealthy,
		}},
		// RecentEvents: bounded, newest-first. The head-state status history is not a
		// single RA read today (Construction-note follow-up); surfaced empty.
		RecentEvents: nil,
		Autoscaler: AutoscalerView{
			// wf.AutoscalerPolicy is already this package's own façade AutoscalerMode
			// currency (autoscalerPolicy, above) — read straight through, no bridge.
			// (autoscalerPolicyToEngine, adapters.go, converts the OTHER direction, at
			// the ProposeDesiredState call site.)
			Mode: wf.AutoscalerPolicy.Mode,
			// Decisions: not retrievable from a single frozen RA read today
			// (Construction-note follow-up); surfaced empty.
			Decisions: nil,
		},
		CurrentRunRate: moneyFromEstimation(projection.CurrentRunRate),
	}
	return view, nil
}

// runtimeStatusFromState bridges operatedsystemstate.RuntimeStatus to this package's
// generated RuntimeStatusSeam (contract.gen.go) — used at the OperatedSystemView façade
// boundary (ViewWorkflow). Kept as an explicit switch (not a raw int cast) per the
// composition root's mapping convention: RuntimeStatusSeam is a legitimately separate
// generated enum from operatedsystemstate.RuntimeStatus, even though their values line
// up today. Task 5 retired its OTHER former caller (the interventionEngine healthChange
// boundary) — see healthStatusFromRuntimeStatus below, which goes straight from
// operatedsystemstate.RuntimeStatus to intervention.HealthStatus now that the Engine is
// reached through its published contract.
func runtimeStatusFromState(s operatedsystemstate.RuntimeStatus) RuntimeStatusSeam {
	switch s {
	case operatedsystemstate.RuntimeStatusUnknown:
		// zero-value sentinel — no equivalent Unknown case to translate to yet.
		return RuntimeStatusUnknown
	case operatedsystemstate.RuntimeStatusPending:
		return RuntimeStatusPending
	case operatedsystemstate.RuntimeStatusHealthy:
		return RuntimeStatusHealthy
	case operatedsystemstate.RuntimeStatusDegraded:
		return RuntimeStatusDegraded
	case operatedsystemstate.RuntimeStatusWithdrawn:
		return RuntimeStatusWithdrawn
	default:
		return RuntimeStatusUnknown
	}
}
