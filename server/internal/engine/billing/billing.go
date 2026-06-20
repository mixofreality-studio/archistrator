// Package billing is the billingEngine — the Engine that encapsulates the
// ServicePricing volatility: how aiarch prices its OWN service (construction
// tokens consumed building the customer's system + the hosting of operated
// systems) to the user.
//
// Contract: designs/aiarch/implementation/contracts/billingEngine.md (FROZEN —
// APPROVED 2026-06-10). Re-cut from the deleted settlementEngine (D-SG) after
// the 2026-06-09 merchant-of-record reversal; this package reuses the sunk
// internal/engine/settlement compute skeleton (Money handling, Strategy host,
// exact-integer money math) and drops the settlement half entirely.
//
// CHARGE-ONLY / ONE-DIRECTIONAL (contract §2 Direction). The Engine produces a
// NON-NEGATIVE serviceInvoiceAmount aiarch charges the user. There is no
// signedNet, no RoutingDirective (payout vs charge), no revenue share, no
// payout/shortfall netting — those left with the reversed merchant-of-record
// model. A computed negative amount is an InternalInvariant bug, never a payout.
//
// PURE & DETERMINISTIC (contract §6). This package does NO I/O, reads NO clock
// (no time.Now()), uses NO RNG (no math/rand), starts no goroutines, holds no
// global mutable state, and makes NO outbound calls to any ResourceAccess,
// Manager, or other Engine (grep "billingEngine ->" architecture.dsl is empty).
// It STATES an amount as a VALUE; it never moves money. The two calling
// Managers (billingManager.closeBillingPeriod, projectDesignManager's
// SDP-assembly) read all inputs from usageAccess / billing head-state /
// project head-state, pass value snapshots in, and execute the charge
// themselves (billingGatewayAccess.chargeUser). This is what makes the
// Managers' direct in-workflow calls replay-safe — the Engine imports no
// Temporal and is never wrapped in an Activity.
//
// Money safety (contract §3, §6): money is NEVER a float — all money math is
// exact int64 minor units (one controlled float→int64 rounding crossing for
// metered usage quantities, inherited from the settlement skeleton). Charging
// real money under an unregistered ServicePricing regime is a financial-
// correctness hazard, so an unknown pricing regime returns an error
// (fweng.InvalidInput, "unknown pricing" — the contract's UnknownPricing kind,
// realized exactly as the frozen siblings realize UnknownInfrastructure /
// UnknownTerms); the Engine NEVER silently falls back to a default regime.
//
// A ZERO INVOICE IS A DOMAIN RESULT, not an error: a zero-usage period (or a
// usage fully covered by a future free tier) yields a zero amount, a normal
// return value — the Manager then issues no charge. The error channel is
// reserved for programmer / contract misuse (ContractMisuse), unknown pricing
// (InvalidInput "unknown pricing"), and broken internal invariants
// (InternalInvariant) only.
//
// Strategy axis (ServicePricing family, contract §6): the Engine pivots
// internally on the opaque ServicePricing.Kind discriminator via the
// unexported pricingStrategy registry. Adding a pricing regime (tiered floors,
// free-tier subsidies, FinOps attribution, volume discounts, per-feature cost)
// is a package-internal Strategy registration + a new enum constant — NOT a
// contract amendment.
//
// Imports ONLY projectstate (the shared Money/OptionID value types owned by
// projectStateAccess — the same downward engine→projectstate edge the sibling
// Engines use) and framework-go/engine (the shared Engine error model, fweng).
package billing

import (
	fweng "github.com/davidmarne/archistrator-platform/framework-go/engine"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
)

// ---------------------------------------------------------------------------
// Input value snapshots (contract §3). The Engine reads these as VALUES; it
// owns none of them and fetches none of them. Per the sunk settlement-skeleton
// precedent (CycleRevenue/CycleUsage), the snapshot types are defined here with
// their canonical homes noted — the canonical RA packages (usageAccess,
// billingStateAccess) do not exist yet; consolidation belongs to the activities
// that build them (see C-BE completion notes, escalations).
// ---------------------------------------------------------------------------

