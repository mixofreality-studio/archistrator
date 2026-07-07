package billing

import (
	"context"
)

// activities_custom.go holds the hand-written (CUSTOM) Temporal Activities the generated
// layer cannot emit — the three revenue-ledger operations wired to the manager-local
// noopRevenueLedger. They have NO frozen contract behind them (revenueLedgerAccess was
// REMOVED under the charge-only model), so temporalgen has nothing to generate; they are
// registered via the manifest's CustomActivities under their existing stable names and
// invoked from the workflow by those name constants (not the generated invoker surface).
//
// TODO(charge-only): the append-only inbound-revenue ledger (revenueLedgerAccess) was
// REMOVED under the charge-only model (slot 5 has no revenue-ledger component; inbound
// end-user revenue is no longer platform-tracked). These three Activities + the revenue-
// fold seam (deps.go revenueLedgerAccess + the record/read spine in workflow.go) keep the
// close/recompute spine compiling unchanged, but they are wired to noopRevenueLedger:
// RecordInboundRevenue / RecordReversal are dropped (return a stub ref) and ReadRange
// returns no facts (GrossInbound folds to zero — under charge-only there is no revenue
// share, only the hosting-cost charge). A follow-up should EXCISE the revenue-fold spine
// from the workflow entirely rather than keep the dormant seam.

// Custom Activity registered names — stable across the temporalgen migration (the
// workflow invokes them by these name constants via workflow.ExecuteActivity, and the
// manifest registers them under the same names).
const (
	actRecordInboundRevenue = "RecordInboundRevenueActivity"
	actRecordReversal       = "RecordReversalActivity"
	actReadRevenueRange     = "ReadRevenueRangeActivity"
)

// customActivities hosts the revenue-ledger Activities. It holds ONLY the
// revenueLedgerAccess seam (the no-op ledger) — the generated genActivities holds the
// contract-backed RA deps; these custom ops have no contract, so they live apart.
type customActivities struct {
	revenueLedger revenueLedgerAccess
}

// readRevenueRangeArgs bundles the revenue range-read inputs (a single struct so the
// Activity takes one serialisable argument).
type readRevenueRangeArgs struct {
	CustomerID customerID
	CycleID    cycleID
}

// RecordInboundRevenueActivity wraps revenueLedgerAccess.recordInboundRevenue. Dedup-id
// idempotent on entry.GatewayEventID (NO Conflict on this append-only ledger).
func (c *customActivities) RecordInboundRevenueActivity(ctx context.Context, entry revenueEntrySeam) (entryRefSeam, error) {
	return mapErr(c.revenueLedger.RecordInboundRevenue(ctx, entry))
}

// RecordReversalActivity wraps revenueLedgerAccess.recordReversal. Dedup-id idempotent
// on the chargeback's GatewayEventID.
func (c *customActivities) RecordReversalActivity(ctx context.Context, reversal reversalEntrySeam) (entryRefSeam, error) {
	return mapErr(c.revenueLedger.RecordReversal(ctx, reversal))
}

// ReadRevenueRangeActivity wraps revenueLedgerAccess.readRange. Pure read; no key.
func (c *customActivities) ReadRevenueRangeActivity(ctx context.Context, a readRevenueRangeArgs) ([]revenueEntrySeam, error) {
	return mapErr(c.revenueLedger.ReadRange(ctx, a.CustomerID, a.CycleID))
}
