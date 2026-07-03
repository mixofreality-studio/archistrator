# Per-Phase Construction Commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the fat per-phase construction workflow with one thin generic GitHub action that dispatches ~30 per-`(type,variant,phase)` slash commands, each loading exactly its own context, backed by a single project-state skill.

**Architecture:** The construction pump already opens one PR per activity, dispatches one CI job per lifecycle phase, and gates each phase against `ReviewPolicy`. This plan makes each phase's job run a thin `/command` (computed in Go from the activity's type/variant/phase) instead of a hardcoded inline prompt. All state reading/writing moves into the agent, guided by a new `the-method-project-state` skill; no `jq` pre-extraction remains.

**Tech Stack:** Go 1.23 (`server/`), GitHub Actions (`anthropics/claude-code-action@v1`), Claude Code commands + skills (`.claude/`).

## Global Constraints

- Go builds/tests run with `GOWORK=off` from `server/` (e.g. `GOWORK=off go test ./internal/resourceaccess/projectstate/...`). `gofmt -w .` before committing Go.
- The 5 canonical phase ids are fixed: `requirements`, `detailed_design`, `test_plan`, `construction`, `integration` (`activityconstructionstatus.go:318-333`). Phase slugs kebab-case the id (`detailed_design` → `detailed-design`).
- The command set is defined as the flattening of `ProfileFor` (`activityprofile.go:45-119`) over every `(type, variant)` — never an independent list. It is exactly 30 commands.
- `project.json` (at `.aiarch/state/project.json` in the construction repo) is the single source of truth. Never write a parallel markdown copy of state; markdown is render-on-read only.
- Command intent prose MUST be authored from a dispatched research agent's read of `research/rightingsoftware/OEBPS/xhtml/` (Löwy, *Righting Software*) — not paraphrased from memory.
- The workflow's load-bearing anchors are immutable: input `idempotency_token`, `run-name: aiarch-cp-${{ inputs.idempotency_token }}`, `concurrency.group` on the token, `allowed_bots: archistrator-bot`.
- The review gate (`runPhaseGate` + `ReviewPolicy`) is already implemented — do not modify it.

---

### Task 1: `CommandFor` routing function

**Files:**
- Create: `server/internal/resourceaccess/projectstate/commandfor.go`
- Test: `server/internal/resourceaccess/projectstate/commandfor_test.go`

**Interfaces:**
- Consumes: `ActivityType`, `TestingVariant`, `ActivityMethodPhase`, `ProfileFor` (all in package `projectstate`).
- Produces: `func CommandFor(t ActivityType, v TestingVariant, p ActivityMethodPhase) string` and `func ProfileSlug(t ActivityType, v TestingVariant) string`.

- [ ] **Step 1: Write the failing test**

Create `commandfor_test.go`:

```go
package projectstate

import "testing"

func TestProfileSlug(t *testing.T) {
	cases := []struct {
		t    ActivityType
		v    TestingVariant
		want string
	}{
		{ActivityTypeService, 0, "service"},
		{ActivityTypeFrontend, 0, "frontend"},
		{ActivityTypeDeployment, 0, "deployment"},
		{ActivityTypeDocumentation, 0, "documentation"},
		{ActivityTypeTesting, TestVariantPlan, "testing-plan"},
		{ActivityTypeTesting, TestVariantHarness, "testing-harness"},
		{ActivityTypeTesting, TestVariantPerf, "testing-perf"},
		{ActivityTypeTesting, TestVariantSystemTest, "testing-systemtest"},
		{ActivityTypeTesting, TestVariantQAProcess, "testing-qa"},
	}
	for _, c := range cases {
		if got := ProfileSlug(c.t, c.v); got != c.want {
			t.Errorf("ProfileSlug(%v,%v) = %q, want %q", c.t, c.v, got, c.want)
		}
	}
}

func TestCommandFor(t *testing.T) {
	if got := CommandFor(ActivityTypeService, 0, MethodPhaseDetailedDesign); got != "service-detailed-design" {
		t.Errorf("got %q, want service-detailed-design", got)
	}
	if got := CommandFor(ActivityTypeTesting, TestVariantHarness, MethodPhaseConstruction); got != "testing-harness-construction" {
		t.Errorf("got %q, want testing-harness-construction", got)
	}
}

// TestCommandForTotalOverProfiles asserts CommandFor returns a non-empty,
// well-formed slug for every phase that ProfileFor actually emits — the command
// matrix is exactly the flattening of ProfileFor.
func TestCommandForTotalOverProfiles(t *testing.T) {
	for _, combo := range allProfileCombos() {
		for _, p := range ProfileFor(combo.t, combo.v).PhaseIDs() {
			got := CommandFor(combo.t, combo.v, p)
			if got == "" {
				t.Errorf("CommandFor(%v,%v,%q) empty", combo.t, combo.v, p)
			}
			if want := ProfileSlug(combo.t, combo.v) + "-" + kebabPhase(p); got != want {
				t.Errorf("CommandFor = %q, want %q", got, want)
			}
		}
	}
}

type profileCombo struct {
	t ActivityType
	v TestingVariant
}

// allProfileCombos enumerates every distinct (type, variant) profile in the domain.
func allProfileCombos() []profileCombo {
	return []profileCombo{
		{ActivityTypeService, 0},
		{ActivityTypeFrontend, 0},
		{ActivityTypeDeployment, 0},
		{ActivityTypeDocumentation, 0},
		{ActivityTypeTesting, TestVariantPlan},
		{ActivityTypeTesting, TestVariantHarness},
		{ActivityTypeTesting, TestVariantPerf},
		{ActivityTypeTesting, TestVariantSystemTest},
		{ActivityTypeTesting, TestVariantQAProcess},
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'CommandFor|ProfileSlug' -v`
Expected: FAIL — `undefined: ProfileSlug`, `undefined: CommandFor`, `undefined: kebabPhase`.

