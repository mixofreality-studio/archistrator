---
name: the-method-project-design-standard-check
description: Walks Appendix C §4 Project Design Guidelines against the Phase 2 artifact slots in project.json. Final gate before Phase 3 / construction. Each item passes, is waived with explicit justification, or sends you back to fix. A verification gate over the committed Phase-2 slots — results are recorded against the .sdpReview slot (there is no separate project-standard-check slot).
---

# Project Design Standard Check

The final gate before construction begins. Every item in Appendix C §4's Project Design Guidelines is verified against the actual Phase 2 artifacts. Failures must be fixed or explicitly waived with a written justification — not silently passed.

This skill is the project-design twin of `[[the-method-system-design-standard-check]]`. It applies the same discipline (PASS / WAIVED / FAIL with verification pointer) to the second half of the design effort. See `[[the-method-doctrine]]` for the underlying directives, especially Directives 5, 6, 7, 8, and 9 — all of which govern project design.

## Canonical source

**Primary:** Löwy, [Appendix C — Design Standard](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml). Focus areas:

- [§4 "Project Design Guidelines"](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec4) — the seven subsections walked below
- [§5 "Project Tracking Guidelines"](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec5) (forward-look — full check during construction)
- [§2 "Directives"](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec2) — Directives 5–9 in particular

## Input

State is git-as-DB: all of this lives in `.aiarch/state/project.json` (a typed JSON aggregate), NOT in `designs/<product>/*.md` or the `.network` slot files. Markdown/DSL/YAML is a render-on-read of the typed state, never the source of truth.

The complete Phase 2 committed slot set:
- `.planningAssumptions`
- `.activityList`
- `.network`
- `.normalSolution`
- `.decompressedSolution`
- `.subcriticalSolution`
- `.compressedSolution`
- `.riskModel`
- `.sdpReview`

The Phase 1 slots are also referenced — project design presupposes a valid system design:
- the committed `.systemDesign` slot
- the committed `.standardCheck` slot (the Phase-1 design standard check; must already be clean)

## Output

A verification gate report walking every Appendix C §4 item against the committed Phase-2 slots, with PASS / WAIVED / FAIL per item. There is **no separate project-standard-check slot** in `project.json` (the Phase-2 slot set ends at `.sdpReview`); record the gate results against the `.sdpReview` slot — in its `Notes` / `CritiqueNotes` and the review verdict — so the standard check travels with the SDP review the gate validates. Any markdown rendering (the table below) is a render-on-read of that recorded result, not a `project-standard-checklist.md` file.

## Procedure

Walk each Appendix C §4 item. For each, record: **PASS**, **WAIVED** (with justification), or **FAIL** (with required fix). Status is determined by inspecting the named slot — no item passes by assertion.

### Section A — General (App C §4.1)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 1a | Do not design a clock | Walk the `.activityList` slot — no activity is calendar-locked to a wall clock; all durations are work-day quanta. Inspect the `.network` slot — no node carries an absolute date as a constraint. | |
| 1b | Never design a project without an architecture that encapsulates the volatilities | the `.systemDesign` slot exists, has passed the Phase-1 `.standardCheck` slot, and every coding activity in the `.activityList` slot maps to exactly one component in the architecture. | |
| 1c | Capture and verify planning assumptions | the `.planningAssumptions` slot exists and enumerates resources, calendar, infrastructure, and external dependencies. | |
| 1d | Follow the design of project design | Phase 2 slots are committed in canonical order: `.planningAssumptions` → `.activityList` → `.network` → `.normalSolution` → `.decompressedSolution` → `.subcriticalSolution` → `.compressedSolution` → `.riskModel` → `.sdpReview`. None is skipped. | |
| 1e | Design several options for the project; at a minimum normal, compressed, and subcritical | the `.normalSolution` slot, the `.compressedSolution` slot, and the `.subcriticalSolution` slot all exist with computed duration and cost. the `.decompressedSolution` slot is also produced as the fourth option. | |
| 1f | Communicate with management in Optionality | the `.sdpReview` slot presents all viable options side-by-side with duration/cost/risk and a time-cost / time-risk curve — not a single recommendation in isolation. | |
| 1g | Always go through SDP review before the main work starts | the `.sdpReview` slot exists and is structured as a management-facing document (audience, recommendation, options table). | |

