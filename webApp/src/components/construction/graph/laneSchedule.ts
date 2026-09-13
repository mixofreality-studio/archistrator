/**
 * A lane's SCHEDULE channels — effort, float and the critical path — from the
 * one shared join (list/activityMeta.ts) the list lens reads too (architect Q2
 * ruling). The graph never re-derives any of them.
 *
 *  - EFFORT sets the lane spine's LENGTH: slot 9 `effortDays` over the widest
 *    effort in the plan (the list's own effortBarFraction). The segments inside
 *    keep their Table A-1 proportions, so a longer spine is the same lifecycle,
 *    drawn longer. With no effort on record there is no fraction: the spine is
 *    drawn at the track's full width and says "effort not on record" rather
 *    than invent a length.
 *  - FLOAT is a rail PLUS an always-visible numeral (colour is never the sole
 *    carrier — WCAG 1.4.1), from `computed[id].totalFloat` and the server's
 *    `band`, passed through. A lane with no computed entry has NO float at all:
 *    no rail, no numeral — never a 0, which would read as "on the critical path".
 *  - CRITICAL PATH is the lane's left edge: 2px, or 3px in full ink on the path
 *    (the list's criticalBorderPx), running the lane's full height.
 *
 * All three belong to the LANE — never to a card, never to an edge (pinned by
 * laneSchedule.test.ts against graphCardPresentation and graphEdges) — and none
 * is a layout input: positions stay a function of the architecture and the
 * activity set alone. They are UNSTAFFED derived-network figures, captioned so
 * in the key. Event times, durations, dates, cost, risk, EV/SPI/% and the
 * TotalWeeks figure stay OFF.
 *
 * Pure — no React — pinned by laneSchedule.test.ts.
 */
import type { FloatBand } from '../../../contracts/types.ts';
import {
  criticalBorderPx,
  effortBarFraction,
  floatBandOf,
  floatPresentation,
} from '../list/activityRowPresentation.ts';

/** The key's caption for every schedule channel on the canvas (architect Q2). */
export const SCHEDULE_CAPTION = 'Float and critical path of the derived network, unstaffed.';

export interface LaneFloat {
  days: number;
  /** Always rendered beside the rail. */
  numeral: string;
  /** The server's band, narrowed; absent when unrecognised (the rail then takes the line ink). */
  band?: FloatBand;
}

export interface LaneSchedule {
  /** Slot 9 effort, when on record. */
  effortDays?: number;
  /** The spine's share of the lane's track, 0..1; absent = no effort on record. */
  spineFraction?: number;
  /** Absent when the network has no computed entry for this activity. */
  float?: LaneFloat;
  critical: boolean;
  /** The lane's left edge weight. */
  borderPx: 2 | 3;
}

/** The fields of an activity node the schedule reads (ActivityNode fits). */
export interface ScheduleBearing {
  effortDays?: number;
  float?: number;
  band?: string;
  onCriticalPath?: boolean;
}

/** The widest effort in the plan — the spine scale. Absent efforts add nothing. */
export function maxEffortOf(nodes: readonly ScheduleBearing[]): number {
  return nodes.reduce((max, n) => Math.max(max, n.effortDays ?? 0), 0);
}

export function laneScheduleFor(node: ScheduleBearing, maxEffortDays: number): LaneSchedule {
  const spineFraction = effortBarFraction(node.effortDays, maxEffortDays);
  const band = floatBandOf(node.band);
  const critical = node.onCriticalPath === true;
  return {
    ...(node.effortDays !== undefined ? { effortDays: node.effortDays } : {}),
    ...(spineFraction !== undefined ? { spineFraction } : {}),
    ...(node.float !== undefined
      ? {
          float: {
            days: node.float,
            numeral: floatPresentation(node.float, band).numeral,
            ...(band !== undefined ? { band } : {}),
          },
        }
      : {}),
    critical,
    borderPx: criticalBorderPx(node.onCriticalPath),
  };
}

/** The float rail's tooltip — the numeral's meaning, and whose figure it is. */
export function floatTooltip(f: LaneFloat): string {
  return `Total float ${f.numeral} ${f.days === 1 ? 'day' : 'days'} — the derived network, unstaffed`;
}

/** The hover card's schedule line — only what is known, nothing invented. */
export function scheduleLine(s: LaneSchedule): string {
  return [
    effortText(s),
    // Named as what it is (designer re-check): the derived network's float,
    // without staffing — never a staffed option's.
    ...(s.float !== undefined ? [`total float ${s.float.numeral} (unstaffed)`] : []),
    ...(s.critical ? ['critical path'] : []),
  ].join(' · ');
}

/** The spine's effort line, for its title and the hover card. */
export function effortText(s: LaneSchedule): string {
  return s.effortDays !== undefined
    ? `${String(s.effortDays)} ${s.effortDays === 1 ? 'day' : 'days'} of effort`
    : 'effort not on record';
}
