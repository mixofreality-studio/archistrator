---
name: the-method-business-alignment
description: System Design — distill vision, business objectives, and mission statement from research input. Architect drives; PM ratifies. Reads designs/<product>/research/. Produces mission.md. Invoke as the first phase of system design, before [[the-method-requirements-analysis]].
---

# Business Alignment

## Canonical source

**Primary:** Löwy, *Righting Software*, [Chapter 5 §3 "Business Alignment"](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev1sec3) — Vision, Objectives, Mission Statement subsections.

**Supporting:**
- [Ch. 5 §3.1 "The Vision"](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev2sec8)
- [Ch. 5 §3.2 "The Business Objectives"](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev2sec9)
- [Ch. 5 §3.3 "Mission Statement"](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev2sec10)

The TradeMe walkthrough in ch. 5 is the worked example. Re-read it if the team has not yet internalized this phase.

## Input

- All files in `methodpoc/designs/<product>/research/`
- `methodpoc/designs/<product>/system/customer-input.md` (if PM has produced one)

## Output

`methodpoc/designs/<product>/system/mission.md` containing:

1. **Vision** — exactly one sentence. Terse and legal-statement-precise.
2. **Business Objectives** — numbered list. Business perspective only.
3. **Mission Statement** — how, expressed in components not features.
4. **Traceability table** — every objective maps to vision; every architectural concern will map back to an objective.

## Procedure

### Step 1 — Draft the vision

Read all research. Distill the system's purpose into one sentence.

**Rules from the book:**
- One sentence. Not a paragraph.
- "Terse and explicit, like a legal statement" (ch. 5).
- Example (TradeMe): *"A platform for building applications to support the TradeMe marketplace."*
- Do NOT include features, technologies, or metrics.

### Step 2 — Extract objectives from vision

Per ch. 5 §3.2:

- Numbered list.
- Business perspective only. No marketing slogans. No technology objectives. No specific feature requirements.
- Typical types (TradeMe had 7): unify repositories, quick turnaround, customization, business visibility, forward-looking, integration, security.
- Each objective is one sentence describing a business outcome.

**Hard rule** from ch. 5: *"you must not allow the engineering or marketing people to own the conversation, or to include technology objectives or specific requirements."*

If the PM or stakeholders try to inject either, push back. This conversation is business-stakeholder-led; the architect distills.

### Step 3 — Write the mission statement

Per ch. 5 §3.3:

- One paragraph maximum.
- Describes *how* the vision will be realized.
- Expressed in terms of **components**, not features.
- Example (TradeMe): *"Design and build a collection of software components that the development team can assemble into applications and features."*

**Critical observation** from ch. 5: *"The mission deliberately does not identify developing features as the mission. The mission is not to build features—the mission is to build components."* This framing compels volatility-based decomposition downstream.

### Step 4 — Verify bidirectional traceability

Build a small table at the bottom of `mission.md`:

| Objective | Supports vision? | Will trace to volatility / component? |
|---|---|---|

Every row must have "yes / how" in both columns. If you can't show traceability, the objective is probably marketing fluff or a feature in disguise — strip or rewrite.

### Step 5 — PM ratification

Hand to the Product Manager (or the user) for ratification. They review for:
- Does the vision capture business intent?
- Are objectives in business language?
- Does the mission align with what customers will see?

They do not author. They ratify or push back. If they push back, iterate until aligned.

## Exit criteria (for router)

`mission.md` exists and contains all four sections (vision, objectives, mission, traceability). Move to `the-method-requirements-analysis`.

## Anti-patterns to reject

- Multi-sentence "visions"
- Technology objectives ("use Kafka", "support 10k req/s")
- Marketing slogans ("delight customers", "transform the industry")
- Mission statements that name features
- Objectives without traceability to vision

## TradeMe reference example

See `ch05.xhtml#ch05lev1sec3` for the full TradeMe distillation. The architect produced:

- **Vision**: "A platform for building applications to support the TradeMe marketplace."
- **Objectives**: 7 numbered items, all business-perspective.
- **Mission**: "Design and build a collection of software components that the development team can assemble into applications and features."

Use this as a template for cadence and tone.
