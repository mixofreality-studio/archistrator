package estimation

import "testing"

func TestWorkerClassFor(t *testing.T) {
	cases := []struct {
		prefix, want string
	}{
		{"C", "junior-developer"},
		{"U", "junior-developer"},
		{"R", "senior-developer"},
		{"I", "senior-developer"},
		{"G", "ui-designer"},
	}
	for _, c := range cases {
		if got := workerClassFor(c.prefix); got != c.want {
			t.Errorf("workerClassFor(%q) = %q, want %q", c.prefix, got, c.want)
		}
	}
}

// The always-emit noncoding inventory has FIXED worker classes per Löwy ch. 9's three
// distinct quality roles — test engineer (builds harnesses), software tester (runs
// system testing), QA engineer (process). They must never collapse into one class.
func TestWorkerClassForNoncodingInventory(t *testing.T) {
	cases := map[string]string{
		"N-STP":   "test-engineer",
		"N-STH":   "test-engineer",
		"N-PERF":  "test-engineer",
		"N-RTH":   "senior-developer",
		"N-SMOKE": "senior-developer",
		"N-QA":    "qa-engineer",
		"N-IT":    "software-tester",
	}
	for name, want := range cases {
		if got := noncodingInventoryClass(name); got != want {
			t.Errorf("noncodingInventoryClass(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestDefaultEffortFor(t *testing.T) {
	cases := []struct {
		kind string
		want float64
	}{
		{"manager", 25},
		{"engine", 15},
		{"resourceAccess", 10},
		{"client", 25},
		{"utility", 10},
		{"resource", 10},
	}
	for _, c := range cases {
		if got := defaultEffortFor(c.kind); got != c.want {
			t.Errorf("defaultEffortFor(%q) = %v, want %v", c.kind, got, c.want)
		}
	}
}

// Every default must satisfy App C §4.4: a 5-day quantum, no god activity (>35d).
func TestDefaultEffortObeysQuantumAndCap(t *testing.T) {
	for _, kind := range []string{"manager", "engine", "resourceAccess", "client", "utility", "resource"} {
		e := defaultEffortFor(kind)
		if e <= 0 || e > 35 {
			t.Errorf("defaultEffortFor(%q) = %v, out of (0,35]", kind, e)
		}
		if int(e)%5 != 0 {
			t.Errorf("defaultEffortFor(%q) = %v, breaks the 5-day quantum", kind, e)
		}
	}
}

func TestDefaultRiskFor(t *testing.T) {
	cases := []struct {
		effort float64
		want   int64
	}{
		{5, 2}, {10, 2}, {15, 3}, {20, 3}, {25, 5}, {30, 5}, {35, 5},
	}
	for _, c := range cases {
		if got := defaultRiskFor(c.effort); got != c.want {
			t.Errorf("defaultRiskFor(%v) = %d, want %d", c.effort, got, c.want)
		}
	}
}

// Risk buckets are Fibonacci (1,2,3,5,8,13). A non-Fibonacci default would corrupt
// every downstream activity-risk roll-up.
func TestDefaultRiskIsFibonacci(t *testing.T) {
	fib := map[int64]bool{1: true, 2: true, 3: true, 5: true, 8: true, 13: true}
	for e := 5.0; e <= 35; e += 5 {
		if r := defaultRiskFor(e); !fib[r] {
			t.Errorf("defaultRiskFor(%v) = %d, not a Fibonacci bucket", e, r)
		}
	}
}

// sampleSystem is a miniature but complete System: one handwritten manager, one engine,
// one resourceAccess, one GENERATED client that carries a UI surface, one vendor
// resource, one owned resource, and one utility. It exercises every emission rule.
func sampleSystem() SystemView {
	return SystemView{
		Components: []SystemComponent{
			{ID: "order-manager", Name: "OrderManager", Kind: "manager", ConstructionProfile: "handwritten"},
			{ID: "pricing-engine", Name: "PricingEngine", Kind: "engine", ConstructionProfile: "handwritten"},
			{ID: "order-access", Name: "OrderAccess", Kind: "resourceAccess", ConstructionProfile: "handwritten"},
			{ID: "web-client", Name: "WebClient", Kind: "client", ConstructionProfile: "generated", UiSurface: true},
			{ID: "stripe", Name: "Stripe", Kind: "resource", Provisioning: "vendor"},
			{ID: "order-db", Name: "OrderDB", Kind: "resource", Provisioning: "owned"},
			{ID: "logging", Name: "Logging", Kind: "utility", ConstructionProfile: "handwritten"},
		},
		CoreUseCaseIDs: []string{"UC1", "UC2"},
	}
}

func names(acts []DerivedActivity) map[string]DerivedActivity {
	m := make(map[string]DerivedActivity, len(acts))
	for _, a := range acts {
		m[a.Name] = a
	}
	return m
}

func TestDeriveActivitiesEmitsOneCodingActivityPerHandwrittenComponent(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	for _, want := range []string{"C-order-manager", "C-pricing-engine", "C-order-access", "C-logging"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing derived coding activity %q", want)
		}
	}
	if a := got["C-order-manager"]; a.ComponentID != "order-manager" || !a.Coding || a.EffortDays != 25 {
		t.Errorf("C-order-manager = %+v, want componentID order-manager, coding, 25d", a)
	}
}

// The platform GENERATES the whole transport tier (REST handlers, typed clients, MCP
// tools, OAS) from the committed contracts. Planning work the generator does is the
// defect this rule exists to prevent — and the live committed list violates it three
// times (C-CW, C-CM, C-CS).
func TestDeriveActivitiesEmitsNoCodingActivityForGeneratedComponents(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	if _, ok := got["C-web-client"]; ok {
		t.Error("emitted a coding activity for a generated-transport client; the generator does that work")
	}
}

// R-* is one per VENDOR resource. Owned stores (the schema/deploy work rides additive
// noncoding) get none — the live list gets this wrong in both directions.
func TestDeriveActivitiesEmitsProvisioningOnlyForVendorResources(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	if _, ok := got["R-stripe"]; !ok {
		t.Error("missing R-stripe for the vendor resource")
	}
	if _, ok := got["R-order-db"]; ok {
		t.Error("emitted R-order-db for an OWNED store; its work rides additive noncoding")
	}
	if a := got["R-stripe"]; a.Coding || a.WorkerClass != "senior-developer" {
		t.Errorf("R-stripe = %+v, want noncoding senior-developer", a)
	}
}

// One U-SPA per Manager: a Client calls Managers, a use case IS a Manager, and the
// verbs-as-tools doctrine makes a manager's generated tool surface its widget set. Plus
// the always-emit scaffold, and G-SPA sequenced before the UI work.
func TestDeriveActivitiesEmitsOneSPAActivityPerManagerPlusScaffold(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	if _, ok := got["U-SPA-order-manager"]; !ok {
		t.Error("missing U-SPA-order-manager")
	}
	if _, ok := got["U-SPA-S"]; !ok {
		t.Error("missing the always-emit U-SPA-S scaffold")
	}
	if _, ok := got["G-SPA"]; !ok {
		t.Error("missing G-SPA for a system with a UI surface")
	}
	if a := got["G-SPA"]; a.WorkerClass != "ui-designer" {
		t.Errorf("G-SPA worker class = %q, want ui-designer", a.WorkerClass)
	}
}

// uiSurface is a SEPARATE axis from constructionProfile: web-client is generated
// (no C-*) AND carries a UI surface (SPA work is real). Collapsing them loses the SPA.
func TestDeriveActivitiesNoSPAWorkWithoutAUISurface(t *testing.T) {
	sys := sampleSystem()
	for i := range sys.Components {
		sys.Components[i].UiSurface = false
	}
	got := names(deriveActivities(sys))
	for _, unwanted := range []string{"U-SPA-S", "G-SPA", "U-SPA-order-manager"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("emitted %q for a system with no UI surface", unwanted)
		}
	}
}

func TestDeriveActivitiesEmitsOneIntegrationActivityPerCoreUseCase(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	for _, want := range []string{"I-UC1", "I-UC2"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %q", want)
		}
	}
}

