---
name: the-method-risk-modeling
description: Quantify and compare risk across all project options (normal, decompressed-normal, subcritical, compressed). Compute criticality risk and activity risk per option, plot the time-risk and time-cost curves, and apply exclusion zones. Produces the typed RiskModel committed to project.json → .riskModel as the analytics input to SDP review. Use after all four options exist; runs as pure analytics over their network states.
---

# Risk Modeling

Each option from phases 2.4–2.6, plus the decompressed-normal from `[[the-method-decompressed-solution]]`, has a duration, cost, and *risk*. Risk is computable, not vibes. This skill is pure analytics: it computes risk metrics across the existing option set, plots the time-risk and time-cost curves, and applies the App C exclusion bounds.

**This skill does not create options.** Decompression of the normal solution is its own activity — see `[[the-method-decompressed-solution]]`. By the time risk-modeling runs, all four options (normal, subcritical, compressed, decompressed-normal) already exist as their own files with their own network states. Risk-modeling reads them and computes metrics + curves across the set.

Per `[[the-method-doctrine]]` Directive 7, the analytics here are what let management see the option set as a *shape* (time-cost curve, time-risk curve), not just a list — that's the basis for an educated decision.

## Canonical source

**Primary:** Löwy, [Chapter 10 "Risk"](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml) in full.

**Key subsections:**
- [Ch. 10 §2 "Time-Risk Curve"](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10lev1sec2)
- [Ch. 10 §3 "Risk Modeling"](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10lev1sec3)
- [Ch. 10 §3.4 "Criticality Risk"](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10lev2sec5)
- [Ch. 10 §3.6 "Activity Risk"](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10lev2sec7)
- [Ch. 10 §4 "Compression and Risk"](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10lev1sec4)
- [Ch. 10 §6 "Risk Metrics"](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10lev1sec6)

