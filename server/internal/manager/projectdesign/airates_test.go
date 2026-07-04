package projectdesign

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// TestDeriveClassRates_FromModelTier checks the AI $/day derivation (F11b) against the
// hand-computed price list × the default throughput (2 MTok in / 0.5 MTok out per day):
//
//	fable  = 2×$10 + 0.5×$50 = $45.00 → 4500¢
//	opus   = 2×$5  + 0.5×$25 = $22.50 → 2250¢
//	sonnet = 2×$3  + 0.5×$15 = $13.50 → 1350¢
func TestDeriveClassRates_FromModelTier(t *testing.T) {
	var pa projectstate.PlanningAssumptions // empty rate card ⇒ documented defaults
	rates := deriveClassRates(pa, []string{"system-architect", "senior-developer", "junior-developer"})

	want := map[string]int64{
		"system-architect": 4500, // fable
		"senior-developer": 2250, // opus
		"junior-developer": 1350, // sonnet
	}
	for class, cents := range want {
		got := rates[class]
		if got.Currency != "USD" || got.MinorUnits != cents {
			t.Errorf("%s rate = %+v, want %d¢ USD", class, got, cents)
		}
	}
}

// TestDeriveClassRates_UnknownClassDefaultsSonnet: a stale/unknown class (e.g. the
// phantom "architect") must still resolve so the option assembles — it falls back to the
// sonnet tier (F11d: phantom classes map to no agent).
func TestDeriveClassRates_UnknownClassDefaultsSonnet(t *testing.T) {
	rates := deriveClassRates(projectstate.PlanningAssumptions{}, []string{"architect"})
	if got := rates["architect"]; got.MinorUnits != 1350 || got.Currency != "USD" {
		t.Errorf("unknown class rate = %+v, want 1350¢ USD (sonnet fallback)", got)
	}
}

// TestDeriveClassRates_AuthoredCardOverridesDefault: an authored RateCard entry wins over
// the default (F11a), including a higher throughput or a top-tier model.
func TestDeriveClassRates_AuthoredCardOverridesDefault(t *testing.T) {
	pa := projectstate.PlanningAssumptions{
		RateCard: map[string]projectstate.WorkerRateSpec{
			"junior-developer": {ModelID: "opus", MegatokensInPerDay: 2, MegatokensOutPerDay: 0.5},
		},
	}
	rates := deriveClassRates(pa, []string{"junior-developer"})
	if got := rates["junior-developer"]; got.MinorUnits != 2250 {
		t.Errorf("authored opus rate = %+v, want 2250¢ (opus, not sonnet default)", got)
	}
}

// TestIndirectDailyRate_DefaultWhenUnset covers the F6 fallback.
func TestIndirectDailyRate_DefaultWhenUnset(t *testing.T) {
	if got := indirectDailyRateOf(projectstate.PlanningAssumptions{}); got != defaultIndirectDailyRate {
		t.Errorf("unset indirect rate = %+v, want default %+v", got, defaultIndirectDailyRate)
	}
	authored := projectstate.Money{MinorUnits: 999, Currency: "USD"}
	if got := indirectDailyRateOf(projectstate.PlanningAssumptions{IndirectDailyRate: authored}); got != authored {
		t.Errorf("authored indirect rate = %+v, want %+v", got, authored)
	}
}
