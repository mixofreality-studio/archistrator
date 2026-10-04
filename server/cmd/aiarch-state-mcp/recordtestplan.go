package main

// recordtestplan.go — the `record-test-plan` ONE-SHOT subcommand: the single writer
// for one component's test plan (deterministic component testing design, §3), used
// while the per-component plans are bound one at a time:
//
//	aiarch-state-mcp record-test-plan --project PATH --component C --record FILE
//
// FILE holds one TestPlan record. It is strictly decoded as the PhaseArtifactPayload
// the recordPhaseArtifact verb takes, spliced into .phaseArtifacts.testPlan[C], the
// whole document re-decoded through the projectstate codec, and the Method coherence
// check run. The write is refused when the record leaves any TP-* error for C, or
// introduces any error the baseline did not have.
//
// The whole-document gate is not applied as-is because, while the plans are recorded
// one at a time, the other components' TP-BOUND errors are expected: "this component's
// TP-* rules plus no new errors" is the per-writer gate.

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// runRecordTestPlan parses the flags, gates the record and writes the document.
func runRecordTestPlan(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("record-test-plan", flag.ContinueOnError)
	project := flags.String("project", "", "the project.json to write (e.g. .aiarch/state/project.json)")
	component := flags.String("component", "", "the component whose testPlan the record replaces")
	record := flags.String("record", "", "a file holding one TestPlan record (JSON)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	for _, f := range []struct{ name, value string }{
		{"project", *project}, {"component", *component}, {"record", *record},
	} {
		if strings.TrimSpace(f.value) == "" {
			return fmt.Errorf("--%s is required", f.name)
		}
	}
	comp := strings.TrimSpace(*component)
	raw, err := os.ReadFile(*project)
	if err != nil {
		return err
	}
	rec, err := os.ReadFile(*record)
	if err != nil {
		return err
	}
	out, err := gateTestPlanRecord(raw, comp, rec)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*project, out, 0o600); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "OK: recorded testPlan[%s]\n", comp)
	return err
}

// gateTestPlanRecord decodes the record, splices it into the document and runs the
// per-writer gate, returning the document to write.
func gateTestPlanRecord(raw []byte, component string, rec []byte) ([]byte, error) {
	var pl projectstate.PhaseArtifactPayload
	if err := json.Unmarshal([]byte(`{"testPlan":`+string(rec)+`}`), &pl); err != nil {
		return nil, fmt.Errorf("record does not conform to PhaseArtifactPayload: %w", err)
	}
	if pl.TestPlan == nil || pl.TestPlan.Component != component {
		return nil, fmt.Errorf("record's component does not name %s", component)
	}
	baseline, err := methodcheck.ValidateProjectJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("baseline coherence check: %w", err)
	}
	out, err := spliceTestPlan(raw, component, rec)
	if err != nil {
		return nil, err
	}
	if _, ok, err := projectstate.DecodeProjectJSON(out, ""); err != nil {
		return nil, fmt.Errorf("read-back decode: %w", err)
	} else if !ok {
		return nil, fmt.Errorf("read-back decode found no project document")
	}
	after, err := methodcheck.ValidateProjectJSON(out)
	if err != nil {
		return nil, fmt.Errorf("coherence check: %w", err)
	}
	if bad := newTestPlanErrors(baseline, after, component); len(bad) > 0 {
		var b strings.Builder
		for _, f := range bad {
			fmt.Fprintf(&b, "  - %s\n", findingKey(f))
		}
		return nil, fmt.Errorf("%d finding(s) for %s:\n%s", len(bad), component, b.String())
	}
	return out, nil
}

// newTestPlanErrors returns the errors in after that refuse the write: every TP-* error
// that names component, and every other error the baseline did not already have.
func newTestPlanErrors(baseline, after []methodcheck.Finding, component string) []methodcheck.Finding {
	seen := map[string]int{}
	for _, f := range baseline {
		if f.Severity == methodcheck.SeverityError {
			seen[findingKey(f)]++
		}
	}
	var bad []methodcheck.Finding
	for _, f := range after {
		if f.Severity != methodcheck.SeverityError {
			continue
		}
		if strings.HasPrefix(string(f.RuleID), "TP-") && findingNames(f, component) {
			bad = append(bad, f)
			continue
		}
		if seen[findingKey(f)] > 0 {
			seen[findingKey(f)]--
			continue
		}
		bad = append(bad, f)
	}
	return bad
}

// findingKey identifies a finding by rule, section and message.
func findingKey(f methodcheck.Finding) string {
	loc := ""
	if f.Location != nil {
		loc = f.Location.Section
	}
	return fmt.Sprintf("[%s] (%s) %s", f.RuleID, loc, f.Message)
}

// findingNames reports whether a finding is about component's test plan.
func findingNames(f methodcheck.Finding, component string) bool {
	if f.Location != nil && strings.Contains(f.Location.Section, "testPlan."+component) {
		return true
	}
	return strings.HasPrefix(f.Message, component+" ")
}

// spliceTestPlan replaces the text of .phaseArtifacts.testPlan[<component>] in place
// with the record exactly as given (re-indented, same escaping as the record), leaving
// every other byte of the committed document untouched. A codec decode/encode
// round-trip is NOT used for the write: it is lossy against the committed document (it
// drops fields and the project id), so only the gate reads through the codec.
func spliceTestPlan(raw []byte, component string, rec []byte) ([]byte, error) {
	doc := string(raw)
	const section = "\n    \"testPlan\": {\n"
	at := strings.Index(doc, section)
	if at < 0 {
		return nil, fmt.Errorf("no .phaseArtifacts.testPlan object in the document")
	}
	var b bytes.Buffer
	if err := json.Indent(&b, bytes.TrimSpace(rec), "      ", "  "); err != nil {
		return nil, err
	}
	entry := "      \"" + component + "\": " + b.String()
	open := "\n      \"" + component + "\": {"
	objEnd := strings.Index(doc[at+len(section)-1:], "\n    }")
	if objEnd < 0 {
		return nil, fmt.Errorf("unterminated .phaseArtifacts.testPlan object")
	}
	start := strings.Index(doc[at:at+len(section)-1+objEnd], open)
	if start < 0 {
		return insertTestPlan(doc, at+len(section)-1, component, entry)
	}
	start += at + 1
	end := strings.Index(doc[start:], "\n      }")
	if end < 0 {
		return nil, fmt.Errorf("unterminated testPlan entry for %s", component)
	}
	end += start + len("\n      }")
	return []byte(doc[:start] + entry + doc[end:]), nil
}

// insertTestPlan adds the entry for a component that has no testPlan key yet, at its
// sorted position among the object's keys (the committed document keeps them sorted).
// body is the index of the newline that opens the object's first key line.
func insertTestPlan(doc string, body int, component, entry string) ([]byte, error) {
	closeAt := strings.Index(doc[body:], "\n    }")
	if closeAt < 0 {
		return nil, fmt.Errorf("unterminated .phaseArtifacts.testPlan object")
	}
	closeAt += body
	const keyLine = "\n      \""
	for pos := body; pos < closeAt; {
		k := strings.Index(doc[pos:closeAt], keyLine)
		if k < 0 {
			break
		}
		k += pos
		name := doc[k+len(keyLine):]
		name = name[:strings.IndexByte(name, '"')]
		if name > component {
			return []byte(doc[:k+1] + entry + ",\n" + doc[k+1:]), nil
		}
		pos = k + len(keyLine)
	}
	return []byte(doc[:closeAt] + ",\n" + entry + doc[closeAt:]), nil
}
