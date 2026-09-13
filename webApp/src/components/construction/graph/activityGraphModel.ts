/**
 * The GRAPH lens's model: which cards exist, which activities ride on which
 * card, which components the plan never builds, and which call-chain edges
 * break the layering.
 *
 * Pure — no React, no DOM — so every rule is decided once, here, and pinned by
 * activityGraphModel.test.ts under node:test. The layout (activityGraphLayout)
 * and the renderers only present what this returns.
 *
 * THE TWO INPUTS, AND WHY BOTH
 * ----------------------------
 * The ARCHITECTURE (the committed `system` slot: components + relationships)
 * decides the shape — Table 11-1 derives one coding activity per component, and
 * its call chains are the dependencies — so the canvas is the architecture, not
 * the CPM network (spec §7.6: "a DAG that already lays out cleanly, versus the
 * CPM spaghetti staircase"). The PLAN (the committed activity list, as the
 * evidence-view activity tree) decides what rides on it.
 *
 * PLACEMENT IS THE ACTIVITY'S LAYER (spec R5, Decision D1)
 * -------------------------------------------------------
 * The server projects each activity's layer (`row.layer`, `row.layerBand`), and
 * R5 is explicit that "the layer of an activity is not the layer of its
 * component": a `U-SPA-<manager>` builds a Client-layer surface whose
 * componentId names a Manager. So an activity rides on its component's card
 * ONLY when the two layers agree. Where they do not, the activity gets its own
 * SURFACE card in its own layer's row, saying which component it builds. A
 * project-wide activity (N-STP, N-IT) — or one the server gave no layer —
 * joins the System-wide band; a layer is never guessed.
 *
 * COVERAGE (spec §7.6, Decision D9)
 * ---------------------------------
 * A LAYERED component no activity builds is HOLLOW — the coverage gap
 * ACT-COMPONENT-COVERAGE exists to catch. A UTILITY is never hollow: doctrine
 * never derives an activity for one, so drawing it hollow ("no activity")
 * read as a defect the plan could fix (designer P1-5 / Q3, adopted by the
 * orchestrator). Utilities render as solid, muted cards and are not counted.
 *
 * THE ALARM FOLLOWS APP C (spec R5, Decision D3)
 * ---------------------------------------------
 * In a layer-positioned render an edge's direction is geometry: down is normal,
 * up is a closed-architecture violation, and sideways within a layer is one too
 * — EXCEPT the one sideways call App C §3.4 sanctions: a QUEUED Manager →
 * Manager call (the-method-layers). The view exists to be a live App C check,
 * so it must agree with App C exactly — never stricter, never looser (the
 * architect's Q1 ruling, spec R5): the exemption needs BOTH endpoints to be
 * Managers AND `mode = queued`, and both sit in one row by construction. The
 * sanctioned case is counted, drawn as the queued call it is, and NOT alarmed.
 * A sync or `eventPubSub` Manager→Manager edge alarms, and so does a queued
 * sideways edge between non-Managers.
 *
 * This is deliberately STRICTER than the server: the Design-Health rule at
 * designhealthengine.go:2522 (`RuleGraphSidewaysSync`) exempts ANY queued
 * same-layer call. The ruling keeps the graph on App C and earmarks the server
 * rule for a design-health wave (spec §11); zero live edges are affected.
 *
 * NO LINES TO THE UTILITIES
 * -------------------------
 * Edges touching a utility are dropped (the founder's ratified diagram
 * convention: the side bar "just exists"). So is any edge whose endpoint is
 * not in the architecture.
 */
import type { CallMode, Layer } from '../../../contracts/types';

// ---------------------------------------------------------------------------
// Inputs — structural, so the real types (C4Component, C4Relationship,
// ActivityNode) and a test's plain fixtures both fit.
// ---------------------------------------------------------------------------

export interface GraphComponentLike {
  id: string;
  name: string;
  layer: Layer;
}

