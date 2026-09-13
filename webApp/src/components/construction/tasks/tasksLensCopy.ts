/**
 * Every sentence the TASKS lens says (Stage C Task 4), as pure functions over the
 * ranked owed set — so the words can be pinned by node:test and never say more
 * than the data behind them.
 */
import type { CiStatus, ReviewPolicyView } from '../../../contracts/types';
import type { BuildStatus } from '../../../contracts/constructionAdapters.ts';
import { FAILURE_REASON_LABEL } from '../../../contracts/constructionAdapters.ts';
import {
  CANONICAL_PHASE_NAME,
  KIND_NOUN,
  isLifecyclePhase,
} from '../detail/bodies/taskBriefing.ts';
import type { ActivityKind } from '../KindBadge';
import { PRESET_GATES, type RankedOwed } from './owedRanking.ts';

const plural = (n: number, one: string, many: string): string =>
  `${String(n)} ${n === 1 ? one : many}`;

function phaseName(lifecyclePhase: string): string {
  return isLifecyclePhase(lifecyclePhase) ? CANONICAL_PHASE_NAME[lifecyclePhase] : lifecyclePhase;
}

function kindNoun(kind: string): string {
  return Object.prototype.hasOwnProperty.call(KIND_NOUN, kind)
    ? KIND_NOUN[kind as keyof typeof KIND_NOUN]
    : kind;
}

// ---------------------------------------------------------------------------
// The header
// ---------------------------------------------------------------------------

/**
 * "N decisions are blocking M downstream activities · K on the critical path".
 * M is the UNION of what waits on them (an activity waiting on two decisions is
 * one activity); K counts the decisions whose own activity is on the critical
 * path. The spec's "· D days of critical path stalled" clause is absent: it needs
 * the time each gate opened, which the client is not told (plan Q2/Q5).
 */
export function headlineFor(items: readonly RankedOwed[]): string {
  const waiting = new Set(items.flatMap((i) => i.blast.downstreamIds));
  const onCp = items.filter((i) => i.blast.onCriticalPath === true).length;
  const verb = items.length === 1 ? 'is' : 'are';
  const head = `${plural(items.length, 'decision', 'decisions')} ${verb} blocking ${plural(
    waiting.size,
    'downstream activity',
    'downstream activities'
  )}`;
  return onCp > 0 ? `${head} · ${String(onCp)} on the critical path` : head;
}

/** "G of CAP worker slots stopped at a gate" (spec §6's "3 of 3 workers idle,
 *  waiting on you") — only when a gate holds a slot and the cap is known. A failed
 *  activity no longer holds a worker, so it does not count. */
export function slotsLineFor(
  items: readonly RankedOwed[],
  cap: number | undefined
): string | undefined {
  const gates = items.filter((i) => i.reason === 'gate').length;
  if (gates === 0 || cap === undefined) return undefined;
  return `${String(gates)} of ${String(cap)} worker slots stopped at a gate`;
}

// ---------------------------------------------------------------------------
// The policy (read-only here — spec §10 cuts editing it from Tasks)
// ---------------------------------------------------------------------------

function policyIsEmpty(policy: ReviewPolicyView | undefined): boolean {
  if (policy === undefined) return true;
  if ((policy.preset ?? '').length > 0) return false;
  return Object.values(policy.gatedPhasesByType).every((phases) => phases.length === 0);
}

/**
 * The permanent degraded banner while no review policy is recorded (spec §7.7).
 * Its words are the true ones for THIS lens (plan DC6): with no policy the server
 * gates only the risk floor, and the rows come from live workflow stages — the
 * spec's draft sentence described the old queue built from activity status.
 */
export function policyBannerFor(policy: ReviewPolicyView | undefined): string | undefined {
  if (!policyIsEmpty(policy)) return undefined;
  return 'No review policy recorded for this project. With none recorded, the server gates only the non-overridable risk floor — a construction dispatch or merge whose contract touches deploy, spend or schema — and every other phase proceeds without asking. The rows below come from each running activity’s live workflow stage.';
}

