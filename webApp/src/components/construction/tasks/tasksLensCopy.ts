/**
 * Every sentence the TASKS lens says (Stage C Task 4), as pure functions over the
 * ranked owed set — so the words can be pinned by node:test and never say more
 * than the data behind them.
 */
import type { CiStatus, ReviewPolicyView } from '../../../contracts/types';
import type { FailureReason } from '../../../contracts/enums.gen';
import type { BuildStatus } from '../../../contracts/constructionAdapters.ts';
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

/** "1 shown · 3 owed" — said whenever the toolbar hides owed decisions, so the
 *  headline's count is never read as the whole owed set (review I4). */
export function filteredLineFor(shown: number, owed: number): string | undefined {
  return shown < owed ? `${String(shown)} shown · ${String(owed)} owed` : undefined;
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
// What could not be checked (architect Q1)
// ---------------------------------------------------------------------------

/** Probe candidates with no answer: still fetching, or failed without one. */
export interface UncheckedCounts {
  pending: number;
  errored: number;
}

const inFlight = (n: number): string =>
  `${String(n)} in-flight ${n === 1 ? 'activity' : 'activities'}`;

/** Said while probes are still in their first fetch. */
export function uncheckedPendingLine(n: number): string | undefined {
  return n > 0 ? `Checking ${inFlight(n)}…` : undefined;
}

/** Said when probes failed without answering — beside a Retry. */
export function uncheckedErroredLine(n: number): string | undefined {
  return n > 0 ? `Couldn't check ${inFlight(n)}` : undefined;
}

/**
 * "Nothing needs you." is a claim about EVERY in-flight activity, so it is made
 * only once every probe has answered; otherwise there is no all-clear headline at
 * all, and the unchecked lines speak instead. `rest` is the variant for when every
 * owed row has just been decided and is lingering ("Nothing else needs you.").
 */
export function allClearHeadlineFor(unchecked: UncheckedCounts, rest = false): string | undefined {
  if (unchecked.pending + unchecked.errored > 0) return undefined;
  return rest ? 'Nothing else needs you.' : 'Nothing needs you.';
}

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

/** Why the machine stopped, per recorded cause — the PM's binding copy (pm-q3-ruling). */
const FAILURE_CAUSE: Readonly<Record<FailureReason, string>> = {
  unknown: 'The run failed for a reason that was not recorded',
  pipelineFailed: 'The agent’s run failed',
  pipelineTimedOut: 'The agent’s run timed out',
  pipelineCancelled: 'The run was cancelled',
  varianceExhausted: 'Gave up after 10 attempts',
  escalationTimedOut: 'No one answered the escalation in time',
  componentUnresolved: 'The plan names a component that isn’t in the design',
  activityUnclassifiable: 'The plan gives this activity no buildable type',
  dependencyUnresolved: 'The plan depends on an activity that doesn’t exist',
  dependencyCycle: 'The plan has a dependency loop',
};

/**
 * The reason a steer-needed or failed activity is owed, as a plain sentence plus
 * what is known of the last run (the PM's copy table) — the row's ask and the
 * pane's header say the same words. Undefined for a gate, whose ask is its exit
 * criterion.
 */
export function reasonSentenceFor(
  item: Pick<RankedOwed, 'reason' | 'variance' | 'failure'>
): string | undefined {
  switch (item.reason) {
    case 'takeover':
      return item.variance !== undefined
        ? `Stopped and asking you how to proceed — ${item.variance}`
        : 'Stopped and asking you how to proceed';
    case 'failed': {
      const cause = FAILURE_CAUSE[item.failure?.reason ?? 'unknown'];
      const detail = item.failure?.detail;
      return detail !== undefined && detail.length > 0 ? `${cause} — ${detail}` : cause;
    }
    case 'gate':
      return undefined;
  }
}

/** The ask, as a sentence about the artifact rather than the machinery. */
export function askFor(item: RankedOwed): string {
  switch (item.reason) {
    case 'takeover':
    case 'failed':
      return `${reasonSentenceFor(item) ?? ''}.`;
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