// CustomerID identifies the billed customer. Canonical home (future):
// billingStateAccess. Not load-bearing in the price computation — carried for
// audit/labeling on the period snapshot.
type CustomerID string

// PeriodID identifies one closed billing period for a customer. Canonical home
// (future): usageAccess / billingStateAccess. Audit/labeling only.
type PeriodID string

// PeriodUsage is a value snapshot of the billing period's metered usage, read
// by the Manager from usageAccess.readRange(customerId, periodId) and passed in
// by value. It meters BOTH construction tokens (build phase) AND hosting/
// compute (operation phase) — usageAccess was repurposed 2026-06-09 to meter
// tokens + hosting. Canonical home: usageAccess. This Engine does not own it
// and does not read it from anywhere.
type PeriodUsage struct {
	CustomerID         CustomerID
	PeriodID           PeriodID
	ConstructionTokens int64   // Σ construction-token consumption over the period (build phase)
	ComputeUnitSeconds float64 // Σ hosting/compute consumed over the period (operation phase)
	StorageBytesMonths float64 // hosting storage component (byte-months)
	EgressBytes        float64 // hosting egress component (bytes)
	Currency           string  // ISO-4217; the invoice currency for the period
}

// ServicePricingKind is the pricing-regime discriminator the Engine pivots on
// (the Strategy key). The launch regime is a flat markup over the raw
// infrastructure + token bill; future regimes (tiered floors, free-tier
// subsidies, FinOps attribution, volume discounts, per-feature cost) are
// additive constants + package-internal Strategy registrations.
type ServicePricingKind int

const (
	// ServicePricingUnknown is the zero value — no regime declared. Pricing
	// under it is forbidden (InvalidInput "unknown pricing", never a default).
	ServicePricingUnknown ServicePricingKind = iota
	// ServicePricingFlatMarkup is the launch regime: invoice = raw token cost
	// + raw hosting cost, each marked up by MarkupPercent.
	ServicePricingFlatMarkup
)

// ServicePricing is the customer's service-pricing-policy snapshot, read by
// the Manager from billing head-state via servicePricingFor(customerID) and
// passed in by value (priceUsage), or carried on the option (priceServiceFor-
// Option). Canonical home (future): billingStateAccess. Charge-only: NO
// revenue-share, NO settlement-schedule, NO shortfall parameters — those left
// with the reversed merchant-of-record model. The internal SHAPE is the
// volatility; the FACT that the policy exists is stable.
type ServicePricing struct {
	Kind ServicePricingKind
	// MarkupPercent is the FlatMarkup regime parameter: the markup applied
	// over the raw token + hosting bill (e.g. 25.0 == raw × 1.25). A negative
	// percent is a legal regime parameter (a subsidy/discount), but a markup
	// that drives a computed amount negative breaks the charge-only invariant
	// and surfaces as InternalInvariant — this Engine never states a payout.
	MarkupPercent float64
	Currency      string // ISO-4217; must match the period usage's currency
}

// ProjectOption is the input to PriceServiceForOption: the committed project
// option, which carries the customer's ServicePricing policy by value (the
// frozen-sibling precedent — D-OE OQ-2 / FU-OE-A: the option is the sanctioned
// carrier of pricing terms). The canonical Phase-2 option model is owned by
// projectStateAccess (projectstate.ProjectOption); that model still carries the
// settlement-era Terms and does NOT yet carry a ServicePricing policy, so this
// package defines the value snapshot the contract §3 specifies — re-homing the
// Pricing field onto the canonical option belongs to the projectstate-owning
// activity (escalated in the C-BE completion notes). The option's other
// option-shaping fields (network, worker mix, usage assumption) are read by
// the two estimation Engines; this Engine ignores them, so the snapshot does
// not carry them.
type ProjectOption struct {
	OptionID projectstate.OptionID
	Pricing  ServicePricing // the customer's service-pricing policy carried on the option
}

// ---------------------------------------------------------------------------
// Output value objects (contract §3) — owned by this Engine; computation
// results, not persisted head-state. The Manager persists the outcome via
// billingStateAccess.recordServiceInvoice; the Engine stores nothing.
// ---------------------------------------------------------------------------

