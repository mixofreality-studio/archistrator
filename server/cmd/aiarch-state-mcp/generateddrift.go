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
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
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
	regenerated, err := regenerate(root, spec, rng.head, specGeneratorNames(spec))
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

// specGeneratorNames is the consumers the task's job runs: modelgen over its contracts, then each
// generator its spec names.
func specGeneratorNames(spec review.TaskSpec) []string {
	names := make([]string, 0, len(spec.Generate)+1)
	if len(spec.Contracts) > 0 {
		names = append(names, "modelgen")
	}
	for _, g := range spec.Generate {
		names = append(names, g.Title)
	}
	return names
}

// regenerate is every file the named consumers write over the project document head holds, by
// repo-relative path; modelgen runs over spec.Contracts.
func regenerate(root string, spec review.TaskSpec, head string, names []string) (map[string][]byte, error) {
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

// testGenerator is the consumer that writes the scenario tests an integration task commits.
const testGenerator = "testgen"

// integrationCommands are the task commands whose job generates the scenario tests before the agent
// runs and whose agent commits them with its wiring.
var integrationCommands = []string{"service-integration", "frontend-integration"}

// admitRegeneratedTests drops from violations the test-code paths (projectstate.IsTestCodePath) an
// integration task's job generated: a changed path whose committed bytes at head equal what testgen
// writes over the same project document. The agent writes no test code, and a path it wrote or
// edited by hand differs from regeneration, so it stays a violation; the generated tests the job
// itself wrote and the agent committed (the command's step 6) are the job's output, held current
// by the same regeneration the drift check above uses. Workspace paths (named "@<repo>/...") are
// never admitted. A task that is not an integration, or a list holding no test-code path, costs
// nothing; otherwise root must be a checkout of head.
func admitRegeneratedTests(root, specPath string, rng editScopeRange, violations []string) ([]string, error) {
	var candidates []string
	for _, v := range violations {
		if !strings.HasPrefix(v, workspaceGlobPrefix) && projectstate.IsTestCodePath(v) {
			candidates = append(candidates, v)
		}
	}
	if len(candidates) == 0 {
		return violations, nil
	}
	spec, err := readSpecFile(specPath)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(integrationCommands, spec.Identity.Command) {
		return violations, nil
	}
	if at, err := commitSHA(root, "HEAD"); err != nil || at != rng.head {
		return nil, fmt.Errorf("the generated-test check regenerates over the tree, so it must run in a checkout of --head %s (HEAD is %s)", rng.head, at)
	}
	regenerated, err := regenerate(root, spec, rng.head, []string{testGenerator})
	if err != nil {
		return nil, err
	}
	var kept []string
	for _, v := range violations {
		if slices.Contains(candidates, v) {
			if want, ok := regenerated[v]; ok {
				if got, err := isolatedGit(root, "show", rng.head+":"+v); err == nil && got == string(want) {
					continue
				}
			}
		}
		kept = append(kept, v)
	}
	return kept, nil
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir() && fs.ValidPath(filepath.Base(p))
}
