package internal_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// This file extends the Method arch checker (arch_test.go) with the appgen
// migration's own invocation-convention gate: internal/manager/**/*.go hand
// files (custom Activities, workflow bodies, dispatch/gitsession/reviewledger
// helpers) must invoke Temporal Activities by METHOD VALUE
// (workflow.ExecuteActivity(ctx, wf.XActivity, args...)) and must never
// register Activities/Workflows directly — registration flows exclusively
// through the generated RegisterWorker(w, manifest) entrypoint
// (worker.gen.go). String-literal activity-name invocation and direct
// Register*WithOptions calls are RESERVED for the generated layer
// (invokers.gen.go / worker.gen.go), which the framework-go-app-generator
// temporalgen emitter owns; a hand file doing either would silently create a
// second, divergent way to call or register an activity that the generated
// surface's TaskQueue/manifest wiring doesn't know about.

// reStringActivityName flags workflow.ExecuteActivity(<ctx-arg>, "<name>", ...) —
// a quoted string literal as the second (activity) argument. Method-value
// invocation (workflow.ExecuteActivity(ctx, wf.FooActivity, ...)) does not
// match: the second argument there is a bare identifier/selector, not a
// quoted string.
var reStringActivityName = regexp.MustCompile(`ExecuteActivity\(\s*[^,\s][^,]*,\s*"`)

// reRegisterWithOptions flags direct calls to the Temporal SDK's
// RegisterActivityWithOptions / RegisterWorkflowWithOptions — the generated
// worker.gen.go's RegisterWorker is the only sanctioned call site.
var reRegisterWithOptions = regexp.MustCompile(`Register(?:Activity|Workflow)WithOptions\(`)

// findNoStringActivityViolations scans the given (path -> source) file set and
// returns one human-readable violation message per offending line. It is pure
// and I/O-free — table-tested directly below without touching the real
// filesystem — so TestNoHandTemporalStringActivityNames only has to wire it to
// the real internal/manager tree.
func findNoStringActivityViolations(files map[string]string) []string {
	var violations []string
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		lines := strings.Split(files[path], "\n")
		for i, line := range lines {
			lineNo := i + 1
			if reStringActivityName.MatchString(line) {
				violations = append(violations, fmt.Sprintf(
					"%s:%d: workflow.ExecuteActivity called with a string-literal activity name — hand files must invoke activities by method value (workflow.ExecuteActivity(ctx, wf.XActivity, ...)); string-literal invocation is reserved for the generated internal/manager/*/invokers.gen.go: %s",
					path, lineNo, strings.TrimSpace(line)))
			}
			if reRegisterWithOptions.MatchString(line) {
				violations = append(violations, fmt.Sprintf(
					"%s:%d: direct call to RegisterActivityWithOptions/RegisterWorkflowWithOptions outside the generated worker.gen.go — hand files must register through the generated RegisterWorker(w, manifest) entrypoint: %s",
					path, lineNo, strings.TrimSpace(line)))
			}
		}
	}
	return violations
}

// TestFindNoStringActivityViolations table-tests the pure checker in memory —
// method-value invocation must stay legal, and each of the two banned shapes
// (string-literal ExecuteActivity, direct Register*WithOptions) must be
// caught, proving the checker actually catches a seeded violation rather than
// vacuously passing.
func TestFindNoStringActivityViolations(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "method-value invocation is legal",
			src:  `func x() { e := workflow.ExecuteActivity(ctx, wf.FooActivity, arg); _ = e }`,
			want: 0,
		},
		{
			name: "multiple method-value invocations stay legal",
			src: `func x() {
	workflow.ExecuteActivity(c, wf.ReadProjectActivity, projectID)
	workflow.ExecuteActivity(rc, wf.RecordPhaseCompletedActivity, recordPhaseCompletedArgs{})
}`,
			want: 0,
		},
		{
			name: "string-literal activity name is flagged",
			src:  `func x() { workflow.ExecuteActivity(ctx, "fooAccess.bar", arg) }`,
			want: 1,
		},
		{
			name: "RegisterActivityWithOptions outside generated file is flagged",
			src:  `func x() { w.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: "x"}) }`,
			want: 1,
		},
		{
			name: "RegisterWorkflowWithOptions outside generated file is flagged",
			src:  `func x() { w.RegisterWorkflowWithOptions(fn, workflow.RegisterOptions{Name: "x"}) }`,
			want: 1,
		},
		{
			name: "both violations in one file are both flagged",
			src: `func x() {
	workflow.ExecuteActivity(ctx, "fooAccess.bar", arg)
	w.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: "x"})
}`,
			want: 2,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := findNoStringActivityViolations(map[string]string{"seed.go": c.src})
			if len(got) != c.want {
				t.Fatalf("findNoStringActivityViolations() = %d violation(s), want %d: %v", len(got), c.want, got)
			}
		})
	}
}

// TestNoHandTemporalStringActivityNames is the real gate: it walks
// internal/manager/**/*.go (excluding *.gen.go and *_test.go, which are
// generated/allowed to construct fake registrations respectively) and fails
// the build the moment a hand file invokes an activity by string literal or
// registers one directly. workermanifest.go is in scope like every other hand
// file — it builds the genWorkerManifest by NAME+method-value pairs
// (genRegisteredActivity{Name: ..., Fn: wf.XActivity}), which this checker
// does not flag; it never itself calls ExecuteActivity or
// Register*WithOptions.
func TestNoHandTemporalStringActivityNames(t *testing.T) {
	root := filepath.Join("manager")
	files := map[string]string{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, ".gen.go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(files) == 0 {
		t.Fatalf("no hand Go files found under %s — checker is not scanning anything", root)
	}

	violations := findNoStringActivityViolations(files)
	if len(violations) > 0 {
		t.Fatalf(
			"internal/manager hand files must invoke activities by method value and register only through the generated RegisterWorker entrypoint; found %d violation(s):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}