- [ ] **Step 3: Write minimal implementation**

Create `commandfor.go`:

```go
package projectstate

import "strings"

// ProfileSlug is the .claude/commands filename stem for an activity profile.
// For testing it encodes the variant (testing-plan/harness/perf/systemtest/qa);
// all other types map 1:1 to their wire name.
func ProfileSlug(t ActivityType, v TestingVariant) string {
	switch t {
	case ActivityTypeFrontend:
		return "frontend"
	case ActivityTypeDeployment:
		return "deployment"
	case ActivityTypeDocumentation:
		return "documentation"
	case ActivityTypeTesting:
		switch v {
		case TestVariantHarness:
			return "testing-harness"
		case TestVariantPerf:
			return "testing-perf"
		case TestVariantSystemTest:
			return "testing-systemtest"
		case TestVariantQAProcess:
			return "testing-qa"
		default: // TestVariantPlan
			return "testing-plan"
		}
	default: // ActivityTypeService
		return "service"
	}
}

// kebabPhase renders a canonical phase id as a command slug segment
// (detailed_design -> detailed-design).
func kebabPhase(p ActivityMethodPhase) string {
	return strings.ReplaceAll(string(p), "_", "-")
}

// CommandFor returns the .claude slash-command name for a (type, variant, phase)
// cell: "<profileSlug>-<phaseSlug>". It is total over exactly the phases
// ProfileFor(t, v) emits, and matches a .claude/commands/<name>.md file.
func CommandFor(t ActivityType, v TestingVariant, p ActivityMethodPhase) string {
	return ProfileSlug(t, v) + "-" + kebabPhase(p)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && gofmt -w internal/resourceaccess/projectstate/ && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'CommandFor|ProfileSlug' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add server/internal/resourceaccess/projectstate/commandfor.go server/internal/resourceaccess/projectstate/commandfor_test.go
git commit -m "feat(projectstate): add CommandFor routing (profile x phase -> command slug)"
```

---

### Task 2: Pass `command` as a dispatch input

**Files:**
- Modify: `server/internal/manager/construction/adapters.go:423-436` (`dispatchInputsFor`)
- Test: `server/internal/manager/construction/adapters_test.go` (create if absent; otherwise append)

**Interfaces:**
- Consumes: `projectstate.DeriveType`, `projectstate.DeriveVariant`, `projectstate.CommandFor`, `pipelineSpec{ActivityID, ComponentID, Phase, Role}` (`deps.go:321`).
- Produces: `dispatchInputsFor` now emits key `command`.

- [ ] **Step 1: Write the failing test**

Append to `adapters_test.go` (create with `package construction` + imports if new):

```go
func TestDispatchInputsForIncludesCommand(t *testing.T) {
	// A service construction phase -> service-construction command.
	in := dispatchInputsFor(pipelineSpec{
		ActivityID:  "C-BM",
		ComponentID: "billingManager",
		Phase:       "construction",
	})
	if in["command"] != "service-construction" {
		t.Errorf("command = %q, want service-construction", in["command"])
	}
	if in["activity_id"] != "C-BM" || in["component_id"] != "billingManager" {
		t.Errorf("activity/component passthrough wrong: %+v", in)
	}
	if in["phase"] != "construction" {
		t.Errorf("phase = %q, want construction", in["phase"])
	}

	// A testing harness detailed-design phase -> testing-harness-detailed-design.
	in2 := dispatchInputsFor(pipelineSpec{
		ActivityID: "N-STH",
		Phase:      "detailed_design",
	})
	if in2["command"] != "testing-harness-detailed-design" {
		t.Errorf("command = %q, want testing-harness-detailed-design", in2["command"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && GOWORK=off go test ./internal/manager/construction/ -run TestDispatchInputsForIncludesCommand -v`
Expected: FAIL — `command = ""` (key absent).

