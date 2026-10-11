# Agent Task Spec Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every agentic activity task gets one deterministic, computed AgentTaskSpec (identity, model+cost, prompt, tools, edit scope, checks) that the task panel renders and that both venues execute and gate on, with a session-scoped `run_checks()` MCP tool and a workflow-owned check gate that records a CheckRun.

**Architecture:** method-assets (platform, stdlib-only) resolves the *method* half of a spec — identity, charter model, rendered command prompt, tool allowlist — from embedded assets. archistrator's `projectstate` package adds the *project* half — edit scope, checks, model override, cost estimate, hash — and is imported by both the server (UI query, dispatch hash) and `aiarch-state-mcp` (job-side recompute, `run_checks`, gate, record). Jobs recompute the spec, refuse on hash mismatch, run the agent with the spec's model and allowlist, then gate edit scope + checks and record a CheckRun (which replaces TestRun).

**Tech Stack:** Go 1.25/1.26 (server, platform modules), MCP go-sdk, GitHub Actions templates (`[[ ]]` delimiters), React + MUI + TanStack Query (webApp), Playwright (generated uitests).

**Spec:** `docs/superpowers/specs/2026-10-11-agent-task-spec-design.md` (rulings R1–R8). Read it before any task.

## Global Constraints

- **No regressions:** Task 0 records the currently failing tests on main in both repos. Every test passing in that baseline must still pass at the end of every task. New tests must pass. Pre-existing reds may stay red.
- **Never weaken a gate** (lint, arch, drift, encapsulation). No `//nolint` added to silence a finding; fix the code.
- **Break freely (pre-prod):** no compat shims and no migration code (B19). archistrator's own `.aiarch/state/project.json` is conformed by a **data edit**.
- **Explicit-path staging only** (`git add <paths>`). Never `git add -A`, `git commit -am`, reset, or rebase in the main archistrator tree; another session works there.
- **No release before founder approval:** no platform tag, no pin bump, no server/webapp release until the founder approves the locally served app (Task D2).
- **Codegen:** never hand-edit `*.gen.*`; edit the source and regenerate (`make gen` in `server/`, `npm run gen:api && npm run gen:ops` in `webApp/`, `go run ./cmd/gen-stepmanifest` in method-assets).
- **Build modes:** platform modules test with `GOWORK=off` in their own dir. During development the server builds with the workspace (`go.work` → `../archistrator-platform/*`) so it sees unreleased platform changes. `GOWORK=off` server builds only after Task D3 bumps pins.
- **Platform commit authorship:** the platform repo's local git identity is `uitests`. Author every platform commit as the founder: `git -c user.name="David Marne" -c user.email="davemarne@gmail.com" commit ...`.
- **Commit trailer** on every commit: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Model aliases are CLI aliases (`opus`, `sonnet`, `fable`, `haiku`) passed verbatim to `--model`.

## Deviations from the spec (founder to confirm at plan review)

1. **Prompt (§4.1):** `prompt.text` is the command body with `$ARGUMENTS` substituted, which is exactly what Claude Code expands `/<command> <args>` into. The charter body and the skill names are separate fields (`prompt.charter`, `prompt.skills`) shown as their own collapsibles. Skills are not inlined: they load on demand, and inlining 20+ skills would *raise* context. The invocation stays `/<command> <args>`, and the spec hash proves the seated files equal what the panel shows.
2. **Query (§4.4):** the spec is served as a new `ProjectViewKind` `taskSpec` on the existing `QueryProjectView` (the `testRun` pattern), not as a 12th manager op.
3. **Rule checks (§4.3):** methodcheck has no per-rule registry (rule ids are unexported constants). A rule check is therefore one check per *rule family scope*, e.g. "Glossary rules (GLOSS-*)". It runs the existing `validate` machinery and fails on any Error finding in scope; per-rule findings appear in its output. The prefix-to-slot attribution table moves from `cmd/aiarch-state-mcp/staleness.go` into `projectstate` as the single registry.
4. **Split resolver (§4.2):** method-assets owns identity/model/prompt/tools (no new deps). `projectstate.TaskSpecFor` owns editScope/checks/estimate/hash. Server and `aiarch-state-mcp` both call `projectstate.TaskSpecFor`.
5. **CheckRun writes skip whole-document methodcheck:** evidence must be written red *or* green. `record-check-run` uses an evidence mutation (encode → decode read-back only), because `applyConstructionMutation` would refuse to record a red rule check.
6. **Deployment, documentation, and testing-harness/perf/qa variants** get only the state rule check plus the build check for their edit scope. There is no derivable command set for them yet; the panel says so.

## Review Focus

1. **Integration edit scope too tight.** Wiring may touch the composition root (`cmd/server/hooks.go`) and `go.mod`/`go.sum`. Expected: these are in `editScope` for integration and construction, so a legitimate wiring change is not rejected. Pinned by a test in Task B2.
2. **Subset scenario run wiping the index.** `run_checks({failedOnly:true})` runs two scenarios. Expected: the other scenarios' verdicts in `results.json` survive, and the `--gate` run starts from a clean `test-results/`, so no stale pass leaks into the gate. Pinned in Tasks A4 and B3.
3. **Hung scenario.** A scenario blocks forever. Expected: it is recorded as `timeout` within its cap and the run continues or ends red. It never hangs the job. Pinned in Tasks A4 and B3.
4. **Red rule check must still record.** A design draft with Error findings. Expected: the CheckRun is recorded with `fail` and the job is red; the record write itself never errors. Pinned in Task B4.
5. **Never-run / no-command tasks.** A human review task or a never-dispatched task. Expected: the panel renders the spec (or a review spec) without an episode and without crashing; "not run" on every check. Pinned in Tasks B2 and C3.

---

## File structure

**Platform (`/Users/davidmarne/mixofrealitystudio/archistrator-platform`, branch `agent-task-spec`)**
- `method-assets/stepmanifest.go` — `StepManifest.Model`
- `method-assets/cmd/gen-stepmanifest/main.go` — parse `model:`, grant `run_checks` to every mode
- `method-assets/stepmanifest.gen.go` — regenerated
- `method-assets/taskspec.go` (new) — `MethodSpec`, `MethodSpecFor`, `RenderCommand`
- `method-assets/taskspec_test.go` (new)
- `method-assets/assets/claude/commands/*.md` — integration steps rewritten; `run_checks()` instruction
- `method-assets/assets/workflows/aiarch-construct.yml.tmpl`, `aiarch-design.yml.tmpl` — spec / pre-step / model / allowlist / gate / record steps
- `method-assets/workflows_test.go`, `construct_injection_test.go`, `scenariosteps_test.go` — updated assertions
- `framework-go-scenariohost/results.go`, `results_test.go` — merge-on-flush, timeout status, per-scenario watchdog
- `framework-go-app-generator/testgen/emit_playwright.go` (+ golden) — reporter merges, per-test timeout

**archistrator (worktree `/Users/davidmarne/mixofrealitystudio/archistrator-ats`, branch `agent-task-spec`)**
- `.aiarch/state/project.json` — `CheckRun` replaces `TestRun` in `projectStateAccess.$defs`; `taskSpec` view defs + kind in `deliveryManager.$defs` and `mcpClient.$defs`; data conformed
- `server/internal/resourceaccess/projectstate/ruleattribution.go` (new, moved from staleness.go)
- `server/internal/resourceaccess/projectstate/taskspec.go` (new) — `TaskSpec`, `TaskSpecFor`, edit scope, checks, hash
- `server/internal/resourceaccess/projectstate/pricing.go` (new, moved from deliverymanager.go)
- `server/internal/resourceaccess/projectstate/taskspec_test.go`, `pricing_test.go` (new)
- `server/cmd/aiarch-state-mcp/checks.go` (new) — check runner
- `server/cmd/aiarch-state-mcp/taskspeccmd.go`, `runchecks.go`, `recordcheckrun.go` (new); `recordtestrun.go` deleted
- `server/cmd/aiarch-state-mcp/tools.go`, `main.go`, `staleness.go`, `constructverbs.go`, `activityrecords.go`
- `server/internal/manager/delivery/deliverymanager.go`, `deliveryactivity.go`
- `server/internal/resourceaccess/agenticjob/agenticjobaccess.go`
- `webApp/src/contracts/{types.ts,wire.ts}`, `webApp/src/hooks/useTaskSpec.ts` (new), `webApp/src/api/fixtureOps.ts`
- `webApp/src/components/activity/TaskSpecPanel.tsx` (new), `taskSpecPanelLogic.ts` (new), `TaskHeader.tsx`, `DispatchBody.tsx`, `ReviewBody.tsx`, `activityViewToGraph.ts`
- `webApp/src/components/construction/renderers/ScenarioRow.tsx` (new, extracted from TestRunPanel.tsx), `TestRunPanel.tsx`, `testRunPanelLogic.ts`
- `webApp/src/containers/ActivityExperienceContainer.tsx`

---

## Task 0: Workspaces and baseline

**Files:**
- Create: `/private/tmp/claude-501/-Users-davidmarne-mixofrealitystudio-archistrator/0d636cd3-8c7b-4fdc-bbb8-146290c55863/scratchpad/baseline/*.txt`, and a copy under `docs/bugs/2026-10-11-agent-task-spec-baseline.md` in the worktree

- [ ] **Step 1: Create the branches**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git worktree add ../archistrator-ats -b agent-task-spec main
cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
git status --porcelain   # must be empty; stop and report if not
git checkout -b agent-task-spec main
```

- [ ] **Step 2: Baseline the platform modules**

```bash
B=/private/tmp/claude-501/-Users-davidmarne-mixofrealitystudio-archistrator/0d636cd3-8c7b-4fdc-bbb8-146290c55863/scratchpad/baseline
mkdir -p $B
cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
for m in method-assets framework-go framework-go-scenariohost framework-go-app-generator; do
  (cd $m && GOWORK=off go test ./... -count=1 -json 2>&1 | jq -r 'select(.Action=="fail" and .Test!=null) | "\(.Package) \(.Test)"' | sort -u > $B/platform-$m.txt)
done
wc -l $B/platform-*.txt
```

- [ ] **Step 3: Baseline the server (workspace build, as development will use)**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-ats/server
go test -p 1 -timeout 90m ./... -count=1 -json 2>&1 | jq -r 'select(.Action=="fail" and .Test!=null) | "\(.Package) \(.Test)"' | sort -u > $B/server-fail.txt
go test -p 1 -timeout 90m ./... -count=1 -json 2>&1 | jq -r 'select(.Action=="fail" and .Test==null) | .Package' | sort -u > $B/server-pkgfail.txt
make lint > $B/server-lint.txt 2>&1; echo "lint exit $?" >> $B/server-lint.txt
make gen-check > $B/server-gencheck.txt 2>&1 || true   # if no aggregate target, run each gen-*-check listed in Makefile
```

Run once: if Docker/Temporal is not up, the scenario packages fail at boot. Record that as baseline-red only if it is also red with the stack up (`docker ps` shows `systemtests-postgres-1`). Start the stack per `uitests/README.md` before this step.

