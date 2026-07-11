package billing

// adapters.go holds the FOLDED composition-root adapters that bridge the published
// engine / ResourceAccess interfaces (the dependencies the GENERATED constructor
// NewBillingManager receives) to the Manager's unexported downstream seams (deps.go).
// Per the founder DI model (2026-06-28) these were retired from cmd/server and live HERE,
// in the one package that knows both sides — the Manager depends on each dependency's
// PUBLISHED interface and adapts it internally (Option-B boundary mapping), exactly as
// operations/construction fold their adapters.
//
// None of these imports Temporal (the Manager owns it); they are plain value-copy bridges
// run inside the Manager's Activities (RA seams) or directly in-workflow (Engine seams).
// The mechanical enum/struct copies map by IDENTITY (an explicit switch), not by raw int,
// so a future re-order on either side is safe. Where the published shape is RICHER than
// the Manager-local seam (extra percent/policy fields) the unset fields default to zero —
// the billing Worker carries no policy config yet, and the stub RAs return
// not-implemented at runtime regardless.

import (
	"context"
	"time"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	billingengine "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
)

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
	default:
		return billingstate.RoutingNoAction
	}
}

// ===========================================================================
// revenueLedgerAccess NO-OP stub.
//
// TODO(charge-only): the append-only inbound-revenue ledger (revenueLedgerAccess)
// was REMOVED under the charge-only model (slot 5 has no revenue-ledger component;
// inbound end-user revenue is no longer platform-tracked). The billing Manager's
// workflow still carries the revenue-fold seam (deps.go revenueLedgerAccess + the
// record/read Activities) so the close/recompute spine keeps compiling unchanged,
// but it is wired to this no-op: RecordInboundRevenue / RecordReversal are dropped
// (return a stub ref) and ReadRange returns no facts (GrossInbound folds to zero —
// under charge-only there is no revenue share, only the hosting-cost charge). A
// follow-up should excise the revenue-fold spine from the workflow entirely rather
// than keep the dormant seam.
// ===========================================================================

type noopRevenueLedger struct{}

var _ revenueLedgerAccess = noopRevenueLedger{}

func (noopRevenueLedger) RecordInboundRevenue(_ context.Context, _ revenueEntrySeam) (entryRefSeam, error) {
	return entryRefSeam(""), nil
}

func (noopRevenueLedger) RecordReversal(_ context.Context, _ reversalEntrySeam) (entryRefSeam, error) {
	return entryRefSeam(""), nil
}