// ServiceInvoice is the output of PriceUsage: a SINGLE non-negative amount
// aiarch charges the user for the period, plus the token/hosting decomposition
// the Manager renders into the customer-facing bill statement. It is NOT a
// signed net (charge-only — there is no payout) and NOT the executed charge —
// the Manager executes that via billingGatewayAccess.chargeUser.
type ServiceInvoice struct {
	// ServiceInvoiceAmount is non-negative: the priced sum of tokens + hosting
	// under ServicePricing. Zero is valid (zero usage / fully free-tier) — the
	// Manager then issues no charge.
	ServiceInvoiceAmount projectstate.Money
	TokenCharge          projectstate.Money // the construction-token component (for the statement)
	HostingCharge        projectstate.Money // the hosting/compute component (for the statement)
}

// HostingRate is the per-unit hosting price the commit-time projection
// surfaces, so the SDP row can show "$X build + $Y/unit hosting". A RATE, not
// an amount — no operated usage exists at commit time.
//
// C-BE field-level refinement (sanctioned by contract §3 note + OQ-6, recorded
// in the C-BE completion notes): the contract drafted PerStorageByteMonth /
// PerEgressByte, but Money is exact INTEGER minor units and a per-byte rate is
// sub-cent (degenerate — always 0 minor units), so the storage and egress
// dimensions are surfaced per GiB instead. The senior's mayAmend amendment
// (D-BE review 2026-06-10: surface the metered egress dimension on the rate)
// is preserved — egress is a first-class rate field, per GiB.
type HostingRate struct {
	PerComputeUnitSecond projectstate.Money // hosting compute rate, per compute-unit-second
	PerStorageGiBMonth   projectstate.Money // hosting storage rate, per GiB-month
	PerEgressGiB         projectstate.Money // hosting egress rate, per GiB (senior-amendment dimension)
	Currency             string             // ISO-4217
}

// ServiceCostProjection is the output of PriceServiceForOption: the
// service-cost numbers the Manager binds to the committed option for the SDP
// confirmation row, joined with constructionEstimationEngine's
// {duration, buildCost, risk} and operationEstimationEngine's usage-cost
// forecast by the MANAGER (never by an Engine→Engine call). A PROJECTION of
// pricing — what aiarch would charge — not an invoiced amount (no metered
// usage exists at commit time).
type ServiceCostProjection struct {
	TokenCostEstimate projectstate.Money // what aiarch's token pricing implies for this option's build
	HostingRate       HostingRate        // the hosting rate the ServicePricing policy implies
}

// ---------------------------------------------------------------------------
// Contract surface (contract §2). Two operations, two callers, ZERO outbound
// edges. Both ops are pure deterministic functions; both return *fweng.Error
// on programmer/contract misuse only.
// ---------------------------------------------------------------------------

// BillingEngine is the frozen two-operation Engine surface.
type BillingEngine interface {
	// PriceUsage — UC5 period-close service-invoice amount. Given the period's
	// metered usage and the customer's service-pricing policy, returns the
	// non-negative amount aiarch charges the user for the period. Called by
	// billingManager directly from closeBillingPeriod workflow code.
	// (billingEngine.md §2.1)
	PriceUsage(usage PeriodUsage, servicePricing ServicePricing) (ServiceInvoice, error)

	// PriceServiceForOption — UC2 commit-time service-cost projection. Takes
	// only the option (which carries the customer's ServicePricing policy by
	// value); returns the token-cost estimate + hosting rate bound to the
	// committed option for the SDP confirmation row. Called by
	// projectDesignManager. (billingEngine.md §2.2)
	PriceServiceForOption(option ProjectOption) (ServiceCostProjection, error)
}

// New returns the stateless billing Engine. Safe to share across calls and
// across the two calling Managers (stateless ⇒ no shared-state hazard).
func New() BillingEngine { return engine{} }

// engine is the stateless implementation. It holds no fields — all behaviour
// is a pure function of the inputs, pivoting on the package-internal
// pricingStrategy registry.
type engine struct{}

