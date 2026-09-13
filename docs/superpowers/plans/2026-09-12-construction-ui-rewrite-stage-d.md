# Construction UI Rewrite — Stage D: the GRAPH lens

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fill the shell's `⬡ GRAPH` lens with an architecture-shaped React Flow graph in the spirit of Löwy's Table 11-1: every component of the committed architecture in its Method layer row, each carrying its activity's lifecycle as a 5-segment spine, the call-chain relationships as edges (upward and unsanctioned sideways edges drawn in an alarm channel), the utilities in a side bar with no lines, and the milestones as a gate ribbon across the top. Layout is deterministic — the same state gives the same positions, pinned by a test.

**Architecture:** Five pure `.ts` modules under `webApp/src/components/construction/graph/` decide everything (model → layout → spine → ribbon → viewport memory), each pinned by `node:test`. Three `.tsx` files present them. The route mounts one component (`ActivityGraphLens`) in the shell's content slot, fed from values the route ALREADY computes for the list lens (the evidence-view activity tree, the toolbar-filtered tree, the committed `system` and `network` slots), so the graph, the list and the detail pane can never disagree about one activity. Selection stays in the URL (Stage B); the viewport is kept in a signature-keyed module store (the `NetworkView.tsx:106-128` pattern the spec makes mandatory).

**Tech Stack:** React 19, TypeScript 5.9 (`exactOptionalPropertyTypes`), `@xyflow/react` 12 (present), MUI 7, `node:test`, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-09-construction-ui-rewrite-design.md` (§3, §5 R5/R7, §6, §7.2–7.4, §7.6, §7.8, §9, §10, §11)
**Predecessors:** `…-stage-a.md`, `…-stage-b.md` (complete on `construction-ui-rewrite` @ `42e9e94`). Stage C (Tasks lens) runs in parallel on its own branch.

## Global Constraints

- Branch `construction-ui-graph-lens` off `42e9e94`, in its own worktree. Never push. Never touch the main checkout.
- Node commands from `webApp/` / `uitests/`. `npm run typecheck` (`tsc -b`) is the TS gate, not `tsc --noEmit`.
- **No vitest.** Tests are `node:test` in `.ts` siblings (a `.tsx` cannot load in Node's type-stripping runner); relative VALUE imports carry `.ts`.
- **Never hand-edit a generated file** (`schema.ts`, `lifecycleTemplates.gen.ts`, …). **No hand-mirroring the server**: phase sets, weights, task keys and labels come from `lifecycleTemplates.gen.ts`; the canonical phase ORDER is read from the Service profile (Figure A-1 verbatim, all five phases), never typed out.
- **Render only (D4, R5).** No LLM output, no stored graph, no server change: `layer`/`layerBand` already reach the row (Stage A/B). `DerivePlan` and the drift gate are untouched.
- **No numbers from the network on the canvas (R5 tripwire)** — no effort, float, cost, critical path or event time. See Decision D2 and Open Question Q2.
- **Channels (§7.2):** magnitudes get geometry, states get at most one chip, `unknown` gets none. Colour is committed to status and layer; **provenance is texture only** (the house hatch, `ProvenanceRailMark`), and `≈ RECONSTRUCTED` is always spelled out in full, stamped on the GROUP (the card), never per lane.
- **Observed only** keeps every activity and strips non-observed evidence — the graph reads the SAME evidence-view rows as the list (`evidenceViewFor`), never its own.
- **Dispatch safety:** nothing in this lens can dispatch. Every Playwright spec calls `page.route('**/execute-next-activity/**', r => r.abort())` before navigating. Live verification runs behind a GET-only proxy.
- **Shared files are touched minimally and additively** (`ConstructionConsole.tsx` lens switch, `ConstructionShell.tsx`, `UIIdentifiers.ts`, `uitests/tests/support/testids.ts`) so the Stage C branch and `construction-ui-rewrite` merge cleanly.
- Gates before every commit, all FOREGROUND: `npm run typecheck && npm run lint && npm test && npm run build` (webApp), `npm run lint` (uitests); Playwright construction specs + the new graph spec from inside `uitests/` against the worktree's Vite (`UITESTS_BASE_URL=http://localhost:5712`). Go gates only if Go changes (it should not).
- One commit per task; messages end with the Co-Authored-By + Claude-Session trailers.

## Facts (verified 2026-09-12 against the live read at :8888, do not re-derive)

