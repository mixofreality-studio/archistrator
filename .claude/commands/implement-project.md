# Implement Project

> Phase 3 / construction. Pick the next unblocked activity from the `.network` slot in `.aiarch/state/project.json` and execute it through the right role agent, gated by the chosen hand-off model. Loop until blocked, complete, or interrupted.
>
> **archistrator is a single Go server repo. State is git-as-DB:** all project
> state lives in typed slots in `.aiarch/state/project.json`, NOT in
> `methodpoc/designs/<product>/*.md` files. Components live under
> `server/internal/<layer>/<pkg>/`. Markdown/DSL is render-on-read.

**Skill reference:** Invoke `the-method` skill. This command orchestrates the Phase 3 sub-skills:

- [[the-method-handoff]] — chosen ONCE at Phase 3 start; sets the contract-design + review ownership model for the project
- [[the-method-service-contract]] — invoked per `detailed-design` activity
- [[the-method-project-tracking]] — invoked weekly during construction
- [[the-method-scope-change]] — event-triggered (and entry point for `/sdp-review`)

## Usage

```
/implement-project <product> [--once|--loop]
```

- `--once` (default): pick and execute one activity, then stop.
- `--loop`: keep picking activities until blocked or complete. Pause for user
  review after each completion. Weekly tracking and scope-change events fire
  inline.

## Prerequisites

- The `.network` slot must be committed with a chosen option set (management has committed).
- The Phase-2 project-design standard check must have passed (recorded against `.sdpReview`).
- The `.systemDesign` architecture artifact is committed.

## Workflow

### Step 0: Pick the hand-off model (ONE-TIME at Phase 3 start)

Skip if the `.handoff` slot is already committed.

Invoke [[the-method-handoff]] via `system-architect`:

> Pick the construction hand-off model for this project per Löwy ch. 14 §5:
>
>   - **Senior hand-off** (default): architect designs detailed contracts;
>     senior reviews and amends; junior implements.
>   - **Senior-as-junior-architect hand-off**: senior designs contracts
>     under architect mentorship; architect reviews contracts; senior
>     reviews junior implementation. Mentorship goal.
>   - **Junior hand-off** (avoid unless trivial): junior designs and
>     implements; architect reviews everything.
>
> Document the choice + rationale in the `.handoff` slot of
> `.aiarch/state/project.json`. State explicitly who designs contracts and who
> reviews implementation, per activity type.

This decision threads through every per-activity step below.

### Step 1: Load context

Dispatch `project-manager`:

> Load the `.network` slot from `.aiarch/state/project.json`.
>
> Verify:
>   - the network's chosen option is set (not null)
>   - the network's start date is set
>   - The system design is complete: the `.systemDesign` artifact is committed
>   - The hand-off model is set: the `.handoff` slot is committed
>
> Also load:
>   - `.systemDesign` (architecture context)
>   - `.planningAssumptions` (constraints)
>   - `.handoff` (contract-design ownership)

### Step 2: Pick next activity

Per the `project-manager` agent's picking algorithm:

```
runnable = activities where
  status == "not-started"
  AND all dependencies are status == "done"

if runnable is empty:
    if every activity is status == "done":
        report "PROJECT COMPLETE — schedule debrief"
        stop
    else:
        report "BLOCKED — investigate"
        list activities with status == "in-progress" or "blocked"
        stop

next = runnable sorted by float_days ascending (critical path first)
       then by id ascending (deterministic tie-break)
```

Record the activity as started in `.activityConstruction[next.id]` (the
`RecordPhaseStarted` verb sets it in-progress with `startedAt = today`).
**Commit before dispatching the role agent.**

### Step 3: Dispatch by activity type

#### If `next.type == "detailed-design"`

Invoke [[the-method-service-contract]]. The hand-off model from Step 0 determines who designs:

- **senior hand-off** → architect designs the contract; senior reviews and amends.
- **senior-as-junior-architect** → senior designs the contract under architect mentorship; architect reviews.
- **junior hand-off** → junior designs the contract; architect reviews.

The designer is dispatched first; the reviewer is dispatched on completion. Output is the typed contract committed to `.serviceContracts[<component>]` (there is no `designs/.../contracts/<component>.md` file; markdown is render-on-read). Walk the Appendix B contract design rules and the Appendix C §6 standard check (3–5 ops per contract, max 12, reject ≥20).

#### If `next.type == "construction"`

Dispatch the implementer (`junior-developer` by default; `senior-developer` if the activity carries a `role: senior-developer` override):

