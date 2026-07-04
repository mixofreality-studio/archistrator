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
	"encoding/json"
	"time"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	billingengine "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/merchantgateway"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

// ===========================================================================
// billingStateAccess adapter — over billingstate.BillingStateAccess.
// ===========================================================================

type billingStateAdapter struct {
	inner billingstate.BillingStateAccess
}

var _ billingStateAccess = billingStateAdapter{}

func (a billingStateAdapter) ReadBilling(ctx context.Context, customerID customerID) (billingHead, error) {
	s, err := a.inner.ReadBilling(fwra.Context{Context: ctx}, customerID)
	if err != nil {
		return billingHead{}, err
	}
	return billingHead{
		ID:            s.ID,
		Version:       version(s.Version),
		GatewayBound:  s.GatewayBound,
		Registered:    s.Registered,
		Terms:         billingTermsFromState(s.Terms),
		PayoutAccount: s.PayoutAccount,
	}, nil
}

func (a billingStateAdapter) ReadPersistentlyDelinquentCustomers(ctx context.Context, scope delinquencyScope) ([]customerSummary, error) {
	rows, err := a.inner.ReadPersistentlyDelinquentCustomers(fwra.Context{Context: ctx}, billingstate.DelinquencyScope{
		ProjectID: scope.ProjectID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]customerSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, customerSummary{ID: r.ID, PauseNotWithdraw: r.PauseNotWithdraw})
	}
	return out, nil
}

func (a billingStateAdapter) RegisterCustomer(ctx context.Context, customerID customerID, expectedVersion version, profile customerProfileSeam, idempotencyKey fwra.IdempotencyKey) (version, error) {
	v, err := a.inner.RegisterCustomer(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		customerID,
		billingstate.Version(expectedVersion),
		billingstate.CustomerProfile{PayoutAccountRef: profile.PayoutAccountRef},
		idempotencyKey,
	)
	return version(v), err
}

func (a billingStateAdapter) BindGatewayLive(ctx context.Context, customerID customerID, expectedVersion version, binding gatewayBindingSeam, idempotencyKey fwra.IdempotencyKey) (version, error) {
	v, err := a.inner.BindGatewayLive(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		customerID,
		billingstate.Version(expectedVersion),
		billingstate.GatewayBinding{ConnectedAccountID: binding.ConnectedAccountID},
		idempotencyKey,
	)
	return version(v), err
}

func (a billingStateAdapter) SettleCycle(ctx context.Context, customerID customerID, expectedVersion version, cycle cycleID, outcome billingOutcomeSeam, idempotencyKey fwra.IdempotencyKey) (version, error) {
	v, err := a.inner.SettleCycle(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		customerID,
		billingstate.Version(expectedVersion),
		string(cycle),
		billingOutcomeToState(outcome),
		idempotencyKey,
	)
	return version(v), err
}

func (a billingStateAdapter) ResettleCycle(ctx context.Context, customerID customerID, expectedVersion version, cycle cycleID, correction billingOutcomeSeam, idempotencyKey fwra.IdempotencyKey) (version, error) {
	v, err := a.inner.ResettleCycle(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		customerID,
		billingstate.Version(expectedVersion),
		string(cycle),
		billingOutcomeToState(correction),
		idempotencyKey,
	)
	return version(v), err
}

func billingTermsFromState(t billingstate.BillingTerms) billingTermsSeam {
	return billingTermsSeam{
		RevenueShareKind: int(t.RevenueShareKind),
		ComputeCostKind:  int(t.ComputeCostKind),
		ScheduleKind:     int(t.ScheduleKind),
		BillingKind:      int(t.BillingKind),
	}
}

func billingOutcomeToState(o billingOutcomeSeam) billingstate.BillingOutcome {
	return billingstate.BillingOutcome{
		Net:       billingstate.Money{MinorUnits: o.Net.MinorUnits, Currency: o.Net.Currency},
		Directive: routingDirectiveToState(o.Directive),
		Escalated: o.Escalated,
	}
}

