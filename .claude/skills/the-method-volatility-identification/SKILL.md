---
name: the-method-volatility-identification
description: System Design — identify areas of volatility along the two axes. This is the architect's signature skill. Reads the committed .mission, .glossary, .scrubbedRequirements from project.json. Produces the typed Volatilities committed to project.json → .volatilities. Invoke after [[the-method-requirements-analysis]], before [[the-method-core-use-cases]].
---

# Volatility Identification

This is the most important phase of system design. The book (ch. 2): *"the whole purpose of requirements analysis is to identify the areas of volatility, and this analysis requires effort and sweat."*

## Canonical source

**Primary:** Löwy, *Righting Software*, [Chapter 2 §3 "Identifying Volatility"](../../../../rightingsoftware/OEBPS/xhtml/ch02.xhtml#ch02lev1sec3).

**Subsections:**
- [§3.1 "Volatile versus Variable"](../../../../rightingsoftware/OEBPS/xhtml/ch02.xhtml#ch02lev2sec11)
- [§3.2 "Axes of Volatility"](../../../../rightingsoftware/OEBPS/xhtml/ch02.xhtml#ch02lev2sec12)
- [§3.4 "Volatilities List"](../../../../rightingsoftware/OEBPS/xhtml/ch02.xhtml#ch02lev2sec14)
- [§3.6 "Resist the Siren Song"](../../../../rightingsoftware/OEBPS/xhtml/ch02.xhtml#ch02lev2sec16)
- [§3.7 "Volatility and the Business"](../../../../rightingsoftware/OEBPS/xhtml/ch02.xhtml#ch02lev2sec17)
- [§3.8 "Design for Your Competitors"](../../../../rightingsoftware/OEBPS/xhtml/ch02.xhtml#ch02lev2sec18)
- [§3.9 "Volatility and Longevity"](../../../../rightingsoftware/OEBPS/xhtml/ch02.xhtml#ch02lev2sec19)

**Worked example:** [Ch. 2 §3.5 "Example: Volatility-Based Trading System"](../../../../rightingsoftware/OEBPS/xhtml/ch02.xhtml#ch02lev2sec15) and [Ch. 5 "TradeMe Areas of Volatility"](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev2sec12).

## Input

State is git-as-DB: archistrator is a single Go-server repo whose canonical project state lives in `.aiarch/state/project.json` (a typed JSON aggregate). Markdown is a render-on-read of the typed state.

- The committed **mission** artifact → `.aiarch/state/project.json` → `.mission`
- The committed **glossary** artifact → `.glossary`
- The committed **scrubbedRequirements** artifact → `.scrubbedRequirements` — the underlying-volatility hint on each entry is your starting list of candidate volatilities
- The research corpus in `project.json` (re-read for context)

## Output

The typed **`Volatilities`** model (Go shape in `server/internal/resourceaccess/projectstate/models_phase1.go`), committed to **`.aiarch/state/project.json` → `.volatilities`** — the **Volatilities List**, grouped by axis. NOT a `volatilities.md` file; any markdown is a render-on-read of this slot.

Per the two usage patterns (agentic/CI dispatch and local interactive): the agent emits the typed `Volatilities` JSON and commits it into `.volatilities`; the server stages it (`StageArtifactForReview`) for the human review gate.

## Procedure

The architect owns this phase. **The PM is not involved in authoring.** They may flag a missing customer-driven volatility after the fact.

### Step 1 — Apply the two axes of volatility (ch. 2 §3.2)

Interrogate the requirements through two independent lenses:

**Axis 1 — Same customer over time.** For each requirement, ask:
- *"Even if presently the system is perfectly aligned with this customer's needs, over time what in their business context will change?"*
- What will the customer want differently in 1 year? 3 years? 5 years?
- The system's projected lifespan is the horizon.

**Axis 2 — All customers at the same point in time.** For each requirement, ask:
- *"Are all your customers now using the system in exactly the same way? What are some of them doing that is different?"*
- Variations across markets, regulations, languages, customer types, sizes.

The axes MUST be **independent**. If areas of change span both axes, you're probably looking at functional decomposition in disguise (ch. 2). Reconsider.

### Step 2 — Iterative design factoring (ch. 2 §3, Figure 2-10)

This is the procedural mechanic that produces the volatilities list:

1. Start with the system as one notional component.
2. Ask: *"Could this single component, as-is, serve this customer forever?"* — wherever the answer is "no, this will change," encapsulate it as a candidate volatility.
3. Ask: *"Could this single component serve all current customers?"* — wherever the answer is "no, this differs across customers," encapsulate it as a candidate volatility.
4. Now repeat steps 2 and 3 against each newly-encapsulated piece. Continue until every point on both axes is encapsulated.

The output is a list. Order doesn't matter yet. Names will be refined.

### Step 3 — Apply volatile-vs-variable filter (ch. 2 §3.1)

For each candidate volatility, classify:

- **Volatile** = open-ended; if not encapsulated in a component, change would ripple across the system. *Keep.*
- **Variable** = handled by conditional logic in code. Not architectural. *Reject.*

Per ch. 2: *"You should be on the lookout for the kind of changes or risks that would have ripple effects across the system. Changes must not invalidate the architecture."*

Examples:
- "Currency formats per locale" → variable (handled by a formatter)
- "How users are authenticated" → volatile (OAuth, SSO, password, biometric all require structural support)

### Step 4 — Apply heuristics (ch. 2 §3.7–3.9)

For each surviving entry, sanity-check using:

**Longevity heuristic (§3.9):** *"the more frequently things change today, the more likely they will change in the future, at the same rate."* Things that have been stable forever probably aren't volatile.

**Design for competitors (§3.8):** Could a direct competitor use your system? Where you'd hit a barrier, that's a volatility you must encapsulate. Where you and competitors do things identically, that's *nature of the business* — do NOT encapsulate.

**Volatility and the business (§3.7):** Distinguish business-level volatility from individual-customer volatility. Both belong; flag which axis each came from.

### Step 5 — Reject anti-patterns

Strip from the list:

- **Speculative encapsulation** — any volatility for which: (a) change is rare AND (b) any encapsulation would be poor. Per ch. 2, this is over-design.
- **Nature-of-the-business entries** — things every competitor does identically. Even if "could conceivably change," the entire industry would have to change. App C: *"Do not encapsulate changes to the nature of the business."*
- **Siren-song habits (§3.6)** — a "Logging block" or "Reporting block" you'd add by reflex without a real business volatility behind it. Per ch. 2: *"just because you always have had a reporting block, or even because you have an existing reporting block, does not mean you need a reporting block."*

### Step 6 — Format the Volatilities List (ch. 2 §3.4)

Render the typed `Volatilities` committed to `.volatilities` matching the format from ch. 2 §3.5 (trading system example) and ch. 5 (TradeMe). The markdown below is the render-on-read view; the JSON slot is the source of truth:

```markdown
# Volatilities List

## Axis 1 — Same customer over time

**User volatility**
Customers will add new user types as the business expands (currently 2; expected 5+ within 3 years). User authentication providers will change as identity standards evolve. Encapsulating user concept and authentication independently insulates the system from these shifts.

**Notification volatility**
Customers will adopt new notification channels (email today; SMS, push, in-app, webhook expected). Transport selection must be replaceable without touching workflow code.

...

## Axis 2 — All customers at the same point in time

**Storage volatility**
Different customers use different databases (Postgres for SaaS tier; SQL Server for enterprise; SQLite for offline customers). Persistence access must be technology-neutral at the seam.

**Security volatility**
Enterprise customers require SAML; consumer tier uses OAuth; some regulated customers require client certs. Authentication scheme must be swappable per deployment.

...
```

**Format rules:**
- Bold volatility name (Pascal-case + "volatility" suffix is the convention)
- Rationale paragraph below
- Group by axis
- Each entry must trace to at least one scrubbed requirement
- Aim for ~6–15 entries total

### Step 7 — Cross-check independence of axes

Re-read the list. If a single volatility entry naturally belongs to both axes, that's a signal it might be a feature in disguise (functional decomposition). Split or rename.

## Exit criteria (for router)

`.aiarch/state/project.json` → `.volatilities` holds the typed `Volatilities` model, grouped by axis, with ~6–15 entries. Each entry has a rationale. No nature-of-business or speculative entries remain. Move to `the-method-core-use-cases`.

## Common mistakes

- **Too few volatilities** (< 5) — you're missing axes; revisit Step 2.
- **Too many** (> 20) — you're cataloguing every variation; apply the volatile-vs-variable filter more aggressively (Step 3).
- **Volatilities that read like features** ("Search volatility", "Reporting volatility") — restate as the underlying change, not the feature.
- **Volatilities that span both axes** — split or reconsider.
- **Volatilities with no business rationale** — strip; if you can't justify it to a stakeholder, it's not real.

## Anchor for downstream phases

Every entry in the committed `.volatilities` slot will eventually map to **at most one component** in the architecture. [[the-method-architecture]] will take this list as input. So get this right — bad volatilities → bad decomposition → bad architecture.
