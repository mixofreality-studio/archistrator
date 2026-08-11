# Compression Moves & Predicate Attachment — Stage 2 design

**Date:** 2026-08-10 · **Builds on:** `docs/superpowers/specs/2026-08-09-derived-activity-list-design.md` (Stage 1, shipped @`b7abd9b`; read its post-execution amendment block first) · **Founder ask:** replace the compressed solution's `criticalSpeedup` fudge factor with Löwy's actual compression — network mutation — and close the two vocabulary gaps Stage 1 left behind. Governing principle unchanged: **do anything deterministic with code, and only non-deterministic things with agents.**

## Current state (verified 2026-08-10)

- Slot 9 holds the **40** derived activities; slot 10 the derived network (19 dependency rows, milestones M0–M3, an 11-activity critical path). `make derived-plan-check` re-derives and fails on drift.
- **Project duration is 165 days.** The previously committed 115 was wrong — see the Stage 1 amendment, item 6.
- The four option slots (11–14) still differ by exactly three scalars: `staffingCap` (6/4/6/6), `criticalSpeedup` (1.0/1.0/**1.8**/1.0), `bufferDays` (0/0/0/20). All four, plus the risk model (15) and SDP review (16), are flagged `staleBasis` against network revision 3 and **were computed against the superseded 115-day network**. Every duration, cost and risk figure in them is therefore wrong.
- Before that staleness, the risk model excluded the compressed option at composite **0.797** against a 0.75 ceiling, and recommended `decompressedSolution` — the *slowest* plan. Compression modelled purely as float compression can only raise risk, so the current model is structurally incapable of producing a viable compressed option.
- `activityListOverrides` holds 25 overrides, **0 additives, 0 additive milestones**. The additive path has never run against real data.

## The problem in one sentence

Löwy compresses by **mutating the network** — inserting simulators to break dependency edges, splitting contract design out to unblock dependents — and the shipped vocabulary can express neither, because it forbids exactly the edge surgery compression requires.

## Design

### Two layers, deliberately separate

The Stage 1 rule *"no exclusions, no derived-edge overrides"* is **correct for the plan** and **wrong for an option**. That is not a contradiction; they are different artifacts:

| | The plan (slot 9/10) | An option (slots 11–14) |
|---|---|---|
| Asserts | what is true of the project | what *would* be true under a staffing/sequencing choice |
| Derived edges | immutable — if an edge is wrong, the *relationship* is wrong, so fix slot 5 | mutable by design — deliberately breaking a real dependency is the technique |
| Authored input | overrides + additives | moves |

Conflating them is what made `criticalSpeedup` possible: a scalar that changes the schedule while asserting nothing about how.

### Layer 1 — predicate attachment (closes C3 and C4)

Stage 1's amendment records that conditions C3 and C4 are unimplementable: `AdditiveActivity.DependsOn` declares only the additive's *own* predecessors, so there is no channel by which an authored entry **gates derived work**. Real examples this blocks today: `N-CI` must precede every coding activity (you cannot build a component before CI exists); `N-SCHEMA` must precede every store-backed ResourceAccess.

`AdditiveActivity` and `AdditiveMilestone` gain a `gates` selector — a **closed** predicate language:

```jsonc
{ "coding": true }                                    // every coding activity
{ "prefix": "U-SPA-" }                                // every SPA construction activity
{ "componentKind": "resourceAccess" }                 // by component kind
{ "componentKind": "resource", "provisioning": "owned" }  // store-backed only
```

Semantics: after derivation and after additives are appended, each `gates` selector expands to edges **additive → each matching derived activity**. Expansion happens before the drift gate hashes the result, so a stale attachment is caught by re-derivation.

**Why predicate and not enumeration.** An enumerated list of 22 coding-activity ids is hand-maintenance — the exact thing this project exists to remove — and it silently goes stale the moment a component is added. A predicate stays correct across architecture change, which is the property that makes a derived list worth having.

**Constraints.** The language is closed (no arbitrary expressions). A selector matching **zero** activities is an error, not a no-op — a predicate that quietly matches nothing is the vacuity defect that ran through Stage 1's test suite, in data form. Selectors may only produce `additive → derived` edges; they can never remove or redirect one.

### Layer 2 — compression moves

A typed `moves[]` list on each option slot, applied to the **materialized plan** to produce that option's network. Four kinds, a closed catalog:

