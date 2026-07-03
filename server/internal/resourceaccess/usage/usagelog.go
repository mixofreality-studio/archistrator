// Package usagelog is the usageAccess component of the archistrator server's
// ResourceAccess layer — the Temporal-free port over the Usage Log, the
// APPEND-ONLY ledger of metered usage facts (contracts/usageAccess.md, FROZEN
// 2026-05-30; schema provisioned by R-PG-US 2026-06-10).
//
// This is the LEDGER discipline, not the head-state discipline
// (operational-concepts.md §13): rows are immutable metered facts — INSERTed
// once, never UPDATEd, never DELETEd (trigger-enforced at the store). There is
// no `version` token, no `applied_mutation` dedup ledger, no fwra.Conflict and
// no fwra.IdempotencyKey on this surface. Idempotency is the caller-supplied,
// globally-unique RUNTIME EVENT ID — an ordinary domain parameter — enforced
// by the DB UNIQUE constraint: a replayed batch collapses per-row to the
// already-recorded entry and returns its EntryRef as success (there is
// deliberately NO public duplicate error).
//
// 2026-06-09 repurpose (semantic, no new verb): the one unified log meters
// BOTH construction-token consumption (build phase — units_unit
// 'construction-token', no operated app) AND hosting/compute consumption
// (operation phase — 'compute-unit-second' / 'storage-byte-month' /
// 'egress-byte', attributed to an operated app) for the user's service bill.
// billingManager reads the range at billing-period close (UC5) and
// billingEngine.PriceUsage folds it; the CycleID VALUE is the billing
// PeriodID. The dimension vocabulary is an OPEN SET (the metering
// volatility) — this component never interprets Unit.
//
// Per The Method's layer model ([[the-method-layers]]): imports NO Temporal;
// no RA→RA, no RA→Engine; atomic business verbs, not CRUD; the total is
// DERIVED by the pricing Engine at read time, never aggregated or stored here.
package usagelog

import (
	"github.com/google/uuid"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// CustomerID is the billing counterparty the usage facts are scoped to
// (canonical billing aggregate key, shared with the billing stores).
// PROVISIONAL per the frozen contract §9 Q3 (escalated to D-MST); an id-type
// realignment there is an additive swap here.
type CustomerID = uuid.UUID

// CycleID is the billing period a usage fact belongs to. Post-repurpose the
// VALUE is the billing PeriodID ("PeriodID replaces the settlement-era
// CycleID"); the name keeps the frozen contract §3.1 spelling.

// OperatedAppID is the operated app a hosting/compute fact is attributed to.
// 2026-06-09 repurpose delta (recorded, not silently absorbed): construction-
// token facts are build-phase and have NO operated app — the zero uuid here
// means "absent" and persists as NULL. Hosting facts carry a non-zero id.
type OperatedAppID = uuid.UUID

// RuntimeEventID is the caller-supplied, globally-unique event identifier
// (runtime / metrics-pipeline / worker supplied) — the natural dedup token for
// an append-only ledger. A domain value, NOT a Temporal key and NOT an
// fwra.IdempotencyKey; the DB UNIQUE constraint on it collapses a replayed
// append to an idempotent success.

// ComputeUnits is one infrastructure-neutral metered quantity — never a
// priced/monetary amount (pricing is the billing Engine's Strategy) and never
// a raw cloud billing lexeme. Unit is the open-set dimension discriminator
// (e.g. "construction-token", "compute-unit-second", "storage-byte-month",
// "egress-byte"); this component stores it opaquely.

// non-negative metered quantity
// infrastructure-neutral unit name (open set)

// EntryRef is an opaque reference to one recorded ledger entry (the ledger's
// own append position). Returned by the write verbs so a caller can correlate
// an append — including the duplicate-replay case, which returns the PRIOR
// entry's ref. It is never a read key (there is no readEntry — contract §2.5).

// UsageRangeQuery is the ReadRange input (frozen Q5 shape). One read scope
// value serves both caller edges: a whole billing period (OperatedAppID nil —
// the period-close fold) and one operated app's facts (OperatedAppID set —
// the cost-projection read).

// OperatedAppID is the OPTIONAL read scope: nil = whole period (the
// period-close fold), set = one operated app's facts (the cost-projection
// read). The `,omitempty` tag is load-bearing for schema-first codegen: it
// captures this field as optional so the generated contract.gen.go preserves
// the POINTER (nil-distinguishable) shape rather than a plain value.

// UsageEvent is one immutable metered usage fact — the element of the write
// batches AND the element type ReadRange replays, in append order. There is
// ONE unified log: tick-recorded, final-recorded, and construction-token facts
// share this shape and the same table (no kind discriminator — contract §3.2).
//
// Ref and RecordedAt are SET BY THIS SEAM: they are outputs of the append
// (and populated on replay); any caller-supplied value is ignored on write.

// zero = absent (construction-token fact) → NULL

// metered, non-negative; never a priced amount
// the globally-unique dedup token (UNIQUE constraint)
// OPTIONAL opaque source-meter payload, audit only; nil if absent
// start of the observed window the fact covers
// end of the observed window (>= WindowStart)
// when the source recorded the observation (caller-supplied)
// when this ledger appended it (set by the seam)
// the entry's own append position (set by the seam)

// UsageAccess is the Temporal-free port over the Usage Log (contract §2).
// Three atomic operations: two append-writes and one range-read.
//
// Write idempotency: each event is deduped INDEPENDENTLY on its own
// RuntimeEventID via the DB UNIQUE constraint. A duplicate id is an idempotent
// no-op SUCCESS returning the prior entry's EntryRef — no second row, no
// double-count, no public Duplicate/Conflict error. A mixed batch (some new,
// some already recorded) succeeds, returning each event's ref in input order.
//
// Error kinds on this port: fwra.Transient / fwra.Infrastructure (retryable)
// and fwra.ContractMisuse (terminal — violated pre-condition). NotFound is
// NOT used: an empty period replays as an empty slice. There is NO Conflict —
// append-only means nothing contends.

// RecordComputeUsage appends a batch of observed usage facts (the periodic
// reconcile-tick record; post-repurpose also the construction-token append).
// Returns the entries' refs in input order; duplicates collapse per-row to
// the prior ref. An empty batch is a no-op success returning an empty slice.

// RecordFinalUsage appends the final usage batch captured at withdraw — the
// same fact shape into the same unified log; the distinction is the business
// moment, not a stored kind (contract §2.2/§2.4). Same idempotency contract.

// ReadRange replays the immutable usage facts in scope (whole period, or one
// operated app's facts when query.OperatedAppID is set) in append order. A
// pure, side-effect-free read: no aggregation, no stored total. An empty
// period returns an empty (non-nil) slice, not NotFound.

// Error is the shared ResourceAccess error model (framework-go), re-exported
// as an alias so this component's contract reads in its own terms while every
// RA component shares one fixed enum. Construct with fwra.New / fwra.Wrap.
type Error = fwra.Error
