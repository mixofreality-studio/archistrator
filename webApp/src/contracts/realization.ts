/**
 * Pure per-use-case call-chain realization: joins a System's step-keyed
 * DynamicView (dynamicViews[].steps, one CallStep per authored activity node)
 * onto a use case, WITHOUT walking the activity graph — that DFS linearization
 * (global sequencing for the Architecture step-through) lives in adapters.ts'
 * toDynamicView; this module is the simpler per-node join the Core Use Cases
 * activity-diagram overlay (Tasks 9-11) consumes directly.
 *
 * Kept import-free of adapters (type-only imports from './types') so it is
 * directly unit-testable under `node --test` (adapters.ts' extensionless
 * imports don't resolve there — see useCaseViews.ts for the same discipline).
 */
import type { CallMode, System, UseCase } from './types';

/** One realized call: a Relationship stripped to the fields the overlay needs. */
export interface RealizedCall {
  from: string;
  to: string;
  mode: CallMode;
  label: string;
}

/** One activity node's realized calls (authored order, unlinearized). */
export interface RealizedStep {
  nodeId: string;
  calls: RealizedCall[];
}

/**
 * Resolves the System's dynamic view linked to `useCaseId` (first match, same
 * convention as viewKeyForUseCase/useCaseViews.ts) and indexes its steps by
 * activityNodeId. A node absent from the returned map has no realized calls —
 * callers render no overlay for it. Empty map when the system is absent, the id
 * is blank, no view links back, or the view carries no steps.
 */
export function realizationByNode(
  system: System | undefined,
  useCaseId: string
): Map<string, RealizedStep> {
  const map = new Map<string, RealizedStep>();
  if (system === undefined) return map;
  const id = useCaseId.trim();
  if (id.length === 0) return map;

  const view = (system.dynamicViews ?? []).find((v) => v.useCaseId === id);
  if (view === undefined) return map;

  for (const step of view.steps ?? []) {
    const calls = (step.calls ?? []).map(
      (c): RealizedCall => ({ from: c.from, to: c.to, mode: c.mode, label: c.label })
    );
    map.set(step.activityNodeId, { nodeId: step.activityNodeId, calls });
  }
  return map;
}

/**
 * The owning use case's actors that appear as a call endpoint (`from` or `to`)
 * anywhere in its realized dynamic view — the "person" participants a sequence
 * view/overlay must render alongside the System's components. Actor order
 * mirrors `uc.actors` (not call-appearance order). Empty when the system or use
 * case is absent, or the use case links no dynamic view.
 */
export function personParticipants(
  system: System | undefined,
  uc: UseCase | undefined
): { id: string; role: string }[] {
  if (system === undefined || uc === undefined) return [];

  const view = (system.dynamicViews ?? []).find((v) => v.useCaseId === uc.id);
  if (view === undefined) return [];

  const endpoints = new Set<string>();
  for (const step of view.steps ?? []) {
    for (const call of step.calls ?? []) {
      endpoints.add(call.from);
      endpoints.add(call.to);
    }
  }

  return (uc.actors ?? [])
    .filter((a) => endpoints.has(a.id))
    .map((a) => ({ id: a.id, role: a.role }));
}