> Implement `<next.component>` against its service contract in
> `.aiarch/state/project.json` → `.serviceContracts["<next.component>"]`
> (in a CI run, pre-extracted to `service-contract.json` at the repo root).
> Activity: `<next.id> — <next.name>`. Duration estimate:
> `<next.duration_days>` days. Component package:
> `server/internal/<layer>/<pkg>/`.
>
> System context: the committed `.systemDesign` artifact.
>
> Execute the activity. Verify with `GOWORK=off go build/vet/test` under
> `server/`. Put completion notes (what was built, any contract deviation,
> test results) in the **PR body + commit messages**; the activity record
> in `.activityConstruction[<next.id>]` captures phase exits and build status.

On completion the senior (per the hand-off model) reviews the construction via [[the-method-review-routing]]. Architect reviews are escalated for failed reviews or material design questions.

#### Other activity types

| `next.type` | Agent | Notes |
|---|---|---|
| `integration` | `system-architect` | Integration across components / layer boundaries; integration note → `.phaseArtifacts.integrationNote` |
| `quality` / testing | `test-engineer` / `software-tester` | Outputs → `.testingState` (see [[the-method-testing]]) |
| `noncoding` | `product-manager` or user | Research, requirements, deployment, training → `.phaseArtifacts` |

The dispatched agent gets the activity context (id, name, type, component, duration, completed dependencies + their notes) and records its output in the appropriate `project.json` slot (`.phaseArtifacts` / `.testingState`) plus the activity record in `.activityConstruction[<next.id>]` — not a `designs/*.md` log.

### Step 4: Verify and close

After the role agent returns:

- For `construction` activities: verify `GOWORK=off go build/vet/test` passes under `server/` AND that the senior review (per hand-off model) is recorded against `.activityConstruction[<next.id>]`. If failing, **do not mark done**. The activity stays in-progress and the user must fix.
- For `detailed-design` activities: verify the contract entry exists at `.serviceContracts[<component>]` and that the appropriate reviewer (per hand-off model) has signed off (recorded on the contract / activity record). The Appendix C §6 contract checklist must pass.
- For `noncoding` / `integration` / testing activities: verify the named output exists in its `.phaseArtifacts` / `.testingState` slot.

If verified:

- Record the activity exit in `.activityConstruction[<next.id>]` (`RecordActivityExited`, `completedAt = today`)
- Commit `project.json`

If not verified:

- Leave the activity in-progress
- Record the issue against `.activityConstruction[<next.id>]`
- Report to user

### Step 5: Weekly tracking (if a week boundary crossed)

If today is the start of a new week (or first activity of a new week), invoke [[the-method-project-tracking]] via `project-manager`:

> Walk Appendix A and the Appendix C §5 standard check. Capture binary
> activity exits, compute earned value, build projections, detect
> off-track patterns.
>
> Compute:
>   - planned_progress_pct (per the chosen option's S-curve)
>   - actual_progress_pct (sum of duration_days of done activities / total duration_days)
>   - planned_effort_days (per option's staffing distribution)
>   - actual_effort_days (sum of duration_days for activities done or in-progress in this week)
>
> Project the trends forward. Apply App A pattern recognition:
>   - All-is-well: nothing to do
>   - Underestimating: alert user; recommend deadline push or scope reduction (NEVER add people)
>   - Resource leak: alert user; escalation path
>   - Overestimating: alert user; recommend releasing a resource or compressing
>
> Record the week-`<N>` tracking point in `project.json` (the earned-value
> point + projection alongside `.activityConstruction` exits). If a corrective
> action requires re-options (variance large enough to redesign), trigger
> Step 6 (scope-change).

### Step 6: Scope-change / variance event (triggered)

If during Step 4 a scope change request arrives, during Step 5 variance triggers a corrective action that requires re-options, or the user invokes `/sdp-review`, invoke [[the-method-scope-change]] (see also `/sdp-review`):

> The architect + project manager re-run project design to produce fresh
> options. Never silently absorb scope. Loop back to `/project-design`
> for a new SDP review.

After scope-change resolves and management commits to an updated option, resume Step 2 with the new `.network` slot.

### Step 7: Loop or stop

If invoked with `--loop`:

- Pause for user review.
- Ask: "Continue?" If yes, go back to Step 2.

If `--once` (default):

- Stop here.

The loop terminates when every activity in the `.network` slot has exited (its `.activityConstruction` record is done).

## Error handling

- **No chosen option on `.network`** → tell user management hasn't committed yet; can't execute.
- **No `.handoff` slot** → run Step 0; cannot dispatch activities without it.
- **All activities done** → say "project complete," recommend `/sdp-review` for next subsystem or formal debrief.
- **All runnable activities require an agent that isn't available** (ui-designer, test-engineer, devops) → tell user to execute manually and update `.activityConstruction` directly, then re-run.
- **Build/test failure on construction** → leave the activity in-progress, tell user, don't proceed.
- **Contract review fails Appendix C §6** → loop back into [[the-method-service-contract]]; do not mark detailed-design done.
- **Activity in progress crossed estimated duration significantly** → flag as schedule risk, surface to user, consider triggering Step 6 (scope-change) for re-options.
