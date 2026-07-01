package projectstate

import "testing"

func TestDeriveKind(t *testing.T) {
	cases := map[string]struct {
		id, component string
		want          ActivityKind
	}{
		"manager": {"C-MST", "settlementManager", ActivityKindService},
		"engine":  {"C-BE", "billingEngine", ActivityKindService},
		"access":  {"C-PA", "projectStateAccess", ActivityKindService},
		"client":  {"C-CW", "webClient", ActivityKindService},
		"spa":     {"U-SPA", "", ActivityKindFrontend},
		"ci":      {"N-CI", "", ActivityKindTesting},
	}
	for name, c := range cases {
		if got := DeriveKind(c.id, c.component); got != c.want {
			t.Errorf("%s: DeriveKind(%q,%q)=%v want %v", name, c.id, c.component, got, c.want)
		}
	}
}

func TestDeriveBuildStatus(t *testing.T) {
	if s, integ := DeriveBuildStatus(CorpusPresence{HasLog: true, HasPassingReview: true}); s != BuildIntegrated || !integ {
		t.Errorf("log+review should be integrated")
	}
	if s, integ := DeriveBuildStatus(CorpusPresence{HasLog: true}); s != BuildInReview || integ {
		t.Errorf("log-only should be in-review, not integrated")
	}
	if s, _ := DeriveBuildStatus(CorpusPresence{}); s != BuildInConstruction {
		t.Errorf("no corpus should default in-construction")
	}
}

func TestDeriveProduced(t *testing.T) {
	got := DeriveProduced(CorpusPresence{HasLog: true, HasContract: true, ContractFile: "implementation/contracts/webClient.md"}, "webClient")
	if len(got) != 2 {
		t.Fatalf("want 2 artifacts (contract+code) got %d", len(got))
	}
	if got[0].Kind != "service-contract" || got[0].Source != "implementation/contracts/webClient.md" || !got[0].Produced {
		t.Errorf("contract artifact wrong: %+v", got[0])
	}
	if got[1].Kind != "code" || !got[1].Produced {
		t.Errorf("code artifact wrong: %+v", got[1])
	}
}

func TestDeriveType_Prefixes(t *testing.T) {
	cases := map[string]ActivityType{
		"U-SPA-Home": ActivityTypeFrontend,
		"N-STP":      ActivityTypeTesting,
		"N-IT":       ActivityTypeTesting,
		"C-Orders":   ActivityTypeService,
		"E-Pricing":  ActivityTypeService,
	}
	for id, want := range cases {
		if got := DeriveType(id); got != want {
			t.Errorf("DeriveType(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestDeriveVariant_TestingPrefixes(t *testing.T) {
	cases := map[string]TestingVariant{
		"N-STP":   TestVariantPlan,
		"N-STH":   TestVariantHarness,
		"N-PERF":  TestVariantPerf,
		"N-IT":    TestVariantSystemTest,
		"N-QA":    TestVariantQAProcess,
		"N-OTHER": TestVariantPlan, // unknown N- falls back to Plan
	}
	for id, want := range cases {
		if got := DeriveVariant(id); got != want {
			t.Errorf("DeriveVariant(%q) = %v, want %v", id, got, want)
		}
	}
}
