package main

// reconcile.go implements the one-shot `aiarch-state-mcp reconcile` subcommand: the
// deterministic project.json merge-conflict resolver the design workflow's "Refresh the
// session branch from main" step invokes when `git merge origin/main` conflicts on
// .aiarch/state/project.json (F80). It reads the two conflicting versions of the file,
// reconciles them through projectstate.ReconcileSlotOntoBase (main's document with the
// session's OWN slot overlaid), and writes the resolved file so the workflow can `git add`
// + commit it to complete the merge — turning a RED dead-end into a clean reconciliation.
//
// It is a THIN CLI seam over ProjectStateAccess code: all state-file semantics live in
// projectstate; this only does argument parsing and file IO.

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// runReconcile parses the reconcile flags and writes the reconciled project.json.
//
// Flags:
//
//	--base <path>  main's project.json (the conflicted merge's THEIR side, stage :3)
//	--ours <path>  the session branch's project.json (the merge's OUR side, stage :2)
//	--out  <path>  where to write the reconciled document (the working-tree project.json)
//
// The artifact kind (which slot the session owns) is read from the ambient
// AIARCH_ARTIFACT_KIND env the workflow already stamps, so the caller never repeats it.
func runReconcile(getenv func(string) string, args []string) error {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	basePath := fs.String("base", "", "path to main's project.json (theirs)")
	oursPath := fs.String("ours", "", "path to the session branch's project.json (ours)")
	outPath := fs.String("out", "", "path to write the reconciled project.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*basePath) == "" || strings.TrimSpace(*oursPath) == "" || strings.TrimSpace(*outPath) == "" {
		return fmt.Errorf("--base, --ours and --out are all required")
	}

	kindStr := strings.TrimSpace(getenv(envArtifactKind))
	if kindStr == "" {
		return fmt.Errorf("%s is required (the ambient artifact kind whose slot this session owns) but was empty", envArtifactKind)
	}
	kind, err := parseArtifactKind(kindStr)
	if err != nil {
		return err
	}

	base, err := os.ReadFile(*basePath)
	if err != nil {
		return fmt.Errorf("read base (main) project.json: %w", err)
	}
	ours, err := os.ReadFile(*oursPath)
	if err != nil {
		return fmt.Errorf("read session-branch project.json: %w", err)
	}

	projectID := projectstate.ProjectID(strings.TrimSpace(getenv(envProjectID)))
	reconciled, err := projectstate.ReconcileSlotOntoBase(base, ours, projectID, kind)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*outPath, reconciled, 0o600); err != nil {
		return fmt.Errorf("write reconciled project.json: %w", err)
	}
	fmt.Fprintf(os.Stderr, "aiarch-state-mcp: reconciled %s slot from session branch onto main's project.json\n", kind)
	return nil
}
