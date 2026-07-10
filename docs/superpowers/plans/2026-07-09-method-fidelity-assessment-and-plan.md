# The Method Fidelity Assessment & Improvement Plan

**Date:** 2026-07-09 · **Basis:** full re-read of *Righting Software* (ch. 1–14 + App A/B/C) against the current
archistrator + archistrator-platform implementation (branch `appgen-step4`, project.json v580, phase 2).

**Goals ratified for this plan (founder, 2026-07-09):**

1. Workflows in archistrator match the workflows defined in the book.
2. A smooth system-design experience: use cases → contracts → real software.
3. Maximum codegen from design/contract schemas; lints/tests for every Method constraint codegen can't
   enforce; agents carry as little responsibility as possible; architecture and code validate against the
   Method rules — including static↔dynamic diagram consistency.
4. Human review & intervention policies: a human can stop and approve before continuing at **any** step of
   the workflow in **any** construction activity.
5. Excellent estimation of agent completion cost using the book's project-design + Appendix A techniques.

---

## 0. Verdict

The skeleton is genuinely the Method — typed 17-slot state, a ~50-rule `methodcheck` engine, a
phase-gated design rail, a per-activity construction pump with the App-A phase lifecycle, and a codegen
pipeline that already leaves agents only method bodies. Nobody else has built this. But the system today is
**a Method-shaped machine with the Method's *judgment* still living in prose**, and that prose has drifted:
the doctrine exists in two independently-evolving copies (`.claude/skills` and `prompts.go`), several skills
contradict the book or the schema outright, the deepest validator doesn't run in CI, contract rules validate
the wrong data, Phase-2 math (floats, risk, EV curves) is not typed state, and the human-gating and
tracking loops — the book's two run-time control systems — are stubs.

The bold move this plan proposes: **stop treating the book's rules as prompt guidance and finish making
them schema + validator + generator.** Prompts should only carry what genuinely requires judgment
(volatility identification, factoring, naming semantics); everything Löwy expressed as a number, a graph
property, or a formula becomes typed state that `methodcheck`/`projectcheck`/`contractcheck` enforce and
generators consume. That is Löwy's own split: *"any two architects analyzing the same project network
should produce very comparable results"* — everything downstream of the network is deterministic and
tool-computable (ch. 8). Archistrator's advantage over a human org is that its developers are metered:
**token spend is direct cost, phase exits are machine events — Appendix A tracking can run with perfect
data, automatically.** No human organization has ever had that.

---

## 1. Workflow fidelity scorecard (book → implementation)

### Phase 1 — System Design

| Book step (ch. 2–5) | Implementation | Verdict |
|---|---|---|
| Vision → objectives → mission, bidirectional traceability | `.mission` slot, business-alignment skill | ✅ faithful |
| Glossary (Four Questions) + scrub solutions-masquerading | `.glossary` + `.scrubbedRequirements` | ✅ faithful |
| Volatilities on the two axes, volatile-vs-variable gate | `.volatilities`, `VOL-AXIS` rule | ✅ faithful |
| Core use cases 2–6 via abstraction | `.coreUseCases` (5 core + 12 nonCore), `CUC-CARD` | ✅ faithful |
| Layered decomposition, closed architecture | `.systemDesign` typed System, methodcheck graph rules | ✅ mostly (§3 gaps) |
| Operational concepts justified per objective | `.operationalConcepts` | ✅ faithful |
| Validation: call chain per **core** use case | DynamicView per **every** use case (deliberate founder extension) | ✅ extension, keep |
| Standard check (App C walk) | `.standardCheck` skill | ⚠️ **contains an inverted rule** (§2.1) |
| Timebox: design 3–5 days | agent-drafted, human-gated — hours | ✅ exceeds book |

### Phase 2 — Project Design

