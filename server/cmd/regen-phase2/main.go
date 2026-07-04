// Command regen-phase2 re-authors the Phase-2 option/risk/SDP slots in
// .aiarch/state/project.json using the book-faithful estimationEngine (float-based risk,
// per-option resource-leveled networks, indirect cost, AI-agent rate card). One-shot
// state-regeneration tool (not part of the steady-state server). It reuses the REAL
// estimation engine for all math; only the tiny rate table + option assembly are inlined
// here (cross-checked by the manager unit tests).
//
// Usage:
//
//	cd server && GOWORK=off go run ./cmd/regen-phase2            # read-only: print the tuning table
//	cd server && GOWORK=off go run ./cmd/regen-phase2 --write    # re-author the slots + bump version
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
)

const statePath = "../.aiarch/state/project.json"

// --- AI rate card (mirrors internal/manager/projectdesign/airates.go; pinned by that
// package's unit tests: fable 4500¢, opus 2250¢, sonnet 1350¢/day). ---
var roleModel = map[string]string{
	"system-architect": "fable", "project-manager": "fable",
	"senior-developer": "opus", "product-manager": "opus", "ui-designer": "opus",
	"junior-developer": "sonnet", "qa-engineer": "sonnet", "test-engineer": "sonnet",
	"software-tester": "sonnet", "ux-reviewer": "sonnet",
}

type price struct{ in, out float64 } // cents per MTok
var pricing = map[string]price{"fable": {1000, 5000}, "opus": {500, 2500}, "sonnet": {300, 1500}, "haiku": {100, 500}}

const mtokIn, mtokOut = 2.0, 0.5

func modelFor(class string) string {
	if m, ok := roleModel[class]; ok {
		return m
	}
	return "sonnet"
}
func rateCents(class string) int64 {
	p := pricing[modelFor(class)]
	return int64(mtokIn*p.in + mtokOut*p.out)
}

const indirectDailyCents = 5000 // $50/day overhead (airates.go defaultIndirectDailyRate)

// optionSpec is one option's knobs.
type optionSpec struct {
	kind    string
	slot    string
	cap     int64
	buffer  float64
	speedup float64
}

// Exclusion-zone bounds (recommendOption / App C §4.7).
const riskTooRisky, riskOverSafe, maxCompression = 0.75, 0.30, 0.30

