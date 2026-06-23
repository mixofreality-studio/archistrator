# SDP Review

> Re-run project design when scope changes or major variance is detected. Produces fresh options for management. Never silently absorb scope changes — per App A, the architect + project manager always re-design and offer options.

**This command is the entry point for [[the-method-scope-change]]. The procedure below mirrors that skill.**

**Skill reference:** Invoke `the-method` skill. Sub-skill: [[the-method-scope-change]].

## Usage

```
/sdp-review <product>
```

## When to run

- A scope change request arrives (new feature, removed feature, deadline shift)
- Weekly tracking ([[the-method-project-tracking]]) detects significant variance (App A patterns)
- A subsystem is finished and the next subsystem starts
- After a `/add-use-case` that meaningfully impacts the plan
- A resource change (someone leaves, new hire arrives)

## Workflow

Invoke [[the-method-scope-change]] and walk its procedure.

### Step 1: Capture the trigger

Ask the user:

1. "What changed? (scope addition, scope reduction, deadline shift, resource change, variance detected, subsystem boundary)"
2. "Who's asking? (customer, management, project tracking)"
3. "What's the current state? (point me at `network.yaml`; I'll read it)"

### Step 2: Snapshot the current state

Dispatch `project-manager`:

> Read current `designs/<product>/project/network.yaml`.
>
> Compute:
>   - Activities done so far (with completed_dates)
>   - Activities in-progress (with started_dates and remaining duration)
>   - Activities still not-started
>   - Current actual vs. planned progress
>   - Earned value so far
>
> Archive current network.yaml to `network-v<N>.yaml` where N is the next
> unused version number.

### Step 3: Apply the change

Branch by trigger type:

#### Scope addition (new use case beyond original spec)

1. Dispatch `product-manager` to formalize the new use case (same as `/add-use-case` Step 1).
2. Dispatch `system-architect` to determine if it fits the existing decomposition.
3. If fits → add new activities to a new `network.yaml`.
4. If doesn't fit → flag potential architecture change; recommend a partial `/system-design` first.

#### Scope reduction

1. Identify which activities can be cut.
2. Verify no in-progress activity depends on the cut work (handle gracefully if so).
3. Recompute the network without the cut activities.

#### Deadline shift

1. Inputs: new target date.
2. Dispatch `system-architect` + `project-manager` to find an option that meets it.
3. If only achievable in death zone → tell user explicitly: not feasible.

#### Resource change (someone leaves, new hire arrives)

1. Update the resource pool.
2. Re-assign activities by float — critical path first, best resources first.
3. Recompute timeline + risk.

#### Variance detected (App A pattern)

1. Apply the App A corrective action for the pattern:
   - Underestimating: push deadline OR reduce scope (never add people)
   - Resource leak: escalate to common manager
   - Overestimating: release resources
2. Produce updated options that reflect the chosen remedy.

### Step 4: Re-run options

This step loops back through the relevant `/project-design` sub-skills with `system-architect` + `project-manager`:

> Produce updated options from the new state. Run [[the-method-normal-solution]],
> [[the-method-decompressed-solution]], [[the-method-subcritical-solution]],
> and [[the-method-compressed-solution]] against the updated network. Then
> run [[the-method-risk-modeling]] against all four.
>
> Preserve all completed activities as `done`. Reset estimates only for
> not-started activities.

### Step 5: Write new SDP

Invoke [[the-method-sdp-review]] via `system-architect`:

> Write a fresh `designs/<product>/project/sdp-review.md` (overwrite). It
> must clearly state:
>   - The trigger for this review (one sentence)
>   - What changed since the prior SDP (concise diff)
>   - The new options (up to four) with duration, cost, criticality risk,
>     activity risk
>   - The architect's recommendation
>   - The cost of *not* making a change (i.e., what happens if management
>     rejects all options)

Then re-run [[the-method-project-design-standard-check]] to validate the new plan before handing it back to management.

Also overwrite `designs/<product>/project/network.yaml` with the new
network. The archive in `network-v<N>.yaml` preserves history.

### Step 6: Present and commit

Show user:

- The updated SDP options
- What the scope change costs them (in days and $)
- What the alternatives look like
- The archive file name so they can compare

Tell user: *"Present this to management. Once they pick an option, update
`network.yaml`'s `chosen_option` and `start_date`, then resume
`/implement-project`."*

## Building trust

App A is explicit: never silently absorb changes. Show management
projections regularly. Demonstrate the ability to detect problems early.
Insist on corrective actions. Over time, this builds trust and earns the
team autonomy.

`/sdp-review` is the formal mechanism for this. Use it visibly and often.

## Error handling

- **Change makes all options infeasible** → tell user, suggest constraint relaxation.
- **In-progress activity affected by scope cut** → flag the wasted work, recommend graceful completion or abandonment with reason.
- **No tracking data exists yet** → this is really a `/project-design`, not a `/sdp-review`. Redirect.
- **Project standard check fails after re-options** → loop back into the offending project-design sub-skill; do not hand to management yet.
