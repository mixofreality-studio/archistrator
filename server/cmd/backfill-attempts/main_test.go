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
	writeFile(t, root, "internal/resourceaccess/gamma/gammaaccess.go", implSource("gamma", "gitGammaAccess", gammaOps...))
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
func profileTasks(typ projectstate.ActivityType, v projectstate.TestingVariant) []projectstate.MethodTask {
	var out []projectstate.MethodTask
	for _, task := range projectstate.TasksForProfile(projectstate.ProfileFor(typ, v)) {
		if !projectstate.IsConditionalTask(task) {
			out = append(out, task)
		}
	}
	return out
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
