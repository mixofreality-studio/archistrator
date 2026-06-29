---
name: the-method-review-routing
description: Construction-time review routing — the hand-run reviewEngine. Given a produced change (component code, UI-design concept, test plan, or UI code), computes and dispatches the reviewer set from the committed .systemDesign architecture + the .serviceContracts entries + .phaseArtifacts UI designs, gates on the verdicts, and applies mayAmend updates. Encapsulates ReviewPolicy. Invoke per work-product completion during Phase 3 construction.
model: inherit
skills: the-method
---

# Review Routing

The hand-run twin of the aiarch `reviewEngine`. When a construction activity produces a work product, this skill computes *who must review it, from what perspective, against which reference artifact* — and dispatches those reviews as a gate before the activity exits.

Encapsulates the **ReviewPolicy** volatility (see the committed `.volatilities` artifact). Routing is **dynamic**: reviewer sets are computed here from the architecture and the artifact kind, never stamped onto the `.activityList` slot.

## Canonical source

- Löwy, [Ch. 14 §5 "The Hand-Off"](../../../research/rightingsoftware/OEBPS/xhtml/ch14.xhtml#ch14lev1sec5) — the review chain this extends.
- [App C §3.4](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml) — integrate incrementally; review is part of the activity exit.

## Relationship to other skills

- `[[the-method-handoff]]` casts **who fills each role** (human vs AI, per the team/customer). This skill computes **which roles must review** a given change. Handoff = actors; review-routing = graph.
- `[[the-method-service-contract]]` produces the per-component **service contract** (`.serviceContracts[component]`) this skill reads to find each component's owner and contract. (Historically called the `D###` detailed-design; it is now the typed contract entry, not a `designs/*.md` file.)

## Input

State is git-as-DB: everything below is read from `.aiarch/state/project.json`.

- The produced change: `{ componentId, artifactKind, ref }` where `artifactKind ∈ { code, ui-design, test-plan, ui-code }`. For `code`, `ref` is the construction PR / commit; for the rest, the relevant `.phaseArtifacts` / `.testingState` entry.
- The committed **architecture** artifact (`.systemDesign`) — for inbound/outbound neighbours of `componentId`.
- The per-component **service contracts** (`.serviceContracts`) — to map component → owning author + contract.
- The committed **UI designs** (`.phaseArtifacts.uiDesign`) — for `ui-code` conformance reviews.

## Procedure

### Step 1 — Classify the change and compute the reviewer set

| `artifactKind` | Reviewers | `referenceArtifact` | `mayAmend` |
|---|---|---|---|
| `code` | (a) the contract author of **each inbound/outbound neighbour** of `componentId` (from `.systemDesign` relationships), reviewing from that neighbour's **contract** perspective; **and** (b) `componentId`'s **own** contract author, reviewing **code ↔ contract alignment** | (a) each neighbour's `.serviceContracts` entry; (b) `componentId`'s `.serviceContracts` entry | (b) only — may update `componentId`'s contract entry |
| `ui-design` | founder/architect-user (approval) + `ux-reviewer` + `product-manager` + `system-architect` | the UI-design brief / Method UI conventions | no |
| `test-plan` | `system-architect` + `product-manager` + `qa-engineer` | the core use cases (`.coreUseCases`) + the System Test Plan (`.testingState.systemTestPlan`, `N-STP`) | no |
| `ui-code` | `ui-designer` / `ux-reviewer` | the approved UI design (`.phaseArtifacts.uiDesign`) | yes — may update the UI design |

To find neighbours of `componentId`: read the `.systemDesign` relationships and collect every relationship where `componentId` is the source (outbound) or destination (inbound), excluding Utilities (logging/diagnostics/security) and Resource edges. The other endpoint is a neighbour whose owner must review.

To find a component's contract author/owner: the contract for that component was produced by the `senior-developer` role (per `[[the-method-handoff]]`) and recorded in `.serviceContracts[component]`. Re-dispatch that role, primed with the component's contract entry.

### Step 2 — Dispatch the reviews

For each `{ reviewer, perspective, referenceArtifact, mayAmend }` in the set, dispatch the reviewer agent with: the change, the `referenceArtifact`, and the `perspective` instruction (e.g. "review this change to `operationsManager` from the perspective of `operatedRuntimeAccess`'s contract — will the integration hold?").

Run independent reviews in parallel.

### Step 3 — Collect verdicts and gate

Each review returns `pass | fail(reason) | amend(target, proposedChange)`.

- All `pass` → the change clears its review gate; the activity may exit.
- Any `fail` → return the change to the constructor with the reasons. If it cannot be resolved, escalate per `[[the-method-scope-change]]` / intervention (a failed verdict is an intervention trigger — it does not silently pass).
- Any `amend` (only valid where `mayAmend = yes`) → if the reviewer **and** the constructor agree, update and re-version the `referenceArtifact` (the component's `.serviceContracts` entry or the `.phaseArtifacts.uiDesign` entry), then re-run the affected review. Record the amendment.

### Step 4 — Record

Record the review outcome against the activity in `.activityConstruction[activityId]` (the hand-run analogue of the `RecordChangeReviewed` verb) — not in a `designs/*.md` log. The activity is not "done" until its review gate is clear.

## Exit criteria (for router)

- A reviewer set was computed for the change from `.systemDesign` + `.serviceContracts` + artifact kind (not from any static list on `.activityList`).
- Every reviewer in the set was dispatched and returned a verdict.
- All verdicts are `pass`, or every `amend` was applied-and-agreed and every `fail` resolved.
- Any `mayAmend` update re-versioned the `.serviceContracts` entry / `.phaseArtifacts.uiDesign` entry.
- The outcome was recorded against `.activityConstruction[activityId]` (`RecordChangeReviewed`).

## Anti-patterns to reject

- **Reviewer lists stamped on activities** — routing is dynamic; the set is computed at change time.
- **Skipping the own-contract alignment review** — that is how code and its contract drift apart.
- **Silently passing a `fail`** — a failed verdict that cannot resolve escalates; it never auto-clears.
- **Amending a contract/UI-design without the constructor's agreement** — `mayAmend` requires reviewer + constructor consensus.
- **Folding review into intervention** — review runs on every change (happy path); intervention only on failure. See the `.operationalConcepts` review-routing ADR.
