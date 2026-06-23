---
name: the-method-operational-concepts
description: System Design — document runtime interaction decisions (sync/queued, pub/sub, layering, patterns). Each decision justified against a business objective. Reads the committed .mission and .systemDesign from project.json. Produces the typed OperationalConcepts committed to project.json → .operationalConcepts. Invoke after [[the-method-architecture]], before [[the-method-system-design-standard-check]].
---

# Operational Concepts

The static architecture says what exists. Operational concepts say how it runs. Each decision must trace back to a business objective from the committed `.mission` artifact — otherwise it's gratuitous complexity.

When the chosen execution infrastructure for Managers is a durable workflow engine (Temporal is the default for this codebase), the operational concepts MUST also commit to the runtime vocabulary in `../the-method-architecture/TEMPORAL-VOCABULARY.md`. That vocabulary is shared by `architecture.dsl` edge labels, dynamic-view interactions, sequence diagrams, and the per-component service contracts produced in [[the-method-service-contract]].

## Canonical source

**Primary:** Löwy, [Ch. 5 §4.3a "Operational Concepts"](../../../../rightingsoftware/OEBPS/xhtml/ch05.xhtml#ch05lev2sec13a) — the TradeMe walkthrough.

**Supporting:**
- [Ch. 3 §6 "Open and Closed Architectures"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev1sec6) — layering style decision
- [Ch. 3 §6.4 "Relaxing the Rules"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev2sec17) — when to deviate from closed
- [Ch. 3 §5 "Subsystems and Services"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev1sec5) — subsystem boundaries
- [Ch. 3 §3.5 "Utilities Bar"](../../../../rightingsoftware/OEBPS/xhtml/ch03.xhtml#ch03lev2sec7) — Message Bus, etc.

**Temporal vocabulary (when applicable):**
- `../the-method-architecture/TEMPORAL-VOCABULARY.md` — workflow / activity / signal / query / update / timer / schedule / child workflow naming and edge-label grammar
- Temporal Encyclopedia: https://docs.temporal.io/encyclopedia
- Temporal Rust quickstart: https://docs.temporal.io/develop/rust/quickstart

## Input

State is git-as-DB: archistrator is a single Go-server repo whose canonical project state lives in `.aiarch/state/project.json` (a typed JSON aggregate). Markdown/DSL is a render-on-read of the typed state.

- The committed **mission** artifact → `.aiarch/state/project.json` → `.mission` — every operational decision must support an objective
- The committed **systemDesign** artifact → `.systemDesign` (the typed `System`: components, relationships, dynamic views — rendered as `architecture.dsl` for reading)

## Output

The typed **`OperationalConcepts`** model (Go shape in `server/internal/resourceaccess/projectstate/models_phase1.go`), committed to **`.aiarch/state/project.json` → `.operationalConcepts`**. NOT an `operational-concepts.md` file — any markdown below is a render-on-read of this slot. Per the two usage patterns (agentic/CI dispatch and local interactive), the agent emits the typed model and commits it into `.operationalConcepts`; the server stages it (`StageArtifactForReview`) for the human review gate.

## Procedure

The architect owns this entirely. No PM input needed.

### Step 1 — Communication topology AND Manager-execution infrastructure

Decide two things in this step (they constrain each other):

1. **External topology** — Message Bus, direct calls, or hybrid for Clients ↔ Managers and Managers ↔ Managers?
2. **Manager-execution infrastructure** — plain in-process request handlers, or a durable workflow engine such as **Temporal**?

Per ch. 5 (TradeMe): *"all communication between all Clients and all Managers takes place over the Message Bus Utility... justified by business objectives (extensibility, integration with external systems)."*

For each choice, document:

- **What** — the topology (direct / Message Bus / hybrid) AND the infrastructure (plain / Temporal / other durable engine)
- **Why** — cite the specific objective(s) from the committed `.mission` artifact
- **Cost** — what does this buy and what does it cost in team complexity? (Ch. 5 warning: *"Not every organization can justify using the pattern... calibrate to team capability."*)

**Temporal as the default Manager infrastructure.** When the system has long-running flows (architect-review gates that suspend for days, multi-step settlement, retry-with-backoff against flaky external systems, scheduled sweeps), **Temporal is the recommended infrastructure**. It absorbs:

- workflow checkpointing (no bespoke recovery loop)
- Activity-level RetryPolicy with backoff
- Signal/Query/Update message-passing into running workflow instances
- durable Timers and Schedules (cron-style recurrences without leader election)
- Child Workflows and ContinueAsNew for unbounded streams

If the team commits to Temporal here, every downstream artifact uses the vocabulary in `../the-method-architecture/TEMPORAL-VOCABULARY.md` — the static-architecture relationships block, each dynamic view, the sequence diagrams, and the service contracts. Document the determinism rules (no nondeterminism inside workflow code, all I/O via Activities, versioning strategy on workflow changes) as part of the cost discussion.

**Default to NOT-Temporal** for systems with no long-running flow, no scheduled work, no cross-process state recovery requirement, and tiny team. The book's plain "Workflow Manager + workflow store" is sufficient there; the Temporal vocabulary doesn't apply.

### Step 2 — Sync vs queued boundaries (Temporal primitive classification)

For each cross-component edge in the architecture, decide sync or queued AND — when on Temporal — name the Temporal primitive that mediates the call. The same map serves both purposes; sync/queued is the runtime mode, the Temporal primitive is the implementation.

Document a small table:

```markdown
## Sync / Queued Map

| From | To | Mode | Temporal primitive | Why |
|---|---|---|---|---|
| Client | OrderManager | Sync (caller blocks on enqueue) | `StartWorkflow(OrderWorkflow, ...)` | One inbound HTTP request → one Workflow start |
| OrderManager (workflow) | PaymentManager | Queued (cross-Manager) | `SignalExternalWorkflow(PaymentManager.workflowId, ChargeRequestedSignal, ...)` | Decouples payment processing; replaces a bus event |
| OrderManager (workflow) | PricingEngine | Sync, in-workflow | (none — deterministic call, not an Activity) | Same-thread pure computation |
| OrderManager (workflow) | OrderAccess | Sync (via Activity) | `Activity: AppendOrderEvent(...)` | Every I/O-bearing call from workflow code is a Temporal Activity |
| OrderManager (workflow) | self (sleep) | Sync, durable | `Timer(<duration>)` | Survives Worker restart |
| OrderManager (workflow) | self (await human) | Sync, durable | `Await Signal(<SignalName>) — workflow suspends` | Architect-review gate or operator action |
```

Rule: prefer queued for Manager↔Manager (App C §4c.iv recommends resolving sideways attempts via queued or async). On Temporal, "queued cross-Manager" becomes `SignalExternalWorkflow(...)` against the target Manager's task queue — the static architecture has no sideways Manager→Manager edge; the cluster mediates the signal.

### Step 3 — Pub/sub edges

List every event published in the system.

Per the Don'ts (App C §6, ch. 3 §6.5):
- **Only Clients and Managers may publish events**
- **Only Clients and Managers may subscribe**
- Engines, ResourceAccess, Resources may neither

Document each event:

```markdown
## Events

| Event name | Published by | Subscribers | Purpose |
|---|---|---|---|
| OrderPlaced | OrderManager | NotificationManager, AnalyticsManager | Trigger downstream workflows |
```

If any non-Manager wants to publish or subscribe, the decomposition is wrong — return to the-method-architecture.

### Step 4 — Layering style

State the architecture's layering style: **closed** (preferred), open, or semi-closed/semi-open.

Per App C §4: prefer closed. If you deviate, document the explicit justification.

Format:

```markdown
## Layering Style

**Chosen: Closed.**

Justification: closed architecture provides maximum encapsulation per The Method's default (App C §4a–c). No business objective in the committed `.mission` artifact requires relaxing closure.

Permitted exceptions used in this design:
- Queued calls between Managers (allowed by closed rules)
- Async event publishing for OrderPlaced (allowed by closed rules per ch. 3 §6.4)

Explicit don'ts enforced:
- No client calls Engine, ResourceAccess, or Resource directly
- No upward calls
- No skipping layers
```

### Step 5 — Patterns adopted

For each higher-order pattern in use, document and justify.

Common patterns from the book:

- **Workflow Manager** (ch. 5) — Managers load workflow instances from a workflow store, execute, persist back. Enables long-running, multi-device workflows. When implemented on **Temporal**, the workflow store IS the Temporal cluster's history service; the bespoke `appendEvent` / `recover` loop the book describes is absorbed by the framework. Use the Temporal-flavoured patterns listed below in place of the bespoke patterns.
- **Message-Is-the-Application** (ch. 5) — Clients send commands; Managers respond. Strong fit for Message-Bus topology, and equally for Temporal (the command becomes `StartWorkflow` / `SignalWorkflow` / `UpdateWorkflow`).
- **Subsystems** (ch. 3 §5) — when Manager count >5 or domain warrants partition, group Managers into subsystems. On Temporal, the natural subsystem boundary is the **Task Queue** — one queue per Manager makes the future split-out a redeploy, not a refactor.

Per ch. 5 caution: *"Not every organization can justify using the pattern... Always calibrate the architecture to the capability and maturity of the developers and management."*

**Temporal-flavoured patterns** (use when the infrastructure decision in Step 1 commits to Temporal). For naming conventions on each of these, see `../the-method-architecture/TEMPORAL-VOCABULARY.md`.

- **Workflow Manager — on Temporal.** Each Manager use-case method is a `WorkflowType`; each ResourceAccess call from the workflow is an `Activity`. Document the determinism rules, the Activity RetryPolicy library (named policies referenced from contracts), and the versioning strategy.
- **Signal-driven gate.** A workflow that suspends on `Await Signal(<SignalName>)` until an out-of-band caller delivers the decision via `SignalWorkflow(<workflowId>, <SignalName>, <payload>)`. Use for human-in-the-loop approvals, operator pauses, and async external-event injection. The suspend point is durable; the workflow id is the continuity token.
- **Scheduler — Temporal Schedules.** One `Schedule[<name>]` per recurring workload; firings execute as workflows on the target Manager's task queue. Replaces leader-elected cron in the application. Idempotent at the firing level (schedule firing id = workflow id).
- **Cross-Manager handoff — SignalExternalWorkflow.** Replaces what a Message Bus event would have been. The architecture has no Manager → Manager edge; the cluster mediates the signal.
- **Child workflows for per-unit work.** When a parent workflow processes a stream of units (one per activity, one per cycle), spawn an `ExecuteChildWorkflow(<ChildWorkflowType>, <input>)` per unit and `ContinueAsNew` the parent to bound its event history.

Format:

```markdown
## Patterns Adopted

### Workflow Manager — on Temporal
**Used in:** OrderManager, FulfillmentManager
**Business objective served:** Quick turnaround (objective 2), Customization (objective 3), Survive infra churn (objective N)
**Team capability:** Senior developers will lead introduction; juniors need ramp on (a) determinism rules, (b) RetryPolicy tuning, (c) versioning strategy. Capture these conventions in the `OperationalConcepts` model itself (the Temporal-determinism / RetryPolicy notes), not a separate `docs/` file.
**Workflow store:** Temporal cluster history service (no application-managed table)
**Business event log (separate concern):** Postgres event-sourced log appended to via `Activity: AppendEvent(...)`

### Signal-driven gate
**Used in:** OrderManager.approveOrder, FulfillmentManager.confirmShipment
**Business objective served:** Audit & intervention (objective 7), Customer self-sufficiency (objective 4)
**Implementation:** Workflow suspends on `Await Signal(<SignalName>)`; resumption is the gate.
**Continuity:** Workflow id `{customerId}:{orderId}` — any Client can `SignalWorkflow` from any channel.

### Scheduler — Temporal Schedules
**Used by:** nextActivity (every 30s), shortfallSweep (every 1h), closeSettlementCycle:<customerId> (per customer)
**Business objective served:** No leader election in app; exactly-once firing across the Worker pool.
**Implementation:** Each schedule is registered at startup; firings appear as workflow executions on the target Manager's task queue.
```

### Step 6 — State handling

For each Manager, document where workflow state lives. On Temporal, split into **technical state** (the workflow execution timeline) and **business state** (durable domain events) — they live in different stores and answer different questions.

- Stateless Manager + workflow store (preferred — supports multi-device, recovery)
- Stateful sessions (only when latency demands it; document the trade-off)

Format (plain infrastructure):

```markdown
## Workflow State

| Manager | Style | Storage |
|---|---|---|
| OrderManager | Stateless + workflow store | Postgres workflow_instances |
| MatchingManager | Stateless + workflow store | Same |
```

Format (Temporal infrastructure):

```markdown
## Workflow State

| Manager | Workflow checkpoints (technical) | Business state |
|---|---|---|
| OrderManager | Temporal cluster history (per workflow id) via `WorkflowExecutionAccess` | Postgres event log (`OrderPlaced`, `OrderShipped`, ...) appended via `Activity: AppendEvent(...)` |
| FulfillmentManager | Same | Same |

**Separation of concerns:**
- **Temporal cluster** holds workflow execution checkpoints — the technical timeline (which activity ran, what was returned, next decision). Replayable execution comes from here. Retention is operationally bounded (days for closed workflows).
- **Project / domain event log** holds the system's business events. Permanent retention. A business entity's lifecycle spans many workflow executions; the log is the single place to query the cross-workflow history.
```

### Step 7 — Subsystem boundaries (if any)

If the architecture has subsystems, document:

- Which Managers belong to which subsystem
- Communication style between subsystems (typically queued / event-driven)
- Independent deployability claim per subsystem

Per ch. 3 §5, subsystems should be "fairly decoupled and independent."

### Step 8 — Cross-checks

Walk the document and verify:

| Check | Action |
|---|---|
| Every decision cites at least one business objective from the committed `.mission` artifact | If not, drop the decision or rewrite the justification |
| No event publisher/subscriber violates the Don'ts | Fix architecture, not the doc |
| Layering style declared (closed/open/semi) | Add it if missing |
| Each adopted pattern names the team-capability assessment | Add it if missing |
| Subsystem count ≤5 (App C §2b) | Reconsider if exceeded |
| (Temporal infrastructure) Sync/Queued Map names a Temporal primitive per row | Add the column or split into sync-mode + primitive columns |
| (Temporal infrastructure) Determinism rules documented | Add the list (no system clock, no random IDs, all I/O via Activities, versioning policy) |
| (Temporal infrastructure) External-system idempotency boundaries enumerated per Activity | Add the per-Activity dedup-key table (Stripe Idempotency-Key, k8s manifest name, gateway event id, etc.) |
| (Temporal infrastructure) Workflow checkpoint store distinguished from business event log | Add the table — they are separate concerns |

## Exit criteria (for router)

`.aiarch/state/project.json` → `.operationalConcepts` holds the typed `OperationalConcepts` model with all eight sections. Each operational decision cites a `.mission` objective. No Don'ts are violated.

Move to `the-method-system-design-standard-check`.

## Anti-patterns to reject

- **"Because everyone does it"** justification — not in the book; not acceptable.
- **Message Bus without a supporting objective** — strip or justify against actual business need.
- **Workflow Manager pattern when no long-running workflow exists** — over-engineering.
- **Stateful Managers without latency justification** — gives up multi-device support; default to stateless.
- **Open architecture by default** — closed is the book's preferred style; explicit justification required to deviate.