### Section B — Staffing (App C §4.2)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 2a | Avoid multiple architects | the `.planningAssumptions` slot names a single architect role. Resource list in the `.normalSolution` slot shows exactly one architect. | |
| 2b | Have a core team in place at the beginning | the `.normalSolution` slot resource histogram starts with the core team (architect, lead, key seniors) at week 1, not ramped in later. | |
| 2c | Ask for only the lowest level of staffing required to progress unimpeded along the critical path | the `.normalSolution` slot is sized to the critical path, not the whole network. Verify the staffing curve matches the critical-path width, not the total activity count. | |
| 2d | Always assign resources based on float | the `.normalSolution` slot assignment narrative shows critical-path activities staffed first, then near-critical, then high-float — best resources flow to lowest float. | |
| 2e | Ensure correct staffing distribution | the `.normalSolution` slot shows a realistic histogram (ramp up, plateau, ramp down — not a flat brick or a spike). | |
| 2f | Ensure a shallow S curve for the planned earned value | the `.normalSolution` slot includes a cumulative earned-value curve that is gently sloped (no late hockey-stick, no front-loaded vertical climb). | |
| 2g | Always assign components to developers in a 1:1 ratio | the `.activityList` slot and the `.normalSolution` slot show one developer per component for detailed-design + construction activities. No two developers share a component; no developer builds two components in parallel. | |
| 2h | Strive for task continuity | the `.normalSolution` slot resource timeline keeps each developer on related activities back-to-back where possible — no fragmenting a person across unrelated components. | |

### Section C — Integration (App C §4.3)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 3a | Avoid mass integration points | the `.network` slot shows no single integration activity that joins many parallel chains at once. Integration is incremental. | |
| 3b | Avoid integration at the end of the project | the `.activityList` slot includes integration activities distributed across the timeline, not a single end-of-project integration phase. | |

### Section D — Estimations (App C §4.4)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 4a | Do not overestimate | the `.activityList` slot estimates are not padded with hidden contingency — risk lives in the network/risk model, not inside individual estimates. | |
| 4b | Do not underestimate | the `.activityList` slot estimates are not aspirational — each cites a basis (similar past work, decomposition into sub-steps, or expert input). | |
| 4c | Strive for accuracy, not precision | Estimates are in 5-day quanta (week-level), not hours or half-days. | |
| 4d | Always use a quantum of five days in any activity estimation | Every duration in the `.activityList` slot and the `.network` slot is a multiple of 5 days. | |
| 4e | Estimate the project as a whole to validate or even initiate your project design | the `.planningAssumptions` slot or the `.normalSolution` slot documents a top-down whole-project estimate that has been cross-checked against the bottom-up activity sum. | |
| 4f | Reduce estimation uncertainty | the `.activityList` slot notes which activities have wide variance and how that uncertainty has been mitigated (prototype, spike, narrowed scope). | |
| 4g | When required, maintain correct estimation dialog | Where estimates were challenged or revised, the `.activityList` slot or the `.planningAssumptions` slot records the dialog (who, what changed, why) — estimates are not silently edited. | |

### Section E — Project network (App C §4.5)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 5a | Treat resource dependencies as dependencies | the `.network` slot encodes resource-driven sequencing as edges, not as implicit scheduling. Two activities sharing one developer are wired in series. | |
| 5b | Verify all activities reside on a chain that starts and ends on a critical path | Walk the `.network` slot — every activity has a path back to project start and forward to project end via the critical path or a feeder chain. No orphan activities. | |
| 5c | Verify all activities have a resource assigned to them | the `.normalSolution` slot (and the other solutions where resources differ) shows every activity in the `.network` slot with a named resource. No unassigned nodes. | |
| 5d | Avoid node diagrams | the `.network` slot semantics are arrow-on-edge (activity = arrow, node = event). Inspect the diagram form rendered from it. | |
| 5e | Prefer arrow diagrams | Same — the artifact is arrow-form, not PERT/MOP node-form. | |
| 5f | Avoid god activities | the `.activityList` slot has no single activity that dwarfs the rest (a single 60-day node among 5-15-day peers is a god activity). | |
| 5g | Break large projects into a network of networks | If total activity count exceeds the cyclomatic-complexity guideline, the `.network` slot is decomposed into sub-networks per subsystem. Otherwise N/A. | |
| 5h | Treat near-critical chains as critical chains | the `.riskModel` slot and the `.normalSolution` slot flag chains with float ≤ ~20% of project duration as near-critical and manage them as criticality contributors. | |
| 5i | Strive for cyclomatic complexity as low as 10 to 12 | Compute cyclomatic complexity of the `.network` slot (edges − nodes + 2). Confirm ≤ ~12, or justify. | |
| 5j | Design by layers to reduce complexity | the `.network` slot chains follow the system-design layer order (RA/Resources → Engines → Managers → Clients) so dependencies fall naturally. | |