**Worked example:** [Ch. 11 §7 "Planning and Risk"](../../../research/rightingsoftware/OEBPS/xhtml/ch11.xhtml#ch11lev1sec7).

**Advanced (optional):** [Ch. 12 §4 "Geometric Risk"](../../../research/rightingsoftware/OEBPS/xhtml/ch12.xhtml#ch12lev1sec4) for projects with god activities or heavily skewed float distributions.

**Standard reference:** [Appendix C §4.7 "Risk"](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec4) — items 7a–7i.

## Input

State is git-as-DB: all of this lives in `.aiarch/state/project.json` (a typed JSON aggregate), NOT in `designs/<product>/*.md` files. Markdown/DSL is a render-on-read of the typed state, never the source of truth.

All four option slots must already be committed, each with its own network state:

- The committed **normalSolution** artifact → `.normalSolution` (network state for normal)
- The committed **decompressedSolution** artifact → `.decompressedSolution` (network state for decompressed-normal)
- The committed **subcriticalSolution** artifact → `.subcriticalSolution` (network state for subcritical)
- The committed **compressedSolution** artifact → `.compressedSolution` (network state for compressed)
- The committed **planningAssumptions** artifact → `.planningAssumptions` (risk flags)

If `.decompressedSolution` is not yet committed, hand back to `[[the-method-decompressed-solution]]` before computing the cross-option analytics. (Decompression needs normal's risk computed first — that's a tighter loop owned by the decompressed-solution skill itself.)

## Output

The risk model is a **typed model committed into `.aiarch/state/project.json` → `.riskModel`** — git is the database. It covers all four options (metrics, time-risk and time-cost curves, exclusion verdicts, recommendation). It is NOT a `risk.md` file; any markdown (including the Step 6 template) is a render-on-read of that JSON slot.

Two usage patterns produce this slot:

1. **Agentic/CI dispatch:** the agent produces the typed `RiskModel` model as JSON and commits it into `.riskModel` on its session branch; the server reads it back and stages it (`StageArtifactForReview`) for the human review gate (`CommitArtifact` / `RejectArtifact`).
2. **Local interactive:** same — produce the typed model and write it into the `.riskModel` slot. Never a `designs/*.md` file.

## Procedure

### Step 1 — Compute criticality risk per option (ch. 10 §3.4)

For each option (normal, decompressed, subcritical, compressed), compute:

```
criticality_risk = count(activities with total_float = 0) / count(all activities)
```

This is a normalized 0–1 score. Higher = more activities have no float = more brittle.

Note: criticality risk should be **customized** per project (App C §4.7a). Some teams may want to count near-critical (float ≤ 5 days) as well — define and document your bands explicitly:

| Risk band | total_float (days) |
|---|---|
| Critical | 0 |
| Near-critical | 1–5 |
| Float | 6+ |

If you use a custom band definition, document it at the top of the `.riskModel` slot.

### Step 2 — Compute activity risk per option (ch. 10 §3.6)

Per ch. 10: float outliers can skew the simple criticality measure. Activity risk is a refined measure weighting by the *float distribution*.

Approach:

```
activity_risk = 1 - (sum(total_float[a]) / (count(activities) * max_float))
```

Or equivalently: high activity risk when most activities have low float; low activity risk when many activities have generous float.

Per App C §4.7b: *"Adjust floats outliers with activity risk."* If one activity has wildly high float (e.g., 200 days when project is 120 days), it distorts the average. Either cap floats at the project duration or use activity risk's robustness.

Both metrics belong in the `.riskModel` slot. They tell slightly different stories.

**Advanced (ch. 12 §4):** If the project has god activities or extreme float outliers, use geometric criticality risk and geometric activity risk instead of arithmetic. The arithmetic mean lies in the face of `[1, 2, 3, 1000]`-style distributions. Apply geometric variants if arithmetic risk gives an unrealistically low reading. Document the choice.

### Step 3 — Plot the time-risk curve

For each option, plot the (duration, risk) pair. Order by duration ascending.

Per ch. 10: this curve has a characteristic shape. Short durations (heavily compressed) have very high risk. Long durations (subcritical) also have high risk (more exposure, brittle long chains). The middle has a **tipping point** where risk is minimized — typically at or near the decompressed-normal.

```markdown
## Time-Risk Curve

Duration (days) | Risk
   90 (compressed)            | 0.72
  120 (normal)                | 0.55
  130 (decompressed-normal)   | 0.48
  165 (subcritical)           | 0.78

(ASCII sketch:)
Risk
  1.0 |
  0.8 |x                          x
  0.6 |        x
  0.4 |            x
      +-----+-----+-----+-----+
       90    120   130   165   Duration
```

The tipping point is around 130 days here — and that's exactly where the decompressed-normal sits, by design.

### Step 4 — Build the time-cost curve

For each option, plot (duration, total_cost).

The curve typically shows:
- Cost rising sharply toward compressed (left side — parallel work / top resources)
- Minimum cost near normal / decompressed-normal
- Cost also rising toward subcritical (right side — indirect cost climbs with duration extension)

```markdown
## Time-Cost Curve

Duration | Total cost (mm)
   90 (compressed)            | 41
  120 (normal)                | 36
  130 (decompressed-normal)   | 37
  165 (subcritical)           | 39
```

The decompressed-normal often sits at or near the cost minimum AND at a healthy risk — that's why it's frequently the SDP's recommended option.

### Step 5 — Identify inclusion/exclusion zones (App C §4.7f–h)

Apply rules across all four options:

| Rule | Action |
|---|---|
| Risk > 0.75 | Exclude. Too risky to commit. (App C §4.7h) |
| Risk < 0.3 | Exclude. Too safe — wasting time and money. (App C §4.7g) |
| Death zone | Exclude. (App C §4.6b) |
| Compression > 30% | Exclude. (App C §4.6e) |
| Efficiency > 25% | Exclude. (App C §4.6f) |
| Normal-class option above risk 0.7 | Normal must have been decompressed (App C §4.7c and ch. 10 §6); flag if it wasn't |

App C §4.7i: *"Avoid project options riskier or safer than the risk crossover points."* The "risk crossover points" are where the risk curve hits 0.3 and 0.75 — these become hard exclusion bounds.

The result is a set of options ranked **IN** or **OUT**. Out-of-bounds options should still appear in the `.riskModel` slot (so the SDP review can show *why* they were excluded), but the SDP recommendation must come from the IN set.

### Step 6 — Commit the typed risk model to `.riskModel`

Produce the typed `RiskModel` model and commit it to `.aiarch/state/project.json` → `.riskModel`. The markdown below is the equivalent **human rendering** — use it to review the analytics, but the source of truth is the slot, not a `risk.md` file:

```markdown
# Risk Modeling — <Product>

## Risk band definition (custom)
- Critical: total_float = 0
- Near-critical: total_float ≤ 5 days
- Float: total_float > 5 days

## Per-option risk metrics

| Option | Duration | Criticality risk | Activity risk | In/Out |
|---|---|---|---|---|
| Compressed | 90 | 0.72 | 0.68 | OUT (near 0.75 ceiling) |
| Normal | 120 | 0.55 | 0.52 | IN |
| Decompressed normal | 130 | 0.48 | 0.45 | IN (recommended) |
| Subcritical | 165 | 0.78 | 0.74 | OUT (> 0.75 ceiling) |

## Time-Risk Curve
<chart>
Tipping point: ~130 days, ~0.48 risk — coincides with decompressed-normal.

## Time-Cost Curve
<chart>
Cost minimum: ~120 days, 36 mm. Decompressed-normal at 130 days / 37 mm is the cost-risk sweet spot.

## Recommended option (for SDP)
**Decompressed normal** — 130 days, 37 mm, risk 0.48. Lowest combined risk-and-cost.

## Excluded options and why
- Compressed (90 days, risk 0.72) — too close to 0.75 risk ceiling; in death zone proximity
- Subcritical (165 days, risk 0.78) — above 0.75 ceiling; smaller team caused brittle long chains

## Risk inputs from planning assumptions
- Bus-factor on senior-dev-1 (only senior on multiple critical activities)
- UX expert availability uncertain in Q3
- (other flags from the committed `.planningAssumptions` slot)

## Risk model choice
- Used arithmetic risk (no god activities, float distribution within expected band)
- (If geometric used: justify here — see ch. 12 §4)
```

## Exit criteria (for router)

- `.aiarch/state/project.json` → `.riskModel` holds a committed typed model
- All four options have criticality risk and activity risk computed
- Time-risk and time-cost curves plotted across the full option set
- Exclusion zones applied; IN/OUT marked per option
- Recommended option named with the cost-and-risk rationale
- Risk band definition documented if customized
- Risk-model choice (arithmetic vs geometric) justified

Hand to `[[the-method-sdp-review]]`.

## Anti-patterns to reject

- **Computing risk on fewer than four options** — the curves only mean something with the full set. If decompressed-normal is missing, send back to `[[the-method-decompressed-solution]]` before plotting curves.
- **Recommending an option in the OUT zone** — App C exclusions are hard. Pick from IN.
- **Single-metric risk** — use both criticality and activity risk; they reveal different patterns.
- **Float outliers ignored** — apply activity risk or geometric risk when distribution is skewed (ch. 12 §4).
- **Curves with two points** — a curve needs at least three points to show shape. Four (compressed, normal, decompressed, subcritical) is the minimum useful set.
- **Decompressing inside this skill** — that's `[[the-method-decompressed-solution]]`'s job. Risk-modeling is analytics only. If you find yourself adding buffers, you're in the wrong skill.

## Advanced: geometric risk (ch. 12, optional)

For large projects or projects with god activities, ch. 12 §4 introduces geometric variants of the risk metrics that are more sensitive to outliers. Apply if standard metrics are giving inconsistent signals — e.g., arithmetic activity risk reports 0.3 (looks safe) while you know most of the work sits on two enormous critical activities. The geometric model will surface the true high risk.

Geometric activity risk is also the right model when computing the decompression target on a skewed project — see `[[the-method-decompressed-solution]]` Step 5 advanced notes.

Not required for typical projects. See `ch12.xhtml#ch12lev1sec4` for details.
