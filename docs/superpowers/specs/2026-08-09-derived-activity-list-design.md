# Derived Activity List & Network — design

**Date:** 2026-08-09 · **Approver:** system-architect (rulings Q1–Q4 recorded below; founder ratified the two escalated calls) · **Founder ask:** the Phase-2 activity list looks mechanical — one activity per component contract, one per component implementation, one per UI app, one per test rig (Löwy Table 11-1). Generate it with code instead of an agent. Governing principle: **do anything deterministic with code, and only non-deterministic things with agents.**

---

## ⚠ AMENDED AFTER EXECUTION — 2026-08-10, Stage 1 shipped @`b7abd9b`

Stage 1 is built and merged. Execution superseded parts of the design below. **Read this block first; where it conflicts with the original text, this block is correct.** The original is preserved unedited as the record of what was designed rather than what exists.

**1. Slot 9 stores the COMPUTED plan, not a delta document.** §1.1's "the slots store only deltas; render-on-read = derive + apply deltas" was abandoned as unimplementable. The layer order is Manager → Engine → ResourceAccess, so `projectstate` can call neither the Manager (where `MaterializeActivityPlan` lives) nor the Engine that derives. Two live readers — `committedPlanInputs` and `committedActivityList` — would have type-asserted a delta document into an empty `ActivityList` and proceeded with **no plan at all**. What shipped: slot 9 holds the computed activities (shape unchanged, so every reader works untouched), a root-level `activityListOverrides` sibling holds the authored input, and `make derived-plan-check` re-derives and fails on drift — the same generated-and-gated pattern the repo already uses for `contract.gen.go`.

**2. Two founder rulings changed what the derivation emits** (2026-08-09, after reviewing the full derived output):
- **`constructionProfile` has a third value, `"provided"`** — a component backed by an off-the-shelf platform or third-party service is configured, not built. All four utilities are provided (Keycloak / OTel / a logging sink / the Temporal substrate). `generated` and `provided` both suppress the coding activity; unauthored still defaults to `handwritten`.
- **`I-*` integration activities are gone entirely.** Table 11-1 has none, and App A already makes Integration a phase of every activity's own lifecycle, so a separate `I-*` double-counts. Consequently **milestones are M0–M3**, not M0–M4 — M4 lost its whole fan-in — and `N-IT` depends on the `U-SPA-*` set (falling back to the `C-<manager>` set when there is no UI surface).

**3. §1.4 conditions C3 and C4 are UNIMPLEMENTABLE as written.** Both describe channels that do not exist in the shipped vocabulary:
- **C3** claims additive deltas express the `N-CI`→coding and `N-SCHEMA`→store-RA fan-outs. `AdditiveActivity.DependsOn` declares only the *predecessors of the additive*. There is no way for an additive to become a **predecessor of a derived activity**.
- **C4** claims a derived milestone can acquire predecessors from additives. `applyAdditiveMilestones` **rejects** any additive milestone shadowing a derived one, and no other channel exists.

Both are latent only because the founder cut the 14 componentless additives, so the additive path has never run against real data. **Stage 2 must resolve them before it can lean on additives.**

**4. The activity count is 40, not 49 or 69.** 69 hand-authored → 40 derived: −3 zombies, −3 generated-transport clients, −4 provided utilities, −5 integration, −14 componentless additives.

**5. §1.8's alias map covers 40 of the 69 construction rows; 29 are deliberately orphaned** (verified 2026-08-10): 3 zombies, 3 generated clients, 4 provided utilities, 5 integration, 11 `N-*` checklist, 2 SPA extras, and `R-DER`. Every one of the 40 derived activities does have a construction row.

**6. A defect this design did not anticipate, now doctrine.** Suppressing a component's coding activity also suppresses **every architecture edge through it**. When the clients became `generated`, all client→manager edges vanished and the `U-SPA-<manager>` activities standing in for them inherited nothing — so the committed plan had the system fully tested **50 days before the managers it tests existed**, understating duration by 43% (115 against a true 165). Every gate was green; the network was internally consistent and incoherent as a schedule. Fixed by wiring `U-SPA-<m> → C-<m>`. The general lesson, now in `method-assets` v0.4.0: **a derived network can satisfy every consistency check and still describe an impossible schedule — solve it and assert an ordering invariant.**

