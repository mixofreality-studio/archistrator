---
name: the-method
description: Entrypoint for Juval Löwy's "The Method" from Righting Software. Indexes all phase-level and cross-cutting sub-skills. Use to navigate to the right skill for the current phase; doctrine lives in [[the-method-doctrine]] and layer rules live in [[the-method-layers]].
---

# The Method

Juval Löwy's "The Method" from *Righting Software* (2019). Book in repo at
`../../../rightingsoftware/`.

This skill is the entrypoint. Doctrine and layer rules are cross-cutting
skills. Each phase of the method has one or more sub-skills below.

## Cross-cutting reference

- [[the-method-doctrine]] — Prime Directive + the 9 Directives
- [[the-method-layers]] — layer model, interaction rules, cardinality
- [[the-method-testing]] — testing & quality doctrine (test types, roles, timing; no BDD)

## Phase 1: System Design

Drives `/system-design`. Produces a validated, layered, volatility-based
architecture as Structurizr DSL plus supporting markdown.

| Skill | Produces |
|---|---|
| [[the-method-business-alignment]] | mission.md |
| [[the-method-requirements-analysis]] | glossary.md + scrubbed-requirements.md |
| [[the-method-volatility-identification]] | volatilities.md |
| [[the-method-core-use-cases]] | core-use-cases.md |
| [[the-method-architecture]] | architecture.dsl |
| [[the-method-operational-concepts]] | operational-concepts.md |
| [[the-method-system-design-standard-check]] | standard-checklist.md |

## Phase 2: Project Design

Drives `/project-design`. Produces ≥3 options for management (normal,
decompressed-normal, compressed; subcritical is shown to be rejected) so they
can make an educated decision per Directive 7.

| Skill | Produces |
|---|---|
| [[the-method-planning-assumptions]] | planning-assumptions.md |
| [[the-method-activity-list]] | activities.md |
| [[the-method-network-draft]] | network.yaml |
| [[the-method-normal-solution]] | normal.md |
| [[the-method-subcritical-solution]] | subcritical.md |
| [[the-method-compressed-solution]] | compressed.md |
| [[the-method-decompressed-solution]] | decompressed.md |
| [[the-method-risk-modeling]] | risk.md |
| [[the-method-sdp-review]] | sdp-review.md |
| [[the-method-project-design-standard-check]] | project-standard-checklist.md |

## Phase 3: Construction

Drives `/implement-project`. Orchestrates the hand-off, per-component contract
design, weekly tracking, and event-triggered scope changes.

| Skill | Cadence |
|---|---|
| [[the-method-handoff]] | once at Phase 3 start |
| [[the-method-service-contract]] | per component, during detailed-design activity |
| [[the-method-review-routing]] | per produced change during construction |
| [[the-method-project-tracking]] | weekly |
| [[the-method-scope-change]] | event-triggered (scope shift or significant variance) |

## Sequencing

Sequencing across phases is driven by:

1. **Commands** — `/system-design`, `/project-design`, `/implement-project`, `/sdp-review` invoke the sub-skills in canonical order.
2. **Data dependencies** — each skill's input is a previous skill's output. The agent cannot run them out of order without producing the required input file first.

This skill (root) is intentionally light — no doctrine, no procedure. Use the sub-skill that fits the current step.
