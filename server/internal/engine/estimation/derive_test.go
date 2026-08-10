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
