package systemdesign

// statevalidationfindings.go holds the APP-SIDE read-back finding generators for the
// state-validation rules the architect ratified 2026-07-05. Each is the review-panel
// twin of an authoritative platform methodcheck rule (tracked "platform twin pending" in
// docs/later.md); the app surfaces them as SessionStateView.Findings so the reviewer sees
// the defect at the human gate. They are DISPLAY findings — they do not hard-fail a read
// (a committed state that violates them, e.g. gtdapp's orphan ResourceAccess and
// empty-encapsulates clients, must keep rendering with the finding visible until an
// amendment fixes it). The presence/consistency rules that CAN hard-fail safely (every
// committed state already satisfies them) live in projectstate.RequireModelFields instead.
//
// Each generator early-returns nil for a non-matching artifact kind / nil draft, mirroring
// useCaseActivityFindings / systemLayerDegenerateFindings, so view() can append them all
// unconditionally.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// stateValidationFindingGenerators is the ordered set of read-back finding generators
// view() appends. Each takes the drafted artifact's kind + model and returns nil for a
// non-matching kind, so the whole set can be applied unconditionally.
var stateValidationFindingGenerators = []func(ArtifactKind, projectstate.ArtifactModel) []Finding{
	raOrphanFindings,      // SYS-RA-ORPHAN
	encapsulatesFindings,  // SYS-ENCAPSULATES
	relDupFindings,        // SYS-REL-DUP
	dvChainFindings,       // DV-CHAIN-CONNECTED
	variationRefFindings,  // UC-VARIATION-REF
	glossaryFourQFindings, // GLOSS-FOURQ
	scrubbedIDFindings,    // SR-ID-UNIQUE
	opcTopicFindings,      // OPC-TOPIC-COVERAGE
}

// raOrphanFindings — SYS-RA-ORPHAN (error). Every ResourceAccess component must have at
// least one outbound sync/queued relationship to a Resource (or to a documented external
// system — an edge target that is not itself a modeled component). A ResourceAccess that
// reaches no resource encapsulates nothing.
func raOrphanFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindSystem {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	kindByID := make(map[string]projectstate.ComponentKind, len(sys.Components))
	for _, c := range sys.Components {
		kindByID[c.ID] = c.Kind
	}
	var out []Finding
	for i, c := range sys.Components {
		if c.Kind != projectstate.CompResourceAccess {
			continue
		}
		reaches := false
		for _, r := range sys.Relationships {
			if r.From != c.ID {
				continue
			}
			if r.Mode != projectstate.CallSync && r.Mode != projectstate.CallQueued {
				continue
			}
			toKind, known := kindByID[r.To]
			// A Resource target, or an external target (not a modeled component),
			// satisfies the rule.
			if !known || toKind == projectstate.CompResource {
				reaches = true
				break
			}
		}
		if !reaches {
			label := componentDisplayLabel(c, i)
			out = append(out, Finding{
				RuleID:   "SYS-RA-ORPHAN",
				Severity: SeverityError,
				Message:  fmt.Sprintf("ResourceAccess %q has no outbound sync/queued relationship to a resource (or documented external system); every ResourceAccess must encapsulate at least one resource.", label),
				Location: &Location{Ordinal: int64(i), Section: "component " + label},
			})
		}
	}
	return out
}

// encapsulatesFindings — SYS-ENCAPSULATES. Every component should name the volatility it
// encapsulates. ERROR for the volatility-owning kinds (client/manager/engine/
// resourceAccess); WARNING for resource/utility (which legitimately own no volatility but
// benefit from a one-line "what this is"). The manager/engine/resourceAccess non-empty
// rule is ALSO enforced hard on the write path by projectstate.RequireModelFields, so in
// practice only empty-encapsulates CLIENTS (error) and resources/utilities (warning) reach
// this read-back surface — which is exactly the gtdapp case that must render, not crash.
func encapsulatesFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindSystem {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	var out []Finding
	for i, c := range sys.Components {
		if strings.TrimSpace(c.Encapsulates) != "" {
			continue
		}
		var sev Severity
		switch c.Kind {
		case projectstate.CompClient, projectstate.CompManager, projectstate.CompEngine, projectstate.CompResourceAccess:
			sev = SeverityError
		case projectstate.CompResource, projectstate.CompUtility:
			sev = SeverityWarning
		}
		label := componentDisplayLabel(c, i)
		out = append(out, Finding{
			RuleID:   "SYS-ENCAPSULATES",
			Severity: sev,
			Message:  fmt.Sprintf("component %q has an empty encapsulates; state the volatility (or, for a resource/utility, the responsibility) it owns.", label),
			Location: &Location{Ordinal: int64(i), Section: "component " + label},
		})
	}
	return out
}

