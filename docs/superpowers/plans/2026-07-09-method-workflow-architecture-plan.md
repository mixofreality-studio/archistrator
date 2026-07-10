# The Method Workflow & Architecture Plan — Be The Method

**Date:** 2026-07-09 · **Companion to:** `2026-07-09-method-fidelity-assessment-and-plan.md` (the fidelity
plan). That plan is the artifact/validator/codegen kernel (workstreams A–F) and stands unchanged. This
document is the step-back the founder asked for on the same day: does archistrator's **control flow** match
the book's **workflows**, and is archistrator's **own architecture** a faithful Method design? Basis: fresh
process-grade extraction of Righting Software (ch. 1–14 + App A/B/C, with verbatim citations), a
reverse-engineering of the implemented orchestration state machines (file:line), and an architecture-level
audit of the dogfood state. Workstream letters continue the fidelity plan: **G** (orchestration), **H**
(self-architecture), **I** (design experience).

---

## 0. Verdict

**The book's workflows are loops. Archistrator's are rails.** Löwy's system design is a fixed-point
iteration — "the act of looking for areas of volatility … is an iterative process interleaved with the
factoring of the design itself" (ch. 2), with an explicit validation→re-decomposition back-edge ("go back
to the drawing board", ch. 5). His project design is a **tree of solutions** (ch. 11 investigated fifteen
to present four), where every staffing pass mutates the network ("resource dependencies are dependencies")
and every iteration is gated by diagnostics (staffing-chart shape, shallow-S EV, efficiency 15–25%,
complexity ~10–12, risk bands). His construction is a weekly control loop whose replan trigger re-enters
project design, with the **unchosen SDP options kept live as pivot targets** (App A). Archistrator
implements linear predecessor-gated pipelines with an amendment back-edge bolted on, a pump that picks
activities by declaration order, and a scope-change chain that parks instead of re-entering Phase 2.

Three root findings:

1. **Phase 3 never consumes Phase 2.** The pump schedules by activity-list declaration order — floats,
   critical path, and the chosen option's resource assignments are never read (`CN/eligibility.go:64-69`).
   The entire economic payload of project design is produced, reviewed, committed … and ignored.
2. **`.phase` is decorative and the boundaries leak.** No verb reads `.phase` as a precondition. Phase-2
   drafting unlocks with Phase 1 unsealed; construction pumps with the SDP uncommitted; the parent's
   auto-seal skips the STD-FAIL-OPEN and stale-basis gates that the manual verb enforces.
3. **Archistrator's own design neither obeys the doctrine nor describes the implementation.** The design
   rail is one volatility implemented twice (SDM/PDM ~2k-line near-clones); the volatile half of
   estimation (the AI rate card) lives in a Manager while the Engine holds fixed math; the Method's own
   rules — the deepest business logic in the system — have **no component** (methodcheck lives in a client
   binary; the correct `artifactValidationEngine` contract exists as an orphan). Meanwhile the real state
   machines (recovery lattice, ContinueAsNew cascade, amendment back-edges) grew far beyond anything in
   the committed dynamic views — the flagship `drive-system-design` view omits the review rail it exists
   to showcase.

The bold move: **make the loops first-class.** The fidelity plan makes the book's *numbers* into schema +
validators; this plan makes the book's *control flow* into typed state machines and makes archistrator's
own architecture the reference Method design — because archistrator's architecture is the first one every
customer will read.

---

## 1. The book's workflows — the normative process spec

