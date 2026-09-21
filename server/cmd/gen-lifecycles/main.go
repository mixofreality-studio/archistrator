// cmd/gen-lifecycles emits the per-activity-type lifecycle — phases (weight, gate,
// exit criterion) and the dispatch/review task DAG — into
// webApp/src/components/activity/lifecycles.gen.ts, straight from the
// lifecycles.json shipped in the method-assets release pinned in go.mod
// (methodassets.Lifecycles). The data is platform-fixed (the same for every
// project), so this tool reads no project.json and takes no flag but -out.
//
// The output is byte-identical to what the webApp's prettier config would write,
// because the webApp's format check covers generated files too: an over-wide
// property breaks after its colon (writeProp), a string is quoted the way prettier
// quotes it (tsString), and the type-key union breaks one member per line once it
// no longer fits on one.
//
// It refuses to generate from data methodassets.ValidateLifecycle rejects.
//
// Usage (matching the Makefile gen-lifecycles / gen-lifecycles-check targets, run
// from server/):
//
//	GOWORK=off go run ./cmd/gen-lifecycles \
//	  -out ../webApp/src/components/activity/lifecycles.gen.ts
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

const defaultOutPath = "../webApp/src/components/activity/lifecycles.gen.ts"

func main() {
	out := flag.String("out", defaultOutPath, "output path for the generated TS file")
	flag.Parse()

	if err := run(*out); err != nil {
		fmt.Fprintf(os.Stderr, "gen-lifecycles: %v\n", err)
		os.Exit(1)
	}
}

func run(outPath string) error {
	src, err := render(methodassets.Lifecycles())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
		return err
	}
	return os.WriteFile(outPath, []byte(src), 0o644) //nolint:gosec // generated TS, not a secret
}

// render is the whole generator as a pure function: lifecycles in, TS source out.
func render(all []methodassets.Lifecycle) (string, error) {
	if len(all) == 0 {
		return "", errors.New("method-assets carries no lifecycles; refusing to emit an empty table")
	}
	var problems []string
	for _, l := range all {
		problems = append(problems, methodassets.ValidateLifecycle(l)...)
	}
	if len(problems) > 0 {
		return "", fmt.Errorf("refusing to generate from invalid lifecycle data:\n  %s", strings.Join(problems, "\n  "))
	}
	if err := checkEmittedStrings(all); err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(tsHeader)
	writeTypeKeys(&b, all)
	b.WriteString(tsTypeDecls)
	for _, l := range all {
		if err := writeLifecycle(&b, l); err != nil {
			return "", err
		}
	}
	b.WriteString(tsFooter)
	return b.String(), nil
}

