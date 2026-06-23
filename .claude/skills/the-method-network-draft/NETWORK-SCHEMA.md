# network.yaml schema

The machine-readable project network. Source of truth for `/project-design`
output and `/implement-project` orchestration.

## File location

`designs/<product>/project/network.yaml`

## Schema

```yaml
project:
  product: <product>                  # matches a product key in the repo
  name: "Human-readable project name"
  status: not-started | in-progress | done
  chosen_option: normal | compressed | subcritical | <custom>
  start_date: YYYY-MM-DD              # set when management commits
  planning_assumptions:
    - "Core team in place day 1"
    - "Test environment available by day 30"
    - "..."

# All activities in the chosen option's network.
activities:
  - id: A001
    name: "Identify core use cases"
    type: noncoding | detailed-design | construction | integration | quality
    component: null | "ComponentName"   # references container ID in architecture.dsl
    duration_days: 5                     # quantum of 5
    role: product-manager | system-architect | project-manager | senior-developer | junior-developer | ux-designer | test-engineer | devops
    dependencies: []                     # list of activity IDs that must be `done` first
    float_days: 0                        # computed; 0 = critical path
    status: not-started | in-progress | done | blocked
    started_date: null | YYYY-MM-DD
    completed_date: null | YYYY-MM-DD
    notes: ""                            # free-form; updated during execution

  - id: A002
    name: "Design Order Manager contract"
    type: detailed-design
    component: OrderManager
    duration_days: 5
    role: senior-developer
    dependencies: [A001]
    float_days: 0
    status: not-started
    started_date: null
    completed_date: null
    notes: ""

  - id: A003
    name: "Build Order Manager"
    type: construction
    component: OrderManager
    duration_days: 10
    role: junior-developer
    dependencies: [A002]
    float_days: 0
    status: not-started
    started_date: null
    completed_date: null
    notes: ""

  # Milestone — a ZERO-DURATION EVENT node (marker), NOT an activity. It has no
  # resource and no risk (ch. 10 excludes zero-duration nodes from risk). Its
  # structural value is FORCED DEPENDENCY: collapsing an N×M layer fan-out into
  # N→milestone→M ("forced-dependency milestones simplify the network", ch. 11).
  # Fan-IN is the milestone's own `dependencies`; fan-OUT is expressed by
  # downstream activities listing the milestone id in THEIR `dependencies`. The
  # existing CPM forward/backward pass treats it like any node (event time =
  # max(predecessor EF); duration 0), so no new edge type is needed.
  - id: M1
    name: "Infrastructure Provisioned"
    type: milestone                      # zero-duration EVENT node
    duration_days: 0                     # REQUIRED 0 for milestone
    public: false                        # PUBLIC (demonstrate to mgmt) | PRIVATE (internal hurdle)
    role: null                           # milestones have no resource
    component: null                      # milestones reference no component
    dependencies: [R001, R002, R003]     # fan-IN (the upstream completing into it)
    on_critical_path: false              # computed; off-CP milestones MUST be private
    float_days: 0                        # computed; event-time slack, display-only (no risk)
    status: not-started
    notes: ""

# Earned value tracking — updated weekly during execution.
tracking:
  weeks:
    - week: 1
      date: YYYY-MM-DD
      planned_progress_pct: 5
      actual_progress_pct: 4
      planned_effort_days: 10
      actual_effort_days: 11
      activity_updates:
        - id: A001
          status: done
          notes: "Found 3 core use cases; one merged with another."
```

## Activity types

| Type | Description | Typical role |
|---|---|---|
| `noncoding` | Research, requirements, training, deployment planning | product-manager, devops, ux-designer |
| `detailed-design` | Design contracts for one component | senior-developer (or system-architect in junior hand-off) |
| `construction` | Implement one component | junior-developer (or senior-developer) |
| `integration` | Connect components, run integration tests | senior-developer + test-engineer |
| `quality` | Quality control (testing, hardening, code review) | test-engineer, senior-developer |
| `milestone` | Zero-duration EVENT node (marker). Forced-dependency simplifier; no resource, no risk. | (none — `role: null`) |

## Computed fields (server-derived; the read model carries these)

The authored inputs are `dependencies` (per activity) + `criticalPath` (activity
names). Every other figure is **CPM-derived server-side** (a forward/backward
pass over ActivityList × Network) and carried on the read model — the client is a
thin presenter and never recomputes. Per the founder gate (2026-06-19) the solve
lives as a Strategy on `constructionEstimationEngine`, computed **at read**.