export interface GraphRelationshipLike {
  from: string;
  to: string;
  mode: CallMode;
}

export interface GraphActivityLike {
  activityId: string;
  label: string;
  layer?: Layer;
  layerBand?: 'layered' | 'projectWide';
  /** ModelActivityItem.componentId — which component the activity builds. */
  componentId?: string;
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

/** The rows of the canvas, top→down, plus the utility side bar. */
export type GraphRow = Exclude<Layer, 'utility'> | 'systemWide' | 'utility';

/** The five Method layer rows, top→down — the rank an edge's direction reads. */
export const LAYERED_ROWS = ['client', 'manager', 'engine', 'resourceAccess', 'resource'] as const;

export type GraphCardKind = 'component' | 'surface' | 'projectWide';

export interface GraphCard<A extends GraphActivityLike = GraphActivityLike> {
  /** The component id for a component card; a prefixed activity id otherwise. */
  id: string;
  kind: GraphCardKind;
  row: GraphRow;
  /** The component's name, or (surface / project-wide) the activity's label. */
  title: string;
  /** Present on a component card only. */
  componentId?: string;
  /** A surface card's component — its name when the architecture has it, else its id. */
  buildsComponent?: string;
  /** The activities riding on this card, ordered by activity id. */
  lanes: A[];
  /** A component card no activity builds. Never true for the other kinds. */
  hollow: boolean;
}

/**
 * `down` — a normal call. `sanctionedSideways` — the queued Manager→Manager
 * call App C §3.4 permits. `sideways` / `up` — layering violations (alarms).
 */
export type EdgeDirection = 'down' | 'sanctionedSideways' | 'sideways' | 'up';

export interface GraphEdge {
  /** `<from>-><to>`, suffixed `#n` for the n-th repeat of a pair. */
  id: string;
  from: string;
  to: string;
  mode: CallMode;
  direction: EdgeDirection;
  alarm: boolean;
}

export interface ActivityGraphModel<A extends GraphActivityLike = GraphActivityLike> {
  /** Component cards in architecture order, then surface cards, then project-wide
   *  cards — each of the latter two ordered by activity id. */
  cards: GraphCard<A>[];
  edges: GraphEdge[];
  alarms: { up: number; sideways: number };
  sanctionedSideways: number;
  hollowCount: number;
  /** Every input activity → the id of the one card carrying it. */
  cardOfActivity: Readonly<Record<string, string>>;
}

export interface GraphModelInput<A extends GraphActivityLike> {
  components: readonly GraphComponentLike[];
  relationships: readonly GraphRelationshipLike[];
  activities: readonly A[];
}

// ---------------------------------------------------------------------------
// The derivation
// ---------------------------------------------------------------------------

/** Locale-independent id order: the same state must lay out the same everywhere. */
function byCodeUnit(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

function rowOfLayer(layer: Layer): GraphRow {
  return layer;
}

export function buildActivityGraphModel<A extends GraphActivityLike>(
  input: GraphModelInput<A>
): ActivityGraphModel<A> {
  const componentById = new Map(input.components.map((c) => [c.id, c]));

  const componentCards: GraphCard<A>[] = input.components.map((c) => ({
    id: c.id,
    kind: 'component',
    row: rowOfLayer(c.layer),
    title: c.name,
    componentId: c.id,
    lanes: [],
    hollow: false,
  }));
  const componentCardById = new Map(componentCards.map((card) => [card.id, card]));

  const surfaceCards: GraphCard<A>[] = [];
  const projectWideCards: GraphCard<A>[] = [];
  const cardOfActivity: Record<string, string> = {};

  const activities = [...input.activities].sort((a, b) => byCodeUnit(a.activityId, b.activityId));
  for (const a of activities) {
    const layer = a.layerBand === 'layered' ? a.layer : undefined;
    if (layer === undefined) {
      const id = `activity:${a.activityId}`;
      projectWideCards.push({
        id,
        kind: 'projectWide',
        row: 'systemWide',
        title: a.label,
        lanes: [a],
        hollow: false,
      });
      cardOfActivity[a.activityId] = id;
      continue;
    }
    const component = a.componentId !== undefined ? componentById.get(a.componentId) : undefined;
    const host = component !== undefined ? componentCardById.get(component.id) : undefined;
    if (host !== undefined && component?.layer === layer) {
      host.lanes.push(a);
      cardOfActivity[a.activityId] = host.id;
      continue;
    }
    // The R5 trap, or a component the architecture does not carry: the activity
    // is drawn where ITS layer says, never on a card in another layer's row.
    const id = `surface:${a.activityId}`;
    const builds = component?.name ?? a.componentId;
    surfaceCards.push({
      id,
      kind: 'surface',
      row: rowOfLayer(layer),
      title: a.label,
      ...(builds !== undefined ? { buildsComponent: builds } : {}),
      lanes: [a],
      hollow: false,
    });
    cardOfActivity[a.activityId] = id;
  }

  let hollowCount = 0;
  for (const card of componentCards) {
    // A utility is never hollow: doctrine plans no activity for one, so "no
    // activity" there is the design, not a gap (designer P1-5 / Q3, adopted).
    card.hollow = card.lanes.length === 0 && card.row !== 'utility';
    if (card.hollow) hollowCount += 1;
  }

  const { edges, alarms, sanctionedSideways } = classifyEdges(
    input.relationships,
    componentCardById
  );

  return {
    cards: [...componentCards, ...surfaceCards, ...projectWideCards],
    edges,
    alarms,
    sanctionedSideways,
    hollowCount,
    cardOfActivity,
  };
}

// ---------------------------------------------------------------------------
// Edges
// ---------------------------------------------------------------------------

const ROW_RANK: ReadonlyMap<GraphRow, number> = new Map(LAYERED_ROWS.map((row, i) => [row, i]));

function directionOf(from: GraphRow, to: GraphRow, mode: CallMode): EdgeDirection | undefined {
  const rf = ROW_RANK.get(from);
  const rt = ROW_RANK.get(to);
  if (rf === undefined || rt === undefined) return undefined;
  if (rt > rf) return 'down';
  if (rt < rf) return 'up';
  // App C §3.4: the only sanctioned sideways call is a QUEUED Manager → Manager.
  return from === 'manager' && mode === 'queued' ? 'sanctionedSideways' : 'sideways';
}

function classifyEdges<A extends GraphActivityLike>(
  relationships: readonly GraphRelationshipLike[],
  componentCardById: ReadonlyMap<string, GraphCard<A>>
): Pick<ActivityGraphModel<A>, 'edges' | 'alarms' | 'sanctionedSideways'> {
  const edges: GraphEdge[] = [];
  const alarms = { up: 0, sideways: 0 };
  let sanctionedSideways = 0;
  const seen = new Map<string, number>();

  for (const r of relationships) {
    const from = componentCardById.get(r.from);
    const to = componentCardById.get(r.to);
    // No lines to or from the utility bar, and none to a component the
    // architecture does not carry.
    if (from === undefined || to === undefined) continue;
    if (from.row === 'utility' || to.row === 'utility') continue;
    const direction = directionOf(from.row, to.row, r.mode);
    if (direction === undefined) continue;

    const pair = `${r.from}->${r.to}`;
    const n = seen.get(pair) ?? 0;
    seen.set(pair, n + 1);

    const alarm = direction === 'up' || direction === 'sideways';
    if (direction === 'up') alarms.up += 1;
    if (direction === 'sideways') alarms.sideways += 1;
    if (direction === 'sanctionedSideways') sanctionedSideways += 1;

    edges.push({
      id: n === 0 ? pair : `${pair}#${String(n)}`,
      from: r.from,
      to: r.to,
      mode: r.mode,
      direction,
      alarm,
    });
  }
  return { edges, alarms, sanctionedSideways };
}
