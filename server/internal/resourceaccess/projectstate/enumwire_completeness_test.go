package projectstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// enumwire_completeness_test.go closes the F81-class hazard: projectstate carries
// 14 closed ordinal enums that marshal to STRING wire names via a hand-maintained
// (ordinal -> name) map/switch — the 13 (ordinal -> name) tables in enumjson.go
// plus ArtifactKind's WireName() switch in identity.go. Adding a new const to one
// of these enums' iota block WITHOUT adding the matching map/switch entry compiles
// fine (Go does not check map/switch exhaustiveness against an iota block) and
// fails only at RUNTIME, the first time that ordinal crosses the wire, with
// "projectstate: <Enum>(<n>) has no wire name" (see marshalEnum in enumjson.go and
// ArtifactKind.MarshalJSON in identity.go). Previously that failure mode was only
// ever caught by hitting it live.
//
// This test closes the hole by checking BIDIRECTIONAL completeness, for each of
// the 14 enums, between the Go wire-name map/switch and the CONTRACT's declared
// `enum` ordinal list — read live from the committed .aiarch/state/project.json's
// serviceContracts.projectStateAccess.$defs, not a hardcoded golden list, so the
// test tracks the contract as it evolves:
//
//   - Direction 1 (map is BEHIND the contract): every ordinal the contract
//     declares must marshal successfully. A declared ordinal with no map entry
//     is exactly the F81 hazard this test exists to close.
//   - Direction 2 (map is AHEAD of the contract): the wire map/registry must not
//     carry MORE entries than the contract declares (2a), and no ordinal outside
//     the declared set, probed across a window past the max declared value, may
//     marshal successfully (2b). Either symptom means a map entry exists for an
//     ordinal the contract doesn't know about — drift in the other direction.
//
// $def location note: all 14 enums' declared ordinal lists were verified to live
// directly in serviceContracts.projectStateAccess.$defs — none needed the
// const-block fallback, and none needed a search in another component's contract.
// That's somewhat notable given ActivityType and TestingVariant are read across
// component boundaries (constructionManager, etc.) at runtime: their canonical
// $def still sits with projectStateAccess, the owning RA, like the other 12.
func TestEnumWireMap_BidirectionalCompletenessVsContract(t *testing.T) {
	declared := loadDeclaredEnumOrdinals(t)

	checks := []struct {
		name    string
		mapSize int
		marshal func(ordinal int) ([]byte, error)
	}{
		{"Axis", len(axisNames), func(o int) ([]byte, error) { return json.Marshal(Axis(o)) }},
		{"CheckStatus", len(checkStatusNames), func(o int) ([]byte, error) { return json.Marshal(CheckStatus(o)) }},
		{"ComponentKind", len(componentKindNames), func(o int) ([]byte, error) { return json.Marshal(ComponentKind(o)) }},
		{"Layer", len(layerNames), func(o int) ([]byte, error) { return json.Marshal(Layer(o)) }},
		{"CallMode", len(callModeNames), func(o int) ([]byte, error) { return json.Marshal(CallMode(o)) }},
		{"Trigger", len(triggerNames), func(o int) ([]byte, error) { return json.Marshal(Trigger(o)) }},
		{"Classification", len(classificationNames), func(o int) ([]byte, error) { return json.Marshal(Classification(o)) }},
		{"ActivityNodeKind", len(activityNodeKindNames), func(o int) ([]byte, error) { return json.Marshal(ActivityNodeKind(o)) }},
		{"DeliveryStyle", len(deliveryStyleNames), func(o int) ([]byte, error) { return json.Marshal(DeliveryStyle(o)) }},
		{"DeploymentProfile", len(deploymentProfileNames), func(o int) ([]byte, error) { return json.Marshal(DeploymentProfile(o)) }},
		{"EdgeKind", len(edgeKindNames), func(o int) ([]byte, error) { return json.Marshal(EdgeKind(o)) }},
		{"ActivityType", len(activityTypeNames), func(o int) ([]byte, error) { return json.Marshal(ActivityType(o)) }},
		{"TestingVariant", len(testingVariantNames), func(o int) ([]byte, error) { return json.Marshal(TestingVariant(o)) }},
		// ArtifactKind has no exposed name->ordinal map (WireName is a switch, not a
		// table) — AllArtifactKinds() is its authoritative enumeration, and its
		// length stands in for "map size" for the Direction-2a check.
		{"ArtifactKind", len(AllArtifactKinds()), func(o int) ([]byte, error) { return json.Marshal(ArtifactKind(o)) }},
	}

	for _, c := range checks {
		c := c
		t.Run(c.name, func(t *testing.T) {
			ords, ok := declared[c.name]
			if !ok || len(ords) == 0 {
				t.Fatalf("%s: no declared `enum` ordinal list found in "+
					"serviceContracts.projectStateAccess.$defs — cannot verify wire-map "+
					"completeness against the contract", c.name)
			}
			verifyEnumWireCompleteness(t, c.name, ords, c.mapSize, c.marshal)
		})
	}
}

