# Structurizr conventions for The Method

How to encode Löwy's volatility-based decomposition in Structurizr DSL.

## File location

`designs/<product>/system/architecture.dsl`

## Element model

The Method's layers map to Structurizr **containers** inside a single
**software system**, tagged by layer. Use `tags` to drive styling and
validation.

```
clients          → tag "client"
managers         → tag "manager"
engines          → tag "engine"
resourceAccess   → tag "resource-access"
resources        → tag "resource"
utilities (bar)  → tag "utility"
```

## Description style

Element descriptions (the second quoted string on a `component` / `container` /
`softwareSystem` line) must be **≤ 150 characters**. Keep them short: name what
the element **encapsulates**, then a brief verb-phrase for what it does.

The component description is metadata in the diagram — not the place to
document edge cases, retention policy, persistence schema, or rationale.
That detail belongs in:

- comment blocks above the element in the DSL,
- `operational-concepts.md` (runtime behavior, idempotency, retries),
- `volatilities.md` (the *why* of the encapsulation).

**Pattern by layer:**

| Layer | Template |
|---|---|
| Manager | `Encapsulates <Workflow> volatility. <one-line role>.` |
| Engine | `Encapsulates <Policy/Model> volatility. <one-line computation>.` |
| ResourceAccess | `Atomic verbs over <Resource>: <verb1>, <verb2>, <verb3>.` *(or)* `Encapsulates <Resource> volatility. <one-line role>.` |
| Resource | `<storage kind / external system> for <purpose>. <one durability/scope note>.` |
| Client | `<transport> for <actor/origin>. Routes to <Manager>.` |

❌ Avoid (over 150, mixes responsibility with implementation detail):

```dsl
settlementEngine = component "Settlement Engine" "Encapsulates RevenueShareTerm + ComputeCostPricing + SettlementSchedule + BillingTerms volatility: given a cycle's revenue events, compute events, and the customer's terms, produces the signed net amount and routes payout/charge. Multi-purpose by intent — split candidate flagged in volatilities.md if it grows." "kotlin" "engine"
```

✅ Preferred:

```dsl
settlementEngine = component "Settlement Engine" "Encapsulates RevenueShareTerm + ComputeCostPricing + SettlementSchedule + BillingTerms volatility. Computes signed net and payout/charge routing." "kotlin" "engine"
```

## Edge-label conventions

Relationship labels name **what** the caller invokes, in the **vocabulary of
the destination layer's responsibility**. Architecture diagrams are infrastructure-
agnostic — never put workflow-engine primitives (`Activity:`,
`StartWorkflow(...)`, `SignalExternalWorkflow`, `Await Signal`, etc.) in
the labels. Those belong in `operational-concepts.md`.

| Edge | Label shape | Why |
|---|---|---|
| Client → Manager | `<managerMethodName>(<args>) → <result>` (or just `<managerMethodName>`) | The Client invokes a method on the Manager. The label IS the method name. List alternative methods on the same edge with `\|`. |
| Manager → Engine | `<EngineMethodName>(<args>) → <output>` | Engines are pure computation. The label is the engine method signature. |
| Manager → ResourceAccess | `<atomicBusinessVerb>(<noun>)` (e.g., `appendEvent(OrderSubmitted)`, `readProjection(projectId)`) | ResourceAccess exposes atomic *business* verbs, not CRUD and not workflow primitives. The label names the verb. Multiple verbs on one edge: separate with `/`. |
| ResourceAccess → Resource | resource-domain verb + idempotency note (e.g., `append entry / read range (idempotency: UNIQUE(event_id))`) | Use verbs that name the *effect* at the resource boundary, not platform-specific commands. If swapping the platform (Argo→Flux, Postgres→DynamoDB, Stripe→Adyen) would force a label change, the label is too implementation-specific — pull the platform name out and rename to the generic effect. |
| Manager → Manager (queued) | `delivers <SignalName> (queued)` | The closed-layering queued-sideways exception. Label names the business signal; the delivery infrastructure is operational detail. |
| anyone → Utility | `<verb>` (e.g., `Logs`, `AuthN/AuthZ`) | Cross-cutting; one-word verbs suffice. |

❌ Avoid (leaks infrastructure or platform names into labels):

