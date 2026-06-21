package estimation

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// diamond builds a small diamond network for the CPM tests: A(5) → B(5),C(15) → D(5).
// Longest path A→C→D = 25 days; B carries 10 days of total float.
func diamond() (projectstate.ActivityList, projectstate.Network) {
	al := projectstate.ActivityList{Activities: []projectstate.ActivityItem{
		{Name: "A", EffortDays: 5, WorkerClass: "dev"},
		{Name: "B", EffortDays: 5, WorkerClass: "dev"},
		{Name: "C", EffortDays: 15, WorkerClass: "dev"},
		{Name: "D", EffortDays: 5, WorkerClass: "dev"},
	}}
	net := projectstate.Network{
		Dependencies: []projectstate.NetworkDependency{
			{Activity: "B", DependsOn: []string{"A"}},
			{Activity: "C", DependsOn: []string{"A"}},
			{Activity: "D", DependsOn: []string{"B", "C"}},
		},
		CriticalPath: []string{"A", "C", "D"},
	}
	return al, net
}

func TestComputeNetwork_ForwardBackwardPass(t *testing.T) {
	al, net := diamond()
	sol, err := New().ComputeNetwork(al, net)
	if err != nil {
		t.Fatalf("ComputeNetwork: %v", err)
	}

	if sol.Summary.TotalDurationDays != 25 {
		t.Fatalf("project duration = %v, want 25", sol.Summary.TotalDurationDays)
	}
	if sol.Summary.CriticalPathDays != 25 {
		t.Fatalf("CP days = %v, want 25", sol.Summary.CriticalPathDays)
	}

	a := sol.Nodes["A"]
	if a.EarliestStart != 0 || a.EarliestFinish != 5 || a.TotalFloat != 0 || !a.OnCriticalPath {
		t.Fatalf("A: %+v", a)
	}
	c := sol.Nodes["C"]
	if c.EarliestStart != 5 || c.EarliestFinish != 20 || c.TotalFloat != 0 || !c.OnCriticalPath {
		t.Fatalf("C: %+v", c)
	}
	b := sol.Nodes["B"]
	// B: ES 5, EF 10, latest start = 20 (so D can start at 20), float = 10. Off-CP.
	if b.EarliestStart != 5 || b.EarliestFinish != 10 || b.TotalFloat != 10 || b.OnCriticalPath {
		t.Fatalf("B: %+v", b)
	}
	d := sol.Nodes["D"]
	if d.EarliestStart != 20 || d.EarliestFinish != 25 || !d.OnCriticalPath {
		t.Fatalf("D: %+v", d)
	}
}

func TestComputeNetwork_BandClassification(t *testing.T) {
	al, net := diamond()
	sol, _ := New().ComputeNetwork(al, net)

	// On-CP nodes are critical.
	if sol.Nodes["A"].Band != BandCritical || sol.Nodes["C"].Band != BandCritical {
		t.Fatalf("on-CP nodes not critical: A=%s C=%s", sol.Nodes["A"].Band, sol.Nodes["C"].Band)
	}
	// B has 10 days float: > red (5) and ≤ yellow (25) ⇒ yellow.
	if sol.Nodes["B"].Band != BandYellow {
		t.Fatalf("B band = %s, want yellow (float 10)", sol.Nodes["B"].Band)
	}
}

func TestComputeNetwork_BandPolicyThresholdsTunable(t *testing.T) {
	// The band thresholds are a Strategy on the policy. Verify the boundaries directly.
	p := BandPolicy{RedMaxDays: 5, YellowMaxDays: 25}
	cases := []struct {
		onCP  bool
		float float64
		want  string
	}{
		{true, 0, BandCritical},
		{true, 100, BandCritical}, // on-CP always critical regardless of float
		{false, 0, BandRed},
		{false, 5, BandRed},
		{false, 6, BandYellow},
		{false, 25, BandYellow},
		{false, 26, BandGreen},
	}
	for _, c := range cases {
		if got := p.classify(c.onCP, c.float); got != c.want {
			t.Errorf("classify(onCP=%v, float=%v) = %s, want %s", c.onCP, c.float, got, c.want)
		}
	}
	if !p.nearCritical(false, 5) || p.nearCritical(false, 6) || p.nearCritical(true, 0) {
		t.Fatal("nearCritical boundary wrong")
	}
}