func (noopRevenueLedger) ReadRange(_ context.Context, _ customerID, _ cycleID) ([]revenueEntrySeam, error) {
	return nil, nil
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
// billingEngine adapter — over billingengine.BillingEngine (the two compute
// verbs the Manager calls DIRECTLY in-workflow).
// ===========================================================================

type billingEngineAdapter struct {
	inner billingengine.BillingEngine
}

var _ billingEngine = billingEngineAdapter{}

func (a billingEngineAdapter) ComputeNet(revenue cycleRevenueSeam, usage cycleUsageSeam, terms billingstate.BillingTerms) (billingResultSeam, error) {
	res, err := a.inner.ComputeNet(
		fweng.Context{Context: context.Background()},
		cycleRevenueToEngine(revenue),
		cycleUsageToEngine(usage),
		termsToEngine(terms),
	)
	if err != nil {
		return billingResultSeam{}, err
	}
	return billingResultFromEngine(res), nil
}

func (a billingEngineAdapter) RecomputeNet(affected reBillingInputSeam) (billingResultSeam, error) {
	res, err := a.inner.RecomputeNet(
		fweng.Context{Context: context.Background()},
		billingengine.ReBillingInput{
			Revenue:      cycleRevenueToEngine(affected.Revenue),
			Usage:        cycleUsageToEngine(affected.Usage),
			Terms:        termsToEngine(affected.Terms),
			PriorSettled: billingResultToEngine(affected.PriorSettled),
		},
	)
	if err != nil {
		return billingResultSeam{}, err
	}
	return billingResultFromEngine(res), nil
}

func cycleRevenueToEngine(r cycleRevenueSeam) billingengine.CycleRevenue {
	return billingengine.CycleRevenue{
		GrossInbound: billingengine.Money{MinorUnits: r.GrossInbound.MinorUnits, Currency: r.GrossInbound.Currency},
		EventCount:   int64(r.EventCount),
	}
}

func cycleUsageToEngine(u cycleUsageSeam) billingengine.CycleUsage {
	return billingengine.CycleUsage{ComputeUnitSeconds: u.ComputeUnitSeconds}
}

func billingResultToEngine(r billingResultSeam) billingengine.BillingResult {
	return billingengine.BillingResult{
		SignedNet:           billingengine.Money{MinorUnits: r.SignedNet.MinorUnits, Currency: r.SignedNet.Currency},
		RoutingDirective:    routingDirectiveToEngine(r.RoutingDirective),
		RevenueShareApplied: billingengine.Money{MinorUnits: r.RevenueShareApplied.MinorUnits, Currency: r.RevenueShareApplied.Currency},
		ComputeCostApplied:  billingengine.Money{MinorUnits: r.ComputeCostApplied.MinorUnits, Currency: r.ComputeCostApplied.Currency},
	}
}

func billingResultFromEngine(r billingengine.BillingResult) billingResultSeam {
	return billingResultSeam{
		SignedNet:           Money{MinorUnits: r.SignedNet.MinorUnits, Currency: r.SignedNet.Currency},
		RoutingDirective:    routingDirectiveFromEngine(r.RoutingDirective),
		RevenueShareApplied: Money{MinorUnits: r.RevenueShareApplied.MinorUnits, Currency: r.RevenueShareApplied.Currency},
		ComputeCostApplied:  Money{MinorUnits: r.ComputeCostApplied.MinorUnits, Currency: r.ComputeCostApplied.Currency},
	}
}

func routingDirectiveToEngine(d routingDirectiveSeam) billingengine.RoutingDirective {
	switch d {
	case routingPayout:
		return billingengine.RoutingPayout
	case routingCharge:
		return billingengine.RoutingCharge
	case routingNoAction:
		// net == 0 (or a recompute delta == 0) — skip; same as default.
		return billingengine.RoutingNoAction
	default:
		return billingengine.RoutingNoAction
	}
}

func routingDirectiveFromEngine(d billingengine.RoutingDirective) routingDirectiveSeam {
	switch d {
	case billingengine.RoutingPayout:
		return routingPayout
	case billingengine.RoutingCharge:
		return routingCharge
	case billingengine.RoutingNoAction:
		// net == 0 (or a recompute delta == 0) — skip; same as default.
		return routingNoAction
	default:
		return routingNoAction
	}
}

// ===========================================================================
// interventionEngine adapter — over intervention.InterventionEngine (the billing-
// failure decision verb).
// ===========================================================================

type interventionAdapter struct {
	inner intervention.InterventionEngine
}

var _ interventionEngine = interventionAdapter{}

func (a interventionAdapter) DecideOnBillingFailure(failure billingFailureSeam) (billingFailureDirectiveSeam, error) {
	d, err := a.inner.DecideOnSettlementFailure(fweng.Context{Context: context.Background()}, intervention.SettlementFailure{
		CustomerID:   intervention.CustomerID(failure.CustomerID.String()),
		CycleID:      intervention.CycleID(string(failure.CycleID)),
		Kind:         billingFailureKindToEngine(failure.Kind),
		AttemptCount: int64(failure.AttemptCount),
		ShortfallAge: int64(failure.ShortfallAge),
	})
	if err != nil {
		return billingRetry, err
	}
	switch d {
	case intervention.SettlementDelay:
		return billingDelay, nil
	case intervention.SettlementEscalate:
		return billingEscalate, nil
	case intervention.SettlementRetry:
		// re-attempt the charge now (within budget) — same as default.
		return billingRetry, nil
	default:
		return billingRetry, nil
	}
}

func billingFailureKindToEngine(k billingFailureKindSeam) intervention.SettlementFailureKind {
	switch k {
	case billingFailureChargeDeclined:
		return intervention.ChargeDeclined
	case billingFailureDisputed:
		return intervention.Disputed
	case billingFailureChargedBack:
		return intervention.ChargedBack
	default:
		return intervention.SettlementFailureKindUnknown
	}
}