func TestDeriveActivitiesAlwaysEmitsTheTestingInventory(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	for _, want := range []string{"N-STP", "N-STH", "N-RTH", "N-SMOKE", "N-QA", "N-PERF", "N-IT"} {
		a, ok := got[want]
		if !ok {
			t.Errorf("missing always-emit %q", want)
			continue
		}
		if a.WorkerClass != noncodingInventoryClass(want) {
			t.Errorf("%s worker class = %q, want %q", want, a.WorkerClass, noncodingInventoryClass(want))
		}
	}
}

// Purity: identical input must give a byte-identical, stably ordered result. Map
// iteration order leaking into the output would make every downstream CPM solve
// nondeterministic.
func TestDeriveActivitiesIsDeterministicAndSorted(t *testing.T) {
	first := deriveActivities(sampleSystem())
	for i := 0; i < 20; i++ {
		next := deriveActivities(sampleSystem())
		if len(next) != len(first) {
			t.Fatalf("length varies across runs: %d vs %d", len(next), len(first))
		}
		for j := range first {
			if next[j].Name != first[j].Name {
				t.Fatalf("order varies across runs at %d: %q vs %q", j, next[j].Name, first[j].Name)
			}
		}
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].Name >= first[i].Name {
			t.Fatalf("not sorted ascending by name at %d: %q then %q", i, first[i-1].Name, first[i].Name)
		}
	}
}

// Every derived activity must be plan-legal on its face (App C 4.4 + the fixed roster).
func TestDeriveActivitiesAllObeyTheEstimationRules(t *testing.T) {
	roster := map[string]bool{
		"system-architect": true, "product-manager": true, "project-manager": true,
		"senior-developer": true, "junior-developer": true, "ui-designer": true,
		"ux-reviewer": true, "qa-engineer": true, "test-engineer": true, "software-tester": true,
	}
	fib := map[int64]bool{1: true, 2: true, 3: true, 5: true, 8: true, 13: true}
	for _, a := range deriveActivities(sampleSystem()) {
		if int(a.EffortDays)%5 != 0 || a.EffortDays <= 0 || a.EffortDays > 35 {
			t.Errorf("%s effort %v breaks the quantum/cap rule", a.Name, a.EffortDays)
		}
		if !roster[a.WorkerClass] {
			t.Errorf("%s worker class %q is not on the fixed roster", a.Name, a.WorkerClass)
		}
		if !fib[a.RiskBucket] {
			t.Errorf("%s risk bucket %d is not Fibonacci", a.Name, a.RiskBucket)
		}
		if !a.Derived {
			t.Errorf("%s is not flagged Derived", a.Name)
		}
	}
}
