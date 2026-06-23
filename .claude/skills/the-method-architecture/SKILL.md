---
name: the-method-architecture
description: Decompose the system into layered components and express the decomposition as a typed System model (components, relationships, one dynamic view per core use case). Call-chain validation iterates back into the decomposition — they are one activity, not two. Produces the typed System committed to project.json → .systemDesign (Structurizr DSL is a render-on-read). Use after core use cases, before operational concepts.
---

# The Method — Architecture

This skill produces the system's static architecture and validates it by tracing every core use case as a call chain. **Decomposition and the typed System model are a single loop**, not two phases: when a call chain fails to draw, the decomposition is wrong, and both the components and the model must be revised together. There is no intermediate component table — the typed `System` (`Components` + `Relationships` + `DynamicViews`) committed to `.aiarch/state/project.json` → `.systemDesign` is the only artifact. The Structurizr DSL is a render-on-read of that model, never the source of truth.

## Cross-cutting references

- [[the-method-layers]] — the canonical layer model, naming conventions, interaction rules, interaction don'ts, and cardinality limits. **This skill does not restate them.** When you need a rule, link there.
- [[the-method-doctrine]] — the Prime Directive and 9 directives. This skill operationalises Directives 1 (Avoid functional decomposition), 2 (Decompose based on volatility), 3 (Provide a composable design), and 4 (Features as integration, not implementation).
- `STRUCTURIZR-CONVENTIONS.md` (sibling file in this skill) — the Structurizr DSL conventions, tag conventions, edge-label conventions, and styling that govern the **render-on-read** of the typed `System` model. Read it before authoring the model. **Architecture diagrams are infrastructure- and platform-agnostic**: edge labels (the `Relationship.Label` field) must use the vocabulary of the destination layer (manager method, engine method signature, atomic business verb), never workflow-engine primitives (`Activity:`, `StartWorkflow(...)`, `SignalExternalWorkflow`) and never platform-specific commands (`git commit`, `ArgoCD reconcile`, `POST /charges`). Infrastructure and platform detail belong in the operational-concepts artifact (`.operationalConcepts`).

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

State is git-as-DB: archistrator is a single Go-server repo whose canonical project state lives in `.aiarch/state/project.json` (a typed JSON aggregate). Markdown/DSL is a render-on-read of the typed state.

- The committed **volatilities** artifact → `.aiarch/state/project.json` → `.volatilities`
- The committed **coreUseCases** artifact → `.coreUseCases`
- The committed **glossary** artifact → `.glossary`
- The committed **mission** artifact → `.mission`

## Output

The typed **`System`** model (Go shape in `server/internal/resourceaccess/projectstate/system.go`: `Components []Component`, `Relationships []Relationship`, `DynamicViews []DynamicView`), committed to **`.aiarch/state/project.json` → `.systemDesign`**. It carries:

1. The static architecture (`Components` + `Relationships`).
2. One `DynamicView` (call chain) per core use case, plus 2–3 non-core dynamic views demonstrating versatility.
3. Supplementary PlantUML sequence-diagram source carried on a dynamic view *only* where order, duration, or multiplicity is non-obvious — not a separate `<use-case>.md` file.

The Structurizr DSL (`architecture.dsl` / `workspace.dsl`) and any sequence-diagram markdown are **render-on-read** of this model, produced by the server's rendering access — never the source of truth, never files you hand-author. Per the two usage patterns (agentic/CI dispatch and local interactive), the agent emits the typed `System` JSON and commits it into `.systemDesign`; the server stages it (`StageArtifactForReview`) for the human review gate.

There is no intermediate "component table" and no hand-written `.dsl` file. The typed `Components` list *is* the table.

## The loop

This skill is one iterative loop:

```
classify volatilities → name components → build the System model → trace each core use case
                                ↑                                              │
                                └─────────── revise both ◄─────────────────────┘
                                       (if any trace fails)
```

A trace that cannot be drawn cleanly is a signal that the decomposition is wrong. Fix decomposition + model together. **Never** twist a use case to fit a bad decomposition.

## Procedure

### Step 1 — Classify volatilities into layer bins (the Four Questions)

Per [Ch. 3 §4.2 "The Four Questions"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev2sec9):

> *"Make a list of all the 'who' and put them in one bin as candidates for Clients. Make a list of all the 'what' and put them in another bin as candidates for Managers, and so on... The result will not be perfect... but it is a start."*

