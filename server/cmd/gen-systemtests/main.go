// cmd/gen-systemtests MECHANICALLY generates the systemtests wire-test step
// tables from the committed System Test Plan
// (.aiarch/state/project.json → .testingState.systemTestPlan) — so the plan
// (what the test-engineer says will break the system) and the harness table
// that drives it can never drift apart.
//
// It lives in the SERVER module because it decodes project.json via the
// projectstate ResourceAccess (the systemtests module, by the test-authoring
// constitution — systemtests/constitution — imports nothing from server: R1/R3).
// This tool is NEVER imported by systemtests; it only WRITES *.gen.go +
// manifest.json files into systemtests/internal/generated. Every emitted file
// references only the hand-written types.go in that SAME package — zero
// imports, so generated output trivially satisfies "stdlib only" without even
// needing stdlib.
//
// One file per scenario (e.g. stp_uc1_table.gen.go), holding one []StepCase
// table per TestCase in that scenario plus an init() that registers each case
// (by CaseID) and the scenario's authored case order into the shared
// generated.Registry / generated.ScenarioOrder. A case/scenario removed from
// the plan makes its file an ORPHAN in the output dir; pruneStale deletes it
// so a `make gen` never leaves stale generated files behind.
//
// Usage (from systemtests/, matching the Makefile `gen` / `gen-check` targets):
//
//	GOWORK=off go run ../server/cmd/gen-systemtests \
//	  -project ../.aiarch/state/project.json \
//	  -out ./internal/generated
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func main() {
	projectPath := flag.String("project", "../.aiarch/state/project.json", "path to project.json")
	outDir := flag.String("out", "../systemtests/internal/generated", "output directory (systemtests/internal/generated)")
	projectID := flag.String("id", "archistrator", "project id used to decode project.json")
	flag.Parse()

	if err := run(*projectPath, *outDir, *projectID); err != nil {
		fmt.Fprintf(os.Stderr, "gen-systemtests: %v\n", err)
		os.Exit(1)
	}
}

// run is the whole generator pipeline: load the plan, render one source file
// per scenario, prune orphans, write the fresh files + manifest. Kept as a
// flat sequence (no branching beyond the load error) to stay well under the
// complexity gate — each step's own logic lives in its own function.
func run(projectPath, outDir, projectID string) error {
	plan, err := loadPlan(projectPath, projectID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil { // #nosec G301 -- generator output dir, no trust boundary
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	entries, err := buildEntries(plan)
	if err != nil {
		return err
	}
	if err := pruneStale(outDir, entries); err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.WriteFile(filepath.Join(outDir, e.FileName), e.Source, 0o600); err != nil { // #nosec G304 -- path is generator-owned, built from a scenario id in the committed plan
			return fmt.Errorf("write %s: %w", e.FileName, err)
		}
	}
	if err := writeManifest(outDir, entries); err != nil {
		return err
	}
	fmt.Printf("gen-systemtests: wrote %d scenario file(s) to %s\n", len(entries), outDir)
	return nil
}

// loadPlan decodes project.json and returns the committed SystemTestPlan, or
// an EMPTY (non-nil) plan when no testing state / plan has been committed yet
// — the generator then prunes any previously-generated files and writes an
// empty manifest, staying idempotent rather than erroring or no-op'ing on a
// dir it should have cleaned.
func loadPlan(path, id string) (projectstate.SystemTestPlan, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the -project CLI flag to a local codegen tool, no trust boundary
	if err != nil {
		return projectstate.SystemTestPlan{}, fmt.Errorf("read %s: %w", path, err)
	}
	proj, ok, err := projectstate.DecodeProjectJSON(raw, projectstate.ProjectID(id))
	if err != nil {
		return projectstate.SystemTestPlan{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if !ok || proj.TestingState == nil || proj.TestingState.SystemTestPlan == nil {
		return projectstate.SystemTestPlan{}, nil
	}
	return *proj.TestingState.SystemTestPlan, nil
}

// scenarioEntry is the generator's in-memory record of one scenario's
// rendered output — the thing pruneStale/writeManifest/run all key off.
type scenarioEntry struct {
	ID       string
	FileName string
	CaseIDs  []string
	Hash     string
	Source   []byte
}

// buildEntries renders one scenarioEntry per TestScenario, sorted by
// ScenarioID for deterministic (byte-stable) output regardless of the
// authored JSON array's on-disk order.
func buildEntries(plan projectstate.SystemTestPlan) ([]scenarioEntry, error) {
	scenarios := append([]projectstate.TestScenario(nil), plan.Scenarios...)
	sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].ID < scenarios[j].ID })

	entries := make([]scenarioEntry, 0, len(scenarios))
	for _, sc := range scenarios {
		src, caseIDs, err := renderScenarioFile(sc)
		if err != nil {
			return nil, fmt.Errorf("render scenario %s: %w", sc.ID, err)
		}
		entries = append(entries, scenarioEntry{
			ID:       sc.ID,
			FileName: scenarioFileName(sc.ID),
			CaseIDs:  caseIDs,
			Hash:     hashScenario(sc),
			Source:   src,
		})
	}
	return entries, nil
}

