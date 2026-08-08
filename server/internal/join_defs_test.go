package internal_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// join_defs_test.go is the HARD CHECK that the .serviceContracts corpus never
// drifts away from the committed systemDesign (slot 5) architecture. Every BUILT
// contract entry (a non-empty goPackage, the same selection cmd/modelgen makes)
// must JOIN — case-folded + stereotype-suffix-normalized, and layer-scoped — to a
// slot-5 component of kind manager | engine | resourceAccess | utility whose
// `encapsulates` is non-empty. Utility joined the buildable set on 2026-08-01 with
// the messageBus fold: MOST utilities are external (Security/Logging/Diagnostics
// carry buildStatus "external" and no contract, so they never reach this check),
// but a utility CAN be app code with a generated contract, and when it is, the
// same no-drift guarantee must hold. A contract joins in one of two ratified shapes: directly, when
// its KEY normalizes to a component id; or as a contract FACET, when its key names
// no component but its `component` field joins an owning component (the ratified
// resource-access facet doctrine — one component, e.g. projectStateAccess, that
// publishes several cohesive contract facets living in its one package). A facet
// joins on the SAME kind, so it must share its owner's layer. This is the
// executable guard against:
//
//   - a contract key that names no architecture component (a junk/orphan entry
//     like the deleted "operationsRead-ruling"), and
//   - a contract key whose normalized name skews from its slot-5 component id
//     (e.g. "constructionEstimationEngine" vs the "estimation-engine" component —
//     the rename to "estimationEngine" is what makes this green), and
//   - a build target pointed at a component with no encapsulated volatility (an
//     architecture leaf that carries no Method encapsulation is not buildable).
//
// The name normalization MIRRORS framework-go's methodcheck.StereotypeSuffixNormalizer
// (lowercase + strip non-alnum, then strip ONE trailing access|engine|manager|client
// suffix) so this untagged check and the methoddesign alignment gate agree on the
// join. It is replicated here (rather than imported) so the check runs in the DEFAULT
// `go test ./...` build, exactly like TestEveryBuiltContractHasModels next to it.

// joinContractEntry is the slice of a .serviceContracts entry this check reads.
type joinContractEntry struct {
	Component string `json:"component"`
	Layer     string `json:"layer"`
	GoPackage string `json:"goPackage"`
}

// slot5Component is the slice of a slots["5"].model.components entry this check reads.
type slot5Component struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Encapsulates string `json:"encapsulates"`
}

// contractLayerToKind maps a ServiceContract.Layer (Method-cased) to the slot-5
// component kind it must join to. Only the buildable layers are in the map; a
// built contract in any other layer (Client, Resource) is itself a failure
// (reported below).
var contractLayerToKind = map[string]string{
	"Manager":        "manager",
	"Engine":         "engine",
	"ResourceAccess": "resourceAccess",
	"Utility":        "utility",
}