Walk every entry in the committed `.volatilities` artifact. Bin it:

| Question | Bin → Layer |
|---|---|
| Who interacts with the system? | Clients |
| What is required of the system? | Managers |
| How (business activity)? | Engines |
| How (resource access)? | ResourceAccess |
| Where (state)? | Resources |
| Cross-cutting concern with cappuccino-machine reusability? | Utilities |

If a volatility lands in two bins, it is either two volatilities or the volatility statement is ambiguous — refine the `.volatilities` artifact before proceeding. See [[the-method-layers]] for full layer identity rules.

### Step 2 — Name and classify each candidate component

For each bin entry, name the component using the conventions in [[the-method-layers]] (e.g., `<Noun>Manager`, `<Gerund>Engine`, `<Noun>Access`). Each becomes a typed `Component` (`Name`, `Kind`, `Encapsulates`, `AtomicBusinessVerbs`; `Layer` is derived server-side from `Kind`). Verify each component:

- Encapsulates **exactly one** volatility from `.volatilities` (recorded in `Component.Encapsulates`).
- Sits in **exactly one** layer.
- Passes the layer's identity test (e.g., Engines do no I/O; ResourceAccess exposes business verbs not CRUD; Utilities pass the cappuccino-machine test).

If a candidate fails its identity test, either re-classify it or split it. If you cannot decide, the volatility itself is probably wrong — return to `the-method-volatility-identification`.

### Step 3 — Check cardinality and smallest-set

Apply the cardinality limits from [[the-method-layers]] (≤5 Managers without subsystems, golden Engines-to-Managers ratio, ~10 components order-of-magnitude, ≥8 Managers is a hard fail).