// relDupFindings — SYS-REL-DUP. An EXACT duplicate relationship (same from, to AND mode)
// is an ERROR (a redundant edge). Two edges on the SAME (from,to) pair that differ (a
// label-split) are a WARNING suggesting the labels be aggregated with " | " onto one edge.
func relDupFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindSystem {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	type pair struct{ from, to string }
	exact := map[string]int{}           // from|to|mode → count
	byPair := map[pair]map[string]int{} // (from,to) → distinct label → count
	order := []pair{}
	for _, r := range sys.Relationships {
		ek := r.From + "|" + r.To + "|" + modeWire(r.Mode)
		exact[ek]++
		p := pair{r.From, r.To}
		if byPair[p] == nil {
			byPair[p] = map[string]int{}
			order = append(order, p)
		}
		byPair[p][r.Label]++
	}
	var out []Finding
	for _, p := range order {
		labels := byPair[p]
		total := 0
		for _, n := range labels {
			total += n
		}
		if total < 2 {
			continue
		}
		// Exact duplicate on any (from,to,mode)?
		dup := false
		for _, r := range sys.Relationships {
			if r.From == p.from && r.To == p.to && exact[r.From+"|"+r.To+"|"+modeWire(r.Mode)] > 1 {
				dup = true
				break
			}
		}
		if dup {
			out = append(out, Finding{
				RuleID:   "SYS-REL-DUP",
				Severity: SeverityError,
				Message:  fmt.Sprintf("relationship %s → %s is declared more than once with the same mode; remove the exact duplicate edge.", p.from, p.to),
				Location: &Location{Section: fmt.Sprintf("relationship %s → %s", p.from, p.to)},
			})
		} else if len(labels) > 1 {
			out = append(out, Finding{
				RuleID:   "SYS-REL-DUP",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("relationship %s → %s is split across %d edges with different labels; aggregate them onto one edge with a \" | \"-joined label.", p.from, p.to, len(labels)),
				Location: &Location{Section: fmt.Sprintf("relationship %s → %s", p.from, p.to)},
			})
		}
	}
	return out
}

// dvChainFindings — DV-CHAIN-CONNECTED (warning). Each dynamic view's edges should form a
// connected chain rooted at a Client participant: every participant must be reachable by
// following the directed edges out of some Client-kind participant. An unrooted or
// disconnected call chain is a modeling smell (a participant nothing calls).
func dvChainFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindSystem {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	kindByID := make(map[string]projectstate.ComponentKind, len(sys.Components))
	for _, c := range sys.Components {
		kindByID[c.ID] = c.Kind
	}
	var out []Finding
	for i, dv := range sys.DynamicViews {
		if len(dv.Participants) <= 1 {
			continue
		}
		adj := map[string][]string{}
		for _, e := range dv.Edges {
			adj[e.From] = append(adj[e.From], e.To)
		}
		roots := []string{}
		for _, pid := range dv.Participants {
			if kindByID[pid] == projectstate.CompClient {
				roots = append(roots, pid)
			}
		}
		label := dvLabel(dv, i)
		if len(roots) == 0 {
			out = append(out, Finding{
				RuleID:   "DV-CHAIN-CONNECTED",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("dynamic view %q has no Client participant to root its call chain; a use-case call chain should originate at a Client.", label),
				Location: &Location{Ordinal: int64(i), Section: "dynamic view " + label},
			})
			continue
		}
		seen := map[string]bool{}
		stack := append([]string{}, roots...)
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[n] {
				continue
			}
			seen[n] = true
			stack = append(stack, adj[n]...)
		}
		var unreached []string
		for _, pid := range dv.Participants {
			if !seen[pid] {
				unreached = append(unreached, pid)
			}
		}
		if len(unreached) > 0 {
			sort.Strings(unreached)
			out = append(out, Finding{
				RuleID:   "DV-CHAIN-CONNECTED",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("dynamic view %q is not a connected chain from its Client root(s): %s unreachable via its edges.", label, strings.Join(unreached, ", ")),
				Location: &Location{Ordinal: int64(i), Section: "dynamic view " + label},
			})
		}
	}
	return out
}

