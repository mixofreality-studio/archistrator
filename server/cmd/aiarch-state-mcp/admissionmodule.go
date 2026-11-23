package main

// admissionmodule.go is Tier 2 for a module with a composition of its own.
//
// conformance.RunChainOn scaffolds a fresh module and generates it with the library's default
// appgen config over null bodies. That is the real build of a greenfield module (every body null
// generated, appgen.json the scaffold's empty object) and not of one whose appgen.json
// configures the composition and whose variant constructors and hooks are hand-written. For such
// a module the candidate is checked against the module's real build: a copy of the module is
// regenerated in place from the candidate project.json with its own Makefile, then built and
// vetted there.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	conformance "github.com/mixofreality-studio/archistrator-platform/framework-go-conformance"
)

// moduleChainSteps are the commands run in the copied module, in order: the module's own
// steady-state regeneration from the candidate document, then the build and vet.
var moduleChainSteps = [][]string{
	{"make", "gen"},
	{"go", "build", "./..."},
	{"go", "vet", "./..."},
}

// copySkipDirs are the directories the module copy leaves out: version control, dependency
// trees and result folders hold nothing the build reads.
var copySkipDirs = map[string]bool{".git": true, "vendor": true, "node_modules": true, "test-results": true}

// ownModuleDir is the checkout directory of the module the document describes when that module
// carries a composition of its own (a go.mod and an appgen.json that configures something).
// ok is false for a module with neither: the scaffold chain is its real build.
func ownModuleDir(root string, raw []byte) (dir string, ok bool, err error) {
	var doc struct {
		ModuleDir string `json:"moduleDir"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return "", false, nil
	}
	dir = filepath.Clean(filepath.FromSlash(doc.ModuleDir))
	if _, err := os.Stat(filepath.Join(root, dir, goModFile)); errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	} else if err != nil {
		return "", false, err
	}
	cfg, err := os.ReadFile(filepath.Join(root, dir, appgenConfigFile)) // #nosec G304 -- the checkout's own module
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var decl map[string]json.RawMessage
	if err := json.Unmarshal(cfg, &decl); err != nil {
		return "", false, fmt.Errorf("%s of the module: %w", appgenConfigFile, err)
	}
	return dir, len(decl) > 0, nil
}

// runModuleChain copies the module (and the document's directory) into scratch at their checkout
// paths, seats raw as the project document, and runs moduleChainSteps there. It stops at the
// first failing step; the failing step is the last one reported.
func (r checkRunner) runModuleChain(ctx context.Context, moduleDir string, raw []byte, scratch string) (conformance.Report, error) {
	rep := conformance.Report{Fixture: "module", Dir: scratch}
	start := r.now()
	copyErr := copyModule(r.root, moduleDir, scratch)
	if copyErr == nil {
		copyErr = writeFileAt(filepath.Join(scratch, statePathPrefix, projectFile), raw)
	}
	step := conformance.StepResult{Name: "copy module", Duration: r.now().Sub(start)}
	if copyErr != nil {
		step.ExitCode, step.Stderr = 1, copyErr.Error()
	}
	rep.Steps = append(rep.Steps, step)
	if copyErr != nil {
		return rep, fmt.Errorf("copy the module: %w", copyErr)
	}
	dir := filepath.Join(scratch, moduleDir)
	for _, argv := range moduleChainSteps {
		begin := r.now()
		out, err := r.exec(ctx, dir, commandEnv(), argv...)
		res := conformance.StepResult{Name: strings.Join(argv, " "), Argv: argv, Duration: r.now().Sub(begin)}
		if err != nil {
			res.ExitCode = stepExitCode(err)
			res.Stderr = string(bytes.TrimSpace(out))
		}
		rep.Steps = append(rep.Steps, res)
		if err != nil {
			return rep, fmt.Errorf("conformance: step %q failed (exit %d): %w", res.Name, res.ExitCode, err)
		}
	}
	return rep, nil
}

func stepExitCode(err error) int {
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// copyModule copies <root>/<moduleDir> into scratch at the same relative path, and the state
// directory (the document's own home, which the module's Makefile reads by relative path). A
// module at the checkout root is the whole checkout.
func copyModule(root, moduleDir, scratch string) error {
	if err := copyModuleTree(filepath.Join(root, moduleDir), filepath.Join(scratch, moduleDir)); err != nil {
		return err
	}
	if moduleDir == "." {
		return nil
	}
	return copyModuleTree(filepath.Join(root, statePathPrefix), filepath.Join(scratch, statePathPrefix))
}

// copyModuleTree copies the regular files and directories of src into dst, leaving out copySkipDirs.
func copyModuleTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		switch {
		case d.IsDir() && copySkipDirs[d.Name()] && rel != ".":
			return fs.SkipDir
		case d.IsDir():
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		case !d.Type().IsRegular():
			return nil
		}
		body, err := os.ReadFile(path) // #nosec G304 -- a file of the checkout the venue is checking
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), body, info.Mode().Perm()) // #nosec G703 -- inside the admission scratch dir
	})
}

func writeFileAt(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}
