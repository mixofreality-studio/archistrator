/**
 * THE per-activity schedule join — the ONE both lenses read (architect Q2
 * ruling: "effort, float and critical path may render now, on both lenses,
 * from ONE shared join: the list's `activityMeta`, lifted so both lenses read
 * it"). ConstructionConsole calls it once and builds the one activity tree from
 * it; the LIST's rails and the GRAPH's lanes then read the same node fields.
 *
 * Joined from the committed MODELS, never from toNetworkView: that view
 * defaults a missing CPM entry to `float: 0` and a missing activity-list entry
 * to `days: 0`, and a fabricated zero float renders as "on the critical path" —
 * the loudest possible lie on either lens. Here an activity with no computed
 * entry gets NO float, NO band and NO criticality: absence stays absence, and
 * both lenses draw no channel for it.
 *
 * What it carries is deliberately narrow — the unstaffed derived-network
 * figures the ruling allows (slot 9 `effortDays`; slot 10 `computed[id]`'s
 * `totalFloat`, `onCriticalPath`, `band`) plus the label and componentId the
 * list already joined. Event times, durations, dates, cost, risk and EV stay
 * OFF (the ruling's "stays OFF" list), so they are not even read here.
 *
 * Pure — no React — pinned by activityMeta.test.ts.
 */
import type { ActivityMeta } from './activityTree.ts';

/** The fields of a committed activity-list item this join reads (slot 9). */
export interface ActivityListItemLike {
  name: string;
  title?: string;
  effortDays?: number;
  componentId?: string;
}

/** The fields of one `network.computed` entry this join reads (slot 10). */
export interface NetworkComputedLike {
  totalFloat: number;
  onCriticalPath: boolean;
  band: string;
}

export function activityMetaFor(
  activities: readonly ActivityListItemLike[] | null | undefined,
  computed: Readonly<Record<string, NetworkComputedLike | undefined>> | undefined
): Record<string, ActivityMeta> {
  const byId: Record<string, ActivityMeta> = {};
  for (const a of activities ?? []) {
    const cpm = computed?.[a.name];
    byId[a.name] = {
      ...(a.title !== undefined && a.title.length > 0 ? { label: a.title } : {}),
      ...(a.effortDays !== undefined ? { effortDays: a.effortDays } : {}),
      ...(cpm !== undefined
        ? { float: cpm.totalFloat, onCriticalPath: cpm.onCriticalPath, band: cpm.band }
        : {}),
      // Task 11's search matches activity id / title / componentId — joined the
      // same way as `label`; a project-wide activity (N-STP, N-IT, …) builds no
      // single component, so it carries none.
      ...(a.componentId !== undefined && a.componentId.length > 0
        ? { componentId: a.componentId }
        : {}),
    };
  }
  return byId;
}
