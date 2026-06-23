# System Design

> Walk the user through system design with The Method. The **architect drives** the entire process. The Product Manager supplies raw business input and ratifies decisions — they do not own volatility analysis, decomposition, or the architecture. This command produces a validated, layered, volatility-based architecture documented as Structurizr DSL plus supporting markdown.

**Skill reference:** Invoke `the-method` skill before starting. This command orchestrates the system-design sub-skills in canonical order:

1. [[the-method-business-alignment]]
2. [[the-method-requirements-analysis]]
3. [[the-method-volatility-identification]]
4. [[the-method-core-use-cases]]
5. [[the-method-architecture]] (merged decomposition + DSL + call-chain validation)
6. [[the-method-operational-concepts]]
7. [[the-method-system-design-standard-check]]

## Division of labor (do not get this wrong)

Per Löwy, the architect owns system design. The PM is a collaborator on customer-facing inputs and a ratifier on business-alignment outputs — nothing more.

| Activity | Architect | Product Manager |
|---|---|---|
| Vision / Objectives / Mission | **Drives drafting** (ch. 5) | Supplies raw business context. Ratifies. *Cannot own objectives* (ch. 5: "you must not allow the engineering or marketing people to own the conversation") |
| Glossary | **Owns** (Who/What/How/Where, ch. 3) | Supplies domain terms from customer language |
| Scrub solutions-masquerading-as-requirements | **Drives the dialogue** (ch. 2) | Supplies raw requirement text |
| Identify volatilities | **Owns entirely.** Two axes, design factoring, longevity, design-for-competitors (ch. 2) | Supplies customer/business context as input |
| Volatilities List | **Owns** | (input only) |
| Core use cases | **Decides** which are core (ch. 4) | Co-discovers with architect; resolves customer conflicts |
| Layered decomposition | **Owns** (Four Questions, cardinality, naming, layering rules — ch. 3) | (none) |
| Operational concepts | **Owns** (ch. 5) | Provides business justification when asked |
| Call chain validation | **Owns** (ch. 4) | (none) |

## Usage

```
/system-design <product>
```

## Prerequisites

`methodpoc/designs/<product>/research/` must exist with at least one research input (competitor analysis, customer interviews, business briefs, market analysis, prior system docs). If not, stop and tell the user to populate it.

## Workflow

### Step 1 — Business alignment (Architect drives; PM ratifies)

Invoke [[the-method-business-alignment]] via `system-architect`:

> Read all files in `methodpoc/designs/<product>/research/`. Drive the
> Vision → Objectives → Mission chain (Löwy ch. 5, "Business Alignment"):
>
> 1. **Vision** — distill to ONE sentence. "Terse and explicit, like a legal statement." Example (TradeMe): *"A platform for building applications to support the TradeMe marketplace."*
> 2. **Business Objectives** — numbered list, business perspective only. NO technology objectives. NO specific feature requirements. NO marketing language. (Ch. 5: "you must not allow the engineering or marketing people to own the conversation.")
> 3. **Mission Statement** — how you will achieve the vision/objectives, expressed in terms of **components** not features. Example (TradeMe): *"Design and build a collection of software components that the development team can assemble into applications and features."*
>
> Maintain **bidirectional traceability**: every objective must trace to vision; mission must support all objectives.
>
> Write to `methodpoc/designs/<product>/system/mission.md`.

Then ask the **Product Manager** (or the user directly) to **ratify** — does this capture the business intent? Iterate until ratified.

### Step 2 — Requirements analysis: glossary + scrubbed requirements (Architect owns)

Invoke [[the-method-requirements-analysis]] via `system-architect`. This single skill covers BOTH the glossary (Four Questions) and the scrubbing of solutions-masquerading-as-requirements.

> Build the glossary by answering the Four Questions across the domain
> (ch. 3 "What's in a Name", ch. 5 "TradeMe Glossary"):
>
> - **Who** uses or interacts with the system?
> - **What** does the system do (workflows, use cases)?
> - **How** does it perform business activities?
> - **Where** does it store state?
>
> Every distinct domain noun/verb gets a one-line definition. Use customer
> language. Output `methodpoc/designs/<product>/system/glossary.md`.
>
> Then, for each requirement statement, ask Löwy's interrogation (ch. 2,
> "Solutions Masquerading As Requirements"):
>
>   1. Is this a solution or a true requirement?
>   2. Are there other possible solutions?
>   3. If so, what is the real requirement and the underlying volatility?
>   4. Is the volatility itself a true requirement, or another solution?
>
> Examples from the book:
>   - "Send email" → strip to "notify users" (transport is volatility)
>   - "Cooking" → strip to "feeding" → "well-being"
>   - "We need a queue" → strip to "user must receive events in order"
>
> Output `methodpoc/designs/<product>/system/scrubbed-requirements.md`
> with before/after for each item. The architect proposes; the PM
> ratifies because they know the customer's actual need.

