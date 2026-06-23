---
name: the-method-project-design-standard-check
description: Walks Appendix C §4 Project Design Guidelines against Phase 2 artifacts. Final gate before Phase 3 / construction. Each item passes, is waived with explicit justification, or sends you back to fix. Produces project-standard-checklist.md.
---

# Project Design Standard Check

The final gate before construction begins. Every item in Appendix C §4's Project Design Guidelines is verified against the actual Phase 2 artifacts. Failures must be fixed or explicitly waived with a written justification — not silently passed.

This skill is the project-design twin of `[[the-method-system-design-standard-check]]`. It applies the same discipline (PASS / WAIVED / FAIL with verification pointer) to the second half of the design effort. See `[[the-method-doctrine]]` for the underlying directives, especially Directives 5, 6, 7, 8, and 9 — all of which govern project design.

## Canonical source

**Primary:** Löwy, [Appendix C — Design Standard](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml). Focus areas:

- [§4 "Project Design Guidelines"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec4) — the seven subsections walked below
- [§5 "Project Tracking Guidelines"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec5) (forward-look — full check during construction)
- [§2 "Directives"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec2) — Directives 5–9 in particular

## Input

The complete Phase 2 artifact set:
- `methodpoc/designs/<product>/project/planning-assumptions.md`
- `methodpoc/designs/<product>/project/activities.md`
- `methodpoc/designs/<product>/project/network.yaml`
- `methodpoc/designs/<product>/project/normal.md`
- `methodpoc/designs/<product>/project/decompressed.md`
- `methodpoc/designs/<product>/project/subcritical.md`
- `methodpoc/designs/<product>/project/compressed.md`
- `methodpoc/designs/<product>/project/risk.md`
- `methodpoc/designs/<product>/project/sdp-review.md`

The Phase 1 artifact set is also referenced — project design presupposes a valid system design:
- `methodpoc/designs/<product>/system/architecture.dsl`
- `methodpoc/designs/<product>/system/standard-checklist.md` (must already be clean)

## Output

`methodpoc/designs/<product>/project/project-standard-checklist.md`

## Procedure

Walk each Appendix C §4 item. For each, record: **PASS**, **WAIVED** (with justification), or **FAIL** (with required fix). Status is determined by inspecting the named artifact — no item passes by assertion.

### Section A — General (App C §4.1)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 1a | Do not design a clock | Walk `activities.md` — no activity is calendar-locked to a wall clock; all durations are work-day quanta. Inspect `network.yaml` — no node carries an absolute date as a constraint. | |
| 1b | Never design a project without an architecture that encapsulates the volatilities | `architecture.dsl` exists, has passed `system/standard-checklist.md`, and every coding activity in `activities.md` maps to exactly one component in the DSL. | |
| 1c | Capture and verify planning assumptions | `planning-assumptions.md` exists and enumerates resources, calendar, infrastructure, and external dependencies. | |
| 1d | Follow the design of project design | Phase 2 artifacts exist in canonical order: planning-assumptions → activities → network → normal → decompressed → subcritical → compressed → risk → sdp-review. None is skipped. | |
| 1e | Design several options for the project; at a minimum normal, compressed, and subcritical | `normal.md`, `compressed.md`, and `subcritical.md` all exist with computed duration and cost. `decompressed.md` is also produced as the fourth option. | |
| 1f | Communicate with management in Optionality | `sdp-review.md` presents all viable options side-by-side with duration/cost/risk and a time-cost / time-risk curve — not a single recommendation in isolation. | |
| 1g | Always go through SDP review before the main work starts | `sdp-review.md` exists and is structured as a management-facing document (audience, recommendation, options table). | |

### Section B — Staffing (App C §4.2)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 2a | Avoid multiple architects | `planning-assumptions.md` names a single architect role. Resource list in `normal.md` shows exactly one architect. | |
| 2b | Have a core team in place at the beginning | `normal.md` resource histogram starts with the core team (architect, lead, key seniors) at week 1, not ramped in later. | |
| 2c | Ask for only the lowest level of staffing required to progress unimpeded along the critical path | `normal.md` is sized to the critical path, not the whole network. Verify the staffing curve matches the critical-path width, not the total activity count. | |
| 2d | Always assign resources based on float | `normal.md` assignment narrative shows critical-path activities staffed first, then near-critical, then high-float — best resources flow to lowest float. | |
| 2e | Ensure correct staffing distribution | `normal.md` shows a realistic histogram (ramp up, plateau, ramp down — not a flat brick or a spike). | |
| 2f | Ensure a shallow S curve for the planned earned value | `normal.md` includes a cumulative earned-value curve that is gently sloped (no late hockey-stick, no front-loaded vertical climb). | |
| 2g | Always assign components to developers in a 1:1 ratio | `activities.md` and `normal.md` show one developer per component for detailed-design + construction activities. No two developers share a component; no developer builds two components in parallel. | |
| 2h | Strive for task continuity | `normal.md` resource timeline keeps each developer on related activities back-to-back where possible — no fragmenting a person across unrelated components. | |

