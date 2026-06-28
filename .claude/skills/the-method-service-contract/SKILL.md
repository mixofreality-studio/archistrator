---
name: the-method-service-contract
description: Design the service contract for one component before construction starts. 3–5 ops per contract, max 12, reject ≥20. Walks Appendix B contract design rules and Appendix C §6 standard check. Invoke per component during detailed-design activities in Phase 3.
---

# Service Contract

The service contract is the public face of a component — the set of operations its clients call. The decomposition (Phase 1) gave you the components; this skill turns each component into a contract that sits in the area of minimum cost. Get this wrong and the system's per-service and integration costs are non-linearly worse, regardless of how clean the architecture looked.

Invoke this skill once per component during Phase 3's detailed-design activities. Each invocation produces one contract file. Together with `[[the-method-handoff]]`, this skill defines who-designs-what and to-what-standard during construction.

Per `[[the-method-doctrine]]` Directive 4 (*features as integration, not implementation*): operations on the contract are integration points where features compose — they are not implementations of features. A contract operation named after a feature is a Directive 4 violation.

## Canonical source

**Primary:** Löwy, [Appendix B "Service Contract Design"](../../../research/rightingsoftware/OEBPS/xhtml/appb.xhtml).

Sections walked:
- [§1 "Is This a Good Design?"](../../../research/rightingsoftware/OEBPS/xhtml/appb.xhtml#appblev1sec1) and [§2 "Modularity and Cost"](../../../research/rightingsoftware/OEBPS/xhtml/appb.xhtml#appblev1sec2) — the area-of-minimum-cost framing
- [§3 "Services and Contracts"](../../../research/rightingsoftware/OEBPS/xhtml/appb.xhtml#appblev1sec3) — contracts as facets; cohesive, logically consistent, independent
- [§4 "Factoring Contracts"](../../../research/rightingsoftware/OEBPS/xhtml/appb.xhtml#appblev1sec4) — factor down, sideways, or up
- [§5 "Contract Design Metrics"](../../../research/rightingsoftware/OEBPS/xhtml/appb.xhtml#appblev1sec5) — size, properties, contracts-per-service
- [§6 "The Contract Design Challenge"](../../../research/rightingsoftware/OEBPS/xhtml/appb.xhtml#appblev1sec6) — why this is hard and who should do it

**Standard reference:** [App C §6 "Service Contract Design Guidelines"](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec6) — six items, walked as a checklist at the end of every contract design.

Cross-references:
- `[[the-method-layers]]` — contracts must respect call-direction rules (Managers may queue to Managers; Engines/RA may not receive queued calls; only Managers and Clients publish or subscribe to events). The contract is the public surface those rules constrain. Includes the "Temporal mapping" table — which applies to the **Manager layer only**; Engines/ResourceAccess/Resources import no Temporal.
- `[[the-method-handoff]]` — who designs and reviews each contract.
- **Temporal vocabulary (Manager layer only).** The canonical naming and grammar for Workflow / Activity / Signal / Query / Update / Timer / Schedule / ChildWorkflow / ContinueAsNew. These names belong to the **Manager layer**, which owns Temporal; every Manager contract MUST classify each public op as one of these primitives. Engine/ResourceAccess/Resource/Utility contracts carry no Temporal classification.

## Input

State is git-as-DB: all of this lives in `.aiarch/state/project.json` (a typed JSON aggregate), NOT in `designs/<product>/*.md` files. Markdown/DSL is a render-on-read of the typed state, never the source of truth.

- The component from the committed **architecture** artifact in `project.json` (its name, layer tag, description, and the relationships into and out of it).
- The component's relevant **operational concepts** from the committed operational-concepts artifact (sync/queued map entries, pub/sub events, layered interaction notes).
- The **hand-off model** from the committed handoff artifact — who designs and who reviews.

## Output

The contract is a **typed entry in `.aiarch/state/project.json` under `.serviceContracts["<ComponentName>"]`** — git is the database. It is NOT a `designs/.../contracts/<ComponentName>.md` file; any markdown (including the Step 8 template) is a render-on-read of that JSON.

The JSON shape mirrors the Go `ServiceContract` type (`server/internal/resourceaccess/projectstate/servicecontract.go`): `Component`, `Layer`, `Stereotype`, `Volatility`, `Status` (`IN-DESIGN` | `FROZEN`), `Inbound` / `Outbound` parties, `Ops` (operation signatures + their I/O structs), `DataContracts`, `ErrorModel`, `Idempotency`, `Revisions`.

The `.serviceContracts` map accumulates the per-component contract corpus that drives Phase 3 construction. A construction CI run extracts one entry to `service-contract.json` at the repo root for the implementer. Repeat the skill for each component.

## Procedure

### Step 1 — Enumerate operations the component must expose

From the architecture and operational concepts:

- For each dynamic view that touches the component, list the operations a caller invokes on it.
- For each event the component publishes or subscribes to (Managers and Clients only — per `[[the-method-layers]]`), list it as a pub/sub interaction, not a contract operation.
- For each cross-Manager queued call into this component, list the queued operation.

Do not invent operations to anticipate features. Each operation must trace to a current operational-concepts entry. New operations to support new use cases are added later, when those use cases are designed; the contract evolves with the system.

### Step 2 — Group operations into contract(s)

Per App B §3.2: a contract is a *facet* of the service. A service may support one or two contracts (App B §5.4 / App C §6.4). Most services have one.

Group operations so that the operations within a single contract are:

- **Logically consistent.** All operations belong to the same facet. `ReadCode()` and `OpenPort()` are not the same facet (App B §4.2 "Factoring Sideways").
- **Cohesive.** The contract describes the interaction completely — no missing operation that callers will need (App B §3.3).
- **Independent.** The contract does not depend on another contract to make sense (App B §3.3).

If a candidate operation does not fit any of the existing groups, split it into a separate contract. App B §4 names the three factoring moves:

| Move | When | How |
|---|---|---|
| Factor down | An operation makes sense only for some implementations (e.g., `AdjustBeam` only for optical scanners) | Move it to a derived contract; let the base contract serve the broader population |
| Factor sideways | Operations are unrelated facets (e.g., reading vs port management) | Split into two independent contracts, both supported by the same service |
| Factor up | The same operation appears in unrelated contracts and belongs equally to both (e.g., `Abort`, `RunDiagnostics`) | Pull it into a common base contract |

### Step 3 — Apply the size metrics (App B §5.2 / App C §6.2)

| Operation count | Verdict | Action |
|---|---|---|
| 1 | Suspect | Investigate — too coarse, too few parameters? Factor up into a wider contract, or fold into the next subsystem. App B §5.2: *"a contract with just one operation is a red flag."* |
| 2 | Suspect for the same reason | Same investigation |
| **3–5** | **Target range** | Proceed |
| 6–9 | Drifting away from minimum cost | Look for collapsible operations |
| 10–12 | Borderline poor | Look hard for factoring opportunities; document why if you keep it |
| ≥ 13 | Poor design | Must factor. Do not waive. |
| ≥ 20 | **Reject** | App B §5.2 / App C §6.2.d: *"You must immediately reject contracts with 20 operations or more."* No justification can save this. Return to Step 2. |

The size metric is an evaluation tool, not a validation tool (App B §5.5). Passing the size metric does not prove the contract is good — but failing it proves the contract is bad.

### Step 4 — Apply the property-and-shape rules

- **No property-like operations** (App B §5.3 / App C §6.3). `GetX` / `SetX` pairs leak state shape to clients. Replace with behavioural operations: `DoSomething`, not `GetSomethingAndDecideWhatToDo`.
- **No CRUD shape.** A contract whose operations are `Create / Read / Update / Delete` is a data contract pretending to be a service contract. If the component is genuinely a CRUD store, it is a ResourceAccess and its contract should still expose business-meaningful operations, not raw CRUD.
- **Contracts per service: 1 or 2** (App B §5.4 / App C §6.4). Three or more independent contracts on one service usually signals the service is too big — split the service, or merge two of the contracts into the next-best facet.

### Step 5 — Verify layer interaction rules against the contract

Cross-reference `[[the-method-layers]]`:

| The contract's component is | Then operations must... |
|---|---|
| Manager | Be invokable by Clients and by other Managers (queued). May call Engines / ResourceAccess. May publish / subscribe to events. |
| Engine | Be invokable by Managers (and ResourceAccess in rare cases). No queued operations on this contract. No events published. |
| ResourceAccess | Be invokable by Managers and Engines. No queued operations. No events published. |
| Resource | Domain-specific contract. No queued operations. No events published. |
| Utility | Invokable by anyone. |
| Client | This component does not expose a contract (Clients consume, do not provide). If you find yourself drafting a contract for a Client, the component is misclassified. |

If the contract violates a layer rule (e.g., an Engine contract that publishes events), the violation is **in the decomposition**, not in the contract. Return to `[[the-method-architecture]]` and fix it there.

### Step 5b — Classify Manager operations by Temporal primitive (when Manager infrastructure is Temporal)

**Temporal is a Manager-layer concern only.** The Manager layer owns workflow/sequencing and is the *only* layer that imports or references Temporal. Engine, ResourceAccess, Resource, and Utility code MUST NOT import Temporal or carry any Temporal classification on their contracts. When a Manager workflow needs to make a call that does I/O (a ResourceAccess op), the Manager wraps that call in a Temporal **Activity it defines and registers itself** — the Activity body delegates to the plain ResourceAccess method. Pure, deterministic Engine ops are called directly from workflow code (no Activity wrapper, because replay re-derives the same result).

So the "op kind" classification below applies to **Manager contracts only**:

| Component layer | Temporal classification | Notes |
|---|---|---|
| Manager | `Workflow` (entry method invoked via `StartWorkflow`) · `Signal` (handler invoked via `SignalWorkflow`) · `Query` (handler invoked via `QueryWorkflow`, read-only) · `Update` (handler invoked via `UpdateWorkflow`, validated write with response) · `Schedule` (a recurring workflow registered at startup; not invoked directly by callers) | A single Manager service-contract typically lists multiple `Workflow` ops (one per use-case method) and the `Signal` / `Query` / `Update` ops the inflight workflows accept. **In addition**, the Manager contract documents the **Activities it registers** — one per ResourceAccess call the workflow makes — each with its `RetryPolicy` name (e.g., `default`, `aggressive`, `externalGateway`) and explicit timeouts (`StartToClose`, optional `ScheduleToClose`, `HeartbeatTimeout` for long-running). These Activities are the Manager's implementation surface, not public ops callers invoke. |
| Engine | **None.** A plain, deterministic, no-I/O function. Imports no Temporal. The contract MUST declare the determinism guarantees (no `time.Now()`, no RNG, no global mutable state, no I/O) so the calling Manager can safely invoke it directly from workflow code. Naming: imperative or noun-phrase, NOT past tense. |
| ResourceAccess | **None.** A plain method that performs I/O. Imports no Temporal. The contract declares **idempotency** and **error-retryability** (which error classes are transient vs terminal — a property of the operation, not of Temporal), plus a non-normative **orchestration guidance** note (recommended timeout, which errors are terminal). The Manager that calls this op wraps it in a Temporal Activity and chooses the actual `RetryPolicy`/timeouts at that call site. |
| Resource | **None** — storage/external systems, not Temporal participants. | |
| Utility | **None** — synchronous in-process. | |

A Manager contract lacking the Temporal classification (on its public ops *and* its registered Activities) is **incomplete**, not a "minor omission" — the classification drives downstream construction (Worker registration, signal/query handler wiring, RetryPolicy selection). Conversely, **any Temporal classification appearing on an Engine, ResourceAccess, Resource, or Utility contract is a layer violation** — those layers do not know Temporal exists.

### Step 5c — Walk the `[[the-method-layers]]` Temporal mapping (Manager contracts only)

If this contract is a **Manager**, walk the mapping in `[[the-method-layers]]` "Temporal mapping" for every public op and every Activity the Manager registers. If a public op falls outside that table (e.g., a Manager op invoked via direct method call rather than `StartWorkflow`), either:

1. revise the infrastructure decision in `[[the-method-operational-concepts]]` (some Managers might be Temporal-hosted, others not), or
2. revise the op's classification.

Do not allow a public op on a "Temporal Manager" contract that bypasses Temporal.

If this contract is an **Engine, ResourceAccess, Resource, or Utility**, skip this step entirely — these layers do not participate in Temporal and their contracts carry no Temporal classification (per Step 5b).

### Step 6 — Verify reuse-readiness

App B §3.3 / §4: good contracts are reusable in principle even when no one else plans to reuse them today. The reusability lens catches:

- Operations specific to the current implementation (e.g., `ScanBarcode128` vs `ReadCode`) — too narrow.
- Operations describing a workflow (`StartOrderThenReserveStockThenChargeCard`) — UI flow leaking into the contract.
- Operations that would change if the underlying technology swapped — encapsulation broke.

Ask: *"If a second team built a different service implementing the same business facet, would they implement this contract unchanged?"* If yes, the contract is reusable. If no, revise.

### Step 7 — Walk the App C §6 standard check

Six items. Each marked PASS / WAIVED (with justification) / FAIL (return to Step 2).

| # | Guideline | How to verify against this contract | Status |
|---|---|---|---|
| 1 | Design reusable service contracts | The reuse test from Step 6 passes — the contract describes a business facet, not an implementation | |
| 2a | Avoid contracts with a single operation | Operation count ≥ 2; if exactly 2, both have been justified | |
| 2b | Strive for 3–5 operations | Operation count is in [3, 5] (preferred) or [3, 9] (acceptable with justification) | |
| 2c | Avoid contracts with more than 12 operations | Operation count ≤ 12 | |
| 2d | Reject contracts with 20+ operations | Operation count < 20 — non-waivable | |
| 3 | Avoid property-like operations | No `GetX` / `SetX` pairs; no operations whose name reveals state shape | |
| 4 | Limit contracts per service to 1 or 2 | This service supports ≤ 2 contracts total (counting this one) | |
| 5 | Avoid junior hand-offs | Per the committed handoff artifact, this contract is being designed by the architect or a senior developer — not a junior | |
| 6 | Only architect or competent senior developers design contracts | Designer named in the committed handoff artifact's contract assignment table | |

Items 2d, 5, and 6 are non-waivable. The others may be waived with a written justification.

### Step 8 — Record the contract in `project.json .serviceContracts["<ComponentName>"]`

The canonical form is the typed JSON entry (shape in **Output** above). The markdown below is the equivalent **human rendering** — use it to review the contract, but the source of truth is the JSON in `.aiarch/state/project.json`, not a `designs/*.md` file:

```markdown
# Service Contract — <ComponentName>

Component: <ComponentName>
Layer: <Manager | Engine | ResourceAccess | Resource | Utility>
Designer: <name from the committed handoff artifact>
Reviewer: <architect>
Date: <YYYY-MM-DD>

## Facets (contracts)

This service supports N contract(s):
1. `I<Name>` — <one-sentence facet description>
2. (optional second contract — non-business cross-cutting facet only, e.g., diagnostics)

## Contract 1 — `I<Name>`

### Description
What facet of the service this contract represents. Written in business language, not implementation language.

### Operations

**Manager contract** — include the "Temporal kind" column:

| Operation | Temporal kind | Inputs | Output | Behaviour |
|---|---|---|---|---|
| `op1(a, b)` | Workflow / Signal / Query / Update / Schedule | `a: TypeA, b: TypeB` | `ResultC` | Behaviour described as a contract: pre-condition, post-condition, side-effects, sync/queued. For Workflows: name the workflow id derivation. For Signals/Queries/Updates: name the WorkflowType this op handles. |
| `op2(x)` | ... | `x: TypeX` | `void` | ... |

**Engine / ResourceAccess / Resource / Utility contract** — omit the "Temporal kind" column entirely (these layers import no Temporal — see Step 5b):

| Operation | Inputs | Output | Behaviour |
|---|---|---|---|
| `op1(a, b)` | `a: TypeA, b: TypeB` | `ResultC` | Pre-condition, post-condition, side-effects. For ResourceAccess: declare idempotency + error-retryability (transient vs terminal) + advisory orchestration guidance (recommended timeout). For Engines: declare determinism guarantees. |
| `op2(x)` | `x: TypeX` | `void` | ... |

### Layer interaction notes
- Callers: <list of Manager / Client names from the committed architecture artifact>
- Sync or queued: <per the committed operational-concepts artifact sync/queued map>
- Events published: <none for Engine / RA / Resource; events listed for Manager and Client>
- Events subscribed: <same constraint>

**Manager contracts only** — add these Temporal-infrastructure notes:
- Task queue: <e.g., `system-design`, `construction` — one queue per Manager>
- Workflow id derivation: <how the workflow id is computed from inputs, e.g., `{projectId}:{artifactKind}`>
- Activities registered: <one per ResourceAccess call this Manager's workflows make — each wrapping a plain RA method, with its RetryPolicy name and timeouts. The RA component imports no Temporal; the Activity wrapper lives here, in the Manager.>
- RetryPolicy library reference: <link to the committed operational-concepts artifact "Activity RetryPolicies" section>

## Factoring decisions

- <Why these operations belong together as one facet>
- <Any factor-down, factor-sideways, or factor-up decisions taken; cite App B examples>
- <Any operation that was tempting to add and was rejected, with reason>

## Reuse review

How a different team implementing the same business facet would implement this contract unchanged. If they would have to change it, identify which operation leaks the current implementation and revise.

## App C §6 standard check

| # | Guideline | Status | Justification (if waived) |
|---|---|---|---|
| 1 | Reusable | PASS | |
| 2a | ≥ 2 operations | PASS | |
| 2b | 3–5 operations | PASS (4 ops) | |
| 2c | ≤ 12 operations | PASS | |
| 2d | < 20 operations | PASS | |
| 3 | No property-like ops | PASS | |
| 4 | ≤ 2 contracts per service | PASS | |
| 5 | Not a junior hand-off | PASS (designer: senior-dev-1) | |
| 6 | Architect or senior designer | PASS (designer: senior-dev-1, reviewer: architect) | |

## Open questions for review

- <Anything the architect should resolve before sign-off>
```

## Exit criteria (for router)

- A `.serviceContracts["<ComponentName>"]` entry exists in `.aiarch/state/project.json` (shape per **Output**)
- Operations are listed with inputs, outputs, and behaviour
- Operation count is in [3, 12] (or in [2, 12] with a written justification for the low count); count is **never** ≥ 20
- No property-like operations
- Number of contracts on the service is 1 or 2
- Layer interaction rules respected (verified against `[[the-method-layers]]`)
- Designer and reviewer named per the committed handoff artifact
- All nine App C §6 items are PASS or WAIVED with justification — items 2d, 5, and 6 are PASS only (non-waivable)
- Architect has reviewed (review chain from the committed handoff artifact)

Repeat per component until every Manager, Engine, ResourceAccess, and Resource has a contract file. Then construction may begin on a per-component basis.

## When to revisit a contract

- A new use case the contract was not designed for needs to invoke the component — review whether the use case is an integration of existing operations (preferred) or whether a new operation is required (re-run this skill, append a revision)
- A `[[the-method-scope-change]]` event modifies the component's responsibility — re-run this skill for that component
- A code review during construction reveals the contract leaks implementation — re-run from Step 6

## Anti-patterns to reject

- **Fat managers (≥ 20 operations)** — App B §5.2 / App C §6.2.d: non-waivable. Return to Step 2 and factor.
- **Property-style operations** (`GetX` / `SetX`) — these are CRUD, not service operations. App B §5.3.
- **Contracts that mirror UI flows** — UI volatility leaks into the contract. The contract is the integration point; the UI sequences calls to it, not the other way round.
- **CRUD-shaped contracts on Managers** — Managers orchestrate behaviour, not data. CRUD on a Manager means the Manager is misclassified as a data store.
- **One-operation contracts** — App B §5.2 red flag. Investigate before accepting.
- **Three or more contracts on one service** — App C §6.4 violated. Split the service or merge facets.
- **Designing contracts to pass the metrics** — App B §5.5 explicitly warns against this. Design for cohesion and reuse; if the contract is good, the metrics follow.
- **Junior hand-off without architect review of every contract** — App C §6.5 / §6.6 violated. Non-waivable.
- **Skipping the App C §6 check** — the check is what makes contract review reproducible across the team.
