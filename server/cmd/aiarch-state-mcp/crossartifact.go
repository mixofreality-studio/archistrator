package main

// crossartifact.go — APP-SIDE cross-artifact Method rules, appended to the framework
// methodcheck finding set at every enforcement point (putDraftModel's in-loop gate,
// applyConstructionMutation, and the `validate` CI subcommand — the append happens
// inside applyGateSeverityPolicies so no enforcement point can miss them and the
// in-loop and CI verdicts can never disagree).
//
// The rules close the System×ActivityList join the framework rules do not cover — the
// drift class observed live on gtdapp 2026-07-10: the Phase-1 System was amended
// (components renamed persistence-access→item-access / person-client→webapp-client,
// NEW components added) while the committed Phase-2 activityList still carried one
// coding activity per OLD component and nothing for the new ones. Only the generic
// staleness flag caught it, and staleness can be acknowledged away.
//
//	ACT-COMPONENT-COVERAGE (Error)   every committed System component in a CODE layer
//	                                 (client / manager / engine / resourceAccess) must
//	                                 be covered by ≥1 coding activity in the committed
//	                                 activityList — The Method's "exactly ONE coding
//	                                 activity per component" (ch. 7; resources and
//	                                 utilities get noncoding provision activities).
//	ACT-UNKNOWN-COMPONENT  (Warning) a coding activity whose derived component matches
//	                                 NO committed System component — the stale-name
//	                                 residue of a component rename.
//	PA-RATECARD-KEYS       (Warning) a planningAssumptions.rateCard key that equals no
//	                                 planningAssumptions.resources entry is dead config:
//	                                 the cost engine's deriveClassRates does an EXACT
//	                                 pa.RateCard[class] lookup (manager/projectdesign/
//	                                 airates.go), so an orphan key is silently ignored.
//	PA-RATECARD-DEFAULTED  (Info)    resource classes with NO rateCard entry ride the
//	                                 documented default rate spec — listed so the
//	                                 silent default is at least visible.
//
// THE ACTIVITY→COMPONENT JOIN mirrors the construction pump's dispatch join
// (internal/manager/construction/eligibility.go, resolveComponentID): normalize to
// [a-z0-9] and take the LONGEST component key contained in the normalized subject,
// with the title truncated at '(' exactly as dispatch does. Dispatch matches the
// activity TITLE against the serviceContracts keys; at Phase-2 validate time no
// contracts exist yet, so the match domain here is the committed System's component
// IDs, Names, and declared ContractKeys — and the activity NAME is a subject alongside
// the title, which is what resolves the `<componentId>-coding` naming convention
// (gtdapp shape) as well as the `Build <ComponentName>` title convention. The derive
// is per-activity single-target (longest match wins), exactly like dispatch, so a
// coding activity covers precisely ONE component.
//
// Severity policy: these are System×ActivityList JOIN rules, so they take the SAME
// staleness-aware downgrade the DEP-* System×OperationalConcepts rules take (see
// staleness.go, systemActivityListJoinRules): while the activityList slot is flagged
// StaleBasis, reconciliation is pending by design and the Error downgrades to Warning.
// Slot-scoped attribution: ACT-* → activityList, PA-* → planningAssumptions
// (ruleSlotAttributionPrefixes), so a session amending a DIFFERENT slot is never
// deadlocked by them.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// appendAppSideCrossArtifactFindings appends the app-side cross-artifact findings for
// proj to findings. Rules only fire over COMMITTED slots (the same posture as the
// framework's cross-artifact rules): an absent / uncommitted counterpart slot means
// there is nothing to join yet, never a violation.
func appendAppSideCrossArtifactFindings(proj projectstate.Project, findings []methodcheck.Finding) []methodcheck.Finding {
	findings = append(findings, activityCoverageFindings(proj)...)
	findings = append(findings, rateCardFindings(proj)...)
	findings = append(findings, paEnumHoleFindings(proj)...)
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

// activityCoverageFindings emits ACT-COMPONENT-COVERAGE / ACT-UNKNOWN-COMPONENT over
// the committed System × committed ActivityList join.
func activityCoverageFindings(proj projectstate.Project) []methodcheck.Finding {
	sys, ok := committedSystem(proj)
	if !ok {
		return nil
	}
	al, ok := committedActivityList(proj)
	if !ok {
		return nil
	}

	var out []methodcheck.Finding
	covered := make(map[string]bool)
	for i, item := range al.Activities {
		if !item.Coding || isIntegrationActivity(item) {
			// Noncoding activities provision resources/utilities and integration
			// activities span components — neither maps to a single code component.
			continue
		}
		compID, found := deriveActivityComponent(item, sys.Components)
		if !found {
			out = append(out, methodcheck.Finding{
				RuleID:   "ACT-UNKNOWN-COMPONENT",
				Severity: methodcheck.SeverityWarning,
				Message: fmt.Sprintf(
					"coding activity %q (%q) derives NO committed System component — likely a stale activity name left behind by a component rename; reconcile the activityList with the amended System",
					item.Name, item.Title),
				Location: &methodcheck.Location{Ordinal: i, Section: "activity " + item.Name},
			})
			continue
		}
		covered[compID] = true
	}
	for i, comp := range sys.Components {
		if !isCodeComponentKind(comp.Kind) {
			continue // resources/utilities get noncoding provision activities, not coding ones
		}
		if covered[comp.ID] {
			continue
		}
		out = append(out, methodcheck.Finding{
			RuleID:   "ACT-COMPONENT-COVERAGE",
			Severity: methodcheck.SeverityError,
			Message: fmt.Sprintf(
				"committed System component %q (%s %q) is covered by NO coding activity in the committed activityList — The Method requires exactly one coding activity per code component (clients/managers/engines/resourceAccess); a System amendment that added or renamed components needs a matching activityList amendment",
				comp.ID, componentKindWord(comp.Kind), comp.Name),
			Location: &methodcheck.Location{Ordinal: i, Section: "component " + comp.ID},
		})
	}
	return out
}

// deriveActivityComponent derives the single System component a coding activity
// builds — the validate-time twin of the construction pump's resolveComponentID
// (internal/manager/construction/eligibility.go): normalized longest-key containment,
// title truncated at '('. Subjects are the activity Name AND Title; keys are the
// component ID, Name, and declared ContractKey. Longest normalized key wins, so an
// activity resolves "operation-estimation-engine" over "estimation-engine".
func deriveActivityComponent(item projectstate.ActivityItem, comps []projectstate.Component) (string, bool) {
	var subjects []string
	if n := normalizeIdent(item.Name); n != "" {
		subjects = append(subjects, n)
	}
	title := item.Title
	if i := strings.IndexByte(title, '('); i >= 0 {
		title = title[:i]
	}
	if n := normalizeIdent(title); n != "" {
		subjects = append(subjects, n)
	}

	best, bestLen := "", 0
	for _, comp := range comps {
		keys := []string{comp.ID, comp.Name}
		if comp.ContractKey != nil {
			keys = append(keys, *comp.ContractKey)
		}
		for _, key := range keys {
			kn := normalizeIdent(key)
			if kn == "" || len(kn) <= bestLen {
				continue
			}
			for _, subject := range subjects {
				if strings.Contains(subject, kn) {
					best, bestLen = comp.ID, len(kn)
					break
				}
			}
		}
	}
	return best, best != ""
}

// isIntegrationActivity reports whether a coding activity is an INTEGRATION activity —
// exempt from the per-component mapping (it spans components). Recognizes both live
// naming conventions: `integrate-*` names (gtdapp shape) and the corpus `I-*` id family
// (seed-construction normalizeID), plus an "integration" title as the fallback signal.
func isIntegrationActivity(item projectstate.ActivityItem) bool {
	name := strings.ToLower(strings.TrimSpace(item.Name))
	if strings.HasPrefix(name, "integrate") || strings.HasPrefix(name, "i-") {
		return true
	}
	return strings.Contains(strings.ToLower(item.Title), "integration")
}

// isCodeComponentKind reports whether the component kind is one of the four CODE
// layers that The Method gives exactly one coding activity each. Resources and
// utilities are provisioned by noncoding activities instead.
func isCodeComponentKind(k projectstate.ComponentKind) bool {
	switch k {
	case projectstate.CompClient, projectstate.CompManager, projectstate.CompEngine, projectstate.CompResourceAccess:
		return true
	default:
		return false
	}
}

// componentKindWord renders a component kind for finding messages.
func componentKindWord(k projectstate.ComponentKind) string {
	switch k {
	case projectstate.CompClient:
		return "client"
	case projectstate.CompManager:
		return "manager"
	case projectstate.CompEngine:
		return "engine"
	case projectstate.CompResourceAccess:
		return "resourceAccess"
	case projectstate.CompResource:
		return "resource"
	case projectstate.CompUtility:
		return "utility"
	default:
		return "component"
	}
}

// normalizeIdent lowercases s and keeps only [a-z0-9] — byte-for-byte the dispatch
// join's normalizer (internal/manager/construction/eligibility.go normalizeIdent),
// duplicated here because it is unexported there and this binary must not widen the
// construction Manager's surface for a validator concern.
func normalizeIdent(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
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

// committedSystem returns the committed System model, when present.
func committedSystem(proj projectstate.Project) (*projectstate.System, bool) {
	if proj.SystemDesign.Status != projectstate.ReviewCommitted {
		return nil, false
	}
	sys, ok := proj.SystemDesign.Model.(*projectstate.System)
	if !ok || sys == nil {
		return nil, false
	}
	return sys, true
}

// committedActivityList returns the committed ActivityList model, when present.
func committedActivityList(proj projectstate.Project) (*projectstate.ActivityList, bool) {
	if proj.ActivityList.Status != projectstate.ReviewCommitted {
		return nil, false
	}
	al, ok := proj.ActivityList.Model.(*projectstate.ActivityList)
	if !ok || al == nil {
		return nil, false
	}
	return al, true
}
