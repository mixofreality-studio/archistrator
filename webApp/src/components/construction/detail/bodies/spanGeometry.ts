/**
 * The subagent-span gantt GEOMETRY — where each span sits inside its episode's
 * own wall clock.
 *
 * EpisodesPanel already REPORTS the spans (a count, and one lineage-tree row
 * each) and already captions the token split with "excludes N subagent spans".
 * What it cannot show is SHAPE: whether four subagents ran side by side for
 * twenty seconds or one after another for two minutes each. That is the whole
 * difference between a parallel fan-out and a serial chain, and it is the first
 * thing anyone reading a slow episode wants to know.
 *
 * Pure percentages of the episode's window, not pixels: the strip is rendered
 * into whatever width the pane has (360-820px, user-resizable), so the geometry
 * cannot be decided here in absolute units.
 *
 * UNTIMED IS A FIRST-CLASS ANSWER. `SubagentSpan.startedAt` / `endedAt` are both
 * optional on the wire, and an episode's own window can be junk. A span that
 * cannot be placed is marked `untimed` and drawn as a dashed full-width track —
 * never dropped (the span DID happen; the clock is what is missing) and never
 * given a plausible position, which would be a fabricated measurement in the one
 * view whose entire purpose is measurement.
 */

/** The subset of a wire `SubagentSpan` this module needs. */
export interface SpanLike {
  toolUseId: string;
  startedAt?: string;
  endedAt?: string;
}

export interface GanttBar {
  toolUseId: string;
  /** Left edge as a percentage of the episode's window. 0 when untimed. */
  leftPct: number;
  /** Width as a percentage of the episode's window. 100 when untimed. */
  widthPct: number;
  /** True when the span (or its episode) carries no usable clock. */
  untimed: boolean;
  /** Milliseconds, when both ends parsed. Absent otherwise — never a guessed 0. */
  durationMs?: number;
}

/**
 * The narrowest bar we will draw. A 200ms span inside a 40-minute episode is
 * 0.008% wide and would render as nothing at all — which reads as "this span is
 * missing", the opposite of the truth. Floored at something visible, and the
 * duration is printed beside the bar so the floor never has to carry a
 * quantitative claim.
 */
const MIN_WIDTH_PCT = 1.5;

function parse(iso: string | undefined): number | undefined {
  if (iso === undefined || iso.length === 0) return undefined;
  const ms = Date.parse(iso);
  return Number.isNaN(ms) ? undefined : ms;
}

export function ganttBarsFor(episode: {
  startedAt: string;
  endedAt: string;
  subagentSpans?: readonly SpanLike[];
}): GanttBar[] {
  const spans = episode.subagentSpans ?? [];
  const start = parse(episode.startedAt);
  const end = parse(episode.endedAt);
  // A zero-length or inverted episode window is not a scale anything can be
  // placed against — every span is untimed rather than all of them crushed to
  // the left edge.
  const window = start !== undefined && end !== undefined && end > start ? end - start : undefined;

  return spans.map((span) => {
    const spanStart = parse(span.startedAt);
    const spanEnd = parse(span.endedAt);
    if (window === undefined || start === undefined || spanStart === undefined) {
      return { toolUseId: span.toolUseId, leftPct: 0, widthPct: 100, untimed: true };
    }
    const leftPct = clampPct(((spanStart - start) / window) * 100);
    if (spanEnd === undefined || spanEnd < spanStart) {
      // Started, never recorded as finished: place the start truthfully and draw
      // the minimum bar rather than inventing an end.
      return { toolUseId: span.toolUseId, leftPct, widthPct: MIN_WIDTH_PCT, untimed: true };
    }
    const rawWidth = ((spanEnd - spanStart) / window) * 100;
    return {
      toolUseId: span.toolUseId,
      leftPct,
      widthPct: clampPct(Math.max(MIN_WIDTH_PCT, rawWidth), 100 - leftPct),
      untimed: false,
      durationMs: spanEnd - spanStart,
    };
  });
}

function clampPct(value: number, max = 100): number {
  if (!Number.isFinite(value)) return 0;
  return Math.min(Math.max(value, 0), max);
}

/** `1.2s` / `45s` / `3m 05s` — the numeral that carries the quantitative claim. */
export function formatSpanDuration(ms: number | undefined): string {
  if (ms === undefined) return '—';
  if (ms < 1000) return `${String(ms)}ms`;
  const seconds = ms / 1000;
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 1 : 0)}s`;
  const minutes = Math.floor(seconds / 60);
  const rem = Math.round(seconds % 60);
  return `${String(minutes)}m ${String(rem).padStart(2, '0')}s`;
}
