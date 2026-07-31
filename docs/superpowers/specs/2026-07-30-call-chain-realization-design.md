# Call-Chain Realization — validated use-case tracing

**Date:** 2026-07-30
**Status:** Ratified in brainstorm; pending founder spec review

## 1. Problem

Löwy: *"you validate the decomposition by tracing each core use case as a call
chain through the components (actor → Client → Manager → Engine/ResourceAccess
→ Resource)."*

Today nothing validates that a use case's activity diagram and its dynamic view
describe the same behavior. The two artifacts are disjoint:

- `UseCase.Activity` (`.coreUseCases`) is a structured graph — nodes
  (`start/action/decision/merge/…`) and guarded control-flow edges.
- `DynamicView` (`.systemDesign`) is a flat ordered edge list
  (`participants` + `edges []Relationship`) that references `useCaseId` but no
  activity node. Nothing relates any call to any step.
- The one join field that existed, `ActivityNode.LinkedCompID`, is null across
  all 16 committed use cases, referenced by zero validation rules, and absent
  from methodcheck's parallel type — unenforced doctrine.

A dynamic view can therefore be completely different from the activity diagram
it claims to validate, and no gate notices.

## 2. Ratified decisions

1. **Structural guarantee** — the correspondence is model shape, not prose or a
   mapping table: the System artifact stores, per activity node, the call
   fragment that realizes it.
2. **Hard gate + live checks** — new rules run in the live methodcheck /
   designhealth tiers AND block System draft/commit at Error severity.
3. **All use cases** — core and nonCore variations alike (matches the existing
   every-use-case dynamic-view founder extension).
4. **Step-keyed fragments** — one authoring unit per activity node; paths are
   derived from the activity graph, never stored.
5. **Persons are participants** — C4-style: actors appear in dynamic views;
   every action step has ≥1 call; there is no "no-call" escape hatch
   (`NoCallReason` was considered and rejected).
6. **No new UI shell** — the existing `UseCaseWalkthrough`/`UseCaseCarousel`
   and `DynamicViewFlow` views are wired to the new data.

## 3. Model

In server `projectstate` (schema-first source; all other copies follow):

```go
// DynamicView is the use-case realization: one CallStep per realized
// activity node. Sequence order is derived by walking the activity graph.
type DynamicView struct {
    UseCaseID string     `json:"useCaseId"`
    Key       string     `json:"key"`
    Title     string     `json:"title"`
    Steps     []CallStep `json:"steps"`
}

// CallStep realizes one activity node as an ordered call fragment.
// Endpoints resolve to a Component.ID or an Actor.ID of the owning use case.
type CallStep struct {
    ActivityNodeID string         `json:"activityNodeId"`
    Calls          []Relationship `json:"calls"` // ≥1, ordered
}
```

- **Deleted:** `DynamicView.Participants` (derived: union of call endpoints),
  `DynamicView.Edges` (replaced by `Steps`), `ActivityNode.LinkedCompID`
  (superseded; `RoleName`/`LinkedActorID` stay — swim-lanes are unaffected).
- `Relationship` is reused as the call type: `Mode ∈ {sync, queued}` and the
  destination-layer label vocabulary carry over unchanged.
- Actors come from `UseCase.Actors`; the System's static model stays
  components-only. The realization is the only join point between the two
  artifacts.
- Step eligibility by node kind: `action` **must** have a step; `decision`
  **may** (when evaluating the guard itself requires a call); all other kinds
  (`start/end/merge/fork/join/swimlane/note/loop/switch/goto/interruptEdge`)
  must not.

## 4. Validation

New `CC-*` (call-chain correspondence) family in platform
`framework-go/methodcheck` (authoritative: `putDraftModel` + CI), mirrored in
the app's `designhealth` live tier. All Error severity; System draft/commit is
blocked while any fire (gate activation is staged — advisory during the PoC
checkpoint, hard from the final phase; see §6).