/** One line: what this project gates, and always the floor. */
export function policySummaryFor(policy: ReviewPolicyView | undefined): string {
  if (policyIsEmpty(policy)) return 'Only the risk floor is gated (deploy · spend · schema).';
  const preset = policy?.preset ?? '';
  const presetGates = PRESET_GATES[preset];
  if (presetGates !== undefined) {
    if (presetGates === 'all') return `Preset “${preset}” gates every phase, plus the risk floor.`;
    if (presetGates.length === 0) return `Preset “${preset}” gates nothing but the risk floor.`;
    return `Preset “${preset}” gates ${presetGates.map(phaseName).join(', ')} for every kind, plus the risk floor.`;
  }
  const parts = Object.entries(policy?.gatedPhasesByType ?? {})
    .filter(([, phases]) => phases.length > 0)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([kind, phases]) => `${kindNoun(kind)} › ${phases.map(phaseName).join(', ')}`);
  return `Gated: ${parts.join('; ')} — plus the risk floor.`;
}

export const STOP_ASKING_LABEL = 'Stop asking me about this class of thing → review policy';

// ---------------------------------------------------------------------------
// The empty state (spec §7.7: "Nothing needs you." is not a dead end)
// ---------------------------------------------------------------------------

export interface EmptyStateCounts {
  eligible: number;
  inFlight: number;
  blocked: number;
}

/** Eligible and blocked from the network-derived statuses; in flight is what the
 *  pump started and has not finished (owedWork.probeCandidatesFor). */
export function emptyStateCounts(
  statuses: ReadonlyMap<string, BuildStatus>,
  inFlight: number
): EmptyStateCounts {
  let eligible = 0;
  let blocked = 0;
  for (const s of statuses.values()) {
    if (s === 'eligible') eligible += 1;
    else if (s === 'blocked') blocked += 1;
  }
  return { eligible, inFlight, blocked };
}

export function emptyStateLine(c: EmptyStateCounts): string {
  return `${String(c.eligible)} eligible · ${String(c.inFlight)} in flight · ${String(c.blocked)} blocked`;
}

// ---------------------------------------------------------------------------
// A row's triage fields (spec §6's seven)
// ---------------------------------------------------------------------------

/** The ask, as a sentence about the artifact rather than the machinery. */
export function askFor(item: RankedOwed): string {
  const name = item.title ?? item.activityId;
  switch (item.reason) {
    case 'takeover':
      return `Steer ${name}: ${item.variance ?? 'the intervention engine escalated a variance'}.`;
    case 'failed': {
      const why = item.failure?.detail ?? FAILURE_REASON_LABEL[item.failure?.reason ?? 'unknown'];
      return `Decide what happens next — ${item.activityId} stopped: ${why}.`;
    }
    case 'gate': {
      const exit = item.gate?.exitCriterion;
      if (exit !== undefined && exit.length > 0) {
        return `Decide whether ${exit.charAt(0).toLowerCase()}${exit.slice(1)}.`;
      }
      const phase = item.gate?.lifecyclePhase;
      if (phase === undefined) {
        return 'Decide a gate the server did not name — the activity reports no current phase.';
      }
      return `Decide the ${phaseName(phase)} gate.`;
    }
  }
}

export const WAITING_UNKNOWN_TOOLTIP =
  'Not reported: the server does not yet tell the console when a gate opened, so this lens does not guess how long it has waited.';

export function roundLabel(round: number | undefined): string {
  return round !== undefined ? `round ${String(round)}` : 'round —';
}

export interface CiVerdict {
  label: string;
  tone: 'danger' | 'ok' | 'muted';
}

/** The machine's verdict. Red CI is the highest time-saving field on the row
 *  (spec §6): it says not to read the diff yet. */
export function ciVerdictFor(ci: CiStatus | undefined): CiVerdict {
  switch (ci) {
    case 'failed':
      return { label: "CI red — don't read the diff yet", tone: 'danger' };
    case 'in_progress':
      return { label: 'CI running', tone: 'muted' };
    case 'success':
      return { label: 'CI green', tone: 'ok' };
    case undefined:
      return { label: 'no CI record', tone: 'muted' };
  }
}

/** The size and shape of what the human is about to read — only what is known. */
export function shapeFor(
  kind: ActivityKind | undefined,
  facts: { contractOps?: number; scenarios?: number }
): string {
  if (kind === 'service' && facts.contractOps !== undefined) {
    return `Contract · ${plural(facts.contractOps, 'op', 'ops')}`;
  }
  if (kind === 'testing' && facts.scenarios !== undefined) {
    return `Test plan · ${plural(facts.scenarios, 'scenario', 'scenarios')}`;
  }
  return '—';
}
