---
name: the-method-architecture
description: Decompose the system into layered components and express the decomposition as Structurizr DSL with one dynamic view per core use case. Call-chain validation iterates back into the decomposition — they are one activity, not two. Produces architecture.dsl. Use after core use cases, before operational concepts.
---

# The Method — Architecture

This skill produces the system's static architecture and validates it by tracing every core use case as a call chain. **Decomposition and DSL writing are a single loop**, not two phases: when a call chain fails to draw, the decomposition is wrong, and both the components and the DSL must be revised together. There is no intermediate component table — `architecture.dsl` is the only artifact.

## Cross-cutting references

- [[the-method-layers]] — the canonical layer model, naming conventions, interaction rules, interaction don'ts, and cardinality limits. **This skill does not restate them.** When you need a rule, link there.
- [[the-method-doctrine]] — the Prime Directive and 9 directives. This skill operationalises Directives 1 (Avoid functional decomposition), 2 (Decompose based on volatility), 3 (Provide a composable design), and 4 (Features as integration, not implementation).
- `STRUCTURIZR-CONVENTIONS.md` (sibling file in this skill) — the Structurizr DSL template, tag conventions, edge-label conventions, and styling. Read it before writing the DSL. **Architecture diagrams are infrastructure- and platform-agnostic**: edge labels must use the vocabulary of the destination layer (manager method, engine method signature, atomic business verb), never workflow-engine primitives (`Activity:`, `StartWorkflow(...)`, `SignalExternalWorkflow`) and never platform-specific commands (`git commit`, `ArgoCD reconcile`, `POST /charges`). Infrastructure and platform detail belong in `operational-concepts.md`.

## Canonical source