func TestComputeNetwork_MilestoneEventTimeAndRiskExclusion(t *testing.T) {
	al, net := diamond()
	// A milestone fanning in on D (max predecessor EF = 25) and one fanning in on B (10).
	net.Milestones = []projectstate.NetworkMilestone{
		{ID: "M-END", Name: "End", Public: true, DependsOn: []string{"D"}},
		{ID: "M-MID", Name: "Mid", Public: false, DependsOn: []string{"B"}},
		{ID: "M-START", Name: "Start", Public: true, DependsOn: nil},
	}
	sol, err := New().ComputeNetwork(al, net)
	if err != nil {
		t.Fatalf("ComputeNetwork: %v", err)
	}
	if len(sol.Milestones) != 3 {
		t.Fatalf("milestones = %d, want 3", len(sol.Milestones))
	}
	byID := map[string]NetworkMilestoneSolution{}
	for _, m := range sol.Milestones {
		byID[m.ID] = m
	}
	// DETERMINING-PREDECESSOR on-CP rule: M-END's determining pred is D (EF 25, on-CP) ⇒
	// M-END on-CP. M-MID's determining pred is B (EF 10, off-CP) ⇒ M-MID off-CP.
	if byID["M-END"].EventTime != 25 || !byID["M-END"].OnCriticalPath {
		t.Fatalf("M-END: %+v (want eventTime 25, on-CP via determining pred D)", byID["M-END"])
	}
	if byID["M-MID"].EventTime != 10 || byID["M-MID"].OnCriticalPath {
		t.Fatalf("M-MID: %+v (want eventTime 10, off-CP via determining pred B)", byID["M-MID"])
	}
	// M-START has NO predecessors: the ROOT convention puts the project-start gate on-CP,
	// eventTime 0 (it marks the project origin).
	if byID["M-START"].EventTime != 0 || !byID["M-START"].OnCriticalPath {
		t.Fatalf("M-START: %+v (want eventTime 0, on-CP via root convention)", byID["M-START"])
	}
	// Milestones are NOT activity nodes (excluded from the node set + the CP count).
	if _, isNode := sol.Nodes["M-END"]; isNode {
		t.Fatal("milestone leaked into activity node set")
	}
	if sol.Summary.CriticalPathActivityCount != 3 {
		t.Fatalf("CP activity count = %d, want 3 (milestones excluded)", sol.Summary.CriticalPathActivityCount)
	}
}

// TestComputeNetwork_MilestoneChaining verifies a milestone may dependOn another
// milestone (the N-DOGFOOD → M5 shape): both are zero-duration nodes in the unified CPM
// graph, so the chained milestone's eventTime follows its predecessor milestone, and its
// on-CP follows its determining (milestone) predecessor — regardless of authored order.
func TestComputeNetwork_MilestoneChaining(t *testing.T) {
	al, net := diamond()
	// N-LATE depends on M-END (a milestone). Authored BEFORE M-END to prove order-
	// independence (the dependency-order milestone pass resolves the chain).
	net.Milestones = []projectstate.NetworkMilestone{
		{ID: "N-LATE", Name: "Late", Public: false, DependsOn: []string{"M-END"}},
		{ID: "M-END", Name: "End", Public: true, DependsOn: []string{"D"}},
	}
	sol, err := New().ComputeNetwork(al, net)
	if err != nil {
		t.Fatalf("ComputeNetwork: %v", err)
	}
	byID := map[string]NetworkMilestoneSolution{}
	for _, m := range sol.Milestones {
		byID[m.ID] = m
	}
	// M-END: eventTime 25 (= D.EF), determining pred D on-CP ⇒ on-CP.
	if byID["M-END"].EventTime != 25 || !byID["M-END"].OnCriticalPath {
		t.Fatalf("M-END: %+v", byID["M-END"])
	}
	// N-LATE chains off M-END (eventTime 25). Its determining pred is M-END, a milestone
	// at the project frontier (25 == projectDuration) ⇒ POST-TERMINAL override forces it
	// OFF-CP (the N-DOGFOOD → M5 shape: a post-frontier marker chained off a milestone).
	if byID["N-LATE"].EventTime != 25 || byID["N-LATE"].OnCriticalPath {
		t.Fatalf("N-LATE: %+v (want eventTime 25, off-CP via post-terminal override)", byID["N-LATE"])
	}
	// Returned slice preserves authored order (N-LATE first).
	if sol.Milestones[0].ID != "N-LATE" || sol.Milestones[1].ID != "M-END" {
		t.Fatalf("authored order not preserved: %v", []string{sol.Milestones[0].ID, sol.Milestones[1].ID})
	}
}