| Book step (ch. 7–12, ch. 14 §"Design of Project Design") | Implementation | Verdict |
|---|---|---|
| Activity list: coding (one per component) + noncoding, 5-day quanta | `.activityList`, clock-correction doctrine | ✅ (typing gaps §4) |
| Network + total/free floats, critical path | `.network` has deps/CP/milestones — **no floats persisted** | ⚠️ floats recomputed ad-hoc |
| Normal / subcritical / compressed / decompressed solutions | 4 Solution slots — **knobs only**, no per-option network state | ⚠️ schema thinner than doctrine |
| Risk: criticality + activity (+ Fibonacci arbitration, outlier fix) | estimation engine has criticality/activity; `.riskModel` = rows only, no curves/exclusions/recommendation home | ⚠️ partial |
| Time-cost / time-risk curves, exclusion zones, death zone | rendered in UI from rows; not validated | ⚠️ display-only |
| SDP review: 3 options ideal, subcritical shown-to-reject, never round risk | `.sdpReview` options + recommendation; decision recorded — `chosenOption` has **no schema home** | ⚠️ |
| Project-design standard check (App C §4, ~46 machine-checkable rules) | prose skill walk only — **zero rules in methodcheck** | ❌ biggest validator gap |

### Phase 3 — Construction

| Book doctrine (ch. 13–14, App A/B) | Implementation | Verdict |
|---|---|---|
| Hand-off model decision (senior / junior-architect / junior) | handoff skill → **nonexistent `.handoff` slot**; real home `.constructionProgress.HandOffModel` | ⚠️ contradiction |
| Contract designed per component before construction, architect-reviewed | service profile: requirements → detailed_design(contract) → test_plan → construction → integration | ✅ faithful spine |
| Contract rules (App B: 3–5 ops, ≤12, reject ≥20, no properties, ≤2/service, factoring) | size rules run on `atomicBusinessVerbs` of the *architecture slot*, **not the real `.serviceContracts`**; no property-ban, no factoring lints | ❌ wrong data |
| Every phase exit is a binary review gate | `runPhaseGate` exists but `ReviewPolicy` is `{}` and reviewer routing is display-only | ❌ goal-4 gap |
| Weekly tracking: EV/effort/projections, corrective actions | 1 EV point, linear planned basis, `RunReplanSweep` returns nil | ❌ goal-5 gap |
| Scope change → re-run project design | `/sdp-review` command procedure exists; no automated trigger from tracking | ⚠️ manual only |
| Testing doctrine (STP early/high-float, harness, system testing, QA≠QC) | testingState slots + N-* activities + role agents | ✅ faithful |
| Deployment/documentation activities | 6 command stubs exist but `DeriveType` can't reach them | ⚠️ undispatchable |

---

## 2. Defect register (fix-first, independent of workstreams)

**2.1 Doctrine bugs (wrong, not just missing):**
- `the-method-system-design-standard-check` item 2d demands *"more Engines than Managers (2:1 favoring
  Engines)"* — **inverted**. Book: fewer Engines than Managers (golden ratio 1→0/1, 2→1, 3→2, 5→3;
  8 Managers = failed design). As written it fails every correct design.
- Same skill's §I greps `architecture.dsl` for Temporal-primitive edge-label prefixes that
  `the-method-architecture` explicitly bans from `Relationship.Label`.
- "Mercedes test" is not book language (verified absent). The doctrinal anchor is **Design for Your
  Competitors** (ch. 2) / the anti-design effort. Rename wherever used.
- `/project-design` Step 2 and `NETWORK-SCHEMA.md` still carry the pre-clock-correction taxonomy
  (separate detailed-design + construction activities) contradicting the activity-list skill.
- `/implement-project` describes a whole second Phase-3 orchestration model (type-routing, self-marked
  status, `aiarch/construct/<id>` branches, a nonexistent `devops` agent) that contradicts the live
  pump+stub rail. Delete or rewrite as a thin console-driving procedure.

**2.2 Schema/state contradictions:**
- `.handoff` slot referenced by 4 skills/commands; does not exist.
- Flat-key fiction (`.mission`, `.compressedSolution`…) in every skill vs actual `slots["0".."16"].model`.
- Skills prescribe writes to Manager-owned read-only fields (`.constructionProgress.points`,
  `.activityConstruction`) via verbs that are AgentHidden.
