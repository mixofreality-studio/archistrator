---
name: the-method-normal-solution
description: Project Design — design the normal solution. Minimum staffing for unimpeded critical path progress. Assign resources by float — critical path first, best resources first. Reads the committed network and planningAssumptions artifacts in project.json. Produces the typed NormalSolution committed to project.json → .normalSolution (carries its own resource-assigned network state). Invoke after [[the-method-network-draft]], before [[the-method-subcritical-solution]].
---

# Normal Solution

The normal solution is the *minimum-cost option*: the smallest team that can progress unimpeded along the critical path. It is the baseline against which all other options are compared.

Per ch. 11: this is not the "best" solution — it's the *natural* solution. Other options will compress (faster, more expensive, riskier) or extend (cheaper-looking but actually riskier).

## Canonical source

**Primary:**
- Löwy, [Ch. 11 §3 "Finding the Normal Solution"](../../../research/rightingsoftware/OEBPS/xhtml/ch11.xhtml#ch11lev1sec3)
- [Ch. 11 §3.4 "Choosing the Normal Solution"](../../../research/rightingsoftware/OEBPS/xhtml/ch11.xhtml#ch11lev2sec10)

**Resource assignment rules:**
- [Ch. 7 §7.3 "Assigning Resources"](../../../research/rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07lev2sec13)
- [Ch. 8 §3 "Floats-Based Scheduling"](../../../research/rightingsoftware/OEBPS/xhtml/ch08.xhtml#ch08lev1sec3)

**Cost calculation:**
- [Ch. 7 §9 "Project Cost"](../../../research/rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07lev1sec9)
- [Ch. 7 §9.1 "Project Efficiency"](../../../research/rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07lev2sec15)

**Earned value:**
- [Ch. 7 §10 "Earned Value Planning"](../../../research/rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07lev1sec10)
- [Ch. 7 §10.2 "The Shallow S Curve"](../../../research/rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07lev2sec17)

**Standard reference:** [Appendix C §4.2 "Staffing"](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec4) — lowest staffing for unimpeded critical path, assign by float, shallow S curve.

## Input

State is git-as-DB: all of this lives in `.aiarch/state/project.json` (a typed JSON aggregate), NOT in `designs/<product>/*.md` or `network.yaml` files. Markdown/DSL/YAML is a render-on-read of the typed state, never the source of truth.

- The committed **network** artifact in `project.json` → `.network` (from [[the-method-network-draft]])
- The committed **planningAssumptions** artifact in `project.json` → `.planningAssumptions`

## Output

The normal solution is a **typed model committed into `.aiarch/state/project.json` → `.normalSolution`** — git is the database. It carries this option's own resource-assigned network state (the base `.network` plus per-activity `resource:` assignments for this option), plus the summary metrics, staffing distribution, S-curve, and costs. It is NOT a `normal.md` file and it does NOT mutate the shared `.network` slot in place; the resource assignments live inside `.normalSolution`. Any markdown (including the Step 8 template) is a render-on-read of that JSON.

Two usage patterns produce this slot:

1. **Agentic/CI dispatch:** the agent produces the typed `NormalSolution` model as JSON and commits it into `.normalSolution` on its session branch; the server reads it back and stages it (`StageArtifactForReview`) for the human review gate (`CommitArtifact` / `RejectArtifact`).
2. **Local interactive:** same — produce the typed model and write it into the `.normalSolution` slot. Never a `designs/*.md` file.

## Procedure

### Step 1 — Identify the minimum staffing level

Per App C §4.2c: *"Ask for only the lowest level of staffing required to progress unimpeded along the critical path."*

Walk the critical path. At every point in time (day-by-day), count the number of distinct resources needed to keep critical activities running. The maximum over all days is your minimum staffing level for the critical path.

Then check non-critical activities: do you have enough resource left over to start them when their float allows? If yes, you're staffed correctly. If no, add resources only as required — never more.

Example (illustrative):

- Critical path: A001 → A002 → A012 → A013 → A040 → ...
- Day 0–5: A001 needs senior-dev. 1 senior-dev required.
- Day 5–20: A002 needs junior-dev. 1 junior-dev required.
- Day 5–10: A003 (Engine design) needs senior-dev (different person from A001 done at day 5? same? check resource dependencies)
- Day 20–35: ...

Walk through and arrive at: e.g., "2 senior-devs, 3 junior-devs, 1 test-engineer for half the project, 1 ux-designer for 25 days."

### Step 2 — Assign actual resources by float (App C §4.2d)

Per Löwy: *"Always assign resources based on float."*

1. **Critical path first, best resources first.** Activities with float = 0 get the most reliable, most trustworthy developers. The book calls out the classic mistake: *"assigning developers to high-visibility but noncritical activities... slowing down the critical path absolutely slows down the project"* (ch. 7 §7.3).
2. **Near-critical chains next.** Float ≤ 5 days = near-critical (App C §5h).
3. **High-float activities last.** Larger float = more scheduling room = lower-risk to assign less senior people.

In this option's network state inside `.normalSolution`: for each activity, populate a `resource` field with a specific identifier (e.g., `senior-dev-1`, `junior-dev-2`, `test-engineer-1`). Keep `role` (the type); add specific `resource` (the assignment).

```yaml
activities:
  - id: A001
    role: senior-developer
    resource: senior-dev-1            # specific assignment for normal solution
    on_critical_path: true
    ...
```

App C §4.2g: *"Always assign components to developers in a 1:1 ratio."* One developer per component (per construction activity). Don't have two juniors share OrderManager — assign one, integrate with peer review.

App C §4.2h: *"Strive for task continuity."* The same developer should do construction in subsequent related activities where possible (e.g., A002 → A005 → A040 stays with junior-dev-2 if their availability allows). Reduces context-switch tax.

### Step 3 — Compute the staffing distribution

For each day from start to project_duration, list which resources are working on which activities. Aggregate into a per-day headcount.

Visualize as a staffing distribution chart (in the `.normalSolution` rendering use a Mermaid `gantt` or simple ASCII histogram):

```
Headcount over time (normal solution)

         ramp-up           plateau            taper
        |---------|---------------------|-----------|
   5 |                ████████████████████████
   4 |          ████████████████████████████████████
   3 |    ██████████████████████████████████████████████
   2 |██████████████████████████████████████████████████████
   1 |█████████████████████████████████████████████████████████
        Day 0       20      40      60      80     100
```

**Healthy shape:** ramp-up → plateau → taper. App C §4.2e: *"Ensure correct staffing distribution."* Steep ramp-up = onboarding chaos. Flat hire-and-hold = inefficiency.

### Step 4 — Compute planned earned value (S curve)

Per ch. 7 §10. The earned-value curve is cumulative percent-complete over time, weighted by effort.

```
EV(day) = sum(duration_days[a] for a in activities if EF[a] ≤ day) / sum(duration_days[a] for a in activities)
```

Plot from day 0 (EV=0%) to project_duration (EV=100%).

The curve should be a **shallow S**. Per App C §4.2f: *"Ensure a shallow S curve for the planned earned value."* If the curve is steep at start (a lot of parallel work early) or steep at end (rush finish), the plan is brittle.

Visualize in the `.normalSolution` rendering. Use ASCII or Mermaid line plot.

### Step 5 — Compute costs (ch. 7 §9)

- **Direct cost** = sum of activity person-days, treating each person-day as cost. Per Löwy: typically measured in *man-months*, not currency, to enable cross-project comparison.
- **Indirect cost** = duration × overhead burn rate. Overhead = PM, architect, ProdMgr, DevOps, infrastructure — everything not tied to an activity.
- **Total cost** = direct + indirect.

```
direct_cost = sum(duration_days for all activities) / 20    # man-months at 20 working days
indirect_cost = duration_days * overhead_burn_rate_man_months_per_day
total_cost = direct_cost + indirect_cost
```

Define overhead burn rate from `planning-assumptions.md` (core team headcount, dev tooling, etc.).

### Step 6 — Compute efficiency

```
efficiency = direct_cost / (direct_cost + indirect_cost)
```

Per App C §4.6f: *"Avoid projects with efficiency higher than 25%."* Higher efficiency means too-lean staffing (no slack); the project is brittle. Lower efficiency (10–20%) is normal and healthy.

If efficiency > 25% in the normal solution, you have too few resources for the work — your "normal" is actually under-staffed and effectively compressed. Reconsider.

### Step 7 — Compute risk (preliminary)

This is a placeholder; full risk modeling lives in [[the-method-risk-modeling]]. For the normal solution, compute:

- **Criticality risk** = (count of activities on critical path) / (total activities). Normalize to 0–1.
- Note any single-resource long chains (bus-factor risk).

Final risk modeling happens in `[[the-method-risk-modeling]]`. Decompression of the normal solution into its own option (decompressed-normal) is `[[the-method-decompressed-solution]]`.

### Step 8 — Commit the typed normal solution to `.normalSolution`

Produce the typed `NormalSolution` model and commit it to `.aiarch/state/project.json` → `.normalSolution`. The markdown below is the equivalent **human rendering** — use it to review the solution, but the source of truth is the slot, not a `normal.md` file:

```markdown
# Normal Solution — <Product>

## Summary
- Duration: N days (≈ N/20 months)
- Total cost: <direct + indirect> man-months
- Direct cost: N man-months
- Indirect cost: N man-months
- Efficiency: N%
- Peak staffing: N
- Preliminary risk: N

## Critical path
A001 → A002 → A012 → A013 → ... → AN
Total critical-path duration: N days

## Resource assignments
| Activity | Resource | Days | Float |
|---|---|---|---|
...

## Staffing distribution
<ASCII or Mermaid chart>

## Planned earned value (S curve)
<ASCII or Mermaid plot>
- Shape: shallow S ✓ / steep / flat

## Costs
| Component | Cost (man-months) |
|---|---|
| Direct (activities) | N |
| Indirect (overhead × duration) | N |
| Total | N |

## Assumptions used
- (reference the committed `.planningAssumptions` slot)
- Specific assumptions exercised in this solution: ...

## Risk flags
- ...
```

## Exit criteria (for router)

- `.aiarch/state/project.json` → `.normalSolution` holds a committed typed model with all sections
- The `.normalSolution` network state carries `resource:` assignments for this option
- Efficiency ≤ 25%
- S curve is shallow

Move to `the-method-subcritical-solution`.

## Anti-patterns to reject

- **Over-staffing the normal solution** ("just in case") — that's not normal, that's compressed. The point of normal is to discover the *floor*.
- **Steep S curve** — indicates lumpy parallel work; smooth the schedule.
- **Efficiency > 25%** — under-staffed; the "normal" plan is already brittle.
- **Single-resource critical-path chains** — flag as risk; consider cross-training as an enabling activity in [[the-method-activity-list]].
- **Ignoring task continuity** — assigning related activities to different developers creates context-switch overhead.