| Fact | Value |
|---|---|
| Committed activities (slot 9) | **29**: 22 `C-*` (5 managers, 7 engines, 10 RA), 4 `R-*`, `U-SPA-web-client`, `N-STP`, `N-IT` |
| `row.layer` / `row.layerBand` | present on every row; `N-STP`/`N-IT` → `projectWide`, no layer; `U-SPA-web-client` → `client` |
| `componentId` | NOT on the construction row; on the activity list (`ActivityMeta.componentId`, joined by the route). 27 of 29 carry one, **injective today** (post-D9) — the model still supports 1..n lanes (pre-D9 projects) |
| Architecture (`system` slot) | **37 components**: 3 client, 5 manager, 7 engine, 10 RA, 8 resource, 4 utility; **74 relationships** (72 sync, 2 queued), no duplicate `(from,to)` pairs |
| Components with no activity | **10**: `mcp-client`, `scheduler-client`, `project-git-repo`, `operated-system-state`, `billing-state`, `usage-log` (layered, 6) + `security`, `logging`, `diagnostics`, `message-bus` (utilities, 4). Spec §7.6's "11" predates D9 |
| Edge directions (utility edges excluded) | 56 down · **2 sideways**, both `queued` Manager→Manager (`billing-manager→operations-manager`, `construction-manager→project-design-manager`) · 0 up · 16 into utilities |
| Milestones | `M0` (no `dependsOn`; 18 activities depend on it via the source rule), `M1` ← 4 `R-*`, `M2` ← 7 engines, `M3` ← 5 managers |
| Reusable | `flowLayout.computeLayout` (barycenter row order), `LAYER_LABEL`, `layerColors`, `flowEdge` + `LayeredStepEdge`, `RowLabelNode`, `UtilityFrameNode`, `FlowCanvas`, `MUTED_OPACITY`; `buildActivityTree`, `applyToolbarToActivities`, `taskRowState`, `noAttemptStateFor`, `activityRowState`, `chipFor`, `readProvenance`/`ProvenanceRailMark`/`ProvenanceGroupStamp` |

## Decisions (where the spec is silent or self-inconsistent; each chosen as the option most consistent with it)