| Rule | Checks |
| --- | --- |
| `CC-STEP-NODE` | every step's `ActivityNodeID` resolves to a node of the owning use case's activity diagram |
| `CC-STEP-UNIQUE` | at most one step per activity node |
| `CC-COVERAGE` | every `action` node has a step; steps on ineligible kinds are illegal; optional on `decision` |
| `CC-STEP-NONEMPTY` | every step has ≥1 call |
| `CC-ENDPOINT-RESOLVES` | every endpoint resolves to exactly one of {component, use-case actor}; dangling or ambiguous ids are Errors |
| `CC-ACTOR-EDGE` | a call touching an actor has a Client-layer component on the other end, mode `sync`, never actor↔actor |
| `CC-PATH-CONNECTED` | for every start→end path (decision branches enumerated, loops taken once, fork branches in declared order — fork-without-join and multiple end nodes supported): concatenated fragments form a connected chain. **Legal fragment roots:** (i) actor→Client — the `clientAction` entry AND mid-chain human re-entry (human gates, operator escalation); (ii) scheduling-Client→Manager — the `timer`/`busMessage` entry (the pump; keeps `scheduler-client` in derived participant sets). Every other call's `From` must already be in the accumulated chain |
| `CC-ACTOR-LANE` | a node carrying `linkedActorId` must have that actor as an endpoint in its step's calls (activity diagrams are amended so only Client-touching human actors use `linkedActorId`; external systems are lanes by `roleName` only) |

Existing rules:

- **Retargeted at step calls:** `DV-EDGE-ENDS`, `DV-EDGE-IN-MODEL`
  (component→component calls only — actor edges have no static counterpart by
  design; matches on `(from, to, mode)`, labels free per call — multiple steps
  reusing one static relationship with different labels is expected),
  `DV-MODE`, `DV-SINGLE-MGR`, `APPC-INT-CLIENT-MULTI-MGR`,
  `APPC-INT-MGR-MULTI-QUEUE`, `DV-STATIC-COVERAGE`, `DV-REL-COVERAGE`
  (computed over the union of fragments), `DV-KEY-UNIQUE`,
  `DV-PLANNED-SKIPPED`.
- `CC-ENDPOINT-RESOLVES` has **no buildStatus exemption**: planned components
  (e.g. `scheduler-client`) resolve by roster id like any other.
- **Retired:** `DV-PART-EXIST`, `DV-PART-USED` (participants are derived),
  `DV-CHAIN-CONNECTED` (superseded by the stronger per-path
  `CC-PATH-CONNECTED`).
- **Unchanged:** `USECASE-DYNAMIC-MISSING`, `ARCH-CHAINCOV`,
  `USECASE-ACTIVITY-MISSING`, all `UC-*`/`CUC-*`/`SYS-*` rules.
- `applyGateSeverityPolicies` staleness note extends to `CC-*`: they are
  System-attributed even though they read CoreUseCases.
- The read-back findings in `systemdesign` manager (`coauthorartifact.go`)
  update to the new shape.

## 5. UI

No new shell; the two existing views consume the realization.

**Adapters (webApp codec)**

- `toDynamicView` linearizes `Steps` deterministically (DFS in authored branch
  order, guards carried into captions), numbers calls globally, tags each call
  with its owning activity step. Participants derived; use-case actors resolve
  to person participants.
- `useCaseViews` carries per-node realization info (step present, calls,
  attributed findings) instead of dropping the linkage.

**DynamicViewFlow (Architecture step, dynamic lens)**

- Person nodes render above the Client layer with a person glyph.
- Two-level step-bar caption: *activity step label — call k/n: call label*.
- Validation reuses the existing `statusBySeq` tinting: calls with an
  attributed `CC-*`/`DV-*` finding render red; finding text in the caption bar.

**UseCaseWalkthrough / UseCaseCarousel**

- Per-node realization badge: ✓ realized / ✗ missing-or-failing (from
  designhealth findings + the realization).
- The walkthrough focus card lists the current step's calls; "View call chain"
  deep-links into the dynamic lens at that step's first call
  (`?view=<key>&step=<n>`, extending `architectureDeepLink`).
- Carousel meta sidebar gains a per-use-case validation chip
  ("9/9 steps realized · all checks pass" or failing rule ids).

Path semantics stay split: the walkthrough is the path-sensitive stepper
(branch buttons); the dynamic lens shows the canonical linearization.

