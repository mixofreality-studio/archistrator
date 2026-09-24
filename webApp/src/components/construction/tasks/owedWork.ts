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
 * cap at once, so the probe count stays that small. The start stamp is written in
 * both shipping profiles (cloud = GitHub, local = GitLocal); only unit-test setups
 * lack it (plan Q1, ruled: keep these per-child probes — they also find orphans).
 *
 * A PROBE THAT HAS NOT ANSWERED IS NOT AN ANSWER
 * ----------------------------------------------
 * A candidate whose probe is still in flight, or failed without answering, is
 * neither owed nor clear: nobody knows. owedWorkFor counts those as `unchecked`,
 * split into pending and errored, and the lens says "Nothing needs you." only when
 * that count is zero (architect Q1 — the honesty gap this branch shipped with).
 */
import type {
  ArtifactSlotView,
  ConstructionReviewer,
  ConstructionRow,
  ConstructionSessionState,
} from '../../../contracts/types';
import type { FailureReason } from '../../../contracts/enums.gen';
import type { ActivityKind } from '../KindBadge';
import { ARTIFACT_STAGE_APP_STRINGS } from '../../../contracts/enums.gen.ts';
import { lifecycleFor } from '../../activity/lifecycles.gen.ts';
import { SLOT_KIND } from '../../activity/taskArtifactFor.ts';
import { profileFor } from '../detail/bodies/taskBriefing.ts';

/** Why a row is owed. */
export type OwedReason = 'gate' | 'takeover' | 'failed';

/** The gate a `gate` item is suspended at. Every field past `lifecyclePhase` is
 *  resolved from the activity's generated profile and ABSENT where the profile
 *  cannot say — an unclassified row, or a phase its profile does not carry. */
