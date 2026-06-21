package billing

import (
	"errors"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// launchPricing is the registered launch regime: a flat 25% markup over the
// raw token + hosting bill, priced in USD.
func launchPricing() ServicePricing {
	return ServicePricing{
		Kind:          ServicePricingFlatMarkup,
		MarkupPercent: 25.0,
		Currency:      "USD",
	}
}

func usd(minor int64) projectstate.Money {
	return projectstate.Money{MinorUnits: minor, Currency: "USD"}
}

const gib = 1024.0 * 1024.0 * 1024.0

// TestPriceUsage_FlatMarkup is the per-Strategy table for the launch FlatMarkup
// regime: happy path, zero usage, each metered hosting dimension (incl. the
// senior-amendment egress dimension), unknown pricing, and contract misuse.
func TestPriceUsage_FlatMarkup(t *testing.T) {
	e := New()

	tests := []struct {
		name      string
		usage     PeriodUsage
		pricing   ServicePricing
		want      ServiceInvoice
		wantErr   bool
		errKind   fweng.Kind
		errDetail string
	}{
		{
			// Tokens: raw = (2,000,000 × 800¢/M) = 1600¢; × 1.25 = 2000¢.
			// Hosting: 100 CUS × 1¢ = 100; 2 GiB-months × 10¢ = 20; 1 GiB egress
			// × 9¢ = 9 → raw 129¢; × 1.25 = 161¢ (exact integer trunc of 161.25).
			// Total = 2161. Decomposition reconciles exactly.
			name: "happy path prices tokens plus hosting under the markup",
			usage: PeriodUsage{
				CustomerID:         "cust-1",
				PeriodID:           "2026-06",
				ConstructionTokens: 2_000_000,
				ComputeUnitSeconds: 100,
				StorageBytesMonths: 2 * gib,
				EgressBytes:        1 * gib,
				Currency:           "USD",
			},
			pricing: launchPricing(),
			want: ServiceInvoice{
				ServiceInvoiceAmount: usd(2161),
				TokenCharge:          usd(2000),
				HostingCharge:        usd(161),
			},
		},
		{
			// Zero usage ⇒ zero invoice — a NORMAL return value, not an error
			// (the Manager then issues no charge).
			name:    "zero usage yields a zero invoice, not an error",
			usage:   PeriodUsage{CustomerID: "cust-1", PeriodID: "2026-06", Currency: "USD"},
			pricing: launchPricing(),
			want: ServiceInvoice{
				ServiceInvoiceAmount: usd(0),
				TokenCharge:          usd(0),
				HostingCharge:        usd(0),
			},
		},
		{
			// PerEgressByte hosting dimension (senior mayAmend amendment): an
			// egress-only period must produce a non-zero hosting charge.
			// 10 GiB × 9¢ = 90¢ raw; × 1.25 = 112¢ (trunc of 112.5).
			name: "egress-only usage is priced (senior-amendment hosting dimension)",
			usage: PeriodUsage{
				CustomerID: "cust-1", PeriodID: "2026-06",
				EgressBytes: 10 * gib, Currency: "USD",
			},
			pricing: launchPricing(),
			want: ServiceInvoice{
				ServiceInvoiceAmount: usd(112),
				TokenCharge:          usd(0),
				HostingCharge:        usd(112),
			},
		},
		{
			// Tokens-only at 0% markup: pass-through of the raw token bill.
			name: "zero markup passes the raw bill through",
			usage: PeriodUsage{
				CustomerID: "cust-1", PeriodID: "2026-06",
				ConstructionTokens: 1_000_000, Currency: "USD",
			},
			pricing: ServicePricing{Kind: ServicePricingFlatMarkup, MarkupPercent: 0, Currency: "USD"},
			want: ServiceInvoice{
				ServiceInvoiceAmount: usd(800),
				TokenCharge:          usd(800),
				HostingCharge:        usd(0),
			},
		},
		{
			// UnknownPricing (contract §3): an undeclared regime is a deploy/
			// config bug — NEVER a silent default to the wrong pricing.
			name:      "unknown pricing kind is unknown-pricing error",
			usage:     PeriodUsage{CustomerID: "c", PeriodID: "p", ConstructionTokens: 1, Currency: "USD"},
			pricing:   ServicePricing{Kind: ServicePricingUnknown, MarkupPercent: 25, Currency: "USD"},
			wantErr:   true,
			errKind:   fweng.InvalidInput,
			errDetail: "unknown pricing",
		},
		{
			name:      "unregistered pricing kind is unknown-pricing error",
			usage:     PeriodUsage{CustomerID: "c", PeriodID: "p", ConstructionTokens: 1, Currency: "USD"},
			pricing:   ServicePricing{Kind: ServicePricingKind(99), MarkupPercent: 25, Currency: "USD"},
			wantErr:   true,
			errKind:   fweng.InvalidInput,
			errDetail: "unknown pricing",
		},
		{
			name:    "negative token count is contract misuse",
			usage:   PeriodUsage{CustomerID: "c", PeriodID: "p", ConstructionTokens: -1, Currency: "USD"},
			pricing: launchPricing(),
			wantErr: true,
			errKind: fweng.ContractMisuse,
		},
		{
			name:    "negative compute seconds is contract misuse",
			usage:   PeriodUsage{CustomerID: "c", PeriodID: "p", ComputeUnitSeconds: -1, Currency: "USD"},
			pricing: launchPricing(),
			wantErr: true,
			errKind: fweng.ContractMisuse,
		},
		{
			name:    "empty usage currency is contract misuse",
			usage:   PeriodUsage{CustomerID: "c", PeriodID: "p", ConstructionTokens: 1},
			pricing: launchPricing(),
			wantErr: true,
			errKind: fweng.ContractMisuse,
		},
		{
			name:  "mismatched currencies is contract misuse",
			usage: PeriodUsage{CustomerID: "c", PeriodID: "p", ConstructionTokens: 1, Currency: "EUR"},
			pricing: ServicePricing{
				Kind: ServicePricingFlatMarkup, MarkupPercent: 25, Currency: "USD",
			},
			wantErr: true,
			errKind: fweng.ContractMisuse,
		},
		{
			// Non-negativity invariant (contract §6 invariant 6): a regime
			// parameter that drives the computed amount negative is an ENGINE
			// invariant breach — charge-only, never a payout.
			name: "computed negative amount is an internal-invariant error, never a payout",
			usage: PeriodUsage{
				CustomerID: "c", PeriodID: "p",
				ConstructionTokens: 1_000_000, Currency: "USD",
			},
			pricing: ServicePricing{Kind: ServicePricingFlatMarkup, MarkupPercent: -200, Currency: "USD"},
			wantErr: true,
			errKind: fweng.InternalInvariant,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.PriceUsage(tt.usage, tt.pricing)
			if tt.wantErr {
				assertEngineErr(t, err, tt.errKind, tt.errDetail)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("invoice = %+v, want %+v", got, tt.want)
			}
			if got.ServiceInvoiceAmount.MinorUnits < 0 {
				t.Fatalf("non-negativity invariant violated: %d", got.ServiceInvoiceAmount.MinorUnits)
			}
		})
	}
}