- `chosen_option`/`start_date` on `.network`, `Notes`/`CritiqueNotes` on SdpReview: no schema homes.
- 3 orphan `.serviceContracts` entries with no systemDesign component (`artifactRenderingAccess`,
  `artifactValidationEngine`, `systemDesignEngine`); `workItemAccess` component has no contract.

**2.3 Enforcement holes:**
- `methodcheck` full suite (`-tags methoddesign`) **not run by archistrator's own CI** — hand-committed
  state bypasses everything except the 4-rule projectmodel loader.
- `internaltoolsgen` has no CI drift gate.
- 15 static relationships never exercised by any dynamic view (13 are utility/security edges — policy
  decision needed; 2 are real: `project-design-manager → construction-pipeline-access` and
  `→ source-control-access`). `DV-REL-COVERAGE` is Warn today.
- Pump's activity↔component join is a title-substring heuristic; unmatched activities silently skipped.
- Two supply-chain postures for aiarch-state-mcp (pinned in design rail, built-from-checkout in construct).

**2.4 Hygiene:** dead `validate-structurizr.sh` hook + structurizr helpers; dead book paths in 3 skills;
vestigial `<product>` args; MCP boilerplate on only 16/30 stubs; `/sdp-review` command/skill name
collision; `cmd/validate` dead weight.

---

## 3. Workstream A — One Doctrine, One Schema (the kernel)

*Make project.json's typed schema the single normative statement of the Method; everything else renders
from it.*

**A1. Type the missing Phase-2/3 state** (modelgen makes this cheap — schema first, Go emitted):
- `Network`: persist per-activity **ES/EF/LS/LF/totalFloat/freeFloat** as *computed* fields with
  provenance (engine-computed on commit, never authored), plus `chosenOption`, `startDate`, and typed
  `Milestone{id, event, public|private}`.
- `Activity`: add typed `kind` (service/frontend/testing/deployment/documentation + variant) and
  `componentId` (validated against `.systemDesign`) — kill `DeriveType`-by-prefix and the
  title-substring join. The webApp's `METHOD_METADATA` derivation becomes a fallback renderer, not truth.
- `Solution`: carry the option's **resource-assigned network state** (assignments by float, per-option
  critical path, duration, direct/indirect cost, efficiency, planned-EV curve points) — computed by the
  estimation engine, stored with provenance, exactly as the option skills already claim.
- `RiskModel`: rows + fitted curve coefficients + R² + exclusion verdicts + crossover points +
  recommendation.
- `SdpReview`: decision record (chosen option, decider, date, rationale) + revision history (scope-change
  re-reviews append).
- `ConstructionProgress`: typed `HandOffModel` decision record (kills the `.handoff` ghost); typed
  tracking points (week, earnedPct, effortPct = **token spend / planned spend**, projection outputs).
- `ReviewPolicy`: typed per `(activityKind, phase)` → `{auto | agentReview | human | humanAfterAgents}`
  with global default **human** (see Workstream D).

**A2. Single doctrine source.** Today the Method prose lives in `.claude/skills` (hand rail) *and*
`server/internal/manager/*/prompts.go` (automated rail) and they already carry divergent founder
extensions. Restructure: one `doctrine/` asset tree (per artifact kind: procedure, checklists, book refs,
founder extensions marked as such), from which (a) the skills' SKILL.md bodies are assembled and (b)
`prompts.go` draft-task strings are `go:embed`-ed. A drift test fails CI if either copy is edited directly.

**A3. Schema-truth rendering.** Generate the slot-map reference (what `the-method-project-state`
hand-maintains) from the Go types; fix every skill's flat-key references in the same pass A2 rewrites them.

**A4. Fix the defect register (§2) as the first commits of this workstream.**

---

## 4. Workstream B — The Validator is the Product (static validation supremacy)

*Every rule the book states as a number, graph property, or formula becomes a checker. Three suites:*

**B1. `methodcheck` (system design) — close the book gaps:**
- **Naming grammar** (ch. 3): two-part PascalCase; suffix ∈ {Manager, Engine, Access}; gerund prefixes
  only on Engines; no atomic business verbs in component prefixes; RA op names must not be CRUD/IO
  verbs (`Select/Insert/Delete/Open/Close/Read/Write/Seek` ⇒ resource leak, Error).