**7. Open for a ruling: the enforcement point weakened.** `ACT-COMPONENT-COVERAGE` ran inside `applyGateSeverityPolicies`, so it fired in the MCP `putDraftModel` **write path** as well as in `validate`. Its replacement is a Go test, and `aiarch-design.yml` runs only `validate --slot` — so an agent amending slot 9 in a design session now gets no in-loop feedback. Folding re-derivation into `validate` was investigated and rejected: `MaterializeActivityPlan` sits in a Manager package importing the Temporal SDK, against `validate.go`'s explicit thin-CLI doctrine, and `activityListOverrides` is not in the typed `Project` model. The predicate is strictly stronger; where it fires is weaker.

---

## Current state (verified 2026-08-09 against `.aiarch/state/project.json`)

- **Slot 9 `.activityList`** holds 69 committed activities. Prefixes: `C`=31, `N`=18, `U`=8, `R`=6, `I`=5, `G`=1.
- **Slot 5 `.systemDesign`** holds 37 components: 3 client, 5 manager, 7 engine, 10 resourceAccess, 8 resource, 4 utility.
- **Slot 10 `.network`** holds 68 dependency edges, 6 milestones (M0–M5), and an authored critical path.
- The `C-*` set is exactly one per code-layer component. `workerClass` on all 69 activities matches the prefix→class rule table in `the-method-activity-list` with **zero exceptions** — it is a pure function of prefix + component kind.
- Downstream math is **already deterministic Go**: `server/internal/engine/estimation/estimationengine.go` (1294 lines) implements CPM forward/backward pass, total/free float, critical path, resource-leveled SSGS scheduling, criticality/activity risk, earned value, and `speedUpCritical`.

So today: **the agent authors the mechanical part and code computes the analytical part.** This design inverts that.

### Defects the current hand-authored list carries

1. **Three zombie activities**, all marked Done+Integrated in `.activityConstruction`:
   - `C-HE` "Build Hand-Off Engine" — HandOffEngine was cut from the architecture (workflow-architecture ruling).
   - `C-WIA` "Build Work Item Access" — no such component among the 37.
   - `R-WIT` "Provision Work Item Tracker" — no such resource.

   All three survive `ACT-COMPONENT-COVERAGE` **only because they omit `componentId`**, which makes them invisible to `activityCoverageFindings`. `C-HE` is additionally still referenced by milestone M2. A derived list would never have emitted any of them.

2. **The generated-transport doctrine is violated three times.** `the-method-activity-list` states a client-tier component whose substance is generated transport gets **no** coding activity ("do not plan work the generator does"). Yet `C-CW` (web-client, 20d), `C-CM` (mcp-client, 25d) and `C-CS` (scheduler-client) are all committed coding activities. Doctrine and plan contradict each other today.

3. **`R-*` is not one per Resource.** Only 4 of 8 resources have one (github, merchant-gateway, construction-pipeline-runtime, operated-runtime). The four **owned stores** — project-git-repo, operated-system-state, billing-state, usage-log — have none; their work rides `N-SCHEMA`/`N-DEP`. `R-DER` ("Durable Execution Runtime") maps to no component at all. The real rule is one `R-*` per **vendor** resource. No `R-*` activity carries a `componentId`; the mapping exists only in prose titles.

4. **The compressed solution is a fudge factor.** The four option slots (11–14) differ by exactly three scalars:

   ```
   normal:        staffingCap 6, criticalSpeedup 1.0, bufferDays 0
   subcritical:   staffingCap 4, criticalSpeedup 1.0, bufferDays 0
   compressed:    staffingCap 6, criticalSpeedup 1.8, bufferDays 0
   decompressed:  staffingCap 6, criticalSpeedup 1.0, bufferDays 20
   ```

   `criticalSpeedup: 1.8` asserts critical activities run 1.8× faster with no account of *how*. That is not Löwy's compression (ch. 9 §6, ch. 11 §4), which mutates the **network**: insert simulators to break dependency edges, split contract design out to unblock dependents, parallelize. There are zero simulator activities and the option slots have no field that could hold a network mutation.

   The consequence is already visible in slot 15: **the compressed option is excluded as too risky** (composite 0.797 > 0.75 ceiling) and the project recommends `decompressedSolution` — its *slowest* plan. Compression modeled purely as float compression can only raise risk, so the current model is structurally incapable of producing a viable compressed option for the SDP review.

## The principle

Löwy ch. 11 is explicit that the network is the direct product of the architecture — Figure 11-4 → Figure 11-5 is literally a transitive reduction over the component dependency chart. An engineered artifact derived from another artifact by a fixed rule should be **computed, not transcribed**. The zombies are the proof that transcription fails silently.

