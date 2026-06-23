# Project Design

> Walk the user through project design with The Method: design the project that will build the system. Produce four viable options (normal, decompressed-normal, subcritical, compressed) so management can make an educated decision. Output the SDP review document and a machine-readable `network.yaml`.

**Skill reference:** Invoke `the-method` skill. This command orchestrates the project-design sub-skills in canonical order:

1. [[the-method-planning-assumptions]]
2. [[the-method-activity-list]]
3. [[the-method-network-draft]] — companion: `methodpoc/.claude/skills/the-method-network-draft/NETWORK-SCHEMA.md`
4. [[the-method-normal-solution]]
5. [[the-method-subcritical-solution]]
6. [[the-method-compressed-solution]]
7. [[the-method-decompressed-solution]]
8. [[the-method-risk-modeling]]
9. [[the-method-sdp-review]]
10. [[the-method-project-design-standard-check]]

## Usage

```
/project-design <product>
```

## Prerequisites

`designs/<product>/system/architecture.dsl` must exist. If it doesn't, stop
and tell the user to run `/system-design <product>` first.

## Workflow

### Step 1: Planning assumptions

Invoke [[the-method-planning-assumptions]]. Ask the user **one at a time**:

1. "Who's on the core team? Architect, project manager, product manager — same person, different people?"
2. "What's the available developer pool? How many senior, how many junior?"
3. "Test engineers? DevOps? External experts (UX, security, DBA)?"
4. "Hard constraints? A deadline you can't slip? A budget ceiling?"
5. "Existing infrastructure or do we build from zero?"
6. "Vacation/holiday calendar over the planning window?"
7. "Other projects competing for the same people?"

Write to `designs/<product>/project/planning-assumptions.md`.

### Step 2: Activity list

Invoke [[the-method-activity-list]] via `system-architect`:

> Read `designs/<product>/system/architecture.dsl` and
> `designs/<product>/project/planning-assumptions.md`. Produce
> `designs/<product>/project/activities.md`:
>
> For each component:
>   - One `detailed-design` activity (5–10 days, senior-developer)
>   - One `construction` activity (5–35 days, junior-developer)
>
> Plus integration activities at each layer boundary.
>
> Plus noncoding activities (ask product-manager for UX scope, ask user for
> infra/training/hardening/deployment activities).
>
> Every activity:
>   - Duration in 5-day quanta, ≤35 days (no god activities)
>   - Type: detailed-design | construction | integration | quality | noncoding
>   - Component (if coding) or null
>   - Behavioral dependencies (what must finish first?)
>
> Output a table, do not write to network.yaml yet.

Show table to user. Iterate.

### Step 3: Network draft

Invoke [[the-method-network-draft]] via `project-manager` with the finalized activity list:

> Write `designs/<product>/project/network.yaml` per the schema in
> `methodpoc/.claude/skills/the-method-network-draft/NETWORK-SCHEMA.md`.
> Initial status of all activities: `not-started`.
>
> Compute total floats for every activity (longest path - earliest start -
> duration). Identify the critical path.
>
> Add resource dependencies as dependencies (treat them as real
> dependencies per App C).

### Step 4: Design options

#### Normal solution

Invoke [[the-method-normal-solution]] via `system-architect` + `project-manager`:

> Minimum staffing for unimpeded progress along the critical path. Assign
> resources by float — critical path first, best resources first.
> Compute: duration, direct cost (sum of effort), indirect cost (duration
> × overhead burn rate), total cost, peak staffing, efficiency.
>
> Write to `designs/<product>/project/normal.md` with the network +
> staffing distribution + planned earned value (S-curve).

#### Subcritical solution

Invoke [[the-method-subcritical-solution]] via `system-architect` + `project-manager`:

> Same network, but with 1–2 fewer developers than normal. Activities
> serialize that could've been parallel. Result: longer, costlier, and
> riskier than normal (disproves the "fewer people = cheaper" intuition).
>
> Write to `designs/<product>/project/subcritical.md`.

#### Compressed solution

Invoke [[the-method-compressed-solution]] via `system-architect` + `project-manager`:

> Add parallel work, top resources on critical activities, activity
> changes to substitute faster paths. Target ~20–30% schedule reduction
> from normal.
>
> Avoid compression > 30% (Design Standard, App C).
> Stop if efficiency > 25% or risk > 0.75.
>
> Write to `designs/<product>/project/compressed.md`.

#### Decompressed-normal solution

Invoke [[the-method-decompressed-solution]] via `system-architect` + `project-manager`:

> Start from the normal solution. Deliberately extend duration to drop
> criticality risk toward the tipping point (~0.5) without consuming the
> float, by reducing staff. The point is a sibling option to normal that
> trades a small schedule increase for substantially lower risk.
>
> Write to `designs/<product>/project/decompressed.md`.

### Step 5: Risk modeling (all four options)

Invoke [[the-method-risk-modeling]] via `project-manager`:

> For each of the four options (normal, decompressed, subcritical,
> compressed) compute:
>   - Criticality risk (normalized count of critical activities)
>   - Activity risk (weighted by float distribution)
>
> Plot the time-cost curve and the time-risk curve.
>
> Apply exclusion zones:
>   - Reject options with risk < 0.3 (overstaffed/wasteful) or > 0.75
>     (too risky).
>   - Reject options in the death zone (below the time-cost curve).
>
> Write to `designs/<product>/project/risk.md`.

### Step 6: SDP Review document

Invoke [[the-method-sdp-review]] via `system-architect`:

> Audience: management decision-makers. Write
> `designs/<product>/project/sdp-review.md` including:
>   - Executive summary (2–3 sentences with architect's recommendation)
>   - Mission / vision recap (from system/mission.md)
>   - Architecture overview (link to architecture.dsl; embed one image of static view if available)
>   - Options table: each row = one of the four options, columns = duration, total cost, peak staff, criticality risk, activity risk
>   - Time-cost curve and time-risk curve (from risk.md)
>   - Planning assumptions list (link to planning-assumptions.md)
>   - Recommendation (architect's pick — but management chooses)

### Step 7: Project Design Standard check (final gate)

Invoke [[the-method-project-design-standard-check]] via `system-architect`:

> Walk Appendix C §4 Project Design Guidelines against the Phase 2
> artifacts. Each item passes, is waived with explicit justification, or
> sends you back to fix.
>
> Output `designs/<product>/project/project-standard-checklist.md`.

This is the final gate before Phase 3 / construction.

### Step 8: Present

Show the user:

```
designs/<product>/project/
├── planning-assumptions.md
├── activities.md
├── network.yaml
├── normal.md
├── subcritical.md
├── compressed.md
├── decompressed.md
├── risk.md
├── sdp-review.md
└── project-standard-checklist.md
```

Tell user: *"Project design complete. Take `sdp-review.md` to management.
Once an option is chosen, set `chosen_option` in `network.yaml` and run
`/implement-project <product>`."*

## Error handling

- **System design missing** → stop, point at `/system-design`.
- **All options in death zone** → project not feasible with constraints. Tell user explicitly. Suggest relaxing a constraint.
- **All options risk > 0.75** → either decompress aggressively (decompressed-normal is the lever), or the project as scoped is too risky to start.
- **A single resource on the critical path** → flag as a bus-factor risk, recommend cross-training as an enabling activity.
- **Standard check fails and can't be waived** → loop back to the offending earlier skill; do not proceed to Phase 3.