- [ ] **Step 3: Write minimal implementation**

Replace `dispatchInputsFor` in `adapters.go`:

```go
// dispatchInputsFor builds the DispatchInputs bag for a construction pipeline dispatch.
// The `command` input is the thin slash-command the workflow runs; it is computed here
// from the activity's derived type/variant and the current phase so the workflow itself
// holds no routing logic. component_id is a Manager-resolved passthrough.
func dispatchInputsFor(spec pipelineSpec) map[string]string {
	m := map[string]string{
		"activity_id":  spec.ActivityID,
		"component_id": spec.ComponentID,
	}
	if spec.Phase != "" {
		m["phase"] = spec.Phase
		typ := projectstate.DeriveType(spec.ActivityID)
		variant := projectstate.DeriveVariant(spec.ActivityID)
		m["command"] = projectstate.CommandFor(typ, variant, projectstate.ActivityMethodPhase(spec.Phase))
	}
	if spec.Role != "" {
		m["role"] = spec.Role
	}
	return m
}
```

Confirm `projectstate` is already imported in `adapters.go` (it is — used at line 206). No new import needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && gofmt -w internal/manager/construction/ && GOWORK=off go test ./internal/manager/construction/ -run TestDispatchInputsForIncludesCommand -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/manager/construction/adapters.go server/internal/manager/construction/adapters_test.go
git commit -m "feat(construction): pass computed command slug as a dispatch input"
```

---

### Task 3: Flip the default construction workflow file

**Files:**
- Modify: `server/cmd/server/config.go:168`
- Test: `server/cmd/server/config_test.go`

**Interfaces:**
- Consumes: `env(...)` helper (already in `config.go`).
- Produces: default `ConstructionWorkflowFile` is `aiarch-construct.yml` when the env var is unset.

- [ ] **Step 1: Write the failing test**

Append to `config_test.go`:

```go
func TestConstructionWorkflowFileDefault(t *testing.T) {
	t.Setenv("ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE", "")
	cfg := Load() // use the same loader the other tests in this file call
	if cfg.ConstructionWorkflowFile != "aiarch-construct.yml" {
		t.Errorf("default ConstructionWorkflowFile = %q, want aiarch-construct.yml", cfg.ConstructionWorkflowFile)
	}
}
```

> Note: match the exact loader symbol used by the existing tests in `config_test.go` (e.g. `Load()` / `loadConfig()`); read lines 1-70 of that file first and mirror its setup.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && GOWORK=off go test ./cmd/server/ -run TestConstructionWorkflowFileDefault -v`
Expected: FAIL — got `aiarch-phase.yml`.

- [ ] **Step 3: Write minimal implementation**

In `config.go:168`, change the default:

```go
		ConstructionWorkflowFile: env("ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE", "aiarch-construct.yml"),
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && GOWORK=off go test ./cmd/server/ -run 'ConstructionWorkflowFile|Config' -v`
Expected: PASS (new test + the existing config tests at lines 24/40/61 still pass — they set the env explicitly).

- [ ] **Step 5: Commit**

```bash
git add server/cmd/server/config.go server/cmd/server/config_test.go
git commit -m "fix(config): default construction workflow to aiarch-construct.yml"
```

---

### Task 4: New skill `the-method-project-state`

**Files:**
- Create: `.claude/skills/the-method-project-state/SKILL.md`

**Interfaces:**
- Produces: a skill referenced as `[[the-method-project-state]]` by every command in Tasks 6-14.

- [ ] **Step 1: Dispatch a research agent to ground the slot map**

Dispatch a `general-purpose` agent:

> "Read `server/internal/resourceaccess/projectstate/*.go` in this repo (especially `models_phase1.go`, `models_phase2.go`, `activityconstructionstatus.go`, `servicecontract.go`, `system.go`). Produce an authoritative list of every top-level slot in the `project.json` head-state object (the JSON field names), what Go type backs each, and a one-line description of what it holds. Include at least: `.systemDesign`, `.serviceContracts`, `.network`, `.activityList`, `.activityConstruction`, `.phaseArtifacts`, `.handoff`, and any testing/review-policy slots. Return a markdown table: slot | Go type | holds. Do not guess field names — quote them from the struct json tags."

- [ ] **Step 2: Write `SKILL.md`**

Create `.claude/skills/the-method-project-state/SKILL.md` using the research return for the slot table:

```markdown
---
name: the-method-project-state
description: The project.json git-as-DB driver. Use whenever a construction command must read from, traverse, or update the typed project state at .aiarch/state/project.json. Teaches the slot map, common read paths, the record-then-commit write discipline, and the git-as-DB invariants.
---

# Project State (git-as-DB)

`project.json` at `.aiarch/state/project.json` is the single source of truth for the whole project. It is a typed JSON object; the Go structs in `server/internal/resourceaccess/projectstate/` are its schema of record. This skill is how a construction agent reads and updates it. Never write a parallel markdown copy of state — markdown is render-on-read only.

## The slot map

<!-- table from the Step-1 research agent: slot | Go type | holds -->

## Reading (you do this yourself — no pre-extraction)

There is no jq pre-extraction step in CI. You read what you need directly. Common paths:

- The activity you were dispatched for: `jq '.activityList.activities[] | select(.name=="<ACTIVITY_ID>")' .aiarch/state/project.json`
- Its service contract (if a component build): `jq '.serviceContracts["<COMPONENT_ID>"]' .aiarch/state/project.json`
- A neighbour's contract (for integration/detailed-design): look up inbound/outbound parties in `.systemDesign` relationships, then read each `.serviceContracts[<neighbour>]`.
- The current review policy / handoff model: `.reviewPolicy`, `.handoff`.

Prefer reading the smallest slice that answers your question; you may run several `jq` reads.

## Updating (record the artifact, then commit)

When your phase produces an artifact that lives in state (e.g. a service contract, a UI-design concept, a phase-artifact note), write it into its typed slot and `git commit` it onto the activity branch. Each artifact maps to a slot:

- service contract -> `.serviceContracts["<component>"]`
- UI-design concept -> `.phaseArtifacts.uiDesign["<surface>"]`
- integration note -> `.phaseArtifacts.integrationNote`
- (code artifacts are files under `server/internal/...`, not state slots)

Write valid typed JSON matching the Go struct for that slot (field names + shapes exactly). Do not invent fields. After writing, commit with a message naming the activity + phase.

## Status is NOT yours

Do not write phase start/exit status or earned-value fields. The Manager (orchestrator) owns `.activityConstruction[...]` status transitions and the review gate; you only write the phase's *artifact*.

## Invariants

- `project.json` is the source of truth; commit after every state write.
- One artifact per phase, into its one slot (or as code).
- Never edit `*/generated/`.
- If a slot's shape is unclear, read the backing Go struct in `projectstate/` rather than guessing.
```

- [ ] **Step 3: Verify the skill file is well-formed**

Run: `head -4 .claude/skills/the-method-project-state/SKILL.md`
Expected: valid frontmatter (`---`, `name:`, `description:`, `---`), matching the format of `.claude/skills/the-method-service-contract/SKILL.md`.

- [ ] **Step 4: Commit**

```bash
git add .claude/skills/the-method-project-state/SKILL.md
git commit -m "feat(skills): add the-method-project-state git-as-DB driver skill"
```

---

### Task 5: Generic action + delete fat workflow and `/construct`

**Files:**
- Modify: `.github/workflows/aiarch-construct.yml`
- Delete: `.github/workflows/aiarch-phase.yml`
- Delete: `.claude/commands/construct.md`

**Interfaces:**
- Consumes: dispatch inputs `command`, `activity_id`, `component_id`, `phase`, `idempotency_token` (from Task 2).
- Produces: a workflow whose single Claude step runs `/${{ inputs.command }} ${{ inputs.component_id }} ${{ inputs.activity_id }}`.

- [ ] **Step 1: Add the `command` input and remove the `jq` extraction step**

In `aiarch-construct.yml`, under `on.workflow_dispatch.inputs`, add before `phase`:

```yaml
      # Thin routing: the Manager computes which slash command this phase runs.
      command:
        description: "slash command to run (e.g. service-construction) — computed by the Manager from type/variant/phase"
        required: true
        type: string
```

Update the `phase` input description to drop "ignored" wording (it is now load-bearing context, though the command already encodes it):

```yaml
      phase:
        description: "App-A method phase for this dispatch (also encoded in `command`)"
        required: false
        type: string
```

Delete the entire `- name: Extract service contract from project.json` step (lines ~93-107, the `jq ... service-contract.json` block).

- [ ] **Step 2: Replace the Claude step's prompt and name**

Change the step name and prompt block:

```yaml
      - name: Run construction step (claude-code-action)
        uses: anthropics/claude-code-action@v1
        with:
          claude_code_oauth_token: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}
          allowed_bots: archistrator-bot
          show_full_output: true
          claude_args: "--dangerously-skip-permissions"
          # Thin prompt: NO construction logic here. The intent lives entirely in the
          # per-(type,variant,phase) command under .claude/commands/, which pulls its
          # role agent + Method skill + the-method-project-state. The Manager computed
          # `command`; the workflow just runs it against the component + activity ids.
          prompt: |
            /${{ inputs.command }} ${{ inputs.component_id }} ${{ inputs.activity_id }}
```

Leave `run-name`, `concurrency`, `permissions`, `allowed_bots`, checkout, and Go setup untouched (load-bearing).

- [ ] **Step 3: Delete the fat workflow and the superseded command**

```bash
git rm .github/workflows/aiarch-phase.yml .claude/commands/construct.md
```

