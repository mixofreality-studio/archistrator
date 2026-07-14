---
name: the-method-system-design-standard-check
description: System Design — final quality gate. Walk the Appendix C Design Standard checklist against the completed design. Each item passes, is waived with explicit justification, or sends you back to fix. Reads all Phase 1 artifact slots from project.json (.mission/.glossary/.scrubbedRequirements/.volatilities/.coreUseCases/.systemDesign/.operationalConcepts). Produces the typed StandardCheck committed to project.json → .standardCheck. Invoke as the last step of system design, before /project-design.
---

# Design Standard Check

The final gate before project design begins. Every item in Appendix C's System Design Guidelines is verified against the actual artifacts. Failures must be fixed or explicitly waived with a written justification — not silently passed.

## Canonical source

**Primary:** Löwy, Appendix C — Design Standard. Focus areas:

- §1 "The Prime Directive"
- §2 "Directives"
- §3 "System Design Guidelines"
- §6 "Service Contract Design Guidelines" (forward-look — full check during construction)

## Input

State is git-as-DB: archistrator is a single Go-server repo whose canonical project state lives in `.aiarch/state/project.json` (a typed JSON aggregate). Markdown/DSL is a render-on-read of the typed state. The complete Phase 1 committed artifact set:

- `.mission`
- `.glossary`
- `.scrubbedRequirements`
- `.volatilities`
- `.coreUseCases`
- `.systemDesign` (the typed `System`; read its rendered `architecture.dsl` where the checks below grep DSL)
- `.operationalConcepts`

## Output

The typed **`StandardCheck`** model (system-design variant), committed to **`.aiarch/state/project.json` → `.standardCheck`**. NOT a `standard-checklist.md` file — any markdown below is a render-on-read of this slot. Per the two usage patterns (agentic/CI dispatch and local interactive), the agent emits the typed model and commits it into `.standardCheck`; the server stages it (`StageArtifactForReview`) for the human review gate.

## Procedure

Walk each Appendix C item. For each, record: **PASS**, **WAIVED** (with justification), or **FAIL** (with required fix).

### Section A — The Prime Directive

| Item | Verification | Status |
|---|---|---|
| Never design against the requirements | The architecture is volatility-based per the committed `.volatilities`, not feature- or domain-based. Verify by inspecting the `Components` in `.systemDesign` (rendered as `container` declarations in `architecture.dsl`): no component is named after a use case or domain. | |

### Section B — Directives (the 9)

| # | Directive | How to verify | Status |
|---|---|---|---|
| 1 | Avoid functional decomposition | `.systemDesign` `Components` (rendered as `architecture.dsl` containers) have no names taken from features | |
| 2 | Decompose based on volatility | Every `Component` in `.systemDesign` cites a `.volatilities` entry in its `Encapsulates`, and the rendered description is ≤150 chars (no retention / schema / mechanics — that goes elsewhere). See "Description style" in `STRUCTURIZR-CONVENTIONS.md`. | |
| 3 | Provide a composable design | All non-core use cases drawn in `.systemDesign` dynamic views trace cleanly through existing components | |
| 4 | Features as integration, not implementation | Confirm no Manager is named after a feature (`Reporting`, `Notifications`) | |
| 5 | Design iteratively, build incrementally | (Forward-look — applies in /implement-project) | N/A here |
| 6 | Design the project to build the system | (Forward-look — Phase 2) | N/A here |
| 7 | Educated decisions with options | (Forward-look — Phase 2) | N/A here |
| 8 | Build along critical path | (Forward-look — Phase 3) | N/A here |
| 9 | Be on time throughout | (Forward-look — Phase 3) | N/A here |

### Section C — Requirements (App C §3.1)

