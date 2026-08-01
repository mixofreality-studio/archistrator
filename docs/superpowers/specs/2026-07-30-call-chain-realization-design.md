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
    ActivityNodeID string      `json:"activityNodeId"`
    Calls          []TraceCall `json:"calls"` // ≥1, ordered
}

// TraceCall is one call within a step (rollout rulings 2026-07-31): the same
// fields as Relationship plus an optional alt-group tag. Calls in one step
// sharing an Alt value are surface-alternatives — equivalent entries, not a
// sequence.
type TraceCall struct {
    From  string   `json:"from"`
    To    string   `json:"to"`
    Mode  CallMode `json:"mode"`
    Label string   `json:"label"`
    Alt   *string  `json:"alt,omitempty"`
}
```

- **Deleted:** `DynamicView.Participants` (derived: union of call endpoints),
  `DynamicView.Edges` (replaced by `Steps`), `ActivityNode.LinkedCompID`
  (superseded; `RoleName`/`LinkedActorID` stay — swim-lanes are unaffected).
- `Relationship` is reused as the call type: `Mode ∈ {sync, queued}` and the
  destination-layer label vocabulary carry over unchanged.
- **`CallStep.Calls` is now `[]TraceCall`** (rollout rulings 2026-07-31): same
  fields as `Relationship` plus optional `Alt *string` (wire `alt`, optional).
  Calls in one step sharing an `alt` value are surface-alternatives —
  equivalent entries, not a sequence.
- Actors come from `UseCase.Actors`; the System's static model stays
  components-only. The realization is the only join point between the two
  artifacts.
- **New activity node kinds (UML-faithful triggers, founder ruling
  2026-07-30):** `ActivityNodeKind` gains `NodeTimeEvent` (accept time event —
  the UML hourglass) and `NodeAcceptEvent` (accept event action) — appended
  after `NodeInterruptEdge` (the iota ordering is load-bearing). Event nodes
  may have **no incoming edge** — standard UML alternative entries — and
  `UC-ACTDIAG` well-formedness accepts that. Every `timer`/`busMessage` use
  case models its trigger as an event node instead of a pseudo-action.
- **`ActivityNode.DecidedBy`** (rollout rulings 2026-07-31): optional
  `*string` (wire `decidedBy`), legal ONLY on `decision`/`switch` kinds;
  resolves like a call endpoint — a `Component.ID` or the owning use case's
  `Actor.ID`. Illegal placement (any other kind) or a value that resolves to
  neither is `CC-DECIDED-BY` (§4).
- Step eligibility by node kind: `action` **must** have a step; `timeEvent` /
  `acceptEvent` **must** (they realize the trigger entry); `decision` **may**
  (when evaluating the guard itself requires a call); all other kinds
  (`start/end/merge/fork/join/swimlane/note/loop/switch/goto/interruptEdge`)
  must not.

## 4. Validation

New `CC-*` (call-chain correspondence) family in platform
`framework-go/methodcheck` (authoritative: `putDraftModel` + CI), mirrored in
the app's `designhealth` live tier. All Error severity; System draft/commit is
blocked while any fire — the staging is implemented as the `ccGateSeverity` /
`ccLiveSeverity` constants (`Warning` in the PoC; the post-QA phase flips them
to `Error`), and the retargeted `DV-STATIC-COVERAGE`/`DV-REL-COVERAGE` ride the
same constants (see §6). Two more rules join the same staging (rollout
rulings 2026-07-31): `CUC-ACTOR-REQUIRED` and `CC-DECIDED-BY` both ride
`ccGateSeverity` — `CUC-ACTOR-REQUIRED` despite being CoreUseCases-attributed
rather than System-attributed, the same staleness exception the
`applyGateSeverityPolicies` note below already carves out for the rest of
this family.

| Rule | Checks |
| --- | --- |
| `CC-VIEW-USECASE` | a dynamic view whose `useCaseId` names no use case in the committed set fires (all other CC rules skip an unresolvable view — this is the guard) |
| `CC-STEP-NODE` | every step's `ActivityNodeID` resolves to a node of the owning use case's activity diagram |
| `CC-STEP-UNIQUE` | at most one step per activity node |
| `CC-COVERAGE` | every `action`, `timeEvent`, and `acceptEvent` node has a step; steps on ineligible kinds are illegal; optional on `decision` |
| `CC-TRIGGER-EVENT` | trigger↔diagram alignment: `timer` diagrams have ≥1 `timeEvent` entry (no incoming edge), `busMessage` ≥1 `acceptEvent` entry, `clientAction` no event-node entries — the trigger taxonomy is machine-checked against the diagram |
| `CC-STEP-NONEMPTY` | every step has ≥1 call |
| `CC-ENDPOINT-RESOLVES` | every endpoint resolves to exactly one of {component, use-case actor}; dangling or ambiguous ids are Errors |
| `CC-ACTOR-EDGE` | a call touching an actor has a Client-layer component on the other end, mode `sync`, never actor↔actor |
| `CC-PATH-CONNECTED` | for every entry→end path — entries are the initial node and any event node; decision branches enumerated, loops taken once, fork branches in declared order; fork-without-join and multiple end nodes supported — concatenated fragments form a connected chain. **Roots are model-driven:** a path from the initial node of a `clientAction` use case roots with actor→Client (both-surface entries legal: multiple equivalent actor→Client + Client→Manager calls in one entry step); a path from a `timeEvent` node roots with scheduling-Client→Manager; from an `acceptEvent` node, with the queued call into the receiving Manager. Mid-chain actor→Client re-entry (human gates, operator escalation) starts a new legal fragment. Every other call's `From` must already be in the accumulated chain. **Alternative groups don't change this (rollout rulings 2026-07-31):** every call sharing an `alt` value seeds `reached`; the `1a`/`1b` numbering is presentation, not a distinct path branch |
| `CC-ACTOR-LANE` | a node carrying `linkedActorId` must have that actor as an endpoint in its step's calls (activity diagrams are amended so only Client-touching human actors use `linkedActorId`; external systems are lanes by `roleName` only) |
| `CUC-ACTOR-REQUIRED` | a `clientAction` use case with zero actors fires — coreUseCases-scoped, section `"useCase "+id` (rollout rulings 2026-07-31) |
| `CC-DECIDED-BY` | a `decidedBy` that resolves to neither a component nor an owning-use-case actor, or that appears on a non-`decision`/`switch` kind, fires — step-scoped/use-case-scoped per site (rollout rulings 2026-07-31) |

Existing rules:

- **Retargeted at step calls:** `DV-EDGE-ENDS`, `DV-EDGE-IN-MODEL`
  (component→component calls only — actor edges have no static counterpart by
  design; matches on `(from, to, mode)`, labels free per call — multiple steps
  reusing one static relationship with different labels is expected),
  `DV-MODE`, `DV-SINGLE-MGR` (all Client entries must target one and the same
  Manager — R4 both-surface entries stay legal), `APPC-INT-CLIENT-MULTI-MGR`,
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
- **Section-grammar alignment (rollout rulings 2026-07-31):** the platform
  tier's CC Section strings move to key-first — `dynamicView <key>` (falling
  back to `useCaseId` only when `key` is empty) view-scoped, `dynamicView
  <key> step <nodeId>` step-scoped, `useCase <ucId>` use-case-scoped —
  matching the app's existing `designhealth` construction (`ccViewLabel`).
  This supersedes the title-first `viewLabel` helper the platform used to
  build CC sections; finding message TEXT may still name the view's title for
  readability — only the Section identifier changes.

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
- **Validation visibility** (rollout rulings 2026-07-31): the FragmentBar's
  passing chip is named — `CC checks · passing` / `N CC findings` — and its
  click-through now routes to the Design Health step; the dynamic-view picker
  gains a per-view roll-up chip alongside it (e.g. `15/15 realized · CC
  clean`); an empty sibling view (no steps authored yet) reads "Not yet
  realized — part of the pending amendment" rather than the generic empty
  state — a distinct, non-failure tone.
- **Walkthrough-driven trace** (founder QA round 2, 2026-07-31): the lens is not
  a chain with a map beside it — it is the use case's own **walkthrough** driving
  the chain. The `UseCaseWalkthrough` from the use-cases screen renders on the
  LEFT (~40%): the focus card, `Next`, the decision **branch buttons** (the
  reader makes the decisions), Back / Restart, the breadcrumb and the
  you-are-here activity map. The call chain follows on the RIGHT (~60%) in
  **fragment mode**: it lights every call the current activity step realizes —
  all at once — and its caption lists them (`n. label · from → to`) in place of
  the single-call Prev/Next pager, which the walkthrough has replaced. A step the
  chain does not realize (and the multi-root entry chooser) reads "No realization
  for this step" rather than crashing or lying (founder QA round 3, 2026-07-31 —
  the mute-all part of this clause is SUPERSEDED by the visited trail in round 4
  below, which leaves the walked chain lit instead: a
  call-less step then MUTED THE WHOLE DIAGRAM — every node and edge, including the
  Utilities carve-out — rather than rendering it plain, and the caption
  differentiates that real gap from a by-design control-flow step,
  merge/fork/join/start/end/…, which reads "Control-flow step — no
  calls" instead; the camera fits ONCE to the whole diagram when the view
  changes and never moves again while stepping, replacing any per-step recenter —
  the self-paced step-through's own per-step recenter is unchanged. Same-day
  addendum: a call-less `decision`/`switch` node is carved out of that mute-all —
  "if it's a decision shouldn't the person or engine responsible for making that
  decision be highlighted?" — decisions highlight their decider — explicit
  `decidedBy` first (rollout rulings 2026-07-31), else actor lane → person,
  else entry-Manager inference — one node lit ("Decided by <name>"),
  everything else muted, Utilities carve-out intact, still no camera
  movement; a decider that can't be resolved falls back to mute-all). `?step=` still deep-links: the seq
  resolves to its owning activity node and then, via a BFS shortest path over the
  activity edges (`walkthroughPathTo`), to the route the walkthrough opens on.
  Views with no linked use case / no activity diagram render the chain full-width
  and self-paged, exactly as before. On a narrow container the panes stack
  **activity-first** — this REVERSES the earlier chain-first ruling (founder QA
  round 2, 2026-07-31: the activity is what he wants primary), and DOM order,
  visual order and tab order still agree at every width because the walkthrough
  is also what owns the controls.
- **Muting** (same round): focus is carried by MUTING, not by a glow alone.
  Everything that is not an endpoint of the lit call(s) — components AND persons
  — fades to the static graph's hover opacity (`MUTED_NODE_OPACITY`, the token
  `ArchitectureFlow` already uses for a hovered component's non-neighbours), with
  the same Utilities carve-out. This applies in BOTH modes: fragment mode and the
  self-paged step-through (`ScenarioBrowser`, service-contract views, and the
  no-use-case fallback).
- **The visited trail** (founder QA round 4, 2026-07-31 — from the
  system-architect's Playwright walk of the whole use case). Fragment mode's
  one-fragment-at-a-time picture never accumulated: every step looked like the
  first, "Next" between two of the seven stacked
  `SystemDesignManager→AgenticJobAccess` strands appeared to do nothing, a
  call-less step blanked the canvas while its caption claimed "the chain stays as
  it was", and `DesignHealth` sat permanently lit because the Utilities carve-out
  exempted it from every mute. Five changes, fragment mode unless noted:
  1. **The Utilities carve-out is dropped in fragment mode.** A Utility mutes
     like everything else unless it is an endpoint of the current fragment's or a
     visited call. The carve-out remains in the self-paced step-through it was
     written for.
  2. **Calls to Utilities draw real edges** (BOTH modes). The static graph's
     no-lines-to-the-Utilities-bar convention does not belong in a call chain,
     where the call IS the content: `ci-check`'s
     `SystemDesignManager→DesignHealth` call renders as a numbered, tintable edge
     routed through the side handles. `ArchitectureFlow` is untouched.
  3. **The chain accretes as you walk.** `UseCaseWalkthrough` gains
     `onPathChange` (the whole breadcrumb route; `[]` for the entry chooser)
     alongside `onCurrentNodeChange`; `ArchitectureView` derives `visitedSeqs`
     (`callTrail.visitedSeqsForPath` — the calls of every path node BEFORE the
     last) and passes it to `DynamicViewFlow`. Three render tiers replace two:
     CURRENT fragment at full strength, VISITED calls and their endpoints at a
     mid tint (`VISITED_OPACITY` 0.55), never-walked calls at
     `MUTED_NODE_OPACITY` — matching the muted nodes, so ghost wires no longer
     float at 0.40 between 0.12 boxes. Back / Restart shrink the trail for free
     because it is path-derived, not stored. The caption gains the chain-wide
     position, "call K–L of 22".
  4. **Canvas↔caption correspondence.** Each CURRENT-fragment edge carries a
     small high-contrast chip at its midpoint bearing the same global seq the
     caption prints (a side route into the Utilities bar places its chip at the
     arrival end, since a dogleg's geometric midpoint lands on unrelated nodes).
     Parallel strands sharing a `(from, to)` pair are fanned laterally by
     per-pair ordinal (`parallelEdges.parallelIndex` / `parallelLane`,
     applied in `LayeredStepEdge`) so seven stacked strands read as seven lines
     and stepping between two of them visibly moves.
  5. **Honest call-less behaviour.** A call-less step shows the walked chain
     rather than blanking. One function (`fragmentCallLessCaption`) decides the
     heading AND its optional gloss together, in a single precedence chain, so
     the two lines cannot disagree — fix round 1, after the gloss was found
     softening a defect heading: a resolved decider first ("Decided by <name>"),
     then the multi-root entry chooser ("Choose an entry to begin." — you pick an
     entry here, there is no step to move forward through), then a real
     realization gap ("No realization for this step", **never** glossed, because
     it is a defect signal and not a navigation state), then by-design control
     flow — "Control flow — no calls; the chain so far stays lit" with the gloss
     when a trail exists, "No calls yet — step forward to begin the chain."
     without one. The gloss ("Nothing new is called here — what stays lit is the
     chain you have already walked.") is emitted in exactly that one control-flow
     state, plus alongside a decider that has a trail behind it. The decider
     highlight (call-less `decision`/`switch`) is otherwise unchanged except that
     it now lights ON TOP of the trail.

**Both-surface entries:** an entry step carrying both `web-client` and
`mcp-client` calls highlights **both** clients when its fragment lights up —
the walkthrough steps by activity node, so the whole fragment (either surface)
is lit together, reading as "either surface performs this." Numbering follows
the model's `alt` grouping (rollout rulings 2026-07-31): calls in one step
sharing an `alt` value render as lettered siblings of one ordinal — `1a`/`1b`
chips and captions — rather than a plain sequence; both-surface entries are
the first alt group in the data.

**ActivityFlow** renders the two new node kinds with their UML glyphs
(hourglass for `timeEvent`, concave pentagon for `acceptEvent`), including as
edge-less entry nodes.

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

**Viewport-adaptive trace layout** (founder QA round 5, 2026-07-31 — a
measured Playwright pass found the two-up trace stacked vertically at a
common real-world width, 996px inside a 1440×900 viewport with the comment
rail open, pushing the call-chain pane entirely below the fold at 48.7% of
the trace visible on initial load). Four changes, all scoped to the
`UseCaseWalkthrough` + `DynamicViewFlow` pairing `ArchitectureView` renders for
the traced two-up layout — the standalone Use Cases screen and the Static /
Component-focus lenses are unaffected:
1. **Side-by-side threshold lowered to 900px** (from 1100). The old threshold
   stacked layouts that were comfortably readable at a 40/60 split.
2. **Viewport-relative canvas heights.** `FlowCanvas`, `DynamicViewFlow`, and
   `ActivityFlow`'s `height` prop widens from `number` to `number | string`;
   the traced two-up layout passes `clamp(360px, 45vh, 560px)` to both the
   walkthrough's you-are-here map and the call chain, so both canvases fit
   above the fold at common viewport heights instead of a fixed pixel height
   pushing the second pane down. Every other caller keeps its numeric height.
3. **The you-are-here map collapses behind a toggle** (`hideMap`, default
   false): in this embedding the map duplicates the call chain beside it, so
   it opens closed behind a "Show activity map ▾" disclosure; the standalone
   screen keeps it always-on. **The per-call list collapses to a summary
   chip** (`compactCalls`, default false): "N calls →", since the call chain's
   own FragmentBar caption is the single source of call detail in this
   embedding; the standalone screen keeps the full from → to · label list.
4. **Sticky controls + capped breadcrumb.** The focus card caps to
   `min(46vh, 420px)` with internal scroll, and its Next/branch controls pin
   `position: sticky; bottom: 0` so they never scroll out of view. The PATH
   breadcrumb caps to ~2 wrapped lines with a "Show full path ▾" toggle once
   it overflows — this applies in both embeddings, since unbounded breadcrumb
   growth was a defect everywhere, not just the traced two-up layout.

Per-pane independent scroll and chat-rail default-collapse were assessed and
explicitly deferred (follow-ups, not part of this clause).

## 6. Migration & rollout

**Model fan-out (4 copies):**

1. Server `projectstate` — schema source regen; `NewSystem` validation;
   read-back findings.
2. Platform `framework-go/methodcheck` — new types + `CC-*` + retargeted
   `DV-*`; its parallel `ActivityNode` gains the `roleName`/`linkedActorId`
   fields it currently omits (needed by `CC-ACTOR-LANE` and actor roots) and
   the two event kinds; `UC-ACTDIAG` well-formedness accepts edge-less event
   entries. framework-go release + archistrator pin bump (release/push remains
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

**Post-QA rollout order (rollout rulings 2026-07-31).** The founder accepted
the PoC slice; the full rollout now follows the numbered plan at
`docs/superpowers/plans/2026-07-31-callchain-rollout.md`, superseding the
open-ended "nothing beyond the PoC slice" gate above with a concrete order:
model refinements land first (this amendment — `decidedBy`, alternative
groups, `CUC-ACTOR-REQUIRED`, key-first grammar, §3–§5) so the 16-view
amendment pass authors against the final model exactly once; then both
validation tiers and the webApp increment; then the activity-diagram
amendment (the §6a fixes below) and the 16 realizations in three
review-sized batches; then the collateral slots (attestations, staleness
pass, `ACT-COMPONENT-COVERAGE`); then the severity flip to `Error`; then the
`method-assets` doctrine update; then a cleanup wave; then release
choreography — founder executes all pushes, tags, and merges.

## 6a. Architecture impact & amendment scope (system-architect input, 2026-07-30)

**Verdict: no component or relationship changes.** The redesign changes the
shape of a typed artifact owned by the already-encapsulated typed-state
volatility behind `project-state-access`; the new rules ride the existing
`system-design-manager → design-health` edge (its signature already takes the
whole project, so cross-slot validation fits); the hard gate is the existing
slot-5 staging path. Every fix the amendment forces is an activity-diagram or
realization-authoring fix — zero static-model surgery.

**Rulings (architect's gap list; founder-ratified 2026-07-30):**

1. **Connectivity is root-aware and model-driven** — path entries are the
   initial node and any event node; mid-chain actor→Client re-entry is a legal
   fragment root (`human-gate`, `escalate-operator`). Without these, correct
   designs fail (`drive-system-design`, `execute-a-construction-activity`,
   `operate-a-delivered-system`).
2. **Triggers are UML event nodes** (founder: stick to standard UML
   activity-diagram concepts): `timeEvent`/`acceptEvent` node kinds model the
   `timer`/`busMessage` entries, and their steps realize the
   scheduling-Client→Manager / queued-into-Manager entry — so
   `scheduler-client` stays in derived participant sets with no special-cased
   root rule.
3. **R4 cross-surface equivalence — both surfaces in the data** (founder
   ruling, replacing the architect's canonical-client-plus-waiver suggestion):
   entry steps carry both `web-client` and `mcp-client` calls wherever both
   surfaces can perform the operation; the UI highlights both/either. No
   waiver needed — `mcp-client` keeps real coverage. The two web-only use
   cases stay single-surface until MCP genuinely supports them.
4. **`linkedActorId` reconciliation** — `CC-ACTOR-LANE` (see §4) plus activity
   amendments clearing external-system "actors" that can never legally call a
   Client (`payment-provider` on `charge-user`, `customer` on
   `customer-charged`, `settlement-manager`, `operated-system`,
   `infrastructure`).
5. **Trigger taxonomy fixes ride the amendment, using standard UML only**:
   `replan-under-scope-change` either gains a `timeEvent` entry (timer/sweep
   reclassification) or a real queued edge — `CC-TRIGGER-EVENT` forces the
   choice; `operate-a-delivered-system` drops its artificial fork in favor of
   two standard entries (initial→operator path + edge-less `timeEvent` entry
   for the reconcile sweep).
6. **Activity-diagram touch-ups, not new calls**: nodes with no honest
   realization become notes or fold into neighbors (`customer-charged`,
   `in-flight`, `argo-reconcile`); `period-elapses` becomes the `timeEvent`
   entry of `bill-the-user-for-usage`.
7. **Entry-call convention**: the actor→Client + Client→Manager entry calls
   ride on the first action node (no "user initiates" node exists) — documented
   in the rewritten step-9 doctrine.
8. **Keys/titles preserved** (`uc1-*`/`var-*`) — they are deep-link anchors.

**Amendment scope facts:** 16 views re-authored; the 5 `timer`/`busMessage`
diagrams gain event-node entries; 125 action nodes across all 16 diagrams need
steps (vs 131 old flat edges); sampled walks
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