### Section F — Time and cost (App C §4.6)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 6a | Accelerate the project first by quick and clean practices rather than compression | the `.compressedSolution` slot documents quick-and-clean wins (better tooling, removing waste, parallel-by-default work patterns) before reaching for extra staff or overlap. | |
| 6b | Never commit to a project in the death zone | the `.sdpReview` slot shows no recommended option past the death-zone boundary on the time-cost curve. Any option in the death zone is plotted but explicitly rejected. | |
| 6c | Compress with parallel work rather than top resources | the `.compressedSolution` slot shows compression achieved primarily by parallelizing previously-serial chains, only secondarily by adding senior resources. | |
| 6d | Compress with top resources carefully and judiciously | Where top resources are used to compress, the `.compressedSolution` slot justifies each instance (specific skill needed, specific bottleneck). | |
| 6e | Avoid compression higher than 30% | the `.compressedSolution` slot total compression vs the `.normalSolution` slot is ≤ 30%, or the option is plotted but rejected. | |
| 6f | Avoid projects with efficiency higher than 25% | Compute efficiency (critical-path work ÷ total work × resources) for each option in the `.riskModel` slot or the `.sdpReview` slot. Flag options above 25%. | |
| 6g | Compress the project even if the likelihood of pursuing any of the compressed options is low | the `.compressedSolution` slot exists regardless of whether the team plans to use it — it informs the time-cost curve. | |

### Section G — Risk (App C §4.7)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 7a | Customize the ranges of criticality risk to your project | the `.riskModel` slot defines the criticality-risk bands (green/yellow/red thresholds) explicitly for this project, not as generic defaults. | |
| 7b | Adjust floats outliers with activity risk | the `.riskModel` slot applies activity-risk weighting to chains with disproportionate floats, producing a blended risk number rather than raw criticality. | |
| 7c | Decompress the normal solution past the tipping point on the risk curve | the `.decompressedSolution` slot exists and shows risk dropped via duration extension (without consuming float through staff cuts). | |
| 7c.i | Target decompression to 0.5 risk | the `.decompressedSolution` slot risk number lands near 0.5. | |
| 7c.ii | Value the risk tipping point more than a specific risk number | the `.riskModel` slot shows the time-risk curve with the tipping point marked, and the `.decompressedSolution` slot is positioned at the tipping point — not at an arbitrary numeric target. | |
| 7d | Do not over-decompress | the `.decompressedSolution` slot risk does not fall below ~0.3 (over-decompression). | |
| 7e | Decompress design-by-layers solutions, perhaps aggressively so | If the `.network` slot is layered, the `.decompressedSolution` slot notes whether aggressive decompression was applied and why. | |
| 7f | Keep normal solutions at less than 0.7 risk | the `.riskModel` slot shows the the `.normalSolution` slot risk number < 0.7. If ≥ 0.7, normal must be redesigned (more staff) — not waived. | |
| 7g | Avoid risk lower than 0.3 | No option in the `.sdpReview` slot has risk < 0.3 as a recommendation. | |
| 7h | Avoid risk higher than 0.75 | No option in the `.sdpReview` slot has risk > 0.75 as a recommendation. | |
| 7i | Avoid project options riskier or safer than the risk crossover points | the `.riskModel` slot plots the exclusion zones (below crossover-safe, above crossover-risky); the `.sdpReview` slot recommends only options inside the viable band. | |

### Section H — Directive cross-check

| # | Directive | How to verify | Status |
|---|---|---|---|
| D5 | Design iteratively, build incrementally | the `.sdpReview` slot recommended option supports incremental delivery — integration is distributed (cross-check with 3a/3b). | |
| D6 | Design the project to build the system | Every component in the `.systemDesign` slot appears as a detailed-design + construction activity pair in the `.activityList` slot. Conversely, every coding activity maps to exactly one component. | |
| D7 | Educated decisions with options | the `.sdpReview` slot recommendation cites the cost / duration / risk trade-off across the four options — not a single solution presented as inevitable. | |
| D8 | Build along the critical path | the `.normalSolution` slot resource-assignment narrative confirms best resources on the critical path first. (Forward-look for actual execution.) | |
| D9 | Be on time throughout | (Forward-look — applies in /implement-project via Project Tracking Guidelines §5) | N/A here |

