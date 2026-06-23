---
name: the-method-network-draft
description: Project Design — convert activities into a project network. Compute total/free floats. Identify critical path. Architect designs; project-manager owns the file. Reads activities.md and planning-assumptions.md. Produces network.yaml (initial, no resource assignment yet). Invoke after [[the-method-activity-list]], before [[the-method-normal-solution]].
---

# Network Draft

The activity list becomes a directed graph of dependencies. Calculate floats. Identify the critical path. Output is `network.yaml` as the machine-readable spine of all downstream phases.

## Canonical source

**Primary:**
- Löwy, [Ch. 7 §7 "Critical Path Analysis"](../../../../rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07lev1sec7)
- [Ch. 7 §7.1 "Project Network"](../../../../rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07lev2sec11)
- [Ch. 7 §7.2 "The Critical Path"](../../../../rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07lev2sec12)
- [Ch. 8 §1 "The Network Diagram"](../../../../rightingsoftware/OEBPS/xhtml/ch08.xhtml#ch08lev1sec1) — node vs arrow diagrams
- [Ch. 8 §2 "Floats"](../../../../rightingsoftware/OEBPS/xhtml/ch08.xhtml#ch08lev1sec2) — total and free float

**Standard reference:** [Appendix C §4.5 "Project network"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec4) — items 5a–5j.

**Schema:** [NETWORK-SCHEMA.md](NETWORK-SCHEMA.md) (co-located with this skill)

## Input

- `methodpoc/designs/<product>/project/activities.md`
- `methodpoc/designs/<product>/project/planning-assumptions.md`

## Output

`methodpoc/designs/<product>/project/network.yaml` — the canonical machine-readable project network.

## Procedure

### Step 1 — Load activities

Parse `activities.md` into a list. For each, capture: id, name, type, component, duration_days, role, dependencies, status (set to `not-started`).

### Step 2 — Verify the dependency graph

Walk every dependency edge:

| Check | Action |
|---|---|
| Every dep id resolves to an actual activity | Fix typo or add missing activity |
| No cycles | If cycle detected, reconsider — dependencies should form a DAG |
| Every activity reaches the critical path | App C §5b — verify by tracing downstream reachability |
| Every activity has a resource (role) | App C §5c |

Treat **resource dependencies as dependencies** (App C §5a). If two activities share the only senior developer and run "in parallel," they're actually sequential — encode the resource dependency explicitly.

### Step 3 — Compute earliest start / earliest finish (forward pass)

For each activity in topological order:

```
ES(activity) = max(EF(dep) for dep in dependencies, default 0)
EF(activity) = ES(activity) + duration_days
```

Where ES = earliest start (day number from project day 0), EF = earliest finish.

### Step 4 — Compute latest start / latest finish (backward pass)

Project duration = `max(EF for all terminal activities)`.

For each activity in reverse topological order:

```
LF(activity) = min(LS(successor) for successor in successors, default project_duration)
LS(activity) = LF(activity) - duration_days
```

### Step 5 — Compute floats (ch. 8 §2)

For each activity:

```
total_float = LS - ES   (slack relative to project deadline)
free_float = min(ES(successor) for successor in successors) - EF(activity)   (slack without delaying any successor)
```

**Critical path** = chain of activities where total_float = 0.

Per App C §5h: *"Treat near-critical chains as critical chains."* Anything with total_float ≤ 5 days is near-critical. Flag.

### Step 6 — Choose diagram form (App C §5d–5e)

Prefer **arrow diagrams** over node diagrams for the human-readable view.

| Form | Pros | Cons |
|---|---|---|
| Arrow (preferred) | Activities on arrows, events on nodes — Critical Path Method (CPM) convention; clearer in print | Slightly less common in software tooling |
| Node | Activities on nodes, dependencies as arrows — Precedence Diagram Method (PDM) convention; common in MS Project | Visually denser; floats harder to read |

For the *YAML file* this is immaterial — the data is the same. The choice affects only the rendered diagram (in `sdp-review.md`).

### Step 7 — Write `network.yaml`

Per the schema in [NETWORK-SCHEMA.md](NETWORK-SCHEMA.md). Format:

```yaml
project:
  product: <product>
  name: "<Project Name>"
  status: not-started
  chosen_option: null               # set later by SDP review
  start_date: null                  # set later when management commits
  planning_assumptions_ref: "planning-assumptions.md"

activities:
  - id: A001
    name: "Detailed design — OrderManager"
    type: detailed-design
    component: OrderManager
    duration_days: 5
    role: senior-developer
    dependencies: []
    earliest_start: 0
    earliest_finish: 5
    latest_start: 0
    latest_finish: 5
    total_float: 0
    free_float: 0
    on_critical_path: true
    near_critical: true
    status: not-started
    started_date: null
    completed_date: null
    notes: ""

  - id: A002
    name: "Build OrderManager"
    type: construction
    component: OrderManager
    duration_days: 15
    role: junior-developer
    dependencies: [A001]
    earliest_start: 5
    earliest_finish: 20
    latest_start: 5
    latest_finish: 20
    total_float: 0
    free_float: 0
    on_critical_path: true
    near_critical: true
    status: not-started
    started_date: null
    completed_date: null
    notes: ""

  ...

network_metadata:
  computed_at: <YYYY-MM-DD>
  total_project_duration_days: <N>
  critical_path: [A001, A002, ..., AN]
  near_critical_chains:
    - [B001, B002, ...]
  total_activities: <count>
  warnings: []                       # any anti-pattern flags
```

### Step 8 — Sanity checks

Apply App C §4.5 + §5 final checks:

| Check | Action |
|---|---|
| All activities reside on a chain that starts and ends on a critical path | App C §5b — flag orphans |
| All activities have a resource assigned | App C §5c |
| Avoid god activities (duration > 35) | App C §5f — should have been caught in [[the-method-activity-list]]; double-check |
| Cyclomatic complexity ~10–12 | Count of independent chains; if higher, the network is too complex — consider subsystems |

### Step 9 — Add risk pre-flags

For each activity, pre-flag:

- **Bus-factor risk**: only one person can do this role and they're on the critical path
- **Dependency chain length**: long single-resource chains
- **Assumption-dependent activities**: activities that rely on a flagged planning assumption

Capture these in `network_metadata.warnings[]`. They feed [[the-method-risk-modeling]].

## Exit criteria (for router)

`network.yaml` exists with:
- All activities present with computed ES/EF/LS/LF/floats
- Critical path identified
- Near-critical chains flagged
- Sanity checks pass
- Pre-flagged risks captured in metadata.warnings

Move to `the-method-normal-solution`.

## Anti-patterns to reject

- **Activities with no successor and no critical-path participation** — orphan; merge or drop.
- **Activities with no resource** — unassignable; assign or drop.
- **God activities (> 35 days)** — split.
- **Single-resource long chains** — bus-factor risk; flag in warnings.
- **Cycles in dependency graph** — fix by removing false dependencies.

## Notes on "behavioral vs nonbehavioral" dependencies (ch. 13)

Per ch. 13 §2: distinguish:

- **Behavioral**: A must finish before B because B uses A's output (e.g., construction depends on detailed-design).
- **Nonbehavioral**: A must finish before B because of resource sharing (e.g., the only senior dev does A then B).

Both encoded as `dependencies[]` in YAML. Both block the network. Override carefully — if you remove a nonbehavioral dependency, you must ensure resource is actually available (different person, same role).

App C §5a: *"Treat resource dependencies as dependencies."* Do not pretend resource conflicts away.
