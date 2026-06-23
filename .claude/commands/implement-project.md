# Implement Project

> Phase 3 / construction. Pick the next unblocked activity from `network.yaml` and execute it through the right role agent, gated by the chosen hand-off model. Loop until blocked, complete, or interrupted.

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

- `designs/<product>/project/network.yaml` must exist with `chosen_option` set (management has committed).
- `designs/<product>/project/project-standard-checklist.md` must exist (Phase 2 standard check passed).
- `designs/<product>/system/architecture.dsl` exists.

## Workflow

### Step 0: Pick the hand-off model (ONE-TIME at Phase 3 start)

Skip if `designs/<product>/implementation/handoff.md` already exists.

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
> Document the choice + rationale in
> `designs/<product>/implementation/handoff.md`. State explicitly who
> designs contracts and who reviews implementation, per activity type.

This decision threads through every per-activity step below.

### Step 1: Load context

Dispatch `project-manager`:

> Load `methodpoc/designs/<product>/project/network.yaml`.
>
> Verify:
>   - `chosen_option` is set (not null)
>   - `start_date` is set
>   - The system design is complete: `designs/<product>/system/architecture.dsl` exists
>   - The hand-off model is set: `designs/<product>/implementation/handoff.md` exists
>
> Also load:
>   - `designs/<product>/system/architecture.dsl` (architecture context)
>   - `designs/<product>/project/planning-assumptions.md` (constraints)
>   - `designs/<product>/implementation/handoff.md` (contract-design ownership)

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

Mark `next.status = "in-progress"` and `next.started_date = today` in
`network.yaml`. **Save before dispatching the role agent.**

### Step 3: Dispatch by activity type

#### If `next.type == "detailed-design"`

Invoke [[the-method-service-contract]]. The hand-off model from Step 0 determines who designs:

- **senior hand-off** → architect designs the contract; senior reviews and amends.
- **senior-as-junior-architect** → senior designs the contract under architect mentorship; architect reviews.
- **junior hand-off** → junior designs the contract; architect reviews.

The designer is dispatched first; the reviewer is dispatched on completion. Output goes to `designs/<product>/implementation/contracts/<component>.md`. Walk the Appendix B contract design rules and the Appendix C §6 standard check (3–5 ops per contract, max 12, reject ≥20).

#### If `next.type == "construction"`

Dispatch the implementer (`junior-developer` by default; `senior-developer` if the activity carries a `role: senior-developer` override):

> Implement `<next.component>` against
> `designs/<product>/implementation/contracts/<next.component>.md`.
> Activity: `<next.id> — <next.name>`. Duration estimate:
> `<next.duration_days>` days.
>
> System context: `methodpoc/designs/<product>/system/architecture.dsl`.
>
> Execute the activity. Write completion notes to
> `methodpoc/designs/<product>/implementation/log/<next.id>.md`.

On completion the senior (per the hand-off model) reviews the construction. Architect reviews are escalated for failed reviews or material design questions.

#### Other activity types

| `next.type` | Agent | Notes |
|---|---|---|
| `integration` | `system-architect` | Integration across components / layer boundaries |
| `quality` | `test-engineer` (out of POC scope — user executes manually) | |
| `noncoding` | `product-manager` or user | Research, requirements, deployment, training |

The dispatched agent gets the activity context (id, name, type, component, duration, completed dependencies + their notes) and writes completion notes to `methodpoc/designs/<product>/implementation/log/<next.id>.md`.

### Step 4: Verify and close

After the role agent returns:

- For `construction` activities: verify the build/tests pass for the affected product AND that the senior review (per hand-off model) is recorded in the log. If failing, **do not mark done**. Status stays `in-progress` and the user must fix.
- For `detailed-design` activities: verify the contract file exists at `implementation/contracts/<component>.md` and that the appropriate reviewer (per hand-off model) has signed off in the file. The Appendix C §6 contract checklist must pass.
- For `noncoding` / `integration` activities: verify the named output artifact exists.

If verified:

- Set `next.status = "done"`
- Set `next.completed_date = today`
- Append a one-line summary to `next.notes`
- Save `network.yaml`

If not verified:

- Keep `status = "in-progress"`
- Log the issue to `implementation/log/<next.id>.md`
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
> Write weekly report to
> `methodpoc/designs/<product>/implementation/log/week-<N>.md`. If a
> corrective action requires re-options (variance large enough to redesign),
> trigger Step 6 (scope-change).

### Step 6: Scope-change / variance event (triggered)

If during Step 4 a scope change request arrives, during Step 5 variance triggers a corrective action that requires re-options, or the user invokes `/sdp-review`, invoke [[the-method-scope-change]] (see also `/sdp-review`):

> The architect + project manager re-run project design to produce fresh
> options. Never silently absorb scope. Loop back to `/project-design`
> for a new SDP review.

After scope-change resolves and management commits to an updated option, resume Step 2 with the new `network.yaml`.

### Step 7: Loop or stop

If invoked with `--loop`:

- Pause for user review.
- Ask: "Continue?" If yes, go back to Step 2.

If `--once` (default):

- Stop here.

The loop terminates when all activities in `network.yaml` are `status == "done"`.

## Error handling

- **No `chosen_option`** → tell user management hasn't committed yet; can't execute.
- **No `handoff.md`** → run Step 0; cannot dispatch activities without it.
- **All activities done** → say "project complete," recommend `/sdp-review` for next subsystem or formal debrief.
- **All runnable activities require an agent the POC doesn't have** (ux-designer, test-engineer, devops) → tell user to execute manually and update `network.yaml` directly, then re-run.
- **Build/test failure on construction** → leave status `in-progress`, tell user, don't proceed.
- **Contract review fails Appendix C §6** → loop back into [[the-method-service-contract]]; do not mark detailed-design done.
- **Activity in progress crossed estimated duration significantly** → flag as schedule risk, surface to user, consider triggering Step 6 (scope-change) for re-options.
