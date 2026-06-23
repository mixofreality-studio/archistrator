---
name: the-method-handoff
description: Choose the construction hand-off model (senior, senior-as-junior-architect, or junior). Documents the team's contract-design ownership and review chain. Use once at the start of Phase 3 / construction, before any detailed-design activity begins.
---

# Hand-Off

The hand-off is the contract between the architect and the people who will design the *details* of each service and then construct it. It is a one-time decision at the start of construction. Pick the wrong model and either the architect becomes a bottleneck (junior hand-off) or the project absorbs untracked rework (uncoordinated senior hand-off).

This skill is invoked once, at the start of Phase 3, before the first detailed-design activity begins. It produces `handoff.md`, which is then referenced by every per-component contract design via `[[the-method-service-contract]]`.

Per `[[the-method-doctrine]]` Directive 5: *Design iteratively, build incrementally.* The hand-off model determines who does the iterative detailed design while construction proceeds.

## Canonical source

**Primary:** Löwy, [Ch. 14 §5 "The Hand-Off"](../../../../rightingsoftware/OEBPS/xhtml/ch14.xhtml#ch14lev1sec5).

Sub-sections walked below:
- [§5 "Junior Hand-Off"](../../../../rightingsoftware/OEBPS/xhtml/ch14.xhtml#ch14lev2sec9)
- [§5 "Senior Hand-Off"](../../../../rightingsoftware/OEBPS/xhtml/ch14.xhtml#ch14lev2sec10)
- [§5 "Senior Developers As Junior Architects"](../../../../rightingsoftware/OEBPS/xhtml/ch14.xhtml#ch14lev2sec11)

**Standard reference:** [App C §6 "Service Contract Design Guidelines"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec6) items 5 ("Avoid junior hand-offs") and 6 ("Have only the architect or competent senior developers design the contracts").

## Input

- `methodpoc/designs/<product>/project/sdp-review.md` — the management-committed option (normal / decompressed / compressed / subcritical) sets the duration that the hand-off must support
- `methodpoc/designs/<product>/project/planning-assumptions.md` — the named team roster with seniority indication
- `methodpoc/designs/<product>/system/architecture.dsl` — gives the count of services that need detailed design

## Output

`methodpoc/designs/<product>/implementation/handoff.md`

This file is consumed by every subsequent invocation of `[[the-method-service-contract]]` to decide *who* designs each contract and *who* reviews it.

## Procedure

### Step 1 — Inventory the team

From `planning-assumptions.md`, classify each developer along Löwy's seniority axis. Per Ch. 14 §5: *"senior developers are those capable of designing the details of the services, whereas junior developers cannot."* This is **not** years of experience.

| Name | Role per planning-assumptions | Capable of detailed design? | Source of confidence |
|---|---|---|---|
| dev-1 | senior | Yes | prior project review, architecture portfolio |
| dev-2 | senior | With mentoring | needs one or two services walked alongside the architect |
| dev-3 | junior | No | brand-new to the domain |
| ... | ... | ... | ... |

If you have zero senior-capable developers, your only option is the junior hand-off — and Ch. 14 §5 plus App C §6.5 both warn that this dooms the project. Surface this to management before construction begins, not after.

### Step 2 — Count the contracts to be designed

From `architecture.dsl`, count the components that will need detailed design (every Manager, every Engine, every ResourceAccess that exposes a service contract). That is the workload to absorb.

Cross-check with the SDP-chosen option's duration: if the architect were to do every contract personally (junior hand-off), would that fit inside the front-end allocated by the chosen option, or would it consume detailed-design time during construction? Ch. 14 §5 notes that for a 12-month project the architect could spend 3–4 months on detailed design alone — that is the size of the burden the junior hand-off creates.

### Step 3 — Choose the hand-off model

The three models, scoring criteria as the book defines them:

| Model | Who designs contracts | Who reviews | Quality | Architect bottleneck | When to use |
|---|---|---|---|---|---|
| **Senior hand-off** | Each senior developer designs their own service contracts | Architect reviews and amends | Highest | Lowest — review only | Team has enough senior developers to cover the service inventory |
| **Senior-as-junior-architect** | A small group of senior developers acts as junior architects — they design contracts in batches *in parallel* with juniors constructing the previous batch | Architect reviews each batch before hand-off to juniors | High when synchronised, requires meticulous project design | Moderate — architect reviews + extra integration points needed | Team has 1–3 senior developers and a larger junior pool |
| **Junior hand-off** | Architect does every contract | Architect | Variable | Highest — architect becomes a bottleneck or front-end stretches to 25–33% of duration | Last resort. Both Ch. 14 §5 and App C §6.5 warn against this. |

Per Ch. 14 §5: *"The senior hand-off is the safest way of accelerating any project because it compresses the schedule while avoiding changes to the critical path, increasing the execution risk, or introducing bottlenecks."* If the team supports it, default to senior hand-off.

Per Ch. 14 §5 on the senior-as-junior-architect model: *"This is the best and only way of mitigating the risks of the junior hand-off."* When you cannot run a pure senior hand-off, this is the right fallback — not the junior hand-off.

### Step 4 — Wire the review chain

Whichever model is chosen, the **architect reviews every contract before it goes to construction**. The chain differs only in who drafts:

- Senior hand-off: senior developer drafts → architect reviews → developer constructs.
- Senior-as-junior-architect: senior-as-junior-architect drafts → architect reviews → junior developer constructs → senior reviews code → junior integrates.
- Junior hand-off: architect drafts → architect reviews (self) → junior constructs → architect reviews code.

**Per-change reviews during construction** — the neighbour-contract review, the design-alignment review, and the UI-conformance review that fire on each produced change — are computed dynamically by `[[the-method-review-routing]]`, not assigned here. This skill decides **who can fill each role** (human vs AI, per `HandOffPolicy`); review-routing decides **which roles must review a given change** (per `ReviewPolicy`). Handoff casts the actors; review-routing draws the graph.

The review chain is the part of the hand-off that is non-negotiable. Skipping architect review on any contract is equivalent to skipping the hand-off entirely.

### Step 5 — Schedule the design-construction pipeline

For the senior-as-junior-architect model especially, the pipeline must be explicit (Ch. 14 §5): *"You must know exactly how many services you can design in advance and how to synchronize the hand-offs with the construction."*

Document, in `handoff.md`:

- Batch size — how many services are designed in each pass before juniors begin constructing them
- Lead distance — how far ahead of construction the design batch must stay
- Buffer — extra integration activities to absorb design-construction de-synchronisation

These numbers feed back into `network.yaml` as detailed-design activities. If the project-design phase did not include them, the network is now under-specified — return to Phase 2 to add them before declaring the hand-off complete.

### Step 6 — Write `handoff.md`

Format:

```markdown
# Hand-Off — <Product>

Date: <YYYY-MM-DD>
Architect: <name>

## Chosen model

<Senior hand-off | Senior-as-junior-architect | Junior hand-off>

### Rationale
- Number of senior-capable developers: N (named below)
- Service contracts to design: M (from architecture.dsl)
- SDP-chosen option duration: <duration> (from sdp-review.md)
- Why this model fits: ...

## Team roster (from planning-assumptions.md)

| Name | Role | Capable of detailed design | Assignment |
|---|---|---|---|
| dev-1 | senior | Yes | Contract design + construction |
| dev-2 | senior | With mentoring | Contract design (junior architect) |
| dev-3 | junior | No | Construction only |

## Contract design assignment

| Component (from architecture.dsl) | Designer | Reviewer | Constructor |
|---|---|---|---|
| OrderManager | dev-1 | architect | dev-1 |
| PricingEngine | dev-2 | architect | dev-3 |
| InventoryAccess | architect | (self-review with peer) | dev-3 |
| ... | ... | ... | ... |

## Review chain

1. Designer drafts contract per `[[the-method-service-contract]]`.
2. Architect reviews contract; amends or returns for rework.
3. Designer hands the approved contract to the constructor.
4. Constructor writes the test plan and the service.
5. Code review with <senior peer | architect>.
6. Integration.

## Pipeline (if senior-as-junior-architect)

- Batch size: N services per design pass
- Lead distance: design batch must complete K days ahead of construction start
- Integration buffer: M extra integration activities have been added to network.yaml v<N>

## Risks accepted

- <e.g., dev-2 is mid-mentorship; first contract she designs will take longer than later ones>
- <e.g., architect availability for review is 50% during weeks 6–8 due to external commitments>

## What this hand-off does NOT cover

- Per-service contract design discipline — see `[[the-method-service-contract]]`
- Construction sequencing — see `network.yaml`
- Weekly tracking — see `[[the-method-project-tracking]]`
- Per-change review routing during construction — see `[[the-method-review-routing]]`
```

## Exit criteria (for router)

- `handoff.md` exists
- A model is explicitly named (senior / senior-as-junior-architect / junior)
- Every component in `architecture.dsl` has a designer, reviewer, and constructor assigned by name
- Review chain is stated (no implicit reviews)
- If senior-as-junior-architect: batch size, lead distance, and any extra detailed-design activities have been reflected in `network.yaml`
- If junior hand-off was chosen, a written justification exists (it is otherwise rejected per App C §6.5)

Hand to `[[the-method-service-contract]]` for the first batch of contract design activities.

## When to revisit

- Team composition changes (key senior leaves; junior promoted) — re-run this skill, produce `handoff-<NN>.md`
- Hand-off model proves unworkable in practice (architect becomes a bottleneck despite senior model) — re-run with new roster assumptions
- Scope change triggers re-entry into Phase 2 via `[[the-method-scope-change]]` — the new SDP may need a different hand-off model

## Anti-patterns to reject

- **"Architect designs every contract"** — that is the junior hand-off. Both Ch. 14 §5 and App C §6.5 warn against it. Use it only as a last resort, with a written justification, and pair it with the architect-as-bottleneck risk on the SDP risk register.
- **Skipping the hand-off discussion entirely** — proceeding to construction with no documented model means contract quality is unmanaged and architect review is implicit (which means it will be skipped). The skill exists because the decision must be deliberate.
- **Senior hand-off without senior developers** — calling junior developers "senior" by title does not make them capable of detailed design. Ch. 14 §5 defines seniority by capability, not tenure.
- **No architect review** — every model retains architect review of every contract. A senior hand-off without architect review is not the senior hand-off; it is uncontrolled design.
- **Promising senior-as-junior-architect without the project-design support** — that model requires extra detailed-design activities and integration points in `network.yaml`. Without them, the pipeline desynchronises and juniors are blocked waiting for designs.
