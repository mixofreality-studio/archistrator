package settlement

// adapters.go holds the FOLDED composition-root adapters that bridge the published
// engine / ResourceAccess interfaces (the dependencies the GENERATED constructor
// NewSettlementManager receives) to the Manager's unexported downstream seams (deps.go).
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
// the settlement Worker carries no policy config yet, and the stub RAs return
// not-implemented at runtime regardless.

import (
	"context"
	"encoding/json"
	"time"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	settlementengine "github.com/mixofreality-studio/archistrator/server/internal/engine/settlement"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/merchantgateway"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/revenueledger"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/settlementstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usagelog"
)

// ===========================================================================
// settlementStateAccess adapter — over settlementstate.SettlementStateAccess.
// ===========================================================================

type settlementStateAdapter struct {
	inner settlementstate.SettlementStateAccess
}

var _ settlementStateAccess = settlementStateAdapter{}

func (a settlementStateAdapter) ReadSettlement(ctx context.Context, customerID CustomerID) (Settlement, error) {
	s, err := a.inner.ReadSettlement(fwra.Context{Context: ctx}, customerID)
	if err != nil {
		return Settlement{}, err
	}
	return Settlement{
		ID:            s.ID,
		Version:       Version(s.Version),
		GatewayBound:  s.GatewayBound,
		Registered:    s.Registered,
		Terms:         settlementTermsFromState(s.Terms),
		PayoutAccount: s.PayoutAccount,
	}, nil
}

func (a settlementStateAdapter) ReadPersistentlyDelinquentCustomers(ctx context.Context, scope DelinquencyScope) ([]CustomerSummary, error) {
	rows, err := a.inner.ReadPersistentlyDelinquentCustomers(fwra.Context{Context: ctx}, settlementstate.DelinquencyScope{
		ProjectID: scope.ProjectID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]CustomerSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, CustomerSummary{ID: r.ID, PauseNotWithdraw: r.PauseNotWithdraw})
	}
	return out, nil
}

func (a settlementStateAdapter) RegisterCustomer(ctx context.Context, customerID CustomerID, expectedVersion Version, profile CustomerProfileSeam, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	v, err := a.inner.RegisterCustomer(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		customerID,
		settlementstate.Version(expectedVersion),
		settlementstate.CustomerProfile{PayoutAccountRef: profile.PayoutAccountRef},
		idempotencyKey,
	)
	return Version(v), err
}

func (a settlementStateAdapter) BindGatewayLive(ctx context.Context, customerID CustomerID, expectedVersion Version, binding GatewayBindingSeam, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	v, err := a.inner.BindGatewayLive(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		customerID,
		settlementstate.Version(expectedVersion),
		settlementstate.GatewayBinding{ConnectedAccountID: binding.ConnectedAccountID},
		idempotencyKey,
	)
	return Version(v), err
}

func (a settlementStateAdapter) SettleCycle(ctx context.Context, customerID CustomerID, expectedVersion Version, cycle CycleID, outcome SettlementOutcomeSeam, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	v, err := a.inner.SettleCycle(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		customerID,
		settlementstate.Version(expectedVersion),
		string(cycle),
		settlementOutcomeToState(outcome),
		idempotencyKey,
	)
	return Version(v), err
}

func (a settlementStateAdapter) ResettleCycle(ctx context.Context, customerID CustomerID, expectedVersion Version, cycle CycleID, correction SettlementOutcomeSeam, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	v, err := a.inner.ResettleCycle(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		customerID,
		settlementstate.Version(expectedVersion),
		string(cycle),
		settlementOutcomeToState(correction),
		idempotencyKey,
	)
	return Version(v), err
}

func settlementTermsFromState(t settlementstate.SettlementTerms) SettlementTermsSeam {
	return SettlementTermsSeam{
		RevenueShareKind: int(t.RevenueShareKind),
		ComputeCostKind:  int(t.ComputeCostKind),
		ScheduleKind:     int(t.ScheduleKind),
		BillingKind:      int(t.BillingKind),
	}
}

