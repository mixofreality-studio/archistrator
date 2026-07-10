// cmd/gen-uitests-fixtures refreshes uitests/testdata/coreUseCasesProject.json's
// coreUseCases SLOT content from this repo's own committed
// `.aiarch/state/project.json` (main branch) — the "REAL committed model
// extracted from this repo's dogfood project.json" the fixture's own doc
// comment (uitests/tests/support/designStubs.ts, stubCommittedCoreUseCases)
// already claims, now backed by a mechanical regen instead of a one-time
// manual copy-paste.
//
// It reads through the SAME sanctioned projectstate.ProjectStateAccess path
// the server itself uses (NewGitLocalProjectStateAccess + ReadProject) rather
// than re-parsing the on-disk project.json storage encoding by hand — that
// encoding is projectstate's own internal concern, not a stable contract, so
// hand-decoding it here would just relocate the hand-mirror problem this
// cleanup is closing.
//
// Only the coreUseCases SLOT (stage/revisions/model) is refreshed; the
// fixture's outer envelope (ProjectID/Name/Owner/Phase/Version/Research) is a
// deliberately synthetic test identity ("walkthrough-fixture", NOT
// archistrator's own project id) — untouched, read back from the existing
// fixture file and re-emitted as-is.
//
// Usage (matching the uitests package.json `regen:core-use-cases-fixture` /
// `check:core-use-cases-fixture` scripts, run from server/):
//
//	GOWORK=off go run ./cmd/gen-uitests-fixtures \
//	  -repo .. -fixture ../uitests/testdata/coreUseCasesProject.json -project archistrator
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// wireProject mirrors the SystemDesignProjectState wire envelope's field
// names/order (server/api/openapi.yaml) — the same shape the SPA's
// mapProjectState decodes and this fixture stubs directly over the wire.
type wireProject struct {
	ProjectID string          `json:"ProjectID"`
	Name      string          `json:"Name"`
	Owner     string          `json:"Owner"`
	Phase     int             `json:"Phase"`
	Version   int             `json:"Version"`
	Research  json.RawMessage `json:"Research"`
	Slots     []wireSlot      `json:"Slots"`
}

type wireSlot struct {
	Kind      string        `json:"kind"`
	Stage     int           `json:"stage"`
	Revisions int64         `json:"revisions"`
	Model     wireSlotModel `json:"model"`
}

type wireSlotModel struct {
	Kind  string          `json:"kind"`
	Model json.RawMessage `json:"model"`
}

func main() {
	repo := flag.String("repo", "..", "path to the archistrator repo root (reads the committed main-branch project.json)")
	fixture := flag.String("fixture", "../uitests/testdata/coreUseCasesProject.json", "path to the uitests fixture to refresh")
	projectID := flag.String("project", "archistrator", "project id to read")
	flag.Parse()

	if err := run(*repo, *fixture, *projectID); err != nil {
		fmt.Fprintf(os.Stderr, "gen-uitests-fixtures: %v\n", err)
		os.Exit(1)
	}
}

func run(repo, fixturePath, projectID string) error {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return fmt.Errorf("resolving repo path: %w", err)
	}

	existing, err := os.ReadFile(fixturePath) //nolint:gosec // dev-tool, path is a flag by design
	if err != nil {
		return fmt.Errorf("reading existing fixture: %w", err)
	}
	var fixtureDoc wireProject
	if err := json.Unmarshal(existing, &fixtureDoc); err != nil {
		return fmt.Errorf("decoding existing fixture: %w", err)
	}
	if len(fixtureDoc.Slots) == 0 {
		return fmt.Errorf("fixture %s has no slots to refresh", fixturePath)
	}

	psa := projectstate.NewGitLocalProjectStateAccess("file://" + absRepo)
	proj, err := psa.ReadProject(fwra.Context{Context: context.Background()}, projectstate.ProjectID(projectID))
	if err != nil {
		return fmt.Errorf("reading committed project %q: %w", projectID, err)
	}

	modelJSON, err := json.Marshal(proj.CoreUseCases.Model)
	if err != nil {
		return fmt.Errorf("marshaling coreUseCases model: %w", err)
	}

	kind := projectstate.KindCoreUseCases.WireName()
	fixtureDoc.Slots[0] = wireSlot{
		Kind:      kind,
		Stage:     int(proj.CoreUseCases.Status),
		Revisions: int64(proj.CoreUseCases.Revisions),
		Model:     wireSlotModel{Kind: kind, Model: modelJSON},
	}

	out, err := json.Marshal(fixtureDoc)
	if err != nil {
		return fmt.Errorf("marshaling fixture: %w", err)
	}
	return os.WriteFile(fixturePath, out, 0o644) //nolint:gosec // generated test fixture, not a secret
}