// variationRefFindings — UC-VARIATION-REF (error). variationOf, when set, must resolve to
// an existing use-case id whose target is CORE. A nonCore use case must carry a non-empty
// rejectionReason. A core use case must NOT carry a variationOf (it is the base, not a
// permutation).
func variationRefFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindCoreUseCases {
		return nil
	}
	cuc, ok := draft.(*projectstate.CoreUseCases)
	if !ok || cuc == nil {
		return nil
	}
	coreIDs := map[projectstate.UseCaseID]bool{}
	for _, d := range cuc.Decisions {
		if d.UseCase.Classification == projectstate.ClassCore {
			coreIDs[d.UseCase.ID] = true
		}
	}
	var out []Finding
	for i, d := range cuc.Decisions {
		uc := d.UseCase
		label := uc.Name
		if label == "" {
			label = fmt.Sprintf("use case %d", i+1)
		}
		loc := &Location{Ordinal: int64(i), Section: "use case " + label}
		if uc.Classification == projectstate.ClassCore {
			if uc.VariationOf != nil && strings.TrimSpace(string(*uc.VariationOf)) != "" {
				out = append(out, Finding{
					RuleID:   "UC-VARIATION-REF",
					Severity: SeverityError,
					Message:  fmt.Sprintf("core use case %q declares a variationOf (%q); a core use case is a base, not a variation — clear variationOf or reclassify it nonCore.", label, string(*uc.VariationOf)),
					Location: loc,
				})
			}
			continue
		}
		// nonCore
		if uc.VariationOf == nil || strings.TrimSpace(string(*uc.VariationOf)) == "" {
			out = append(out, Finding{
				RuleID:   "UC-VARIATION-REF",
				Severity: SeverityError,
				Message:  fmt.Sprintf("nonCore use case %q has no variationOf; a nonCore use case must link to the core use case it permutes.", label),
				Location: loc,
			})
		} else if !coreIDs[*uc.VariationOf] {
			out = append(out, Finding{
				RuleID:   "UC-VARIATION-REF",
				Severity: SeverityError,
				Message:  fmt.Sprintf("nonCore use case %q has variationOf %q, which does not resolve to an existing CORE use case.", label, string(*uc.VariationOf)),
				Location: loc,
			})
		}
		if strings.TrimSpace(d.RejectionReason) == "" {
			out = append(out, Finding{
				RuleID:   "UC-VARIATION-REF",
				Severity: SeverityError,
				Message:  fmt.Sprintf("nonCore use case %q has an empty rejectionReason; state why it is not core.", label),
				Location: loc,
			})
		}
	}
	return out
}

// canonicalGlossaryCategories is the closed Four-Questions category set (ch. 4).
var canonicalGlossaryCategories = map[string]bool{"Who": true, "What": true, "How": true, "Where": true}

// glossaryFourQFindings — GLOSS-FOURQ. WARNING coverage: at least one term should cover
// each of Who / What / How / Where. ERROR: a term whose category is not one of the four
// canonical values.
func glossaryFourQFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindGlossary {
		return nil
	}
	g, ok := draft.(*projectstate.Glossary)
	if !ok || g == nil {
		return nil
	}
	var out []Finding
	counts := map[string]int{}
	for i, it := range g.Items {
		cat := strings.TrimSpace(it.Category)
		if !canonicalGlossaryCategories[cat] {
			out = append(out, Finding{
				RuleID:   "GLOSS-FOURQ",
				Severity: SeverityError,
				Message:  fmt.Sprintf("glossary term %q has non-canonical category %q; use one of Who|What|How|Where.", it.Term, it.Category),
				Location: &Location{Ordinal: int64(i), Section: "glossary term " + it.Term},
			})
			continue
		}
		counts[cat]++
	}
	for _, cat := range []string{"Who", "What", "How", "Where"} {
		if counts[cat] == 0 {
			out = append(out, Finding{
				RuleID:   "GLOSS-FOURQ",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("no glossary term covers the %q question; the Four Questions each want at least one term.", cat),
				Location: &Location{Section: "glossary"},
			})
		}
	}
	return out
}

