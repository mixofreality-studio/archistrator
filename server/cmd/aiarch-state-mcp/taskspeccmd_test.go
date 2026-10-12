package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// seedOwnCheckout seats archistrator's own committed project.json in a fresh checkout
// root and returns the root.
func seedOwnCheckout(t *testing.T) string {
	t.Helper()
	_, raw := copyCommittedProject(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, statePathPrefix), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, statePathPrefix, projectFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestTaskSpecCmd_HashRoundTrip: the printed hash is the written spec's hash, the
// ComputeHash of the file as decoded, and the hash the server's own TaskSpecFor call
// computes from the same project.json (R13: one code path, no CLI-specific hashing).
func TestTaskSpecCmd_HashRoundTrip(t *testing.T) {
	root := seedOwnCheckout(t)
	out := filepath.Join(t.TempDir(), "spec.json")
	var stdout bytes.Buffer
	err := runTaskSpec([]string{"--activity", "C-agentic-job-access", "--task", "integration", "--root", root, "--out", out}, &stdout)
	if err != nil {
		t.Fatalf("task-spec: %v", err)
	}
	printed := stdout.String()
	if !strings.HasSuffix(printed, "\n") || strings.Count(printed, "\n") != 1 {
		t.Fatalf("stdout must be the hash and one newline, got %q", printed)
	}
	printed = strings.TrimSuffix(printed, "\n")

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var spec projectstate.TaskSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if len(printed) != 64 || printed != spec.Hash || printed != spec.ComputeHash() {
		t.Fatalf("printed %q, file hash %q, recomputed %q", printed, spec.Hash, spec.ComputeHash())
	}

	assertServerHash(t, root, printed)
	assertWorkflowShape(t, raw)
}

// assertServerHash: the server's own TaskSpecFor over the same project.json computes
// the hash task-spec printed.
func assertServerHash(t *testing.T, root, printed string) {
	t.Helper()
	committed, err := os.ReadFile(filepath.Join(root, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := projectstate.DecodeProjectJSON(committed, "archistrator")
	if err != nil {
		t.Fatal(err)
	}
	server, err := projectstate.TaskSpecFor(&p, "C-agentic-job-access", "integration")
	if err != nil {
		t.Fatal(err)
	}
	if server.Hash != printed {
		t.Fatalf("server hash %s != task-spec hash %s", server.Hash, printed)
	}
}

// assertWorkflowShape: the workflow reads .model.id and .tools.builtin (an array) with jq.
func assertWorkflowShape(t *testing.T, raw []byte) {
	t.Helper()
	var shape struct {
		Model struct {
			ID string `json:"id"`
		} `json:"model"`
		Tools struct {
			Builtin []string `json:"builtin"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil || shape.Model.ID == "" || shape.Tools.Builtin == nil {
		t.Fatalf("spec lacks .model.id or .tools.builtin: %v %+v", err, shape)
	}
}

// TestTaskSpecCmd_UnknownActivityExits2: an activity the plan does not hold is a
// resolver error, exit 2, and writes no spec.
func TestTaskSpecCmd_UnknownActivityExits2(t *testing.T) {
	root := seedOwnCheckout(t)
	out := filepath.Join(t.TempDir(), "spec.json")
	err := runTaskSpec([]string{"--activity", "C-nope", "--task", "integration", "--root", root, "--out", out}, &bytes.Buffer{})
	var ee exitError
	if !errors.As(err, &ee) || ee.code != 2 {
		t.Fatalf("want exit 2, got %v", err)
	}
	if _, serr := os.Stat(out); !os.IsNotExist(serr) {
		t.Fatal("a failed resolve must write no spec")
	}
}

func TestTaskSpecCmd_FlagsRequired(t *testing.T) {
	err := runTaskSpec([]string{"--activity", "C-agentic-job-access"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--task") {
		t.Fatalf("want a missing --task error, got %v", err)
	}
}

// TestVersionCmd: `version` prints the binary's task-spec generation — the method-assets
// module version it was built with and the spec schema version — the value the local
// rail compares with the server's own (R56).
func TestVersionCmd(t *testing.T) {
	var out bytes.Buffer
	handled, err := runOneShotCommand([]string{"version"})
	if !handled || err != nil {
		t.Fatalf("version: handled=%v err=%v", handled, err)
	}
	if err := runVersion(nil, &out); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out.String())
	if got != projectstate.StateToolVersion() || !strings.HasPrefix(got, "method-assets ") ||
		!strings.HasSuffix(got, "; spec-schema "+strconv.Itoa(projectstate.TaskSpecSchemaVersion)) {
		t.Fatalf("version printed %q, want %q", got, projectstate.StateToolVersion())
	}
	var ee exitError
	if err := runVersion([]string{"extra"}, &out); !errors.As(err, &ee) || ee.code != 2 {
		t.Fatalf("version with an argument: want exit 2, got %v", err)
	}
}