func TestEveryBuiltContractJoinsAComponent(t *testing.T) {
	repoRoot := findRepoRootFromCwd(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".aiarch", "state", "project.json"))
	if err != nil {
		t.Fatalf("read project.json: %v", err)
	}
	var top struct {
		ServiceContracts map[string]joinContractEntry `json:"serviceContracts"`
		Slots            map[string]struct {
			Model struct {
				Components []slot5Component `json:"components"`
			} `json:"model"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("parse project.json: %v", err)
	}
	if len(top.ServiceContracts) == 0 {
		t.Fatal("no .serviceContracts in project.json")
	}
	comps := top.Slots["5"].Model.Components
	if len(comps) == 0 {
		t.Fatal("no slot-5 (systemDesign) components in project.json")
	}

	// Index the buildable slot-5 components by (normalized-id, kind). Only
	// manager/engine/resourceAccess/utility kinds are join targets; a
	// same-normalized-name resource (e.g. the "settlement-state" resource vs the
	// settlementState RA) must NOT satisfy a contract, so the layer is part of the key.
	index := map[joinKey]slot5Component{}
	// byName indexes the same buildable components by normalized id alone. It is
	// used ONLY to sharpen the failure message on the facet path: it lets the check
	// say "owner exists but at the wrong layer" instead of "owner does not exist".
	byName := map[string]slot5Component{}
	for _, c := range comps {
		switch c.Kind {
		case "manager", "engine", "resourceAccess", "utility":
			index[joinKey{normalizeComponentName(c.ID), c.Kind}] = c
			byName[normalizeComponentName(c.ID)] = c
		}
	}

	keys := make([]string, 0, len(top.ServiceContracts))
	for k := range top.ServiceContracts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	built := 0
	for _, k := range keys {
		e := top.ServiceContracts[k]
		if e.GoPackage == "" {
			continue // stub / non-built entry — no build target, like modelgen skips
		}
		built++
		checkContractJoin(t, k, e, index, byName)
	}
	if built == 0 {
		t.Fatal("no built (goPackage) service contracts found — the check verified nothing")
	}
}

// joinKey addresses a buildable slot-5 component by its normalized id AND its
// kind. The layer is part of the key on purpose: a same-normalized-name resource
// (the "settlement-state" resource vs the settlementState RA) must NOT satisfy a
// contract.
type joinKey struct{ name, kind string }

// checkContractJoin verifies that ONE built service contract joins a slot-5
// component — by its own key, or (for a contract FACET) through its `component`
// field — and that the component it lands on encapsulates something.
//
// Split out of TestEveryBuiltContractJoinsAComponent so the test body reads as
// read → index → check-each, with the join rules and their sharpened failure
// messages in one place.
func checkContractJoin(t *testing.T, k string, e joinContractEntry, index map[joinKey]slot5Component, byName map[string]slot5Component) {
	t.Helper()
	kind, ok := contractLayerToKind[e.Layer]
	if !ok {
		t.Errorf("%s: built contract has layer %q — only Manager/Engine/ResourceAccess/Utility are buildable, joinable layers", k, e.Layer)
		return
	}
	comp, found := index[joinKey{normalizeComponentName(k), kind}]
	if !found {
		// The key names no component. This is legal for a contract FACET: a
		// contract whose key is not a component id but whose `component` field
		// joins an owning component (ratified resource-access facet doctrine —
		// one component publishes several cohesive contract facets, all in the
		// one package). The facet join is keyed on the SAME kind, so a facet
		// that crosses layers (owner of a different kind) does NOT satisfy this
		// lookup — enforcing the "a facet shares its owner's layer" rule.
		comp, found = index[joinKey{normalizeComponentName(e.Component), kind}]
	}
	if !found {
		// Sharpen the failure: distinguish a fossil entry (no owner at all)
		// from a facet whose owner exists but sits at a different layer.
		if owner, exists := byName[normalizeComponentName(e.Component)]; exists && e.Component != "" {
			t.Errorf("%s: contract facet declares layer %q (kind %q) but its owning component %q (via component field %q) is kind %q — a contract facet must share its owning component's layer",
				k, e.Layer, kind, owner.ID, e.Component, owner.Kind)
		} else {
			t.Errorf("%s: built %s contract joins no slot-5 component — its key (normalized %q) names no component, and its component field %q names no component either (orphan/junk entry, or a contract-key↔component-id naming skew)",
				k, e.Layer, normalizeComponentName(k), e.Component)
		}
		return
	}
	if strings.TrimSpace(comp.Encapsulates) == "" {
		t.Errorf("%s: built contract joins slot-5 component %q but that component encapsulates nothing — an architecture leaf with no Method encapsulation is not buildable", k, comp.ID)
	}
}

// normalizeComponentName mirrors framework-go methodcheck.StereotypeSuffixNormalizer:
// lowercase + strip every non-alphanumeric rune, then strip exactly ONE trailing
// Method stereotype suffix (access | engine | manager | client) when a non-empty
// remainder is left. So "estimationEngine", the "estimation-engine" component id, and
// an "EstimationEngine" all reduce to "estimation"; "SettlementState"/"settlement-state"
// reduce to "settlementstate" (no suffix stripped). A name whose whole value IS a bare
// suffix ("Manager") is left intact.
func normalizeComponentName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	base := b.String()
	for _, suf := range []string{"access", "engine", "manager", "client"} {
		if len(base) > len(suf) && strings.HasSuffix(base, suf) {
			return base[:len(base)-len(suf)]
		}
	}
	return base
}