// TestPriceServiceForOption_FlatMarkup is the per-Strategy table for the
// commit-time projection under the launch FlatMarkup regime.
func TestPriceServiceForOption_FlatMarkup(t *testing.T) {
	e := New()

	tests := []struct {
		name      string
		option    ProjectOption
		want      ServiceCostProjection
		wantErr   bool
		errKind   fweng.Kind
		errDetail string
	}{
		{
			// Token estimate: notional 5M build tokens × 800¢/M = 4000¢ raw;
			// × 1.25 = 5000¢. Rates: 1¢/CUS → 1¢; 10¢/GiB-month → 12¢ (trunc of
			// 12.5); 9¢/GiB egress → 11¢ (trunc of 11.25) — the egress dimension
			// is surfaced per the senior mayAmend amendment.
			name:   "happy path projects token estimate and per-unit hosting rates",
			option: ProjectOption{OptionID: "opt-normal", Pricing: launchPricing()},
			want: ServiceCostProjection{
				TokenCostEstimate: usd(5000),
				HostingRate: HostingRate{
					PerComputeUnitSecond: usd(1),
					PerStorageGiBMonth:   usd(12),
					PerEgressGiB:         usd(11),
					Currency:             "USD",
				},
			},
		},
		{
			name: "unknown pricing on the option is unknown-pricing error",
			option: ProjectOption{
				OptionID: "opt-normal",
				Pricing:  ServicePricing{Kind: ServicePricingUnknown, Currency: "USD"},
			},
			wantErr:   true,
			errKind:   fweng.InvalidInput,
			errDetail: "unknown pricing",
		},
		{
			name:    "empty option is contract misuse",
			option:  ProjectOption{},
			wantErr: true,
			errKind: fweng.ContractMisuse,
		},
		{
			name: "empty pricing currency on the option is contract misuse",
			option: ProjectOption{
				OptionID: "opt-normal",
				Pricing:  ServicePricing{Kind: ServicePricingFlatMarkup, MarkupPercent: 25},
			},
			wantErr: true,
			errKind: fweng.ContractMisuse,
		},
		{
			// Charge-only discipline holds at projection time too: a parameter
			// that turns the estimate/rates negative is an Engine invariant breach.
			name: "computed negative projection is an internal-invariant error",
			option: ProjectOption{
				OptionID: "opt-normal",
				Pricing:  ServicePricing{Kind: ServicePricingFlatMarkup, MarkupPercent: -200, Currency: "USD"},
			},
			wantErr: true,
			errKind: fweng.InternalInvariant,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.PriceServiceForOption(tt.option)
			if tt.wantErr {
				assertEngineErr(t, err, tt.errKind, tt.errDetail)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("projection = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestDeterminism asserts that identical inputs yield identical outputs across
// repeated invocations of BOTH ops (the Engine reads no clock/RNG/state) —
// the property that makes the Managers' direct in-workflow calls replay-safe.
func TestDeterminism(t *testing.T) {
	e := New()
	usage := PeriodUsage{
		CustomerID: "cust-1", PeriodID: "2026-06",
		ConstructionTokens: 1_234_567,
		ComputeUnitSeconds: 250, StorageBytesMonths: 10 * gib, EgressBytes: 5 * gib,
		Currency: "USD",
	}
	pricing := launchPricing()
	option := ProjectOption{OptionID: "opt-compressed", Pricing: pricing}

	firstInvoice, err := e.PriceUsage(usage, pricing)
	if err != nil {
		t.Fatalf("first PriceUsage: %v", err)
	}
	firstProjection, err := e.PriceServiceForOption(option)
	if err != nil {
		t.Fatalf("first PriceServiceForOption: %v", err)
	}
	for i := 0; i < 100; i++ {
		inv, err := e.PriceUsage(usage, pricing)
		if err != nil {
			t.Fatalf("PriceUsage call %d: %v", i, err)
		}
		if inv != firstInvoice {
			t.Fatalf("PriceUsage call %d non-deterministic: %+v != %+v", i, inv, firstInvoice)
		}
		proj, err := e.PriceServiceForOption(option)
		if err != nil {
			t.Fatalf("PriceServiceForOption call %d: %v", i, err)
		}
		if proj != firstProjection {
			t.Fatalf("PriceServiceForOption call %d non-deterministic: %+v != %+v", i, proj, firstProjection)
		}
	}
}

// TestMoneyIsExactInt64 asserts invoice money carries exact int64 minor units
// (no float money path) and that the token/hosting decomposition reconciles
// with the total exactly, in int64.
func TestMoneyIsExactInt64(t *testing.T) {
	e := New()
	got, err := e.PriceUsage(
		PeriodUsage{
			CustomerID: "cust-1", PeriodID: "2026-06",
			ConstructionTokens: 999_999, // raw = (999,999×800 + 500,000)/1,000,000 = 800 exact int64
			ComputeUnitSeconds: 33,      // raw hosting 33¢
			Currency:           "USD",
		},
		ServicePricing{Kind: ServicePricingFlatMarkup, MarkupPercent: 33.0, Currency: "USD"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// token: 800 × 13300/10000 = 1064 exact; hosting: 33 × 13300/10000 = 43
	// (integer trunc of 43.89) — both exact int64 paths.
	if got.TokenCharge.MinorUnits != 1064 {
		t.Fatalf("token charge = %d minor units, want exact int64 1064", got.TokenCharge.MinorUnits)
	}
	if got.HostingCharge.MinorUnits != 43 {
		t.Fatalf("hosting charge = %d minor units, want exact int64 43", got.HostingCharge.MinorUnits)
	}
	reconciled := got.TokenCharge.MinorUnits + got.HostingCharge.MinorUnits
	if reconciled != got.ServiceInvoiceAmount.MinorUnits {
		t.Fatalf("money does not reconcile exactly: %d != %d", reconciled, got.ServiceInvoiceAmount.MinorUnits)
	}
}

func assertEngineErr(t *testing.T, err error, wantKind fweng.Kind, wantDetail string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error of kind %v, got nil", wantKind)
	}
	var ee *fweng.Error
	if !errors.As(err, &ee) {
		t.Fatalf("expected *fweng.Error, got %T: %v", err, err)
	}
	if ee.Kind != wantKind {
		t.Fatalf("error kind = %v, want %v (detail %q)", ee.Kind, wantKind, ee.Detail)
	}
	if wantDetail != "" && ee.Detail != wantDetail {
		t.Fatalf("error detail = %q, want %q", ee.Detail, wantDetail)
	}
	if ee.Retryable {
		t.Fatalf("engine error must never be retryable")
	}
}
