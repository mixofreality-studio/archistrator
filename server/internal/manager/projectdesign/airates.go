package projectdesign

// airates.go derives each project option's per-worker-class build-cost rate from the AI
// rate card (Phase-2 estimation rework F11). Team members are AI AGENTS, not humans, so
// the old flat $800/$500 human day-rates are gone: an agent's cost is the LLM inference
// it burns per agent-day = expected tokens × the Claude API price for the model that
// agent runs.
//
//	rate($/day) = MegatokensInPerDay × price_in + MegatokensOutPerDay × price_out
//
// The role→model mapping is the source-of-truth agent roster in .claude/agents/*.md
// frontmatter (F11c). Phantom worker classes that map to no agent (architect,
// devops-agent, web-engineer-agent) are intentionally absent (F11d).
//
// Pure + deterministic (no clock, no RNG, no I/O) so the SDP assembly stays replay-safe.

import (
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// modelPrice is the Claude API price for one model, in USD MINOR UNITS (cents) per
// megatoken (MTok). Source: Anthropic price list (F11b) — fable $10/$50, opus $5/$25,
// sonnet $3/$15, haiku $1/$5 per MTok in/out.
type modelPrice struct {
	inCentsPerMTok  float64
	outCentsPerMTok float64
}

// apiPricing is the per-model Claude API price list, keyed by the frontmatter model id.
var apiPricing = map[string]modelPrice{
	"fable":  {inCentsPerMTok: 1000, outCentsPerMTok: 5000}, // $10 in / $50 out
	"opus":   {inCentsPerMTok: 500, outCentsPerMTok: 2500},  // $5 in / $25 out
	"sonnet": {inCentsPerMTok: 300, outCentsPerMTok: 1500},  // $3 in / $15 out
	"haiku":  {inCentsPerMTok: 100, outCentsPerMTok: 500},   // $1 in / $5 out
}

// priceFamily normalizes a model id to its apiPricing family key. The rate card's
// modelId is authored as a FULL API id ("claude-opus-4-8", "claude-haiku-4-5-20251001")
// while apiPricing is keyed by short family names — the exact-key lookup silently
// priced EVERY full id as sonnet (found live on gtdapp 2026-07-11: the opus architect
// class costed at sonnet rates). Substring match on the lowercased id; unknown ids
// keep the documented sonnet fallback via the caller's miss branch.
func priceFamily(modelID string) string {
	id := strings.ToLower(modelID)
	for _, fam := range [...]string{"fable", "opus", "sonnet", "haiku"} {
		if strings.Contains(id, fam) {
			return fam
		}
	}
	return id
}

// roleModel maps each worker CLASS (agent role) to the model it runs (F11c), taken
// verbatim from .claude/agents/*.md frontmatter. The phantom classes (architect,
// devops-agent, web-engineer-agent) are deliberately NOT here.
var roleModel = map[string]string{
	"system-architect": "fable",
	"project-manager":  "fable",
	"senior-developer": "opus",
	"product-manager":  "opus",
	"ui-designer":      "opus",
	"junior-developer": "sonnet",
	"qa-engineer":      "sonnet",
	"test-engineer":    "sonnet",
	"software-tester":  "sonnet",
	"ux-reviewer":      "sonnet",
}

// Default token throughput per agent-day (F11a). Kept uniform across classes so the cost
// SPREAD between classes comes purely from the model tier (fable roles are the most
// expensive per day, sonnet roles the cheapest). Tunable per-class via
// PlanningAssumptions.RateCard once the state pass authors it.
const (
	defaultMTokInPerDay  = 2.0 // ~2M input tokens / agent-day (context + tool results)
	defaultMTokOutPerDay = 0.5 // ~0.5M output tokens / agent-day (generated code + notes)
)

// defaultModelForClass returns the model a class runs, defaulting an UNKNOWN class (e.g.
// a stale "architect" fixture) to sonnet so rate derivation never fails a valid option.
func defaultModelForClass(class string) string {
	if m, ok := roleModel[class]; ok {
		return m
	}
	return "sonnet"
}

// defaultRateSpec returns the default AI rate spec for a class (uniform throughput on the
// class's mapped model).
func defaultRateSpec(class string) projectstate.WorkerRateSpec {
	return projectstate.WorkerRateSpec{
		ModelID:             defaultModelForClass(class),
		MegatokensInPerDay:  defaultMTokInPerDay,
		MegatokensOutPerDay: defaultMTokOutPerDay,
	}
}

// deriveClassRates computes the per-day build-cost rate for every worker class used by
// the option (F11b). It prefers the authored PlanningAssumptions.RateCard entry, falling
// back to the documented default spec for any class the card omits, so an option always
// assembles even before the state pass authors the card. Deterministic: the output map
// is keyed by class; iteration order is irrelevant.
func deriveClassRates(pa projectstate.PlanningAssumptions, classes []string) map[string]projectstate.Money {
	rates := make(map[string]projectstate.Money, len(classes))
	for _, class := range classes {
		spec, ok := pa.RateCard[class]
		if !ok || spec.ModelID == "" {
			spec = defaultRateSpec(class)
		}
		rates[class] = rateForSpec(spec)
	}
	return rates
}

// rateForSpec turns a rate spec into a USD/day Money via the Claude API price list. An
// unknown model id falls back to sonnet pricing (never panics). Deterministic integer
// truncation (no rounding-mode ambiguity) matches the estimationEngine's cost math.
func rateForSpec(spec projectstate.WorkerRateSpec) projectstate.Money {
	price, ok := apiPricing[priceFamily(spec.ModelID)]
	if !ok {
		price = apiPricing["sonnet"]
	}
	cents := spec.MegatokensInPerDay*price.inCentsPerMTok + spec.MegatokensOutPerDay*price.outCentsPerMTok
	return projectstate.Money{MinorUnits: int64(cents), Currency: "USD"}
}

// defaultIndirectDailyRate is the overhead burn per calendar day used when
// PlanningAssumptions.IndirectDailyRate is unset (F6). $50/day (5000 cents USD) — the
// platform/orchestration overhead that accrues over the schedule regardless of which
// agents are active. Makes a longer (subcritical) option demonstrably costlier.
var defaultIndirectDailyRate = projectstate.Money{MinorUnits: 5000, Currency: "USD"}

// indirectDailyRateOf returns the authored indirect rate, or the documented default when
// unset.
func indirectDailyRateOf(pa projectstate.PlanningAssumptions) projectstate.Money {
	if pa.IndirectDailyRate.MinorUnits != 0 || pa.IndirectDailyRate.Currency != "" {
		return pa.IndirectDailyRate
	}
	return defaultIndirectDailyRate
}