```
webClient -> orderManager "StartWorkflow(SubmitOrderWorkflow, orderId, SubmitOrderRequest)"
orderManager -> orderAccess "Activity: AppendEvent(OrderSubmitted) [StartToClose=15m]"
orderManager -> pricingEngine "Activity: ComputeTotal(lineItems) → total"
operatedRuntimeAccess -> operatedRuntime "git commit manifests; ArgoCD reconciles"
merchantGatewayAccess -> merchantGateway "POST /charges (Stripe Idempotency-Key header)"
```

✅ Preferred (vocabulary of the destination layer; platform-agnostic):

```
webClient -> orderManager "submitOrder(orderId, lineItems)"
orderManager -> orderAccess "appendEvent(OrderSubmitted)"
orderManager -> pricingEngine "ComputeTotal(lineItems) → total"
operatedRuntimeAccess -> operatedRuntime "publish desired state (idempotency: deterministic manifest paths)"
merchantGatewayAccess -> merchantGateway "charge customer (idempotency: gateway event id)"
```

**Rule of thumb.** If you swapped Temporal for Akka, Argo for Flux,
Postgres for DynamoDB, or Stripe for Adyen, would the label have to
change? If yes, the label is leaking implementation detail through the
encapsulation — pull the platform name out and use the generic effect.
Infrastructure-specific primitives, retry/timeout policies, and platform API
shapes belong in `operational-concepts.md`, not in Structurizr labels.

## Template

This is the starting template that `/system-design` writes. Names, layers,
and call chains get filled in per product.

```dsl
workspace "<Product Name>" "Volatility-based decomposition per The Method." {

    !identifiers hierarchical

    model {
        // ---- Actors ----
        user = person "User" "Primary customer / system user."

        // ---- The System ----
        system = softwareSystem "<Product Name>" "<one-line mission>" {

            // ===== Clients =====
            // Entry points. UI / public API.
            webApp = container "Web App" "User-facing web client." "react" "client"

            // ===== Managers =====
            // Workflow orchestration. Encapsulates use-case volatility.
            // Managers are "almost expendable" — they hold the volatile glue.
            // <ManagerName>Manager = container "<Manager Name>" "Encapsulates <volatility>." "" "manager"

            // ===== Engines =====
            // Pure business activities. No I/O.
            // <EngineName>Engine = container "<Engine Name>" "Encapsulates <volatility>." "" "engine"

            // ===== ResourceAccess =====
            // Atomic business verbs against resources. Resource-neutral interface.
            // <Access>Access = container "<Access> Access" "Atomic verbs over <Resource>." "" "resource-access"

            // ===== Resources =====
            // Data / external systems.
            // <Resource>Db = container "<Resource> DB" "" "postgres" "resource"

            // ===== Utilities bar =====
            // Cross-cutting. Anyone can call.
            logging  = container "Logging"  "Structured logs."     "" "utility"
            security = container "Security" "AuthN/AuthZ."          "" "utility"
            diagnostics = container "Diagnostics" "Health/metrics." "" "utility"
        }

        // ---- Relationships ----
        // Only the rules from The Method are allowed:
        //   Client → one Manager per use case
        //   Manager → Engine | ResourceAccess | (queued) Manager
        //   Engine → ResourceAccess
        //   ResourceAccess → Resource
        //   anyone → Utility
        //
        // Define the relationships needed to support every core use case.
    }

    views {
        // -------- Static architecture (the layered pyramid) --------
        container system "static-architecture" "Layered static architecture." {
            include *
            autolayout tb
        }

        // -------- One dynamic view per CORE USE CASE = a call chain --------
        // dynamic system "<use-case-key>" "<Use Case Name>" {
        //     user -> system.webApp "<actor action>"
        //     system.webApp -> system.<manager> "<API call>"
        //     system.<manager> -> system.<engine> "<method>"
        //     system.<manager> -> system.<access> "<verb>"
        //     system.<access> -> system.<resource> "<I/O>"
        //     autolayout lr
        // }

        styles {
            element "Person" {
                shape Person
                color "#ffffff"
                background "#1b5e20"
            }
            element "client" {
                background "#2e7d32"
                color "#ffffff"
            }
            element "manager" {
                background "#1565c0"
                color "#ffffff"
            }
            element "engine" {
                background "#6a1b9a"
                color "#ffffff"
            }
            element "resource-access" {
                background "#ef6c00"
                color "#ffffff"
            }
            element "resource" {
                background "#424242"
                color "#ffffff"
                shape Cylinder
            }
            element "utility" {
                background "#546e7a"
                color "#ffffff"
                shape RoundedBox
            }
        }
    }
}
```

