package billing

// adapters.go holds the FOLDED composition-root adapters that bridge the published
// ResourceAccess interfaces (the dependencies the GENERATED constructor
// NewBillingManager receives) to the Manager's unexported downstream seams (deps.go),
// plus the two REAL Engine-contract divergence bridges (termsToEngine,
// routingDirectiveToState). Per the founder DI model (2026-06-28) these were retired
// from cmd/server and live HERE, in the one package that knows both sides — the
// Manager depends on each dependency's PUBLISHED interface and adapts it internally
// (Option-B boundary mapping), exactly as operations/construction fold their adapters.
//
// None of these imports Temporal (the Manager owns it); they are plain value-copy bridges
// run inside the Manager's Activities (RA seams). The two Engines (billingengine.
// BillingEngine / intervention.InterventionEngine) have NO adapter — the workflow calls
// their published contracts directly (workflow.go). The mechanical enum/struct copies
// map by IDENTITY (an explicit switch), not by raw int, so a future re-order on either
// side is safe. Where the published shape is RICHER than the Manager-local seam (extra
// percent/policy fields) the unset fields default to zero — the billing Worker carries
// no policy config yet, and the stub RAs return not-implemented at runtime regardless.

import (
	"context"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	billingengine "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
)

// revenueLedgerAccess (B7): the former Manager-local seam + noopRevenueLedger stub
// adapter are RETIRED. The workflow now reaches this RA through the generated typed
// invokers (invokers.gen.go), speaking billingstate.RevenueEntry/ReversalEntry/EntryRef
// directly — no adapter needed (see workermanifest.go WorkerManifest, which threads
// m.revenueLedger straight into genActivities.RevenueLedger).

// ===========================================================================
// billingStateAccess contract converters. The former billingStateAdapter struct AND
// the Manager-local billingHead/billingOutcomeSeam/gatewayBindingSeam/customerSummary/
// delinquencyScope/version mirrors are retired — the workflow reaches the RA through
// the generated invokers (invokers.gen.go) and speaks the billingstate contract types
// (Billing, BillingTerms, BillingOutcome, CustomerSummary, DelinquencyScope,
// GatewayBinding, Version) directly. The two converters below are the REAL
// divergences that remain: the engine-owned BillingTerms/RoutingDirective are
// distinct named types from their billingstate counterparts.
// ===========================================================================

// termsToEngine bridges the RA-owned terms head-state onto the engine's compute
// input. The two contracts genuinely disagree (the engine carries percent fields
// the head-state does not); zero-fill preserves today's behavior. Divergence
// earmarked for a project.json contract alignment (see plan Task 12 earmarks).
func termsToEngine(t billingstate.BillingTerms) billingengine.BillingTerms {
	return billingengine.BillingTerms{
		RevenueShare: billingengine.RevenueShareKind(t.RevenueShareKind),
		ComputeCost:  billingengine.ComputeCostKind(t.ComputeCostKind),
		Schedule:     billingengine.ScheduleKind(t.ScheduleKind),
	}
}

// routingDirectiveToState maps the engine-owned routing decision onto the
// RA-owned persisted enum by IDENTITY (explicit switch — re-order safe).
func routingDirectiveToState(d billingengine.RoutingDirective) billingstate.RoutingDirective {
	switch d {
	case billingengine.RoutingPayout:
		return billingstate.RoutingPayout
	case billingengine.RoutingCharge:
		return billingstate.RoutingCharge
	case billingengine.RoutingNoAction:
		return billingstate.RoutingNoAction
	default:
		return billingstate.RoutingNoAction
	}
}

// ===========================================================================
// durableExecutionAccess adapter — over durableexecution.DurableExecutionAccess. Only
// the startup RegisterSchedule verb is consumed (the platform-wide shortfallSweep; the
// workflow-invoked deliverSignal + per-customer registerSchedule now go through the
// generated invokers). The published ScheduleSpec resolves the task queue via its
// KindBinding table, so the seam's TaskQueue is not threaded.
// ===========================================================================

type durableAdapter struct {
	inner durableexecution.DurableExecutionAccess
}

var _ durableExecutionAccess = durableAdapter{}

func (a durableAdapter) RegisterSchedule(ctx context.Context, spec scheduleSpec) error {
	return a.inner.RegisterSchedule(
		fwra.Context{Context: ctx},
		durableexecution.ScheduleID(spec.ID),
		durableexecution.ScheduleSpec{
			ExecutionKind: durableexecution.ExecutionKind(spec.WorkflowType),
			Cadence:       durableexecution.Cadence{Every: time.Duration(spec.IntervalSecs) * time.Second},
		},
	)
}

// ===========================================================================
// billingEngine / interventionEngine adapters — RETIRED. The workflow calls the
// published billingengine.BillingEngine / intervention.InterventionEngine contracts
// DIRECTLY (workflow.go), passing fweng.Context{Context: context.Background()} inline
// at each call site. termsToEngine + routingDirectiveToState above remain the two REAL
// divergence bridges.
// ===========================================================================
