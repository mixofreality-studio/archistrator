package main

// crossartifact.go — APP-SIDE cross-artifact Method rules, appended to the framework
// methodcheck finding set at every enforcement point (putDraftModel's in-loop gate,
// applyConstructionMutation, and the `validate` CI subcommand — the append happens
// inside applyGateSeverityPolicies so no enforcement point can miss them and the
// in-loop and CI verdicts can never disagree).
//
// The System×ActivityList coverage join that used to live here (ACT-COMPONENT-COVERAGE /
// ACT-UNKNOWN-COMPONENT, retired 2026-08-09) is GONE: coverage moved from validation to
// derivation. The Phase-2 activity list is now DERIVED from the committed System
// (server/internal/engine/estimation, DerivePlan) rather than hand-authored, so "does
// every code component have exactly one coding activity" is true BY CONSTRUCTION — a
// stronger guarantee than any gate could check after the fact, backed by the
// `derived-plan-check` drift gate (server/internal/manager/projectdesign/manager_test.go,
// TestDerivedPlanMatchesCommittedState) rather than a validation rule. See
// docs/superpowers/specs/2026-08-09-derived-activity-list-design.md and Task 11 of
// docs/superpowers/plans/2026-08-09-derived-activity-list-stage1.md.
//
//	PA-RATECARD-KEYS       (Warning) a planningAssumptions.rateCard key that equals no
//	                                 planningAssumptions.resources entry is dead config:
//	                                 the cost engine's deriveClassRates does an EXACT
//	                                 pa.RateCard[class] lookup (manager/projectdesign/
//	                                 airates.go), so an orphan key is silently ignored.
//	PA-RATECARD-DEFAULTED  (Info)    resource classes with NO rateCard entry ride the
//	                                 documented default rate spec — listed so the
//	                                 silent default is at least visible.
//
// Slot-scoped attribution: PA-* → planningAssumptions (ruleSlotAttributionPrefixes), so
// a session amending a DIFFERENT slot is never deadlocked by them.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/designhealth"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// appendAppSideCrossArtifactFindings appends the app-side cross-artifact findings for
// proj to findings. Rules only fire over COMMITTED slots (the same posture as the
// framework's cross-artifact rules): an absent / uncommitted counterpart slot means
// there is nothing to join yet, never a violation.
//
// It also appends the LIVE design-health tier (internal/engine/designhealth, Wave-2
// reshape-3): the pure DH-* rule layer over the re-encoded document. Re-encoding
// proj yields the same bytes methodcheck.ValidateProjectJSON validated at this seam,
// so the live tier sees exactly the drafted state. DH-* findings then flow through
// the same staleness / slot-scope severity policies below (DH-* is attributed to the
// systemDesign slot in ruleSlotAttributionPrefixes), so the in-loop authoring gate,
// the construct gate, and the CI `validate` subcommand share one design-health verdict.
func appendAppSideCrossArtifactFindings(proj projectstate.Project, findings []methodcheck.Finding) []methodcheck.Finding {
	findings = append(findings, rateCardFindings(proj)...)
	findings = append(findings, paEnumHoleFindings(proj)...)
	if raw, err := projectstate.EncodeProjectJSON(proj); err == nil {
		findings = append(findings, designhealth.EvaluateRaw(raw)...)
	}
	return findings
}