## Validation rules (used by `/system-design`)

After the DSL is written, the system-architect agent validates it against The
Method's rules. Each rule below is an automated check.

| Rule | Check |
|---|---|
| Every core use case has a dynamic view | Count of dynamic views == count of core use cases |
| Clients call exactly one Manager per use case | In each dynamic view, count of distinct Manager targets ≤ 1 |
| No calling up | No relationship goes from a lower layer to a higher layer |
| No calling sideways within a layer | Except queued Manager→Manager (model as `delivers <SignalName> (queued)`) |
| No skipping layers | Client doesn't call Engine/ResourceAccess/Resource directly |
| Engines/ResourceAccess/Resources don't subscribe | No incoming queued edges |
| Cardinality | ≤5 Managers (no subsystems); ≤3 per subsystem; more Engines than Managers |
| Total component count | Order of magnitude 10 |
| Edge-label vocabulary | Labels use the destination layer's vocabulary: Client→Manager = manager method name; Manager→Engine = engine method signature; Manager→ResourceAccess = atomic business verbs; ResourceAccess→Resource = resource-native I/O. No workflow-engine primitives in labels (no `Activity:`, no `StartWorkflow(`, etc.). See "Edge-label conventions" above. |
| No dynamic-view edge targets the infrastructure ResourceAccess | Dynamic views show business call chains; the durable-execution infrastructure is an implementation detail. The static-architecture view retains all Manager → infrastructure-access edges. See "Infrastructure ResourceAccess is omitted from dynamic views" below. |

## Rendering

For local viewing, use the project's `structurizr-serve` wrapper. It runs the
current `structurizr/structurizr` Docker image (NOT the deprecated
`structurizr/lite`) and mounts the system directory:

```bash
./methodpoc/structurizr-serve <product>
# open http://localhost:8080
```

The parser expects the workspace file to be named `workspace.dsl`. The
architect writes the canonical artifact to `architecture.dsl` AND a
byte-identical copy to `workspace.dsl`. The PostToolUse hook (see
"Validation" below) keeps the copy in sync after any edit to
`architecture.dsl`.

## Validation

After ANY edit to a `*.dsl` file under `designs/<product>/system/`, the file
MUST be validated against the parser. This is non-negotiable — Structurizr is
strict and the parser errors are not always obvious from reading the file
(see "Common DSL pitfalls" below).

**Manual command (any project):**

```bash
./methodpoc/structurizr-validate <product>
```

The script wraps `structurizr/structurizr validate` and additionally treats
any `ERROR` line in the parser output as a failure — the bare validate
command exits 0 on certain syntax errors (notably `styles` block syntax) even
though the server cannot load the workspace. The wrapper script catches
these.

**Automatic validation (PostToolUse hook):**

`methodpoc/.claude/settings.local.json` registers a PostToolUse hook for
`Edit|Write|MultiEdit`. The hook script at
`methodpoc/.claude/hooks/validate-structurizr.sh`:

1. No-ops silently for any file path that is not
   `methodpoc/designs/<project>/system/*.dsl`.
2. When `architecture.dsl` is edited, syncs the change to `workspace.dsl`.
3. Runs `structurizr-validate --file <path>` on the edited file.
4. On validation failure, exits 2 with the parser output on stderr — the
   agent sees the error in the tool result and cannot proceed until it
   fixes the DSL.

**Exit criteria for any architecture skill that writes DSL**: the file
parses cleanly under `structurizr-validate`. Do not move on to the next
step with a non-validating DSL.

## Common DSL pitfalls

The Structurizr DSL parser has several traps that bite agents. Encode all of
these by following the template above.

**Pitfall: inline-brace `element` blocks in `styles`.** The new
`structurizr/structurizr` parser rejects inline-brace form for `element`.
Each property must be on its own line.

❌ Rejected (server fails to load, `validate` quietly returns 0):

```dsl
styles {
    element "Person" { shape Person color "#ffffff" background "#1b5e20" }
}
```

✅ Required:

```dsl
styles {
    element "Person" {
        shape Person
        color "#ffffff"
        background "#1b5e20"
    }
}
```

