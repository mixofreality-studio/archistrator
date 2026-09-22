package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ---- fixture --------------------------------------------------------------------------
//
// A miniature architecture that exercises every evidence path and every failing
// condition once:
//
//	U-SPA-web-client  client, contract without a goPackage          -> condition 2
//	C-alpha-manager   manager, implemented                          -> qualifies (code)
//	C-beta-engine     engine, no contractKey                        -> condition 1
//	C-gamma-access    RA with a facet, own interface implemented    -> qualifies (code, facet rule)
//	C-delta-access    RA, stub: true                                -> condition 2
//	R-gamma-store     resource reached by gamma-access              -> qualifies (inferred)
//	R-delta-gateway   resource reached by delta-access              -> its RA fails
//	R-orphan          resource no RA reaches                        -> nothing to infer from
//	N-STP             componentless, founder sign-off, plan present -> qualifies (sign-off)
//	N-IT              componentless, no sign-off                    -> no evidence path

const fixtureHead = "0123456789abcdef0123456789abcdef01234567"

var (
	alphaOps = []string{"OpenAlpha", "CloseAlpha"}
	gammaOps = []string{"ReadGamma", "WriteGamma", "ListGamma"}
	deltaOps = []string{"ChargeDelta"}
)

func strp(s string) *string { return &s }

func contractFor(component, goPackage, iface string, ops ...string) projectstate.ServiceContract {
	sc := projectstate.ServiceContract{Component: component, GoPackage: goPackage, Title: component + " contract"}
	sc.Interface.Name = iface
	for _, op := range ops {
		sc.Interface.Operations = append(sc.Interface.Operations, projectstate.ContractOperation{Name: op})
	}
	return sc
}

func fixtureProject() projectstate.Project {
	sys := &projectstate.System{
		Components: []projectstate.Component{
			{ID: "web-client", Name: "WebClient", Encapsulates: "a fixture volatility", Kind: projectstate.CompClient, Layer: projectstate.LayerClient, ContractKey: strp("webClient")},
			{ID: "alpha-manager", Name: "AlphaManager", Encapsulates: "a fixture volatility", Kind: projectstate.CompManager, Layer: projectstate.LayerManager, ContractKey: strp("alphaManager")},
			{ID: "beta-engine", Name: "BetaEngine", Encapsulates: "a fixture volatility", Kind: projectstate.CompEngine, Layer: projectstate.LayerEngine},
			{ID: "gamma-access", Name: "GammaAccess", Encapsulates: "a fixture volatility", Kind: projectstate.CompResourceAccess, Layer: projectstate.LayerResourceAccess, ContractKey: strp("gammaAccess")},
			{ID: "delta-access", Name: "DeltaAccess", Encapsulates: "a fixture volatility", Kind: projectstate.CompResourceAccess, Layer: projectstate.LayerResourceAccess, ContractKey: strp("deltaAccess")},
			{ID: "gamma-store", Name: "GammaStore", Encapsulates: "a fixture volatility", Kind: projectstate.CompResource, Layer: projectstate.LayerResource},
			{ID: "delta-gateway", Name: "DeltaGateway", Encapsulates: "a fixture volatility", Kind: projectstate.CompResource, Layer: projectstate.LayerResource},
			{ID: "orphan", Name: "Orphan", Encapsulates: "a fixture volatility", Kind: projectstate.CompResource, Layer: projectstate.LayerResource},
		},
		Relationships: []projectstate.Relationship{
			{From: "web-client", To: "alpha-manager"},
			{From: "alpha-manager", To: "gamma-access"},
			{From: "alpha-manager", To: "delta-access"},
			{From: "gamma-access", To: "gamma-store"},
			{From: "delta-access", To: "delta-gateway"},
			{From: "alpha-manager", To: "orphan"}, // a Manager edge is not a ResourceAccess edge.
		},
	}
	list := &projectstate.ActivityList{Activities: []projectstate.ActivityItem{
		{Name: "U-SPA-web-client", WorkerClass: "junior-developer", Coding: true, ComponentID: "web-client"},
		{Name: "C-alpha-manager", WorkerClass: "junior-developer", Coding: true, ComponentID: "alpha-manager"},
		{Name: "C-beta-engine", WorkerClass: "junior-developer", Coding: true, ComponentID: "beta-engine"},
		{Name: "C-gamma-access", WorkerClass: "junior-developer", Coding: true, ComponentID: "gamma-access"},
		{Name: "C-delta-access", WorkerClass: "junior-developer", Coding: true, ComponentID: "delta-access"},
		{Name: "R-gamma-store", WorkerClass: "senior-developer", ComponentID: "gamma-store"},
		{Name: "R-delta-gateway", WorkerClass: "senior-developer", ComponentID: "delta-gateway"},
		{Name: "R-orphan", WorkerClass: "senior-developer", ComponentID: "orphan"},
		{Name: "N-STP", WorkerClass: "test-engineer"},
		{Name: "N-IT", WorkerClass: "software-tester"},
	}}
	delta := contractFor("deltaAccess", "internal/resourceaccess/delta", "DeltaAccess", deltaOps...)
	delta.Stub = true
	p := projectstate.Project{ID: "fixture", Version: 3}
	p.SystemDesign = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: sys, Revisions: 1}
	p.ActivityList = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: list, Revisions: 1}
	p.ServiceContracts = map[string]projectstate.ServiceContract{
		"webClient":        contractFor("webClient", "", "WebClient", "Render"),
		"alphaManager":     contractFor("alphaManager", "internal/manager/alpha", "AlphaManager", alphaOps...),
		"gammaAccess":      contractFor("gammaAccess", "internal/resourceaccess/gamma", "GammaAccess", gammaOps...),
		"gammaFacetAccess": contractFor("gammaAccess", "internal/resourceaccess/gamma", "GammaFacetAccess", "FacetOnly"),
		"deltaAccess":      delta,
	}
	p.TestingState = &projectstate.TestingState{SystemTestPlan: &projectstate.SystemTestPlan{
		Scenarios: []projectstate.TestScenario{{ID: "STP-1"}, {ID: "STP-2"}, {ID: "STP-3"}},
	}}
	// The slot-10 network. By default every QUALIFYING activity depends only on qualifying,
	// integrated activities (or on M0), so the qualifying set is fully Done; the
	// integration tests below re-point edges at the non-qualifiers.
	p.Network = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 1, Model: &projectstate.Network{
		Dependencies: []projectstate.NetworkDependency{
			{Activity: "U-SPA-web-client", DependsOn: []string{"C-alpha-manager"}},
			{Activity: "C-alpha-manager", DependsOn: []string{"C-gamma-access"}},
			{Activity: "C-beta-engine", DependsOn: []string{"M0"}},
			{Activity: "C-gamma-access", DependsOn: []string{"R-gamma-store"}},
			{Activity: "C-delta-access", DependsOn: []string{"R-delta-gateway"}},
			{Activity: "R-gamma-store", DependsOn: []string{"M0"}},
			{Activity: "R-delta-gateway", DependsOn: []string{"M0"}},
			{Activity: "R-orphan", DependsOn: []string{"M0"}},
			{Activity: "N-STP", DependsOn: []string{"M0"}},
			{Activity: "N-IT", DependsOn: []string{"N-STP", "U-SPA-web-client"}},
		},
		Milestones: []projectstate.NetworkMilestone{
			{ID: "M0", Name: "SDP Review Approved", Public: true},
			{ID: "M1", Name: "Infrastructure Provisioned", DependsOn: []string{"R-delta-gateway", "R-gamma-store"}},
		},
	}}
	return p
}

// implSource is a hand-written Go file whose receiver recv has one method per op.
func implSource(pkg, recv string, ops ...string) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\ntype " + recv + " struct{}\n")
	for _, op := range ops {
		b.WriteString("\nfunc (r *" + recv + ") " + op + "() error { return nil }\n")
	}
	return b.String()
}

// methodsSource is implSource without the type declaration: the methods only, for a
// receiver declared in another file of the package.
func methodsSource(pkg, recv string, ops ...string) string {
	return strings.Replace(implSource(pkg, recv, ops...), "type "+recv+" struct{}\n", "", 1)
}

// componentOf returns the fixture System's component id, for a test to edit in place.
func componentOf(t *testing.T, p *projectstate.Project, id string) *projectstate.Component {
	t.Helper()
	sys := p.SystemDesign.Model.(*projectstate.System)
	for i := range sys.Components {
		if sys.Components[i].ID == id {
			return &sys.Components[i]
		}
	}
	t.Fatalf("fixture has no component %q", id)
	return nil
}

// fieldedImplSource is implSource with a receiver that holds a field — the shape of a
// ResourceAccess that binds its Resource (3b).
func fieldedImplSource(pkg, recv string, ops ...string) string {
	return strings.Replace(implSource(pkg, recv, ops...), "type "+recv+" struct{}", "type "+recv+" struct{ root string }", 1)
}