- [ ] **Step 4: Verify the workflow YAML parses**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/aiarch-construct.yml')); print('ok')"`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/aiarch-construct.yml
git commit -m "feat(ci): make aiarch-construct generic; drop aiarch-phase and /construct"
```

---

### Tasks 6-14: Author the 30 command files (one task per profile)

Each of these tasks authors the commands for one profile. Every command file follows this exact template; only the four bracketed fields and the research-grounded intent vary:

````markdown
# /<command-name>

> <ONE-LINE PURPOSE — the `<phase-label>` step of a `<profile>` activity.>

**Arguments** — `$ARGUMENTS` is `<component_id> <activity_id>` (two space-separated tokens; `component_id` may be empty for non-component activities). Parse once; do not swap them. Work lands on the shared activity branch `activity/<activity_id>` and its single PR — you are contributing commits to a PR that already exists (or open it if this is the first phase). Do NOT open a second PR.

**Agent + skills.** Work to the standard of the **`<AGENT>`** agent (`.claude/agents/<AGENT>.md`). Follow **[[<METHOD-SKILL>]]** and **[[the-method-project-state]]** for all reading/updating of `.aiarch/state/project.json`.

**Goal / intention.** <RESEARCH-GROUNDED PROSE: what this lifecycle step is for, from Righting Software. 3-8 sentences.>

## Steps

1. **Read what you need** from `.aiarch/state/project.json` per [[the-method-project-state]]: <READS>.
2. **Produce** the phase artifact: <PRODUCES> and commit it onto branch `activity/<activity_id>`.
3. **Verify** (only your own output; fast checks): <VERIFY>.
4. **Stop.** Do not mark phase status (the Manager owns that) and do not merge. Leave the PR open for the gate.
````

**State paths live in the skill, not in commands.** `project.json` uses a dual storage model: flat top-level runtime keys (`.serviceContracts`, `.activityConstruction`, `.testingState`, `.phaseArtifacts`) and a `.slots["<ArtifactKind ordinal>"].model` map for Phase-1/2 artifacts (systemDesign = slot 5, activityList = slot 9, coreUseCases = slot 4, etc.). Commands MUST NOT hardcode jq slot paths — they describe reads in artifact terms ("the activity", "its contract", "the system design") and defer the actual paths to `[[the-method-project-state]]`. The `READS`/`PRODUCES` cells below are artifact-term descriptions, not raw paths. There is no `.handoff` slot; never reference one.

**Authoring procedure for every command (do this per file):**
1. Dispatch a research agent (`Explore` or `general-purpose`) with: *"Read <CHAPTERS> in `research/rightingsoftware/OEBPS/xhtml/`. Summarize the responsibilities and intent of the `<role/phase>` in Löwy's Method — what this person produces, what 'done' means, what they must NOT do. Return 5-8 sentences I can adapt into a command's goal statement."*
2. Write the command file from the template, filling `Goal / intention` from the research return (not from memory).
3. `gofmt` is N/A; just ensure the file exists at `.claude/commands/<command-name>.md`.

Per-profile field tables follow. `READS`/`PRODUCES`/`VERIFY` are starting points; the research agent may sharpen the prose.

---

### Task 6: `service-*` commands (5 files)

**Files:** Create `.claude/commands/service-{requirements,detailed-design,test-plan,construction,integration}.md`

| command | agent | method-skill | research chapters | produces |
|---|---|---|---|---|
| service-requirements | senior-developer | the-method-service-contract | ch07, ch09 | SRS note → `.phaseArtifacts` |
| service-detailed-design | senior-developer or system-architect (per the Manager-assigned worker class) | the-method-service-contract | appb, ch14 | contract → `.serviceContracts[component]` |
| service-test-plan | test-engineer | the-method-testing | ch09, ch11 | test-plan slice → `.phaseArtifacts` |
| service-construction | junior-developer | the-method-layers | ch14 | code → `server/internal/<layer>/<pkg>/` |
| service-integration | system-architect | the-method-layers | ch11, ch12 | wiring + `.phaseArtifacts.integrationNote` |

- [ ] **Step 1: Author `service-construction` first as the worked example.**

Dispatch the research agent (ch14 — junior developer role + hand-off). Then write `.claude/commands/service-construction.md`:

````markdown
# /service-construction

> The Construction step of a service activity: implement the component exactly to its frozen contract, verify, and add your commits to the activity PR.

**Arguments** — `$ARGUMENTS` is `<component_id> <activity_id>`. Parse once; do not swap. Commits land on the existing activity branch `activity/<activity_id>` and its single PR.

**Agent + skills.** Work to the standard of the **`junior-developer`** agent (`.claude/agents/junior-developer.md`). Follow **[[the-method-layers]]** (layer + call-direction rules) and **[[the-method-project-state]]** for reading the contract from state.

**Goal / intention.** <FROM RESEARCH: the junior developer builds one component against a contract already designed by a senior; implements exactly what the contract specifies — no more, no less; never widens the contract silently; is code-reviewed by the senior who designed it. "Done" = the contract is satisfied and the component's own fast checks pass.>

## Steps

1. **Read the contract** from `.aiarch/state/project.json` → `.serviceContracts["<component_id>"]` per [[the-method-project-state]]. It carries `Layer`, `Ops`, `Inbound`/`Outbound`, `DataContracts`, `ErrorModel`, `Idempotency`. Implement exactly it. If it has a gap, do NOT widen it (see `junior-developer`).
2. **Implement** under `server/internal/<layer>/<pkg>/` (`<layer>` from the contract's `Layer`). Match existing code in that layer. Stay inside the component. Do NOT edit `*/generated/`. Commit onto `activity/<activity_id>`.
3. **Verify YOUR code** (working-directory `server`): `gofmt -w .`; `GOWORK=off go build ./...`; `GOWORK=off go vet ./...`; `GOWORK=off go test ./internal/<layer>/<pkg>/...`. Only your package — not `make test-short`.
4. **Stop.** Do not mark phase status (the Manager owns it) and do not merge. Leave the PR for the gate.
````

- [ ] **Step 2: Author the other four `service-*` files** using the template + a research agent each (chapters per the table above).

- [ ] **Step 3: Verify the five files exist.**

Run: `ls .claude/commands/service-*.md | wc -l`
Expected: `5`.

- [ ] **Step 4: Commit**

```bash
git add .claude/commands/service-*.md
git commit -m "feat(commands): add service-* per-phase construction commands"
```

---

### Task 7: `frontend-*` commands (5 files)

**Files:** Create `.claude/commands/frontend-{requirements,detailed-design,test-plan,construction,integration}.md`

| command | agent | method-skill | research chapters | produces |
|---|---|---|---|---|
| frontend-requirements | ui-designer | the-method-core-use-cases | ch06, ch07 | UX requirements → `.phaseArtifacts` |
| frontend-detailed-design | ui-designer | (ui-design routing) | ch14 | UI design → `.phaseArtifacts.uiDesign[surface]` |
| frontend-test-plan | test-engineer | the-method-testing | ch09, ch11 | flows slice → `.phaseArtifacts` |
| frontend-construction | junior-developer | the-method-layers | ch14 | SPA code |
| frontend-integration | system-architect | the-method-layers | ch11, ch12 | wiring + `.phaseArtifacts.integrationNote` |

- [ ] **Step 1: Author all five** using the template + a research agent each (labels: UX Requirements, Design, Flows, Construction, Integration).
- [ ] **Step 2: Verify.** Run: `ls .claude/commands/frontend-*.md | wc -l` → `5`.
- [ ] **Step 3: Commit** `git add .claude/commands/frontend-*.md && git commit -m "feat(commands): add frontend-* per-phase construction commands"`

---

### Task 8: `deployment-*` commands (3 files)

**Files:** Create `.claude/commands/deployment-{detailed-design,construction,integration}.md` (labels: Provisioning Spec, Construction, Convergence Verification)

| command | agent | method-skill | research chapters | produces |
|---|---|---|---|---|
| deployment-detailed-design | project-manager | the-method-planning-assumptions | ch07, appc | provisioning spec → `.phaseArtifacts` |
| deployment-construction | junior-developer | — | ch07, ch14 | infra/manifests |
| deployment-integration | system-architect | — | ch12 | convergence verification note |

- [ ] **Step 1: Author all three** via template + research agent each.
- [ ] **Step 2: Verify.** `ls .claude/commands/deployment-*.md | wc -l` → `3`.
- [ ] **Step 3: Commit** `git add .claude/commands/deployment-*.md && git commit -m "feat(commands): add deployment-* per-phase construction commands"`

---

### Task 9: `documentation-*` commands (3 files)

**Files:** Create `.claude/commands/documentation-{detailed-design,construction,integration}.md` (labels: Outline, Authoring, Doc Review)

| command | agent | method-skill | research chapters | produces |
|---|---|---|---|---|
| documentation-detailed-design | system-architect | the-method-architecture | ch03, ch04 | doc outline → `.phaseArtifacts` |
| documentation-construction | system-architect | — | ch03, ch05 | doc files |
| documentation-integration | system-architect | — | appc | doc review note |

- [ ] **Step 1: Author all three** via template + research agent each.
- [ ] **Step 2: Verify.** `ls .claude/commands/documentation-*.md | wc -l` → `3`.
- [ ] **Step 3: Commit** `git add .claude/commands/documentation-*.md && git commit -m "feat(commands): add documentation-* per-phase construction commands"`

---

### Task 10: `testing-plan-*` commands (3 files)

**Files:** Create `.claude/commands/testing-plan-{requirements,construction,integration}.md` (labels: Use-Case Trace, Plan Authoring, Plan Review)

| command | agent | method-skill | research chapters | produces |
|---|---|---|---|---|
| testing-plan-requirements | test-engineer | the-method-testing | ch09, ch11 | use-case trace → `.phaseArtifacts` |
| testing-plan-construction | test-engineer | the-method-testing | ch09, ch11 | system test plan → `.phaseArtifacts` / `.testingState` |
| testing-plan-integration | qa-engineer | the-method-testing | ch09, ch14 | plan review note |

- [ ] **Step 1: Author all three** via template + research agent each (test-engineer = "writes code to break the system", not a tester).
- [ ] **Step 2: Verify.** `ls .claude/commands/testing-plan-*.md | wc -l` → `3`.
- [ ] **Step 3: Commit** `git add .claude/commands/testing-plan-*.md && git commit -m "feat(commands): add testing-plan-* per-phase construction commands"`

---

### Task 11: `testing-harness-*` commands (3 files)

**Files:** Create `.claude/commands/testing-harness-{detailed-design,construction,integration}.md` (labels: Harness Design, Harness Construction, Harness Review)

| command | agent | method-skill | research chapters | produces |
|---|---|---|---|---|
| testing-harness-detailed-design | test-engineer | the-method-testing | ch11, ch14 | harness design → `.phaseArtifacts` |
| testing-harness-construction | test-engineer | the-method-testing | ch11, ch14 | harness code |
| testing-harness-integration | qa-engineer | the-method-testing | ch09, ch14 | harness review note |

- [ ] **Step 1: Author all three** via template + research agent each.
- [ ] **Step 2: Verify.** `ls .claude/commands/testing-harness-*.md | wc -l` → `3`.
- [ ] **Step 3: Commit** `git add .claude/commands/testing-harness-*.md && git commit -m "feat(commands): add testing-harness-* per-phase construction commands"`

---

### Task 12: `testing-perf-*` commands (3 files)

**Files:** Create `.claude/commands/testing-perf-{detailed-design,construction,integration}.md` (labels: Perf Scenario Design, Rig Construction, Rig Review)

| command | agent | method-skill | research chapters | produces |
|---|---|---|---|---|
| testing-perf-detailed-design | test-engineer | the-method-testing | ch11, ch13 | perf scenarios → `.phaseArtifacts` |
| testing-perf-construction | test-engineer | the-method-testing | ch11, ch13 | perf rig code |
| testing-perf-integration | qa-engineer | the-method-testing | ch09, ch13 | rig review note |

- [ ] **Step 1: Author all three** via template + research agent each.
- [ ] **Step 2: Verify.** `ls .claude/commands/testing-perf-*.md | wc -l` → `3`.
- [ ] **Step 3: Commit** `git add .claude/commands/testing-perf-*.md && git commit -m "feat(commands): add testing-perf-* per-phase construction commands"`

---

### Task 13: `testing-systemtest-*` commands (3 files)

**Files:** Create `.claude/commands/testing-systemtest-{requirements,construction,integration}.md` (labels: Smoke Pass, Use-Case Execution, Regression & Sign-off)

| command | agent | method-skill | research chapters | produces |
|---|---|---|---|---|
| testing-systemtest-requirements | software-tester | the-method-testing | ch09, ch11 | smoke pass → `.testingState` |
| testing-systemtest-construction | software-tester | the-method-testing | ch11, ch13 | use-case execution + defects → `.testingState` |
| testing-systemtest-integration | software-tester | the-method-testing | ch11, ch13 | regression & sign-off note |

- [ ] **Step 1: Author all three** via template + research agent each (software-tester runs the harness + regression, files defects — NOT the test-engineer, NOT QA).
- [ ] **Step 2: Verify.** `ls .claude/commands/testing-systemtest-*.md | wc -l` → `3`.
- [ ] **Step 3: Commit** `git add .claude/commands/testing-systemtest-*.md && git commit -m "feat(commands): add testing-systemtest-* per-phase construction commands"`

---

### Task 14: `testing-qa-*` commands (2 files)

**Files:** Create `.claude/commands/testing-qa-{detailed-design,construction}.md` (labels: Gate Definition, Process Audit)

| command | agent | method-skill | research chapters | produces |
|---|---|---|---|---|
| testing-qa-detailed-design | qa-engineer | the-method-testing | ch09, ch14 | gate definition → `.phaseArtifacts` (the Manager records the actual `.reviewPolicy` separately; the agent does NOT write it) |
| testing-qa-construction | qa-engineer | the-method-testing | ch09, ch14 | process audit note |

- [ ] **Step 1: Author both** via template + research agent each (QA = process reviewer, "sign of organizational maturity"; QA ≠ testing/quality-control).
- [ ] **Step 2: Verify.** `ls .claude/commands/testing-qa-*.md | wc -l` → `2`.
- [ ] **Step 3: Commit** `git add .claude/commands/testing-qa-*.md && git commit -m "feat(commands): add testing-qa-* per-phase construction commands"`

---

### Task 15: Lock the matrix — command-file existence test

**Files:**
- Test: `server/internal/resourceaccess/projectstate/commandfiles_test.go`

**Interfaces:**
- Consumes: `allProfileCombos()` and `CommandFor` (Task 1), `ProfileFor.PhaseIDs()`.

- [ ] **Step 1: Write the test** (files already exist from Tasks 6-14, so this passes immediately — it is a drift guard, not TDD-red).

Create `commandfiles_test.go`:

```go
package projectstate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// repoCommandsDir walks up from this test file to the repo root (the dir holding
// .claude) and returns .claude/commands.
func repoCommandsDir(t *testing.T) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, ".claude", "commands")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not locate .claude/commands above test file")
	return ""
}

