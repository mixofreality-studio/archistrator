---
name: the-method-decompressed-solution
description: Design the decompressed-normal solution. Take the normal solution and deliberately extend its duration to drop criticality risk toward the tipping point (~0.5) without consuming the float by reducing staff. Produces the typed DecompressedSolution committed to project.json → .decompressedSolution as the 4th option for SDP review. Use after normal solution has been built and its risk has been computed; runs in parallel with subcritical and compressed as a sibling option.
---

# Decompressed Solution

The decompressed solution is the cost-risk sweet spot option. It takes the normal solution — already minimum-staffed for unimpeded critical path progress — and *extends* duration by introducing float along the network so the project becomes less brittle. Risk drops. Indirect cost rises slightly because duration grew. Direct cost is essentially unchanged because staffing is the same.

This is usually the **recommended** option in the SDP review. It exists as a sibling to normal, subcritical, and compressed — not as a sub-step of risk modeling.

Critical rule ([ch. 10 §5](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10lev1sec5)): *keep the original staffing.* Do not be tempted to consume the new float by reducing the team. That defeats the entire point of decompression.

This is a Phase 2 project-design activity. The layer model from `[[the-method-layers]]` does not apply here. Per `[[the-method-doctrine]]` Directive 7, this option exists so management has a viable middle-ground choice with quantified risk reduction — not just normal-vs-compressed-vs-subcritical.

## Canonical source