const generatedHeader = "// Code generated by modelgen. DO NOT EDIT.\n\n"

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fixtureServer writes the server tree the fixture's contracts point at. gamma has NO
// file for its facet interface — the facet rule must not ask for one.
func fixtureServer(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "internal/manager/alpha/contract.gen.go", generatedHeader+"package alpha\n")
	writeFile(t, root, "internal/manager/alpha/alphamanager.go", implSource("alpha", "alphaManager", alphaOps...))
	writeFile(t, root, "internal/resourceaccess/gamma/contract.gen.go", generatedHeader+"package gamma\n")
	writeFile(t, root, "internal/resourceaccess/gamma/gammaaccess.go", fieldedImplSource("gamma", "gitGammaAccess", gammaOps...))
	writeFile(t, root, "internal/resourceaccess/delta/contract.gen.go", generatedHeader+"package delta\n")
	writeFile(t, root, "internal/resourceaccess/delta/deltaaccess.go", implSource("delta", "notConfigured", deltaOps...))
	return root
}

func evaluateFixture(t *testing.T, p projectstate.Project, root string) map[string]verdict {
	t.Helper()
	vs, err := evaluate(inputs{Project: p, ServerRoot: root, Head: fixtureHead})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	out := make(map[string]verdict, len(vs))
	for _, v := range vs {
		out[v.ActivityID] = v
	}
	return out
}

