package main

// workspacedispatch_test.go holds the venue half of workspace repos: a task whose edit scope names
// a declared sibling repo gets a worktree of it beside the project's own, and what the sandbox may
// write, what the edit-scope gate judges, what the check gate runs over and what counts as the
// run's progress all follow from that. The local executor runs a real task spec over a fake
// `claude` that edits the way the test says.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// wsOwned is the directory of plat the fixture's todo-list-access contract owns.
const wsOwned = `["@plat/lib"]`

// agent is the fake `claude` on PATH: its script runs the body the test last set, in the project's
// worktree with $PLAT the plat worktree (the venue's AIARCH_WORKSPACE_ROOTS) and $OWN the project's,
// and prints a stream that reached the model. It is installed before the executor is built (the
// executor resolves `claude` once), and a test changes what it does between dispatches.
type agent struct{ bodyPath string }

// installAgent puts the fake claude, and a `make` that skips the generation the fixture's released
// platform pin cannot do for a project declaring ownedPaths, on PATH.
func installAgent(t *testing.T) agent {
	t.Helper()
	dir := t.TempDir()
	a := agent{bodyPath: filepath.Join(dir, "body.sh")}
	a.does(t, "true")
	script := `#!/bin/sh
cat >/dev/null
OWN=$(pwd)
PLAT=$(printf '%s' "$AIARCH_WORKSPACE_ROOTS" | sed -e 's/.*"plat":"\([^"]*\)".*/\1/')
commit() { git -c user.name=agent -c user.email=agent@example.com commit -q -m agent; }
. ` + a.bodyPath + `
cat <<'STREAM'
` + streamReached + `
STREAM
exit 0
`
	realMake, err := exec.LookPath("make")
	if err != nil {
		t.Fatal(err)
	}
	// The job generates the component's models before the agent runs; the fixture's scaffold pins a
	// released projectmodel that does not know ownedPaths, so that step is not what these tests judge.
	makeStub := "#!/bin/sh\nif [ \"$1\" = gen-models ]; then exit 0; fi\nexec " + realMake + " \"$@\"\n"
	for name, body := range map[string]string{"claude": script, "make": makeStub} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil { //nolint:gosec // an executable test script
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(filepath.ListSeparator)+pathWithout(t, "claude"))
	return a
}

