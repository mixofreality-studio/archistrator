package billing

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// This file declares the Manager's CONSUMER-SIDE dependency interfaces (the Go
// "accept interfaces" idiom). All of billingManager's collaborators are either
// not yet built as Go packages, or are built but consumed via a narrow seam so
// the composition root adapts the concrete type and the test fakes stay small.
//
// Built collaborators consumed via narrow seam:
//   - billingEngine   — BUILT (internal/engine/billing). Narrow seam so the
//     composition root adapts CustomerID(uuid) → billing.CustomerID(string) and
//     maps the pricing types. No Seam suffix because the types are fully local.
//   - sourceControlAccess — BUILT (internal/resourceaccess/sourcecontrol). Only
//     the ConfirmAppInstallation op is consumed here; the composition root adapts
//     AccountRef resolution from CustomerID and drops the Installation return value.
//   - durableExecutionAccess — BUILT (internal/resourceaccess/durableexecution).
//     Two category-B ops: DeliverSignal + RegisterSchedule.
//
// Not-yet-built collaborators (consumed via seam with local mirror types):
//   - interventionEngine    — not yet built (FU-BM-3: op name drift vs concrete pkg)
//   - billingStateAccess    — not yet built
//   - usageAccess           — partially built (usagelog), but seam differs (fold op)
//   - billingGatewayAccess  — not yet built
//
// The data types each not-yet-built Engine/RA exchanges are declared here in the
// Manager-local SEAM form mirroring the frozen contract. When the owner ships, these
// local mirrors are deleted and the import substituted; no public façade op changes.

// ===========================================================================
// billingEngine — BUILT (internal/engine/billing). Narrow consumer interface
// mirroring billingEngine.BillingEngine §2.1 (PriceUsage). The composition
// root adapts the concrete engine.New() (maps CustomerID / ServicePricing types).
// Pure, deterministic: called directly in-workflow, NOT wrapped in an Activity.
// ===========================================================================

// BillingEngine mirrors billingEngine.md §2.1 — the period-close service invoice.
type BillingEngine interface {
	// PriceUsage computes the non-negative service invoice for a closed period.
	// Called DIRECTLY in closeBillingPeriodWorkflow (no Activity, pure deterministic).
	PriceUsage(usage PeriodUsageSeam, pricing ServicePricingSeam) (ServiceInvoiceSeam, error)
}

// Money is the infrastructure-neutral exact monetary amount (minor units + currency).
// Mirrors projectstate.Money; exact int64, never a float.
type Money struct {
	MinorUnits int64  `json:"minorUnits"`
	Currency   string `json:"currency"`
}

// ServicePricingKindSeam is the pricing-regime discriminator (mirrors billing.ServicePricingKind).
type ServicePricingKindSeam int

const (
	ServicePricingUnknownSeam    ServicePricingKindSeam = iota
	ServicePricingFlatMarkupSeam                        // launch: flat markup over raw token + hosting bill
)

// ServicePricingSeam mirrors billingEngine.md §3 ServicePricing — the customer's
// pricing policy snapshot, read from billing head-state and passed by value.
type ServicePricingSeam struct {
	Kind          ServicePricingKindSeam `json:"kind"`
	MarkupPercent float64                `json:"markupPercent"`
	Currency      string                 `json:"currency"`
}

// PeriodUsageSeam mirrors billingEngine.md §3 PeriodUsage — the period's metered
// usage snapshot, folded by the Manager from usageAccess and passed by value.
type PeriodUsageSeam struct {
	CustomerID         CustomerID `json:"customerId"`
	PeriodID           PeriodID   `json:"periodId"`
	ConstructionTokens int64      `json:"constructionTokens"`
	ComputeUnitSeconds float64    `json:"computeUnitSeconds"`
	StorageBytesMonths float64    `json:"storageBytesMonths"`
	EgressBytes        float64    `json:"egressBytes"`
	Currency           string     `json:"currency"`
}

// ServiceInvoiceSeam mirrors billingEngine.md §3 ServiceInvoice — the Engine's
// computed non-negative invoice amount + decomposition. Zero is valid (zero usage).
type ServiceInvoiceSeam struct {
	ServiceInvoiceAmount Money `json:"serviceInvoiceAmount"`
	TokenCharge          Money `json:"tokenCharge"`
	HostingCharge        Money `json:"hostingCharge"`
}