func qualifyingSet(vs map[string]verdict) []string {
	var out []string
	for id, v := range vs {
		if v.Qualifies {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// ---- the qualifying set ---------------------------------------------------------------

// The exact set, asserted as a SET so an activity that starts qualifying by accident
// fails as loudly as one that stops. Each non-qualifier is pinned to its failing
// condition, not just to "no".
func TestEvaluate_ExactQualifyingSetOnTheFixture(t *testing.T) {
	vs := evaluateFixture(t, fixtureProject(), fixtureServer(t))

	want := []string{"C-alpha-manager", "C-gamma-access", "N-STP", "R-gamma-store"}
	if got := qualifyingSet(vs); !reflect.DeepEqual(got, want) {
		t.Fatalf("qualifying set = %v, want %v", got, want)
	}
	failing := map[string]string{
		"U-SPA-web-client": "condition 2: serviceContracts[webClient] has no goPackage",
		"C-beta-engine":    "condition 1",
		"C-delta-access":   "condition 2: serviceContracts[deltaAccess] is stub: true",
		"R-delta-gateway":  "its ResourceAccess C-delta-access does not qualify",
		"R-orphan":         "no ResourceAccess has a slot-5 relationship",
		"N-IT":             "no founder sign-off is recorded",
	}
	for id, reason := range failing {
		if v := vs[id]; !strings.Contains(v.Reason, reason) {
			t.Errorf("%s: reason = %q, want it to name %q", id, v.Reason, reason)
		}
	}
	if len(vs) != 10 {
		t.Errorf("evaluated %d activities, want every one of the plan's 10", len(vs))
	}
}

// ---- the three conditions -------------------------------------------------------------

func TestFullyImplemented_EachConditionFailsTheComponent(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(p *projectstate.Project, root string)
		subject string
		want    string
	}{
		{
			name: "condition 1 — a contractKey with no .serviceContracts entry",
			mutate: func(p *projectstate.Project, _ string) {
				delete(p.ServiceContracts, "alphaManager")
			},
			subject: "C-alpha-manager", want: "condition 1: no .serviceContracts entry has component \"alphaManager\"",
		},
		{
			name: "condition 1 — only facets, no entry for the component's own interface",
			mutate: func(p *projectstate.Project, _ string) {
				delete(p.ServiceContracts, "gammaAccess")
			},
			subject: "C-gamma-access", want: "condition 1: .serviceContracts has no entry for the component's own interface",
		},
		{
			name: "condition 2 — a facet with no goPackage",
			mutate: func(p *projectstate.Project, _ string) {
				sc := p.ServiceContracts["gammaFacetAccess"]
				sc.GoPackage = ""
				p.ServiceContracts["gammaFacetAccess"] = sc
			},
			subject: "C-gamma-access", want: "condition 2: serviceContracts[gammaFacetAccess] has no goPackage",
		},
		{
			name: "condition 2 — a stub facet fails the component it belongs to",
			mutate: func(p *projectstate.Project, _ string) {
				sc := p.ServiceContracts["gammaFacetAccess"]
				sc.Stub = true
				p.ServiceContracts["gammaFacetAccess"] = sc
			},
			subject: "C-gamma-access", want: "condition 2: serviceContracts[gammaFacetAccess] is stub: true",
		},
		{
			name: "condition 3 — no contract.gen.go",
			mutate: func(_ *projectstate.Project, root string) {
				_ = os.Remove(filepath.Join(root, "internal/manager/alpha/contract.gen.go"))
			},
			subject: "C-alpha-manager", want: "condition 3: server/internal/manager/alpha/contract.gen.go",
		},
		{
			name: "condition 3 — no hand-written <lowercase interface>.go",
			mutate: func(_ *projectstate.Project, root string) {
				_ = os.Remove(filepath.Join(root, "internal/manager/alpha/alphamanager.go"))
			},
			subject: "C-alpha-manager", want: "condition 3: server/internal/manager/alpha/alphamanager.go",
		},
		{
			name: "condition 3 — the file is generated, not hand-written",
			mutate: func(_ *projectstate.Project, root string) {
				writeFile(t, root, "internal/manager/alpha/alphamanager.go", generatedHeader+implSource("alpha", "alphaManager", alphaOps...))
			},
			subject: "C-alpha-manager", want: "is generated",
		},
		{
			name: "condition 3 — one contract operation has no method",
			mutate: func(_ *projectstate.Project, root string) {
				writeFile(t, root, "internal/manager/alpha/alphamanager.go", implSource("alpha", "alphaManager", alphaOps[0]))
			},
			subject: "C-alpha-manager", want: "no one receiver implements all 2 contract operations",
		},
		{
			name: "condition 3 — the operations are split across two receivers",
			mutate: func(_ *projectstate.Project, root string) {
				src := implSource("alpha", "halfOne", alphaOps[0]) + strings.TrimPrefix(implSource("alpha", "halfTwo", alphaOps[1]), "package alpha\n")
				writeFile(t, root, "internal/manager/alpha/alphamanager.go", src)
			},
			subject: "C-alpha-manager", want: "no one receiver implements all 2 contract operations",
		},
		{
			// The literal first reading of condition 3: a declared `type <Interface>Impl`.
			// A declaration with no methods implements nothing and must not qualify.
			name: "condition 3 — a declared <Interface>Impl with no methods is not an implementation",
			mutate: func(_ *projectstate.Project, root string) {
				writeFile(t, root, "internal/manager/alpha/alphamanager.go", "package alpha\n\ntype AlphaManagerImpl struct{}\n")
			},
			subject: "C-alpha-manager", want: "declares no method for any of the 2 contract operations",
		},
		{
			name: "condition 1 — an empty contractKey names nothing",
			mutate: func(p *projectstate.Project, _ string) {
				componentOf(t, p, "alpha-manager").ContractKey = strp("")
			},
			subject: "C-alpha-manager", want: "condition 1: component \"alpha-manager\" names no contractKey",
		},
		{
			// The entry filed under the component's key belongs to another component; only
			// the facet is grouped under it, so the component has no own interface to read.
			name: "condition 1 — the entry under the component's own key belongs to another component",
			mutate: func(p *projectstate.Project, _ string) {
				sc := p.ServiceContracts["gammaAccess"]
				sc.Component = "otherAccess"
				p.ServiceContracts["gammaAccess"] = sc
			},
			subject: "C-gamma-access", want: "condition 1: .serviceContracts has no entry for the component's own interface (key \"gammaAccess\")",
		},
		{
			name: "condition 2 — a facet in another goPackage",
			mutate: func(p *projectstate.Project, _ string) {
				sc := p.ServiceContracts["gammaFacetAccess"]
				sc.GoPackage = "internal/resourceaccess/elsewhere"
				p.ServiceContracts["gammaFacetAccess"] = sc
			},
			subject: "C-gamma-access",
			want:    "condition 2: serviceContracts[gammaFacetAccess] is in internal/resourceaccess/elsewhere, not the component's package internal/resourceaccess/gamma",
		},
		{
			// Every receiver has a method for each of zero operations. That is vacuous, and
			// it must be refused as evidence — alphamanager.go still has methods, so a
			// missing guard would qualify the component.
			name: "condition 3 — a contract with no operations is refused, not vacuously covered",
			mutate: func(p *projectstate.Project, _ string) {
				sc := p.ServiceContracts["alphaManager"]
				sc.Interface.Operations = nil
				p.ServiceContracts["alphaManager"] = sc
			},
			subject: "C-alpha-manager", want: "condition 3: serviceContracts[alphaManager] declares no operations",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, root := fixtureProject(), fixtureServer(t)
			c.mutate(&p, root)
			v := evaluateFixture(t, p, root)[c.subject]
			if v.Qualifies {
				t.Fatalf("%s qualified; want it refused (%s)", c.subject, c.want)
			}
			if !strings.Contains(v.Reason, c.want) {
				t.Errorf("reason = %q, want it to contain %q", v.Reason, c.want)
			}
		})
	}
}

// The FACET RULE: gamma's facet has no file of its own, and the component still
// qualifies — on its own interface — with a basis that cites every contract grouped under
// it. Demanding a file per facet would fail projectStateAccess, which is implemented.
func TestFullyImplemented_FacetInterfacesAreNotRequiredToHaveAFile(t *testing.T) {
	vs := evaluateFixture(t, fixtureProject(), fixtureServer(t))
	v := vs["C-gamma-access"]
	if !v.Qualifies {
		t.Fatalf("C-gamma-access did not qualify: %s", v.Reason)
	}
	for _, want := range []string{
		"serviceContracts[gammaAccess,gammaFacetAccess]",
		"server/internal/resourceaccess/gamma/contract.gen.go",
		"server/internal/resourceaccess/gamma/gammaaccess.go",
		"GammaAccess implemented by gitGammaAccess",
		"@ " + fixtureHead,
		founderRulingRef,
	} {
		if !strings.Contains(v.Basis, want) {
			t.Errorf("basis %q does not cite %q", v.Basis, want)
		}
	}
}

// The revenueLedgerAccess shape: the component's OWN entry is the stub and its facet is
// not. The facet being built does not rescue the component.
func TestFullyImplemented_AnOwnStubWithABuiltFacetStillFails(t *testing.T) {
	p, root := fixtureProject(), fixtureServer(t)
	own := p.ServiceContracts["gammaAccess"]
	own.Stub = true
	p.ServiceContracts["gammaAccess"] = own
	v := evaluateFixture(t, p, root)["C-gamma-access"]
	if v.Qualifies || !strings.Contains(v.Reason, "serviceContracts[gammaAccess] is stub: true") {
		t.Errorf("verdict = %+v, want refused on the component's own stub", v)
	}
}

// ---- condition 3b: a ResourceAccess binds a Resource ----------------------------------

// 3b (architect ruling, 2026-09-12): a ResourceAccess qualifies only when at least one
// COVERING receiver is a struct with a field, its declaration resolved anywhere in the
// package's non-test files. Each case rewrites gamma's package and says what it expects.
func TestFullyImplemented_AResourceAccessMustHoldAField(t *testing.T) {
	const pkg = "internal/resourceaccess/gamma/"
	cases := []struct {
		name  string
		files map[string]string // package file -> content; gammaaccess.go is always set
		// refused is the reason substring when the component must fail; "" means it qualifies.
		refused string
		// implemented is the basis's parenthetical when it qualifies.
		implemented string
		// cited is the exact file list when it qualifies.
		cited []string
	}{
		{
			name:    "the only covering receiver is an empty struct{}",
			files:   map[string]string{"gammaaccess.go": implSource("gamma", "gitGammaAccess", gammaOps...)},
			refused: "condition 3b: no covering receiver of this ResourceAccess (gitGammaAccess) is a struct with a field",
		},
		{
			name: "a fielded struct that covers nothing does not count",
			files: map[string]string{"gammaaccess.go": implSource("gamma", "gitGammaAccess", gammaOps...) +
				"\ntype gammaConfig struct{ root string }\n"},
			refused: "condition 3b",
		},
		{
			name: "a fielded declaration in a _test.go file does not count",
			files: map[string]string{
				"gammaaccess.go":      methodsSource("gamma", "gitGammaAccess", gammaOps...),
				"gammaaccess_test.go": "package gamma\n\ntype gitGammaAccess struct{ root string }\n",
			},
			refused: "condition 3b",
		},
		{
			name: "a live receiver beside an empty no-op: the fielded one is named",
			files: map[string]string{"gammaaccess.go": fieldedImplSource("gamma", "gitGammaAccess", gammaOps...) +
				strings.TrimPrefix(implSource("gamma", "noopGammaAccess", gammaOps...), "package gamma\n")},
			implemented: "(GammaAccess implemented by gitGammaAccess, noopGammaAccess; kind resourceAccess, bound by fielded gitGammaAccess)",
			cited:       []string{"server/" + pkg + "contract.gen.go", "server/" + pkg + "gammaaccess.go"},
		},
		{
			name: "declared in contract.gen.go (the GitArtifactAccess shape)",
			files: map[string]string{
				"contract.gen.go": generatedHeader + "package gamma\n\ntype GitGammaAccess struct{ repo string }\n",
				"gammaaccess.go":  methodsSource("gamma", "GitGammaAccess", gammaOps...),
			},
			implemented: "(GammaAccess implemented by GitGammaAccess; kind resourceAccess, bound by fielded GitGammaAccess)",
			cited:       []string{"server/" + pkg + "contract.gen.go", "server/" + pkg + "gammaaccess.go"},
		},
		{
			name: "declared in another hand-written file, which is then cited",
			files: map[string]string{
				"gammatypes.go":  "package gamma\n\nimport \"fmt\"\n\ntype gitGammaAccess struct{ fmt.Stringer }\n",
				"gammaaccess.go": methodsSource("gamma", "gitGammaAccess", gammaOps...),
			},
			implemented: "(GammaAccess implemented by gitGammaAccess; kind resourceAccess, bound by fielded gitGammaAccess)",
			cited:       []string{"server/" + pkg + "contract.gen.go", "server/" + pkg + "gammaaccess.go", "server/" + pkg + "gammatypes.go"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, root := fixtureProject(), fixtureServer(t)
			for file, content := range c.files {
				writeFile(t, root, pkg+file, content)
			}
			v := evaluateFixture(t, p, root)["C-gamma-access"]
			if c.refused != "" {
				if v.Qualifies || !strings.Contains(v.Reason, c.refused) {
					t.Fatalf("verdict = %+v, want refused with %q", v, c.refused)
				}
				return
			}
			if !v.Qualifies {
				t.Fatalf("C-gamma-access did not qualify: %s", v.Reason)
			}
			if !strings.Contains(v.Basis, c.implemented) {
				t.Errorf("basis = %q, want it to carry %q", v.Basis, c.implemented)
			}
			if !reflect.DeepEqual(v.Files, c.cited) {
				t.Errorf("cited files = %v, want %v", v.Files, c.cited)
			}
			for _, f := range c.cited {
				if !strings.Contains(v.Basis, f) {
					t.Errorf("basis %q does not cite %s", v.Basis, f)
				}
			}
		})
	}
}

// Engines and Managers are stateless by doctrine: an empty struct{} receiver qualifies
// them, 3b is not applied, and their basis carries no kind clause.
func TestFullyImplemented_EnginesAndManagersAreExemptFrom3b(t *testing.T) {
	p, root := fixtureProject(), fixtureServer(t)
	componentOf(t, &p, "beta-engine").ContractKey = strp("betaEngine")
	p.ServiceContracts["betaEngine"] = contractFor("betaEngine", "internal/engine/beta", "BetaEngine", "Compute")
	writeFile(t, root, "internal/engine/beta/contract.gen.go", generatedHeader+"package beta\n")
	writeFile(t, root, "internal/engine/beta/betaengine.go", implSource("beta", "betaEngine", "Compute"))

	vs := evaluateFixture(t, p, root)
	for _, id := range []string{"C-beta-engine", "C-alpha-manager"} {
		v := vs[id]
		if !v.Qualifies {
			t.Errorf("%s did not qualify on an empty struct{} receiver: %s", id, v.Reason)
			continue
		}
		if strings.Contains(v.Basis, "kind ") || strings.Contains(v.Basis, "fielded") {
			t.Errorf("%s: basis %q carries a 3b clause, but 3b does not apply to it", id, v.Basis)
		}
	}
}

// ---- the resource inference -----------------------------------------------------------

func TestInferredFromAccess_AResourceFollowsItsResourceAccess(t *testing.T) {
	vs := evaluateFixture(t, fixtureProject(), fixtureServer(t))
	v := vs["R-gamma-store"]
	if !v.Qualifies {
		t.Fatalf("R-gamma-store did not qualify: %s", v.Reason)
	}
	for _, want := range []string{"inferred, not read", "C-gamma-access", "serviceContracts[gammaAccess]", founderRulingRef} {
		if !strings.Contains(v.Basis, want) {
			t.Errorf("basis %q does not say %q", v.Basis, want)
		}
	}
	if v.CodeRef != "" || v.ContractRef != "" {
		t.Errorf("a Resource has no code or contract of its own to cite, got code=%q contract=%q", v.CodeRef, v.ContractRef)
	}

	// Once gamma-access stops qualifying, so does the resource it reaches.
	p, root := fixtureProject(), fixtureServer(t)
	_ = os.Remove(filepath.Join(root, "internal/resourceaccess/gamma/gammaaccess.go"))
	if v := evaluateFixture(t, p, root)["R-gamma-store"]; v.Qualifies {
		t.Error("R-gamma-store qualified although its only ResourceAccess no longer does")
	}
}

// A resource reached by two ResourceAccess components needs BOTH.
func TestInferredFromAccess_EveryResourceAccessMustQualify(t *testing.T) {
	p, root := fixtureProject(), fixtureServer(t)
	sys := p.SystemDesign.Model.(*projectstate.System)
	sys.Relationships = append(sys.Relationships, projectstate.Relationship{From: "delta-access", To: "gamma-store"})
	v := evaluateFixture(t, p, root)["R-gamma-store"]
	if v.Qualifies || !strings.Contains(v.Reason, "C-delta-access does not qualify") {
		t.Errorf("verdict = %+v, want refused on the second, failing ResourceAccess", v)
	}
}

// ---- the sign-off path ----------------------------------------------------------------

func TestSignOff_NSTPQualifiesOnTheRecordedSignOffAndTheCountedArtifact(t *testing.T) {
	vs := evaluateFixture(t, fixtureProject(), fixtureServer(t))
	v := vs["N-STP"]
	if !v.Qualifies {
		t.Fatalf("N-STP did not qualify: %s", v.Reason)
	}
	const want = `testingState.systemTestPlan (3 scenarios) + founderSignOff[2026-09-12]="stp looks good in the app. sign off. continue"`
	if v.Basis != want {
		t.Errorf("basis = %q\nwant    %q", v.Basis, want)
	}
	if v.ArtifactRef != "testingState.systemTestPlan" {
		t.Errorf("artifact ref = %q", v.ArtifactRef)
	}
}

// A sign-off never stands alone: with the artifact gone, the activity does not qualify.
func TestSignOff_DoesNotStandWithoutItsArtifact(t *testing.T) {
	for name, ts := range map[string]*projectstate.TestingState{
		"no testing state":         nil,
		"no system test plan":      {},
		"a plan with no scenarios": {SystemTestPlan: &projectstate.SystemTestPlan{}},
	} {
		t.Run(name, func(t *testing.T) {
			p := fixtureProject()
			p.TestingState = ts
			if v := evaluateFixture(t, p, fixtureServer(t))["N-STP"]; v.Qualifies {
				t.Errorf("N-STP qualified with %s: %s", name, v.Basis)
			}
		})
	}
}

// The recorded sign-off is the founder's words, verbatim, and it is the ONLY one: N-IT
// and every other componentless activity have no evidence path.
func TestFounderSignOffs_AreExactlyTheRecordedDecision(t *testing.T) {
	if len(founderSignOffs) != 1 {
		t.Fatalf("%d sign-offs recorded, want exactly 1 (N-STP, 2026-09-12)", len(founderSignOffs))
	}
	s := founderSignOffs[0]
	if s.ActivityID != "N-STP" || s.Date != "2026-09-12" || s.Quote != "stp looks good in the app. sign off. continue" {
		t.Errorf("sign-off = %+v", s)
	}
	if s.Artifact.Ref != "testingState.systemTestPlan" {
		t.Errorf("sign-off artifact = %q", s.Artifact.Ref)
	}
}

// The ruling a code or inferred basis cites is the founder's sentence, verbatim. A
// paraphrase here would be committed into every basis as if the founder had said it.
func TestFounderRuling_IsQuotedVerbatim(t *testing.T) {
	const said = "assume any component that is fully implemented is done and reviewed and integrated"
	if founderRuling != said {
		t.Errorf("founderRuling = %q\nthe founder said %q", founderRuling, said)
	}
	if want := "founderRuling[2026-09-09]=" + said; founderRulingRef != want {
		t.Errorf("founderRulingRef = %q, want %q", founderRulingRef, want)
	}
}

// ---- the attempts ---------------------------------------------------------------------

func backfillFixture(t *testing.T) (projectstate.Project, map[string]verdict) {
	t.Helper()
	p, root := fixtureProject(), fixtureServer(t)
	vs, err := evaluate(inputs{Project: p, ServerRoot: root, Head: fixtureHead})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backfill(&p, vs, time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	byID := map[string]verdict{}
	for _, v := range vs {
		byID[v.ActivityID] = v
	}
	return p, byID
}

// profileTasks is the profile's non-conditional task set for one activity type/variant.
// TasksForProfile already excludes the two sub-attempt tasks by construction (they are
// not lifecycle nodes), so this is a direct pass-through kept as its own name for the
// tests below that read it as "what the tool ought to derive".
func profileTasks(typ projectstate.ActivityType, v projectstate.TestingVariant) []projectstate.MethodTask {
	return projectstate.TasksForProfile(projectstate.ProfileFor(typ, v))
}

// Every qualifying activity gets exactly one passed attempt per non-conditional task of
// its profile, every one validates, every one is backfilled by this tool with the
// verdict's basis — and a non-qualifier gets no row at all.
func TestBackfill_EveryAttemptIsValidBackfilledAndCoversTheProfile(t *testing.T) {
	p, vs := backfillFixture(t)

	wantType := map[string]projectstate.ActivityType{
		"C-alpha-manager": projectstate.ActivityTypeService,
		"C-gamma-access":  projectstate.ActivityTypeService,
		"R-gamma-store":   projectstate.ActivityTypeDeployment,
		"N-STP":           projectstate.ActivityTypeTesting,
	}
	var rows []string
	for id := range p.ActivityConstruction {
		rows = append(rows, id)
	}
	sort.Strings(rows)
	if want := []string{"C-alpha-manager", "C-gamma-access", "N-STP", "R-gamma-store"}; !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %v, want exactly the qualifying set %v", rows, want)
	}
	for id, typ := range wantType {
		row := p.ActivityConstruction[id]
		if row.Type != typ {
			t.Errorf("%s: type = %v, want %v", id, row.Type, typ)
		}
		var got []projectstate.MethodTask
		for _, a := range row.Attempts {
			got = append(got, a.Task)
			if err := a.Provenance.Validate(); err != nil {
				t.Errorf("%s: %v", a.AttemptID, err)
			}
			if a.Provenance.Origin != projectstate.OriginBackfilled {
				t.Errorf("%s: origin = %q — nothing here was observed", a.AttemptID, a.Provenance.Origin)
			}
			if a.Provenance.Generator != generatorID || a.Provenance.Basis != vs[id].Basis {
				t.Errorf("%s: provenance = %+v", a.AttemptID, a.Provenance)
			}
			if a.Outcome != projectstate.OutcomePassed || a.Attempt != 1 || a.AttemptID != projectstate.AttemptID(id, a.Task, 1) {
				t.Errorf("%s: outcome/attempt/id = %v/%d/%s", a.AttemptID, a.Outcome, a.Attempt, a.AttemptID)
			}
		}
		if want := profileTasks(typ, projectstate.TestVariantPlan); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: tasks = %v, want the profile minus the conditionals %v", id, got, want)
		}
	}
}