// does sets what the agent does on its next run.
func (a agent) does(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(a.bodyPath, []byte(body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// constructionOf dispatches the fixture component's construction task and returns the terminal
// observation. attempt distinguishes two dispatches of the same task on one branch.
func constructionOf(t *testing.T, access agenticjob.AgenticJobAccess, spec review.TaskSpec, attempt int) agenticjob.PipelineObservation {
	t.Helper()
	rc := fwra.Context{Context: context.Background(), IdempotencyKey: fwra.IdempotencyKey(fmt.Sprintf("ws-dispatch-%s-%d", t.Name(), attempt))}
	handle, err := access.SubmitAgenticJob(rc, agenticjob.PipelineSpec{
		ProjectID: "todomvc", ActivityID: freshActivity, Steps: []agenticjob.PipelineStep{{Name: "construct"}},
		DispatchInputs: map[string]string{
			"command": spec.Identity.Command, "component_id": freshComponent, "activity_id": freshActivity,
			"task_id": "construction", "spec_hash": spec.Hash, "revision": fmt.Sprintf("%s:construction:%d", freshActivity, attempt),
		},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return observeToTerminal(t, access, rc, handle)
}

// workspaceConstruction is the construction fixture: a project whose todo-list-access contract owns
// plat's lib directory, and the real spec of its construction task.
func workspaceConstruction(t *testing.T) (repo, plat, parent string, access agenticjob.AgenticJobAccess, spec review.TaskSpec, ag agent) {
	t.Helper()
	ag = installAgent(t)
	repo, plat, parent, access = workspaceFixtureOf(t, workspaceState(t, platRepos, wsOwned))
	spec = activitySpec(t, repo, freshActivity, "construction")
	if !slices.Contains(globsOf(spec), "@plat/lib/**") {
		t.Fatalf("the construction scope does not name the workspace directory: %v", globsOf(spec))
	}
	return repo, plat, parent, access, spec, ag
}

func globsOf(spec review.TaskSpec) []string {
	var out []string
	for _, c := range spec.EditScope.Components {
		out = append(out, c.Paths...)
	}
	return append(out, spec.EditScope.Extra...)
}

// resultOn is the status of check id in the CheckRun the run recorded on the activity branch, or "".
func resultOn(t *testing.T, repo, id string) string {
	t.Helper()
	run, ok := checkRunOnBranch(t, repo, "activity/"+freshActivity, freshActivity, "construction")
	if !ok {
		t.Fatal("the branch holds no CheckRun for the construction task")
	}
	for _, r := range run.Results {
		if r.CheckId == id {
			return r.Status
		}
	}
	return ""
}

const (
	platBadFmt  = `package lib\nfunc  A( ) {}\n`
	platGoodFmt = `package lib\n\nfunc A() {}\n`
)

// An attempt that edits only the workspace repo is a run that produced commits: it must not fail
// as "no commits on the target branch" (the primary branch did not move), and the checks of the
// workspace directory ran in that repo's worktree on what it committed.
func TestWorkspaceDispatch_AWorkspaceOnlyEditIsARunThatProducedCommits(t *testing.T) {
	repo, plat, _, access, spec, ag := workspaceConstruction(t)
	ag.does(t, `printf 'package lib\n\nfunc A() {}\n' > "$PLAT/lib/a.go"; git -C "$PLAT" add lib/a.go; (cd "$PLAT" && commit)`)
	obs := constructionOf(t, access, spec, 1)
	if strings.Contains(obs.Diagnostic, "produced no commits") {
		t.Fatalf("a workspace-only edit was failed as no commits: %s", obs.Diagnostic)
	}
	for _, id := range []string{"gofmt", "build", "vet", "test"} {
		if got := resultOn(t, repo, "command:"+id+"-ws-plat-lib"); got != "pass" {
			t.Errorf("check %s-ws-plat-lib over the committed edit: status %q, want pass (run: %s)", id, got, obs.Diagnostic)
		}
	}
	if got := gitOut(t, plat, "show", "activity/"+freshActivity+":lib/a.go"); !strings.Contains(got, "func A()") {
		t.Errorf("the platform branch does not hold the agent commit: %q (run: %+v)", got, obs)
	}
}

// What the checks judge in a workspace repo is what its gated branch HOLDS: an uncommitted fix and an
// untracked file the agent left behind must not make a red commit pass (the merge lands the branch,
// not the worktree).
func TestWorkspaceDispatch_TheCheckGateJudgesTheWorkspaceBranchNotItsWorktree(t *testing.T) {
	repo, _, _, access, spec, ag := workspaceConstruction(t)
	ag.does(t, `printf '`+platBadFmt+`' > "$PLAT/lib/a.go"; git -C "$PLAT" add lib/a.go; (cd "$PLAT" && commit)
printf '`+platGoodFmt+`' > "$PLAT/lib/a.go"; echo stray > "$PLAT/lib/untracked.go"`)
	obs := constructionOf(t, access, spec, 1)
	if obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("a branch holding a misformatted file passed its gate: %+v", obs)
	}
	if got := resultOn(t, repo, "command:gofmt-ws-plat-lib"); got != "fail" {
		t.Fatalf("gofmt over the workspace directory: status %q, want fail (the committed file is misformatted; the uncommitted fix is not the branch)", got)
	}
}

// Two attempts that leave different platform code and fail the same way are progress, not a repeat:
// the progress fingerprint covers the workspace repo's tree.
func TestWorkspaceDispatch_TheProgressFingerprintCoversTheWorkspaceTree(t *testing.T) {
	_, _, _, access, spec, ag := workspaceConstruction(t)
	ag.does(t, `printf 'package lib\nfunc  A( ) {}\n' > "$PLAT/lib/a.go"; git -C "$PLAT" add lib/a.go; (cd "$PLAT" && commit)`)
	first := constructionOf(t, access, spec, 1)
	ag.does(t, `printf 'package lib\nfunc  B( ) {}\n' > "$PLAT/lib/a.go"; git -C "$PLAT" add lib/a.go; (cd "$PLAT" && commit)`)
	second := constructionOf(t, access, spec, 2)
	if first.ProgressFingerprint == nil || second.ProgressFingerprint == nil {
		t.Fatalf("the red attempts left no fingerprint: %v, %v (%s | %s)", first.ProgressFingerprint, second.ProgressFingerprint, first.Diagnostic, second.Diagnostic)
	}
	if *first.ProgressFingerprint == *second.ProgressFingerprint {
		t.Fatal("two attempts that edited the platform differently, with the same failure, share a fingerprint: the loop would count them as no progress")
	}
}

// The edit-scope gate judges the workspace repo's diff beside the project's: a path outside the
// task's scope in the workspace repo is refused, and so is a project-repo file spelled like a path of
// the workspace repo's directory.
func TestWorkspaceDispatch_TheEditScopeGateJudgesTheWorkspaceDiff(t *testing.T) {
	_, _, _, access, spec, ag := workspaceConstruction(t)
	ag.does(t, `mkdir -p "$PLAT/other"; printf 'package other\n' > "$PLAT/other/y.go"; git -C "$PLAT" add other/y.go; (cd "$PLAT" && commit)
mkdir -p '@plat/lib'; printf 'package lib\n' > '@plat/lib/z.go'; git add -- '@plat/lib/z.go'; commit`)
	obs := constructionOf(t, access, spec, 1)
	if obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("out-of-scope edits passed the gate: %+v", obs)
	}
	for _, want := range []string{"@plat/other/y.go", "@plat/lib/z.go"} {
		if !strings.Contains(obs.Diagnostic, want) {
			t.Errorf("the edit-scope refusal does not name %s: %s", want, obs.Diagnostic)
		}
	}
}

// What the agent's sandbox may write is exactly its own worktree, its git dir, the build cache and,
// for the workspace repo, that repo's worktree and git dir: nothing broader.
func TestWorkspaceDispatch_TheSandboxOpensExactlyTheWorkspaceWorktreeAndGitDir(t *testing.T) {
	_, plat, _, access, spec, ag := workspaceConstruction(t)
	out := filepath.Join(t.TempDir(), "run.txt")
	ag.does(t, fmt.Sprintf(`cp ../aiarch-settings.json %q.settings; printf '%%s\n%%s\n%%s\n' "$OWN" "$PLAT" "$AIARCH_WORKSPACE_ROOTS" > %q`, out, out))
	_ = constructionOf(t, access, spec, 1)
	raw, err := os.ReadFile(out + ".settings") //nolint:gosec // G304: a file the test's own script wrote.
	if err != nil {
		t.Fatalf("the agent never ran (no settings copy): %v", err)
	}
	var cfg struct {
		Sandbox struct {
			Filesystem struct {
				AllowWrite []string `json:"allowWrite"`
			} `json:"filesystem"`
		} `json:"sandbox"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(mustRead(t, out)), "\n")
	own, platWT := resolved(t, lines[0]), resolved(t, lines[1])
	platGit := resolved(t, filepath.Join(plat, ".git"))
	var got []string
	for _, p := range cfg.Sandbox.Filesystem.AllowWrite {
		got = append(got, resolved(t, p))
	}
	if len(got) != 5 {
		t.Fatalf("the sandbox may write %d paths, want its worktree, git dir, build cache and the workspace worktree and git dir: %v", len(got), got)
	}
	for _, want := range []string{own, platWT, platGit} {
		if !slices.Contains(got, want) {
			t.Errorf("the sandbox does not open %s: %v", want, got)
		}
	}
	for _, p := range got {
		if p == "/" || p == filepath.Dir(own) || p == filepath.Dir(filepath.Dir(own)) {
			t.Errorf("the sandbox opens %s, wider than the run's own directories: %v", p, got)
		}
	}
	var roots map[string]string
	if err := json.Unmarshal([]byte(lines[2]), &roots); err != nil || resolved(t, roots["plat"]) != platWT {
		t.Errorf("AIARCH_WORKSPACE_ROOTS = %q, want plat -> the plat worktree %s", lines[2], platWT)
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p) //nolint:gosec // G304: a file the test's own script wrote.
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// resolved is p with symlinks resolved (macOS temp dirs sit behind one): through its longest
// ancestor that is still on disk, so a path the run has since removed compares like one that is not.
func resolved(t *testing.T, p string) string {
	t.Helper()
	rest := ""
	for dir := p; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest)
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
	return p
}

// The sandbox write path comes from the project's MAIN, not from the agent's checkout: a state
// document committed on the activity branch that points plat at another repo does not make the
// next attempt open that repo, and a scope naming a repo main does not declare is refused before any
// worktree of it exists.
func TestWorkspaceDispatch_TheDeclarationIsMainsNotTheAgentsCheckout(t *testing.T) {
	t.Run("a relPath rewritten on the activity branch", func(t *testing.T) {
		repo, plat, parent, access, spec, ag := workspaceConstruction(t)
		evil := siblingRepo(t, parent, "evil", map[string]string{"README.md": "evil\n"})
		branchState(t, repo, workspaceState(t, `[{"id":"plat","relPath":"../evil"}]`, wsOwned))
		ag.does(t, `true`)
		obs := constructionOf(t, access, spec, 1)
		if got := gitOut(t, evil, "branch", "--list", "activity/*"); got != "" {
			t.Fatalf("the repo the agent's checkout named got the activity branch: %q (run: %s)", got, obs.Diagnostic)
		}
		if got := gitOut(t, plat, "branch", "--list", "activity/"+freshActivity); got == "" {
			t.Errorf("the repo main declares got no worktree (run: %s)", obs.Diagnostic)
		}
	})
	t.Run("a scope naming a repo main does not declare", func(t *testing.T) {
		repo, _, parent, access, _, ag := workspaceConstruction(t)
		evil := siblingRepo(t, parent, "evil", map[string]string{"README.md": "evil\n"})
		branchState(t, repo, workspaceState(t, `[{"id":"plat2","relPath":"../evil"}]`, `["@plat2/lib"]`))
		spec := stateSpecOnBranch(t, repo)
		ag.does(t, `true`)
		obs := constructionOf(t, access, spec, 1)
		if obs.Phase != agenticjob.PhaseFailed || !strings.Contains(obs.Diagnostic, "workspace repo plat2, which the project does not declare") {
			t.Fatalf("the undeclared repo was not refused: %+v", obs)
		}
		if got := gitOut(t, evil, "branch", "--list", "activity/*"); got != "" {
			t.Fatalf("the undeclared repo got the activity branch: %q", got)
		}
	})
}

// branchState commits state onto the activity branch of the fixture project (created off main).
func branchState(t *testing.T, repo string, state []byte) {
	t.Helper()
	branch := "activity/" + freshActivity
	inRepo(t, repo, "git", "branch", branch, "main")
	wt := filepath.Join(t.TempDir(), "wt")
	inRepo(t, repo, "git", "worktree", "add", "--quiet", wt, branch)
	writeRepoFile(t, wt, ".aiarch/state/project.json", state)
	inRepo(t, wt, "git", "add", "-A")
	inRepo(t, wt, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "state on the branch")
	inRepo(t, repo, "git", "worktree", "remove", "--force", wt)
}

// stateSpecOnBranch is the construction spec of the state the activity branch holds.
func stateSpecOnBranch(t *testing.T, repo string) review.TaskSpec {
	t.Helper()
	raw := inRepo(t, repo, "git", "show", "activity/"+freshActivity+":.aiarch/state/project.json")
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON([]byte(raw), anyProject)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := deriveTaskSpec(&proj, freshActivity, "construction")
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
