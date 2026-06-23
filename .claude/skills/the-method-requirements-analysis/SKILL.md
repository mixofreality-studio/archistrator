---
name: the-method-requirements-analysis
description: System Design — build the glossary via the Four Questions and scrub solutions-masquerading-as-requirements. Architect drives both passes together. Reads mission.md and designs/<product>/research/. Produces glossary.md and scrubbed-requirements.md. Invoke after [[the-method-business-alignment]], before [[the-method-volatility-identification]].
---

# Requirements Analysis (Glossary + Scrubbing)

This phase pairs two architect-driven activities that must happen together: building the glossary (so terms are stable) and scrubbing solutions-masquerading-as-requirements (which often surface during glossary work).

## Canonical source

**Primary scrubbing:** Löwy, *Righting Software*, [Chapter 2 §3.3 "Solutions Masquerading as Requirements"](../../../../rightingsoftware/OEBPS/xhtml/ch02.xhtml#ch02lev2sec13).

**Primary glossary / naming:** [Chapter 3 §4.1 "What's in a Name"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev2sec8) and [§4.2 "The Four Questions"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev2sec9).

**Worked example:** [Ch. 5 "TradeMe Glossary"](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev2sec11).

**Standard reference:** [Appendix C §3.1d "Eliminate solutions masquerading as requirements"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec3) (System Design Guidelines, item 1d).

## Input

- `methodpoc/designs/<product>/system/mission.md` (from prior phase)
- All files in `methodpoc/designs/<product>/research/`
- `methodpoc/designs/<product>/system/customer-input.md` (if present)

## Outputs

1. `methodpoc/designs/<product>/system/glossary.md`
2. `methodpoc/designs/<product>/system/scrubbed-requirements.md`

## Procedure

### Pass 1 — Build the glossary (ch. 3)

Use the **Four Questions** to canvas the domain. Per ch. 3:

| Question | What it captures | Will later become |
|---|---|---|
| **Who** uses or interacts? | Actors, roles | Clients |
| **What** is required of the system? | Behaviors, use cases | Managers |
| **How** does it perform business activities? | Activities, algorithms | Engines |
| **How** does it access resources? | Verbs over data | ResourceAccess |
| **Where** does state live? | Stores, queues, external systems | Resources |

For each question, list every distinct domain noun or verb that appears in the research. Define each in one line using **customer language**.

Format `glossary.md`:

```markdown
# Glossary

## Actors (Who)
- **Tradesman** — a skilled service provider registered with the platform.
- **Contractor** — a tradesman managing a project on behalf of a customer.
...

## Behaviors (What)
- **Match Tradesman** — assign the best-fit tradesman to a customer request.
- **Onboard Tradesman** — register and verify a new tradesman.
...

## Activities (How)
- **Skill matching** — rank tradesmen by skill alignment with request.
- **Availability check** — filter tradesmen by current schedule.
...

## Resource access (How)
- **Credit tradesman account** — atomic verb increasing balance.
- **Search tradesman registry** — atomic verb querying registered set.
...

## Resources (Where)
- **Tradesman registry** — the persisted set of registered tradesmen.
- **Project store** — the persisted state of ongoing projects.
...
```

### Pass 2 — Scrub solutions-masquerading-as-requirements (ch. 2)

For each requirement statement in research, drive Löwy's interrogation:

1. **Is this a solution or a true requirement?**
2. **Are there other possible solutions?** If yes, this is a solution, not a requirement.
3. **What is the real requirement and underlying volatility?**
4. **Is the volatility itself a true requirement, or another solution?** (Recurse.)

Per ch. 2: *"Start by pointing out the solutions masquerading as requirements, and ask if there are other possible solutions? If so, then what were the real requirements and the underlying volatility? Once you identify the volatility, you must determine if the need to address that volatility is a true requirement or is still a solution masquerading as a requirement. Once you have finished scrubbing away all the solutions, what you are left with are likely great candidates for volatility-based decomposition."*

**Examples from the book to internalize:**

| Stated requirement | First scrub | Final scrub |
|---|---|---|
| "Send email" | Notify users; transport is volatile | (Final: notify users) |
| "Cooking" | Feeding | Well-being |
| "We need a queue" | User receives events | User receives events in order |
| "Add a notification service" | Notify users on state changes | Same — but architecture must encapsulate transport volatility |

Format `scrubbed-requirements.md`:

```markdown
# Scrubbed Requirements

| # | Original (from research) | Scrubbed requirement | Underlying volatility (hint for [[the-method-volatility-identification]]) |
|---|---|---|---|
| 1 | "Send confirmation email when order placed" | "Notify customer when order is placed" | Notification transport will vary by customer and over time |
| 2 | "Use Redis cache for hot data" | "Read-heavy access must be fast" | Storage technology may change |
| 3 | ... | ... | ... |
```

The **third column is critical** — it's the input to [[the-method-volatility-identification]]. Each scrubbed requirement should surface a candidate volatility.

### Pass 3 — Reconcile glossary and scrubbed requirements

Read both files. Check:
- Every actor in scrubbed requirements is in the glossary (under Who)
- Every behavior in scrubbed requirements maps to a glossary entry (under What)
- Where glossary terms differ from research wording, the glossary wins — update `scrubbed-requirements.md` to use glossary terms

## PM role

The PM is dispatched after architect produces drafts:
- Glossary: PM flags missing terms or misnamed concepts the customer would not recognize
- Scrubbed requirements: PM ratifies that the architect's "real requirement" still serves the customer's actual need

PM does not author either file.

## Exit criteria (for router)

Both `glossary.md` and `scrubbed-requirements.md` exist. Glossary has entries under all five Four-Question categories. Every research requirement appears in scrubbed-requirements.md with a candidate volatility hint. Move to `the-method-volatility-identification`.

## Anti-patterns to reject

- **CRUD-style entries** in the glossary ("create order", "update user") — these are implementations, not behaviors. Restate as business verbs.
- **Untouched requirements** in scrubbed-requirements.md — if the "scrubbed" column matches the "original" column exactly, you didn't interrogate hard enough.
- **Marketing names** in glossary — replace with operational terms.
- **Tech-stack names** anywhere — "Redis cache" is not a requirement, "fast read access" is.