### Section C — Integration (App C §4.3)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 3a | Avoid mass integration points | `network.yaml` shows no single integration activity that joins many parallel chains at once. Integration is incremental. | |
| 3b | Avoid integration at the end of the project | `activities.md` includes integration activities distributed across the timeline, not a single end-of-project integration phase. | |

### Section D — Estimations (App C §4.4)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 4a | Do not overestimate | `activities.md` estimates are not padded with hidden contingency — risk lives in the network/risk model, not inside individual estimates. | |
| 4b | Do not underestimate | `activities.md` estimates are not aspirational — each cites a basis (similar past work, decomposition into sub-steps, or expert input). | |
| 4c | Strive for accuracy, not precision | Estimates are in 5-day quanta (week-level), not hours or half-days. | |
| 4d | Always use a quantum of five days in any activity estimation | Every duration in `activities.md` and `network.yaml` is a multiple of 5 days. | |
| 4e | Estimate the project as a whole to validate or even initiate your project design | `planning-assumptions.md` or `normal.md` documents a top-down whole-project estimate that has been cross-checked against the bottom-up activity sum. | |
| 4f | Reduce estimation uncertainty | `activities.md` notes which activities have wide variance and how that uncertainty has been mitigated (prototype, spike, narrowed scope). | |
| 4g | When required, maintain correct estimation dialog | Where estimates were challenged or revised, `activities.md` or `planning-assumptions.md` records the dialog (who, what changed, why) — estimates are not silently edited. | |

### Section E — Project network (App C §4.5)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 5a | Treat resource dependencies as dependencies | `network.yaml` encodes resource-driven sequencing as edges, not as implicit scheduling. Two activities sharing one developer are wired in series. | |
| 5b | Verify all activities reside on a chain that starts and ends on a critical path | Walk `network.yaml` — every activity has a path back to project start and forward to project end via the critical path or a feeder chain. No orphan activities. | |
| 5c | Verify all activities have a resource assigned to them | `normal.md` (and the other solutions where resources differ) shows every activity in `network.yaml` with a named resource. No unassigned nodes. | |
| 5d | Avoid node diagrams | `network.yaml` semantics are arrow-on-edge (activity = arrow, node = event). Inspect the diagram form rendered from it. | |
| 5e | Prefer arrow diagrams | Same — the artifact is arrow-form, not PERT/MOP node-form. | |
| 5f | Avoid god activities | `activities.md` has no single activity that dwarfs the rest (a single 60-day node among 5-15-day peers is a god activity). | |
| 5g | Break large projects into a network of networks | If total activity count exceeds the cyclomatic-complexity guideline, `network.yaml` is decomposed into sub-networks per subsystem. Otherwise N/A. | |
| 5h | Treat near-critical chains as critical chains | `risk.md` and `normal.md` flag chains with float ≤ ~20% of project duration as near-critical and manage them as criticality contributors. | |
| 5i | Strive for cyclomatic complexity as low as 10 to 12 | Compute cyclomatic complexity of `network.yaml` (edges − nodes + 2). Confirm ≤ ~12, or justify. | |
| 5j | Design by layers to reduce complexity | `network.yaml` chains follow the system-design layer order (RA/Resources → Engines → Managers → Clients) so dependencies fall naturally. | |

### Section F — Time and cost (App C §4.6)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 6a | Accelerate the project first by quick and clean practices rather than compression | `compressed.md` documents quick-and-clean wins (better tooling, removing waste, parallel-by-default work patterns) before reaching for extra staff or overlap. | |
| 6b | Never commit to a project in the death zone | `sdp-review.md` shows no recommended option past the death-zone boundary on the time-cost curve. Any option in the death zone is plotted but explicitly rejected. | |
| 6c | Compress with parallel work rather than top resources | `compressed.md` shows compression achieved primarily by parallelizing previously-serial chains, only secondarily by adding senior resources. | |
| 6d | Compress with top resources carefully and judiciously | Where top resources are used to compress, `compressed.md` justifies each instance (specific skill needed, specific bottleneck). | |
| 6e | Avoid compression higher than 30% | `compressed.md` total compression vs `normal.md` is ≤ 30%, or the option is plotted but rejected. | |
| 6f | Avoid projects with efficiency higher than 25% | Compute efficiency (critical-path work ÷ total work × resources) for each option in `risk.md` or `sdp-review.md`. Flag options above 25%. | |
| 6g | Compress the project even if the likelihood of pursuing any of the compressed options is low | `compressed.md` exists regardless of whether the team plans to use it — it informs the time-cost curve. | |

### Section G — Risk (App C §4.7)

