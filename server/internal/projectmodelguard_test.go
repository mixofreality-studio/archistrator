package internal_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// projectmodelguard_test.go is the drift guard promised by the spec's "Schema
// ownership & evolution" section: archistrator's own CI must fail if its
// writers (the aiarch-state MCP tools, hand-authored fixtures, whatever
// touches .aiarch/state/project.json) produce a document the PUBLISHED
// projectmodel parser rejects.
//
// Unlike TestMethodDesignArtifacts (method_design_test.go), this test runs
// UNTAGGED in the default suite. That gate is build-tagged `methoddesign`
// because it exercises the full Method-invariant design ruleset, which is
// expensive and has historically needed staged rollout against a moving
// framework-go release. Neither reason applies here: projectmodel is now a
// published, pinned dependency (see go.mod), and Load is cheap — a single
// parse-and-cross-validate pass. There is no reason to keep this gate out of
// `go test ./...`; if a writer-side shape change breaks the codegen subset,
// CI should fail on every run, not only when someone remembers to pass
// -tags methoddesign.
func TestProjectJSONLoadsUnderPublishedProjectmodel(t *testing.T) {
	root := findRepoRootFromCwd(t)
	raw, err := os.ReadFile(filepath.Join(root, ".aiarch", "state", "project.json"))
	if err != nil {
		t.Fatalf("read project.json: %v", err)
	}

	model, err := projectmodel.Load(raw)
	if err != nil {
		t.Fatalf("committed project.json no longer loads under the published "+
			"framework-go-projectmodel parser: %v\n"+
			"this means a writer-side shape change broke the codegen subset "+
			"and the platform-first evolution process (spec: Schema ownership "+
			"& evolution) was skipped — the schema change must land in "+
			"framework-go-projectmodel FIRST, then be consumed here", err)
	}

	for _, w := range model.Warnings {
		t.Logf("projectmodel warning: %s", w)
	}

	// Second, cheap assertion: pin the five manager contract keys the rest of
	// this codebase (dispatch, construction, generators) hardcodes by name.
	// An accidental rename in project.json must fail HERE, with a clear
	// message pointing at the renamed key, not deep inside a generator that
	// silently emits nothing for a key it no longer recognizes.
	wantManagers := map[string]string{
		"systemDesignManager":  "Manager",
		"projectDesignManager": "Manager",
		"constructionManager":  "Manager",
		"operationsManager":    "Manager",
		"billingManager":       "Manager",
	}
	for key, wantLayer := range wantManagers {
		c, ok := model.Contracts[key]
		if !ok {
			t.Errorf("expected manager contract %q not found in .serviceContracts — "+
				"was it renamed? (drift guard would otherwise catch this only in a generator)", key)
			continue
		}
		if c.Layer != wantLayer {
			t.Errorf("manager contract %q has layer %q, want %q", key, c.Layer, wantLayer)
		}
	}
}

// TestServiceContractsMatchSystemComponents guards the direction the
// alignment pass in method_design_test.go does NOT check: contracts →
// architecture. TestMethodDesignArtifacts checks components→code (and flags
// orphaned packages), but nothing previously checked that every
// .serviceContracts entry still joins to a systemDesign component. That gap
// is exactly how fossil contract entries (removed from the architecture
// diagram by later design rulings but never pruned from project.json)
// survived undetected. This test closes it: every .serviceContracts entry
// must resolve to a systemDesign component, or the entry is stale and the
// architecture diagram (systemDesign, the source of truth) says it should not
// exist.
//
// A contract resolves in one of two ratified shapes:
//
//   - COMPONENT contract — its key joins a component via
//     model.System.ComponentByContractKey (the exact/heuristic key↔component
//     join). This is the common one-component-one-contract case.
//
//   - contract FACET — its key names NO component, but its `component` field
//     resolves to an owning component. This is the ratified resource-access
//     facet doctrine (operational-concepts "resource-access facets"): a single
//     component (e.g. projectStateAccess, billingStateAccess) publishes several
//     cohesive contract facets that all live in the one package. A facet is
//     valid IFF its `component` field resolves AND the facet declares the same
//     layer as its owning component (a facet cannot cross layers).
//
// A contract whose key does not join AND whose `component` field resolves to
// nothing is a stale/fossil entry — that error is preserved.
func TestServiceContractsMatchSystemComponents(t *testing.T) {
	root := findRepoRootFromCwd(t)
	model, err := projectmodel.LoadFile(filepath.Join(root, ".aiarch", "state", "project.json"))
	if err != nil {
		t.Fatalf("load project.json: %v", err)
	}

	for key, c := range model.Contracts {
		// Shape 1: the key is itself a component contract key. Nothing more to check.
		if _, ok := model.System.ComponentByContractKey(key); ok {
			continue
		}
		// Shape 2: contract facet — the key names no component, so it is valid
		// only if its `component` field joins an owning component.
		owner, ok := model.System.ComponentByContractKey(c.Component)
		if !ok {
			t.Errorf("service contract %q resolves to no systemDesign component — "+
				"its key is not a component contract key, and its component field %q "+
				"names no component either. It is a stale/fossil entry (the architecture "+
				"diagram is the source of truth; either add a systemDesign component for "+
				"it, point its component field at an existing one to make it a facet, or "+
				"delete the contract)", key, c.Component)
			continue
		}
		// A facet must share its owner's layer (component layer is kind-cased,
		// e.g. "resourceAccess"; the contract layer is Method-cased, e.g.
		// "ResourceAccess" — EqualFold reconciles the casing convention).
		if !strings.EqualFold(c.Layer, owner.Layer) {
			t.Errorf("contract facet %q declares layer %q but its owning component %q "+
				"(via component field %q) is layer %q — a contract facet must share its "+
				"owning component's layer", key, c.Layer, owner.ID, c.Component, owner.Layer)
		}
	}
}
