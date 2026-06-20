package settlement

// This file documents the two webhook-fed Signals the CloseCycleWorkflow handles
// (settlementManager.md §6.1/§6.2). Both are durable point-to-point Signals targeting
// the affected cycle's workflow id {customerId}:{cycleId}:close; signal-with-start
// starts the cycle workflow when it is not yet running (§6.2).
//
//   - inboundRevenueReceived (op 2.5): the cycle workflow appends the revenue fact to
//     the Revenue Ledger (idempotent on the gateway event id). The append is the
//     durable record; the net is (re)computed at cycle close, not at signal time.
//     Drained by CloseCycleWorkflow.drainInboundRevenue (workflow.go).
//   - chargebackReceived (op 2.6): the cycle workflow appends the reversal, recomputes
//     the net forward-only, records the correction, and routes the delta. Handled by
//     CloseCycleWorkflow.awaitChargeback → recomputeCycle (workflow.go).
//
// The signal PAYLOADS are the façade input types GatewayRevenueEvent /
// GatewayReversalEvent (contract.go) — the verified webhook body the Manager maps onto
// the revenueLedgerAccess-owned entry shapes. awaitSignal (the wait on these channels)
// is the Manager's OWN in-workflow primitive (D-DA category A), NOT a contract op.

// Signal names (settlementManager.md §6.1/§6.2). The two webhook-fed revenue facts are
// delivered as durable Signals to the affected cycle's workflow.
const (
	// SignalInboundRevenueReceived backs RecordInboundRevenue (op 2.5).
	SignalInboundRevenueReceived = "inboundRevenueReceived"
	// SignalChargebackReceived backs RecordRevenueReversal (op 2.6).
	SignalChargebackReceived = "chargebackReceived"
)