// TestEveryProfilePhaseHasCommandFile asserts the command matrix is exactly the
// flattening of ProfileFor: every (profile, phase) has a .claude/commands/<name>.md.
func TestEveryProfilePhaseHasCommandFile(t *testing.T) {
	cmds := repoCommandsDir(t)
	seen := map[string]bool{}
	for _, combo := range allProfileCombos() {
		for _, p := range ProfileFor(combo.t, combo.v).PhaseIDs() {
			name := CommandFor(combo.t, combo.v, p)
			seen[name] = true
			path := filepath.Join(cmds, name+".md")
			if _, err := os.Stat(path); err != nil {
				t.Errorf("missing command file for (%v,%v,%q): %s.md", combo.t, combo.v, p, name)
			}
		}
	}
	// Sanity: the matrix is exactly 30 distinct commands.
	if len(seen) != 30 {
		t.Errorf("expected 30 distinct commands, got %d", len(seen))
	}
}
```

- [ ] **Step 2: Run the test**

Run: `cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestEveryProfilePhaseHasCommandFile -v`
Expected: PASS (all 30 files exist; count is 30).

- [ ] **Step 3: Full verification**

Run:
```bash
cd server && gofmt -l . && GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test ./internal/resourceaccess/projectstate/... ./internal/manager/construction/... ./cmd/server/...
```
Expected: no gofmt output, build/vet clean, all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add server/internal/resourceaccess/projectstate/commandfiles_test.go
git commit -m "test(projectstate): lock command matrix to ProfileFor flattening"
```

