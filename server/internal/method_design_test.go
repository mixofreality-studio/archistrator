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
// SCOPE — the design↔code ALIGNMENT pass (ALIGN-*) is DELIBERATELY NOT wired here
// yet (Arch is left zero, so methodcheck.Check skips the layer/alignment/encapsulation
// phases). Turning it on against the current committed System (slot 5, 38 components)
// surfaces 53 alignment Errors that are NOT per-component bugs but a systemic
// mismatch requiring an architecture/framework decision, in three buckets:
//
//  1. STEREOTYPE-SUFFIX NAMING (~19): the design names components with the Method
//     stereotype suffix (WebClient, ReviewEngine, ProjectStateAccess) while the
//     code names package leaves bare (client/web, engine/review,
//     resourceaccess/projectstate). methodcheck's default normalizer (lowercase +
//     strip non-alnum) does NOT strip the suffix, so "projectstateaccess" !=
//     "projectstate". The framework's own convention (testdata/alignapp) names
//     leaves WITH the suffix (stateaccess, validatingengine).
//  2. NAME DIVERGENCE (~a dozen): design vs code disagree beyond the suffix —
//     BillingManager↔manager/settlement, ConstructionEstimationEngine↔engine/
//     estimation, BillingGatewayAccess↔resourceaccess/merchantgateway,
//     UsageAccess↔resourceaccess/usage, plus extra code packages the design
//     does not declare (engine/settlement, resourceaccess/{artifact,revenueledger,
//     settlementstate,worker}).
//  3. NOT-YET-BUILT (5): MCPClient, SchedulerClient, and the three Utilities
//     (Security/Logging/Diagnostics — provided by framework-go/utilities, not the
//     app's internal/) have no internal package. The align rule has NO documented
//     "planned/not-yet-built" mechanism (it excludes only Kind==Resource), so each
//     is reported ALIGN-MISSING-PKG.
//
// Flipping the full gate on is a ONE-LINE change here (add Arch: appArchSpec() and
// EncapsulationAllowlist: encapAllowlist(), both already defined in arch_test.go)
// once the naming reconciliation + a not-yet-built mechanism are decided. Until
// then this gate guards the design rules and stays green.
//
// Build-tagged OUT of the default run: CI can invoke it explicitly via
// `go test -tags methoddesign ./internal/ -run TestMethodDesignArtifacts` against
// the PUBLISHED framework-go v0.4.0.
func TestMethodDesignArtifacts(t *testing.T) {
	root := findRepoRoot(t)
	methodcheck.Check(t, methodcheck.ProjectSpec{
		RepoRoot: root,
		// Arch intentionally omitted — see SCOPE above. Adding
		//   Arch:                   appArchSpec(),
		//   EncapsulationAllowlist: encapAllowlist(),
		// turns on the full layer + encapsulation + alignment gate.
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