export interface OwedGate {
  /** The lifecycle-phase wire name the decision is sent against (the row's
   *  currentLifecyclePhase, the key submit-phase-decision takes). Absent when the
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

/** What one row's evidence says: a decision is owed, nothing is owed, or the
 *  probe that would say has not answered. */
type RowVerdict = OwedItem | 'clear' | 'unchecked';

function owedFor(
  row: ConstructionRow,
  session: ConstructionSessionState | null | undefined,
  titleFor: ((id: string) => string | undefined) | undefined
): RowVerdict {
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
  // `null` is an ANSWER (the probe established there is no session). `undefined`
  // is the absence of one — pending, errored, or never asked — and must never read
  // as "nothing is owed".
  if (session === undefined) return 'unchecked';
  if (session === null) return 'clear';
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
  if (session.stage !== 'awaitingApproval') return 'clear';
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
 * The three DESIGN activities at the head of the plan. They are dispatched by
 * the design rails, not the construction pump, so `constructionGetSessionState`
 * never answers for them and {@link owedFor}'s gate branch — which needs a live
 * session at `awaitingApproval` — can never fire for one.
 */
const DESIGN_KINDS: ReadonlySet<string> = new Set([
  'requirements',
  'architecture',
  'projectDesign',
]);

/**
 * A DESIGN activity's owed decision, derived rather than probed.
 *
 * requirements / architecture / projectDesign are dispatched by the design
 * rails, not the construction pump, so `constructionGetSessionState` never
 * answers for them and `owedFor`'s gate branch cannot see them (it needs a
 * session at `awaitingApproval`). What IS visible, from the same project read
 * the plan already makes, is the artifact slot: a slot at
 * `stage === 'awaitingReview'` is precisely "a draft is staged and a human
 * owes it a verdict". That is the whole condition, and it needs no probe, no
 * new op and no second poll.
 *
 * The lifecycle's phase → artifactKind mapping says WHICH slots belong to the
 * activity (requirements: mission/glossary/volatilities/coreUseCases;
 * architecture: system; projectDesign: sdpReview), and the first such slot
 * awaiting review is the gate the row is sitting at.
 */
export function designOwedFor(
  row: ConstructionRow,
  slots: readonly ArtifactSlotView[],
  titleFor: ((id: string) => string | undefined) | undefined
): OwedItem | 'clear' {
  const def = row.kind === undefined ? undefined : lifecycleFor(row.kind);
  if (def === undefined) return 'clear';
  // `adapters.slotStageFromOrdinal` is EXACTLY this index, and is the function
  // every container calls — but `contracts/adapters.ts` cannot be loaded by
  // `node --test` (its relative value imports carry no extension, and adding
  // them breaks `uitests`' own tsconfig, which type-checks adapters through a
  // type-only import in `testids.ts`). So this pure module reads the ONE
  // GENERATED tuple that function is a one-line index into — the same single
  // source of truth, never a hand-written ordinal switch.
  const stageOf = new Map(slots.map((s) => [s.kind, ARTIFACT_STAGE_APP_STRINGS[s.stage]]));
  // The phases in their authored order — the first one still awaiting a verdict
  // is where the activity is stopped. A later phase's slot cannot be staged
  // before an earlier one's is committed, so "first" is also "current".
  for (const phase of def.phases) {
    const artifactKind = def.tasks.find(
      (t) => t.phase === phase.id && t.artifactKind !== undefined
    )?.artifactKind;
    const slotKind = artifactKind === undefined ? undefined : SLOT_KIND[artifactKind];
    if (slotKind === undefined || stageOf.get(slotKind) !== 'awaitingReview') continue;
    const gateTask = def.tasks.find((t) => t.id === phase.gate);
    const round = row.attempts.filter((a) => a.task === phase.gate).length;
    return {
      ...base(row, 'gate', titleFor),
      key:
        round > 0 ? `${row.activityId}:${phase.gate}:${String(round)}` : `${row.activityId}:gate`,
      gate: {
        lifecyclePhase: phase.id,
        phaseName: phase.label,
        exitCriterion: phase.exitCriterion,
        task: phase.gate,
        ...(gateTask !== undefined ? { label: gateTask.title } : {}),
      },
      ...(round > 0 ? { round } : {}),
      // A design review's reviewer set lives on its own review thread, not on a
      // construction session — this derivation has none to report, and says so
      // with an empty set rather than inventing one.
      reviewers: [],
    };
  }
  return 'clear';
}

/** Probe candidates with no answer yet — each id in exactly one list, sorted. */
export interface UncheckedProbes {
  /** Still in their first fetch. */
  pending: readonly string[];
  /** Failed without answering (the retry is spent). */
  errored: readonly string[];
}

export interface OwedWork {
  /** Every owed decision, in activity-id order (owedRanking.ts ranks on top). */
  items: OwedItem[];
  unchecked: UncheckedProbes;
}

export interface OwedWorkInput {
  rows: Readonly<Record<string, ConstructionRow>> | undefined;
  sessions: SessionsByActivity;
  /** Probes that failed without answering (constructionSessions.erroredProbesFor). */
  erroredProbes?: readonly string[];
  /** The project's artifact slots — the DESIGN activities' only owed evidence
   *  (designOwedFor). Absent leaves the three design rows clear, which is what
   *  every caller that cannot see the slots should say. */
  slots?: readonly ArtifactSlotView[];
  titleFor?: (id: string) => string | undefined;
}

/**
 * The owed set, and what could not be checked. Only a probe CANDIDATE can be
 * unchecked: a row nobody asks about (backfilled, planned, finished) was never
 * going to have a session to report.
 */
export function owedWorkFor(input: OwedWorkInput): OwedWork {
  const candidates = new Set(probeCandidatesFor(input.rows));
  const errored = new Set(input.erroredProbes ?? []);
  const items: OwedItem[] = [];
  const pending: string[] = [];
  const failed: string[] = [];
  for (const row of Object.values(input.rows ?? {})) {
    // The DESIGN branch is taken FIRST, and it is total: the session branch
    // below can never fire for these three kinds (no construction session is
    // ever opened for them), so asking it would report every design activity
    // as clear no matter what its slot says.
    // ...except for a recorded terminal failure, which outranks every gate and
    // is reported the same way for a design activity as for any other.
    const terminallyFailed = row.status === 'failed' || row.failureReason !== undefined;
    const verdict =
      !terminallyFailed && row.kind !== undefined && DESIGN_KINDS.has(row.kind)
        ? designOwedFor(row, input.slots ?? [], input.titleFor)
        : owedFor(row, input.sessions[row.activityId], input.titleFor);
    if (verdict === 'clear') continue;
    if (verdict === 'unchecked') {
      if (candidates.has(row.activityId)) {
        (errored.has(row.activityId) ? failed : pending).push(row.activityId);
      }
      continue;
    }
    items.push(verdict);
  }
  const byId = (a: string, b: string): number => a.localeCompare(b);
  return {
    items: items.sort((a, b) => byId(a.activityId, b.activityId)),
    unchecked: { pending: pending.sort(byId), errored: failed.sort(byId) },
  };
}

/** Every owed decision across the rows, in activity-id order. */
export function owedItemsFor(input: OwedWorkInput): OwedItem[] {
  return owedWorkFor(input).items;
}