// Each attempt points at what backs it, and at nothing when only a ruling does.
func TestBackfill_EvidenceRefsPointOnlyAtWhatBacksTheTask(t *testing.T) {
	p, _ := backfillFixture(t)
	for _, a := range p.ActivityConstruction["C-gamma-access"].Attempts {
		var want projectstate.EvidenceRef
		switch a.Task {
		case projectstate.TaskDetailedDesign, projectstate.TaskDesignReview:
			want = projectstate.EvidenceRef{Kind: projectstate.EvidenceContract, Ref: "gammaAccess"}
		case projectstate.TaskConstruction, projectstate.TaskCodeReview:
			want = projectstate.EvidenceRef{Kind: projectstate.EvidenceGit, Ref: fixtureHead}
		}
		if a.Evidence != want {
			t.Errorf("%s: evidence = %+v, want %+v", a.AttemptID, a.Evidence, want)
		}
	}
	for _, a := range p.ActivityConstruction["N-STP"].Attempts {
		if a.Evidence != (projectstate.EvidenceRef{Kind: projectstate.EvidenceArtifact, Ref: "testingState.systemTestPlan"}) {
			t.Errorf("%s: evidence = %+v, want the signed-off artifact", a.AttemptID, a.Evidence)
		}
	}
	for _, a := range p.ActivityConstruction["R-gamma-store"].Attempts {
		if a.Evidence != (projectstate.EvidenceRef{}) {
			t.Errorf("%s: evidence = %+v, want none — an inference has no artifact", a.AttemptID, a.Evidence)
		}
	}
}