- **Golden-ratio bands** as the book's table (1→0–1, 2→1, 3→2, 5→3, ≥8 Managers = Error), replacing the
  blunt `Engines>Managers` warn.
- **Symmetry heuristic** (ch. 3): compute a call-pattern signature per Manager per dynamic view
  (calls-engine? calls-RA? publishes? queues?); flag Managers whose use cases diverge in pattern
  (presence/absence asymmetry) — Warn, it's a smell not a law.
- **≤1 queued Manager target per use case** (verify `DV-SINGLE-MGR` implements exactly this; the book's
  fix is "use Pub/Sub instead" — say so in the finding).
- **Static↔dynamic coverage promotion** (the founder's core ask): keep `DV-EDGE-IN-MODEL` (no invented
  edges) at Error; **promote `DV-REL-COVERAGE` (every static relationship appears in ≥1 dynamic view) from
  Warn to Error, with a policy exemption for Utility-bar and Security edges** (house diagram convention:
  utilities draw no lines). Fix the 2 real uncovered `project-design-manager` edges in the dogfood state.
- **Contract↔architecture bijection**: every Manager/Engine/RA component has exactly one
  `.serviceContracts` entry and vice versa (orphans in either direction = Error). Kills §2.2's drift class.
- Volatility gradient / reuse gradient / almost-expendable Manager stay judgment items in the standard
  check — but add the proxy: Manager contract op-count vs Engine+RA fan-out ratio as Info.

**B2. `contractcheck` (App B) — run against the REAL `.serviceContracts`:**
- Size ladder on actual `interface.operations`: 1 op Warn · 3–5 pass · 6–9 Info · ≥12 Error · **≥20
  non-waivable Fail**.
- **Property-like operation ban**: `Get*/Set*` pairs, zero-arg scalar getters, attribute-CRUD shapes ⇒ Error.
- ≤2 contracts per service (cross-cutting facets whitelistable).
- **Factoring lints**: duplicate op signatures across contracts (suggest factor-up); mixed facets — business
  op + infrastructure op in one contract (suggest factor-sideways); op meaningless for an implementer
  (down) stays judgment.
- Contract churn metric: revisions of a frozen contract tracked; a contract that changes every time
  requirements change is the book's "hallmark of bad design" (Warn, trend-based).
- Meta-rule preserved in tooling posture: **metric-pass never auto-approves; metric-fail always blocks**
  (App B: evaluation not validation).

**B3. `projectcheck` (App C §4/§5 — the missing suite, ~46 rules).** Highest-value new build. Over
`.activityList`+`.network`+solutions+`.riskModel`+`.sdpReview`+`.constructionProgress`:
- Topology: single start/end; every activity on a chain ending on the critical path; P=1; explicit resource-
  dependency edges when float was consumed; network cyclomatic complexity E−N+2P reported, healthy 10–12.
- Activities: durations ≡ 0 (mod 5), 5–35d, ≥40d = god-activity Error (also >mean+1σ); noncoding
  presence check (test plan, harness, system testing, deployment, documentation, front-end); front end
  15–25% of duration.
- Staffing/EV: assignment order matches ascending float; 1:1 component:worker at any instant; planned EV
  is a **shallow S** (reject linear/hockey-stick/early-plateau by curve-shape test); efficiency 15–25%
  (>25% Warn, ≥40% Error).
- Compression/risk: compression ≤30%; compressed-only-critical-activities; death-zone auto-reject of any
  chosen option below the total-cost curve; risk bands 0.3–0.75 all options, normal ≤0.7, decompression
  target 0.5 (±band), da-Vinci sanity on the curve; Fibonacci arbitration when criticality vs activity risk
  diverge; float-outlier adjustment (replace >1σ with mean+1σ) applied before activity risk.
- SDP: ≥3 options with genuinely differing (duration, cost, risk); subcritical present (to be rejected);
  risk numbers never rounded (validator checks precision preserved).