---

## Self-Review

**Spec coverage:**
- One thin action → Task 5. ✓
- 30 per-(type,variant,phase) commands → Tasks 6-14 (5+5+3+3+3+3+3+3+2 = 30). ✓
- One PR per activity, steps contribute commits → template's "contribute to the existing activity PR" + backend already opens one PR per activity (unchanged). ✓
- Command name computed in Go, passed as input → Tasks 1-2. ✓
- `component_id` kept as passthrough → Task 2 (kept) + Task 5 prompt. ✓
- No jq / agent reads state itself → Task 5 (jq step removed) + Task 4 skill. ✓
- Project-state skill → Task 4. ✓
- Artifact-vs-status split → skill "Status is NOT yours" + template step 4. ✓
- Config default flip → Task 3. ✓
- Delete aiarch-phase.yml → Task 5. ✓
- Review gate already done → no task, noted in Global Constraints. ✓
- Matrix drift guard → Task 15. ✓
- Research-grounded prompts → authoring procedure in Tasks 6-14. ✓

**Placeholder scan:** The bracketed `<...>` fields in the command template and the `service-construction` `Goal / intention` are intentional author-fill-from-research slots, not TODOs — the authoring procedure specifies exactly how to fill each (dispatch research agent → adapt return). All Go steps show complete code. No "TBD"/"handle edge cases"/"similar to Task N".

**Type consistency:** `CommandFor(t, v, p)` / `ProfileSlug(t, v)` / `kebabPhase(p)` / `allProfileCombos()` used identically across Tasks 1, 2, and 15. Dispatch input keys (`command`, `activity_id`, `component_id`, `phase`) consistent between Task 2 (producer) and Task 5 (consumer). Command names in the tables match `CommandFor` output (`<profileSlug>-<kebab phase>`).

## Notes for the implementer

- Tasks 1-3 and 15 are pure Go/config and fully deterministic. Tasks 4 and 6-14 involve dispatched research agents; their *prose* output will vary, but the file paths, agent/skill assignments, and slot targets are fixed by the tables.
- `/implement-project` (the local, non-CI orchestrator) is intentionally **out of scope** — it still exists and may later be rewired to call these 30 commands. Do not delete or edit it in this plan.
- `DeriveType` currently routes only Service/Frontend/Testing to dispatch; the deployment/documentation commands (Tasks 8-9) are authored and matrix-locked but only reachable once a separate follow-up extends `DeriveType`. This is expected, not a gap.