---

## Part 1 — Derived baseline + authored deltas (stage 1)

### 1.1 Slot shape

`.activityList` (slot 9) and `.network` (slot 10) stop storing materialized lists. Code derives the full baseline from `.systemDesign` + `.planningAssumptions`; the slots store only **deltas**. Render-on-read = derive + apply deltas.

This is the same pattern already shipped for the deployment view (derived + authored edges) and is applied again one level up in Part 2.

### 1.2 Derivation rules

| Derived output | Source | Rule |
|---|---|---|
| `C-<component>` | slot 5 | one per code-layer component (client/manager/engine/resourceAccess) with `constructionProfile: handwritten` |
| *(no activity)* | slot 5 | code-layer components with `constructionProfile: generated` — the generator does the work |
| `R-<resource>` | slot 5 | one per Resource component with `provisioning: vendor` |
| *(no activity)* | slot 5 | Resource components with `provisioning: owned` — schema/deploy work is additive noncoding |
| `U-SPA-<manager>` | slot 5 | one per Manager component (see 1.3) |
| `U-SPA-S` | — | always emit when a client declares `uiSurface: true`: SPA scaffold, auth wiring, design system |
| `G-SPA` | slot 5 | emit when any client declares `uiSurface: true` |
| `I-UC<n>` | `.coreUseCases` | one per core use case |
| `N-STP` `N-STH` `N-RTH` `N-SMOKE` `N-QA` `N-PERF` `N-IT` | — | the always-emit testing/QA inventory (7) |
| `workerClass` | prefix + kind | fixed table; matches all 69 live activities with zero exceptions |
| `effortDays` / `riskBucket` | layer table | see 1.6 |
| dependency edges | slot 5 relationships | transitive reduction (Fig 11-4 → 11-5) |
| pattern edges | — | fixed: `N-STP`→`N-STH`, `G-SPA`→`U-SPA-*`, component cluster→`I-UC*`, `I-*`→`N-IT` |
| milestones M0–M4 | — | M0 SDP Review (fixed doctrine — no construction before it); M1–M3 layer-completion; M4 use-cases-demonstrable off the `I-UC*` set |

**Derived vs additive noncoding.** Of the 18 live `N-*` activities, 7 are the always-emit inventory above. The other 11 (`N-SCHEMA`, `N-CI`, `N-SEC`, `N-ADR`, `N-RUN`, `N-REQ`, `N-ARCH`, `N-PLAN`, `N-SC`, `N-DEP`, `N-HARD`) are the ch. 13 checklist filtered to this project — **additive deltas**. Walking that checklist and deciding what applies *is* the legitimate non-deterministic residue.

### 1.3 U-SPA per manager

**Ruling: one `U-SPA-<manager>` per Manager component, plus the always-emit `U-SPA-S` scaffold.**

Evidence: 5 of the 8 hand-authored `U-SPA*` activities already map 1:1 to the 5 managers, without anyone having specified the rule.

| Activity | Manager |
|---|---|
| `U-SPA-1` Phase-1 system-design screens | `system-design-manager` |
| `U-SPA-2` Phase-2 project-design screens | `project-design-manager` |
| `U-SPA-3` Construction tracking | `construction-manager` |
| `U-SPA-4` Operations console | `operations-manager` |
| `U-SPA-5` Billing screens | `billing-manager` |

Justification: a Client calls Managers, a use case is a Manager, and the verbs-as-tools / MCP-Apps doctrine means a manager's generated tool surface == its contract ops == its widget set. One activity per manager therefore delivers a coherent, independently-integrable slice.

Residue, both **additive deltas**:
- `U-SPA-6` (change-request → subproject) spans system-design + project-design managers — two use cases fronted by two managers. No interaction-rule violation, but it maps to no single manager.
- `U-SPA-TEAM` (agents roster). Architect flagged this as suspect — if it fronts a manager read op it belongs inside that manager's derived activity as an effort override. **Verified 2026-08-09:** `webApp/src/routes/TeamView.tsx` imports `utilities/data/team` (static data) and makes no API call. It is genuinely componentless. Additive delta stands.

A screen that crosses managers is the exception, not a reason to weaken the default.

### 1.4 Delta vocabulary — numbers + additive only (Q1)

**Ruling: option (a).** Deltas may:

1. **Override** `effortDays` / `riskBucket` on a derived activity, **with a written justification string stored in the delta**.
2. **Add** an activity that maps to no single component.

Deltas may **not** remove a derived activity, nor rewrite a derived-to-derived dependency edge.