func main() {
	write := len(os.Args) > 1 && os.Args[1] == "--write"

	raw, err := os.ReadFile(statePath)
	must(err)
	var doc map[string]json.RawMessage
	must(json.Unmarshal(raw, &doc))
	var slots map[string]json.RawMessage
	must(json.Unmarshal(doc["slots"], &slots))

	activities := readActivities(slots["9"])
	deps, milestones := readNetwork(slots["10"])

	// Distinct worker classes + their derived rates.
	classes := map[string]struct{}{}
	for _, a := range activities {
		classes[a.WorkerClass] = struct{}{}
	}
	rates := map[string]estimation.Money{}
	for c := range classes {
		rates[c] = estimation.Money{MinorUnits: rateCents(c), Currency: "USD"}
	}

	specs := []optionSpec{
		{"normalSolution", "11", 6, 0, 1},
		{"subcriticalSolution", "12", 4, 0, 1},
		{"compressedSolution", "13", 6, 0, 1.8},
		{"decompressedSolution", "14", 6, decompBuffer, 1},
	}

	eng := estimation.NewEstimationEngine()
	fmt.Printf("%-22s %8s %10s %10s %10s %8s %8s %8s\n", "option", "durDays", "direct$", "indirect$", "total$", "crit", "activity", "composite")
	results := map[string]estimation.ConstructionEstimate{}
	for _, s := range specs {
		opt := buildOption(s, activities, deps, milestones, rates)
		est, err := eng.EstimateForOption(fweng.Context{}, opt)
		must(err)
		results[s.slot] = est
		fmt.Printf("%-22s %8.1f %10.2f %10.2f %10.2f %8.3f %8.3f %8.3f\n",
			s.kind, est.DurationDays,
			float64(est.DirectCost.MinorUnits)/100, float64(est.IndirectCost.MinorUnits)/100,
			float64(est.BuildCost.MinorUnits)/100,
			est.Risk.CriticalityRisk, est.Risk.ActivityRisk, est.Risk.Composite)
	}

	if !write {
		fmt.Println("\n-- compressed speedup sweep (cap 6) vs normal --")
		nrm, _ := eng.EstimateForOption(fweng.Context{}, buildOption(optionSpec{"n", "n", 6, 0, 1}, activities, deps, milestones, rates))
		for _, sp := range []float64{1.2, 1.3, 1.4, 1.5, 1.6, 1.8} {
			opt := buildOption(optionSpec{"c", "c", 6, 0, sp}, activities, deps, milestones, rates)
			est, _ := eng.EstimateForOption(fweng.Context{}, opt)
			comp := (nrm.DurationDays - est.DurationDays) / nrm.DurationDays * 100
			fmt.Printf("  sp=%.1f  dur=%.1f (comp %.1f%%)  total$=%.0f (normal %.0f)  composite=%.3f (normal %.3f)\n",
				sp, est.DurationDays, comp, float64(est.BuildCost.MinorUnits)/100, float64(nrm.BuildCost.MinorUnits)/100, est.Risk.Composite, nrm.Risk.Composite)
		}
		fmt.Println("\n-- cap sweep (buffer 0, speedup 1): find where normal has genuine float --")
		for _, c := range []int64{3, 4, 5, 6, 8, 10, 12, 15, 20} {
			opt := buildOption(optionSpec{"x", "x", c, 0, 1}, activities, deps, milestones, rates)
			est, _ := eng.EstimateForOption(fweng.Context{}, opt)
			fmt.Printf("  cap=%2d  dur=%.1f  crit=%.3f  activity=%.3f  composite=%.3f\n", c, est.DurationDays, est.Risk.CriticalityRisk, est.Risk.ActivityRisk, est.Risk.Composite)
		}
	}

	// Decompression-buffer sweep (tuning aid): show criticality risk vs buffer.
	if !write {
		fmt.Println("\n-- decompressed risk sweep (cap 6) --")
		for _, b := range []float64{0, 5, 10, 15, 20, 25, 30, 40, 60} {
			opt := buildOption(optionSpec{"decompressedSolution", "14", 6, b, 1}, activities, deps, milestones, rates)
			est, _ := eng.EstimateForOption(fweng.Context{}, opt)
			fmt.Printf("  buffer=%.0f  dur=%.1f  crit=%.3f  composite=%.3f\n", b, est.DurationDays, est.Risk.CriticalityRisk, est.Risk.Composite)
		}
		fmt.Println("\n(read-only; pass --write to re-author the slots)")
		return
	}

	writeState(doc, slots, specs, results, rates)
}

// decompBuffer is tuned so composite risk lands near the ~0.5 tipping point (cap 6:
// buffer 20 → composite ≈0.49) without over-decompressing.
const decompBuffer = 20

func buildOption(s optionSpec, acts []estimation.OptionActivity, deps []estimation.NetworkDependency, ms []estimation.NetworkMilestone, rates map[string]estimation.Money) estimation.ProjectOption {
	return estimation.ProjectOption{
		OptionId:            estimation.OptionID(s.kind),
		Network:             estimation.ActivityNetwork{Activities: acts, Dependencies: deps, Milestones: ms},
		WorkerMix:           estimation.WorkerMix{ClassRates: rates, StaffingCap: s.cap},
		CalendarDaysPerWeek: 2, // shared planning calendar (moonlight 2 d/wk)
		IndirectDailyRate:   estimation.Money{MinorUnits: indirectDailyCents, Currency: "USD"},
		BufferDays:          s.buffer,
		CriticalSpeedup:     s.speedup,
	}
}

func readActivities(rawSlot json.RawMessage) []estimation.OptionActivity {
	var slot struct {
		Model struct {
			Activities []struct {
				Name        string  `json:"name"`
				EffortDays  float64 `json:"effortDays"`
				WorkerClass string  `json:"workerClass"`
			} `json:"activities"`
		} `json:"model"`
	}
	must(json.Unmarshal(rawSlot, &slot))
	out := make([]estimation.OptionActivity, 0, len(slot.Model.Activities))
	for _, a := range slot.Model.Activities {
		out = append(out, estimation.OptionActivity{ActivityId: a.Name, EffortDays: a.EffortDays, WorkerClass: a.WorkerClass})
	}
	return out
}