func TestValidateAttempts_RefusesAnInvalidOrObservedAttempt(t *testing.T) {
	good := projectstate.TaskAttempt{AttemptID: "X:srs:1", Provenance: projectstate.AttemptProvenance{Origin: projectstate.OriginBackfilled, Basis: "b"}}
	if err := validateAttempts([]projectstate.TaskAttempt{good}); err != nil {
		t.Fatalf("a valid attempt was refused: %v", err)
	}
	noBasis := good
	noBasis.Provenance.Basis = ""
	observed := good
	observed.Provenance.Origin = projectstate.OriginObserved
	for name, a := range map[string]projectstate.TaskAttempt{"empty basis": noBasis, "observed": observed} {
		if err := validateAttempts([]projectstate.TaskAttempt{good, a}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A row that already holds real history is never overwritten; this tool's own earlier
// backfill is replaced, and the row keeps its other fields.
func TestBackfill_NeverOverwritesRealHistory(t *testing.T) {
	p, root := fixtureProject(), fixtureServer(t)
	vs, err := evaluate(inputs{Project: p, ServerRoot: root, Head: fixtureHead})
	if err != nil {
		t.Fatal(err)
	}
	observed := projectstate.TaskAttempt{AttemptID: "C-alpha-manager:srs:1", Provenance: projectstate.AttemptProvenance{Origin: projectstate.OriginObserved}}
	p.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"C-alpha-manager": {ActivityID: "C-alpha-manager", Attempts: []projectstate.TaskAttempt{observed}},
	}
	if _, err := backfill(&p, vs, time.Now()); err == nil || !strings.Contains(err.Error(), "real history") {
		t.Fatalf("want a refusal to overwrite an observed attempt, got %v", err)
	}

	prior := observed
	prior.Provenance = projectstate.AttemptProvenance{Origin: projectstate.OriginBackfilled, Generator: generatorID, Basis: "earlier run"}
	p.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"C-alpha-manager": {ActivityID: "C-alpha-manager", FailureDetail: "kept", Attempts: []projectstate.TaskAttempt{prior}},
	}
	if _, err := backfill(&p, vs, time.Now()); err != nil {
		t.Fatalf("re-running over this tool's own backfill: %v", err)
	}
	row := p.ActivityConstruction["C-alpha-manager"]
	if row.FailureDetail != "kept" || len(row.Attempts) < 2 || row.Attempts[0].Provenance.Basis == "earlier run" {
		t.Errorf("row = %+v, want its fields kept and its attempts replaced", row)
	}
}

// "This tool's own earlier backfill" means backfilled by THIS generator. A row another
// tool backfilled is someone else's record — its basis is theirs to defend — and it is
// refused like observed history, not replaced.
func TestBackfill_RefusesARowBackfilledByAnotherGenerator(t *testing.T) {
	p, root := fixtureProject(), fixtureServer(t)
	vs, err := evaluate(inputs{Project: p, ServerRoot: root, Head: fixtureHead})
	if err != nil {
		t.Fatal(err)
	}
	theirs := projectstate.TaskAttempt{AttemptID: "C-alpha-manager:srs:1", Provenance: projectstate.AttemptProvenance{
		Origin: projectstate.OriginBackfilled, Generator: "cmd/some-other-tool", Basis: "their basis",
	}}
	p.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"C-alpha-manager": {ActivityID: "C-alpha-manager", Attempts: []projectstate.TaskAttempt{theirs}},
	}
	_, err = backfill(&p, vs, time.Now())
	if err == nil || !strings.Contains(err.Error(), "real history") || !strings.Contains(err.Error(), "cmd/some-other-tool") {
		t.Fatalf("want a refusal naming the other generator, got %v", err)
	}
	if got := p.ActivityConstruction["C-alpha-manager"].Attempts; len(got) != 1 || got[0].Provenance.Basis != "their basis" {
		t.Errorf("the other generator's row was touched: %+v", got)
	}
}

// ---- de-qualification: a regression refuses the run ------------------------------------

// backfilledThenRegressed backfills the fixture, then applies regress to the evidence
// and re-evaluates. It returns the backfilled project, a deep copy of its rows, and the
// fresh verdicts.
func backfilledThenRegressed(t *testing.T, regress func(p *projectstate.Project, root string)) (projectstate.Project, []byte, []verdict) {
	t.Helper()
	p, root := fixtureProject(), fixtureServer(t)
	vs, err := evaluate(inputs{Project: p, ServerRoot: root, Head: fixtureHead})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backfill(&p, vs, time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	snapshot, err := json.Marshal(p.ActivityConstruction)
	if err != nil {
		t.Fatal(err)
	}
	regress(&p, root)
	again, err := evaluate(inputs{Project: p, ServerRoot: root, Head: "fedcba9876543210fedcba9876543210fedcba98"})
	if err != nil {
		t.Fatal(err)
	}
	return p, snapshot, again
}

// refusedUntouched runs the re-backfill and asserts it is refused with every want in the
// error, and that no row moved.
func refusedUntouched(t *testing.T, p projectstate.Project, snapshot []byte, vs []verdict, want ...string) {
	t.Helper()
	_, err := backfill(&p, vs, time.Date(2026, 9, 13, 8, 30, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("a re-run over a regressed backfilled activity was accepted; want it refused")
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error = %q\nwant it to contain %q", err, w)
		}
	}
	after, mErr := json.Marshal(p.ActivityConstruction)
	if mErr != nil {
		t.Fatal(mErr)
	}
	if !bytes.Equal(after, snapshot) {
		t.Error("the refused run changed .activityConstruction; want nothing written")
	}
}

// The implementing file is deleted after this tool backfilled the component. The re-run
// is refused, naming the activity and the condition it now fails — and nothing moves,
// not even gamma's row, whose evidence changed in the same run and would otherwise be
// re-derived.
func TestBackfill_RefusesWhenABackfilledActivityLosesItsImplementingFile(t *testing.T) {
	p, snapshot, vs := backfilledThenRegressed(t, func(_ *projectstate.Project, root string) {
		_ = os.Remove(filepath.Join(root, "internal/manager/alpha/alphamanager.go"))
		writeFile(t, root, "internal/resourceaccess/gamma/gammaaccess.go", fieldedImplSource("gamma", "gitGammaAccess", gammaOps...)+
			strings.TrimPrefix(implSource("gamma", "noopGammaAccess", gammaOps...), "package gamma\n"))
	})
	refusedUntouched(t, p, snapshot, vs,
		"refusing to write anything: activities this tool backfilled no longer qualify (1)",
		"regression for a human to decide",
		"\n  C-alpha-manager: condition 3: server/internal/manager/alpha/alphamanager.go")
}

// The contract flips to stub: true after this tool backfilled the component. Every
// activity that regresses with it is named — the RA, and the Resource inferred from it.
func TestBackfill_RefusesWhenABackfilledActivitysContractFlipsToStub(t *testing.T) {
	p, snapshot, vs := backfilledThenRegressed(t, func(p *projectstate.Project, _ string) {
		sc := p.ServiceContracts["gammaAccess"]
		sc.Stub = true
		p.ServiceContracts["gammaAccess"] = sc
	})
	refusedUntouched(t, p, snapshot, vs,
		"no longer qualify (2)",
		"\n  C-gamma-access: condition 2: serviceContracts[gammaAccess] is stub: true",
		"\n  R-gamma-store: inferred: its ResourceAccess C-gamma-access does not qualify")
}

// Only THIS generator's backfill is a claim the tool must defend. A non-qualifying
// activity whose row holds observed history (even one carrying this tool's name as its
// generator — observed is not backfilled), or another generator's backfill, or no
// attempt at all, is not a regression of anything this tool wrote.
func TestBackfill_ANonQualifierWithoutThisGeneratorsAttemptsIsNotARegression(t *testing.T) {
	p, root := fixtureProject(), fixtureServer(t)
	vs, err := evaluate(inputs{Project: p, ServerRoot: root, Head: fixtureHead})
	if err != nil {
		t.Fatal(err)
	}
	p.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"N-IT": {ActivityID: "N-IT", Attempts: []projectstate.TaskAttempt{
			{AttemptID: "N-IT:testing:1", Provenance: projectstate.AttemptProvenance{Origin: projectstate.OriginObserved}},
		}},
		"R-orphan": {ActivityID: "R-orphan", Attempts: []projectstate.TaskAttempt{
			{AttemptID: "R-orphan:construction:1", Provenance: projectstate.AttemptProvenance{Origin: projectstate.OriginObserved, Generator: generatorID}},
		}},
		"C-delta-access": {ActivityID: "C-delta-access", Attempts: []projectstate.TaskAttempt{
			{AttemptID: "C-delta-access:srs:1", Provenance: projectstate.AttemptProvenance{Origin: projectstate.OriginBackfilled, Generator: "cmd/some-other-tool", Basis: "theirs"}},
		}},
		"C-beta-engine": {ActivityID: "C-beta-engine"},
	}
	if _, err := backfill(&p, vs, time.Now()); err != nil {
		t.Fatalf("refused a run with no regression of this tool's own rows: %v", err)
	}
}

// ---- the writer -----------------------------------------------------------------------