Reasoning: an exclusion delta asserts that a committed component requires no work — which is either false, or an admission that it should not be a component (Directive 2: every component encapsulates a volatility; a volatility that costs nothing to encapsulate is not one). Justified exclusions recreate the zombie failure mode inverted — **a wrong exclusion is silent where a wrong derivation is loud.** Edge overrides break Fig 11-4: if a derived edge is wrong, the *relationship* is wrong; fix slot 5.

**Conditions:**

- **C1 — Doctrine facts live in the System as typed attributes, never in deltas.** Add to slot-5 components: `constructionProfile: generated | handwritten` (code-layer), `provisioning: vendor | owned` (resources), and `uiSurface: bool` (clients). This is how the generated-transport rule, the vendor/owned split, and the SPA-derivation trigger are expressed under a no-exclusion vocabulary. That mcp-client's substance is generator output is an *architectural* fact — it already lives as prose in `encapsulates`. It belongs in the model, reviewed as architecture.

  `uiSurface` is a separate axis from `constructionProfile` and the two must not be collapsed: **web-client is `constructionProfile: generated` *and* `uiSurface: true`.** Its Go transport tier is generator output (so no `C-*` derives), while the browser SPA in front of it is real handwritten work — which is precisely the work the `U-SPA-<manager>` set and `G-SPA` account for.
- **C2 — Additive deltas may not carry a `componentId`.** An added activity bound to a component is a covert exclusion/replacement channel. Additive is for genuinely componentless work: `R-DER`, `U-SPA-6`, `U-SPA-TEAM`, and the 11 checklist noncoding activities.
- **C3 — An added activity declares its own incident edges only.** Adding a node necessarily adds edges; that is additive on the graph, not an override. This is how the `N-CI`→coding and `N-SCHEMA`→store-RA fan-outs are expressed. Derived-to-derived edges stay untouchable. Nonbehavioral (resource contention) dependencies need no authored edges at all — the SSGS resource-leveling in `estimationengine.go` already realizes them from `staffingCap`, which is more faithful to App C §5a than hand-drawn resource edges.
- **C4 — Milestones.** M0–M4 derive; **M5** (`v1 Production Live`) depends entirely on additive noncoding (`N-IT`, `N-HARD`, `N-DEP`, `N-RUN`) and is therefore itself additive. A derived milestone may still *acquire* predecessors from additive activities — M1 derives over the `R-*` vendor set, and the `N-SCHEMA` / `N-CI` deltas attach themselves to it under C3 (an additive activity declares its own incident edges). Derivation fixes the milestone's identity and its derived predecessors, not the closed set of everything that can point at it.

The delta file becomes the entire human-review surface for slot 9 — small, legible, every line defending itself.

### 1.5 Gate deletions

Coverage becomes true by construction, which is strictly stronger than a gate. Delete from `server/cmd/aiarch-state-mcp/`:

- `ACT-COMPONENT-COVERAGE` and `ACT-UNKNOWN-COMPONENT` (`crossartifact.go`, `activityCoverageFindings`)
- the fuzzy normalized longest-key-containment join (`deriveActivityComponent`)
- the System×ActivityList staleness downgrade (`staleness.go`: `systemActivityListJoinRules`, `activityListStaleDowngradeNote`, the `ACT-` entry in `ruleSlotAttributionPrefixes`)

**Do not touch** `PA-RATECARD-*`, `PA-INFRA-KIND`, `PA-TERMS-REGIME`, or the `designhealth` DH-* tier — they are unrelated rule families sharing the same file and enforcement seam.

This removes the rule class that has been blocking construction phase-artifact writes since 2026-07-10.

### 1.6 Effort and risk defaults (Q3)

**Ruling: code emits quantized defaults from the pure layer table; the agent overrides with justification. Signal-driven defaults rejected.**

Signal-driven estimation (contract op count, `encapsulatesVolatilities` length, `atomicBusinessVerbs` count, inbound degree) fails three ways: **service contracts are Phase-3 artifacts** and do not exist at Phase-2 time, so the richest signal is unavailable; a regression over slot-5 metadata is false precision, exactly what App C §4.4 forbids ("strive for accuracy, not precision"); and it makes the baseline churn whenever a relationship is edited, destabilizing estimates for non-estimation reasons.

Defaults are the band midpoints the skill already states:

| Kind | default `effortDays` |
|---|---|
| Manager | 25 |
| Engine | 15 |
| ResourceAccess | 10 |
| Client (handwritten) | 25 |
| Utility | 10 |
| Resource (vendor) | 10 |

