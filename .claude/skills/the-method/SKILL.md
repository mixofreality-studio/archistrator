---
name: the-method
description: Entrypoint for Juval Löwy's "The Method" from Righting Software. Indexes all phase-level and cross-cutting sub-skills. Use to navigate to the right skill for the current phase; doctrine lives in [[the-method-doctrine]] and layer rules live in [[the-method-layers]].
---

# The Method

Juval Löwy's "The Method" from *Righting Software* (2019). Book in repo at
`../../../research/rightingsoftware/`.

This skill is the entrypoint. Doctrine and layer rules are cross-cutting
skills. Each phase of the method has one or more sub-skills below.

**archistrator runs as a single Go server repo. State is git-as-DB:** all
Method artifacts are typed slots/maps in `.aiarch/state/project.json` (a typed
JSON aggregate), NOT `methodpoc/designs/<product>/*.md` files. The markdown/DSL
shapes named in the "Produces" columns below are **render-on-read** views of
that typed state — the JSON is the source of truth. The `project.json` slot for
each artifact is named in the tables.

## Cross-cutting reference

- [[the-method-doctrine]] — Prime Directive + the 9 Directives
- [[the-method-layers]] — layer model, interaction rules, cardinality
- [[the-method-testing]] — testing & quality doctrine (test types, roles, timing; no BDD)

## Phase 1: System Design

Drives `/system-design`. Produces a validated, layered, volatility-based
architecture committed to typed slots in `project.json` (rendered on read as
Structurizr DSL + markdown).

| Skill | Produces (render) | `project.json` slot |
|---|---|---|
| [[the-method-business-alignment]] | mission.md | `.mission` |
| [[the-method-requirements-analysis]] | glossary.md + scrubbed-requirements.md | `.glossary` + `.scrubbedRequirements` |
| [[the-method-volatility-identification]] | volatilities.md | `.volatilities` |
| [[the-method-core-use-cases]] | core-use-cases.md | `.coreUseCases` |
| [[the-method-architecture]] | architecture.dsl | `.systemDesign` |
| [[the-method-operational-concepts]] | operational-concepts.md | `.operationalConcepts` |
| [[the-method-system-design-standard-check]] | standard-checklist.md | `.standardCheck` |

## Phase 2: Project Design

Drives `/project-design`. Produces ≥3 options for management (normal,
decompressed-normal, compressed; subcritical is shown to be rejected) so they
can make an educated decision per Directive 7.

| Skill | Produces (render) | `project.json` slot |
|---|---|---|
| [[the-method-planning-assumptions]] | planning-assumptions.md | `.planningAssumptions` |
| [[the-method-activity-list]] | activities.md | `.activityList` |
| [[the-method-network-draft]] | network.yaml (render) | `.network` |
| [[the-method-normal-solution]] | normal.md | `.normalSolution` |
| [[the-method-subcritical-solution]] | subcritical.md | `.subcriticalSolution` |
| [[the-method-compressed-solution]] | compressed.md | `.compressedSolution` |
| [[the-method-decompressed-solution]] | decompressed.md | `.decompressedSolution` |
| [[the-method-risk-modeling]] | risk.md | `.riskModel` |
| [[the-method-sdp-review]] | sdp-review.md | `.sdpReview` |
| [[the-method-project-design-standard-check]] | project-standard-checklist.md | `.standardCheck` |

The `.network` slot **replaces** the old `network.yaml` file — there is no
YAML file on disk; the network is a typed slot rendered as YAML on read.

## Phase 3: Construction

Drives `/implement-project`. Orchestrates the hand-off, per-component contract
design, weekly tracking, and event-triggered scope changes.

| Skill | Cadence | `project.json` target |
|---|---|---|
| [[the-method-handoff]] | once at Phase 3 start | `.handoff` |
| [[the-method-service-contract]] | per component, during detailed-design activity | `.serviceContracts[component]` |
| [[the-method-review-routing]] | per produced change during construction | `.activityConstruction[activityId]` (review verdicts) |
| [[the-method-project-tracking]] | weekly | `.activityConstruction` + tracking projections |
| [[the-method-scope-change]] | event-triggered (scope shift or significant variance) | re-runs Phase 2 → `.sdpReview` + `.network` |

## Sequencing

Sequencing across phases is driven by:

1. **Commands** — `/system-design`, `/project-design`, `/implement-project`, `/sdp-review` invoke the sub-skills in canonical order.
2. **Data dependencies** — each skill's input is a previous skill's output. Inputs and outputs are typed slots in `project.json`; a skill cannot run until the slot it reads has been committed by the preceding skill.

This skill (root) is intentionally light — no doctrine, no procedure. Use the sub-skill that fits the current step.