PM reviews glossary for missing or misnamed terms and ratifies the scrubbing.

### Step 3 — Volatility identification (Architect owns entirely)

Invoke [[the-method-volatility-identification]] via `system-architect`. This is the architect's signature skill. PM stays out except as a source of customer/business context.

> Discover areas of volatility per Löwy ch. 2. Procedure:
>
> 1. **Apply the two axes of volatility** (ch. 2, "Axes Of Volatility"):
>    - *Same customer over time* — even if the system fits this customer now, what will change in their business context over the system's lifespan?
>    - *All customers at the same point in time* — what differs between customers using the system today?
>    The axes MUST be independent. If areas of change span both axes, that's usually functional decomposition in disguise.
>
> 2. **Iterative design factoring** (ch. 2, Figure 2-10): Start with one component. Ask "Could this component, as-is, serve this customer forever?" — encapsulate the answer. Then "Could this component serve all current customers?" — encapsulate again. Repeat until every point on both axes is encapsulated.
>
> 3. **Distinguish volatile vs variable** (ch. 2, "Volatile Versus Variable"):
>    - Volatile = open-ended; unencapsulated cost would ripple across the system.
>    - Variable = handled with conditional logic in code; not architectural.
>    Reject variables from the list.
>
> 4. **Apply longevity heuristic**: the more often something changes today, the more often it'll change at the same rate going forward.
>
> 5. **Design for competitors**: could a direct competitor use your system? Barriers reveal volatilities; identical practices reveal "nature of the business" — do NOT encapsulate those.
>
> 6. **Resist speculative design**: don't encapsulate changes to the nature of the business. Two indicators: change is rare AND any encapsulation would be poor.
>
> 7. **Resist the siren song**: a "reporting block" or any familiar pattern only goes in if a specific business volatility justifies it. Habit is not a justification.
>
> Output `methodpoc/designs/<product>/system/volatilities.md`. Format
> per ch. 2 "Example: Volatility-Based Trading System": each entry =
> bolded volatility name + rationale paragraph. Group entries by axis.

Show to user. The PM may flag a missing customer-driven volatility; the architect decides whether to include it.

### Step 4 — Core use cases (Architect decides; PM co-discovers)

Invoke [[the-method-core-use-cases]] via `system-architect`:

> Identify the core use cases per Löwy ch. 4. Procedure:
>
> 1. List **all** raw use cases from research (don't filter yet).
> 2. For each, ask: is this the essence of the business, or a permutation of something deeper?
> 3. Look for abstractions. Core use cases often need a new name not present in the customer's vocabulary. Example (TradeMe ch. 5): customer provided 8 use cases — Add Tradesman, Pay Tradesman, etc. — but only ONE was core: Match Tradesman. The rest were "just a list of simple functionalities... add little business value and do not differentiate the system."
> 4. **Target 2–6 core use cases.** Rarely more.
> 5. Capture each as an **activity diagram** when there are nested conditions (ch. 3 Design Standard 1c) — represent flow + alternative paths.
>
> Output `methodpoc/designs/<product>/system/core-use-cases.md` with:
>   - The full list of raw use cases received
>   - The 2–6 core use cases (each with one-paragraph behavior description)
>   - An explicit "non-core" list with reasoning for each rejection

The PM ratifies. If they object, work it out: the architect has abstraction taste; the PM has customer reality. Both must agree before proceeding. **Stop here if you have >6 core use cases** — abstraction is incomplete.

### Step 5 — Architecture: layered decomposition + Structurizr DSL + call-chain validation (Architect owns)

Invoke [[the-method-architecture]] via `system-architect`. This single skill covers the layered decomposition (Four Questions, cardinality, naming, anti-pattern rejection, smallest-set check), the Structurizr DSL representation, and the call-chain validation. Decomposition and call-chain validation iterate together — they are one activity, not two.

> Apply the Four Questions to the glossary + volatilities (ch. 3, "The
> Four Questions"):
>
>   - **Who** → candidates for **Clients**
>   - **What is required of the system** → candidates for **Managers**
>   - **How** activities happen → candidates for **Engines**
>   - **How** resources are accessed → candidates for **ResourceAccess**
>   - **Where** state lives → candidates for **Resources**
>
> Then classify each candidate. Use these guidelines:
>
> - **Managers** encapsulate sequence/workflow volatility for a *family* of related use cases. Almost expendable.
> - **Engines** encapsulate business activity volatility (Strategy pattern). Names are gerunds: `MatchingEngine`, `PricingEngine`, `CalculatingEngine`. No I/O.
> - **ResourceAccess** components expose *atomic business verbs* over a Resource — `credit`, `debit`, `match`, `assign` — never CRUD, never raw I/O ops.
> - **Resources** = physical stores, queues, external systems.
> - **Utilities** must pass the litmus test: "Could this plausibly be reused in any other system, e.g., a smart cappuccino machine?" (ch. 3)
>
> **Naming**: Pascal-case compounds with type suffix — `OrderManager`, `MatchingEngine`, `TradesAccess`, `OrderDB`. Gerunds ONLY on Engines.
>
> **Cardinality** (ch. 3 + App C):
>   - ≤5 Managers without subsystems
>   - Manager-to-Engine ratio: 1M → 0–1E; 2M → 1E; 3M → 2E; 5M → 3E
>   - If you have ≥8 Managers, decomposition has failed
>
> **Layering style**: prefer **closed** (App C directive). No calling up. No sideways within a layer except queued Manager↔Manager and Manager→Engine. No skipping layers.
>
> Anti-patterns to reject (re-iterate decomposition until clean):
>   - Functional decomposition (`OrderProcessing`, `Reporting`)
>   - Domain decomposition (`UserService`, `TradesmanService`)
>   - God service / services explosion / chained services
>   - Speculative or reflex components
>
> **Smallest set check (ch. 4):** order of magnitude ~10 components total; method-typical 2–5 Managers, 2–3 Engines, 3–8 ResourceAccess+Resources, ~6 Utilities. Reduce if you can; never collapse to one (god) or fan out to one-per-use-case.
>
> Once the decomposition is clean, write the Structurizr DSL following
> `methodpoc/.claude/skills/the-method-architecture/STRUCTURIZR-CONVENTIONS.md`.
> Output `methodpoc/designs/<product>/system/architecture.dsl`:
>   - One `softwareSystem` with all components as containers
>   - Each container tagged with its layer (`client`, `manager`, `engine`, `resource-access`, `resource`, `utility`)
>   - One `container` view named `static-architecture` showing the layered pyramid
>   - One `dynamic` view per core use case (call chain — see below)
>   - The styles block from STRUCTURIZR-CONVENTIONS
>
> Also write a `workspace.dsl` (copy of `architecture.dsl`) so Structurizr Lite can render it.
>
> Then validate every core use case as a call chain (ch. 4 "Architecture Validation"):
>   1. Take the activity diagram from `core-use-cases.md`. Add swim lanes for components/subsystems.
>   2. Trace through the static architecture: Client → exactly one Manager → Engines/ResourceAccess → Resources.
>   3. Encode as a `dynamic` view in `architecture.dsl`. Solid arrows = sync, dashed = queued.
>   4. Where call order/duration/multiplicity matters, ALSO produce a sequence diagram in `system/sequence-diagrams/<use-case>.md`.
>
> **Definition of valid (ch. 4):** every core use case must trace cleanly through the existing decomposition. If it can't, the **decomposition is wrong**, not the use case. Iterate the decomposition.
>
> Also produce 2–3 non-core call chains to demonstrate versatility (ch. 5).

Validation rules from `STRUCTURIZR-CONVENTIONS.md`:

| Rule | Action if failed |
|---|---|
| Every core use case has a dynamic view | Iterate call-chain validation |
| Each dynamic view: Client → exactly one Manager | Decomposition wrong → iterate |
| No calling up | Decomposition wrong → iterate |
| No sideways except queued Manager↔Manager / Manager→Engine | Decomposition wrong → iterate |
| No skipping layers | Decomposition wrong → iterate |
| Engines/ResourceAccess/Resources don't publish or subscribe to events | Decomposition wrong → iterate |
| Cardinality limits respected | Decomposition wrong → iterate |

### Step 6 — Operational concepts (Architect owns)

Invoke [[the-method-operational-concepts]] via `system-architect`:

> Document operational decisions in
> `methodpoc/designs/<product>/system/operational-concepts.md`. Each
> decision MUST be justified against a business objective from `mission.md`
> (ch. 5, "Operational Concepts"):
>
> - **Communication topology**: Message Bus or direct calls? If Message Bus, which Utilities mediate?
> - **Sync vs queued boundaries**: which Manager↔Manager calls are queued? Which calls within subsystems are sync?
> - **Pub/sub edges**: who publishes events? Only Clients/Managers may publish; only Clients/Managers may subscribe. Map each edge.
> - **Layering style**: closed (default) — confirm and state why open or semi-closed was NOT chosen.
> - **Patterns adopted**: e.g., Workflow Manager, Message-Is-the-Application. For each, cite the business objective it serves and the team's capability to implement it (ch. 5 cautions against adopting patterns the team can't sustain).
> - **State handling**: where do Managers persist workflow state? Stateless workflow + workflow store, or stateful sessions?

### Step 7 — System Design Standard check (final gate)

Invoke [[the-method-system-design-standard-check]] via `system-architect`:

> Run the System Design Guidelines checklist from
> `../rightingsoftware/OEBPS/xhtml/appc.xhtml`:
>
> - Requirements: behavior captured as use cases, activity diagrams where nested, solutions-masquerading scrubbed, core use cases drive validation
> - Cardinality: limits respected
> - Attributes: volatility/reuse direction, no encapsulation of nature-of-business, Managers expendable, symmetric, no public channels for internal calls
> - Layers: closed; no calling up/sideways/skip; subsystems used to extend
> - Interaction rules and don'ts: full list verified
>
> Output `methodpoc/designs/<product>/system/standard-checklist.md`.

Report violations to user. Each must be either fixed or explicitly waived with justification.

### Step 8 — Wrap up

Show final layout:

```
methodpoc/designs/<product>/system/
├── mission.md                  (vision + objectives + mission statement)
├── glossary.md                 (Who/What/How/Where)
├── scrubbed-requirements.md    (before/after on solutions-masquerading)
├── volatilities.md             (the Volatilities List, grouped by axis)
├── core-use-cases.md           (raw list, core list, rejections with reasons)
├── architecture.dsl            (Structurizr source of truth)
├── workspace.dsl               (copy for Structurizr Lite)
├── operational-concepts.md     (topology, sync/queued, pub/sub, patterns)
├── standard-checklist.md       (Appendix C system design checklist)
└── sequence-diagrams/          (when order/duration/multiplicity matters)
    └── <use-case>.md
```

Render command for the user:

```bash
docker run -it --rm -p 8080:8080 \
  -v "$PWD/methodpoc/designs/<product>/system:/usr/local/structurizr" \
  structurizr/lite
# open http://localhost:8080
```

Tell user: *"System design complete. The architecture is stable until the nature of the business changes. Next: `/project-design <product>` to plan construction."*

## Error handling

- **Research directory empty** → stop, ask user to populate.
- **PM tries to author objectives or volatilities** → reject; architect drives those.
- **>6 core use cases** → abstraction incomplete; architect must iterate with PM.
- **Component count outside ~10–24** → iterate the architecture skill.
- **Any core use case can't be traced as a call chain** → decomposition is wrong, NOT the use case. Iterate the architecture skill.
- **Functional or domain decomposition detected** → reject; restart the architecture skill with explicit guard against the anti-pattern.
- **Speculative encapsulation detected** (component for hypothetical future need) → remove it.
- **Pattern adopted without business-objective justification** → strip or justify.

## Key book references

All step citations are to:
- ch. 1: What Is The Method?
- ch. 2: Decomposition (volatility-based, axes of volatility, volatilities list, solutions masquerading)
- ch. 3: Structure (layers, Four Questions, cardinality, naming, layering rules)
- ch. 4: Composition (core use cases, smallest set, architect's mission, call chains)
- ch. 5: TradeMe worked example (business alignment, anti-design effort, the architecture, design validation)
- App C: Design Standard (the checklist)

Source files at `/Users/davidmarne/mixofrealitystudio/software/rightingsoftware/OEBPS/xhtml/`.