- **D1 — Placement is the activity's server layer (R5), not its component's.** A component card sits in `component.layer`'s row and hosts every activity whose `componentId` is that component AND whose `row.layer` equals that layer. An activity whose layer differs from its component's (the R5 trap — pre-D9 `U-SPA-<manager>`) gets its own *surface* card in its own layer's row, subtitled with the component it builds. `projectWide` activities (and any with no layer) go to the System-wide band. Zero surface cards today; the rule is pinned by a test because it is the trap R5 names.
- **D2 — Ship without CPM numbers.** §7.2 maps effort to node width and float/CP to rails "in the graph", but R5 rules "ship the layer render without numbers" behind the `activityListOverrides` tripwire. R5 is the later, narrower domain ruling, so card width is constant and no float/CP/effort/eventTime appears. Q2 asks whether the tripwire is now cleared.
- **D3 — The alarm follows App C, including its carve-out.** R5 says any upward or sideways edge alarms; it also says the view's purpose is "a live App C layering check". App C §3.4 (the-method-layers) forbids sideways calls "except queued Manager → Manager", and the server's own App C authority implements exactly that (`designhealthengine.go:2520`). The two live sideways edges are that sanctioned case. Alarming them would render a design defect App C says does not exist, so: `up` → alarm; `sideways` → alarm unless `queued` and Manager→Manager (drawn dashed, as every queued call already is, and counted separately). Q1 asks the architect to ratify.
- **D4 — Filters dim, never move.** The toolbar (search/scope/kind/layer) is shared (§10 cuts a separate graph filter bar). In the graph a non-matching lane/card dims to `MUTED_OPACITY`; nothing is removed or re-laid-out, because positions are a function of STATE only (determinism + poll stability). Sort and "Expand to current phase" are list-only: disabled in the graph with a tooltip saying why.
- **D5 — The spine is five canonical slots.** Canonical order is the Service profile's phase order (the only profile carrying all five). A profile phase takes width ∝ its Table A-1 weight; a phase absent from the profile renders as a fixed 4% gap at 40% opacity (§7.2 `absent`). An unclassified activity renders "Unclassified" and **no spine at all** (AC4).
- **D6 — Segment state reuses the list's task rules.** Ticks use `taskRowState(task, row.status, noAttemptStateFor(row))`; a segment is `complete` iff the server says the phase is complete, else the loudest of its ticks (awaitingHuman › failed › running), else `incomplete` (known, gate not passed), else the no-attempt state (`notStarted` hollow / `unknown` dashed). The graph never re-derives completion.
- **D7 — LOD by zoom, plus a hover card.** LOD-0 below zoom 0.8 (geometry + fill + card title), LOD-1 at ≥0.8 (phase labels + task ticks, retries doubled). The ">12 nodes in viewport" criterion is dropped (zoom approximates it without per-frame viewport math). Hover at any zoom shows an unscaled `NodeToolbar` card naming the lanes and their phase states, because at fit zoom nothing on the canvas is legible. LOD-2 is cut (§10): selecting opens the detail pane.
- **D8 — The ribbon is HTML above the canvas.** Milestones never pan away. Each shows id, name and a fan-in count `k/n complete` (complete = `percentComplete === 100`, the App A binary exit arithmetic), hatched if any feeder is reconstructed; `M0`, having no fan-in, shows `gates N` instead of a state. No event times (D2). Hovering a milestone hover-focuses its feeders (or, for M0, what it gates).
- **D9 — Every component without an activity is hollow, utilities included.** Spec §7.6 lists utilities among the "no activity" examples; the statement is factual. Q3 asks whether utilities (never derived by doctrine) should instead read differently.
- **D10 — Selection pins a ring, hover focuses.** Clicking a lane writes `a=<id>` (clicking a spine segment at LOD-1 writes `a` + `p`); the pane opens. Neighbourhood focus is HOVER only (the static-view convention); a pinned selection does not dim the canvas while the pane is open.
- **D11 — Viewport memory.** `FlowCanvas` gains optional `defaultViewport` / `onMoveEnd` / `minZoom` props (additive). The graph stores its viewport per content signature (project + component ids + activity ids, NOT statuses) and restores it on remount; with nothing stored it fits the view. A deep-linked selection with no stored viewport frames that card once.

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `webApp/src/components/flow/flowShared.tsx` | `FlowCanvas` optional viewport props (additive) | 1 |
| `…/construction/graph/graphViewport.ts` *(new)* | signature, viewport store, LOD rule | 1 |
| `…/construction/graph/activityGraphModel.ts` *(new)* | cards, lanes, hollow, edges + direction/alarm | 2 |
| `…/construction/graph/activityGraphLayout.ts` *(new)* | deterministic positions, rows, bar, decor | 3 |
| `…/construction/graph/laneSpine.ts` *(new)* | 5-slot spine, segment + tick states | 4 |
| `…/construction/graph/gateRibbon.ts` *(new)* | milestone ribbon model | 5 |
| `…/construction/graph/GraphNodes.tsx`, `graphNodeTypes.ts` *(new)* | card / lane / spine rendering | 6 |
| `…/construction/graph/ActivityGraphLens.tsx`, `GateRibbon.tsx` *(new)* | canvas, edges, hover-focus, legend, ribbon | 7 |
| `webApp/src/routes/ConstructionConsole.tsx`, `lens/ConstructionShell.tsx` | mount the lens; list-only controls disabled in the graph | 8 |
| `webApp/src/utilities/constants/UIIdentifiers.ts`, `uitests/tests/support/testids.ts` | graph testids (one additive block each) | 6, 9 |
| `uitests/tests/construction-graph-lens.spec.ts` *(new)* | black-box acceptance of the lens | 9 |

---

### Task 1: Viewport memory and the LOD rule

**Files:** Create `graph/graphViewport.ts`, `graph/graphViewport.test.ts`; Modify `flow/flowShared.tsx`.

**Interfaces — produces:** `graphSignatureOf(projectId, componentIds, activityIds): string`; `loadGraphViewport(sig)`, `saveGraphViewport(sig, vp)`; `LOD1_MIN_ZOOM = 0.8`; `lodFor(zoom, hovered): 0 | 1`; `FlowCanvas` props `defaultViewport?`, `onMoveEnd?`, `minZoom?` (fitView only when no `defaultViewport`).

The console polls at 1.5s while cascading and a lens switch remounts the canvas; xyflow's `fitView` would reset the operator's pan/zoom on every remount. Copy `NetworkView`'s pattern: a module-level store keyed by a content signature that ignores status churn.