| Move | Effect on the option's network |
|---|---|
| `simulator {target, dependents, effortDays, riskBucket}` | insert `S-<target>`; redirect each named dependent's edge from `C-<target>` to `S-<target>`; emit the integration-debt edge so the real dependency is repaid downstream |
| `designFirst {target, designEffortDays}` | split `C-<target>` into `D-<target>` (contract design) + `C-<target>` (build); dependents' edges move to `D-<target>`; `C-<target>` depends on `D-<target>` |
| `topResources {speedup, targets}` | subsumes today's `criticalSpeedup`, capped at **1.5** per the skill's stated range |
| `split {target, parts}` | parallel pipelines over one activity. Least mechanical — implement last |

`ComputeNetwork` / `EstimateForOption` consume the mutated network unchanged; the existing CPM, float, SSGS levelling and risk machinery is untouched.

**`criticalSpeedup` is retained as `topResources`' parameter**, not deleted — it is the legitimate typed form of exactly one move (Löwy's *second* lever). What was wrong was it being the *entire* compressed solution.

### The schedule-coherence invariant

Stage 1's C1 defect is the governing lesson: a derived network passed every consistency check while asserting the system was fully tested 50 days before the managers it tests existed. Compression rewires far more aggressively than suppression did, so every option's network must be **solved and asserted**, never merely compared:

1. **Terminal-gate ordering** — the system-testing gate finishes after every activity it gates.
2. **Node completeness** — solved node count equals activities + milestones. Stage 1 silently dropped three activities because the node universe is built from *edges*, not from the activity list.
3. **Integration debt survives** — every `simulator` has a downstream activity repaying the dependency it broke. A simulator that eliminates its own debt is compression by accounting fraud.
4. **Deferral, not inversion** — a move may *defer* a dependency the architecture asserts; it may never reverse one. If `A` depends on `B`, no option may finish `A` before `B` without a simulator standing in for `B`.

These are assertions over the *solved* network, not over its shape.

### The search

Division of labour, unchanged from the ratified Part 2:

- **Code proposes candidate sites** — critical-path activities ranked by dependent fan-out.
- **The agent prices each candidate move**, and may veto with justification. A ResourceAccess simulator is a trivial fake (~5d); a `billing-manager` simulator carrying real business logic may price at 60% of the component, at which point the hill-climb rejects it naturally. No special rule needed.
- **Code selects and iterates** against the slot-15 stopping conditions (`maxCompressionPct` 0.3, `tooRiskyThreshold` 0.75, `overSafeThreshold` 0.3) plus App C §4.6's caps — ≤30% compression, efficiency ≤25%, never the death zone.

Justifications for move pricing follow Stage 1's rule, now doctrine: **ground in a property, not a superlative.** Every false justification Stage 1 produced was a superlative, each falsified by one counterexample.

**Applied moves must surface in `.sdpReview`** — Directive 7 requires management see *what* the compression buys, not a multiplier.

## Sequencing

1. **Predicate attachment** — `gates` on additives and additive milestones; C3/C4 closed; drift gate extended to cover expanded edges.
2. **Re-solve the four options against the 165-day network.** Slots 11–16 describe a network that no longer exists. Until this lands there is no honest baseline to compress against, and no way to know whether the compressed option is still excluded.
3. **`moves[]` representation** + `simulator` and `designFirst` (the two that mutate the graph), with the coherence invariant asserted.
4. **`topResources`** absorbing `criticalSpeedup`; `split` last.
5. **Candidate search + agent pricing + greedy loop**; applied moves surfaced in `.sdpReview`.

Steps 1 and 2 are independent and can run in either order; 3 depends on both.

## Testing

- **Predicate expansion**: a selector matching zero activities is rejected; `{coding: true}` expands to exactly the coding set for the committed System; expansion survives adding a component to the fixture.
- **Each move kind**: applied to a fixture network, produces the stated shape, and `ComputeNetwork` consumes the result.
- **The four coherence invariants**, asserted over the *solved* network for every option. Each must be proven load-bearing by mutation — break the invariant deliberately, observe RED.
- **The greedy loop halts** at every slot-15 threshold, and never exceeds App C's caps.
- **A simulator that removes its integration debt is rejected.**
- **Regression**: `make derived-plan-check` stays green — moves live on option slots and must never alter slot 9 or 10.

## Open items carried from Stage 1

- **I4 — the enforcement point weakened.** `ACT-COMPONENT-COVERAGE` fired inside the MCP `putDraftModel` write path; the drift gate is a Go test, and `aiarch-design.yml` runs only `validate --slot`, so an agent amending slot 9 in a design session gets no in-loop feedback. Folding re-derivation into `validate` was investigated and rejected on sound grounds (Manager package, Temporal dependency, `activityListOverrides` absent from the typed `Project` model). **Needs a ruling; not resolved by this design.**
- The `.claude` skills tree is materialized from `method-assets`; doctrine changes must be released there (v0.4.0 carries Stage 1's) or they evaporate on the next `make claude-assets`.