// Compile-time assertion that engine satisfies the port.
var _ BillingEngine = engine{}

// PriceUsage prices a closed period's metered usage under the customer's
// ServicePricing policy. All money math is exact int64 minor units.
// (billingEngine.md §2.1)
func (engine) PriceUsage(usage PeriodUsage, servicePricing ServicePricing) (ServiceInvoice, error) {
	// Pre-conditions — Manager wiring bugs, not "no-invoice-possible" outcomes
	// (contract §2.1 / §3: structurally-invalid input folds into ContractMisuse;
	// no separate InvalidInput-for-semantics kind on a no-parse-step Engine).
	if usage.Currency == "" {
		return ServiceInvoice{}, fweng.New(fweng.ContractMisuse,
			"PriceUsage: usage currency is empty (Manager failed to assemble a valid PeriodUsage)")
	}
	if servicePricing.Currency == "" {
		return ServiceInvoice{}, fweng.New(fweng.ContractMisuse,
			"PriceUsage: servicePricing currency is empty (Manager failed to assemble a valid ServicePricing)")
	}
	if usage.Currency != servicePricing.Currency {
		return ServiceInvoice{}, fweng.New(fweng.ContractMisuse,
			"PriceUsage: usage and servicePricing currencies mismatch")
	}
	if usage.ConstructionTokens < 0 {
		return ServiceInvoice{}, fweng.New(fweng.ContractMisuse,
			"PriceUsage: construction token count is negative")
	}
	if usage.ComputeUnitSeconds < 0 || usage.StorageBytesMonths < 0 || usage.EgressBytes < 0 {
		return ServiceInvoice{}, fweng.New(fweng.ContractMisuse,
			"PriceUsage: metered hosting quantity is negative")
	}

	strategy, serr := strategyFor(servicePricing)
	if serr != nil {
		return ServiceInvoice{}, serr
	}

	invoice := strategy.priceUsage(usage, servicePricing)

	// Internal-invariant guards (Engine bugs, not domain outcomes). The
	// non-negativity invariant is the structural enforcement of the
	// charge-only / one-directional discipline (contract §6 invariant 6):
	// this Engine never states a payout.
	if invoice.ServiceInvoiceAmount.MinorUnits < 0 {
		return ServiceInvoice{}, fweng.New(fweng.InternalInvariant,
			"PriceUsage: computed a negative serviceInvoiceAmount (charge-only Engine must never state a payout)")
	}
	if invoice.TokenCharge.MinorUnits < 0 || invoice.HostingCharge.MinorUnits < 0 {
		return ServiceInvoice{}, fweng.New(fweng.InternalInvariant,
			"PriceUsage: computed a negative invoice component")
	}
	if invoice.TokenCharge.MinorUnits > invoice.ServiceInvoiceAmount.MinorUnits {
		return ServiceInvoice{}, fweng.New(fweng.InternalInvariant,
			"PriceUsage: token charge exceeds the total invoice amount")
	}
	if invoice.TokenCharge.MinorUnits+invoice.HostingCharge.MinorUnits != invoice.ServiceInvoiceAmount.MinorUnits {
		return ServiceInvoice{}, fweng.New(fweng.InternalInvariant,
			"PriceUsage: invoice decomposition does not reconcile with the total")
	}

	return invoice, nil
}

// PriceServiceForOption projects the committed option's service cost (token
// estimate + hosting rate) under the option-carried ServicePricing policy.
// (billingEngine.md §2.2)
func (engine) PriceServiceForOption(option ProjectOption) (ServiceCostProjection, error) {
	// Pre-conditions — a nil/empty option is a Manager bug (contract §2.2).
	if option.OptionID == "" {
		return ServiceCostProjection{}, fweng.New(fweng.ContractMisuse,
			"PriceServiceForOption: option is empty (Manager failed to assemble a valid ProjectOption)")
	}
	if option.Pricing.Currency == "" {
		return ServiceCostProjection{}, fweng.New(fweng.ContractMisuse,
			"PriceServiceForOption: option pricing currency is empty")
	}

	strategy, serr := strategyFor(option.Pricing)
	if serr != nil {
		return ServiceCostProjection{}, serr
	}

	projection := strategy.project(option)

	// Internal-invariant guards: a negative estimate or per-unit rate would be
	// a per-unit payout — forbidden on a charge-only Engine.
	if projection.TokenCostEstimate.MinorUnits < 0 {
		return ServiceCostProjection{}, fweng.New(fweng.InternalInvariant,
			"PriceServiceForOption: computed a negative token cost estimate")
	}
	if projection.HostingRate.PerComputeUnitSecond.MinorUnits < 0 ||
		projection.HostingRate.PerStorageGiBMonth.MinorUnits < 0 ||
		projection.HostingRate.PerEgressGiB.MinorUnits < 0 {
		return ServiceCostProjection{}, fweng.New(fweng.InternalInvariant,
			"PriceServiceForOption: computed a negative hosting rate")
	}

	return projection, nil
}