- [ ] **Step 1: Failing tests** — signature is order-insensitive (`['b','a']` ≡ `['a','b']`), changes when the component or activity SET changes, and is NOT an input of any status; store round-trips and isolates signatures; `lodFor(0.79,false)===0`, `lodFor(0.8,false)===1`, `lodFor(0.3,true)===1`.
- [ ] **Step 2:** `node --test src/components/construction/graph/graphViewport.test.ts` → fails (module missing).
- [ ] **Step 3: Implement.** `FlowCanvas`: spread `defaultViewport`/`onMoveEnd`/`minZoom` only when given; `fitView={defaultViewport === undefined}` — every existing caller passes none and is byte-identical in behaviour.
- [ ] **Step 4: Gates.** typecheck, lint, test, build.
- [ ] **Step 5: Commit** `feat(construction): graph viewport memory and the LOD rule (Stage D Task 1)`.

### Task 2: The graph model — cards, lanes, hollow, edges

**Files:** Create `graph/activityGraphModel.ts`, `graph/activityGraphModel.test.ts`.

**Interfaces — consumes:** `C4Component[]`/`C4Relationship[]` (`toC4View`), `ActivityNode[]` (`buildActivityTree`, carrying `layer`, `layerBand`, `componentId`). **Produces:**

```ts
type GraphRow = 'client' | 'manager' | 'engine' | 'resourceAccess' | 'resource' | 'systemWide' | 'utility';
interface GraphCard { id: string; kind: 'component' | 'surface' | 'projectWide'; row: GraphRow;
  title: string; componentId?: string; buildsComponent?: string; lanes: ActivityNode[]; hollow: boolean }
type EdgeDirection = 'down' | 'sanctionedSideways' | 'sideways' | 'up';
interface GraphEdge { id: string; from: string; to: string; mode: CallMode; direction: EdgeDirection; alarm: boolean }
interface ActivityGraphModel { cards: GraphCard[]; edges: GraphEdge[];
  alarms: { up: number; sideways: number }; sanctionedSideways: number; hollowCount: number;
  cardOfActivity: Readonly<Record<string, string>> }
buildActivityGraphModel({ components, relationships, activities }): ActivityGraphModel
```

Rules (Decisions D1, D3, D9): component cards in system-slot order; lanes sorted by activity id; surface and project-wide cards sorted by activity id and appended after the component cards; an edge is kept only between two non-utility component cards (no lines to or from the bar); direction by row rank; `alarm = up || sideways`.

- [ ] **Step 1: Failing tests** —
  - every activity lands in exactly one card (`cardOfActivity` total over the input);
  - **R5 trap:** a `U-SPA-x` activity with `componentId = 'x-manager'` and `layer = 'client'` becomes a *surface* card in the client row, and the manager card does NOT host it;
  - a `projectWide` activity (`N-STP`) lands in `systemWide` with no component;
  - a component with no activity is `hollow`, utilities included, and lives in the `utility` row;
  - edges touching a utility are dropped;
  - `manager→engine` is `down`; `engine→manager` is `up` + alarm; `engine→engine` is `sideways` + alarm; **queued `manager→manager` is `sanctionedSideways`, not an alarm; the same pair `sync` IS an alarm;**
  - the model is identical when the activities are passed in reverse order (determinism).
- [ ] **Step 2:** run → fails.
- [ ] **Step 3: Implement** (pure; no React).
- [ ] **Step 4: Gates.** Mutation-verify: swap `row.layer` for the component's layer → the R5 test fails; drop the queued carve-out → the sanctioned test fails; revert both.
- [ ] **Step 5: Commit** `feat(construction): the graph model — layer placement, hollow coverage, App C edge alarms (Stage D Task 2)`.

### Task 3: Deterministic layout

**Files:** Create `graph/activityGraphLayout.ts`, `graph/activityGraphLayout.test.ts`.

**Interfaces — produces:** `layoutActivityGraph(model): GraphLayout` with `pos`/`size` per card id, `rows: { row; y; height; label }[]`, `bar?: { x; top; bottom }`, `width`, `height`; `decorNodes(layout): Node[]` (reusing the `rowLabel` / `utilityFrame` node types).