func routingDirectiveToState(d routingDirectiveSeam) billingstate.RoutingDirective {
	switch d {
	case routingPayout:
		return billingstate.RoutingPayout
	case routingCharge:
		return billingstate.RoutingCharge
	case routingNoAction:
		// net == 0 (or a recompute delta == 0) — skip; same as default.
		return billingstate.RoutingNoAction
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
// usageAccess adapter — over usage.UsageAccess (billing reads the whole cycle).
// ===========================================================================

type usageAdapter struct {
	inner usage.UsageAccess
}

var _ usageAccess = usageAdapter{}

func (a usageAdapter) ReadRange(ctx context.Context, query usageRangeQuerySeam) ([]usageEventSeam, error) {
	events, err := a.inner.ReadRange(fwra.Context{Context: ctx}, usage.UsageRangeQuery{
		CustomerID:    query.CustomerID,
		CycleID:       usage.CycleID(query.CycleID),
		OperatedAppID: query.OperatedAppID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]usageEventSeam, 0, len(events))
	for _, e := range events {
		out = append(out, usageEventSeam{
			CustomerID:    e.CustomerID,
			OperatedAppID: e.OperatedAppID,
			CycleID:       cycleID(e.CycleID),
			Units:         computeUnitsSeam{Amount: e.Units.Amount, Unit: e.Units.Unit},
			OccurredAt:    e.OccurredAt,
		})
	}
	return out, nil
}

// ===========================================================================
// merchantGatewayAccess adapter — over merchantgateway.MerchantGatewayAccess. The
// idempotency key is a plain string (Stripe-native dedup), not an fwra.IdempotencyKey.
// ===========================================================================

type merchantGatewayAdapter struct {
	inner merchantgateway.MerchantGatewayAccess
}

var _ merchantGatewayAccess = merchantGatewayAdapter{}

func (a merchantGatewayAdapter) PayoutCustomer(ctx context.Context, customerID customerID, amount Money, idempotencyKey string) error {
	return a.inner.PayoutCustomer(
		fwra.Context{Context: ctx, IdempotencyKey: fwra.IdempotencyKey(idempotencyKey)},
		customerID,
		merchantgateway.Money{MinorUnits: amount.MinorUnits, Currency: amount.Currency},
		idempotencyKey,
	)
}

func (a merchantGatewayAdapter) ChargeCustomer(ctx context.Context, customerID customerID, amount Money, idempotencyKey string) error {
	return a.inner.ChargeCustomer(
		fwra.Context{Context: ctx, IdempotencyKey: fwra.IdempotencyKey(idempotencyKey)},
		customerID,
		merchantgateway.Money{MinorUnits: amount.MinorUnits, Currency: amount.Currency},
		idempotencyKey,
	)
}

func (a merchantGatewayAdapter) CreateConnectedAccount(ctx context.Context, customerID customerID, idempotencyKey string) (gatewayBindingSeam, error) {
	b, err := a.inner.CreateConnectedAccount(
		fwra.Context{Context: ctx, IdempotencyKey: fwra.IdempotencyKey(idempotencyKey)},
		customerID,
		idempotencyKey,
	)
	if err != nil {
		return gatewayBindingSeam{}, err
	}
	return gatewayBindingSeam{ConnectedAccountID: b.ConnectedAccountID}, nil
}

func (a merchantGatewayAdapter) ValidateStoredInstrument(ctx context.Context, customerID customerID, idempotencyKey string) error {
	return a.inner.ValidateStoredInstrument(
		fwra.Context{Context: ctx, IdempotencyKey: fwra.IdempotencyKey(idempotencyKey)},
		customerID,
		idempotencyKey,
	)
}

// ===========================================================================
// operatedRuntimeAccess adapter — over operatedruntime.OperatedRuntimeAccess (only the
// onboarding wirePaymentConfig verb).
// ===========================================================================

type operatedRuntimeAdapter struct {
	inner operatedruntime.OperatedRuntimeAccess
}

var _ operatedRuntimeAccess = operatedRuntimeAdapter{}

func (a operatedRuntimeAdapter) WirePaymentConfig(ctx context.Context, deployedAppID deployedAppID, binding gatewayBindingSeam, idempotencyKey fwra.IdempotencyKey) error {
	return a.inner.WirePaymentConfig(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		deployedAppID,
		operatedruntime.GatewayBinding{ConnectedAccountID: binding.ConnectedAccountID},
		idempotencyKey,
	)
}

// ===========================================================================
// durableExecutionAccess adapter — over durableexecution.DurableExecutionAccess (the two
// category-B control-plane verbs). The seam's deliverSignalPayload is JSON-encoded into
// the published ExecutionPayload; the published ScheduleSpec resolves the task queue via
// its KindBinding table, so the seam's TaskQueue is not threaded.
// ===========================================================================

type durableAdapter struct {
	inner durableexecution.DurableExecutionAccess
}

var _ durableExecutionAccess = durableAdapter{}

func (a durableAdapter) DeliverSignal(ctx context.Context, targetWorkflowID string, signalName string, payload deliverSignalPayload) error {
	bytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return a.inner.DeliverSignal(
		fwra.Context{Context: ctx},
		durableexecution.ExecutionID(targetWorkflowID),
		durableexecution.SignalName(signalName),
		durableexecution.ExecutionPayload{Bytes: bytes, ContentType: "application/json"},
	)
}

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

func (a billingEngineAdapter) ComputeNet(revenue cycleRevenueSeam, usage cycleUsageSeam, terms billingTermsSeam) (billingResultSeam, error) {
	res, err := a.inner.ComputeNet(
		fweng.Context{Context: context.Background()},
		cycleRevenueToEngine(revenue),
		cycleUsageToEngine(usage),
		billingTermsToEngine(terms),
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
			Terms:        billingTermsToEngine(affected.Terms),
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

func billingTermsToEngine(t billingTermsSeam) billingengine.BillingTerms {
	return billingengine.BillingTerms{
		RevenueShare: billingengine.RevenueShareKind(t.RevenueShareKind),
		ComputeCost:  billingengine.ComputeCostKind(t.ComputeCostKind),
		Schedule:     billingengine.ScheduleKind(t.ScheduleKind),
	}
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