- [ ] **Step 4: Baseline the webApp**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-ats/webApp
npm ci
npm run typecheck > $B/web-typecheck.txt 2>&1; echo "exit $?" >> $B/web-typecheck.txt
npm run lint > $B/web-lint.txt 2>&1; echo "exit $?" >> $B/web-lint.txt
npm test > $B/web-test.txt 2>&1; echo "exit $?" >> $B/web-test.txt
```

- [ ] **Step 5: Write the baseline doc and commit it**

Write `docs/bugs/2026-10-11-agent-task-spec-baseline.md` listing every failing test or package from the files above, verbatim, under one heading per suite, with the main SHA it was taken at.

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-ats
git add docs/bugs/2026-10-11-agent-task-spec-baseline.md
git commit -m "docs(ats): test baseline before agent task spec

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

**Regression check used by every later task** (call it `regress-check`): re-run the relevant suite with the same `jq` filter and `comm -13 baseline new`. Any line printed is a regression and blocks the task.

---

# Part A — Platform

## Task A1: Charter model into the step manifest

**Files:**
- Modify: `method-assets/stepmanifest.go:50` (StepManifest)
- Modify: `method-assets/cmd/gen-stepmanifest/main.go` (`parseCharters` :373, `parseToolsBlock` :388, `manifestFor` :220, `render` :420)
- Regenerate: `method-assets/stepmanifest.gen.go`
- Test: `method-assets/stepmanifest_test.go`

**Interfaces:**
- Produces: `StepManifest.Model string` (charter `model:` value, e.g. `"opus"`); `func ModelForAgent(agent string) (string, bool)` in `stepmanifest.go`.

- [ ] **Step 1: Write the failing test** (append to `stepmanifest_test.go`)

```go
func TestStepManifestCarriesTheCharterModel(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for slug, m := range Manifests() {
		body := string(files[".claude/agents/"+m.Agent+".md"])
		want := charterModel(t, body)
		if m.Model != want {
			t.Errorf("%s: Model = %q, charter %s says %q", slug, m.Model, m.Agent, want)
		}
		if m.Model == "" {
			t.Errorf("%s: charter %s has no model: line", slug, m.Agent)
		}
	}
	if got, ok := ModelForAgent("junior-developer"); !ok || got != "sonnet" {
		t.Errorf("ModelForAgent(junior-developer) = %q,%v; want sonnet,true", got, ok)
	}
	if _, ok := ModelForAgent("no-such-agent"); ok {
		t.Error("ModelForAgent(no-such-agent) reported ok")
	}
}