// stateDocument encodes p through the codec and adds what a committed document carries
// that the codec does not — a real updatedAt and the activityListOverrides sidecar — so
// a test can prove a rewrite leaves both alone.
func stateDocument(t *testing.T, p projectstate.Project) []byte {
	t.Helper()
	enc, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	const zeroStamp = `"updatedAt": "0001-01-01T00:00:00Z"`
	if !bytes.Contains(enc, []byte(zeroStamp)) {
		t.Fatalf("fixture encoding carries no %s to replace", zeroStamp)
	}
	withSidecar := bytes.Replace(enc, []byte(zeroStamp),
		[]byte(`"updatedAt": "2026-08-09T05:30:44.213963Z", "activityListOverrides": {"overrides": []}`), 1)
	var compact, out bytes.Buffer
	if err := json.Compact(&compact, withSidecar); err != nil {
		t.Fatalf("compact fixture: %v", err)
	}
	if err := json.Indent(&out, compact.Bytes(), "", "  "); err != nil {
		t.Fatalf("indent fixture: %v", err)
	}
	out.WriteByte('\n')
	return out.Bytes()
}

func topLevel(t *testing.T, doc []byte) []member {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, doc); err != nil {
		t.Fatal(err)
	}
	ms, err := members(compact.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

func rewriteFixture(t *testing.T, p projectstate.Project, raw []byte) ([]byte, error) {
	t.Helper()
	root := fixtureServer(t)
	vs, err := evaluate(inputs{Project: p, ServerRoot: root, Head: fixtureHead})
	if err != nil {
		t.Fatal(err)
	}
	return rewrite(raw, func(p *projectstate.Project) error {
		_, err := backfill(p, vs, time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC))
		return err
	})
}

// The live file has no .activityConstruction member (the codec omits an empty map). The
// writer ADDS it — at the codec's own position, after slots — and every other member,
// including the two the codec does not carry, keeps its exact bytes and order.
func TestRewrite_AddsTheMemberAtTheCodecPositionAndTouchesNothingElse(t *testing.T) {
	p := fixtureProject()
	p.ConstructionProgress = &projectstate.ConstructionProgress{TotalWeeks: 49}
	raw := stateDocument(t, p)
	if bytes.Contains(raw, []byte(`"activityConstruction"`)) {
		t.Fatal("fixture already holds the member")
	}

	out, err := rewriteFixture(t, p, raw)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	before, after := topLevel(t, raw), topLevel(t, out)
	var keys []string
	for _, m := range after {
		keys = append(keys, m.key)
	}
	at := strings.Index(strings.Join(keys, ","), "slots,activityConstruction,constructionProgress")
	if at < 0 {
		t.Errorf("member order = %v, want activityConstruction between slots and constructionProgress", keys)
	}
	var kept []member
	for _, m := range after {
		if m.key != constructionMember {
			kept = append(kept, m)
		}
	}
	if !reflect.DeepEqual(kept, before) {
		t.Error("a member other than activityConstruction changed")
	}
	for _, keep := range []string{`"updatedAt": "2026-08-09T05:30:44.213963Z"`, `"activityListOverrides": {`} {
		if !bytes.Contains(out, []byte(keep)) {
			t.Errorf("the rewrite lost %s", keep)
		}
	}
	back, err := decodeDocument(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.ActivityConstruction) != 4 {
		t.Errorf("decoded %d rows, want the 4 qualifying activities", len(back.ActivityConstruction))
	}
}

// The coordinator's finding against Task 4's splicer: a replaced member must go in as
// exact bytes or be checked against the original. A committed .activityConstruction the
// codec cannot round-trip byte-for-byte (here, a field it does not carry) would lose data
// in the replacement, and a codec-vs-codec check cannot see the loss — so it is refused.
func TestRewrite_RefusesToReplaceAMemberTheCodecCannotRoundTrip(t *testing.T) {
	p := fixtureProject()
	p.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{"C-beta-engine": {ActivityID: "C-beta-engine"}}
	raw := stateDocument(t, p)
	lossy := bytes.Replace(raw, []byte(`"activityID": "C-beta-engine",`),
		[]byte(`"activityID": "C-beta-engine",
      "legacyField": "the codec does not carry this",`), 1)
	if bytes.Equal(lossy, raw) {
		t.Fatal("fixture edit did not apply")
	}
	if _, err := rewriteFixture(t, p, lossy); err == nil || !strings.Contains(err.Error(), "codec round trip") {
		t.Fatalf("want a refusal naming the round trip, got %v", err)
	}

	// The same member WITHOUT the extra field round-trips, and is replaced in place.
	out, err := rewriteFixture(t, p, raw)
	if err != nil {
		t.Fatalf("rewrite over a clean member: %v", err)
	}
	back, err := decodeDocument(out)
	if err != nil {
		t.Fatal(err)
	}
	if _, kept := back.ActivityConstruction["C-beta-engine"]; !kept || len(back.ActivityConstruction) != 5 {
		t.Errorf("rows = %d, want the untouched row kept plus the 4 qualifying ones", len(back.ActivityConstruction))
	}
}

// FIDELITY GATE: a document re-indenting cannot reproduce is refused, so a write never
// smuggles a reformat of unrelated state into the diff.
func TestRewrite_RefusesADocumentItCannotReproduce(t *testing.T) {
	p := fixtureProject()
	raw := bytes.Replace(stateDocument(t, p), []byte(`"id": "fixture"`), []byte(`"id":   "fixture"`), 1)
	if _, err := rewriteFixture(t, p, raw); err == nil || !strings.Contains(err.Error(), "byte-for-byte") {
		t.Fatalf("want the fidelity refusal, got %v", err)
	}
}

// The writer owns one member; an edit that reaches anything else is refused.
func TestRewrite_RefusesAnEditOutsideItsMember(t *testing.T) {
	raw := stateDocument(t, fixtureProject())
	_, err := rewrite(raw, func(p *projectstate.Project) error {
		p.Name = "renamed"
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "may not touch") {
		t.Fatalf("want a refusal, got %v", err)
	}
}

// A document holding .activityConstruction twice is refused. Which copy counts is up to
// the reader, and without the refusal the splice would rewrite both copies and write a
// document that still holds the member twice.
func TestRewrite_RefusesADocumentHoldingTheMemberTwice(t *testing.T) {
	p := fixtureProject()
	p.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{"C-beta-engine": {ActivityID: "C-beta-engine"}}
	var dup []member
	for _, m := range topLevel(t, stateDocument(t, p)) {
		dup = append(dup, m)
		if m.key == constructionMember {
			dup = append(dup, m)
		}
	}
	var raw bytes.Buffer
	if err := json.Indent(&raw, joinMembers(dup), "", "  "); err != nil {
		t.Fatal(err)
	}
	raw.WriteByte('\n')
	if _, err := rewriteFixture(t, p, raw.Bytes()); err == nil || !strings.Contains(err.Error(), `member "activityConstruction" twice`) {
		t.Fatalf("want a refusal naming the duplicated member, got %v", err)
	}
}

// rewriteAt runs the tool's whole edit over raw as run() does: decode, evaluate the
// server tree at head, backfill stamped now.
func rewriteAt(t *testing.T, raw []byte, root, head string, now time.Time) []byte {
	t.Helper()
	p, err := decodeDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	vs, err := evaluate(inputs{Project: p, ServerRoot: root, Head: head})
	if err != nil {
		t.Fatal(err)
	}
	out, err := rewrite(raw, func(p *projectstate.Project) error {
		_, err := backfill(p, vs, now)
		return err
	})
	if err != nil {
		t.Fatalf("rewrite at %s: %v", head, err)
	}
	return out
}

// constructionAttempt is a row's construction attempt, whose git evidence names the
// commit its code was read at.
func constructionAttempt(t *testing.T, doc []byte, id string) projectstate.TaskAttempt {
	t.Helper()
	p, err := decodeDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.ActivityConstruction[id].Attempts {
		if a.Task == projectstate.TaskConstruction {
			return a
		}
	}
	t.Fatalf("%s has no construction attempt", id)
	return projectstate.TaskAttempt{}
}

// BYTE-IDEMPOTENT RE-RUNS. A re-run at a later commit and a later time over unchanged
// evidence produces the document it was given, byte for byte — so run() takes its
// "nothing to write" branch — and every row keeps its original generatedAt and the
// commit it was first read at. Where the evidence DID change, that row alone is
// re-derived and re-stamped.
func TestRewrite_ReRunIsByteIdenticalUnlessTheEvidenceChanged(t *testing.T) {
	const laterHead = "fedcba9876543210fedcba9876543210fedcba98"
	first, later := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 13, 8, 30, 0, 0, time.UTC)
	root := fixtureServer(t)
	once := rewriteAt(t, stateDocument(t, fixtureProject()), root, fixtureHead, first)

	again := rewriteAt(t, once, root, laterHead, later)
	if !bytes.Equal(again, once) {
		t.Fatalf("a re-run over unchanged evidence rewrote the document (%d bytes -> %d bytes)", len(once), len(again))
	}
	for _, id := range []string{"C-alpha-manager", "C-gamma-access"} {
		a := constructionAttempt(t, again, id)
		if a.Evidence.Ref != fixtureHead || !strings.Contains(a.Provenance.Basis, "@ "+fixtureHead+" ") || !a.Provenance.GeneratedAt.Equal(first) {
			t.Errorf("%s: re-stamped (evidence %s, generatedAt %v, basis %q), want the original citation and time kept",
				id, a.Evidence.Ref, a.Provenance.GeneratedAt, a.Provenance.Basis)
		}
	}

	// gamma gains a no-op receiver beside its live one: its derivation changes, so it is
	// re-derived at the new commit and time. alpha's evidence did not change; it is kept.
	writeFile(t, root, "internal/resourceaccess/gamma/gammaaccess.go", fieldedImplSource("gamma", "gitGammaAccess", gammaOps...)+
		strings.TrimPrefix(implSource("gamma", "noopGammaAccess", gammaOps...), "package gamma\n"))
	changed := rewriteAt(t, once, root, laterHead, later)
	gamma := constructionAttempt(t, changed, "C-gamma-access")
	if gamma.Evidence.Ref != laterHead || !gamma.Provenance.GeneratedAt.Equal(later) || !strings.Contains(gamma.Provenance.Basis, "noopGammaAccess") {
		t.Errorf("C-gamma-access: evidence %s, generatedAt %v, basis %q — want it re-derived at %s",
			gamma.Evidence.Ref, gamma.Provenance.GeneratedAt, gamma.Provenance.Basis, laterHead)
	}
	if alpha := constructionAttempt(t, changed, "C-alpha-manager"); alpha.Evidence.Ref != fixtureHead || !alpha.Provenance.GeneratedAt.Equal(first) {
		t.Errorf("C-alpha-manager: re-stamped (%s, %v) although its evidence did not change", alpha.Evidence.Ref, alpha.Provenance.GeneratedAt)
	}
}

