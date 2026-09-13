// cmd/gen-uiprofiles emits the per-ActivityKind canonical lifecycle phase
// table (id/phase/name/weight/tasks) into
// webApp/src/components/construction/lifecycleTemplates.gen.ts, mechanically
// derived from internal/resourceaccess/projectstate/projectstateaccess.go's
// ProfileFor. This closes a webApp hand-mirror: lifecycleTemplates.ts used to
// hand-author its own per-kind phase breakdown, ported long ago from a frozen
// UX mock — see that file's header for the resolved discrepancy against the
// server's later-ratified canonical 5-phase Profile
// (Requirements/DetailedDesign/TestPlan/Construction/Integration, a WEIGHTED
// SUBSET per activity kind so the shared earned-value formula, Appendix A,
// stays uniform across kinds).
//
// This tool needs no project.json — Profile/ActivityType/TestingVariant are a
// closed, static enumeration, not project data. It writes id/phase/name/weight/
// exitCriterion/tasks. The exit criterion and each task's label are PER PROFILE
// (projectstate.ExitCriterionFor / TaskLabelFor): a test plan's construction phase
// is closed by a "Scenario Review", not by the book's "Code Review" (designer P1-7).
// The book's own task name still travels beside it as bookLabel.
//
// Testing emits all FIVE variant profiles the server carries (Plan/Harness/
// Perf/SystemTest/QAProcess — materially different weights) into their own
// consts plus the GENERATED_TESTING_VARIANTS record below, so a testing
// activity's rendered lifecycle matches its actual variant instead of always
// falling back to the Plan (N-STP) shape. Testing is 7 of 40 committed
// activities, so this mattered for most of them.
//
// Every phase also carries its Figure A-1 task vocabulary (TasksForPhase) —
// the KEYS are invariant across profiles; only the labels vary — each task flagged gate (its success IS the phase's binary exit criterion,
// GateTaskFor) and conditional (rendered only when a real attempt record
// exists, IsConditionalTask) — so the SPA can render the task breakdown
// without hand-mirroring the server.
//
// Usage (matching the Makefile gen-uiprofiles / gen-uiprofiles-check targets,
// run from server/):
//
//	GOWORK=off go run ./cmd/gen-uiprofiles \
//	  -out ../webApp/src/components/construction/lifecycleTemplates.gen.ts
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// kindSpec pairs a webApp ActivityKind literal + its generated const name with
// the (ActivityType, TestingVariant) cell that produces its Profile.
type kindSpec struct {
	tsKind       string
	constName    string
	activityType projectstate.ActivityType
	variant      projectstate.TestingVariant
}

var kinds = []kindSpec{
	{"service", "SERVICE_PHASES", projectstate.ActivityTypeService, projectstate.TestVariantPlan},
	{"frontend", "FRONTEND_PHASES", projectstate.ActivityTypeFrontend, projectstate.TestVariantPlan},
	{"testing", "TESTING_PHASES", projectstate.ActivityTypeTesting, projectstate.TestVariantPlan},
	{"deployment", "DEPLOYMENT_PHASES", projectstate.ActivityTypeDeployment, projectstate.TestVariantPlan},
	{"documentation", "DOCUMENTATION_PHASES", projectstate.ActivityTypeDocumentation, projectstate.TestVariantPlan},
	{"uiDesign", "UI_DESIGN_PHASES", projectstate.ActivityTypeUIDesign, projectstate.TestVariantPlan},
	{"integration", "INTEGRATION_PHASES", projectstate.ActivityTypeIntegration, projectstate.TestVariantPlan},
}

// testingVariants emits the FIVE testing profiles the server actually carries. The
// generator used to emit only TestVariantPlan, so an N-IT activity rendered the N-STP
// shape — wrong for four of the five, and testing is 7 of 40 committed activities.
var testingVariants = []struct {
	tsVariant string
	constName string
	variant   projectstate.TestingVariant
}{
	{"plan", "TESTING_PLAN_PHASES", projectstate.TestVariantPlan},
	{"harness", "TESTING_HARNESS_PHASES", projectstate.TestVariantHarness},
	{"perf", "TESTING_PERF_PHASES", projectstate.TestVariantPerf},
	{"systemTest", "TESTING_SYSTEM_TEST_PHASES", projectstate.TestVariantSystemTest},
	{"qaProcess", "TESTING_QA_PROCESS_PHASES", projectstate.TestVariantQAProcess},
}

func main() {
	out := flag.String("out", "../webApp/src/components/construction/lifecycleTemplates.gen.ts", "output path for the generated TS file")
	flag.Parse()

	if err := run(*out); err != nil {
		fmt.Fprintf(os.Stderr, "gen-uiprofiles: %v\n", err)
		os.Exit(1)
	}
}

func run(outPath string) error {
	var b strings.Builder
	writeHeader(&b)

	for _, k := range kinds {
		writePhasesConst(&b, k.constName, k.activityType, k.variant)
	}
	writeTemplatesRecord(&b)

	for _, v := range testingVariants {
		writePhasesConst(&b, v.constName, projectstate.ActivityTypeTesting, v.variant)
	}
	writeTestingVariantsRecord(&b)

	return os.WriteFile(outPath, []byte(b.String()), 0o644) //nolint:gosec // generated TS, not a secret
}