// checkEmittedStrings refuses to render any lifecycle carrying a string field
// tsString would emit un-escaped-but-invalid: a control character (U+0000 to
// U+001F, U+007F DELETE, or the JS/TS-legal-but-invisible U+2028/U+2029 line
// and paragraph separators). tsString escapes only backslash and the chosen
// quote, so a literal newline (routine: JSON "\n" decodes to a real newline
// rune, and nothing in ValidateLifecycle's contract guards string content)
// would otherwise land inside a quoted TS literal as a raw line break —
// invalid TS that fails to parse. Checked once, up front, so writeLifecycle
// and writeTask never have to handle this failure themselves.
func checkEmittedStrings(all []methodassets.Lifecycle) error {
	for _, l := range all {
		if err := checkEmittedString(l.Type, "type", l.Type); err != nil {
			return err
		}
		for _, lp := range l.Phases {
			if err := checkPhaseStrings(l.Type, lp); err != nil {
				return err
			}
		}
		for _, task := range l.Tasks {
			if err := checkTaskStrings(l.Type, task); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkPhaseStrings(lifecycleType string, lp methodassets.LifecyclePhase) error {
	fields := [...]struct{ name, value string }{
		{"id", lp.ID}, {"label", lp.Label}, {"gate", lp.Gate}, {"exitCriterion", lp.ExitCriterion},
	}
	for _, f := range fields {
		if err := checkEmittedString(lifecycleType, f.name, f.value); err != nil {
			return err
		}
	}
	return nil
}

func checkTaskStrings(lifecycleType string, task methodassets.LifecycleTask) error {
	fields := [...]struct{ name, value string }{
		{"id", task.ID}, {"kind", task.Kind}, {"title", task.Title}, {"phase", task.Phase},
		{"reviews", task.Reviews}, {"command", task.Command},
		{"workerClass", task.WorkerClass}, {"artifactKind", task.ArtifactKind},
	}
	for _, f := range fields {
		if err := checkEmittedString(lifecycleType, f.name, f.value); err != nil {
			return err
		}
	}
	for _, dep := range task.DependsOn {
		if err := checkEmittedString(lifecycleType, "dependsOn", dep); err != nil {
			return err
		}
	}
	return nil
}

// checkEmittedString errors, naming the lifecycle type, the field and the
// offending rune, on the first control character found in value.
func checkEmittedString(lifecycleType, field, value string) error {
	if r, bad := firstUnescapableRune(value); bad {
		return fmt.Errorf("lifecycle %q: field %s contains the unescapable control character %U; tsString escapes only backslash and quotes, so this would emit invalid TS", lifecycleType, field, r)
	}
	return nil
}

// firstUnescapableRune reports the first rune in s that tsString would write
// into a TS string literal un-escaped despite being invalid or unrenderable
// there: the C0 control range and DELETE, or the two Unicode line-breaking
// separators JS/TS treat as legal-but-invisible whitespace inside a literal.
func firstUnescapableRune(s string) (rune, bool) {
	for _, r := range s {
		if (r >= 0x00 && r <= 0x1F) || r == 0x7F || r == 0x2028 || r == 0x2029 {
			return r, true
		}
	}
	return 0, false
}

const tsHeader = `// Code generated by server/cmd/gen-lifecycles. DO NOT EDIT.
// Source: lifecycles.json in github.com/mixofreality-studio/archistrator-platform/method-assets,
// at the version pinned in server/go.mod.
//
// The per-activity-type lifecycle: its phases (earned-value weight, gate, exit criterion) and
// its task DAG. Platform-fixed data, the same for every project, so it is compiled into the
// webApp rather than fetched. STATIC shape only: a task's state and its revisions are project
// data and arrive from the server.
//
// Field names line up with the lifecycle graph's props vocabulary (lifecycleGraphTypes.ts:
// LifecycleNode id/kind/title/phase/dependsOn/revisionGroup, LifecyclePhase id/label/weight),
// so a container builds a graph node by spreading a task and adding its state and revisions.

/** A task either dispatches an agent, which produces an artifact, or reviews one. */
export type LifecycleTaskKind = 'dispatch' | 'review';

`

const tsTypeDecls = `
/** A Figure A-2 grouping of tasks: the earned-value unit. */
export interface LifecyclePhaseDef {
  id: string;
  label: string;
  /** Table A-1 earned-value weight, in percent; a lifecycle's weights sum to 100. */
  weight: number;
  /** Id of the review task whose success IS this phase's exit. */
  gate: string;
  /** That exit, as one sentence in this lifecycle's own words. */
  exitCriterion: string;
}

/** One node of a lifecycle's task DAG. */
export interface LifecycleTaskDef {
  id: string;
  kind: LifecycleTaskKind;
  title: string;
  /** The {@link LifecyclePhaseDef.id} this task belongs to. */
  phase: string;
  /** Ids of the tasks this one waits on. Tasks are in authored order: the trunk comes first. */
  dependsOn: readonly string[];
  /** A dispatch and the review that judges it revise ONE artifact together: they share this. */
  revisionGroup: string;
  /** Review only: the dispatch task it judges. A send-back re-opens that pair. */
  reviews?: string;
  /** The slash command an agent runs for this task, when one does. */
  command?: string;
  /** The agent charter that command adopts. */
  workerClass?: string;
  /** What a dispatch produces (or what a review that names no dispatch task judges). */
  artifactKind?: string;
}

/** The task DAG every activity of one type walks. */
export interface LifecycleDef {
  type: LifecycleTypeKey;
  phases: readonly LifecyclePhaseDef[];
  tasks: readonly LifecycleTaskDef[];
}

export const LIFECYCLES: readonly LifecycleDef[] = [
`

const tsFooter = `];

/** The lifecycle for an activity-type key; undefined for a key this build does not carry. */
export function lifecycleFor(typeKey: string): LifecycleDef | undefined {
  return LIFECYCLES.find((l) => l.type === typeKey);
}
`

// writeTypeKeys emits the closed union of type keys: on one line when it fits
// prettier's width, otherwise one member per line — prettier's own two forms.
func writeTypeKeys(b *strings.Builder, all []methodassets.Lifecycle) {
	keys := make([]string, len(all))
	for i, l := range all {
		keys[i] = tsString(l.Type)
	}
	b.WriteString("/** The closed set of activity-type keys: the ActivityType wire name, or testing:<variant>. */\n")
	oneLine := "export type LifecycleTypeKey = " + strings.Join(keys, " | ") + ";"
	if utf8.RuneCountInString(oneLine) <= prettierPrintWidth {
		b.WriteString(oneLine + "\n")
		return
	}
	b.WriteString("export type LifecycleTypeKey =\n  | " + strings.Join(keys, "\n  | ") + ";\n")
}

func writeLifecycle(b *strings.Builder, l methodassets.Lifecycle) error {
	b.WriteString("  {\n")
	writeProp(b, "    ", "type", tsString(l.Type))
	b.WriteString("    phases: [\n")
	for _, lp := range l.Phases {
		b.WriteString("      {\n")
		writeProp(b, memberIndent, "id", tsString(lp.ID))
		writeProp(b, memberIndent, "label", tsString(lp.Label))
		writeProp(b, memberIndent, "weight", fmt.Sprintf("%d", lp.Weight))
		writeProp(b, memberIndent, "gate", tsString(lp.Gate))
		writeProp(b, memberIndent, "exitCriterion", tsString(lp.ExitCriterion))
		b.WriteString("      },\n")
	}
	b.WriteString("    ],\n    tasks: [\n")
	for _, task := range l.Tasks {
		if err := writeTask(b, task); err != nil {
			return fmt.Errorf("%s: %w", l.Type, err)
		}
	}
	b.WriteString("    ],\n  },\n")
	return nil
}

// memberIndent is the indent of a property inside a phases[] / tasks[] member.
const memberIndent = "        "

func writeTask(b *strings.Builder, task methodassets.LifecycleTask) error {
	deps := make([]string, len(task.DependsOn))
	for i, d := range task.DependsOn {
		deps[i] = tsString(d)
	}
	depsLine := memberIndent + "dependsOn: [" + strings.Join(deps, ", ") + "],"
	if n := utf8.RuneCountInString(depsLine); n > prettierPrintWidth {
		return fmt.Errorf("task %q: its dependsOn line is %d characters; prettier would break that array one id per line and this generator does not — teach writeTask that form before shipping a task this wide", task.ID, n)
	}

	b.WriteString("      {\n")
	writeProp(b, memberIndent, "id", tsString(task.ID))
	writeProp(b, memberIndent, "kind", tsString(task.Kind))
	writeProp(b, memberIndent, "title", tsString(task.Title))
	writeProp(b, memberIndent, "phase", tsString(task.Phase))
	b.WriteString(depsLine + "\n")
	writeProp(b, memberIndent, "revisionGroup", tsString(revisionGroupOf(task)))
	writeOptionalProp(b, "reviews", task.Reviews)
	writeOptionalProp(b, "command", task.Command)
	writeOptionalProp(b, "workerClass", task.WorkerClass)
	writeOptionalProp(b, "artifactKind", task.ArtifactKind)
	b.WriteString("      },\n")
	return nil
}

// revisionGroupOf is the artifact a task revises: a review revises the artifact of
// the dispatch task it judges, so the pair numbers its revisions together; any other
// task numbers its revisions alone.
func revisionGroupOf(task methodassets.LifecycleTask) string {
	if task.Reviews != "" {
		return task.Reviews
	}
	return task.ID
}

// writeOptionalProp omits an empty value entirely: the TS property is optional, and
// under exactOptionalPropertyTypes an absent key is not the same as an empty string.
func writeOptionalProp(b *strings.Builder, key, value string) {
	if value != "" {
		writeProp(b, memberIndent, key, tsString(value))
	}
}

// prettierPrintWidth is the webApp's prettier printWidth (webApp/.prettierrc).
const prettierPrintWidth = 100

// writeProp writes "<indent><key>: <literal>," — or, when that line would exceed
// prettier's print width, the key alone with the literal on the next line indented
// two further spaces, which is how prettier breaks an over-long property. Width is
// measured in CHARACTERS, as prettier measures it, not bytes.
//
// Copied from cmd/gen-uiprofiles (two package mains cannot share a helper, and the
// layering gate leaves no internal package to hold one); that copy dies in stage 2.
func writeProp(b *strings.Builder, indent, key, literal string) {
	line := indent + key + ": " + literal + ","
	if utf8.RuneCountInString(line) <= prettierPrintWidth {
		b.WriteString(line + "\n")
		return
	}
	fmt.Fprintf(b, "%s%s:\n%s  %s,\n", indent, key, indent, literal)
}

// tsString renders a Go string as a TS string literal the way prettier does under the
// repo's singleQuote config: single-quoted, unless the text holds more single quotes
// than double quotes, in which case prettier prefers double quotes to save escapes.
func tsString(s string) string {
	quote := "'"
	if strings.Count(s, "'") > strings.Count(s, `"`) {
		quote = `"`
	}
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, quote, `\`+quote)
	return quote + escaped + quote
}
