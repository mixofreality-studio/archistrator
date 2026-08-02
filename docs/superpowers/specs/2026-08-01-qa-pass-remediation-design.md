# QA-Pass Remediation — Design

**Date:** 2026-08-01
**Status:** Approved by founder (this session)
**Source:** Founder's manual QA pass — 19 numbered notes, adjudicated one-by-one.
**Execution model:** Planner/orchestrator dispatches all implementation to subagents; UI changes follow the founder review loop (run locally on real state, Playwright-checked, stop-for-review per UI change).

## 1. Note → workstream traceability

| Note | Summary | Workstream |
|------|---------|------------|
| 1, 1a | Realized-by / glossary links don't deep-link to content | A |
| 2 | Is linkage enforced? | B (rules), F (test coverage rules) |
| 3 | Delete Required Behaviors; mimic book ch05 order | B |
| 4 | Review policy has no selected default on load | A |
| 5 | Project-design cost model wrong for AI reality | C |
| 6 | Kill the 4 solution tabs; options live on SDP Review | C |
| 7 | Milestone buttons say m1/m2 instead of names | A |
| 8 | Review policy vs intervention policy confusion | D |
| 9 | Full-screen review experience (not a drawer) | D |
| 10 | Filter approvals by kind | A/D |
| 11 | Artifacts page purpose / facelift | E |
| 12 | Contracts as the 4th C4 (code) level from Architecture | E |
| 13 | Construction tab deep links broken | A |
| 14 | Tests walk the use case; path coverage | F |
| 15 | Testing artifact too busy | F |
| 16 | Deployment diagrams need structurizr-style relationships | G |
| 17 | Test results in the home tree | F (write-back), E (tree slot) |
| 18 | project.json health / plausible construction history | H |
| 19 | Design Health page, waivers, gerund rule | B |

## 2. Adjudicated decisions (founder, this session)

1. **Required Behaviors: retire the slot fully.** Page, data slot, rail step, predecessor links all go. Requirements scrubbing becomes an internal technique inside volatility drafting (its canonical home per ch02). The teardown is complete — no hidden-required-slot residue like the standardCheck half-teardown left behind.
2. **Phase-1 nav = 7 steps:** Mission → Glossary → Volatilities → Core Use Cases → Architecture → Deployment & Operations Model → Design Validation. Core Use Cases stay before Architecture (drafting order unchanged). Design Validation is a new render-only step absorbing the Dynamic lens.
3. **Waivers are dead. Two rule classes only:** *hard* (error, blocks draft commit, no exceptions) and *advisory* (agents must address-or-justify in-loop; never blocks; never user-visible). Design Health leaves the user nav entirely; findings keep gating agent drafts and CI. Attestations remain as internal agent-authored records on the System artifact.
4. **Cost model = tokens + plan capacity.** Per-activity token estimates; $ derived per venue (API list price vs subscription seat cost + allowance); schedule constrained by plan rate limits, not headcount; human review enters as latency + optional $/hr at policy-gated phases.
5. **SDP options = Löwy's four, reinterpreted and computed** (plan/fan-out scenarios). The riskModel drafted step retires; risk is engine-computed only. Phase-2 spine = assumptions → activity list → network → SDP Review.
6. **Artifacts tab dissolves.** Contracts surface inside Architecture as the Code lens; UI designs and test designs join the home tree as sections; construction console = Tracker + Reviews.
7. **Full-screen review layout = artifact-first + command bar** (full-bleed artifact viewer, slim header with queue prev/next, fixed bottom action bar).
8. **Test-plan viewer = walk-the-use-case**, behaving like the Architecture Dynamic lens (walkthrough + accreting call chain), with per-path coverage.
9. **Deployment model v2 = diagram parity only** (IDs, relationships, validation, edge rendering). Helm-oriented fields and any generator are out of scope, earmarked for N-DEP.

## 3. WS-A — Deep links & quick wins