// scenarioFileName derives "stp_uc1_table.gen.go" from "STP-UC1": lowercase,
// non-alnum runs collapsed to underscore.
func scenarioFileName(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String() + "_table.gen.go"
}

// caseVarName derives an unexported Go identifier from a CaseID, e.g.
// "STP-UC1-H1" -> "stpUC1H1" (first hyphen-segment lowercased, the rest
// concatenated verbatim — case IDs are already authored in a Go-identifier-
// safe alphabet).
func caseVarName(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) == 0 || parts[0] == "" {
		return "case"
	}
	var b strings.Builder
	b.WriteString(strings.ToLower(parts[0]))
	for _, p := range parts[1:] {
		b.WriteString(p)
	}
	return b.String()
}

// hashScenario is the content hash embedded in the generated file's header
// comment: sha256 of the scenario's canonical (Go-struct-field-ordered) JSON
// encoding. Two generator runs over an unchanged plan produce the identical
// hash and therefore identical bytes — the byte-stable idempotency guarantee
// the drift check (`make gen-check`) relies on.
func hashScenario(sc projectstate.TestScenario) string {
	raw, err := json.Marshal(sc)
	if err != nil {
		return "" // best-effort; a header-comment provenance aid, not load-bearing
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// renderScenarioFile builds one scenario's *.gen.go source (header + one
// []StepCase table per case + the registering init()) and canonicalizes it
// through go/format — the generator's own gofmt, so `make gen` never needs a
// separate format pass and the drift check compares apples to apples.
func renderScenarioFile(sc projectstate.TestScenario) ([]byte, []string, error) {
	var b strings.Builder
	caseIDs := make([]string, 0, len(sc.Cases))
	varNames := make([]string, 0, len(sc.Cases))

	fmt.Fprintf(&b, "// Code generated by gen-systemtests; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// source: %s (source-hash sha256:%s)\n", sc.ID, hashScenario(sc))
	b.WriteString("//\n// Generated from the committed System Test Plan\n")
	b.WriteString("// (.aiarch/state/project.json .testingState.systemTestPlan) by\n")
	b.WriteString("// server/cmd/gen-systemtests. Regenerate with `make gen` (systemtests/Makefile);\n")
	b.WriteString("// `make gen-check` fails CI on drift between this file and the committed plan.\n")
	b.WriteString("package generated\n\n")

	for _, c := range sc.Cases {
		varName := caseVarName(c.ID)
		varNames = append(varNames, varName)
		caseIDs = append(caseIDs, c.ID)
		renderCaseTable(&b, sc, c, varName)
	}
	renderInit(&b, sc.ID, caseIDs, varNames)

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, nil, fmt.Errorf("gofmt generated source: %w", err)
	}
	return formatted, caseIDs, nil
}

// renderCaseTable writes one `var <name> = []StepCase{ ... }` table — one
// StepCase row per TestStep in the case, in Seq order.
func renderCaseTable(b *strings.Builder, sc projectstate.TestScenario, c projectstate.TestCase, varName string) {
	steps := append([]projectstate.TestStep(nil), c.Steps...)
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Seq < steps[j].Seq })

	fmt.Fprintf(b, "var %s = []StepCase{\n", varName)
	for _, st := range steps {
		renderStep(b, sc, c, st)
	}
	b.WriteString("}\n\n")
}

