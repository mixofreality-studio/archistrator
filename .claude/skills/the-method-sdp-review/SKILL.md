---
name: the-method-sdp-review
description: Project Design — assemble the SDP Review. Audience is management. Presents the viable options (normal, decompressed-normal, subcritical, compressed) with duration/cost/risk, the time-cost and time-risk curves, and the architect's recommendation. Reads all Phase 2 artifact slots plus the committed .mission and .systemDesign from project.json. Produces the typed SdpReview committed to project.json → .sdpReview. Invoke after [[the-method-risk-modeling]] as the final phase of project design, before [[the-method-project-design-standard-check]].
---

# SDP Review Document

The Software Development Plan review is the artifact management uses to make an *educated decision* about how to commit to the project. It is the culmination of all Phase 2 work. After this, management picks one option, project enters Phase 3 (construction).

Per Löwy: this is the moment of *educated decisions with viable options that differ by schedule, cost, and risk* (Directive 7).

## Canonical source

**Primary:**
- Löwy, [Ch. 7 §3.2 "Software Development Plan Review"](../../../../rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07lev2sec5)
- [Ch. 11 §8 "SDP Review"](../../../../rightingsoftware/OEBPS/xhtml/ch11.xhtml#ch11lev1sec8)
- [Ch. 11 §8.1 "Presenting the Options"](../../../../rightingsoftware/OEBPS/xhtml/ch11.xhtml#ch11lev2sec21)

**Worked examples:**
- TradeMe SDP options: [Ch. 11 Table 11-11 "Project design options for review"](../../../../rightingsoftware/OEBPS/xhtml/ch11.xhtml)
- Second worked example SDP: [Ch. 13 §7 "Comparing the Options"](../../../../rightingsoftware/OEBPS/xhtml/ch13.xhtml#ch13lev1sec7) + [§9 "Preparing for the SDP Review"](../../../../rightingsoftware/OEBPS/xhtml/ch13.xhtml#ch13lev1sec9)

**Standard reference:** [Appendix C §4.1g "Always go through SDP review before the main work starts"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec4) and [§4.1f "Communicate with management in Optionality"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec4).

## Input

State is git-as-DB: all of this lives in `.aiarch/state/project.json` (a typed JSON aggregate), NOT in `designs/<product>/*.md` or `network.yaml` files. Markdown/DSL/YAML is a render-on-read of the typed state, never the source of truth.

- All Phase 2 committed slots:
  - `.planningAssumptions`
  - `.activityList`
  - `.network`
  - `.normalSolution`
  - `.subcriticalSolution`
  - `.compressedSolution`
  - `.decompressedSolution`
  - `.riskModel`
- System design context: the committed `.mission` and `.systemDesign` slots

## Output

The SDP review is a **typed model committed into `.aiarch/state/project.json` → `.sdpReview`** — git is the database. It is the artifact management reads (rendered to a management-facing document). It is NOT an `sdp-review.md` file; any markdown (including the section templates below) is a render-on-read of that JSON slot. After management commits to an option, the chosen option and start date are captured (see Step 10).

Two usage patterns produce this slot:

1. **Agentic/CI dispatch:** the agent produces the typed `SdpReview` model as JSON and commits it into `.sdpReview` on its session branch; the server reads it back and stages it (`StageArtifactForReview`) for the human review gate (`CommitArtifact` / `RejectArtifact`), and `AdvancePhase` moves the project to Phase 3.
2. **Local interactive:** same — produce the typed model and write it into the `.sdpReview` slot. Never a `designs/*.md` file.

## Procedure

### Step 1 — Pick the viable options to present

Per App C §4.1e: *"Design several options for the project; at a minimum, design normal, compressed, and subcritical solutions."*

Per ch. 11: typical SDP presents 3–5 options. Always include:

1. **Normal** (or **Decompressed Normal** if decompression was applied) — the cost-risk sweet spot
2. **Compressed** — fastest option still in the inclusion zone
3. **Subcritical** — slowest option, to disprove the "fewer people = cheaper" intuition

You may include additional points along the time-cost curve if relevant (e.g., a moderately compressed option).

**Exclude** options that:
- Risk > 0.75 (App C §4.7h)
- Risk < 0.3 (App C §4.7g)
- In death zone (App C §4.6b)
- Compression > 30% (App C §4.6e)
- Efficiency > 25% (App C §4.6f)

### Step 2 — Build the executive summary

Top of the `.sdpReview` model. Three to four sentences max. Must answer:

- What's the project?
- What's the architect's recommendation?
- What are the trade-offs at a glance?

Example:

> The project is to build a tradesman-matching platform for TradeMe with three core use cases. The recommended option is **Decompressed Normal**: 130 working days, 37 man-months total cost, risk 0.48. Faster (90-day) and slower (165-day) options exist but trade significantly worse cost-risk profiles. Management's decision drives commitment.

### Step 3 — Recap architecture and mission

Brief — one section, half a page max. Include:

- One-sentence vision (from the committed `.mission` slot)
- The 2–6 core use cases by name (from the committed `.coreUseCases` slot)
- An image or summary of the static architecture (from the committed `.systemDesign` slot; ideally embed a rendered PNG if the DSL render has been run)
- Number of components, by layer (count components per layer tag in the committed `.systemDesign` slot)

This grounds management in *what* will be built before the *how*.

### Step 4 — Present the options table

The headline artifact. Per ch. 11 Table 11-11:

```markdown
## Project Design Options for Management Review

| Option | Duration | Total cost | Direct cost | Indirect cost | Peak staff | Risk | Notes |
|---|---|---|---|---|---|---|---|
| **Decompressed normal (recommended)** | 130 days | 37 mm | 24 mm | 13 mm | 5 | 0.48 | Cost-risk sweet spot |
| Normal | 120 days | 36 mm | 24 mm | 12 mm | 5 | 0.55 | Slightly riskier than decompressed |
| Compressed | 90 days | 41 mm | 32 mm | 9 mm | 8 | 0.72 | Fastest acceptable option |
| Subcritical | 165 days | 39 mm | 22 mm | 17 mm | 3 | 0.78 | Smaller team, longer, riskier, total cost rises |
```

Round duration and cost (App C §4.1f spirit — communicate in Optionality, not in spurious precision). Keep risk to two decimal places to preserve the comparison.

### Step 5 — Present the curves

Include both:

1. **Time-Cost Curve** (from the committed `.riskModel` slot) — shows the cost shape of the option space
2. **Time-Risk Curve** (from the committed `.riskModel` slot) — shows the risk shape, with tipping point marked

If text-only output, ASCII charts suffice. If Mermaid is renderable, prefer xy-chart-beta or scatter.

### Step 6 — Architect's recommendation

A clear paragraph stating the recommendation and reasoning. Per ch. 11 §8.1: management cares about *why*, not just *what*.

Format:

```markdown
## Architect's Recommendation

**Recommended: Decompressed Normal.**

Reasoning:
1. Risk is 0.48 — past the tipping point of the risk curve, with no over-decompression penalty.
2. Cost is 37 mm — within 3% of the absolute minimum on the time-cost curve.
3. Duration is 130 days — 10 days longer than normal, buying meaningful risk reduction.
4. The 90-day compressed option saves 40 days but adds 4 mm in cost (~11%) AND raises risk to 0.72 — questionable trade unless the deadline pressure is hard.
5. The 165-day subcritical option costs more *and* is riskier — never the right choice unless there's a binding cash constraint.

Final decision is management's. The architect recommends but does not commit.
```

### Step 7 — Cost of not deciding

Per Löwy implicit in App A and ch. 7: management often wants to "wait and see." Make the cost of indecision explicit.

```markdown
## Cost of Not Deciding Now

Each week of delay before commitment costs:
- ~0.6 mm in core-team idle (architect, PjM, PdM on retainer)
- Loss of the senior developer hires currently available (planning assumption: 2 seniors available for 60 more days)
- Slip on the dependent integration with the existing platform (release window: Q3)

Decide by <date>.
```

### Step 8 — Planning assumptions explicit

List the key assumptions from the committed `.planningAssumptions` slot that drive these options. Management ratifies (or disputes) them at this review.

```markdown
## Planning Assumptions

(Full list in the committed `.planningAssumptions` slot. Key ones:)

1. Core team in place day 1
2. 2 senior developers available for 60+ days
3. UX expert available 25 days in Q2
4. No competing priority project consumes >20% of the team
5. Test environment provisioned by day 30
```

If management disputes an assumption ("we can't guarantee #4"), the architect recomputes — this is normal SDP-review dialog.

### Step 9 — Design Standard compliance summary

Brief table showing compliance with the project-design portion of App C:

```markdown
## Design Standard Compliance (App C)

| Section | Item | Status |
|---|---|---|
| Directive 6 | Design the project to build the system | ✓ |
| Directive 7 | Educated decisions with options | ✓ |
| §4.1c | Capture planning assumptions | ✓ |
| §4.1e | Multiple options (normal, compressed, subcritical) | ✓ |
| §4.2c | Lowest staffing for unimpeded critical path | ✓ |
| §4.2e | Correct staffing distribution | ✓ |
| §4.2f | Shallow S curve | ✓ |
| §4.4d | 5-day estimation quantum | ✓ |
| §4.6a | Quick-and-clean first | ✓ |
| §4.6e | Compression ≤ 30% | ✓ |
| §4.7c | Decompressed normal to ~0.5 risk | ✓ |
| §4.7f–h | Exclusion zones applied | ✓ |
```

### Step 10 — Decision capture

A blank section management fills in:

```markdown
## Decision

| | |
|---|---|
| Date | _______________ |
| Decision-maker(s) | _______________ |
| Chosen option | _______________ |
| Start date | _______________ |
| Assumptions accepted | _______________ |
| Any condition / amendment | _______________ |
| Signature(s) | _______________ |
```

Once filled, capture the decision in the `.sdpReview` model and set the chosen option / start date on the committed `.network` slot:

```yaml
# fields set on the .network slot
project:
  chosen_option: <name>
  start_date: <date>
```

Then `AdvancePhase` moves the project into Phase 3 (or run `/implement-project` locally) to begin construction.

## Exit criteria (for router)

- `.aiarch/state/project.json` → `.sdpReview` holds a committed typed model with all sections
- ≥3 viable options
- Recommended option named with reasoning
- Time-cost and time-risk curves included
- Compliance summary present
- Decision capture section blank, ready to fill

Phase 2 complete. Hand to management.

## Anti-patterns to reject

- **Single-option SDP** — by definition not a review, not educated. App C requires ≥3.
- **"Hidden recommendation"** — bury the trade-offs; let management guess. Bad faith. State the recommendation and the why.
- **Overly precise numbers** — "37.6 man-months" implies false precision. Round.
- **Skipping the cost-of-delay calculation** — management defers indefinitely without it.
- **Compliance summary as a victory lap** — it's a checklist, not bragging. If items fail, fix them, don't paper over.
- **No decision capture section** — review meeting ends without a decision; project re-enters limbo.

## On the "junior hand-off" trap (ch. 14)

If your team composition is all juniors and you're proposing the normal option without explicit detailed-design activities, you've implicit-junior-hand-off-ed the project. Surface this in the SDP. Either:

- Get budget for a senior, OR
- Acknowledge the architect will personally do detailed design (extends front-end significantly, makes the architect a bottleneck), OR
- Accept reduced quality and elevated risk

Make this conversation explicit at the SDP review, not later.
