/**
 * The steer-needed and failed rows' actions: Retry…, ⋯ Skip… and Re-queue… (the PM's
 * Q3 ruling, pm-q3-ruling.md; its copy table is used verbatim). They ship LOCKED.
 *
 * The PM's must-hold: Retry and Re-queue carry an operator note, and they may not
 * ship until that note provably reaches the agent's next attempt. The server now
 * persists the note and delivers it on both venues. What unlocks the actions is
 * the verification run (one local run and one GitHub run carrying a note), which
 * shows the agent reads it first. Until that run passes, every action here is
 * present but disabled, and it says why. The unlock is uniform, with no venue
 * condition, and it is this ONE flag.
 *
 * Re-queue has a second lock of its own: the server has no verb that puts a
 * failed activity back in line yet. It stays disabled, with that reason, even
 * after the flag flips.
 */
import type { FailureReason } from '../../../contracts/enums.gen';
import type { OwedReason } from './owedWork.ts';

/** THE unlock. Flip it only after the note-delivery verification run passes. */
export const STEER_ACTIONS_UNLOCKED = false;

/** Why Retry, Skip and Re-queue are off while the flag is down. */
export const STEER_LOCKED_REASON =
  'Retry and re-queue unlock once a verification run shows your note reaching the agent. Until then, steer from GitHub or the MCP override_activity tool.';

/** Why Re-queue is off even when unlocked: no server verb exists for it yet. */
export const REQUEUE_NOT_BUILT_REASON =
  'Re-queue is not built yet: the server cannot put a failed activity back in line.';

export type SteerActionId = 'retry' | 'skip' | 'requeue';

export interface SteerAction {
  id: SteerActionId;
  label: string;
  disabled: boolean;
  /** Why a disabled action is disabled, said on hover. */
  reason?: string;
  /** Behind the row's ⋯ (Skip is never a primary action). */
  overflow: boolean;
}

export interface SteerBar {
  /** In the order the row shows them. Review and the GitHub link are the row's own. */
  actions: SteerAction[];
  /** Whether Review is the row's primary action. It is on plan-defect failures,
   *  and whenever the primary steer action is disabled: a disabled button is
   *  never the thing the row asks you to press. */
  reviewPrimary: boolean;
  /** The plan-defect warning a Re-queue opens with. */
  warning?: string;
}

/** Failures the plan caused, not the agent: Review is primary on these. */
const PLAN_DEFECTS: ReadonlySet<FailureReason> = new Set<FailureReason>([
  'componentUnresolved',
  'activityUnclassifiable',
  'dependencyUnresolved',
  'dependencyCycle',
]);

export function isPlanDefect(reason: FailureReason | undefined): boolean {
  return reason !== undefined && PLAN_DEFECTS.has(reason);
}

export const PLAN_DEFECT_WARNING =
  'This failed because of the plan, not the agent. Re-queuing before you amend the plan will fail the same way.';

/**
 * The steer actions for one owed row, or none for a gate (a gate's actions are
 * Approve and Send back). Takeover and Reassign are cut: both behave exactly like
 * Retry.
 */
export function steerBarFor(
  item: { reason: OwedReason; failure?: { reason: FailureReason } | undefined },
  unlocked: boolean = STEER_ACTIONS_UNLOCKED
): SteerBar | undefined {
  const lock = (a: Omit<SteerAction, 'disabled'>, extraLock?: string): SteerAction => {
    if (!unlocked) return { ...a, disabled: true, reason: STEER_LOCKED_REASON };
    if (extraLock !== undefined) return { ...a, disabled: true, reason: extraLock };
    return { ...a, disabled: false };
  };
  switch (item.reason) {
    case 'gate':
      return undefined;
    case 'takeover': {
      const actions = [
        lock({ id: 'retry', label: 'Retry…', overflow: false }),
        lock({ id: 'skip', label: 'Skip…', overflow: true }),
      ];
      return { actions, reviewPrimary: actions[0]?.disabled !== false };
    }
    case 'failed': {
      const planDefect = isPlanDefect(item.failure?.reason);
      const actions = [
        lock({ id: 'requeue', label: 'Re-queue…', overflow: false }, REQUEUE_NOT_BUILT_REASON),
      ];
      return {
        actions,
        reviewPrimary: planDefect || actions[0]?.disabled !== false,
        ...(planDefect ? { warning: PLAN_DEFECT_WARNING } : {}),
      };
    }
  }
}

// ---------------------------------------------------------------------------
// The composers' and dialog's words (the PM's copy table). The composers open
// only once the flag is up; the words live here so the unlock changes no copy.
// ---------------------------------------------------------------------------

/** Attempts a variance gets (the server's maxVarianceAttempts). */
export const MAX_VARIANCE_ATTEMPTS = 10;

function dollars(costUsd: number | undefined): string {
  return costUsd === undefined ? '$—' : `$${costUsd.toFixed(2)}`;
}

export const RETRY_COPY = {
  title: (activityId: string): string => `Retry ${activityId}`,
  noteLabel: 'What should the agent do differently?',
  notePlaceholder: 'Required. The agent reads this on its next attempt.',
  button: 'Retry now',
  dispatched: (attempt: number): string => `Retrying — attempt ${String(attempt)} dispatched`,
  didNotLand: 'Retry did not land',
  rejected: 'Rejected',
  unknown: 'Outcome unknown',
} as const;

/** `Attempt N of 10 · $X spent on this activity so far`; unknowns read "—". */
export function retryMetaLine(
  attempt: number | undefined,
  attemptBudget: number | undefined,
  costUsd: number | undefined
): string {
  const n = attempt === undefined ? '—' : String(attempt);
  const of = String(attemptBudget ?? MAX_VARIANCE_ATTEMPTS);
  return `Attempt ${n} of ${of} · ${dollars(costUsd)} spent on this activity so far`;
}

export const REQUEUE_COPY = {
  title: (activityId: string): string => `Re-queue ${activityId}`,
  noteLabel: 'What changed since it failed?',
  button: 'Re-queue',
  backInLine: 'Back in line — waits for a free worker slot',
  started: (phase: string): string => `Started — now in ${phase}`,
} as const;

/** `Failed: <reason>. N attempts · $X spent.`; unknowns read "—". */
export function requeueMetaLine(
  reason: string,
  attempts: number | undefined,
  costUsd: number | undefined
): string {
  const n = attempts === undefined ? '—' : String(attempts);
  return `Failed: ${reason}. ${n} attempts · ${dollars(costUsd)} spent.`;
}

export const SKIP_COPY = {
  title: (activityId: string): string => `Skip ${activityId} and mark it done?`,
  body: (downstream: number, critical: number): string =>
    `Nothing will be built for this activity. ${String(downstream)} downstream activities (${String(critical)} on the critical path) will start as if it were finished.`,
  noteLabel: 'Why is it safe to skip?',
  confirm: (downstream: number): string => `Skip and unblock ${String(downstream)}`,
  cancel: 'Cancel',
} as const;