### 3.1 Selection deep-linking
- Extend the design routes' `validateSearch` (`webApp/src/routes/router.tsx` — today only `systemDesignRoute` validates `{view, step}`) with a `select: string` param. Values use the **comment-anchor id vocabulary** already defined per item (`webApp/src/components/comments/CommentContext.tsx` anchor builders: objectives, glossary terms, ops knobs, behaviors-era anchors excluded, volatilities, use cases). No new id scheme.
- Widen `StepLink`'s hard-typed `search` prop (`webApp/src/components/shared/StepLink.tsx:36-39`) to carry `select`.
- Consuming pages follow the proven resolver idiom from `webApp/src/components/flow/architectureDeepLink.ts` (param wins on mount; the reader's subsequent changes win over same-location remounts; interoperates with module-scope `viewMemory`). New shared utility: resolve `select` → auto-select the item + `scrollIntoView` (none exists in the app today).
- Emitting/consuming pages in scope: `MissionView` realized-by chips carry the knob id (from `deploymentOpsLogic.realizingKnobs`); `OperationalConceptsView` consumes knob selection; `GlossaryView` usage chips carry the term and the glossary consumes term selection (search/filter state currently local `useState`); `UseCaseCarousel` accepts a use-case id (today always lands on index 0); `VolatilityMap` accepts a volatility id; reverse links (knob → objective, volatility → sources) carry their targets symmetrically.

### 3.2 Console tab/URL state (note 13)
- `/project/$projectId/construction` gains an optional `{-$tab}` path segment (tracker | reviews), mirroring the design routes' slug pattern and its normalize-with-replace behavior (`SystemDesignContainer.tsx` — the documented F-GTD-14 fix for exactly this defect class). `ConstructionConsole.tsx:123` `useState` dies. Same treatment for `OperationsConsole.tsx:106`.
- The selected activity in the lifecycle panel becomes a search param so reviews and tracker positions are shareable.

### 3.3 Review-policy default (note 4)
- Root cause: dogfood `project.json` has `reviewPolicy: {}`; `reviewPolicyToContract` (`server/internal/manager/systemdesign/systemdesignmanager.go:2584-2588`) omits empty policies; UI has no fallback; only project creation seeds vibes.
- Fix server-side: empty policy reads as `preset: vibes` (default-on-read). **Precondition:** implementer reads the deliberate no-write rationale at `server/cmd/archistrator/init.go:26`; if defaulting-on-read conflicts with it, fall back to a UI-level display default instead. WS-H sets the dogfood preset explicitly regardless.

### 3.4 One-liners
- Milestone chips: `webApp/src/components/project/NetworkView.tsx:446` renders `m.name` (keep `m.id` as a mono prefix). Data already has good names (M1 "Infrastructure Provisioned", …).
- Reviews queue gains the kind filter chips copied from `ArtifactsTab.tsx:206-241` (`FilterChip`, counts, application) — metadata (`ConstructionRow.kind`, `variant`) already present (note 10).

## 4. WS-B — Phase-1 information architecture & doctrine

### 4.1 New spine
Order: mission, glossary, volatilities, coreUseCases, system, operationalConcepts, designValidation.

Design Validation is a UI-only spine step, **not** an `ArtifactKind` — no wire kind, no slot, no codec entry. The spine model gains the notion of a render-only step appended after the six authored kinds; server-side required/predecessor/rail structures know nothing about it.

Touch points (from recon):
- SPA: `webApp/src/contracts/methodMetadata.ts` (`PHASE1_ORDER`, `METHOD_METADATA`, slugs), `SystemDesignContainer.tsx` `buildSpine` step-lock, `HomeBase.tsx` TOC, `ArtifactRenderer` dispatch.
- Server: `phase1PredecessorKind` (`systemdesignmanager.go:1488-1504`), `Phase1RequiredKinds` (`projectstateaccess.go:2492-2502`), the Temporal rail spawn order (`systemdesignphase.go:57-95`), `downstreamKinds` staleness chain (`projectstateaccess.go:2576-2596`), `phaseProgress` count (`projectstateaccess.go:735-757`) → **n/6 committed** (six authored slots; Design Validation is render-only and carries no commit state).

### 4.2 Required Behaviors retirement
- Slot kind 2 (`scrubbedRequirements`) removed from required kinds, predecessor chain, rail, and codecs. `ScrubbedRequirementsView` deleted.
- Volatility drafting absorbs the scrub-solutions pass: `the-method-volatility-identification` skill + `volatilities-draft` command gain the ch02 scrubbing technique and the PM critic that `scrubbedRequirements` had (customer-proxy review of what got scrubbed). `the-method-requirements-analysis` skill and `scrubbed-requirements-draft`/`-critique` commands retire.
- `Volatility.Traces` semantics change from B-NN ids to references into research sources / use cases. `DH-VOL-TRACE` re-points: hard rule that every volatility cites at least one source (non-empty traces); the B-NN join and platform `VOL-TRACE` vocabulary join retire. `VOL-GLOSS` (volatility names a glossary concept) survives as hard.
- Glossary usage chips drop the Behaviors row (`glossaryLogic.ts`).
- Migration: existing projects with a committed `scrubbedRequirements` slot — slot data dropped on read/write; volatility traces left as free text (they were `[]string` anyway).

### 4.3 Design Validation step
- Render-only nav step (like Design Health was, but user-facing and permanent): per-use-case walkthrough (`UseCaseWalkthrough`) beside the accreting call chain (`DynamicViewFlow` fragment mode) — the exact experience currently living in Architecture's Dynamic lens (`ArchitectureView.tsx:483-512, 657-672`), relocated. Deep link `?view=&step=` moves with it.
- Unlocks when Architecture (`system`) commits. No slot, no commit state, no drafting.

### 4.4 Design Health / standardCheck / waivers teardown
- Design Health leaves nav + renderer dispatch (`ArtifactRenderer.tsx:109-114`, `SystemDesignView.tsx:506-510`, `DesignHealthView.tsx` deleted). `GetDesignHealth` endpoint **stays** (agents, CI, internal tooling).
- Finish the standardCheck teardown: remove from `Phase1RequiredKinds`, the seal refusal (`systemdesignphase.go:123-131`), `standardCheckFailItems` (`systemdesignmanager.go:891-900`), `STD-STATUS-EXPLICIT` (`projectstateaccess.go:5404-5420`). This also fixes the impossible "7/8 committed" chip.
- Waivers: `System.Waivers`, `Volatilities.Waivers` fields removed (schema + codec regen both repos); `GetDesignHealth` waiver join removed; standing waivers deleted from dogfood data (WS-H executes the data edit). `the-method-system-design-standard-check` skill rewritten: no waivers; attestations (Prime Directive, D1–D4, §3a–d) remain agent-authored on the System artifact, internal only.
- **Rule reclassification:** every current rule (designhealth `DH-*`/`CC-*`, platform methodcheck, crossartifact `ACT-*`/`PA-*`) lands in the hard/advisory table. Defaults: existing Errors → hard; existing Warnings/Info and all judgment bands (§2h volatility count, §2d Engine:Manager ratio, naming style) → advisory. The implementation plan enumerates the full table; any rule we won't stand behind gets deleted, not softened.

### 4.5 Doctrine corrections & new rules
- Gerund rule corrected everywhere it's stated (`the-method-layers/SKILL.md:26`, `agents/system-architect.md:152,157`, `commands/system-design.md:174,179`, doc-comment `projectstateaccess.go:3893`): gerunds are **permitted only on** Engines (book ch03); an Engine is *not required* to use one. `AccountEngine` is criticized for vagueness, not for lacking -ing.
- New advisory rule `GLOSS-USED`: glossary term referenced by no downstream artifact (closes note-2 gap a). Objective coverage (`DH-OBJ-COVERAGE`) survives as advisory. Behavior-orphan checks die with the slot. Scenario coverage rules land in WS-F.

## 5. WS-C — Project-design model v2

### 5.1 What's wrong today (recon-confirmed)
Cost = effort-days × $/day (`assemblesdpreview.go` rate card; tokens collapse to a daily rate immediately); scheduler = flat anonymous worker pool, one activity per worker for its full duration, single global `StaffingCap`, `WorkerClass` used for rate lookup only (`estimationengine.go` SSGS); zero modeling of plan types, rate limits, human review time; `ReviewPolicy` influences nothing numerically; `$50/day` indirect dominates (69% of dogfood build cost); riskModel step is LLM-drafted while SDP recomputes risk — they can disagree.

### 5.2 Planning assumptions v2
- **Capacity inventory** replaces the human-shaped resource list: entries `{planType: pro|max|max5x|max20x|api, seats, monthlyCostMinorUnits | apiBudget, concurrentSessionLimit, tokenThroughputPerWindow}`.
- **Human reviewers** (optional): `{role, hoursPerDay, hourlyRateMinorUnits?}` — consumed only when the review policy gates phases.
- `indirectDailyRate` retired as a primary driver (kept optional for genuinely fixed overheads). `rateCard` (MTok/day → $/day collapse) retired.

### 5.3 Activity list v2
- `ActivityItem` gains per-activity token estimates `{estTokensIn, estTokensOut}` (units: tokens, not $). Source: the token-calibration workstream's episode ledger once populated; class-tier defaults until then. `workerClass → model` mapping survives for pricing and for calibration bucketing.

### 5.4 Engine v2
- **Schedule:** dependency network + fan-out bounded by plan concurrency (aggregate concurrent sessions across seats), token-throughput throttling when aggregate demand exceeds window capacity, and **review latency injected at policy-gated phases** (per activity kind × gated phase, from `ReviewPolicy` + reviewer availability). Vibes ⇒ zero human latency. Float/critical-path math over the resulting network is unchanged (criticality-risk doctrine intact).
- **Cost:** Σ activity tokens × venue price (API), or seat cost + allowance-consumption accounting (subscription plans), + human review hours × rate when declared.
- **Risk:** criticality + activity risk computed from the network exactly as today; drafted risk rows die.

### 5.5 Options & slots
- Four **computed** scenarios: normal (declared plan + policy), compressed (tier-up / +seats, ≤30% compression cap, review-bottleneck premium replacing s²), subcritical (tier-down — proving cheaper-plan is slower *and* riskier), decompressed (normal + buffer toward composite risk ≈ 0.5). Exclusion zones (0.75 / 0.30 / 30%) unchanged.
- Slots 11–15 (`normalSolution`, `subcriticalSolution`, `compressedSolution`, `decompressedSolution`, `riskModel`) **retire**. `sdpReview` slot absorbs the option rows (already its shape). Readers must tolerate legacy files still carrying the dead slots.
- Phase-2 spine = 4 steps (`PHASE2_ORDER`): planningAssumptions → activityList → network → sdpReview. `SolutionView` and `RiskModelView` are deleted; `SdpReviewView` gains a per-option scenario-config disclosure (plan, seats, policy, throttle assumptions) so nothing about an option is hidden jargon (note 6).
- Skills/commands for the retired steps (`normal-solution-draft`, `subcritical-…`, `compressed-…`, `decompressed-…`, `risk-model-draft`, and their `the-method-*` counterparts) retire; `the-method-sdp-review` absorbs the four-scenario computation contract.

## 6. WS-D — Reviews experience

- **Rename:** Interventions tab → **Reviews**. Console = Tracker + Reviews.
- **Full-screen review route** `/project/$projectId/construction/review/$activityId` (deep-linkable): layout A — slim header (activity identity, gate, lifecycle, queue ‹ i/n ›), full-bleed artifact viewer (registry-dispatched per kind), fixed bottom command bar (Approve / Send back / Take over / Reassign / Skip; Pause/Replay in overflow; Comment arms existing `CommentContext` anchors). Built by extracting `DrawerBody`/`OperatorBar` from `InterventionDrawer.tsx` into an `InterventionReview` component; the drawer dies. Operator actions remain disabled until the live pump (R-CPR) — unchanged semantics.
- **Policy consolidation:** one editing surface. The home-page `ReviewPolicyControl` grows the per-kind gate switches (progressive disclosure under the preset radios); `PolicyPanel` on the construction console goes read-only-summary + link. Both halves already live in the same `ReviewPolicy` struct.
- **Wire `reviewPolicyRef`:** committing Deployment & Ops sets `ReviewPolicy.Preset` from the artifact's `reviewPolicyRef` (via the existing `SetReviewPolicy` path, closed vocabulary validated). Today the field is decoration; after this, design-time choice and runtime policy cannot diverge.

## 7. WS-E — Artifact browsing

- **Architecture lenses become Static / Component focus / Code** (Dynamic relocated to Design Validation). Code lens = C4 code level: component dropdown (UI-selection convention: dropdowns for dynamic counts) → `ContractCodeFlow` interface diagram + contract facets, joined via existing `Component.contractKey ↔ project.serviceContracts` (`contractComponentId.ts` resolution). Components without contracts render an honest empty state.
- **Home tree sections** below Deployment & Ops: **UI Design** (frontend design prose + live preview, reusing `FrontendArtifactView`) and **Testing** (per-UC test plans via the WS-F viewer; a results row per suite — uitests, systemtests — reserved now, lit by WS-F write-back). `HomeBase.tsx` TOC filter (`PHASE1_ORDER`-only today) extends to a sectioned outline.
- **Artifacts tab deleted.** The renderer registry (`artifactRenderers.tsx`, uniform `ArtifactRendererProps`) re-homes to a shared location consumed by: full-screen review (WS-D), home tree sections, Architecture Code lens.

## 8. WS-F — Testing experience

- **Schema:** `TestCase` gains `pathRef` (ordered activity-node ids through the use case's activity diagram); `TestStep` gains optional `activityNodeId`. `gen-systemtests` carries the new fields through generated tables. The synthetic-id hack in `ScenarioBrowser` dies.
- **Coverage:** computed against the existing platform path enumerator (`framework-go/methodcheck/activitypaths.go` — decisions branch, forks cross-product, **loops traversed once** (cycle handling exists), work-budget capped). New advisory rules: `STP-PATH-COVER` (every enumerated path has ≥1 case) and `STP-UC-SCENARIO` (every core UC has a scenario — 11 of 16 currently have none, silently).
- **Viewer:** rebuilt walkthrough-first, identical interaction grammar to the Design Validation step: walkthrough drives; tests on the current path listed beside the accreting call chain; per-UC coverage meter (paths covered / enumerated); generated Go test tables one click away. Replaces the dropdown-heavy `ScenarioBrowser` layout (note 15).
- **Results write-back (note 17):** CI system-test and Playwright runs emit a results JSON artifact; a write-back step records `TestRun` + per-step `red|green` into `testingState` (models exist — `TestRun`, `TestStep.Status` — **nothing writes them today**) via the `recordTestingState` verb and commits. Home-tree Testing rows render the latest run per suite.
- **Cross-repo note:** new `STP-*` rules and any `activitypaths` changes land in `archistrator-platform/framework-go` — platform release + pin bump required.

## 9. WS-G — Deployment model v2 (diagram parity)

- **Schema:** stable `id` on `DeploymentNode`, `ContainerInstance`, `InfrastructureNode`, `SoftwareSystemInstance` (elements are name-keyed today — names aren't unique across nesting and can't anchor edges); `relationships[] {fromId, toId, description, technology, tags}` on `DeploymentEnvironment` (per-profile, since infra differs per profile).
- **Validation:** new `DEP-*` predicates — endpoint existence + uniqueness, no cross-environment edges, advisory consistency join to `System.Relationships` (a container-to-container edge should be backed by a component-level call).
- **Renderer:** `DeploymentFlow.tsx` builds real `Edge[]` (today hardcodes `edges={[]}`) with routing across the `parentId`-nested groups; containment layout retained.
- **Migration:** existing deployment data gains derived ids (from names) on read/write; authoring skill (`the-method-operational-concepts`) documents relationships.
- **Out of scope (earmarked N-DEP):** helm-oriented fields (image/chart refs, ports, env, replicas), any helm/argo generator, the stubbed k8s runtime.

## 10. WS-H — Dogfood state repair (strictly last)

Preconditions: WS-B (slot layout final) and WS-C (Phase-2 model final) landed.

- Regenerate all Phase-2 artifacts from the current System (rev 6+) under the new model — ending the rev-1-slots-kept-alive-by-stale-acks state (every Phase-2 slot is pinned to `system@rev2` today; four amendments unreconciled).
- Roster reconciliation: drop `C-HE` (Hand-Off Engine no longer a component); resolve `C-WIA` and the 9 `U-SPA-*` component-join mismatches; adjudicate the 4 contracts without System components (`constructionTransitionAccess`, `designSessionAccess`, `gitActivityStatusAccess`, `revenueLedgerAccess`) and the 4 modeled-but-unbuilt components (`schedulerClient`, `designHealthEngine`, `logging`, `diagnostics`) against what actually exists in code — architecture and roster end up telling the same story.
- Construction history: one Done activity per real component/service/deliverable, provenance derived from **actual git history** where possible (real commits, real dates), plausible fabrication only where necessary. `constructionProgress` populated with earned-value points consistent with that history.
- `reviewPolicy` = `{preset: vibes}`; review threads empty (consistent with vibes — no fabricated review history); standing waivers deleted; stale-acks resolved by the redrafts themselves; `updatedAt` honest.
- Test-plan scenarios for uncovered UCs are **not** fabricated here — the `STP-UC-SCENARIO` advisory (WS-F) keeps the gap visible until real scenarios are authored.

## 11. Sequencing & risks

**Order:** A → B → {D, E, G in parallel} → F → C (largest; mostly server-side, may overlap D–G) → H strictly last.

Risks / coordination:
- **Codec regen ripple** (WS-B waiver-field removal, WS-C slot retirement, WS-F/G schema additions): schema-first pipeline — regen must land with each change or screens render empty (established failure mode).
- **Platform releases:** WS-F rules and WS-G/WS-B methodcheck changes touch `archistrator-platform/framework-go` — release + pin coordination, CI green on both repos.
- **Workflow drain:** slot/step retirements change the Temporal rail — drain in-flight workflows before deploying (same discipline as the callchain-realization ship).
- **Legacy tolerance:** readers must tolerate old project.json files carrying retired slots (scrubbedRequirements, standardCheck, solutions, riskModel) until re-committed.
- **WS-C estimation defaults:** token estimates start as class-tier defaults; numbers improve as the token-calibration episode ledger fills. The model must be honest about confidence until then.

## 12. Out of scope

- Helm/argo generation and k8s runtime realization (N-DEP earmark).
- Enabling operator-bar actions (R-CPR live-pump prerequisite).
- The system audit-log workstream (separate spec, in flight).
- Authoring new system-test scenarios for the 11 uncovered UCs (construction work, tracked by the new advisory rule).
