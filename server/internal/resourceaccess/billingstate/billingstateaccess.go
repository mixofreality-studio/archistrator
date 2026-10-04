// Package billingstate is the billingStateAccess component: the billing head-state
// aggregate.
//
// It used to carry a second contract facet, revenueLedgerAccess — an append-only ledger
// of inbound end-user revenue — as a permanent no-op: under the charge-only model
// (billingManager.md §3.0, R-013) the platform never invoices for, or shares in,
// end-user revenue, so every append was dropped and every read returned no facts. dct
// Plan 4 Task 3 deleted the facet (spec §11 ruling A5: deleted, not deprecated). A
// future real revenue ledger is a new contract designed against a use case, not this
// stub revived.
package billingstate