**Error visibility rule:** dangling refs (unknown `ActivityNodeID`,
unresolvable endpoint) are visible error states in the UI — never silently
dropped (today's adapter silently drops unknown participants; that behavior is
removed).

## 6. Migration & rollout

**Model fan-out (4 copies):**

1. Server `projectstate` — schema source regen; `NewSystem` validation;
   read-back findings.
2. Platform `framework-go/methodcheck` — new types + `CC-*` + retargeted
   `DV-*`; framework-go release + archistrator pin bump (release/push remains
   with the founder).
3. Server `designhealth` — hand-rolled decode moves to the new shape; mirrors
   `CC-*`.
4. `framework-go-projectmodel` published slice — unaffected (deliberately does
   not parse dynamicViews). WebApp: OAS + codec regen.

**Committed states — tolerant decode, no legacy rendering.** Old-shape views
decode as zero-step views (never a parse failure — architecture screens must
not render empty). Design Health then truthfully reports every use case
unrealized until the alignment amendment lands. Old `edges` are never rendered
or carried forward.

**Archistrator's own project.json alignment (explicit deliverable).** The
rollout ends with a System amendment on archistrator's own state re-authoring
all 16 dynamic views as step-keyed realizations, gated by the new rules. Per
founder direction, the **system-architect agent is consulted for input**
before/during this amendment — if a chain won't draw, the fix is assessed
against the volatilities (composable-design caution: never reshape a component
so a chain draws). Architecture-impact findings from the pre-spec
system-architect review are in §6a below.

**Platform prompt/skill updates (explicit deliverable).** The doctrine lives in
method-assets and is materialized into every archistrator-built app, so
downstream apps (gtdapp, gtd-qa2, …) learn the new authoring rules:

- `the-method-architecture` SKILL.md: step 9 rewritten (author `Steps`, actors
  as participants, step-eligibility rules); the "back-populate `linkedComp`"
  section deleted; the per-view validation table updated to the `CC-*` reality;
  step 11's PlantUML-carried-on-DynamicView instruction dropped (the field
  never existed).
- system-draft / system-critique command skills updated to author and review
  realizations.
- method-assets release + archistrator pin bump + `.claude` materialization
  (drift gate keeps copies honest).
- Other live projects (gtdapp, gtd-qa2) receive the same amendment treatment
  when next touched; until then their Design Health reports unrealized use
  cases — truthful, not broken.

**Implementation order — proof-of-concept checkpoint (founder QA).** All work
happens on a **new branch created before any change**. The first vertical
slice delivers model + validation + UI end-to-end for **one core use case**:
archistrator's own project.json is amended so that single use case carries a
step-keyed realization (`drive-system-design`, confirmed by §6a — branches,
loops, a mid-chain human re-entry, and 14 realizable action nodes exercise
everything). Then work **stops**: the app is run locally against the real state and
the founder QAs that use case in Chrome — walkthrough badges, dynamic-lens
captions and tinting, deep links, Design Health chips. During the PoC the
`CC-*` rules run in the live tiers (advisory) so one realized use case can be
inspected amid 15 unrealized ones; the hard draft/commit gate flips on in the
final phase together with the full 16-view alignment amendment. Nothing beyond
the PoC slice proceeds without founder sign-off.

## 6a. Architecture impact & amendment scope (system-architect input, 2026-07-30)

**Verdict: no component or relationship changes.** The redesign changes the
shape of a typed artifact owned by the already-encapsulated typed-state
volatility behind `project-state-access`; the new rules ride the existing
`system-design-manager → design-health` edge (its signature already takes the
whole project, so cross-slot validation fits); the hard gate is the existing
slot-5 staging path. Every fix the amendment forces is an activity-diagram or
realization-authoring fix — zero static-model surgery.

