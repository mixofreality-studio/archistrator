---
name: the-method-subcritical-solution
description: Project Design — design the subcritical solution. Deliberately understaffed. Counterintuitively LONGER, COSTLIER, and RISKIER than normal. The point is to disprove the "fewer people = cheaper" intuition for management. Reads normal.md, network.yaml, planning-assumptions.md. Produces subcritical.md. Invoke after [[the-method-normal-solution]], before [[the-method-compressed-solution]].
---

# Subcritical Solution

A subcritical solution removes 1–2 developers from the normal solution. Many managers reflexively assume this saves money. The book's whole point of producing this option is to **prove that intuition wrong**: subcritical is longer, costlier in total (because indirect cost piles up over the extended duration), AND riskier.

You produce it for the SDP review so management can see the trap and reject it.

## Canonical source

**Primary:** Löwy, [Ch. 11 §3.3 "Going Subcritical"](../../../../rightingsoftware/OEBPS/xhtml/ch11.xhtml#ch11lev2sec9) — the worked example with the 7th iteration of the normal-solution search.

**Supporting:**
- [Ch. 7 §9 "Project Cost"](../../../../rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07lev1sec9) — indirect cost behavior over time
- [Ch. 9 §5 "Project Cost Elements"](../../../../rightingsoftware/OEBPS/xhtml/ch09.xhtml#ch09lev1sec5) — total/direct/indirect costs
- [Ch. 9 §5.7 "Staffing and Cost Elements"](../../../../rightingsoftware/OEBPS/xhtml/ch09.xhtml#ch09lev2sec13)

**Standard reference:** [Appendix C §4.1e](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec4) — *"Design several options for the project; at a minimum, design normal, compressed, and subcritical solutions."*

## Input

- `methodpoc/designs/<product>/project/normal.md`
- `methodpoc/designs/<product>/project/network.yaml`
- `methodpoc/designs/<product>/project/planning-assumptions.md`

## Output

`methodpoc/designs/<product>/project/subcritical.md`

## Procedure

### Step 1 — Reduce staffing by 1 or 2

Start from normal solution staffing. Remove the resource that has the most opportunity for serialization — typically a junior-developer or test-engineer.

Why these and not seniors: seniors are usually on the detailed-design critical chain; removing them is no longer subcritical, it's broken.

Pick the role and headcount that, when removed, forces previously-parallel activities to serialize.

### Step 2 — Recompute the network

With the reduced pool:

1. Walk the network. Activities that previously ran in parallel because two juniors handled them now serialize through one junior.
2. Some non-critical activities will become critical (their float consumed by serialization).
3. New critical path emerges, longer than before.
4. Recompute ES/EF/LS/LF for every activity.
5. Recompute total/free floats.

This is mechanical. Apply the same algorithm as [[the-method-network-draft]].

### Step 3 — Recompute costs

Two effects:

| Component | Effect | Why |
|---|---|---|
| **Direct cost** | Same or slightly less | Total person-days for the work didn't change much (each construction still takes the same days; only ordering changed). Slightly less if you removed a person entirely. |
| **Indirect cost** | INCREASES significantly | Duration is longer → overhead burn rate × duration is higher |
| **Total cost** | Usually HIGHER than normal | Indirect increase dominates |

This is the **counterintuitive result** the book wants on the management table.

Per ch. 9: indirect cost is proportional to duration. Removing a resource shortens direct cost slightly but extends duration substantially → total goes up.

### Step 4 — Recompute efficiency

```
efficiency = direct_cost / total_cost
```

Subcritical efficiency is **lower** than normal (because indirect grows). This often surprises people who think "fewer people = leaner" — the chart shows the opposite.

### Step 5 — Recompute risk

Risk **increases** in subcritical:

- **Criticality risk** rises (more activities pushed onto the critical path due to serialization)
- **Bus-factor risk** rises (the reduced team has fewer redundant skills)
- **Schedule risk** rises (longer duration = more exposure to disruption)

App C §4.7: criticality risk > 0.75 = reject. Subcritical often pushes past this. Use that as the rejection signal.

### Step 6 — Write `subcritical.md`

Mirror the structure of `normal.md` but add the comparison columns:

```markdown
# Subcritical Solution — <Product>

## Summary (vs. normal)

| Metric | Normal | Subcritical | Delta |
|---|---|---|---|
| Duration | 120 days | 165 days | +37% |
| Direct cost | 24 mm | 22 mm | -8% |
| Indirect cost | 12 mm | 17 mm | +42% |
| **Total cost** | **36 mm** | **39 mm** | **+8%** |
| Efficiency | 17% | 12% | worse |
| Peak staffing | 5 | 3 | smaller team |
| Preliminary risk | 0.45 | 0.72 | higher |

**Verdict:** Subcritical is longer (+37%), more expensive (+8%), AND riskier (+0.27). The smaller team is not cheaper.

## The reduction
- Removed: 1 junior-developer (junior-dev-3)
- Resources remaining: <list>

## New critical path
A001 → A002 → ... → AN (longer than normal)

## Why it's worse
1. Activities that ran in parallel under normal now serialize through the remaining junior-developer
2. Indirect cost (overhead × duration) grew by N man-months due to the duration extension
3. Bus-factor risk increased: only one junior-dev now handles all junior-tier construction
4. Near-critical chains became critical (no float left to absorb disruption)

## Staffing distribution
<chart>

## Planned earned value (S curve)
<chart — typically less shallow than normal, sometimes flat-then-spike>

## Costs (man-months)
| Cost | Subcritical |
|---|---|
| Direct | N |
| Indirect | N |
| Total | N |

## Risk flags
- ...

## Conclusion
Subcritical exists in this SDP solely to demonstrate the trap of "smaller team must be cheaper." It should not be the recommended option. Present it to management precisely so they reject it.
```

### Step 7 — Confirm it's actually subcritical, not broken

The line between "subcritical" and "infeasible" is real. Check:

- Does the network still complete? (If you removed the only senior dev, no — that's broken, not subcritical.)
- Does each remaining role have enough hands for the new serial workload?
- Is the duration extension realistic, or did you accidentally produce a fantasy timeline?

If the subcritical solution is infeasible, your removal was wrong. Pick a different resource to remove.

## Exit criteria (for router)

`subcritical.md` exists with the comparison table showing it's longer, costlier, and riskier than normal. Network is feasible (just suboptimal).

Move to `the-method-compressed-solution`.

## Anti-patterns to reject

- **Subcritical that's shorter than normal** — algorithmic mistake; recompute the network.
- **Subcritical with same risk as normal** — you didn't push it hard enough; or your risk model is wrong.
- **Subcritical that removes the senior dev from a senior-hand-off project** — that's not subcritical, it's broken.
- **Treating subcritical as a real option to recommend** — it exists for contrast, not for the road.

## When subcritical might actually be chosen (rare)

Management may choose subcritical if:
- There's a hard cash constraint that forbids the normal team headcount
- The duration extension is acceptable (no hard deadline)
- They explicitly accept the elevated risk

This is rare. The SDP must surface the trade-offs clearly so the choice is informed, not accidental.
