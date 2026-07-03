// cmd/replan-sdp is a ONE-OFF developer tool: recompute the Phase-2 risk model
// and SDP review from the (regenerated) activity list + network + solution
// params using the REAL constructionEstimationEngine, so their numbers reflect
// the current one-activity-per-component network. It reads project.json, computes
// per-option ConstructionEstimate via estimation.EstimateForOption (the same call
// the projectDesign workflow makes — assembleOption/toEstimationOption inlined),
// and writes the recomputed risk+SDP to an output JSON the python splicer applies.
// Not wired into the server; run manually. See the 2026-07-01 activity re-plan.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
)

type money struct {
	MinorUnits int64  `json:"minorUnits"`
	Currency   string `json:"currency"`
}
type activity struct {
	Name        string  `json:"name"`
	EffortDays  float64 `json:"effortDays"`
	WorkerClass string  `json:"workerClass"`
	RiskBucket  int64   `json:"riskBucket"`
}
type solModel struct {
	SlotKind            string           `json:"slotKind"`
	StaffingCap         int64            `json:"staffingCap"`
	CalendarDaysPerWeek float64          `json:"calendarDaysPerWeek"`
	ClassRates          map[string]money `json:"classRates"`
}
type sdpOption struct {
	OptionID             string          `json:"optionId"`
	SolutionKind         string          `json:"solutionKind"`
	DurationDays         float64         `json:"durationDays"`
	BuildCost            money           `json:"buildCost"`
	CompositeRisk        float64         `json:"compositeRisk"`
	ProjectedMonthlyCost json.RawMessage `json:"projectedMonthlyCost"`
	ExpectedPerCycleNet  json.RawMessage `json:"expectedPerCycleNet"`
	RevenueSharePercent  json.RawMessage `json:"revenueSharePercent"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: replan-sdp <project.json path>")
		os.Exit(1)
	}
	// os.Args[1] is an operator-supplied CLI path (this is a manual, one-off
	// developer tool — see the package doc). filepath.Clean normalizes it
	// (collapses ".." segments, no symlink resolution needed since this reads
	// a plain file the operator names on their own machine).
	inPath := filepath.Clean(os.Args[1])
	raw, err := os.ReadFile(inPath)
	if err != nil {
		panic(err)
	}
	var doc map[string]json.RawMessage
	must(json.Unmarshal(raw, &doc))
	var slots map[string]struct {
		Kind  int             `json:"kind"`
		Model json.RawMessage `json:"model"`
	}
	must(json.Unmarshal(doc["slots"], &slots))

	// activities (slot 9) + criticalPath (slot 10)
	var al struct {
		Activities []activity `json:"activities"`
	}
	must(json.Unmarshal(slots["9"].Model, &al))
	var nw struct {
		CriticalPath []string `json:"criticalPath"`
	}
	must(json.Unmarshal(slots["10"].Model, &nw))
	onCP := map[string]bool{}
	for _, n := range nw.CriticalPath {
		onCP[n] = true
	}

	// solution params by slotKind (slots 11-14)
	sols := map[string]solModel{}
	for _, k := range []string{"11", "12", "13", "14"} {
		var s solModel
		must(json.Unmarshal(slots[k].Model, &s))
		sols[s.SlotKind] = s
	}
	// existing SDP options (keep usage/settlement facets + optionId per solutionKind)
	var sdp struct {
		Options []sdpOption `json:"options"`
	}
	must(json.Unmarshal(slots["16"].Model, &sdp))
	existing := map[string]sdpOption{}
	for _, o := range sdp.Options {
		existing[o.SolutionKind] = o
	}

	eng := estimation.NewEstimationEngine()
	ctx := fweng.Context{Context: context.Background()}

	order := []string{"normalSolution", "decompressedSolution", "subcriticalSolution", "compressedSolution"}
	var riskRows []map[string]interface{}
	var sdpOptions []map[string]interface{}
	for _, kind := range order {
		sol := sols[kind]
		acts := make([]estimation.OptionActivity, 0, len(al.Activities))
		for _, a := range al.Activities {
			acts = append(acts, estimation.OptionActivity{
				ActivityId: a.Name, EffortDays: a.EffortDays, WorkerClass: a.WorkerClass,
				OnCriticalPath: onCP[a.Name], RiskBucket: a.RiskBucket,
			})
		}
		rates := map[string]estimation.Money{}
		for cls, m := range sol.ClassRates {
			rates[cls] = estimation.Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
		}
		opt := estimation.ProjectOption{
			OptionId:            estimation.OptionID(kind),
			Network:             estimation.ActivityNetwork{Activities: acts},
			WorkerMix:           estimation.WorkerMix{ClassRates: rates, StaffingCap: sol.StaffingCap},
			CalendarDaysPerWeek: sol.CalendarDaysPerWeek,
		}
		ce, err := eng.EstimateForOption(ctx, opt)
		if err != nil {
			panic(fmt.Errorf("%s: %w", kind, err))
		}
		riskRows = append(riskRows, map[string]interface{}{
			"solutionKind": kind, "criticalityRisk": r3(ce.Risk.CriticalityRisk),
			"activityRisk": r3(ce.Risk.ActivityRisk), "composite": r3(ce.Risk.Composite),
		})
		ex := existing[kind]
		o := map[string]interface{}{
			"optionId": ex.OptionID, "solutionKind": kind,
			"durationDays":         ce.DurationDays,
			"buildCost":            map[string]interface{}{"minorUnits": ce.BuildCost.MinorUnits, "currency": ce.BuildCost.Currency},
			"compositeRisk":        r3(ce.Risk.Composite),
			"projectedMonthlyCost": ex.ProjectedMonthlyCost, "expectedPerCycleNet": ex.ExpectedPerCycleNet,
			"revenueSharePercent": ex.RevenueSharePercent,
		}
		sdpOptions = append(sdpOptions, o)
		fmt.Fprintf(os.Stderr, "%-22s dur=%.0fd cost=%d risk(c=%.3f a=%.3f comp=%.3f)\n",
			kind, ce.DurationDays, ce.BuildCost.MinorUnits, ce.Risk.CriticalityRisk, ce.Risk.ActivityRisk, ce.Risk.Composite)
	}

	out := map[string]interface{}{
		"riskModel":  map[string]interface{}{"rows": riskRows},
		"sdpOptions": sdpOptions,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	must(enc.Encode(out))
}

func r3(f float64) float64 { return float64(int64(f*1000+0.5)) / 1000 }
func must(err error) {
	if err != nil {
		panic(err)
	}
}
