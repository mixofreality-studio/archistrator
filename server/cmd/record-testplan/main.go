// Command record-testplan is the one-off single-writer for one component's test plan:
// it strictly decodes {"testPlan": <record>} as the PhaseArtifactPayload the aiarch-state MCP
// recordPhaseArtifact verb takes, splices the record into .phaseArtifacts.testPlan[<component>],
// re-decodes the whole document through the projectstate codec, then runs the Method
// coherence check and refuses the write when the record leaves any TP-* error for that
// component, or introduces any error the baseline did not have.
//
// The whole-document gate is not applied as-is because, while the per-component plans are
// recorded one at a time, the other components' TP-BOUND errors are expected: scoping to
// "this component's TP-* rules plus no new errors" is the per-writer gate.
//
// Usage: record-testplan <project.json> <component> <record.json>
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "REJECT:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: record-testplan <project.json> <component> <record.json>")
	}
	path, component, recPath := args[0], args[1], args[2]
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rec, err := os.ReadFile(recPath)
	if err != nil {
		return err
	}
	var pl projectstate.PhaseArtifactPayload
	if err := json.Unmarshal([]byte(`{"testPlan":`+string(rec)+`}`), &pl); err != nil {
		return fmt.Errorf("record does not conform to PhaseArtifactPayload: %w", err)
	}
	if pl.TestPlan == nil || pl.TestPlan.Component != component {
		return fmt.Errorf("record's component does not name %s", component)
	}
	baseline, err := methodcheck.ValidateProjectJSON(raw)
	if err != nil {
		return fmt.Errorf("baseline coherence check: %w", err)
	}
	out, err := splice(raw, component, rec)
	if err != nil {
		return err
	}
	if _, ok, err := projectstate.DecodeProjectJSON(out, ""); err != nil || !ok {
		return fmt.Errorf("read-back decode: %v (found=%v)", err, ok)
	}
	after, err := methodcheck.ValidateProjectJSON(out)
	if err != nil {
		return fmt.Errorf("coherence check: %w", err)
	}
	seen := map[string]int{}
	for _, f := range baseline {
		if f.Severity == methodcheck.SeverityError {
			seen[key(f)]++
		}
	}
	var bad []methodcheck.Finding
	for _, f := range after {
		if f.Severity != methodcheck.SeverityError {
			continue
		}
		if strings.HasPrefix(string(f.RuleID), "TP-") && mentions(f, component) {
			bad = append(bad, f)
			continue
		}
		if seen[key(f)] > 0 {
			seen[key(f)]--
			continue
		}
		bad = append(bad, f)
	}
	if len(bad) > 0 {
		var b strings.Builder
		for _, f := range bad {
			fmt.Fprintf(&b, "  - %s\n", key(f))
		}
		return fmt.Errorf("%d finding(s) for %s:\n%s", len(bad), component, b.String())
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return err
	}
	fmt.Printf("OK: recorded testPlan[%s]\n", component)
	return nil
}

func key(f methodcheck.Finding) string {
	loc := ""
	if f.Location != nil {
		loc = f.Location.Section
	}
	return fmt.Sprintf("[%s] (%s) %s", f.RuleID, loc, f.Message)
}

func mentions(f methodcheck.Finding, component string) bool {
	if f.Location != nil && strings.Contains(f.Location.Section, "testPlan."+component) {
		return true
	}
	return strings.HasPrefix(f.Message, component+" ")
}

// splice replaces the text of .phaseArtifacts.testPlan[<component>] in place with the
// record exactly as given (re-indented, same escaping as the record), leaving every other
// byte of the committed document untouched. A codec decode/encode round-trip is NOT
// used for the write: it is lossy against the committed document (it drops fields and the
// project id), so only the gate reads through the codec.
func splice(raw []byte, component string, rec []byte) ([]byte, error) {
	doc := string(raw)
	const section = "\n    \"testPlan\": {\n"
	at := strings.Index(doc, section)
	if at < 0 {
		return nil, fmt.Errorf("no .phaseArtifacts.testPlan object in the document")
	}
	open := "\n      \"" + component + "\": {"
	start := strings.Index(doc[at:], open)
	if start < 0 {
		return nil, fmt.Errorf("no testPlan entry for %s to replace", component)
	}
	start += at + 1
	end := strings.Index(doc[start:], "\n      }")
	if end < 0 {
		return nil, fmt.Errorf("unterminated testPlan entry for %s", component)
	}
	end += start + len("\n      }")
	var b bytes.Buffer
	if err := json.Indent(&b, bytes.TrimSpace(rec), "      ", "  "); err != nil {
		return nil, err
	}
	entry := "      \"" + component + "\": " + b.String()
	return []byte(doc[:start] + entry + doc[end:]), nil
}