## Output format

Record the gate result against the `.sdpReview` slot (in `Notes` / `CritiqueNotes` and the review verdict) as a single table with **every** item from Sections A–H, with a Status column showing PASS / WAIVED / FAIL. The markdown below is the human rendering of that recorded result.

For WAIVED items, include a Justification column with a sentence explaining why this project intentionally deviates and which business objective (from the committed `.mission` slot) backs the deviation.

For FAIL items, do not waive — return to the prior Phase 2 step, fix, and re-run this skill.

```markdown
# Project Design Standard Checklist — <Product>

Date: <YYYY-MM-DD>
Reviewer: <agent or user>

| Section | Item | Status | Justification (if waived) | Fix needed (if failed) |
|---|---|---|---|---|
| General 1a | Do not design a clock | PASS | | |
| General 1b | Architecture encapsulates volatilities | PASS | | |
| General 1e | Normal / compressed / subcritical options exist | PASS | | |
...
| Staffing 2g | 1:1 components-to-developers | PASS | | |
...
| Network 5d | Avoid node diagrams | PASS | | |
| Network 5e | Prefer arrow diagrams | PASS | | |
...
| Risk 7f | Normal solution risk < 0.7 | PASS | | |
| Risk 7c.i | Decompressed targets 0.5 | PASS | | |
...
| D6 | Design the project to build the system | PASS | | |

## Summary

- Total items checked: 47
- PASS: 44
- WAIVED: 3
- FAIL: 0

Phase 2 design is complete. Ready for /implement-project.
```

## Exit criteria (for router)

- The gate result is recorded against the `.sdpReview` slot
- Zero FAIL entries (any FAIL sends you back to the relevant Phase 2 step — typically the named slot in the verification column)
- Every WAIVED entry has a written justification tied to a business objective from the committed `.mission` slot
- Summary block counts total / PASS / WAIVED / FAIL

Project design is complete. Next: `AdvancePhase` into Phase 3 / construction (or `/implement-project` locally).

## When to waive vs fix

**Waive when:**
- The deviation is intentional and traces to a business objective from the committed `.mission` slot
- The book itself acknowledges contexts where the rule may bend (e.g., 5g network-of-networks is N/A for very small projects; 5e arrow-vs-node is occasionally waived when tooling forces a node diagram, provided the semantics are preserved)
- Management has accepted the trade-off explicitly in the `.sdpReview` slot

**Fix when:**
- The violation reveals a malformed network (5a, 5b, 5c — these are correctness conditions, not preferences)
- The violation breaks a hard threshold (6b death zone, 6e >30% compression, 7f normal >0.7 risk, 7h any option >0.75 risk)
- The violation invalidates the SDP review's optionality (1e missing one of the required solutions; 1f single-option recommendation)
- The violation has no business objective backing it

## Anti-patterns to reject

- **Single-option SDP review** — the `.sdpReview` slot recommends only "the plan" with no alternatives. Violates 1e, 1f, D7. Fix by building the missing options.
- **Padded estimates** — individual activities in the `.activityList` slot carry hidden buffer "just in case". Violates 4a, 4c. Fix by moving contingency into the risk model where it is visible.
- **Late integration phase** — a single integration node at the end of the `.network` slot. Violates 3a, 3b, D5. Fix by distributing integration activities across the timeline.
- **God activity** — one activity dwarfing the rest in the `.activityList` slot. Violates 5f. Decompose.
- **Normal at risk ≥ 0.7** — handed off as-is rather than redesigned. Violates 7f. Add staff or reduce scope; do not waive.
- **Death-zone recommendation** — the `.sdpReview` slot recommends an option in the death zone. Violates 6b, D7. The death zone is a hard exclusion, not a trade-off.
- **Resources without float-based assignment** — best people sprinkled across high-float activities while the critical path runs on juniors. Violates 2d, D8.
- **Components without 1:1 developer mapping** — two developers on one component or one developer multitasking two in parallel. Violates 2g.
- **Decompressed solution missing or at the same duration as normal** — the decompressed option is the analytical proof that risk drops with time; skipping it leaves the time-risk curve unsupported. Violates 7c.