// ===========================================================================
// interventionEngine — FROZEN, NOT YET BUILT. Consumer interface + local mirrors
// of the billing-failure decision op. The op name in this seam is
// DecideOnBillingFailure (C-BM contract name); FU-BM-3 tracks the drift vs the
// concrete interventionEngine package (which names it DecideOnSettlementFailure).
// The composition root adapts. Pure, deterministic: direct in-workflow, no Activity.
// ===========================================================================

// InterventionEngine mirrors interventionEngine §2 for the billingManager's billing-
// failure decision. FU-BM-3: concrete package op name drift noted; seam uses
// C-BM contract naming. DECIDE → the Manager EXECUTES.
type InterventionEngine interface {
	DecideOnBillingFailure(failure BillingFailureSeam) (BillingFailureDirectiveSeam, error)
}

// BillingFailureSeam is the billing-failure context fed to the intervention decision.
type BillingFailureSeam struct {
	CustomerID   CustomerID `json:"customerId"`
	PeriodID     PeriodID   `json:"periodId"`
	AttemptCount int        `json:"attemptCount"`
}

// BillingFailureDirectiveSeam is the closed billing-failure directive set.
// The directive IDENTITY (not the numeric value) is load-bearing.
type BillingFailureDirectiveSeam int

const (
	// BillingFailureEscalate — tolerance exhausted; leave for retry sweep + delinquency signal.
	BillingFailureEscalate BillingFailureDirectiveSeam = iota
	// BillingFailureTransient — transient decline; leave for retry sweep without signal.
	BillingFailureTransient
)

// ===========================================================================
// billingStateAccess — FROZEN, NOT YET BUILT. Narrow consumer interface.
// Four ops: one cross-row read (delinquent customers), one aggregate read, and
// two additive writes (open aggregate, record invoice). Each WRITE carries
// expectedVersion + idempotencyKey; a stale-version fwra.Conflict drives the §6.5
// re-read→re-apply loop.
// ===========================================================================