Per-node computed block:

```yaml
    earliest_start:  0      # forward pass
    earliest_finish: 5
    latest_start:    0      # backward pass
    latest_finish:   5
    total_float:     0      # latest_start − earliest_start; 0 ⇒ on critical path
    free_float:      0
    on_critical_path: true  # in criticalPath[] OR total_float == 0
    near_critical:   false  # 0 < total_float ≤ NEAR_CRITICAL_THRESHOLD (5d)
    band: critical          # criticality band (Strategy/Policy): critical | high | medium | low
    column: 0               # topological depth (longest-path layer) for layout
```

Summary block (network-level):

```yaml
network_summary:
  total_duration_days: 0        # infinite-resource bound (max earliest_finish)
  critical_path_activity_count: 0
  critical_path_days: 0
  max_float: 0
  near_critical_count: 0
```

## Constraints (validated by `/project-design`)

| Rule | Check |
|---|---|
| Duration in 5-day quanta | `duration_days % 5 == 0` — **exempt `type == milestone`** (must be 0) |
| No god activities | `duration_days <= 35` |
| All deps exist | every `dependencies[*]` resolves to an activity in the file |
| Every activity has a resource | `role` is set — **exempt `type == milestone`** (`role: null`) |
| Every activity reaches the critical path | DAG analysis: no orphan branches |
| `component` set for coding activities | type ∈ {detailed-design, construction} ⇒ component non-null |
| `component` references real container | string matches an ID in `architecture.dsl` |
| Milestone is a zero-duration event | `type == milestone ⇒ duration_days == 0` and `public` is set |
| Off-CP milestones should be private (LINT, not hard fail) | `type == milestone` and `on_critical_path == false` and `public == true` ⇒ WARN. Allowed only when the milestone is an explicit public demo/release gate. A pure internal off-CP hurdle (e.g. M1 Infrastructure, M2 Managers) should be private. |
| `on_critical_path` is computed (FAN-OUT ⇒ standard float rule; MARKER ⇒ determining-predecessor rule) | NOT authored. PREFERRED: materialize the milestone with FAN-OUT (downstream nodes depend on it, replacing the milestone's fan-in subset — a clean no-op where the consumer's deps include exactly that subset). A materialized milestone then has real successors, so the STANDARD float rule applies directly: `on_critical_path == (total_float == 0)` from the real backward pass — and it lands on-CP exactly when it sits on the zero-float path (e.g. M3/M4: I-UC1..5→M3, N-IT→M4). FALLBACK for a milestone left as a pure fan-in MARKER (no clean fan-out — e.g. M1 à-la-carte infra, M5 terminal): use the DETERMINING-PREDECESSOR rule — on-CP iff the max-`earliest_finish` fan-in node (ties prefer on-CP) is on-CP, PLUS the t=0 root (M0) on and the terminal-at-`project_duration` milestone (M5) on; a post-v1 milestone (N-DOGFOOD) is off-CP regardless. The two rules AGREE for materialized milestones; the det-pred fallback exists only so a marker's on-CP reflects the achievement it gates rather than the slack of a dead-end sink. Do NOT apply the bare slack-sink float rule to a marker (its LF pins to project duration and wrongly drops on-CP markers). |
| Milestone event times CHAIN | the forward pass is milestone-aware: a milestone's `earliest_finish` = its event time = max over deps of (dep is activity ? EF(dep) : eventTime(dep)). A milestone may depend on another milestone (e.g. N-DOGFOOD → M5 ⇒ N-DOGFOOD event time = M5 event time). |
| Milestones excluded from risk | risk computation skips `type == milestone` nodes (zero-duration, ch. 10) |
| Role is a roster id | `role` ∈ team.ts roster ids (or null for milestones); no `devops` / `architect` / `*-agent` |

## How `/implement-project` uses this

```pseudocode
load network.yaml
runnable = [a for a in activities
            if a.status == "not-started"
            and all(d.status == "done" for d in a.dependencies)]
next = runnable.sorted_by(float_days_asc).first
# dispatch the agent that matches next.role
# on completion: mark next.status = "done", set completed_date
```

## How `/sdp-review` uses this

On scope change, the file is **regenerated** — old `network.yaml` is archived
to `project/network-v<N>.yaml` and replaced. Tracking history is preserved
across versions so progress isn't lost.