// verifyEnumWireCompleteness runs both completeness directions for one enum: every
// contract-declared ordinal must marshal (direction 1), and the wire-name
// map/registry must carry no more entries — nor marshal any ordinal outside the
// declared set — than the contract declares (direction 2). Factored out of
// TestEnumWireMap_BidirectionalCompletenessVsContract to keep that function's
// cognitive complexity within the repo's gocognit/gocyclo gate.
func verifyEnumWireCompleteness(t *testing.T, name string, declaredOrds []int, mapSize int, marshal func(int) ([]byte, error)) {
	t.Helper()

	declaredSet := make(map[int]bool, len(declaredOrds))
	maxDeclared := 0
	for _, o := range declaredOrds {
		declaredSet[o] = true
		if o > maxDeclared {
			maxDeclared = o
		}
	}

	// Direction 1: every ordinal the CONTRACT declares must marshal successfully
	// via the Go wire-name map/switch.
	for _, o := range declaredOrds {
		if _, err := marshal(o); err != nil {
			t.Errorf("F81 hazard: %s ordinal %d is declared in the contract's enum "+
				"list but has NO wire-name map entry (marshal error: %v) — a const was "+
				"added to the iota block without a matching wire-name map/switch entry "+
				"in enumjson.go/identity.go; this compiles fine and fails only at "+
				"runtime, the first time this value crosses the wire", name, o, err)
		}
	}

	// Direction 2a: the wire-name map/registry must not carry MORE entries than
	// the contract declares.
	if mapSize != len(declaredOrds) {
		drift := "behind"
		if mapSize > len(declaredOrds) {
			drift = "ahead of"
		}
		t.Errorf("F81 hazard (reverse): %s wire-name map/registry has %d entries but "+
			"the contract declares %d ordinals — the map has drifted %s the contract",
			name, mapSize, len(declaredOrds), drift)
	}

	// Direction 2b: no ordinal OUTSIDE the declared set — probed across
	// [0, maxDeclared+5] — may marshal successfully. A success here means the
	// wire-name map carries an entry for an ordinal the contract doesn't know
	// about.
	for probe := 0; probe <= maxDeclared+5; probe++ {
		if declaredSet[probe] {
			continue
		}
		if _, err := marshal(probe); err == nil {
			t.Errorf("F81 hazard (reverse): %s ordinal %d is NOT in the contract's "+
				"declared enum list but marshals successfully — the wire-name map has "+
				"an entry the contract doesn't declare", name, probe)
		}
	}
}

// contractDef is the slice of a $defs entry this test reads: its declared `enum`
// list, if any. Kept as raw messages because some $defs enums are string-backed
// (e.g. ActivityMethodPhase, CritiqueVerdict) rather than the integer ordinals
// this test covers — those are filtered out in loadDeclaredEnumOrdinals.
type contractDef struct {
	Enum []json.RawMessage `json:"enum"`
}

// loadDeclaredEnumOrdinals reads the repo's committed .aiarch/state/project.json
// and returns, for every $def under serviceContracts.projectStateAccess that
// declares a purely-integer `enum` list, its name -> declared ordinal list.
// Non-integer (string-backed) enums are skipped: they use a different wire
// encoding and are out of scope for the F81 ordinal-drift hazard this test
// closes.
func loadDeclaredEnumOrdinals(t *testing.T) map[string][]int {
	t.Helper()
	root := findRepoRootFromCwd(t)
	raw, err := os.ReadFile(filepath.Join(root, ".aiarch", "state", "project.json"))
	if err != nil {
		t.Fatalf("read project.json: %v", err)
	}

	var top struct {
		ServiceContracts map[string]struct {
			Defs map[string]contractDef `json:"$defs"`
		} `json:"serviceContracts"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("parse project.json: %v", err)
	}
	psa, ok := top.ServiceContracts["projectStateAccess"]
	if !ok || len(psa.Defs) == 0 {
		t.Fatal("serviceContracts.projectStateAccess.$defs missing or empty in project.json")
	}

	out := make(map[string][]int, len(psa.Defs))
	for name, def := range psa.Defs {
		if len(def.Enum) == 0 {
			continue
		}
		ords := make([]int, 0, len(def.Enum))
		allInt := true
		for _, r := range def.Enum {
			var n int
			if err := json.Unmarshal(r, &n); err != nil {
				allInt = false
				break
			}
			ords = append(ords, n)
		}
		if !allInt {
			continue // string-backed enum — different wire encoding, out of scope
		}
		out[name] = ords
	}
	return out
}

// findRepoRootFromCwd ascends from the test's working directory to the directory
// holding `.aiarch/state/project.json` (the repo root). Mirrors the identical
// helper in server/internal/contract_defs_test.go (package internal_test);
// duplicated here (rather than shared) because that helper lives in a different
// package/build unit and this test stays dependency-free within projectstate's
// own test suite.
func findRepoRootFromCwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".aiarch", "state", "project.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (.aiarch/state/project.json) ascending from %s", dir)
		}
		dir = parent
	}
}