- Tracking (App C §5): binary exits only; phase weights sum 100% consistently; cadence respected;
  progress reported on integrations never features; near-critical float time-series exists.

**B4. Wire it all into CI + write gates:** `make method-check` (or its successor `aiarch validate`) becomes
an untagged required check in `server-checks.yml` AND stays in the MCP write gate; the same binary is the
required PR check in user project repos (already true) — one validator, three seats. `internaltoolsgen`
gets a drift gate. Unify `mcpemit` vs `framework-go-mcp-generator` (pick one, delete the other).

---

## 5. Workstream C — Codegen Completion (agents write bodies, nothing else)

*Current state is already strong: modelgen + temporalgen + httpgen + typed OAS leave an agent exactly
Manager workflow bodies, RA method bodies, Engine bodies. Finish the last mile:*

- **C1. `composegen`** (spec step 8): generated `main.gen.go`/`config.gen.go` composition root — DI wiring
  is pure contract data; a hand-written main is an agent liability. Archistrator's own `cmd/server/main.go`
  converts to it first (dogfood).
- **C2. `transportgen`** (spec step 5) + deployment model (step 7) + delivered-app packaging (9–10),
  per the approved 2026-07-06 spec.
- **C3. Generated target-app CI**: every generated repo ships `arch.Check` + `CheckGeneratedSurface` +
  `methodcheck` + golangci config + the construct workflow — the Method gates travel with the product.
  (This *is* the business: customers' apps validate against the Method forever.)
- **C4. Construction briefs, generated not prompted**: per activity, render the agent's full context from
  typed state — the frozen contract, the dynamic views the component participates in (its exact edges =
  its call obligations), its STP slice, its deps' contracts, DONE-means criteria from the phase profile.
  The 30 stubs shrink to near-nothing; doctrinal edits stop requiring 4–6 synchronized file touches.
- **C5. Test scaffolds from STP** generalized to target apps (the `gen-systemtests` pattern).
- **C6. Dynamic-view ↔ contract op linting**: every dynamic-view edge label that names an operation must
  resolve to an op on the target component's contract (and conversely, unreferenced ops reported Info).
  This closes the last "made-up relationship" class: diagrams ↔ contracts ↔ code all reconciled.

---

## 6. Workstream D — Universal Human Gating (goal 4)

*Book basis: every phase exit is a review (App A); reviews are the binary exit criteria. So the gate
substrate already matches the book — make policy real and the human sovereign:*

- **D1. Typed `ReviewPolicy`** (A1): per `(activityKind, phase)` → gate mode; **default: human approval on
  every phase exit of every activity**; relaxation is an explicit policy edit (audited in state). Empty
  policy today silently means whatever `RequiresHuman` defaults to — make the default the book's posture.
- **D2. Enforce reviewer routing**: `reviewEngine.ProposeReviews` output stops being display-only. Gate
  modes `agentReview`/`humanAfterAgents` dispatch actual reviewer jobs (same pipeline substrate, `review`
  job mode; reviewer role = computed set: contract author for code, architect for detailed design,
  ui quad for ui-design), collect typed verdicts (pass/fail/amend + mayAmend semantics), aggregate, then
  suspend for the human if policy requires. Verdicts recorded in `.activityConstruction`.
- **D3. Mid-phase intervention**: pause is project-level today; add per-activity pause + cancel-current-
  pipeline + takeover at any point (the pump already supports override verbs — surface them per phase, not
  just on variance).
- **D4. One intervention idiom in the UI**: merge `InterventionDrawer`'s dead OperatorBar with the live
  `ActivityTrackingDetail` verbs; every gate (design artifact, SDP, phase exit, deploy) renders through one
  approval surface with the artifact diff and the reviewer verdicts.
- **D5. Wire deployment/documentation dispatch** (`R-*`→Deployment, N-ADR/N-DEP disambiguation) so all 30
  stubs are reachable and therefore gate-able — "any construction activity" includes those six.

---

## 7. Workstream E — Estimation & Tracking Excellence (goal 5)

*The book's deepest promise, and archistrator's structural advantage: agent cost is metered per token and
phase exits are machine events. App A can run continuously with perfect data.*

- **E1. Direct cost = token spend.** Record per-pipeline-run token usage (+ wall time) against
  `(activity, phase)`. Effort formula `C(t)=S(t)/R` becomes exact: S = metered spend, R = estimated spend
  (airates × PERT effort). Indirect cost = server/CI overhead per calendar time. The EV chart gets its
  honest AC line back.
- **E2. PERT-per-kind priors, throughput-calibrated.** Per activity kind × layer, maintain
  (O, M, P) spend/duration priors; **every completed activity updates the posterior** (the book's
  "measured team throughput," continuous instead of monthly). Estimation engine already computes EV —
  extend to projections: regression over ≥4 tracking points → projected completion date + projected cost
  overrun, exactly App A's geometry.
- **E3. Planned EV as true shallow-S** derived from the chosen option's network (not linear basis);
  auto-record a tracking point on every phase exit (weights from the activity profile: 15/20/10/40/15) —
  the Manager owns this; skills stop pretending agents write `.constructionProgress`.
- **E4. Corrective-action engine**: implement `flagVariances` — classify the live pattern
  (underestimating / overestimating / leak-analog = spend without phase exits) per App A §5, propose the
  book's actions (revise estimates via measured throughput, reduce scope, change solution option), and
  **event-trigger the scope-change rail** (`/sdp-review` re-plan) instead of waiting for a human to notice.
- **E5. SDP options priced in dollars**: duration (wall-clock), cost (projected token+infra spend in
  currency via airates), risk (full formula set) — Directive 7 with real numbers. Never round risk.
- **E6. Risk completeness**: persist criticality + activity + Fibonacci per option (A1 schema), outlier
  adjustment, geometric variant as god-activity detector, crossover computation — all in the estimation
  engine, all validated by projectcheck (B3).

---

## 8. Workstream F — Design Experience (goal 2)

- **F1. Use-case → contract → code thread in the UI**: from a dynamic-view edge, jump to the contract op
  it names (C6 makes the join typed); from a contract op, see which dynamic views exercise it and which
  activity built it. The full thread — use case → call chain → contract → activity → PR — becomes clickable.
- **F2. Research corpus management**: multi-source, growable after start, visible corpus panel (today:
  one-shot title+content offered only on 409).
- **F3. Ask everywhere**: extend Phase-1 Ask (pm/architect Q&A without redraft) to Phase 2 and
  construction gates.
- **F4. Reachable operations**: link operated apps from the project (deploy-after-construction →
  operations console bridge); today the route has zero inbound links.
- **F5. SDP presentation doctrine**: 3-option default framing, subcritical labeled "shown to be
  rejected," Richter-scale risk framing, plain-language option names (the book's ch. 13 renames).
- **F6. Validator visibility**: every gate shows its machine findings today — extend to the new suites
  (contractcheck at detailed-design gates, projectcheck at Phase-2 gates) so the human approves with the
  full rule report in view.

---

## 9. Sequencing

**Now (days) — correctness quick wins:** §2 defect register: inverted ratio rule, `.handoff` ghost,
stale `/project-design` step 2 + NETWORK-SCHEMA, dead hook/paths, orphan contracts + 2 uncovered edges in
dogfood state, method-check into CI (B4 first slice), MCP boilerplate on all 30 stubs.

**Next (1–2 weeks each, parallelizable):**
1. **A1 schema extensions** (unblocks B3, E*, D1 — most other work reads these types).
2. **B2 contractcheck on real contracts** + B1 gaps (naming grammar, coverage promotion, bijection).
3. **D1+D2 review policy + enforced routing** (goal 4 end-to-end).
4. **C1 composegen** (+ C4 generated briefs).

**Then:** B3 projectcheck suite · E1–E4 tracking loop · C2 transportgen/deployment · A2 doctrine
unification · F* UX thread.

**Doctrine of the whole plan:** if the book states it as a number, we validate it; if it's structure, we
generate it; if it's judgment, we prompt it — and a human can stop the machine at every gate.
