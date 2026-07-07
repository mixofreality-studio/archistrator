package main

// validate.go implements the one-shot `aiarch-state-mcp validate` subcommand — the
// Method-invariant REQUIRED CI check the seated design workflow runs on every design PR
// (it replaced the seated `go test ./...` gate, 2026-07-06). Like `reconcile`, it is a
// THIN CLI seam over ProjectStateAccess code: it reads the checkout's committed
// .aiarch/state/project.json, decodes it through the STRICT server codec (read-back
// parity), runs the identical methodcheck design rules putDraftModel enforces in-loop,
// and applies the GATE SEVERITY POLICIES (staleness.go): the staleness-aware
// cross-artifact downgrade plus — when `--slot` names the job's ambient artifact — the
// SLOT-SCOPED downgrade, so an amendment is judged only on its own slot's coherence and
// the structural rules, never deadlocked by pre-existing defects on sibling slots it
// cannot write. Without `--slot` the gate runs in whole-document mode (staleness only)
// for standalone use.
//
// WHY A SUBCOMMAND AND NOT THE SEATED go test: the enforcement stack must be
// SELF-UPDATING. The seated aiarch_method_test.go resolves methodcheck through the
// seated go.mod's framework-go pin, so every rule/severity change needed a platform
// release + a product go.mod bump — the drift class the managed-scaffold sync exists to
// kill. This binary already ships the whole app-side validation stack at the
// StateMcpModulePin the workflow installs, and the sync-on-dispatch refreshes seated
// workflows whenever that pin moves — one pin, one gate, no drift. (The seated
// go.mod/aiarch_method_test.go scaffold remains for the product repo's OWN CI once it
// has Go code: the arch layer rules + design↔code alignment need go/packages over the
// product module, which an installed binary cannot run.)
//
// Verdict rule (identical to methodcheck.Check and putDraftModel): only surviving
// SeverityError findings fail; Warnings/Info ride along advisory. Exit is non-zero only
// on surviving errors (or an unreadable/undecodable/incoherent state document).

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// runValidate parses the validate flags, runs the policy-aware Method gate over the
// checkout at --root (default "."), writes the gate log to out, and returns a non-nil
// error iff the gate fails (surviving Error findings, or a broken state document).
//
// Flags:
//
//	--root <dir>       the checked-out repo root containing .aiarch/state/project.json
//	--slot <artifact>  the session's AMBIENT artifact slot (e.g. "System" or
//	                   "systemDesign"; any form parseArtifactKind accepts). When set,
//	                   the slot-scoped severity policy applies: errors attributed to
//	                   OTHER slots are pre-existing committed data this session cannot
//	                   write and downgrade to annotated warnings. Empty/absent = the
//	                   whole-document gate (staleness policy only).
func runValidate(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root containing "+filepath.Join(statePathPrefix, projectFile))
	slot := fs.String("slot", "", "ambient artifact slot for slot-scoped severity (empty = whole-document)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var ambient projectstate.ArtifactKind
	hasAmbient := false
	if s := strings.TrimSpace(*slot); s != "" {
		k, kerr := parseArtifactKind(s)
		if kerr != nil {
			return fmt.Errorf("--slot: %w", kerr)
		}
		ambient, hasAmbient = k, true
	}

	path := filepath.Join(*root, statePathPrefix, projectFile)
	raw, err := os.ReadFile(path) //nolint:gosec // path is the caller's checkout root joined to a constant
	if err != nil {
		if os.IsNotExist(err) {
			// Mirror methodcheck.Check's posture: a repo with no committed .aiarch state
			// has nothing to validate — a clean pass, never a red gate.
			logf(out, "aiarch-state validate: no %s under %s — nothing to validate (clean pass)\n",
				filepath.Join(statePathPrefix, projectFile), *root)
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	// STRICT server-codec decode (read-back parity — the same decode the server and
	// putDraftModel run). A committed document that does not decode is malformed
	// committed state: a hard failure, exactly as the server read-back would treat it.
	// The zero ProjectID is fine: the rules read the typed slot models, never the id.
	proj, ok, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil {
		return fmt.Errorf("the committed project state does not decode (the server would reject it on read-back): %w", err)
	}
	if !ok {
		logf(out, "aiarch-state validate: %s is empty — nothing to validate (clean pass)\n",
			filepath.Join(statePathPrefix, projectFile))
		return nil
	}

	// The identical Method-invariant rule set the seated go-test gate ran
	// (methodcheck.ValidateProjectJSON), then the gate severity policies (staleness.go):
	// the staleness-aware cross-artifact downgrade always; the slot-scoped downgrade
	// when --slot supplied the session's ambient artifact.
	findings, ferr := methodcheck.ValidateProjectJSON(raw)
	if ferr != nil {
		return fmt.Errorf("the committed state is not a coherent artifact set: %w", ferr)
	}
	findings = applyGateSeverityPolicies(proj, ambient, hasAmbient, findings)

	errCount := 0
	for _, f := range findings {
		logf(out, "%s\n", formatGateFinding(f))
		if f.Severity == methodcheck.SeverityError {
			errCount++
		}
	}
	if errCount > 0 {
		return fmt.Errorf("%d Method rule violation(s) (of %d finding(s)) — fix the design before merge", errCount, len(findings))
	}
	logf(out, "aiarch-state validate: PASS (%d advisory finding(s), 0 errors)\n", len(findings))
	return nil
}

// logf writes one gate-log line, deliberately discarding the writer error: the log is
// diagnostics; the gate verdict travels exclusively through runValidate's return value.
func logf(out io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(out, format, args...)
}

// formatGateFinding renders one finding in the gate-log shape the old go-test gate
// printed (severity, rule id, message, locus) so run logs stay comparable across the
// gate migration.
func formatGateFinding(f methodcheck.Finding) string {
	var sev string
	switch f.Severity {
	case methodcheck.SeverityError:
		sev = "ERROR"
	case methodcheck.SeverityWarning:
		sev = "WARNING"
	case methodcheck.SeverityInfo:
		sev = "INFO"
	default:
		sev = "INFO"
	}
	at := ""
	if f.Location != nil && f.Location.Section != "" {
		at = "  (at " + f.Location.Section + ")"
	}
	return fmt.Sprintf("%-7s  [%s]  %s%s", sev, f.RuleID, f.Message, at)
}
