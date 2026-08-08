# Appendix C Compliance Map

Source: Juval Löwy, *Righting Software* (2019), Appendix C "Design Standard" (`research/rightingsoftware/OEBPS/xhtml/appc.xhtml`) — the Prime Directive, the 9 Directives, System Design Guidelines §1–§6, Project Design Guidelines §1–§7, Project Tracking Guidelines, and Service Contract Design Guidelines.

This document maps **every item in Appendix C** to how archistrator enforces it. Every row is tagged with one or more of:

| Tag | Meaning |
|---|---|
| **[1] Structural** | Enforced by the nature of using archistrator — the typed schema, the single-role agent roster, an agent's MCP tool grant (or lack of it), or an assembly algorithm that cannot *produce* a violating result. Nobody has to remember to check; the shape of the system makes the violation unrepresentable or unreachable. |
| **[2] Mechanical** | Enforced by an automated rule that inspects the committed/drafted state and emits a pass/fail finding. Three sub-mechanisms exist (see "How mechanical enforcement works" below): the app-side **live tier**, the platform **committed/authoring-gate tier**, and a small number of **numeric guards embedded in engine code**. |
| **[3] Skill-reviewed** | Walked explicitly by a Method skill that installs during construction — an agent applies PASS / WAIVED / FAIL judgment with a required justification, or authors a waiver/attestation. This is where genuinely non-machine-checkable judgment (the Prime Directive, "is this design symmetric," "is this contract reusable") lives. |
| **[gap]** | Not currently covered by any of the above. Listed with a remediation note in [Gaps and remediation plan](#gaps-and-remediation-plan). |

A guideline typically carries more than one tag — e.g. service-contract op-count is checked by *two independent* mechanical layers *and* walked by a skill before the contract ever reaches the mechanical gate.

## How mechanical enforcement works

Archistrator runs Appendix-C mechanical rules in three places, all over the same typed `project.json`:

1. **App-side live tier** — `server/internal/engine/designhealth/designhealthengine.go`. A pure function, render-on-read, run at the `putDraftModel` authoring gate, in the Design Health view, and in CI. Rule IDs: `DH-*` and the shared `CC-*` / `CUC-*` call-chain family. Mostly `SeverityWarning`/`SeverityInfo` (advisory, non-blocking) except the `CC-*` family and contract/facet rules, which are `SeverityError` (hard gate, since the 2026-08-01 rollout).
2. **Platform committed/authoring-gate tier** — `archistrator-platform/framework-go/methodcheck/*.go`. The published framework module archistrator imports. Rule IDs: `SYS-*`, `APPC-*`, `DV-*`, `CC-*` (shared with the live tier), `DEP-*`, `STP-*`, etc. Carries the canonical Appendix-C **coverage matrix** (`coverage.go` → `DefaultCoverage()`), which explicitly classifies every System Design / Service Contract item as `automated-design` / `automated-contract` / `human-judgment` / `permission`, and a platform test (`emitters.go`) asserts every item marked automated actually has a wired emitter — so this classification can't silently rot.
3. **Numeric guards embedded in engine code** — e.g. `server/internal/manager/projectdesign/assemblesdpreview.go`'s `sdpOptionInBand`/`recommendOption`, which mechanically excludes any project-design option outside the App-C risk/compression bands from being the *recommended* option. **Caveat, verified directly in code:** this steers the recommendation only — `sdpCommit` (assemblesdpreview.go:147-179) checks only that the architect's chosen `OptionID` was one of the assembled options (`optionInReview`), not that it was in-band. An architect can still commit an out-of-band option (e.g. a >30%-compressed or >0.75-risk one); the mechanism makes a bad option *hard to end up with by default*, not *impossible to choose*. `DefaultCoverage()` does **not** know about these guards (it only classifies System Design + Service Contract); see [Gaps](#gaps-and-remediation-plan).

Guideline severities: directives are `SeverityError` at the mechanical layer and non-waivable; guideline-severity mechanical findings (`SeverityWarning`) can be downgraded to `SeverityInfo` via `applyWaivers` when a `StandardCheck` waiver names the rule with a justification.

---

## Prime Directive & the 9 Directives

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| PRIME | Never design against the requirements. | **[3]** | `the-method-system-design-standard-check` — architect attestation on `.systemDesign`, evidenced by the volatility→component mapping and "no component named after a use case or domain." Re-attested on every amendment. |
| DIR-1 | Avoid functional decomposition. | **[2]+[3]** | **[2]** `DH-COMP-NO-VOLATILITY` (live tier) — every Manager/Engine/ResourceAccess must encapsulate ≥1 volatility; a component owning none is flagged as "functional decomposition, the siren song" verbatim. **[3]** D1 attestation (component roster review) in `the-method-system-design-standard-check`. |
| DIR-2 | Decompose based on volatility. | **[2]+[3]** | **[2]** `DH-VOL-ENCAP-MISSING`/`DH-COMP-VOL-DANGLING` (live tier, `SeverityError`) — every volatility must be owned by ≥1 component via the typed `encapsulatesVolatilities` join; dangling references are errors. **[3]** D2 attestation ("every component encapsulates exactly one volatility" — the live join backs presence, the attestation judges meaningfulness). |
| DIR-3 | Provide a composable design. | **[3]** | D3 attestation, two evidenced sub-claims: non-core use cases compose from existing components (cite the non-core dynamic views); the roster is the *smallest* set that satisfies every core use case (justification must name what was consolidated and why a smaller set fails). |
| DIR-4 | Offer features as aspects of integration, not implementation. | **[3]** | D4 attestation, evidenced by dynamic views showing features emerging from component interaction, not a new component per feature. Also invoked directly by `the-method-service-contract` ("a contract operation named after a feature is a Directive 4 violation"). |
| DIR-5 | Design iteratively, build incrementally. | **[1]+[3]** | **[1]** Phase-1 half: the amendment→staleness→re-run loop is itself the mechanism (cited as evidence, not separately attested). **[3]** Phase-2 half: `the-method-project-design-standard-check` §H "D5" cross-checks that the SDP-recommended option supports incremental delivery / distributed integration. |
| DIR-6 | Design the project to build the system. | **[3]** | `the-method-project-design-standard-check` §H "D6": every `.systemDesign` component maps to exactly one detailed-design+construction activity pair in `.activityList`, and conversely — walked, not machine-joined. |
| DIR-7 | Drive educated decisions with viable options that differ by schedule, cost, and risk. | **[1]+[3]** | **[1]** `.sdpReview` is *assembled*, not hand-written, from four computed options (normal/decompressed/subcritical/compressed) — a single-option review is structurally absent unless a solution slot was never committed. **[3]** §H "D7" confirms the recommendation cites the trade-off, not a single inevitable plan. |
| DIR-8 | Build the project along its critical path. | **[3]** | §H "D8": `.normalSolution` resource-assignment narrative confirms best resources land on the critical path first (forward-looking check; execution-time confirmation is Directive 9's job). |
| DIR-9 | Be on time throughout the project. | **[3]** | `the-method-project-tracking` — mandatory weekly cadence, earned value / CPI / SPI, App A §5 pattern classification, corrective-action triggers. This skill's whole existence *is* Directive 9's enforcement mechanism. |

---

## System Design Guidelines

### §1 Requirements

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| SYS-1a | Capture required behavior, not required functionality. | **[1]+[3]** | **[1]** The committed artifact is typed `Required Behaviors` (`.scrubbedRequirements`), not a generic feature/backlog shape. **[3]** `the-method-requirements-analysis` drives the scrubbing pass. |
| SYS-1b | Describe required behavior with use cases. | **[1]** | `.coreUseCases` is a required typed artifact (id, classification, trigger, actors, activity diagram) — there is no code path that reaches System Design without authoring it in this shape. |
| SYS-1c | Document all use cases with nested conditions with activity diagrams. | **[2]** | `UC-ACTDIAG` (`ruleUcActDiagram`, committed tier) — coverage-matrix-bound `SYS-1c`, `automated-design`. |
| SYS-1d | Eliminate solutions masquerading as requirements. | **[3]** | `the-method-requirements-analysis` — the "Four Questions" + scrubbing pass is precisely this. |
| SYS-1e | Validate the system design by ensuring it supports all core use cases. | **[2]** | Doubly covered: `ARCH-CHAINCOV` (committed tier — every *core* UC has a dynamic view) + live-tier `DH-COV-UC-DYNAMIC` (`SeverityError`) + `USECASE-DYNAMIC-MISSING` (committed tier, founder extension: *every* UC, core or non-core, needs a dynamic view) + the full `CC-*`/`CUC-*` call-chain-correspondence family (11 rules, both tiers, `SeverityError`) verifying the dynamic view doesn't just *exist* but actually *corresponds* to the use case's activity diagram end-to-end. This goes well beyond the book's literal ask — see [Additional validations](#additional-validations-beyond-appendix-c). |

### §2 Cardinality

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| SYS-2a | Avoid more than five Managers without subsystems. | **[2]** | `SYS-CARD-MGR` (committed, `SeverityError`, hard 5-Manager cap) + `DH-CARD-MANAGERS` (live tier, `SeverityWarning`, same threshold, softer severity for the render-on-read strip). |
| SYS-2b | Avoid more than a handful of subsystems. | **[gap]** | No mechanical check (subsystems aren't yet modeled as a first-class typed construct); `human-judgment` per `DefaultCoverage()`. |
| SYS-2c | Avoid more than three Managers per subsystem. | **[2]** | `APPC-CARD-SUB-MGR` (committed, `SeverityWarning`) — currently approximated as "total Managers > 3" since subsystem membership isn't modeled; see [Gaps](#gaps-and-remediation-plan). |
| SYS-2d | Strive for a golden ratio of Engines to Managers. | **[2]** | `SYS-CARD-RATIO` (committed, `SeverityWarning`, flags Engines > Managers) + `DH-CARD-ENGINES` (live tier, flags >3 Engines against the ch.4 2–3 smallest-set band). |
| SYS-2e | Allow ResourceAccess to access more than one Resource if necessary. | *(permission)* | Grants, not prohibits — `AppCPermission` in `DefaultCoverage()`, nothing to violate. |

### §3 Attributes

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| SYS-3a | Volatility should decrease top-down. | **[3]** | Folded into the D2/D3 attestation cluster in `the-method-system-design-standard-check` ("the layer ordering" cited as evidence). |
| SYS-3b | Reuse should increase top-down. | **[3]** | Same attestation cluster. |
| SYS-3c | Do not encapsulate changes to the nature of the business. | **[3]** | Attestation cluster — "the `.volatilities` 'nature of business' filter results" is the cited evidence. |
| SYS-3d | Managers should be almost expendable. | **[3]** | Attestation cluster — "the Manager-expendability walk." |
| SYS-3e | Design should be symmetric. | **[gap]** | Not in the attestation table (which stops at §3a–§3d) and not mechanically checkable. See [Gaps](#gaps-and-remediation-plan). |
| SYS-3f | Never use a public communication channel for internal system interactions. | **[gap]** | Same — not in the attestation table, no mechanical check. See [Gaps](#gaps-and-remediation-plan). |

### §4 Layers

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| SYS-4a | Avoid open architecture. | **[2]** | `APPC-ARCH-OPEN` (committed, `SeverityWarning`, layer-skip signal). |
| SYS-4b | Avoid semi-closed/semi-open architecture. | **[2]** | `APPC-ARCH-SEMI-OPEN` (committed, `SeverityWarning`, sideways-sync-between-peers signal). |
| SYS-4c-i | Do not call up. | **[2]** | `SYS-NOUP` (committed, `SeverityError`) + `DH-GRAPH-UPCALL` (live tier, `SeverityError`). |
| SYS-4c-ii | Do not call sideways (except queued Manager↔Manager). | **[2]** | `SYS-NOSIDE`/`SYS-5d` (committed, `SeverityError`) + `DH-GRAPH-SIDEWAYS-SYNC` (live tier, `SeverityError`). |
| SYS-4c-iii | Do not call more than one layer down. | **[2]** | `SYS-NOSKIP` (committed, `SeverityError`) + `DH-GRAPH-CLIENT-ENTRY` (live tier — Client may only enter at a Manager). |
| SYS-4c-iv | Resolve attempts at opening the architecture via queued/async. | **[gap]** | Prescriptive advice about *how to fix* a violation, not itself a checkable state — `human-judgment`, no dedicated check needed beyond the don't-violate rules above. |
| SYS-4d | Extend the system by implementing subsystems. | **[gap]** | Same category as SYS-2b — subsystems aren't a modeled construct yet. |

### §5 Interaction rules

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| SYS-5a | All components can call Utilities. | *(permission)* | Grants, not prohibits. Live tier still checks the inverse defect (`DH-GRAPH-UTIL-REACHABLE` — an unreachable Utility is dead architecture). |
| SYS-5b | Managers and Engines can call ResourceAccess. | *(permission)* | Grants, not prohibits. The prohibitions bounding it (`DH-GRAPH-ENGINE-IO`, `SYS-NOUP/NOSIDE/NOSKIP`) are the actual gates. |
| SYS-5c | Managers can call Engines. | *(permission)* | Grants, not prohibits. `DH-GRAPH-MANAGER-EMPTY` (live tier) checks the inverse defect: a Manager orchestrating *nothing* (no Engine or ResourceAccess edge). |
| SYS-5d | Managers can queue calls to another Manager. | **[2]** | Same rule as SYS-4c-ii (`SYS-NOSIDE`/`DH-GRAPH-SIDEWAYS-SYNC`) — a same-layer call is legal only when queued. |

### §6 Interaction don'ts (directives)

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| SYS-6a | Clients do not call multiple Managers in the same use case. | **[2]** | `APPC-INT-CLIENT-MULTI-MGR` (committed, `SeverityError`) + `DH-CHAIN-ENTRY-MANAGER` (live tier, per dynamic view). |
| SYS-6b | Managers do not queue calls to more than one Manager in the same use case. | **[2]** | `APPC-INT-MGR-MULTI-QUEUE` (committed, `SeverityError`) + `DH-CHAIN-QUEUED-MANAGER` (live tier). |
| SYS-6c | Engines do not receive queued calls. | **[2]** | `APPC-INT-ENGINE-NO-QUEUE` (committed, `SeverityError`) + `DH-GRAPH-QUEUED-TARGET` (live tier). |
| SYS-6d | ResourceAccess components do not receive queued calls. | **[2]** | `APPC-INT-RA-NO-QUEUE` (committed, `SeverityError`) + `DH-GRAPH-QUEUED-TARGET` (live tier, same rule covers both kinds). |
| SYS-6e | Clients do not publish events. | **[2]** | `APPC-INT-CLIENT-NO-PUB` (committed, `SeverityError`). |
| SYS-6f | Engines do not publish events. | **[2]** | `APPC-INT-ENGINE-NO-PUB` (committed, `SeverityError`). |
| SYS-6g | ResourceAccess components do not publish events. | **[2]** | `APPC-INT-RA-NO-PUB` (committed, `SeverityError`). |
| SYS-6h | Resources do not publish events. | **[2]** | `APPC-INT-RESOURCE-NO-PUB` (committed, `SeverityError`). |
| SYS-6i | Engines, ResourceAccess, and Resources do not subscribe to events. | **[2]** | `APPC-INT-NONMGR-NO-SUB` (committed, `SeverityError`). |

---

## Project Design Guidelines

### §1 General

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| PROJ-1a | Do not design a clock. | **[1]+[3]** | **[1]** `.network`/`.activityList` model duration as relative day-offsets (`DurationDays float64`, earliest/latest start/finish as offsets) — there is no absolute-calendar-date field to wire a clock into. **[3]** `the-method-project-design-standard-check` §A-1a walks it explicitly anyway. |
| PROJ-1b | Never design a project without an architecture that encapsulates the volatilities. | **[1]+[3]** | **[1]** Project Design cannot begin without a committed `.systemDesign` slot (Phase-2 skills read it as required input). **[3]** §A-1b confirms every coding activity maps to exactly one component. |
| PROJ-1c | Capture and verify planning assumptions. | **[3]** | §A-1c — `.planningAssumptions` slot existence + content walk. |
| PROJ-1d | Follow the design of project design. | **[3]** | §A-1d — canonical slot-commit order walk. |
| PROJ-1e | Design several options for the project (normal, compressed, subcritical at minimum). | **[1]+[3]** | **[1]** Four solution engines (normal/decompressed/subcritical/compressed) are separate committed slots the SDP-review assembly reads — there is no single-option code path. **[3]** §A-1e confirms all four exist with computed duration/cost. |
| PROJ-1f | Communicate with management in Optionality. | **[3]** | §A-1f — `.sdpReview` presents options side-by-side, not a lone recommendation. |
| PROJ-1g | Always go through SDP review before the main work starts. | **[1]+[3]** | **[1]** Phase-3/construction dispatch (`Begin → ExecuteNextActivity`) is gated on Phase-2 completion; the `.sdpReview` slot is the last Phase-2 slot in the canonical commit order. **[3]** §A-1g confirms the artifact's shape (audience, recommendation, options table). |

### §2 Staffing

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| PROJ-2a | Avoid multiple architects. | **[1]** | The agent roster has exactly one `system-architect` role; there is no mechanism to dispatch a second one. `the-method-project-design-standard-check` §B-2a is a secondary, redundant human check. |
| PROJ-2b | Have a core team in place at the beginning. | **[3]** | §B-2b — `.normalSolution` histogram walk. |
| PROJ-2c | Ask for only the lowest level of staffing required to progress unimpeded along the critical path. | **[3]** | §B-2c. |
| PROJ-2d | Always assign resources based on float. | **[3]** | §B-2d. |
| PROJ-2e | Ensure correct staffing distribution. | **[3]** | §B-2e. |
| PROJ-2f | Ensure a shallow S curve for the planned earned value. | **[3]** | §B-2f. |
| PROJ-2g | Always assign components to developers in a 1:1 ratio. | **[3]** | §B-2g. |
| PROJ-2h | Strive for task continuity. | **[3]** | §B-2h. |

### §3 Integration

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| PROJ-3a | Avoid mass integration points. | **[3]** | §C-3a. |
| PROJ-3b | Avoid integration at the end of the project. | **[3]** | §C-3b. |

### §4 Estimations

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| PROJ-4a | Do not overestimate. | **[3]** | §D-4a. |
| PROJ-4b | Do not underestimate. | **[3]** | §D-4b. |
| PROJ-4c | Strive for accuracy, not precision. | **[3]** | §D-4c. |
| PROJ-4d | Always use a quantum of five days in any activity estimation. | **[3]** *(gap: not schema-enforced)* | §D-4d walks it, but `DurationDays` is an unconstrained `float64` — nothing rejects a 3-day or 7-day activity. See [Gaps](#gaps-and-remediation-plan). |
| PROJ-4e | Estimate the project as a whole to validate or initiate project design. | **[3]** | §D-4e. |
| PROJ-4f | Reduce estimation uncertainty. | **[3]** | §D-4f. |
| PROJ-4g | When required, maintain correct estimation dialog. | **[3]** | §D-4g. |

### §5 Project network

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| PROJ-5a | Treat resource dependencies as dependencies. | **[3]** | §E-5a. |
| PROJ-5b | Verify all activities reside on a chain that starts and ends on a critical path. | **[3]** | §E-5b. |
| PROJ-5c | Verify all activities have a resource assigned to them. | **[3]** | §E-5c. |
| PROJ-5d | Avoid node diagrams. | **[1]** | The `.network` typed shape is arrow-on-edge (`activity = edge`) — a node-diagram encoding isn't representable in the schema. §E-5d is a redundant human confirmation. |
| PROJ-5e | Prefer arrow diagrams. | **[1]** | Same structural fact as 5d. |
| PROJ-5f | Avoid god activities. | **[3]** | §E-5f. |
| PROJ-5g | Break large projects into a network of networks. | **[3]** *(gap: no sub-network construct)* | §E-5g notes "N/A" absent a sub-network mechanism — same modeling gap as SYS-2b/4d. |
| PROJ-5h | Treat near-critical chains as critical chains. | **[3]** | §E-5h, and re-checked weekly by `the-method-project-tracking` (App C §5.6). |
| PROJ-5i | Strive for cyclomatic complexity as low as 10 to 12. | **[3]** | §E-5i (manual computation: edges − nodes + 2). |
| PROJ-5j | Design by layers to reduce complexity. | **[3]** | §E-5j. |

### §6 Time and cost

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| PROJ-6a | Accelerate first by quick-and-clean practices rather than compression. | **[3]** | §F-6a. |
| PROJ-6b | Never commit to a project in the death zone. | **[2, soft]+[3]** | `sdpOptionInBand` (`assemblesdpreview.go`) excludes any option whose composite risk falls outside `[0.30, 0.75]` from being the *recommendation* — but does not block the architect from committing an out-of-band option (see caveat above). §F-6b's human walk is therefore the actual backstop, not a redundant confirmation. |
| PROJ-6c | Compress with parallel work rather than top resources. | **[3]** | §F-6c. |
| PROJ-6d | Compress with top resources carefully and judiciously. | **[3]** | §F-6d. |
| PROJ-6e | Avoid compression higher than 30%. | **[2, soft]+[3]** | `maxCompression = 0.30` in `sdpOptionInBand` — a compressed option >30% shorter than normal is excluded from the recommendation, but not from being committed (see caveat above). §F-6e's human walk is the actual backstop. |
| PROJ-6f | Avoid projects with efficiency higher than 25%. | **[3]** *(gap: not mechanically bounded)* | §F-6f is a manual computation; no engine-side efficiency guard exists yet. See [Gaps](#gaps-and-remediation-plan). |
| PROJ-6g | Compress the project even if unlikely to be pursued. | **[1]** | The compressed-solution engine runs unconditionally as one of the four options, regardless of which the architect expects management to choose. |

### §7 Risk

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| PROJ-7a | Customize the ranges of criticality risk to your project. | **[3]** | §G-7a. |
| PROJ-7b | Adjust float outliers with activity risk. | **[3]** | §G-7b. |
| PROJ-7c | Decompress the normal solution past the tipping point on the risk curve. | **[1]+[3]** | **[1]** The decompressed-solution is a separate committed engine/slot, always produced. **[3]** §G-7c confirms risk dropped via duration extension without cutting staff. |
| PROJ-7c-i | Target decompression to 0.5 risk. | **[3]** *(gap: not mechanically targeted)* | §G-7c-i is a manual read of the computed risk number; no engine-side target-seeking toward 0.5. |
| PROJ-7c-ii | Value the tipping point over a specific risk number. | **[3]** | §G-7c-ii. |
| PROJ-7d | Do not over-decompress. | **[3]** *(gap: no lower bound enforced)* | §G-7d checks risk doesn't fall below ~0.3, but only `sdpOptionInBand`'s `[0.30, 0.75]` band actually *excludes* out-of-band options from recommendation — and that only fires at SDP-assembly time, not at decompressed-solution authoring time. |
| PROJ-7e | Decompress design-by-layers solutions, perhaps aggressively. | **[3]** | §G-7e. |
| PROJ-7f | Keep normal solutions at less than 0.7 risk. | **[3]** *(gap: not mechanically bounded)* | §G-7f is a manual walk; `sdpOptionInBand`'s band is `[0.30, 0.75]`, looser than the 0.7 ceiling this item asks for specifically on the *normal* option. |
| PROJ-7g | Avoid risk lower than 0.3. | **[2, soft]** | `riskOverSafe = 0.30` in `sdpOptionInBand` — excluded from recommendation, not from commit (see caveat above). |
| PROJ-7h | Avoid risk higher than 0.75. | **[2, soft]** | `riskTooRisky = 0.75` in `sdpOptionInBand` — excluded from recommendation, not from commit (see caveat above). |
| PROJ-7i | Avoid options riskier or safer than the risk crossover points. | **[2, soft]** | Same `sdpOptionInBand` exclusion band; `recommendOption`'s fallback message explicitly names "App C risk-crossover exclusions applied." Same commit-time caveat applies. |

---

## Project Tracking Guidelines

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| TRACK-1 | Adopt binary exit criteria for internal phases of an activity. | **[3]** | `the-method-project-tracking` §9 item 1, weekly, non-optional. |
| TRACK-2 | Assign consistent phase weights across all activities. | **[3]** | §9 item 2. |
| TRACK-3 | Track progress and effort on a weekly basis. | **[3]** | §9 item 3 — "never less than weekly" is explicit in the skill's exit criteria. |
| TRACK-4 | Never base progress reports on features. | **[3] non-waivable** | §9 item 4 — explicitly called out as non-waivable in the skill. |
| TRACK-5 | Always base progress reports on integration points. | **[3] non-waivable** | §9 item 5 — non-waivable. |
| TRACK-6 | Track the float of near-critical chains. | **[3]** | §9 item 6, plus the dedicated "Near-critical chain float" table required in every weekly log. |

---

## Service Contract Design Guidelines

| Ref | Guideline | Enforcement | Mechanism |
|---|---|---|---|
| SVC-1 | Design reusable service contracts. | **[3]** | `the-method-service-contract` Step 6 "reuse review" + §7 item 1. |
| SVC-2a | Avoid contracts with a single operation. | **[2]+[3]** | **[2]** `APPC-SVC-SINGLE` (committed, `SeverityWarning`). **[3]** Step 3 sizing table + §7 item 2a. |
| SVC-2b | Strive for 3–5 operations per contract. | **[2]+[3]** | **[2]** `APPC-SVC-STRIVE` (committed, `SeverityInfo`). **[3]** Step 3 + §7 item 2b. |
| SVC-2c | Avoid contracts with more than 12 operations. | **[2]+[3]** | **[2]** `APPC-SVC-AVOID-12` (committed, `SeverityWarning`) + `DH-CONTRACT-OPCOUNT-MAX` (live tier, `SeverityWarning`). **[3]** Step 3 + §7 item 2c. |
| SVC-2d | Reject contracts with 20+ operations. | **[2]+[3] non-waivable** | **[2]** `APPC-SVC-REJECT-20` (committed, `SeverityError`, directive) + `DH-CONTRACT-OPCOUNT-REJECT` (live tier, `SeverityError`) — hard-blocks at both authoring and render-on-read. **[3]** Step 3 marks it non-waivable explicitly. |
| SVC-3 | Avoid property-like operations. | **[3]** | Step 4 + §7 item 3. No mechanical check — a `GetX`/`SetX` naming heuristic would be cheap to add; see [Gaps](#gaps-and-remediation-plan). |
| SVC-4 | Limit the number of contracts per service to 1 or 2. | **[3]** | Step 4 + §7 item 4. `DH-CONTRACT-FACET` (live tier) mechanically enforces *facet layer consistency* but not the 1-or-2 *count* — a related but distinct check; see [Gaps](#gaps-and-remediation-plan). |
| SVC-5 | Avoid junior hand-offs. | **[1]+[3] non-waivable** | **[1]** The `junior-developer` agent's MCP tool grant does **not** include `recordServiceContract` — it is structurally incapable of authoring a contract, regardless of what any prompt asks it to do. Only `senior-developer` and `system-architect` carry that tool. **[3]** Step 7 item 5 is the redundant human confirmation, marked non-waivable. |
| SVC-6 | Have only the architect or competent senior developers design contracts. | **[1]+[3] non-waivable** | Same structural fact as SVC-5 — the agent roster itself is the enforcement (`system-architect`, `senior-developer` only). §7 item 6, non-waivable. |

---

## Additional validations beyond Appendix C

Archistrator enforces a substantial set of mechanical rules that have **no direct Appendix C item** — either founder extensions of a book guideline, or cross-artifact consistency checks the book doesn't need because it doesn't have a git-as-DB typed state to keep consistent:

- **`CC-*` / `CUC-ACTOR-REQUIRED` call-chain correspondence family** (11 rules, both tiers, `SeverityError`) — verifies a use case's dynamic view doesn't just *exist* (SYS-1e's literal ask) but actually *walks* the use case's activity diagram end-to-end: every branch/decision resolves (`CC-DECIDED-BY`), every path is connected (`CC-PATH-CONNECTED`), trigger kind matches entry-node shape (`CC-TRIGGER-EVENT`), actors only touch the system through a Client (`CC-ACTOR-EDGE`), swim-lane actors are actually realized (`CC-ACTOR-LANE`). This is far more rigorous than "has a dynamic view."
- **`USECASE-DYNAMIC-MISSING`** — founder extension requiring *every* use case (core and non-core variation alike) to carry a dynamic view, not just core ones (SYS-1e is core-only).
- **`SYSTEM-LAYER-DEGENERATE`** — catches a drafting-agent bug class: a schema default silently collapsing an omitted layer field to `client`, producing an architecture that passes every layer rule vacuously.
- **`DH-UC-ESSENCE-MISSING`** — every use case classified `core` must carry a non-empty `essenceRationale` arguing *why* it's core (ch. 3 "core use cases are the essence of the business").
- **`DH-OBJ-RESOLVE` / `DH-OBJ-COVERAGE`** — bidirectional objective↔architecture traceability (ch. 5): every objective reference resolves, and every objective is referenced by ≥1 operational decision.
- **`DH-VOL-TRACE`** — every volatility's `traces` field resolves to a committed Required-Behavior id.
- **`DH-CONTRACT-DEADOP`** — the same operation name published by two contracts of one facet group; signature-sensitive severity (exact duplicate = Error, name collision with distinct signature = Warning).
- **`DV-STATIC-COVERAGE` / `DV-REL-COVERAGE`** (committed tier only) — every static architecture component/relationship must appear in ≥1 dynamic view; the inverse of SYS-1e (dynamic views must correspond to the static model, not just the other way round).
- **`GLOSS-FOURQ`, `SR-ID-UNIQUE`, `OPC-TOPIC-COVERAGE`, `SYS-RA-ORPHAN`, `SYS-REL-DUP`, `UC-GUARD-LABEL`, `UC-VARIATION-REF`, `VOL-AXIS-EXPLICIT`, `STD-STATUS-EXPLICIT`, `STD-FAIL-OPEN`** — state-validation twins guarding cross-artifact id/reference integrity (glossary four-questions completeness, requirement id uniqueness, objective-topic coverage, orphaned ResourceAccess, duplicate relationships, etc.) with no Appendix C analog — the book doesn't need them because it has no typed git-as-DB to drift.
- **`DEP-*` family** (10 rules) — deployment-model/operational-concepts consistency (container references, profile completeness, test-vs-prod component-set identity) — governs the Deployment & Operations Model, which is a founder addition beyond Löwy's core Method.
- **`STP-*` family** (11 rules) — System Test Plan consistency against the committed contracts and call chains (op existence, arg name/type match, walk legality) — supports `the-method-testing` doctrine, no direct Appendix C item.
- **`ALIGN-*` family** (`align.go`) — construction-code-vs-model drift: a package missing/extra relative to the committed architecture, a component's code package mismatched to its modeled layer.
- **File-layout / layer-boundary gates** — Go side: `TestFileLayout` (zero-waiver gate, one handwritten impl file per component, per the layer file-layout standard) and the `errcheck`/`govet`/`ineffassign`/`staticcheck`/`unused`/`gochecksumtype`/`gosec`/`revive`/`gocritic`/`gocyclo` lint suite. TypeScript side: the `eslint-boundaries` layer-DAG gate (routes→components→hooks→api+contracts/utilities). Neither has an Appendix C item — they're the code-layer enforcement of the same closed-architecture discipline SYS-4/§6 apply at the model layer.
- **Waiver/attestation re-opening on amendment** — a structural property of the whole system: because waivers and attestations live *on* the artifact they qualify (not a separate ledger), any amendment to that artifact marks them stale and forces re-affirmation. This is itself a design property with no Appendix C counterpart but is what keeps §-item judgments (D1–D4, §3a–d, cardinality waivers) from silently rotting.

---

## Gaps and remediation plan

Ordered roughly by how cheap/valuable the fix is.

1. **§3e "design symmetric" and §3f "no public channels for internal interactions" are missing from the attestation table.** `the-method-system-design-standard-check`'s attestation table (the "Attestations" section) stops at §3a–§3d. These two items are real judgment calls an architect can and should make, they're just not currently prompted for.
   - *Fix:* add two rows to the attestation table in `.claude/skills/the-method-system-design-standard-check/SKILL.md` — "§3e: design symmetric" (evidence: compare analogous use-case chains for structural symmetry) and "§3f: no public channel for internal interactions" (evidence: every Manager↔Manager / Manager↔Engine / Manager↔ResourceAccess edge uses the modeled internal call/queue/event modes, never an externally-addressable channel). Small, self-contained skill edit.

2. **PROJ-4d (5-day quantum) is walked by a skill but not schema-enforced.** `DurationDays` is an unconstrained `float64` in `.activityList`/`.network`; nothing rejects a 3-day or 7-day activity.
   - *Fix option A (cheap):* a live-tier `DH-*` rule (or a committed-tier `SYS-*`/`APPC-*` rule) flagging any `DurationDays` not a positive multiple of 5 — advisory `SeverityWarning`, matching the guideline (not directive) status.
   - *Fix option B (structural, bigger lift):* change the wire type to an integer count of 5-day quanta. Bigger blast radius (touches every consumer of `DurationDays`); not recommended unless the advisory warning proves insufficient in practice.

3. **PROJ-6f (efficiency ≤ 25%) has no mechanical bound**, unlike its risk/compression siblings which `sdpOptionInBand` already excludes.
   - *Fix:* extend `sdpOptionInBand` (`assemblesdpreview.go`) with an efficiency computation (critical-path work ÷ total work × resources) and exclude options above 0.25, mirroring the existing `riskTooRisky`/`riskOverSafe`/`maxCompression` pattern. Needs the resource-hours data already present in `.normalSolution`'s resource assignment.

4. **PROJ-7f (normal solution risk < 0.7) is looser than the general `[0.30, 0.75]` band `sdpOptionInBand` applies to every option.** The book asks for a *tighter* ceiling specifically on the normal option (0.7, not 0.75).
   - *Fix:* add a normal-solution-specific check (in the same function or the standard-check skill) so an in-band-but-≥0.7-risk normal solution triggers redesign rather than only WAIVED/FAIL by human walk.

5. **PROJ-7c-i (target decompression to 0.5) and PROJ-7d (don't over-decompress below ~0.3) are read manually, not target-seeking or bounded.** Lower priority than the ones above — the book itself treats the tipping point as a target to aim for by iteration, not a hard bound like the risk band, so a human-in-the-loop check may be the right permanent posture here rather than a gap to close.

6. **SVC-3 (no property-like operations) has no mechanical check.** A cheap `GetX`/`SetX` naming heuristic (flag any operation name matching `^(Get|Set)[A-Z]`) would catch the common case; won't catch every "shape leaks state" violation (the book's real concern), so keep the skill's Step 4 judgment as the primary control and treat a naming-heuristic rule as a supplementary early-warning, not a substitute.

7. **SVC-4 (≤2 contracts per service) has no mechanical count check**, even though the closely related facet-layer-consistency rule (`DH-CONTRACT-FACET`) already exists and shares the same facet-group grouping logic `DH-CONTRACT-DEADOP` uses.
   - *Fix:* a small addition to `contractFindings` in `designhealthengine.go` — group contracts by owning component (same grouping `deadOpFindings` already builds) and flag groups of size >2, `SeverityWarning`.

8. **`DefaultCoverage()` (the platform's canonical coverage matrix) is stale relative to `designhealth`'s newer live-tier rules and to the `assemblesdpreview.go` risk/compression guards.** It only classifies System Design + Service Contract items, and even within System Design it predates several rules added in the 2026-07-30/07-31/08-01 call-chain-realization and rollout work (`DH-UC-ESSENCE-MISSING`, `DH-OBJ-*`, `DH-COMP-NO-VOLATILITY`, the full `CC-*`/`CUC-*` family beyond the original `ARCH-CHAINCOV`). It has no Project Design section reflecting the `assemblesdpreview.go` numeric guards documented above at all — Project Design items are uniformly `human-judgment` in the matrix even where mechanical enforcement now exists.
   - *Fix:* this is a platform-repo change (`archistrator-platform/framework-go/methodcheck/coverage.go`), not an archistrator-repo one. Lower priority than the app-side fixes above since the matrix is a self-consistency tool for the platform's own emitter registry, not something archistrator's construction flow reads at runtime — but worth a follow-up pass so the matrix stays trustworthy as a reference.

9. **SYS-2b/SYS-4d (subsystem guidelines) and PROJ-5g (network of networks) have no mechanical support** because subsystems aren't yet a modeled construct in `.systemDesign`/`.network`. This is a genuine, structural absence rather than a missed check — closing it means designing a `Subsystem` typed construct first, which is a modeling decision, not a validation-rule addition. Out of scope for a quick fix; flagged for awareness only.

10. **`the-method-project-design-standard-check`'s Input section still references a committed `.standardCheck` slot** ("the committed `.standardCheck` slot (the Phase-1 design standard check; must already be clean)"), but `the-method-system-design-standard-check` retired that slot entirely (waivers/attestations now live on `.systemDesign`/`.volatilities` directly, "There is no longer a `.standardCheck` slot"). This is a doc-drift inconsistency between two skills, not a compliance gap — noted here so it doesn't get mistaken for one; worth a one-line fix in `the-method-project-design-standard-check/SKILL.md`'s Input section next time either skill is touched.

11. **The `sdpOptionInBand` risk/compression exclusion band (PROJ-6b/6e/7g/7h/7i) only steers the *recommendation* — it does not block the architect from committing an out-of-band option.** Verified directly: `sdpCommit` (`assemblesdpreview.go:147-179`) validates the chosen `OptionID` is one of the assembled options (`optionInReview`) but never re-checks `sdpOptionInBand` on the chosen option before staging/committing. So today, an architect *can* commit a >30%-compressed or >0.75-risk option — the mechanical guard silently steers what gets *recommended*, and `the-method-project-design-standard-check`'s human walk is the only thing actually standing between the architect and a death-zone commit.
    - *Fix:* add an explicit re-check in `sdpCommit` — if the chosen option fails `sdpOptionInBand`, require `sig` to carry an override justification (mirroring the waiver-justification pattern used elsewhere), or reject outright and force the architect to only ever commit an in-band option, with §F-6b/7g/7h treated as the directive-strength "never commit to the death zone" language implies. This is arguably higher priority than items 2–7 above since it's the one place a *directive*-level guideline ("never commit to a project in the death zone") currently has a softer mechanical backstop than its guideline-level siblings.