Then apply the smallest-set test from [Ch. 4 §2](../../../../rightingsoftware/OEBPS/xhtml/ch04.xhtml#ch04lev1sec2):

> *"Once you cannot think of a smaller set of building blocks, you have found your best design."*

If you can reduce further without losing a volatility, reduce.

### Step 4 — Reject anti-patterns

Before building the model, scan the candidate set for these smells. Any hit means restart from Step 1 with the smell as a guard:

| Anti-pattern | Indicator |
|---|---|
| Functional decomposition | Components named after features (`OrderProcessing`, `Reporting`, `Notifications`) |
| Domain decomposition | Components named after domains/entities (`UserService`, `ProductService`) |
| God service | One Manager doing everything |
| Services explosion | One component per use case |
| Chained services | A→B→C where B presumes A or C |
| Speculative encapsulation | Component for a future need with no current volatility |
| Reflex components | `Logging`/`Reporting` declared by reflex with no specific volatility behind it |

### Step 5 — Read the render conventions

Open `STRUCTURIZR-CONVENTIONS.md` (sibling to this SKILL.md). It defines how the server renders the typed `System` to Structurizr DSL (the `workspace`/`model`/`views`/`styles` blocks, per-layer tags `client`/`manager`/`engine`/`resource-access`/`resource`/`utility`, description style, edge-label conventions). You do not author the DSL — you author the typed `System` model so that the render conventions are satisfiable. Read them before building the `Components` and `Relationships` so your component names, descriptions, and edge labels conform.

### Step 6 — Author a `Component` entry for every component

For each component identified in Step 2, add a typed `Component` to `System.Components`:

- `Name` — the component name (e.g., `OrderManager`); the server assigns `ID = Slug(Name)`.
- `Kind` — one of the closed taxonomy (`Client`/`Manager`/`Engine`/`ResourceAccess`/`Resource`/`Utility`); the server derives `Layer` from `Kind` — you never emit a layer.
- `Encapsulates` — the single volatility this component owns (Manager/Engine/RA); `""` for Resource/Utility. **≤ 150 characters** when it renders as the on-element description: name the volatility plus a brief verb-phrase. Implementation detail (retention, idempotency mechanics, schema, rationale) belongs in the `.operationalConcepts` artifact or the `.volatilities` artifact — NOT here. See "Description style" in `STRUCTURIZR-CONVENTIONS.md`.
- `AtomicBusinessVerbs` — for ResourceAccess, the atomic business verbs it exposes.

Also capture every actor named in core use cases (rendered as `person` declarations).

### Step 7 — Add only the relationships needed for core use cases

Do NOT exhaustively wire every plausible edge. Add only the `Relationship` entries (`From`, `To`, `Mode`, `Label`) to `System.Relationships` actually exercised by core use cases (Step 8 will exercise them).

Every relationship must comply with the closed-architecture rules in [[the-method-layers]]: no calling up, no sideways within a layer except queued Manager→Manager, no skipping layers, plus the don'ts (Engines don't receive queued calls, Engines/ResourceAccess/Resources don't publish events, etc.).

Mark sync vs queued in `Relationship.Mode` (`CallSync` / `CallQueued`) — the renderer styles each distinctly.

**Edge-label vocabulary.** `Relationship.Label` uses the vocabulary of the **destination layer's responsibility**. The architecture stays infrastructure- and platform-agnostic; the infrastructure-specific primitives go in the `.operationalConcepts` artifact. See `STRUCTURIZR-CONVENTIONS.md` "Edge-label conventions" for the full table and the rule-of-thumb test. Quick summary:

| Edge | Label shape |
|---|---|
| Client → Manager | `<managerMethodName>(<args>) → <result>` |
| Manager → Engine | `<EngineMethodName>(<args>) → <output>` |
| Manager → ResourceAccess | `<atomicBusinessVerb>(<noun>)` (e.g., `appendEvent(OrderSubmitted)`) |
| Manager → infrastructure-access ResourceAccess | atomic verbs in the infrastructure's *generic* domain (e.g., `awaitSignal(reviewDecision)`, `scheduleNextActivity`) — never workflow-engine product primitives |
| Manager → Manager (queued) | `delivers <SignalName> (queued)` |
| ResourceAccess → Resource | resource-domain verb + idempotency note — no platform-specific commands (no `git commit`, no `INSERT … ON CONFLICT`, no `POST /charges`) |

### Step 8 — The static-architecture view (render-on-read)

The static-architecture view (Clients at top, Resources at bottom — the layered pyramid) is derived automatically from `System.Components` + `System.Relationships` by the renderer; you do not author a separate view declaration. Just ensure the components and relationships are complete and well-tagged so the rendered top-to-bottom layout is correct. See `STRUCTURIZR-CONVENTIONS.md` for the rendered shape.

### Step 9 — Author one `DynamicView` per core use case (the validation)

For each core use case in the committed `.coreUseCases`, add a typed `DynamicView` to `System.DynamicViews` (`UseCaseID`, `Key`, `Title`, `Participants`, ordered `Edges`):

- `UseCaseID` — links to the `UseCase` it validates.
- `Key` — stable view key, e.g. `uc1-coauthor-method-artifact`.
- `Participants` — the component IDs the chain touches.
- `Edges` — the ordered call chain, each a `Relationship` with `Mode ∈ {CallSync, CallQueued}`:

```
<actor>   -> <client>     "<actor action>"
<client>  -> <manager>    "<API call>"
<manager> -> <engine>     "<method call>"
<manager> -> <access>     "<atomic business verb>"
<access>  -> <resource>   "<I/O>"
```

This is the **call chain** referenced throughout Chapter 4 and 5 of the book. The `DynamicView` IS the validation artifact — tracing it cleanly proves the decomposition supports the use case. The renderer emits each as a Structurizr `dynamic` view.

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

Per [Ch. 5 §6](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev1sec6): validate that the architecture also handles non-core use cases without modification. Pick 2–3 entries from the rejection list in the committed `.coreUseCases` and add their `DynamicView` entries to the same `System` model.

If a non-core use case cannot be drawn either, the decomposition is missing a volatility — return to `the-method-volatility-identification` before continuing.

### Step 11 — Add sequence diagrams only where order/duration/multiplicity matter

Call chains are the default. Per Ch. 4, a PlantUML sequence diagram is only warranted when:

- The order of calls between multiple components is non-obvious from the call chain.
- Duration or SLA per call matters.
- A single component participates multiple times.

Carry each such diagram as PlantUML sequence-diagram **source on the corresponding `DynamicView`** (not a separate `<use-case>.md` file) using **PlantUML sequence diagrams** (a `@startuml` / `participant <Long Name> as <Alias>` / `Caller -> Callee : message` / `Callee --> Caller : return` / `alt ... else ... end` / `loop ... end` / `@enduml` block). The renderer emits and validates it. TradeMe used a sequence diagram for Terminate Tradesman (Ch. 5 Figure 5-28). Use the same restraint — over-spec is its own anti-pattern. **Do not use Mermaid `sequenceDiagram`** — PlantUML is the validated format.

### Step 12 — Apply the Structurizr Conventions validation checklist

Walk the rules table from `STRUCTURIZR-CONVENTIONS.md` against your typed `System` model. Every row must pass. Any failure either fixes here or sends you back to Step 1.

### Step 13 — Commit the typed `System` to `.systemDesign`

There is no `workspace.dsl` to copy and no `.dsl` file to author. Commit the typed `System` model into `.aiarch/state/project.json` → `.systemDesign`. The server's rendering access derives `architecture.dsl` / `workspace.dsl` (and any sequence diagrams) render-on-read from this slot; the Structurizr DSL is never a hand-maintained file.

### Step 14 — Validate the model (render + parser gate, server-side)

This is a hard gate. When the typed `System` is staged/committed, the server renders it to Structurizr DSL and runs the strict parser as part of artifact validation (the parser has traps — `styles` block syntax, dynamic-view edges not declared in the model — that the typed model is shaped to avoid). A render or parse failure means the model is malformed — fix the `Components` / `Relationships` / `DynamicViews` and re-stage. Do NOT advance to operational concepts with a model that does not render+parse cleanly.

If a build is involved when running the server locally, the Go build is `GOWORK=off go build ./...` / `go vet ./...` / `go test ./...` under `server/`.

## Exit criteria

- `.aiarch/state/project.json` → `.systemDesign` holds the typed `System` model, and it renders to Structurizr DSL that parses cleanly (no parser errors, no ERROR-level log lines) during server-side artifact validation.
- Every `Component` cites a volatility from `.volatilities` in its `Encapsulates`.
- Cardinality limits respected (see [[the-method-layers]]).
- Every core use case from `.coreUseCases` has a `DynamicView` that traces cleanly through the layers.
- 2–3 non-core use cases also draw cleanly.
- Sequence-diagram source is carried on any `DynamicView` where order/duration/multiplicity is non-obvious.

Move to `the-method-operational-concepts`.

## Anti-patterns to reject

- **Missing dynamic view for a core use case** — incomplete validation.
- **Dynamic view enters multiple Managers from a Client** — Don't 6a; decomposition wrong.
- **Dynamic view shows Client → Engine, Client → ResourceAccess, or Client → Resource directly** — layer skip; decomposition wrong.
- **Engines or ResourceAccess publishing events** — Don't rule violated; component misclassified.
- **Sequence diagrams in place of call chains for simple flows** — over-spec; call chain suffices.
- **No supplementary sequence diagram where multi-party order matters** — under-spec; reader cannot reconstruct the flow.
- **Mermaid `flowchart` or `sequenceDiagram`** — both are deprecated for Method artifacts; use PlantUML activity (new syntax) for use-case activity diagrams and PlantUML sequence for supplementary sequence diagrams. The PlantUML hook validates every block on save.
- **A component with an empty `Encapsulates` where one is required (or a feature-name in it)** — usually means it was named before the volatility was clear.
- **An `Encapsulates` over 150 characters, or one that documents retention / persistence schema / idempotency mechanics / rationale** — that detail belongs in the `.operationalConcepts` or `.volatilities` artifact. The rendered on-element description names the encapsulated volatility and the role; nothing more.

## TradeMe reference

Re-read [Ch. 5 §6](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev1sec6) for the worked example: the architect validated 8 use cases — 7 as call chains, 1 (Terminate Tradesman) as a sequence diagram because timing mattered. Use the same discipline.

## Common failure modes

- **A volatility maps to two components.** One of them is unnecessary, or the volatility was poorly stated. Resolve in the `.volatilities` artifact first, then redo Step 1.
- **A component has no volatility from `.volatilities` behind it.** Drop the component.
- **Manager-to-Engine ratio wrong (too few Engines).** Either Managers are too thick — extract Engines — or there are too few Engines because business activities are buried inside Managers. Refactor.
- **A Utility doesn't pass the cappuccino-machine test.** It is not a Utility. Reclassify (often as ResourceAccess or Engine) or remove.
- **A core use case won't draw.** The decomposition is wrong. Do not weaken the use case to fit; revise the decomposition.