// The systemdesign view-model's constructionStarted (Begin versus Resume) reads a stored
// row's Phase, Phases, StartedAt, FailureReason and FailureDetail as fields only the pump
// writes. That trust is safe only while this tool never writes them: a row it creates
// carries none of them, and a re-run over a row it backfilled earlier keeps whatever that
// row held.
func TestBackfill_NeverWritesThePumpsFields(t *testing.T) {
	p, _ := backfillFixture(t)
	if len(p.ActivityConstruction) == 0 {
		t.Fatal("the fixture backfilled no row; the test would pass vacuously")
	}
	for id, row := range p.ActivityConstruction {
		if row.Phase != projectstate.ActivityConstructionNotStarted || len(row.Phases) != 0 || row.StartedAt != nil ||
			row.FailureReason != projectstate.FailureReasonUnknown || row.FailureDetail != "" || len(row.OperatorNotes) != 0 {
			t.Errorf("%s: backfill wrote pump-owned state: phase=%v phases=%v startedAt=%v failure=%v/%q notes=%v",
				id, row.Phase, row.Phases, row.StartedAt, row.FailureReason, row.FailureDetail, row.OperatorNotes)
		}
	}

	p, root := fixtureProject(), fixtureServer(t)
	vs, err := evaluate(inputs{Project: p, ServerRoot: root, Head: fixtureHead})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	held := projectstate.ActivityConstructionStatus{
		ActivityID: "C-alpha-manager",
		Phase:      projectstate.ActivityConstructionDone,
		Phases:     []projectstate.PhaseCompletion{{Phase: projectstate.MethodPhaseIntegration, Weight: 20, Completed: true}},
		StartedAt:  &started,
		Attempts: []projectstate.TaskAttempt{{AttemptID: "C-alpha-manager:srs:1", Provenance: projectstate.AttemptProvenance{
			Origin: projectstate.OriginBackfilled, Generator: generatorID, Basis: "earlier run"}}},
	}
	p.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{"C-alpha-manager": held}
	if _, err := backfill(&p, vs, time.Now()); err != nil {
		t.Fatalf("re-running over this tool's own backfill: %v", err)
	}
	row := p.ActivityConstruction["C-alpha-manager"]
	if row.Phase != held.Phase || !reflect.DeepEqual(row.Phases, held.Phases) || row.StartedAt != held.StartedAt {
		t.Errorf("re-run changed pump-owned state: phase=%v phases=%v startedAt=%v, want %v %v %v",
			row.Phase, row.Phases, row.StartedAt, held.Phase, held.Phases, held.StartedAt)
	}
}

// ---- integration: gated on every dependency being fully Done ---------------------------

// networkOfFixture is the fixture's slot-10 Network, for a test to edit in place.
func networkOfFixture(t *testing.T, p *projectstate.Project) *projectstate.Network {
	t.Helper()
	n, ok := p.Network.Model.(*projectstate.Network)
	if !ok {
		t.Fatalf("fixture network is %T", p.Network.Model)
	}
	return n
}

// dependOn replaces activity's slot-10 dependencies with deps.
func dependOn(t *testing.T, p *projectstate.Project, activity string, deps ...string) {
	t.Helper()
	n := networkOfFixture(t, p)
	for i := range n.Dependencies {
		if n.Dependencies[i].Activity == activity {
			n.Dependencies[i].DependsOn = deps
			return
		}
	}
	n.Dependencies = append(n.Dependencies, projectstate.NetworkDependency{Activity: activity, DependsOn: deps})
}

// itemOf is the fixture plan's ActivityItem for id.
func itemOf(t *testing.T, p projectstate.Project, id string) projectstate.ActivityItem {
	t.Helper()
	for _, a := range p.ActivityList.Model.(*projectstate.ActivityList).Activities {
		if a.Name == id {
			return a
		}
	}
	t.Fatalf("fixture plan has no activity %q", id)
	return projectstate.ActivityItem{}
}