// BillingStateAccess mirrors billingStateAccess §2 — the billing head-state RA.
// Reads are pure; writes carry the version guard + dedup-first idempotency key.
type BillingStateAccess interface {
	// ReadBillingAggregate returns the whole head-state aggregate (NotFound if absent).
	ReadBillingAggregate(ctx context.Context, customerID CustomerID) (BillingAggregate, error)
	// ReadDelinquentCustomers returns the persistently-delinquent customers set
	// (customers with a persistently-declined invoice outstanding).
	ReadDelinquentCustomers(ctx context.Context) ([]DelinquentCustomerSeam, error)
	// OpenBillingAggregate opens the billing aggregate (additive; expectedVersion=0 for
	// fresh registration; Conflict on race).
	OpenBillingAggregate(ctx context.Context, customerID CustomerID, expectedVersion Version, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// RecordServiceInvoice records the period-close invoice outcome (additive; Conflict loop).
	RecordServiceInvoice(ctx context.Context, customerID CustomerID, expectedVersion Version, periodID PeriodID, invoice ServiceInvoiceSeam, charged bool, idempotencyKey fwra.IdempotencyKey) (Version, error)
}

// Version is the billing head-state optimistic-concurrency version.
type Version uint64

// BillingAggregate mirrors billingStateAccess §3 — the head-state aggregate the
// workflow reads to carry expectedVersion forward, resolve ServicePricing, and guard
// against a period already closed (P3).
type BillingAggregate struct {
	ID             CustomerID         `json:"id"`
	Version        Version            `json:"version"`
	Registered     bool               `json:"registered"`
	ServicePricing ServicePricingSeam `json:"servicePricing"`
	ClosedPeriods  []ClosedPeriodSeam `json:"closedPeriods"`
}

// ClosedPeriodSeam mirrors one already-closed period recorded in the billing aggregate.
// Used by the P3 period-already-closed guard in closeBillingPeriodWorkflow.
type ClosedPeriodSeam struct {
	PeriodID PeriodID `json:"periodId"`
	Charged  bool     `json:"charged"`
}

// DelinquentCustomerSeam mirrors billingStateAccess §3 — one persistently-delinquent
// customer with the outstanding declined invoice amount.
type DelinquentCustomerSeam struct {
	CustomerID CustomerID `json:"customerId"`
	PeriodID   PeriodID   `json:"periodId"`
	Amount     Money      `json:"amount"`
}

// ===========================================================================
// usageAccess — BUILT (internal/resourceaccess/usagelog) but consumed via a narrow
// seam that presents the FOLDED period-usage snapshot the billingEngine.PriceUsage
// expects. The composition root adapts usagelog.ReadRange → fold → PeriodUsageSeam.
// Pure read; no key.
// ===========================================================================

// UsageAccess mirrors usageAccess §2 for billingManager's period-fold read.
type UsageAccess interface {
	// ReadPeriodUsage folds all usage events for the period into a PeriodUsageSeam.
	// A period with no events returns a zero-valued PeriodUsageSeam (no error).
	ReadPeriodUsage(ctx context.Context, customerID CustomerID, periodID PeriodID) (PeriodUsageSeam, error)
}

// ===========================================================================
// billingGatewayAccess — FROZEN, NOT YET BUILT. Narrow consumer interface.
// Two ops: ValidateStoredInstrument (zero-amount auth, idempotent on gatewayRequestKey)
// and ChargeUser (charge, idempotent on gatewayRequestKey via Stripe dedup).
// A hard decline surfaces as fwra.ContentPolicy (the contract's declined-charge kind,
// NOT a BillingError — routed to interventionEngine.DecideOnBillingFailure in-workflow).
// GatewayRequestKey = ${workflowId}:${activityId} (activityIdempotencyKey in activities.go).
// ===========================================================================

// BillingGatewayAccess mirrors billingGatewayAccess §2. The manager derives the
// GatewayRequestKey inside each Activity wrapper; the caller never manages keys.
type BillingGatewayAccess interface {
	// ValidateStoredInstrument performs a zero-amount authorization to confirm the
	// user's stored charge instrument is valid. Idempotent on gatewayRequestKey.
	ValidateStoredInstrument(ctx context.Context, customerID CustomerID, gatewayRequestKey string) error
	// ChargeUser charges the user for the given amount. Idempotent on gatewayRequestKey
	// (Stripe-native dedup). A hard decline returns fwra.ContentPolicy (terminal);
	// transient failures return fwra.Transient (retryable by the Activity RetryPolicy).
	ChargeUser(ctx context.Context, customerID CustomerID, amount Money, gatewayRequestKey string) error
}

// ===========================================================================
// sourceControlAccess — BUILT (internal/resourceaccess/sourcecontrol). Narrow
// consumer interface: only the app-installation confirmation the registerCustomer
// workflow needs (amendment A-1; D-SC Q2/Q3 co-location ruling). The composition
// root adapts CustomerID → AccountRef and drops the Installation return value.
// Activity-wrapped; naturally idempotent (discover/confirm).
// ===========================================================================

// SourceControlAccess is the Manager's consumer view of sourceControlAccess for the
// registerCustomer path: confirm the GitHub App is installed on the customer's account.
type SourceControlAccess interface {
	// ConfirmAppInstallation discovers/confirms aiarch's standing GitHub App
	// authorization on the customer's account. NotFound (not installed) is a
	// registration-failure terminal (surfaces as FailedPrecondition to the caller).
	ConfirmAppInstallation(ctx context.Context, customerID CustomerID) error
}

// ===========================================================================
// durableExecutionAccess — BUILT (internal/resourceaccess/durableexecution). Two
// category-B control-plane verbs: DeliverSignal (queued M→M to operationsManager)
// and RegisterSchedule (per-customer period-close + hourly retry-sweep schedules).
// awaitSignal is NOT used here — billingManager has no inbound queued Signals.
// ===========================================================================

// DurableExecutionAccess is the Manager's consumer view: the two category-B ops.
type DurableExecutionAccess interface {
	// DeliverSignal delivers a queued signal to another Manager's workflow (the
	// one sanctioned M→M edge: applyDelinquencyPolicy → operationsManager).
	DeliverSignal(ctx context.Context, targetWorkflowID string, signalName string, payload deliverSignalPayload) error
	// RegisterSchedule registers (idempotently, by id) a recurring Schedule.
	RegisterSchedule(ctx context.Context, spec scheduleSpec) error
}

// deliverSignalPayload is the applyDelinquencyPolicy payload delivered to
// operationsManager (the receiving handler deduplicates; D-DA §9 OQ3). The
// composition root adapts it onto durableexecution.ExecutionPayload.
type deliverSignalPayload struct {
	CustomerID       CustomerID `json:"customerId"`
	PauseNotWithdraw bool       `json:"pauseNotWithdraw"`
}

// scheduleSpec mirrors durableexecution.ScheduleSpec for the Schedules this Manager
// registers. The composition root adapts the concrete RA.
type scheduleSpec struct {
	ID           string
	WorkflowType string
	TaskQueue    string
	IntervalSecs int
}
