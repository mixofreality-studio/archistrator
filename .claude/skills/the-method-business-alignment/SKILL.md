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
3. **Mission Statement** — HOW the vision will be realized, stated in business and user language, 1–3 terse sentences (see Step 3 and the draft-job doctrine). (`Mission`)
4. **Traceability** — every objective maps to vision; every architectural concern will map back to an objective. (Captured in the objectives + mission; verify per Step 4.)

## Procedure

### Step 1 — Draft the vision

Read all research. Distill the system's purpose into one sentence.

**Rules from the book:**
- One sentence. Not a paragraph.
- "Terse and explicit, like a legal statement" (ch. 5).
- Example (TradeMe): *"A platform for building applications to support the TradeMe marketplace."*
- Do NOT include features, technologies, or metrics.

**Cadence rules (binding):**
- Target roughly **10–20 words** — TradeMe's vision is ten. If yours runs long, you are describing, not distilling.
- An **em-dash chain** ("X — enabling Y — so that Z") is a smuggled second sentence. Cut it.
- No **benefit-tail** that restates the objectives ("…so teams ship faster"). The WHY belongs to the objectives, not the vision.

**Technology carve-out:** the vision names no technologies. Sole exception: when a technology IS the founder's stated product identity (the founder frames the product itself as, e.g., "an X-powered Y"), at most ONE identity qualifier may name it. Competitive differentiators otherwise inform the mission's *how* and the objectives' *why* — they do not belong in the vision sentence.

### Step 2 — Extract objectives from vision

Per ch. 5 §3.2:

- Numbered list.
- Business perspective only. No marketing slogans. No technology objectives. No specific feature requirements.
- Typical types (TradeMe had 7): unify repositories, quick turnaround, customization, business visibility, forward-looking, integration, security.
- Each objective is one sentence describing a business outcome.

**Hard rule** from ch. 5: *"you must not allow the engineering or marketing people to own the conversation, or to include technology objectives or specific requirements."*

If the PM or stakeholders try to inject either, push back. This conversation is business-stakeholder-led; the architect distills.

### Step 3 — Write the mission statement

Per ch. 5 §3.3, the three artifacts divide cleanly: the **vision** is *what* the business will receive, the **objectives** are *why* the business wants it, and the **mission** is ***how* you will do it**. A mission that does not state a how is not a mission.

- 1–3 terse sentences (one short paragraph maximum).
- States a recognizable **approach** — the distinct way the vision will be realized. It MUST NOT merely restate the vision or the objectives in different words; a restatement is a defect, not a mission.
- Written **purely in business and user language** — no structural vocabulary (see the draft-job doctrine's MUST-NOT rule). A how is always expressible without decomposition terms: an operating approach like "standardize and automate the end-to-end workflow so every new market variation is a configuration change, not a rebuild" states a how with zero structural words.

**Book context (do not copy its vocabulary).** Löwy's TradeMe mission — *"Design and build a collection of software components that the development team can assemble into applications and features"* — uses "components" as a rhetorical device to compel volatility-based decomposition downstream (*"The mission is not to build features—the mission is to build components."*). For archistrator's committed `.mission` the founder's ruling is binding: the drafted mission text achieves the same forcing function WITHOUT software-decomposition vocabulary. Take from TradeMe the *shape* (a terse how, deliberately not a feature list) — never the word "components".

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

Produce the mission from the research corpus. The three parts divide per ch. 5: **vision = WHAT** the business will receive, **objectives = WHY** the business wants it, **mission = HOW you will do it**.

- The vision is ONE terse sentence naming the future the system creates — the WHAT. Cadence target ~10–20 words (TradeMe's is ten). An em-dash chain is a smuggled second sentence; no benefit-tail restating the objectives.
- Each objective is a numbered, measurable BUSINESS outcome (not a feature deliverable) — the WHY.
- The mission states **HOW the vision will be realized** — 1–3 terse sentences, in business and user language. It MUST state a recognizable approach, and it MUST NOT merely restate the vision or the objectives in different words: describing the business capability or the user value again is the vision's and objectives' job, and a mission that repeats them is a defect.
- First distill the 2-3 business pillars that DIFFERENTIATE this system from competitors; use them to frame the mission's *how* and the objectives' *why*. The vision itself stays technology-free — unless a technology IS the founder's stated product identity, in which case at most one identity qualifier may appear in it.
- Write the mission PURELY in business and user language: you MUST NOT use the words component, module, service, subsystem, layer, or any system-architecture / software-decomposition terminology, and you MUST NOT assert or imply any breakdown of the system into parts. The structural boundaries are derived LATER from volatility analysis in the Structure artifact — pre-deciding a decomposition here is a defect. This rule does NOT conflict with stating a how: a how is an operating approach ("standardize X", "automate Y end-to-end", "make every Z a configuration change") and is always expressible without structural vocabulary.
- **Constraints carry-forward (binding).** Founder-stated technical, deployment, or operational constraints (e.g. "will be operated by archistrator") are correctly EXCLUDED from the vision, objectives, and mission — but they MUST NOT be silently dropped. Enumerate every excluded constraint explicitly, verbatim where possible: name each one in your `publishDraft` message and in your review-thread responses (the typed `MissionStatement` model has no notes field, so the co-author thread IS the carry-forward channel), so requirements analysis scrubs them into `.scrubbedRequirements` and volatility identification receives them. Losing a stated constraint here is a defect.

**Reconciliation with the book framing above.** Löwy frames the mission as "build components, not features" (Step 3, and the TradeMe quote). That is his rhetorical device to *force* volatility-based decomposition downstream — it is NOT license to put software-decomposition words in the committed mission text. For archistrator's committed `.mission`, the business-and-user-language rule above is binding and stricter: the mission states the how in business and user language, and never uses component / module / service / subsystem / layer terminology.

## Exit criteria (for router)

`.aiarch/state/project.json` → `.mission` holds a typed `MissionStatement` with a one-sentence terse vision, business-only numbered objectives, and a mission that states HOW the vision will be realized in business-and-user language (no software-decomposition terminology, no restatement of the vision or objectives, per the draft-job doctrine above), with verified bidirectional traceability; any founder-stated constraints excluded from the artifact have been enumerated per the constraints carry-forward clause. Move to `the-method-requirements-analysis`.

## Anti-patterns to reject

- Multi-sentence "visions"
- Visions with em-dash-chained clauses or a benefit-tail (a smuggled second sentence)
- Technology objectives ("use Kafka", "support 10k req/s")
- Marketing slogans ("delight customers", "transform the industry")
- Mission statements that name features
- Mission restates vision/objectives — a mission that repeats the WHAT or the WHY in different words instead of stating a HOW
- Silently dropping founder-stated technical/deployment constraints instead of enumerating them (constraints carry-forward)
- Objectives without traceability to vision

## TradeMe reference example

See Löwy Ch. 5 §3 for the full TradeMe distillation. The architect produced:

- **Vision**: "A platform for building applications to support the TradeMe marketplace."
- **Objectives**: 7 numbered items, all business-perspective.
- **Mission**: "Design and build a collection of software components that the development team can assemble into applications and features."

Use this as a template for cadence and tone ONLY — not vocabulary. The drafted mission text must still obey the business-and-user-language MUST-NOT rule in the draft-job doctrine above (no component / module / service / subsystem / layer terminology); see the reconciliation note.
