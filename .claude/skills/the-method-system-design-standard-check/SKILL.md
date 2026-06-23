---
name: the-method-system-design-standard-check
description: System Design — final quality gate. Walk the Appendix C Design Standard checklist against the completed design. Each item passes, is waived with explicit justification, or sends you back to fix. Reads all Phase 1 artifacts (mission/glossary/scrubbed-requirements/volatilities/core-use-cases/architecture.dsl/operational-concepts). Produces standard-checklist.md. Invoke as the last step of system design, before /project-design.
---

# Design Standard Check

The final gate before project design begins. Every item in Appendix C's System Design Guidelines is verified against the actual artifacts. Failures must be fixed or explicitly waived with a written justification — not silently passed.

## Canonical source

**Primary:** Löwy, [Appendix C — Design Standard](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml). Focus areas:

- [§1 "The Prime Directive"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec1)
- [§2 "Directives"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec2)
- [§3 "System Design Guidelines"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec3)
- [§6 "Service Contract Design Guidelines"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec6) (forward-look — full check during construction)

## Input

The complete Phase 1 artifact set:
- `methodpoc/designs/<product>/system/mission.md`
- `methodpoc/designs/<product>/system/glossary.md`
- `methodpoc/designs/<product>/system/scrubbed-requirements.md`
- `methodpoc/designs/<product>/system/volatilities.md`
- `methodpoc/designs/<product>/system/core-use-cases.md`
- `methodpoc/designs/<product>/system/architecture.dsl`
- `methodpoc/designs/<product>/system/operational-concepts.md`

## Output

`methodpoc/designs/<product>/system/standard-checklist.md`

## Procedure

Walk each Appendix C item. For each, record: **PASS**, **WAIVED** (with justification), or **FAIL** (with required fix).

### Section A — The Prime Directive

| Item | Verification | Status |
|---|---|---|
| Never design against the requirements | The architecture is volatility-based per `volatilities.md`, not feature- or domain-based. Verify by inspecting the `container` declarations in `architecture.dsl`: no component is named after a use case or domain. | |

### Section B — Directives (the 9)

| # | Directive | How to verify | Status |
|---|---|---|---|
| 1 | Avoid functional decomposition | `architecture.dsl` containers have no names taken from features | |
| 2 | Decompose based on volatility | Every container in `architecture.dsl` cites a `volatilities.md` entry in its description, and the description is ≤150 chars (no retention / schema / mechanics — that goes elsewhere). See "Description style" in `STRUCTURIZR-CONVENTIONS.md`. | |
| 3 | Provide a composable design | All non-core use cases drawn in architecture.dsl trace cleanly through existing components | |
| 4 | Features as integration, not implementation | Confirm no Manager is named after a feature (`Reporting`, `Notifications`) | |
| 5 | Design iteratively, build incrementally | (Forward-look — applies in /implement-project) | N/A here |
| 6 | Design the project to build the system | (Forward-look — Phase 2) | N/A here |
| 7 | Educated decisions with options | (Forward-look — Phase 2) | N/A here |
| 8 | Build along critical path | (Forward-look — Phase 3) | N/A here |
| 9 | Be on time throughout | (Forward-look — Phase 3) | N/A here |

### Section C — Requirements (App C §3.1)