**Pitfall: dynamic-view edges that don't exist in the model.** Every
relationship used inside a `dynamic` view MUST already be declared in the
`model` block. The parser does NOT auto-create edges from dynamic views.

❌ Parser error: `A relationship between <X> and <Y> does not exist in model`.

The fix is always to add the missing `<X> -> <Y> "..."` line under the
`// Manager → ResourceAccess` (or appropriate) section of the model, not to
remove the dynamic-view edge.

**Pitfall: workspace.dsl drift from architecture.dsl.** The hook syncs
on edits to `architecture.dsl`. If you edit `workspace.dsl` directly, no
sync happens — the canonical artifact silently drifts. Always edit
`architecture.dsl` and let the hook propagate.

**Pitfall: escaped quotes inside relationship descriptions.** The parser
does NOT support `\"` inside `"..."` relationship-description strings.
If a label needs to wrap a name in quotes, use square brackets instead —
e.g., `deliver[orderSubmitted]`, not `deliver \"orderSubmitted\"`.

## Why dynamic views = call chains

The book (ch. 4) defines a call chain as "a sequence-style diagram per core
use case showing the chain through Client → Manager → Engine/ResourceAccess
→ Resource." Structurizr's dynamic views are exactly that primitive: an
ordered list of relationships between containers, scoped to a single use
case. The dynamic view IS the call chain.

## Infrastructure ResourceAccess is omitted from dynamic views

A ResourceAccess that fronts a Manager-execution infrastructure — any
durable-workflow engine, actor cluster, or scheduler runtime that the
Manager's use-case methods execute *on* — exists in the **static**
architecture and the **relationships** block. It is the encapsulation
of WorkflowRuntime volatility and must be visible there.

It does NOT appear in **dynamic views (per-use-case call chains)**.

**Why.** Löwy never draws the workflow engine inside every TradeMe call
chain (ch. 5). The engine is the infrastructure the Manager runs *on*, not a
participant in the business call chain. Showing it in every dynamic view:

- repeats the same edges across every use case (every Manager has the
  same infrastructure primitives: timers, signals, child executions, schedule
  registrations),
- obscures the business flow with infrastructure primitives,
- treats an implementation detail (which infrastructure was chosen) as if it
  were part of the use case's domain semantics — it isn't.

**Where the infrastructure behaviour goes instead.** Infrastructure primitives —
durable timers, awaited signals, cross-workflow signals, child executions,
continue-as-new, scheduled executions — belong in:

- the **static-architecture** edges from Manager → infrastructure ResourceAccess
  (so the encapsulation is documented once, with the full primitive list,
  in business verbs over the infrastructure),
- `operational-concepts.md` (where, why, and with what retention/replay
  semantics — including the infrastructure-specific names of those primitives,
  e.g., Temporal `Activity` / `Signal` / `Schedule`, Akka actor messages,
  etc.),
- per-Manager sequence diagrams when timing or signal ordering is what
  the diagram is for (sequence diagrams are the right place to show a
  `suspend → external event → resume` rhythm).

**What a dynamic view shows instead.** The business-logical edges only:

- `Client → Manager` — the use case starts/resumes; the edge label is the
  manager method name.
- `Manager → Engine` — the engine method signature.
- `Manager → ResourceAccess` (other than the infrastructure access) — the
  atomic business verb that does the I/O.
- `ResourceAccess → Resource` — the actual I/O.

**Cross-Manager signals.** When a Manager delivers a signal to another
Manager, the dynamic view shows a **queued Manager→Manager edge**
directly (per the closed-layer queued-sideways rule, ch. 3). The label
names the business signal — e.g., `delivers applyDelinquencyPolicy
(queued)`. The infrastructure-level delivery mechanism stays in the static
view and `operational-concepts.md`.

**Suspend-points.** A `Phase A: workflow suspends → Phase B: client
signals it` interaction is conveyed by ordering alone in the dynamic
view: the Client's resume-call edge follows the Manager's last pre-suspend
verb (typically `appendEvent(<Something>AwaitingReview)`). The reader
infers the suspension from the event name and the resume from the next
client method. The infrastructure's await-signal edge is not drawn.

**Validation impact.** The standard check verifies that no dynamic view
contains an edge whose target is a infrastructure ResourceAccess (any
component tagged `infrastructure-access`, or a ResourceAccess whose role is
encapsulating WorkflowRuntime volatility). Static-architecture edges to
the same component are required and unaffected.