**Primary:**
- Löwy, [Chapter 3 "Structure"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml) — layers, classification, layering rules, Design Don'ts.
- [Ch. 4 §2 "Composable Design"](../../../../rightingsoftware/OEBPS/xhtml/ch04.xhtml#ch04lev1sec2) — the smallest-set principle.
- [Ch. 4 §2.2 "Architecture Validation"](../../../../rightingsoftware/OEBPS/xhtml/ch04.xhtml#ch04lev2sec4) — call chains validate decompositions.
- [Ch. 5 §4 "The Architecture"](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev1sec4) and [§6 "Design Validation"](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev1sec6) — TradeMe worked example.

**Supporting:**
- [Ch. 3 §4 "Classification Guidelines"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev1sec4) — the Four Questions, naming.
- [Ch. 3 §6 "Open and Closed Architectures"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev1sec6) and [§6.5 Design Don'ts](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev2sec18).
- [Appendix C §3 "System Design Guidelines"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec3) — items 2, 3, 4, 5, 6.

**Structurizr docs:** https://docs.structurizr.com/dsl.

## Input

- `methodpoc/designs/<product>/system/volatilities.md`
- `methodpoc/designs/<product>/system/core-use-cases.md`
- `methodpoc/designs/<product>/system/glossary.md`
- `methodpoc/designs/<product>/system/mission.md`

## Output

1. `methodpoc/designs/<product>/system/architecture.dsl` — the canonical artifact. Contains the static architecture, one dynamic view per core use case, and 2–3 non-core dynamic views demonstrating versatility.
2. `methodpoc/designs/<product>/system/workspace.dsl` — copy of `architecture.dsl` so Structurizr Lite can render it (it expects the filename `workspace.dsl`).
3. `methodpoc/designs/<product>/system/sequence-diagrams/<use-case>.md` — supplementary PlantUML sequence diagrams *only* where order, duration, or multiplicity is non-obvious from the dynamic view.

There is no intermediate "component table" file. The DSL is the canonical home for the component list — the `container` declarations *are* the table.

## The loop

This skill is one iterative loop:

```
classify volatilities → name components → write DSL → trace each core use case
                                ↑                                       │
                                └─────── revise both ◄──────────────────┘
                                  (if any trace fails)
```

A trace that cannot be drawn cleanly is a signal that the decomposition is wrong. Fix decomposition + DSL together. **Never** twist a use case to fit a bad decomposition.

## Procedure

### Step 1 — Classify volatilities into layer bins (the Four Questions)

Per [Ch. 3 §4.2 "The Four Questions"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev2sec9):

> *"Make a list of all the 'who' and put them in one bin as candidates for Clients. Make a list of all the 'what' and put them in another bin as candidates for Managers, and so on... The result will not be perfect... but it is a start."*

Walk every entry in `volatilities.md`. Bin it:

| Question | Bin → Layer |
|---|---|
| Who interacts with the system? | Clients |
| What is required of the system? | Managers |
| How (business activity)? | Engines |
| How (resource access)? | ResourceAccess |
| Where (state)? | Resources |
| Cross-cutting concern with cappuccino-machine reusability? | Utilities |

If a volatility lands in two bins, it is either two volatilities or the volatility statement is ambiguous — refine `volatilities.md` before proceeding. See [[the-method-layers]] for full layer identity rules.

### Step 2 — Name and classify each candidate component

For each bin entry, name the component using the conventions in [[the-method-layers]] (e.g., `<Noun>Manager`, `<Gerund>Engine`, `<Noun>Access`). Verify each component:

- Encapsulates **exactly one** volatility from `volatilities.md`.
- Sits in **exactly one** layer.
- Passes the layer's identity test (e.g., Engines do no I/O; ResourceAccess exposes business verbs not CRUD; Utilities pass the cappuccino-machine test).

If a candidate fails its identity test, either re-classify it or split it. If you cannot decide, the volatility itself is probably wrong — return to `the-method-volatility-identification`.

### Step 3 — Check cardinality and smallest-set

Apply the cardinality limits from [[the-method-layers]] (≤5 Managers without subsystems, golden Engines-to-Managers ratio, ~10 components order-of-magnitude, ≥8 Managers is a hard fail).

Then apply the smallest-set test from [Ch. 4 §2](../../../../rightingsoftware/OEBPS/xhtml/ch04.xhtml#ch04lev1sec2):

> *"Once you cannot think of a smaller set of building blocks, you have found your best design."*

If you can reduce further without losing a volatility, reduce.

### Step 4 — Reject anti-patterns

Before writing DSL, scan the candidate set for these smells. Any hit means restart from Step 1 with the smell as a guard:

| Anti-pattern | Indicator |
|---|---|
| Functional decomposition | Components named after features (`OrderProcessing`, `Reporting`, `Notifications`) |
| Domain decomposition | Components named after domains/entities (`UserService`, `ProductService`) |
| God service | One Manager doing everything |
| Services explosion | One component per use case |
| Chained services | A→B→C where B presumes A or C |
| Speculative encapsulation | Component for a future need with no current volatility |
| Reflex components | `Logging`/`Reporting` declared by reflex with no specific volatility behind it |

### Step 5 — Bootstrap the DSL from the convention template

Open `STRUCTURIZR-CONVENTIONS.md` (sibling to this SKILL.md). Copy the template into `architecture.dsl`. The template provides the `workspace`, `model`, `views`, and `styles` blocks plus the per-layer tags (`client`, `manager`, `engine`, `resource-access`, `resource`, `utility`).

### Step 6 — Write `container` declarations for every component

For each component identified in Step 2, write a `container` inside the `softwareSystem`:

```dsl
<componentIdentifier> = container "<Display Name>" "<one-line volatility description>" "<technology>" "<layer-tag>"
```

Conventions:
- `componentIdentifier` is camelCase matching the component name (e.g., `orderManager`).
- Display name is human-readable ("Order Manager").
- Description: **≤ 150 characters**, names the volatility encapsulated plus a brief verb-phrase for what it does. See "Description style" in `STRUCTURIZR-CONVENTIONS.md` for per-layer patterns. Implementation detail (retention, idempotency mechanics, schema, rationale) belongs in `operational-concepts.md`, `volatilities.md`, or a comment block above the element — NOT in the on-element description.
- Technology: stack hint (e.g., `kotlin`, `react`, `postgres`).
- Layer tag is exactly one of: `client`, `manager`, `engine`, `resource-access`, `resource`, `utility`.

Add `person` declarations for every actor named in core use cases.

### Step 7 — Add only the relationships needed for core use cases

Do NOT exhaustively wire every plausible edge. Add only the relationships actually exercised by core use cases (Step 8 will exercise them).

Every relationship must comply with the closed-architecture rules in [[the-method-layers]]: no calling up, no sideways within a layer except queued Manager→Manager, no skipping layers, plus the don'ts (Engines don't receive queued calls, Engines/ResourceAccess/Resources don't publish events, etc.).

Mark sync vs queued in the relationship description (e.g., `"(queued) places order"`) or via a tag — both render distinctly in the styles block.

**Edge-label vocabulary.** Labels use the vocabulary of the **destination layer's responsibility**. The architecture stays infrastructure- and platform-agnostic; the infrastructure-specific primitives go in `operational-concepts.md`. See `STRUCTURIZR-CONVENTIONS.md` "Edge-label conventions" for the full table and the rule-of-thumb test. Quick summary:

| Edge | Label shape |
|---|---|
| Client → Manager | `<managerMethodName>(<args>) → <result>` |
| Manager → Engine | `<EngineMethodName>(<args>) → <output>` |
| Manager → ResourceAccess | `<atomicBusinessVerb>(<noun>)` (e.g., `appendEvent(OrderSubmitted)`) |
| Manager → infrastructure-access ResourceAccess | atomic verbs in the infrastructure's *generic* domain (e.g., `awaitSignal(reviewDecision)`, `scheduleNextActivity`) — never workflow-engine product primitives |
| Manager → Manager (queued) | `delivers <SignalName> (queued)` |
| ResourceAccess → Resource | resource-domain verb + idempotency note — no platform-specific commands (no `git commit`, no `INSERT … ON CONFLICT`, no `POST /charges`) |

### Step 8 — Write the static-architecture view

Inside `views`:

```dsl
container <systemIdentifier> "static-architecture" "Layered static architecture per The Method." {
    include *
    autolayout tb
}
```

Top-to-bottom layout puts Clients at the top and Resources at the bottom — matches the layered pyramid mental model.

### Step 9 — Write one dynamic view per core use case (the validation)

For each core use case in `core-use-cases.md`, write a `dynamic` view:

```dsl
dynamic <systemIdentifier> "<use-case-key>" "<Use Case Name>" {
    <actor> -> <client>     "<actor action>"
    <client> -> <manager>   "<API call>"
    <manager> -> <engine>   "<method call>"
    <manager> -> <access>   "<atomic business verb>"
    <access> -> <resource>  "<I/O>"
    autolayout lr
}
```

This is the **call chain** referenced throughout Chapter 4 and 5 of the book. The dynamic view IS the validation artifact — drawing it cleanly proves the decomposition supports the use case.

**Suspend/resume use cases.** If the use case suspends (waiting for an external event) and resumes, do not draw the infrastructure's `awaitSignal` edge inside the dynamic view — the infrastructure ResourceAccess is omitted from dynamic views (see `STRUCTURIZR-CONVENTIONS.md` "Infrastructure ResourceAccess is omitted from dynamic views"). The suspension is implied by the order of edges: the Manager's last pre-suspend verb (typically `appendEvent(<Something>AwaitingReview)`) is followed by the Client's resume call (e.g., `submitReviewDecision`).

**Per-view validation rules:**

| Rule | If broken, the failure means |
|---|---|
| Exactly one Manager appears as entry-from-Client | Don't 6a violated — Client → multiple Managers; decomposition wrong |
| Every edge respects closed-layer rules | Layer rule violation; decomposition wrong (see [[the-method-layers]]) |
| Engines/ResourceAccess/Resources are not publishers or subscribers | A Don't is violated (see [[the-method-layers]]) |
| Every step has a meaningful action label, not generic verbs | Documentation incomplete |
| The last edge produces the outcome named in the core use case | Use case incomplete — either the decomposition is missing a component, or a component's responsibilities are wrong |
| Every edge label uses the destination-layer vocabulary (no `Activity:` / `StartWorkflow(` / `git commit` / `POST /endpoint` style labels) | The label is leaking workflow-engine or platform implementation into the architecture — rewrite per `STRUCTURIZR-CONVENTIONS.md` "Edge-label conventions" |
| No dynamic-view edge targets a infrastructure ResourceAccess | The infrastructure is implementation; static-architecture edges retain it, dynamic views do not |

**If any rule fails, the decomposition is wrong — not the use case.** Return to Step 1, revise the component set, regenerate the affected `container` declarations and relationships, and re-trace. Iterate until every core use case draws cleanly.

### Step 10 — Demonstrate versatility with 2–3 non-core call chains

Per [Ch. 5 §6](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev1sec6): validate that the architecture also handles non-core use cases without modification. Pick 2–3 entries from the rejection table in `core-use-cases.md` and write their dynamic views in the same DSL.

If a non-core use case cannot be drawn either, the decomposition is missing a volatility — return to `the-method-volatility-identification` before continuing.

### Step 11 — Add sequence diagrams only where order/duration/multiplicity matter

Call chains are the default. Per Ch. 4, a PlantUML sequence diagram is only warranted when:

- The order of calls between multiple components is non-obvious from the call chain.
- Duration or SLA per call matters.
- A single component participates multiple times.

Write each such diagram at `system/sequence-diagrams/<use-case>.md` using **PlantUML sequence diagrams** (```puml fenced block wrapping `@startuml` / `participant <Long Name> as <Alias>` / `Caller -> Callee : message` / `Callee --> Caller : return` / `alt ... else ... end` / `loop ... end` / `@enduml`). The PlantUML hook validates every block on save. TradeMe used a sequence diagram for Terminate Tradesman (Ch. 5 Figure 5-28). Use the same restraint — over-spec is its own anti-pattern. **Do not use Mermaid `sequenceDiagram`** — PlantUML is the validated format.

### Step 12 — Apply the Structurizr Conventions validation checklist

Walk the rules table from `STRUCTURIZR-CONVENTIONS.md`. Every row must pass. Any failure either fixes here or sends you back to Step 1.

### Step 13 — Write the `workspace.dsl` copy

The Structurizr parser expects the workspace file to be named `workspace.dsl`. Copy:

```bash
cp methodpoc/designs/<product>/system/architecture.dsl \
   methodpoc/designs/<product>/system/workspace.dsl
```

Going forward, the PostToolUse hook keeps `workspace.dsl` in lockstep with `architecture.dsl` after every edit. See `STRUCTURIZR-CONVENTIONS.md` "Validation" for details.

### Step 14 — Validate the DSL against the parser

This is a hard gate. The Structurizr DSL is strict and the parser has traps that are not obvious from reading the file (notably `styles` block syntax, and dynamic-view edges that aren't declared in the model). Run:

```bash
./methodpoc/structurizr-validate <product>
```

The wrapper exits 0 only when the parser is fully clean (no errors AND no ERROR-level log lines that the bare `validate` command silently tolerates). Any failure means the DSL is broken — fix and re-run. Do NOT proceed to operational concepts with a non-validating workspace.

If the PostToolUse hook is configured (see `STRUCTURIZR-CONVENTIONS.md` "Validation"), the hook runs this automatically on every edit and blocks the agent with the parser output on failure. In that case Step 14 is implicit; the explicit run remains the final exit gate.

The user can render with:

```bash
./methodpoc/structurizr-serve <product>
# open http://localhost:8080
```

(`structurizr-serve` uses the current `structurizr/structurizr` Docker image. `structurizr/lite` is deprecated — do not reference it.)

## Exit criteria

- `architecture.dsl` exists and `./methodpoc/structurizr-validate <product>` exits 0 (no parser errors and no ERROR-level log lines).
- `workspace.dsl` is a byte-identical copy of `architecture.dsl`.
- Every component declared cites a volatility from `volatilities.md` in its description.
- Cardinality limits respected (see [[the-method-layers]]).
- Every core use case from `core-use-cases.md` has a dynamic view that traces cleanly through the layers.
- 2–3 non-core use cases also draw cleanly.
- Sequence diagrams exist for any use case where order/duration/multiplicity is non-obvious.

Move to `the-method-operational-concepts`.

## Anti-patterns to reject

- **Missing dynamic view for a core use case** — incomplete validation.
- **Dynamic view enters multiple Managers from a Client** — Don't 6a; decomposition wrong.
- **Dynamic view shows Client → Engine, Client → ResourceAccess, or Client → Resource directly** — layer skip; decomposition wrong.
- **Engines or ResourceAccess publishing events** — Don't rule violated; component misclassified.
- **Sequence diagrams in place of call chains for simple flows** — over-spec; call chain suffices.
- **No supplementary sequence diagram where multi-party order matters** — under-spec; reader cannot reconstruct the flow.
- **Mermaid `flowchart` or `sequenceDiagram`** — both are deprecated for Method artifacts; use PlantUML activity (new syntax) for use-case activity diagrams and PlantUML sequence for supplementary sequence diagrams. The PlantUML hook validates every block on save.
- **A component declared with no description (or a feature-name description)** — usually means it was named before the volatility was clear.
- **A description over 150 characters, or one that documents retention / persistence schema / idempotency mechanics / rationale** — that detail belongs in `operational-concepts.md`, `volatilities.md`, or a DSL comment block. The on-element description names the encapsulated volatility and the role; nothing more.

## TradeMe reference

Re-read [Ch. 5 §6](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev1sec6) for the worked example: the architect validated 8 use cases — 7 as call chains, 1 (Terminate Tradesman) as a sequence diagram because timing mattered. Use the same discipline.

## Common failure modes

- **A volatility maps to two components.** One of them is unnecessary, or the volatility was poorly stated. Resolve in `volatilities.md` first, then redo Step 1.
- **A component has no volatility from `volatilities.md` behind it.** Drop the component.
- **Manager-to-Engine ratio wrong (too few Engines).** Either Managers are too thick — extract Engines — or there are too few Engines because business activities are buried inside Managers. Refactor.
- **A Utility doesn't pass the cappuccino-machine test.** It is not a Utility. Reclassify (often as ResourceAccess or Engine) or remove.
- **A core use case won't draw.** The decomposition is wrong. Do not weaken the use case to fit; revise the decomposition.
