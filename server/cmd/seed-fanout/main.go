// Command seed-fanout applies the architect's milestone fan-OUT edge deltas (task #13,
// 2026-06-19) to the live project.json network slot so the seed matches network.yaml's
// materialized fan-out collapse. Each delta REPLACES a collapsed predecessor subset with
// the milestone id on a specific downstream consumer (keeping that node's other deps).
//
// It is a STRICT, AUDITABLE rewrite: for every target node it asserts the current
// dependsOn equals the expected BEFORE set (order-insensitive) and fails loudly on any
// mismatch — it never blindly overwrites. It touches ONLY the 10 named nodes; all other
// dependencies (and the milestones[] array, computed at read) are left untouched. The
// collapse is a verified no-op on floats/CP/duration; this tool just makes the stored
// graph match the authored one. It does NOT commit.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// fanoutDelta is one downstream consumer's BEFORE→AFTER dependsOn rewrite.
type fanoutDelta struct {
	node   string
	before []string
	after  []string
}

// deltas are the architect's exact per-node REPLACE operations (verbatim).
var deltas = []fanoutDelta{
	// M2 "Managers Complete" fan-in {C-MSD,C-MPD,C-MCN,C-MOP,C-BM} collapsed on 4 consumers.
	{"I-DE",
		[]string{"C-DA", "C-MSD", "C-MPD", "C-MCN", "C-MOP", "C-BM"},
		[]string{"C-DA", "M2"}},
	{"I-CL",
		[]string{"C-CW", "C-CM", "C-CS", "C-MSD", "C-MPD", "C-MCN", "C-MOP", "C-BM"},
		[]string{"C-CW", "C-CM", "C-CS", "M2"}},
	{"I-UTIL",
		[]string{"C-SE", "C-LG", "C-DG", "C-MSD", "C-MPD", "C-MCN", "C-MOP", "C-BM", "C-CW", "C-CM", "C-CS"},
		[]string{"C-SE", "C-LG", "C-DG", "M2", "C-CW", "C-CM", "C-CS"}},
	{"I-RA",
		[]string{"C-MSD", "C-MPD", "C-MCN", "C-MOP", "C-BM", "C-PA", "C-PA-R", "C-PA-C2", "C-CP", "C-CP-R", "C-OR", "C-UA", "C-BG", "C-SC", "C-DA"},
		[]string{"M2", "C-PA", "C-PA-R", "C-PA-C2", "C-CP", "C-CP-R", "C-OR", "C-UA", "C-BG", "C-SC", "C-DA"}},
	// M3 "Backend Wired" fan-in {I-RA,I-DE} collapsed on all 5 UCs.
	{"I-UC1",
		[]string{"I-CL", "I-DE", "I-RA", "U-SPA-1", "I-GIT-DESIGN"},
		[]string{"I-CL", "M3", "U-SPA-1", "I-GIT-DESIGN"}},
	{"I-UC2",
		[]string{"I-CL", "I-DE", "I-RA", "U-SPA-2", "I-UC1"},
		[]string{"I-CL", "M3", "U-SPA-2", "I-UC1"}},
	{"I-UC3",
		[]string{"I-CL", "I-DE", "I-RA", "U-SPA-3", "I-UC2"},
		[]string{"I-CL", "M3", "U-SPA-3", "I-UC2"}},
	{"I-UC4",
		[]string{"I-CL", "I-DE", "I-RA", "U-SPA-4"},
		[]string{"I-CL", "M3", "U-SPA-4"}},
	{"I-UC5",
		[]string{"I-CL", "I-DE", "I-RA", "U-SPA-5", "C-BM", "C-BE", "C-BG", "C-UA"},
		[]string{"I-CL", "M3", "U-SPA-5", "C-BM", "C-BE", "C-BG", "C-UA"}},
	// M4 "Use Cases Demonstrable" fan-in {I-UC1..5} collapsed on N-IT.
	{"N-IT",
		[]string{"N-STH", "N-RTH", "I-UC1", "I-UC2", "I-UC3", "I-UC4", "I-UC5"},
		[]string{"N-STH", "N-RTH", "M4"}},
}

func main() {
	file := flag.String("file", "/Users/davidmarne/mixofrealitystudio/archistrator/.aiarch/state/project.json", "path to project.json to rewrite")
	id := flag.String("id", "archistrator", "project id")
	flag.Parse()

	raw, err := os.ReadFile(*file)
	must(err, "read project.json")
	proj, ok, err := projectstate.DecodeProjectJSON(raw, projectstate.ProjectID(*id))
	must(err, "decode project.json")
	if !ok {
		fail("no project document")
	}
	net, ok := proj.Network.Model.(*projectstate.Network)
	if !ok || net == nil {
		fail("no network slot model")
	}

	byNode := map[string]int{}
	for i, d := range net.Dependencies {
		byNode[d.Activity] = i
	}

	applied := 0
	for _, delta := range deltas {
		idx, found := byNode[delta.node]
		if !found {
			fail(fmt.Sprintf("target node %s not present in dependencies", delta.node))
		}
		cur := net.Dependencies[idx].DependsOn
		if !sameSet(cur, delta.before) {
			fail(fmt.Sprintf("node %s: current dependsOn does not match expected BEFORE\n  current : %v\n  expected: %v", delta.node, cur, delta.before))
		}
		net.Dependencies[idx].DependsOn = append([]string(nil), delta.after...)
		applied++
		fmt.Printf("  %-8s %v -> %v\n", delta.node, delta.before, delta.after)
	}

	enc, err := projectstate.EncodeProjectJSON(proj)
	must(err, "encode project.json")
	must(os.WriteFile(*file, enc, 0o644), "write project.json")
	fmt.Printf("applied %d fan-out deltas to %s\n", applied, *id)
}

// sameSet reports whether a and b contain the same elements (order-insensitive).
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func must(err error, ctx string) {
	if err != nil {
		fail(ctx + ": " + err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "seed-fanout:", msg)
	os.Exit(1)
}