func settlementOutcomeToState(o SettlementOutcomeSeam) settlementstate.SettlementOutcome {
	return settlementstate.SettlementOutcome{
		Net:       settlementstate.Money{MinorUnits: o.Net.MinorUnits, Currency: o.Net.Currency},
		Directive: routingDirectiveToState(o.Directive),
		Escalated: o.Escalated,
	}
}

func routingDirectiveToState(d RoutingDirectiveSeam) settlementstate.RoutingDirective {
	switch d {
	case RoutingPayout:
		return settlementstate.RoutingPayout
	case RoutingCharge:
		return settlementstate.RoutingCharge
	default:
		return settlementstate.RoutingNoAction
	}
}

// ===========================================================================
// revenueLedgerAccess adapter — over revenueledger.RevenueLedgerAccess.
// ===========================================================================

type revenueLedgerAdapter struct {
	inner revenueledger.RevenueLedgerAccess
}

var _ revenueLedgerAccess = revenueLedgerAdapter{}

func (a revenueLedgerAdapter) RecordInboundRevenue(ctx context.Context, entry RevenueEntrySeam) (EntryRefSeam, error) {
	ref, err := a.inner.RecordInboundRevenue(fwra.Context{Context: ctx}, revenueledger.RevenueEntry{
		CustomerID:     entry.CustomerID,
		CycleID:        string(entry.CycleID),
		Kind:           revenueKindToLedger(entry.Kind),
		Amount:         revenueledger.Money{MinorUnits: entry.Amount.MinorUnits, Currency: entry.Amount.Currency},
		GatewayEventID: entry.GatewayEventID,
		OccurredAt:     entry.OccurredAt,
	})
	return EntryRefSeam(ref), err
}

func (a revenueLedgerAdapter) RecordReversal(ctx context.Context, reversal ReversalEntrySeam) (EntryRefSeam, error) {
	ref, err := a.inner.RecordReversal(fwra.Context{Context: ctx}, revenueledger.ReversalEntry{
		CustomerID:             reversal.CustomerID,
		CycleID:                string(reversal.CycleID),
		Amount:                 revenueledger.Money{MinorUnits: reversal.Amount.MinorUnits, Currency: reversal.Amount.Currency},
		GatewayEventID:         reversal.GatewayEventID,
		ReversesGatewayEventID: reversal.ReversesGatewayEventID,
		OccurredAt:             reversal.OccurredAt,
	})
	return EntryRefSeam(ref), err
}

func (a revenueLedgerAdapter) ReadRange(ctx context.Context, customerID CustomerID, cycleID CycleID) ([]RevenueEntrySeam, error) {
	entries, err := a.inner.ReadRange(fwra.Context{Context: ctx}, customerID, string(cycleID))
	if err != nil {
		return nil, err
	}
	out := make([]RevenueEntrySeam, 0, len(entries))
	for _, e := range entries {
		out = append(out, RevenueEntrySeam{
			CustomerID:     e.CustomerID,
			CycleID:        CycleID(e.CycleID),
			Kind:           revenueKindFromLedger(e.Kind),
			Amount:         Money{MinorUnits: e.Amount.MinorUnits, Currency: e.Amount.Currency},
			GatewayEventID: e.GatewayEventID,
			OccurredAt:     e.OccurredAt,
		})
	}
	return out, nil
}

func revenueKindToLedger(k RevenueKindSeam) revenueledger.RevenueKind {
	if k == RevenueKindReversal {
		return revenueledger.RevenueKindReversal
	}
	return revenueledger.RevenueKindInbound
}

func revenueKindFromLedger(k revenueledger.RevenueKind) RevenueKindSeam {
	if k == revenueledger.RevenueKindReversal {
		return RevenueKindReversal
	}
	return RevenueKindInbound
}

// ===========================================================================
// usageAccess adapter — over usagelog.UsageAccess (settlement reads the whole cycle).
// ===========================================================================

type usageAdapter struct {
	inner usagelog.UsageAccess
}

var _ usageAccess = usageAdapter{}