// backfillWith evaluates and backfills a prepared fixture project.
func backfillWith(t *testing.T, p projectstate.Project) (projectstate.Project, map[string]verdict) {
	t.Helper()
	root := fixtureServer(t)
	vs, err := evaluate(inputs{Project: p, ServerRoot: root, Head: fixtureHead})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if _, err := backfill(&p, vs, time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	byID := map[string]verdict{}
	for _, v := range vs {
		byID[v.ActivityID] = v
	}
	return p, byID
}

// tasksOf is a row's attempted tasks, in ledger order.
func tasksOf(row projectstate.ActivityConstructionStatus) []projectstate.MethodTask {
	var out []projectstate.MethodTask
	for _, a := range row.Attempts {
		out = append(out, a.Task)
	}
	return out
}

// withoutIntegration is a task list minus every Integration-phase task.
func withoutIntegration(tasks []projectstate.MethodTask) []projectstate.MethodTask {
	var out []projectstate.MethodTask
	for _, task := range tasks {
		if projectstate.PhaseForTask(task) != projectstate.MethodPhaseIntegration {
			out = append(out, task)
		}
	}
	return out
}

// assertIntegrationPending: every non-conditional task but Integration's is attempted, the
// basis ends with the pending clause and the ruling, and the pump reads the row as
// running-in-review — never NotStarted (fresh work), never Done (unblocking dependents).
func assertIntegrationPending(t *testing.T, p projectstate.Project, vs map[string]verdict, id, clause string) {
	t.Helper()
	row := p.ActivityConstruction[id]
	want := withoutIntegration(profileTasks(row.Type, row.Variant))
	if got := tasksOf(row); !reflect.DeepEqual(got, want) {
		t.Errorf("%s: tasks = %v, want every non-conditional task but Integration's %v", id, got, want)
	}
	if len(want) == len(profileTasks(row.Type, row.Variant)) {
		t.Fatalf("%s: the profile has no Integration task; the test would pass vacuously", id)
	}
	suffix := " + " + clause + " + " + integrationRulingRef
	for _, a := range row.Attempts {
		if !strings.HasSuffix(a.Provenance.Basis, suffix) {
			t.Errorf("%s: basis = %q\nwant it to end %q", a.AttemptID, a.Provenance.Basis, suffix)
		}
	}
	if vs[id].IntegrationPending != clause || !strings.HasSuffix(vs[id].Reason, "; "+clause) {
		t.Errorf("%s: pending = %q, reason = %q, want the clause %q", id, vs[id].IntegrationPending, vs[id].Reason, clause)
	}
	phase, build := projectstate.EffectiveConstructionPhase(row, itemOf(t, p, id))
	if phase != projectstate.ActivityConstructionRunning || build != projectstate.BuildInReview {
		t.Errorf("%s: effective = %v/%v, want running/in-review", id, phase, build)
	}
}

// assertDone: every non-conditional task is attempted, the basis says nothing about
// Integration being pending, and the pump reads the row as Done and integrated.
func assertDone(t *testing.T, p projectstate.Project, vs map[string]verdict, id string) {
	t.Helper()
	row := p.ActivityConstruction[id]
	if got, want := tasksOf(row), profileTasks(row.Type, row.Variant); !reflect.DeepEqual(got, want) {
		t.Errorf("%s: tasks = %v, want the whole profile %v", id, got, want)
	}
	if vs[id].IntegrationPending != "" || strings.Contains(vs[id].Basis, "Integration pending") {
		t.Errorf("%s: pending = %q, basis = %q, want it integrated", id, vs[id].IntegrationPending, vs[id].Basis)
	}
	phase, build := projectstate.EffectiveConstructionPhase(row, itemOf(t, p, id))
	if phase != projectstate.ActivityConstructionDone || build != projectstate.BuildIntegrated {
		t.Errorf("%s: effective = %v/%v, want done/integrated", id, phase, build)
	}
}

// A dependency that does not qualify leaves its dependent integration pending. Both
// failing dependencies are named, in id order (given here out of order), each with why.
func TestIntegration_ADependencyThatIsNotBuiltLeavesTheDependentPending(t *testing.T) {
	p := fixtureProject()
	dependOn(t, &p, "C-alpha-manager", "R-orphan", "C-delta-access", "C-gamma-access")
	p, vs := backfillWith(t, p)

	assertIntegrationPending(t, p, vs, "C-alpha-manager",
		"Integration pending: dependency C-delta-access is not built; dependency R-orphan is not built.")
	for _, id := range []string{"C-gamma-access", "R-gamma-store", "N-STP"} {
		assertDone(t, p, vs, id)
	}
}

// Transitively: N-STP depends on C-alpha-manager, which qualifies but is itself pending,
// so N-STP is pending too — and it says its dependency is built but not integrated.
func TestIntegration_PendingPropagatesToEveryDependent(t *testing.T) {
	p := fixtureProject()
	dependOn(t, &p, "C-alpha-manager", "C-delta-access")
	dependOn(t, &p, "N-STP", "C-alpha-manager")
	p, vs := backfillWith(t, p)

	assertIntegrationPending(t, p, vs, "C-alpha-manager", "Integration pending: dependency C-delta-access is not built.")
	assertIntegrationPending(t, p, vs, "N-STP", "Integration pending: dependency C-alpha-manager is built but not integrated.")
	assertDone(t, p, vs, "C-gamma-access")
}

// Every dependency fully Done means Done, even when the dependent comes FIRST in plan
// order: the gate is computed in topological order, never in plan order. N-STP is moved to
// the front and depends on C-alpha-manager <- C-gamma-access <- R-gamma-store <- M0.
func TestIntegration_AllDependenciesDoneMeansDoneWhateverThePlanOrder(t *testing.T) {
	p := fixtureProject()
	list := p.ActivityList.Model.(*projectstate.ActivityList)
	for i, a := range list.Activities {
		if a.Name == "N-STP" {
			list.Activities = append([]projectstate.ActivityItem{a}, append(list.Activities[:i:i], list.Activities[i+1:]...)...)
			break
		}
	}
	if list.Activities[0].Name != "N-STP" {
		t.Fatal("fixture reorder did not apply")
	}
	dependOn(t, &p, "N-STP", "C-alpha-manager")
	p, vs := backfillWith(t, p)

	for _, id := range []string{"N-STP", "C-alpha-manager", "C-gamma-access", "R-gamma-store"} {
		assertDone(t, p, vs, id)
	}
}

// A milestone stands for what it depends on, as it does for the pump: M1 fans in
// R-delta-gateway (not built) and R-gamma-store, so a dependent of M1 is pending on the
// former; M0 depends on nothing and holds nothing back.
func TestIntegration_AMilestoneStandsForTheActivitiesItDependsOn(t *testing.T) {
	p := fixtureProject()
	dependOn(t, &p, "C-alpha-manager", "M1")
	p, vs := backfillWith(t, p)
	assertIntegrationPending(t, p, vs, "C-alpha-manager", "Integration pending: dependency R-delta-gateway is not built.")

	p = fixtureProject()
	dependOn(t, &p, "C-alpha-manager", "M0")
	p, vs = backfillWith(t, p)
	assertDone(t, p, vs, "C-alpha-manager")
}

// The graph must be whole for the verdict to be one: every defect is refused, by name.
func TestIntegration_RefusesADependencyGraphThatIsNotWhole(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, p *projectstate.Project)
		want   string
	}{
		{"no network", func(_ *testing.T, p *projectstate.Project) { p.Network = projectstate.ArtifactSlot{} },
			"slot network holds <nil>, not a Network"},
		{"a dangling dependency id", func(t *testing.T, p *projectstate.Project) { dependOn(t, p, "C-alpha-manager", "C-nowhere") },
			`slot 10, C-alpha-manager: dependency "C-nowhere" names neither an activity of the committed plan nor a slot-10 milestone`},
		{"dependencies for an unplanned activity", func(t *testing.T, p *projectstate.Project) { dependOn(t, p, "C-ghost", "M0") },
			`slot 10 lists dependencies for "C-ghost", which is not an activity of the committed plan`},
		{"a milestone cycle", func(t *testing.T, p *projectstate.Project) {
			n := networkOfFixture(t, p)
			n.Milestones = append(n.Milestones, projectstate.NetworkMilestone{ID: "M2", DependsOn: []string{"M3"}}, projectstate.NetworkMilestone{ID: "M3", DependsOn: []string{"M2"}})
			dependOn(t, p, "C-alpha-manager", "M2")
		}, `milestone "M2" depends, directly or transitively, on itself`},
		{"an activity cycle", func(t *testing.T, p *projectstate.Project) { dependOn(t, p, "C-gamma-access", "C-alpha-manager") },
			"slot 10 has a dependency cycle; these activities cannot be ordered: C-alpha-manager, C-gamma-access, N-IT, U-SPA-web-client"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := fixtureProject()
			c.mutate(t, &p)
			_, err := evaluate(inputs{Project: p, ServerRoot: fixtureServer(t), Head: fixtureHead})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

// Re-runs stay byte-identical with an integration-pending row in the document; and when a
// dependency edge moves, only the activity it gates is re-derived — every other row keeps
// its exact attempts.
func TestIntegration_ReRunsAreIdempotentAndOnlyTheGatedRowMoves(t *testing.T) {
	const laterHead = "fedcba9876543210fedcba9876543210fedcba98"
	first, later := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 13, 8, 30, 0, 0, time.UTC)
	root := fixtureServer(t)
	full := rewriteAt(t, stateDocument(t, fixtureProject()), root, fixtureHead, first)

	p, err := decodeDocument(full)
	if err != nil {
		t.Fatal(err)
	}
	dependOn(t, &p, "C-alpha-manager", "C-delta-access", "C-gamma-access")
	pending := rewriteAt(t, stateDocument(t, p), root, laterHead, later)
	if again := rewriteAt(t, pending, root, laterHead, later.Add(time.Hour)); !bytes.Equal(again, pending) {
		t.Fatalf("a re-run over an integration-pending document rewrote it (%d bytes -> %d bytes)", len(pending), len(again))
	}

	was, err := decodeDocument(full)
	if err != nil {
		t.Fatal(err)
	}
	now, err := decodeDocument(pending)
	if err != nil {
		t.Fatal(err)
	}
	for id, row := range was.ActivityConstruction {
		if id == "C-alpha-manager" {
			continue
		}
		if !reflect.DeepEqual(now.ActivityConstruction[id], row) {
			t.Errorf("%s moved although no dependency of it changed", id)
		}
	}
	alpha := now.ActivityConstruction["C-alpha-manager"]
	if got, want := tasksOf(alpha), withoutIntegration(tasksOf(was.ActivityConstruction["C-alpha-manager"])); !reflect.DeepEqual(got, want) {
		t.Errorf("C-alpha-manager: tasks = %v, want its earlier backfill minus Integration %v", got, want)
	}
}

// The narrowing ruling is cited stated plainly (designer final pass, item 5): the
// founder's verbatim words carried a question ("…is that really possible? how can they
// be done in code…"), which read as doubt inside every committed basis. The quote lives
// in the SDD ledger; the basis states the ruling.
func TestIntegrationRuling_IsStatedPlainly(t *testing.T) {
	const ruling = "Built against its dependencies' contracts; integration waits until every dependency is Done."
	if integrationRuling != ruling {
		t.Errorf("integrationRuling = %q\nwant %q", integrationRuling, ruling)
	}
	if want := "founderRuling[2026-09-13]=" + ruling; integrationRulingRef != want {
		t.Errorf("integrationRulingRef = %q, want %q", integrationRulingRef, want)
	}
	for _, doubt := range []string{"?", "really possible", "how can they"} {
		if strings.Contains(integrationRulingRef, doubt) {
			t.Errorf("the cited ruling carries %q: %q", doubt, integrationRulingRef)
		}
	}
}
