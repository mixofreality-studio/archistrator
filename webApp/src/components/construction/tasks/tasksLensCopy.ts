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

/** The degraded banner: a bold title and one sentence, then the link to set one. */
export interface PolicyBanner {
  title: string;
  body: string;
}

/**
 * The permanent degraded banner while no review policy is recorded (spec §7.7,
 * amended 2026-09-12): the designer's one-liner, which is the orchestrator's
 * reconciliation of the PM's Q3 copy with designer P1-5. With no policy the server
 * gates only the risk floor. The rest of what the PM's sentence said — failures
 * and escalations show here under any policy, and where the rows come from — is
 * the headline's tooltip (HEADLINE_TOOLTIP), not a second paragraph here.
 */
export function policyBannerFor(policy: ReviewPolicyView | undefined): PolicyBanner | undefined {
  if (!policyIsEmpty(policy)) return undefined;
  return {
    title: 'No review policy recorded.',
    body: 'Only the risk floor is gated — changes touching deploy, spend or schema always ask you; everything else proceeds without asking.',
  };
}

export const SET_POLICY_LABEL = 'Set a review policy →';

/** The headline's tooltip: what the lens shows under any policy, and from where. */
export const HEADLINE_TOOLTIP =
  'Failures and escalations show here under any policy. The rows come from each running activity’s live workflow stage.';

/**
 * What a row's WHY offers about turning its question off (designer P1-5):
 * "stop asking" only where a POLICY rule opened the gate — the one thing a policy
 * edit can change — and a risk-floor gate says it can't be turned off. A steer or a
 * failure is not a policy question at all.
 */
export function whyAffordanceFor(
  item: Pick<RankedOwed, 'reason' | 'why' | 'gate'>
): 'stopAsking' | 'stopAskingFloorMayAsk' | 'cantTurnOff' | undefined {
  if (item.reason !== 'gate') return undefined;
  if (item.why.riskFloor) return 'cantTurnOff';
  // The floor gates a CONSTRUCTION dispatch whose contract touches deploy, spend or
  // schema, under every policy (EffectiveGate) — and the client cannot see that
  // contract scan. So "stop asking" on a construction gate is hedged: turning the
  // rule off may not stop this one (tasks round 2, designer).
  return item.gate?.lifecyclePhase === 'construction' ? 'stopAskingFloorMayAsk' : 'stopAsking';
}

export const CANT_TURN_OFF_LABEL = 'Can’t be turned off';

/** Said after "stop asking" where the risk floor could still hold the activity. */
export const FLOOR_MAY_STILL_ASK_LABEL = '…the risk floor may still ask';
export const FLOOR_MAY_STILL_ASK_TOOLTIP =
  'Under every policy, a construction dispatch whose contract touches deploy, spend or schema still needs you. Turning this rule off may not stop this gate.';

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

/** The Retry beside it: "Retrying…" while a failed probe is being asked again
 *  (designer re-check B1), so a click is seen to do something. */
export function retryLabel(retrying: boolean): string {
  return retrying ? 'Retrying…' : 'Retry';
}

/** The TASKS badge: its text, and what it means on hover. */
export interface TasksBadge {
  text: string;
  title: string;
}

/**
 * The TASKS badge (designer re-check): the owed count, and a "?" while some
 * in-flight activity has not been checked — blank there read as "nothing is owed",
 * which the lens itself refuses to say. Nothing at all only when nothing is owed
 * AND every probe answered.
 */
export function tasksBadgeFor(owed: number, unchecked: number): TasksBadge | undefined {
  const owedText = `${plural(owed, 'decision', 'decisions')} owed`;
  if (unchecked === 0) return owed > 0 ? { text: String(owed), title: owedText } : undefined;
  const notChecked = `${inFlight(unchecked)} not checked yet`;
  return owed > 0
    ? { text: `${String(owed)}?`, title: `${owedText} · ${notChecked}` }
    : { text: '?', title: notChecked };
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

/**
 * Every activity in exactly one bucket, so the line sums to the activity count
 * (designer final pass: it read 27 of 29, dropping the two integration-pending
 * rows, next to "Construction running…").
 */
export interface EmptyStateCounts {
  eligible: number;
  /** Integration-pending activities still waiting on a dependency (pendingResume). */
  waiting: number;
  inFlight: number;
  blocked: number;
  /** Integrated — the work already behind you (designer P2). */
  done: number;
  /** A recorded terminal failure (the TASKS rows above already list each one). */
  failed: number;
  /** An activity the server could not classify. */
  unclassified: number;
}

/**
 * The partition, one bucket per activity in `statuses` (computeActivityStatuses):
 *   - `waiting` first: an integration-pending row still waiting on a dependency.
 *     Its status reads blocked; the line names it for what it is.
 *   - `inFlight`: what the pump started and has not finished (the probe
 *     candidates, owedWork.probeCandidatesFor) — a fresh pickup with no evidence
 *     yet reads eligible by status — plus any status that is work under way.
 *   - then eligible / blocked / done / failed / unclassified by status.
 */
export function emptyStateCounts(
  statuses: ReadonlyMap<string, BuildStatus>,
  sets: { inFlight: ReadonlySet<string>; waiting: ReadonlySet<string> }
): EmptyStateCounts {
  const c: EmptyStateCounts = {
    eligible: 0,
    waiting: 0,
    inFlight: 0,
    blocked: 0,
    done: 0,
    failed: 0,
    unclassified: 0,
  };
  for (const [id, s] of statuses) c[bucketFor(id, s, sets)] += 1;
  return c;
}

function bucketFor(
  id: string,
  s: BuildStatus,
  sets: { inFlight: ReadonlySet<string>; waiting: ReadonlySet<string> }
): keyof EmptyStateCounts {
  if (sets.waiting.has(id)) return 'waiting';
  if (sets.inFlight.has(id)) return 'inFlight';
  switch (s) {
    case 'eligible':
      return 'eligible';
    case 'blocked':
      return 'blocked';
    case 'integrated':
      return 'done';
    case 'failed':
      return 'failed';
    case 'unclassified':
      return 'unclassified';
    case 'in-review':
    case 'in-construction':
    case 'in-detailed-design':
    case 'not-started':
      // Under way by status (the live override's `not-started` included): in flight.
      return 'inFlight';
  }
}

export function emptyStateLine(c: EmptyStateCounts): string {
  const parts = [`${String(c.eligible)} eligible`];
  if (c.waiting > 0) parts.push(`${String(c.waiting)} waiting on dependencies`);
  parts.push(`${String(c.inFlight)} in flight`, `${String(c.blocked)} blocked`);
  parts.push(`${String(c.done)} done`);
  if (c.failed > 0) parts.push(`${String(c.failed)} failed`);
  if (c.unclassified > 0) parts.push(`${String(c.unclassified)} unclassified`);
  return parts.join(' · ');
}

/** The empty card's Begin, as a link that says what it would start with (designer
 *  P2): "Begin construction — 4 eligible". It opens the same confirm step. */
export function beginLinkLabel(label: string, eligible: number): string {
  return `${label} — ${String(eligible)} eligible`;
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
