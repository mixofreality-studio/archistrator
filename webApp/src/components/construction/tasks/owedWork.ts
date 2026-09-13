/**
 * The TASKS lens's owed set — every decision the construction pipeline is stopped
 * on and waiting for a human to make (Stage C Task 1). Pure: no React, so node:test
 * reaches it (relative VALUE imports carry `.ts`).
 *
 * WHERE "OWED" COMES FROM
 * -----------------------
 * A human is owed a decision when, and only when, the running system says so:
 *
 *   - the activity's per-activity construction session is at `awaitingApproval`
 *     — a lifecycle-phase (or merge) gate is suspended on the human's decision;
 *   - its session is at `awaitingTakeover` — the intervention engine escalated a
 *     variance and the workflow waits for an operator steer;
 *   - its head-state records a terminal `failed` — the pump gave up, and the human
 *     is the reason it can restart (spec §6: "the machine is waiting for a
 *     decision" and "the machine stopped" are the same job, so one table).
 *
 * Head-state `in-review` is NOT on that list. The server derives it from phase
 * completions — "some phases are complete, not all" — so it is true of every
 * activity mid-lifecycle, gated or not. The old InterventionQueue read it as "at
 * the human gate", which is the defect spec §1 opens with.
 *
 * WHICH SESSIONS ARE WORTH ASKING
 * -------------------------------
 * A session exists only for an activity the pump dispatched and has not finished,
 * so probeCandidatesFor asks only there: the row carries the pump's start stamp and
 * neither a finish stamp nor a recorded failure. That excludes every backfilled
 * row (reconstructed evidence, no pump ever ran it) and every planned-no-record
 * row, which is all 29 today — zero probes. The pump runs at most the supervision
 * cap at once, so the probe count stays that small. The start stamp is written only
 * under the git profile; outside it this set is empty and the lens cannot see a
 * waiting gate (plan Q1).
 */
import type {
  ConstructionReviewer,
  ConstructionRow,
  ConstructionSessionState,
} from '../../../contracts/types';
import type { FailureReason } from '../../../contracts/enums.gen';
import type { ActivityKind } from '../KindBadge';
import { profileFor } from '../detail/bodies/taskBriefing.ts';

/** Why a row is owed. */
export type OwedReason = 'gate' | 'takeover' | 'failed';

/** The gate a `gate` item is suspended at. Every field past `lifecyclePhase` is
 *  resolved from the activity's generated profile and ABSENT where the profile
 *  cannot say — an unclassified row, or a phase its profile does not carry. */
export interface OwedGate {
  /** The lifecycle-phase wire name the decision is sent against (the row's
   *  currentLifecyclePhase — what PhaseGatePanel has always used). Absent when the
   *  row reports no current phase: then no decision can be addressed at all, and
   *  the lens says so instead of sending one against a guessed key. */
  lifecyclePhase?: string;
  phaseName?: string;
  /** The Figure A-1 gate task key (`designReview`, `codeReview`, …). */
  task?: string;
  /** This profile's words for the gate task, and the book's name for it. */
  label?: string;
  bookLabel?: string;
  exitCriterion?: string;
}

export interface OwedItem {
  /** `<activityId>:<gateTask>:<round>` where all three are known — the decision's
   *  own identity (spec §6: one row per (activity, gate, attempt)) — otherwise
   *  `<activityId>:<reason>`. */
  key: string;
  reason: OwedReason;
  activityId: string;
  title?: string;
  kind?: ActivityKind;
  /** Present on `gate` items only. */
  gate?: OwedGate;
  /** The gate task's attempts in the ledger, the pending one included. Absent when
   *  the ledger holds none for it — no live writer appends attempts yet. */
  round?: number;
  /** The reviewEngine's computed reviewer set, from the session. */
  reviewers: readonly ConstructionReviewer[];
  /** `takeover` items: the flagged variance the engine escalated. */
  variance?: string;
  /** `failed` items: the recorded terminal reason and its detail. */
  failure?: { reason: FailureReason; detail?: string };
}

/**
 * Per activity: the live session, `null` where the probe ESTABLISHED there is none
 * (the dormant-pump 404), and absent where it was not asked or has not answered.
 */
