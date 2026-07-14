---
name: the-method-business-alignment
description: System Design — distill vision, business objectives, and mission statement from research input. Architect drives; PM ratifies. Reads the research corpus in project.json. Produces the typed MissionStatement committed to project.json → .mission. Invoke as the first phase of system design, before [[the-method-requirements-analysis]].
---

# Business Alignment

## Canonical source

**Primary:** Löwy, *Righting Software*, Chapter 5 §3 "Business Alignment" — Vision, Objectives, Mission Statement subsections.

**Supporting:**
- Ch. 5 §3.1 "The Vision"
- Ch. 5 §3.2 "The Business Objectives"
- Ch. 5 §3.3 "Mission Statement"

The TradeMe walkthrough in ch. 5 is the worked example. Re-read it if the team has not yet internalized this phase.

## Input

State is git-as-DB: archistrator is a single Go-server repo whose canonical project state lives in `.aiarch/state/project.json` (a typed JSON aggregate). There are no `designs/<product>/*.md` files — any markdown is a render-on-read of the typed state.

- The research corpus in `.aiarch/state/project.json` (the ingested business briefs, customer interviews, competitor analysis, market analysis, prior-system docs)
- The PM's customer-input notes carried in project state (if the PM has produced any)

## Output

The typed **`MissionStatement`** model (Go shape in `internal/resourceaccess/projectstate/models_phase1.go`: `Vision string`, `Objectives []Objective`, `Mission string`), committed to **`.aiarch/state/project.json` → `.mission`**. NOT a `mission.md` file — any markdown rendering is produced render-on-read from this slot.

Two usage patterns produce the same result:
1. **Agentic / CI dispatch** — the agent emits the typed `MissionStatement` JSON and commits it into `.mission` on its session branch; the server reads it back and stages it (`StageArtifactForReview`) for the human review gate.
2. **Local interactive** (a human running `/system-design` in Claude Code) — same: produce the typed model and write it into the `.mission` slot. Never a `designs/*.md` file.

The model carries:

1. **Vision** — exactly one sentence. Terse and legal-statement-precise. (`Vision`)
2. **Business Objectives** — numbered list. Business perspective only. (`Objectives`, each an `Objective`)
3. **Mission Statement** — how, expressed in components not features (in Löwy's book framing — the draft-job doctrine below constrains the drafted narrative to business/user language; see the reconciliation note). (`Mission`)
4. **Traceability** — every objective maps to vision; every architectural concern will map back to an objective. (Captured in the objectives + mission; verify per Step 4.)

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

Before committing the `MissionStatement`, walk each objective through this table (a render-on-read view of the slot; you do not persist the table separately):

| Objective | Supports vision? | Will trace to volatility / component? |
|---|---|---|

Every row must have "yes / how" in both columns. If you can't show traceability, the objective is probably marketing fluff or a feature in disguise — strip or rewrite.

### Step 5 — PM ratification

Hand to the Product Manager (or the user) for ratification. They review for:
- Does the vision capture business intent?
- Are objectives in business language?
- Does the mission align with what customers will see?

They do not author. They ratify or push back. If they push back, iterate until aligned.

## Draft-job doctrine (CI dispatch)

This is the normative task the CI draft job (and a local `/system-design` run) executes to produce the `MissionStatement`. It is self-contained: everything a draft agent needs to draft a sound mission is stated here.

Produce the mission from the research corpus.

- The vision is ONE terse sentence naming the future the system creates.
- First distill the 2-3 business pillars that DIFFERENTIATE this system from competitors; ground the vision, mission, and objectives in those.
- The mission narrative describes the BUSINESS CAPABILITY and USER-FACING VALUE of the end-to-end workflow — why it matters, and what outcome or trust it produces for the user — NOT a feature list.
- Write it PURELY in business and user language: you MUST NOT use the words component, module, service, subsystem, layer, or any system-architecture / software-decomposition terminology, and you MUST NOT assert or imply any breakdown of the system into parts. The structural boundaries are derived LATER from volatility analysis in the Structure artifact — pre-deciding a decomposition here is a defect.
- Each objective is a numbered, measurable BUSINESS outcome (not a feature deliverable).

**Reconciliation with the book framing above.** Löwy frames the mission as "build components, not features" (Step 3, and the TradeMe quote). That is his rhetorical device to *force* volatility-based decomposition downstream — it is NOT license to put software-decomposition words in the committed mission text. For archistrator's committed `.mission`, the business-and-user-language rule above is binding and stricter: the mission narrative names business capability and user-facing value, and never uses component / module / service / subsystem / layer terminology.

## Exit criteria (for router)

`.aiarch/state/project.json` → `.mission` holds a typed `MissionStatement` with a one-sentence vision, business-only numbered objectives, a business-and-user-language mission (no software-decomposition terminology per the draft-job doctrine above), and verified bidirectional traceability. Move to `the-method-requirements-analysis`.

## Anti-patterns to reject

- Multi-sentence "visions"
- Technology objectives ("use Kafka", "support 10k req/s")
- Marketing slogans ("delight customers", "transform the industry")
- Mission statements that name features
- Objectives without traceability to vision

## TradeMe reference example

See Löwy Ch. 5 §3 for the full TradeMe distillation. The architect produced:

- **Vision**: "A platform for building applications to support the TradeMe marketplace."
- **Objectives**: 7 numbered items, all business-perspective.
- **Mission**: "Design and build a collection of software components that the development team can assemble into applications and features."

Use this as a template for cadence and tone ONLY — not vocabulary. The drafted mission text must still obey the business-and-user-language MUST-NOT rule in the draft-job doctrine above (no component / module / service / subsystem / layer terminology); see the reconciliation note.