| # | Guideline | Verification | Status |
|---|---|---|---|
| 1a | Capture required behavior, not functionality | Inspect `.coreUseCases` — each describes behavior + outcome, not features | |
| 1b | Describe required behavior with use cases | `.coreUseCases` is committed with all core entries | |
| 1c | Document use cases with nested conditions via activity diagrams | Every use case with branches in `.coreUseCases` carries PlantUML activity-diagram source (renders as a ```puml ... @startuml ... @enduml block) | |
| 1d | Eliminate solutions masquerading as requirements | `.scrubbedRequirements` is committed and shows before/after for every research item | |
| 1e | Validate by supporting all core use cases | Every core use case has a `DynamicView` in `.systemDesign` | |

### Section D — Cardinality (App C §3.2)

| # | Guideline | Verification | Status |
|---|---|---|---|
| 2a | ≤5 Managers without subsystems | Count `Manager`-kind components in `.systemDesign` (rendered as `manager`-tagged containers) | |
| 2b | Few subsystems (≤handful) | Count subsystems in `.operationalConcepts` | |
| 2c | ≤3 Managers per subsystem | Per-subsystem count | |
| 2d | Golden Engines-to-Managers ratio | Confirm more Engines than Managers (or at least 2:1 favoring Engines) | |
| 2e | ResourceAccess may serve multiple Resources | Inspect `.systemDesign` `Relationships` — note any `ResourceAccess` component with edges to multiple `Resource` components | |

### Section E — Attributes (App C §3.3)

| # | Guideline | Verification | Status |
|---|---|---|---|
| 3a | Volatility decreases top-down | Clients most volatile → Resources least. Spot-check `.volatilities` against the `Component.Encapsulates` values in `.systemDesign`. | |
| 3b | Reuse increases top-down | Utilities most reusable (cappuccino test passes). | |
| 3c | Do not encapsulate changes to nature of business | Walk `.volatilities` — flag any "nature of business" entries that snuck in | |
| 3d | Managers should be almost expendable | For each Manager, ask: if this Manager were replaced, are Engines/RA/Resources/Utilities still useful? | |
| 3e | Symmetric design | Similar Managers handle pub/sub similarly; similar Engines exposed similarly | |
| 3f | No public channels for internal interactions | Inspect `.operationalConcepts` — Message Bus is internal; no direct internet routes between Managers | |

### Section F — Layers (App C §3.4)

| # | Guideline | Verification | Status |
|---|---|---|---|
| 4a | Avoid open architecture | `.operationalConcepts` declares closed (or justifies otherwise) | |
| 4b | Avoid semi-closed/semi-open | Same | |
| 4c | Prefer closed | Same | |
| 4c.i | Do not call up | Walk every `Relationship` in `.systemDesign` — flag any low→high | |
| 4c.ii | No sideways except queued M↔M / M→E | Same | |
| 4c.iii | No skipping layers | Same | |
| 4c.iv | Resolve open attempts via queued or async | Verify `.operationalConcepts` documents queueing for cross-Manager flows | |
| 4d | Extend the system by implementing subsystems | Forward-look | N/A here |

### Section G — Interaction rules (App C §3.5)

| # | Guideline | Verification | Status |
|---|---|---|---|
| 5a | All components can call Utilities | (Permitted; no violation possible) | PASS |
| 5b | Managers and Engines can call ResourceAccess | Inspect dynamic views | |
| 5c | Managers can call Engines | Inspect dynamic views | |
| 5d | Managers can queue calls to another Manager | Inspect `.operationalConcepts` Sync/Queued Map | |

### Section H — Interaction don'ts (App C §3.6)

| # | Don't | Verification | Status |
|---|---|---|---|
| 6a | Clients do not call multiple Managers in same use case | Every `DynamicView` in `.systemDesign` enters exactly one Manager from a Client | |
| 6b | Managers do not queue to >1 Manager in same use case | Inspect each Manager's dynamic-view participation; on Temporal, count `SignalExternalWorkflow(...)` calls per use case | |
| 6c | Engines do not receive queued calls | Verify all incoming Engine edges are sync; on Temporal, verify no `Activity:` prefix on Manager → Engine edges (engines are deterministic in-workflow calls, not Activities) | |
| 6d | ResourceAccess does not receive queued calls | Same for RA | |
| 6e | Clients do not publish events | Verify no Client appears as publisher in `.operationalConcepts` Events table | |
| 6f | Engines do not publish events | Same for Engines | |
| 6g | ResourceAccess does not publish events | Same for RA | |
| 6h | Resources do not publish events | Same for Resources | |
| 6i | Engines/RA/Resources do not subscribe | Verify all subscribers are Clients or Managers | |

### Section I — Temporal vocabulary (when Managers run on Temporal)

Apply this section ONLY when `.operationalConcepts` §1 declares Temporal as the Manager infrastructure. Otherwise mark every row N/A. The grep checks below run against the **rendered** `architecture.dsl` (the render-on-read of `.systemDesign`).

| # | Guideline | Verification | Status |
|---|---|---|---|
| 7a | Every Client → Manager edge label uses a Temporal primitive | Grep `architecture.dsl` relationships block: every Client → Manager edge starts with `StartWorkflow(`, `SignalWorkflow(`, `QueryWorkflow(`, `UpdateWorkflow(`, or `Schedule[` | |
| 7b | Every Manager → ResourceAccess edge label starts with `Activity:` | Grep `architecture.dsl` | |
| 7c | Every Manager → Engine edge label is a deterministic call (no `Activity:` prefix) | Inspect the `.systemDesign` relationships (or rendered DSL); engines are deterministic in-workflow calls, no Activity wrapper | |
| 7d | Every Manager → `workflowExecutionAccess` edge label names a Temporal primitive (`Timer`, `Await Signal`, `SignalExternalWorkflow`, `ExecuteChildWorkflow`, `ContinueAsNew`, or `Schedule[...]`) | Grep `architecture.dsl` | |
| 7e | Workflow types end in `Workflow`; Signal types end in `Signal`; Activity types are imperative verbs (not past tense) | Inspect identifiers in `.systemDesign` dynamic views and (when present) in the committed `.serviceContracts` entries | |
| 7f | Sequence-diagram source carried on `.systemDesign` dynamic views uses Temporal vocabulary (no `MessageBus` participant; signal/timer/activity arrows named with Temporal primitives) | Inspect each rendered PlantUML sequence diagram (```puml ... @startuml ... @enduml block) | |
| 7g | `.operationalConcepts` Sync/Queued Map names a Temporal primitive per row | Inspect the table — every row should have an explicit primitive column or equivalent | |
| 7h | Determinism rules for workflow code documented in `.operationalConcepts` | Look for the list (no system clock, no random IDs, all I/O via Activities, versioning policy) | |
| 7i | External-system idempotency boundaries enumerated per Activity | Look for the per-Activity dedup-key table (Stripe Idempotency-Key, k8s manifest name, gateway event id, etc.) | |
| 7j | Workflow checkpoint store distinguished from business event log | `.operationalConcepts` §7 (or equivalent) names both stores per Manager and explains the separation of concerns | |

If any 7a–7f fails, fix the typed `System` in `.systemDesign` (and re-render) and re-run. 7g–7j failures usually mean `.operationalConcepts` needs to be filled out — return to [[the-method-operational-concepts]].

## Output format

Author the typed `StandardCheck` model (committed to `.standardCheck`) carrying **every** item from Sections A–H, each with a Status of PASS / WAIVED / FAIL. The markdown table below is the render-on-read view of that slot.

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

## Draft-job doctrine (CI dispatch)

This is the normative task the CI draft job (and a local `/system-design` run) executes to produce the system-design `StandardCheck`. It is self-contained: everything a draft agent needs to run a sound gate — including exactly which items are in scope — is stated here.

Walk the App C design standard, but ONLY the items checkable at THIS system-design gate — the design directives and the System Design guideline section. Check the design directives: avoid functional decomposition, decompose based on volatility, provide a composable design, treat features as aspects of integration (not as building blocks), design iteratively while building incrementally, and — where the design makes an architectural choice that had real alternatives — drive that decision with options. Then walk the System Design guideline section: capture behaviour not functionality, every component traces to a volatility (no functional or domain decomposition), cardinality limits respected (Managers a handful, fewer Engines than Managers), volatility decreases and reuse increases top-down, Managers do no I/O, closed-layer rules respected (no calling up, sideways, or skipping layers), and the interaction don'ts (one Manager per client call chain; no queued or pub/sub from the wrong layers). For each IN-SCOPE item emit pass (the design satisfies it), waived (with a concrete justification for why THIS system consciously accepts the exception — e.g. a cardinality guideline deliberately exceeded for a documented reason), or fail (the design violates it). A waiver without a real justification is itself a fail.

SCOPE — do NOT walk the project-design or project-tracking parts of the standard at this gate. The project directives (design the project to build the system, build along the critical path, be on time throughout), the Project Design guideline sections (general, staffing, integration, estimations, network, time-and-cost, risk), and the Project Tracking guideline section are OUT OF SCOPE at the system-design gate — no project design exists yet, so there is nothing to check them against. Do NOT emit them at all, and in particular do NOT emit them as waived: waived is reserved for genuine, justified exceptions to IN-SCOPE system-design items, NOT for phase-inapplicable items (marking an out-of-scope item "waived: no project design exists yet" pollutes the waiver as a conscious-exception signal). Those items are checked at their own Phase-2 SDP gate (the project-design standard check), so nothing is lost by leaving them out here.

## Exit criteria (for router)

- `.aiarch/state/project.json` → `.standardCheck` holds the typed `StandardCheck` model
- Zero FAIL entries (any FAIL sends you back to the relevant prior phase)
- Every WAIVED entry has a written justification
- Summary block at bottom

System design is complete. Next: `/project-design <product>`.

## When to waive vs fix

**Waive when:**
- The deviation is intentional and traces to a business objective from the committed `.mission` artifact
- The book itself acknowledges contexts where the rule may bend (e.g., open architecture for tiny systems — though rare)
- The team has accepted the trade-off explicitly

**Fix when:**
- The violation reveals a bad decomposition
- The violation breaks a Don't (Don'ts are rarely waivable)
- The violation has no business objective backing it

## Common findings on first pass

- **Don't 6e–h violations** — events publishing from wrong layer. Usually a Manager-naming error (Engine misclassified). Fix the component's `Kind`/`Name` in `.systemDesign` (the layer is derived from `Kind`).
- **Cardinality 2a exceeded** — too many Managers; introduce subsystems or merge.
- **Symmetry 3e violations** — uneven pub/sub patterns across Managers. Standardize.
- **Open architecture 4a** silently snuck in via direct API gateway access. Reroute through proper Managers.