// ---------------------------------------------------------------------------
// Internal Strategy axis (ServicePricing family — contract §6). NOT on the
// contract. Adding a pricing regime is a new ServicePricingKind constant + a
// new case here. Strategy registrations are compile-time bindings keyed off
// the opaque ServicePricing discriminator — no global mutable registry.
// ---------------------------------------------------------------------------

// pricingStrategy is the per-regime deterministic pricing Strategy (contract
// §6 conceptual shape). Implementations are pure: no I/O, no clock, no RNG.
// Pre-conditions and post-invariants are enforced by the engine wrapper, so a
// strategy computes only.
type pricingStrategy interface {
	priceUsage(usage PeriodUsage, pricing ServicePricing) ServiceInvoice
	project(option ProjectOption) ServiceCostProjection
}

// strategyFor resolves the pricing Strategy for a ServicePricing policy, or
// reports the contract's UnknownPricing error when no Strategy is compiled in
// for the regime — realized as fweng.InvalidInput("unknown pricing"), exactly
// as the frozen siblings realize UnknownInfrastructure / UnknownTerms. The
// Engine NEVER falls back to a default regime: charging real money under a
// silently-defaulted pricing regime is the financial-correctness hazard the
// rule guards against (a deploy/config bug, not a domain outcome).
func strategyFor(pricing ServicePricing) (pricingStrategy, *fweng.Error) {
	switch pricing.Kind {
	case ServicePricingFlatMarkup:
		return flatMarkupStrategy{}, nil
	default:
		return nil, fweng.New(fweng.InvalidInput, "unknown pricing")
	}
}

// ---------------------------------------------------------------------------
// Launch regime: FlatMarkup — a markup over the raw infrastructure + token
// bill. Raw unit-cost constants are deterministic Strategy constants of the
// launch regime (NOT clock/RNG/config reads), inherited from the sunk
// settlement skeleton and the operationestimation sibling. Constants chosen to
// be reasonable, not authoritative — the volatility is the SHAPE of the model.
// ---------------------------------------------------------------------------

const (
	// rawTokenCentsPerMillionTokens is the raw (pre-markup) blended LLM cost,
	// in minor units (cents), per 1,000,000 construction tokens.
	rawTokenCentsPerMillionTokens int64 = 800
	// rawComputeCentsPerComputeUnitSecond is the raw hosting compute cost per
	// metered compute-unit-second (the settlement-skeleton base constant).
	rawComputeCentsPerComputeUnitSecond float64 = 1.0
	// rawStorageCentsPerGiBMonth is the raw hosting storage cost per GiB-month.
	rawStorageCentsPerGiBMonth float64 = 10.0
	// rawEgressCentsPerGiB is the raw hosting egress cost per GiB.
	rawEgressCentsPerGiB float64 = 9.0
	// notionalBuildTokens is the deterministic commit-time assumption of the
	// construction-token volume an option's build implies, used for the
	// TokenCostEstimate (no metered usage exists at commit time; the option's
	// effort-shaping fields are the estimation Engines' inputs, not this
	// Engine's — contract §3 ProjectOption note).
	notionalBuildTokens int64 = 5_000_000
)

const bytesPerGiB = 1024.0 * 1024.0 * 1024.0

