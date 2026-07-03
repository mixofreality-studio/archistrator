# Per-Phase Construction Commands — Design

**Date:** 2026-07-02
**Status:** Approved design, pending implementation plan
**Area:** Construction dispatch (`.github/workflows`, `.claude/commands`, `.claude/skills`, `server/internal/manager/construction`, `server/internal/resourceaccess/projectstate`)

## Problem

Construction is currently dispatched two incompatible ways:

- `aiarch-construct.yml` — the *documented* aligned workflow: thin, runs `/construct`, reads `project.json`. Referenced by docs/spec/tests but **not** the runtime default.
- `aiarch-phase.yml` — the *actual* runtime default (`config.go:168`). Fat: five `if: phase == …` steps, each carrying a hardcoded inline prompt, writing markdown (`docs/srs/*.md`) and never touching `project.json`.

The fat workflow puts intent and instructions inside the action YAML, mixes per-phase logic into one file, and hardcodes context extraction (`jq` pre-pulls a service contract into `service-contract.json`). This is the opposite of "each dispatch loads exactly the context it needs."

Meanwhile the backend **already** models everything we want:

- `ProfileFor(type, variant)` (`activityprofile.go:45-119`) is the single source of truth for which lifecycle phases each activity type runs, in order, with per-type labels and earned-value weights.
- One branch/PR per activity (`activity/<id>`, `gitnaming.go:26-28`), opened once (`workflow.go:493`) and reused.
- One GH-Actions job per phase (`workflow.go:499-547`), all committing to that shared PR.
- A per-phase review gate already sits between dispatches (`runPhaseGate`, `workflow.go:539`).

The gap is narrow: **the per-phase jobs run fat inline prompts instead of thin, per-`(type,variant,phase)` slash commands, and there is no consolidated skill teaching the agent how to read/update the `project.json` store.**

## Goal

- **One** generic construction GH action, carrying zero prompt logic.
- **~30** small `.claude/commands`, one per `(profile, phase)` cell of `ProfileFor` — each with a distinct intent, loading only the context that phase needs.
- Each activity still produces **one PR**; each lifecycle step is a separate action run contributing commits to that PR, so the review policy can gate a human between any two steps.
- A dedicated skill that makes the agent excellent at traversing, understanding, and updating the `project.json` git-as-DB object — replacing all ad-hoc `jq` scripting in the pipeline.

## The activity-type / lifecycle-phase matrix (authoritative)

Types — `ActivityType` (`activityconstructionstatus.go:234-247`): `service`, `frontend`, `testing`, `deployment`, `documentation` (the "Docs" UI chip). Testing has 5 variants — `TestingVariant` (`activityconstructionstatus.go:285-293`): Plan (`N-STP`), Harness (`N-STH`), Perf (`N-PERF`), SystemTest (`N-IT`), QAProcess (`N-QA`).

Phases — `ActivityMethodPhase`, a string enum (`activityconstructionstatus.go:318-333`), exactly five ids: `requirements`, `detailed_design`, `test_plan`, `construction`, `integration`.

The per-type ordered phase sets come straight from `ProfileFor`:

| Profile (type / variant) | Ordered phases | Command count |
|---|---|---|
| service | requirements, detailed_design, test_plan, construction, integration | 5 |
| frontend | requirements, detailed_design, test_plan, construction, integration | 5 |
| deployment | detailed_design, construction, integration | 3 |
| documentation | detailed_design, construction, integration | 3 |
| testing / Plan | requirements, construction, integration | 3 |
| testing / Harness | detailed_design, construction, integration | 3 |
| testing / Perf | detailed_design, construction, integration | 3 |
| testing / SystemTest | requirements, construction, integration | 3 |
| testing / QAProcess | detailed_design, construction | 2 |

**Total: 30 commands.** The command set is defined as the flattening of `ProfileFor` over every `(type, variant, phase)` — it is not an independent list to maintain.

## Design

### 1. One thin, generic action

Consolidate to a single `aiarch-construct.yml`; **delete `aiarch-phase.yml`**. The action keeps the existing dispatch cadence (one job per phase, all committing to the shared `activity/<id>` branch/PR, `runPhaseGate` between phases). Its five inline-prompt steps collapse to one step:

```yaml
- name: Run construction step (claude-code-action)
  uses: anthropics/claude-code-action@v1
  with:
    claude_code_oauth_token: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}
    allowed_bots: archistrator-bot
    show_full_output: true
    claude_args: "--dangerously-skip-permissions"
    prompt: |
      /${{ inputs.command }} ${{ inputs.activity_id }}
```

There is **no** `jq` / `service-contract.json` pre-extraction step. The agent loads what it needs from `project.json` itself (see §4).