func charterModel(t *testing.T, body string) string {
	t.Helper()
	fm, _, ok := strings.Cut(strings.TrimPrefix(body, "---\n"), "\n---")
	if !ok {
		t.Fatalf("charter has no frontmatter")
	}
	for l := range strings.SplitSeq(fm, "\n") {
		if v, ok := strings.CutPrefix(l, "model:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `cd method-assets && GOWORK=off go test -run TestStepManifestCarriesTheCharterModel ./...`
Expected: compile failure, `m.Model undefined`.

- [ ] **Step 3: Implement**

In `stepmanifest.go`, add the field after `Mode`, plus the helper:

```go
	Mode    string   // draft|critique|answer|construct
	Model   string   // the charter's model: line — the CLI --model alias this step runs on
}

// ModelForAgent is the model a charter declares, read from any manifest whose
// Agent is that charter. Every charter backs at least one command, so a charter
// with no manifest is not a dispatchable worker class.
func ModelForAgent(agent string) (string, bool) {
	for _, m := range stepManifests {
		if m.Agent == agent {
			return m.Model, true
		}
	}
	return "", false
}
```

In `cmd/gen-stepmanifest/main.go`:
- Change `parseCharters` to return `map[string]charter`, where `type charter struct{ tools []string; model string }`.
- Add `parseModel(body string) string`, which scans the same frontmatter as `parseToolsBlock` for a `model:` line.
- Update `allowedTools(charter, ...)` callers to pass `c.tools`.
- In `manifestFor`, set `Model: charters[agent].model` and fail generation with `fmt.Errorf("charter %s has no model: line", agent)` if it is empty.

```go
type charter struct {
	tools []string
	model string
}

func parseModel(body string) string {
	fm, _, ok := strings.Cut(strings.TrimPrefix(body, "---\n"), "\n---")
	if !ok {
		return ""
	}
	for l := range strings.SplitSeq(fm, "\n") {
		if v, ok := strings.CutPrefix(l, "model:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
```

In `render`, emit after Mode: `fmt.Fprintf(&b, "\t\tModel:   %q,\n", m.Model)`.

- [ ] **Step 4: Regenerate and run the tests**

```bash
cd method-assets && GOWORK=off go run ./cmd/gen-stepmanifest -o stepmanifest.gen.go
GOWORK=off go test ./... -count=1
```
Expected: PASS, including `TestStepManifestRegenerationIsNoOp`. regress-check against `platform-method-assets.txt`.

- [ ] **Step 5: Commit**

```bash
git add method-assets/stepmanifest.go method-assets/stepmanifest.gen.go method-assets/cmd/gen-stepmanifest/main.go method-assets/stepmanifest_test.go
git -c user.name="David Marne" -c user.email="davemarne@gmail.com" commit -m "method-assets: the step manifest carries the charter's model

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

## Task A2: Grant `run_checks` to every mode

**Files:**
- Modify: `method-assets/cmd/gen-stepmanifest/main.go:89-118`
- Regenerate: `method-assets/stepmanifest.gen.go`
- Test: `method-assets/stepmanifest_test.go`

**Interfaces:**
- Produces: every `StepManifest.Tools` contains `mcp__aiarch-state__run_checks`; `MCPTools()` includes `run_checks`.

- [ ] **Step 1: Failing test**

```go
func TestEveryStepIsGrantedRunChecks(t *testing.T) {
	for slug, m := range Manifests() {
		if !slices.Contains(m.MCPTools(), "run_checks") {
			t.Errorf("%s: run_checks not granted", slug)
		}
	}
}
```

- [ ] **Step 2: Run it** — `GOWORK=off go test -run TestEveryStepIsGrantedRunChecks ./...` → FAIL.

- [ ] **Step 3: Implement**

```go
// in composedVerbModes
	"run_checks": {modeDraft, modeCritique, modeAnswer, modeConstruct},

// modeImplicitVerbs: run_checks is the step's own gate. Every step runs its
// checks, whatever its charter lists.
var modeImplicitVerbs = map[string][]string{
	modeDraft:     {"run_checks"},
	modeCritique:  {"run_checks"},
	modeAnswer:    {"run_checks"},
	modeConstruct: {"getOperatorNotes", "run_checks"},
}
```

- [ ] **Step 4: Regenerate and test** — same commands as A1 Step 4. Expected PASS.

- [ ] **Step 5: Commit** — `method-assets: every step is granted run_checks`.

## Task A3: MethodSpec resolver

**Files:**
- Create: `method-assets/taskspec.go`, `method-assets/taskspec_test.go`

**Interfaces:**
- Consumes: `LifecycleFor`, `ManifestFor`, `ClaudeFiles`, `StepManifest.Model` (A1).
- Produces:

```go
type MethodSpec struct {
	LifecycleKey  string
	TaskID        string
	Kind          string // LifecycleTaskDispatch | LifecycleTaskReview
	Title         string
	Phase         string
	Command       string // "" for a review task with no command
	Agent         string // charter stem; == WorkerClass
	WorkerClass   string
	ArtifactKind  string // task's, else the reviewed task's
	ExitCriterion string // the phase's
	Mode          string // manifest mode
	Model         string // charter model alias
	Prompt        string // command body, $ARGUMENTS substituted
	Charter       string // charter body (frontmatter stripped)
	Skills        []string
	MCPTools      []string // sorted, prefix-stripped
	BuiltinTools  []string // sorted
}
func MethodSpecFor(lifecycleKey, taskID string, args []string) (MethodSpec, error)
func RenderCommand(command string, args []string) (string, error)
```

- [ ] **Step 1: Failing tests**

```go
func TestMethodSpecForServiceConstruction(t *testing.T) {
	s, err := MethodSpecFor("service", "construction", []string{"agentic-job-access", "C-agentic-job-access"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Command != "service-construction" || s.WorkerClass != "junior-developer" || s.Model != "sonnet" {
		t.Fatalf("identity/model = %+v", s)
	}
	if s.ExitCriterion == "" || s.ArtifactKind != "Construction" || s.Mode != "construct" {
		t.Fatalf("phase facts = %+v", s)
	}
	if strings.Contains(s.Prompt, "$ARGUMENTS") || !strings.Contains(s.Prompt, "agentic-job-access C-agentic-job-access") {
		t.Fatalf("prompt not rendered:\n%s", s.Prompt)
	}
	if strings.HasPrefix(s.Charter, "---") || s.Charter == "" {
		t.Fatalf("charter not stripped of frontmatter")
	}
	if !slices.Contains(s.MCPTools, "run_checks") || !slices.Contains(s.BuiltinTools, "Bash") {
		t.Fatalf("tools = %v %v", s.MCPTools, s.BuiltinTools)
	}
}

func TestMethodSpecForReviewWithoutCommand(t *testing.T) {
	s, err := MethodSpecFor("service", "codeReview", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != LifecycleTaskReview || s.Command != "" || s.Prompt != "" || len(s.MCPTools) != 0 {
		t.Fatalf("review spec = %+v", s)
	}
	if s.ArtifactKind != "Construction" { // codeReview reviews construction
		t.Fatalf("artifactKind = %q", s.ArtifactKind)
	}
}

func TestMethodSpecForEveryLifecycleTask(t *testing.T) {
	for _, l := range Lifecycles() {
		for _, task := range l.Tasks {
			s, err := MethodSpecFor(l.Type, task.ID, []string{"c", "a"})
			if err != nil {
				t.Errorf("%s/%s: %v", l.Type, task.ID, err)
				continue
			}
			if task.Command != "" && (s.Model == "" || s.Prompt == "") {
				t.Errorf("%s/%s: dispatchable task has empty model or prompt", l.Type, task.ID)
			}
		}
	}
}

func TestMethodSpecForUnknown(t *testing.T) {
	if _, err := MethodSpecFor("service", "nope", nil); err == nil {
		t.Fatal("unknown task resolved")
	}
	if _, err := MethodSpecFor("nope", "construction", nil); err == nil {
		t.Fatal("unknown lifecycle resolved")
	}
}

func TestRenderCommandDesignTakesNoArguments(t *testing.T) {
	p, err := RenderCommand("mission-draft", nil)
	if err != nil || p == "" || strings.Contains(p, "$ARGUMENTS") {
		t.Fatalf("RenderCommand(mission-draft) = %q, %v", p, err)
	}
}
```

- [ ] **Step 2: Run them** — FAIL (undefined).

- [ ] **Step 3: Implement `taskspec.go`**

```go
package methodassets

import (
	"fmt"
	"strings"
)

// MethodSpec is the method half of an agent task spec: everything the embedded
// lifecycle, step manifest, command, charter and skill assets decide about one
// lifecycle task, with no project data. archistrator's projectstate.TaskSpecFor
// adds the project half (edit scope, checks, cost, hash).
type MethodSpec struct { /* fields exactly as in Interfaces above */ }

// MethodSpecFor resolves one lifecycle task. args are the command's
// $ARGUMENTS tokens (<component_id> <activity_id> for construction commands,
// none for design commands). A review task with no command resolves to its
// identity only.
func MethodSpecFor(lifecycleKey, taskID string, args []string) (MethodSpec, error) {
	l, ok := LifecycleFor(lifecycleKey)
	if !ok {
		return MethodSpec{}, fmt.Errorf("no lifecycle %q", lifecycleKey)
	}
	var task *LifecycleTask
	for i := range l.Tasks {
		if l.Tasks[i].ID == taskID {
			task = &l.Tasks[i]
		}
	}
	if task == nil {
		return MethodSpec{}, fmt.Errorf("lifecycle %q has no task %q", lifecycleKey, taskID)
	}
	s := MethodSpec{
		LifecycleKey: lifecycleKey, TaskID: taskID, Kind: task.Kind, Title: task.Title,
		Phase: task.Phase, Command: task.Command, Agent: task.WorkerClass, WorkerClass: task.WorkerClass,
		ArtifactKind: artifactKindOf(l, *task),
	}
	for _, p := range l.Phases {
		if p.ID == task.Phase {
			s.ExitCriterion = p.ExitCriterion
		}
	}
	if task.Command == "" {
		return s, nil
	}
	m, ok := ManifestFor(task.Command)
	if !ok {
		return MethodSpec{}, fmt.Errorf("task %s/%s: no manifest for command %q", lifecycleKey, taskID, task.Command)
	}
	files, err := ClaudeFiles()
	if err != nil {
		return MethodSpec{}, err
	}
	prompt, err := RenderCommand(task.Command, args)
	if err != nil {
		return MethodSpec{}, err
	}
	s.Mode, s.Model, s.Prompt = m.Mode, m.Model, prompt
	s.Charter = stripFrontmatter(string(files[".claude/agents/"+m.Agent+".md"]))
	s.Skills = append([]string(nil), m.Skills...)
	s.MCPTools, s.BuiltinTools = m.MCPTools(), m.BuiltinTools()
	return s, nil
}

// RenderCommand is what Claude Code expands "/<command> <args>" into: the
// command body with $ARGUMENTS replaced by the space-joined args.
func RenderCommand(command string, args []string) (string, error) {
	files, err := ClaudeFiles()
	if err != nil {
		return "", err
	}
	body, ok := files[".claude/commands/"+command+".md"]
	if !ok {
		return "", fmt.Errorf("no command %q", command)
	}
	return strings.ReplaceAll(string(body), "$ARGUMENTS", strings.Join(args, " ")), nil
}

func stripFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	_, rest, ok := strings.Cut(strings.TrimPrefix(body, "---\n"), "\n---")
	if !ok {
		return body
	}
	return strings.TrimLeft(rest, "\n")
}

// artifactKindOf mirrors webApp activityViewToGraph.artifactKindOf: the task's
// own kind, else the reviewed task's, else a sibling in the same revision group.
func artifactKindOf(l Lifecycle, t LifecycleTask) string {
	if t.ArtifactKind != "" {
		return t.ArtifactKind
	}
	for _, o := range l.Tasks {
		if o.ID == t.Reviews && o.ArtifactKind != "" {
			return o.ArtifactKind
		}
	}
	return ""
}
```

(`ClaudeFiles()` builds a map on every call. Cache it in a package-level `sync.OnceValues` if the per-call cost shows up in the B-tasks' tests.)

- [ ] **Step 4: Run** — `GOWORK=off go test ./... -count=1` → PASS. regress-check.

- [ ] **Step 5: Commit** — `method-assets: MethodSpecFor resolves a lifecycle task's identity, model, prompt and tools`.

## Task A4: scenariohost — merge on flush, timeouts

**Files:**
- Modify: `framework-go-scenariohost/results.go` (`StatusSkip` block, `writeIndex` :213-228, `RunScenario` :245)
- Test: `framework-go-scenariohost/results_test.go`

**Interfaces:**
- Produces: `StatusTimeout = "timeout"`. `writeIndex` merges by scenario id into an existing `results.json`. `RunScenario` enforces `ScenarioTimeout()` (env `AIARCH_SCENARIO_TIMEOUT`, Go duration, default `5m`): on expiry it records `timeout`, flushes, and exits the process with code 1.

- [ ] **Step 1: Failing tests**

```go
func TestWriteIndexMergesWithThePriorIndex(t *testing.T) {
	h := &Host{ResultsRoot: t.TempDir()}
	h.Record("c", ScenarioResult{Scenario: "a", Status: StatusPass})
	h.Record("c", ScenarioResult{Scenario: "b", Status: StatusFail})
	if err := h.writeIndex("c", h.resultsFor("c")); err != nil {
		t.Fatal(err)
	}
	h2 := &Host{ResultsRoot: h.ResultsRoot}
	h2.Record("c", ScenarioResult{Scenario: "b", Status: StatusPass})
	if err := h2.writeIndex("c", h2.resultsFor("c")); err != nil {
		t.Fatal(err)
	}
	got := readIndex(t, filepath.Join(h.ResultsRoot, "c", "results.json"))
	if len(got) != 2 || got[0].Scenario != "a" || got[0].Status != StatusPass || got[1].Status != StatusPass {
		t.Fatalf("merged index = %+v", got)
	}
}

func TestScenarioTimeoutDefaultAndEnv(t *testing.T) {
	t.Setenv("AIARCH_SCENARIO_TIMEOUT", "")
	if ScenarioTimeout() != 5*time.Minute {
		t.Fatal("default")
	}
	t.Setenv("AIARCH_SCENARIO_TIMEOUT", "2s")
	if ScenarioTimeout() != 2*time.Second {
		t.Fatal("env")
	}
}
```

Use the existing `writeIndex` signature at :213 (adapt `h.resultsFor` to however `verdictsByComponent` exposes a component's results; read :183-228 first). `readIndex` is a test helper that unmarshals `[]ScenarioResult`. For the watchdog, add a subprocess test: re-exec the test binary with `-test.run=TestHelperHangingScenario` and `AIARCH_SCENARIO_TIMEOUT=1s`, then assert exit code 1 and an index entry with `status: timeout`.

- [ ] **Step 2: Run** — FAIL.

- [ ] **Step 3: Implement**

```go
const StatusTimeout = "timeout"

// ScenarioTimeout is the per-scenario cap. A scenario that outlives it is recorded
// as timeout and the process exits, so one hung scenario can never hang a job.
func ScenarioTimeout() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("AIARCH_SCENARIO_TIMEOUT")); err == nil && d > 0 {
		return d
	}
	return 5 * time.Minute
}
```

In `writeIndex`, before sorting: read the existing `results.json` if present (ignore `os.ErrNotExist`, return any other error), build `map[scenario]ScenarioResult` from it, overlay this process's verdicts, then flatten, sort by Scenario, and write. Update the doc comment at :133-146: subset runs merge, and callers that want a fresh index delete `test-results/<component>/` first.

In `RunScenario`, start a timer of `ScenarioTimeout()` before calling `fn`:

```go
timer := time.AfterFunc(ScenarioTimeout(), func() {
	h.Record(component, ScenarioResult{Scenario: id, Status: StatusTimeout,
		Stderr: fmt.Sprintf("scenario exceeded %s", ScenarioTimeout())})
	_ = h.writeIndexes() // flush what this process has; replay is skipped on timeout
	fmt.Fprintf(os.Stderr, "scenariohost: %s/%s timed out\n", component, id)
	os.Exit(1)
})
defer timer.Stop()
```

`writeIndexes` refuses undeclared verdicts. A timeout is a declared scenario, so this is fine; confirm against :155-177.

- [ ] **Step 4: Run** — `cd framework-go-scenariohost && GOWORK=off go test ./... -count=1` → PASS. regress-check.

- [ ] **Step 5: Commit** — `scenariohost: subset runs merge into the index; a hung scenario times out`.

## Task A5: Playwright reporter — merge, per-test timeout

**Files:**
- Modify: `framework-go-app-generator/testgen/emit_playwright.go` (`playwrightReporter` const :206-250, `playwrightConfig` :183)
- Update goldens: `framework-go-app-generator/testgen/testdata/*` (regenerate with the package's golden-update flag; grep `update` in `testgen_test.go`)

**Interfaces:**
- Produces: the reporter's `onEnd` reads the existing `results.json` in `outputDir` and overlays this run's verdicts by scenario. A Playwright `timedOut` status maps to `timeout`. The config sets `timeout: Number(process.env.AIARCH_SCENARIO_TIMEOUT_MS ?? 300000)`.

- [ ] **Step 1: Failing test** in `testgen_test.go`: assert the emitted reporter source contains `existsSync(` + `readFileSync(` before the write, and maps `timedOut` to `'timeout'`; assert the emitted config contains `AIARCH_SCENARIO_TIMEOUT_MS`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** — in the reporter template's `onEnd`:

```ts
const file = path.join(outDir, 'results.json');
const prior: Result[] = fs.existsSync(file) ? JSON.parse(fs.readFileSync(file, 'utf8')) : [];
const merged = new Map(prior.map((r) => [r.scenario, r]));
for (const r of this.results) merged.set(r.scenario, r);
fs.writeFileSync(file, JSON.stringify([...merged.values()].sort((a, b) => a.scenario.localeCompare(b.scenario)), null, 2));
```

In the status mapping, use `result.status === 'timedOut' ? 'timeout' : ...`. In the config, add `timeout: Number(process.env.AIARCH_SCENARIO_TIMEOUT_MS ?? 300000),`.

- [ ] **Step 4: Regenerate goldens and run** — `cd framework-go-app-generator && GOWORK=off go test ./... -count=1` → PASS. regress-check.
- [ ] **Step 5: Commit** — `app-generator/testgen: the Playwright reporter merges subset runs; per-test timeout`.

## Task A6: Command prompts use `run_checks`

**Files:**
- Modify: `method-assets/assets/claude/commands/service-integration.md`, `frontend-integration.md` (steps 4, 7, 9 and the frontend equivalents)
- Modify: every construct-mode and draft/critique command body: add one standard line
- Test: `method-assets/stepmanifest_test.go` (or a new `commands_test.go`)

**Interfaces:**
- Produces: every command with a manifest contains the exact line ``**Checks.** Call `run_checks()` until it reports every check passing, and finish with one full `run_checks()` call. Do not run the checks yourself.``. No command contains `record-test-run`, `make test-scenarios` or `make gen-tests`.

- [ ] **Step 1: Failing test**

```go
const checksLine = "**Checks.** Call `run_checks()` until it reports every check passing, and finish with one full `run_checks()` call. Do not run the checks yourself."

func TestEveryStepCommandRunsItsChecks(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for slug := range Manifests() {
		body := string(files[".claude/commands/"+slug+".md"])
		if !strings.Contains(body, checksLine) {
			t.Errorf("%s: missing the run_checks line", slug)
		}
		for _, banned := range []string{"record-test-run", "make test-scenarios", "make gen-tests"} {
			if strings.Contains(body, banned) {
				t.Errorf("%s: still tells the agent to %q (the workflow owns it)", slug, banned)
			}
		}
	}
}
```

- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Edit the commands.**
  - Insert `checksLine` as its own paragraph directly above `## Steps` in every manifest command.
  - In `service-integration.md`:
    - replace step 4 with: "The tests are already generated: the job ran `make gen-tests` before you started. Never edit a generated file and never change the plan."
    - delete step 7 (run) and step 9 (record), and renumber the rest;
    - in step 8, drop "Never commit `test-results/`" only if testgen no longer writes there; otherwise keep it;
    - step 3 "Verify" becomes "call `run_checks()`".
  - Make the same edits to `frontend-integration.md` (its run/copy/record steps).
  - Update the Goal paragraphs: "the job generates and runs the scenario tests and records the run; you wire and fill hooks".
  - Construction commands: replace the "Verify YOUR code" step's commands with "call `run_checks()`", and keep "No tests here".
- [ ] **Step 4: Run** — `GOWORK=off go test ./... -count=1` → PASS. regress-check (the `commandwrites_test.go`/`scenariosteps_test.go` assertions on integration text may need updating to the new wording; update those assertions, never delete a test).
- [ ] **Step 5: Commit** — `method-assets: steps call run_checks; the job owns test generation, runs and records`.

## Task A7: Workflow templates — spec, model, allowlist, gate, record

**Files:**
- Modify: `method-assets/assets/workflows/aiarch-construct.yml.tmpl`, `aiarch-design.yml.tmpl`
- Modify tests: `method-assets/workflows_test.go`, `construct_injection_test.go`, `workflow_injection_test.go`, `scenariosteps_test.go`

**Interfaces:**
- Consumes (Part B binary subcommands, specified here so both sides match):
  - `aiarch-state-mcp task-spec --activity <id> --task <taskId> [--root .] --out <file>` prints the hash on stdout and writes the spec JSON to `--out`.
  - `aiarch-state-mcp run-checks --gate --spec <file> --out <dir>` writes `<dir>/results.json` (`[]CheckResult`) and `<dir>/checks/<checkId>.log`. It exits 0 when all pass, 1 when any fails or times out, and 2 on internal error.
  - `aiarch-state-mcp edit-scope --spec <file> --base <sha> --head <sha>` exits 1 on violation and prints the offending paths.
  - `aiarch-state-mcp record-check-run --spec <file> --results <dir>/results.json --run-id <id> --artifact <ref> --revision <rev>` writes the CheckRun into `.aiarch/state/project.json` in the working tree.
- New dispatch inputs (construct): `task_id` (required), `spec_hash` (required). Design: `activity_id`, `task_id`, `spec_hash` (all required).

- [ ] **Step 1: Failing tests** in `workflows_test.go`:

```go
func TestWorkflowTemplatesRunTheSpec(t *testing.T) {
	for _, name := range []string{"aiarch-construct.yml.tmpl", "aiarch-design.yml.tmpl"} {
		body := templateSource(t, name) // existing helper or read from assetsFS
		for _, want := range []string{
			`task-spec --activity "${AIARCH_ACTIVITY_ID}" --task "${AIARCH_TASK_ID}"`,
			`steps.spec.outputs.model`,
			`--tools ${{ steps.spec.outputs.builtin }}`,
			`edit-scope --spec`,
			`run-checks --gate --spec`,
			`record-check-run --spec`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", name, want)
			}
		}
		for _, gone := range []string{"--disallowedTools", "step-tools --command", "Fail on a red scenario run"} {
			if strings.Contains(body, gone) {
				t.Errorf("%s: still has %q", name, gone)
			}
		}
	}
}
```

Update `TestWorkflowTemplatesScopeThePromptSurface` to drop the `step-tools` and `disallowed` substrings. Add the new inputs to the hostile-input validation tests: both ids must match the existing id regex, and `spec_hash` must match `^[0-9a-f]{64}$`.

- [ ] **Step 2: Run** — FAIL.

- [ ] **Step 3: Edit `aiarch-construct.yml.tmpl`**
  - Inputs: add `task_id` and `spec_hash` (required, strings). Validate them in "Validate dispatch inputs" with the existing id regex and the hash regex, and export `AIARCH_TASK_ID` / `AIARCH_SPEC_HASH` to `$GITHUB_ENV`.
  - Replace "Resolve step tools" with:

```yaml
      - name: Resolve the task spec
        id: spec
        run: |
          got="$("${MCP_BIN}" task-spec --activity "${AIARCH_ACTIVITY_ID}" --task "${AIARCH_TASK_ID}" --root . --out "${RUNNER_TEMP}/spec.json")"
          if [ "${got}" != "${AIARCH_SPEC_HASH}" ]; then
            echo "::error::task spec drifted: dispatched ${AIARCH_SPEC_HASH}, job resolved ${got}"; exit 1
          fi
          echo "model=$(jq -r .model.id "${RUNNER_TEMP}/spec.json")" >> "$GITHUB_OUTPUT"
          echo "builtin=$(jq -r '.tools.builtin | join(",")' "${RUNNER_TEMP}/spec.json")" >> "$GITHUB_OUTPUT"
          git fetch origin "activity/${AIARCH_ACTIVITY_ID}" 2>/dev/null && base="$(git rev-parse FETCH_HEAD)" || base="$(git rev-parse HEAD)"
          echo "base=${base}" >> "$GITHUB_OUTPUT"
      - name: Generate the scenario tests
        if: ${{ inputs.phase == 'integration' }}
        env: { GOWORK: "off" }
        run: make gen-tests
```

  - Delete "Expose the recorder" (:288-299). Keep `AIARCH_REVISION` in the MCP env, because `record-check-run` needs it.
  - The claude step:

```yaml
            claude_args: "--dangerously-skip-permissions --model ${{ steps.spec.outputs.model }} --tools ${{ steps.spec.outputs.builtin }} --mcp-config ${{ runner.temp }}/aiarch-mcp.json --strict-mcp-config --setting-sources project --exclude-dynamic-system-prompt-sections"
```

    (`--tools` limits the built-ins to the allowlist. MCP tools are already limited by the server's grant, so the deny list goes away.)

  - Replace the post-agent steps (:335-378) with:

```yaml
      - name: Gate the edit scope
        if: ${{ always() && steps.spec.outcome == 'success' }}
        run: |
          git fetch origin "activity/${AIARCH_ACTIVITY_ID}"
          "${MCP_BIN}" edit-scope --spec "${RUNNER_TEMP}/spec.json" --base "${{ steps.spec.outputs.base }}" --head FETCH_HEAD
      - name: Run the checks
        id: checks
        if: ${{ always() && steps.spec.outcome == 'success' }}
        run: |
          git checkout -B "activity/${AIARCH_ACTIVITY_ID}" FETCH_HEAD
          rm -rf test-results
          set +e; "${MCP_BIN}" run-checks --gate --spec "${RUNNER_TEMP}/spec.json" --out check-results; rc=$?; set -e
          echo "rc=${rc}" >> "$GITHUB_OUTPUT"
          [ "${rc}" -le 1 ] || exit "${rc}"
      - name: Upload check results
        if: ${{ always() && steps.checks.outcome == 'success' }}
        uses: actions/upload-artifact@v4
        with:
          name: check-results-${{ inputs.activity_id }}-${{ github.run_id }}
          path: |
            check-results/**
            test-results/**
          retention-days: 90
      - name: Record the check run
        if: ${{ always() && steps.checks.outcome == 'success' }}
        run: |
          "${MCP_BIN}" record-check-run --spec "${RUNNER_TEMP}/spec.json" --results check-results/results.json \
            --run-id "gh-${GITHUB_RUN_ID}" --artifact "check-results-${AIARCH_ACTIVITY_ID}-${GITHUB_RUN_ID}" --revision "${AIARCH_REVISION}"
          git add .aiarch/state/project.json
          git -c user.name="aiarch-checks" -c user.email="checks@aiarch.invalid" commit -m "checks: record run gh-${GITHUB_RUN_ID}"
          git push origin "HEAD:activity/${AIARCH_ACTIVITY_ID}"
      - name: Fail on a red check
        if: ${{ always() && steps.checks.outputs.rc != '0' }}
        run: exit 1
```

  Keep the "no `${{ }}` inside run bodies" rule that `NoExpressionInsideAnyRunBody` enforces: pass `steps.spec.outputs.base` through `env:` (`AIARCH_BASE`), not inline. The snippet above inlines it only for readability, and the implementer must move it to `env:`.

- [ ] **Step 4: Edit `aiarch-design.yml.tmpl`** the same way:
  - add the `activity_id`, `task_id` and `spec_hash` inputs;
  - add the spec step after "Record base SHA" (the base is the existing `steps.base` output);
  - add `--model`/`--tools` and drop `--disallowedTools`;
  - after the "Verify claude pushed" step, add edit-scope (head = the target branch), then run-checks, upload, record (commit + push to `TARGET_BRANCH`), and fail-on-red;
  - delete the separate `validate` job, because the rule checks replace `validate --slot`. Do this only if Task B4's rule check covers the same severity policy (`applyGateSeverityPolicies`); otherwise keep the `validate` job and note it in the commit.
- [ ] **Step 5: Run** — `GOWORK=off go test ./... -count=1`, then render and actionlint as CI does (`METHODASSETS_RENDER_DIR=$(mktemp -d) go test -run TestDumpRenderedWorkflowsForLint ./... && actionlint $METHODASSETS_RENDER_DIR/*.yml`). PASS. regress-check.
- [ ] **Step 6: Commit** — `method-assets: jobs run the task spec — model, allowlist, edit-scope gate, check gate, recorded check run`.

---

# Part B — archistrator server

All Part B work happens in `/Users/davidmarne/mixofrealitystudio/archistrator-ats`. Build and test with the workspace (no `GOWORK=off`) so Part A is visible.

## Task B1: Schema — CheckRun and the taskSpec view

**Files:**
- Modify: `.aiarch/state/project.json` (+ optional top-level `moduleDir`, set to `"server"`; add it to the project schema and the `Project` struct):
  - `.serviceContracts.projectStateAccess.$defs`: `TestRun` → `CheckRun`, `ScenarioResult` → `CheckResult`;
  - `.serviceContracts.deliveryManager.$defs` and the duplicate `.serviceContracts.mcpClient.$defs`: `ProjectViewKind` + `taskSpec`, `ProjectViewQuery.taskId` + `revision`, `ProjectView.taskSpec`, new `DeliveryTaskSpecView`; `DeliveryTestRunView` stays as the testing panel's view, fed from CheckRun;
  - data: `.testingState.testRuns` → `.testingState.checkRuns`.
- Regenerate: `cd server && make gen` (contract.gen.go files, fakes, handlers, MCP tools, `api/openapi.yaml`)
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go:4881` (`TestingState.TestRuns` → `CheckRuns []CheckRun`), `:1995` (`PhaseArtifactPayload.TestRun` → `CheckRun *CheckRun`), `ApplyPhaseArtifactPayload` (:2180, replace by ID)

**Interfaces:**
- Produces (Go, generated from the schema):

```go
type CheckRun struct {
	ID        string        `json:"id"`
	Activity  string        `json:"activity"`
	Task      string        `json:"task"`
	Revision  string        `json:"revision"`
	SpecHash  string        `json:"specHash"`
	StartedAt *time.Time    `json:"startedAt,omitempty"`
	EndedAt   *time.Time    `json:"endedAt,omitempty"`
	Artifact  string        `json:"artifact"`
	Results   []CheckResult `json:"results"`
}
type CheckResult struct {
	CheckID    string `json:"checkId"`
	Kind       string `json:"kind"`   // rule | command | scenario
	Status     string `json:"status"` // pass | fail | skip | timeout
	DurationMs int    `json:"durationMs"`
	OutputRef  string `json:"outputRef,omitempty"`
	TraceRef   string `json:"traceRef,omitempty"`
	VideoRef   string `json:"videoRef,omitempty"`
}
```

- `DeliveryTaskSpecView` (manager view, generated) mirrors `projectstate.TaskSpec` (B2) field for field, plus `checkRun *DeliveryCheckRunView` (same fields as CheckRun) and `specChanged bool`.
- Scenario check ids: `scenario:<contractKey>:<scenarioId>`. Rule: `rule:<scope>`, where scope is a lowercased artifact kind or `tp`/`project`. Command: `command:<name>`.

- [ ] **Step 1: Edit the schema JSON.** Follow the `testRun` precedent (commit `0368439c`) for the shape of `$defs`, `x-enum-varnames` and `required`. Edit both copies of the delivery defs. Remove `TestRun`/`ScenarioResult` from `projectStateAccess.$defs`.
- [ ] **Step 2: Conform the data:** `jq '.testingState.checkRuns = [] | del(.testingState.testRuns)'`. Existing runs are dropped (B19, pre-prod; the testing gate reruns on next dispatch). Write it back with the repo's canonical formatting: use the projectstate encoder via a one-off `go run` of `EncodeProjectJSON`, not raw jq output, so the diff is canonical.
- [ ] **Step 3: Regenerate:** `cd server && make gen`. The build will break at every `TestRun` use. That is expected, and B5/B6 fix them.
- [ ] **Step 4: Fix the compile** in `projectstateaccess.go` (TestingState, PhaseArtifactPayload, ApplyPhaseArtifactPayload replace-by-ID on `CheckRuns`). Then, with `go build ./... 2>&1 | head -50`, temporarily list the remaining breaks. They are owned by B4 (cmd/aiarch-state-mcp) and B5 (delivery, artifact). Do not commit a non-building tree: B1–B4 commit together at the B5 commit.

## Task B2: `projectstate.TaskSpecFor`

**Files:**
- Create: `server/internal/resourceaccess/projectstate/ruleattribution.go` (move `ruleSlotAttributionPrefixes`, `attributeRule`, `ruleAttribution` consts from `cmd/aiarch-state-mcp/staleness.go:161-227`, exported as `RuleAttribution`, `AttributeRule`, `RulePrefixesFor(kind ArtifactKind) []string`)
- Create: `server/internal/resourceaccess/projectstate/pricing.go` (move `modelPrice`, `apiPricing`, `priceFamily`, `defaultMTokInPerDay/OutPerDay`, `rateForSpec` from `deliverymanager.go:12230-12333`; export `RateForSpec`, `DefaultRateSpec(class)`; `defaultModelForClass` reads `methodassets.ModelForAgent(class)` and `roleModel` is deleted)
- Create: `server/internal/resourceaccess/projectstate/taskspec.go`, `taskspec_test.go`
- Modify: `server/cmd/aiarch-state-mcp/staleness.go` (use the moved symbols), `server/internal/manager/delivery/deliverymanager.go` (use `projectstate.RateForSpec`/`DefaultRateSpec`)
- Modify: `server/internal/arch_test.go` encapsulation allowlist if `TestGeneratedOnlyPublic` flags the new exports (regenerate with `ENCAP_DUMP=1`)

**Interfaces:**
- Consumes: `methodassets.MethodSpecFor` (A3), `ComponentScenarios` (:10502), `LifecycleKeyFor` (:8697), `componentContractKey` logic (copy the 10-line lookup from `deliverymanager.go:~14378` into projectstate as `ContractKeyOf(p *Project, componentID string) (string, bool)`, and make deliverymanager call it).
- Produces:

```go
type TaskSpec struct {
	Activity      string        `json:"activity"`
	Task          string        `json:"task"`
	Identity      SpecIdentity  `json:"identity"`
	Model         SpecModel     `json:"model"`
	Prompt        SpecPrompt    `json:"prompt"`
	Tools         SpecTools     `json:"tools"`
	EditScope     SpecEditScope `json:"editScope"`
	Checks        []SpecCheck   `json:"checks"`
	Hash          string        `json:"hash"`
}
type SpecIdentity struct{ Kind, Title, Phase, Command, Agent, WorkerClass, ArtifactKind, ExitCriterion, Mode string }
type SpecModel struct {
	ID     string       `json:"id"`
	Source string       `json:"source"` // projectOverride | charter
	Estimate SpecEstimate `json:"estimate"`
}
type SpecEstimate struct{ TokensIn, TokensOut int64; USD float64 }
type SpecPrompt struct {
	Text    string   `json:"text"`
	Tokens  int      `json:"tokens"` // len(text)/4, a display estimate
	Charter string   `json:"charter"`
	Skills  []string `json:"skills"`
}
type SpecTools struct{ MCP, Builtin []string }
type SpecEditScope struct {
	Components []SpecScopeComponent `json:"components"`
	Extra      []string             `json:"extra"`     // repo-relative globs outside any component (go.mod, hooks.go …)
	StateOnly  bool                 `json:"stateOnly"` // only .aiarch/state/** may change
}
type SpecScopeComponent struct{ ID string; Paths []string }
type SpecCheck struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"` // rule | command | scenario
	Title      string          `json:"title"`
	Source     SpecCheckSource `json:"source"`
	TimeoutSec int             `json:"timeoutSec"`
}
type SpecCheckSource struct {
	RulePrefixes []string `json:"rulePrefixes,omitempty"` // empty slice + Rule set = whole document
	RuleSlot     string   `json:"ruleSlot,omitempty"`     // ambient slot for the severity policy; "" = whole document
	Argv         []string `json:"argv,omitempty"`
	Dir          string   `json:"dir,omitempty"` // repo-relative working directory
	Scenario     string   `json:"scenario,omitempty"`
	Contract     string   `json:"contract,omitempty"`
	UseCase      string   `json:"useCase,omitempty"`
}

func TaskSpecFor(p *Project, activityID, taskID string) (TaskSpec, error)
func (s TaskSpec) ComputeHash() string // sha256 hex over canonical JSON of s with Hash=""
```

All fields carry JSON tags; write them in the file even where the block above abbreviates.

**Resolution rules** (implement exactly):

1. Find the activity in the project (its type, variant and componentId, from the same source `QueryActivityView` uses; read `readProjectForActivityView` in deliverymanager first). Then `key := LifecycleKeyFor(type, variant)`.
2. Args: construction-style commands get `[componentID, activityID]`; design commands (mode draft/critique/answer) get none.
3. Call `methodassets.MethodSpecFor(key, taskID, args)` and copy the result into Identity/Prompt/Tools.
4. **Model:** `pa.RateCard[workerClass].ModelID` if the PlanningAssumptions slot is committed and the entry is non-empty → `projectOverride`, else `ms.Model` → `charter`.
5. **Estimate:**
   - `days` = activity effort days, from the derived activity list entry the estimation engine uses (`OptionActivity.EffortDays`, found via the same list `workerClassesOf(*list)` reads at deliverymanager.go:5588);
   - `share` = phase weight / 100 / number of dispatch tasks in that phase;
   - `rate` = `pa.RateCard[class]` or `DefaultRateSpec(class)` with ModelID replaced by the resolved model;
   - TokensIn = `days*share*rate.MegatokensInPerDay*1e6`, TokensOut likewise, USD from `apiPricing`.
   - If any input is missing, the estimate is zero.
6. **Edit scope** by identity:
   - **Mode draft/critique/answer**, or phase `requirements`/`detailed_design`/`test_plan`, or kind review → `StateOnly: true`.
   - **Service construction:** for each contract of the component (the System `contractKey` plus every `.serviceContracts` facet whose `component` equals it, matching `gateContracts`), take `goPackage` → `<module>/<goPackage>/**`, where `<module>` is `server` if the project's repo layout has `server/go.mod` (decided by `ProjectModuleDir(p)`, which returns the project's optional top-level `moduleDir` field; empty means repo root, the scaffold default. B1 adds `moduleDir` to the project schema and the `Project` struct, and sets it to `"server"` in our own data).
     - Extra: `<module>/go.mod`, `<module>/go.sum`.
   - **Service integration:** construction's scope plus `<module>/cmd/server/hooks.go`.
   - **Frontend construction/integration:** `webApp/src/**`. Integration adds `uitests/generated/<contract>/hooks.ts` for each contract, and `webApp/package.json`, `webApp/package-lock.json`.
   - **Other variants:** the component's goPackage paths if it has any, else `StateOnly`.
   - `.aiarch/state/**` is always permitted (written through state tools), and the gate allows it implicitly; it is not listed.
7. **Checks** by identity:
   - **Draft/critique/answer:** one rule check `rule:<kind>`, `RuleSlot: ArtifactKind`, `RulePrefixes: RulePrefixesFor(kind)`, title "<Kind> rules (<prefixes>)". Timeout 120.
   - **Requirements/detailed_design (state tasks):** `rule:project`, whole document, title "Method rules (whole project)". Timeout 120.
   - **Test plan:** `rule:tp`, `RulePrefixes: ["TP-"]`, title "Test plan rules (TP-*)". Timeout 120.
   - **Service construction:** for package dir `<module>/<goPackage>`:
     - `command:gofmt` — argv `["sh","-c","test -z \"$(gofmt -l <goPackage>)\""]`, dir `<module>`
     - `command:build` — `["go","build","./<goPackage>/..."]`
     - `command:vet` — `["go","vet","./<goPackage>/..."]`
     - `command:lint` — `["golangci-lint","run","./<goPackage>/..."]`
     - `command:arch` — `["go","test","-count=1","-run","^(TestFileLayout|TestGeneratedOnlyPublic)$","./internal/"]`
     - Env `GOWORK=off` is applied by the runner to every Go command. Timeouts: 300, except lint 600.
   - **Service integration:** construction's commands, plus `command:gen-tests-check` (`["make","gen-tests-check"]`, dir `<module>`), plus one scenario check per scenario in `ComponentScenarios(p, contract)` for each contract: id `scenario:<contract>:<scenario.ID>`, title `scenario.Title`, Source{Scenario, Contract, UseCase}, timeout 300.
   - **Frontend construction:** `command:typecheck` (`["npm","run","typecheck"]`, dir `webApp`), `command:lint` (`["npx","eslint","src"]`, dir `webApp`).
   - **Frontend integration:** those, plus gen-tests-check (repo root), plus scenario checks per contract.
   - **Review kind with no command:** no checks.
   - **Other variants:** `rule:project` only.
   - Sort checks by kind order (rule, command, scenario), then id.
8. `Hash = ComputeHash()` (sha256 over `json.Marshal` of the struct with Hash cleared; field order is struct order, and slices are pre-sorted).

- [ ] **Step 1: Write failing tests** (`taskspec_test.go`). Use the repo's own project fixture: decode `../../../../.aiarch/state/project.json` with `DecodeProjectJSON`, the way other projectstate tests load it (grep for an existing loader first and reuse it).

```go
func TestTaskSpecForServiceIntegration(t *testing.T) {
	p := ownProject(t)
	s, err := TaskSpecFor(p, "C-agentic-job-access", "integration")
	if err != nil {
		t.Fatal(err)
	}
	if s.Identity.Command != "service-integration" || s.Model.ID == "" || s.Prompt.Text == "" {
		t.Fatalf("identity/model/prompt: %+v", s.Identity)
	}
	if s.EditScope.StateOnly || len(s.EditScope.Components) != 1 ||
		!slices.Contains(s.EditScope.Components[0].Paths, "server/internal/resourceaccess/agenticjob/**") {
		t.Fatalf("edit scope: %+v", s.EditScope)
	}
	if !slices.Contains(s.EditScope.Extra, "server/cmd/server/hooks.go") || !slices.Contains(s.EditScope.Extra, "server/go.mod") {
		t.Fatalf("integration extras: %v", s.EditScope.Extra)
	}
	var scen int
	for _, c := range s.Checks {
		if c.Kind == "scenario" {
			scen++
			if !strings.HasPrefix(c.ID, "scenario:agenticJobAccess:") {
				t.Errorf("scenario id %q", c.ID)
			}
		}
	}
	want, _ := ComponentScenarios(p, "agenticJobAccess")
	if scen != len(want) || scen == 0 {
		t.Fatalf("scenario checks %d, projected %d", scen, len(want))
	}
}

func TestTaskSpecForDesignDraftIsStateOnlyWithSlotRules(t *testing.T) {
	p := ownProject(t)
	act := firstActivityOfType(t, p, "requirements") // the design prefix activity
	s, err := TaskSpecFor(p, act, "glossaryDraft")
	if err != nil {
		t.Fatal(err)
	}
	if !s.EditScope.StateOnly || len(s.Checks) != 1 || s.Checks[0].ID != "rule:glossary" ||
		!slices.Equal(s.Checks[0].Source.RulePrefixes, []string{"GLOSS-"}) {
		t.Fatalf("design spec: %+v %+v", s.EditScope, s.Checks)
	}
}

func TestTaskSpecForHumanReviewHasNoPromptOrChecks(t *testing.T) {
	s, err := TaskSpecFor(ownProject(t), "C-agentic-job-access", "codeReview")
	if err != nil {
		t.Fatal(err)
	}
	if s.Prompt.Text != "" || len(s.Checks) != 0 || len(s.Tools.MCP) != 0 || !s.EditScope.StateOnly {
		t.Fatalf("review spec: %+v", s)
	}
}

func TestTaskSpecHashIsStableAndSensitive(t *testing.T) {
	p := ownProject(t)
	a, _ := TaskSpecFor(p, "C-agentic-job-access", "construction")
	b, _ := TaskSpecFor(p, "C-agentic-job-access", "construction")
	if a.Hash == "" || a.Hash != b.Hash {
		t.Fatal("hash unstable")
	}
	b.Model.ID = "haiku"
	if b.ComputeHash() == a.Hash {
		t.Fatal("hash ignores the model")
	}
}

func TestTaskSpecModelOverride(t *testing.T) {
	p := ownProject(t)
	setRateCardModel(t, p, "junior-developer", "haiku") // mutate the decoded PlanningAssumptions model
	s, _ := TaskSpecFor(p, "C-agentic-job-access", "construction")
	if s.Model.ID != "haiku" || s.Model.Source != "projectOverride" {
		t.Fatalf("model = %+v", s.Model)
	}
}

func TestTaskSpecForEveryActivityTask(t *testing.T) {
	p := ownProject(t)
	for _, a := range allActivities(p) { // helper over the activity source used in step 1 of resolution
		l, _ := methodassets.LifecycleFor(LifecycleKeyFor(a.Type, a.Variant))
		for _, task := range l.Tasks {
			if _, err := TaskSpecFor(p, a.ID, task.ID); err != nil {
				t.Errorf("%s/%s: %v", a.ID, task.ID, err)
			}
		}
	}
}

func TestDefaultModelFollowsTheCharter(t *testing.T) {
	for _, class := range []string{"system-architect", "junior-developer", "project-manager"} {
		want, _ := methodassets.ModelForAgent(class)
		if got := DefaultRateSpec(class).ModelID; got != want {
			t.Errorf("%s: %q, charter %q", class, got, want)
		}
	}
}
```

Write `ownProject`, `firstActivityOfType`, `setRateCardModel` and `allActivities` as small helpers in the test file against the real types. Read the activity source first, and do not invent fields.

- [ ] **Step 2: Run** — `cd server && go test ./internal/resourceaccess/projectstate/ -run 'TaskSpec|DefaultModel' -count=1` → FAIL.
- [ ] **Step 3: Implement** `ruleattribution.go` and `pricing.go` (pure moves plus exports), then `taskspec.go` per the resolution rules. Update staleness.go and deliverymanager.go to call the moved symbols, and delete the originals.
- [ ] **Step 4: Run** the package tests, `go build ./...` (fine to still fail only on B1's TestRun breaks), and `go test ./cmd/aiarch-state-mcp/ -run Stale -count=1`. PASS. regress-check on the packages touched.
- [ ] **Step 5: Commit** at the B5 commit (the tree builds only then). Stage with explicit paths.

## Task B3: Check runner

**Files:**
- Create: `server/cmd/aiarch-state-mcp/checks.go`, `checks_test.go`

**Interfaces:**
- Consumes: `projectstate.TaskSpec`/`SpecCheck` (B2); the `validate` logic (`validate.go:56`), refactored into `func validateFindings(root string, ambient projectstate.ArtifactKind, hasAmbient bool) ([]methodcheck.Finding, error)` that both `runValidate` and the runner call.
- Produces:

```go
type CheckResult = projectstate.CheckResult
type checkRunner struct {
	root    string                          // repo root (the checkout)
	outDir  string                          // results dir; logs at outDir/checks/<sanitized id>.log
	exec    func(ctx context.Context, dir string, env []string, argv ...string) (out []byte, err error)
	now     func() time.Time
}
// Run runs the given checks; scenario checks are batched per contract into one process.
func (r checkRunner) Run(ctx context.Context, checks []projectstate.SpecCheck) []CheckResult
func failedOnly(prev []CheckResult, all []projectstate.SpecCheck) []projectstate.SpecCheck
```

**Behaviour:**
- **rule:** call `validateFindings(root, slot, slot!="")`, apply `applyGateSeverityPolicies` exactly as `runValidate` does, and keep only findings matching `RulePrefixes` (none means all). The check fails iff any Error finding remains. The log lists every kept finding as `SEV RULE-ID message (section)`.
- **command:** `exec.CommandContext` with a timeout of `TimeoutSec`, in `root/Dir`. Env is the process env plus `GOWORK=off` when `argv[0]=="go"` or `argv[0]=="make"`. Pass on exit 0, fail on nonzero, and `timeout` when the deadline is exceeded. The log is the combined output.
- **scenario:** group checks by Contract.
  - **Go contract** (one whose goPackage is non-empty): `go test -p 1 -count=1 ./<goPackage>/ -run '^TestScenario(_[A-Za-z0-9]+)?_(<ident1>|<ident2>…)$' -json`, with `AIARCH_SCENARIO_TIMEOUT=<TimeoutSec>s` and an overall context timeout of `sum(TimeoutSec)+120s`. `ident(id)` is the testgen ident function, imported from app-generator testgen if it is exported, else replicated with a test that pins it against two generated test names in `access_scenarios.gen_test.go`.
  - **Web contract:** `npx playwright test -c uitests/generated/<contract>/playwright.config.gen.ts --grep '<id1>|<id2>'`, with `AIARCH_SCENARIO_TIMEOUT_MS`.
  - Then read `<module>/test-results/<contract>/results.json` (Go) or `uitests/test-results/<contract>/results.json` (web, then copied to `test-results/<contract>/` as the frontend-integration step used to do). Map each selected scenario to its status; a selected scenario absent from the index is `fail` with log "no verdict recorded". Copy the video and trace refs. The per-check log is the scenario's stdout+stderr from the index, or for Go the `goTestOutputFor` slice of the `-json` stream (port `webApp` `testRunPanelLogic.goTestOutputFor` logic: filter `Test` == the scenario's test name).
- Results are returned in check order. `outDir/results.json` is written only by the subcommand (B4), not by `Run`.

- [ ] **Step 1: Failing tests** with a fake `exec`:

```go
func TestRunCommandCheckPassFailTimeout(t *testing.T) {
	r := checkRunner{root: t.TempDir(), outDir: t.TempDir(), now: time.Now,
		exec: func(ctx context.Context, dir string, env []string, argv ...string) ([]byte, error) {
			switch argv[0] {
			case "ok":
				return []byte("fine"), nil
			case "bad":
				return []byte("boom"), errors.New("exit status 1")
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		}}
	res := r.Run(context.Background(), []projectstate.SpecCheck{
		{ID: "command:ok", Kind: "command", Source: projectstate.SpecCheckSource{Argv: []string{"ok"}}, TimeoutSec: 5},
		{ID: "command:bad", Kind: "command", Source: projectstate.SpecCheckSource{Argv: []string{"bad"}}, TimeoutSec: 5},
		{ID: "command:hang", Kind: "command", Source: projectstate.SpecCheckSource{Argv: []string{"hang"}}, TimeoutSec: 1},
	})
	got := []string{res[0].Status, res[1].Status, res[2].Status}
	if !slices.Equal(got, []string{"pass", "fail", "timeout"}) {
		t.Fatalf("statuses %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "command_bad.log")); !strings.Contains(string(b), "boom") {
		t.Fatal("log not written")
	}
}

func TestRuleCheckFiltersByPrefixAndFailsOnError(t *testing.T) {
	root := repoWithProject(t, glossaryWithAnErrorFinding(t)) // write a project.json the GLOSS rules reject
	r := checkRunner{root: root, outDir: t.TempDir(), now: time.Now}
	res := r.Run(context.Background(), []projectstate.SpecCheck{
		{ID: "rule:glossary", Kind: "rule", Source: projectstate.SpecCheckSource{RuleSlot: "Glossary", RulePrefixes: []string{"GLOSS-"}}, TimeoutSec: 60},
		{ID: "rule:volatilities", Kind: "rule", Source: projectstate.SpecCheckSource{RuleSlot: "Volatilities", RulePrefixes: []string{"VOL-"}}, TimeoutSec: 60},
	})
	if res[0].Status != "fail" || res[1].Status != "pass" {
		t.Fatalf("got %s %s", res[0].Status, res[1].Status)
	}
}

func TestScenarioCheckMissingVerdictFails(t *testing.T) { /* fake exec writes results.json with only scenario A; checks A and B → A pass, B fail "no verdict recorded" */ }

func TestFailedOnlySelectsFailuresAndTimeouts(t *testing.T) {
	all := []projectstate.SpecCheck{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	prev := []CheckResult{{CheckID: "a", Status: "pass"}, {CheckID: "b", Status: "fail"}, {CheckID: "c", Status: "timeout"}}
	got := failedOnly(prev, all)
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "c" {
		t.Fatalf("%v", got)
	}
}
```

Build `glossaryWithAnErrorFinding` by starting from the own project fixture and blanking a required glossary term field that a GLOSS-* Error rule checks. Find one by reading `framework-go/methodcheck/rules.go` GLOSS rules first.

- [ ] **Step 2: Run** — `go test ./cmd/aiarch-state-mcp/ -run 'RunCommandCheck|RuleCheck|ScenarioCheck|FailedOnly' -count=1` → FAIL.
- [ ] **Step 3: Implement** `checks.go` and the `validateFindings` refactor.
- [ ] **Step 4: Run** — PASS. regress-check `cmd/aiarch-state-mcp`.
- [ ] **Step 5: Commit** at the B5 commit.

## Task B4: Subcommands and the `run_checks` verb

**Files:**
- Create: `server/cmd/aiarch-state-mcp/taskspeccmd.go`, `runchecks.go`, `editscope.go`, `recordcheckrun.go` (+ `_test.go` for each)
- Delete: `server/cmd/aiarch-state-mcp/recordtestrun.go`, `recordtestrun_test.go`
- Modify: `main.go:26-70` (cases), `tools.go` (verb), `session.go` (`AIARCH_TASK_ID` → `Session.TaskID`), `constructverbs.go:177-187` (drop the TestRun refusal path), `activityrecords.go` (TestRun → CheckRun reads)

**Interfaces** (CLI contract as in A7):
- `task-spec --activity A --task T [--root .] --out F`: read `root/.aiarch/state/project.json`, `TaskSpecFor`, write indented JSON to F, print `Hash` + newline. Exit 2 on error.
- `run-checks [--gate] --spec F --out D [--failed-only]`: load the spec; with `--gate`, delete `D` first and run all; without it (agent path, used by the tool), run all or failed-only from `D/results.json`. Write `D/results.json` as `[]CheckResult`. Exit 0 if all pass, 1 otherwise.
- `edit-scope --spec F --base B --head H`: `git diff --name-only B H` in cwd. A path is allowed iff it is under `.aiarch/state/`, or (if not StateOnly) it matches a component path glob or an `Extra` entry. Glob semantics: `**` is any suffix, otherwise `path.Match`. Print violations; exit 1 if any.
- `record-check-run --spec F --results R --run-id ID --artifact REF --revision REV`: build `CheckRun{ID, Activity, Task, Revision, SpecHash: spec.Hash, StartedAt (results file mtime minus the sum of durations; acceptable), EndedAt now, Artifact, Results}` and write it with `applyEvidenceMutation` (new in `constructverbs.go`: mutate → `EncodeProjectJSON` → `DecodeProjectJSON` read-back → write; **no** methodcheck). `--revision` falls back to `$AIARCH_REVISION`, and one is required.
- MCP verb `run_checks`, modes all, input `{failedOnly bool}`:
  - Resolve the spec with `TaskSpecFor(project, s.ActivityID, s.TaskID)` from the session's state root.
  - Run with outDir `<stateRoot>/.aiarch/tmp/checks` (gitignored; add it to `.gitignore` in our repo and to the scaffold `.gitignore` in method-assets A7 if one exists).
  - Return text: `"N passed, M failed."` plus, per failure, `"✗ <title> [<status>]\n<last 40 lines of log>"` plus `"Full logs: <dir>"`. Never list passing checks.
  - If `s.ActivityID` or `s.TaskID` is empty, return an error naming the missing env var.

- [ ] **Step 1: Failing tests**:
  - `task-spec` round-trip: printed hash == the `ComputeHash` of the written file.
  - `edit-scope`: build a temp git repo with commits touching an allowed path, `.aiarch/state/project.json`, and a disallowed path → exit 1 listing only the disallowed one; StateOnly + a code path → violation.
  - `record-check-run`: with a project whose glossary has an Error finding (reuse the B3 fixture) the write **succeeds**, and the CheckRun is present after decode.
  - `run_checks` verb: an in-process server with a fake runner (inject `checkRunner.exec`) returns only failures in its text and accepts `failedOnly`.
  - main dispatch: `record-test-run` is no longer a subcommand (returns handled=false).
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement.** Port the existing `recordtestrun_test.go` cases that still apply (activity resolution, revision required) to `recordcheckrun_test.go` before deleting the old file. Do not lose coverage.
- [ ] **Step 4: Run** — `go test ./cmd/aiarch-state-mcp/ -count=1` → PASS. regress-check.
- [ ] **Step 5: Commit** at the B5 commit.

## Task B5: Delivery manager — dispatch hash, taskSpec view, testing gate on CheckRun

**Files:**
- Modify: `server/internal/manager/delivery/deliverymanager.go`:
  - `pipelineSpec` :12364 (+`TaskID`, `SpecHash`)
  - `dispatchInputsFor` :12478 (+`task_id`, `spec_hash`)
  - view switch :11566 (+`case ProjectViewTaskSpec`)
  - new `queryTaskSpecView`
  - `queryTestRunView` :14114 / `testRunView` :14244 / `selectTestRun` :14327 / `selectContractRun` :14387 read CheckRun
- Modify: `server/internal/manager/delivery/deliveryactivity.go`:
  - `submitPipeline` :4625 and its callers (compute the spec, set TaskID/SpecHash)
  - `submitDesignJob` :653 (+`activity_id`, `task_id`, `spec_hash`)
  - `readTestingGateEvidence` :6400 / `joinTestRun` read the integration task's CheckRun
- Modify: `server/internal/resourceaccess/artifact/artifactaccess.go` if `GetTestRunIndex` keys on TestRun (it should take the artifact ref and contract; adapt the signature only if forced)
- Modify: `server/internal/resourceaccess/agenticjob/agenticjobaccess.go` local-rail input keys (+`task_id`, `spec_hash`; full local sequence in B6)

**Interfaces:**
- Consumes: `projectstate.TaskSpecFor`, `CheckRun`.
- Produces:
  - `ProjectView.TaskSpec *DeliveryTaskSpecView` for `{kind: taskSpec, projectId, activityId, taskId, revision?}`.
  - `specChanged = checkRun != nil && checkRun.SpecHash != spec.Hash`.
  - The CheckRun is selected by (activity, task, revision); with no revision given, it is the latest by EndedAt.
  - Read order for the project: `activity/<id>` branch, then main (the `readTestingGateProject` pattern). For design-lifecycle activities, read the branch of the revision's attempt (`judgedSubject(ws,t)` holds the attempt; use its recorded target branch), then main.
- `testRunView(contract)` builds the existing `DeliveryTestRunView` from the integration CheckRun's results with `CheckID` prefix `scenario:<contract>:`. The scenario id is the suffix. `run.id` is `CheckRun.ID`, so `TestRunPanel` keeps working unchanged.

- [ ] **Step 1: Failing tests** (manager hooks tests in `manager_hooks_test.go`, following the existing `testRun` view test):
  - The taskSpec view for a never-run task returns a spec with no checkRun.
  - With a CheckRun at a different specHash → `specChanged` is true.
  - `dispatchInputsFor` includes `task_id` and a 64-hex `spec_hash` equal to `TaskSpecFor(...).Hash`.
  - The testing gate holds when the integration CheckRun is missing a projected scenario, and passes when all `scenario:<contract>:*` results pass at the judged revision. Port each existing `testingGateHold` case to CheckRun inputs; do not delete cases.
- [ ] **Step 2: Run** — `go test ./internal/manager/delivery/ -count=1 -run 'TaskSpec|DispatchInputs|TestingGate'` → FAIL.
- [ ] **Step 3: Implement.**
  - Compute the spec in the activity (a deterministic read; it must run in an activity, not workflow code, because it reads project state).
  - `submitPipeline` receives `TaskID`/`SpecHash` through `pipelineSpec`.
  - **Temporal determinism:** new fields on activity inputs are additive. Run the replay tests (`go test ./internal/... -run Replay`). If a recorded history breaks, re-record per the DCT Plan 2 Task 4b procedure, and record in the commit that workflows must be drained before deploy.
- [ ] **Step 4: Fix every remaining TestRun compile break** (`go build ./... && go vet ./...`), then run `go test -p 1 ./... -count=1` (stack up) → regress-check against `server-fail.txt`/`server-pkgfail.txt`. Then `make lint` and every `make gen-*-check`.
- [ ] **Step 5: Commit B1+B2+B3+B4+B5** with explicit paths (list them with `git status --porcelain`; review each):

```bash
git add .aiarch/state/project.json server/api/openapi.yaml server/internal/... server/cmd/aiarch-state-mcp/... .gitignore
git commit -m "feat(ats): computed agent task spec; run_checks; CheckRun replaces TestRun; dispatch carries the spec hash

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

## Task B6: Local rail runs the same sequence

**Files:**
- Modify: `server/internal/resourceaccess/agenticjob/agenticjobaccess.go`:
  - `localDispatchPlan` :1453 (+`taskID`, `specHash`, `phase`)
  - `constructDispatchPlan` :1519, `designDispatchPlan` :1576
  - `dispatch` :1642 (pre-steps)
  - `claudeCommand` :3462 (+`--model`, `--tools`)
  - `writeClaudeSettings` (drop disallowed)
  - `awaitCompletion` :2727 (gates before `removeWorktree`)
  - delete `disallowedBuiltinTools` :3489
- Test: `server/internal/resourceaccess/agenticjob/access_hooks_test.go` or the package's existing local-rail tests (find with `grep -n constructDispatchPlan *_test.go`)

**Interfaces:**
- Pre-steps (after `seatPromptSurface`, before `cmd.Start`):
  1. `stateMCPBin task-spec --activity A --task T --root workDir --out parentDir/spec.json`; mismatch with `plan.specHash` → fail the run with `outcome: failed` and reason "task spec drifted".
  2. Integration phase: `make gen-tests` in the module dir.
- Argv: `claudeCommand(ctx, bin, model, builtin []string)` adds `"--model", model, "--tools", strings.Join(builtin, ",")`. `designModelFromEnv` and `ARCHISTRATOR_LOCAL_EXEC_DESIGN_MODEL` are deleted; the spec's model wins.
- Gates (in `awaitCompletion`, after `cmd.Wait`, before `removeWorktree`):
  1. `edit-scope --base beforeSHA --head <branch tip>`;
  2. `run-checks --gate --out workDir/check-results`;
  3. `record-check-run` with run id `local-<UTC ts>` and artifact `.aiarch/test-results/<run>` (copy `check-results` and `test-results` there);
  4. commit `.aiarch/state/project.json` and the copied results on the activity branch.
  - The run outcome is failed if the edit scope fails or any check is non-pass; the reason is the first failing check id.
  - These run even when the agent exited nonzero, matching `always()` in the cloud template.

- [ ] **Step 1: Failing tests:**
  - `constructDispatchPlan` carries model, builtin tools and specHash from a given spec;
  - the `claudeCommand` argv contains `--model sonnet --tools Bash,Edit,...` and no `--disallowedTools`;
  - with a fake `stateMCPBin` script (write a shell script to a temp dir that echoes a fixed hash and writes fixtures), a hash mismatch yields a failed outcome before claude starts;
  - a red `run-checks` (script exits 1) yields a failed outcome, and the record subcommand was still invoked (the script appends its argv to a file and the test asserts on it).
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the package tests and regress-check; `make lint`.
- [ ] **Step 5: Commit** — `feat(ats): the local rail resolves, runs and gates the task spec like the cloud job`.

---

# Part C — webApp

Work in `/Users/davidmarne/mixofrealitystudio/archistrator-ats/webApp`. Unit tests are not allowed (`eslint.config.js` refuses `*.test.ts`), so verification is `npm run typecheck`, `npm run lint`, `npm test` (script tests) and the Playwright/browser pass in D1. Keep logic in pure `*Logic.ts` modules so D1 exercises it through the UI.

## Task C1: Contracts, mapper, hook, fixtures

**Files:**
- Regenerate: `npm run gen:api && npm run gen:ops` (after B5's `make gen-client`)
- Modify: `src/contracts/types.ts` (+`TaskSpecView`, `CheckRunView`, `CheckResultView`, `SpecCheckView`), `src/contracts/wire.ts` (+`mapTaskSpec`)
- Create: `src/hooks/useTaskSpec.ts`
- Modify: `src/api/fixtureOps.ts` (`VIEW_OP` answer for `taskSpec`) + preview fixtures

**Interfaces:**
- Produces:

```ts
export interface SpecCheckView { id: string; kind: 'rule' | 'command' | 'scenario'; title: string;
  source: { rulePrefixes?: string[]; ruleSlot?: string; argv?: string[]; dir?: string; scenario?: string; contract?: string; useCase?: string };
  timeoutSec: number }
export interface CheckResultView { checkId: string; kind: string; status: 'pass' | 'fail' | 'skip' | 'timeout';
  durationMs: number; outputRef?: string; traceRef?: string; videoRef?: string }
export interface CheckRunView { id: string; activity: string; task: string; revision: string; specHash: string;
  startedAt?: string; endedAt?: string; artifact: string; results: CheckResultView[] }
export interface TaskSpecView {
  activity: string; task: string;
  identity: { kind: string; title: string; phase: string; command: string; agent: string; workerClass: string; artifactKind: string; exitCriterion: string; mode: string };
  model: { id: string; source: 'projectOverride' | 'charter'; estimate: { tokensIn: number; tokensOut: number; usd: number } };
  prompt: { text: string; tokens: number; charter: string; skills: string[] };
  tools: { mcp: string[]; builtin: string[] };
  editScope: { components: { id: string; paths: string[] }[]; extra: string[]; stateOnly: boolean };
  checks: SpecCheckView[]; hash: string;
  checkRun?: CheckRunView; specChanged: boolean;
}
export function useTaskSpec(projectId: string, activityId: string, taskId: string | undefined, revision: number | undefined): UseQueryResult<TaskSpecView>
```

`useTaskSpec` copies the `useTestRun` pattern exactly: `queryProjectView(ops, { kind: 'taskSpec', projectId, activityId, taskId, revision })`, throws `ApiError(200,'empty_body',...)` when `taskSpec` is absent, key `['taskSpec', projectId, activityId, taskId, revision]`, enabled when all ids are non-empty, `staleTime: 60_000`.

- [ ] **Step 1:** Regenerate; `npm run typecheck` → errors only where TestRun names changed. Fix `wire.ts`/`types.ts`: the test-run panel types stay, fed by the server's adapted view.
- [ ] **Step 2:** Add the types, `mapTaskSpec`, hook and fixture answer.
- [ ] **Step 3:** `npm run typecheck && npm run lint && npm test` → clean relative to the baseline.
- [ ] **Step 4: Commit** — `feat(webApp): taskSpec contract, mapper and hook`.

## Task C2: Extract reusable scenario row pieces

**Files:**
- Create: `src/components/construction/renderers/ScenarioRow.tsx`
- Modify: `TestRunPanel.tsx` (import from ScenarioRow; behaviour unchanged), `testRunPanelLogic.ts` (`summarize` counts `timeout`)

**Interfaces:**
- Produces: `export function statusChip(status: string, t: Tokens): ReactNode` (`timeout` → danger tone, label "timeout"); `export function ScenarioDetail(props: { scenario: TestRunScenarioView; fileUrl: (path: string) => string; outputs: Record<string, AsyncArtifactData<string>>; t: Tokens }): ReactNode`; `export function OutputTab(...)` moved verbatim. `ScenarioDetail` takes `fileUrl` instead of `panel`.

- [ ] **Step 1:** Move the three pieces verbatim (TestRunPanel.tsx :37-194) and change `panel.fileUrl` → `fileUrl`. Update TestRunPanel's call sites.
- [ ] **Step 2:** `npm run typecheck && npm run lint` → clean.
- [ ] **Step 3: Commit** — `refactor(webApp): scenario row pieces are reusable outside TestRunPanel`.

## Task C3: TaskSpecPanel

**Files:**
- Create: `src/components/activity/TaskSpecPanel.tsx`, `src/components/activity/taskSpecPanelLogic.ts`
- Modify: `TaskHeader.tsx` (keep the title + RevisionSelect row; the facts box becomes `<TaskSpecPanel>`; keep `data-testid={UI_IDENTIFIERS.Activity.TASK_FACTS}` on the panel root, because the generated spec `execute-a-project-activity-P1.spec.gen.ts:17` asserts it)
- Modify: `DispatchBody.tsx`, `ReviewBody.tsx` (prop `facts: TaskFacts` → `spec: AsyncView<TaskSpecView>`, plus `episodeCostUsd?: number`)
- Modify: `activityViewToGraph.ts` (delete `taskFactsFor` and `TaskFacts`; keep `artifactKindOf` only if another caller remains)
- Modify: `src/components/comments/CommentContext.tsx` (+`taskSpecCheckAnchor(activityId, taskId, checkId)` → `$.testingState.checkRuns[activity=${a}][task=${t}].results[checkId=${c}]`; `testRunScenarioAnchor` → `$.testingState.checkRuns[id=${runId}].results[checkId=scenario:${contract}:${scenarioId}]`, with its call site in TestRunPanel passing the contract)
- Modify: `src/components/activity/activityCopy.ts` (export `ROLE_LABEL`; add copy strings below)

**Interfaces:**
- Consumes: `TaskSpecView`, `statusChip`/`ScenarioDetail` (C2), `RoleAvatar`, `CommentableList`, `useTokens`, GatePanel's collapse pattern (`GatePanel.tsx:115-192`).
- Produces: `export function TaskSpecPanel(props: { spec: AsyncView<TaskSpecView>; episodeCostUsd?: number; activityId: string; outputs: Record<string, AsyncArtifactData<string>>; fileUrl: (path: string) => string }): ReactNode`

`taskSpecPanelLogic.ts` (pure):

```ts
export type CheckRowState = { check: SpecCheckView; result?: CheckResultView; state: 'not-run' | 'pass' | 'fail' | 'skip' | 'timeout' };
export function checkRows(spec: TaskSpecView): CheckRowState[] {
  const byId = new Map((spec.checkRun?.results ?? []).map((r) => [r.checkId, r]));
  return spec.checks.map((check) => {
    const result = byId.get(check.id);
    return { check, result, state: result === undefined ? 'not-run' : result.status };
  });
}
export const KIND_ORDER = ['rule', 'command', 'scenario'] as const;
export const KIND_LABEL: Record<SpecCheckView['kind'], string> = { rule: 'Rules', command: 'Commands', scenario: 'Scenarios' };
export function groupByKind(rows: CheckRowState[]): { kind: SpecCheckView['kind']; rows: CheckRowState[] }[] {
  return KIND_ORDER.map((kind) => ({ kind, rows: rows.filter((r) => r.check.kind === kind) })).filter((g) => g.rows.length > 0);
}
export function formatUsd(n: number): string { return n < 0.01 ? '<$0.01' : `$${n.toFixed(2)}`; }
export function formatTokens(n: number): string { return n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1e3 ? `${Math.round(n / 1e3)}k` : `${n}`; }
export function sourceLabel(c: SpecCheckView): string {
  if (c.kind === 'rule') return c.source.rulePrefixes?.length ? c.source.rulePrefixes.map((p) => `${p}*`).join(' ') : 'all method rules';
  if (c.kind === 'command') return (c.source.argv ?? []).join(' ');
  return `${c.source.contract ?? ''} · use case ${c.source.useCase ?? ''}`;
}
```

**Layout** (MUI + tokens; mirror TaskHeader's frame styling — `bgcolor: t.paperAlt, border: 1.5px solid t.line`):
1. **Header row:**
   - `<RoleAvatar seed={workerClass} size={26}/>` + the ROLE_LABEL text;
   - Fact `COMMAND` `/command`;
   - Fact `MODEL`: a chip with `model.id`, tooltip "from charter" / "project override";
   - Fact `COST`: `est ${formatUsd(estimate.usd)}`, plus ` · actual ${formatUsd(episodeCostUsd)}` when given;
   - Fact `EXIT CRITERION` (wide).
   - For a review with no command: avatar/label only if workerClass is set, plus the exit criterion.
2. **Prompt:** a collapsible (closed by default) titled `PROMPT · ~${formatTokens(prompt.tokens)} tokens` with a `<pre>` in `t.mono` holding the text (`whiteSpace: pre-wrap`, `maxHeight: 480`, scroll). Two nested collapsibles follow: `CHARTER · <agent>` (body) and `SKILLS · n` (chips). Hidden when `prompt.text === ''`.
3. **Tools:** label `TOOLS`; groups "State tools" (mcp) and "Built-ins" (builtin) as small mono chips (the TestRunPanel :384 chip style).
4. **May edit:** label `MAY EDIT`.
   - `stateOnly` → text "Writes through state tools only".
   - Otherwise component chips (`id`). Clicking one toggles a list of its `paths`, followed by `extra` paths.
5. **Checks:** label `CHECKS`, with counts `n passed · m failed · k not run`.
   - When `specChanged`, a warning chip "spec changed since this run".
   - `groupByKind` sections. Each group is a `CommentableList` with `getAnchor` → `taskSpecCheckAnchor`.
   - Each row: `statusChip(state)` (with `not-run` → awaiting tone, label "not run"), the title, `sourceLabel` in muted mono, and the duration if run.
   - A row with a result expands to:
     - scenario kind → `<ScenarioDetail>`, built from the result plus the spec check (id = scenario, title);
     - otherwise → the `outputRef` text from `outputs`, in a `<pre>`.
   - Empty `checks` → muted line "No checks for this task".
6. **Loading:** a skeleton line. **Error:** an inline muted error line. Neither blocks the episode timeline below.

Copy strings (add to `activityCopy.ts`): `SPEC_NO_CHECKS = 'No checks for this task'`, `SPEC_STATE_ONLY = 'Writes through state tools only'`, `SPEC_CHANGED = 'spec changed since this run'`, `SPEC_NOT_RUN = 'not run'`.

- [ ] **Step 1:** Write `taskSpecPanelLogic.ts` and `TaskSpecPanel.tsx` per the layout.
- [ ] **Step 2:** Swap TaskHeader's facts box for the panel, and change the DispatchBody/ReviewBody props. Keep `GeneratingScene`'s `dispatchRoleLine(spec.data?.identity.workerClass, title)`.
- [ ] **Step 3:** Container (`ActivityExperienceContainer.tsx`):
  - call `const taskSpec = useTaskSpec(projectId, activityId, sel?.taskId, sel?.revision)` next to `useEpisodeTimeline` (:325);
  - replace `facts` (:282) with `taskSpec`;
  - `verbsFor`/`taskArtifactFor` (:436-450) read `taskSpec.data?.identity.artifactKind`;
  - outputs for non-scenario check logs: add `useCheckRunOutputs(taskSpec.data)` in `hooks/useTaskSpec.ts`, mirroring `useTestRunOutputs` (it fetches each result's `outputRef` via `fetchTestRunFileText` against `checkRun.id`);
  - `episodeCostUsd = timeline.data?.record.costUsd`.
- [ ] **Step 4:** `npm run typecheck && npm run lint && npm test && npm run build` → clean relative to the baseline. `cd ../server && make gen-lifecycles-check` still clean (lifecycles.gen.ts is untouched).
- [ ] **Step 5: Commit** — `feat(webApp): the task spec panel — model, cost, prompt, tools, edit scope and checks on every agentic task`.

---

# Part D — Verify, founder review, release

## Task D1: Local end-to-end on real state

- [ ] **Step 1:** Boot the stack and app per memory `archistrator-run-app-locally.md`:
  - build the server **with the workspace** (`go build -o $SCRATCH/server-bin ./cmd/server` from `server/`, with no `GOWORK=off`, so the unreleased platform code is used);
  - seed a bare state repo from the worktree's `.aiarch/state/project.json` (top-level `id` set);
  - `nohup ... & disown`;
  - `cd webApp && npm run dev -- --port 5180`.
- [ ] **Step 2:** In Chrome (claude-in-chrome; Playwright MCP was down this session), screenshot each of:
  - a never-run construction task (all checks "not run", prompt collapsed);
  - an integration task (scenario checks listed with use-case links);
  - a design draft task (state-only, one rule check);
  - a human review task (`codeReview`);
  - a task with a red CheckRun and a green CheckRun. To get these, run one real local job with `ARCHISTRATOR_CONSTRUCTION_DRYRUN` off on a small component, or inject a CheckRun into the seeded state for display only. Prefer the real run; if it is not feasible in reasonable time, inject and say so;
  - the spec-changed badge (edit the seeded RateCard model after the run).
- [ ] **Step 3:** Run the generated webClient Playwright spec that asserts `activity-task-facts` → PASS.
- [ ] **Step 4:** Full regress-check across all suites against the baseline files. Any new red blocks D2.

## Task D2: Founder review (STOP)

- [ ] Present to the founder: the local URL, the screenshots, the regress-check result (with any baseline-red tests named), and the six deviations above. **Wait for explicit approval.** Apply requested changes and repeat D1 for the touched screens.

## Task D3: Release (only after D2 approval)

- [ ] **Step 1:** Merge the platform branch to platform main (fast-forward, founder-authored commits), then tag and push. Each tag's minor version goes up by one from the latest in Task 0's recon:
  - `method-assets/v0.11.0`
  - `framework-go-scenariohost/v0.4.0`
  - `framework-go-app-generator/v0.13.0`
  - Bump the pins in `method-assets/scaffold.go:22-33` (AppGeneratorVersion) in a release commit, then tag method-assets after the pin bump.
- [ ] **Step 2:** In the archistrator worktree:
  - bump `server/go.mod` requires to the new tags;
  - `GOWORK=off go mod tidy`;
  - `GOWORK=off go build ./... && GOWORK=off make test-short`;
  - re-seat the workflow templates into `.github/workflows/` (via the scaffold re-seat target used in prior releases: grep the Makefile for `seat`/`scaffold`).
  - Commit.
- [ ] **Step 3:** Message session `archistrator-6b` (ListAgents / SendMessage) before merging to main, then merge `agent-task-spec` into main and push. `release.yml` auto-tags server/webapp.
- [ ] **Step 4:** Deploy notes in `docs/bugs/2026-10-11-agent-task-spec-deploy.md`:
  - drain in-flight construct/design jobs and any `*:activity:*` workflows started before the deploy;
  - the deploy owes the items already listed in memory `archistrator-unified-activity-experience` if they are still open.
- [ ] **Step 5:** Update memory `archistrator-agent-task-spec.md` with the shipped SHAs/tags and open earmarks.
