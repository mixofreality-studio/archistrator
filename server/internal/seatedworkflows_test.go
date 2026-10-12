package internal_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// reseatEnv, when set to 1, makes TestSeatedWorkflowsEqualTheTemplates write the rendered
// bytes over the seated files instead of failing. It is the re-seat command.
const reseatEnv = "ATS_RESEAT_WORKFLOWS"

// R55 (final review I4): the workflows this repo itself runs (.github/workflows/
// aiarch-construct.yml and aiarch-design.yml) are SEATED copies of the workspace
// method-assets templates, and a template change that is not re-seated leaves the repo
// running stale jobs (A9, A10 and A11 each changed the templates after B5 seated them).
// This is the drift gate for those two files, the counterpart of the materialized-.claude
// gate (make claude-assets): the seated bytes must equal methodassets.ScaffoldFiles
// rendered with the same values the R3 re-seat used (AppSlug "", the in-source
// StateMcpModulePath / StateMcpModulePin, the values sync-on-dispatch renders with).
//
// StateMcpModulePin stays at its current value here. R50 moves it at D3, and the
// re-seat runs again then (ATS_RESEAT_WORKFLOWS=1, below).
func TestSeatedWorkflowsEqualTheTemplates(t *testing.T) {
	files, err := methodassets.ScaffoldFiles(methodassets.ScaffoldData{
		StateMcpModulePath:    sourcecontrol.StateMcpModulePath,
		StateMcpModuleVersion: sourcecontrol.StateMcpModulePin,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, wf := range []string{".github/workflows/aiarch-construct.yml", ".github/workflows/aiarch-design.yml"} {
		want, ok := files[wf]
		if !ok || len(want) == 0 {
			t.Fatalf("the scaffold renders no %s", wf)
		}
		seated := filepath.Join("..", "..", filepath.FromSlash(wf))
		if os.Getenv(reseatEnv) == "1" {
			if err := os.WriteFile(seated, want, 0o644); err != nil { //nolint:gosec // a workflow file is world-readable
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(seated)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s has drifted from the workspace templates; re-seat with: cd server && %s=1 go test ./internal -run TestSeatedWorkflowsEqualTheTemplates",
				wf, reseatEnv)
		}
	}
}
