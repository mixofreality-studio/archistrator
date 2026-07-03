//go:build methoddesign

package internal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// TestMethodDesignArtifacts runs the platform methodcheck gate over archistrator's
// OWN committed `.aiarch/state/project.json` — archistrator eats its own dog food
// exactly as a downstream consumer repo would via the generated
// aiarch_method_test.go.tmpl harness. methodcheck.Check is the all-in-one Method
// validator: in this configuration it runs the Method-invariant DESIGN rules over
// the committed JSON (including the C4 deployment DEP-* rules), which are GREEN
// against the published framework-go v0.4.0 that decodes the C4 container model.
//
// The layer rules (TestMethodLayering) and the encapsulation gate
// (TestGeneratedOnlyPublic) already run in the DEFAULT `go test ./...` suite via
// arch_test.go, so CI already covers them.
//
// SCOPE — the design↔code ALIGNMENT + conformance passes are NOW WIRED (framework-go
// v0.4.1). Three mechanisms shipped that closed the previously-open buckets:
//
//  1. StereotypeSuffixNormalizer (NameNormalizer) strips ONE trailing Method
//     stereotype suffix so the design's stereotyped names (WebClient, ReviewEngine,
//     ProjectStateAccess) match the bare code leaves (client/web, engine/review,
//     resourceaccess/projectstate).
//  2. Layer-scoped matching (compKey = name+layer) keeps same-named leaves in
//     different layers distinct — manager/settlement vs engine/settlement no longer
//     collide once both normalize to "settlement".
//  3. Component.buildStatus (planned/external) gives the align rule its
//     not-yet-built mechanism: MCPClient/SchedulerClient/WorkItemAccess are
//     buildStatus=planned (skip missing-pkg; ALIGN-STALE-PLANNED only once code
//     lands) and the three Utilities (Security/Logging/Diagnostics) are
//     buildStatus=external (framework-provided; ALIGN-EXTERNAL-* provenance).
//
// The architect's naming reconciliation (usagelog rename, billing removed from the
// System) plus the buildStatus markers make this gate GREEN: zero alignment Errors.
//
// Build-tagged OUT of the default run: CI can invoke it explicitly via
// `go test -tags methoddesign ./internal/ -run TestMethodDesignArtifacts` against
// the PUBLISHED framework-go v0.4.1.
func TestMethodDesignArtifacts(t *testing.T) {
	root := findRepoRoot(t)
	methodcheck.Check(t, methodcheck.ProjectSpec{
		RepoRoot: root,
		// FULL gate: design rules + layer rules + the design↔code ALIGNMENT +
		// conformance passes. Arch + EncapsulationAllowlist are the same helpers
		// arch_test.go passes to arch.Check / arch.CheckGeneratedSurface.
		Arch:                   appArchSpec(),
		EncapsulationAllowlist: encapAllowlist(),
		// StereotypeSuffixNormalizer strips one trailing Method stereotype suffix
		// (access|engine|manager|client) so the design's stereotyped component
		// names (ProjectStateAccess, ReviewEngine) match the bare package leaves
		// (projectstate, review); layer-scoped compKey keeps same-named leaves in
		// different layers distinct. framework-go v0.4.1.
		NameNormalizer: methodcheck.StereotypeSuffixNormalizer,
	})
}

// findRepoRoot ascends from the current working directory until it finds a
// directory containing `.aiarch/state/project.json` — the repo root. It does
// NOT hardcode a relative "../.." (the test binary's working directory is not
// a stable API) and does NOT use runtime.Caller (which would resolve against
// the source tree at build time, not the checkout being validated).
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for {
		candidate := filepath.Join(dir, ".aiarch", "state", "project.json")
		if _, err := os.Stat(candidate); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (.aiarch/state/project.json) ascending from %s", dir)
		}
		dir = parent
	}
}