// scrubbedIDFindings — SR-ID-UNIQUE (error). Every scrubbed requirement must carry a
// non-empty, unique id and a non-empty statement.
func scrubbedIDFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindScrubbedRequirements {
		return nil
	}
	sr, ok := draft.(*projectstate.ScrubbedRequirements)
	if !ok || sr == nil {
		return nil
	}
	var out []Finding
	seen := map[string]bool{}
	for i, it := range sr.Items {
		id := strings.TrimSpace(it.ID)
		loc := &Location{Ordinal: int64(i), Section: fmt.Sprintf("requirement %d", i+1)}
		if id == "" {
			out = append(out, Finding{
				RuleID:   "SR-ID-UNIQUE",
				Severity: SeverityError,
				Message:  fmt.Sprintf("scrubbed requirement %d has an empty id; every requirement needs a stable non-empty id.", i+1),
				Location: loc,
			})
		} else if seen[id] {
			out = append(out, Finding{
				RuleID:   "SR-ID-UNIQUE",
				Severity: SeverityError,
				Message:  fmt.Sprintf("scrubbed requirement id %q is duplicated; requirement ids must be unique.", id),
				Location: loc,
			})
		} else {
			seen[id] = true
		}
		if strings.TrimSpace(it.Statement) == "" {
			out = append(out, Finding{
				RuleID:   "SR-ID-UNIQUE",
				Severity: SeverityError,
				Message:  fmt.Sprintf("scrubbed requirement %q has an empty statement.", it.ID),
				Location: loc,
			})
		}
	}
	return out
}

// opcCanonicalTopics maps a canonical ch.5 operational-concept topic to the substrings
// that evidence it appears among decisions[].topic.
var opcCanonicalTopics = []struct {
	name  string
	needs []string
}{
	{"topology", []string{"topology"}},
	{"sync/queued", []string{"sync", "queued"}},
	{"layering style", []string{"layering"}},
	{"state handling", []string{"state"}},
}

// opcTopicFindings — OPC-TOPIC-COVERAGE (info). Nudge when a canonical ch.5 topic
// (topology, sync/queued, layering style, state handling) is absent from decisions[].topic.
func opcTopicFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindOperationalConcepts {
		return nil
	}
	op, ok := draft.(*projectstate.OperationalConcepts)
	if !ok || op == nil {
		return nil
	}
	var topics []string
	for _, d := range op.Decisions {
		topics = append(topics, strings.ToLower(d.Topic))
	}
	joined := strings.Join(topics, " | ")
	var out []Finding
	for _, t := range opcCanonicalTopics {
		covered := false
		for _, need := range t.needs {
			if strings.Contains(joined, need) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, Finding{
				RuleID:   "OPC-TOPIC-COVERAGE",
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("no operational-concept decision addresses %q; ch.5 expects topology, sync/queued, layering style, and state handling to be decided.", t.name),
				Location: &Location{Section: "operational concepts"},
			})
		}
	}
	return out
}

// ---- small shared helpers ----

func componentDisplayLabel(c projectstate.Component, i int) string {
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	if strings.TrimSpace(c.ID) != "" {
		return c.ID
	}
	return fmt.Sprintf("component %d", i+1)
}

func dvLabel(dv projectstate.DynamicView, i int) string {
	if strings.TrimSpace(dv.Title) != "" {
		return dv.Title
	}
	if strings.TrimSpace(dv.Key) != "" {
		return dv.Key
	}
	if strings.TrimSpace(dv.UseCaseID) != "" {
		return dv.UseCaseID
	}
	return fmt.Sprintf("dynamic view %d", i+1)
}

func modeWire(m projectstate.CallMode) string {
	b, err := m.MarshalJSON()
	if err != nil {
		return fmt.Sprintf("mode(%d)", int(m))
	}
	return strings.Trim(string(b), `"`)
}