Condensed from the full extraction (kept in this plan's basis notes); these are the target state machines.
`[LOOP]`/`[GATE]`/`[DECIDE]` mark control-flow features an orchestrator must encode.

**W1. System design** (ch. 2–5): requirements → business alignment (vision→objectives→mission, strictly
ordered, bidirectional traceability GATE) → requirements analysis with **two parallel outputs** —
volatilities (with scrubbing) and core use cases (2–6, "often via some iterative process") → decomposition
⇄ structure/layering ⇄ **validation** [LOOP: volatility-hunting is *interleaved* with factoring;
propose-and-inspect with retraction is legitimate; validation failure re-enters decomposition]. DONE GATE:
"Once you can produce an interaction between your services for each **core** use case, you have produced a
valid design" (ch. 4) + smallest-set convergence. Design proper takes days ("with practice … a few hours");
the long pole is analysis. Then project design **back-to-back** as "a single continuous design effort."

**W2. Project design** (ch. 7–13, ch. 14 "Design of Project Design"): planning assumptions (each scenario
FORKS a design) → activity list (peer-reviewed) → estimation on **two tracks** (per-activity dialog +
**broadband overall estimate** kept aside as a later cross-check GATE) → network from call chains [LOOP:
ch. 13 needed "multiple passes" and even fixed the call chains — a legal back-edge into Phase 1 artifacts]
→ milestones incl. **M0 = SDP review** ("none of the construction activities should start before the SDP
review") → normal solution [LOOP: iterate staffing↓ until an activity goes critical; each pass adds
resource dependencies and **rewrites the network**; diagnostics gate every iteration] → subcritical
(designed to be rejected) → compression [LOOP: compress the previously-compressed; levers in order:
practices → better resources on critical path → parallel work via **enabling activities: contract design,
simulators, + mandatory repeat-integration activities**; STOP list incl. >30%] → time–cost curve + death
zone → risk (criticality + activity, Fibonacci arbitration) → decompression [LOOP: float only — "never pad
estimates, never cut staff"; target the tipping point ~0.5] → rebuild curves, exclusion zones (>0.75, <0.3)
→ SDP. Roles: architect designs with PM *input*; abstract resources only; PM assigns real people
post-decision. The meta-process is self-similar: project design is itself a 22-activity mini-project.

**W3. Construction + tracking** (ch. 14, App A): once — hand-off DECIDE (senior / senior-as-junior-architect
/ junior) + per-activity phase lifecycle with **binary exit criteria** and weights; every review GATE fails
back to "repeat the preceding task." Weekly loop: collect binary exits + spend → EV/effort → **regression
projections** ("drive your car looking at where the car is going to be") → classify the four variance
signatures (well / underestimating / resource-leak / overestimating) → corrective action — and **any
non-trivial corrective action ⇒ redesign the project**, pivoting among the *retained* unchosen SDP options.
Scope creep: "politely ask to get back to them" — pause → redesign → present numbers → management
re-decides. Adding people is forbidden except "near the project's origin" (= pivot to a pre-designed
compressed option). PM tracks; architect redesigns; they close the loop together.

**W4. SDP review**: M0, end of the fuzzy front end (15–25% of project duration). "Three options is ideal…
Four is fine as long as at least one is an obvious mistake" (the subcritical slot). Round everything
**except risk**. Management chooses; one option is always Kill; **sign-off is the project's life-insurance
policy** conditioned on staying inside the plan's parameters.

**W5. Service contracts** (App B, App A Fig A-1): per-component, **inside** the construction activity
(SRS → STP → exploratory construction for insight → detailed design → architect review → construction),
*pulled forward only as a compression enabling-activity*. Who designs = the hand-off decision; every design
"reviewed with the architect before hand-off." Contract metrics are evaluation, never validation.

---

## 2. Gap register

Orchestration gaps (**O**) from the implementation map; architecture findings (**F**) from the dogfood
audit. Fidelity-plan §2 defects are not repeated.

**O1. Pump ignores project design.** Eligibility = declaration order + name tie-break; no float, no
critical path, no chosen-option assignments (`CN/eligibility.go:20-84`).
**O2. `.phase` unread; leaks both ways.** planningAssumptions has no Phase-1-seal predecessor
(`PD/projectdesignmanager.go:186-189`); pump requires only network+activityList committed
(`CN/eligibility.go:21-34`) — construction can start before the SDP exists. Direct violation of M0.
**O3. Auto-seal ≠ manual seal.** Parent's `runPhaseAdvance` (`SD/workflow.go:434`) checks only
"all committed"; STD-FAIL-OPEN and stale-basis refusals live solely on the manual façades.
**O4. Pause doesn't pause.** `PauseProject` signals `{proj}:construction`, but the cascading pump runs as
`{proj}:nextActivity:{tick}` and listens on its own id (`CN/signals.go:51` vs `CN/workflow.go:349`).
**O5. Silent starvation.** Unresolvable activity→component mapping is skipped with a log warning; the pump
reports quiescent — indistinguishable from "done" (`CN/eligibility.go:77-82`).
**O6. Scope-change chain severed.** `replan-under-scope-change` ends at "record replan-required";
`flagVariances` returns nil; nothing re-enters ProjectDesignManager (`CN/constructionmanager.go:187-192`).
**O7. Auto-merged "+1".** With zero ReviewPolicy, `finalizeActivity` self-relays the architecture approval
and merges with no human — while head-state records `ArchApproved` as a sign-off (fidelity D1 fixes the
default; the record semantics need fixing with it).
**O8. `SubmitPhaseDecision` silently discards** decisions for non-gated phases (no F19-style precheck).
**O9. Twin drift.** SDM/PDM near-clones already diverge (Describe-first defense present in one, absent in
the other).
**O10. No broadband estimate anywhere.** The book's overall-estimation cross-check (W2) — "at least one of
these numbers is wrong, and you must investigate" (ch. 7) — has no slot, no skill, no gate.
**O11. Views can't derive the control flow.** The implemented recovery lattice, cascade, and back-edges
exist only in workflow code + QA-incident comments.

**F1. Design rail = one volatility, two Managers** (SDM+PDM share 7 near-identical ops; phase-split is
decomposition by time — the classic functional-decomposition tell).
**F2. SystemDesignManager god-contract**: 13 ops (over App B's 12) mixing project-admin CRUD setters
(`SetResearchInput`, `SetOperatingModel`), the rail, and tracking reads.
**F3. Estimation inverted**: two Engines for one volatility, holding *non-volatile* CPM/EV math, while the
genuinely volatile rate card sits in ProjectDesignManager (`projectdesign/airates.go`).
**F4. Declared volatilities not encapsulated**: Temporal SDK in all five Manager packages;
`mainBranch`+PR mechanics in three Managers; rail-off is a Manager if-branch — yet "Durable Execution
Runtime" and "Source Control Target" are slot-3 volatilities. Design-for-your-competitors fails.
**F5. GH Actions leaks through the CPA contract**: `PipelineSpec` requires `WorkflowFile` + raw
`DispatchInputs` even though the impl already takes these as construction-time config.
**F6. ReviewEngine covers ⅓ of its volatility**: proposals only; gating/mayAmend in ConstructionManager;
design-phase reviews never touch it.
**F7/F8. Flagship views wrong**: `drive-system-design` omits source-control-access, validation, and the
iterate/reject loop; `execute-a-construction-activity` shows a fully autonomous pipeline — no client, no
`SubmitPhaseDecision`, no phase lifecycle — the exact opposite of ratified goal 4.
**F9. Weekly tracking routed through SystemDesignManager** (compute-at-read in `SD/catalog.go`), no
persisted series, no InterventionEngine in the chain.
**F10. Validation homeless** (methodcheck in a client-tier binary; orphan `artifactValidationEngine`
contract is the un-promoted correct design; zero views contain a validation step).
**F11. Drafting strategy is Engine work in Managers**: 580 lines of per-kind prompt business logic in
`prompts.go`×2; orphan `systemDesignEngine` contract again the missing component.
**F12. Missing volatilities**: Method-artifact **schema version** (the product literally versions other
people's architectures — no row, no seam, no owner), **construction venue** (multi-venue proposal smears
it across three seams), **agent runtime/model** (fires independently of "pipeline target").
**F13. workItemAccess** has a component but no contract — a volatility with a box and no seam.

---

## 3. Workstream G — Orchestrate the Loops (workflows match the book)

*Make the book's control flow typed state, enforced in Managers — not prose. Depends on fidelity A1
(typed floats, per-option network state, componentId, ReviewPolicy) — same schema, consumed here.*

- **G1. Phase-boundary integrity.** `.phase` becomes a real precondition on every draft/pump verb.
  Phase-2 drafting requires Phase 1 sealed; **pump eligibility requires `.sdpReview` committed + option
  bound + phase == construction** (M0: "none of the construction activities should start before the SDP
  review"). Auto-seal calls the same gate function as the manual verbs (kills O3) — one seal path,
  standard-check FAILs and stale basis always block. The only book-grounded early start is ch. 14's
  concession that "it is possible to design a few services up front, but not all of them" — so the recorded
  founder override is scoped to **pre-SDP detailed-design/contract activities for a bounded set of
  services** (exactly the enabling-activity set of an I3 candidate compressed option). Construction-phase
  activities proper get no early-start path.
- **G2. The pump consumes the project design.** Scheduling reads the **chosen option's** resource-assigned
  network: dispatch by ascending total float (critical path first), concurrency capped by the option's
  staffing level per instant, activity→component via typed `componentId` (fidelity A1 kills the
  title-substring join). Unresolvable or starved activities become a **surfaced operator finding**, never a
  silent skip (O5). This is the single highest-leverage change in the plan: it is the moment Phase 2
  starts steering Phase 3.
- **G3. The design loop, first-class.** The rail keeps its draft→critique→review→commit spine but gains the
  book's back-edges as typed transitions: validation-fail re-entry (ValidationEngine findings block staging,
  route to redraft with findings as feedback), and the existing amendment/StaleBasis machinery documented
  and rendered as *the* ch. 2 factoring loop (it already is one — name it, view it, gate it). Convergence
  gate = standard check + all core use cases validated.
- **G4. Project design as a solution tree.** Persist every solution iteration's network state (fidelity A1
  Solution schema) with provenance and its diagnostic verdicts — the diagnostics run **per iteration** as
  loop gates (staffing shape, shallow-S, efficiency 15–25%, complexity ~10–12, throughput plausibility),
  not only at the end-of-phase standard check. Add the **broadband estimate** as a typed artifact +
  cross-validation gate against the detailed design (O10). Planning-assumption scenarios fork sibling
  trees. Design-by-layers is the documented pivot when complexity diagnoses demand it (ch. 13 precedent).
- **G5. Close the tracking control loop** (absorbs fidelity E4 and makes it architectural). Weekly (or
  per-phase-exit — agents give perfect data) EV points persisted by ConstructionManager; `flagVariances`
  implements App A's four signatures; non-trivial corrective action **event-triggers ProjectDesignManager
  re-entry**: pause dependents → fresh options (calibrated to measured agent throughput) → SDP re-review →
  management re-decides → pump resumes on the new option. The **unchosen SDP options stay addressable** as
  pivot targets — including the time-boxed "add-people near the origin" pivot to the pre-designed
  compressed option. This closes O6 with components that all already exist.
- **G6. Control-flow defect fixes.** Pause signal routed to the live cascade id + per-activity pause (O4;
  fidelity D3); `SubmitPhaseDecision` precheck (O8); `ArchApproved` recorded only when a human or a
  policy-authorized agent reviewer actually approved (O7); unify the SDM/PDM recovery lattice as part of H1
  (O9).

## 4. Workstream H — Redesign Thyself (archistrator's own architecture)

*Run archistrator's own improvement through archistrator: these land as **amendments to the dogfood
project.json via the design rail** — slot-5 amendment sessions, StaleBasis cascade, review, commit. Be the
Method on the Method.*

- **H1. One DesignManager; promote the orphans.** Collapse SDM+PDM into a single DesignManager
  parameterized by artifact kind (the rail is one sequence volatility — F1); project-admin CRUD leaves the
  Manager surface (F2). Extract **DraftingEngine** (per-kind prompt strategy — realizes the orphan
  `systemDesignEngine` contract, F11) and **ValidationEngine** (methodcheck as a real component — realizes
  the orphan `artifactValidationEngine` contract, F10), both in the design-rail call chain and views. The
  delicious part: the orphan contracts flagged as state defects in the fidelity plan were the *correct
  design* — promote them instead of deleting them. Binding conditions (architect review, 2026-07-10):
  (1) the merged Manager proves **≤12 ops** — the F2 relocation of project-admin CRUD lands in the same
  amendment, and passing fidelity-B2 contractcheck is the merge's acceptance criterion (sequence B2 with or
  before H1: the validator judging its own architect is the product demo); (2) the orphan *components* are
  promoted but their contracts are **redesigned kind-parameterized** — `ComposeDraftBrief(kind, basis)` /
  `ComposeCritiqueBrief(kind, draft)` and `Validate(kind, model) → Findings`, 2–4 ops each — never the
  per-kind 8/17-op shapes (App B: a contract that changes every time a kind is added is "the hallmark of
  bad design").
- **H2. One EstimationEngine, owning the rate card.** Merge OperationEstimationEngine as a strategy; move
  `airates.go` (rates, model roster, role mapping) into the Engine; CPM/EV math becomes internal
  calculators (F3). Fixes the volatile/fixed inversion and the engine census in one refactor.
- **H3. Substrate honesty** (F4/F5/F12/F13). Founder ruling per declared volatility: **Temporal** — either
  strike "Durable Execution Runtime" to an operational-concepts *decision* (recommended: the fwm framework
  already blunts it; pretending five Temporal-native Managers encapsulate it is the worst of both), or
  fund the full encapsulation. **Source control** — one owner for `mainBranch` + branch/PR composition
  behind SourceControlAccess with a rail-on/off strategy. **CPA contract** — delete `WorkflowFile`, replace
  `DispatchInputs` with a typed construction-context payload. **Volatilities added**: schema version (owner:
  ProjectStateAccess, with an explicit versioning strategy), construction venue (one owner, aligned with
  the multi-venue proposal), agent runtime/model (seam: DraftingEngine + dispatch payload). Contract for
  `workItemAccess` or delete the box.
- **H4. Views tell the truth, and a gate keeps them true.** Redraw `drive-system-design` (rail edges,
  validation edge, labeled reject/redraft loop), `execute-a-construction-activity` (client +
  `SubmitPhaseDecision` gate + phase lifecycle), `track-weekly-project-progress` (ConstructionManager +
  InterventionEngine), `replan-under-scope-change` (the G5 chain through ProjectDesignManager and back).
  Then the drift gate: extend methodcheck so every Manager workflow transition class (dispatch, gate,
  back-edge, cascade) must be derivable from some dynamic-view edge — the O11 class becomes un-regressable
  (this is fidelity goal 3's static↔dynamic consistency, applied to archistrator itself).
- **H5. Engine census: explicit waiver, not subsystem framing** (revised per architect review,
  2026-07-10 — the original subsystem proposal was **rejected as cosmetic**: 4 Managers need no subsystems
  (App C allows ≤5 without), the inversion survives inside every proposed subsystem, and the shared
  Engines (Estimation, Intervention) falsify subsystem cohesion). The honest instrument is a plain
  standard-check waiver: *"Engine census exceeds the golden-ratio guideline; every Engine individually
  passes the strategy-volatility test; the domain is unusually rules-dense."* Legitimate — the ratio is an
  App C guideline, not a directive. Census reductions that ARE real: **cut HandOffEngine** (a 1-op wrapper
  around a once-per-project human decision; fold into the typed `HandOffModel` decision record + Construction
  Manager gating — census −1), and **ReviewEngine's survival is conditioned on fidelity D2 landing** (it
  legitimately grows into routing + verdict aggregation + mayAmend; a permanently-1-op Engine gets folded).
  **Validation ≠ Review stays two Engines** — decisive reason: deployment destiny (ValidationEngine IS
  methodcheck and ships into customers' repos as the travelling PR gate; ReviewEngine is archistrator-
  internal routing policy — different change drivers, consumers, release cadence). Subsystem names survive
  only as UI/documentation grouping.

## 5. Workstream I — The Design Experience (use cases → contracts → software, smoothly)

- **I1. The thread** (extends fidelity F1): use case → call chain → contract op → activity → PR, clickable
  both ways, powered by the C6 edge-label↔contract-op lint and G2's typed componentId.
- **I2. Solution-tree explorer — as drill-down, never as the decision moment** (PM ratification,
  2026-07-10). The SDP front page is the book's six-item summary and nothing more: duration/cost/risk
  table · time–cost + time–risk curves · risk as a real unrounded number · plain-language option names ·
  subcritical present and labeled shown-to-be-rejected (educational, not selectable) · the architect's
  recommendation with a one-line why. Each option carries a one-line provenance teaser ("arrived at after
  4 staffing passes; 3 rejected for …"); the full tree — iterations, staffing-shape/shallow-S/efficiency/
  complexity verdicts — opens one click behind it. Educate-don't-plead means the educated summary is the
  door and the derivation is available, not forced.
- **I3. Contract-first as the default compressed option.** The book's strongest compression lever is
  pulling contract design forward as enabling activities + simulators + mandatory repeat-integration
  (ch. 9/13). Agent economics make this lever nearly free — archistrator's generated seams (the
  seam-adapter pattern being cleaned up right now was exactly this technique) mean every option set should
  include a contract-first compressed solution by default, priced honestly with the added integration
  activities and its risk computed, never silently adopted. Compression is computed even when it won't be
  chosen — it powers the death zone and the G5 pivot set. Refinements (2026-07-10): the option artifact
  must **walk the ch. 9 lever ladder per project** (practices/infrastructure → top resources → parallel
  work) and record why the earlier rungs are pre-banked (codegen ≈ infrastructure, model tier ≈ top
  resources) rather than ossifying into a rung-three template; customer-facing it is **computed by default,
  never pre-selected** — a priced peer option in outcome language ("design the connections up front: ~X
  sooner for ~$Y more, risk Z"), architect recommendation flagged (PM). Note the App B coupling: under a
  **junior** hand-off the book *prefers* contracts designed ahead in parallel — the hand-off decision is
  the shared premise of this ruling and Q3(a); if agents are ever ruled junior, contract-first flips from
  compression lever to the normal solution's structure.
- **I4. Live options after the decision.** The SDP page keeps unchosen options addressable during Phase 3
  (they are the pivot targets G5 needs), with the sign-off record and its "valid while inside plan
  parameters" scope rendered — the life-insurance-policy semantics, visible.
- **I5. Debrief as an activity.** Per milestone and per project: estimation accuracy vs actuals feeds the
  PERT posteriors (fidelity E2). The cross-project calibration loop the book prescribes and almost every
  org skips — archistrator gets it for free from metered agents.
- **I6. Review-loop ergonomics** (PM, 2026-07-10 — the two highest-value review-UI investments; gate
  *count* is not the fatigue lever, gate *cost* is): (a) **the "what changed since your feedback" diff** —
  every redraft opens on *your feedback → what changed → the rest is unchanged*, making a reject loop cost
  seconds instead of a full re-read; (b) **draft-ahead with staged gates intact** — downstream artifacts
  pre-draft in the background so the common approve-path has zero wait and Phase 1 feels like one reviewing
  session with checkpoints; a reject simply invalidates the stale pre-drafts (the StaleBasis machinery
  already models this). The mega-draft "review once" path is explicitly NOT built — rejection must stay
  cheap (goal 4's teeth). Each gate leads with the decision ("Approve this mission?" + one plain-language
  paragraph), shows one-hop upstream provenance, and surfaces architect judgment calls at the right gate
  ("you mentioned X; I judged it a variation of core use case Y — disagree?"). Never a blank spinner:
  drafting progress is visible reasoning, which for this customer is the product.

---

## 6. Rulings (resolved 2026-07-10)

Gate 1 decided by the founder; gates 2–6 and the three follow-up questions delegated to be resolved "by
the book," with system-architect and product-manager role consultations on record.

### 6.1 The six gates

1. **Temporal — DECIDED (founder):** decided variable, not a volatility. Strike "Durable Execution
   Runtime" from slot 3; record the commitment in operational concepts (H3's first amendment).
2. **DesignManager merge — RATIFIED with conditions** (see H1): ≤12-op proof with the F2 relocation in the
   same amendment; B2 contractcheck as the acceptance gate; orphan components promoted with
   **kind-parameterized redesigned contracts**, never the per-kind op ladders. Book basis: ch. 3 — "a
   Manager can support more than one family of use cases … as different facets"; App B prescribes reducing
   Managers to facets; the 7 duplicated ops are the ch. 2 wrongly-split-volatility tell.
3. **M0 pump gate — RATIFIED, override narrowed** (see G1): pump requires committed `.sdpReview` + bound
   option + phase == construction. The override is scoped to pre-SDP contract-design of a bounded set of
   services (ch. 14: "possible to design a few services up front, but not all of them") — i.e. the
   enabling activities of an I3 candidate option. No blanket construction early-start; front-end trimming
   was a mis-citation (it shortens the front end, it does not move M0).
4. **Cardinality — subsystem framing REJECTED, explicit waiver RATIFIED** (see H5): plus cut HandOffEngine,
   condition ReviewEngine on D2, keep Validation ≠ Review (deployment destiny).
5. **Contract-first compressed entrant — RATIFIED with lever-ladder discipline** (see I3): computed by
   default, never pre-selected; per-project lever-ladder record; hand-off coupling noted.
6. **Estimation merge + rate card into the Engine — RATIFIED clean** (see H2): ~4-op contract
   (`EstimateForOption` shared), inside App B's sweet spot. Ch. 8 determinism makes CPM/EV calculator code,
   not a seam; the rate card is the real volatility.

### 6.2 Q1 — Design-phase prompt granularity: one prompt per artifact, two book groupings

**Ruling: one drafting prompt per artifact step**, with exactly the groupings the book treats as one
activity: **glossary + scrubbedRequirements drafted together** (one requirements-analysis pass — note this
*changes live behavior*: the current rail drafts them as two prompts) and **architecture +
call-chain-validation together** (ch. 4: the validation IS the done-gate of the decomposition). Rejected:
the mega-prompt (all 8 artifacts at once) — it destroys per-artifact review gates, per-artifact validation,
PM-critique routing, and goal 4's teeth (a rejection must be cheap; rejecting a mega-draft is a rework
avalanche the customer experiences as "I approved something I didn't understand"). Also rejected: the
volatilities+coreUCs+architecture 3-in-1 episode — ch. 2's interleave is real doctrine, but it is a
*license within* volatility identification ("suggest areas of volatility, then examine the resultant
architecture … retract"), not a mandate to co-commit three artifacts the book reviews separately.

The fixed-point loop is realized by three mechanisms instead: (a) **propose-and-inspect license inside the
volatilities prompt** (may sketch a tentative decomposition to test encapsulation, must discard it);
(b) the **amendment/StaleBasis back-edge** when a later step invalidates an earlier commitment; (c)
**validation findings routed as redraft feedback** (G3). On single-architect integrity (ch. 7): the enemy
is *partitioned ownership*, not sequential sessions — every session owns the whole artifact and reads the
whole committed prior design, and the continuous single mind the book requires is the human at every gate.
**Refinement adopted — the "negative space" carrier:** each Phase-1 artifact gets a typed
considered-and-rejected section (volatilities and coreUseCases already have the pattern; extend to the
System slot: rejected decompositions with reasons), and successor briefs include predecessors' negative
space. Löwy's own device: he externalizes rejected alternatives precisely so the design survives its
designer. Customer-side, the granularity is delivered with I6's ergonomics (draft-ahead + feedback diff).

### 6.3 Q2 — Context injection: generated briefs from a typed ContextSpec

**Ruling: replace prompt-in-dispatch-payload with generated briefs.** A typed per-artifact-kind
`ContextSpec` owned by DraftingEngine declares: the exact predecessor slots to read (the skills' "Reads:"
lines made machine-readable), research-corpus refs, reviewer feedback + review ledger verbatim, validation
findings (G3), the negative-space carriers (Q1), the produce-target and its gates — rendered
deterministically into a brief committed on the session branch (auditable, versioned; the brief format is
owned by the F12 schema-version volatility). The dispatch input carries **ids only** — the 64KB cliff and
`prompts.go`'s 580 hand-written lines both die. Doctrine stays in the repo checkout (fidelity A2 single
source); constraints are validators, not prose. `ComposeDraftBrief`/`ComposeCritiqueBrief` are ops on the
redesigned DraftingEngine contract; the H4 redraw of `drive-system-design` shows that edge; C6 lints it —
one join point across three rulings. **Include a `withhold:` field now**: the broadband estimate (O10)
requires deliberate context *exclusion* (it must not read `.activityList` to serve as an independent
cross-check) — only a typed spec can guarantee a negative.

**Context gap audit (fix in prompts.go now; carried into ContextSpec as the permanent fix).** Severe —
the step cannot be performed as the book defines it: (1) **scrubbedRequirements gets no research corpus**
(`systemdesign/prompts.go:65` — the agent can only scrub requirements it invents from the mission; ch. 2
scrubbing interrogates the customer's actual statements); (2) **standardCheck reads only
System+OperationalConcepts** (:75) — the App C walk it is ordered to perform needs Mission, Volatilities,
CoreUseCases; (3) **network never reads the System slot** (`projectdesign/prompts.go:63`) — ch. 13 derives
behavioral dependencies from the call chains; the network is drafted blind to them. Material: glossary
reads mission only (Four Questions run over the domain corpus); volatilities gets no research access (the
competitor lens and longevity heuristic need business context); coreUseCases omits scrubbedRequirements
(contradicting its own skill); activityList omits operationalConcepts (ch. 13's noncoding activities came
from it). Already right and to be preserved in the brief format: System's brief carries the full
volatilities model with rationale; feedback + ledger ride verbatim; SDP assembly stays deterministic.

### 6.4 Q3 — Service contracts: construction-phase, dynamics first, linted at commit

**Ruling, four parts.** (a) **Construction is the right phase**: contracts are designed per-component in
the detailed-design phase of each activity (App A Fig A-1; App B "make the contract design part of each
service life cycle") — *conditioned on the senior hand-off in force*; under a junior hand-off App B
prefers contracts designed ahead in parallel, so record the hand-off decision as this ruling's premise.
(b) **Dynamic views do NOT need contracts**: ch. 4's validation gate is "an interaction between your
services" — component interactions, not signatures. The architecture is valid, reviewable, and committable
before any contract exists. (c) **Dynamics first, contracts to fit after — never iterate all contracts
upfront until dynamics look right**: that is detailed design in the front end, the book's named failure
mode ("detailed design in the front end simply takes too long," ch. 14). (d) **The reconciliation is the
C6 lint at contract-commit time**, with discoveries flowing back as view amendments (ch. 13 precedent: the
design team "discovered some missing dependencies in the call chains" during project design and fixed
them — the back-edge is book-sanctioned). Today nothing validates that dynamic edges resolve to contract
ops — C6 gates the detailed-design phase exit once built.

**Design-time edge-label grammar** (valid before contracts exist, lintable after):
`label := interaction [" — " prose]` where `interaction` is either a **responsibility** (imperative verb
phrase in the destination layer's vocabulary — Manager: use-case verb; Engine: gerund activity; RA: atomic
business verb) or an **op-shape** `name(params)`. Lint semantics: op-shape against a committed contract
must resolve (Error); op-shape against an uncommitted contract is a recorded promise (Info, checked at
that contract's commit); responsibility-phrase against a committed contract asks for binding (Warn);
unreferenced contract ops are Info. Temporal-primitive/platform tokens in labels stay Error at all times
(the standard-check §I grep that contradicts this is fidelity-§2.1's bug). Audit note: the 17 dogfood
views are already mostly op-shaped and C6-lintable; the grammar exists so *customer* projects at initial
design time aren't taught to smuggle signatures into the front end. Customer-side (PM): the felt gap of
"boxes and arrows then a cliff" is closed by I1's edge→future-contract-op preview — the commitment made
visible ("this interaction becomes an operation on X, designed in activity A"), not premature signatures.

## 7. Sequencing

**Now (days, no ratification needed — pure defects):** O3 (one seal path), O4 (pause reaches the cascade),
O5 (surface starvation), O8 (decision precheck), F5 (CPA contract cleanup — coordinate with the in-flight
seam-adapter plan), **the three severe context gaps in prompts.go** (§6.3 — scrub gets the corpus,
standardCheck gets mission+volatilities+coreUCs, network gets the System slot; small edits now, ContextSpec
is the permanent home), fidelity-plan quick wins continue in parallel.

**Next (rulings resolved 2026-07-10 — see §6; 1–2 weeks each):**
1. **G1 + G2** — phase integrity + pump-consumes-project-design (needs fidelity A1 types; the moment the
   Method's economics start steering construction).
2. **H1** — DesignManager + engine promotions, via dogfood amendment sessions (also resolves O9; do the
   H4 view redraw in the same amendment; fidelity B2 contractcheck lands with-or-before as the merge's
   acceptance gate; the Q1 grouping change and Q2 ContextSpec/brief mechanism ride the DraftingEngine
   extraction).
3. **G5** — the tracking loop closed end-to-end (with fidelity E1–E3 data underneath).
4. **H3** — substrate rulings + missing volatilities (schema-version seam first: it is the volatility most
   likely to fire).

**Then:** G3/G4 loop semantics + broadband gate · H4 derivability gate in methodcheck · I1–I5 experience
thread.

**Coordination notes:** the aiarch-state agent-surface plan (2026-07-06) and seam-adapter cleanup
(2026-07-09) are complementary and unblocked; H1 renames their Manager targets — land them first. The
fidelity plan's workstreams A/B remain the kernel this plan's G/H consume; where they overlap (D⊂G6/G5-gates,
E4⊂G5), this plan's framing governs the architecture and the fidelity plan governs the schema/validators.

**Doctrine of the whole plan:** the fidelity plan made the book's numbers into validators; this plan makes
the book's loops into state machines, and makes archistrator's own architecture the first proof. If it's a
number, validate it; if it's structure, generate it; if it's judgment, prompt it; **if it's a loop, make it
a typed back-edge with a gate** — and a human can stop the machine at every one.
