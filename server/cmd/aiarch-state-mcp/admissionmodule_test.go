package main

// admissionmodule_test.go pins Tier 2 for a module with a composition of its own: the candidate is
// built as the module (a regenerated copy), not in a default scaffold that never sees the module's
// appgen.json or hand-written constructors.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	conformance "github.com/mixofreality-studio/archistrator-platform/framework-go-conformance"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

type execCall struct {
	dir  string
	argv string
}

func tier2Check() review.SpecCheck {
	return review.SpecCheck{ID: "admission:tier2", Kind: projectstate.CheckKindAdmission, Title: "Tier 2", TimeoutSec: 60}
}

// moduleCheckout is a checkout whose module (server/) has a go.mod and the given appgen.json, a
// hand-written file, and the directories the copy must leave out.
func moduleCheckout(t *testing.T, appgenJSON string) (root string, raw []byte) {
	t.Helper()
	root = t.TempDir()
	raw = []byte(`{"moduleDir":"server"}`)
	writeRepoFile(t, root, ".aiarch/state/project.json", []byte(`{"moduleDir":"server","committed":true}`))
	writeRepoFile(t, root, "server/go.mod", []byte("module example.com/own\n"))
	writeRepoFile(t, root, "server/appgen.json", []byte(appgenJSON))
	writeRepoFile(t, root, "server/internal/x/hand.go", []byte("package x\n"))
	writeRepoFile(t, root, "server/node_modules/dep/index.js", []byte("x"))
	writeRepoFile(t, root, "server/.git/HEAD", []byte("x"))
	return root, raw
}

func TestAdmitTier2_ModuleWithOwnCompositionBuildsAsItself(t *testing.T) {
	root, raw := moduleCheckout(t, `{"variantConstructorNoError":{"messageBus/Temporal":true}}`)
	writeRepoFile(t, root, ".aiarch/state/project.json", raw)
	prev := runChainOn
	runChainOn = func(context.Context, string, []byte, string, ...conformance.ChainOption) (conformance.Report, error) {
		t.Fatal("a module with its own appgen.json must not be built in the default scaffold")
		return conformance.Report{}, nil
	}
	t.Cleanup(func() { runChainOn = prev })

	candidate := []byte(`{"moduleDir":"server","candidate":true}`)
	var calls []execCall
	var seenProject []byte
	r := checkRunner{root: root, outDir: filepath.Join(root, "out"), now: time.Now,
		exec: func(_ context.Context, dir string, env []string, argv ...string) ([]byte, error) {
			calls = append(calls, execCall{dir, strings.Join(argv, " ")})
			if len(calls) == 1 {
				var err error
				seenProject, err = os.ReadFile(filepath.Join(dir, "..", ".aiarch", "state", "project.json"))
				if err != nil {
					t.Error(err)
				}
				if _, err := os.Stat(filepath.Join(dir, "internal", "x", "hand.go")); err != nil {
					t.Errorf("the module's hand-written tree must be in the scratch copy: %v", err)
				}
				for _, skipped := range []string{"node_modules", ".git"} {
					if _, err := os.Stat(filepath.Join(dir, skipped)); err == nil {
						t.Errorf("%s must not be copied", skipped)
					}
				}
				if !slices.Contains(env, "GOWORK=off") {
					t.Error("steps must run with GOWORK=off")
				}
			}
			return nil, nil
		}}
	res := r.runAdmissionOver(t, candidate)
	if res.Status != checkPass {
		t.Fatalf("status = %s", res.Status)
	}
	if !bytes.Equal(seenProject, candidate) {
		t.Errorf("the scratch module's project.json = %q, want the candidate", seenProject)
	}
	var got []string
	for _, c := range calls {
		got = append(got, c.argv)
		if filepath.Base(c.dir) != "server" || strings.HasPrefix(c.dir, root) {
			t.Errorf("%s ran in %s, want the scratch copy of server/", c.argv, c.dir)
		}
	}
	if strings.Join(got, "|") != "make gen|go build ./...|go vet ./..." {
		t.Errorf("steps = %v", got)
	}
}

func TestAdmitTier2_ModuleChainFailureLogsTheFailingStepTail(t *testing.T) {
	root, raw := moduleCheckout(t, `{"envPrefix":"X"}`)
	writeRepoFile(t, root, ".aiarch/state/project.json", raw)
	r := checkRunner{root: root, outDir: filepath.Join(root, "out"), now: time.Now,
		exec: func(_ context.Context, _ string, _ []string, argv ...string) ([]byte, error) {
			if argv[0] == "go" && argv[1] == "build" {
				return []byte("internal/x/x.go:3:1: undefined: Y\n"), errors.New("exit status 1")
			}
			return nil, nil
		}}
	res := r.runAdmissionOver(t, raw)
	log, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "admission_tier2.log"))
	if res.Status != checkFail || !strings.Contains(string(log), "undefined: Y") {
		t.Fatalf("status %s, log:\n%s", res.Status, log)
	}
}

// A module with no appgen.json or the scaffold's empty one is greenfield: the scaffold chain is
// its real build.
func TestAdmitTier2_DefaultCompositionKeepsTheScaffoldChain(t *testing.T) {
	for name, cfg := range map[string]string{"empty object": "{}", "absent": ""} {
		t.Run(name, func(t *testing.T) {
			root, raw := moduleCheckout(t, cfg)
			if cfg == "" {
				if err := os.Remove(filepath.Join(root, "server", "appgen.json")); err != nil {
					t.Fatal(err)
				}
			}
			writeRepoFile(t, root, ".aiarch/state/project.json", raw)
			var chains int
			prev := runChainOn
			runChainOn = func(context.Context, string, []byte, string, ...conformance.ChainOption) (conformance.Report, error) {
				chains++
				return conformance.Report{}, nil
			}
			t.Cleanup(func() { runChainOn = prev })
			r := checkRunner{root: root, outDir: filepath.Join(root, "out"), now: time.Now,
				exec: func(context.Context, string, []string, ...string) ([]byte, error) {
					t.Fatal("a default-composition module must not run module steps")
					return nil, nil
				}}
			if res := r.runAdmissionOver(t, raw); res.Status != checkPass || chains != 1 {
				t.Fatalf("status %s, chains %d", res.Status, chains)
			}
		})
	}
}

// runAdmissionOver runs the Tier 2 check over candidate as the checkout's project.json.
func (r checkRunner) runAdmissionOver(t *testing.T, candidate []byte) CheckResult {
	t.Helper()
	writeRepoFile(t, r.root, ".aiarch/state/project.json", candidate)
	return r.Run(context.Background(), []review.SpecCheck{tier2Check()})[0]
}

// The unchanged own project passes Tier 2 as the module it is: regenerated with its own Makefile
// from the committed document, then built and vetted. Under the default scaffold this fails on
// every hand-written variant constructor.
func TestAdmitTier2_OwnProjectPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("regenerates and builds the whole module: minutes")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".aiarch", "state", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := checkRunner{root: root, outDir: t.TempDir(), now: time.Now}
	res := r.Run(context.Background(), []review.SpecCheck{{ID: "admission:tier2", Kind: projectstate.CheckKindAdmission, Title: "Tier 2", TimeoutSec: 1800}})[0]
	if res.Status != checkPass {
		log, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "admission_tier2.log"))
		t.Fatalf("own project Tier 2 = %s (document %d bytes):\n%s", res.Status, len(raw), log)
	}
}