`riskBucket` is a dumb table keyed to the effort band: ≤10d → 2–3, 15–20d → 3–5, ≥25d → 5–8 — consistent with the live distribution (risk 1:1, 2:18, 3:28, 5:13, 8:9).

The live effort spread (Managers 20–35, RAs 10–35) means roughly half the defaults get overridden. **That is the desired outcome**: every override is a justified delta, and agent tokens are spent exactly where judgment lives. The Step-5 broadband cross-check stays an authored field — it is the architect's gut, definitionally non-derivable.

Net effect on App C §4.4: the 5-day quantum and the ≤35-day god-activity cap become enforced by construction; accuracy-not-precision is *strengthened*, because the default is an honest band midpoint rather than a pseudo-model.

### 1.7 Architectural placement (Q2)

**Ruling: extend `estimation-engine`. No new component; not the Manager.**

"How the activity inventory is derived from the architecture" is not a distinct volatility — it is the front half of the committed **Construction Estimation Model** volatility (slot 3, sameCustomerOverTime), which `estimation-engine` already declares it encapsulates ("HOW construction duration, cost, and risk are computed") alongside `ComputeNetwork`. Architecture → activities → network → estimate is one computational pipeline over the same typed inputs, changing on the same axis (when Method doctrine changes) for the same consumers. Splitting facets of one volatility into two components is precisely the overfold defect the R-014 projectstate ruling reversed.

Cardinality confirms it: 7 engines against 5 managers already exceeds the App C §3.2 golden ratio (5 managers → ~3 engines). An 8th engine has no defense.

The Manager is disqualified by layer law: Managers encapsulate workflow volatility and are nearly expendable; a pure deterministic derivation buried in `project-design-manager` is unreusable, untestable in isolation, and thickens the layer that must stay thin.

**Cost:** a service-contract amendment on `estimation-engine` plus its detailed design. **No slot-5 amendment for the placement itself, no downstream Phase-2 reshape.** Warranted.

**Condition:** preserve engine purity exactly as today (Option B encapsulation) — the Manager converts typed System/PlanningAssumptions views at the call boundary; the engine performs no I/O and does not import `projectstate`.

### 1.8 Activity ID stability — founder call, ratified

Derived IDs must be a deterministic function of `componentId`. The canonical id becomes `C-<component-id>`; the hand-chosen short form (`C-AA`, `C-BM`) survives as a **render label only**.

**Landmine:** all 69 rows in `.activityConstruction` are keyed by the old short names and every one is Done+Integrated.

**Ratified resolution: alias map, not a key rewrite.** A historical-alias table maps old short keys → derived canonical ids. Rewriting 69 Done+Integrated construction records to gain cosmetic key consistency is risk with no payoff.

### 1.9 Slot-5 amendment — founder call, ratified

C1 requires amending the committed System to add `constructionProfile` and `provisioning`, with the downstream Phase-2 stale-basis cascade that implies. **Ratified: pay it.** The alternative is allowing exclusion deltas, which the architect rejected. This is the difference between a design that cannot drift and one that can.

Consequence to expect: with all three clients marked `generated`, `C-CW` / `C-CM` / `C-CS` cease to derive. `C-CW` currently sits **on the critical path** (`… C-BM → C-CW → U-SPA-S → U-SPA-3 → I-UC3 …`), so the critical path will move. This is a correction, not a regression — those activities contradict standing doctrine.

---

## Part 2 — Compression as network mutation (stage 2)

### 2.1 `criticalSpeedup` is a defect (Q4-i)