Dispatch inputs reduce to:

- `command` (string, required) — the slash-command name, computed by the Manager.
- `activity_id` (string, required) — the activity the agent works; it looks up everything else it needs from `project.json`.
- `component_id` (string, required) — a **Manager-resolved passthrough**. The Manager already computes it via `resolveComponentID(item.Title, produced, proj.ServiceContracts)` (`adapters.go:99`, a heuristic title/artifact→contract-key match). Re-deriving it agent-side would duplicate fragile Go logic, so the resolved id is handed over. It may be empty for non-component activities (some testing/deployment/docs).
- `phase` (string, required) — retained for the Manager's gate/status bookkeeping.
- `idempotency_token` (string, required) — unchanged RA dedup anchor.

Only the `jq` *contract-extraction* step is removed; `component_id` stays. The agent reads the contract itself from `.serviceContracts[component_id]` and traverses all other slots directly.

### 2. Command name computed in Go, passed as a dispatch input

A pure function colocated with `ProfileFor`:

```go
// CommandFor returns the .claude slash-command name for a (type, variant, phase)
// cell. It is total over exactly the phases ProfileFor(type, variant) emits.
func CommandFor(t ActivityType, v TestingVariant, p ActivityMethodPhase) string
```

It returns `"<profileSlug>-<phaseSlug>"`:

- profileSlug: `service`, `frontend`, `deployment`, `documentation`, or for testing `testing-plan` / `testing-harness` / `testing-perf` / `testing-systemtest` / `testing-qa`.
- phaseSlug (kebab): `requirements`, `detailed-design`, `test-plan`, `construction`, `integration`.

The Manager already resolves `type`/`variant` at dispatch (`adapters.go:206-207`) and walks `phase` (`workflow.go:499`). `dispatchInputsFor` (`adapters.go:423-436`) adds `"command": CommandFor(type, variant, phase)` and drops `component_id`. **The YAML holds no routing map** — all routing is `CommandFor`.

`config.go:168` default flips from `aiarch-phase.yml` to `aiarch-construct.yml`, resolving the current drift where docs/tests and runtime disagree.

### 3. The 30 command files

Named `<profileSlug>-<phaseSlug>.md` under `.claude/commands/`. Each is small and declares four things: its intent, the one agent it runs to standard, the one Method skill it pulls, and — always — the shared project-state skill (§4). No data-plumbing logic. Representative rows:

| command | agent | Method skill | reads from project.json | produces (artifact → location) |
|---|---|---|---|---|
| `service-detailed-design` | senior/architect per `.handoff` | `the-method-service-contract` | `.systemDesign`, neighbor contracts | service contract → `.serviceContracts[component]` |
| `service-construction` | `junior-developer` | `the-method-layers` | `.serviceContracts[component]` | code → `server/internal/<layer>/<pkg>/` |
| `service-integration` | `system-architect` | — | neighbor contracts, `main.go` seams | wiring + `.phaseArtifacts.integrationNote` |
| `frontend-detailed-design` | `ui-designer` | (ui-design routing) | `.coreUseCases`, `.systemDesign` | UI design → `.phaseArtifacts.uiDesign[surface]` |
| `frontend-construction` | `junior-developer` | `the-method-layers` | UI design artifact | SPA code |
| `testing-harness-construction` | `test-engineer` | `the-method-testing` | `.systemDesign`, test plan | harness code |
| `testing-systemtest-construction` | `software-tester` | `the-method-testing` | `.testingState`, harness | test run + defects |
| `documentation-construction` | `system-architect` / PM | — | mission, `.systemDesign` | doc files |

(The full per-command intent text is authored during implementation; the matrix above fixes the agent/skill/slot assignments.)

#### Command-authoring methodology (grounded in the source material)

The intent/goal prose in each command must not be paraphrased from memory. When authoring a command, first dispatch a research agent to read the relevant *Righting Software* (Löwy) material for that activity type's role and responsibilities, and write the command's "what this step is for" section from that. The corpus is the EPUB at `research/rightingsoftware/OEBPS/xhtml/` (chapters `ch01`–`ch14`, appendices `appa`/`appb`/`appc`).

Starting map of activity type/role → most relevant chapters (the research agent confirms and expands):

- **service / detailed-design & construction** — `ch14` (the team; senior/junior developer roles, hand-off), `appb` (contract design).
- **all phases / standards & directives** — `appc` (design/project-design standard checks, the directives).
- **frontend (ui-design)** — UI-design role material; confirm chapter via the research agent.
- **testing (all variants) & quality** — `ch09`, `ch11`, `ch12`, `ch13` (testing types, test engineer vs tester vs QA), `ch14` (roles).
- **project tracking / integration cadence** — `appa` (tracking), `ch07` (project design roles).