Within-row order is `flowLayout.computeLayout`'s barycenter sweep (reused, not re-implemented): the non-utility, non-system-wide cards go in as `LayoutComponent`s with their row as layer, the model's edges as `LayoutEdge`s, and the returned x gives each card's column index. Geometry is this module's own because a card's height varies with its lane count: `card h = HEAD + max(1, lanes) × LANE + PAD`, row height = tallest card, rows stacked with a fixed gap, System-wide last, utilities stacked in a bar right of the widest row. Row labels: `Clients / Managers / Engines / Resource Access / Resources / System-wide`.

- [ ] **Step 1: Failing tests** —
  - **golden:** a 6-card fixture produces exactly pinned coordinates (literal numbers in the test);
  - **same state, same positions:** two calls deep-equal;
  - **status is not an input:** changing every lane's attempts/phases/status moves nothing;
  - **input order is not an input:** activities reversed → identical positions;
  - rows top→down in Method order with System-wide last; no row overlaps the next;
  - every utility card is at `bar.x`; no non-utility card reaches it;
  - a 2-lane card is taller than a 1-lane card and its row grows to fit.
- [ ] **Step 2:** run → fails.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Gates.** Mutation-verify: replace the computeLayout order with input order → golden test fails; add a status-dependent term (e.g. `+ lane.percentComplete`) → the invariance test fails; revert.
- [ ] **Step 5: Commit** `feat(construction): deterministic layered layout for the graph lens (Stage D Task 3)`.

### Task 4: The lane spine — the mini-lifecycle as geometry

**Files:** Create `graph/laneSpine.ts`, `graph/laneSpine.test.ts`.

**Interfaces — produces:**

```ts
type SegmentState = 'complete' | 'awaitingHuman' | 'failed' | 'running' | 'incomplete' | 'notStarted' | 'unknown' | 'absent';
interface SpineTick { nodeId: string; task: string; label: string; state: RowState; retries: number; gate: boolean }
interface SpineSegment { phase: LifecyclePhase; name?: string; weight?: number; fraction: number; state: SegmentState; ticks: SpineTick[] }
interface LaneSpine { unclassified: boolean; segments: SpineSegment[] }
laneSpineFor(node: ActivityNode): LaneSpine
CANONICAL_LIFECYCLE: readonly LifecyclePhase[]   // = GENERATED_TEMPLATES.service order
```

- [ ] **Step 1: Failing tests** — service lane: 5 segments, fractions ∝ 15/20/10/40/15 summing to 1; a deployment lane: its absent phases are `absent` gaps of the fixed fraction at their canonical positions; an unclassified lane: `unclassified: true`, zero segments; a server-complete phase is `complete` regardless of tick states; a phase with a failed latest attempt is `failed`; a gate task running on an `in-review` row makes the segment `awaitingHuman`; a classified row with no evidence reads `notStarted`, an unclassified or pre-history row `unknown`; a task with attempts 1..3 carries `retries: 2`; conditional tasks appear as ticks only when attempted (inherited from the tree).
- [ ] **Step 2:** run → fails.
- [ ] **Step 3: Implement** — reads `ActivityNode.phases` (never re-derives completion) and `taskRowState`/`noAttemptStateFor`.
- [ ] **Step 4: Gates.** Mutation-verify: return a default profile for unclassified → AC4 test fails; revert.
- [ ] **Step 5: Commit** `feat(construction): the lane spine — Table A-1 weights as width, state as fill (Stage D Task 4)`.

### Task 5: The gate ribbon

**Files:** Create `graph/gateRibbon.ts`, `graph/gateRibbon.test.ts`.

**Interfaces — produces:** `gateRibbonFor(milestones, dependencies, nodes): RibbonMilestone[]` where `RibbonMilestone = { id; name; feeders: string[]; gates: string[]; complete: number; provenance: ProvenanceOrigin }`.

- [ ] **Step 1: Failing tests** — milestones keep network order; `M1` feeders are its `dependsOn`, `complete` counts those at `percentComplete === 100`; `M0` (no `dependsOn`) has zero feeders and `gates` = every activity that lists it; provenance is the worst over feeders (reconstructed feeder → reconstructed; none → `unknown`); a feeder id absent from the tree counts as not complete (never crashes).
- [ ] **Step 2–5:** run, implement, gates, commit `feat(construction): milestones as a gate ribbon (Stage D Task 5)`.

### Task 6: Render the cards