Legitimate as the typed form of exactly **one** move — top resources on the critical path (ch. 9's *second* lever; `estimationengine.go:611` cites F5e correctly). A defect as the *entire* compressed solution: the primary lever is parallel work via network mutation (ch. 9 §6, ch. 11 §4, ch. 13 "enabling activities"). The live 1.8× also exceeds the skill's own 1.3–1.5× top-resource range with zero enabling activities behind it. Management is being shown a fabricated point on the time-cost curve — and the risk model correctly refuses it.

**Keep the scalar, demoted to the `topResources` move's parameter.**

### 2.2 Typed move list (Q4-ii)

A typed `moves[]` list in `.compressedSolution`, applied to the derived base network at render-on-read — the same derived+deltas pattern one level up. Tagged union, four kinds:

| Move | Effect |
|---|---|
| `simulator {target, dependents, effortDays, riskBucket}` | inserts `S-<id>`, rewires dependents' edges to it; the real dependency is restored at the `I-*` integration activity |
| `designFirst {target, designEffortDays}` | extracts `D-<id>`; dependents' contract dependency moves to `D-*`, `C-*` depends on `D-*`. The skill is explicit this is the **only** legitimate source of `D-*` activities |
| `topResources {speedup, targets}` | subsumes today's scalar; capped at 1.5 |
| `split {target, parts}` | parallel pipelines. Least mechanical — implement last |

`ComputeNetwork` / `EstimateForOption` evaluate the mutated network unchanged.

### 2.3 Closed catalog, code-driven search, agent-priced moves (Q4-iii)

The book's catalog is itself closed and small; code can hold it. Ch. 11 §4's compression **is** a deterministic greedy loop — compress the current critical path, recompute, repeat — once move costs are known.

Division of labor:

- **Code proposes candidate sites**: critical-path activities ranked by dependent fan-out.
- **The agent prices each candidate move**, and may veto with justification. A ResourceAccess simulator is a trivial fake (~5d); a `billing-manager` simulator carrying real business logic may price at 60% of the real component — at which point the hill-climb rejects it naturally. No special rule needed.
- **Code selects and iterates** against the slot-15 stopping conditions (`maxCompressionPct` 0.3, `tooRiskyThreshold` 0.75, `overSafeThreshold` 0.3) plus App C §4.6's hard caps (≤30% compression, efficiency ≤25%, never the death zone).

**Condition:** the applied move list must surface in `.sdpReview`. Directive 7 requires management to see *what* the compression buys, not a multiplier.

---

## Sequencing

Two stages, designed together so stage 1 does not ship a representation that cannot hold stage 2's moves.

**Stage 1 — derived list + derived network.** Slot-5 attribute amendment (C1) → derivation in `estimation-engine` → delta vocabulary + validation → alias map → gate deletions → doctrine updates.

**Stage 2 — compression moves.** Typed `moves[]` union → move semantics → candidate-site ranking → greedy loop against existing evaluators → `.sdpReview` surfacing.

**Sequencing condition (architect):** `criticalSpeedup` must **not** be removed in stage 1. It is the only compression lever the system has until the move catalog lands.

## Testing

- **Golden-file derivation test.** Derive from the committed slot 5 + planning assumptions; assert the output matches the current 69-activity list modulo the four known corrections (three zombies dropped, three generated-client activities dropped, `R-*` set re-derived from `provisioning`, `U-SPA-*` re-derived per manager). This is the load-bearing test — it proves the deriver reproduces a plan a human architect actually authored and reviewed.
- **Transitive-reduction test** over a fixture reproducing Löwy Fig 11-4 → Fig 11-5 exactly.
- **Delta-vocabulary rejection tests**: additive with a `componentId` rejected (C2); override without a justification rejected; any attempt to express an exclusion or a derived-to-derived edge rewrite rejected.
- **Alias-map round-trip**: every one of the 69 historical `.activityConstruction` keys resolves to exactly one derived canonical id.
- **Engine purity**: `estimation-engine` still imports no `projectstate` and performs no I/O.
- **Move-application tests** (stage 2): each move kind mutates the network as specified and `ComputeNetwork` consumes the result; the greedy loop halts at every slot-15 threshold.

## Earmarks

- `.claude/skills/the-method-activity-list/SKILL.md` and `the-method-compressed-solution/SKILL.md` both need doctrine updates when this ships — the draft-job doctrine section becomes "author the deltas," not "author the list."
- Milestone **M2 still references the zombie `C-HE`**; it is fixed by derivation, but note the current committed network is internally inconsistent until then.
- The `.sdpReview` recommendation will change once a *real* compressed option can clear the 0.75 risk ceiling. Today it recommends `decompressedSolution` partly because the compressed option is a fudge factor that only raises risk.

## Key files

- `.aiarch/state/project.json` — slots 3, 5, 9, 10, 11–15
- `server/internal/engine/estimation/estimationengine.go` — `ComputeNetwork`, `EstimateForOption`, `speedUpCritical` (~611), band policy, SSGS
- `server/cmd/aiarch-state-mcp/crossartifact.go`, `staleness.go` — ACT-* deletions (PA-*/DH-* untouched)
- `webApp/src/routes/TeamView.tsx` — evidence for the `U-SPA-TEAM` additive ruling
- `.claude/skills/the-method-activity-list/SKILL.md`, `the-method-compressed-solution/SKILL.md`