// paEnumHoleFindings emits PA-INFRA-KIND / PA-TERMS-REGIME over the committed
// PlanningAssumptions — the zero-value enum holes that pass every earlier gate and
// then FAIL the deterministic SDP assembly (found live on gtdapp 2026-07-11: the
// operationEstimationEngine refused infrastructureKind=Unknown, then the
// settlementEngine refused revenueShare/computeCost=Unknown; both engines correctly
// never silently default, so the hole must be caught at draft time instead).
//
//	PA-INFRA-KIND   (Error)  infrastructureKind is Unknown — every estimate engine
//	                         needs a concrete launch infrastructure.
//	PA-TERMS-REGIME (Error)  a settlement PERCENT is authored while its regime KIND
//	                         enum is Unknown (revenueSharePercent>0 with
//	                         revenueShare=0, computeMarkupPercent>0 with
//	                         computeCost=0, or either regime set with schedule=0) —
//	                         the terms fields are kind enums, not amounts, and the
//	                         settlementEngine refuses Unknown regimes.
func paEnumHoleFindings(proj projectstate.Project) []methodcheck.Finding {
	if proj.PlanningAssumptions.Status != projectstate.ReviewCommitted {
		return nil
	}
	pa, ok := proj.PlanningAssumptions.Model.(*projectstate.PlanningAssumptions)
	if !ok || pa == nil {
		return nil
	}

	var out []methodcheck.Finding
	if pa.InfrastructureKind == projectstate.InfrastructureKindUnknown {
		out = append(out, methodcheck.Finding{
			RuleID:   "PA-INFRA-KIND",
			Severity: methodcheck.SeverityError,
			Message: "planningAssumptions.infrastructureKind is 0 (unknown) — the estimate engines refuse an unknown " +
				"launch infrastructure at SDP assembly (no silent default); author the concrete kind (1 = goTemporalPostgres, the platform palette)",
			Location: &methodcheck.Location{Ordinal: 0, Section: "infrastructureKind"},
		})
	}

	t := pa.Terms
	regimeAuthored := t.RevenueShare != projectstate.RevenueShareUnknown || t.ComputeCost != projectstate.ComputeCostUnknown
	if t.RevenueSharePercent > 0 && t.RevenueShare == projectstate.RevenueShareUnknown {
		out = append(out, methodcheck.Finding{
			RuleID:   "PA-TERMS-REGIME",
			Severity: methodcheck.SeverityError,
			Message: "terms.revenueSharePercent is authored but terms.revenueShare is 0 (unknown) — revenueShare is a KIND enum " +
				"(1=launchFlat10, 2=negotiatedRate), not an amount; the settlementEngine refuses unknown regimes",
			Location: &methodcheck.Location{Ordinal: 0, Section: "terms.revenueShare"},
		})
	}
	if t.ComputeMarkupPercent > 0 && t.ComputeCost == projectstate.ComputeCostUnknown {
		out = append(out, methodcheck.Finding{
			RuleID:   "PA-TERMS-REGIME",
			Severity: methodcheck.SeverityError,
			Message: "terms.computeMarkupPercent is authored but terms.computeCost is 0 (unknown) — computeCost is a KIND enum " +
				"(1=flatMarkup, 2=tieredFloors), not an amount; the settlementEngine refuses unknown regimes",
			Location: &methodcheck.Location{Ordinal: 1, Section: "terms.computeCost"},
		})
	}
	if regimeAuthored && t.Schedule == projectstate.ScheduleUnknown {
		out = append(out, methodcheck.Finding{
			RuleID:   "PA-TERMS-REGIME",
			Severity: methodcheck.SeverityError,
			Message: "settlement regimes are authored but terms.schedule is 0 (unknown) — schedule is a KIND enum " +
				"(1=monthly, 2=weekly, 3=daily); the settlementEngine refuses an unknown billing schedule",
			Location: &methodcheck.Location{Ordinal: 2, Section: "terms.schedule"},
		})
	}
	return out
}

// rateCardFindings emits PA-RATECARD-KEYS / PA-RATECARD-DEFAULTED over the committed
// PlanningAssumptions (a single-artifact consistency check: rateCard keys against the
// declared resource classes).
func rateCardFindings(proj projectstate.Project) []methodcheck.Finding {
	if proj.PlanningAssumptions.Status != projectstate.ReviewCommitted {
		return nil
	}
	pa, ok := proj.PlanningAssumptions.Model.(*projectstate.PlanningAssumptions)
	if !ok || pa == nil {
		return nil
	}

	resources := make(map[string]bool, len(pa.Resources))
	for _, r := range pa.Resources {
		resources[r] = true
	}

	var out []methodcheck.Finding
	keys := make([]string, 0, len(pa.RateCard))
	for k := range pa.RateCard {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic finding order over the map
	for i, key := range keys {
		if resources[key] {
			continue
		}
		out = append(out, methodcheck.Finding{
			RuleID:   "PA-RATECARD-KEYS",
			Severity: methodcheck.SeverityWarning,
			Message: fmt.Sprintf(
				"planningAssumptions.rateCard key %q equals no planningAssumptions.resources entry (resources: %s) — the cost engine's deriveClassRates does an EXACT pa.RateCard[class] lookup, so this entry is dead config that silently never applies (orphaned by a rename or a spelling drift?)",
				key, strings.Join(pa.Resources, ", ")),
			Location: &methodcheck.Location{Ordinal: i, Section: "rateCard " + key},
		})
	}

	var defaulted []string
	for _, r := range pa.Resources {
		if _, ok := pa.RateCard[r]; !ok {
			defaulted = append(defaulted, r)
		}
	}
	if len(defaulted) > 0 {
		out = append(out, methodcheck.Finding{
			RuleID:   "PA-RATECARD-DEFAULTED",
			Severity: methodcheck.SeverityInfo,
			Message: fmt.Sprintf(
				"resource class(es) with no rateCard entry ride the silent default rate spec: %s — author rateCard entries to make their cost basis explicit",
				strings.Join(defaulted, ", ")),
			Location: &methodcheck.Location{Ordinal: 0, Section: "rateCard"},
		})
	}
	return out
}
