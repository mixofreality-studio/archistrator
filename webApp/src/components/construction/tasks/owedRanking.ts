/**
 * What each owed decision costs to leave, which rule opened it, and the order the
 * TASKS lens lists them in (Stage C Task 2). Pure; node:test reaches it.
 *
 * BLAST RADIUS
 * ------------
 * The activities that cannot start until this decision is made: every activity
 * transitively downstream of it in the committed network, walking dependency rows
 * and milestone fan-ins (a milestone is an event, traversed but never counted). The
 * walk STOPS at an activity already done — its dependants' dependency on it is
 * satisfied, so they are not waiting on this decision through it.
 *
 * WHY — WHICH RULE OPENED THE GATE
 * --------------------------------
 * The server decides whether a (kind, lifecycle phase) is gated with
 * `ReviewPolicy.EffectiveGate` (projectstateaccess.go): the non-overridable risk
 * floor first (a construction dispatch — or local merge — of an activity whose
 * contract touches deploy/spend/schema), then the preset switch (vibes / checkpoints
 * / full), then the explicit per-kind map when no preset is set. The client cannot
 * see the floor's input (the contract keyword scan runs once, at workflow start), and
 * restating that keyword list here would be a hand-mirror. So attribution works by
 * elimination: when the CURRENT policy gates this (kind, phase), the policy opened it;
 * when it does not, the only rule left is the risk floor. The tooltip says the one
 * caveat — the workflow snapshots the policy at its start, so a policy edited since
 * could also explain an open gate.
 *
 * The preset → phase table below is `EffectiveGate`'s switch, restated FOR
 * ATTRIBUTION ONLY: nothing on the client decides whether to gate. If the server's
 * switch changes, owedRanking.test.ts's preset case is the one to update with it.
 *
 * ORDER (spec §6)
 * ---------------
 * Risk-floor gates first (they never converge away), then blast radius descending,
 * then age descending — unknown today (no gate-open time reaches the client; plan
 * Q2), so ties fall through — then activity id, so the order is total and stable
 * across the 1.5s poll.
 */
import type { NetworkModel, ReviewPolicyView } from '../../../contracts/types';
import { FAILURE_REASON_LABEL } from '../../../contracts/constructionAdapters.ts';
import {
  CANONICAL_PHASE_NAME,
  KIND_NOUN,
  isLifecyclePhase,
} from '../detail/bodies/taskBriefing.ts';
import type { OwedItem } from './owedWork.ts';

// ---------------------------------------------------------------------------
// Blast radius
// ---------------------------------------------------------------------------

/** Successor index over activities AND milestones: node → the nodes that depend on it. */
function successorIndex(network: NetworkModel): Map<string, string[]> {
  const next = new Map<string, string[]>();
  const link = (from: string, to: string): void => {
    const list = next.get(from);
    if (list === undefined) next.set(from, [to]);
    else list.push(to);
  };
  for (const d of network.dependencies ?? []) {
    for (const p of d.dependsOn ?? []) link(p, d.activity);
  }
  for (const m of network.milestones ?? []) {
    for (const p of m.dependsOn ?? []) link(p, m.id);
  }
  return next;
}

/** Every activity transitively waiting on `activityId`, sorted by id. */
export function downstreamOf(
  network: NetworkModel,
  activityId: string,
  done: ReadonlySet<string>
): string[] {
  const milestones = new Set((network.milestones ?? []).map((m) => m.id));
  const next = successorIndex(network);
  const seen = new Set<string>([activityId]);
  const waiting = new Set<string>();
  const queue = [activityId];
  for (let node = queue.shift(); node !== undefined; node = queue.shift()) {
    for (const succ of next.get(node) ?? []) {
      if (seen.has(succ)) continue;
      seen.add(succ);
      // Done: its dependants are satisfied through it, so the wait stops here.
      if (done.has(succ)) continue;
      if (!milestones.has(succ)) waiting.add(succ);
      queue.push(succ);
    }
  }
  return [...waiting].sort((a, b) => a.localeCompare(b));
}

// ---------------------------------------------------------------------------
// WHY
// ---------------------------------------------------------------------------

export interface OwedWhy {
  /** One line: the rule, in the operator's words. */
  rule: string;
  /** The gate is attributed to the non-overridable risk floor (by elimination). */
  riskFloor: boolean;
  tooltip: string;
}