| # | Guideline | Verification | Status |
|---|---|---|---|
| 1a | Capture required behavior, not functionality | Inspect `core-use-cases.md` — each describes behavior + outcome, not features | |
| 1b | Describe required behavior with use cases | `core-use-cases.md` exists with all core entries | |
| 1c | Document use cases with nested conditions via activity diagrams | Every use case with branches in `core-use-cases.md` has a PlantUML activity diagram (```puml ... @startuml ... @enduml block) | |
| 1d | Eliminate solutions masquerading as requirements | `scrubbed-requirements.md` exists and shows before/after for every research item | |
| 1e | Validate by supporting all core use cases | Every core use case has a dynamic view in `architecture.dsl` | |

### Section D — Cardinality (App C §3.2)

| # | Guideline | Verification | Status |
|---|---|---|---|
| 2a | ≤5 Managers without subsystems | Count containers tagged `manager` in `architecture.dsl` | |
| 2b | Few subsystems (≤handful) | Count subsystems in `operational-concepts.md` | |
| 2c | ≤3 Managers per subsystem | Per-subsystem count | |
| 2d | Golden Engines-to-Managers ratio | Confirm more Engines than Managers (or at least 2:1 favoring Engines) | |
| 2e | ResourceAccess may serve multiple Resources | Inspect `architecture.dsl` relationships — note any `resource-access` container with edges to multiple `resource` containers | |

### Section E — Attributes (App C §3.3)

| # | Guideline | Verification | Status |
|---|---|---|---|
| 3a | Volatility decreases top-down | Clients most volatile → Resources least. Spot-check `volatilities.md` against the container descriptions in `architecture.dsl`. | |
| 3b | Reuse increases top-down | Utilities most reusable (cappuccino test passes). | |
| 3c | Do not encapsulate changes to nature of business | Walk `volatilities.md` — flag any "nature of business" entries that snuck in | |
| 3d | Managers should be almost expendable | For each Manager, ask: if this Manager were replaced, are Engines/RA/Resources/Utilities still useful? | |
| 3e | Symmetric design | Similar Managers handle pub/sub similarly; similar Engines exposed similarly | |
| 3f | No public channels for internal interactions | Inspect `operational-concepts.md` — Message Bus is internal; no direct internet routes between Managers | |

### Section F — Layers (App C §3.4)

| # | Guideline | Verification | Status |
|---|---|---|---|
| 4a | Avoid open architecture | `operational-concepts.md` declares closed (or justifies otherwise) | |
| 4b | Avoid semi-closed/semi-open | Same | |
| 4c | Prefer closed | Same | |
| 4c.i | Do not call up | Walk every relationship in `architecture.dsl` — flag any low→high | |
| 4c.ii | No sideways except queued M↔M / M→E | Same | |
| 4c.iii | No skipping layers | Same | |
| 4c.iv | Resolve open attempts via queued or async | Verify `operational-concepts.md` documents queueing for cross-Manager flows | |
| 4d | Extend the system by implementing subsystems | Forward-look | N/A here |

### Section G — Interaction rules (App C §3.5)

| # | Guideline | Verification | Status |
|---|---|---|---|
| 5a | All components can call Utilities | (Permitted; no violation possible) | PASS |
| 5b | Managers and Engines can call ResourceAccess | Inspect dynamic views | |
| 5c | Managers can call Engines | Inspect dynamic views | |
| 5d | Managers can queue calls to another Manager | Inspect `operational-concepts.md` Sync/Queued Map | |

### Section H — Interaction don'ts (App C §3.6)

| # | Don't | Verification | Status |
|---|---|---|---|
| 6a | Clients do not call multiple Managers in same use case | Every dynamic view in `architecture.dsl` enters exactly one Manager from a Client | |
| 6b | Managers do not queue to >1 Manager in same use case | Inspect each Manager's dynamic-view participation; on Temporal, count `SignalExternalWorkflow(...)` calls per use case | |
| 6c | Engines do not receive queued calls | Verify all incoming Engine edges are sync; on Temporal, verify no `Activity:` prefix on Manager → Engine edges (engines are deterministic in-workflow calls, not Activities) | |
| 6d | ResourceAccess does not receive queued calls | Same for RA | |
| 6e | Clients do not publish events | Verify no Client appears as publisher in `operational-concepts.md` Events table | |
| 6f | Engines do not publish events | Same for Engines | |
| 6g | ResourceAccess does not publish events | Same for RA | |
| 6h | Resources do not publish events | Same for Resources | |
| 6i | Engines/RA/Resources do not subscribe | Verify all subscribers are Clients or Managers | |

### Section I — Temporal vocabulary (when Managers run on Temporal)

Apply this section ONLY when `operational-concepts.md` §1 declares Temporal as the Manager infrastructure. Otherwise mark every row N/A.

| # | Guideline | Verification | Status |
|---|---|---|---|
| 7a | Every Client → Manager edge label uses a Temporal primitive | Grep `architecture.dsl` relationships block: every Client → Manager edge starts with `StartWorkflow(`, `SignalWorkflow(`, `QueryWorkflow(`, `UpdateWorkflow(`, or `Schedule[` | |
| 7b | Every Manager → ResourceAccess edge label starts with `Activity:` | Grep `architecture.dsl` | |
| 7c | Every Manager → Engine edge label is a deterministic call (no `Activity:` prefix) | Grep `architecture.dsl`; engines are deterministic in-workflow calls per `the-method-architecture/TEMPORAL-VOCABULARY.md` | |
| 7d | Every Manager → `workflowExecutionAccess` edge label names a Temporal primitive (`Timer`, `Await Signal`, `SignalExternalWorkflow`, `ExecuteChildWorkflow`, `ContinueAsNew`, or `Schedule[...]`) | Grep `architecture.dsl` | |
| 7e | Workflow types end in `Workflow`; Signal types end in `Signal`; Activity types are imperative verbs (not past tense) | Inspect identifiers in dynamic views and (when present) in the contract files under `implementation/contracts/` | |
| 7f | Sequence diagrams under `system/sequence-diagrams/` use Temporal vocabulary (no `MessageBus` participant; signal/timer/activity arrows named per `TEMPORAL-VOCABULARY.md`) | Inspect each PlantUML sequence diagram (```puml ... @startuml ... @enduml block) | |
| 7g | `operational-concepts.md` Sync/Queued Map names a Temporal primitive per row | Inspect the table — every row should have an explicit primitive column or equivalent | |
| 7h | Determinism rules for workflow code documented in `operational-concepts.md` | Look for the list (no system clock, no random IDs, all I/O via Activities, versioning policy) | |
| 7i | External-system idempotency boundaries enumerated per Activity | Look for the per-Activity dedup-key table (Stripe Idempotency-Key, k8s manifest name, gateway event id, etc.) | |
| 7j | Workflow checkpoint store distinguished from business event log | `operational-concepts.md` §7 (or equivalent) names both stores per Manager and explains the separation of concerns | |

If any 7a–7f fails, fix the DSL or sequence diagram and re-run. 7g–7j failures usually mean `operational-concepts.md` needs to be filled out — return to [[the-method-operational-concepts]].

## Output format

Write `standard-checklist.md` as a single table with **every** item from Sections A–H, with a Status column showing PASS / WAIVED / FAIL.

For WAIVED items, include a fourth column "Justification" with a sentence explaining why this design intentionally deviates.

For FAIL items, do not waive — return to the prior phase, fix, and re-run this skill.

```markdown
# System Design Standard Checklist — <Product>

Date: <YYYY-MM-DD>
Reviewer: <agent or user>

| Section | Item | Status | Justification (if waived) | Fix needed (if failed) |
|---|---|---|---|---|
| Prime Directive | Never design against requirements | PASS | | |
| Directive 1 | Avoid functional decomposition | PASS | | |
| Directive 2 | Decompose by volatility | PASS | | |
...
| Don't 6a | Clients call one Manager per use case | PASS | | |
...

## Summary

- Total items checked: 38
- PASS: 36
- WAIVED: 2
- FAIL: 0

Phase 1 design is complete.
```

## Exit criteria (for router)

- `standard-checklist.md` exists
- Zero FAIL entries (any FAIL sends you back to the relevant prior phase)
- Every WAIVED entry has a written justification
- Summary block at bottom

System design is complete. Next: `/project-design <product>`.

## When to waive vs fix

**Waive when:**
- The deviation is intentional and traces to a business objective from `mission.md`
- The book itself acknowledges contexts where the rule may bend (e.g., open architecture for tiny systems — though rare)
- The team has accepted the trade-off explicitly

**Fix when:**
- The violation reveals a bad decomposition
- The violation breaks a Don't (Don'ts are rarely waivable)
- The violation has no business objective backing it

## Common findings on first pass

- **Don't 6e–h violations** — events publishing from wrong layer. Usually a Manager-naming error (Engine misclassified). Fix the container's layer tag and name in `architecture.dsl`.
- **Cardinality 2a exceeded** — too many Managers; introduce subsystems or merge.
- **Symmetry 3e violations** — uneven pub/sub patterns across Managers. Standardize.
- **Open architecture 4a** silently snuck in via direct API gateway access. Reroute through proper Managers.
