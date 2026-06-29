---
name: the-method-compressed-solution
description: Project Design — design the compressed solution. Shorter duration via parallel work first, top resources second. Target ≤30% compression. Stops at death zone. Reads the committed normalSolution, network, planningAssumptions artifacts in project.json. Produces the typed CompressedSolution committed to project.json → .compressedSolution. Invoke after [[the-method-subcritical-solution]], before [[the-method-risk-modeling]].
---

# Compressed Solution

The compressed solution offers management a "go faster" option. It costs more (parallel work or top resources are not free). It is riskier (parallel work increases coordination overhead and execution risk). But it shortens delivery — sometimes by enough to justify the trade.

Rules from the book: parallel work *first*, top resources *second*, never below the death zone, never beyond 30% compression.

## Canonical source

**Primary:**
- Löwy, [Ch. 9 §1 "Accelerating Software Projects"](../../../research/rightingsoftware/OEBPS/xhtml/ch09.xhtml#ch09lev1sec1)
- [Ch. 9 §2 "Schedule Compression"](../../../research/rightingsoftware/OEBPS/xhtml/ch09.xhtml#ch09lev1sec2)
- [Ch. 9 §3 "Time-Cost Curve"](../../../research/rightingsoftware/OEBPS/xhtml/ch09.xhtml#ch09lev1sec3)
- [Ch. 9 §4 "Avoiding Classic Mistakes"](../../../research/rightingsoftware/OEBPS/xhtml/ch09.xhtml#ch09lev1sec4) — death zone
- [Ch. 9 §6 "Network Compression"](../../../research/rightingsoftware/OEBPS/xhtml/ch09.xhtml#ch09lev1sec6)
- [Ch. 11 §4 "Network Compression"](../../../research/rightingsoftware/OEBPS/xhtml/ch11.xhtml#ch11lev1sec4) — worked example with iterations

**Standard reference:** [Appendix C §4.6 "Time and cost"](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec4) — items 6a–6g.

## Input

State is git-as-DB: all of this lives in `.aiarch/state/project.json` (a typed JSON aggregate), NOT in `designs/<product>/*.md` or `network.yaml` files. Markdown/DSL/YAML is a render-on-read of the typed state, never the source of truth.

- The committed **normalSolution** artifact in `project.json` → `.normalSolution` (the baseline being compressed, with its resource-assigned network state) and the committed **network** artifact → `.network`
- The committed **planningAssumptions** artifact in `project.json` → `.planningAssumptions`

## Output

The compressed solution is a **typed model committed into `.aiarch/state/project.json` → `.compressedSolution`** — git is the database. It carries this option's own recomputed network state (compression extras and resource swaps), summary metrics, and the comparison-to-normal table. It is NOT a `compressed.md` file; any markdown (including the Step 7 template) is a render-on-read of that JSON slot.

Two usage patterns produce this slot:

1. **Agentic/CI dispatch:** the agent produces the typed `CompressedSolution` model as JSON and commits it into `.compressedSolution` on its session branch; the server reads it back and stages it (`StageArtifactForReview`) for the human review gate (`CommitArtifact` / `RejectArtifact`).
2. **Local interactive:** same — produce the typed model and write it into the `.compressedSolution` slot. Never a `designs/*.md` file.

## Procedure

### Step 1 — Apply quick-and-clean first (App C §4.6a)

Per App C: *"Accelerate the project first by quick and clean practices rather than compression."*

Before reaching for compression techniques, check:

| Quick-and-clean lever | Action |
|---|---|
| Are there hidden dependencies you can remove? | Re-examine each dependency in the committed `.network` slot |
| Can the team skip activities that don't add value? | Drop them (rare; usually each was added intentionally) |
| Can specialists replace generalists? | Hire / engage an expert per ch. 9 §1 |
| Is there parallel work the team simply hadn't planned? | Surface and add |
| Are activity estimates inflated by safety padding? | Right-size (don't crash; right-size) |

If quick-and-clean reduces duration meaningfully, use that as your compressed baseline before applying real compression.

### Step 2 — Compress via parallel work (App C §4.6c — preferred)

Per Löwy: *"Compress with parallel work rather than top resources."* Why: parallel work changes the *network* without changing per-activity cost much. Top-resource compression is paying premium for the same activity.

Parallel work mechanics:

1. **Split serial chains.** A → B → C might become {A1, A2} in parallel → {B1, B2} in parallel → C. Requires extra resources but breaks the chain.
2. **Pipeline.** Start B as soon as enough of A is done to unblock B (intra-activity dependency). Requires either splitting A into sub-activities or accepting "soft" dependency.
3. **Enabling activities.** Add activities like "build simulator for component X" so downstream work isn't blocked waiting for X's completion. Per ch. 9, simulators are a classic acceleration technique.
4. **Pre-work for noncoding.** UX design can start before architecture is complete (with risk); infra provisioning can run in parallel with early construction.

For each parallel-work change:
- Add the new activities to this option's network state inside `.compressedSolution` (likely under a `compressed_extras:` list)
- Add the resources needed
- Recompute network

### Step 3 — Compress via top resources (App C §4.6d — secondary)

Per Löwy: *"Compress with top resources carefully and judiciously."*

If parallel work alone doesn't hit the compression target, swap in higher-skilled resources on critical-path activities.

How: top resources have lower duration on the same activity (they're faster). Update the activity's `duration_days` based on the resource's effective throughput multiplier (typically 1.3–1.5× for top-tier).

**Cost implications:** Top resources are more expensive per day. Direct cost rises. Sometimes substantially.

Apply sparingly: only on critical-path activities. Apply broadly and you've inflated direct cost without buying much (high float activities don't accelerate the project).

### Step 4 — Iterate compression

Per ch. 11, compression is iterative. Each iteration:

1. Identify the current critical path.
2. Apply one compression technique to one or more critical activities.
3. Recompute the network.
4. The critical path likely shifts (a previously near-critical chain becomes critical).
5. Repeat on the new critical path.

Stop when:
- You hit the **compression target** (typical: 20–30% reduction from normal).
- You hit the **30% cap** (App C §4.6e: *"avoid compression higher than 30%"*).
- You hit the **death zone** (App C §4.6b: *"never commit to a project in the death zone"*).
- You hit the **efficiency ceiling** (App C §4.6f: efficiency > 25% means brittle).

### Step 5 — Identify the death zone (Ch. 9 §4 — critical)

The death zone is the region of the time-cost curve where any further compression is infeasible at any cost. Beyond it, no amount of money or people can deliver.

Per ch. 9: characterize the death zone via the time-cost curve (full curve built in [[the-method-risk-modeling]]). For now:

- Plot points: each compression iteration gives a (duration, total_cost) point.
- As you compress further, cost rises but duration reduction plateaus.
- The death zone is the asymptote — duration cannot go below a hard minimum no matter what cost you accept.

If your current compressed solution lies in the death zone region, **back off**. Per App C §4.6b: this is a non-negotiable rule.

### Step 6 — Compute final compressed metrics

After iteration converges:

| Metric | Compressed value |
|---|---|
| Duration | N days |
| Direct cost | N man-months (higher than normal — parallel work and/or top resources added) |
| Indirect cost | N man-months (lower than normal — duration is shorter) |
| Total cost | direct + indirect (usually higher than normal, despite indirect drop) |
| Efficiency | N% (verify ≤ 25%) |
| Peak staffing | N (higher than normal due to parallel work) |
| Compression vs normal | N% (verify ≤ 30%) |

App C reminder: compress the project *even if the likelihood of pursuing any of the compressed options is low* (§4.6g). The exercise reveals the time-cost shape, which informs every other option.

### Step 7 — Commit the typed compressed solution to `.compressedSolution`

Produce the typed `CompressedSolution` model and commit it to `.aiarch/state/project.json` → `.compressedSolution`. The markdown below is the equivalent **human rendering** — use it to review the solution, but the source of truth is the slot, not a `compressed.md` file:

```markdown
# Compressed Solution — <Product>

## Summary (vs. normal)

| Metric | Normal | Compressed | Delta |
|---|---|---|---|
| Duration | 120 days | 90 days | -25% ✓ within cap |
| Direct cost | 24 mm | 32 mm | +33% (parallel work + top resources) |
| Indirect cost | 12 mm | 9 mm | -25% (shorter duration) |
| **Total cost** | **36 mm** | **41 mm** | **+14%** |
| Efficiency | 17% | 22% | within cap ✓ |
| Peak staffing | 5 | 8 | parallel work needed |
| Preliminary risk | 0.45 | 0.62 | higher |
| Compression from normal | — | 25% | within 30% cap ✓ |
| Death zone | — | No | ✓ |

## Compression techniques applied

### 1. Parallel work
- Split OrderManager construction into two pipelines (workflow + state-handling) — added junior-dev-4
- Pipeline UX design with first construction activities — added soft dependency
- Built a Pricing Engine simulator so downstream RA work didn't wait

### 2. Top resources
- Assigned senior-dev-1 (top-tier) to A012 (Manager construction) instead of junior — duration 15 → 10 days

## New critical path
A001 → A002p (split) → A012 → ... → AN
Total: 90 days

## Resource assignments
| Activity | Resource | Days | Float |
|---|---|---|---|
...

## Staffing distribution
<chart — shows higher peak than normal>

## Planned earned value
<chart — steeper but still shallow-S>

## Costs (man-months)
| Cost | Compressed |
|---|---|
| Direct | 32 |
| Indirect | 9 |
| Total | 41 |

## Risk flags
- Parallel pipelines increase coordination overhead
- Bus-factor on top-tier senior-dev-1 (now on critical path)
- Soft UX-construction dependency: risk if UX runs late

## When to choose compressed over normal
- Hard deadline that normal would miss
- Revenue/strategic value of earlier delivery > N man-months in extra cost
- Team genuinely has the bench depth for parallel pipelines
```

## Exit criteria (for router)

`.aiarch/state/project.json` → `.compressedSolution` holds a committed typed model with:
- Comparison table showing duration shorter, total cost higher
- Compression ≤ 30%
- Efficiency ≤ 25%
- Not in death zone
- New critical path identified
- Compression techniques explicitly named

Move to `the-method-risk-modeling`.

## Anti-patterns to reject

- **Compression > 30%** — App C hard rule. Back off.
- **Death zone** — App C hard rule. Back off.
- **Top resources everywhere** — pays premium for activities with float. Apply only to critical path.
- **Parallel work without resource backing** — pretending two juniors can work on one component simultaneously (App C §4.2g: 1:1 component-to-developer).
- **Indirect cost ignored** — compression saves indirect but costs direct. Show both.
- **Skipping the death-zone check** — non-negotiable per App C.