**Files:** Create `graph/GraphNodes.tsx`, `graph/graphNodeTypes.ts`; Modify `UIIdentifiers.ts` (one additive `// Stage D — the GRAPH lens` block).

**Interfaces — produces:** node types `graphCard` (component / surface / project-wide) and the reused `rowLabel` / `utilityFrame`; testids `GRAPH_CANVAS`, `GRAPH_RIBBON`, `GRAPH_KEY`, `GRAPH_LAYER_CHECK`, `graphCard(id)`, `graphLane(activityId)`, `graphSegment(activityId, phase)`, `graphMilestone(id)`.

A card: a 3px layer-colour top edge (`layerColors`), the title (+ `builds <component>` on a surface card), and one lane per activity. A lane: provenance rail (`ProvenanceRailMark`), activity id, the spine, and at most one chip (`chipFor(activityRowState(row))`). The card carries ONE spelled-out `≈ RECONSTRUCTED` (`ProvenanceGroupStamp`) when any lane is reconstructed. Hollow: dashed hairline, no fill, muted, `no activity`. Spine fills per §7.2: complete = committed fill; failed = `dangerFg` on `awaitingBg`; awaitingHuman = `awaitingBg` + accent edge; running = teal dot (the only animation, off under reduced motion); incomplete = solid hairline; notStarted = solid hairline, hollow; unknown = dashed hairline; absent = 40% gap. LOD-1 adds phase names under segments and task ticks (doubled stroke where `retries > 0`). Handles `t`/`b` (ids shared with `C4Node`, so `flowEdge` routes unchanged). Hover card via `NodeToolbar`. `data-row`, `data-hollow`, `data-lanes`, `data-provenance`, `data-state` attributes for the black-box spec.

- [ ] **Step 1:** the presentation helpers (segment fill tokens, LOD) are already pinned in Tasks 1/4; this task adds no pure logic — if it grows any, it goes in a `.ts` sibling with a test.
- [ ] **Step 2–4:** implement; typecheck, lint (React Compiler rules), test, build.
- [ ] **Step 5: Commit** `feat(construction): graph cards — lanes, spines, hollow coverage, provenance on the group (Stage D Task 6)`.

### Task 7: The canvas, the edges, the ribbon

**Files:** Create `graph/ActivityGraphLens.tsx`, `graph/GateRibbon.tsx`.

**Interfaces — consumes:** `activities` (the evidence-view tree), `visible` (toolbar-filtered), `systemEnvelope`, `network` model, `projectId`, `selection`, `onSelect`. **Produces:** `<ActivityGraphLens …/>`.

Edges via `flowEdge` (no labels, ever): `down` normal; `sanctionedSideways` dashed (queued); `up`/`sideways` in the ALARM channel — `dangerFg`, 2.5px, never hidden, not even under hover-focus. Hover-focus exactly as `ArchitectureFlow`: hovered card + direct neighbours lit, others at `MUTED_OPACITY`, non-incident non-alarm edges hidden, leave debounced 90 ms; utilities never dim. Filter dimming (D4). Key strip (HTML, beside the ribbon): layer colours present, the spine key, `Layering check: N upward · N sideways (+N queued Manager→Manager, App C §3.4)`, `N components with no activity`. Canvas height fills the viewport below the toolbar (`clamp`), `minZoom` low enough to fit the widest row.

- [ ] **Step 1–4:** implement; gates; then run the worktree Vite and LOOK at a screenshot at 1600 before committing (Task 8 wires it).
- [ ] **Step 5: Commit** `feat(construction): the graph canvas — App C alarm edges, hover-focus, gate ribbon (Stage D Task 7)`.

### Task 8: Mount the lens in the shell

**Files:** Modify `routes/ConstructionConsole.tsx` (the lens content switch + graph subtitle), `lens/ConstructionShell.tsx` (graph hint; optional `sortDisabledReason` prop).

The content ternary gains one arm: `lens === 'graph' ? <ActivityGraphLens …/> : <LensComingLater …/>` (Tasks keeps its placeholder here — Stage C replaces its own arm). "Expand to current phase" and Sort are passed disabled with a reason in the graph (D4). The `PhaseGatePanel` stays list-only (unchanged).

- [ ] **Step 1:** live-drive: `?lens=graph` renders; a lane click opens the pane with `a=` in the URL; `?lens=graph&a=C-review-engine` rings that card.
- [ ] **Step 2–4:** gates + all construction Playwright specs (the list lens must not regress).
- [ ] **Step 5: Commit** `feat(construction): the GRAPH lens replaces its placeholder (Stage D Task 8)`.