// flatMarkupStrategy is the launch ServicePricing regime: each raw component
// (token bill, hosting bill) is marked up by MarkupPercent, exactly, in
// integer minor units.
type flatMarkupStrategy struct{}

func (flatMarkupStrategy) priceUsage(usage PeriodUsage, pricing ServicePricing) ServiceInvoice {
	currency := usage.Currency

	tokenCharge := applyMarkup(rawTokenCostMinorUnits(usage.ConstructionTokens), pricing.MarkupPercent)
	hostingCharge := applyMarkup(rawHostingCostMinorUnits(usage), pricing.MarkupPercent)

	return ServiceInvoice{
		ServiceInvoiceAmount: projectstate.Money{MinorUnits: tokenCharge + hostingCharge, Currency: currency},
		TokenCharge:          projectstate.Money{MinorUnits: tokenCharge, Currency: currency},
		HostingCharge:        projectstate.Money{MinorUnits: hostingCharge, Currency: currency},
	}
}

func (flatMarkupStrategy) project(option ProjectOption) ServiceCostProjection {
	pricing := option.Pricing
	currency := pricing.Currency
	money := func(minor int64) projectstate.Money {
		return projectstate.Money{MinorUnits: minor, Currency: currency}
	}

	return ServiceCostProjection{
		TokenCostEstimate: money(applyMarkup(rawTokenCostMinorUnits(notionalBuildTokens), pricing.MarkupPercent)),
		HostingRate: HostingRate{
			PerComputeUnitSecond: money(applyMarkup(roundToInt64(rawComputeCentsPerComputeUnitSecond), pricing.MarkupPercent)),
			PerStorageGiBMonth:   money(applyMarkup(roundToInt64(rawStorageCentsPerGiBMonth), pricing.MarkupPercent)),
			PerEgressGiB:         money(applyMarkup(roundToInt64(rawEgressCentsPerGiB), pricing.MarkupPercent)),
			Currency:             currency,
		},
	}
}

// ---------------------------------------------------------------------------
// Pure helpers (no I/O, no clock, no RNG) — exact-integer money math inherited
// from the sunk settlement skeleton.
// ---------------------------------------------------------------------------

// rawTokenCostMinorUnits converts a construction-token count to raw minor
// units under the launch token rate. Exact integer arithmetic with a single
// round-half-up at the per-million divisor.
func rawTokenCostMinorUnits(tokens int64) int64 {
	return (tokens*rawTokenCentsPerMillionTokens + 500_000) / 1_000_000
}

// rawHostingCostMinorUnits sums the period's metered hosting dimensions
// (compute + storage + egress) at the raw launch unit costs. The metered
// quantities are float usage counters (not money); they are converted to
// integer minor units exactly once, here — the SINGLE controlled float→int64
// crossing — and never carried as float money thereafter.
func rawHostingCostMinorUnits(usage PeriodUsage) int64 {
	computeCents := usage.ComputeUnitSeconds * rawComputeCentsPerComputeUnitSecond
	storageCents := usage.StorageBytesMonths / bytesPerGiB * rawStorageCentsPerGiBMonth
	egressCents := usage.EgressBytes / bytesPerGiB * rawEgressCentsPerGiB
	return roundToInt64(computeCents + storageCents + egressCents)
}

// applyMarkup applies a percent markup to a minor-units base, exactly:
// base × (1 + markup/100) == base × (10000 + markupTimes100) / 10000, all in
// int64. markupTimes100 keeps two decimal places of percent precision without
// float money (the settlement-skeleton idiom).
func applyMarkup(baseMinorUnits int64, markupPercent float64) int64 {
	markupTimes100 := roundToInt64(markupPercent * 100)
	return baseMinorUnits * (10000 + markupTimes100) / 10000
}

// roundToInt64 rounds a float quantity to the nearest int64 (round half away
// from zero). Inherited verbatim from the settlement skeleton: it is the
// single controlled crossing from float usage/percent quantities to exact
// integer arithmetic; money itself is never a float.
func roundToInt64(f float64) int64 {
	if f >= 0 {
		return int64(f + 0.5)
	}
	return int64(f - 0.5)
}
