---
name: the-method-layers
description: Reference for the layer model in Juval Löwy's The Method — Clients, Managers, Engines, ResourceAccess, Resources, Utilities — plus the interaction rules, interaction don'ts, and cardinality limits. Use when another skill needs the authoritative classification, naming, or call-graph rule for a component.
---

# The Method — Layers

This is a pure reference skill. It holds the canonical layer model, interaction rules, interaction don'ts, and cardinality limits. Other skills (decomposition, architecture, design-standard-check, validate-architecture) cite this skill instead of restating the rules inline.

Sources:
- [Ch. 3 §3 "Typical Layers"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev1sec3) — what each layer encapsulates.
- [Ch. 3 §4 "Classification Guidelines"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev1sec4) — naming, Four Questions, Managers-to-Engines ratio, key observations.
- [Ch. 3 §6 "Open and Closed Architectures"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev1sec6) — layering rules and design "don'ts".
- [Ch. 4 §2 "Composable Design"](../../../../rightingsoftware/OEBPS/xhtml/ch04.xhtml#ch04lev1sec2) — the smallest-set principle.
- [Appendix C §3 "System Design Guidelines"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec3) — items 2 (Cardinality), 4 (Layers), 5 (Interaction rules), 6 (Interaction don'ts).

## The six layers

*The Method* uses five horizontal layers (Clients, Managers, Engines, ResourceAccess, Resources — top to bottom) plus a Utilities bar that runs alongside them. Every component lives in exactly one layer.

| Layer | What goes here | Naming | Examples |
|---|---|---|---|
| **Clients** | Entry points: end-user apps, public APIs, other systems. Encapsulates client volatility (web/mobile/desktop/agent etc.). | Form-descriptive: `WebApp`, `MobileApp`, `PublicAPI`. | React app, iOS app, REST gateway |
| **Managers** | Workflow / sequence volatility for a *family* of related use cases. Almost expendable — orchestrate Engines + ResourceAccess. | `<Noun>Manager` — `OrderManager`, `AccountManager`. Noun describes the encapsulated workflow volatility. | OrderManager, OnboardingManager |
| **Engines** | Business **activity** volatility — Strategy pattern. **No I/O.** Pure computation, decision, transformation. | `<Gerund>Engine` — `PricingEngine`, `MatchingEngine`, `CalculatingEngine`. Gerunds MANDATORY here and FORBIDDEN elsewhere in the business / access layers. | PricingEngine, MatchingEngine |
| **ResourceAccess** | Atomic business verbs over a Resource — `credit`, `debit`, `match`, `assign`. **Never CRUD. Never raw I/O.** May serve more than one Resource. | `<Noun>Access` — `OrderAccess`, `AccountAccess`. Noun describes the data or business concept exposed. | OrderAccess, IdentityAccess |
| **Resources** | Physical stores, queues, external systems. Internal or external to the system. | `<Noun><Technology>` — `OrderDB`, `EmailProvider`, `EventBus`. | PostgreSQL, S3, Stripe, Kafka topic |
| **Utilities** (bar) | Cross-cutting infrastructure. Cappuccino-machine test: *"could this plausibly be used in any other system?"* If no, it is not a Utility. | Concern-descriptive: `Logging`, `Security`, `Diagnostics`, `Pub/Sub`. | Logging, Security, Pub/Sub |

### The Four Questions (Ch. 3 §4.2)

Loose mapping that initiates and validates classification:

| Question | Layer |
|---|---|
| Who interacts with the system? | Clients |
| What is required of the system? | Managers |
| How (business activity)? | Engines |
| How (resource access)? | ResourceAccess |
| Where (state)? | Resources |
| Cross-cutting concern usable in any system? | Utilities |

The mapping is loose because **volatility trumps everything**: if there is little or no volatility in the "how", the Manager can perform both "what" and "how".

## Interaction rules

From [Appendix C §3.5 "Interaction rules"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec3). These are permissive: they describe what *can* happen.

- **Clients** call exactly **one Manager per use case** (§3.5; restated as §3.6 don't 6a — "Clients do not call multiple Managers in the same use case").
- All components can call **Utilities**.
- **Managers** and **Engines** can call **ResourceAccess**.
- **ResourceAccess** components call **Resources**.
- **Managers** can call **Engines**.
- **Managers** can queue calls to another **Manager**.

From the closed-architecture rules ([App C §3.4](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec3)): no calling up, no calling sideways within a layer (except queued Manager → Manager), no skipping layers.

## Interaction don'ts

From [Appendix C §3.6 "Interaction don'ts"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec3) and [Ch. 3 §6.5 "Design 'Don'ts'"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev2sec18). These are prohibitive: any one of them is a red flag for functional decomposition.

- Clients do not call multiple Managers in the same use case.
- Clients do not call Engines.
- Managers do not queue calls to more than one Manager in the same use case (use Pub/Sub instead).
- Engines do not receive queued calls.
- ResourceAccess components do not receive queued calls.
- Clients do not publish events.
- Engines do not publish events.
- ResourceAccess components do not publish events.
- Resources do not publish events.
- Engines, ResourceAccess, and Resources do not subscribe to events.
- Engines never call each other.
- ResourceAccess components never call each other (use a single ResourceAccess that joins multiple Resources instead).

When you find yourself wanting to violate one of these, the underlying problem is almost always: you have a functional decomposition hiding behind Method-style names. Restart the decomposition.

## Cardinality

From [Appendix C §3.2 "Cardinality"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec3) and [Ch. 3 §4.3 "Managers-to-Engines Ratio"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev2sec10).

- ≤ **5 Managers** in a system without subsystems.
- ≤ a handful of **subsystems** (rule of thumb: ≤5).
- ≤ **3 Managers per subsystem**.
- Strive for the **golden Engines-to-Managers ratio** — typically **fewer** Engines than Managers (the book: you end up with fewer Engines than you imagine):

| Managers | Typical Engines |
|---|---|
| 1 | 0–1 |
| 2 | 1 |
| 3 | 2 |
| 4 | 2–3 |
| 5 | 3 |

(Table is illustrative — the rule of thumb is fewer Engines than Managers, not the exact counts.)

- ResourceAccess components may serve more than one Resource.
- **Order of magnitude: ~10 components total** across all layers ([Ch. 4 §2 "The Architect's Mission"](../../../../rightingsoftware/OEBPS/xhtml/ch04.xhtml#ch04lev2sec4) — "a dozen or two at the most"). If you have hundreds, the decomposition is wrong.

**Hard fail:** ≥8 Managers (violates [Directive 1 "Avoid functional decomposition"](../the-method-doctrine/SKILL.md)). Per Ch. 3 §4.3: *"you have already failed to produce a good design"* — that many Managers indicates functional or domain decomposition. Restart.

## Temporal mapping (when Managers run on Temporal)

When the operational concepts commit to Temporal as the Manager-execution infrastructure (see [[the-method-operational-concepts]] Step 1), each interaction rule above maps to a specific Temporal primitive. Edge labels in `architecture.dsl` and steps in sequence diagrams use these primitive names verbatim per `../the-method-architecture/TEMPORAL-VOCABULARY.md`.

**Temporal lives only in the Manager layer.** Engines, ResourceAccess, Resources, and Utilities import no Temporal and contain no Temporal types. The mapping below is written from the Manager's perspective: when a Manager workflow needs to make a ResourceAccess call (which does I/O), the **Manager defines and registers a Temporal Activity** whose body delegates to the plain ResourceAccess method — the Activity, its `RetryPolicy`, and its timeouts all belong to the Manager. Engine calls are deterministic, so the Manager invokes them directly from workflow code with no Activity. A ResourceAccess or Engine package that imports Temporal is a layer violation.

| Method interaction | Temporal primitive | Edge-label form |
|---|---|---|
| Client → Manager (start a use case) | Start a Workflow | `StartWorkflow(<WorkflowType>, <workflowId>, <input>)` |
| Client → Manager (deliver a decision to a running workflow) | Signal a Workflow | `SignalWorkflow(<workflowId>, <SignalName>, <payload>)` |
| Client → Manager (read state without mutation) | Query a Workflow | `QueryWorkflow(<workflowId>, <QueryName>) → <result>` |
| Client → Manager (synchronous tracked write) | Update a Workflow | `UpdateWorkflow(<workflowId>, <UpdateName>, <payload>) → <result>` |
| SchedulerClient → Manager (recurring) | Temporal Schedule | `Schedule[<name>] → executes <WorkflowType>` |
| Manager → Engine | Deterministic in-workflow call (NOT an Activity); Engine imports no Temporal | `<Name>(<args>) → <output>` |
| Manager → ResourceAccess | Activity **defined and registered by the Manager**, wrapping the plain RA method; RA imports no Temporal | `Activity: <ActivityName>` |
| Manager → Manager (queued, cross-Manager) | Signal an external Workflow | `SignalExternalWorkflow(<targetWorkflowId>, <SignalName>, <payload>)` — routed through `workflowExecutionAccess`; the static architecture has no sideways Manager → Manager edge |
| Manager → self (sleep) | Durable Timer | `Timer(<duration>)` |
| Manager → self (await human / external event) | Await Signal | `Await Signal(<SignalName>) — workflow suspends` |
| Manager → child workflow | ExecuteChildWorkflow | `ExecuteChildWorkflow(<ChildWorkflowType>, <input>)` |
| Manager → reset history at a checkpoint | Continue-As-New | `ContinueAsNew(<input>)` |
| ResourceAccess → Resource | The plain RA method's actual I/O (running inside the Manager's Activity wrapper, but itself Temporal-free) | `<atomic verb>` plus idempotency key derivation (the Manager passes the key in, e.g. `${workflowId}:${activityId}`, since the RA method cannot read Temporal context) |

**Why "queued M↔M" maps to `SignalExternalWorkflow`, not to a static Manager → Manager edge.** The book's queued sideways edge is satisfied operationally by Temporal: the source Manager's workflow calls `workflowExecutionAccess.SignalExternalWorkflow(target, ...)`, which is routed by the Temporal cluster to a workflow on the target Manager's task queue. From a layering perspective the call goes downward through ResourceAccess (`workflowExecutionAccess`) and the cluster, then up into a different Manager — there is no static sideways edge. The cardinality limit on queued Manager↔Manager edges (Don't 6b: do not queue to more than one Manager per use case) still applies and counts `SignalExternalWorkflow` calls.

## Layering style — prefer closed

From [Appendix C §3.4 "Layers"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec3) and [Ch. 3 §6](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev1sec6).

- Prefer **closed architecture**: a component may only call into the next layer down.
- Avoid open and semi-closed/semi-open architectures.
- Resolve apparent need to "open" the architecture by **queued calls** or **asynchronous event publishing** via a Pub/Sub Utility.
- Extend the system by adding subsystems, not by breaking layering rules.

## Key observations (Ch. 3 §4.3)

A well-designed Method system exhibits these qualities. Deviation is a smell:

| Observation | Check |
|---|---|
| Volatility decreases top-down | Clients are most volatile; Resources least. If a Resource is your most volatile component, reconsider. |
| Reuse increases top-down | Clients are hardly reusable; Utilities are universally reusable. If a Utility is single-use, it is probably not a Utility. |
| Managers are almost expendable | Each Manager's loss should leave Engines/ResourceAccess/Resources/Utilities reusable. If not, the Manager is too thick (likely functional decomposition). |
| Design is symmetric | Similar Managers and Engines designed similarly. If three of four use cases in a Manager publish events and the fourth does not, the asymmetry is a design smell. |

## How to cite

When another skill needs an interaction rule or a cardinality limit, link to this file and the specific subsection. For example:

> Per [the-method-layers](../the-method-layers/SKILL.md), Engines never receive queued calls (App C §3.6).

Do not restate the layer table or the don'ts inline in other skills. If you find yourself rewriting the interaction don'ts in a third skill, link here instead.

## See also

- [the-method-doctrine](../the-method-doctrine/SKILL.md) — the Prime Directive and 9 directives, of which Directives 1–4 are operationalised by this layer model.
- [the-method-architecture](../the-method-architecture/SKILL.md) — the procedure for binning volatilities into these layers and expressing them as Structurizr DSL.
- [`TEMPORAL-VOCABULARY.md`](../the-method-architecture/TEMPORAL-VOCABULARY.md) — the edge-label vocabulary that makes the Temporal mapping above explicit in `architecture.dsl` and sequence diagrams.
- [the-method-operational-concepts](../the-method-operational-concepts/SKILL.md) — where the infrastructure decision (Temporal vs plain) is made.
- [the-method-system-design-standard-check](../the-method-system-design-standard-check/SKILL.md) — the full Appendix C checklist applied as a quality gate.