One research agent per command (or per profile, reused across its phases) reads its slice and returns the role's responsibility and intent; the command author writes the prompt from that return, not from prior knowledge.

### 4. Shared skill: the project-state driver

A single new skill — proposed name `the-method-project-state` — that every construction command loads. It consolidates the state-access knowledge currently smeared across commands/agents into one authoritative place, and is the reason no `jq` scripting lives in the pipeline. It teaches the agent to:

- **Map the store** — what each slot holds (`.systemDesign`, `.serviceContracts`, `.network`, `.activityConstruction`, `.phaseArtifacts`, `.testingState`, `.handoff`, …), pointing at the Go structs in `server/internal/resourceaccess/projectstate/` as the schema of record.
- **Traverse** — common read paths done by the agent (find the activity, its component, its neighbors' contracts, the current phase) using `jq` / direct reads it runs itself.
- **Update** — write a valid typed slot and `git commit` it; which artifact each phase produces and where it belongs; schema/validation invariants so it cannot emit malformed JSON.
- **Discipline** — `project.json` is the single source of truth; commit after every write; never write a parallel markdown copy.

### 5. Artifact vs. status ownership

Clean split, resolving the prior ambiguity about "how writes happen":

- **The agent owns artifacts.** Guided by the project-state skill, it writes the phase's product into its slot (or as repo code) and commits it to the shared activity branch.
- **The Manager owns status.** It keeps writing the phase start/exit status transitions it already does, because the review gate and earned-value tracking depend on those being written reliably by the orchestrator, not as a best-effort agent step.

### 6. Review gate

**Already implemented — no work in this plan.** `runPhaseGate` (`workflow.go:539-547`) is live: it records the phase start and, *iff* the `ReviewPolicy` requires a human for this `(activityType, phase)`, suspends on a phase-multiplexed decision signal before the next phase dispatches. The policy plumbing exists end-to-end — `ReviewPolicy`, `GatedPhasesByType`, `ReviewPolicyFromGateIDs`, and `UpdateReviewPolicy` (op 2.7). Because each lifecycle step is its own dispatch committing to the shared PR, a required human review already lands **between two commits on one PR**, exactly matching the requirement. This plan changes only what each phase's job *runs*, not the gate around it.

## Invariants & tests

- `CommandFor` is **total** over exactly the phases `ProfileFor(type, variant).PhaseIDs()` emits — a test asserts this for every type/variant.
- A test asserts a `.claude/commands/<name>.md` file exists for every `CommandFor` output and that no orphan command files exist — the command matrix cannot silently drift from `ProfileFor`.
- The consolidated action declares `command`, `activity_id`, `phase`, `idempotency_token` and no others; `config_test.go` asserts the default workflow file is `aiarch-construct.yml`.

## Out of scope

- The UI's separate, more granular display phase templates (`webapp/.../lifecycleTemplates.ts`, 7–8 phases) — a display taxonomy, not the dispatch model. Not reconciled here.
- `DeriveType` currently emitting only Service/Frontend/Testing (deployment/documentation reachable only via `ClassifyType`) — wiring those into dispatch is a separate follow-up; this design defines their commands so they are ready when dispatch reaches them.
- Retiring the dead `hooks/validate-structurizr.sh` hook — unrelated cleanup.

## Migration / removal

- Delete `aiarch-phase.yml` and its inline prompts.
- Delete the `service-contract.json` `jq` extraction step.
- Retire or fold the existing `/construct` and per-phase-prompt logic into the new command set; `/implement-project`'s Step 3 branch table is superseded by `CommandFor` for the dispatch path.

## Summary of changes

1. New `CommandFor(type, variant, phase)` in `projectstate` (+ tests for totality and command-file existence).
2. `adapters.go dispatchInputsFor`: add `command` (keep `component_id` as passthrough).
3. `config.go:168`: default → `aiarch-construct.yml`; update `config_test.go`.
4. `aiarch-construct.yml`: single generic step running `/${{ inputs.command }} ${{ inputs.component_id }} ${{ inputs.activity_id }}`; remove the `jq` extraction step; delete `.claude/commands/construct.md` (superseded).
5. Delete `aiarch-phase.yml`.
6. 30 command files under `.claude/commands/`, authored with the research-grounded methodology.
7. New `the-method-project-state` skill; commands reference it; ad-hoc state instructions removed from other command/agent files.
8. Review gate: already implemented (`runPhaseGate` + `ReviewPolicy`) — **no code**, verification only.