func readNetwork(rawSlot json.RawMessage) ([]estimation.NetworkDependency, []estimation.NetworkMilestone) {
	var slot struct {
		Model struct {
			Dependencies []struct {
				Activity  string   `json:"activity"`
				DependsOn []string `json:"dependsOn"`
			} `json:"dependencies"`
			Milestones []struct {
				ID        string   `json:"id"`
				DependsOn []string `json:"dependsOn"`
			} `json:"milestones"`
		} `json:"model"`
	}
	must(json.Unmarshal(rawSlot, &slot))
	deps := make([]estimation.NetworkDependency, 0, len(slot.Model.Dependencies))
	for _, d := range slot.Model.Dependencies {
		deps = append(deps, estimation.NetworkDependency{Activity: d.Activity, DependsOn: d.DependsOn})
	}
	ms := make([]estimation.NetworkMilestone, 0, len(slot.Model.Milestones))
	for _, m := range slot.Model.Milestones {
		ms = append(ms, estimation.NetworkMilestone{Id: m.ID, DependsOn: m.DependsOn})
	}
	return deps, ms
}

// existingRows indexes the committed sdpReview rows by their solutionKind wire string, so
// the regen PRESERVES the operation/settlement facets (projectedMonthlyCost,
// expectedPerCycleNet, revenueSharePercent) and the optionId label — this rework only
// recomputes the construction-side columns (duration/cost/risk).
func existingRows(slots map[string]json.RawMessage) map[string]map[string]any {
	var slot struct {
		Model struct {
			Options []map[string]any `json:"options"`
		} `json:"model"`
	}
	_ = json.Unmarshal(slots["16"], &slot)
	out := map[string]map[string]any{}
	for _, r := range slot.Model.Options {
		if k, ok := r["solutionKind"].(string); ok {
			out[k] = r
		}
	}
	return out
}

func writeState(doc, slots map[string]json.RawMessage, specs []optionSpec, results map[string]estimation.ConstructionEstimate, rates map[string]estimation.Money) {
	prior := existingRows(slots)
	// classRates JSON (sorted for determinism).
	classRatesJSON := map[string]any{}
	classNames := make([]string, 0, len(rates))
	for c := range rates {
		classNames = append(classNames, c)
	}
	sort.Strings(classNames)
	for _, c := range classNames {
		classRatesJSON[c] = map[string]any{"minorUnits": rates[c].MinorUnits, "currency": "USD"}
	}
	rateCard := map[string]any{}
	for _, c := range classNames {
		rateCard[c] = map[string]any{"modelId": modelFor(c), "megatokensInPerDay": mtokIn, "megatokensOutPerDay": mtokOut}
	}

	// 1) planningAssumptions: add rateCard + indirectDailyRate (drop phantom classes — the
	// card only lists the real activity-list classes).
	patchModel(slots, "8", func(m map[string]any) {
		m["rateCard"] = rateCard
		m["indirectDailyRate"] = map[string]any{"minorUnits": indirectDailyCents, "currency": "USD"}
	})

	// 2) four solution slots: shared calendar (0 override), caps/buffer/speedup, AI rates.
	for _, s := range specs {
		s := s
		patchModel(slots, s.slot, func(m map[string]any) {
			m["staffingCap"] = s.cap
			m["calendarDaysPerWeek"] = 0 // no per-option override — use the shared planning calendar
			m["bufferDays"] = s.buffer
			m["criticalSpeedup"] = s.speedup
			m["classRates"] = classRatesJSON
		})
	}

	// 3) sdpReview rows + recommendation (F10, from the real engine numbers). Only the
	// construction-side columns are recomputed; optionId + operation/settlement facets are
	// preserved from the prior committed row (this rework owns duration/cost/risk only).
	rows := make([]map[string]any, 0, len(specs))
	riskRows := make([]map[string]any, 0, len(specs))
	optionIDByKind := map[string]string{}
	normalDur := results["11"].DurationDays
	for _, s := range specs {
		e := results[s.slot]
		included, reason := includeVerdict(e, normalDur)
		row := map[string]any{}
		for k, v := range prior[s.kind] {
			row[k] = v
		}
		optionID := s.kind
		if oid, ok := row["optionId"].(string); ok && oid != "" {
			optionID = oid
		}
		optionIDByKind[s.kind] = optionID
		row["optionId"] = optionID
		row["solutionKind"] = s.kind
		row["durationDays"] = e.DurationDays
		row["buildCost"] = money(e.BuildCost)
		row["compositeRisk"] = e.Risk.Composite
		rows = append(rows, row)

		riskRows = append(riskRows, map[string]any{
			"solutionKind":    s.kind,
			"criticalityRisk": e.Risk.CriticalityRisk,
			"activityRisk":    e.Risk.ActivityRisk,
			"composite":       e.Risk.Composite,
			"durationDays":    e.DurationDays,
			"totalCost":       money(e.BuildCost),
			"included":        included,
			"exclusionReason": reason,
		})
	}
	recKind, rationale := recommend(specs, results, normalDur)

	patchModel(slots, "16", func(m map[string]any) {
		m["options"] = rows
		m["recommendation"] = optionIDByKind[recKind]
		m["rationale"] = rationale
	})
	patchModel(slots, "15", func(m map[string]any) {
		m["rows"] = riskRows
		m["tooRiskyThreshold"] = riskTooRisky
		m["overSafeThreshold"] = riskOverSafe
		m["maxCompressionPct"] = maxCompression
		m["recommendation"] = recKind
	})

	// Re-marshal slots + bump version/updatedAt.
	slotsJSON, err := json.Marshal(slots)
	must(err)
	doc["slots"] = slotsJSON
	var ver int64
	must(json.Unmarshal(doc["version"], &ver))
	ver++
	vJSON, _ := json.Marshal(ver)
	doc["version"] = vJSON
	tJSON, _ := json.Marshal(time.Now().UTC().Format(time.RFC3339Nano))
	doc["updatedAt"] = tJSON

	out, err := json.MarshalIndent(doc, "", "  ")
	must(err)
	out = append(out, '\n')
	must(os.WriteFile(statePath, out, 0o600))
	fmt.Printf("wrote %s (version -> %d, recommendation=%s)\n", statePath, ver, recKind)
}