| # | Guideline | How to verify | Status |
|---|---|---|---|
| 7a | Customize the ranges of criticality risk to your project | `risk.md` defines the criticality-risk bands (green/yellow/red thresholds) explicitly for this project, not as generic defaults. | |
| 7b | Adjust floats outliers with activity risk | `risk.md` applies activity-risk weighting to chains with disproportionate floats, producing a blended risk number rather than raw criticality. | |
| 7c | Decompress the normal solution past the tipping point on the risk curve | `decompressed.md` exists and shows risk dropped via duration extension (without consuming float through staff cuts). | |
| 7c.i | Target decompression to 0.5 risk | `decompressed.md` risk number lands near 0.5. | |
| 7c.ii | Value the risk tipping point more than a specific risk number | `risk.md` shows the time-risk curve with the tipping point marked, and `decompressed.md` is positioned at the tipping point — not at an arbitrary numeric target. | |
| 7d | Do not over-decompress | `decompressed.md` risk does not fall below ~0.3 (over-decompression). | |
| 7e | Decompress design-by-layers solutions, perhaps aggressively so | If `network.yaml` is layered, `decompressed.md` notes whether aggressive decompression was applied and why. | |
| 7f | Keep normal solutions at less than 0.7 risk | `risk.md` shows the `normal.md` risk number < 0.7. If ≥ 0.7, normal must be redesigned (more staff) — not waived. | |
| 7g | Avoid risk lower than 0.3 | No option in `sdp-review.md` has risk < 0.3 as a recommendation. | |
| 7h | Avoid risk higher than 0.75 | No option in `sdp-review.md` has risk > 0.75 as a recommendation. | |
| 7i | Avoid project options riskier or safer than the risk crossover points | `risk.md` plots the exclusion zones (below crossover-safe, above crossover-risky); `sdp-review.md` recommends only options inside the viable band. | |

### Section H — Directive cross-check

| # | Directive | How to verify | Status |
|---|---|---|---|
| D5 | Design iteratively, build incrementally | `sdp-review.md` recommended option supports incremental delivery — integration is distributed (cross-check with 3a/3b). | |
| D6 | Design the project to build the system | Every component in `architecture.dsl` appears as a detailed-design + construction activity pair in `activities.md`. Conversely, every coding activity maps to exactly one component. | |
| D7 | Educated decisions with options | `sdp-review.md` recommendation cites the cost / duration / risk trade-off across the four options — not a single solution presented as inevitable. | |
| D8 | Build along the critical path | `normal.md` resource-assignment narrative confirms best resources on the critical path first. (Forward-look for actual execution.) | |
| D9 | Be on time throughout | (Forward-look — applies in /implement-project via Project Tracking Guidelines §5) | N/A here |

## Output format

Write `project-standard-checklist.md` as a single table with **every** item from Sections A–H, with a Status column showing PASS / WAIVED / FAIL.

For WAIVED items, include a Justification column with a sentence explaining why this project intentionally deviates and which business objective (from `mission.md`) backs the deviation.

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

- `project-standard-checklist.md` exists
- Zero FAIL entries (any FAIL sends you back to the relevant Phase 2 step — typically the named artifact in the verification column)
- Every WAIVED entry has a written justification tied to a business objective from `mission.md`
- Summary block at bottom counts total / PASS / WAIVED / FAIL

Project design is complete. Next: `/implement-project <product>` (Phase 3 / construction).

## When to waive vs fix

**Waive when:**
- The deviation is intentional and traces to a business objective from `mission.md`
- The book itself acknowledges contexts where the rule may bend (e.g., 5g network-of-networks is N/A for very small projects; 5e arrow-vs-node is occasionally waived when tooling forces a node diagram, provided the semantics are preserved)
- Management has accepted the trade-off explicitly in `sdp-review.md`

**Fix when:**
- The violation reveals a malformed network (5a, 5b, 5c — these are correctness conditions, not preferences)
- The violation breaks a hard threshold (6b death zone, 6e >30% compression, 7f normal >0.7 risk, 7h any option >0.75 risk)
- The violation invalidates the SDP review's optionality (1e missing one of the required solutions; 1f single-option recommendation)
- The violation has no business objective backing it

## Anti-patterns to reject

- **Single-option SDP review** — `sdp-review.md` recommends only "the plan" with no alternatives. Violates 1e, 1f, D7. Fix by building the missing options.
- **Padded estimates** — individual activities in `activities.md` carry hidden buffer "just in case". Violates 4a, 4c. Fix by moving contingency into the risk model where it is visible.
- **Late integration phase** — a single integration node at the end of `network.yaml`. Violates 3a, 3b, D5. Fix by distributing integration activities across the timeline.
- **God activity** — one activity dwarfing the rest in `activities.md`. Violates 5f. Decompose.
- **Normal at risk ≥ 0.7** — handed off as-is rather than redesigned. Violates 7f. Add staff or reduce scope; do not waive.
- **Death-zone recommendation** — `sdp-review.md` recommends an option in the death zone. Violates 6b, D7. The death zone is a hard exclusion, not a trade-off.
- **Resources without float-based assignment** — best people sprinkled across high-float activities while the critical path runs on juniors. Violates 2d, D8.
- **Components without 1:1 developer mapping** — two developers on one component or one developer multitasking two in parallel. Violates 2g.
- **Decompressed solution missing or at the same duration as normal** — the decompressed option is the analytical proof that risk drops with time; skipping it leaves the time-risk curve unsupported. Violates 7c.
