/**
 * The console's gate decisions, read back from the QueryClient's mutation cache
 * (review C1). Pure: it takes the cache's mutation STATES (useMutationState's
 * output, structurally) and returns one record per owed-item key — the latest
 * decision sent for it — so node:test pins the parsing.
 *
 * WHY THE CACHE AND NOT COMPONENT STATE
 * -------------------------------------
 * Component state dies with the console. Navigating home and back while a
 * decision was on the wire used to bring the gate back with Approve enabled, and a
 * second click sent a second signal. The mutation cache belongs to the
 * QueryClient: a pending decision is still pending after a remount, and a settled
 * one keeps its answer (and the item it was made on, for the lingering row) for the
 * cache's gcTime, far longer than the 30s linger.
 */
import type { DecisionRecord, GateDecision } from './decisionFlow.ts';
import type { RankedOwed } from './owedRanking.ts';

/** The fields of a TanStack MutationState this reads — structurally. */
export interface DecisionMutationState {
  status: 'idle' | 'pending' | 'success' | 'error';
  variables?: unknown;
  data?: unknown;
  error?: unknown;
  submittedAt: number;
}

export interface DecisionEntry {
  record: DecisionRecord;
  /** The owed item as it was when decided — what a lingering row shows. Absent
   *  when the cached snapshot does not read as one. */
  item?: RankedOwed;
  /** The request is on the wire right now. */
  pending: boolean;
}

interface Vars {
  activityId: string;
  phase: string;
  decision: GateDecision;
  occurrence: { key: string; epoch: number; snapshot?: unknown };
}

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null;

function varsOf(v: unknown): Vars | undefined {
  if (!isObject(v) || !isObject(v['occurrence'])) return undefined;
  const occ = v['occurrence'];
  const { activityId, phase, decision } = v;
  if (typeof activityId !== 'string' || typeof phase !== 'string') return undefined;
  if (decision !== 'approve' && decision !== 'sendBack') return undefined;
  if (typeof occ['key'] !== 'string' || typeof occ['epoch'] !== 'number') return undefined;
  return {
    activityId,
    phase,
    decision,
    occurrence: { key: occ['key'], epoch: occ['epoch'], snapshot: occ['snapshot'] },
  };
}

/** The owed-item key a cached decision's variables carry, if they read as one. */
export function decisionKeyOf(variables: unknown): string | undefined {
  return varsOf(variables)?.occurrence.key;
}

/** A shallow read of the snapshot the route put there itself. */
function itemOf(snapshot: unknown, key: string): RankedOwed | undefined {
  if (!isObject(snapshot) || snapshot['key'] !== key) return undefined;
  if (!isObject(snapshot['blast']) || !isObject(snapshot['why'])) return undefined;
  return snapshot as unknown as RankedOwed;
}

function answeredAtOf(v: unknown): number | undefined {
  return isObject(v) && typeof v['answeredAt'] === 'number' ? v['answeredAt'] : undefined;
}

function errorOf(v: unknown): { status?: number; message: string } {
  if (!isObject(v)) return { message: String(v) };
  const message = typeof v['message'] === 'string' ? v['message'] : 'request failed';
  return typeof v['status'] === 'number' ? { status: v['status'], message } : { message };
}

/** One entry per owed-item key: the LATEST decision sent for it. */
export function decisionEntriesFrom(
  states: readonly DecisionMutationState[]
): Record<string, DecisionEntry> {
  const latest = new Map<string, { entry: DecisionEntry; submittedAt: number }>();
  for (const s of states) {
    const vars = varsOf(s.variables);
    if (vars === undefined || s.status === 'idle') continue;
    const key = vars.occurrence.key;
    const prev = latest.get(key);
    if (prev !== undefined && prev.submittedAt > s.submittedAt) continue;
    const sentAt =
      s.status === 'success'
        ? answeredAtOf(s.data)
        : s.status === 'error'
          ? (answeredAtOf(s.error) ?? s.submittedAt)
          : undefined;
    const record: DecisionRecord = {
      key,
      activityId: vars.activityId,
      decision: vars.decision,
      epoch: vars.occurrence.epoch,
      gatedPhase: vars.phase,
      ...(sentAt !== undefined ? { sentAt } : {}),
      ...(s.status === 'error' ? { error: errorOf(s.error) } : {}),
    };
    const item = itemOf(vars.occurrence.snapshot, key);
    latest.set(key, {
      entry: { record, pending: s.status === 'pending', ...(item !== undefined ? { item } : {}) },
      submittedAt: s.submittedAt,
    });
  }
  return Object.fromEntries([...latest].map(([key, { entry }]) => [key, entry]));
}
