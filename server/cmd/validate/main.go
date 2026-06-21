package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func main() {
	file := flag.String("file", ".aiarch/state/project.json", "path to project.json")
	id := flag.String("id", "archistrator", "project id")
	flag.Parse()

	raw, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", *file, err)
		os.Exit(1)
	}

	proj, ok, err := projectstate.DecodeProjectJSON(raw, projectstate.ProjectID(*id))
	if err != nil {
		fmt.Fprintf(os.Stderr, "DECODE FAILED: %v\n", err)
		os.Exit(1)
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "DECODE FAILED: no project document")
		os.Exit(1)
	}

	// Round-trip stability: encode the decoded aggregate and decode again.
	enc, err := projectstate.EncodeProjectJSON(proj)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ENCODE FAILED: %v\n", err)
		os.Exit(1)
	}
	if _, ok2, err2 := projectstate.DecodeProjectJSON(enc, projectstate.ProjectID(*id)); err2 != nil || !ok2 {
		fmt.Fprintf(os.Stderr, "ROUND-TRIP FAILED: ok=%v err=%v\n", ok2, err2)
		os.Exit(1)
	}

	fmt.Printf("OK  id=%s version=%d phase=%d owner=%s name=%q\n", proj.ID, proj.Version, proj.Phase, proj.Owner, proj.Name)
	committed := committedSlots(proj)
	sort.Ints(committed)
	fmt.Printf("committed slots: %v\n", committed)
}

// committedSlots returns the kind ordinals whose slot is ReviewCommitted (status 2).
func committedSlots(p projectstate.Project) []int {
	var out []int
	add := func(ord int, s projectstate.ArtifactSlot) {
		if s.Status == projectstate.ReviewCommitted {
			out = append(out, ord)
		}
	}
	add(0, p.Mission)
	add(1, p.Glossary)
	add(2, p.ScrubbedRequirements)
	add(3, p.Volatilities)
	add(4, p.CoreUseCases)
	add(5, p.SystemDesign)
	add(6, p.OperationalConcepts)
	add(7, p.StandardCheck)
	add(8, p.PlanningAssumptions)
	add(9, p.ActivityList)
	add(10, p.Network)
	add(11, p.NormalSolution)
	add(12, p.SubcriticalSolution)
	add(13, p.CompressedSolution)
	add(14, p.DecompressedSolution)
	add(15, p.RiskModel)
	add(16, p.SdpReview)
	return out
}
