//go:build methoddesign

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// TestMethodActivityRecords runs the activity-record rules (activityrecords.go) over
// archistrator's OWN checkout and committed .aiarch/state/project.json — the dogfood
// half of `make method-check` for the rules methodcheck cannot run because they read the
// checkout: every component built here has its completion record, and every passed
// testing task stands on a qualifying CheckRun. Any REC-* Error fails it.
func TestMethodActivityRecords(t *testing.T) {
	root := committedStateRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, statePathPrefix, projectFile))
	if err != nil {
		t.Fatalf("read project state: %v", err)
	}
	proj, ok, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil || !ok {
		t.Fatalf("decode project state: ok=%v err=%v", ok, err)
	}
	findings, err := activityRecordFindings(proj, newCheckout(os.DirFS(root)), projectstate.ComponentScenarios)
	if err != nil {
		t.Fatalf("activity records: %v", err)
	}
	for _, f := range findings {
		if f.Severity == methodcheck.SeverityError {
			t.Errorf("%s", formatGateFinding(f))
		}
	}
}

// committedStateRoot ascends from the working directory to the directory holding the committed
// project state.
func committedStateRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, statePathPrefix, projectFile)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no %s above the working directory", filepath.Join(statePathPrefix, projectFile))
		}
		dir = parent
	}
}