func (a usageAdapter) ReadRange(ctx context.Context, query UsageRangeQuerySeam) ([]UsageEventSeam, error) {
	events, err := a.inner.ReadRange(fwra.Context{Context: ctx}, usagelog.UsageRangeQuery{
		CustomerID:    query.CustomerID,
		CycleID:       usagelog.CycleID(query.CycleID),
		OperatedAppID: query.OperatedAppID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]UsageEventSeam, 0, len(events))
	for _, e := range events {
		out = append(out, UsageEventSeam{
			CustomerID:    e.CustomerID,
			OperatedAppID: e.OperatedAppID,
			CycleID:       CycleID(e.CycleID),
			Units:         ComputeUnitsSeam{Amount: e.Units.Amount, Unit: e.Units.Unit},
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

func (a merchantGatewayAdapter) PayoutCustomer(ctx context.Context, customerID CustomerID, amount Money, idempotencyKey string) error {
	return a.inner.PayoutCustomer(
		fwra.Context{Context: ctx, IdempotencyKey: fwra.IdempotencyKey(idempotencyKey)},
		customerID,
		merchantgateway.Money{MinorUnits: amount.MinorUnits, Currency: amount.Currency},
		idempotencyKey,
	)
}

func (a merchantGatewayAdapter) ChargeCustomer(ctx context.Context, customerID CustomerID, amount Money, idempotencyKey string) error {
	return a.inner.ChargeCustomer(
		fwra.Context{Context: ctx, IdempotencyKey: fwra.IdempotencyKey(idempotencyKey)},
		customerID,
		merchantgateway.Money{MinorUnits: amount.MinorUnits, Currency: amount.Currency},
		idempotencyKey,
	)
}

func (a merchantGatewayAdapter) CreateConnectedAccount(ctx context.Context, customerID CustomerID, idempotencyKey string) (GatewayBindingSeam, error) {
	b, err := a.inner.CreateConnectedAccount(
		fwra.Context{Context: ctx, IdempotencyKey: fwra.IdempotencyKey(idempotencyKey)},
		customerID,
		idempotencyKey,
	)
	if err != nil {
		return GatewayBindingSeam{}, err
	}
	return GatewayBindingSeam{ConnectedAccountID: b.ConnectedAccountID}, nil
}

func (a merchantGatewayAdapter) ValidateStoredInstrument(ctx context.Context, customerID CustomerID, idempotencyKey string) error {
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

func (a operatedRuntimeAdapter) WirePaymentConfig(ctx context.Context, deployedAppID DeployedAppID, binding GatewayBindingSeam, idempotencyKey fwra.IdempotencyKey) error {
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
// settlementEngine adapter — over settlementengine.SettlementEngine (the two compute
// verbs the Manager calls DIRECTLY in-workflow).
// ===========================================================================

type settlementEngineAdapter struct {
	inner settlementengine.SettlementEngine
}

var _ settlementEngine = settlementEngineAdapter{}

func (a settlementEngineAdapter) ComputeNet(revenue CycleRevenueSeam, usage CycleUsageSeam, terms SettlementTermsSeam) (SettlementResultSeam, error) {
	res, err := a.inner.ComputeNet(
		fweng.Context{Context: context.Background()},
		cycleRevenueToEngine(revenue),
		cycleUsageToEngine(usage),
		settlementTermsToEngine(terms),
	)
	if err != nil {
		return SettlementResultSeam{}, err
	}
	return settlementResultFromEngine(res), nil
}

func (a settlementEngineAdapter) RecomputeNet(affected ReSettlementInputSeam) (SettlementResultSeam, error) {
	res, err := a.inner.RecomputeNet(
		fweng.Context{Context: context.Background()},
		settlementengine.ReSettlementInput{
			Revenue:      cycleRevenueToEngine(affected.Revenue),
			Usage:        cycleUsageToEngine(affected.Usage),
			Terms:        settlementTermsToEngine(affected.Terms),
			PriorSettled: settlementResultToEngine(affected.PriorSettled),
		},
	)
	if err != nil {
		return SettlementResultSeam{}, err
	}
	return settlementResultFromEngine(res), nil
}

func cycleRevenueToEngine(r CycleRevenueSeam) settlementengine.CycleRevenue {
	return settlementengine.CycleRevenue{
		GrossInbound: settlementengine.Money{MinorUnits: r.GrossInbound.MinorUnits, Currency: r.GrossInbound.Currency},
		EventCount:   int64(r.EventCount),
	}
}

func cycleUsageToEngine(u CycleUsageSeam) settlementengine.CycleUsage {
	return settlementengine.CycleUsage{ComputeUnitSeconds: u.ComputeUnitSeconds}
}

func settlementTermsToEngine(t SettlementTermsSeam) settlementengine.SettlementTerms {
	return settlementengine.SettlementTerms{
		RevenueShare: settlementengine.RevenueShareKind(t.RevenueShareKind),
		ComputeCost:  settlementengine.ComputeCostKind(t.ComputeCostKind),
		Schedule:     settlementengine.ScheduleKind(t.ScheduleKind),
	}
}

func settlementResultToEngine(r SettlementResultSeam) settlementengine.SettlementResult {
	return settlementengine.SettlementResult{
		SignedNet:           settlementengine.Money{MinorUnits: r.SignedNet.MinorUnits, Currency: r.SignedNet.Currency},
		RoutingDirective:    routingDirectiveToEngine(r.RoutingDirective),
		RevenueShareApplied: settlementengine.Money{MinorUnits: r.RevenueShareApplied.MinorUnits, Currency: r.RevenueShareApplied.Currency},
		ComputeCostApplied:  settlementengine.Money{MinorUnits: r.ComputeCostApplied.MinorUnits, Currency: r.ComputeCostApplied.Currency},
	}
}

func settlementResultFromEngine(r settlementengine.SettlementResult) SettlementResultSeam {
	return SettlementResultSeam{
		SignedNet:           Money{MinorUnits: r.SignedNet.MinorUnits, Currency: r.SignedNet.Currency},
		RoutingDirective:    routingDirectiveFromEngine(r.RoutingDirective),
		RevenueShareApplied: Money{MinorUnits: r.RevenueShareApplied.MinorUnits, Currency: r.RevenueShareApplied.Currency},
		ComputeCostApplied:  Money{MinorUnits: r.ComputeCostApplied.MinorUnits, Currency: r.ComputeCostApplied.Currency},
	}
}

func routingDirectiveToEngine(d RoutingDirectiveSeam) settlementengine.RoutingDirective {
	switch d {
	case RoutingPayout:
		return settlementengine.RoutingPayout
	case RoutingCharge:
		return settlementengine.RoutingCharge
	default:
		return settlementengine.RoutingNoAction
	}
}

func routingDirectiveFromEngine(d settlementengine.RoutingDirective) RoutingDirectiveSeam {
	switch d {
	case settlementengine.RoutingPayout:
		return RoutingPayout
	case settlementengine.RoutingCharge:
		return RoutingCharge
	default:
		return RoutingNoAction
	}
}

// ===========================================================================
// interventionEngine adapter — over intervention.InterventionEngine (the settlement-
// failure decision verb).
// ===========================================================================

type interventionAdapter struct {
	inner intervention.InterventionEngine
}

var _ interventionEngine = interventionAdapter{}

func (a interventionAdapter) DecideOnSettlementFailure(failure SettlementFailureSeam) (SettlementFailureDirectiveSeam, error) {
	d, err := a.inner.DecideOnSettlementFailure(fweng.Context{Context: context.Background()}, intervention.SettlementFailure{
		CustomerID:   intervention.CustomerID(failure.CustomerID.String()),
		CycleID:      intervention.CycleID(string(failure.CycleID)),
		Kind:         settlementFailureKindToEngine(failure.Kind),
		AttemptCount: int64(failure.AttemptCount),
		ShortfallAge: int64(failure.ShortfallAge),
	})
	if err != nil {
		return SettlementRetry, err
	}
	switch d {
	case intervention.SettlementDelay:
		return SettlementDelay, nil
	case intervention.SettlementEscalate:
		return SettlementEscalate, nil
	default:
		return SettlementRetry, nil
	}
}

func settlementFailureKindToEngine(k SettlementFailureKindSeam) intervention.SettlementFailureKind {
	switch k {
	case SettlementFailureChargeDeclined:
		return intervention.ChargeDeclined
	case SettlementFailureDisputed:
		return intervention.Disputed
	case SettlementFailureChargedBack:
		return intervention.ChargedBack
	default:
		return intervention.SettlementFailureKindUnknown
	}
}