**Rulings folded into the design (from the architect's gap list):**

1. **Connectivity is root-aware** — `CC-PATH-CONNECTED` legal fragment roots
   are actor→Client (entry and mid-chain human re-entry: `human-gate`,
   `escalate-operator`) and scheduling-Client→Manager (the pump entry for
   `timer`/`busMessage`). Without these, correct designs fail
   (`drive-system-design`, `execute-a-construction-activity`,
   `operate-a-delivered-system`).
2. **Scheduler entry is accepted**, not omitted — otherwise `scheduler-client`
   (buildStatus=planned) appears in zero derived participant sets and vanishes
   from every render.
3. **R4 cross-surface equivalence**: realizations use ONE canonical Client
   (web-client); the 9 old dual-entry (`web-client` + `mcp-client`) duplicate
   edges are not carried per step. `mcp-client`'s resulting
   `DV-STATIC-COVERAGE` miss is covered by a waiver backed by the R4
   attestation (same Manager entry, cross-surface equivalence).
4. **`linkedActorId` reconciliation** — `CC-ACTOR-LANE` (see §4) plus activity
   amendments clearing external-system "actors" that can never legally call a
   Client (`payment-provider` on `charge-user`, `customer` on
   `customer-charged`, `settlement-manager`, `operated-system`,
   `infrastructure`).
5. **Trigger taxonomy fixes ride the amendment**:
   `replan-under-scope-change` (busMessage with no queued static realization —
   reclassify to timer/sweep) and `operate-a-delivered-system` (dual-trigger
   fork-without-join, two end nodes — split into per-trigger realizations or
   reshape the fork at the use-case level).
6. **Activity-diagram touch-ups, not new calls**: nodes with no honest
   realization become notes or fold into neighbors (`customer-charged`,
   `in-flight`, `argo-reconcile`).
7. **Entry-call convention**: the actor→Client + Client→Manager entry calls
   ride on the first action node (no "user initiates" node exists) — documented
   in the rewritten step-9 doctrine.
8. **Keys/titles preserved** (`uc1-*`/`var-*`) — they are deep-link anchors.

**Amendment scope facts:** 16 views re-authored; 125 action nodes across all
16 diagrams need steps (vs 131 old flat edges); sampled walks
(`view-the-project-state-log`, `drive-system-design`,
`bill-the-user-for-usage`, plus cross-checks) found every action node
realizable from the existing static relationship set once the touch-ups above
land. `drive-system-design`: all 14 action nodes realizable — confirmed as the
PoC candidate.

**Collateral slots the amendment must carry:**

- Slot-5 attestations re-staged; Directives 3 and 4 re-worded against
  step-keyed realizations (they cite dynamic views / "new call chains").
  Existing waivers carry over unchanged; no committed waiver cites a retired
  rule id.
- `serviceContracts.projectStateAccess` (committed Phase-3 contract):
  `$defs.DynamicView` regenerated to `steps[]`/CallStep with actor-capable
  endpoints; `$defs.ActivityNode` loses `linkedCompId`.
- Downstream committed slots (6, 8–16): fresh reviewed-unaffected/amend
  staleness pass after the slot-5 amendment (slot 7 torn down — skip).
- `activityConstruction.U-SPA-1` records: historical, untouched; the webApp
  change is new construction scope.

## 7. Testing

- **methodcheck:** table-driven unit tests per `CC-*` rule and each retargeted
  `DV-*` rule; path-walker tests (branch fan-out, loop-taken-once,
  fork-in-declared-order, fork-without-join, multiple end nodes, actor-rooted
  vs scheduler-rooted chains, mid-chain actor re-entry).
- **Server:** regen compiles; designhealth rule tests; gate tests proving a
  System draft/commit with a failing `CC-*` is rejected at `putDraftModel` and
  at commit.
- **WebApp:** adapter tests (deterministic linearization, person resolution,
  per-step tagging, dangling-ref surfacing); component tests for badges and
  captions; Playwright against archistrator's real state, stopping for founder
  review per UI change (established UI review loop).

## 8. Non-goals & earmarks

**Non-goals (v1):**

- No semantic matching of prose labels — correspondence is authored; structure
  is machine-checked.
- The doctrine row "the last edge produces the outcome named in the use case"
  stays a reviewer judgment, not a mechanical rule.
- No Structurizr DSL emitter (still doctrine-only, unchanged by this work).

**Earmarks:**

- Validate call labels against committed service contracts (Phase-3 join) once
  a component has one.
- The unimplemented doctrine rule "no dynamic-view edge targets an
  infrastructure ResourceAccess."
- Meaningful-action-label and destination-layer-vocabulary label rules (prose
  quality — likely critique-tier, not mechanical).
