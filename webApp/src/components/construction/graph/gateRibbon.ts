/**
 * The gate ribbon — the network's milestones, lifted off the canvas into a
 * strip across the top (spec §7.6: "Milestones leave the canvas and become a
 * gate ribbon across the top"), so they never pan away and never tangle the
 * architecture's own edges.
 *
 * WHAT A MILESTONE SAYS, AND WHAT IT MAY NOT (Decisions D8, D12)
 * --------------------------------------------------------------
 *  - its FEEDERS — the activities its `dependsOn` names (structural, always);
 *  - what it GATES — every activity whose dependencies list it (structural;
 *    this is all M0 can say, having no fan-in of its own);
 *  - its PROVENANCE — the worst origin among its feeders;
 *  - how many feeders are COMPLETE (`percentComplete === 100`, App A's binary
 *    exit arithmetic) — but ONLY when every feeder's evidence was observed.
 *
 * That last condition is spec §9.2 ("no laundered aggregate"): a count
 * computed over reconstructed or unrecorded evidence would put a
 * trustworthy-looking number on work nobody watched happen. Where the
 * condition fails the count is ABSENT and the renderer reads "—" — never a
 * badged number. No event time is carried at all (R5's CPM tripwire, D2).
 *
 * Pure — no React — pinned by gateRibbon.test.ts.
 */
import type { ActivityNode } from '../list/activityTree.ts';
import { worstOriginOf, type ProvenanceOrigin } from '../provenanceAxis.ts';

export interface RibbonMilestoneInput {
  id: string;
  name: string;
  dependsOn?: readonly string[] | null;
}

export interface RibbonDependencyInput {
  activity: string;
  dependsOn: readonly string[] | null;
}

export interface RibbonMilestone {
  id: string;
  name: string;
  /** The activities this milestone waits on, in authored order. */
  feeders: string[];
  /** The activities that wait on this milestone, by id. */
  gates: string[];
  /** Feeders at 100% — PRESENT ONLY when every feeder's evidence is observed (§9.2). */
  complete?: number;
  /** The worst origin among the feeders; `unknown` when any is unrecorded or missing. */
  provenance: ProvenanceOrigin;
}

/** Worst-first: a reconstructed feeder outranks an unrecorded one, which outranks observed. */
const RANK: Record<ProvenanceOrigin, number> = {
  observed: 0,
  unknown: 1,
  backfilled: 2,
  synthesized: 3,
};

function byCodeUnit(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

export function gateRibbonFor(
  milestones: readonly RibbonMilestoneInput[],
  dependencies: readonly RibbonDependencyInput[],
  nodes: readonly ActivityNode[]
): RibbonMilestone[] {
  const nodeById = new Map(nodes.map((n) => [n.activityId, n]));

  return milestones.map((m): RibbonMilestone => {
    const feeders = [...(m.dependsOn ?? [])];
    const gates = dependencies
      .filter((d) => (d.dependsOn ?? []).includes(m.id))
      .map((d) => d.activity)
      .sort(byCodeUnit);

    // Per-feeder origin: a feeder the tree does not carry is UNRECORDED — it can
    // neither be counted complete nor vouch for the count.
    const origins = feeders.map((id): ProvenanceOrigin => {
      const node = nodeById.get(id);
      return node === undefined ? 'unknown' : worstOriginOf(node);
    });
    const provenance = origins.reduce<ProvenanceOrigin>(
      (worst, o) => (RANK[o] > RANK[worst] ? o : worst),
      feeders.length === 0 ? 'unknown' : 'observed'
    );

    const everyFeederObserved = feeders.length > 0 && origins.every((o) => o === 'observed');
    const complete = everyFeederObserved
      ? feeders.filter((id) => nodeById.get(id)?.percentComplete === 100).length
      : undefined;

    return {
      id: m.id,
      name: m.name,
      feeders,
      gates,
      ...(complete !== undefined ? { complete } : {}),
      provenance,
    };
  });
}
