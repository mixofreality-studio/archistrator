/**
 * The GRAPH lens's presentation rules — which token paints each spine state and
 * each task tick, what a state is called, what the ribbon's count says and how
 * the layering check reads. Pure, pinned by graphPresentation.test.ts; the
 * .tsx renderers only apply what this returns.
 *
 * NO NEW COLOURS. Every fill is a token the list lens already uses for the same
 * state (detailPaneState.taskDetailStateFill), so a state reads identically in
 * the list, the pane and the graph, across all five themes. Colour is spent on
 * STATE here; provenance is texture (the hatched rail), never a colour.
 */
import type { Tokens } from '../../../utilities/theme/themes';
import { taskDetailStateFill } from '../detail/detailPaneState.ts';
import { ROW_STATE_LABEL, type RowState } from '../list/activityRowPresentation.ts';
import type { SegmentState } from './laneSpine.ts';

/** Every segment state, loudest first — for exhaustive tests and legends. */
export const SEGMENT_STATES: readonly SegmentState[] = [
  'awaitingHuman',
  'failed',
  'running',
  'complete',
  'incomplete',
  'notStarted',
  'unknown',
  'absent',
];

/** What each segment state is called — the list's own words wherever it has one. */
export const SEGMENT_STATE_LABEL: Record<SegmentState, string> = {
  complete: 'Gate passed',
  awaitingHuman: ROW_STATE_LABEL.awaitingHuman,
  failed: ROW_STATE_LABEL.failed,
  running: ROW_STATE_LABEL.running,
  incomplete: 'Gate not passed',
  notStarted: ROW_STATE_LABEL.notStarted,
  unknown: ROW_STATE_LABEL.unknown,
  absent: ROW_STATE_LABEL.absent,
};

export interface SegmentPaint {
  fill: string;
  border: string;
  borderStyle: 'solid' | 'dashed' | 'none';
  opacity: number;
  /** The one animated mark on the surface (running), off under reduced motion. */
  animated: boolean;
  /** A 3px left edge — awaitingHuman's "loudest thing on the screen". */
  accentEdge?: string;
}

/**
 * The spine segment for one state (spec §7.2's state table, as geometry): only
 * the four states that assert something HAPPENED are filled; `unknown` is a
 * dashed hairline (chip-less, fill-less — that IS the signal), `notStarted` a
 * solid hollow hairline, `absent` a gap at 40% opacity.
 */
export function segmentPaint(t: Tokens, state: SegmentState): SegmentPaint {
  switch (state) {
    case 'complete': {
      const f = taskDetailStateFill(t, 'passed');
      return {
        fill: f.border,
        border: f.border,
        borderStyle: 'solid',
        opacity: 1,
        animated: false,
      };
    }
    case 'awaitingHuman': {
      const f = taskDetailStateFill(t, 'awaitingHuman');
      return {
        fill: f.bg,
        border: f.border,
        borderStyle: 'solid',
        opacity: 1,
        animated: false,
        accentEdge: t.accent,
      };
    }
    case 'failed': {
      const f = taskDetailStateFill(t, 'failed');
      return { fill: f.bg, border: f.border, borderStyle: 'solid', opacity: 1, animated: false };
    }
    case 'running': {
      const f = taskDetailStateFill(t, 'running');
      return { fill: f.bg, border: f.border, borderStyle: 'solid', opacity: 1, animated: true };
    }
    case 'incomplete':
      return {
        fill: 'transparent',
        border: t.muted,
        borderStyle: 'solid',
        opacity: 1,
        animated: false,
      };
    case 'notStarted':
      return {
        fill: 'transparent',
        border: t.line,
        borderStyle: 'solid',
        opacity: 1,
        animated: false,
      };
    case 'unknown':
      return {
        fill: 'transparent',
        border: t.line,
        borderStyle: 'dashed',
        opacity: 1,
        animated: false,
      };
    case 'absent':
      return {
        fill: 'transparent',
        border: t.line,
        borderStyle: 'none',
        opacity: 0.4,
        animated: false,
      };
  }
}

export interface TickPaint {
  color: string;
  hollow: boolean;
  dashed: boolean;
}

/** One task tick (LOD-1), from the list's own task-row state. */
export function tickPaint(t: Tokens, state: RowState): TickPaint {
  switch (state) {
    case 'passed':
      return { color: taskDetailStateFill(t, 'passed').border, hollow: false, dashed: false };
    case 'failed':
      return { color: taskDetailStateFill(t, 'failed').border, hollow: false, dashed: false };
    case 'running':
      return { color: taskDetailStateFill(t, 'running').border, hollow: false, dashed: false };
    case 'awaitingHuman':
      return {
        color: taskDetailStateFill(t, 'awaitingHuman').border,
        hollow: false,
        dashed: false,
      };
    case 'skipped':
    case 'notStarted':
      return { color: t.muted, hollow: true, dashed: false };
    case 'unknown':
      return { color: t.line, hollow: true, dashed: true };
    case 'superseded':
    case 'absent':
      return { color: t.line, hollow: true, dashed: false };
  }
}

/**
 * Why Sort and "Expand to current phase" are off in the GRAPH lens (Decision
 * D4): a card's position is the architecture's, never a sort key, and every
 * lane already shows its whole lifecycle — there is nothing to expand.
 */
export const GRAPH_LIST_ONLY_REASON =
  "The graph's positions are the architecture's own and every lane already shows its whole lifecycle — sort and expand apply to the list lens.";

// ---------------------------------------------------------------------------
// The ribbon's count (spec §9.2, Decision D8)
// ---------------------------------------------------------------------------

/**
 * `k/n` only when the ribbon model carries a trusted count; an em dash when it
 * does not (a count over reconstructed or unrecorded evidence would be a
 * laundered aggregate — never a badged number); `gates N` for a milestone with
 * no feeders of its own (M0).
 */
export function ribbonCountLabel(m: {
  feeders: readonly string[];
  gates: readonly string[];
  complete?: number;
}): string {
  if (m.feeders.length === 0) return `gates ${String(m.gates.length)}`;
  if (m.complete === undefined) return '—';
  return `${String(m.complete)}/${String(m.feeders.length)}`;
}

// ---------------------------------------------------------------------------
// The up/sideways check (spec R5, App C §3.4, Decision D3)
// ---------------------------------------------------------------------------

/**
 * Named for what it checks — edge DIRECTION only (upward, and sideways other
 * than the one sanctioned case). It is not the whole App C layering standard:
 * Client-entry, queued-target and Don't 6b are Design Health's, not this
 * view's, so the label does not claim them (code review, graph lens).
 */
export function layeringCheckText(m: {
  alarms: { up: number; sideways: number };
  sanctionedSideways: number;
}): string {
  const base = `Up/sideways check: ${String(m.alarms.up)} upward · ${String(m.alarms.sideways)} sideways`;
  return m.sanctionedSideways > 0
    ? `${base} · ${String(m.sanctionedSideways)} queued Manager→Manager (sanctioned, App C §3.4)`
    : base;
}