func includeVerdict(e estimation.ConstructionEstimate, normalDur float64) (bool, string) {
	if e.Risk.Composite > riskTooRisky {
		return false, fmt.Sprintf("composite risk %.3f exceeds the %.2f ceiling", e.Risk.Composite, riskTooRisky)
	}
	if e.Risk.Composite < riskOverSafe {
		return false, fmt.Sprintf("composite risk %.3f below the %.2f floor (too safe)", e.Risk.Composite, riskOverSafe)
	}
	if normalDur > 0 && e.DurationDays < normalDur && (normalDur-e.DurationDays)/normalDur > maxCompression {
		return false, "compression exceeds the 30% cap"
	}
	return true, ""
}

func recommend(specs []optionSpec, results map[string]estimation.ConstructionEstimate, normalDur float64) (string, string) {
	best, bestComposite := "", 2.0
	for _, s := range specs {
		e := results[s.slot]
		if ok, _ := includeVerdict(e, normalDur); !ok {
			continue
		}
		if e.Risk.Composite < bestComposite {
			best, bestComposite = s.kind, e.Risk.Composite
		}
	}
	if best == "" {
		// Fallback: lowest composite overall.
		for _, s := range specs {
			if e := results[s.slot]; e.Risk.Composite < bestComposite {
				best, bestComposite = s.kind, e.Risk.Composite
			}
		}
		return best, fmt.Sprintf("all options out of the App C risk band; picked lowest composite risk %.3f", bestComposite)
	}
	return best, fmt.Sprintf("recommend %s: lowest in-band composite risk %.3f (App C exclusions applied)", best, bestComposite)
}

func money(m estimation.Money) map[string]any {
	c := m.Currency
	if c == "" {
		c = "USD"
	}
	return map[string]any{"minorUnits": m.MinorUnits, "currency": c}
}

// patchModel decodes slot[key].model, applies fn, and re-encodes it in place.
func patchModel(slots map[string]json.RawMessage, key string, fn func(map[string]any)) {
	var slot map[string]json.RawMessage
	must(json.Unmarshal(slots[key], &slot))
	var model map[string]any
	must(json.Unmarshal(slot["model"], &model))
	fn(model)
	mJSON, err := json.Marshal(model)
	must(err)
	slot["model"] = mJSON
	sJSON, err := json.Marshal(slot)
	must(err)
	slots[key] = sJSON
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "regen-phase2:", err)
		os.Exit(1)
	}
}