// TestComputeNetwork_MilestoneDeterminingPredOffCP verifies the determining-predecessor
// rule: M-X fans in on {A (on-CP, EF 5), B (off-CP, EF 10)} ⇒ determining pred is B (the
// max-EF node, EF 10 = eventTime) which is OFF-CP ⇒ M-X off-CP. (A is on-CP but is NOT
// the determining predecessor — it finishes earlier, so it does not gate the event.)
func TestComputeNetwork_MilestoneDeterminingPredOffCP(t *testing.T) {
	al, net := diamond()
	net.Milestones = []projectstate.NetworkMilestone{
		{ID: "M-X", Name: "X", Public: false, DependsOn: []string{"A", "B"}},
	}
	sol, _ := New().ComputeNetwork(al, net)
	m := sol.Milestones[0]
	if m.EventTime != 10 {
		t.Fatalf("M-X eventTime = %v, want 10 (determining pred B)", m.EventTime)
	}
	if m.OnCriticalPath {
		t.Fatal("M-X should be OFF-CP: determining pred B is off-CP (A is on-CP but not determining)")
	}
}

// TestComputeNetwork_StartGateMilestone verifies the ROOT convention: a no-predecessor
// start-gate milestone (M0 "SDP Review Approved") is on-CP at eventTime 0 — it marks the
// project origin. No fan-out edge is required under the determining-predecessor rule.
func TestComputeNetwork_StartGateMilestone(t *testing.T) {
	al, net := diamond()
	net.Milestones = []projectstate.NetworkMilestone{
		{ID: "M0", Name: "SDP Review Approved", Public: true, DependsOn: nil},
	}
	sol, _ := New().ComputeNetwork(al, net)
	m := sol.Milestones[0]
	if m.EventTime != 0 || !m.OnCriticalPath {
		t.Fatalf("M0 start gate: %+v (want eventTime 0, on-CP via root convention)", m)
	}
}

// TestComputeNetwork_PostTerminalMilestoneOffCP verifies the POST-TERMINAL override that
// distinguishes the terminal release milestone (stays on-CP) from a post-v1 marker chained
// off it (forced off-CP) — the M5 vs N-DOGFOOD distinction. M-REL's determining pred is an
// ACTIVITY at the frontier ⇒ on-CP; M-POST chains off M-REL (a milestone at the frontier)
// ⇒ post-terminal ⇒ off-CP, even though its determining pred M-REL is on-CP.
func TestComputeNetwork_PostTerminalMilestoneOffCP(t *testing.T) {
	al, net := diamond()
	net.Milestones = []projectstate.NetworkMilestone{
		{ID: "M-REL", Name: "v1 Release", Public: true, DependsOn: []string{"D"}},    // det pred D (activity, on-CP, EF 25 = duration)
		{ID: "M-POST", Name: "Post-v1", Public: false, DependsOn: []string{"M-REL"}}, // chained off the release milestone
	}
	sol, _ := New().ComputeNetwork(al, net)
	byID := map[string]NetworkMilestoneSolution{}
	for _, m := range sol.Milestones {
		byID[m.ID] = m
	}
	if !byID["M-REL"].OnCriticalPath {
		t.Fatalf("M-REL should be ON-CP (terminal release, determining pred is an on-CP activity): %+v", byID["M-REL"])
	}
	if byID["M-POST"].EventTime != 25 || byID["M-POST"].OnCriticalPath {
		t.Fatalf("M-POST should be OFF-CP (post-terminal, chained off milestone M-REL): %+v", byID["M-POST"])
	}
}

func TestComputeNetwork_EmptyNetworkIsEmptyResultNotError(t *testing.T) {
	sol, err := New().ComputeNetwork(projectstate.ActivityList{}, projectstate.Network{})
	if err != nil {
		t.Fatalf("empty network should not error: %v", err)
	}
	if len(sol.Nodes) != 0 || sol.Summary.TotalDurationDays != 0 {
		t.Fatalf("empty network not empty: %+v", sol)
	}
}

func TestComputeNetwork_Deterministic(t *testing.T) {
	al, net := diamond()
	a, _ := New().ComputeNetwork(al, net)
	b, _ := New().ComputeNetwork(al, net)
	if a.Summary != b.Summary {
		t.Fatal("summary not deterministic")
	}
	for id, n := range a.Nodes {
		if n != b.Nodes[id] {
			t.Fatalf("node %s not deterministic: %+v vs %+v", id, n, b.Nodes[id])
		}
	}
}

func TestComputeNetwork_SummaryRollups(t *testing.T) {
	al, net := diamond()
	sol, _ := New().ComputeNetwork(al, net)
	// 3 on-CP (A,C,D), max float 10 (B), 0 near-critical (B's 10 > red threshold 5).
	if sol.Summary.CriticalPathActivityCount != 3 {
		t.Fatalf("CP count = %d, want 3", sol.Summary.CriticalPathActivityCount)
	}
	if sol.Summary.MaxFloat != 10 {
		t.Fatalf("max float = %v, want 10", sol.Summary.MaxFloat)
	}
	if sol.Summary.NearCriticalCount != 0 {
		t.Fatalf("near-critical = %d, want 0", sol.Summary.NearCriticalCount)
	}
}
