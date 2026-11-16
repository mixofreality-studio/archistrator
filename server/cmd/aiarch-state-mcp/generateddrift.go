package main

// generateddrift.go — the edit-scope gate's second half for generated files (FPC D6). A path the
// task spec names as generated (spec.Generated: what the generators its task runs write, from
// their manifests) may sit inside the scope's globs and still must not be hand-edited: the
// committed bytes must equal what the generator writes over the same project.json. So every
// changed path in spec.Generated is regenerated from the project document as head holds it and
// compared byte for byte; a difference is reported as EDIT-GENERATED-DRIFT.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
)

// driftRuleID is the rule id the gate reports a hand-edited generated file under.
const driftRuleID = "EDIT-GENERATED-DRIFT"

// generatedDriftViolations returns the changed paths (sorted) that spec.Generated names and whose
// committed bytes at head differ from regeneration, each as "EDIT-GENERATED-DRIFT <path>". A spec
// with no generated path, or a change touching none, costs nothing; otherwise root must be a
// checkout of head (the regeneration reads the tree to know what is built).
func generatedDriftViolations(root, specPath string, rng editScopeRange) ([]string, error) {
	spec, err := readSpecFile(specPath)
	if err != nil {
		return nil, err
	}
	if len(spec.Generated) == 0 {
		return nil, nil
	}
	changed, err := branchChangedPaths(root, rng)
	if err != nil {
		return nil, err
	}
	var touched []string
	for _, p := range changed {
		if slices.Contains(spec.Generated, p) {
			touched = append(touched, p)
		}
	}
	if len(touched) == 0 {
		return nil, nil
	}
	if at, err := commitSHA(root, "HEAD"); err != nil || at != rng.head {
		return nil, fmt.Errorf("the generated-file check regenerates over the tree, so it must run in a checkout of --head %s (HEAD is %s)", rng.head, at)
	}
	regenerated, err := regenerate(root, spec, rng.head)
	if err != nil {
		return nil, err
	}
	var bad []string
	for _, p := range touched {
		want, ok := regenerated[p]
		if !ok {
			continue // not written over this tree (its contract is not built here): nothing to compare to
		}
		got, err := isolatedGit(root, "show", rng.head+":"+p)
		if err != nil || got != string(want) {
			bad = append(bad, driftRuleID+" "+displayPath(p))
		}
	}
	return bad, nil
}

// regenerate is every file the consumers spec runs write over the project document head holds, by
// repo-relative path.
func regenerate(root string, spec review.TaskSpec, head string) (map[string][]byte, error) {
	project, err := isolatedGit(root, "show", head+":.aiarch/state/project.json")
	if err != nil {
		return nil, fmt.Errorf("read the project document at %s: %w", head, err)
	}
	var doc struct {
		ModuleDir string `json:"moduleDir"`
	}
	if err := json.Unmarshal([]byte(project), &doc); err != nil {
		return nil, err
	}
	modPath, err := isolatedGit(root, "show", head+":"+path.Join(doc.ModuleDir, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("read go.mod at %s: %w", head, err)
	}
	module, err := moduleLine(modPath)
	if err != nil {
		return nil, err
	}
	cfg := appgen.DefaultConfig()
	if raw, err := isolatedGit(root, "show", head+":"+path.Join(doc.ModuleDir, "appgen.json")); err == nil {
		if cfg, err = appgen.ParseConfig([]byte(raw)); err != nil {
			return nil, fmt.Errorf("appgen.json: %w", err)
		}
	}
	in := appgen.Input{
		Project: []byte(project), ModulePath: module, ModuleDir: doc.ModuleDir, Config: cfg,
		Tree: os.DirFS(filepath.Join(root, doc.ModuleDir)),
	}
	if web := filepath.Join(root, "webApp"); dirExists(web) {
		in.WebTree = os.DirFS(web)
	}
	names := make([]string, 0, len(spec.Generate)+1)
	if len(spec.Contracts) > 0 {
		names = append(names, "modelgen")
	}
	for _, g := range spec.Generate {
		names = append(names, g.Title)
	}
	out := map[string][]byte{}
	for _, name := range names {
		c, ok := appgen.ConsumerNamed(name)
		if !ok {
			return nil, fmt.Errorf("the spec runs %q, which is no generator consumer", name)
		}
		run := in
		if name == "modelgen" {
			run.Only = spec.Contracts
		}
		files, err := c.Emit(run)
		if err != nil {
			return nil, fmt.Errorf("regenerate %s: %w", name, err)
		}
		for _, f := range files {
			out[f.Path] = f.Content
		}
	}
	return out, nil
}

// moduleLine is the module path of a go.mod's text.
func moduleLine(gomod string) (string, error) {
	sc := bufio.NewScanner(strings.NewReader(gomod))
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`), nil
		}
	}
	return "", errors.New("go.mod has no module line")
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir() && fs.ValidPath(filepath.Base(p))
}