### Task 9: Black-box acceptance + live verification

**Files:** Create `uitests/tests/construction-graph-lens.spec.ts`; Modify `uitests/tests/support/testids.ts` (one additive block).

Every test aborts `**/execute-next-activity/**` first. Expected sets are computed from the API over the wire (`get-project`), never hardcoded:

1. every committed activity has exactly one lane; `N-STP`/`N-IT` sit in the System-wide row; `U-SPA-web-client` sits in the Clients row (above every manager);
2. every component with no activity renders hollow and says `no activity`;
3. no edge touches a utility; utilities sit in the side bar;
4. the layering-check line states the counts computed from the relationships and layers;
5. **determinism:** a reload yields byte-identical card transforms;
6. **selection + viewport survive a remount:** pan/zoom, select a lane, switch to LIST and back — the viewport transform and `a=` are unchanged, the card is ringed;
7. a lane click opens the detail pane; its run action is present and enabled;
8. Observed only: the lane count is unchanged and no lane carries the hatch;
9. the ribbon shows M0–M3;
10. Sort and Expand are disabled in the graph.

Then screenshots at 1280, 1366, 1600 (rest, hover, selected + pane, Observed only) into `scratchpad/stage-d/`, read back and checked against §7.2/§7.6.

- [ ] Steps: write the spec, run it red against the placeholder commit where applicable, green at HEAD; mutation-verify the determinism and utility-edge assertions (break the model, see red); run ALL construction specs; commit `test(construction): black-box acceptance for the GRAPH lens (Stage D Task 9)`.

---

## Stage D exit criteria

```bash
cd webApp && npm run typecheck && npm run lint && npm test && npm run build
cd ../uitests && npm run lint
UITESTS_BASE_URL=http://localhost:5712 npx playwright test construction-
```

Plus: §9 AC1 (Observed only keeps every lane, strips evidence), AC3 (the pane's run action from a graph selection), AC4 (Unclassified: no spine), AC6 (selection and viewport survive remount/poll), AC7 (Playwright-driven, screenshots at three widths).

## Open questions (need a ruling; none blocks this stage — each has a shipped default)

- **Q1 (architect) — Sideways alarm vs App C's carve-out.** R5: "any upward or sideways edge is a design-defect indicator". App C §3.4 and `designhealthengine.go:2520` sanction *queued* Manager→Manager, and the live architecture has exactly two such edges. **Shipped (D3):** they are drawn dashed, counted as sanctioned, and NOT alarmed; unqueued sideways and all upward edges alarm. **Recommend ratifying D3** — the view's stated purpose is a live App C check, and a check that disagrees with App C is a false alarm on the screen whose job is to never assert what it does not know.
- **Q2 (architect → founder) — Is R5's CPM tripwire cleared?** The tripwire blocks effort/float/CP on the graph until `activityListOverrides` reach a re-materialization. On this branch the 25 overrides were deleted (F3) and slot 10 is materialized and drift-gated with exact equality (ledger Tasks 2–4). **Shipped (D2):** no numbers. **Recommend:** the architect confirms the tripwire is satisfied (the codec still drops `activityListOverrides` on write — inert with zero overrides, but it should be fixed before any override is re-introduced), after which §7.2's effort-width and float/CP rails are a small follow-up.
- **Q3 (designer) — Utilities as hollow.** Doctrine never derives a utility activity, so the four utilities will read `no activity` forever. **Shipped (D9):** hollow + `no activity`, as §7.6 lists them. **Recommend** keeping it (it is true, and the bar is visually separate), unless the designer finds it reads as a defect at a glance.
- **Q4 (PM) — What M0 should assert.** M0 has no fan-in; its real exit is the SDP review. **Shipped (D8):** `gates N`, no state. Deriving a state from the `sdpReview` slot's stage would be a new claim; flagged rather than invented.

## Earmarks

- LOD-2 nested Figure A-1 sub-flow — cut (§10); the list carries the same information losslessly.
- §7.2 effort width / float rail / CP border on the graph — behind Q2.
- The 2 queued M→M edges are the only same-row edges; if a project draws many, route them as arcs rather than straight steps.
- Windowing/virtualization — not needed at 37 cards.