// renderStep writes one StepCase composite literal, denormalizing the
// owning scenario/case identity onto every step row. Zero-value fields
// (empty Inputs, ExpectResult, etc.) are omitted from the literal — Go
// defaults them — so the emitted source stays proportional to what the plan
// actually specified for that step.
func renderStep(b *strings.Builder, sc projectstate.TestScenario, c projectstate.TestCase, st projectstate.TestStep) {
	b.WriteString("\t{\n")
	fmt.Fprintf(b, "\t\tScenarioID: %q,\n", sc.ID)
	fmt.Fprintf(b, "\t\tUseCase: %q,\n", sc.UseCase)
	fmt.Fprintf(b, "\t\tCaseID: %q,\n", c.ID)
	fmt.Fprintf(b, "\t\tCaseKind: %q,\n", c.Kind)
	fmt.Fprintf(b, "\t\tCaseTitle: %q,\n", c.Title)
	fmt.Fprintf(b, "\t\tSeq: %d,\n", st.Seq)
	fmt.Fprintf(b, "\t\tComponent: %q,\n", st.Component)
	fmt.Fprintf(b, "\t\tOperation: %q,\n", st.Operation)
	renderInputs(b, st.Inputs)
	if st.Expect.Result != "" {
		fmt.Fprintf(b, "\t\tExpectResult: %q,\n", st.Expect.Result)
	}
	if st.Expect.ErrorExpected {
		b.WriteString("\t\tExpectError: true,\n")
	}
	if st.Expect.ErrorCode != "" {
		fmt.Fprintf(b, "\t\tExpectedErrorCode: %q,\n", st.Expect.ErrorCode)
	}
	if st.Assertion != "" {
		fmt.Fprintf(b, "\t\tAssertion: %q,\n", st.Assertion)
	}
	b.WriteString("\t},\n")
}

// renderInputs writes the Inputs: []InputArg{...} field, in the plan's
// authored order (argument order is part of the authored record).
func renderInputs(b *strings.Builder, inputs []projectstate.TestArg) {
	if len(inputs) == 0 {
		return
	}
	b.WriteString("\t\tInputs: []InputArg{\n")
	for _, in := range inputs {
		fmt.Fprintf(b, "\t\t\t{Name: %q, Value: %q", in.Name, in.Value)
		if in.SchemaRef != "" {
			fmt.Fprintf(b, ", SchemaRef: %q", in.SchemaRef)
		}
		b.WriteString("},\n")
	}
	b.WriteString("\t\t},\n")
}

// renderInit writes the file-level init() that registers every case's table
// (by CaseID) and the scenario's authored case order into the shared
// generated.Registry / generated.ScenarioOrder.
func renderInit(b *strings.Builder, scenarioID string, caseIDs, varNames []string) {
	b.WriteString("func init() {\n")
	for i, id := range caseIDs {
		fmt.Fprintf(b, "\tregister(%q, %s)\n", id, varNames[i])
	}
	fmt.Fprintf(b, "\tregisterScenario(%q, []string{", scenarioID)
	for i, id := range caseIDs {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(b, "%q", id)
	}
	b.WriteString("})\n")
	b.WriteString("}\n")
}

// pruneStale deletes every previously-generated *_table.gen.go file in outDir
// that no longer corresponds to a scenario in the current plan — a case or
// whole scenario removed from the System Test Plan must not leave an orphan
// table behind for the runner (or a stale drift-check baseline) to trip over.
// Scoped to the *_table.gen.go glob only: it never touches the hand-written
// types.go or manifest.json (rewritten separately, always).
func pruneStale(outDir string, entries []scenarioEntry) error {
	expected := make(map[string]bool, len(entries))
	for _, e := range entries {
		expected[e.FileName] = true
	}
	matches, err := filepath.Glob(filepath.Join(outDir, "*_table.gen.go"))
	if err != nil {
		return fmt.Errorf("glob %s: %w", outDir, err)
	}
	for _, m := range matches {
		if expected[filepath.Base(m)] {
			continue
		}
		if err := os.Remove(m); err != nil {
			return fmt.Errorf("prune stale %s: %w", m, err)
		}
		fmt.Printf("gen-systemtests: pruned stale %s (case/scenario removed from plan)\n", filepath.Base(m))
	}
	return nil
}

// manifestDoc is the manifest.json shape: which generator-owned file, holding
// which case ids, backs each scenario — the record pruneStale's NEXT run (and
// a human) uses to see what the generator currently owns. Deliberately holds
// NO wall-clock timestamp: manifest.json is part of the drift-checked output,
// so it must be exactly as byte-stable as the *.gen.go files it describes.
type manifestDoc struct {
	Scenarios map[string]manifestScenario `json:"scenarios"`
}

type manifestScenario struct {
	File    string   `json:"file"`
	CaseIDs []string `json:"caseIds"`
	Hash    string   `json:"sourceHash"`
}

func writeManifest(outDir string, entries []scenarioEntry) error {
	doc := manifestDoc{Scenarios: make(map[string]manifestScenario, len(entries))}
	for _, e := range entries {
		doc.Scenarios[e.ID] = manifestScenario{File: e.FileName, CaseIDs: e.CaseIDs, Hash: e.Hash}
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	raw = append(raw, '\n')
	path := filepath.Join(outDir, "manifest.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil { // #nosec G304 -- path is generator-owned, fixed filename under -out
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}