/** EffectiveGate's preset switch — attribution only (see the module comment). */
export const PRESET_GATES: Readonly<Record<string, readonly string[] | 'all'>> = {
  vibes: [],
  checkpoints: ['detailed_design', 'construction', 'integration'],
  full: 'all',
};

function phaseName(lifecyclePhase: string | undefined): string {
  if (lifecyclePhase === undefined) return 'an unreported phase';
  return isLifecyclePhase(lifecyclePhase) ? CANONICAL_PHASE_NAME[lifecyclePhase] : lifecyclePhase;
}

/** Which rule of the current policy gates (kind, phase), if any. */
function policyRuleFor(
  policy: ReviewPolicyView | undefined,
  kind: OwedItem['kind'],
  lifecyclePhase: string
): string | undefined {
  const preset = policy?.preset ?? '';
  const presetGates = PRESET_GATES[preset];
  if (presetGates !== undefined) {
    const gated = presetGates === 'all' || presetGates.includes(lifecyclePhase);
    return gated ? `Preset “${preset}” · ${phaseName(lifecyclePhase)}` : undefined;
  }
  if (kind === undefined) return undefined;
  const gated = (policy?.gatedPhasesByType[kind] ?? []).includes(lifecyclePhase);
  return gated ? `Policy · ${KIND_NOUN[kind]} › ${phaseName(lifecyclePhase)}` : undefined;
}

export function whyFor(item: OwedItem, policy: ReviewPolicyView | undefined): OwedWhy {
  switch (item.reason) {
    case 'takeover':
      return {
        rule: 'Intervention engine escalated a variance',
        riskFloor: false,
        tooltip:
          'The intervention engine escalated a variance (decideOnVariance) and the workflow is waiting for an operator steer.',
      };
    case 'failed': {
      const label = FAILURE_REASON_LABEL[item.failure?.reason ?? 'unknown'];
      return {
        rule: `Failed · ${label}`,
        riskFloor: false,
        tooltip:
          'The pump recorded a terminal failure for this activity; it will not restart on its own.',
      };
    }
    case 'gate': {
      const lifecyclePhase = item.gate?.lifecyclePhase;
      const rule =
        lifecyclePhase !== undefined ? policyRuleFor(policy, item.kind, lifecyclePhase) : undefined;
      if (rule !== undefined) {
        return {
          rule,
          riskFloor: false,
          tooltip:
            "The project's current review policy gates this phase. The workflow uses the policy captured when the activity started.",
        };
      }
      return {
        rule: 'Risk floor · non-overridable',
        riskFloor: true,
        tooltip:
          'The current review policy does not gate this phase, so the gate is the risk floor: a construction dispatch or merge of a contract that touches deploy, spend or schema always needs a human. (A policy in force when this activity started, since edited, would also explain it.)',
      };
    }
  }
}

// ---------------------------------------------------------------------------
// Ranking
// ---------------------------------------------------------------------------

export interface OwedBlast {
  /** Activities transitively waiting on this decision — the count, and the ids,
   *  so a header can count the UNION across rows instead of double-counting an
   *  activity waiting on two decisions. */
  downstream: number;
  downstreamIds: readonly string[];
  /** The activity's own total float and critical-path flag from the committed CPM —
   *  ABSENT when the network carries no entry for it, never a fabricated 0 (which
   *  would render as "on the critical path"). */
  float?: number;
  onCriticalPath?: boolean;
  band?: string;
}

export type RankedOwed = OwedItem & { blast: OwedBlast; why: OwedWhy };

export function rankOwed(
  items: readonly OwedItem[],
  ctx: {
    network: NetworkModel | undefined;
    done: ReadonlySet<string>;
    policy: ReviewPolicyView | undefined;
  }
): RankedOwed[] {
  const ranked = items.map((item): RankedOwed => {
    const cpm = ctx.network?.computed?.[item.activityId];
    const downstreamIds =
      ctx.network !== undefined ? downstreamOf(ctx.network, item.activityId, ctx.done) : [];
    return {
      ...item,
      blast: {
        downstream: downstreamIds.length,
        downstreamIds,
        ...(cpm !== undefined
          ? { float: cpm.totalFloat, onCriticalPath: cpm.onCriticalPath, band: cpm.band }
          : {}),
      },
      why: whyFor(item, ctx.policy),
    };
  });
  return ranked.sort(
    (a, b) =>
      Number(b.why.riskFloor) - Number(a.why.riskFloor) ||
      b.blast.downstream - a.blast.downstream ||
      a.activityId.localeCompare(b.activityId)
  );
}