func writeHeader(b *strings.Builder) {
	b.WriteString(`// Code generated by server/cmd/gen-uiprofiles. DO NOT EDIT.
// Source: server/internal/resourceaccess/projectstate/projectstateaccess.go (ProfileFor).
//
// id/phase/name/weight/exitCriterion/tasks per activity kind, sourced from the
// server's single canonical Profile (Requirements/DetailedDesign/TestPlan/
// Construction/Integration weighted subset) and its per-profile display copy
// (ExitCriterionFor / TaskLabelFor). Task keys and weights never vary by label.

import type { ActivityKind } from './KindBadge';
import type { TestingVariantName } from '../../contracts/types';

/** Canonical Method lifecycle phase (Righting Software Appendix A / Table A-1). */
export type LifecyclePhase =
  | 'requirements'
  | 'detailed_design'
  | 'test_plan'
  | 'construction'
  | 'integration';

/** One Figure A-1 task within a lifecycle phase. */
export interface GeneratedTask {
  /** The Figure A-1 task KEY — invariant across profiles; the ledger's join key. */
  task: string;
  /** This profile's display label for the task (server: TaskLabelFor). */
  label: string;
  /** The book's own name for the task (server: LabelForTask). */
  bookLabel: string;
  /** True when this task's success IS the phase's binary exit criterion (App A). */
  gate: boolean;
  /** True when the task is emitted only if a real attempt record exists. */
  conditional: boolean;
}

/** One canonical-phase entry in a kind's profile — id/phase/name/weight/exitCriterion/tasks. */
export interface GeneratedPhase {
  id: string;
  phase: LifecyclePhase;
  name: string;
  /** % contribution (App A Table A-1); weights sum to 100 per kind. */
  weight: number;
  /** This profile's binary exit criterion for the phase (server: ExitCriterionFor). */
  exitCriterion: string;
  tasks: readonly GeneratedTask[];
}

`)
}

func writePhasesConst(
	b *strings.Builder,
	constName string,
	t projectstate.ActivityType,
	v projectstate.TestingVariant,
) {
	profile := projectstate.ProfileFor(t, v)

	fmt.Fprintf(b, "export const %s: readonly GeneratedPhase[] = [\n", constName)
	for _, p := range profile.Phases {
		id := projectstate.CommandFor(t, v, p.Phase)
		b.WriteString("  {\n")
		fmt.Fprintf(b, "    id: %s,\n", tsString(id))
		fmt.Fprintf(b, "    phase: %s,\n", tsString(string(p.Phase)))
		fmt.Fprintf(b, "    name: %s,\n", tsString(p.Label))
		fmt.Fprintf(b, "    weight: %d,\n", p.Weight)
		writeProp(b, "    ", "exitCriterion", tsString(projectstate.ExitCriterionFor(t, v, p.Phase)))
		b.WriteString("    tasks: [\n")
		for _, task := range projectstate.TasksForPhase(p.Phase) {
			fmt.Fprintf(b, "      {\n        task: %s,\n        label: %s,\n        bookLabel: %s,\n        gate: %t,\n        conditional: %t,\n      },\n",
				tsString(string(task)),
				tsString(projectstate.TaskLabelFor(t, v, task)),
				tsString(projectstate.LabelForTask(task)),
				projectstate.GateTaskFor(p.Phase) == task,
				projectstate.IsConditionalTask(task))
		}
		b.WriteString("    ],\n")
		b.WriteString("  },\n")
	}
	b.WriteString("];\n\n")
}

func writeTemplatesRecord(b *strings.Builder) {
	b.WriteString("export const GENERATED_TEMPLATES: Record<ActivityKind, readonly GeneratedPhase[]> = {\n")
	for _, k := range kinds {
		fmt.Fprintf(b, "  %s: %s,\n", k.tsKind, k.constName)
	}
	b.WriteString("};\n\n")
}

// writeTestingVariantsRecord emits a lookup from the webApp TestingVariantName
// literal to that variant's own GeneratedPhase profile, so a variant-aware
// caller can render the right shape instead of the one-size TESTING_PHASES.
//
// Keyed by TestingVariantName, not by string: the record is EXHAUSTIVE over the five
// variants the server carries, so a variant added on either side is a TS compile error
// rather than a silent undefined lookup. Same precedent as GENERATED_TEMPLATES, which
// is keyed by the webApp's ActivityKind.
func writeTestingVariantsRecord(b *strings.Builder) {
	b.WriteString("export const GENERATED_TESTING_VARIANTS: Record<TestingVariantName, readonly GeneratedPhase[]> = {\n")
	for _, v := range testingVariants {
		fmt.Fprintf(b, "  %s: %s,\n", v.tsVariant, v.constName)
	}
	b.WriteString("};\n")
}

// prettierPrintWidth is the webApp's prettier printWidth. The generated file must be
// byte-identical to what prettier would write, or the webApp's format check fails on
// a file nobody may hand-edit.
const prettierPrintWidth = 100

// writeProp writes `<indent><key>: <literal>,` — or, when that line would exceed
// prettier's print width, the key alone with the literal on the next line indented
// two further spaces, which is exactly how prettier breaks an over-long property.
//
// Width is measured in CHARACTERS, as prettier measures it, not bytes: the copy
// carries "—" and "…", three bytes each, and len() would break a line prettier
// keeps whole (fix-B review M5).
func writeProp(b *strings.Builder, indent, key, literal string) {
	line := indent + key + ": " + literal + ","
	if utf8.RuneCountInString(line) <= prettierPrintWidth {
		b.WriteString(line + "\n")
		return
	}
	fmt.Fprintf(b, "%s%s:\n%s  %s,\n", indent, key, indent, literal)
}

// tsString renders a Go string as a TS string literal the way prettier does with the
// repo's singleQuote config: single-quoted, unless the text holds a single quote and no
// double quote, in which case prettier picks double quotes to avoid the escape.
func tsString(s string) string {
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
}
