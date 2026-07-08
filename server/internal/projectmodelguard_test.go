package internal_test

import (
	"os"
	"path/filepath"
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
