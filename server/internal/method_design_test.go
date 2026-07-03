//go:build methoddesign

package internal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// TestMethodDesignArtifacts runs the platform methodcheck DESIGN-rule gate over
// archistrator's OWN committed `.aiarch/state/project.json` — i.e. archistrator
// eats its own dog food, checking its Method state against every applicable
// design verb (including the deployment DEP-* rules) exactly as a downstream
// consumer repo would via the generated aiarch_method_test.go.tmpl harness.
//
// Build-tagged OUT of the default `go test ./...` / `make test` / `make
// test-short` run: CI builds this module GOWORK=off against the PUBLISHED
// framework-go release. Tasks 1-2 reshaped the LOCAL (workspace) framework-go
// deployment structs/rules to a C4 container model, but the current
// project.json still carries the OLD deployment shape, so decoding it against
// the NEW rules is expected to fail (either a decode-shape error or DEP-*
// findings). Running this only in workspace mode (no GOWORK=off) lets us see
// that failure now, ahead of Task 8 rewriting project.json's deployment data to
// match the new shape and turning this gate green. A later release flips
// `-tags methoddesign` into CI once the framework-go release ships.
func TestMethodDesignArtifacts(t *testing.T) {
	root := findRepoRoot(t)
	path := filepath.Join(root, ".aiarch", "state", "project.json")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	findings, err := methodcheck.ValidateProjectJSON(raw)
	if err != nil {
		t.Fatalf("ValidateProjectJSON(%s): coherence fault: %v", path, err)
	}

	for _, f := range findings {
		loc := ""
		if f.Location != nil {
			loc = " [" + f.Location.Section + "]"
		}
		msg := string(f.RuleID) + ": " + f.Message + loc
		switch f.Severity {
		case methodcheck.SeverityError:
			t.Errorf("%s", msg)
		default: // SeverityWarning, SeverityInfo
			t.Logf("%s", msg)
		}
	}
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