**Primary:**
- Löwy, [Ch. 10 §5 "Risk Decompression"](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10lev1sec5)
- [Ch. 10 §5 "How To Decompress"](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10lev2sec10)
- [Ch. 10 §5 "Decompression Target"](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10lev2sec11)
- [Ch. 12 §3 "Finding the Decompression Target"](../../../research/rightingsoftware/OEBPS/xhtml/ch12.xhtml#ch12lev1sec3) — calculus-based identification of the inflection point

**Advanced (for skewed projects):**
- [Ch. 12 §4 "Geometric Risk"](../../../research/rightingsoftware/OEBPS/xhtml/ch12.xhtml#ch12lev1sec4) — use geometric risk instead of arithmetic when the float distribution is uneven (god activities, large outliers)

**Worked example:** [Ch. 11 §7 "Planning and Risk"](../../../research/rightingsoftware/OEBPS/xhtml/ch11.xhtml#ch11lev1sec7) — `D3` chosen as decompression target.

**Standard reference:** [Appendix C §4.7c "Decompress the normal solution past the tipping point on the risk curve"](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec4) and §4.7d ("Do not over-decompress").

## Input

State is git-as-DB: all of this lives in `.aiarch/state/project.json` (a typed JSON aggregate), NOT in `designs/<product>/*.md` or `network.yaml` files. Markdown/DSL/YAML is a render-on-read of the typed state, never the source of truth.

- The committed **normalSolution** artifact in `project.json` → `.normalSolution` (the baseline being decompressed, with its resource-assigned network state) and the committed **network** artifact → `.network`
- Normal solution's computed risk (criticality risk and activity risk) — produced by an initial pass of `[[the-method-risk-modeling]]` on the normal option alone, or computed inline as Step 1 here if risk-modeling hasn't run yet
- The committed **planningAssumptions** artifact in `project.json` → `.planningAssumptions` (risk flags that motivate decompression)

## Output

The decompressed solution is a **typed model committed into `.aiarch/state/project.json` → `.decompressedSolution`** — git is the database. It carries this option's own network state (normal's network plus labeled decompression-buffer activities; original activity durations unchanged), summary metrics, the iteration table, and the comparison-to-normal table. It is NOT a `decompressed.md` file; any markdown (including the Step 8 template) is a render-on-read of that JSON slot.

This is the **4th option** alongside `.normalSolution`, `.subcriticalSolution`, and `.compressedSolution`. All four become inputs to `[[the-method-risk-modeling]]` (for the final time-cost and time-risk curves across the full option space) and then to `[[the-method-sdp-review]]`.

Two usage patterns produce this slot:

1. **Agentic/CI dispatch:** the agent produces the typed `DecompressedSolution` model as JSON and commits it into `.decompressedSolution` on its session branch; the server reads it back and stages it (`StageArtifactForReview`) for the human review gate (`CommitArtifact` / `RejectArtifact`).
2. **Local interactive:** same — produce the typed model and write it into the `.decompressedSolution` slot. Never a `designs/*.md` file.

## Procedure

### Step 1 — Confirm normal's risk justifies decompression

Per ch. 10 §6 "Risk Metrics": *"Keep normal solutions under 0.7. You should always decompress high-risk normal solutions."*

Read normal's current criticality risk and activity risk:

| Normal's risk | Action |
|---|---|
| 0.3 – 0.5 | Decompression optional; you may still produce a decompressed point to show management the trade |
| 0.5 – 0.7 | Decompress to drop risk toward the tipping point (~0.5) |
| > 0.7 | Decompress is mandatory per App C §4.7c; high-risk normal must not be presented as recommendable |

If normal already sits at or below 0.5, decompression past the tipping point usually starts increasing risk again (over-decompression) — see Step 5.

### Step 2 — Choose decompression mechanism (ch. 10 §5 "How To Decompress")

Two mechanisms, picked per project shape:

1. **Push the last activity / last event down the timeline.** Simplest. Adds float to all prior activities uniformly. Per ch. 10: *"In the case of the network depicted in Figure 10-4, decompressing activity 16 by 10 days results in a criticality risk of 0.47 and an activity risk of 0.52."*

2. **Decompress one or two key activities on the critical path** (e.g., adding buffer at an earlier node). Per ch. 10: *"the further down the network you decompress, the more you need to decompress because any slip in an upstream activity can consume the float of the downstream activities. The earlier in the network you decompress, the less likely it is that all of the float you have introduced will be consumed."*

Practical guidance:

| Project shape | Mechanism |
|---|---|
| Single long critical chain | Push last event |
| Multiple converging critical paths | Buffer the merge points |
| One identifiable high-risk activity (bus factor, unknown technology) | Buffer that activity directly |
| Long-tailed integration phase | Push the last event |

You may mix mechanisms.

### Step 3 — Add decompression float to this option's network state in `.decompressedSolution`

For each chosen decompression point:

- If pushing the last event: add a `decompression_buffer` activity at the end, with `duration_days: N` and no work content.
- If buffering a critical activity: add a `decompression_buffer_<activity_id>` activity *after* it, on the same chain, with `duration_days: N`.
- **Do not** edit the original activity's `duration_days`. That hides the decompression and looks like estimation padding — which ch. 10 §5 explicitly warns against: *"a classic mistake when trying to reduce risk is to pad estimations. This will actually make matters worse... The whole point of decompression is to keep the original estimations unchanged and instead increase the float along all network paths."*

Buffer activities should be tagged so it's obvious they are decompression float, not real work.

### Step 4 — Recompute network and risk after decompression

Mechanical:

1. Recompute ES/EF/LS/LF for every activity with the buffer(s) in place.
2. Recompute total/free floats.
3. The critical path may stay the same in shape but be longer; near-critical activities will now have more float.
4. Recompute criticality risk: `count(total_float = 0) / count(all activities)`.
5. Recompute activity risk: weighted by the float distribution.

Track the (duration, criticality_risk, activity_risk) point.

### Step 5 — Iterate to the decompression target (~0.5 risk)

Per [Ch. 10 §5 "Decompression Target"](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10lev2sec11): *"the ideal decompression target is a risk of 0.5, as it targets the tipping point in the risk curve."*

Procedure:

1. Add a small buffer (e.g., 5 days). Recompute. Note the new risk point.
2. If risk is still > 0.5 and well above the tipping point, add more.
3. If risk has dropped to or just past 0.5, **stop** — you're at the target.
4. If risk has barely moved (e.g., dropped from 0.55 to 0.54 with 10 added days), you're decompressing in the wrong place. Switch mechanism (Step 2). The chain you're buffering may not be the binding one.

Per App C §4.7c.ii: *"Value the risk tipping point more than a specific risk number."* If the curve flattens at 0.45 risk and 130 days, that's better than chasing exactly 0.5 at 140 days.

**Advanced — Finding the tipping point via calculus (ch. 12 §3):**
For projects where the risk curve has been fitted to a polynomial, the inflection point (where the second derivative equals zero) is the objective decompression target. Per ch. 12: *"merely eyeballing a chart is not a good engineering practice. Instead, you should apply elementary calculus to identify the decompression target in a consistent and objective manner."* The worked example computes the tipping point at 10.62 months / risk 0.55, justifying option `D3` as the decompression choice. Use this if (a) your risk curve is skewed and 0.5 isn't visually obvious, or (b) the project is large enough to warrant the rigor.

**Advanced — Geometric risk (ch. 12 §4):**
If the float distribution is heavily uneven (god activities, large outliers), arithmetic risk gives a false low reading. Use geometric criticality risk and geometric activity risk to get a truer signal before deciding the decompression target. Don't decompress against a misleading number.

### Step 6 — Stop before over-decompressing

Per [Ch. 10 §6](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10lev1sec6) and App C §4.7d: *"Do not over-decompress."*

Stop criteria:

- Risk has reached the tipping point (~0.5, or wherever the curve inflects).
- Risk is no longer dropping meaningfully per added day of buffer (diminishing returns — additional float lands on activities that already had plenty).
- Indirect cost growth begins to dominate the cost story (the point moves visibly *up* the time-cost curve on the right side).

Over-decompression is *not* "safer." Per ch. 10: *"Excessive decompression will have diminishing returns when all activities have high float. Any additional decompression beyond this point will not reduce the design risk, but will increase the overall overestimation risk and waste time."*

### Step 7 — Recompute costs

Two effects, mirroring subcritical's logic but milder:

| Component | Effect | Why |
|---|---|---|
| **Direct cost** | Unchanged | Same staffing; same person-days of work. Per ch. 10 §5: *"When you decompress a project design solution, you still design it with the original staffing. Do not be tempted to consume the additional decompression float and reduce the staff."* |
| **Indirect cost** | Increases slightly | Duration is longer → overhead × duration is higher |
| **Total cost** | Slightly higher than normal | Indirect rises; direct doesn't |
| **Efficiency** | Slightly lower than normal | direct / total drops as total rises |

The increase is small compared to subcritical (which extends duration *and* serializes work) — that's the point. Decompressed normal is *not* subcritical.

### Step 8 — Commit the typed decompressed solution to `.decompressedSolution`

Produce the typed `DecompressedSolution` model and commit it to `.aiarch/state/project.json` → `.decompressedSolution`. The markdown below is the equivalent **human rendering** — use it to review the solution, but the source of truth is the slot, not a `decompressed.md` file:

```markdown
# Decompressed Solution — <Product>

## Summary (vs. normal)

| Metric | Normal | Decompressed | Delta |
|---|---|---|---|
| Duration | 120 days | 130 days | +8% |
| Direct cost | 24 mm | 24 mm | unchanged (same staffing) |
| Indirect cost | 12 mm | 13 mm | +8% (longer duration) |
| **Total cost** | **36 mm** | **37 mm** | **+3%** |
| Efficiency | 17% | 16% | slightly lower |
| Peak staffing | 5 | 5 | unchanged |
| Criticality risk | 0.55 | 0.48 | -0.07 ✓ at tipping point |
| Activity risk | 0.52 | 0.45 | -0.07 |

**Verdict:** ~10 days of buffer drops risk past the tipping point at a 3% total-cost increase. Cost-risk sweet spot.

## Decompression strategy
- Mechanism: pushed last event by 5 days; buffered critical activity A012 by 5 days
- Rationale: A012 has the highest bus-factor risk (only one senior); buffering it locally absorbs the most likely disruption source
- Staffing: unchanged from normal (per ch. 10 §5 rule)

## Iterations
| Iteration | Added float (days) | Where | Criticality risk | Activity risk |
|---|---|---|---|---|
| 0 (normal) | 0 | — | 0.55 | 0.52 |
| 1 | +5 | last event | 0.51 | 0.49 |
| 2 | +5 | buffer after A012 | 0.48 | 0.45 |
| 3 (rejected — over-decompression) | +10 | last event | 0.47 | 0.44 (diminishing returns) |

Final: iteration 2.

## New critical path
A001 → A002 → ... → A012 → A012_buffer → ... → final_buffer
Total: 130 days

## Costs (man-months)
| Cost | Decompressed |
|---|---|
| Direct | 24 |
| Indirect | 13 |
| Total | 37 |

## Risk flags absorbed
- Bus-factor on senior-dev-1 on A012 — local buffer absorbs slip
- UX expert availability uncertain in Q3 — last-event buffer absorbs late integration slip

## Risk flags NOT absorbed (still on the table)
- (anything decompression can't fix — capture for SDP review)

## When to choose decompressed over normal
- Track record of slipping projects in this org
- High-uncertainty environment (volatile priorities, team turnover)
- Hard external commitment date that benefits from a buffer
- Management's risk appetite is low and the small total-cost increase is acceptable
```

## Exit criteria (for router)

`.aiarch/state/project.json` → `.decompressedSolution` holds a committed typed model with:
- Comparison table showing duration slightly longer, risk lower, total cost slightly higher
- Decompression mechanism named (pushed last event / buffered specific activity / both)
- Staffing explicitly equal to normal (rule from ch. 10 §5)
- Final risk near the tipping point (~0.5, or justified deviation)
- Iteration table showing the path to the chosen point
- Updated network state captured (buffers added to this option's network state in `.decompressedSolution`, original activity durations unchanged)

Hand to `[[the-method-risk-modeling]]` for the full-option-set risk analysis (time-cost curve, time-risk curve, exclusion zones across all four options).

## Anti-patterns to reject

- **Padding original activity estimates instead of adding buffer activities** — disguises decompression as pessimism, and ch. 10 §5 explicitly rejects this: *"a classic mistake when trying to reduce risk is to pad estimations."* Buffers must be separate, labeled activities.
- **Reducing staffing to "pay for" the extra duration** — defeats the entire purpose. Ch. 10 §5: *"Do not be tempted to consume the additional decompression float and reduce the staff—that defeats the purpose of risk decompression in the first place."* Decompressed is **not** subcritical.
- **Over-decompressing past the tipping point** — diminishing returns, risk eventually rises again as indirect cost dominates and the project becomes harder to commit to. Stop at the tipping point.
- **Decompressing in the wrong place** — adding float far downstream of the actual risk source. The buffer must lie on the critical or near-critical chain that contains the risk. Watch the metric: if risk barely moves, switch mechanism.
- **Eyeballing the tipping point on a noisy curve** — for skewed risk distributions, use ch. 12 §3 calculus or ch. 12 §4 geometric risk instead.
- **Skipping decompression because "normal looks fine"** — App C §4.7c calls for decompression as standard practice on every SDP, even if the chosen normal already sits near 0.5. The decompressed point gives management a comparison.

## Relationship to other options

| Option | Duration | Staffing | Direct cost | Indirect cost | Total cost | Risk |
|---|---|---|---|---|---|---|
| Subcritical | longest | reduced | ≈ same | highest | high | high |
| Normal | baseline | minimum-unimpeded | baseline | baseline | minimum* | moderate-high |
| **Decompressed normal** | **+5–10%** | **same as normal** | **same as normal** | **slightly higher** | **slightly higher** | **lowest** |
| Compressed | shortest | augmented (parallel + top) | highest | lowest | high | highest |

*Normal is often at or near minimum total cost in theory, but in practice (ch. 10 §5 note) sits a bit off — which is one reason decompressed-normal is so often the sweet spot.
