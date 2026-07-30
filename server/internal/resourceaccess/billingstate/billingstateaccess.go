// revenueledgeraccess.go carries the revenueLedgerAccess CONTRACT FACET of the
// billingStateAccess component: the append-only ledger of usage-derived revenue events
// feeding the charge-only billing rail (Settlement→Billing ratification, 2026-07-03).
// The facet was re-folded into this package (reversing the ea56a36 split) per the Wave 1
// state reconciliation — one component, one Go package — while remaining a separate facet
// contract (component: billingStateAccess) in project.json .serviceContracts.
package billingstate

import (
	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// revenueledger.go is the contract impl for RevenueLedgerAccess (promoted from the
// manager-local noop seam manager/billing used to carry — see
// manager/billing/adapters.go's retired noopRevenueLedger).
//
// TODO(charge-only): the append-only inbound-revenue ledger (revenueLedgerAccess)
// was REMOVED under the charge-only model (billingManager.md §3.0: the platform
// never invoices for, or shares in, end-user revenue — R-013). The billing
// Manager's workflow still carries the revenue-fold spine (deps.go
// revenueLedgerAccess + the record/read Activities) so the close/recompute
// choreography keeps compiling unchanged, but every seam still wired to a ledger
// speaks to a permanent no-op: RecordInboundRevenue / RecordReversal are dropped
// (return a stub ref) and ReadRange returns no facts (GrossInbound folds to zero —
// under charge-only there is no revenue share, only the hosting-cost charge). This
// component is the CONTRACT-BACKED promotion of that same no-op — the manager's own
// ctx-based seam + its private noopRevenueLedger stay in place unchanged until the
// billing rewire task migrates the workflow onto this generated interface and
// deletes the manager-local duplicate.
//
// DEDUP NOTE (preserved from the manager-local noop): a real ledger implementation
// would dedup RecordInboundRevenue/RecordReversal on entry.GatewayEventID (a
// duplicate delivery is success, not an error) — this no-op has nothing to dedup
// against (it persists nothing), so every call trivially "succeeds" without ever
// observing a repeat.

// noopRevenueLedgerAccess is the permanent no-op RevenueLedgerAccess. It performs
// NO persistence — do not add any behind this type; a future real ledger is a new,
// separate implementation, not this one grown up.
type noopRevenueLedgerAccess struct{}

// NewRevenueLedgerAccess returns the permanent no-op RevenueLedgerAccess. It takes
// no arguments (there is no infrastructure binding — see the package doc TODO).
func NewRevenueLedgerAccess() RevenueLedgerAccess { return noopRevenueLedgerAccess{} }

var _ RevenueLedgerAccess = noopRevenueLedgerAccess{}

// RecordInboundRevenue drops the inbound-revenue fact (charge-only no-op; dedup on
// entry.GatewayEventID is moot — nothing is persisted to dedup against).
func (noopRevenueLedgerAccess) RecordInboundRevenue(_ fwra.Context, _ RevenueEntry) (EntryRef, error) {
	return EntryRef(""), nil
}

// RecordReversal drops the reversal/chargeback fact (charge-only no-op; dedup on
// reversal.GatewayEventID is moot — nothing is persisted to dedup against).
func (noopRevenueLedgerAccess) RecordReversal(_ fwra.Context, _ ReversalEntry) (EntryRef, error) {
	return EntryRef(""), nil
}

// ReadRange returns no facts (GrossInbound folds to zero under charge-only).
func (noopRevenueLedgerAccess) ReadRange(_ fwra.Context, _ uuid.UUID, _ string) ([]RevenueEntry, error) {
	return nil, nil
}