export type SessionsByActivity = Readonly<
  Record<string, ConstructionSessionState | null | undefined>
>;

/** The activities whose session is worth probing: dispatched by the pump, not yet
 *  finished, not failed. Sorted, so a stable list yields stable query keys. */
export function probeCandidatesFor(
  rows: Readonly<Record<string, ConstructionRow>> | undefined
): string[] {
  return Object.values(rows ?? {})
    .filter(
      (r) =>
        r.recorded &&
        r.startedAt !== undefined &&
        r.completedAt === undefined &&
        r.status !== 'failed' &&
        r.failureReason === undefined
    )
    .map((r) => r.activityId)
    .sort((a, b) => a.localeCompare(b));
}

function gateFor(row: ConstructionRow): OwedGate {
  const lifecyclePhase = row.currentLifecyclePhase;
  if (lifecyclePhase === undefined) return {};
  const phase = profileFor(row)?.find((p) => p.phase === lifecyclePhase);
  if (phase === undefined) return { lifecyclePhase };
  const task = phase.tasks.find((tk) => tk.gate);
  return {
    lifecyclePhase,
    phaseName: phase.name,
    exitCriterion: phase.exitCriterion,
    ...(task !== undefined
      ? { task: task.task, label: task.label, bookLabel: task.bookLabel }
      : {}),
  };
}

function base(
  row: ConstructionRow,
  reason: OwedReason,
  titleFor: ((id: string) => string | undefined) | undefined
): Pick<OwedItem, 'reason' | 'activityId' | 'title' | 'kind'> {
  const title = titleFor?.(row.activityId);
  return {
    reason,
    activityId: row.activityId,
    ...(title !== undefined ? { title } : {}),
    ...(row.kind !== undefined ? { kind: row.kind } : {}),
  };
}

function owedFor(
  row: ConstructionRow,
  session: ConstructionSessionState | null | undefined,
  titleFor: ((id: string) => string | undefined) | undefined
): OwedItem | undefined {
  // A recorded terminal failure outranks whatever a (likely closed) session says.
  if (row.status === 'failed' || row.failureReason !== undefined) {
    return {
      ...base(row, 'failed', titleFor),
      key: `${row.activityId}:failed`,
      reviewers: [],
      failure: {
        reason: row.failureReason ?? 'unknown',
        ...(row.failureDetail !== undefined ? { detail: row.failureDetail } : {}),
      },
    };
  }
  if (session === null || session === undefined) return undefined;
  const reviewers = session.view.reviewSet?.reviewers ?? [];
  if (session.stage === 'awaitingTakeover') {
    const summary = session.view.variance?.summary;
    return {
      ...base(row, 'takeover', titleFor),
      key: `${row.activityId}:takeover`,
      reviewers,
      ...(summary !== undefined && summary.length > 0 ? { variance: summary } : {}),
    };
  }
  if (session.stage !== 'awaitingApproval') return undefined;
  const gate = gateFor(row);
  const gateTask = gate.task;
  const round = gateTask !== undefined ? row.attempts.filter((a) => a.task === gateTask).length : 0;
  return {
    ...base(row, 'gate', titleFor),
    key:
      gateTask !== undefined && round > 0
        ? `${row.activityId}:${gateTask}:${String(round)}`
        : `${row.activityId}:gate`,
    gate,
    ...(round > 0 ? { round } : {}),
    reviewers,
  };
}

/**
 * Every owed decision across the rows, in activity-id order (owedRanking.ts
 * applies the product's default sort on top).
 */
export function owedItemsFor(input: {
  rows: Readonly<Record<string, ConstructionRow>> | undefined;
  sessions: SessionsByActivity;
  titleFor?: (id: string) => string | undefined;
}): OwedItem[] {
  const out: OwedItem[] = [];
  for (const row of Object.values(input.rows ?? {})) {
    const item = owedFor(row, input.sessions[row.activityId], input.titleFor);
    if (item !== undefined) out.push(item);
  }
  return out.sort((a, b) => a.activityId.localeCompare(b.activityId));
}
